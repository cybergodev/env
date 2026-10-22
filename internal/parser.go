// Provides internal line parsing utilities.

package internal

import (
	"bytes"
	"strings"
)

// LineParserConfig holds the configuration needed for line parsing.
// Variable-count limits, the overwrite policy and expansion are enforced by
// the callers that own the whole-file view (the root parser checks
// MaxVariables over the parsed map, the loader applies the overwrite policy
// when merging, and expansion runs as a separate pass); per-line parsing
// needs only the two syntax switches below.
type LineParserConfig struct {
	AllowExportPrefix bool
	AllowYamlSyntax   bool
	// ExpandVariables mirrors the root Config flag: the line parser needs it
	// to know whether a double-quoted "\$" must survive as "$$" for the
	// later expansion pass (see parseDoubleQuotedBytes).
	ExpandVariables bool
}

// LineParser handles parsing of individual lines.
type LineParser struct {
	config         LineParserConfig
	keyValidator   LineKeyValidator
	valueValidator LineValueValidator
	auditor        LineAuditLogger
	expander       LineExpander
}

// NewLineParser creates a new LineParser.
// The validators, auditor, and expander parameters accept interfaces rather than concrete types,
// allowing for better testability and flexibility.
//
// Value validation follows the keyValidator's capabilities: when it also
// implements LineValueValidator (as the full internal Validator does), value
// checks run; a keyValidator that does not leaves value validation off
// entirely — construct through a full Validator to get both.
func NewLineParser(cfg LineParserConfig, keyValidator LineKeyValidator, auditor LineAuditLogger, expander LineExpander) *LineParser {
	// If keyValidator also implements LineValueValidator, use it for both
	var valueValidator LineValueValidator
	if vv, ok := keyValidator.(LineValueValidator); ok {
		valueValidator = vv
	}

	return &LineParser{
		config:         cfg,
		keyValidator:   keyValidator,
		valueValidator: valueValidator,
		auditor:        auditor,
		expander:       expander,
	}
}

// hasNonSpace reports whether the line contains any byte other than space or
// tab. Used to keep the no-separator audit quiet for whitespace-only input.
func hasNonSpace(line []byte) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return true
		}
	}
	return false
}

// ParseLineBytes parses a single line from a byte slice and returns the key and value.
// This version avoids string allocation by working directly with bytes.
func (p *LineParser) ParseLineBytes(line []byte) (string, string, error) {
	key, value, _, err := p.parseLineBytes(line)
	return key, value, err
}

// ParseLineBytesQuote is ParseLineBytes that additionally reports whether the
// value was single-quoted. Single-quoted .env values are literal (bash '...'
// semantics): the caller must exempt them from variable expansion, and a '$'
// inside them stays a literal dollar.
func (p *LineParser) ParseLineBytesQuote(line []byte) (string, string, bool, error) {
	return p.parseLineBytes(line)
}

// parseLineBytes is the shared line-parsing core behind ParseLineBytes and
// ParseLineBytesQuote.
func (p *LineParser) parseLineBytes(line []byte) (string, string, bool, error) {
	// Fast path for empty or very short lines
	if len(line) == 0 {
		return "", "", false, nil
	}

	// Handle export prefix - use byte-level comparison to avoid string allocation
	if p.config.AllowExportPrefix && len(line) > 7 {
		// Check for "export " prefix (7 characters: e-x-p-o-r-t plus a
		// space OR a tab — bash accepts both after export)
		if line[0] == 'e' && line[1] == 'x' && line[2] == 'p' &&
			line[3] == 'o' && line[4] == 'r' && line[5] == 't' &&
			(line[6] == ' ' || line[6] == '\t') {
			line = line[7:]
		}
	}

	// Find the separator using byte-level search.
	// A manual loop outperforms bytes.IndexAny for typical short env lines
	// (≤40 chars) because IndexAny's charset-bitset setup overhead exceeds
	// the per-byte comparison cost at that length.
	sepIdx := -1
	for i := 0; i < len(line); i++ {
		if line[i] == '=' {
			sepIdx = i
			break
		}
		// ':' separates only in YAML-syntax mode. With the flag off (the
		// default), prose lines like "See: https://example.com" previously
		// parsed as a variable named "See"; they are now dropped (and
		// audited) as no-separator lines, matching peer .env parsers.
		if line[i] == ':' && p.config.AllowYamlSyntax {
			sepIdx = i
			break
		}
	}
	if sepIdx == -1 {
		// A non-blank line without a separator is dropped by contract
		// (empty key, caller skips it). Audit the drop so a typo like a
		// missing '=' is observable when auditing is enabled — the line
		// content goes through MaskInString, never in raw form.
		if hasNonSpace(line) {
			_ = p.auditor.LogError(ActionParse, "", "no separator, line skipped: "+MaskInString(string(line)))
		}
		return "", "", false, nil // No assignment on this line
	}

	// Trim key using byte-level operations
	keyStart := 0
	keyEnd := sepIdx
	for keyStart < keyEnd && (line[keyStart] == ' ' || line[keyStart] == '\t') {
		keyStart++
	}
	for keyEnd > keyStart && (line[keyEnd-1] == ' ' || line[keyEnd-1] == '\t') {
		keyEnd--
	}

	// SECURITY: Must copy the key string because the scanner buffer will be
	// overwritten on the next Scan() call. Using bytesToString here would cause
	// the interned key to point to invalid memory after the buffer is reused.
	// InternKeyBytes returns a stable heap copy (independent of the buffer) on
	// a cache miss, while avoiding the temporary string allocation that an
	// InternKeyBytes(string(...))-style call would incur on every call —
	// including cache hits.
	key := InternKeyBytes(line[keyStart:keyEnd])

	// Validate key
	if err := p.keyValidator.ValidateKey(key); err != nil {
		return "", "", false, err
	}

	// Parse the value
	value := line[sepIdx+1:]
	parsedValue, singleQuoted, err := p.parseValueBytes(value)
	if err != nil {
		return "", "", false, err
	}

	// Validate value
	if p.valueValidator != nil {
		if err := p.valueValidator.ValidateValue(parsedValue); err != nil {
			return "", "", false, err
		}
	}

	return key, parsedValue, singleQuoted, nil
}

// ParseValueBytes parses a value from a byte slice handling quotes and escapes.
// This version avoids string allocation until the final result.
func (p *LineParser) ParseValueBytes(value []byte) (string, error) {
	s, _, err := p.parseValueBytes(value)
	return s, err
}

// parseValueBytes is ParseValueBytes that additionally reports whether the
// value was single-quoted (see ParseLineBytesQuote for why callers need this).
func (p *LineParser) parseValueBytes(value []byte) (string, bool, error) {
	// Fast path for empty value
	if len(value) == 0 {
		return "", false, nil
	}

	// Fast byte-level trim for leading/trailing whitespace
	start := 0
	end := len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t') {
		end--
	}
	value = value[start:end]

	if len(value) == 0 {
		return "", false, nil
	}

	// Handle quoted values. A closing quote followed by whitespace and an
	// inline '#' comment is accepted — KEY="v" # note previously failed the
	// whole file because the quoted parsers demanded the final byte be the
	// quote.
	switch value[0] {
	case '"', '\'':
		quote := value[0]
		end := findClosingQuote(value, quote)
		if end == -1 {
			return "", false, ErrInvalidValue
		}
		rest := value[end+1:]
		j := 0
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j < len(rest) && (rest[j] != '#' || j == 0) {
			return "", false, ErrInvalidValue // junk after the closing quote
		}
		if quote == '\'' {
			s, err := ParseSingleQuotedBytes(value[:end+1])
			return s, true, err
		}
		s, err := parseDoubleQuotedBytes(value[:end+1], p.config.ExpandVariables)
		return s, false, err
	}

	// Handle YAML-style values if enabled
	if p.config.AllowYamlSyntax {
		if unquoted, ok := TryParseYamlValueBytes(value); ok {
			return unquoted, false, nil
		}
	}

	// Unquoted value - remove inline comments using byte-level search
	// Find the " #" pattern — a '#' preceded by a space OR a tab (bash
	// starts comments after any whitespace; the tab form previously stayed
	// in the value).
	commentIdx := -1
	if len(value) >= 2 {
		for i := 0; i < len(value)-1; i++ {
			if (value[i] == ' ' || value[i] == '\t') && value[i+1] == '#' {
				commentIdx = i
				break
			}
		}
	}
	if commentIdx != -1 {
		value = value[:commentIdx]
		// Trim trailing whitespace after comment removal
		end = len(value)
		for end > 0 && (value[end-1] == ' ' || value[end-1] == '\t') {
			end--
		}
		value = value[:end]
	}

	// SECURITY: The string() conversion creates an independent copy of the bytes.
	// value is a slice into the scanner's reusable buffer, which will be
	// overwritten on the next Scan() call. Without this copy, the returned
	// string would become corrupted when the buffer is reused.
	// The allocation cost is necessary for memory safety.
	return string(value), false, nil
}

// ParseBoolLoose is the library-wide boolean grammar, shared by the typed
// getters (GetBool, GetSlice[bool]) and the struct mapper (ParseInto /
// UnmarshalInto / []bool fields): 1/0, no/on, yes/off, true/false,
// enabled/disabled, ASCII case-insensitive. Previously the two layers used
// two grammars — accessors accepted "yes" while struct mapping (strict
// strconv.ParseBool) rejected it — so the same string parsed differently
// depending on the API. ok is false for anything else; the caller decides
// the error shape.
func ParseBoolLoose(s string) (value bool, ok bool) {
	n := len(s)
	switch n {
	case 1:
		if s[0] == '1' || s[0] == '0' {
			return s[0] == '1', true
		}
	case 2:
		if EqualFoldASCII(s, "no") {
			return false, true
		}
		if EqualFoldASCII(s, "on") {
			return true, true
		}
	case 3:
		if EqualFoldASCII(s, "yes") {
			return true, true
		}
		if EqualFoldASCII(s, "off") {
			return false, true
		}
	case 4:
		if EqualFoldASCII(s, "true") {
			return true, true
		}
	case 5:
		if EqualFoldASCII(s, "false") {
			return false, true
		}
	case 7:
		if EqualFoldASCII(s, "enabled") {
			return true, true
		}
	case 8:
		if EqualFoldASCII(s, "disabled") {
			return false, true
		}
	}
	return false, false
}

// escapeTable provides O(1) lookup for escape sequence translation.
// Index by escaped character byte value, returns the translated character.
// 0 means pass through the character as-is.
var escapeTable = [256]byte{
	'n': '\n', 'r': '\r', 't': '\t',
	'\\': '\\', '"': '"', '$': '$',
}

// ParseDoubleQuotedBytes handles double-quoted values from a byte slice with escape sequences.
func ParseDoubleQuotedBytes(value []byte) (string, error) {
	return parseDoubleQuotedBytes(value, false)
}

// parseDoubleQuotedBytes is ParseDoubleQuotedBytes with a mode for values
// that will later pass through variable expansion: keepDollarEscape re-emits
// an escaped dollar as "$$", which the expander folds back to one literal
// dollar. Without it the unescape below would turn "\$5" into "$5" and the
// expansion pass would then treat the $5 as a reference — the exact escape
// the syntax exists to provide would be destroyed by the two-pass design.
func parseDoubleQuotedBytes(value []byte, keepDollarEscape bool) (string, error) {
	if len(value) < 2 || value[len(value)-1] != '"' {
		return "", ErrInvalidValue
	}

	// Extract content between quotes
	content := value[1 : len(value)-1]

	// Fast path: no escape sequences
	//
	// SECURITY: The string(content) conversion is essential here.
	// content is a slice into the scanner's reusable buffer, which will be
	// overwritten on the next Scan() call. The string() conversion creates
	// an independent copy of the bytes, ensuring the returned string remains
	// valid after the scanner buffer is reused.
	//
	// DO NOT optimize this to use unsafe.StringData or similar - the copy
	// is necessary for memory safety.
	if bytes.IndexByte(content, '\\') == -1 {
		return string(content), nil
	}

	// Use pooled byte slice for escaped content (more efficient than strings.Builder)
	buf := GetByteSlice()
	defer PutByteSlice(buf)

	// Ensure capacity - allocate exactly what we need, no more
	if cap(*buf) < len(content) {
		*buf = make([]byte, 0, len(content))
	}
	*buf = (*buf)[:0]

	// Optimized escape processing with lookup table
	for i := 0; i < len(content); {
		c := content[i]
		if c == '\\' && i+1 < len(content) {
			escaped := content[i+1]
			if escaped == '$' && keepDollarEscape {
				// Preserve the escape across the later expansion pass (see
				// the function comment).
				*buf = append(*buf, '$', '$')
				i += 2
				continue
			}
			// Use lookup table for O(1) escape translation
			if translated := escapeTable[escaped]; translated != 0 {
				*buf = append(*buf, translated)
			} else {
				// Unknown escape keeps its backslash (bash semantics):
				// "C:\Users\foo" previously lost every backslash, and
				// "\temp" turned into a TAB.
				*buf = append(*buf, '\\', escaped)
			}
			i += 2
		} else {
			*buf = append(*buf, c)
			i++
		}
	}

	// SECURITY: Must copy the result because buf is a pooled buffer that will be
	// returned to the pool and potentially reused by other goroutines.
	// Using bytesToString here would cause data corruption when the buffer is reused.
	return string(*buf), nil
}

// findClosingQuote returns the index of the quote character that closes the
// quoted value opening at value[0], or -1 when unterminated. Double-quoted
// content skips \<char> escape pairs; single-quoted content has no escapes,
// so the first quote closes (the historical last-byte-is-a-quote check
// mis-parsed pathological inputs such as "'a'b'" as a'b).
func findClosingQuote(value []byte, quote byte) int {
	for i := 1; i < len(value); i++ {
		c := value[i]
		if quote == '"' && c == '\\' {
			i++ // skip the escaped character
			continue
		}
		if c == quote {
			return i
		}
	}
	return -1
}

// ParseSingleQuotedBytes handles single-quoted values from a byte slice (no escape processing).
func ParseSingleQuotedBytes(value []byte) (string, error) {
	if len(value) < 2 || value[len(value)-1] != '\'' {
		return "", ErrInvalidValue
	}

	// SECURITY: The string() conversion creates an independent copy of the bytes.
	// value is a slice into the scanner's reusable buffer, which will be
	// overwritten on the next Scan() call. Without this copy, the returned
	// string would become corrupted when the buffer is reused.
	return string(value[1 : len(value)-1]), nil
}

// TryParseYamlValueBytes attempts to parse YAML-style values from a byte slice.
func TryParseYamlValueBytes(value []byte) (string, bool) {
	// Check for YAML boolean/null values using byte-level comparison
	if len(value) == 4 {
		if (value[0] == 't' || value[0] == 'T') && (value[1] == 'r' || value[1] == 'R') &&
			(value[2] == 'u' || value[2] == 'U') && (value[3] == 'e' || value[3] == 'E') {
			return string(value), true
		}
		if (value[0] == 'n' || value[0] == 'N') && (value[1] == 'u' || value[1] == 'U') &&
			(value[2] == 'l' || value[2] == 'L') && (value[3] == 'l' || value[3] == 'L') {
			return "", true
		}
	}
	if len(value) == 5 {
		if (value[0] == 'f' || value[0] == 'F') && (value[1] == 'a' || value[1] == 'A') &&
			(value[2] == 'l' || value[2] == 'L') && (value[3] == 's' || value[3] == 'S') &&
			(value[4] == 'e' || value[4] == 'E') {
			return string(value), true
		}
	}
	if len(value) == 1 && value[0] == '~' {
		return "", true
	}

	// Check for YAML numbers
	if IsYamlNumberBytes(value) {
		return string(value), true
	}

	return "", false
}

// IsYamlNumberBytes checks if a byte slice is a valid YAML number.
func IsYamlNumberBytes(s []byte) bool {
	if len(s) == 0 {
		return false
	}

	// Simple check for integer/float patterns
	hasDigit := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if c != '-' && c != '+' && c != '.' && c != 'e' && c != 'E' {
			return false
		}
	}
	return hasDigit
}

// ExpandAll expands all variables in the map. The noExpand set names keys
// whose values were single-quoted and must pass through literally (nil
// expands everything). Exempt values still serve as lookup sources for
// other references — expansion inserts looked-up text without rescanning it.
// Returns the original map if no expansion is needed, avoiding unnecessary allocations.
// This method delegates to Expander.ExpandAllInMap for the actual expansion logic.
func (p *LineParser) ExpandAll(vars map[string]string, noExpand map[string]struct{}) (map[string]string, error) {
	// Type assert to concrete Expander type to access optimized ExpandAllInMap
	// This is safe because we always use *Expander internally
	if expander, ok := p.expander.(*Expander); ok {
		result, err := expander.ExpandAllInMap(vars, noExpand)
		if err != nil {
			// Log the failure with its actual cause — ":?" required-variable
			// and depth errors previously all logged as "cycle detected".
			if expErr, ok := err.(*ExpansionError); ok && expErr.Key != "" {
				msg := "expansion depth exceeded"
				switch {
				case expErr.Kind == ExpansionRequiredKind:
					msg = "required variable not set"
				case strings.Contains(expErr.Chain, "cycle"):
					msg = "cycle detected"
				}
				_ = p.auditor.LogError(ActionExpand, expErr.Key, msg)
			}
			return nil, err
		}
		return result, nil
	}

	// Fallback: expand manually using the interface method
	return p.expandAllUsingInterface(vars, noExpand)
}

// expandAllUsingInterface expands all variables using the VariableExpander interface.
// This is used when the expander is not the built-in *Expander type.
func (p *LineParser) expandAllUsingInterface(vars map[string]string, noExpand map[string]struct{}) (map[string]string, error) {
	// Fast path: check if any values need expansion
	needsExpansion := false
	for _, value := range vars {
		if strings.IndexByte(value, '$') != -1 {
			needsExpansion = true
			break
		}
	}

	// No expansion needed - return original map without copying
	if !needsExpansion {
		return vars, nil
	}

	// Expand all values using the interface method
	result := make(map[string]string, len(vars))
	for key, value := range vars {
		// Single-quoted values are literal for custom expanders too.
		if _, literal := noExpand[key]; literal {
			result[key] = value
			continue
		}
		expanded, err := p.expander.Expand(value)
		if err != nil {
			_ = p.auditor.LogError(ActionExpand, key, err.Error())
			return nil, err
		}
		result[key] = expanded
	}

	return result, nil
}

// keysToUpperImpl is the shared implementation for converting map keys to uppercase.
// The result map is provided by the caller to allow both pooled and non-pooled variants.
func keysToUpperImpl(m map[string]string, result map[string]bool) {
	for k := range m {
		if k == "" {
			continue
		}
		result[ToUpperASCII(k)] = true
	}
}

// KeysToUpperPooled converts map keys to uppercase using a pooled map.
// The returned map MUST be returned to the pool using PutKeysToUpperMap after use.
// This reduces allocations when the map is only needed temporarily.
//
// Example:
//
//	upperKeys := KeysToUpperPooled(vars)
//	defer PutKeysToUpperMap(upperKeys)
//	// use upperKeys...
func KeysToUpperPooled(m map[string]string) map[string]bool {
	result := getKeysToUpperMap()
	keysToUpperImpl(m, result)
	return result
}

// PutKeysToUpperMap returns a pooled map obtained from KeysToUpperPooled.
// It is safe to call with nil.
func PutKeysToUpperMap(m map[string]bool) {
	putKeysToUpperMap(m)
}
