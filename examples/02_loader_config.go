//go:build examples

// Package main demonstrates Loader configuration options.
// Use this when you need fine-grained control over loading behavior.
package main

import (
	"fmt"
	"log"

	"github.com/cybergodev/env"
)

func main() {

	demonstrateDefaultConfig()

	demonstrateDevelopmentConfig()

	demonstrateProductionConfig()

	demonstrateCustomConfig()
}

func demonstrateDefaultConfig() {
	fmt.Println("=== Default Configuration ===")
	// DefaultConfig provides secure defaults suitable for most use cases.
	// Files configured in cfg.Filenames are automatically loaded by New().
	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	fmt.Printf("Loaded %d variables\n", loader.Len())
	fmt.Printf("APP_NAME: %s\n", loader.GetString("APP_NAME"))
}

func demonstrateDevelopmentConfig() {
	fmt.Println("\n=== Development Configuration ===")
	// DevelopmentConfig is optimized for development:
	// - FailOnMissingFile: false (graceful handling)
	// - OverwriteExisting: true (easy iteration)
	// - Relaxed size limits
	cfg := env.DevelopmentConfig()
	cfg.Filenames = []string{"examples/data/config.env"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	fmt.Printf("APP_ENV: %s\n", loader.GetString("APP_ENV"))
}

func demonstrateProductionConfig() {
	fmt.Println("\n=== Production Configuration ===")
	// ProductionConfig provides maximum security:
	// - FailOnMissingFile: true (fail fast)
	// - AuditEnabled: true (compliance)
	// - Strict size limits
	//
	// IMPORTANT: Don't use os.Stdout with JSONAuditHandler in production
	// because Close() will close stdout. Use a file or custom writer instead.
	// Here we use a NopAuditHandler for demo purposes to avoid closing stdout.
	cfg := env.ProductionConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditHandler = env.NewNopAuditHandler() // Use NopHandler to avoid closing stdout

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	fmt.Printf("Loaded with audit enabled: %d variables\n", loader.Len())
}

func demonstrateCustomConfig() {
	fmt.Println("\n=== Custom Configuration ===")
	// Create a fully custom configuration.
	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.OverwriteExisting = true
	cfg.Prefix = "DB_" // Only process variables that start with the DB_ prefix
	cfg.RequiredKeys = []string{"DB_HOST", "DB_PORT"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// Validate required keys
	if err := loader.Validate(); err != nil {
		log.Fatalf("Validation failed: %v", err)
	}

	// Only DB_ prefixed variables are accessible
	keys := loader.Keys()
	fmt.Printf("Variables with DB_ prefix: %v\n", keys)
}
