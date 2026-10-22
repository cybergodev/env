package internal

import (
	"errors"
	"strings"
	"testing"
)

// ============================================================================
// Error() Message Tests
// ============================================================================

// TestErrorMessage merges the per-type Error() tests into a single table,
// pinning the exact message form of every formatting branch (with/without
// optional fields, plus the FileError size/limit boundary).
func TestErrorMessage(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantSub []string
		notSub  []string
	}{
		// ParseError
		{
			name:    "ParseError with file and line",
			err:     &ParseError{File: "test.env", Line: 10, Err: errors.New("syntax error")},
			wantSub: []string{"parse error in test.env at line 10: syntax error"},
		},
		{
			name:    "ParseError without file",
			err:     &ParseError{Line: 5, Err: errors.New("syntax error")},
			wantSub: []string{"parse error at line 5: syntax error"},
		},
		// ValidationError
		{
			name:    "ValidationError with field",
			err:     &ValidationError{Field: "API_KEY", Message: "is required"},
			wantSub: []string{`validation error on field "API_KEY": is required`},
		},
		{
			name:    "ValidationError without field",
			err:     &ValidationError{Message: "validation failed"},
			wantSub: []string{"validation error: validation failed"},
		},
		// SecurityError
		{
			name:    "SecurityError with key",
			err:     &SecurityError{Action: "set", Key: "PATH", Reason: "forbidden key"},
			wantSub: []string{`security violation: set blocked for key "PATH": forbidden key`},
		},
		{
			name:    "SecurityError without key",
			err:     &SecurityError{Action: "load", Reason: "file too large"},
			wantSub: []string{"security violation: load blocked: file too large"},
		},
		// FileError
		{
			name:    "FileError with size and limit",
			err:     &FileError{Path: ".env", Op: "read", Err: ErrFileTooLarge, Size: 1024, Limit: 512},
			wantSub: []string{"file error: read .env (size 1024 exceeds limit 512): file exceeds maximum size limit"},
		},
		{
			name:    "FileError simple",
			err:     &FileError{Path: ".env", Op: "open", Err: errors.New("permission denied")},
			wantSub: []string{"file error: open .env: permission denied"},
		},
		{
			// BOUNDARY (errors.go ~L154): the size/limit arm requires BOTH
			// Size > 0 and Limit > 0; a Size without a Limit falls back to the
			// simple form with no size/limit rendering.
			name:    "FileError size without limit keeps the simple form",
			err:     &FileError{Path: ".env", Op: "read", Err: ErrFileTooLarge, Size: 1024},
			wantSub: []string{"file error: read .env: file exceeds maximum size limit"},
			notSub:  []string{"exceeds limit", "size 1024"},
		},
		// MarshalError
		{
			name:    "MarshalError with field",
			err:     &MarshalError{Field: "Config", Message: "unsupported type"},
			wantSub: []string{`marshal error on field "Config": unsupported type`},
		},
		{
			name:    "MarshalError without field",
			err:     &MarshalError{Message: "invalid struct"},
			wantSub: []string{"marshal error: invalid struct"},
		},
		// JSONError
		{
			name:    "JSONError with path",
			err:     &JSONError{Path: "$.database.port", Message: "invalid type"},
			wantSub: []string{"JSON error at $.database.port: invalid type"},
		},
		{
			name:    "JSONError without path",
			err:     &JSONError{Message: "unexpected EOF"},
			wantSub: []string{"JSON error: unexpected EOF"},
		},
		// YAMLError
		{
			name:    "YAMLError with path, line and column",
			err:     &YAMLError{Path: "config.yaml", Line: 10, Column: 5, Message: "invalid indent"},
			wantSub: []string{"YAML error at config.yaml (line 10, col 5): invalid indent"},
		},
		{
			name:    "YAMLError with path only",
			err:     &YAMLError{Path: "config.yaml", Message: "duplicate key"},
			wantSub: []string{"YAML error at config.yaml: duplicate key"},
		},
		{
			name:    "YAMLError with line only",
			err:     &YAMLError{Line: 15, Message: "unexpected character"},
			wantSub: []string{"YAML error (line 15): unexpected character"},
		},
		{
			name:    "YAMLError with line and column",
			err:     &YAMLError{Line: 20, Column: 10, Message: "parse error"},
			wantSub: []string{"YAML error (line 20, col 10): parse error"},
		},
		{
			name:    "YAMLError without details",
			err:     &YAMLError{Message: "invalid YAML"},
			wantSub: []string{"YAML error: invalid YAML"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, want := range tt.wantSub {
				if !strings.Contains(msg, want) {
					t.Errorf("Error() = %q, want to contain %q", msg, want)
				}
			}
			for _, not := range tt.notSub {
				if strings.Contains(msg, not) {
					t.Errorf("Error() = %q, should not contain %q", msg, not)
				}
			}
		})
	}
}

// ============================================================================
// Unwrap Tests
// ============================================================================

// TestErrorUnwrap merges the per-type Unwrap tests into one table. Rows with
// wantUnwrap nil pin that a missing underlying error unwraps to nil; the
// other rows additionally verify errors.Is reaches the underlying error.
func TestErrorUnwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	tests := []struct {
		name string
		err  interface {
			error
			Unwrap() error
		}
		wantUnwrap error
	}{
		{"ParseError unwraps its underlying error", &ParseError{File: "test.env", Line: 1, Err: underlying}, underlying},
		{"ParseError without underlying unwraps to nil", &ParseError{Line: 1}, nil},
		{"FileError unwraps its underlying error", &FileError{Path: "test.env", Op: "open", Err: underlying}, underlying},
		{"FileError without underlying unwraps to nil", &FileError{Path: "test.env", Op: "open"}, nil},
		{"JSONError unwraps its underlying error", &JSONError{Path: "$", Message: "parse failed", Err: underlying}, underlying},
		{"JSONError without underlying unwraps to nil", &JSONError{Message: "no underlying"}, nil},
		{"YAMLError unwraps its underlying error", &YAMLError{Path: "test.yaml", Line: 5, Message: "parse failed", Err: underlying}, underlying},
		{"YAMLError without underlying unwraps to nil", &YAMLError{Message: "no underlying"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Unwrap(); got != tt.wantUnwrap {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantUnwrap)
			}
			if tt.wantUnwrap != nil && !errors.Is(tt.err, tt.wantUnwrap) {
				t.Error("errors.Is should match the underlying error")
			}
		})
	}
}

// ============================================================================
// ExpansionError Tests
// ============================================================================

// TestExpansionErrorMessage pins the per-Kind rendering of ExpansionError:
// required-variable and cycle errors must not masquerade as depth-limit
// overflows ("(0/0)"), the diagnostic Chain must survive when set, and key
// names must be masked (DefaultMaskKey: "***" for keys <= 3 chars, first 2
// chars + "***" for longer keys).
func TestExpansionErrorMessage(t *testing.T) {
	tests := []struct {
		name    string
		err     *ExpansionError
		wantSub []string
		notSub  []string
	}{
		{
			name:    "required variable names the cause",
			err:     &ExpansionError{Key: "API_KEY", Kind: ExpansionRequiredKind, Chain: "required variable not set"},
			wantSub: []string{"required variable", "not set"},
			notSub:  []string{"depth limit"},
		},
		{
			name:    "cycle error carries the cause, not (0/0)",
			err:     &ExpansionError{Key: "A", Chain: "cycle detected"},
			wantSub: []string{"cycle detected"},
			notSub:  []string{"(0/0)", "depth limit"},
		},
		{
			name:    "anonymous depth error keeps depth and chain",
			err:     &ExpansionError{Depth: 5, Limit: 10, Chain: "A > B"},
			wantSub: []string{"depth limit exceeded (5/10)", "A > B"},
			notSub:  []string{"key"},
		},
		{
			name:    "keyed depth error with chain keeps both",
			err:     &ExpansionError{Key: "A", Depth: 5, Limit: 10, Chain: "A > B > C"},
			wantSub: []string{"exceeded depth limit (5/10)", "A > B > C"},
			notSub:  []string{"(0/0)"},
		},
		{
			name:    "keyed depth error without chain",
			err:     &ExpansionError{Key: "A", Depth: 5, Limit: 10},
			wantSub: []string{"exceeded depth limit (5/10)"},
			notSub:  []string{"(0/0)", "chain"},
		},
		{
			// Folded from TestExpansionError: keys are masked to the fixed
			// token (SEC-005) and the raw key must never leak into the message.
			name:    "short key is fully masked",
			err:     &ExpansionError{Key: "VAR", Depth: 10, Limit: 5, Chain: "A -> B -> C"},
			wantSub: []string{`key "[MASKED]" exceeded depth limit (10/5)`, "chain: A -> B -> C"},
			notSub:  []string{"VAR"},
		},
		{
			// Folded from TestExpansionError_LongKey: key spelling is never
			// partially revealed (SEC-005).
			name:    "long key is masked",
			err:     &ExpansionError{Key: "DATABASE_PASSWORD", Depth: 10, Limit: 5, Chain: "A -> B -> C"},
			wantSub: []string{`key "[MASKED]" exceeded depth limit (10/5)`},
			notSub:  []string{"DATABASE_PASSWORD", "DA"},
		},
		{
			// Folded from TestExpansionError_NoKey: an empty Key renders the
			// anonymous depth form with the chain, never `""`.
			name:    "empty key renders the anonymous depth form",
			err:     &ExpansionError{Depth: 10, Limit: 5, Chain: "A -> B -> C"},
			wantSub: []string{"depth limit exceeded (10/5)", "chain: A -> B -> C"},
			notSub:  []string{`""`},
		},
		{
			// BOUNDARY (errors.go ~L198-200): the empty-Key branch returns
			// BEFORE the cycle arm, so an empty-key cycle error degrades to
			// the (0/0) depth rendering instead of the cycle form.
			name:    "empty key in the cycle form falls back to the depth rendering",
			err:     &ExpansionError{Chain: "cycle detected"},
			wantSub: []string{"depth limit exceeded (0/0)", "chain: cycle detected"},
			notSub:  []string{"key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, want := range tt.wantSub {
				if !strings.Contains(msg, want) {
					t.Errorf("Error() = %q, want to contain %q", msg, want)
				}
			}
			for _, not := range tt.notSub {
				if strings.Contains(msg, not) {
					t.Errorf("Error() = %q, should not contain %q", msg, not)
				}
			}
		})
	}
}

func TestExpansionError_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    *ExpansionError
		target error
		match  bool
	}{
		{"depth error matches ErrExpansionDepth", &ExpansionError{Depth: 3, Limit: 2}, ErrExpansionDepth, true},
		{"zero-value kind (depth/cycle) matches", &ExpansionError{Chain: "A -> B -> A"}, ErrExpansionDepth, true},
		{"required-variable error does not match", &ExpansionError{Kind: ExpansionRequiredKind, Key: "VAR"}, ErrExpansionDepth, false},
		{"does not match wrong target", &ExpansionError{Depth: 3, Limit: 2}, ErrInvalidValue, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.match {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, got, tt.match)
			}
		})
	}
}

// ============================================================================
// Is() Matching Tests
// ============================================================================

func TestValidationError_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    *ValidationError
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
		{"key pattern rule matches ErrInvalidKey", &ValidationError{Field: "key", Rule: "pattern"}, ErrInvalidKey, true},
		{"key non_empty rule matches ErrInvalidKey", &ValidationError{Field: "key", Rule: "non_empty"}, ErrInvalidKey, true},
		{"key max_length rule matches ErrInvalidKey", &ValidationError{Field: "key", Rule: "max_length"}, ErrInvalidKey, true},
		{"key ascii_only rule matches ErrInvalidKey", &ValidationError{Field: "key", Rule: "ascii_only"}, ErrInvalidKey, true},
		{"pattern rule on non-key field does not match ErrInvalidKey", &ValidationError{Field: "value", Rule: "pattern"}, ErrInvalidKey, false},
		{"required rule matches ErrMissingRequired", &ValidationError{Rule: "required"}, ErrMissingRequired, true},
		{"max_variables rule matches ErrMaxVariables", &ValidationError{Rule: "max_variables"}, ErrMaxVariables, true},
		{"value rule does not match ErrInvalidKey", &ValidationError{Rule: "value"}, ErrInvalidKey, false},
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

	// key_access rejections match ErrForbiddenKey in addition to ErrSecurityViolation.
	forbidden := &SecurityError{Action: "key_access", Reason: "key is forbidden"}
	if !errors.Is(forbidden, ErrForbiddenKey) {
		t.Error("errors.Is(key_access SecurityError, ErrForbiddenKey) = false, want true")
	}
	if !errors.Is(forbidden, ErrSecurityViolation) {
		t.Error("errors.Is(key_access SecurityError, ErrSecurityViolation) = false, want true")
	}

	// Non-key_access security errors do not match ErrForbiddenKey.
	pathErr := &SecurityError{Action: "file_access", Reason: "path traversal detected"}
	if errors.Is(pathErr, ErrForbiddenKey) {
		t.Error("errors.Is(file_access SecurityError, ErrForbiddenKey) = true, want false")
	}
}

// ============================================================================
// Sentinel Errors Tests
// ============================================================================

// TestSentinelErrors verifies every sentinel defined in errors.go is non-nil
// with a non-empty message.
func TestSentinelErrors(t *testing.T) {
	sentinelErrors := []struct {
		name string
		err  error
	}{
		{"ErrFileTooLarge", ErrFileTooLarge},
		{"ErrLineTooLong", ErrLineTooLong},
		{"ErrInvalidValue", ErrInvalidValue},
		{"ErrSecurityViolation", ErrSecurityViolation},
		{"ErrInvalidKey", ErrInvalidKey},
		{"ErrForbiddenKey", ErrForbiddenKey},
		{"ErrMaxVariables", ErrMaxVariables},
		{"ErrDuplicateKey", ErrDuplicateKey},
		{"ErrMissingRequired", ErrMissingRequired},
		{"ErrExpansionDepth", ErrExpansionDepth},
	}

	for _, tt := range sentinelErrors {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatalf("%s is nil", tt.name)
			}
			if tt.err.Error() == "" {
				t.Errorf("%s has an empty message", tt.name)
			}
		})
	}
}
