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

// appSection and dbSection use RELATIVE keys: the enclosing struct's env tag
// supplies the prefix. This mirrors how YAML/JSON nested keys flatten
// (app.name → APP_NAME, db.host → DB_HOST).
//
// NOTE: do NOT reuse the absolute-tag AppConfig inside another struct — a
// nested field's tags are always prefixed, so it would look up APP_APP_NAME
// and silently match nothing.
type appSection struct {
	Name string `env:"NAME"`
	Port int    `env:"PORT"`
}

type dbSection struct {
	Host    string        `env:"HOST"`
	Port    int           `env:"PORT"`
	Timeout time.Duration `env:"TIMEOUT"`
}

// FullConfig demonstrates nested struct support: the outer field's env tag
// is the key prefix for every inner field (env:"APP" + env:"NAME" → APP_NAME).
type FullConfig struct {
	App appSection `env:"APP"`
	DB  dbSection  `env:"DB"`
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

	// Nested struct: env:"APP" + env:"NAME" resolves to the flattened
	// APP_NAME key produced by the YAML loader.
	var full FullConfig
	if err := env.ParseInto(&full); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Nested: App.Name=%s  DB.Host=%s\n", full.App.Name, full.DB.Host)

	// MarshalStruct emits the same flattened keys — nested round-trips work.
	nested, err := env.MarshalStruct(full)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Nested marshaled: %v\n", nested)

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
