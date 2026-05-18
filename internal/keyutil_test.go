package internal

import (
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

func TestToUpperASCIISafe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		// Valid ASCII cases
		{"lowercase", "hello", "HELLO", false},
		{"uppercase", "HELLO", "HELLO", false},
		{"mixed", "HeLLo", "HELLO", false},
		{"empty", "", "", false},
		{"with numbers", "abc123", "ABC123", false},
		{"with symbols", "abc-xyz", "ABC-XYZ", false},

		// Non-ASCII cases (should error)
		{"unicode letter", "héllo", "", true},
		{"unicode at start", "éabc", "", true},
		{"unicode at end", "abcé", "", true},
		{"unicode in middle", "abéc", "", true},
		{"chinese", "你好", "", true},
		{"emoji", "test🔥", "", true},
		{"mixed unicode", "test世界", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ToUpperASCIISafe(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ToUpperASCIISafe(%q) expected error, got nil", tt.input)
				}
				if err != ErrNonASCII {
					t.Errorf("ToUpperASCIISafe(%q) error = %v, want ErrNonASCII", tt.input, err)
				}
			} else {
				if err != nil {
					t.Errorf("ToUpperASCIISafe(%q) unexpected error: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("ToUpperASCIISafe(%q) = %q, want %q", tt.input, result, tt.expected)
				}
			}
		})
	}
}

func TestIsASCII(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty", "", true},
		{"simple", "hello", true},
		{"with symbols", "hello-world_123!", true},
		{"all printable ASCII", strings.Repeat("a", 128), true},
		{"unicode", "héllo", false},
		{"chinese", "你好", false},
		{"emoji", "🔥", false},
		{"mixed", "hello世界", false},
		{"byte 128", string([]byte{128}), false},
		{"byte 255", string([]byte{255}), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsASCII(tt.input)
			if result != tt.expected {
				t.Errorf("IsASCII(%q) = %v, want %v", tt.input, result, tt.expected)
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

func TestInternKey_EdgeCases(t *testing.T) {
	ClearInternCache()

	tests := []struct {
		name  string
		key   string
		want  string
	}{
		{"empty string not interned", "", ""},
		{"simple key", "SIMPLE", "SIMPLE"},
		{"key with underscores", "MY_KEY_NAME", "MY_KEY_NAME"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InternKey(tt.key)
			if got != tt.want {
				t.Errorf("InternKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestInternKey_Eviction(t *testing.T) {
	ClearInternCache()

	// Fill the cache to trigger eviction
	for i := 0; i < maxInternSize*2; i++ {
		key := "EVICT_KEY_" + strings.Repeat("x", i%20+1)
		InternKey(key)
	}

	// Verify cache still works after eviction
	key := "POST_EVICT_KEY"
	if got := InternKey(key); got != key {
		t.Errorf("InternKey after eviction = %q, want %q", got, key)
	}
}

func TestInternKeyConsistency(t *testing.T) {
	// Clear cache before test
	ClearInternCache()

	// Test that cache and order slice remain consistent
	keys := make([]string, 0, maxInternSize+10)

	// Fill cache beyond capacity to trigger eviction
	for i := 0; i < maxInternSize+50; i++ {
		key := "KEY_" + strings.Repeat("A", i%10)
		keys = append(keys, key)
		interned := InternKey(key)
		if interned != key {
			t.Errorf("InternKey(%q) = %q, want %q", key, interned, key)
		}
	}

	// Verify we can still intern new keys after eviction
	newKey := "NEW_TEST_KEY"
	interned := InternKey(newKey)
	if interned != newKey {
		t.Errorf("InternKey(%q) = %q, want %q", newKey, interned, newKey)
	}
}
