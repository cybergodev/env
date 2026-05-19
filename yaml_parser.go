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
	maxDepth := cfg.YAMLMaxDepth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	flattenCfg := internal.YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      cfg.YAMLNullAsEmpty,
		NumberAsString:   cfg.YAMLNumberAsString,
		BoolAsString:     cfg.YAMLBoolAsString,
		MaxDepth:         maxDepth,
	}

	return &yamlParser{
		structuredParserConfig: structuredParserConfig{
			config:    cfg,
			validator: factory.Validator(),
			auditor:   factory.Auditor(),
		},
		flatten: flattenCfg,
	}, nil
}

// Parse reads and parses YAML content from an io.Reader.
func (p *yamlParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	return p.structuredParseResult(r, filename, "YAML", func(data []byte) (map[string]string, error) {
		value, err := internal.ParseYAML(data, p.flatten.MaxDepth)
		if err != nil {
			return nil, err
		}
		defer internal.ReleaseValue(value)
		return internal.FlattenYAML(value, p.flatten)
	})
}
