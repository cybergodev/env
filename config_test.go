package env

import (
	"errors"
	"regexp"
	"testing"
)

// ============================================================================
// Config Preset Tests (Table-Driven)
// ============================================================================

func TestConfigPresets(t *testing.T) {
	tests := []struct {
		name     string
		factory  func() Config
		checks   []struct {
			desc string
			ok   bool
		}
	}{
		{
			name:    "DefaultConfig",
			factory: DefaultConfig,
			checks: []struct {
				desc string
				ok   bool
			}{
				{"Filenames=[.env]", false},  // placeholder; real checks below
			},
		},
		{
			name:    "DevelopmentConfig",
			factory: DevelopmentConfig,
			checks:  nil, // checked via assertions below
		},
		{
			name:    "TestingConfig",
			factory: TestingConfig,
			checks:  nil,
		},
		{
			name:    "ProductionConfig",
			factory: ProductionConfig,
			checks:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.factory()

			switch tt.name {
			case "DefaultConfig":
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
				if !cfg.JSONNullAsEmpty || !cfg.JSONNumberAsString || !cfg.JSONBoolAsString || cfg.JSONMaxDepth != 10 {
					t.Error("JSON defaults incorrect")
				}
				if !cfg.YAMLNullAsEmpty || !cfg.YAMLNumberAsString || !cfg.YAMLBoolAsString || cfg.YAMLMaxDepth != 10 {
					t.Error("YAML defaults incorrect")
				}

			case "DevelopmentConfig":
				if cfg.FailOnMissingFile || !cfg.OverwriteExisting || !cfg.AllowYamlSyntax {
					t.Error("DevelopmentConfig: FailOnMissing=false, Overwrite=true, Yaml=true")
				}
				if cfg.MaxFileSize != 10*1024*1024 || cfg.MaxVariables != 500 {
					t.Errorf("DevelopmentConfig: MaxFileSize=%d, MaxVariables=%d", cfg.MaxFileSize, cfg.MaxVariables)
				}
				if !cfg.ValidateValues {
					t.Error("DevelopmentConfig: ValidateValues should be true")
				}

			case "TestingConfig":
				if cfg.FailOnMissingFile || !cfg.OverwriteExisting {
					t.Error("TestingConfig: FailOnMissing=false, Overwrite=true")
				}
				if cfg.MaxFileSize != 64*1024 || cfg.MaxVariables != 50 {
					t.Errorf("TestingConfig: MaxFileSize=%d, MaxVariables=%d", cfg.MaxFileSize, cfg.MaxVariables)
				}
				if cfg.AuditEnabled {
					t.Error("TestingConfig: AuditEnabled should be false")
				}

			case "ProductionConfig":
				if !cfg.FailOnMissingFile || cfg.OverwriteExisting {
					t.Error("ProductionConfig: FailOnMissing=true, Overwrite=false")
				}
				if !cfg.AuditEnabled || !cfg.ValidateValues {
					t.Error("ProductionConfig: AuditEnabled=true, ValidateValues=true")
				}
				if cfg.MaxFileSize != 64*1024 || cfg.MaxVariables != 50 {
					t.Errorf("ProductionConfig: MaxFileSize=%d, MaxVariables=%d", cfg.MaxFileSize, cfg.MaxVariables)
				}
			}
		})
	}
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

	t.Run("invalid key pattern", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.KeyPattern = regexp.MustCompile(`^[a-z]+$`)
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should fail with invalid key pattern")
		}
	})

	t.Run("invalid JSON max depth zero", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.JSONMaxDepth = 0
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should fail with zero JSONMaxDepth")
		}
	})

	t.Run("invalid JSON max depth too large", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.JSONMaxDepth = 101
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should fail with JSONMaxDepth > 100")
		}
	})

	t.Run("invalid YAML max depth zero", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.YAMLMaxDepth = 0
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should fail with zero YAMLMaxDepth")
		}
	})

	t.Run("invalid YAML max depth too large", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.YAMLMaxDepth = 101
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should fail with YAMLMaxDepth > 100")
		}
	})

	t.Run("valid custom key pattern", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.KeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with valid pattern error = %v", err)
		}
	})
}

// ============================================================================
// KeyPattern Edge Case Tests
// ============================================================================

func TestKeyPattern_EdgeCases(t *testing.T) {
	// Test the default key pattern behavior
	t.Run("nil pattern allows standard keys", func(t *testing.T) {
		cfg := DefaultConfig()
		// KeyPattern is nil by default for fast byte-level validation
		if cfg.KeyPattern != nil {
			t.Error("KeyPattern should be nil by default")
		}

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// Standard keys should work
		if err := loader.Set("TEST_KEY", "value"); err != nil {
			t.Errorf("Set() error = %v", err)
		}
		if err := loader.Set("API_KEY_123", "value"); err != nil {
			t.Errorf("Set() error = %v", err)
		}
	})

	t.Run("custom pattern matches valid keys", func(t *testing.T) {
		cfg := DefaultConfig()
		// Pattern that matches uppercase with underscores and numbers (and TEST_KEY)
		cfg.KeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		// Keys matching the pattern should work
		if err := loader.Set("VALID_KEY", "value"); err != nil {
			t.Errorf("Set() error = %v", err)
		}
	})

	t.Run("pattern must match TEST_KEY during validation", func(t *testing.T) {
		cfg := DefaultConfig()
		// Pattern that only allows lowercase - this will fail validation
		// because it can't match TEST_KEY
		cfg.KeyPattern = regexp.MustCompile(`^[a-z]+$`)

		_, err := New(cfg)
		if err == nil {
			t.Error("New() should fail with pattern that doesn't match TEST_KEY")
		}
	})

	t.Run("pattern must not allow numeric start", func(t *testing.T) {
		cfg := DefaultConfig()
		// Pattern that allows keys starting with numbers
		// This should fail validation because it allows numeric-start keys
		cfg.KeyPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_]*$`)

		_, err := New(cfg)
		if err == nil {
			t.Error("New() should fail with pattern that allows numeric-start keys")
		}
	})
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
}

// ============================================================================
// Config with Custom FileSystem Tests
// ============================================================================

func TestConfig_WithCustomFileSystem(t *testing.T) {
	fs := newTestFileSystem()
	fs.files[".env"] = "KEY=value"

	cfg := DefaultConfig()
	cfg.FileSystem = fs

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	if err := loader.LoadFiles(".env"); err != nil {
		t.Fatalf("LoadFiles() error = %v", err)
	}

	if loader.GetString("KEY") != "value" {
		t.Errorf("GetString(\"KEY\") = %q, want %q", loader.GetString("KEY"), "value")
	}
}

// ============================================================================
// Config with AuditHandler Tests
// ============================================================================

func TestConfig_WithAuditHandler(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuditEnabled = true
	cfg.AuditHandler = NewNopAuditHandler()

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	// Should not error with audit handler configured
	if err := loader.Set("KEY", "value"); err != nil {
		t.Errorf("Set() error = %v", err)
	}
}

// ============================================================================
// Config with AllowedKeys Tests
// ============================================================================

func TestConfig_WithAllowedKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedKeys = []string{"ALLOWED_KEY"}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	t.Run("allowed key", func(t *testing.T) {
		if err := loader.Set("ALLOWED_KEY", "value"); err != nil {
			t.Errorf("Set() allowed key error = %v", err)
		}
	})

	t.Run("non-allowed key", func(t *testing.T) {
		if err := loader.Set("NOT_ALLOWED", "value"); err == nil {
			t.Error("Set() should fail with non-allowed key")
		}
	})
}

// ============================================================================
// Config with ForbiddenKeys Tests
// ============================================================================

func TestConfig_WithForbiddenKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ForbiddenKeys = []string{"FORBIDDEN_KEY"}
	cfg.OverwriteExisting = true

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	if err := loader.Set("FORBIDDEN_KEY", "value"); err == nil {
		t.Error("Set() should fail with forbidden key")
	}
}

// ============================================================================
// Config with RequiredKeys Tests
// ============================================================================

func TestConfig_WithRequiredKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RequiredKeys = []string{"REQUIRED_KEY"}

	loader, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer loader.Close()

	t.Run("validate without required key", func(t *testing.T) {
		if err := loader.Validate(); err == nil {
			t.Error("Validate() should fail without required key")
		}
	})

	t.Run("validate with required key", func(t *testing.T) {
		if err := loader.Set("REQUIRED_KEY", "value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Validate(); err != nil {
			t.Errorf("Validate() error = %v", err)
		}
	})
}

// ============================================================================
// Config OverwriteExisting Tests
// ============================================================================

func TestConfig_OverwriteExisting(t *testing.T) {
	t.Run("overwrite disabled", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.OverwriteExisting = false

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY", "value1"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Set("KEY", "value2"); err != nil {
			t.Fatalf("Set() error = %v, want nil", err)
		}

		// Value should not change
		if loader.GetString("KEY") != "value1" {
			t.Errorf("GetString(\"KEY\") = %q, want %q", loader.GetString("KEY"), "value1")
		}
	})

	t.Run("overwrite enabled", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.OverwriteExisting = true

		loader, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer loader.Close()

		if err := loader.Set("KEY", "value1"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := loader.Set("KEY", "value2"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		// Value should change
		if loader.GetString("KEY") != "value2" {
			t.Errorf("GetString(\"KEY\") = %q, want %q", loader.GetString("KEY"), "value2")
		}
	})
}

// ============================================================================
// Config.IsZero Completeness Tests
// ============================================================================

func TestConfigIsZero_Completeness(t *testing.T) {
	// Zero-value config should be detected as zero
	var zeroCfg Config
	if !zeroCfg.IsZero() {
		t.Error("Zero-value Config should return true for IsZero()")
	}

	// DefaultConfig should NOT be zero
	defaultCfg := DefaultConfig()
	if defaultCfg.IsZero() {
		t.Error("DefaultConfig() should return non-zero Config")
	}

	// Each non-zero-default field should make IsZero return false
	tests := []struct {
		name string
		cfg  Config
	}{
		{"MaxFileSize", Config{LimitsConfig: LimitsConfig{MaxFileSize: 1}}},
		{"MaxVariables", Config{LimitsConfig: LimitsConfig{MaxVariables: 1}}},
		{"MaxLineLength", Config{LimitsConfig: LimitsConfig{MaxLineLength: 1}}},
		{"MaxKeyLength", Config{LimitsConfig: LimitsConfig{MaxKeyLength: 1}}},
		{"MaxValueLength", Config{LimitsConfig: LimitsConfig{MaxValueLength: 1}}},
		{"MaxExpansionDepth", Config{LimitsConfig: LimitsConfig{MaxExpansionDepth: 1}}},
		{"ValidateValues", Config{ValidationConfig: ValidationConfig{ValidateValues: true}}},
		{"JSONNullAsEmpty", Config{JSONConfig: JSONConfig{JSONNullAsEmpty: true}}},
		{"JSONNumberAsString", Config{JSONConfig: JSONConfig{JSONNumberAsString: true}}},
		{"JSONBoolAsString", Config{JSONConfig: JSONConfig{JSONBoolAsString: true}}},
		{"YAMLNullAsEmpty", Config{YAMLConfig: YAMLConfig{YAMLNullAsEmpty: true}}},
		{"YAMLNumberAsString", Config{YAMLConfig: YAMLConfig{YAMLNumberAsString: true}}},
		{"YAMLBoolAsString", Config{YAMLConfig: YAMLConfig{YAMLBoolAsString: true}}},
		{"AllowExportPrefix", Config{ParsingConfig: ParsingConfig{AllowExportPrefix: true}}},
		{"ExpandVariables", Config{ParsingConfig: ParsingConfig{ExpandVariables: true}}},
		{"KeyPattern", Config{ValidationConfig: ValidationConfig{KeyPattern: regexp.MustCompile(".")}}},
		{"Filenames", Config{FileConfig: FileConfig{Filenames: []string{".env"}}}},
		{"FileSystem", Config{ComponentConfig: ComponentConfig{FileSystem: DefaultFileSystem}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.IsZero() {
				t.Errorf("Config with %s set should not be detected as zero", tt.name)
			}
		})
	}
}
