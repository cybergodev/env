package env

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Concurrent Access Tests for Loader
// ============================================================================

// TestLoader_Concurrent exercises the loader under a variety of concurrent
// operation mixes. It replaces five former single-scenario tests
// (ConcurrentGet, ConcurrentSet, ConcurrentReadWrite, ConcurrentSetDelete,
// ConcurrentAllAndModify). Each scenario asserts the values observed during
// the concurrent phase as well as the final post-wait state.
func TestLoader_Concurrent(t *testing.T) {
	scenarios := []struct {
		name   string
		write  bool // goroutine performs Set/Delete (needs OverwriteExisting)
		fn     func(l *Loader)
		verify func(t *testing.T, l *Loader) // post-concurrency assertions
	}{
		{"read GetString/Lookup/GetSecure", false, func(l *Loader) {
			for j := 0; j < 500; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				if v := l.GetString(key); v != "initial" {
					t.Errorf("GetString(%q) = %q, want %q", key, v, "initial")
					return
				}
				if v, ok := l.Lookup(key); !ok || v != "initial" {
					t.Errorf("Lookup(%q) = (%q, %v), want (%q, true)", key, v, ok, "initial")
					return
				}
				sv := l.GetSecure(key)
				if sv == nil {
					t.Errorf("GetSecure(%q) = nil, want a value", key)
					return
				}
				if got := sv.Reveal(); got != "initial" {
					t.Errorf("GetSecure(%q).Reveal() = %q, want %q", key, got, "initial")
					sv.Release()
					return
				}
				sv.Release()
			}
		}, nil},
		{"write Set", true, func(l *Loader) {
			for j := 0; j < 500; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				if err := l.Set(key, "value"); err != nil {
					t.Errorf("Set(%q) error = %v, want nil", key, err)
					return
				}
				// Read-back under concurrent writes of the same value.
				if v, ok := l.Lookup(key); !ok || v != "value" {
					t.Errorf("Lookup(%q) = (%q, %v), want (%q, true)", key, v, ok, "value")
					return
				}
			}
		}, func(t *testing.T, l *Loader) {
			for i := 0; i < 26; i++ {
				key := "KEY_" + string(rune('A'+i))
				if v, ok := l.Lookup(key); !ok || v != "value" {
					t.Errorf("Lookup(%q) = (%q, %v), want (%q, true)", key, v, ok, "value")
				}
			}
		}},
		{"read Keys/All/Len", false, func(l *Loader) {
			for j := 0; j < 200; j++ {
				if keys := l.Keys(); len(keys) != 26 {
					t.Errorf("Keys() returned %d keys, want 26", len(keys))
					return
				}
				if all := l.All(); len(all) != 26 {
					t.Errorf("All() returned %d keys, want 26", len(all))
					return
				}
				if n := l.Len(); n != 26 {
					t.Errorf("Len() = %d, want 26", n)
					return
				}
			}
		}, nil},
		{"write Delete", true, func(l *Loader) {
			for j := 0; j < 300; j++ {
				key := "KEY_" + string(rune('A'+j%26))
				if err := l.Delete(key); err != nil {
					t.Errorf("Delete(%q) error = %v, want nil", key, err)
					return
				}
				// Nothing re-inserts, so a just-deleted key must be gone.
				if _, ok := l.Lookup(key); ok {
					t.Errorf("Lookup(%q) hit after Delete, want miss", key)
					return
				}
			}
		}, func(t *testing.T, l *Loader) {
			for i := 0; i < 26; i++ {
				if _, ok := l.Lookup("KEY_" + string(rune('A'+i))); ok {
					t.Error("key still present after concurrent Delete")
				}
			}
		}},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.OverwriteExisting = true
			loader, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			// Pre-populate all 26 keys with "initial" so every scenario
			// starts from a known state; the scenarios assert against these
			// values during and after the concurrent phase.
			for i := 0; i < 26; i++ {
				if err := loader.Set("KEY_"+string(rune('A'+i)), "initial"); err != nil {
					t.Fatalf("setup Set() error = %v", err)
				}
			}

			var wg sync.WaitGroup
			concurrency := 10
			wg.Add(concurrency)
			for k := 0; k < concurrency; k++ {
				go func() {
					defer wg.Done()
					sc.fn(loader)
				}()
			}
			wg.Wait()

			if sc.verify != nil {
				sc.verify(t, loader)
			}
		})
	}
}

// TestLoader_ConcurrentFastPaths stresses the lock-free single-key read path
// (Lookup/GetSecure via the atomic closed flag) and the read-lock Set fast
// path (OverwriteExisting + no AutoApply) together with concurrent Close.
// The Set write-lock slow path (DefaultConfig: no overwrite) is the second
// scenario — it merged here from the former ConcurrentWithClose test, which
// had no assertions.
// Invariants: no data race, no panic, and every observed value is one of the
// written ones (graceful degradation — reads that lose the race with Close
// see the cleared state).
func TestLoader_ConcurrentFastPaths(t *testing.T) {
	t.Run("set fast path", func(t *testing.T) {
		for run := 0; run < 20; run++ {
			cfg := DefaultConfig()
			cfg.OverwriteExisting = true // activates the Set read-lock fast path
			loader, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}

			var wg sync.WaitGroup

			// Writers using the Set fast path
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 500; j++ {
						if err := loader.Set("KEY", "value"); err != nil && !errors.Is(err, ErrClosed) {
							t.Errorf("Set: unexpected error: %v", err)
							return
						}
					}
				}()
			}

			// Lock-free single-key readers
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 500; j++ {
						if v, ok := loader.Lookup("KEY"); ok && v != "value" {
							t.Errorf("Lookup: got %q, want %q", v, "value")
							return
						}
						if sv := loader.GetSecure("KEY"); sv != nil {
							sv.Release()
						}
					}
				}()
			}

			// Closer racing with the paths above. The delay varies by run so
			// Close lands before, in the middle of, and after the writer loop
			// — a fixed instant would exercise only one point of the
			// Set-fast-path × Close race window.
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(time.Duration(run%10) * 100 * time.Microsecond)
				_ = loader.Close()
			}()

			wg.Wait()

			// After Close completes, all single-key reads must report absence.
			if _, ok := loader.Lookup("KEY"); ok {
				t.Error("Lookup after Close should miss")
			}
			if loader.GetSecure("KEY") != nil {
				t.Error("GetSecure after Close should return nil")
			}
		}
	})

	t.Run("set write-lock slow path", func(t *testing.T) {
		// DefaultConfig leaves OverwriteExisting=false, so every Set takes
		// the loader write lock (env.go Set slow path): the first call
		// inserts, the rest hit the Has-based skip branch.
		for run := 0; run < 10; run++ {
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}

			var wg sync.WaitGroup
			const iterations = 100
			closed := int64(0)

			// Operations
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < iterations; j++ {
						if err := loader.Set("KEY", "value"); err != nil && !errors.Is(err, ErrClosed) {
							t.Errorf("Set: unexpected error: %v", err)
							return
						}
						// Missing (pre-insert or post-Close) yields "", set
						// yields "value" — nothing else is ever written.
						if v := loader.GetString("KEY"); v != "value" && v != "" {
							t.Errorf("GetString: got %q, want %q or empty", v, "value")
							return
						}
						_ = loader.Keys()
					}
				}()
			}

			// Closer: CAS-once, so Close happens at most once per run.
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					if atomic.CompareAndSwapInt64(&closed, 0, 1) {
						_ = loader.Close()
					}
				}
			}()

			wg.Wait()

			if !loader.IsClosed() {
				t.Error("IsClosed() = false after closer goroutine ran")
			}
		}
	})
}

// TestLoader_ConcurrentSetTwoValueInvariant pins the write linearizability
// invariant under two distinct concurrent values: readers must only ever
// observe "A" or "B" — never a torn, stale-variant, or third state.
func TestLoader_ConcurrentSetTwoValueInvariant(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Seed the key so readers always find it (nothing deletes it).
	if err := loader.Set("TOGGLE_KEY", "A"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	// Writers: each goroutine alternates between the two values.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				value := "A"
				if (id+j)%2 == 1 {
					value = "B"
				}
				if err := loader.Set("TOGGLE_KEY", value); err != nil {
					t.Errorf("Set error = %v, want nil", err)
					return
				}
			}
		}(i)
	}

	// Readers: the value is always one of the two written strings.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if v, ok := loader.Lookup("TOGGLE_KEY"); !ok || (v != "A" && v != "B") {
					t.Errorf("Lookup = (%q, %v), want one of A/B present", v, ok)
					return
				}
				if v := loader.GetString("TOGGLE_KEY"); v != "A" && v != "B" {
					t.Errorf("GetString = %q, want one of A/B", v)
					return
				}
			}
		}()
	}

	wg.Wait()

	if v, _ := loader.Lookup("TOGGLE_KEY"); v != "A" && v != "B" {
		t.Errorf("final Lookup = %q, want one of A/B", v)
	}
}

// ============================================================================
// Concurrent Access Tests for SecureMap
// ============================================================================

// TestSecureMap_Concurrent exercises all secureMap operations under concurrent
// access. Each scenario targets a specific operation mix; together they replace
// six former single-scenario tests (ConcurrentAccess, ConcurrentSetAll,
// ConcurrentDeleteAndRead, ConcurrentToMapAndModify, ConcurrentKeysAndClear,
// ResourceLeakConcurrentAccess).
func TestSecureMap_Concurrent(t *testing.T) {
	makeKey := func(j int) string { return "KEY_" + string(rune('A'+j%26)) }

	scenarios := []struct {
		name string
		fn   func(sm *secureMap)
	}{
		{
			name: "Set and Get",
			fn: func(sm *secureMap) {
				for j := 0; j < 500; j++ {
					key := makeKey(j)
					sm.Set(key, "value")
					sm.Get(key)
				}
			},
		},
		{
			name: "GetSecure and Delete",
			fn: func(sm *secureMap) {
				for j := 0; j < 500; j++ {
					key := makeKey(j)
					sm.GetSecure(key)
					sm.Delete(key)
				}
			},
		},
		{
			name: "SetAll",
			fn: func(sm *secureMap) {
				for j := 0; j < 100; j++ {
					sm.SetAll(map[string]string{"KEY_A": "a", "KEY_B": "b"})
				}
			},
		},
		{
			name: "ToMap and Keys",
			fn: func(sm *secureMap) {
				for j := 0; j < 200; j++ {
					_ = sm.ToMap()
					_ = sm.Keys()
					_ = sm.Len()
				}
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			sm := newSecureMap()
			// Pre-populate so readers have data
			for i := 0; i < 26; i++ {
				sm.Set(makeKey(i), "initial")
			}

			var wg sync.WaitGroup
			concurrency := 10
			wg.Add(concurrency)
			for k := 0; k < concurrency; k++ {
				go func() {
					defer wg.Done()
					sc.fn(sm)
				}()
			}
			wg.Wait()
		})
	}
}

// ============================================================================
// Concurrent Access Tests for SecureValue
// ============================================================================

// TestSecureValue_ConcurrentAccess races concurrent readers (String, Bytes,
// Length, Masked, IsClosed) against each teardown path: no closer, Close, and
// Release (which also returns the value to secureValuePool while readers may
// still hold it). It merges the former ConcurrentAccess, ConcurrentWithClose,
// and ConcurrentReleaseAndRead tests; unlike them, it asserts the observed
// values: String is either the masked-open form or "[CLOSED]", and Length is
// either the full length or 0.
func TestSecureValue_ConcurrentAccess(t *testing.T) {
	const value = "test_value"

	cases := []struct {
		name    string
		closeOp func(sv *SecureValue) // nil: value is never closed
	}{
		{name: "no closer", closeOp: nil},
		{name: "Close", closeOp: func(sv *SecureValue) { _ = sv.Close() }},
		{name: "Release", closeOp: func(sv *SecureValue) { sv.Release() }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for run := 0; run < 10; run++ {
				sv := NewSecureValue(value)

				var wg sync.WaitGroup
				const iterations = 100
				closed := int64(0)

				// Readers
				for i := 0; i < 5; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for j := 0; j < iterations; j++ {
							if s := sv.String(); s != "[CLOSED]" && !strings.HasPrefix(s, "[SECURE:") {
								t.Errorf("String() = %q, want [SECURE:..] or [CLOSED]", s)
								return
							}
							if b := sv.Bytes(); b != nil && len(b) != len(value) {
								t.Errorf("Bytes() length = %d, want %d or nil (closed)", len(b), len(value))
								return
							}
							if n := sv.Length(); n != 0 && n != len(value) {
								t.Errorf("Length() = %d, want %d or 0 (closed)", n, len(value))
								return
							}
							_ = sv.Masked()
							_ = sv.IsClosed()
						}
					}()
				}

				// Closer: CAS-once, so teardown happens at most once per run.
				if tc.closeOp != nil {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for j := 0; j < iterations; j++ {
							if atomic.CompareAndSwapInt64(&closed, 0, 1) {
								tc.closeOp(sv)
							}
						}
					}()
				}

				wg.Wait()

				if tc.closeOp != nil {
					if !sv.IsClosed() {
						t.Error("IsClosed() = false after closeOp ran")
					}
					if got := sv.Length(); got != 0 {
						t.Errorf("Length() after close = %d, want 0", got)
					}
				} else {
					sv.Release() // best-effort: return the open value to the pool
				}
			}
		})
	}
}

// ============================================================================
// Concurrent Access Tests for Parser Registry
// ============================================================================

// TestCreateParsers_FactoryRegistersParser verifies that a parser factory may
// call RegisterParser from inside its callback without deadlocking.
// createParsers previously invoked factory callbacks while holding
// globalParserRegistry.mu.RLock; RegisterParser needs the write lock, and
// RWMutex is not reentrant, so this self-deadlocked.
func TestCreateParsers_FactoryRegistersParser(t *testing.T) {
	outerFormat := nextTestFormat()
	innerFormat := nextTestFormat()

	// Insert via the internal map (like registerBuiltin) so the test can
	// remove its entries afterwards — RegisterParser is permanent.
	globalParserRegistry.mu.Lock()
	globalParserRegistry.factories[outerFormat] = func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		// Must not deadlock: the write lock requires the read lock held by
		// the caller to be released first.
		if err := RegisterParser(innerFormat, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return nil, nil
		}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	globalParserRegistry.mu.Unlock()

	defer func() {
		globalParserRegistry.mu.Lock()
		delete(globalParserRegistry.factories, outerFormat)
		delete(globalParserRegistry.factories, innerFormat)
		globalParserRegistry.mu.Unlock()
	}()

	// Guard against a regression re-introducing the deadlock: fail the test
	// instead of hanging the whole suite.
	done := make(chan error, 1)
	go func() {
		cfg := DefaultConfig()
		factory := cfg.buildComponentFactory()
		defer func() { _ = factory.Close() }() // best-effort cleanup
		_, err := createParsers(cfg, factory)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("createParsers() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("createParsers deadlocked: factory callback blocked on registry write lock")
	}
}

// ============================================================================
// Concurrent Access Tests for ComponentFactory
// ============================================================================

// TestComponentFactory_ConcurrentClose races component access against zero or
// more concurrent Close callers. It merges the former ConcurrentAccess test
// (closers=0: pure concurrent access) into ConcurrentClose (closers=3).
func TestComponentFactory_ConcurrentClose(t *testing.T) {
	for _, closers := range []int{0, 3} {
		t.Run(fmt.Sprintf("closers=%d", closers), func(t *testing.T) {
			for run := 0; run < 10; run++ {
				cfg := DefaultConfig()
				factory := cfg.buildComponentFactory()

				var wg sync.WaitGroup
				const iterations = 100

				// Users
				for i := 0; i < 5; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for j := 0; j < iterations; j++ {
							_ = factory.Validator()
							_ = factory.Auditor()
							_ = factory.Expander()
							_ = factory.IsClosed()
						}
					}()
				}

				// Closers
				for i := 0; i < closers; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for j := 0; j < iterations/10; j++ {
							_ = factory.Close() // best-effort cleanup
						}
					}()
				}

				wg.Wait()
			}
		})
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
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Pre-populate with many variables. Keys must be pattern-valid: the
	// previous rune-suffix construction silently failed validation for ~75%
	// of iterations (control/punct chars), populating ~246 of 1000 keys.
	for i := 0; i < 1000; i++ {
		if err := loader.Set(fmt.Sprintf("STRESS_KEY_%d", i), "value"); err != nil {
			t.Fatalf("setup Set() error = %v", err)
		}
	}

	var wg sync.WaitGroup
	iterations := 10000
	concurrency := 50

	// Mixed operations. Keys reuse the numeric suffixes populated above —
	// strconv.Itoa, not rune casts, so Set/Lookup actually hit the map.
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "STRESS_KEY_" + strconv.Itoa(j%1000)
				switch j % 5 {
				case 0:
					_ = loader.Set(key, "new_value") // stress loop; valid key cannot fail
				case 1:
					_ = loader.GetString(key)
				case 2:
					_ = loader.Keys()
				case 3:
					_ = loader.Len()
				case 4:
					_, _ = loader.Lookup(key)
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
	// Apply() writes the loader's variables into the process environment;
	// unset the two keys it may touch so later tests see a clean env.
	t.Cleanup(func() {
		_ = os.Unsetenv("KEY1") // best-effort cleanup of Apply's env writes
		_ = os.Unsetenv("KEY2")
	})

	t.Run("validate reports missing required keys", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RequiredKeys = []string{"KEY1", "KEY2"}
		loader, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		var wg sync.WaitGroup
		const iterations = 100

		// Concurrent Apply. Only KEY1 is ever Set, so KEY2 stays missing.
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					if err := loader.Set("KEY1", "value"); err != nil {
						t.Errorf("Set(KEY1) error = %v, want nil", err)
						return
					}
					_ = loader.Apply() // concurrent-apply stress; Validate below carries the assertions
				}
			}()
		}

		// Concurrent Validate: KEY2 is never Set, so every call must fail.
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					if err := loader.Validate(); err == nil {
						t.Error("Validate() = nil, want missing-required-key error (KEY2 never set)")
						return
					}
				}
			}()
		}

		wg.Wait()

		if err := loader.Validate(); err == nil {
			t.Error("final Validate() = nil, want missing-required-key error")
		}
	})

	t.Run("validate succeeds when all required keys set", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RequiredKeys = []string{"KEY1", "KEY2"}
		loader, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		// Success path: seed every required key before concurrency starts.
		for _, key := range []string{"KEY1", "KEY2"} {
			if err := loader.Set(key, "value"); err != nil {
				t.Fatalf("setup Set(%s) error = %v", key, err)
			}
		}

		var wg sync.WaitGroup
		const iterations = 100

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = loader.Apply() // keep Apply racing Validate, as in the failure variant
				}
			}()
		}

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					if err := loader.Validate(); err != nil {
						t.Errorf("Validate() = %v, want nil (all required keys set)", err)
						return
					}
				}
			}()
		}

		wg.Wait()

		if err := loader.Validate(); err != nil {
			t.Errorf("final Validate() = %v, want nil", err)
		}
	})
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

// TestLoader_ConcurrentAutoApply exercises Set's write-lock slow path under
// AutoApply (env.go): Setenv-first-then-store, the existing-env skip under
// !OverwriteExisting, markAppliedLocked, and Delete's per-key appliedKeys
// bookkeeping — all under concurrent writers, deleters and readers on the
// same keys (the race detector validates the shared state; the final
// per-key sequence pins the observable contract deterministically).
func TestLoader_ConcurrentAutoApply(t *testing.T) {
	for _, overwrite := range []bool{true, false} {
		name := "overwrite"
		if !overwrite {
			name = "no-overwrite"
		}
		t.Run(name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.AutoApply = true
			cfg.OverwriteExisting = overwrite
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			const keys = 8
			prefix := "FIX001_AUTO_" + strconv.FormatInt(time.Now().UnixNano()%1000, 10) + "_"
			t.Cleanup(func() {
				for i := 0; i < keys; i++ {
					_ = os.Unsetenv(prefix + strconv.Itoa(i)) // best-effort cleanup
				}
			})

			var wg sync.WaitGroup
			for i := 0; i < keys; i++ {
				key := prefix + strconv.Itoa(i)
				wg.Add(3)
				go func() { // writer: drives the Setenv-first slow path
					defer wg.Done()
					for j := 0; j < 50; j++ {
						if err := loader.Set(key, "v"+strconv.Itoa(j)); err != nil {
							t.Errorf("Set(%q) error = %v", key, err)
							return
						}
					}
				}()
				go func() { // deleter: races the appliedKeys bookkeeping
					defer wg.Done()
					for j := 0; j < 25; j++ {
						if err := loader.Delete(key); err != nil {
							t.Errorf("Delete(%q) error = %v", key, err)
							return
						}
					}
				}()
				go func() { // reader: concurrent read-back
					defer wg.Done()
					for j := 0; j < 50; j++ {
						loader.Lookup(key)
					}
				}()
			}
			wg.Wait()

			if !loader.IsApplied() {
				t.Error("IsApplied() = false after concurrent AutoApply Sets")
			}

			// Deterministic final sequence per key: clear, apply, verify both
			// stores agree, then remove and verify both are gone.
			for i := 0; i < keys; i++ {
				key := prefix + strconv.Itoa(i)
				_ = loader.Delete(key)
				if err := loader.Set(key, "final"); err != nil {
					t.Fatalf("Set(%q) error = %v", key, err)
				}
				if v, ok := loader.Lookup(key); !ok || v != "final" {
					t.Errorf("Lookup(%q) = (%q, %v), want (\"final\", true)", key, v, ok)
				}
				if v, ok := os.LookupEnv(key); !ok || v != "final" {
					t.Errorf("process env for %q = (%q, %v), want (\"final\", true) after AutoApply Set", key, v, ok)
				}
				if err := loader.Delete(key); err != nil {
					t.Fatalf("Delete(%q) error = %v", key, err)
				}
				if _, ok := os.LookupEnv(key); ok {
					t.Errorf("Delete(%q) left the key live in the process env", key)
				}
			}
		})
	}
}

// Singleton Tests
// ============================================================================

// TestSingleton_Lifecycle walks the default-loader lifecycle end to end in
// one place (it replaces four former single-scenario tests: GetDefaultLoader,
// ResetDefaultLoader, SetDefaultLoader_AlreadyInitialized and
// ResetClosesLoader): uninitialized access errors, initialization succeeds,
// re-initialization fails with ErrAlreadyInitialized, reset closes the old
// loader, and repeated resets keep reporting ErrNotInitialized.
func TestSingleton_Lifecycle(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

	// Without Load(), should return ErrNotInitialized.
	_, err := getDefaultLoader()
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("getDefaultLoader() error = %v, want ErrNotInitialized", err)
	}

	// After explicit setup, should succeed.
	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}
	got, err := getDefaultLoader()
	if err != nil || got == nil {
		t.Fatalf("getDefaultLoader() after set = (%v, %v), want a loader", got, err)
	}

	// Second initialization should fail with ErrAlreadyInitialized.
	loader2, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := setDefaultLoader(loader2); err == nil {
		t.Error("setDefaultLoader() should fail with ErrAlreadyInitialized")
	} else if !errors.Is(err, ErrAlreadyInitialized) {
		t.Errorf("setDefaultLoader() error = %v, want ErrAlreadyInitialized", err)
	}

	// Reset should close the old loader and leave no default behind.
	if err := ResetDefaultLoader(); err != nil {
		t.Fatalf("ResetDefaultLoader() error = %v", err)
	}
	if !loader.IsClosed() {
		t.Error("old loader should be closed after reset")
	}

	// Repeated resets are safe; access errors again.
	for i := 0; i < 3; i++ {
		if err := ResetDefaultLoader(); err != nil {
			t.Fatalf("ResetDefaultLoader() #%d error = %v", i+1, err)
		}
	}
	if _, err := getDefaultLoader(); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("getDefaultLoader() after reset error = %v, want ErrNotInitialized", err)
	}
}

// ============================================================================
// Concurrent Access Tests for Singleton
// ============================================================================

// TestSingleton_ConcurrentAccess tests concurrent access to the default loader.
func TestSingleton_ConcurrentAccess(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

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
		resetDefaultLoader()

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
					resetDefaultLoader()
					l, lerr := New(DefaultConfig())
					if lerr == nil {
						_ = setDefaultLoader(l) // ErrAlreadyInitialized expected when racing other resets
					}
				}
			}()
		}

		wg.Wait()
		resetDefaultLoader()
	}
}

// ============================================================================
// Resource Leak Tests
// ============================================================================

// (TestLoader_ResourceCleanup was removed: Close/IsClosed and the closed-
// loader operation matrix are pinned by TestLoader_CloseAndIsClosed and
// TestLoader_ClosedBehavior in loader_test.go, and closing audit-enabled
// loaders by TestMultipleLoader_NoResourceLeak below.)

// TestComponentFactory_CloseIdempotent is covered by TestComponentFactory's
// "Close" + "IsClosed" subtests in loader_test.go.

// TestBufferedHandler_FlushOnClose verifies that BufferedHandler flushes
// remaining events when closed.
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

// (TestSecureMap_ClearReleasesMemory was removed: the clear semantics — Len
// drops to 0 and the map accepts new writes — are pinned by TestSecureMap's
// "clear" case in secure_test.go.)

// ============================================================================
// Additional Resource Leak Tests
// ============================================================================

// TestSingleton_ResetClosesLoader is covered by TestSingleton_Lifecycle's
// reset phase (old loader closed, ErrNotInitialized afterwards).

// TestLoader_MultipleCloseSafe verifies that calling Close() on a Loader
// multiple times is safe.
// TestLoader_MultipleCloseSafe is covered by TestLoader_CloseAndIsClosed in
// loader_test.go (second-close idempotency).

// TestUnmarshalMap_ConcurrentSharedFactory exercises the package-level parse
// path (UnmarshalMap → parseString), which builds a parser on the
// lazily-initialized SHARED ComponentFactory (sharedParseFactoryOnce). The
// factory's validator, auditor, and expander are shared by every call, so
// this checks that sharing under concurrency: the expander's pooled
// visited/lookup maps, the intern cache, and the scanner buffer pool all
// cycle while concurrent parses run.
func TestUnmarshalMap_ConcurrentSharedFactory(t *testing.T) {
	const data = `SHARED_BASE=prod
SHARED_DB=${SHARED_BASE}_db
SHARED_URL=${SHARED_BASE}://example.com
# comment line
`
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				m, err := UnmarshalMap(data)
				if err != nil {
					t.Errorf("UnmarshalMap: %v", err)
					return
				}
				if m["SHARED_DB"] != "prod_db" {
					t.Errorf("SHARED_DB = %q, want %q", m["SHARED_DB"], "prod_db")
					return
				}
				if m["SHARED_URL"] != "prod://example.com" {
					t.Errorf("SHARED_URL = %q, want %q", m["SHARED_URL"], "prod://example.com")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestParserRegistry_ConcurrentRegisterAndCreate races RegisterParser (which
// mutates the registry map and flips hasCustom under the write lock) against
// concurrent createParsers, which reads hasCustom atomically and snapshots
// the map under the read lock. A Loader constructed during the race may miss
// a parser registered after its snapshot — documented: register parsers
// during init — but the registry access itself must be race-free and every
// snapshot must yield a valid parser set.
func TestParserRegistry_ConcurrentRegisterAndCreate(t *testing.T) {
	// Take all formats up front: nextTestFormat is atomic, but hoisting the
	// calls keeps the registrar goroutine linear.
	formats := make([]FileFormat, 8)
	for i := range formats {
		formats[i] = nextTestFormat()
	}
	defer func() {
		// RegisterParser is permanent; remove the entries via the internal
		// map (under its lock) so later tests see a clean registry, matching
		// TestCreateParsers_FactoryRegistersParser's cleanup.
		globalParserRegistry.mu.Lock()
		for _, f := range formats {
			delete(globalParserRegistry.factories, f)
		}
		globalParserRegistry.mu.Unlock()
	}()

	var wg sync.WaitGroup

	// Registrar: flips hasCustom and grows the map while constructors read it.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, f := range formats {
			if err := RegisterParser(f, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
				return nil, nil
			}); err != nil {
				t.Errorf("RegisterParser: %v", err)
				return
			}
		}
	}()

	// Constructors: exercise both the hasCustom fast path and the locked
	// snapshot path while the registry mutates.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cfg := DefaultConfig()
				factory := cfg.buildComponentFactory()
				parsers, err := createParsers(cfg, factory)
				if err != nil {
					t.Errorf("createParsers: %v", err)
				} else {
					for _, p := range parsers {
						if closer, ok := p.(io.Closer); ok {
							_ = closer.Close()
						}
					}
				}
				_ = factory.Close() // best-effort cleanup
			}
		}()
	}

	wg.Wait()
}

// TestSecureValue_ConcurrentMemoryLockToggle flips the process-global memory
// lock switch while values are created, queried, and released. The switch is
// an atomic, so each NewSecureValue either attempts the lock or skips it; the
// value's own state must stay consistent either way (full-length data, clean
// release). The lock-state queries (IsMemoryLocked, MemoryLockError, String)
// were folded in from the former ConcurrentWithMemoryLock test. Strict mode
// stays off to keep the default failure handler from writing to stderr on
// platforms where VirtualLock/mlock is denied.
func TestSecureValue_ConcurrentMemoryLockToggle(t *testing.T) {
	origEnabled := IsMemoryLockEnabled()
	origStrict := IsMemoryLockStrict()
	defer SetMemoryLockEnabled(origEnabled)
	defer SetMemoryLockStrict(origStrict)
	SetMemoryLockStrict(false)

	const value = "toggle_test_value"
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Toggler: flip the global enable while values are in flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		on := true
		for {
			select {
			case <-stop:
				return
			default:
			}
			SetMemoryLockEnabled(on)
			on = !on
		}
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				sv := NewSecureValue(value)
				if got := sv.Length(); got != len(value) {
					t.Errorf("Length() = %d, want %d", got, len(value))
					return
				}
				// Lock-state queries under the racing switch: a value either
				// skipped the lock (created while disabled), holds it, or
				// carries the failure — never both locked and errored.
				locked, lockErr := sv.IsMemoryLocked(), sv.MemoryLockError()
				if locked && lockErr != nil {
					t.Errorf("value reports locked and a lock error: %v", lockErr)
					return
				}
				_ = sv.String() // masked output embeds the (racing) lock status
				sv.Release()
			}
		}()
	}

	close(stop)
	wg.Wait()
}
