// Package main demonstrates SecureValue for handling sensitive data.
// SecureValue zeros its backing memory on Close/Release/GC, and optionally
// locks pages with mlock to prevent swapping to disk.
package main

import (
	"fmt"
	"log"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateBasics()

	demonstrateFromLoader()

	demonstrateLifecycle()

	demonstrateMemoryLock()
}

// demonstrateBasics shows the core SecureValue operations.
func demonstrateBasics() {
	fmt.Println("=== SecureValue Basics ===")
	sv := env.NewSecureValue("super_secret_password_123")

	fmt.Printf("  Reveal:  %s\n", sv.Reveal()) // plaintext — use carefully
	fmt.Printf("  String:  %s\n", sv.String()) // masked (fmt.Stringer)
	fmt.Printf("  Masked:  %s\n", sv.Masked()) // safe for logging
	fmt.Printf("  Length:  %d bytes\n", sv.Length())

	sv.Close()
	fmt.Printf("  After Close: %q\n", sv.Reveal()) // empty — memory zeroed
}

// demonstrateFromLoader shows reading a secret via the Loader.
func demonstrateFromLoader() {
	fmt.Println("\n=== From Loader ===")
	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// GetSecure returns a defensive copy; caller must Close/Release it.
	if sv := loader.GetSecure("DB_PASSWORD"); sv != nil {
		fmt.Printf("  DB_PASSWORD: %s\n", sv.Masked())
		sv.Release()
	}
	if sv := loader.GetSecure("API_KEY"); sv != nil {
		fmt.Printf("  API_KEY:     %s\n", sv.Masked())
		sv.Release()
	}
}

// demonstrateLifecycle compares Close (zero only) vs Release (zero + pool).
func demonstrateLifecycle() {
	fmt.Println("\n=== Close vs Release ===")
	// Close zeros memory but does not return to the pool.
	a := env.NewSecureValue("secret_a")
	fmt.Printf("  a before close:  %s\n", a.Reveal())
	a.Close()
	fmt.Printf("  a after close:   %q\n", a.Reveal())

	// Release zeros memory AND returns the object to the pool for reuse.
	// Prefer Release in hot paths; use Close when you want to guarantee no reuse.
	b := env.NewSecureValue("secret_b")
	fmt.Printf("  b before release: %s\n", b.Reveal())
	b.Release()
	fmt.Printf("  b after release:  %q\n", b.Reveal())

	// Bytes returns a caller-owned copy — clear it when done.
	c := env.NewSecureValue("byte_secret")
	raw := c.Bytes()
	fmt.Printf("  c.Bytes(): %d bytes\n", len(raw))
	env.ClearBytes(raw)
	c.Release()
}

// demonstrateMemoryLock shows mlock integration (platform-dependent).
func demonstrateMemoryLock() {
	fmt.Println("\n=== Memory Lock ===")
	if !env.IsMemoryLockSupported() {
		fmt.Println("  Not supported on this platform")
		return
	}

	fmt.Printf("  Enabled: %v  Strict: %v\n",
		env.IsMemoryLockEnabled(), env.IsMemoryLockStrict())

	// Enable strict mode so lock failures become observable.
	env.SetMemoryLockEnabled(true)
	env.SetMemoryLockStrict(true)

	sv, err := env.NewSecureValueStrict("needs_mlock")
	if err != nil {
		fmt.Printf("  Lock failed (expected without privileges): %v\n", err)
	} else {
		fmt.Printf("  Locked: %v\n", sv.IsMemoryLocked())
		sv.Close()
	}

	// Restore defaults
	env.SetMemoryLockStrict(false)
	env.SetMemoryLockEnabled(false)
}
