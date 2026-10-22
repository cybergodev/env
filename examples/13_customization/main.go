// Package main demonstrates custom components and advanced parsing rules:
// a custom Validator (key/value/required), a custom variable Expander, a
// custom in-memory FileSystem, fail-closed auditing, custom
// Marshaler/Unmarshaler codecs, and the brace-expansion operators.
//
// Every scenario inlines its own data (UnmarshalMapWithConfig or an
// in-memory FileSystem), so the example runs from any working directory and
// never writes to the repository.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateCustomValidator()

	demonstrateCustomExpander()

	demonstrateInMemoryFileSystem()

	demonstrateFailClosedAudit()

	demonstrateCustomMarshalUnmarshal()

	demonstrateBraceOperators()
}

// prefixValidator is a full env.Validator implementation (ValidateKey,
// ValidateValue, ValidateRequired) that replaces the built-in validator. It
// rejects keys with a reserved prefix, rejects empty values, and requires a
// specific key to be present.
type prefixValidator struct{}

func (prefixValidator) ValidateKey(key string) error {
	if strings.HasPrefix(key, "BLOCKED_") {
		return fmt.Errorf("key %q uses a reserved prefix", key)
	}
	return nil
}

func (prefixValidator) ValidateValue(value string) error {
	if value == "" {
		return errors.New("empty values are not allowed")
	}
	return nil
}

func (prefixValidator) ValidateRequired(keys map[string]bool) error {
	// keys is the uppercase key set; the built-in validator reads
	// Config.RequiredKeys, but a custom validator must enforce its own.
	if !keys["REQUIRED_KEY"] {
		return errors.New("REQUIRED_KEY must be present")
	}
	return nil
}

// demonstrateCustomValidator wires prefixValidator into one-shot parsing.
// Key and value rules run at parse time; the required-key rule runs at the
// end of the parse (and again on Loader.Validate for file loads).
func demonstrateCustomValidator() {
	fmt.Println("=== Custom Validator ===")

	cfg := env.DefaultConfig()
	cfg.CustomValidator = prefixValidator{}

	// Key rule: BLOCKED_X is rejected before the value rules are reached.
	_, err := env.UnmarshalMapWithConfig(cfg, "BLOCKED_X=ok\n")
	fmt.Printf("  blocked key:  %v\n", err)

	// Value rule: an empty value is rejected.
	_, err = env.UnmarshalMapWithConfig(cfg, "SOME_KEY=\n")
	fmt.Printf("  empty value:  %v\n", err)

	// Required rule: REQUIRED_KEY must be present.
	_, err = env.UnmarshalMapWithConfig(cfg, "SOME_KEY=ok\n")
	fmt.Printf("  missing req:  %v\n", err)

	// All rules satisfied.
	m, err := env.UnmarshalMapWithConfig(cfg, "REQUIRED_KEY=present\nSOME_KEY=ok\n")
	if err != nil {
		log.Fatalf("expected success: %v", err)
	}
	fmt.Printf("  accepted:     %v\n", m)

	// A custom validator replaces the built-in, so the built-in sentinels
	// (env.ErrInvalidKey) do NOT match its errors — errors.Is only matches
	// the built-in validator's typed errors.
	fmt.Printf("  is ErrInvalidKey: %v\n", errors.Is(err, env.ErrInvalidKey))
}

// percentExpander is a custom env.VariableExpander that resolves %VAR%
// references against its own captured map. The built-in expander handles
// $VAR / ${VAR}; replacing it lets a caller support a different syntax, or a
// non-environment source, without touching the file format.
//
// The contract is minimal: Expand receives one raw value (with references
// unresolved) and returns the expanded value. A custom expander owns its own
// lookup; it is called for .env values only — JSON and YAML are never
// expanded.
type percentExpander struct{ vars map[string]string }

func (p percentExpander) Expand(s string) (string, error) {
	for k, v := range p.vars {
		s = strings.ReplaceAll(s, "%"+k+"%", v)
	}
	return s, nil
}

func demonstrateCustomExpander() {
	fmt.Println("\n=== Custom Expander ===")

	cfg := env.DefaultConfig()
	cfg.CustomExpander = percentExpander{vars: map[string]string{"HOST": "api.example.com"}}

	// %HOST% resolves through the custom expander; $VAR does NOT (the built-in
	// expander was replaced, so $DB_HOST passes through verbatim).
	m, err := env.UnmarshalMapWithConfig(cfg,
		"GREETING=hello %HOST%\nDB_HOST=localhost\nURL=http://$DB_HOST\n")
	if err != nil {
		log.Fatalf("expand failed: %v", err)
	}
	fmt.Printf("  GREETING: %q\n", m["GREETING"])
	fmt.Printf("  URL:      %q (\\$DB_HOST left verbatim)\n", m["URL"])
}

// memFileSystem is an in-memory env.FileSystem: files live in a map instead
// of on disk. It demonstrates the dependency-injection seam used to make
// tests (and embedded configurations) independent of the real filesystem.
type memFileSystem struct{ files map[string]string }

func (fs memFileSystem) Open(name string) (env.File, error) {
	content, ok := fs.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &memFile{data: content}, nil
}
func (fs memFileSystem) OpenFile(name string, flag int, perm os.FileMode) (env.File, error) {
	return fs.Open(name)
}
func (fs memFileSystem) Stat(name string) (os.FileInfo, error) {
	content, ok := fs.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &memFileInfo{size: int64(len(content))}, nil
}
func (fs memFileSystem) MkdirAll(string, os.FileMode) error { return nil }
func (fs memFileSystem) Remove(string) error                { return nil }
func (fs memFileSystem) Rename(string, string) error        { return nil }
func (fs memFileSystem) Getenv(key string) string {
	return "" // not used by this demo
}
func (fs memFileSystem) Setenv(string, string) error     { return nil }
func (fs memFileSystem) Unsetenv(string) error           { return nil }
func (fs memFileSystem) LookupEnv(string) (string, bool) { return "", false }

// memFile implements env.File over an in-memory string.
type memFile struct {
	data string
	pos  int
}

func (f *memFile) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}
func (f *memFile) Write(p []byte) (int, error) { return len(p), nil }
func (f *memFile) Close() error                { return nil }
func (f *memFile) Stat() (os.FileInfo, error)  { return &memFileInfo{size: int64(len(f.data))}, nil }
func (f *memFile) Sync() error                 { return nil }

// memFileInfo is the minimal os.FileInfo the loader's size check needs.
type memFileInfo struct{ size int64 }

func (fi *memFileInfo) Name() string       { return "mem" }
func (fi *memFileInfo) Size() int64        { return fi.size }
func (fi *memFileInfo) Mode() os.FileMode  { return 0o644 }
func (fi *memFileInfo) ModTime() time.Time { return time.Now() }
func (fi *memFileInfo) IsDir() bool        { return false }
func (fi *memFileInfo) Sys() interface{}   { return nil }

func demonstrateInMemoryFileSystem() {
	fmt.Println("\n=== In-Memory FileSystem ===")

	cfg := env.DefaultConfig()
	cfg.FileSystem = memFileSystem{files: map[string]string{
		"config.env": "APP_NAME=embedded\nAPP_PORT=9090\n",
	}}
	cfg.Filenames = []string{"config.env"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("New failed: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	fmt.Printf("  APP_NAME: %s\n", loader.GetString("APP_NAME"))
	fmt.Printf("  APP_PORT: %s\n", loader.GetString("APP_PORT"))
}

// failingHandler is an env.AuditHandler whose Log always fails. Combined
// with AuditFailClosed, a lost audit event fails the operation that emitted
// it instead of being silently discarded (the default is fail-open).
type failingHandler struct{}

func (failingHandler) Log(env.AuditEvent) error { return errors.New("disk full") }
func (failingHandler) Close() error             { return nil }

func demonstrateFailClosedAudit() {
	fmt.Println("\n=== Fail-Closed Audit ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = nil // no load at construction — audit stays clean until Set
	cfg.AuditEnabled = true
	cfg.AuditFailClosed = true
	cfg.AuditHandler = failingHandler{}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("New failed: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// The Set's audit event cannot be recorded, so the operation reports the
	// loss. A CustomAuditor participates the same way by implementing
	// TakeFailure() error.
	err = loader.Set("SECRET_VAR", "value")
	fmt.Printf("  Set: errors.Is(err, ErrAuditFailed)=%v\n",
		errors.Is(err, env.ErrAuditFailed))
}

// Coordinates implements env.Marshaler and env.Unmarshaler. The custom codecs
// take precedence over struct-tag reflection: MarshalStruct serializes through
// MarshalEnv, and UnmarshalInto populates through UnmarshalEnv. Here the two
// float fields collapse into a single comma-joined GEO_POINT value.
type Coordinates struct {
	Lat float64
	Lng float64
}

func (c *Coordinates) MarshalEnv() ([]byte, error) {
	return []byte(fmt.Sprintf("GEO_POINT=%.3f,%.3f\n", c.Lat, c.Lng)), nil
}

func (c *Coordinates) UnmarshalEnv(m map[string]string) error {
	raw, ok := m["GEO_POINT"]
	if !ok {
		return errors.New("missing GEO_POINT")
	}
	lat, lng, ok := strings.Cut(raw, ",")
	if !ok {
		return fmt.Errorf("GEO_POINT %q is not a lat,lng pair", raw)
	}
	var err error
	if c.Lat, err = strconv.ParseFloat(lat, 64); err != nil {
		return fmt.Errorf("parse lat: %w", err)
	}
	if c.Lng, err = strconv.ParseFloat(lng, 64); err != nil {
		return fmt.Errorf("parse lng: %w", err)
	}
	return nil
}

func demonstrateCustomMarshalUnmarshal() {
	fmt.Println("\n=== Custom Marshaler / Unmarshaler ===")

	original := &Coordinates{Lat: 40.7128, Lng: -74.0060}

	envMap, err := env.MarshalStruct(original)
	if err != nil {
		log.Fatalf("MarshalStruct failed: %v", err)
	}
	fmt.Printf("  marshaled:   %v\n", envMap)

	var restored Coordinates
	if err := env.UnmarshalInto(envMap, &restored); err != nil {
		log.Fatalf("UnmarshalInto failed: %v", err)
	}
	fmt.Printf("  round-trip:  %+v\n", restored)
}

// demonstrateBraceOperators shows the shell-style brace operators the
// built-in expander understands: ${VAR:-default}, ${VAR:=default}, and
// ${VAR:?message}, plus the $$ escape for a literal dollar sign.
func demonstrateBraceOperators() {
	fmt.Println("\n=== Brace Operators ===")

	// ${VAR:-default}: substitute the default when VAR is unset/empty.
	m, err := env.UnmarshalMap("A=${MISSING:-fallback}\n")
	if err != nil {
		log.Fatalf(":- failed: %v", err)
	}
	fmt.Printf("  :-  A=%q\n", m["A"])

	// ${VAR:=default}: substitute AND assign — the key materializes in the map.
	m, err = env.UnmarshalMap("B=${ASSIGNED:=created}\n")
	if err != nil {
		log.Fatalf(":= failed: %v", err)
	}
	fmt.Printf("  :=  B=%q (ASSIGNED=%q)\n", m["B"], m["ASSIGNED"])

	// ${VAR:?message}: fail fast when VAR is unset.
	_, err = env.UnmarshalMap("C=${NEEDED:?NEEDED must be set}\n")
	fmt.Printf("  :?  err=%v\n", err)

	// $$: a literal dollar sign, not a variable reference.
	m, _ = env.UnmarshalMap("D=$$HOME\n")
	fmt.Printf("  $$  D=%q\n", m["D"])
}
