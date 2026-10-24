package env

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
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

// rmLoad loads one file (name + content) through the public API with a
// DefaultConfig over the in-memory file system, failing the test on any load
// error. Shared by the default-limits and colon-space suites.
func rmLoad(t *testing.T, filename, content string) *Loader {
	t.Helper()
	fs := newTestFileSystem()
	fs.files[filename] = content

	cfg := DefaultConfig()
	cfg.FileSystem = fs
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = loader.Close() })
	if err := loader.LoadFiles(filename); err != nil {
		t.Fatalf("LoadFiles(%q) error = %v", filename, err)
	}
	return loader
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
			// Fail-closed baseline (D-002/M-1): control characters are
			// rejected even with ValidateValues disabled — the .env parse
			// path has never had a switch for them.
			name:      "json control char rejected with ValidateValues disabled",
			filename:  "nulval.json",
			content:   "{\"K\": \"bad\\u0000value\"}",
			configFn:  func(c *Config) { c.ValidateValues = false },
			wantErr:   true,
			wantField: "value",
		},
		{
			name:      "yaml control char rejected with ValidateValues disabled",
			filename:  "nulval.yaml",
			content:   "K: \"bad\x01value\"\n",
			configFn:  func(c *Config) { c.ValidateValues = false },
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
			defer func() { _ = loader.Close() }() // best-effort cleanup

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

// TestStructuredParser_DefaultLimitsAccommodateRealisticConfigs pins the
// DefaultConfig limits sized in FIX-008: structured configs flatten into
// compound keys (65-128 chars), nest 10-20 levels (OpenAPI-style documents),
// and credential-export files reach hundreds of entries at 2-8KB each. Every
// case below failed under the previous defaults (64-char keys, depth 10,
// 500 variables, 2MB files). The long-credential subtest additionally pins
// the DefaultConfig line limit: single-line credential values (browser
// cookie exports, JWTs) commonly reach 2-8KB and must load under DefaultConfig
// without custom limits — its ~2119-char line would have been rejected by a
// 1024-char default.
func TestStructuredParser_DefaultLimitsAccommodateRealisticConfigs(t *testing.T) {
	t.Run("89-char flattened key from nested map", func(t *testing.T) {
		loader := rmLoad(t, "keylen.yaml",
			"providers:\n  gateway:\n    upstream:\n      pools:\n        primary:\n          connection:\n            max_idle_seconds: 30\n")
		if got := loader.GetString("providers.gateway.upstream.pools.primary.connection.max_idle_seconds"); got != "30" {
			t.Errorf("GetString(long flattened key) = %q, want %q", got, "30")
		}
	})

	t.Run("12-level nesting", func(t *testing.T) {
		var sb strings.Builder
		for i := 0; i < 12; i++ {
			sb.WriteString(strings.Repeat("  ", i))
			fmt.Fprintf(&sb, "level%d:\n", i)
		}
		sb.WriteString(strings.Repeat("  ", 12) + "leaf: value\n")

		loader := rmLoad(t, "deep.yaml", sb.String())
		if got := loader.GetString("level0.level1.level2.level3.level4.level5.level6.level7.level8.level9.level10.level11.leaf"); got != "value" {
			t.Errorf("GetString(deep leaf) = %q, want %q", got, "value")
		}
	})

	t.Run("600 variables", func(t *testing.T) {
		var sb strings.Builder
		for i := 0; i < 600; i++ {
			fmt.Fprintf(&sb, "domain%03d.com: cookie-value-%d\n", i, i)
		}
		loader := rmLoad(t, "many.yaml", sb.String())
		if got := loader.Len(); got != 600 {
			t.Errorf("Len() = %d, want 600", got)
		}
	})

	t.Run("2.25MB file of credential-sized values", func(t *testing.T) {
		var sb strings.Builder
		filler := strings.Repeat("x", 5000)
		for i := 0; i < 450; i++ {
			fmt.Fprintf(&sb, "domain%03d.com: %s\n", i, filler)
		}
		loader := rmLoad(t, "big.yaml", sb.String())
		if got := loader.Len(); got != 450 {
			t.Errorf("Len() = %d, want 450", got)
		}
	})

	t.Run("single-line credential value fits default line limit", func(t *testing.T) {
		value := "session=" + strings.Repeat("a", 2100) + "; flag=1"
		loader := rmLoad(t, "config.yaml", "apnews.com: "+value)
		if got := loader.GetString("apnews.com"); got != value {
			t.Errorf("GetString(\"apnews.com\") length = %d, want %d", len(got), len(value))
		}
	})
}

// TestYAMLParser_ColonSpaceInValues verifies end-to-end that a colon+space
// inside a value — most commonly an embedded JSON blob with ": " separators,
// like a cookie carrying g_state={"i_l": 0} — no longer breaks parsing.
// Previously the lexer misread the value as a key and the resulting bogus
// key failed validation, rejecting the whole file.
func TestYAMLParser_ColonSpaceInValues(t *testing.T) {
	t.Run("cookie string embedding spaced JSON stays one value", func(t *testing.T) {
		want := `Code=abc; Visitor=v; g_state={"i_l": 0, "i_e": {"enable_itp_optimization": 24}, "i_t": 1792402790803}`
		loader := rmLoad(t, "config.yaml", "apnews.com: "+want+"\n")
		if got := loader.GetString("apnews.com"); got != want {
			t.Errorf("GetString(\"apnews.com\") = %q, want %q", got, want)
		}
	})

	t.Run("value that is spaced JSON flattens inline", func(t *testing.T) {
		loader := rmLoad(t, "config.yaml", "g_state: {\"i_l\": 0, \"i_e\": {\"x\": 1}, \"list\": [1, 2]}\n")
		want := map[string]string{
			"G_STATE_I_L":    "0",
			"G_STATE_I_E_X":  "1",
			"G_STATE_LIST_0": "1",
			"G_STATE_LIST_1": "2",
		}
		for k, w := range want {
			if got := loader.GetString(k); got != w {
				t.Errorf("GetString(%q) = %q, want %q", k, got, w)
			}
		}
	})

	t.Run("colon and hash inside JSON strings are preserved", func(t *testing.T) {
		want := `{"note": "a: b # c", "brace": "}"}`
		loader := rmLoad(t, "config.yaml", "cfg: "+want+"\n")
		if got := loader.GetString("CFG_NOTE"); got != "a: b # c" {
			t.Errorf("GetString(\"CFG_NOTE\") = %q, want %q", got, "a: b # c")
		}
		if got := loader.GetString("CFG_BRACE"); got != "}" {
			t.Errorf("GetString(\"CFG_BRACE\") = %q, want %q", got, "}")
		}
	})

	t.Run("comment after closed flow region", func(t *testing.T) {
		loader := rmLoad(t, "config.yaml", "g_state: {\"i_l\": 0} # trailing note\n")
		if got := loader.GetString("G_STATE_I_L"); got != "0" {
			t.Errorf("GetString(\"G_STATE_I_L\") = %q, want %q", got, "0")
		}
	})

	t.Run("depth-0 colon-space is rejected, not split", func(t *testing.T) {
		// A ": " at flow depth 0 still terminates the scalar, but the line no
		// longer splits into two entries: a plain value may not carry ": ",
		// so "outer: inner: value" is a mapping error (PyYAML reports
		// "mapping values are not allowed here"). Previously OUTER silently
		// became "" and INNER "value" — one entry's data split across two
		// keys with no error. Quoting keeps the value literal.
		cfg := DefaultConfig()
		fs := newTestFileSystem()
		fs.files["config.yaml"] = "outer: inner: value\n"
		cfg.FileSystem = fs
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup
		if err := loader.LoadFiles("config.yaml"); err == nil {
			t.Fatal("LoadFiles() error = nil, want an error for an unquoted ': ' in a plain value")
		}

		quoted := rmLoad(t, "config.yaml", "outer: 'inner: value'\n")
		if got := quoted.GetString("OUTER"); got != "inner: value" {
			t.Errorf("GetString(\"OUTER\") = %q, want %q", got, "inner: value")
		}
		if got := quoted.GetString("INNER"); got != "" {
			t.Errorf("GetString(\"INNER\") = %q, want empty (a quoted value must not split)", got)
		}
	})
}

// TestReadAllInto covers the buffer-growth and read-error paths of the
// structured-parser file reader directly.
func TestReadAllInto(t *testing.T) {
	t.Run("grows buffer beyond initial capacity", func(t *testing.T) {
		content := strings.Repeat("x", 2048)
		buf, err := readAllInto(make([]byte, 0, 64), strings.NewReader(content))
		if err != nil {
			t.Fatalf("readAllInto() error = %v", err)
		}
		if string(buf) != content {
			t.Errorf("readAllInto() read %d bytes, want %d", len(buf), len(content))
		}
	})

	t.Run("read error is returned", func(t *testing.T) {
		wantErr := errors.New("disk failure")
		// Same contract as "returns partially-filled buffer on error" below,
		// kept for the plain errors.Is path with a non-nil partial prefix.
		_, err := readAllInto(make([]byte, 0, 64), io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(wantErr)))
		if !errors.Is(err, wantErr) {
			t.Errorf("readAllInto() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("empty reader returns empty buffer", func(t *testing.T) {
		buf, err := readAllInto(make([]byte, 0, 64), strings.NewReader(""))
		if err != nil {
			t.Fatalf("readAllInto() error = %v", err)
		}
		if len(buf) != 0 {
			t.Errorf("readAllInto() = %d bytes, want 0", len(buf))
		}
	})

	t.Run("zero-capacity buffer grows from the 512-byte floor", func(t *testing.T) {
		buf, err := readAllInto(make([]byte, 0), strings.NewReader("hello"))
		if err != nil {
			t.Fatalf("readAllInto() error = %v", err)
		}
		if string(buf) != "hello" {
			t.Errorf("readAllInto() = %q, want %q", buf, "hello")
		}
	})

	// Pins the readAllInto contract the structured-parse error path relies
	// on: on error it must return the partially-filled buffer, not nil, so
	// the caller can clear it before pooling. Regression: the error path in
	// structuredParseResult previously pooled the untouched original pointer,
	// whose zero length made clear() a no-op and left raw file contents
	// resident in readerBufferPool.
	t.Run("returns partially-filled buffer on error", func(t *testing.T) {
		secret := []byte("PASSWORD=supersecret-value")
		wantErr := errors.New("read failed")
		buf := make([]byte, 0, 64)

		data, err := readAllInto(buf, &partialReader{data: secret, err: wantErr})
		if !errors.Is(err, wantErr) {
			t.Fatalf("readAllInto() error = %v, want %v", err, wantErr)
		}
		if data == nil {
			t.Fatal("readAllInto() returned nil buffer on error; caller cannot clear pooled memory")
		}
		if string(data) != string(secret) {
			t.Errorf("partial data = %q, want %q", data, secret)
		}
	})
}

// TestReaderBufferPool_Boundary covers the defensive paths of the structured
// parser's reader buffer pool: nil puts, oversized buffers refused entry, and
// the fallback allocation when the pool returns an unexpected type.
func TestReaderBufferPool_Boundary(t *testing.T) {
	t.Run("nil buffer is ignored", func(t *testing.T) {
		putReaderBuffer(nil) // must not panic
	})

	t.Run("oversized buffer is not pooled", func(t *testing.T) {
		big := make([]byte, 0, maxPooledReaderBufferSize+1)
		putReaderBuffer(&big)
		// No observable assertion: the contract is that the oversized buffer is
		// simply dropped, and the pool's buffers stay ≤ maxPooledReaderBufferSize.
	})

	t.Run("unexpected pool type falls back to a fresh buffer", func(t *testing.T) {
		readerBufferPool.Put(new(int)) // poison the pool with a foreign type
		buf := getReaderBuffer()
		if cap(*buf) != 64*1024 {
			t.Errorf("fallback buffer capacity = %d, want %d", cap(*buf), 64*1024)
		}
		putReaderBuffer(buf) // restore a well-typed buffer to the pool
	})
}

// TestStructuredParseResult_Boundaries drives the remaining structured-parse
// branches directly: generic (non-limit) read errors passed through unwrapped,
// the ErrLineTooLong branch (minified single-line JSON above MaxLineLength),
// MaxKeyLength enforcement, and the audit-enabled duration log on success.
func TestStructuredParseResult_Boundaries(t *testing.T) {
	t.Run("generic read error is returned as-is", func(t *testing.T) {
		wantErr := errors.New("device not ready")
		c := &structuredParserConfig{config: DefaultConfig(), auditor: &mockFullAuditLogger{}}

		_, err := c.structuredParseResult(&partialReader{data: []byte("{"), err: wantErr}, "f.json", "JSON",
			func([]byte) (map[string]string, error) { return nil, nil })
		if !errors.Is(err, wantErr) {
			t.Errorf("structuredParseResult() error = %v, want the raw %v", err, wantErr)
		}
	})

	// The ErrLineTooLong branch of structuredParseResult was previously
	// untested (only ErrFileTooLarge was). A minified JSON document is ONE
	// line for the MaxLineLength check regardless of format; the failure is
	// wrapped in a FileError with Op "size_check" whose chain carries
	// ErrLineTooLong (the sentinel-level wiring of this same path is pinned
	// in errors_test.go TestSentinelErrorWiring).
	t.Run("minified single-line JSON above MaxLineLength wraps ErrLineTooLong", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxLineLength = 64
		c := &structuredParserConfig{config: cfg, auditor: &mockFullAuditLogger{}}

		minified := `{"KEY":"` + strings.Repeat("x", 200) + `"}` // one line, > MaxLineLength
		_, err := c.structuredParseResult(strings.NewReader(minified), "f.json", "JSON",
			func([]byte) (map[string]string, error) { return nil, nil })
		if err == nil {
			t.Fatal("structuredParseResult() error = nil, want ErrLineTooLong")
		}
		var fe *FileError
		if !errors.As(err, &fe) {
			t.Fatalf("structuredParseResult() error = %T (%v), want *FileError", err, err)
		}
		if fe.Op != "size_check" {
			t.Errorf("FileError.Op = %q, want %q", fe.Op, "size_check")
		}
		if !errors.Is(err, ErrLineTooLong) {
			t.Errorf("errors.Is(err, ErrLineTooLong) = false, err = %v", err)
		}
	})

	t.Run("key exceeding MaxKeyLength is rejected", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxKeyLength = 4
		c := &structuredParserConfig{config: cfg, auditor: &mockFullAuditLogger{}}

		_, err := c.structuredParseResult(strings.NewReader(`{}`), "f.json", "JSON",
			func([]byte) (map[string]string, error) { return map[string]string{"TOOLONGKEY": "v"}, nil })
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Rule != "max_length" {
			t.Errorf("structuredParseResult() error = %v, want ValidationError rule max_length", err)
		}
	})

	t.Run("audit enabled logs parse duration on success", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.AuditEnabled = true
		factory := cfg.buildComponentFactory()
		defer func() { _ = factory.Close() }() // best-effort cleanup
		auditor := &mockFullAuditLogger{}
		c := &structuredParserConfig{config: cfg, validator: factory.Validator(), auditor: auditor}

		result, err := c.structuredParseResult(strings.NewReader(`{"K":"v"}`), "f.json", "JSON",
			func(data []byte) (map[string]string, error) { return map[string]string{"K": "v"}, nil })
		if err != nil {
			t.Fatalf("structuredParseResult() error = %v", err)
		}
		if result["K"] != "v" {
			t.Errorf("result[K] = %q, want %q", result["K"], "v")
		}
		found := false
		for _, entry := range auditor.logs {
			if entry == "LogWithDuration" {
				found = true
			}
		}
		if !found {
			t.Errorf("audit log entries = %v, want a LogWithDuration entry", auditor.logs)
		}
	})
}

// partialReader yields the given bytes once, then fails with a custom error.
type partialReader struct {
	data []byte
	err  error
	done bool
}

func (r *partialReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	n := copy(p, r.data)
	return n, r.err
}

// TestStructuredParseErrorPathClearsPooledBuffer verifies end to end that a
// failing structured parse leaves no raw file contents in the reader buffer
// pool: after the error, the next pooled buffer contains only zeroes.
func TestStructuredParseErrorPathClearsPooledBuffer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFileSize = 32
	cfg.MaxLineLength = 1024

	// Content longer than MaxFileSize so the read fails mid-file with
	// ErrFileTooLarge after ~32 bytes of secret-bearing content.
	oversized := []byte("PASSWORD=supersecret-payload-that-exceeds-the-limit")
	c := &structuredParserConfig{
		config:  cfg,
		auditor: &mockFullAuditLogger{},
	}
	flattenStub := func([]byte) (map[string]string, error) { return nil, nil }

	if _, err := c.structuredParseResult(bytes.NewReader(oversized), "test.json", "JSON", flattenStub); err == nil {
		t.Fatal("expected ErrFileTooLarge from oversized input")
	}

	// The buffer just returned to the pool must be fully zeroed.
	next := getReaderBuffer()
	for i, b := range *next {
		if b != 0 {
			t.Fatalf("pooled buffer dirty at byte %d after read error; raw file contents would persist in the pool", i)
		}
	}
}

// TestStructuredParser_MaxKeyLengthBoundary pins the e2e MaxKeyLength check in
// validateResult at its exact boundary: the comparison is
// len(key) > MaxKeyLength (strict), so a flattened key of exactly
// DefaultMaxKeyLength (128) bytes loads and one byte more is rejected with a
// key/max_length ValidationError. Existing coverage sat far from the boundary
// (an 89-char key and a unit row with the cap shrunk to 4).
func TestStructuredParser_MaxKeyLengthBoundary(t *testing.T) {
	tests := []struct {
		name    string
		keyLen  int
		wantErr bool
	}{
		{name: "key of exactly MaxKeyLength bytes loads", keyLen: DefaultMaxKeyLength, wantErr: false},
		{name: "key one byte beyond MaxKeyLength is rejected", keyLen: DefaultMaxKeyLength + 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := strings.Repeat("A", tt.keyLen)
			fs := newTestFileSystem()
			fs.files["keylen.json"] = `{"` + key + `": "v"}`

			cfg := DefaultConfig()
			cfg.FileSystem = fs
			cfg.Filenames = nil
			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }()

			err = loader.LoadFiles("keylen.json")
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("LoadFiles() error = %v, want nil", err)
				}
				if got := loader.GetString(key); got != "v" {
					t.Errorf("GetString(%d-byte key) = %q, want %q", tt.keyLen, got, "v")
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Field != "key" || ve.Rule != "max_length" {
				t.Errorf("LoadFiles() error = %v, want ValidationError{Field: %q, Rule: %q}", err, "key", "max_length")
			}
		})
	}
}
