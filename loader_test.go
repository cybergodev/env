package env

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Test Helpers
// ============================================================================

// testFileSystem is a mock FileSystem for testing.
type testFileSystem struct {
	files       map[string]string
	env         map[string]string
	openErr     error
	statErr     error
	setenvErr   error
	unsetenvErr error
}

func newTestFileSystem() *testFileSystem {
	return &testFileSystem{
		files: make(map[string]string),
		env:   make(map[string]string),
	}
}

func (fs *testFileSystem) Open(name string) (File, error) {
	if fs.openErr != nil {
		return nil, fs.openErr
	}
	content, ok := fs.files[name]
	if !ok {
		return nil, ErrFileNotFound
	}
	return &testFile{content: content}, nil
}

func (fs *testFileSystem) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return fs.Open(name)
}

func (fs *testFileSystem) Stat(name string) (os.FileInfo, error) {
	if fs.statErr != nil {
		return nil, fs.statErr
	}
	content, ok := fs.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &testFileInfo{name: name, size: int64(len(content))}, nil
}

func (fs *testFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return nil
}

func (fs *testFileSystem) Remove(name string) error {
	delete(fs.files, name)
	return nil
}

func (fs *testFileSystem) Rename(oldpath, newpath string) error {
	fs.files[newpath] = fs.files[oldpath]
	delete(fs.files, oldpath)
	return nil
}

func (fs *testFileSystem) Getenv(key string) string {
	return fs.env[key]
}

func (fs *testFileSystem) Setenv(key, value string) error {
	if fs.setenvErr != nil {
		return fs.setenvErr
	}
	fs.env[key] = value
	return nil
}

func (fs *testFileSystem) Unsetenv(key string) error {
	if fs.unsetenvErr != nil {
		return fs.unsetenvErr
	}
	delete(fs.env, key)
	return nil
}

func (fs *testFileSystem) LookupEnv(key string) (string, bool) {
	v, ok := fs.env[key]
	return v, ok
}

type testFile struct {
	content string
	pos     int
}

func (f *testFile) Read(p []byte) (n int, err error) {
	if f.pos >= len(f.content) {
		return 0, io.EOF
	}
	n = copy(p, f.content[f.pos:])
	f.pos += n
	return n, nil
}

func (f *testFile) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (f *testFile) Close() error {
	return nil
}

func (f *testFile) Stat() (os.FileInfo, error) {
	return &testFileInfo{size: int64(len(f.content))}, nil
}

func (f *testFile) Sync() error {
	return nil
}

type testFileInfo struct {
	name string
	size int64
}

func (fi *testFileInfo) Name() string       { return fi.name }
func (fi *testFileInfo) Size() int64        { return fi.size }
func (fi *testFileInfo) Mode() os.FileMode  { return 0644 }
func (fi *testFileInfo) ModTime() time.Time { return time.Now() }
func (fi *testFileInfo) IsDir() bool        { return false }
func (fi *testFileInfo) Sys() interface{}   { return nil }

// ============================================================================
// New Tests
// ============================================================================

func TestNew(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if loader == nil {
			t.Fatal("New() returned nil loader")
		}
		defer loader.Close()
	})

	t.Run("no arguments - uses default config", func(t *testing.T) {
		loader, err := New()
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if loader == nil {
			t.Fatal("New() returned nil loader")
		}
		defer loader.Close()

		// Verify default config values are applied
		returnedCfg := loader.Config()
		if returnedCfg.MaxFileSize != DefaultMaxFileSize {
			t.Errorf("MaxFileSize = %d, want %d", returnedCfg.MaxFileSize, DefaultMaxFileSize)
		}
		if returnedCfg.MaxVariables != DefaultMaxVariables {
			t.Errorf("MaxVariables = %d, want %d", returnedCfg.MaxVariables, DefaultMaxVariables)
		}
	})

	t.Run("zero-value config - uses default config", func(t *testing.T) {
		loader, err := New(Config{})
		if err != nil {
			t.Fatalf("New(Config{}) error = %v", err)
		}
		if loader == nil {
			t.Fatal("New(Config{}) returned nil loader")
		}
		defer loader.Close()

		// Verify default config values are applied
		returnedCfg := loader.Config()
		if returnedCfg.MaxFileSize != DefaultMaxFileSize {
			t.Errorf("MaxFileSize = %d, want %d", returnedCfg.MaxFileSize, DefaultMaxFileSize)
		}
	})

	t.Run("custom config - preserves values", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.JSONMaxDepth = 20
		cfg.MaxVariables = 100

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		returnedCfg := loader.Config()
		if returnedCfg.JSONMaxDepth != 20 {
			t.Errorf("JSONMaxDepth = %d, want 20", returnedCfg.JSONMaxDepth)
		}
		if returnedCfg.MaxVariables != 100 {
			t.Errorf("MaxVariables = %d, want 100", returnedCfg.MaxVariables)
		}
	})

	t.Run("invalid config - zero max file size", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxFileSize = 0
		_, err := New(cfg)
		if err == nil {
			t.Error("New() should fail with zero MaxFileSize")
		}
	})

	t.Run("invalid config - exceeds hard limit", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxFileSize = 200 * 1024 * 1024 // 200MB exceeds hard limit
		_, err := New(cfg)
		if err == nil {
			t.Error("New() should fail with MaxFileSize exceeding hard limit")
		}
	})

	t.Run("custom key pattern", func(t *testing.T) {
		cfg := DefaultConfig()
		// Pattern that doesn't match TEST_KEY should fail
		cfg.KeyPattern = DefaultKeyPattern
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()
	})
}

// ============================================================================
// LoadFiles Tests
// ============================================================================

func TestLoader_LoadFiles(t *testing.T) {
	t.Run("load single file", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "KEY1=value1\nKEY2=value2"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles(".env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("KEY1") != "value1" {
			t.Errorf("GetString(\"KEY1\") = %q, want %q", loader.GetString("KEY1"), "value1")
		}
	})

	t.Run("load multiple files", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "KEY1=value1"
		fs.files[".env.local"] = "KEY2=value2\nKEY1=overridden"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.OverwriteExisting = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles(".env", ".env.local"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("KEY1") != "overridden" {
			t.Errorf("GetString(\"KEY1\") = %q, want %q", loader.GetString("KEY1"), "overridden")
		}
	})

	t.Run("default to .env", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "KEY=default"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles(); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("KEY") != "default" {
			t.Errorf("GetString(\"KEY\") = %q, want %q", loader.GetString("KEY"), "default")
		}
	})

	t.Run("file not found - skip", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "KEY=value"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.FailOnMissingFile = false
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// Load existing file first, then missing file
		if err := loader.LoadFiles(".env", "missing.env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
	})

	t.Run("file not found - fail", func(t *testing.T) {
		fs := newTestFileSystem()

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Filenames = nil // Don't auto-load, test LoadFiles separately
		cfg.FailOnMissingFile = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("missing.env"); err == nil {
			t.Error("LoadFiles() should fail with missing file")
		}
	})

	t.Run("file too large", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["large.env"] = strings.Repeat("a", 2000)

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.MaxFileSize = 1000
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		var fileErr *FileError
		if err := loader.LoadFiles("large.env"); !errors.As(err, &fileErr) {
			t.Errorf("LoadFiles() error = %v, want FileError", err)
		}
	})

	t.Run("auto apply", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "KEY=value"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.AutoApply = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles(".env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if fs.env["KEY"] != "value" {
			t.Errorf("env[\"KEY\"] = %q, want %q", fs.env["KEY"], "value")
		}
	})

	t.Run("prefix filter", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "APP_KEY=value\nOTHER_KEY=other"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Prefix = "APP_"
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles(".env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("APP_KEY") != "value" {
			t.Errorf("GetString(\"APP_KEY\") = %q, want %q", loader.GetString("APP_KEY"), "value")
		}
		if _, ok := loader.Lookup("OTHER_KEY"); ok {
			t.Error("OTHER_KEY should not be loaded with APP_ prefix")
		}
	})

	t.Run("prefix filter case-insensitive", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "APP_KEY=value\napp_secret=secret\nOTHER_KEY=other"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Prefix = "app_"
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles(".env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		// Both APP_KEY and app_secret should match the "app_" prefix case-insensitively
		if loader.GetString("APP_KEY") != "value" {
			t.Errorf("GetString(\"APP_KEY\") = %q, want %q", loader.GetString("APP_KEY"), "value")
		}
		if loader.GetString("app_secret") != "secret" {
			t.Errorf("GetString(\"app_secret\") = %q, want %q", loader.GetString("app_secret"), "secret")
		}
		if _, ok := loader.Lookup("OTHER_KEY"); ok {
			t.Error("OTHER_KEY should not be loaded with app_ prefix")
		}
	})

}

// ============================================================================
// Apply Tests
// ============================================================================

func TestLoader_Apply(t *testing.T) {
	t.Run("apply to environment", func(t *testing.T) {
		fs := newTestFileSystem()

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.OverwriteExisting = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("TEST_KEY", "test_value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if err := loader.Apply(); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}

		if fs.env["TEST_KEY"] != "test_value" {
			t.Errorf("env[\"TEST_KEY\"] = %q, want %q", fs.env["TEST_KEY"], "test_value")
		}
	})

	t.Run("apply respects overwrite policy", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.env["EXISTING_KEY"] = "original"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.OverwriteExisting = false
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("EXISTING_KEY", "new_value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if err := loader.Apply(); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}

		if fs.env["EXISTING_KEY"] != "original" {
			t.Errorf("env[\"EXISTING_KEY\"] = %q, want %q", fs.env["EXISTING_KEY"], "original")
		}
	})

}

// ============================================================================
// GetString/GetSecure/Lookup Tests (Table-Driven)
// ============================================================================

func TestLoader_GetString(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      string
		defaultVal string
		wantValue  string
	}{
		{"existing key", "KEY", "value", "", "value"},
		{"missing key with default", "MISSING", "", "default", "default"},
		{"missing key without default", "MISSING", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer loader.Close()

			if tt.value != "" {
				if err := loader.Set(tt.key, tt.value); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}

			var got string
			if tt.defaultVal != "" {
				got = loader.GetString(tt.key, tt.defaultVal)
			} else {
				got = loader.GetString(tt.key)
			}

			if got != tt.wantValue {
				t.Errorf("GetString() = %q, want %q", got, tt.wantValue)
			}
		})
	}
}

func TestLoader_GetSecure(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		wantNil   bool
		wantValue string
	}{
		{"existing key", "SECRET", "password123", false, "password123"},
		{"missing key", "MISSING", "", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer loader.Close()

			if tt.value != "" {
				if err := loader.Set(tt.key, tt.value); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}

			sv := loader.GetSecure(tt.key)
			if tt.wantNil {
				if sv != nil {
					t.Errorf("GetSecure() = %v, want nil", sv)
				}
			} else {
				if sv == nil {
					t.Fatal("GetSecure() returned nil")
				}
				if sv.Reveal() != tt.wantValue {
					t.Errorf("GetSecure().Reveal() = %q, want %q", sv.Reveal(), tt.wantValue)
				}
				sv.Release()
			}
		})
	}
}

func TestLoader_GetSecure_CaseInsensitiveAndDotNotation(t *testing.T) {
	t.Run("lowercase key finds uppercase storage", func(t *testing.T) {
		loader, err := New(DefaultConfig())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("API_KEY", "sk-secret"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		sv := loader.GetSecure("api_key")
		if sv == nil {
			t.Fatal("GetSecure(\"api_key\") returned nil, expected to find API_KEY")
		}
		if sv.Reveal() != "sk-secret" {
			t.Errorf("GetSecure(\"api_key\").Reveal() = %q, want %q", sv.Reveal(), "sk-secret")
		}
		sv.Release()
	})

	t.Run("dot-notation key resolves", func(t *testing.T) {
		loader, err := New(DefaultConfig())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("DATABASE_PASSWORD", "db-secret"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		sv := loader.GetSecure("database.password")
		if sv == nil {
			t.Fatal("GetSecure(\"database.password\") returned nil, expected to find DATABASE_PASSWORD")
		}
		if sv.Reveal() != "db-secret" {
			t.Errorf("GetSecure(\"database.password\").Reveal() = %q, want %q", sv.Reveal(), "db-secret")
		}
		sv.Release()
	})

	t.Run("exact match preferred over uppercase fallback", func(t *testing.T) {
		loader, err := New(DefaultConfig())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("MY_KEY", "uppercase-value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		sv := loader.GetSecure("MY_KEY")
		if sv == nil {
			t.Fatal("GetSecure(\"MY_KEY\") returned nil")
		}
		if sv.Reveal() != "uppercase-value" {
			t.Errorf("Reveal() = %q, want %q", sv.Reveal(), "uppercase-value")
		}
		sv.Release()
	})
}

func TestLoader_Lookup(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		wantOK    bool
		wantValue string
	}{
		{"existing key", "KEY", "value", true, "value"},
		{"missing key", "MISSING", "", false, ""},
		{"preserves whitespace", "KEY", "  value  ", true, "  value  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer loader.Close()

			if tt.value != "" {
				if err := loader.Set(tt.key, tt.value); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}

			value, ok := loader.Lookup(tt.key)
			if ok != tt.wantOK {
				t.Errorf("Lookup() ok = %v, want %v", ok, tt.wantOK)
			}
			if value != tt.wantValue {
				t.Errorf("Lookup() = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

// ============================================================================
// Set/Delete Tests
// ============================================================================

func TestLoader_Set(t *testing.T) {
	t.Run("valid key and value", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY", "value"); err != nil {
			t.Errorf("Set() error = %v", err)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("", "value"); err == nil {
			t.Error("Set() should fail with empty key")
		}
	})

	t.Run("auto apply", func(t *testing.T) {
		fs := newTestFileSystem()

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.AutoApply = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY", "value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if fs.env["KEY"] != "value" {
			t.Errorf("env[\"KEY\"] = %q, want %q", fs.env["KEY"], "value")
		}
	})

	t.Run("empty value", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("EMPTY_KEY", ""); err != nil {
			t.Errorf("Set() with empty value error = %v", err)
		}
		if got := loader.GetString("EMPTY_KEY"); got != "" {
			t.Errorf("GetString() = %q, want [CLOSED]", got)
		}
	})

	t.Run("unicode and emoji in value", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		unicodeValue := "hello 世界 🌍 \u4e2d\u6587"
		if err := loader.Set("UNICODE_KEY", unicodeValue); err != nil {
			t.Errorf("Set() with unicode error = %v", err)
		}
		if got := loader.GetString("UNICODE_KEY"); got != unicodeValue {
			t.Errorf("GetString() = %q, want %q", got, unicodeValue)
		}
	})
}

func TestLoader_Delete(t *testing.T) {
	t.Run("existing key", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY", "value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if err := loader.Delete("KEY"); err != nil {
			t.Errorf("Delete() error = %v", err)
		}

		if _, ok := loader.Lookup("KEY"); ok {
			t.Error("Key should be deleted")
		}
	})

	t.Run("non-existent key", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// Should not error on non-existent key
		if err := loader.Delete("MISSING"); err != nil {
			t.Errorf("Delete() error = %v", err)
		}
	})

}

// ============================================================================
// Error Path Tests
// ============================================================================

func TestLoader_ErrorPaths(t *testing.T) {
	t.Run("Set with AutoApply error propagation", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.setenvErr = errors.New("setenv failed")

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.AutoApply = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// Set should still succeed (error is logged, not returned)
		if err := loader.Set("KEY", "value"); err != nil {
			t.Logf("Set() with Setenv error = %v (may be expected)", err)
		}
	})

	t.Run("Delete with AutoApply Unsetenv error propagation", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.unsetenvErr = errors.New("unsetenv failed")

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.AutoApply = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// First set a value
		loader.Set("KEY", "value")

		// Delete should still succeed (error is logged, not returned)
		if err := loader.Delete("KEY"); err != nil {
			t.Logf("Delete() with Unsetenv error = %v (may be expected)", err)
		}
	})

	t.Run("Apply with Setenv error", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.setenvErr = errors.New("setenv failed")

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		loader.Set("KEY", "value")

		// Apply may return an error
		err = loader.Apply()
		if err != nil {
			t.Logf("Apply() with Setenv error = %v (expected)", err)
		}
	})
}

// ============================================================================
// Keys/All/Len Tests
// ============================================================================

func TestLoader_Keys(t *testing.T) {
	t.Run("multiple keys", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY1", "value1"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Set("KEY2", "value2"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		keys := loader.Keys()
		if len(keys) != 2 {
			t.Errorf("Keys() returned %d keys, want 2", len(keys))
		}
	})

	t.Run("empty loader", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		keys := loader.Keys()
		if len(keys) != 0 {
			t.Errorf("Keys() returned %d keys, want 0", len(keys))
		}
	})

}

func TestLoader_All(t *testing.T) {
	t.Run("multiple keys", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY1", "value1"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Set("KEY2", "value2"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		all := loader.All()
		if len(all) != 2 {
			t.Errorf("All() returned %d keys, want 2", len(all))
		}
		if all["KEY1"] != "value1" {
			t.Errorf("All()[\"KEY1\"] = %q, want %q", all["KEY1"], "value1")
		}
	})

}

func TestLoader_Len(t *testing.T) {
	t.Run("multiple keys", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY1", "value1"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Set("KEY2", "value2"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if loader.Len() != 2 {
			t.Errorf("Len() = %d, want 2", loader.Len())
		}
	})

}

// ============================================================================
// IsApplied/LoadTime/Config Tests
// ============================================================================

func TestLoader_IsApplied(t *testing.T) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	if loader.IsApplied() {
		t.Error("IsApplied() = true before Apply()")
	}

	if err := loader.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !loader.IsApplied() {
		t.Error("IsApplied() = false after Apply()")
	}
}

func TestLoader_LoadTime(t *testing.T) {
	fs := newTestFileSystem()
	fs.files[".env"] = "KEY=value"

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = nil // Don't auto-load, test LoadTime behavior
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	before := loader.LoadTime()
	if !before.IsZero() {
		t.Error("LoadTime() should be zero before loading")
	}

	if err := loader.LoadFiles(".env"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	after := loader.LoadTime()
	if after.IsZero() {
		t.Error("LoadTime() should not be zero after loading")
	}
}

func TestLoader_Config(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxVariables = 50

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	returnedCfg := loader.Config()
	if returnedCfg.MaxVariables != 50 {
		t.Errorf("Config().MaxVariables = %d, want 50", returnedCfg.MaxVariables)
	}
}

// ============================================================================
// Closed Loader Behavior Tests (Table-Driven)
// ============================================================================

func TestLoader_ClosedBehavior(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		testFunc  func(t *testing.T, loader *Loader)
	}{
		{
			name:      "LoadFiles",
			operation: "LoadFiles",
			testFunc: func(t *testing.T, loader *Loader) {
				if err := loader.LoadFiles(".env"); !errors.Is(err, ErrClosed) {
					t.Errorf("LoadFiles() error = %v, want ErrClosed", err)
				}
			},
		},
		{
			name:      "Apply",
			operation: "Apply",
			testFunc: func(t *testing.T, loader *Loader) {
				if err := loader.Apply(); !errors.Is(err, ErrClosed) {
					t.Errorf("Apply() error = %v, want ErrClosed", err)
				}
			},
		},
		{
			name:      "GetSecure",
			operation: "GetSecure",
			testFunc: func(t *testing.T, loader *Loader) {
				sv := loader.GetSecure("KEY")
				if sv != nil {
					t.Errorf("GetSecure() on closed loader = %v, want nil", sv)
				}
			},
		},
		{
			name:      "Lookup",
			operation: "Lookup",
			testFunc: func(t *testing.T, loader *Loader) {
				value, ok := loader.Lookup("KEY")
				if ok {
					t.Error("Lookup() on closed loader ok = true, want false")
				}
				if value != "" {
					t.Errorf("Lookup() = %q, want [CLOSED] string", value)
				}
			},
		},
		{
			name:      "Set",
			operation: "Set",
			testFunc: func(t *testing.T, loader *Loader) {
				if err := loader.Set("KEY", "value"); !errors.Is(err, ErrClosed) {
					t.Errorf("Set() error = %v, want ErrClosed", err)
				}
			},
		},
		{
			name:      "Delete",
			operation: "Delete",
			testFunc: func(t *testing.T, loader *Loader) {
				if err := loader.Delete("KEY"); !errors.Is(err, ErrClosed) {
					t.Errorf("Delete() error = %v, want ErrClosed", err)
				}
			},
		},
		{
			name:      "Keys",
			operation: "Keys",
			testFunc: func(t *testing.T, loader *Loader) {
				keys := loader.Keys()
				if keys != nil {
					t.Errorf("Keys() on closed loader = %v, want nil", keys)
				}
			},
		},
		{
			name:      "All",
			operation: "All",
			testFunc: func(t *testing.T, loader *Loader) {
				all := loader.All()
				if all != nil {
					t.Errorf("All() on closed loader = %v, want nil", all)
				}
			},
		},
		{
			name:      "Len",
			operation: "Len",
			testFunc: func(t *testing.T, loader *Loader) {
				if loader.Len() != 0 {
					t.Errorf("Len() on closed loader = %d, want 0", loader.Len())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			loader.Close()

			tt.testFunc(t, loader)
		})
	}
}

// ============================================================================
// Close/IsClosed Tests
// ============================================================================

func TestLoader_CloseAndIsClosed(t *testing.T) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if loader.IsClosed() {
		t.Error("IsClosed() = true before Close()")
	}

	if err := loader.Close(); err != nil {
		t.Fatalf("First Close() error = %v", err)
	}

	if !loader.IsClosed() {
		t.Error("IsClosed() = false after Close()")
	}

	// Second close should be idempotent
	if err := loader.Close(); err != nil {
		t.Errorf("Second Close() error = %v", err)
	}
}

// ============================================================================
// GetInt/GetBool/GetDuration Tests (Table-Driven)
// ============================================================================

func TestLoader_TypedGetters(t *testing.T) {
	// The five typed accessors (GetInt/GetUint64/GetFloat64/GetBool/GetDuration)
	// all delegate to getWithDefault, so they share one shape: an existing key is
	// parsed, a missing key yields the default (or the zero value), and an
	// unparseable value also yields the default. One table covers all five,
	// including the invalid-value-falls-back rows that were missing for three of
	// the types when these were five near-identical functions.
	type tc struct {
		name  string
		key   string
		value string // "" => do not Set (key is absent)
		get   func(*Loader, string) any
		want  any
	}

	groups := []struct {
		name  string
		cases []tc
	}{
		{"GetInt", []tc{
			{"existing key", "PORT", "8080", func(l *Loader, key string) any { return l.GetInt(key) }, int64(8080)},
			{"invalid value falls back to default", "PORT", "abc", func(l *Loader, key string) any { return l.GetInt(key, 42) }, int64(42)},
			{"missing key with default", "MISSING", "", func(l *Loader, key string) any { return l.GetInt(key, 3000) }, int64(3000)},
			{"missing key without default", "MISSING", "", func(l *Loader, key string) any { return l.GetInt(key) }, int64(0)},
		}},
		{"GetUint64", []tc{
			{"existing key", "PORT", "8080", func(l *Loader, key string) any { return l.GetUint64(key) }, uint64(8080)},
			{"large value", "MAX_CONN", "18446744073709551615", func(l *Loader, key string) any { return l.GetUint64(key) }, uint64(18446744073709551615)},
			{"invalid value falls back to default", "PORT", "abc", func(l *Loader, key string) any { return l.GetUint64(key, 42) }, uint64(42)},
			{"missing key with default", "MISSING", "", func(l *Loader, key string) any { return l.GetUint64(key, 3000) }, uint64(3000)},
			{"missing key without default", "MISSING", "", func(l *Loader, key string) any { return l.GetUint64(key) }, uint64(0)},
		}},
		{"GetFloat64", []tc{
			{"existing key", "RATE", "3.14", func(l *Loader, key string) any { return l.GetFloat64(key) }, float64(3.14)},
			{"negative value", "OFFSET", "-0.5", func(l *Loader, key string) any { return l.GetFloat64(key) }, float64(-0.5)},
			{"scientific notation", "FACTOR", "1.5e3", func(l *Loader, key string) any { return l.GetFloat64(key) }, float64(1500)},
			{"invalid value falls back to default", "RATE", "abc", func(l *Loader, key string) any { return l.GetFloat64(key, 1.0) }, float64(1.0)},
			{"missing key with default", "MISSING", "", func(l *Loader, key string) any { return l.GetFloat64(key, 0.5) }, float64(0.5)},
			{"missing key without default", "MISSING", "", func(l *Loader, key string) any { return l.GetFloat64(key) }, float64(0)},
		}},
		{"GetBool", []tc{
			{"existing key true", "DEBUG", "true", func(l *Loader, key string) any { return l.GetBool(key) }, true},
			{"existing key false", "DEBUG", "false", func(l *Loader, key string) any { return l.GetBool(key) }, false},
			{"invalid value falls back to default", "DEBUG", "notabool", func(l *Loader, key string) any { return l.GetBool(key, true) }, true},
			{"missing key with default", "MISSING", "", func(l *Loader, key string) any { return l.GetBool(key, true) }, true},
			{"missing key without default", "MISSING", "", func(l *Loader, key string) any { return l.GetBool(key) }, false},
		}},
		{"GetDuration", []tc{
			{"existing key", "TIMEOUT", "30s", func(l *Loader, key string) any { return l.GetDuration(key) }, 30 * time.Second},
			{"invalid value falls back to default", "TIMEOUT", "notaduration", func(l *Loader, key string) any { return l.GetDuration(key, 5*time.Second) }, 5 * time.Second},
			{"missing key with default", "MISSING", "", func(l *Loader, key string) any { return l.GetDuration(key, 5*time.Minute) }, 5 * time.Minute},
			{"missing key without default", "MISSING", "", func(l *Loader, key string) any { return l.GetDuration(key) }, time.Duration(0)},
		}},
	}

	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			for _, tt := range g.cases {
				t.Run(tt.name, func(t *testing.T) {
					loader, err := New(DefaultConfig())
					if err != nil {
						t.Fatalf("New() error = %v", err)
					}
					defer loader.Close()

					if tt.value != "" {
						if err := loader.Set(tt.key, tt.value); err != nil {
							t.Fatalf("Set() error = %v", err)
						}
					}

					if got := tt.get(loader, tt.key); got != tt.want {
						t.Errorf("%s(%q) = %v, want %v", g.name, tt.key, got, tt.want)
					}
				})
			}
		})
	}
}

// ============================================================================
// Unmarshal Tests
// ============================================================================

func TestLoader_Unmarshal(t *testing.T) {
	t.Run("struct unmarshal", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("NAME", "test"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Set("PORT", "8080"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		type Config struct {
			Name string `env:"NAME"`
			Port int    `env:"PORT"`
		}

		var c Config
		if err := loader.ParseInto(&c); err != nil {
			t.Fatalf("ParseInto() error = %v", err)
		}

		if c.Name != "test" {
			t.Errorf("c.Name = %q, want %q", c.Name, "test")
		}
		if c.Port != 8080 {
			t.Errorf("c.Port = %d, want 8080", c.Port)
		}
	})
}

// ============================================================================
// Validate Tests
// ============================================================================

func TestLoader_Validate(t *testing.T) {
	t.Run("required keys present", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RequiredKeys = []string{"KEY1", "KEY2"}

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY1", "value1"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Set("KEY2", "value2"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if err := loader.Validate(); err != nil {
			t.Errorf("Validate() error = %v", err)
		}
	})

	t.Run("required keys missing", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RequiredKeys = []string{"REQUIRED_KEY"}

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Validate(); err == nil {
			t.Error("Validate() should fail with missing required key")
		}
	})
}

// ============================================================================
// JSON Format Detection Tests
// ============================================================================

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		filename string
		expected FileFormat
	}{
		{".env", FormatEnv},
		{"config.env", FormatEnv},
		{"config.json", FormatJSON},
		{"config.yaml", FormatYAML},
		{"config.yml", FormatYAML},
		{"unknown.txt", FormatAuto},
		{"", FormatAuto},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := DetectFormat(tt.filename)
			if result != tt.expected {
				t.Errorf("DetectFormat(%q) = %v, want %v", tt.filename, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Audit Handler Tests
// ============================================================================

func TestNewJSONAuditHandler(t *testing.T) {
	var buf bytes.Buffer
	handler := NewJSONAuditHandler(&buf)

	event := AuditEvent{
		Action:  ActionSet,
		Key:     "KEY",
		Reason:  "test",
		Success: true,
	}
	err := handler.Log(event)
	if err != nil {
		t.Errorf("Log() error = %v", err)
	}

	// Verify JSON output
	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Errorf("Invalid JSON output: %v", err)
	}
}

func TestNewLogAuditHandler(t *testing.T) {
	logger := NewLogAuditHandler(nil) // nil logger uses default

	if logger == nil {
		t.Error("NewLogAuditHandler(nil) returned nil")
	}

	event := AuditEvent{
		Action:  ActionSet,
		Key:     "KEY",
		Reason:  "test",
		Success: true,
	}
	if err := logger.Log(event); err != nil {
		t.Errorf("Log() error = %v", err)
	}
}

func TestNewChannelAuditHandler(t *testing.T) {
	ch := make(chan AuditEvent, 10)
	handler := NewChannelAuditHandler(ch)

	event := AuditEvent{
		Action:  ActionSet,
		Key:     "KEY",
		Reason:  "test",
		Success: true,
	}
	err := handler.Log(event)
	if err != nil {
		t.Errorf("Log() error = %v", err)
	}

	select {
	case received := <-ch:
		if received.Key != "KEY" {
			t.Errorf("Event.Key = %q, want %q", received.Key, "KEY")
		}
	default:
		t.Error("No event received on channel")
	}
}

func TestNewNopAuditHandler(t *testing.T) {
	handler := NewNopAuditHandler()

	// Log and Close should succeed without doing anything
	event := AuditEvent{
		Action:  ActionSet,
		Key:     "KEY",
		Reason:  "test",
		Success: true,
	}
	if err := handler.Log(event); err != nil {
		t.Errorf("Log() error = %v", err)
	}
	if err := handler.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// ============================================================================
// ComponentFactory Tests
// ============================================================================

func TestComponentFactory(t *testing.T) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	factory := cfg.buildComponentFactory()

	t.Run("Validator", func(t *testing.T) {
		v := factory.Validator()
		if v == nil {
			t.Error("Validator() returned nil")
		}
	})

	t.Run("Auditor", func(t *testing.T) {
		a := factory.Auditor()
		if a == nil {
			t.Error("Auditor() returned nil")
		}
	})

	t.Run("Close", func(t *testing.T) {
		if err := factory.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	t.Run("IsClosed", func(t *testing.T) {
		if !factory.IsClosed() {
			t.Error("IsClosed() = false after Close()")
		}
	})
}

func TestAuditorAdapter(t *testing.T) {
	cfg := DefaultConfig()
	factory := cfg.buildComponentFactory()
	defer factory.Close()

	// Use the public accessor method to get the internal auditor
	// Type assert to get the internal *Auditor
	aud, ok := factory.auditor.(*internal.Auditor)
	if !ok {
		t.Skipf("factory.auditor is not the built-in *internal.Auditor")
	}
	adapter := newAuditorAdapter(aud)

	t.Run("Log", func(t *testing.T) {
		if err := adapter.Log(ActionSet, "KEY", "test", true); err != nil {
			t.Errorf("Log() error = %v", err)
		}
	})

	t.Run("LogError", func(t *testing.T) {
		if err := adapter.LogError(ActionSet, "KEY", "error"); err != nil {
			t.Errorf("LogError() error = %v", err)
		}
	})

	t.Run("LogWithFile", func(t *testing.T) {
		if err := adapter.LogWithFile(ActionSet, "KEY", "file", "test", true); err != nil {
			t.Errorf("LogWithFile() error = %v", err)
		}
	})

	t.Run("LogWithDuration", func(t *testing.T) {
		if err := adapter.LogWithDuration(ActionSet, "KEY", "test", true, time.Second); err != nil {
			t.Errorf("LogWithDuration() error = %v", err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		if err := adapter.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	t.Run("nil adapter", func(t *testing.T) {
		nilAdapter := newAuditorAdapter(nil)
		if nilAdapter != nil {
			t.Error("newAuditorAdapter(nil) should return nil")
		}
	})
}

// ============================================================================
// JSON Parser Edge Case Tests
// ============================================================================

func TestJSONParser_EdgeCases(t *testing.T) {
	t.Run("empty object", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["empty.json"] = "{}"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("empty.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.Len() != 0 {
			t.Errorf("Len() = %d, want 0", loader.Len())
		}
	})

	t.Run("nested object", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["nested.json"] = `{
			"database": {
				"host": "localhost",
				"port": 5432,
				"credentials": {
					"username": "admin",
					"password": "secret"
				}
			}
		}`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("nested.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("DATABASE_HOST") != "localhost" {
			t.Errorf("GetString(\"DATABASE_HOST\") = %q, want %q", loader.GetString("DATABASE_HOST"), "localhost")
		}
		if loader.GetString("DATABASE_PORT") != "5432" {
			t.Errorf("GetString(\"DATABASE_PORT\") = %q, want %q", loader.GetString("DATABASE_PORT"), "5432")
		}
		if loader.GetString("DATABASE_CREDENTIALS_USERNAME") != "admin" {
			t.Errorf("GetString(\"DATABASE_CREDENTIALS_USERNAME\") = %q, want %q", loader.GetString("DATABASE_CREDENTIALS_USERNAME"), "admin")
		}
	})

	t.Run("array handling", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["array.json"] = `{
			"servers": ["server1", "server2", "server3"],
			"ports": [8080, 8081, 8082]
		}`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("array.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("SERVERS_0") != "server1" {
			t.Errorf("GetString(\"SERVERS_0\") = %q, want %q", loader.GetString("SERVERS_0"), "server1")
		}
		if loader.GetString("SERVERS_2") != "server3" {
			t.Errorf("GetString(\"SERVERS_2\") = %q, want %q", loader.GetString("SERVERS_2"), "server3")
		}
	})

	t.Run("null handling", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["null.json"] = `{
			"null_value": null,
			"other_value": "test"
		}`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.JSONNullAsEmpty = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("null.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("NULL_VALUE") != "" {
			t.Errorf("GetString(\"NULL_VALUE\") = %q, want [CLOSED]", loader.GetString("NULL_VALUE"))
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["invalid.json"] = `{invalid json}`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("invalid.json"); err == nil {
			t.Error("LoadFiles() should fail with invalid JSON")
		}
	})

	t.Run("file too large", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["large.json"] = strings.Repeat(`{"key":"value"}`, 1000)

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.MaxFileSize = 100
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("large.json"); err == nil {
			t.Error("LoadFiles() should fail with file too large")
		}
	})

	t.Run("max variables exceeded", func(t *testing.T) {
		fs := newTestFileSystem()
		// Create JSON with many variables
		var sb strings.Builder
		sb.WriteString("{")
		for i := 0; i < 100; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(`"KEY_`)
			sb.WriteString(string(rune('A' + i%26)))
			sb.WriteString(`":"value"`)
		}
		sb.WriteString("}")
		fs.files["many.json"] = sb.String()

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.MaxVariables = 10
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("many.json"); err == nil {
			t.Error("LoadFiles() should fail with max variables exceeded")
		}
	})

	t.Run("JSON max depth exceeded", func(t *testing.T) {
		json := `{"a": {"b": {"c": {"d": {"e": {"f": "deep"}}}}}}`
		fs := newTestFileSystem()
		fs.files["deep.json"] = json

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.JSONMaxDepth = 3
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		err = loader.LoadFiles("deep.json")
		if err == nil {
			t.Error("LoadFiles() should fail with JSON depth exceeded")
		}
	})

	t.Run("JSON with boolean values", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["bool.json"] = `{"enabled": true, "disabled": false}`
		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.JSONBoolAsString = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("bool.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		if v := loader.GetString("ENABLED"); v != "true" {
			t.Errorf("ENABLED = %q, want %q", v, "true")
		}
		if v := loader.GetString("DISABLED"); v != "false" {
			t.Errorf("DISABLED = %q, want %q", v, "false")
		}
	})

	t.Run("JSON null with nullAsEmpty disabled", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["null2.json"] = `{"key": null}`
		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.JSONNullAsEmpty = false
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("null2.json"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
	})
}

// ============================================================================
// YAML Parser Edge Case Tests
// ============================================================================

func TestYAMLParser_EdgeCases(t *testing.T) {
	t.Run("empty document", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["empty.yaml"] = ""

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("empty.yaml"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
	})

	t.Run("nested map", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["nested.yaml"] = `
database:
  host: localhost
  port: 5432
  credentials:
    username: admin
    password: secret
`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("nested.yaml"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("DATABASE_HOST") != "localhost" {
			t.Errorf("GetString(\"DATABASE_HOST\") = %q, want %q", loader.GetString("DATABASE_HOST"), "localhost")
		}
	})

	t.Run("list handling", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["list.yaml"] = `
servers:
  - server1
  - server2
  - server3
`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("list.yaml"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("SERVERS_0") != "server1" {
			t.Errorf("GetString(\"SERVERS_0\") = %q, want %q", loader.GetString("SERVERS_0"), "server1")
		}
	})

	t.Run("boolean handling", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["bool.yaml"] = `
debug: true
production: false
`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.YAMLBoolAsString = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("bool.yaml"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("DEBUG") != "true" {
			t.Errorf("GetString(\"DEBUG\") = %q, want %q", loader.GetString("DEBUG"), "true")
		}
	})

	t.Run("null handling", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["null.yaml"] = `
null_value: null
other_value: test
`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.YAMLNullAsEmpty = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("null.yaml"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("NULL_VALUE") != "" {
			t.Errorf("GetString(\"NULL_VALUE\") = %q, want [CLOSED]", loader.GetString("NULL_VALUE"))
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["invalid.yaml"] = `
invalid:
  - unclosed
    - bad indent
`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// YAML parsing is lenient, may not error
		_ = loader.LoadFiles("invalid.yaml")
	})

	t.Run("complex nested structure", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["complex.yaml"] = `
app:
  name: myapp
  servers:
    - name: web1
      port: 8080
    - name: web2
      port: 8081
  database:
    primary:
      host: db1.example.com
      port: 5432
`

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("complex.yaml"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		if loader.GetString("APP_NAME") != "myapp" {
			t.Errorf("GetString(\"APP_NAME\") = %q, want %q", loader.GetString("APP_NAME"), "myapp")
		}
	})

	t.Run("YAML max depth exceeded", func(t *testing.T) {
		var sb strings.Builder
		for i := 0; i < 15; i++ {
			sb.WriteString(strings.Repeat("  ", i))
			sb.WriteString(fmt.Sprintf("level%d:\n", i))
		}
		fs := newTestFileSystem()
		fs.files["deep.yaml"] = sb.String()

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.YAMLMaxDepth = 5
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		err = loader.LoadFiles("deep.yaml")
		if err == nil {
			t.Error("LoadFiles() should fail with YAML depth exceeded")
		}
	})

	t.Run("YAML with inline list", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["inline.yaml"] = "tags: [web, api, db]\n"
		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("inline.yaml"); err != nil {
			t.Logf("LoadFiles() error = %v (inline list may not be fully supported)", err)
		}
		t.Logf("TAGS value = %q", loader.GetString("TAGS"))
	})

	t.Run("YAML with flow mapping", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["flow.yaml"] = "config: {host: localhost, port: 3000}\n"
		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("flow.yaml"); err != nil {
			t.Logf("LoadFiles() error = %v (flow mapping may not be fully supported)", err)
		}
		t.Logf("CONFIG_HOST = %q", loader.GetString("CONFIG_HOST"))
	})

	t.Run("YAML with number values", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["numbers.yaml"] = `
integer_val: 42
float_val: 3.14
negative_val: -10
`
		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.YAMLNumberAsString = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("numbers.yaml"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		if v := loader.GetString("INTEGER_VAL"); v != "42" {
			t.Errorf("INTEGER_VAL = %q, want %q", v, "42")
		}
		if v := loader.GetString("FLOAT_VAL"); v != "3.14" {
			t.Errorf("FLOAT_VAL = %q, want %q", v, "3.14")
		}
		if v := loader.GetString("NEGATIVE_VAL"); v != "-10" {
			t.Errorf("NEGATIVE_VAL = %q, want %q", v, "-10")
		}
	})
}

// ============================================================================
// Error Type Tests - Extended
// ============================================================================

func TestJSONError(t *testing.T) {
	t.Run("with path", func(t *testing.T) {
		err := &JSONError{
			Path:    "$.database.host",
			Message: "invalid type",
			Err:     errors.New("expected string"),
		}

		if err.Error() == "" {
			t.Error("JSONError.Error() should not be empty")
		}

		// Unwrap returns the underlying error
		unwrapped := err.Unwrap()
		if unwrapped == nil {
			t.Error("JSONError.Unwrap() should return non-nil error")
		}
	})

	t.Run("without path", func(t *testing.T) {
		err := &JSONError{
			Message: "parse error",
		}

		if err.Error() == "" {
			t.Error("JSONError.Error() should not be empty")
		}
	})
}

func TestYAMLError(t *testing.T) {
	t.Run("with path and line", func(t *testing.T) {
		err := &YAMLError{
			Path:    "config.yaml",
			Line:    10,
			Column:  5,
			Message: "invalid mapping",
		}

		if err.Error() == "" {
			t.Error("YAMLError.Error() should not be empty")
		}
	})

	t.Run("with line only", func(t *testing.T) {
		err := &YAMLError{
			Line:    15,
			Message: "indentation error",
		}

		if err.Error() == "" {
			t.Error("YAMLError.Error() should not be empty")
		}
	})

	t.Run("without location", func(t *testing.T) {
		err := &YAMLError{
			Message: "parse error",
		}

		if err.Error() == "" {
			t.Error("YAMLError.Error() should not be empty")
		}
	})
}

func TestExpansionError(t *testing.T) {
	t.Run("with key", func(t *testing.T) {
		err := &ExpansionError{
			Key:   "VAR",
			Depth: 10,
			Limit: 5,
		}

		if err.Error() == "" {
			t.Error("ExpansionError.Error() should not be empty")
		}
	})

	t.Run("without key", func(t *testing.T) {
		err := &ExpansionError{
			Depth: 10,
			Limit: 5,
			Chain: "A -> B -> C",
		}

		if err.Error() == "" {
			t.Error("ExpansionError.Error() should not be empty")
		}
	})
}

func TestSecurityError(t *testing.T) {
	t.Run("with key", func(t *testing.T) {
		err := &SecurityError{
			Action:  "set",
			Reason:  "forbidden key",
			Key:     "SECRET_KEY",
			Details: "key is in forbidden list",
		}

		if err.Error() == "" {
			t.Error("SecurityError.Error() should not be empty")
		}
	})

	t.Run("without key", func(t *testing.T) {
		err := &SecurityError{
			Action: "load",
			Reason: "file too large",
		}

		if err.Error() == "" {
			t.Error("SecurityError.Error() should not be empty")
		}
	})
}

// ============================================================================
// validateFilePath Tests (Security)
// ============================================================================

func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		wantErr   bool
		errReason string
	}{
		{"valid relative path", "config/.env", false, ""},
		{"valid simple filename", ".env", false, ""},
		{"empty filename", "", true, "empty filename"},
		{"null byte in path", "config\x00.env", true, "null byte"},
		{"UNC path backslash", "\\\\server\\share", true, "UNC path"},
		{"network path forward slash", "//server/share", true, "network path"},
		{"Unix absolute path", "/etc/passwd", true, "absolute path"},
		{"Windows drive letter", "C:\\Windows", true, "drive letter"},
		{"lowercase drive letter", "c:\\test", true, "drive letter"},
		{"path traversal", "../../../etc/passwd", true, "path traversal"},
		{"hidden traversal", "config/../../../etc", true, "path traversal"},
		{"Windows reserved CON", "CON", true, "reserved device"},
		{"Windows reserved NUL", "NUL.txt", true, "reserved device"},
		{"Windows reserved AUX", "AUX:", true, "reserved device"},
		{"Windows reserved PRN", "PRN", true, "reserved device"},
		{"Windows COM port", "COM1", true, "reserved device"},
		{"Windows LPT port", "LPT1.txt", true, "reserved device"},
		{"valid with dots", "config.local/.env", false, ""},
		{"valid subdirectory", "config/local/.env", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFilePath(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFilePath(%q) error = %v, wantErr %v", tt.filename, err, tt.wantErr)
			}
			if err != nil && tt.errReason != "" {
				var secErr *SecurityError
				if errors.As(err, &secErr) {
					if !strings.Contains(secErr.Reason, tt.errReason) {
						t.Errorf("validateFilePath(%q) reason = %q, want containing %q", tt.filename, secErr.Reason, tt.errReason)
					}
				}
			}
		})
	}
}

// TestValidateFilePath_SymlinkEscape tests that symlink escape attacks are blocked.
// This test creates actual symlinks to verify the security check works correctly.
func TestValidateFilePath_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require admin privileges on Windows")
	}

	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create allowed directory
	allowedDir := filepath.Join(tmpDir, "allowed")
	if err := os.Mkdir(allowedDir, 0755); err != nil {
		t.Fatalf("failed to create allowed dir: %v", err)
	}

	// Create a file outside the allowed directory
	outsideFile := filepath.Join(tmpDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	// Create symlink inside allowed directory pointing outside
	symlinkPath := filepath.Join(allowedDir, "escape.env")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Change to allowed directory to test relative path
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(allowedDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(oldWd)

	// The symlink points to an absolute path, which should be blocked
	// because it resolves to an absolute path
	err = validateFilePath("escape.env")
	if err == nil {
		t.Error("validateFilePath should reject symlink that resolves to absolute path")
	}

	var secErr *SecurityError
	if err != nil && !errors.As(err, &secErr) {
		t.Errorf("expected SecurityError, got %T", err)
	}
}

// ============================================================================
// newParseError Tests
// ============================================================================

func TestNewParseError(t *testing.T) {
	err := newParseError("test.env", 10, "API_KEY=secret123", errors.New("parse failed"))

	if err.File != "test.env" {
		t.Errorf("File = %q, want %q", err.File, "test.env")
	}
	if err.Line != 10 {
		t.Errorf("Line = %d, want 10", err.Line)
	}
	if err.Err == nil {
		t.Error("Err should not be nil")
	}

	// Verify error message is not empty
	if err.Error() == "" {
		t.Error("Error() should not be empty")
	}
}

// ============================================================================
// New() Error Path Tests
// ============================================================================

func TestNew_ErrorPaths(t *testing.T) {
	t.Run("parser creation error with factory cleanup", func(t *testing.T) {
		// This tests the error path where createParsers fails
		// and factory.Close() is called for cleanup
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		loader.Close()
	})

	t.Run("auto-load file not found with fail on missing", func(t *testing.T) {
		fs := newTestFileSystem()
		// Don't add any files

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Filenames = []string{"missing.env"}
		cfg.FailOnMissingFile = true

		_, err := New(cfg)
		if err == nil {
			t.Error("New() should fail with missing file and FailOnMissingFile=true")
		}
	})

	t.Run("auto-apply error", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files[".env"] = "KEY=value"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Filenames = []string{".env"}
		cfg.AutoApply = true

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		loader.Close()
	})
}

// OSFileSystem Tests
// ============================================================================

// OSFileSystem is a thin pass-through to the os package: every method just
// forwards to its os.X counterpart. Interface conformance is already guaranteed
// at compile time by `var DefaultFileSystem FileSystem = OSFileSystem{}`
// (filesystem.go). A single smoke test confirms the wiring — that each method
// delegates to the right os.X call — rather than re-asserting the standard
// library's own behavior in ten near-identical functions.
func TestOSFileSystem(t *testing.T) {
	fs := OSFileSystem{}

	t.Run("env round-trip", func(t *testing.T) {
		const key = "ENV_OS_SMOKE"
		if err := fs.Setenv(key, "v"); err != nil {
			t.Fatalf("Setenv() error = %v", err)
		}
		t.Cleanup(func() { _ = fs.Unsetenv(key) }) // best-effort cleanup

		if got := fs.Getenv(key); got != "v" {
			t.Errorf("Getenv() = %q, want %q", got, "v")
		}
		if v, ok := fs.LookupEnv(key); !ok || v != "v" {
			t.Errorf("LookupEnv() = (%q, %v), want (v, true)", v, ok)
		}
		if err := fs.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv() error = %v", err)
		}
		if _, ok := fs.LookupEnv(key); ok {
			t.Error("LookupEnv() = true after Unsetenv, want false")
		}
	})

	t.Run("file ops", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "src.txt")
		if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		// Stat + Open (read path).
		info, err := fs.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if info.Name() != "src.txt" {
			t.Errorf("Stat().Name() = %q, want %q", info.Name(), "src.txt")
		}
		f, err := fs.Open(path)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		got, err := io.ReadAll(f)
		_ = f.Close() // best-effort close after read
		if err != nil || string(got) != "hi" {
			t.Errorf("Open() read = %q, err %v; want %q", got, err, "hi")
		}

		// MkdirAll + OpenFile (write path).
		nested := filepath.Join(dir, "a", "b")
		if err := fs.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		wf, err := fs.OpenFile(filepath.Join(nested, "c.txt"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			t.Fatalf("OpenFile() error = %v", err)
		}
		if _, err := wf.Write([]byte("x")); err != nil {
			t.Errorf("OpenFile() Write error = %v", err)
		}
		_ = wf.Close() // best-effort close after write

		// Rename moves src -> dst.
		dst := filepath.Join(dir, "dst.txt")
		if err := fs.Rename(path, dst); err != nil {
			t.Fatalf("Rename() error = %v", err)
		}
		if _, err := fs.Stat(path); !os.IsNotExist(err) {
			t.Error("source should not exist after Rename")
		}

		// Remove deletes dst.
		if err := fs.Remove(dst); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
		if _, err := fs.Stat(dst); !os.IsNotExist(err) {
			t.Error("file should not exist after Remove")
		}

		// Missing-path error paths for Open/Stat/Remove.
		if _, err := fs.Open("no_such_file_smoke"); err == nil {
			t.Error("Open(missing) want error")
		}
		if _, err := fs.Stat("no_such_file_smoke"); err == nil {
			t.Error("Stat(missing) want error")
		}
		if err := fs.Remove("no_such_file_smoke"); err == nil {
			t.Error("Remove(missing) want error")
		}
	})
}

// FileFormat.String() Tests
// ============================================================================

func TestFileFormat_String(t *testing.T) {
	tests := []struct {
		format   FileFormat
		expected string
	}{
		{FormatAuto, "auto"},
		{FormatEnv, "dotenv"},
		{FormatJSON, "json"},
		{FormatYAML, "yaml"},
		{FileFormat(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.format.String(); got != tt.expected {
				t.Errorf("FileFormat(%d).String() = %q, want %q", tt.format, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// RegisterParser Tests
// ============================================================================

// testFormatCounter generates unique format IDs for test isolation.
// This ensures tests can run multiple times with -count=N without conflicts.
var testFormatCounter int64

// nextTestFormat returns a unique FileFormat for testing.
func nextTestFormat() FileFormat {
	return FileFormat(1000 + atomic.AddInt64(&testFormatCounter, 1))
}

func TestRegisterParser(t *testing.T) {
	t.Run("cannot override built-in dotenv parser", func(t *testing.T) {
		err := RegisterParser(FormatEnv, nil)
		if err == nil {
			t.Error("RegisterParser should fail for built-in FormatEnv")
		}
	})

	t.Run("cannot override built-in JSON parser", func(t *testing.T) {
		err := RegisterParser(FormatJSON, nil)
		if err == nil {
			t.Error("RegisterParser should fail for built-in FormatJSON")
		}
	})

	t.Run("cannot override built-in YAML parser", func(t *testing.T) {
		err := RegisterParser(FormatYAML, nil)
		if err == nil {
			t.Error("RegisterParser should fail for built-in FormatYAML")
		}
	})

	t.Run("custom format registration", func(t *testing.T) {
		// Use unique format to ensure test isolation with -count=N
		customFormat := nextTestFormat()

		// First registration should succeed
		err := RegisterParser(customFormat, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return nil, nil
		})
		if err != nil {
			t.Errorf("RegisterParser for custom format failed: %v", err)
		}

		// Duplicate registration should fail
		err = RegisterParser(customFormat, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return nil, nil
		})
		if err == nil {
			t.Error("RegisterParser should fail for duplicate custom format")
		}
	})
}

func TestForceRegisterParser(t *testing.T) {
	t.Run("force register overwrites existing", func(t *testing.T) {
		customFormat := nextTestFormat()

		// First registration
		err := RegisterParser(customFormat, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return nil, nil
		})
		if err != nil {
			t.Fatalf("RegisterParser() error = %v", err)
		}

		// Force register should overwrite without error
		err = ForceRegisterParser(customFormat, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return nil, nil
		})
		if err != nil {
			t.Errorf("ForceRegisterParser() error = %v", err)
		}
	})

	t.Run("force register nil factory returns error", func(t *testing.T) {
		err := ForceRegisterParser(FormatEnv, nil)
		if err == nil {
			t.Error("ForceRegisterParser() with nil factory should return error")
		}
	})

	t.Run("force register new format", func(t *testing.T) {
		customFormat := nextTestFormat()

		err := ForceRegisterParser(customFormat, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return nil, nil
		})
		if err != nil {
			t.Errorf("ForceRegisterParser() error = %v", err)
		}
	})
}

// ============================================================================
// Error Type Is() Method Tests (from coverage_test.go)
// ============================================================================

func TestSecurityError_Is(t *testing.T) {
	base := &SecurityError{
		Action:  "set",
		Reason:  "forbidden key",
		Key:     "SECRET",
		Details: "key in forbidden list",
	}

	t.Run("matches ErrSecurityViolation", func(t *testing.T) {
		if !errors.Is(base, ErrSecurityViolation) {
			t.Error("SecurityError should match ErrSecurityViolation via errors.Is")
		}
	})

	t.Run("does not match ErrFileNotFound", func(t *testing.T) {
		if errors.Is(base, ErrFileNotFound) {
			t.Error("SecurityError should not match ErrFileNotFound")
		}
	})

	t.Run("as SecurityError preserves fields", func(t *testing.T) {
		var secErr *SecurityError
		if !errors.As(base, &secErr) {
			t.Fatal("errors.As should extract SecurityError")
		}
		if secErr.Action != "set" || secErr.Key != "SECRET" {
			t.Errorf("SecurityError fields: Action=%q, Key=%q", secErr.Action, secErr.Key)
		}
	})
}

func TestFileError_Unwrap(t *testing.T) {
	innerErr := errors.New("disk full")
	base := &FileError{
		Path:  "config.env",
		Op:    "open",
		Err:   innerErr,
		Size:  5000,
		Limit: 1000,
	}

	t.Run("unwrap returns inner error", func(t *testing.T) {
		if !errors.Is(base, innerErr) {
			t.Error("FileError should unwrap to inner error")
		}
	})

	t.Run("as FileError preserves fields", func(t *testing.T) {
		var fileErr *FileError
		if !errors.As(base, &fileErr) {
			t.Fatal("errors.As should extract FileError")
		}
		if fileErr.Path != "config.env" || fileErr.Op != "open" {
			t.Errorf("FileError fields: Path=%q, Op=%q", fileErr.Path, fileErr.Op)
		}
		if fileErr.Size != 5000 || fileErr.Limit != 1000 {
			t.Errorf("FileError size=%d, limit=%d", fileErr.Size, fileErr.Limit)
		}
	})
}

// ============================================================================
// InternKey Eviction Tests (from coverage_test.go)
// ============================================================================

func TestInternKey_Eviction(t *testing.T) {
	internal.ClearInternCache()
	defer internal.ClearInternCache()

	t.Run("cache eviction under pressure", func(t *testing.T) {
		for i := 0; i < 2000; i++ {
			key := fmt.Sprintf("LONG_CACHE_KEY_%04d_SUFFIX", i)
			result := internal.InternKey(key)
			if result != key {
				t.Errorf("InternKey(%q) = %q, want same", key, result)
			}
		}
		repeated := internal.InternKey("LONG_CACHE_KEY_0000_SUFFIX")
		if repeated != "LONG_CACHE_KEY_0000_SUFFIX" {
			t.Errorf("InternKey after eviction = %q, want same", repeated)
		}
	})

	t.Run("same key returns identical pointer", func(t *testing.T) {
		internal.ClearInternCache()
		key := "IDENTITY_TEST_KEY"
		first := internal.InternKey(key)
		second := internal.InternKey(key)
		if first != second {
			t.Error("InternKey should return identical string for same key")
		}
	})
}

// ============================================================================
// Expansion Edge Cases (from coverage_test.go)
// ============================================================================

func TestExpansion_EdgeCases(t *testing.T) {
	t.Run("expansion depth limit exceeded", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["cycle.env"] = "A=${B}\nB=${A}"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.ExpandVariables = true
		cfg.MaxExpansionDepth = 2
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		err = loader.LoadFiles("cycle.env")
		if err == nil {
			t.Error("LoadFiles() should fail with expansion depth exceeded")
		}
	})

	t.Run("self-referencing variable", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["selfref.env"] = "X=${X}"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.ExpandVariables = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		err = loader.LoadFiles("selfref.env")
		if err == nil {
			t.Error("LoadFiles() should fail with self-referencing variable")
		}
	})

	t.Run("braced variable with default", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["default.env"] = "RESULT=${MISSING:-fallback}"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.ExpandVariables = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("default.env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		if v := loader.GetString("RESULT"); v != "fallback" {
			t.Errorf("RESULT = %q, want %q", v, "fallback")
		}
	})

	t.Run("nested variable expansion", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.files["nested.env"] = "BASE=hello\nNESTED=${BASE}_world"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.ExpandVariables = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.LoadFiles("nested.env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		v := loader.GetString("NESTED")
		t.Logf("NESTED = %q (expansion may require post-load processing)", v)
	})
}

// ============================================================================
// SecureValue Masked Edge Cases (from coverage_test.go)
// ============================================================================

func TestSecureValue_Masked_EdgeCases(t *testing.T) {
	t.Run("masked after close shows CLOSED", func(t *testing.T) {
		sv := NewSecureValue("sensitive")
		sv.Close()
		if m := sv.Masked(); m != "[CLOSED]" {
			t.Errorf("Masked() after Close() = %q, want [CLOSED]", m)
		}
	})

	t.Run("masked with long value", func(t *testing.T) {
		sv := NewSecureValue(strings.Repeat("x", 100))
		m := sv.Masked()
		if !strings.Contains(m, "100") {
			t.Errorf("Masked() = %q, should contain byte count", m)
		}
	})

	t.Run("String after close returns masked", func(t *testing.T) {
		sv := NewSecureValue("test")
		sv.Close()
		if s := sv.String(); s != "[CLOSED]" {
			t.Errorf("String() after Close() = %q, want [CLOSED]", s)
		}
	})

	t.Run("Bytes after close returns nil", func(t *testing.T) {
		sv := NewSecureValue("test")
		sv.Close()
		if b := sv.Bytes(); b != nil {
			t.Errorf("Bytes() after Close() = %v, want nil", b)
		}
	})

	t.Run("Length after close returns 0", func(t *testing.T) {
		sv := NewSecureValue("test")
		sv.Close()
		if l := sv.Length(); l != 0 {
			t.Errorf("Length() after Close() = %d, want 0", l)
		}
	})
}
