// Package internal provides file I/O utilities.
package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// WriteFile writes content to a file using an atomic write pattern.
// It writes to a temp file first, then renames for atomicity.
func WriteFile(filename string, buf *bytes.Buffer) (err error) {
	// Create parent directory if needed
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return &FileError{Path: filename, Op: "mkdir", Err: err}
		}
	}

	// Write to temp file first for atomic operation
	tempFile := filename + ".tmp"

	// Create with restricted permissions (0600 for sensitive env files)
	file, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return &FileError{Path: filename, Op: "create", Err: err}
	}

	// Track whether file is still open for cleanup purposes
	fileClosed := false

	// Use named return parameter to ensure cleanup on error
	defer func() {
		if err != nil {
			// Best effort cleanup on error - only close if still open
			if !fileClosed {
				_ = file.Close() /* best-effort cleanup; error not actionable */
			}
			_ = os.Remove(tempFile) /* best-effort cleanup; error not actionable */
		}
	}()

	// Write content
	if _, err = buf.WriteTo(file); err != nil {
		return &FileError{Path: filename, Op: "write", Err: err}
	}

	// Ensure data is flushed to disk
	if err = file.Sync(); err != nil {
		return &FileError{Path: filename, Op: "sync", Err: err}
	}

	// Close before rename (required on Windows)
	if closeErr := file.Close(); closeErr != nil {
		err = closeErr
		return &FileError{Path: filename, Op: "close", Err: closeErr}
	}
	fileClosed = true

	// Atomic rename
	if err = os.Rename(tempFile, filename); err != nil {
		_ = os.Remove(tempFile) /* best-effort cleanup; error not actionable */
		return &FileError{Path: filename, Op: "rename", Err: err}
	}

	return nil
}

// MarshalEnv converts a map to .env file format.
func MarshalEnv(m map[string]string, sorted bool) (string, error) {
	keys := make([]string, 0, len(m))

	// Calculate exact size needed to avoid grow calls
	totalSize := 0
	for k, v := range m {
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
		default:
			buf.WriteByte(c)
		}
	}

	buf.WriteByte('"')
}

// EscapeValue escapes a value for .env file format.
// This function uses pooled builder for efficiency.
func EscapeValue(value string) string {
	buf := GetBuilder()
	defer PutBuilder(buf)
	buf.Grow(len(value) + 10)
	escapeValueToBuilder(buf, value)
	return buf.String()
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

// setNestedValue sets a value in a nested map structure based on underscore-separated keys.
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
			// If it's not a map, we can't nest further, use full key
			m[key] = inferJSONType(value)
			return
		}
	}

	lastKey := parts[len(parts)-1]
	current[lastKey] = inferJSONType(value)
}

// inferJSONType attempts to infer the appropriate JSON type from a string value.
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

	// Null
	if value == "null" {
		return nil
	}

	// Integer
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return i
	}

	// Float
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
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

	// Also quote if it starts with special characters
	if len(value) > 0 && (value[0] == ' ' || value[0] == '\t' || value[0] == '-' || value[0] == '*') {
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
