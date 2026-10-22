package env

import (
	"fmt"
	"io"
	"runtime"
	"strconv"
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
//
// FINALIZER INVARIANT: the finalizer is set exactly once, when a SecureValue
// object is first created, and is never cleared. Keeping it set while the
// object sits in the pool is safe because sync.Pool holds a reference to
// pooled objects, and finalizers only run on unreachable objects:
//   - If the runtime evicts a closed SecureValue from the pool, the finalizer
//     observes closed=true and is a no-op.
//   - If a caller abandons an OPEN SecureValue (the case the finalizer exists
//     for — e.g. a dropped GetSecure result or an unclosed Loader), the
//     finalizer runs and zeroes the data, exactly as before.
//
// SetFinalizer is a relatively expensive runtime call (~60% of NewSecureValue
// cost in profiles), so paying it once per object lifetime instead of on
// every pool cycle is a significant win on the Set/SetAll hot paths.
var secureValuePool = sync.Pool{
	New: func() any {
		sv := &SecureValue{}
		runtime.SetFinalizer(sv, (*SecureValue).finalize)
		return sv
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
		// Fallback: create new SecureValue if pool returns unexpected type.
		// The finalizer is set here (and in pool.New) exactly once per object;
		// see the FINALIZER INVARIANT on secureValuePool.
		sv = &SecureValue{}
		runtime.SetFinalizer(sv, (*SecureValue).finalize)
	}
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
// Note: The finalizer is set once at object creation (see the FINALIZER
// INVARIANT on secureValuePool) and needs no maintenance here.
//
// State consistency: The entire operation is protected by mutex lock,
// ensuring no concurrent reads can observe partial state. We mark the
// SecureValue as open (closed=false) only after data is fully prepared.
//
// Lock ordering: secureMap reaches this method under its shard lock
// (shard.mu.Lock → sv.mu.Lock). secureMap.Get deliberately skips sv.mu and
// reads sv.data under shard.mu.RLock alone, so every writer of sv.data —
// this method included — MUST be reached through shard.mu.Lock (see the lock
// analysis on secureMap.Get). Do not add an sv.data write path that is not
// shard-locked.
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
		// SECURITY: clear the full capacity, not just len — the buffer is
		// dropped or re-sliced below, and clearing to cap guarantees no byte
		// of the old value can survive in [len, cap) slack.
		clear(sv.data[:cap(sv.data)])
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
		// The full-capacity clear above already wiped the whole buffer, so
		// shrinking to valueLen leaves only zeroed slack in [valueLen, cap).
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
//
// Strict mode: when SetMemoryLockStrict(true) has been enabled, a lock failure
// is surfaced through onStrictLockFailure so high-security callers are
// notified (the default handler logs to stderr). The SecureValue itself stays
// valid and usable; the data is simply not protected from swapping.
func (sv *SecureValue) tryLockMemory() {
	if !IsMemoryLockEnabled() || len(sv.data) == 0 {
		return
	}

	err := internal.LockMemory(sv.data)
	if err != nil {
		sv.lockErr = err
		// Strict mode: make the failure observable. In non-strict mode we
		// continue silently — the data is still usable, just not locked.
		if IsMemoryLockStrict() {
			if h := onStrictLockFailure.Load(); h != nil {
				(*h)(err)
			}
		}
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
// Pool interaction (see the FINALIZER INVARIANT on secureValuePool):
//   - The finalizer stays set for the object's entire lifetime
//   - Objects referenced by the pool (or by a live secureMap shard) are
//     reachable, so this method cannot run on them
//   - Pooled objects evicted by the runtime are always closed (Release closed
//     them before pooling), so the fast-path check below makes this a no-op
//   - Additional mutex protection provides defense-in-depth
func (sv *SecureValue) finalize() {
	// Fast path: if already closed, nothing to do
	if sv.closed.Load() {
		return
	}

	// SECURITY: Acquire mutex to prevent race with Release()
	// This is defense-in-depth for the abandonment case (an open SecureValue
	// becoming unreachable without an explicit Close/Release).
	sv.mu.Lock()
	defer sv.mu.Unlock()

	// Double-check after acquiring lock (standard pattern)
	if sv.closed.Load() {
		return
	}

	sv.clearDataLocked()
	sv.closed.Store(true)
}

// clearDataLocked securely zeros the data slice's full capacity, including
// any [len, cap) slack left by earlier re-slices.
// Uses volatile-style writes through unsafe.Pointer to prevent compiler optimization.
// Must be called with sv.mu held.
//
// Like reset, this is a writer of sv.data and participates in the secureMap
// lock-order contract: secureMap.Delete/Clear remove the SV from the shard
// map under shard.mu.Lock and release it outside that lock, so by the time
// this method runs the SV is no longer reachable through the map (see the
// lock analysis on secureMap.Get). Do not add an sv.data write path that
// bypasses that ordering.
func (sv *SecureValue) clearDataLocked() {
	// SECURITY: operate on the full capacity so bytes in [len, cap) slack are
	// wiped too. cap == 0 covers both nil and empty slices.
	full := sv.data[:cap(sv.data)]
	if len(full) == 0 {
		return
	}

	// Unlock memory before clearing (if it was locked)
	if sv.locked {
		internal.UnlockMemory(sv.data)
		sv.locked = false
	}

	// Use volatile-style clearing to prevent compiler optimization
	dataPtr := unsafe.Pointer(&full[0])
	for i := range full {
		*(*byte)(unsafe.Add(dataPtr, i)) = 0
	}
	runtime.KeepAlive(full)
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
// The finalizer is intentionally NOT cleared before pooling — see the
// FINALIZER INVARIANT on secureValuePool. The object stays closed here, so
// even if the runtime later finalizes an evicted pooled object, finalize()
// observes closed=true and is a no-op.
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

	// Build without fmt.Sprintf to avoid reflection and interface-boxing overhead.
	// Format: "[SECURE:<N> bytes<lockStatus>]"
	var buf [48]byte
	n := copy(buf[:], "[SECURE:")
	n = len(strconv.AppendInt(buf[:n], int64(len(sv.data)), 10))
	n += copy(buf[n:], " bytes")
	n += copy(buf[n:], lockStatus)
	buf[n] = ']'
	n++
	return string(buf[:n])
}

// MarshalJSON implements json.Marshaler.
// It returns a redacted representation to prevent accidental serialization
// of the secret value through json.Marshal or similar reflection-based
// serializers. The plaintext is never included in JSON output.
func (sv *SecureValue) MarshalJSON() ([]byte, error) {
	if sv == nil {
		return []byte("null"), nil
	}
	return []byte(`"` + sv.String() + `"`), nil
}

// MarshalText implements encoding.TextMarshaler.
// It returns a masked representation consistent with String() to prevent
// accidental exposure through text-based encoders (e.g. encoding/xml,
// text/template, log structured loggers).
func (sv *SecureValue) MarshalText() ([]byte, error) {
	if sv == nil {
		return []byte("[NIL]"), nil
	}
	return []byte(sv.String()), nil
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
//
// Chosen by interleaved benchmarking of 8/16/32 shards plus a padded-8
// variant (same binary, alternating runs to cancel machine drift on this
// hybrid P/E-core system): 32 shards cut ConcurrentGet by ~42% (the RWMutex
// readerCount RMW was the top CPU item under concurrent reads — fewer
// readers per shard means fewer caches bouncing the same lock word) and
// match or beat the smaller counts on single-goroutine Get/Lookup. The one
// measured cost is batch SetAll (+~18% for 100 keys: more per-shard locks
// and map heads), a load-time-only operation. Shard-struct cache padding was
// tested and rejected: it inflated the loader working set and slowed every
// access path. Shard maps are created lazily, so an empty loader allocates
// none of them.
//
// Must remain a power of two (HashKey masks with numSecureMapShards-1).
const numSecureMapShards = 32

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
// Shard maps are created lazily on first write: reads and iteration over a
// nil map are valid no-ops in Go, so an empty (or briefly-used) loader does
// not pay for any of the 32 empty map headers up front.
func newSecureMap() *secureMap {
	return &secureMap{}
}

// shardMapLocked returns the shard's map, creating it on first use.
// The caller must already hold shard.mu.
func shardMapLocked(shard *secureMapShard) map[string]*SecureValue {
	if shard.values == nil {
		shard.values = make(map[string]*SecureValue)
	}
	return shard.values
}

// getShard returns the shard for a given key.
func (sm *secureMap) getShard(key string) *secureMapShard {
	return &sm.shards[hashKey(key)]
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
		shardMapLocked(shard)[key] = NewSecureValue(value)
		sm.count.Add(1)
		shard.mu.Unlock()
	}
}

// secureKV is a key/value pair used to batch values into shards in SetAll.
// A slice of pairs is more compact than the previous per-shard
// map[string]string grouping: a single contiguous array with no per-bucket
// overhead, and one allocation per non-empty shard instead of a map (which
// allocates a bucket array) per shard.
type secureKV struct {
	key   string
	value string
}

// bucketByShard distributes values into per-shard slices backed by a single
// flat allocation. One contiguous slice is cheaper to allocate and to GC than
// up to numSecureMapShards separate slices, and gives better locality when
// each shard's batch is processed in order. This is the shared bucketing
// logic used by SetAll and SetAllIfAbsent.
func bucketByShard(values map[string]string) [numSecureMapShards][]secureKV {
	// First pass: count items per shard and turn the counts into prefix
	// offsets (offsets[i] is the start of shard i's segment in flat).
	var offsets [numSecureMapShards + 1]int
	for key := range values {
		offsets[hashKey(key)+1]++
	}
	for i := 1; i <= numSecureMapShards; i++ {
		offsets[i] += offsets[i-1]
	}

	// Second pass: distribute values into the per-shard segments. Each
	// segment starts empty with capacity capped at its exact count, so
	// appends always fit in the flat slice and never reallocate.
	flat := make([]secureKV, len(values))
	var buckets [numSecureMapShards][]secureKV
	for i := range numSecureMapShards {
		buckets[i] = flat[offsets[i]:offsets[i]:offsets[i+1]]
	}
	for key, value := range values {
		idx := hashKey(key)
		buckets[idx] = append(buckets[idx], secureKV{key: key, value: value})
	}

	return buckets
}

// SetAll stores multiple values securely in a batch operation.
// This is more efficient than calling Set multiple times as it
// groups operations by shard to minimize lock acquisitions.
func (sm *secureMap) SetAll(values map[string]string) {
	if len(values) == 0 {
		return
	}

	buckets := bucketByShard(values)

	// Process each shard under a single lock.
	for i := range numSecureMapShards {
		if len(buckets[i]) == 0 {
			continue
		}
		sm.setShardValues(i, buckets[i])
	}
}

// setShardValues sets multiple values in a single shard.
// Uses in-place updates for existing keys to reduce allocations.
// The shard map grows on demand via Go's native map reallocation — the
// previous manual pre-sizing did not outperform the runtime and added
// maintenance cost, so it was removed.
func (sm *secureMap) setShardValues(shardIdx int, pairs []secureKV) {
	shard := &sm.shards[shardIdx]
	shard.mu.Lock()

	// Track new keys for count update
	newKeys := 0
	values := shardMapLocked(shard)
	for i := range pairs {
		key, value := pairs[i].key, pairs[i].value
		if existing, ok := values[key]; ok {
			// In-place update: reuse existing SecureValue
			existing.reset(value)
		} else {
			values[key] = NewSecureValue(value)
			newKeys++
		}
	}
	shard.mu.Unlock()

	// Update count after releasing lock
	if newKeys > 0 {
		sm.count.Add(int64(newKeys))
	}
}

// SetAllIfAbsent inserts values whose keys do not already exist.
// Existing keys are left unchanged. Returns the number of new keys inserted.
//
// This is more efficient than the pattern of checking Has() per key then
// calling SetAll() because it avoids creating an intermediate filtered map
// and acquires each shard lock only once for the entire batch.
func (sm *secureMap) SetAllIfAbsent(values map[string]string) int {
	if len(values) == 0 {
		return 0
	}

	buckets := bucketByShard(values)

	// Process each shard under a single lock, inserting only new keys.
	newKeys := 0
	for i := range numSecureMapShards {
		if len(buckets[i]) == 0 {
			continue
		}
		shard := &sm.shards[i]
		shard.mu.Lock()
		values := shardMapLocked(shard)
		for j := range buckets[i] {
			key, value := buckets[i][j].key, buckets[i][j].value
			if _, ok := values[key]; !ok {
				values[key] = NewSecureValue(value)
				newKeys++
			}
		}
		shard.mu.Unlock()
	}

	if newKeys > 0 {
		sm.count.Add(int64(newKeys))
	}
	return newKeys
}

// Has reports whether a key exists without allocating the value string.
// This is significantly faster than Get when only existence is needed
// (e.g., overwrite checks) because it avoids the string(sv.data) copy
// and the SecureValue read lock — only the closed flag is read atomically.
func (sm *secureMap) Has(key string) bool {
	shard := sm.getShard(key)
	shard.mu.RLock()
	sv, ok := shard.values[key]
	if !ok {
		shard.mu.RUnlock()
		return false
	}
	// closed is atomic.Bool — safe to read without SV lock.
	// A concurrent Close() could set closed right after this check,
	// but that's inherent in concurrent access; Has() reports the
	// state at the instant of the call.
	closed := sv.closed.Load()
	shard.mu.RUnlock()
	return !closed
}

// Get retrieves a value. Returns the value and whether it exists.
//
// Lock analysis: sv.mu.RLock is intentionally NOT acquired here. All writes to
// sv.data (reset, clearDataLocked) occur under shard.mu.Lock — either via
// Set/setShardValues (which Lock the shard before calling reset) or via
// Delete/Clear (which Lock the shard, remove the SV from the map, Unlock, then
// Release the SV outside the lock). Therefore holding shard.mu.RLock is
// sufficient to guarantee sv.data and sv.closed are stable for the duration of
// this read. sv.closed is atomic.Bool, so the Load is safe without sv.mu.
// This eliminates one RLock/RUnlock pair per Get — the single largest CPU
// hotspot identified by profiling (23.6% in atomic operations).
func (sm *secureMap) Get(key string) (string, bool) {
	shard := sm.getShard(key)
	shard.mu.RLock()
	sv, ok := shard.values[key]
	if !ok {
		shard.mu.RUnlock()
		return "", false
	}
	if sv.closed.Load() {
		shard.mu.RUnlock()
		return "", false
	}
	// data may be nil for empty string values — string(nil) == "" is correct.
	// sv.data is stable because shard.mu.RLock blocks all writers (see comment above).
	result := string(sv.data)
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
		// sv.data is stable under shard.mu.RLock — see Get() lock analysis.
		if sv.closed.Load() {
			return nil
		}
		return NewSecureValue(string(sv.data))
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
		// Drop the shard map entirely (lazy re-creation on next write)
		// instead of keeping an empty map header alive.
		shard.values = nil
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
// The caller should be aware that this creates copies of sensitive data
// outside SecureValue's protected lifecycle (SEC-010): the strings are
// ordinary Go memory that is neither zeroed on release nor memory-lockable.
// Prefer GetSecure for values that must remain protected.
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
		// sv.data is stable under shard.mu.RLock — see Get() lock analysis.
		for k, sv := range shard.values {
			if !sv.closed.Load() {
				result[k] = string(sv.data)
			}
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
