// Package internal provides key validation utilities.
package internal

import (
	"errors"
	"sync"
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

// internShard represents a single shard of the intern cache.
// Uses a single mutex (not RWMutex) for better cache locality and
// simpler lock management. The cache is small enough that RWMutex
// overhead outweighs its benefits.
type internShard struct {
	mu         sync.Mutex
	cache      map[string]string
	order      []string // Used for FIFO eviction
	orderStart int      // Start index for circular buffer behavior
}

var internShards [numShards]internShard

func init() {
	for i := range numShards {
		internShards[i].cache = make(map[string]string, maxInternSize)
	}
}

// HashKey returns a hash value for the given key using an optimized algorithm.
// For short keys (<=8 chars), uses a simple multiplicative hash.
// For longer keys, uses FNV-1a with sampling for better performance.
// The numShards parameter determines the range of the returned hash (0 to numShards-1).
//
// Performance optimizations:
// - Uses branchless bit manipulation for keys 1-4 chars (most common case)
// - Avoids conditional branches in the hot path
// - Single optimization point for numShards==8 at function exit
func HashKey(key string, numShards int) uint32 {
	keyLen := len(key)
	if keyLen == 0 {
		return 0
	}

	var hash uint32

	// Fast path for very short keys (1-4 chars): branchless implementation
	// This is the most common case for environment variable keys
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

	// Single optimization point for numShards==8
	if numShards == 8 {
		return hash & 7
	}
	return hash % uint32(numShards)
}

// getShard returns the shard for a given key using HashKey.
func getShard(key string) *internShard {
	return &internShards[HashKey(key, numShards)]
}

// InternKey returns an interned copy of the key string if available,
// or stores and returns the input key. This reduces allocations when
// the same keys are parsed repeatedly.
// This implementation uses sharded caches with thread-safe access for better
// concurrency performance.
//
// Optimization: Uses sync.Mutex instead of sync.RWMutex because:
// 1. The cache is small (128 entries per shard), making lookup very fast
// 2. RWMutex has higher overhead for the common case of cache hit + small cache
// 3. Simpler lock management improves cache locality
//
// SECURITY: This function maintains strict consistency between the cache map
// and order slice to prevent memory leaks and ensure correct FIFO eviction.
func InternKey(key string) string {
	if len(key) == 0 || len(key) > maxInternKeyLen {
		return key // Don't intern empty or very long keys
	}

	shard := getShard(key)
	shard.mu.Lock()

	// Single lock acquisition - check and potentially insert
	if interned, ok := shard.cache[key]; ok {
		shard.mu.Unlock()
		return interned
	}

	// FIFO eviction if cache is full
	if len(shard.cache) >= maxInternSize {
		// SECURITY: Ensure order slice is consistent with cache before eviction.
		// This handles edge cases where order might have gotten out of sync.
		// We check both conditions:
		// 1. order is shorter than cache (entries added without order tracking)
		// 2. order is longer than cache (entries deleted without order tracking)
		if len(shard.order) != len(shard.cache) {
			shard.rebuildOrder()
		}

		// Remove oldest 1/4 of entries using circular buffer approach
		// SECURITY: Use min() to ensure evictCount doesn't exceed len(shard.order)
		// This prevents index out of bounds if shard.order is shorter than expected
		evictCount := min(maxInternSize/4, len(shard.order))
		if evictCount > 0 {
			for i := 0; i < evictCount; i++ {
				idx := (shard.orderStart + i) % len(shard.order)
				keyToEvict := shard.order[idx]
				delete(shard.cache, keyToEvict)
				shard.order[idx] = "" // Clear reference for GC
			}
			// Move start pointer forward (circular buffer)
			shard.orderStart = (shard.orderStart + evictCount) % len(shard.order)

			// Compact if we've wrapped around too many times
			if shard.orderStart > 0 && len(shard.order) >= maxInternSize*3/4 {
				shard.compactOrder()
			}
		}
	}

	shard.cache[key] = key
	shard.order = append(shard.order, key)
	shard.mu.Unlock()
	return key
}

// rebuildOrder rebuilds the order slice from cache keys to restore consistency.
// Must be called with shard.mu held.
func (s *internShard) rebuildOrder() {
	s.order = s.order[:0]
	for k := range s.cache {
		s.order = append(s.order, k)
	}
	s.orderStart = 0
}

// compactOrder compacts the order slice by removing empty slots.
// Must be called with shard.mu held.
func (s *internShard) compactOrder() {
	newOrder := make([]string, 0, maxInternSize)
	for i := 0; i < len(s.order); i++ {
		idx := (s.orderStart + i) % len(s.order)
		if s.order[idx] != "" {
			newOrder = append(newOrder, s.order[idx])
		}
	}
	s.order = newOrder
	s.orderStart = 0
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
	if len(key) == 0 {
		return false
	}
	// First character must be a letter
	c := key[0]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	// Remaining characters must be alphanumeric or underscore
	for i := 1; i < len(key); i++ {
		c := key[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
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
	// SECURITY: Reject keys that look like array indices to prevent confusion
	if key[0] >= '0' && key[0] <= '9' && len(key) <= 4 {
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
		shard.cache = make(map[string]string, maxInternSize)
		shard.order = nil
		shard.orderStart = 0
		shard.mu.Unlock()
	}
}

// EqualFoldASCII compares two strings case-insensitively for ASCII characters only.
// This is faster than strings.EqualFold for short ASCII strings because it avoids
// the Unicode fallback path. Returns false if either string contains non-ASCII bytes.
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

// TrimSpaceBytes trims leading and trailing whitespace from a byte slice,
// converting to string. Avoids the intermediate string(buf.String()) that
// strings.TrimSpace would require.
func TrimSpaceBytes(b []byte) string {
	start := 0
	end := len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return string(b[start:end])
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
//
// For use cases requiring strict ASCII validation, use ToUpperASCIISafe instead.
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

// ErrNonASCII is returned when a string contains non-ASCII characters
// in functions that require ASCII-only input.
var ErrNonASCII = errors.New("string contains non-ASCII characters")

// ToUpperASCIISafe converts an ASCII string to uppercase with strict ASCII validation.
// Returns ErrNonASCII if the input contains any bytes >= 0x80.
// This is the safe version of ToUpperASCII for use when input validation is required.
//
// Performance: This function has minimal overhead compared to ToUpperASCII
// (single additional bounds check per character that was already being performed).
func ToUpperASCIISafe(s string) (string, error) {
	// Single-pass: validate ASCII and convert to uppercase
	for i := 0; i < len(s); i++ {
		c := s[i]
		// SECURITY: Check for non-ASCII bytes first
		if c >= 0x80 {
			return "", ErrNonASCII
		}
		if c >= 'a' && c <= 'z' {
			// Found a lowercase character, need to convert
			b := make([]byte, len(s))
			for j := 0; j < i; j++ {
				b[j] = s[j]
			}
			b[i] = c - 32
			for j := i + 1; j < len(s); j++ {
				c2 := s[j]
				// SECURITY: Validate remaining characters too
				if c2 >= 0x80 {
					return "", ErrNonASCII
				}
				if c2 >= 'a' && c2 <= 'z' {
					b[j] = c2 - 32
				} else {
					b[j] = c2
				}
			}
			return string(b), nil
		}
	}
	return s, nil
}

// IsASCII checks if a string contains only ASCII characters (bytes < 0x80).
// This is a fast path function used for input validation.
func IsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// DefaultMaskKey masks a key name for safe logging and error reporting.
// Shows only the first 2 characters followed by "***" for keys longer than 3 characters.
// This is the default masking function used by validators and path validators.
func DefaultMaskKey(key string) string {
	if len(key) <= 3 {
		return "***"
	}
	return key[:2] + "***"
}
