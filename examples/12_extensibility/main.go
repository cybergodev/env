// Package main demonstrates extensibility: registering a custom parser for
// file formats the library does not know, and consuming a Loader through the
// small segregated interfaces (dependency injection) so code under test does
// not need files or loaders at all.
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateCustomParser()

	demonstrateInterfaces()

	demonstrateCustomComponents()
}

// colonParser parses a trivial "KEY: value" format (one pair per line, '#'
// comments) used here for legacy .conf files. A real parser should validate
// input with factory.Validator() and honor the Config's limits, like the
// built-in parsers do.
type colonParser struct {
	validator env.Validator
}

// Parse implements env.EnvParser.
func (p *colonParser) Parse(r io.Reader, filename string) (map[string]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	vars := make(map[string]string)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("%s: line %d: expected 'KEY: value'", filename, i+1)
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		if err := p.validator.ValidateKey(key); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", filename, i+1, err)
		}
		vars[key] = strings.TrimSpace(value)
	}
	return vars, nil
}

// switchToTempWorkDir moves the process into a fresh writable temporary
// directory and returns a restore function for `defer`. The path validator
// only accepts relative paths (absolute paths are rejected for security),
// so demos that create config files at runtime chdir into a temp directory
// instead of writing into the repository — read-only checkouts keep working.
func switchToTempWorkDir() func() {
	tmpDir, err := os.MkdirTemp("", "env-example-")
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		log.Fatalf("Failed to enter temp dir: %v", err)
	}

	return func() {
		_ = os.Chdir(oldWd)      // best-effort restore
		_ = os.RemoveAll(tmpDir) // best-effort cleanup
	}
}

// demonstrateCustomParser registers colonParser for FormatAuto — the format
// DetectFormat returns for unknown extensions such as .conf. Register before
// creating Loaders: each Loader snapshots the registry at construction.
func demonstrateCustomParser() {
	fmt.Println("=== Custom Parser (RegisterParser) ===")

	factory := func(cfg env.Config, f *env.ComponentFactory) (env.EnvParser, error) {
		return &colonParser{validator: f.Validator()}, nil
	}
	if err := env.RegisterParser(env.FormatAuto, factory); err != nil {
		log.Fatalf("RegisterParser failed: %v", err)
	}

	// Run from a temp working directory (see switchToTempWorkDir): loading
	// needs a relative path, and the generated .conf never touches the
	// repository.
	restore := switchToTempWorkDir()
	defer restore()

	const confFile = "_legacy.conf"
	content := "# legacy deployment settings\nSERVICE_HOST: api.example.com\nSERVICE_PORT: 8443\n"
	if err := os.WriteFile(confFile, []byte(content), 0o644); err != nil {
		log.Fatalf("Failed to write config file: %v", err)
	}

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{confFile}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	fmt.Printf("  SERVICE_HOST: %s\n", loader.GetString("SERVICE_HOST"))
	fmt.Printf("  SERVICE_PORT: %s\n", loader.GetString("SERVICE_PORT"))
}

// printAppSection depends only on env.EnvGetter — the smallest interface it
// needs. *env.Loader satisfies it (as do the other fine-grained interfaces:
// EnvSetter, EnvFileLoader, EnvApplicator, EnvCloser), and so does any stub.
func printAppSection(source string, g env.EnvGetter) {
	name := g.GetString("APP_NAME", "unknown")
	if _, ok := g.Lookup("APP_PORT"); ok {
		fmt.Printf("  %-9s app=%s (port configured)\n", source+":", name)
	}
}

// stubGetter is a fixed EnvGetter for unit tests — no files, no Loader.
type stubGetter struct{ name, port string }

func (s stubGetter) GetString(key string, defaultValue ...string) string {
	if key == "APP_NAME" {
		return s.name
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (s stubGetter) Lookup(key string) (string, bool) {
	if key == "APP_PORT" {
		return s.port, true
	}
	return "", false
}

func (s stubGetter) Keys() []string { return []string{"APP_NAME", "APP_PORT"} }

func (s stubGetter) All() map[string]string {
	return map[string]string{"APP_NAME": s.name, "APP_PORT": s.port}
}

// demonstrateInterfaces shows interface-segregated consumption: production
// code passes the real Loader, tests pass a stub — the consumer cannot tell
// (and does not care about) the difference.
func demonstrateInterfaces() {
	fmt.Println("\n=== Interfaces (EnvGetter) ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	printAppSection("loader", loader)                                   // real configuration
	printAppSection("stub", stubGetter{name: "test-app", port: "9090"}) // test double
}

// metricsAuditor counts audit events by action instead of writing log
// lines — e.g. to feed a monitoring endpoint. It implements the minimal
// AuditLogger interface (LogError only); the library adapts it to the full
// interface. It is safe for concurrent use, as the component contract
// requires (one component instance is shared by every operation).
//
// The same ComponentConfig mechanism accepts CustomValidator,
// CustomExpander, and a FileSystem override for tests.
type metricsAuditor struct {
	mu     sync.Mutex
	counts map[env.AuditAction]int
}

// LogError implements env.AuditLogger.
func (m *metricsAuditor) LogError(action env.AuditAction, key, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[action]++
	return nil
}

// demonstrateCustomComponents replaces the built-in audit logger with the
// custom implementation above via ComponentConfig.CustomAuditor.
func demonstrateCustomComponents() {
	fmt.Println("\n=== Custom Components (CustomAuditor) ===")

	auditor := &metricsAuditor{counts: make(map[env.AuditAction]int)}

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditEnabled = true
	cfg.CustomAuditor = auditor

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// A failed typed read is audited (successful reads are not).
	_ = loader.GetInt("APP_NAME")

	fmt.Printf("  Audit events by action: %v\n", auditor.counts)
}
