// Package main demonstrates error handling patterns: sentinel errors via
// errors.Is, and typed errors via errors.As.
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateSentinelErrors()

	demonstrateTypedErrors()

	demonstrateMissingFile()

	demonstrateClosedLoader()

	demonstrateForbiddenKeys()

	demonstrateExpansionErrors()
}

// demonstrateSentinelErrors checks for specific error conditions with errors.Is.
func demonstrateSentinelErrors() {
	fmt.Println("=== Sentinel Errors (errors.Is) ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"nonexistent.env"}
	cfg.FailOnMissingFile = true

	_, err := env.New(cfg)
	if errors.Is(err, env.ErrFileNotFound) {
		fmt.Println("  ErrFileNotFound: file does not exist")
	} else if err != nil {
		fmt.Printf("  Other error: %v\n", err)
	}
}

// demonstrateTypedErrors extracts structured details via errors.As.
func demonstrateTypedErrors() {
	fmt.Println("\n=== Typed Errors (errors.As) ===")

	// Trigger a ParseError with malformed input.
	// "1INVALID=x" violates the key pattern (must start with a letter).
	_, err := env.UnmarshalMap("1INVALID=x")
	if err != nil {
		var parseErr *env.ParseError
		if errors.As(err, &parseErr) {
			fmt.Printf("  ParseError: file=%q line=%d\n", parseErr.File, parseErr.Line)
		} else {
			fmt.Printf("  Other error: %v\n", err)
		}
	}

	// Trigger a ValidationError with a bad integer.
	var cfg struct {
		Port int `env:"BAD_INT"`
	}
	if err := env.UnmarshalInto(map[string]string{"BAD_INT": "not_a_number"}, &cfg); err != nil {
		var valErr *env.ValidationError
		if errors.As(err, &valErr) {
			fmt.Printf("  ValidationError: field=%s message=%s\n", valErr.Field, valErr.Message)
		}
	}
}

// demonstrateMissingFile shows graceful handling when FailOnMissingFile is false.
func demonstrateMissingFile() {
	fmt.Println("\n=== Missing File (graceful) ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"nonexistent.env"}
	cfg.FailOnMissingFile = false // default — missing files are silently skipped

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Expected nil error: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	fmt.Printf("  Loaded %d variables (file skipped silently)\n", loader.Len())
}

// demonstrateClosedLoader shows operations after Close return ErrClosed.
func demonstrateClosedLoader() {
	fmt.Println("\n=== Closed Loader ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = nil

	loader, _ := env.New(cfg)
	_ = loader.Close()

	if err := loader.Set("KEY", "value"); err != nil {
		if errors.Is(err, env.ErrClosed) {
			fmt.Println("  ErrClosed: cannot use a closed loader")
		}
	}
}

// demonstrateForbiddenKeys shows the key policy pair:
// ForbiddenKeys rejects the listed keys (on Set AND on file load);
// AllowedKeys is the mirror image — any key NOT on the list is rejected.
// Both fail with a SecurityError matching ErrForbiddenKey via errors.Is.
func demonstrateForbiddenKeys() {
	fmt.Println("\n=== Key Policy (Forbidden / Allowed) ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = nil
	cfg.ValidationConfig.ForbiddenKeys = []string{"AWS_SECRET_ACCESS_KEY"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	err = loader.Set("AWS_SECRET_ACCESS_KEY", "should-not-succeed")
	fmt.Printf("  forbidden: errors.Is(err, ErrForbiddenKey)=%v\n",
		errors.Is(err, env.ErrForbiddenKey))

	var secErr *env.SecurityError
	if errors.As(err, &secErr) {
		// Key names are masked in the message; the struct field carries the
		// sanitized key for programmatic handling.
		fmt.Printf("  SecurityError: action=%s key=%q\n", secErr.Action, secErr.Key)
	}

	// AllowedKeys: everything not explicitly permitted is rejected —
	// useful for configs with a fixed, known key set.
	allowCfg := env.DefaultConfig()
	allowCfg.ValidationConfig.AllowedKeys = []string{"APP_NAME"}
	_, err = env.UnmarshalMapWithConfig(allowCfg, "APP_NAME=ok\nEXTRA=blocked\n")
	fmt.Printf("  allowed:   errors.Is(err, ErrForbiddenKey)=%v\n",
		errors.Is(err, env.ErrForbiddenKey))
}

// demonstrateExpansionErrors shows expansion failures: reference cycles and
// the required-variable syntax ${VAR:?message}.
func demonstrateExpansionErrors() {
	fmt.Println("\n=== Expansion Errors (ExpansionError) ===")

	// A reference cycle: A → B → A.
	_, err := env.UnmarshalMap("A=${B}\nB=${A}\n")
	fmt.Printf("  cycle:    errors.Is(err, ErrExpansionDepth)=%v\n",
		errors.Is(err, env.ErrExpansionDepth))

	var expErr *env.ExpansionError
	if errors.As(err, &expErr) {
		fmt.Printf("  ExpansionError: kind=ExpansionDepthKind=%v\n",
			expErr.Kind == env.ExpansionDepthKind)
	}

	// ${VAR:?message} requires VAR to be set — fail fast on missing inputs.
	_, err = env.UnmarshalMap("DSN=${DB_DSN:?DB_DSN must be set}\n")
	fmt.Printf("  required: %v\n", err)
}
