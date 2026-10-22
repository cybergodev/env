// Provides file I/O utilities.

package internal

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// MarshalEnv converts a map to .env file format.
func MarshalEnv(m map[string]string, sorted bool) (string, error) {
	keys := make([]string, 0, len(m))

	// Calculate exact size needed to avoid grow calls
	totalSize := 0
	for k, v := range m {
		// SECURITY (SEC-06): reject keys that cannot survive a re-parse
		// before emitting anything.
		if err := validateEnvKeyForMarshal(k); err != nil {
			return "", err
		}
		keys = append(keys, k)
		// key + '=' + escaped_value + '\n'
		// Estimate escape overhead: special chars might double in size
		estimatedValueLen := len(v)
		if needsEscapeEstimate(v) {
			estimatedValueLen = estimatedValueLen*2 + 2 // +2 for quotes
		}
		totalSize += len(k) + 1 + estimatedValueLen + 1 // key + = + value + \n
	}

	if sorted {
		sort.Strings(keys)
	}

	buf := GetBuilder()
	defer PutBuilder(buf)
	buf.Grow(totalSize)

	for _, key := range keys {
		value := m[key]

		// Write directly to buffer to avoid intermediate allocations
		buf.WriteString(key)
		buf.WriteByte('=')
		escapeValueToBuilder(buf, value)
		buf.WriteByte('\n')
	}

	return buf.String(), nil
}

// needsEscapeEstimate quickly checks if a value likely needs escaping.
// This is used for capacity estimation only.
func needsEscapeEstimate(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '"' || c == '\'' || c == '#' || c == '\\' {
			return true
		}
	}
	return false
}

// coercesOnYamlReload reports whether the bare (unquoted) scalar text would
// not survive a reload under AllowYamlSyntax: TryParseYamlValueBytes collapses
// "null" and "~" to the EMPTY STRING (everything else — true/false and
// number-like text included — passes through unchanged), so exactly those two
// spellings must be quoted. Mirrors the intent of escapeYAMLValue's guard on
// the YAML writer side, narrowed to the .env reload path's real losses.
func coercesOnYamlReload(value string) bool {
	return value == "~" || EqualFoldASCII(value, "null")
}

// escapeValueToBuilder writes an escaped value directly to a strings.Builder.
// This avoids intermediate string allocations by combining the quoting check
// and escape counting into a single pass.
func escapeValueToBuilder(buf *strings.Builder, value string) {
	// Single pass to check for quoting needs and count escapes
	needsQuoting := false
	escapeCount := 0

	for i := 0; i < len(value); i++ {
		c := value[i]
		switch c {
		case ' ', '#', '\'':
			needsQuoting = true
		case '\n', '\r', '\t':
			needsQuoting = true
			escapeCount++
		case '"':
			needsQuoting = true
			escapeCount++
		case '\\':
			// Backslash doesn't trigger quoting, but needs escape if quoting
			escapeCount++
		}
	}

	// Values that TryParseYamlValueBytes collapses to the empty string on a
	// reload ("null" and "~") are quoted — quoting is always re-parseable,
	// mirroring escapeYAMLValue's guard, so a Marshal→Load round trip
	// survives AllowYamlSyntax configurations (DevelopmentConfig ships it
	// enabled).
	if !needsQuoting && coercesOnYamlReload(value) {
		needsQuoting = true
	}

	// SECURITY (SEC-009, CWE-150/74): a value that contains a '$' reference
	// ($VAR or ${VAR}) would be re-expanded by the loader on a Marshal→Load
	// round trip (ExpandVariables defaults on), silently rewriting the value.
	// Such values are quoted so the escaping below can emit the dollar as a
	// literal — the .env double-quoted grammar carries "\$" through to a
	// literal dollar after expansion (see parseDoubleQuotedBytes).
	if strings.IndexByte(value, '$') != -1 {
		needsQuoting = true
	}

	if !needsQuoting {
		if value == "" {
			buf.WriteString(`""`)
		} else {
			buf.WriteString(value)
		}
		return
	}

	// Pre-allocate space
	buf.Grow(len(value) + escapeCount + 2)

	buf.WriteByte('"')

	// Use byte-level iteration for ASCII performance
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch c {
		case '\n':
			buf.WriteString("\\n")
		case '\r':
			buf.WriteString("\\r")
		case '\t':
			buf.WriteString("\\t")
		case '"':
			buf.WriteString("\\\"")
		case '\\':
			buf.WriteString("\\\\")
		// SECURITY (SEC-009): an escaped dollar ("\$") is preserved by the
		// double-quoted parser as "$$" for the later expansion pass, which
		// folds it back to one literal dollar — the exact round-trip the
		// quoting above exists to guarantee.
		case '$':
			buf.WriteString(`\$`)
		default:
			buf.WriteByte(c)
		}
	}

	buf.WriteByte('"')
}

// validateEnvKeyForMarshal reports whether key can be represented verbatim
// in .env output. Keys are never escaped (the .env parser does not support
// quoted keys), so a key that would change tokenization on re-parse must
// fail the marshal instead of producing output that re-parses as different
// — potentially attacker-chosen — lines (SEC-06, CWE-74).
func validateEnvKeyForMarshal(key string) error {
	if key == "" {
		return &MarshalError{Field: "key", Message: "key cannot be empty"}
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < 0x20 || c == 0x7F {
			return &MarshalError{Field: "key", Message: "key contains control characters and cannot be represented in .env output"}
		}
		if c == '=' || c == ':' {
			return &MarshalError{Field: "key", Message: "key contains '=' or ':' and cannot be represented in .env output"}
		}
	}
	if key[0] == '#' {
		return &MarshalError{Field: "key", Message: "key starts with '#' and would be parsed as a comment"}
	}
	if strings.HasPrefix(key, "export ") {
		return &MarshalError{Field: "key", Message: `key starts with "export " and would be stripped on re-parse`}
	}
	if key[0] == ' ' || key[len(key)-1] == ' ' {
		return &MarshalError{Field: "key", Message: "key has leading or trailing whitespace, which would be trimmed on re-parse"}
	}
	return nil
}

// validateYAMLKeyForMarshal reports whether key can be represented verbatim
// as an unquoted YAML mapping key. A key is rejected when it contains
// control characters, a ':' that terminates the key token (followed by
// whitespace or end of key), a comment-starting '#' (at the start or after
// whitespace), an array-item prefix ("- "), or leading/trailing whitespace
// that would be trimmed on re-parse (SEC-06, CWE-74).
func validateYAMLKeyForMarshal(key string) error {
	if key == "" {
		return &MarshalError{Field: "key", Message: "key cannot be empty"}
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < 0x20 || c == 0x7F {
			return &MarshalError{Field: "key", Message: "key contains control characters and cannot be represented in YAML output"}
		}
		if c == ':' && (i+1 == len(key) || key[i+1] == ' ' || key[i+1] == '\t') {
			return &MarshalError{Field: "key", Message: "key contains ':' followed by whitespace or end of key and cannot be represented in YAML output"}
		}
		if c == '#' && (i == 0 || key[i-1] == ' ' || key[i-1] == '\t') {
			return &MarshalError{Field: "key", Message: "key contains a comment-starting '#' and cannot be represented in YAML output"}
		}
	}
	if key[0] == '-' && len(key) > 1 && key[1] == ' ' {
		return &MarshalError{Field: "key", Message: `key starts with "- " and would be parsed as an array item`}
	}
	if key[0] == ' ' || key[len(key)-1] == ' ' {
		return &MarshalError{Field: "key", Message: "key has leading or trailing whitespace, which would be trimmed on re-parse"}
	}
	return nil
}

// validateJSONKeyForMarshal reports whether key can round-trip through JSON
// output. JSON can encode any key, but the library's own loader rejects
// flattened keys failing IsValidJSONKey (see the structured parsers), so
// emitting such a key would produce output the same library refuses to
// load — the inconsistency SEC-06 removed from the .env and YAML paths.
func validateJSONKeyForMarshal(key string) error {
	if key == "" {
		return &MarshalError{Field: "key", Message: "key cannot be empty"}
	}
	if !IsValidJSONKey(key) {
		return &MarshalError{Field: "key", Message: "key cannot be represented in JSON output: fails the loader key pattern (letters, digits, '_', '@', '-', '.')"}
	}
	return nil
}

// MarshalFormat represents the output format for marshaling.
type MarshalFormat int

const (
	// FormatEnv outputs in .env file format.
	FormatEnv MarshalFormat = iota
	// FormatJSON outputs in JSON format.
	FormatJSON
	// FormatYAML outputs in YAML format.
	FormatYAML
)

// MarshalEnvAs converts a map to the specified format.
func MarshalEnvAs(m map[string]string, format MarshalFormat, sorted bool) (string, error) {
	switch format {
	case FormatEnv:
		return MarshalEnv(m, sorted)
	case FormatJSON:
		data, err := marshalToJSON(m, sorted)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case FormatYAML:
		return marshalToYAML(m, sorted)
	default:
		return "", &MarshalError{
			Field:   "format",
			Message: fmt.Sprintf("unsupported format: %d", format),
		}
	}
}

// marshalToJSON converts a map to JSON format.
// For simple flat maps, outputs a simple JSON object.
// For nested keys (containing underscores), attempts to create nested structure.
func marshalToJSON(m map[string]string, sorted bool) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}

	// SECURITY (SEC-06): reject keys that cannot survive a re-parse before
	// emitting anything, mirroring the .env and YAML writers.
	for k := range m {
		if err := validateJSONKeyForMarshal(k); err != nil {
			return nil, err
		}
	}

	// Build nested structure from flat map
	result := make(map[string]interface{})

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if sorted {
		sort.Strings(keys)
	}

	for _, key := range keys {
		value := m[key]
		setNestedValue(result, key, value)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, &MarshalError{
			Field:   "json",
			Message: fmt.Sprintf("failed to marshal JSON: %v", err),
		}
	}

	return data, nil
}

// setNestedValue sets a value in a nested map structure based on
// underscore-separated keys. Sorted input (the public Marshal paths always
// sort) processes a scalar prefix key before the longer keys that would nest
// under it, so conflicts resolve to the flat fallback deterministically. For
// unsorted input the function is hardened in BOTH conflict directions: a
// scalar arriving where a nested map already sits (and a longer key nesting
// through an existing scalar) stores the incoming entry under its full flat
// key at the top level instead of overwriting the other entry's data.
func setNestedValue(m map[string]interface{}, key, value string) {
	parts := strings.Split(key, "_")
	current := m

	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if _, exists := current[part]; !exists {
			current[part] = make(map[string]interface{})
		}
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			// A scalar already occupies this path component — keep it and
			// store the incoming entry under its full flat key.
			m[key] = inferJSONType(value)
			return
		}
	}

	lastKey := parts[len(parts)-1]
	if _, isMap := current[lastKey].(map[string]interface{}); isMap {
		// A nested map (from a longer sibling key processed earlier) already
		// sits here — keep its subtree and store the incoming entry under
		// its full flat key at the top level.
		m[key] = inferJSONType(value)
		return
	}
	current[lastKey] = inferJSONType(value)
}

// inferJSONType converts a string to a typed JSON value only when the
// conversion round-trips: the canonical re-rendering must equal the input
// string, so a Marshal→load cycle returns the original value ("7"→7,
// "0.1"→0.1). Non-canonical spellings ("007", "1.50", "1e3", "+5", "-0")
// stay strings, mirroring escapeYAMLValue, which quotes number-like YAML
// scalars for the same reason. "true"/"false" re-render identically and stay
// typed. "null" is NOT converted: JSON null reloads as "" (NullAsEmpty), so
// typing it would silently rewrite the value.
func inferJSONType(value string) interface{} {
	// Empty string
	if value == "" {
		return ""
	}

	// Boolean
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	// Integer — only the canonical spelling
	if i, err := strconv.ParseInt(value, 10, 64); err == nil && strconv.FormatInt(i, 10) == value {
		return i
	}

	// Float — only the canonical spelling, checked against the LOADER's
	// rendering: whole numbers reload via FormatInt (see flattenValue), so
	// "-0" must stay a string even though FormatFloat renders it verbatim
	// (it would reload as "0").
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		if f == float64(int64(f)) {
			if strconv.FormatInt(int64(f), 10) == value {
				return f
			}
		} else if strconv.FormatFloat(f, 'f', -1, 64) == value {
			return f
		}
	}

	// String (default)
	return value
}

// marshalToYAML converts a map to YAML format.
// Outputs a simple YAML document with key-value pairs.
func marshalToYAML(m map[string]string, sorted bool) (string, error) {
	if len(m) == 0 {
		return "", nil
	}

	// SECURITY (SEC-06): reject keys that cannot survive a re-parse before
	// emitting anything.
	for k := range m {
		if err := validateYAMLKeyForMarshal(k); err != nil {
			return "", err
		}
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if sorted {
		sort.Strings(keys)
	}

	buf := GetBuilder()
	defer PutBuilder(buf)

	for _, key := range keys {
		value := m[key]
		buf.WriteString(key)
		buf.WriteString(": ")
		buf.WriteString(escapeYAMLValue(value))
		buf.WriteByte('\n')
	}

	return buf.String(), nil
}

// escapeYAMLValue escapes a value for YAML format.
func escapeYAMLValue(value string) string {
	if value == "" {
		return `""`
	}

	// Check if quoting is needed
	needsQuoting := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == ':' || c == '#' || c == '\n' || c == '\r' || c == '\t' ||
			c == '"' || c == '\'' || c == '[' || c == ']' || c == '{' || c == '}' {
			needsQuoting = true
			break
		}
	}

	// Quote scalars the YAML reader would type-coerce on the way back in
	// (null/~/true/false/numbers), so Marshal→Unmarshal round-trips unchanged.
	if EqualFoldASCII(value, "true") || EqualFoldASCII(value, "false") ||
		value == "null" || value == "~" || looksLikeNumber(value) {
		needsQuoting = true
	}

	// Also quote if it starts or ends with special characters. Leading/trailing
	// whitespace is stripped from plain-style scalars, so quoting is required
	// for the value to survive a round trip.
	last := len(value) - 1
	if value[0] == ' ' || value[0] == '\t' || value[0] == '-' || value[0] == '*' ||
		value[last] == ' ' || value[last] == '\t' {
		needsQuoting = true
	}

	if !needsQuoting {
		return value
	}

	// Escape and quote using pooled builder
	buf := GetBuilder()
	defer PutBuilder(buf)
	buf.Grow(len(value) + 10)
	buf.WriteByte('"')
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch c {
		case '\n':
			buf.WriteString("\\n")
		case '\r':
			buf.WriteString("\\r")
		case '\t':
			buf.WriteString("\\t")
		case '"':
			buf.WriteString("\\\"")
		case '\\':
			buf.WriteString("\\\\")
		default:
			buf.WriteByte(c)
		}
	}
	buf.WriteByte('"')
	return buf.String()
}
