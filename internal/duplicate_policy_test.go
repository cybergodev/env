package internal

import (
	"errors"
	"strings"
	"testing"
)

// ============================================================================
// YAML Parser Duplicate-Key Policy
// ============================================================================

// TestParseYAMLWithPolicy_DuplicateKeys pins the parser-level duplicate-key
// error. The full policy-outcome matrix (inherit/first/last winning values
// for this same document) is covered loader-level by the root package's
// TestDuplicateKeyPolicy_YAML (duplicate_key_test.go); the parser-level
// unique value here is the YAMLError line number of the duplicate.
func TestParseYAMLWithPolicy_DuplicateKeys(t *testing.T) {
	const doc = "KEY: first\nKEY: second\n"

	tests := []struct {
		name     string
		policy   DuplicateKeyPolicy
		wantLine int
	}{
		{name: "error reports duplicate at its line", policy: DuplicateKeyError, wantLine: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := ParseYAMLWithPolicy([]byte(doc), 10, tt.policy)
			if err == nil {
				t.Fatal("ParseYAMLWithPolicy() error = nil, want duplicate-key error")
			}
			var yamlErr *YAMLError
			if !errors.As(err, &yamlErr) {
				t.Fatalf("error %v is not *YAMLError", err)
			}
			if !errors.Is(err, ErrDuplicateKey) {
				t.Errorf("error %v does not match ErrDuplicateKey via errors.Is", err)
			}
			if yamlErr.Line != tt.wantLine {
				t.Errorf("YAMLError.Line = %d, want %d (line of the duplicate)", yamlErr.Line, tt.wantLine)
			}
			ReleaseValue(value)
		})
	}
}

// TestParseYAMLWithPolicy_NestedDuplicate ensures the policy applies at any
// mapping depth, not just the document root, and that the reported line is
// the duplicate occurrence's own line.
func TestParseYAMLWithPolicy_NestedDuplicate(t *testing.T) {
	const doc = "outer:\n  inner: one\n  inner: two\n"

	value, err := ParseYAMLWithPolicy([]byte(doc), 10, DuplicateKeyError)
	if err == nil {
		ReleaseValue(value)
		t.Fatal("ParseYAMLWithPolicy() error = nil, want duplicate-key error at nested level")
	}
	var yamlErr *YAMLError
	if !errors.As(err, &yamlErr) {
		t.Fatalf("error %v is not *YAMLError", err)
	}
	if !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("error %v does not match ErrDuplicateKey via errors.Is", err)
	}
	if yamlErr.Line != 3 {
		t.Errorf("YAMLError.Line = %d, want 3 (line of the nested duplicate)", yamlErr.Line)
	}
}

// TestParseYAMLWithPolicy_FirstWinsReleasesTree guards the pool hygiene of
// the FirstWins branch: the discarded duplicate subtree is returned to the
// value pool (a smoke test — pool internals are not observable, so this
// mainly pins that the kept value survives intact).
func TestParseYAMLWithPolicy_FirstWinsKeepsTree(t *testing.T) {
	const doc = "a: 1\nb:\n  c: 2\na: 9\n"

	value, err := ParseYAMLWithPolicy([]byte(doc), 10, DuplicateKeyFirstWins)
	if err != nil {
		t.Fatalf("ParseYAMLWithPolicy() error = %v", err)
	}
	defer ReleaseValue(value)
	if value.Map["a"].Scalar != "1" {
		t.Errorf("a = %q, want %q (first occurrence)", value.Map["a"].Scalar, "1")
	}
	if value.Map["b"].Map["c"].Scalar != "2" {
		t.Errorf("b.c = %q, want %q (sibling tree unaffected)", value.Map["b"].Map["c"].Scalar, "2")
	}
}

// ============================================================================
// Flatten Collision Detection (F2)
// ============================================================================
// Collision errors for different source paths (nested vs literal, array index
// vs literal, case-only difference) are covered with the full policy dimension
// by the root package's TestFlattenCollision_YAML / TestFlattenCollision_JSON
// (duplicate_key_test.go). The parser-level tests below keep the shapes that
// must NOT trip the collision check.

// TestFlattenYAMLNoFalseCollision guards the common shapes that must NOT
// trip the collision check: ordinary nesting, arrays, empty containers and
// repeated flattening of disjoint subtrees.
func TestFlattenYAMLNoFalseCollision(t *testing.T) {
	docs := []string{
		"a:\n  b: 1\nc_d: 2\n", // disjoint names — no overlap
		"a:\n  - 1\n  - 2\n",   // array only
		"a: {}\n",              // empty map alone
		"a: []\n",              // empty array alone
		"root:\n  child:\n    leaf: v\n",
	}
	for _, doc := range docs {
		value, err := ParseYAML([]byte(doc), 10)
		if err != nil {
			t.Fatalf("ParseYAML(%q) error = %v", doc, err)
		}
		if _, err := FlattenYAML(value, defaultFlattenCfg()); err != nil {
			ReleaseValue(value)
			t.Errorf("FlattenYAML(%q) error = %v, want nil", doc, err)
		}
		ReleaseValue(value)
	}
}

// ============================================================================
// JSON Duplicate-Key Pre-Scan (detector)
// ============================================================================

// TestDetectJSONDuplicateKeys covers the conservative structural detector:
// true positives at any depth, and — critically — no false positives on
// structural characters inside strings, escapes, or malformed input.
func TestDetectJSONDuplicateKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKey  string
		wantFind bool
	}{
		{name: "no duplicates nested", input: `{"a": {"b": 1}, "c": [1, 2, {"d": 3}]}`},
		{name: "top level duplicate", input: `{"a": 1, "a": 2}`, wantKey: "a", wantFind: true},
		{name: "nested object duplicate", input: `{"x": 0, "o": {"k": 1, "k": 2}}`, wantKey: "k", wantFind: true},
		{name: "duplicate inside array element", input: `[{"k": 1}, {"k": 1, "k": 2}]`, wantKey: "k", wantFind: true},
		{name: "same key in sibling objects is not a duplicate", input: `{"l": {"k": 1}, "r": {"k": 2}}`},
		{name: "structural chars in string values", input: `{"a": "{,}[:]\"", "b": "[1,2]"}`},
		{name: "escaped quote inside value", input: `{"a": "x\"y, \"z\": 1", "b": 2}`},
		{name: "escaped backslash then quote", input: `{"a": "x\\", "b": "y\""}`},
		// Keys are compared DECODED (see decodeKeyEscapes): two spellings
		// that decode to the same key - a plain letter next to its
		// unicode-escaped form - ARE detected as duplicates, matching what
		// encoding/json last-wins decode would silently collapse.
		{name: "escaped key equals raw key detected", input: `{"a": 1, "\u0061": 2}`, wantKey: "a", wantFind: true},
		{name: "malformed key escape still scans (raw fallback)", input: `{"a": 1, "\q": 2}`},
		{name: "whitespace variants", input: "\t{\n  \"a\"  :  1  ,\n  \"a\" : 2\n}  ", wantKey: "a", wantFind: true},
		{name: "empty object and array", input: `{"e": {}, "a": []}`},
		{name: "scalars at top level", input: `"just a string"`},
		{name: "number top level", input: `42`},
		{name: "malformed missing colon", input: `{"a" 1}`},
		{name: "malformed trailing comma", input: `{"a": 1,}`},
		{name: "malformed unterminated string", input: `{"a": "x`},
		{name: "malformed trailing garbage", input: `{"a": 1} extra`},
		{name: "malformed duplicate then garbage", input: `{"a": 1, "a": 2} trailing`, wantKey: "a", wantFind: true},
		{name: "empty input", input: ``},
		// Remaining malformed shapes: the failure arms of the array/object
		// loops and the backslash-at-end-of-key arm of scanString.
		{name: "malformed escape at end of key", input: `{"a\`},
		{name: "malformed end of input after object value", input: `{"a": 1`},
		{name: "malformed array bracket after object value", input: `{"a": 1]`},
		{name: "malformed unterminated array string", input: `["a`},
		{name: "malformed array comma then end of input", input: `["a",`},
		{name: "malformed missing array separator", input: `[1 2]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, found := detectJSONDuplicateKeys([]byte(tt.input), 100)
			if found != tt.wantFind {
				t.Fatalf("detectJSONDuplicateKeys(%q) = (%q, %v), want find=%v", tt.input, key, found, tt.wantFind)
			}
			if found && key != tt.wantKey {
				t.Errorf("duplicated key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

// TestDetectJSONDuplicateKeys_DepthCap verifies the defensive recursion
// guard: nesting beyond maxDepth aborts the scan (reporting no duplicate)
// instead of recursing without bound.
func TestDetectJSONDuplicateKeys_DepthCap(t *testing.T) {
	deep := `{"a":` + strings.Repeat(`{"b":`, 50) + `1` + strings.Repeat(`}`, 50) + `}`
	if _, found := detectJSONDuplicateKeys([]byte(deep), 10); found {
		t.Fatal("detectJSONDuplicateKeys() found duplicate beyond depth cap, want abort")
	}
	// Same document within the cap parses fine.
	if _, found := detectJSONDuplicateKeys([]byte(deep), 100); found {
		t.Fatal("detectJSONDuplicateKeys() false positive on deep distinct-key document")
	}
}

// ============================================================================
// DuplicateKeyPolicy.String
// ============================================================================

// TestDuplicateKeyPolicy_String covers String for every policy plus the
// unknown-value fallback.
func TestDuplicateKeyPolicy_String(t *testing.T) {
	tests := []struct {
		policy DuplicateKeyPolicy
		want   string
	}{
		{DuplicateKeyInherit, "inherit"},
		{DuplicateKeyFirstWins, "first-wins"},
		{DuplicateKeyLastWins, "last-wins"},
		{DuplicateKeyError, "error"},
		{DuplicateKeyPolicy(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.policy.String(); got != tt.want {
			t.Errorf("DuplicateKeyPolicy(%d).String() = %q, want %q", int(tt.policy), got, tt.want)
		}
	}
}

// TestFlattenInlineJSONDuplicatePolicy pins the duplicate-key policy wiring
// for inline JSON inside YAML scalars (flattenInlineJSON). The root-level
// matrix (duplicate_key_test.go) reaches this path only through the public
// API; these rows keep it attributed to this package's own coverage.
func TestFlattenInlineJSONDuplicatePolicy(t *testing.T) {
	// Inline JSON is only parsed for scalars under a key (a bare root
	// scalar is dropped), so each document nests the JSON under "conf".
	dupDoc := func() *Value {
		return &Value{
			Type: ValueTypeMap,
			Map: map[string]*Value{
				"conf": {
					Type:   ValueTypeScalar,
					Scalar: `{"KEY": "first", "KEY": "second"}`,
					Line:   1, Column: 1,
				},
			},
		}
	}
	cleanDoc := func() *Value {
		return &Value{
			Type: ValueTypeMap,
			Map: map[string]*Value{
				"conf": {
					Type:   ValueTypeScalar,
					Scalar: `{"KEY": "first", "OTHER": "x"}`,
					Line:   1, Column: 1,
				},
			},
		}
	}

	tests := []struct {
		name      string
		policy    DuplicateKeyPolicy
		doc       func() *Value
		wantValue string // expected KEY value on success
		wantDup   bool   // expect an error matching ErrDuplicateKey
	}{
		{name: "error policy rejects inline duplicates", policy: DuplicateKeyError, doc: dupDoc, wantDup: true},
		{name: "first-wins policy rejects inline duplicates", policy: DuplicateKeyFirstWins, doc: dupDoc, wantDup: true},
		{name: "last-wins policy keeps last", policy: DuplicateKeyLastWins, doc: dupDoc, wantValue: "second"},
		{name: "inherit keeps last", policy: DuplicateKeyInherit, doc: dupDoc, wantValue: "second"},
		{name: "first-wins accepts clean inline JSON", policy: DuplicateKeyFirstWins, doc: cleanDoc, wantValue: "first"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultFlattenCfg()
			cfg.DuplicateKey = tt.policy
			result, err := FlattenYAML(tt.doc(), cfg)
			if tt.wantDup {
				if err == nil {
					t.Fatalf("FlattenYAML() error = nil, want ErrDuplicateKey")
				}
				if !errors.Is(err, ErrDuplicateKey) {
					t.Errorf("FlattenYAML() error = %v, want ErrDuplicateKey chain", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FlattenYAML() error = %v, want nil", err)
			}
			if got := result["CONF_KEY"]; got != tt.wantValue {
				t.Errorf("CONF_KEY = %q, want %q", got, tt.wantValue)
			}
		})
	}
}
