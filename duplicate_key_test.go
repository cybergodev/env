package env

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

// ============================================================================
// Duplicate-Key Policy Tests (F1) and Flat-Key Collision Tests (F2)
// ============================================================================

// newDupTestLoader builds a loader over the in-memory file system with the
// given policy and file contents, mirroring TestParser_BoundaryConditions.
func newDupTestLoader(t *testing.T, policy DuplicateKeyPolicy, files map[string]string) *Loader {
	t.Helper()
	fs := newTestFileSystem()
	maps.Copy(fs.files, files)
	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = nil // don't auto-load
	cfg.DuplicateKeyPolicy = policy
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return loader
}

// rmDupCase is one row of the merged duplicate-key policy table. It covers
// every format (chosen via filename + content): .env, YAML, JSON, and inline
// JSON inside YAML.
type rmDupCase struct {
	name     string
	filename string
	policy   DuplicateKeyPolicy
	// overwrite sets OverwriteExisting; only the Inherit policy consults it.
	overwrite bool
	content   string
	// wantKey is the key read back on success; "" means "KEY".
	wantKey   string
	wantValue string
	// wantErrIs / wantErrText describe the expected failure; either being
	// set marks the row as an error row.
	wantErrIs   error
	wantErrText string
	// wantParseLine, when > 0, asserts the .env duplicate error is a
	// *ParseError pointing at the line of the duplicate occurrence.
	wantParseLine int
}

// rmRunDupCase executes one rmDupCase through the public LoadFiles API.
func rmRunDupCase(t *testing.T, tt rmDupCase) {
	t.Helper()
	fs := newTestFileSystem()
	fs.files[tt.filename] = tt.content
	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = nil
	cfg.DuplicateKeyPolicy = tt.policy
	cfg.OverwriteExisting = tt.overwrite

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }()

	err = loader.LoadFiles(tt.filename)
	if tt.wantErrIs != nil || tt.wantErrText != "" {
		if err == nil {
			t.Fatal("LoadFiles() error = nil, want error")
		}
		if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
			t.Errorf("LoadFiles() error = %v, want errors.Is(err, %v)", err, tt.wantErrIs)
		}
		if tt.wantErrText != "" && !strings.Contains(err.Error(), tt.wantErrText) {
			t.Errorf("LoadFiles() error = %v, want containing %q", err, tt.wantErrText)
		}
		if tt.wantParseLine > 0 {
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("LoadFiles() error = %T (%v), want *ParseError", err, err)
			}
			if pe.Line != tt.wantParseLine {
				t.Errorf("ParseError.Line = %d, want %d (line of the duplicate occurrence)", pe.Line, tt.wantParseLine)
			}
		}
		return
	}
	if err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}
	key := tt.wantKey
	if key == "" {
		key = "KEY"
	}
	if v := loader.GetString(key); v != tt.wantValue {
		t.Errorf("%s = %q, want %q", key, v, tt.wantValue)
	}
}

// TestDuplicateKeyPolicy exercises the duplicate-key policy across all four
// formats in one table.
//
// .env: Inherit follows OverwriteExisting (first wins when false, last when
// true); the explicit policies ignore OverwriteExisting.
//
// YAML: only the Inherit x OverwriteExisting interaction row and one error
// row are kept — the plain first/last/error matrix is byte-equivalent to the
// parser-level matrix in internal/duplicate_policy_test.go
// (TestParseYAMLWithPolicy_DuplicateKeys). The interaction row pins that YAML
// Inherit keeps the last occurrence regardless of OverwriteExisting, which
// the internal matrix (no overwrite dimension) cannot express.
//
// JSON: FirstWins is unsupported and rejected instead of silently violated.
//
// Inline JSON: the policy reaches JSON values embedded in YAML, and
// looks-like-JSON scalars keep their plain-scalar fallback under FirstWins.
func TestDuplicateKeyPolicy(t *testing.T) {
	const envDoc = "KEY=first\nKEY=second\n"
	const yamlDoc = "KEY: first\nKEY: second\n"

	tests := []rmDupCase{
		// --- .env ---
		{name: "env inherit keeps first when overwrite disabled",
			filename: "test.env", policy: DuplicateKeyInherit, overwrite: false,
			content: envDoc, wantValue: "first"},
		{name: "env inherit keeps last when overwrite enabled",
			filename: "test.env", policy: DuplicateKeyInherit, overwrite: true,
			content: envDoc, wantValue: "second"},
		{name: "env first wins ignores overwrite",
			filename: "test.env", policy: DuplicateKeyFirstWins, overwrite: true,
			content: envDoc, wantValue: "first"},
		{name: "env last wins ignores overwrite",
			filename: "test.env", policy: DuplicateKeyLastWins, overwrite: false,
			content: envDoc, wantValue: "second"},
		// The .env duplicate error must be a *ParseError at the line of the
		// second occurrence (parser.go records lineNum at the duplicate).
		// The internal YAML matrix asserts YAMLError.Line; this is the .env
		// counterpart.
		{name: "env error policy reports duplicate at line 2",
			filename: "test.env", policy: DuplicateKeyError,
			content: envDoc, wantErrIs: ErrDuplicateKey, wantParseLine: 2},

		// --- YAML (see the comment above for what is and is not repeated) ---
		{name: "yaml inherit keeps last even with overwrite enabled",
			filename: "test.yaml", policy: DuplicateKeyInherit, overwrite: true,
			content: yamlDoc, wantValue: "second"},
		{name: "yaml error policy reports duplicate",
			filename: "test.yaml", policy: DuplicateKeyError,
			content: yamlDoc, wantErrIs: ErrDuplicateKey},

		// --- JSON ---
		{name: "json inherit keeps last",
			filename: "test.json", policy: DuplicateKeyInherit,
			content: `{"KEY": "first", "KEY": "second"}`, wantValue: "second"},
		{name: "json last wins",
			filename: "test.json", policy: DuplicateKeyLastWins,
			content: `{"KEY": "first", "KEY": "second"}`, wantValue: "second"},
		{name: "json error on nested duplicate",
			filename: "test.json", policy: DuplicateKeyError,
			content:   `{"outer": {"KEY": "first", "KEY": "second"}}`,
			wantErrIs: ErrDuplicateKey, wantErrText: "duplicate object key"},
		{name: "json first wins rejects duplicates",
			filename: "test.json", policy: DuplicateKeyFirstWins,
			content:   `{"KEY": "first", "KEY": "second"}`,
			wantErrIs: ErrDuplicateKey, wantErrText: "cannot be resolved"},
		// Without duplicates the resolution order is irrelevant — a clean
		// document must load under every policy, FirstWins included (re-review R-1).
		{name: "json first wins accepts clean document",
			filename: "test.json", policy: DuplicateKeyFirstWins,
			content: `{"KEY": "single"}`, wantValue: "single"},
		{name: "json error accepts distinct keys",
			filename: "test.json", policy: DuplicateKeyError,
			content: `{"A": "1", "B": {"C": "2"}}`, wantKey: "B_C", wantValue: "2"},

		// --- inline JSON inside YAML ---
		// The inline object's keys flatten under KEY (KEY_K), and the
		// duplicate k resolves to its last occurrence.
		{name: "inline json inherit keeps last in inline object",
			filename: "test.yaml", policy: DuplicateKeyInherit,
			content: "KEY: {\"k\": \"first\", \"k\": \"second\"}\n", wantKey: "KEY_K", wantValue: "second"},
		{name: "inline json error detects duplicate in inline object",
			filename: "test.yaml", policy: DuplicateKeyError,
			content: "KEY: {\"k\": \"first\", \"k\": \"second\"}\n", wantErrIs: ErrDuplicateKey},
		// Clean inline JSON resolves identically under every policy and must
		// load under FirstWins too (re-review R-1).
		{name: "inline json first wins accepts clean inline document",
			filename: "test.yaml", policy: DuplicateKeyFirstWins,
			content: "KEY: {\"a\": 1, \"b\": 2}\n", wantKey: "KEY_A", wantValue: "1"},
		{name: "inline json error accepts clean inline document",
			filename: "test.yaml", policy: DuplicateKeyError,
			content: "KEY: {\"a\": 1, \"b\": 2}\n", wantKey: "KEY_A", wantValue: "1"},
		{name: "inline json first wins rejects duplicates",
			filename: "test.yaml", policy: DuplicateKeyFirstWins,
			content:   "KEY: {\"k\": \"first\", \"k\": \"second\"}\n",
			wantErrIs: ErrDuplicateKey, wantErrText: "cannot be resolved"},
		// A scalar that merely looks like a collection but fails to parse as
		// both strict JSON and YAML flow keeps the lenient plain-scalar
		// fallback under every policy. ("[not json at all]" now parses as a
		// one-element YAML flow sequence, so it no longer qualifies.)
		{name: "inline json looks-like-JSON scalar stays plain under first wins",
			filename: "test.yaml", policy: DuplicateKeyFirstWins,
			content: "KEY: [not json, 'at all\n", wantValue: "[not json, 'at all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rmRunDupCase(t, tt)
		})
	}
}

// TestFlattenCollision verifies that two different source paths collapsing
// onto the same flattened key are rejected deterministically (previously the
// winner followed map iteration order), across both YAML and JSON documents.
// The collision check lives in the flatteners and is policy-independent, so
// two representative policies (the default and the strictest) cover it.
func TestFlattenCollision(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  string
	}{
		{name: "yaml nested key vs literal underscore key", filename: "test.yaml", content: "a_b: 1\na:\n  b: 2\n"},
		{name: "yaml array index vs literal index key", filename: "test.yaml", content: "a_0: x\na:\n  - y\n"},
		{name: "yaml case-only difference", filename: "test.yaml", content: "foo: 1\nFOO: 2\n"},
		{name: "json nested key vs literal underscore key", filename: "test.json", content: `{"a_b": 1, "a": {"b": 2}}`},
		{name: "json uppercase nested vs lowercase literal", filename: "test.json", content: `{"A": {"B": 2}, "a_b": 1}`},
		{name: "json array index vs literal index key", filename: "test.json", content: `{"a_0": "x", "a": ["y"]}`},
	}

	for _, tt := range tests {
		for _, policy := range []DuplicateKeyPolicy{DuplicateKeyInherit, DuplicateKeyError} {
			t.Run(tt.name+"/"+policy.String(), func(t *testing.T) {
				loader := newDupTestLoader(t, policy, map[string]string{tt.filename: tt.content})
				defer func() { _ = loader.Close() }() // best-effort cleanup

				err := loader.LoadFiles(tt.filename)
				if err == nil {
					t.Fatal("LoadFiles() error = nil, want flat-key collision error")
				}
				if !errors.Is(err, ErrDuplicateKey) {
					t.Errorf("LoadFiles() error = %v, want errors.Is(err, ErrDuplicateKey)", err)
				}
			})
		}
	}
}

// TestCrossFileMergeUnchanged guards the cross-file semantics the policy
// must not touch: file order + OverwriteExisting, across mixed formats with
// a unified flattened key space.
func TestCrossFileMergeUnchanged(t *testing.T) {
	files := map[string]string{
		"a.yaml": "SHARED: from_yaml\nONLY_A: a\n",
		"b.env":  "SHARED=from_env\nONLY_B=b\n",
		"c.json": `{"SHARED": "from_json"}`,
	}

	t.Run("first file wins by default", func(t *testing.T) {
		loader := newDupTestLoader(t, DuplicateKeyInherit, files)
		defer func() { _ = loader.Close() }() // best-effort cleanup
		if err := loader.LoadFiles("a.yaml", "b.env", "c.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		if v := loader.GetString("SHARED"); v != "from_yaml" {
			t.Errorf("SHARED = %q, want %q", v, "from_yaml")
		}
		if v := loader.GetString("ONLY_B"); v != "b" {
			t.Errorf("ONLY_B = %q, want %q", v, "b")
		}
	})

	t.Run("later files override when overwrite enabled", func(t *testing.T) {
		fs := newTestFileSystem()
		maps.Copy(fs.files, files)
		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Filenames = nil
		cfg.OverwriteExisting = true

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.LoadFiles("a.yaml", "b.env", "c.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		if v := loader.GetString("SHARED"); v != "from_json" {
			t.Errorf("SHARED = %q, want %q", v, "from_json")
		}
	})

	t.Run("explicit policies do not affect cross-file merge", func(t *testing.T) {
		loader := newDupTestLoader(t, DuplicateKeyLastWins, files)
		defer func() { _ = loader.Close() }() // best-effort cleanup
		if err := loader.LoadFiles("a.yaml", "b.env", "c.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		if v := loader.GetString("SHARED"); v != "from_yaml" {
			t.Errorf("SHARED = %q, want %q (cross-file merge still first-file-wins)", v, "from_yaml")
		}
	})
}

// TestDuplicateKeyPolicyConfigValidation covers the enum validation and the
// IsZero interaction (a Config whose only non-default field is the policy
// must not be replaced by DefaultConfig).
func TestDuplicateKeyPolicyConfigValidation(t *testing.T) {
	t.Run("rejects unknown policy value", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuplicateKeyPolicy = DuplicateKeyPolicy(99)
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() error = nil, want enum rejection")
		}

		loader, err := New(cfg)
		if err == nil {
			_ = loader.Close() // best-effort cleanup
			t.Fatal("New() error = nil, want ErrInvalidConfig")
		}
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("New() error = %v, want errors.Is(err, ErrInvalidConfig)", err)
		}
	})

	t.Run("IsZero detects a non-inherit policy", func(t *testing.T) {
		cfg := Config{ParsingConfig: ParsingConfig{DuplicateKeyPolicy: DuplicateKeyError}}
		if cfg.IsZero() {
			t.Fatal("IsZero() = true for Config with only DuplicateKeyPolicy set; " +
				"New would silently replace it with DefaultConfig")
		}
	})

	t.Run("all four policies pass validation", func(t *testing.T) {
		for _, p := range []DuplicateKeyPolicy{DuplicateKeyInherit, DuplicateKeyFirstWins, DuplicateKeyLastWins, DuplicateKeyError} {
			cfg := DefaultConfig()
			cfg.DuplicateKeyPolicy = p
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() with policy %v error = %v", p, err)
			}
		}
	})
}
