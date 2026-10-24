package internal

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// ============================================================================
// YAML Parser Tests
// ============================================================================
//
// Coverage map for this file:
//   - TestYAMLParser_Structure: every parse-pipeline shape (table-driven,
//     through the public ParseYAML API).
//   - TestParseYAML / TestParseYAML_Complex: the real entry point.
//   - TestYAMLParser_TokenLevelBranches and the direct-construction tests
//     below: defensive branches the lexer never emits.
//
// Lexer-level behavior (quoting, escapes, colons-in-values, comment
// stripping, line endings, token counts) is pinned authoritatively in
// yaml_lexer_test.go and is deliberately not retested here.

// iyValueKeys returns the sorted keys of a parsed Value map for failure
// messages. (keysOf in yaml_flatten_test.go serves the same purpose for
// map[string]string and is shared with json_flatten_test.go.)
func iyValueKeys(m map[string]*Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestYAMLParser_Structure exercises the structural branches of parseMap,
// parseArray, and parseNestedValue through the full ParseYAML pipeline:
// basic nesting, arrays, comments before nested content, blank lines,
// dedents, array items at EOF, nested array/map combinations, the depth
// limit, and error transmission from the lexer.
func TestYAMLParser_Structure(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxDepth int // 0 → 10
		wantErr  bool
		check    func(t *testing.T, root *Value)
	}{
		{
			name:  "simple nested map",
			input: "database:\n  host: localhost\n  port: 5432",
			check: func(t *testing.T, root *Value) {
				db := root.Map["database"]
				if db == nil || db.Type != ValueTypeMap {
					t.Fatal("expected 'database' map")
				}
				if db.Map["host"].Scalar != "localhost" {
					t.Errorf("database.host = %q, want %q", db.Map["host"].Scalar, "localhost")
				}
				if db.Map["port"].Scalar != "5432" {
					t.Errorf("database.port = %q, want %q", db.Map["port"].Scalar, "5432")
				}
			},
		},
		{
			name:  "array of scalars",
			input: "items:\n  - one\n  - two\n  - three",
			check: func(t *testing.T, root *Value) {
				items := root.Map["items"]
				if items == nil || items.Type != ValueTypeArray {
					t.Fatal("expected 'items' array")
				}
				expected := []string{"one", "two", "three"}
				if len(items.Array) != len(expected) {
					t.Fatalf("items length = %d, want %d", len(items.Array), len(expected))
				}
				for i, exp := range expected {
					if items.Array[i].Scalar != exp {
						t.Errorf("items[%d] = %q, want %q", i, items.Array[i].Scalar, exp)
					}
				}
			},
		},
		{
			name:  "array of maps",
			input: "servers:\n  - host: server1\n    port: 8080\n  - host: server2\n    port: 9090",
			check: func(t *testing.T, root *Value) {
				servers := root.Map["servers"]
				if servers == nil || servers.Type != ValueTypeArray {
					t.Fatal("expected 'servers' array")
				}
				if len(servers.Array) != 2 {
					t.Fatalf("servers length = %d, want 2", len(servers.Array))
				}
				want := [2]struct{ host, port string }{{"server1", "8080"}, {"server2", "9090"}}
				for i, w := range want {
					srv := servers.Array[i]
					if srv.Type != ValueTypeMap {
						t.Fatalf("servers[%d] type = %v, want ValueTypeMap", i, srv.Type)
					}
					if srv.Map["host"].Scalar != w.host {
						t.Errorf("servers[%d].host = %q, want %q", i, srv.Map["host"].Scalar, w.host)
					}
					if srv.Map["port"].Scalar != w.port {
						t.Errorf("servers[%d].port = %q, want %q", i, srv.Map["port"].Scalar, w.port)
					}
				}
			},
		},
		{
			name:  "key with empty value",
			input: "key:",
			check: func(t *testing.T, root *Value) {
				if got := root.Map["key"]; got == nil || got.Scalar != "" {
					t.Errorf("key = %v, want an empty scalar", got)
				}
			},
		},
		{
			name:  "empty document yields empty root map",
			input: "",
			check: func(t *testing.T, root *Value) {
				if root.Type != ValueTypeMap {
					t.Errorf("result type = %v, want ValueTypeMap", root.Type)
				}
				if len(root.Map) != 0 {
					t.Errorf("result map length = %d, want 0", len(root.Map))
				}
			},
		},
		{
			name:  "document start then content",
			input: "---\nkey: value",
			check: func(t *testing.T, root *Value) {
				if root.Map["key"].Scalar != "value" {
					t.Errorf("key = %q, want %q", root.Map["key"].Scalar, "value")
				}
			},
		},
		{
			name:  "multiple sibling keys",
			input: "key1: value1\nkey2: value2\nkey3: value3",
			check: func(t *testing.T, root *Value) {
				if len(root.Map) != 3 {
					t.Fatalf("result map length = %d, want 3 (keys: %v)", len(root.Map), iyValueKeys(root.Map))
				}
				expected := map[string]string{
					"key1": "value1",
					"key2": "value2",
					"key3": "value3",
				}
				for key, exp := range expected {
					if root.Map[key].Scalar != exp {
						t.Errorf("result[%q] = %q, want %q", key, root.Map[key].Scalar, exp)
					}
				}
			},
		},
		{
			// Parser-level quoting pin: the escape is decoded by the lexer
			// (pinned in yaml_lexer_test.go) and scalarFromToken must copy
			// the token's IsQuoted flag onto the Value — quoted scalars are
			// exempt from null/bool/number coercion downstream.
			name:  "quoted value keeps escapes and the quoting flag",
			input: `key: "line1\nline2"`,
			check: func(t *testing.T, root *Value) {
				v := root.Map["key"]
				if v == nil || v.Type != ValueTypeScalar || !v.Quoted {
					t.Fatalf("key = %v, want a quoted scalar", v)
				}
				if v.Scalar != "line1\nline2" {
					t.Errorf("key = %q, want %q", v.Scalar, "line1\nline2")
				}
			},
		},
		{
			// PIN (column-based nesting): one space of indentation is
			// strictly deeper than the parent's column 0, so "sub" nests
			// under "key". The old width/2 truncation treated it as the
			// same level, silently re-parenting "sub" as a root sibling.
			name:  "one-space indent nests under the parent",
			input: "key:\n sub: v",
			check: func(t *testing.T, root *Value) {
				if len(root.Map) != 1 {
					t.Fatalf("root has %d keys, want 1 (keys: %v)", len(root.Map), iyValueKeys(root.Map))
				}
				key := root.Map["key"]
				if key == nil || key.Type != ValueTypeMap {
					t.Fatalf("key = %v, want a nested map", key)
				}
				if got := key.Map["sub"]; got == nil || got.Scalar != "v" {
					t.Errorf("key.sub = %v, want scalar \"v\"", got)
				}
			},
		},
		{
			// Document semantics (GEN-001): a second '---' document used to
			// be silently dropped — the load reported success while holding
			// a fraction of the file. Multi-document streams now fail
			// loudly; load one document per file.
			name:    "second document errors",
			input:   "a: 1\n---\nb: 2",
			wantErr: true,
		},
		{
			// parseDocument/parseNestedValue use depth >= maxDepth, so
			// nesting exactly AT the limit still succeeds.
			name:     "nesting exactly at maxDepth succeeds",
			input:    "a:\n  b: v",
			maxDepth: 2,
			check: func(t *testing.T, root *Value) {
				a := root.Map["a"]
				if a == nil || a.Map["b"] == nil || a.Map["b"].Scalar != "v" {
					t.Errorf("a.b = %v, want \"v\"", a)
				}
			},
		},
		{
			// ...and one level beyond the limit errors out.
			name:     "nesting one beyond maxDepth errors",
			input:    "a:\n  b:\n    c: v",
			maxDepth: 2,
			wantErr:  true,
		},
		{
			name:     "nesting beyond a low maxDepth errors",
			input:    "a:\n  b:\n    c:\n      d:\n        e: value",
			maxDepth: 2,
			wantErr:  true,
		},
		{
			// Error transmission: a lexer failure (unterminated quoted
			// string) must surface as a ParseYAML error, never be silently
			// parsed. This is the single lexer-error case at parser level;
			// the full lexer error matrix lives in yaml_lexer_test.go.
			name:    "lexer error propagates through ParseYAML",
			input:   `key: "unterminated`,
			wantErr: true,
		},
		{
			name:  "key with comment then nested map",
			input: "parent:\n  # a comment\n  child: value",
			check: func(t *testing.T, root *Value) {
				parent := root.Map["parent"]
				if parent == nil || parent.Map["child"] == nil {
					t.Fatal("expected parent.child")
				}
				if got := parent.Map["child"].Scalar; got != "value" {
					t.Errorf("parent.child = %q, want %q", got, "value")
				}
			},
		},
		{
			name:  "key with comment and blank line then nested map",
			input: "parent: # trailing comment\n\n  child: value",
			check: func(t *testing.T, root *Value) {
				parent := root.Map["parent"]
				if parent == nil || parent.Map["child"] == nil {
					t.Fatal("expected parent.child after comment and blank line")
				}
			},
		},
		{
			name:  "comment after key with scalar sibling",
			input: "key: # just a comment\nother: value",
			check: func(t *testing.T, root *Value) {
				if root.Map["other"] == nil || root.Map["other"].Scalar != "value" {
					t.Errorf("other = %v, want value", root.Map["other"])
				}
			},
		},
		{
			name:  "blank lines and comment before nested value",
			input: "parent:\n\n  # inner comment\n  child: value",
			check: func(t *testing.T, root *Value) {
				parent := root.Map["parent"]
				if parent == nil || parent.Map["child"] == nil {
					t.Fatal("expected parent.child after blank lines and comment")
				}
			},
		},
		{
			// A comment-only nested block leaves the key with an empty
			// (null) scalar, and the following dedented sibling key is
			// recovered — previously the sibling was silently dropped
			// because parseNestedValue left the closing Dedent unconsumed,
			// making the enclosing map end its own scope early.
			name:  "comment-only nested value recovers dedented sibling",
			input: "key:\n  # only a comment\nother: value",
			check: func(t *testing.T, root *Value) {
				key := root.Map["key"]
				if key == nil || key.Type != ValueTypeScalar || key.Scalar != "" {
					t.Errorf("key = %v, want an empty scalar", key)
				}
				other := root.Map["other"]
				if other == nil || other.Scalar != "value" {
					t.Errorf("other = %v, want value", other)
				}
				if len(root.Map) != 2 {
					t.Errorf("root has %d keys, want 2", len(root.Map))
				}
			},
		},
		{
			name:  "key with empty value then nested scalar",
			input: "key:\n  value",
			check: func(t *testing.T, root *Value) {
				key := root.Map["key"]
				if key == nil {
					t.Fatal("expected 'key'")
				}
				if key.Type != ValueTypeScalar || key.Scalar != "value" {
					t.Errorf("key = %v, want the nested scalar \"value\"", key)
				}
			},
		},
		{
			name:  "nested map followed by sibling key",
			input: "root:\n  inner: val\nkey2: val2",
			check: func(t *testing.T, root *Value) {
				if len(root.Map) != 2 {
					t.Errorf("expected 2 root keys, got %d", len(root.Map))
				}
			},
		},
		{
			name:  "nested map with dedent to sibling",
			input: "outer:\n  inner:\n    deep: val\n  sibling: val2",
			check: func(t *testing.T, root *Value) {
				outer := root.Map["outer"]
				if outer == nil || outer.Type != ValueTypeMap {
					t.Fatal("expected outer map")
				}
				if outer.Map["sibling"] == nil {
					t.Error("expected sibling key after dedent")
				}
			},
		},
		{
			name:  "nested value at EOF",
			input: "parent:\n  child:",
			check: func(t *testing.T, root *Value) {
				child := root.Map["parent"].Map["child"]
				if child == nil || child.Scalar != "" {
					t.Error("expected empty child value at EOF")
				}
			},
		},
		{
			name:  "array item at EOF",
			input: "items:\n  -",
			check: func(t *testing.T, root *Value) {
				items := root.Map["items"]
				if items == nil || items.Type != ValueTypeArray {
					t.Fatal("expected items array")
				}
			},
		},
		{
			name:  "array item newline then non-indented sibling",
			input: "items:\n  -\nother: value",
			check: func(t *testing.T, root *Value) {
				if root.Map["other"] == nil || root.Map["other"].Scalar != "value" {
					t.Errorf("other = %v, want value", root.Map["other"])
				}
			},
		},
		{
			// Nested-dash arrays keep every row: "- - a / - b" builds one
			// inner array per outer item. Previously only the first row was
			// represented (flattened as [empty-array, "a", "b"]) and later
			// rows were dropped entirely.
			name:  "array of nested arrays keeps both rows",
			input: "matrix:\n  - - a\n    - b\n  - - c\n    - d",
			check: func(t *testing.T, root *Value) {
				matrix := root.Map["matrix"]
				if matrix == nil || matrix.Type != ValueTypeArray {
					t.Fatal("expected matrix array")
				}
				if len(matrix.Array) != 2 {
					t.Fatalf("matrix has %d items, want 2", len(matrix.Array))
				}
				wantRows := [][]string{{"a", "b"}, {"c", "d"}}
				for i, want := range wantRows {
					row := matrix.Array[i]
					if row.Type != ValueTypeArray || len(row.Array) != len(want) {
						t.Fatalf("matrix[%d] = %v, want an array of %d scalars", i, row, len(want))
					}
					for j, w := range want {
						if got := row.Array[j]; got.Type != ValueTypeScalar || got.Scalar != w {
							t.Errorf("matrix[%d][%d] = %q, want %q", i, j, got.Scalar, w)
						}
					}
				}
			},
		},
		{
			// Genuine continuation-line shape. The lexer does not join
			// plain-scalar continuation lines, and under column-based
			// nesting (M-013) the over-indented "y" (col 6) and the
			// mid-width trailing "- z" (col 4, net zero depth change from
			// col 6) leave a sequence item in a mapping context — the
			// parser now rejects the document instead of the previous
			// silent drop of both lines (data = [["x"]], "y" and "z" lost
			// without any signal).
			name:    "nested array direct",
			input:   "data:\n  - - x\n      y\n    - z",
			wantErr: true,
		},
		{
			name:  "array item with newline before nested map",
			input: "items:\n  -\n    key1: val1\n    key2: val2",
			check: func(t *testing.T, root *Value) {
				items := root.Map["items"]
				if items == nil || items.Type != ValueTypeArray || len(items.Array) != 1 {
					t.Fatalf("expected items array of length 1, got %v", items)
				}
				item := items.Array[0]
				if item.Type != ValueTypeMap {
					t.Fatalf("item type = %v, want ValueTypeMap", item.Type)
				}
				if item.Map["key1"] == nil || item.Map["key1"].Scalar != "val1" {
					t.Error("expected key1=val1 in array item")
				}
			},
		},
		{
			name:  "array with comment between items",
			input: "list:\n  - one\n  # middle comment\n  - two",
			check: func(t *testing.T, root *Value) {
				list := root.Map["list"]
				if list == nil || list.Type != ValueTypeArray {
					t.Fatal("expected list array")
				}
				if len(list.Array) != 2 {
					t.Errorf("list length = %d, want 2", len(list.Array))
				}
			},
		},
		{
			name:  "complex nesting (services)",
			input: "services:\n  - name: web\n    ports:\n      - 80\n      - 443\n    env:\n      PORT: \"8080\"\n  - name: db\n    ports:\n      - 5432",
			check: func(t *testing.T, root *Value) {
				services := root.Map["services"]
				if services == nil || services.Type != ValueTypeArray || len(services.Array) != 2 {
					t.Fatalf("expected services array of length 2, got %v", services)
				}
				web := services.Array[0]
				if web.Map["name"] == nil || web.Map["name"].Scalar != "web" {
					t.Error("expected first service name=web")
				}
				ports := web.Map["ports"]
				if ports == nil || ports.Type != ValueTypeArray || len(ports.Array) != 2 {
					t.Error("expected web.ports to be a 2-element array")
				}
			},
		},
		{
			// Non-ASCII text passes through the whole pipeline verbatim:
			// the tree stores the document's bytes, and case handling
			// (which is ASCII-only, see flatten_keys.go) never applies at
			// parse level.
			name:  "unicode key and value preserved",
			input: "键: 值",
			check: func(t *testing.T, root *Value) {
				v := root.Map["键"]
				if v == nil || v.Type != ValueTypeScalar || v.Scalar != "值" {
					t.Errorf("root[\"键\"] = %v, want scalar \"值\" (keys: %v)", v, iyValueKeys(root.Map))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxDepth := tt.maxDepth
			if maxDepth == 0 {
				maxDepth = 10
			}
			result, err := ParseYAML([]byte(tt.input), maxDepth)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

// ============================================================================
// ParseYAML Function Tests
// ============================================================================

// (The trivial single-pair ParseYAML smoke test was removed: "key: value"
// through the full pipeline is pinned by TestYAMLParser_Structure, and
// ParseYAML's own integration behavior by TestParseYAML_Complex below.)

// TestParseYAML_RootSequenceErrors pins the root-sequence rejection: a
// document starting with "- " previously fell through parseMap's
// "shouldn't happen" Dash break and parsed to a SILENTLY EMPTY map — the
// whole file loaded as zero variables with no error. (JSON root arrays
// flatten to _0.._N keys; YAML root sequences error instead.)
func TestParseYAML_RootSequenceErrors(t *testing.T) {
	for _, input := range []string{
		"- a\n- b\n",
		"- a\n",
		"key: 1\n- b\n", // sequence at map level mid-document
	} {
		result, err := ParseYAML([]byte(input), 10)
		if err == nil {
			t.Errorf("ParseYAML(%q) succeeded with result %v, want an error", input, result)
			continue
		}
		if !strings.Contains(err.Error(), "mapping") {
			t.Errorf("ParseYAML(%q) error = %v, want it to name the root-mapping requirement", input, err)
		}
	}
}

// TestParseYAML_SilentDataLossErrors pins the four shapes that used to load a
// fraction of the document while reporting success: a bare scalar line, an
// unquoted ": " inside a plain value, an empty mapping key, and a value
// followed by leftover text on the same line. Each is malformed YAML (PyYAML
// rejects the middle three and parses the first as a non-mapping document,
// which this library does not support), so each must fail loudly instead of
// silently dropping data.
func TestParseYAML_SilentDataLossErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"bare scalar line in mapping", "just text here\nother: keep\n"},
		{"colon+space inside plain value", "apnews.com: Code=abc; note: keep\n"},
		{"value ending with a colon", "k: abc:\n"},
		{"empty mapping key", "\"\": v\n"},
		{"junk after a quoted value", "k: \"abc\" junk\n"},
		{"unquoted apostrophe truncates a quoted value", "k: 'it's fine'\n"},
		{"root flow collection", "{a: 1, b: 2}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := ParseYAML([]byte(tt.input), 10)
			if err == nil {
				if value != nil {
					ReleaseValue(value)
				}
				t.Fatalf("ParseYAML(%q) succeeded, want an error (data would be dropped silently)", tt.input)
			}
		})
	}
}

// TestParseYAML_QuotedFormsStayLoadable pins the escape hatch: every shape
// rejected above becomes a literal value once quoted, so callers keep a
// documented fix.
func TestParseYAML_QuotedFormsStayLoadable(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		want  string
	}{
		{"single-quoted colon+space", "k: 'a: b'\n", "k", "a: b"},
		{"double-quoted colon+space", "k: \"a: b\"\n", "k", "a: b"},
		{"single-quoted apostrophe", "k: 'it''s fine'\n", "k", "it's fine"},
		{"block scalar keeps content", "k: |-\n  a: b\n", "k", "a: b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := ParseYAML([]byte(tt.input), 10)
			if err != nil {
				t.Fatalf("ParseYAML(%q) error = %v", tt.input, err)
			}
			defer ReleaseValue(value)
			got := value.Map[tt.key]
			if got == nil || got.Scalar != tt.want {
				t.Errorf("%s = %v, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// TestParseYAML_IndentedCommentKeepsSiblings pins the comment/indent fix: a
// comment-only line never changes the indent level, so an indented comment
// between two keys does not close the enclosing map and drop everything after
// it.
func TestParseYAML_IndentedCommentKeepsSiblings(t *testing.T) {
	for _, input := range []string{
		"k: v\n  # note\nb: 2\n",
		"k: v\n    # deeper note\nb: 2\n",
		"parent:\n  a: 1\n    # note\n  b: 2\nafter: 3\n",
	} {
		value, err := ParseYAML([]byte(input), 10)
		if err != nil {
			t.Fatalf("ParseYAML(%q) error = %v", input, err)
		}
		ReleaseValue(value)
	}

	value, err := ParseYAML([]byte("k: v\n  # note\nb: 2\n"), 10)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	defer ReleaseValue(value)
	if value.Map["b"] == nil || value.Map["b"].Scalar != "2" {
		t.Errorf("b = %v, want 2 (a comment line must not end the mapping)", value.Map["b"])
	}
}

func TestParseYAML_Complex(t *testing.T) {
	data := []byte(`
app:
  name: myapp
  version: "1.0.0"
database:
  host: localhost
  port: 5432
  credentials:
    username: admin
    password: secret
features:
  - auth
  - logging
  - cache
`)

	result, err := ParseYAML(data, 10)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}

	// The parser preserves key case verbatim — uppercasing happens only in
	// the flattener's buildFlatKey (flatten_keys.go), never in the lexer
	// or parser — so the tree keeps the document's lowercase keys.
	app := result.Map["app"]
	if app == nil {
		t.Fatalf("expected 'app' key, got keys: %v", iyValueKeys(result.Map))
	}
	name := app.Map["name"]
	if name == nil || name.Scalar != "myapp" {
		t.Errorf("app.name = %v, want %q", name, "myapp")
	}
	version := app.Map["version"]
	if version == nil || version.Scalar != "1.0.0" {
		t.Errorf("app.version = %v, want %q", version, "1.0.0")
	}

	db := result.Map["database"]
	if db == nil {
		t.Fatalf("expected 'database' key, got keys: %v", iyValueKeys(result.Map))
	}
	host := db.Map["host"]
	if host == nil || host.Scalar != "localhost" {
		t.Errorf("database.host = %v, want %q", host, "localhost")
	}
	creds := db.Map["credentials"]
	if creds == nil {
		t.Fatalf("expected database.credentials key, got keys: %v", iyValueKeys(db.Map))
	}
	username := creds.Map["username"]
	if username == nil || username.Scalar != "admin" {
		t.Errorf("database.credentials.username = %v, want %q", username, "admin")
	}
	password := creds.Map["password"]
	if password == nil || password.Scalar != "secret" {
		t.Errorf("database.credentials.password = %v, want %q", password, "secret")
	}

	features := result.Map["features"]
	if features == nil || features.Type != ValueTypeArray {
		t.Fatalf("expected 'features' array, got %v", features)
	}
	wantFeatures := []string{"auth", "logging", "cache"}
	if len(features.Array) != len(wantFeatures) {
		t.Fatalf("features length = %d, want %d", len(features.Array), len(wantFeatures))
	}
	for i, want := range wantFeatures {
		if got := features.Array[i].Scalar; got != want {
			t.Errorf("features[%d] = %q, want %q", i, got, want)
		}
	}
}

// ============================================================================
// Value Type Tests
// ============================================================================

// TestValueConstructors pins the exported Value constructors' field wiring:
// type, source position, and the non-nil container each container
// constructor must install.
func TestValueConstructors(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		v := NewScalarValue("test", 1, 5)
		if v.Type != ValueTypeScalar || v.Scalar != "test" || v.Line != 1 || v.Column != 5 {
			t.Errorf("NewScalarValue() = %+v, want scalar %q at 1:5", v, "test")
		}
	})

	t.Run("map", func(t *testing.T) {
		v := NewMapValue(2, 10)
		if v.Type != ValueTypeMap || v.Map == nil || v.Line != 2 || v.Column != 10 {
			t.Errorf("NewMapValue() = %+v, want a non-nil map at 2:10", v)
		}
	})

	t.Run("array", func(t *testing.T) {
		v := NewArrayValue(3, 15)
		if v.Type != ValueTypeArray || v.Array == nil || v.Line != 3 || v.Column != 15 {
			t.Errorf("NewArrayValue() = %+v, want a non-nil array at 3:15", v)
		}
	})
}

// ============================================================================
// Parser Edge Cases
// ============================================================================

func TestYAMLParser_DefaultMaxDepth(t *testing.T) {
	// Test that default maxDepth is applied when 0 or negative
	lexer := newYAMLLexer([]byte("key: value"))
	tokens, err := lexer.tokenizeInto(nil)
	if err != nil {
		t.Fatalf("Tokenize() error = %v", err)
	}

	parser := NewYAMLParser(tokens, 0)
	if parser.maxDepth != 16 {
		t.Errorf("maxDepth = %d, want 16 (default, matches the root package's defaultStructuredMaxDepth)", parser.maxDepth)
	}

	parser = NewYAMLParser(tokens, -1)
	if parser.maxDepth != 16 {
		t.Errorf("maxDepth = %d, want 16 (default, matches the root package's defaultStructuredMaxDepth)", parser.maxDepth)
	}
}

// TestReleaseValue covers the nil-safe and idempotent release paths; the
// full-tree release semantics are pinned separately by
// TestReleaseValue_Nested below.
func TestReleaseValue(t *testing.T) {
	t.Run("nil is a no-op", func(t *testing.T) {
		ReleaseValue(nil) // should not panic
	})

	t.Run("scalar is cleared", func(t *testing.T) {
		v := NewScalarValue("test", 1, 1)
		ReleaseValue(v)
		if v.Scalar != "" {
			t.Error("scalar not cleared after release")
		}
	})

	t.Run("double release does not panic", func(t *testing.T) {
		v := NewScalarValue("test", 1, 1)
		ReleaseValue(v)
		ReleaseValue(v) // guarded no-op: Value.pooled blocks the second Put
	})
}

func TestReleaseValue_Nested(t *testing.T) {
	input := `
parent:
  child1: value1
  child2:
    - a
    - b
`
	value, err := ParseYAML([]byte(input), 10)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	ReleaseValue(value)
	// Verify the tree holds no residual content. Small containers may be
	// retained on the pooled node for reuse (see maxPooledValueChildren);
	// the release contract is emptiness, not a nil reference.
	if len(value.Map) != 0 {
		t.Error("map not cleared after release")
	}
}

// TestYAMLParser_CurrentBeyondTokens pins the EOF sentinel contract of
// Parser.current(): exhausting the token stream must yield TokenEOF, never
// panic or read out of bounds.
func TestYAMLParser_CurrentBeyondTokens(t *testing.T) {
	p := NewYAMLParser(nil, 10)
	p.advance() // already past the (empty) token slice
	if got := p.current().Type; got != TokenEOF {
		t.Errorf("current() beyond tokens = %v, want TokenEOF", got)
	}
}

// TestYAMLParser_ZeroMaxDepthRejected covers parseDocument's depth guard,
// reachable only when the parser is constructed with a non-positive maxDepth
// (NewYAMLParser clamps public callers to a minimum of 10).
func TestYAMLParser_ZeroMaxDepthRejected(t *testing.T) {
	p := &Parser{maxDepth: 0}
	if _, err := p.Parse(); err == nil {
		t.Error("Parse() with maxDepth 0 should exceed the depth limit")
	}
}

// TestParseYAML_HashInPlainScalars pins the YAML comment rule: '#' begins a
// comment only at line start or after whitespace; '#' inside a plain scalar
// is data. Regression: the lexer truncated at any '#', corrupting values such
// as http://host/p#frag or pa#ss.
func TestParseYAML_HashInPlainScalars(t *testing.T) {
	data := []byte(`# full line comment
url: http://host/p#frag
password: pa#ss
note: value # trailing comment
`)

	result, err := ParseYAML(data, 10)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}

	if got := result.Map["url"].Scalar; got != "http://host/p#frag" {
		t.Errorf("url = %q, want %q", got, "http://host/p#frag")
	}
	if got := result.Map["password"].Scalar; got != "pa#ss" {
		t.Errorf("password = %q, want %q", got, "pa#ss")
	}
	if got := result.Map["note"].Scalar; got != "value" {
		t.Errorf("note = %q, want %q (comment after whitespace must still be stripped)", got, "value")
	}
	if len(result.Map) != 3 {
		t.Errorf("expected exactly 3 keys (full-line comment ignored), got %d", len(result.Map))
	}
}

// TestParseYAML_HashMidTokenKey documents that a '#' inside an unquoted key
// (not preceded by whitespace) is preserved as part of the key; downstream
// key validation rejects it with an explicit error rather than the previous
// silent truncation of the key.
func TestParseYAML_HashMidTokenKey(t *testing.T) {
	result, err := ParseYAML([]byte("weird#key: v"), 10)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	node := result.Map["weird#key"]
	if node == nil {
		t.Fatalf("key containing mid-token '#' was not preserved; keys: %v", iyValueKeys(result.Map))
	}
	if node.Scalar != "v" {
		t.Errorf("weird#key = %q, want %q", node.Scalar, "v")
	}
}

// mkKey/mkValue/mkDash construct tokens for the branch-level parser tests.
func mkKey(v string, indent int) Token {
	return Token{Type: TokenKey, Value: v, Indent: indent, Line: 1, Column: 1}
}

// TestYAMLParser_TokenLevelBranches drives the parser with hand-built token
// sequences that the lexer never (or rarely) emits, pinning the defensive
// branches of parseMap, parseNestedValue, and parseArray: document separators
// inside structures, dedent-by-indent, comments and newlines after keys,
// inline maps and dashes as values, and depth-limit error propagation.
func TestYAMLParser_TokenLevelBranches(t *testing.T) {
	tests := []struct {
		name    string
		tokens  []Token
		maxDep  int
		wantErr bool
		check   func(t *testing.T, root *Value)
	}{
		{
			name:   "key below expected indent ends nested map",
			tokens: []Token{mkKey("K", 0), {Type: TokenIndent, Indent: 1}, mkKey("INNER", 0), {Type: TokenValue, Value: "v"}},
			check: func(t *testing.T, root *Value) {
				if root.Map["INNER"] == nil || root.Map["INNER"].Scalar != "v" {
					t.Errorf("INNER = %v, want scalar v", root.Map["INNER"])
				}
			},
		},
		{
			name:   "comment after key at EOF yields empty scalar",
			tokens: []Token{mkKey("K", 0), {Type: TokenComment, Value: "c"}, {Type: TokenEOF}},
			check: func(t *testing.T, root *Value) {
				if root.Map["K"] == nil || root.Map["K"].Scalar != "" {
					t.Errorf("K = %v, want empty scalar", root.Map["K"])
				}
			},
		},
		{
			name:   "newline after key without indent yields empty scalar",
			tokens: []Token{mkKey("K", 0), {Type: TokenNewline}, mkKey("N", 1), {Type: TokenValue, Value: "v"}},
			check: func(t *testing.T, root *Value) {
				if root.Map["K"] == nil || root.Map["K"].Scalar != "" {
					t.Errorf("K = %v, want empty scalar (no indent token)", root.Map["K"])
				}
			},
		},
		{
			name:   "dash directly after key parses as array value",
			tokens: []Token{mkKey("K", 0), {Type: TokenDash, Indent: 1}, {Type: TokenValue, Value: "a"}},
			check: func(t *testing.T, root *Value) {
				if root.Map["K"] == nil || root.Map["K"].Type != ValueTypeArray || len(root.Map["K"].Array) != 1 {
					t.Errorf("K = %v, want single-element array", root.Map["K"])
				}
			},
		},
		{
			// A second key on the SAME line means the plain scalar carried a
			// ": " of its own ("k: a: b"), which YAML rejects; the parser must
			// not turn one value into two entries.
			name:    "second key on the same line is an error",
			tokens:  []Token{mkKey("K", 0), mkKey("N", 0), {Type: TokenValue, Value: "v"}},
			wantErr: true,
		},
		{
			name:   "EOF right after key indent yields empty scalar",
			tokens: []Token{mkKey("K", 0), {Type: TokenIndent, Indent: 1}},
			check: func(t *testing.T, root *Value) {
				if root.Map["K"] == nil || root.Map["K"].Scalar != "" {
					t.Errorf("K = %v, want empty scalar at EOF", root.Map["K"])
				}
			},
		},
		{
			name:   "comments between key and nested map",
			tokens: []Token{mkKey("K", 0), {Type: TokenComment, Value: "c1"}, {Type: TokenNewline}, {Type: TokenIndent, Indent: 1}, mkKey("N", 1), {Type: TokenValue, Value: "v"}},
			check: func(t *testing.T, root *Value) {
				k := root.Map["K"]
				if k == nil || k.Map["N"] == nil || k.Map["N"].Scalar != "v" {
					t.Errorf("K = %v, want nested map with N=v after comments", k)
				}
			},
		},
		{
			// parseNestedValue yields an empty scalar for the unknown token
			// (defensive branch: TokenColon is never emitted by the lexer),
			// but the token itself is then still pending at mapping level —
			// which is now an error instead of a silent skip, so no entry can
			// disappear without a signal.
			name:    "unexpected token in nested value is an error",
			tokens:  []Token{mkKey("K", 0), {Type: TokenIndent, Indent: 1}, {Type: TokenColon}, {Type: TokenEOF}},
			wantErr: true,
		},
		{
			// The token stream holds a value where the mapping expects an
			// entry; the parser used to skip it, dropping data silently.
			name:    "stray value token in mapping context is an error",
			tokens:  []Token{mkKey("K", 0), {Type: TokenValue, Value: "v"}, {Type: TokenValue, Value: "stray"}},
			wantErr: true,
		},
		{
			name:   "dash then EOF yields array with empty scalar",
			tokens: []Token{mkKey("K", 0), {Type: TokenDash, Indent: 1}},
			check: func(t *testing.T, root *Value) {
				k := root.Map["K"]
				if k == nil || k.Type != ValueTypeArray || len(k.Array) != 1 || k.Array[0].Scalar != "" {
					t.Errorf("K = %v, want array with one empty scalar", k)
				}
			},
		},
		{
			name:   "indent directly after dash parses nested item",
			tokens: []Token{mkKey("K", 0), {Type: TokenDash, Indent: 1}, {Type: TokenIndent, Indent: 2}, mkKey("N", 2), {Type: TokenValue, Value: "v"}},
			check: func(t *testing.T, root *Value) {
				k := root.Map["K"]
				if k == nil || k.Type != ValueTypeArray || len(k.Array) != 1 {
					t.Fatalf("K = %v, want one array item", k)
				}
				if k.Array[0].Map["N"] == nil {
					t.Errorf("array item = %v, want a map with N", k.Array[0])
				}
			},
		},
		{
			name:   "nested dash chain exceeds depth limit",
			tokens: []Token{mkKey("K", 0), {Type: TokenDash, Indent: 1}, {Type: TokenDash, Indent: 2}, {Type: TokenDash, Indent: 3}},
			maxDep: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxDepth := tt.maxDep
			if maxDepth == 0 {
				maxDepth = 10
			}
			p := NewYAMLParser(tt.tokens, maxDepth)
			root, err := p.Parse()
			if tt.maxDep != 0 {
				// Depth-limit case: expect an error.
				if err == nil {
					t.Fatal("Parse() error = nil, want depth-limit error")
				}
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("Parse() error = nil, want an error for the malformed mapping")
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, root)
		})
	}
}

// TestYAMLParser_DocumentStartTerminatesNested pinpoints the token-level
// behavior the former TokenLevelBranches rows covered: a nested map or
// array ends CLEANLY on a '---' separator (the separator is left as the
// current token and later keys are not consumed into the nested value).
// Through Parse() such input is now a hard multi-document error (see
// TestYAMLParser_Structure), so this drives parseMap directly.
func TestYAMLParser_DocumentStartTerminatesNested(t *testing.T) {
	t.Run("map", func(t *testing.T) {
		p := NewYAMLParser([]Token{
			mkKey("K", 0), {Type: TokenIndent, Indent: 1}, mkKey("N", 1),
			{Type: TokenValue, Value: "v"}, {Type: TokenDocumentStart},
			mkKey("AFTER", 0), {Type: TokenValue, Value: "w"},
		}, 10)
		root, err := p.parseMap(0, 0)
		if err != nil {
			t.Fatalf("parseMap() error = %v", err)
		}
		k := root.Map["K"]
		if k == nil || k.Type != ValueTypeMap || k.Map["N"] == nil || k.Map["N"].Scalar != "v" {
			t.Fatalf("K = %v, want nested map with N=v (terminated at document start)", k)
		}
		if root.Map["AFTER"] != nil {
			t.Errorf("AFTER was consumed into the first document; want it past the separator")
		}
		if p.current().Type != TokenDocumentStart {
			t.Errorf("current token = %v, want TokenDocumentStart left unconsumed", p.current().Type)
		}
	})

	t.Run("array", func(t *testing.T) {
		p := NewYAMLParser([]Token{
			mkKey("K", 0), {Type: TokenDash, Indent: 1}, {Type: TokenValue, Value: "a"},
			{Type: TokenDocumentStart},
		}, 10)
		root, err := p.parseMap(0, 0)
		if err != nil {
			t.Fatalf("parseMap() error = %v", err)
		}
		k := root.Map["K"]
		if k == nil || k.Type != ValueTypeArray || len(k.Array) != 1 || k.Array[0].Scalar != "a" {
			t.Errorf("K = %v, want array with one element (terminated at document start)", k)
		}
	})
}

// TestYAMLParser_BlockScalars pins GEN-001 block scalar support: literal
// (|) and folded (>) scalars with all three chomping indicators, explicit
// indentation, nested keys after the block, and the error paths (header
// with content, bad explicit indent).
func TestYAMLParser_BlockScalars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"literal clip keeps one trailing newline",
			"cert: |\n  -----BEGIN-----\n  abc\nnext: 1\n", "-----BEGIN-----\nabc\n"},
		{"literal strip drops the trailing newline",
			"cert: |-\n  a\n  b\n", "a\nb"},
		{"literal keep keeps blank lines",
			"cert: |+\n  a\n\n\n", "a\n\n\n"},
		{"empty literal block",
			"cert: |\nnext: 1\n", ""},
		{"literal at EOF without trailing newline",
			"cert: |\n  a", "a\n"},
		{"extra indentation beyond the block indent is content",
			"v: |\n    a\n      b\n    c\n", "a\n  b\nc\n"},
		{"blank line inside the block is content",
			"v: |\n  a\n\n  b\n", "a\n\nb\n"},
		{"header comment is not content",
			"v: | # cert\n  a\n", "a\n"},
		{"folded joins lines with spaces",
			"v: >\n  a\n  b\n", "a b\n"},
		{"folded blank line becomes a newline",
			"v: >\n  a\n\n  b\n", "a\nb\n"},
		{"folded strip",
			"v: >-\n  a\n  b\n", "a b"},
		{"folded keep",
			"v: >+\n  a\n\n", "a\n\n"},
		{"explicit indentation indicator",
			"v: |2\n   a\n", " a\n"},
		{"block under a nested key",
			"top:\n  cert: |\n    line1\n    line2\n  sibling: v\n", "line1\nline2\n"},
		{"crlf line endings",
			"cert: |\r\n  a\r\n  b\r\n", "a\nb\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := ParseYAMLWithPolicy([]byte(tt.input), 10, DuplicateKeyInherit)
			if err != nil {
				t.Fatalf("ParseYAML() error = %v", err)
			}
			// Locate the value: top-level "cert"/"v", or top.cert.
			v := root.Map["cert"]
			if v == nil {
				v = root.Map["v"]
			}
			if v == nil {
				if top := root.Map["top"]; top != nil {
					v = top.Map["cert"]
				}
			}
			if v == nil {
				t.Fatalf("value key not found (root: %v)", iyValueKeys(root.Map))
			}
			if v.Scalar != tt.want {
				t.Errorf("block scalar = %q, want %q", v.Scalar, tt.want)
			}
			if !v.Quoted {
				t.Error("block scalar value is not quoted (must flatten verbatim)")
			}
		})
	}

	// Sibling keys after the block still parse.
	root, err := ParseYAMLWithPolicy([]byte("cert: |\n  body\nnext: 1\n"), 10, DuplicateKeyInherit)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	if root.Map["next"] == nil || root.Map["next"].Scalar != "1" {
		t.Errorf("next = %v, want 1 (sibling after block)", root.Map["next"])
	}

	// A header with content on the same line is a plain scalar, not a block.
	root, err = ParseYAMLWithPolicy([]byte("v: | text\n"), 10, DuplicateKeyInherit)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	if root.Map["v"].Scalar != "| text" {
		t.Errorf("v = %q, want plain scalar %q", root.Map["v"].Scalar, "| text")
	}

	// Explicit indentation deeper than the content: loud error.
	if _, err := ParseYAMLWithPolicy([]byte("v: |4\n  a\n"), 10, DuplicateKeyInherit); err == nil {
		t.Error("ParseYAML() = nil error, want indentation error for explicit |4 with 2-space content")
	}
}

// TestYAMLParser_MultiDocumentErrors pins the GEN-001 multi-document error
// through the public parse entry: leading separators are fine, a second
// document is a hard error, and the message names the separator.
func TestYAMLParser_MultiDocumentErrors(t *testing.T) {
	if _, err := ParseYAMLWithPolicy([]byte("---\na: 1\n"), 10, DuplicateKeyInherit); err != nil {
		t.Fatalf("leading separator: ParseYAML() error = %v", err)
	}
	_, err := ParseYAMLWithPolicy([]byte("a: 1\n---\nb: 2\n"), 10, DuplicateKeyInherit)
	if err == nil {
		t.Fatal("second document: ParseYAML() = nil, want multi-document error")
	}
	var yamlErr *YAMLError
	if !errors.As(err, &yamlErr) || !strings.Contains(yamlErr.Message, "multiple YAML documents") {
		t.Errorf("error = %v, want a YAMLError naming multiple documents", err)
	}
}

// TestYAMLParser_ParseNestedValueIndentEntry covers the indent-consumption
// branch at the head of parseNestedValue, reachable only when the parser is
// handed a token stream whose current token is an indent at entry.
func TestYAMLParser_ParseNestedValueIndentEntry(t *testing.T) {
	p := NewYAMLParser([]Token{{Type: TokenIndent, Indent: 1}, mkKey("N", 1), {Type: TokenValue, Value: "v"}}, 10)
	v, err := p.parseNestedValue(1, 1)
	if err != nil {
		t.Fatalf("parseNestedValue() error = %v", err)
	}
	if v == nil || v.Map["N"] == nil {
		t.Errorf("parseNestedValue() = %v, want map with N", v)
	}
}

// TestNewArrayValue_PooledReuse pins the pool-reuse branch: a pooled Value
// carrying a non-nil Array is reset via truncation, not reallocation.
func TestNewArrayValue_PooledReuse(t *testing.T) {
	valuePool.Put(&Value{Array: []*Value{NewScalarValue("stale", 1, 1)}})

	v := NewArrayValue(1, 1)
	if v.Type != ValueTypeArray {
		t.Fatalf("NewArrayValue() type = %v, want ValueTypeArray", v.Type)
	}
	if len(v.Array) != 0 {
		t.Errorf("reused Array length = %d, want 0 (stale elements must be dropped)", len(v.Array))
	}
}

// TestNewMapValue_PooledReuse is the map counterpart of the array test
// above: a pooled Value that still carries a non-nil Map (only reachable
// via a seeded pool entry — ReleaseValue nils Map before returning values)
// must be reused with its stale entries cleared.
func TestNewMapValue_PooledReuse(t *testing.T) {
	valuePool.Put(&Value{Map: map[string]*Value{"stale": NewScalarValue("stale", 1, 1)}})

	v := NewMapValue(1, 1)
	if v.Type != ValueTypeMap {
		t.Fatalf("NewMapValue() type = %v, want ValueTypeMap", v.Type)
	}
	if len(v.Map) != 0 {
		t.Errorf("reused Map length = %d, want 0 (stale entries must be cleared)", len(v.Map))
	}
}
