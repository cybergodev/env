# Concurrency Safety Report

---

## Executive Summary

The `env` library is designed from the ground up to be **fully thread-safe** for production use in high-concurrency environments. All public APIs are safe for concurrent access by multiple goroutines without requiring external synchronization.

### Key Guarantees

| Guarantee | Status |
|-----------|--------|
| Thread-safe by default | ✅ Verified |
| No data races | ✅ Tested with `-race` flag |
| Safe concurrent read/write | ✅ Verified |
| Safe concurrent close | ✅ Verified |
| Memory safety on cleanup | ✅ Verified |

---

## Concurrency Architecture

### 1. Sharded Storage (secureMap)

The core storage mechanism uses a **sharded map architecture** to minimize lock contention:

```
┌─────────────────────────────────────────────────────┐
│                    secureMap                        │
├──────────┬──────────┬──────────┬─────────┬──────────┤
│ Shard 0  │ Shard 1  │ Shard 2  │   ...   │ Shard 31 │
│ RWMutex  │ RWMutex  │ RWMutex  │         │ RWMutex  │
│ map[K]V  │ map[K]V  │ map[K]V  │         │ map[K]V  │
└──────────┴──────────┴──────────┴─────────┴──────────┘
```

**Implementation Details:**
- **32 shards** for optimal distribution (raised from 8 by interleaved benchmarking; see the `numSecureMapShards` note in secure.go)
- **Lazy shard maps** — each shard's map is created on first write; reads over a nil map are no-ops, so empty loaders cost nothing
- **Hybrid hash** for shard selection — a loop-free little-endian byte combine for very short keys (1–4 chars), multiplicative (Knuth) XOR hashing for short keys (5–8 chars, the common case), full FNV-1a for medium keys (9–16 chars), and FNV-1a with first/last-8-byte sampling for longer keys (>16 chars)
- **Per-shard RWMutex** for fine-grained locking
- **Atomic counter** for O(1) `Len()` operations

```go
const numSecureMapShards = 32

type secureMapShard struct {
    mu     sync.RWMutex
    values map[string]*SecureValue
}

type secureMap struct {
    shards [numSecureMapShards]secureMapShard
    count  atomic.Int64 // Fast count without traversing shards
}
```

**Benefits:**
- Reduced lock contention vs single global lock
- Better scalability with CPU cores
- Predictable performance under load

### 2. Loader Synchronization

The `Loader` type uses comprehensive synchronization:

```go
type Loader struct {
    mu          sync.RWMutex        // Protects all mutable state
    vars        *secureMap          // Sharded thread-safe storage (immutable pointer)
    closedFast  atomic.Bool         // Mirrors `closed` for lock-free single-key reads
    applied     bool                // Access via lock
    appliedKeys map[string]struct{} // Process-env keys this loader set (via lock)
    closed      bool                // Access via lock
    loadTime    time.Time           // Protected by lock
    // ... other immutable fields (config, factory, parsers, fs)
}
```

**Locking Strategy:**
- **Multi-key read operations** (`All`, `Keys`, `ParseInto`, …) → `RLock()` / `RUnlock()` for cross-shard snapshot consistency
- **Write operations** → `Lock()` / `Unlock()`
- **Single-key reads** (`Lookup`, `GetSecure`, `GetString`) → lock-free: an atomic `closed` check plus the per-shard locks in `secureMap`. A read racing `Close()` either observes the value or the cleared state (graceful degradation, as guaranteed below); it cannot panic or tear
- **`Set` with `OverwriteExisting` and no `AutoApply`** → `RLock()` fast path: there is no check-then-act to serialize and no loader-level state is mutated, so only `secureMap`'s internal locks are needed
- **Always use `defer`** for unlock to prevent deadlocks

### 3. SecureValue Thread Safety

`SecureValue` uses a combination of mutex and atomic operations:

```go
type SecureValue struct {
    mu      sync.RWMutex     // Protects data access
    data    []byte           // Sensitive value
    closed  atomic.Bool      // Lock-free closed state check
    locked  bool             // Tracks if memory is mlock'd
    lockErr error            // Stores any mlock error for strict mode
}
```

**Safety Features:**
- Thread-safe read access via `RLock()`
- Safe concurrent close via `atomic.Bool`
- GC-safe cleanup with `runtime.SetFinalizer`
- Optional memory locking (mlock) to prevent swapping

### 4. Singleton Pattern (Default Loader)

The default loader is an **explicitly-initialized singleton** — it is *not* lazily
created on first access. It must be set once via `Load()`, `Overload()` or
`LoadWithConfig()` (all call `setDefaultLoader()` under a mutex). Subsequent
reads use a lock-free atomic pointer load and return `ErrNotInitialized` until
it has been set.

```go
var (
    defaultLoader atomic.Pointer[Loader]
    defaultMu     sync.Mutex
)

// getDefaultLoader is the fast read path used by every package-level getter.
// It only performs an atomic load — no creation, no locking.
func getDefaultLoader() (*Loader, error) {
    if loader := defaultLoader.Load(); loader != nil {
        return loader, nil
    }
    return nil, ErrNotInitialized // caller must call Load()/LoadWithConfig() first
}

// setDefaultLoader stores the singleton under the mutex, exactly once.
// Returns ErrAlreadyInitialized if a loader was already set.
func setDefaultLoader(loader *Loader) error {
    defaultMu.Lock()
    defer defaultMu.Unlock()
    if current := defaultLoader.Load(); current != nil {
        return ErrAlreadyInitialized
    }
    defaultLoader.Store(loader)
    return nil
}
```

**Guarantees:**
- Explicit, once-only initialization via `Load()` / `Overload()` / `LoadWithConfig()` (no silent auto-creation)
- Lock-free fast path for read access (atomic `Pointer.Load`)
- Safe concurrent reset via `Swap()` in `ResetDefaultLoader()`

### 5. ComponentFactory Lifecycle

Thread-safe factory with atomic close state:

```go
func (f *ComponentFactory) Close() error {
    if f == nil {
        return nil
    }

    // Atomic transition: open → closed
    if !f.closed.CompareAndSwap(false, true) {
        return nil // Already closed
    }

    f.mu.Lock()
    defer f.mu.Unlock()

    // Close the auditor only if it implements io.Closer
    if c, ok := f.auditor.(io.Closer); ok {
        return c.Close()
    }
    return nil
}
```

---

## Tested Scenarios

### Concurrent Read/Write Operations

```go
// Test: 4 scenarios, each with 10 goroutines performing a single operation
// category: reads (500 iterations: GetString/Lookup/GetSecure),
// Set (500, with OverwriteExisting), Keys/All/Len (200), Delete (300)
func TestLoader_Concurrent(t *testing.T)
```

**Operations Tested:**
- `GetString()`, `Lookup()`, `GetSecure()`
- `Keys()`, `All()`, `Len()`
- `Set()` (with `OverwriteExisting: true`)
- `Delete()`

> The `Set` read-lock fast path (`OverwriteExisting` without `AutoApply`) and
> concurrent `Close()` are additionally exercised by `TestLoader_ConcurrentFastPaths`.

**Result:** ✅ No data races, consistent reads and writes

### Concurrent Operations with Close

```go
// Test: Operations (Set fast path + lock-free reads + Close) running together
func TestLoader_ConcurrentFastPaths(t *testing.T)
```

**Result:** ✅ Graceful degradation, no panics

### High Concurrency Stress Test

```go
// Test: 50 goroutines × 10,000 iterations
func TestStress_HighConcurrency(t *testing.T)
```

**Result:** ✅ Stable under extreme load

---

## Race Detector Verification

All tests are regularly run with the Go race detector:

```bash
go test -race ./...
go test -race -count=200 ./...  # Flaky race detection
```

**Verified Packages:**
- `github.com/cybergodev/env` (root package)
- `github.com/cybergodev/env/internal` (internal package)

---

## Memory Safety Guarantees

### SecureValue Cleanup

| Scenario | Mechanism | Guarantee |
|----------|-----------|-----------|
| Explicit `Close()` | Mutex-protected clear | Immediate zeroing |
| Explicit `Release()` | Pool return with clear | Zeroing + reuse |
| GC collection | `runtime.SetFinalizer` | Automatic zeroing |
| `ClearBytes()` | Manual utility | Explicit zeroing |

### No Resource Leaks

- **SecureValue pool** prevents unbounded allocations
- **Loader.Close()** properly releases all resources
- **Factory.Close()** cleans up auditor connections

---

## Best Practices for Concurrent Use

### 1. Use Package-Level Functions

```go
// ✅ Thread-safe by default
env.Load(".env")
port := env.GetInt("PORT", 8080)
```

### 2. Share Loader Across Goroutines

```go
// ✅ Safe - Loader is thread-safe
loader, _ := env.New(cfg)

var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        _ = loader.GetString("KEY")
    }()
}
wg.Wait()
```

### 3. Proper Cleanup in Concurrent Code

```go
// ✅ Use defer for cleanup
loader, _ := env.New(cfg)
defer loader.Close()

// Or explicit close with error handling
if err := loader.Close(); err != nil {
    log.Printf("close error: %v", err)
}
```

### 4. SecureValue in Concurrent Contexts

```go
// ✅ Thread-safe access
sv := env.GetSecure("API_KEY")
if sv != nil {
    defer sv.Release()  // Safe to call from any goroutine

    // Multiple goroutines can read safely
    data := sv.Bytes()
    defer env.ClearBytes(data)
}
```

---

## Anti-Patterns to Avoid

### 1. Creating Multiple Loaders for Same File

```go
// ❌ Inefficient - creates duplicate resources
loader1, _ := env.New(cfg)
loader2, _ := env.New(cfg)  // Unnecessary duplication

// ✅ Use singleton or share loader
env.Load(".env")  // Package-level singleton
```

### 2. Not Closing Loaders

```go
// ❌ Resource leak
loader, _ := env.New(cfg)
// ... use loader
// Missing: loader.Close()

// ✅ Always close or use defer
loader, _ := env.New(cfg)
defer loader.Close()
```

### 3. Concurrent Access to Closed Loader

```go
// ❌ Undefined behavior
loader.Close()
val := loader.GetString("KEY")  // Returns "" (zero value) after close

// ✅ Check state before use
if !loader.IsClosed() {
    val := loader.GetString("KEY")
}
```

---

## Performance Characteristics

### Read Operations (Lock-Free Single-Key / Per-Shard RWMutex)

| Operation | Contention | Expected Performance |
|-----------|------------|---------------------|
| `GetString` | None | O(1) lock-free (atomic closed check + shard lock) |
| `GetInt` | None | O(1) lock-free |
| `Lookup` | None | O(1) lock-free |
| `Keys` | Medium | O(n) across shards (read lock) |
| `Len` | Low | O(1) atomic count (loader read lock + atomic counter) |

### Write Operations (Per-Shard Mutex)

| Operation | Contention | Expected Performance |
|-----------|------------|---------------------|
| `Set` (overwrite, no auto-apply) | Low | O(1) with read lock fast path |
| `Set` (other configs) | Medium | O(1) with Lock |
| `Delete` | Medium | O(1) with Lock |
| `SetAll` | Low | Batch per shard |

### Scalability

- **Horizontal:** Scales well with CPU cores (32 shards)
- **Vertical:** Memory usage bounded by `MaxVariables` config
- **Throughput:** ~1M+ ops/sec on modern hardware

---

## Conclusion

The `env` library provides **production-grade concurrency safety** through:

1. **Sharded architecture** for scalable performance
2. **Proper synchronization** with RWMutex and atomic operations
3. **Comprehensive testing** with race detector
4. **Safe resource lifecycle** with proper cleanup

The library is suitable for high-concurrency production workloads where thread safety is critical.

---

## Test Coverage

All concurrency tests are located in:

| File | Coverage |
|------|----------|
| `concurrent_test.go` | Loader, SecureMap, SecureValue, Singleton concurrency tests |
| `internal/concurrent_test.go` | InternKeyBytes, Expander, Auditor, Validator, Pool tests |
| `secure_test.go` | SecureValue/SecureMap, masking, and memory-lock functional tests |

Run all tests with race detection:
```bash
go test -race ./...
go test -race -count=200 ./...  # Extended race detection
```
