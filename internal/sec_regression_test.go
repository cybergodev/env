package internal

// Regression tests for the SEC-001 security audit fixes (2026-09-06).
// Each test pins one finding so a future refactor cannot silently
// reintroduce it. Root-package counterparts live in
// sec_regression_test.go at the module root.

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestSEC01_StructIntoDoesNotLeakValue verifies that struct-field conversion
// failures report the field and reason without echoing the offending value:
// raw strconv/time errors embed the input (e.g. `parsing "<secret>"`), which
// reached callers of ParseInto/UnmarshalInto/UnmarshalStruct.
func TestSEC01_StructIntoDoesNotLeakValue(t *testing.T) {
	const secret = "s3cr3t-PIN-9876"
	tests := []struct {
		name  string
		field any
	}{
		{"int", &struct {
			F int `env:"K"`
		}{}},
		{"uint", &struct {
			F uint `env:"K"`
		}{}},
		{"float", &struct {
			F float64 `env:"K"`
		}{}},
		{"bool", &struct {
			F bool `env:"K"`
		}{}},
		{"duration", &struct {
			F time.Duration `env:"K"`
		}{}},
		{"int_slice", &struct {
			F []int `env:"K"`
		}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := StructInto(map[string]string{"K": secret}, reflect.ValueOf(tt.field).Elem(), "")
			if err == nil {
				t.Fatal("expected conversion error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error message leaks raw value: %v", err)
			}
			var me *MarshalError
			if !errors.As(err, &me) {
				t.Errorf("want *MarshalError, got %T: %v", err, err)
			}
		})
	}

	// Out-of-range keeps the failure reason but still drops the value.
	err := StructInto(map[string]string{"K": "300"}, reflect.ValueOf(&struct {
		F int8 `env:"K"`
	}{}).Elem(), "")
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("range reason lost: %v", err)
	}
	if strings.Contains(err.Error(), "300") {
		t.Errorf("range error echoes the value: %v", err)
	}
}

// TestSEC02_YAMLTokenCap verifies Tokenize fails fast at HardMaxYAMLTokens
// instead of materializing an unbounded token slice (~56 bytes per token —
// a 2 MB file of "a\n" lines previously allocated ~640 MB before any
// MaxVariables check could run).
func TestSEC02_YAMLTokenCap(t *testing.T) {
	// "a\n" yields 2 tokens per 2 bytes; HardMaxYAMLTokens/2+1 lines cross the cap.
	input := strings.Repeat("a\n", HardMaxYAMLTokens/2+1)
	lexer := newYAMLLexer([]byte(input))
	_, err := lexer.tokenizeInto(nil)
	lexer.release()
	if err == nil {
		t.Fatal("expected token cap error")
	}
	var ye *YAMLError
	if !errors.As(err, &ye) {
		t.Fatalf("want *YAMLError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "token count") {
		t.Errorf("error should mention token count: %v", err)
	}
}

// TestSEC04_MaskInStringSanitizesPatterns verifies MaskInString masks
// key=value secret pairs in addition to truncating long input. The
// truncation-boundary rows were ported from the removed TestMaskInString
// (sensitive_test.go) with exact expectations.
func TestSEC04_MaskInStringSanitizesPatterns(t *testing.T) {
	tests := []struct{ input, want string }{
		{"password=hunter2", "password=[MASKED]"},
		{"api_key=sk-123", "api_key=[MASKED]"},
		{"plain value", "plain value"},
		// Exactly maxLen (50) characters is unchanged...
		{strings.Repeat("b", 50), strings.Repeat("b", 50)},
		// ...while anything longer is cut to 47 characters plus "...".
		{strings.Repeat("a", 100), strings.Repeat("a", 47) + "..."},
	}
	for _, tt := range tests {
		if got := MaskInString(tt.input); got != tt.want {
			t.Errorf("MaskInString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestSEC05_RequiredMessageNotEchoed verifies the file-supplied ${VAR:?msg}
// text is not copied into the ExpansionError (Chain field or Error text).
func TestSEC05_RequiredMessageNotEchoed(t *testing.T) {
	e := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Lookup:   func(string) (string, bool) { return "", false },
		Mode:     ModeAll,
	})
	_, err := e.Expand("${MISSING_VAR:?leak-me}")
	if err == nil {
		t.Fatal("expected ExpansionError")
	}
	var ee *ExpansionError
	if !errors.As(err, &ee) {
		t.Fatalf("want *ExpansionError, got %T: %v", err, err)
	}
	if ee.Kind != ExpansionRequiredKind {
		t.Errorf("kind = %v, want ExpansionRequiredKind", ee.Kind)
	}
	if strings.Contains(ee.Chain, "leak-me") || strings.Contains(err.Error(), "leak-me") {
		t.Errorf("file-supplied :? message leaked: chain=%q err=%v", ee.Chain, err)
	}
}

// TestSEC06_MarshalRejectsUnsafeKeys verifies both emitters refuse keys
// that would change tokenization when the output is re-parsed, instead of
// emitting corrupted or injectable documents. Keys are never escaped — the
// parsers do not support quoted keys — so rejection is the only safe
// behavior.
func TestSEC06_MarshalRejectsUnsafeKeys(t *testing.T) {
	tests := []struct {
		name   string
		m      map[string]string
		format MarshalFormat
	}{
		{"env key with newline", map[string]string{"A\nB=INJECTED": "1"}, FormatEnv},
		{"env key with =", map[string]string{"A=B": "1"}, FormatEnv},
		{"env key with :", map[string]string{"A:B": "1"}, FormatEnv},
		{"env comment key", map[string]string{"#A": "1"}, FormatEnv},
		{"env export-prefixed key", map[string]string{"export X": "1"}, FormatEnv},
		{"env empty key", map[string]string{"": "1"}, FormatEnv},
		{"env padded key", map[string]string{" A": "1"}, FormatEnv},
		{"yaml key with newline", map[string]string{"A\nB": "1"}, FormatYAML},
		{"yaml key colon+space", map[string]string{"A: B": "1"}, FormatYAML},
		{"yaml key trailing colon", map[string]string{"A:": "1"}, FormatYAML},
		{"yaml comment key", map[string]string{"A #c": "1"}, FormatYAML},
		{"yaml dash-prefixed key", map[string]string{"- A": "1"}, FormatYAML},
		{"json key with space", map[string]string{"A B": "1"}, FormatJSON},
		{"json key with =", map[string]string{"A=B": "1"}, FormatJSON},
		{"json empty key", map[string]string{"": "1"}, FormatJSON},
		{"json bracket key", map[string]string{"A[0]": "1"}, FormatJSON},
		{"json pure-digit key", map[string]string{"123": "1"}, FormatJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MarshalEnvAs(tt.m, tt.format, true)
			if err == nil {
				t.Fatalf("MarshalEnvAs succeeded, want error; output: %q", out)
			}
			var me *MarshalError
			if !errors.As(err, &me) || me.Field != "key" {
				t.Errorf("want MarshalError{Field: key}, got %T: %v", err, err)
			}
		})
	}

	// Keys that survive a re-parse are still accepted in both formats.
	safe := map[string]string{"GOOD_KEY": "v", "cache.hosts.0": "h", "A#B": "x"}
	for _, format := range []MarshalFormat{FormatEnv, FormatYAML} {
		if _, err := MarshalEnvAs(safe, format, true); err != nil {
			t.Errorf("MarshalEnvAs(%v) rejected safe keys: %v", format, err)
		}
	}
	// JSON accepts the loader's own key space (IsValidJSONKey). "A#B" is
	// env/YAML-safe but NOT loader-loadable from JSON, so it is excluded.
	jsonSafe := map[string]string{"GOOD_KEY": "v", "cache.hosts.0": "h", "user@example": "u"}
	if _, err := MarshalEnvAs(jsonSafe, FormatJSON, true); err != nil {
		t.Errorf("MarshalEnvAs(FormatJSON) rejected safe keys: %v", err)
	}
}

// TestSEC003_HashKeyNonPositiveShards pins the defensive total-function
// contract for HashKey: a non-positive shard count returns 0 instead of
// panicking on the modulo path. All production callers pass power-of-two
// constants (8, 32), so this is a latent-input guard, but the function is
// widely reachable and must never be a panic primitive.
func TestSEC003_HashKeyNonPositiveShards(t *testing.T) {
	for _, shards := range []int{0, -1, -32} {
		if got := HashKey("KEY", shards); got != 0 {
			t.Errorf("HashKey(%q, %d) = %d, want 0", "KEY", shards, got)
		}
	}
	// Sanity: valid shard counts keep working in both hash branches.
	for _, shards := range []int{8, 32} {
		if got := HashKey("KEY", shards); int(got) >= shards {
			t.Errorf("HashKey(%q, %d) = %d, out of range", "KEY", shards, got)
		}
	}
	if got := HashKey("a.b.c", 7); int(got) >= 7 { // non-power-of-two modulo path
		t.Errorf("HashKey(%q, 7) = %d, out of range", "a.b.c", got)
	}
}
