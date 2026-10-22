// Provides key validation utilities.

package internal

import (
	"strings"
	"sync"
	"sync/atomic"
)

// Hash constant for multiplicative hashing.
// This is Knuth's multiplicative hash constant: 2^32 / φ (golden ratio).
// The golden ratio φ ≈ 1.6180339887... provides good distribution properties.
// Using this constant spreads hash values uniformly across the output space.
const hashMultiplier = 2654435761

// Key interning cache limits.
// maxInternSize is the maximum number of keys to cache per shard.
// maxInternKeyLen is the maximum key length to intern (longer keys are not cached
// as they are less likely to be repeated and would waste memory).
const (
	maxInternSize   = 128 // Per shard (increased from 64 for better hit rate)
	maxInternKeyLen = 64
	numShards       = 8 // Increased from 4 for better concurrency on modern CPUs
)

// internEntry is the immutable unit stored in both the shard maps and the
// front-cache slots. The cache maps hold *internEntry (not bare strings) so
// publishing a hit into a front slot reuses the existing pointer: taking the
// address of a string local to make a *string would force that local to
// escape, and the compiler would heap-allocate it on EVERY slow-path call —
// measured as +16 B/op on parses whose keys lost their slot to a collision —
// even on calls that never store.
type internEntry struct {
	key string
}

// internShard represents a single shard of the intern cache.
// Uses a single mutex (not RWMutex) for better cache locality and
// simpler lock management. The cache is small enough that RWMutex
// overhead outweighs its benefits.
type internShard struct {
	mu    sync.Mutex
	cache map[string]*internEntry
}

var internShards [numShards]internShard

func init() {
	for i := range numShards {
		internShards[i].cache = make(map[string]*internEntry, maxInternSize)
	}
}

// HashKey returns a hash value for the given key.
// For short keys (<=8 chars) it uses a multiplicative hash that combines the
// key bytes without a per-character loop; for longer keys it uses FNV-1a over
// the first and last 8 bytes (sampling).
// The shards parameter determines the range of the returned hash (0 to shards-1).
//
// Performance notes:
//   - For keys 1-4 chars (the common case) the bytes are combined with fixed
//     indexing rather than a loop.
//   - Power-of-two shard counts (the only values used in this package) map to
//     a single AND.
func HashKey(key string, shards int) uint32 {
	hash := hashKeyRaw(key)

	// Defensive: shards <= 0 has no valid answer; returning 0 keeps this a
	// total function instead of panicking on the modulo below. All current
	// callers pass power-of-two constants (8, 32), so this arm is latent.
	if shards <= 0 {
		return 0
	}
	// Power-of-two shard counts (all current callers) reduce to a single AND,
	// which the compiler constant-folds when shards is a constant argument.
	if shards&(shards-1) == 0 {
		return hash & uint32(shards-1)
	}
	return hash % uint32(shards)
}

// hashKeyRaw is HashKey before the shard-range reduction: the full 32-bit hash
// from which both the shard index and the intern front-cache slot are derived.
func hashKeyRaw(key string) uint32 {
	keyLen := len(key)
	if keyLen == 0 {
		return 0
	}

	var hash uint32

	// Fast path for very short keys (1-4 chars): loop-free byte combination.
	// This is the most common case for environment variable keys.
	if keyLen <= 4 {
		// SAFETY: Go guarantees zero-initialization for local variables.
		// The array b is fully initialized to zeros before we copy key bytes.
		// For keys shorter than 4 chars, unused positions remain zero.
		// The hash calculation correctly incorporates keyLen to ensure
		// different-length keys produce different hashes even if their
		// common prefix bytes are identical.
		var b [4]byte
		for i := 0; i < keyLen; i++ {
			b[i] = key[i]
		}
		hash = uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
		hash ^= uint32(keyLen) * hashMultiplier
	} else if keyLen <= 8 {
		// Fast path for short keys (5-8 chars)
		hash = uint32(key[0]) | uint32(key[1])<<8 | uint32(key[2])<<16 | uint32(key[3])<<24
		hash ^= uint32(key[4]) * hashMultiplier
		if keyLen > 5 {
			hash ^= uint32(key[5]) * hashMultiplier
		}
		if keyLen > 6 {
			hash ^= uint32(key[6]) * hashMultiplier
		}
		if keyLen > 7 {
			hash ^= uint32(key[7]) * hashMultiplier
		}
		hash ^= uint32(keyLen) * hashMultiplier
	} else {
		// FNV-1a hash for longer keys with sampling
		hash = uint32(2166136261) // FNV offset basis
		if keyLen <= 16 {
			for i := 0; i < keyLen; i++ {
				hash ^= uint32(key[i])
				hash *= 16777619 // FNV prime
			}
		} else {
			// Sample first 8 and last 8 characters for long keys
			for i := 0; i < 8; i++ {
				hash ^= uint32(key[i])
				hash *= 16777619
			}
			for i := keyLen - 8; i < keyLen; i++ {
				hash ^= uint32(key[i])
				hash *= 16777619
			}
		}
	}
	return hash
}

// getShard returns the shard for a given key using HashKey.
func getShard(key string) *internShard {
	return &internShards[HashKey(key, numShards)]
}

// internFrontSlots is the slot count of the lock-free front cache in front of
// the sharded intern maps. It must be a power of two (slot selection masks).
// Slots are filled once and never displaced (see InternKeyBytes), so the
// count is sized to keep fill-time collisions rare for realistic key counts
// (~5 colliding pairs per 100 keys at 1024 slots; 8KB of static storage).
const internFrontSlots = 1024

// internFront is a direct-mapped atomic cache indexed by an independent
// position-dependent hash of the key bytes. A hit returns without touching the
// shard mutex; a miss falls through to the locked sharded map, which remains
// the authoritative store. Profiling showed the shard lock + map probe at
// ~11% of .env parse CPU with all-hits traffic, dominated by the mutex
// round-trip; the atomic load removes it.
//
// Concurrency: entries are immutable once published, so readers only need the
// atomic load/store ordering. A slot may point at an entry that the sharded
// map has since evicted — the string is still valid, stable memory, which is
// all the interning contract requires.
var internFront [internFrontSlots]atomic.Pointer[internEntry]

// internSlotHash is the front-cache slot hash (FNV-1a over the key bytes),
// deliberately independent of hashKeyRaw: the short-key multiplicative hash
// XORs position-4..7 bytes against the SAME multiplier, so byte-swapped keys
// ("VAR_19" vs "VAR_91") collide in it. That is harmless for shard selection
// (the shard map compares full keys) but fatal for a direct-mapped slot
// cache, where such a pair would evict each other on every call.
func internSlotHash(b []byte) uint32 {
	h := uint32(2166136261) // FNV offset basis
	for i := 0; i < len(b); i++ {
		h ^= uint32(b[i])
		h *= 16777619 // FNV prime
	}
	return h
}

// equalStringBytes compares s and b without allocating (a manual loop rather
// than relying on the compiler eliding string(b) in comparisons).
func equalStringBytes(s string, b []byte) bool {
	if len(s) != len(b) {
		return false
	}
	for i := range b {
		if s[i] != b[i] {
			return false
		}
	}
	return true
}

// InternKeyBytes interns a key supplied as a byte slice, returning a stable
// interned string. This reduces allocations when the same keys are parsed
// repeatedly, which matters on hot parse paths where keys arrive as []byte
// slices into a reusable buffer.
//
// Why this avoids an allocation on a cache hit: the compiler optimizes the
// string(b) conversions away:
//   - getShard(string(b)) on the slow path: escape analysis proves the
//     converted string does not escape (HashKey only reads its bytes), so no
//     allocation occurs.
//   - shard.cache[string(b)]: the documented compiler elision for
//     map[string]V[string([]byte)] index expressions allocates nothing.
//
// On a cache miss the key is allocated exactly once (inside storeInterned,
// which stores it in the map). The returned string is a stable heap copy
// independent of b, so it stays valid after b is reused — this preserves the
// scanner-buffer-safety invariant required by ParseLineBytes.
//
// Read path: a lock-free direct-mapped front cache (see internFront) absorbs
// repeated keys before the shard mutex is taken. The sharded maps remain the
// authoritative bounded store (sync.Mutex, not RWMutex: the cache is small —
// 128 entries per shard — so lookup is fast enough that RWMutex overhead
// outweighs its benefit, and simpler lock management improves cache locality).
func InternKeyBytes(b []byte) string {
	if len(b) == 0 || len(b) > maxInternKeyLen {
		return string(b) // Don't intern empty or very long keys; must copy
	}

	slot := &internFront[internSlotHash(b)&(internFrontSlots-1)]
	if hit := slot.Load(); hit != nil && equalStringBytes(hit.key, b) {
		return hit.key
	}

	shard := getShard(string(b))
	shard.mu.Lock()
	defer shard.mu.Unlock()

	var entry *internEntry
	if cached, ok := shard.cache[string(b)]; ok {
		entry = cached
	} else {
		entry = storeInterned(shard, string(b))
	}
	// Fill the front slot only when empty. Displacing an occupied slot would
	// churn whenever the hot-key set exceeds the slot count; leaving the
	// incumbent in place keeps the steady state allocation-free and simply
	// routes the colliding key down the locked path. The stored pointer is
	// the map's own entry, so publishing allocates nothing (see internEntry).
	if slot.Load() == nil {
		slot.Store(entry)
	}
	return entry.key
}

// storeInterned inserts key into the shard's cache (with bounded eviction) and
// returns the stored entry. The caller must already hold shard.mu and must
// have confirmed the key is absent.
//
// Bounded cache: when full, evict a single entry. Go randomizes map iteration
// order, so evicting the first key found is effectively random eviction —
// simple and sufficient for a small interning cache. The previous FIFO
// circular-buffer machinery produced the same observable behavior (bounded
// cache, correct interning) with far more moving parts.
func storeInterned(shard *internShard, key string) *internEntry {
	if len(shard.cache) >= maxInternSize {
		for k := range shard.cache {
			delete(shard.cache, k)
			break
		}
	}

	entry := &internEntry{key: key}
	shard.cache[key] = entry
	return entry
}

// isValidDefaultKey is the canonical implementation for validating environment variable
// keys against the default pattern ^[A-Za-z][A-Za-z0-9_]*$.
//
// This function is used by:
//   - Validator.ValidateKey() when no custom KeyPattern is configured
//   - Expander for validating variable names during expansion
//
// Performance: This byte-level implementation is significantly faster than regexp
// for the common case of standard environment variable names.
//
// Ownership: This is the single source of truth for default key validation logic.
// Do not duplicate this logic elsewhere; all components should call this function
// when using the default pattern.
func isValidDefaultKey(key string) bool {
	valid, _ := validateDefaultKeyScan(key)
	return valid
}

// validateDefaultKeyScan validates ^[A-Za-z][A-Za-z0-9_]*$ in a single pass and
// also reports whether the key contains any non-ASCII byte. Both signals come
// from the same scan because every byte >= 0x80 already fails the pattern's
// character classes — the separate ASCII homograph scan in ValidateKey visited
// the exact same bytes. Callers use the non-ASCII flag to pick the more precise
// error rule (ascii_only takes precedence over pattern, matching the previous
// scan-order behavior).
func validateDefaultKeyScan(key string) (valid, hasNonASCII bool) {
	if len(key) == 0 {
		return false, false
	}
	// First character must be a letter
	c := key[0]
	if c >= 0x80 {
		return false, true
	}
	valid = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	// Remaining characters must be alphanumeric or underscore
	for i := 1; i < len(key); i++ {
		c := key[i]
		if c >= 0x80 {
			return false, true
		}
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '_' {
			valid = false
		}
	}
	return valid, false
}

// isVarChar returns true if c is a valid variable name character.
// Used by the expander for parsing variable references.
func isVarChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_'
}

// IsValidJSONKey checks if a key matches the JSON key pattern ^[A-Za-z0-9_@\-.]+$
// This is faster than using regex for the common case.
// Allowed characters: letters, digits, underscore, at-sign, hyphen, dot.
// Note: Square brackets are NOT allowed to prevent key name confusion and
// ambiguity with array index notation.
func IsValidJSONKey(key string) bool {
	if len(key) == 0 {
		return false
	}
	// SECURITY: Reject pure-digit keys of ANY length — they are ambiguous
	// with array indices in dot-notation resolution (ResolvePath resolves
	// numeric segments up to maxSafeIndex, far beyond four digits; the
	// previous <=4 guard rejected "1234" while letting "12345" through).
	if key[0] >= '0' && key[0] <= '9' {
		allDigits := true
		for i := 0; i < len(key); i++ {
			if key[i] < '0' || key[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return false // Reject pure numeric keys
		}
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		// Check allowed characters using a fast path for common cases
		// SECURITY: Square brackets [ ] are explicitly excluded to prevent
		// key confusion attacks and ambiguity with array indexing
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '@' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

// ClearInternCache clears the key interning cache.
// This is useful for long-running applications that want to release
// memory held by cached keys that are no longer needed.
// This function is safe for concurrent use.
func ClearInternCache() {
	for i := range numShards {
		shard := &internShards[i]
		shard.mu.Lock()
		shard.cache = make(map[string]*internEntry, maxInternSize)
		shard.mu.Unlock()
	}
	// Drop front-cache entries too: without this, a cleared intern cache
	// would keep answering hits from stale slot entries and repopulate the
	// shard maps on the next miss from a potentially unwanted generation.
	for i := range internFront {
		internFront[i].Store(nil)
	}
}

// EqualFoldASCII compares two strings case-insensitively for ASCII letters.
// This is faster than strings.EqualFold for short ASCII strings because it avoids
// the Unicode fallback path. Bytes outside A-Z/a-z — including all non-ASCII
// bytes — are compared literally with no Unicode case folding:
// EqualFoldASCII("ü", "ü") is true, but EqualFoldASCII("Ü", "ü") is false.
func EqualFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// SplitQuotedCommaParts splits a comma-separated list, honoring double
// quotes: a comma inside quotes does not split, the grouping quotes are
// removed, and backslash escapes (\" and \\) are unfolded. It is the
// parse-side counterpart of the quoting valueToString applies to
// string-slice elements containing ',' or '"' — together they make such
// elements survive a Marshal→Unmarshal round trip. A plain element splits
// exactly like strings.Split(s, ","). It additionally reports, per element,
// whether it was wrapped in grouping quotes; callers that trim elements
// must trim only the unquoted ones: trimming quoted elements silently
// strips the significant leading/trailing whitespace the quotes exist to
// preserve. Both list consumers (setSliceValue for struct mapping, the
// root package's parseCommaSeparated for GetSlice) follow this rule so the
// two read paths agree on the same input.
//
// For quoted elements, whitespace OUTSIDE the quotes is separator gap, not
// content: the gap between a comma and the opening quote (and between the
// closing quote and the next comma) is dropped, so 'a, "b "' yields "b " —
// not " b " — while the quoted trailing space survives.
func SplitQuotedCommaParts(s string) (parts []string, quoted []bool) {
	parts = make([]string, 0, strings.Count(s, ",")+1)
	quoted = make([]bool, 0, cap(parts))
	var elem []byte
	elemQuoted := false
	inQuotes := false
	seenContent := false
	// contentEnd bounds the element's real content: bytes appended inside
	// quotes or non-space bytes outside them. Trailing outside-quote
	// whitespace is provisional and trimmed at flush for quoted elements.
	contentEnd := 0
	flush := func() {
		if elemQuoted {
			elem = elem[:contentEnd]
		}
		parts = append(parts, string(elem))
		quoted = append(quoted, elemQuoted)
		elem = elem[:0]
		elemQuoted = false
		seenContent = false
		contentEnd = 0
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
			elemQuoted = true
			seenContent = true
		case c == '\\' && inQuotes && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\'):
			elem = append(elem, s[i+1])
			seenContent = true
			contentEnd = len(elem)
			i++
		case c == ',' && !inQuotes:
			flush()
		default:
			// Whitespace between a separator and the element's first
			// content (the opening quote counts as content) is gap, not
			// content: quoted elements must not inherit it, since their
			// defining property is that their whitespace is significant.
			if !inQuotes && !seenContent && (c == ' ' || c == '\t') {
				continue
			}
			elem = append(elem, c)
			seenContent = true
			if inQuotes || (c != ' ' && c != '\t') {
				contentEnd = len(elem)
			}
		}
	}
	flush()
	return parts, quoted
}

// HasUpperPrefix checks whether s starts with the given uppercase prefix,
// performing case-insensitive comparison on s without allocation.
// The prefix must already be uppercase.
func HasUpperPrefix(s string, upperPrefix string) bool {
	if len(s) < len(upperPrefix) {
		return false
	}
	for i := 0; i < len(upperPrefix); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		if c != upperPrefix[i] {
			return false
		}
	}
	return true
}

// TrimSpace trims leading and trailing whitespace from a string.
// This is an optimized version that returns the original string if no trimming is needed,
// avoiding allocation in the common case where values are already trimmed.
func TrimSpace(s string) string {
	// Fast path for empty string
	if len(s) == 0 {
		return s
	}

	// Find first non-whitespace character
	start := 0
	for start < len(s) {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}

	// Find last non-whitespace character
	end := len(s)
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}

	// Return original if no trimming needed
	if start == 0 && end == len(s) {
		return s
	}

	return s[start:end]
}

// ToUpperASCII converts an ASCII string to uppercase.
// This is faster than strings.ToUpper for ASCII-only strings.
// Uses single-pass algorithm: convert while detecting lowercase.
// Returns the uppercase string (shares backing array if already uppercase).
//
// SECURITY WARNING: This function is designed for ASCII-only input.
// Non-ASCII bytes (>= 0x80) are passed through unchanged without validation.
// Callers must validate input if ASCII-only keys are required for security.
// For environment variable keys, this is acceptable because:
// 1. Environment variable names are conventionally ASCII
// 2. Key validation elsewhere rejects non-ASCII keys
// 3. Visual spoofing attacks with Unicode are mitigated by key pattern validation
func ToUpperASCII(s string) string {
	// Single-pass: convert to uppercase while detecting if conversion is needed
	// This avoids the double-pass of check-then-convert
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			// Found a lowercase character, need to convert
			// Allocate buffer and copy what we've seen so far
			b := make([]byte, len(s))
			for j := 0; j < i; j++ {
				b[j] = s[j]
			}
			// Convert current character
			b[i] = c - 32
			// Continue converting remaining characters
			for j := i + 1; j < len(s); j++ {
				c2 := s[j]
				if c2 >= 'a' && c2 <= 'z' {
					b[j] = c2 - 32
				} else {
					b[j] = c2
				}
			}
			return string(b)
		}
	}
	// No lowercase characters found, return original
	return s
}

// DefaultMaskKey masks a key name for safe logging and error reporting.
// SECURITY (SEC-005): the previous two-character prefix ("DA***") leaked the
// start of the key name, and the key itself is frequently sensitive (e.g.
// DATABASE_PASSWORD). The mask is now a fixed token with no key content, so a
// masked key reveals neither its spelling nor its length. This is the default
// masking function used by validators, path validators, and error rendering.
func DefaultMaskKey(key string) string {
	return "[MASKED]"
}
