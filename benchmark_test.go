package env

import (
	"bytes"
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
		sb.WriteString(fmt.Sprintf("VAR_%d=\"value_%d_with_some_longer_content_%d\"\n", i, i, i))
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
		if i%3 == 0 {
			sb.WriteString(fmt.Sprintf("VAR_%d=\"${BASE_URL}/endpoint/%d\"\n", i, i))
		} else if i%3 == 1 {
			sb.WriteString(fmt.Sprintf("VAR_%d=\"$API_KEY-token-%d\"\n", i, i))
		} else {
			sb.WriteString(fmt.Sprintf("VAR_%d=\"simple_value_%d\"\n", i, i))
		}
	}
	return sb.String()
}

// ============================================================================
// Parser Benchmarks
// ============================================================================

func BenchmarkParser_SmallFile(b *testing.B) {
	content := generateEnvContent(10)
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatEnv]
		_, err := parser.Parse(r, "benchmark.env")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParser_MediumFile(b *testing.B) {
	content := generateEnvContent(100)
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatEnv]
		_, err := parser.Parse(r, "benchmark.env")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParser_LargeFile(b *testing.B) {
	content := generateEnvContent(500)
	cfg := DefaultConfig()
	cfg.MaxVariables = 1000 // Increase limit for large file test
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatEnv]
		_, err := parser.Parse(r, "benchmark.env")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParser_WithExpansion(b *testing.B) {
	content := generateEnvContentWithExpansion(100)
	cfg := DefaultConfig()
	cfg.ExpandVariables = true
	cfg.MaxVariables = 200 // Increase limit for expansion test
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatEnv]
		_, err := parser.Parse(r, "benchmark.env")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// Loader Benchmarks
// ============================================================================

func BenchmarkLoader_LoadFiles_Small(b *testing.B) {
	content := generateEnvContent(10)
	cfg := DefaultConfig()

	const benchFile = "bench_loader_small.env"
	if err := os.WriteFile(benchFile, []byte(content), 0600); err != nil {
		b.Fatal(err)
	}
	defer os.Remove(benchFile)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loader, err := New(cfg)
		if err != nil {
			b.Fatal(err)
		}
		if err := loader.LoadFiles(benchFile); err != nil {
			b.Fatal(err)
		}
		loader.Close()
	}
}

func BenchmarkLoader_LoadFiles_Medium(b *testing.B) {
	content := generateEnvContent(100)
	cfg := DefaultConfig()
	cfg.MaxVariables = 200

	const benchFile = "bench_loader_medium.env"
	if err := os.WriteFile(benchFile, []byte(content), 0600); err != nil {
		b.Fatal(err)
	}
	defer os.Remove(benchFile)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loader, err := New(cfg)
		if err != nil {
			b.Fatal(err)
		}
		if err := loader.LoadFiles(benchFile); err != nil {
			b.Fatal(err)
		}
		loader.Close()
	}
}

func BenchmarkLoader_Get(b *testing.B) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	// Pre-populate with 100 variables
	for i := 0; i < 100; i++ {
		loader.Set(fmt.Sprintf("VAR_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("VAR_%d", i%100)
		_ = loader.GetString(key)
	}
}

func BenchmarkLoader_Lookup(b *testing.B) {
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	// Pre-populate with 100 variables
	for i := 0; i < 100; i++ {
		loader.Set(fmt.Sprintf("VAR_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("VAR_%d", i%100)
		_, _ = loader.Lookup(key)
	}
}

func BenchmarkLoader_Set(b *testing.B) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("VAR_%d", i%100)
		loader.Set(key, fmt.Sprintf("value_%d", i))
	}
}

// ============================================================================
// LineParser Benchmarks
// ============================================================================

func BenchmarkLineParser_SimpleLine(b *testing.B) {
	line := []byte("KEY=value")
	cfg := internal.LineParserConfig{
		AllowExportPrefix: false,
		AllowYamlSyntax:   false,
		OverwriteExisting: true,
		MaxVariables:      1000,
		ExpandVariables:   false,
	}
	validator := internal.NewValidator(internal.ValidatorConfig{})
	auditor := internal.NewAuditor(internal.NewNopHandler(), nil, nil, false)
	expander := internal.NewExpander(internal.ExpanderConfig{})
	lp := internal.NewLineParser(cfg, validator, auditor, expander)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lp.ParseLineBytes(line)
	}
}

func BenchmarkLineParser_QuotedValue(b *testing.B) {
	line := []byte(`KEY="value with spaces and \"escapes\""`)
	cfg := internal.LineParserConfig{
		AllowExportPrefix: false,
		AllowYamlSyntax:   false,
		OverwriteExisting: true,
		MaxVariables:      1000,
		ExpandVariables:   false,
	}
	validator := internal.NewValidator(internal.ValidatorConfig{})
	auditor := internal.NewAuditor(internal.NewNopHandler(), nil, nil, false)
	expander := internal.NewExpander(internal.ExpanderConfig{})
	lp := internal.NewLineParser(cfg, validator, auditor, expander)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lp.ParseLineBytes(line)
	}
}

func BenchmarkLineParser_WithExport(b *testing.B) {
	line := []byte("export KEY=value")
	cfg := internal.LineParserConfig{
		AllowExportPrefix: true,
		AllowYamlSyntax:   false,
		OverwriteExisting: true,
		MaxVariables:      1000,
		ExpandVariables:   false,
	}
	validator := internal.NewValidator(internal.ValidatorConfig{})
	auditor := internal.NewAuditor(internal.NewNopHandler(), nil, nil, false)
	expander := internal.NewExpander(internal.ExpanderConfig{})
	lp := internal.NewLineParser(cfg, validator, auditor, expander)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lp.ParseLineBytes(line)
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
		expander.Expand(input)
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
		expander.Expand(input)
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
		expander.Expand(input)
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
		expander.Expand(input)
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
		expander.Expand(input)
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("KEY_%d", i%100)
		sm.Set(key, fmt.Sprintf("value_%d", i))
	}
}

func BenchmarkSecureMap_Get(b *testing.B) {
	sm := newSecureMap()
	defer sm.Clear()

	// Pre-populate
	for i := 0; i < 100; i++ {
		sm.Set(fmt.Sprintf("KEY_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("KEY_%d", i%100)
		sm.Get(key)
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

func BenchmarkInternKey_New(b *testing.B) {
	// Clear cache first
	internal.ClearInternCache()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use different keys to avoid cache hits
		key := fmt.Sprintf("KEY_%d", i)
		internal.InternKey(key)
	}
}

func BenchmarkInternKey_Cached(b *testing.B) {
	// Clear cache and pre-populate
	internal.ClearInternCache()
	for i := 0; i < 100; i++ {
		internal.InternKey(fmt.Sprintf("KEY_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("KEY_%d", i%100)
		internal.InternKey(key)
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

func BenchmarkParseDoubleQuoted_NoEscape(b *testing.B) {
	input := []byte(`"simple value"`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		internal.ParseDoubleQuotedBytes(input)
	}
}

func BenchmarkParseDoubleQuoted_WithEscape(b *testing.B) {
	input := []byte(`"value with \"escapes\" and \n newlines"`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		internal.ParseDoubleQuotedBytes(input)
	}
}

// ============================================================================
// Scanner Buffer Benchmarks
// ============================================================================

func BenchmarkScannerBuffer(b *testing.B) {
	content := generateEnvContent(100)
	reader := bytes.NewReader([]byte(content))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Seek(0, 0)
		buf := make([]byte, 64*1024)
		scanner := bytes.NewReader(buf)
		_, _ = scanner.ReadAt(buf, 0)
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
	defer loader.Close()

	// Pre-populate with 100 variables and pre-compute keys
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("VAR_%d", i)
		keys[i] = key
		loader.Set(key, "value")
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

// ============================================================================
// JSON Parser Benchmarks
// ============================================================================

func generateJSONContent(numVars int) string {
	var sb strings.Builder
	sb.Grow(numVars * 50)
	sb.WriteString("{")
	for i := 0; i < numVars; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("\"var_%d\":\"value_%d_with_some_content\"", i, i))
	}
	sb.WriteString("}")
	return sb.String()
}

func BenchmarkJSONParser_Small(b *testing.B) {
	content := generateJSONContent(10)
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatJSON]
		_, err := parser.Parse(r, "benchmark.json")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONParser_Medium(b *testing.B) {
	content := generateJSONContent(100)
	cfg := DefaultConfig()
	cfg.MaxVariables = 200
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatJSON]
		_, err := parser.Parse(r, "benchmark.json")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// YAML Parser Benchmarks
// ============================================================================

func generateYAMLContent(numVars int) string {
	var sb strings.Builder
	sb.Grow(numVars * 50)
	for i := 0; i < numVars; i++ {
		sb.WriteString(fmt.Sprintf("var_%d: value_%d_with_some_content\n", i, i))
	}
	return sb.String()
}

func BenchmarkYAMLParser_Small(b *testing.B) {
	content := generateYAMLContent(10)
	cfg := DefaultConfig()
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatYAML]
		_, err := parser.Parse(r, "benchmark.yaml")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkYAMLParser_Medium(b *testing.B) {
	content := generateYAMLContent(100)
	cfg := DefaultConfig()
	cfg.MaxVariables = 200
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(content)
		parser := loader.parsers[FormatYAML]
		_, err := parser.Parse(r, "benchmark.yaml")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoader_ConcurrentSet(b *testing.B) {
	cfg := DefaultConfig()
	cfg.OverwriteExisting = true
	loader, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer loader.Close()

	// Pre-compute keys to avoid fmt.Sprintf in hot path
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("VAR_%d", i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			loader.Set(keys[i%100], "value")
			i++
		}
	})
}
