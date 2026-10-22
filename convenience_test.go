package env

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// ============================================================================
// Test Helpers
// ============================================================================

// setupTestLoader creates a new Loader with default config and sets it as the default loader.
// It automatically resets the default loader when the test completes.
func setupTestLoader(t *testing.T) *Loader {
	t.Helper()
	resetDefaultLoader()
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = ResetDefaultLoader() })
	return loader
}

// resetDefaultLoader resets the package default loader for test isolation,
// discarding the error: it is only the old loader's Close result, which
// tests do not act on.
func resetDefaultLoader() {
	_ = ResetDefaultLoader()
}

// ============================================================================
// Convenience Getter Tests (Table-Driven)
// ============================================================================

func TestConvenienceGet(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string // stored value; empty means the key is not set
		call  func(key string) any
		want  any
	}{
		// String
		{"get string existing", "KEY", "value", func(key string) any { return GetString(key) }, "value"},
		{"get string with default", "MISSING", "", func(key string) any { return GetString(key, "default_value") }, "default_value"},
		{"get string missing no default", "MISSING", "", func(key string) any { return GetString(key) }, ""},

		// Int
		{"get int existing", "PORT", "8080", func(key string) any { return GetInt(key) }, int64(8080)},
		{"get int with default", "MISSING", "", func(key string) any { return GetInt(key, 9999) }, int64(9999)},
		{"get int missing no default", "NOT_EXISTS", "", func(key string) any { return GetInt(key) }, int64(0)},
		// A stored value that fails to parse falls back to the default — or to
		// the zero value when none is given (getWithDefault in accessors.go).
		{"get int invalid stored value falls back to default", "BAD", "abc", func(key string) any { return GetInt(key, 9999) }, int64(9999)},
		{"get int invalid stored value no default is zero", "BAD", "abc", func(key string) any { return GetInt(key) }, int64(0)},
		// firstOrZero (utils.go) takes the FIRST variadic default, ignoring
		// the rest.
		{"get int multiple defaults uses the first", "MISSING", "", func(key string) any { return GetInt(key, 1, 2) }, int64(1)},

		// Bool
		{"get bool true", "DEBUG", "true", func(key string) any { return GetBool(key) }, true},
		{"get bool false", "DEBUG", "false", func(key string) any { return GetBool(key) }, false},
		{"get bool with default", "MISSING", "", func(key string) any { return GetBool(key, true) }, true},
		{"get bool missing no default", "NOT_EXISTS", "", func(key string) any { return GetBool(key) }, false},
		{"get bool invalid stored value falls back to default", "BAD", "maybe", func(key string) any { return GetBool(key, true) }, true},

		// Duration
		{"get duration existing", "TIMEOUT", "30s", func(key string) any { return GetDuration(key) }, 30 * time.Second},
		{"get duration with default", "MISSING", "", func(key string) any { return GetDuration(key, 5*time.Minute) }, 5 * time.Minute},
		{"get duration missing no default", "NOT_EXISTS", "", func(key string) any { return GetDuration(key) }, time.Duration(0)},
		{"get duration invalid stored value falls back to default", "BAD", "not_a_duration", func(key string) any { return GetDuration(key, 5*time.Minute) }, 5 * time.Minute},

		// Uint64
		{"get uint64 existing", "PORT", "8080", func(key string) any { return GetUint64(key) }, uint64(8080)},
		{"get uint64 with default", "MISSING", "", func(key string) any { return GetUint64(key, 9999) }, uint64(9999)},
		{"get uint64 missing no default", "NOT_EXISTS", "", func(key string) any { return GetUint64(key) }, uint64(0)},
		{"get uint64 invalid stored value falls back to default", "BAD", "abc", func(key string) any { return GetUint64(key, 9999) }, uint64(9999)},

		// Float64
		{"get float64 existing", "RATE", "3.14", func(key string) any { return GetFloat64(key) }, 3.14},
		{"get float64 with default", "MISSING", "", func(key string) any { return GetFloat64(key, 0.5) }, 0.5},
		{"get float64 missing no default", "NOT_EXISTS", "", func(key string) any { return GetFloat64(key) }, 0.0},
		{"get float64 invalid stored value falls back to default", "BAD", "abc", func(key string) any { return GetFloat64(key, 0.5) }, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := setupTestLoader(t)

			// Set value if provided
			if tt.value != "" {
				if err := loader.Set(tt.key, tt.value); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}

			if err := setDefaultLoader(loader); err != nil {
				t.Fatalf("setDefaultLoader() error = %v", err)
			}

			if got := tt.call(tt.key); got != tt.want {
				t.Errorf("call(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestConvenienceLookup(t *testing.T) {
	loader := setupTestLoader(t)

	if err := loader.Set("KEY", "value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Test Lookup with existing value
	value, ok := Lookup("KEY")
	if !ok {
		t.Error("Lookup(\"KEY\") ok = false, want true")
	}
	if value != "value" {
		t.Errorf("Lookup(\"KEY\") = %q, want %q", value, "value")
	}

	// Test Lookup with missing key
	_, ok = Lookup("MISSING")
	if ok {
		t.Error("Lookup(\"MISSING\") ok = true, want false")
	}

	// Empty key is a miss, not an error
	t.Run("empty key is a miss", func(t *testing.T) {
		if value, ok := Lookup(""); ok || value != "" {
			t.Errorf("Lookup(\"\") = (%q, %v), want (\"\", false)", value, ok)
		}
	})
}

func TestConvenienceSet(t *testing.T) {
	loader := setupTestLoader(t)

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Test Set
	if err := Set("KEY", "value"); err != nil {
		t.Errorf("Set() error = %v", err)
	}

	// Verify the value was set
	value, ok := Lookup("KEY")
	if !ok {
		t.Error("Set should set the key")
	}
	if value != "value" {
		t.Errorf("Set() value = %q, want %q", value, "value")
	}
}

func TestConvenienceNoLoader(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

	// With no default loader configured, every package-level accessor returns
	// the caller-supplied default, or its zero value when none is given.
	withDefault := []struct {
		name string
		call func() any
		want any
	}{
		{"GetString", func() any { return GetString("KEY", "default") }, "default"},
		{"GetInt", func() any { return GetInt("KEY", 123) }, int64(123)},
		{"GetBool", func() any { return GetBool("KEY", true) }, true},
		{"GetDuration", func() any { return GetDuration("KEY", 10*time.Second) }, 10 * time.Second},
		{"GetUint64", func() any { return GetUint64("KEY", 999) }, uint64(999)},
		{"GetFloat64", func() any { return GetFloat64("KEY", 3.14) }, float64(3.14)},
	}
	for _, tt := range withDefault {
		t.Run(tt.name+"/default", func(t *testing.T) {
			if got := tt.call(); got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.name, got, tt.want)
			}
		})
	}

	withoutDefault := []struct {
		name string
		call func() any
		want any
	}{
		{"GetString", func() any { return GetString("KEY") }, ""},
		{"GetInt", func() any { return GetInt("KEY") }, int64(0)},
		{"GetBool", func() any { return GetBool("KEY") }, false},
		{"GetUint64", func() any { return GetUint64("KEY") }, uint64(0)},
		{"GetFloat64", func() any { return GetFloat64("KEY") }, float64(0)},
		{"GetDuration", func() any { return GetDuration("KEY") }, time.Duration(0)},
	}
	for _, tt := range withoutDefault {
		t.Run(tt.name+"/zero", func(t *testing.T) {
			if got := tt.call(); got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.name, got, tt.want)
			}
		})
	}

	t.Run("GetSlice returns nil", func(t *testing.T) {
		if got := GetSlice[string]("KEY"); got != nil {
			t.Errorf("GetSlice() with no loader = %v, want nil", got)
		}
	})

	// From the former TestGetSlice_NoLoaderReturnsDefault: the caller's
	// default comes back verbatim without a loader.
	t.Run("GetSlice returns the default", func(t *testing.T) {
		got := GetSlice[string]("KEY", []string{"fallback"})
		if !sliceEqual(got, []string{"fallback"}) {
			t.Errorf("GetSlice[string]() with no loader = %v, want [fallback]", got)
		}
	})

	t.Run("Lookup returns empty and false", func(t *testing.T) {
		if value, ok := Lookup("KEY"); ok || value != "" {
			t.Errorf("Lookup() with no loader = (%q, %v), want (\"\", false)", value, ok)
		}
	})

	t.Run("Keys returns nil", func(t *testing.T) {
		if keys := Keys(); keys != nil {
			t.Errorf("Keys() with no loader = %v, want nil", keys)
		}
	})

	t.Run("All returns nil", func(t *testing.T) {
		if all := All(); all != nil {
			t.Errorf("All() with no loader = %v, want nil", all)
		}
	})

	t.Run("Len returns zero", func(t *testing.T) {
		if count := Len(); count != 0 {
			t.Errorf("Len() with no loader = %d, want 0", count)
		}
	})

	t.Run("GetSecure returns nil", func(t *testing.T) {
		if sv := GetSecure("KEY"); sv != nil {
			t.Errorf("GetSecure() with no loader = %v, want nil", sv)
		}
	})

	notInitialized := []struct {
		name string
		call func() error
	}{
		{"Set", func() error { return Set("KEY", "value") }},
		{"Delete", func() error { return Delete("KEY") }},
		{"Validate", func() error { return Validate() }},
	}
	for _, tt := range notInitialized {
		t.Run(tt.name+"/ErrNotInitialized", func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrNotInitialized) {
				t.Errorf("%s() with no loader error = %v, want ErrNotInitialized", tt.name, err)
			}
		})
	}

	t.Run("ParseInto/ErrNotInitialized", func(t *testing.T) {
		type Config struct {
			Host string `env:"DB_HOST"`
		}
		var c Config
		if err := ParseInto(&c); !errors.Is(err, ErrNotInitialized) {
			t.Errorf("ParseInto with no loader error = %v, want ErrNotInitialized", err)
		}
	})
}

// TestGetSlice_WithDefaultLoader pins the WITH-loader branch of the
// package-level GetSlice (convenience.go): with an initialized default
// loader, the indexed key family is resolved against it.
func TestGetSlice_WithDefaultLoader(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup
	for _, kv := range [][2]string{{"HOSTS_0", "a"}, {"HOSTS_1", "b"}} {
		if err := loader.Set(kv[0], kv[1]); err != nil {
			t.Fatalf("Set(%q) error = %v", kv[0], err)
		}
	}
	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	got := GetSlice[string]("HOSTS")
	if !sliceEqual(got, []string{"a", "b"}) {
		t.Errorf("GetSlice[string](HOSTS) = %v, want [a b]", got)
	}
}

// ============================================================================
// Load Function Tests
// ============================================================================

// TestLoadFiles_ThroughDefaultLoader consolidates the load-path variants
// (dotenv, custom filename, default filename, JSON, YAML) into one table:
// every row loads files, installs the loader as default, and verifies the
// values through the package-level accessors.
func TestLoadFiles_ThroughDefaultLoader(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		cfgFn func() Config
		// loadArgs are passed to LoadFiles; nil means "use the default filenames".
		loadArgs []string
		verify   func(t *testing.T)
	}{
		{
			// One row for both dotenv wiring paths: loadArgs nil means
			// LoadFiles() with no arguments, which defaults to ".env" — the
			// explicit-arguments path is exercised by the rows below.
			name:  "dotenv file via the default filename",
			files: map[string]string{".env": "LOAD_KEY=load_value\nDEFAULT_KEY=default_value"},
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.AutoApply = true
				return cfg
			},
			loadArgs: nil,
			verify: func(t *testing.T) {
				if got := GetString("LOAD_KEY"); got != "load_value" {
					t.Errorf("GetString(\"LOAD_KEY\") = %q, want %q", got, "load_value")
				}
				if got := GetString("DEFAULT_KEY"); got != "default_value" {
					t.Errorf("GetString(\"DEFAULT_KEY\") = %q, want %q", got, "default_value")
				}
			},
		},
		{
			name:  "custom filename with TestingConfig",
			files: map[string]string{"custom.env": "CUSTOM_KEY=custom_value"},
			cfgFn: func() Config {
				cfg := TestingConfig()
				cfg.Filenames = []string{"custom.env"}
				return cfg
			},
			loadArgs: []string{"custom.env"},
			verify: func(t *testing.T) {
				if got := GetString("CUSTOM_KEY"); got != "custom_value" {
					t.Errorf("GetString(\"CUSTOM_KEY\") = %q, want %q", got, "custom_value")
				}
			},
		},
		{
			name:  "multiple keys with typed accessors",
			files: map[string]string{"init.env": "INIT_KEY=init_value\nPORT=3000"},
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.Filenames = []string{"init.env"}
				return cfg
			},
			loadArgs: []string{"init.env"},
			verify: func(t *testing.T) {
				if v := GetString("INIT_KEY"); v != "init_value" {
					t.Errorf("GetString(INIT_KEY) = %q, want %q", v, "init_value")
				}
				if v := GetInt("PORT", 0); v != 3000 {
					t.Errorf("GetInt(PORT) = %d, want %d", v, 3000)
				}
			},
		},
		{
			// Wiring-only row: the structured file reaches the package-level
			// accessors flattened and uppercased. The full flatten semantics
			// (nesting, arrays, collisions) are covered by the edge-case
			// tables in loader_test.go.
			name:  "json file wiring: flattened and uppercased",
			files: map[string]string{"config.json": `{"database": {"host": "db.example.com", "port": 3306}}`},
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.AutoApply = true
				return cfg
			},
			loadArgs: []string{"config.json"},
			verify: func(t *testing.T) {
				if got := GetString("DATABASE_HOST"); got != "db.example.com" {
					t.Errorf("GetString(\"DATABASE_HOST\") = %q, want %q", got, "db.example.com")
				}
			},
		},
		{
			// Wiring-only row, YAML counterpart of the JSON row above.
			name:  "yaml file wiring: flattened and uppercased",
			files: map[string]string{"config.yaml": "server:\n  host: yaml.example.com\n  port: 8443"},
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.AutoApply = true
				return cfg
			},
			loadArgs: []string{"config.yaml"},
			verify: func(t *testing.T) {
				if got := GetString("SERVER_HOST"); got != "yaml.example.com" {
					t.Errorf("GetString(\"SERVER_HOST\") = %q, want %q", got, "yaml.example.com")
				}
			},
		},
		{
			// Regression: keys containing literal dots (stored verbatim, e.g.
			// "APNEWS.COM") must be reachable through Lookup, not only via
			// All(). The value is a cookie-style string whose embedded JSON
			// contains quotes, braces and commas — all plain-scalar data.
			name:  "yaml dotted key with cookie-style value",
			files: map[string]string{"config.yaml": "apnews.com: Code=scivqvknmzlrlloi; Visitor=scivqvknmzlrlloi; g_state={\"i_l\":0,\"i_e\":{\"enable_itp_optimization\":24},\"i_t\":1792402790803}"},
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.AutoApply = true
				return cfg
			},
			loadArgs: []string{"config.yaml"},
			verify: func(t *testing.T) {
				want := `Code=scivqvknmzlrlloi; Visitor=scivqvknmzlrlloi; g_state={"i_l":0,"i_e":{"enable_itp_optimization":24},"i_t":1792402790803}`
				if got := GetString("apnews.com"); got != want {
					t.Errorf("GetString(\"apnews.com\") = %q, want %q", got, want)
				}
				if got := GetString("APNEWS.COM"); got != want {
					t.Errorf("GetString(\"APNEWS.COM\") = %q, want %q", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetDefaultLoader()
			defer resetDefaultLoader()

			fs := newTestFileSystem()
			for name, content := range tt.files {
				fs.files[name] = content
			}

			cfg := tt.cfgFn()
			cfg.FileSystem = fs

			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if err := loader.LoadFiles(tt.loadArgs...); err != nil {
				t.Fatalf("LoadFiles() error = %v", err)
			}

			if err := setDefaultLoader(loader); err != nil {
				t.Fatalf("setDefaultLoader() error = %v", err)
			}

			tt.verify(t)
		})
	}
}

// ============================================================================
// ParseInto Function Tests
// ============================================================================

// TestParseInto consolidates the package-level ParseInto variants in one
// table: plain env tags, inline envDefault fallbacks, and lowercase/dotted
// tag resolution.
func TestParseInto(t *testing.T) {
	type appConfig struct {
		Host  string `env:"APP_HOST"`
		Port  int    `env:"APP_PORT"`
		Debug bool   `env:"APP_DEBUG"`
	}
	type dbConfig struct {
		Host string `env:"DB_HOST,envDefault:localhost"`
		Port int    `env:"DB_PORT,envDefault:5432"`
	}
	type taggedConfig struct {
		APIKey string `env:"deepseek_key"`
		DBHost string `env:"database.host"`
	}

	tests := []struct {
		name      string
		vars      map[string]string
		newTarget func() any
		check     func(t *testing.T, v any)
	}{
		{
			name: "typed fields from env tags",
			vars: map[string]string{"APP_HOST": "localhost", "APP_PORT": "8080", "APP_DEBUG": "true"},
			newTarget: func() any {
				return &appConfig{}
			},
			check: func(t *testing.T, v any) {
				c := v.(*appConfig)
				if c.Host != "localhost" {
					t.Errorf("Host = %q, want %q", c.Host, "localhost")
				}
				if c.Port != 8080 {
					t.Errorf("Port = %d, want 8080", c.Port)
				}
				if !c.Debug {
					t.Errorf("Debug = %v, want true", c.Debug)
				}
			},
		},
		{
			name: "inline envDefault applied when key missing",
			vars: map[string]string{},
			newTarget: func() any {
				return &dbConfig{}
			},
			check: func(t *testing.T, v any) {
				c := v.(*dbConfig)
				if c.Host != "localhost" {
					t.Errorf("Host = %q, want %q", c.Host, "localhost")
				}
				if c.Port != 5432 {
					t.Errorf("Port = %d, want 5432", c.Port)
				}
			},
		},
		{
			name: "lowercase and dotted tags resolve case-insensitively",
			vars: map[string]string{"DEEPSEEK_KEY": "sk-test-123", "DATABASE_HOST": "db.example.com"},
			newTarget: func() any {
				return &taggedConfig{}
			},
			check: func(t *testing.T, v any) {
				c := v.(*taggedConfig)
				if c.APIKey != "sk-test-123" {
					t.Errorf("APIKey = %q, want %q", c.APIKey, "sk-test-123")
				}
				if c.DBHost != "db.example.com" {
					t.Errorf("DBHost = %q, want %q", c.DBHost, "db.example.com")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetDefaultLoader()
			defer resetDefaultLoader()

			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			for k, v := range tt.vars {
				if err := loader.Set(k, v); err != nil {
					t.Fatalf("Set(%q) error = %v", k, err)
				}
			}
			if err := setDefaultLoader(loader); err != nil {
				t.Fatalf("setDefaultLoader() error = %v", err)
			}

			target := tt.newTarget()
			if err := ParseInto(target); err != nil {
				t.Fatalf("ParseInto() error = %v", err)
			}
			tt.check(t, target)
		})
	}
}

// TestParseInto_EdgeCases covers the invalid-target shapes: each must return
// an error instead of panicking.
func TestParseInto_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		call func(l *Loader) error
	}{
		{"nil target", func(l *Loader) error { return l.ParseInto(nil) }},
		{"non-pointer target", func(l *Loader) error {
			type configStruct struct {
				Key string `env:"KEY"`
			}
			var c configStruct
			return l.ParseInto(c) // not a pointer
		}},
		{"pointer to non-struct", func(l *Loader) error {
			var str string
			return l.ParseInto(&str)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := tt.call(loader); err == nil {
				t.Errorf("ParseInto(%s) should return error", tt.name)
			}
		})
	}
}

// ============================================================================
// Load() Function Tests
// ============================================================================

func TestInitWithConfig(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

	// Create test file system
	fs := newTestFileSystem()
	fs.files["custom.env"] = "CUSTOM_KEY=custom_value"

	// Use LoadWithConfig - this actually calls the function
	cfg := DefaultConfig()
	cfg.Filenames = []string{"custom.env"}
	cfg.FileSystem = fs
	cfg.OverwriteExisting = true

	// Call the actual LoadWithConfig function
	err := LoadWithConfig(cfg)
	if err != nil {
		t.Fatalf("LoadWithConfig() error = %v", err)
	}

	// Verify value via convenience function
	if v := GetString("CUSTOM_KEY"); v != "custom_value" {
		t.Errorf("GetString(CUSTOM_KEY) = %q, want %q", v, "custom_value")
	}
}

// ============================================================================
// Overload Tests
// ============================================================================

// newRelativeTempDir creates a temporary directory INSIDE the package
// directory and removes it when the test completes. The path validator
// rejects absolute paths (drive letters, leading '/'), so t.TempDir() —
// which is absolute — cannot be used for files fed to Load/Overload.
func newRelativeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "envtest_")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) // test artifacts only
	return dir
}

// TestOverload_MultiFileLastWins pins Overload's cross-file merge semantics
// end to end through the public API, against Load's first-file-wins default
// in the same scenario: with Overload a later file overrides an earlier one,
// while keys unique to the earlier file still load.
func TestOverload_MultiFileLastWins(t *testing.T) {
	dir := newRelativeTempDir(t)
	base := filepath.Join(dir, "base.env")
	override := filepath.Join(dir, "override.env")
	if err := os.WriteFile(base, []byte("SHARED_KEY=from_base\nONLY_BASE=base_value\n"), 0o600); err != nil {
		t.Fatalf("WriteFile base.env: %v", err)
	}
	if err := os.WriteFile(override, []byte("SHARED_KEY=from_override\n"), 0o600); err != nil {
		t.Fatalf("WriteFile override.env: %v", err)
	}
	// Load/Overload auto-apply to the real os.Environ and Close() does not
	// unset applied variables; restore the process env for later tests.
	t.Cleanup(func() {
		_ = os.Unsetenv("SHARED_KEY") // best-effort test cleanup
		_ = os.Unsetenv("ONLY_BASE")
	})

	// Load: the first file to set a key wins.
	resetDefaultLoader()
	defer resetDefaultLoader()
	if err := Load(base, override); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := GetString("SHARED_KEY", ""); got != "from_base" {
		t.Errorf("Load: SHARED_KEY = %q, want %q (first file wins)", got, "from_base")
	}

	// Overload: the later file wins; unique keys still load.
	resetDefaultLoader()
	if err := Overload(base, override); err != nil {
		t.Fatalf("Overload() error = %v", err)
	}
	if got := GetString("SHARED_KEY", ""); got != "from_override" {
		t.Errorf("Overload: SHARED_KEY = %q, want %q (later file wins)", got, "from_override")
	}
	if got := GetString("ONLY_BASE", ""); got != "base_value" {
		t.Errorf("Overload: ONLY_BASE = %q, want %q (unique keys still load)", got, "base_value")
	}
}

// TestOverload_JSONThenYAML mirrors the motivating use case: a JSON base
// configuration refined by a YAML file. Structured keys flatten into one
// key space, so the YAML value wins for shared keys and JSON-only keys
// survive.
func TestOverload_JSONThenYAML(t *testing.T) {
	dir := newRelativeTempDir(t)
	jsonFile := filepath.Join(dir, "config.json")
	yamlFile := filepath.Join(dir, "config.yaml")
	jsonDoc := `{"deepseek": {"key": "from_json", "model": "deepseek-v4-pro"}, "moonshot": {"key": "mk"}}`
	yamlDoc := "deepseek:\n  key: from_yaml\n"
	if err := os.WriteFile(jsonFile, []byte(jsonDoc), 0o600); err != nil {
		t.Fatalf("WriteFile config.json: %v", err)
	}
	if err := os.WriteFile(yamlFile, []byte(yamlDoc), 0o600); err != nil {
		t.Fatalf("WriteFile config.yaml: %v", err)
	}
	// Overload auto-applies to the real os.Environ; restore it for later
	// tests. The applied names are the flattened "_"-joined forms.
	t.Cleanup(func() {
		_ = os.Unsetenv("DEEPSEEK_KEY") // best-effort test cleanup
		_ = os.Unsetenv("DEEPSEEK_MODEL")
		_ = os.Unsetenv("MOONSHOT_KEY")
	})

	resetDefaultLoader()
	defer resetDefaultLoader()
	if err := Overload(jsonFile, yamlFile); err != nil {
		t.Fatalf("Overload() error = %v", err)
	}
	if got := GetString("deepseek.key", ""); got != "from_yaml" {
		t.Errorf("deepseek.key = %q, want %q (YAML overrides JSON)", got, "from_yaml")
	}
	// The same value under its stored form: nested keys flatten with "_"
	// (deepseek.key → DEEPSEEK_KEY), which is also the name applied to
	// os.Environ — see the cleanup below.
	if got := GetString("DEEPSEEK_KEY", ""); got != "from_yaml" {
		t.Errorf("DEEPSEEK_KEY = %q, want %q (flattened storage form)", got, "from_yaml")
	}
	if got := GetString("deepseek.model", ""); got != "deepseek-v4-pro" {
		t.Errorf("deepseek.model = %q, want %q (JSON-only key survives)", got, "deepseek-v4-pro")
	}
	if got := GetString("moonshot.key", ""); got != "mk" {
		t.Errorf("moonshot.key = %q, want %q", got, "mk")
	}
}

// TestOverload_ReplacesProcessEnv verifies the process-environment half of
// the Overload contract: loaded values REPLACE variables already present in
// os.Environ, where Load leaves existing process variables untouched.
func TestOverload_ReplacesProcessEnv(t *testing.T) {
	dir := newRelativeTempDir(t)
	file := filepath.Join(dir, "overload.env")
	if err := os.WriteFile(file, []byte("ENVTEST_OVERLOAD_KEY=from_file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile overload.env: %v", err)
	}

	// t.Setenv restores the original (unset) state after the test, once the
	// deferred resetDefaultLoader has closed the loaders.
	t.Setenv("ENVTEST_OVERLOAD_KEY", "from_process")

	resetDefaultLoader()
	defer resetDefaultLoader()

	// Load leaves the existing process variable untouched.
	if err := Load(file); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := os.Getenv("ENVTEST_OVERLOAD_KEY"); got != "from_process" {
		t.Errorf("process env after Load = %q, want %q (left untouched)", got, "from_process")
	}

	// Overload replaces it with the file value.
	resetDefaultLoader()
	if err := Overload(file); err != nil {
		t.Fatalf("Overload() error = %v", err)
	}
	if got := os.Getenv("ENVTEST_OVERLOAD_KEY"); got != "from_file" {
		t.Errorf("process env after Overload = %q, want %q (replaced by file value)", got, "from_file")
	}
}

// TestDefaultLoaderAlreadyInitialized unifies the already-initialized
// coverage: Load, Overload and LoadWithConfig share one default loader, so
// whichever entrance initialized it, every later initialization call fails
// with ErrAlreadyInitialized.
func TestDefaultLoaderAlreadyInitialized(t *testing.T) {
	// The file does not exist; FailOnMissingFile defaults to false, so the
	// first initialization succeeds silently (see
	// TestLoadMissingFileBehavior for the FailOnMissingFile matrix).
	const missing = "envtest_no_such_file.env"

	withMissingFile := func() Config {
		cfg := DefaultConfig()
		cfg.Filenames = []string{missing}
		return cfg
	}

	tests := []struct {
		name   string
		init   func() error // first call: must succeed
		reinit func() error // second call: must fail with ErrAlreadyInitialized
	}{
		{"Load twice",
			func() error { return Load(missing) },
			func() error { return Load(missing) }},
		{"Overload twice",
			func() error { return Overload(missing) },
			func() error { return Overload(missing) }},
		{"Load after Overload",
			func() error { return Overload(missing) },
			func() error { return Load(missing) }},
		{"LoadWithConfig twice",
			func() error { return LoadWithConfig(withMissingFile()) },
			func() error { return LoadWithConfig(withMissingFile()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetDefaultLoader()
			defer resetDefaultLoader()

			if err := tt.init(); err != nil {
				t.Fatalf("initial call error = %v, want nil", err)
			}
			if err := tt.reinit(); !errors.Is(err, ErrAlreadyInitialized) {
				t.Errorf("second call error = %v, want ErrAlreadyInitialized", err)
			}
		})
	}
}

// TestLoadMissingFileBehavior pins the FailOnMissingFile matrix across the
// initialization entrances: with the default false a missing file is skipped
// silently; FailOnMissingFile=true (only reachable through LoadWithConfig)
// surfaces ErrFileNotFound.
func TestLoadMissingFileBehavior(t *testing.T) {
	const missing = "envtest_no_such_file.env"

	tests := []struct {
		name    string
		init    func() error
		wantErr bool
	}{
		{"Load with missing file succeeds silently",
			func() error { return Load(missing) }, false},
		{"Overload with missing file succeeds silently",
			func() error { return Overload(missing) }, false},
		{"LoadWithConfig with FailOnMissingFile=false succeeds silently", func() error {
			cfg := DefaultConfig()
			cfg.Filenames = []string{missing}
			cfg.FailOnMissingFile = false
			return LoadWithConfig(cfg)
		}, false},
		{"LoadWithConfig with FailOnMissingFile=true errors", func() error {
			cfg := DefaultConfig()
			cfg.Filenames = []string{missing}
			cfg.FailOnMissingFile = true
			return LoadWithConfig(cfg)
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetDefaultLoader()
			defer resetDefaultLoader()

			err := tt.init()
			if tt.wantErr {
				if !errors.Is(err, ErrFileNotFound) {
					t.Errorf("error = %v, want ErrFileNotFound", err)
				}
				return
			}
			if err != nil {
				t.Errorf("error = %v, want nil (missing file skipped)", err)
			}
		})
	}
}

// ============================================================================
// Keys/All/Len/Delete Function Tests
// ============================================================================

// TestKeysAllLen verifies the three introspection accessors in one table:
// they share the same setup (two keys in the default loader) and only differ
// in the accessor under test.
func TestKeysAllLen(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.Set("KEY1", "value1"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := loader.Set("KEY2", "value2"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	t.Run("Keys returns both keys", func(t *testing.T) {
		keys := Keys()
		if len(keys) != 2 {
			t.Fatalf("Keys() returned %d keys, want 2", len(keys))
		}
		keyMap := make(map[string]bool)
		for _, k := range keys {
			keyMap[k] = true
		}
		if !keyMap["KEY1"] || !keyMap["KEY2"] {
			t.Errorf("Keys() = %v, want [KEY1, KEY2]", keys)
		}
	})

	t.Run("All returns both entries", func(t *testing.T) {
		all := All()
		if len(all) != 2 {
			t.Fatalf("All() returned %d entries, want 2", len(all))
		}
		if all["KEY1"] != "value1" {
			t.Errorf("All()[\"KEY1\"] = %q, want %q", all["KEY1"], "value1")
		}
		if all["KEY2"] != "value2" {
			t.Errorf("All()[\"KEY2\"] = %q, want %q", all["KEY2"], "value2")
		}
	})

	t.Run("Len returns the entry count", func(t *testing.T) {
		if count := Len(); count != 2 {
			t.Errorf("Len() = %d, want 2", count)
		}
	})
}

func TestDelete(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.Set("KEY_TO_DELETE", "value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Verify key exists
	if _, ok := Lookup("KEY_TO_DELETE"); !ok {
		t.Fatal("KEY_TO_DELETE should exist before delete")
	}

	// Delete the key
	if err := Delete("KEY_TO_DELETE"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify key no longer exists
	if _, ok := Lookup("KEY_TO_DELETE"); ok {
		t.Error("KEY_TO_DELETE should not exist after delete")
	}
}

// ============================================================================
// GetSecure/Validate Function Tests
// ============================================================================

// TestGetSecure covers the package-level GetSecure in table form: a hit
// returns a SecureValue whose Reveal exposes the secret, while any miss —
// unknown or empty key — returns nil.
func TestGetSecure(t *testing.T) {
	resetDefaultLoader()
	defer resetDefaultLoader()

	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.Set("SECRET_KEY", "secret_value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	tests := []struct {
		name    string
		key     string
		wantNil bool
	}{
		{"existing key returns secure value", "SECRET_KEY", false},
		{"non-existent key returns nil", "NON_EXISTENT_KEY", true},
		{"empty key returns nil", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := GetSecure(tt.key)
			if tt.wantNil {
				if sv != nil {
					t.Errorf("GetSecure(%q) = %v, want nil", tt.key, sv)
				}
				return
			}
			if sv == nil {
				t.Fatalf("GetSecure(%q) returned nil, want non-nil", tt.key)
			}
			if got := sv.Reveal(); got != "secret_value" {
				t.Errorf("GetSecure(%q).Reveal() = %q, want %q", tt.key, got, "secret_value")
			}
			// Clean up
			sv.Release()
		})
	}
}

// TestValidateRequiredKeys covers both outcomes of the required-keys check
// through the package-level Validate().
func TestValidateRequiredKeys(t *testing.T) {
	t.Run("all required keys present", func(t *testing.T) {
		resetDefaultLoader()
		defer resetDefaultLoader()

		cfg := DefaultConfig()
		cfg.RequiredKeys = []string{"REQUIRED_KEY"}

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		if err := loader.Set("REQUIRED_KEY", "value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if err := setDefaultLoader(loader); err != nil {
			t.Fatalf("setDefaultLoader() error = %v", err)
		}

		if err := Validate(); err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("missing required key fails", func(t *testing.T) {
		resetDefaultLoader()
		defer resetDefaultLoader()

		cfg := DefaultConfig()
		cfg.RequiredKeys = []string{"MISSING_REQUIRED_KEY"}

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		if err := setDefaultLoader(loader); err != nil {
			t.Fatalf("setDefaultLoader() error = %v", err)
		}

		if err := Validate(); err == nil {
			t.Error("Validate() should return error for missing required key")
		}
	})
}

// TestGetSliceFrom drives every element type and resolution strategy through
// one closure table. want is compared with reflect.DeepEqual, so a nil want
// must be spelled as a typed nil slice ([]string(nil)) — DeepEqual
// distinguishes nil from empty.
func TestGetSliceFrom(t *testing.T) {
	tests := []struct {
		name      string
		setupVars map[string]string
		key       string
		call      func(l *Loader, key string) any
		want      any
	}{
		{
			name:      "indexed keys string",
			setupVars: map[string]string{"PORTS_0": "8080", "PORTS_1": "8081", "PORTS_2": "8082"},
			key:       "PORTS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"8080", "8081", "8082"},
		},
		{
			name:      "indexed keys int",
			setupVars: map[string]string{"NUMBERS_0": "1", "NUMBERS_1": "2", "NUMBERS_2": "3"},
			key:       "NUMBERS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[int](l, key) },
			want:      []int{1, 2, 3},
		},
		{
			name:      "indexed keys int64",
			setupVars: map[string]string{"BIGS_0": "100", "BIGS_1": "200"},
			key:       "BIGS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[int64](l, key) },
			want:      []int64{100, 200},
		},
		{
			name:      "indexed keys uint",
			setupVars: map[string]string{"UNS_0": "10", "UNS_1": "20"},
			key:       "UNS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[uint](l, key) },
			want:      []uint{10, 20},
		},
		{
			name:      "indexed keys uint64",
			setupVars: map[string]string{"U64S_0": "100", "U64S_1": "200"},
			key:       "U64S",
			call:      func(l *Loader, key string) any { return GetSliceFrom[uint64](l, key) },
			want:      []uint64{100, 200},
		},
		{
			name:      "indexed keys bool",
			setupVars: map[string]string{"FLAGS_0": "true", "FLAGS_1": "false", "FLAGS_2": "yes"},
			key:       "FLAGS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[bool](l, key) },
			want:      []bool{true, false, true},
		},
		{
			name:      "indexed keys float64",
			setupVars: map[string]string{"RATES_0": "1.5", "RATES_1": "2.5"},
			key:       "RATES",
			call:      func(l *Loader, key string) any { return GetSliceFrom[float64](l, key) },
			want:      []float64{1.5, 2.5},
		},
		{
			name:      "indexed keys duration",
			setupVars: map[string]string{"TIMES_0": "5s", "TIMES_1": "10m"},
			key:       "TIMES",
			call:      func(l *Loader, key string) any { return GetSliceFrom[time.Duration](l, key) },
			want:      []time.Duration{5 * time.Second, 10 * time.Minute},
		},
		{
			name:      "comma-separated string",
			setupVars: map[string]string{"HOSTS": "localhost,127.0.0.1,example.com"},
			key:       "HOSTS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"localhost", "127.0.0.1", "example.com"},
		},
		{
			name:      "comma-separated int",
			setupVars: map[string]string{"PORTS": "80,443,8080"},
			key:       "PORTS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[int](l, key) },
			want:      []int{80, 443, 8080},
		},
		{
			name:      "comma-separated with spaces",
			setupVars: map[string]string{"NAMES": "  alice , bob ,  charlie  "},
			key:       "NAMES",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"alice", "bob", "charlie"},
		},
		{
			name:      "comma-separated empty parts skipped",
			setupVars: map[string]string{"ITEMS": "a,,b,,,c"},
			key:       "ITEMS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"a", "b", "c"},
		},
		{
			name:      "not found with default",
			setupVars: map[string]string{},
			key:       "MISSING",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key, []string{"default1", "default2"}) },
			want:      []string{"default1", "default2"},
		},
		{
			name:      "not found without default returns nil",
			setupVars: map[string]string{},
			key:       "MISSING",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string(nil),
		},
		{
			// GEN-001: a miss on the first candidate (the literal key) used to
			// return the default from getSliceFromIndexedKeys, short-circuiting
			// the candidate loop so data under the dot-notation candidate was
			// never probed and the default shadowed real data.
			name:      "default does not shadow dot-notation candidate data",
			setupVars: map[string]string{"SERVICE_CORS_ALLOW_ORIGINS_0": "https://a.com", "SERVICE_CORS_ALLOW_ORIGINS_1": "https://b.com"},
			key:       "service.cors.allow_origins",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key, []string{"fallback"}) },
			want:      []string{"https://a.com", "https://b.com"},
		},
		{
			// GEN-001, uppercase-path variant: the literal "DATABASE.PORTS"
			// misses; the underscore-joined candidate holds the data.
			name:      "default does not shadow uppercase-path candidate data",
			setupVars: map[string]string{"DATABASE_PORTS_0": "3306"},
			key:       "DATABASE.PORTS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[int](l, key, []int{5432}) },
			want:      []int{3306},
		},
		{
			// A comma-separated value under the first candidate is found data:
			// it must win over the default exactly as it did before the fix.
			name:      "comma value with default still wins",
			setupVars: map[string]string{"HOSTS": "a,b"},
			key:       "HOSTS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key, []string{"fallback"}) },
			want:      []string{"a", "b"},
		},
		{
			// An existing-but-empty comma value falls back to the default via
			// parseCommaSeparated("") while still counting as found.
			name:      "empty comma value with default returns default",
			setupVars: map[string]string{"EMPTY": ""},
			key:       "EMPTY",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key, []string{"fallback"}) },
			want:      []string{"fallback"},
		},
		{
			// A comma value whose elements fail to parse discards the list and
			// returns the default (documented semantics, unchanged by the fix).
			name:      "comma parse failure with default returns default",
			setupVars: map[string]string{"NUMS": "1,bad,3"},
			key:       "NUMS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[int](l, key, []int{7}) },
			want:      []int{7},
		},
		{
			name:      "empty value returns nil",
			setupVars: map[string]string{"EMPTY": ""},
			key:       "EMPTY",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string(nil),
		},
		{
			// A parse failure in the comma-separated fallback discards the
			// whole list and returns the default.
			name:      "parse error returns default",
			setupVars: map[string]string{"BAD_INT_0": "not_a_number"},
			key:       "BAD_INT",
			call:      func(l *Loader, key string) any { return GetSliceFrom[int](l, key, []int{42}) },
			want:      []int{42},
		},
		{
			name:      "indexed keys take precedence over comma",
			setupVars: map[string]string{"KEY": "comma,separated", "KEY_0": "indexed0", "KEY_1": "indexed1"},
			key:       "KEY",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"indexed0", "indexed1"},
		},
		{
			name:      "dot-notation path resolution",
			setupVars: map[string]string{"SERVICE_CORS_ALLOW_ORIGINS_0": "https://a.com", "SERVICE_CORS_ALLOW_ORIGINS_1": "https://b.com"},
			key:       "service.cors.allow_origins",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"https://a.com", "https://b.com"},
		},
		{
			name:      "dot-notation with uppercase key",
			setupVars: map[string]string{"DATABASE_PORTS_0": "3306", "DATABASE_PORTS_1": "3307"},
			key:       "DATABASE.PORTS",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"3306", "3307"},
		},
		{
			// Pinned from accessors.go getSliceFromIndexedKeys: the loop
			// breaks at the first missing index, so only the elements before
			// the gap are returned (SPARSE_2 is unreachable).
			name:      "sparse index stops at the first gap",
			setupVars: map[string]string{"SPARSE_0": "a", "SPARSE_2": "c"},
			key:       "SPARSE",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string{"a"},
		},
		{
			// Pinned from accessors.go (~L318-321): an indexed element that
			// fails to parse is skipped while the remaining elements are
			// kept — unlike the comma-separated fallback (see the
			// "parse error returns default" row).
			name:      "indexed element parse failure skipped, rest kept",
			setupVars: map[string]string{"MIX_0": "1", "MIX_1": "bad", "MIX_2": "3"},
			key:       "MIX",
			call:      func(l *Loader, key string) any { return GetSliceFrom[int](l, key) },
			want:      []int{1, 3},
		},
		{
			name:      "empty key is a miss",
			setupVars: map[string]string{},
			key:       "",
			call:      func(l *Loader, key string) any { return GetSliceFrom[string](l, key) },
			want:      []string(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			// Setup variables
			for k, v := range tt.setupVars {
				if err := loader.Set(k, v); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}

			if got := tt.call(loader, tt.key); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetSliceFrom(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestGetSliceFrom_MaxSliceSizeCap pins the DoS bound in
// getSliceFromIndexedKeys (accessors.go): iteration stops at maxSliceSize
// (10000) indexed keys even when all of them exist, so the returned slice is
// capped at exactly that length.
func TestGetSliceFrom_MaxSliceSizeCap(t *testing.T) {
	const wantCap = 10000

	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	for i := 0; i < wantCap; i++ {
		if err := loader.Set("CAPS_"+strconv.Itoa(i), strconv.Itoa(i)); err != nil {
			t.Fatalf("Set(CAPS_%d) error = %v", i, err)
		}
	}

	got := GetSliceFrom[int](loader, "CAPS")
	if len(got) != wantCap {
		t.Fatalf("len(GetSliceFrom[int](loader, \"CAPS\")) = %d, want %d (maxSliceSize cap)", len(got), wantCap)
	}
	if got[0] != 0 || got[wantCap-1] != wantCap-1 {
		t.Errorf("boundary elements = [%d, %d], want [0, %d]", got[0], got[wantCap-1], wantCap-1)
	}
}

// TestGetSliceFrom_LiteralDottedKeys verifies slice access for keys that
// literally contain dots. A YAML "apnews.com" array flattens to
// APNEWS.COM_0/APNEWS.COM_1; before the ResolveBaseKeys fix GetSliceFrom
// only probed the underscore-joined form (APNEWS_COM_0) and never found it.
// The fixture loads from YAML because loader.Set rejects dotted keys under
// the default key pattern (file loads allow them via IsValidJSONKey).
func TestGetSliceFrom_LiteralDottedKeys(t *testing.T) {
	fs := newTestFileSystem()
	fs.files["config.yaml"] = "apnews.com:\n  - alpha\n  - beta\n  - gamma\n" +
		"feeds.example.com: one, two, three\n" +
		"database:\n  hosts:\n    - db1\n    - db2\n"

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup
	if err := loader.LoadFiles("config.yaml"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	t.Run("indexed keys under literal dotted key", func(t *testing.T) {
		got := GetSliceFrom[string](loader, "apnews.com")
		if !sliceEqual(got, []string{"alpha", "beta", "gamma"}) {
			t.Errorf("GetSliceFrom(\"apnews.com\") = %v, want [alpha beta gamma]", got)
		}
	})

	t.Run("uppercase form of literal dotted key", func(t *testing.T) {
		got := GetSliceFrom[string](loader, "APNEWS.COM")
		if !sliceEqual(got, []string{"alpha", "beta", "gamma"}) {
			t.Errorf("GetSliceFrom(\"APNEWS.COM\") = %v, want [alpha beta gamma]", got)
		}
	})

	t.Run("comma-separated fallback under literal dotted key", func(t *testing.T) {
		got := GetSliceFrom[string](loader, "feeds.example.com")
		if !sliceEqual(got, []string{"one", "two", "three"}) {
			t.Errorf("GetSliceFrom(\"feeds.example.com\") = %v, want [one two three]", got)
		}
	})

	t.Run("nested dot-notation path still resolves", func(t *testing.T) {
		got := GetSliceFrom[string](loader, "database.hosts")
		if !sliceEqual(got, []string{"db1", "db2"}) {
			t.Errorf("GetSliceFrom(\"database.hosts\") = %v, want [db1 db2]", got)
		}
	})

	t.Run("missing dotted key returns default", func(t *testing.T) {
		got := GetSliceFrom[string](loader, "nope.example", []string{"fallback"})
		if !sliceEqual(got, []string{"fallback"}) {
			t.Errorf("GetSliceFrom(\"nope.example\") = %v, want [fallback]", got)
		}
	})
}

// TestGetSliceFrom_UnavailableLoader covers both degenerate loaders in one
// table: a nil loader and a closed loader behave like a miss — the default
// when one is provided, nil otherwise.
func TestGetSliceFrom_UnavailableLoader(t *testing.T) {
	tests := []struct {
		name     string
		loaderFn func(t *testing.T) *Loader
	}{
		{"nil loader", func(t *testing.T) *Loader { return nil }},
		{"closed loader", func(t *testing.T) *Loader {
			t.Helper()
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_ = loader.Close() // best-effort cleanup
			return loader
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := tt.loaderFn(t)

			if got := GetSliceFrom[string](loader, "KEY", []string{"default"}); !sliceEqual(got, []string{"default"}) {
				t.Errorf("GetSliceFrom with default = %v, want [default]", got)
			}
			if got := GetSliceFrom[string](loader, "KEY"); got != nil {
				t.Errorf("GetSliceFrom without default = %v, want nil", got)
			}
		})
	}
}

// sliceEqual is a helper function to compare slices of any comparable type
func sliceEqual[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLookup_CommaSeparatedFallback keeps the integration-representative
// rows of the instance Lookup's indexed-access fallback (index-0 hit,
// index-1 fallback, out-of-range miss); the full comma semantics matrix
// (precedence, whitespace, non-indexed paths) lives in
// internal/lookup_test.go.
func TestLookup_CommaSeparatedFallback(t *testing.T) {
	tests := []struct {
		name      string
		setupVars map[string]string
		key       string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "comma-separated fallback index 0",
			setupVars: map[string]string{"SERVICE_CORS_ALLOW_ORIGINS": "https://www.example.com,https://admin.example.com"},
			key:       "service.cors.allow_origins.0",
			wantValue: "https://www.example.com",
			wantOK:    true,
		},
		{
			name:      "comma-separated fallback index 1",
			setupVars: map[string]string{"SERVICE_CORS_ALLOW_ORIGINS": "https://www.example.com,https://admin.example.com"},
			key:       "service.cors.allow_origins.1",
			wantValue: "https://admin.example.com",
			wantOK:    true,
		},
		{
			name:      "comma-separated index out of range",
			setupVars: map[string]string{"SERVICE_CORS_ALLOW_ORIGINS": "one,two"},
			key:       "service.cors.allow_origins.5",
			wantValue: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			for k, v := range tt.setupVars {
				if err := loader.Set(k, v); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}

			gotValue, gotOK := loader.Lookup(tt.key)
			if gotValue != tt.wantValue || gotOK != tt.wantOK {
				t.Errorf("Lookup(%q) = (%q, %v), want (%q, %v)",
					tt.key, gotValue, gotOK, tt.wantValue, tt.wantOK)
			}
		})
	}
}
