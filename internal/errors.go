// Package internal provides shared internal types and utilities.
package internal

import (
	"errors"
	"fmt"
)

// Sentinel errors used by internal packages.
var (
	// ErrFileTooLarge indicates the file exceeds the maximum allowed size.
	ErrFileTooLarge = fmt.Errorf("file exceeds maximum size limit")

	// ErrLineTooLong indicates a line exceeds the maximum allowed length.
	ErrLineTooLong = fmt.Errorf("line exceeds maximum length limit")

	// ErrInvalidValue indicates a value is invalid.
	ErrInvalidValue = errors.New("invalid value content")

	// ErrSecurityViolation indicates a security policy violation.
	ErrSecurityViolation = errors.New("security policy violation")
)

// ParseError provides detailed information about parsing failures.
type ParseError struct {
	File    string // The file being parsed (if applicable)
	Line    int    // The line number where the error occurred
	Content string // Sanitized content (sensitive data masked)
	Err     error  // The underlying error
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("parse error in %s at line %d: %s", e.File, e.Line, e.Err)
	}
	return fmt.Sprintf("parse error at line %d: %s", e.Line, e.Err)
}

// Unwrap returns the underlying error for errors.Is() and errors.As().
func (e *ParseError) Unwrap() error {
	return e.Err
}

// ValidationError provides detailed information about validation failures.
type ValidationError struct {
	Field   string // The field that failed validation
	Value   string // Sanitized value (sensitive data masked)
	Rule    string // The validation rule that was violated
	Message string // Human-readable explanation
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error on field %q: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// Is implements errors.Is for ValidationError.
// Only matches ErrInvalidValue when the error is value-related (not key or config errors).
func (e *ValidationError) Is(target error) bool {
	if target == ErrInvalidValue {
		switch e.Rule {
		case "value", "null_byte", "control_char", "utf8":
			return true
		}
	}
	return false
}

// SecurityError provides detailed information about security violations.
type SecurityError struct {
	Action  string // The action that was blocked
	Reason  string // The security reason for blocking
	Key     string // The key involved (if applicable, sanitized)
	Details string // Additional sanitized details
}

// Error implements the error interface.
func (e *SecurityError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("security violation: %s blocked for key %q: %s", e.Action, e.Key, e.Reason)
	}
	return fmt.Sprintf("security violation: %s blocked: %s", e.Action, e.Reason)
}

// Is implements errors.Is for SecurityError.
// This allows errors.Is(err, ErrSecurityViolation) to match SecurityError.
func (e *SecurityError) Is(target error) bool {
	return target == ErrSecurityViolation
}

// FileError provides detailed information about file-related errors.
type FileError struct {
	Path  string // The file path
	Op    string // The operation that failed (open, read, stat)
	Err   error  // The underlying error
	Size  int64  // File size if relevant
	Limit int64  // The limit that was exceeded if relevant
}

// Error implements the error interface.
func (e *FileError) Error() string {
	if e.Size > 0 && e.Limit > 0 {
		return fmt.Sprintf("file error: %s %s (size %d exceeds limit %d): %v", e.Op, e.Path, e.Size, e.Limit, e.Err)
	}
	return fmt.Sprintf("file error: %s %s: %v", e.Op, e.Path, e.Err)
}

// Unwrap returns the underlying error for errors.Is() and errors.As().
func (e *FileError) Unwrap() error {
	return e.Err
}

// ExpansionError provides detailed information about variable expansion failures.
type ExpansionError struct {
	Key   string // The key being expanded
	Depth int    // The current expansion depth
	Limit int    // The maximum allowed depth
	Chain string // The expansion chain (sanitized)
}

// Error implements the error interface.
// SECURITY: Key names are masked to prevent sensitive information leakage.
// Only the first 2 characters of the key are shown followed by "***".
func (e *ExpansionError) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("expansion error: depth limit exceeded (%d/%d), chain: %s", e.Depth, e.Limit, e.Chain)
	}
	// Mask the key to prevent leaking sensitive key names in error messages
	maskedKey := maskKeyName(e.Key)
	return fmt.Sprintf("expansion error: key %q exceeded depth limit (%d/%d)", maskedKey, e.Depth, e.Limit)
}

// maskKeyName masks a key name for safe error reporting.
// Delegates to DefaultMaskKey for consistency with the rest of the codebase.
func maskKeyName(key string) string {
	return DefaultMaskKey(key)
}

// JSONError represents a JSON parsing error.
type JSONError struct {
	Path    string // JSON path where error occurred
	Message string
	Err     error
}

// Error implements the error interface.
func (e *JSONError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("JSON error at %s: %s", e.Path, e.Message)
	}
	return fmt.Sprintf("JSON error: %s", e.Message)
}

// Unwrap returns the underlying error for errors.Is() and errors.As().
func (e *JSONError) Unwrap() error {
	return e.Err
}

// YAMLError represents a YAML parsing error.
type YAMLError struct {
	Path    string // YAML path where error occurred
	Line    int    // Line number where error occurred
	Column  int    // Column number where error occurred
	Message string
	Err     error
}

// Error implements the error interface.
func (e *YAMLError) Error() string {
	var location string
	if e.Path != "" {
		location = fmt.Sprintf(" at %s", e.Path)
	}
	if e.Line > 0 {
		if e.Column > 0 {
			location = fmt.Sprintf("%s (line %d, col %d)", location, e.Line, e.Column)
		} else {
			location = fmt.Sprintf("%s (line %d)", location, e.Line)
		}
	}
	if location != "" {
		return fmt.Sprintf("YAML error%s: %s", location, e.Message)
	}
	return fmt.Sprintf("YAML error: %s", e.Message)
}

// Unwrap returns the underlying error for errors.Is() and errors.As().
func (e *YAMLError) Unwrap() error {
	return e.Err
}

// MarshalError represents a marshaling/unmarshaling error.
type MarshalError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *MarshalError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("marshal error on field %q: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("marshal error: %s", e.Message)
}
