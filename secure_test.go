package env

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
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
		{"release twice is safe", "test", "release_twice", nil, true},

		// Masked output
		{"masked with value", "test", "masked", "[SECURE:4 bytes]", false},
		{"masked when closed", "test", "masked_closed", "[CLOSED]", true},
		{"masked long value contains count", strings.Repeat("x", 100), "masked_count", "100", false},

		// State queries after Close
		{"string after close", "test", "string_closed", "[CLOSED]", true},
		{"bytes after close nil", "test", "bytes_closed", "", true},
		{"length after close zero", "test", "length_closed", 0, true},
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
				_ = sv.Close() // best-effort cleanup
				if sv.Masked() != tt.wantResult.(string) {
					t.Errorf("Masked() = %q, want %q", sv.Masked(), tt.wantResult)
				}
			case "masked_count":
				if m := sv.Masked(); !strings.Contains(m, tt.wantResult.(string)) {
					t.Errorf("Masked() = %q, should contain %q", m, tt.wantResult)
				}
			case "string_closed":
				_ = sv.Close() // best-effort cleanup
				if sv.String() != tt.wantResult.(string) {
					t.Errorf("String() after Close() = %q, want %q", sv.String(), tt.wantResult)
				}
			case "bytes_closed":
				_ = sv.Close() // best-effort cleanup
				if sv.Bytes() != nil {
					t.Errorf("Bytes() after Close() = %v, want nil", sv.Bytes())
				}
			case "length_closed":
				_ = sv.Close() // best-effort cleanup
				if sv.Length() != tt.wantResult.(int) {
					t.Errorf("Length() after Close() = %d, want %d", sv.Length(), tt.wantResult)
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
			case "release_twice":
				// Merged from the former TestSecureValue_DoubleReleaseSafe: a
				// repeated Release on an already-pooled value is a no-op.
				sv.Release()
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
	// Stress alloc/free: must not panic, and reused values must carry the new
	// secret (reset clears the old value before reuse).
	for range 10 {
		sv := NewSecureValue("test")
		sv.Release()
	}
	for range 5 {
		if sv := NewSecureValue("new"); sv.Reveal() != "new" {
			t.Errorf("reused SecureValue = %q, want %q", sv.Reveal(), "new")
		}
	}

	// A released SecureValue is returned to the underlying sync.Pool, so the
	// next allocation must hand back the same object (pointer identity). sync.Pool
	// may evict under GC pressure, so require reuse on at least one of several
	// tight alloc/free rounds rather than every one — but a healthy pool reuses
	// reliably.
	reused := 0
	for range 5 {
		first := NewSecureValue("a")
		first.Release()

		second := NewSecureValue("b")
		if first == second {
			reused++
		}
		second.Release()
	}
	if reused == 0 {
		t.Error("SecureValue pool never reused a released value across 5 rounds")
	} else {
		t.Logf("SecureValue pool reused %d/5 released values", reused)
	}
}

// (TestSecureValue_ResetStateConsistency was removed: pool reuse returning a
// reset, usable value is pinned by TestSecureValuePool, and the empty-value
// behavior by TestSecureValue's "empty value string/length/masked" rows.)

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
				// The map stays usable after Clear (formerly pinned by the
				// removed TestSecureMap_ClearReleasesMemory).
				sm.Set("NEWKEY", "newvalue")
				if sm.Len() != 1 {
					t.Errorf("Len() after Set on a cleared map = %d, want 1", sm.Len())
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
		// From the former TestSecureValue_MemoryLockError: no lock was
		// attempted, so no lock error is recorded either.
		if err := sv.MemoryLockError(); err != nil {
			t.Errorf("MemoryLockError() = %v, want nil when disabled", err)
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
		_ = sv.Close() // best-effort cleanup

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

	t.Run("empty_value_lock_enabled_no_error", func(t *testing.T) {
		SetMemoryLockEnabled(true)

		sv, err := NewSecureValueStrict("")
		if sv != nil {
			defer sv.Release()
		}

		// reset() returns early for zero-length values before tryLockMemory
		// runs, so no lock is ever attempted and strict mode sees no error.
		if err != nil {
			t.Errorf("NewSecureValueStrict(\"\") error = %v, want nil for zero-length value", err)
		}
		if sv == nil {
			t.Fatal("NewSecureValueStrict(\"\") returned nil")
		}
	})
}

// (TestSecureValue_MemoryLockError was folded into TestMemoryLock_SecureValue's
// disabled_by_default case above.)

// TestSecureValue_MaskedLockStatus pins the lock-status suffix that Masked
// (and String, which delegates to it) appends when memory locking is enabled:
// " locked" on success, " lock-failed" on failure, and nothing at all while
// disabled. Which enabled-state outcome applies depends on OS privileges, so
// the expectation is derived from the value's own lock state — but the exact
// string, including the suffix and closing bracket, is asserted.
func TestSecureValue_MaskedLockStatus(t *testing.T) {
	origEnabled := IsMemoryLockEnabled()
	defer SetMemoryLockEnabled(origEnabled)

	const value = "test"

	t.Run("disabled omits lock status", func(t *testing.T) {
		SetMemoryLockEnabled(false)
		sv := NewSecureValue(value)
		defer sv.Release()
		if got, want := sv.Masked(), "[SECURE:4 bytes]"; got != want {
			t.Errorf("Masked() = %q, want %q", got, want)
		}
	})

	t.Run("enabled appends lock outcome", func(t *testing.T) {
		SetMemoryLockEnabled(true)
		sv := NewSecureValue(value)
		defer sv.Release()

		var want string
		switch {
		case sv.IsMemoryLocked():
			want = "[SECURE:4 bytes locked]"
			// A successful lock proves the platform supports memory
			// locking — pins IsMemoryLockSupported's body alongside.
			if !IsMemoryLockSupported() {
				t.Error("IsMemoryLockSupported() = false after a successful lock")
			}
		case sv.MemoryLockError() != nil:
			want = "[SECURE:4 bytes lock-failed]"
		default:
			want = "[SECURE:4 bytes unlocked]"
		}
		if got := sv.Masked(); got != want {
			t.Errorf("Masked() = %q, want %q", got, want)
		}
		if got := sv.String(); got != want {
			t.Errorf("String() = %q, want %q (String delegates to Masked)", got, want)
		}
	})

	// BOUNDARY, deterministic: a value created while locking was disabled
	// carries neither a lock nor a lock error, so flipping the switch on
	// afterwards must render the "unlocked" suffix.
	t.Run("enabled after creation renders unlocked", func(t *testing.T) {
		SetMemoryLockEnabled(false)
		sv := NewSecureValue(value)
		defer sv.Release()
		SetMemoryLockEnabled(true)

		const want = "[SECURE:4 bytes unlocked]"
		if got := sv.Masked(); got != want {
			t.Errorf("Masked() = %q, want %q", got, want)
		}
	})
}

// TestSecureValue_RevealAfterClose pins Reveal on a closed value (secure.go):
// the backing buffer is zeroed on Close, so the reveal is the empty string.
func TestSecureValue_RevealAfterClose(t *testing.T) {
	sv := NewSecureValue("test")
	if err := sv.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := sv.Reveal(); got != "" {
		t.Errorf("Reveal() after Close() = %q, want \"\"", got)
	}
}

// TestNewSecureValue_PoolPoison pins NewSecureValue's pool type guard: a
// poisoned secureValuePool must fall back to a fresh value instead of
// panicking on the type assertion.
func TestNewSecureValue_PoolPoison(t *testing.T) {
	for i := 0; i < 4; i++ {
		secureValuePool.Put(new(int))
	}
	sv := NewSecureValue("x")
	if sv == nil {
		t.Fatal("NewSecureValue() = nil for a poisoned pool")
	}
	if got := sv.Reveal(); got != "x" {
		t.Errorf("Reveal() = %q, want %q (fresh fallback value)", got, "x")
	}
	sv.Release()
}

// TestDefaultStrictLockFailureHandler covers the D-002 strict-mode hook. The
// handler logs a security warning (with the causing error) to the standard
// logger; strict mode is opt-in so the user has asked to be informed.
func TestDefaultStrictLockFailureHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Default()
	origOut := logger.Writer()
	origFlags := logger.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0) // drop timestamp for a deterministic substring check
	defer func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	}()

	defaultStrictLockFailureHandler(errors.New("boom"))

	got := buf.String()
	if !strings.Contains(got, "memory lock failed") {
		t.Errorf("log output = %q, want substring %q", got, "memory lock failed")
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("log output = %q, want the wrapped error text %q", got, "boom")
	}
}

// TestOnStrictLockFailureSwappable confirms the hook is redirectable — tests
// and internal callers can swap it without expanding the public API, and the
// swap is actually honored.
func TestOnStrictLockFailureSwappable(t *testing.T) {
	orig := onStrictLockFailure.Load()
	defer onStrictLockFailure.Store(orig)

	var (
		called bool
		gotErr error
	)
	h := strictLockFailureHandler(func(err error) {
		called = true
		gotErr = err
	})
	onStrictLockFailure.Store(&h)

	if ptr := onStrictLockFailure.Load(); ptr != nil {
		(*ptr)(errors.New("injected"))
	}

	if !called {
		t.Error("swapped onStrictLockFailure was not invoked")
	}
	if gotErr == nil || gotErr.Error() != "injected" {
		t.Errorf("handler received err = %v, want %q", gotErr, "injected")
	}
}

// ============================================================================
// Sensitive Helper Wiring Tests
// ============================================================================

// TestSensitiveHelpersWiring covers the delegation in sensitive_export.go:
// each exported helper must return its internal implementation's output. The
// full behavior matrices live in internal/sensitive_test.go; the rows here
// (one or two per function) pin known output shapes so the wiring cannot
// silently detach from the internal implementation.
func TestSensitiveHelpersWiring(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		// IsSensitiveKey -> internal.IsKey
		{"IsSensitiveKey flags API_KEY", strconv.FormatBool(IsSensitiveKey("API_KEY")), "true"},
		{"IsSensitiveKey passes HOST through", strconv.FormatBool(IsSensitiveKey("HOST")), "false"},

		// MaskValue -> internal.MaskValue: sensitive keys mask with the fixed
		// token (SEC-004 — no length oracle), non-sensitive values pass through.
		{"MaskValue masks sensitive key", MaskValue("PASSWORD", "super_secret_123"), "[MASKED]"},
		{"MaskValue masks empty value under sensitive key", MaskValue("SECRET", ""), "[MASKED]"},
		{"MaskValue passes non-sensitive value", MaskValue("HOST", "localhost"), "localhost"},

		// MaskKey -> internal.MaskKey: fixed token with no key content (SEC-005).
		{"MaskKey hides the key spelling", MaskKey("API_KEY"), "[MASKED]"},

		// MaskSensitiveInString -> internal.MaskInString (SEC-04 posture)
		{"MaskSensitiveInString masks secret pair", MaskSensitiveInString("password=hunter2"), "password=[MASKED]"},
		{"MaskSensitiveInString truncates to 47 chars", MaskSensitiveInString(strings.Repeat("a", 60)), strings.Repeat("a", 47) + "..."},

		// SanitizeForLog -> internal.SanitizeForLog
		{"SanitizeForLog masks secret pair", SanitizeForLog("password=my_secret_password"), "password=[MASKED]"},
		{"SanitizeForLog keeps non-sensitive text", SanitizeForLog("host=localhost port=8080"), "host=localhost port=8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

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
	t.Run("Release", func(t *testing.T) {
		var sv *SecureValue
		sv.Release() // nil guard: no-op, must not panic
		if !sv.IsClosed() {
			t.Error("nil IsClosed() = false after nil Release(), want true")
		}
	})
	t.Run("IsMemoryLocked", func(t *testing.T) {
		var sv *SecureValue
		if sv.IsMemoryLocked() {
			t.Error("nil IsMemoryLocked() = true, want false")
		}
	})
	t.Run("MemoryLockError", func(t *testing.T) {
		var sv *SecureValue
		if err := sv.MemoryLockError(); err != nil {
			t.Errorf("nil MemoryLockError() = %v, want nil", err)
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
		// BOUNDARY against the hard key-length guard (accessors.go): a base
		// of HardMaxKeyLength-2 plus "_0" lands exactly on the limit and is
		// allowed; one byte more is rejected with "".
		{"total exactly at hard max", strings.Repeat("A", internal.HardMaxKeyLength-2), 0, strings.Repeat("A", internal.HardMaxKeyLength-2) + "_0"},
		{"total one over hard max", strings.Repeat("A", internal.HardMaxKeyLength-1), 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildIndexedKey(tt.base, tt.index)
			// Unconditional: the negative-index row's expected "" is the real
			// contract (buildIndexedKey returns "" for index < 0), not a
			// placeholder to skip.
			if result != tt.expected {
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

	// BOUNDARY (accessors.go getWithDefault): typed accessors on a nil
	// loader return the caller's default (or the zero value) before any
	// lookup is attempted.
	t.Run("GetInt nil returns default", func(t *testing.T) {
		var l *Loader
		if v := l.GetInt("KEY", 7); v != 7 {
			t.Errorf("nil GetInt() = %v, want 7", v)
		}
	})
	t.Run("GetDuration nil returns zero", func(t *testing.T) {
		var l *Loader
		if v := l.GetDuration("KEY"); v != 0 {
			t.Errorf("nil GetDuration() = %v, want 0", v)
		}
	})
}

// (TestNewCloseableChannelHandler and TestBuildValidatorAdapter_CustomValidator
// were removed: construction + Close of the closeable channel handler is
// pinned by TestCloseableChannelHandler_DroppedEvents in
// audit_failclosed_test.go, and the custom-validator factory wiring — with
// the same fullMockValidator/minimalMockValidator mocks actually INVOKED — by
// TestComponentFactory_Validator in utils_test.go, which is the stronger
// assertion.)

// TestBuildAuditorAdapter keeps the factory.go branch with no runtime
// equivalent elsewhere: a custom auditor implementing FullAuditLogger is
// passed through unwrapped. (The former "non-standard auditor" case was
// removed — TestSetDelete_AuditGating in loader_test.go drives the same
// wrapper end to end through New and asserts event routing.)
func TestBuildAuditorAdapter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CustomAuditor = &mockFullAuditLogger{}
	factory := cfg.buildComponentFactory()
	defer func() { _ = factory.Close() }() // best-effort cleanup

	if a := factory.Auditor(); a == nil {
		t.Fatal("Auditor() returned nil")
	}
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

// TestSecureMap_Has covers the allocation-free existence check directly.
// A closed SecureValue inside a shard counts as absent even though the shard
// map still holds the entry — reachable only through the shard, since no
// public API closes an in-map value.
func TestSecureMap_Has(t *testing.T) {
	sm := newSecureMap()
	defer sm.Clear()
	sm.Set("OPEN_KEY", "value")
	sm.Set("CLOSED_KEY", "value")

	shard := sm.getShard("CLOSED_KEY")
	shard.mu.RLock()
	stored := shard.values["CLOSED_KEY"]
	shard.mu.RUnlock()
	_ = stored.Close() // best-effort cleanup

	tests := []struct {
		key  string
		want bool
	}{
		{"OPEN_KEY", true},
		{"CLOSED_KEY", false},
		{"MISSING_KEY", false},
	}
	for _, tt := range tests {
		if got := sm.Has(tt.key); got != tt.want {
			t.Errorf("Has(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}

	// BOUNDARY (secure.go): a stored-but-closed value reads as absent from
	// Get and GetSecure alike — the closed check, not just the map lookup,
	// gates both readers.
	if v, ok := sm.Get("CLOSED_KEY"); ok || v != "" {
		t.Errorf("Get(CLOSED_KEY) = (%q, %v), want (\"\", false)", v, ok)
	}
	if sv := sm.GetSecure("CLOSED_KEY"); sv != nil {
		t.Errorf("GetSecure(CLOSED_KEY) = %v, want nil", sv)
	}
}

// TestSecureMap_SetAllIfAbsent covers the batch insert-if-missing path: it
// returns the count of newly inserted keys and leaves existing keys' values
// untouched.
func TestSecureMap_SetAllIfAbsent(t *testing.T) {
	sm := newSecureMap()
	defer sm.Clear()
	sm.Set("EXISTING", "original")

	inserted := sm.SetAllIfAbsent(map[string]string{
		"EXISTING": "must-not-replace",
		"FRESH_A":  "a",
		"FRESH_B":  "b",
	})
	if inserted != 2 {
		t.Errorf("SetAllIfAbsent() inserted = %d, want 2", inserted)
	}
	if v, ok := sm.Get("EXISTING"); !ok || v != "original" {
		t.Errorf("Get(EXISTING) = (%q, %v), want (%q, true): existing keys must be untouched", v, ok, "original")
	}
	for _, key := range []string{"FRESH_A", "FRESH_B"} {
		if !sm.Has(key) {
			t.Errorf("Has(%q) = false, want true after SetAllIfAbsent", key)
		}
	}
	if got := sm.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}

	// A fully-present batch inserts nothing and still changes no values.
	if n := sm.SetAllIfAbsent(map[string]string{"EXISTING": "x", "FRESH_A": "y"}); n != 0 {
		t.Errorf("SetAllIfAbsent() = %d, want 0 for fully-present batch", n)
	}
	if v, _ := sm.Get("FRESH_A"); v != "a" {
		t.Errorf("Get(FRESH_A) = %q, want %q", v, "a")
	}

	// An empty batch is a no-op.
	if n := sm.SetAllIfAbsent(nil); n != 0 {
		t.Errorf("SetAllIfAbsent(nil) = %d, want 0", n)
	}
}

// ============================================================================
// Marshaling Redaction Tests
// ============================================================================

// TestSecureValue_MarshalRedaction verifies that reflection-based serializers
// never see the plaintext: MarshalJSON and MarshalText must both emit the
// masked form, including for nil and closed values.
func TestSecureValue_MarshalRedaction(t *testing.T) {
	const secret = "super-secret-value"

	t.Run("MarshalJSON emits masked form", func(t *testing.T) {
		sv := NewSecureValue(secret)
		defer sv.Release()

		b, err := json.Marshal(sv)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		out := string(b)
		if strings.Contains(out, secret) {
			t.Errorf("MarshalJSON() leaked plaintext: %s", out)
		}
		if !strings.Contains(out, sv.Masked()) {
			t.Errorf("MarshalJSON() = %s, want masked form %q", out, sv.Masked())
		}
	})

	t.Run("MarshalJSON on nil receiver returns null", func(t *testing.T) {
		var sv *SecureValue
		// Call the method directly: json.Marshal short-circuits nil pointers
		// and would never invoke MarshalJSON.
		b, err := sv.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}
		if string(b) != "null" {
			t.Errorf("MarshalJSON() = %s, want null", b)
		}
	})

	t.Run("MarshalJSON on closed value stays masked", func(t *testing.T) {
		sv := NewSecureValue(secret)
		_ = sv.Close() // best-effort cleanup

		b, err := json.Marshal(sv)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		if strings.Contains(string(b), secret) {
			t.Errorf("MarshalJSON() leaked plaintext after Close: %s", b)
		}
	})

	t.Run("MarshalJSON inside nested struct stays masked", func(t *testing.T) {
		type credentials struct {
			Token *SecureValue `json:"token"`
		}

		sv := NewSecureValue(secret)
		defer sv.Release()

		b, err := json.Marshal(credentials{Token: sv})
		if err != nil {
			t.Fatalf("json.Marshal(struct) error = %v", err)
		}
		if strings.Contains(string(b), secret) {
			t.Errorf("nested json.Marshal() leaked plaintext: %s", b)
		}
	})

	t.Run("MarshalText emits masked form", func(t *testing.T) {
		sv := NewSecureValue(secret)
		defer sv.Release()

		b, err := sv.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText() error = %v", err)
		}
		if strings.Contains(string(b), secret) {
			t.Errorf("MarshalText() leaked plaintext: %s", b)
		}
		if string(b) != sv.Masked() {
			t.Errorf("MarshalText() = %s, want %q", b, sv.Masked())
		}
	})

	t.Run("MarshalText on nil receiver returns NIL marker", func(t *testing.T) {
		var sv *SecureValue
		b, err := sv.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText() error = %v", err)
		}
		if string(b) != "[NIL]" {
			t.Errorf("MarshalText() = %s, want [NIL]", b)
		}
	})
}

// ============================================================================
// Memory Lock Failure Path Tests
// ============================================================================

// TestSecureValue_MemoryLockFailureContract pins the strict-mode failure
// contract: when locking fails, MemoryLockError must be set, the value must
// not report as locked, and the strict-mode handler must have been invoked;
// when locking succeeds the value reports locked with no error. Exactly one
// of the two outcomes holds regardless of OS privileges.
func TestSecureValue_MemoryLockFailureContract(t *testing.T) {
	originalEnabled := IsMemoryLockEnabled()
	originalStrict := IsMemoryLockStrict()
	originalHandler := onStrictLockFailure.Load()
	defer func() {
		SetMemoryLockEnabled(originalEnabled)
		SetMemoryLockStrict(originalStrict)
		onStrictLockFailure.Store(originalHandler)
	}()

	SetMemoryLockEnabled(true)
	SetMemoryLockStrict(true)

	var handlerCalled atomic.Bool
	var handler strictLockFailureHandler = func(err error) {
		if err == nil {
			t.Error("strict lock failure handler invoked with nil error")
		}
		handlerCalled.Store(true)
	}
	onStrictLockFailure.Store(&handler)

	// A large buffer is used to make VirtualLock/mlock failure (and thus the
	// strict handler path) likely on systems without SE_LOCK_MEMORY/raised
	// RLIMIT_MEMLOCK; on privileged systems the success path is pinned.
	sv := NewSecureValue(strings.Repeat("x", 64<<20))
	defer sv.Release()

	lockErr := sv.MemoryLockError()
	locked := sv.IsMemoryLocked()

	if lockErr != nil && locked {
		t.Error("IsMemoryLocked() = true despite MemoryLockError() being set")
	}
	if lockErr != nil && !handlerCalled.Load() {
		t.Error("strict mode enabled but lock failure handler was not invoked")
	}
	if lockErr == nil && !locked {
		t.Errorf("lock reported neither failed (err=%v) nor succeeded (locked=%v)", lockErr, locked)
	}

	// The value stays usable either way — the data is just not swap-protected.
	if sv.Reveal() != strings.Repeat("x", 64<<20) {
		t.Error("Reveal() changed after memory lock failure")
	}
}

// ============================================================================
// Full-Capacity Clearing (GEN-001 hardening)
// ============================================================================

// TestSecureValueClearCoversFullCapacity pins the security invariant that
// wiping covers the slice's full capacity, not just its length: after an
// in-place shrink-and-reuse (reset to a shorter value) the [len, cap) slack
// must contain only zeros, and Close must zero the whole backing array.
func TestSecureValueClearCoversFullCapacity(t *testing.T) {
	sv := NewSecureValue("secretXX") // 8 bytes: len == cap

	// Shrink in place: cap 8 >= 4 and 8 <= 4*2, so reset takes the reuse
	// branch and re-slices the same backing array.
	sv.reset("abcd")
	if len(sv.data) != 4 || cap(sv.data) != 8 {
		t.Fatalf("after reset: len=%d cap=%d, want len=4 cap=8", len(sv.data), cap(sv.data))
	}
	for i, b := range sv.data[4:] {
		if b != 0 {
			t.Fatalf("reuse slack byte %d = %q, want 0 (old value survived the shrink)", i, b)
		}
	}

	// Close must zero the entire backing array, slack included. Keep an alias
	// so the array stays observable after clearDataLocked drops the reference.
	backing := sv.data[:cap(sv.data)]
	if err := sv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for i, b := range backing {
		if b != 0 {
			t.Fatalf("backing array byte %d = %q after Close, want 0", i, b)
		}
	}
}
