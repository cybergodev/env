# Changelog

All notable changes to the cybergodev/env library will be documented in this file.

---

## v1.3.1 - Panic Containment & YAML Correctness (2026-10-25)

### Added
- Panic containment for every config-injected component — a panic in a custom `Validator`/`Expander`/`Auditor`/`FileSystem`/parser or a struct-field (un)marshaler callback surfaces as an error instead of crashing the process
- `ClearInternCache()` — exported release knob for long-running applications to clear the key-intern cache
- CI fuzz job — 30s mutation fuzzing per PR for the `.env`/JSON/YAML parsers (previously an audit-time-only exercise)

### Fixed
- Custom `KeyPattern`s no longer have to match a literal sample key — namespace patterns like `^MYAPP_[A-Z0-9_]*$` are accepted; unsafe patterns (empty, numeric-start, `=`/`:`/control chars) remain rejected
- Path validation for injected (non-OS) filesystems no longer consults the host disk, so a custom-FS load can no longer be accepted or rejected based on files outside the injected boundary
- `[]time.Duration` struct fields round-trip through `ParseInto`/`MarshalStruct`, symmetric with `GetSlice[time.Duration]`
- `MarshalStruct` emits only the `KEY` from `env:"KEY,envDefault:V"` tags instead of the whole tag string as the map key
- `UnmarshalMapWithConfig` closes custom parsers (`io.Closer`) on both the success and the parse-error path
- `Set` with `AutoApply` audit-logs `os.Setenv` failures, matching `Apply`'s failure branch
- YAML values containing `': '` (cookie-style inline objects) no longer corrupt or drop their file; glued-colon lines (`localhost:8080`) and unknown double-quoted escapes are preserved

### Changed
- `JSONBoolAsString` is deprecated and its default is now `false` — it has never affected JSON output (JSON bools always render `"true"`/`"false"` canonically)
- `Loader.Validate` builds its uppercase-key index through the shared pool instead of allocating a fresh map per call
- Package and Global Mode docs clarify the uninitialized-vs-closed error split (`ErrNotInitialized` vs `ErrClosed`) and the `AutoApply` divergence between `Load`/`New`
- `ComponentConfig` documents the panic contract for custom components; `NewChannelAuditHandler` documents the channel close-ordering hazard

---

## v1.3.0 - Security Hardening, Duplicate-Key Policy & YAML Correctness (2026-10-22)

### Added
- `env.Overload()` — godotenv-convention initializer: later files win shared keys and file values replace process-environment variables
- `UnmarshalMapWithConfig` / `UnmarshalStructWithConfig` — one-shot parsing with full `Config` semantics (limits, duplicate policy, validation, prefix)
- `ParsingConfig.DuplicateKeyPolicy` (`Inherit`/`FirstWins`/`LastWins`/`Error`) — per-document duplicate-key handling for `.env`/YAML; duplicate-bearing JSON always errors, clean documents load unchanged
- `ComponentConfig.AuditFailClosed` + `ErrAuditFailed` — opt-in fail-closed auditing: `LoadFiles`/`Apply`/`Set` fail when an audit event is lost
- `Auditor.SetFailClosed`/`TakeFailure`/`Failures` and `CloseableChannelHandler.DroppedEvents` — lost-audit-event observability
- YAML block scalars (`|`/`>` with chomp and explicit-indent indicators) — multi-line values such as certificates and keys load verbatim
- YAML flow collections (`[a, b]`, `{x: 1}`) flatten into indexed/prefixed keys instead of being stored as raw text
- `${VAR:=default}` assigns the expanded default POSIX-style when the variable is unset everywhere
- Permanent fuzz targets for all three parsers (`FuzzParseEnv`/`FuzzParseJSON`/`FuzzParseYAML`)
- CI pipeline: vet + race tests on an Ubuntu/Windows matrix, golangci-lint with gosec, coverage gate ≥ 90%
- Examples 12 (extensibility) and 13 (customization) — the 13-example suite now demonstrates the full public API

### Fixed
- Cyclic and self-referential struct types no longer crash `ParseInto`/`UnmarshalStruct`/`MarshalStruct` with fatal OOM — recursion is depth-capped and returns `*MarshalError`
- `time.Time` and other `TextMarshaler`/`TextUnmarshaler` struct fields round-trip through struct mapping (previously silent no-ops in both directions)
- Dotted top-level keys (`apnews.com`) resolve through `Lookup`/`GetString`/`GetSecure`/`GetSliceFrom` — the literal key is probed before dot-notation candidates
- YAML values embedding `": "` inside inline JSON (cookie-style nested objects) no longer corrupt the whole file
- `ParseInto` no longer self-deadlocks when a user `UnmarshalText`/`UnmarshalEnv` callback re-enters `Set`/`Delete`/`Close`
- `GetSliceFrom`'s default no longer shadows data stored under uppercase or dot-notation candidate keys
- False "cycle detected" failures eliminated — single-colon `${VAR:x}` references, literal single-quoted keys, and unused default text no longer close phantom cycles
- `.env` quoted values accept trailing `# comments`; unknown double-quoted escapes keep their backslash (`C:\Users\foo` no longer loses `\`)
- UTF-8 BOM stripped at every read entry point (Windows editors and `Out-File` output load cleanly)
- Windows path hardening: reserved device names (`CON`, `NUL.txt`) rejected in every component, NTFS alternate data streams (`file.env:stream`) rejected, backslash-rooted paths (`\etc\passwd`) rejected on all platforms
- Filenames containing interior `..` (`my..file.env`) load — traversal detection is now per-component
- YAML indentation is column-based — odd-width indents (a 1-space child) nest correctly instead of silently re-parenting keys
- `Set` with `AutoApply` stores the value only after `Setenv` succeeds (failed sets stay retryable); `Delete` reports `Unsetenv` failures instead of swallowing them
- `Loader.Close` closes custom parsers implementing `io.Closer` (previously leaked for the loader's lifetime)
- A zero-value `Loader` behaves like a closed loader instead of panicking; a panicking `ParserFactory` callback becomes a creation error instead of a crash
- `[]string` elements containing commas, quotes, or significant whitespace round-trip through `Marshal`/`ParseInto`
- `float32` struct fields format and parse at their own bit width (`0.1` no longer prints float64 digits)
- Duplicate `.env` lines no longer trip `MaxVariables` — the limit counts distinct keys
- `Config.IsZero` covers every field — partially-set configs are no longer silently replaced by `DefaultConfig` (pinned by a reflection test)
- `AuditEnabled=false` now also gates `Set`/`Delete` audit events, matching the documented contract

### Changed
- Default limits raised for realistic configs: `MaxKeyLength` 64→128, `MaxVariables` 500→1000, `MaxFileSize` 2MB→8MB, `MaxLineLength`/`MaxValueLength` 1024/4096→8192, structured nesting depth 10→16 (hard caps unchanged)
- `Set` accepts the structured key space — dotted keys like `service.host-name` set like their YAML/JSON counterparts (custom `KeyPattern` still governs; `PATH`/`LD_PRELOAD` stay forbidden)
- Variable expansion applies to `.env` values only — JSON and YAML documents never expand (now stated consistently across godoc and README)
- `Marshal` round-trip fidelity: `null`/`~` and `$`-bearing values are quoted/escaped, and JSON marshaling preserves non-canonical number strings (`007`, `1e3`) verbatim
- One loose boolean grammar across typed getters and struct mapping — `DEBUG=yes` parses in both
- Audit hardening: `SetEnabled` after `Close` is a no-op, `JSONHandler.Close` is independently race-safe, and handler write failures surface with context
- `docs/SECURITY.md` discloses the `SecureValue` protection scope — unlocked string copies, lock/zero windows, dump exposure, and masking side channels
- Configuration style contract documented: `Config` is the single configuration channel, with zero-value vs partially-set semantics and the process-global knobs enumerated
- Test suite deduplicated and table-driven; combined coverage 95.9% → 97.9%

### Performance
- Intern front cache (lock-free direct-mapped slots) plus token-queue and value-node pooling: nested YAML parse -62% B/op, -25% allocs/op
- Scoped scanner-buffer clearing (only the region that held file content): `Parser_SmallFile` -22.7% time
- Single-allocation uppercase flat-key joins: `YAMLParser_Nested` -15.5% and `JSONParser_Nested` -16.1% allocs/op
- secureMap shards 8→32 (`Loader_ConcurrentGet` -42%), word-at-a-time value validation, and a registry fast path for parser construction

---

## v1.2.3 - Security Hardening, Correctness & Performance (2026-09-07)

### Breaking
- Conversion errors from `ParseInto`/`UnmarshalInto`/`UnmarshalStruct` changed type: raw `*strconv.NumError`/time errors → `*env.MarshalError`; match via `errors.As` or `IsMarshalError()` (offending values are no longer embedded in error messages)
- Custom `KeyPattern`s that accept injection-enabling syntax (`=`, `:`, newline, NUL) now fail `Config` validation instead of enabling key injection on re-parse
- Pathological structured documents (>200K YAML tokens / JSON nodes) now fail fast with `YAMLError`/`JSONError` instead of unbounded allocation (a 2MB file previously amplified to ~644MB)
- `MaskSensitiveInString` output changes for pattern-bearing strings (embedded secrets are now masked); YAML `Marshal` now quotes coercible scalars (`PORT: "8080"`) so values survive round-trips unchanged

### Fixed
- Single-variable expansion no longer drops literal suffixes — `"$FOO bar"` with `FOO` unset yields `" bar"`, not `""`
- Struct decoding no longer silently truncates out-of-range integers (300 in an `int8` field became 44); overflow now returns an error, on all field widths and 32-bit platforms alike
- `StructInto` now resolves prefixed keys for untagged and multi-level tagged nested structs (previously dropped values that `MarshalStruct` emits)
- `Delete()` no longer unsets process env vars the loader never set (previously clobbered `HOME`, `TERM`, …) and now correctly unsets variables applied via `Set()` with `AutoApply`
- Registering a parser from inside a factory callback no longer self-deadlocks the parser registry (RWMutex reentrancy)

### Changed
- Single-key reads (`Lookup`, `GetSecure`) are lock-free; `Set` uses a read-lock fast path when `OverwriteExisting` is set and `AutoApply` is off
- Sentinel errors now match via `errors.Is`: `ErrInvalidKey`, `ErrForbiddenKey`, `ErrMaxVariables`, `ErrMissingRequired` (and `New()` wraps `ErrInvalidConfig`)
- `Auditor.Close()` disables logging before closing the handler, so post-`Close` `Log*` calls are no-ops
- BSDs/Solaris/Illumos and other non-mlock platforms now cross-compile correctly (build tags narrowed to linux/darwin with a no-op fallback elsewhere)
- Doc accuracy: `Lookup` returns stored values untrimmed (trimming is `.env` parse-time only); file loading is first-file-wins unless `OverwriteExisting=true`
- Test suite deduplicated and consolidated (table-driven merges); overall coverage 94.5% → 95.9%

### Performance
- Lock-free single-key reads and the `Set` fast path: `Loader_ConcurrentGet` -53%, `Loader_ConcurrentSet` -36%
- YAML lexer pass (exact-size token copies + token-slice pooling): `YAMLParser_Medium` -16.6% time / -57% bytes; `YAMLParser_Small` -15.4% / -57%
- `SecureValue` finalizer set once per object (`SecureValue_New` -53.1%); `SetAll` buckets into one flat allocation (`SecureMap_SetAll` -37.3%)
- pprof-guided pass (geomean -8.1% time / -3.7% allocs): pooled 64KB reader buffers, single-pass key homograph scan, lazy shard maps, allocation-free reserved-device-name checks

---

## v1.2.2 - Performance, Concurrency Hardening & Security (2026-08-12)

### Added
- `SecureValue.MarshalJSON()` / `MarshalText()` — redacting marshalers that prevent accidental secret exposure through `json.Marshal`, `encoding/xml`, `text/template`, and other reflection-based serializers (`String()` alone only covers `fmt`-style formatting)
- `secureMap.Has()` — zero-allocation key-existence check (no string copy, no SecureValue read lock)
- `secureMap.SetAllIfAbsent()` — batch insert that skips existing keys; used as a new fast path in file loading when `OverwriteExisting=false`
- `SecureReader` pooling via `sync.Pool` (`NewSecureReader` / `ReleaseSecureReader`) — eliminates per-Parse allocation of the reader struct
- `BufferedHandler.safeFlush()` — background flush goroutine now recovers panics from user-supplied handlers, reporting them via `OnError` instead of crashing
- `CloseableChannelHandler` unbuffered-channel support — `Log()` blocks until a receiver is ready or the handler closes, with deadlock-free `Close()` via `sync.Once`
- `validateRequiredKeys()` — shared required-key validation extracted from duplicated code in `.env` parser and structured (JSON/YAML) parsers
- Structured parser key-length validation — `MaxKeyLength` is now enforced for JSON/YAML keys, consistent with the `.env` parser
- Examples restructured into per-directory `main.go` packages (01–11), including new **09_error_handling**, **10_concurrency**, and **11_advanced** examples; added `config.json` and `config.yaml` to example data
- Expanded sensitive-key pattern list in `docs/SECURITY.md`: `PUBLIC_KEY`, `ENCRYPTION_KEY`, `ENCRYPT_KEY`, `DECRYPT_KEY`, `SIGNING_KEY`, `SIGN_KEY`, `VERIFY_KEY`, `SSN`, `SOCIAL_SECURITY`, `CREDIT_CARD`, `CARD_NUMBER`, `CVV`, `CVC`, `CCV`, `PAN`, `MNEMONIC`, `SEED`, `RECOVERY`, `WALLET`, `PRIVATE_ADDRESS`, `CONNECTION_STRING`, `CONN_STRING`, `DATABASE_URL`, `DB_PASSWORD`, `AWS_SECRET`, `AZURE_KEY`, `GCP_KEY`, `SERVICE_ACCOUNT`

### Changed
- `accessors.go` — all value-accessor methods (`GetString`, `GetInt`, `GetBool`, `GetDuration`, `GetFloat64`, `GetUint64`, `GetSecure`, `Lookup`, `Keys`, `All`, `Len`, `IsApplied`, `LoadTime`, `Config`, `GetSliceFrom`) extracted from `env.go` into a dedicated file; `env.go` now focuses on loader lifecycle
- `Lookup()` inlines a fast path for simple keys (no dots) to avoid function-value indirection through `ResolveKey`
- `Config.IsZero()` now correctly accounts for `JSONMaxDepth`, `YAMLMaxDepth`, `ValidateUTF8`, and `Prefix` fields — previously these were omitted from the zero-value check
- `Load()` no longer overwrites `cfg.Filenames` when called with no arguments, preserving `DefaultConfig()` defaults
- `loadFileLocked` uses `Has()` (no allocation) instead of `Get()` for overwrite-policy checks; audit logging is gated behind `AuditEnabled`
- `parseSliceElement` audit log in `GetSliceFrom` uses `Has` for existence checks where value is not needed
- `factory.go` — removed unused private adapter methods (`lineParserValidator/Auditor/Expander`); parser accesses factory fields directly
- `onStrictLockFailure` changed from a plain function variable to `atomic.Pointer[strictLockFailureHandler]` for race-free concurrent access from `tryLockMemory`
- `internal/errors.go` — `ErrFileTooLarge` / `ErrLineTooLong` use `errors.New` instead of `fmt.Errorf` (no format args; proper sentinel identity)
- `marshalToYAML` returns `(string, error)` instead of `([]byte, error)` to avoid a final allocation
- `escapeYAMLValue` uses byte-level iteration instead of rune-range for ASCII character checks
- Concurrent tests consolidated: five former single-scenario tests merged into one table-driven `TestLoader_Concurrent`
- `docs/CONCURRENCY_SAFETY.md` — updated shard-hash description and consolidated test documentation

### Fixed
- TOCTOU race in `ParseInto()` / `Validate()` — now hold the read lock across the entire operation instead of check-then-act, preventing a race where `Close()` runs between the `IsClosed` check and data access
- Deadlock in `ResetDefaultLoader()` — mutex is released before calling `Close()`, preventing deadlock if Close triggers code that needs the default loader
- `defaultMaskSensitive` off-by-one — `s[:maxLen]` would panic on inputs of length exactly `maxLen` (50); now `s[:maxLen-3]`
- `CloseableChannelHandler.Close()` potential deadlock — `close(done)` now runs via `sync.Once` before acquiring `closeMu`, ensuring blocked unbuffered sends are interrupted without deadlock
- `keysToUpper()` accesses `l.vars.Keys()` directly (caller already holds read lock) to avoid nested RLock

### Security
- `SecureValue` now implements `MarshalJSON` / `MarshalText` — secrets are masked in all common serialization paths, not just `fmt`-style formatting; closes the reflection-based serializer gap documented in the project guidelines
- `docs/SECURITY.md` clarifies that file paths must be relative (absolute, UNC, URL-encoded, and traversal paths are rejected)

### Performance
- **Audit-gated parse path** — `time.Now()` syscalls and audit log calls skipped entirely when `AuditEnabled=false`; affects `loadFilesInternal`, `loadFileLocked`, `Parse`, and `structuredParseResult`
- **secureMap lock reduction** — removed `sv.mu.RLock`/`RUnlock` from `Get()`, `GetSecure()`, and `ToMap()`; documented that `shard.mu.RLock` is sufficient because all writes occur under `shard.mu.Lock`. This was the single largest CPU hotspot (23.6% in atomic operations)
- **SecureReader pooling** — `sync.Pool` reuse eliminates per-Parse allocation; `defer ReleaseSecureReader()` added to parser
- **`SecureValue.Masked()`** — replaced `fmt.Sprintf` with manual `strconv.AppendInt` into a stack-allocated buffer, eliminating reflection and interface-boxing overhead
- **`containsIgnoreCase`** — folded non-ASCII detection into the comparison loop, halving bytes examined for ASCII strings (the common case)
- **New `SetAllIfAbsent` fast path** — when `Prefix=""` and `OverwriteExisting=false`, file loading avoids creating an intermediate filtered map and eliminates per-key allocations

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

- Go 1.25+ (updated from 1.24+ in v1.2.0)
- Zero external dependencies

---
