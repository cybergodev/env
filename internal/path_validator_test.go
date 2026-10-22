package internal

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// ============================================================================
// PathValidator Tests
// ============================================================================

// TestPathValidator_DotDotFilenames pins the per-component traversal check:
// interior double dots inside a NAME are legitimate ("my..file.env"), while
// a ".." COMPONENT anywhere in the (Clean-ed) path is traversal. The
// previous substring test rejected the former.
func TestPathValidator_DotDotFilenames(t *testing.T) {
	v := NewPathValidator(PathValidatorConfig{})

	valid := []string{
		"my..file.env",
		"config..backup.env",
		"a..b/c.env",
		"normal.env",
		"a.b/../c.env", // interior parent resolved away by Clean
	}
	for _, name := range valid {
		if err := v.Validate(name); err != nil {
			t.Errorf("Validate(%q) error = %v, want nil (interior dots are not traversal)", name, err)
		}
	}

	invalid := []string{
		"../x.env",
		"..\\x.env",
		"..",
		"../..",
		"a/../../b.env", // Clean → ../b.env: leading parent survives
	}
	for _, name := range invalid {
		if err := v.Validate(name); err == nil {
			t.Errorf("Validate(%q) = nil, want traversal error", name)
		}
	}
}

func TestPathValidator_Validate(t *testing.T) {
	v := NewPathValidator(PathValidatorConfig{})

	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string // partial match in error reason
	}{
		// Valid paths
		{"valid relative path", ".env", false, ""},
		{"valid nested path", "config/.env", false, ""},
		{"valid current dir", "./.env", false, ""},
		{"valid filename with extension", "app.production.env", false, ""},

		// Invalid paths - security checks
		{"empty path", "", true, "empty filename"},
		{"null byte", "file\x00.env", true, "null byte"},
		{"URL encoded", "file%2e.env", true, "URL encoded"},
		{"UNC path", "\\\\server\\share", true, "UNC path"},
		{"forward slash UNC", "//server/share", true, "network path"},
		{"Unix absolute path", "/etc/passwd", true, "rooted path"},
		// A single leading backslash is a drive-rooted path on Windows
		// ("\Windows\win.ini" resolves against the current drive's root)
		// even though filepath.IsAbs reports false for it; the forms below
		// all previously passed validation.
		{"backslash-rooted path", `\Windows\win.ini`, true, "rooted path"},
		{"backslash-rooted unix spelling", `\etc\passwd`, true, "rooted path"},
		{"root-local device NTFS", `\??\C:\x`, true, "rooted path"},
		{"root-local device dot", `\.\C:\x`, true, "rooted path"},
		// Drive-letter rows live in TestPathValidator_DriveLetterPerGOOS:
		// the check is Windows-only now ("c:config" is a legal relative
		// filename on Unix).
		{"path traversal", "../../../etc/passwd", true, "path traversal"},
		{"path traversal in middle", "config/../../../etc/passwd", true, "path traversal"},

		// Reserved names apply in ANY directory: non-leading components must
		// be checked too (D-002: only the first component was inspected).
		// These are the integration-shaped rows; the full per-device matrix
		// (bare CON/PRN/COM1-9/LPT1-9/pseudo devices, extensions, colons,
		// lowercase, negatives) lives in TestCheckReservedDeviceNames.
		{"device in subdir", "subdir/CON", true, "reserved device"},
		{"device after backslash", `backup\NUL.txt`, true, "reserved device"},
		{"device in later component", "a/../COM1", true, "reserved device"},
		{"pseudo device with extension in subdir", "logs/CONIN$.txt", true, "reserved"},

		// SEC-007 (CWE-117): control characters other than tab are rejected so
		// a crafted filename cannot inject forged audit-log records.
		{"newline in path", "evil\n.env", true, "control character"},
		{"carriage return in path", "evil\r.env", true, "control character"},
		{"NUL already covered", "evil\x01.env", true, "control character"},

		// Pins current behavior: non-ASCII components are ordinary names —
		// they never ASCII-fold onto a reserved device name.
		{"non-ASCII directory and file", "配置/.env", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				var secErr *SecurityError
				if !errors.As(err, &secErr) {
					t.Errorf("Validate(%q) error type = %T, want *SecurityError", tt.path, err)
					return
				}
				if tt.errMsg != "" && secErr.Reason != "" && !strings.Contains(secErr.Reason, tt.errMsg) {
					t.Errorf("Validate(%q) error reason = %q, should contain %q", tt.path, secErr.Reason, tt.errMsg)
				}
			}
		})
	}
}

func TestPathValidator_WithCustomMaskKey(t *testing.T) {
	customMaskCalled := false
	v := NewPathValidator(PathValidatorConfig{
		MaskKey: func(key string) string {
			customMaskCalled = true
			return "***MASKED***"
		},
	})

	// Path traversal triggers key masking
	err := v.Validate("../../../etc/passwd")
	if err == nil {
		t.Fatal("Validate() should fail for path traversal")
	}

	if !customMaskCalled {
		t.Error("custom MaskKey function should have been called")
	}
}

func TestValidateFilePath(t *testing.T) {
	// Test the convenience function
	if err := ValidateFilePath(".env"); err != nil {
		t.Errorf("ValidateFilePath(\".env\") error = %v", err)
	}

	if err := ValidateFilePath(""); err == nil {
		t.Error("ValidateFilePath(\"\") should return error")
	}
}

func TestDefaultPathValidator(t *testing.T) {
	// DefaultPathValidator should be usable
	if DefaultPathValidator == nil {
		t.Fatal("DefaultPathValidator should not be nil")
	}

	if err := DefaultPathValidator.Validate("valid.env"); err != nil {
		t.Errorf("DefaultPathValidator.Validate() error = %v", err)
	}
}

// ============================================================================
// checkReservedDeviceNames Tests
// ============================================================================

func TestCheckReservedDeviceNames(t *testing.T) {
	v := &PathValidator{maskKey: DefaultMaskKey}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Too short for reserved names
		{"short name", "ab", false},
		{"3 char non-reserved", "ABC", false},

		// CON, PRN, AUX, NUL (3-char reserved)
		{"CON", "CON", true},
		{"PRN", "PRN", true},
		{"AUX", "AUX", true},
		{"NUL", "NUL", true},
		{"con lowercase", "con", true},
		{"CON.txt", "CON.txt", true},
		{"CON:", "CON:", true},

		// COM/LPT ports (4-char reserved)
		{"COM1", "COM1", true},
		{"COM5", "COM5", true},
		{"COM9", "COM9", true},
		{"LPT1", "LPT1", true},
		{"LPT9", "LPT9", true},
		{"com1 lowercase", "com1", true},
		{"COM1.txt", "COM1.txt", true},
		{"COM1:", "COM1:", true}, // colon counts as an extension separator

		// Pseudo devices
		{"CONIN$", "CONIN$", true},
		{"CONOUT$", "CONOUT$", true},
		{"CLOCK$", "CLOCK$", true},

		// Non-reserved
		{"CONFIG", "CONFIG", false},
		{"COMPANY", "COMPANY", false},
		{"COM10", "COM10", false}, // COM10 is not reserved (only COM1-9)
		{"COM0", "COM0", false},   // COM0 is not reserved (only COM1-9)

		// Windows strips trailing spaces and dots from each component
		// before resolving names, so these all address the device — the
		// validator reduces to the base name before matching.
		{"CON with trailing space", "CON ", true},
		{"COM1 with trailing space", "COM1 ", true},
		{"CON space dot extension", "CON .txt", true},
		// A trailing dot IS caught: '.' is an accepted extension separator
		// (and Windows likewise treats "NUL." as a device).
		{"NUL with trailing dot", "NUL.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.checkReservedDeviceNames(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkReservedDeviceNames(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// ============================================================================
// validateResolvedPath Tests
// ============================================================================

func TestValidateResolvedPath(t *testing.T) {
	v := &PathValidator{maskKey: DefaultMaskKey}

	// BOUNDARY: the absolute-resolution error branch. filepath.IsAbs is
	// platform-dependent (drive letter on Windows, leading slash elsewhere),
	// so the absolute input is built per GOOS and the row runs everywhere.
	absResolved := "/resolved/etc/passwd"
	if runtime.GOOS == "windows" {
		absResolved = `C:\resolved\etc\passwd`
	}

	tests := []struct {
		name     string
		resolved string
		wantErr  bool
		errMsg   string
	}{
		// Safe resolved paths
		{"relative path", "config/.env", false, ""},
		{"current dir file", ".env", false, ""},
		{"nested relative", "a/b/c/.env", false, ""},

		// A symlink resolving to an absolute location must be rejected even
		// without any ".." component (the second of the two error branches).
		{"absolute resolved path", absResolved, true, "symlink resolves to absolute path"},

		// Path traversal in resolved path should be rejected
		{"path traversal resolved", "../etc/passwd", true, "symlink escapes allowed directory"},
		{"multi traversal resolved", "../../etc/passwd", true, "symlink escapes allowed directory"},

		// Edge cases
		{"empty resolved", "", false, ""},
		{"dot", ".", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateResolvedPath(tt.resolved)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResolvedPath(%q) error = %v, wantErr %v", tt.resolved, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				var secErr *SecurityError
				if !errors.As(err, &secErr) {
					t.Errorf("validateResolvedPath(%q) error type = %T, want *SecurityError", tt.resolved, err)
					return
				}
				if tt.errMsg != "" && secErr.Reason != "" && !strings.Contains(secErr.Reason, tt.errMsg) {
					t.Errorf("validateResolvedPath(%q) error reason = %q, should contain %q", tt.resolved, secErr.Reason, tt.errMsg)
				}
			}
		})
	}
}

// TestPathValidator_VeryLongPath pins current behavior for paths far beyond
// the classic 32k Windows limit: Validate has no length gate, so a very long
// relative path is accepted (an invalid length would surface later, at the
// file-operation layer).
func TestPathValidator_VeryLongPath(t *testing.T) {
	v := NewPathValidator(PathValidatorConfig{})
	long := strings.Repeat("ab/", 12000) + "x.env" // ~36k chars
	if err := v.Validate(long); err != nil {
		t.Errorf("Validate(36k-char relative path) error = %v, want nil (no length gate)", err)
	}
}

// TestPathValidator_DriveLetterPerGOOS pins the GOOS-gated drive-letter
// check: absolute drive paths are rejected on Windows, while the same
// spelling is a legal relative filename on Unix.
func TestPathValidator_DriveLetterPerGOOS(t *testing.T) {
	for _, p := range []string{"C:\\Windows", "c:\\test"} {
		err := DefaultPathValidator.Validate(p)
		if runtime.GOOS == "windows" {
			if err == nil {
				t.Errorf("Validate(%q) = nil on Windows, want drive-letter rejection", p)
			}
		} else if err != nil {
			t.Errorf("Validate(%q) = %v on Unix, want accepted relative filename", p, err)
		}
	}
}

// TestPathValidator_AlternateDataStreams pins the Windows NTFS ADS
// rejection: a colon in any component is stream syntax
// ("config/app.env:hidden" opens the "hidden" stream of app.env), a
// different file identity than the one validated — and one whose extension
// misleads DetectFormat. ':' stays legal on Unix.
func TestPathValidator_AlternateDataStreams(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ADS syntax is Windows-only; ':' is a legal filename byte elsewhere")
	}
	for _, p := range []string{`config/app.env:hidden`, `app.env:stream`, `dir\c:x`} {
		err := DefaultPathValidator.Validate(p)
		if err == nil {
			t.Errorf("Validate(%q) = nil, want alternate-data-stream rejection", p)
			continue
		}
		var secErr *SecurityError
		if !errors.As(err, &secErr) || !strings.Contains(secErr.Reason, "alternate data stream") {
			t.Errorf("Validate(%q) error = %v, want an alternate-data-stream SecurityError", p, err)
		}
	}
}

// TestPathValidator_SymlinkResolution covers validateSymlinks against the
// real filesystem (EvalSymlinks needs existing paths to resolve): an
// existing relative file validates, a missing file under an existing parent
// validates the parent instead, a fully missing tree is allowed (the file
// operation reports the failure later), and a symlink resolving to an
// absolute location is the escape attack the check exists to stop.
func TestPathValidator_SymlinkResolution(t *testing.T) {
	// EvalSymlinks resolves against the process working directory, so run
	// from a fresh temp dir (t.Chdir restores it afterwards).
	t.Chdir(t.TempDir())
	v := NewPathValidator(PathValidatorConfig{})

	t.Run("existing file resolves and validates", func(t *testing.T) {
		if err := os.WriteFile("real.env", []byte("KEY=1"), 0o600); err != nil {
			t.Fatalf("WriteFile(real.env) error = %v", err)
		}
		if err := v.validateSymlinks("real.env"); err != nil {
			t.Errorf("validateSymlinks(real.env) error = %v, want nil", err)
		}
	})

	t.Run("missing file under existing parent validates the parent", func(t *testing.T) {
		if err := os.Mkdir("sub", 0o750); err != nil {
			t.Fatalf("Mkdir(sub) error = %v", err)
		}
		if err := v.validateSymlinks("sub/missing.env"); err != nil {
			t.Errorf("validateSymlinks(sub/missing.env) error = %v, want nil", err)
		}
	})

	t.Run("missing file and missing parent is allowed", func(t *testing.T) {
		if err := v.validateSymlinks("nodir/missing.env"); err != nil {
			t.Errorf("validateSymlinks(nodir/missing.env) error = %v, want nil (deferred to the file operation)", err)
		}
	})

	t.Run("symlink resolving to an absolute path is rejected", func(t *testing.T) {
		target := t.TempDir() // absolute
		if err := os.Symlink(target, "escape.env"); err != nil {
			t.Skipf("symlink creation failed (%v) — needs Developer Mode or admin on Windows", err)
		}
		err := v.validateSymlinks("escape.env")
		if err == nil {
			t.Fatal("validateSymlinks(symlink to absolute) = nil, want SecurityError")
		}
		var secErr *SecurityError
		if !errors.As(err, &secErr) || !strings.Contains(secErr.Reason, "absolute path") {
			t.Errorf("validateSymlinks(symlink to absolute) error = %v, want an absolute-path SecurityError", err)
		}
	})
}
