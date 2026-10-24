package env

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	// The loader checks size via the FILE's Stat() (env.go loadFileLocked),
	// not via FileSystem.Stat, so an injected statErr must fail on the
	// returned file to exercise the FileError{"stat"} path.
	return &testFile{content: content, statErr: fs.statErr}, nil
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
	statErr error // when non-nil, Stat() fails with this error
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
	if f.statErr != nil {
		return nil, f.statErr
	}
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
	t.Run("no arguments - uses default config", func(t *testing.T) {
		loader, err := New()
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if loader == nil {
			t.Fatal("New() returned nil loader")
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

		returnedCfg := loader.Config()
		if returnedCfg.JSONMaxDepth != 20 {
			t.Errorf("JSONMaxDepth = %d, want 20", returnedCfg.JSONMaxDepth)
		}
		if returnedCfg.MaxVariables != 100 {
			t.Errorf("MaxVariables = %d, want 100", returnedCfg.MaxVariables)
		}
	})

	t.Run("custom key pattern accepted", func(t *testing.T) {
		cfg := DefaultConfig()
		// A namespace pattern need not match any particular sample key —
		// validation only rejects patterns that would accept unsafe keys —
		// and is then enforced when keys are validated. (The rejection side
		// of a custom pattern is covered by TestLoader_Set_KeyRules.)
		cfg.KeyPattern = regexp.MustCompile(`^MYAPP_[A-Z0-9_]*$`)
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("MYAPP_KEY", "value"); err != nil {
			t.Fatalf("Set(key matching custom pattern) error = %v", err)
		}
	})

	t.Run("multiple configs are rejected", func(t *testing.T) {
		cfgA := DefaultConfig()
		cfgA.Filenames = nil // don't auto-load
		cfgA.MaxVariables = 100
		cfgB := DefaultConfig()
		cfgB.MaxVariables = 200

		_, err := New(cfgA, cfgB)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("New(cfgA, cfgB) error = %v, want ErrInvalidConfig "+
				"(extra Configs must be rejected, not silently ignored)", err)
		}
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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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

	t.Run("unknown extension falls back to env parser", func(t *testing.T) {
		// DetectFormat maps ".txt" to FormatAuto; LoadFiles must fall back to
		// the .env parser rather than failing (env.go loadFileLocked).
		fs := newTestFileSystem()
		fs.files["notes.txt"] = "KEY=value"

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.LoadFiles("notes.txt"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}
		if loader.GetString("KEY") != "value" {
			t.Errorf("GetString(\"KEY\") = %q, want %q", loader.GetString("KEY"), "value")
		}
	})

}

// TestLoader_LoadFiles_FileSystemErrors drives the mock's openErr/statErr
// injection fields through LoadFiles: both must surface as *FileError with
// the failing operation recorded and the original error wrapped.
func TestLoader_LoadFiles_FileSystemErrors(t *testing.T) {
	openErr := errors.New("disk detached")
	statErr := errors.New("stat corrupted")

	tests := []struct {
		name   string
		fsMod  func(fs *testFileSystem)
		wantOp string
		wantIs error
	}{
		{
			name:   "open failure returns FileError wrapping original",
			fsMod:  func(fs *testFileSystem) { fs.openErr = openErr },
			wantOp: "open",
			wantIs: openErr,
		},
		{
			name:   "stat failure returns FileError wrapping original",
			fsMod:  func(fs *testFileSystem) { fs.statErr = statErr },
			wantOp: "stat",
			wantIs: statErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newTestFileSystem()
			fs.files["broken.env"] = "KEY=value"
			tt.fsMod(fs)

			cfg := DefaultConfig()
			cfg.FileSystem = fs
			cfg.Filenames = nil // don't auto-load
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			err = loader.LoadFiles("broken.env")
			var fileErr *FileError
			if !errors.As(err, &fileErr) {
				t.Fatalf("LoadFiles() error = %v (%T), want *FileError", err, err)
			}
			if fileErr.Op != tt.wantOp {
				t.Errorf("FileError.Op = %q, want %q", fileErr.Op, tt.wantOp)
			}
			if !errors.Is(err, tt.wantIs) {
				t.Errorf("LoadFiles() error = %v, want it wrapping %v", err, tt.wantIs)
			}
		})
	}
}

// TestLoader_ExpandAmplificationBounded drives the GEN-001 amplification
// bound through the full .env pipeline: a multi-reference DAG doubles per
// level without cycling, and the raw-value length check cannot see it because
// it validates file text before expansion. The factory wires
// LimitsConfig.MaxValueLength into the expander, so LoadFiles must fail with
// a max_length ValidationError instead of materializing the amplified values.
func TestLoader_ExpandAmplificationBounded(t *testing.T) {
	fs := newTestFileSystem()
	// A=1024 bytes, B=2049, C=4099, D=8199 — one byte over the default
	// MaxValueLength (8192) after expansion, at depth 4 of an allowed 5.
	seed := strings.Repeat("x", 1024)
	fs.files["amplify.env"] = "A=" + seed + "\nB=${A}.${A}\nC=${B}.${B}\nD=${C}.${C}\n"

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	err = loader.LoadFiles("amplify.env")
	if err == nil {
		t.Fatal("LoadFiles() error = nil, want amplified value over the limit to fail the load")
	}
	var vErr *ValidationError
	if !errors.As(err, &vErr) || vErr.Rule != "max_length" {
		t.Errorf("LoadFiles() error = %v, want *ValidationError with rule max_length", err)
	}
}

// TestLoader_LoadLimits_AtBoundary pins the off-by-one semantics of the size
// and count limits: a file exactly MaxFileSize bytes and a file with exactly
// MaxVariables keys both load — the size check is strictly greater-than
// (env.go) and the variable check fires only beyond the limit (parser.go).
func TestLoader_LoadLimits_AtBoundary(t *testing.T) {
	const content = "A=1\nB=2\nC=3\n"

	tests := []struct {
		name   string
		cfgMod func(*Config)
	}{
		{
			name:   "file size exactly MaxFileSize succeeds",
			cfgMod: func(c *Config) { c.MaxFileSize = int64(len(content)) },
		},
		{
			name:   "variable count exactly MaxVariables succeeds",
			cfgMod: func(c *Config) { c.MaxVariables = 3 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newTestFileSystem()
			fs.files["edge.env"] = content

			cfg := DefaultConfig()
			cfg.FileSystem = fs
			cfg.Filenames = nil // don't auto-load
			tt.cfgMod(&cfg)

			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			if err := loader.LoadFiles("edge.env"); err != nil {
				t.Fatalf("LoadFiles() error = %v, want nil at the exact limit", err)
			}
			if loader.Len() != 3 {
				t.Errorf("Len() = %d, want 3", loader.Len())
			}
		})
	}
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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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

	t.Run("apply skips key explicitly set to empty string", func(t *testing.T) {
		// applyLocked uses LookupEnv to distinguish "not set" from "set to
		// the empty string": a variable explicitly set to "" counts as
		// existing and must NOT be applied over when OverwriteExisting is
		// false (env.go).
		fs := newTestFileSystem()
		fs.env["EXISTING"] = "" // set, but to the empty string

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.OverwriteExisting = false
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("EXISTING", "value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Apply(); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if got, ok := fs.LookupEnv("EXISTING"); !ok || got != "" {
			t.Errorf("env[\"EXISTING\"] = (%q, %v), want (\"\", true): the key must NOT be applied over an existing empty value", got, ok)
		}
	})

	t.Run("apply partial failure masks key and keeps earlier keys", func(t *testing.T) {
		fs := &ltSetenvRecorderFS{testFileSystem: newTestFileSystem()}
		fs.files["app.env"] = "KEY_A=a\nKEY_B=b\nKEY_C=c\n"
		fs.failKey = "KEY_B"
		fs.failErr = errors.New("setenv failed")

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Filenames = nil // don't auto-load
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.LoadFiles("app.env"); err != nil {
			t.Fatalf("LoadFiles() error = %v", err)
		}

		err = loader.Apply()
		if err == nil {
			t.Fatal("Apply() error = nil, want the Setenv failure")
		}
		// The failing key is masked (first 2 chars + "***"), never the raw key.
		if msg := err.Error(); !strings.Contains(msg, MaskKey("KEY_B")) {
			t.Errorf("Apply() error = %q, want it to contain the masked key %q", msg, MaskKey("KEY_B"))
		} else if strings.Contains(msg, "KEY_B") {
			t.Errorf("Apply() error = %q, must not leak the raw key", msg)
		}

		// Apply returns at the first failure: keys attempted before it stay
		// set, keys after it were never attempted (map order is recorded).
		failedAt := -1
		for i, key := range fs.calls {
			if key == fs.failKey {
				failedAt = i
				break
			}
		}
		if failedAt == -1 {
			t.Fatal("Setenv was never called for the failing key")
		}
		values := map[string]string{"KEY_A": "a", "KEY_B": "b", "KEY_C": "c"}
		for _, key := range fs.calls[:failedAt] {
			if got := fs.env[key]; got != values[key] {
				t.Errorf("env[%q] = %q, want %q (key applied before the failure must persist)", key, got, values[key])
			}
		}
		for _, key := range fs.calls[failedAt+1:] {
			if _, ok := fs.env[key]; ok {
				t.Errorf("env[%q] was applied after the failure, want it untouched", key)
			}
		}
		if _, ok := fs.env[fs.failKey]; ok {
			t.Errorf("env[%q] was set despite the Setenv failure", fs.failKey)
		}
	})

}

// ltSetenvRecorderFS wraps *testFileSystem, recording every Setenv key and
// optionally failing exactly one key (with its own failErr, so the inner
// mock's fail-everything setenvErr stays unset), making Apply's
// partial-failure path assertable deterministically despite map iteration
// order.
type ltSetenvRecorderFS struct {
	*testFileSystem
	failKey string
	failErr error
	calls   []string
}

func (fs *ltSetenvRecorderFS) Setenv(key, value string) error {
	fs.calls = append(fs.calls, key)
	if key == fs.failKey {
		return fs.failErr
	}
	return fs.testFileSystem.Setenv(key, value)
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
			defer func() { _ = loader.Close() }() // best-effort cleanup

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
			defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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

	// ("exact match preferred over uppercase fallback" was removed: with no
	// lowercase competitor in storage it asserted the same Set-then-GetSecure
	// round trip as the table's "existing key" row.)
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
			defer func() { _ = loader.Close() }() // best-effort cleanup

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

	// The uppercase fallback in Lookup's dot-free fast path: .env keys are
	// stored as written while YAML/JSON flatten to uppercase, so a
	// lowercase probe must find an uppercase-stored key.
	t.Run("lowercase probe hits uppercase storage", func(t *testing.T) {
		loader, err := New(DefaultConfig())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("KEY", "value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		value, ok := loader.Lookup("key")
		if !ok || value != "value" {
			t.Errorf("Lookup(\"key\") = (%q, %v), want (\"value\", true)", value, ok)
		}
	})
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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("", "value"); err == nil {
			t.Error("Set() should fail with empty key")
		}
	})

	t.Run("invalid value rejected when ValidateValues is enabled", func(t *testing.T) {
		cfg := DefaultConfig() // ValidateValues defaults to true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		// NUL bytes are disallowed in values (injection defense).
		if err := loader.Set("KEY", "bad\x00value"); err == nil {
			t.Error("Set() should fail for value containing a NUL byte")
		}
	})

	t.Run("control chars rejected even when ValidateValues is disabled", func(t *testing.T) {
		// Fail-closed baseline (D-002/M-1): maximum length and control
		// characters are enforced on every write path regardless of the
		// ValidateValues switch, matching the .env parse path.
		cfg := DefaultConfig()
		cfg.ValidateValues = false
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("KEY", "bad\x00value"); err == nil {
			t.Error("Set() should reject a NUL byte even with validation disabled")
		}
	})

	t.Run("utf8 check follows ValidateUTF8 regardless of ValidateValues", func(t *testing.T) {
		// The UTF-8 check follows its own flag on every path, mirroring the
		// .env parse path (which runs the full ValidateValue unconditionally):
		// with ValidateUTF8=true an invalid-UTF-8 value is rejected whether or
		// not ValidateValues is set; with ValidateUTF8=false it passes.
		for _, tc := range []struct {
			validateValues bool
			validateUTF8   bool
			wantErr        bool
		}{{false, true, true}, {true, true, true}, {false, false, false}} {
			cfg := DefaultConfig()
			cfg.ValidateValues = tc.validateValues
			cfg.ValidateUTF8 = tc.validateUTF8
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			err = loader.Set("KEY", "bad\xffvalue")
			if tc.wantErr && err == nil {
				t.Errorf("Set() should reject invalid UTF-8 with ValidateValues=%v, ValidateUTF8=%v", tc.validateValues, tc.validateUTF8)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Set() with ValidateValues=%v, ValidateUTF8=%v error = %v, want nil", tc.validateValues, tc.validateUTF8, err)
			}
			_ = loader.Close() // best-effort cleanup
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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("EMPTY_KEY", ""); err != nil {
			t.Errorf("Set() with empty value error = %v", err)
		}
		if got := loader.GetString("EMPTY_KEY"); got != "" {
			t.Errorf("GetString() = %q, want \"\" (the stored empty value)", got)
		}
	})

	t.Run("unicode and emoji in value", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		unicodeValue := "hello 世界 🌍 \u4e2d\u6587"
		if err := loader.Set("UNICODE_KEY", unicodeValue); err != nil {
			t.Errorf("Set() with unicode error = %v", err)
		}
		if got := loader.GetString("UNICODE_KEY"); got != unicodeValue {
			t.Errorf("GetString() = %q, want %q", got, unicodeValue)
		}
	})

	t.Run("dotted key accepted and retrievable", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("APNEWS.COM", "Code=abc; g_state={\"i_l\":0}"); err != nil {
			t.Fatalf("Set(\"APNEWS.COM\") error = %v, want nil (structured-format key rules)", err)
		}
		if got, ok := loader.Lookup("apnews.com"); !ok || got != "Code=abc; g_state={\"i_l\":0}" {
			t.Errorf("Lookup(\"apnews.com\") = (%q, %v), want stored cookie value", got, ok)
		}
	})

	t.Run("invalid key characters still rejected", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		for _, key := range []string{"BAD KEY", "A=B", "A[0]", "höst"} {
			if err := loader.Set(key, "v"); err == nil {
				t.Errorf("Set(%q) = nil, want error", key)
			}
		}
	})
}

// TestLoader_Set_KeyRules covers the two contract points of Set's key
// validation beyond the default path: a custom KeyPattern keeps governing
// programmatic keys, and validators without the ValidateSetKey capability
// (custom minimal implementations) fall back to ValidateKey unchanged.
func TestLoader_Set_KeyRules(t *testing.T) {
	t.Run("custom KeyPattern still governs Set", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.KeyPattern = regexp.MustCompile(`^[A-Z_]+$`)
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("GOOD_KEY", "v"); err != nil {
			t.Errorf("Set(GOOD_KEY) error = %v, want nil", err)
		}
		if err := loader.Set("A.B", "v"); err == nil {
			t.Error("Set(A.B) = nil, want error under custom pattern")
		}
	})

	t.Run("validator without ValidateSetKey falls back to ValidateKey", func(t *testing.T) {
		mock := &recordingKeyValidator{err: errors.New("rejected by mock")}
		cfg := DefaultConfig()
		cfg.CustomValidator = mock
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("A.B", "v"); err == nil || !strings.Contains(err.Error(), "rejected by mock") {
			t.Errorf("Set(A.B) error = %v, want the mock's ValidateKey error", err)
		}
		if !mock.called {
			t.Error("fallback did not call the custom ValidateKey")
		}
	})
}

// recordingKeyValidator is a custom Validator (no ValidateSetKey capability)
// that records whether ValidateKey was invoked.
type recordingKeyValidator struct {
	err    error
	called bool
}

func (m *recordingKeyValidator) ValidateKey(key string) error {
	m.called = true
	return m.err
}

func (m *recordingKeyValidator) ValidateValue(value string) error { return nil }

func (m *recordingKeyValidator) ValidateRequired(keys map[string]bool) error { return nil }

func TestLoader_Delete(t *testing.T) {
	t.Run("existing key", func(t *testing.T) {
		cfg := DefaultConfig()
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
	t.Run("Set with AutoApply is retryable after Setenv failure", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.setenvErr = errors.New("setenv failed")

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.AutoApply = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("KEY", "value"); err == nil {
			t.Fatal("Set() with Setenv failure should return an error")
		}
		// The in-memory store must be untouched: the old flow stored the
		// value before applying, so with OverwriteExisting=false a retry
		// hit the skip branch and the value could never be applied.
		if _, ok := loader.Lookup("KEY"); ok {
			t.Error("Lookup(KEY) found a value after a failed Set; memory must stay unchanged")
		}

		fs.setenvErr = nil
		if err := loader.Set("KEY", "value"); err != nil {
			t.Fatalf("retry Set() error = %v", err)
		}
		if v, ok := loader.Lookup("KEY"); !ok || v != "value" {
			t.Errorf("Lookup(KEY) = (%q, %v) after retry, want (%q, true)", v, ok, "value")
		}
	})

	t.Run("Delete reports Unsetenv error", func(t *testing.T) {
		fs := newTestFileSystem()
		fs.unsetenvErr = errors.New("unsetenv failed")

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.AutoApply = true
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup

		// The key must have been applied by this loader for Delete to
		// attempt Unsetenv.
		if err := loader.Set("KEY", "value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		// The in-memory copy is removed either way, but the failed unset
		// must surface: the process environment still holds the value
		// while the caller believes it gone.
		err = loader.Delete("KEY")
		if err == nil {
			t.Fatal("Delete() with Unsetenv failure should return an error")
		}
		if !strings.Contains(err.Error(), "unsetenv failed") {
			t.Errorf("Delete() error = %v, want it to wrap the Unsetenv failure", err)
		}
		if _, ok := loader.Lookup("KEY"); ok {
			t.Error("in-memory copy should already be removed")
		}
	})

	// ("Apply with Setenv error" was folded away: applyLocked's Setenv
	// failure branch is driven — with the additional masking and key
	// persistence semantics asserted — by TestLoader_Apply's
	// "apply partial failure masks key and keeps earlier keys" case.)
}

// ============================================================================
// Keys/All/Len Tests
// ============================================================================

// TestLoader_Keys_All_Len merges the former TestLoader_Keys, TestLoader_All
// and TestLoader_Len into one table: each row seeds a loader from a map and
// runs a check against the collection accessor under test.
func TestLoader_Keys_All_Len(t *testing.T) {
	twoKeys := map[string]string{"KEY1": "value1", "KEY2": "value2"}

	tests := []struct {
		name  string
		setup map[string]string
		check func(t *testing.T, l *Loader)
	}{
		{
			name:  "Keys returns every stored key",
			setup: twoKeys,
			check: func(t *testing.T, l *Loader) {
				keys := l.Keys()
				if len(keys) != 2 {
					t.Errorf("Keys() returned %d keys, want 2", len(keys))
				}
				want := map[string]bool{"KEY1": true, "KEY2": true}
				for _, k := range keys {
					if !want[k] {
						t.Errorf("Keys() returned unexpected key %q", k)
					}
				}
			},
		},
		{
			name: "Keys on empty loader returns no keys",
			check: func(t *testing.T, l *Loader) {
				if keys := l.Keys(); len(keys) != 0 {
					t.Errorf("Keys() returned %d keys, want 0", len(keys))
				}
			},
		},
		{
			name:  "All maps keys to their values",
			setup: twoKeys,
			check: func(t *testing.T, l *Loader) {
				all := l.All()
				if len(all) != 2 {
					t.Errorf("All() returned %d keys, want 2", len(all))
				}
				if all["KEY1"] != "value1" {
					t.Errorf("All()[\"KEY1\"] = %q, want %q", all["KEY1"], "value1")
				}
			},
		},
		{
			name: "All on empty loader returns no entries",
			check: func(t *testing.T, l *Loader) {
				if all := l.All(); len(all) != 0 {
					t.Errorf("All() returned %d keys, want 0", len(all))
				}
			},
		},
		{
			name:  "Len counts stored variables",
			setup: twoKeys,
			check: func(t *testing.T, l *Loader) {
				if l.Len() != 2 {
					t.Errorf("Len() = %d, want 2", l.Len())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(DefaultConfig())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			for key, value := range tt.setup {
				if err := loader.Set(key, value); err != nil {
					t.Fatalf("Set(%q) error = %v", key, err)
				}
			}

			tt.check(t, loader)
		})
	}
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
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if loader.IsApplied() {
		t.Error("IsApplied() = true before Apply()")
	}

	if err := loader.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !loader.IsApplied() {
		t.Error("IsApplied() = false after Apply()")
	}

	// BOUNDARY: a closed loader reports not-applied regardless of history.
	if err := loader.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if loader.IsApplied() {
		t.Error("IsApplied() = true after Close(), want false")
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
	defer func() { _ = loader.Close() }() // best-effort cleanup

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

	// BOUNDARY: a closed loader reports a zero load time.
	if err := loader.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := loader.LoadTime(); !got.IsZero() {
		t.Errorf("LoadTime() after Close() = %v, want the zero time", got)
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
					t.Errorf("Lookup() = %q, want \"\" (zero value on closed loader)", value)
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
		{
			name:      "ParseInto",
			operation: "ParseInto",
			testFunc: func(t *testing.T, loader *Loader) {
				var target struct{}
				if err := loader.ParseInto(&target); !errors.Is(err, ErrClosed) {
					t.Errorf("ParseInto() error = %v, want ErrClosed", err)
				}
			},
		},
		{
			name:      "Validate",
			operation: "Validate",
			testFunc: func(t *testing.T, loader *Loader) {
				if err := loader.Validate(); !errors.Is(err, ErrClosed) {
					t.Errorf("Validate() error = %v, want ErrClosed", err)
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
			_ = loader.Close() // best-effort cleanup

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

// ltCloseErrHandler is an AuditHandler whose Close returns a fixed error,
// used to verify that Loader.Close propagates the audit handler's failure
// (Loader.Close → factory.Close → auditor.Close → handler.Close).
type ltCloseErrHandler struct{ closeErr error }

func (h *ltCloseErrHandler) Log(AuditEvent) error { return nil }

func (h *ltCloseErrHandler) Close() error { return h.closeErr }

func TestLoader_Close_AuditHandlerError(t *testing.T) {
	closeErr := errors.New("audit handler close failed")

	cfg := DefaultConfig()
	cfg.AuditHandler = &ltCloseErrHandler{closeErr: closeErr}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := loader.Close(); !errors.Is(err, closeErr) {
		t.Errorf("Close() error = %v, want the audit handler's close error %v", err, closeErr)
	}
	if !loader.IsClosed() {
		t.Error("IsClosed() = false after Close(), want true even when the handler fails")
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
			{"int64 overflow falls back to default", "BIG", "9223372036854775808", func(l *Loader, key string) any { return l.GetInt(key, 7) }, int64(7)},
			{"missing key with default", "MISSING", "", func(l *Loader, key string) any { return l.GetInt(key, 3000) }, int64(3000)},
			{"missing key without default", "MISSING", "", func(l *Loader, key string) any { return l.GetInt(key) }, int64(0)},
		}},
		{"GetUint64", []tc{
			{"existing key", "PORT", "8080", func(l *Loader, key string) any { return l.GetUint64(key) }, uint64(8080)},
			{"large value", "MAX_CONN", "18446744073709551615", func(l *Loader, key string) any { return l.GetUint64(key) }, uint64(18446744073709551615)},
			{"invalid value falls back to default", "PORT", "abc", func(l *Loader, key string) any { return l.GetUint64(key, 42) }, uint64(42)},
			{"negative value falls back to default", "PORT", "-1", func(l *Loader, key string) any { return l.GetUint64(key, 42) }, uint64(42)},
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
			// A bare number is not a valid duration for time.ParseDuration
			// ("missing unit"), so the default is returned — no implicit
			// seconds/milliseconds interpretation.
			{"bare number without unit falls back to default", "TIMEOUT", "5000", func(l *Loader, key string) any { return l.GetDuration(key, 5*time.Second) }, 5 * time.Second},
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
					defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		defer func() { _ = loader.Close() }() // best-effort cleanup

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
		// Extension matching is case-insensitive...
		{"CONFIG.JSON", FormatJSON},
		{".YML", FormatYAML},
		// ...and uses the final extension only (path/filepath.Ext).
		{"app.production.env", FormatEnv},
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

// TestAuditHandlerConstructors verifies each handler constructor produces a
// working handler in one test: Log must succeed and the handler-specific
// side effect must be observable.
func TestAuditHandlerConstructors(t *testing.T) {
	event := AuditEvent{
		Action:  ActionSet,
		Key:     "KEY",
		Reason:  "test",
		Success: true,
	}

	// JSON handler: output must be valid JSON.
	t.Run("JSON handler", func(t *testing.T) {
		var buf bytes.Buffer
		handler := NewJSONAuditHandler(&buf)
		if err := handler.Log(event); err != nil {
			t.Errorf("Log() error = %v", err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Errorf("Invalid JSON output: %v", err)
		}
	})

	// Log handler: nil logger falls back to stderr.
	t.Run("log handler with nil logger", func(t *testing.T) {
		handler := NewLogAuditHandler(nil)
		if handler == nil {
			t.Fatal("NewLogAuditHandler(nil) returned nil")
		}
		if err := handler.Log(event); err != nil {
			t.Errorf("Log() error = %v", err)
		}
	})

	// Channel handler: event must arrive on the channel.
	t.Run("channel handler", func(t *testing.T) {
		ch := make(chan AuditEvent, 10)
		handler := NewChannelAuditHandler(ch)
		if err := handler.Log(event); err != nil {
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
	})

	// Nop handler: Log and Close succeed without side effects.
	t.Run("nop handler", func(t *testing.T) {
		handler := NewNopAuditHandler()
		if err := handler.Log(event); err != nil {
			t.Errorf("Log() error = %v", err)
		}
		if err := handler.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

// ============================================================================
// ComponentFactory Tests
// ============================================================================

// TestComponentFactory covers only the Close/IsClosed lifecycle: the
// Validator/Auditor accessors are exercised functionally by
// TestFactoryAuditor_PublicInterface and every loader test, so asserting
// they are non-nil carries no information.
func TestComponentFactory(t *testing.T) {
	cfg := DefaultConfig()
	factory := cfg.buildComponentFactory()

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

func TestFactoryAuditor_PublicInterface(t *testing.T) {
	cfg := DefaultConfig()
	factory := cfg.buildComponentFactory()
	defer func() { _ = factory.Close() }() // best-effort cleanup

	// The built-in *internal.Auditor satisfies the public FullAuditLogger
	// interface directly — no adapter wrapper exists since AuditAction
	// became an alias of internal.Action. Exercise the public view.
	auditor := factory.Auditor()
	if auditor == nil {
		t.Fatal("factory.Auditor() returned nil")
	}

	t.Run("Log", func(t *testing.T) {
		if err := auditor.Log(ActionSet, "KEY", "test", true); err != nil {
			t.Errorf("Log() error = %v", err)
		}
	})

	t.Run("LogError", func(t *testing.T) {
		if err := auditor.LogError(ActionSet, "KEY", "error"); err != nil {
			t.Errorf("LogError() error = %v", err)
		}
	})

	t.Run("LogWithFile", func(t *testing.T) {
		if err := auditor.LogWithFile(ActionSet, "KEY", "file", "test", true); err != nil {
			t.Errorf("LogWithFile() error = %v", err)
		}
	})

	t.Run("LogWithDuration", func(t *testing.T) {
		if err := auditor.LogWithDuration(ActionSet, "KEY", "test", true, time.Second); err != nil {
			t.Errorf("LogWithDuration() error = %v", err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		if err := auditor.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

// ============================================================================
// JSON Parser Edge Case Tests
// ============================================================================

func TestJSONParser_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		content   string        // used when contentFn is nil
		contentFn func() string // generated content (many-variables case)
		cfgMod    func(*Config)
		wantErr   bool
		want      map[string]string // exact expected key set and values
	}{
		{
			name:    "empty object",
			content: "{}",
			want:    map[string]string{},
		},
		{
			name: "nested object",
			content: `{
				"database": {
					"host": "localhost",
					"port": 5432,
					"credentials": {
						"username": "admin",
						"password": "secret"
					}
				}
			}`,
			want: map[string]string{
				"DATABASE_HOST":                 "localhost",
				"DATABASE_PORT":                 "5432",
				"DATABASE_CREDENTIALS_USERNAME": "admin",
				"DATABASE_CREDENTIALS_PASSWORD": "secret",
			},
		},
		{
			name: "array handling",
			content: `{
				"servers": ["server1", "server2", "server3"],
				"ports": [8080, 8081, 8082]
			}`,
			want: map[string]string{
				"SERVERS_0": "server1",
				"SERVERS_1": "server2",
				"SERVERS_2": "server3",
				"PORTS_0":   "8080",
				"PORTS_1":   "8081",
				"PORTS_2":   "8082",
			},
		},
		{
			name:    "null handling",
			content: "{\n\t\"null_value\": null,\n\t\"other_value\": \"test\"\n}",
			cfgMod:  func(c *Config) { c.JSONNullAsEmpty = true },
			want:    map[string]string{"NULL_VALUE": "", "OTHER_VALUE": "test"},
		},
		{
			name:    "invalid JSON",
			content: `{invalid json}`,
			wantErr: true,
		},
		{
			// The file-too-large path is covered by TestLoader_LoadFiles
			// "file too large" (format-independent, asserts *FileError).
			name: "max variables exceeded",
			contentFn: func() string {
				var sb strings.Builder
				sb.WriteString("{")
				for i := 0; i < 100; i++ {
					if i > 0 {
						sb.WriteString(",")
					}
					fmt.Fprintf(&sb, `"KEY_%03d":"value"`, i)
				}
				sb.WriteString("}")
				return sb.String()
			},
			cfgMod:  func(c *Config) { c.MaxVariables = 10 },
			wantErr: true,
		},
		{
			name:    "JSON max depth exceeded",
			content: `{"a": {"b": {"c": {"d": {"e": {"f": "deep"}}}}}}`,
			cfgMod:  func(c *Config) { c.JSONMaxDepth = 3 },
			wantErr: true,
		},
		{
			name:    "JSON with boolean values",
			content: `{"enabled": true, "disabled": false}`,
			cfgMod:  func(c *Config) { c.JSONBoolAsString = true },
			want:    map[string]string{"ENABLED": "true", "DISABLED": "false"},
		},
		{
			// With the flag off, a JSON null is stored as the literal string
			// "null" (internal flattenDecoded), not skipped and not empty.
			name:    "JSON null with nullAsEmpty disabled",
			content: `{"key": null}`,
			cfgMod:  func(c *Config) { c.JSONNullAsEmpty = false },
			want:    map[string]string{"KEY": "null"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := tt.content
			if tt.contentFn != nil {
				content = tt.contentFn()
			}
			fs := newTestFileSystem()
			fs.files["edge.json"] = content

			cfg := DefaultConfig()
			cfg.FileSystem = fs
			if tt.cfgMod != nil {
				tt.cfgMod(&cfg)
			}
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			err = loader.LoadFiles("edge.json")
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadFiles() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			got := loader.All()
			if len(got) != len(tt.want) {
				t.Fatalf("All() = %v (%d keys), want %v (%d keys)", got, len(got), tt.want, len(tt.want))
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("All()[%q] = %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

// ============================================================================
// YAML Parser Edge Case Tests
// ============================================================================

func TestYAMLParser_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		cfgMod  func(*Config)
		wantErr bool
		want    map[string]string
	}{
		{
			name:    "empty document loads without error",
			content: "",
		},
		{
			name: "nested map",
			content: `
database:
  host: localhost
  port: 5432
  credentials:
    username: admin
    password: secret
`,
			want: map[string]string{"DATABASE_HOST": "localhost"},
		},
		{
			name: "list handling",
			content: `
servers:
  - server1
  - server2
  - server3
`,
			want: map[string]string{"SERVERS_0": "server1"},
		},
		{
			name: "boolean handling",
			content: `
debug: true
production: false
`,
			cfgMod: func(c *Config) { c.YAMLBoolAsString = true },
			want:   map[string]string{"DEBUG": "true"},
		},
		{
			name: "null handling",
			content: `
null_value: null
other_value: test
`,
			cfgMod: func(c *Config) { c.YAMLNullAsEmpty = true },
			want:   map[string]string{"NULL_VALUE": ""},
		},
		{
			// The parser is deliberately lenient: a malformed block is
			// recovered as plain list items rather than failing the load.
			name: "invalid yaml is recovered leniently",
			content: `
invalid:
  - unclosed
    - bad indent
`,
			want: map[string]string{"INVALID_0": "unclosed", "INVALID_1": "bad indent"},
		},
		{
			name: "complex nested structure",
			content: `
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
`,
			want: map[string]string{"APP_NAME": "myapp"},
		},
		{
			// YAML flow sequences with unquoted scalars are flattened into
			// indexed keys via the flow→JSON conversion (GEN-001: they were
			// previously stored as the raw text, which GetSlice then split
			// at the wrong boundaries).
			name:    "inline flow list flattened to indexed keys",
			content: "tags: [web, api, db]\n",
			want:    map[string]string{"TAGS_0": "web", "TAGS_1": "api", "TAGS_2": "db"},
		},
		{
			// GEN-001: block scalars previously stored the literal "|"
			// and dropped the indented body; they now load verbatim.
			name:    "literal block scalar loads verbatim",
			content: "cert: |\n  -----BEGIN CERT-----\n  data\n  -----END CERT-----\nport: 9090\n",
			want: map[string]string{
				"CERT": "-----BEGIN CERT-----\ndata\n-----END CERT-----\n",
				"PORT": "9090",
			},
		},
		{
			// YAML-style flow mappings are not JSON (unquoted keys); the
			// flow→JSON conversion flattens them like their block-form
			// equivalents. (Previously the whole text was stored as a raw
			// scalar; before that, the ": " inside the braces made the lexer
			// misread the value as a bogus key and the load errored.)
			name:    "flow mapping flattened to prefixed keys",
			content: "config: {host: localhost, port: 3000}\n",
			want:    map[string]string{"CONFIG_HOST": "localhost", "CONFIG_PORT": "3000"},
		},
		{
			name: "number values as strings",
			content: `
integer_val: 42
float_val: 3.14
negative_val: -10
`,
			cfgMod: func(c *Config) { c.YAMLNumberAsString = true },
			want: map[string]string{
				"INTEGER_VAL":  "42",
				"FLOAT_VAL":    "3.14",
				"NEGATIVE_VAL": "-10",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newTestFileSystem()
			fs.files["edge.yaml"] = tt.content

			cfg := DefaultConfig()
			cfg.FileSystem = fs
			if tt.cfgMod != nil {
				tt.cfgMod(&cfg)
			}
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			err = loader.LoadFiles("edge.yaml")
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadFiles() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for key, want := range tt.want {
				if got := loader.GetString(key); got != want {
					t.Errorf("GetString(%q) = %q, want %q", key, got, want)
				}
			}
		})
	}

	t.Run("YAML max depth exceeded", func(t *testing.T) {
		var sb strings.Builder
		for i := 0; i < 15; i++ {
			sb.WriteString(strings.Repeat("  ", i))
			_, _ = fmt.Fprintf(&sb, "level%d:\n", i) // builder writes cannot fail
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
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.LoadFiles("deep.yaml"); err == nil {
			t.Error("LoadFiles() should fail with YAML depth exceeded")
		}
	})
}

// ============================================================================
// Error Type Tests (Table-Driven)
// ============================================================================

func TestErrorTypes_Error(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantUnwrap bool // if true, verify Unwrap() returns non-nil
	}{
		{"JSONError with path", &JSONError{
			Path: "$.database.host", Message: "invalid type", Err: errors.New("expected string"),
		}, true},
		{"JSONError without path", &JSONError{Message: "parse error"}, false},
		{"YAMLError with path and line", &YAMLError{
			Path: "config.yaml", Line: 10, Column: 5, Message: "invalid mapping",
		}, false},
		{"YAMLError with line only", &YAMLError{Line: 15, Message: "indentation error"}, false},
		{"YAMLError without location", &YAMLError{Message: "parse error"}, false},
		{"ExpansionError with key", &ExpansionError{Key: "VAR", Depth: 10, Limit: 5}, false},
		{"ExpansionError without key", &ExpansionError{
			Depth: 10, Limit: 5, Chain: "A -> B -> C",
		}, false},
		{"SecurityError with key", &SecurityError{
			Action: "set", Reason: "forbidden key", Key: "SECRET_KEY", Details: "key is in forbidden list",
		}, false},
		{"SecurityError without key", &SecurityError{
			Action: "load", Reason: "file too large",
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() == "" {
				t.Errorf("%T.Error() should not be empty", tt.err)
			}
			if tt.wantUnwrap {
				if u, ok := tt.err.(interface{ Unwrap() error }); ok {
					if u.Unwrap() == nil {
						t.Errorf("%T.Unwrap() should return non-nil", tt.err)
					}
				}
			}
		})
	}
}

// ============================================================================
// validateFilePath Tests (Security)
// ============================================================================

func TestValidateFilePath(t *testing.T) {
	// The full rejection matrix (network paths, drive letters, every
	// reserved device name, more valid-path shapes) lives in
	// internal/path_validator_test.go. This keeps one representative row per
	// rejection class plus a valid control, wired through the public
	// validateFilePath entry point.
	tests := []struct {
		name      string
		filename  string
		wantErr   bool
		errReason string
	}{
		{"valid relative path", "config/.env", false, ""},
		{"UNC path backslash", "\\\\server\\share", true, "UNC path"},
		{"path traversal", "../../../etc/passwd", true, "path traversal"},
		{"Windows reserved in subdir", "subdir/CON", true, "reserved device"},
		{"Windows reserved after backslash", `backup\NUL.txt`, true, "reserved device"},
		{"Windows reserved later component", "a/../COM1", true, "reserved device"},
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

// TestLoader_LoadFiles_PathTraversal pins the end-to-end wiring of path
// validation into LoadFiles (env.go loadFileLocked): a traversal path is
// rejected with a SecurityError before any filesystem access happens.
func TestLoader_LoadFiles_PathTraversal(t *testing.T) {
	loader, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	err = loader.LoadFiles("../../etc/passwd")
	if err == nil {
		t.Fatal("LoadFiles(traversal path) error = nil, want SecurityError")
	}
	var secErr *SecurityError
	if !errors.As(err, &secErr) {
		t.Fatalf("LoadFiles(traversal path) error = %T, want *SecurityError", err)
	}
	if !strings.Contains(secErr.Reason, "path traversal") {
		t.Errorf("LoadFiles(traversal path) reason = %q, want it to mention path traversal", secErr.Reason)
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
	defer func() { _ = os.Chdir(oldWd) }() // best-effort restore of the original working directory

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

// TestValidateFilePathForFS pins the FS-aware path-validation wiring
// (D-002/MAJ-2): the OS file system (including the panic-containment
// wrapper the loader installs) keeps the FULL rule set with symlink
// resolution, while injected file systems get the static, disk-free rules —
// their loads must not depend on host-disk state.
func TestValidateFilePathForFS(t *testing.T) {
	if !isOSFileSystem(OSFileSystem{}) {
		t.Error("isOSFileSystem(OSFileSystem{}) = false, want true")
	}
	if !isOSFileSystem(resolveComponentFileSystem(nil)) {
		t.Error("isOSFileSystem(wrapped default) = false, want true")
	}
	if isOSFileSystem(resolveComponentFileSystem(newTestFileSystem())) {
		t.Error("isOSFileSystem(wrapped custom fs) = true, want false")
	}

	// Static rules still apply to injected file systems.
	err := validateFilePathForFS("../escape.env", resolveComponentFileSystem(newTestFileSystem()))
	if err == nil {
		t.Fatal("validateFilePathForFS(traversal, custom fs) error = nil, want SecurityError")
	}
	var secErr *SecurityError
	if !errors.As(err, &secErr) || !strings.Contains(secErr.Reason, "path traversal") {
		t.Errorf("validateFilePathForFS(traversal, custom fs) error = %v, want path traversal SecurityError", err)
	}

	// A clean relative name passes at both fidelities.
	if err := validateFilePathForFS("config/.env", resolveComponentFileSystem(newTestFileSystem())); err != nil {
		t.Errorf("validateFilePathForFS(clean, custom fs) error = %v, want nil", err)
	}
	if err := validateFilePathForFS("config/.env", resolveComponentFileSystem(nil)); err != nil {
		t.Errorf("validateFilePathForFS(clean, os fs) error = %v, want nil", err)
	}
}

// TestLoader_LoadFiles_CustomFS_IgnoresHostSymlinks extends the symlink story
// to injected file systems (D-002/MAJ-2): a name that IS a host-disk symlink
// to an absolute location is rejected by full validation, yet a load reading
// from an in-memory FS under the same name must succeed — previously the
// symlink check consulted the host disk and failed the in-memory load.
func TestLoader_LoadFiles_CustomFS_IgnoresHostSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require admin privileges on Windows")
	}

	tmpDir := t.TempDir()
	outsideFile := filepath.Join(tmpDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(tmpDir, "escape.env")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }() // best-effort restore of the original working directory

	// Control: full validation still rejects the host-disk symlink.
	if err := validateFilePath("escape.env"); err == nil {
		t.Fatal("validateFilePath(symlink to absolute) error = nil, want rejection")
	}

	// The in-memory FS is unaffected by that host-disk state.
	fs := newTestFileSystem()
	fs.files["escape.env"] = "KEY=from-memory\n"
	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.FileSystem = fs
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.LoadFiles("escape.env"); err != nil {
		t.Fatalf("LoadFiles(symlinked name, custom fs) error = %v, want nil", err)
	}
	if v, ok := loader.Lookup("KEY"); !ok || v != "from-memory" {
		t.Errorf("Lookup(KEY) = (%q, %v), want (%q, true)", v, ok, "from-memory")
	}
}

// TestSet_AutoApply_SetenvFailure_Audited pins MIN-8: the os-level Setenv
// failure inside Set reaches the audit stream, mirroring applyLocked's
// failure branch.
func TestSet_AutoApply_SetenvFailure_Audited(t *testing.T) {
	handler := &utRecordingAuditHandler{}

	fs := newTestFileSystem()
	fs.setenvErr = errors.New("setenv failed")

	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.FileSystem = fs
	cfg.AutoApply = true
	cfg.AuditEnabled = true
	cfg.AuditHandler = handler

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.Set("KEY", "value"); err == nil {
		t.Fatal("Set() with Setenv failure should return an error")
	}

	found := false
	for _, e := range handler.recorded() {
		if e.Action == ActionSet && !e.Success && strings.Contains(e.Reason, "setenv failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("no failed ActionSet audit event carrying the Setenv error; want the failure audited like applyLocked")
	}
}

// ============================================================================
// newParseError Tests
// ============================================================================

// TestNewParseError pins the one behavioral point of the constructor: the
// Content field is sanitized (errors.go newParseError applies
// MaskSensitiveInString), because Error() does not print Content but callers
// can still read it via errors.As.
func TestNewParseError(t *testing.T) {
	err := newParseError("test.env", 10, "PASSWORD=secret123", errors.New("parse failed"))

	if err.File != "test.env" {
		t.Errorf("File = %q, want %q", err.File, "test.env")
	}
	if err.Line != 10 {
		t.Errorf("Line = %d, want 10", err.Line)
	}
	if err.Err == nil {
		t.Error("Err should not be nil")
	}

	// MaskInString replaces the value of any key=value pair matching a
	// sensitive pattern with "[MASKED]": "PASSWORD=secret123" becomes
	// "PASSWORD=[MASKED]". The secret must not survive in any form.
	if got := err.Content; got != "PASSWORD=[MASKED]" {
		t.Errorf("Content = %q, want %q", got, "PASSWORD=[MASKED]")
	}
	if strings.Contains(err.Content, "secret123") {
		t.Error("Content leaks the secret value")
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

	t.Run("auto-apply error during New", func(t *testing.T) {
		// Real error injection: the file loads fine, but applying it to the
		// (mocked) process environment fails, so New itself must fail.
		fs := newTestFileSystem()
		fs.files[".env"] = "KEY=value"
		fs.setenvErr = errors.New("setenv failed")

		cfg := DefaultConfig()
		cfg.FileSystem = fs
		cfg.Filenames = []string{".env"}
		cfg.AutoApply = true

		if _, err := New(cfg); err == nil {
			t.Error("New() should fail when AutoApply cannot set the environment")
		}
	})

	t.Run("partially-filled config is not auto-completed", func(t *testing.T) {
		// Documented semantics (env.go New): a fully zero Config means
		// DefaultConfig, but a partially-set one is NOT auto-completed —
		// unset limits stay zero and Validate rejects them.
		cfg := Config{FileConfig: FileConfig{Filenames: []string{".env"}}}

		_, err := New(cfg)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("New(partially-filled Config) error = %v, want ErrInvalidConfig", err)
		}
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
	// Nil-factory guards (built-in and custom formats) live in
	// TestRegisterParser_NilFactory.
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

// TestUnmarshalMapWithConfig_ClosesCustomParsers pins the one-shot parser
// cleanup (D-002/MIN-2): a custom parser registered via RegisterParser gets
// its io.Closer honored on both the success and the parse-error path, the
// same cleanup Loader.Close performs.
func TestUnmarshalMapWithConfig_ClosesCustomParsers(t *testing.T) {
	t.Run("success path", func(t *testing.T) {
		format := nextTestFormat()
		closed := &atomic.Int32{}
		if err := RegisterParser(format, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return &closingParser{closed: closed}, nil
		}); err != nil {
			t.Fatalf("RegisterParser() error = %v", err)
		}

		cfg := DefaultConfig()
		if _, err := UnmarshalMapWithConfig(cfg, "A=1", format); err != nil {
			t.Fatalf("UnmarshalMapWithConfig() error = %v", err)
		}
		if got := closed.Load(); got != 1 {
			t.Errorf("parser Close calls after success = %d, want 1", got)
		}
	})

	t.Run("parse error path", func(t *testing.T) {
		format := nextTestFormat()
		closed := &atomic.Int32{}
		if err := RegisterParser(format, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return &closingParser{closed: closed, parseErr: errors.New("boom")}, nil
		}); err != nil {
			t.Fatalf("RegisterParser() error = %v", err)
		}

		cfg := DefaultConfig()
		if _, err := UnmarshalMapWithConfig(cfg, "A=1", format); err == nil {
			t.Fatal("UnmarshalMapWithConfig() error = nil, want parse error")
		}
		if got := closed.Load(); got != 1 {
			t.Errorf("parser Close calls after parse error = %d, want 1", got)
		}
	})
}

// closingParser is a minimal EnvParser whose Close calls are counted.
type closingParser struct {
	closed   *atomic.Int32
	parseErr error
}

func (p *closingParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	if p.parseErr != nil {
		return nil, p.parseErr
	}
	return map[string]string{"A": "1"}, nil
}

func (p *closingParser) Close() error {
	p.closed.Add(1)
	return nil
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

// ============================================================================
// Expansion Edge Cases (from coverage_test.go)
// ============================================================================

func TestExpansion_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		cfgMod  func(*Config)
		wantErr bool
		want    map[string]string
	}{
		{
			name:    "expansion depth limit exceeded",
			content: "A=${B}\nB=${A}",
			cfgMod: func(c *Config) {
				c.ExpandVariables = true
				c.MaxExpansionDepth = 2
			},
			wantErr: true,
		},
		{
			name:    "self-referencing variable",
			content: "X=${X}",
			cfgMod:  func(c *Config) { c.ExpandVariables = true },
			wantErr: true,
		},
		{
			name:    "braced variable with default",
			content: "RESULT=${MISSING:-fallback}",
			cfgMod:  func(c *Config) { c.ExpandVariables = true },
			want:    map[string]string{"RESULT": "fallback"},
		},
		{
			name:    "nested variable expansion",
			content: "BASE=hello\nNESTED=${BASE}_world",
			cfgMod:  func(c *Config) { c.ExpandVariables = true },
			want:    map[string]string{"BASE": "hello", "NESTED": "hello_world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newTestFileSystem()
			fs.files["expand.env"] = tt.content

			cfg := DefaultConfig()
			cfg.FileSystem = fs
			if tt.cfgMod != nil {
				tt.cfgMod(&cfg)
			}
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			err = loader.LoadFiles("expand.env")
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadFiles() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for key, want := range tt.want {
				if got := loader.GetString(key); got != want {
					t.Errorf("GetString(%q) = %q, want %q", key, got, want)
				}
			}
		})
	}
}

// ============================================================================
// Env Parser Boundary Tests (table-driven)
// ============================================================================

// TestParser_BoundaryConditions exercises uncovered branches in the env
// parser's Parse method: parse-error break, line-too-long scanner error,
// export-prefix handling, and the max-variables ceiling. (Duplicate-key
// behavior is covered by duplicate_key_test.go across all policies.)
func TestParser_BoundaryConditions(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		configFn  func(*Config)
		wantErr   bool
		wantErrIs error  // when set, the error must match via errors.Is
		wantKey   string // verify this key exists after parse
		wantValue string // expected value for wantKey
		wantNoKey string // verify this key does NOT exist
	}{
		{
			name:     "line too long triggers scanner error",
			content:  strings.Repeat("a", 2000) + "\n",
			configFn: func(c *Config) { c.MaxLineLength = 100 },
			wantErr:  true,
		},
		{
			name:      "export prefix stripped",
			content:   "export KEY=value\n",
			wantKey:   "KEY",
			wantValue: "value",
		},
		{
			name:      "comment and empty lines skipped",
			content:   "# comment\n\n   \nKEY=value\n",
			wantKey:   "KEY",
			wantValue: "value",
			wantNoKey: "COMMENT",
		},
		{
			// BOUNDARY (parser.go): trailing spaces on the line are trimmed
			// from the value — only an escaped space or quotes preserve them.
			name:      "trailing spaces trimmed from value",
			content:   "KEY=value   \n",
			wantKey:   "KEY",
			wantValue: "value",
		},
		{
			name:      "max variables exceeded during env parse",
			content:   "A=1\nB=2\nC=3\n",
			configFn:  func(c *Config) { c.MaxVariables = 2 },
			wantErr:   true,
			wantErrIs: ErrMaxVariables,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newTestFileSystem()
			fs.files["test.env"] = tt.content

			cfg := DefaultConfig()
			cfg.FileSystem = fs
			cfg.Filenames = nil // don't auto-load
			if tt.configFn != nil {
				tt.configFn(&cfg)
			}

			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }() // best-effort cleanup

			err = loader.LoadFiles("test.env")
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoadFiles() error = nil, want error")
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("LoadFiles() error = %v, want it matching %v via errors.Is", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFiles() error = %v", err)
			}

			if tt.wantKey != "" {
				if v := loader.GetString(tt.wantKey); v != tt.wantValue {
					t.Errorf("GetString(%q) = %q, want %q", tt.wantKey, v, tt.wantValue)
				}
			}
			if tt.wantNoKey != "" {
				if _, ok := loader.Lookup(tt.wantNoKey); ok {
					t.Errorf("Lookup(%q) should return false", tt.wantNoKey)
				}
			}
		})
	}
}

// ============================================================================
// Parser & Registry Internal Boundary Tests
// ============================================================================

// failingReadCloser yields its data once, then fails with a custom error on
// every subsequent read. Used to drive the .env parser's generic
// scanner-error path (not ErrFileTooLarge / ErrLineTooLong).
type failingReadCloser struct {
	data []byte
	err  error
	done bool
}

func (r *failingReadCloser) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.data), nil
}

// TestParser_Parse_DirectReaderErrors covers parser.Parse paths that the
// loader-level tests cannot reach: line-parse failures from ParseLineBytes,
// generic (non-limit) reader errors surfacing as ParseError, and the
// Len()-based result-map pre-sizing for large inputs.
func TestParser_Parse_DirectReaderErrors(t *testing.T) {
	newTestParser := func(t *testing.T) *parser {
		t.Helper()
		cfg := DefaultConfig()
		factory := cfg.buildComponentFactory()
		t.Cleanup(func() { _ = factory.Close() })
		p, err := newParserWithFactory(cfg, factory)
		if err != nil {
			t.Fatalf("newParserWithFactory() error = %v", err)
		}
		return p
	}

	t.Run("unterminated quote fails the line", func(t *testing.T) {
		_, err := newTestParser(t).Parse(strings.NewReader("GOOD=1\nBROKEN=\"unterminated\n"), "broken.env")
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("Parse() error = %v (%T), want *ParseError", err, err)
		}
		if pe.Line != 2 {
			t.Errorf("ParseError.Line = %d, want 2", pe.Line)
		}
	})

	t.Run("generic read error becomes ParseError", func(t *testing.T) {
		wantErr := errors.New("device detached")
		_, err := newTestParser(t).Parse(&failingReadCloser{data: []byte("A=1\n"), err: wantErr}, "io.env")
		if !errors.Is(err, wantErr) {
			t.Errorf("Parse() error = %v, want it wrapping %v", err, wantErr)
		}
	})

	t.Run("Len-reporting reader pre-sizes the result map", func(t *testing.T) {
		// > 60*64 bytes with distinct keys: the estimate (one var per 60 chars)
		// exceeds the 64-entry floor, exercising the capacity heuristic.
		cfg := DefaultConfig()
		cfg.MaxVariables = 2000 // default 500 would reject 1050 keys
		factory := cfg.buildComponentFactory()
		t.Cleanup(func() { _ = factory.Close() })
		p, err := newParserWithFactory(cfg, factory)
		if err != nil {
			t.Fatalf("newParserWithFactory() error = %v", err)
		}

		var content strings.Builder
		for i := 0; i < 1050; i++ {
			fmt.Fprintf(&content, "KEY%d=1\n", i)
		}
		result, err := p.Parse(strings.NewReader(content.String()), "big.env")
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if len(result) != 1050 {
			t.Errorf("len(result) = %d, want 1050", len(result))
		}
	})
}

// TestNewParserWithFactory_NilFactory covers the parser constructor guard.
func TestNewParserWithFactory_NilFactory(t *testing.T) {
	if _, err := newParserWithFactory(Config{}, nil); err == nil {
		t.Error("newParserWithFactory(nil) should return an error")
	}
}

// TestScannerBufferPool_Boundary covers the scanner buffer pool's defensive
// paths: nil puts, oversized buffers refused entry, the used-region clear
// contract, and the fallback allocation when the pool returns an unexpected
// type.
func TestScannerBufferPool_Boundary(t *testing.T) {
	t.Run("nil buffer is ignored", func(t *testing.T) {
		putScannerBuffer(nil, 0) // must not panic
	})

	t.Run("oversized buffer is not pooled", func(t *testing.T) {
		big := make([]byte, internal.MaxPooledScannerBufferSize+1)
		putScannerBuffer(&big, len(big)*2) // dropped at the cap gate, never pooled (the clamp subtest below covers the used-count clamp)

		// The cap gate must hold: the next get may return some other pooled
		// buffer, but never one above MaxPooledScannerBufferSize — in
		// particular not the oversized buffer just refused entry.
		buf := getScannerBuffer()
		if cap(*buf) > internal.MaxPooledScannerBufferSize {
			t.Errorf("getScannerBuffer() capacity = %d, want <= %d (oversized buffer leaked into the pool)", cap(*buf), internal.MaxPooledScannerBufferSize)
		}
		if cap(*buf) == cap(big) {
			t.Errorf("getScannerBuffer() returned the refused oversized buffer (cap %d)", cap(big))
		}
		putScannerBuffer(buf, 0) // return the legal buffer for later tests
	})

	t.Run("unexpected pool type falls back to a fresh buffer", func(t *testing.T) {
		scannerBufferPool.Put(new(int)) // poison the pool with a foreign type
		buf := getScannerBuffer()
		if cap(*buf) != 64*1024 {
			t.Errorf("fallback buffer capacity = %d, want %d", cap(*buf), 64*1024)
		}
		putScannerBuffer(buf, 0) // restore a well-typed buffer to the pool (nothing was dirtied)
	})

	t.Run("used region is cleared, tail is not", func(t *testing.T) {
		buf := make([]byte, 64*1024)
		for i := range buf {
			buf[i] = 0xAA // simulate a parse that dirtied the whole buffer
		}
		b := buf
		putScannerBuffer(&b, 16)
		// The clear happens in place on the shared backing array: the first
		// 16 bytes (the region a 16-byte read would have dirtied) must be
		// zeroed, the untouched tail must survive — that is the optimization.
		for i := 0; i < 16; i++ {
			if buf[i] != 0 {
				t.Fatalf("byte %d inside used region not cleared: 0x%02X", i, buf[i])
			}
		}
		for i := 16; i < len(buf); i++ {
			if buf[i] != 0xAA {
				t.Fatalf("byte %d outside used region was modified: 0x%02X", i, buf[i])
			}
		}
		// The buffer entered the pool; restore its clean-at-rest invariant
		// through the aliasing slice so later consumers are unaffected.
		clear(buf)
	})

	t.Run("used count above length is clamped and clears everything", func(t *testing.T) {
		// BOUNDARY (parser.go putScannerBuffer): a used count larger than the
		// buffer is clamped to len(*buf), so the whole buffer is cleared —
		// defensive against callers misreporting the dirtied region.
		buf := make([]byte, 4096)
		for i := range buf {
			buf[i] = 0xAA
		}
		b := buf
		putScannerBuffer(&b, len(b)+1)
		for i, c := range buf {
			if c != 0 {
				t.Fatalf("byte %d not cleared under the clamped used count: 0x%02X", i, c)
			}
		}
	})
}

// TestRegisterParser_NilFactory covers the registry's nil-factory guard on
// both registration entry points (RegisterParser and ForceRegisterParser),
// for the built-in formats and for fresh custom formats. Custom formats come
// from nextTestFormat() so repeated runs (-count=N) stay isolated.
func TestRegisterParser_NilFactory(t *testing.T) {
	tests := []struct {
		name    string
		format  func() FileFormat
		force   bool
		factory ParserFactory
		wantErr bool
	}{
		{"RegisterParser nil factory on dotenv", func() FileFormat { return FormatEnv }, false, nil, true},
		{"RegisterParser nil factory on json", func() FileFormat { return FormatJSON }, false, nil, true},
		{"RegisterParser nil factory on yaml", func() FileFormat { return FormatYAML }, false, nil, true},
		{"RegisterParser nil factory on custom format", nextTestFormat, false, nil, true},
		{"ForceRegisterParser nil factory on built-in dotenv", func() FileFormat { return FormatEnv }, true, nil, true},
		{"ForceRegisterParser nil factory on custom format", nextTestFormat, true, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.force {
				err = ForceRegisterParser(tt.format(), tt.factory)
			} else {
				err = RegisterParser(tt.format(), tt.factory)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestCreateParsers_FactoryErrorClosesCreatedParsers covers the registry's
// error-cleanup path: when one factory fails, parsers already created by
// other factories must be closed before the error is returned.
//
// createParsers iterates its factory snapshot in random map order, so a
// single call may fail the bad factory before the closer factory ever runs,
// leaving nothing to clean up. The test retries (bounded) until one call
// creates the closer before the failure — a coin flip per call — then asserts
// every creation was matched by exactly one Close.
func TestCreateParsers_FactoryErrorClosesCreatedParsers(t *testing.T) {
	okFormat := nextTestFormat()
	badFormat := nextTestFormat()

	closer := &countingCloserParser{}
	var created atomic.Int32
	globalParserRegistry.mu.Lock()
	globalParserRegistry.factories[okFormat] = func(Config, *ComponentFactory) (EnvParser, error) {
		created.Add(1)
		return closer, nil
	}
	globalParserRegistry.factories[badFormat] = func(Config, *ComponentFactory) (EnvParser, error) {
		return nil, errors.New("factory exploded")
	}
	globalParserRegistry.mu.Unlock()

	defer func() {
		globalParserRegistry.mu.Lock()
		delete(globalParserRegistry.factories, okFormat)
		delete(globalParserRegistry.factories, badFormat)
		globalParserRegistry.mu.Unlock()
	}()

	cfg := DefaultConfig()
	factory := cfg.buildComponentFactory()
	defer func() { _ = factory.Close() }() // best-effort cleanup

	const maxAttempts = 32
	for i := 0; i < maxAttempts && created.Load() == 0; i++ {
		parsers, err := createParsers(cfg, factory)
		if err == nil {
			t.Fatal("createParsers() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "failed to create") {
			t.Errorf("createParsers() error = %v, want it wrapped with \"failed to create\"", err)
		}
		if parsers != nil {
			t.Errorf("createParsers() = %v on error, want nil", parsers)
		}
	}
	if created.Load() == 0 {
		t.Fatalf("map order never placed the closer before the failing factory in %d attempts", maxAttempts)
	}
	if got := closer.closeCount.Load(); got != created.Load() {
		t.Errorf("parser closed %d times for %d creations, want 1 close per creation", got, created.Load())
	}
}

// countingCloserParser is a no-op EnvParser that counts Close calls.
type countingCloserParser struct {
	closeCount atomic.Int32
}

func (p *countingCloserParser) Parse(io.Reader, string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (p *countingCloserParser) Close() error {
	p.closeCount.Add(1)
	return nil
}

// TestNewJSONParser_DefaultMaxDepth pins the JSONMaxDepth<=0 fallback:
// newJSONParserWithFactory clamps a non-positive depth to
// defaultStructuredMaxDepth. Config.Validate normally prevents this
// (range 1..100), so the constructor is exercised directly, bypassing
// validation.
func TestNewJSONParser_DefaultMaxDepth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.JSONMaxDepth = 0 // invalid for New(), valid for the constructor

	factory := cfg.buildComponentFactory()
	defer func() { _ = factory.Close() }() // best-effort cleanup

	p, err := newJSONParserWithFactory(cfg, factory)
	if err != nil {
		t.Fatalf("newJSONParserWithFactory() error = %v", err)
	}

	nestJSON := func(depth int) string {
		doc := `"leaf"`
		for i := 0; i < depth; i++ {
			doc = `{"a":` + doc + `}`
		}
		return doc
	}

	if _, err := p.Parse(strings.NewReader(nestJSON(defaultStructuredMaxDepth-1)), "ok.json"); err != nil {
		t.Errorf("Parse() at depth %d error = %v, want nil with default depth %d", defaultStructuredMaxDepth, err, defaultStructuredMaxDepth)
	}
	if _, err := p.Parse(strings.NewReader(nestJSON(defaultStructuredMaxDepth+1)), "deep.json"); err == nil {
		t.Errorf("Parse() at depth %d should exceed the default max depth of %d", defaultStructuredMaxDepth+2, defaultStructuredMaxDepth)
	}
}

// TestBuildComponentFactoryWithFS_NilFSAndCustomExpander covers the
// nil-filesystem default and the custom-expander pass-through in the
// component factory.
type stubExpander struct{ called bool }

func (e *stubExpander) Expand(s string) (string, error) {
	e.called = true
	return s, nil
}

func TestBuildComponentFactoryWithFS_NilFSAndCustomExpander(t *testing.T) {
	cfg := DefaultConfig()
	exp := &stubExpander{}
	cfg.CustomExpander = exp

	factory := cfg.buildComponentFactoryWithFS(nil) // nil fs falls back to DefaultFileSystem
	defer func() { _ = factory.Close() }()          // best-effort cleanup

	if factory.Expander() == nil {
		t.Fatal("Expander() = nil, want the custom expander")
	}
	if _, err := factory.Expander().Expand("$X"); err != nil {
		t.Fatalf("custom Expand() error = %v", err)
	}
	if !exp.called {
		t.Error("factory.Expander() did not delegate to the custom expander")
	}
}

// recordingAuditor counts LogError calls; used to pin audit gating.
type recordingAuditor struct {
	count int
}

func (r *recordingAuditor) LogError(action AuditAction, key, errMsg string) error {
	r.count++
	return nil
}

// TestSetDelete_AuditGating pins AuditEnabled=false suppression with a CUSTOM
// auditor: Set/Delete success events previously reached custom auditors
// unconditionally (the adapter routes them through LogError), contradicting
// the documented "activates audit logging for all loader operations".
func TestSetDelete_AuditGating(t *testing.T) {
	rec := &recordingAuditor{}
	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.CustomAuditor = rec
	cfg.AuditEnabled = false
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.Set("KEY", "value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := loader.Delete("KEY"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if rec.count != 0 {
		t.Errorf("custom auditor received %d events with AuditEnabled=false, want 0", rec.count)
	}
}

// closableParser is a minimal EnvParser whose Close records being called.
type closableParser struct {
	closed *bool
}

func (p closableParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (p closableParser) Close() error {
	*p.closed = true
	return nil
}

// TestLoaderClose_ClosesCustomParsers pins that Loader.Close closes parsers
// implementing io.Closer — previously only createParsers' construction-
// failure path ever closed them, so a custom parser's resources leaked for
// the loader's whole lifetime.
func TestLoaderClose_ClosesCustomParsers(t *testing.T) {
	closed := false
	err := ForceRegisterParser(FormatEnv, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return closableParser{closed: &closed}, nil
	})
	if err != nil {
		t.Fatalf("ForceRegisterParser() error = %v", err)
	}
	// Restore the built-in .env parser for the rest of the package's tests —
	// the registry is process-global.
	defer func() {
		_ = ForceRegisterParser(FormatEnv, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
			return newParserWithFactory(cfg, factory)
		})
	}()

	cfg := DefaultConfig()
	cfg.Filenames = nil
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := loader.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closed {
		t.Error("custom parser was not closed by Loader.Close")
	}
}

// TestParserMaxVariablesCountsDistinctKeys pins the .env variable-count
// semantics aligned with the structured parsers: only DISTINCT keys count
// against MaxVariables. A duplicate line the duplicate policy would skip
// anyway previously tripped the limit at the exact boundary and failed the
// whole file.
func TestParserMaxVariablesCountsDistinctKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.MaxVariables = 2
	factory := cfg.buildComponentFactory()
	p, err := newParserWithFactory(cfg, factory)
	if err != nil {
		t.Fatalf("newParserWithFactory() error = %v", err)
	}

	m, err := p.Parse(strings.NewReader("A=1\nB=2\nB=3\n"), "")
	if err != nil {
		t.Fatalf("Parse(2 distinct + 1 duplicate) error = %v, want nil", err)
	}
	if len(m) != 2 {
		t.Errorf("Parse() produced %d keys, want 2", len(m))
	}

	if _, err := p.Parse(strings.NewReader("A=1\nB=2\nC=3\n"), ""); err == nil {
		t.Error("Parse(3 distinct keys with MaxVariables=2) succeeded, want max-variables error")
	}
}

// TestLoadFiles_WrappedNotExistSkipped pins that a custom FileSystem whose
// Open returns an error WRAPPING fs.ErrNotExist is treated as
// file-not-found: os.IsNotExist alone does not traverse wrapped chains, so
// FailOnMissingFile=false never skipped such files before.
func TestLoadFiles_WrappedNotExistSkipped(t *testing.T) {
	fs := newTestFileSystem()
	fs.openErr = fmt.Errorf("open failed: %w", os.ErrNotExist)

	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.FileSystem = fs
	cfg.FailOnMissingFile = false
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.LoadFiles("ghost.env"); err != nil {
		t.Errorf("LoadFiles() = %v, want wrapped not-exist skipped", err)
	}
}
