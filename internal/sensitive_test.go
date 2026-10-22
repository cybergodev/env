package internal

import (
	"strings"
	"testing"
)

func TestIsKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		// Boundary: the empty key is never sensitive.
		{"", false},

		// Sensitive keys
		{"PASSWORD", true},
		{"password", true},
		{"DB_PASSWORD", true},
		{"API_KEY", true},
		{"SECRET_TOKEN", true},
		{"PRIVATE_KEY", true},
		{"ACCESS_KEY", true},
		{"SECRET_KEY", true},
		{"CREDENTIAL", true},
		{"AUTH_TOKEN", true},
		{"SESSION_ID", true},
		{"COOKIE_SECRET", true},
		{"PASSPHRASE", true},

		// SECURITY: DATABASE_URL is now considered sensitive because
		// it typically contains credentials (e.g., postgresql://user:password@host/db)
		{"DATABASE_URL", true},
		{"CONNECTION_STRING", true},

		// Non-ASCII boundaries: containsIgnoreCase must fall back to its
		// Unicode-aware path (a window byte >= 0x80) and stay correct both
		// when the pattern misses ("PÄSSWORD" is not PASSWORD) and when it
		// hits after the non-ASCII byte.
		{"PÄSSWORD", false},
		{"Ω_API_KEY", true},
		// containsWordIgnoreCase hits the same >= 0x80 window break and
		// must not match PAN through the Ä.
		{"PÄN", false},

		// Non-sensitive keys
		{"HOSTNAME", false},
		{"PORT", false},
		{"DEBUG", false},
		{"LOG_LEVEL", false},
		{"SERVER_NAME", false},
		{"TIMEOUT", false},

		// SEC-006 word-anchoring: ordinary words that embed the short patterns
		// AUTH / PAN / PWD must NOT be misclassified as sensitive.
		{"AUTHOR", false},
		{"AUTHORITY", false},
		{"COMPANY", false},
		{"PANCAKE", false},
		{"EXPAND", false},
		{"JAPAN", false},
		{"SPANISH", false},
		{"MONKEY", false},
		{"KEYBOARD", false},

		// SEC-006 word-anchoring: the same short patterns still match at
		// identifier boundaries (surrounded by '_' or end-of-key).
		{"DB_AUTH", true},
		{"MYSQL_PWD", true},
		{"DB_PWD", true},
		{"PWD", true},
		{"PAN_NUMBER", true},
		{"AUTH", true},

		// SEC-006 additions: password/PII spellings PASSWORD lacked.
		{"USERNAME", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := IsKey(tt.key)
			if result != tt.expected {
				t.Errorf("IsKey(%q) = %v, want %v", tt.key, result, tt.expected)
			}
		})
	}
}

func TestMaskValue(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      string
		contains   string
		notContain string
	}{
		{
			name:       "sensitive value masked",
			key:        "PASSWORD",
			value:      "secret123",
			contains:   "MASKED",
			notContain: "secret123",
		},
		{
			name:       "short non-sensitive value unchanged",
			key:        "PORT",
			value:      "8080",
			contains:   "8080",
			notContain: "",
		},
		{
			name:       "long non-sensitive value truncated",
			key:        "HOSTNAME",
			value:      "verylonghostnamethatneedstobetruncated.example.com",
			contains:   "...",
			notContain: "example.com",
		},
		{
			// SEC-04 parity with MaskInString: truncation alone let
			// key=value secrets through unmasked in non-sensitive values.
			name:       "non-sensitive value with embedded secret sanitized",
			key:        "NOTE",
			value:      "note password=hunter2 and more",
			contains:   "note",
			notContain: "hunter2",
		},
		{
			name:       "short non-sensitive value with token pattern sanitized",
			key:        "COMMENT",
			value:      "token=abc123",
			contains:   "[MASKED]",
			notContain: "abc123",
		},
		{
			// SEC-04 follow-up: a secret carried INSIDE a value under a
			// non-sensitive key name still gets its exposed prefix scrubbed
			// via the pwd= pattern.
			name:       "non-sensitive value with connection-string password sanitized",
			key:        "DB_CONN",
			value:      "Server=db;Pwd=hunter2;Extra=1",
			contains:   "[MASKED]",
			notContain: "hunter2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskValue(tt.key, tt.value)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("MaskValue() = %q, want to contain %q", result, tt.contains)
			}
			if tt.notContain != "" && strings.Contains(result, tt.notContain) {
				t.Errorf("MaskValue() = %q, should not contain %q", result, tt.notContain)
			}
		})
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"AB", "[MASKED]"},
		{"ABC", "[MASKED]"},
		{"ABCD", "[MASKED]"},
		{"API_KEY", "[MASKED]"},
		{"", "[MASKED]"},
		{"DATABASE_PASSWORD", "[MASKED]"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := MaskKey(tt.key)
			if result != tt.expected {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, result, tt.expected)
			}
		})
	}
}

func TestSanitizeForLog(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		notContain string
	}{
		{
			name:       "password masked",
			input:      "password=secret123",
			notContain: "secret123",
		},
		{
			name:       "token masked",
			input:      "token=abc123xyz",
			notContain: "abc123xyz",
		},
		{
			name:       "api_key masked",
			input:      "api_key=mysecretkey",
			notContain: "mysecretkey",
		},
		// PIN: the "=" forms are DERIVED from Patterns — these spellings had
		// no entry in the former hand-maintained list and leaked verbatim.
		{
			name:       "encryption_key equals form masked",
			input:      "encryption_key=hunter2",
			notContain: "hunter2",
		},
		{
			name:       "sign_key equals form masked",
			input:      "sign_key=abcd1234",
			notContain: "abcd1234",
		},
		{
			name:       "conn_string equals form masked",
			input:      "conn_string=Server=db;Pwd=hunter2",
			notContain: "hunter2",
		},
		{
			name:       "service_account equals form masked",
			input:      "service_account=svc-prod-9f2",
			notContain: "svc-prod-9f2",
		},
		{
			name:       "encryption_key colon form masked",
			input:      "encryption_key: hunter2",
			notContain: "hunter2",
		},
		{
			name:       "control chars removed",
			input:      "value\x00with\x01nulls",
			notContain: "\x00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeForLog(tt.input)
			if strings.Contains(result, tt.notContain) {
				t.Errorf("SanitizeForLog() = %q, should not contain %q", result, tt.notContain)
			}
		})
	}
}

func TestPatternsList(t *testing.T) {
	// Verify Patterns list is not empty
	if len(Patterns) == 0 {
		t.Error("Patterns list should not be empty")
	}

	// Verify each pattern is uppercase
	for _, pattern := range Patterns {
		if pattern != strings.ToUpper(pattern) {
			t.Errorf("Pattern %q should be uppercase", pattern)
		}
	}
}

// TestSanitizeForLog_NonASCIIAlignment verifies pattern indices stay aligned
// with the original string when it contains non-ASCII characters whose
// lowercased form has a different byte length. Regression test: matching ran
// on a strings.ToLower copy, and characters like U+023A (2 bytes -> 3 bytes
// lowercased) shifted the computed index past len(s), silently skipping the
// replacement and leaking the secret.
func TestSanitizeForLog_NonASCIIAlignment(t *testing.T) {
	// U+023A (LATIN CAPITAL LETTER A WITH STROKE) grows under ToLower.
	expand := strings.Repeat("Ⱥ", 20) + "password=supersecret tail"
	if out := SanitizeForLog(expand); strings.Contains(out, "supersecret") {
		t.Errorf("SanitizeForLog() leaked secret with length-expanding runes: %q", out)
	}

	// U+0130 (LATIN CAPITAL LETTER I WITH DOT ABOVE) shrinks under ToLower.
	shrink := strings.Repeat("İ", 20) + "token=abc123xyz more"
	if out := SanitizeForLog(shrink); strings.Contains(out, "abc123xyz") {
		t.Errorf("SanitizeForLog() leaked secret with length-shrinking runes: %q", out)
	}

	// Pure ASCII behavior is unchanged.
	if out := SanitizeForLog("user=bob Password=hunter2"); !strings.Contains(out, "[MASKED]") || strings.Contains(out, "hunter2") {
		t.Errorf("SanitizeForLog() = %q, want masked password", out)
	}
}

// TestSanitizeForLog_ColonAndBearerForms pins the GEN-001 mask extension:
// colon-separated pairs (YAML/plain text), the authorization=/pwd= equals
// forms, and the "Bearer <token>" scheme are masked with the same key list
// as the shell-style key=value form. Colon forms skip horizontal whitespace
// before the value so canonical YAML ("password: hunter2") masks too.
func TestSanitizeForLog_ColonAndBearerForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"yaml pair with space", "db:\n  password: hunter2\n", "db:\n  password: [MASKED]\n"},
		{"yaml pair indented with tab", "password:\thunter2", "password:\t[MASKED]"},
		{"compact colon pair", "token:abc123", "token:[MASKED]"},
		{"api key colon form", "api_key: sk-1234567890", "api_key: [MASKED]"},
		{"authorization equals form masks scheme", "authorization=Bearer", "authorization=[MASKED]"},
		{"pwd in connection string", "Server=db;Pwd=hunter2", "Server=db;Pwd=[MASKED]"},
		// The value-boundary extension (GEN-001) masks the whole remainder
		// as one region — "Bearer" and the token alike — instead of two
		// adjacent masks split at the space.
		{"bearer scheme masks the token", "Authorization: Bearer eyJhbGciOi", "Authorization: [MASKED]"},
		{"non-sensitive colon pairs untouched", "host: localhost:5432", "host: localhost:5432"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeForLog(tt.input); got != tt.want {
				t.Errorf("SanitizeForLog(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestSanitizeForLog_MultiWordValues pins the GEN-001 value-boundary fix:
// a secret containing spaces must be masked in full, quoted values mask
// through the closing quote, and the next key=value pair still starts a new
// region instead of being swallowed by the mask.
func TestSanitizeForLog_MultiWordValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"unquoted multi-word secret fully masked",
			"password=my secret phrase", "password=[MASKED]"},
		{"double-quoted value masks through closing quote",
			`password="my secret phrase"`, "password=[MASKED]"},
		{"single-quoted value masks through closing quote",
			"password='two words'", "password=[MASKED]"},
		{"next pair starts a new region",
			"password=my secret token=abc", "password=[MASKED] token=[MASKED]"},
		{"colon pair after multi-word value",
			"password=my secret host: localhost", "password=[MASKED] host: localhost"},
		{"interior equals stay inside the mask",
			"password=a=b", "password=[MASKED]"},
		{"url with colon inside value stays masked",
			"secret=http://example.com path", "secret=[MASKED]"},
		{"yaml colon form with multi-word value",
			"apikey: three word key", "apikey: [MASKED]"},
		{"unterminated quote masks to end of line",
			"password='never closed\nnext line", "password=[MASKED]\nnext line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeForLog(tt.input); got != tt.want {
				t.Errorf("SanitizeForLog(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestIndexFoldASCII pins the direct contract of the ASCII folding index:
// offsets refer to the original string, a negative start offset is clamped
// to 0, and non-ASCII bytes never match.
func TestIndexFoldASCII(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		pattern string
		from    int
		want    int
	}{
		{"match at start", "abc", "a", 0, 0},
		{"match after offset", "abcbd", "b", 2, 3},
		{"case folds", "aBc", "abc", 0, 0},
		{"negative from is clamped to zero", "abc", "b", -1, 1},
		{"no match", "abc", "d", 0, -1},
		{"non-ascii bytes skipped, later ascii still matches", "Ωbc", "bc", 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := indexFoldASCII(tt.s, tt.pattern, tt.from); got != tt.want {
				t.Errorf("indexFoldASCII(%q, %q, %d) = %d, want %d", tt.s, tt.pattern, tt.from, got, tt.want)
			}
		})
	}
}
