package env

import (
	"path/filepath"
	"strings"
)

// FileFormat represents the file format for environment configuration.
type FileFormat int

const (
	// FormatAuto automatically detects the format based on file extension.
	FormatAuto FileFormat = iota
	// FormatEnv represents the .env file format.
	FormatEnv
	// FormatJSON represents a JSON file format.
	FormatJSON
	// FormatYAML represents a YAML file format.
	FormatYAML
)

// DetectFormat detects the file format based on the file extension.
// Returns FormatEnv for ".env" files, FormatJSON for ".json" files,
// FormatYAML for ".yaml" and ".yml" files, and FormatAuto for unknown extensions.
//
// Example:
//
//	fmt.Println(env.DetectFormat("config.json"))  // json
//	fmt.Println(env.DetectFormat("settings.yaml")) // yaml
//	fmt.Println(env.DetectFormat(".env"))           // dotenv
func DetectFormat(filename string) FileFormat {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".env":
		return FormatEnv
	case ".json":
		return FormatJSON
	case ".yaml", ".yml":
		return FormatYAML
	default:
		return FormatAuto
	}
}

// String returns the string representation of the file format.
// Returns "auto", "dotenv", "json", "yaml", or "unknown" based on the format value.
func (f FileFormat) String() string {
	switch f {
	case FormatAuto:
		return "auto"
	case FormatEnv:
		return "dotenv"
	case FormatJSON:
		return "json"
	case FormatYAML:
		return "yaml"
	default:
		return "unknown"
	}
}
