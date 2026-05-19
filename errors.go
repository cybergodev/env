package env

import (
	"errors"

	ierrors "github.com/cybergodev/env/internal"
)

// Sentinel errors provide simple error comparison using errors.Is().
var (
	// ErrFileNotFound indicates the specified file does not exist.
	ErrFileNotFound = errors.New("file not found")

	// ErrFileTooLarge indicates the file exceeds the maximum allowed size.
	// Re-exported from internal/errors for backward compatibility.
	ErrFileTooLarge = ierrors.ErrFileTooLarge

	// ErrLineTooLong indicates a line exceeds the maximum allowed length.
	// Re-exported from internal/errors for backward compatibility.
	ErrLineTooLong = ierrors.ErrLineTooLong

	// ErrInvalidKey indicates the key does not match the required pattern.
	ErrInvalidKey = errors.New("invalid key format")

	// ErrForbiddenKey indicates the key is not allowed for security reasons.
	ErrForbiddenKey = errors.New("key is forbidden for security reasons")

	// ErrSecurityViolation indicates a general security policy violation.
	// Re-exported from internal/errors for backward compatibility.
	ErrSecurityViolation = ierrors.ErrSecurityViolation

	// ErrExpansionDepth indicates variable expansion exceeded the maximum depth.
	ErrExpansionDepth = errors.New("variable expansion depth exceeded")

	// ErrMaxVariables indicates the maximum number of variables has been reached.
	ErrMaxVariables = errors.New("maximum number of variables exceeded")

	// ErrInvalidValue indicates the value contains invalid content.
	// Re-exported from internal/errors for backward compatibility.
	ErrInvalidValue = ierrors.ErrInvalidValue

	// ErrMissingRequired indicates a required key is missing.
	ErrMissingRequired = errors.New("required key is missing")

	// ErrDuplicateKey indicates a duplicate key was encountered.
	ErrDuplicateKey = errors.New("duplicate key encountered")

	// ErrClosed indicates the loader has been closed.
	ErrClosed = errors.New("loader has been closed")

	// ErrInvalidConfig indicates the configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")
)

// ParseError provides detailed information about parsing failures.
// This is an alias for internal.ParseError to maintain backward compatibility.
type ParseError = ierrors.ParseError

// ValidationError provides detailed information about validation failures.
// This is an alias for internal.ValidationError to maintain backward compatibility.
type ValidationError = ierrors.ValidationError

// SecurityError provides detailed information about security violations.
// This is an alias for internal.SecurityError to maintain backward compatibility.
type SecurityError = ierrors.SecurityError

// FileError provides detailed information about file-related errors.
// This is an alias for internal.FileError to maintain backward compatibility.
type FileError = ierrors.FileError

// ExpansionError provides detailed information about variable expansion failures.
// This is an alias for internal.ExpansionError to maintain backward compatibility.
type ExpansionError = ierrors.ExpansionError

// JSONError represents a JSON parsing error.
// This is an alias for internal.JSONError to maintain backward compatibility.
type JSONError = ierrors.JSONError

// YAMLError represents a YAML parsing error.
// This is an alias for internal.YAMLError to maintain backward compatibility.
type YAMLError = ierrors.YAMLError

// MarshalError represents a marshaling/unmarshaling error.
// This is an alias for internal.MarshalError to maintain backward compatibility.
type MarshalError = ierrors.MarshalError

// newParseError creates a new ParseError with sanitized content.
func newParseError(file string, line int, content string, err error) *ParseError {
	return &ParseError{
		File:    file,
		Line:    line,
		Content: MaskSensitiveInString(content),
		Err:     err,
	}
}

// newFileError creates a new FileError.
func newFileError(path, op string, err error) *FileError {
	return &FileError{
		Path: path,
		Op:   op,
		Err:  err,
	}
}
