package internal

import (
	"testing"
)

// ============================================================================
// PathValidator Tests
// ============================================================================

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
		{"Unix absolute path", "/etc/passwd", true, "absolute path"},
		{"Windows drive letter", "C:\\Windows", true, "drive letter"},
		{"lowercase drive letter", "c:\\test", true, "drive letter"},
		{"path traversal", "../../../etc/passwd", true, "path traversal"},
		{"path traversal in middle", "config/../../../etc/passwd", true, "path traversal"},

		// Windows reserved device names
		{"CON device", "CON", true, "reserved device"},
		{"PRN device", "PRN", true, "reserved device"},
		{"AUX device", "AUX", true, "reserved device"},
		{"NUL device", "NUL", true, "reserved device"},
		{"COM1 device", "COM1", true, "reserved device"},
		{"COM9 device", "COM9", true, "reserved device"},
		{"LPT1 device", "LPT1", true, "reserved device"},
		{"LPT9 device", "LPT9", true, "reserved device"},
		{"CONIN$ device", "CONIN$", true, "reserved"},
		{"CONOUT$ device", "CONOUT$", true, "reserved"},
		{"CLOCK$ device", "CLOCK$", true, "reserved"},
		{"device with extension", "CON.txt", true, "reserved device"},
		{"device with colon", "COM1:", true, "reserved device"},
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
				if !AsError(err, &secErr) {
					t.Errorf("Validate(%q) error type = %T, want *SecurityError", tt.path, err)
					return
				}
				if tt.errMsg != "" && secErr.Reason != "" {
					// Check if error reason contains expected substring
					found := false
					for i := 0; i <= len(secErr.Reason)-len(tt.errMsg); i++ {
						if secErr.Reason[i:i+len(tt.errMsg)] == tt.errMsg {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Validate(%q) error reason = %q, should contain %q", tt.path, secErr.Reason, tt.errMsg)
					}
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

		// Pseudo devices
		{"CONIN$", "CONIN$", true},
		{"CONOUT$", "CONOUT$", true},
		{"CLOCK$", "CLOCK$", true},

		// Non-reserved
		{"CONFIG", "CONFIG", false},
		{"COMPANY", "COMPANY", false},
		{"COM10", "COM10", false}, // COM10 is not reserved (only COM1-9)
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
				if !AsError(err, &secErr) {
					t.Errorf("validateResolvedPath(%q) error type = %T, want *SecurityError", tt.resolved, err)
					return
				}
				if tt.errMsg != "" && secErr.Reason != "" {
					found := false
					for i := 0; i <= len(secErr.Reason)-len(tt.errMsg); i++ {
						if secErr.Reason[i:i+len(tt.errMsg)] == tt.errMsg {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("validateResolvedPath(%q) error reason = %q, should contain %q", tt.resolved, secErr.Reason, tt.errMsg)
					}
				}
			}
		})
	}
}
