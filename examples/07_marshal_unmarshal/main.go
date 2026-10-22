// Package main demonstrates Marshal/Unmarshal utilities for converting
// between environment variables, maps, structs, and multiple file formats.
package main

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateUnmarshal()

	demonstrateMarshal()

	demonstrateFormats()

	demonstrateRoundTrip()

	demonstrateConfiguredParsing()
}

// demonstrateUnmarshal parses formatted strings into maps and structs.
func demonstrateUnmarshal() {
	fmt.Println("=== Unmarshal ===")

	// .env string → map
	envMap, err := env.UnmarshalMap("APP_NAME=myapp\nAPP_PORT=8080")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  .env → map: %d keys\n", len(envMap))

	// JSON string → struct
	type Server struct {
		Host string `env:"SERVER_HOST"`
		Port int    `env:"SERVER_PORT"`
	}
	var srv Server
	if err := env.UnmarshalStruct(`{"server": {"host": "0.0.0.0", "port": 8080}}`,
		&srv, env.FormatJSON); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  JSON → struct: Host=%s Port=%d\n", srv.Host, srv.Port)

	// FormatAuto detects the format from the data itself (nested keys flatten).
	autoMap, err := env.UnmarshalMap(`{"auto_detect": "works"}`, env.FormatAuto)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  auto-detected: %v\n", autoMap)
}

// demonstrateMarshal converts maps and structs to .env strings.
func demonstrateMarshal() {
	fmt.Println("\n=== Marshal ===")

	// map → .env (sorted keys)
	envStr, err := env.Marshal(map[string]string{
		"APP_NAME": "myapp",
		"APP_PORT": "8080",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  map → .env:\n%s", envStr)

	// struct → .env
	type Config struct {
		Name string `env:"APP_NAME"`
		Port int    `env:"APP_PORT"`
	}
	cfgStr, err := env.Marshal(Config{Name: "myapp", Port: 8080})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  struct → .env:\n%s", cfgStr)
}

// demonstrateFormats shows multi-format output from the same map.
func demonstrateFormats() {
	fmt.Println("\n=== Multi-Format ===")
	data := map[string]string{
		"APP_NAME": "myapp",
		"APP_PORT": "8080",
		"DB_HOST":  "localhost",
	}

	jsonStr, _ := env.Marshal(data, env.FormatJSON)
	yamlStr, _ := env.Marshal(data, env.FormatYAML)

	fmt.Printf("  JSON:\n%s", jsonStr)
	fmt.Printf("  YAML:\n%s", yamlStr)
}

// demonstrateRoundTrip shows struct → map → struct round-trip fidelity.
func demonstrateRoundTrip() {
	fmt.Println("\n=== Round Trip ===")
	type Server struct {
		Host    string        `env:"SERVER_HOST"`
		Port    int           `env:"SERVER_PORT"`
		Timeout time.Duration `env:"SERVER_TIMEOUT"`
	}

	original := Server{Host: "localhost", Port: 8080, Timeout: 30 * time.Second}

	// struct → map
	envMap, err := env.MarshalStruct(original)
	if err != nil {
		log.Fatal(err)
	}

	// map → struct
	var restored Server
	if err := env.UnmarshalInto(envMap, &restored); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  Original:  %+v\n", original)
	fmt.Printf("  Restored:  %+v\n", restored)

	// Print sorted keys for visibility
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("  Map keys:  %v\n", keys)
}

// demonstrateConfiguredParsing applies a Config to one-shot parsing:
// the DuplicateKeyPolicy decides how duplicate keys in one document resolve,
// via the WithConfig variants of UnmarshalMap/UnmarshalStruct.
func demonstrateConfiguredParsing() {
	fmt.Println("\n=== Configured Parsing (DuplicateKeyPolicy) ===")

	const dupData = "APP_NAME=first\nAPP_NAME=second\n"

	// Default policy (DuplicateKeyInherit): .env follows OverwriteExisting,
	// which is false in DefaultConfig — the first occurrence wins.
	first, err := env.UnmarshalMap(dupData)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  inherit:    APP_NAME=%s\n", first["APP_NAME"])

	// DuplicateKeyLastWins: last occurrence wins regardless.
	cfg := env.DefaultConfig()
	cfg.ParsingConfig.DuplicateKeyPolicy = env.DuplicateKeyLastWins
	last, err := env.UnmarshalMapWithConfig(cfg, dupData)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  last-wins:  APP_NAME=%s\n", last["APP_NAME"])

	// DuplicateKeyError: any duplicate is rejected (matches ErrDuplicateKey).
	cfg.ParsingConfig.DuplicateKeyPolicy = env.DuplicateKeyError
	_, err = env.UnmarshalMapWithConfig(cfg, dupData)
	fmt.Printf("  error:      rejected=%v (errors.Is ErrDuplicateKey)\n",
		errors.Is(err, env.ErrDuplicateKey))

	// UnmarshalStructWithConfig: the same config-driven rules for structs.
	type Server struct {
		Host string `env:"SERVER_HOST"`
		Port int    `env:"SERVER_PORT"`
	}
	var srv Server
	err = env.UnmarshalStructWithConfig(env.DefaultConfig(),
		"server:\n  host: 0.0.0.0\n  port: 8443\n", &srv, env.FormatYAML)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  YAML → struct: Host=%s Port=%d\n", srv.Host, srv.Port)
}
