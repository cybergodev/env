// Provides internal struct marshaling utilities.

package internal

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// Panic containment for struct-field (un)marshaler callbacks (SEC-003)
// ============================================================================

// The three safe* helpers mirror the root package's component wrappers (see
// recovering.go) and internal.safeHandlerLog: struct-field marshalers are
// user-supplied code reached reflectively, and a panic in them surfaces as an
// error from the public API (ParseInto, UnmarshalInto, Marshal, MarshalStruct)
// instead of crashing the process. The stack is included so the user-code
// fault stays debuggable.

func panicError(component string, r any) error {
	return fmt.Errorf("%s panicked: %v\n%s", component, r, debug.Stack())
}

// safeMarshalText invokes a TextMarshaler with panic containment.
func safeMarshalText(m encoding.TextMarshaler) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			data, err = nil, panicError("MarshalText callback", r)
		}
	}()
	return m.MarshalText()
}

// safeMarshalJSON invokes a json.Marshaler with panic containment.
func safeMarshalJSON(m json.Marshaler) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			data, err = nil, panicError("MarshalJSON callback", r)
		}
	}()
	return m.MarshalJSON()
}

// safeUnmarshalText invokes a TextUnmarshaler with panic containment.
func safeUnmarshalText(u encoding.TextUnmarshaler, text []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("UnmarshalText callback", r)
		}
	}()
	return u.UnmarshalText(text)
}

// Struct converts a struct to environment variables.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Nested structs are flattened with underscore-separated keys.
func Struct(v interface{}, prefix string) (map[string]string, error) {
	return marshalStruct(v, prefix, 0)
}

// maxStructNestingDepth bounds struct-reflection recursion on both the
// marshal and unmarshal sides. Real config structs nest far below this (the
// JSON/YAML flatten default depth is 16); the cap exists solely to turn a
// cyclic value (marshal: a non-nil pointer field reaching back into the
// value graph) or a self-referential type (unmarshal: StructInto allocates
// nil pointer fields before recursing, so recursion never terminates on
// types like `type Node struct{ Next *Node }`) from an unrecoverable
// fatal out-of-memory crash into a *MarshalError.
const maxStructNestingDepth = 100

// errStructDepthExceeded is returned when struct-reflection recursion
// exceeds maxStructNestingDepth. The prefix (the flattened key path
// accumulated at that depth) becomes the MarshalError Field, so a genuinely
// too-deep acyclic struct is debuggable, not just cyclic ones.
func errStructDepthExceeded(prefix string) error {
	field := prefix
	if field == "" {
		field = "value"
	}
	return &MarshalError{
		Field:   field,
		Message: fmt.Sprintf("maximum struct nesting depth (%d) exceeded; possible cyclic reference", maxStructNestingDepth),
	}
}

func marshalStruct(v interface{}, prefix string, depth int) (map[string]string, error) {
	if depth > maxStructNestingDepth {
		return nil, errStructDepthExceeded(prefix)
	}
	if v == nil {
		return nil, nil
	}

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil, nil
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, &MarshalError{
			Field:   "value",
			Message: "expected struct or pointer to struct",
		}
	}

	// Pre-allocate result map based on struct field count as a lower bound estimate
	typ := val.Type()
	result := make(map[string]string, val.NumField())

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// Skip unexported fields
		if !field.CanInterface() {
			continue
		}

		// Get env tag
		tag := fieldType.Tag.Get("env")
		if tag == "-" {
			continue
		}

		// Determine the key. The inline default form `env:"KEY,envDefault:V"`
		// contributes only KEY: structInto parses the same form, and emitting
		// the full tag as the key previously produced unusable entries (and a
		// later MarshalError at .env emission, where ',' fails the key
		// pattern).
		key := tag
		if idx := strings.Index(tag, ",envDefault:"); idx >= 0 {
			key = tag[:idx]
		}
		if key == "" {
			key = ToUpperASCII(fieldType.Name)
		}
		if prefix != "" {
			key = buildPrefixedKey(prefix, key)
		}

		// Handle nested structs — except struct kinds implementing
		// encoding.TextMarshaler (time.Time is the canonical case): those
		// are scalar-like and fall through to valueToString's marshaler
		// branch, which field recursion made unreachable (recursing walked
		// their unexported internals and silently contributed nothing).
		if field.Kind() == reflect.Struct && !implementsTextMarshaler(field) {
			nested, err := marshalStruct(field.Interface(), key, depth+1)
			if err != nil {
				return nil, err
			}
			for k, v := range nested {
				result[k] = v
			}
			continue
		}

		// Handle pointers to structs (same TextMarshaler exemption: a
		// *time.Time passes the probe through its value-receiver methods)
		if field.Kind() == reflect.Pointer && !field.IsNil() {
			if field.Elem().Kind() == reflect.Struct && !implementsTextMarshaler(field) {
				nested, err := marshalStruct(field.Elem().Interface(), key, depth+1)
				if err != nil {
					return nil, err
				}
				for k, v := range nested {
					result[k] = v
				}
				continue
			}
		}

		// Convert value to string
		strVal, err := valueToString(field)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", fieldType.Name, err)
		}

		if strVal != "" {
			result[key] = strVal
		}
	}

	return result, nil
}

// StructInto populates a struct from environment variables.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Optional `envDefault:"value"` sets a default if the key is not found.
// Also supports inline format: `env:"KEY,envDefault:VALUE"`.
//
// Recursion is bounded by maxStructNestingDepth: a self-referential struct
// type returns a *MarshalError instead of recursing (and allocating) forever.
func StructInto(data map[string]string, val reflect.Value, prefix string) error {
	return structInto(data, val, prefix, 0)
}

func structInto(data map[string]string, val reflect.Value, prefix string, depth int) error {
	if depth > maxStructNestingDepth {
		return errStructDepthExceeded(prefix)
	}
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// Skip unexported fields
		if !field.CanSet() {
			continue
		}

		// Get env tag
		tag := fieldType.Tag.Get("env")
		if tag == "-" {
			continue
		}

		// Parse env tag for inline default: "KEY,envDefault:VALUE"
		key := tag
		defaultVal := fieldType.Tag.Get("envDefault")

		if tag != "" && strings.Contains(tag, ",envDefault:") {
			parts := strings.SplitN(tag, ",envDefault:", 2)
			key = parts[0]
			if len(parts) > 1 && defaultVal == "" {
				defaultVal = parts[1]
			}
		}

		// Determine the key
		if key == "" {
			key = ToUpperASCII(fieldType.Name)
		}

		// Handle nested structs — except struct kinds implementing
		// encoding.TextUnmarshaler (time.Time): those are scalar-like and
		// fall through to the leaf path below, whose setFieldValue
		// TextUnmarshaler branch was unreachable for struct kinds (recursion
		// walked unexported internals and silently matched nothing).
		if field.Kind() == reflect.Struct && !implementsTextUnmarshaler(field) {
			// Prefix nested fields with this field's key (tag or uppercased
			// field name), accumulating the outer prefix — mirroring
			// marshalStruct and the JSON/YAML flatteners so an untagged
			// Inner{Host} round-trips through INNER_HOST in both directions.
			// Regression: the prefix was previously passed only when the
			// field had an explicit env tag and did not accumulate, so
			// untagged nested structs looked up HOST and silently dropped the
			// flattened value, and tagged multi-level nesting looked up
			// I_HOST where marshal emits M_I_HOST.
			nestedPrefix := buildPrefixedKey(prefix, key)
			if err := structInto(data, field, nestedPrefix, depth+1); err != nil {
				return err
			}
			continue
		}

		// Handle pointers (same TextUnmarshaler exemption: a *time.Time
		// matches through its pointer method set and is set as a leaf)
		if field.Kind() == reflect.Pointer {
			if field.Type().Elem().Kind() == reflect.Struct && !implementsTextUnmarshaler(field) {
				// Create new struct if needed
				if field.IsNil() {
					field.Set(reflect.New(field.Type().Elem()))
				}
				// Same prefix accumulation as the struct branch above.
				nestedPrefix := buildPrefixedKey(prefix, key)
				if err := structInto(data, field.Elem(), nestedPrefix, depth+1); err != nil {
					return err
				}
				continue
			}
		}

		// Leaf path: non-struct fields, and struct kinds diverted from the
		// recursion branches above by the TextUnmarshaler probes.
		if prefix != "" {
			key = buildPrefixedKey(prefix, key)
		}

		// Gets value from data using case-insensitive and dot-notation resolution
		value, ok := LookupInMap(data, key)
		if !ok {
			if defaultVal != "" {
				value = defaultVal
			} else {
				continue
			}
		}

		// Set the value
		if err := setFieldValue(field, value); err != nil {
			return fmt.Errorf("field %s: %w", fieldType.Name, err)
		}
	}

	return nil
}

// valueToString converts a reflect.Value to a string for env format.
func valueToString(v reflect.Value) (string, error) {
	// Handle nil
	if !v.IsValid() {
		return "", nil
	}

	// A nil pointer cannot serve its marshaler methods: the interface probe
	// succeeds (a nil *time.Time matches TextMarshaler through its
	// value-receiver method set), but calling the method dereferences the
	// nil pointer and panics. Render as empty — marshalStruct omits empty
	// values, matching the documented nil-pointer omission.
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return "", nil
	}

	// Handle interfaces
	if v.CanInterface() {
		// Check for TextMarshaler
		if tm, ok := v.Interface().(encoding.TextMarshaler); ok {
			data, err := safeMarshalText(tm)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}

		// Check for JSON marshaler for complex types
		if jm, ok := v.Interface().(json.Marshaler); ok {
			data, err := safeMarshalJSON(jm)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}

	// Handle based on kind
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Special case for time.Duration
		if v.Type().String() == "time.Duration" {
			return v.Interface().(time.Duration).String(), nil
		}
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		// Format at the field's own precision: a float32 rendered at 64-bit
		// precision carries double-precision noise (float32(0.1) would print
		// as "0.10000000149011612").
		bitSize := 64
		if v.Kind() == reflect.Float32 {
			bitSize = 32
		}
		return strconv.FormatFloat(v.Float(), 'f', -1, bitSize), nil
	case reflect.Slice:
		// Handle []byte — render as comma-separated decimals, the exact form
		// setSliceValue's Uint8 branch parses back. The previous raw-string
		// rendering ("hello" → "hello") made Struct→StructInto on the same
		// []byte field fail with "invalid unsigned integer value".
		if v.Type().Elem().Kind() == reflect.Uint8 {
			raw := v.Bytes()
			if len(raw) == 0 {
				return "", nil
			}
			sb := GetBuilder()
			defer PutBuilder(sb)
			for i, b := range raw {
				if i > 0 {
					sb.WriteByte(',')
				}
				sb.WriteString(strconv.FormatUint(uint64(b), 10))
			}
			return sb.String(), nil
		}
		// Handle string slices - use builder for efficiency
		if v.Type().Elem().Kind() == reflect.String {
			n := v.Len()
			if n == 0 {
				return "", nil
			}
			// Use pooled builder to avoid allocation
			sb := GetBuilder()
			defer PutBuilder(sb)
			for i := 0; i < n; i++ {
				elem := v.Index(i).String()
				if i > 0 {
					sb.WriteByte(',')
				}
				// Quote elements containing the list separator or a quote
				// mark so SplitQuotedComma reassembles them as one element
				// on the way back — unquoted, "a,b" would read back as two
				// elements (silent data corruption). Leading/trailing
				// whitespace also requires quoting: the unmarshal side trims
				// unquoted elements, so "a " would silently round-trip to "a".
				if strings.ContainsAny(elem, ",\"") || elem != strings.TrimSpace(elem) {
					sb.WriteByte('"')
					for j := 0; j < len(elem); j++ {
						if c := elem[j]; c == '"' || c == '\\' {
							sb.WriteByte('\\')
						}
						sb.WriteByte(elem[j])
					}
					sb.WriteByte('"')
				} else {
					sb.WriteString(elem)
				}
			}
			return sb.String(), nil
		}
		// Handle []time.Duration — rendered via Duration.String(), the exact
		// form setSliceValue's duration branch parses back. Previously this
		// fell to "unsupported slice type" while GetSlice[time.Duration]
		// parsed the same value fine (the same layer inconsistency the bool
		// grammar unification removed).
		if v.Type().Elem().String() == "time.Duration" {
			n := v.Len()
			if n == 0 {
				return "", nil
			}
			sb := GetBuilder()
			defer PutBuilder(sb)
			for i := 0; i < n; i++ {
				if i > 0 {
					sb.WriteByte(',')
				}
				sb.WriteString(time.Duration(v.Index(i).Int()).String())
			}
			return sb.String(), nil
		}
		return "", &MarshalError{
			Field:   "slice",
			Message: "unsupported slice type",
		}
	case reflect.Pointer:
		if v.IsNil() {
			return "", nil
		}
		return valueToString(v.Elem())
	default:
		return "", &MarshalError{
			Field:   "value",
			Message: fmt.Sprintf("unsupported type: %s", v.Kind()),
		}
	}
}

// implementsTextMarshaler reports whether v's value implements
// encoding.TextMarshaler. Struct-kind fields implementing it (time.Time is
// the canonical case) are scalar-like: they take valueToString's marshaler
// branch instead of field recursion, which walks their internals and
// silently contributes nothing.
func implementsTextMarshaler(v reflect.Value) bool {
	if !v.CanInterface() {
		return false
	}
	_, ok := v.Interface().(encoding.TextMarshaler)
	return ok
}

// implementsTextUnmarshaler reports whether field can be populated from text
// via encoding.TextUnmarshaler. For value fields the method lives on the
// pointer receiver (time.Time), so the probe uses Addr; for pointer fields
// it is already in the value's own method set.
func implementsTextUnmarshaler(field reflect.Value) bool {
	if field.Kind() == reflect.Pointer {
		if !field.CanInterface() {
			return false
		}
		_, ok := field.Interface().(encoding.TextUnmarshaler)
		return ok
	}
	if !field.CanAddr() || !field.Addr().CanInterface() {
		return false
	}
	_, ok := field.Addr().Interface().(encoding.TextUnmarshaler)
	return ok
}

// convError converts a strconv/time conversion error into a MarshalError
// that does not echo the offending input value. Raw strconv and
// time.ParseDuration errors embed the input in their message (e.g.
// `strconv.ParseInt: parsing "<secret>": invalid syntax`), so returning
// them from ParseInto/UnmarshalInto leaks the value into logs and error
// reports whenever the caller prints the error (SEC-01, CWE-209).
// The failure reason is preserved; the value is not.
func convError(kind string, err error) error {
	msg := "invalid " + kind + " value"
	var ne *strconv.NumError
	if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
		msg += ": value out of range"
	}
	return &MarshalError{Field: kind, Message: msg}
}

// setFieldValue sets a struct field from a string value.
func setFieldValue(field reflect.Value, value string) error {
	if value == "" && field.Kind() != reflect.String {
		return nil // Don't set empty values for non-string fields
	}

	// Handle pointer types
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setFieldValue(field.Elem(), value)
	}

	// Check for TextUnmarshaler
	if field.CanAddr() {
		if tu, ok := field.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return safeUnmarshalText(tu, []byte(value))
		}
	}

	// Handle based on kind
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		// Library-wide boolean grammar (yes/no/on/off/...), the same one
		// GetBool accepts — the strict strconv grammar here previously made
		// DEBUG=yes an error through ParseInto while GetBool parsed it fine.
		b, ok := ParseBoolLoose(value)
		if !ok {
			return convError("boolean", strconv.ErrSyntax)
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Special case for time.Duration
		if field.Type().String() == "time.Duration" {
			d, err := time.ParseDuration(value)
			if err != nil {
				return convError("duration", err)
			}
			field.SetInt(int64(d))
			return nil
		}
		// Parse with the field's actual bit width: reflect's SetInt converts
		// like a C cast, so parsing at 64 bits would silently truncate
		// out-of-range values (300 → 44 in an int8 field) instead of failing.
		i, err := strconv.ParseInt(value, 10, field.Type().Bits())
		if err != nil {
			return convError("integer", err)
		}
		field.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// Same bit-width rule as the signed case (SetUint silently wraps).
		u, err := strconv.ParseUint(value, 10, field.Type().Bits())
		if err != nil {
			return convError("unsigned integer", err)
		}
		field.SetUint(u)
	case reflect.Float32, reflect.Float64:
		// Parse at the field's bit width, mirroring the integer rule above:
		// SetFloat silently converts like a C cast, so a value that fits
		// float64 but overflows float32 ("1e39") would become +Inf instead
		// of failing.
		f, err := strconv.ParseFloat(value, field.Type().Bits())
		if err != nil {
			return convError("float", err)
		}
		field.SetFloat(f)
	case reflect.Slice:
		return setSliceValue(field, value)
	default:
		return &MarshalError{
			Field:   "value",
			Message: fmt.Sprintf("unsupported type: %s", field.Kind()),
		}
	}

	return nil
}

// setSliceValue handles setting slice fields from comma-separated values.
func setSliceValue(field reflect.Value, value string) error {
	if value == "" {
		return nil
	}

	parts, quoted := SplitQuotedCommaParts(value)
	slice := reflect.MakeSlice(field.Type(), len(parts), len(parts))

	elemKind := field.Type().Elem().Kind()

	for i, part := range parts {
		// Trim only unquoted elements: grouping quotes exist to preserve
		// significant leading/trailing whitespace, which a blanket trim
		// silently stripped ("a , b" round-tripped as ["a","b"]).
		if !quoted[i] {
			part = strings.TrimSpace(part)
		}
		switch elemKind {
		case reflect.String:
			slice.Index(i).SetString(part)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			// time.Duration elements parse as durations — the same special
			// case setFieldValue applies to scalar fields, keeping the
			// struct-mapping layer at parity with GetSlice's element
			// grammar.
			if field.Type().Elem().String() == "time.Duration" {
				d, derr := time.ParseDuration(part)
				if derr != nil {
					return convError("duration", derr)
				}
				slice.Index(i).SetInt(int64(d))
				continue
			}
			// Bit width of the element type — parsing at the element's own
			// width makes out-of-range values an error instead of a silent
			// truncate on Set (same rationale as setFieldValue). Bits() is
			// only valid for arithmetic types, so it must stay inside the
			// numeric branches — calling it for []string panics.
			n, err := strconv.ParseInt(part, 10, field.Type().Elem().Bits())
			if err != nil {
				return convError("integer", err)
			}
			slice.Index(i).SetInt(n)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			n, err := strconv.ParseUint(part, 10, field.Type().Elem().Bits())
			if err != nil {
				return convError("unsigned integer", err)
			}
			slice.Index(i).SetUint(n)
		case reflect.Bool:
			b, ok := ParseBoolLoose(part)
			if !ok {
				return convError("boolean", strconv.ErrSyntax)
			}
			slice.Index(i).SetBool(b)
		case reflect.Float32, reflect.Float64:
			// Element bit width, same rationale as the scalar float branch.
			f, err := strconv.ParseFloat(part, field.Type().Elem().Bits())
			if err != nil {
				return convError("float", err)
			}
			slice.Index(i).SetFloat(f)
		default:
			return &MarshalError{
				Field:   "slice",
				Message: fmt.Sprintf("unsupported slice element type: %s", elemKind),
			}
		}
	}

	field.Set(slice)
	return nil
}

// buildPrefixedKey joins prefix and key with underscore.
// Uses stack-allocated buffer for short keys to avoid heap allocation.
func buildPrefixedKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	totalLen := len(prefix) + 1 + len(key)
	if totalLen <= 64 {
		var buf [64]byte
		n := copy(buf[:], prefix)
		buf[n] = '_'
		n++
		n += copy(buf[n:], key)
		return string(buf[:n])
	}
	return prefix + "_" + key
}
