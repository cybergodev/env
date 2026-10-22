package internal

import (
	"errors"
	"regexp"
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

// TestNewValidator_ZeroConfigFailsClosed pins the fail-closed behavior of a
// zero-value ValidatorConfig: MaxKeyLength and MaxValueLength are both 0, so
// ValidateKey/ValidateValue reject every NON-EMPTY input on the length check
// alone. Callers must derive configs from the documented defaults rather
// than passing a zero config.
func TestNewValidator_ZeroConfigFailsClosed(t *testing.T) {
	v := NewValidator(ValidatorConfig{})

	if err := v.ValidateKey("KEY"); err == nil {
		t.Error("ValidateKey(non-empty) with zero config = nil, want max_length error (fail-closed)")
	}
	if err := v.ValidateValue("value"); err == nil {
		t.Error("ValidateValue(non-empty) with zero config = nil, want max_length error (fail-closed)")
	}
	// The empty value is the one input a zero config still accepts: length 0
	// passes the check and there are no control characters to reject.
	if err := v.ValidateValue(""); err != nil {
		t.Errorf("ValidateValue(\"\") with zero config = %v, want nil", err)
	}
}

// ============================================================================
// ValidateKey Tests
// ============================================================================

// TestValidator_ValidateKey covers the .env key contract in one table: shape
// (default scan and custom patterns), length boundaries, allowed/forbidden
// key policy, and the Unicode homograph defense — keys containing non-ASCII
// bytes are rejected under both the default scan and a custom pattern
// (SECURITY: prevents ℌOST-style spoofs of restricted keys; custom patterns
// keep the dedicated ASCII scan because a user pattern may permit non-ASCII).
func TestValidator_ValidateKey(t *testing.T) {
	const homograph = "ℌOST" // U+210C SCRIPT CAPITAL H, looks like HOST
	permissivePattern := regexp.MustCompile(`.*`)
	strictPattern := regexp.MustCompile(`^[A-Z_]+$`)
	defaultCfg := ValidatorConfig{MaxKeyLength: 64}

	tests := []struct {
		name    string
		cfg     ValidatorConfig
		key     string
		wantErr bool
	}{
		// Shape under the default scan.
		{name: "valid key", cfg: defaultCfg, key: "MY_KEY", wantErr: false},
		{name: "valid key with numbers", cfg: defaultCfg, key: "KEY123", wantErr: false},
		{name: "underscore start invalid", cfg: defaultCfg, key: "_PRIVATE_KEY", wantErr: true}, // First char must be letter
		{name: "empty key", cfg: defaultCfg, key: "", wantErr: true},
		{name: "starts with number", cfg: defaultCfg, key: "123_KEY", wantErr: true},
		{name: "contains hyphen", cfg: defaultCfg, key: "MY-KEY", wantErr: true},
		{name: "contains space", cfg: defaultCfg, key: "MY KEY", wantErr: true},
		{name: "contains dot", cfg: defaultCfg, key: "MY.KEY", wantErr: true},
		{name: "lowercase allowed", cfg: defaultCfg, key: "my_key", wantErr: false}, // lowercase is allowed, just converted
		{name: "single letter", cfg: defaultCfg, key: "K", wantErr: false},
		{name: "single underscore invalid", cfg: defaultCfg, key: "_", wantErr: true},
		{name: "underscore in middle", cfg: defaultCfg, key: "MY_KEY_NAME", wantErr: false},
		// Allowed/forbidden policy.
		{name: "allowed key accepted", cfg: ValidatorConfig{AllowedKeys: []string{"ALLOWED_KEY"}, MaxKeyLength: 64}, key: "ALLOWED_KEY", wantErr: false},
		{name: "non-allowed key rejected", cfg: ValidatorConfig{AllowedKeys: []string{"ALLOWED_KEY"}, MaxKeyLength: 64}, key: "NOT_ALLOWED", wantErr: true},
		{name: "forbidden key rejected", cfg: ValidatorConfig{ForbiddenKeys: []string{"PATH"}, MaxKeyLength: 64}, key: "PATH", wantErr: true},
		{name: "normal key unaffected by forbidden list", cfg: ValidatorConfig{ForbiddenKeys: []string{"PATH"}, MaxKeyLength: 64}, key: "MY_KEY", wantErr: false},
		// Length boundaries.
		{name: "within length limit", cfg: ValidatorConfig{MaxKeyLength: 10}, key: "SHORT", wantErr: false},
		// Boundary: exactly at the limit is still valid; one over fails.
		{name: "exactly at length limit", cfg: ValidatorConfig{MaxKeyLength: 10}, key: "ABCDEFGHIJ", wantErr: false},
		{name: "one over length limit", cfg: ValidatorConfig{MaxKeyLength: 10}, key: "ABCDEFGHIJK", wantErr: true},
		// Non-ASCII homograph defense (default scan and custom patterns).
		{name: "default check rejects homograph", cfg: defaultCfg, key: homograph, wantErr: true},
		{name: "default check accepts ASCII key", cfg: defaultCfg, key: "HOST", wantErr: false},
		{name: "custom pattern rejects homograph", cfg: ValidatorConfig{KeyPattern: permissivePattern, MaxKeyLength: 64}, key: homograph, wantErr: true},
		{name: "custom pattern rejects non-matching key", cfg: ValidatorConfig{KeyPattern: strictPattern, MaxKeyLength: 64}, key: "INVALID-KEY", wantErr: true},
		{name: "custom pattern accepts matching key", cfg: ValidatorConfig{KeyPattern: strictPattern, MaxKeyLength: 64}, key: "GOOD_KEY", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator(tt.cfg)
			err := v.ValidateKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

// ============================================================================
// ValidateValue Tests
// ============================================================================

func TestValidator_ValidateValue_MaxLength(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		MaxValueLength: 10,
	})

	t.Run("within limit", func(t *testing.T) {
		if err := v.ValidateValue("short"); err != nil {
			t.Errorf("ValidateValue(short) error = %v", err)
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		// Boundary: a value whose length equals MaxValueLength passes —
		// only strictly longer values fail the check.
		if err := v.ValidateValue(strings.Repeat("a", 10)); err != nil {
			t.Errorf("ValidateValue(exactly MaxValueLength) error = %v, want nil", err)
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
			t.Fatal("ValidateRequired() should fail with missing keys")
		}

		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("error type = %T, want ValidationError", err)
		}
		if valErr.Rule != "required" {
			t.Errorf("error rule = %q, want 'required'", valErr.Rule)
		}
	})

	t.Run("nil keys map reports all required missing", func(t *testing.T) {
		// Boundary: a nil map reads as "nothing present", so every required
		// key is reported missing (nil-map lookups return the zero value).
		v := NewValidator(ValidatorConfig{
			RequiredKeys: []string{"KEY1", "KEY2"},
		})

		err := v.ValidateRequired(nil)
		if err == nil {
			t.Fatal("ValidateRequired(nil) should fail when required keys are configured")
		}

		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("error type = %T, want ValidationError", err)
		}
		if valErr.Rule != "required" {
			t.Errorf("error rule = %q, want 'required'", valErr.Rule)
		}
		for _, key := range []string{"KEY1", "KEY2"} {
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error %q should list missing key %s", err, key)
			}
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
// validateValueChars Tests
// ============================================================================

// TestValidateValueChars pins validateValueChars' byte-table semantics,
// including the word-at-a-time prefilter boundaries: chunk edges at 8/16
// bytes, borrow-generator byte patterns in the hasLess/hasDel lanes, and
// high (>= 0x80) byte values, which the prefilter may flag but the exact
// byte loop must clear.
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
		// Word-at-a-time prefilter boundaries (8-byte chunks).
		{"len7", "abcdefg", false},
		{"len8", "abcdefgh", false},
		{"len9", "abcdefghi", false},
		{"len15", "abcdefghijklmno", false},
		{"len16", "abcdefghijklmnop", false},
		{"tab-nl-cr allowed", "a\tb\nc\rd", false},
		{"allowed block of 8 tabs", "\t\t\t\t\t\t\t\t", false},
		{"mixed allowed 8", "\t\n\r\t\n\r\t\n", false},
		{"NUL at 0", "\x00abcdefg", true},
		{"NUL at 7", "abcdefg\x00", true},
		{"NUL at 8 (2nd chunk)", "abcdefgh\x00", true},
		{"NUL at 15", "abcdefghijklmnu\x00", true},
		{"DEL at 0", "\x7fabcdef", true},
		{"DEL at 7", "abcdefg\x7f", true},
		{"DEL at 8", "abcdefgh\x7f", true},
		{"0x01 at 6", "abcdef\x01g", true},
		{"0x1f at 13", "abcdefghijklm\x1fno", true},
		{"0x0b (VT) mid", "abcde\x0bfghijkl", true},
		{"high bytes allowed", "\x80\x9f\xa0\xff\x85\xfe\x80\x90", false},
		{"high bytes + NUL", "\x80\x9f\xa0\xff\x00\xfe\x80\x90", true},
		{"high bytes + DEL", "\x80\x9f\xa0\xff\x7f\xfe\x80\x90", true},
		{"borrow-generator then NUL", "\xff\xff\xff\xff\xff\xff\xff\x00", true},
		{"0x9f block then NUL", "\x9f\x9f\x9f\x9f\x9f\x9f\x9f\x00", true},
		{"0xA0 block (spurious flag)", "\xa0\xa0\xa0\xa0\xa0\xa0\xa0\xa0", false},
		{"0x7f-lane xor pattern", "aaa\x7faaaa", true},
		{"last byte only tail", "abcdefgh" + "ijklmnop" + "q", false},
		{"NUL exactly at tail pos", "abcdefghijklmnopqrstuv\x00", true},
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

// TestValidator_ValidateSetKey pins the programmatic-key contract in one
// table: with the default configuration Set accepts the structured-format key
// space (IsValidJSONKey — a superset of the .env pattern that adds dots,
// hyphens and at-signs), a custom KeyPattern governs instead of the default
// scan, and shape, length, ASCII and policy rules still reject everything
// invalid.
func TestValidator_ValidateSetKey(t *testing.T) {
	tests := []struct {
		name   string
		cfg    ValidatorConfig
		accept []string
		reject []string
	}{
		{
			name: "default config accepts the structured-format key space",
			cfg:  ValidatorConfig{MaxKeyLength: 64, MaxValueLength: 128},
			accept: []string{
				"HOST",              // plain .env-shaped key
				"DB_PORT",           // underscore
				"APNEWS.COM",        // dotted (YAML/JSON file-shaped)
				"service.host-name", // dots + hyphen, lowercase (uppercased by Set callers as needed)
				"user@example",      // at-sign
				"v2",                // leading digit with letters: not index-like
			},
			reject: []string{
				"",                      // empty
				"BAD KEY",               // space
				"A=B",                   // equals sign
				"A[0]",                  // brackets (array-index ambiguity)
				"123",                   // short pure-numeric (array-index-like)
				"12345",                 // long pure-numeric: also index-like (ResolvePath resolves segments up to maxSafeIndex)
				"999999999",             // nine-digit pure-numeric, same class
				"höst",                  // non-ASCII (homograph)
				strings.Repeat("K", 65), // exceeds MaxKeyLength
			},
		},
		{
			name:   "custom pattern governs",
			cfg:    ValidatorConfig{KeyPattern: regexp.MustCompile(`^[A-Z_]+$`), MaxKeyLength: 64},
			accept: []string{"GOOD_KEY"},
			reject: []string{"A.B"}, // dotted key fails the custom pattern
		},
		{
			// The default forbidden-key list is merged in by the component
			// factory; a bare NewValidator only knows what it is given.
			name:   "policy enforced case-insensitively",
			cfg:    ValidatorConfig{MaxKeyLength: 64, ForbiddenKeys: []string{"PATH", "LD_PRELOAD"}},
			accept: []string{"APNEWS.COM"}, // policy permits dotted keys
			reject: []string{"PATH", "ld_preload"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator(tt.cfg)
			for _, key := range tt.accept {
				if err := v.ValidateSetKey(key); err != nil {
					t.Errorf("ValidateSetKey(%q) error = %v, want nil", key, err)
				}
			}
			for _, key := range tt.reject {
				if err := v.ValidateSetKey(key); err == nil {
					t.Errorf("ValidateSetKey(%q) = nil, want error", key)
				}
			}
		})
	}
}

// TestValidator_ValidateKeyPolicy verifies the policy-only check used by the
// structured (JSON/YAML) parsers: it enforces the allowed/forbidden key lists
// without applying the .env key pattern, so format-specific keys (dots,
// hyphens) pass while dangerous keys are still rejected.
func TestValidator_ValidateKeyPolicy(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		ForbiddenKeys:  []string{"PATH", "LD_PRELOAD"},
		MaxKeyLength:   DefaultMaxKeyLength,
		MaxValueLength: DefaultMaxValueLength,
	})

	if err := v.ValidateKeyPolicy("PATH"); err == nil {
		t.Error("ValidateKeyPolicy(PATH) should be rejected by forbidden list")
	} else if !errors.Is(err, ErrForbiddenKey) {
		t.Errorf("ValidateKeyPolicy(PATH) should match ErrForbiddenKey, got %v", err)
	}
	if err := v.ValidateKeyPolicy("ld_preload"); err == nil {
		t.Error("ValidateKeyPolicy should be case-insensitive")
	}
	if err := v.ValidateKeyPolicy("service.host-name"); err != nil {
		t.Errorf("ValidateKeyPolicy(format-specific key) should pass, got %v", err)
	}

	// AllowedKeys policy: anything outside the list is rejected.
	allowed := NewValidator(ValidatorConfig{
		AllowedKeys:    []string{"HOST", "PORT"},
		MaxKeyLength:   DefaultMaxKeyLength,
		MaxValueLength: DefaultMaxValueLength,
	})
	if err := allowed.ValidateKeyPolicy("HOST"); err != nil {
		t.Errorf("ValidateKeyPolicy(HOST) should pass allowed list, got %v", err)
	}
	if err := allowed.ValidateKeyPolicy("OTHER"); err == nil {
		t.Error("ValidateKeyPolicy(OTHER) should be rejected by allowed list")
	}

	// Full ValidateKey still applies pattern + policy.
	if err := v.ValidateKey("PATH"); err == nil {
		t.Error("ValidateKey(PATH) should be rejected")
	}
}

// ============================================================================
// ValidateValueBasic Tests
// ============================================================================

// TestValidator_ValidateValueBasic pins the fail-closed baseline used by the
// Set and structured-load paths when ValidateValues is disabled (D-002/M-1):
// for the built-in validator it equals the full check — length and control
// characters always, and the UTF-8 check when the validator is configured
// with ValidateUTF8 (the flag governs it alone, on every path).
func TestValidator_ValidateValueBasic(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxValueLength: 8})

	if err := v.ValidateValueBasic("short"); err != nil {
		t.Errorf("ValidateValueBasic(short) = %v, want nil", err)
	}
	if err := v.ValidateValueBasic("bad\x00value"); err == nil {
		t.Error("ValidateValueBasic should reject NUL bytes")
	}
	if err := v.ValidateValueBasic("\x01ctrl"); err == nil {
		t.Error("ValidateValueBasic should reject control characters")
	}
	if err := v.ValidateValueBasic("too long value"); err == nil {
		t.Error("ValidateValueBasic should reject values over MaxValueLength")
	}
	if err := v.ValidateValueBasic("\xffbad"); err != nil {
		t.Errorf("ValidateValueBasic(invalid UTF-8) = %v, want nil (validator not configured for UTF-8)", err)
	}

	// With ValidateUTF8 configured, the baseline honors it — keeping the
	// switch-off write paths at parity with the .env parse path, where the
	// UTF-8 check follows its own flag alone.
	utf8v := NewValidator(ValidatorConfig{MaxValueLength: 8, ValidateUTF8: true})
	if err := utf8v.ValidateValueBasic("\xffbad"); err == nil {
		t.Error("ValidateValueBasic should reject invalid UTF-8 when configured with ValidateUTF8")
	}
	if err := utf8v.ValidateValueBasic("ok"); err != nil {
		t.Errorf("ValidateValueBasic(valid UTF-8) = %v, want nil", err)
	}
}
