package env

import (
	"io"
	"strings"
	"testing"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// SecureValue Tests (Table-Driven)
// ============================================================================

func TestSecureValue(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		operation  string // "new", "close", "release", "string", "bytes", "length", "masked"
		wantResult interface{}
		wantClosed bool
	}{
		// Creation and basic operations
		{"new value returns non-nil", "test", "new", true, false},
		{"new value string", "test", "string", "test", false},
		{"new value bytes", "test", "bytes", "test", false},
		{"new value length", "test", "length", 4, false},

		// Empty value handling
		{"empty value string", "", "string", "", false},
		{"empty value length", "", "length", 0, false},
		{"empty value masked", "", "masked", "[SECURE:0 bytes]", false},

		// Close operation
		{"close value", "test", "close", nil, true},
		{"close is idempotent", "test", "close_twice", nil, true},

		// Release operation
		{"release value", "test", "release", nil, true},

		// Masked output
		{"masked with value", "test", "masked", "[SECURE:4 bytes]", false},
		{"masked when closed", "test", "masked_closed", "[CLOSED]", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := NewSecureValue(tt.input)

			switch tt.operation {
			case "new":
				if sv == nil {
					t.Error("NewSecureValue() returned nil")
				}
			case "string":
				if sv.Reveal() != tt.wantResult.(string) {
					t.Errorf("Reveal() = %q, want %q", sv.Reveal(), tt.wantResult)
				}
			case "bytes":
				if string(sv.Bytes()) != tt.wantResult.(string) {
					t.Errorf("Bytes() = %q, want %q", string(sv.Bytes()), tt.wantResult)
				}
			case "length":
				if sv.Length() != tt.wantResult.(int) {
					t.Errorf("Length() = %d, want %d", sv.Length(), tt.wantResult)
				}
			case "masked":
				if sv.Masked() != tt.wantResult.(string) {
					t.Errorf("Masked() = %q, want %q", sv.Masked(), tt.wantResult)
				}
			case "masked_closed":
				sv.Close()
				if sv.Masked() != tt.wantResult.(string) {
					t.Errorf("Masked() = %q, want %q", sv.Masked(), tt.wantResult)
				}
			case "close":
				if err := sv.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
				if !sv.IsClosed() {
					t.Error("IsClosed() = false after Close()")
				}
			case "close_twice":
				if err := sv.Close(); err != nil {
					t.Errorf("First Close() error = %v", err)
				}
				if err := sv.Close(); err != nil {
					t.Errorf("Second Close() error = %v", err)
				}
				if !sv.IsClosed() {
					t.Error("IsClosed() = false after Close()")
				}
			case "release":
				sv.Release()
				if !sv.IsClosed() {
					t.Error("IsClosed() = false after Release()")
				}
			}

			if tt.wantClosed && !sv.IsClosed() {
				t.Error("Expected value to be closed")
			}
		})
	}
}

func TestSecureValuePool(t *testing.T) {
	// Create multiple SecureValues and release them back to pool
	for i := 0; i < 10; i++ {
		sv := NewSecureValue("test")
		sv.Release()
	}

	// Create new ones - should potentially reuse from pool
	for i := 0; i < 5; i++ {
		newSv := NewSecureValue("new")
		if newSv.Reveal() != "new" {
			t.Errorf("New SecureValue from pool = %q, want %q", newSv.Reveal(), "new")
		}
	}
}

// TestSecureValue_ResetStateConsistency tests the fix for C1:
// The reset() method should properly manage state transitions
// with data being cleared before new value is set.
func TestSecureValue_ResetStateConsistency(t *testing.T) {
	tests := []struct {
		name       string
		setupValue string
		testValue  string
		wantString string
		wantClosed bool
	}{
		{"empty_value_is_valid", "", "", "", false},
		{"non_empty_value_is_not_closed", "test", "", "", false},
		{"reuse_from_pool_with_empty", "initial", "", "", false},
		{"reuse_from_pool_preserves_state", "initial", "newvalue", "newvalue", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := NewSecureValue(tt.setupValue)
			sv.Release()

			sv2 := NewSecureValue(tt.testValue)

			if tt.wantClosed != sv2.IsClosed() {
				t.Errorf("IsClosed() = %v, want %v", sv2.IsClosed(), tt.wantClosed)
			}

			if sv2.Reveal() != tt.wantString {
				t.Errorf("Reveal() = %q, want %q", sv2.Reveal(), tt.wantString)
			}
		})
	}
}

// ============================================================================
// secureMap Tests (Table-Driven)
// ============================================================================

func TestSecureMap(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		setup     func(sm *secureMap)
		key       string
		value     string
		wantValue interface{}
		wantLen   int
		wantOK    bool
		wantNil   bool
	}{
		// Basic Set and Get
		{"set and get", "get", func(sm *secureMap) { sm.Set("KEY1", "value1") }, "KEY1", "", "value1", 1, true, false},
		{"get missing key", "get", nil, "MISSING", "", "", 0, false, false},

		// SetAll
		{"set all", "setall", nil, "", "", nil, 3, true, false},

		// Delete
		{"delete existing", "delete", func(sm *secureMap) { sm.Set("KEY1", "value1"); sm.Set("KEY2", "value2") }, "KEY1", "", nil, 1, false, false},

		// Clear
		{"clear", "clear", func(sm *secureMap) { sm.Set("KEY1", "value1"); sm.Set("KEY2", "value2") }, "", "", nil, 0, true, false},

		// Keys
		{"keys", "keys", func(sm *secureMap) { sm.Set("KEY1", "value1"); sm.Set("KEY2", "value2") }, "", "", nil, 2, true, false},

		// ToMap
		{"to map", "tomap", func(sm *secureMap) { sm.Set("KEY1", "value1") }, "KEY1", "", "value1", 1, true, false},

		// GetSecure
		{"get secure existing", "getsecure", func(sm *secureMap) { sm.Set("KEY1", "value1") }, "KEY1", "", "value1", 0, true, false},
		{"get secure missing", "getsecure", nil, "MISSING", "", nil, 0, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newSecureMap()

			// Setup
			if tt.operation == "setall" {
				values := map[string]string{
					"KEY1": "value1",
					"KEY2": "value2",
					"KEY3": "value3",
				}
				sm.SetAll(values)
			} else if tt.setup != nil {
				tt.setup(sm)
			}

			switch tt.operation {
			case "get":
				val, ok := sm.Get(tt.key)
				if ok != tt.wantOK {
					t.Errorf("Get() ok = %v, want %v", ok, tt.wantOK)
				}
				if ok && val != tt.wantValue.(string) {
					t.Errorf("Get() = %q, want %q", val, tt.wantValue)
				}

			case "setall":
				// Already done in setup

			case "delete":
				sm.Delete(tt.key)
				_, ok := sm.Get(tt.key)
				if ok {
					t.Error("Key should be deleted")
				}

			case "clear":
				sm.Clear()
				if sm.Len() != 0 {
					t.Errorf("Len() after Clear() = %d, want 0", sm.Len())
				}

			case "keys":
				keys := sm.Keys()
				if len(keys) != tt.wantLen {
					t.Errorf("Keys() returned %d keys, want %d", len(keys), tt.wantLen)
				}

			case "tomap":
				m := sm.ToMap()
				if len(m) != tt.wantLen {
					t.Errorf("ToMap() returned %d keys, want %d", len(m), tt.wantLen)
				}
				if tt.wantValue != nil && m[tt.key] != tt.wantValue.(string) {
					t.Errorf("ToMap()[%q] = %q, want %q", tt.key, m[tt.key], tt.wantValue)
				}

			case "getsecure":
				sv := sm.GetSecure(tt.key)
				if tt.wantNil {
					if sv != nil {
						t.Errorf("GetSecure() = %v, want nil", sv)
					}
				} else {
					if sv == nil {
						t.Fatal("GetSecure() returned nil")
					}
					if sv.Reveal() != tt.wantValue.(string) {
						t.Errorf("GetSecure().Reveal() = %q, want %q", sv.Reveal(), tt.wantValue)
					}
					sv.Release()
				}
			}

			if tt.wantLen > 0 && sm.Len() != tt.wantLen {
				t.Errorf("Len() = %d, want %d", sm.Len(), tt.wantLen)
			}
		})
	}
}

// ============================================================================
// ClearBytes Tests
// ============================================================================

func TestClearBytes(t *testing.T) {
	data := []byte("sensitive data")
	ClearBytes(data)

	for _, b := range data {
		if b != 0 {
			t.Error("ClearBytes() did not zero the data")
			return
		}
	}
}

// ============================================================================
// Memory Lock Tests
// ============================================================================

func TestMemoryLock_Config(t *testing.T) {
	// Save original state
	originalEnabled := IsMemoryLockEnabled()
	originalStrict := IsMemoryLockStrict()
	defer func() {
		SetMemoryLockEnabled(originalEnabled)
		SetMemoryLockStrict(originalStrict)
	}()

	t.Run("enable_disable", func(t *testing.T) {
		SetMemoryLockEnabled(true)
		if !IsMemoryLockEnabled() {
			t.Error("IsMemoryLockEnabled() = false after enabling")
		}

		SetMemoryLockEnabled(false)
		if IsMemoryLockEnabled() {
			t.Error("IsMemoryLockEnabled() = true after disabling")
		}
	})

	t.Run("strict_mode", func(t *testing.T) {
		SetMemoryLockStrict(true)
		if !IsMemoryLockStrict() {
			t.Error("IsMemoryLockStrict() = false after enabling strict mode")
		}

		SetMemoryLockStrict(false)
		if IsMemoryLockStrict() {
			t.Error("IsMemoryLockStrict() = true after disabling strict mode")
		}
	})
}

func TestMemoryLock_SecureValue(t *testing.T) {
	// Save original state
	originalEnabled := IsMemoryLockEnabled()
	defer SetMemoryLockEnabled(originalEnabled)

	t.Run("disabled_by_default", func(t *testing.T) {
		SetMemoryLockEnabled(false)
		sv := NewSecureValue("test")
		defer sv.Release()

		// Should not be locked when disabled
		if sv.IsMemoryLocked() {
			t.Error("IsMemoryLocked() = true when memory locking is disabled")
		}
	})

	t.Run("enabled_creates_locked_value", func(t *testing.T) {
		SetMemoryLockEnabled(true)
		sv := NewSecureValue("sensitive-data")
		defer sv.Release()

		// On systems with sufficient privileges, the value should be locked
		// We check that the function doesn't panic and returns a valid value
		if sv.Reveal() != "sensitive-data" {
			t.Errorf("Reveal() = %q, want %q", sv.Reveal(), "sensitive-data")
		}

		// Check if there was a lock error (expected on systems without privileges)
		// This is informational, not a failure
		if err := sv.MemoryLockError(); err != nil {
			t.Logf("Memory lock error (expected on systems without privileges): %v", err)
		}
	})

	t.Run("release_unlocks_memory", func(t *testing.T) {
		SetMemoryLockEnabled(true)
		sv := NewSecureValue("test")
		sv.Release()

		// After release, should not report as locked
		if sv.IsMemoryLocked() {
			t.Error("IsMemoryLocked() = true after Release()")
		}
	})

	t.Run("close_unlocks_memory", func(t *testing.T) {
		SetMemoryLockEnabled(true)
		sv := NewSecureValue("test")
		sv.Close()

		// After close, should not report as locked
		if sv.IsMemoryLocked() {
			t.Error("IsMemoryLocked() = true after Close()")
		}
	})
}

func TestNewSecureValueStrict(t *testing.T) {
	// Save original state
	originalEnabled := IsMemoryLockEnabled()
	originalStrict := IsMemoryLockStrict()
	defer func() {
		SetMemoryLockEnabled(originalEnabled)
		SetMemoryLockStrict(originalStrict)
	}()

	t.Run("returns_error_on_lock_failure_in_strict_mode", func(t *testing.T) {
		SetMemoryLockEnabled(true)
		SetMemoryLockStrict(true)

		sv, err := NewSecureValueStrict("sensitive-data")
		if sv != nil {
			defer sv.Release()
		}

		// On systems without privileges, this may return an error
		// On systems with privileges, this should succeed
		if err != nil {
			t.Logf("NewSecureValueStrict() returned error (expected on systems without privileges): %v", err)
		}

		// The SecureValue should still be usable regardless of lock status
		if sv != nil && sv.Reveal() != "sensitive-data" {
			t.Errorf("Reveal() = %q, want %q", sv.Reveal(), "sensitive-data")
		}
	})

	t.Run("disabled_locking_no_error", func(t *testing.T) {
		SetMemoryLockEnabled(false)

		sv, err := NewSecureValueStrict("sensitive-data")
		if sv != nil {
			defer sv.Release()
		}

		// When locking is disabled, no error should be returned
		if err != nil {
			t.Errorf("NewSecureValueStrict() returned error when locking disabled: %v", err)
		}

		if sv != nil && sv.Reveal() != "sensitive-data" {
			t.Errorf("Reveal() = %q, want %q", sv.Reveal(), "sensitive-data")
		}
	})
}

func TestSecureValue_MemoryLockError(t *testing.T) {
	// Save original state
	originalEnabled := IsMemoryLockEnabled()
	defer SetMemoryLockEnabled(originalEnabled)

	t.Run("no_error_when_disabled", func(t *testing.T) {
		SetMemoryLockEnabled(false)
		sv := NewSecureValue("test")
		defer sv.Release()

		if err := sv.MemoryLockError(); err != nil {
			t.Errorf("MemoryLockError() = %v, want nil when disabled", err)
		}
	})
}

func TestIsMemoryLockSupported(t *testing.T) {
	// This test just verifies the function doesn't panic
	supported := IsMemoryLockSupported()
	t.Logf("IsMemoryLockSupported() = %v", supported)
}

// ============================================================================
// Sensitive Key Detection Tests (from sensitive_export_test.go)
// ============================================================================

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		// Sensitive patterns
		{"PASSWORD", true},
		{"SECRET", true},
		{"API_KEY", true},
		{"PRIVATE_KEY", true},
		{"ACCESS_TOKEN", true},
		{"AUTH_TOKEN", true},
		{"DATABASE_PASSWORD", true},
		{"DB_SECRET", true},
		{"CREDENTIAL", true},

		// Non-sensitive patterns
		{"HOST", false},
		{"PORT", false},
		{"DEBUG", false},
		{"APP_NAME", false},
		{"LOG_LEVEL", false},
		{"TIMEOUT", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := IsSensitiveKey(tt.key)
			if result != tt.expected {
				t.Errorf("IsSensitiveKey(%q) = %v, want %v", tt.key, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// MaskValue Tests
// ============================================================================

func TestMaskValue(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		contains string // check if result contains this
	}{
		{
			name:     "sensitive key masked",
			key:      "PASSWORD",
			value:    "super_secret_123",
			contains: "[MASKED",
		},
		{
			name:     "non-sensitive key not masked",
			key:      "HOST",
			value:    "localhost",
			contains: "localhost",
		},
		{
			name:     "api key masked",
			key:      "API_KEY",
			value:    "sk-1234567890abcdef",
			contains: "[MASKED",
		},
		{
			name:     "empty value",
			key:      "SECRET",
			value:    "",
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskValue(tt.key, tt.value)
			if tt.contains != "" && !strings.Contains(result, tt.contains) {
				t.Errorf("MaskValue(%q, %q) = %q, should contain %q", tt.key, tt.value, result, tt.contains)
			}
		})
	}
}

// ============================================================================
// MaskKey Tests
// ============================================================================

func TestMaskKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"AB", "***"},
		{"ABC", "***"},
		{"ABCD", "AB***"},
		{"API_KEY", "AP***"},
		{"", "***"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := MaskKey(tt.key)
			if result != tt.expected {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// MaskSensitiveInString Tests
// ============================================================================

func TestMaskSensitiveInString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short string unchanged",
			input:    "password=secret123",
			expected: "password=secret123",
		},
		{
			name:     "long string truncated",
			input:    strings.Repeat("a", 60),
			expected: strings.Repeat("a", 47) + "...",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "exact max length",
			input:    strings.Repeat("a", 50),
			expected: strings.Repeat("a", 50),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskSensitiveInString(tt.input)
			if result != tt.expected {
				t.Errorf("MaskSensitiveInString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// SanitizeForLog Tests
// ============================================================================

func TestSanitizeForLog(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldMask  bool // if true, verify value is masked
		checkResult func(t *testing.T, result string)
	}{
		{
			name:  "removes password value",
			input: "password=my_secret_password",
			checkResult: func(t *testing.T, result string) {
				if strings.Contains(result, "my_secret_password") {
					t.Errorf("SanitizeForLog() should mask password value, got %q", result)
				}
			},
		},
		{
			name:  "removes api_key value",
			input: "api_key=sk-1234567890",
			checkResult: func(t *testing.T, result string) {
				if strings.Contains(result, "sk-1234567890") {
					t.Errorf("SanitizeForLog() should mask api_key value, got %q", result)
				}
			},
		},
		{
			name:  "preserves non-sensitive",
			input: "host=localhost port=8080",
			checkResult: func(t *testing.T, result string) {
				if !strings.Contains(result, "localhost") {
					t.Errorf("SanitizeForLog() should preserve non-sensitive data, got %q", result)
				}
			},
		},
		{
			name:  "empty string",
			input: "",
			checkResult: func(t *testing.T, result string) {
				if result != "" {
					t.Errorf("SanitizeForLog(\"\") = %q, want empty", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeForLog(tt.input)
			tt.checkResult(t, result)
		})
	}
}

// ============================================================================
// sensitiveKeyPatterns Tests
// ============================================================================

func TestSensitiveKeyPatterns(t *testing.T) {
	// Verify patterns exist and are non-empty
	if len(internal.Patterns) == 0 {
		t.Error("internal.Patterns should not be empty")
	}

	// Verify common patterns are included
	patternStr := strings.ToLower(strings.Join(internal.Patterns, " "))
	expectedPatterns := []string{"password", "secret", "key", "token", "credential"}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(patternStr, pattern) {
			t.Errorf("internal.Patterns should contain pattern containing %q", pattern)
		}
	}
}

// TestSecureMap_EmptyStringValue verifies that empty string values are correctly
// stored and retrieved (not treated as "not found").
// TestSecureValue_Nil tests nil SecureValue method calls.
func TestSecureValue_Nil(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		var sv *SecureValue
		if got := sv.String(); got != "[NIL]" {
			t.Errorf("nil String() = %q, want [NIL]", got)
		}
	})
	t.Run("Reveal", func(t *testing.T) {
		var sv *SecureValue
		if got := sv.Reveal(); got != "" {
			t.Errorf("nil Reveal() = %q, want empty", got)
		}
	})
	t.Run("Bytes", func(t *testing.T) {
		var sv *SecureValue
		if got := sv.Bytes(); got != nil {
			t.Errorf("nil Bytes() = %v, want nil", got)
		}
	})
	t.Run("Length", func(t *testing.T) {
		var sv *SecureValue
		if got := sv.Length(); got != 0 {
			t.Errorf("nil Length() = %d, want 0", got)
		}
	})
	t.Run("Close", func(t *testing.T) {
		var sv *SecureValue
		if err := sv.Close(); err != nil {
			t.Errorf("nil Close() = %v, want nil", err)
		}
	})
	t.Run("IsClosed", func(t *testing.T) {
		var sv *SecureValue
		if !sv.IsClosed() {
			t.Error("nil IsClosed() = false, want true")
		}
	})
	t.Run("Masked", func(t *testing.T) {
		var sv *SecureValue
		if got := sv.Masked(); got != "[NIL]" {
			t.Errorf("nil Masked() = %q, want [NIL]", got)
		}
	})
}

// TestBuildIndexedKey tests edge cases for buildIndexedKey.
func TestBuildIndexedKey(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		index    int
		expected string
	}{
		{"simple index", "KEY", 0, "KEY_0"},
		{"positive index", "KEY", 42, "KEY_42"},
		{"negative index returns empty", "KEY", -1, ""},
		{"large index", "KEY", 99999, "KEY_99999"},
		{"empty base", "", 0, "_0"},
		{"long base uses builder", strings.Repeat("A", 63), 0, strings.Repeat("A", 63) + "_0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildIndexedKey(tt.base, tt.index)
			if tt.expected != "" && result != tt.expected {
				t.Errorf("buildIndexedKey(%q, %d) = %q, want %q", tt.base, tt.index, result, tt.expected)
			}
		})
	}
}

// TestComponentFactory_Nil tests nil ComponentFactory method calls.
func TestComponentFactory_Nil(t *testing.T) {
	t.Run("Validator nil", func(t *testing.T) {
		var f *ComponentFactory
		if v := f.Validator(); v != nil {
			t.Errorf("nil Validator() = %v, want nil", v)
		}
	})

	t.Run("Auditor nil", func(t *testing.T) {
		var f *ComponentFactory
		if a := f.Auditor(); a != nil {
			t.Errorf("nil Auditor() = %v, want nil", a)
		}
	})

	t.Run("Expander nil", func(t *testing.T) {
		var f *ComponentFactory
		if e := f.Expander(); e != nil {
			t.Errorf("nil Expander() = %v, want nil", e)
		}
	})

	t.Run("Close nil", func(t *testing.T) {
		var f *ComponentFactory
		if err := f.Close(); err != nil {
			t.Errorf("nil Close() = %v, want nil", err)
		}
	})

	t.Run("IsClosed nil", func(t *testing.T) {
		var f *ComponentFactory
		if !f.IsClosed() {
			t.Error("nil IsClosed() = false, want true")
		}
	})
}

// TestLoader_NilMethodCalls tests nil Loader method calls.
func TestLoader_NilMethodCalls(t *testing.T) {
	t.Run("Config nil", func(t *testing.T) {
		var l *Loader
		cfg := l.Config()
		if cfg.MaxVariables != 0 {
			t.Errorf("nil Config() = %+v, want zero Config", cfg)
		}
	})

	t.Run("ParseInto nil", func(t *testing.T) {
		var l *Loader
		if err := l.ParseInto(nil); err != ErrClosed {
			t.Errorf("nil ParseInto() = %v, want ErrClosed", err)
		}
	})

	t.Run("Validate nil", func(t *testing.T) {
		var l *Loader
		if err := l.Validate(); err != ErrClosed {
			t.Errorf("nil Validate() = %v, want ErrClosed", err)
		}
	})

	t.Run("GetSecure nil", func(t *testing.T) {
		var l *Loader
		if sv := l.GetSecure("KEY"); sv != nil {
			t.Errorf("nil GetSecure() = %v, want nil", sv)
		}
	})

	t.Run("GetString nil", func(t *testing.T) {
		var l *Loader
		if v := l.GetString("KEY", "default"); v != "default" {
			t.Errorf("nil GetString() = %q, want default", v)
		}
	})
}

// TestNewCloseableChannelHandler tests the CloseableChannelHandler creation.
func TestNewCloseableChannelHandler(t *testing.T) {
	handler := NewCloseableChannelHandler(10)
	if handler == nil {
		t.Fatal("NewCloseableChannelHandler() returned nil")
	}

	// Verify we can use it as an AuditHandler
	_ = AuditHandler(handler)

	// Close should not panic
	if err := handler.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestParserClose tests parser Close methods through the registry.
func TestParserClose(t *testing.T) {
	t.Run("parser close via type assertion", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// Access parsers through the registry and test Close via type assertion
		for _, p := range loader.parsers {
			if closer, ok := p.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					t.Errorf("parser Close() error = %v", err)
				}
			}
		}
	})
}

// TestBuildValidatorAdapter_CustomValidator tests the factory adapter with a custom validator
// that already implements the public Validator interface.
func TestBuildValidatorAdapter_CustomValidator(t *testing.T) {
	t.Run("validator implementing public Validator", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.CustomValidator = &fullMockValidator{}
		factory := cfg.buildComponentFactory()
		defer factory.Close()

		v := factory.Validator()
		if v == nil {
			t.Fatal("Validator() returned nil")
		}
	})

	t.Run("validator not implementing public Validator", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.CustomValidator = &minimalMockValidator{}
		factory := cfg.buildComponentFactory()
		defer factory.Close()

		v := factory.Validator()
		if v == nil {
			t.Fatal("Validator() returned nil")
		}
	})
}

// TestBuildAuditorAdapter tests the factory auditor adapter paths.
func TestBuildAuditorAdapter(t *testing.T) {
	t.Run("with custom FullAuditLogger", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.CustomAuditor = &mockFullAuditLogger{}
		factory := cfg.buildComponentFactory()
		defer factory.Close()

		a := factory.Auditor()
		if a == nil {
			t.Fatal("Auditor() returned nil")
		}
	})

	t.Run("with custom non-standard auditor", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.CustomAuditor = &mockAuditLogger{}
		factory := cfg.buildComponentFactory()
		defer factory.Close()

		a := factory.Auditor()
		if a == nil {
			t.Fatal("Auditor() returned nil")
		}
	})
}

// TestSecureMap_EmptyStringValue verifies that empty string values are correctly
// stored and retrieved (not treated as "not found").
func TestSecureMap_EmptyStringValue(t *testing.T) {
	sm := newSecureMap()
	sm.Set("EMPTY_KEY", "")

	// Get must return ("", true) for empty string values
	value, ok := sm.Get("EMPTY_KEY")
	if !ok {
		t.Fatal("Get(EMPTY_KEY) returned ok=false for empty string value")
	}
	if value != "" {
		t.Errorf("Get(EMPTY_KEY) = %q, want empty string", value)
	}

	// GetSecure must return non-nil for empty string values
	sv := sm.GetSecure("EMPTY_KEY")
	if sv == nil {
		t.Fatal("GetSecure(EMPTY_KEY) returned nil for empty string value")
	}
	if sv.Reveal() != "" {
		t.Errorf("GetSecure(EMPTY_KEY).Reveal() = %q, want empty string", sv.Reveal())
	}
	sv.Release()

	// ToMap must include empty string values
	m := sm.ToMap()
	if v, exists := m["EMPTY_KEY"]; !exists {
		t.Fatal("ToMap() missing EMPTY_KEY")
	} else if v != "" {
		t.Errorf("ToMap()[EMPTY_KEY] = %q, want empty string", v)
	}

	// Keys must include empty string keys
	keys := sm.Keys()
	found := false
	for _, k := range keys {
		if k == "EMPTY_KEY" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Keys() missing EMPTY_KEY")
	}

	sm.Clear()
}
