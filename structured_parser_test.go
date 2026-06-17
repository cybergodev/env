package env

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// This file exercises structuredParserConfig.validateResult (structured_parser.go),
// the shared validation gate used by both the JSON and YAML parsers. The function
// is otherwise only reached on happy paths by TestJSONParser_EdgeCases and
// TestYAMLParser_EdgeCases, leaving three of its four branches and the
// required-keys path uncovered. Each case below drives a real LoadFiles parse so
// the behavior is verified end-to-end through the public API.

// makeManyYAML returns a YAML document with n distinct scalar keys, used to
// exceed a small MaxVariables cap.
func makeManyYAML(n int) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString("KEY_")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(": v\n")
	}
	return sb.String()
}

func TestStructuredParser_ValidateResult(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		content   string
		configFn  func(*Config)
		wantErr   bool
		wantField string // expected ValidationError.Field; "" skips the field check
	}{
		{
			name:      "yaml max variables exceeded",
			filename:  "many.yaml",
			content:   makeManyYAML(50),
			configFn:  func(c *Config) { c.MaxVariables = 10 },
			wantErr:   true,
			wantField: "variables",
		},
		{
			name:      "json invalid key character rejected",
			filename:  "badkey.json",
			content:   `{"KEY!": "v"}`,
			wantErr:   true,
			wantField: "key",
		},
		{
			name:      "yaml invalid key (space) rejected",
			filename:  "badkey.yaml",
			content:   "\"bad key\": v\n",
			wantErr:   true,
			wantField: "key",
		},
		{
			// A value longer than MaxValueLength exercises the ValidateValues ->
			// ValidateValue error path. (DefaultConfig already sets
			// ValidateValues=true; we only shrink the length cap.)
			name:      "json value exceeds MaxValueLength",
			filename:  "badval.json",
			content:   `{"K": "abcdefghij"}`,
			configFn:  func(c *Config) { c.MaxValueLength = 3 },
			wantErr:   true,
			wantField: "value",
		},
		{
			name:     "json required key missing",
			filename: "reqmissing.json",
			content:  `{"PRESENT": "v"}`,
			configFn: func(c *Config) { c.RequiredKeys = []string{"MISSING"} },
			wantErr:  true,
		},
		{
			name:     "json required key present succeeds",
			filename: "reqok.json",
			content:  `{"REQUIRED": "v"}`,
			configFn: func(c *Config) { c.RequiredKeys = []string{"REQUIRED"} },
			wantErr:  false,
		},
		{
			name:     "yaml happy path parses cleanly",
			filename: "ok.yaml",
			content:  "NAME: env\nPORT: \"8080\"\n",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			fs := newTestFileSystem()
			fs.files[tt.filename] = tt.content
			cfg.FileSystem = fs
			if tt.configFn != nil {
				tt.configFn(&cfg)
			}

			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer loader.Close()

			err = loader.LoadFiles(tt.filename)
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoadFiles() error = nil, want a validation error")
				}
				if tt.wantField != "" {
					var ve *ValidationError
					if !errors.As(err, &ve) {
						t.Errorf("error is not a *ValidationError: %T (%v)", err, err)
					} else if ve.Field != tt.wantField {
						t.Errorf("ValidationError.Field = %q, want %q", ve.Field, tt.wantField)
					}
				}
				return
			}
			if err != nil {
				t.Errorf("LoadFiles() error = %v, want nil", err)
			}
		})
	}
}
