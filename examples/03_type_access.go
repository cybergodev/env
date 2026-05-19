//go:build examples

// Package main demonstrates typed access methods for environment variables.
// The library provides convenient getters for common types.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cybergodev/env"
)

func main() {
	// Initialize configuration from JSON file
	if err := env.Load("examples/data/config.json"); err != nil {
		log.Fatalf("Failed to load: %v", err)
	}

	demonstrateStringAccess()

	demonstrateIntAccess()

	demonstrateBoolAccess()

	demonstrateDurationAccess()

	demonstrateFloatAccess()

	demonstrateUintAccess()

	demonstrateSliceAccess()

	demonstrateLookup()
}

func demonstrateStringAccess() {
	fmt.Println("=== String Access ===")
	// Simple string get
	name := env.GetString("app.name")
	fmt.Printf("app.name: %q\n", name)

	// With default value (demonstrates fallback when key missing)
	missing := env.GetString("missing.key", "default_value")
	fmt.Printf("missing.key (default): %q\n", missing)

	// Nested path access (dot notation)
	dbHost := env.GetString("db.host", "localhost")
	fmt.Printf("db.host: %q\n", dbHost)
}

func demonstrateIntAccess() {
	fmt.Println("\n=== Integer Access ===")
	// Integer with default
	port := env.GetInt("app.port", 9090)
	fmt.Printf("app.port: %d\n", port)

	// Nested integer
	maxConn := env.GetInt("db.max_connections", 10)
	fmt.Printf("db.max_connections: %d\n", maxConn)

	// Missing key returns 0 or default
	missing := env.GetInt("nonexistent", 42)
	fmt.Printf("nonexistent (with default): %d\n", missing)
}

func demonstrateBoolAccess() {
	fmt.Println("\n=== Boolean Access ===")
	// Boolean values
	debug := env.GetBool("app.debug", false)
	fmt.Printf("app.debug: %v\n", debug)

	// Feature flags
	cacheEnabled := env.GetBool("cache.enabled", false)
	fmt.Printf("cache.enabled: %v\n", cacheEnabled)

	rateLimit := env.GetBool("features.rate_limit", true)
	fmt.Printf("features.rate_limit: %v\n", rateLimit)
}

func demonstrateDurationAccess() {
	fmt.Println("\n=== Duration Access ===")
	// Duration parsing
	timeout := env.GetDuration("db.timeout", 10*time.Second)
	fmt.Printf("db.timeout: %v\n", timeout)

	// Cache TTL
	ttl := env.GetDuration("cache.ttl", 5*time.Minute)
	fmt.Printf("cache.ttl: %v\n", ttl)

	// With default for missing
	missing := env.GetDuration("missing.duration", 1*time.Hour)
	fmt.Printf("missing.duration (default): %v\n", missing)
}

func demonstrateFloatAccess() {
	fmt.Println("\n=== Float Access ===")
	// Float64 values
	ratio := env.GetFloat64("cache.ratio", 0.5)
	fmt.Printf("cache.ratio: %v\n", ratio)

	// Missing key returns 0 or default
	missing := env.GetFloat64("nonexistent", 3.14)
	fmt.Printf("nonexistent (with default): %v\n", missing)
}

func demonstrateUintAccess() {
	fmt.Println("\n=== Unsigned Integer Access ===")
	// Uint64 values (useful for sizes, counters)
	maxSize := env.GetUint64("cache.max_size", 1024)
	fmt.Printf("cache.max_size: %d\n", maxSize)

	// Missing key returns 0 or default
	missing := env.GetUint64("nonexistent", 4096)
	fmt.Printf("nonexistent (with default): %d\n", missing)
}

func demonstrateSliceAccess() {
	fmt.Println("\n=== Slice Access ===")
	// Indexed access (arrays in JSON/YAML)
	host0 := env.GetString("cache.hosts.0")
	fmt.Printf("cache.hosts.0: %q\n", host0)

	// String slice from array (global mode)
	hosts := env.GetSlice[string]("cache.hosts")
	fmt.Printf("cache.hosts: %v\n", hosts)

	// Integer slice (default fallback when key not found)
	ports := env.GetSlice[int]("nonexistent_ports", []int{8080, 8081})
	fmt.Printf("nonexistent_ports (default): %v\n", ports)

	// Instance mode slice access using GetSliceFrom
	// GetSliceFrom reads indexed keys: TAGS_0, TAGS_1, TAGS_2, ...
	loader, err := env.New(env.DefaultConfig())
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()
	loader.Set("TAGS_0", "alpha")
	loader.Set("TAGS_1", "beta")
	loader.Set("TAGS_2", "stable")
	tags := env.GetSliceFrom[string](loader, "TAGS")
	fmt.Printf("TAGS (instance mode): %v\n", tags)
}

func demonstrateLookup() {
	fmt.Println("\n=== Lookup and Existence ===")
	// Check existence and get value
	if value, exists := env.Lookup("app.port"); exists {
		fmt.Printf("app.port exists: %v\n", value)
	}

	if _, exists := env.Lookup("db.password"); exists {
		fmt.Println("db.password: [HIDDEN]")
	}

	// Missing key
	if _, exists := env.Lookup("nonexistent.key"); !exists {
		fmt.Println("nonexistent.key does not exist")
	}
}
