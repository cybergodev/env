package internal

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// ============================================================================
// JSON Flatten Tests
// ============================================================================

// defaultJSONCfg returns the standard flatten configuration used by most
// cases; individual cases override fields as needed.
func defaultJSONCfg() JSONFlattenConfig {
	return JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}
}

func TestFlattenJSON(t *testing.T) {
	longKey := strings.Repeat("K", 70)

	depth2 := defaultJSONCfg()
	depth2.MaxDepth = 2
	dupError := defaultJSONCfg()
	dupError.DuplicateKey = DuplicateKeyError
	dupFirst := defaultJSONCfg()
	dupFirst.DuplicateKey = DuplicateKeyFirstWins
	dupLast := defaultJSONCfg()
	dupLast.DuplicateKey = DuplicateKeyLastWins

	tests := []struct {
		name        string
		input       string
		cfg         JSONFlattenConfig
		want        map[string]string // keys asserted when non-nil
		wantLen     int               // expected len(result); -1 skips the check
		wantErr     bool
		wantJSONErr bool // error must unwrap to *JSONError
		check       func(t *testing.T, result map[string]string)
	}{
		{
			name:    "empty input yields empty map",
			input:   "",
			cfg:     defaultJSONCfg(),
			wantLen: 0,
		},
		{
			// The root pre-scan rejects the whole document before any
			// decoding; distinct from the container-loop propagation case.
			name:        "root depth pre-scan rejects deep document",
			input:       `{"a":{"b":{"c":1}}}`,
			cfg:         depth2,
			wantErr:     true,
			wantJSONErr: true,
		},
		{
			name:        "flat-key collision rejects document",
			input:       `{"a_b": 1, "a": {"b": 2}}`,
			cfg:         defaultJSONCfg(),
			wantErr:     true,
			wantJSONErr: true,
		},
		{
			name:        "duplicate-key error policy pre-scan rejects",
			input:       `{"KEY": 1, "KEY": 2}`,
			cfg:         dupError,
			wantErr:     true,
			wantJSONErr: true,
		},
		{
			name:        "first-wins policy rejects JSON duplicates explicitly",
			input:       `{"KEY": 1, "KEY": 2}`,
			cfg:         dupFirst,
			wantErr:     true,
			wantJSONErr: true,
		},
		{
			name:    "last-wins policy accepts JSON duplicates",
			input:   `{"KEY": 1, "KEY": 2}`,
			cfg:     dupLast,
			want:    map[string]string{"KEY": "2"},
			wantLen: 1,
		},
		{
			name:    "root null yields empty map",
			input:   `null`,
			cfg:     defaultJSONCfg(),
			wantLen: 0,
		},
		{
			name:    "root scalar yields empty map",
			input:   `"hello"`,
			cfg:     defaultJSONCfg(),
			wantLen: 0,
		},
		{
			// Root scalars contribute no keys (the prefix guard in
			// flattenDecoded): bool and number mirror the string case above.
			name:    "root bool yields empty map",
			input:   `true`,
			cfg:     defaultJSONCfg(),
			wantLen: 0,
		},
		{
			name:    "root number yields empty map",
			input:   `42`,
			cfg:     defaultJSONCfg(),
			wantLen: 0,
		},
		{
			name:    "root array yields indexed items",
			input:   `["a", "b", "c"]`,
			cfg:     defaultJSONCfg(),
			wantLen: 3,
			check: func(t *testing.T, result map[string]string) {
				values := make(map[string]bool, len(result))
				for _, v := range result {
					values[v] = true
				}
				for _, want := range []string{"a", "b", "c"} {
					if !values[want] {
						t.Errorf("root array missing value %q (got %v)", want, result)
					}
				}
			},
		},
		{
			name:    "simple object",
			input:   `{"key1": "value1", "key2": "value2"}`,
			cfg:     defaultJSONCfg(),
			want:    map[string]string{"KEY1": "value1", "KEY2": "value2"},
			wantLen: 2,
		},
		{
			name:  "nested object",
			input: `{"database": {"host": "localhost", "port": 5432}}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"DATABASE_HOST": "localhost", "DATABASE_PORT": "5432"},
		},
		{
			name:  "array with underscore format",
			input: `{"items": ["one", "two", "three"]}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"ITEMS_0": "one", "ITEMS_1": "two", "ITEMS_2": "three"},
		},
		{
			name:  "array with bracket format",
			input: `{"items": ["a", "b", "c"]}`,
			cfg: func() JSONFlattenConfig {
				cfg := defaultJSONCfg()
				cfg.ArrayIndexFormat = "bracket"
				return cfg
			}(),
			want: map[string]string{"ITEMS[0]": "a", "ITEMS[1]": "b", "ITEMS[2]": "c"},
		},
		{
			name: "array of objects",
			input: `{"servers": [
				{"host": "server1", "port": 8080},
				{"host": "server2", "port": 9090}
			]}`,
			cfg: defaultJSONCfg(),
			want: map[string]string{
				"SERVERS_0_HOST": "server1",
				"SERVERS_0_PORT": "8080",
				"SERVERS_1_HOST": "server2",
				"SERVERS_1_PORT": "9090",
			},
			wantLen: 4,
		},
		{
			name: "nested array",
			input: `{"matrix": [
				["a", "b"],
				["c", "d"]
			]}`,
			cfg:     defaultJSONCfg(),
			want:    map[string]string{"MATRIX_0_0": "a", "MATRIX_0_1": "b", "MATRIX_1_0": "c", "MATRIX_1_1": "d"},
			wantLen: 4,
		},
		{
			name:  "null as empty",
			input: `{"key": null}`,
			cfg: func() JSONFlattenConfig {
				cfg := defaultJSONCfg()
				cfg.NullAsEmpty = true
				return cfg
			}(),
			want: map[string]string{"KEY": ""},
		},
		{
			name:  "null preserved",
			input: `{"key": null}`,
			cfg: func() JSONFlattenConfig {
				cfg := defaultJSONCfg()
				cfg.NullAsEmpty = false
				return cfg
			}(),
			want: map[string]string{"KEY": "null"},
		},
		{
			name:  "true as string",
			input: `{"enabled": true}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"ENABLED": "true"},
		},
		{
			name:  "false as string",
			input: `{"enabled": false}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"ENABLED": "false"},
		},
		{
			name:  "integer",
			input: `{"count": 42}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"COUNT": "42"},
		},
		{
			name:  "float",
			input: `{"rate": 3.14}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"RATE": "3.14"},
		},
		{
			name:  "float as integer",
			input: `{"count": 42.0}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"COUNT": "42"},
		},
		{
			name:  "negative number",
			input: `{"temp": -10}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"TEMP": "-10"},
		},
		{
			// 1e21 actually distinguishes the two renderings (flatten_keys.go
			// float64 arm): NumberAsString off renders via %v, which keeps
			// the exponent ("1e+21"). An integer like 42 renders identically
			// either way and would pin nothing.
			name:  "number rendered when NumberAsString is off",
			input: `{"count": 1e21}`,
			cfg: func() JSONFlattenConfig {
				cfg := defaultJSONCfg()
				cfg.NumberAsString = false
				return cfg
			}(),
			want: map[string]string{"COUNT": "1e+21"},
		},
		{
			// Companion: with NumberAsString on, the same input goes through
			// FormatFloat('f', -1) — 1e21 overflows int64, so the whole-number
			// FormatInt shortcut does not apply and the value expands.
			name:  "large exponent rendered expanded when NumberAsString is on",
			input: `{"count": 1e21}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"COUNT": "1000000000000000000000"},
		},
		{
			// Unified empty-container semantics (M-4): a key present in the
			// document exists in the store as "" — mirroring null handling
			// and the YAML block form, instead of vanishing.
			name:  "empty object produces empty string",
			input: `{"empty": {}}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"EMPTY": ""},
		},
		{
			name:  "empty array produces empty string",
			input: `{"empty": []}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"EMPTY": ""},
		},
		{
			// An empty ROOT document stays keyless (the prefix guard).
			name:    "empty root object produces no entries",
			input:   `{}`,
			cfg:     defaultJSONCfg(),
			wantLen: 0,
		},
		{
			name:        "invalid JSON returns JSONError",
			input:       `{invalid json}`,
			cfg:         defaultJSONCfg(),
			wantErr:     true,
			wantJSONErr: true,
		},
		{
			// Boundary: depth == MaxDepth is accepted; only strictly deeper
			// documents fail. (The over-limit side is pinned by
			// TestFlattenJSON_DepthErrorPropagation, including the *JSONError
			// type and container-loop propagation.)
			name:  "max depth exact boundary succeeds",
			input: `{"a":{"b":1}}`,
			cfg: func() JSONFlattenConfig {
				cfg := defaultJSONCfg()
				cfg.MaxDepth = 3
				return cfg
			}(),
			want: map[string]string{"A_B": "1"},
		},
		{
			// Boundary: writeUpperASCII passes non-ASCII bytes through
			// unchanged (byte-level fold) and the delimiter joins the path
			// segments without altering them.
			name:  "unicode keys pass through with delimiter join",
			input: `{"数据库":{"主机":"x"}}`,
			cfg:   defaultJSONCfg(),
			want:  map[string]string{"数据库_主机": "x"},
		},
		{
			// Boundary: an unrecognized ArrayIndexFormat string is not an
			// error — buildFlatArrayIndex falls back to the underscore form
			// for anything that is not "bracket".
			name:  "unknown array index format falls back to underscore",
			input: `{"items": ["a"]}`,
			cfg: func() JSONFlattenConfig {
				cfg := defaultJSONCfg()
				cfg.ArrayIndexFormat = "roman"
				return cfg
			}(),
			want: map[string]string{"ITEMS_0": "a"},
		},
		{
			// Boundary: an empty KeyDelimiter concatenates path segments
			// with no separator (buildFlatKey writes the delimiter verbatim).
			name:  "empty key delimiter concatenates without separator",
			input: `{"a":{"b":"x"}}`,
			cfg: func() JSONFlattenConfig {
				cfg := defaultJSONCfg()
				cfg.KeyDelimiter = ""
				return cfg
			}(),
			want: map[string]string{"AB": "x"},
		},
		{
			name: "complex structure",
			input: `{
				"app": {
					"name": "myapp",
					"version": "1.0.0",
					"features": ["auth", "logging"]
				},
				"database": {
					"host": "localhost",
					"port": 5432
				}
			}`,
			cfg: defaultJSONCfg(),
			want: map[string]string{
				"APP_NAME":       "myapp",
				"APP_VERSION":    "1.0.0",
				"APP_FEATURES_0": "auth",
				"APP_FEATURES_1": "logging",
				"DATABASE_HOST":  "localhost",
				"DATABASE_PORT":  "5432",
			},
			wantLen: 6,
		},
		{
			// Boundary: combined key length crosses the builder fast-path
			// threshold for nested keys.
			name:    "long nested key",
			input:   fmt.Sprintf(`{"%s": {"nested": "value"}}`, longKey),
			cfg:     defaultJSONCfg(),
			wantLen: 1,
			check: func(t *testing.T, result map[string]string) {
				for k := range result {
					if len(k) < 70 {
						t.Errorf("expected key length >= 70, got %d: %q", len(k), k)
					}
				}
			},
		},
		{
			name:    "long key with bracket format uses builder path",
			input:   fmt.Sprintf(`{"%s": ["a", "b"]}`, longKey),
			cfg:     func() JSONFlattenConfig { cfg := defaultJSONCfg(); cfg.ArrayIndexFormat = "bracket"; return cfg }(),
			wantLen: 2,
			check: func(t *testing.T, result map[string]string) {
				values := make(map[string]bool, len(result))
				for k, v := range result {
					if !strings.Contains(k, "[") {
						t.Errorf("expected bracket in key %q", k)
					}
					values[v] = true
				}
				for _, want := range []string{"a", "b"} {
					if !values[want] {
						t.Errorf("missing value %q (got %v)", want, result)
					}
				}
			},
		},
		{
			name:    "long key with underscore format uses builder path",
			input:   fmt.Sprintf(`{"%s": ["a"]}`, longKey),
			cfg:     defaultJSONCfg(),
			wantLen: 1,
			check: func(t *testing.T, result map[string]string) {
				for k := range result {
					if len(k) < 70 {
						t.Errorf("expected key length >= 70, got %d: %q", len(k), k)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FlattenJSON([]byte(tt.input), tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FlattenJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantJSONErr {
				var jsonErr *JSONError
				if !errors.As(err, &jsonErr) {
					t.Fatalf("error type = %T, want *JSONError", err)
				}
				return
			}
			if tt.wantErr {
				return
			}
			for key, exp := range tt.want {
				if result[key] != exp {
					t.Errorf("result[%q] = %q, want %q", key, result[key], exp)
				}
			}
			// wantLen defaults to len(want) so a row that lists expected keys
			// also pins the result size without repeating the count.
			wantLen := tt.wantLen
			if wantLen == 0 && len(tt.want) > 0 {
				wantLen = len(tt.want)
			}
			if len(result) != wantLen {
				t.Errorf("len(result) = %d, want %d (got keys: %v)", len(result), wantLen, keysOf(result))
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

// ============================================================================
// buildKey Tests (JSON)
// ============================================================================

func TestBuildKey_JSON(t *testing.T) {
	// Key construction is shared with the YAML flattener (flatten_keys.go);
	// these cases pin the JSON-side layout through the shared builders.
	tests := []struct {
		prefix   string
		key      string
		expected string
	}{
		{"", "key", "KEY"},
		{"APP", "key", "APP_KEY"},
		{"APP_DATABASE", "host", "APP_DATABASE_HOST"},
		{"", "lower", "LOWER"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"_"+tt.key, func(t *testing.T) {
			result := buildFlatKey(tt.prefix, tt.key, "_")
			if result != tt.expected {
				t.Errorf("buildFlatKey(%q, %q) = %q, want %q", tt.prefix, tt.key, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// buildArrayIndex Tests (JSON)
// ============================================================================

func TestBuildArrayIndex_JSON(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		index    int
		cfg      JSONFlattenConfig
		expected string
	}{
		{
			name:     "underscore format",
			prefix:   "ITEMS",
			index:    0,
			cfg:      JSONFlattenConfig{ArrayIndexFormat: "underscore", KeyDelimiter: "_"},
			expected: "ITEMS_0",
		},
		{
			name:     "bracket format",
			prefix:   "ITEMS",
			index:    5,
			cfg:      JSONFlattenConfig{ArrayIndexFormat: "bracket"},
			expected: "ITEMS[5]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFlatArrayIndex(tt.prefix, tt.index, tt.cfg.KeyDelimiter, tt.cfg.ArrayIndexFormat)
			if result != tt.expected {
				t.Errorf("buildFlatArrayIndex(%q, %d) = %q, want %q", tt.prefix, tt.index, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Depth Propagation, Pre-Scanner & Default-Branch Coverage
// ============================================================================

// TestFlattenJSON_DepthErrorPropagation pins the fail-late depth contract:
// inputs whose bracket nesting passes the conservative pre-scan but whose
// value depth still reaches MaxDepth must surface a *JSONError from
// flattenValue, propagated through the container loops back to FlattenJSON.
func TestFlattenJSON_DepthErrorPropagation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"error propagates through map loops", `{"a":{"b":1}}`},
		{"error propagates through array loops", `[["x"]]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultJSONCfg()
			cfg.MaxDepth = 2
			_, err := FlattenJSON([]byte(tt.input), cfg)
			if err == nil {
				t.Fatalf("FlattenJSON(%s) error = nil, want depth error", tt.input)
			}
			var jsonErr *JSONError
			if !errors.As(err, &jsonErr) {
				t.Errorf("error = %T, want *JSONError", err)
			}
		})
	}
}

// TestFlattenJSON_NilInput pins the len==0 branch (json_flatten.go): nil
// input is not an error — it yields an empty map, like an empty byte slice.
func TestFlattenJSON_NilInput(t *testing.T) {
	result, err := FlattenJSON(nil, defaultJSONCfg())
	if err != nil {
		t.Fatalf("FlattenJSON(nil) error = %v, want nil", err)
	}
	if len(result) != 0 {
		t.Errorf("FlattenJSON(nil) = %v, want empty map", result)
	}
}

// TestFlattenJSON_WhitespaceOnly pins the GEN-001 consistency fix: a JSON
// document containing only whitespace flattens to an empty map like an empty
// file does, instead of surfacing a JSON syntax error (the .env and YAML
// paths already treated whitespace-only input as empty).
func TestFlattenJSON_WhitespaceOnly(t *testing.T) {
	for _, in := range []string{"", "\n", " \t\r\n", "   "} {
		result, err := FlattenJSON([]byte(in), defaultJSONCfg())
		if err != nil {
			t.Fatalf("FlattenJSON(%q) error = %v, want nil", in, err)
		}
		if len(result) != 0 {
			t.Errorf("FlattenJSON(%q) = %v, want empty map", in, result)
		}
	}
}

// TestScanJSONLimits covers the pre-scanner in one table: the tolerance
// branches (negative nesting clamps to zero; string bodies, including
// escaped quotes, are skipped so brackets inside JSON strings never count)
// and the node cap, exercised independently of the global HardMaxJSONNodes
// constant via per-row caps. maxDepth is 5 for every row.
func TestScanJSONLimits(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxNodes  int
		wantDepth bool
		wantNodes bool
	}{
		{"unbalanced close clamps to zero", `{"a":1}]`, HardMaxJSONNodes, false, false},
		{"bracket inside string ignored", `{"k": "a[b"}`, HardMaxJSONNodes, false, false},
		{"escaped quote does not end string", `{"a\":\"b": 1}`, HardMaxJSONNodes, false, false},
		{"deep real nesting detected", `[[[[[[1]]]]]]`, HardMaxJSONNodes, true, false},
		// `[1,2,3]` = 1 opening bracket + 2 commas = 3 nodes.
		{"node cap exceeded", `[1,2,3]`, 2, false, true},
		{"node cap exactly at limit ok", `[1,2,3]`, 3, false, false},
		{"string contents must not count toward the node cap", `"[,,"`, 2, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDepth, gotNodes := scanJSONLimits([]byte(tt.input), 0, 5, tt.maxNodes)
			if gotDepth != tt.wantDepth {
				t.Errorf("scanJSONLimits(%s) depth = %v, want %v", tt.input, gotDepth, tt.wantDepth)
			}
			if gotNodes != tt.wantNodes {
				t.Errorf("scanJSONLimits(%s) nodes = %v, want %v", tt.input, gotNodes, tt.wantNodes)
			}
		})
	}
}

// TestFlattenJSON_NodeCap (SEC-02): documents whose structural node count
// exceeds HardMaxJSONNodes are rejected by the pre-scan before
// json.Unmarshal materializes a parse tree disproportionate to the input.
func TestFlattenJSON_NodeCap(t *testing.T) {
	// Each "[]," contributes 2 nodes (open bracket + comma).
	input := "[" + strings.Repeat("[],", HardMaxJSONNodes/2+1) + "]"
	_, err := FlattenJSON([]byte(input), defaultJSONCfg())
	if err == nil {
		t.Fatal("expected node cap error")
	}
	var je *JSONError
	if !errors.As(err, &je) {
		t.Fatalf("want *JSONError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "node count") {
		t.Errorf("error should mention node count: %v", err)
	}
}

// TestFlattenValue_UnsupportedType pins flattenValue's terminal default
// branch with a type json.Unmarshal never produces.
func TestFlattenValue_UnsupportedType(t *testing.T) {
	err := flattenValue(complex128(1+2i), "K", defaultJSONCfg(), map[string]string{}, 0)
	if err == nil {
		t.Fatal("flattenValue(complex128) error = nil, want error")
	}
	var jsonErr *JSONError
	if !errors.As(err, &jsonErr) {
		t.Errorf("error = %T, want *JSONError", err)
	}
}
