// Provides shared internal types and utilities.

package internal

import (
	"errors"
	"fmt"
)

// Sentinel errors used by internal packages.
var (
	// ErrFileTooLarge indicates the file exceeds the maximum allowed size.
	ErrFileTooLarge = errors.New("file exceeds maximum size limit")

	// ErrLineTooLong indicates a line exceeds the maximum allowed length.
	ErrLineTooLong = errors.New("line exceeds maximum length limit")

	// ErrInvalidValue indicates a value is invalid.
	ErrInvalidValue = errors.New("invalid value content")

	// ErrSecurityViolation indicates a security policy violation.
	ErrSecurityViolation = errors.New("security policy violation")

	// ErrInvalidKey indicates the key does not match the required pattern.
	// Matches key-rule ValidationErrors via errors.Is.
	ErrInvalidKey = errors.New("invalid key format")

	// ErrForbiddenKey indicates the key is not allowed for security reasons.
	// Matches key_access SecurityErrors (forbidden list / allowed list) via errors.Is.
	ErrForbiddenKey = errors.New("key is forbidden for security reasons")

	// ErrMaxVariables indicates the maximum number of variables has been reached.
	// Matches the max-variables ValidationError (Rule "max_variables") via errors.Is.
	ErrMaxVariables = errors.New("maximum number of variables exceeded")

	// ErrDuplicateKey indicates a duplicate key was encountered. Matches, via
	// errors.Is, the parse errors returned under the DuplicateKeyError policy
	// (.env / YAML / JSON, including inline JSON inside YAML) and the always-on
	// flat-key collision errors from the YAML/JSON flatteners.
	ErrDuplicateKey = errors.New("duplicate key encountered")

	// ErrAuditFailed indicates an audit event was lost while fail-closed
	// auditing was enabled (Config.AuditFailClosed): the handler errored,
	// panicked, or dropped the event on a full channel. The operation that
	// lost its audit trail reports this error instead of succeeding
	// silently.
	ErrAuditFailed = errors.New("audit logging failed")

	// ErrMissingRequired indicates a required key is missing.
	// Matches the required-keys ValidationError (Rule "required") via errors.Is.
	ErrMissingRequired = errors.New("required key is missing")

	// ErrExpansionDepth indicates variable expansion exceeded the maximum depth
	// or hit a variable cycle. errors.Is(err, ErrExpansionDepth) matches an
	// *ExpansionError whose Kind is ExpansionDepthKind (the common case).
	ErrExpansionDepth = errors.New("variable expansion depth exceeded")
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
// Matches sentinels by rule category:
//   - ErrInvalidValue: value-related rules (value, null_byte, control_char, utf8)
//   - ErrInvalidKey: key-related rules on field "key" (non_empty, max_length, ascii_only, pattern)
//   - ErrMissingRequired: rule "required"
//   - ErrMaxVariables: rule "max_variables"
func (e *ValidationError) Is(target error) bool {
	switch target {
	case ErrInvalidValue:
		switch e.Rule {
		case "value", "null_byte", "control_char", "utf8":
			return true
		}
	case ErrInvalidKey:
		if e.Field == "key" {
			switch e.Rule {
			case "non_empty", "max_length", "ascii_only", "pattern":
				return true
			}
		}
	case ErrMissingRequired:
		return e.Rule == "required"
	case ErrMaxVariables:
		return e.Rule == "max_variables"
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
// key_access rejections (forbidden list / not in allowed list) additionally
// match ErrForbiddenKey.
func (e *SecurityError) Is(target error) bool {
	if target == ErrSecurityViolation {
		return true
	}
	return target == ErrForbiddenKey && e.Action == actionKeyAccess
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

// ExpansionErrorKind classifies the cause of an ExpansionError.
type ExpansionErrorKind int

const (
	// ExpansionDepthKind indicates the expansion hit a recursion-depth limit or a
	// variable cycle. This is the zero value so the common depth/cycle errors need
	// no explicit classification. errors.Is(err, ErrExpansionDepth) matches them.
	ExpansionDepthKind ExpansionErrorKind = iota

	// ExpansionRequiredKind indicates a required variable (${VAR:?message}) was
	// unset or empty. This is not a depth overflow, so it does not match
	// ErrExpansionDepth.
	ExpansionRequiredKind
)

// ExpansionError provides detailed information about variable expansion failures.
type ExpansionError struct {
	Key   string             // The key being expanded
	Depth int                // The current expansion depth
	Limit int                // The maximum allowed depth
	Chain string             // The expansion chain (sanitized)
	Kind  ExpansionErrorKind // The cause category (zero value = depth/cycle)
}

// Error implements the error interface.
// SECURITY: Key names are masked to prevent sensitive information leakage.
// Only the first 2 characters of the key are shown followed by "***".
func (e *ExpansionError) Error() string {
	// Mask the key to prevent leaking sensitive key names in error messages
	maskedKey := maskKeyName(e.Key)
	if e.Kind == ExpansionRequiredKind {
		return fmt.Sprintf("expansion error: required variable %q is not set", maskedKey)
	}
	if e.Key == "" {
		return fmt.Sprintf("expansion error: depth limit exceeded (%d/%d), chain: %s", e.Depth, e.Limit, e.Chain)
	}
	if e.Depth == 0 && e.Limit == 0 && e.Chain != "" {
		// Cycle-detection error from ExpandAllInMap: no depth was actually
		// exceeded — rendering it as "exceeded depth limit (0/0)" (the old
		// behavior for every Key-carrying error) misdiagnosed the cause and
		// dropped the chain.
		return fmt.Sprintf("expansion error: key %q: %s", maskedKey, e.Chain)
	}
	if e.Chain != "" {
		return fmt.Sprintf("expansion error: key %q exceeded depth limit (%d/%d), chain: %s", maskedKey, e.Depth, e.Limit, e.Chain)
	}
	return fmt.Sprintf("expansion error: key %q exceeded depth limit (%d/%d)", maskedKey, e.Depth, e.Limit)
}

// Is implements errors.Is for ExpansionError.
// It matches ErrExpansionDepth for depth/cycle violations (ExpansionDepthKind)
// but not for required-variable errors (ExpansionRequiredKind), which are a
// distinct failure mode. This makes the previously orphaned ErrExpansionDepth
// sentinel usable via errors.Is.
func (e *ExpansionError) Is(target error) bool {
	return target == ErrExpansionDepth && e.Kind != ExpansionRequiredKind
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
