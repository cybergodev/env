package internal

import (
	"errors"
	"strings"
	"testing"
)

func TestParseError(t *testing.T) {
	tests := []struct {
		name    string
		err     *ParseError
		wantMsg string
		hasFile bool
	}{
		{
			name:    "with file and line",
			err:     &ParseError{File: "test.env", Line: 10, Err: errors.New("syntax error")},
			wantMsg: "test.env",
			hasFile: true,
		},
		{
			name:    "without file",
			err:     &ParseError{Line: 5, Err: errors.New("syntax error")},
			wantMsg: "line 5",
			hasFile: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestParseErrorUnwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &ParseError{File: "test.env", Line: 1, Err: underlying}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlying)
	}

	// Test errors.Is compatibility
	if !errors.Is(err, underlying) {
		t.Error("errors.Is should match underlying error")
	}
}

func TestValidationError(t *testing.T) {
	tests := []struct {
		name    string
		err     *ValidationError
		wantMsg string
	}{
		{
			name:    "with field",
			err:     &ValidationError{Field: "API_KEY", Message: "is required"},
			wantMsg: "API_KEY",
		},
		{
			name:    "without field",
			err:     &ValidationError{Message: "validation failed"},
			wantMsg: "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestSecurityError(t *testing.T) {
	tests := []struct {
		name    string
		err     *SecurityError
		wantMsg string
	}{
		{
			name:    "with key",
			err:     &SecurityError{Action: "set", Key: "PATH", Reason: "forbidden key"},
			wantMsg: "PATH",
		},
		{
			name:    "without key",
			err:     &SecurityError{Action: "load", Reason: "file too large"},
			wantMsg: "load",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestFileError(t *testing.T) {
	tests := []struct {
		name    string
		err     *FileError
		wantMsg string
	}{
		{
			name:    "with size limit",
			err:     &FileError{Path: ".env", Op: "read", Err: ErrFileTooLarge, Size: 1024, Limit: 512},
			wantMsg: "exceeds limit",
		},
		{
			name:    "simple error",
			err:     &FileError{Path: ".env", Op: "open", Err: errors.New("permission denied")},
			wantMsg: ".env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestFileErrorUnwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &FileError{Path: "test.env", Op: "open", Err: underlying}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlying)
	}

	// Test errors.Is compatibility
	if !errors.Is(err, underlying) {
		t.Error("errors.Is should match underlying error")
	}
}

func TestExpansionError(t *testing.T) {
	err := &ExpansionError{
		Key:   "VAR",
		Depth: 10,
		Limit: 5,
		Chain: "A -> B -> C",
	}

	msg := err.Error()
	// Key is now masked for security
	// Keys <= 3 chars are fully masked as "***"
	if !strings.Contains(msg, "***") {
		t.Errorf("Error() = %q, want to contain masked key ***", msg)
	}
	if !strings.Contains(msg, "10") || !strings.Contains(msg, "5") {
		t.Errorf("Error() = %q, want to contain depth/limit", msg)
	}
}

func TestExpansionError_LongKey(t *testing.T) {
	err := &ExpansionError{
		Key:   "DATABASE_PASSWORD",
		Depth: 10,
		Limit: 5,
		Chain: "A -> B -> C",
	}

	msg := err.Error()
	// Keys > 3 chars are partially masked (first 2 chars + ***)
	if !strings.Contains(msg, "DA***") {
		t.Errorf("Error() = %q, want to contain masked key DA***", msg)
	}
	if !strings.Contains(msg, "10") || !strings.Contains(msg, "5") {
		t.Errorf("Error() = %q, want to contain depth/limit", msg)
	}
}

func TestMarshalError(t *testing.T) {
	tests := []struct {
		name    string
		err     *MarshalError
		wantMsg string
	}{
		{
			name:    "with field",
			err:     &MarshalError{Field: "Config", Message: "unsupported type"},
			wantMsg: "Config",
		},
		{
			name:    "without field",
			err:     &MarshalError{Message: "invalid struct"},
			wantMsg: "invalid struct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

// ============================================================================
// JSONError Tests
// ============================================================================

func TestJSONError(t *testing.T) {
	tests := []struct {
		name    string
		err     *JSONError
		wantMsg string
	}{
		{
			name:    "with path",
			err:     &JSONError{Path: "$.database.port", Message: "invalid type"},
			wantMsg: "$.database.port",
		},
		{
			name:    "without path",
			err:     &JSONError{Message: "unexpected EOF"},
			wantMsg: "JSON error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestJSONErrorUnwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &JSONError{Path: "$", Message: "parse failed", Err: underlying}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlying)
	}

	// Test errors.Is compatibility
	if !errors.Is(err, underlying) {
		t.Error("errors.Is should match underlying error")
	}

	// Test nil underlying error
	errNoUnderlying := &JSONError{Message: "no underlying"}
	if unwrapped := errNoUnderlying.Unwrap(); unwrapped != nil {
		t.Errorf("Unwrap() with nil Err = %v, want nil", unwrapped)
	}
}

// ============================================================================
// YAMLError Tests
// ============================================================================

func TestYAMLError(t *testing.T) {
	tests := []struct {
		name    string
		err     *YAMLError
		wantMsg string
	}{
		{
			name:    "with path and line",
			err:     &YAMLError{Path: "config.yaml", Line: 10, Column: 5, Message: "invalid indent"},
			wantMsg: "line 10",
		},
		{
			name:    "with path no location",
			err:     &YAMLError{Path: "config.yaml", Message: "duplicate key"},
			wantMsg: "config.yaml",
		},
		{
			name:    "with line only",
			err:     &YAMLError{Line: 15, Message: "unexpected character"},
			wantMsg: "line 15",
		},
		{
			name:    "without details",
			err:     &YAMLError{Message: "invalid YAML"},
			wantMsg: "YAML error",
		},
		{
			name:    "with line and column",
			err:     &YAMLError{Line: 20, Column: 10, Message: "parse error"},
			wantMsg: "col 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestYAMLErrorUnwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &YAMLError{Path: "test.yaml", Line: 5, Message: "parse failed", Err: underlying}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlying)
	}

	// Test errors.Is compatibility
	if !errors.Is(err, underlying) {
		t.Error("errors.Is should match underlying error")
	}

	// Test nil underlying error
	errNoUnderlying := &YAMLError{Message: "no underlying"}
	if unwrapped := errNoUnderlying.Unwrap(); unwrapped != nil {
		t.Errorf("Unwrap() with nil Err = %v, want nil", unwrapped)
	}
}

// ============================================================================
// Additional ExpansionError Tests
// ============================================================================

func TestExpansionError_NoKey(t *testing.T) {
	err := &ExpansionError{
		Key:   "",
		Depth: 10,
		Limit: 5,
		Chain: "A -> B -> C",
	}

	msg := err.Error()
	if !strings.Contains(msg, "chain") {
		t.Errorf("Error() = %q, want to contain 'chain'", msg)
	}
	if strings.Contains(msg, "\"\"") {
		t.Errorf("Error() should not contain empty key quotes, got %q", msg)
	}
}

// ============================================================================
// Sentinel Errors Tests
// ============================================================================

func TestValidationError_Is(t *testing.T) {
	tests := []struct {
		name string
		err  *ValidationError
		target error
		match  bool
	}{
		{"value rule matches ErrInvalidValue", &ValidationError{Rule: "value"}, ErrInvalidValue, true},
		{"null_byte rule matches ErrInvalidValue", &ValidationError{Rule: "null_byte"}, ErrInvalidValue, true},
		{"control_char rule matches ErrInvalidValue", &ValidationError{Rule: "control_char"}, ErrInvalidValue, true},
		{"utf8 rule matches ErrInvalidValue", &ValidationError{Rule: "utf8"}, ErrInvalidValue, true},
		{"key rule does not match ErrInvalidValue", &ValidationError{Rule: "key"}, ErrInvalidValue, false},
		{"empty rule does not match ErrInvalidValue", &ValidationError{Rule: ""}, ErrInvalidValue, false},
		{"config rule does not match ErrInvalidValue", &ValidationError{Rule: "config"}, ErrInvalidValue, false},
		{"does not match wrong target", &ValidationError{Rule: "value"}, ErrFileTooLarge, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.match {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, got, tt.match)
			}
		})
	}
}

func TestSecurityError_Is(t *testing.T) {
	err := &SecurityError{Action: "set", Key: "FORBIDDEN", Reason: "blocked"}

	if !errors.Is(err, ErrSecurityViolation) {
		t.Error("errors.Is(SecurityError, ErrSecurityViolation) = false, want true")
	}

	if errors.Is(err, ErrFileTooLarge) {
		t.Error("errors.Is(SecurityError, ErrFileTooLarge) = true, want false")
	}
}

func TestSentinelErrors(t *testing.T) {
	// Verify sentinel errors are defined
	sentinelErrors := []error{
		ErrFileTooLarge,
		ErrLineTooLong,
		ErrInvalidValue,
	}

	for i, err := range sentinelErrors {
		if err == nil {
			t.Errorf("sentinel error %d is nil", i)
		}
		if err.Error() == "" {
			t.Errorf("sentinel error %d has empty message", i)
		}
	}
}
