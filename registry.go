package env

import (
	"fmt"
	"io"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// ParserFactory creates an EnvParser from a Config and ComponentFactory.
// Custom parser implementations should implement this function signature
// to register their parsers with the library.
type ParserFactory func(cfg Config, factory *ComponentFactory) (EnvParser, error)

// parserRegistry holds registered parser factories.
type parserRegistry struct {
	mu        sync.RWMutex
	factories map[FileFormat]ParserFactory
	// hasCustom reports whether anything beyond the built-in parsers was
	// ever registered (including built-in overrides via ForceRegisterParser).
	// It is set under mu and only ever transitions to true, so a load is a
	// conservative snapshot: createParsers may take the slow path
	// unnecessarily, but never misses a registered custom parser.
	hasCustom atomic.Bool
}

// globalParserRegistry is the global registry for parser factories.
var globalParserRegistry = &parserRegistry{
	factories: make(map[FileFormat]ParserFactory),
}

// RegisterParser registers a custom parser factory for a specific file format.
// Returns an error if a factory is already registered for the format.
// Built-in formats (DotEnv, JSON, YAML) cannot be overridden for security.
//
// This is a process-global runtime knob rather than a Config setting: it
// mutates the package-level parser registry and affects every Loader created
// afterwards. A Loader snapshots the registry at construction, so register
// parsers during program initialization, before creating Loaders.
//
// Example:
//
//	err := env.RegisterParser(env.FormatEnv, func(cfg env.Config, factory *env.ComponentFactory) (env.EnvParser, error) {
//	    return &MyCustomParser{validator: factory.Validator()}, nil
//	})
func RegisterParser(format FileFormat, factory ParserFactory) error {
	globalParserRegistry.mu.Lock()
	defer globalParserRegistry.mu.Unlock()

	// Prevent overriding built-in parsers for security
	if format == FormatEnv || format == FormatJSON || format == FormatYAML {
		return fmt.Errorf("cannot override built-in parser for format: %s", format.String())
	}

	if factory == nil {
		return fmt.Errorf("factory cannot be nil for format: %s", format.String())
	}

	if _, exists := globalParserRegistry.factories[format]; exists {
		return fmt.Errorf("parser already registered for format: %s", format.String())
	}

	globalParserRegistry.hasCustom.Store(true)
	globalParserRegistry.factories[format] = factory
	return nil
}

// ForceRegisterParser registers a parser factory, allowing override of
// built-in parsers (FormatEnv, FormatJSON, FormatYAML).
//
// WARNING: Use with caution. Overriding built-in parsers may introduce
// security vulnerabilities if the replacement parser doesn't implement
// the same security checks (key validation, value validation, size limits, etc.).
// Ensure your custom parser properly validates all input.
//
// This is intended for advanced use cases where you need complete control
// over parsing behavior, such as:
//   - Adding custom security checks to the built-in parser
//   - Implementing format extensions (e.g., HEREDOC support, multi-line values)
//   - Testing with mock parsers
//
// Like RegisterParser, this is a process-global runtime knob rather than a
// Config setting: it mutates the package-level parser registry and affects
// every Loader created afterwards (a Loader snapshots the registry at
// construction).
//
// Example:
//
//	// Override the default .env parser with a custom implementation
//	err := env.ForceRegisterParser(env.FormatEnv, func(cfg env.Config, factory *env.ComponentFactory) (env.EnvParser, error) {
//	    return &MyCustomEnvParser{
//	        validator: factory.Validator(),
//	        auditor:   factory.Auditor(),
//	    }, nil
//	})
func ForceRegisterParser(format FileFormat, factory ParserFactory) error {
	globalParserRegistry.mu.Lock()
	defer globalParserRegistry.mu.Unlock()

	if factory == nil {
		return fmt.Errorf("factory cannot be nil for format: %s", format.String())
	}

	// Security note: overriding built-in parsers may bypass validation.
	// The caller is responsible for ensuring equivalent security checks.
	// No warning printed — libraries should not write to stdout.

	globalParserRegistry.hasCustom.Store(true)
	globalParserRegistry.factories[format] = factory
	return nil
}

// registerBuiltin registers a built-in parser factory without lock checking.
// This is used internally during init() to register default parsers.
func (r *parserRegistry) registerBuiltin(format FileFormat, factory ParserFactory) {
	r.factories[format] = factory
}

// createParsers creates all registered parsers for the given configuration.
// Returns a map of parsers keyed by file format.
//
// Fast path: when nothing beyond the built-in parsers has been registered
// (the overwhelming default), the built-in factories are invoked directly —
// no registry lock and no snapshot map copy per Loader construction. Any
// RegisterParser/ForceRegisterParser call flips hasCustom and construction
// takes the general path from then on.
//
// The general path snapshots the registry under the read lock, but the
// factory callbacks are invoked only AFTER releasing it. ParserFactory is
// user-supplied code: calling it while holding globalParserRegistry.mu.RLock
// would deadlock if a factory (transitively) calls
// RegisterParser/ForceRegisterParser — RWMutex is not reentrant, so the
// write-lock acquisition inside would block on the read lock held by this
// same goroutine. It would also serialize unrelated Loader constructions
// behind potentially slow factory code.
//
// SEC-003: in the general path every produced parser is wrapped for panic
// containment (see recovering.go) — invokeParserFactory already contains a
// panicking FACTORY; the wrapper extends that to a panicking Parse. The fast
// path (nothing custom ever registered) leaves the built-in parsers
// unwrapped: they are trusted library code, and the fast path exists to keep
// default Loader construction allocation-free.
func createParsers(cfg Config, factory *ComponentFactory) (map[FileFormat]EnvParser, error) {
	snapshot := builtinParsers
	customRegistered := globalParserRegistry.hasCustom.Load()
	if customRegistered {
		globalParserRegistry.mu.RLock()
		snapshot = make(map[FileFormat]ParserFactory, len(globalParserRegistry.factories))
		for format, parserFactory := range globalParserRegistry.factories {
			snapshot[format] = parserFactory
		}
		globalParserRegistry.mu.RUnlock()
	}

	parsers := make(map[FileFormat]EnvParser, len(snapshot))
	for format, parserFactory := range snapshot {
		parser, err := invokeParserFactory(format, parserFactory, cfg, factory)
		if err != nil {
			// Clean up already created parsers to prevent resource leak
			// Use type assertion since EnvParser interface doesn't include Close
			for _, p := range parsers {
				if closer, ok := p.(io.Closer); ok {
					_ = closer.Close()
				}
			}
			return nil, fmt.Errorf("failed to create %s parser: %w", format.String(), err)
		}
		if customRegistered {
			parser = newRecoveringParser(parser)
		}
		parsers[format] = parser
	}

	return parsers, nil
}

// invokeParserFactory calls a user-supplied ParserFactory with panic
// protection, mirroring the auditor's safeHandlerLog/safeHandlerClose: a
// factory that panics surfaces as a creation error from New() instead of
// crashing the process during initialization. The stack is included to make
// the user-code fault debuggable.
func invokeParserFactory(format FileFormat, factoryFn ParserFactory, cfg Config, factory *ComponentFactory) (parser EnvParser, err error) {
	defer func() {
		if r := recover(); r != nil {
			parser, err = nil, fmt.Errorf("parser factory for %s panicked: %v\n%s", format.String(), r, debug.Stack())
		}
	}()
	return factoryFn(cfg, factory)
}

// builtinParsers is the built-in parser factory map, keyed by format. The
// map and its closures are stateless, so exactly one instance is built at
// package init and shared by every Loader construction — createParsers'
// fast path previously allocated a fresh map plus three closures per New().
//
// MUST NOT be mutated after init: it is read concurrently by createParsers.
var builtinParsers = map[FileFormat]ParserFactory{
	FormatEnv: func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return newParserWithFactory(cfg, factory)
	},
	FormatJSON: func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return newJSONParserWithFactory(cfg, factory)
	},
	FormatYAML: func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return newYAMLParserWithFactory(cfg, factory)
	},
}

// init registers the built-in parsers.
// Built-in parsers are registered using registerBuiltin to bypass
// the security check in RegisterParser that prevents overriding built-in formats.
func init() {
	for format, parserFactory := range builtinParsers {
		globalParserRegistry.registerBuiltin(format, parserFactory)
	}
}
