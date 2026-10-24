// Panic containment for config-injected components (SEC-003).
//
// The library's established contract — see internal.Auditor.safeHandlerLog and
// invokeParserFactory — is that user-supplied code invoked by the library must
// not crash the process: a panic surfaces as an error from the public API that
// triggered it, with the stack included so the user-code fault stays
// debuggable. The built-in components always had this containment on their
// user-facing seams (audit handlers, parser factories, the isSensitive/masker
// callbacks); custom components plugged in through ComponentConfig did not; a
// panicking CustomValidator crashed Set/Load, a panicking CustomAuditor crashed
// every audited operation, and so on.
//
// The wrappers below extend the same containment to every config-injected
// component, with zero overhead for default (built-in) configurations: a
// wrapper is installed only when the corresponding Config field is non-nil.
//
// Capability preservation: several call sites probe components for optional
// interfaces (validateSetKV, needsRequiredCheck, validateResult,
// auditCheckFailClosed, the io.Closer cleanups). Each wrapper implements those
// optionals; when the wrapped component lacks one, the method reproduces
// exactly what the failed probe produced before (documented per method), so
// wrapping is behavior-identical for components that do not panic.
package env

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"
)

// panicError renders a recovered panic as an error, mirroring the format of
// internal.safeHandlerLog and invokeParserFactory: component name, panic
// value, and stack.
func panicError(component string, r any) error {
	return fmt.Errorf("%s panicked: %v\n%s", component, r, debug.Stack())
}

// ============================================================================
// Custom Validator
// ============================================================================

// Compile-time capability checks: the wrapper must satisfy every interface the
// library probes on a validator, so wrapping never changes which code path a
// non-panicking custom validator takes.
var (
	_ Validator           = (*recoveringValidator)(nil)
	_ setKeyValidator     = (*recoveringValidator)(nil)
	_ basicValueValidator = (*recoveringValidator)(nil)
	_ hasRequiredKeys     = (*recoveringValidator)(nil)
	_ keyPolicyValidator  = (*recoveringValidator)(nil)
)

// recoveringValidator wraps a config-injected CustomValidator so a panic in
// any of its methods surfaces as a validation error instead of crashing the
// process.
type recoveringValidator struct {
	Validator
	// Optional capabilities of the wrapped validator, resolved once at wrap
	// time. A nil field means the wrapped validator lacks the capability and
	// the corresponding wrapper method takes the documented fallback.
	setKey   setKeyValidator
	basicVal basicValueValidator
	reqKeys  hasRequiredKeys
	policy   keyPolicyValidator
}

// newRecoveringValidator wraps v, snapshotting its optional capabilities.
func newRecoveringValidator(v Validator) *recoveringValidator {
	rv := &recoveringValidator{Validator: v}
	if s, ok := v.(setKeyValidator); ok {
		rv.setKey = s
	}
	if b, ok := v.(basicValueValidator); ok {
		rv.basicVal = b
	}
	if h, ok := v.(hasRequiredKeys); ok {
		rv.reqKeys = h
	}
	if p, ok := v.(keyPolicyValidator); ok {
		rv.policy = p
	}
	return rv
}

// ValidateKey validates a key with panic containment.
func (w *recoveringValidator) ValidateKey(key string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom validator ValidateKey", r)
		}
	}()
	return w.Validator.ValidateKey(key)
}

// ValidateValue validates a value with panic containment.
func (w *recoveringValidator) ValidateValue(value string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom validator ValidateValue", r)
		}
	}()
	return w.Validator.ValidateValue(value)
}

// ValidateRequired checks required keys with panic containment.
func (w *recoveringValidator) ValidateRequired(keys map[string]bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom validator ValidateRequired", r)
		}
	}()
	return w.Validator.ValidateRequired(keys)
}

// ValidateSetKey validates a programmatically-set key with panic containment.
// Fallback when the wrapped validator lacks the capability: delegate to
// ValidateKey — exactly what validateSetKV's setKeyCap==nil branch did.
func (w *recoveringValidator) ValidateSetKey(key string) (err error) {
	if w.setKey == nil {
		return w.ValidateKey(key)
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom validator ValidateSetKey", r)
		}
	}()
	return w.setKey.ValidateSetKey(key)
}

// ValidateValueBasic applies the baseline value check with panic containment.
// Fallback when the wrapped validator lacks the capability: return nil,
// skipping the check — exactly what the failed basicValueValidator probe did.
func (w *recoveringValidator) ValidateValueBasic(value string) (err error) {
	if w.basicVal == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom validator ValidateValueBasic", r)
		}
	}()
	return w.basicVal.ValidateValueBasic(value)
}

// HasRequiredKeys reports whether required keys are configured. Fallback when
// the wrapped validator lacks the capability: return true, the conservative
// default needsRequiredCheck applied to capability-less validators. A panic in
// the capability method also yields true — there is no error channel on a bool
// probe, and "assume a check is needed" is the fail-closed answer.
func (w *recoveringValidator) HasRequiredKeys() (ok bool) {
	if w.reqKeys == nil {
		return true
	}
	defer func() {
		if r := recover(); r != nil {
			ok = true
		}
	}()
	return w.reqKeys.HasRequiredKeys()
}

// ValidateKeyPolicy checks the allowed/forbidden key policy with panic
// containment. Fallback when the wrapped validator lacks the capability:
// return nil, skipping the policy check — exactly what hasPolicy=false did.
func (w *recoveringValidator) ValidateKeyPolicy(key string) (err error) {
	if w.policy == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom validator ValidateKeyPolicy", r)
		}
	}()
	return w.policy.ValidateKeyPolicy(key)
}

// ============================================================================
// Custom Expander
// ============================================================================

// recoveringExpander wraps a config-injected CustomExpander so a panic in
// Expand surfaces as an expansion error instead of crashing the process.
type recoveringExpander struct {
	VariableExpander
}

// Expand expands a value with panic containment.
func (w *recoveringExpander) Expand(s string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", panicError("custom expander Expand", r)
		}
	}()
	return w.VariableExpander.Expand(s)
}

// ============================================================================
// Custom Auditor
// ============================================================================

// Compile-time checks: the wrapper satisfies FullAuditLogger (what
// buildAuditorAdapter probes) and the optional TakeFailure capability
// auditCheckFailClosed probes under AuditFailClosed.
var (
	_ FullAuditLogger                  = (*recoveringAuditLogger)(nil)
	_ io.Closer                        = (*recoveringAuditLogger)(nil)
	_ interface{ TakeFailure() error } = (*recoveringAuditLogger)(nil)
)

// recoveringAuditLogger wraps a config-injected CustomAuditor so a panic in
// any of its methods is contained instead of crashing the process. The
// built-in *internal.Auditor already recovers panicking handlers internally;
// custom auditors got no such protection, despite being exactly as
// user-supplied as the handlers are.
//
// Minimal auditors (LogError only) keep the synthesis auditorInterfaceWrapper
// provides: the wrapper holds one over its own recovered LogError and routes
// the extended methods through it when the custom auditor does not implement
// them itself, so event routing is unchanged.
type recoveringAuditLogger struct {
	inner  AuditLogger
	direct FullAuditLogger // non-nil: the custom implements FullAuditLogger; Log* delegates here
	synth  FullAuditLogger // non-nil: auditorInterfaceWrapper over this wrapper's LogError
	closer io.Closer
	taker  interface{ TakeFailure() error }
}

// newRecoveringAuditLogger wraps a, snapshotting its optional capabilities.
func newRecoveringAuditLogger(a AuditLogger) *recoveringAuditLogger {
	w := &recoveringAuditLogger{inner: a}
	if full, ok := a.(FullAuditLogger); ok {
		w.direct = full
	} else {
		w.synth = &auditorInterfaceWrapper{AuditLogger: w}
	}
	if c, ok := a.(io.Closer); ok {
		w.closer = c
	}
	if t, ok := a.(interface{ TakeFailure() error }); ok {
		w.taker = t
	}
	return w
}

// LogError records an error event with panic containment.
func (w *recoveringAuditLogger) LogError(action AuditAction, key, errMsg string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom auditor LogError", r)
		}
	}()
	return w.inner.LogError(action, key, errMsg)
}

// Log records an audit event: direct delegation with containment for full
// auditors, the synthesized LogError routing for minimal ones (whose Log
// routing already passed through LogError before the wrap).
func (w *recoveringAuditLogger) Log(action AuditAction, key, reason string, success bool) (err error) {
	if w.direct == nil {
		return w.synth.Log(action, key, reason, success) // routes through w.LogError (contained)
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom auditor Log", r)
		}
	}()
	return w.direct.Log(action, key, reason, success)
}

// LogWithFile records an audit event with file information; see Log.
func (w *recoveringAuditLogger) LogWithFile(action AuditAction, key, file, reason string, success bool) (err error) {
	if w.direct == nil {
		return w.synth.LogWithFile(action, key, file, reason, success)
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom auditor LogWithFile", r)
		}
	}()
	return w.direct.LogWithFile(action, key, file, reason, success)
}

// LogWithDuration records an audit event with timing; see Log.
func (w *recoveringAuditLogger) LogWithDuration(action AuditAction, key, reason string, success bool, duration time.Duration) (err error) {
	if w.direct == nil {
		return w.synth.LogWithDuration(action, key, reason, success, duration)
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom auditor LogWithDuration", r)
		}
	}()
	return w.direct.LogWithDuration(action, key, reason, success, duration)
}

// Close closes the custom auditor with containment. A custom that does not
// implement io.Closer returns nil, matching the previous failed probe.
func (w *recoveringAuditLogger) Close() (err error) {
	if w.closer == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom auditor Close", r)
		}
	}()
	return w.closer.Close()
}

// TakeFailure reports the latched failure for AuditFailClosed. Fallback when
// the custom auditor lacks the capability: nil, matching the previous failed
// probe (auditCheckFailClosed then skips the check).
func (w *recoveringAuditLogger) TakeFailure() (err error) {
	if w.taker == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom auditor TakeFailure", r)
		}
	}()
	return w.taker.TakeFailure()
}

// ============================================================================
// Custom FileSystem
// ============================================================================

// Compile-time checks for the full FileSystem/File surfaces.
var (
	_ FileSystem = (*recoveringFileSystem)(nil)
	_ File       = (*recoveringFile)(nil)
)

// recoveringFileSystem wraps a config-injected FileSystem so a panic in any
// method the library calls surfaces as an error (or, for the boolean
// LookupEnv, as "unset" — see its doc) instead of crashing the process.
// Methods the library never calls are recovered too so the whole injected
// surface is uniform.
type recoveringFileSystem struct {
	FileSystem
}

// resolveComponentFileSystem returns fs for library use: a nil fs falls back
// to DefaultFileSystem, and the result is ALWAYS wrapped for panic
// containment — including DefaultFileSystem itself, which users may replace
// with their own implementation at init (the documented knob contract), and
// a custom implementation panicking there must not crash Load either. The
// wrapper adds one interface indirection per call; every operation it guards
// (Open, LookupEnv, Setenv, ...) is syscall-adjacent, so the overhead is
// negligible relative to the call it wraps.
func resolveComponentFileSystem(fs FileSystem) FileSystem {
	if fs == nil {
		fs = DefaultFileSystem
	}
	return &recoveringFileSystem{FileSystem: fs}
}

// Open opens a file with panic containment; the returned File is wrapped so
// panics from its methods are contained as well.
func (w *recoveringFileSystem) Open(name string) (file File, err error) {
	defer func() {
		if r := recover(); r != nil {
			file, err = nil, panicError("custom file system Open", r)
		}
	}()
	f, err := w.FileSystem.Open(name)
	if err == nil && f != nil {
		f = &recoveringFile{File: f}
	}
	return f, err
}

// LookupEnv looks up a variable with panic containment. It has no error
// channel, so a panic converts to ("", false) — treated as "not set", the
// same fail-soft answer a concurrent miss produces. A file system that
// panics inside LookupEnv is broken regardless; the library prefers not to
// take the process down with it.
func (w *recoveringFileSystem) LookupEnv(key string) (value string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			value, ok = "", false
		}
	}()
	return w.FileSystem.LookupEnv(key)
}

// Setenv sets a variable with panic containment.
func (w *recoveringFileSystem) Setenv(key, value string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom file system Setenv", r)
		}
	}()
	return w.FileSystem.Setenv(key, value)
}

// Unsetenv unsets a variable with panic containment.
func (w *recoveringFileSystem) Unsetenv(key string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom file system Unsetenv", r)
		}
	}()
	return w.FileSystem.Unsetenv(key)
}

// Getenv retrieves a variable with panic containment (an empty string doubles
// as the miss answer, mirroring LookupEnv).
func (w *recoveringFileSystem) Getenv(key string) (value string) {
	defer func() {
		if r := recover(); r != nil {
			value = ""
		}
	}()
	return w.FileSystem.Getenv(key)
}

// OpenFile opens a file with flags with panic containment.
func (w *recoveringFileSystem) OpenFile(name string, flag int, perm os.FileMode) (file File, err error) {
	defer func() {
		if r := recover(); r != nil {
			file, err = nil, panicError("custom file system OpenFile", r)
		}
	}()
	f, err := w.FileSystem.OpenFile(name, flag, perm)
	if err == nil && f != nil {
		f = &recoveringFile{File: f}
	}
	return f, err
}

// Stat stats a path with panic containment.
func (w *recoveringFileSystem) Stat(name string) (info os.FileInfo, err error) {
	defer func() {
		if r := recover(); r != nil {
			info, err = nil, panicError("custom file system Stat", r)
		}
	}()
	return w.FileSystem.Stat(name)
}

// MkdirAll creates directories with panic containment.
func (w *recoveringFileSystem) MkdirAll(path string, perm os.FileMode) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom file system MkdirAll", r)
		}
	}()
	return w.FileSystem.MkdirAll(path, perm)
}

// Remove removes a file with panic containment.
func (w *recoveringFileSystem) Remove(name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom file system Remove", r)
		}
	}()
	return w.FileSystem.Remove(name)
}

// Rename renames a file with panic containment.
func (w *recoveringFileSystem) Rename(oldpath, newpath string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom file system Rename", r)
		}
	}()
	return w.FileSystem.Rename(oldpath, newpath)
}

// recoveringFile wraps a File returned by a custom file system so panics
// from its methods surface as errors instead of crashing the process.
type recoveringFile struct {
	File
}

// Read reads with panic containment.
func (w *recoveringFile) Read(p []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			n, err = 0, panicError("custom file Read", r)
		}
	}()
	return w.File.Read(p)
}

// Write writes with panic containment.
func (w *recoveringFile) Write(p []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			n, err = 0, panicError("custom file Write", r)
		}
	}()
	return w.File.Write(p)
}

// Close closes with panic containment.
func (w *recoveringFile) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom file Close", r)
		}
	}()
	return w.File.Close()
}

// Stat stats with panic containment.
func (w *recoveringFile) Stat() (info os.FileInfo, err error) {
	defer func() {
		if r := recover(); r != nil {
			info, err = nil, panicError("custom file Stat", r)
		}
	}()
	return w.File.Stat()
}

// Sync syncs with panic containment.
func (w *recoveringFile) Sync() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("custom file Sync", r)
		}
	}()
	return w.File.Sync()
}

// ============================================================================
// Custom Parser
// ============================================================================

var (
	_ EnvParser = (*recoveringParser)(nil)
	_ io.Closer = (*recoveringParser)(nil)
)

// recoveringParser wraps a parser produced by a registered (custom) parser
// factory so a panic in its Parse surfaces as a parse error instead of
// crashing the process — the Parse-time counterpart of invokeParserFactory's
// construction-time containment. The io.Closer cleanup paths in Close and
// createParsers probe for Close; the wrapper keeps that working.
type recoveringParser struct {
	EnvParser
	closer io.Closer // non-nil when the wrapped parser implements io.Closer
}

// newRecoveringParser wraps p, snapshotting its io.Closer capability.
func newRecoveringParser(p EnvParser) *recoveringParser {
	w := &recoveringParser{EnvParser: p}
	if c, ok := p.(io.Closer); ok {
		w.closer = c
	}
	return w
}

// Parse parses a document with panic containment.
func (w *recoveringParser) Parse(r io.Reader, filename string) (vars map[string]string, err error) {
	defer func() {
		if r := recover(); r != nil {
			vars, err = nil, panicError("parser Parse", r)
		}
	}()
	return w.EnvParser.Parse(r, filename)
}

// Close closes the wrapped parser with containment. A parser that does not
// implement io.Closer returns nil, matching the previous failed probe.
func (w *recoveringParser) Close() (err error) {
	if w.closer == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = panicError("parser Close", r)
		}
	}()
	return w.closer.Close()
}

// ============================================================================
// Struct-field / target (un)marshaler callbacks
// ============================================================================

// safeUnmarshalEnv invokes a user-supplied Unmarshaler with panic containment,
// mirroring the component wrappers: the callback runs on the caller's own
// type, reached reflectively through UnmarshalInto.
func safeUnmarshalEnv(u Unmarshaler, data map[string]string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError("UnmarshalEnv callback", r)
		}
	}()
	return u.UnmarshalEnv(data)
}
