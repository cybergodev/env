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
| **Key Pattern Validation** | Only allows `^[A-Za-z][A-Za-z0-9_]*$` by default |
| **Value Content Validation** | Blocks null bytes and control characters |
| **Key Length Limits** | Default: 64 chars, Hard limit: 1024 chars |
| **Value Length Limits** | Default: 4096 chars, Hard limit: 1MB |

### File Security

| Protection | Description |
|------------|-------------|
| **File Size Limits** | Default: 2MB, Hard limit: 100MB |
| **Line Length Limits** | Default: 1024 chars, Hard limit: 64KB |
| **Variable Count Limits** | Default: 500, Hard limit: 10,000 |
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
PERL5OPT, PYTHONPATH, RUBYLIB, NODE_PATH
# Windows-specific injection
COMSPEC, PATHEXT, SYSTEMROOT, WINDIR
```

> Custom forbidden keys can be added via `cfg.ForbiddenKeys`; they are merged
> with the defaults above (the defaults cannot be removed).

### Memory Security

| Feature | Description |
|---------|-------------|
| **SecureValue** | Automatic memory zeroing on GC/cleanup |
| **Sensitive Data Masking** | Logs never expose passwords, tokens, keys |
| **Memory Pool Clearing** | Pooled objects are cleared before reuse |
| **Finalizer Cleanup** | GC triggers secure memory erasure |

### Variable Expansion Security

| Protection | Description |
|------------|-------------|
| **Depth Limiting** | Default: 5 levels, Hard limit: 20 levels |
| **Cycle Detection** | Prevents infinite expansion loops |
| **Key Validation** | Only valid keys are expanded |

### Concurrency Safety

- All operations are **thread-safe**
- Sharded storage design (8 shards) for reduced lock contention
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
sensitive keys** as `[MASKED:<len> chars]` (where `<len>` is the key length);
non-sensitive keys are logged verbatim. Optional fields (`file`, `masked`,
`details`, `duration_ns`) are omitted when empty.

```json
{
    "timestamp": "2026-03-11T10:30:00Z",
    "action": "set",
    "key": "[MASKED:7 chars]",
    "reason": "loaded (sensitive)",
    "success": true,
    "masked": true
}
```

> Note: The `AP***` (2-char prefix) style is used only in validator/path error
> messages (`DefaultMaskKey`), not in audit output. Sensitive keys loaded from
> files are audited with reason `"loaded (sensitive)"` on the fast path.

### Built-in Handlers

| Handler | Use Case |
|---------|----------|
| `JSONAuditHandler` | Structured logs, SIEM integration |
| `LogAuditHandler` | Standard Go log package |
| `ChannelAuditHandler` | Custom async processing |
| `NopAuditHandler` | Disabled auditing |

---

## Sensitive Data Detection

Keys containing these patterns (case-insensitive) are automatically masked:

```
# Authentication & Authorization
PASSWORD, SECRET, TOKEN, AUTH, CREDENTIAL, PASSPHRASE, SESSION, COOKIE

# API & Keys
API_KEY, APIKEY, ACCESS_KEY, SECRET_KEY, PRIVATE_KEY, PUBLIC_KEY

# Encryption & Security
PRIVATE, ENCRYPTION_KEY, ENCRYPT_KEY, DECRYPT_KEY, SIGNING_KEY, SIGN_KEY, VERIFY_KEY

# Financial & Personal (PII)
SSN, SOCIAL_SECURITY, CREDIT_CARD, CARD_NUMBER, CVV, CVC, CCV, PAN

# Crypto & Blockchain
MNEMONIC, SEED, RECOVERY, WALLET, PRIVATE_ADDRESS

# Database & Infrastructure
CONNECTION_STRING, CONN_STRING, DATABASE_URL, DB_PASSWORD

# Cloud & Services
AWS_SECRET, AZURE_KEY, GCP_KEY, SERVICE_ACCOUNT
```

Matching is substring-based and case-insensitive — a key like `MY_API_KEY`
matches because it contains `API_KEY`.

### Example

```go
// Key: DATABASE_PASSWORD
// In audit output (masked): [MASKED:17 chars]
// In error messages (DefaultMaskKey): DA***
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

---

## Security Checklist

| Item | Status |
|------|--------|
| Input validation on all keys and values | Implemented |
| File size and line length limits | Implemented |
| Forbidden keys blocking | Implemented |
| Memory zeroing for sensitive data | Implemented |
| Audit logging support | Implemented |
| Thread-safe operations | Implemented |
| Race condition prevention | Verified (100+ test runs) |
| Sensitive data masking in logs/errors | Implemented |
| Variable expansion cycle detection | Implemented |
| Hard limits to prevent DoS | Implemented |

---

## License

This project is licensed under the MIT License - see [LICENSE](../LICENSE) for details.
