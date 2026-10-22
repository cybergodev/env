package env

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Test Data Generation
// ============================================================================

// generateEnvContent creates env file content with the specified number of variables.
func generateEnvContent(numVars int) string {
	var sb strings.Builder
	sb.Grow(numVars * 50)
	for i := 0; i < numVars; i++ {
		_, _ = fmt.Fprintf(&sb, "VAR_%d=\"value_%d_with_some_longer_content_%d\"\n", i, i, i) // builder writes cannot fail
	}
	return sb.String()
}

// generateEnvContentWithExpansion creates env content with variable references.
func generateEnvContentWithExpansion(numVars int) string {
	var sb strings.Builder
	sb.Grow(numVars * 60)
	sb.WriteString("BASE_URL=\"https://api.example.com\"\n")
	sb.WriteString("API_KEY=\"secret-key-12345\"\n")
	for i := 0; i < numVars; i++ {
		switch i % 3 { // builder writes cannot fail
		case 0:
			_, _ = fmt.Fprintf(&sb, "VAR_%d=\"${BASE_URL}/endpoint/%d\"\n", i, i)
		case 1:
			_, _ = fmt.Fprintf(&sb, "VAR_%d=\"$API_KEY-token-%d\"\n", i, i)
		default:
			_, _ = fmt.Fprintf(&sb, "VAR_%d=\"simple_value_%d\"\n", i, i)
		}
	}
	return sb.String()
}

// benchParse builds a loader from cfg and runs one parser.Parse pass over
// content per iteration. The former per-size benchmark functions each
// duplicated this body with only the fixture and config differing.
func benchParse(b *testing.B, format FileFormat, filename, content string, cfg Config) {
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	parser := loader.parsers[format]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parser.Parse(strings.NewReader(content), filename); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// Parser Benchmarks
// ============================================================================

// BenchmarkParser measures .env parsing across file sizes and with variable
// expansion enabled (the former SmallFile/MediumFile/LargeFile/WithExpansion
// functions as sub-benchmarks).
func BenchmarkParser(b *testing.B) {
	b.Run("vars=10", func(b *testing.B) {
		benchParse(b, FormatEnv, "benchmark.env", generateEnvContent(10), DefaultConfig())
	})
	b.Run("vars=100", func(b *testing.B) {
		benchParse(b, FormatEnv, "benchmark.env", generateEnvContent(100), DefaultConfig())
	})
	b.Run("vars=500", func(b *testing.B) {
		cfg := DefaultConfig()
		cfg.MaxVariables = 1000
		benchParse(b, FormatEnv, "benchmark.env", generateEnvContent(500), cfg)
	})
	b.Run("expansion", func(b *testing.B) {
		cfg := DefaultConfig()
		cfg.ExpandVariables = true
		cfg.MaxVariables = 200
		benchParse(b, FormatEnv, "benchmark.env", generateEnvContentWithExpansion(100), cfg)
	})
}

// ============================================================================
// Loader Benchmarks
// ============================================================================

// BenchmarkLoader_LoadFiles measures the full New+LoadFiles+Close cycle from
// a file on disk (the former Small/Medium functions as sub-benchmarks).
func BenchmarkLoader_LoadFiles(b *testing.B) {
	sizes := []struct {
		name     string
		numVars  int
		maxVars  int // 0: keep the DefaultConfig value
		benchTag string
	}{
		{"small", 10, 0, "small"},
		{"medium", 100, 200, "medium"},
	}
	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			cfg := DefaultConfig()
			if sz.maxVars > 0 {
				cfg.MaxVariables = sz.maxVars
			}

			benchFile := "bench_loader_" + sz.benchTag + ".env"
			if err := os.WriteFile(benchFile, []byte(generateEnvContent(sz.numVars)), 0600); err != nil {
				b.Fatal(err)
			}
			defer func() { _ = os.Remove(benchFile) }() // best-effort cleanup

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				loader, err := New(cfg)
				if err != nil {
					b.Fatal(err)
				}
				if err := loader.LoadFiles(benchFile); err != nil {
					b.Fatal(err)
				}
				_ = loader.Close() // best-effort cleanup
			}
		})
	}
}

func BenchmarkLoader_Get(b *testing.B) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Pre-populate with 100 variables and pre-compute keys
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("VAR_%d", i)
		if err := loader.Set(keys[i], fmt.Sprintf("value_%d", i)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = loader.GetString(keys[i%100])
	}
}

func BenchmarkLoader_Lookup(b *testing.B) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Pre-populate with 100 variables and pre-compute keys
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("VAR_%d", i)
		if err := loader.Set(keys[i], fmt.Sprintf("value_%d", i)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = loader.Lookup(keys[i%100])
	}
}

func BenchmarkLoader_Set(b *testing.B) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Pre-populate and pre-compute keys
	keys := make([]string, 100)
	values := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("VAR_%d", i)
		values[i] = fmt.Sprintf("value_%d", i)
		if err := loader.Set(keys[i], values[i]); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = loader.Set(keys[i%100], values[i%100]) // hot loop; valid keys cannot fail
	}
}

// ============================================================================
// LineParser Benchmarks
// ============================================================================

// BenchmarkLineParser measures single-line parsing across input shapes (the
// former SimpleLine/QuotedValue/WithExport functions as sub-benchmarks).
func BenchmarkLineParser(b *testing.B) {
	lines := []struct {
		name              string
		line              string
		allowExportPrefix bool
	}{
		{"simple", "KEY=value", false},
		{"quoted", `KEY="value with spaces and \"escapes\""`, false},
		{"export", "export KEY=value", true},
	}
	for _, tt := range lines {
		b.Run(tt.name, func(b *testing.B) {
			cfg := internal.LineParserConfig{
				AllowExportPrefix: tt.allowExportPrefix,
				AllowYamlSyntax:   false,
			}
			validator := internal.NewValidator(internal.ValidatorConfig{})
			auditor := internal.NewAuditor(internal.NewNopHandler(), nil, nil, false)
			expander := internal.NewExpander(internal.ExpanderConfig{})
			lp := internal.NewLineParser(cfg, validator, auditor, expander)

			line := []byte(tt.line)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _ = lp.ParseLineBytes(line) // benchmark measures the call; result unused
			}
		})
	}
}

// ============================================================================
// Expander Benchmarks
// ============================================================================

func BenchmarkExpander_NoVariables(b *testing.B) {
	input := "This is a simple string with no variables"
	expander := internal.NewExpander(internal.ExpanderConfig{
		MaxDepth: 5,
		Lookup:   func(key string) (string, bool) { return "", false },
		Mode:     internal.ModeEnv,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expander.Expand(input) // benchmark measures the call; result unused
	}
}

func BenchmarkExpander_SingleVariable(b *testing.B) {
	input := "$VAR"
	expander := internal.NewExpander(internal.ExpanderConfig{
		MaxDepth: 5,
		Lookup:   func(key string) (string, bool) { return "value", true },
		Mode:     internal.ModeEnv,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expander.Expand(input) // benchmark measures the call; result unused
	}
}

func BenchmarkExpander_BracedVariable(b *testing.B) {
	input := "${VAR}"
	expander := internal.NewExpander(internal.ExpanderConfig{
		MaxDepth: 5,
		Lookup:   func(key string) (string, bool) { return "value", true },
		Mode:     internal.ModeEnv,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expander.Expand(input) // benchmark measures the call; result unused
	}
}

func BenchmarkExpander_MultipleVariables(b *testing.B) {
	input := "prefix_${VAR1}_middle_${VAR2}_suffix_${VAR3}"
	// Pre-compute lookup values to avoid fmt.Sprintf overhead in benchmark
	lookupValues := map[string]string{
		"VAR1": "VAR1_value",
		"VAR2": "VAR2_value",
		"VAR3": "VAR3_value",
	}
	expander := internal.NewExpander(internal.ExpanderConfig{
		MaxDepth: 5,
		Lookup: func(key string) (string, bool) {
			if v, ok := lookupValues[key]; ok {
				return v, true
			}
			return "", false
		},
		Mode: internal.ModeEnv,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expander.Expand(input) // benchmark measures the call; result unused
	}
}

func BenchmarkExpander_WithDefault(b *testing.B) {
	input := "${VAR:-default_value}"
	expander := internal.NewExpander(internal.ExpanderConfig{
		MaxDepth: 5,
		Lookup:   func(key string) (string, bool) { return "", false },
		Mode:     internal.ModeAll,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expander.Expand(input) // benchmark measures the call; result unused
	}
}

// ============================================================================
// SecureValue Benchmarks
// ============================================================================

func BenchmarkSecureValue_New(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sv := NewSecureValue("sensitive_value_12345")
		sv.Release()
	}
}

func BenchmarkSecureValue_String(b *testing.B) {
	sv := NewSecureValue("sensitive_value_12345")
	defer sv.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sv.String()
	}
}

func BenchmarkSecureValue_Bytes(b *testing.B) {
	sv := NewSecureValue("sensitive_value_12345")
	defer sv.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sv.Bytes()
	}
}

// ============================================================================
// secureMap Benchmarks
// ============================================================================

func BenchmarkSecureMap_Set(b *testing.B) {
	sm := newSecureMap()
	defer sm.Clear()

	// Pre-compute keys and values
	keys := make([]string, 100)
	values := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("KEY_%d", i)
		values[i] = fmt.Sprintf("value_%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.Set(keys[i%100], values[i%100])
	}
}

func BenchmarkSecureMap_Get(b *testing.B) {
	sm := newSecureMap()
	defer sm.Clear()

	// Pre-populate and pre-compute keys
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("KEY_%d", i)
		sm.Set(keys[i], fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.Get(keys[i%100])
	}
}

func BenchmarkSecureMap_SetAll(b *testing.B) {
	values := make(map[string]string, 100)
	for i := 0; i < 100; i++ {
		values[fmt.Sprintf("KEY_%d", i)] = fmt.Sprintf("value_%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm := newSecureMap()
		sm.SetAll(values)
		sm.Clear()
	}
}

// ============================================================================
// Key Interning Benchmarks
// ============================================================================

func BenchmarkInternKeyBytes_New(b *testing.B) {
	// Clear cache first
	internal.ClearInternCache()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use different keys to avoid cache hits
		key := fmt.Sprintf("KEY_%d", i)
		internal.InternKeyBytes([]byte(key))
	}
}

// BenchmarkInternKeyBytes_Cached measures the byte-slice interning path used by
// the parser (keys arrive as []byte from the scanner buffer). On a cache hit it
// allocates nothing, unlike interning a freshly-built string, which allocates
// a temporary string on every call. (The former CachedFromStrings variant was
// removed: its hot loop rebuilt the key with fmt.Sprintf, so it measured the
// caller's string construction rather than the interning cache.)
func BenchmarkInternKeyBytes_Cached(b *testing.B) {
	internal.ClearInternCache()
	keys := make([][]byte, 100)
	for i := 0; i < 100; i++ {
		keys[i] = []byte(fmt.Sprintf("KEY_%d", i))
		internal.InternKeyBytes(keys[i]) // pre-populate
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = internal.InternKeyBytes(keys[i%100])
	}
}

// ============================================================================
// ToUpperASCII Benchmarks
// ============================================================================

func BenchmarkToUpperASCII(b *testing.B) {
	inputs := []string{
		"lowercase_key",
		"UPPERCASE_KEY",
		"MixedCase_Key_123",
	}

	for _, input := range inputs {
		b.Run(input, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				internal.ToUpperASCII(input)
			}
		})
	}
}

// ============================================================================
// Pool Benchmarks
// ============================================================================

func BenchmarkBuilderPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sb := internal.GetBuilder()
		sb.WriteString("test content")
		sb.WriteString(" more content")
		_ = sb.String()
		internal.PutBuilder(sb)
	}
}

// ============================================================================
// Bytes vs String Benchmarks
// ============================================================================

// BenchmarkParseDoubleQuoted measures double-quoted value parsing with and
// without escapes (the former NoEscape/WithEscape functions as
// sub-benchmarks).
func BenchmarkParseDoubleQuoted(b *testing.B) {
	inputs := []struct {
		name  string
		input string
	}{
		{"no-escape", `"simple value"`},
		{"with-escape", `"value with \"escapes\" and \n newlines"`},
	}
	for _, tt := range inputs {
		b.Run(tt.name, func(b *testing.B) {
			input := []byte(tt.input)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = internal.ParseDoubleQuotedBytes(input) // benchmark measures the call; result unused
			}
		})
	}
}

// ============================================================================
// Concurrent Access Benchmarks
// ============================================================================

func BenchmarkLoader_ConcurrentGet(b *testing.B) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Pre-populate with 100 variables and pre-compute keys
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("VAR_%d", i)
		keys[i] = key
		if err := loader.Set(key, "value"); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = loader.GetString(keys[i%100])
			i++
		}
	})
}

func BenchmarkLoader_ConcurrentSet(b *testing.B) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Pre-compute keys to avoid fmt.Sprintf in hot path
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("VAR_%d", i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = loader.Set(keys[i%100], "value") // hot loop; valid keys cannot fail
			i++
		}
	})
}

// ============================================================================
// Structured Parser Benchmarks
// ============================================================================

func generateJSONContent(numVars int) string {
	var sb strings.Builder
	sb.Grow(numVars * 50)
	sb.WriteString("{")
	for i := 0; i < numVars; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(&sb, "\"var_%d\":\"value_%d_with_some_content\"", i, i) // builder writes cannot fail
	}
	sb.WriteString("}")
	return sb.String()
}

// generateYAMLContent creates a flat YAML document with numVars variables.
func generateYAMLContent(numVars int) string {
	var sb strings.Builder
	sb.Grow(numVars * 50)
	for i := 0; i < numVars; i++ {
		_, _ = fmt.Fprintf(&sb, "var_%d: value_%d_with_some_content\n", i, i) // builder writes cannot fail
	}
	return sb.String()
}

// generateNestedYAMLContent creates a nested YAML document: numGroups groups
// of three keyed settings each (~3*numGroups flattened leaves). Unlike the
// flat fixtures above, every leaf key is built with a non-empty prefix, which
// exercises the nested key-construction path in the flatteners.
func generateNestedYAMLContent(numGroups int) string {
	var sb strings.Builder
	sb.Grow(numGroups * 70)
	for g := 0; g < numGroups; g++ {
		_, _ = fmt.Fprintf(&sb, "group_%d:\n  host: host_%d.example.com\n  port: %d\n  enabled: true\n", g, g, 8000+g) // builder writes cannot fail
	}
	return sb.String()
}

// generateNestedJSONContent creates a nested JSON document matching the YAML
// fixture's shape (~3*numGroups flattened leaves with prefixed key builds).
func generateNestedJSONContent(numGroups int) string {
	var sb strings.Builder
	sb.Grow(numGroups * 90)
	sb.WriteString("{")
	for g := 0; g < numGroups; g++ {
		if g > 0 {
			sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(&sb, `"group_%d":{"host":"host_%d.example.com","port":%d,"enabled":true}`, g, g, 8000+g) // builder writes cannot fail
	}
	sb.WriteString("}")
	return sb.String()
}

// BenchmarkJSONParser measures JSON parsing across flat sizes and a nested
// document (the former Small/Medium/Nested functions as sub-benchmarks).
func BenchmarkJSONParser(b *testing.B) {
	b.Run("flat-10", func(b *testing.B) {
		benchParse(b, FormatJSON, "benchmark.json", generateJSONContent(10), DefaultConfig())
	})
	b.Run("flat-100", func(b *testing.B) {
		cfg := DefaultConfig()
		cfg.MaxVariables = 200
		cfg.MaxLineLength = 16384 // Single-line JSON exceeds the 8192 default
		benchParse(b, FormatJSON, "benchmark.json", generateJSONContent(100), cfg)
	})
	b.Run("nested-99", func(b *testing.B) {
		cfg := DefaultConfig()
		cfg.MaxVariables = 200
		cfg.MaxLineLength = 16384 // Single-line JSON exceeds the 8192 default
		benchParse(b, FormatJSON, "benchmark.json", generateNestedJSONContent(33), cfg)
	})
}

// BenchmarkYAMLParser measures YAML parsing across flat sizes and a nested
// document (the former Small/Medium/Nested functions as sub-benchmarks).
func BenchmarkYAMLParser(b *testing.B) {
	b.Run("flat-10", func(b *testing.B) {
		benchParse(b, FormatYAML, "benchmark.yaml", generateYAMLContent(10), DefaultConfig())
	})
	b.Run("flat-100", func(b *testing.B) {
		cfg := DefaultConfig()
		cfg.MaxVariables = 200
		benchParse(b, FormatYAML, "benchmark.yaml", generateYAMLContent(100), cfg)
	})
	b.Run("nested-99", func(b *testing.B) {
		cfg := DefaultConfig()
		cfg.MaxVariables = 200
		benchParse(b, FormatYAML, "benchmark.yaml", generateNestedYAMLContent(33), cfg)
	})
}

// ============================================================================
// Targeted Optimization Benchmarks
// ============================================================================

// BenchmarkHasUpperPrefix benchmarks the case-insensitive prefix check.
func BenchmarkHasUpperPrefix(b *testing.B) {
	b.Run("match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			internal.HasUpperPrefix("APP_DATABASE_HOST", "APP_")
		}
	})
	b.Run("no_match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			internal.HasUpperPrefix("DATABASE_HOST", "APP_")
		}
	})
	b.Run("lowercase_match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			internal.HasUpperPrefix("app_database_host", "APP_")
		}
	})
}

// BenchmarkEqualFoldASCII benchmarks the case-insensitive string comparison.
func BenchmarkEqualFoldASCII(b *testing.B) {
	inputs := []struct {
		name string
		a, b string
	}{
		{"null", "NULL", "null"},
		{"true", "True", "true"},
		{"false", "FALSE", "false"},
		{"mismatch", "hello", "world"},
	}
	for _, tt := range inputs {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				internal.EqualFoldASCII(tt.a, tt.b)
			}
		})
	}
}

// BenchmarkYAMLConvertScalar benchmarks the YAML scalar conversion path.
func BenchmarkYAMLConvertScalar(b *testing.B) {
	// This tests the internal path that was optimized to avoid allocations
	cfg := internal.YAMLFlattenConfig{
		NullAsEmpty:    true,
		NumberAsString: true,
		BoolAsString:   true,
	}
	scalars := []struct {
		name  string
		value string
	}{
		{"null", "null"},
		{"true", "true"},
		{"false", "False"},
		{"number", "42"},
		{"string", "hello_world"},
		{"tilde", "~"},
	}
	for _, tt := range scalars {
		b.Run(tt.name, func(b *testing.B) {
			// Access internal FlattenYAML to test the full path including convertYAMLScalar
			for i := 0; i < b.N; i++ {
				_, _ = internal.FlattenYAML( // benchmark measures the call; result unused
					internal.NewScalarValue(tt.value, 1, 1),
					cfg,
				)
			}
		})
	}
}
