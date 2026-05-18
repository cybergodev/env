package internal

import (
	"fmt"
	"testing"
)

func TestParseLine(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		MaxKeyLength:   64,
		MaxValueLength: 1024,
	})
	a := NewAuditor(nil, nil, nil, false)
	e := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Mode:     ModeNone,
	})

	lp := NewLineParser(LineParserConfig{
		AllowExportPrefix: true,
		OverwriteExisting: true,
	}, v, a, e)

	tests := []struct {
		name      string
		line      string
		wantKey   string
		wantValue string
		wantErr   bool
	}{
		{
			name:      "simple assignment",
			line:      "KEY=value",
			wantKey:   "KEY",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name:      "export prefix",
			line:      "export KEY=value",
			wantKey:   "KEY",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name:      "double quoted",
			line:      `KEY="value with spaces"`,
			wantKey:   "KEY",
			wantValue: "value with spaces",
			wantErr:   false,
		},
		{
			name:      "single quoted",
			line:      `KEY='value with spaces'`,
			wantKey:   "KEY",
			wantValue: "value with spaces",
			wantErr:   false,
		},
		{
			name:      "colon separator",
			line:      "KEY:value",
			wantKey:   "KEY",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name:      "inline comment",
			line:      "KEY=value # comment",
			wantKey:   "KEY",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name:    "no assignment",
			line:    "NO_ASSIGNMENT_HERE",
			wantKey: "",
			wantErr: false,
		},
		{
			name:    "empty line",
			line:    "",
			wantKey: "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, err := lp.ParseLine(tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("ParseLine() key = %q, want %q", key, tt.wantKey)
			}
			if value != tt.wantValue {
				t.Errorf("ParseLine() value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

func TestParseDoubleQuoted(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple",
			input:    `"hello world"`,
			expected: "hello world",
			wantErr:  false,
		},
		{
			name:     "escape newline",
			input:    `"line1\nline2"`,
			expected: "line1\nline2",
			wantErr:  false,
		},
		{
			name:     "escape tab",
			input:    `"col1\tcol2"`,
			expected: "col1\tcol2",
			wantErr:  false,
		},
		{
			name:     "escape quote",
			input:    `"say \"hello\""`,
			expected: `say "hello"`,
			wantErr:  false,
		},
		{
			name:     "escape backslash",
			input:    `"path\\to\\file"`,
			expected: `path\to\file`,
			wantErr:  false,
		},
		{
			name:     "escape dollar",
			input:    `"$VAR"`,
			expected: "$VAR",
			wantErr:  false,
		},
		{
			name:    "unclosed quote",
			input:   `"unclosed`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDoubleQuoted(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDoubleQuoted() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("ParseDoubleQuoted() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseSingleQuoted(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple",
			input:    `'hello world'`,
			expected: "hello world",
			wantErr:  false,
		},
		{
			name:     "no escape processing",
			input:    `'line1\nline2'`,
			expected: "line1\\nline2",
			wantErr:  false,
		},
		{
			name:    "unclosed quote",
			input:   `'unclosed`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSingleQuoted(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSingleQuoted() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("ParseSingleQuoted() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTryParseYamlValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		ok       bool
	}{
		{
			name:     "boolean true",
			input:    "true",
			expected: "true",
			ok:       true,
		},
		{
			name:     "boolean false",
			input:    "false",
			expected: "false",
			ok:       true,
		},
		{
			name:     "null",
			input:    "null",
			expected: "",
			ok:       true,
		},
		{
			name:     "tilde null",
			input:    "~",
			expected: "",
			ok:       true,
		},
		{
			name:     "integer",
			input:    "123",
			expected: "123",
			ok:       true,
		},
		{
			name:     "float",
			input:    "3.14",
			expected: "3.14",
			ok:       true,
		},
		{
			name:     "negative number",
			input:    "-42",
			expected: "-42",
			ok:       true,
		},
		{
			name:     "regular string",
			input:    "hello",
			expected: "",
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := TryParseYamlValue(tt.input)
			if ok != tt.ok {
				t.Errorf("TryParseYamlValue() ok = %v, want %v", ok, tt.ok)
			}
			if result != tt.expected {
				t.Errorf("TryParseYamlValue() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestIsYamlNumber(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"-123", true},
		{"3.14", true},
		{"-3.14", true},
		{"1e10", true},
		{"1E10", true},
		{"+123", true},
		{"", false},
		{"abc", false},
		{"12abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsYamlNumber(tt.input); got != tt.want {
				t.Errorf("IsYamlNumber(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestKeysToUpper(t *testing.T) {
	input := map[string]string{
		"key1": "value1",
		"KEY2": "value2",
		"Key3": "value3",
	}

	result := KeysToUpper(input)

	// All keys should be uppercase
	for key := range result {
		if key != "KEY1" && key != "KEY2" && key != "KEY3" {
			t.Errorf("unexpected key: %q", key)
		}
	}
}

// TestParseLineBytesScannerBufferSafety tests that parsed values are correctly
// copied and not corrupted when the scanner buffer is reused.
// This is a regression test for the scanner buffer data corruption bug.
func TestParseLineBytesScannerBufferSafety(t *testing.T) {
	v := NewValidator(ValidatorConfig{
		MaxKeyLength:   64,
		MaxValueLength: 1024,
	})
	a := NewAuditor(nil, nil, nil, false)
	e := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Mode:     ModeNone,
	})

	lp := NewLineParser(LineParserConfig{
		AllowExportPrefix: true,
		OverwriteExisting: true,
	}, v, a, e)

	// Simulate scanner buffer reuse scenario
	// Create a buffer that will be "reused" between parses
	scannerBuffer := make([]byte, 1024)

	tests := []struct {
		name     string
		line1    string
		line2    string
		wantKey1 string
		wantVal1 string
		wantKey2 string
		wantVal2 string
	}{
		{
			name:     "unquoted values",
			line1:    "KEY1=value1",
			line2:    "KEY2=different_value",
			wantKey1: "KEY1",
			wantVal1: "value1",
			wantKey2: "KEY2",
			wantVal2: "different_value",
		},
		{
			name:     "double quoted values without escapes",
			line1:    `KEY1="value with spaces"`,
			line2:    `KEY2="another value"`,
			wantKey1: "KEY1",
			wantVal1: "value with spaces",
			wantKey2: "KEY2",
			wantVal2: "another value",
		},
		{
			name:     "single quoted values",
			line1:    `KEY1='single quoted'`,
			line2:    `KEY2='another single'`,
			wantKey1: "KEY1",
			wantVal1: "single quoted",
			wantKey2: "KEY2",
			wantVal2: "another single",
		},
		{
			name:     "double quoted with escapes",
			line1:    `KEY1="line1\nline2"`,
			line2:    `KEY2="line3\nline4"`,
			wantKey1: "KEY1",
			wantVal1: "line1\nline2",
			wantKey2: "KEY2",
			wantVal2: "line3\nline4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate first scan
			copy(scannerBuffer, tt.line1)
			line1 := scannerBuffer[:len(tt.line1)]

			key1, val1, err := lp.ParseLineBytes(line1)
			if err != nil {
				t.Fatalf("ParseLineBytes(line1) error = %v", err)
			}
			if key1 != tt.wantKey1 {
				t.Errorf("ParseLineBytes(line1) key = %q, want %q", key1, tt.wantKey1)
			}
			if val1 != tt.wantVal1 {
				t.Errorf("ParseLineBytes(line1) value = %q, want %q", val1, tt.wantVal1)
			}

			// Simulate buffer reuse (overwrite with second line)
			copy(scannerBuffer, tt.line2)
			line2 := scannerBuffer[:len(tt.line2)]

			// Parse second line
			key2, val2, err := lp.ParseLineBytes(line2)
			if err != nil {
				t.Fatalf("ParseLineBytes(line2) error = %v", err)
			}
			if key2 != tt.wantKey2 {
				t.Errorf("ParseLineBytes(line2) key = %q, want %q", key2, tt.wantKey2)
			}
			if val2 != tt.wantVal2 {
				t.Errorf("ParseLineBytes(line2) value = %q, want %q", val2, tt.wantVal2)
			}

			// CRITICAL: First value should still be intact after buffer reuse
			if val1 != tt.wantVal1 {
				t.Errorf("DATA CORRUPTION: val1 changed from %q to %q after buffer reuse",
					tt.wantVal1, val1)
			}
		})
	}
}

// ============================================================================
// KeysToUpperPooled Tests
// ============================================================================

func TestKeysToUpperPooled(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]bool
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: map[string]bool{},
		},
		{
			name:     "single key",
			input:    map[string]string{"key": "value"},
			expected: map[string]bool{"KEY": true},
		},
		{
			name:     "multiple keys",
			input:    map[string]string{"key1": "v1", "KEY2": "v2", "Key3": "v3"},
			expected: map[string]bool{"KEY1": true, "KEY2": true, "KEY3": true},
		},
		{
			name:     "with empty key",
			input:    map[string]string{"": "empty", "VALID": "value"},
			expected: map[string]bool{"VALID": true},
		},
		{
			name:     "mixed case keys",
			input:    map[string]string{"Database_Host": "localhost", "DATABASE_PORT": "5432"},
			expected: map[string]bool{"DATABASE_HOST": true, "DATABASE_PORT": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KeysToUpperPooled(tt.input)
			defer PutKeysToUpperMap(result)

			// Check all expected keys are present
			for k := range tt.expected {
				if !result[k] {
					t.Errorf("KeysToUpperPooled() missing key %q", k)
				}
			}

			// Check result has expected number of keys
			if len(result) != len(tt.expected) {
				t.Errorf("KeysToUpperPooled() returned %d keys, want %d", len(result), len(tt.expected))
			}
		})
	}
}

func TestKeysToUpperPooled_LargeMap(t *testing.T) {
	// Test with a map larger than MaxPooledMapSize to ensure it still works
	input := make(map[string]string, MaxPooledMapSize+10)
	for i := 0; i < MaxPooledMapSize+10; i++ {
		input[fmt.Sprintf("KEY_%d", i)] = fmt.Sprintf("value_%d", i)
	}

	result := KeysToUpperPooled(input)
	// Should still work, just won't be pooled on return
	if len(result) != MaxPooledMapSize+10 {
		t.Errorf("KeysToUpperPooled() returned %d keys, want %d", len(result), MaxPooledMapSize+10)
	}

	// Safe to call Put even for large maps
	PutKeysToUpperMap(result)
}

// mockValueValidator is a separate value validator for testing SetValueValidator.
type mockValueValidator struct {
	err error
}

func (m *mockValueValidator) ValidateValue(value string) error {
	return m.err
}

func TestParseValueBytes(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxKeyLength: 64})
	a := NewAuditor(nil, nil, nil, false)
	e := NewExpander(ExpanderConfig{MaxDepth: 5, Mode: ModeNone})
	lp := NewLineParser(LineParserConfig{}, v, a, e)

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"simple value", "hello", "hello", false},
		{"double quoted", `"hello world"`, "hello world", false},
		{"single quoted", "'hello world'", "hello world", false},
		{"with trailing comment", "value # comment", "value", false},
		{"with leading whitespace", "  value  ", "value", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lp.ParseValueBytes([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseValueBytes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseValueBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSingleQuotedBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid", "'hello'", "hello", false},
		{"empty quotes", "''", "", false},
		{"too short", "'", "", true},
		{"missing closing quote", "'hello", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSingleQuotedBytes([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSingleQuotedBytes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseSingleQuotedBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTryParseYamlValueBytes(t *testing.T) {
	tests := []struct {
		input     string
		wantVal   string
		wantMatch bool
	}{
		{"true", "true", true},
		{"True", "True", true},
		{"TRUE", "TRUE", true},
		{"false", "false", true},
		{"False", "False", true},
		{"null", "", true},
		{"Null", "", true},
		{"NULL", "", true},
		{"~", "", true},
		{"123", "123", true},
		{"3.14", "3.14", true},
		{"-42", "-42", true},
		{"1e10", "1e10", true},
		{"hello", "", false},
		{"", "", false},
		{"abc", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotVal, gotMatch := TryParseYamlValueBytes([]byte(tt.input))
			if gotMatch != tt.wantMatch {
				t.Errorf("TryParseYamlValueBytes(%q) match = %v, want %v", tt.input, gotMatch, tt.wantMatch)
			}
			if gotVal != tt.wantVal {
				t.Errorf("TryParseYamlValueBytes(%q) = %q, want %q", tt.input, gotVal, tt.wantVal)
			}
		})
	}
}

func TestSetValueValidator(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxKeyLength: 64})
	a := NewAuditor(nil, nil, nil, false)
	e := NewExpander(ExpanderConfig{MaxDepth: 5, Mode: ModeNone})

	lp := NewLineParser(LineParserConfig{}, v, a, e)

	// Set a separate value validator - just verify it doesn't panic
	lp.SetValueValidator(&mockValueValidator{err: nil})
	lp.SetValueValidator(&mockValueValidator{err: fmt.Errorf("bad value")})
}

// mockExpander is a non-*Expander implementation for testing expandAllUsingInterface.
type mockExpander struct {
	expandErr error
}

func (m *mockExpander) Expand(s string) (string, error) {
	if s == "$FAIL" {
		return "", m.expandErr
	}
	return "expanded_" + s, nil
}

func TestExpandAllUsingInterface(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxKeyLength: 64})
	a := NewAuditor(nil, nil, nil, false)

	t.Run("uses interface expander", func(t *testing.T) {
		me := &mockExpander{}
		lp := NewLineParser(LineParserConfig{}, v, a, me)

		vars := map[string]string{
			"KEY1": "$VAR1",
			"KEY2": "$VAR2",
		}

		result, err := lp.ExpandAll(vars)
		if err != nil {
			t.Fatalf("ExpandAll() error = %v", err)
		}
		if result["KEY1"] != "expanded_$VAR1" {
			t.Errorf("KEY1 = %q, want %q", result["KEY1"], "expanded_$VAR1")
		}
	})

	t.Run("no expansion needed", func(t *testing.T) {
		me := &mockExpander{}
		lp := NewLineParser(LineParserConfig{}, v, a, me)

		vars := map[string]string{
			"KEY1": "plain_value",
			"KEY2": "another_value",
		}

		result, err := lp.ExpandAll(vars)
		if err != nil {
			t.Fatalf("ExpandAll() error = %v", err)
		}
		// Should return the same map since no expansion needed
		if result["KEY1"] != "plain_value" {
			t.Errorf("KEY1 = %q, want %q", result["KEY1"], "plain_value")
		}
	})

	t.Run("expansion error", func(t *testing.T) {
		me := &mockExpander{expandErr: fmt.Errorf("expansion failed")}
		lp := NewLineParser(LineParserConfig{}, v, a, me)

		vars := map[string]string{
			"KEY1": "$FAIL",
		}

		_, err := lp.ExpandAll(vars)
		if err == nil {
			t.Error("ExpandAll() should return error on expansion failure")
		}
	})
}

func TestPutKeysToUpperMap_Nil(t *testing.T) {
	// Should not panic with nil
	PutKeysToUpperMap(nil)
}
