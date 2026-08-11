package env

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Value Accessors
// ============================================================================

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
		_ = loader.factory.Auditor().LogError(internal.ActionGet, key, fmt.Sprintf("parse failed: %v", err))
		return firstOrZero(defaultValue...)
	}
	return result
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

// Lookup retrieves a value by key and reports whether it exists.
// Supports dot-notation path resolution for nested keys (e.g., "database.host" -> "DATABASE_HOST").
// For indexed access (e.g., "service.cors.origins.0"), falls back to comma-separated values
// if indexed key is not found.
// Returns the value with leading and trailing whitespace trimmed.
func (l *Loader) Lookup(key string) (string, bool) {
	if err := l.enterRead(); err != nil {
		return "", false
	}
	defer l.exitRead()

	// Fast path: simple keys (no dots) — inline the lookup to avoid the
	// function-value indirection of ResolveKey(key, l.vars.Get).
	// This lets the compiler emit a direct method call to secureMap.Get.
	if strings.IndexByte(key, '.') == -1 {
		if value, ok := l.vars.Get(key); ok {
			return value, true
		}
		upper := internal.ToUpperASCII(key)
		if upper != key {
			if value, ok := l.vars.Get(upper); ok {
				return value, true
			}
		}
		return "", false
	}

	// Dot-notation: delegate to ResolveKey for path expansion.
	return internal.ResolveKey(key, l.vars.Get)
}

// GetSecure retrieves a SecureValue by key.
// Uses the same key resolution strategy as Lookup (exact match, uppercase fallback,
// dot-notation) via internal.ResolveKeyName to ensure consistency.
func (l *Loader) GetSecure(key string) *SecureValue {
	if err := l.enterRead(); err != nil {
		return nil
	}
	defer l.exitRead()

	// Single-pass resolution: find key and allocate SecureValue atomically
	// to avoid TOCTOU race between exists() and GetSecure().
	var result *SecureValue
	internal.ResolveKeyName(key, func(k string) bool {
		if sv := l.vars.GetSecure(k); sv != nil {
			result = sv
			return true
		}
		return false
	})
	return result
}

// ============================================================================
// Metadata Accessors
// ============================================================================

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

// Keys returns all keys.
func (l *Loader) Keys() []string {
	if err := l.enterRead(); err != nil {
		return nil
	}
	defer l.exitRead()

	return l.vars.Keys()
}

// All returns all environment variables as a map.
func (l *Loader) All() map[string]string {
	if err := l.enterRead(); err != nil {
		return nil
	}
	defer l.exitRead()

	return l.vars.ToMap()
}

// Len returns the number of loaded variables.
func (l *Loader) Len() int {
	if err := l.enterRead(); err != nil {
		return 0
	}
	defer l.exitRead()

	return l.vars.Len()
}

// IsApplied returns true if the variables have been applied to os.Environ.
func (l *Loader) IsApplied() bool {
	if err := l.enterRead(); err != nil {
		return false
	}
	defer l.exitRead()
	return l.applied
}

// LoadTime returns the time when variables were last loaded.
func (l *Loader) LoadTime() time.Time {
	if err := l.enterRead(); err != nil {
		return time.Time{}
	}
	defer l.exitRead()
	return l.loadTime
}

// ============================================================================
// Typed Getters
// ============================================================================

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

// ============================================================================
// Slice Access
// ============================================================================

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
	// maxStackKeyLen should be <= internal.HardMaxKeyLength
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

// GetSliceFrom retrieves a slice of values from a loader by iterating through indexed keys.
// If the key is not found and no default is provided, returns nil.
// Supports dot-notation path resolution for nested keys.
//
// Indexed keys are searched in format: KEY_0, KEY_1, KEY_2, etc.
// Also supports comma-separated values as fallback for .env files.
//
// Type parameter T is constrained to: string, int, int64, uint, uint64, bool, float64, time.Duration.
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

	if err := loader.enterRead(); err != nil {
		// nil or closed loader: behave like a miss
		return firstOrZero(defaultValue...)
	}
	defer loader.exitRead()

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
	for i := range maxSliceSize {
		indexedKey := buildIndexedKey(baseKey, i)
		value, ok := loader.vars.Get(indexedKey)
		if !ok {
			break
		}

		parsed, err := parseSliceElement[T](value)
		if err != nil {
			// Log parse failure for debugging and skip this element
			_ = loader.factory.Auditor().LogError(internal.ActionGet, indexedKey,
				fmt.Sprintf("slice element parse failed: %v", err))
			continue
		}
		result = append(result, parsed)
	}

	// SECURITY: Log if we hit the slice size limit (potential DoS attempt)
	if len(result) >= maxSliceSize {
		_ = loader.factory.Auditor().LogError(internal.ActionGet, baseKey,
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
