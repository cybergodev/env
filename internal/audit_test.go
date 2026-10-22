package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewAuditor(t *testing.T) {
	// NewAuditor unconditionally returns a non-nil auditor (every optional
	// argument has a fallback), so the observable contracts are the enabled
	// flag wiring, the per-argument defaults, and disabled Log being a no-op.
	t.Run("enabled flag is wired", func(t *testing.T) {
		if a := NewAuditor(nil, nil, nil, true); !a.enabled.Load() {
			t.Error("NewAuditor(enabled=true) should be enabled")
		}
		if a := NewAuditor(nil, nil, nil, false); a.enabled.Load() {
			t.Error("NewAuditor(enabled=false) should be disabled")
		}
	})

	t.Run("disabled Log is a no-op", func(t *testing.T) {
		a := NewAuditor(nil, nil, nil, false)
		if err := a.Log(ActionSet, "KEY", "test", true); err != nil {
			t.Errorf("Log() when disabled should return nil, got %v", err)
		}
	})

	// Pins the NewAuditor fallback (audit.go): an ENABLED auditor constructed
	// with a nil handler gets NopHandler, so Log is a successful no-op rather
	// than a nil-pointer panic.
	t.Run("enabled auditor with nil handler falls back to Nop", func(t *testing.T) {
		a := NewAuditor(nil, nil, nil, true)
		if !a.IsEnabled() {
			t.Fatal("auditor should be enabled")
		}
		if err := a.Log(ActionSet, "KEY", "test", true); err != nil {
			t.Errorf("Log() with nil handler error = %v, want nil", err)
		}
		if err := a.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	// Partial-nil construction: a non-nil handler and isSensitive with a nil
	// masker must install the identity masker default.
	t.Run("partial nil arguments get their defaults", func(t *testing.T) {
		captured := &eventCapturer{}
		a := NewAuditor(captured, func(string) bool { return false }, nil, true)
		if err := a.Log(ActionGet, "KEY", "read", true); err != nil {
			t.Fatalf("Log() error = %v", err)
		}
		if len(captured.events) != 1 || captured.events[0].Key != "KEY" {
			t.Errorf("captured %+v, want one event with key KEY", captured.events)
		}
	})
}

// TestAuditorLogVariants exercises the Log* entry points through one
// channel-capture table: each row constructs an auditor over a fresh channel
// handler, logs one event, receives it and checks the variant-specific
// fields.
func TestAuditorLogVariants(t *testing.T) {
	isSensitive := func(key string) bool {
		return strings.Contains(strings.ToUpper(key), "PASSWORD")
	}
	masker := func(key, value string) string {
		if isSensitive(key) {
			return "[MASKED]"
		}
		return value
	}

	tests := []struct {
		name       string
		newAuditor func(handler Handler) *Auditor // nil: plain enabled auditor
		log        func(a *Auditor) error
		disabled   bool // auditor constructed disabled: no event may arrive
		check      func(t *testing.T, event Event)
	}{
		{
			name: "log",
			log:  func(a *Auditor) error { return a.Log(ActionSet, "KEY", "test", true) },
			check: func(t *testing.T, event Event) {
				if event.Action != ActionSet {
					t.Errorf("event action = %v, want %v", event.Action, ActionSet)
				}
				if event.Key != "KEY" {
					t.Errorf("event key = %v, want KEY", event.Key)
				}
			},
		},
		{
			// maskKey replaces a sensitive key wholesale (audit.go), and the
			// Masked flag is computed from the ORIGINAL key before masking.
			name:       "log masks sensitive key",
			newAuditor: func(h Handler) *Auditor { return NewAuditor(h, isSensitive, masker, true) },
			log:        func(a *Auditor) error { return a.Log(ActionSet, "PASSWORD", "test", true) },
			check: func(t *testing.T, event Event) {
				if !event.Masked {
					t.Error("sensitive key should be marked as masked")
				}
				if event.Key != "[MASKED]" {
					t.Errorf("event key = %q, want \"[MASKED]\"", event.Key)
				}
			},
		},
		{
			name:       "log leaves non-sensitive key unmasked",
			newAuditor: func(h Handler) *Auditor { return NewAuditor(h, isSensitive, masker, true) },
			log:        func(a *Auditor) error { return a.Log(ActionSet, "USER", "test", true) },
			check: func(t *testing.T, event Event) {
				if event.Masked {
					t.Error("non-sensitive key should not be marked as masked")
				}
				if event.Key != "USER" {
					t.Errorf("event key = %q, want USER", event.Key)
				}
			},
		},
		{
			name: "log with file",
			log:  func(a *Auditor) error { return a.LogWithFile(ActionLoad, "", ".env", "loaded", true) },
			check: func(t *testing.T, event Event) {
				if event.File != ".env" {
					t.Errorf("event file = %v, want .env", event.File)
				}
			},
		},
		{
			name: "log with duration",
			log: func(a *Auditor) error {
				return a.LogWithDuration(ActionLoad, "", "test", true, 100*time.Millisecond)
			},
			check: func(t *testing.T, event Event) {
				if want := (100 * time.Millisecond).Nanoseconds(); event.Duration != want {
					t.Errorf("event duration = %v, want %v", event.Duration, want)
				}
			},
		},
		{
			// LogError is the production error path: Success is forced false
			// and the message lands in Reason.
			name: "log error records failure",
			log:  func(a *Auditor) error { return a.LogError(ActionSet, "KEY", "boom") },
			check: func(t *testing.T, event Event) {
				if event.Success {
					t.Error("LogError event Success = true, want false")
				}
				if event.Reason != "boom" {
					t.Errorf("event reason = %q, want boom", event.Reason)
				}
			},
		},
		{
			// The Log* variants share acquireLogEvent's disabled early-return:
			// a disabled auditor returns nil without touching the handler.
			name:       "log with file disabled is a no-op",
			newAuditor: func(h Handler) *Auditor { return NewAuditor(h, nil, nil, false) },
			log:        func(a *Auditor) error { return a.LogWithFile(ActionLoad, "", ".env", "loaded", true) },
			disabled:   true,
		},
		{
			name:       "log with duration disabled is a no-op",
			newAuditor: func(h Handler) *Auditor { return NewAuditor(h, nil, nil, false) },
			log:        func(a *Auditor) error { return a.LogWithDuration(ActionLoad, "", "test", true, time.Second) },
			disabled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan Event, 1)
			handler := NewChannelHandler(ch)
			a := NewAuditor(handler, nil, nil, true)
			if tt.newAuditor != nil {
				a = tt.newAuditor(handler)
			}

			if err := tt.log(a); err != nil {
				t.Fatalf("Log() error = %v", err)
			}

			if tt.disabled {
				select {
				case event := <-ch:
					t.Errorf("disabled Log delivered event %+v, want none", event)
				default:
				}
				return
			}

			select {
			case event := <-ch:
				tt.check(t, event)
			case <-time.After(time.Second):
				t.Error("timeout waiting for event")
			}
		})
	}
}

func TestAuditorSetEnabled(t *testing.T) {
	a := NewAuditor(nil, nil, nil, false)

	a.SetEnabled(true)
	if !a.IsEnabled() {
		t.Error("IsEnabled() should return true after SetEnabled(true)")
	}

	a.SetEnabled(false)
	if a.IsEnabled() {
		t.Error("IsEnabled() should return false after SetEnabled(false)")
	}
}

func TestJSONHandler(t *testing.T) {
	t.Run("writes valid JSON", func(t *testing.T) {
		var buf strings.Builder
		handler := NewJSONHandler(&buf)

		event := Event{
			Timestamp: time.Now(),
			Action:    ActionSet,
			Key:       "KEY",
			Reason:    "test",
			Success:   true,
		}

		if err := handler.Log(event); err != nil {
			t.Fatalf("JSONHandler.Log() error = %v", err)
		}

		// Verify output is valid JSON
		output := buf.String()
		var parsed Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
			t.Errorf("output is not valid JSON: %v\noutput: %s", err, output)
		}
	})

	// Pins the error branch a well-behaved buffer never reaches: a failing
	// writer must surface the write error, not swallow it.
	t.Run("write error is surfaced", func(t *testing.T) {
		handler := NewJSONHandler(failingWriter{})
		err := handler.Log(Event{Action: ActionSet, Key: "KEY"})
		if err == nil {
			t.Fatal("JSONHandler.Log() error = nil, want write error")
		}
		if !strings.Contains(err.Error(), "failed to write audit event") {
			t.Errorf("JSONHandler.Log() error = %q, want write-failure message", err)
		}
	})
}

// failingWriter is a test-only io.Writer whose Write always fails, used to
// drive JSONHandler.Log's write-error branch (audit.go).
var errFailingWriter = errors.New("synthetic write failure")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errFailingWriter
}

func TestLogHandler(t *testing.T) {
	tests := []struct {
		name      string
		event     Event
		wantParts []string // substrings expected in the log line
		skipParts []string // substrings that must NOT appear
	}{
		{
			name:      "event with key",
			event:     Event{Timestamp: time.Now(), Action: ActionSet, Key: "KEY", Reason: "test", Success: true},
			wantParts: []string{"action=set", "key=KEY", "success=true", `reason="test"`},
			skipParts: []string{"file=", "duration="},
		},
		{
			name:      "event without key omits key field",
			event:     Event{Timestamp: time.Now(), Action: ActionLoad, Reason: "nofile", Success: false},
			wantParts: []string{"action=load", "success=false", `reason="nofile"`},
			skipParts: []string{"key="},
		},
		{
			name:      "event with file appends file field",
			event:     Event{Timestamp: time.Now(), Action: ActionLoad, Key: "KEY", File: ".env", Success: true},
			wantParts: []string{`file=".env"`},
		},
		{
			name:      "sub-millisecond duration formatted in microseconds",
			event:     Event{Timestamp: time.Now(), Action: ActionLoad, Key: "KEY", Success: true, Duration: 1500},
			wantParts: []string{"duration=1μs"},
		},
		{
			name:      "millisecond duration formatted in ms",
			event:     Event{Timestamp: time.Now(), Action: ActionLoad, Key: "KEY", Success: true, Duration: 2500000},
			wantParts: []string{"duration=2.50ms"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := NewLogHandler(log.New(&buf, "[TEST] ", 0))

			if err := handler.Log(tt.event); err != nil {
				t.Fatalf("LogHandler.Log() error = %v", err)
			}

			output := buf.String()
			for _, want := range tt.wantParts {
				if !strings.Contains(output, want) {
					t.Errorf("log output missing %q: %s", want, output)
				}
			}
			for _, skip := range tt.skipParts {
				if strings.Contains(output, skip) {
					t.Errorf("log output should not contain %q: %s", skip, output)
				}
			}
		})
	}
}

// TestLogHandlerNilLogger pins the nil-logger fallback: NewLogHandler(nil)
// installs the default stderr logger (audit.go), so logging one event
// succeeds instead of panicking on a nil *log.Logger.
func TestLogHandlerNilLogger(t *testing.T) {
	handler := NewLogHandler(nil)
	if err := handler.Log(Event{Action: ActionGet}); err != nil {
		t.Errorf("Log() with nil logger error = %v, want nil", err)
	}
}

// TestLogHandlerFileInjection pins SEC-007 (CWE-117): the file field is
// rendered quoted (%q), so a filename containing a newline cannot inject a
// forged log record into the LogHandler stream.
func TestLogHandlerFileInjection(t *testing.T) {
	var buf bytes.Buffer
	handler := NewLogHandler(log.New(&buf, "[TEST] ", 0))

	malicious := "safe.env\n action=forged key=FAKE success=true"
	if err := handler.Log(Event{Action: ActionLoad, File: malicious, Success: true}); err != nil {
		t.Fatalf("LogHandler.Log() error = %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "\naction=forged") {
		t.Errorf("log injection succeeded: %q", out)
	}
	if !strings.Contains(out, "file=") {
		t.Errorf("file field missing from output: %q", out)
	}
	// The newline must be escaped by %q, so it never appears as a raw line
	// break inside the log entry.
	if strings.Count(out, "\n") > 1 {
		t.Errorf("file field produced more than one line: %q", out)
	}
}

// TestChannelHandlersDeliver checks the delivery contract shared by both
// channel handlers: Log hands the event to the channel unchanged.
func TestChannelHandlersDeliver(t *testing.T) {
	tests := []struct {
		name string
		newH func() (Handler, <-chan Event)
	}{
		{"ChannelHandler", func() (Handler, <-chan Event) {
			ch := make(chan Event, 1)
			return NewChannelHandler(ch), ch
		}},
		{"CloseableChannelHandler", func() (Handler, <-chan Event) {
			h := NewCloseableChannelHandler(10)
			return h, h.Channel()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, ch := tt.newH()
			if ch == nil {
				t.Fatal("channel should not be nil")
			}
			defer func() { _ = handler.Close() }() // best-effort cleanup

			event := Event{
				Timestamp: time.Now(),
				Action:    ActionSet,
				Key:       "KEY",
			}

			if err := handler.Log(event); err != nil {
				t.Fatalf("Log() error = %v", err)
			}

			select {
			case received := <-ch:
				if received.Action != ActionSet {
					t.Errorf("received action = %v, want %v", received.Action, ActionSet)
				}
			case <-time.After(time.Second):
				t.Error("timeout waiting for event")
			}
		})
	}
}

func TestNopHandler(t *testing.T) {
	handler := NewNopHandler()

	event := Event{Action: ActionSet}

	err := handler.Log(event)
	if err != nil {
		t.Errorf("NopHandler.Log() error = %v", err)
	}

	err = handler.Close()
	if err != nil {
		t.Errorf("NopHandler.Close() error = %v", err)
	}
}

// TestDefaultHandler pins that DefaultHandler returns a usable stderr
// LogHandler (audit.go): logging one event to it succeeds.
func TestDefaultHandler(t *testing.T) {
	if err := DefaultHandler().Log(Event{Action: ActionGet, Key: "KEY"}); err != nil {
		t.Errorf("DefaultHandler().Log() error = %v, want nil", err)
	}
}

// ============================================================================
// Close Method Tests
// ============================================================================

// TestHandlerClose checks the Close contract of every handler in one table.
func TestHandlerClose(t *testing.T) {
	tests := []struct {
		name string
		newH func(t *testing.T) Handler
	}{
		{"json handler with closer", func(t *testing.T) Handler {
			tmpFile, err := os.CreateTemp("", "audit*.json")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) }) // best-effort cleanup
			return NewJSONHandler(tmpFile)
		}},
		// Should not error when the underlying writer has no Close.
		{"json handler without closer", func(t *testing.T) Handler {
			return NewJSONHandler(&strings.Builder{})
		}},
		{"log handler", func(t *testing.T) Handler {
			return NewLogHandler(nil)
		}},
		// DefaultHandler returns a stderr LogHandler, so its Close follows the
		// same contract (Log behavior is pinned by TestDefaultHandler).
		{"default handler", func(t *testing.T) Handler {
			return DefaultHandler()
		}},
		{"channel handler", func(t *testing.T) Handler {
			return NewChannelHandler(make(chan Event, 1))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.newH(t)
			if err := handler.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})
	}
}

// TestAuditorLogAfterCloseIsNoop pins the documented Close semantics
// (audit.go): after Close, Log* calls are no-ops that return nil — no write
// to a handler whose resources may already be released, and no event.
func TestAuditorLogAfterCloseIsNoop(t *testing.T) {
	ch := make(chan Event, 1)
	a := NewAuditor(NewChannelHandler(ch), nil, nil, true)

	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := a.Log(ActionSet, "KEY", "test", true); err != nil {
		t.Errorf("Log() after Close() error = %v, want nil (documented no-op)", err)
	}
	select {
	case event := <-ch:
		t.Errorf("Log() after Close() delivered event %+v, want none", event)
	default:
		// No event — expected.
	}
}

// countingHandler counts the events that actually reach it.
type countingHandler struct {
	events int
}

func (h *countingHandler) Log(event Event) error {
	h.events++
	return nil
}

func (h *countingHandler) Close() error { return nil }

// TestAuditor_SetEnabledAfterCloseIsNoop pins the GEN-001 fix: a closed
// auditor cannot be resurrected by SetEnabled(true) — its handler's
// resources may already be released, and resuming writes was a
// use-after-close hazard.
func TestAuditor_SetEnabledAfterCloseIsNoop(t *testing.T) {
	handler := &countingHandler{}
	a := NewAuditor(handler, nil, nil, true)

	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	a.SetEnabled(true)
	if a.IsEnabled() {
		t.Fatal("SetEnabled(true) re-enabled a closed auditor")
	}

	// Log after close stays a no-op success; no event reaches the handler.
	if err := a.Log(ActionLoad, "KEY", "reason", true); err != nil {
		t.Fatalf("Log() after Close = %v, want nil (no-op)", err)
	}
	if handler.events != 0 {
		t.Errorf("handler received %d events after Close, want 0", handler.events)
	}
}

// TestAuditor_FailClosedLatch verifies the sticky-failure mechanics directly:
// with fail-closed armed, the first failed Log latches an error that
// TakeFailure reports exactly once; disarmed, failures are only counted.
func TestAuditor_FailClosedLatch(t *testing.T) {
	t.Run("armed latches first failure once", func(t *testing.T) {
		a := NewAuditor(&panicTestHandler{}, nil, nil, true)
		a.SetFailClosed(true)

		if err := a.Log(ActionLoad, "KEY", "reason", true); err == nil {
			t.Fatal("Log() = nil, want the recovered panic error")
		}
		if err := a.TakeFailure(); err == nil {
			t.Fatal("TakeFailure() = nil, want the latched failure")
		}
		if err := a.TakeFailure(); err != nil {
			t.Fatalf("second TakeFailure() = %v, want nil (cleared)", err)
		}
		if a.Failures() != 1 {
			t.Errorf("Failures() = %d, want 1", a.Failures())
		}
	})

	t.Run("disarmed counts but does not latch", func(t *testing.T) {
		a := NewAuditor(&panicTestHandler{}, nil, nil, true)

		if err := a.Log(ActionLoad, "KEY", "reason", true); err == nil {
			t.Fatal("Log() = nil, want the recovered panic error")
		}
		if err := a.TakeFailure(); err != nil {
			t.Fatalf("TakeFailure() = %v, want nil (never armed)", err)
		}
		if a.Failures() != 1 {
			t.Errorf("Failures() = %d, want 1", a.Failures())
		}
	})
}

// panicTestHandler panics on Log to verify recover protection when the
// auditor invokes a user-supplied handler.
type panicTestHandler struct{}

func (h *panicTestHandler) Log(event Event) error {
	panic("simulated handler panic")
}

func (h *panicTestHandler) Close() error {
	return nil
}

// closePanicTestHandler panics on Close to verify recover protection when the
// auditor or buffered handler closes a user-supplied handler.
type closePanicTestHandler struct{}

func (h *closePanicTestHandler) Log(event Event) error {
	return nil
}

func (h *closePanicTestHandler) Close() error {
	panic("simulated close panic")
}

// TestAuditor_LogHandlerPanicRecovery verifies that a panic in a user-supplied
// handler during a foreground Auditor.Log call is recovered and returned as an
// error instead of crashing the process.
func TestAuditor_LogHandlerPanicRecovery(t *testing.T) {
	a := NewAuditor(&panicTestHandler{}, nil, nil, true)

	err := a.Log(ActionSet, "KEY", "test", true)
	if err == nil {
		t.Fatal("Log() with panicking handler should return an error")
	}
	if !strings.Contains(err.Error(), "audit handler panicked") {
		t.Errorf("Log() error = %q, want substring %q", err.Error(), "audit handler panicked")
	}

	// The auditor must remain usable after recovering.
	if err := a.Log(ActionSet, "KEY", "test", true); err == nil {
		t.Error("subsequent Log() should still report the panic as an error")
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// TestAuditor_CloseHandlerPanicRecovery verifies that a panic in a
// user-supplied handler during Auditor.Close is recovered and returned as an
// error instead of crashing the process.
func TestAuditor_CloseHandlerPanicRecovery(t *testing.T) {
	a := NewAuditor(&closePanicTestHandler{}, nil, nil, true)

	err := a.Close()
	if err == nil {
		t.Fatal("Close() with panicking handler should return an error")
	}
	if !strings.Contains(err.Error(), "audit handler panicked during Close") {
		t.Errorf("Close() error = %q, want substring %q", err.Error(), "audit handler panicked during Close")
	}
}

// ============================================================================
// CloseableChannelHandler Tests
// ============================================================================

// TestCloseableChannelHandler_BufferFullDrops pins the buffered drop path
// (audit.go): when the buffer is full and nothing drains it, the next Log
// drops the event and reports it ("audit channel full, event dropped")
// instead of blocking.
func TestCloseableChannelHandler_BufferFullDrops(t *testing.T) {
	handler := NewCloseableChannelHandler(1)
	defer func() { _ = handler.Close() }() // best-effort cleanup

	if err := handler.Log(Event{Action: ActionSet, Key: "FIRST"}); err != nil {
		t.Fatalf("first Log() error = %v, want nil (buffer has room)", err)
	}

	err := handler.Log(Event{Action: ActionSet, Key: "SECOND"})
	if err == nil {
		t.Fatal("second Log() error = nil, want buffer-full drop error")
	}
	if !strings.Contains(err.Error(), "audit channel full, event dropped") {
		t.Errorf("second Log() error = %q, want drop message", err)
	}

	// The dropped event never reached the channel; the first one did.
	if ev := <-handler.Channel(); ev.Key != "FIRST" {
		t.Errorf("received key = %q, want FIRST (SECOND must be dropped)", ev.Key)
	}

	// DroppedEvents pins the GEN-001 drop counter: one discard, observable
	// without the best-effort caller having kept the Log error.
	if got := handler.DroppedEvents(); got != 1 {
		t.Errorf("DroppedEvents() = %d, want 1", got)
	}
}

func TestCloseableChannelHandler_CloseChannel(t *testing.T) {
	handler := NewCloseableChannelHandler(10)
	ch := handler.Channel()

	// Log an event
	_ = handler.Log(Event{Action: ActionSet})

	// Close should close the channel
	if err := handler.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Drain the buffered event first
	<-ch

	// Channel should be closed now
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed")
	}
}

func TestCloseableChannelHandler_ReceiverUnblocks(t *testing.T) {
	handler := NewCloseableChannelHandler(0) // Unbuffered

	receiverDone := make(chan struct{})
	go func() {
		ch := handler.Channel()
		// This will block until handler is closed
		for range ch {
			// Consume events
		}
		close(receiverDone)
	}()

	// Give receiver time to start blocking
	time.Sleep(50 * time.Millisecond)

	// Close the handler - this should unblock the receiver
	if err := handler.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for receiver to finish
	select {
	case <-receiverDone:
		// Success - receiver was unblocked
	case <-time.After(time.Second):
		t.Error("receiver should have been unblocked by Close()")
	}
}

func TestCloseableChannelHandler_LogAfterClose(t *testing.T) {
	handler := NewCloseableChannelHandler(10)

	// Close first
	if err := handler.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Log after close should return error
	err := handler.Log(Event{Action: ActionSet})
	if err == nil {
		t.Error("Log() after Close() should return error")
	}
}

// TestCloseableChannelHandler_CloseDuringUnbufferedSend pins the unbuffered
// send's done-branch: a Log blocked because no receiver is ready must return
// "handler is closed" (counting the drop) when Close interrupts it, instead
// of deadlocking or racing a send on a closed channel.
func TestCloseableChannelHandler_CloseDuringUnbufferedSend(t *testing.T) {
	handler := NewCloseableChannelHandler(0) // unbuffered

	errc := make(chan error, 1)
	go func() { errc <- handler.Log(Event{Action: ActionSet, Key: "BLOCKED"}) }()

	// Give the sender time to block in the unbuffered send.
	time.Sleep(50 * time.Millisecond)
	if err := handler.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case err := <-errc:
		if err == nil {
			t.Error("blocked Log() error = nil, want handler-closed error")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Log() did not return after Close()")
	}
	if got := handler.DroppedEvents(); got != 1 {
		t.Errorf("DroppedEvents() = %d, want 1 (interrupted send counted)", got)
	}
}

// TestCloseableChannelHandler_CloseLifecycle covers the Close contract in one
// place: repeated Close is idempotent, and IsClosed reports both states.
func TestCloseableChannelHandler_CloseLifecycle(t *testing.T) {
	handler := NewCloseableChannelHandler(10)

	if handler.IsClosed() {
		t.Error("IsClosed() = true before Close(), want false")
	}

	// Close multiple times should not error
	for i := 0; i < 3; i++ {
		if err := handler.Close(); err != nil {
			t.Errorf("Close() #%d error = %v", i+1, err)
		}
	}

	if !handler.IsClosed() {
		t.Error("IsClosed() should return true")
	}
}

func TestCloseableChannelHandler_NegativeBufferSize(t *testing.T) {
	// Negative buffer size should be treated as 0
	handler := NewCloseableChannelHandler(-1)
	defer func() { _ = handler.Close() }() // best-effort cleanup

	// Should still work (unbuffered)
	ch := handler.Channel()
	go func() {
		_ = handler.Log(Event{Action: ActionSet})
	}()

	select {
	case <-ch:
		// Success
	case <-time.After(time.Second):
		t.Error("should have received event")
	}
}

func TestCloseableChannelHandler_Concurrent(t *testing.T) {
	handler := NewCloseableChannelHandler(100)
	defer func() { _ = handler.Close() }() // best-effort cleanup

	ch := handler.Channel()
	received := make(chan int, 100)

	// Start receiver
	go func() {
		for range ch {
			received <- 1
		}
	}()

	// Concurrent senders
	var done sync.WaitGroup
	for i := 0; i < 10; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			for j := 0; j < 10; j++ {
				_ = handler.Log(Event{Action: ActionSet})
			}
		}()
	}

	done.Wait()

	// Wait for all events to be received
	timeout := time.After(time.Second)
	count := 0
	for count < 100 {
		select {
		case <-received:
			count++
		case <-timeout:
			t.Errorf("timeout waiting for events, received %d/100", count)
			return
		}
	}
}

// TestAuditEventPool_Boundary covers the audit event pool's defensive paths:
// nil puts are ignored, and a pool poisoned with an unexpected type falls
// back to a fresh Event.
func TestAuditEventPool_Boundary(t *testing.T) {
	t.Run("nil event is ignored", func(t *testing.T) {
		putAuditEvent(nil) // must not panic
	})

	t.Run("unexpected pool type falls back to a fresh event", func(t *testing.T) {
		// sync.Pool may serve any previously pooled victim, so poison it
		// with several foreign objects to make the type-check fallback
		// observable regardless of which one Get returns.
		for i := 0; i < 4; i++ {
			auditEventPool.Put(new(int))
		}
		ev := getAuditEvent()
		if ev == nil {
			t.Fatal("getAuditEvent() = nil, want a fresh fallback Event")
		}
		putAuditEvent(ev) // restore a well-typed event to the pool
	})
}

// TestChannelHandlerNilChannel pins the nil-channel guard: Log must return
// an error immediately instead of blocking forever on a nil channel send
// (a send on a nil channel never completes).
func TestChannelHandlerNilChannel(t *testing.T) {
	h := NewChannelHandler(nil)
	if err := h.Log(Event{Action: ActionGet}); err == nil {
		t.Error("Log(nil channel) error = nil, want error")
	}
	if err := h.Close(); err != nil {
		t.Errorf("Close(nil channel handler) error = %v, want nil", err)
	}
}

// eventCapturer collects events for assertions.
type eventCapturer struct {
	events []Event
}

func (c *eventCapturer) Log(event Event) error {
	c.events = append(c.events, event)
	return nil
}

func (c *eventCapturer) Close() error { return nil }

// TestAuditor_PanickingCallbacksFailSafe pins the panic guards on the
// user-supplied isSensitive/masker callbacks (safeIsSensitive /
// safeMaskSensitive, mirroring safeHandlerLog): a panicking callback must
// fail SAFE — the key is treated as sensitive and hidden entirely — instead
// of crashing the caller of Log.
func TestAuditor_PanickingCallbacksFailSafe(t *testing.T) {
	cap := &eventCapturer{}
	a := NewAuditor(cap,
		func(string) bool { panic("isSensitive boom") },
		func(key, value string) string { panic("masker boom") },
		true,
	)

	if err := a.Log(ActionGet, "SECRET_KEY", "read", true); err != nil {
		t.Fatalf("Log() error = %v", err)
	}
	if len(cap.events) != 1 {
		t.Fatalf("captured %d events, want 1", len(cap.events))
	}
	ev := cap.events[0]
	if !ev.Masked {
		t.Error("Masked = false, want true (panicking isSensitive fails safe)")
	}
	if ev.Key != "[MASKED]" {
		t.Errorf("Key = %q, want [MASKED] (panicking masker fails safe)", ev.Key)
	}
}
