// Provides YAML flattening utilities.

package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

// YAMLFlattenConfig holds configuration for YAML flattening.
type YAMLFlattenConfig struct {
	// KeyDelimiter is the delimiter for nested keys (default: "_").
	KeyDelimiter string
	// ArrayIndexFormat controls how array indices are formatted.
	// "underscore": KEY_0, KEY_1, etc.
	// "bracket": KEY[0], KEY[1], etc.
	ArrayIndexFormat string
	// NullAsEmpty converts null/~ values to empty strings (default: true).
	NullAsEmpty bool
	// NumberAsString converts numbers to strings (default: true).
	NumberAsString bool
	// BoolAsString converts booleans to strings (default: true).
	BoolAsString bool
	// MaxDepth limits the maximum nesting depth to prevent stack overflow.
	MaxDepth int
	// DuplicateKey resolves duplicate keys inside inline JSON values
	// (see DuplicateKeyPolicy). Document-level YAML duplicates are resolved
	// by the parser (ParseYAMLWithPolicy) before flattening. The zero value
	// keeps the historical last-occurrence-wins behavior.
	DuplicateKey DuplicateKeyPolicy
}

// FlattenYAML converts a YAML Value tree to a flat map of string key-value pairs.
// Keys are converted to uppercase with the configured delimiter.
func FlattenYAML(value *Value, cfg YAMLFlattenConfig) (map[string]string, error) {
	if value == nil {
		return make(map[string]string), nil
	}

	// Estimate leaf count from the root map for pre-allocation
	estSize := estimateLeafCount(value)
	result := make(map[string]string, estSize)
	if err := flattenYAMLValue(value, "", cfg, result, 0); err != nil {
		return nil, err
	}

	return result, nil
}

// estimateLeafCount provides a rough estimate of the number of leaf values
// in a YAML tree to pre-size the result map. Only traverses one level deep.
func estimateLeafCount(v *Value) int {
	if v == nil {
		return 0
	}
	switch v.Type {
	case ValueTypeMap:
		return len(v.Map)
	case ValueTypeArray:
		return len(v.Array)
	default:
		return 1
	}
}

// flattenYAMLValue recursively flattens a YAML value.
func flattenYAMLValue(value *Value, prefix string, cfg YAMLFlattenConfig, result map[string]string, depth int) error {
	// Check depth limit - use >= for strict enforcement
	if depth >= cfg.MaxDepth {
		return &YAMLError{
			Path:    prefix,
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", cfg.MaxDepth),
		}
	}

	if value == nil {
		return nil
	}

	switch value.Type {
	case ValueTypeScalar:
		if prefix == "" {
			return nil
		}
		if value.Quoted {
			// Quoted scalars are always strings per YAML semantics: no inline
			// JSON parsing, no null/bool/number coercion, and interior
			// whitespace (including trailing) is significant. This is also
			// what makes Marshal→Unmarshal round trips lossless.
			return setFlatYAMLValue(result, prefix, value.Scalar)
		}
		// Check for inline JSON array or object
		scalar := TrimSpace(value.Scalar)
		if len(scalar) >= 2 {
			if (scalar[0] == '[' && scalar[len(scalar)-1] == ']') ||
				(scalar[0] == '{' && scalar[len(scalar)-1] == '}') {
				// Try to parse as inline JSON, then as a YAML flow collection
				// (unquoted scalars are not strict JSON). A syntax failure of
				// both forms falls back to treating the text as a plain scalar
				// (lenient recovery), but depth-limit, duplicate-key and
				// collision errors must propagate: swallowing them made the
				// MaxDepth boundary advisory-only — a depth hit silently
				// stored the raw scalar instead of erroring (same class for
				// the new dup checks).
				if err := flattenViaInlineJSON(scalar, prefix, cfg, result, depth); err != nil {
					var depthErr *YAMLError
					if errors.As(err, &depthErr) {
						return err
					}
					// Not valid inline JSON or YAML flow (syntax):
					// fall through to regular scalar
				} else {
					return nil
				}
			}
		}
		return setFlatYAMLValue(result, prefix, convertYAMLScalar(value.Scalar, cfg))

	case ValueTypeMap:
		if len(value.Map) == 0 && prefix != "" {
			// Empty map as empty string
			return setFlatYAMLValue(result, prefix, "")
		}
		for key, val := range value.Map {
			newPrefix := buildYAMLKey(prefix, key, cfg)
			if err := flattenYAMLValue(val, newPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}

	case ValueTypeArray:
		if len(value.Array) == 0 && prefix != "" {
			// Empty array as empty string
			return setFlatYAMLValue(result, prefix, "")
		}
		for i, val := range value.Array {
			newPrefix := buildYAMLArrayIndex(prefix, i, cfg)
			if err := flattenYAMLValue(val, newPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}
	}

	return nil
}

// setFlatYAMLValue stores a flattened leaf and rejects flat-key collisions:
// two DIFFERENT source paths collapsing onto the same flattened key (e.g.
// "a_b: 1" next to "a: {b: 2}", or a literal "a_0" next to an array "a").
// Before this check the winner depended on Go map iteration order, so the
// same file could produce different values across runs. Collisions are
// always an error — there is no previous behavior worth preserving.
func setFlatYAMLValue(result map[string]string, prefix, value string) error {
	if _, exists := result[prefix]; exists {
		return &YAMLError{
			Path: prefix,
			Message: fmt.Sprintf(
				"flat-key collision on %q: two different source paths collapse to the same flattened key",
				prefix),
			Err: ErrDuplicateKey,
		}
	}
	result[prefix] = value
	return nil
}

// convertYAMLScalar converts a YAML scalar to a string based on configuration.
// Uses zero-allocation byte-level comparisons instead of strings.ToLower/TrimSpace.
func convertYAMLScalar(s string, cfg YAMLFlattenConfig) string {
	// Trim whitespace without allocation (returns original if already trimmed)
	s = TrimSpace(s)

	// Handle null/empty using case-insensitive comparison without allocation
	if len(s) == 0 {
		if cfg.NullAsEmpty {
			return ""
		}
		return "null"
	}
	if s == "~" || EqualFoldASCII(s, "null") {
		if cfg.NullAsEmpty {
			return ""
		}
		return "null"
	}

	// Handle boolean using case-insensitive comparison without allocation
	if EqualFoldASCII(s, "true") {
		if cfg.BoolAsString {
			return "true"
		}
		return s
	}
	if EqualFoldASCII(s, "false") {
		if cfg.BoolAsString {
			return "false"
		}
		return s
	}

	// Handle numbers: fast path to skip strconv when the string clearly isn't a number.
	// This avoids the massive allocation from strconv.syntaxError for non-numeric scalars.
	if cfg.NumberAsString {
		if looksLikeNumber(s) {
			// Try integer
			if num, err := strconv.ParseInt(s, 10, 64); err == nil {
				return strconv.FormatInt(num, 10)
			}
			// Try float
			if num, err := strconv.ParseFloat(s, 64); err == nil {
				if num == float64(int64(num)) {
					return strconv.FormatInt(int64(num), 10)
				}
				return strconv.FormatFloat(num, 'f', -1, 64)
			}
		}
	}

	return s
}

// looksLikeNumber does a cheap byte-level scan to reject strings that
// cannot possibly be parsed as an integer or float.
// Returns true only if the string starts with a digit or sign and
// contains only [0-9eE.+-] with at least one digit.
func looksLikeNumber(s string) bool {
	if len(s) == 0 {
		return false
	}
	i := 0
	if s[0] == '+' || s[0] == '-' {
		if len(s) == 1 {
			return false
		}
		i = 1
	}
	hasDigit := false
	for ; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
			return false
		}
	}
	return hasDigit
}

// buildYAMLKey constructs a key from prefix and key parts.
// Delegates to the shared flattener key builder (see flatten_keys.go).
func buildYAMLKey(prefix, key string, cfg YAMLFlattenConfig) string {
	return buildFlatKey(prefix, key, cfg.KeyDelimiter)
}

// buildYAMLArrayIndex constructs a key for array elements.
// Delegates to the shared flattener key builder (see flatten_keys.go).
func buildYAMLArrayIndex(prefix string, index int, cfg YAMLFlattenConfig) string {
	return buildFlatArrayIndex(prefix, index, cfg.KeyDelimiter, cfg.ArrayIndexFormat)
}

// flattenInlineJSON parses and flattens inline JSON arrays or objects within YAML.
func flattenInlineJSON(jsonStr, prefix string, cfg YAMLFlattenConfig, result map[string]string, depth int) error {
	// SECURITY: Pre-validate JSON nesting depth and node count to prevent
	// excessive memory allocation during json.Unmarshal. Shares the same
	// scanning logic as the standalone JSON path (see scanJSONLimits in
	// json_flatten.go). Both violations propagate as *YAMLError so the
	// caller treats them as hard errors, not syntax-recovery fallbacks.
	// The bytes are converted once: the same slice feeds the limit scan,
	// json.Unmarshal and the duplicate-key pre-scan below.
	b := []byte(jsonStr)
	depthExceeded, nodesExceeded := scanJSONLimits(b, depth, cfg.MaxDepth, HardMaxJSONNodes)
	if depthExceeded {
		return &YAMLError{
			Path:    prefix,
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", cfg.MaxDepth),
		}
	}
	if nodesExceeded {
		return &YAMLError{
			Path:    prefix,
			Message: fmt.Sprintf("maximum node count exceeded (%d)", HardMaxJSONNodes),
		}
	}

	// Parse the inline JSON. A syntax error returns to the caller, whose
	// lenient-recovery fallback treats the text as a plain scalar.
	var raw interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}

	// Duplicate-key policy for the payload — enforced only after the text
	// proved to be a parseable collection (strict JSON or YAML flow), so
	// scalars that merely LOOK like a collection ("{x:1}" — colon not
	// followed by a space) keep their plain-scalar fallback under every
	// policy (the standalone JSON path applies the same rules before
	// decoding; see FlattenJSON). A payload WITHOUT duplicates resolves
	// identically under every policy and is never rejected here; FirstWins
	// rejects only payloads the pre-scan proves to contain duplicates. These
	// are hard errors (*YAMLError), so the caller in flattenYAMLValue
	// propagates them rather than falling back.
	switch cfg.DuplicateKey {
	case DuplicateKeyFirstWins:
		if key, dup := detectJSONDuplicateKeys(b, cfg.MaxDepth-depth); dup {
			return &YAMLError{
				Path: prefix,
				Message: fmt.Sprintf(
					"duplicate object key %q in inline JSON cannot be resolved with DuplicateKeyFirstWins "+
						"(encoding/json discards earlier occurrences); use LastWins or Error", key),
				Err: ErrDuplicateKey,
			}
		}
	case DuplicateKeyError:
		if key, dup := detectJSONDuplicateKeys(b, cfg.MaxDepth-depth); dup {
			return &YAMLError{
				Path:    prefix,
				Message: fmt.Sprintf("duplicate object key %q in inline JSON", key),
				Err:     ErrDuplicateKey,
			}
		}
	}

	// Flatten the decoded payload through the shared walker (same rules as
	// the standalone JSON path, with *YAMLError semantics). The payload text
	// starts with '[' or '{' (the caller's inline-JSON detection); empty
	// containers keep the key as "" — unified with the YAML block form and
	// null handling.
	return flattenDecoded(raw, prefix, decodedFlattenOpts{
		maxDepth:       cfg.MaxDepth,
		nullAsEmpty:    cfg.NullAsEmpty,
		numberAsString: cfg.NumberAsString,
		delim:          cfg.KeyDelimiter,
		indexFmt:       cfg.ArrayIndexFormat,
	}, result, depth, setFlatYAMLValue, yamlDepthError, yamlUnsupportedError)
}

// yamlDepthError builds the inline path's depth-limit error.
func yamlDepthError(path string, maxDepth int) error {
	return &YAMLError{
		Path:    path,
		Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", maxDepth),
	}
}

// yamlUnsupportedError builds the terminal-type error (unreachable through
// json.Unmarshal; the old YAML copy silently stored a "%v" rendering).
func yamlUnsupportedError(path string, v interface{}) error {
	return &YAMLError{
		Path:    path,
		Message: fmt.Sprintf("unsupported decoded JSON type: %T", v),
	}
}

// flattenViaInlineJSON flattens a collection-looking scalar through the
// inline-JSON path, first as strict JSON and then — when the text is not
// valid JSON — as a YAML flow collection converted by yamlFlowToJSON.
//
// Error semantics match the single-attempt behavior this replaces: hard
// failures (*YAMLError: depth, node count, duplicate keys, flat-key
// collisions) always propagate so the caller never silently degrades them;
// only a syntax failure of BOTH forms is returned as a plain error for the
// caller's lenient scalar fallback.
func flattenViaInlineJSON(scalar, prefix string, cfg YAMLFlattenConfig, result map[string]string, depth int) error {
	err := flattenInlineJSON(scalar, prefix, cfg, result, depth)
	if err == nil {
		return nil
	}
	var yamlErr *YAMLError
	if errors.As(err, &yamlErr) {
		return err // hard error: never a fallback candidate
	}
	converted, ok := yamlFlowToJSON(scalar)
	if !ok {
		return err // not a YAML flow collection either: original syntax error
	}
	return flattenInlineJSON(converted, prefix, cfg, result, depth)
}

// yamlFlowToJSON converts a YAML flow collection ([a, b], {x: 1}, ['q', "d"])
// into equivalent JSON text so it can be flattened through the inline-JSON
// path. YAML flow syntax differs from JSON: map keys and sequence elements
// may be unquoted plain scalars, single-quoted strings are allowed, "~" is
// null, and " #" starts a comment. It returns ok=false when the text is not
// a well-formed flow collection; the caller then keeps its plain-scalar
// fallback.
//
// The conversion is a single linear scan without regular expressions.
// Plain scalars that look numeric, or equal true/false/null/~, are emitted
// as canonical JSON values; everything else becomes a JSON string. The
// output is always valid JSON when ok=true.
func yamlFlowToJSON(s string) (string, bool) {
	p := &flowParser{s: s}
	p.skipSpace()
	if p.i >= len(p.s) {
		return "", false
	}
	v, ok := p.parseValue(0)
	if !ok {
		return "", false
	}
	p.skipSpace()
	if p.i != len(p.s) {
		return "", false // trailing garbage
	}
	return string(v), true
}

// maxFlowDepth bounds flowParser recursion. Real depth policing stays with
// scanJSONLimits inside the inline-JSON path; this guard only protects the
// converter itself from pathological inputs before that scan runs.
const maxFlowDepth = 256

// flowParser is a single-pass YAML flow collection to JSON converter.
type flowParser struct {
	s string
	i int
}

// skipSpace consumes whitespace and comments ('#' to end of input).
func (p *flowParser) skipSpace() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\r', '\n':
			p.i++
		case '#':
			p.i = len(p.s)
		default:
			return
		}
	}
}

func (p *flowParser) parseValue(depth int) ([]byte, bool) {
	if depth > maxFlowDepth {
		return nil, false
	}
	p.skipSpace()
	if p.i >= len(p.s) {
		return nil, false
	}
	switch p.s[p.i] {
	case '[':
		return p.parseSeq(depth)
	case '{':
		return p.parseMap(depth)
	case '"':
		return p.parseDoubleQuoted()
	case '\'':
		return p.parseSingleQuoted()
	default:
		return p.parsePlainScalar()
	}
}

func (p *flowParser) parseSeq(depth int) ([]byte, bool) {
	p.i++ // consume '['
	out := []byte{'['}
	for {
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, false
		}
		if p.s[p.i] == ']' {
			p.i++
			return append(out, ']'), true
		}
		v, ok := p.parseValue(depth + 1)
		if !ok {
			return nil, false
		}
		out = append(out, v...)
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, false
		}
		switch p.s[p.i] {
		case ',':
			p.i++
			p.skipSpace()
			// Tolerate a trailing comma before the closing bracket; it is
			// not emitted (JSON rejects trailing commas).
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				return append(out, ']'), true
			}
			out = append(out, ',')
		case ']':
			p.i++
			return append(out, ']'), true
		default:
			return nil, false
		}
	}
}

func (p *flowParser) parseMap(depth int) ([]byte, bool) {
	p.i++ // consume '{'
	out := []byte{'{'}
	for {
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, false
		}
		if p.s[p.i] == '}' {
			p.i++
			return append(out, '}'), true
		}
		key, ok := p.parseKey()
		if !ok {
			return nil, false
		}
		out = append(out, key...)
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, false
		}
		p.i++ // consume ':'
		out = append(out, ':')
		v, ok := p.parseValue(depth + 1)
		if !ok {
			return nil, false
		}
		out = append(out, v...)
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, false
		}
		switch p.s[p.i] {
		case ',':
			p.i++
			p.skipSpace()
			// Tolerate a trailing comma before the closing brace; it is
			// not emitted (JSON rejects trailing commas).
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				return append(out, '}'), true
			}
			out = append(out, ',')
		case '}':
			p.i++
			return append(out, '}'), true
		default:
			return nil, false
		}
	}
}

// parseKey parses a flow-map key: a quoted string or a plain scalar, always
// emitted as a JSON string (JSON object keys must be strings).
func (p *flowParser) parseKey() ([]byte, bool) {
	p.skipSpace()
	if p.i >= len(p.s) {
		return nil, false
	}
	switch p.s[p.i] {
	case '"':
		return p.parseDoubleQuoted()
	case '\'':
		return p.parseSingleQuoted()
	default:
		return appendJSONString(nil, p.scanPlain()), true
	}
}

// parsePlainScalar scans an unquoted scalar and renders it as a JSON value.
func (p *flowParser) parsePlainScalar() ([]byte, bool) {
	return plainToJSON(p.scanPlain()), true
}

// scanPlain consumes an unquoted plain scalar: bytes up to the first flow
// indicator (',', ']', '}'), a map-separating ':', or a comment ('#' after
// whitespace). Leading whitespace is skipped by the caller; trailing
// whitespace is trimmed here. An empty result is a valid (null) scalar.
func (p *flowParser) scanPlain() string {
	start := p.i
	end := -1 // one past the last non-space byte
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == ',' || c == ']' || c == '}' {
			break
		}
		if c == ':' && p.isMapSeparator(p.i) {
			break
		}
		if c == '#' && p.i > start && isFlowSpace(p.s[p.i-1]) {
			break
		}
		p.i++
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			end = p.i
		}
	}
	if end == -1 {
		return ""
	}
	return p.s[start:end]
}

// isMapSeparator reports whether the ':' at index at separates a map key
// from its value: YAML requires a following space or a flow indicator.
// Colons inside scalars ("http://x") therefore stay part of the scalar.
func (p *flowParser) isMapSeparator(at int) bool {
	if at+1 >= len(p.s) {
		return true
	}
	switch p.s[at+1] {
	case ' ', '\t', '\r', '\n', ',', '}', ']':
		return true
	}
	return false
}

// isFlowSpace reports whether c is whitespace inside a flow collection.
func isFlowSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// plainToJSON renders a plain flow scalar as a JSON value: numbers and the
// literals true/false/null/~ become canonical JSON values; anything else
// becomes a JSON string. Canonicalization (and rejecting Inf/NaN, which
// ParseFloat accepts but JSON does not) guarantees valid JSON output.
func plainToJSON(s string) []byte {
	switch {
	case s == "~" || EqualFoldASCII(s, "null"):
		return []byte("null")
	case EqualFoldASCII(s, "true"):
		return []byte("true")
	case EqualFoldASCII(s, "false"):
		return []byte("false")
	}
	if looksLikeNumber(s) {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return strconv.AppendInt(nil, n, 10)
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil &&
			!math.IsInf(f, 0) && !math.IsNaN(f) {
			return strconv.AppendFloat(nil, f, 'g', -1, 64)
		}
	}
	return appendJSONString(nil, s)
}

// parseDoubleQuoted consumes a double-quoted YAML string and returns it as a
// JSON string literal. Only escapes JSON itself defines are translated;
// YAML-only escapes (\x, \U, \0, ...) make the whole conversion fail so the
// caller keeps its plain-scalar fallback for the value.
func (p *flowParser) parseDoubleQuoted() ([]byte, bool) {
	p.i++ // opening quote
	var raw []byte
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch c {
		case '"':
			p.i++
			return appendJSONString(nil, string(raw)), true
		case '\\':
			if p.i+1 >= len(p.s) {
				return nil, false
			}
			e := p.s[p.i+1]
			switch e {
			case '"', '\\', '/':
				raw = append(raw, e)
			case 'b':
				raw = append(raw, '\b')
			case 'f':
				raw = append(raw, '\f')
			case 'n':
				raw = append(raw, '\n')
			case 'r':
				raw = append(raw, '\r')
			case 't':
				raw = append(raw, '\t')
			case 'u':
				if p.i+6 > len(p.s) {
					return nil, false
				}
				v, ok := parseHex4(p.s[p.i+2 : p.i+6])
				if !ok {
					return nil, false
				}
				raw = utf8.AppendRune(raw, rune(v))
				p.i += 4
			default:
				return nil, false
			}
			p.i += 2
		default:
			raw = append(raw, c)
			p.i++
		}
	}
	return nil, false // unterminated
}

// parseSingleQuoted consumes a single-quoted YAML string (” escapes a
// quote) and returns it as a JSON string literal.
func (p *flowParser) parseSingleQuoted() ([]byte, bool) {
	p.i++ // opening quote
	var raw []byte
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\'' {
			if p.i+1 < len(p.s) && p.s[p.i+1] == '\'' {
				raw = append(raw, '\'')
				p.i += 2
				continue
			}
			p.i++
			return appendJSONString(nil, string(raw)), true
		}
		raw = append(raw, c)
		p.i++
	}
	return nil, false // unterminated
}

// hexDigits maps nibbles to lowercase hexadecimal digits.
var hexDigits = "0123456789abcdef"

// parseHex4 decodes exactly four hexadecimal digits.
func parseHex4(s string) (uint32, bool) {
	if len(s) != 4 {
		return 0, false
	}
	var v uint32
	for i := 0; i < 4; i++ {
		c := s[i]
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return 0, false
		}
		v = v<<4 | d
	}
	return v, true
}

// appendJSONString appends s to dst as a quoted, escaped JSON string.
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"')
}
