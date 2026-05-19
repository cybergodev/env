package env

import (
	"errors"
	"sync"
	"sync/atomic"
)

// ErrAlreadyInitialized is returned when attempting to set a default loader
// when one has already been initialized.
var ErrAlreadyInitialized = errors.New("default loader already initialized")

// ErrNotInitialized is returned by convenience functions (GetString, GetInt, etc.)
// when called before Load() or LoadWithConfig().
var ErrNotInitialized = errors.New("default loader not initialized; call Load() first")

// ============================================================================
// Default Loader Singleton
// ============================================================================

var (
	defaultLoader atomic.Pointer[Loader]
	defaultMu     sync.Mutex
)

// getDefaultLoader returns the default loader set by Load() or LoadWithConfig().
// Returns ErrNotInitialized if no loader has been set.
func getDefaultLoader() (*Loader, error) {
	if loader := defaultLoader.Load(); loader != nil {
		return loader, nil
	}
	return nil, ErrNotInitialized
}

// ResetDefaultLoader resets the default loader singleton.
// This function is intended for use in tests to ensure isolation between test cases.
// It is safe for concurrent use.
//
// Returns any error that occurred while closing the old loader.
// A nil return value indicates either no loader was set or it was closed successfully.
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
	oldLoader := defaultLoader.Swap(nil)

	if oldLoader != nil {
		err := oldLoader.Close()
		defaultMu.Unlock()
		return err
	}
	defaultMu.Unlock()
	return nil
}

// setDefaultLoader sets the given loader as the default loader.
// Returns ErrAlreadyInitialized if already initialized.
func setDefaultLoader(loader *Loader) error {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	if current := defaultLoader.Load(); current != nil {
		return ErrAlreadyInitialized
	}
	defaultLoader.Store(loader)
	return nil
}
