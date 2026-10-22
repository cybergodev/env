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
	// DefaultMaxFileSize is the maximum allowed file size (8 MB).
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
//
// It is a process-global knob rather than a Config setting: New reads it
// without locking, so assign it at program init, before concurrent use of
// this package — mutating it concurrently with New/Load is a data race (the
// same contract DefaultFileSystem documents).
var DefaultKeyPattern *regexp.Regexp = nil

// defaultForbiddenKeysSlice lists keys that could affect system behavior.
// These keys are forbidden by default to prevent:
//   - PATH injection attacks
//   - Library preloading attacks (LD_PRELOAD, LD_LIBRARY_PATH, DYLD_*)
//   - Shell escape attacks (SHELL, ENV, BASH_ENV, IFS)
//   - Language-specific injection (PYTHONPATH, PERL5OPT, RUBYLIB, NODE_PATH)
//   - Windows-specific injection vectors (COMSPEC, PATHEXT, SYSTEMROOT, WINDIR)
//   - Runtime/VM code-execution option vectors (SEC-003, CWE-74/94):
//     NODE_OPTIONS, JAVA_TOOL_OPTIONS, JDK_JAVA_OPTIONS, _JAVA_OPTIONS,
//     PYTHONSTARTUP, PYTHONINSPECT, PS4, RUBYOPT, PERL5LIB, RUSTFLAGS, etc.
//   - Git config/proxy injection (GIT_* — arbitrary command execution via
//     GIT_PROXY_COMMAND and arbitrary config via GIT_CONFIG_*).
//
// It is a plain slice literal rather than a value derived from a map so
// nothing beyond the list itself stays resident after init; a test
// (TestDefaultForbiddenKeysSlice) asserts there are no duplicates, which the
// previous map source guaranteed structurally.
var defaultForbiddenKeysSlice = []string{
	"PATH",
	"LD_PRELOAD",
	"LD_LIBRARY_PATH",
	"LD_DEBUG",
	"LD_AUDIT",
	"LD_PRELOAD_32",
	"LD_PRELOAD_64",
	"LD_LIBRARY_PATH_32",
	"LD_LIBRARY_PATH_64",
	"DYLD_INSERT_LIBRARIES",
	"DYLD_LIBRARY_PATH",
	"IFS",
	"SHELL",
	"ENV",
	"BASH_ENV",
	"PERL5OPT",
	"PERL5LIB",
	"PYTHONPATH",
	"PYTHONSTARTUP",
	"PYTHONINSPECT",
	"RUBYLIB",
	"RUBYOPT",
	"GEM_PATH",
	"GEM_HOME",
	"NODE_PATH",
	"NODE_OPTIONS",
	"RUSTFLAGS",
	"JAVA_TOOL_OPTIONS",
	"JDK_JAVA_OPTIONS",
	"_JAVA_OPTIONS",
	"CLASSPATH",
	"PS4",
	"SHELLOPTS",
	"BASHOPTS",
	"GIT_CONFIG_COUNT",
	"GIT_PROXY_COMMAND",
	// Windows-specific injection vectors
	"COMSPEC",
	"PATHEXT",
	"SYSTEMROOT",
	"WINDIR",
}
