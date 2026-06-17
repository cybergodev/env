# Changelog

All notable changes to the cybergodev/env library will be documented in this file.

---

## v1.2.1 - Reliability & Parse-Path Performance (2026-06-18)

### Added
- `ExpansionErrorKind` type with `ExpansionDepthKind` / `ExpansionRequiredKind` constants — classify variable-expansion failures
- `ExpansionError.Kind` field exposing the failure kind; `ErrExpansionDepth` re-exported from the root package
- Strict memory-lock failure hook (`onStrictLockFailure`) makes lock failures observable in strict mode

### Changed
- `ExpansionError.Is()` now matches `ErrExpansionDepth` for depth/cycle errors (previously orphaned); `${VAR:?}` required-variable errors intentionally do not match
- `Loader.Validate` / `ParseInto` return `ErrClosed` for a closed loader, matching their documented contract
- `GetSliceFrom` uses the shared `enterRead`/`exitRead` accessors for consistent read-path guarding
- Parser scanner max token size raised to 256 KB so `ErrLineTooLong` / `ErrFileTooLarge` surface before `bufio.ErrTooLong`
- `secureMap.SetAll` batches values into per-shard slices; `InternKey` eviction simplified to single-entry
- `validateValueChars` uses a single-byte lookup table (removed unsafe 8-byte unrolling); `lookupBoolASCII` reuses shared `EqualFoldASCII`
- Examples: handle `Set`/`Delete` errors instead of discarding them; document the `examples` build tag for running samples

### Fixed
- JSON `FlattenJSON` pre-validates nesting depth before `json.Unmarshal` (DoS / stack-exhaustion defense; the YAML path already had it)
- `BufferedHandler.Flush` no longer drops events on write failure — re-queues the unwritten tail
- `SecureReader` accepts a stream ending exactly at the size limit instead of false-positive rejecting exact-size input
- `parseSliceElement` returns `*ValidationError` consistently for all numeric/float types
- `Loader.Close` marks the loader closed before closing the owned factory (no half-open state on failure)

### Performance
- Eliminated the per-line key string allocation on the `.env` parse path (`InternKeyBytes`, 0 allocs/op on cache hit)
- `Parser_MediumFile` -48% allocs / -20% time; `Parser_LargeFile` -50% allocs
- `LineParser` allocs 2 → 1; `Loader_LoadFiles_Medium` -24% allocs
- Skip uppercase-key index build when the validator has no required keys (~28% of parse-path bytes removed)
- `secureMap.SetAll`: -15.8% B/op, -12.7% allocs/op, -11.3% ns/op

---

## v1.2.0 - Unified Key Resolution & Production Hardening (2026-05-20)

### Breaking
- Lazy singleton init removed — `Load()` must be called before convenience functions; returns `ErrNotInitialized` otherwise
- `ComponentFactory.LineParserValidator/Auditor/Expander()` privatized (returned internal-only types)
- Parser `Close()` methods removed (were no-ops; parsers own no resources)
- `SecureValue.String()` returns masked representation — use `Reveal()` for plaintext

### Added
- `GetFloat64()` / `GetUint64()` — typed access for float64 and uint64 values (instance + global)
- `ErrNotInitialized` — sentinel error for uninitialized default loader
- `SetMemoryLockStrict()` / `NewSecureValueStrict()` — strict memory lock mode
- `DetectFormat()` — auto-detect file format (.env / JSON / YAML) from content
- `ResolveKeyName()` / `LookupInMap()` — shared case-insensitive + dot-notation key resolution
- `EqualFoldASCII()` / `HasUpperPrefix()` — zero-allocation ASCII case utilities
- `PutBuilderDiscard()` — opt-out of builder pooling for sensitive content
- `ReleaseValue()` — return YAML `Value` nodes to sync.Pool after use
- Windows forbidden keys: COMSPEC, PATHEXT, SYSTEMROOT, WINDIR
- Nil receiver guards on SecureValue (10 methods), Loader (17 methods), ComponentFactory
- 21 `Example*` test functions for pkg.go.dev documentation visibility

### Changed
- Key resolution unified: `GetSecure`, `StructInto`, `Lookup` all use case-insensitive + dot-notation strategy
- Prefix filtering in `LoadFiles` is case-insensitive
- `CloseableChannelHandler.Log` returns error when channel full instead of blocking
- `BufferedHandler.Close` guarantees flush goroutine exits via `sync.WaitGroup`
- `ValidationError.Is()` only matches `ErrInvalidValue` for value-related rules

### Fixed
- TOCTOU race in `GetSecure` — single atomic lookup replaces two-step exists+get
- `ResetDefaultLoader` race — `Close()` runs under mutex
- JSON/YAML `SecureReader` `MaxLineLength` was hardcoded 0 (unlimited line length)
- Empty string values incorrectly treated as "not found" in Get/GetSecure/ToMap
- Concurrent Close+Log panic in `CloseableChannelHandler` (mutex around channel close)
- Recursive comment skipping stack overflow in `parseNestedValue` (iterative loop)
- Duplicate package doc comments causing godoc conflict
- Benchmark temp paths blocked by path validator on Windows
- Example code printed passwords in plaintext; used wrong struct tags

### Performance
- `Parser_WithExpansion`: -25% time, -54% memory, -48% allocs
- `YAMLParser_Medium`: -16% time, -24% memory, -54% allocs
- `YAMLParser_Small`: -11% time, -26% memory, -48% allocs
- `Expander_SingleVariable`: -19% time; `BracedVariable`: -14% time
- `Parser_LargeFile`: -11% time
- `looksLikeNumber()` fast path eliminates ~732 MB/iter of failed parse allocations
- YAML/JSON key builder uses direct concatenation for keys ≤64 chars (avoids pool overhead)
- `buildArrayIndex` coverage: 34.8% → 100%; overall: 83.0% → 87.9%

---

## v1.1.1 - Production Readiness & Performance (2026-05-07)

### Added
- `Reveal()` — explicit plaintext access method on `SecureValue`
- `ErrDuplicateKey` — sentinel error for duplicate key detection

### Changed
- `ForceRegisterParser()` prints security warning when overriding built-in parsers
- `secureMap.Get()` acquires read lock for thread-safety consistency

### Fixed
- Stale `Config()` godoc referencing non-existent `Security.*` fields
- Transient loader inconsistency during `setDefaultLoader` rollback
- Sensitive data residue in pooled scanner buffers

### Security
- `ValidationError` messages now consistently mask sensitive values
- `CloseableChannelHandler` uses select/done pattern instead of recover()

### Performance
- `secureMap.Set` updates in-place, 25-100% fewer allocs for overwrite workloads
- `parseBool` uses byte-level comparison, eliminating per-parse allocations
- `detectDataFormat` uses `IndexByte` scanning instead of `strings.Split`
- `Loader.Set` skips redundant lookup when `OverwriteExisting=true`

---

## v1.1.0 - Performance & Architecture Refactoring (2026-03-22)

### Breaking Changes
- Removed deprecated grouped accessors: use direct `Config` field access instead of `GetFileConfig()`/`SetFileConfig()`, `GetValidationConfig()`/`SetValidationConfig()`, etc.

### Added
- `ForceRegisterParser()` — allows overriding built-in parsers for advanced use cases
- `ToUpperASCIISafe()` / `IsASCII()` — fast ASCII validation with zero overhead
- `ErrNonASCII` / `ErrValidateRequiredUnsupported` — explicit sentinel errors
- `ValidateUTF8` config option — optional UTF-8 value validation
- `CloseableChannelHandler` — audit handler with owned channel lifecycle
- Config sub-structs for grouped access documentation

### Changed
- `New()` accepts optional `Config` parameter; zero-value defaults to `DefaultConfig()`
- Singleton error cache expires after 30s for transient failure recovery
- Extracted adapter types to `adapters.go` for better organization
- `finalize()` now mutex-protected for thread-safety

### Fixed
- `ValidateRequired()` returns explicit error instead of silent `nil` for minimal validators
- `containsIgnoreCase()` now handles non-ASCII input correctly
- Resource leak in `parseString()` with double `Close()` call
- Inaccurate pre-allocation in `buildChain()` error messages

### Security
- `InternKey()` cache consistency improved with FIFO eviction correctness
- `validateValueChars()` unsafe pointer usage documented with safety invariants
- Added security invariant documentation for fast path operations

### Performance
- Parser: ~9% faster, ~9% less memory for large files
- YAML parser: ~9% faster, ~25% less memory for medium files
- JSON parser: ~6% faster
- Key validation: ~5-10% improvement
- `SanitizeForLog()`: O(n*m) → single-pass scanning

---

## v1.0.1 - Security Hardening & Performance (2026-03-19)

### Added
- `CloseableChannelHandler` — audit handler with owned channel lifecycle for proper resource cleanup
- `validateResolvedPath()` — symlink escape attack prevention for file paths
- `io.Closer` compile-time interface checks for all closeable types

### Changed
- `New()` now supports optional Config parameter; zero-value defaults to `DefaultConfig()`
- `ExpandAll` returns original map when no expansion needed (14.5% faster, 31.8% less memory)
- Use Go 1.21+ `clear()` builtin for byte-zeroing operations
- `ChannelHandler` documentation clarified: caller owns channel lifecycle

### Fixed
- Symlink escape attacks blocked with `filepath.EvalSymlinks()` validation
- Sensitive keys masked in variable expansion error chains
- Channel ownership ambiguity — documented caller responsibility for closing

### Security
- TOCTOU defense-in-depth documentation for file loading operations
- Windows `VirtualLock` privilege requirements documented for production deployments

---

## v1.0.0 - Initial Release (2026-03-01)

### Core Features

| Feature | Description |
|---------|-------------|
| **Multi-Format Support** | Auto-detect and parse `.env`, `.json`, `.yaml` files |
| **Type-Safe Access** | `GetString`, `GetInt`, `GetBool`, `GetDuration`, `GetSlice[T]` |
| **Variable Expansion** | Full `${VAR}`, `${VAR:-default}`, `${VAR-default}` syntax |
| **Struct Mapping** | `ParseInto`, `env` tags with `envDefault` support |
| **Serialization** | `Marshal`/`UnmarshalMap`/`UnmarshalStruct` for env/JSON/YAML |

### Security

| Feature | Description |
|---------|-------------|
| **SecureValue** | Auto-zeroing memory, GC-safe cleanup, memory pooling |
| **Memory Locking** | Cross-platform `mlock`/`VirtualLock` support (Unix/Windows) |
| **Sensitive Masking** | Auto-detect and mask passwords, tokens, API keys |
| **Path Protection** | Block traversal (`..`), absolute paths, UNC paths |
| **Forbidden Keys** | Prevent `PATH`, `LD_PRELOAD`, `DYLD_*`, etc. override |
| **Input Validation** | Null bytes, control chars, size limits, expansion depth |

### Concurrency

| Feature | Description |
|---------|-------------|
| **Sharded Storage** | 8 shards with FNV-1a hash distribution |
| **Thread-Safe** | RWMutex per shard, atomic counters |
| **Memory Pools** | `sync.Pool` for SecureValue, Parser, Scanner buffers |

### Audit

| Feature | Description |
|---------|-------------|
| **Handlers** | JSON, Log, Channel, Nop implementations |
| **Actions** | Load, Parse, Get, Set, Delete, Validate, Expand, Security, Error |

### Configuration

| Preset | Use Case |
|--------|----------|
| `DefaultConfig()` | Secure defaults for general use |
| `DevelopmentConfig()` | Relaxed limits, overwrite enabled |
| `TestingConfig()` | Tight limits, isolated testing |
| `ProductionConfig()` | Strict security, audit enabled |

### Limits (Defaults / Hard)

| Setting | Default | Hard Limit |
|---------|---------|------------|
| MaxFileSize | 2 MB | 100 MB |
| MaxLineLength | 1,024 | 64 KB |
| MaxKeyLength | 64 | 1,024 |
| MaxValueLength | 4,096 | 1 MB |
| MaxVariables | 500 | 10,000 |
| MaxExpansionDepth | 5 | 20 |

### API Surface

**Package Functions:** `Load`, `GetString`, `GetInt`, `GetBool`, `GetDuration`, `GetSlice[T]`, `Lookup`, `Set`, `Delete`, `Keys`, `All`, `Len`, `GetSecure`, `Validate`, `ParseInto`, `Marshal`, `UnmarshalMap`, `UnmarshalStruct`, `New`, `ResetDefaultLoader`

**Utility Functions:** `IsSensitiveKey`, `MaskValue`, `MaskKey`, `MaskSensitiveInString`, `SanitizeForLog`, `DetectFormat`, `ClearBytes`, `NewSecureValue`, `SetMemoryLockEnabled`, `IsMemoryLockSupported`

**Loader Methods:** `LoadFiles`, `Apply`, `Validate`, `Close`, `IsApplied`, `IsClosed`, `LoadTime`, `Config`

**SecureValue Methods:** `String`, `Bytes`, `Length`, `Masked`, `Close`, `Release`, `IsClosed`, `IsMemoryLocked`, `MemoryLockError`

### Requirements

- Go 1.24+
- Zero external dependencies

---
