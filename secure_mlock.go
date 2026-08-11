package env

import (
	"log"
	"sync/atomic"

	"github.com/cybergodev/env/internal"
)

// memoryLockConfig controls memory locking behavior for SecureValue.
// Memory locking prevents sensitive data from being swapped to disk,
// which is important for high-security applications handling passwords,
// API keys, and other sensitive credentials.
//
// Users control memory locking through package-level functions:
//   - SetMemoryLockEnabled(bool) - Enable/disable globally
//   - IsMemoryLockEnabled() bool - Check current state
//   - SetMemoryLockStrict(bool) - Enable strict mode
//   - IsMemoryLockStrict() bool - Check strict mode
//   - IsMemoryLockSupported() bool - Check platform support
//
// Security Considerations:
//   - On Unix systems, mlock() requires CAP_IPC_LOCK capability or root privileges
//   - On Windows, VirtualLock() requires SE_LOCK_MEMORY_NAME privilege
//   - If privileges are missing, the operation will fail silently (by default)
//   - Use SetMemoryLockStrict(true) to receive errors when locking fails
//
// Performance Impact:
//   - Memory locking has minimal overhead during allocation
//   - Locked pages cannot be paged out, which may increase memory pressure
//   - Recommended to keep SecureValue objects small and short-lived
type memoryLockConfig struct {
	// enabled controls whether memory locking is attempted
	enabled atomic.Bool

	// strict controls whether locking failures return errors
	// If false (default), locking failures are silently ignored
	// If true, NewSecureValue will return an error if locking fails
	strict atomic.Bool

	// supported indicates if the platform supports memory locking
	// This is set at init time based on the build target
	supported bool
}

// memLockConfig is the global memory lock configuration.
var memLockConfig = memoryLockConfig{
	supported: internal.MemLockSupported(),
}

// SetMemoryLockEnabled enables or disables memory locking globally.
// This affects all new SecureValue objects created after the call.
// Existing SecureValue objects are not affected.
//
// This function is safe to call from multiple goroutines simultaneously.
//
// Example:
//
//	func main() {
//	    // Enable at application startup
//	    env.SetMemoryLockEnabled(true)
//
//	    // ... rest of application
//	}
func SetMemoryLockEnabled(enabled bool) {
	memLockConfig.enabled.Store(enabled)
}

// IsMemoryLockEnabled returns whether memory locking is currently enabled.
func IsMemoryLockEnabled() bool {
	return memLockConfig.enabled.Load()
}

// SetMemoryLockStrict sets whether memory locking failures should return errors.
// By default, locking failures are silently ignored for compatibility.
// Enable strict mode for high-security applications that require confirmation
// that memory is actually locked.
//
// Example:
//
//	env.SetMemoryLockEnabled(true)
//	env.SetMemoryLockStrict(true) // Now locking failures will return errors
//	sv := env.NewSecureValue("sensitive-data")
//	// If locking failed, sv will still be valid but the error is logged
func SetMemoryLockStrict(strict bool) {
	memLockConfig.strict.Store(strict)
}

// IsMemoryLockStrict returns whether strict mode is enabled.
func IsMemoryLockStrict() bool {
	return memLockConfig.strict.Load()
}

// strictLockFailureHandler is the function signature for strict memory-lock
// failure notifications.
type strictLockFailureHandler func(error)

// onStrictLockFailure is invoked when strict memory-lock mode is enabled and a
// lock attempt fails. It is the mechanism that makes SetMemoryLockStrict
// observable: without it, enabling strict mode had no effect on the non-error
// constructors (NewSecureValue cannot return an error).
//
// The default implementation logs a security warning to the standard logger
// (stderr). Strict mode is opt-in, so the user has explicitly asked to be
// informed of locking failures. The hook is an unexported variable so tests
// (and internal callers) can redirect or suppress it without expanding the
// public API. SecureValue has no reference to a Loader/Auditor, so auditor
// routing is not possible at this layer.
//
// Thread Safety: The handler is stored in an atomic.Pointer so that concurrent
// reads from tryLockMemory and test-time swaps are race-free. The atomic load
// adds ~1 ns per call — negligible compared to the syscall it follows.
var onStrictLockFailure atomic.Pointer[strictLockFailureHandler]

// init installs the default strict-lock-failure handler.
func init() {
	h := strictLockFailureHandler(defaultStrictLockFailureHandler)
	onStrictLockFailure.Store(&h)
}

// defaultStrictLockFailureHandler logs a memory-lock failure to the standard
// logger. It is the default value of onStrictLockFailure.
func defaultStrictLockFailureHandler(err error) {
	log.Printf("env: memory lock failed in strict mode: %v", err)
}

// IsMemoryLockSupported returns whether the current platform supports memory locking.
// This returns false on platforms like wasm or nacl where memory locking
// is not available.
//
// Note: This only indicates platform support, not whether the process has
// the required privileges to actually lock memory.
func IsMemoryLockSupported() bool {
	return memLockConfig.supported
}

// The following functions are implemented by platform-specific files:
// - memLockSupported() bool - Returns true if platform supports memory locking
// - lockMemory(data []byte) error - Locks memory to prevent swapping
// - unlockMemory(data []byte) - Unlocks previously locked memory
