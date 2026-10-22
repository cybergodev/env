// Provides YAML parsing utilities.

package internal

import (
	"fmt"
	"sync"
)

// ValueType represents the type of a YAML value.
type ValueType int

const (
	// ValueTypeScalar represents a scalar value (string, number, bool, null).
	ValueTypeScalar ValueType = iota
	// ValueTypeMap represents a map/object.
	ValueTypeMap
	// ValueTypeArray represents an array/list.
	ValueTypeArray
)

// Value represents a YAML value.
type Value struct {
	Type   ValueType
	Scalar string
	Map    map[string]*Value
	Array  []*Value
	Line   int
	Column int
	// Quoted reports that a ValueTypeScalar came from a quoted token.
	// YAML semantics: quoted scalars are always strings — no null/bool/number
	// coercion, and interior whitespace (including trailing) is significant.
	Quoted bool
	// pooled marks a *Value that ReleaseValue has already returned to
	// valuePool. Constructors clear it on Get; a second ReleaseValue sees
	// it set and returns without putting the node back again — a double
	// Put would leave the same pointer in the pool twice, and a later
	// ParseYAML could then draw one object for two tree positions and
	// build a cycle that ReleaseValue cannot walk.
	pooled bool
}

// valuePool pools *Value nodes to reduce allocation pressure during YAML parsing.
var valuePool = sync.Pool{
	New: func() any {
		return &Value{}
	},
}

// NewScalarValue creates a new scalar value (from pool).
func NewScalarValue(s string, line, col int) *Value {
	v := valuePool.Get().(*Value)
	v.pooled = false
	v.Type = ValueTypeScalar
	v.Scalar = s
	v.Line = line
	v.Column = col
	return v
}

// NewMapValue creates a new map value with pre-allocated capacity (from pool).
func NewMapValue(line, col int) *Value {
	return newMapValueCap(line, col, 8)
}

// newMapValueCap creates a pooled map value whose backing map is sized to
// hold at least size entries, avoiding incremental growth. Used for the
// document root where the entry count is estimable from the token count.
func newMapValueCap(line, col, size int) *Value {
	v := valuePool.Get().(*Value)
	v.pooled = false
	v.Type = ValueTypeMap
	if v.Map == nil {
		v.Map = make(map[string]*Value, size)
	} else {
		// Reuse existing map after clearing
		clear(v.Map)
	}
	v.Line = line
	v.Column = col
	return v
}

// NewArrayValue creates a new array value with pre-allocated capacity (from pool).
func NewArrayValue(line, col int) *Value {
	v := valuePool.Get().(*Value)
	v.pooled = false
	v.Type = ValueTypeArray
	if v.Array == nil {
		v.Array = make([]*Value, 0, 4)
	} else {
		v.Array = v.Array[:0]
	}
	v.Line = line
	v.Column = col
	return v
}

// Parser parses YAML tokens into a Value tree.
type Parser struct {
	tokens    []Token
	pos       int
	maxDepth  int
	dupPolicy DuplicateKeyPolicy
}

// NewYAMLParser creates a new YAML parser with the DuplicateKeyInherit
// policy (each format's historical duplicate-key behavior). Callers that
// need an explicit policy use NewYAMLParserWithPolicy.
func NewYAMLParser(tokens []Token, maxDepth int) *Parser {
	return NewYAMLParserWithPolicy(tokens, maxDepth, DuplicateKeyInherit)
}

// NewYAMLParserWithPolicy creates a new YAML parser. The dupPolicy governs
// duplicate mapping keys within one document (see DuplicateKeyPolicy); the
// zero value keeps the historical last-occurrence-wins behavior.
func NewYAMLParserWithPolicy(tokens []Token, maxDepth int, dupPolicy DuplicateKeyPolicy) *Parser {
	if maxDepth <= 0 {
		// Defensive fallback for direct internal callers; the root package's
		// parsers already substitute defaultStructuredMaxDepth before calling.
		maxDepth = 16
	}
	return &Parser{
		tokens:    tokens,
		maxDepth:  maxDepth,
		dupPolicy: dupPolicy,
	}
}

// Parse parses the tokens and returns the root value.
func (p *Parser) Parse() (*Value, error) {
	return p.parseDocument(0)
}

// parseDocument parses a YAML document.
func (p *Parser) parseDocument(depth int) (*Value, error) {
	if depth >= p.maxDepth {
		return nil, &YAMLError{
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", p.maxDepth),
		}
	}

	// Skip document start markers
	p.skipDocumentStarts()

	// Skip leading newlines
	p.skipNewlines()

	if p.isEOF() {
		return NewMapValue(1, 1), nil
	}

	// A root-level sequence ("- a\n- b") is not a mapping: parseMap would
	// fall into its TokenDash "shouldn't happen" break and silently return
	// an EMPTY map, loading the whole document as zero variables with no
	// error. Fail loudly instead. (JSON flattens root arrays to _0.._N
	// keys; giving YAML sequences the same treatment would be a larger
	// semantic change.)
	if p.current().Type == TokenDash {
		return nil, &YAMLError{
			Line:    p.current().Line,
			Column:  p.current().Column,
			Message: "YAML document root must be a mapping, not a sequence",
		}
	}

	// Parse root level
	root, err := p.parseMap(depth, 0)
	if err != nil {
		return nil, err
	}

	// GEN-001: a '---' separator after the first document previously broke
	// the map loop and every later document was dropped silently — the load
	// reported success while holding a fraction of the file. Fail loudly
	// instead, mirroring the explicit root-sequence rejection above.
	if p.current().Type == TokenDocumentStart {
		return nil, &YAMLError{
			Line:    p.current().Line,
			Column:  p.current().Column,
			Message: "multiple YAML documents are not supported; load one document per file (found a second '---' separator)",
		}
	}

	return root, nil
}

// parseMap parses a YAML map.
func (p *Parser) parseMap(depth, expectedIndent int) (*Value, error) {
	var root *Value
	if depth == 0 {
		// Root document map: pre-size from the token count (~3 tokens per
		// entry: newline, key, value) so large flat documents don't pay for
		// repeated map growth. The hint is clamped to HardMaxVariables: a
		// deeply nested document can hold up to HardMaxYAMLTokens tokens
		// under a handful of root keys, and the flattened key count the
		// loader accepts is bounded by MaxVariables anyway. Nested maps keep
		// the small default — depth 0 is reachable only from parseDocument.
		root = newMapValueCap(p.currentLine(), p.currentColumn(),
			min(len(p.tokens)/3, HardMaxVariables))
	} else {
		root = NewMapValue(p.currentLine(), p.currentColumn())
	}

	for !p.isEOF() {
		// Skip newlines
		p.skipNewlines()

		if p.isEOF() {
			break
		}

		// Check for document start
		if p.current().Type == TokenDocumentStart {
			break
		}

		// Check for dedent (end of this map)
		if p.current().Type == TokenDedent {
			p.advance()
			break
		}

		// Check indentation
		currentIndent := p.current().Indent
		if currentIndent < expectedIndent {
			// End of current map level
			break
		}

		// Skip indents
		if p.current().Type == TokenIndent {
			p.advance()
			continue
		}

		// Parse key-value pair or array item
		if p.current().Type == TokenKey {
			key := p.current().Value
			keyLine := p.current().Line
			p.advance()

			// Skip newlines after colon
			p.skipNewlines()

			var value *Value
			var err error

			if p.isEOF() {
				value = NewScalarValue("", p.currentLine(), p.currentColumn())
			} else if p.current().Type == TokenIndent {
				// Nested structure
				p.advance()
				value, err = p.parseNestedValue(depth+1, currentIndent+1)
			} else if p.current().Type == TokenValue {
				value = p.scalarFromToken()
			} else if p.current().Type == TokenComment {
				// Comment line(s) directly after the colon. A nested block
				// may still follow after the newline and any further comment
				// lines (see consumeCommentsAfterKey).
				if p.consumeCommentsAfterKey() {
					value, err = p.parseNestedValue(depth+1, currentIndent+1)
				} else {
					value = NewScalarValue("", p.currentLine(), p.currentColumn())
				}
			} else if p.current().Type == TokenDash {
				// Array as value
				value, err = p.parseArray(depth+1, currentIndent+1)
			} else if p.current().Type == TokenKey {
				// Inline nested map
				value, err = p.parseMap(depth+1, currentIndent+1)
			} else {
				value = NewScalarValue("", p.currentLine(), p.currentColumn())
			}

			if err != nil {
				return nil, err
			}

			// Resolve duplicate mapping keys per the configured policy.
			// Inherit and LastWins keep the historical last-occurrence-wins
			// behavior (plain map assignment); FirstWins keeps the existing
			// entry and Error rejects the document.
			if _, exists := root.Map[key]; exists {
				switch p.dupPolicy {
				case DuplicateKeyError:
					return nil, &YAMLError{
						Path:    key,
						Line:    keyLine,
						Message: "duplicate mapping key",
						Err:     ErrDuplicateKey,
					}
				case DuplicateKeyFirstWins:
					// Return the freshly parsed (losing) subtree to the pool;
					// it is a disjoint tree from the kept value.
					ReleaseValue(value)
					continue
				}
			}

			root.Map[key] = value
		} else if p.current().Type == TokenDash {
			// A sequence item at mapping level is malformed YAML — the
			// root case ("- a" as the first token, covered by the dedicated
			// check in parseDocument) and the mid-document case
			// ("key: 1\n- b") both land here. parseMap previously broke,
			// silently dropping the item and everything after it.
			return nil, &YAMLError{
				Line:    p.current().Line,
				Column:  p.current().Column,
				Message: "sequence item not allowed in mapping context (YAML root must be a mapping)",
			}
		} else if p.current().Type == TokenComment {
			p.advance()
		} else {
			// Unknown token, skip
			p.advance()
		}
	}

	return root, nil
}

// consumeCommentsAfterKey advances past the comment lines that may follow a
// key's colon (e.g. "key: # comment"). It reports whether a nested block was
// found after the comments, in which case the block's opening Indent token
// has been consumed and the caller should parse the nested value.
//
// This is the single implementation of the comment/newline/nested sequencing
// rules for values after a key. parseMap's early skipNewlines (run right
// after the key) already consumes a bare "key:\n  nested" sequence, so this
// helper only fires for comments — the former TokenNewline branch in the
// value chain was unreachable after that skip and has been removed.
func (p *Parser) consumeCommentsAfterKey() bool {
	for p.current().Type == TokenComment {
		p.advance()
	}
	if p.current().Type != TokenNewline {
		return false
	}
	p.skipNewlines()
	for p.current().Type == TokenComment {
		p.advance()
		p.skipNewlines()
	}
	if p.current().Type == TokenIndent {
		p.advance()
		return true
	}
	return false
}

// parseNestedValue parses a nested value (could be map, array, or scalar).
func (p *Parser) parseNestedValue(depth, expectedIndent int) (*Value, error) {
	if depth >= p.maxDepth {
		return nil, &YAMLError{
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", p.maxDepth),
		}
	}

	p.skipNewlines()

	if p.isEOF() {
		return NewScalarValue("", p.currentLine(), p.currentColumn()), nil
	}

	// Check if we have a nested structure
	if p.current().Type == TokenIndent {
		p.advance()
	}

	// Skip comments iteratively (not recursively to avoid stack overflow
	// on pathological input with many consecutive comments).
	// After each comment, re-skip newlines and indents since the recursive
	// version would re-enter the function and handle those.
	for p.current().Type == TokenComment {
		p.advance()
		p.skipNewlines()
		if p.current().Type == TokenIndent {
			p.advance()
		}
	}

	// Check what follows
	if p.current().Type == TokenKey {
		return p.parseMap(depth, expectedIndent)
	} else if p.current().Type == TokenDash {
		return p.parseArray(depth, expectedIndent)
	} else if p.current().Type == TokenValue {
		return p.scalarFromToken(), nil
	} else if p.current().Type == TokenDedent {
		// The nested block turned out to be empty (e.g. it held only
		// comments) and this Dedent closes it. Consume it here so the
		// caller resumes at the dedented level — leaving it unconsumed
		// made the next map up treat its own scope as ended and silently
		// dropped sibling keys after the comment block.
		p.advance()
		return NewScalarValue("", p.currentLine(), p.currentColumn()), nil
	}

	return NewScalarValue("", p.currentLine(), p.currentColumn()), nil
}

// parseArray parses a YAML array.
func (p *Parser) parseArray(depth, expectedIndent int) (*Value, error) {
	if depth >= p.maxDepth {
		return nil, &YAMLError{
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", p.maxDepth),
		}
	}

	root := NewArrayValue(p.currentLine(), p.currentColumn())

	for !p.isEOF() {
		p.skipNewlines()

		if p.isEOF() {
			break
		}

		// Check for document start
		if p.current().Type == TokenDocumentStart {
			break
		}

		// Check for dedent
		if p.current().Type == TokenDedent {
			p.advance()
			break
		}

		// Check indentation
		currentIndent := p.current().Indent
		if currentIndent < expectedIndent {
			break
		}

		// Skip indents
		if p.current().Type == TokenIndent {
			p.advance()
			continue
		}

		// Check for dash (array item)
		if p.current().Type == TokenDash {
			p.advance()

			var value *Value
			var err error

			if p.isEOF() {
				value = NewScalarValue("", p.currentLine(), p.currentColumn())
			} else if p.current().Type == TokenNewline {
				// Multi-line array item
				p.advance()
				p.skipNewlines()
				if p.current().Type == TokenIndent {
					p.advance()
					value, err = p.parseNestedValue(depth+1, currentIndent+1)
				} else {
					value = NewScalarValue("", p.currentLine(), p.currentColumn())
				}
			} else if p.current().Type == TokenIndent {
				p.advance()
				value, err = p.parseNestedValue(depth+1, currentIndent+1)
			} else if p.current().Type == TokenValue {
				value = p.scalarFromToken()
			} else if p.current().Type == TokenKey {
				// Map as array item (KEY at same indent level as DASH)
				value, err = p.parseMap(depth+1, currentIndent)
			} else if p.current().Type == TokenDash {
				// Nested array. In "- - a" the second dash sits at the SAME
				// indent level as the first (no Indent token separates them),
				// so the nested array starts at currentIndent — the previous
				// currentIndent+1 made the nested array's own first dash fail
				// its indent guard, yielding an empty item and letting the
				// scalars spill into the outer array.
				value, err = p.parseArray(depth+1, currentIndent)
			} else {
				value = NewScalarValue("", p.currentLine(), p.currentColumn())
			}

			if err != nil {
				return nil, err
			}

			root.Array = append(root.Array, value)
		} else if p.current().Type == TokenComment {
			p.advance()
		} else {
			// Not an array item, end of array
			break
		}
	}

	return root, nil
}

// eofToken is the shared end-of-input sentinel returned by current().
// It is read-only: parsers never mutate tokens.
var eofToken = Token{Type: TokenEOF}

// scalarFromToken creates a scalar Value from the current TokenValue token,
// preserving the token's quoting flag (a quoted scalar must never be
// type-coerced downstream — see Value.Quoted), and advances past it.
func (p *Parser) scalarFromToken() *Value {
	tok := p.current()
	v := NewScalarValue(tok.Value, tok.Line, tok.Column)
	v.Quoted = tok.IsQuoted
	p.advance()
	return v
}

// current returns a pointer to the current token.
// The tokens slice is never mutated during parsing, so the pointer stays
// valid until the next advance(). Returning a pointer avoids copying the
// 56-byte Token struct on every check — current() is called several times
// per token in the parse loops, making those copies a measured hotspot.
func (p *Parser) current() *Token {
	if p.pos >= len(p.tokens) {
		return &eofToken
	}
	return &p.tokens[p.pos]
}

// advance moves to the next token.
func (p *Parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// isEOF checks if we've reached the end of tokens.
func (p *Parser) isEOF() bool {
	return p.pos >= len(p.tokens) || p.tokens[p.pos].Type == TokenEOF
}

// currentLine returns the current line number.
func (p *Parser) currentLine() int {
	if p.isEOF() {
		return 0
	}
	return p.tokens[p.pos].Line
}

// currentColumn returns the current column number.
func (p *Parser) currentColumn() int {
	if p.isEOF() {
		return 0
	}
	return p.tokens[p.pos].Column
}

// skipNewlines skips newline tokens.
func (p *Parser) skipNewlines() {
	for !p.isEOF() && p.current().Type == TokenNewline {
		p.advance()
	}
}

// skipDocumentStarts skips document start markers.
func (p *Parser) skipDocumentStarts() {
	for !p.isEOF() && p.current().Type == TokenDocumentStart {
		p.advance()
	}
}

// maxPooledValueChildren bounds the container a pooled Value node may retain
// after ReleaseValue. Re-making the per-node map was a measurable share of
// YAML parse allocations (every map/array node allocated a fresh container on
// its next use); retaining the CLEARED container makes the next NewMapValue /
// NewArrayValue reuse it. Larger containers are dropped so one huge document
// cannot inflate every pooled node permanently.
const maxPooledValueChildren = 64

// ReleaseValue recursively returns a Value tree to the pool.
// After calling this, the Value and all its children must not be accessed.
//
// SECURITY: retained maps and arrays are fully cleared — clear() removes every
// key string (file content) from the map, and the array's whole capacity is
// zeroed so no stale *Value pins a pooled (possibly reused) node.
func ReleaseValue(v *Value) {
	if v == nil || v.pooled {
		return
	}
	switch v.Type {
	case ValueTypeMap:
		for _, child := range v.Map {
			ReleaseValue(child)
		}
		if len(v.Map) > maxPooledValueChildren {
			v.Map = nil
		} else {
			clear(v.Map)
		}
	case ValueTypeArray:
		for _, child := range v.Array {
			ReleaseValue(child)
		}
		if cap(v.Array) > maxPooledValueChildren {
			v.Array = nil
		} else {
			clear(v.Array[:cap(v.Array)])
			v.Array = v.Array[:0]
		}
	}
	v.Scalar = ""
	v.Quoted = false
	v.Type = ValueTypeScalar
	v.pooled = true
	valuePool.Put(v)
}

// ParseYAML parses YAML input and returns a Value tree.
// The caller MUST call ReleaseValue on the returned tree when done
// to return nodes to the pool and prevent memory growth.
//
// Duplicate mapping keys resolve to the last occurrence. The YAML
// specification requires an error here; this parser is deliberately lenient
// (the .env parser similarly tolerates duplicates, skipping them). Use
// ParseYAMLWithPolicy to select a different resolution.
func ParseYAML(data []byte, maxDepth int) (*Value, error) {
	return ParseYAMLWithPolicy(data, maxDepth, DuplicateKeyInherit)
}

// ParseYAMLWithPolicy parses YAML input with an explicit duplicate-key
// policy (see DuplicateKeyPolicy). Release rules match ParseYAML.
func ParseYAMLWithPolicy(data []byte, maxDepth int, dupPolicy DuplicateKeyPolicy) (*Value, error) {
	// Lex directly over the caller's buffer (no full-file copy): every token
	// value is copied out as an independent string during tokenization, and
	// the Value tree holds only those copies, so aliasing data is safe for
	// the duration of this call.
	lexer := newYAMLLexer(data)
	// Seed tokenization with a pooled token slice — the largest per-parse
	// allocation (see tokenSlicePool).
	tokensPtr := getTokenSlice()
	tokens, err := lexer.tokenizeInto(*tokensPtr)
	*tokensPtr = tokens // tokenizeInto may reallocate; pool the final slice
	lexer.release()
	if err != nil {
		putTokenSlice(tokensPtr)
		return nil, err
	}

	parser := NewYAMLParserWithPolicy(tokens, maxDepth, dupPolicy)
	value, perr := parser.Parse()
	putTokenSlice(tokensPtr)
	if perr != nil {
		return nil, perr
	}
	return value, nil
}
