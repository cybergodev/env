package internal

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestParseLineBytes covers line-level concerns only: key/separator/export
// prefix/blank lines plus the boundary pins. Value semantics (quoting,
// comments, YAML scalars) are covered by TestParseValueBytes — ParseLineBytes
// delegates value parsing to it.
func TestParseLineBytes(t *testing.T) {
	lp := imNewLineParser(nil, ModeNone, LineParserConfig{AllowExportPrefix: true})

	tests := []struct {
		name      string
		line      string
		wantKey   string
		wantValue string
		wantErr   bool
	}{
		{
			name:      "simple assignment",
			line:      "KEY=value",
			wantKey:   "KEY",
			wantValue: "value",
		},
		{
			name:      "export prefix",
			line:      "export KEY=value",
			wantKey:   "KEY",
			wantValue: "value",
		},
		{
			// PIN: ':' separates only in YAML-syntax mode (see
			// TestParseLineBytes_ColonSeparatorGated); with the default
			// config the line is dropped as a no-separator line instead of
			// silently becoming a variable.
			name: "colon separator dropped without AllowYamlSyntax",
			line: "KEY:value",
		},
		{
			name: "no assignment",
			line: "NO_ASSIGNMENT_HERE",
		},
		{
			name: "empty line",
			line: "",
		},
		// BOUNDARY: a whitespace-only line has no separator and no
		// non-space content — dropped silently like the empty line.
		{
			name: "whitespace-only line dropped",
			line: "   ",
		},
		// BOUNDARY: whitespace around both the key and the value is trimmed.
		{
			name:      "whitespace around key and value trimmed",
			line:      "  KEY  =  value",
			wantKey:   "KEY",
			wantValue: "value",
		},
		// BOUNDARY: trailing spaces (unlike '\r' above) are trimmed from the
		// value.
		{
			name:      "trailing spaces trimmed from value",
			line:      "KEY=value   ",
			wantKey:   "KEY",
			wantValue: "value",
		},
		// BOUNDARY: an inline comment needs at least one whitespace byte
		// before '#'; two spaces behave like one.
		{
			name:      "double space before inline comment",
			line:      "KEY=value  # comment",
			wantKey:   "KEY",
			wantValue: "value",
		},
		// BOUNDARY: an unterminated double quote is an invalid value, not a
		// silently-truncated one.
		{
			name:    "unterminated double quote errors",
			line:    `KEY="unterminated`,
			wantErr: true,
		},
		// BOUNDARY: an empty key is rejected by the key validator
		// (ValidateKey's non_empty rule), not silently skipped.
		{
			name:    "empty key errors",
			line:    "=value",
			wantErr: true,
		},
		// BOUNDARY: the export-prefix gate requires len(line) > 7. A line of
		// exactly 7 bytes ("export=") is NOT stripped — it parses as a normal
		// assignment to the key "export" — while 8 bytes ("export =")
		// strips the prefix, leaves "=", and fails key validation.
		{
			name:      "export prefix at exactly 7 bytes is not stripped",
			line:      "export=",
			wantKey:   "export",
			wantValue: "",
		},
		{
			name:    "export prefix at 8 bytes is stripped",
			line:    "export =",
			wantErr: true,
		},
		// PIN: the caller's scanner strips only '\n' and ParseValueBytes
		// trims spaces/tabs only, so a trailing '\r' survives into the value.
		{
			name:      "carriage return kept in value",
			line:      "KEY=value\r",
			wantKey:   "KEY",
			wantValue: "value\r",
		},
		// BOUNDARY: value length against the configured MaxValueLength=1024.
		{
			name:      "value at max length accepted",
			line:      "K=" + strings.Repeat("a", 1024),
			wantKey:   "K",
			wantValue: strings.Repeat("a", 1024),
		},
		{
			name:    "value over max length errors",
			line:    "K=" + strings.Repeat("a", 1025),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, err := lp.ParseLineBytes([]byte(tt.line))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLineBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("ParseLineBytes() key = %q, want %q", key, tt.wantKey)
			}
			if value != tt.wantValue {
				t.Errorf("ParseLineBytes() value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

func TestParseDoubleQuotedBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple",
			input:    `"hello world"`,
			expected: "hello world",
			wantErr:  false,
		},
		{
			name:     "escape newline",
			input:    `"line1\nline2"`,
			expected: "line1\nline2",
			wantErr:  false,
		},
		{
			name:     "escape tab",
			input:    `"col1\tcol2"`,
			expected: "col1\tcol2",
			wantErr:  false,
		},
		{
			name:     "escape quote",
			input:    `"say \"hello\""`,
			expected: `say "hello"`,
			wantErr:  false,
		},
		{
			name:     "escape backslash",
			input:    `"path\\to\\file"`,
			expected: `path\to\file`,
			wantErr:  false,
		},
		{
			name:     "escape dollar",
			input:    `"$VAR"`,
			expected: "$VAR",
			wantErr:  false,
		},
		{
			name:    "unclosed quote",
			input:   `"unclosed`,
			wantErr: true,
		},
		{
			// Unknown escapes keep their backslash (bash semantics):
			// "C:\Users\foo" previously lost every backslash, and "\temp"
			// turned into a TAB.
			name:     "unknown escape keeps the backslash",
			input:    `"C:\Users\foo"`,
			expected: `C:\Users\foo`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDoubleQuotedBytes([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDoubleQuotedBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("ParseDoubleQuotedBytes() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestIsYamlNumberBytes owns the YAML number filter rows ("123"/"3.14"/
// "-123"/"1e10" and variants live here; TestTryParseYamlValueBytes keeps
// only a single representative number row).
func TestIsYamlNumberBytes(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"-123", true},
		{"3.14", true},
		{"-3.14", true},
		{"1e10", true},
		{"1E10", true},
		{"+123", true},
		{"", false},
		{"abc", false},
		{"12abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsYamlNumberBytes([]byte(tt.input)); got != tt.want {
				t.Errorf("IsYamlNumberBytes(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestParseLineBytesScannerBufferSafety tests that parsed values are correctly
// copied and not corrupted when the scanner buffer is reused.
// This is a regression test for the scanner buffer data corruption bug.
// Shrunk to the two rows that matter for the copy paths: the plain unquoted
// control and the escaped double-quoted row (the pooled GetByteSlice copy
// path); the unescaped-double and single-quoted paths are covered by the
// other parser tables.
func TestParseLineBytesScannerBufferSafety(t *testing.T) {
	lp := imNewLineParser(nil, ModeNone, LineParserConfig{AllowExportPrefix: true})

	// Simulate scanner buffer reuse scenario
	// Create a buffer that will be "reused" between parses
	scannerBuffer := make([]byte, 1024)

	tests := []struct {
		name     string
		line1    string
		line2    string
		wantKey1 string
		wantVal1 string
		wantKey2 string
		wantVal2 string
	}{
		{
			name:     "unquoted values",
			line1:    "KEY1=value1",
			line2:    "KEY2=different_value",
			wantKey1: "KEY1",
			wantVal1: "value1",
			wantKey2: "KEY2",
			wantVal2: "different_value",
		},
		{
			// The pooled-buffer copy path: escaped double-quoted values are
			// assembled in GetByteSlice() memory and must be copied out
			// before the buffer is reused.
			name:     "double quoted with escapes",
			line1:    `KEY1="line1\nline2"`,
			line2:    `KEY2="line3\nline4"`,
			wantKey1: "KEY1",
			wantVal1: "line1\nline2",
			wantKey2: "KEY2",
			wantVal2: "line3\nline4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate first scan
			copy(scannerBuffer, tt.line1)
			line1 := scannerBuffer[:len(tt.line1)]

			key1, val1, err := lp.ParseLineBytes(line1)
			if err != nil {
				t.Fatalf("ParseLineBytes(line1) error = %v", err)
			}
			if key1 != tt.wantKey1 {
				t.Errorf("ParseLineBytes(line1) key = %q, want %q", key1, tt.wantKey1)
			}
			if val1 != tt.wantVal1 {
				t.Errorf("ParseLineBytes(line1) value = %q, want %q", val1, tt.wantVal1)
			}

			// Simulate buffer reuse (overwrite with second line)
			copy(scannerBuffer, tt.line2)
			line2 := scannerBuffer[:len(tt.line2)]

			// Parse second line
			key2, val2, err := lp.ParseLineBytes(line2)
			if err != nil {
				t.Fatalf("ParseLineBytes(line2) error = %v", err)
			}
			if key2 != tt.wantKey2 {
				t.Errorf("ParseLineBytes(line2) key = %q, want %q", key2, tt.wantKey2)
			}
			if val2 != tt.wantVal2 {
				t.Errorf("ParseLineBytes(line2) value = %q, want %q", val2, tt.wantVal2)
			}

			// CRITICAL: First value should still be intact after buffer reuse
			if val1 != tt.wantVal1 {
				t.Errorf("DATA CORRUPTION: val1 changed from %q to %q after buffer reuse",
					tt.wantVal1, val1)
			}
		})
	}
}

// ============================================================================
// KeysToUpperPooled Tests
// ============================================================================

func TestKeysToUpperPooled(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]bool
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: map[string]bool{},
		},
		{
			name:     "nil map",
			input:    nil,
			expected: map[string]bool{},
		},
		{
			name:     "single key",
			input:    map[string]string{"key": "value"},
			expected: map[string]bool{"KEY": true},
		},
		{
			name:     "multiple keys",
			input:    map[string]string{"key1": "v1", "KEY2": "v2", "Key3": "v3"},
			expected: map[string]bool{"KEY1": true, "KEY2": true, "KEY3": true},
		},
		{
			name:     "with empty key",
			input:    map[string]string{"": "empty", "VALID": "value"},
			expected: map[string]bool{"VALID": true},
		},
		{
			name:     "mixed case keys",
			input:    map[string]string{"Database_Host": "localhost", "DATABASE_PORT": "5432"},
			expected: map[string]bool{"DATABASE_HOST": true, "DATABASE_PORT": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KeysToUpperPooled(tt.input)
			defer PutKeysToUpperMap(result)

			// Check all expected keys are present
			for k := range tt.expected {
				if !result[k] {
					t.Errorf("KeysToUpperPooled() missing key %q", k)
				}
			}

			// Check result has expected number of keys
			if len(result) != len(tt.expected) {
				t.Errorf("KeysToUpperPooled() returned %d keys, want %d", len(result), len(tt.expected))
			}
		})
	}

	// Folded in from the former TestPutKeysToUpperMap_Nil: returning a nil
	// map to the pool must not panic.
	t.Run("PutKeysToUpperMap nil is safe", func(t *testing.T) {
		PutKeysToUpperMap(nil)
	})
}

func TestKeysToUpperPooled_LargeMap(t *testing.T) {
	// Test with a map larger than MaxPooledMapSize to ensure it still works
	input := make(map[string]string, MaxPooledMapSize+10)
	for i := 0; i < MaxPooledMapSize+10; i++ {
		input[fmt.Sprintf("KEY_%d", i)] = fmt.Sprintf("value_%d", i)
	}

	result := KeysToUpperPooled(input)
	// Should still work, just won't be pooled on return
	if len(result) != MaxPooledMapSize+10 {
		t.Errorf("KeysToUpperPooled() returned %d keys, want %d", len(result), MaxPooledMapSize+10)
	}

	// Safe to call Put even for large maps
	PutKeysToUpperMap(result)
}

// TestParseValueBytes owns the value-semantics coverage (ParseLineBytes
// delegates here): quoting, whitespace, comment stripping and the '#'
// boundary pins.
func TestParseValueBytes(t *testing.T) {
	lp := imNewLineParser(nil, ModeNone, LineParserConfig{})

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"simple value", "hello", "hello", false},
		{"double quoted", `"hello world"`, "hello world", false},
		{"single quoted", "'hello world'", "hello world", false},
		{"with trailing comment", "value # comment", "value", false},
		{"with leading whitespace", "  value  ", "value", false},
		// PIN: a '#' only starts a comment when preceded by a space
		// (the parser looks for the " #" pair), so an inline '#' survives.
		{"hash without preceding space stays", "val#ue", "val#ue", false},
		// PIN: after trimming the leading whitespace, a value that STARTS
		// with '#' is not a comment either — only " #" pairs are stripped.
		{"value-leading hash after trim is not a comment", " # comment", "# comment", false},
		// A closing quote followed by whitespace + '#' is accepted — these
		// previously failed the whole file (the quoted parsers demanded the
		// final byte be the quote).
		{"double quoted with trailing comment", `"value" # comment`, "value", false},
		{"single quoted keeps interior hash", `'lit # not a comment'`, "lit # not a comment", false},
		{"junk after closing quote errors", `"value" x`, "", true},
		{"comment glued to closing quote errors", `"value"#c`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lp.ParseValueBytes([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseValueBytes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseValueBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestParseValueBytes_YamlSyntax wires AllowYamlSyntax through a LineParser —
// previously the flag was never set in any test. Bare YAML scalars are
// accepted when the flag is on and treated as ordinary strings when off.
// For true/123 the pass-through rendering is identical either way; the
// observable difference is the null family (~, null), which collapses to the
// empty string only when the flag is enabled.
func TestParseValueBytes_YamlSyntax(t *testing.T) {
	off := imNewLineParser(nil, ModeNone, LineParserConfig{})
	on := imNewLineParser(nil, ModeNone, LineParserConfig{AllowYamlSyntax: true})

	tests := []struct {
		name    string
		input   string
		wantOff string
		wantOn  string
	}{
		{"bool true", "true", "true", "true"},
		{"bool True", "True", "True", "True"},
		{"null collapses when enabled", "null", "null", ""},
		{"tilde collapses when enabled", "~", "~", ""},
		{"integer passes through", "123", "123", "123"},
		{"float passes through", "3.14", "3.14", "3.14"},
		{"plain word unaffected", "hello", "hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOff, err := off.ParseValueBytes([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseValueBytes(yaml off, %q) error = %v", tt.input, err)
			}
			if gotOff != tt.wantOff {
				t.Errorf("ParseValueBytes(yaml off, %q) = %q, want %q", tt.input, gotOff, tt.wantOff)
			}

			gotOn, err := on.ParseValueBytes([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseValueBytes(yaml on, %q) error = %v", tt.input, err)
			}
			if gotOn != tt.wantOn {
				t.Errorf("ParseValueBytes(yaml on, %q) = %q, want %q", tt.input, gotOn, tt.wantOn)
			}
		})
	}
}

func TestParseSingleQuotedBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid", "'hello'", "hello", false},
		{"empty quotes", "''", "", false},
		// Single-quoted values do not process escape sequences: \n stays literal.
		{"no escape processing", `'line1\nline2'`, "line1\\nline2", false},
		{"too short", "'", "", true},
		{"missing closing quote", "'hello", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSingleQuotedBytes([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSingleQuotedBytes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseSingleQuotedBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTryParseYamlValueBytes keeps the null/~/bool coverage plus ONE number
// row; the number filter matrix belongs to TestIsYamlNumberBytes.
func TestTryParseYamlValueBytes(t *testing.T) {
	tests := []struct {
		input     string
		wantVal   string
		wantMatch bool
	}{
		{"true", "true", true},
		{"True", "True", true},
		{"TRUE", "TRUE", true},
		{"false", "false", true},
		{"False", "False", true},
		{"null", "", true},
		{"Null", "", true},
		{"NULL", "", true},
		{"~", "", true},
		{"123", "123", true}, // one representative number; see TestIsYamlNumberBytes
		{"hello", "", false},
		{"", "", false},
		{"abc", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotVal, gotMatch := TryParseYamlValueBytes([]byte(tt.input))
			if gotMatch != tt.wantMatch {
				t.Errorf("TryParseYamlValueBytes(%q) match = %v, want %v", tt.input, gotMatch, tt.wantMatch)
			}
			if gotVal != tt.wantVal {
				t.Errorf("TryParseYamlValueBytes(%q) = %q, want %q", tt.input, gotVal, tt.wantVal)
			}
		})
	}
}

// imSubstExpander is a non-*Expander LineExpander that substitutes $K and
// ${K} references from its map — close enough to *Expander semantics for the
// shared imExpandAllCases table while still driving the interface fallback
// path (expandAllUsingInterface). It performs no cycle detection and honors
// no Mode; cases needing either are marked skipInterface. Substitution is a
// single pass over the original map (braced forms first), so chained
// references beyond one hop are not supported — the shared cases avoid them.
type imSubstExpander struct {
	vars map[string]string
}

func (m *imSubstExpander) Expand(s string) (string, error) {
	for k, v := range m.vars {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
		s = strings.ReplaceAll(s, "$"+k, v)
	}
	return s, nil
}

// imFailExpander fails every expansion, for the interface path's
// error-propagation branch.
type imFailExpander struct {
	err error
}

func (f *imFailExpander) Expand(string) (string, error) { return "", f.err }

// TestExpandAllUsingInterface runs the shared ExpandAll case table through
// LineParser.ExpandAll with a non-*Expander LineExpander (the interface
// fallback), then pins the error-propagation branch with a failing expander.
func TestExpandAllUsingInterface(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxKeyLength: 64})
	a := NewAuditor(nil, nil, nil, false)

	for _, tt := range imExpandAllCases {
		if tt.skipInterface {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			// The mock resolves references against the union of the map
			// being expanded and the case's external lookup vars.
			mockVars := make(map[string]string, len(tt.vars)+len(tt.external))
			for k, val := range tt.vars {
				mockVars[k] = val
			}
			for k, val := range tt.external {
				mockVars[k] = val
			}
			lp := NewLineParser(LineParserConfig{}, v, a, &imSubstExpander{vars: mockVars})

			result, err := lp.ExpandAll(tt.vars, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExpandAll() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for k, want := range tt.expected {
				if result[k] != want {
					t.Errorf("ExpandAll()[%q] = %q, want %q", k, result[k], want)
				}
			}
		})
	}

	t.Run("expansion error propagates", func(t *testing.T) {
		lp := NewLineParser(LineParserConfig{}, v, a, &imFailExpander{err: fmt.Errorf("expansion failed")})
		_, err := lp.ExpandAll(map[string]string{"KEY1": "$FAIL"}, nil)
		if err == nil {
			t.Error("ExpandAll() should return error on expansion failure")
		}
	})

	// BOUNDARY: single-quoted (literal) values are exempt for custom
	// expanders too — the failing expander proves Expand is never called on
	// the exempt value.
	t.Run("literal values are exempt for custom expanders too", func(t *testing.T) {
		lp := NewLineParser(LineParserConfig{}, v, a, &imFailExpander{err: fmt.Errorf("must not be called on a literal")})
		result, err := lp.ExpandAll(map[string]string{"PW": "$BOOM"}, map[string]struct{}{"PW": {}})
		if err != nil {
			t.Fatalf("ExpandAll() error = %v, want nil (literal value skipped)", err)
		}
		if result["PW"] != "$BOOM" {
			t.Errorf("ExpandAll()[PW] = %q, want %q inserted verbatim", result["PW"], "$BOOM")
		}
	})
}

// TestExpandAll_RequiredErrorLoggedWithCause pins the built-in path's error
// logging branch for ":?" required variables (audit.go previously collapsed
// these into "cycle detected"): the returned error is the ExpansionRequired
// kind, and the auditor (a no-op here) is consulted with the actual cause.
func TestExpandAll_RequiredErrorLoggedWithCause(t *testing.T) {
	lp := imNewLineParser(nil, ModeAll, LineParserConfig{ExpandVariables: true})

	_, err := lp.ExpandAll(map[string]string{"KEY": "${MISSING:?req}"}, nil)
	if err == nil {
		t.Fatal("ExpandAll() error = nil, want required-variable error")
	}
	var expErr *ExpansionError
	if !errors.As(err, &expErr) {
		t.Fatalf("ExpandAll() error = %T, want *ExpansionError", err)
	}
	if expErr.Kind != ExpansionRequiredKind {
		t.Errorf("ExpansionError.Kind = %v, want ExpansionRequiredKind", expErr.Kind)
	}
}

// TestParseLineBytesQuote_ProvenanceAndExpansion pins the quote-provenance
// channel end to end at the internal layer: single-quoted values are reported
// and exempted from expansion (a '$' inside them stays literal), double-quoted
// \$ survives the two-pass design as $$ and folds back to one dollar, and
// values referencing a literal value adopt it without rescanning.
func TestParseLineBytesQuote_ProvenanceAndExpansion(t *testing.T) {
	lp := imNewLineParser(nil, ModeAll, LineParserConfig{ExpandVariables: true})

	lines := []string{
		`PASSWORD='my$ecret'`,
		`NAME="world"`,
		`GREET="hello $NAME"`,
		`PRICE="cost: \$5"`,
		`REF=$NAME`,
	}
	vars := map[string]string{}
	noExpand := map[string]struct{}{}
	for _, ln := range lines {
		key, value, singleQuoted, err := lp.ParseLineBytesQuote([]byte(ln))
		if err != nil {
			t.Fatalf("ParseLineBytesQuote(%q) error = %v", ln, err)
		}
		vars[key] = value
		if singleQuoted {
			noExpand[key] = struct{}{}
		} else {
			delete(noExpand, key)
		}
	}
	if _, ok := noExpand["PASSWORD"]; !ok {
		t.Error("PASSWORD should be reported as single-quoted")
	}
	if _, ok := noExpand["NAME"]; ok {
		t.Error("double-quoted NAME must not be flagged as literal")
	}

	out, err := lp.ExpandAll(vars, noExpand)
	if err != nil {
		t.Fatalf("ExpandAll() error = %v", err)
	}
	if got := out["PASSWORD"]; got != "my$ecret" {
		t.Errorf("PASSWORD = %q, want literal %q (single quotes suppress expansion)", got, "my$ecret")
	}
	if got := out["GREET"]; got != "hello world" {
		t.Errorf("GREET = %q, want %q", got, "hello world")
	}
	if got := out["PRICE"]; got != `cost: $5` {
		t.Errorf("PRICE = %q, want %q (escaped dollar survives expansion)", got, `cost: $5`)
	}
	if got := out["REF"]; got != "world" {
		t.Errorf("REF = %q, want %q", got, "world")
	}
}

// TestParseLineBytesQuote_OverwriteClearsProvenance pins that the LAST write
// to a key owns its quote provenance: a single-quoted value overwritten by an
// unquoted one goes back into the expansion set.
func TestParseLineBytesQuote_OverwriteClearsProvenance(t *testing.T) {
	lp := imNewLineParser(nil, ModeAll, LineParserConfig{ExpandVariables: true})

	_, first, single, err := lp.ParseLineBytesQuote([]byte(`A='x$y'`))
	if err != nil || !single || first != "x$y" {
		t.Fatalf("first parse = (%q, %v, %v), want (x$y, true, nil)", first, single, err)
	}
	_, second, single, err := lp.ParseLineBytesQuote([]byte(`A=x$y`))
	if err != nil || single || second != "x$y" {
		t.Fatalf("second parse = (%q, %v, %v), want (x$y, false, nil)", second, single, err)
	}

	out, err := lp.ExpandAll(map[string]string{"A": second}, nil)
	if err != nil {
		t.Fatalf("ExpandAll() error = %v", err)
	}
	if got := out["A"]; got != "x" {
		t.Errorf("A = %q, want %q (unquoted value IS expanded)", got, "x")
	}
}

// TestParseLineBytes_ColonSeparatorGated pins AllowYamlSyntax gating ':' as
// a key/value separator: with the flag off (the default) a "KEY:value" line
// is dropped as a no-separator line instead of silently becoming a variable;
// with the flag on it parses like '='.
func TestParseLineBytes_ColonSeparatorGated(t *testing.T) {
	off := imNewLineParser(nil, ModeNone, LineParserConfig{})
	on := imNewLineParser(nil, ModeNone, LineParserConfig{AllowYamlSyntax: true})

	if key, value, err := off.ParseLineBytes([]byte("KEY:value")); err != nil || key != "" || value != "" {
		t.Errorf("yaml off: ParseLineBytes = (%q, %q, %v), want dropped no-separator line", key, value, err)
	}
	key, value, err := on.ParseLineBytes([]byte("KEY:value"))
	if err != nil || key != "KEY" || value != "value" {
		t.Errorf("yaml on: ParseLineBytes = (%q, %q, %v), want (KEY, value, nil)", key, value, err)
	}
	// '=' still separates in both modes, and ':' inside a VALUE after '=' is
	// not a separator.
	for _, lp := range []*LineParser{off, on} {
		if k, v, err := lp.ParseLineBytes([]byte("URL=https://example.com")); err != nil || k != "URL" || v != "https://example.com" {
			t.Errorf("ParseLineBytes(URL line) = (%q, %q, %v), want (URL, https://example.com, nil)", k, v, err)
		}
	}
}

// TestParseLineBytes_ExportTabPrefix pins that a tab is accepted after the
// export prefix (bash accepts any whitespace; only a space was recognized).
func TestParseLineBytes_ExportTabPrefix(t *testing.T) {
	lp := imNewLineParser(nil, ModeNone, LineParserConfig{AllowExportPrefix: true})
	key, value, err := lp.ParseLineBytes([]byte("export\tKEY=value"))
	if err != nil || key != "KEY" || value != "value" {
		t.Errorf("ParseLineBytes(export<TAB>) = (%q, %q, %v), want (KEY, value, nil)", key, value, err)
	}
}

// TestParseValueBytes_TabComment pins that a '#' preceded by a TAB starts an
// inline comment, matching the space form (the tab form previously stayed in
// the value).
func TestParseValueBytes_TabComment(t *testing.T) {
	lp := imNewLineParser(nil, ModeNone, LineParserConfig{})
	got, err := lp.ParseValueBytes([]byte("value\t# comment"))
	if err != nil {
		t.Fatalf("ParseValueBytes() error = %v", err)
	}
	if got != "value" {
		t.Errorf("ParseValueBytes() = %q, want %q", got, "value")
	}
}
