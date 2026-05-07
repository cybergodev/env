package env

import (
	"errors"
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
	ResetDefaultLoader()
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = ResetDefaultLoader() })
	return loader
}

// ============================================================================
// Convenience Getter Tests (Table-Driven)
// ============================================================================

func TestConvenienceGet(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      string
		getter     string // "string", "int", "bool", "duration"
		defaultVal interface{}
		wantValue  interface{}
	}{
		// String tests
		{"get string existing", "KEY", "value", "string", nil, "value"},
		{"get string with default", "MISSING", "", "string", "default_value", "default_value"},
		{"get string missing no default", "MISSING", "", "string", nil, ""},

		// Int tests
		{"get int existing", "PORT", "8080", "int", nil, int64(8080)},
		{"get int with default", "MISSING", "", "int", int64(9999), int64(9999)},
		{"get int missing no default", "NOT_EXISTS", "", "int", nil, int64(0)},

		// Bool tests
		{"get bool true", "DEBUG", "true", "bool", nil, true},
		{"get bool false", "DEBUG", "false", "bool", nil, false},
		{"get bool with default", "MISSING", "", "bool", true, true},
		{"get bool missing no default", "NOT_EXISTS", "", "bool", nil, false},

		// Duration tests
		{"get duration existing", "TIMEOUT", "30s", "duration", nil, 30 * time.Second},
		{"get duration with default", "MISSING", "", "duration", 5 * time.Minute, 5 * time.Minute},
		{"get duration missing no default", "NOT_EXISTS", "", "duration", nil, time.Duration(0)},
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

			switch tt.getter {
			case "string":
				var got string
				if tt.defaultVal != nil {
					got = GetString(tt.key, tt.defaultVal.(string))
				} else {
					got = GetString(tt.key)
				}
				if got != tt.wantValue.(string) {
					t.Errorf("GetString() = %v, want %v", got, tt.wantValue)
				}

			case "int":
				var got int64
				if tt.defaultVal != nil {
					got = GetInt(tt.key, tt.defaultVal.(int64))
				} else {
					got = GetInt(tt.key)
				}
				if got != tt.wantValue.(int64) {
					t.Errorf("GetInt() = %v, want %v", got, tt.wantValue)
				}

			case "bool":
				var got bool
				if tt.defaultVal != nil {
					got = GetBool(tt.key, tt.defaultVal.(bool))
				} else {
					got = GetBool(tt.key)
				}
				if got != tt.wantValue.(bool) {
					t.Errorf("GetBool() = %v, want %v", got, tt.wantValue)
				}

			case "duration":
				var got time.Duration
				if tt.defaultVal != nil {
					got = GetDuration(tt.key, tt.defaultVal.(time.Duration))
				} else {
					got = GetDuration(tt.key)
				}
				if got != tt.wantValue.(time.Duration) {
					t.Errorf("GetDuration() = %v, want %v", got, tt.wantValue)
				}
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

func TestConvenienceGetSlice(t *testing.T) {
	loader := setupTestLoader(t)

	if err := loader.Set("PORTS_0", "8080"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := loader.Set("PORTS_1", "8081"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Test GetSlice with indexed keys
	result := GetSliceFrom[int](loader, "PORTS")
	if len(result) != 2 || result[0] != 8080 || result[1] != 8081 {
		t.Errorf("GetSliceFrom[int]() = %v, want [8080 8081]", result)
	}

	// Test GetSlice with default
	resultStr := GetSliceFrom[string](loader, "MISSING", []string{"default"})
	if len(resultStr) != 1 || resultStr[0] != "default" {
		t.Errorf("GetSliceFrom[string]() with default = %v, want [default]", resultStr)
	}
}

func TestConvenienceNoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Without a loader, should return defaults/zero values
	got := GetString("KEY", "default")
	if got != "default" {
		t.Errorf("GetString with no loader = %q, want \"default\"", got)
	}

	gotInt := GetInt("KEY", 123)
	if gotInt != 123 {
		t.Errorf("GetInt with no loader = %d, want 123", gotInt)
	}

	// With defaults
	gotBool := GetBool("KEY", true)
	if gotBool != true {
		t.Errorf("GetBool with no loader = %v, want true", gotBool)
	}

	gotDuration := GetDuration("KEY", 10*time.Second)
	if gotDuration != 10*time.Second {
		t.Errorf("GetDuration with no loader = %v, want 10s", gotDuration)
	}

	// Without defaults
	if GetString("KEY") != "" {
		t.Error("GetString with no loader and no default should return \"\"")
	}
	if GetInt("KEY") != 0 {
		t.Error("GetInt with no loader and no default should return 0")
	}
	if GetBool("KEY") != false {
		t.Error("GetBool with no loader and no default should return false")
	}
	if GetDuration("KEY") != 0 {
		t.Error("GetDuration with no loader and no default should return 0")
	}
}

// ============================================================================
// Load Function Tests
// ============================================================================

func TestLoad(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	fs := newTestFileSystem()
	fs.files[".env"] = "LOAD_KEY=load_value"

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.AutoApply = true

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Load the file first
	if err := loader.LoadFiles(".env"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Test that we can get the loaded value
	if got := GetString("LOAD_KEY"); got != "load_value" {
		t.Errorf("GetString(\"LOAD_KEY\") = %q, want %q", got, "load_value")
	}
}

func TestLoadWithConfig(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	fs := newTestFileSystem()
	fs.files["custom.env"] = "CUSTOM_KEY=custom_value"

	cfg := TestingConfig()
	cfg.FileSystem = fs
	cfg.Filenames = []string{"custom.env"}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.LoadFiles("custom.env"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Test that we can get the loaded value
	if got := GetString("CUSTOM_KEY"); got != "custom_value" {
		t.Errorf("GetString(\"CUSTOM_KEY\") = %q, want %q", got, "custom_value")
	}
}

// ============================================================================
// ParseInto Function Tests
// ============================================================================

func TestParseInto(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.Set("APP_HOST", "localhost"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := loader.Set("APP_PORT", "8080"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := loader.Set("APP_DEBUG", "true"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	type AppConfig struct {
		Host  string `env:"APP_HOST"`
		Port  int    `env:"APP_PORT"`
		Debug bool   `env:"APP_DEBUG"`
	}

	var appCfg AppConfig
	if err := ParseInto(&appCfg); err != nil {
		t.Fatalf("ParseInto() error = %v", err)
	}

	if appCfg.Host != "localhost" {
		t.Errorf("Host = %q, want %q", appCfg.Host, "localhost")
	}
	if appCfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", appCfg.Port)
	}
	if appCfg.Debug != true {
		t.Errorf("Debug = %v, want true", appCfg.Debug)
	}
}

func TestParseInto_WithInlineDefault(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	type Config struct {
		Host string `env:"DB_HOST,envDefault:localhost"`
		Port int    `env:"DB_PORT,envDefault:5432"`
	}

	var c Config
	if err := ParseInto(&c); err != nil {
		t.Fatalf("ParseInto() error = %v", err)
	}

	if c.Host != "localhost" {
		t.Errorf("Host = %q, want %q", c.Host, "localhost")
	}
	if c.Port != 5432 {
		t.Errorf("Port = %d, want 5432", c.Port)
	}
}

func TestParseInto_NoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Without explicit loader, getDefaultLoader creates one automatically
	// So ParseInto should succeed but struct fields remain empty
	type Config struct {
		Host string `env:"DB_HOST"`
	}

	var c Config
	err := ParseInto(&c)
	if err != nil {
		t.Errorf("ParseInto with no loader should not error, got: %v", err)
	}
	// Host should be empty since DB_HOST is not set
	if c.Host != "" {
		t.Errorf("Host = %q, want empty string", c.Host)
	}
}

func TestParseInto_EdgeCases(t *testing.T) {
	t.Run("nil target", func(t *testing.T) {
		ResetDefaultLoader()
		defer ResetDefaultLoader()

		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := setDefaultLoader(loader); err != nil {
			t.Fatalf("setDefaultLoader() error = %v", err)
		}

		err = ParseInto(nil)
		if err == nil {
			t.Error("ParseInto(nil) should return error")
		}
	})

	t.Run("non-pointer target", func(t *testing.T) {
		ResetDefaultLoader()
		defer ResetDefaultLoader()

		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := setDefaultLoader(loader); err != nil {
			t.Fatalf("setDefaultLoader() error = %v", err)
		}

		type ConfigStruct struct {
			Key string `env:"KEY"`
		}
		var c ConfigStruct

		// Using loader.ParseInto with non-pointer should error
		err = loader.ParseInto(c) // Not a pointer
		if err == nil {
			t.Error("ParseInto(non-pointer) should return error")
		}
	})

	t.Run("pointer to non-struct", func(t *testing.T) {
		ResetDefaultLoader()
		defer ResetDefaultLoader()

		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := setDefaultLoader(loader); err != nil {
			t.Fatalf("setDefaultLoader() error = %v", err)
		}

		var str string
		err = loader.ParseInto(&str)
		if err == nil {
			t.Error("ParseInto(pointer to string) should return error")
		}
	})
}

// ============================================================================
// Load() Function Tests
// ============================================================================

func TestInit_EquivalentToLoad(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Create test file system
	fs := newTestFileSystem()
	fs.files["init.env"] = "INIT_KEY=init_value\nPORT=3000"

	// Use Load() via config with custom filesystem
	cfg := DefaultConfig()
	cfg.Filenames = []string{"init.env"}
	cfg.FileSystem = fs

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.LoadFiles("init.env"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Verify values via package-level functions
	if v := GetString("INIT_KEY"); v != "init_value" {
		t.Errorf("GetString(INIT_KEY) = %q, want %q", v, "init_value")
	}

	if v := GetInt("PORT", 0); v != 3000 {
		t.Errorf("GetInt(PORT) = %d, want %d", v, 3000)
	}
}

func TestInitWithConfig(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

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

	t.Run("already initialized error", func(t *testing.T) {
		// Second call should fail
		err := LoadWithConfig(cfg)
		if err == nil {
			t.Error("LoadWithConfig() should fail when already initialized")
		}
		if !errors.Is(err, ErrAlreadyInitialized) {
			t.Errorf("LoadWithConfig() error = %v, want ErrAlreadyInitialized", err)
		}
	})
}

// ============================================================================
// Load() Function Tests
// ============================================================================

func TestLoad_AlreadyInitialized(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// First, initialize the default loader
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Now try to call Load() - should fail with ErrAlreadyInitialized
	err = Load("test.env")
	if err == nil {
		t.Error("Load() should return error when default loader already initialized")
	}

	if err != ErrAlreadyInitialized {
		t.Errorf("Load() error = %v, want ErrAlreadyInitialized", err)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Create test file system without the requested file
	fs := newTestFileSystem()
	// Don't add "nonexistent.env"

	cfg := DefaultConfig()
	cfg.Filenames = nil // Don't auto-load files in New()
	cfg.FileSystem = fs
	cfg.AutoApply = true
	cfg.FailOnMissingFile = true // Enable error on missing files

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// LoadFiles should return an error when FailOnMissingFile is true
	err = loader.LoadFiles("nonexistent.env")
	if err == nil {
		t.Error("LoadFiles() should return error for non-existent file when FailOnMissingFile is true")
	}
}

func TestLoad_DefaultFilename(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Create test file system with default .env
	fs := newTestFileSystem()
	fs.files[".env"] = "DEFAULT_KEY=default_value"

	cfg := DefaultConfig() // No filename = default .env
	cfg.FileSystem = fs
	cfg.AutoApply = true

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.LoadFiles(); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Verify value is accessible
	if got := GetString("DEFAULT_KEY"); got != "default_value" {
		t.Errorf("GetString(\"DEFAULT_KEY\") = %q, want %q", got, "default_value")
	}
}

// ============================================================================
// Load with JSON/YAML Tests
// ============================================================================

func TestLoad_JSONFile(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Create test file system with JSON file
	fs := newTestFileSystem()
	fs.files["config.json"] = `{"database": {"host": "db.example.com", "port": 3306}}`

	cfg := DefaultConfig()
	cfg.Filenames = []string{"config.json"}
	cfg.FileSystem = fs
	cfg.AutoApply = true

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.LoadFiles("config.json"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// JSON keys are flattened and uppercased
	if got := GetString("DATABASE_HOST"); got != "db.example.com" {
		t.Errorf("GetString(\"DATABASE_HOST\") = %q, want %q", got, "db.example.com")
	}

	if got := GetInt("DATABASE_PORT"); got != 3306 {
		t.Errorf("GetInt(\"DATABASE_PORT\") = %d, want 3306", got)
	}
}

func TestLoad_YAMLFile(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// Create test file system with YAML file
	fs := newTestFileSystem()
	fs.files["config.yaml"] = "server:\n  host: yaml.example.com\n  port: 8443"

	cfg := DefaultConfig()
	cfg.Filenames = []string{"config.yaml"}
	cfg.FileSystem = fs
	cfg.AutoApply = true

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.LoadFiles("config.yaml"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// YAML keys are flattened and uppercased
	if got := GetString("SERVER_HOST"); got != "yaml.example.com" {
		t.Errorf("GetString(\"SERVER_HOST\") = %q, want %q", got, "yaml.example.com")
	}

	if got := GetInt("SERVER_PORT"); got != 8443 {
		t.Errorf("GetInt(\"SERVER_PORT\") = %d, want 8443", got)
	}
}

// ============================================================================
// Keys/All/Len/Delete Function Tests
// ============================================================================

func TestKeys(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

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

	keys := Keys()
	if len(keys) != 2 {
		t.Errorf("Keys() returned %d keys, want 2", len(keys))
	}

	// Check that both keys are present
	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}
	if !keyMap["KEY1"] || !keyMap["KEY2"] {
		t.Errorf("Keys() = %v, want [KEY1, KEY2]", keys)
	}
}

func TestKeys_NoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// getDefaultLoader creates a default loader automatically
	// So Keys() should return an empty slice, not nil
	keys := Keys()
	if len(keys) != 0 {
		t.Errorf("Keys() with no loader = %v, want empty slice", keys)
	}
}

func TestAll(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

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

	all := All()
	if len(all) != 2 {
		t.Errorf("All() returned %d entries, want 2", len(all))
	}

	if all["KEY1"] != "value1" {
		t.Errorf("All()[\"KEY1\"] = %q, want %q", all["KEY1"], "value1")
	}
	if all["KEY2"] != "value2" {
		t.Errorf("All()[\"KEY2\"] = %q, want %q", all["KEY2"], "value2")
	}
}

func TestAll_NoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// getDefaultLoader creates a default loader automatically
	// So All() should return an empty map, not nil
	all := All()
	if len(all) != 0 {
		t.Errorf("All() with no loader = %v, want empty map", all)
	}
}

func TestLen(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

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

	count := Len()
	if count != 2 {
		t.Errorf("Len() = %d, want 2", count)
	}
}

func TestLen_NoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	count := Len()
	if count != 0 {
		t.Errorf("Len() with no loader = %d, want 0", count)
	}
}

func TestDelete(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

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

func TestDelete_NoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// getDefaultLoader creates a default loader automatically
	// So Delete() should succeed (deleting non-existent key is ok)
	err := Delete("KEY")
	if err != nil {
		t.Errorf("Delete() with auto-created loader error = %v, want nil", err)
	}
}

// ============================================================================
// GetSecure/Validate Function Tests
// ============================================================================

func TestGetSecure(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

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

	sv := GetSecure("SECRET_KEY")
	if sv == nil {
		t.Fatal("GetSecure() returned nil, want non-nil")
	}

	if sv.Reveal() != "secret_value" {
		t.Errorf("GetSecure().Reveal() = %q, want %q", sv.Reveal(), "secret_value")
	}

	// Clean up
	sv.Release()
}

func TestGetSecure_NotFound(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	sv := GetSecure("NON_EXISTENT_KEY")
	if sv != nil {
		t.Errorf("GetSecure() for non-existent key = %v, want nil", sv)
	}
}

func TestGetSecure_NoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	sv := GetSecure("KEY")
	if sv != nil {
		t.Errorf("GetSecure() with no loader = %v, want nil", sv)
	}
}

func TestValidate(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

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

	// Should pass validation
	if err := Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestValidate_MissingRequired(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	cfg := DefaultConfig()
	cfg.RequiredKeys = []string{"MISSING_REQUIRED_KEY"}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := setDefaultLoader(loader); err != nil {
		t.Fatalf("setDefaultLoader() error = %v", err)
	}

	// Should fail validation
	err = Validate()
	if err == nil {
		t.Error("Validate() should return error for missing required key")
	}
}

func TestValidate_NoLoader(t *testing.T) {
	ResetDefaultLoader()
	defer ResetDefaultLoader()

	// getDefaultLoader creates a default loader automatically
	// So Validate() should succeed (no required keys means validation passes)
	err := Validate()
	if err != nil {
		t.Errorf("Validate() with auto-created loader error = %v, want nil", err)
	}
}
func TestGetSliceFrom(t *testing.T) {
	tests := []struct {
		name         string
		setupVars    map[string]string
		key          string
		wantString   []string
		wantInt      []int
		wantInt64    []int64
		wantUint     []uint
		wantUint64   []uint64
		wantBool     []bool
		wantFloat64  []float64
		wantDuration []time.Duration
		wantNil      bool
		defaultStr   []string
		defaultInt   []int
	}{
		{
			name:       "indexed keys string",
			setupVars:  map[string]string{"PORTS_0": "8080", "PORTS_1": "8081", "PORTS_2": "8082"},
			key:        "PORTS",
			wantString: []string{"8080", "8081", "8082"},
		},
		{
			name:      "indexed keys int",
			setupVars: map[string]string{"NUMBERS_0": "1", "NUMBERS_1": "2", "NUMBERS_2": "3"},
			key:       "NUMBERS",
			wantInt:   []int{1, 2, 3},
		},
		{
			name:      "indexed keys int64",
			setupVars: map[string]string{"BIGS_0": "100", "BIGS_1": "200"},
			key:       "BIGS",
			wantInt64: []int64{100, 200},
		},
		{
			name:      "indexed keys uint",
			setupVars: map[string]string{"UNS_0": "10", "UNS_1": "20"},
			key:       "UNS",
			wantUint:  []uint{10, 20},
		},
		{
			name:       "indexed keys uint64",
			setupVars:  map[string]string{"U64S_0": "100", "U64S_1": "200"},
			key:        "U64S",
			wantUint64: []uint64{100, 200},
		},
		{
			name:      "indexed keys bool",
			setupVars: map[string]string{"FLAGS_0": "true", "FLAGS_1": "false", "FLAGS_2": "yes"},
			key:       "FLAGS",
			wantBool:  []bool{true, false, true},
		},
		{
			name:        "indexed keys float64",
			setupVars:   map[string]string{"RATES_0": "1.5", "RATES_1": "2.5"},
			key:         "RATES",
			wantFloat64: []float64{1.5, 2.5},
		},
		{
			name:         "indexed keys duration",
			setupVars:    map[string]string{"TIMES_0": "5s", "TIMES_1": "10m"},
			key:          "TIMES",
			wantDuration: []time.Duration{5 * time.Second, 10 * time.Minute},
		},
		{
			name:       "comma-separated string",
			setupVars:  map[string]string{"HOSTS": "localhost,127.0.0.1,example.com"},
			key:        "HOSTS",
			wantString: []string{"localhost", "127.0.0.1", "example.com"},
		},
		{
			name:      "comma-separated int",
			setupVars: map[string]string{"PORTS": "80,443,8080"},
			key:       "PORTS",
			wantInt:   []int{80, 443, 8080},
		},
		{
			name:       "comma-separated with spaces",
			setupVars:  map[string]string{"NAMES": "  alice , bob ,  charlie  "},
			key:        "NAMES",
			wantString: []string{"alice", "bob", "charlie"},
		},
		{
			name:       "comma-separated empty parts skipped",
			setupVars:  map[string]string{"ITEMS": "a,,b,,,c"},
			key:        "ITEMS",
			wantString: []string{"a", "b", "c"},
		},
		{
			name:       "not found with default",
			setupVars:  map[string]string{},
			key:        "MISSING",
			wantString: []string{"default1", "default2"},
			defaultStr: []string{"default1", "default2"},
		},
		{
			name:      "not found without default returns nil",
			setupVars: map[string]string{},
			key:       "MISSING",
			wantNil:   true,
		},
		{
			name:      "empty value returns nil",
			setupVars: map[string]string{"EMPTY": ""},
			key:       "EMPTY",
			wantNil:   true,
		},
		{
			name:       "parse error returns default",
			setupVars:  map[string]string{"BAD_INT_0": "not_a_number"},
			key:        "BAD_INT",
			wantInt:    []int{42},
			defaultInt: []int{42},
		},
		{
			name:       "indexed keys take precedence over comma",
			setupVars:  map[string]string{"KEY": "comma,separated", "KEY_0": "indexed0", "KEY_1": "indexed1"},
			key:        "KEY",
			wantString: []string{"indexed0", "indexed1"},
		},
		{
			name:       "dot-notation path resolution",
			setupVars:  map[string]string{"SERVICE_CORS_ALLOW_ORIGINS_0": "https://a.com", "SERVICE_CORS_ALLOW_ORIGINS_1": "https://b.com"},
			key:        "service.cors.allow_origins",
			wantString: []string{"https://a.com", "https://b.com"},
		},
		{
			name:       "dot-notation with uppercase key",
			setupVars:  map[string]string{"DATABASE_PORTS_0": "3306", "DATABASE_PORTS_1": "3307"},
			key:        "DATABASE.PORTS",
			wantString: []string{"3306", "3307"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer loader.Close()

			// Setup variables
			for k, v := range tt.setupVars {
				if err := loader.Set(k, v); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}

			// Test based on expected type
			if tt.wantString != nil || tt.defaultStr != nil {
				var got []string
				if tt.defaultStr != nil {
					got = GetSliceFrom[string](loader, tt.key, tt.defaultStr)
				} else {
					got = GetSliceFrom[string](loader, tt.key)
				}
				if !sliceEqual(got, tt.wantString) {
					t.Errorf("GetSliceFrom[string]() = %v, want %v", got, tt.wantString)
				}
			}

			if tt.wantInt != nil || tt.defaultInt != nil {
				var got []int
				if tt.defaultInt != nil {
					got = GetSliceFrom[int](loader, tt.key, tt.defaultInt)
				} else {
					got = GetSliceFrom[int](loader, tt.key)
				}
				if !sliceEqual(got, tt.wantInt) {
					t.Errorf("GetSliceFrom[int]() = %v, want %v", got, tt.wantInt)
				}
			}

			if tt.wantInt64 != nil {
				got := GetSliceFrom[int64](loader, tt.key)
				if !sliceEqual(got, tt.wantInt64) {
					t.Errorf("GetSliceFrom[int64]() = %v, want %v", got, tt.wantInt64)
				}
			}

			if tt.wantUint != nil {
				got := GetSliceFrom[uint](loader, tt.key)
				if !sliceEqual(got, tt.wantUint) {
					t.Errorf("GetSliceFrom[uint]() = %v, want %v", got, tt.wantUint)
				}
			}

			if tt.wantUint64 != nil {
				got := GetSliceFrom[uint64](loader, tt.key)
				if !sliceEqual(got, tt.wantUint64) {
					t.Errorf("GetSliceFrom[uint64]() = %v, want %v", got, tt.wantUint64)
				}
			}

			if tt.wantBool != nil {
				got := GetSliceFrom[bool](loader, tt.key)
				if !sliceEqual(got, tt.wantBool) {
					t.Errorf("GetSliceFrom[bool]() = %v, want %v", got, tt.wantBool)
				}
			}

			if tt.wantFloat64 != nil {
				got := GetSliceFrom[float64](loader, tt.key)
				if !sliceEqual(got, tt.wantFloat64) {
					t.Errorf("GetSliceFrom[float64]() = %v, want %v", got, tt.wantFloat64)
				}
			}

			if tt.wantDuration != nil {
				got := GetSliceFrom[time.Duration](loader, tt.key)
				if !sliceEqual(got, tt.wantDuration) {
					t.Errorf("GetSliceFrom[time.Duration]() = %v, want %v", got, tt.wantDuration)
				}
			}

			if tt.wantNil {
				got := GetSliceFrom[string](loader, tt.key)
				if got != nil {
					t.Errorf("GetSliceFrom[string]() = %v, want nil", got)
				}
			}
		})
	}
}

func TestGetSliceFrom_NilLoader(t *testing.T) {
	result := GetSliceFrom[string](nil, "KEY", []string{"default"})
	if !sliceEqual(result, []string{"default"}) {
		t.Errorf("GetSliceFrom with nil loader = %v, want [default]", result)
	}

	result = GetSliceFrom[string](nil, "KEY")
	if result != nil {
		t.Errorf("GetSliceFrom with nil loader and no default = %v, want nil", result)
	}
}

func TestGetSliceFrom_ClosedLoader(t *testing.T) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	loader.Close()

	result := GetSliceFrom[string](loader, "KEY", []string{"default"})
	if !sliceEqual(result, []string{"default"}) {
		t.Errorf("GetSliceFrom with closed loader = %v, want [default]", result)
	}

	result = GetSliceFrom[string](loader, "KEY")
	if result != nil {
		t.Errorf("GetSliceFrom with closed loader and no default = %v, want nil", result)
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

func TestLookup_CommaSeparatedFallback(t *testing.T) {
	tests := []struct {
		name      string
		setupVars map[string]string
		key       string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "indexed key takes precedence",
			setupVars: map[string]string{"ORIGINS_0": "first", "ORIGINS": "second,third"},
			key:       "origins.0",
			wantValue: "first",
			wantOK:    true,
		},
		{
			name:      "comma-separated fallback",
			setupVars: map[string]string{"SERVICE_CORS_ALLOW_ORIGINS": "https://www.example.com,https://admin.example.com"},
			key:       "service.cors.allow_origins.0",
			wantValue: "https://www.example.com",
			wantOK:    true,
		},
		{
			name:      "comma-separated fallback second element",
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
		{
			name:      "no indexed key and no comma-separated",
			setupVars: map[string]string{},
			key:       "service.cors.origins.0",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "comma-separated with whitespace",
			setupVars: map[string]string{"SERVERS": "  host1 , host2 , host3  "},
			key:       "servers.1",
			wantValue: "host2",
			wantOK:    true,
		},
		{
			name:      "non-indexed path returns original value",
			setupVars: map[string]string{"DATABASE_HOST": "localhost"},
			key:       "database.host",
			wantValue: "localhost",
			wantOK:    true,
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

func TestGetString_CommaSeparatedIndex(t *testing.T) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Set up comma-separated value
	if err := loader.Set("SERVICE_CORS_ALLOW_ORIGINS", "https://www.example.com,https://admin.example.com"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Test Lookup with indexed access to comma-separated value
	got, ok := loader.Lookup("service.cors.allow_origins.0")
	if !ok || got != "https://www.example.com" {
		t.Errorf("Lookup(\"service.cors.allow_origins.0\") = (%q, %v), want (\"https://www.example.com\", true)", got, ok)
	}

	got, ok = loader.Lookup("service.cors.allow_origins.1")
	if !ok || got != "https://admin.example.com" {
		t.Errorf("Lookup(\"service.cors.allow_origins.1\") = (%q, %v), want (\"https://admin.example.com\", true)", got, ok)
	}

	// Index out of range returns false
	got, ok = loader.Lookup("service.cors.allow_origins.5")
	if ok {
		t.Errorf("Lookup with out of range index = (%q, %v), want (\"\", false)", got, ok)
	}
}

// ============================================================================
// New Isolation Tests
// ============================================================================

func TestNewIsolation(t *testing.T) {
	t.Run("creates new loader with default config", func(t *testing.T) {
		loader, err := New()
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if loader == nil {
			t.Fatal("New() returned nil")
		}
		defer loader.Close()
	})

	t.Run("creates new loader with custom config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxVariables = 100

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if loader == nil {
			t.Fatal("New() returned nil")
		}
		defer loader.Close()

		returnedCfg := loader.Config()
		if returnedCfg.MaxVariables != 100 {
			t.Errorf("MaxVariables = %d, want 100", returnedCfg.MaxVariables)
		}
	})

	t.Run("does not affect default loader", func(t *testing.T) {
		ResetDefaultLoader()
		defer ResetDefaultLoader()

		// Create isolated loader and set a value
		loader, err := New()
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("ISOLATED_KEY", "isolated_value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		// Default loader should not have this key
		// (getDefaultLoader creates a new loader, so it won't have ISOLATED_KEY)
		defaultVal := GetString("ISOLATED_KEY")
		if defaultVal != "" {
			t.Errorf("GetString(\"ISOLATED_KEY\") = %q, want empty (isolated loader should not affect default)", defaultVal)
		}
	})
}
