// Package main demonstrates typed access methods for all supported types.
// JSON is loaded so that dot-notation paths (e.g. "db.host") can be shown
// alongside flat key access.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cybergodev/env"
)

func main() {
	if err := env.Load("examples/data/config.json"); err != nil {
		log.Fatalf("Failed to load: %v", err)
	}

	// --- String ---
	fmt.Println("=== String ===")
	fmt.Printf("  app.name:      %q\n", env.GetString("app.name"))
	fmt.Printf("  db.host:       %q\n", env.GetString("db.host", "localhost"))
	fmt.Printf("  missing (def): %q\n", env.GetString("missing.key", "fallback"))

	// --- Integer ---
	fmt.Println("\n=== Integer ===")
	fmt.Printf("  app.port:      %d\n", env.GetInt("app.port", 9090))
	fmt.Printf("  db.port:       %d\n", env.GetInt("db.port", 5432))

	// --- Boolean ---
	fmt.Println("\n=== Boolean ===")
	fmt.Printf("  app.debug:       %v\n", env.GetBool("app.debug", false))
	fmt.Printf("  cache.enabled:   %v\n", env.GetBool("cache.enabled", false))

	// --- Duration ---
	fmt.Println("\n=== Duration ===")
	fmt.Printf("  db.timeout:  %v\n", env.GetDuration("db.timeout", 10*time.Second))
	fmt.Printf("  cache.ttl:   %v\n", env.GetDuration("cache.ttl", 5*time.Minute))

	// --- Float / Uint ---
	fmt.Println("\n=== Float & Uint ===")
	fmt.Printf("  cache.ratio:    %f\n", env.GetFloat64("cache.ratio", 0.5))
	fmt.Printf("  cache.max_size: %d\n", env.GetUint64("cache.max_size", 1024))

	// --- Slice (from JSON array) ---
	fmt.Println("\n=== Slice ===")
	// cache.hosts is ["redis1:6379", "redis2:6379"] → flattened to CACHE_HOSTS_0, CACHE_HOSTS_1
	hosts := env.GetSlice[string]("cache.hosts")
	fmt.Printf("  cache.hosts:    %v\n", hosts)
	fmt.Printf("  cache.hosts.0:  %q\n", env.GetString("cache.hosts.0"))

	// Comma-separated values are supported too — the element type comes from
	// the type parameter (string, int, bool, ...).
	if err := env.Set("DB_REPLICAS", "10.0.0.1,10.0.0.2,10.0.0.3"); err != nil {
		log.Fatalf("Failed to set: %v", err)
	}
	fmt.Printf("  DB_REPLICAS:    %v\n", env.GetSlice[string]("DB_REPLICAS"))

	// --- Lookup ---
	fmt.Println("\n=== Lookup ===")
	if val, ok := env.Lookup("app.port"); ok {
		fmt.Printf("  app.port: exists=%v value=%s\n", ok, val)
	}
	if _, ok := env.Lookup("nonexistent"); !ok {
		fmt.Println("  nonexistent: not found")
	}

	// --- Slice in instance mode (GetSliceFrom) ---
	// GetSliceFrom is the Loader-based form of GetSlice — Go methods cannot
	// have type parameters, so the instance variant is a generic function.
	fmt.Println("\n=== Slice, instance mode ===")
	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.json"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	fmt.Printf("  cache.hosts:    %v\n", env.GetSliceFrom[string](loader, "cache.hosts"))

	// A scalar (non-indexed, non-comma) value resolves to a one-element slice.
	fmt.Printf("  cache.max_size: %v\n", env.GetSliceFrom[int](loader, "cache.max_size"))
}
