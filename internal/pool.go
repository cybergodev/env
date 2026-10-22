// Provides shared resource pools for the env package.

package internal

import (
	"strings"
	"sync"
)

// ============================================================================
// Shared Strings.Builder Pool
// ============================================================================

// builderPool provides a pool of reusable strings.Builder instances.
// This is shared across multiple packages to reduce allocations for
// string building operations.
var builderPool = sync.Pool{
	New: func() any {
		return new(strings.Builder)
	},
}

// GetBuilder retrieves a strings.Builder from the shared pool.
// Returns a fallback builder if the pool returns an unexpected type.
func GetBuilder() *strings.Builder {
	sb, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		// Fallback: create new builder if pool returns unexpected type
		return new(strings.Builder)
	}
	sb.Reset()
	return sb
}

// PutBuilder returns a strings.Builder to the shared pool.
//
// SCOPE NOTE: strings.Builder.Reset() drops the internal buffer entirely
// (buf = nil), so this pool recycles only the small builder struct header —
// each GetBuilder/Write cycle still allocates a fresh byte buffer on first
// write. That is the minimum allocation profile anyway for callers that
// return builder.String() (zero-copy into a fresh buffer, no hazard when the
// builder is reused); pooling a reusable buffer instead (e.g. bytes.Buffer)
// would not reduce the total: String() must copy out, and retained pooled
// buffers would hold stale contents.
//
// SECURITY LIMITATION: strings.Builder does not expose its internal buffer,
// so we cannot directly clear the contents before pooling. However, Reset()
// releases the buffer (it is not retained, and any String() result already
// holds the only reference to its bytes). For environments requiring strict
// data clearing, consider not using the pool for sensitive operations.
func PutBuilder(sb *strings.Builder) {
	if sb == nil {
		return
	}
	// Reset drops the buffer reference (allowing GC of the previous string
	// data) and returns the builder struct to its zero state before pooling.
	// Cap() is always 0 after Reset (buf = nil), so no size gate is possible
	// or needed — only the struct header is ever pooled (see SCOPE NOTE).
	sb.Reset()
	builderPool.Put(sb)
}

// ============================================================================
// Byte Slice Pool for Value Parsing
// ============================================================================

// byteSlicePool provides a pool of reusable byte slices for value parsing.
// This reduces allocations when converting byte slices to strings.
var byteSlicePool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, 256)
		return &buf
	},
}

// GetByteSlice retrieves a byte slice from the pool.
// Returns a fallback slice if the pool returns an unexpected type.
func GetByteSlice() *[]byte {
	buf, ok := byteSlicePool.Get().(*[]byte)
	if !ok {
		b := make([]byte, 0, 256)
		return &b
	}
	*buf = (*buf)[:0]
	return buf
}

// PutByteSlice returns a byte slice to the pool.
// Slices with capacity exceeding MaxPooledByteSliceSize are discarded.
//
// SECURITY NOTE: This function clears the slice contents before pooling
// to prevent sensitive data from persisting in pooled buffers. This is
// important because byte slices may contain parsed environment variable values.
// The capacity check is performed after clearing to prevent memory bloat.
func PutByteSlice(buf *[]byte) {
	if buf == nil {
		return
	}
	// SECURITY: Clear the slice contents before pooling to prevent sensitive data leakage
	// This is important for environment variable values which may contain secrets
	clear(*buf)
	// Don't pool very large slices
	if cap(*buf) <= MaxPooledByteSliceSize {
		byteSlicePool.Put(buf)
	}
}

// ============================================================================
// KeysToUpper Map Pool
// ============================================================================

// keysToUpperPool provides a pool of reusable maps for KeysToUpper operations.
// This reduces allocations when converting map keys to uppercase for comparison.
var keysToUpperPool = sync.Pool{
	New: func() any {
		return make(map[string]bool, 64)
	},
}

// getKeysToUpperMap retrieves a map from the pool.
// Returns a fallback map if the pool returns an unexpected type.
func getKeysToUpperMap() map[string]bool {
	m, ok := keysToUpperPool.Get().(map[string]bool)
	if !ok {
		// Fallback: create new map if pool returns unexpected type
		return make(map[string]bool, 64)
	}
	return m
}

// putKeysToUpperMap returns a map to the pool after clearing it.
// Maps with more entries than MaxPooledMapSize are discarded to prevent memory bloat.
//
// SECURITY NOTE: Size is checked BEFORE clearing because clear() sets len to 0.
// We preserve the original size to make the pooling decision.
func putKeysToUpperMap(m map[string]bool) {
	if m == nil {
		return
	}
	// Check size before clearing - after clear() len(m) will be 0
	size := len(m)

	// SECURITY: Use clear() builtin (Go 1.21+) for guaranteed complete clearing.
	// This is O(1) and prevents partial clears that could leave stale entries.
	clear(m)

	// Don't pool very large maps (check original size before clearing)
	// Use <= to include maps at exactly MaxPooledMapSize
	if size <= MaxPooledMapSize {
		keysToUpperPool.Put(m)
	}
}
