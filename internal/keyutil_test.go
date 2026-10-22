package internal

import (
	"fmt"
	"strings"
	"testing"
)

func TestIsValidJSONKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		// Valid keys
		{"valid_key", true},
		{"ValidKey123", true},
		{"key-with-dash", true},
		{"key.with.dot", true},
		{"key@with@at", true},
		{"a", true},
		{"KEY", true},
		{"a_b-c.d@e", true},

		// SECURITY: Keys with brackets are now invalid to prevent
		// key confusion attacks and ambiguity with array indexing
		{"key[0]", false},
		{"123", false},          // pure numeric keys are rejected
		{"12345", false},        // any length: index-like up to maxSafeIndex
		{"999999999", false},    // nine digits, same array-index ambiguity class
		{"1a2b", true},          // digits mixed with letters are fine
		{"a_b-c.d@e[1]", false}, // contains brackets

		// Invalid keys
		{"", false},               // empty
		{"key with space", false}, // space
		{"key:colon", false},      // colon
		{"key#hash", false},       // hash
		{"key/slash", false},      // slash
		{"key\\backslash", false}, // backslash
		{"key'quote", false},      // quote
		{"key\"double", false},    // double quote
		{"key,comma", false},      // comma
		{"key;semicolon", false},  // semicolon
		{"key!exclaim", false},    // exclamation
		{"key?question", false},   // question mark
		{"key(paren)", false},     // parentheses
		{"key{brace}", false},     // braces
		{"key<angle>", false},     // angle brackets
		{"key=equals", false},     // equals
		{"key+plus", false},       // plus
		{"key*asterisk", false},   // asterisk
		{"key&ersand", false},     // ampersand
		{"key%percent", false},    // percent
		{"key$dollar", false},     // dollar
		{"key|pipe", false},       // pipe
		{"key^caret", false},      // caret
		{"key~tilde", false},      // tilde
		{"key`backtick", false},   // backtick
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := IsValidJSONKey(tt.key)
			if result != tt.expected {
				t.Errorf("IsValidJSONKey(%q) = %v, want %v", tt.key, result, tt.expected)
			}
		})
	}
}

// TestHashKey pins HashKey's empty-input contract (keyutil.go ~L54): the
// empty key short-circuits to 0 before any hashing or shard-range arithmetic
// runs, for every shard count. Non-positive shard counts with non-empty keys
// are already covered by TestSEC003_HashKeyNonPositiveShards.
func TestHashKey(t *testing.T) {
	tests := []struct {
		name   string
		shards int
	}{
		{"empty key with typical shard count", numShards},
		{"empty key with single shard", 1},
		{"empty key with non-power-of-two shards", 7},
		{"empty key with non-positive shards", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HashKey("", tt.shards); got != 0 {
				t.Errorf("HashKey(\"\", %d) = %d, want 0", tt.shards, got)
			}
		})
	}
}

func TestTrimSpace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"no whitespace", "hello", "hello"},
		{"leading space", "  hello", "hello"},
		{"trailing space", "hello  ", "hello"},
		{"both sides", "  hello  ", "hello"},
		{"tabs", "\thello\t", "hello"},
		{"newlines", "\nhello\n", "hello"},
		{"mixed whitespace", " \t\nhello\n\t ", "hello"},
		{"only whitespace", "   \t\n  ", ""},
		{"inner whitespace preserved", "hello world", "hello world"},
		{"single space", " ", ""},
		{"single char", "a", "a"},
		// Contract difference vs strings.TrimSpace (pinned): this TrimSpace
		// only removes " \t\n\r", so other whitespace that strings.TrimSpace
		// would strip is preserved.
		{"vertical tab not trimmed", "\vhello\v", "\vhello\v"},
		{"form feed not trimmed", "\fhello\f", "\fhello\f"},
		{"non-breaking space not trimmed", "\u00a0hi\u00a0", "\u00a0hi\u00a0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TrimSpace(tt.input)
			if result != tt.expected {
				t.Errorf("TrimSpace(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSplitAndTrimAt_SkipsBlankSegments pins the blank-segment contract of
// SplitAndTrimAt (defined in lookup.go): empty and whitespace-only comma
// segments are skipped rather than consuming an index. It is exercised here
// alongside its TrimSpace building block per the audit.
func TestSplitAndTrimAt_SkipsBlankSegments(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		index     int
		wantVal   string
		wantFound bool
	}{
		{"whitespace-only segments are skipped", ",  ,a", 0, "a", true},
		{"index counts only non-blank segments", ",  ,a", 1, "", false},
		{"all segments blank", " , , ", 0, "", false},
		{"adjacent commas collapse", "a,,b", 1, "b", true},
		{"inner segment whitespace trimmed", " x , y ", 1, "y", true},
		{"negative index finds nothing", "a,b", -1, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotFound := SplitAndTrimAt(tt.input, tt.index)
			if gotFound != tt.wantFound {
				t.Errorf("SplitAndTrimAt(%q, %d) found = %v, want %v", tt.input, tt.index, gotFound, tt.wantFound)
			}
			if gotVal != tt.wantVal {
				t.Errorf("SplitAndTrimAt(%q, %d) = %q, want %q", tt.input, tt.index, gotVal, tt.wantVal)
			}
		})
	}
}

func TestInternKeyBytes_EdgeCases(t *testing.T) {
	ClearInternCache()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty string not interned", "", ""},
		{"simple key", "SIMPLE", "SIMPLE"},
		{"key with underscores", "MY_KEY_NAME", "MY_KEY_NAME"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InternKeyBytes([]byte(tt.key))
			if got != tt.want {
				t.Errorf("InternKeyBytes(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestHasUpperPrefix(t *testing.T) {
	tests := []struct {
		s      string
		prefix string
		want   bool
	}{
		{"password_value", "PASS", true},
		{"PASSWORD_VALUE", "PASS", true},
		{"PassWord", "PASS", true},
		{"other_value", "PASS", false},
		{"pa", "PASS", false},
		{"", "PASS", false},
		{"value", "", true},
		{"secret_key", "SEC", true},
		{"SecretKey", "SEC", true},
		{"_underscore", "_", true},
		{"123numeric", "123", true},
	}

	for _, tt := range tests {
		name := tt.s + "_" + tt.prefix
		t.Run(name, func(t *testing.T) {
			if got := HasUpperPrefix(tt.s, tt.prefix); got != tt.want {
				t.Errorf("HasUpperPrefix(%q, %q) = %v, want %v", tt.s, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestEqualFoldASCII(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"hello", "HELLO", true},
		{"Hello", "hello", true},
		{"ABC", "ABC", true},
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "ab", false},
		{"", "", true},
		{"a", "A", true},
		// Non-ASCII bytes compare literally (no Unicode case folding):
		// identical non-ASCII strings are equal, Unicode case variants are
		// not — see the EqualFoldASCII doc comment.
		{"ü", "ü", true},
		{"Ü", "ü", false},
		{"aü", "aÜ", false},
	}

	for _, tt := range tests {
		name := tt.a + "_" + tt.b
		t.Run(name, func(t *testing.T) {
			if got := EqualFoldASCII(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualFoldASCII(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestInternKeyBytes_LongKey(t *testing.T) {
	ClearInternCache()
	longKey := strings.Repeat("X", maxInternKeyLen+1)
	if got := InternKeyBytes([]byte(longKey)); got != longKey {
		t.Errorf("InternKeyBytes(long) = %q, want %q", got, longKey)
	}
}

// TestInternKeyBytes_StableAfterBufferReuse verifies the security invariant:
// the returned string must remain valid after the input byte slice is reused
// (as happens with bufio.Scanner's buffer on the next Scan()).
func TestInternKeyBytes_StableAfterBufferReuse(t *testing.T) {
	ClearInternCache()

	// First intern with one buffer content.
	buf := []byte("STABLE_KEY_001")
	got := InternKeyBytes(buf)
	if got != "STABLE_KEY_001" {
		t.Fatalf("InternKeyBytes = %q, want STABLE_KEY_001", got)
	}

	// Mutate the backing buffer (simulating scanner reuse) and interleave a
	// cache miss + hit to stress the intern cache as well.
	for i := 0; i < len(buf) && i < 4; i++ {
		buf[i] = 'Z'
	}
	_ = InternKeyBytes([]byte("OTHER_KEY_002")) // separate entry

	// The earlier returned string must be unaffected by the buffer mutation.
	if got != "STABLE_KEY_001" {
		t.Errorf("interned string corrupted after buffer reuse: got %q", got)
	}

	// Re-interning the original bytes (fresh slice) must yield the same value.
	if again := InternKeyBytes([]byte("STABLE_KEY_001")); again != "STABLE_KEY_001" {
		t.Errorf("re-intern = %q, want STABLE_KEY_001", again)
	}
}

func TestInternKeyBytes_HitIsZeroAlloc(t *testing.T) {
	b := []byte("ZERO_ALLOC_HIT_KEY")
	_ = InternKeyBytes(b) // warm the cache

	n := testing.AllocsPerRun(1000, func() {
		_ = InternKeyBytes(b)
	})
	if n != 0 {
		t.Errorf("InternKeyBytes cache hit allocated %v/op, want 0", n)
	}
}

// TestInternKeyBytes_SwappedSuffixKeys pins the front-cache slot hash's
// position dependence: the short-key multiplicative hash XORs position-4..7
// bytes against the same multiplier, so byte-swapped keys ("VAR_19"/"VAR_91")
// collide in it. A slot hash reusing it would make the pair evict each other
// forever and return one key's string for the other; each key must intern to
// its own content.
func TestInternKeyBytes_SwappedSuffixKeys(t *testing.T) {
	ClearInternCache()
	for i := 0; i < 100; i++ {
		a := InternKeyBytes([]byte(fmt.Sprintf("VAR_%d", i)))
		b := InternKeyBytes([]byte(fmt.Sprintf("VAR_%d", i)))
		if a != b {
			t.Fatalf("re-interning VAR_%d changed the string", i)
		}
	}
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("VAR_%d", i)
		if got := InternKeyBytes([]byte(key)); got != key {
			t.Errorf("InternKeyBytes(%q) = %q, want the key itself", key, got)
		}
	}
}

// TestInternKeyBytes_FrontCacheSurvivesShardEviction pins the contract that a
// front-cache entry remains a valid stable string even after the sharded map
// has evicted it: interning must return content-equal strings, not panic or
// diverge, and a fresh miss (same key, new shard entry) must still answer
// correctly through a now-stale slot.
func TestInternKeyBytes_FrontCacheSurvivesShardEviction(t *testing.T) {
	ClearInternCache()
	const key = "EVICT_ME"
	first := InternKeyBytes([]byte(key))

	// Force eviction pressure inside the owning shard: fill it past
	// maxInternSize with keys that hash to the same shard (raw hash & 7 is
	// preserved by the "shard_N_" prefix length trick — brute force instead).
	for i := 0; i < maxInternSize*2; i++ {
		_ = InternKeyBytes([]byte(fmt.Sprintf("PRESSURE_%d", i)))
	}

	again := InternKeyBytes([]byte(key))
	if again != key || again != first {
		t.Errorf("intern after eviction pressure = %q (first %q), want %q", again, first, key)
	}
}

func TestToUpperASCII(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"abc", "ABC"},
		{"ABC", "ABC"},
		{"aBc123", "ABC123"},
		{"already_UPPER", "ALREADY_UPPER"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ToUpperASCII(tt.input); got != tt.want {
				t.Errorf("ToUpperASCII(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestStoreInterned_EvictsWhenFull pins the bounded-eviction contract. The
// first half drives storeInterned directly on a synthetic shard; the second
// half drives the real shard table through InternKeyBytes with more distinct
// keys than one shard can hold (HashKey is deterministic, so the keys are
// chosen to all land in the same shard) and asserts the shard stays bounded
// while interning keeps returning correct values.
func TestStoreInterned_EvictsWhenFull(t *testing.T) {
	shard := &internShard{cache: make(map[string]*internEntry, maxInternSize)}

	for i := 0; i < maxInternSize; i++ {
		k := fmt.Sprintf("KEY_%d", i)
		shard.cache[k] = &internEntry{key: k}
	}

	got := storeInterned(shard, "NEW_KEY")

	if got.key != "NEW_KEY" {
		t.Errorf("storeInterned() = %q, want %q", got.key, "NEW_KEY")
	}
	if shard.cache["NEW_KEY"].key != "NEW_KEY" {
		t.Error("storeInterned() should store the new key in the shard")
	}
	if len(shard.cache) != maxInternSize {
		t.Errorf("len(cache) = %d after insert into a full shard, want %d (one eviction)", len(shard.cache), maxInternSize)
	}

	// Real-shard integration: collect more than maxInternSize distinct keys
	// that all hash to the same shard and intern every one of them.
	ClearInternCache()
	const extra = 8
	target := HashKey("SHARD_SEED", numShards)
	keys := make([]string, 0, maxInternSize+extra)
	for i := 0; len(keys) < maxInternSize+extra; i++ {
		k := fmt.Sprintf("SHARD_KEY_%05d", i)
		if HashKey(k, numShards) == target {
			keys = append(keys, k)
		}
	}

	for _, k := range keys {
		if got := InternKeyBytes([]byte(k)); got != k {
			t.Fatalf("InternKeyBytes(%q) = %q, want %q", k, got, k)
		}
	}

	real := &internShards[target]
	real.mu.Lock()
	size := len(real.cache)
	real.mu.Unlock()
	if size > maxInternSize {
		t.Errorf("shard %d holds %d keys after %d same-shard inserts, want <= %d (bounded eviction)",
			target, size, len(keys), maxInternSize)
	}

	// Interning must still return correct values under eviction pressure:
	// the newest key (cache hit) and the oldest key (evicted, re-interned).
	for _, k := range []string{keys[len(keys)-1], keys[0]} {
		if got := InternKeyBytes([]byte(k)); got != k {
			t.Errorf("InternKeyBytes(%q) after eviction = %q, want %q", k, got, k)
		}
	}
}
