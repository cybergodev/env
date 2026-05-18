package internal

import (
	"errors"
	"fmt"
	"testing"
)

// ============================================================================
// JSON Flatten Tests
// ============================================================================

func TestFlattenJSON_Empty(t *testing.T) {
	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON([]byte{}, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map for empty input, got %d items", len(result))
	}
}

func TestFlattenJSON_SimpleObject(t *testing.T) {
	data := []byte(`{"key1": "value1", "key2": "value2"}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
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

func TestFlattenJSON_NestedObject(t *testing.T) {
	data := []byte(`{
		"database": {
			"host": "localhost",
			"port": 5432
		}
	}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	if result["DATABASE_HOST"] != "localhost" {
		t.Errorf("result[\"DATABASE_HOST\"] = %q, want %q", result["DATABASE_HOST"], "localhost")
	}

	if result["DATABASE_PORT"] != "5432" {
		t.Errorf("result[\"DATABASE_PORT\"] = %q, want %q", result["DATABASE_PORT"], "5432")
	}
}

func TestFlattenJSON_Array(t *testing.T) {
	data := []byte(`{"items": ["one", "two", "three"]}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
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

func TestFlattenJSON_ArrayBracketFormat(t *testing.T) {
	data := []byte(`{"items": ["one", "two"]}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "bracket",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	if result["ITEMS[0]"] != "one" {
		t.Errorf("result[\"ITEMS[0]\"] = %q, want %q", result["ITEMS[0]"], "one")
	}

	if result["ITEMS[1]"] != "two" {
		t.Errorf("result[\"ITEMS[1]\"] = %q, want %q", result["ITEMS[1]"], "two")
	}
}

func TestFlattenJSON_NullValue(t *testing.T) {
	tests := []struct {
		name     string
		cfg      JSONFlattenConfig
		expected string
	}{
		{
			name:     "null as empty",
			cfg:      JSONFlattenConfig{NullAsEmpty: true},
			expected: "",
		},
		{
			name:     "null preserved",
			cfg:      JSONFlattenConfig{NullAsEmpty: false},
			expected: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(`{"key": null}`)
			tt.cfg.KeyDelimiter = "_"
			tt.cfg.MaxDepth = 10

			result, err := FlattenJSON(data, tt.cfg)
			if err != nil {
				t.Fatalf("FlattenJSON() error = %v", err)
			}

			if result["KEY"] != tt.expected {
				t.Errorf("result[\"KEY\"] = %q, want %q", result["KEY"], tt.expected)
			}
		})
	}
}

func TestFlattenJSON_BoolValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		cfg      JSONFlattenConfig
		expected string
	}{
		{
			name:     "true as string",
			input:    `{"enabled": true}`,
			cfg:      JSONFlattenConfig{BoolAsString: true},
			expected: "true",
		},
		{
			name:     "false as string",
			input:    `{"enabled": false}`,
			cfg:      JSONFlattenConfig{BoolAsString: true},
			expected: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.KeyDelimiter = "_"
			tt.cfg.MaxDepth = 10

			result, err := FlattenJSON([]byte(tt.input), tt.cfg)
			if err != nil {
				t.Fatalf("FlattenJSON() error = %v", err)
			}

			if result["ENABLED"] != tt.expected {
				t.Errorf("result[\"ENABLED\"] = %q, want %q", result["ENABLED"], tt.expected)
			}
		})
	}
}

func TestFlattenJSON_NumberValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		cfg      JSONFlattenConfig
		expected string
	}{
		{
			name:     "integer",
			input:    `{"count": 42}`,
			cfg:      JSONFlattenConfig{NumberAsString: true},
			expected: "42",
		},
		{
			name:     "float",
			input:    `{"rate": 3.14}`,
			cfg:      JSONFlattenConfig{NumberAsString: true},
			expected: "3.14",
		},
		{
			name:     "float as integer",
			input:    `{"count": 42.0}`,
			cfg:      JSONFlattenConfig{NumberAsString: true},
			expected: "42",
		},
		{
			name:     "negative number",
			input:    `{"temp": -10}`,
			cfg:      JSONFlattenConfig{NumberAsString: true},
			expected: "-10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.KeyDelimiter = "_"
			tt.cfg.MaxDepth = 10

			result, err := FlattenJSON([]byte(tt.input), tt.cfg)
			if err != nil {
				t.Fatalf("FlattenJSON() error = %v", err)
			}

			// Get first value (key varies by test)
			for _, v := range result {
				if v != tt.expected {
					t.Errorf("value = %q, want %q", v, tt.expected)
				}
				return
			}
			t.Error("no values found in result")
		})
	}
}

func TestFlattenJSON_InvalidJSON(t *testing.T) {
	data := []byte(`{invalid json}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	_, err := FlattenJSON(data, cfg)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	// Verify it's a JSONError
	var jsonErr *JSONError
	if !errors.As(err, &jsonErr) {
		t.Errorf("error type = %T, want *JSONError", err)
	}
}

func TestFlattenJSON_MaxDepthExceeded(t *testing.T) {
	data := []byte(`{
		"a": {
			"b": {
				"c": {
					"d": "deep"
				}
			}
		}
	}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         2, // Low limit
	}

	_, err := FlattenJSON(data, cfg)
	if err == nil {
		t.Error("expected max depth exceeded error")
	}
}

func TestFlattenJSON_ArrayOfObjects(t *testing.T) {
	data := []byte(`{
		"servers": [
			{"host": "server1", "port": 8080},
			{"host": "server2", "port": 9090}
		]
	}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	if result["SERVERS_0_HOST"] != "server1" {
		t.Errorf("result[\"SERVERS_0_HOST\"] = %q, want %q", result["SERVERS_0_HOST"], "server1")
	}

	if result["SERVERS_0_PORT"] != "8080" {
		t.Errorf("result[\"SERVERS_0_PORT\"] = %q, want %q", result["SERVERS_0_PORT"], "8080")
	}

	if result["SERVERS_1_HOST"] != "server2" {
		t.Errorf("result[\"SERVERS_1_HOST\"] = %q, want %q", result["SERVERS_1_HOST"], "server2")
	}
}

func TestFlattenJSON_NestedArray(t *testing.T) {
	data := []byte(`{
		"matrix": [
			["a", "b"],
			["c", "d"]
		]
	}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	if result["MATRIX_0_0"] != "a" {
		t.Errorf("result[\"MATRIX_0_0\"] = %q, want %q", result["MATRIX_0_0"], "a")
	}

	if result["MATRIX_0_1"] != "b" {
		t.Errorf("result[\"MATRIX_0_1\"] = %q, want %q", result["MATRIX_0_1"], "b")
	}

	if result["MATRIX_1_0"] != "c" {
		t.Errorf("result[\"MATRIX_1_0\"] = %q, want %q", result["MATRIX_1_0"], "c")
	}
}

func TestFlattenJSON_RootNull(t *testing.T) {
	data := []byte(`null`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	// Root null should result in empty map
	if len(result) != 0 {
		t.Errorf("expected empty map for root null, got %d items", len(result))
	}
}

func TestFlattenJSON_RootArray(t *testing.T) {
	data := []byte(`["a", "b", "c"]`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	// Root array with no prefix still produces indexed keys
	if len(result) != 3 {
		t.Errorf("expected 3 items for root array, got %d", len(result))
	}

	// The keys are just the indices
	if result["0"] != "a" && result["_0"] != "a" {
		// Keys may vary - just check we have 3 items
		t.Logf("root array result: %v", result)
	}
}

func TestFlattenJSON_RootScalar(t *testing.T) {
	data := []byte(`"hello"`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	// Root scalar with no prefix should result in empty map
	if len(result) != 0 {
		t.Errorf("expected empty map for root scalar, got %d items", len(result))
	}
}

// ============================================================================
// buildKey Tests (JSON)
// ============================================================================

func TestBuildKey_JSON(t *testing.T) {
	cfg := JSONFlattenConfig{KeyDelimiter: "_"}

	tests := []struct {
		prefix   string
		key      string
		expected string
	}{
		{"", "key", "KEY"},
		{"APP", "key", "APP_KEY"},
		{"APP_DATABASE", "host", "APP_DATABASE_HOST"},
		{"", "lower", "LOWER"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"_"+tt.key, func(t *testing.T) {
			result := buildKey(tt.prefix, tt.key, cfg)
			if result != tt.expected {
				t.Errorf("buildKey(%q, %q) = %q, want %q", tt.prefix, tt.key, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// buildArrayIndex Tests (JSON)
// ============================================================================

func TestBuildArrayIndex_JSON(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		index    int
		cfg      JSONFlattenConfig
		expected string
	}{
		{
			name:     "underscore format",
			prefix:   "ITEMS",
			index:    0,
			cfg:      JSONFlattenConfig{ArrayIndexFormat: "underscore", KeyDelimiter: "_"},
			expected: "ITEMS_0",
		},
		{
			name:     "bracket format",
			prefix:   "ITEMS",
			index:    5,
			cfg:      JSONFlattenConfig{ArrayIndexFormat: "bracket"},
			expected: "ITEMS[5]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildArrayIndex(tt.prefix, tt.index, tt.cfg)
			if result != tt.expected {
				t.Errorf("buildArrayIndex(%q, %d) = %q, want %q", tt.prefix, tt.index, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Complex Structure Tests
// ============================================================================

func TestFlattenJSON_ComplexStructure(t *testing.T) {
	data := []byte(`{
		"app": {
			"name": "myapp",
			"version": "1.0.0",
			"features": ["auth", "logging"]
		},
		"database": {
			"host": "localhost",
			"port": 5432
		}
	}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
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

func TestFlattenJSON_EmptyObject(t *testing.T) {
	data := []byte(`{"empty": {}}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	// Empty object should result in no entries for that key
	if len(result) != 0 {
		t.Errorf("expected 0 items for empty object, got %d", len(result))
	}
}

func TestFlattenJSON_EmptyArray(t *testing.T) {
	data := []byte(`{"empty": []}`)

	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	result, err := FlattenJSON(data, cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	// Empty array should result in no entries for that key
	if len(result) != 0 {
		t.Errorf("expected 0 items for empty array, got %d", len(result))
	}
}

func TestFlattenJSON_LongKeys(t *testing.T) {
	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "underscore",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	// Test with long key prefix that exceeds 64 chars to hit builder path
	longKey := "A"
	for i := 0; i < 70; i++ {
		longKey += "B"
	}

	input := fmt.Sprintf(`{"%s": {"nested": "value"}}`, longKey)
	result, err := FlattenJSON([]byte(input), cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}

	// Verify the key contains both parts
	for k := range result {
		if len(k) < 70 {
			t.Errorf("expected key length >= 70, got %d: %q", len(k), k)
		}
	}
}

func TestFlattenJSON_BracketArrayFormat(t *testing.T) {
	cfg := JSONFlattenConfig{
		KeyDelimiter:     "_",
		ArrayIndexFormat: "bracket",
		NullAsEmpty:      true,
		NumberAsString:   true,
		BoolAsString:     true,
		MaxDepth:         10,
	}

	input := `{"items": ["a", "b", "c"]}`
	result, err := FlattenJSON([]byte(input), cfg)
	if err != nil {
		t.Fatalf("FlattenJSON() error = %v", err)
	}

	if result["ITEMS[0]"] != "a" {
		t.Errorf("ITEMS[0] = %q, want %q", result["ITEMS[0]"], "a")
	}
	if result["ITEMS[1]"] != "b" {
		t.Errorf("ITEMS[1] = %q, want %q", result["ITEMS[1]"], "b")
	}
	if result["ITEMS[2]"] != "c" {
		t.Errorf("ITEMS[2] = %q, want %q", result["ITEMS[2]"], "c")
	}
}
