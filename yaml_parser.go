package env

import (
	"errors"
	"io"
	"time"

	"github.com/cybergodev/env/internal"
)

// yamlParser handles parsing of YAML configuration files.
type yamlParser struct {
	config    Config
	validator Validator
	auditor   FullAuditLogger
	flatten   internal.YAMLFlattenConfig
}

// Compile-time check that yamlParser implements EnvParser.
var _ EnvParser = (*yamlParser)(nil)

// newYAMLParserWithFactory creates a new yamlParser with a ComponentFactory.
// The factory lifecycle is managed by the caller (typically the Loader), not by the parser.
func newYAMLParserWithFactory(cfg Config, factory *ComponentFactory) (*yamlParser, error) {
	maxDepth := cfg.YAMLMaxDepth
	if maxDepth <= 0 {
		maxDepth = 10 // Default depth
	}

	flattenCfg := internal.YAMLFlattenConfig{
		KeyDelimiter:     "_",          // Always use underscore for storage keys
		ArrayIndexFormat: "underscore", // Always use underscore format (KEY_0, KEY_1)
		NullAsEmpty:      cfg.YAMLNullAsEmpty,
		NumberAsString:   cfg.YAMLNumberAsString,
		BoolAsString:     cfg.YAMLBoolAsString,
		MaxDepth:         maxDepth,
	}

	return &yamlParser{
		config:    cfg,
		validator: factory.Validator(),
		auditor:   factory.Auditor(),
		flatten:   flattenCfg,
	}, nil
}

// Parse reads and parses YAML content from an io.Reader.
func (p *yamlParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	start := time.Now()

	// Wrap with secure reader
	secureRd := internal.NewSecureReader(r, p.config.MaxFileSize, p.config.MaxLineLength)
	data, err := io.ReadAll(secureRd)
	if err != nil {
		if errors.Is(err, internal.ErrFileTooLarge) || errors.Is(err, internal.ErrLineTooLong) {
			_ = p.auditor.LogError(internal.ActionParse, "", "file exceeds size limit")
			return nil, &YAMLError{
				Path:    filename,
				Message: "file exceeds size limit",
				Err:     err,
			}
		}
		return nil, err
	}

	// Parse YAML
	value, err := internal.ParseYAML(data, p.flatten.MaxDepth)
	if err != nil {
		_ = p.auditor.LogError(internal.ActionParse, "", "invalid YAML syntax")
		return nil, err
	}
	// Ensure the Value tree is always released back to the pool.
	defer internal.ReleaseValue(value)

	// Flatten YAML to environment variables
	result, err := internal.FlattenYAML(value, p.flatten)
	if err != nil {
		_ = p.auditor.LogError(internal.ActionParse, "", "failed to flatten YAML")
		return nil, err
	}

	// Validate parsed result using shared validation logic
	if err := (&structuredParserConfig{
		config:    p.config,
		validator: p.validator,
		auditor:   p.auditor,
	}).validateResult(result, "YAML"); err != nil {
		return nil, err
	}

	_ = p.auditor.LogWithDuration(internal.ActionParse, "", "parsed YAML: "+filename, true, time.Since(start))
	return result, nil
}

// Close releases resources held by the parser.
// Note: The parser does not own the ComponentFactory; it is managed by the caller.
func (p *yamlParser) Close() error {
	return nil
}
