// Adapter pattern implementation for the env package.
//
// This file implements adapters for internal types that need to be exposed
// via public interfaces with extended functionality.
//
// Interface hierarchy (simplified):
//   - env.KeyValidator = internal.KeyValidator
//   - env.ValueValidator = internal.ValueValidator
//   - env.VariableExpander = internal.VariableExpander
//   - env.AuditLogger = internal.AuditLogger
//
// The remaining adapters handle cases where the public interface has more
// methods than the minimal internal interface:
//   - auditorAdapter: internal.Auditor → FullAuditLogger
//   - validatorInterfaceWrapper: minimal validator → Validator (with ValidateRequired)
//   - auditorInterfaceWrapper: minimal auditor → FullAuditLogger
package env

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cybergodev/env/internal"
)

// ErrValidateRequiredUnsupported is returned when a custom validator
// does not implement the ValidateRequired method. Users who need required
// key validation should implement the full Validator interface.
var ErrValidateRequiredUnsupported = errors.New(
	"custom validator does not implement ValidateRequired; " +
		"implement Validator interface for required key validation",
)

// ============================================================================
// Internal → Public Adapters (for extended interfaces)
// ============================================================================

// auditorAdapter adapts internal.Auditor to the public FullAuditLogger interface.
// This adapter is used when the built-in internal.Auditor needs to be exposed
// to users via the public API (e.g., Loader.Auditor()).
type auditorAdapter struct {
	auditor *internal.Auditor
}

// Compile-time check that auditorAdapter implements FullAuditLogger.
var _ FullAuditLogger = (*auditorAdapter)(nil)

// newAuditorAdapter creates a new FullAuditLogger from an internal.Auditor.
func newAuditorAdapter(a *internal.Auditor) FullAuditLogger {
	if a == nil {
		return nil
	}
	return &auditorAdapter{auditor: a}
}

// Log implements AuditLogger.
func (a *auditorAdapter) Log(action AuditAction, key, reason string, success bool) error {
	return a.auditor.Log(internal.Action(action), key, reason, success)
}

// LogError implements AuditLogger.
func (a *auditorAdapter) LogError(action AuditAction, key, errMsg string) error {
	return a.auditor.LogError(internal.Action(action), key, errMsg)
}

// LogWithFile implements AuditLogger.
func (a *auditorAdapter) LogWithFile(action AuditAction, key, file, reason string, success bool) error {
	return a.auditor.LogWithFile(internal.Action(action), key, file, reason, success)
}

// LogWithDuration implements AuditLogger.
func (a *auditorAdapter) LogWithDuration(action AuditAction, key, reason string, success bool, duration time.Duration) error {
	return a.auditor.LogWithDuration(internal.Action(action), key, reason, success, duration)
}

// Close implements AuditLogger.
func (a *auditorAdapter) Close() error {
	if a == nil || a.auditor == nil {
		return nil
	}
	return a.auditor.Close()
}

// validatorInterfaceWrapper adapts a minimal KeyValidator to the full Validator interface.
// This adapter is used when a custom validator only implements KeyValidator
// and needs to satisfy the full Validator interface.
type validatorInterfaceWrapper struct {
	KeyValidator
}

// ValidateValue returns nil by default for wrapped validators.
func (w *validatorInterfaceWrapper) ValidateValue(value string) error {
	if vv, ok := w.KeyValidator.(ValueValidator); ok {
		return vv.ValidateValue(value)
	}
	return nil
}

// ValidateRequired returns ErrValidateRequiredUnsupported for minimal validators.
func (w *validatorInterfaceWrapper) ValidateRequired(keys map[string]bool) error {
	return ErrValidateRequiredUnsupported
}

// auditorInterfaceWrapper adapts a minimal AuditLogger to the full FullAuditLogger interface.
// This adapter is used when a custom auditor only implements AuditLogger
// and needs to satisfy the full FullAuditLogger interface.
//
// Design Note: The minimal AuditLogger interface only provides LogError, so this adapter
// routes all log entries (both success and failure) through LogError. The status prefix
// [ok] or [error] distinguishes the outcome. For full audit capabilities with proper
// success/error distinction, implement FullAuditLogger directly instead of relying on
// this adapter.
type auditorInterfaceWrapper struct {
	AuditLogger
}

// Compile-time check that auditorInterfaceWrapper implements FullAuditLogger.
var _ FullAuditLogger = (*auditorInterfaceWrapper)(nil)

func (w *auditorInterfaceWrapper) Log(action AuditAction, key, reason string, success bool) error {
	prefix := "[error] "
	if success {
		prefix = "[ok] "
	}
	return w.AuditLogger.LogError(action, key, prefix+reason)
}

func (w *auditorInterfaceWrapper) LogWithFile(action AuditAction, key, file, reason string, success bool) error {
	prefix := "[error] "
	if success {
		prefix = "[ok] "
	}
	return w.AuditLogger.LogError(action, key, prefix+reason+" (file: "+file+")")
}

func (w *auditorInterfaceWrapper) LogWithDuration(action AuditAction, key, reason string, success bool, duration time.Duration) error {
	// Duration formatting requires fmt.Sprintf, but we minimize other allocations
	prefix := "[error] "
	if success {
		prefix = "[ok] "
	}
	return w.AuditLogger.LogError(action, key, fmt.Sprintf("%s%s (duration: %v)", prefix, reason, duration))
}

func (w *auditorInterfaceWrapper) Close() error {
	if c, ok := w.AuditLogger.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
