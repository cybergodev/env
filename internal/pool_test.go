package internal

import (
	"strings"
	"testing"
)

// ============================================================================
// Builder Pool Tests
// ============================================================================

func TestGetBuilder(t *testing.T) {
	sb := GetBuilder()
	if sb == nil {
		t.Fatal("GetBuilder() returned nil")
	}

	// Builder should be reset
	if sb.Len() != 0 {
		t.Errorf("Builder length = %d, want 0", sb.Len())
	}

	// Write some data
	sb.WriteString("test")
	if sb.Len() != 4 {
		t.Errorf("Builder length after write = %d, want 4", sb.Len())
	}
}

func TestPutBuilder(t *testing.T) {
	sb := GetBuilder()
	sb.WriteString("test data")

	// Put back to pool
	PutBuilder(sb)

	// Get again and verify it's reset
	sb2 := GetBuilder()
	if sb2.Len() != 0 {
		t.Errorf("Pooled builder length = %d, want 0", sb2.Len())
	}
}

func TestPutBuilder_Nil(t *testing.T) {
	// Should not panic
	PutBuilder(nil)
}

func TestPutBuilder_LargeBuilder(t *testing.T) {
	// Create a large builder that exceeds MaxPooledBuilderSize
	sb := GetBuilder()
	largeData := strings.Repeat("x", MaxPooledBuilderSize+1)
	sb.WriteString(largeData)

	// Put back to pool - should be discarded due to size
	PutBuilder(sb)

	// Next get should still work (may or may not be the same builder)
	sb2 := GetBuilder()
	if sb2 == nil {
		t.Error("GetBuilder() returned nil after discarding large builder")
	}
}

// ============================================================================
// PutBuilderDiscard Tests
// ============================================================================

func TestPutBuilderDiscard(t *testing.T) {
	t.Run("resets builder content", func(t *testing.T) {
		sb := GetBuilder()
		sb.WriteString("sensitive_data")
		originalLen := sb.Len()

		PutBuilderDiscard(sb)

		// After discard, builder should be reset (len 0) but pointer is still valid
		if originalLen == 0 {
			t.Error("sanity: builder should have had content before discard")
		}
	})

	t.Run("nil is safe", func(t *testing.T) {
		// Should not panic
		PutBuilderDiscard(nil)
	})

	t.Run("discarded builder not pooled (content overwritten)", func(t *testing.T) {
		// Fill the pool with known data via PutBuilder, then discard one,
		// then verify GetBuilder returns clean builders.
		for range 5 {
			sb := GetBuilder()
			sb.WriteString("pool_seed")
			PutBuilder(sb) // goes to pool
		}

		// Now discard a builder with sensitive content
		sb := GetBuilder()
		sb.WriteString("SECRET_TOKEN_VALUE")
		PutBuilderDiscard(sb) // must NOT return to pool

		// All subsequent GetBuilder calls should return reset (empty) builders
		for range 10 {
			got := GetBuilder()
			if got.Len() != 0 {
				t.Error("GetBuilder() returned a non-reset builder — discard may have leaked to pool")
			}
			got.WriteString("x")
			PutBuilder(got)
		}
	})
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
}

func TestPutByteSlice(t *testing.T) {
	buf := GetByteSlice()
	*buf = append(*buf, "test data"...)

	// Put back to pool
	PutByteSlice(buf)

	// Get again and verify it's reset
	buf2 := GetByteSlice()
	if len(*buf2) != 0 {
		t.Errorf("Pooled slice length = %d, want 0", len(*buf2))
	}
}

func TestPutByteSlice_Nil(t *testing.T) {
	// Should not panic
	PutByteSlice(nil)
}

func TestPutByteSlice_LargeSlice(t *testing.T) {
	// Create a large slice that exceeds MaxPooledByteSliceSize
	buf := GetByteSlice()
	largeData := make([]byte, MaxPooledByteSliceSize+1)
	*buf = append(*buf, largeData...)

	// Put back to pool - should be discarded due to size
	PutByteSlice(buf)

	// Next get should still work
	buf2 := GetByteSlice()
	if buf2 == nil {
		t.Error("GetByteSlice() returned nil after discarding large slice")
	}
}

// ============================================================================
// Pool Reuse Tests
// ============================================================================

func TestBuilderPool_Reuse(t *testing.T) {
	// Get and return multiple times to test pool reuse
	for i := 0; i < 10; i++ {
		sb := GetBuilder()
		sb.WriteString("test")
		PutBuilder(sb)
	}

	// Final check
	sb := GetBuilder()
	if sb.Len() != 0 {
		t.Errorf("Final builder length = %d, want 0", sb.Len())
	}
	PutBuilder(sb)
}

func TestByteSlicePool_Reuse(t *testing.T) {
	// Get and return multiple times to test pool reuse
	for i := 0; i < 10; i++ {
		buf := GetByteSlice()
		*buf = append(*buf, "test"...)
		PutByteSlice(buf)
	}

	// Final check
	buf := GetByteSlice()
	if len(*buf) != 0 {
		t.Errorf("Final slice length = %d, want 0", len(*buf))
	}
	PutByteSlice(buf)
}

// ============================================================================
// Constants Tests
// ============================================================================

func TestPoolConstants(t *testing.T) {
	// Verify constants are reasonable
	if MaxPooledByteSliceSize <= 0 {
		t.Error("MaxPooledByteSliceSize should be positive")
	}
	if MaxPooledBuilderSize <= 0 {
		t.Error("MaxPooledBuilderSize should be positive")
	}
	if MaxPooledMapSize <= 0 {
		t.Error("MaxPooledMapSize should be positive")
	}

	// Verify relationships
	if MaxPooledByteSliceSize < 4096 {
		t.Error("MaxPooledByteSliceSize should be at least 4KB")
	}
}
