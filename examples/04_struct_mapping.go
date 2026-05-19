//go:build examples

// Package main demonstrates struct mapping with env tags.
// Use this for type-safe configuration loading into structs.
package main

import (
	"fmt"
	"log"

	"github.com/cybergodev/env"
)

// AppConfig represents the application configuration structure.
// Use `env` tags to map environment variables to struct fields.
type AppConfig struct {
	Name    string `env:"APP_NAME"`
	Port    int    `env:"APP_PORT"`
	Debug   bool   `env:"APP_DEBUG"`
	Version string `env:"APP_VERSION" envDefault:"1.0.0"`
}

// DatabaseConfig represents database connection settings.
type DatabaseConfig struct {
	Host           string `env:"DB_HOST"`
	Port           int    `env:"DB_PORT"`
	Name           string `env:"DB_NAME"`
	User           string `env:"DB_USER"`
	Password       string `env:"DB_PASSWORD"`
	MaxConnections int    `env:"DB_MAX_CONNECTIONS" envDefault:"10"`
	EnableSSL      bool   `env:"DB_ENABLE_SSL" envDefault:"false"`
}

// FullConfig demonstrates nested struct support.
type FullConfig struct {
	App      AppConfig
	Database DatabaseConfig
}

func main() {
	// Initialize environment file
	if err := env.Load("examples/data/config.yaml"); err != nil {
		log.Fatalf("Failed to load: %v", err)
	}

	demonstrateSimpleUnmarshal()

	demonstrateStructWithDefaults()

	demonstrateNestedStruct()

	demonstrateStructMarshal()
}

func demonstrateSimpleUnmarshal() {
	fmt.Println("=== Simple Struct Unmarshal ===")
	var cfg AppConfig

	// Automatically populate struct from loaded environment variables
	// based on `env` tags in the struct definition
	if err := env.ParseInto(&cfg); err != nil {
		log.Fatalf("Failed to parse: %v", err)
	}

	fmt.Printf("Name: %s\n", cfg.Name)
	fmt.Printf("Port: %d\n", cfg.Port)
	fmt.Printf("Debug: %v\n", cfg.Debug)
}

func demonstrateStructWithDefaults() {
	fmt.Println("\n=== Struct with Defaults ===")
	var cfg DatabaseConfig

	// Automatically populate with defaults from envDefault tags
	// DB_ENABLE_SSL uses default value from tag since not set in env
	if err := env.ParseInto(&cfg); err != nil {
		log.Fatalf("Failed to parse: %v", err)
	}

	fmt.Printf("Host: %s\n", cfg.Host)
	fmt.Printf("Port: %d\n", cfg.Port)
	fmt.Printf("MaxConnections: %d\n", cfg.MaxConnections)
	fmt.Printf("EnableSSL (default): %v\n", cfg.EnableSSL)
}

func demonstrateNestedStruct() {
	fmt.Println("\n=== Nested Struct ===")
	// Nested structs are populated from loaded environment variables.
	// The YAML/JSON parser flattens nested keys to UPPER_CASE (e.g. app.name → APP_NAME)
	// which then maps to the env tags in each nested struct field.
	var cfg FullConfig
	if err := env.ParseInto(&cfg); err != nil {
		log.Fatalf("Failed to parse: %v", err)
	}

	fmt.Printf("App.Name: %s\n", cfg.App.Name)
	fmt.Printf("App.Port: %d\n", cfg.App.Port)
	fmt.Printf("Database.Host: %s\n", cfg.Database.Host)
}

func demonstrateStructMarshal() {
	fmt.Println("\n=== Struct to Env (Marshal) ===")
	// Convert struct back to environment map
	cfg := DatabaseConfig{
		Host:           "production.db.example.com",
		Port:           5432,
		Name:           "proddb",
		User:           "admin",
		Password:       "secret",
		MaxConnections: 100,
		EnableSSL:      true,
	}

	envMap, err := env.MarshalStruct(cfg)
	if err != nil {
		log.Fatalf("Failed to marshal: %v", err)
	}

	fmt.Println("Marshaled environment variables:")
	for k, v := range envMap {
		fmt.Printf("  %s: %s\n", k, v)
	}
}
