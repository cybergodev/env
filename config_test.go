package env

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Config Preset Tests
// ============================================================================

func TestConfigPresets(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		cfg := DefaultConfig()

		// File handling
		if len(cfg.Filenames) != 1 || cfg.Filenames[0] != ".env" {
			t.Error("Filenames should be [.env]")
		}
		if cfg.FailOnMissingFile || cfg.OverwriteExisting {
			t.Error("FailOnMissingFile/OverwriteExisting should be false")
		}
		if !cfg.AllowExportPrefix || cfg.AllowYamlSyntax {
			t.Error("AllowExportPrefix=true, AllowYamlSyntax=false expected")
		}
		// Size limits
		if cfg.MaxFileSize != DefaultMaxFileSize || cfg.MaxLineLength != DefaultMaxLineLength {
			t.Error("Size limits should match defaults")
		}
		if cfg.MaxKeyLength != DefaultMaxKeyLength || cfg.MaxValueLength != DefaultMaxValueLength {
			t.Error("Length limits should match defaults")
		}
		if cfg.MaxVariables != DefaultMaxVariables || cfg.MaxExpansionDepth != DefaultMaxExpansionDepth {
			t.Error("Variable/depth limits should match defaults")
		}
		// Validation
		if cfg.KeyPattern != nil || len(cfg.AllowedKeys) != 0 || len(cfg.ForbiddenKeys) != 0 || len(cfg.RequiredKeys) != 0 {
			t.Error("Validation config should be empty/nil by default")
		}
		if !cfg.ValidateValues || !cfg.ExpandVariables {
			t.Error("ValidateValues and ExpandVariables should be true")
		}
		if cfg.AuditEnabled {
			t.Error("AuditEnabled should be false")
		}
		// JSON/YAML
		if !cfg.JSONNullAsEmpty || !cfg.JSONNumberAsString || cfg.JSONBoolAsString || cfg.JSONMaxDepth != defaultStructuredMaxDepth {
			t.Error("JSON defaults incorrect")
		}
		if !cfg.YAMLNullAsEmpty || !cfg.YAMLNumberAsString || !cfg.YAMLBoolAsString || cfg.YAMLMaxDepth != defaultStructuredMaxDepth {
			t.Error("YAML defaults incorrect")
		}
	})

	t.Run("DevelopmentConfig", func(t *testing.T) {
		cfg := DevelopmentConfig()

		if cfg.FailOnMissingFile || !cfg.OverwriteExisting || !cfg.AllowYamlSyntax {
			t.Error("DevelopmentConfig: FailOnMissing=false, Overwrite=true, Yaml=true")
		}
		if cfg.MaxFileSize != 10*1024*1024 || cfg.MaxVariables != DefaultMaxVariables {
			t.Errorf("DevelopmentConfig: MaxFileSize=%d, MaxVariables=%d", cfg.MaxFileSize, cfg.MaxVariables)
		}
		if !cfg.ValidateValues {
			t.Error("DevelopmentConfig: ValidateValues should be true")
		}
	})

	t.Run("TestingConfig", func(t *testing.T) {
		cfg := TestingConfig()

		if cfg.FailOnMissingFile || !cfg.OverwriteExisting {
			t.Error("TestingConfig: FailOnMissing=false, Overwrite=true")
		}
		if cfg.MaxFileSize != 64*1024 || cfg.MaxVariables != 50 {
			t.Errorf("TestingConfig: MaxFileSize=%d, MaxVariables=%d", cfg.MaxFileSize, cfg.MaxVariables)
		}
		if cfg.AuditEnabled {
			t.Error("TestingConfig: AuditEnabled should be false")
		}
	})

	t.Run("ProductionConfig", func(t *testing.T) {
		cfg := ProductionConfig()

		if !cfg.FailOnMissingFile || cfg.OverwriteExisting {
			t.Error("ProductionConfig: FailOnMissing=true, Overwrite=false")
		}
		if !cfg.AuditEnabled || !cfg.ValidateValues {
			t.Error("ProductionConfig: AuditEnabled=true, ValidateValues=true")
		}
		if cfg.MaxFileSize != 64*1024 || cfg.MaxVariables != 50 {
			t.Errorf("ProductionConfig: MaxFileSize=%d, MaxVariables=%d", cfg.MaxFileSize, cfg.MaxVariables)
		}
	})
}

// ============================================================================
// Config.Validate Tests
// ============================================================================

func TestConfig_Validate(t *testing.T) {
	t.Run("valid default config", func(t *testing.T) {
		cfg := DefaultConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v", err)
		}
	})

	// JSONMaxDepth and YAMLMaxDepth share the valid range 1..100 inclusive
	// (validateFormatConfig: `<= 0 || > 100`). Note that 0 is an ERROR at the
	// Validate layer — defaulting only happens when a fully-zero Config is
	// replaced by DefaultConfig() in New. 1 and 100 are the at-boundary valid
	// rows; 0 and 101 sit just outside.
	setters := []struct {
		name string
		set  func(*Config, int)
	}{
		{"JSONMaxDepth", func(c *Config, v int) { c.JSONMaxDepth = v }},
		{"YAMLMaxDepth", func(c *Config, v int) { c.YAMLMaxDepth = v }},
	}
	for _, setter := range setters {
		for _, tt := range []struct {
			val     int
			wantErr bool
		}{
			{val: 0, wantErr: true},
			{val: 1, wantErr: false},
			{val: 100, wantErr: false},
			{val: 101, wantErr: true},
		} {
			t.Run(fmt.Sprintf("%s=%d", setter.name, tt.val), func(t *testing.T) {
				cfg := DefaultConfig()
				setter.set(&cfg, tt.val)

				err := cfg.Validate()
				if (err != nil) != tt.wantErr {
					t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !tt.wantErr {
					return
				}
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("Validate() error = %T (%v), want *ValidationError", err, err)
				}
				if ve.Field != setter.name {
					t.Errorf("ValidationError.Field = %q, want %q", ve.Field, setter.name)
				}
			})
		}
	}

	// ExpansionScope is an enum with exactly two legal values; anything else
	// (here 99) must be rejected. DuplicateKeyPolicy has equivalent coverage
	// in duplicate_key_test.go; this closes the asymmetry.
	t.Run("invalid ExpansionScope enum value", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.ExpansionScope = ExpansionScope(99)

		err := cfg.Validate()
		if err == nil {
			t.Fatal("Validate() error = nil, want enum rejection")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("Validate() error = %T (%v), want *ValidationError", err, err)
		}
		if ve.Field != "ExpansionScope" {
			t.Errorf("ValidationError.Field = %q, want %q", ve.Field, "ExpansionScope")
		}
	})
}

// ============================================================================
// KeyPattern Validation (table)
// ============================================================================

// TestValidateKeyPattern drives every branch of validateKeyPattern through
// Config.Validate and pins which rule fires. For rejected patterns it also
// asserts New() refuses the configuration.
func TestValidateKeyPattern(t *testing.T) {
	tests := []struct {
		name      string
		pattern   *regexp.Regexp
		wantValid bool
		wantRule  string // ValidationError.Rule of the rejecting branch; "" when valid
	}{
		{name: "nil pattern uses the default validation", pattern: nil, wantValid: true},
		{name: "uppercase pattern is valid", pattern: regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`), wantValid: true},
		{
			// Namespace-style patterns need not match any particular sample
			// key; validation only rejects patterns that would accept unsafe
			// keys (the probes below).
			name:      "namespace pattern not matching TEST_KEY is accepted",
			pattern:   regexp.MustCompile(`^MYAPP_[A-Z0-9_]*$`),
			wantValid: true,
		},
		{
			name:      "pattern allowing numeric start is rejected",
			pattern:   regexp.MustCompile(`^[A-Z0-9][A-Z0-9_]*$`),
			wantValid: false,
			wantRule:  "reject_numeric_start",
		},
		{
			// A pattern matching the empty string would accept empty key
			// names (config.go reject_empty branch); previously untested.
			name:      "pattern matching empty string is rejected",
			pattern:   regexp.MustCompile(`^[A-Z0-9_]*$`),
			wantValid: false,
			wantRule:  "reject_empty",
		},
		{
			// One representative of the SEC-06 unsafe-character probes; the
			// full pattern matrix lives in sec_regression_test.go
			// (TestSEC06_KeyPatternRejectsInjectionEnablers).
			name:      "pattern matching separator characters is rejected",
			pattern:   regexp.MustCompile(`^[A-Z][A-Z=_]*$`),
			wantValid: false,
			wantRule:  "reject_unsafe_chars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.KeyPattern = tt.pattern
			cfg.Filenames = nil // no auto-load: rejection must come from validation alone

			err := cfg.Validate()
			if tt.wantValid {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error = %T (%v), want *ValidationError", err, err)
			}
			if ve.Rule != tt.wantRule {
				t.Errorf("ValidationError.Rule = %q, want %q", ve.Rule, tt.wantRule)
			}
			loader, err := New(cfg)
			if err == nil {
				_ = loader.Close()
				t.Error("New() error = nil, want the invalid pattern to be rejected")
			}
		})
	}
}

// ============================================================================
// KeyPattern Edge Case Tests (runtime behavior)
// ============================================================================

// (TestKeyPattern_EdgeCases's "nil pattern allows standard keys" case was
// removed: DefaultConfig ships KeyPattern nil (asserted in TestConfigPresets)
// and the nil-pattern Set path is the default configuration under which every
// other loader test in this package already runs.)

// TestKeyPattern_CustomPattern merges config-level validity of
// ^[A-Z][A-Z0-9_]*$ plus the runtime Set behavior under that pattern.
func TestKeyPattern_CustomPattern(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with valid pattern error = %v", err)
	}
	cfg.Filenames = nil

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	if err := loader.Set("VALID_KEY", "value"); err != nil {
		t.Errorf("Set() error = %v", err)
	}
}

// ============================================================================
// validateConfigLimits Tests
// ============================================================================

func TestValidateConfigLimits(t *testing.T) {
	tests := []struct {
		name        string
		maxSize     int64
		maxLineLen  int
		maxKeyLen   int
		maxValLen   int
		maxVars     int
		maxDepth    int
		wantErr     bool
		errContains string
	}{
		{
			name:       "valid limits",
			maxSize:    DefaultMaxFileSize,
			maxLineLen: DefaultMaxLineLength,
			maxKeyLen:  DefaultMaxKeyLength,
			maxValLen:  DefaultMaxValueLength,
			maxVars:    DefaultMaxVariables,
			maxDepth:   DefaultMaxExpansionDepth,
			wantErr:    false,
		},
		{
			// At-cap boundary rows: validateConfigLimits compares with a
			// strict `>` against the HardMax* constants, so a value exactly at
			// the cap must pass. A regression to `>=` fails these rows.
			name:       "max file size at hard cap passes",
			maxSize:    internal.HardMaxFileSize,
			maxLineLen: DefaultMaxLineLength,
			maxKeyLen:  DefaultMaxKeyLength,
			maxValLen:  DefaultMaxValueLength,
			maxVars:    DefaultMaxVariables,
			maxDepth:   DefaultMaxExpansionDepth,
			wantErr:    false,
		},
		{
			name:       "max line length at hard cap passes",
			maxSize:    DefaultMaxFileSize,
			maxLineLen: internal.HardMaxLineLength,
			maxKeyLen:  DefaultMaxKeyLength,
			maxValLen:  DefaultMaxValueLength,
			maxVars:    DefaultMaxVariables,
			maxDepth:   DefaultMaxExpansionDepth,
			wantErr:    false,
		},
		{
			name:       "max key length at hard cap passes",
			maxSize:    DefaultMaxFileSize,
			maxLineLen: DefaultMaxLineLength,
			maxKeyLen:  internal.HardMaxKeyLength,
			maxValLen:  DefaultMaxValueLength,
			maxVars:    DefaultMaxVariables,
			maxDepth:   DefaultMaxExpansionDepth,
			wantErr:    false,
		},
		{
			name:       "max value length at hard cap passes",
			maxSize:    DefaultMaxFileSize,
			maxLineLen: DefaultMaxLineLength,
			maxKeyLen:  DefaultMaxKeyLength,
			maxValLen:  internal.HardMaxValueLength,
			maxVars:    DefaultMaxVariables,
			maxDepth:   DefaultMaxExpansionDepth,
			wantErr:    false,
		},
		{
			name:       "max variables at hard cap passes",
			maxSize:    DefaultMaxFileSize,
			maxLineLen: DefaultMaxLineLength,
			maxKeyLen:  DefaultMaxKeyLength,
			maxValLen:  DefaultMaxValueLength,
			maxVars:    internal.HardMaxVariables,
			maxDepth:   DefaultMaxExpansionDepth,
			wantErr:    false,
		},
		{
			name:       "max expansion depth at hard cap passes",
			maxSize:    DefaultMaxFileSize,
			maxLineLen: DefaultMaxLineLength,
			maxKeyLen:  DefaultMaxKeyLength,
			maxValLen:  DefaultMaxValueLength,
			maxVars:    DefaultMaxVariables,
			maxDepth:   internal.HardMaxExpansionDepth,
			wantErr:    false,
		},
		{
			name:        "zero max file size",
			maxSize:     0,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxFileSize",
		},
		{
			name:        "negative max file size",
			maxSize:     -1,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxFileSize",
		},
		{
			name:        "exceeds hard max file size",
			maxSize:     200 * 1024 * 1024,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxFileSize",
		},
		{
			name:        "zero max line length",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  0,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxLineLength",
		},
		{
			name:        "exceeds hard max line length",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  100 * 1024,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxLineLength",
		},
		{
			name:        "zero max key length",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   0,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxKeyLength",
		},
		{
			name:        "exceeds hard max key length",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   1025,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxKeyLength",
		},
		{
			name:        "zero max value length",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   0,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxValueLength",
		},
		{
			name:        "exceeds hard max value length",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   1024*1024 + 1,
			maxVars:     DefaultMaxVariables,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxValueLength",
		},
		{
			name:        "zero max variables",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     0,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxVariables",
		},
		{
			name:        "exceeds hard max variables",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     10001,
			maxDepth:    DefaultMaxExpansionDepth,
			wantErr:     true,
			errContains: "MaxVariables",
		},
		{
			name:        "zero max expansion depth",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    0,
			wantErr:     true,
			errContains: "MaxExpansionDepth",
		},
		{
			name:        "exceeds hard max expansion depth",
			maxSize:     DefaultMaxFileSize,
			maxLineLen:  DefaultMaxLineLength,
			maxKeyLen:   DefaultMaxKeyLength,
			maxValLen:   DefaultMaxValueLength,
			maxVars:     DefaultMaxVariables,
			maxDepth:    50,
			wantErr:     true,
			errContains: "MaxExpansionDepth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigLimits(tt.maxSize, tt.maxLineLen, tt.maxKeyLen, tt.maxValLen, tt.maxVars, tt.maxDepth)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfigLimits() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				var valErr *ValidationError
				if !errors.As(err, &valErr) {
					t.Errorf("validateConfigLimits() error type = %T, want *ValidationError", err)
				} else if tt.errContains != "" && valErr.Field != tt.errContains {
					t.Errorf("validateConfigLimits() error field = %s, want %s", valErr.Field, tt.errContains)
				}
			}
		})
	}
}

// ============================================================================
// newValidationError Tests
// ============================================================================

func TestNewValidationError(t *testing.T) {
	err := newValidationError("TestField", "test_value", "test_rule", "test message")

	if err.Field != "TestField" {
		t.Errorf("Field = %q, want %q", err.Field, "TestField")
	}
	if err.Rule != "test_rule" {
		t.Errorf("Rule = %q, want %q", err.Rule, "test_rule")
	}
	if err.Message != "test message" {
		t.Errorf("Message = %q, want %q", err.Message, "test message")
	}
	if err.Value != "test_value" {
		t.Errorf("Value = %q, want %q (benign values pass through unmasked)", err.Value, "test_value")
	}

	// Values that look sensitive are masked before reaching the error
	// (newValidationError routes Value through MaskSensitiveInString).
	masked := newValidationError("PasswordField", "password=hunter2", "test_rule", "test message")
	if masked.Value != "password=[MASKED]" {
		t.Errorf("Value = %q, want %q (sensitive-looking values must be masked)", masked.Value, "password=[MASKED]")
	}
}

// ============================================================================
// Config with AuditHandler Tests
// ============================================================================

func TestConfig_WithAuditHandler(t *testing.T) {
	ch := make(chan AuditEvent, 10)

	cfg := DefaultConfig()
	cfg.AuditEnabled = true
	cfg.AuditHandler = NewChannelAuditHandler(ch)

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	// The configured handler must receive audit events produced by loader
	// operations, not just be accepted during construction. New() emits
	// load events first, so drain the channel until the Set event arrives.
	if err := loader.Set("KEY", "value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	found := false
	for range cap(ch) {
		select {
		case event := <-ch:
			if event.Action == ActionSet && event.Key == "KEY" {
				found = true
				if !event.Success {
					t.Errorf("audit event success = false, want true")
				}
			}
		default:
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("no Set audit event received on channel after Set()")
	}
}

// ============================================================================
// Config Policy Behavior (AllowedKeys / ForbiddenKeys / RequiredKeys /
// OverwriteExisting), table-driven
// ============================================================================

// rmConfigPolicyCase is one row of the merged policy table: build a Config,
// run the loader operations, then check the op error sentinel and the
// resulting key/value state.
type rmConfigPolicyCase struct {
	name   string
	setup  func(*Config)
	ops    func(*Loader) error
	wantOp error // errors.Is target for the error returned by ops; nil expects success
	wantKV map[string]string
}

func TestConfigPolicyBehavior(t *testing.T) {
	tests := []rmConfigPolicyCase{
		{
			name:   "allowed key is accepted",
			setup:  func(c *Config) { c.AllowedKeys = []string{"ALLOWED_KEY"} },
			ops:    func(l *Loader) error { return l.Set("ALLOWED_KEY", "value") },
			wantKV: map[string]string{"ALLOWED_KEY": "value"},
		},
		{
			name:   "key outside AllowedKeys is rejected",
			setup:  func(c *Config) { c.AllowedKeys = []string{"ALLOWED_KEY"} },
			ops:    func(l *Loader) error { return l.Set("NOT_ALLOWED", "value") },
			wantOp: ErrForbiddenKey,
		},
		{
			name:   "ForbiddenKeys entry is rejected",
			setup:  func(c *Config) { c.ForbiddenKeys = []string{"FORBIDDEN_KEY"}; c.OverwriteExisting = true },
			ops:    func(l *Loader) error { return l.Set("FORBIDDEN_KEY", "value") },
			wantOp: ErrForbiddenKey,
		},
		{
			name: "overwrite disabled keeps first value",
			ops: func(l *Loader) error {
				if err := l.Set("KEY", "value1"); err != nil {
					return err
				}
				return l.Set("KEY", "value2")
			},
			wantKV: map[string]string{"KEY": "value1"},
		},
		{
			name:  "overwrite enabled takes last value",
			setup: func(c *Config) { c.OverwriteExisting = true },
			ops: func(l *Loader) error {
				if err := l.Set("KEY", "value1"); err != nil {
					return err
				}
				return l.Set("KEY", "value2")
			},
			wantKV: map[string]string{"KEY": "value2"},
		},
		{
			// RequiredKeys-through-config wiring row. The full loader-level
			// scenario (multiple required keys, ordering) lives in
			// loader_test.go TestLoader_Validate; this row pins that a Config
			// carrying RequiredKeys drives Loader.Validate: missing -> error,
			// set -> nil.
			name:  "required key drives Validate",
			setup: func(c *Config) { c.RequiredKeys = []string{"REQUIRED_KEY"} },
			ops: func(l *Loader) error {
				if err := l.Validate(); err == nil {
					return errors.New("Validate() = nil, want failure while REQUIRED_KEY is missing")
				}
				if err := l.Set("REQUIRED_KEY", "value"); err != nil {
					return err
				}
				return l.Validate()
			},
			wantKV: map[string]string{"REQUIRED_KEY": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Filenames = nil // no auto-load: the ops below are the scenario
			if tt.setup != nil {
				tt.setup(&cfg)
			}

			loader, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer func() { _ = loader.Close() }()

			opErr := tt.ops(loader)
			if tt.wantOp == nil {
				if opErr != nil {
					t.Errorf("ops error = %v, want nil", opErr)
				}
			} else if !errors.Is(opErr, tt.wantOp) {
				t.Errorf("ops error = %v, want errors.Is(err, %v)", opErr, tt.wantOp)
			}

			for key, want := range tt.wantKV {
				if got := loader.GetString(key); got != want {
					t.Errorf("GetString(%q) = %q, want %q", key, got, want)
				}
			}
		})
	}
}

// ============================================================================
// Config.IsZero Completeness Tests
// ============================================================================

// (TestConfigIsZero_Completeness and its rmIsZero* stubs were removed: the
// reflection walk in TestConfigIsZero_CoversEveryField below makes the same
// per-field assertion for EVERY Config leaf field — including fields added
// later, which the former hand-maintained table silently missed.)

// Stubs for the Config interface fields in TestConfigIsZero_CoversEveryField.
type izValidator struct{}

func (izValidator) ValidateKey(string) error               { return nil }
func (izValidator) ValidateValue(string) error             { return nil }
func (izValidator) ValidateRequired(map[string]bool) error { return nil }

type izExpander struct{}

func (izExpander) Expand(s string) (string, error) { return s, nil }

type izAuditor struct{}

func (izAuditor) LogError(AuditAction, string, string) error { return nil }

type izHandler struct{}

func (izHandler) Log(AuditEvent) error { return nil }
func (izHandler) Close() error         { return nil }

// nonZeroConfigValue synthesizes a non-zero value of type t for the IsZero
// coverage walk. The SECOND return is invalid for kinds the synthesizer does
// not know (reported by the test).
func nonZeroConfigValue(t reflect.Type) reflect.Value {
	switch t.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(true).Convert(t)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.ValueOf(1).Convert(t)
	case reflect.String:
		return reflect.ValueOf("x").Convert(t)
	case reflect.Slice:
		return reflect.MakeSlice(t, 1, 1)
	case reflect.Pointer:
		return reflect.New(t.Elem())
	}
	return reflect.Value{}
}

// TestConfigIsZero_CoversEveryField locks IsZero's field coverage against
// drift: setting ANY single leaf field of Config (including fields promoted
// through the embedded sections) to a non-zero value must make IsZero report
// false. Two real bugs — ExpansionScope and DuplicateKeyPolicy silently
// replaced by DefaultConfig() — came from fields missing here while the
// compiler stayed silent.
func TestConfigIsZero_CoversEveryField(t *testing.T) {
	// Lead assertions (from the former TestConfigIsZero_Completeness): the
	// zero value IS zero, and DefaultConfig is not.
	var zeroCfg Config
	if !zeroCfg.IsZero() {
		t.Error("Zero-value Config should return true for IsZero()")
	}
	defaultCfg := DefaultConfig()
	if defaultCfg.IsZero() {
		t.Error("DefaultConfig() should return non-zero Config")
	}

	// Concrete implementations for the interface-typed fields, keyed by
	// field name.
	interfaceImpls := map[string]any{
		"FileSystem":      DefaultFileSystem,
		"CustomValidator": izValidator{},
		"CustomExpander":  izExpander{},
		"CustomAuditor":   izAuditor{},
		"AuditHandler":    izHandler{},
	}

	var walk func(section reflect.Type, prefix string)
	walk = func(section reflect.Type, prefix string) {
		for i := 0; i < section.NumField(); i++ {
			f := section.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type, prefix+f.Name+".")
				continue
			}

			var v reflect.Value
			if f.Type.Kind() == reflect.Interface {
				impl, ok := interfaceImpls[f.Name]
				if !ok {
					t.Errorf("%s%s: interface field without a test stub — add one so coverage stays complete", prefix, f.Name)
					continue
				}
				v = reflect.ValueOf(impl)
				if !v.Type().Implements(f.Type) {
					t.Errorf("%s%s: stub %T does not implement %s", prefix, f.Name, impl, f.Type)
					continue
				}
			} else {
				v = nonZeroConfigValue(f.Type)
			}
			if !v.IsValid() {
				t.Errorf("%s%s (%s): unsupported field kind — extend nonZeroConfigValue", prefix, f.Name, f.Type)
				continue
			}

			cfgValue := reflect.ValueOf(&Config{}).Elem()
			field := cfgValue.FieldByName(f.Name) // promoted through embedding
			if !field.IsValid() || !field.CanSet() {
				t.Fatalf("%s%s: not settable on Config", prefix, f.Name)
			}
			field.Set(v)
			if cfgValue.Addr().Interface().(*Config).IsZero() {
				t.Errorf("Config with only %s%s set reports IsZero()=true — the field is missing from IsZero", prefix, f.Name)
			}
		}
	}
	walk(reflect.TypeOf(Config{}), "")
}

// ============================================================================
// Default Forbidden-Keys List (moved from the former limits_test.go — the
// Set-level forbidden-key policy tests belong with the other Config policy
// behavior)
// ============================================================================

// TestDefaultForbiddenKeysSlice guards the slice-literal conversion of the
// default forbidden-key list: the previous map-derived initialization made
// duplicates structurally impossible, so the literal needs an explicit check.
func TestDefaultForbiddenKeysSlice(t *testing.T) {
	seen := make(map[string]bool, len(defaultForbiddenKeysSlice))
	for _, k := range defaultForbiddenKeysSlice {
		if seen[k] {
			t.Errorf("duplicate forbidden key: %s", k)
		}
		seen[k] = true
	}
	if len(defaultForbiddenKeysSlice) == 0 {
		t.Error("defaultForbiddenKeysSlice must not be empty")
	}
}

// TestDefaultForbiddenKeysRejectInjectionVectors pins SEC-003 (CWE-74/94): the
// runtime/VM option-injection vectors added to the default forbidden-key list
// must actually be rejected by a Loader built from DefaultConfig.
func TestDefaultForbiddenKeysRejectInjectionVectors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Filenames = nil
	loader, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loader.Close() }() // best-effort cleanup

	for _, key := range []string{
		"NODE_OPTIONS", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS",
		"CLASSPATH", "PYTHONSTARTUP", "PYTHONINSPECT", "PS4", "SHELLOPTS", "BASHOPTS",
		"GIT_CONFIG_COUNT", "GIT_PROXY_COMMAND", "PERL5LIB", "RUBYOPT",
	} {
		if err := loader.Set(key, "injected"); err == nil {
			t.Errorf("Set(%q) = nil, want ErrForbiddenKey", key)
		}
	}
}
