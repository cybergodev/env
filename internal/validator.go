// Provides input validation for environment variable keys and values.

package internal

import (
	"encoding/binary"
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
// It truncates long values the same way as MaskInString.
func defaultMaskSensitive(s string) string {
	return MaskInString(s)
}

// actionKeyAccess is the SecurityError.Action value used when a key is
// rejected by the forbidden-keys or allowed-keys policy. SecurityError.Is
// matches ErrForbiddenKey for this action.
const actionKeyAccess = "key_access"

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
	// (e.g., ℌOST, U+210C, looks similar to HOST but would bypass ASCII-only
	// validation). For the default pattern this check is folded into the same
	// scan as the pattern validation — every byte >= 0x80 fails the pattern's
	// character classes anyway, so a separate pass would duplicate work. The
	// ascii_only rule still takes precedence over pattern, matching the
	// previous scan-order behavior. Custom patterns keep the dedicated scan
	// because a user-supplied pattern may permit non-ASCII characters.
	if v.useDefaultKeyCheck {
		valid, hasNonASCII := validateDefaultKeyScan(key)
		if hasNonASCII {
			return v.newValidationError("key", key, "ascii_only", "key contains non-ASCII characters")
		}
		if !valid {
			return v.newValidationError("key", key, "pattern", "key does not match required pattern")
		}
	} else {
		if err := v.validateCustomKeyShape(key); err != nil {
			return err
		}
	}

	return v.ValidateKeyPolicy(key)
}

// validateCustomKeyShape checks a key against a user-supplied KeyPattern,
// including the non-ASCII homograph scan that a custom pattern may not
// itself reject. Shared by ValidateKey and ValidateSetKey.
func (v *Validator) validateCustomKeyShape(key string) error {
	for i := 0; i < len(key); i++ {
		if key[i] >= 0x80 {
			return v.newValidationError("key", key, "ascii_only", "key contains non-ASCII characters")
		}
	}
	if v.keyPattern != nil && !v.keyPattern.MatchString(key) {
		return v.newValidationError("key", key, "pattern", "key does not match required pattern")
	}
	return nil
}

// ValidateSetKey validates a programmatically-set key (Loader.Set).
//
// The store legitimately holds keys containing dots and hyphens loaded from
// structured files (governed by IsValidJSONKey); Set accepts that same key
// space so programmatic values can mirror file data — with the default
// configuration the .env pattern (which rejects dots) no longer makes Set
// stricter than file loads. A user-supplied KeyPattern expresses a stricter
// intent and keeps governing programmatic keys unchanged. The
// allowed/forbidden key policy is enforced exactly like ValidateKey.
func (v *Validator) ValidateSetKey(key string) error {
	if len(key) == 0 {
		return v.newValidationError("key", key, "non_empty", "key cannot be empty")
	}
	if len(key) > v.maxKeyLength {
		return v.newValidationError("key", key, "max_length", "key exceeds maximum length")
	}

	if v.useDefaultKeyCheck {
		// Structured-format key shape: letters, digits, '_', '@', '-', '.'
		// (the default .env pattern is a strict subset of it). The character
		// class rejects non-ASCII bytes, covering the homograph scan, and
		// array-index-like/bracket keys stay excluded.
		if !IsValidJSONKey(key) {
			return v.newValidationError("key", key, "pattern", "key does not match required pattern")
		}
	} else if err := v.validateCustomKeyShape(key); err != nil {
		return err
	}

	return v.ValidateKeyPolicy(key)
}

// ValidateKeyPolicy checks a key against the allowed-keys / forbidden-keys
// policy only, skipping the pattern and length checks that are format-specific.
//
// Structured formats (JSON/YAML) use their own key pattern (IsValidJSONKey,
// which permits dots, hyphens and other characters the .env pattern rejects)
// but MUST still enforce the same security policy as the .env parser —
// otherwise a config.json containing "PATH" or "LD_PRELOAD" would bypass the
// default forbidden-keys list and be applied to the process environment.
func (v *Validator) ValidateKeyPolicy(key string) error {
	// Only compute uppercase key if we need to check lists
	if len(v.allowedKeys) == 0 && len(v.forbiddenKeys) == 0 {
		return nil
	}
	upperKey := ToUpperASCII(key)

	// Check if in allowed list (if specified)
	if len(v.allowedKeys) > 0 && !v.allowedKeys[upperKey] {
		return v.newSecurityError(actionKeyAccess, "key not in allowed list", key)
	}

	// Check forbidden list
	if v.forbiddenKeys[upperKey] {
		return v.newSecurityError(actionKeyAccess, "key is forbidden", key)
	}

	return nil
}

// ValidateValueBasic enforces the always-on value baseline for write paths
// whose ValidateValues switch is off (Set, JSON/YAML loads), keeping them at
// exact parity with the .env line parser — which runs ValidateValue
// unconditionally. Every check the built-in validator can perform is part of
// that baseline: maximum value length, control-character rejection, and
// UTF-8 validation when the validator was configured with ValidateUTF8 (the
// UTF-8 check follows its own flag alone, on every path, exactly as the .env
// parser has always honored it). It therefore delegates to ValidateValue, so
// the two cannot drift apart; ValidateValues remains effective only for
// custom validators without this capability, which skip value validation
// entirely when the switch is off.
func (v *Validator) ValidateValueBasic(value string) error {
	return v.ValidateValue(value)
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
// An earlier 8-byte-at-a-time unrolling was removed for readability, then
// reinstated (in the guarded form below) after CPU profiles of the parse and
// Set hot paths showed this scan as the largest single flat consumer
// (~10% of .env parse time). The word-at-a-time prefilter only decides
// whether a chunk is provably clean; any chunk it flags is re-checked by the
// simple byte loop, so the disallowed set and the error reporting live in
// exactly one place (badCharTable / badCharError) and correctness does not
// depend on the bit tricks.
func validateValueChars(value string) error {
	// Word-at-a-time prefilter, 8 bytes per iteration. A chunk is provably
	// clean when no lane holds a byte < 0x20 and no lane holds 0x7F (DEL);
	// every other byte — including all of 0x80..0xFF — is allowed.
	//
	//   hasLess: (w - 0x20..) &^ w & 0x80..  — per-lane borrow-out detection
	//     ("has byte less than n" from Bit Twiddling Hacks). Inter-lane
	//     borrows can only add flags (false positives), never clear a
	//     genuine one, so flagged chunks fall through to the exact loop.
	//   hasDel:  zero-byte detector applied to w ^ 0x7F.., which turns DEL
	//     bytes into zero bytes.
	//
	// The []byte conversion does not allocate: LittleEndian.Uint64 is
	// inlinable and the converted slice does not escape.
	i := 0
	for ; i+8 <= len(value); i += 8 {
		w := binary.LittleEndian.Uint64([]byte(value[i : i+8]))
		const hi = 0x8080808080808080
		hasLess := (w - 0x2020202020202020) &^ w & hi
		del := w ^ 0x7F7F7F7F7F7F7F7F
		hasDel := (del - 0x0101010101010101) &^ del & hi
		if hasLess|hasDel != 0 {
			for j := i; j < i+8; j++ {
				if badCharTable[value[j]] != 0 {
					return badCharError(value[j], j)
				}
			}
		}
	}
	for ; i < len(value); i++ {
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
// Test probe: production paths call the injected IsSensitive func (or the
// auditor's copy) directly; this accessor exists so tests can verify the
// injected predicate without reaching into struct fields.
func (v *Validator) IsSensitive(key string) bool {
	return v.isSensitive(key)
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
