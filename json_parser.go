package env

import (
	"io"

	"github.com/cybergodev/env/internal"
)

// jsonParser handles parsing of JSON configuration files.
type jsonParser struct {
	structuredParserConfig
	flatten internal.JSONFlattenConfig
}

// Compile-time check that jsonParser implements EnvParser.
var _ EnvParser = (*jsonParser)(nil)

// newJSONParserWithFactory creates a new jsonParser with a ComponentFactory.
func newJSONParserWithFactory(cfg Config, factory *ComponentFactory) (*jsonParser, error) {
	maxDepth := cfg.JSONMaxDepth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	flattenCfg := internal.JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      cfg.JSONNullAsEmpty,
		NumberAsString:   cfg.JSONNumberAsString,
		BoolAsString:     cfg.JSONBoolAsString,
		MaxDepth:         maxDepth,
	}
	return &jsonParser{
		structuredParserConfig: structuredParserConfig{
			config:    cfg,
			validator: factory.Validator(),
			auditor:   factory.Auditor(),
		},
		flatten: flattenCfg,
	}, nil
}

// Parse reads and parses JSON content from an io.Reader.
func (p *jsonParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	return p.structuredParseResult(r, filename, "JSON", func(data []byte) (map[string]string, error) {
		return internal.FlattenJSON(data, p.flatten)
	})
}
