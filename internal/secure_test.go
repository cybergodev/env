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
	t.Run("line with trailing newline", func(t *testing.T) {
		// Line with more than 10 characters
		content := "this_line_is_too_long_for_limit\n"
		reader := NewSecureReader(strings.NewReader(content), 1024, 10)

		buf := make([]byte, len(content))
		_, err := reader.Read(buf)

		if err != ErrLineTooLong {
			t.Errorf("expected ErrLineTooLong, got %v", err)
		}
	})

	t.Run("final segment without trailing newline", func(t *testing.T) {
		// Boundary: the over-long segment is the last chunk and carries no
		// newline, so the tail-segment check must fire.
		content := "this_line_is_too_long"
		reader := NewSecureReader(strings.NewReader(content), 1024, 10)

		buf := make([]byte, len(content))
		_, err := reader.Read(buf)

		if err != ErrLineTooLong {
			t.Errorf("expected ErrLineTooLong, got %v", err)
		}
	})
}

// TestSecureReaderExactSizeEOF verifies that a stream ending exactly at the
// size limit is accepted (EOF, not ErrFileTooLarge) and that the terminal
// EOF state persists across subsequent reads.
func TestSecureReaderExactSizeEOF(t *testing.T) {
	content := strings.Repeat("a", 100)
	reader := NewSecureReader(strings.NewReader(content), 100, 1024)

	buf := make([]byte, 100)
	n, err := reader.Read(buf)
	if n != 100 {
		t.Errorf("read %d bytes, want 100", n)
	}
	if err != io.EOF {
		t.Errorf("exact-size read error = %v, want io.EOF", err)
	}

	// Subsequent reads must keep reporting EOF, not ErrFileTooLarge.
	if _, err := reader.Read(buf); err != io.EOF {
		t.Errorf("subsequent read error = %v, want io.EOF", err)
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
	t.Run("ErrFileTooLarge persisted", func(t *testing.T) {
		content := strings.Repeat("a", 200)
		reader := NewSecureReader(strings.NewReader(content), 100, 1024)

		buf := make([]byte, 100)
		// First read should trigger error
		_, _ = reader.Read(buf) // state-shaping; the assertion below covers the persisted error

		// Second read should return same error
		buf2 := make([]byte, 100)
		_, err := reader.Read(buf2)

		if err != ErrFileTooLarge {
			t.Errorf("expected persistent error ErrFileTooLarge, got %v", err)
		}
	})

	t.Run("ErrLineTooLong persisted", func(t *testing.T) {
		content := strings.Repeat("a", 200) // one long line, no newline
		reader := NewSecureReader(strings.NewReader(content), 1024, 10)

		buf := make([]byte, 100)
		if _, err := reader.Read(buf); err != ErrLineTooLong {
			t.Fatalf("first Read error = %v, want ErrLineTooLong", err)
		}
		// r.err is sticky: the terminal error must survive subsequent reads.
		if _, err := reader.Read(buf); err != ErrLineTooLong {
			t.Errorf("subsequent Read error = %v, want persisted ErrLineTooLong", err)
		}
	})
}

// TestSecureReader_BytesDelivered covers the BytesDelivered accessor: after
// every chunked Read it must equal the bytes actually delivered so far, and
// at EOF it must equal the full content length.
func TestSecureReader_BytesDelivered(t *testing.T) {
	content := "one\ntwo\nthree\n"
	reader := NewSecureReader(strings.NewReader(content), 1024, 1024)

	var delivered int64
	buf := make([]byte, 4) // smaller than the content to force multiple reads
	for {
		n, err := reader.Read(buf)
		delivered += int64(n)
		if got := reader.BytesDelivered(); got != delivered {
			t.Fatalf("BytesDelivered() = %d after %d delivered bytes, want equal", got, delivered)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}
	if delivered != int64(len(content)) {
		t.Errorf("delivered %d bytes at EOF, want %d", delivered, len(content))
	}
}

// TestSecureReader_ZeroLineLimit pins the maxLineLen==0 contract ("no line
// limit", see the Read comment in secure.go): a single very long line passes
// untouched, while the same input under a small limit is rejected.
func TestSecureReader_ZeroLineLimit(t *testing.T) {
	content := strings.Repeat("a", 4096) // one long line, no newline
	buf := make([]byte, len(content))

	unlimited := NewSecureReader(strings.NewReader(content), 8192, 0)
	n, err := unlimited.Read(buf)
	if err != nil {
		t.Errorf("Read with maxLineLen 0 error = %v, want nil (no line limit)", err)
	}
	if n != len(content) {
		t.Errorf("Read with maxLineLen 0 = %d bytes, want %d", n, len(content))
	}

	limited := NewSecureReader(strings.NewReader(content), 8192, 64)
	if _, err := limited.Read(buf); err != ErrLineTooLong {
		t.Errorf("Read with maxLineLen 64 error = %v, want ErrLineTooLong", err)
	}
}

// TestSecureReader_NonPositiveMaxSize pins the behavior for maxSize <= 0: the
// pre-read guard (totalRead >= maxSize) fires before any underlying I/O, so
// the very first Read returns ErrFileTooLarge.
func TestSecureReader_NonPositiveMaxSize(t *testing.T) {
	for _, maxSize := range []int64{0, -1} {
		reader := NewSecureReader(strings.NewReader("content"), maxSize, 64)
		n, err := reader.Read(make([]byte, 8))
		if err != ErrFileTooLarge {
			t.Errorf("maxSize %d: Read error = %v, want ErrFileTooLarge", maxSize, err)
		}
		if n != 0 {
			t.Errorf("maxSize %d: Read n = %d, want 0", maxSize, n)
		}
	}
}

// TestSecureReader_EmptyBuffer pins Read with a zero-length destination:
// nothing is read (n == 0), no error, no panic.
func TestSecureReader_EmptyBuffer(t *testing.T) {
	reader := NewSecureReader(strings.NewReader("content"), 1024, 10)
	n, err := reader.Read(nil)
	if n != 0 {
		t.Errorf("Read(nil) n = %d, want 0", n)
	}
	if err != nil {
		t.Errorf("Read(nil) error = %v, want nil", err)
	}
}

// TestSecureReader_CRLFLineCounting pins how the line scan treats CR: only
// '\n' resets the counter (Read scans with bytes.IndexByte for '\n'), so a CR
// counts toward the current line's length — a CRLF line whose visible
// characters exactly hit maxLineLen trips the limit, and a bare CR never
// splits a line.
func TestSecureReader_CRLFLineCounting(t *testing.T) {
	t.Run("CRLF within limit passes", func(t *testing.T) {
		content := "012345678\r\n" // 9 visible chars; CR is the 10th byte before \n
		reader := NewSecureReader(strings.NewReader(content), 1024, 10)
		n, err := reader.Read(make([]byte, len(content)))
		if err != nil {
			t.Errorf("Read error = %v, want nil", err)
		}
		if n != len(content) {
			t.Errorf("Read n = %d, want %d", n, len(content))
		}
	})

	t.Run("CRLF at exact visible limit trips limit", func(t *testing.T) {
		content := "0123456789\r\n" // 10 visible chars + CR = 11 bytes before \n
		reader := NewSecureReader(strings.NewReader(content), 1024, 10)
		if _, err := reader.Read(make([]byte, len(content))); err != ErrLineTooLong {
			t.Errorf("Read error = %v, want ErrLineTooLong (CR counts toward line length)", err)
		}
	})

	t.Run("bare CR is not a terminator", func(t *testing.T) {
		content := "01234\r56789\n" // two 5-char halves only if CR terminated lines
		reader := NewSecureReader(strings.NewReader(content), 1024, 10)
		if _, err := reader.Read(make([]byte, len(content))); err != ErrLineTooLong {
			t.Errorf("Read error = %v, want ErrLineTooLong (only \\n resets the counter)", err)
		}
	})
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
		_, _ = first.Read(make([]byte, 5)) // exercise Read to set internal state
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
