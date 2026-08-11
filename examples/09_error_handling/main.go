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
	defer loader.Close()

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
