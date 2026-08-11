// Package main demonstrates utility functions: introspection, masking,
// and format detection.
package main

import (
	"fmt"
	"log"

	"github.com/cybergodev/env"
)

func main() {
	if err := env.Load("examples/data/config.env"); err != nil {
		log.Fatalf("Failed to load: %v", err)
	}

	demonstrateIntrospection()

	demonstrateDelete()

	demonstrateMasking()

	demonstrateFormatDetection()
}

// demonstrateIntrospection shows Len, Keys, and All.
func demonstrateIntrospection() {
	fmt.Println("=== Introspection ===")
	fmt.Printf("  Total variables: %d\n", env.Len())
	fmt.Printf("  Keys: %v\n", env.Keys())

	// Iterate a sample from All()
	all := env.All()
	count := 0
	fmt.Println("  Sample from All():")
	for k, v := range all {
		if count >= 3 {
			break
		}
		fmt.Printf("    %s = %s\n", k, v)
		count++
	}
}

// demonstrateDelete shows set → delete → verify.
func demonstrateDelete() {
	fmt.Println("\n=== Delete ===")
	_ = env.Set("TEMP_VAR", "temporary")
	fmt.Printf("  Before: %s\n", env.GetString("TEMP_VAR"))

	_ = env.Delete("TEMP_VAR")
	_, exists := env.Lookup("TEMP_VAR")
	fmt.Printf("  After:  exists=%v\n", exists)
}

// demonstrateMasking shows sensitive-key detection and value masking.
func demonstrateMasking() {
	fmt.Println("\n=== Masking ===")

	// IsSensitiveKey — classify by key name
	for _, key := range []string{"DB_PASSWORD", "API_KEY", "APP_NAME"} {
		fmt.Printf("  %-16s sensitive=%v\n", key, env.IsSensitiveKey(key))
	}

	// MaskValue / MaskKey — redact for logging
	pw := env.GetString("DB_PASSWORD")
	fmt.Printf("\n  MaskValue(DB_PASSWORD): %s\n", env.MaskValue("DB_PASSWORD", pw))
	fmt.Printf("  MaskKey(DB_PASSWORD):    %s\n", env.MaskKey("DB_PASSWORD"))

	// SanitizeForLog — auto-mask sensitive patterns in arbitrary strings
	raw := fmt.Sprintf("Config: DB_PASSWORD=%s, API_KEY=%s",
		env.GetString("DB_PASSWORD"), env.GetString("API_KEY"))
	fmt.Printf("  SanitizeForLog: %s\n", env.SanitizeForLog(raw))

	// Safe iteration: auto-mask based on key sensitivity
	fmt.Println("\n  Safe iteration (first 5):")
	for i, key := range env.Keys() {
		if i >= 5 {
			break
		}
		val := env.GetString(key)
		if env.IsSensitiveKey(key) {
			fmt.Printf("    %s = %s\n", env.MaskKey(key), env.MaskValue(key, val))
		} else {
			fmt.Printf("    %s = %s\n", key, val)
		}
	}
}

// demonstrateFormatDetection shows DetectFormat for common extensions.
func demonstrateFormatDetection() {
	fmt.Println("\n=== Format Detection ===")
	for _, f := range []string{".env", "config.json", "settings.yaml", "app.yml", "unknown.txt"} {
		fmt.Printf("  %-16s → %s\n", f, env.DetectFormat(f))
	}
}
