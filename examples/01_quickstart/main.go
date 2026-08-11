// Package main demonstrates the simplest way to load and access environment
// variables using the global (package-level) API.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cybergodev/env"
)

func main() {
	// Load initializes the global loader and applies variables to os.Environ.
	// Call this once at startup; subsequent calls return ErrAlreadyInitialized.
	if err := env.Load("examples/data/config.env"); err != nil {
		log.Fatalf("Failed to load: %v", err)
	}

	// Typed access with optional defaults (zero-value when key is absent)
	fmt.Printf("APP_NAME:    %s\n", env.GetString("APP_NAME", "unknown"))
	fmt.Printf("APP_PORT:    %d\n", env.GetInt("APP_PORT", 9090))
	fmt.Printf("DEBUG:       %v\n", env.GetBool("DEBUG", false))
	fmt.Printf("DB_TIMEOUT:  %v\n", env.GetDuration("DB_TIMEOUT", 10*time.Second))

	// Existence check without exposing the value
	if _, exists := env.Lookup("DB_PASSWORD"); exists {
		fmt.Println("DB_PASSWORD: [HIDDEN]")
	}

	// Set a value at runtime (propagates to os.Environ via the global loader)
	if err := env.Set("RUNTIME_VAR", "set_at_runtime"); err != nil {
		log.Fatalf("Failed to set: %v", err)
	}
	fmt.Printf("RUNTIME_VAR: %s\n", env.GetString("RUNTIME_VAR"))
}
