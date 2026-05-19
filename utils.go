package env

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Type Constraints
// ============================================================================

// defaultUnmarshalDepth is the default nesting depth used by UnmarshalMap
// when no Config is available. Matches the DefaultConfig depth of 10.
const defaultUnmarshalDepth = 10

// sliceElement is a type constraint for supported slice element types.
// This constraint is used by GetSlice and GetSliceFrom functions to ensure
// type-safe parsing of slice values from environment variables.
type sliceElement interface {
	string | int | int64 | uint | uint64 | bool | float64 | time.Duration
}

// ============================================================================
// Generic Helpers
// ============================================================================

// firstOrZero returns the first element of a variadic slice, or the zero value
// if the slice is empty. This is a generic helper for default value handling.
func firstOrZero[T any](values ...T) T {
	if len(values) > 0 {
		return values[0]
	}
	var zero T
	return zero
}

// ============================================================================
// Internal Parse Utilities
// ============================================================================

// parseBool parses a boolean string with common variations.
// Uses byte-level case-insensitive comparison to avoid allocations from
// strings.ToLower and strings.TrimSpace.
func parseBool(s string) (bool, error) {
	s = internal.TrimSpace(s)
	if len(s) == 0 {
		return false, nil
	}
	if v, ok := lookupBoolASCII(s); ok {
		return v, nil
	}
	return false, &ValidationError{
		Field:   "bool",
		Value:   MaskSensitiveInString(s),
		Message: "invalid boolean value",
	}
}

// lookupBoolASCII performs case-insensitive boolean lookup without allocating.
// Handles common boolean representations: true/false, yes/no, on/off, 1/0, enabled/disabled.
func lookupBoolASCII(s string) (bool, bool) {
	n := len(s)
	switch n {
	case 1:
		if s[0] == '1' || s[0] == '0' {
			return s[0] == '1', true
		}
	case 2:
		if equalFoldASCII(s, "no") {
			return false, true
		}
		if equalFoldASCII(s, "on") {
			return true, true
		}
	case 3:
		if equalFoldASCII(s, "yes") {
			return true, true
		}
		if equalFoldASCII(s, "off") {
			return false, true
		}
	case 4:
		if equalFoldASCII(s, "true") {
			return true, true
		}
	case 5:
		if equalFoldASCII(s, "false") {
			return false, true
		}
	case 7:
		if equalFoldASCII(s, "enabled") {
			return true, true
		}
	case 8:
		if equalFoldASCII(s, "disabled") {
			return false, true
		}
	}
	return false, false
}

// equalFoldASCII compares two strings case-insensitively for ASCII characters only.
// This is faster than strings.EqualFold for the common case of short ASCII strings.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca := a[i]
		cb := b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// parseDuration parses a duration string with additional validation.
func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(internal.TrimSpace(s))
	if err != nil {
		return 0, &ValidationError{
			Field:   "duration",
			Value:   MaskSensitiveInString(s),
			Message: "invalid duration format",
		}
	}
	return d, nil
}

// parseInt parses an integer string with bounds checking.
func parseInt(s string, bits int) (int64, error) {
	n, err := strconv.ParseInt(internal.TrimSpace(s), 10, bits)
	if err != nil {
		return 0, &ValidationError{
			Field:   "int",
			Value:   MaskSensitiveInString(s),
			Message: "invalid integer value",
		}
	}
	return n, nil
}

// parseUint parses an unsigned integer string with bounds checking.
func parseUint(s string, bits int) (uint64, error) {
	n, err := strconv.ParseUint(internal.TrimSpace(s), 10, bits)
	if err != nil {
		return 0, &ValidationError{
			Field:   "uint",
			Value:   MaskSensitiveInString(s),
			Message: "invalid unsigned integer value",
		}
	}
	return n, nil
}

// parseFloat64 parses a floating-point string with bounds checking.
func parseFloat64(s string) (float64, error) {
	n, err := strconv.ParseFloat(internal.TrimSpace(s), 64)
	if err != nil {
		return 0, &ValidationError{
			Field:   "float",
			Value:   MaskSensitiveInString(s),
			Message: "invalid float value",
		}
	}
	return n, nil
}

// ============================================================================
// Marshal/Unmarshal Utilities
// ============================================================================

// Marshal converts data to the specified format with sorted keys (default: .env format).
// The input can be a map[string]string or a struct (will be converted to map first).
// The format parameter is optional; if not provided, defaults to .env format.
// Supported formats: FormatEnv, FormatJSON, FormatYAML.
//
// Keys are always sorted for consistent output.
//
// Example:
//
//	// Map to .env format (sorted)
//	envString, _ := env.Marshal(mapData)
//
//	// Struct to .env format (sorted)
//	envString, _ := env.Marshal(config)
//
//	// Map to JSON format (sorted)
//	jsonString, _ := env.Marshal(mapData, env.FormatJSON)
//
//	// Struct to YAML format (sorted)
//	yamlString, _ := env.Marshal(config, env.FormatYAML)
func Marshal(data any, format ...FileFormat) (string, error) {
	f := FormatEnv
	if len(format) > 0 {
		f = format[0]
	}

	// Convert input to map if needed
	m, err := toMap(data)
	if err != nil {
		return "", err
	}

	// Always use sorted output for consistency
	return internal.MarshalEnvAs(m, toInternalFormat(f), true)
}

// toMap converts input data to map[string]string.
// Supports map[string]string and struct types.
func toMap(data any) (map[string]string, error) {
	if data == nil {
		return nil, &ValidationError{
			Field:   "data",
			Message: "data cannot be nil",
		}
	}

	// Check if it's already a map
	if m, ok := data.(map[string]string); ok {
		return m, nil
	}

	// Check if it's a pointer to map
	if pm, ok := data.(*map[string]string); ok {
		if pm == nil {
			return nil, &ValidationError{
				Field:   "data",
				Message: "map pointer cannot be nil",
			}
		}
		return *pm, nil
	}

	// Try to convert struct to map
	return MarshalStruct(data)
}

// toInternalFormat converts public FileFormat to internal MarshalFormat.
func toInternalFormat(f FileFormat) internal.MarshalFormat {
	switch f {
	case FormatJSON:
		return internal.FormatJSON
	case FormatYAML:
		return internal.FormatYAML
	default:
		return internal.FormatEnv
	}
}

// detectDataFormat auto-detects the format of input data.
// Returns FormatJSON for JSON, FormatYAML for YAML, FormatEnv otherwise.
// Uses IndexByte line scanning to avoid allocating a full string slice.
func detectDataFormat(data string) FileFormat {
	data = strings.TrimSpace(data)

	// Empty data defaults to .env
	if len(data) == 0 {
		return FormatEnv
	}

	// JSON: starts with { or [
	if data[0] == '{' || data[0] == '[' {
		return FormatJSON
	}

	// Scan lines without allocating a slice
	start := 0
	for start < len(data) {
		end := strings.IndexByte(data[start:], '\n')
		var line string
		if end == -1 {
			line = data[start:]
			start = len(data)
		} else {
			line = data[start : start+end]
			start += end + 1
		}

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// YAML list item: starts with "- "
		if strings.HasPrefix(line, "- ") {
			return FormatYAML
		}

		// YAML pattern: key: value (colon+space takes precedence over equals)
		// Correctly handles YAML values containing "=" like: connection: host=db port=5432
		if strings.Contains(line, ": ") {
			return FormatYAML
		}

		// .env pattern: contains = (only when no ": " pattern found)
		if strings.Contains(line, "=") {
			return FormatEnv
		}

		break
	}

	return FormatEnv
}

// UnmarshalMap parses a formatted string into a map[string]string.
// The format parameter is optional and defaults to FormatEnv.
// Use FormatAuto for automatic format detection.
//
// Nested structures (JSON/YAML) are flattened with underscore delimiter.
//
// Example:
//
//	// .env format (default)
//	m, _ := env.UnmarshalMap("KEY=value")
//
//	// JSON format
//	m, _ := env.UnmarshalMap(`{"server": {"host": "localhost"}}`, env.FormatJSON)
//
//	// Auto-detect format
//	m, _ := env.UnmarshalMap(jsonString, env.FormatAuto)
func UnmarshalMap(data string, format ...FileFormat) (map[string]string, error) {
	f := FormatEnv
	if len(format) > 0 {
		f = format[0]
	}

	// Auto-detect if requested
	if f == FormatAuto {
		f = detectDataFormat(data)
	}

	switch f {
	case FormatJSON:
		return unmarshalJSON(data)
	case FormatYAML:
		return unmarshalYAML(data)
	default:
		return parseString(data)
	}
}

// unmarshalJSON parses a JSON string into a map.
func unmarshalJSON(data string) (map[string]string, error) {
	if data == "" {
		return make(map[string]string), nil
	}

	cfg := internal.JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         defaultUnmarshalDepth,
	}

	result, err := internal.FlattenJSON([]byte(data), cfg)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// unmarshalYAML parses a YAML string into a map.
func unmarshalYAML(data string) (map[string]string, error) {
	if data == "" {
		return make(map[string]string), nil
	}

	cfg := internal.YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         defaultUnmarshalDepth,
	}

	value, err := internal.ParseYAML([]byte(data), cfg.MaxDepth)
	if err != nil {
		return nil, err
	}

	result, err := internal.FlattenYAML(value, cfg)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// IsMarshalError checks if an error is a marshaling error.
func IsMarshalError(err error) bool {
	var me *MarshalError
	return errors.As(err, &me)
}

// ============================================================================
// Struct Marshaling/Unmarshaling
// ============================================================================

// Marshaler is the interface for types that can marshal themselves to env format.
type Marshaler interface {
	MarshalEnv() ([]byte, error)
}

// Unmarshaler is the interface for types that can unmarshal themselves from env values.
type Unmarshaler interface {
	UnmarshalEnv(map[string]string) error
}

// MarshalStruct converts a struct to environment variables.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Nested structs are flattened with underscore-separated keys.
func MarshalStruct(v any) (map[string]string, error) {
	// Check for Marshaler interface
	if m, ok := v.(Marshaler); ok {
		data, err := m.MarshalEnv()
		if err != nil {
			return nil, err
		}
		// Parse the marshaled data
		return UnmarshalMap(string(data))
	}

	return internal.Struct(v, "")
}

// UnmarshalStruct parses a formatted string and populates the struct.
// The format parameter is optional and defaults to FormatEnv.
// Use FormatAuto for automatic format detection.
//
// Struct fields should use `env:"KEY"` tags for mapping.
//
// Example:
//
//	type Config struct {
//	    Host string `env:"SERVER_HOST"`
//	    Port int    `env:"SERVER_PORT"`
//	}
//
//	// .env format (default)
//	var cfg Config
//	_ = env.UnmarshalStruct("SERVER_HOST=localhost\nSERVER_PORT=8080", &cfg)
//
//	// JSON format
//	_ = env.UnmarshalStruct(`{"server": {"host": "localhost"}}`, &cfg, env.FormatJSON)
func UnmarshalStruct(data string, v any, format ...FileFormat) error {
	m, err := UnmarshalMap(data, format...)
	if err != nil {
		return err
	}
	return UnmarshalInto(m, v)
}

// UnmarshalInto populates a struct from a map[string]string.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Optional `envDefault:"value"` sets a default if the key is not found.
func UnmarshalInto(data map[string]string, v any) error {
	if v == nil {
		return &ValidationError{
			Field:   "value",
			Message: "cannot unmarshal into nil",
		}
	}

	// Check for Unmarshaler interface
	if u, ok := v.(Unmarshaler); ok {
		return u.UnmarshalEnv(data)
	}

	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return &ValidationError{
			Field:   "value",
			Message: "expected non-nil pointer",
		}
	}

	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return &ValidationError{
			Field:   "value",
			Message: "expected pointer to struct",
		}
	}

	return internal.StructInto(data, val, "")
}

// ============================================================================
// Slice Parsing Utilities
// ============================================================================

// parseSliceElement parses a string value into the target type T.
func parseSliceElement[T sliceElement](value string) (T, error) {
	var zero T

	// Trim whitespace for consistent behavior across all types
	trimmed := internal.TrimSpace(value)

	switch any(zero).(type) {
	case string:
		return any(trimmed).(T), nil
	case int:
		n, e := strconv.Atoi(trimmed)
		return any(n).(T), e
	case int64:
		n, e := strconv.ParseInt(trimmed, 10, 64)
		return any(n).(T), e
	case uint:
		n, e := strconv.ParseUint(trimmed, 10, 64)
		return any(uint(n)).(T), e
	case uint64:
		n, e := strconv.ParseUint(trimmed, 10, 64)
		return any(n).(T), e
	case bool:
		b, e := parseBool(trimmed)
		return any(b).(T), e
	case float64:
		n, e := strconv.ParseFloat(trimmed, 64)
		return any(n).(T), e
	case time.Duration:
		d, e := parseDuration(trimmed)
		return any(d).(T), e
	default:
		var zero T
		return zero, fmt.Errorf("parseSliceElement: unsupported type %T", zero)
	}
}

// parseCommaSeparated parses a comma-separated string into a slice of T.
func parseCommaSeparated[T sliceElement](value string, defaultValue ...[]T) []T {
	if value == "" {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]T, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		parsed, err := parseSliceElement[T](part)
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0]
			}
			return nil
		}
		result = append(result, parsed)
	}

	return result
}
