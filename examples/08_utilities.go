//go:build examples

// Package main demonstrates utility functions for environment management.
// These helpers are useful for introspection, iteration, and safe logging.
package main

import (
	"fmt"
	"log"

	"github.com/cybergodev/env"
)

func main() {
	// Initialize environment from file
	if err := env.Load("examples/data/config.env"); err != nil {
		log.Fatalf("Failed to load: %v", err)
	}

	demonstrateIntrospection()

	demonstrateDelete()

	demonstrateMasking()

	demonstrateFormatDetection()
}

func demonstrateIntrospection() {
	fmt.Println("=== Keys, All, Len ===")
	// Len returns the count of loaded variables
	fmt.Printf("Total variables: %d\n", env.Len())

	// Keys returns all key names
	keys := env.Keys()
	fmt.Printf("Keys: %v\n", keys)

	// All returns the complete environment as a map
	all := env.All()
	fmt.Printf("Sample from All():\n")
	count := 0
	for k, v := range all {
		if count >= 3 {
			break
		}
		fmt.Printf("  %s = %s\n", k, v)
		count++
	}
}

func demonstrateDelete() {
	fmt.Println("\n=== Delete ===")
	// Set a temporary value
	if err := env.Set("TEMP_VAR", "temporary"); err != nil {
		log.Printf("Set failed: %v", err)
	}
	fmt.Printf("Before delete: %s\n", env.GetString("TEMP_VAR"))

	// Delete removes the key
	if err := env.Delete("TEMP_VAR"); err != nil {
		log.Printf("Delete failed: %v", err)
	}

	value, exists := env.Lookup("TEMP_VAR")
	fmt.Printf("After delete: exists=%v, value=%q\n", exists, value)
}

func demonstrateMasking() {
	fmt.Println("\n=== Sensitive Key Masking ===")
	// IsSensitiveKey checks if a key name suggests sensitive data
	sensitiveKeys := []string{"DB_PASSWORD", "API_KEY", "APP_NAME"}
	for _, key := range sensitiveKeys {
		if env.IsSensitiveKey(key) {
			fmt.Printf("%s: SENSITIVE\n", key)
		} else {
			fmt.Printf("%s: not sensitive\n", key)
		}
	}

	// MaskValue masks sensitive values for safe logging
	password := env.GetString("DB_PASSWORD")
	masked := env.MaskValue("DB_PASSWORD", password)
	fmt.Printf("\nMasked password: %s\n", masked)

	// MaskKey masks key names containing sensitive words
	maskedKey := env.MaskKey("DB_PASSWORD")
	fmt.Printf("Masked key: %s\n", maskedKey)

	// SanitizeForLog automatically masks both keys and values
	logLine := fmt.Sprintf("Config: DB_PASSWORD=%s, API_KEY=%s, APP_NAME=%s",
		env.GetString("DB_PASSWORD"),
		env.GetString("API_KEY"),
		env.GetString("APP_NAME"))
	safeLog := env.SanitizeForLog(logLine)
	fmt.Printf("\nSafe log output:\n%s\n", safeLog)

	// MaskSensitiveInString masks patterns in arbitrary strings
	text := "Connection string: postgres://admin:secret123@localhost:5432/mydb"
	maskedText := env.MaskSensitiveInString(text)
	fmt.Printf("\nMasked connection string:\n%s\n", maskedText)

	// Example: safely iterate and log all config (show first 5)
	fmt.Println("\nSafe iteration example (first 5):")
	count := 0
	for _, key := range env.Keys() {
		if count >= 5 {
			break
		}
		value := env.GetString(key)
		// Auto-mask based on key sensitivity
		if env.IsSensitiveKey(key) {
			fmt.Printf("  %s = %s\n", env.MaskKey(key), env.MaskValue(key, value))
		} else {
			fmt.Printf("  %s = %s\n", key, value)
		}
		count++
	}
}

func demonstrateFormatDetection() {
	fmt.Println("\n=== Format Detection ===")
	// DetectFormat identifies file format by extension
	formats := []string{".env", "config.json", "settings.yaml", "app.yml", "unknown.txt"}
	for _, filename := range formats {
		fmt.Printf("%s → %s\n", filename, env.DetectFormat(filename))
	}
}
