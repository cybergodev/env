// Package main demonstrates advanced features: variable expansion,
// multiple file loading with override, and prefix-based filtering.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateVariableExpansion()

	demonstrateMultipleFiles()

	demonstratePrefixFilter()
}

// demonstrateVariableExpansion shows ${VAR} and $VAR interpolation.
// ExpandVariables is enabled by default in all preset configs.
func demonstrateVariableExpansion() {
	fmt.Println("=== Variable Expansion ===")

	// config.env defines:
	//   APP_HOST=localhost
	//   APP_PORT=8080
	//   APP_URL=http://${APP_HOST}:${APP_PORT}
	// ExpandVariables interpolates ${APP_HOST} → localhost, ${APP_PORT} → 8080.
	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.ExpandVariables = true // enabled by default, shown for clarity

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	fmt.Printf("  APP_HOST: %s\n", loader.GetString("APP_HOST"))
	fmt.Printf("  APP_PORT: %s\n", loader.GetString("APP_PORT"))
	fmt.Printf("  APP_URL:  %s  (expanded)\n", loader.GetString("APP_URL"))
}

// demonstrateMultipleFiles shows override behavior when loading
// multiple files — later files win when OverwriteExisting is true.
func demonstrateMultipleFiles() {
	fmt.Println("\n=== Multiple Files (override) ===")

	// Create a temporary override file using a relative path.
	// The path validator blocks absolute paths for security.
	const overrideFile = "examples/data/_override.env"
	if err := os.WriteFile(overrideFile, []byte("APP_PORT=9999\n"), 0o644); err != nil {
		log.Fatalf("Failed to write override file: %v", err)
	}
	defer os.Remove(overrideFile)

	cfg := env.DevelopmentConfig() // OverwriteExisting=true
	cfg.Filenames = []string{
		"examples/data/config.env", // base: APP_PORT=8080
		overrideFile,               // overrides: APP_PORT=9999
	}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	fmt.Printf("  APP_PORT: %s (base=8080, overridden by second file)\n",
		loader.GetString("APP_PORT"))
}

// demonstratePrefixFilter shows how Prefix restricts processing to
// variables matching the given prefix.
func demonstratePrefixFilter() {
	fmt.Println("\n=== Prefix Filter ===")

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.Prefix = "FEATURE_" // only FEATURE_* variables are loaded

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	fmt.Printf("  FEATURE_ keys: %v\n", loader.Keys())
}
