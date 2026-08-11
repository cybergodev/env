// Package main demonstrates mapping environment variables to Go structs
// using `env` tags, including nested structs, defaults, and marshaling.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cybergodev/env"
)

// AppConfig maps to flat env keys via the `env` tag.
type AppConfig struct {
	Name    string `env:"APP_NAME"`
	Port    int    `env:"APP_PORT"`
	Debug   bool   `env:"APP_DEBUG"`
	Version string `env:"APP_VERSION" envDefault:"1.0.0"`
}

// DatabaseConfig demonstrates envDefault tags for optional fields.
type DatabaseConfig struct {
	Host           string        `env:"DB_HOST"`
	Port           int           `env:"DB_PORT"`
	MaxConnections int           `env:"DB_MAX_CONNECTIONS" envDefault:"10"`
	EnableSSL      bool          `env:"DB_ENABLE_SSL" envDefault:"false"`
	Timeout        time.Duration `env:"DB_TIMEOUT"`
}

// FullConfig demonstrates nested struct support.
// YAML/JSON nested keys flatten to UPPER_CASE and map to each sub-struct's tags.
type FullConfig struct {
	App AppConfig
	DB  DatabaseConfig
}

func main() {
	// YAML flattens: app.name → APP_NAME, db.host → DB_HOST, etc.
	if err := env.Load("examples/data/config.yaml"); err != nil {
		log.Fatalf("Failed to load: %v", err)
	}

	// Simple unmarshal
	var app AppConfig
	if err := env.ParseInto(&app); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("App:  %+v\n", app)

	// Defaults: DB_ENABLE_SSL is not in the file → envDefault:"false" applies
	var db DatabaseConfig
	if err := env.ParseInto(&db); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("DB:   %+v\n", db)

	// Nested struct
	var full FullConfig
	if err := env.ParseInto(&full); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Nested: App.Name=%s  DB.Host=%s\n", full.App.Name, full.DB.Host)

	// Marshal a struct back to an env-key map
	envMap, err := env.MarshalStruct(DatabaseConfig{
		Host: "prod.db.example.com", Port: 5432,
		MaxConnections: 100, EnableSSL: true, Timeout: 30 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nMarshaled DB config: %v\n", envMap)
}
