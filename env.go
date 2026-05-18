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
// Use package-level functions after calling Load(). Best for simple applications:
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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cybergodev/env/internal"
)

// Loader is the main type for loading and managing environment variables.
// It provides thread-safe access to environment variables with full
// security validation, audit logging, and error handling.
type Loader struct {
	config      Config
	factory     *ComponentFactory
	ownsFactory bool
	validator   Validator
	auditor     FullAuditLogger
	expander    VariableExpander
	parsers     map[FileFormat]EnvParser
	fs          FileSystem

	mu       sync.RWMutex
	vars     *secureMap
	applied  bool
	closed   bool
	loadTime time.Time
}

// Compile-time check that Loader implements EnvLoader.
var _ EnvLoader = (*Loader)(nil)

// Compile-time check that Loader implements io.Closer.
var _ io.Closer = (*Loader)(nil)

// New creates a new Loader with the given configuration.
//
// BEHAVIOR:
//   - Does NOT set the package-level default loader
//   - Does NOT auto-apply to os.Environ (unless cfg.AutoApply = true)
//   - Can be called multiple times to create independent instances
//   - Requires explicit lifecycle management: defer loader.Close()
//
// If no configuration is provided or a zero-value Config is passed,
// DefaultConfig() is used automatically.
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
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Use default file system if not specified
	fs := c.FileSystem
	if fs == nil {
		fs = DefaultFileSystem
	}

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
		validator:   factory.Validator(),
		auditor:     factory.Auditor(),
		expander:    factory.Expander(),
		parsers:     parsers,
		fs:          fs,
		vars:        newSecureMap(),
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
// Files are loaded sequentially; later files can override values from earlier files.
//
// Example:
//
//	// Load default .env file
//	err := loader.LoadFiles()
//
//	// Load specific files
//	err := loader.LoadFiles(".env", ".env.local")
func (l *Loader) LoadFiles(filenames ...string) error {
	if l == nil {
		return ErrClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return ErrClosed
	}

	// Default to .env if no files specified
	if len(filenames) == 0 {
		filenames = []string{".env"}
	}

	return l.loadFilesInternal(filenames, false)
}

// loadFilesInternal is the shared implementation for file loading.
// Thread safety:
//   - Called from New(): no lock required (loader not yet shared)
//   - Called from LoadFiles(): caller must hold l.mu
// The cleanupOnErr parameter determines whether to close the loader on error (used during initialization).
func (l *Loader) loadFilesInternal(filenames []string, cleanupOnErr bool) error {
	start := time.Now()

	for _, filename := range filenames {
		if err := l.loadFileLocked(filename); err != nil {
			if errors.Is(err, ErrFileNotFound) && !l.config.FailOnMissingFile {
				_ = l.auditor.LogWithFile(internal.ActionLoad, "", filename, "file not found, skipping", true)
				continue
			}
			if cleanupOnErr {
				if closeErr := l.Close(); closeErr != nil {
					_ = l.auditor.LogError(internal.ActionError, "", "cleanup failed: "+closeErr.Error())
				}
			}
			return err
		}
	}

	l.loadTime = time.Now()
	_ = l.auditor.LogWithDuration(internal.ActionLoad, "", "loaded files", true, time.Since(start))

	if l.config.AutoApply {
		if err := l.applyLocked(); err != nil {
			if cleanupOnErr {
				if closeErr := l.Close(); closeErr != nil {
					_ = l.auditor.LogError(internal.ActionError, "", "cleanup failed: "+closeErr.Error())
				}
			}
			return err
		}
	}

	return nil
}

// loadFileLocked loads a single file.
// Thread safety: safe to call with or without l.mu; secureMap provides its own locking.
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
	start := time.Now()

	// SECURITY: Validate file path to prevent path traversal attacks
	if err := validateFilePath(filename); err != nil {
		_ = l.auditor.LogError(internal.ActionSecurity, "", "path validation failed: "+err.Error())
		return err
	}

	file, err := l.fs.Open(filename)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, ErrFileNotFound) {
			return newFileError(filename, "open", ErrFileNotFound)
		}
		return newFileError(filename, "open", err)
	}
	defer file.Close()

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

	// Fast path: if no prefix filter and overwrite is enabled, use SetAll directly
	if l.config.Prefix == "" && l.config.OverwriteExisting {
		// SECURITY: Log sensitive key operations even in fast path for audit trail
		for key := range vars {
			if IsSensitiveKey(key) {
				_ = l.auditor.Log(internal.ActionSet, key, "loaded (sensitive)", true)
			}
		}
		l.vars.SetAll(vars)
		_ = l.auditor.LogWithDuration(internal.ActionFileAccess, "", "file loaded: "+filename, true, time.Since(start))
		return nil
	}

	// Filter and prepare variables for batch set
	toSet := make(map[string]string, len(vars))

	// Pre-compute uppercase prefix once outside the loop
	upperPrefix := internal.ToUpperASCII(l.config.Prefix)

	for key, value := range vars {
		// Check prefix if configured (case-insensitive for consistency with other key matching)
		if l.config.Prefix != "" && !strings.HasPrefix(internal.ToUpperASCII(key), upperPrefix) {
			continue
		}

		// Check overwrite policy
		if _, exists := l.vars.Get(key); exists && !l.config.OverwriteExisting {
			_ = l.auditor.Log(internal.ActionSet, key, "skipped (no overwrite)", false)
			continue
		}

		toSet[key] = value
		_ = l.auditor.Log(internal.ActionSet, key, "loaded", true)
	}

	// Use batch set for better performance
	l.vars.SetAll(toSet)

	_ = l.auditor.LogWithDuration(internal.ActionFileAccess, "", "file loaded: "+filename, true, time.Since(start))
	return nil
}

// Apply applies all loaded variables to the process environment.
func (l *Loader) Apply() error {
	if l == nil {
		return ErrClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return ErrClosed
	}

	return l.applyLocked()
}

// applyLocked applies variables to the environment.
// Thread safety: safe to call with or without l.mu; individual operations are internally synchronized.
func (l *Loader) applyLocked() error {
	keys := l.vars.Keys()
	for _, key := range keys {
		value, ok := l.vars.Get(key)
		if !ok {
			continue
		}

		// Check for existing value using LookupEnv to distinguish between
		// "not set" and "empty string". This correctly handles the case where
		// an environment variable is explicitly set to empty string.
		if _, exists := l.fs.LookupEnv(key); exists && !l.config.OverwriteExisting {
			_ = l.auditor.Log(internal.ActionSet, key, "skipped (existing)", false)
			continue
		}

		if err := l.fs.Setenv(key, value); err != nil {
			_ = l.auditor.LogError(internal.ActionSet, key, err.Error())
			return fmt.Errorf("failed to set %s: %w", MaskKey(key), err)
		}

		_ = l.auditor.Log(internal.ActionSet, key, "applied", true)
	}

	l.applied = true
	return nil
}

// GetString retrieves a value by key with optional default.
// If the key is not found and no default is provided, returns empty string.
// Supports dot-notation path resolution for nested keys (e.g., "database.host" -> "DATABASE_HOST").
//
// Example:
//
//	value := loader.GetString("KEY")           // Returns "" if not found
//	value := loader.GetString("KEY", "default") // Returns "default" if not found
func (l *Loader) GetString(key string, defaultValue ...string) string {
	value, ok := l.Lookup(key)
	if !ok {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return value
}

// GetSecure retrieves a SecureValue by key.
// Uses the same key resolution strategy as Lookup (exact match, uppercase fallback,
// dot-notation) via internal.ResolveKeyName to ensure consistency.
func (l *Loader) GetSecure(key string) *SecureValue {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return nil
	}

	// Phase 1: find the matching storage key without allocating SecureValues.
	var matchedKey string
	internal.ResolveKeyName(key, func(k string) bool {
		if l.vars.exists(k) {
			matchedKey = k
			return true
		}
		return false
	})
	if matchedKey == "" {
		return nil
	}

	// Phase 2: allocate SecureValue only for the matched key.
	return l.vars.GetSecure(matchedKey)
}

// Lookup retrieves a value by key and reports whether it exists.
// Supports dot-notation path resolution for nested keys (e.g., "database.host" -> "DATABASE_HOST").
// For indexed access (e.g., "service.cors.origins.0"), falls back to comma-separated values
// if indexed key is not found.
// Returns the value with leading and trailing whitespace trimmed.
func (l *Loader) Lookup(key string) (string, bool) {
	if l == nil {
		return "", false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return "", false
	}

	return internal.ResolveKey(key, l.vars.Get)
}

// buildIndexedKey efficiently constructs an indexed key (e.g., "KEY_0", "KEY_1").
// It uses a stack-allocated buffer to avoid heap allocations in the common case.
//
// SECURITY: Returns empty string if the resulting key would exceed hardMaxKeyLength.
func buildIndexedKey(baseKey string, index int) string {
	// SECURITY: Check for negative index
	if index < 0 {
		return ""
	}

	// Pre-calculate required capacity for the index
	indexLen := 1
	for tmp := index; tmp >= 10; tmp /= 10 {
		indexLen++
	}

	// Calculate total length
	totalLen := len(baseKey) + 1 + indexLen

	// SECURITY: Check against hardMaxKeyLength to prevent excessively long keys
	if totalLen > internal.HardMaxKeyLength {
		return ""
	}

	// Use stack-allocated array for small keys (most common case)
	// maxStackKeyLen should be <= internal.hardMaxKeyLength
	const maxStackKeyLen = 64
	if totalLen <= maxStackKeyLen {
		var buf [maxStackKeyLen]byte
		n := copy(buf[:], baseKey)
		buf[n] = '_'
		n++
		// Append integer without allocation
		b := strconv.AppendInt(buf[n:n], int64(index), 10)
		return string(buf[:n+len(b)])
	}

	// Fallback for longer keys (but still within hardMaxKeyLength)
	var sb strings.Builder
	sb.Grow(totalLen)
	sb.WriteString(baseKey)
	sb.WriteByte('_')
	var ibuf [20]byte
	sb.Write(strconv.AppendInt(ibuf[:0], int64(index), 10))
	return sb.String()
}

// Set sets a value for a key.
func (l *Loader) Set(key, value string) error {
	if l == nil {
		return ErrClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return ErrClosed
	}

	// Validate key
	if err := l.validator.ValidateKey(key); err != nil {
		_ = l.auditor.LogError(internal.ActionSet, key, err.Error())
		return err
	}

	// Validate value
	if l.config.ValidateValues {
		if err := l.validator.ValidateValue(value); err != nil {
			_ = l.auditor.LogError(internal.ActionSet, key, err.Error())
			return err
		}
	}

	// Check overwrite policy
	// Only call Get (which allocates) when we actually need to check the policy.
	// When OverwriteExisting=true, skip the Get entirely to avoid unnecessary allocation.
	if !l.config.OverwriteExisting {
		if _, exists := l.vars.Get(key); exists {
			_ = l.auditor.Log(internal.ActionSet, key, "skipped (no overwrite)", false)
			return nil
		}
	}

	l.vars.Set(key, value)
	_ = l.auditor.Log(internal.ActionSet, key, "set", true)

	// Apply to environment if auto-apply is enabled
	if l.config.AutoApply {
		if err := l.fs.Setenv(key, value); err != nil {
			return fmt.Errorf("failed to set environment: %w", err)
		}
	}

	return nil
}

// Delete removes a key.
func (l *Loader) Delete(key string) error {
	if l == nil {
		return ErrClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return ErrClosed
	}

	l.vars.Delete(key)
	_ = l.auditor.Log(internal.ActionDelete, key, "deleted", true)

	// Remove from environment if applied
	if l.applied {
		if err := l.fs.Unsetenv(key); err != nil {
			_ = l.auditor.LogError(internal.ActionDelete, key, err.Error())
		}
	}

	return nil
}

// Keys returns all keys.
func (l *Loader) Keys() []string {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return nil
	}

	return l.vars.Keys()
}

// All returns all environment variables as a map.
func (l *Loader) All() map[string]string {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return nil
	}

	return l.vars.ToMap()
}

// Len returns the number of loaded variables.
func (l *Loader) Len() int {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return 0
	}

	return l.vars.Len()
}

// IsApplied returns true if the variables have been applied to os.Environ.
func (l *Loader) IsApplied() bool {
	if l == nil {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.applied
}

// LoadTime returns the time when variables were last loaded.
func (l *Loader) LoadTime() time.Time {
	if l == nil {
		return time.Time{}
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loadTime
}

// Close closes the loader and securely clears all stored values.
// If the loader owns its ComponentFactory, it will also close the factory.
func (l *Loader) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	l.vars.Clear()

	// Only close the factory if we own it
	// This prevents double-closing when the factory is shared
	if l.ownsFactory && l.factory != nil {
		if err := l.factory.Close(); err != nil {
			return err
		}
	}

	l.closed = true
	return nil
}

// IsClosed returns true if the loader has been closed.
func (l *Loader) IsClosed() bool {
	if l == nil {
		return true
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.closed
}

// getWithDefault is a generic helper for retrieving values with optional defaults.
// It handles the common pattern of looking up a key, parsing it, and returning
// a default value if the key is not found or parsing fails.
// Parse failures are logged to the auditor for debugging purposes.
func getWithDefault[T any](loader *Loader, key string, parse func(string) (T, error), defaultValue ...T) T {
	if loader == nil {
		return firstOrZero(defaultValue...)
	}
	value, ok := loader.Lookup(key)
	if !ok {
		return firstOrZero(defaultValue...)
	}
	result, err := parse(value)
	if err != nil {
		// Log parse failure for debugging
		_ = loader.auditor.LogError(internal.ActionGet, key, fmt.Sprintf("parse failed: %v", err))
		return firstOrZero(defaultValue...)
	}
	return result
}

// Config returns the loader's configuration.
// Note: The returned Config should be treated as read-only.
// Modifying the ValidationConfig (KeyPattern, AllowedKeys, ForbiddenKeys, RequiredKeys),
// LimitsConfig, or ComponentConfig fields may affect the loader's behavior.
// For a safe mutable copy, manually copy the necessary fields.
func (l *Loader) Config() Config {
	if l == nil {
		return Config{}
	}
	return l.config
}

// GetInt retrieves an integer value with optional default.
// If the key is not found and no default is provided, returns 0.
//
// Example:
//
//	port := loader.GetInt("PORT")           // Returns 0 if not found
//	port := loader.GetInt("PORT", 8080)     // Returns 8080 if not found
func (l *Loader) GetInt(key string, defaultValue ...int64) int64 {
	return getWithDefault(l, key, func(s string) (int64, error) {
		return parseInt(s, 64)
	}, defaultValue...)
}

// GetUint64 retrieves an unsigned integer value with optional default.
// If the key is not found and no default is provided, returns 0.
//
// Example:
//
//	port := loader.GetUint64("PORT")           // Returns 0 if not found
//	port := loader.GetUint64("PORT", 8080)     // Returns 8080 if not found
func (l *Loader) GetUint64(key string, defaultValue ...uint64) uint64 {
	return getWithDefault(l, key, func(s string) (uint64, error) {
		return parseUint(s, 64)
	}, defaultValue...)
}

// GetFloat64 retrieves a floating-point value with optional default.
// If the key is not found and no default is provided, returns 0.
//
// Example:
//
//	rate := loader.GetFloat64("RATE")           // Returns 0 if not found
//	rate := loader.GetFloat64("RATE", 0.5)      // Returns 0.5 if not found
func (l *Loader) GetFloat64(key string, defaultValue ...float64) float64 {
	return getWithDefault(l, key, parseFloat64, defaultValue...)
}

// GetBool retrieves a boolean value with optional default.
// If the key is not found and no default is provided, returns false.
//
// Example:
//
//	debug := loader.GetBool("DEBUG")           // Returns false if not found
//	debug := loader.GetBool("DEBUG", true)     // Returns true if not found
func (l *Loader) GetBool(key string, defaultValue ...bool) bool {
	return getWithDefault(l, key, parseBool, defaultValue...)
}

// GetDuration retrieves a duration value with optional default.
// If the key is not found and no default is provided, returns 0.
//
// Example:
//
//	timeout := loader.GetDuration("TIMEOUT")                  // Returns 0 if not found
//	timeout := loader.GetDuration("TIMEOUT", 30*time.Second) // Returns 30s if not found
func (l *Loader) GetDuration(key string, defaultValue ...time.Duration) time.Duration {
	return getWithDefault(l, key, parseDuration, defaultValue...)
}

// GetSliceFrom retrieves a slice of values from a loader by iterating through indexed keys.
// If the key is not found and no default is provided, returns nil.
// Supports dot-notation path resolution for nested keys.
//
// Indexed keys are searched in format: KEY_0, KEY_1, KEY_2, etc.
// Also supports comma-separated values as fallback for .env files.
//
// # Why a Function Instead of a Method?
//
// This is a generic function rather than a method because Go does not support
// type parameters on methods. The pattern is:
//
//	// Method approach (not possible in Go):
//	// loader.GetSlice[int]("PORTS")  // ❌ Compile error
//
//	// Function approach (current implementation):
//	env.GetSliceFrom[int](loader, "PORTS")  // ✓ Works
//
// For package-level usage without a loader instance, use GetSlice[T]().
//
// Example:
//
//	ports := env.GetSliceFrom[int](loader, "PORTS")           // Returns []int{8080, 8081} from PORTS_0, PORTS_1
//	hosts := env.GetSliceFrom[string](loader, "HOSTS", []string{"localhost"}) // With default
func GetSliceFrom[T sliceElement](loader *Loader, key string, defaultValue ...[]T) []T {
	// Fast path for nil loader
	if loader == nil {
		return firstOrZero(defaultValue...)
	}

	loader.mu.RLock()
	defer loader.mu.RUnlock()

	// Return default if closed
	if loader.closed {
		return firstOrZero(defaultValue...)
	}

	// GetString candidate keys from path resolver (handles dot-notation)
	candidates := internal.ResolvePath(key)

	// Try each candidate in priority order
	for _, baseKey := range candidates {
		result := getSliceFromIndexedKeys[T](loader, baseKey, defaultValue)
		if len(result) > 0 {
			return result
		}
	}

	// No indexed keys found, return default or nil
	return firstOrZero(defaultValue...)
}

// getSliceFromIndexedKeys tries to get a slice from indexed keys for a specific base key.
func getSliceFromIndexedKeys[T sliceElement](loader *Loader, baseKey string, defaultValue [][]T) []T {
	// Collect values from indexed keys: KEY_0, KEY_1, KEY_2, ...
	// SECURITY: Add maximum slice size limit to prevent DoS via infinite loop
	// This is consistent with hardMaxVariables (10000) from internal/limits.go
	const maxSliceSize = 10000

	var result []T
	for i := 0; i < maxSliceSize; i++ {
		indexedKey := buildIndexedKey(baseKey, i)
		value, ok := loader.vars.Get(indexedKey)
		if !ok {
			break
		}

		parsed, err := parseSliceElement[T](value)
		if err != nil {
			// Log parse failure for debugging and skip this element
			_ = loader.auditor.LogError(internal.ActionGet, indexedKey,
				fmt.Sprintf("slice element parse failed: %v", err))
			continue
		}
		result = append(result, parsed)
	}

	// SECURITY: Log if we hit the slice size limit (potential DoS attempt)
	if len(result) >= maxSliceSize {
		_ = loader.auditor.LogError(internal.ActionGet, baseKey,
			fmt.Sprintf("slice size limit reached (%d elements)", maxSliceSize))
	}

	// If no indexed keys found, try comma-separated value
	if len(result) == 0 {
		if value, ok := loader.vars.Get(baseKey); ok {
			return parseCommaSeparated[T](value, defaultValue...)
		}
	}

	// Return default only if we collected nothing and have a default
	if len(result) == 0 && len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return result
}

// ParseInto populates a struct from loaded environment variables.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Optional `envDefault:"value"` sets a default if the key is not found.
func (l *Loader) ParseInto(v interface{}) error {
	if l == nil {
		return ErrClosed
	}
	return UnmarshalInto(l.All(), v)
}

// Validate validates the loaded environment against required and allowed keys.
func (l *Loader) Validate() error {
	if l == nil {
		return ErrClosed
	}
	return l.validator.ValidateRequired(l.keysToUpper())
}

// keysToUpper returns all keys as uppercase for comparison.
func (l *Loader) keysToUpper() map[string]bool {
	keys := l.Keys()
	result := make(map[string]bool, len(keys))
	for _, k := range keys {
		result[internal.ToUpperASCII(k)] = true
	}
	return result
}

// validateFilePath validates a file path for security.
// It checks for path traversal attempts and other potentially dangerous patterns.
// Note: This function delegates to internal.PathValidator for the actual validation.
func validateFilePath(filename string) error {
	return internal.ValidateFilePath(filename)
}
