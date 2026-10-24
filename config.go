package env

import (
	"regexp"
	"strconv"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Configuration Types (Nested Structures)
// ============================================================================

// FileConfig controls file loading behavior.
type FileConfig struct {
	// Filenames lists the files to load. Defaults to [".env"].
	Filenames []string
	// FailOnMissingFile causes LoadFiles to return an error when a file does not exist.
	FailOnMissingFile bool
	// OverwriteExisting allows overwriting environment variables that are already set.
	OverwriteExisting bool
	// AutoApply calls Apply automatically after loading files in New.
	AutoApply bool
}

// ValidationConfig controls key and value validation.
type ValidationConfig struct {
	// RequiredKeys lists keys that must be present after loading. Validate returns ErrMissingRequired if any are missing.
	RequiredKeys []string
	// AllowedKeys restricts which keys are permitted. Empty means all keys are allowed.
	AllowedKeys []string
	// ForbiddenKeys lists keys that are always rejected. Set returns ErrForbiddenKey if any are present.
	ForbiddenKeys []string
	// KeyPattern is a custom regexp for validating key names. Nil uses the
	// fast built-in check (DefaultKeyPattern). The pattern may restrict keys
	// to any namespace or shape (e.g. ^MYAPP_[A-Z0-9_]*$) — it does not have
	// to match any particular sample key. Validate rejects only patterns
	// that would accept unsafe keys: ones matching the empty string, keys
	// starting with a digit, or keys containing '=', ':', newline or control
	// characters (the .env emitter cannot round-trip those, see
	// validateKeyPattern).
	KeyPattern *regexp.Regexp
	// ValidateValues enables the validator's value checks on Set and
	// structured (JSON/YAML) loads. With the built-in validator it no longer
	// disables anything: the always-on baseline (see ValidateValueBasic)
	// covers every check ValidateValue performs — maximum value length,
	// control-character rejection, and the UTF-8 check when ValidateUTF8 is
	// set — keeping those paths at parity with .env parsing, which has never
	// had a switch for value validation. The flag remains effective for
	// custom validators without the baseline capability, which skip value
	// validation entirely when it is false.
	ValidateValues bool
	// ValidateUTF8 enables validation that all values are valid UTF-8.
	// Honored on every write path (file loads and Set), independently of
	// ValidateValues.
	ValidateUTF8 bool
}

// LimitsConfig controls size and count limits for parsing.
type LimitsConfig struct {
	// MaxFileSize is the maximum allowed file size in bytes.
	MaxFileSize int64
	// MaxVariables is the maximum number of variables per file. Timing note:
	// the .env parser enforces this mid-parse (it fails on the first
	// variable beyond the limit), while the JSON/YAML parsers check the
	// flattened key count after the document is parsed and flattened — for
	// those formats the fail-fast memory bounds during parsing are the
	// YAML token cap and the JSON structural-node cap (see
	// internal.HardMaxYAMLTokens / HardMaxJSONNodes), not this limit.
	MaxVariables int
	// MaxLineLength is the maximum allowed line length in bytes. Enforced
	// for ALL formats, including JSON and YAML. A minified (single-line)
	// JSON document is one line for this purpose: it must fit within this
	// limit, whose hard cap is 64KB (HardMaxLineLength) — a larger minified
	// document cannot be loaded by raising the config alone; pretty-print
	// it instead. Files whose long lines are legitimate (cookie exports,
	// JWTs) fit the 8KB default this limit was sized for.
	MaxLineLength int
	// MaxKeyLength is the maximum allowed key length in bytes.
	MaxKeyLength int
	// MaxValueLength is the maximum allowed value length in bytes.
	MaxValueLength int
	// MaxExpansionDepth limits recursive variable expansion depth.
	MaxExpansionDepth int
}

// JSONConfig controls JSON parsing behavior.
type JSONConfig struct {
	// JSONNullAsEmpty converts JSON null values to empty strings.
	JSONNullAsEmpty bool
	// JSONNumberAsString renders JSON numbers as canonical decimal strings
	// ("8080", "0.5"). When false, numbers still load but render with Go's
	// default float formatting (fmt %v — 1e21 becomes "1e+21"). Numbers are
	// never rejected.
	JSONNumberAsString bool
	// JSONBoolAsString is retained for API symmetry with YAMLBoolAsString but
	// has never affected output: a decoded JSON bool carries no raw source
	// text to preserve, so JSON booleans always render the canonical
	// "true"/"false" (see internal/flatten_keys.go flattenDecoded's bool case).
	// It is accepted and ignored for backward compatibility and must not be
	// relied on to gate JSON bool handling.
	//
	// Deprecated: JSON bools are always rendered canonically regardless of this
	// flag. Use YAMLBoolAsString to govern YAML boolean coercion instead.
	JSONBoolAsString bool // Deprecated: no effect on JSON output
	// JSONMaxDepth limits nesting depth for JSON objects. Must be between 1 and 100.
	JSONMaxDepth int
}

// YAMLConfig controls YAML parsing behavior.
type YAMLConfig struct {
	// YAMLNullAsEmpty converts YAML null/~ values to empty strings.
	YAMLNullAsEmpty bool
	// YAMLNumberAsString canonicalizes number-looking scalar values ("007"
	// becomes "7"). When false, scalars are kept verbatim as written.
	// Values are never rejected.
	YAMLNumberAsString bool
	// YAMLBoolAsString converts YAML booleans to strings instead of rejecting them.
	YAMLBoolAsString bool
	// YAMLMaxDepth limits nesting depth for YAML mappings. Must be between 1 and 100.
	YAMLMaxDepth int
}

// ExpansionScope controls which variables are visible to ${VAR} and $VAR
// expansion in values.
type ExpansionScope int

const (
	// ExpansionFileThenProcess resolves references against the variables in
	// the parsed file first, then falls back to the process environment
	// (os.LookupEnv). This matches traditional dotenv semantics, but lets an
	// untrusted configuration file capture unrelated process secrets (cloud
	// credentials, tokens) into values that are later logged or persisted
	// (SEC-03). It is now opt-in: the secure default is ExpansionFileOnly.
	ExpansionFileThenProcess ExpansionScope = iota
	// ExpansionFileOnly restricts expansion to variables defined in the
	// parsed file(s) alone. References to variables that exist only in the
	// process environment expand to the empty string. This is the DEFAULT:
	// it prevents a less-trusted configuration file from reading unrelated
	// process secrets. Use ExpansionFileThenProcess only when the file source
	// is fully trusted.
	ExpansionFileOnly
)

// ParsingConfig controls general parsing behavior.
type ParsingConfig struct {
	// AllowExportPrefix allows the "export KEY=value" shell syntax in .env files.
	AllowExportPrefix bool
	// AllowYamlSyntax enables YAML-style values (lists, maps) in .env files.
	// It also enables ':' as a key/value separator: with the flag off (the
	// default) only '=' separates, so prose lines such as
	// "See: https://example.com" are dropped (and audited) instead of
	// silently becoming variables.
	AllowYamlSyntax bool
	// ExpandVariables enables ${VAR} and $VAR expansion in .env values.
	// JSON and YAML documents are never expanded; their values are taken
	// literally.
	ExpandVariables bool
	// ExpansionScope restricts which variables ${VAR} references can resolve
	// from. The default (ExpansionFileOnly) resolves file-local variables only
	// and blocks the process-environment fallback, so an untrusted
	// configuration file cannot read unrelated process secrets (SEC-03).
	// Set to ExpansionFileThenProcess to restore dotenv-style process fallback
	// for files you trust.
	ExpansionScope ExpansionScope
	// DuplicateKeyPolicy resolves duplicate keys WITHIN one source document
	// (see DuplicateKeyPolicy for the per-format semantics of the default).
	// It does not govern cross-file merging (that is OverwriteExisting) nor
	// the always-fatal flat-key collision check in YAML/JSON flattening.
	DuplicateKeyPolicy DuplicateKeyPolicy
}

// ComponentConfig holds custom component implementations and advanced options.
//
// Concurrency contract: a custom component is shared by every Loader and
// parser built from the Config and is invoked from concurrent goroutines
// (concurrent Set/Get on one Loader, or several Loaders built from one
// Config), so it must be safe for concurrent use. The built-in
// implementations are.
//
// Panic contract (SEC-003): custom components are wrapped for panic
// containment — a panic inside CustomValidator, CustomExpander, CustomAuditor,
// FileSystem (and the File it returns), or a parser from RegisterParser
// surfaces as an error from the public API that invoked it instead of
// crashing the process. The same applies to struct-field UnmarshalText /
// MarshalText / MarshalJSON callbacks and the Unmarshaler interface during
// ParseInto/UnmarshalInto/Marshal.
type ComponentConfig struct {
	// CustomValidator replaces the built-in key/value validator. Nil uses the default.
	// Must be safe for concurrent use.
	CustomValidator Validator
	// CustomExpander replaces the built-in variable expander. Nil uses the default.
	// Must be safe for concurrent use.
	CustomExpander VariableExpander
	// CustomAuditor replaces the built-in audit logger. Nil uses the default.
	// Must be safe for concurrent use.
	CustomAuditor AuditLogger
	// FileSystem overrides file system operations. Nil uses OSFileSystem. Useful for testing.
	// Panic containment applies (see the ComponentConfig doc); LookupEnv has no
	// error channel, so a panic there converts to ("", false) — treated as
	// "not set".
	FileSystem FileSystem
	// AuditHandler receives audit events when AuditEnabled is true.
	// Must be safe for concurrent use: Log may be invoked from concurrent
	// goroutines (Close is serialized against Log by the built-in auditor).
	// Reentrancy caveat: Log/Close run while the Loader may hold its lock —
	// a callback that calls back into the SAME Loader's mutating methods
	// (Set/Delete/Close/LoadFiles) self-deadlocks on the non-reentrant mutex.
	AuditHandler AuditHandler
	// AuditEnabled activates audit logging for all loader operations.
	AuditEnabled bool
	// AuditFailClosed makes lost audit events fail the operations that
	// emitted them: with auditing enabled, a handler error, handler panic,
	// or event dropped on a full channel surfaces as an error (matching
	// ErrAuditFailed) from LoadFiles, Apply, Set and Delete instead of being
	// silently discarded. The built-in auditor latches the first failure;
	// a CustomAuditor participates by implementing TakeFailure() error.
	// Without this flag auditing stays fail-open: operation results never
	// depend on the audit pipeline.
	AuditFailClosed bool
	// Prefix restricts processing to environment variables that start with this prefix.
	// Matching is ASCII case-insensitive: Prefix "MyApp_" admits MYAPP_X and
	// myapp_x as well.
	Prefix string
}

// ============================================================================
// Configuration
// ============================================================================

// Config holds all configuration options for the Loader.
//
// Configuration is organized into nested structures for better organization
// while maintaining backward compatibility through Go's struct embedding.
// You can access fields either way:
//
//	// Old way (still works via field promotion):
//	cfg.Filenames = []string{".env"}
//	cfg.MaxFileSize = 1024
//
//	// New way (recommended for clarity):
//	cfg.FileConfig.Filenames = []string{".env"}
//	cfg.LimitsConfig.MaxFileSize = 1024
//
// For sensible defaults, use DefaultConfig():
//
//	cfg := env.DefaultConfig()
//	cfg.FileConfig.Filenames = []string{".env"}
type Config struct {
	// FileConfig controls file loading behavior.
	FileConfig
	// ValidationConfig controls key and value validation rules.
	ValidationConfig
	// LimitsConfig controls size and count limits for parsing.
	LimitsConfig
	// JSONConfig controls JSON file parsing options.
	JSONConfig
	// YAMLConfig controls YAML file parsing options.
	YAMLConfig
	// ParsingConfig controls general parsing behavior for .env files.
	ParsingConfig
	// ComponentConfig holds custom components and advanced options.
	ComponentConfig
}

// Validate validates the configuration and returns an error if invalid.
// This method performs all configuration validation by delegating to focused sub-methods.
func (c *Config) Validate() error {
	if err := c.validateLimits(); err != nil {
		return err
	}
	if err := c.validateFormatConfig(); err != nil {
		return err
	}
	if err := c.validateAdvancedOptions(); err != nil {
		return err
	}
	return nil
}

// validateLimits validates size and count limits.
func (c *Config) validateLimits() error {
	return validateConfigLimits(
		c.MaxFileSize,
		c.MaxLineLength,
		c.MaxKeyLength,
		c.MaxValueLength,
		c.MaxVariables,
		c.MaxExpansionDepth,
	)
}

// validateFormatConfig validates JSON and YAML format configuration.
func (c *Config) validateFormatConfig() error {
	if c.JSONMaxDepth <= 0 || c.JSONMaxDepth > 100 {
		return newValidationError("JSONMaxDepth", "", "range", "must be between 1 and 100")
	}
	if c.YAMLMaxDepth <= 0 || c.YAMLMaxDepth > 100 {
		return newValidationError("YAMLMaxDepth", "", "range", "must be between 1 and 100")
	}
	return nil
}

// validateAdvancedOptions validates advanced options like custom key patterns.
func (c *Config) validateAdvancedOptions() error {
	if c.ExpansionScope != ExpansionFileThenProcess && c.ExpansionScope != ExpansionFileOnly {
		return newValidationError("ExpansionScope", strconv.Itoa(int(c.ExpansionScope)), "enum",
			"must be ExpansionFileThenProcess or ExpansionFileOnly")
	}
	switch c.DuplicateKeyPolicy {
	case DuplicateKeyInherit, DuplicateKeyFirstWins, DuplicateKeyLastWins, DuplicateKeyError:
		// ok
	default:
		return newValidationError("DuplicateKeyPolicy", strconv.Itoa(int(c.DuplicateKeyPolicy)), "enum",
			"must be DuplicateKeyInherit, DuplicateKeyFirstWins, DuplicateKeyLastWins, or DuplicateKeyError")
	}
	if c.KeyPattern != nil {
		return validateKeyPattern(c.KeyPattern)
	}
	return nil
}

// validateKeyPattern validates a custom key pattern.
func validateKeyPattern(pattern *regexp.Regexp) error {
	// The pattern may target any key namespace — it is NOT required to match
	// any particular sample key: a pattern that matches nothing makes every
	// key fail validation (self-inflicted, no security consequence), so
	// probing a magic key only rejected legitimate namespace patterns
	// (^MYAPP_[A-Z0-9_]*$). The predicate probes below carry the actual
	// security requirements.

	// Test that the pattern does not match empty strings
	if pattern.MatchString("") {
		return newValidationError("KeyPattern", pattern.String(), "reject_empty",
			"key pattern must not match empty strings")
	}

	// Test that the pattern does not match keys starting with numbers
	// (standard env var convention: must start with letter)
	if pattern.MatchString("123_INVALID") {
		return newValidationError("KeyPattern", pattern.String(), "reject_numeric_start",
			"key pattern must not match keys starting with numbers")
	}

	// SECURITY (SEC-06): a pattern that also matches keys containing
	// separators or control characters lets such keys pass Set(). The .env
	// emitter cannot represent them, previously producing output that
	// re-parses as different (attacker-chosen) lines — round-trip injection.
	// Reject patterns that match any of these probes.
	for _, probe := range []string{"A=B", "A:B", "A\nB", "A\rB", "A\x00B"} {
		if pattern.MatchString(probe) {
			return newValidationError("KeyPattern", pattern.String(), "reject_unsafe_chars",
				"key pattern must not match keys containing '=', ':', newline, or control characters")
		}
	}

	return nil
}

// IsZero returns true if the Config appears to be uninitialized (all fields zero).
// This is useful to determine if DefaultConfig() should be applied.
//
// Note: A partially-initialized Config may not be detected as zero.
// Always start from DefaultConfig() for custom configurations:
//
//	cfg := env.DefaultConfig()
//	cfg.Filenames = []string{".env.production"}
func (c *Config) IsZero() bool {
	// Check numeric limits (non-zero defaults in DefaultConfig)
	if c.MaxFileSize != 0 || c.MaxVariables != 0 ||
		c.MaxLineLength != 0 || c.MaxKeyLength != 0 ||
		c.MaxValueLength != 0 || c.MaxExpansionDepth != 0 ||
		c.JSONMaxDepth != 0 || c.YAMLMaxDepth != 0 {
		return false
	}

	// Check boolean fields (any non-zero value means initialized)
	if c.ValidateValues || c.ValidateUTF8 || c.JSONNullAsEmpty ||
		c.JSONNumberAsString || c.JSONBoolAsString ||
		c.YAMLNullAsEmpty || c.YAMLNumberAsString ||
		c.YAMLBoolAsString || c.AllowExportPrefix || c.ExpandVariables ||
		c.OverwriteExisting || c.AutoApply || c.FailOnMissingFile ||
		c.AllowYamlSyntax || c.AuditEnabled || c.AuditFailClosed {
		return false
	}

	// ExpansionScope is an enum, not a bool: any value other than the zero
	// value (ExpansionFileThenProcess) means initialized. The zero value is
	// the legacy process-fallback mode, so a Config that does not explicitly
	// opt into it must be treated as initialized rather than silently
	// replaced by DefaultConfig() (which now sets ExpansionFileOnly).
	if c.ExpansionScope != ExpansionFileThenProcess {
		return false
	}

	// DuplicateKeyPolicy is an enum too (same bug class as ExpansionScope):
	// a non-Inherit policy must keep the Config from being treated as zero
	// and replaced by DefaultConfig() in New().
	if c.DuplicateKeyPolicy != DuplicateKeyInherit {
		return false
	}

	// Check pointer/interface fields (non-nil means initialized)
	if c.KeyPattern != nil || c.FileSystem != nil ||
		c.CustomValidator != nil || c.CustomExpander != nil ||
		c.CustomAuditor != nil || c.AuditHandler != nil {
		return false
	}

	// Check string fields (non-empty means partially initialized)
	if c.Prefix != "" {
		return false
	}

	// Check slices (non-nil means partially initialized)
	if c.Filenames != nil || c.RequiredKeys != nil ||
		c.AllowedKeys != nil || c.ForbiddenKeys != nil {
		return false
	}

	return true
}

// ============================================================================
// Configuration Factories
// ============================================================================

// defaultStructuredMaxDepth bounds JSON/YAML document nesting. Real-world
// structured configs (OpenAPI/JSON-Schema-style documents) commonly nest
// 10-20 levels; 16 accommodates them while still bounding recursion well
// below the validated maximum (100). It backs DefaultConfig
// (JSONMaxDepth/YAMLMaxDepth), UnmarshalMap's structured paths, and the
// structured parsers' defensive fallback when constructed with a non-positive
// depth.
const defaultStructuredMaxDepth = 16

// DefaultConfig returns a Config with secure default values.
// These defaults are suitable for high-security environments.
func DefaultConfig() Config {
	return Config{
		FileConfig: FileConfig{
			Filenames:         []string{".env"},
			FailOnMissingFile: false,
			OverwriteExisting: false,
			AutoApply:         false,
		},
		ValidationConfig: ValidationConfig{
			RequiredKeys:   nil,
			AllowedKeys:    nil,
			ForbiddenKeys:  nil,
			KeyPattern:     DefaultKeyPattern,
			ValidateValues: true,
		},
		LimitsConfig: LimitsConfig{
			MaxFileSize:       DefaultMaxFileSize,
			MaxVariables:      DefaultMaxVariables,
			MaxLineLength:     DefaultMaxLineLength,
			MaxKeyLength:      DefaultMaxKeyLength,
			MaxValueLength:    DefaultMaxValueLength,
			MaxExpansionDepth: DefaultMaxExpansionDepth,
		},
		JSONConfig: JSONConfig{
			JSONNullAsEmpty:    true,
			JSONNumberAsString: true,
			// JSONBoolAsString is deliberately left false: it has no effect on
			// JSON output (see its field doc), and a false default makes the
			// field's no-op nature obvious rather than implying it does work.
			JSONBoolAsString: false,
			JSONMaxDepth:     defaultStructuredMaxDepth,
		},
		YAMLConfig: YAMLConfig{
			YAMLNullAsEmpty:    true,
			YAMLNumberAsString: true,
			YAMLBoolAsString:   true,
			YAMLMaxDepth:       defaultStructuredMaxDepth,
		},
		ParsingConfig: ParsingConfig{
			AllowExportPrefix: true,
			AllowYamlSyntax:   false,
			ExpandVariables:   true,
			// SEC-003: default to ExpansionFileOnly so an untrusted config file
			// cannot capture unrelated process secrets. The legacy process
			// fallback (ExpansionFileThenProcess) is opt-in for trusted files.
			ExpansionScope: ExpansionFileOnly,
			// DuplicateKeyPolicy zero value (DuplicateKeyInherit) keeps each
			// format's historical duplicate-key behavior.
			DuplicateKeyPolicy: DuplicateKeyInherit,
		},
		ComponentConfig: ComponentConfig{
			CustomValidator: nil,
			CustomExpander:  nil,
			CustomAuditor:   nil,
			FileSystem:      nil,
			AuditHandler:    nil,
			AuditEnabled:    false,
			Prefix:          "",
		},
	}
}

// DevelopmentConfig returns a Config optimized for development environments.
// This configuration prioritizes developer experience and flexibility:
//   - FailOnMissingFile: false (graceful handling of missing .env files)
//   - OverwriteExisting: true (easy iteration during development)
//   - AllowYamlSyntax: true (supports YAML-style values)
//   - Relaxed file-size limit (10MB; variable count keeps the default)
//   - Value validation ENABLED for security (prevents injection attacks)
//
// Example:
//
//	cfg := env.DevelopmentConfig()
//	cfg.FileConfig.Filenames = []string{".env.development"}
//	loader, err := env.New(cfg)
func DevelopmentConfig() Config {
	cfg := DefaultConfig()
	cfg.FileConfig.FailOnMissingFile = false
	cfg.FileConfig.OverwriteExisting = true
	cfg.ParsingConfig.AllowYamlSyntax = true
	cfg.LimitsConfig.MaxFileSize = 10 * 1024 * 1024
	// ValidateValues remains true for security - never disable value validation
	return cfg
}

// TestingConfig returns a Config optimized for testing environments.
// This configuration is designed for isolated, repeatable tests:
//   - FailOnMissingFile: false (tests may not have .env files)
//   - OverwriteExisting: true (test isolation)
//   - Compact size limits (test files are typically small)
//   - No audit logging (reduces test noise)
//
// Example:
//
//	func TestSomething(t *testing.T) {
//	    cfg := env.TestingConfig()
//	    cfg.FileConfig.Filenames = []string{".env.test"}
//	    loader, err := env.New(cfg)
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    defer loader.Close()
//	}
func TestingConfig() Config {
	cfg := DefaultConfig()
	cfg.FileConfig.FailOnMissingFile = false
	cfg.FileConfig.OverwriteExisting = true
	cfg.LimitsConfig.MaxFileSize = 64 * 1024 // 64KB
	cfg.LimitsConfig.MaxVariables = 50
	cfg.ComponentConfig.AuditEnabled = false
	return cfg
}

// ProductionConfig returns a Config optimized for production environments.
// This configuration provides maximum security for production deployments:
//   - FailOnMissingFile: true (fail fast on configuration errors)
//   - AuditEnabled: true (compliance and security monitoring)
//   - Strict size limits (64KB files, 50 variables)
//   - Value validation enabled
//
// Example:
//
//	cfg := env.ProductionConfig()
//	cfg.FileConfig.Filenames = []string{"app.env"}
//	cfg.ComponentConfig.AuditHandler = env.NewJSONAuditHandler(os.Stdout)
//	loader, err := env.New(cfg)
func ProductionConfig() Config {
	cfg := DefaultConfig()
	cfg.FileConfig.FailOnMissingFile = true
	cfg.FileConfig.OverwriteExisting = false
	cfg.ComponentConfig.AuditEnabled = true
	cfg.ValidationConfig.ValidateValues = true
	cfg.LimitsConfig.MaxFileSize = 64 * 1024 // 64KB
	cfg.LimitsConfig.MaxVariables = 50
	return cfg
}

// ============================================================================
// Validation Helpers
// ============================================================================

// validateConfigLimits validates that configuration limits are within acceptable ranges.
func validateConfigLimits(maxSize int64, maxLineLen, maxKeyLen, maxValLen, maxVars, maxDepth int) error {
	if maxSize <= 0 {
		return newValidationError("MaxFileSize", "", "positive", "must be positive")
	}
	if maxSize > internal.HardMaxFileSize {
		return newValidationError("MaxFileSize", "", "hard_limit", "exceeds hard limit")
	}
	if maxLineLen <= 0 {
		return newValidationError("MaxLineLength", "", "positive", "must be positive")
	}
	if maxLineLen > internal.HardMaxLineLength {
		return newValidationError("MaxLineLength", "", "hard_limit", "exceeds hard limit")
	}
	if maxKeyLen <= 0 {
		return newValidationError("MaxKeyLength", "", "positive", "must be positive")
	}
	if maxKeyLen > internal.HardMaxKeyLength {
		return newValidationError("MaxKeyLength", "", "hard_limit", "exceeds hard limit")
	}
	if maxValLen <= 0 {
		return newValidationError("MaxValueLength", "", "positive", "must be positive")
	}
	if maxValLen > internal.HardMaxValueLength {
		return newValidationError("MaxValueLength", "", "hard_limit", "exceeds hard limit")
	}
	if maxVars <= 0 {
		return newValidationError("MaxVariables", "", "positive", "must be positive")
	}
	if maxVars > internal.HardMaxVariables {
		return newValidationError("MaxVariables", "", "hard_limit", "exceeds hard limit")
	}
	if maxDepth <= 0 {
		return newValidationError("MaxExpansionDepth", "", "positive", "must be positive")
	}
	if maxDepth > internal.HardMaxExpansionDepth {
		return newValidationError("MaxExpansionDepth", "", "hard_limit", "exceeds hard limit")
	}
	return nil
}

// newValidationError creates a new ValidationError.
func newValidationError(field, value, rule, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   MaskSensitiveInString(value),
		Rule:    rule,
		Message: message,
	}
}
