package internal

import (
	"testing"
)

// ============================================================================
// YAML Flatten Tests
// ============================================================================

func TestFlattenYAML_NilValue(t *testing.T) {
	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(nil, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML(nil) error = %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map for nil value, got %d items", len(result))
	}
}

func TestFlattenYAML_SimpleMap(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"KEY1": NewScalarValue("value1", 1, 1),
			"KEY2": NewScalarValue("value2", 1, 1),
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}

	if result["KEY1"] != "value1" {
		t.Errorf("result[\"KEY1\"] = %q, want %q", result["KEY1"], "value1")
	}

	if result["KEY2"] != "value2" {
		t.Errorf("result[\"KEY2\"] = %q, want %q", result["KEY2"], "value2")
	}
}

func TestFlattenYAML_NestedMap(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"DATABASE": {
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"HOST": NewScalarValue("localhost", 2, 3),
					"PORT": NewScalarValue("5432", 2, 3),
				},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["DATABASE_HOST"] != "localhost" {
		t.Errorf("result[\"DATABASE_HOST\"] = %q, want %q", result["DATABASE_HOST"], "localhost")
	}

	if result["DATABASE_PORT"] != "5432" {
		t.Errorf("result[\"DATABASE_PORT\"] = %q, want %q", result["DATABASE_PORT"], "5432")
	}
}

func TestFlattenYAML_Array(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"ITEMS": {
				Type: ValueTypeArray,
				Array: []*Value{
					NewScalarValue("one", 1, 1),
					NewScalarValue("two", 1, 1),
					NewScalarValue("three", 1, 1),
				},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["ITEMS_0"] != "one" {
		t.Errorf("result[\"ITEMS_0\"] = %q, want %q", result["ITEMS_0"], "one")
	}

	if result["ITEMS_1"] != "two" {
		t.Errorf("result[\"ITEMS_1\"] = %q, want %q", result["ITEMS_1"], "two")
	}

	if result["ITEMS_2"] != "three" {
		t.Errorf("result[\"ITEMS_2\"] = %q, want %q", result["ITEMS_2"], "three")
	}
}

func TestFlattenYAML_ArrayBracketFormat(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"ITEMS": {
				Type: ValueTypeArray,
				Array: []*Value{
					NewScalarValue("one", 1, 1),
					NewScalarValue("two", 1, 1),
				},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "bracket",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["ITEMS[0]"] != "one" {
		t.Errorf("result[\"ITEMS[0]\"] = %q, want %q", result["ITEMS[0]"], "one")
	}

	if result["ITEMS[1]"] != "two" {
		t.Errorf("result[\"ITEMS[1]\"] = %q, want %q", result["ITEMS[1]"], "two")
	}
}

func TestFlattenYAML_EmptyMap(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"EMPTY": {
				Type: ValueTypeMap,
				Map:  map[string]*Value{},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["EMPTY"] != "" {
		t.Errorf("result[\"EMPTY\"] = %q, want empty string", result["EMPTY"])
	}
}

func TestFlattenYAML_EmptyArray(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"EMPTY": {
				Type:  ValueTypeArray,
				Array: []*Value{},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["EMPTY"] != "" {
		t.Errorf("result[\"EMPTY\"] = %q, want empty string", result["EMPTY"])
	}
}

func TestFlattenYAML_MaxDepthExceeded(t *testing.T) {
	// Create deeply nested structure
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"A": {
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"B": {
						Type: ValueTypeMap,
						Map: map[string]*Value{
							"C": NewScalarValue("deep", 1, 1),
						},
					},
				},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         2, // Low limit
	}

	_, err := FlattenYAML(value, cfg)
	if err == nil {
		t.Error("expected max depth exceeded error")
	}
}

func TestFlattenYAML_ScalarWithNoPrefix(t *testing.T) {
	value := NewScalarValue("standalone", 1, 1)

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	// Scalar with no prefix should not be added
	if len(result) != 0 {
		t.Errorf("expected empty map for scalar with no prefix, got %d items", len(result))
	}
}

// ============================================================================
// convertYAMLScalar Tests
// ============================================================================

func TestConvertYAMLScalar(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		cfg      YAMLFlattenConfig
		expected string
	}{
		{
			name:     "null as empty",
			input:    "null",
			cfg:      YAMLFlattenConfig{NullAsEmpty: true},
			expected: "",
		},
		{
			name:     "null preserved",
			input:    "null",
			cfg:      YAMLFlattenConfig{NullAsEmpty: false},
			expected: "null",
		},
		{
			name:     "tilde as empty",
			input:    "~",
			cfg:      YAMLFlattenConfig{NullAsEmpty: true},
			expected: "",
		},
		{
			name:     "true bool",
			input:    "true",
			cfg:      YAMLFlattenConfig{BoolAsString: true},
			expected: "true",
		},
		{
			name:     "false bool",
			input:    "false",
			cfg:      YAMLFlattenConfig{BoolAsString: true},
			expected: "false",
		},
		{
			name:     "integer",
			input:    "42",
			cfg:      YAMLFlattenConfig{NumberAsString: true},
			expected: "42",
		},
		{
			name:     "float",
			input:    "3.14",
			cfg:      YAMLFlattenConfig{NumberAsString: true},
			expected: "3.14",
		},
		{
			name:     "float as int",
			input:    "42.0",
			cfg:      YAMLFlattenConfig{NumberAsString: true},
			expected: "42",
		},
		{
			name:     "negative number",
			input:    "-42",
			cfg:      YAMLFlattenConfig{NumberAsString: true},
			expected: "-42",
		},
		{
			name:     "regular string",
			input:    "hello world",
			cfg:      YAMLFlattenConfig{},
			expected: "hello world",
		},
		{
			name:     "empty string",
			input:    "",
			cfg:      YAMLFlattenConfig{NullAsEmpty: true},
			expected: "",
		},
		{
			name:     "TRUE uppercase",
			input:    "TRUE",
			cfg:      YAMLFlattenConfig{BoolAsString: true},
			expected: "true",
		},
		{
			name:     "False mixed case",
			input:    "False",
			cfg:      YAMLFlattenConfig{BoolAsString: true},
			expected: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertYAMLScalar(tt.input, tt.cfg)
			if result != tt.expected {
				t.Errorf("convertYAMLScalar(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// buildYAMLKey Tests
// ============================================================================

func TestBuildYAMLKey(t *testing.T) {
	cfg := YAMLFlattenConfig{KeyDelimiter: "_"}

	tests := []struct {
		prefix   string
		key      string
		expected string
	}{
		{"", "KEY", "KEY"},
		{"APP", "KEY", "APP_KEY"},
		{"APP_DATABASE", "HOST", "APP_DATABASE_HOST"},
		{"", "lower", "LOWER"},
		{"prefix", "MixedCase", "prefix_MIXEDCASE"}, // prefix is not uppercased, only key is
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"_"+tt.key, func(t *testing.T) {
			result := buildYAMLKey(tt.prefix, tt.key, cfg)
			if result != tt.expected {
				t.Errorf("buildYAMLKey(%q, %q) = %q, want %q", tt.prefix, tt.key, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// buildYAMLArrayIndex Tests
// ============================================================================

func TestBuildYAMLArrayIndex(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		index    int
		cfg      YAMLFlattenConfig
		expected string
	}{
		{
			name:     "underscore format",
			prefix:   "ITEMS",
			index:    0,
			cfg:      YAMLFlattenConfig{ArrayIndexFormat: "underscore", KeyDelimiter: "_"},
			expected: "ITEMS_0",
		},
		{
			name:     "bracket format",
			prefix:   "ITEMS",
			index:    5,
			cfg:      YAMLFlattenConfig{ArrayIndexFormat: "bracket"},
			expected: "ITEMS[5]",
		},
		{
			name:     "nested underscore",
			prefix:   "SERVERS_PORTS",
			index:    2,
			cfg:      YAMLFlattenConfig{ArrayIndexFormat: "underscore", KeyDelimiter: "_"},
			expected: "SERVERS_PORTS_2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildYAMLArrayIndex(tt.prefix, tt.index, tt.cfg)
			if result != tt.expected {
				t.Errorf("buildYAMLArrayIndex(%q, %d) = %q, want %q", tt.prefix, tt.index, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Inline JSON Tests
// ============================================================================

func TestFlattenYAML_InlineJSONArray(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"CONFIG": NewScalarValue(`["a", "b", "c"]`, 1, 1),
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["CONFIG_0"] != "a" {
		t.Errorf("result[\"CONFIG_0\"] = %q, want %q", result["CONFIG_0"], "a")
	}
	if result["CONFIG_1"] != "b" {
		t.Errorf("result[\"CONFIG_1\"] = %q, want %q", result["CONFIG_1"], "b")
	}
	if result["CONFIG_2"] != "c" {
		t.Errorf("result[\"CONFIG_2\"] = %q, want %q", result["CONFIG_2"], "c")
	}
}

func TestFlattenYAML_InlineJSONObject(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"CONFIG": NewScalarValue(`{"host": "localhost", "port": 8080}`, 1, 1),
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["CONFIG_HOST"] != "localhost" {
		t.Errorf("result[\"CONFIG_HOST\"] = %q, want %q", result["CONFIG_HOST"], "localhost")
	}
	if result["CONFIG_PORT"] != "8080" {
		t.Errorf("result[\"CONFIG_PORT\"] = %q, want %q", result["CONFIG_PORT"], "8080")
	}
}

func TestFlattenYAML_InvalidInlineJSON(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"CONFIG": NewScalarValue(`[not valid json]`, 1, 1),
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	// Invalid JSON should be treated as regular scalar
	if result["CONFIG"] != "[not valid json]" {
		t.Errorf("result[\"CONFIG\"] = %q, want %q", result["CONFIG"], "[not valid json]")
	}
}

// ============================================================================
// Complex Structure Tests
// ============================================================================

func TestFlattenYAML_ComplexStructure(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"APP": {
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"NAME":    NewScalarValue("myapp", 1, 1),
					"VERSION": NewScalarValue("1.0.0", 1, 1),
					"FEATURES": {
						Type: ValueTypeArray,
						Array: []*Value{
							NewScalarValue("auth", 1, 1),
							NewScalarValue("logging", 1, 1),
						},
					},
				},
			},
			"DATABASE": {
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"HOST": NewScalarValue("localhost", 1, 1),
					"PORT": NewScalarValue("5432", 1, 1),
				},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	expected := map[string]string{
		"APP_NAME":       "myapp",
		"APP_VERSION":    "1.0.0",
		"APP_FEATURES_0": "auth",
		"APP_FEATURES_1": "logging",
		"DATABASE_HOST":  "localhost",
		"DATABASE_PORT":  "5432",
	}

	for key, exp := range expected {
		if result[key] != exp {
			t.Errorf("result[%q] = %q, want %q", key, result[key], exp)
		}
	}
}

func TestFlattenYAML_NilInMap(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"KEY": nil,
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	// Should not panic
	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	// nil value should be skipped
	if len(result) != 0 {
		t.Errorf("expected empty map for nil value in map, got %d items", len(result))
	}
}

func TestFlattenYAML_NilInArray(t *testing.T) {
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"ITEMS": {
				Type: ValueTypeArray,
				Array: []*Value{
					NewScalarValue("one", 1, 1),
					nil,
					NewScalarValue("three", 1, 1),
				},
			},
		},
	}

	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	// Should not panic
	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["ITEMS_0"] != "one" {
		t.Errorf("result[\"ITEMS_0\"] = %q, want %q", result["ITEMS_0"], "one")
	}
	// Index 1 is nil, should be skipped
	if result["ITEMS_2"] != "three" {
		t.Errorf("result[\"ITEMS_2\"] = %q, want %q", result["ITEMS_2"], "three")
	}
}

func TestFlattenYAML_LongKeys(t *testing.T) {
	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	// Build a long key that exceeds 64 chars to hit builder path
	longKey := "a"
	for i := 0; i < 70; i++ {
		longKey += "b"
	}

	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			longKey: {
				Type: ValueTypeMap,
				Map: map[string]*Value{
					"nested": NewScalarValue("value", 1, 1),
				},
			},
		},
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}

	for k := range result {
		if len(k) < 70 {
			t.Errorf("expected key length >= 70, got %d: %q", len(k), k)
		}
	}
}

func TestFlattenYAML_BracketArrayFormat(t *testing.T) {
	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "bracket",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"items": {
				Type: ValueTypeArray,
				Array: []*Value{
					NewScalarValue("a", 1, 1),
					NewScalarValue("b", 1, 1),
					NewScalarValue("c", 1, 1),
				},
			},
		},
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	if result["ITEMS[0]"] != "a" {
		t.Errorf("ITEMS[0] = %q, want %q", result["ITEMS[0]"], "a")
	}
	if result["ITEMS[1]"] != "b" {
		t.Errorf("ITEMS[1] = %q, want %q", result["ITEMS[1]"], "b")
	}
}

func TestFlattenYAML_InlineJSON(t *testing.T) {
	cfg := YAMLFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	// Test with inline JSON-like string value
	value := &Value{
		Type: ValueTypeMap,
		Map: map[string]*Value{
			"config": NewScalarValue(`{"nested": {"key": "value"}}`, 1, 1),
		},
	}

	result, err := FlattenYAML(value, cfg)
	if err != nil {
		t.Fatalf("FlattenYAML() error = %v", err)
	}

	// Should detect inline JSON and flatten it
	found := false
	for k := range result {
		if len(k) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one key from flattened data")
	}
}
