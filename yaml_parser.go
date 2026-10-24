package env

import (
	"io"

	"github.com/cybergodev/env/internal"
)

// yamlParser handles parsing of YAML configuration files.
type yamlParser struct {
	structuredParserConfig
	flatten internal.YAMLFlattenConfig
}

// Compile-time check that yamlParser implements EnvParser.
var _ EnvParser = (*yamlParser)(nil)

// newYAMLParserWithFactory creates a new yamlParser with a ComponentFactory.
func newYAMLParserWithFactory(cfg Config, factory *ComponentFactory) (*yamlParser, error) {
	flattenCfg := internal.YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      cfg.YAMLNullAsEmpty,
		NumberAsString:   cfg.YAMLNumberAsString,
		BoolAsString:     cfg.YAMLBoolAsString,
		MaxDepth:         normalizeStructuredDepth(cfg.YAMLMaxDepth),
		DuplicateKey:     cfg.DuplicateKeyPolicy,
	}

	return &yamlParser{
		structuredParserConfig: newStructuredParserConfig(cfg, factory),
		flatten:                flattenCfg,
	}, nil
}

// Parse reads and parses YAML content from an io.Reader.
// Duplicate mapping keys resolve per the configured DuplicateKeyPolicy
// (default: last occurrence wins, deliberately lenient; see
// internal.ParseYAMLWithPolicy). Flat-key collisions from different source
// paths always error (see internal.FlattenYAML).
func (p *yamlParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	return p.structuredParseResult(r, filename, "YAML", func(data []byte) (map[string]string, error) {
		value, err := internal.ParseYAMLWithPolicy(data, p.flatten.MaxDepth, p.flatten.DuplicateKey)
		if err != nil {
			return nil, err
		}
		defer internal.ReleaseValue(value)
		return internal.FlattenYAML(value, p.flatten)
	})
}
