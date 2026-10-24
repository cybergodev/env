package env_test

// Regression pins for cookie-style values under dotted YAML keys: values
// containing '=', ';', spaces, '+', '/', embedded double quotes and inline
// JSON regions (g_state={...}) must round-trip whole through the YAML
// loader, and Marshal must re-quote them so a reload is lossless. The
// simplified shape (Code=abc; g_state={"i_l":0}) is pinned by
// TestSEC06_MarshalOutputIsLoadable; these tests hold the full field shape
// that triggered the review.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cybergodev/env"
)

// reviewCookieValue is the exact value from the review request: an AP-style
// cookie whose g_state attribute embeds a compact JSON object with nested
// braces, quoted strings, and base64ish content ('+', '/').
const reviewCookieValue = `Code=scivqvknmzlrlloi; Visitor=scivqvknmzlrlloi; g_state={"i_l":0,"i_ll":1792316390803,"i_b":"hMB/iUY+3xgSvikIIEagFJMc6bpqxlrur7jBExbCKg4","i_e":{"enable_itp_optimization":24},"i_et":1792316390803,"i_t":1792402790803}`

const reviewCookieYAML = "apnews.com: " + reviewCookieValue + "\n"

func TestYAML_CookieValueUnderDottedKey(t *testing.T) {
	m, err := env.UnmarshalMap(reviewCookieYAML, env.FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalMap error: %v", err)
	}
	if len(m) != 1 {
		t.Errorf("want exactly 1 entry, got %d: %v", len(m), m)
	}
	if got := m["APNEWS.COM"]; got != reviewCookieValue {
		t.Errorf("value round-trip mismatch:\n got: %.60q...\nwant: %.60q...", got, reviewCookieValue)
	}
}

func TestYAML_CookieValueLoaderLookup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(reviewCookieYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }() // best-effort restore

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"config.yaml"}
	loader, err := env.New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// The literal dotted key and its lowercase spelling both resolve.
	for _, key := range []string{"APNEWS.COM", "apnews.com"} {
		if v, ok := loader.Lookup(key); !ok || v != reviewCookieValue {
			t.Errorf("Lookup(%q) = (%t, %.60q...)", key, ok, v)
		}
	}
}

func TestYAML_CookieValueMarshalRoundTrip(t *testing.T) {
	out, err := env.Marshal(map[string]string{"apnews.com": reviewCookieValue}, env.FormatYAML)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	back, err := env.UnmarshalMap(out, env.FormatYAML)
	if err != nil {
		t.Fatalf("re-parse of Marshal output failed: %v\noutput:\n%s", err, out)
	}
	if got := back["APNEWS.COM"]; got != reviewCookieValue {
		t.Errorf("Marshal round-trip mismatch:\n got: %.60q...\nwant: %.60q...\nmarshaled:\n%s", got, reviewCookieValue, out)
	}
}
