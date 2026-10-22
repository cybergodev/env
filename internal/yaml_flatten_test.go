package internal

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// ============================================================================
// YAML Flatten Tests
// ============================================================================

// defaultFlattenCfg returns the standard flatten configuration used by most
// cases; individual tests override fields as needed.
func defaultFlattenCfg() YAMLFlattenConfig {
	return YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}
}

// bracketFlattenCfg returns the standard configuration with bracket array
// indexing ("KEY[0]" instead of "KEY_0").
func bracketFlattenCfg() YAMLFlattenConfig {
	cfg := defaultFlattenCfg()
	cfg.ArrayIndexFormat = "bracket"
	return cfg
}

// TestFlattenYAML_UnbalancedQuoteKeyNotDropped pins the isKeyStart line-bound
// fix: an apostrophe in a plain key ("it's: 1") previously made the quote
// skip swallow the rest of the input, so the ": " check never fired, the
// line parsed as stray values, and parseMap silently discarded it. The line
// now classifies as a key and survives to (and is rejected by) key
// validation — loud, not silent.
func TestFlattenYAML_UnbalancedQuoteKeyNotDropped(t *testing.T) {
	result := flattenYAMLT(t, "it's: 1\nb: 2\n")
	if result["IT'S"] != "1" {
		t.Errorf("IT'S = %q, want 1 (the line must not be silently dropped)", result["IT'S"])
	}
	if result["B"] != "2" {
		t.Errorf("B = %q, want 2", result["B"])
	}
}

// TestFlattenYAML_EmptyContainerSemantics pins the unified empty-container
// behavior: "a: {}" (inline), "a: []" and the block/null form all yield
// A:"" — previously the inline forms dropped the key entirely while the
// null form emitted "", so the same logical value produced different key
// sets depending on its spelling.
func TestFlattenYAML_EmptyContainerSemantics(t *testing.T) {
	for _, doc := range []string{
		"a: {}\nb: 1\n",
		"a: []\nb: 1\n",
		"a:\nb: 1\n",
	} {
		result := flattenYAMLT(t, doc)
		if result["A"] != "" {
			t.Errorf("FlattenYAML(%q): A = %q, want \"\" (empty container keeps the key)", doc, result["A"])
		}
		if result["B"] != "1" {
			t.Errorf("FlattenYAML(%q): B = %q, want 1", doc, result["B"])
		}
	}
}

// flattenYAMLT parses and flattens a YAML document for tests.
func flattenYAMLT(t *testing.T, doc string) map[string]string {
	t.Helper()
	value, err := ParseYAML([]byte(doc), 16)
	if err != nil {
		t.Fatalf("ParseYAML(%q) error = %v", doc, err)
	}
	defer ReleaseValue(value)
	result, err := FlattenYAML(value, defaultFlattenCfg())
	if err != nil {
		t.Fatalf("FlattenYAML(%q) error = %v", doc, err)
	}
	return result
}

func TestFlattenYAML(t *testing.T) {
	tests := []struct {
		name    string
		value   *Value
		cfg     YAMLFlattenConfig
		want    map[string]string // keys asserted when non-nil
		wantLen int               // expected len(result); -1 skips the check
		wantErr bool
		check   func(t *testing.T, result map[string]string) // extra shape assertions
	}{
		{
			name:    "nil value yields empty map",
			value:   nil,
			cfg:     defaultFlattenCfg(),
			wantLen: 0,
		},
		{
			name: "simple map",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"KEY1": NewScalarValue("value1", 1, 1),
					"KEY2": NewScalarValue("value2", 1, 1),
				},
			},
			cfg:     defaultFlattenCfg(),
			want:    map[string]string{"KEY1": "value1", "KEY2": "value2"},
			wantLen: 2,
		},
		{
			name: "nested map",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"DATABASE": {
						Type: ValueTypeMap,
						Map: map[string]*Value{
							"HOST": NewScalarValue("localhost", 2, 3),
							"PORT": NewScalarValue("5432", 2, 3),
						},
					},
				},
			},
			cfg:     defaultFlattenCfg(),
			want:    map[string]string{"DATABASE_HOST": "localhost", "DATABASE_PORT": "5432"},
			wantLen: 2,
		},
		{
			// KeyDelimiter is configurable: "." joins nested keys as
			// "DATABASE.HOST" instead of "DATABASE_HOST". Uppercasing is
			// independent of the delimiter — every key segment (including
			// the appended leaf) folds via writeUpperASCII in buildFlatKey;
			// only the joiner changes.
			name: "custom key delimiter joins nested keys",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"database": {
						Type: ValueTypeMap,
						Map: map[string]*Value{
							"host": NewScalarValue("localhost", 2, 3),
						},
					},
				},
			},
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.KeyDelimiter = "."
				return cfg
			}(),
			want:    map[string]string{"DATABASE.HOST": "localhost"},
			wantLen: 1,
		},
		{
			// Flat-key collision: a literal "a_b" and a nested "a: {b: ...}"
			// collapse onto A_B — always an error, independent of policy
			// (the collision check lives in the flattener). Same contract
			// as the root-level TestFlattenCollision, pinned here so the
			// internal package exercises its own error branch.
			name: "flat-key collision between literal and nested key",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"a_b": NewScalarValue("literal", 1, 1),
					"a": {
						Type: ValueTypeMap,
						Map:  map[string]*Value{"b": NewScalarValue("nested", 2, 1)},
					},
				},
			},
			cfg:     defaultFlattenCfg(),
			wantErr: true,
		},
		{
			// ToUpperASCII in buildFlatKey (flatten_keys.go) is ASCII-only:
			// non-ASCII keys pass through byte-for-byte with no case
			// folding.
			name: "unicode key is preserved and not uppercased",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"键": NewScalarValue("值", 1, 1),
				},
			},
			cfg:     defaultFlattenCfg(),
			want:    map[string]string{"键": "值"},
			wantLen: 1,
		},
		{
			name: "array with underscore format",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"ITEMS": {
						Type: ValueTypeArray,
						Array: []*Value{
							NewScalarValue("one", 1, 1),
							NewScalarValue("two", 1, 1),
							NewScalarValue("three", 1, 1),
						},
					},
				},
			},
			cfg:     defaultFlattenCfg(),
			want:    map[string]string{"ITEMS_0": "one", "ITEMS_1": "two", "ITEMS_2": "three"},
			wantLen: 3,
		},
		{
			name: "array with bracket format",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"ITEMS": {
						Type: ValueTypeArray,
						Array: []*Value{
							NewScalarValue("one", 1, 1),
							NewScalarValue("two", 1, 1),
						},
					},
				},
			},
			cfg:     bracketFlattenCfg(),
			want:    map[string]string{"ITEMS[0]": "one", "ITEMS[1]": "two"},
			wantLen: 2,
		},
		{
			name: "long key with bracket format uses builder path",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					strings.Repeat("K", 70): {
						Type:  ValueTypeArray,
						Array: []*Value{NewScalarValue("a", 1, 1), NewScalarValue("b", 1, 1)},
					},
				},
			},
			cfg:     bracketFlattenCfg(),
			wantLen: 2,
			check: func(t *testing.T, result map[string]string) {
				for k := range result {
					if !strings.Contains(k, "[") {
						t.Errorf("expected bracket in key %q", k)
					}
				}
			},
		},
		{
			name: "long key with underscore format uses builder path",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					strings.Repeat("K", 70): {
						Type:  ValueTypeArray,
						Array: []*Value{NewScalarValue("a", 1, 1)},
					},
				},
			},
			cfg:     defaultFlattenCfg(),
			wantLen: 1,
			check: func(t *testing.T, result map[string]string) {
				for k := range result {
					if !strings.HasSuffix(k, "_0") {
						t.Errorf("expected underscore index suffix in key %q", k)
					}
				}
			},
		},
		{
			name: "empty nested map flattens to empty string",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"EMPTY": {Type: ValueTypeMap, Map: map[string]*Value{}},
				},
			},
			cfg:     defaultFlattenCfg(),
			want:    map[string]string{"EMPTY": ""},
			wantLen: 1,
		},
		{
			name: "empty array flattens to empty string",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"EMPTY": {Type: ValueTypeArray, Array: []*Value{}},
				},
			},
			cfg:     defaultFlattenCfg(),
			want:    map[string]string{"EMPTY": ""},
			wantLen: 1,
		},
		{
			name: "max depth exceeded returns error",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"A": {
						Type: ValueTypeMap,
						Map: map[string]*Value{
							"B": {
								Type: ValueTypeMap,
								Map: map[string]*Value{
									"C": NewScalarValue("deep", 1, 1),
								},
							},
						},
					},
				},
			},
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.MaxDepth = 2
				return cfg
			}(),
			wantErr: true,
		},
		{
			// Depth errors propagate through every enclosing array loop:
			// an element hitting MaxDepth must abort the whole flatten.
			name: "array depth error propagates through array loops",
			value: func() *Value {
				inner := NewArrayValue(1, 1)
				inner.Array = append(inner.Array, NewScalarValue("x", 1, 1))
				mid := NewArrayValue(1, 1)
				mid.Array = append(mid.Array, inner)
				root := NewArrayValue(1, 1)
				root.Array = append(root.Array, mid)
				return root
			}(),
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.MaxDepth = 2
				return cfg
			}(),
			wantErr: true,
		},
		{
			// Zero-value config: MaxDepth 0 makes the very first
			// flattenYAMLValue call trip its depth check (0 >= 0), before
			// any value is visited.
			name:    "zero-value config errors immediately on depth",
			value:   &Value{Type: ValueTypeMap, Map: map[string]*Value{"KEY": NewScalarValue("v", 1, 1)}},
			cfg:     YAMLFlattenConfig{},
			wantErr: true,
		},
		{
			name:    "scalar with no prefix is dropped",
			value:   NewScalarValue("standalone", 1, 1),
			cfg:     defaultFlattenCfg(),
			wantLen: 0,
		},
		{
			name: "nil value in map is skipped",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"KEY": nil,
				},
			},
			cfg:     defaultFlattenCfg(),
			wantLen: 0,
		},
		{
			name: "nil element in array keeps sibling indexes",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"ITEMS": {
						Type: ValueTypeArray,
						Array: []*Value{
							NewScalarValue("one", 1, 1),
							nil,
							NewScalarValue("three", 1, 1),
						},
					},
				},
			},
			cfg:     defaultFlattenCfg(),
			want:    map[string]string{"ITEMS_0": "one", "ITEMS_2": "three"},
			wantLen: 2,
		},
		{
			name: "long nested key exceeds inline builder threshold",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"a" + strings.Repeat("b", 70): {
						Type: ValueTypeMap,
						Map: map[string]*Value{
							"nested": NewScalarValue("value", 1, 1),
						},
					},
				},
			},
			cfg:     defaultFlattenCfg(),
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
			name: "float is preserved when NumberAsString is off",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"pi": NewScalarValue("3.14", 1, 1),
				},
			},
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.NumberAsString = false
				return cfg
			}(),
			want:    map[string]string{"PI": "3.14"},
			wantLen: 1,
		},
		{
			name: "complex structure",
			value: &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"APP": {
						Type: ValueTypeMap,
						Map: map[string]*Value{
							"NAME":    NewScalarValue("myapp", 1, 1),
							"VERSION": NewScalarValue("1.0.0", 1, 1),
							"FEATURES": {
								Type: ValueTypeArray,
								Array: []*Value{
									NewScalarValue("auth", 1, 1),
									NewScalarValue("logging", 1, 1),
								},
							},
						},
					},
					"DATABASE": {
						Type: ValueTypeMap,
						Map: map[string]*Value{
							"HOST": NewScalarValue("localhost", 1, 1),
							"PORT": NewScalarValue("5432", 1, 1),
						},
					},
				},
			},
			cfg: defaultFlattenCfg(),
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FlattenYAML(tt.value, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FlattenYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var yamlErr *YAMLError
				if !errors.As(err, &yamlErr) {
					t.Errorf("FlattenYAML() error = %T (%v), want *YAMLError", err, err)
				}
				return
			}
			for key, exp := range tt.want {
				if result[key] != exp {
					t.Errorf("result[%q] = %q, want %q", key, result[key], exp)
				}
			}
			if tt.wantLen >= 0 && len(result) != tt.wantLen {
				t.Errorf("len(result) = %d, want %d (got keys: %v)", len(result), tt.wantLen, keysOf(result))
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

// keysOf returns the sorted keys of m for failure messages.
func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ============================================================================
// Inline JSON Tests
// ============================================================================

// TestFlattenYAML_InlineJSON drives the inline-JSON handling of unquoted
// YAML scalars through the full FlattenYAML pipeline: containers, scalar
// rendering under every config knob, syntax-failure fallback, and the depth
// limit. (Quoted scalars never take this path — see
// TestFlattenYAML_QuotedScalarsStayStrings.)
func TestFlattenYAML_InlineJSON(t *testing.T) {
	tests := []struct {
		name    string
		scalar  string // inline JSON scalar stored under "key"
		cfg     YAMLFlattenConfig
		want    map[string]string
		wantErr bool
	}{
		{
			name:   "array",
			scalar: `["a", "b", "c"]`,
			cfg:    defaultFlattenCfg(),
			want: map[string]string{
				"KEY_0": "a", "KEY_1": "b", "KEY_2": "c",
			},
		},
		{
			name:   "object",
			scalar: `{"host": "localhost", "port": 8080}`,
			cfg:    defaultFlattenCfg(),
			want: map[string]string{
				"KEY_HOST": "localhost", "KEY_PORT": "8080",
			},
		},
		{
			name:   "nested object",
			scalar: `{"a": {"b": "deep"}}`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_A_B": "deep"},
		},
		{
			name:   "array of objects",
			scalar: `[{"host": "a"}, {"host": "b"}]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0_HOST": "a", "KEY_1_HOST": "b"},
		},
		{
			name:   "mixed array elements",
			scalar: `[1, "a", null, true, 1.5]`,
			cfg:    defaultFlattenCfg(),
			want: map[string]string{
				"KEY_0": "1",
				"KEY_1": "a",
				"KEY_2": "", // null with NullAsEmpty=true
				"KEY_3": "true",
				"KEY_4": "1.5",
			},
		},
		{
			// Whole-number floats render without a decimal point; true
			// fractional floats keep their shortest representation.
			name:   "int-like float formatted without decimal point",
			scalar: `[2.0, 3.25]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0": "2", "KEY_1": "3.25"},
		},
		{
			name:   "nested object inside array",
			scalar: `[{"host": "db", "port": 5432}]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0_HOST": "db", "KEY_0_PORT": "5432"},
		},
		{
			name:   "null preserved when NullAsEmpty is off",
			scalar: `{"sub": null}`,
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.NullAsEmpty = false
				return cfg
			}(),
			want: map[string]string{"KEY_SUB": "null"},
		},
		{
			name:   "bool rendered when BoolAsString is off",
			scalar: `{"flag": true}`,
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.BoolAsString = false
				return cfg
			}(),
			want: map[string]string{"KEY_FLAG": "true"},
		},
		{
			name:   "number rendered when NumberAsString is off",
			scalar: `{"count": 42}`,
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.NumberAsString = false
				return cfg
			}(),
			want: map[string]string{"KEY_COUNT": "42"},
		},
		{
			name:   "float is not stringified when NumberAsString is off",
			scalar: `{"ratio": 1.5}`,
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.NumberAsString = false
				return cfg
			}(),
			want: map[string]string{"KEY_RATIO": "1.5"},
		},
		{
			// Unquoted YAML flow scalars are not strict JSON; the flow
			// converter now parses them instead of storing the raw text as
			// a scalar (GEN-001: the lenient fallback silently corrupted
			// flow collection data).
			name:   "unquoted YAML flow array is flattened",
			scalar: `[a, b]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0": "a", "KEY_1": "b"},
		},
		{
			name:   "unquoted YAML flow map is flattened",
			scalar: `{x: 1}`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_X": "1"},
		},
		{
			name:   "single-quoted flow elements preserve commas and spaces",
			scalar: `['a b', "c, d"]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0": "a b", "KEY_1": "c, d"},
		},
		{
			// ':' not followed by a space stays inside the plain scalar.
			name:   "colon inside flow scalar survives",
			scalar: `[http://example.com, y]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0": "http://example.com", "KEY_1": "y"},
		},
		{
			name:   "empty flow element becomes null",
			scalar: `[a,,b]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0": "a", "KEY_1": "", "KEY_2": "b"},
		},
		{
			name:   "trailing comma in flow array is tolerated",
			scalar: `[a, b,]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0": "a", "KEY_1": "b"},
		},
		{
			// Quote characters inside a plain flow element are content.
			name:   "quote inside plain flow element is content",
			scalar: `[not 'valid json]`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY_0": "not 'valid json"},
		},
		{
			// The inner '[' never closes, so both strict JSON and the flow
			// conversion fail: the plain-scalar fallback survives for
			// genuinely broken text.
			name:   "unclosed inner collection treated as regular scalar",
			scalar: `{a: [}`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY": "{a: [}"},
		},
		{
			// A ':' not followed by a space makes the text unparseable as a
			// flow map, so it keeps the plain-scalar fallback.
			name:   "colon without following space treated as regular scalar",
			scalar: `{x:1}`,
			cfg:    defaultFlattenCfg(),
			want:   map[string]string{"KEY": "{x:1}"},
		},
		{
			// Depth-limit hits inside inline JSON propagate as errors (the
			// fail-fast pre-scan trips at startDepth+nesting > MaxDepth);
			// only syntax failures fall back to the plain-scalar treatment.
			name:   "depth overflow inside inline JSON errors",
			scalar: `{"a": {"b": {"c": "too deep"}}}`,
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.MaxDepth = 3
				return cfg
			}(),
			wantErr: true,
		},
		{
			// Boundary: startDepth+nesting == MaxDepth passes the fail-fast
			// pre-check but still trips flattenDecoded's own depth check
			// (depth >= MaxDepth) one level deeper — and that error now
			// propagates instead of degrading to the raw scalar.
			name:   "exact boundary depth trips recursive check after pre-check passes",
			scalar: `{"a": "b"}`,
			cfg: func() YAMLFlattenConfig {
				cfg := defaultFlattenCfg()
				cfg.MaxDepth = 2
				return cfg
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := &Value{
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"key": NewScalarValue(tt.scalar, 1, 1),
				},
			}

			result, err := FlattenYAML(value, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FlattenYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for key, exp := range tt.want {
				if result[key] != exp {
					t.Errorf("result[%q] = %q, want %q", key, result[key], exp)
				}
			}
		})
	}
}

// TestFlattenInlineJSON_ScalarDirectCall is the one surviving direct-call
// case of the former branch-coverage test: flattenInlineJSON itself (not via
// FlattenYAML) decodes a bare JSON scalar and stores it through the shared
// walker's scalar branch. The container-loop error propagation formerly
// tested alongside it is covered by the depth rows of
// TestFlattenYAML_InlineJSON — same walker, full-pipeline entry.
func TestFlattenInlineJSON_ScalarDirectCall(t *testing.T) {
	result := map[string]string{}
	if err := flattenInlineJSON(`"text"`, "K", defaultFlattenCfg(), result, 0); err != nil {
		t.Fatalf("flattenInlineJSON(scalar) error = %v", err)
	}
	if result["K"] != "text" {
		t.Errorf("result[K] = %q, want %q", result["K"], "text")
	}
}

// ============================================================================
// convertYAMLScalar Tests
// ============================================================================

// TestConvertYAMLScalar pins the scalar conversion table: null variants
// (including the EqualFoldASCII case-insensitive forms), booleans under both
// BoolAsString settings, number normalization (exponents, int-like floats),
// and the pass-through default.
func TestConvertYAMLScalar(t *testing.T) {
	tests := []struct {
		name  string
		input string
		cfg   YAMLFlattenConfig
		want  string
	}{
		{
			name:  "null as empty",
			input: "null",
			cfg:   YAMLFlattenConfig{NullAsEmpty: true},
			want:  "",
		},
		{
			name:  "null preserved",
			input: "null",
			cfg:   YAMLFlattenConfig{NullAsEmpty: false},
			want:  "null",
		},
		{
			// Null detection is ASCII case-insensitive (EqualFoldASCII).
			name:  "Null (mixed case) as empty",
			input: "Null",
			cfg:   YAMLFlattenConfig{NullAsEmpty: true},
			want:  "",
		},
		{
			name:  "NULL (upper case) preserved as literal",
			input: "NULL",
			cfg:   YAMLFlattenConfig{NullAsEmpty: false},
			want:  "null",
		},
		{
			name:  "tilde as empty",
			input: "~",
			cfg:   YAMLFlattenConfig{NullAsEmpty: true},
			want:  "",
		},
		{
			name:  "empty renders null when NullAsEmpty=false",
			input: "",
			cfg:   YAMLFlattenConfig{NullAsEmpty: false},
			want:  "null",
		},
		{
			name:  "empty renders empty when NullAsEmpty=true",
			input: "",
			cfg:   YAMLFlattenConfig{NullAsEmpty: true},
			want:  "",
		},
		{
			name:  "true canonical when BoolAsString=true",
			input: "true",
			cfg:   YAMLFlattenConfig{BoolAsString: true},
			want:  "true",
		},
		{
			name:  "TRUE keeps case when BoolAsString=false",
			input: "TRUE",
			cfg:   YAMLFlattenConfig{BoolAsString: false},
			want:  "TRUE",
		},
		{
			name:  "False keeps case when BoolAsString=false",
			input: "False",
			cfg:   YAMLFlattenConfig{BoolAsString: false},
			want:  "False",
		},
		{
			name:  "false canonical when BoolAsString=true",
			input: "false",
			cfg:   YAMLFlattenConfig{BoolAsString: true},
			want:  "false",
		},
		{
			name:  "integer",
			input: "42",
			cfg:   YAMLFlattenConfig{NumberAsString: true},
			want:  "42",
		},
		{
			name:  "negative number",
			input: "-42",
			cfg:   YAMLFlattenConfig{NumberAsString: true},
			want:  "-42",
		},
		{
			name:  "float",
			input: "3.14",
			cfg:   YAMLFlattenConfig{NumberAsString: true},
			want:  "3.14",
		},
		{
			name:  "float as int",
			input: "42.0",
			cfg:   YAMLFlattenConfig{NumberAsString: true},
			want:  "42",
		},
		{
			// Exponent notation parses as a float and normalizes: 1.5e3 is
			// a whole number, so it renders as the integer 1500.
			name:  "exponent normalized to integer",
			input: "1.5e3",
			cfg:   YAMLFlattenConfig{NumberAsString: true},
			want:  "1500",
		},
		{
			name:  "regular string",
			input: "hello world",
			cfg:   YAMLFlattenConfig{},
			want:  "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertYAMLScalar(tt.input, tt.cfg)
			if result != tt.want {
				t.Errorf("convertYAMLScalar(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

// ============================================================================
// buildYAMLKey / buildYAMLArrayIndex Tests
// ============================================================================

// TestBuildYAMLKey keeps only the YAML-side long-key observation: a long
// combined key still joins correctly through the shared builder, and the
// PREFIX is never uppercased (writeUpperASCII applies to the key part
// only). The basic construction matrix of the shared buildFlatKey
// (flatten_keys.go) lives in json_flatten_test.go (TestBuildKey_JSON).
func TestBuildYAMLKey(t *testing.T) {
	cfg := YAMLFlattenConfig{KeyDelimiter: "_"}
	prefix := strings.Repeat("p", 60)
	key := strings.Repeat("k", 20)
	want := prefix + "_" + strings.Repeat("K", 20)

	if got := buildYAMLKey(prefix, key, cfg); got != want {
		t.Errorf("buildYAMLKey(long prefix) = %q, want %q", got, want)
	}
}

// TestBuildYAMLArrayIndex keeps only the YAML-side long-prefix rows: long
// keys still render correctly through the shared builder in both index
// formats. The basic underscore/bracket matrix of the shared
// buildFlatArrayIndex (flatten_keys.go) lives in json_flatten_test.go
// (TestBuildArrayIndex_JSON).
func TestBuildYAMLArrayIndex(t *testing.T) {
	longPrefix := strings.Repeat("K", 70)

	tests := []struct {
		name     string
		prefix   string
		index    int
		cfg      YAMLFlattenConfig
		expected string
	}{
		{
			name:     "underscore format with long prefix",
			prefix:   longPrefix,
			index:    7,
			cfg:      YAMLFlattenConfig{ArrayIndexFormat: "underscore", KeyDelimiter: "_"},
			expected: longPrefix + "_7",
		},
		{
			name:     "bracket format with long prefix",
			prefix:   longPrefix,
			index:    9,
			cfg:      YAMLFlattenConfig{ArrayIndexFormat: "bracket"},
			expected: longPrefix + "[9]",
		},
		{
			name:     "bracket format with 61-char prefix",
			prefix:   strings.Repeat("K", 61),
			index:    1,
			cfg:      YAMLFlattenConfig{ArrayIndexFormat: "bracket"},
			expected: strings.Repeat("K", 61) + "[1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildYAMLArrayIndex(tt.prefix, tt.index, tt.cfg)
			if result != tt.expected {
				t.Errorf("buildYAMLArrayIndex(%q, %d) = %q, want %q", tt.prefix, tt.index, result, tt.expected)
			}
		})
	}
}

// TestFlattenYAML_QuotedScalarsStayStrings verifies YAML semantics on the read
// path: a quoted scalar is always a string — no null/bool/number coercion and
// no whitespace stripping — even when NullAsEmpty/BoolAsString are enabled.
// Regression: the quoting flag was dropped between lexer and Value tree, so
// `MODE: "null"` read back as "" and `SPACED: "trailing "` lost its space.
func TestFlattenYAML_QuotedScalarsStayStrings(t *testing.T) {
	input := `MODE: "null"
EMPTY_WORD: "~"
FLAG: "true"
RATIO: "3.14"
SPACED: "trailing space "
PLAIN_NULL: null
`

	value, err := ParseYAML([]byte(input), 10)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	defer ReleaseValue(value)

	result, err := FlattenYAML(value, defaultFlattenCfg())
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	want := map[string]string{
		"MODE":       "null",
		"EMPTY_WORD": "~",
		"FLAG":       "true",
		"RATIO":      "3.14",
		"SPACED":     "trailing space ",
		"PLAIN_NULL": "", // plain null still coerces per NullAsEmpty
	}
	for key, expected := range want {
		if got := result[key]; got != expected {
			t.Errorf("result[%q] = %q, want %q", key, got, expected)
		}
	}
}

// TestEstimateLeafCount pins the one-level-deep sizing heuristic used to
// pre-allocate the result map.
func TestEstimateLeafCount(t *testing.T) {
	tests := []struct {
		name string
		v    *Value
		want int
	}{
		{"nil", nil, 0},
		{"scalar", NewScalarValue("x", 1, 1), 1},
		{
			"map counts entries",
			&Value{Type: ValueTypeMap, Map: map[string]*Value{
				"A": NewScalarValue("1", 1, 1),
				"B": NewScalarValue("2", 1, 1),
				"C": NewScalarValue("3", 1, 1),
			}},
			3,
		},
		{
			"array counts elements",
			&Value{Type: ValueTypeArray, Array: []*Value{
				NewScalarValue("1", 1, 1),
				NewScalarValue("2", 1, 1),
			}},
			2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateLeafCount(tt.v); got != tt.want {
				t.Errorf("estimateLeafCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestLooksLikeNumber pins the cheap numeric pre-filter: it must accept
// anything strconv can parse (digits, sign, exponent, dot) and reject
// everything else without allocating.
func TestLooksLikeNumber(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"+", false},
		{"-", false},
		{".", false},
		{"abc", false},
		{"1a", false},
		{"0x1F", false},
		{"1_000", false},
		{"0", true},
		{"42", true},
		{"-42", true},
		{"+1.5", true},
		{"1.5e3", true},
		{"-2E-2", true},
		// "e5" passes the pre-filter (charset + digit) but is rejected by
		// strconv downstream — the filter only guarantees "cannot be a number"
		// rejections, not full validation.
		{"e5", true},
	}

	for _, tt := range tests {
		if got := looksLikeNumber(tt.s); got != tt.want {
			t.Errorf("looksLikeNumber(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

// TestFlattenDecodedYAML_UnsupportedType pins the YAML-side error
// constructor injected into the shared decoded-JSON walker: a type
// json.Unmarshal never produces must fail closed as a *YAMLError carrying
// the yamlUnsupportedError message. (The shared walker's default branch
// itself is covered for both formats by json_flatten_test.go's
// TestFlattenValue_UnsupportedType. Calls flattenDecoded directly — the
// former flattenInlineValue wrapper was production-dead and removed.)
func TestFlattenDecodedYAML_UnsupportedType(t *testing.T) {
	cfg := defaultFlattenCfg()
	err := flattenDecoded(42, "K", decodedFlattenOpts{
		maxDepth:       cfg.MaxDepth,
		nullAsEmpty:    cfg.NullAsEmpty,
		numberAsString: cfg.NumberAsString,
		delim:          cfg.KeyDelimiter,
		indexFmt:       cfg.ArrayIndexFormat,
	}, map[string]string{}, 0, setFlatYAMLValue, yamlDepthError, yamlUnsupportedError)
	if err == nil {
		t.Fatal("flattenDecoded(42) error = nil, want error")
	}
	var yamlErr *YAMLError
	if !errors.As(err, &yamlErr) {
		t.Fatalf("flattenDecoded(42) error = %T, want *YAMLError", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "unsupported decoded JSON type") {
		t.Errorf("error = %q, want the yamlUnsupportedError message", msg)
	}
}

// ============================================================================
// yamlFlowToJSON Tests
// ============================================================================

// TestYAMLFlowToJSON drives the YAML flow-collection → JSON converter directly,
// exercising branches that the full-pipeline TestFlattenYAML_InlineJSON only
// reaches indirectly: quoted-string escapes (\u, \t), YAML-only escapes that
// must fail the conversion (\x, \U, \0), unterminated quotes, the comment/space
// interactions in scanPlain (isFlowSpace), parseHex4, and the depth guard.
func TestYAMLFlowToJSON(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string // expected JSON when wantOK is true
		wantOK bool
	}{
		{
			name:   "plain scalars become JSON strings",
			input:  `[a, b]`,
			want:   `["a","b"]`,
			wantOK: true,
		},
		{
			name:   "plain map with number value",
			input:  `{x: 1}`,
			want:   `{"x":1}`,
			wantOK: true,
		},
		{
			name:   "null variants canonicalize",
			input:  `[~, null, Null, NULL]`,
			want:   `[null,null,null,null]`,
			wantOK: true,
		},
		{
			name:   "bool variants canonicalize case-insensitively",
			input:  `[TRUE, False, true, false]`,
			want:   `[true,false,true,false]`,
			wantOK: true,
		},
		{
			name:   "numbers canonicalize",
			input:  `[1e3, -5, 2.0, 1.5]`,
			want:   `[1000,-5,2,1.5]`,
			wantOK: true,
		},
		{
			// Inf/NaN/overflowing floats never emit non-JSON literals: the
			// pre-filter or the ParseFloat range error keeps them strings.
			name:   "Inf NaN and overflow stay strings",
			input:  `[Inf, NaN, 1e999]`,
			want:   `["Inf","NaN","1e999"]`,
			wantOK: true,
		},
		{
			name:   "empty flow element is an empty string",
			input:  `[a,,b]`,
			want:   `["a","","b"]`,
			wantOK: true,
		},
		{
			name:   "trailing comma in array is tolerated",
			input:  `[a, b,]`,
			want:   `["a","b"]`,
			wantOK: true,
		},
		{
			name:   "empty array",
			input:  `[]`,
			want:   `[]`,
			wantOK: true,
		},
		{
			name:   "empty map",
			input:  `{}`,
			want:   `{}`,
			wantOK: true,
		},
		{
			name:   "nested empty collection",
			input:  `[{}]`,
			want:   `[{}]`,
			wantOK: true,
		},
		{
			name:   "array of objects",
			input:  `[{a: 1}]`,
			want:   `[{"a":1}]`,
			wantOK: true,
		},
		{
			name:   "deeply nested flow",
			input:  `{a: [b, {c: 'd'}]}`,
			want:   `{"a":["b",{"c":"d"}]}`,
			wantOK: true,
		},
		{
			name:   "double-quoted key",
			input:  `{"a b": 1}`,
			want:   `{"a b":1}`,
			wantOK: true,
		},
		{
			name:   "single-quoted key",
			input:  `{'a': 1}`,
			want:   `{"a":1}`,
			wantOK: true,
		},
		{
			name:   "plain key with dash",
			input:  `{a-b: 1}`,
			want:   `{"a-b":1}`,
			wantOK: true,
		},
		{
			// \uXXXX decodes through parseHex4's three nibble arms (digits,
			// lowercase, uppercase) and re-encodes as UTF-8.
			name:   "unicode escape decodes (hex digits)",
			input:  `["A"]`,
			want:   `["A"]`,
			wantOK: true,
		},
		{
			name:   "unicode escape decodes (lowercase hex)",
			input:  `["é"]`,
			want:   `["é"]`,
			wantOK: true,
		},
		{
			name:   "unicode escape decodes (uppercase hex)",
			input:  `["Z"]`,
			want:   `["Z"]`,
			wantOK: true,
		},
		{
			// EOF in the middle of a \u escape (length check fails before
			// any nibble is read).
			name:   "escape truncated at end of input fails conversion",
			input:  `["a\u12`,
			wantOK: false,
		},
		{
			name:   "tab escape round-trips",
			input:  `["\t"]`,
			want:   `["\t"]`,
			wantOK: true,
		},
		{
			name:   "single-quoted doubled quote decodes",
			input:  `['it''s']`,
			want:   `["it's"]`,
			wantOK: true,
		},
		{
			name:   "colon inside scalar survives",
			input:  `[http://example.com, y]`,
			want:   `["http://example.com","y"]`,
			wantOK: true,
		},
		{
			name:   "hash without preceding space is content",
			input:  `[a#c]`,
			want:   `["a#c"]`,
			wantOK: true,
		},
		{
			// scanPlain stops at a '#' preceded by whitespace (isFlowSpace
			// true branch); skipSpace then treats the '#' as a comment to end
			// of input, so the collection never closes → conversion fails.
			name:   "hash after whitespace starts a comment and fails conversion",
			input:  `[a #c]`,
			wantOK: false,
		},
		{
			name:   "YAML-only hex escape fails conversion",
			input:  `["\x41"]`,
			wantOK: false,
		},
		{
			name:   "YAML-only unicode escape fails conversion",
			input:  `["\U00000041"]`,
			wantOK: false,
		},
		{
			name:   "YAML-only octal escape fails conversion",
			input:  `["\0"]`,
			wantOK: false,
		},
		{
			name:   "truncated unicode escape fails conversion",
			input:  `["\u12"]`,
			wantOK: false,
		},
		{
			name:   "unterminated double quote fails conversion",
			input:  `["unterminated]`,
			wantOK: false,
		},
		{
			name:   "unterminated single quote fails conversion",
			input:  `['unterminated]`,
			wantOK: false,
		},
		{
			name:   "garbage after quoted value fails conversion",
			input:  `["a"b]`,
			wantOK: false,
		},
		{
			name:   "trailing garbage fails conversion",
			input:  `[a] x`,
			wantOK: false,
		},
		{
			name:   "colon without following space fails conversion",
			input:  `{x:1}`,
			wantOK: false,
		},
		{
			name:   "unterminated sequence fails conversion",
			input:  `[a`,
			wantOK: false,
		},
		{
			name:   "empty input fails conversion",
			input:  ``,
			wantOK: false,
		},
		{
			name:   "whitespace-only input fails conversion",
			input:  `   `,
			wantOK: false,
		},
		{
			name:   "depth beyond maxFlowDepth fails conversion",
			input:  strings.Repeat("[", maxFlowDepth+50),
			wantOK: false,
		},
		{
			// parseMap branches: multiple entries, trailing comma (tolerated,
			// not emitted), and the failure arms — garbage after a value,
			// comma then EOF, an unterminated quoted key, and EOF after the
			// colon.
			name:   "flow map with multiple entries",
			input:  `{a: 1, b: 2}`,
			want:   `{"a":1,"b":2}`,
			wantOK: true,
		},
		{
			name:   "flow map trailing comma is tolerated",
			input:  `{a: 1,}`,
			want:   `{"a":1}`,
			wantOK: true,
		},
		{
			// A flow-closing bracket where ',' or '}' must appear ends the
			// map (a space inside a value is legal plain-scalar content —
			// "{a: 1 x}" legitimately parses as the scalar "1 x").
			name:   "flow map value followed by flow bracket fails conversion",
			input:  `{a: 1]`,
			wantOK: false,
		},
		{
			name:   "flow map comma then end of input fails conversion",
			input:  `{a: 1,`,
			wantOK: false,
		},
		{
			name:   "unterminated quoted flow-map key fails conversion",
			input:  `{'unterminated}`,
			wantOK: false,
		},
		{
			name:   "end of input after flow-map colon fails conversion",
			input:  `{a:`,
			wantOK: false,
		},
		{
			// A comma directly after the colon reads as an empty scalar
			// value (YAML flow null), so the entry round-trips as "".
			name:   "flow map empty value before comma",
			input:  `{a:,b: 1}`,
			want:   `{"a":"","b":1}`,
			wantOK: true,
		},
		{
			name:   "invalid hex nibble fails conversion",
			input:  `["\u00G1"]`,
			wantOK: false,
		},
		{
			// A raw control byte inside a plain scalar must be escaped as
			// \u00XX by appendJSONString (the c < 0x20 branch).
			name:   "control byte in plain scalar is escaped",
			input:  "[a\x01b]",
			want:   `["a\u0001b"]`,
			wantOK: true,
		},
		{
			name:   "newline escape decodes",
			input:  `["\n"]`,
			want:   `["\n"]`,
			wantOK: true,
		},
		{
			name:   "carriage-return escape decodes",
			input:  `["\r"]`,
			want:   `["\r"]`,
			wantOK: true,
		},
		{
			name:   "backslash and quote escapes decode",
			input:  `["a\\b\"c"]`,
			want:   `["a\\b\"c"]`,
			wantOK: true,
		},
		{
			name:   "solidus escape decodes",
			input:  `["a\/b"]`,
			want:   `["a/b"]`,
			wantOK: true,
		},
		{
			// appendJSONString has dedicated escapes for \n \r \t \" \\ but not
			// \b \f: those land in the c < 0x20 branch and become \u00XX.
			name:   "backspace and formfeed escapes re-escape as unicode",
			input:  `["\b\f"]`,
			want:   `["\u0008\u000c"]`,
			wantOK: true,
		},
		{
			// A plain scalar after a colon absorbs a following space-separated
			// word (no flow indicator terminates it), so "1 b" is one value.
			name:   "space-separated words after colon stay one plain scalar",
			input:  `{a: 1 b}`,
			want:   `{"a":"1 b"}`,
			wantOK: true,
		},
		{
			// An empty value after the colon scans as the empty plain scalar,
			// rendered as the empty JSON string.
			name:   "empty map value becomes empty string",
			input:  `{a:}`,
			want:   `{"a":""}`,
			wantOK: true,
		},
		{
			name:   "map missing colon fails conversion",
			input:  `{a 1}`,
			wantOK: false,
		},
		{
			name:   "unterminated map value fails conversion",
			input:  `{a: [1,`,
			wantOK: false,
		},
		{
			name:   "unterminated nested map fails conversion",
			input:  `{a: {b: 1}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := yamlFlowToJSON(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("yamlFlowToJSON(%q) ok = %v, want %v (got %q)", tt.input, ok, tt.wantOK, got)
			}
			if !tt.wantOK {
				return
			}
			if got != tt.want {
				t.Errorf("yamlFlowToJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
