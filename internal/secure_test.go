package internal

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestSecureReaderBasic(t *testing.T) {
	content := "line1\nline2\nline3\n"
	reader := NewSecureReader(strings.NewReader(content), 1024, 1024)

	buf := make([]byte, len(content))
	n, err := io.ReadFull(reader, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		t.Errorf("ReadFull() error = %v", err)
	}
	if n != len(content) {
		t.Errorf("ReadFull() read %d bytes, want %d", n, len(content))
	}
}

func TestSecureReaderSizeLimit(t *testing.T) {
	content := strings.Repeat("a", 1000)
	reader := NewSecureReader(strings.NewReader(content), 100, 1024)

	buf := make([]byte, 1000)
	n, err := reader.Read(buf)

	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
	if n > 100 {
		t.Errorf("read %d bytes, should not exceed limit of 100", n)
	}
}

func TestSecureReaderLineLengthLimit(t *testing.T) {
	// Line with more than 10 characters
	content := "this_line_is_too_long_for_limit\n"
	reader := NewSecureReader(strings.NewReader(content), 1024, 10)

	buf := make([]byte, len(content))
	_, err := reader.Read(buf)

	if err != ErrLineTooLong {
		t.Errorf("expected ErrLineTooLong, got %v", err)
	}
}

func TestSecureReaderMultipleReads(t *testing.T) {
	content := "short line\n"
	reader := NewSecureReader(strings.NewReader(content), 1024, 1024)

	var result bytes.Buffer
	buf := make([]byte, 5) // Small buffer to force multiple reads

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			result.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			break
		}
	}

	if result.String() != content {
		t.Errorf("read %q, want %q", result.String(), content)
	}
}

func TestSecureReaderHardLimits(t *testing.T) {
	// Test that hard limits are enforced even with larger config
	reader := NewSecureReader(nil, 200*1024*1024, 100*1024) // Exceeds hard limits

	// Hard limits should cap the values
	const expectedMaxSize = 100 * 1024 * 1024
	const expectedMaxLine = 64 * 1024

	if reader.maxSize > expectedMaxSize {
		t.Errorf("maxSize %d exceeds hard limit %d", reader.maxSize, expectedMaxSize)
	}
	if reader.maxLineLen > expectedMaxLine {
		t.Errorf("maxLineLen %d exceeds hard limit %d", reader.maxLineLen, expectedMaxLine)
	}
}

func TestSecureReaderResetLineOnNewline(t *testing.T) {
	// Content with lines exactly at limit
	content := "0123456789\n0123456789\n"
	reader := NewSecureReader(strings.NewReader(content), 1024, 10)

	buf := make([]byte, len(content))
	n, err := reader.Read(buf)

	// Should succeed because newline resets the counter
	if err == ErrLineTooLong {
		t.Error("line length should be reset by newline")
	}
	if n != len(content) {
		t.Errorf("read %d bytes, want %d", n, len(content))
	}
}

func TestSecureReaderErrorPersisted(t *testing.T) {
	content := strings.Repeat("a", 200)
	reader := NewSecureReader(strings.NewReader(content), 100, 1024)

	buf := make([]byte, 100)
	// First read should trigger error
	reader.Read(buf)

	// Second read should return same error
	buf2 := make([]byte, 100)
	_, err := reader.Read(buf2)

	if err != ErrFileTooLarge {
		t.Errorf("expected persistent error ErrFileTooLarge, got %v", err)
	}
}

// ============================================================================
// ReleaseSecureReader Tests
// ============================================================================

func TestReleaseSecureReader(t *testing.T) {
	t.Run("valid reader returns to pool", func(t *testing.T) {
		reader := NewSecureReader(strings.NewReader("test"), 1024, 1024)
		// Should not panic
		ReleaseSecureReader(reader)
	})

	t.Run("nil is safe", func(t *testing.T) {
		// Should not panic
		ReleaseSecureReader(nil)
	})

	t.Run("reader reused from pool after release", func(t *testing.T) {
		// Create, release, create again — the second should reuse the pooled struct
		first := NewSecureReader(strings.NewReader("data1"), 512, 64)
		first.Read(make([]byte, 5)) // exercise Read to set internal state
		ReleaseSecureReader(first)

		second := NewSecureReader(strings.NewReader("data2"), 512, 64)
		// Verify the reused reader is functional
		buf := make([]byte, 5)
		n, err := second.Read(buf)
		if err != nil && err != io.EOF {
			t.Errorf("reused reader Read() error = %v", err)
		}
		if n != 5 || string(buf) != "data2" {
			t.Errorf("reused reader Read() = %q (%d bytes), want \"data2\" (5 bytes)", string(buf[:n]), n)
		}
		// totalRead should be reset from the previous reader's state
		if second.totalRead != 5 {
			t.Errorf("reused reader totalRead = %d, want 5 (should be reset)", second.totalRead)
		}
	})

	t.Run("released reader has nil underlying reader (GC-friendly)", func(t *testing.T) {
		reader := NewSecureReader(strings.NewReader("content"), 1024, 1024)
		ReleaseSecureReader(reader)
		if reader.reader != nil {
			t.Error("released reader should have nil underlying reader for GC")
		}
	})
}
