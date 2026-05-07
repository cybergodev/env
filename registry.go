package env

import (
	"fmt"
	"io"
	"sync"
)

// ParserFactory creates an EnvParser from a Config and ComponentFactory.
// Custom parser implementations should implement this function signature
// to register their parsers with the library.
type ParserFactory func(cfg Config, factory *ComponentFactory) (EnvParser, error)

// parserRegistry holds registered parser factories.
type parserRegistry struct {
	mu        sync.RWMutex
	factories map[FileFormat]ParserFactory
}

// globalParserRegistry is the global registry for parser factories.
var globalParserRegistry = &parserRegistry{
	factories: make(map[FileFormat]ParserFactory),
}

// RegisterParser registers a custom parser factory for a specific file format.
// Returns an error if a factory is already registered for the format.
// Built-in formats (DotEnv, JSON, YAML) cannot be overridden for security.
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

	if _, exists := globalParserRegistry.factories[format]; exists {
		return fmt.Errorf("parser already registered for format: %s", format.String())
	}

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

	// Warn when replacing a built-in parser — this has security implications
	if format == FormatEnv || format == FormatJSON || format == FormatYAML {
		fmt.Printf("WARNING: ForceRegisterParser is overriding built-in parser for %s. "+
			"Ensure the replacement implements the same security checks "+
			"(key validation, value validation, size limits, forbidden keys).\n", format.String())
	}

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
func createParsers(cfg Config, factory *ComponentFactory) (map[FileFormat]EnvParser, error) {
	globalParserRegistry.mu.RLock()
	defer globalParserRegistry.mu.RUnlock()

	parsers := make(map[FileFormat]EnvParser, len(globalParserRegistry.factories))

	for format, parserFactory := range globalParserRegistry.factories {
		parser, err := parserFactory(cfg, factory)
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
		parsers[format] = parser
	}

	return parsers, nil
}

// init registers the built-in parsers.
// Built-in parsers are registered using registerBuiltin to bypass
// the security check in RegisterParser that prevents overriding built-in formats.
func init() {
	// Register dot-env parser
	globalParserRegistry.registerBuiltin(FormatEnv, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return newParserWithFactory(cfg, factory)
	})

	// Register JSON parser
	globalParserRegistry.registerBuiltin(FormatJSON, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return newJSONParserWithFactory(cfg, factory)
	})

	// Register YAML parser
	globalParserRegistry.registerBuiltin(FormatYAML, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return newYAMLParserWithFactory(cfg, factory)
	})
}
