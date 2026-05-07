package env

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrAlreadyInitialized is returned when attempting to set a default loader
// when one has already been initialized.
var ErrAlreadyInitialized = errors.New("default loader already initialized")

// cachedError wraps an error with a timestamp for expiration tracking.
type cachedError struct {
	err       error
	timestamp time.Time
}

// Error cache expiration duration.
// Errors older than this will be discarded, allowing retry.
// This prevents permanent failure from transient errors (e.g., file temporarily unavailable).
const errorCacheTTL = 30 * time.Second

// ============================================================================
// Default Loader Singleton
// ============================================================================

var (
	defaultLoader     atomic.Pointer[Loader]
	defaultErrWrapper atomic.Pointer[cachedError] // Cached initialization error with timestamp
	defaultMu         sync.Mutex
)

// getDefaultLoader returns the default loader, creating it if necessary.
// This function is thread-safe using atomic.Pointer and mutex for initialization.
//
// Error Caching: Initialization errors are cached for errorCacheTTL duration to prevent
// infinite retry loops while still allowing recovery from transient failures.
// Use ResetDefaultLoader() to immediately clear the cached error and retry initialization.
func getDefaultLoader() (*Loader, error) {
	// Fast path: check if already initialized
	if loader := defaultLoader.Load(); loader != nil {
		return loader, nil
	}

	// Fast path: check cached error with expiration
	// SECURITY: Only check expiration here, don't try to clear
	// This avoids TOCTOU race with concurrent error updates
	if errWrapper := defaultErrWrapper.Load(); errWrapper != nil {
		if time.Since(errWrapper.timestamp) < errorCacheTTL {
			return nil, errWrapper.err
		}
		// Error expired - let slow path handle proper clearing under lock
	}

	// Slow path: acquire lock for initialization
	defaultMu.Lock()
	defer defaultMu.Unlock()

	// Double-check after acquiring lock
	if loader := defaultLoader.Load(); loader != nil {
		return loader, nil
	}

	// Double-check cached error with expiration
	if errWrapper := defaultErrWrapper.Load(); errWrapper != nil {
		if time.Since(errWrapper.timestamp) < errorCacheTTL {
			return nil, errWrapper.err
		}
		// Error expired, clear it
		defaultErrWrapper.Store(nil)
	}

	loader, err := New(DefaultConfig())
	if err != nil {
		// Cache error with timestamp to allow recovery from transient failures
		wrapper := &cachedError{
			err:       err,
			timestamp: time.Now(),
		}
		defaultErrWrapper.Store(wrapper)
		return nil, err
	}

	defaultLoader.Store(loader)
	return loader, nil
}

// ResetDefaultLoader resets the default loader singleton and clears any cached error.
// This function is intended for use in tests to ensure isolation between test cases.
// It is safe for concurrent use.
//
// Returns any error that occurred while closing the old loader.
// A nil return value indicates either no loader was set or it was closed successfully.
//
// Design: The function atomically swaps the loader to nil while holding the lock,
// then closes the old loader outside the lock. This design choice:
//   - Ensures only one reset can happen at a time (mutex protected)
//   - Allows new loaders to be created immediately after the swap
//   - Clears cached initialization errors to allow retry
//   - Avoids blocking concurrent operations during potentially slow Close()
//   - The old and new loaders are completely independent
//
// Concurrent getDefaultLoader() calls during reset will:
//   - Either see the old loader (before swap) - safe to use
//   - Or see nil and create a new loader (after swap) - safe to use
//
// They will never receive a closed loader.
//
// Example:
//
//	func TestSomething(t *testing.T) {
//	    if err := env.ResetDefaultLoader(); err != nil {
//	        t.Logf("warning: failed to reset loader: %v", err)
//	    }
//	    defer env.ResetDefaultLoader()
//	    // ... test code
//	}
func ResetDefaultLoader() error {
	defaultMu.Lock()
	// Atomically swap to nil while holding the lock.
	// This ensures only one reset can happen at a time.
	oldLoader := defaultLoader.Swap(nil)
	// Clear cached error to allow retry after reset
	defaultErrWrapper.Store(nil)
	defaultMu.Unlock()

	// Close outside the lock to avoid blocking concurrent operations.
	// The old loader is no longer accessible via defaultLoader,
	// so this is safe even if new loaders are created concurrently.
	// The old and new loaders are independent - closing one doesn't affect the other.
	if oldLoader != nil {
		return oldLoader.Close()
	}
	return nil
}

// setDefaultLoader sets the given loader as the default loader.
// Returns ErrAlreadyInitialized if already initialized.
// Uses mutex-protected Load+Store to avoid the transient inconsistency
// that a Swap+Store-rollback pattern would introduce.
func setDefaultLoader(loader *Loader) error {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	if current := defaultLoader.Load(); current != nil {
		return ErrAlreadyInitialized
	}
	defaultLoader.Store(loader)
	return nil
}
