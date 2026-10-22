package env

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// parseBool Tests
// ============================================================================

func TestParseBool(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
		wantErr  bool
	}{
		{"", false, false},
		{"0", false, false},
		{"1", true, false},
		{"true", true, false},
		{"TRUE", true, false},
		{"false", false, false},
		{"FALSE", false, false},
		{"yes", true, false},
		{"Yes", true, false},
		{"YES", true, false},
		{"no", false, false},
		{"No", false, false},
		{"NO", false, false},
		{"on", true, false},
		{"On", true, false},
		{"ON", true, false},
		{"off", false, false},
		{"Off", false, false},
		{"OFF", false, false},
		{"enabled", true, false},
		{"Enabled", true, false},
		{"ENABLED", true, false},
		{"disabled", false, false},
		{"Disabled", false, false},
		{"DISABLED", false, false},
		{"y", false, true},
		{"Y", false, true},
		{"n", false, true},
		{"N", false, true},
		{"invalid", false, true},
		// Mixed case is the point of EqualFoldASCII's case folding.
		{"TrUe", true, false},
		{"yEs", true, false},
		// Leading/trailing whitespace around a valid token is trimmed first.
		{"  true  ", true, false},
		{"  ", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseBool(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseBool(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if result != tt.expected {
				t.Errorf("parseBool(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// parseDuration Tests
// ============================================================================

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"", 0, true},
		{"0", 0, false},
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"1h", time.Hour, false},
		{"1.5h", 90 * time.Minute, false},
		{"invalid", 0, true},
		{"  30s", 30 * time.Second, false},
		// Negative durations are valid time.ParseDuration input.
		{"-30s", -30 * time.Second, false},
		// Bare numbers lack a unit (only "0" is unit-less) and error out.
		{"3000", 0, true},
		// Whitespace-only input trims to "" which time.ParseDuration rejects.
		{"   ", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if result != tt.expected {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// parseInt Tests
// ============================================================================

func TestParseInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"", 0, true},
		{"0", 0, false},
		{"42", 42, false},
		{"-42", -42, false},
		{"123", 123, false},
		{"invalid", 0, true},
		{"  42", 42, false},
		// int64 overflow: MaxInt64 + 1.
		{"9223372036854775808", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseInt(tt.input, 64)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInt(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if result != tt.expected {
				t.Errorf("parseInt(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// parseUint Tests
// ============================================================================

func TestParseUint(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
		wantErr  bool
	}{
		// Empty errors here (unlike parseBool, which returns false for "").
		{"", 0, true},
		{"0", 0, false},
		{"42", 42, false},
		{"18446744073709551615", 18446744073709551615, false}, // uint64 max
		{"-1", 0, true},  // negatives are invalid unsigned input
		{"1.5", 0, true}, // not base-10 integer syntax
		{"invalid", 0, true},
		{"18446744073709551616", 0, true}, // 2^64: one past uint64 max
		{"  42", 42, false},               // trimmed before parsing
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseUint(tt.input, 64)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseUint(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if result != tt.expected {
				t.Errorf("parseUint(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// parseFloat64 Tests
// ============================================================================

func TestParseFloat64(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
		wantErr  bool
	}{
		{"", 0, true},
		{"3.14", 3.14, false},
		{"-2.5", -2.5, false},
		{"42", 42, false}, // bare integer parses fine
		// Pinned: "1e999" overflows float64, and strconv.ParseFloat reports
		// ErrRange (returning ±Inf alongside the error) — parseFloat64
		// surfaces that as a *ValidationError, not as ±Inf.
		{"1e999", 0, true},
		{"invalid", 0, true},
		{"  3.5", 3.5, false}, // trimmed before parsing
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseFloat64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFloat64(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if result != tt.expected {
				t.Errorf("parseFloat64(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Marshal With Format Tests
// ============================================================================

// TestMarshalWithFormat is the single table covering format selection
// (dotenv/json/yaml), map and struct inputs, always-sorted key output, and
// unsupported input errors. Marshal defaults to FormatEnv, so one explicit
// FormatEnv row covers both the no-argument and the explicit call path.
//
// When sortedKeys is non-empty, each key must occur at a strictly increasing
// index — a real ordering assertion per format. JSON nests underscore keys
// one level ("A_KEY" -> {"A": {"KEY": ...}}), so its sortedKeys are the
// top-level object keys, which encoding/json emits in sorted map-key order.
func TestMarshalWithFormat(t *testing.T) {
	type utAppConfig struct {
		Name string `env:"APP_NAME"`
		Port int    `env:"APP_PORT"`
	}

	mapInput := map[string]string{
		"APP_NAME": "myapp",
		"APP_PORT": "8080",
		"DEBUG":    "true",
	}
	structInput := utAppConfig{Name: "myapp", Port: 8080}
	sortedInput := map[string]string{
		"Z_KEY": "z",
		"A_KEY": "a",
		"M_KEY": "m",
	}

	tests := []struct {
		name        string
		input       any
		format      FileFormat
		wantSubstrs []string
		sortedKeys  []string // must occur at strictly increasing indexes
		wantErr     bool
	}{
		{
			name:        "map to dotenv",
			input:       mapInput,
			format:      FormatEnv,
			wantSubstrs: []string{"APP_NAME=myapp"},
		},
		{
			name:        "map to json nests underscore keys",
			input:       mapInput,
			format:      FormatJSON,
			wantSubstrs: []string{`"APP"`, `"NAME"`},
		},
		{
			name:        "map to yaml",
			input:       mapInput,
			format:      FormatYAML,
			wantSubstrs: []string{"APP_NAME: myapp"},
		},
		{
			name:        "struct to dotenv",
			input:       structInput,
			format:      FormatEnv,
			wantSubstrs: []string{"APP_NAME=myapp", "APP_PORT=8080"},
		},
		{
			name:        "struct to json",
			input:       structInput,
			format:      FormatJSON,
			wantSubstrs: []string{`"NAME"`, `"myapp"`},
		},
		{
			name:        "struct to yaml",
			input:       structInput,
			format:      FormatYAML,
			wantSubstrs: []string{"APP_NAME: myapp"},
		},
		{
			name:        "struct pointer to dotenv",
			input:       &structInput,
			format:      FormatEnv,
			wantSubstrs: []string{"APP_NAME=myapp"},
		},
		{
			name:       "dotenv keys are sorted",
			input:      sortedInput,
			format:     FormatEnv,
			sortedKeys: []string{"A_KEY", "M_KEY", "Z_KEY"},
		},
		{
			name:       "json keys are sorted",
			input:      sortedInput,
			format:     FormatJSON,
			sortedKeys: []string{`"A"`, `"M"`, `"Z"`},
		},
		{
			name:       "yaml keys are sorted",
			input:      sortedInput,
			format:     FormatYAML,
			sortedKeys: []string{"A_KEY:", "M_KEY:", "Z_KEY:"},
		},
		{
			name:    "nil input errors",
			input:   nil,
			format:  FormatEnv,
			wantErr: true,
		},
		{
			// toMap falls through to MarshalStruct, which rejects non-struct
			// kinds ("expected struct or pointer to struct").
			name:    "int input errors",
			input:   42,
			format:  FormatEnv,
			wantErr: true,
		},
		{
			name:    "slice input errors",
			input:   []int{1},
			format:  FormatEnv,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Marshal(tt.input, tt.format)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for _, sub := range tt.wantSubstrs {
				if !strings.Contains(result, sub) {
					t.Errorf("Marshal() output missing %q, got:\n%s", sub, result)
				}
			}
			prevIdx := -1
			for _, key := range tt.sortedKeys {
				idx := strings.Index(result, key)
				if idx < 0 {
					t.Errorf("Marshal() output missing sorted key %q, got:\n%s", key, result)
					return
				}
				if idx <= prevIdx {
					t.Errorf("Marshal() sorted key %q at index %d must come after the previous key at index %d, got:\n%s", key, idx, prevIdx, result)
					return
				}
				prevIdx = idx
			}
		})
	}
}

// ============================================================================
// IsMarshalError Tests
// ============================================================================

func TestIsMarshalError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "is marshal error",
			err:      &MarshalError{Field: "test", Message: "test"},
			expected: true,
		},
		{
			name:     "is validation error",
			err:      &ValidationError{Field: "test", Message: "test"},
			expected: false,
		},
		{
			name:     "is other error",
			err:      errors.New("test"),
			expected: false,
		},
		{
			name:     "is nil",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsMarshalError(tt.err)
			if result != tt.expected {
				t.Errorf("IsMarshalError() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// ============================================================================
// MarshalStruct Tests
// ============================================================================

// testMarshaler implements the Marshaler interface for testing.
type testMarshaler struct {
	data string
}

func (m *testMarshaler) MarshalEnv() ([]byte, error) {
	return []byte(m.data), nil
}

func TestToMap(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "map[string]string passthrough",
			input: map[string]string{"KEY": "value"},
			want:  map[string]string{"KEY": "value"},
		},
		{
			name:    "nil input",
			input:   nil,
			wantErr: true,
		},
		{
			name:    "nil map pointer",
			input:   (*map[string]string)(nil),
			wantErr: true,
		},
		{
			name: "valid map pointer",
			input: func() interface{} {
				m := map[string]string{"KEY": "value"}
				return &m
			}(),
			want: map[string]string{"KEY": "value"},
		},
		{
			name: "struct input",
			input: struct {
				Name string `env:"NAME"`
			}{Name: "test"},
			want: map[string]string{"NAME": "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toMap(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("toMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for k, v := range tt.want {
					if got[k] != v {
						t.Errorf("toMap()[%q] = %q, want %q", k, got[k], v)
					}
				}
			}
		})
	}
}

func TestMarshalStruct_Marshaler(t *testing.T) {
	t.Run("implements Marshaler interface", func(t *testing.T) {
		m := &testMarshaler{data: "KEY=value\nPORT=8080"}
		result, err := MarshalStruct(m)
		if err != nil {
			t.Fatalf("MarshalStruct() error = %v", err)
		}
		if result["KEY"] != "value" {
			t.Errorf("result[\"KEY\"] = %q, want %q", result["KEY"], "value")
		}
		if result["PORT"] != "8080" {
			t.Errorf("result[\"PORT\"] = %q, want %q", result["PORT"], "8080")
		}
	})
}

func TestMarshalStruct(t *testing.T) {
	t.Run("basic struct", func(t *testing.T) {
		type TestMarshalConfig struct {
			Name string `env:"NAME"`
			Port int    `env:"PORT"`
		}

		c := TestMarshalConfig{
			Name: "test",
			Port: 8080,
		}

		result, err := MarshalStruct(&c)
		if err != nil {
			t.Fatalf("MarshalStruct() error = %v", err)
		}

		if result["NAME"] != "test" {
			t.Errorf("result[\"NAME\"] = %q, want %q", result["NAME"], "test")
		}
		if result["PORT"] != "8080" {
			t.Errorf("result[\"PORT\"] = %q, want %q", result["PORT"], "8080")
		}
	})

	t.Run("empty struct", func(t *testing.T) {
		type TestEmptyMarshalConfig struct{}

		result, err := MarshalStruct(&TestEmptyMarshalConfig{})
		if err != nil {
			t.Fatalf("MarshalStruct() error = %v", err)
		}
		if len(result) != 0 {
			t.Errorf("result = %v, want empty map", result)
		}
	})
}

// ============================================================================
// UnmarshalInto Tests
// ============================================================================

func TestUnmarshalInto(t *testing.T) {
	t.Run("basic struct", func(t *testing.T) {
		type TestUnmarshalConfig struct {
			Name string `env:"NAME"`
			Port int    `env:"PORT"`
		}

		data := map[string]string{
			"NAME": "test",
			"PORT": "8080",
		}

		var c TestUnmarshalConfig
		err := UnmarshalInto(data, &c)
		if err != nil {
			t.Fatalf("UnmarshalInto() error = %v", err)
		}
		if c.Name != "test" {
			t.Errorf("c.Name = %q, want %q", c.Name, "test")
		}
		if c.Port != 8080 {
			t.Errorf("c.Port = %d, want %d", c.Port, 8080)
		}
	})

	t.Run("pointer to nil pointer", func(t *testing.T) {
		// Both a nil map and a populated map hit the same guard in
		// UnmarshalInto: the destination's element is a nil pointer, not a
		// struct ("expected pointer to struct").
		type TestUnmarshalConfigNil struct {
			Name string `env:"NAME"`
		}
		for _, data := range []map[string]string{nil, {"NAME": "test"}} {
			var c *TestUnmarshalConfigNil
			err := UnmarshalInto(data, &c)
			if err == nil {
				t.Error("UnmarshalInto() should return error for pointer to nil pointer")
			}
		}
	})

	t.Run("nil map into valid struct pointer", func(t *testing.T) {
		// Pinned: a nil map is NOT an error — every field lookup misses and
		// the struct keeps its zero value (UnmarshalInto → StructInto).
		type TestUnmarshalConfigEmptyMap struct {
			Name string `env:"NAME"`
		}
		var c TestUnmarshalConfigEmptyMap
		err := UnmarshalInto(nil, &c)
		if err != nil {
			t.Errorf("UnmarshalInto(nil, &c) error = %v, want nil", err)
		}
		if c.Name != "" {
			t.Errorf("c.Name = %q, want zero value", c.Name)
		}
	})

	t.Run("non-pointer", func(t *testing.T) {
		data := map[string]string{
			"NAME": "test",
		}
		var c int
		err := UnmarshalInto(data, c)
		if err == nil {
			t.Error("UnmarshalInto() should return error for non-pointer")
		}
	})

	t.Run("non-struct pointer", func(t *testing.T) {
		data := map[string]string{}
		var c string
		err := UnmarshalInto(data, &c)
		if err == nil {
			t.Error("UnmarshalInto() should return error for pointer to non-struct")
		}
	})

	t.Run("narrow integer field rejects out-of-range value", func(t *testing.T) {
		// Regression: values were parsed at 64-bit width and then silently
		// truncated by reflect (300 → 44 in an int8 field). They must error.
		type NarrowConfig struct {
			Port int8 `env:"PORT"`
		}
		var c NarrowConfig
		err := UnmarshalInto(map[string]string{"PORT": "300"}, &c)
		if err == nil {
			t.Fatalf("UnmarshalInto() succeeded with out-of-range int8 value, Port = %d", c.Port)
		}
		if c.Port != 0 {
			t.Errorf("Port = %d on failure, want untouched zero value", c.Port)
		}
	})
}

// TestMarshalYAMLRoundTripFidelity verifies that string values which look like
// YAML scalars (bools, null, numbers) and values with trailing whitespace
// survive a Marshal→UnmarshalMap round trip through FormatYAML unchanged.
// Regression: they were emitted unquoted, so the YAML reader coerced them
// ("null" → "", "true" → "true"/bool normalization, trailing space stripped).
func TestMarshalYAMLRoundTripFidelity(t *testing.T) {
	original := map[string]string{
		"MODE":       "null",
		"EMPTY_WORD": "~",
		"FLAG":       "true",
		"OTHER_FLAG": "false",
		"NUMBER":     "8080",
		"RATIO":      "3.14",
		"SPACED":     "trailing ",
	}
	data, err := Marshal(original, FormatYAML)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	roundTripped, err := UnmarshalMap(data, FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalMap() error = %v", err)
	}

	for key, want := range original {
		if got := roundTripped[key]; got != want {
			t.Errorf("round trip changed %s: got %q, want %q (marshaled as:\n%s)", key, got, want, data)
		}
	}
}

// ============================================================================
// UnmarshalStruct (String Version) Tests
// ============================================================================
func TestUnmarshalStructFromString(t *testing.T) {
	// One shared destination type: SERVER_HOST/SERVER_PORT match the
	// flattened keys of the nested JSON/YAML documents below.
	type utServerConfig struct {
		Host string `env:"SERVER_HOST"`
		Port int    `env:"SERVER_PORT"`
	}

	tests := []struct {
		name     string
		data     string
		format   FileFormat
		wantHost string
		wantPort int
	}{
		{
			name:     "env format to struct",
			data:     "SERVER_HOST=localhost\nSERVER_PORT=8080",
			format:   FormatEnv,
			wantHost: "localhost",
			wantPort: 8080,
		},
		{
			name:     "json format to struct",
			data:     `{"server": {"host": "localhost", "port": 8080}}`,
			format:   FormatJSON,
			wantHost: "localhost",
			wantPort: 8080,
		},
		{
			name:     "yaml format to struct",
			data:     "server:\n  host: localhost\n  port: 8080\n",
			format:   FormatYAML,
			wantHost: "localhost",
			wantPort: 8080,
		},
		{
			// Port absent from the data stays at its zero value.
			name:     "auto-detect json",
			data:     `{"server": {"host": "auto-detected"}}`,
			format:   FormatAuto,
			wantHost: "auto-detected",
			wantPort: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg utServerConfig
			if err := UnmarshalStruct(tt.data, &cfg, tt.format); err != nil {
				t.Fatalf("UnmarshalStruct() error = %v", err)
			}
			if cfg.Host != tt.wantHost {
				t.Errorf("cfg.Host = %q, want %q", cfg.Host, tt.wantHost)
			}
			if cfg.Port != tt.wantPort {
				t.Errorf("cfg.Port = %d, want %d", cfg.Port, tt.wantPort)
			}
		})
	}
}

// ============================================================================
// UnmarshalMap Tests
// ============================================================================

func TestUnmarshalMap(t *testing.T) {
	// deepYAML nests two levels beyond defaultStructuredMaxDepth so the
	// flatten step must reject it regardless of the default's exact value.
	var deepYAML strings.Builder
	for i := range defaultStructuredMaxDepth + 2 {
		deepYAML.WriteString(strings.Repeat("  ", i))
		deepYAML.WriteString("key:\n")
	}
	deepYAML.WriteString(strings.Repeat("  ", defaultStructuredMaxDepth+2))
	deepYAML.WriteString("leaf: deep")

	// deepJSON is the JSON counterpart: each '{"a":' opens one bracket, so
	// the nesting exceeds defaultStructuredMaxDepth by two.
	var deepJSON strings.Builder
	deepJSON.WriteString(strings.Repeat(`{"a":`, defaultStructuredMaxDepth+2))
	deepJSON.WriteString("1")
	deepJSON.WriteString(strings.Repeat("}", defaultStructuredMaxDepth+2))

	tests := []struct {
		name    string
		data    string
		formats []FileFormat
		want    map[string]string
		wantLen int // expected len(result); defaults to len(want)
		wantErr bool
	}{
		{
			name: "env format (default)",
			data: "KEY=value\nPORT=8080",
			want: map[string]string{"KEY": "value", "PORT": "8080"},
		},
		{
			name:    "empty env input yields empty map",
			data:    "",
			wantLen: 0,
		},
		{
			name:    "json format",
			data:    `{"database": {"host": "localhost", "port": 5432}}`,
			formats: []FileFormat{FormatJSON},
			want:    map[string]string{"DATABASE_HOST": "localhost", "DATABASE_PORT": "5432"},
		},
		{
			// Boundary: empty JSON input short-circuits to an empty map.
			name:    "empty json input yields empty map",
			data:    "",
			formats: []FileFormat{FormatJSON},
			wantLen: 0,
		},
		{
			name:    "invalid json returns error",
			data:    `{invalid json}`,
			formats: []FileFormat{FormatJSON},
			wantErr: true,
		},
		{
			name:    "yaml format",
			data:    "database:\n  host: localhost\n  port: 5432\n",
			formats: []FileFormat{FormatYAML},
			want:    map[string]string{"DATABASE_HOST": "localhost", "DATABASE_PORT": "5432"},
		},
		{
			// Boundary: empty YAML input short-circuits to an empty map.
			name:    "empty yaml input yields empty map",
			data:    "",
			formats: []FileFormat{FormatYAML},
			wantLen: 0,
		},
		{
			// Boundary: nesting beyond the default structured depth errors.
			name:    "yaml exceeding depth limit returns error",
			data:    deepYAML.String(),
			formats: []FileFormat{FormatYAML},
			wantErr: true,
		},
		{
			// Boundary: the JSON counterpart of the YAML depth row above.
			name:    "json exceeding depth limit returns error",
			data:    deepJSON.String(),
			formats: []FileFormat{FormatJSON},
			wantErr: true,
		},
		{
			// Pinned: a top-level JSON array flattens to bare indexed keys —
			// the underscore array-index join applied to an empty prefix.
			name:    "top-level json array flattens to indexed keys",
			data:    `[1, 2, 3]`,
			formats: []FileFormat{FormatJSON},
			want:    map[string]string{"_0": "1", "_1": "2", "_2": "3"},
		},
		{
			name:    "auto-detect json",
			data:    `{"key": "value"}`,
			formats: []FileFormat{FormatAuto},
			want:    map[string]string{"KEY": "value"},
		},
		{
			name:    "auto-detect yaml",
			data:    "key: value\nother: test",
			formats: []FileFormat{FormatAuto},
			want:    map[string]string{"KEY": "value", "OTHER": "test"},
		},
		{
			name:    "auto-detect env (default)",
			data:    "KEY=value\nOTHER=test",
			formats: []FileFormat{FormatAuto},
			want:    map[string]string{"KEY": "value", "OTHER": "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := UnmarshalMap(tt.data, tt.formats...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for key, want := range tt.want {
				if result[key] != want {
					t.Errorf("result[%q] = %q, want %q", key, result[key], want)
				}
			}
			// wantLen defaults to len(want) so a row listing expected keys
			// also pins the result size.
			wantLen := tt.wantLen
			if wantLen == 0 {
				wantLen = len(tt.want)
			}
			if len(result) != wantLen {
				t.Errorf("len(result) = %d, want %d (got %v)", len(result), wantLen, result)
			}
		})
	}
}

// ============================================================================
// detectDataFormat Tests
// ============================================================================

func TestDetectDataFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected FileFormat
	}{
		{"json object", `{"key": "value"}`, FormatJSON},
		{"json array", `[1, 2, 3]`, FormatJSON},
		// A '{' prefix decides JSON before any validity check.
		{"json prefix decides regardless of validity", "{invalid", FormatJSON},
		{"yaml with colon", "key: value", FormatYAML},
		{"yaml with list", "- item1\n- item2", FormatYAML},
		// A colon without a trailing space is not the YAML "key: value" pattern.
		{"colon without space is env", "key:value", FormatEnv},
		// A dash without a trailing space is not a YAML list item.
		{"dash without space is env", "-item", FormatEnv},
		{"env format", "KEY=value", FormatEnv},
		// Detection stops at the first valid line: an env line first means
		// later YAML lines never influence the result.
		{"env first line wins over later yaml", "KEY=value\nhost: localhost", FormatEnv},
		{"env with comment", "# comment\nKEY=value", FormatEnv},
		{"empty string", "", FormatEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectDataFormat(tt.input)
			if result != tt.expected {
				t.Errorf("detectDataFormat(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Mock Validators for Testing (from adapters_test.go)
// ============================================================================

// mockLineKeyValidator only implements ValidateKey (LineKeyValidator).
// Used for testing the validatorInterfaceWrapper with minimal interface.
type mockLineKeyValidator struct {
	err error
}

func (m *mockLineKeyValidator) ValidateKey(key string) error {
	return m.err
}

// mockLineKeyValueValidator implements ValidateKey and ValidateValue but not ValidateRequired.
type mockLineKeyValueValidator struct {
	keyErr   error
	valueErr error
}

func (v *mockLineKeyValueValidator) ValidateKey(key string) error {
	return v.keyErr
}

func (v *mockLineKeyValueValidator) ValidateValue(value string) error {
	return v.valueErr
}

// fullMockValidator implements the complete Validator interface.
type fullMockValidator struct {
	keyErr      error
	valueErr    error
	requiredErr error
}

func (f *fullMockValidator) ValidateKey(key string) error {
	return f.keyErr
}

func (f *fullMockValidator) ValidateValue(value string) error {
	return f.valueErr
}

func (f *fullMockValidator) ValidateRequired(keys map[string]bool) error {
	return f.requiredErr
}

// minimalMockValidator implements Validator but ValidateRequired returns ErrValidateRequiredUnsupported.
type minimalMockValidator struct {
	keyErr   error
	valueErr error
}

func (m *minimalMockValidator) ValidateKey(key string) error {
	return m.keyErr
}

func (m *minimalMockValidator) ValidateValue(value string) error {
	return m.valueErr
}

func (m *minimalMockValidator) ValidateRequired(keys map[string]bool) error {
	return ErrValidateRequiredUnsupported
}

// ============================================================================
// validatorInterfaceWrapper Tests
// ============================================================================

func TestValidatorInterfaceWrapper_ValidateKey(t *testing.T) {
	tests := []struct {
		name    string
		wrapper *validatorInterfaceWrapper
		key     string
		wantErr bool
	}{
		{
			name:    "passes validation",
			wrapper: &validatorInterfaceWrapper{&mockLineKeyValidator{}},
			key:     "TEST_KEY",
			wantErr: false,
		},
		{
			name:    "fails validation",
			wrapper: &validatorInterfaceWrapper{&mockLineKeyValidator{err: errors.New("invalid key")}},
			key:     "BAD_KEY",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.wrapper.ValidateKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatorInterfaceWrapper_ValidateValue(t *testing.T) {
	t.Run("validator with LineValueValidator delegates", func(t *testing.T) {
		// mockLineKeyValueValidator implements both LineKeyValidator and LineValueValidator
		val := &mockLineKeyValueValidator{valueErr: errors.New("bad value")}
		wrapper := &validatorInterfaceWrapper{val}

		err := wrapper.ValidateValue("bad")
		if err == nil {
			t.Error("ValidateValue() should delegate to LineValueValidator")
		}
		if err.Error() != "bad value" {
			t.Errorf("ValidateValue() error = %v, want 'bad value'", err)
		}
	})

	t.Run("validator without LineValueValidator returns nil", func(t *testing.T) {
		wrapper := &validatorInterfaceWrapper{&mockLineKeyValidator{}}
		err := wrapper.ValidateValue("any value")
		if err != nil {
			t.Errorf("ValidateValue() should return nil when LineValueValidator not implemented, got %v", err)
		}
	})
}

func TestValidatorInterfaceWrapper_ValidateRequired(t *testing.T) {
	t.Run("returns ErrValidateRequiredUnsupported", func(t *testing.T) {
		wrapper := &validatorInterfaceWrapper{&mockLineKeyValidator{}}
		keys := map[string]bool{"KEY1": true, "KEY2": true}

		err := wrapper.ValidateRequired(keys)
		if !errors.Is(err, ErrValidateRequiredUnsupported) {
			t.Errorf("ValidateRequired() error = %v, want ErrValidateRequiredUnsupported", err)
		}
	})
}

// ============================================================================
// Integration Tests with ComponentFactory
// ============================================================================

// TestComponentFactory_Validator covers the three validator wirings in one
// table: a minimal custom validator (ValidateRequired unsupported), a full
// custom validator (delegates), and the built-in validator with required
// keys (reports the missing key itself).
func TestComponentFactory_Validator(t *testing.T) {
	tests := []struct {
		name            string
		cfgFn           func() Config
		wantUnsupported bool   // ValidateRequired returns ErrValidateRequiredUnsupported
		wantRequiredMsg string // non-empty: exact error message; empty: any non-nil error
	}{
		{
			name: "minimal custom validator",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.CustomValidator = &minimalMockValidator{}
				return cfg
			},
			wantUnsupported: true,
		},
		{
			name: "full custom validator",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.CustomValidator = &fullMockValidator{requiredErr: errors.New("missing keys")}
				return cfg
			},
			wantRequiredMsg: "missing keys",
		},
		{
			name: "built-in validator with required keys",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.RequiredKeys = []string{"REQUIRED_KEY"}
				return cfg
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfgFn()
			factory := cfg.buildComponentFactory()
			defer func() { _ = factory.Close() }() // best-effort cleanup

			validator := factory.Validator()

			// ValidateKey passes under every wiring.
			if err := validator.ValidateKey("TEST_KEY"); err != nil {
				t.Errorf("ValidateKey() error = %v, want nil", err)
			}

			err := validator.ValidateRequired(map[string]bool{"OTHER_KEY": true})
			if got := errors.Is(err, ErrValidateRequiredUnsupported); got != tt.wantUnsupported {
				t.Errorf("ValidateRequired() = %v, ErrValidateRequiredUnsupported match = %v, want %v", err, got, tt.wantUnsupported)
			}
			switch {
			case tt.wantUnsupported:
				if err == nil {
					t.Error("ValidateRequired() should return an error")
				}
			case tt.wantRequiredMsg != "":
				if err == nil || err.Error() != tt.wantRequiredMsg {
					t.Errorf("ValidateRequired() = %v, want error %q", err, tt.wantRequiredMsg)
				}
			default:
				if err == nil {
					t.Error("ValidateRequired() should fail for missing required key")
				}
			}
		})
	}
}

// ============================================================================
// Integration Tests with Loader
// ============================================================================

func TestLoader_New_WithMinimalCustomValidatorAndRequiredKeys(t *testing.T) {
	fs := newTestFileSystem()
	fs.files["test.env"] = "EXISTING_KEY=value"

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = []string{"test.env"}
	cfg.RequiredKeys = []string{"REQUIRED_KEY"}
	cfg.CustomValidator = &minimalMockValidator{} // Returns ErrValidateRequiredUnsupported

	// New() should fail because ValidateRequired is called during file parsing
	// and minimalMockValidator returns ErrValidateRequiredUnsupported
	_, err := New(cfg)
	if !errors.Is(err, ErrValidateRequiredUnsupported) {
		t.Errorf("New() error = %v, want ErrValidateRequiredUnsupported", err)
	}
}

func TestLoader_New_WithMinimalCustomValidatorNoFiles(t *testing.T) {
	// When no files are loaded, ValidateRequired is not called during New()
	cfg := DefaultConfig()
	cfg.Filenames = []string{} // No files to load
	cfg.CustomValidator = &minimalMockValidator{}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Validate() should still return ErrValidateRequiredUnsupported
	err = loader.Validate()
	if !errors.Is(err, ErrValidateRequiredUnsupported) {
		t.Errorf("Validate() error = %v, want ErrValidateRequiredUnsupported", err)
	}
}

func TestLoader_Validate_WithFullCustomValidator(t *testing.T) {
	fs := newTestFileSystem()
	fs.files["test.env"] = "EXISTING_KEY=value"

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = []string{"test.env"}
	cfg.RequiredKeys = []string{"REQUIRED_KEY"}
	cfg.CustomValidator = &fullMockValidator{} // Implements full Validator

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// Validate() should NOT return ErrValidateRequiredUnsupported
	// (it may return a different error from the fullMockValidator, but not the unsupported error)
	err = loader.Validate()
	if errors.Is(err, ErrValidateRequiredUnsupported) {
		t.Errorf("Validate() should not return ErrValidateRequiredUnsupported for full validator, got %v", err)
	}
}

// ============================================================================
// ErrValidateRequiredUnsupported Tests
// ============================================================================

func TestErrValidateRequiredUnsupported(t *testing.T) {
	errMsg := ErrValidateRequiredUnsupported.Error()
	if !strings.Contains(errMsg, "ValidateRequired") {
		t.Errorf("Error message should mention ValidateRequired, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "Validator") {
		t.Errorf("Error message should mention Validator interface, got: %s", errMsg)
	}
}

// ============================================================================
// Auditor Tests (the built-in *internal.Auditor is exposed directly as
// FullAuditLogger — no adapter wrapper)
// ============================================================================

// utRecordingAuditHandler is an AuditHandler that records every event it
// receives. The mutex guards the slice because audit calls arrive from
// loader paths that may run concurrently.
type utRecordingAuditHandler struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (h *utRecordingAuditHandler) Log(event AuditEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
	return nil
}

func (h *utRecordingAuditHandler) Close() error {
	return nil
}

// recorded returns a snapshot of the events received so far.
func (h *utRecordingAuditHandler) recorded() []AuditEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]AuditEvent(nil), h.events...)
}

func TestAuditor_IntegrationWithLoader(t *testing.T) {
	fs := newTestFileSystem()
	fs.files[".env"] = "KEY=value"

	handler := &utRecordingAuditHandler{}

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.AuditEnabled = true
	cfg.AuditHandler = handler

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if loader.GetString("KEY") != "value" {
		t.Errorf("GetString(\"KEY\") = %q, want %q", loader.GetString("KEY"), "value")
	}

	// Audit must actually flow through the custom handler: loading .env
	// emits at least one "load" action event (the "loaded files" summary).
	var actions []AuditAction
	loadSeen := false
	for _, ev := range handler.recorded() {
		actions = append(actions, ev.Action)
		if ev.Action == ActionLoad {
			loadSeen = true
		}
	}
	if len(actions) == 0 {
		t.Fatal("expected at least one audit event, got none")
	}
	if !loadSeen {
		t.Errorf("expected an audit event with action %q, got actions %v", ActionLoad, actions)
	}
}

// ============================================================================
// auditorInterfaceWrapper Tests
// ============================================================================

type mockAuditLogger struct {
	lastAction AuditAction
	lastKey    string
	lastErrMsg string
	logError   error
}

func (m *mockAuditLogger) LogError(action AuditAction, key, errMsg string) error {
	m.lastAction = action
	m.lastKey = key
	m.lastErrMsg = errMsg
	return m.logError
}

type mockFullAuditLogger struct {
	logs []string
}

func (m *mockFullAuditLogger) Log(action AuditAction, key, reason string, success bool) error {
	m.logs = append(m.logs, "Log")
	return nil
}

func (m *mockFullAuditLogger) LogError(action AuditAction, key, errMsg string) error {
	m.logs = append(m.logs, "LogError")
	return nil
}

func (m *mockFullAuditLogger) LogWithFile(action AuditAction, key, file, reason string, success bool) error {
	m.logs = append(m.logs, "LogWithFile")
	return nil
}

func (m *mockFullAuditLogger) LogWithDuration(action AuditAction, key, reason string, success bool, duration time.Duration) error {
	m.logs = append(m.logs, "LogWithDuration")
	return nil
}

func (m *mockFullAuditLogger) Close() error {
	m.logs = append(m.logs, "Close")
	return nil
}

func TestAuditorInterfaceWrapper(t *testing.T) {
	tests := []struct {
		name      string
		invoke    func(w *auditorInterfaceWrapper) error
		wantMsg   string // expected mockAuditLogger.lastErrMsg ("": assert non-empty)
		wantMsgOK bool   // true: assert non-empty instead of exact match
	}{
		{
			name: "Log success",
			invoke: func(w *auditorInterfaceWrapper) error {
				return w.Log(ActionSet, "KEY", "reason", true)
			},
			wantMsg: "[ok] reason",
		},
		{
			name: "Log failure",
			invoke: func(w *auditorInterfaceWrapper) error {
				return w.Log(ActionSet, "KEY", "reason", false)
			},
			wantMsg: "[error] reason",
		},
		{
			name: "LogWithFile",
			invoke: func(w *auditorInterfaceWrapper) error {
				return w.LogWithFile(ActionSet, "KEY", "test.env", "reason", true)
			},
			wantMsg: "[ok] reason (file: test.env)",
		},
		{
			name: "LogWithDuration",
			invoke: func(w *auditorInterfaceWrapper) error {
				return w.LogWithDuration(ActionSet, "KEY", "reason", true, 100*time.Millisecond)
			},
			wantMsgOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAuditLogger{}
			wrapper := &auditorInterfaceWrapper{AuditLogger: mock}

			if err := tt.invoke(wrapper); err != nil {
				t.Fatalf("invoke() error = %v", err)
			}
			switch {
			case tt.wantMsgOK:
				if mock.lastErrMsg == "" {
					t.Error("should produce non-empty message")
				}
			case mock.lastErrMsg != tt.wantMsg:
				t.Errorf("errMsg = %q, want %q", mock.lastErrMsg, tt.wantMsg)
			}
		})
	}

	t.Run("Close non-closer returns nil", func(t *testing.T) {
		wrapper := &auditorInterfaceWrapper{AuditLogger: &mockAuditLogger{}}
		if err := wrapper.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	t.Run("Close closer delegates", func(t *testing.T) {
		mock := &mockFullAuditLogger{}
		wrapper := &auditorInterfaceWrapper{AuditLogger: mock}

		if err := wrapper.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
		if len(mock.logs) != 1 || mock.logs[0] != "Close" {
			t.Errorf("Close() should delegate, logs = %v", mock.logs)
		}
	})
}

// ============================================================================
// Slice Parsing Boundary Tests
// ============================================================================

// TestParseSliceElement_InvalidInput pins the error contract across every
// supported element type: all parse failures must surface as *ValidationError
// (not raw strconv errors), enabling uniform handling by callers.
func TestParseSliceElement_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		parse   func() error
		wantErr bool
	}{
		{"string never fails", func() error { _, err := parseSliceElement[string]("anything"); return err }, false},
		{"int invalid", func() error { _, err := parseSliceElement[int]("12x"); return err }, true},
		{"int overflow", func() error { _, err := parseSliceElement[int]("9223372036854775808"); return err }, true},
		{"int64 invalid", func() error { _, err := parseSliceElement[int64]("bad"); return err }, true},
		{"uint negative", func() error { _, err := parseSliceElement[uint]("-1"); return err }, true},
		{"uint64 invalid", func() error { _, err := parseSliceElement[uint64]("1.5"); return err }, true},
		{"float invalid", func() error { _, err := parseSliceElement[float64]("x"); return err }, true},
		{"bool invalid", func() error { _, err := parseSliceElement[bool]("maybe"); return err }, true},
		{"duration invalid", func() error { _, err := parseSliceElement[time.Duration]("notaduration"); return err }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.parse()
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Errorf("error = %v (%T), want *ValidationError", err, err)
				}
			}
		})
	}
}

// TestParseSliceElement_ExtremeValidValues checks the numeric boundaries of
// each element type round-trip exactly.
func TestParseSliceElement_ExtremeValidValues(t *testing.T) {
	t.Run("int64 extremes", func(t *testing.T) {
		for _, s := range []string{"-9223372036854775808", "9223372036854775807"} {
			v, err := parseSliceElement[int64](s)
			if err != nil {
				t.Fatalf("parseSliceElement[int64](%q) error = %v", s, err)
			}
			if strconv.FormatInt(v, 10) != s {
				t.Errorf("parseSliceElement[int64](%q) = %d, round-trip mismatch", s, v)
			}
		}
	})

	t.Run("uint64 max", func(t *testing.T) {
		v, err := parseSliceElement[uint64]("18446744073709551615")
		if err != nil || v != 18446744073709551615 {
			t.Errorf("parseSliceElement[uint64] max = %d, %v", v, err)
		}
	})

	t.Run("whitespace is trimmed before parsing", func(t *testing.T) {
		v, err := parseSliceElement[int]("  42\t")
		if err != nil || v != 42 {
			t.Errorf("parseSliceElement[int] with whitespace = %d, %v", v, err)
		}
	})
}

// TestParseCommaSeparated_Fallback pins the default-value semantics: empty
// input and unparseable elements fall back to the caller's default when one
// is provided, and to nil otherwise; empty segments are dropped.
// Quoted elements — the form Marshal emits for slice elements containing
// ',' or '"' — must split back as single elements; see internal.SplitQuotedComma
// (parse-side counterpart of valueToString's quoting).
func TestParseCommaSeparated_QuotedElements(t *testing.T) {
	got := parseCommaSeparated[string](`"a,b", "he said \"hi\"", plain`)
	want := []string{"a,b", `he said "hi"`, "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseCommaSeparated() = %#v, want %#v", got, want)
	}
}

// TestParseCommaSeparated_QuotedWhitespacePreserved pins the GEN-001 fix that
// aligned GetSlice's comma parsing with the struct-mapping path: only
// UNQUOTED elements are trimmed, so a quoted element keeps significant
// leading/trailing whitespace (previously "b " and plain "b" were
// indistinguishable here while UnmarshalStruct told them apart).
func TestParseCommaSeparated_QuotedWhitespacePreserved(t *testing.T) {
	got := parseCommaSeparated[string](`a , "b "`)
	want := []string{"a", "b "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseCommaSeparated() = %#v, want %#v", got, want)
	}

	// Unquoted elements still trim on both sides.
	got = parseCommaSeparated[string](`  a  ,  b  `)
	want = []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseCommaSeparated() = %#v, want %#v", got, want)
	}
}

func TestParseCommaSeparated_Fallback(t *testing.T) {
	def := []int{7}

	tests := []struct {
		name  string
		value string
		def   []int // nil = call WITHOUT the variadic default argument
		want  []int
	}{
		{"empty without default", "", nil, nil},
		{"empty with default", "", def, def},
		// Contract difference vs "": a whitespace-only string is non-empty,
		// so it skips the default-value branch and returns a NON-nil empty
		// slice (the blank segment is dropped after the slice is allocated).
		{"whitespace-only without default", " ", nil, []int{}},
		{"invalid element without default", "1,bad,3", nil, nil},
		{"invalid element with default", "1,bad,3", def, def},
		{"blank segments dropped", " 1 , , 2 ,", nil, []int{1, 2}},
		{"all valid", "1,2,3", nil, []int{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The no-default rows must OMIT the variadic argument entirely:
			// passing a nil slice would still count as one (empty) default,
			// taking the len(defaultValue) > 0 branches for the wrong reason.
			var got []int
			if tt.def == nil {
				got = parseCommaSeparated[int](tt.value)
			} else {
				got = parseCommaSeparated(tt.value, tt.def)
			}
			if tt.want == nil && got != nil {
				t.Fatalf("parseCommaSeparated(%q) = %v, want nil", tt.value, got)
			}
			if tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseCommaSeparated(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Unmarshal Boundary Tests
// ============================================================================

// recordingUnmarshaler implements Unmarshaler to verify that UnmarshalInto
// defers to custom unmarshaling before any reflection takes place.
type recordingUnmarshaler struct {
	got map[string]string
	err error
}

func (u *recordingUnmarshaler) UnmarshalEnv(m map[string]string) error {
	u.got = m
	return u.err
}

func TestUnmarshalInto_UnmarshalerPrecedence(t *testing.T) {
	t.Run("success delegates the map", func(t *testing.T) {
		u := &recordingUnmarshaler{}
		data := map[string]string{"K": "v"}
		if err := UnmarshalInto(data, u); err != nil {
			t.Fatalf("UnmarshalInto() error = %v", err)
		}
		if u.got["K"] != "v" {
			t.Errorf("UnmarshalEnv received %v, want the source map", u.got)
		}
	})

	t.Run("custom error propagates", func(t *testing.T) {
		wantErr := errors.New("custom unmarshal failure")
		u := &recordingUnmarshaler{err: wantErr}
		if err := UnmarshalInto(map[string]string{"K": "v"}, u); !errors.Is(err, wantErr) {
			t.Errorf("UnmarshalInto() error = %v, want %v", err, wantErr)
		}
	})
}

// failingEnvMarshaler implements Marshaler to verify MarshalStruct propagates
// marshaling failures.
type failingEnvMarshaler struct{}

func (failingEnvMarshaler) MarshalEnv() ([]byte, error) {
	return nil, errors.New("marshal exploded")
}

func TestMarshalStruct_MarshalerError(t *testing.T) {
	if _, err := MarshalStruct(failingEnvMarshaler{}); err == nil {
		t.Error("MarshalStruct() should propagate the Marshaler error")
	}
}

func TestUnmarshalStruct_PropagatesParseError(t *testing.T) {
	var target struct {
		K string
	}
	if err := UnmarshalStruct("BROKEN=\"unterminated\n", &target); err == nil {
		t.Error("UnmarshalStruct() should propagate the UnmarshalMap parse error")
	}
}

// TestUnmarshalMap_FormatAutoFallback pins the auto-detection fallback: data
// whose first meaningful line matches no format signature (no ": ", no "=")
// and comment/blank-only data both default to FormatEnv parsing.
func TestUnmarshalMap_FormatAutoFallback(t *testing.T) {
	tests := []struct {
		name string
		data string
		want map[string]string
	}{
		{"content with no format signature", "plainword", map[string]string{}},
		{"comments and blanks only", "# a comment\n\n# another\n", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalMap(tt.data, FormatAuto)
			if err != nil {
				t.Fatalf("UnmarshalMap() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Errorf("UnmarshalMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnmarshalMap_EnvQuoteSemantics pins the .env quote-semantics fixes end
// to end through the public parse path (parseString uses the same Parse as
// file loading): a trailing " #…" comment after a quoted value is accepted,
// single-quoted values are exempt from variable expansion, a double-quoted \$\
// survives expansion as one literal dollar, and unknown escapes keep their
// backslash.
func TestUnmarshalMap_EnvQuoteSemantics(t *testing.T) {
	content := strings.Join([]string{
		`PASSWORD='my$ecret'`,
		`NAME="world"`,
		`GREET="hello $NAME" # trailing comment`,
		`PRICE="cost: \$5"`,
		`DIR="C:\Users\foo"`,
		``,
	}, "\n")

	m, err := UnmarshalMap(content)
	if err != nil {
		t.Fatalf("UnmarshalMap() error = %v", err)
	}
	if got := m["PASSWORD"]; got != "my$ecret" {
		t.Errorf("PASSWORD = %q, want literal %q (single quotes suppress expansion)", got, "my$ecret")
	}
	if got := m["GREET"]; got != "hello world" {
		t.Errorf("GREET = %q, want %q (trailing comment stripped, reference expanded)", got, "hello world")
	}
	if got := m["PRICE"]; got != "cost: $5" {
		t.Errorf("PRICE = %q, want %q (escaped dollar survives expansion)", got, "cost: $5")
	}
	if got := m["DIR"]; got != `C:\Users\foo` {
		t.Errorf("DIR = %q, want %q (unknown escape keeps its backslash)", got, `C:\Users\foo`)
	}
}

// TestUnmarshalMap_YAMLSequenceRootAndSingleQuotes pins the YAML-side fixes:
// a root-level sequence errors instead of loading as an empty map, and
// single-quoted scalars keep backslashes literally.
func TestUnmarshalMap_YAMLSequenceRootAndSingleQuotes(t *testing.T) {
	if _, err := UnmarshalMap("- a\n- b\n", FormatYAML); err == nil {
		t.Error("UnmarshalMap(root sequence, YAML) succeeded, want an error naming the mapping requirement")
	} else if !strings.Contains(err.Error(), "mapping") {
		t.Errorf("error = %v, want it to name the root-mapping requirement", err)
	}

	m, err := UnmarshalMap("dir: 'C:\\Users\\app'\n", FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalMap(single-quoted backslashes) error = %v", err)
	}
	if got := m["DIR"]; got != `C:\Users\app` {
		t.Errorf("DIR = %q, want %q (single-quoted backslashes are literal)", got, `C:\Users\app`)
	}
}

// TestUnmarshalMap_BOMStripped pins UTF-8 BOM handling at all three format
// entry points: a leading EF BB BF (Windows Notepad / PowerShell Out-File
// default) previously made the .env first key fail validation with a
// misleading non-ASCII error and made JSON error as invalid syntax.
func TestUnmarshalMap_BOMStripped(t *testing.T) {
	const bom = "\xEF\xBB\xBF"

	m, err := UnmarshalMap(bom + "KEY=value\n")
	if err != nil {
		t.Fatalf("UnmarshalMap(.env with BOM) error = %v", err)
	}
	if m["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", m["KEY"], "value")
	}

	m, err = UnmarshalMap(bom+`{"a":"b"}`, FormatJSON)
	if err != nil {
		t.Fatalf("UnmarshalMap(JSON with BOM) error = %v", err)
	}
	if m["A"] != "b" {
		t.Errorf("A = %q, want %q", m["A"], "b")
	}

	m, err = UnmarshalMap(bom+"a: b\n", FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalMap(YAML with BOM) error = %v", err)
	}
	if m["A"] != "b" {
		t.Errorf("A = %q, want %q", m["A"], "b")
	}
}

// TestMarshalEnv_DollarRoundTrip pins SEC-009 (CWE-150/74): a value containing
// a dollar reference is quoted and escaped so a Marshal→Unmarshal round trip
// does not re-expand it (ExpandVariables defaults on).
func TestMarshalEnv_DollarRoundTrip(t *testing.T) {
	original := map[string]string{
		"K": "cost $5",
		"H": "${HOME}/app",
	}
	out, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(out, `cost \$5`) || !strings.Contains(out, `\${HOME}/app`) {
		t.Errorf("dollar references not escaped in output:\n%s", out)
	}
	back, err := UnmarshalMap(out)
	if err != nil {
		t.Fatalf("UnmarshalMap() error = %v", err)
	}
	for k, want := range original {
		if back[k] != want {
			t.Errorf("round trip %s = %q, want %q (dollar must be literal)", k, back[k], want)
		}
	}
}

// TestMarshalEnv_RoundTripYamlSyntax pins the null/~ quoting guard on the
// .env writer: bare "null" and "~" values previously collapsed to the empty
// string when the output was reloaded under AllowYamlSyntax (the
// DevelopmentConfig preset ships it enabled). Number-like and boolean text
// passes through unchanged and stays bare.
func TestMarshalEnv_RoundTripYamlSyntax(t *testing.T) {
	out, err := Marshal(map[string]string{"A": "null", "B": "~", "C": "8080", "D": "true"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(out, `A="null"`) || !strings.Contains(out, `B="~"`) {
		t.Errorf("null/~ not quoted in output:\n%s", out)
	}
	if !strings.Contains(out, "C=8080") || !strings.Contains(out, "D=true") {
		t.Errorf("passthrough values needlessly quoted:\n%s", out)
	}

	cfg := DefaultConfig()
	cfg.AllowYamlSyntax = true
	m, err := UnmarshalMapWithConfig(cfg, out)
	if err != nil {
		t.Fatalf("UnmarshalMapWithConfig() error = %v", err)
	}
	for k, want := range map[string]string{"A": "null", "B": "~", "C": "8080", "D": "true"} {
		if m[k] != want {
			t.Errorf("round trip %s = %q, want %q", k, m[k], want)
		}
	}
}

// TestUnmarshalMap_YAMLOddIndents pins column-based nesting: 1-space and
// 3-space children previously aliased their parent's level (the width/2
// truncation), silently re-parenting keys in hand-edited files.
func TestUnmarshalMap_YAMLOddIndents(t *testing.T) {
	m, err := UnmarshalMap("a:\n b: 1\nc: 2\n", FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalMap(1-space indent) error = %v", err)
	}
	if m["A_B"] != "1" || m["C"] != "2" || len(m) != 2 {
		t.Errorf("1-space indent: got %v, want {A_B:1, C:2}", m)
	}

	m, err = UnmarshalMap("a:\n  b:\n   c: 1\n", FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalMap(3-space indent) error = %v", err)
	}
	if m["A_B_C"] != "1" || len(m) != 1 {
		t.Errorf("3-space indent: got %v, want {A_B_C:1}", m)
	}
}

// TestUnmarshalMap_FormatAutoDetection pins the auto-detection fixes: the
// separator that appears FIRST wins (an .env assignment whose value contains
// ": " classifies as .env; a YAML mapping whose value contains "="
// classifies as YAML), and a bare "key:" line with an empty value — the
// canonical start of a YAML block — classifies as YAML instead of falling
// through to .env.
func TestUnmarshalMap_FormatAutoDetection(t *testing.T) {
	m, err := UnmarshalMap("GREETING=hello: world\n", FormatAuto)
	if err != nil {
		t.Fatalf("UnmarshalMap(.env with colon-space in value) error = %v", err)
	}
	if m["GREETING"] != "hello: world" {
		t.Errorf("GREETING = %q, want %q (must classify as .env)", m["GREETING"], "hello: world")
	}

	m, err = UnmarshalMap("connection: host=db\n", FormatAuto)
	if err != nil {
		t.Fatalf("UnmarshalMap(YAML with = in value) error = %v", err)
	}
	if m["CONNECTION"] != "host=db" {
		t.Errorf("CONNECTION = %q, want %q (must classify as YAML)", m["CONNECTION"], "host=db")
	}

	m, err = UnmarshalMap("server:\n  port: 1\n", FormatAuto)
	if err != nil {
		t.Fatalf("UnmarshalMap(YAML block with empty-value first key) error = %v", err)
	}
	if m["SERVER_PORT"] != "1" {
		t.Errorf("SERVER_PORT = %q, want %q (bare key: line must classify as YAML)", m["SERVER_PORT"], "1")
	}
}
