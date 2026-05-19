// Package internal provides internal struct marshaling utilities.
package internal

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Struct converts a struct to environment variables.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Nested structs are flattened with underscore-separated keys.
func Struct(v interface{}, prefix string) (map[string]string, error) {
	return marshalStruct(v, prefix)
}

func marshalStruct(v interface{}, prefix string) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
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

		// Determine the key
		key := tag
		if key == "" {
			key = ToUpperASCII(fieldType.Name)
		}
		if prefix != "" {
			key = buildPrefixedKey(prefix, key)
		}

		// Handle nested structs
		if field.Kind() == reflect.Struct {
			nested, err := marshalStruct(field.Interface(), key)
			if err != nil {
				return nil, err
			}
			for k, v := range nested {
				result[k] = v
			}
			continue
		}

		// Handle pointers to structs
		if field.Kind() == reflect.Ptr && !field.IsNil() {
			if field.Elem().Kind() == reflect.Struct {
				nested, err := marshalStruct(field.Elem().Interface(), key)
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
func StructInto(data map[string]string, val reflect.Value, prefix string) error {
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

		// Handle nested structs
		if field.Kind() == reflect.Struct {
			// Only pass prefix to nested struct if this field has an explicit env tag.
			// If no env tag, child fields use their full env tags directly.
			nestedPrefix := ""
			if tag != "" {
				nestedPrefix = key
			}
			if err := StructInto(data, field, nestedPrefix); err != nil {
				return err
			}
			continue
		}

		// Handle pointers
		if field.Kind() == reflect.Ptr {
			if field.Type().Elem().Kind() == reflect.Struct {
				// Create new struct if needed
				if field.IsNil() {
					field.Set(reflect.New(field.Type().Elem()))
				}
				// Only pass prefix to nested struct if this field has an explicit env tag
				nestedPrefix := ""
				if tag != "" {
					nestedPrefix = key
				}
				if err := StructInto(data, field.Elem(), nestedPrefix); err != nil {
					return err
				}
				continue
			}
		}

		// For non-struct fields, add prefix if parent passed one
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

	// Handle interfaces
	if v.CanInterface() {
		// Check for TextMarshaler
		if tm, ok := v.Interface().(encoding.TextMarshaler); ok {
			data, err := tm.MarshalText()
			if err != nil {
				return "", err
			}
			return string(data), nil
		}

		// Check for JSON marshaler for complex types
		if jm, ok := v.Interface().(json.Marshaler); ok {
			data, err := jm.MarshalJSON()
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
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	case reflect.Slice:
		// Handle []byte
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return string(v.Bytes()), nil
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
				if i > 0 {
					sb.WriteByte(',')
				}
				sb.WriteString(v.Index(i).String())
			}
			return sb.String(), nil
		}
		return "", &MarshalError{
			Field:   "slice",
			Message: "unsupported slice type",
		}
	case reflect.Ptr:
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

// setFieldValue sets a struct field from a string value.
func setFieldValue(field reflect.Value, value string) error {
	if value == "" && field.Kind() != reflect.String {
		return nil // Don't set empty values for non-string fields
	}

	// Handle pointer types
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setFieldValue(field.Elem(), value)
	}

	// Check for TextUnmarshaler
	if field.CanAddr() {
		if tu, ok := field.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return tu.UnmarshalText([]byte(value))
		}
	}

	// Handle based on kind
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Special case for time.Duration
		if field.Type().String() == "time.Duration" {
			d, err := time.ParseDuration(value)
			if err != nil {
				return err
			}
			field.SetInt(int64(d))
			return nil
		}
		i, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
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

	parts := strings.Split(value, ",")
	slice := reflect.MakeSlice(field.Type(), len(parts), len(parts))

	elemKind := field.Type().Elem().Kind()

	for i, part := range parts {
		part = strings.TrimSpace(part)
		switch elemKind {
		case reflect.String:
			slice.Index(i).SetString(part)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				return err
			}
			slice.Index(i).SetInt(n)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			n, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return err
			}
			slice.Index(i).SetUint(n)
		case reflect.Bool:
			b, err := strconv.ParseBool(part)
			if err != nil {
				return err
			}
			slice.Index(i).SetBool(b)
		case reflect.Float32, reflect.Float64:
			f, err := strconv.ParseFloat(part, 64)
			if err != nil {
				return err
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
