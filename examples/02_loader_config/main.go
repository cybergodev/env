// Package main demonstrates Loader configuration using preset and custom configs.
// Use Instance Mode (New + Loader methods) for tests, multiple configs, or
// explicit lifecycle control — contrast with the global Load() in example 01.
package main

import (
	"fmt"
	"log"

	"github.com/cybergodev/env"
)

func main() {
	demonstratePresets()

	demonstrateCustomConfig()

	demonstrateLifecycle()
}

// demonstratePresets shows the four built-in configuration presets.
func demonstratePresets() {
	fmt.Println("=== Config Presets ===")

	// Each preset is optimized for a different environment.
	// Key differences:
	//   Default     – secure, moderate limits, no overwrite
	//   Development – relaxed limits, overwrite enabled, YAML values
	//   Production  – strict limits, audit enabled, fail-fast on missing files
	//   Testing     – compact limits, overwrite enabled, no audit
	presets := []struct {
		name string
		cfg  env.Config
	}{
		{"Default", env.DefaultConfig()},
		{"Development", env.DevelopmentConfig()},
		{"Production", env.ProductionConfig()},
		{"Testing", env.TestingConfig()},
	}

	for _, p := range presets {
		p.cfg.Filenames = []string{"examples/data/config.env"}
		// Production enables audit; use NopHandler so Close() won't close stdout.
		if p.cfg.AuditEnabled {
			p.cfg.AuditHandler = env.NewNopAuditHandler()
		}

		loader, err := env.New(p.cfg)
		if err != nil {
			log.Fatalf("%s: %v", p.name, err)
		}
		fmt.Printf("  %-14s %d variables\n", p.name+":", loader.Len())
		_ = loader.Close()
	}
}

// demonstrateCustomConfig shows prefix filtering and required-key validation.
func demonstrateCustomConfig() {
	fmt.Println("\n=== Custom Config (prefix + required keys) ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.Prefix = "DB_" // only process DB_ variables
	cfg.RequiredKeys = []string{"DB_HOST", "DB_PORT"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// Validate checks that all RequiredKeys are present.
	if err := loader.Validate(); err != nil {
		log.Fatalf("Validation failed: %v", err)
	}

	fmt.Printf("  DB_ keys: %v\n", loader.Keys())
}

// demonstrateLifecycle shows the LoadFiles/Apply/Close lifecycle.
func demonstrateLifecycle() {
	fmt.Println("\n=== Lifecycle (LoadFiles, Apply, Close) ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = nil // don't auto-load at construction

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// LoadFiles loads incrementally; later files can override earlier ones.
	if err := loader.LoadFiles("examples/data/config.env"); err != nil {
		log.Fatalf("LoadFiles failed: %v", err)
	}

	// Apply writes to os.Environ (New does NOT auto-apply unless AutoApply=true).
	if err := loader.Apply(); err != nil {
		log.Fatalf("Apply failed: %v", err)
	}

	fmt.Printf("  Loaded at:  %v\n", loader.LoadTime())
	fmt.Printf("  IsApplied:  %v\n", loader.IsApplied())
	fmt.Printf("  IsClosed:   %v\n", loader.IsClosed())
}
