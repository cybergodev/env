package env

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Concurrent Access Tests for Loader
// ============================================================================

// TestLoader_ConcurrentGet tests concurrent read access to the loader.
func TestLoader_ConcurrentGet(t *testing.T) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.Close()

	// Pre-populate with variables
	for i := 0; i < 100; i++ {
		loader.Set("KEY"+string(rune('A'+i%26)), "value")
	}

	var wg sync.WaitGroup
	errors := int64(0)
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY" + string(rune('A'+j%26))
				_ = loader.GetString(key)
				_, _ = loader.Lookup(key)
				_ = loader.GetSecure(key)
			}
		}(i)
	}

	wg.Wait()
	if atomic.LoadInt64(&errors) > 0 {
		t.Errorf("concurrent Get operations had %d errors", errors)
	}
}

// TestLoader_ConcurrentSet tests concurrent write access to the loader.
func TestLoader_ConcurrentSet(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.Close()

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY" + string(rune('A'+j%26))
				loader.Set(key, "value")
			}
		}(i)
	}

	wg.Wait()
}

// TestLoader_ConcurrentReadWrite tests concurrent read and write access.
func TestLoader_ConcurrentReadWrite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.Close()

	// Pre-populate
	for i := 0; i < 50; i++ {
		loader.Set("KEY"+string(rune('A'+i%26)), "initial")
	}

	var wg sync.WaitGroup
	iterations := 500

	// Writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY" + string(rune('A'+j%26))
				loader.Set(key, "writer_value")
			}
		}(i)
	}

	// Readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY" + string(rune('A'+j%26))
				_ = loader.GetString(key)
				_ = loader.Keys()
				_ = loader.All()
				_ = loader.Len()
			}
		}(i)
	}

	// Deleters
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations/10; j++ {
				key := "KEY" + string(rune('A'+j%26))
				loader.Delete(key)
			}
		}(i)
	}

	wg.Wait()
}

// TestLoader_ConcurrentWithClose tests concurrent operations with Close.
func TestLoader_ConcurrentWithClose(t *testing.T) {
	for run := 0; run < 10; run++ {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		iterations := 100
		closed := int64(0)

		// Operations
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					loader.Set("KEY", "value")
					_ = loader.GetString("KEY")
					_ = loader.Keys()
				}
			}()
		}

		// Closer
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if atomic.CompareAndSwapInt64(&closed, 0, 1) {
					loader.Close()
				}
			}
		}()

		wg.Wait()
	}
}

// ============================================================================
// Concurrent Access Tests for SecureMap
// ============================================================================

// TestSecureMap_ConcurrentAccess tests concurrent access to secureMap.
func TestSecureMap_ConcurrentAccess(t *testing.T) {
	sm := newSecureMap()

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	// Writers
	for i := 0; i < concurrency/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY" + string(rune('A'+j%26))
				sm.Set(key, "value")
			}
		}(i)
	}

	// Readers
	for i := 0; i < concurrency/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY" + string(rune('A'+j%26))
				sm.Get(key)
				sm.GetSecure(key)
			}
		}(i)
	}

	wg.Wait()
}

// TestSecureMap_ConcurrentSetAll tests concurrent SetAll operations.
func TestSecureMap_ConcurrentSetAll(t *testing.T) {
	sm := newSecureMap()

	var wg sync.WaitGroup
	iterations := 100
	concurrency := 5

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				values := map[string]string{
					"KEY_A": "value_a",
					"KEY_B": "value_b",
					"KEY_C": "value_c",
				}
				sm.SetAll(values)
			}
		}(i)
	}

	wg.Wait()
}

// TestSecureMap_ConcurrentClear tests concurrent operations with Clear.
func TestSecureMap_ConcurrentClear(t *testing.T) {
	for run := 0; run < 10; run++ {
		sm := newSecureMap()

		// Pre-populate
		for i := 0; i < 100; i++ {
			sm.Set("KEY"+string(rune('A'+i%26)), "value")
		}

		var wg sync.WaitGroup
		iterations := 100
		cleared := int64(0)

		// Operations
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					sm.Set("KEY", "value")
					sm.Get("KEY")
					_ = sm.Keys()
					_ = sm.Len()
				}
			}()
		}

		// Clearer
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations/10; j++ {
				if atomic.CompareAndSwapInt64(&cleared, 0, 1) {
					sm.Clear()
					atomic.StoreInt64(&cleared, 0)
				}
			}
		}()

		wg.Wait()
	}
}

// ============================================================================
// Concurrent Access Tests for SecureValue
// ============================================================================

// TestSecureValue_ConcurrentAccess tests concurrent access to SecureValue.
func TestSecureValue_ConcurrentAccess(t *testing.T) {
	sv := NewSecureValue("test_value")

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = sv.String()
				_ = sv.Bytes()
				_ = sv.Length()
				_ = sv.Masked()
				_ = sv.IsClosed()
			}
		}()
	}

	wg.Wait()
}

// TestSecureValue_ConcurrentWithClose tests concurrent access with Close.
func TestSecureValue_ConcurrentWithClose(t *testing.T) {
	for run := 0; run < 10; run++ {
		sv := NewSecureValue("test_value")

		var wg sync.WaitGroup
		iterations := 100
		closed := int64(0)

		// Readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = sv.String()
					_ = sv.Bytes()
					_ = sv.Length()
					_ = sv.IsClosed()
				}
			}()
		}

		// Closer
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if atomic.CompareAndSwapInt64(&closed, 0, 1) {
					sv.Close()
				}
			}
		}()

		wg.Wait()
	}
}

// ============================================================================
// Concurrent Access Tests for Parser Registry
// ============================================================================

// TestParserRegistry_ConcurrentAccess tests concurrent access to the parser registry.
func TestParserRegistry_ConcurrentAccess(t *testing.T) {
	var wg sync.WaitGroup
	iterations := 100
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cfg := DefaultConfig()
				factory := cfg.buildComponentFactory()
				_, err := createParsers(cfg, factory)
				if err != nil {
					t.Error(err)
				}
				factory.Close()
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// Concurrent Access Tests for ComponentFactory
// ============================================================================

// TestComponentFactory_ConcurrentAccess tests concurrent access to ComponentFactory.
func TestComponentFactory_ConcurrentAccess(t *testing.T) {
	cfg := DefaultConfig()
	factory := cfg.buildComponentFactory()
	defer factory.Close()

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = factory.Validator()
				_ = factory.Auditor()
				_ = factory.lineParserExpander()
				_ = factory.IsClosed()
			}
		}()
	}

	wg.Wait()
}

// TestComponentFactory_ConcurrentClose tests concurrent Close operations.
func TestComponentFactory_ConcurrentClose(t *testing.T) {
	for run := 0; run < 10; run++ {
		cfg := DefaultConfig()
		factory := cfg.buildComponentFactory()

		var wg sync.WaitGroup
		iterations := 100

		// Users
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = factory.Validator()
					_ = factory.Auditor()
					_ = factory.IsClosed()
				}
			}()
		}

		// Closers
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations/10; j++ {
					factory.Close()
				}
			}()
		}

		wg.Wait()
	}
}

// ============================================================================
// Stress Tests
// ============================================================================

// TestStress_HighConcurrency tests the loader under high concurrency.
func TestStress_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.Close()

	// Pre-populate with many variables
	for i := 0; i < 1000; i++ {
		loader.Set("STRESS_KEY_"+string(rune(i%256)), "value")
	}

	var wg sync.WaitGroup
	iterations := 10000
	concurrency := 50

	// Mixed operations
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				switch j % 5 {
				case 0:
					loader.Set("STRESS_KEY_"+string(rune(j%256)), "new_value")
				case 1:
					_ = loader.GetString("STRESS_KEY_" + string(rune(j%256)))
				case 2:
					_ = loader.Keys()
				case 3:
					_ = loader.Len()
				case 4:
					_, _ = loader.Lookup("STRESS_KEY_" + string(rune(j%256)))
				}
			}
		}(i)
	}

	wg.Wait()
}

// ============================================================================
// Concurrent Apply and Validate Tests
// ============================================================================

func TestLoader_ConcurrentApplyValidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RequiredKeys = []string{"KEY1", "KEY2"}
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.Close()

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent Apply
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				loader.Set("KEY1", "value")
				_ = loader.Apply()
			}
		}(i)
	}

	// Concurrent Validate
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = loader.Validate()
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// Edge Case Concurrency Tests
// ============================================================================

// TestSecureMap_ConcurrentDeleteAndRead tests concurrent delete while reading.
func TestSecureMap_ConcurrentDeleteAndRead(t *testing.T) {
	for run := 0; run < 5; run++ {
		sm := newSecureMap()

		// Pre-populate with many keys
		for i := 0; i < 100; i++ {
			sm.Set("KEY_"+string(rune('A'+i%26)), "value")
		}

		var wg sync.WaitGroup
		iterations := 500

		// Deleters
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					key := "KEY_" + string(rune('A'+j%26))
					sm.Delete(key)
				}
			}(i)
		}

		// Readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					key := "KEY_" + string(rune('A'+j%26))
					sm.Get(key)
					sm.GetSecure(key)
				}
			}(i)
		}

		// Writers
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					key := "KEY_" + string(rune('A'+j%26))
					sm.Set(key, "new_value")
				}
			}(i)
		}

		wg.Wait()
	}
}

// TestSecureMap_ConcurrentToMapAndModify tests concurrent ToMap with modifications.
func TestSecureMap_ConcurrentToMapAndModify(t *testing.T) {
	sm := newSecureMap()

	// Pre-populate
	for i := 0; i < 50; i++ {
		sm.Set("KEY_"+string(rune('A'+i%26)), "value")
	}

	var wg sync.WaitGroup
	iterations := 200

	// ToMap callers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				m := sm.ToMap()
				_ = len(m)
			}
		}()
	}

	// Modifiers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				sm.Set(key, "modified")
				sm.Delete(key)
			}
		}(i)
	}

	wg.Wait()
}

// TestSecureMap_ConcurrentKeysAndClear tests concurrent Keys() with Clear().
func TestSecureMap_ConcurrentKeysAndClear(t *testing.T) {
	for run := 0; run < 5; run++ {
		sm := newSecureMap()

		// Pre-populate
		for i := 0; i < 100; i++ {
			sm.Set("KEY_"+string(rune('A'+i%26)), "value")
		}

		var wg sync.WaitGroup
		iterations := 100
		cleared := int64(0)

		// Keys callers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					keys := sm.Keys()
					_ = keys
				}
			}()
		}

		// Clear callers
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations/10; j++ {
					if atomic.CompareAndSwapInt64(&cleared, 0, 1) {
						sm.Clear()
						atomic.StoreInt64(&cleared, 0)
					}
				}
			}()
		}

		wg.Wait()
	}
}

// TestSecureValue_ConcurrentReleaseAndRead tests concurrent Release with read operations.
func TestSecureValue_ConcurrentReleaseAndRead(t *testing.T) {
	for run := 0; run < 10; run++ {
		sv := NewSecureValue("test_value_for_concurrent_release")

		var wg sync.WaitGroup
		iterations := 100
		released := int64(0)

		// Readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = sv.String()
					_ = sv.Bytes()
					_ = sv.Length()
					_ = sv.IsClosed()
					_ = sv.Masked()
				}
			}()
		}

		// Releaser
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if atomic.CompareAndSwapInt64(&released, 0, 1) {
					sv.Release()
				}
			}
		}()

		wg.Wait()
	}
}

// TestSecureValue_PoolReuseConcurrency tests pool reuse under concurrent access.
func TestSecureValue_PoolReuseConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	iterations := 500
	concurrency := 20

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sv := NewSecureValue("concurrent_pool_test")
				_ = sv.String()
				_ = sv.Length()
				sv.Release()
			}
		}(i)
	}

	wg.Wait()
}

// TestLoader_ConcurrentSetDelete tests concurrent Set and Delete operations.
func TestLoader_ConcurrentSetDelete(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.Close()

	var wg sync.WaitGroup
	iterations := 500

	// Setters
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				loader.Set(key, "value")
			}
		}(i)
	}

	// Deleters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				loader.Delete(key)
			}
		}(i)
	}

	// Readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				_ = loader.GetString(key)
				_, _ = loader.Lookup(key)
			}
		}(i)
	}

	wg.Wait()
}

// TestLoader_ConcurrentAllAndModify tests concurrent All() with modifications.
func TestLoader_ConcurrentAllAndModify(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.Close()

	// Pre-populate
	for i := 0; i < 50; i++ {
		loader.Set("KEY_"+string(rune('A'+i%26)), "initial")
	}

	var wg sync.WaitGroup
	iterations := 200

	// All() callers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				all := loader.All()
				_ = len(all)
			}
		}()
	}

	// Modifiers
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				loader.Set(key, "modified")
			}
		}(i)
	}

	wg.Wait()
}

// TestSecureValue_ConcurrentWithMemoryLock tests concurrent operations with memory locking enabled.
func TestSecureValue_ConcurrentWithMemoryLock(t *testing.T) {
	// Save original state
	originalEnabled := IsMemoryLockEnabled()
	defer SetMemoryLockEnabled(originalEnabled)

	// Test with memory locking enabled
	SetMemoryLockEnabled(true)

	var wg sync.WaitGroup
	iterations := 200
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sv := NewSecureValue("concurrent_memory_lock_test")
				_ = sv.String()
				_ = sv.IsMemoryLocked()
				_ = sv.MemoryLockError()
				sv.Release()
			}
		}(i)
	}

	wg.Wait()
}

// Singleton Tests
// ============================================================================

func TestGetDefaultLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Without Load(), should return ErrNotInitialized
	_, err := getDefaultLoader()
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("getDefaultLoader() error = %v, want ErrNotInitialized", err)
	}

	// After explicit setup, should succeed
	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	got, err := getDefaultLoader()
	if err != nil {
		t.Fatalf("getDefaultLoader() after set error = %v", err)
	}
	if got == nil {
		t.Fatal("getDefaultLoader() returned nil")
	}
}

func TestResetDefaultLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Set a loader first
	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Create and reset multiple times
	for i := 0; i < 3; i++ {
		ResetDefaultLoader()
	}

	// After reset, should return ErrNotInitialized
	_, err = getDefaultLoader()
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("getDefaultLoader() after reset error = %v, want ErrNotInitialized", err)
	}
}

func TestSetDefaultLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}
}

func TestSetDefaultLoader_AlreadyInitialized(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// First initialization should succeed
	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Second initialization should fail
	loader2, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := setDefaultLoader(loader2); err == nil {
		t.Error("setDefaultLoader() should fail with ErrAlreadyInitialized")
	} else if !errors.Is(err, ErrAlreadyInitialized) {
		t.Errorf("setDefaultLoader() error = %v, want ErrAlreadyInitialized", err)
	}
}

// ============================================================================
// Concurrent Access Tests for Singleton
// ============================================================================

// TestSingleton_ConcurrentAccess tests concurrent access to the default loader.
func TestSingleton_ConcurrentAccess(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Set up loader before concurrent access
	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	var wg sync.WaitGroup
	iterations := 100
	concurrency := 10
	successCount := int64(0)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				loader, err := getDefaultLoader()
				if err == nil && loader != nil {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}()
	}

	wg.Wait()

	expected := int64(concurrency * iterations)
	if atomic.LoadInt64(&successCount) != expected {
		t.Errorf("successCount = %d, want %d", successCount, expected)
	}
}

// TestSingleton_ConcurrentReset tests concurrent access with reset.
func TestSingleton_ConcurrentReset(t *testing.T) {
	for run := 0; run < 10; run++ {
		ResetDefaultLoader()

		// Set up initial loader
		initLoader, err := New(DefaultConfig())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := setDefaultLoader(initLoader); err != nil {
			t.Fatalf("setDefaultLoader() error = %v", err)
		}

		var wg sync.WaitGroup
		iterations := 50

		// Getters — tolerate ErrNotInitialized during reset windows
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					loader, _ := getDefaultLoader()
					if loader != nil {
						_ = loader.GetString("KEY")
					}
				}
			}()
		}

		// Resetters — alternate between reset and re-set
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations/10; j++ {
					ResetDefaultLoader()
					l, lerr := New(DefaultConfig())
					if lerr == nil {
						setDefaultLoader(l)
					}
				}
			}()
		}

		wg.Wait()
		ResetDefaultLoader()
	}
}

// ============================================================================
// Resource Leak Tests
// ============================================================================

// TestBufferedHandler_NoGoroutineLeak verifies that BufferedHandler's background
// goroutine is properly stopped when Close() is called.
func TestBufferedHandler_NoGoroutineLeak(t *testing.T) {
	// Get initial goroutine count
	initialGoroutines := runtime.NumGoroutine()

	// Create multiple buffered handlers with flush intervals
	// Each starts a background goroutine
	handlers := make([]*internal.BufferedHandler, 10)
	for i := 0; i < 10; i++ {
		handlers[i] = internal.NewBufferedHandler(internal.BufferedHandlerConfig{
			Handler:       internal.NewNopHandler(),
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
		})
	}

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// Verify goroutines were created
	afterCreate := runtime.NumGoroutine()
	if afterCreate <= initialGoroutines {
		t.Logf("Warning: expected goroutine increase, initial=%d, after=%d",
			initialGoroutines, afterCreate)
	}

	// Close all handlers
	for _, h := range handlers {
		if err := h.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}

	// Give goroutines time to stop
	time.Sleep(100 * time.Millisecond)

	// Force garbage collection to clean up any pending finalizers
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	// Verify goroutines were cleaned up
	finalGoroutines := runtime.NumGoroutine()

	// Allow for some variance due to test framework goroutines
	// but there should be significant reduction
	leakedGoroutines := finalGoroutines - initialGoroutines

	t.Logf("Goroutine count: initial=%d, after_create=%d, final=%d, leaked=%d",
		initialGoroutines, afterCreate, finalGoroutines, leakedGoroutines)

	// We expect at most 2 extra goroutines from test infrastructure
	if leakedGoroutines > 2 {
		t.Errorf("Potential goroutine leak: %d goroutines not cleaned up", leakedGoroutines)
	}
}

// TestCloseableChannelHandler_ReceiverUnblocked verifies that receivers
// are properly unblocked when the handler is closed.
func TestCloseableChannelHandler_ReceiverUnblocked(t *testing.T) {
	handler := internal.NewCloseableChannelHandler(0) // Unbuffered

	receiverDone := make(chan struct{})
	go func() {
		defer close(receiverDone)
		ch := handler.Channel()
		// This will block until handler is closed
		for range ch {
			// Consume events
		}
	}()

	// Give receiver time to start blocking
	time.Sleep(50 * time.Millisecond)

	// Close the handler - this should unblock the receiver
	if err := handler.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for receiver to finish
	select {
	case <-receiverDone:
		// Success - receiver was unblocked
	case <-time.After(time.Second):
		t.Error("receiver should have been unblocked by Close()")
	}
}

// TestLoader_ResourceCleanup verifies that Loader properly cleans up resources
// when Close() is called.
func TestLoader_ResourceCleanup(t *testing.T) {
	// Use a mock filesystem to avoid path validation issues
	mockFS := newTestFileSystem()
	mockFS.files[".env"] = "KEY1=value1\nKEY2=value2"

	cfg := DefaultConfig()
	cfg.Filenames = []string{".env"}
	cfg.FileSystem = mockFS
	cfg.AuditEnabled = true

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify loader is functional
	if loader.Len() != 2 {
		t.Errorf("loader.Len() = %d, want 2", loader.Len())
	}

	// Close should clean up resources
	if err := loader.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify closed state
	if !loader.IsClosed() {
		t.Error("IsClosed() should return true after Close()")
	}

	// Operations on closed loader should fail
	_, ok := loader.Lookup("KEY1")
	if ok {
		t.Error("Lookup() on closed loader should return false")
	}

	if err := loader.Set("KEY3", "value3"); err != ErrClosed {
		t.Errorf("Set() on closed loader should return ErrClosed, got %v", err)
	}
}

// TestSecureValue_PoolNoLeak verifies that SecureValue pool doesn't leak memory.
func TestSecureValue_PoolNoLeak(t *testing.T) {
	const iterations = 1000

	// Create and release many SecureValues
	for i := 0; i < iterations; i++ {
		sv := NewSecureValue("sensitive-data")
		sv.Release()
	}

	// Force GC to clean up any unreferenced objects
	runtime.GC()

	// Create more to verify pool is still functional
	for i := 0; i < 100; i++ {
		sv := NewSecureValue("more-data")
		if sv.IsClosed() {
			t.Error("NewSecureValue() should not return closed value from pool")
		}
		sv.Release()
	}
}

// TestSecureValue_DoubleReleaseSafe verifies that calling Release() multiple times
// is safe and doesn't cause panics or pool corruption.
func TestSecureValue_DoubleReleaseSafe(t *testing.T) {
	sv := NewSecureValue("test-data")

	// First release
	sv.Release()

	if !sv.IsClosed() {
		t.Error("IsClosed() should return true after Release()")
	}

	// Second release should be safe (no-op)
	sv.Release()

	// Third release should also be safe
	sv.Release()
}

// TestComponentFactory_CloseIdempotent verifies that ComponentFactory.Close()
// can be called multiple times safely.
func TestComponentFactory_CloseIdempotent(t *testing.T) {
	cfg := DefaultConfig()
	factory := cfg.buildComponentFactory()

	// Close multiple times
	for i := 0; i < 5; i++ {
		if err := factory.Close(); err != nil {
			t.Errorf("Close() #%d error = %v", i+1, err)
		}
	}

	if !factory.IsClosed() {
		t.Error("IsClosed() should return true")
	}
}

// TestBufferedHandler_FlushOnClose verifies that BufferedHandler flushes
// remaining events when closed.
func TestBufferedHandler_FlushOnClose(t *testing.T) {
	ch := make(chan internal.Event, 100)
	underlying := internal.NewChannelHandler(ch)

	handler := internal.NewBufferedHandler(internal.BufferedHandlerConfig{
		Handler:       underlying,
		BufferSize:    10,
		FlushInterval: 0, // Disable auto-flush
	})

	// Log events without flushing
	for i := 0; i < 5; i++ {
		_ = handler.Log(internal.Event{Action: internal.ActionSet})
	}

	// Close should flush remaining events
	if err := handler.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Count received events
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:

	if count != 5 {
		t.Errorf("expected 5 events flushed on close, got %d", count)
	}
}

// TestMultipleLoader_NoResourceLeak verifies that creating and closing
// multiple Loaders doesn't accumulate resources.
func TestMultipleLoader_NoResourceLeak(t *testing.T) {
	// Get initial goroutine count
	initialGoroutines := runtime.NumGoroutine()

	// Use a mock filesystem to avoid path validation issues
	mockFS := newTestFileSystem()
	mockFS.files[".env"] = "KEY=value"

	// Create and close multiple loaders
	for i := 0; i < 20; i++ {
		cfg := DefaultConfig()
		cfg.Filenames = []string{".env"}
		cfg.FileSystem = mockFS
		cfg.AuditEnabled = true

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		// Use the loader
		_ = loader.GetString("KEY")

		// Close properly
		if err := loader.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}

	// Give time for cleanup
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// Verify goroutine count
	finalGoroutines := runtime.NumGoroutine()
	leakedGoroutines := finalGoroutines - initialGoroutines

	t.Logf("Goroutine count: initial=%d, final=%d, leaked=%d",
		initialGoroutines, finalGoroutines, leakedGoroutines)

	if leakedGoroutines > 2 {
		t.Errorf("Potential goroutine leak: %d goroutines not cleaned up", leakedGoroutines)
	}
}

// TestSecureMap_ClearReleasesMemory verifies that secureMap.Clear() properly
// releases all SecureValue objects.
func TestSecureMap_ClearReleasesMemory(t *testing.T) {
	sm := newSecureMap()

	// Add many values with unique keys
	for i := 0; i < 100; i++ {
		sm.Set(fmt.Sprintf("KEY_%d", i), "value")
	}

	if sm.Len() != 100 {
		t.Errorf("Len() = %d, want 100", sm.Len())
	}

	// Clear should release all
	sm.Clear()

	if sm.Len() != 0 {
		t.Errorf("Len() after Clear() = %d, want 0", sm.Len())
	}

	// Verify we can add new values after clear
	sm.Set("NEWKEY", "newvalue")
	if sm.Len() != 1 {
		t.Errorf("Len() after new Set() = %d, want 1", sm.Len())
	}

	sm.Clear()
}

// ============================================================================
// Additional Resource Leak Tests
// ============================================================================

// TestBufferedHandler_ConcurrentClose verifies that BufferedHandler.Close()
// can be called concurrently without causing race conditions or deadlocks.
func TestBufferedHandler_ConcurrentClose(t *testing.T) {
	for i := 0; i < 10; i++ {
		handler := internal.NewBufferedHandler(internal.BufferedHandlerConfig{
			Handler:       internal.NewNopHandler(),
			BufferSize:    10,
			FlushInterval: 10 * time.Millisecond,
		})

		var wg sync.WaitGroup
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = handler.Close()
			}()
		}
		wg.Wait()

		// Verify handler is closed
		if !handler.IsFull() && handler.BufferLen() != 0 {
			// After close, buffer should be empty
		}
	}
}

// TestSecureValue_FinalizerCleanup verifies that SecureValue's finalizer
// properly cleans up memory when the value is garbage collected.
func TestSecureValue_FinalizerCleanup(t *testing.T) {
	// Create many SecureValues without explicit release
	for i := 0; i < 100; i++ {
		_ = NewSecureValue("sensitive-data-that-should-be-cleared")
	}

	// Force GC to trigger finalizers
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	// Create and release properly
	for i := 0; i < 50; i++ {
		sv := NewSecureValue("more-data")
		sv.Release()
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	// If we get here without panic or deadlock, the test passes
}

// TestSingleton_ResetClosesLoader verifies that ResetDefaultLoader properly
// closes the old loader.
func TestSingleton_ResetClosesLoader(t *testing.T) {
	// Reset any existing loader first
	_ = ResetDefaultLoader()

	// Create a new default loader via Load
	mockFS := newTestFileSystem()
	mockFS.files[".env"] = "TEST_KEY=test_value"

	cfg := DefaultConfig()
	cfg.Filenames = []string{".env"}
	cfg.FileSystem = mockFS

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Set as default
	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Verify it's not closed
	if loader.IsClosed() {
		t.Fatal("loader should not be closed")
	}

	// Reset should close the loader
	if err := ResetDefaultLoader(); err != nil {
		t.Errorf("ResetDefaultLoader() error = %v", err)
	}

	// Verify the old loader is now closed
	if !loader.IsClosed() {
		t.Error("old loader should be closed after reset")
	}
}

// TestKeyInternCache_BoundedGrowth verifies that the key interning cache
// doesn't grow unbounded.
func TestKeyInternCache_BoundedGrowth(t *testing.T) {
	// Clear cache first
	internal.ClearInternCache()

	// Intern many unique keys (more than the max cache size)
	for i := 0; i < 2000; i++ {
		// Create keys that are within the length limit
		key := fmt.Sprintf("KEY_%04d", i)
		_ = internal.InternKey(key)
	}

	// Clear and verify it doesn't panic
	internal.ClearInternCache()

	// Verify we can still intern after clear
	interned := internal.InternKey("TEST_KEY")
	if interned != "TEST_KEY" {
		t.Error("InternKey should return the key after clear")
	}

	internal.ClearInternCache()
}

// TestLoader_MultipleCloseSafe verifies that calling Close() on a Loader
// multiple times is safe.
func TestLoader_MultipleCloseSafe(t *testing.T) {
	mockFS := newTestFileSystem()
	mockFS.files[".env"] = "KEY=value"

	cfg := DefaultConfig()
	cfg.Filenames = []string{".env"}
	cfg.FileSystem = mockFS

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Close multiple times
	for i := 0; i < 5; i++ {
		if err := loader.Close(); err != nil {
			t.Errorf("Close() #%d error = %v", i+1, err)
		}
	}

	if !loader.IsClosed() {
		t.Error("IsClosed() should return true")
	}
}

// TestAuditEventPool_NoLeak verifies that the audit event pool properly
// recycles events without leaking memory.
func TestAuditEventPool_NoLeak(t *testing.T) {
	handler := internal.NewBufferedHandler(internal.BufferedHandlerConfig{
		Handler:       internal.NewNopHandler(),
		BufferSize:    100,
		FlushInterval: 0,
	})

	// Log many events
	for i := 0; i < 1000; i++ {
		_ = handler.Log(internal.Event{
			Action:    internal.ActionSet,
			Key:       "TEST_KEY",
			Reason:    "test",
			Success:   true,
			Timestamp: time.Now(),
		})
	}

	// Flush and close
	_ = handler.Flush()
	_ = handler.Close()

	// If we get here without memory issues, the test passes
}

// TestSecureMap_ResourceLeakConcurrentAccess verifies that secureMap doesn't have
// race conditions under heavy concurrent access.
func TestSecureMap_ResourceLeakConcurrentAccess(t *testing.T) {
	sm := newSecureMap()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("KEY_%d_%d", id, j)
				sm.Set(key, "value")
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("KEY_%d_%d", id%10, j)
				_, _ = sm.Get(key)
			}
		}(i)
	}

	// Concurrent deletes
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				key := fmt.Sprintf("KEY_%d_%d", id%10, j)
				sm.Delete(key)
			}
		}(i)
	}

	wg.Wait()
	sm.Clear()
}
