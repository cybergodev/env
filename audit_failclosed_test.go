package env

import (
	"errors"
	"strings"
	"testing"
)

// ============================================================================
// Fail-Closed Auditing (GEN-001)
// ============================================================================

// failingAuditHandler is an AuditHandler whose Log always fails, standing in
// for a broken audit pipeline (full disk, closed writer, ...).
type failingAuditHandler struct {
	calls int
}

func (h *failingAuditHandler) Log(event AuditEvent) error {
	h.calls++
	return errors.New("disk full")
}

func (h *failingAuditHandler) Close() error { return nil }

// newFailClosedLoader builds a loader over an in-memory file system with a
// failing audit handler and the given AuditFailClosed setting.
func newFailClosedLoader(t *testing.T, failClosed bool, handler *failingAuditHandler) *Loader {
	t.Helper()
	fs := newTestFileSystem()
	fs.files["audit.env"] = "AUDIT_KEY=1\n"
	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = nil // don't auto-load
	cfg.AuditEnabled = true
	cfg.AuditFailClosed = failClosed
	cfg.AuditHandler = handler
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return loader
}

// TestAuditFailClosed_LoadFiles verifies that a lost audit event fails the
// operation that emitted it when AuditFailClosed is set, and that the
// default stays fail-open (the load succeeds, the loss is only observable
// via the handler).
func TestAuditFailClosed_LoadFiles(t *testing.T) {
	t.Run("fail-closed surfaces the lost event", func(t *testing.T) {
		handler := &failingAuditHandler{}
		loader := newFailClosedLoader(t, true, handler)
		defer func() { _ = loader.Close() }() // best-effort cleanup

		err := loader.LoadFiles("audit.env")
		if !errors.Is(err, ErrAuditFailed) {
			t.Fatalf("LoadFiles() error = %v, want ErrAuditFailed", err)
		}
		if handler.calls == 0 {
			t.Error("audit handler was never invoked")
		}
	})

	t.Run("default is fail-open", func(t *testing.T) {
		handler := &failingAuditHandler{}
		loader := newFailClosedLoader(t, false, handler)
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.LoadFiles("audit.env"); err != nil {
			t.Fatalf("LoadFiles() error = %v, want nil (fail-open default)", err)
		}
		if handler.calls == 0 {
			t.Error("audit handler was never invoked")
		}
	})
}

// TestAuditFailClosed_SetAndApply verify the remaining top-level mutating
// operations carry the same check.
func TestAuditFailClosed_SetAndApply(t *testing.T) {
	t.Run("Set", func(t *testing.T) {
		handler := &failingAuditHandler{}
		loader := newFailClosedLoader(t, true, handler)
		defer func() { _ = loader.Close() }() // best-effort cleanup

		if err := loader.Set("SOME_KEY", "value"); !errors.Is(err, ErrAuditFailed) {
			t.Fatalf("Set() error = %v, want ErrAuditFailed", err)
		}
	})

	t.Run("Apply", func(t *testing.T) {
		handler := &failingAuditHandler{}
		loader := newFailClosedLoader(t, true, handler)
		defer func() { _ = loader.Close() }() // best-effort cleanup

		// Store a variable through the internal setter (whose audit event
		// is latched but deliberately not checked) so Apply has a variable
		// to emit an audit event for.
		if err := loader.set("SOME_KEY", "value"); err != nil {
			t.Fatalf("set() error = %v", err)
		}
		if err := loader.Apply(); !errors.Is(err, ErrAuditFailed) {
			t.Fatalf("Apply() error = %v, want ErrAuditFailed", err)
		}
	})
}

// TestCloseableChannelHandler_DroppedEvents pins the GEN-001 drop counter: a
// buffered handler with no consumer counts every event it discards, and the
// count survives Close.
func TestCloseableChannelHandler_DroppedEvents(t *testing.T) {
	handler := NewCloseableChannelHandler(1)
	// Fill the single buffer slot plus two overflow events. Errors are
	// deliberately discarded: the counter is the observable.
	_ = handler.Log(AuditEvent{Action: ActionLoad})
	_ = handler.Log(AuditEvent{Action: ActionLoad})
	_ = handler.Log(AuditEvent{Action: ActionLoad})
	if got := handler.DroppedEvents(); got != 2 {
		t.Errorf("DroppedEvents() = %d, want 2", got)
	}
	if err := handler.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// A send after close is also a lost event.
	_ = handler.Log(AuditEvent{Action: ActionLoad})
	if got := handler.DroppedEvents(); got != 3 {
		t.Errorf("DroppedEvents() after Close = %d, want 3", got)
	}
}

// TestAuditFailClosed_ChannelFull verifies the end-to-end path for the
// channel-drop case: a tiny buffer with no consumer, fail-closed on.
func TestAuditFailClosed_ChannelFull(t *testing.T) {
	fs := newTestFileSystem()
	fs.files["audit.env"] = "AUDIT_KEY=1\n"
	// A one-slot buffered channel nobody drains: the load emits several
	// events, so at least one must be dropped.
	handler := NewCloseableChannelHandler(1)
	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = nil
	cfg.AuditEnabled = true
	cfg.AuditFailClosed = true
	cfg.AuditHandler = handler
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// The load emits several events into a one-slot buffer nobody drains;
	// at least one drop must latch and fail the load.
	loadErr := loader.LoadFiles("audit.env")
	if loadErr == nil {
		t.Fatal("LoadFiles() = nil, want ErrAuditFailed (events dropped on full channel)")
	}
	if !strings.Contains(loadErr.Error(), "audit") && !errors.Is(loadErr, ErrAuditFailed) {
		t.Fatalf("LoadFiles() error = %v, want an audit failure", loadErr)
	}
	if handler.DroppedEvents() == 0 {
		t.Error("DroppedEvents() = 0, want > 0")
	}
}

// TestAuditFailClosed_CustomAuditorWithoutTakeFailure pins
// auditCheckFailClosed's interface-assertion branch (env.go): a CustomAuditor
// that does NOT implement TakeFailure cannot participate in fail-closed
// signaling, so operations succeed silently — the documented contract
// (config.go: only a TakeFailure-implementing CustomAuditor is consulted).
func TestAuditFailClosed_CustomAuditorWithoutTakeFailure(t *testing.T) {
	fs := newTestFileSystem()
	fs.files["audit.env"] = "AUDIT_KEY=1\n"
	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = nil
	cfg.AuditEnabled = true
	cfg.AuditFailClosed = true
	cfg.CustomAuditor = &mockAuditLogger{} // LogError only — no TakeFailure
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.Set("SOME_KEY", "value"); err != nil {
		t.Errorf("Set() error = %v, want nil (CustomAuditor without TakeFailure is not consulted)", err)
	}
	if err := loader.LoadFiles("audit.env"); err != nil {
		t.Errorf("LoadFiles() error = %v, want nil (no fail-closed signal available)", err)
	}
}

// TestAuditFailClosed_HealthyPathNoFailure pins the TakeFailure()==nil tail
// (env.go): with fail-closed armed and a healthy built-in audit pipeline, a
// successful operation reports success — the check must not fabricate a
// failure when nothing was latched.
func TestAuditFailClosed_HealthyPathNoFailure(t *testing.T) {
	fs := newTestFileSystem()
	fs.files["audit.env"] = "AUDIT_KEY=1\n"
	cfg := DefaultConfig()
	cfg.FileSystem = fs
	cfg.Filenames = nil
	cfg.AuditEnabled = true
	cfg.AuditFailClosed = true
	cfg.AuditHandler = NewCloseableChannelHandler(64) // healthy, room to spare
	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.LoadFiles("audit.env"); err != nil {
		t.Fatalf("LoadFiles() error = %v, want nil (healthy pipeline, no latched failure)", err)
	}
	if err := loader.Set("SOME_KEY", "value"); err != nil {
		t.Fatalf("Set() error = %v, want nil (healthy pipeline, no latched failure)", err)
	}
}
