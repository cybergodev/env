package internal

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// ============================================================================
// Builder Pool Tests
// ============================================================================

func TestPutBuilder_LargeBuilder(t *testing.T) {
	// Reset() drops the buffer (buf = nil), so PutBuilder pools the struct
	// header unconditionally — Cap() is always 0 after Reset and the old
	// size gate could never trigger. This pins that a builder that once
	// held large data is still poolable and comes back empty.
	sb := GetBuilder()
	largeData := strings.Repeat("x", 16*1024+1) // larger than any builder gate ever was
	sb.WriteString(largeData)
	if sb.Len() == 0 {
		t.Fatal("sanity: builder should hold the large data before Put")
	}

	PutBuilder(sb)

	sb2 := GetBuilder()
	if sb2 == nil {
		t.Fatal("GetBuilder() returned nil after pooling large-data builder")
	}
	if sb2.Len() != 0 {
		t.Errorf("reused builder length = %d, want 0 (Reset drops the buffer)", sb2.Len())
	}
	if sb2.Cap() != 0 {
		t.Errorf("reused builder Cap() = %d, want 0 (Reset drops the buffer)", sb2.Cap())
	}
}

// ============================================================================
// Byte Slice Pool Tests
// ============================================================================

func TestGetByteSlice(t *testing.T) {
	buf := GetByteSlice()
	if buf == nil {
		t.Fatal("GetByteSlice() returned nil")
	}

	// Slice should be empty but have capacity
	if len(*buf) != 0 {
		t.Errorf("Slice length = %d, want 0", len(*buf))
	}
	if cap(*buf) < 256 {
		t.Errorf("Slice capacity = %d, want at least 256", cap(*buf))
	}

	// Write some data
	*buf = append(*buf, "test"...)
	if len(*buf) != 4 {
		t.Errorf("Slice length after append = %d, want 4", len(*buf))
	}

	t.Run("falls back on wrong-typed pool entry", func(t *testing.T) {
		// Poison the pool with an unexpected type; GetByteSlice must return
		// a freshly allocated, usable slice instead of panicking.
		// (A pointer value keeps the Put allocation-free per SA6002.)
		byteSlicePool.Put(new(int))
		fallback := GetByteSlice()
		if fallback == nil {
			t.Fatal("GetByteSlice() returned nil for poisoned pool")
		}
		if len(*fallback) != 0 {
			t.Errorf("fallback slice length = %d, want 0", len(*fallback))
		}
		if cap(*fallback) < 256 {
			t.Errorf("fallback slice capacity = %d, want at least 256", cap(*fallback))
		}
	})
}

// ============================================================================
// Pool Reuse Tests
// ============================================================================

func TestBuilderPool_Reuse(t *testing.T) {
	t.Run("reuse cycle keeps builders usable and reset", func(t *testing.T) {
		// Get and return multiple times to test pool reuse
		for i := 0; i < 10; i++ {
			sb := GetBuilder()
			sb.WriteString("test")
			if sb.Len() != 4 {
				t.Fatalf("builder Len() = %d after WriteString(\"test\"), want 4", sb.Len())
			}
			PutBuilder(sb)
		}

		// Final check
		sb := GetBuilder()
		if sb == nil {
			t.Fatal("GetBuilder() returned nil")
		}
		if sb.Len() != 0 {
			t.Errorf("Final builder length = %d, want 0", sb.Len())
		}
		PutBuilder(sb)
	})

	t.Run("put nil is a no-op and Get still returns a fresh builder", func(t *testing.T) {
		PutBuilder(nil)
		sb := GetBuilder()
		if sb == nil {
			t.Fatal("GetBuilder() returned nil after PutBuilder(nil)")
		}
		if sb.Len() != 0 {
			t.Errorf("builder length = %d, want 0", sb.Len())
		}
		PutBuilder(sb)
	})

	t.Run("double Put does not corrupt the pool", func(t *testing.T) {
		sb := GetBuilder()
		sb.WriteString("dup")
		PutBuilder(sb)
		PutBuilder(sb) // second consecutive return must not panic
		next := GetBuilder()
		if next == nil {
			t.Fatal("GetBuilder() returned nil after double Put")
		}
		if next.Len() != 0 {
			t.Errorf("builder length = %d after double Put, want 0", next.Len())
		}
		PutBuilder(next)
	})

	t.Run("poisoned pool entry falls back to a fresh builder", func(t *testing.T) {
		// The pool is any-typed; a wrong-typed entry must hit GetBuilder's
		// defensive type assertion instead of panicking. sync.Pool may serve
		// any previously pooled victim, so poison it with several foreign
		// objects to make the fallback observable regardless of which one
		// Get returns.
		for i := 0; i < 4; i++ {
			builderPool.Put(new(int))
		}
		sb := GetBuilder()
		if sb == nil {
			t.Fatal("GetBuilder() returned nil for poisoned pool")
		}
		if sb.Len() != 0 {
			t.Errorf("fallback builder length = %d, want 0", sb.Len())
		}
		sb.WriteString("usable")
		if sb.Len() != 6 {
			t.Errorf("fallback builder Len() = %d after WriteString, want 6", sb.Len())
		}
		PutBuilder(sb)
	})
}

func TestByteSlicePool_Reuse(t *testing.T) {
	t.Run("reuse cycle keeps slices usable and reset", func(t *testing.T) {
		// Get and return multiple times to test pool reuse
		for i := 0; i < 10; i++ {
			buf := GetByteSlice()
			*buf = append(*buf, "test"...)
			if len(*buf) != 4 {
				t.Fatalf("slice length = %d after appending \"test\", want 4", len(*buf))
			}
			PutByteSlice(buf)
		}

		// Final check
		buf := GetByteSlice()
		if len(*buf) != 0 {
			t.Errorf("Final slice length = %d, want 0", len(*buf))
		}
		PutByteSlice(buf)
	})

	t.Run("put nil is a no-op and Get still returns a fresh slice", func(t *testing.T) {
		PutByteSlice(nil)
		buf := GetByteSlice()
		if buf == nil {
			t.Fatal("GetByteSlice() returned nil after PutByteSlice(nil)")
		}
		if len(*buf) != 0 {
			t.Errorf("slice length = %d, want 0", len(*buf))
		}
		PutByteSlice(buf)
	})

	t.Run("put clears contents before pooling", func(t *testing.T) {
		// SECURITY: PutByteSlice zeroes the caller's buffer (pool.go), so
		// secrets cannot persist in pooled storage.
		b := []byte("secret")
		buf := &b
		PutByteSlice(buf)
		for i, c := range *buf {
			if c != 0 {
				t.Errorf("byte %d = %#x after PutByteSlice, want 0 (contents cleared)", i, c)
			}
		}
	})

	t.Run("double Put does not corrupt the pool", func(t *testing.T) {
		buf := GetByteSlice()
		*buf = append(*buf, "dup"...)
		PutByteSlice(buf)
		PutByteSlice(buf) // second consecutive return must not panic
		next := GetByteSlice()
		if next == nil {
			t.Fatal("GetByteSlice() returned nil after double Put")
		}
		if len(*next) != 0 {
			t.Errorf("slice length = %d after double Put, want 0", len(*next))
		}
		if cap(*next) > MaxPooledByteSliceSize {
			t.Errorf("slice capacity = %d after double Put, want <= %d", cap(*next), MaxPooledByteSliceSize)
		}
		PutByteSlice(next)
	})
}

// ============================================================================
// Byte Slice Size Gate Tests
// ============================================================================

// TestPutByteSlice_LargeSlice pins the capacity gate in PutByteSlice
// (pool.go: `cap(*buf) <= MaxPooledByteSliceSize`). The gate is inclusive:
// a buffer at exactly MaxPooledByteSliceSize stays pool-eligible, while one
// above the gate is provably discarded — its pointer can never be handed
// back by a later Get.
func TestPutByteSlice_LargeSlice(t *testing.T) {
	t.Run("capacity above the gate is discarded", func(t *testing.T) {
		oversized := make([]byte, 0, MaxPooledByteSliceSize+1)
		buf := &oversized
		PutByteSlice(buf)

		got := GetByteSlice()
		if got == nil {
			t.Fatal("GetByteSlice() returned nil after discarding large slice")
		}
		if got == buf {
			t.Fatalf("PutByteSlice pooled a %d-cap buffer; the gate must discard anything above %d",
				cap(*buf), MaxPooledByteSliceSize)
		}
		if cap(*got) > MaxPooledByteSliceSize {
			t.Errorf("GetByteSlice() capacity = %d, want <= %d", cap(*got), MaxPooledByteSliceSize)
		}
		PutByteSlice(got)
	})

	t.Run("capacity exactly at the gate stays pool-eligible", func(t *testing.T) {
		atGate := make([]byte, 0, MaxPooledByteSliceSize)
		buf := &atGate
		PutByteSlice(buf)

		got := GetByteSlice()
		if got == nil {
			t.Fatal("GetByteSlice() returned nil after pooling an at-gate slice")
		}
		if len(*got) != 0 {
			t.Errorf("pooled slice length = %d, want 0", len(*got))
		}
		// sync.Pool does not guarantee handing back the most recent Put, so
		// identity is asserted opportunistically: when this exact buffer does
		// come back, it proves the inclusive gate let it through intact.
		if got == buf && cap(*got) != MaxPooledByteSliceSize {
			t.Errorf("at-gate buffer came back with capacity = %d, want %d", cap(*got), MaxPooledByteSliceSize)
		}
		PutByteSlice(got)
	})
}

// ============================================================================
// KeysToUpper Map Pool Tests
// ============================================================================

// TestKeysToUpperMapPool covers getKeysToUpperMap/putKeysToUpperMap (pool.go),
// the one pool pair that had no direct tests. The put gate is
// `size <= MaxPooledMapSize`, checked BEFORE clear() empties the map.
func TestKeysToUpperMapPool(t *testing.T) {
	t.Run("get returns a fresh usable map", func(t *testing.T) {
		m := getKeysToUpperMap()
		if m == nil {
			t.Fatal("getKeysToUpperMap() returned nil")
		}
		if len(m) != 0 {
			t.Errorf("map length = %d, want 0", len(m))
		}
		m["IE_USABLE"] = true
		if !m["IE_USABLE"] {
			t.Error("map returned by getKeysToUpperMap is not usable")
		}
		putKeysToUpperMap(m)
	})

	t.Run("put nil is a no-op", func(t *testing.T) {
		putKeysToUpperMap(nil)
	})

	t.Run("round trip clears the map for the next user", func(t *testing.T) {
		m := getKeysToUpperMap()
		m["IE_A"] = true
		m["IE_B"] = true
		putKeysToUpperMap(m)
		if len(m) != 0 {
			t.Errorf("len(m) = %d after putKeysToUpperMap, want 0 (cleared before pooling)", len(m))
		}
		next := getKeysToUpperMap()
		if len(next) != 0 {
			t.Errorf("len(next) = %d after Get, want 0", len(next))
		}
		if _, leaked := next["IE_A"]; leaked {
			t.Error("stale entry IE_A leaked into the next map from the pool")
		}
		putKeysToUpperMap(next)
	})

	t.Run("map above MaxPooledMapSize is discarded but still cleared", func(t *testing.T) {
		m := make(map[string]bool)
		for i := 0; i < MaxPooledMapSize+1; i++ {
			m[fmt.Sprintf("IE_BIG_%d", i)] = true
		}
		putKeysToUpperMap(m)
		if len(m) != 0 {
			t.Errorf("len(m) = %d after put, want 0 (cleared even when discarded)", len(m))
		}
		// Discard is observable via aliasing: had the oversized map been
		// pooled, a subsequent Get could hand back that very map, making
		// writes through it visible in m.
		next := getKeysToUpperMap()
		next["IE_ALIAS_PROBE"] = true
		if _, aliased := m["IE_ALIAS_PROBE"]; aliased {
			t.Errorf("map with %d entries (limit %d) was pooled; the gate must discard it",
				MaxPooledMapSize+1, MaxPooledMapSize)
		}
		putKeysToUpperMap(next)
	})

	t.Run("map at exactly MaxPooledMapSize stays pool-eligible", func(t *testing.T) {
		m := make(map[string]bool)
		for i := 0; i < MaxPooledMapSize; i++ {
			m[fmt.Sprintf("IE_AT_%d", i)] = true
		}
		putKeysToUpperMap(m) // gate is size <= MaxPooledMapSize: no panic, no discard
		if len(m) != 0 {
			t.Errorf("len(m) = %d after put, want 0 (cleared)", len(m))
		}
		next := getKeysToUpperMap()
		if len(next) != 0 {
			t.Errorf("len(next) = %d after Get, want 0", len(next))
		}
		putKeysToUpperMap(next)
	})

	t.Run("poisoned pool entry falls back to a fresh map", func(t *testing.T) {
		// The pool is any-typed; a wrong-typed entry must hit
		// getKeysToUpperMap's defensive type assertion instead of panicking.
		// Several foreign objects make the fallback observable regardless of
		// which previously pooled victim Get serves.
		for i := 0; i < 4; i++ {
			keysToUpperPool.Put(new(int))
		}
		m := getKeysToUpperMap()
		if m == nil {
			t.Fatal("getKeysToUpperMap() returned nil for a poisoned pool")
		}
		if len(m) != 0 {
			t.Errorf("fallback map length = %d, want 0", len(m))
		}
		m["IE_FALLBACK"] = true
		if !m["IE_FALLBACK"] {
			t.Error("fallback map is not usable")
		}
		putKeysToUpperMap(m)
	})
}

// ============================================================================
// Constants Tests
// ============================================================================

func TestPoolConstants(t *testing.T) {
	// Verify the constants production code actually gates on:
	// MaxPooledByteSliceSize (PutByteSlice, pool.go) and MaxPooledMapSize
	// (putKeysToUpperMap, pool.go / expander.go). PutBuilder pools
	// unconditionally (Reset() drops the buffer, so there is no capacity
	// left to gate on — the former MaxPooledBuilderSize constant was
	// removed as dead).
	if MaxPooledByteSliceSize <= 0 {
		t.Error("MaxPooledByteSliceSize should be positive")
	}
	if MaxPooledMapSize <= 0 {
		t.Error("MaxPooledMapSize should be positive")
	}

	// Verify relationships
	if MaxPooledByteSliceSize < 4096 {
		t.Error("MaxPooledByteSliceSize should be at least 4KB")
	}
}

// ============================================================================
// Lexer Buffer Pool Tests
// ============================================================================

// TestLexerBufferPool_Lifecycle covers the lexer buffer pool's basic
// contract in one place: get returns a usable buffer that comes back reset,
// and put(nil) is a no-op. (The multiple-returns case deliberately lives in
// its own test BELOW TestLexerBufferPool_ConcurrentAccess: its duplicate
// pool entries must never be observable to a concurrent Get.)
func TestLexerBufferPool_Lifecycle(t *testing.T) {
	t.Run("get returns a usable buffer that resets across the pool", func(t *testing.T) {
		buf := getLexerBuffer()
		if buf == nil {
			t.Fatal("getLexerBuffer() returned nil")
		}

		buf.WriteString("test data")
		if buf.Len() != 9 {
			t.Errorf("Buffer length = %d, want 9", buf.Len())
		}

		putLexerBuffer(buf)

		buf2 := getLexerBuffer()
		if buf2.Len() != 0 {
			t.Errorf("Pooled buffer length = %d, want 0", buf2.Len())
		}
		putLexerBuffer(buf2)
	})

	t.Run("put nil is a no-op", func(t *testing.T) {
		putLexerBuffer(nil) // must not panic
	})
}

// TestLexerBufferPool_LargeBuffer pins the capacity gate in putLexerBuffer
// (yaml_lexer.go: `buf.Cap() <= 4096`). Buffers are built with an exact
// capacity via bytes.NewBuffer because growing through Write does not land
// on the gate value exactly. The gate is inclusive: Cap 4096 stays
// pool-eligible, 4097 is provably discarded (its pointer can never come
// back from a later Get).
func TestLexerBufferPool_LargeBuffer(t *testing.T) {
	t.Run("capacity above the gate is discarded", func(t *testing.T) {
		buf := bytes.NewBuffer(make([]byte, 4097)) // len = cap = 4097
		if buf.Len() != 4097 {
			t.Fatalf("sanity: buffer length = %d, want 4097", buf.Len())
		}
		putLexerBuffer(buf)

		got := getLexerBuffer()
		if got == nil {
			t.Fatal("getLexerBuffer() returned nil after discarding large buffer")
		}
		if got == buf {
			t.Fatal("putLexerBuffer pooled a 4097-cap buffer; the gate must discard anything above 4096")
		}
		putLexerBuffer(got)
	})

	t.Run("capacity exactly at the gate stays pool-eligible", func(t *testing.T) {
		buf := bytes.NewBuffer(make([]byte, 0, 4096)) // cap = 4096, len = 0
		if buf.Cap() != 4096 {
			t.Fatalf("sanity: buffer Cap = %d, want 4096", buf.Cap())
		}
		putLexerBuffer(buf)

		got := getLexerBuffer()
		if got == nil {
			t.Fatal("getLexerBuffer() returned nil after pooling an at-gate buffer")
		}
		if got.Len() != 0 {
			t.Errorf("pooled buffer length = %d, want 0", got.Len())
		}
		// Opportunistic identity check (sync.Pool does not guarantee the most
		// recent Put comes back): if the exact buffer is reused, its capacity
		// must have survived the round trip, proving the inclusive gate.
		if got == buf && got.Cap() != 4096 {
			t.Errorf("at-gate buffer came back with Cap = %d, want 4096", got.Cap())
		}
		putLexerBuffer(got)
	})
}

func TestLexerBufferPool_ConcurrentAccess(t *testing.T) {
	// Test concurrent access to the lexer buffer pool
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				buf := getLexerBuffer()
				buf.WriteString("concurrent test")
				putLexerBuffer(buf)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
}

// TestLexerBufferPool_NoLeakOnMultipleReturns pins that returning the same
// buffer several times does not corrupt the pool. It MUST stay ordered after
// TestLexerBufferPool_ConcurrentAccess (and all other pool consumers): a
// double Put leaves duplicate pointers in the pool, and a later concurrent
// Get could hand the same buffer to two goroutines — a data race by test
// construction, not a library defect. No concurrent lexer-pool test may be
// added after this one.
func TestLexerBufferPool_NoLeakOnMultipleReturns(t *testing.T) {
	buf := getLexerBuffer()
	buf.WriteString("test1")

	// Returning the same buffer multiple times (should be safe).
	putLexerBuffer(buf)
	putLexerBuffer(buf) // Second return should be safe
	putLexerBuffer(buf)

	// Drain the duplicated entries WITHOUT returning them, so later
	// (serial) pool consumers can never observe a duplicate pointer either.
	for i := 0; i < 3; i++ {
		_ = getLexerBuffer()
	}

	// Get a new buffer - should work fine
	buf2 := getLexerBuffer()
	if buf2 == nil {
		t.Error("getLexerBuffer() returned nil after multiple returns")
	}
	buf2.WriteString("test2")
	putLexerBuffer(buf2)
}
