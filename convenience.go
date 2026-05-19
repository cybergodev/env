package env

import (
	"time"
)

// ============================================================================
// Package-level Convenience Functions (Global Mode)
// ============================================================================
//
// This file implements the Global Mode API — a set of package-level functions
// that delegate to a singleton Loader instance. These functions provide the
// simplest way to use the library for applications with a single configuration.
//
// # Usage Pattern
//
// Call Load() or LoadWithConfig() once at startup, then use the getter/setter
// functions freely throughout the application:
//
//	err := env.Load(".env")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	port := env.GetInt("PORT", 8080)
//	host := env.GetString("DATABASE_HOST", "localhost")
//
// # Relationship to Instance Mode
//
// Every package-level function is a thin proxy over the corresponding Loader
// method. The two modes share identical method signatures:
//
//	env.GetString("KEY")        // Global Mode
//	loader.GetString("KEY")     // Instance Mode
//
// Use Global Mode when you need a single, application-wide configuration.
// Use Instance Mode (New + Loader methods) for tests, multiple configs, or
// explicit lifecycle control. See env.go package documentation for details.
//
// # Uninitialized Behavior
//
// If no loader has been set (Load/LoadWithConfig not called), the functions
// behave as follows:
//   - Get* functions: return the provided default value (or zero value)
//   - Lookup: returns ("", false)
//   - Keys/All/Len/GetSecure: return nil/0
//   - Set/Delete/Validate/ParseInto: return ErrNotInitialized
//
// ============================================================================

// withLoader executes a function with the default loader.
// If the loader cannot be obtained, returns the provided default value.
// This helper reduces boilerplate in the convenience functions.
func withLoader[T any](fn func(*Loader) T, def T) T {
	loader, err := getDefaultLoader()
	if err != nil {
		return def
	}
	return fn(loader)
}

// GetString retrieves a value from the default loader with optional default.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns an empty string if the key is not found and no default is provided.
// Returns the provided default value (or zero value) if no loader is initialized.
// Use Lookup to distinguish between "not found" and "empty value".
//
// Example:
//
//	env.Load(".env")
//	value := env.GetString("KEY")           // Returns "" if not found
//	value := env.GetString("KEY", "default") // Returns "default" if not found
func GetString(key string, defaultValue ...string) string {
	return withLoader(func(l *Loader) string {
		return l.GetString(key, defaultValue...)
	}, firstOrZero(defaultValue...))
}

// GetInt retrieves an integer value from the default loader with optional default.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns 0 if the key is not found and no default is provided.
// Returns the provided default value (or zero value) if no loader is initialized.
//
// Example:
//
//	env.Load(".env")
//	port := env.GetInt("PORT")           // Returns 0 if not found
//	port := env.GetInt("PORT", 8080)     // Returns 8080 if not found
func GetInt(key string, defaultValue ...int64) int64 {
	return withLoader(func(l *Loader) int64 {
		return l.GetInt(key, defaultValue...)
	}, firstOrZero(defaultValue...))
}

// GetBool retrieves a boolean value from the default loader with optional default.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns false if the key is not found and no default is provided.
// Returns the provided default value (or zero value) if no loader is initialized.
//
// Example:
//
//	env.Load(".env")
//	debug := env.GetBool("DEBUG")           // Returns false if not found
//	debug := env.GetBool("DEBUG", true)     // Returns true if not found
func GetBool(key string, defaultValue ...bool) bool {
	return withLoader(func(l *Loader) bool {
		return l.GetBool(key, defaultValue...)
	}, firstOrZero(defaultValue...))
}

// GetUint64 retrieves an unsigned integer value from the default loader with optional default.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns 0 if the key is not found and no default is provided.
// Returns the provided default value (or zero value) if no loader is initialized.
//
// Example:
//
//	env.Load(".env")
//	port := env.GetUint64("PORT")           // Returns 0 if not found
//	port := env.GetUint64("PORT", 8080)     // Returns 8080 if not found
func GetUint64(key string, defaultValue ...uint64) uint64 {
	return withLoader(func(l *Loader) uint64 {
		return l.GetUint64(key, defaultValue...)
	}, firstOrZero(defaultValue...))
}

// GetFloat64 retrieves a floating-point value from the default loader with optional default.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns 0 if the key is not found and no default is provided.
// Returns the provided default value (or zero value) if no loader is initialized.
//
// Example:
//
//	env.Load(".env")
//	rate := env.GetFloat64("RATE")           // Returns 0 if not found
//	rate := env.GetFloat64("RATE", 0.5)      // Returns 0.5 if not found
func GetFloat64(key string, defaultValue ...float64) float64 {
	return withLoader(func(l *Loader) float64 {
		return l.GetFloat64(key, defaultValue...)
	}, firstOrZero(defaultValue...))
}

// GetDuration retrieves a duration value from the default loader with optional default.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns 0 if the key is not found and no default is provided.
// Returns the provided default value (or zero value) if no loader is initialized.
//
// Example:
//
//	env.Load(".env")
//	timeout := env.GetDuration("TIMEOUT")                  // Returns 0 if not found
//	timeout := env.GetDuration("TIMEOUT", 30*time.Second) // Returns 30s if not found
func GetDuration(key string, defaultValue ...time.Duration) time.Duration {
	return withLoader(func(l *Loader) time.Duration {
		return l.GetDuration(key, defaultValue...)
	}, firstOrZero(defaultValue...))
}

// Lookup retrieves a value and existence from the default loader.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns ("", false) if no loader is initialized.
//
// Example:
//
//	env.Load(".env")
//	value, exists := env.Lookup("DATABASE_URL")
//	if !exists {
//	    log.Fatal("DATABASE_URL is required")
//	}
func Lookup(key string) (string, bool) {
	loader, err := getDefaultLoader()
	if err != nil {
		return "", false
	}
	return loader.Lookup(key)
}

// Set sets a value in the default loader.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns ErrNotInitialized if no loader is initialized.
//
// Example:
//
//	env.Load(".env")
//	if err := env.Set("DEBUG", "true"); err != nil {
//	    log.Fatal(err)
//	}
func Set(key, value string) error {
	loader, err := getDefaultLoader()
	if err != nil {
		return err
	}
	return loader.Set(key, value)
}

// GetSlice retrieves a slice of values from the default loader.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns nil if the key is not found and no default is provided.
// Returns nil if no loader is initialized.
//
// Indexed keys are searched in format: KEY_0, KEY_1, KEY_2, etc.
// Also supports comma-separated values as fallback for .env files.
//
// For Instance Mode, use GetSliceFrom[T](loader, key) instead.
// See GetSliceFrom documentation for details on why it is a function, not a method.
//
// Example:
//
//	ports := env.GetSlice[int]("PORTS")
//	hosts := env.GetSlice[string]("HOSTS", []string{"localhost"})
func GetSlice[T sliceElement](key string, defaultValue ...[]T) []T {
	loader, err := getDefaultLoader()
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return nil
	}
	return GetSliceFrom[T](loader, key, defaultValue...)
}

// Keys returns all keys from the default loader.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns nil if no loader is initialized.
//
// Example:
//
//	for _, key := range env.Keys() {
//	    fmt.Printf("%s = %s\n", key, env.GetString(key))
//	}
func Keys() []string {
	return withLoader((*Loader).Keys, nil)
}

// All returns all environment variables from the default loader as a map.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns nil if no loader is initialized.
//
// Example:
//
//	vars := env.All()
//	for key, value := range vars {
//	    fmt.Printf("%s = %s\n", key, value)
//	}
func All() map[string]string {
	return withLoader((*Loader).All, nil)
}

// Len returns the number of loaded variables from the default loader.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns 0 if no loader is initialized.
//
// Example:
//
//	count := env.Len()
//	fmt.Printf("Loaded %d environment variables\n", count)
func Len() int {
	return withLoader((*Loader).Len, 0)
}

// Delete removes a key from the default loader.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns ErrNotInitialized if no loader is initialized.
//
// Example:
//
//	if err := env.Delete("TEMP_VAR"); err != nil {
//	    log.Fatal(err)
//	}
func Delete(key string) error {
	loader, err := getDefaultLoader()
	if err != nil {
		return err
	}
	return loader.Delete(key)
}

// GetSecure retrieves a SecureValue from the default loader.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns nil if the key is not found or no loader is initialized.
// Use GetSecure for sensitive values that need secure memory handling.
//
// Example:
//
//	sv := env.GetSecure("API_KEY")
//	if sv != nil {
//	    fmt.Println(sv.Masked()) // Safe for logging
//	    data := sv.Bytes()
//	    defer env.ClearBytes(data)
//	    // Use data securely
//	}
func GetSecure(key string) *SecureValue {
	return withLoader(func(l *Loader) *SecureValue {
		return l.GetSecure(key)
	}, nil)
}

// Validate validates the default loader against required and allowed keys.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns ErrNotInitialized if no loader is initialized, or validation errors.
//
// Example:
//
//	if err := env.Validate(); err != nil {
//	    log.Fatal("Environment validation failed:", err)
//	}
func Validate() error {
	loader, err := getDefaultLoader()
	if err != nil {
		return err
	}
	return loader.Validate()
}

// ============================================================================
// Quick Load Functions
// ============================================================================

// Load initializes the default loader with the given files.
// This function MUST be called before using any package-level convenience functions
// (GetString, GetInt, GetBool, etc.).
//
// IMPORTANT: This function:
//  1. Sets the package-level default loader (used by GetString, GetInt, etc.)
//  2. Auto-applies all loaded variables to os.Environ
//
// For isolated instances without auto-apply, use New().
//
// Files are loaded sequentially; later files can override values from earlier files.
//
// Supported file formats:
//   - .env files (dotenv format)
//   - .json files (JSON format with nested structure)
//   - .yaml/.yml files (YAML format with nested structure)
//
// For JSON/YAML files, nested values are flattened and can be accessed using dot-notation:
//
//	// config.json: {"database": {"host": "localhost", "port": 5432}}
//	env.Load("config.json")
//	host := env.GetString("database.host") // "localhost"
//	port := env.GetInt("database.port")    // 5432
//
// Returns ErrAlreadyInitialized if the default loader has already been initialized.
//
// Example:
//
//	// Initialize with default .env file
//	if err := env.Load(); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Initialize with multiple files
//	if err := env.Load(".env", ".env.local"); err != nil {
//	    log.Fatal(err)
//	}
//	// Now use package-level functions
//	port := env.GetInt("PORT", 8080)
func Load(filenames ...string) error {
	cfg := DefaultConfig()
	cfg.Filenames = filenames
	cfg.AutoApply = true

	loader, err := New(cfg)
	if err != nil {
		return err
	}

	// Set as default loader for package-level functions
	if err := setDefaultLoader(loader); err != nil {
		_ = loader.Close() // best-effort cleanup; error not actionable
		return err
	}

	return nil
}

// LoadWithConfig initializes the default loader with a custom configuration.
// This function MUST be called before using any package-level convenience functions
// (GetString, GetInt, GetBool, etc.).
//
// IMPORTANT: This function:
//  1. Sets the package-level default loader (used by GetString, GetInt, etc.)
//  2. Forces AutoApply=true regardless of cfg.AutoApply value
//
// For isolated instances without setting the default, use New().
//
// Returns ErrAlreadyInitialized if the default loader has already been initialized.
//
// Example:
//
//	cfg := env.DefaultConfig()
//	cfg.Filenames = []string{".env.production"}
//	cfg.OverwriteExisting = true
//	if err := env.LoadWithConfig(cfg); err != nil {
//	    log.Fatal(err)
//	}
func LoadWithConfig(cfg Config) error {
	// Force AutoApply: convenience functions (GetString, etc.) require
	// variables applied to os.Environ. Use New() for manual control.
	cfg.AutoApply = true
	loader, err := New(cfg)
	if err != nil {
		return err
	}

	// Set as default loader for package-level functions
	if err := setDefaultLoader(loader); err != nil {
		_ = loader.Close()
		return err
	}

	return nil
}

// ParseInto populates a struct from the default loader's environment variables.
// Requires Load() or LoadWithConfig() to have been called first.
// Returns ErrNotInitialized if no loader is initialized.
// Struct fields can be tagged with `env:"KEY"` to specify the env variable name.
// Optional `envDefault:"value"` sets a default if the key is not found.
//
// This function automatically reads all loaded environment variables and maps
// them to struct fields based on the `env` tags. It eliminates the need to
// manually build a data map before calling UnmarshalStruct.
//
// Example:
//
//	type Config struct {
//	    Host string `env:"DB_HOST"`
//	    Port int    `env:"DB_PORT,envDefault:5432"`
//	    Debug bool  `env:"DEBUG,envDefault:false"`
//	}
//
//	if err := env.Load(".env"); err != nil {
//	    log.Fatal(err)
//	}
//
//	var cfg Config
//	if err := env.ParseInto(&cfg); err != nil {
//	    log.Fatal(err)
//	}
func ParseInto(v any) error {
	loader, err := getDefaultLoader()
	if err != nil {
		return err
	}
	return loader.ParseInto(v)
}
