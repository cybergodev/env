package env

import (
	"errors"
	"strings"
	"testing"

	"github.com/cybergodev/env/internal"
)

// TestLoader_SetAutoApplyThenDelete verifies that a variable applied to the
// environment via Set (AutoApply) is removed from the environment when
// deleted. Regression test: Set previously did not set l.applied, so Delete
// skipped Unsetenv and leaked the variable into the process environment.
func TestLoader_SetAutoApplyThenDelete(t *testing.T) {
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
	if !loader.IsApplied() {
		t.Error("IsApplied() = false after Set() with AutoApply")
	}
	if _, ok := fs.LookupEnv("KEY"); !ok {
		t.Fatal("KEY should be set in the environment after Set() with AutoApply")
	}

	if err := loader.Delete("KEY"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := fs.LookupEnv("KEY"); ok {
		t.Error("KEY should be removed from the environment after Delete()")
	}
}

// TestLoader_Set_AutoApplySkipsExistingEnv verifies that Set with AutoApply
// honors the same skip policy as Apply: with OverwriteExisting disabled, a
// variable that already exists in the process environment is not overwritten
// there, while the loader's in-memory value is still updated (D-002/M-3:
// Set previously called Setenv unconditionally, diverging from Apply).
func TestLoader_Set_AutoApplySkipsExistingEnv(t *testing.T) {
	fs := newTestFileSystem()
	fs.env["EXISTING"] = "original"

	cfg := DefaultConfig() // OverwriteExisting defaults to false
	cfg.FileSystem = fs
	cfg.AutoApply = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.Set("EXISTING", "updated"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got := fs.env["EXISTING"]; got != "original" {
		t.Errorf("env[\"EXISTING\"] = %q, want %q (process env must not be overwritten)", got, "original")
	}
	if got, ok := loader.Lookup("EXISTING"); !ok || got != "updated" {
		t.Errorf("Lookup(\"EXISTING\") = %q, %v; want %q, true (in-memory value is still updated)", got, ok, "updated")
	}

	// Keys absent from the process environment are still applied.
	if err := loader.Set("NEW_KEY", "v"); err != nil {
		t.Fatalf("Set(NEW_KEY) error = %v", err)
	}
	if got := fs.env["NEW_KEY"]; got != "v" {
		t.Errorf("env[\"NEW_KEY\"] = %q, want %q", got, "v")
	}

	// With OverwriteExisting the existing process-env value IS overwritten.
	cfgOverwrite := cfg
	cfgOverwrite.OverwriteExisting = true
	loader2, err := New(cfgOverwrite)
	if err != nil {
		t.Fatalf("New(overwrite) error = %v", err)
	}
	defer func() { _ = loader2.Close() }()
	if err := loader2.Set("EXISTING", "updated2"); err != nil {
		t.Fatalf("Set(overwrite) error = %v", err)
	}
	if got := fs.env["EXISTING"]; got != "updated2" {
		t.Errorf("env[\"EXISTING\"] = %q, want %q with OverwriteExisting", got, "updated2")
	}
}

// TestSentinelErrorWiring verifies, via a table, that the sentinel errors
// documented on LoadFiles/Set/Validate/New are actually matched via errors.Is
// by the errors those code paths return.
func TestSentinelErrorWiring(t *testing.T) {
	tests := []struct {
		name    string
		trigger func() error
		wantIs  []error
	}{
		{
			name: "New with invalid config matches ErrInvalidConfig",
			trigger: func() error {
				cfg := DefaultConfig()
				cfg.MaxFileSize = -1
				_, err := New(cfg)
				return err
			},
			wantIs: []error{ErrInvalidConfig},
		},
		{
			name: "Set with invalid key matches ErrInvalidKey",
			trigger: func() error {
				loader, err := New(DefaultConfig())
				if err != nil {
					return err
				}
				defer func() { _ = loader.Close() }()
				return loader.Set("IN=VALID", "value")
			},
			wantIs: []error{ErrInvalidKey},
		},
		{
			name: "Set with forbidden key matches ErrForbiddenKey",
			trigger: func() error {
				loader, err := New(DefaultConfig())
				if err != nil {
					return err
				}
				defer func() { _ = loader.Close() }()
				return loader.Set("PATH", "/usr/bin/evil")
			},
			wantIs: []error{ErrForbiddenKey, ErrSecurityViolation},
		},
		{
			name: "Validate with missing required key matches ErrMissingRequired",
			trigger: func() error {
				cfg := DefaultConfig()
				cfg.Filenames = nil // no auto-load; empty loader state
				cfg.RequiredKeys = []string{"MUST_EXIST"}
				loader, err := New(cfg)
				if err != nil {
					return err
				}
				defer func() { _ = loader.Close() }()
				return loader.Validate()
			},
			wantIs: []error{ErrMissingRequired},
		},
		{
			name: "parse exceeding MaxVariables matches ErrMaxVariables",
			trigger: func() error {
				fs := newTestFileSystem()
				fs.files[".env"] = "A=1\nB=2\n"

				cfg := DefaultConfig()
				cfg.FileSystem = fs
				cfg.MaxVariables = 1
				_, err := New(cfg) // auto-loads ".env" from the mock fs
				return err
			},
			wantIs: []error{ErrMaxVariables},
		},
		{
			// ErrFileNotFound: LoadFiles returns it (wrapped in a FileError)
			// only when FailOnMissingFile is set; otherwise the file is skipped.
			name: "missing file with FailOnMissingFile matches ErrFileNotFound",
			trigger: func() error {
				cfg := DefaultConfig()
				cfg.FileSystem = newTestFileSystem() // no files at all
				cfg.Filenames = nil
				cfg.FailOnMissingFile = true
				loader, err := New(cfg)
				if err != nil {
					return err
				}
				defer func() { _ = loader.Close() }()
				return loader.LoadFiles("missing.env")
			},
			wantIs: []error{ErrFileNotFound},
		},
		{
			// ErrFileTooLarge: the pre-parse size check in loadFileLocked
			// wraps it in a FileError{Op: "size_check"}.
			name: "file above MaxFileSize matches ErrFileTooLarge",
			trigger: func() error {
				fs := newTestFileSystem()
				fs.files[".env"] = "KEY=longer-than-the-limit\n"

				cfg := DefaultConfig()
				cfg.FileSystem = fs
				cfg.MaxFileSize = 4
				_, err := New(cfg) // auto-load fails on the size check
				return err
			},
			wantIs: []error{ErrFileTooLarge},
		},
		{
			// ErrLineTooLong via structured parsing: a minified (single-line)
			// JSON document is one line for the MaxLineLength check; the
			// structured parsers wrap the failure in a FileError with
			// Op "size_check" (branch detail pinned in structured_parser_test.go).
			name: "structured line above MaxLineLength matches ErrLineTooLong",
			trigger: func() error {
				fs := newTestFileSystem()
				fs.files["cfg.json"] = `{"KEY":"` + strings.Repeat("x", 200) + `"}`

				cfg := DefaultConfig()
				cfg.FileSystem = fs
				cfg.Filenames = nil
				cfg.MaxLineLength = 32
				loader, err := New(cfg)
				if err != nil {
					return err
				}
				defer func() { _ = loader.Close() }()
				return loader.LoadFiles("cfg.json")
			},
			wantIs: []error{ErrLineTooLong},
		},
		{
			// ErrInvalidValue via the public value-validation path: Set
			// (ValidateValues on) rejects control characters with a
			// ValidationError{Rule: "control_char"} that matches the
			// sentinel. The typed getters (GetInt/GetBool/...) cannot serve
			// here: getWithDefault (accessors.go) swallows parse failures and
			// returns the default, and their ValidationErrors carry no Rule,
			// so they never match ErrInvalidValue — pinned as a comment
			// rather than an assertion.
			name: "Set with control character in value matches ErrInvalidValue",
			trigger: func() error {
				loader, err := New(DefaultConfig())
				if err != nil {
					return err
				}
				defer func() { _ = loader.Close() }()
				return loader.Set("KEY", "bad\x01value")
			},
			wantIs: []error{ErrInvalidValue},
		},
		{
			// ErrExpansionDepth: a self-referential expansion cycle
			// (A=${B}, B=${A}) detected while expanding after load. The
			// ExpansionError (depth or cycle kind) matches the sentinel.
			name: "expansion cycle matches ErrExpansionDepth",
			trigger: func() error {
				fs := newTestFileSystem()
				fs.files[".env"] = "A=${B}\nB=${A}\n"

				cfg := DefaultConfig()
				cfg.FileSystem = fs
				_, err := New(cfg) // auto-load expands and hits the cycle
				return err
			},
			wantIs: []error{ErrExpansionDepth},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.trigger()
			if err == nil {
				t.Fatal("trigger() error = nil, want an error")
			}
			for _, want := range tt.wantIs {
				if !errors.Is(err, want) {
					t.Errorf("errors.Is(err, %v) = false, err = %v", want, err)
				}
			}
		})
	}
}

// TestReExportedSentinels pins the re-export block in errors.go. Every
// aliased sentinel must be the SAME error value as its internal root: the
// public errors are assigned from the internal variables, so errors.Is holds
// by identity — a regression that re-declares one with errors.New breaks every
// errors.Is(err, env.ErrX) chain built on internal error values. The three
// package-owned sentinels (errors.New in errors.go) have no internal root;
// their contract is only non-nil with a non-empty message.
func TestReExportedSentinels(t *testing.T) {
	aliases := []struct {
		name string
		pub  error
		root error
	}{
		{"ErrFileTooLarge", ErrFileTooLarge, internal.ErrFileTooLarge},
		{"ErrLineTooLong", ErrLineTooLong, internal.ErrLineTooLong},
		{"ErrInvalidKey", ErrInvalidKey, internal.ErrInvalidKey},
		{"ErrForbiddenKey", ErrForbiddenKey, internal.ErrForbiddenKey},
		{"ErrSecurityViolation", ErrSecurityViolation, internal.ErrSecurityViolation},
		{"ErrExpansionDepth", ErrExpansionDepth, internal.ErrExpansionDepth},
		{"ErrMaxVariables", ErrMaxVariables, internal.ErrMaxVariables},
		{"ErrInvalidValue", ErrInvalidValue, internal.ErrInvalidValue},
		{"ErrMissingRequired", ErrMissingRequired, internal.ErrMissingRequired},
		{"ErrDuplicateKey", ErrDuplicateKey, internal.ErrDuplicateKey},
	}
	for _, tt := range aliases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.pub == nil {
				t.Fatalf("%s must not be nil", tt.name)
			}
			if tt.pub.Error() == "" {
				t.Errorf("%s must carry a non-empty message", tt.name)
			}
			if !errors.Is(tt.pub, tt.root) {
				t.Errorf("%s is not the same error value as its internal root; "+
					"errors.Is chains against internal error values would break", tt.name)
			}
		})
	}

	owned := []struct {
		name string
		err  error
	}{
		{"ErrFileNotFound", ErrFileNotFound},
		{"ErrClosed", ErrClosed},
		{"ErrInvalidConfig", ErrInvalidConfig},
	}
	for _, tt := range owned {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatalf("%s must not be nil", tt.name)
			}
			if tt.err.Error() == "" {
				t.Errorf("%s must carry a non-empty message", tt.name)
			}
		})
	}
}

// TestStructuredFormats_EnforceForbiddenKeys verifies JSON/YAML file loads
// enforce the forbidden-keys policy the same way the .env parser does.
// Regression test: the structured parsers validated only length/pattern/value,
// so a config.json containing "PATH" or "LD_PRELOAD" bypassed the policy and
// was applied to the process environment.
func TestStructuredFormats_EnforceForbiddenKeys(t *testing.T) {
	files := map[string]string{
		"config.json": `{"PATH": "/usr/evil/bin", "LD_PRELOAD": "/tmp/evil.so", "GOOD": "1"}`,
		"config.yaml": "PATH: /usr/evil/bin\nGOOD: 1\n",
	}

	for filename, content := range files {
		t.Run(filename, func(t *testing.T) {
			fs := newTestFileSystem()
			fs.files[filename] = content

			cfg := DefaultConfig()
			cfg.Filenames = []string{filename}
			cfg.FileSystem = fs
			cfg.AutoApply = true

			_, err := New(cfg)
			if err == nil {
				t.Fatal("New() should reject a file containing forbidden keys")
			}
			if !errors.Is(err, ErrForbiddenKey) {
				t.Errorf("errors.Is(err, ErrForbiddenKey) = false, err = %v", err)
			}
			if _, ok := fs.LookupEnv("PATH"); ok {
				t.Error("PATH must not be applied to the environment")
			}
		})
	}
}

// TestLoader_DeleteForeignEnvVar verifies Delete does not unset process
// environment variables the loader never applied. Regression test: Delete
// unset ANY key once the loader had applied something, clobbering foreign
// process variables like HOME.
func TestLoader_DeleteForeignEnvVar(t *testing.T) {
	fs := newTestFileSystem()
	if err := fs.Setenv("FOREIGN_KEY", "original"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.AutoApply = true
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Loader applied something of its own, then a foreign key is deleted.
	if err := loader.Set("OWN_KEY", "v"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if !loader.IsApplied() {
		t.Fatal("IsApplied() = false after applied Set()")
	}

	if err := loader.Delete("FOREIGN_KEY"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if v, ok := fs.LookupEnv("FOREIGN_KEY"); !ok || v != "original" {
		t.Errorf("foreign env var clobbered by Delete(): value = %q, exists = %v", v, ok)
	}
	// (The applied-key cleanup half of the Delete contract — Set with
	// AutoApply, then Delete, then the env var is gone — is pinned by
	// TestLoader_SetAutoApplyThenDelete above and TestLoader_ConcurrentAutoApply.)
}
