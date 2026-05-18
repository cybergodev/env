package internal

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscapeValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple value",
			input:    "simple",
			expected: "simple",
		},
		{
			name:     "value with space",
			input:    "value with space",
			expected: `"value with space"`,
		},
		{
			name:     "value with newline",
			input:    "line1\nline2",
			expected: `"line1\nline2"`,
		},
		{
			name:     "value with tab",
			input:    "col1\tcol2",
			expected: `"col1\tcol2"`,
		},
		{
			name:     "value with quote",
			input:    `say "hello"`,
			expected: `"say \"hello\""`,
		},
		{
			name:     "value with hash",
			input:    "value#comment",
			expected: `"value#comment"`,
		},
		{
			name:     "value with backslash only",
			input:    "path\\to\\file",
			expected: "path\\to\\file", // no quoting needed, returned as-is
		},
		{
			name:     "empty value",
			input:    "",
			expected: `""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EscapeValue(tt.input)
			if result != tt.expected {
				t.Errorf("EscapeValue() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMarshalEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		sorted   bool
		contains []string
	}{
		{
			name: "simple map",
			input: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			sorted:   false,
			contains: []string{"KEY1=value1", "KEY2=value2"},
		},
		{
			name: "sorted output",
			input: map[string]string{
				"B_KEY": "b",
				"A_KEY": "a",
			},
			sorted:   true,
			contains: []string{"A_KEY=a", "B_KEY=b"},
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			sorted:   false,
			contains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MarshalEnv(tt.input, tt.sorted)
			if err != nil {
				t.Errorf("MarshalEnv() error = %v", err)
				return
			}
			for _, c := range tt.contains {
				if !strings.Contains(result, c) {
					t.Errorf("MarshalEnv() should contain %q, got %q", c, result)
				}
			}
		})
	}
}

func TestMarshalEnvSorted(t *testing.T) {
	input := map[string]string{
		"Z_KEY": "z",
		"A_KEY": "a",
		"M_KEY": "m",
	}

	result, err := MarshalEnv(input, true)
	if err != nil {
		t.Errorf("MarshalEnv() error = %v", err)
		return
	}

	// Verify order: A_KEY should appear before M_KEY before Z_KEY
	aIdx := strings.Index(result, "A_KEY")
	mIdx := strings.Index(result, "M_KEY")
	zIdx := strings.Index(result, "Z_KEY")

	if !(aIdx < mIdx && mIdx < zIdx) {
		t.Errorf("keys not in sorted order: A=%d, M=%d, Z=%d", aIdx, mIdx, zIdx)
	}
}

func TestWriteFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "env-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filename := filepath.Join(tmpDir, "test.env")
	content := "KEY=value\n"

	var buf bytes.Buffer
	buf.WriteString(content)

	err = WriteFile(filename, &buf)
	if err != nil {
		t.Errorf("WriteFile() error = %v", err)
		return
	}

	// Verify file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("file was not created")
		return
	}

	// Verify content
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Errorf("failed to read file: %v", err)
		return
	}

	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}
}

func TestWriteFileCreatesDirectory(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "env-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Target file in non-existent subdirectory
	filename := filepath.Join(tmpDir, "subdir", "nested", "test.env")
	content := "KEY=value\n"

	var buf bytes.Buffer
	buf.WriteString(content)

	err = WriteFile(filename, &buf)
	if err != nil {
		t.Errorf("WriteFile() error = %v", err)
		return
	}

	// Verify file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("file was not created in nested directory")
	}
}

// ============================================================================
// MarshalEnvAs Tests (Multi-format Marshal)
// ============================================================================

func TestMarshalEnvAs(t *testing.T) {
	input := map[string]string{
		"APP_NAME": "myapp",
		"APP_PORT": "8080",
		"DEBUG":    "true",
	}

	t.Run("dotenv format", func(t *testing.T) {
		result, err := MarshalEnvAs(input, FormatEnv, false)
		if err != nil {
			t.Errorf("MarshalEnvAs() error = %v", err)
			return
		}
		if !strings.Contains(result, "APP_NAME=myapp") {
			t.Errorf("expected .env format, got: %s", result)
		}
	})

	t.Run("json format", func(t *testing.T) {
		result, err := MarshalEnvAs(input, FormatJSON, false)
		if err != nil {
			t.Errorf("MarshalEnvAs() error = %v", err)
			return
		}
		if !strings.Contains(result, `"APP"`) || !strings.Contains(result, `"NAME"`) {
			t.Errorf("expected JSON format with nested structure, got: %s", result)
		}
	})

	t.Run("yaml format", func(t *testing.T) {
		result, err := MarshalEnvAs(input, FormatYAML, false)
		if err != nil {
			t.Errorf("MarshalEnvAs() error = %v", err)
			return
		}
		if !strings.Contains(result, "APP_NAME: myapp") {
			t.Errorf("expected YAML format, got: %s", result)
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := MarshalEnvAs(input, MarshalFormat(99), false)
		if err == nil {
			t.Error("expected error for invalid format")
		}
	})
}

// ============================================================================
// JSON Marshal Tests
// ============================================================================

func TestMarshalToJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		sorted   bool
		contains []string
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			sorted:   false,
			contains: []string{"{}"},
		},
		{
			name: "simple flat map",
			input: map[string]string{
				"NAME": "test",
				"PORT": "8080",
			},
			sorted:   false,
			contains: []string{`"NAME"`, `"test"`, `"PORT"`, `8080`},
		},
		{
			name: "nested keys",
			input: map[string]string{
				"DB_HOST": "localhost",
				"DB_PORT": "5432",
			},
			sorted:   false,
			contains: []string{`"DB"`, `"HOST"`, `"localhost"`},
		},
		{
			name: "boolean values",
			input: map[string]string{
				"DEBUG":   "true",
				"VERBOSE": "false",
			},
			sorted:   false,
			contains: []string{`"DEBUG"`, `true`, `"VERBOSE"`, `false`},
		},
		{
			name: "numeric values",
			input: map[string]string{
				"COUNT": "42",
				"RATIO": "3.14",
			},
			sorted:   false,
			contains: []string{`"COUNT"`, `42`, `"RATIO"`, `3.14`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalToJSON(tt.input, tt.sorted)
			if err != nil {
				t.Errorf("marshalToJSON() error = %v", err)
				return
			}
			for _, c := range tt.contains {
				if !strings.Contains(string(result), c) {
					t.Errorf("marshalToJSON() should contain %q, got %s", c, result)
				}
			}
		})
	}
}

func TestInferJSONType(t *testing.T) {
	tests := []struct {
		input    string
		expected interface{}
	}{
		{"true", true},
		{"false", false},
		{"null", nil},
		{"42", int64(42)},
		{"3.14", float64(3.14)},
		{"hello", "hello"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := inferJSONType(tt.input)
			if result != tt.expected {
				t.Errorf("inferJSONType(%q) = %v (%T), want %v (%T)",
					tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}

// ============================================================================
// YAML Marshal Tests
// ============================================================================

func TestMarshalToYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		sorted   bool
		contains []string
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			sorted:   false,
			contains: []string{},
		},
		{
			name: "simple map",
			input: map[string]string{
				"NAME": "test",
				"PORT": "8080",
			},
			sorted:   false,
			contains: []string{"NAME: test", "PORT: 8080"},
		},
		{
			name: "sorted output",
			input: map[string]string{
				"Z_KEY": "z",
				"A_KEY": "a",
			},
			sorted:   true,
			contains: []string{"A_KEY: a", "Z_KEY: z"},
		},
		{
			name: "value with special chars",
			input: map[string]string{
				"MESSAGE": "hello: world",
			},
			sorted:   false,
			contains: []string{`MESSAGE: "hello: world"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalToYAML(tt.input, tt.sorted)
			if err != nil {
				t.Errorf("marshalToYAML() error = %v", err)
				return
			}
			for _, c := range tt.contains {
				if !strings.Contains(string(result), c) {
					t.Errorf("marshalToYAML() should contain %q, got %s", c, result)
				}
			}
		})
	}
}

func TestEscapeYAMLValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"", `""`},
		{"has: colon", `"has: colon"`},
		{"has# hash", `"has# hash"`},
		{"has\n newline", `"has\n newline"`},
		{"has \"quote\"", `"has \"quote\""`},
		{"- starts with dash", `"- starts with dash"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeYAMLValue(tt.input)
			if result != tt.expected {
				t.Errorf("escapeYAMLValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMarshalToYAMLSorted(t *testing.T) {
	input := map[string]string{
		"Z_KEY": "z",
		"A_KEY": "a",
		"M_KEY": "m",
	}

	result, err := marshalToYAML(input, true)
	if err != nil {
		t.Errorf("marshalToYAML() error = %v", err)
		return
	}

	// Verify order: A_KEY should appear before M_KEY before Z_KEY
	str := string(result)
	aIdx := strings.Index(str, "A_KEY:")
	mIdx := strings.Index(str, "M_KEY:")
	zIdx := strings.Index(str, "Z_KEY:")

	if !(aIdx < mIdx && mIdx < zIdx) {
		t.Errorf("keys not in sorted order: A=%d, M=%d, Z=%d", aIdx, mIdx, zIdx)
	}
}

// ============================================================================
// Resource Cleanup Tests
// ============================================================================

// TestWriteFile_TempFileCleanup verifies that temp files are cleaned up
// when WriteFile fails at various stages.
func TestWriteFile_TempFileCleanup(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "env-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("temp file cleaned up on invalid path", func(t *testing.T) {
		// Use an invalid path (e.g., null byte in path)
		filename := filepath.Join(tmpDir, "test\x00file.env")
		var buf bytes.Buffer
		buf.WriteString("KEY=value\n")

		// This should fail because path is invalid
		err := WriteFile(filename, &buf)
		if err == nil {
			t.Error("expected error for invalid path")
		}

		// Temp file should not exist (it shouldn't have been created)
		tempFile := filename + ".tmp"
		if _, err := os.Stat(tempFile); err == nil {
			t.Errorf("temp file should not exist for invalid path")
		}
	})

	t.Run("temp file cleaned up on directory creation error", func(t *testing.T) {
		// Try to create a file in a path that would require creating a directory
		// but use a path component that is actually an existing file
		blockingFile := filepath.Join(tmpDir, "blocker")
		if err := os.WriteFile(blockingFile, []byte("blocking"), 0644); err != nil {
			t.Fatalf("failed to create blocking file: %v", err)
		}

		// Try to write to a file inside what should be a directory but is a file
		filename := filepath.Join(blockingFile, "subdir", "test.env")
		var buf bytes.Buffer
		buf.WriteString("KEY=value\n")

		// This should fail because "blocker" is a file, not a directory
		err := WriteFile(filename, &buf)
		if err == nil {
			t.Error("expected error when parent path is a file")
		}

		// Temp file should not exist
		tempFile := filename + ".tmp"
		if _, err := os.Stat(tempFile); err == nil {
			t.Errorf("temp file should not exist: %s", tempFile)
		}
	})
}

// TestWriteFile_NoDoubleClose verifies that WriteFile doesn't
// double-close the file handle.
func TestWriteFile_NoDoubleClose(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "env-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write multiple files to stress-test the file handle management
	for i := range 10 {
		filename := filepath.Join(tmpDir, "test"+string(rune('0'+i))+".env")
		var buf bytes.Buffer
		buf.WriteString("KEY=value\n")

		err := WriteFile(filename, &buf)
		if err != nil {
			t.Errorf("WriteFile() error = %v", err)
			continue
		}

		// Verify file exists and has correct content
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Errorf("failed to read file: %v", err)
			continue
		}
		if string(data) != "KEY=value\n" {
			t.Errorf("file content = %q, want %q", string(data), "KEY=value\n")
		}
	}
}

func TestWriteFile_ErrorPaths(t *testing.T) {
	t.Run("invalid path", func(t *testing.T) {
		var buf bytes.Buffer
		buf.WriteString("test")

		// Use an invalid path that should fail
		err := WriteFile(string([]byte{0}), &buf)
		if err == nil {
			t.Error("WriteFile() should fail for invalid path")
		}
	})
}
