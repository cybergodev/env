// Component factory for the env package.
//
// This file implements ComponentFactory which creates and manages shared components
// used by Loader and Parser. It provides a clean lifecycle for validator, auditor,
// and expander instances.
//
// The factory uses adapters (defined in adapters.go) to bridge between public interfaces
// and internal interfaces, allowing both built-in and custom components to work seamlessly.
package env

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/cybergodev/env/internal"
)

// ComponentFactory creates and manages shared components used by Loader and Parser.
// It provides a clean lifecycle for validator, auditor, and expander instances.
// ComponentFactory is safe for concurrent use.
type ComponentFactory struct {
	// Store interfaces; concrete type detection via type assertion when needed
	validator internal.LineKeyValidator
	auditor   internal.LineAuditLogger
	expander  internal.LineExpander
	closed    atomic.Bool
	mu        sync.Mutex // Protects Close()

	// Cached public-facing adapters (set once during construction)
	cachedValidator Validator
	cachedAuditor   FullAuditLogger
}

// Compile-time check that ComponentFactory implements io.Closer.
var _ io.Closer = (*ComponentFactory)(nil)

// Validator returns the validator component as a Validator interface.
func (f *ComponentFactory) Validator() Validator {
	if f == nil {
		return nil
	}
	return f.cachedValidator
}

// Auditor returns the audit logger component as FullAuditLogger.
func (f *ComponentFactory) Auditor() FullAuditLogger {
	if f == nil {
		return nil
	}
	return f.cachedAuditor
}

// Close releases resources held by the factory.
// After calling Close, the factory should not be used.
// Safe to call multiple times; subsequent calls return nil.
// This method is safe for concurrent use.
func (f *ComponentFactory) Close() error {
	if f == nil {
		return nil
	}
	// Use CompareAndSwap for atomic transition from open to closed state.
	if !f.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Try to close if it implements io.Closer
	if c, ok := f.auditor.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// IsClosed returns true if the factory has been closed.
// This method is safe for concurrent use.
func (f *ComponentFactory) IsClosed() bool {
	if f == nil {
		return true
	}
	return f.closed.Load()
}

// Expander returns the expander as VariableExpander interface.
func (f *ComponentFactory) Expander() VariableExpander {
	if f == nil {
		return nil
	}
	// VariableExpander and internal.LineExpander are now the same type
	// via type aliases, so direct return works
	return f.expander
}

// buildComponentFactoryWithFS creates a new ComponentFactory from the configuration
// using the provided FileSystem for environment variable lookup.
// If fs is nil, DefaultFileSystem is used.
// If custom components are provided in Config, they will be used instead of built-in ones.
func (c *Config) buildComponentFactoryWithFS(fs FileSystem) *ComponentFactory {
	// Use default file system if not provided
	if fs == nil {
		fs = DefaultFileSystem
	}

	handler := c.AuditHandler
	if handler == nil {
		handler = internal.DefaultHandler()
	}

	lookup := fs.LookupEnv
	// SECURITY (SEC-03): in file-only scope the expander must not fall back
	// to the process environment — a less-trusted config file could
	// otherwise capture unrelated process secrets (${AWS_SECRET_...}) into
	// values that are later logged or persisted. A nil Lookup makes
	// NewExpander substitute an always-unset lookup; ExpandAllInMap still
	// resolves file-local variables from the parsed map before consulting it.
	if c.ExpansionScope == ExpansionFileOnly {
		lookup = nil
	}

	// Start with pre-computed default forbidden keys
	forbiddenKeys := make([]string, 0, len(defaultForbiddenKeysSlice)+len(c.ForbiddenKeys))
	forbiddenKeys = append(forbiddenKeys, defaultForbiddenKeysSlice...)
	// Add custom forbidden keys
	forbiddenKeys = append(forbiddenKeys, c.ForbiddenKeys...)

	factory := &ComponentFactory{}

	// Use custom validator if provided, otherwise create default
	if c.CustomValidator != nil {
		// KeyValidator and internal.LineKeyValidator are the same type via
		// aliases, so the custom validator is assigned directly.
		factory.validator = c.CustomValidator
	} else {
		factory.validator = internal.NewValidator(internal.ValidatorConfig{
			KeyPattern:     c.KeyPattern,
			AllowedKeys:    c.AllowedKeys,
			ForbiddenKeys:  forbiddenKeys,
			RequiredKeys:   c.RequiredKeys,
			MaxKeyLength:   c.MaxKeyLength,
			MaxValueLength: c.MaxValueLength,
			ValidateUTF8:   c.ValidateUTF8,
			IsSensitive:    IsSensitiveKey,
			MaskKey:        MaskKey,
			MaskSensitive:  MaskSensitiveInString,
		})
	}

	// Use custom auditor if provided, otherwise create default
	if c.CustomAuditor != nil {
		// AuditLogger and internal.LineAuditLogger are the same type via
		// aliases, so the custom auditor is assigned directly.
		factory.auditor = c.CustomAuditor
	} else {
		auditor := internal.NewAuditor(handler, IsSensitiveKey, MaskValue, c.AuditEnabled)
		// GEN-001 fail-closed auditing: arm the sticky failure latch so the
		// Loader can surface lost events at the end of top-level operations.
		auditor.SetFailClosed(c.AuditFailClosed)
		factory.auditor = auditor
	}

	// Use custom expander if provided, otherwise create default
	if c.CustomExpander != nil {
		// VariableExpander and internal.LineExpander are the same type via
		// aliases, so the custom expander is assigned directly.
		factory.expander = c.CustomExpander
	} else {
		factory.expander = internal.NewExpander(internal.ExpanderConfig{
			MaxDepth:   c.MaxExpansionDepth,
			Lookup:     lookup,
			Mode:       internal.ModeAll,
			KeyPattern: c.KeyPattern,
			// Bound expanded output to the configured value limit — raw
			// values are checked at parse time, but multi-reference
			// expansion can otherwise amplify far past it (GEN-001).
			MaxValueLength: c.MaxValueLength,
		})
	}

	// Pre-compute public-facing adapters once
	factory.cachedValidator = factory.buildValidatorAdapter()
	factory.cachedAuditor = factory.buildAuditorAdapter()

	return factory
}

// buildValidatorAdapter creates the public Validator adapter from the internal validator.
func (f *ComponentFactory) buildValidatorAdapter() Validator {
	switch v := f.validator.(type) {
	case Validator:
		return v
	default:
		return &validatorInterfaceWrapper{v}
	}
}

// buildAuditorAdapter creates the public FullAuditLogger view from the
// internal auditor. The built-in *internal.Auditor satisfies FullAuditLogger
// directly (AuditAction is an alias of internal.Action), so it is returned
// unwrapped; only minimal LogError-only auditors get wrapped.
func (f *ComponentFactory) buildAuditorAdapter() FullAuditLogger {
	switch a := f.auditor.(type) {
	case FullAuditLogger:
		return a
	default:
		return &auditorInterfaceWrapper{a}
	}
}
