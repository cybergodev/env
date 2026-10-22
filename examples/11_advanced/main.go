// Package main demonstrates advanced features: variable expansion,
// multiple file loading with override, and prefix-based filtering.
package main

import (
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateVariableExpansion()

	demonstrateExpansionScope()

	demonstrateMultipleFiles()

	demonstratePrefixFilter()

	demonstrateGlobalControl()
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
	defer func() { _ = loader.Close() }() // best-effort cleanup

	fmt.Printf("  APP_HOST: %s\n", loader.GetString("APP_HOST"))
	fmt.Printf("  APP_PORT: %s\n", loader.GetString("APP_PORT"))
	fmt.Printf("  APP_URL:  %s  (expanded)\n", loader.GetString("APP_URL"))
}

// demonstrateExpansionScope restricts what ${VAR} references can read.
// The default (ExpansionFileOnly) blocks the process-environment fallback so
// an untrusted config file cannot capture unrelated process secrets (SEC-03);
// ExpansionFileThenProcess restores the dotenv-style fallback for files you
// trust.
func demonstrateExpansionScope() {
	fmt.Println("\n=== Expansion Scope (SEC-03) ===")

	const token = "process-secret-token"
	if err := os.Setenv("CLOUD_TOKEN", token); err != nil {
		log.Fatalf("Setenv failed: %v", err)
	}
	defer func() { _ = os.Unsetenv("CLOUD_TOKEN") }() // best-effort cleanup

	const untrusted = "CALLBACK_URL=https://hooks.example.com?t=${CLOUD_TOKEN}"

	// Default scope: file-only, so the process variable is invisible and the
	// reference expands to the empty string.
	leaky, err := env.UnmarshalMap(untrusted)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  default:     CALLBACK_URL=%q (process var invisible)\n",
		leaky["CALLBACK_URL"])

	// File-then-process scope: the file reads the process variable. Opt-in for
	// fully trusted configuration files only.
	cfg := env.DefaultConfig()
	cfg.ParsingConfig.ExpansionScope = env.ExpansionFileThenProcess
	safe, err := env.UnmarshalMapWithConfig(cfg, untrusted)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  file+proc:   CALLBACK_URL=%q (process var read)\n",
		safe["CALLBACK_URL"])
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

// demonstrateMultipleFiles shows override behavior when loading
// multiple files — later files win when OverwriteExisting is true.
func demonstrateMultipleFiles() {
	fmt.Println("\n=== Multiple Files (override) ===")

	// Stage the base config and the generated override file in a temp
	// working directory (see switchToTempWorkDir): loading needs relative
	// paths, and the repository itself stays untouched.
	base, err := os.ReadFile("examples/data/config.env")
	if err != nil {
		log.Fatalf("Failed to read config.env: %v", err)
	}
	restore := switchToTempWorkDir()
	defer restore()
	if err := os.WriteFile("config.env", base, 0o644); err != nil {
		log.Fatalf("Failed to stage config.env: %v", err)
	}
	if err := os.WriteFile("_override.env", []byte("APP_PORT=9999\n"), 0o644); err != nil {
		log.Fatalf("Failed to write override file: %v", err)
	}

	cfg := env.DevelopmentConfig() // OverwriteExisting=true
	cfg.Filenames = []string{
		"config.env",    // base: APP_PORT=8080
		"_override.env", // overrides: APP_PORT=9999
	}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

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
	defer func() { _ = loader.Close() }() // best-effort cleanup

	featureKeys := loader.Keys()
	sort.Strings(featureKeys) // Keys() order is unspecified
	fmt.Printf("  FEATURE_ keys: %v\n", featureKeys)
}

// demonstrateGlobalControl shows the package-level loader lifecycle:
// Load (first definition wins), Overload (later definitions win),
// ResetDefaultLoader (the global loader is a singleton — re-initializing
// requires an explicit reset), and LoadWithConfig (the fully configurable
// variant of both presets).
func demonstrateGlobalControl() {
	fmt.Println("\n=== Global Loader Control (Load / Overload / Reset) ===")

	// Same relative-path requirement as the override demo: stage the base
	// config and the generated file in a temp working directory.
	base, err := os.ReadFile("examples/data/config.env")
	if err != nil {
		log.Fatalf("Failed to read config.env: %v", err)
	}
	restore := switchToTempWorkDir()
	defer restore()
	if err := os.WriteFile("config.env", base, 0o644); err != nil {
		log.Fatalf("Failed to stage config.env: %v", err)
	}
	if err := os.WriteFile("_global.env", []byte("APP_PORT=9999\n"), 0o644); err != nil {
		log.Fatalf("Failed to write override file: %v", err)
	}

	// Load: OverwriteExisting is off — the first definition of APP_PORT wins.
	if err := env.Load("config.env", "_global.env"); err != nil {
		log.Fatalf("Load failed: %v", err)
	}
	fmt.Printf("  Load:     APP_PORT=%s (first file wins)\n", env.GetString("APP_PORT"))

	// The default loader is already initialized — reset before re-loading.
	if err := env.ResetDefaultLoader(); err != nil {
		log.Fatalf("ResetDefaultLoader failed: %v", err)
	}

	// Overload: preset shortcut with OverwriteExisting — later files win.
	if err := env.Overload("config.env", "_global.env"); err != nil {
		log.Fatalf("Overload failed: %v", err)
	}
	fmt.Printf("  Overload: APP_PORT=%s (later file wins)\n", env.GetString("APP_PORT"))

	// LoadWithConfig: any Config, same global convenience functions.
	if err := env.ResetDefaultLoader(); err != nil {
		log.Fatalf("ResetDefaultLoader failed: %v", err)
	}
	gcfg := env.DefaultConfig()
	gcfg.Filenames = []string{"config.env"}
	gcfg.Prefix = "DB_"
	if err := env.LoadWithConfig(gcfg); err != nil {
		log.Fatalf("LoadWithConfig failed: %v", err)
	}
	fmt.Printf("  LoadWithConfig: DB_HOST=%s (%d DB_ keys via Prefix)\n",
		env.GetString("DB_HOST"), env.Len())
}
