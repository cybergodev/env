package internal

import (
	"strconv"
	"strings"
	"testing"
)

// ============================================================================
// YAML Lexer Tests
// ============================================================================

// iyTokenize lexes input and fails the test on a lexer error.
func iyTokenize(t *testing.T, input string) []Token {
	t.Helper()
	lexer := newYAMLLexer([]byte(input))
	tokens, err := lexer.tokenizeInto(nil)
	if err != nil {
		t.Fatalf("tokenizeInto(%q) error = %v", input, err)
	}
	return tokens
}

// TestYAMLLexer_TokenSequences pins complete token streams for the basic
// single- and multi-line shapes: key/value order, document separators, dash
// handling, quoting, line/indent bookkeeping, and every supported line
// ending (LF, CRLF, bare CR).
//
// Matching semantics: each want entry is matched against the token at the
// same position (prefix match — the stream may be longer). A field left at
// its zero value in a want entry is not compared; a TokenEOF want entry
// must additionally be the final token of the stream.
func TestYAMLLexer_TokenSequences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []struct {
			typ    TokenType
			val    string
			indent int
			line   int
		}
	}{
		{
			name:  "simple key and value",
			input: "key: value",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "key", 0, 1},
				{TokenValue, "value", 0, 1},
				{TokenEOF, "", 0, 1},
			},
		},
		{
			name:  "nested keys carry indent level",
			input: "database:\n  host: localhost",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "database", 0, 1},
				{TokenNewline, "", 0, 1},
				{TokenIndent, "", 1, 2},
				{TokenKey, "host", 1, 2},
				{TokenValue, "localhost", 1, 2},
			},
		},
		{
			name:  "document start then key",
			input: "---\nkey: value",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenDocumentStart, "", 0, 1},
				{TokenNewline, "", 0, 1},
				{TokenKey, "key", 0, 2},
				{TokenValue, "value", 0, 2},
				{TokenEOF, "", 0, 2},
			},
		},
		{
			// "---" exactly at EOF still scans as a document separator
			// (isDocumentStart treats end-of-input as a valid follower).
			name:  "document start at end of input",
			input: "---",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenDocumentStart, "", 0, 1},
				{TokenEOF, "", 0, 1},
			},
		},
		{
			// "---" followed by a non-space character is NOT a document
			// separator: the line classifies as a plain key whose value
			// happens to start with three dashes.
			name:  "triple dash followed by non-space stays a key",
			input: "---key: v",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "---key", 0, 1},
				{TokenValue, "v", 0, 1},
				{TokenEOF, "", 0, 1},
			},
		},
		{
			name:  "dash inside key",
			input: "api-key: value",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "api-key", 0, 1},
				{TokenValue, "value", 0, 1},
				{TokenEOF, "", 0, 1},
			},
		},
		{
			name:  "colon inside quoted value",
			input: `url: "https://example.com:8080"`,
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "url", 0, 1},
				{TokenValue, "https://example.com:8080", 0, 1},
				{TokenEOF, "", 0, 1},
			},
		},
		{
			name:  "line tracking across lines",
			input: "key1: value1\nkey2: value2",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "key1", 0, 1},
				{TokenValue, "value1", 0, 1},
				{TokenNewline, "", 0, 1},
				{TokenKey, "key2", 0, 2},
				{TokenValue, "value2", 0, 2},
				{TokenEOF, "", 0, 2},
			},
		},
		{
			name:  "empty input yields only EOF",
			input: "",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenEOF, "", 0, 1},
			},
		},
		{
			name:  "CRLF line endings",
			input: "key1: value1\r\nkey2: value2",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "key1", 0, 1},
				{TokenValue, "value1", 0, 1},
				{TokenNewline, "", 0, 1},
				{TokenKey, "key2", 0, 2},
				{TokenValue, "value2", 0, 2},
				{TokenEOF, "", 0, 2},
			},
		},
		{
			// Bare CR is a full line ending (handleNewline accepts CR,
			// CRLF and LF): the value stops before the CR and the next
			// key starts on line 2.
			name:  "bare CR line ending",
			input: "key: value\rnext: v",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "key", 0, 1},
				{TokenValue, "value", 0, 1},
				{TokenNewline, "", 0, 1},
				{TokenKey, "next", 0, 2},
				{TokenValue, "v", 0, 2},
				{TokenEOF, "", 0, 2},
			},
		},
		{
			// Tabs count as 2 spaces of column width each: two tabs put the
			// line one NESTING level deeper (column 4 > 0). The level is
			// the stack position, not the column width — the old width/2
			// arithmetic reported "2" for a single level of nesting.
			name:  "tabs count as two spaces",
			input: "root:\n\t\tnested: value",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "root", 0, 1},
				{TokenNewline, "", 0, 1},
				{TokenIndent, "", 1, 2},
				{TokenKey, "nested", 1, 2},
				{TokenValue, "value", 1, 2},
				{TokenDedent, "", 0, 2},
				{TokenEOF, "", 0, 2},
			},
		},
		{
			// Non-ASCII keys and values pass through the scanners
			// byte-for-byte; case folding never applies at token level.
			name:  "unicode key and value preserved",
			input: "键: 值",
			want: []struct {
				typ    TokenType
				val    string
				indent int
				line   int
			}{
				{TokenKey, "键", 0, 1},
				{TokenValue, "值", 0, 1},
				{TokenEOF, "", 0, 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := iyTokenize(t, tt.input)
			if len(tokens) < len(tt.want) {
				t.Fatalf("got %d tokens, want at least %d", len(tokens), len(tt.want))
			}
			for i, w := range tt.want {
				got := tokens[i]
				if got.Type != w.typ {
					t.Errorf("token[%d].Type = %v, want %v", i, got.Type, w.typ)
					continue
				}
				if w.val != "" && got.Value != w.val {
					t.Errorf("token[%d].Value = %q, want %q", i, got.Value, w.val)
				}
				if w.line != 0 && got.Line != w.line {
					t.Errorf("token[%d].Line = %d, want %d", i, got.Line, w.line)
				}
				if w.indent != 0 && got.Indent != w.indent {
					t.Errorf("token[%d].Indent = %d, want %d", i, got.Indent, w.indent)
				}
				if w.typ == TokenEOF && i != len(tokens)-1 {
					t.Errorf("token[%d] is EOF but %d tokens follow", i, len(tokens)-1-i)
				}
			}
		})
	}
}

// TestYAMLLexer_TokenCounts pins the exact per-type token counts for the
// canonical multi-line shapes (indent/dedent balance included).
func TestYAMLLexer_TokenCounts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[TokenType]int
	}{
		{
			name:  "nested keys",
			input: "database:\n  host: localhost\n  port: 5432",
			want: map[TokenType]int{
				TokenKey:     3,
				TokenValue:   2,
				TokenNewline: 2,
				TokenIndent:  1,
				TokenDedent:  1,
				TokenEOF:     1,
			},
		},
		{
			name:  "array items",
			input: "items:\n  - one\n  - two\n  - three",
			want: map[TokenType]int{
				TokenKey:     1,
				TokenValue:   3,
				TokenDash:    3,
				TokenNewline: 3,
				TokenIndent:  1,
				TokenDedent:  1,
				TokenEOF:     1,
			},
		},
		{
			name:  "comments",
			input: "# This is a comment\nkey: value # inline comment",
			want: map[TokenType]int{
				TokenComment: 2,
				TokenKey:     1,
				TokenValue:   1,
				TokenNewline: 1,
				TokenEOF:     1,
			},
		},
		{
			name:  "multiple documents",
			input: "---\ndoc1: value\n---\ndoc2: value2",
			want: map[TokenType]int{
				TokenDocumentStart: 2,
				TokenKey:           2,
				TokenValue:         2,
				TokenNewline:       3,
				TokenEOF:           1,
			},
		},
		{
			// Exact balance, verified against handleNewline: level 1 and
			// level 2 pushes emit 2 Indent tokens; the mid-document dedent
			// to level 1 and the EOF cleanup together emit 2 Dedent tokens.
			name:  "indent and dedent balance",
			input: "root:\n  nested:\n    deep: value\n  back: here",
			want: map[TokenType]int{
				TokenKey:     4,
				TokenValue:   2,
				TokenNewline: 3,
				TokenIndent:  2,
				TokenDedent:  2,
				TokenEOF:     1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := iyTokenize(t, tt.input)
			got := make(map[TokenType]int)
			for _, tok := range tokens {
				got[tok.Type]++
			}
			if len(got) != len(tt.want) {
				t.Fatalf("token type counts = %v, want exactly %v", got, tt.want)
			}
			for typ, n := range tt.want {
				if got[typ] != n {
					t.Errorf("token type %v: got %d, want %d (all counts: %v)", typ, got[typ], n, got)
				}
			}
		})
	}
}

// TestYAMLLexer_IndentDepthCap pins the DoS guard in handleNewline: the
// indent stack holds at most maxIndentDepth (256) entries, so a document of
// successively deeper lines is rejected with a YAMLError once the 256th
// indent increase would be pushed. 255 increases (stack depth 256 including
// the root level) still tokenize cleanly.
func TestYAMLLexer_IndentDepthCap(t *testing.T) {
	build := func(levels int) string {
		var b strings.Builder
		b.WriteString("k0: v")
		for i := 1; i <= levels; i++ {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", 2*i))
			b.WriteString("k" + strconv.Itoa(i) + ": v")
		}
		return b.String()
	}

	t.Run("255 deeper lines still tokenize", func(t *testing.T) {
		tokens := iyTokenize(t, build(255))
		indents := 0
		for _, tok := range tokens {
			if tok.Type == TokenIndent {
				indents++
			}
		}
		if indents != 255 {
			t.Errorf("indent tokens = %d, want 255", indents)
		}
	})

	t.Run("256th deeper line hits the cap", func(t *testing.T) {
		lexer := newYAMLLexer([]byte(build(300)))
		_, err := lexer.tokenizeInto(nil)
		if err == nil {
			t.Fatal("tokenizeInto(deeply indented) error = nil, want indent-depth error")
		}
		if !strings.Contains(err.Error(), "maximum indent depth exceeded") {
			t.Errorf("error = %v, want it to mention the indent depth cap", err)
		}
	})
}

// TestYAMLLexer_ColonSpaceInsideValue pins the flow-depth rule: a ':' (or
// comment '#') only ends a value at brace depth 0. Inside an inline
// JSON/flow region both are data, so values embedding JSON with ": "
// separators scan as one token instead of being misread as a key.
func TestYAMLLexer_ColonSpaceInsideValue(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantKey string
		wantVal string
	}{
		{
			name:    "cookie string embedding spaced JSON",
			input:   `apnews.com: Code=abc; g_state={"i_l": 0}`,
			wantKey: "apnews.com",
			wantVal: `Code=abc; g_state={"i_l": 0}`,
		},
		{
			name:    "value that is spaced JSON",
			input:   `g_state: {"i_l": 0, "i_e": {"x": 1}}`,
			wantKey: "g_state",
			wantVal: `{"i_l": 0, "i_e": {"x": 1}}`,
		},
		{
			name:    "colons and hash inside JSON strings are data",
			input:   `k: {"a": "b:c #d}", "e": [1, 2]}`,
			wantKey: "k",
			wantVal: `{"a": "b:c #d}", "e": [1, 2]}`,
		},
		{
			name:    "escaped quote inside JSON string keeps brace balance",
			input:   `k: {"a": "quote \" brace }"}`,
			wantKey: "k",
			wantVal: `{"a": "quote \" brace }"}`,
		},
		{
			name:    "bracket flow region with colon-space",
			input:   `k: arr=[{"i": 2}]; x=1`,
			wantKey: "k",
			wantVal: `arr=[{"i": 2}]; x=1`,
		},
		{
			name:    "comment after closed flow region still applies",
			input:   `k: {"a": 1} # note`,
			wantKey: "k",
			wantVal: `{"a": 1}`,
		},
		{
			name:    "unclosed flow region scans to end of line (lenient)",
			input:   `k: {"a": 1`,
			wantKey: "k",
			wantVal: `{"a": 1`,
		},
		{
			name:    "comment at depth 0 after plain value unchanged",
			input:   `k: v # c`,
			wantKey: "k",
			wantVal: `v`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := iyTokenize(t, tt.input)

			var key, val *Token
			for i := range tokens {
				switch tokens[i].Type {
				case TokenKey:
					if key == nil {
						key = &tokens[i]
					}
				case TokenValue:
					if val == nil {
						val = &tokens[i]
					}
				}
			}
			if key == nil || val == nil {
				t.Fatalf("expected a key and a value token, got %d tokens", len(tokens))
			}
			if key.Value != tt.wantKey {
				t.Errorf("key token = %q, want %q", key.Value, tt.wantKey)
			}
			if val.Value != tt.wantVal {
				t.Errorf("value token = %q, want %q", val.Value, tt.wantVal)
			}
			// The first value on the line must be the ONLY value token: a
			// ": " inside the value must not split it into extra tokens.
			valueCount := 0
			for _, tok := range tokens {
				if tok.Type == TokenValue {
					valueCount++
				}
			}
			if valueCount != 1 {
				t.Errorf("got %d value tokens, want 1 (value was split)", valueCount)
			}
		})
	}
}

// TestYAMLLexer_ColonSpaceAtDepthZeroStillSplitsKeys pins the unchanged
// counterpart of the flow-depth rule: at brace depth 0 a colon+space still
// terminates the scalar, preserving "outer: inner: value" inline maps.
func TestYAMLLexer_ColonSpaceAtDepthZeroStillSplitsKeys(t *testing.T) {
	tokens := iyTokenize(t, `outer: inner: value`)

	var got []Token
	for _, tok := range tokens {
		if tok.Type == TokenKey || tok.Type == TokenValue {
			got = append(got, tok)
		}
	}
	want := []struct {
		typ   TokenType
		value string
	}{
		{TokenKey, "outer"},
		{TokenKey, "inner"},
		{TokenValue, "value"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d key/value tokens, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Type != w.typ || got[i].Value != w.value {
			t.Errorf("token %d = (%v, %q), want (%v, %q)", i, got[i].Type, got[i].Value, w.typ, w.value)
		}
	}
}

func TestYAMLLexer_QuotedStrings(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantKey string
		wantVal string
	}{
		{
			name:    "double quoted value",
			input:   `key: "value with spaces"`,
			wantKey: "key",
			wantVal: "value with spaces",
		},
		{
			name:    "single quoted value",
			input:   `key: 'value with spaces'`,
			wantKey: "key",
			wantVal: "value with spaces",
		},
		{
			name:    "double quoted key",
			input:   `"quoted key": value`,
			wantKey: "quoted key",
			wantVal: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := iyTokenize(t, tt.input)
			if len(tokens) < 2 {
				t.Fatalf("expected at least 2 tokens, got %d", len(tokens))
			}

			if tokens[0].Type != TokenKey || tokens[0].Value != tt.wantKey {
				t.Errorf("key token = %q, want %q", tokens[0].Value, tt.wantKey)
			}

			if tokens[1].Type != TokenValue || tokens[1].Value != tt.wantVal {
				t.Errorf("value token = %q, want %q", tokens[1].Value, tt.wantVal)
			}
		})
	}
}

func TestYAMLLexer_EscapeSequences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"newline", `"value\nline2"`, "value\nline2"},
		{"tab", `"value\ttab"`, "value\ttab"},
		{"carriage return", `"value\rreturn"`, "value\rreturn"},
		{"backslash", `"path\\to\\file"`, "path\\to\\file"},
		{"escaped quote", `"say \"hello\""`, `say "hello"`},
		// YAML single-quoted scalars are literal: a backslash survives
		// verbatim and only a doubled quote folds. The historical
		// escape-processing form ('\'' → ') was a YAML spec violation that
		// corrupted Windows paths.
		{"single-quoted backslash is literal", `'C:\Users\app'`, `C:\Users\app`},
		{"single-quoted trailing backslash", `'C:\Users\'`, `C:\Users\`},
		{"single-quoted doubled quote folds", `'it''s'`, `it's`},
		{
			// Unknown escapes hit scanQuotedString's default branch, which
			// writes ONLY the escaped character: the backslash is dropped,
			// so "\x" renders as "x" (not "\x" and not an error).
			"unknown escape drops the backslash",
			`"a\xz"`,
			"axz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := iyTokenize(t, "key: "+tt.input)

			// Find the value token
			for _, tok := range tokens {
				if tok.Type == TokenValue {
					if tok.Value != tt.expected {
						t.Errorf("value = %q, want %q", tok.Value, tt.expected)
					}
					return
				}
			}
			t.Error("no value token found")
		})
	}
}

// TestYAMLLexer_ErrorInputs collects the lexer's quoted-string error
// contracts in one table: a missing closing quote, a trailing backslash
// (escape at end of input — a different branch from the missing quote, which
// the former escape-at-EOF test never actually reached: its `\"` consumed
// the quote and errored as unterminated instead), a raw newline inside the
// quotes, and the null-byte escape (SECURITY: \0 is rejected at lexer level
// for defense in depth — null bytes enable log truncation, string truncation
// in C interop, and bypass of controls that expect no nulls).
func TestYAMLLexer_ErrorInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"unterminated string", `key: "unterminated`, "unterminated"},
		{"escape sequence at end of input", `key: "value\`, "escape sequence"},
		{"newline inside quoted string", "key: \"abc\nnext: v", "unterminated"},
		{"null byte escape rejected", `key: "value\0null"`, "null byte"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := newYAMLLexer([]byte(tt.input))
			_, err := lexer.tokenizeInto(nil)
			if err == nil {
				t.Fatalf("tokenize(%q) error = nil, want %q", tt.input, tt.wantMsg)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("tokenize(%q) error = %q, want it to mention %q", tt.input, err, tt.wantMsg)
			}
		})
	}
}

func TestYAMLLexer_NextToken(t *testing.T) {
	lexer := newYAMLLexer([]byte("key: value"))

	// Test NextToken one at a time
	tok, err := lexer.NextToken()
	if err != nil {
		t.Fatalf("NextToken() error = %v", err)
	}
	if tok.Type != TokenKey {
		t.Errorf("first token type = %v, want TokenKey", tok.Type)
	}

	tok, err = lexer.NextToken()
	if err != nil {
		t.Fatalf("NextToken() error = %v", err)
	}
	if tok.Type != TokenValue {
		t.Errorf("second token type = %v, want TokenValue", tok.Type)
	}

	tok, err = lexer.NextToken()
	if err != nil {
		t.Fatalf("NextToken() error = %v", err)
	}
	if tok.Type != TokenEOF {
		t.Errorf("third token type = %v, want TokenEOF", tok.Type)
	}
}
