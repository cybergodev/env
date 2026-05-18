package env

import (
	"fmt"
	"io"
	"maps"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// SecureValue
// ============================================================================

// secureValuePool provides a pool of reusable SecureValue objects.
// This significantly reduces allocations for high-frequency Set operations.
// Note: We do NOT set the finalizer in pool.New because:
// 1. Release() clears the finalizer before returning to pool
// 2. NewSecureValue() sets the finalizer when taking from pool
// This avoids "finalizer already set" panics.
var secureValuePool = sync.Pool{
	New: func() any {
		return &SecureValue{}
	},
}

// SecureValue wraps a sensitive value with automatic memory zeroing.
// When the value is garbage collected, its memory is securely cleared.
// If memory locking is enabled, the data is also protected from being
// swapped to disk.
type SecureValue struct {
	mu      sync.RWMutex
	data    []byte
	closed  atomic.Bool
	locked  bool  // tracks if memory is currently locked
	lockErr error // stores any locking error for strict mode
}

// NewSecureValue creates a new SecureValue from a string.
// The value is stored in a separate memory allocation that will be
// zeroed when the SecureValue is garbage collected or explicitly closed.
// This function uses a pool to reduce allocations.
//
// Memory Locking:
// If memory locking is enabled globally (via SetMemoryLockEnabled(true)),
// this function will attempt to lock the memory to prevent swapping.
// Locking failures are silently ignored unless strict mode is enabled.
func NewSecureValue(value string) *SecureValue {
	sv, ok := secureValuePool.Get().(*SecureValue)
	if !ok {
		// Fallback: create new SecureValue if pool returns unexpected type
		sv = &SecureValue{}
	}
	// Set finalizer for secure cleanup on GC.
	// This is safe because Release() clears the finalizer before
	// returning to pool, so we always need to set it here.
	runtime.SetFinalizer(sv, (*SecureValue).finalize)
	sv.reset(value)
	return sv
}

// NewSecureValueStrict creates a new SecureValue and returns an error
// if memory locking is enabled but fails.
// Use this function when you need to ensure that the memory is actually
// protected from being swapped to disk.
//
// Example:
//
//	env.SetMemoryLockEnabled(true)
//	sv, err := env.NewSecureValueStrict("sensitive-data")
//	if err != nil {
//	    // Memory locking failed - handle appropriately
//	    log.Printf("Warning: memory not locked: %v", err)
//	}
//	defer sv.Release()
func NewSecureValueStrict(value string) (*SecureValue, error) {
	sv := NewSecureValue(value)

	// Check if there was a locking error
	if err := sv.MemoryLockError(); err != nil {
		// In strict mode, return the error
		// The SecureValue is still valid and usable
		return sv, fmt.Errorf("memory lock failed: %w", err)
	}

	return sv, nil
}

// reset initializes or reinitializes the SecureValue with a new value.
// This is used when reusing pooled SecureValue objects.
// Note: The finalizer is set in NewSecureValue() for each use.
// We do NOT reset the finalizer here - Release() clears it before
// returning to the pool, and NewSecureValue() sets a fresh one when reused.
//
// State consistency: The entire operation is protected by mutex lock,
// ensuring no concurrent reads can observe partial state. We mark the
// SecureValue as open (closed=false) only after data is fully prepared.
//
// Memory Locking: If enabled globally, attempts to lock the memory
// to prevent swapping to disk. Locking failures are handled according
// to strict mode configuration.
func (sv *SecureValue) reset(value string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	valueLen := len(value)

	// Clear existing data for security
	// This must happen before any early returns to ensure old sensitive data is wiped
	if sv.data != nil {
		// Unlock memory before clearing (if it was locked)
		if sv.locked {
			internal.UnlockMemory(sv.data)
			sv.locked = false
		}
		clear(sv.data) // Use builtin for efficient zeroing (Go 1.21+)
	}

	// Reset lock error state
	sv.lockErr = nil

	// Fast path for empty string: set nil data and mark as open
	// Note: Empty string is a valid value, not "closed"
	if valueLen == 0 {
		sv.data = nil
		sv.closed.Store(false)
		return
	}

	// Try to reuse existing buffer if capacity is sufficient
	// This reduces allocations for frequently reused SecureValue objects
	// The 2x limit prevents unbounded memory growth while allowing efficient reuse
	if sv.data != nil && cap(sv.data) >= valueLen && cap(sv.data) <= valueLen*2 {
		// SECURITY: Clear the entire capacity before reusing to prevent
		// residual data from remaining in unused capacity.
		// This is important because sv.data may have been cleared above
		// with only the current length, not the full capacity.
		oldCap := cap(sv.data)
		fullSlice := sv.data[:oldCap]
		clear(fullSlice)
		sv.data = sv.data[:valueLen]
		copy(sv.data, value)
		// Attempt to lock memory if enabled
		sv.tryLockMemory()
		sv.closed.Store(false)
		return
	}

	// Allocate new buffer if reuse is not possible
	sv.data = []byte(value)
	// Attempt to lock memory if enabled
	sv.tryLockMemory()
	sv.closed.Store(false)
}

// tryLockMemory attempts to lock the memory if memory locking is enabled.
// Must be called with sv.mu held.
// Stores any error for strict mode handling.
func (sv *SecureValue) tryLockMemory() {
	if !IsMemoryLockEnabled() || len(sv.data) == 0 {
		return
	}

	err := internal.LockMemory(sv.data)
	if err != nil {
		sv.lockErr = err
		// In non-strict mode, we continue despite the error
		// The data is still usable, just not protected from swapping
	} else {
		sv.locked = true
	}
}

// finalize is called by the garbage collector to securely clear the value.
//
// Thread Safety:
// - This method is called by GC when the SecureValue becomes unreachable
// - At that point, no goroutine should have access to the object
// - We use atomic.Bool for the closed flag to ensure safe reads
// - SECURITY: We acquire the mutex to prevent race with concurrent Release()
//
// Race Prevention:
// - Release() clears the finalizer BEFORE putting the object back in the pool
// - NewSecureValue() sets a fresh finalizer when taking from the pool
// - This ensures no finalizer runs on pooled objects being reused
// - Additional mutex protection provides defense-in-depth
//
// SECURITY REQUIREMENT:
// Callers that return SecureValue to the pool MUST follow this sequence:
//  1. Call runtime.SetFinalizer(sv, nil) to clear the finalizer
//  2. Then call secureValuePool.Put(sv)
//
// Failure to clear the finalizer before Put() may cause the GC to run
// finalize() while the object is being reused, leading to data corruption.
func (sv *SecureValue) finalize() {
	// Fast path: if already closed, nothing to do
	if sv.closed.Load() {
		return
	}

	// SECURITY: Acquire mutex to prevent race with Release()
	// This is defense-in-depth - the finalizer should already be cleared
	// before the object is returned to the pool, but we protect against
	// any edge cases where this might not happen.
	sv.mu.Lock()
	defer sv.mu.Unlock()

	// Double-check after acquiring lock (standard pattern)
	if sv.closed.Load() {
		return
	}

	sv.clearDataLocked()
	sv.closed.Store(true)
}

// clearDataLocked securely zeros the data slice.
// Uses volatile-style writes through unsafe.Pointer to prevent compiler optimization.
// Must be called with sv.mu held.
func (sv *SecureValue) clearDataLocked() {
	// SECURITY: len(slice) == 0 handles both nil and empty slices.
	if len(sv.data) == 0 {
		return
	}

	// Unlock memory before clearing (if it was locked)
	if sv.locked {
		internal.UnlockMemory(sv.data)
		sv.locked = false
	}

	// Use volatile-style clearing to prevent compiler optimization
	dataPtr := unsafe.Pointer(&sv.data[0])
	for i := range sv.data {
		*(*byte)(unsafe.Pointer(uintptr(dataPtr) + uintptr(i))) = 0
	}
	runtime.KeepAlive(sv.data)
	sv.data = nil
	sv.lockErr = nil
}

// String returns a masked representation safe for logging and formatting.
// This implements fmt.Stringer to prevent accidental secret leakage through
// fmt.Printf, log.Println, or error wrapping. For the actual plaintext value,
// use Reveal() explicitly.
func (sv *SecureValue) String() string {
	if sv == nil {
		return "[NIL]"
	}
	return sv.Masked()
}

// Reveal returns the plaintext value as a string.
// The caller is responsible for handling the returned string securely —
// avoid logging, serializing, or storing it in persistently accessible locations.
// Use this only when the actual value is needed for cryptographic operations,
// API calls, or similar secure processing.
func (sv *SecureValue) Reveal() string {
	if sv == nil {
		return ""
	}
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	if sv.closed.Load() {
		return ""
	}
	return string(sv.data)
}

// Bytes returns a copy of the value as a byte slice.
// The caller is responsible for securely clearing the returned slice using ClearBytes.
func (sv *SecureValue) Bytes() []byte {
	if sv == nil {
		return nil
	}
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	if sv.closed.Load() {
		return nil
	}
	result := make([]byte, len(sv.data))
	copy(result, sv.data)
	return result
}

// Length returns the length of the value without exposing it.
func (sv *SecureValue) Length() int {
	if sv == nil {
		return 0
	}
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	if sv.closed.Load() {
		return 0
	}
	return len(sv.data)
}

// Close securely clears the value and marks it as closed.
// After calling Close, all access methods return zero values.
// Note: This method does NOT return the SecureValue to the pool.
// For explicit pool return, use Release() instead.
func (sv *SecureValue) Close() error {
	if sv == nil {
		return nil
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.closed.Load() {
		return nil
	}
	sv.clearDataLocked()
	sv.closed.Store(true)
	return nil
}

// Release securely clears the value and returns it to the pool.
// This is more efficient than Close() for high-frequency operations
// as it allows the SecureValue to be reused.
//
// The finalizer is cleared before returning to the pool to ensure:
// 1. The object can be safely reused without finalizer interference
// 2. NewSecureValue() will set a fresh finalizer when the object is reused
func (sv *SecureValue) Release() {
	if sv == nil {
		return
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.closed.Load() {
		return
	}
	sv.clearDataLocked()
	sv.closed.Store(true)
	// Clear finalizer before returning to pool. This prevents a race condition
	// where GC might trigger the finalizer while the object is being reused.
	// NewSecureValue() will set a new finalizer when this object is taken from the pool.
	runtime.SetFinalizer(sv, nil)
	secureValuePool.Put(sv)
}

// IsClosed returns true if the value has been closed.
func (sv *SecureValue) IsClosed() bool {
	if sv == nil {
		return true
	}
	return sv.closed.Load()
}

// Compile-time check that SecureValue implements io.Closer.
var _ io.Closer = (*SecureValue)(nil)

// Masked returns a masked representation for logging.
func (sv *SecureValue) Masked() string {
	if sv == nil {
		return "[NIL]"
	}
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	if sv.closed.Load() {
		return "[CLOSED]"
	}
	if sv.data == nil {
		return "[SECURE:0 bytes]"
	}

	// Include lock status in the masked representation
	lockStatus := ""
	if IsMemoryLockEnabled() {
		if sv.locked {
			lockStatus = " locked"
		} else if sv.lockErr != nil {
			lockStatus = " lock-failed"
		} else {
			lockStatus = " unlocked"
		}
	}

	return fmt.Sprintf("[SECURE:%d bytes%s]", len(sv.data), lockStatus)
}

// IsMemoryLocked returns true if the value's memory is currently locked
// (protected from being swapped to disk).
// Returns false if memory locking is not enabled or if locking failed.
func (sv *SecureValue) IsMemoryLocked() bool {
	if sv == nil {
		return false
	}
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return sv.locked && !sv.closed.Load()
}

// MemoryLockError returns any error that occurred during memory locking.
// Returns nil if locking was successful or not attempted.
// This is useful in strict mode to detect if memory locking failed.
func (sv *SecureValue) MemoryLockError() error {
	if sv == nil {
		return nil
	}
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return sv.lockErr
}

// ============================================================================
// secureMap (Sharded for better concurrency)
// ============================================================================

// numSecureMapShards defines the number of shards in secureMap.
// Value 8 provides good balance between concurrency and memory overhead:
//   - Reduces lock contention by distributing keys across multiple shards
//   - Each shard has its own mutex, allowing parallel reads/writes
//   - Memory overhead is minimal compared to single-lock design
const numSecureMapShards = 8

// secureMap capacity constants for efficient map growth.
const (
	// minShardCapacity is the minimum initial capacity for a shard's map.
	minShardCapacity = 8

	// loadFactorGrowthThreshold triggers map growth when len(values)*3 < newSize*2.
	// This corresponds to a load factor of approximately 66%.
	loadFactorGrowthThreshold = 3

	// capacityGrowthFactor determines new capacity as newSize * 4 / 3.
	// This provides 33% extra capacity to reduce future reallocations.
	capacityGrowthNumerator   = 4
	capacityGrowthDenominator = 3
)

// secureMapShard represents a single shard of the secure map.
type secureMapShard struct {
	mu     sync.RWMutex
	values map[string]*SecureValue
}

// secureMap provides a thread-safe map for storing sensitive values.
// Uses sharding to reduce lock contention in concurrent scenarios.
type secureMap struct {
	shards [numSecureMapShards]secureMapShard
	count  atomic.Int64 // Total count across all shards for fast Keys() allocation
}

// hashKey returns the shard index for a given key using FNV-1a hash.
// Uses the shared HashKey function from internal package.
func hashKey(key string) uint32 {
	return internal.HashKey(key, numSecureMapShards)
}

// newSecureMap creates a new secureMap with sharded storage.
func newSecureMap() *secureMap {
	sm := &secureMap{}
	for i := range numSecureMapShards {
		sm.shards[i].values = make(map[string]*SecureValue)
	}
	return sm
}

// getShard returns the shard for a given key.
func (sm *secureMap) getShard(key string) *secureMapShard {
	return &sm.shards[hashKey(key)]
}

// exists checks whether a key exists and is not closed, without allocating a SecureValue.
func (sm *secureMap) exists(key string) bool {
	shard := sm.getShard(key)
	shard.mu.RLock()
	sv, ok := shard.values[key]
	if !ok {
		shard.mu.RUnlock()
		return false
	}
	closed := sv.closed.Load()
	shard.mu.RUnlock()
	return !closed
}

// Set stores a value securely.
// When overwriting an existing key, updates the SecureValue in-place when possible,
// avoiding pool Get/Put overhead and reducing allocations.
func (sm *secureMap) Set(key string, value string) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	if existing, ok := shard.values[key]; ok {
		// In-place update: reuse existing SecureValue to avoid pool cycle + allocation.
		// Lock ordering is safe: shard.mu → sv.mu (reset acquires sv.mu.Lock).
		// No reverse ordering exists anywhere in the codebase.
		existing.reset(value)
		shard.mu.Unlock()
	} else {
		shard.values[key] = NewSecureValue(value)
		sm.count.Add(1)
		shard.mu.Unlock()
	}
}

// SetAll stores multiple values securely in a batch operation.
// This is more efficient than calling Set multiple times as it
// groups operations by shard to minimize lock acquisitions.
func (sm *secureMap) SetAll(values map[string]string) {
	if len(values) == 0 {
		return
	}

	// Distribute values to shard maps
	shardValues, shardCounts := sm.distributeToShards(values)

	// Process each shard
	for i := range numSecureMapShards {
		if shardCounts[i] == 0 {
			continue
		}
		sm.setShardValues(i, shardValues[i], shardCounts[i])
	}
}

// distributeToShards distributes values to per-shard maps for batch processing.
// Returns the distributed values and count per shard.
// Optimized to use pre-allocated stack arrays for small batches.
func (sm *secureMap) distributeToShards(values map[string]string) ([numSecureMapShards]map[string]string, [numSecureMapShards]int) {
	// First pass: count items per shard for accurate pre-allocation
	var shardCounts [numSecureMapShards]int
	for key := range values {
		shardCounts[hashKey(key)]++
	}

	// Pre-allocate shard maps with exact sizes
	var shardValues [numSecureMapShards]map[string]string
	for i := range numSecureMapShards {
		if shardCounts[i] > 0 {
			// Pre-allocate with exact size to avoid map growth
			shardValues[i] = make(map[string]string, shardCounts[i])
		}
	}

	// Second pass: distribute values to shards
	for key, value := range values {
		shardIdx := hashKey(key)
		shardValues[shardIdx][key] = value
	}

	return shardValues, shardCounts
}

// ensureShardCapacity ensures the shard map has enough capacity for new entries.
// Must be called with shard.mu held.
// Optimized to reduce allocations by using more aggressive growth for empty maps.
func (sm *secureMap) ensureShardCapacity(shard *secureMapShard, additionalCount int) {
	currentLen := len(shard.values)
	newSize := currentLen + additionalCount

	// Handle empty map initialization
	if currentLen == 0 {
		// Use the larger of: additionalCount with growth factor, or minimum capacity
		newCap := max(newSize, minShardCapacity)
		// Add growth buffer to reduce future reallocations
		newCap = newCap * capacityGrowthNumerator / capacityGrowthDenominator
		shard.values = make(map[string]*SecureValue, newCap)
		return
	}

	// Handle map growth if load factor would be too high
	// Grow when current size * loadFactorGrowthThreshold < new size * 2
	if currentLen*loadFactorGrowthThreshold < newSize*2 {
		// Calculate new capacity with growth factor
		newCap := newSize * capacityGrowthNumerator / capacityGrowthDenominator
		// Ensure minimum growth to avoid frequent reallocations
		if newCap < currentLen*2 {
			newCap = currentLen * 2
		}
		newMap := make(map[string]*SecureValue, newCap)
		maps.Copy(newMap, shard.values)
		shard.values = newMap
	}
}

// setShardValues sets multiple values in a single shard.
// Handles capacity management and secure value lifecycle.
// Uses in-place updates for existing keys to reduce allocations.
func (sm *secureMap) setShardValues(shardIdx int, values map[string]string, count int) {
	shard := &sm.shards[shardIdx]
	shard.mu.Lock()

	// Ensure capacity for new entries
	sm.ensureShardCapacity(shard, count)

	// Track new keys for count update
	newKeys := 0
	for key, value := range values {
		if existing, ok := shard.values[key]; ok {
			// In-place update: reuse existing SecureValue
			existing.reset(value)
		} else {
			shard.values[key] = NewSecureValue(value)
			newKeys++
		}
	}
	shard.mu.Unlock()

	// Update count after releasing lock
	if newKeys > 0 {
		sm.count.Add(int64(newKeys))
	}
}

// Get retrieves a value. Returns the value and whether it exists.
func (sm *secureMap) Get(key string) (string, bool) {
	shard := sm.getShard(key)
	shard.mu.RLock()
	sv, ok := shard.values[key]
	if !ok {
		shard.mu.RUnlock()
		return "", false
	}
	// Acquire SecureValue's read lock for consistency with GetSecure/ToMap
	sv.mu.RLock()
	if sv.closed.Load() {
		sv.mu.RUnlock()
		shard.mu.RUnlock()
		return "", false
	}
	// data may be nil for empty string values — string(nil) == "" is correct
	result := string(sv.data)
	sv.mu.RUnlock()
	shard.mu.RUnlock()
	return result, true
}

// GetSecure retrieves a copy of the SecureValue for the given key.
// Returns nil if the key is not found.
//
// The returned SecureValue is a defensive copy that is safe to use
// independently of the parent Loader. The caller is responsible for
// calling Close() or Release() on the returned value when no longer needed.
//
// Example:
//
//	sv := loader.GetSecure("API_KEY")
//	if sv != nil {
//	    defer sv.Release()
//	    // Use sv safely
//	}
func (sm *secureMap) GetSecure(key string) *SecureValue {
	shard := sm.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	if sv, ok := shard.values[key]; ok {
		// Acquire SecureValue's read lock to prevent data race
		sv.mu.RLock()
		if sv.closed.Load() {
			sv.mu.RUnlock()
			return nil
		}
		result := NewSecureValue(string(sv.data))
		sv.mu.RUnlock()
		return result
	}
	return nil
}

// Delete removes a value and securely clears it.
func (sm *secureMap) Delete(key string) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	var toRelease *SecureValue
	if sv, ok := shard.values[key]; ok {
		toRelease = sv // Save reference to release after unlocking
		delete(shard.values, key)
		sm.count.Add(-1)
	}
	shard.mu.Unlock()
	// Release old value outside of shard lock to avoid lock order inversion
	if toRelease != nil {
		toRelease.Release()
	}
}

// Clear removes all values and securely clears them.
func (sm *secureMap) Clear() {
	for i := range numSecureMapShards {
		shard := &sm.shards[i]
		shard.mu.Lock()
		// Collect values to release after unlocking
		toRelease := make([]*SecureValue, 0, len(shard.values))
		for _, sv := range shard.values {
			toRelease = append(toRelease, sv)
		}
		shard.values = make(map[string]*SecureValue)
		shard.mu.Unlock()
		// Release old values outside of shard lock to avoid lock order inversion
		for _, sv := range toRelease {
			sv.Release()
		}
	}
	// Reset count after clearing all shards
	sm.count.Store(0)
}

// Keys returns all keys in the map.
// Uses atomic counter for O(1) allocation sizing instead of double traversal.
//
// Note: In concurrent scenarios, the returned slice may have slightly different
// capacity than the actual number of keys due to concurrent modifications.
// This is a performance optimization - the capacity estimate is approximate
// and the actual key count may differ by the time the slice is allocated
// and the time keys are collected. The slice will grow automatically if needed.
// For an exact snapshot, external synchronization is required.
func (sm *secureMap) Keys() []string {
	// Use atomic count for fast capacity estimation
	totalKeys := int(sm.count.Load())

	// Handle edge case of empty map
	if totalKeys == 0 {
		return nil
	}

	keys := make([]string, 0, totalKeys)
	for i := range numSecureMapShards {
		shard := &sm.shards[i]
		shard.mu.RLock()
		for k := range shard.values {
			keys = append(keys, k)
		}
		shard.mu.RUnlock()
	}
	return keys
}

// Len returns the number of entries.
// Uses atomic counter for O(1) performance instead of traversing all shards.
//
// Note: In concurrent scenarios, the returned count may be slightly stale
// due to concurrent modifications. For an exact count,
// external synchronization is required.
func (sm *secureMap) Len() int {
	return int(sm.count.Load())
}

// ToMap returns a copy of all values as a regular map.
// The caller should be aware that this creates copies of sensitive data.
//
// Note: Uses atomic counter for O(1) allocation sizing.
// In concurrent scenarios, the returned map may have slightly different
// entries than the actual count due to concurrent modifications.
// For an exact snapshot, external synchronization is required.
func (sm *secureMap) ToMap() map[string]string {
	// Use atomic count for fast capacity estimation
	totalKeys := int(sm.count.Load())

	// Handle edge case of empty map
	if totalKeys == 0 {
		return nil
	}

	result := make(map[string]string, totalKeys)
	for i := range numSecureMapShards {
		shard := &sm.shards[i]
		shard.mu.RLock()
		for k, sv := range shard.values {
			// Acquire SecureValue's read lock to prevent data race
			sv.mu.RLock()
			if !sv.closed.Load() {
				result[k] = string(sv.data)
			}
			sv.mu.RUnlock()
		}
		shard.mu.RUnlock()
	}
	return result
}

// Compile-time check that secureMap implements EnvStorage.
var _ EnvStorage = (*secureMap)(nil)

// ============================================================================
// Utility Functions
// ============================================================================

// ClearBytes securely zeros a byte slice.
// Use this function to clear sensitive data returned by SecureValue.Bytes().
//
// Example:
//
//	data := sv.Bytes()
//	defer env.ClearBytes(data)
func ClearBytes(b []byte) {
	clear(b) // Use builtin for efficient zeroing (Go 1.21+)
}
