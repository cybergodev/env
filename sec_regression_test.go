package env

// Regression tests for the SEC-001 security audit fixes (2026-09-06),
// root-package surface. Internal-package counterparts live in
// internal/sec_regression_test.go.

import (
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/cybergodev/env/internal"
)

// TestSEC01_UnmarshalMapJSONEnforcesForbiddenKeys pins the SEC-002 contract:
// the no-config JSON/YAML shortcut now runs the DefaultConfig validation
// pipeline, so the built-in forbidden-key policy applies uniformly.
func TestSEC01_UnmarshalMapJSONEnforcesForbiddenKeys(t *testing.T) {
	if _, err := UnmarshalMap(`{"PATH": "/evil"}`, FormatJSON); err == nil {
		t.Fatal("UnmarshalMap(JSON) accepted a forbidden key PATH")
	} else if !errors.Is(err, ErrForbiddenKey) {
		t.Fatalf("UnmarshalMap(JSON) error = %v, want it to match ErrForbiddenKey", err)
	}

	if _, err := UnmarshalMap("path: /evil\n", FormatYAML); err == nil {
		t.Fatal("UnmarshalMap(YAML) accepted a forbidden key PATH")
	} else if !errors.Is(err, ErrForbiddenKey) {
		t.Fatalf("UnmarshalMap(YAML) error = %v, want it to match ErrForbiddenKey", err)
	}
}

// TestSEC01_UnmarshalMapEnvStillRejectsForbiddenKeys pins that the .env path
// (which already validated) is unchanged after the JSON/YAML paths were
// brought to parity.
func TestSEC01_UnmarshalMapEnvStillRejectsForbiddenKeys(t *testing.T) {
	if _, err := UnmarshalMap("PATH=/evil\n"); err == nil {
		t.Fatal("UnmarshalMap(.env) accepted a forbidden key PATH")
	} else if !errors.Is(err, ErrForbiddenKey) {
		t.Fatalf("UnmarshalMap(.env) error = %v, want it to match ErrForbiddenKey", err)
	}
}

// TestSEC01_UnmarshalIntoDoesNotLeakValue pins the public contract: a
// conversion failure during struct mapping must not embed the raw value in
// the error message (values are frequently secrets).
func TestSEC01_UnmarshalIntoDoesNotLeakValue(t *testing.T) {
	const secret = "s3cr3t-PIN-9876"
	type Cfg struct {
		Pin int `env:"DB_PASSWORD"`
	}
	err := UnmarshalInto(map[string]string{"DB_PASSWORD": secret}, &Cfg{})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("raw value leaked: %v", err)
	}
	if !IsMarshalError(err) {
		t.Errorf("want MarshalError, got %T: %v", err, err)
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		t.Error("must not be a ValidationError carrying the value in .Value")
	}
}

// TestSEC02_UnmarshalMapYAMLTokenCap exercises the YAML token cap through
// the public UnmarshalMap path. The line count is derived from
// internal.HardMaxYAMLTokens so the pin cannot silently detach from the
// constant ("a\n" yields 2 tokens per line; HardMaxYAMLTokens/2+1 lines cross
// the cap).
func TestSEC02_UnmarshalMapYAMLTokenCap(t *testing.T) {
	input := strings.Repeat("a\n", internal.HardMaxYAMLTokens/2+1)
	_, err := UnmarshalMap(input, FormatYAML)
	if err == nil {
		t.Fatal("expected token cap error")
	}
	if !strings.Contains(err.Error(), "token count") {
		t.Errorf("error should mention token count: %v", err)
	}
}

// TestSEC04_MaskSensitiveInStringMasksPatterns is a single wiring check for
// the exported helper (key=value secret pairs are masked, not merely
// truncated); the authoritative pattern matrix lives in internal
// (TestSEC04_MaskInStringSanitizesPatterns).
func TestSEC04_MaskSensitiveInStringMasksPatterns(t *testing.T) {
	if got := MaskSensitiveInString("password=hunter2"); got != "password=[MASKED]" {
		t.Errorf("MaskSensitiveInString = %q, want password=[MASKED]", got)
	}
}

// TestSEC03_ExpansionFileOnly verifies the ExpansionFileOnly scope:
// file-local references expand normally, while references that would fall
// back to the process environment expand to empty. This is now the default
// scope, exercised explicitly here to pin the mechanism.
func TestSEC03_ExpansionFileOnly(t *testing.T) {
	t.Setenv("SECRET_PROCESS_TOKEN", "topsecret-xyz") // auto-restored after the test

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }() // best-effort restore

	content := "FOO=file-local\nLOCAL_REF=${FOO}\nLEAK=${SECRET_PROCESS_TOKEN}\n"
	if err := os.WriteFile(".env.sec03", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Filenames = []string{".env.sec03"}
	cfg.ExpansionScope = ExpansionFileOnly
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if v, _ := loader.Lookup("LOCAL_REF"); v != "file-local" {
		t.Errorf("LOCAL_REF = %q, want %q: file-local expansion must keep working", v, "file-local")
	}
	if v, _ := loader.Lookup("LEAK"); v != "" {
		t.Errorf("LEAK = %q, want empty: file-only scope must not read the process environment", v)
	}
}

// TestSEC03_DefaultIsFileOnly pins the SEC-003 default: a Loader built from
// DefaultConfig() must NOT read the process environment during expansion.
// The legacy dotenv fallback is now opt-in via ExpansionFileThenProcess.
func TestSEC03_DefaultIsFileOnly(t *testing.T) {
	t.Setenv("SEC03_PROCESS_VAR", "from-process")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }() // best-effort restore

	if err := os.WriteFile(".env.sec03", []byte("REF=${SEC03_PROCESS_VAR}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Filenames = []string{".env.sec03"}
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if v, _ := loader.Lookup("REF"); v != "" {
		t.Errorf("REF = %q, want empty (default scope must be file-only; process fallback is opt-in)", v)
	}
}

// TestSEC03_FileThenProcessOptIn verifies the legacy process-fallback scope
// still works when explicitly requested, so trusted-file callers can opt back
// into dotenv semantics after the SEC-003 default flip.
func TestSEC03_FileThenProcessOptIn(t *testing.T) {
	t.Setenv("SEC03_PROCESS_VAR", "from-process")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }() // best-effort restore

	if err := os.WriteFile(".env.sec03", []byte("REF=${SEC03_PROCESS_VAR}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Filenames = []string{".env.sec03"}
	cfg.ExpansionScope = ExpansionFileThenProcess
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if v, _ := loader.Lookup("REF"); v != "from-process" {
		t.Errorf("REF = %q, want %q (FileThenProcess opt-in must keep dotenv semantics)", v, "from-process")
	}
}

// TestSEC06_KeyPatternRejectsInjectionEnablers verifies custom key patterns
// that accept separator/newline characters fail Config validation — such
// keys previously enabled .env round-trip injection via Marshal.
func TestSEC06_KeyPatternRejectsInjectionEnablers(t *testing.T) {
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`^[A-Za-z](?s:.)*$`), // allows newline, '=', ':'
		regexp.MustCompile(`^[\w=]+$`),          // allows '='
		regexp.MustCompile(`^[\w:]+$`),          // allows ':'
	} {
		cfg := DefaultConfig()
		cfg.KeyPattern = pattern
		if err := cfg.Validate(); err == nil {
			t.Errorf("pattern %q should be rejected", pattern)
		}
	}

	// A safe custom pattern is still accepted.
	cfg := DefaultConfig()
	cfg.KeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)
	if err := cfg.Validate(); err != nil {
		t.Errorf("safe pattern rejected: %v", err)
	}
}

// TestSEC06_MarshalRejectsInjectionKeys keeps one representative example of
// Marshal refusing to emit a key that would re-parse as different lines (the
// injection primitive); internal SEC06 holds the full 17-case matrix across
// formats.
func TestSEC06_MarshalRejectsInjectionKeys(t *testing.T) {
	if _, err := Marshal(map[string]string{"A\nB=INJECTED": "1"}); err == nil {
		t.Error("Marshal(.env) accepted a key with an embedded newline")
	}
}

// TestSEC03_FileOnlyScopeNotTreatedAsZeroConfig pins the IsZero fix: a Config
// whose only non-default field is ExpansionScope used to be detected as a
// zero-value Config and silently replaced by DefaultConfig() inside New(),
// dropping the file-only expansion restriction without any error.
func TestSEC03_FileOnlyScopeNotTreatedAsZeroConfig(t *testing.T) {
	cfg := Config{ParsingConfig: ParsingConfig{ExpansionScope: ExpansionFileOnly}}
	if cfg.IsZero() {
		t.Fatal("Config with only ExpansionScope set must not be detected as zero — " +
			"New() would silently replace it with DefaultConfig() and drop the SEC-03 restriction")
	}

	// A partially-initialized Config now fails validation loudly, like any
	// other partially-filled Config, instead of being swapped for the default.
	if _, err := New(cfg); err == nil {
		t.Fatal("expected validation error for partially-initialized config, got nil")
	}
}

// TestSEC003_ZeroValueLoaderNoPanic pins the panic-safety contract for the
// zero-value Loader: every public method fails closed (ErrClosed from
// state-changing methods, zero values from getters) instead of
// dereferencing the nil internal map/factory. Valid Loaders from New() are
// unaffected — vars is always non-nil there.
func TestSEC003_ZeroValueLoaderNoPanic(t *testing.T) {
	var l Loader

	if !l.IsClosed() {
		t.Error("zero-value Loader should report IsClosed()=true (unusable)")
	}
	if err := l.Set("K", "v"); !errors.Is(err, ErrClosed) {
		t.Errorf("Set on zero-value Loader: err = %v, want ErrClosed", err)
	}
	if err := l.Delete("K"); !errors.Is(err, ErrClosed) {
		t.Errorf("Delete on zero-value Loader: err = %v, want ErrClosed", err)
	}
	if err := l.LoadFiles("x.env"); !errors.Is(err, ErrClosed) {
		t.Errorf("LoadFiles on zero-value Loader: err = %v, want ErrClosed", err)
	}
	if err := l.Apply(); !errors.Is(err, ErrClosed) {
		t.Errorf("Apply on zero-value Loader: err = %v, want ErrClosed", err)
	}
	if err := l.Validate(); !errors.Is(err, ErrClosed) {
		t.Errorf("Validate on zero-value Loader: err = %v, want ErrClosed", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close on zero-value Loader: err = %v, want nil", err)
	}
	if v, ok := l.Lookup("K"); ok || v != "" {
		t.Errorf("Lookup on zero-value Loader = (%q, %v), want (\"\", false)", v, ok)
	}
	if l.GetSecure("K") != nil {
		t.Error("GetSecure on zero-value Loader should return nil")
	}
	if s := l.GetString("K", "dflt"); s != "dflt" {
		t.Errorf("GetString on zero-value Loader = %q, want default", s)
	}
	if n := l.Len(); n != 0 {
		t.Errorf("Len on zero-value Loader = %d, want 0", n)
	}
	if keys := l.Keys(); keys != nil {
		t.Errorf("Keys on zero-value Loader = %v, want nil", keys)
	}
	if m := l.All(); m != nil {
		t.Errorf("All on zero-value Loader = %v, want nil", m)
	}
	if got := GetSliceFrom[string](&l, "K"); got != nil {
		t.Errorf("GetSliceFrom on zero-value Loader = %v, want nil", got)
	}
}

// TestSEC003_ParserFactoryPanicReturnsError pins the construction boundary:
// a user-supplied ParserFactory that panics surfaces as a creation error
// from createParsers (and therefore New/LoadWithConfig) instead of crashing
// the process — the same containment the auditor applies to user handlers.
func TestSEC003_ParserFactoryPanicReturnsError(t *testing.T) {
	format := nextTestFormat()

	globalParserRegistry.mu.Lock()
	globalParserRegistry.factories[format] = func(cfg Config, factory *ComponentFactory) (EnvParser, error) {
		panic("simulated factory panic")
	}
	globalParserRegistry.hasCustom.Store(true)
	globalParserRegistry.mu.Unlock()

	defer func() {
		globalParserRegistry.mu.Lock()
		delete(globalParserRegistry.factories, format)
		globalParserRegistry.mu.Unlock()
	}()

	cfg := DefaultConfig()
	factory := cfg.buildComponentFactory()
	defer func() { _ = factory.Close() }() // best-effort cleanup

	parsers, err := createParsers(cfg, factory)
	if err == nil {
		for _, p := range parsers {
			if closer, ok := p.(io.Closer); ok {
				_ = closer.Close()
			}
		}
		t.Fatal("createParsers should return an error for a panicking factory")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("error should mention the panic: %v", err)
	}
}
