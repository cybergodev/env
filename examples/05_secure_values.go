//go:build examples

// Package main demonstrates SecureValue for handling sensitive data.
// SecureValue automatically zeros memory when closed or garbage collected.
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

func demonstrateBasics() {
	fmt.Println("=== SecureValue Basics ===")
	// Create a SecureValue from a sensitive string
	password := env.NewSecureValue("super_secret_password_123")

	// Reveal the plaintext value (use only when the actual value is needed)
	fmt.Printf("Value: %s\n", password.Reveal())

	// String() returns masked representation (safe for %s formatting)
	fmt.Printf("String(): %s\n", password.String())

	// Masked() returns a safe representation for logging
	fmt.Printf("Masked: %s\n", password.Masked())

	// Get length without exposing the value
	fmt.Printf("Length: %d bytes\n", password.Length())

	// Clean up when done (zeros the memory)
	password.Close()
}

func demonstrateFromLoader() {
	fmt.Println("\n=== SecureValue from Loader ===")
	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// Get sensitive values as SecureValue
	securePassword := loader.GetSecure("DB_PASSWORD")
	if securePassword != nil {
		fmt.Printf("DB_PASSWORD: %s\n", securePassword.Masked())
		securePassword.Close()
	}

	secureAPIKey := loader.GetSecure("API_KEY")
	if secureAPIKey != nil {
		fmt.Printf("API_KEY: %s\n", secureAPIKey.Masked())
		secureAPIKey.Close()
	}
}

func demonstrateLifecycle() {
	fmt.Println("\n=== Lifecycle: Close vs Release ===")
	// Close() zeros memory — subsequent Reveal() returns empty string
	secret := env.NewSecureValue("temporary_secret")
	fmt.Printf("Before close: %s\n", secret.Reveal())
	secret.Close()
	fmt.Printf("After close (zeroed): %q\n", secret.Reveal())

	// Release() zeros memory AND returns to pool (more efficient for frequent use)
	secret2 := env.NewSecureValue("another_secret")
	fmt.Printf("Before release: %s\n", secret2.Reveal())

	// Release returns the object to the pool for reuse
	secret2.Release()
	fmt.Printf("After release (zeroed): %q\n", secret2.Reveal())

	// Bytes() returns a copy that must be cleared by the caller
	secret3 := env.NewSecureValue("byte_example")
	bytes := secret3.Bytes()
	fmt.Printf("\nBytes length: %d\n", len(bytes))
	env.ClearBytes(bytes) // Clear external copies when done
	secret3.Release()
}

func demonstrateMemoryLock() {
	fmt.Println("\n=== Memory Lock Configuration ===")
	// Memory locking prevents sensitive data from being swapped to disk.
	// On Linux: uses mlock/mlockall. On Windows: uses VirtualLock.
	if env.IsMemoryLockSupported() {
		fmt.Printf("Memory lock supported on this platform\n")
		fmt.Printf("Enabled: %v, Strict: %v\n",
			env.IsMemoryLockEnabled(), env.IsMemoryLockStrict())

		// Enable strict mode — SecureValue creation fails if mlock fails
		env.SetMemoryLockStrict(true)

		strict, err := env.NewSecureValueStrict("needs_mlock")
		if err != nil {
			fmt.Printf("Strict mlock failed: %v\n", err)
		} else {
			fmt.Printf("Strict mlock locked: %v\n", strict.IsMemoryLocked())
			strict.Close()
		}

		// Reset to default (non-strict)
		env.SetMemoryLockStrict(false)
	} else {
		fmt.Println("Memory lock not supported on this platform")
	}
}
