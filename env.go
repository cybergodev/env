// Package env provides a high-security environment variable library for Go 1.25+.
// It is designed for applications where security, concurrent access, and production-grade
// features are critical.
//
// The library supports multiple file formats (.env, JSON, YAML), secure memory handling
// for sensitive values, comprehensive audit logging, and extensive validation.
//
// # Two Usage Modes
//
// The library provides two complementary usage patterns:
//
// ## Global Mode (Simple)
//
// Use package-level functions after calling Load() — or Overload(), its
// last-file-wins variant. Best for simple applications:
//
//	err := env.Load(".env")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	port := env.GetInt("PORT", 8080)
//	host := env.GetString("DATABASE_HOST", "localhost")
//
// ## Instance Mode (Advanced)
//
// Create isolated Loader instances with New(). Best for tests and fine-grained control:
//
//	cfg := env.DefaultConfig()
//	cfg.Filenames = []string{".env"}
//	loader, err := env.New(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer loader.Close()
//	value := loader.GetString("DATABASE_URL")
//
// # When to Use Which Mode
//
// Use Global Mode when:
//   - Simple application with single configuration
//   - Want automatic apply to os.Environ (Load does this by default)
//   - Quick scripts, prototypes, or small services
//
// Use Instance Mode when:
//   - Writing tests that need isolation
//   - Multiple configurations in same process
//   - Need control over when variables are applied to os.Environ
//   - Want explicit lifecycle management with Close()
//
// # Secure Value Handling
//
// For sensitive values like API keys and passwords, use SecureValue:
//
//	sv := loader.GetSecure("API_KEY")
//	if sv != nil {
//	    defer sv.Release()
//	    data := sv.Bytes()
//	    // use data securely
//	    env.ClearBytes(data)
//	}
//
// # Struct Mapping
//
// Map environment variables to structs using tags:
//
//	type Config struct {
//	    Host string `env:"DB_HOST" envDefault:"localhost"`
//	    Port int    `env:"DB_PORT" envDefault:"5432"`
//	}
//
//	var cfg Config
//	if err := env.ParseInto(&cfg); err != nil {
//	    log.Fatal(err)
//	}
//
// # Environment Presets
//
// The library provides preset configurations for different environments:
//   - DefaultConfig: Secure defaults for general use
//   - DevelopmentConfig: Relaxed limits, overwrite enabled
//   - TestingConfig: Isolated, compact limits
//   - ProductionConfig: Strict limits, audit enabled
//
// # Configuration Style
//
// Configuration flows through exactly one channel: the Config struct, seeded
// from DefaultConfig() or one of the presets above. The library has no
// functional options — new settings are added as Config fields with an
// explicit default in DefaultConfig().
//
// Zero-value semantics: a fully zero Config is treated as DefaultConfig() by
// New; a partially-set Config is NOT auto-completed — unset limits stay zero
// and are rejected by Validate with ErrInvalidConfig. Always start from
// DefaultConfig() (or a preset) and modify fields.
//
// Every package-level accessor is a thin proxy for the same-named Loader
// method with the same signature (env.GetString ↔ Loader.GetString,
// env.Set ↔ Loader.Set, ...), so the two layers behave identically once a
// default loader is set. Exceptions: GetSlice pairs with GetSliceFrom
// because Go methods cannot take type parameters; the initializers
// Load/Overload/LoadWithConfig set up the shared default loader (instance
// counterpart: New); and the Loader-only lifecycle methods (LoadFiles,
// Apply, Close, IsClosed, IsApplied, LoadTime, Config) plus the stateless
// parse/marshal/utility families have no package-level counterpart by
// design.
//
// Two consequences of the initializer design are easy to misread as layer
// inconsistencies and are called out explicitly:
//
//   - Uninitialized vs closed errors: the package-level Set/Delete/Validate/
//     ParseInto return ErrNotInitialized when no default loader exists, while
//     the same-named Loader methods return ErrClosed on a nil or closed
//     Loader. These are two genuinely different states, not a mismatch —
//     getDefaultLoader only hands out the live singleton, so a package-level
//     accessor never fails merely because initialization is missing. One
//     narrow exception: a concurrent ResetDefaultLoader can close that
//     singleton after it was fetched, and a package-level mutator then
//     reports ErrClosed exactly like its instance counterpart.
//
//   - AutoApply: Load/Overload/LoadWithConfig force AutoApply=true regardless
//     of cfg.AutoApply, so loaded (and subsequently Set) values are written to
//     os.Environ; New honors cfg.AutoApply and defaults it to false. The same
//     Config therefore has different process-environment side effects across
//     the two layers, by design.
//
// The default loader has no env.Apply()/env.Close(): Load already applies, and
// release goes through ResetDefaultLoader, which closes and clears the
// singleton (the instance counterparts are Loader.Apply/Loader.Close). Every
// other package-level accessor (GetString, GetInt, GetBool, GetUint64,
// GetFloat64, GetDuration, Lookup, GetSecure, Keys, All, Len, Set, Delete,
// Validate, ParseInto, GetSlice) is a direct proxy whose behavior matches the
// same-named Loader method.
//
// A few process-wide knobs are deliberately NOT part of Config because they
// are global rather than per-instance: SetMemoryLockEnabled/SetMemoryLockStrict
// (memory locking), RegisterParser/ForceRegisterParser (the parser registry),
// and the DefaultFileSystem variable. Set these during program initialization.
//
// # File Format Support
//
// Supported file formats:
//   - .env: Standard dotenv format with KEY=value pairs
//   - .json: JSON files with nested structure (flattened with underscores)
//   - .yaml/.yml: YAML files with nested structure (flattened with underscores)
//
// # Thread Safety
//
// All Loader methods are safe for concurrent use. The library uses sharded
// locking for optimal performance in high-concurrency scenarios.
//
// # Panic Safety
//
// Public APIs do not panic on invalid input, on a nil or zero-value Loader,
// or when user-supplied code invoked by the library panics: custom
// components (ComponentConfig), parsers from RegisterParser, and
// struct-field (un)marshaler callbacks are wrapped so a panic surfaces as an
// error from the call that triggered it (see ComponentConfig's panic
// contract).
//
// # Error Types
//
// The library defines several error types for precise error handling:
//   - ErrFileNotFound: File does not exist
//   - ErrFileTooLarge: File exceeds size limit
//   - ErrInvalidKey: Key format validation failed
//   - ErrSecurityViolation: Security policy violation
//   - ErrClosed: Loader has been closed
//   - ParseError: Parsing failure with file/line info
//   - ValidationError: Configuration or value validation failure
//   - SecurityError: Security-related error
//   - FileError: File operation error
//   - ExpansionError: Variable expansion error
//   - JSONError: JSON parsing error
//   - YAMLError: YAML parsing error
//   - MarshalError: Marshaling/unmarshaling error
package env

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cybergodev/env/internal"
)

// Loader is the main type for loading and managing environment variables.
// It provides thread-safe access to environment variables with full
// security validation, audit logging, and error handling.
//
// The zero value is not usable: construct Loaders with New (directly or via
// Load/Overload/LoadWithConfig). Method calls on a zero-value Loader fail
// closed — ErrClosed from state-changing methods, zero values from getters —
// rather than panicking.
type Loader struct {
	config      Config
	factory     *ComponentFactory
	ownsFactory bool
	parsers     map[FileFormat]EnvParser
	fs          FileSystem

	mu   sync.RWMutex
	vars *secureMap
	// closedFast mirrors closed as an atomic so single-key reads (Lookup,
	// GetSecure) can check loader state without taking l.mu — under high
	// concurrency the RWMutex reader count itself becomes the bottleneck.
	// It is written exactly once, under l.mu, in Close.
	closedFast  atomic.Bool
	applied     bool
	appliedKeys map[string]struct{} // process-env keys this loader actually set
	closed      bool
	loadTime    time.Time

	// Capability assertions resolved once at construction so the Set hot
	// path does not repeat them on every call (see validateSetKV). The
	// factory's validator is fixed for the Loader's lifetime, so caching
	// the assertion result is equivalent to re-asserting per call.
	setKeyCap   setKeyValidator
	basicValCap basicValueValidator
}

// Compile-time check that Loader implements EnvLoader.
var _ EnvLoader = (*Loader)(nil)

// Compile-time check that Loader implements io.Closer.
var _ io.Closer = (*Loader)(nil)

// enterRead acquires the read lock and validates loader state.
// Returns ErrClosed if the loader is nil, not constructed by New (zero
// value: vars is nil), or closed.
// Caller must call exitRead() when done.
func (l *Loader) enterRead() error {
	if l == nil || l.vars == nil {
		return ErrClosed
	}
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return ErrClosed
	}
	return nil
}

// exitRead releases the read lock.
func (l *Loader) exitRead() { l.mu.RUnlock() }

// enterWrite acquires the write lock and validates loader state.
// Returns ErrClosed if the loader is nil, not constructed by New (zero
// value: vars is nil), or closed.
// Caller must call exitWrite() when done.
func (l *Loader) enterWrite() error {
	if l == nil || l.vars == nil {
		return ErrClosed
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return ErrClosed
	}
	return nil
}

// exitWrite releases the write lock.
func (l *Loader) exitWrite() { l.mu.Unlock() }

// cleanupOnError closes the loader if cleanup is true, logging any close error.
// Returns the original error regardless of cleanup outcome.
func (l *Loader) cleanupOnError(err error, cleanup bool) error {
	if !cleanup {
		return err
	}
	if closeErr := l.Close(); closeErr != nil {
		_ = l.factory.Auditor().LogError(internal.ActionError, "", "cleanup failed: "+closeErr.Error())
	}
	return err
}

// New creates a new Loader with the given configuration.
//
// BEHAVIOR:
//   - Does NOT set the package-level default loader
//   - Does NOT auto-apply to os.Environ (unless cfg.AutoApply = true)
//   - With no Config (or a fully zero one) it uses DefaultConfig, whose
//     Filenames is [".env"] — an empty loader requires clearing Filenames
//     explicitly; a partially-set Config is NOT auto-completed (see below)
//   - Can be called multiple times to create independent instances
//   - Requires explicit lifecycle management: defer loader.Close()
//
// Configuration semantics:
//   - cfg is variadic for call-site convenience: New() and New(Config{})
//     both mean DefaultConfig(). At most one Config is accepted; passing
//     several is rejected with ErrInvalidConfig — silently ignoring the
//     extras would hide a call-site bug (the caller believes the second
//     Config took effect).
//   - A fully zero Config is replaced by DefaultConfig(). A partially-set
//     Config is NOT auto-completed — unset limits stay zero and Validate
//     rejects them with ErrInvalidConfig. Always start from DefaultConfig()
//     or one of the presets and modify fields.
//
// FOR SIMPLE USE CASES: Use Load() instead, which sets up
// the package-level default and applies to os.Environ automatically.
//
// WHEN TO USE New():
//   - Multiple loaders in one application (different configs/files)
//   - Testing with isolated environment state
//   - When you need explicit control over when variables are applied
//
// Example:
//
//	// Use default configuration
//	loader, err := env.New()
//
//	// Use custom configuration
//	cfg := env.DefaultConfig()
//	cfg.Filenames = []string{".env.production"}
//	cfg.AutoApply = true
//	loader, err := env.New(cfg)
func New(cfg ...Config) (*Loader, error) {
	if len(cfg) > 1 {
		return nil, fmt.Errorf("%w: New accepts at most one Config, got %d; "+
			"extra Configs are never applied — pass exactly one", ErrInvalidConfig, len(cfg))
	}
	var c Config
	if len(cfg) > 0 {
		c = cfg[0]
		// If zero-value Config is passed, use defaults
		if c.IsZero() {
			c = DefaultConfig()
		}
	} else {
		c = DefaultConfig()
	}

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	// Use default file system if not specified; custom file systems are
	// wrapped for panic containment (SEC-003, see recovering.go).
	fs := resolveComponentFileSystem(c.FileSystem)

	// Build shared components once
	factory := c.buildComponentFactoryWithFS(fs)

	// Create parsers using registry
	parsers, err := createParsers(c, factory)
	if err != nil {
		if closeErr := factory.Close(); closeErr != nil {
			// Log close error via factory's auditor if available
			if aud := factory.Auditor(); aud != nil {
				_ = aud.LogError(internal.ActionError, "", "factory cleanup failed: "+closeErr.Error())
			}
		}
		return nil, err
	}

	loader := &Loader{
		config:      c,
		factory:     factory,
		ownsFactory: true,
		parsers:     parsers,
		fs:          fs,
		vars:        newSecureMap(),
	}
	// Resolve the optional validator capabilities once; a nil value means
	// the capability is absent and validateSetKV falls back to the base
	// interface methods, exactly as a per-call assertion would.
	if v, ok := factory.Validator().(setKeyValidator); ok {
		loader.setKeyCap = v
	}
	if bv, ok := factory.Validator().(basicValueValidator); ok {
		loader.basicValCap = bv
	}

	// Auto-load files if Filenames is configured
	if len(c.Filenames) > 0 {
		if err := loader.loadFilesInternal(c.Filenames, true); err != nil {
			return nil, err
		}
	}

	return loader, nil
}

// LoadFiles loads environment variables from multiple files in order.
// If no filenames are provided, defaults to ".env".
// Files are loaded sequentially. A later file only overrides values from an
// earlier one when OverwriteExisting is true; with the default (false) the
// first file to set a key wins.
//
// Paths must be RELATIVE: absolute paths, Windows drive letters, UNC paths,
// and any ".." component are rejected with SecurityError. Docker/Kubernetes
// users mounting configs at absolute locations should chdir to the config
// directory or pass paths relative to the working directory.
//
// Returns:
//   - ErrClosed: if the loader is nil or has been closed
//   - ErrFileNotFound: if a file does not exist and FailOnMissingFile is true
//   - ParseError: if a file contains invalid syntax
//   - SecurityError: if a file path fails security validation
//   - ErrFileTooLarge: if a file exceeds MaxFileSize
//
// Example:
//
//	// Load default .env file
//	err := loader.LoadFiles()
//
//	// Load specific files
//	err := loader.LoadFiles(".env", ".env.local")
func (l *Loader) LoadFiles(filenames ...string) error {
	if err := l.enterWrite(); err != nil {
		return err
	}
	defer l.exitWrite()

	// Default to .env if no files specified
	if len(filenames) == 0 {
		filenames = []string{".env"}
	}

	return l.loadFilesInternal(filenames, false)
}

// loadFilesInternal is the shared implementation for file loading.
// Thread safety: caller MUST hold l.mu (write lock). New() callers are exempt
// because the loader is not yet shared, but LoadFiles() acquires the lock first.
// This method modifies l.loadTime and l.applied, which require write-lock protection.
//
// The cleanupOnErr parameter determines whether to close the loader on error (used during initialization).
func (l *Loader) loadFilesInternal(filenames []string, cleanupOnErr bool) error {
	var start time.Time
	auditEnabled := l.config.AuditEnabled
	if auditEnabled {
		start = time.Now()
	}

	for _, filename := range filenames {
		if err := l.loadFileLocked(filename); err != nil {
			if errors.Is(err, ErrFileNotFound) && !l.config.FailOnMissingFile {
				if l.config.AuditEnabled {
					_ = l.factory.Auditor().LogWithFile(internal.ActionLoad, "", filename, "file not found, skipping", true)
				}
				continue
			}
			return l.cleanupOnError(err, cleanupOnErr)
		}
	}

	l.loadTime = time.Now()
	if auditEnabled {
		_ = l.factory.Auditor().LogWithDuration(internal.ActionLoad, "", "loaded files", true, time.Since(start))
	}

	if l.config.AutoApply {
		if err := l.applyLocked(); err != nil {
			return l.cleanupOnError(err, cleanupOnErr)
		}
	}

	return l.auditCheckFailClosed()
}

// auditCheckFailClosed surfaces a latched audit failure when the loader is
// configured with AuditFailClosed (GEN-001). The built-in auditor latches
// the first handler failure; a CustomAuditor participates by implementing
// TakeFailure() error, which is detected via interface assertion so the
// AuditLogger interface itself stays unchanged. Checked at the end of
// top-level mutating operations so a lost audit trail fails the operation
// that caused it. With concurrent operations the latched failure is
// delivered to whichever operation checks first — signaling is best-effort,
// not exactly-once.
func (l *Loader) auditCheckFailClosed() error {
	if !l.config.AuditFailClosed {
		return nil
	}
	taker, ok := l.factory.Auditor().(interface{ TakeFailure() error })
	if !ok {
		return nil
	}
	if err := taker.TakeFailure(); err != nil {
		return fmt.Errorf("%w: audit event lost while fail-closed auditing was enabled: %v",
			ErrAuditFailed, err)
	}
	return nil
}

// loadFileLocked loads a single file.
// Thread safety: caller MUST hold l.mu (write lock). While individual secureMap
// operations are internally synchronized, this method modifies loader state
// (via loadFilesInternal → l.loadTime, l.applied) that requires write-lock protection.
//
// SECURITY - Defense-in-Depth for TOCTOU:
// There is a theoretical Time-Of-Check-Time-Of-Use window between Open() and Stat()
// where the file could be replaced or modified. This is mitigated by:
//  1. SecureReader: The parser wraps the file with SecureReader which enforces
//     hard limits during reading, providing secondary enforcement of size constraints.
//  2. Hard Limits: Even if the file grows between Stat() and Read(), SecureReader
//     will stop reading at hardMaxFileSize (100MB), preventing memory exhaustion.
//  3. Validation: All parsed content is validated for size, format, and safety
//     regardless of the initial Stat() results.
func (l *Loader) loadFileLocked(filename string) error {
	// Only record start time when audit is enabled — avoids time.Now() syscall
	// and string concatenation in the common (audit-disabled) case.
	var start time.Time
	auditEnabled := l.config.AuditEnabled
	if auditEnabled {
		start = time.Now()
	}

	// SECURITY: Validate file path to prevent path traversal attacks
	if err := validateFilePathForFS(filename, l.fs); err != nil {
		_ = l.factory.Auditor().LogError(internal.ActionSecurity, "", "path validation failed: "+err.Error())
		return err
	}

	file, err := l.fs.Open(filename)
	if err != nil {
		// os.IsNotExist does not traverse wrapped error chains, so a custom
		// FileSystem returning fmt.Errorf("...: %w", fs.ErrNotExist) is also
		// checked via errors.Is — otherwise FailOnMissingFile=false never
		// skipped such files.
		if os.IsNotExist(err) || errors.Is(err, ErrFileNotFound) || errors.Is(err, fs.ErrNotExist) {
			return newFileError(filename, "open", ErrFileNotFound)
		}
		return newFileError(filename, "open", err)
	}
	defer func() { _ = file.Close() }() // read-only handle; close error is not actionable

	// Get file info for size check
	// Note: This provides a fast-path check for obviously oversized files.
	// SecureReader provides defense-in-depth for files that grow after this check.
	info, err := file.Stat()
	if err != nil {
		return newFileError(filename, "stat", err)
	}

	if info.Size() > l.config.MaxFileSize {
		return &FileError{
			Path:  filename,
			Op:    "size_check",
			Size:  info.Size(),
			Limit: l.config.MaxFileSize,
			Err:   ErrFileTooLarge,
		}
	}

	// Detect file format and select appropriate parser
	format := DetectFormat(filename)
	parser, ok := l.parsers[format]
	if !ok {
		// Fall back to dot-env parser for unknown formats or FormatAuto
		parser = l.parsers[FormatEnv]
	}

	if parser == nil {
		return newFileError(filename, "parse", fmt.Errorf("no parser registered for format %v", format))
	}

	vars, err := parser.Parse(file, filename)
	if err != nil {
		return err
	}

	// Fast path: no prefix filter → apply the whole file directly.
	// SetAll overwrites; SetAllIfAbsent skips existing keys without building
	// an intermediate filtered map or per-key Get() allocations.
	if l.config.Prefix == "" {
		if l.config.AuditEnabled {
			for key := range vars {
				if !l.config.OverwriteExisting && l.vars.Has(key) {
					_ = l.factory.Auditor().Log(internal.ActionSet, key, "skipped (no overwrite)", false)
				} else {
					_ = l.factory.Auditor().Log(internal.ActionSet, key, "loaded", true)
				}
			}
		}
		if l.config.OverwriteExisting {
			l.vars.SetAll(vars)
		} else {
			l.vars.SetAllIfAbsent(vars)
		}
		if auditEnabled {
			_ = l.factory.Auditor().LogWithDuration(internal.ActionFileAccess, "", "file loaded: "+filename, true, time.Since(start))
		}
		return nil
	}

	// Slow path: prefix filtering needed.
	// Pre-compute uppercase prefix once outside the loop.
	toSet := make(map[string]string, len(vars))
	upperPrefix := internal.ToUpperASCII(l.config.Prefix)

	for key, value := range vars {
		// Check prefix if configured (case-insensitive, zero-allocation)
		if !internal.HasUpperPrefix(key, upperPrefix) {
			continue
		}

		// Check overwrite policy using Has (no string allocation)
		if !l.config.OverwriteExisting && l.vars.Has(key) {
			if l.config.AuditEnabled {
				_ = l.factory.Auditor().Log(internal.ActionSet, key, "skipped (no overwrite)", false)
			}
			continue
		}

		toSet[key] = value
		if l.config.AuditEnabled {
			_ = l.factory.Auditor().Log(internal.ActionSet, key, "loaded", true)
		}
	}

	// Use batch set for better performance
	l.vars.SetAll(toSet)

	if auditEnabled {
		_ = l.factory.Auditor().LogWithDuration(internal.ActionFileAccess, "", "file loaded: "+filename, true, time.Since(start))
	}
	return nil
}

// Apply applies all loaded variables to the process environment.
// Only sets variables that do not already exist unless OverwriteExisting is true.
//
// If an os.Setenv fails midway, the loop stops and returns the error WITHOUT
// rolling back: keys already set remain in the process environment while
// IsApplied reports false (the pass did not complete). Treat a failed Apply
// as "partially applied", not as "nothing was applied".
//
// Returns:
//   - ErrClosed: if the loader is nil or has been closed
//   - Wrapped os errors: if setting an environment variable fails
func (l *Loader) Apply() error {
	if err := l.enterWrite(); err != nil {
		return err
	}
	defer l.exitWrite()

	if err := l.applyLocked(); err != nil {
		return err
	}
	return l.auditCheckFailClosed()
}

// applyLocked applies variables to the environment.
// Thread safety: caller MUST hold l.mu (write lock). This method sets l.applied
// and l.appliedKeys, which require write-lock protection.
func (l *Loader) applyLocked() error {
	for key, value := range l.vars.ToMap() {
		// Check for existing value using LookupEnv to distinguish between
		// "not set" and "empty string". This correctly handles the case where
		// an environment variable is explicitly set to empty string.
		if _, exists := l.fs.LookupEnv(key); exists && !l.config.OverwriteExisting {
			if l.config.AuditEnabled {
				_ = l.factory.Auditor().Log(internal.ActionSet, key, "skipped (existing)", false)
			}
			continue
		}

		if err := l.fs.Setenv(key, value); err != nil {
			_ = l.factory.Auditor().LogError(internal.ActionSet, key, err.Error())
			return fmt.Errorf("failed to set %s: %w", MaskKey(key), err)
		}
		l.markAppliedLocked(key)

		if l.config.AuditEnabled {
			_ = l.factory.Auditor().Log(internal.ActionSet, key, "applied", true)
		}
	}

	l.applied = true
	return nil
}

// markAppliedLocked records that this loader set key in the process
// environment. Thread safety: caller MUST hold l.mu (write lock).
func (l *Loader) markAppliedLocked(key string) {
	if l.appliedKeys == nil {
		l.appliedKeys = make(map[string]struct{})
	}
	l.appliedKeys[key] = struct{}{}
}

// wasAppliedLocked reports whether this loader set key in the process
// environment. Thread safety: caller MUST hold l.mu (either lock).
func (l *Loader) wasAppliedLocked(key string) bool {
	_, ok := l.appliedKeys[key]
	return ok
}

// setKeyValidator is an optional capability of validators that can validate
// programmatically-set keys under the structured-format key rules (dots,
// hyphens and other characters the .env pattern rejects). It is defined here
// (consumer side) so existing external Validator implementations keep
// compiling; they simply fall back to ValidateKey.
type setKeyValidator interface {
	ValidateSetKey(key string) error
}

// basicValueValidator is an optional capability of validators that enforce a
// baseline of value checks (maximum length, control-character rejection)
// independent of the ValidateValues switch. Defined consumer-side (like
// setKeyValidator above) so existing external Validator implementations keep
// compiling; when ValidateValues is false they keep the previous behavior of
// no value validation.
type basicValueValidator interface {
	ValidateValueBasic(value string) error
}

// validateSetKV validates a Set key/value pair and reports failures to the
// auditor. Shared by both lock paths of Set. The optional-capability
// assertions (setKeyCap / basicValCap) were resolved once in New.
func (l *Loader) validateSetKV(key, value string) error {
	// Validate key. Programmatic keys follow the structured-format key rules
	// (IsValidJSONKey) when the validator supports it: the store legitimately
	// holds dotted keys loaded from YAML/JSON files, and Set must accept the
	// same key space. Validators without the capability (custom minimal
	// implementations) keep the stricter .env-pattern behavior.
	if l.setKeyCap != nil {
		if err := l.setKeyCap.ValidateSetKey(key); err != nil {
			_ = l.factory.Auditor().LogError(internal.ActionSet, key, err.Error())
			return err
		}
	} else if err := l.factory.Validator().ValidateKey(key); err != nil {
		_ = l.factory.Auditor().LogError(internal.ActionSet, key, err.Error())
		return err
	}

	// Validate value. ValidateValues gates whether the validator's full
	// ValidateValue runs; when it is off, the baseline capability
	// (ValidateValueBasic) still applies. For the built-in validator the two
	// are equivalent, so Set matches the .env parse path for every flag
	// combination (the UTF-8 check follows ValidateUTF8 alone); custom
	// validators without the capability skip value checks when the switch is
	// off, as before.
	if l.config.ValidateValues {
		if err := l.factory.Validator().ValidateValue(value); err != nil {
			_ = l.factory.Auditor().LogError(internal.ActionSet, key, err.Error())
			return err
		}
	} else if l.basicValCap != nil {
		if err := l.basicValCap.ValidateValueBasic(value); err != nil {
			_ = l.factory.Auditor().LogError(internal.ActionSet, key, err.Error())
			return err
		}
	}
	return nil
}

// Set sets a value for a key.
// If OverwriteExisting is false and the key already exists, the call is silently skipped.
//
// With AutoApply enabled the value is also applied to the process
// environment under the same skip policy as Apply: when OverwriteExisting is
// false and the variable already exists in the process environment, the
// environment copy is left unchanged (the loader's in-memory value is still
// updated). If applying to the process environment fails, Set returns the
// error with BOTH copies unchanged — the in-memory store is updated only
// after a successful Setenv, so a failed Set can be retried.
//
// With the default configuration, keys follow the structured-format key rules
// (letters, digits, '_', '@', '-', '.' — the same shape YAML/JSON file loads
// accept), so keys containing dots such as "APNEWS.COM" are settable. A
// custom KeyPattern keeps governing Set unchanged.
//
// Returns:
//   - ErrClosed: if the loader is nil or has been closed
//   - ErrInvalidKey: if the key fails validation
//   - ErrForbiddenKey: if the key is rejected by the forbidden-keys or allowed-keys policy
//   - ErrInvalidValue: if ValidateValues is true and the value fails validation
//   - ErrAuditFailed: if AuditFailClosed is set and an audit event was lost
func (l *Loader) Set(key, value string) error {
	if err := l.set(key, value); err != nil {
		return err
	}
	return l.auditCheckFailClosed()
}

// set is Set's implementation; the public wrapper adds the fail-closed
// audit check so every success path (including the no-overwrite skip)
// surfaces a lost audit event.
func (l *Loader) set(key, value string) error {
	// Fast path: with OverwriteExisting there is no Has→Set check-then-act to
	// serialize, and without AutoApply, Set touches no loader-level state
	// (applied/appliedKeys/loadTime) — only l.vars, which synchronizes
	// internally. A read lock therefore suffices, keeping concurrent Set
	// calls from serializing on the loader mutex; Close() still takes the
	// write lock, so it fully excludes this path.
	// l.config is immutable after New, so reading it here needs no lock.
	// The l.vars check mirrors enterRead/enterWrite: a zero-value Loader
	// (never passed through New) fails closed instead of dereferencing nil.
	if l != nil && l.vars != nil && l.config.OverwriteExisting && !l.config.AutoApply {
		if err := l.enterRead(); err != nil {
			return err
		}
		defer l.exitRead()

		if err := l.validateSetKV(key, value); err != nil {
			return err
		}
		l.vars.Set(key, value)
		if l.config.AuditEnabled {
			_ = l.factory.Auditor().Log(internal.ActionSet, key, "set", true)
		}
		return nil
	}

	if err := l.enterWrite(); err != nil {
		return err
	}
	defer l.exitWrite()

	if err := l.validateSetKV(key, value); err != nil {
		return err
	}

	// Check overwrite policy
	// Use Has (no allocation) instead of Get (allocates string copy).
	if !l.config.OverwriteExisting {
		if l.vars.Has(key) {
			if l.config.AuditEnabled {
				_ = l.factory.Auditor().Log(internal.ActionSet, key, "skipped (no overwrite)", false)
			}
			return nil
		}
	}

	// With AutoApply, set the process environment FIRST and store in memory
	// only on success: a failed Setenv previously left the new value stored
	// while Set reported an error, and with OverwriteExisting=false a retry
	// then hit the skip branch above — the caller could neither detect nor
	// redo the failed Set.
	if l.config.AutoApply {
		// Mirror applyLocked's policy: with OverwriteExisting disabled, a
		// variable that already exists in the process environment is left
		// untouched there while the in-memory value is still updated.
		if _, exists := l.fs.LookupEnv(key); exists && !l.config.OverwriteExisting {
			l.vars.Set(key, value)
			if l.config.AuditEnabled {
				_ = l.factory.Auditor().Log(internal.ActionSet, key, "skipped (existing)", false)
			}
			return nil
		}
		if err := l.fs.Setenv(key, value); err != nil {
			// Mirror applyLocked: the os-level failure reaches the audit
			// stream, not only the returned error.
			_ = l.factory.Auditor().LogError(internal.ActionSet, key, err.Error())
			return fmt.Errorf("failed to set environment: %w", err)
		}
		l.vars.Set(key, value)
		if l.config.AuditEnabled {
			_ = l.factory.Auditor().Log(internal.ActionSet, key, "set", true)
		}
		// Record the key so Delete() knows to unset it from the process
		// environment, and mark the loader as applied for IsApplied().
		l.markAppliedLocked(key)
		l.applied = true
		return nil
	}

	l.vars.Set(key, value)
	if l.config.AuditEnabled {
		_ = l.factory.Auditor().Log(internal.ActionSet, key, "set", true)
	}

	return nil
}

// Delete removes a key from the loaded environment.
// If the key was applied to the process environment by this loader
// (via Apply, AutoApply, or Set with AutoApply), it is also unset there.
// Keys the loader never applied are not touched in the process environment.
//
// Returns:
//   - ErrClosed: if the loader is nil or has been closed
//   - Wrapped os errors: if unsetting an applied variable from the process
//     environment fails (the in-memory copy is already removed by then)
func (l *Loader) Delete(key string) error {
	if err := l.enterWrite(); err != nil {
		return err
	}
	defer l.exitWrite()

	l.vars.Delete(key)
	if l.config.AuditEnabled {
		_ = l.factory.Auditor().Log(internal.ActionDelete, key, "deleted", true)
	}

	// Remove from the process environment only if this loader put it there.
	// Per-key tracking matters: unsetting on l.applied alone would remove
	// foreign process variables (HOME, TERM, ...) the loader never owned.
	if l.wasAppliedLocked(key) {
		if err := l.fs.Unsetenv(key); err != nil {
			// The in-memory delete above already happened and appliedKeys
			// keeps the entry (state stays coherent), but the caller must
			// see that a possibly sensitive value is still live in the
			// process environment — swallowing this while Apply/Set fail
			// loudly on their os counterparts was asymmetric.
			_ = l.factory.Auditor().LogError(internal.ActionDelete, key, err.Error())
			return fmt.Errorf("failed to unset %s: %w", MaskKey(key), err)
		}
		delete(l.appliedKeys, key)
	}

	return l.auditCheckFailClosed()
}

// closeParsers closes parsers that hold resources, best-effort. Shared by
// Loader.Close and the one-shot parse path in UnmarshalMapWithConfig, so a
// custom parser registered via RegisterParser gets the same cleanup whether
// it ran inside a Loader or a standalone parse.
func closeParsers(parsers map[FileFormat]EnvParser) {
	for _, p := range parsers {
		if closer, ok := p.(io.Closer); ok {
			_ = closer.Close() // best-effort, matches createParsers cleanup
		}
	}
}

// Close closes the loader and securely clears all stored values.
// If the loader owns its ComponentFactory, it will also close the factory.
//
// Note: variables this loader previously applied to the process environment
// (via Apply, AutoApply, or Set with AutoApply) are NOT unset by Close —
// only the in-memory copies are cleared. Use Delete for per-key removal
// from the process environment before closing.
func (l *Loader) Close() error {
	if err := l.enterWrite(); err != nil {
		return nil // Close on nil/closed is not an error
	}
	defer l.exitWrite()

	l.vars.Clear()

	// Mark the loader closed before closing the owned factory. This guarantees
	// the loader is never left half-open (vars cleared but closed=false) if
	// factory.Close fails; subsequent calls observe a closed loader either way.
	// closedFast publishes the same state to lock-free single-key readers.
	l.closed = true
	l.closedFast.Store(true)

	// Close parsers that hold resources, mirroring the registry's
	// construction-failure path (createParsers closes already-created
	// parsers when a sibling factory fails). Built-in parsers are not
	// closable; custom parsers registered via RegisterParser may be, and
	// skipping this leaked their resources for the loader's whole lifetime.
	closeParsers(l.parsers)

	// Only close the factory if we own it.
	// This prevents double-closing when the factory is shared.
	if l.ownsFactory && l.factory != nil {
		if err := l.factory.Close(); err != nil {
			return err
		}
	}

	return nil
}

// IsClosed returns true if the loader has been closed, and for a nil or
// zero-value Loader (never constructed through New). Reads the atomic
// closedFast mirror instead of taking l.mu, consistent with the lock-free
// single-key reads (Lookup, GetSecure); both flags are set together under
// the write lock in Close.
func (l *Loader) IsClosed() bool {
	if l == nil || l.vars == nil {
		return true
	}
	return l.closedFast.Load()
}

// ParseInto populates a struct from loaded environment variables.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Optional `envDefault:"value"` sets a default if the key is not found.
//
// Returns:
//   - ErrClosed: if the loader is nil or has been closed
//   - MarshalError: if struct tag parsing or type conversion fails
func (l *Loader) ParseInto(v any) error {
	// Hold the read lock only while taking the snapshot, to prevent a TOCTOU
	// race where another goroutine closes the loader between the state check
	// and ToMap(). The lock is released before UnmarshalInto because it runs
	// user-provided UnmarshalText/UnmarshalEnv callbacks; a callback that
	// re-enters a write path (Set, Delete, Close) would self-deadlock on
	// l.mu, which is not reentrant.
	if err := l.enterRead(); err != nil {
		return err
	}
	m := l.vars.ToMap()
	l.exitRead()
	return UnmarshalInto(m, v)
}

// Validate validates the loaded environment against required keys.
// Checks that all RequiredKeys are present. ForbiddenKeys are enforced at
// Set/parse time (see Set), not by this method.
//
// Returns:
//   - ErrClosed: if the loader is nil or has been closed
//   - ErrMissingRequired: if a required key is not present
func (l *Loader) Validate() error {
	// Hold the read lock across the snapshot to prevent a TOCTOU race where
	// another goroutine closes the loader between the state check and Keys().
	if err := l.enterRead(); err != nil {
		return err
	}
	defer l.exitRead()
	// Pooled uppercase-key index, the same mechanism the parsers use
	// (validateRequiredKeys); this previously built a fresh map per call.
	upperKeys := internal.KeysToUpperFromSlice(l.vars.Keys())
	defer internal.PutKeysToUpperMap(upperKeys)
	return l.factory.Validator().ValidateRequired(upperKeys)
}

// validateFilePath validates a file path for security with the FULL rule
// set, including symlink resolution against the real file system.
// It checks for path traversal attempts and other potentially dangerous patterns.
// Note: This function delegates to internal.PathValidator for the actual validation.
func validateFilePath(filename string) error {
	return internal.ValidateFilePath(filename)
}

// validateFilePathForFS validates a file path at the right fidelity for fs:
// the full rule set — including real symlink resolution — when fs is the OS
// file system, and the static, disk-free rules when fs is injected.
// ValidateFilePath's EvalSymlinks step bypasses the FileSystem abstraction:
// with a custom (e.g. in-memory) file system it consulted host-disk state
// the injected FS never reads, so a load could be rejected (or accepted)
// based on files outside the injected boundary. For an injected FS its own
// Open is the final arbiter of what exists; the SecureReader limits remain
// as defense in depth either way.
func validateFilePathForFS(filename string, fs FileSystem) error {
	if isOSFileSystem(fs) {
		return validateFilePath(filename)
	}
	return internal.ValidateFilePathStatic(filename)
}

// isOSFileSystem reports whether fs, after unwrapping the panic-containment
// layer (see recovering.go), is the real OS file system.
func isOSFileSystem(fs FileSystem) bool {
	if wrapped, ok := fs.(*recoveringFileSystem); ok {
		fs = wrapped.FileSystem
	}
	_, ok := fs.(OSFileSystem)
	return ok
}
