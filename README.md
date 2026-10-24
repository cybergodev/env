# Env - High-Performance Go Environment Variable Library

[![Go Reference](https://pkg.go.dev/badge/github.com/cybergodev/env.svg)](https://pkg.go.dev/github.com/cybergodev/env)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Security Policy](https://img.shields.io/badge/security-policy-blue.svg)](docs/SECURITY.md)
[![Thread Safe](https://img.shields.io/badge/thread%20safe-%E2%9C%93-brightgreen.svg)](docs/CONCURRENCY_SAFETY.md)

**[中文文档](README_zh-CN.md)** | **[www.cybergo.dev/env](https://www.cybergo.dev/env/)**

---

## 📋 Overview

**Env** is a production-ready, zero-dependency, thread-safe Go library for environment variable management. It focuses on **security**, **concurrency**, and **developer experience**.

### ✨ Key Features

| Feature | Description |
|:--------|:------------|
| 🚀 **One-Line Setup** | `env.Load(".env")` loads and applies to `os.Environ` |
| 🔒 **Type Safety** | `GetString`, `GetInt`, `GetBool`, `GetDuration`, `GetSlice[T]` |
| 📁 **Multi-Format** | Auto-detect `.env`, `.json`, `.yaml` files |
| ⚡ **Thread Safety** | Sharded storage (32 shards) + RWMutex for high concurrency |
| 🛡️ **Secure Memory** | `SecureValue` auto-zeroes sensitive data with memory pooling |
| 🔄 **Variable Expansion** | Full `${VAR}` syntax with default values |
| 📝 **Audit Logging** | Built-in JSON/Log/Channel handlers for compliance |
| 🧪 **Testing Ready** | Isolated loaders for test isolation |
| 📦 **Zero Dependencies** | Standard library only |

---

## 📦 Installation

```bash
go get github.com/cybergodev/env
```

**Requirements:** Go 1.25+

---

## 🚀 Quick Start

### Step 1: Create a `.env` file

```env
# Application
APP_NAME=myapp
APP_PORT=8080
DEBUG=true

# Database
DB_HOST=localhost
DB_PORT=5432
DB_PASSWORD=secret123

# Timeouts
TIMEOUT=30s
```

### Step 2: Use in Go code

```go
package main

import (
    "fmt"
    "log"
    "time"

    "github.com/cybergodev/env"
)

func main() {
    // One-line initialization - loads and applies to os.Environ
    if err := env.Load(".env"); err != nil {
        log.Fatalf("Failed to load: %v", err)
    }

    // Type-safe access with defaults
    port    := env.GetInt("APP_PORT", 8080)
    debug   := env.GetBool("DEBUG", false)
    timeout := env.GetDuration("TIMEOUT", 30*time.Second)

    fmt.Printf("Server: %s:%d\n", env.GetString("APP_NAME", "unknown"), port)
    fmt.Printf("Debug: %v, Timeout: %v\n", debug, timeout)
}
```

---

## 📚 Usage Guide

### Basic Operations

```go
// Load multiple files (with the default config the first value wins;
// use Overload below to let later files override)
if err := env.Load(".env", "config.json", ".env.local"); err != nil {
    log.Fatal(err)
}

// Or: later files override earlier ones AND existing process env vars
// (godotenv Overload convention — prefer Load when deployment-injected
// variables must keep precedence over file values)
if err := env.Overload("config.json", "config.yaml"); err != nil {
    log.Fatal(err)
}

// Check existence
value, exists := env.Lookup("KEY")
if !exists {
    // Handle missing key
}

// CRUD operations
if err := env.Set("KEY", "value"); err != nil { /* handle */ }
if err := env.Delete("KEY"); err != nil { /* handle */ }
keys  := env.Keys()               // Get all keys
all   := env.All()                // Get all variables as map
count := env.Len()                // Variable count
```

> **Note:** Package-level functions (`GetString`, `Set`, `Lookup`, ...) require `Load()`,
> `Overload()` or `LoadWithConfig()` to be called first. Before initialization, `Get*`/`Lookup`/
> `Keys`/`All`/`Len`/`GetSecure` silently return defaults/zero values, while `Set`,
> `Delete`, `Validate`, and `ParseInto` return `ErrNotInitialized`.
> `Load()` initializes the default loader exactly once (the three initializers share one
> default loader); a second call returns
> `ErrAlreadyInitialized`. To reinitialize (e.g., between tests), call `ResetDefaultLoader()` first.

### Duplicate Keys & Merge Order

**Across files** (any mix of `.env`/`.json`/`.yaml` — nested keys flatten into one
uppercased key space, so `DATABASE_HOST` in `.env` and `database: {host: ...}` in
YAML are the same key): files load in list order. With the default
(`OverwriteExisting = false`) the **first file wins** and later duplicates are
skipped; with `true` later files override. The package-level `Overload`
initializer is the shorthand for that setting (godotenv convention) — note
that it also replaces variables already present in the process environment,
which `Load` never does.

**Within one file**, the default resolution is format-specific:

| Format | Duplicate `KEY` resolves to |
|--------|------------------------------|
| `.env` | first occurrence when `OverwriteExisting=false` (default), last when `true` |
| YAML | last occurrence (regardless of `OverwriteExisting`) |
| JSON | last occurrence — `encoding/json` decode order (regardless of `OverwriteExisting`) |

Set a `DuplicateKeyPolicy` to make this uniform or strict:

```go
cfg := env.DefaultConfig()
cfg.DuplicateKeyPolicy = env.DuplicateKeyError // reject duplicate keys with env.ErrDuplicateKey
// also available: DuplicateKeyFirstWins, DuplicateKeyLastWins, DuplicateKeyInherit (default)
```

Notes:

- `DuplicateKeyFirstWins` applies to `.env` and YAML. For JSON data (including
  inline JSON in YAML) a document that actually contains duplicate keys is
  rejected — `encoding/json` discards earlier occurrences, so the policy cannot
  be honored for it — while JSON documents without duplicates load unchanged.
  Under `DuplicateKeyError` (and for the FirstWins check), JSON duplicates are
  found by a conservative pre-scan that compares raw key bytes; escape-equivalent
  spellings of the same key are not detected.
- **Flat-key collisions always error** (any policy): two different source paths
  that flatten to the same key (`a_b: 1` next to `a: {b: 2}`, a literal `a_0`
  next to an array `a`, or keys differing only in case) previously resolved
  nondeterministically by map iteration order; they now fail with
  `env.ErrDuplicateKey`.
- **Case:** YAML/JSON keys are uppercased when flattened; `.env` keys are stored
  as written, so keys differing only in case do not collide across formats.
  Lookups try the exact key first, then its uppercase form (see `Lookup`).

### Type Access

```go
// String (with default)
name := env.GetString("APP_NAME", "default-app")

// Integer (returns int64)
port := env.GetInt("PORT", 8080)

// Boolean - supports: true/1/yes/on/enabled, false/0/no/off/disabled
debug := env.GetBool("DEBUG", false)

// Duration
timeout := env.GetDuration("TIMEOUT", 30*time.Second)

// Float (returns float64)
ratio := env.GetFloat64("RATIO", 0.5)

// Unsigned integer (returns uint64)
maxSize := env.GetUint64("MAX_SIZE", 1024)

// Generic slice: string, int, int64, uint, uint64, bool, float64, time.Duration
hosts := env.GetSlice[string]("HOSTS", []string{"localhost"})
ports := env.GetSlice[int]("PORTS", []int{8080})
```

### Flexible Key Lookup

All `Get*` methods support **case-insensitive** and **dot-notation** key resolution, so you can access values in the style that best fits your code:

```go
// Given .env: DEEPSEEK_KEY=sk-abc123

// Case-insensitive lookup (uppercase fallback)
env.GetString("DEEPSEEK_KEY")   // "sk-abc123" — exact match
env.GetString("deepseek_key")   // "sk-abc123" — uppercase fallback
env.GetString("DeepSeek_Key")   // "sk-abc123" — uppercase fallback

// Dot notation (dot → underscore, auto-uppercased)
env.GetString("deepseek.key")   // "sk-abc123" — resolves to DEEPSEEK_KEY
env.GetString("db.host")        // resolves to DB_HOST

// Mixed underscores and dots
env.GetString("test_app.key")   // resolves to TEST_APP_KEY

// Works with all Get* methods
env.GetInt("app.port")          // resolves to APP_PORT
env.GetBool("debug.mode")       // resolves to DEBUG_MODE

// Indexed access into comma-separated values (virtual lookup)
// Given .env: LIST=a,b,c
env.GetString("LIST.0")         // "a" — first element
env.GetString("list.2")         // "c" — third element (case-insensitive base)
```

**Resolution strategy:**

| Step | Simple key (no dots) | Dot-notation key |
|:-----|:---------------------|:-----------------|
| 1 | Exact match | Convert dots to underscores, uppercase → lookup |
| 2 | Uppercase fallback | (done in step 1) |
| 3 | — | If key ends in a numeric index: resolve base key, split comma-separated value at that index |

**Best practice:** Use `UPPER_SNAKE_CASE` in `.env` files. This ensures all lookup styles work correctly.

### Struct Mapping

```go
type Config struct {
    Port    int           `env:"PORT,envDefault:8080"` // inline default
    Debug   bool          `env:"DEBUG" envDefault:"false"` // separate tag — both syntaxes work
    Timeout time.Duration `env:"TIMEOUT"`
    Origins []string      `env:"CORS_ORIGINS"`
}

var cfg Config
if err := env.Load(".env"); err != nil {
    log.Fatal(err)
}
if err := env.ParseInto(&cfg); err != nil {
    log.Fatal(err)
}
```

### Loader API (Fine-grained Control)

```go
cfg := env.ProductionConfig()
cfg.Filenames = []string{"app.env"} // relative paths only; absolute paths are rejected

loader, err := env.New(cfg)
if err != nil {
    log.Fatal(err)
}
defer loader.Close()

// Load additional files
if err := loader.LoadFiles("override.env"); err != nil {
    log.Fatal(err)
}

// Apply to os.Environ
if err := loader.Apply(); err != nil {
    log.Fatal(err)
}

// Access values
port := loader.GetInt("PORT", 8080)
```

---

## 📁 Multi-Format Support

### .env Files

```env
# Comments start with #
DATABASE_URL=postgres://localhost:5432/db
PORT=8080
DEBUG=true

# Quotes are optional
MESSAGE="Hello World"
SINGLE='Single quotes work too'

# Variable expansion
HOST=localhost
URL=${HOST}:${PORT:-443}
```

### JSON (Auto-flattened)

```json
{
    "database": { "host": "localhost", "port": 5432 },
    "ports": [8080, 8081]
}
```

**Access:**
```go
env.GetString("database.host")    // "localhost" (dot notation)
env.GetInt("database.port")       // 5432
env.GetSlice[int]("ports")        // [8080, 8081]
// Also works: DATABASE_HOST, DATABASE_PORT
```

### YAML (Auto-flattened)

```yaml
database:
  host: localhost
  port: 5432
ports: [8080, 8081]
```

**Access:** Same as JSON - use dot notation or uppercase underscore format.

> **Values containing `": "` must be quoted.** A plain (unquoted) YAML value
> may not contain a colon followed by whitespace — YAML reads it as a nested
> key. Such a line is now rejected with a `YAMLError` instead of being split
> into two entries. Quote the value (`'...'` or `"..."`) to keep it literal:
>
> ```yaml
> # REJECTED: "Expires: ..." would be read as a second key
> apnews.com: sid=1; Expires: Wed, 21 Oct 2015 07:28:00 GMT
>
> # OK
> apnews.com: 'sid=1; Expires: Wed, 21 Oct 2015 07:28:00 GMT'
> ```
>
> Inline JSON/flow regions (`{...}`, `[...]`) are exempt: `": "` inside them
> is data, so `g_state={"i_l": 0}` and `{"a": "b: c"}` load as written.
> Empty keys, bare scalar lines, and text left after a closing quote are
> likewise rejected rather than silently dropped.
> Values starting with `{` or `[` that form a complete flow collection
> flatten into prefixed/indexed keys (`k: {"a": 1}` → `K_A`); quote them to
> keep the text as one string.

---

## 🔄 Serialization / Deserialization

```go
// Map to format string
data := map[string]string{"PORT": "8080", "DEBUG": "true"}

envString, _  := env.Marshal(data)                      // .env (default)
jsonString, _ := env.Marshal(data, env.FormatJSON)      // JSON
yamlString, _ := env.Marshal(data, env.FormatYAML)      // YAML

// Parse string to Map
m, _ := env.UnmarshalMap("PORT=8080\nDEBUG=true")           // .env format
m, _ := env.UnmarshalMap(`{"port": 8080}`, env.FormatJSON)  // JSON
m, _ := env.UnmarshalMap(yamlString, env.FormatAuto)        // Auto-detect

// Struct <-> Map conversion
m, _ := env.MarshalStruct(&config)          // Struct to map
env.UnmarshalInto(m, &config)               // Map to struct

// String directly to struct
env.UnmarshalStruct("PORT=8080\nDEBUG=true", &config, env.FormatEnv)
```

> **Marshal rejects keys the loader would reject.** Every emitted document
> must be loadable by this library, so `Marshal` fails with `*MarshalError`
> on a key its own reader refuses: `.env` output requires the `.env` key
> grammar (`^[A-Za-z][A-Za-z0-9_]*$`), YAML/JSON output the flattened key
> space (letters, digits, `_`, `@`, `-`, `.`). A store can legitimately hold
> wider keys — `Loader.Set` accepts dotted names and YAML loads store literal
> keys like `APNEWS.COM` — so pick a format whose reader accepts them, or
> rename the key. Use `errors.Is(err, env.ErrInvalidKey)`-style checks via
> `env.IsMarshalError(err)`.

---

## 🔄 Variable Expansion

`.env` files fully support `${VAR}` syntax:

```env
HOST=localhost
PORT=8080

# Variable reference
URL=${HOST}:${PORT}                    # → "localhost:8080"

# Default value if unset (empty string is preserved)
TIMEOUT=${TIMEOUT:-30s}

# Assign default if unset — the assigned value becomes a real variable,
# visible to later references in the same file and to Get/Apply
NAME=${NAME:=default_value}

# Required variable — error if unset or empty
API_KEY=${API_KEY:?API_KEY must be set}

# Combined expansion
FULL_URL=https://${HOST}:${PORT:-443}
```

Two deliberate divergences from POSIX shell behavior:

- **`${EMPTY:-default}` keeps the empty string** when `EMPTY` is set to "".
  The `:-` default fires only when the variable is UNSET (an empty string is
  a valid explicit value), unlike POSIX where `:-` fires on empty too.
- **`${}` expands to the literal `{}`** rather than erroring with bash's
  "bad substitution".

`:=` assignment semantics: during a file load the expanded default is
assigned to the variable — it becomes visible to references in keys that
follow in the pass (keys are processed in sorted order, so the outcome is
deterministic) and it materializes in the loaded map like any parsed
variable. A variable that is already set (in the file or in the process
environment) is never reassigned.

---

## 🔒 Secure Value Handling

Use `SecureValue` for sensitive data like passwords, API keys, and tokens:

```go
// Get SecureValue
sv := env.GetSecure("API_KEY")
if sv != nil {
    defer sv.Release()

    // Safe logging (String() returns masked representation)
    // Output format: [SECURE:<N> bytes]
    // When memory locking is enabled, a status suffix is appended:
    //   [SECURE:32 bytes locked] / [SECURE:32 bytes unlocked] / [SECURE:32 bytes lock-failed]
    fmt.Println(sv)                // [SECURE:32 bytes]
    fmt.Println(sv.Masked())       // [SECURE:32 bytes]

    // Access actual value (use with caution!)
    value := sv.Reveal()

    // Get bytes (caller must clean up)
    data := sv.Bytes()
    defer env.ClearBytes(data)     // Manual zeroing
}

// Create SecureValue directly
secret := env.NewSecureValue("super_secret")
defer secret.Release()

// Create with strict error checking
secret, err := env.NewSecureValueStrict("super_secret")
if err != nil {
    log.Fatal("Memory lock failed:", err)
}
defer secret.Release()
```

### SecureValue Methods

| Method | Description |
|:-------|:------------|
| `String()` | Get masked representation (safe for `fmt.Printf`, logging) |
| `Reveal()` | Get plaintext value (**use with caution!**) |
| `Bytes()` | Get byte slice copy (caller must clean up) |
| `Length()` | Get value length without exposing it |
| `Masked()` | Get masked representation for logging |
| `Close()` | Zero memory, don't return to pool |
| `Release()` | Zero memory and return to pool |
| `IsClosed()` | Check if closed |
| `IsMemoryLocked()` | Check if memory is protected from swap |
| `MemoryLockError()` | Get error from memory locking attempt |

---

## 📝 Audit Logging

```go
auditFile, err := os.Create("audit.log")
if err != nil {
    log.Fatal(err)
}
defer auditFile.Close()

cfg := env.ProductionConfig()
cfg.AuditEnabled = true
// Use a file, not os.Stdout: loader.Close() closes the handler, which closes the writer
cfg.AuditHandler = env.NewJSONAuditHandler(auditFile)

loader, err := env.New(cfg)
if err != nil {
    log.Fatal(err)
}
defer loader.Close()

if err := loader.LoadFiles("app.env"); err != nil {
    log.Fatal(err)
}
// Output: {"timestamp":"...","action":"parse","reason":"parsed: app.env","success":true,"duration_ns":150000}
//         {"timestamp":"...","action":"set","key":"[MASKED]","reason":"loaded","success":true,"masked":true}
//
// Sensitive keys (e.g. API_KEY) are masked as [MASKED] in audit output.
```

**Built-in Handlers:**

```go
env.NewJSONAuditHandler(w)              // JSON format → io.Writer
env.NewLogAuditHandler(logger)          // Standard log.Logger
env.NewChannelAuditHandler(ch)          // Channel (external processing)
env.NewCloseableChannelHandler(size)    // Owned buffered channel with lifecycle
env.NewNopAuditHandler()                // No-op (discard)
```

---

## 🧪 Testing Support

```go
func TestConfig(t *testing.T) {
    // Create isolated loader (doesn't affect global state)
    cfg := env.TestingConfig()
    cfg.Filenames = []string{".env.test"}

    loader, err := env.New(cfg)
    if err != nil {
        t.Fatal(err)
    }
    defer loader.Close()

    port := loader.GetInt("PORT", 8080)
    // Test your code...
}

// Reset default loader between tests (required if Load() was already called)
func TestMain(m *testing.M) {
    if err := env.ResetDefaultLoader(); err != nil {
        log.Printf("reset default loader: %v", err)
    }
    os.Exit(m.Run())
}
```

---

## 🛠️ Utility Functions

```go
// Sensitive key detection
env.IsSensitiveKey("API_SECRET")  // true
env.IsSensitiveKey("HOST")        // false

// Value masking — sensitive keys become "[MASKED]";
// non-sensitive values ≤ 20 chars pass through, longer ones are truncated + "..."
env.MaskValue("API_KEY", "secret123")  // "[MASKED]"

// Key masking for logging — sensitive keys become "[MASKED]" (fixed token)
env.MaskKey("DB_PASSWORD")  // "[MASKED]"

// String sanitization
safe := env.SanitizeForLog(userInput)

// Mask sensitive content in string
masked := env.MaskSensitiveInString(logMessage)

// Format detection
env.DetectFormat("config.yaml")  // FormatYAML (String() → "yaml")
```

### Sensitive Key Patterns

Automatically detected (case-insensitive):

```
password, secret, token, credential, passphrase,
api_key, apikey, access_key, secret_key, private_key,
private, auth, session, cookie,
encryption_key, signing_key, connection_string,
database_url, db_password, ssn, credit_card,
mnemonic, seed, recovery, wallet, service_account
```

> **Note:** This is a representative subset. The full list contains 50 patterns.
> Matching is case-insensitive; most patterns match as substrings (a key like
> `MY_API_KEY` matches because it contains `API_KEY`), while four short tokens
> (`AUTH`, `PAN`, `PWD`, `USERNAME`) match only at identifier boundaries, so
> keys like `AUTHOR` or `EXPAND` are not flagged.

---

## ⚙️ Configuration

### Preset Configurations

```go
env.DefaultConfig()     // Safe defaults
env.DevelopmentConfig() // Relaxed limits + allow override
env.TestingConfig()     // Tight config + isolated testing
env.ProductionConfig()  // Strict security + audit
```

### Configuration Comparison

| Setting | Default | Development | Testing | Production |
|---------|---------|-------------|---------|------------|
| `FailOnMissingFile` | false | false | false | **true** |
| `OverwriteExisting` | false | **true** | **true** | false |
| `ValidateValues` | **true** | **true** | **true** | **true** |
| `AuditEnabled` | false | false | false | **true** |
| `MaxFileSize` | 8 MB | 10 MB | 64 KB | 64 KB |
| `MaxVariables` | 1,000 | 1,000 | 50 | 50 |

### Full Configuration Options

```go
cfg := env.DefaultConfig()

// === File Handling (FileConfig) ===
cfg.Filenames         = []string{".env"}
cfg.FailOnMissingFile = false
cfg.OverwriteExisting = true
cfg.AutoApply         = true

// === Validation (ValidationConfig) ===
cfg.RequiredKeys   = []string{"DB_URL"}
cfg.AllowedKeys    = []string{"PORT", "DEBUG"}  // Empty = allow all
cfg.ForbiddenKeys  = []string{"PATH"}           // Block dangerous keys
cfg.ValidateValues = true
cfg.ValidateUTF8   = true
cfg.KeyPattern     = nil                  // nil = DefaultKeyPattern

// === Security Limits (LimitsConfig) ===
cfg.MaxFileSize       = 8 << 20   // 8 MB
cfg.MaxVariables      = 1000
cfg.MaxLineLength     = 8192
cfg.MaxKeyLength      = 128
cfg.MaxValueLength    = 8192
cfg.MaxExpansionDepth = 5

// === Parsing (ParsingConfig) ===
cfg.AllowExportPrefix  = true                     // Allow "export KEY=value"
cfg.AllowYamlSyntax    = false                    // YAML-style values in .env
cfg.ExpandVariables    = true                     // Expand ${VAR} (.env only; JSON/YAML never expand)
cfg.DuplicateKeyPolicy = env.DuplicateKeyInherit  // Inherit / FirstWins / LastWins / Error
cfg.ExpansionScope     = env.ExpansionFileOnly    // ${VAR} resolves from file variables only (SEC-03)

// === JSON Options (JSONConfig) ===
cfg.JSONNullAsEmpty    = true
cfg.JSONNumberAsString = true
// cfg.JSONBoolAsString — deprecated, no effect: JSON bools always render
// "true"/"false" canonically. Left false (the default).
cfg.JSONMaxDepth       = 16

// === YAML Options (YAMLConfig) ===
cfg.YAMLNullAsEmpty    = true
cfg.YAMLNumberAsString = true
cfg.YAMLBoolAsString   = true
cfg.YAMLMaxDepth       = 16

// === Advanced (ComponentConfig) ===
cfg.Prefix          = "APP_"  // Only load keys with prefix
cfg.FileSystem      = nil     // nil = OS filesystem
cfg.CustomValidator = nil     // Replace built-in key/value validator
cfg.CustomExpander  = nil     // Replace built-in variable expander
cfg.CustomAuditor   = nil     // Replace built-in audit logger

// === Audit Logging (ComponentConfig) ===
cfg.AuditEnabled = true
cfg.AuditHandler = env.NewJSONAuditHandler(os.Stdout) // demo only; prefer a file — Close() closes the writer
```

> **Note:** Config uses nested structs (`FileConfig`, `ValidationConfig`, `LimitsConfig`,
> `JSONConfig`, `YAMLConfig`, `ParsingConfig`, `ComponentConfig`) with field promotion.
> You can access fields directly (`cfg.Filenames`) or explicitly (`cfg.FileConfig.Filenames`).

> **Configuration style (API contract):** the `Config` struct is the single
> configuration channel — the library has no functional options, and every
> field has an explicit default in `DefaultConfig()`. Zero-value semantics:
> a fully zero `Config{}` is treated as `DefaultConfig()` by `New`; a
> partially-set Config is *not* auto-completed (unset limits fail `Validate`
> with `ErrInvalidConfig`) — always start from `DefaultConfig()` or a preset.
> A few process-wide knobs are deliberately not Config fields:
> `SetMemoryLockEnabled` / `SetMemoryLockStrict`, `RegisterParser` /
> `ForceRegisterParser`, and the `DefaultFileSystem` variable — set them at
> program init.

### Default Limits

| Setting | Default | Hard Limit |
|---------|---------|------------|
| MaxFileSize | 8 MB | 100 MB |
| MaxLineLength | 8,192 chars | 64 KB |
| MaxKeyLength | 128 chars | 1,024 chars |
| MaxValueLength | 8,192 chars | 1 MB |
| MaxVariables | 1,000 | 10,000 |
| MaxExpansionDepth | 5 | 20 |
| JSONMaxDepth / YAMLMaxDepth | 16 | 100 |

> **Line-length scope:** `MaxLineLength` applies to ALL formats, including JSON
> and YAML. A minified (single-line) JSON document is one line for this purpose
> and must fit within the limit — its hard cap is 64 KB, so a larger minified
> document cannot be loaded by raising the config alone; pretty-print it instead.
> The `.env` parser enforces `MaxVariables` mid-parse; JSON/YAML check the
> flattened key count after parsing (memory during parsing is bounded by the
> YAML token / JSON node caps instead).

---

## ⚠️ Error Handling

The library exposes sentinel errors for `errors.Is` and structured error types for `errors.As`:

```go
if err := env.Load(".env"); err != nil {
    switch {
    case errors.Is(err, env.ErrFileNotFound):
        // file missing — provide a fallback or fail fast
    case errors.Is(err, env.ErrFileTooLarge):
        // exceeds cfg.MaxFileSize
    case errors.Is(err, env.ErrSecurityViolation):
        // forbidden key or security policy violation
    }
}

var parseErr *env.ParseError
if errors.As(err, &parseErr) {
    fmt.Printf("%s:%d: %v\n", parseErr.File, parseErr.Line, parseErr.Err)
}
```

**Sentinel errors:** `ErrFileNotFound`, `ErrFileTooLarge`, `ErrLineTooLong`, `ErrInvalidKey`,
`ErrForbiddenKey`, `ErrSecurityViolation`, `ErrInvalidValue`, `ErrMissingRequired`,
`ErrMaxVariables`, `ErrExpansionDepth`, `ErrClosed`, `ErrInvalidConfig`,
`ErrNotInitialized`, `ErrAlreadyInitialized`, `ErrDuplicateKey`, `ErrAuditFailed`

**Structured error types:** `ParseError` (file/line), `ValidationError`, `SecurityError`,
`FileError`, `ExpansionError`, `JSONError`, `YAMLError`, `MarshalError` — check with
`env.IsMarshalError(err)` where applicable.

See [examples/09_error_handling](examples/09_error_handling/) for a runnable demo.

---

## 📖 API Reference

The two layers mirror each other: every package-level function below is a
thin proxy for the same-named `Loader` method with the same signature
(`env.GetString` ↔ `loader.GetString`, `env.Set` ↔ `loader.Set`, …). The
exceptions: `Load` / `Overload` / `LoadWithConfig` initialize the shared
default loader (instance counterpart: `New`); `GetSlice` pairs with
`GetSliceFrom` (Go methods cannot take type parameters); and the stateless
parse / marshal / utility / memory-lock / parser-registry families have no
instance counterpart by design.

### Package Functions

| Function | Description |
|:---------|:------------|
| `Load(files...)` | Load files and apply to `os.Environ` (one-time; see note above) |
| `Overload(files...)` | Like `Load`, but later files override earlier ones and existing process env vars |
| `LoadWithConfig(cfg)` | Load with custom config (forces `AutoApply = true`) |
| `GetString(key, def...)` | Get string value |
| `GetInt(key, def...)` | Get `int64` value |
| `GetBool(key, def...)` | Get boolean value |
| `GetDuration(key, def...)` | Get duration value |
| `GetFloat64(key, def...)` | Get `float64` value |
| `GetUint64(key, def...)` | Get `uint64` value |
| `GetSlice[T](key, def...)` | Get generic slice |
| `GetSliceFrom[T](loader, key, def...)` | Get slice from specific loader |
| `Lookup(key)` | Get value + existence check |
| `Set(key, value)` | Set value (returns error) |
| `Delete(key)` | Delete key (returns error) |
| `Keys()` | Get all keys |
| `All()` | Get all variables as map |
| `Len()` | Get variable count |
| `GetSecure(key)` | Get `SecureValue` for sensitive data |
| `Validate()` | Validate required keys |
| `ParseInto(&struct)` | Populate struct from env vars |
| `Marshal(data, format?)` | Convert map/struct to format string |
| `UnmarshalMap(string, format?)` | Parse format string to map |
| `UnmarshalMapWithConfig(cfg, string, format?)` | One-shot parse with full Config semantics (policy, limits, validation) |
| `UnmarshalStruct(string, &struct, format?)` | Parse string to struct |
| `UnmarshalStructWithConfig(cfg, string, &struct, format?)` | Parse to struct with full Config semantics |
| `UnmarshalInto(map, &struct)` | Populate struct from map |
| `MarshalStruct(struct)` | Convert struct to map |
| `IsMarshalError(err)` | Check if error is a marshal error |
| `DefaultConfig()` | Safe default configuration |
| `DevelopmentConfig()` | Relaxed limits + overwrite enabled |
| `TestingConfig()` | Tight limits, isolated testing |
| `ProductionConfig()` | Strict security + audit enabled |
| `New(cfg...)` | Create loader (at most 1 Config — passing more returns `ErrInvalidConfig`; fully-zero `Config{}` → `DefaultConfig()`; partial Config must start from `DefaultConfig()`) |
| `NewSecureValue(string)` | Create SecureValue from string |
| `NewSecureValueStrict(string)` | Create SecureValue with error on lock failure |
| `ResetDefaultLoader()` | Reset singleton for testing (returns error) |
| `ClearBytes([]byte)` | Securely zero byte slice |
| `ClearInternCache()` | Release the .env key-interning cache (between parse bursts) |
| `SetMemoryLockEnabled(bool)` | Enable/disable memory locking |
| `IsMemoryLockEnabled()` | Check if memory locking is enabled |
| `SetMemoryLockStrict(bool)` | Enable strict mode for lock failures |
| `IsMemoryLockStrict()` | Check if strict mode is enabled |
| `IsMemoryLockSupported()` | Check if platform supports memory locking |
| `RegisterParser(format, factory)` | Register custom parser |
| `ForceRegisterParser(format, factory)` | Override built-in parser |
| `DetectFormat(filename)` | Detect file format from extension |
| `IsSensitiveKey(key)` | Check if key suggests sensitive data |
| `MaskValue(key, value)` | Mask value based on key sensitivity |
| `MaskKey(key)` | Mask key name for logging |
| `SanitizeForLog(s)` | Remove sensitive info from string |
| `MaskSensitiveInString(s)` | Mask sensitive content in string |

### Loader Methods

| Method | Description |
|:-------|:------------|
| **Access** | |
| `GetString(key, def...)` | Get string value |
| `GetInt(key, def...)` | Get `int64` value |
| `GetBool(key, def...)` | Get boolean value |
| `GetDuration(key, def...)` | Get duration value |
| `GetFloat64(key, def...)` | Get `float64` value |
| `GetUint64(key, def...)` | Get `uint64` value |
| `Lookup(key)` | Get value + existence check |
| `GetSecure(key)` | Get `SecureValue` for sensitive data |
| `Keys()` | Get all keys |
| `All()` | Get all variables as map |
| `Len()` | Get variable count |
| **Mutation** | |
| `Set(key, value)` | Set value (returns error) |
| `Delete(key)` | Delete key (returns error) |
| **File & Lifecycle** | |
| `LoadFiles(files...)` | Load files into loader (returns error) |
| `Apply()` | Apply to `os.Environ` (returns error) |
| `ParseInto(&struct)` | Populate struct from env vars |
| `Validate()` | Validate required keys (returns error) |
| `Close()` | Close and cleanup resources |
| **Status** | |
| `IsApplied()` | Check if applied to os.Environ |
| `IsClosed()` | Check if closed |
| `LoadTime()` | Get last load time |
| `Config()` | Get loader configuration (read-only) |

---

## 🔐 Memory Locking (Advanced)

For high-security applications, enable memory locking to prevent sensitive data from being swapped to disk:

```go
// Enable memory locking at startup
env.SetMemoryLockEnabled(true)

// Optional: Enable strict mode to fail if locking fails
env.SetMemoryLockStrict(true)

// Check platform support
if env.IsMemoryLockSupported() {
    // Platform supports mlock/VirtualLock
}

// Create SecureValue with locking
sv := env.NewSecureValue("api_secret")
defer sv.Release()

// Check if memory is actually locked
if sv.IsMemoryLocked() {
    fmt.Println("Memory is protected from swap")
}
```

**Requirements:**
- **Unix**: Requires `CAP_IPC_LOCK` capability or root privileges
- **Windows**: Requires `SE_LOCK_MEMORY_NAME` privilege

---

## 🔌 Custom Parsers (Advanced)

Register custom parsers for additional file formats:

```go
// Register a custom parser (cannot override built-in formats)
err := env.RegisterParser(customFormat, func(cfg env.Config, factory *env.ComponentFactory) (env.EnvParser, error) {
    return &MyCustomParser{validator: factory.Validator()}, nil
})

// Force override built-in parsers (use with caution!)
err := env.ForceRegisterParser(env.FormatEnv, customFactory)
```

---

## 🛡️ Security Features

| Feature | Description |
|:--------|:------------|
| **Key/Value Validation** | Block invalid formats and dangerous patterns |
| **Forbidden Keys** | Prevent overwriting `PATH`, `LD_PRELOAD`, `DYLD_*`, etc. |
| **Size Limits** | File size, line length, variable count limits |
| **Expansion Depth** | Prevent exponential expansion attacks |
| **Sensitive Data Masking** | Auto-detect and mask passwords, tokens, keys |
| **Secure Memory** | `SecureValue` zeroes memory on GC/cleanup |
| **Path Traversal Protection** | Block `..`, absolute paths, UNC paths |

---

## ⚡ Performance

| Metric | Value |
|:-------|:------|
| **Sharded Concurrency** | 32 shards for parallel access |
| **Memory Pooling** | Reusable SecureValue, Builder, Scanner pools |
| **Zero Allocations** | Fast path for simple key lookups |
| **Benchmarks** | Run `go test -bench=. -benchmem` |

---

## 📁 Examples

See the [examples](examples) directory for complete, runnable example programs.
Each example is a self-contained program in its own directory.

| Example | Description |
|:--------|:------------|
| [01_quickstart](examples/01_quickstart/) | Global mode basics: `Load`, typed accessors, `Lookup`, `Set` |
| [02_loader_config](examples/02_loader_config/) | Instance mode, config presets, prefix filter, required keys, lifecycle |
| [03_type_access](examples/03_type_access/) | All typed getters: string, int, bool, duration, float, uint, slice (global + instance) |
| [04_struct_mapping](examples/04_struct_mapping/) | Struct unmarshal/marshal with `env` tags, nested structs, defaults |
| [05_secure_values](examples/05_secure_values/) | `SecureValue` lifecycle, `Close` vs `Release`, masked JSON, memory locking |
| [06_audit_logging](examples/06_audit_logging/) | JSON, Channel, and Log audit handlers; `AuditEvent` fields |
| [07_marshal_unmarshal](examples/07_marshal_unmarshal/) | Format conversion: map ↔ string, struct ↔ map, multi-format output, duplicate-key policy |
| [08_utilities](examples/08_utilities/) | Introspection (`Keys`/`All`/`Len`), masking, format detection |
| [09_error_handling](examples/09_error_handling/) | Sentinel errors (`errors.Is`), typed errors (`errors.As`), key policy, expansion errors |
| [10_concurrency](examples/10_concurrency/) | Concurrent reads, writes, and mixed access |
| [11_advanced](examples/11_advanced/) | Variable expansion (incl. file-only scope), multiple file override, prefix filter, global loader control |
| [12_extensibility](examples/12_extensibility/) | Custom parser registration (`RegisterParser`), interface-based consumption (`EnvGetter`), custom components |
| [13_customization](examples/13_customization/) | Custom `Validator`/`Expander`/`FileSystem`, fail-closed audit (`ErrAuditFailed`), custom `Marshaler`/`Unmarshaler`, brace operators |

Shared data files for the examples live in [`examples/data/`](examples/data/).

---

## 📄 License

MIT License - See [LICENSE](LICENSE) file for details.

---

If this project helps you, please give it a Star! ⭐
