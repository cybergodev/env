// Package internal provides YAML flattening utilities.
package internal

import (
	"encoding/json"
	"fmt"
	"strconv"
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
		// Check for inline JSON array or object
		scalar := TrimSpace(value.Scalar)
		if len(scalar) >= 2 {
			if (scalar[0] == '[' && scalar[len(scalar)-1] == ']') ||
				(scalar[0] == '{' && scalar[len(scalar)-1] == '}') {
				// Try to parse as inline JSON
				if err := flattenInlineJSON(scalar, prefix, cfg, result, depth); err == nil {
					return nil
				}
				// If parsing fails, fall through to treat as regular scalar
			}
		}
		result[prefix] = convertYAMLScalar(value.Scalar, cfg)

	case ValueTypeMap:
		if len(value.Map) == 0 && prefix != "" {
			// Empty map as empty string
			result[prefix] = ""
			return nil
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
			result[prefix] = ""
			return nil
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
// Uses direct concatenation for the common case (short keys) to avoid pool overhead.
func buildYAMLKey(prefix, key string, cfg YAMLFlattenConfig) string {
	key = ToUpperASCII(key)
	if prefix == "" {
		return key
	}
	// For the common case of short keys, direct concatenation is faster
	// than pool Get/Put overhead. The pool is beneficial only for very long keys.
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

// buildYAMLArrayIndex constructs a key for array elements.
// Uses direct concatenation for short keys to avoid pool overhead.
func buildYAMLArrayIndex(prefix string, index int, cfg YAMLFlattenConfig) string {
	indexStr := strconv.Itoa(index)

	switch cfg.ArrayIndexFormat {
	case "bracket":
		// prefix[index] format
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
	default: // underscore
		// prefix_index format
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

// flattenInlineJSON parses and flattens inline JSON arrays or objects within YAML.
func flattenInlineJSON(jsonStr, prefix string, cfg YAMLFlattenConfig, result map[string]string, depth int) error {
	// SECURITY: Pre-validate JSON nesting depth to prevent excessive memory
	// allocation during json.Unmarshal. Count brackets conservatively
	// (may overcount brackets inside quoted strings, but that's safe).
	nesting := 0
	for i := 0; i < len(jsonStr); i++ {
		switch jsonStr[i] {
		case '{', '[':
			nesting++
			if depth+nesting > cfg.MaxDepth {
				return &YAMLError{
					Path:    prefix,
					Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", cfg.MaxDepth),
				}
			}
		case '}', ']':
			nesting--
			if nesting < 0 {
				nesting = 0
			}
		case '"':
			// Skip string contents to avoid counting brackets inside strings
			i++
			for i < len(jsonStr) {
				if jsonStr[i] == '\\' {
					i++ // skip escaped char
				} else if jsonStr[i] == '"' {
					break
				}
				i++
			}
		}
	}

	// Parse the inline JSON
	var raw interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return err
	}

	// Flatten the parsed value directly
	switch v := raw.(type) {
	case []interface{}:
		// Inline array
		for i, item := range v {
			itemPrefix := buildYAMLArrayIndex(prefix, i, cfg)
			if err := flattenInlineValue(item, itemPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		// Inline object
		for key, val := range v {
			keyPrefix := buildYAMLKey(prefix, key, cfg)
			if err := flattenInlineValue(val, keyPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}
	default:
		// Simple scalar value
		result[prefix] = fmt.Sprintf("%v", v)
	}

	return nil
}

// flattenInlineValue flattens a value from inline JSON.
func flattenInlineValue(value interface{}, prefix string, cfg YAMLFlattenConfig, result map[string]string, depth int) error {
	// Check depth limit - use >= for strict enforcement
	if depth >= cfg.MaxDepth {
		return &YAMLError{
			Path:    prefix,
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", cfg.MaxDepth),
		}
	}

	switch v := value.(type) {
	case nil:
		if cfg.NullAsEmpty {
			result[prefix] = ""
		} else {
			result[prefix] = "null"
		}
	case bool:
		if cfg.BoolAsString {
			result[prefix] = strconv.FormatBool(v)
		} else {
			result[prefix] = fmt.Sprintf("%t", v)
		}
	case float64:
		if cfg.NumberAsString {
			if v == float64(int64(v)) {
				result[prefix] = strconv.FormatInt(int64(v), 10)
			} else {
				result[prefix] = strconv.FormatFloat(v, 'f', -1, 64)
			}
		} else {
			result[prefix] = fmt.Sprintf("%v", v)
		}
	case string:
		result[prefix] = v
	case []interface{}:
		for i, item := range v {
			itemPrefix := buildYAMLArrayIndex(prefix, i, cfg)
			if err := flattenInlineValue(item, itemPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		for key, val := range v {
			keyPrefix := buildYAMLKey(prefix, key, cfg)
			if err := flattenInlineValue(val, keyPrefix, cfg, result, depth+1); err != nil {
				return err
			}
		}
	default:
		result[prefix] = fmt.Sprintf("%v", v)
	}

	return nil
}
