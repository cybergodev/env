// Package internal provides JSON flattening utilities.
package internal

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// JSONFlattenConfig holds configuration for JSON flattening.
type JSONFlattenConfig struct {
	// KeyDelimiter is the delimiter for nested keys (default: "_").
	KeyDelimiter string
	// ArrayIndexFormat controls how array indices are formatted.
	// "underscore": KEY_0, KEY_1, etc.
	ArrayIndexFormat string
	// NullAsEmpty converts null values to empty strings (default: true).
	NullAsEmpty bool
	// NumberAsString converts numbers to strings (default: true).
	NumberAsString bool
	// BoolAsString converts booleans to strings (default: true).
	BoolAsString bool
	// MaxDepth limits the maximum nesting depth to prevent stack overflow.
	MaxDepth int
}

// FlattenJSON converts nested JSON data to a flat map of string key-value pairs.
// Keys are converted to uppercase with the configured delimiter.
func FlattenJSON(data []byte, cfg JSONFlattenConfig) (map[string]string, error) {
	if len(data) == 0 {
		return make(map[string]string), nil
	}

	// Parse JSON
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &JSONError{
			Path:    "",
			Message: "invalid JSON syntax",
			Err:     err,
		}
	}

	// Estimate capacity from input size to reduce map growth.
	// Average JSON key-value pair is ~30-50 bytes of raw JSON, producing one flat entry.
	estSize := len(data) / 40
	if estSize < 4 {
		estSize = 4
	}
	result := make(map[string]string, estSize)
	if err := flattenValue(raw, "", cfg, result, 0); err != nil {
		return nil, err
	}

	return result, nil
}

// flattenValue recursively flattens a JSON value.
func flattenValue(value interface{}, prefix string, cfg JSONFlattenConfig, result map[string]string, depth int) error {
	// Check depth limit - use >= for strict enforcement
	if depth >= cfg.MaxDepth {
		return &JSONError{
			Path:    prefix,
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", cfg.MaxDepth),
		}
	}

	switch v := value.(type) {
	case nil:
		if prefix == "" {
			return nil
		}
		if cfg.NullAsEmpty {
			result[prefix] = ""
		} else {
			result[prefix] = "null"
		}

	case bool:
		if prefix == "" {
			return nil
		}
		if cfg.BoolAsString {
			result[prefix] = strconv.FormatBool(v)
		} else {
			result[prefix] = fmt.Sprintf("%t", v)
		}

	case float64:
		if prefix == "" {
			return nil
		}
		if cfg.NumberAsString {
			// Format as integer if it's a whole number
			if v == float64(int64(v)) {
				result[prefix] = strconv.FormatInt(int64(v), 10)
			} else {
				result[prefix] = strconv.FormatFloat(v, 'f', -1, 64)
			}
		} else {
			result[prefix] = fmt.Sprintf("%v", v)
		}

	case string:
		if prefix == "" {
			return nil
		}
		result[prefix] = v

	case map[string]interface{}:
		for key, val := range v {
			newPrefix := buildKey(prefix, key, cfg)
			if err := flattenValue(val, newPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}

	case []interface{}:
		for i := range v {
			newPrefix := buildArrayIndex(prefix, i, cfg)
			if err := flattenValue(v[i], newPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}

	default:
		return &JSONError{
			Path:    prefix,
			Message: fmt.Sprintf("unsupported JSON type: %T", value),
		}
	}

	return nil
}

// buildKey constructs a key from prefix and key parts.
// Uses direct concatenation for short keys to avoid pool overhead.
func buildKey(prefix, key string, cfg JSONFlattenConfig) string {
	key = ToUpperASCII(key)
	if prefix == "" {
		return key
	}
	totalLen := len(prefix) + len(cfg.KeyDelimiter) + len(key)
	if totalLen <= 64 {
		return prefix + cfg.KeyDelimiter + key
	}
	sb := GetBuilder()
	defer PutBuilder(sb)
	sb.Grow(totalLen)
	sb.WriteString(prefix)
	sb.WriteString(cfg.KeyDelimiter)
	sb.WriteString(key)
	return sb.String()
}

// buildArrayIndex constructs a key for array elements.
// Uses direct concatenation for short keys to avoid pool overhead.
func buildArrayIndex(prefix string, index int, cfg JSONFlattenConfig) string {
	indexStr := strconv.Itoa(index)

	switch cfg.ArrayIndexFormat {
	case "bracket":
		totalLen := len(prefix) + 1 + len(indexStr) + 1
		if totalLen <= 64 {
			return prefix + "[" + indexStr + "]"
		}
		sb := GetBuilder()
		defer PutBuilder(sb)
		sb.Grow(totalLen)
		sb.WriteString(prefix)
		sb.WriteByte('[')
		sb.WriteString(indexStr)
		sb.WriteByte(']')
		return sb.String()
	default:
		totalLen := len(prefix) + len(cfg.KeyDelimiter) + len(indexStr)
		if totalLen <= 64 {
			return prefix + cfg.KeyDelimiter + indexStr
		}
		sb := GetBuilder()
		defer PutBuilder(sb)
		sb.Grow(totalLen)
		sb.WriteString(prefix)
		sb.WriteString(cfg.KeyDelimiter)
		sb.WriteString(indexStr)
		return sb.String()
	}
}
