// Package env — Component Factory.
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
	cachedExpander  VariableExpander
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

// lineParserValidator returns the validator as internal.LineKeyValidator interface.
func (f *ComponentFactory) lineParserValidator() internal.LineKeyValidator {
	return f.validator
}

// lineParserAuditor returns the auditor as internal.LineAuditLogger interface.
func (f *ComponentFactory) lineParserAuditor() internal.LineAuditLogger {
	return f.auditor
}

// lineParserExpander returns the expander as internal.LineExpander interface.
func (f *ComponentFactory) lineParserExpander() internal.LineExpander {
	return f.expander
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

// buildComponentFactory creates a new ComponentFactory from the configuration.
// It uses the default OS file system for environment variable lookup.
func (c *Config) buildComponentFactory() *ComponentFactory {
	return c.buildComponentFactoryWithFS(DefaultFileSystem)
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

	lookup := func(key string) (string, bool) {
		return fs.LookupEnv(key)
	}

	// Start with pre-computed default forbidden keys
	forbiddenKeys := make([]string, 0, len(defaultForbiddenKeysSlice)+len(c.ForbiddenKeys))
	forbiddenKeys = append(forbiddenKeys, defaultForbiddenKeysSlice...)
	// Add custom forbidden keys
	forbiddenKeys = append(forbiddenKeys, c.ForbiddenKeys...)

	factory := &ComponentFactory{}

	// Use custom validator if provided, otherwise create default
	if c.CustomValidator != nil {
		// Since KeyValidator = types.KeyValidator = internal.LineKeyValidator,
		// we can directly use the custom validator
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
		// Since AuditLogger = types.AuditLogger = internal.LineAuditLogger,
		// we can directly use the custom auditor
		factory.auditor = c.CustomAuditor
	} else {
		factory.auditor = internal.NewAuditor(handler, IsSensitiveKey, MaskValue, c.AuditEnabled)
	}

	// Use custom expander if provided, otherwise create default
	if c.CustomExpander != nil {
		// Since VariableExpander = types.VariableExpander = internal.LineExpander,
		// we can directly use the custom expander
		factory.expander = c.CustomExpander
	} else {
		factory.expander = internal.NewExpander(internal.ExpanderConfig{
			MaxDepth:   c.MaxExpansionDepth,
			Lookup:     lookup,
			Mode:       internal.ModeAll,
			KeyPattern: c.KeyPattern,
		})
	}

	// Pre-compute public-facing adapters once
	factory.cachedValidator = factory.buildValidatorAdapter()
	factory.cachedAuditor = factory.buildAuditorAdapter()
	factory.cachedExpander = factory.expander

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

// buildAuditorAdapter creates the public FullAuditLogger adapter from the internal auditor.
func (f *ComponentFactory) buildAuditorAdapter() FullAuditLogger {
	switch a := f.auditor.(type) {
	case *internal.Auditor:
		return newAuditorAdapter(a)
	case FullAuditLogger:
		return a
	default:
		return &auditorInterfaceWrapper{a}
	}
}
