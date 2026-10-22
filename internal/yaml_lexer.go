// Provides YAML lexing utilities.

package internal

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
)

// lexerBufferPool provides a pool of reusable bytes.Buffer instances.
// This reduces allocations for frequent buffer operations in the YAML lexer.
var lexerBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// getLexerBuffer retrieves a bytes.Buffer from the pool.
// The buffer is reset before use.
func getLexerBuffer() *bytes.Buffer {
	buf, ok := lexerBufferPool.Get().(*bytes.Buffer)
	if !ok {
		// Fallback: create new buffer if pool returns unexpected type
		return new(bytes.Buffer)
	}
	buf.Reset()
	return buf
}

// putLexerBuffer returns a bytes.Buffer to the pool.
// Buffers with capacity exceeding 4096 bytes are discarded
// to prevent memory bloat.
func putLexerBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// Don't pool very large buffers
	if buf.Cap() <= 4096 {
		lexerBufferPool.Put(buf)
	}
}

// TokenType represents the type of a YAML token.
type TokenType int

const (
	// TokenEOF marks the end of input.
	TokenEOF TokenType = iota
	// TokenNewline represents a newline character.
	TokenNewline
	// TokenDocumentStart represents a document separator (---).
	TokenDocumentStart
	// TokenKey represents a key in a key-value pair.
	TokenKey
	// TokenValue represents a scalar value.
	TokenValue
	// TokenDash represents the start of an array item.
	TokenDash
	// TokenColon represents the colon separator. It is never emitted by the
	// Lexer (scanKey consumes the colon inline); the constant is kept as a
	// synthetic "unknown token" fixture for parser tests.
	TokenColon
	// TokenComment represents a comment.
	TokenComment
	// TokenIndent represents increased indentation.
	TokenIndent
	// TokenDedent represents decreased indentation. Indent carries the
	// indentation level being dedented TO (the target level), consistently
	// between the buffered and EOF emit paths.
	TokenDedent
)

// Token represents a YAML token.
type Token struct {
	Type     TokenType
	Value    string
	Line     int
	Column   int
	Indent   int // Nesting depth (0-based stack position of the line's column)
	IsQuoted bool
}

// lexerPool pools Lexer structs to avoid per-parse allocation.
var lexerPool = sync.Pool{
	New: func() any {
		return &Lexer{
			indents: make([]int, 1, 8),
		}
	},
}

// Lexer tokenizes YAML input.
type Lexer struct {
	input     []byte
	pos       int
	line      int
	column    int
	indent    int     // Current indentation level
	indents   []int   // Stack of indentation levels
	tokens    []Token // Buffered tokens
	tokenHead int     // Consumption index into tokens (see NextToken)
	buffered  bool    // Whether we have buffered tokens
	eof       bool    // Whether we've reached EOF
}

// newYAMLLexer creates a lexer over a byte slice without copying it.
// This is the hot-path entry used by ParseYAML: the input aliases the
// caller's read buffer, which stays valid for the duration of Tokenize
// because every token value is copied out as an independent string
// (see trimInputRange and scanQuotedString).
func newYAMLLexer(input []byte) *Lexer {
	l, ok := lexerPool.Get().(*Lexer)
	if !ok {
		l = &Lexer{
			indents: make([]int, 1, 8),
		}
	}
	l.input = input
	l.pos = 0
	l.line = 1
	l.column = 1
	l.indent = 0
	l.indents = l.indents[:1]
	l.indents[0] = 0
	l.tokens = l.tokens[:0]
	l.tokenHead = 0
	l.buffered = false
	l.eof = false
	return l
}

// maxPooledLexerTokens bounds the retained buffered-token slice. The queue
// holds at most the pending indent/dedent tokens for one line, so this only
// guards against pathological growth.
const maxPooledLexerTokens = 64

// release returns the Lexer to the pool.
//
// The buffered-token slice is retained (cleared) rather than dropped: it
// starts at nil on every parse otherwise, and its append growth from 1 was
// ~11% of YAML parse allocations — indent/dedent buffering on nested
// documents re-walked the 1→2→4→… grow chain on every single parse.
//
// SECURITY: the full backing array is cleared, not just the live prefix —
// NextToken advances the slice head (l.tokens = l.tokens[1:]), so stale
// Token values before the offset still reference token strings (file
// content) and must be dropped before pooling.
func (l *Lexer) release() {
	l.input = nil
	if cap(l.tokens) > maxPooledLexerTokens {
		l.tokens = nil
	} else {
		clear(l.tokens[:cap(l.tokens)])
		l.tokens = l.tokens[:0]
	}
	l.tokenHead = 0
	lexerPool.Put(l)
}

// NextToken returns the next token from the input.
//
// The buffered queue is consumed through tokenHead rather than by re-slicing
// the head away: slicing (l.tokens = l.tokens[1:]) advances the slice base, so
// appends would periodically regrow from a zero remaining capacity even with
// the slice pooled. With a head index the slice rewinds to its base on drain
// and the next line's indent/dedent tokens reuse the same storage.
func (l *Lexer) NextToken() (Token, error) {
	if l.buffered {
		if l.tokenHead < len(l.tokens) {
			t := l.tokens[l.tokenHead]
			l.tokenHead++
			if l.tokenHead == len(l.tokens) {
				l.tokens = l.tokens[:0]
				l.tokenHead = 0
				l.buffered = false
			}
			return t, nil
		}
		// Defensive: buffered with an exhausted queue cannot happen (the
		// flag clears on drain), but failing closed here keeps NextToken
		// total instead of indexing out of range.
		l.buffered = false
	}

	if l.eof {
		return Token{Type: TokenEOF, Line: l.line, Column: l.column}, nil
	}

	return l.nextToken()
}

// nextToken reads and returns the next token.
func (l *Lexer) nextToken() (Token, error) {
	// Skip whitespace except newlines
	l.skipSpaces()

	if l.pos >= len(l.input) {
		l.eof = true
		// Emit at most ONE pending dedent before EOF: every parser loop
		// also breaks on EOF, so unmatched dedents are never consumed —
		// the machinery exists only to keep the token stream shape regular.
		if len(l.indents) > 1 {
			l.indents = l.indents[:len(l.indents)-1]
			return Token{Type: TokenDedent, Line: l.line, Column: l.column, Indent: len(l.indents) - 1}, nil
		}
		return Token{Type: TokenEOF, Line: l.line, Column: l.column}, nil
	}

	ch := l.input[l.pos]

	// Handle newlines
	if ch == '\n' {
		return l.handleNewline()
	}

	// Handle carriage return (CRLF or CR only)
	if ch == '\r' {
		// Don't skip here - let handleNewline do it
		return l.handleNewline()
	}

	// Handle document separator
	if ch == '-' && l.isDocumentStart() {
		return l.scanDocumentStart()
	}

	// Handle comment
	if ch == '#' {
		return l.scanComment()
	}

	// Handle array item
	if ch == '-' && l.isDashStart() {
		return l.scanDash()
	}

	// Handle key-value pair
	if l.isKeyStart() {
		return l.scanKey()
	}

	// Handle value (scalar)
	return l.scanValue()
}

// skipSpaces skips space and tab characters.
func (l *Lexer) skipSpaces() {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ' ' || ch == '\t' {
			l.pos++
			l.column++
		} else {
			break
		}
	}
}

// handleNewline processes a newline and handles indentation changes.
func (l *Lexer) handleNewline() (Token, error) {
	startLine := l.line
	l.line++
	l.column = 1

	// Skip the newline character(s)
	if l.pos < len(l.input) && l.input[l.pos] == '\r' {
		l.pos++
		if l.pos < len(l.input) && l.input[l.pos] == '\n' {
			l.pos++
		}
	} else if l.pos < len(l.input) && l.input[l.pos] == '\n' {
		l.pos++
	}

	// Count indentation of next line
	newIndent := 0
countLoop:
	for l.pos < len(l.input) {
		switch ch := l.input[l.pos]; ch {
		case ' ':
			newIndent++
			l.pos++
		case '\t':
			// Treat tab as 2 spaces
			newIndent += 2
			l.pos++
		case '\n', '\r':
			// Empty line, continue to next
			if ch == '\r' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '\n' {
				l.pos++
			}
			l.pos++
			l.line++
			l.column = 1
			newIndent = 0
		default:
			// Any other character ends the indentation run.
			break countLoop
		}
	}

	// The indent stack holds RAW column counts; a token's level is its
	// stack position, so any strictly deeper column nests one level down
	// regardless of width. The previous newIndent/2 truncation made odd
	// widths alias the enclosing level: a 1-space child became a sibling
	// of its 0-space parent and a 3-space child a sibling of its 2-space
	// parent, silently re-parenting keys in hand-edited files.
	currentColumn := l.indents[len(l.indents)-1]

	if newIndent > currentColumn {
		// SECURITY: Limit indent stack depth to prevent unbounded memory growth
		// from malicious YAML with monotonically increasing indentation.
		const maxIndentDepth = 256
		if len(l.indents) >= maxIndentDepth {
			return Token{}, &YAMLError{
				Line:    startLine,
				Column:  l.column,
				Message: fmt.Sprintf("maximum indent depth exceeded (%d)", maxIndentDepth),
			}
		}
		// Indentation increased: one deeper level, whatever the width.
		l.indents = append(l.indents, newIndent)
		l.indent = len(l.indents) - 1
		// Buffer newline and indent
		l.tokens = append(l.tokens, Token{Type: TokenIndent, Line: l.line, Column: 1, Indent: l.indent})
		l.buffered = true
		return Token{Type: TokenNewline, Line: startLine, Column: l.column}, nil
	} else if newIndent < currentColumn {
		// Dedent by column: pop every deeper column, then resume at the
		// enclosing column — or push the new intermediate column as a fresh
		// sibling level (dedenting from 4 to 3 over a stacked 2 stays at
		// depth 2, which YAML's column-based nesting requires). Dedents
		// emitted per NET depth change, not per pop.
		oldDepth := len(l.indents) - 1
		for len(l.indents) > 1 && l.indents[len(l.indents)-1] > newIndent {
			l.indents = l.indents[:len(l.indents)-1]
		}
		if l.indents[len(l.indents)-1] < newIndent {
			l.indents = append(l.indents, newIndent)
		}
		newDepth := len(l.indents) - 1
		l.indent = newDepth

		for i := 0; i < oldDepth-newDepth; i++ {
			l.tokens = append(l.tokens, Token{Type: TokenDedent, Line: l.line, Column: 1, Indent: newDepth})
		}
		if oldDepth > newDepth {
			l.buffered = true
		}
		return Token{Type: TokenNewline, Line: startLine, Column: l.column}, nil
	}

	return Token{Type: TokenNewline, Line: startLine, Column: l.column}, nil
}

// isDocumentStart checks if current position is a document separator.
func (l *Lexer) isDocumentStart() bool {
	if l.pos+2 >= len(l.input) {
		return false
	}
	// Check for ---
	if l.input[l.pos] == '-' && l.input[l.pos+1] == '-' && l.input[l.pos+2] == '-' {
		// Must be at start of line or after whitespace
		if l.column == 1 || (l.pos > 0 && (l.input[l.pos-1] == ' ' || l.input[l.pos-1] == '\t')) {
			// Check what follows
			if l.pos+3 >= len(l.input) {
				return true
			}
			next := l.input[l.pos+3]
			return next == ' ' || next == '\t' || next == '\n' || next == '\r'
		}
	}
	return false
}

// scanDocumentStart scans a document separator.
func (l *Lexer) scanDocumentStart() (Token, error) {
	startCol := l.column
	l.pos += 3
	l.column += 3
	return Token{Type: TokenDocumentStart, Line: l.line, Column: startCol}, nil
}

// isDashStart checks if dash is start of array item.
func (l *Lexer) isDashStart() bool {
	if l.pos+1 >= len(l.input) {
		return true // Last character dash
	}
	next := l.input[l.pos+1]
	return next == ' ' || next == '\t' || next == '\n' || next == '\r'
}

// scanDash scans an array item marker.
func (l *Lexer) scanDash() (Token, error) {
	startCol := l.column
	l.pos++
	l.column++
	return Token{Type: TokenDash, Line: l.line, Column: startCol, Indent: l.indent}, nil
}

// isKeyStart checks if we're at the start of a key.
//
// A ':' followed by whitespace only qualifies as the key separator at flow
// depth 0. Inside an inline JSON/flow region ({"a": 1}) the separator is
// data, so a value that merely embeds such a region — e.g. a cookie string
// carrying g_state={"i_l": 0} — must not be misread as a key.
func (l *Lexer) isKeyStart() bool {
	// Look for a colon before newline
	braceDepth := 0
	for i := l.pos; i < len(l.input); i++ {
		ch := l.input[i]
		if ch == '\n' || ch == '\r' {
			return false
		}
		if braceDepth > 0 && ch == '"' {
			// JSON string inside the flow region: skip it so braces and
			// colons within the string do not affect depth tracking. The
			// skip is bounded by the line: an unterminated string must not
			// swallow the rest of the input. On a line break we back up so
			// the outer loop observes it and returns false.
			i++
			for i < len(l.input) {
				c := l.input[i]
				if c == '"' {
					break
				}
				if c == '\n' || c == '\r' {
					i-- // let the outer loop see the line break
					break
				}
				if c == '\\' {
					i++
				}
				i++
			}
			continue
		}
		if ch == ':' {
			// Check if colon is followed by space, newline, or end
			if braceDepth == 0 {
				if i+1 >= len(l.input) {
					return true
				}
				next := l.input[i+1]
				return next == ' ' || next == '\t' || next == '\n' || next == '\r'
			}
			continue
		}
		// Quoted key (only meaningful at flow depth 0: inside a flow region
		// a single quote is plain data — JSON strings use double quotes,
		// which are handled above). The skip is bounded by the line and only
		// happens when a closing quote exists on it: an UNTERMINATED quote
		// ("it's: 1") previously swallowed the rest of the input, the ": "
		// check never fired, and the whole line was silently dropped
		// downstream. With no closing quote the scan simply continues, the
		// colon check classifies the line as a key, and the quote-bearing
		// key then fails key validation — loud, not silent.
		if braceDepth == 0 && (ch == '"' || ch == '\'') {
			quote := ch
			j := i + 1
			for j < len(l.input) {
				c := l.input[j]
				if c == quote || c == '\n' || c == '\r' {
					break
				}
				if c == '\\' {
					j++
				}
				j++
			}
			if j < len(l.input) && l.input[j] == quote {
				i = j // skip past the closing quote; i++ moves beyond it
			}
		}
		if ch == '{' || ch == '[' {
			braceDepth++
		} else if (ch == '}' || ch == ']') && braceDepth > 0 {
			braceDepth--
		}
	}
	return false
}

// scanKey scans a key token.
func (l *Lexer) scanKey() (Token, error) {
	startLine := l.line
	startCol := l.column

	// Handle quoted keys
	if l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '"' || ch == '\'' {
			str, err := l.scanQuotedString(ch)
			if err != nil {
				return Token{}, err
			}
			// Skip colon and whitespace
			l.skipSpaces()
			if l.pos < len(l.input) && l.input[l.pos] == ':' {
				l.pos++
				l.column++
			}
			return Token{Type: TokenKey, Value: str, Line: startLine, Column: startCol, Indent: l.indent, IsQuoted: true}, nil
		}
	}

	// Unquoted key: scan by index and slice the input once instead of copying
	// byte-by-byte through a pooled buffer. The string() conversion makes an
	// exact-size copy so the token value stays independent of l.input.
	contentStart := l.pos
	contentEnd := l.pos
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ':' {
			// Check if colon is followed by space, newline, or end
			if l.pos+1 >= len(l.input) {
				contentEnd = l.pos
				l.pos++
				break
			}
			next := l.input[l.pos+1]
			if next == ' ' || next == '\t' || next == '\n' || next == '\r' {
				contentEnd = l.pos
				l.pos++
				l.column++
				break
			}
		}
		if ch == '\n' || ch == '\r' {
			contentEnd = l.pos
			break
		}
		if ch == '#' && isCommentStart(l.input, l.pos) {
			// Same whitespace rule as scanValue: '#' after whitespace ends the
			// key and starts a comment; '#' mid-token stays part of the key and
			// is then rejected by key validation as an explicit error instead
			// of silently truncating the key.
			contentEnd = l.pos
			break
		}
		l.pos++
		l.column++
		contentEnd = l.pos
	}

	return Token{Type: TokenKey, Value: trimInputRange(l.input, contentStart, contentEnd), Line: startLine, Column: startCol, Indent: l.indent}, nil
}

// trimInputRange trims ASCII spaces and tabs (and, defensively, CR/LF) from
// both ends of input[start:end] and returns an exact-size copy.
// It replaces the previous buffered WriteByte round-trip: the scanned region
// can never contain a newline (the scanners stop there), so the same trim
// semantics apply with one allocation and no buffer churn.
//
// SECURITY/RETENTION: the string() copy is deliberate — returning the
// substring input[start:end] would be zero-copy but would keep the entire
// input alive for as long as any token value (and any result-map entry built
// from it) is referenced.
func trimInputRange(input []byte, start, end int) string {
	for start < end {
		c := input[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}
	for end > start {
		c := input[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return string(input[start:end])
}

// isCommentStart reports whether the '#' at input[pos] begins a YAML comment.
// Per the YAML spec a comment starts at the beginning of a line or where '#'
// is preceded by whitespace; '#' anywhere else is plain-scalar data. pos == 0
// counts as a comment start (beginning of input/line).
func isCommentStart(input []byte, pos int) bool {
	if pos == 0 {
		return true
	}
	switch input[pos-1] {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// scanValue scans a scalar value.
func (l *Lexer) scanValue() (Token, error) {
	startLine := l.line
	startCol := l.column

	// Skip leading spaces
	l.skipSpaces()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Line: l.line, Column: l.column}, nil
	}

	// Handle quoted strings
	ch := l.input[l.pos]
	if ch == '"' || ch == '\'' {
		str, err := l.scanQuotedString(ch)
		if err != nil {
			return Token{}, err
		}
		return Token{Type: TokenValue, Value: str, Line: startLine, Column: startCol, Indent: l.indent, IsQuoted: true}, nil
	}

	// Handle block scalars (| and >) — GEN-001: they previously scanned as
	// the literal "|" scalar while the parser silently dropped the indented
	// content lines, corrupting multi-line values (certificates, keys)
	// without any error.
	if ch == '|' || ch == '>' {
		if style, chomp, explicit, ok := l.peekBlockHeader(); ok {
			return l.scanBlockScalar(style, chomp, explicit, startLine, startCol)
		}
		// Not a block header (content follows on the same line):
		// plain scalar starting with '|' or '>'.
	}

	// Unquoted value: scan by index and slice the input once instead of
	// copying byte-by-byte through a pooled buffer (see trimInputRange).
	// A ':' (or comment '#') only ends the value at flow depth 0: inside an
	// inline JSON/flow region ({"a": 1}) both are data, so values embedding
	// JSON with ": " separators — e.g. g_state={"i_l": 0} — scan whole.
	contentStart := l.pos
	contentEnd := l.pos
	braceDepth := 0
	inJSONString := false
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '\n' || ch == '\r' {
			contentEnd = l.pos
			break
		}
		if inJSONString {
			if ch == '\\' && l.pos+1 < len(l.input) {
				l.pos++
				l.column++
			}
			if ch == '"' {
				inJSONString = false
			}
		} else if braceDepth > 0 && ch == '"' {
			inJSONString = true
		} else if ch == '{' || ch == '[' {
			braceDepth++
		} else if (ch == '}' || ch == ']') && braceDepth > 0 {
			braceDepth--
		} else if ch == '#' && braceDepth == 0 && isCommentStart(l.input, l.pos) {
			// YAML starts a comment only where '#' is preceded by whitespace
			// (or begins the value). '#' inside a plain scalar — pa#ss,
			// http://host/p#frag — is data, not a comment marker; the same
			// holds inside a flow region, where truncating at " # " would
			// cut an inline JSON string like {"note": "a # b"} in half.
			contentEnd = l.pos
			break
		} else if ch == ':' && braceDepth == 0 {
			// Check if this looks like a nested key (colon followed by space/newline)
			if l.pos+1 < len(l.input) {
				next := l.input[l.pos+1]
				if next == ' ' || next == '\t' || next == '\n' || next == '\r' {
					// This is a key, stop here
					contentEnd = l.pos
					break
				}
			}
		}
		l.pos++
		l.column++
		contentEnd = l.pos
	}

	return Token{Type: TokenValue, Value: trimInputRange(l.input, contentStart, contentEnd), Line: startLine, Column: startCol, Indent: l.indent}, nil
}

// peekBlockHeader reports whether the value starting at l.pos is a block
// scalar header: the style indicator ('|' literal or '>' folded), an
// optional chomping indicator ('+' keep / '-' strip), an optional explicit
// indentation digit (1-9, relative to the parent), optional spaces, an
// optional comment, and then end of line. Anything else on the line means
// the indicator is plain-scalar content ("| text"), and the caller keeps
// its regular scalar scan. Pure lookahead: nothing is consumed.
func (l *Lexer) peekBlockHeader() (style, chomp byte, explicitIndent int, ok bool) {
	i := l.pos
	if i >= len(l.input) || (l.input[i] != '|' && l.input[i] != '>') {
		return 0, 0, 0, false
	}
	style = l.input[i]
	i++
	if i < len(l.input) && (l.input[i] == '+' || l.input[i] == '-') {
		chomp = l.input[i]
		i++
	}
	if i < len(l.input) && l.input[i] >= '1' && l.input[i] <= '9' {
		explicitIndent = int(l.input[i] - '0')
		i++
	}
	for i < len(l.input) && (l.input[i] == ' ' || l.input[i] == '\t') {
		i++
	}
	if i < len(l.input) && l.input[i] == '#' {
		for i < len(l.input) && l.input[i] != '\n' && l.input[i] != '\r' {
			i++
		}
	}
	if i >= len(l.input) || l.input[i] == '\n' || l.input[i] == '\r' {
		return style, chomp, explicitIndent, true
	}
	return 0, 0, 0, false
}

// scanBlockScalar consumes a block scalar (GEN-001) whose header starts at
// l.pos and returns it as a QUOTED value token — quoted so flattening
// inserts the text verbatim: no inline-JSON probing, no null/bool/number
// coercion, and interior whitespace (the whole point of a block scalar)
// preserved. Content lines are every following line indented deeper than
// the key's own indentation; blank lines belong to the block at any
// indentation. Scanning stops at the first non-blank line at or below the
// parent indentation, with l.pos rewound onto the newline that ended the
// last consumed line so handleNewline emits the normal newline/dedent
// tokens for the terminator line.
//
// Chomping: '-' strips every trailing newline, '+' keeps them all, and the
// default clips to exactly one. Folding ('>'): a newline between two
// non-empty lines becomes a space, each empty line becomes a newline —
// the more-indented-lines-stay-literal nuance of the spec is deliberately
// simplified away (such lines fold like any other).
func (l *Lexer) scanBlockScalar(style, chomp byte, explicitIndent int, startLine, startCol int) (Token, error) {
	parentIndent := l.indents[len(l.indents)-1]

	// Consume the header: style, chomp, explicit digit, spaces, comment.
	l.pos++
	l.column++
	if chomp != 0 {
		l.pos++
		l.column++
	}
	if explicitIndent > 0 {
		l.pos++
		l.column++
	}
	l.skipSpaces()
	if l.pos < len(l.input) && l.input[l.pos] == '#' {
		for l.pos < len(l.input) && l.input[l.pos] != '\n' && l.input[l.pos] != '\r' {
			l.pos++
			l.column++
		}
	}

	// EOF right after the header: an empty block.
	if l.pos >= len(l.input) {
		return Token{Type: TokenValue, Value: "", Line: startLine, Column: startCol, Indent: l.indent, IsQuoted: true}, nil
	}

	// Consume the header line's newline; remember it as the initial rewind
	// point (an immediately following terminator line means an empty block,
	// and handleNewline must reprocess this newline for that line).
	headerNewline := l.pos
	switch l.input[l.pos] {
	case '\r':
		l.pos++
		if l.pos < len(l.input) && l.input[l.pos] == '\n' {
			l.pos++
		}
	case '\n':
		l.pos++
	}
	l.line++
	l.column = 1
	prevNewline := headerNewline

	var lines []string // content lines, blockIndent stripped
	pendingBlanks := 0 // blank lines waiting to be flushed before content
	blockIndent := -1  // auto-detected from the first content line
	if explicitIndent > 0 {
		blockIndent = parentIndent + explicitIndent
	}

	for l.pos < len(l.input) {
		lineStart := l.pos
		j := lineStart
		for j < len(l.input) && l.input[j] == ' ' {
			j++
		}
		eol := j
		for eol < len(l.input) && l.input[eol] != '\n' {
			eol++
		}

		if eol == j {
			// Blank line (nothing after the indent run). A final
			// whitespace-only line at EOF ends the block instead.
			if eol >= len(l.input) {
				l.pos = len(l.input)
				break
			}
			l.pos = eol + 1
			l.line++
			l.column = 1
			pendingBlanks++
			continue
		}

		indent := j - lineStart
		if indent <= parentIndent {
			// Terminator line at or below the key's indentation: rewind
			// onto the previous line's newline and let the regular
			// machinery re-scan this line.
			l.pos = prevNewline
			l.line--
			l.column = 1
			break
		}
		if blockIndent == -1 {
			blockIndent = indent
		} else if explicitIndent > 0 && indent < blockIndent {
			return Token{}, &YAMLError{
				Line:    l.line,
				Column:  j - lineStart + 1,
				Message: fmt.Sprintf("block scalar line indented %d spaces, expected at least %d (explicit indentation indicator)", indent, blockIndent),
			}
		}

		// Content line: strip the block indentation (a line indented less
		// than the auto-detected blockIndent keeps its extra spaces).
		strip := blockIndent
		if indent < strip {
			strip = indent
		}
		start := lineStart + strip
		end := eol
		if end > start && l.input[end-1] == '\r' {
			end--
		}
		for k := 0; k < pendingBlanks; k++ {
			lines = append(lines, "")
		}
		pendingBlanks = 0
		lines = append(lines, string(l.input[start:end]))

		prevNewline = eol
		l.pos = eol + 1
		l.line++
		l.column = 1
	}

	// Assemble per style and chomping indicator.
	var b strings.Builder
	if style == '|' {
		for i, ln := range lines {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(ln)
		}
	} else { // '>' folded
		for i, ln := range lines {
			if i == 0 {
				b.WriteString(ln)
				continue
			}
			switch {
			case ln == "":
				b.WriteByte('\n')
			case lines[i-1] == "":
				b.WriteString(ln)
			default:
				b.WriteByte(' ')
				b.WriteString(ln)
			}
		}
	}
	value := b.String()
	hasContent := len(lines) > 0
	switch chomp {
	case '-': // strip: no trailing newline at all
	case '+': // keep: the last line's newline plus every trailing blank
		if hasContent || pendingBlanks > 0 {
			b.WriteByte('\n')
		}
		for k := 0; k < pendingBlanks; k++ {
			b.WriteByte('\n')
		}
		value = b.String()
	default: // clip (YAML default): exactly one trailing newline
		if hasContent {
			value = value + "\n"
		}
	}

	return Token{Type: TokenValue, Value: value, Line: startLine, Column: startCol, Indent: l.indent, IsQuoted: true}, nil
}

// scanQuotedString scans a quoted string.
func (l *Lexer) scanQuotedString(quote byte) (string, error) {
	l.pos++ // Skip opening quote
	l.column++

	buf := getLexerBuffer()
	defer putLexerBuffer(buf)
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == quote {
			// YAML single-quoted scalars fold a doubled quote into one
			// literal quote ('it''s' → it's).
			if quote == '\'' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '\'' {
				buf.WriteByte('\'')
				l.pos += 2
				l.column += 2
				continue
			}
			l.pos++
			l.column++
			return buf.String(), nil
		}
		// Escape processing is double-quote-only: YAML single-quoted scalars
		// are literal, so a backslash must survive verbatim. Running the
		// escape switch for them corrupted Windows paths ('C:\Users\app' →
		// C:Usersapp) and could consume the closing quote ('...\' errored
		// as unterminated).
		if quote == '"' && ch == '\\' {
			l.pos++
			l.column++
			if l.pos >= len(l.input) {
				return "", &YAMLError{
					Line:    l.line,
					Column:  l.column,
					Message: "unexpected end of input in escape sequence",
				}
			}
			escaped := l.input[l.pos]
			switch escaped {
			case 'n':
				buf.WriteByte('\n')
			case 't':
				buf.WriteByte('\t')
			case 'r':
				buf.WriteByte('\r')
			case '\\':
				buf.WriteByte('\\')
			case '"':
				buf.WriteByte('"')
			case '\'':
				buf.WriteByte('\'')
			// SECURITY: \0 escape is explicitly rejected at the lexer level for defense in depth.
			// Null bytes can cause:
			// - Log injection (log entries truncated at null byte)
			// - String truncation vulnerabilities in C interop
			// - Bypass of security controls that don't expect nulls
			// This provides early rejection before value validation runs.
			case '0':
				return "", &YAMLError{
					Line:    l.line,
					Column:  l.column,
					Message: "null byte escape (\\0) is not allowed in YAML values",
				}
			default:
				buf.WriteByte(escaped)
			}
			l.pos++
			l.column++
			continue
		}
		if ch == '\n' || ch == '\r' {
			return "", &YAMLError{
				Line:    l.line,
				Column:  l.column,
				Message: "unterminated string",
			}
		}
		buf.WriteByte(ch)
		l.pos++
		l.column++
	}

	return "", &YAMLError{
		Line:    l.line,
		Column:  l.column,
		Message: "unterminated string",
	}
}

// scanComment scans a comment.
func (l *Lexer) scanComment() (Token, error) {
	startLine := l.line
	startCol := l.column
	contentStart := l.pos + 1 // skip '#'
	l.pos++
	l.column++

	contentEnd := l.pos
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '\n' || ch == '\r' {
			contentEnd = l.pos
			break
		}
		l.pos++
		l.column++
		contentEnd = l.pos
	}

	return Token{Type: TokenComment, Value: trimInputRange(l.input, contentStart, contentEnd), Line: startLine, Column: startCol, Indent: l.indent}, nil
}

// tokenSlicePool reuses the token slices produced by tokenization.
// A 100-variable document yields ~300 tokens ≈ 17KB — by far the largest
// single allocation of a YAML parse — so pooling it removes most of the
// per-parse allocation churn. The pool stores *[]Token because Tokenize may
// reallocate while appending; the caller writes the final slice back through
// the pointer before returning it (same pattern as the reader buffers in the
// root package's structured_parser.go).
var tokenSlicePool = sync.Pool{
	New: func() any {
		s := make([]Token, 0, 256)
		return &s
	},
}

// getTokenSlice retrieves a token slice from the pool.
func getTokenSlice() *[]Token {
	p, ok := tokenSlicePool.Get().(*[]Token)
	if !ok {
		s := make([]Token, 0, 256)
		return &s
	}
	return p
}

// putTokenSlice returns a token slice to the pool.
// It drops references to token values (which may hold file contents) so the
// strings can be collected, and discards slices whose capacity exceeds
// MaxPooledYAMLTokenCount to prevent memory bloat.
func putTokenSlice(p *[]Token) {
	if p == nil || *p == nil {
		return
	}
	clear(*p) // drop Value string references
	if cap(*p) > MaxPooledYAMLTokenCount {
		return // too large, let GC reclaim it
	}
	*p = (*p)[:0]
	tokenSlicePool.Put(p)
}

// tokenizeInto tokenizes into the provided slice, reusing its capacity and
// growing it as needed, and returns the (possibly reallocated) slice. On
// error the partially-filled slice is returned alongside the error so pooled
// callers can return it to the pool. Callers that do not pool the token slice
// pass nil.
//
// Pre-allocates for efficiency: a typical line yields ~3 tokens (newline,
// key, value), while single-line flow-style input is better estimated by
// length. Taking the max of both heuristics lands within one growth step of
// the exact count and avoids reallocating the 56-byte-per-token slice
// mid-parse.
func (l *Lexer) tokenizeInto(tokens []Token) ([]Token, error) {
	// Line-count estimate (3 tokens per line + slack), floored by the
	// length-based estimate for inputs with few or no newlines.
	estimatedTokens := bytes.Count(l.input, []byte{'\n'})*3 + 8
	if byLength := len(l.input)/20 + 10; byLength > estimatedTokens {
		estimatedTokens = byLength
	}
	if estimatedTokens > 1024 {
		estimatedTokens = 1024
	}
	if cap(tokens) < estimatedTokens {
		tokens = make([]Token, 0, estimatedTokens)
	}
	tokens = tokens[:0]

	for {
		tok, err := l.NextToken()
		if err != nil {
			return tokens, err
		}
		// SECURITY (SEC-02): fail fast once the token count reaches the hard
		// cap. Every token is ~56 bytes, so tokenizing a size-legal file made
		// of tiny lines would otherwise amplify the input into gigabytes of
		// memory before the parser's MaxVariables check ever runs.
		if len(tokens) >= HardMaxYAMLTokens {
			return tokens, &YAMLError{
				Line:    l.line,
				Column:  l.column,
				Message: fmt.Sprintf("maximum token count exceeded (%d)", HardMaxYAMLTokens),
			}
		}
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}
	return tokens, nil
}
