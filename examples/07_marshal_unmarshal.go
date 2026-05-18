//go:build examples

// Package main demonstrates Marshal/Unmarshal utilities
// for converting between environment variables and various formats.
package main

import (
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
}

func demonstrateUnmarshal() {
	fmt.Println("=== Unmarshal: String to Map ===")
	// Parse .env format string into map
	envString := `APP_NAME=myapp
APP_PORT=8080
DEBUG=true`

	envMap, err := env.UnmarshalMap(envString)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Parsed %d variables\n", len(envMap))

	// Parse JSON directly into struct
	jsonString := `{"server": {"host": "0.0.0.0", "port": 8080}}`

	type Config struct {
		Host string `env:"SERVER_HOST"`
		Port int    `env:"SERVER_PORT"`
	}

	var cfg Config
	if err := env.UnmarshalStruct(jsonString, &cfg, env.FormatJSON); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("JSON to struct: Host=%s, Port=%d\n", cfg.Host, cfg.Port)
}

func demonstrateMarshal() {
	fmt.Println("\n=== Marshal: Map/Struct to String ===")
	// Map to .env format (keys sorted)
	envMap := map[string]string{
		"APP_NAME": "myapp",
		"APP_PORT": "8080",
	}

	envString, err := env.Marshal(envMap)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Map to .env:\n%s", envString)

	// Struct to .env format
	type Config struct {
		Name string `env:"APP_NAME"`
		Port int    `env:"APP_PORT"`
	}

	config := Config{Name: "myapp", Port: 8080}
	configEnv, err := env.Marshal(config)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Struct to .env:\n%s", configEnv)
}

func demonstrateFormats() {
	fmt.Println("\n=== Multi-Format Output ===")
	envMap := map[string]string{
		"APP_NAME": "myapp",
		"APP_PORT": "8080",
		"DB_HOST":  "localhost",
	}

	// JSON output
	jsonString, err := env.Marshal(envMap, env.FormatJSON)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("JSON:\n%s\n", jsonString)

	// YAML output
	yamlString, err := env.Marshal(envMap, env.FormatYAML)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("YAML:\n%s", yamlString)
}

func demonstrateRoundTrip() {
	fmt.Println("\n=== Round Trip ===")
	type ServerConfig struct {
		Host    string        `env:"SERVER_HOST"`
		Port    int           `env:"SERVER_PORT"`
		Timeout time.Duration `env:"SERVER_TIMEOUT"`
	}

	// Struct -> Map
	original := ServerConfig{Host: "localhost", Port: 8080, Timeout: 30 * time.Second}
	envMap, err := env.MarshalStruct(original)
	if err != nil {
		log.Fatal(err)
	}

	// Map -> Struct
	var restored ServerConfig
	if err := env.UnmarshalInto(envMap, &restored); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Original: Host=%s, Port=%d\n", original.Host, original.Port)
	fmt.Printf("Restored: Host=%s, Port=%d\n", restored.Host, restored.Port)

	// Print sorted keys
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Print("Generated map: ")
	for _, k := range keys {
		fmt.Printf("%s=%s ", k, envMap[k])
	}
	fmt.Println()
}
