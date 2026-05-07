# Changelog

All notable changes to the cybergodev/env library will be documented in this file.

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
