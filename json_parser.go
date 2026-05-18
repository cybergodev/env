package env

import (
	"errors"
	"io"
	"time"

	"github.com/cybergodev/env/internal"
)

// jsonParser handles parsing of JSON configuration files.
type jsonParser struct {
	config    Config
	validator Validator
	auditor   FullAuditLogger
	flatten   internal.JSONFlattenConfig
}

// Compile-time check that jsonParser implements EnvParser.
var _ EnvParser = (*jsonParser)(nil)

// newJSONParserWithFactory creates a new jsonParser with a ComponentFactory.
// The factory lifecycle is managed by the caller (typically the Loader), not by the parser.
func newJSONParserWithFactory(cfg Config, factory *ComponentFactory) (*jsonParser, error) {
	maxDepth := cfg.JSONMaxDepth
	if maxDepth <= 0 {
		maxDepth = 10 // Default depth
	}

	flattenCfg := internal.JSONFlattenConfig{
		KeyDelimiter:     "_",          // Always use underscore for storage keys
		ArrayIndexFormat: "underscore", // Always use underscore format (KEY_0, KEY_1)
		NullAsEmpty:      cfg.JSONNullAsEmpty,
		NumberAsString:   cfg.JSONNumberAsString,
		BoolAsString:     cfg.JSONBoolAsString,
		MaxDepth:         maxDepth,
	}
	return &jsonParser{
		config:    cfg,
		validator: factory.Validator(),
		auditor:   factory.Auditor(),
		flatten:   flattenCfg,
	}, nil
}

// Parse reads and parses JSON content from an io.Reader.
func (p *jsonParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	start := time.Now()

	// Wrap with secure reader
	secureRd := internal.NewSecureReader(r, p.config.MaxFileSize, p.config.MaxLineLength)
	data, err := io.ReadAll(secureRd)
	if err != nil {
		if errors.Is(err, internal.ErrFileTooLarge) || errors.Is(err, internal.ErrLineTooLong) {
			_ = p.auditor.LogError(internal.ActionParse, "", "file exceeds size limit")
			return nil, &JSONError{
				Path:    filename,
				Message: "file exceeds size limit",
				Err:     err,
			}
		}
		return nil, err
	}

	// Flatten JSON to environment variables
	result, err := internal.FlattenJSON(data, p.flatten)
	if err != nil {
		_ = p.auditor.LogError(internal.ActionParse, "", "invalid JSON syntax")
		return nil, err
	}

	// Validate parsed result using shared validation logic
	if err := (&structuredParserConfig{
		config:    p.config,
		validator: p.validator,
		auditor:   p.auditor,
	}).validateResult(result, "JSON"); err != nil {
		return nil, err
	}

	_ = p.auditor.LogWithDuration(internal.ActionParse, "", "parsed JSON: "+filename, true, time.Since(start))
	return result, nil
}

// Close releases resources held by the parser.
// Note: The parser does not own the ComponentFactory; it is managed by the caller.
func (p *jsonParser) Close() error {
	return nil
}
