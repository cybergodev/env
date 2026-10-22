package internal

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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

// TestStruct is the single marshal-direction table: the merge of the former
// TestStruct_EdgeCases and TestMarshalStruct_UnsupportedKindErrors.
func TestStruct(t *testing.T) {
	// Fixture types for the table (formerly local to the merged tests).
	type ptrOuter struct {
		Inner *testStruct `env:"INNER"`
	}
	type withUnexported struct {
		Exported   string `env:"EXPORTED"`
		unexported string // must be ignored
	}
	type unicodeCfg struct {
		Zh    string `env:"ZH"`
		Emoji string `env:"EMOJI"`
	}

	tests := []struct {
		name        string
		input       interface{}
		prefix      string
		want        map[string]string
		wantLen     int  // exact result size (always pinned; default 0 fits only the empty-struct row)
		wantNil     bool // result must be a nil map
		wantErr     bool
		errContains string // substring the error must name
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
			wantLen: 4,
		},
		{
			// Partial want: the nested struct's zero numeric fields also
			// emit ("0", "false", "0s") — only empty STRINGS are omitted.
			name:    "nested struct",
			input:   nestedStruct{Config: testStruct{Name: "nested"}},
			want:    map[string]string{"CONFIG_NAME": "nested"},
			wantLen: 4,
		},
		{
			name:    "with prefix",
			input:   testStruct{Name: "test"},
			prefix:  "APP",
			want:    map[string]string{"APP_NAME": "test"},
			wantLen: 4,
		},
		{
			name:    "ignored field",
			input:   taggedStruct{CustomName: "value", Ignored: "should not appear"},
			want:    map[string]string{"MY_CUSTOM_NAME": "value"},
			wantLen: 1,
		},
		{
			// Owns the nil-interface branch (marshal.go `if v == nil`),
			// distinct from the nil-struct-pointer row below. The verify
			// map is empty, so the nil assertion is what actually pins it.
			name:    "nil input",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "nil pointer to struct",
			input:   (*testStruct)(nil),
			wantNil: true,
		},
		{
			name:    "non-struct input",
			input:   "not a struct",
			wantErr: true,
		},
		{
			name:    "pointer to nested struct",
			input:   ptrOuter{Inner: &testStruct{Name: "nested"}},
			want:    map[string]string{"INNER_NAME": "nested"},
			wantLen: 4,
		},
		{
			name:    "empty struct",
			input:   struct{}{},
			wantLen: 0,
		},
		{
			name:    "unexported field ignored",
			input:   withUnexported{Exported: "value", unexported: "secret"},
			want:    map[string]string{"EXPORTED": "value"},
			wantLen: 1,
		},
		{
			name:        "chan field errors naming the field",
			input:       marshalBadField{Ch: make(chan int, 1)},
			wantErr:     true,
			errContains: "Ch",
		},
		{
			name:    "nested struct propagates unsupported-kind error",
			input:   marshalNestedBad{Inner: marshalBadField{Ch: make(chan int, 1)}},
			wantErr: true,
		},
		{
			name:    "pointer-nested struct propagates unsupported-kind error",
			input:   marshalPtrNestedBad{Inner: &marshalBadField{Ch: make(chan int, 1)}},
			wantErr: true,
		},
		{
			// BOUNDARY: unicode values (中文/emoji) marshal unchanged.
			name:    "unicode values",
			input:   unicodeCfg{Zh: "中文值", Emoji: "🎉"},
			want:    map[string]string{"ZH": "中文值", "EMOJI": "🎉"},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Struct(tt.input, tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Struct() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Struct() error = %v, want it to name %q", err, tt.errContains)
				}
				return
			}
			if tt.wantNil {
				if result != nil {
					t.Errorf("Struct() = %v, want nil map", result)
				}
				return
			}
			for k, v := range tt.want {
				if result[k] != v {
					t.Errorf("Struct()[%q] = %q, want %q", k, result[k], v)
				}
			}
			if len(result) != tt.wantLen {
				t.Errorf("Struct() returned %d keys, want %d (%v)", len(result), tt.wantLen, result)
			}
		})
	}
}

// TestStructTextMarshalerFields pins that struct-kind fields implementing
// encoding.TextMarshaler/TextUnmarshaler (time.Time is the canonical case)
// serialize and parse as scalar leaves. Field recursion previously walked
// their unexported internals and silently contributed nothing in BOTH
// directions.
func TestStructTextMarshalerFields(t *testing.T) {
	type Cfg struct {
		At    time.Time  `env:"TS"`
		Until *time.Time `env:"UNTIL"`
		Note  string     `env:"NOTE"`
	}
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	// Marshal: both value and pointer time.Time fields emit RFC 3339.
	m, err := Struct(Cfg{At: start, Until: &start, Note: "n"}, "")
	if err != nil {
		t.Fatalf("Struct error = %v", err)
	}
	if m["TS"] != "2026-10-18T12:00:00Z" {
		t.Errorf("TS = %q, want RFC 3339 rendering", m["TS"])
	}
	if m["UNTIL"] != "2026-10-18T12:00:00Z" {
		t.Errorf("UNTIL = %q, want RFC 3339 rendering", m["UNTIL"])
	}

	// A nil *time.Time is omitted, like other nil pointers.
	mNil, err := Struct(Cfg{Note: "n"}, "")
	if err != nil {
		t.Fatalf("Struct error = %v", err)
	}
	if _, exists := mNil["UNTIL"]; exists {
		t.Errorf("nil *time.Time field should be omitted, got %q", mNil["UNTIL"])
	}

	// Unmarshal: both field shapes parse back (the pointer is allocated).
	var out Cfg
	if err := StructInto(map[string]string{
		"TS":    "2026-10-18T12:00:00Z",
		"UNTIL": "2026-10-18T12:00:00Z",
	}, reflect.ValueOf(&out).Elem(), ""); err != nil {
		t.Fatalf("StructInto error = %v", err)
	}
	if !out.At.Equal(start) {
		t.Errorf("At = %v, want %v", out.At, start)
	}
	if out.Until == nil || !out.Until.Equal(start) {
		t.Errorf("Until = %v, want %v", out.Until, start)
	}

	// Invalid text is a loud error, not a silent no-op.
	var bad Cfg
	if err := StructInto(map[string]string{"TS": "not-a-time"}, reflect.ValueOf(&bad).Elem(), ""); err == nil {
		t.Error("StructInto(invalid time) = nil, want error")
	}
}

// TestStructInto is the single unmarshal-direction table: the merge of the
// former TestStructIntoWithDefaults, TestStructInto_EdgeCases,
// TestStructInto_LowercaseTag, TestStructInto_DotNotationTag,
// TestStructInto_InlineDefaultTag and TestStructInto_NestedErrorPropagation.
func TestStructInto(t *testing.T) {
	// Fixture types for the table (formerly local to the merged tests).
	type defaultsCfg struct {
		Name    string `env:"NAME" envDefault:"default_name"`
		Port    int    `env:"PORT" envDefault:"3000"`
		Missing string `env:"MISSING"`
	}
	type innerValue struct {
		Value string `env:"VALUE"`
	}
	type outerValue struct {
		Inner innerValue `env:"INNER"`
	}
	type outerPtr struct {
		Inner *innerValue `env:"INNER"`
	}
	type skipCfg struct {
		Value  string `env:"VALUE"`
		SkipMe string `env:"-"`
	}
	type unexportedCfg struct {
		Exported   string `env:"EXPORTED"`
		unexported string
	}
	type lowerKeyCfg struct {
		APIKey string `env:"deepseek_key"`
	}
	type lowerHostCfg struct {
		Host string `env:"host"`
	}
	type mixedKeyCfg struct {
		Key string `env:"DeepSeek_Key"`
	}
	type dotHostCfg struct {
		Host string `env:"database.host"`
	}
	type dotPortCfg struct {
		Port string `env:"server.http.port"`
	}
	type dotDefaultCfg struct {
		Host string `env:"database.host" envDefault:"127.0.0.1"`
	}
	type inlineCfg struct {
		Mode string `env:"MODE,envDefault:prod"`
	}
	type badInner struct {
		N int
	}
	type badPlain struct {
		In badInner `env:"IN"`
	}
	type badPtr struct {
		In *badInner `env:"IN"`
	}
	type unicodeCfg struct {
		Zh    string `env:"ZH"`
		Emoji string `env:"EMOJI"`
	}

	tests := []struct {
		name    string
		data    map[string]string
		prefix  string
		newOut  func() any
		check   func(*testing.T, any)
		wantErr bool
	}{
		{
			name: "all fields set",
			data: map[string]string{
				"NAME":    "test",
				"PORT":    "8080",
				"ENABLED": "true",
				"TIMEOUT": "5s",
			},
			newOut: func() any { return new(testStruct) },
			check: func(t *testing.T, out any) {
				s := out.(*testStruct)
				if s.Name != "test" {
					t.Errorf("Name = %q, want %q", s.Name, "test")
				}
				if s.Port != 8080 {
					t.Errorf("Port = %d, want %d", s.Port, 8080)
				}
				if !s.Enabled {
					t.Errorf("Enabled = false, want true")
				}
				if s.Timeout != 5*time.Second {
					t.Errorf("Timeout = %v, want %v", s.Timeout, 5*time.Second)
				}
			},
		},
		{
			name:   "envDefault applies when key missing",
			data:   map[string]string{}, // empty data
			newOut: func() any { return new(defaultsCfg) },
			check: func(t *testing.T, out any) {
				s := out.(*defaultsCfg)
				if s.Name != "default_name" {
					t.Errorf("Name = %q, want default", s.Name)
				}
				if s.Port != 3000 {
					t.Errorf("Port = %d, want 3000", s.Port)
				}
				if s.Missing != "" {
					t.Errorf("Missing = %q, should be empty", s.Missing)
				}
			},
		},
		{
			name:   "nested struct via flattened key",
			data:   map[string]string{"INNER_VALUE": "nested_value"},
			newOut: func() any { return new(outerValue) },
			check: func(t *testing.T, out any) {
				if got := out.(*outerValue).Inner.Value; got != "nested_value" {
					t.Errorf("Inner.Value = %q, want %q", got, "nested_value")
				}
			},
		},
		{
			name:   "pointer to nested struct allocated",
			data:   map[string]string{"INNER_VALUE": "pointer_value"},
			newOut: func() any { return new(outerPtr) },
			check: func(t *testing.T, out any) {
				s := out.(*outerPtr)
				if s.Inner == nil || s.Inner.Value != "pointer_value" {
					t.Errorf("Inner.Value = %v, want %q", s.Inner, "pointer_value")
				}
			},
		},
		{
			name:   "env dash skips field",
			data:   map[string]string{"VALUE": "kept", "SKIPME": "should_not_be_set"},
			newOut: func() any { return new(skipCfg) },
			check: func(t *testing.T, out any) {
				s := out.(*skipCfg)
				if s.Value != "kept" {
					t.Errorf("Value = %q, want %q", s.Value, "kept")
				}
				if s.SkipMe != "" {
					t.Errorf("SkipMe = %q, should be empty (skipped)", s.SkipMe)
				}
			},
		},
		{
			name:   "unexported field skipped",
			data:   map[string]string{"EXPORTED": "value", "UNEXPORTED": "should_be_ignored"},
			newOut: func() any { return new(unexportedCfg) },
			check: func(t *testing.T, out any) {
				s := out.(*unexportedCfg)
				if s.Exported != "value" {
					t.Errorf("Exported = %q, want %q", s.Exported, "value")
				}
				if s.unexported != "" {
					t.Errorf("unexported = %q, should be empty", s.unexported)
				}
			},
		},
		{
			name:   "lowercase tag matches uppercase key",
			data:   map[string]string{"DEEPSEEK_KEY": "sk-123"},
			newOut: func() any { return new(lowerKeyCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*lowerKeyCfg).APIKey; got != "sk-123" {
					t.Errorf("APIKey = %q, want %q", got, "sk-123")
				}
			},
		},
		{
			name:   "lowercase tag with prefix",
			data:   map[string]string{"APP_HOST": "localhost"},
			prefix: "app",
			newOut: func() any { return new(lowerHostCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*lowerHostCfg).Host; got != "localhost" {
					t.Errorf("Host = %q, want %q", got, "localhost")
				}
			},
		},
		{
			name:   "mixed case tag",
			data:   map[string]string{"DEEPSEEK_KEY": "val"},
			newOut: func() any { return new(mixedKeyCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*mixedKeyCfg).Key; got != "val" {
					t.Errorf("Key = %q, want %q", got, "val")
				}
			},
		},
		{
			name:   "dot-notation tag resolves to flattened key",
			data:   map[string]string{"DATABASE_HOST": "localhost"},
			newOut: func() any { return new(dotHostCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*dotHostCfg).Host; got != "localhost" {
					t.Errorf("Host = %q, want %q", got, "localhost")
				}
			},
		},
		{
			name:   "multi-level dot-notation",
			data:   map[string]string{"SERVER_HTTP_PORT": "8080"},
			newOut: func() any { return new(dotPortCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*dotPortCfg).Port; got != "8080" {
					t.Errorf("Port = %q, want %q", got, "8080")
				}
			},
		},
		{
			name:   "dot-notation with envDefault",
			data:   map[string]string{},
			newOut: func() any { return new(dotDefaultCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*dotDefaultCfg).Host; got != "127.0.0.1" {
					t.Errorf("Host = %q, want %q", got, "127.0.0.1")
				}
			},
		},
		{
			name:   "dot-notation takes precedence over default",
			data:   map[string]string{"DATABASE_HOST": "production.db"},
			newOut: func() any { return new(dotDefaultCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*dotDefaultCfg).Host; got != "production.db" {
					t.Errorf("Host = %q, want %q", got, "production.db")
				}
			},
		},
		{
			name:   "inline default: data value wins",
			data:   map[string]string{"MODE": "dev"},
			newOut: func() any { return new(inlineCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*inlineCfg).Mode; got != "dev" {
					t.Errorf("Mode = %q, want %q", got, "dev")
				}
			},
		},
		{
			name:   "inline default applies",
			data:   map[string]string{},
			newOut: func() any { return new(inlineCfg) },
			check: func(t *testing.T, out any) {
				if got := out.(*inlineCfg).Mode; got != "prod" {
					t.Errorf("Mode = %q, want %q", got, "prod")
				}
			},
		},
		{
			name:    "nested struct parse error propagates",
			data:    map[string]string{"IN_N": "abc"},
			newOut:  func() any { return new(badPlain) },
			wantErr: true,
		},
		{
			name:    "pointer-nested parse error propagates",
			data:    map[string]string{"IN_N": "abc"},
			newOut:  func() any { return new(badPtr) },
			wantErr: true,
		},
		{
			// BOUNDARY: a present-but-empty value for a non-string field
			// takes setFieldValue's early-leave branch (value == "" and
			// kind != String → no-op), exercised here through the full
			// StructInto path rather than only the setFieldValue unit.
			name:   "empty-string int field leaves zero value",
			data:   map[string]string{"PORT": ""},
			newOut: func() any { return new(testStruct) },
			check: func(t *testing.T, out any) {
				s := out.(*testStruct)
				if s.Port != 0 {
					t.Errorf("Port = %d, want 0 (empty value must not be parsed)", s.Port)
				}
				if s.Name != "" {
					t.Errorf("Name = %q, want empty (key absent)", s.Name)
				}
			},
		},
		{
			// BOUNDARY: unicode values (中文/emoji) unmarshal unchanged.
			name:   "unicode values",
			data:   map[string]string{"ZH": "中文值", "EMOJI": "🎉"},
			newOut: func() any { return new(unicodeCfg) },
			check: func(t *testing.T, out any) {
				s := out.(*unicodeCfg)
				if s.Zh != "中文值" || s.Emoji != "🎉" {
					t.Errorf("unicode fields = %q/%q, want %q/%q", s.Zh, s.Emoji, "中文值", "🎉")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.newOut()
			err := StructInto(tt.data, reflect.ValueOf(out).Elem(), tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Fatalf("StructInto() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, out)
			}
		})
	}
}

// TestStructEmptyStringRoundTripAsymmetry pins the empty-string round-trip
// asymmetry: marshal OMITS fields whose rendered value is "" (the
// `strVal != ""` gate in marshalStruct), so an explicitly-empty field does
// not survive Struct → StructInto. On the way back the key is simply absent,
// so envDefault applies (or the zero value remains). Reported, not fixed:
// the flat map format has no representation for an explicit empty value.
func TestStructEmptyStringRoundTripAsymmetry(t *testing.T) {
	type cfg struct {
		Name string `env:"NAME" envDefault:"fallback"`
	}

	m, err := Struct(cfg{Name: ""}, "")
	if err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
	if _, exists := m["NAME"]; exists {
		t.Fatalf("Struct(empty field) emitted NAME=%q; empty values should be omitted", m["NAME"])
	}

	var out cfg
	if err := StructInto(m, reflect.ValueOf(&out).Elem(), ""); err != nil {
		t.Fatalf("StructInto() error = %v", err)
	}
	if out.Name != "fallback" {
		t.Errorf("round trip Name = %q, want %q (explicit empty lost to envDefault)", out.Name, "fallback")
	}
}

// TestStructSliceCommaRoundTrip pins that []string elements containing the
// list separator or a quote mark survive the flat map format: valueToString
// quotes such elements and SplitQuotedComma reassembles them. (Before the
// fix this was silent corruption: ["a,b", "c"] came back as ["a", "b", "c"].)
func TestStructSliceCommaRoundTrip(t *testing.T) {
	type cfg struct {
		Tags []string `env:"TAGS"`
	}

	m, err := Struct(cfg{Tags: []string{"a,b", "c"}}, "")
	if err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
	if m["TAGS"] != `"a,b",c` {
		t.Fatalf("Struct() TAGS = %q, want %q (comma element quoted)", m["TAGS"], `"a,b",c`)
	}

	var out cfg
	if err := StructInto(m, reflect.ValueOf(&out).Elem(), ""); err != nil {
		t.Fatalf("StructInto() error = %v", err)
	}
	want := []string{"a,b", "c"}
	if !reflect.DeepEqual(out.Tags, want) {
		t.Errorf("round trip Tags = %v, want %v (comma element must stay one element)", out.Tags, want)
	}

	// An embedded quote mark is escaped inside the quoted element and also
	// survives the round trip.
	m2, err := Struct(cfg{Tags: []string{`he said "hi"`, "plain"}}, "")
	if err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
	var out2 cfg
	if err := StructInto(m2, reflect.ValueOf(&out2).Elem(), ""); err != nil {
		t.Fatalf("StructInto() error = %v", err)
	}
	want2 := []string{`he said "hi"`, "plain"}
	if !reflect.DeepEqual(out2.Tags, want2) {
		t.Errorf("round trip Tags = %v, want %v (quoted element with escapes)", out2.Tags, want2)
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
		// PIN: []byte renders as comma-separated decimals — the exact form
		// setSliceValue parses back. The previous raw-string rendering made
		// Struct→StructInto on the same []byte field fail.
		{"byte slice", []byte("bytes"), "98,121,116,101,115", false},
		// BOUNDARY: an empty byte slice renders as the empty string, not "0".
		{"empty byte slice", []byte{}, "", false},
		{"string slice", []string{"a", "b"}, "a,b", false},
		{"int8", int8(8), "8", false},
		{"int16", int16(16), "16", false},
		{"int32", int32(32), "32", false},
		{"int64", int64(64), "64", false},
		{"uint", uint(10), "10", false},
		{"uint8", uint8(8), "8", false},
		{"uint16", uint16(16), "16", false},
		{"uint32", uint32(32), "32", false},
		{"uint64", uint64(64), "64", false},
		{"float32", float32(3.5), "3.5", false}, // exact-representable float32 value
		// Formatted at the field's 32-bit precision: the old 64-bit rendering
		// printed "0.10000000149011612" for float32(0.1) (D-002/M-5).
		{"float32 inexact", float32(0.1), "0.1", false},
		{"nil pointer", (*string)(nil), "", false},
		{"non-nil pointer dereferences", intptr(42), "42", false},
		{"invalid value (nil interface)", nil, "", false},
		{"TextMarshaler (time.Time)", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "2026-01-02T03:04:05Z", false},
		{"json.Marshaler", json.RawMessage(`{"a":1}`), `{"a":1}`, false},
		{"empty string slice", []string{}, "", false},
		{"unsupported slice", []int{1, 2, 3}, "", true},
		{"unsupported map", map[string]string{"a": "b"}, "", true},
		// BOUNDARY: the default unsupported-kind branch (previously only
		// map/chan were covered): array and complex kinds carry no
		// marshaler and must error instead of silently rendering.
		{"unsupported array", [2]int{1, 2}, "", true},
		{"unsupported complex", complex128(1 + 2i), "", true},
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

// intptr returns a pointer to v; used to express pointer-typed expectations
// in the setFieldValue table.
func intptr(v int) *int { return &v }

// TestSetFieldValue covers the scalar set paths plus the rare branches
// folded in from the former TestSetFieldValue_RarePaths: TextUnmarshaler
// delegation, duration/float parse errors, slice passthrough, and the
// float-slice parse error.
func TestSetFieldValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		target  interface{}
		want    interface{}
		wantErr bool
		check   func(t *testing.T, val reflect.Value) // optional custom verification (replaces the want comparison)
	}{
		{"string", "hello", new(string), "hello", false, nil},
		{"int", "42", new(int), 42, false, nil},
		{"bool", "true", new(bool), true, false, nil},
		{"float", "3.14", new(float64), 3.14, false, nil},
		{"empty value for non-string leaves zero value", "", new(int), 0, false, nil},
		{"pointer to int", "42", new(*int), intptr(42), false, nil},
		{"uint types", "42", new(uint), uint(42), false, nil},
		{"float32 type", "3.14", new(float32), float32(3.14), false, nil},
		// Parsed at the field's 32-bit width: "1e39" overflows float32 and
		// must error instead of silently becoming +Inf (D-002/M-5).
		{"float32 overflow", "1e39", new(float32), 0, true, nil},
		{"invalid int", "not_a_number", new(int), 0, true, nil},
		{"invalid bool", "not_a_bool", new(bool), false, true, nil},
		{"unsupported type", "value", new(map[string]string), nil, true, nil},
		// Narrow-width fields must reject out-of-range values instead of
		// silently truncating them (300 → 44 in int8, 256 → 0 in uint8).
		{"int8 overflow", "300", new(int8), 0, true, nil},
		{"int16 overflow", "40000", new(int16), 0, true, nil},
		{"uint8 overflow", "256", new(uint8), 0, true, nil},
		{"uint16 overflow", "70000", new(uint16), 0, true, nil},
		// Folded rare paths:
		{"TextUnmarshaler delegates", "hello", new(textUnmarshalTarget), nil, false,
			func(t *testing.T, val reflect.Value) {
				if got := val.Interface().(textUnmarshalTarget).got; got != "hello" {
					t.Errorf("UnmarshalText received %q, want %q", got, "hello")
				}
			}},
		{"time.Duration parse error", "not-a-duration", new(time.Duration), 0, true, nil},
		{"float parse error", "abc", new(float64), 0, true, nil},
		{"slice passthrough splits", "a,b", new([]string), []string{"a", "b"}, false, nil},
		{"float slice parse error", "1.5,x", new([]float64), nil, true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.target).Elem()
			err := setFieldValue(val, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setFieldValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil {
				tt.check(t, val)
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
		{"int slice with spaces", " 1 , 2 , 3 ", new([]int), []int{1, 2, 3}, false},
		{"uint slice", "1,2,3", new([]uint), []uint{1, 2, 3}, false},
		{"float slice", "1.1,2.2,3.3", new([]float64), []float64{1.1, 2.2, 3.3}, false},
		// Boundary: empty input yields an empty (possibly nil) slice; the
		// resulting value is unspecified beyond being empty, so only the
		// error is asserted.
		{"empty", "", new([]string), nil, false},
		{"invalid int slice element", "1,not_a_number,3", new([]int), nil, true},
		{"invalid bool slice element", "true,not_a_bool", new([]bool), nil, true},
		{"unsupported slice element type", "value", new([]map[string]string), nil, true},
		// Slice elements follow the same bit-width rule as scalar fields.
		{"int8 slice element overflow", "300", new([]int8), nil, true},
		{"uint8 slice element overflow", "256", new([]uint8), nil, true},
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

type marshalBadField struct {
	Ch chan int
}

type marshalNestedBad struct {
	Inner marshalBadField
}

type marshalPtrNestedBad struct {
	Inner *marshalBadField
}

// TestMarshalStruct_TopLevelPointer covers Struct invoked with a non-nil
// struct pointer (the val.Elem() dereference path).
func TestMarshalStruct_TopLevelPointer(t *testing.T) {
	type ptrTarget struct {
		Host string `env:"HOST"`
	}
	got, err := Struct(&ptrTarget{Host: "h1"}, "")
	if err != nil {
		t.Fatalf("Struct(&struct) error = %v", err)
	}
	if got["HOST"] != "h1" {
		t.Errorf("result[HOST] = %q, want %q", got["HOST"], "h1")
	}
}

// TestStructIntoNestedPrefixSymmetry pins the marshal/unmarshal prefix
// symmetry: nested structs map to PARENT_CHILD keys in both directions,
// matching the JSON/YAML flatteners. Regression: StructInto previously passed
// no prefix for untagged nested structs (looking up HOST instead of
// INNER_HOST), so flattened nested values were silently dropped.
func TestStructIntoNestedPrefixSymmetry(t *testing.T) {
	type Inner struct {
		Host string
	}
	type Outer struct {
		Inner Inner
		Name  string
	}

	envMap, err := Struct(Outer{Inner: Inner{Host: "db.example.com"}, Name: "app"}, "")
	if err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
	if envMap["INNER_HOST"] != "db.example.com" || envMap["NAME"] != "app" {
		t.Fatalf("Struct() = %v, want INNER_HOST=db.example.com NAME=app", envMap)
	}

	var out Outer
	if err := StructInto(envMap, reflect.ValueOf(&out).Elem(), ""); err != nil {
		t.Fatalf("StructInto() error = %v", err)
	}
	if out.Inner.Host != "db.example.com" {
		t.Errorf("out.Inner.Host = %q, want %q (nested value was silently dropped)", out.Inner.Host, "db.example.com")
	}
	if out.Name != "app" {
		t.Errorf("out.Name = %q, want %q", out.Name, "app")
	}
}

// TestStructIntoMultiLevelTaggedPrefixAccumulates covers tagged multi-level
// nesting: marshal emits M_I_HOST, so unmarshal must accumulate prefixes and
// look up the same key (regression: it previously looked up I_HOST).
func TestStructIntoMultiLevelTaggedPrefixAccumulates(t *testing.T) {
	type Leaf struct {
		Host string `env:"HOST"`
	}
	type Mid struct {
		Leaf Leaf `env:"I"`
	}
	type Top struct {
		Mid Mid `env:"M"`
	}

	envMap, err := Struct(Top{Mid: Mid{Leaf: Leaf{Host: "h"}}}, "")
	if err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
	if envMap["M_I_HOST"] != "h" {
		t.Fatalf("Struct() = %v, want M_I_HOST=h", envMap)
	}

	var out Top
	if err := StructInto(envMap, reflect.ValueOf(&out).Elem(), ""); err != nil {
		t.Fatalf("StructInto() error = %v", err)
	}
	if out.Mid.Leaf.Host != "h" {
		t.Errorf("out.Mid.Leaf.Host = %q, want %q", out.Mid.Leaf.Host, "h")
	}
}

// errTextMarshaler and errJSONMarshaler are deliberately string-kind (not
// structs): a struct field of struct kind is recursed into by marshalStruct
// and never reaches valueToString's marshaler checks.
type errTextMarshaler string

func (errTextMarshaler) MarshalText() ([]byte, error) { return nil, errors.New("text boom") }

type errJSONMarshaler string

func (errJSONMarshaler) MarshalJSON() ([]byte, error) { return nil, errors.New("json boom") }

// TestMarshalStruct_MarshalerErrors covers TextMarshaler and json.Marshaler
// implementations that fail: the error must surface instead of being
// swallowed.
func TestMarshalStruct_MarshalerErrors(t *testing.T) {
	t.Run("TextMarshaler error", func(t *testing.T) {
		type host struct {
			F errTextMarshaler
		}
		if _, err := Struct(host{}, ""); err == nil {
			t.Error("Struct(failing TextMarshaler) error = nil, want error")
		}
	})

	t.Run("json.Marshaler error", func(t *testing.T) {
		type host struct {
			F errJSONMarshaler
		}
		if _, err := Struct(host{}, ""); err == nil {
			t.Error("Struct(failing json.Marshaler) error = nil, want error")
		}
	})
}

type textUnmarshalTarget struct {
	got string
}

func (t *textUnmarshalTarget) UnmarshalText(b []byte) error {
	t.got = string(b)
	return nil
}

// TestBuildPrefixedKey_LongKeys covers the fallback concatenation branch
// taken when prefix+key exceeds the 64-byte stack buffer.
func TestBuildPrefixedKey_LongKeys(t *testing.T) {
	prefix := strings.Repeat("P", 40)
	key := strings.Repeat("K", 40)
	if got := buildPrefixedKey(prefix, key); got != prefix+"_"+key {
		t.Errorf("buildPrefixedKey(long) = %q, want %q_%q", got, prefix, key)
	}
}

// TestStructIntoSelfReferential guards the recursion depth cap on the
// unmarshal side: StructInto's pointer branch allocates nil pointer fields
// (reflect.New) before recursing, so a self-referential type previously
// recursed and allocated forever — the process died with an unrecoverable
// `fatal error: out of memory` (verified empirically before the cap). The
// embedded-pointer variant (type Node struct{ *Node }) flows through the same
// pointer branch, so the depth cap must catch it too.
func TestStructIntoSelfReferential(t *testing.T) {
	t.Run("named field", func(t *testing.T) {
		type node struct {
			Next *node
			Name string
		}
		var n node
		err := StructInto(map[string]string{"NAME": "x"}, reflect.ValueOf(&n).Elem(), "")
		if err == nil {
			t.Fatal("StructInto(self-referential type) error = nil, want depth-limit error")
		}
		var me *MarshalError
		if !errors.As(err, &me) {
			t.Fatalf("StructInto(self-referential type) error = %T, want *MarshalError", err)
		}
	})

	t.Run("embedded pointer", func(t *testing.T) {
		type Node struct {
			*Node
			Name string
		}
		var n Node
		err := StructInto(map[string]string{"NAME": "x"}, reflect.ValueOf(&n).Elem(), "")
		if err == nil {
			t.Fatal("StructInto(embedded self-reference) error = nil, want depth-limit error")
		}
		var me *MarshalError
		if !errors.As(err, &me) {
			t.Fatalf("StructInto(embedded self-reference) error = %T, want *MarshalError", err)
		}
	})
}

// TestErrStructDepthExceededField pins the empty-prefix fallback directly:
// recursion at the root has accumulated no flattened key path yet, so the
// error must report Field "value" instead of an empty field. (Root-depth
// recursion is unreachable through the public Struct/StructInto entry
// points, which always start with an empty prefix at depth 0.)
func TestErrStructDepthExceededField(t *testing.T) {
	var me *MarshalError
	if err := errStructDepthExceeded(""); !errors.As(err, &me) {
		t.Fatalf("errStructDepthExceeded(\"\") = %T, want *MarshalError", err)
	} else if me.Field != "value" {
		t.Errorf("Field = %q, want \"value\" (empty prefix falls back)", me.Field)
	}
	if err := errStructDepthExceeded("A_B"); !errors.As(err, &me) {
		t.Fatalf("errStructDepthExceeded(prefix) = %T, want *MarshalError", err)
	} else if me.Field != "A_B" {
		t.Errorf("Field = %q, want the accumulated prefix passed through", me.Field)
	}
}

// TestImplementsTextMarshalerGuards pins the CanInterface guard directly:
// values reflection refuses to export (an unexported field read without a
// pointer to the parent) must report false instead of panicking.
func TestImplementsTextMarshalerGuards(t *testing.T) {
	unexported := reflect.ValueOf(struct{ t time.Time }{}).Field(0)
	if unexported.CanInterface() {
		t.Fatal("unexported field unexpectedly CanInterface — pick a different construction")
	}
	if implementsTextMarshaler(unexported) {
		t.Error("implementsTextMarshaler(unexported time.Time) = true, want false")
	}
	if !implementsTextMarshaler(reflect.ValueOf(time.Now())) {
		t.Error("implementsTextMarshaler(time.Time) = false, want true")
	}
}

// TestImplementsTextUnmarshalerGuards pins the addressability/interface
// guards directly for both field shapes: pointer fields need CanInterface,
// and value fields need an addressable field whose Addr() can Interface().
func TestImplementsTextUnmarshalerGuards(t *testing.T) {
	// Pointer-kind field that cannot Interface(): unexported pointer field.
	if implementsTextUnmarshaler(reflect.ValueOf(struct{ p *time.Time }{}).Field(0)) {
		t.Error("implementsTextUnmarshaler(unexported pointer field) = true, want false")
	}

	// Value-kind field that is not addressable (field of a non-pointer value).
	if implementsTextUnmarshaler(reflect.ValueOf(struct{ T time.Time }{}).Field(0)) {
		t.Error("implementsTextUnmarshaler(unaddressable field) = true, want false")
	}

	// Addressable but unexported: Addr().CanInterface() is false.
	v := struct{ t time.Time }{}
	if implementsTextUnmarshaler(reflect.ValueOf(&v).Elem().Field(0)) {
		t.Error("implementsTextUnmarshaler(addressable unexported field) = true, want false")
	}

	// The positive path: an addressable exported time.Time field.
	w := struct{ T time.Time }{}
	if !implementsTextUnmarshaler(reflect.ValueOf(&w).Elem().Field(0)) {
		t.Error("implementsTextUnmarshaler(addressable time.Time) = false, want true")
	}
}

// TestStructCyclicValue guards the recursion depth cap on the marshal side:
// marshalStruct recurses into non-nil pointer fields, so a cyclic value
// (n.Next = n) previously recursed forever — same unrecoverable fatal OOM
// crash as the unmarshal side.
func TestStructCyclicValue(t *testing.T) {
	type node struct {
		Next *node
		Name string
	}
	n := &node{Name: "root"}
	n.Next = n
	m, err := Struct(n, "")
	if err == nil {
		t.Fatalf("Struct(cyclic value) error = nil (returned %d keys)", len(m))
	}
	var me *MarshalError
	if !errors.As(err, &me) {
		t.Fatalf("Struct(cyclic value) error = %T, want *MarshalError", err)
	}
}

// TestStructDeepNestingSanity pins that legitimately deep (but acyclic)
// nesting below the cap still round-trips — the depth cap must only reject
// pathological shapes, not real ones.
func TestStructDeepNestingSanity(t *testing.T) {
	type l3 struct {
		Leaf string `env:"LEAF"`
	}
	type l2 struct {
		L3 l3 `env:"L3"`
	}
	type l1 struct {
		L2 l2 `env:"L2"`
	}
	m, err := Struct(l1{L2: l2{L3: l3{Leaf: "v"}}}, "")
	if err != nil {
		t.Fatalf("Struct(deep acyclic) error = %v", err)
	}
	if got, ok := m["L2_L3_LEAF"]; !ok || got != "v" {
		t.Errorf("Struct(deep acyclic) m[L2_L3_LEAF] = %q, %v; want \"v\", true", got, ok)
	}
}

// TestStructRoundTrip_ByteArrayAndWhitespace pins the marshal/unmarshal
// representation fixes: []byte round-trips as comma-separated decimals
// (matching the parse side; the raw-string rendering made the same field fail
// to read back), and string-slice elements with significant leading/trailing
// whitespace survive (marshal quotes them; unmarshal trims only unquoted
// elements).
func TestStructRoundTrip_ByteArrayAndWhitespace(t *testing.T) {
	type doc struct {
		Raw  []byte
		Tags []string
	}
	in := doc{Raw: []byte("hi!"), Tags: []string{"a ", " b", "c d"}}

	out, err := Struct(in, "")
	if err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
	if got := out["RAW"]; got != "104,105,33" {
		t.Errorf("RAW = %q, want comma-decimal %q", got, "104,105,33")
	}

	var back doc
	if err := StructInto(out, reflect.ValueOf(&back).Elem(), ""); err != nil {
		t.Fatalf("StructInto() error = %v", err)
	}
	if string(back.Raw) != "hi!" {
		t.Errorf("Raw = %v, want %q", back.Raw, "hi!")
	}
	if len(back.Tags) != 3 || back.Tags[0] != "a " || back.Tags[1] != " b" || back.Tags[2] != "c d" {
		t.Errorf("Tags = %q, want %q (whitespace must survive the round trip)", back.Tags, in.Tags)
	}
}

// TestStructInto_LooseBoolGrammar pins the library-wide boolean grammar in
// the struct mapper: yes/no/on/off parse exactly as they do through GetBool
// (strict strconv semantics previously made DEBUG=yes an error here).
func TestStructInto_LooseBoolGrammar(t *testing.T) {
	// Derive the key spelling from the marshal side so the pin does not
	// depend on the field-name convention.
	out, err := Struct(struct{ Flag bool }{true}, "")
	if err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("Struct() produced %d keys, want 1", len(out))
	}
	var key string
	var val string
	for k, v := range out {
		key, val = k, v
	}
	if val != "true" {
		t.Fatalf("marshal of true = %q, want %q", val, "true")
	}

	for input, want := range map[string]bool{
		"yes": true, "no": false, "on": true, "off": false,
		"enabled": true, "disabled": false, "1": true, "0": false,
		"TRUE": true, "False": false,
	} {
		var s struct{ Flag bool }
		out[key] = input
		if err := StructInto(out, reflect.ValueOf(&s).Elem(), ""); err != nil {
			t.Errorf("StructInto(%q) error = %v", input, err)
			continue
		}
		if s.Flag != want {
			t.Errorf("StructInto(%q) = %v, want %v", input, s.Flag, want)
		}
	}

	out[key] = "maybe"
	if err := StructInto(out, reflect.ValueOf(&struct{ Flag bool }{}).Elem(), ""); err == nil {
		t.Error(`StructInto("maybe") succeeded, want invalid-boolean error`)
	}
}
