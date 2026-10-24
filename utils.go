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
	if v, ok := internal.ParseBoolLoose(s); ok {
		return v, nil
	}
	return false, &ValidationError{
		Field:   "bool",
		Value:   MaskSensitiveInString(s),
		Message: "invalid boolean value",
	}
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
// Round-trip caveats:
//   - '$' is not escaped. A value containing "$VAR" or "${VAR}" expands when
//     the output is re-parsed with ExpandVariables enabled (the default).
//     Disable expansion on the reading side, or avoid Marshal for such values.
//   - When marshaling a struct, fields whose value marshals to an empty
//     string (empty strings, nil pointers, empty slices) are omitted;
//     scalar zero values such as 0 and false are kept (see MarshalStruct).
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

		// First-occurrence comparison of the two separator shapes: whichever
		// appears EARLIER opens the assignment/mapping, so
		// "GREETING=hello: world" classifies as .env (the '=' at index 8
		// opens the assignment; the ": " inside the value is data) while
		// "connection: host=db" classifies as YAML (the ": " opens the
		// mapping; the '=' is inside the value).
		colon := strings.Index(line, ": ")
		eq := strings.IndexByte(line, '=')
		switch {
		case colon != -1 && (eq == -1 || colon < eq):
			return FormatYAML
		case eq != -1:
			return FormatEnv
		}

		// A bare "key:" line with an empty value — the canonical start of a
		// YAML block — contains neither separator; classify it as YAML
		// instead of falling through to .env.
		if isYAMLBareKeyLine(line) {
			return FormatYAML
		}

		break
	}

	return FormatEnv
}

// isYAMLBareKeyLine reports whether line is exactly a mapping key with an
// empty value: [\w@\-.]+ ':' optional trailing whitespace.
func isYAMLBareKeyLine(line string) bool {
	i := 0
	for i < len(line) && isBareKeyByte(line[i]) {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != ':' {
		return false
	}
	for j := i + 1; j < len(line); j++ {
		if line[j] != ' ' && line[j] != '\t' {
			return false
		}
	}
	return true
}

func isBareKeyByte(c byte) bool {
	return c == '_' || c == '@' || c == '-' || c == '.' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// UnmarshalMap parses a formatted string into a map[string]string.
// The format parameter is optional and defaults to FormatEnv.
// Use FormatAuto for automatic format detection.
//
// Nested structures (JSON/YAML) are flattened with underscore delimiter.
// Duplicate keys resolve per the DuplicateKeyInherit policy, and flat-key
// collisions always error — see DuplicateKeyPolicy for details.
//
// SECURITY (SEC-002): every format now runs through the same DefaultConfig
// pipeline as a single-file load, so the built-in forbidden-key policy
// (PATH, LD_PRELOAD, NODE_OPTIONS, ...) and key validation apply uniformly.
// This function takes no Config; use UnmarshalMapWithConfig to select a
// DuplicateKeyPolicy, limits, validation, and expansion settings.
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

	return UnmarshalMapWithConfig(DefaultConfig(), data, f)
}

// UnmarshalMapWithConfig parses a formatted string into a map[string]string
// with the same semantics a Loader built from the same Config applies to a
// single loaded file: LimitsConfig (file size, line/key/value length,
// variable count), ParsingConfig (duplicate-key policy; variable expansion,
// which applies to .env input only — JSON and YAML values are never
// expanded), JSONConfig / YAMLConfig format options, ValidationConfig (key
// and value rules, RequiredKeys), and ComponentConfig (custom validator /
// expander / auditor; Prefix filters the parsed keys, with the same rule as
// file loading) all take effect. The format parameter is optional and
// defaults to FormatEnv; FormatAuto is detected from the data, like
// UnmarshalMap.
//
// cfg follows the standard zero-value contract: a fully zero Config means
// DefaultConfig(); a partially-set Config is rejected with ErrInvalidConfig
// — always start from DefaultConfig().
//
// FileConfig (Filenames, FailOnMissingFile, OverwriteExisting, AutoApply)
// has no one-shot counterpart and is ignored.
//
// UnmarshalMap is the no-config shortcut that runs this same DefaultConfig
// pipeline for every format (SEC-002). Prefer this function when a custom
// DuplicateKeyPolicy, limits, validation, or expansion settings are needed.
//
// Example:
//
//	cfg := env.DefaultConfig()
//	cfg.ParsingConfig.DuplicateKeyPolicy = env.DuplicateKeyError
//	m, err := env.UnmarshalMapWithConfig(cfg, yamlData, env.FormatYAML)
func UnmarshalMapWithConfig(cfg Config, data string, format ...FileFormat) (map[string]string, error) {
	f := FormatEnv
	if len(format) > 0 {
		f = format[0]
	}
	if f == FormatAuto {
		f = detectDataFormat(data)
	}

	if cfg.IsZero() {
		cfg = DefaultConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	// Build components and parsers through the same path a Loader uses
	// (New → buildComponentFactoryWithFS → createParsers), so parsing
	// behavior is identical to loading one file through a Loader built
	// from cfg.
	factory := cfg.buildComponentFactoryWithFS(cfg.FileSystem)
	defer func() { _ = factory.Close() }() // best-effort cleanup of the audit handler

	parsers, err := createParsers(cfg, factory)
	if err != nil {
		return nil, err
	}
	// One-shot counterpart of Loader.Close's parser cleanup (closeParsers in
	// env.go): a custom parser registered via RegisterParser may hold
	// resources, and this path previously dropped them on both the success
	// and the error path.
	defer closeParsers(parsers)

	p, ok := parsers[f]
	if !ok || p == nil {
		// Mirror LoadFiles: unknown formats fall back to the .env parser.
		p = parsers[FormatEnv]
	}
	if p == nil {
		return nil, fmt.Errorf("no parser available for format %v", f)
	}

	vars, err := p.Parse(strings.NewReader(data), "")
	if err != nil {
		return nil, err
	}

	// Apply the prefix filter with the same rule as file loading
	// (loadFileLocked): uppercase prefix, case-insensitive match.
	if cfg.Prefix != "" {
		upperPrefix := internal.ToUpperASCII(cfg.Prefix)
		filtered := make(map[string]string, len(vars))
		for key, value := range vars {
			if internal.HasUpperPrefix(key, upperPrefix) {
				filtered[key] = value
			}
		}
		vars = filtered
	}

	return vars, nil
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
//
// Fields whose value marshals to an empty string — empty strings, nil
// pointers, and empty slices — are omitted from the output. Scalar zero
// values are kept ("0", "false"), since they do not marshal to "".
//
// Struct-kind fields implementing encoding.TextMarshaler (e.g. time.Time)
// serialize via MarshalText as scalar leaves instead of field recursion.
//
// Cyclic values (a pointer field reaching back into the value graph) are
// rejected with a *MarshalError: nesting depth is capped instead of
// recursing without bound.
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

// UnmarshalStructWithConfig parses a formatted string and populates the
// struct under cfg — the config-driven counterpart of UnmarshalStruct.
// Every parsing rule of UnmarshalMapWithConfig applies (limits,
// duplicate-key policy, expansion, validation, custom components); see its
// documentation for the fields that are honored and ignored. The format
// parameter is optional and defaults to FormatEnv; FormatAuto is detected
// from the data.
//
// Example:
//
//	cfg := env.DefaultConfig()
//	cfg.ParsingConfig.DuplicateKeyPolicy = env.DuplicateKeyError
//	var out Config
//	if err := env.UnmarshalStructWithConfig(cfg, yamlData, &out, env.FormatYAML); err != nil {
//	    log.Fatal(err)
//	}
func UnmarshalStructWithConfig(cfg Config, data string, v any, format ...FileFormat) error {
	m, err := UnmarshalMapWithConfig(cfg, data, format...)
	if err != nil {
		return err
	}
	return UnmarshalInto(m, v)
}

// UnmarshalInto populates a struct from a map[string]string.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Optional `envDefault:"value"` sets a default if the key is not found.
//
// Slice fields are populated by splitting the value on commas. For STRING
// slices, empty elements are preserved as empty entries ("a,,b" yields three
// elements); for numeric and boolean slices an empty element is a conversion
// error (there is no "zero-valued" spelling of "" for strconv). This differs
// from GetSlice, which skips empty elements when parsing comma-separated
// fallback values.
//
// Struct-kind fields implementing encoding.TextUnmarshaler (e.g. time.Time,
// matched through the pointer method set) are populated via UnmarshalText;
// invalid text is a *MarshalError.
//
// Cyclic structures are rejected with a *MarshalError: nesting depth is
// capped, so a self-referential struct type returns an error instead of
// recursing (and allocating) without bound.
func UnmarshalInto(data map[string]string, v any) error {
	if v == nil {
		return &ValidationError{
			Field:   "value",
			Message: "cannot unmarshal into nil",
		}
	}

	// Check for Unmarshaler interface
	if u, ok := v.(Unmarshaler); ok {
		return safeUnmarshalEnv(u, data)
	}

	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Pointer || val.IsNil() {
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
// All parse failures are reported as *ValidationError for consistent error
// handling by callers (the previous version returned a mix of raw strconv
// errors and ValidationError depending on the type).
//
// Strings are returned UNTRIMMED: whether whitespace is significant is a
// quoting decision only the caller can make (a quoted comma-separated
// element preserves it, an unquoted one does not), and numeric parsing
// below trims internally as before.
func parseSliceElement[T sliceElement](value string) (T, error) {
	var zero T

	trimmed := internal.TrimSpace(value)

	switch any(zero).(type) {
	case string:
		return any(value).(T), nil
	case int:
		n, e := strconv.Atoi(trimmed)
		if e != nil {
			return zero, &ValidationError{Field: "int", Value: MaskSensitiveInString(value), Message: "invalid integer value"}
		}
		return any(n).(T), nil
	case int64:
		n, e := strconv.ParseInt(trimmed, 10, 64)
		if e != nil {
			return zero, &ValidationError{Field: "int64", Value: MaskSensitiveInString(value), Message: "invalid integer value"}
		}
		return any(n).(T), nil
	case uint:
		// strconv.IntSize (not a fixed 64) so 32-bit platforms reject values
		// that would silently truncate when converted to uint.
		n, e := strconv.ParseUint(trimmed, 10, strconv.IntSize)
		if e != nil {
			return zero, &ValidationError{Field: "uint", Value: MaskSensitiveInString(value), Message: "invalid unsigned integer value"}
		}
		return any(uint(n)).(T), nil
	case uint64:
		n, e := strconv.ParseUint(trimmed, 10, 64)
		if e != nil {
			return zero, &ValidationError{Field: "uint64", Value: MaskSensitiveInString(value), Message: "invalid unsigned integer value"}
		}
		return any(n).(T), nil
	case bool:
		b, e := parseBool(trimmed)
		if e != nil {
			return zero, e
		}
		return any(b).(T), nil
	case float64:
		n, e := strconv.ParseFloat(trimmed, 64)
		if e != nil {
			return zero, &ValidationError{Field: "float", Value: MaskSensitiveInString(value), Message: "invalid float value"}
		}
		return any(n).(T), nil
	case time.Duration:
		d, e := parseDuration(trimmed)
		if e != nil {
			return zero, e
		}
		return any(d).(T), nil
	default:
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

	parts, quoted := internal.SplitQuotedCommaParts(value)
	result := make([]T, 0, len(parts))

	for i, part := range parts {
		// Trim only unquoted elements, matching the struct-mapping path
		// (setSliceValue): grouping quotes exist to preserve significant
		// leading/trailing whitespace, so a blanket trim here made GetSlice
		// disagree with UnmarshalStruct on the same Marshal output.
		if !quoted[i] {
			part = strings.TrimSpace(part)
		}
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

// ============================================================================
// Process-wide Utilities
// ============================================================================

// ClearInternCache releases the memory held by the .env key-interning cache.
// Parsing .env content interns key strings in a bounded process-wide cache
// (see internal.InternKeyBytes); long-running applications that parse in
// bursts can call this between bursts to drop the retained keys.
//
// Like the other process-wide knobs (see the package documentation), it is
// deliberately not part of Config. Safe for concurrent use.
func ClearInternCache() {
	internal.ClearInternCache()
}
