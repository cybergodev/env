package internal

import (
	"errors"
	"strings"
	"testing"
)

// TestMarshalEnv_ValueEscaping pins escapeValueToBuilder's quoting rules via
// the public MarshalEnv path (single-key map; each output line is "K=<escaped>\n").
func TestMarshalEnv_ValueEscaping(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple value", "simple", "simple"},
		{"value with space", "value with space", `"value with space"`},
		{"value with newline", "line1\nline2", `"line1\nline2"`},
		{"value with carriage return", "a\rb", `"a\rb"`},
		{"value with tab", "col1\tcol2", `"col1\tcol2"`},
		{"value with quote", `say "hello"`, `"say \"hello\""`},
		{"value with hash", "value#comment", `"value#comment"`},
		// BOUNDARY: a single quote triggers quoting but needs no escaping —
		// only the double quote is special inside the double-quoted grammar.
		{"value with single quote", "it's", `"it's"`},
		// A lone backslash neither triggers quoting nor is escaped.
		{"value with backslash only", `path\to\file`, `path\to\file`},
		// BOUNDARY: once quoting is on (here: by the space), a backslash IS
		// escaped, so the value re-parses to the same bytes.
		{"backslash escaped when quoted", `path\to file`, `"path\\to file"`},
		{"empty value", "", `""`},
		// BOUNDARY: values the YAML reader coerces to "" on reload ("null",
		// "~") are quoted even though they contain no special character.
		{"yaml null coerces to quoted", "null", `"null"`},
		{"yaml tilde coerces to quoted", "~", `"~"`},
		// SEC-009: dollar references are quoted and escaped so a Marshal→Load
		// round trip does not re-expand them.
		{"value with dollar ref", "cost $5", `"cost \$5"`},
		{"value with braced ref", "${HOME}/app", `"\${HOME}/app"`},
		{"bare dollar", "$", `"\$"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MarshalEnv(map[string]string{"K": tt.input}, false)
			if err != nil {
				t.Errorf("MarshalEnv() error = %v", err)
				return
			}
			if want := "K=" + tt.expected + "\n"; result != want {
				t.Errorf("MarshalEnv() = %q, want %q", result, want)
			}
		})
	}
}

func TestMarshalEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		sorted   bool
		contains []string
	}{
		{
			name: "simple map",
			input: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			sorted:   false,
			contains: []string{"KEY1=value1", "KEY2=value2"},
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			sorted:   false,
			contains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MarshalEnv(tt.input, tt.sorted)
			if err != nil {
				t.Errorf("MarshalEnv() error = %v", err)
				return
			}
			for _, c := range tt.contains {
				if !strings.Contains(result, c) {
					t.Errorf("MarshalEnv() should contain %q, got %q", c, result)
				}
			}
		})
	}
}

// TestMarshalEnv_KeyValidation pins the SEC-06 key-validation arms of
// validateEnvKeyForMarshal through the public MarshalEnv path (previously
// never triggered by any test): keys that would change tokenization on a
// re-parse fail the marshal before any output is produced.
func TestMarshalEnv_KeyValidation(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"empty key", "", true},
		{"key with equals sign", "A=B", true},
		{"key with colon", "A:B", true},
		{"key with control character", "BAD\x00KEY", true},
		{"key starts with hash", "#COMMENT", true},
		{"export prefix", "export PATH", true},
		{"leading space", " KEY", true},
		{"trailing space", "KEY ", true},
		// The reader's own grammar is the final gate: a key the .env loader
		// rejects must not be emitted, or the writer produces files this
		// library refuses to load (SEC-06). Interior spaces, dots, hyphens
		// and '@' all fail ^[A-Za-z][A-Za-z0-9_]*$ on reload.
		{"interior space", "BAD KEY", true},
		{"dotted key", "apnews.com", true},
		{"hyphenated key", "api-key", true},
		{"at-sign key", "user@host", true},
		{"leading digit", "1KEY", true},
		{"plain key", "GOOD_KEY", false},
		{"key with digits", "KEY_2", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarshalEnv(map[string]string{tt.key: "v"}, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MarshalEnv(key=%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
			var marshalErr *MarshalError
			if tt.wantErr && !errors.As(err, &marshalErr) {
				t.Errorf("MarshalEnv(key=%q) error = %T, want *MarshalError", tt.key, err)
			}
		})
	}
}

// TestMarshalEnv_NilMap pins the nil-map contract: no keys, no error, empty
// output.
func TestMarshalEnv_NilMap(t *testing.T) {
	result, err := MarshalEnv(nil, false)
	if err != nil {
		t.Fatalf("MarshalEnv(nil) error = %v, want nil", err)
	}
	if result != "" {
		t.Errorf("MarshalEnv(nil) = %q, want empty string", result)
	}
}

// TestMarshalSorted pins the sorted=true key ordering for the .env and YAML
// writers through one table (both marshal funcs share the
// func(map[string]string, bool) (string, error) shape; the JSON encoder
// sorts keys itself).
func TestMarshalSorted(t *testing.T) {
	tests := []struct {
		name    string
		marshal func(map[string]string, bool) (string, error)
	}{
		{"MarshalEnv", MarshalEnv},
		{"marshalToYAML", marshalToYAML},
	}

	input := map[string]string{
		"Z_KEY": "z",
		"A_KEY": "a",
		"M_KEY": "m",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.marshal(input, true)
			if err != nil {
				t.Fatalf("%s(sorted) error = %v", tt.name, err)
			}

			// Verify order: A_KEY should appear before M_KEY before Z_KEY.
			aIdx := strings.Index(result, "A_KEY")
			mIdx := strings.Index(result, "M_KEY")
			zIdx := strings.Index(result, "Z_KEY")
			if aIdx < 0 || mIdx < 0 || zIdx < 0 {
				t.Fatalf("output missing expected keys: %q", result)
			}
			if aIdx >= mIdx || mIdx >= zIdx {
				t.Errorf("keys not in sorted order: A=%d, M=%d, Z=%d", aIdx, mIdx, zIdx)
			}
		})
	}
}

// ============================================================================
// MarshalEnvAs Tests (Multi-format Marshal)
// ============================================================================

func TestMarshalEnvAs(t *testing.T) {
	input := map[string]string{
		"APP_NAME": "myapp",
		"APP_PORT": "8080",
		"DEBUG":    "true",
	}

	t.Run("dotenv format", func(t *testing.T) {
		result, err := MarshalEnvAs(input, FormatEnv, false)
		if err != nil {
			t.Errorf("MarshalEnvAs() error = %v", err)
			return
		}
		if !strings.Contains(result, "APP_NAME=myapp") {
			t.Errorf("expected .env format, got: %s", result)
		}
	})

	t.Run("json format", func(t *testing.T) {
		result, err := MarshalEnvAs(input, FormatJSON, false)
		if err != nil {
			t.Errorf("MarshalEnvAs() error = %v", err)
			return
		}
		if !strings.Contains(result, `"APP"`) || !strings.Contains(result, `"NAME"`) {
			t.Errorf("expected JSON format with nested structure, got: %s", result)
		}
	})

	t.Run("yaml format", func(t *testing.T) {
		result, err := MarshalEnvAs(input, FormatYAML, false)
		if err != nil {
			t.Errorf("MarshalEnvAs() error = %v", err)
			return
		}
		if !strings.Contains(result, "APP_NAME: myapp") {
			t.Errorf("expected YAML format, got: %s", result)
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := MarshalEnvAs(input, MarshalFormat(99), false)
		if err == nil {
			t.Error("expected error for invalid format")
		}
	})
}

// ============================================================================
// JSON Marshal Tests
// ============================================================================

func TestMarshalToJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		sorted   bool
		contains []string
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			sorted:   false,
			contains: []string{"{}"},
		},
		{
			name: "simple flat map",
			input: map[string]string{
				"NAME": "test",
				"PORT": "8080",
			},
			sorted:   false,
			contains: []string{`"NAME"`, `"test"`, `"PORT"`, `8080`},
		},
		{
			name: "nested keys",
			input: map[string]string{
				"DB_HOST": "localhost",
				"DB_PORT": "5432",
			},
			sorted:   false,
			contains: []string{`"DB"`, `"HOST"`, `"localhost"`},
		},
		{
			name: "boolean values",
			input: map[string]string{
				"DEBUG":   "true",
				"VERBOSE": "false",
			},
			sorted:   false,
			contains: []string{`"DEBUG"`, `true`, `"VERBOSE"`, `false`},
		},
		{
			name: "numeric values",
			input: map[string]string{
				"COUNT": "42",
				"RATIO": "3.14",
			},
			sorted:   false,
			contains: []string{`"COUNT"`, `42`, `"RATIO"`, `3.14`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalToJSON(tt.input, tt.sorted)
			if err != nil {
				t.Errorf("marshalToJSON() error = %v", err)
				return
			}
			for _, c := range tt.contains {
				if !strings.Contains(string(result), c) {
					t.Errorf("marshalToJSON() should contain %q, got %s", c, result)
				}
			}
		})
	}
}

func TestInferJSONType(t *testing.T) {
	tests := []struct {
		input    string
		expected interface{}
	}{
		{"true", true},
		{"false", false},
		// "null" is NOT typed: JSON null reloads as "" (NullAsEmpty), so
		// converting would silently rewrite the value on a Marshal→load
		// cycle. It stays a string, like non-canonical number spellings.
		{"null", "null"},
		{"42", int64(42)},
		{"3.14", float64(3.14)},
		{"hello", "hello"},
		{"", ""},
		// Non-canonical spellings round-trip as strings (M-6): typing them
		// re-rendered "007" as 7 and "1.50" as 1.5 — silent mutation.
		{"007", "007"},
		{"1.50", "1.50"},
		{"1e3", "1e3"},
		{"+5", "+5"},
		{"-0", "-0"},
		// BOUNDARY: 2^64 overflows ParseInt, but its shortest float64
		// rendering is 18446744073709552000 — not the input spelling — so
		// the canonical-form rule keeps it a string (no silent mutation).
		{"18446744073709551616", "18446744073709551616"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := inferJSONType(tt.input)
			if result != tt.expected {
				t.Errorf("inferJSONType(%q) = %v (%T), want %v (%T)",
					tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}

// TestMarshalToYAML_KeyValidation pins the SEC-06 arms of
// validateYAMLKeyForMarshal through the public marshalToYAML path — the YAML
// twin of TestMarshalEnv_KeyValidation: keys that would change tokenization
// on a re-parse (comment-starting '#', a ':' that terminates the key token,
// array-item prefixes, edge whitespace) fail the marshal before any output
// is produced.
func TestMarshalToYAML_KeyValidation(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"empty key", "", true},
		{"key starts with hash", "#COMMENT", true},
		{"hash after space", "A #B", true},
		{"colon at end of key", "A:", true},
		{"colon followed by tab", "A:\tB", true},
		{"array-item prefix", "- ITEM", true},
		{"leading space", " KEY", true},
		{"trailing space", "KEY ", true},
		// Syntactically plain, but the loader's structured key grammar
		// (IsValidJSONKey) rejects them on re-parse — so they must not be
		// emitted either (SEC-06: writer and reader agree on the key space).
		{"interior colon", "A:B", true},
		{"interior hash", "A#B", true},
		{"space inside key", "A B", true},
		{"plus sign", "A+B", true},
		// Loader-legal keys pass.
		{"plain key", "GOOD_KEY", false},
		{"dotted key", "cache.hosts.0", false},
		{"hyphenated key", "api-key", false},
		{"at-sign key", "user@example", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := marshalToYAML(map[string]string{tt.key: "v"}, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("marshalToYAML(key=%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
			var marshalErr *MarshalError
			if tt.wantErr && !errors.As(err, &marshalErr) {
				t.Errorf("marshalToYAML(key=%q) error = %T, want *MarshalError", tt.key, err)
			}
		})
	}
}

// TestSetNestedValue_UnsortedConflictKeepsData pins the bidirectional
// conflict hardening: whichever order unsorted keys arrive in, neither the
// scalar nor the nested subtree is lost. (The public Marshal paths always
// sort, where the scalar prefix key lands first and the longer key falls
// back to the flat form — this exercises the hardened unsorted path.)
func TestSetNestedValue_UnsortedConflictKeepsData(t *testing.T) {
	for _, order := range [][2]string{{"A_B_C", "A_B"}, {"A_B", "A_B_C"}} {
		result := make(map[string]interface{})
		values := map[string]string{"A_B": "scalar", "A_B_C": "deep"}
		for _, k := range order {
			setNestedValue(result, k, values[k])
		}

		scalar := ""
		if outer, ok := result["A"].(map[string]interface{}); ok {
			if v, ok := outer["B"].(string); ok {
				scalar = v
			}
		}
		if scalar == "" {
			if v, ok := result["A_B"].(string); ok {
				scalar = v
			}
		}
		if scalar != "scalar" {
			t.Errorf("order %v: scalar lost (result=%v)", order, result)
		}

		deep := ""
		if outer, ok := result["A"].(map[string]interface{}); ok {
			if mid, ok := outer["B"].(map[string]interface{}); ok {
				if v, ok := mid["C"].(string); ok {
					deep = v
				}
			}
		}
		if deep == "" {
			if v, ok := result["A_B_C"].(string); ok {
				deep = v
			}
		}
		if deep != "deep" {
			t.Errorf("order %v: deep value lost (result=%v)", order, result)
		}
	}
}

// TestMarshalJSONRoundTripPreservesValues pins the M-6 rule end to end: a
// value is typed as JSON number/bool only when the canonical re-rendering
// equals the input string, so Marshal→FlattenJSON returns every value
// unchanged. Previously "007"→7, "1.50"→1.5 and "null"→null silently
// mutated the values on reload.
func TestMarshalJSONRoundTripPreservesValues(t *testing.T) {
	m := map[string]string{
		"VER":   "1.50",
		"PAD":   "007",
		"LABEL": "null",
		"N":     "42",
		"F":     "0.1",
		"B":     "true",
	}
	out, err := MarshalEnvAs(m, FormatJSON, true)
	if err != nil {
		t.Fatalf("MarshalEnvAs error = %v", err)
	}
	back, err := FlattenJSON([]byte(out), defaultJSONCfg())
	if err != nil {
		t.Fatalf("FlattenJSON error = %v", err)
	}
	for k, v := range m {
		if back[k] != v {
			t.Errorf("round-trip[%q] = %q, want %q", k, back[k], v)
		}
	}
}

// ============================================================================
// YAML Marshal Tests
// ============================================================================

func TestMarshalToYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		sorted   bool
		contains []string
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			sorted:   false,
			contains: []string{},
		},
		{
			name: "simple map",
			input: map[string]string{
				"NAME": "test",
				"PORT": "8080",
			},
			sorted: false,
			// Numeric strings are quoted so the value round-trips as a string
			// (an unquoted 8080 would still parse back to "8080", but bool/null
			// scalars would be coerced — the quoting rule is uniform).
			contains: []string{"NAME: test", `PORT: "8080"`},
		},
		{
			name: "sorted output",
			input: map[string]string{
				"Z_KEY": "z",
				"A_KEY": "a",
			},
			sorted:   true,
			contains: []string{"A_KEY: a", "Z_KEY: z"},
		},
		{
			name: "value with special chars",
			input: map[string]string{
				"MESSAGE": "hello: world",
			},
			sorted:   false,
			contains: []string{`MESSAGE: "hello: world"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalToYAML(tt.input, tt.sorted)
			if err != nil {
				t.Errorf("marshalToYAML() error = %v", err)
				return
			}
			for _, c := range tt.contains {
				if !strings.Contains(string(result), c) {
					t.Errorf("marshalToYAML() should contain %q, got %s", c, result)
				}
			}
		})
	}
}

func TestEscapeYAMLValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "simple", "simple"},
		{"empty", "", `""`},
		{"has: colon", "has: colon", `"has: colon"`},
		{"has# hash", "has# hash", `"has# hash"`},
		{"has\n newline", "has\n newline", `"has\n newline"`},
		{"has \"quote\"", "has \"quote\"", `"has \"quote\""`},
		{"- starts with dash", "- starts with dash", `"- starts with dash"`},
		// Scalars the YAML reader would type-coerce on reparse are quoted so
		// the string value survives a Marshal→Unmarshal round trip.
		{"true", "true", `"true"`},
		{"null", "null", `"null"`},
		{"~", "~", `"~"`},
		{"42", "42", `"42"`},
		{"3.14", "3.14", `"3.14"`},
		// Trailing whitespace is stripped from plain scalars — quote it.
		{"trailing space", "trailing ", `"trailing "`},
		// Control characters trigger quoting and are escaped inside the
		// quoted scalar; a backslash is escaped only when quoting is on.
		{"carriage return", "a\rb", `"a\rb"`},
		{"tab", "a\tb", `"a\tb"`},
		{"backslash plus tab", "a\\b\t", `"a\\b\t"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeYAMLValue(tt.input)
			if result != tt.expected {
				t.Errorf("escapeYAMLValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSetNestedValue_ConflictBranch covers the non-map intermediate
// conflict: once "A" holds a scalar, "A_B" cannot nest under it and is
// stored at the root under its full key instead.
func TestSetNestedValue_ConflictBranch(t *testing.T) {
	m := map[string]interface{}{}
	setNestedValue(m, "A", "1") // A becomes a scalar
	setNestedValue(m, "A_B", "2")

	if _, ok := m["A_B"]; !ok {
		t.Fatalf("A_B missing from result %v", m)
	}
	if _, ok := m["A"].(map[string]interface{}); ok {
		t.Error("A became a map, want it to stay a scalar")
	}
}
