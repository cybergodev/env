package env

import (
	"regexp"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Security Limits (re-exported from internal/limits)
// ============================================================================

// Default security limits for high-security configurations.
// These values are intentionally conservative to prevent various attacks.
const (
	// DefaultMaxFileSize is the maximum allowed file size (2 MB).
	DefaultMaxFileSize = internal.DefaultMaxFileSize

	// DefaultMaxLineLength is the maximum allowed line length.
	DefaultMaxLineLength = internal.DefaultMaxLineLength

	// DefaultMaxKeyLength is the maximum allowed key length.
	DefaultMaxKeyLength = internal.DefaultMaxKeyLength

	// DefaultMaxValueLength is the maximum allowed value length.
	DefaultMaxValueLength = internal.DefaultMaxValueLength

	// DefaultMaxVariables is the maximum number of variables per file.
	DefaultMaxVariables = internal.DefaultMaxVariables

	// DefaultMaxExpansionDepth is the maximum variable expansion depth.
	DefaultMaxExpansionDepth = internal.DefaultMaxExpansionDepth
)

// ============================================================================
// Default Patterns
// ============================================================================

// DefaultKeyPattern is the default pattern for valid keys.
// Set to nil to use fast byte-level validation (isValidDefaultKey) instead of regex.
// This provides ~10x performance improvement for key validation.
// The regex pattern ^[A-Za-z][A-Za-z0-9_]*$ is still available for reference:
//
//	var defaultKeyPatternRegex = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var DefaultKeyPattern *regexp.Regexp = nil

// defaultForbiddenKeys contains keys that could affect system behavior.
// These keys are forbidden by default to prevent:
//   - PATH injection attacks
//   - Library preloading attacks (LD_PRELOAD, LD_LIBRARY_PATH, DYLD_*)
//   - Shell escape attacks (SHELL, ENV, BASH_ENV, IFS)
//   - Language-specific injection (PYTHONPATH, PERL5OPT, RUBYLIB, NODE_PATH)
var defaultForbiddenKeys = map[string]bool{
	"PATH":                  true,
	"LD_PRELOAD":            true,
	"LD_LIBRARY_PATH":       true,
	"LD_DEBUG":              true,
	"LD_AUDIT":              true,
	"LD_PRELOAD_32":         true,
	"LD_PRELOAD_64":         true,
	"LD_LIBRARY_PATH_32":    true,
	"LD_LIBRARY_PATH_64":    true,
	"DYLD_INSERT_LIBRARIES": true,
	"DYLD_LIBRARY_PATH":     true,
	"IFS":                   true,
	"SHELL":                 true,
	"ENV":                   true,
	"BASH_ENV":              true,
	"PERL5OPT":              true,
	"PYTHONPATH":            true,
	"RUBYLIB":               true,
	"NODE_PATH":             true,
	// Windows-specific injection vectors
	"COMSPEC":               true,
	"PATHEXT":               true,
	"SYSTEMROOT":            true,
	"WINDIR":                true,
}

// defaultForbiddenKeysSlice is a pre-computed slice of defaultForbiddenKeys.
// This avoids map iteration on every factory creation.
var defaultForbiddenKeysSlice = func() []string {
	keys := make([]string, 0, len(defaultForbiddenKeys))
	for k := range defaultForbiddenKeys {
		keys = append(keys, k)
	}
	return keys
}()
