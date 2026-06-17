package internal

import (
	"strings"
	"testing"
)

// ============================================================================
// NewValidator Tests
// ============================================================================

func TestNewValidator(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := ValidatorConfig{}
		v := NewValidator(cfg)

		if v == nil {
			t.Fatal("NewValidator() returned nil")
		}
		if v.keyPattern != nil {
			t.Error("keyPattern should be nil with default config")
		}
		if !v.useDefaultKeyCheck {
			t.Error("useDefaultKeyCheck should be true when no pattern provided")
		}
	})

	t.Run("with custom functions", func(t *testing.T) {
		cfg := ValidatorConfig{
			IsSensitive:   func(key string) bool { return strings.Contains(key, "SECRET") },
			MaskKey:       func(key string) string { return "***" },
			MaskSensitive: func(s string) string { return "[MASKED]" },
		}
		v := NewValidator(cfg)

		if !v.isSensitive("MY_SECRET") {
			t.Error("custom IsSensitive should detect SECRET")
		}
		if v.maskKey("KEY") != "***" {
			t.Error("custom MaskKey should return ***")
		}
		if v.maskSensitive("value") != "[MASKED]" {
			t.Error("custom MaskSensitive should return [MASKED]")
		}
	})

	t.Run("with allowed keys", func(t *testing.T) {
		cfg := ValidatorConfig{
			AllowedKeys: []string{"ALLOWED_KEY", "another_key"},
		}
		v := NewValidator(cfg)

		if len(v.allowedKeys) != 2 {
			t.Errorf("allowedKeys length = %d, want 2", len(v.allowedKeys))
		}
		// Keys should be uppercase
		if !v.allowedKeys["ALLOWED_KEY"] {
			t.Error("ALLOWED_KEY should be in allowedKeys")
		}
		if !v.allowedKeys["ANOTHER_KEY"] {
			t.Error("ANOTHER_KEY should be in allowedKeys (uppercase)")
		}
	})

	t.Run("with forbidden keys", func(t *testing.T) {
		cfg := ValidatorConfig{
			ForbiddenKeys: []string{"PATH", "HOME"},
		}
		v := NewValidator(cfg)

		if len(v.forbiddenKeys) != 2 {
			t.Errorf("forbiddenKeys length = %d, want 2", len(v.forbiddenKeys))
		}
		if !v.forbiddenKeys["PATH"] {
			t.Error("PATH should be in forbiddenKeys")
		}
	})

	t.Run("with required keys", func(t *testing.T) {
		cfg := ValidatorConfig{
			RequiredKeys: []string{"API_KEY", "DATABASE_URL"},
		}
		v := NewValidator(cfg)

		if len(v.requiredKeys) != 2 {
			t.Errorf("requiredKeys length = %d, want 2", len(v.requiredKeys))
		}
		if !v.requiredKeys["API_KEY"] {
			t.Error("API_KEY should be in requiredKeys")
		}
	})
}

// ============================================================================
// ValidateKey Tests
// ============================================================================

func TestValidator_ValidateKey(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		MaxKeyLength: 64,
	})

	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid key", "MY_KEY", false},
		{"valid key with numbers", "KEY123", false},
		{"underscore start invalid", "_PRIVATE_KEY", true}, // First char must be letter
		{"empty key", "", true},
		{"starts with number", "123_KEY", true},
		{"contains hyphen", "MY-KEY", true},
		{"contains space", "MY KEY", true},
		{"contains dot", "MY.KEY", true},
		{"lowercase allowed", "my_key", false}, // lowercase is allowed, just converted
		{"single letter", "K", false},
		{"single underscore invalid", "_", true},
		{"underscore in middle", "MY_KEY_NAME", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestValidator_ValidateKey_AllowedKeys(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		AllowedKeys:  []string{"ALLOWED_KEY"},
		MaxKeyLength: 64,
	})

	t.Run("allowed key", func(t *testing.T) {
		if err := v.ValidateKey("ALLOWED_KEY"); err != nil {
			t.Errorf("ValidateKey(allowed) error = %v", err)
		}
	})

	t.Run("non-allowed key", func(t *testing.T) {
		err := v.ValidateKey("NOT_ALLOWED")
		if err == nil {
			t.Error("ValidateKey(non-allowed) should fail")
		}
		var secErr *SecurityError
		if !AsError(err, &secErr) {
			t.Errorf("error type = %T, want SecurityError", err)
		}
	})
}

func TestValidator_ValidateKey_ForbiddenKeys(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		ForbiddenKeys: []string{"PATH"},
		MaxKeyLength:  64,
	})

	t.Run("normal key", func(t *testing.T) {
		if err := v.ValidateKey("MY_KEY"); err != nil {
			t.Errorf("ValidateKey(normal) error = %v", err)
		}
	})

	t.Run("forbidden key", func(t *testing.T) {
		err := v.ValidateKey("PATH")
		if err == nil {
			t.Error("ValidateKey(forbidden) should fail")
		}
		var secErr *SecurityError
		if !AsError(err, &secErr) {
			t.Errorf("error type = %T, want SecurityError", err)
		}
	})
}

func TestValidator_ValidateKey_MaxLength(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		MaxKeyLength: 10,
	})

	t.Run("within limit", func(t *testing.T) {
		if err := v.ValidateKey("SHORT"); err != nil {
			t.Errorf("ValidateKey(short) error = %v", err)
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		err := v.ValidateKey("THIS_KEY_IS_TOO_LONG")
		if err == nil {
			t.Error("ValidateKey(too long) should fail")
		}
	})
}

// ============================================================================
// ValidateValue Tests
// ============================================================================

func TestValidator_ValidateValue(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		MaxValueLength: 1024,
	})

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"normal value", "hello world", false},
		{"with newline", "line1\nline2", false},
		{"with tab", "col1\tcol2", false},
		{"with carriage return", "line1\rline2", false},
		{"null byte", "value\x00value", true},
		{"control char", "value\x01value", true},
		{"DEL char", "value\x7Fvalue", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateValue(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidator_ValidateValue_MaxLength(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		MaxValueLength: 10,
	})

	t.Run("within limit", func(t *testing.T) {
		if err := v.ValidateValue("short"); err != nil {
			t.Errorf("ValidateValue(short) error = %v", err)
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		err := v.ValidateValue("this value is too long")
		if err == nil {
			t.Error("ValidateValue(too long) should fail")
		}
	})
}

// ============================================================================
// ValidateRequired Tests
// ============================================================================

func TestValidator_ValidateRequired(t *testing.T) {
	t.Run("no required keys", func(t *testing.T) {
		v := NewValidator(ValidatorConfig{})
		keys := map[string]bool{"KEY": true}

		if err := v.ValidateRequired(keys); err != nil {
			t.Errorf("ValidateRequired() with no required keys error = %v", err)
		}
	})

	t.Run("all required keys present", func(t *testing.T) {
		v := NewValidator(ValidatorConfig{
			RequiredKeys: []string{"KEY1", "KEY2"},
		})
		keys := map[string]bool{"KEY1": true, "KEY2": true, "KEY3": true}

		if err := v.ValidateRequired(keys); err != nil {
			t.Errorf("ValidateRequired() error = %v", err)
		}
	})

	t.Run("missing required key", func(t *testing.T) {
		v := NewValidator(ValidatorConfig{
			RequiredKeys: []string{"KEY1", "KEY2", "KEY3"},
		})
		keys := map[string]bool{"KEY1": true} // Missing KEY2 and KEY3

		err := v.ValidateRequired(keys)
		if err == nil {
			t.Error("ValidateRequired() should fail with missing keys")
		}

		var valErr *ValidationError
		if !AsError(err, &valErr) {
			t.Errorf("error type = %T, want ValidationError", err)
		}
		if valErr.Rule != "required" {
			t.Errorf("error rule = %q, want 'required'", valErr.Rule)
		}
	})
}

// TestValidator_HasRequiredKeys covers the capability the parser fast-path
// probes (needsRequiredCheck in parser.go): with no required keys configured it
// reports false so parsers skip building the uppercase-key index.
func TestValidator_HasRequiredKeys(t *testing.T) {
	t.Run("empty config reports false", func(t *testing.T) {
		v := NewValidator(ValidatorConfig{})
		if v.HasRequiredKeys() {
			t.Error("HasRequiredKeys() = true, want false for empty config")
		}
	})

	t.Run("with required keys reports true", func(t *testing.T) {
		v := NewValidator(ValidatorConfig{
			RequiredKeys: []string{"KEY1", "KEY2"},
		})
		if !v.HasRequiredKeys() {
			t.Error("HasRequiredKeys() = false, want true when RequiredKeys set")
		}
	})
}

// ============================================================================
// IsSensitive Tests
// ============================================================================

func TestValidator_IsSensitive(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		v := NewValidator(ValidatorConfig{})
		if v.IsSensitive("ANY_KEY") {
			t.Error("default IsSensitive should return false")
		}
	})

	t.Run("custom function", func(t *testing.T) {
		v := NewValidator(ValidatorConfig{
			IsSensitive: func(key string) bool {
				return strings.Contains(strings.ToUpper(key), "PASSWORD")
			},
		})

		if !v.IsSensitive("MY_PASSWORD") {
			t.Error("IsSensitive should detect PASSWORD")
		}
		if v.IsSensitive("MY_KEY") {
			t.Error("IsSensitive should not detect regular key")
		}
	})
}

// ============================================================================
// ShouldMask Tests
// ============================================================================

func TestValidator_ShouldMask(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		IsSensitive: func(key string) bool {
			return strings.Contains(strings.ToUpper(key), "SECRET")
		},
	})

	t.Run("sensitive key should mask", func(t *testing.T) {
		if !v.ShouldMask("MY_SECRET") {
			t.Error("ShouldMask should return true for sensitive key")
		}
	})

	t.Run("non-sensitive key should not mask", func(t *testing.T) {
		if v.ShouldMask("MY_KEY") {
			t.Error("ShouldMask should return false for non-sensitive key")
		}
	})
}

// ============================================================================
// MaskValue Tests
// ============================================================================

func TestValidator_MaskValue(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		IsSensitive: func(key string) bool {
			return strings.Contains(strings.ToUpper(key), "SECRET")
		},
	})

	t.Run("sensitive value masked", func(t *testing.T) {
		result := v.MaskValue("MY_SECRET", "super_secret_password_12345")
		if !strings.Contains(result, "[MASKED:") {
			t.Errorf("MaskValue(sensitive) = %q, should contain [MASKED:", result)
		}
	})

	t.Run("short non-sensitive value shown", func(t *testing.T) {
		result := v.MaskValue("MY_KEY", "short")
		if result != "short" {
			t.Errorf("MaskValue(short non-sensitive) = %q, want 'short'", result)
		}
	})

	t.Run("long non-sensitive value truncated", func(t *testing.T) {
		longValue := "this_is_a_very_long_value_that_exceeds_20_chars"
		result := v.MaskValue("MY_KEY", longValue)
		if !strings.HasSuffix(result, "...") {
			t.Errorf("MaskValue(long non-sensitive) = %q, should end with ...", result)
		}
	})
}

// ============================================================================
// Default Functions Tests
// ============================================================================

func TestDefaultIsSensitive(t *testing.T) {
	// Default function always returns false
	if defaultIsSensitive("PASSWORD") {
		t.Error("defaultIsSensitive should always return false")
	}
	if defaultIsSensitive("SECRET") {
		t.Error("defaultIsSensitive should always return false")
	}
}

func TestDefaultMaskKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"AB", "***"},        // len <= 3
		{"ABC", "***"},       // len <= 3
		{"ABCD", "AB***"},    // len > 3
		{"API_KEY", "AP***"}, // len > 3
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := DefaultMaskKey(tt.input)
			if result != tt.expected {
				t.Errorf("DefaultMaskKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultMaskSensitive(t *testing.T) {
	t.Run("short value", func(t *testing.T) {
		result := defaultMaskSensitive("short")
		if result != "short" {
			t.Errorf("defaultMaskSensitive(short) = %q, want 'short'", result)
		}
	})

	t.Run("long value truncated", func(t *testing.T) {
		longValue := strings.Repeat("a", 60)
		result := defaultMaskSensitive(longValue)
		if !strings.HasSuffix(result, "...") {
			t.Errorf("defaultMaskSensitive(long) should end with ..., got %q", result)
		}
		if len(result) > 53 { // 50 + "..."
			t.Errorf("defaultMaskSensitive(long) length = %d, should be <= 53", len(result))
		}
	})
}

// ============================================================================
// validateValueChars Tests
// ============================================================================

func TestValidateValueChars(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", false},
		{"normal ASCII", "hello world", false},
		{"with tab", "hello\tworld", false},
		{"with newline", "hello\nworld", false},
		{"with carriage return", "hello\rworld", false},
		{"null byte", "hello\x00world", true},
		{"control char BEL", "hello\x07world", true},
		{"control char BS", "hello\x08world", true},
		{"DEL char", "hello\x7Fworld", true},
		{"unicode", "hello 世界", false},
		{"all allowed controls", "\t\n\r", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateValueChars(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateValueChars() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// AsError is a helper to check error types
func AsError(err error, target interface{}) bool {
	if err == nil {
		return false
	}
	switch t := target.(type) {
	case **ValidationError:
		if ve, ok := err.(*ValidationError); ok {
			*t = ve
			return true
		}
	case **SecurityError:
		if se, ok := err.(*SecurityError); ok {
			*t = se
			return true
		}
	}
	return false
}
