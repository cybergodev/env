# Security Policy

[![Security](https://img.shields.io/badge/Security-Hardened-green.svg)]()
[![Audit](https://img.shields.io/badge/Audit-Enabled-blue.svg)]()

## Reporting a Vulnerability

**Do NOT report security vulnerabilities through public GitHub issues.**

Instead, please report them via:

- **Email**: cybergodev@gmail.com
- **GitHub Security Advisory**: [Report a vulnerability](https://github.com/cybergodev/env/security/advisories/new)

---

## Security Features

This library is designed for **high-security environments** with the following built-in protections:

### Input Validation

| Protection | Description |
|------------|-------------|
| **Key Pattern Validation** | `.env` parsing allows only `^[A-Za-z][A-Za-z0-9_]*$` by default; `Loader.Set()` accepts the wider structured-format key space (letters, digits, `_`, `@`, `-`, `.` — dotted keys are settable) unless a custom `KeyPattern` is configured |
| **Value Content Validation** | Blocks null bytes and control characters (tab, newline, and carriage return are permitted) |
| **Key Length Limits** | Default: 128 chars, Hard limit: 1024 chars |
| **Value Length Limits** | Default: 8192 chars, Hard limit: 1MB |

### File Security

| Protection | Description |
|------------|-------------|
| **File Size Limits** | Default: 8MB, Hard limit: 100MB |
| **Line Length Limits** | Default: 8192 chars, Hard limit: 64KB |
| **Variable Count Limits** | Default: 1000, Hard limit: 10,000 |
| **Nesting Depth Limits (JSON/YAML)** | Default: 16 levels, Hard limit: 100 levels |
| **Format Detection** | Automatic detection by extension |

### Forbidden Keys

The following system-critical keys are blocked by default:

```
# Dynamic linker / library preloading (Unix)
PATH, LD_PRELOAD, LD_PRELOAD_32, LD_PRELOAD_64,
LD_LIBRARY_PATH, LD_LIBRARY_PATH_32, LD_LIBRARY_PATH_64,
LD_DEBUG, LD_AUDIT, DYLD_INSERT_LIBRARIES, DYLD_LIBRARY_PATH
# Shell escape
IFS, SHELL, ENV, BASH_ENV
# Language-specific injection
PERL5OPT, PERL5LIB, PYTHONPATH, PYTHONSTARTUP, PYTHONINSPECT,
RUBYLIB, RUBYOPT, GEM_PATH, GEM_HOME, NODE_PATH, NODE_OPTIONS
# Runtime/VM code-execution option vectors
RUSTFLAGS, JAVA_TOOL_OPTIONS, JDK_JAVA_OPTIONS, _JAVA_OPTIONS,
CLASSPATH, PS4, SHELLOPTS, BASHOPTS
# Git config/proxy injection
GIT_CONFIG_COUNT, GIT_PROXY_COMMAND
# Windows-specific injection
COMSPEC, PATHEXT, SYSTEMROOT, WINDIR
```

> Custom forbidden keys can be added via `cfg.ForbiddenKeys`; they are merged
> with the defaults above (the defaults cannot be removed).

### Memory Security

| Feature | Description |
|---------|-------------|
| **SecureValue** | The value's internal byte slice is zeroed — including unused slice capacity — on Close/Release and by a finalizer |
| **Memory locking** | Optional (`SetMemoryLockEnabled`): mlock on Linux/macOS, VirtualLock on Windows |
| **Sensitive Data Masking** | Logs and errors never expose values; sensitive keys are masked |
| **Memory Pool Clearing** | Pooled objects are cleared before reuse |
| **Finalizer Cleanup** | GC triggers secure erasure of the internal byte slice |

**Protection scope — what memory locking does NOT cover (GEN-001 disclosure).**
The lock and zeroing protect only the `SecureValue`'s internal `[]byte`:

- Every read API that returns a Go string — `Reveal` on a SecureValue,
  `ToMap`, `GetSecure` (which copies through an intermediate string), and
  `Apply` — produces an immutable string copy that is
  neither locked nor zeroed; it survives until the GC reclaims it, and the
  GC does not erase memory. Minimize reads, and prefer `Bytes()` (a copy
  you can wipe yourself with `ClearBytes`) or short-lived scopes.
- Memory locking exists on Linux, macOS and Windows only. On other
  platforms (BSD, Solaris, AIX, Plan9, WASM) `LockMemory` returns
  `ErrMemoryLockNotSupported` and, outside strict mode, the failure is
  silent: values are usable and unlocked. Check `IsMemoryLocked()` on the
  value, or `IsMemoryLockSupported()` for platform support, if locking is a
  requirement.
- A window of unlocked memory exists between value creation and the lock
  call, and between unlock and zeroing on reset.
- mlock/VirtualLock do NOT prevent hibernation files or crash dumps from
  writing the memory to disk, and on Unix they require the process's
  locked-memory limit (`ulimit -l` / `CAP_IPC_LOCK`) to be raised for large
  working sets.
- Values that pass through variable expansion transit pooled buffers and
  `strings.Builder` scratch that cannot be zeroed; only their pool slots
  are cleared for reuse.

**Masking side channel.** `SecureValue.Masked()` embeds a length —
`[SECURE:<N> bytes]` — which leaks the length (and rotation) of the secret to
anyone who can read the logs. Key masks and value masks are fixed tokens
(`[MASKED]`) that reveal neither content nor length, per SEC-005. This is a
deliberate debugging affordance; treat masked logs accordingly when the
length of a SecureValue is itself sensitive.

### Variable Expansion Security

| Protection | Description |
|------------|-------------|
| **Depth Limiting** | Default: 5 levels, Hard limit: 20 levels |
| **Cycle Detection** | Prevents infinite expansion loops |
| **Key Validation** | Only valid keys are expanded |

### Expansion Scope

By default, `${VAR}` references resolve from the loaded file only
(`ExpansionFileOnly`). The process-environment fallback is **opt-in** and must
be requested explicitly — a configuration file otherwise cannot capture
unrelated process secrets (e.g. `${AWS_SECRET_ACCESS_KEY}`) into its values.

When configuration files come from a fully trusted source, restore the
traditional dotenv semantics by opting back into the process fallback:

```go
cfg := env.DefaultConfig()
cfg.ExpansionScope = env.ExpansionFileThenProcess
```

With the default `ExpansionFileOnly`, references that are not defined in the
file(s) being loaded expand to the empty string instead of reading
`os.Environ`.

### Concurrency Safety

- All operations are **thread-safe**
- Sharded storage design (32 shards) for reduced lock contention
- Atomic operations for fast-path checks
- 100+ race detection test runs pass

---

## Configuration

### Production-Ready Configuration

```go
cfg := env.ProductionConfig()
cfg.Filenames = []string{"app.env"}  // relative paths only; absolute paths are rejected
cfg.AuditEnabled = true
cfg.AuditHandler = env.NewJSONAuditHandler(auditFile)
cfg.RequiredKeys = []string{"DATABASE_URL", "API_KEY"}
```

> **Path Restriction**: For security, file paths must be relative — absolute
> paths (`/etc/app/.env`, `C:\app\.env`), UNC paths (`\\server\share`),
> URL-encoded paths (`%2e%2e`), and path traversal (`../`) are all rejected.
> Use a working directory or symlink strategy to point at files outside the
> current directory.

> **Warning**: Do NOT use `os.Stdout` with `JSONAuditHandler` in production.
> The handler's `Close()` method will close the underlying writer, which would
> close stdout. Use a file or custom writer instead. For testing, use
> `NewNopAuditHandler()` to avoid this issue.

### Security Settings

```go
cfg := env.DefaultConfig()

// Key validation
cfg.KeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)
cfg.ValidateValues = true

// Key restrictions
cfg.AllowedKeys = []string{"APP_PORT", "APP_HOST"}
cfg.ForbiddenKeys = []string{"PATH", "HOME"}

// Limits
cfg.MaxFileSize = 64 * 1024      // 64KB
cfg.MaxVariables = 50
cfg.MaxKeyLength = 32
cfg.MaxValueLength = 1024
```

---

## Secure Value Handling

### Basic Usage

```go
sv := env.GetSecure("API_KEY")
if sv != nil {
    // Safe for logging — Masked()/String() never expose the secret
    log.Println(sv.Masked()) // [SECURE:32 bytes]

    // Access the plaintext value (handle with care — never log or persist it)
    value := sv.Reveal()

    // Or get bytes (caller must clear)
    data := sv.Bytes()
    defer env.ClearBytes(data)

    // Cleanup when done
    sv.Close()
}
```

### Automatic Cleanup

```go
sv := env.NewSecureValue("secret")
// When sv is garbage collected, memory is automatically zeroed
// Or explicitly:
sv.Close() // Zero memory immediately
```

---

## Audit Logging

### Enable Auditing

```go
cfg := env.ProductionConfig()
cfg.AuditEnabled = true

// Use a file for audit output (recommended for production)
auditFile, err := os.Create("/var/log/app/audit.json")
if err != nil {
    log.Fatalf("failed to create audit file: %v", err)
}
defer auditFile.Close() // Ensure file is closed when done
cfg.AuditHandler = env.NewJSONAuditHandler(auditFile)

// Or use NopAuditHandler for testing
cfg.AuditHandler = env.NewNopAuditHandler()
```

### Audit Output Format

Each event is written as a single JSON line. The `key` field is **masked for
sensitive keys** as `[MASKED]`; non-sensitive keys are logged verbatim.
Optional fields (`file`, `masked`, `details`, `duration_ns`) are omitted when
empty.

```json
{
    "timestamp": "2026-03-11T10:30:00Z",
    "action": "set",
    "key": "[MASKED]",
    "reason": "loaded",
    "success": true,
    "masked": true
}
```

> Note: Sensitive keys loaded from files use the same reason `"loaded"` as
> other keys; sensitivity is reflected by the masked `key` field and
> `"masked": true`.

### Built-in Handlers

| Handler | Use Case |
|---------|----------|
| `JSONAuditHandler` | Structured logs, SIEM integration |
| `LogAuditHandler` | Standard Go log package |
| `ChannelAuditHandler` | Custom async processing (caller-owned channel) |
| `CloseableChannelHandler` | Async processing with owned channel lifecycle (`Close()` closes the channel) |
| `NopAuditHandler` | Disabled auditing |

### Audit Failure Semantics

**Default: fail-open.** Audit logging is best-effort — a handler error (full
disk, closed writer), a handler panic, or an event dropped because a channel
buffer is full never fails the configuration operation that emitted the
event. Operation results are independent of the audit pipeline.

Observability for lost events:

- `CloseableChannelHandler.DroppedEvents()` counts every event the handler
  discarded (full buffer with no consumer, or sends after `Close()`).
- `Auditor.Failures()` on the built-in auditor counts every event that never
  reached its handler.

**Opt-in: fail-closed.** Set `Config.AuditFailClosed` (with `AuditEnabled`)
to make a lost event fail the operation that emitted it: `LoadFiles`,
`Apply`, `Set` and `Delete` then return an error matching
`env.ErrAuditFailed` instead of succeeding silently. The built-in auditor
latches the first failure and reports it at the end of the operation; a
`CustomAuditor` participates by implementing `TakeFailure() error` (detected
via interface assertion, so the `AuditLogger` interface itself is unchanged).
With concurrent operations the latched failure is delivered to whichever
operation checks first — the signaling is best-effort, not exactly-once.

Once an auditor is closed it stays disabled: `SetEnabled(true)` on a closed
auditor is a no-op (its handler's resources may already be released).

---

## Sensitive Data Detection

Keys containing these patterns (case-insensitive) are automatically masked:

```
# Authentication & Authorization
PASSWORD, PASSWD, SECRET, TOKEN, AUTH, CREDENTIAL, PASSPHRASE, SESSION, COOKIE

# API & Keys
API_KEY, APIKEY, ACCESS_KEY, SECRET_KEY, PRIVATE_KEY, PUBLIC_KEY, KEYSTORE

# Encryption & Security
PRIVATE, ENCRYPTION_KEY, ENCRYPT_KEY, DECRYPT_KEY, SIGNING_KEY, SIGN_KEY, VERIFY_KEY, CERT, SALT, NONCE

# Financial & Personal (PII)
SSN, SOCIAL_SECURITY, CREDIT_CARD, CARD_NUMBER, CVV, CVC, CCV, PAN

# Crypto & Blockchain
MNEMONIC, SEED, RECOVERY, WALLET, PRIVATE_ADDRESS

# Database & Infrastructure
CONNECTION_STRING, CONN_STRING, DATABASE_URL, DB_PASSWORD, DSN

# Password/PII spellings
PWD, USERNAME

# Cloud & Services
AWS_SECRET, AZURE_KEY, GCP_KEY, SERVICE_ACCOUNT
```

Matching is case-insensitive. Most patterns match as substrings — a key like
`MY_API_KEY` matches because it contains `API_KEY`. Four short tokens are
matched only at identifier boundaries to avoid over-masking ordinary words
(`AUTH`, `PAN`, `PWD`, `USERNAME` — so `AUTHOR`, `COMPANY` and `MONKEY` are
not flagged, while `DB_AUTH` and `MYSQL_PWD` still are).

### Example

```go
// Key: DATABASE_PASSWORD
// In audit output (masked): [MASKED]
// In error messages (DefaultMaskKey): [MASKED]
```

---

## Security Best Practices

### Do

- Use `ProductionConfig()` in production environments
- Enable audit logging for compliance
- Use `SecureValue` for sensitive data
- Set `RequiredKeys` to fail fast on missing configuration
- Use `FailOnMissingFile: true` in production

### Don't

- Never log raw environment values
- Never store `SecureValue` pointers longer than needed
- Never disable `ValidateValues` in production
- Never allow untrusted input as file paths
- Avoid `Overload` / `OverwriteExisting = true` when environment variables injected by the deployment platform (container secrets, CI variables) must keep precedence over file values — `Overload` replaces existing process variables, `Load` never does

---

## Security Checklist

| Item | Status |
|------|--------|
| Input validation on all keys and values | Implemented |
| File size and line length limits | Implemented (all formats; a minified single-line JSON document must fit `MaxLineLength`, hard-capped at 64 KB) |
| Forbidden keys blocking | Implemented |
| Memory zeroing for sensitive data | Implemented for SecureValue's internal buffer (string copies from read APIs are NOT zeroable — see "Protection scope" above) |
| Audit logging support | Implemented |
| Thread-safe operations | Implemented |
| Race condition prevention | Verified (100+ test runs) |
| Sensitive data masking in logs/errors | Implemented |
| Variable expansion cycle detection | Implemented |
| Hard limits to prevent DoS | Implemented |
| Flat-key collision rejection (nondeterministic resolution removed) | Implemented (always on, `ErrDuplicateKey`) |
| Strict duplicate-key mode (`DuplicateKeyError`) | Implemented (opt-in) |

---

## License

This project is licensed under the MIT License - see [LICENSE](../LICENSE) for details.
