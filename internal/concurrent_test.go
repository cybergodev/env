package internal

import (
	"sync"
	"sync/atomic"
	"testing"
)

// ============================================================================
// Concurrent Access Tests for InternKeyBytes
// ============================================================================

func TestInternKeyBytes_ConcurrentAccess(t *testing.T) {
	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "TEST_KEY_" + string(rune('A'+j%26))
				result := InternKeyBytes([]byte(key))
				if result != key {
					t.Errorf("InternKeyBytes(%q) = %q, want %q", key, result, key)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestInternKeyBytes_ConcurrentWithClear(t *testing.T) {
	for run := 0; run < 10; run++ {
		ClearInternCache()

		var wg sync.WaitGroup
		iterations := 100
		cleared := int64(0)

		// Interners
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					if got := InternKeyBytes([]byte("TEST_KEY")); got != "TEST_KEY" {
						t.Errorf("InternKeyBytes concurrent with clear = %q, want %q", got, "TEST_KEY")
						return
					}
				}
			}()
		}

		// Clearers
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations/10; j++ {
					if atomic.CompareAndSwapInt64(&cleared, 0, 1) {
						ClearInternCache()
						atomic.StoreInt64(&cleared, 0)
					}
				}
			}()
		}

		wg.Wait()

		// After the intern/clear storm the cache must still intern correctly.
		if got := InternKeyBytes([]byte("TEST_KEY")); got != "TEST_KEY" {
			t.Errorf("InternKeyBytes after clear storm = %q, want %q", got, "TEST_KEY")
		}
	}
}

// ============================================================================
// Concurrent Access Tests for Expander
// ============================================================================

func TestExpander_ConcurrentAccess(t *testing.T) {
	lookup := func(key string) (string, bool) {
		switch key {
		case "VAR1":
			return "value1", true
		case "VAR2":
			return "value2", true
		default:
			return "", false
		}
	}

	expander := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Lookup:   lookup,
		Mode:     ModeAll,
	})

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, err := expander.Expand("$VAR1 and $VAR2")
				if err != nil {
					t.Errorf("Expand() error = %v", err)
				}
			}
		}()
	}

	wg.Wait()
}

func TestDetectCycle_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	iterations := 100
	concurrency := 5

	// Cyclic map (A→B→A): the concurrent loop keeps its race-detector value,
	// while the assertions exercise the true path — the previous acyclic map
	// never returned a cycle at all.
	vars := map[string]string{
		"A": "$B",
		"B": "$A",
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cycle, found := DetectCycle(vars, nil)
				if !found {
					t.Errorf("DetectCycle(cyclic map) found = false, want true")
					return
				}
				if cycle == "" {
					t.Errorf("DetectCycle(cyclic map) cycle = %q, want non-empty key", cycle)
					return
				}
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// Concurrent Access Tests for Auditor
// ============================================================================

func TestAuditor_ConcurrentAccess(t *testing.T) {
	handler := NewNopHandler()
	auditor := NewAuditor(handler,
		func(key string) bool { return false },
		func(key, value string) string { return value },
		true,
	)

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Nothing disables the auditor or fails the NopHandler in this
				// test, so all three results are deterministic.
				if err := auditor.Log(ActionSet, "KEY", "test", true); err != nil {
					t.Errorf("Log() error = %v, want nil", err)
					return
				}
				if err := auditor.LogError(ActionGet, "KEY", "error"); err != nil {
					t.Errorf("LogError() error = %v, want nil", err)
					return
				}
				if !auditor.IsEnabled() {
					t.Errorf("IsEnabled() = false, want true (never disabled in this test)")
					return
				}
			}
		}()
	}

	wg.Wait()
}

func TestAuditor_ConcurrentWithEnable(t *testing.T) {
	for run := 0; run < 10; run++ {
		handler := NewNopHandler()
		auditor := NewAuditor(handler,
			func(key string) bool { return false },
			func(key, value string) string { return value },
			true,
		)

		var wg sync.WaitGroup
		iterations := 100

		// Loggers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = auditor.Log(ActionSet, "KEY", "test", true) // error irrelevant under concurrent stress
					_ = auditor.IsEnabled()
				}
			}()
		}

		// Enablers/Disablers
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations/10; j++ {
					auditor.SetEnabled(j%2 == 0)
				}
			}()
		}

		wg.Wait()
	}
}

// ============================================================================
// Concurrent Access Tests for Validator
// ============================================================================

func TestValidator_ConcurrentAccess(t *testing.T) {
	validator := NewValidator(ValidatorConfig{
		MaxKeyLength:   256,
		MaxValueLength: 4096,
	})

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = validator.ValidateKey("VALID_KEY")
				_ = validator.ValidateValue("valid value")
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// Concurrent Access Tests for Pools
// ============================================================================

func TestPool_ConcurrentAccess(t *testing.T) {
	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Builder pool
				builder := GetBuilder()
				builder.WriteString("test")
				PutBuilder(builder)

				// Byte slice pool
				buf := GetByteSlice()
				*buf = append(*buf, "test"...)
				PutByteSlice(buf)
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// Concurrent Access Tests for LineParser
// ============================================================================

func TestLineParser_ConcurrentAccess(t *testing.T) {
	validator := NewValidator(ValidatorConfig{
		MaxKeyLength:   256,
		MaxValueLength: 4096,
	})
	auditor := NewAuditor(NewNopHandler(),
		func(key string) bool { return false },
		func(key, value string) string { return value },
		false,
	)
	expander := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Lookup:   func(key string) (string, bool) { return "", false },
		Mode:     ModeNone,
	})

	parser := NewLineParser(LineParserConfig{
		AllowExportPrefix: true,
		AllowYamlSyntax:   true,
	}, validator, auditor, expander)

	var wg sync.WaitGroup
	iterations := 1000
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _, _ = parser.ParseLineBytes([]byte("KEY=value"))
			}
		}()
	}

	wg.Wait()
}
