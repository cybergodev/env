package internal

import (
	"reflect"
	"testing"
	"time"
)

type testStruct struct {
	Name    string        `env:"NAME"`
	Port    int           `env:"PORT"`
	Enabled bool          `env:"ENABLED"`
	Timeout time.Duration `env:"TIMEOUT"`
}

type nestedStruct struct {
	Config testStruct `env:"CONFIG"`
}

type taggedStruct struct {
	CustomName string `env:"MY_CUSTOM_NAME"`
	Ignored    string `env:"-"`
}

func TestStruct(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		prefix  string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "simple struct",
			input: testStruct{
				Name:    "test",
				Port:    8080,
				Enabled: true,
				Timeout: 5 * time.Second,
			},
			want: map[string]string{
				"NAME":    "test",
				"PORT":    "8080",
				"ENABLED": "true",
				"TIMEOUT": "5s",
			},
			wantErr: false,
		},
		{
			name: "nested struct",
			input: nestedStruct{
				Config: testStruct{
					Name: "nested",
				},
			},
			want: map[string]string{
				"CONFIG_NAME": "nested",
			},
			wantErr: false,
		},
		{
			name: "with prefix",
			input: testStruct{
				Name: "test",
			},
			prefix: "APP",
			want: map[string]string{
				"APP_NAME": "test",
			},
			wantErr: false,
		},
		{
			name: "ignored field",
			input: taggedStruct{
				CustomName: "value",
				Ignored:    "should not appear",
			},
			want: map[string]string{
				"MY_CUSTOM_NAME": "value",
			},
			wantErr: false,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Struct(tt.input, tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Errorf("Struct() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			for k, v := range tt.want {
				if result[k] != v {
					t.Errorf("Struct()[%q] = %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

func TestStructInto(t *testing.T) {
	data := map[string]string{
		"NAME":    "test",
		"PORT":    "8080",
		"ENABLED": "true",
		"TIMEOUT": "5s",
	}

	var result testStruct
	err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
	if err != nil {
		t.Errorf("StructInto() error = %v", err)
		return
	}

	if result.Name != "test" {
		t.Errorf("Name = %q, want %q", result.Name, "test")
	}
	if result.Port != 8080 {
		t.Errorf("Port = %d, want %d", result.Port, 8080)
	}
	if result.Enabled != true {
		t.Errorf("Enabled = %v, want %v", result.Enabled, true)
	}
	if result.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want %v", result.Timeout, 5*time.Second)
	}
}

func TestStructIntoWithDefaults(t *testing.T) {
	type structWithDefaults struct {
		Name    string `env:"NAME" envDefault:"default_name"`
		Port    int    `env:"PORT" envDefault:"3000"`
		Missing string `env:"MISSING"` // No default
	}

	data := map[string]string{} // Empty data

	var result structWithDefaults
	err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
	if err != nil {
		t.Errorf("StructInto() error = %v", err)
		return
	}

	if result.Name != "default_name" {
		t.Errorf("Name = %q, want default", result.Name)
	}
	if result.Port != 3000 {
		t.Errorf("Port = %d, want 3000", result.Port)
	}
	if result.Missing != "" {
		t.Errorf("Missing = %q, should be empty", result.Missing)
	}
}

func TestValueToString(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{"string", "hello", "hello", false},
		{"int", 42, "42", false},
		{"bool true", true, "true", false},
		{"bool false", false, "false", false},
		{"float", 3.14, "3.14", false},
		{"duration", 5 * time.Second, "5s", false},
		{"byte slice", []byte("bytes"), "bytes", false},
		{"string slice", []string{"a", "b"}, "a,b", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.input)
			result, err := valueToString(val)
			if (err != nil) != tt.wantErr {
				t.Errorf("valueToString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.want {
				t.Errorf("valueToString() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestSetFieldValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		target  interface{}
		want    interface{}
		wantErr bool
	}{
		{"string", "hello", new(string), "hello", false},
		{"int", "42", new(int), 42, false},
		{"bool", "true", new(bool), true, false},
		{"float", "3.14", new(float64), 3.14, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.target).Elem()
			err := setFieldValue(val, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setFieldValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				got := val.Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("setFieldValue() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestSetSliceValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		target  interface{}
		want    interface{}
		wantErr bool
	}{
		{"string slice", "a,b,c", new([]string), []string{"a", "b", "c"}, false},
		{"int slice", "1,2,3", new([]int), []int{1, 2, 3}, false},
		{"bool slice", "true,false", new([]bool), []bool{true, false}, false},
		{"empty", "", new([]string), []string(nil), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.target).Elem()
			err := setSliceValue(val, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setSliceValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.value != "" {
				got := val.Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("setSliceValue() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// ============================================================================
// Additional ValueToString Tests
// ============================================================================

func TestValueToString_Additional(t *testing.T) {
	tests := []struct {
		name      string
		input     interface{}
		want      string
		wantErr   bool
		wantEmpty bool // if true, just check for empty string
	}{
		{"int8", int8(8), "8", false, false},
		{"int16", int16(16), "16", false, false},
		{"int32", int32(32), "32", false, false},
		{"int64", int64(64), "64", false, false},
		{"uint", uint(10), "10", false, false},
		{"uint8", uint8(8), "8", false, false},
		{"uint16", uint16(16), "16", false, false},
		{"uint32", uint32(32), "32", false, false},
		{"uint64", uint64(64), "64", false, false},
		{"float32", float32(3.5), "3.5", false, false}, // Use exact float32 value
		{"nil pointer", (*string)(nil), "", false, false},
		{"unsupported slice", []int{1, 2, 3}, "", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.input)
			result, err := valueToString(val)
			if (err != nil) != tt.wantErr {
				t.Errorf("valueToString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantEmpty && result != tt.want {
				t.Errorf("valueToString() = %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Additional SetFieldValue Tests
// ============================================================================

func TestSetFieldValue_Additional(t *testing.T) {
	t.Run("empty value for non-string", func(t *testing.T) {
		var i int
		val := reflect.ValueOf(&i).Elem()
		err := setFieldValue(val, "")
		if err != nil {
			t.Errorf("setFieldValue(empty) error = %v", err)
		}
		// Should remain zero value
		if i != 0 {
			t.Errorf("i = %d, want 0", i)
		}
	})

	t.Run("pointer to int", func(t *testing.T) {
		var ptr *int
		val := reflect.ValueOf(&ptr).Elem()
		err := setFieldValue(val, "42")
		if err != nil {
			t.Errorf("setFieldValue(ptr) error = %v", err)
		}
		if ptr == nil || *ptr != 42 {
			t.Errorf("ptr = %v, want *42", ptr)
		}
	})

	t.Run("uint types", func(t *testing.T) {
		var u uint
		val := reflect.ValueOf(&u).Elem()
		err := setFieldValue(val, "42")
		if err != nil {
			t.Errorf("setFieldValue(uint) error = %v", err)
		}
		if u != 42 {
			t.Errorf("u = %d, want 42", u)
		}
	})

	t.Run("float types", func(t *testing.T) {
		var f float32
		val := reflect.ValueOf(&f).Elem()
		err := setFieldValue(val, "3.14")
		if err != nil {
			t.Errorf("setFieldValue(float32) error = %v", err)
		}
		if f != 3.14 {
			t.Errorf("f = %v, want 3.14", f)
		}
	})

	t.Run("invalid int", func(t *testing.T) {
		var i int
		val := reflect.ValueOf(&i).Elem()
		err := setFieldValue(val, "not_a_number")
		if err == nil {
			t.Error("setFieldValue(invalid int) should return error")
		}
	})

	t.Run("invalid bool", func(t *testing.T) {
		var b bool
		val := reflect.ValueOf(&b).Elem()
		err := setFieldValue(val, "not_a_bool")
		if err == nil {
			t.Error("setFieldValue(invalid bool) should return error")
		}
	})

	t.Run("unsupported type", func(t *testing.T) {
		var m map[string]string
		val := reflect.ValueOf(&m).Elem()
		err := setFieldValue(val, "value")
		if err == nil {
			t.Error("setFieldValue(unsupported) should return error")
		}
	})
}

// ============================================================================
// Additional SetSliceValue Tests
// ============================================================================

func TestSetSliceValue_Additional(t *testing.T) {
	t.Run("int slice with spaces", func(t *testing.T) {
		var slice []int
		val := reflect.ValueOf(&slice).Elem()
		err := setSliceValue(val, " 1 , 2 , 3 ")
		if err != nil {
			t.Errorf("setSliceValue() error = %v", err)
		}
		if len(slice) != 3 || slice[0] != 1 || slice[1] != 2 || slice[2] != 3 {
			t.Errorf("slice = %v, want [1 2 3]", slice)
		}
	})

	t.Run("uint slice", func(t *testing.T) {
		var slice []uint
		val := reflect.ValueOf(&slice).Elem()
		err := setSliceValue(val, "1,2,3")
		if err != nil {
			t.Errorf("setSliceValue() error = %v", err)
		}
		if len(slice) != 3 || slice[0] != 1 || slice[1] != 2 || slice[2] != 3 {
			t.Errorf("slice = %v, want [1 2 3]", slice)
		}
	})

	t.Run("float slice", func(t *testing.T) {
		var slice []float64
		val := reflect.ValueOf(&slice).Elem()
		err := setSliceValue(val, "1.1,2.2,3.3")
		if err != nil {
			t.Errorf("setSliceValue() error = %v", err)
		}
		if len(slice) != 3 {
			t.Errorf("slice length = %d, want 3", len(slice))
		}
	})

	t.Run("invalid int slice element", func(t *testing.T) {
		var slice []int
		val := reflect.ValueOf(&slice).Elem()
		err := setSliceValue(val, "1,not_a_number,3")
		if err == nil {
			t.Error("setSliceValue() should return error for invalid int")
		}
	})

	t.Run("invalid bool slice element", func(t *testing.T) {
		var slice []bool
		val := reflect.ValueOf(&slice).Elem()
		err := setSliceValue(val, "true,not_a_bool")
		if err == nil {
			t.Error("setSliceValue() should return error for invalid bool")
		}
	})

	t.Run("unsupported slice element type", func(t *testing.T) {
		var slice []map[string]string
		val := reflect.ValueOf(&slice).Elem()
		err := setSliceValue(val, "value")
		if err == nil {
			t.Error("setSliceValue() should return error for unsupported type")
		}
	})
}

// ============================================================================
// Struct Edge Cases Tests
// ============================================================================

func TestStruct_EdgeCases(t *testing.T) {
	t.Run("nil pointer to struct", func(t *testing.T) {
		var s *testStruct
		result, err := Struct(s, "")
		if err != nil {
			t.Errorf("Struct(nil pointer) error = %v", err)
		}
		if result != nil {
			t.Errorf("Struct(nil pointer) = %v, want nil", result)
		}
	})

	t.Run("non-struct input", func(t *testing.T) {
		_, err := Struct("not a struct", "")
		if err == nil {
			t.Error("Struct(string) should return error")
		}
	})

	t.Run("pointer to nested struct", func(t *testing.T) {
		type outer struct {
			Inner *testStruct `env:"INNER"`
		}
		s := outer{
			Inner: &testStruct{
				Name: "nested",
			},
		}
		result, err := Struct(s, "")
		if err != nil {
			t.Errorf("Struct() error = %v", err)
		}
		if result["INNER_NAME"] != "nested" {
			t.Errorf("result[\"INNER_NAME\"] = %q, want \"nested\"", result["INNER_NAME"])
		}
	})

	t.Run("empty struct", func(t *testing.T) {
		type empty struct{}
		result, err := Struct(empty{}, "")
		if err != nil {
			t.Errorf("Struct(empty) error = %v", err)
		}
		if len(result) != 0 {
			t.Errorf("Struct(empty) = %v, want empty map", result)
		}
	})

	t.Run("unexported field ignored", func(t *testing.T) {
		type withUnexported struct {
			Exported   string `env:"EXPORTED"`
			unexported string // should be ignored
		}
		s := withUnexported{
			Exported:   "value",
			unexported: "secret",
		}
		result, err := Struct(s, "")
		if err != nil {
			t.Errorf("Struct() error = %v", err)
		}
		if len(result) != 1 || result["EXPORTED"] != "value" {
			t.Errorf("Struct() = %v, want only EXPORTED", result)
		}
	})
}

// ============================================================================
// StructInto Edge Cases Tests
// ============================================================================

func TestStructInto_EdgeCases(t *testing.T) {
	t.Run("nested struct", func(t *testing.T) {
		type inner struct {
			Value string `env:"VALUE"`
		}
		type outer struct {
			Inner inner `env:"INNER"`
		}

		data := map[string]string{
			"INNER_VALUE": "nested_value",
		}

		var result outer
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Inner.Value != "nested_value" {
			t.Errorf("result.Inner.Value = %q, want \"nested_value\"", result.Inner.Value)
		}
	})

	t.Run("pointer to nested struct", func(t *testing.T) {
		type inner struct {
			Value string `env:"VALUE"`
		}
		type outer struct {
			Inner *inner `env:"INNER"`
		}

		data := map[string]string{
			"INNER_VALUE": "pointer_value",
		}

		var result outer
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Inner == nil || result.Inner.Value != "pointer_value" {
			t.Errorf("result.Inner.Value = %v, want \"pointer_value\"", result.Inner)
		}
	})

	t.Run("env tag dash skips field", func(t *testing.T) {
		type withSkip struct {
			Value  string `env:"VALUE"`
			SkipMe string `env:"-"`
		}

		data := map[string]string{
			"VALUE":  "kept",
			"SKIPME": "should_not_be_set",
		}

		var result withSkip
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Value != "kept" {
			t.Errorf("result.Value = %q, want \"kept\"", result.Value)
		}
		if result.SkipMe != "" {
			t.Errorf("result.SkipMe = %q, should be empty (skipped)", result.SkipMe)
		}
	})

	t.Run("unexported field skipped", func(t *testing.T) {
		type withUnexported struct {
			Exported   string `env:"EXPORTED"`
			unexported string
		}

		data := map[string]string{
			"EXPORTED":   "value",
			"UNEXPORTED": "should_be_ignored",
		}

		var result withUnexported
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Exported != "value" {
			t.Errorf("result.Exported = %q, want \"value\"", result.Exported)
		}
		// unexported field should remain empty
		if result.unexported != "" {
			t.Errorf("result.unexported = %q, should be empty", result.unexported)
		}
	})
}

// ============================================================================
// Lowercase and Dot-Notation Tag Tests
// ============================================================================

func TestStructInto_LowercaseTag(t *testing.T) {
	t.Run("lowercase tag matches uppercase key", func(t *testing.T) {
		type Config struct {
			APIKey string `env:"deepseek_key"`
		}

		data := map[string]string{"DEEPSEEK_KEY": "sk-123"}
		var result Config
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.APIKey != "sk-123" {
			t.Errorf("APIKey = %q, want %q", result.APIKey, "sk-123")
		}
	})

	t.Run("lowercase tag with prefix", func(t *testing.T) {
		type Config struct {
			Host string `env:"host"`
		}

		data := map[string]string{"APP_HOST": "localhost"}
		var result Config
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "app")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Host != "localhost" {
			t.Errorf("Host = %q, want %q", result.Host, "localhost")
		}
	})

	t.Run("mixed case tag", func(t *testing.T) {
		type Config struct {
			Key string `env:"DeepSeek_Key"`
		}

		data := map[string]string{"DEEPSEEK_KEY": "val"}
		var result Config
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Key != "val" {
			t.Errorf("Key = %q, want %q", result.Key, "val")
		}
	})
}

func TestStructInto_DotNotationTag(t *testing.T) {
	t.Run("dot-notation tag resolves to flattened key", func(t *testing.T) {
		type Config struct {
			Host string `env:"database.host"`
		}

		data := map[string]string{"DATABASE_HOST": "localhost"}
		var result Config
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Host != "localhost" {
			t.Errorf("Host = %q, want %q", result.Host, "localhost")
		}
	})

	t.Run("multi-level dot-notation", func(t *testing.T) {
		type Config struct {
			Port string `env:"server.http.port"`
		}

		data := map[string]string{"SERVER_HTTP_PORT": "8080"}
		var result Config
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Port != "8080" {
			t.Errorf("Port = %q, want %q", result.Port, "8080")
		}
	})

	t.Run("dot-notation with envDefault", func(t *testing.T) {
		type Config struct {
			Host string `env:"database.host" envDefault:"127.0.0.1"`
		}

		data := map[string]string{}
		var result Config
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Host != "127.0.0.1" {
			t.Errorf("Host = %q, want %q", result.Host, "127.0.0.1")
		}
	})

	t.Run("dot-notation takes precedence over default", func(t *testing.T) {
		type Config struct {
			Host string `env:"database.host" envDefault:"127.0.0.1"`
		}

		data := map[string]string{"DATABASE_HOST": "production.db"}
		var result Config
		err := StructInto(data, reflect.ValueOf(&result).Elem(), "")
		if err != nil {
			t.Errorf("StructInto() error = %v", err)
		}
		if result.Host != "production.db" {
			t.Errorf("Host = %q, want %q", result.Host, "production.db")
		}
	})
}
