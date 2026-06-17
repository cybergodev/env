// Package internal provides input validation for environment variable keys and values.
package internal

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidatorConfig holds configuration for creating a new Validator.
type ValidatorConfig struct {
	KeyPattern     *regexp.Regexp
	AllowedKeys    []string
	ForbiddenKeys  []string
	RequiredKeys   []string
	MaxKeyLength   int
	MaxValueLength int
	ValidateUTF8   bool              // Validate that values are valid UTF-8
	IsSensitive    func(string) bool // Injected from root package
	MaskKey        func(string) string
	MaskSensitive  func(string) string
}

// Validator provides input validation for environment variable keys and values.
type Validator struct {
	keyPattern         *regexp.Regexp
	allowedKeys        map[string]bool
	forbiddenKeys      map[string]bool
	requiredKeys       map[string]bool
	maxKeyLength       int
	maxValueLength     int
	validateUTF8       bool
	isSensitive        func(string) bool
	maskKey            func(string) string
	maskSensitive      func(string) string
	useDefaultKeyCheck bool // If true, use fast byte checks instead of regex
}

// defaultIsSensitive is the default sensitive key check function.
func defaultIsSensitive(key string) bool { return false }

// defaultMaskSensitive is the default sensitive value masking function.
func defaultMaskSensitive(s string) string {
	const maxLen = 50
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// NewValidator creates a new Validator with the specified configuration.
func NewValidator(cfg ValidatorConfig) *Validator {
	v := &Validator{
		keyPattern:         cfg.KeyPattern,
		maxKeyLength:       cfg.MaxKeyLength,
		maxValueLength:     cfg.MaxValueLength,
		validateUTF8:       cfg.ValidateUTF8,
		useDefaultKeyCheck: cfg.KeyPattern == nil,
	}

	// Set functions with defaults
	if cfg.IsSensitive != nil {
		v.isSensitive = cfg.IsSensitive
	} else {
		v.isSensitive = defaultIsSensitive
	}
	if cfg.MaskKey != nil {
		v.maskKey = cfg.MaskKey
	} else {
		v.maskKey = DefaultMaskKey
	}
	if cfg.MaskSensitive != nil {
		v.maskSensitive = cfg.MaskSensitive
	} else {
		v.maskSensitive = defaultMaskSensitive
	}

	// Only create maps if we have keys to add
	if len(cfg.AllowedKeys) > 0 {
		v.allowedKeys = make(map[string]bool, len(cfg.AllowedKeys))
		for _, k := range cfg.AllowedKeys {
			v.allowedKeys[ToUpperASCII(k)] = true
		}
	}

	if len(cfg.ForbiddenKeys) > 0 {
		v.forbiddenKeys = make(map[string]bool, len(cfg.ForbiddenKeys))
		for _, k := range cfg.ForbiddenKeys {
			v.forbiddenKeys[ToUpperASCII(k)] = true
		}
	}

	if len(cfg.RequiredKeys) > 0 {
		v.requiredKeys = make(map[string]bool, len(cfg.RequiredKeys))
		for _, k := range cfg.RequiredKeys {
			v.requiredKeys[ToUpperASCII(k)] = true
		}
	}

	return v
}

// ValidateKey validates an environment variable key.
// Returns an error if the key is invalid or forbidden.
func (v *Validator) ValidateKey(key string) error {
	// Check length
	if len(key) == 0 {
		return v.newValidationError("key", key, "non_empty", "key cannot be empty")
	}
	if len(key) > v.maxKeyLength {
		return v.newValidationError("key", key, "max_length", "key exceeds maximum length")
	}

	// SECURITY: Check for non-ASCII characters to prevent Unicode homograph attacks
	// This must be done before pattern validation to avoid bypass via lookalike characters
	// e.g., ℌOST (U+210C) looks similar to HOST but would bypass ASCII-only validation
	for i := 0; i < len(key); i++ {
		if key[i] >= 0x80 {
			return v.newValidationError("key", key, "ascii_only", "key contains non-ASCII characters")
		}
	}

	// Check pattern - use fast byte-level check for default pattern
	if v.useDefaultKeyCheck {
		// Fast path: validate default pattern ^[A-Za-z][A-Za-z0-9_]*$ without regex
		if !isValidDefaultKey(key) {
			return v.newValidationError("key", key, "pattern", "key does not match required pattern")
		}
	} else if v.keyPattern != nil && !v.keyPattern.MatchString(key) {
		return v.newValidationError("key", key, "pattern", "key does not match required pattern")
	}

	// Only compute uppercase key if we need to check lists
	if len(v.allowedKeys) > 0 || len(v.forbiddenKeys) > 0 {
		upperKey := ToUpperASCII(key)

		// Check if in allowed list (if specified)
		if len(v.allowedKeys) > 0 && !v.allowedKeys[upperKey] {
			return v.newSecurityError("key_access", "key not in allowed list", key)
		}

		// Check forbidden list
		if v.forbiddenKeys[upperKey] {
			return v.newSecurityError("key_access", "key is forbidden", key)
		}
	}

	return nil
}

// ValidateValue validates an environment variable value.
// Returns an error if the value contains invalid content.
func (v *Validator) ValidateValue(value string) error {
	// Check length
	if len(value) > v.maxValueLength {
		return v.newValidationError("value", "", "max_length", "value exceeds maximum length")
	}

	// Optional: Validate UTF-8 encoding
	if v.validateUTF8 && !utf8.ValidString(value) {
		return v.newValidationError("value", "", "utf8", "value contains invalid UTF-8 encoding")
	}

	// Fast path: check for problematic control characters using optimized scan
	// This uses a lookup table for O(1) character classification
	return validateValueChars(value)
}

// badCharTable is a lookup table for invalid characters.
// Index 0-31: control characters (0x00-0x1F)
// Index 127: DEL character (0x7F)
// Allowed characters are marked as 0, invalid as 1.
// badCharTable[i] == 1 means byte i is disallowed in values.
// Entries 0x00-0x08, 0x0B-0x0C, 0x0E-0x1F, 0x7F are set to 1 (disallowed).
// All other entries are 0 (allowed) by Go's zero-initialization.
var badCharTable = [256]byte{
	// 0x00-0x08: control chars (invalid)
	1, 1, 1, 1, 1, 1, 1, 1, 1,
	// 0x09: tab (allowed)
	0,
	// 0x0A: newline (allowed)
	0,
	// 0x0B-0x0C: control chars (invalid)
	1, 1,
	// 0x0D: carriage return (allowed)
	0,
	// 0x0E-0x1F: control chars (invalid)
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	// 0x7F: DEL (invalid)
	127: 1,
}

// validateValueChars checks for invalid characters using a lookup table.
// Each byte is classified in O(1) via badCharTable; the first disallowed byte
// yields a descriptive error.
//
// The previous 8-byte-at-a-time (SIMD-style) unrolling plus an unsafe
// string-to-bytes helper were removed for readability. Per the project
// guidelines, readability wins over micro-optimization unless profiling
// identifies a regression, and the simple loop is obviously correct.
func validateValueChars(value string) error {
	for i := 0; i < len(value); i++ {
		if badCharTable[value[i]] != 0 {
			return badCharError(value[i], i)
		}
	}
	return nil
}

// badCharError creates an appropriate error for a bad character.
func badCharError(c byte, pos int) error {
	if c == 0 {
		return &ValidationError{
			Field:   "value",
			Value:   "",
			Rule:    "null_byte",
			Message: "value contains null byte",
		}
	}
	return &ValidationError{
		Field:   "value",
		Value:   "",
		Rule:    "control_char",
		Message: fmt.Sprintf("value contains control character at position %d", pos),
	}
}

// HasRequiredKeys reports whether the validator has any required keys
// configured. Callers use this to skip building the uppercase-key index when
// there is nothing to validate — the common case (no RequiredKeys in Config).
func (v *Validator) HasRequiredKeys() bool {
	return len(v.requiredKeys) > 0
}

// ValidateRequired checks that all required keys are present.
// Returns an error listing any missing required keys.
func (v *Validator) ValidateRequired(keys map[string]bool) error {
	if len(v.requiredKeys) == 0 {
		return nil
	}

	missing := make([]string, 0, len(v.requiredKeys))
	for reqKey := range v.requiredKeys {
		if !keys[reqKey] {
			missing = append(missing, reqKey)
		}
	}

	if len(missing) > 0 {
		return &ValidationError{
			Field:   "required_keys",
			Value:   "",
			Rule:    "required",
			Message: "missing required keys: " + strings.Join(missing, ", "),
		}
	}

	return nil
}

// IsSensitive returns true if the key appears to be sensitive.
func (v *Validator) IsSensitive(key string) bool {
	return v.isSensitive(key)
}

// ShouldMask returns true if the key's value should be masked in logs.
func (v *Validator) ShouldMask(key string) bool {
	return v.IsSensitive(key)
}

// MaskValue masks a value for logging purposes.
func (v *Validator) MaskValue(key, value string) string {
	if !v.ShouldMask(key) {
		if len(value) <= maxValueDisplayLen {
			return value
		}
		return value[:maxValueDisplayLen-3] + "..."
	}
	// For sensitive values, show only length
	return fmt.Sprintf("[MASKED:%d chars]", len(value))
}

// newValidationError creates a new ValidationError.
func (v *Validator) newValidationError(field, value, rule, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   v.maskSensitive(value),
		Rule:    rule,
		Message: message,
	}
}

// newSecurityError creates a new SecurityError.
func (v *Validator) newSecurityError(action, reason, key string) *SecurityError {
	return &SecurityError{
		Action: action,
		Reason: reason,
		Key:    v.maskKey(key),
	}
}
