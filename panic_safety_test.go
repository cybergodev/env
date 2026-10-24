package env

// Regression tests for the SEC-003 panic-containment wrappers (recovering.go):
// a panic inside any config-injected custom component (validator, expander,
// auditor, file system, registered parser) or struct-field (un)marshaler
// callback must surface as an error from the public API that invoked it —
// never crash the process. The capability-preservation tests pin that the
// wrappers are behavior-identical for components that do NOT panic.

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Panicking custom validator
// ============================================================================

type panickingValidator struct{}

func (panickingValidator) ValidateKey(string) error               { panic("validator boom") }
func (panickingValidator) ValidateValue(string) error             { panic("validator boom") }
func (panickingValidator) ValidateRequired(map[string]bool) error { panic("validator boom") }

func TestSEC003_PanickingCustomValidator(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.CustomValidator = panickingValidator{}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v (construction itself runs no validation)", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.Set("K", "v"); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("Set() with panicking validator: err = %v, want contained panic error", err)
	}
	if err := loader.Validate(); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("Validate() with panicking validator: err = %v, want contained panic error", err)
	}

	// The parse path validates each line through the same wrapper.
	parseCfg := DefaultConfig()
	parseCfg.CustomValidator = panickingValidator{}
	if _, err := UnmarshalMapWithConfig(parseCfg, "K=v\n"); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("UnmarshalMapWithConfig: err = %v, want contained panic error", err)
	}
}

// plainCustomValidator implements only the base Validator interface — no
// optional capabilities — so the wrapper's fallbacks are exercised.
type plainCustomValidator struct {
	keyErr      error
	requiredErr error
}

func (v *plainCustomValidator) ValidateKey(string) error   { return v.keyErr }
func (v *plainCustomValidator) ValidateValue(string) error { return nil }
func (v *plainCustomValidator) ValidateRequired(map[string]bool) error {
	return v.requiredErr
}

// capabilityValidator additionally implements the optional capabilities the
// library probes (ValidateSetKey, HasRequiredKeys), pinning that the wrapper
// forwards them.
type capabilityValidator struct {
	keyErr      error
	setKeyErr   error
	reqKeys     bool
	requiredErr error
}

func (v *capabilityValidator) ValidateKey(string) error   { return v.keyErr }
func (v *capabilityValidator) ValidateValue(string) error { return nil }
func (v *capabilityValidator) ValidateRequired(map[string]bool) error {
	return v.requiredErr
}
func (v *capabilityValidator) ValidateSetKey(string) error { return v.setKeyErr }
func (v *capabilityValidator) HasRequiredKeys() bool       { return v.reqKeys }

func TestSEC003_CustomValidatorCapabilitiesPreserved(t *testing.T) {
	newLoader := func(v Validator) *Loader {
		t.Helper()
		cfg := DefaultConfig()
		cfg.Filenames = nil
		cfg.CustomValidator = v
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		return loader
	}

	t.Run("ValidateSetKey forwarded over strict fallback", func(t *testing.T) {
		// setKeyErr=nil admits the key while ValidateKey would reject it —
		// success proves the optional capability is delegated, not fallen
		// back from.
		loader := newLoader(&capabilityValidator{keyErr: errors.New("strict nope")})
		defer func() { _ = loader.Close() }() // best-effort cleanup
		if err := loader.Set("a.b", "v"); err != nil {
			t.Errorf("Set(dotted) = %v, want nil (ValidateSetKey forwarded)", err)
		}
	})

	t.Run("ValidateSetKey absent falls back to ValidateKey", func(t *testing.T) {
		loader := newLoader(&plainCustomValidator{keyErr: errors.New("nope")})
		defer func() { _ = loader.Close() }() // best-effort cleanup
		if err := loader.Set("PLAIN", "v"); err == nil || !strings.Contains(err.Error(), "nope") {
			t.Errorf("Set(PLAIN) = %v, want the mock ValidateKey error via the fallback", err)
		}
	})

	t.Run("HasRequiredKeys=false skips the parse-time required check", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.CustomValidator = &capabilityValidator{reqKeys: false, requiredErr: errors.New("missing")}
		cfg.RequiredKeys = []string{"MISSING_KEY"}
		if _, err := UnmarshalMapWithConfig(cfg, "K=v\n"); err != nil {
			t.Errorf("parse with HasRequiredKeys=false: err = %v, want nil (check skipped)", err)
		}
	})

	t.Run("HasRequiredKeys absent keeps the parse-time required check", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.CustomValidator = &plainCustomValidator{requiredErr: errors.New("missing")}
		cfg.RequiredKeys = []string{"MISSING_KEY"}
		if _, err := UnmarshalMapWithConfig(cfg, "K=v\n"); err == nil || !strings.Contains(err.Error(), "missing") {
			t.Errorf("parse with capability-less validator: err = %v, want ValidateRequired error", err)
		}
	})
}

// ============================================================================
// Panicking custom expander
// ============================================================================

type panickingExpander struct{}

func (panickingExpander) Expand(string) (string, error) { panic("expander boom") }

func TestSEC003_PanickingCustomExpander(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CustomExpander = panickingExpander{}
	cfg.ExpandVariables = true

	_, err := UnmarshalMapWithConfig(cfg, "A=${B}\nB=x\n")
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("UnmarshalMapWithConfig: err = %v, want contained panic error", err)
	}
}

// ============================================================================
// Panicking custom auditor
// ============================================================================

type panickingAuditor struct{}

func (a *panickingAuditor) LogError(AuditAction, string, string) error { panic("auditor boom") }
func (a *panickingAuditor) Log(AuditAction, string, string, bool) error {
	panic("auditor boom")
}
func (a *panickingAuditor) LogWithFile(AuditAction, string, string, string, bool) error {
	panic("auditor boom")
}
func (a *panickingAuditor) LogWithDuration(AuditAction, string, string, bool, time.Duration) error {
	panic("auditor boom")
}
func (a *panickingAuditor) Close() error { panic("auditor close boom") }

type panickingMinimalAuditor struct{}

func (panickingMinimalAuditor) LogError(AuditAction, string, string) error {
	panic("auditor boom")
}

func TestSEC003_PanickingCustomAuditor(t *testing.T) {
	// Auditing is best-effort on most paths (errors discarded with _ =), so
	// the contract here is: the operation completes without crashing, and
	// Close surfaces the contained panic from the custom auditor's Close.
	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.AuditEnabled = true
	cfg.CustomAuditor = &panickingAuditor{}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := loader.Set("K", "v"); err != nil {
		t.Errorf("Set() = %v, want nil (audit failure must not fail the operation)", err)
	}
	if err := loader.Close(); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("Close() = %v, want contained panic from auditor Close", err)
	}

	// Minimal auditor (LogError only): the synthesized Log routes through the
	// recovered LogError, so an audited operation does not crash either, and
	// Close stays nil for a custom without io.Closer.
	minCfg := DefaultConfig()
	minCfg.Filenames = nil
	minCfg.AuditEnabled = true
	minCfg.CustomAuditor = panickingMinimalAuditor{}
	minLoader, err := New(minCfg)
	if err != nil {
		t.Fatalf("New(minimal) error = %v", err)
	}
	if err := minLoader.Set("K", "v"); err != nil {
		t.Errorf("Set(minimal) = %v, want nil", err)
	}
	if err := minLoader.Close(); err != nil {
		t.Errorf("Close(minimal) = %v, want nil (no io.Closer on the custom)", err)
	}
}

// takeFailureAuditor participates in AuditFailClosed through the wrapper:
// Log fails, TakeFailure reports the latched sentinel.
type takeFailureAuditor struct{ taken *bool }

func (a *takeFailureAuditor) LogError(AuditAction, string, string) error {
	return errors.New("log failed")
}
func (a *takeFailureAuditor) Log(AuditAction, string, string, bool) error {
	return errors.New("log failed")
}
func (a *takeFailureAuditor) LogWithFile(AuditAction, string, string, string, bool) error {
	return errors.New("log failed")
}
func (a *takeFailureAuditor) LogWithDuration(AuditAction, string, string, bool, time.Duration) error {
	return errors.New("log failed")
}
func (a *takeFailureAuditor) Close() error { return nil }
func (a *takeFailureAuditor) TakeFailure() error {
	*a.taken = true
	return errors.New("latched")
}

func TestSEC003_AuditorTakeFailureForwarded(t *testing.T) {
	taken := false
	cfg := DefaultConfig()
	cfg.Filenames = nil
	cfg.AuditEnabled = true
	cfg.AuditFailClosed = true
	cfg.CustomAuditor = &takeFailureAuditor{taken: &taken}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.Set("K", "v"); !errors.Is(err, ErrAuditFailed) {
		t.Fatalf("Set() = %v, want ErrAuditFailed (TakeFailure forwarded)", err)
	}
	if !taken {
		t.Error("TakeFailure was not reached through the wrapper")
	}
}

// ============================================================================
// Panicking custom file system
// ============================================================================

// panicFS panics on selected operations; non-panicking storage lives in an
// env map (Setenv/LookupEnv) so the AutoApply paths can run to completion.
type panicFS struct {
	env           map[string]string
	panicOnOpen   bool
	panicOnLookup bool
	panicOnSet    bool
}

func (f *panicFS) Open(string) (File, error) {
	if f.panicOnOpen {
		panic("fs open boom")
	}
	return nil, os.ErrNotExist
}
func (f *panicFS) OpenFile(string, int, os.FileMode) (File, error) { return nil, os.ErrNotExist }
func (f *panicFS) Stat(string) (os.FileInfo, error)                { return nil, os.ErrNotExist }
func (f *panicFS) MkdirAll(string, os.FileMode) error              { return nil }
func (f *panicFS) Remove(string) error                             { return nil }
func (f *panicFS) Rename(string, string) error                     { return nil }
func (f *panicFS) Getenv(string) string                            { return "" }
func (f *panicFS) Setenv(key, value string) error {
	if f.panicOnSet {
		panic("fs setenv boom")
	}
	f.env[key] = value
	return nil
}
func (f *panicFS) Unsetenv(string) error { return nil }
func (f *panicFS) LookupEnv(key string) (string, bool) {
	if f.panicOnLookup {
		panic("fs lookup boom")
	}
	v, ok := f.env[key]
	return v, ok
}

func TestSEC003_PanickingCustomFileSystem(t *testing.T) {
	t.Run("Open panic becomes an error", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Filenames = []string{"x.env"}
		cfg.FileSystem = &panicFS{env: map[string]string{}, panicOnOpen: true}
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Errorf("New() = %v, want contained panic error", err)
		}
	})

	t.Run("Setenv panic becomes an error", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Filenames = nil
		cfg.AutoApply = true
		cfg.FileSystem = &panicFS{env: map[string]string{}, panicOnSet: true}
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup
		if err := loader.Set("K", "v"); err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Errorf("Set() = %v, want contained panic error", err)
		}
	})

	t.Run("LookupEnv panic is contained to unset", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Filenames = nil
		cfg.AutoApply = true
		cfg.FileSystem = &panicFS{env: map[string]string{}, panicOnLookup: true}
		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = loader.Close() }() // best-effort cleanup
		// Overwrite is on by DefaultConfig: the panicked lookup reads as
		// "unset" and the set proceeds.
		if err := loader.Set("K", "v"); err != nil {
			t.Errorf("Set() = %v, want nil (panicked lookup treated as unset)", err)
		}
	})

	t.Run("expansion lookup through custom FS is contained", func(t *testing.T) {
		// Regression (SEC-003 re-review): UnmarshalMapWithConfig hands the
		// custom FileSystem to the factory as a non-nil parameter, which
		// previously bypassed the wrap branch — the expander's captured
		// fs.LookupEnv was raw, and a panic escaped through the public API.
		// ${MISSING} is absent from the parsed map, so expansion must consult
		// the (panicking) external lookup.
		cfg := DefaultConfig()
		cfg.FileSystem = &panicFS{env: map[string]string{}, panicOnLookup: true}
		cfg.ExpansionScope = ExpansionFileThenProcess
		cfg.ExpandVariables = true
		m, err := UnmarshalMapWithConfig(cfg, "A=${MISSING}\n")
		if err != nil {
			t.Fatalf("UnmarshalMapWithConfig: err = %v, want nil (lookup panic contained)", err)
		}
		if got := m["A"]; got != "" {
			t.Errorf("A = %q, want empty (panicked lookup treated as unset)", got)
		}
	})

	t.Run("DefaultFileSystem knob is contained", func(t *testing.T) {
		// The knob is documented as user-replaceable at init; a custom
		// implementation that panics must not crash Load (nil cfg.FileSystem
		// resolves to the knob).
		orig := DefaultFileSystem
		DefaultFileSystem = &panicFS{env: map[string]string{}, panicOnOpen: true}
		defer func() { DefaultFileSystem = orig }()

		cfg := DefaultConfig()
		cfg.Filenames = []string{"x.env"}
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Errorf("New() via DefaultFileSystem knob = %v, want contained panic error", err)
		}
	})
}

// ============================================================================
// Panicking custom parser
// ============================================================================

type panickingParser struct{}

func (panickingParser) Parse(reader io.Reader, _ string) (map[string]string, error) {
	panic("parser boom")
}

func TestSEC003_PanickingCustomParser(t *testing.T) {
	// FormatAuto is the documented route for custom parsers in file loading:
	// DetectFormat returns it for unknown extensions.
	if err := RegisterParser(FormatAuto, func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		return panickingParser{}, nil
	}); err != nil {
		t.Fatalf("RegisterParser(FormatAuto) error = %v", err)
	}
	defer func() {
		globalParserRegistry.mu.Lock()
		delete(globalParserRegistry.factories, FormatAuto)
		globalParserRegistry.mu.Unlock()
	}()

	fs := newTestFileSystem()
	fs.files["data.custom"] = "K=v\n"

	cfg := DefaultConfig()
	cfg.Filenames = []string{"data.custom"}
	cfg.FileSystem = fs
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("New() with panicking parser: err = %v, want contained panic error", err)
	}
}

// ============================================================================
// Panicking struct-field / target (un)marshaler callbacks
// ============================================================================

type boomTextUnmarshaler struct{}

func (boomTextUnmarshaler) UnmarshalText([]byte) error { panic("unmarshaltext boom") }

type boomTextMarshaler struct{}

func (boomTextMarshaler) MarshalText() ([]byte, error) { panic("marshaltext boom") }

// boomJSONMarshaler is a non-struct kind: a struct{} implementing only
// json.Marshaler would be walked as an (empty) struct by marshalStruct and
// never reach valueToString's json.Marshaler probe.
type boomJSONMarshaler int

func (boomJSONMarshaler) MarshalJSON() ([]byte, error) { panic("marshaljson boom") }

type boomUnmarshalEnv struct{}

func (boomUnmarshalEnv) UnmarshalEnv(map[string]string) error { panic("unmarshaleenv boom") }

func TestSEC003_PanickingMarshalerCallbacks(t *testing.T) {
	t.Run("UnmarshalText", func(t *testing.T) {
		var target struct {
			F boomTextUnmarshaler `env:"K"`
		}
		if err := UnmarshalInto(map[string]string{"K": "v"}, &target); err == nil ||
			!strings.Contains(err.Error(), "panicked") {
			t.Errorf("UnmarshalInto: err = %v, want contained panic error", err)
		}
	})

	t.Run("MarshalText", func(t *testing.T) {
		_, err := MarshalStruct(struct {
			F boomTextMarshaler `env:"K"`
		}{})
		if err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Errorf("MarshalStruct: err = %v, want contained panic error", err)
		}
	})

	t.Run("MarshalJSON", func(t *testing.T) {
		_, err := Marshal(struct {
			F boomJSONMarshaler `env:"K"`
		}{})
		if err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Errorf("Marshal: err = %v, want contained panic error", err)
		}
	})

	t.Run("UnmarshalEnv", func(t *testing.T) {
		if err := UnmarshalInto(map[string]string{"K": "v"}, &boomUnmarshalEnv{}); err == nil ||
			!strings.Contains(err.Error(), "panicked") {
			t.Errorf("UnmarshalInto(Unmarshaler): err = %v, want contained panic error", err)
		}
	})
}

// TestSEC003_RecoveringFileNormalPath pins that the wrappers are transparent
// for working custom components: the loader reads through the wrapped
// FileSystem and File and produces the expected value end to end.
func TestSEC003_RecoveringFileNormalPath(t *testing.T) {
	fs := newTestFileSystem()
	fs.files["ok.env"] = "KEY=value\n"

	cfg := DefaultConfig()
	cfg.Filenames = []string{"ok.env"}
	cfg.FileSystem = fs

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if v, ok := loader.Lookup("KEY"); !ok || v != "value" {
		t.Errorf("Lookup(KEY) = (%q, %v), want (\"value\", true)", v, ok)
	}
}
