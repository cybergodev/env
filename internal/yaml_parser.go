// Package internal provides YAML parsing utilities.
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
	v.Type = ValueTypeScalar
	v.Scalar = s
	v.Line = line
	v.Column = col
	return v
}

// NewMapValue creates a new map value with pre-allocated capacity (from pool).
func NewMapValue(line, col int) *Value {
	v := valuePool.Get().(*Value)
	v.Type = ValueTypeMap
	if v.Map == nil {
		v.Map = make(map[string]*Value, 8)
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
	tokens   []Token
	pos      int
	maxDepth int
}

// NewYAMLParser creates a new YAML parser.
func NewYAMLParser(tokens []Token, maxDepth int) *Parser {
	if maxDepth <= 0 {
		maxDepth = 10
	}
	return &Parser{
		tokens:   tokens,
		maxDepth: maxDepth,
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

	// Parse root level
	return p.parseMap(depth, 0)
}

// parseMap parses a YAML map.
func (p *Parser) parseMap(depth, expectedIndent int) (*Value, error) {
	root := NewMapValue(p.currentLine(), p.currentColumn())

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
				value = NewScalarValue(p.current().Value, p.currentLine(), p.currentColumn())
				p.advance()
			} else if p.current().Type == TokenComment {
				// Skip comments and check for nested structure on next line
				for p.current().Type == TokenComment {
					p.advance()
				}
				// Now expect newline followed by possible nested content
				if p.current().Type == TokenNewline {
					p.skipNewlines()
					for p.current().Type == TokenComment {
						p.advance()
						p.skipNewlines()
					}
					if p.current().Type == TokenIndent {
						p.advance()
						value, err = p.parseNestedValue(depth+1, currentIndent+1)
					} else {
						value = NewScalarValue("", p.currentLine(), p.currentColumn())
					}
				} else {
					value = NewScalarValue("", p.currentLine(), p.currentColumn())
				}
			} else if p.current().Type == TokenNewline {
				// Skip newlines and check for nested structure on next line
				p.skipNewlines()

				// Skip any comments between newlines and nested content
				for p.current().Type == TokenComment {
					p.advance()
					p.skipNewlines()
				}

				if p.current().Type == TokenIndent {
					// Nested structure after newline(s) and comment(s)
					p.advance()
					value, err = p.parseNestedValue(depth+1, currentIndent+1)
				} else {
					// Empty value
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

			root.Map[key] = value
		} else if p.current().Type == TokenDash {
			// This shouldn't happen at map level without a key
			break
		} else if p.current().Type == TokenComment {
			p.advance()
		} else {
			// Unknown token, skip
			p.advance()
		}
	}

	return root, nil
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
		value := NewScalarValue(p.current().Value, p.currentLine(), p.currentColumn())
		p.advance()
		return value, nil
	} else if p.current().Type == TokenDedent {
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
				value = NewScalarValue(p.current().Value, p.currentLine(), p.currentColumn())
				p.advance()
			} else if p.current().Type == TokenKey {
				// Map as array item (KEY at same indent level as DASH)
				value, err = p.parseMap(depth+1, currentIndent)
			} else if p.current().Type == TokenDash {
				// Nested array
				value, err = p.parseArray(depth+1, currentIndent+1)
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

// current returns the current token.
func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
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

// ReleaseValue recursively returns a Value tree to the pool.
// After calling this, the Value and all its children must not be accessed.
func ReleaseValue(v *Value) {
	if v == nil {
		return
	}
	switch v.Type {
	case ValueTypeMap:
		for key, child := range v.Map {
			ReleaseValue(child)
			delete(v.Map, key)
		}
		v.Map = nil
	case ValueTypeArray:
		for _, child := range v.Array {
			ReleaseValue(child)
		}
		v.Array = nil
	}
	v.Scalar = ""
	v.Type = ValueTypeScalar
	valuePool.Put(v)
}

// ParseYAML parses YAML input and returns a Value tree.
// The caller MUST call ReleaseValue on the returned tree when done
// to return nodes to the pool and prevent memory growth.
func ParseYAML(data []byte, maxDepth int) (*Value, error) {
	lexer := NewYAMLLexer(string(data))
	tokens, err := lexer.Tokenize()
	lexer.release()
	if err != nil {
		return nil, err
	}

	parser := NewYAMLParser(tokens, maxDepth)
	return parser.Parse()
}
