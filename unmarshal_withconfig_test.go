package env

import (
	"errors"
	"testing"
)

// ============================================================================
// UnmarshalMapWithConfig / UnmarshalStructWithConfig (D-005 Phase 2)
//
// These tests pin the config-driven one-shot parsing contract: the same
// semantics a Loader built from the same Config applies to a single file —
// duplicate-key policy, limits, expansion, format options, and the prefix
// filter — plus the zero-value and ErrInvalidConfig contract shared with New.
// ============================================================================

func TestUnmarshalMapWithConfig_ZeroConfigUsesDefaults(t *testing.T) {
	const doc = `{"server": {"port": 8080}}`

	// A fully zero Config is DefaultConfig(): JSON numbers flatten to strings.
	m, err := UnmarshalMapWithConfig(Config{}, doc, FormatJSON)
	if err != nil {
		t.Fatalf("UnmarshalMapWithConfig(Config{}, …) error = %v", err)
	}
	if m["SERVER_PORT"] != "8080" {
		t.Errorf("SERVER_PORT = %q, want %q", m["SERVER_PORT"], "8080")
	}

	// FormatAuto is detected from the data, as in UnmarshalMap.
	m, err = UnmarshalMapWithConfig(Config{}, doc, FormatAuto)
	if err != nil {
		t.Fatalf("UnmarshalMapWithConfig(FormatAuto) error = %v", err)
	}
	if m["SERVER_PORT"] != "8080" {
		t.Errorf("FormatAuto SERVER_PORT = %q, want %q", m["SERVER_PORT"], "8080")
	}
}

// TestUnmarshalMapWithConfig_DuplicateKeyPolicy keeps one row per format for
// each contract unique to this entry point: a zero-value Config keeps each
// format's historical default policy (Inherit), and an explicit policy is
// transmitted through the same parser pipeline a Loader uses. The full policy
// matrix (Inherit × OverwriteExisting, FirstWins, LastWins, JSON FirstWins
// rejection, flat-key collisions) lives in duplicate_key_test.go via the
// loader path.
func TestUnmarshalMapWithConfig_DuplicateKeyPolicy(t *testing.T) {
	tests := []struct {
		name      string
		zeroCfg   bool // true: Config{} (→ DefaultConfig); false: DefaultConfig + policy
		policy    DuplicateKeyPolicy
		format    FileFormat
		doc       string
		wantValue string
		wantErr   error
	}{
		// Inherit keeps the .env historical behavior: first wins while
		// OverwriteExisting is false (DefaultConfig keeps it false).
		{name: "env zero-value config keeps first-wins default", zeroCfg: true, format: FormatEnv, doc: "A=1\nA=2\n", wantValue: "1"},
		// Inherit keeps YAML's historical behavior: last occurrence wins.
		{name: "yaml zero-value config keeps last-wins default", zeroCfg: true, format: FormatYAML, doc: "a: 1\na: 2\n", wantValue: "2"},
		// Inherit keeps the JSON behavior: last occurrence wins (encoding/json).
		{name: "json zero-value config keeps last-wins default", zeroCfg: true, format: FormatJSON, doc: `{"a": 1, "a": 2}`, wantValue: "2"},
		{name: "env policy transmitted: error", policy: DuplicateKeyError, format: FormatEnv, doc: "A=1\nA=2\n", wantErr: ErrDuplicateKey},
		{name: "yaml policy transmitted: error", policy: DuplicateKeyError, format: FormatYAML, doc: "a: 1\na: 2\n", wantErr: ErrDuplicateKey},
		{name: "json policy transmitted: error", policy: DuplicateKeyError, format: FormatJSON, doc: `{"a": 1, "a": 2}`, wantErr: ErrDuplicateKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			if tt.zeroCfg {
				cfg = Config{}
			} else {
				cfg.DuplicateKeyPolicy = tt.policy
			}

			m, err := UnmarshalMapWithConfig(cfg, tt.doc, tt.format)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want errors.Is(%v)", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if m["A"] != tt.wantValue {
				t.Errorf("A = %q, want %q", m["A"], tt.wantValue)
			}
		})
	}
}

func TestUnmarshalMapWithConfig_Limits(t *testing.T) {
	t.Run("max variables", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxVariables = 2
		_, err := UnmarshalMapWithConfig(cfg, "A=1\nB=2\nC=3\n")
		if err == nil {
			t.Fatal("error = nil, want max-variables error")
		}
		if !errors.Is(err, ErrMaxVariables) {
			t.Errorf("error = %v, want errors.Is(ErrMaxVariables)", err)
		}
	})

	t.Run("max line length", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxLineLength = 8
		_, err := UnmarshalMapWithConfig(cfg, "AAAAAAAAAA=1")
		if err == nil {
			t.Fatal("error = nil, want line-too-long error")
		}
		if !errors.Is(err, ErrLineTooLong) {
			t.Errorf("error = %v, want errors.Is(ErrLineTooLong)", err)
		}
	})
}

func TestUnmarshalMapWithConfig_InvalidConfig(t *testing.T) {
	// Partially-set Config is NOT auto-completed: unset limits stay zero and
	// Validate rejects them — the same contract New enforces.
	cfg := Config{}
	cfg.MaxFileSize = 8 << 20

	_, err := UnmarshalMapWithConfig(cfg, "A=1")
	if err == nil {
		t.Fatal("error = nil, want ErrInvalidConfig")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("error = %v, want errors.Is(ErrInvalidConfig)", err)
	}

	// The struct variant surfaces the same error.
	if err := UnmarshalStructWithConfig(cfg, "A=1", &struct {
		A string `env:"A"`
	}{}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("UnmarshalStructWithConfig error = %v, want errors.Is(ErrInvalidConfig)", err)
	}
}

func TestUnmarshalMapWithConfig_Expansion(t *testing.T) {
	const doc = "A=1\nB=${A}\n"

	// DefaultConfig expands ${A} from the parsed document.
	m, err := UnmarshalMapWithConfig(Config{}, doc)
	if err != nil {
		t.Fatalf("default error = %v", err)
	}
	if m["B"] != "1" {
		t.Errorf("expanded B = %q, want %q", m["B"], "1")
	}

	// ExpandVariables=false keeps the literal reference.
	cfg := DefaultConfig()
	cfg.ExpandVariables = false
	m, err = UnmarshalMapWithConfig(cfg, doc)
	if err != nil {
		t.Fatalf("no-expand error = %v", err)
	}
	if m["B"] != "${A}" {
		t.Errorf("literal B = %q, want %q", m["B"], "${A}")
	}
}

func TestUnmarshalMapWithConfig_NumberRendering(t *testing.T) {
	t.Run("json canonical vs default float formatting", func(t *testing.T) {
		const doc = `{"n": 1e21}`

		// DefaultConfig renders canonical decimal strings.
		m, err := UnmarshalMapWithConfig(Config{}, doc, FormatJSON)
		if err != nil {
			t.Fatalf("default error = %v", err)
		}
		if m["N"] != "1000000000000000000000" {
			t.Errorf("default N = %q, want %q", m["N"], "1000000000000000000000")
		}

		// Opting out switches to fmt %v rendering; numbers are never rejected.
		cfg := DefaultConfig()
		cfg.JSONNumberAsString = false
		m, err = UnmarshalMapWithConfig(cfg, doc, FormatJSON)
		if err != nil {
			t.Fatalf("NumberAsString=false error = %v", err)
		}
		if m["N"] != "1e+21" {
			t.Errorf("NumberAsString=false N = %q, want %q", m["N"], "1e+21")
		}
	})

	t.Run("yaml canonical vs verbatim scalars", func(t *testing.T) {
		const doc = "a: 007\n"

		m, err := UnmarshalMapWithConfig(Config{}, doc, FormatYAML)
		if err != nil {
			t.Fatalf("default error = %v", err)
		}
		if m["A"] != "7" {
			t.Errorf("default A = %q, want %q", m["A"], "7")
		}

		cfg := DefaultConfig()
		cfg.YAMLNumberAsString = false
		m, err = UnmarshalMapWithConfig(cfg, doc, FormatYAML)
		if err != nil {
			t.Fatalf("NumberAsString=false error = %v", err)
		}
		if m["A"] != "007" {
			t.Errorf("NumberAsString=false A = %q, want %q", m["A"], "007")
		}
	})
}

func TestUnmarshalMapWithConfig_Prefix(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Prefix = "APP_"

	m, err := UnmarshalMapWithConfig(cfg, "APP_X=1\nOTHER=2\n")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if _, ok := m["OTHER"]; ok {
		t.Error("OTHER kept, want filtered out by Prefix")
	}
	if m["APP_X"] != "1" {
		t.Errorf("APP_X = %q, want %q", m["APP_X"], "1")
	}
	if len(m) != 1 {
		t.Errorf("len(m) = %d, want 1", len(m))
	}
}

// (TestUnmarshalMapWithConfig_Prefix was removed: the case-insensitive prefix
// filter through the same entry point is pinned — with a lowercase prefix and
// a stronger len==1 assertion — by the "prefix matches case-insensitively"
// row of TestUnmarshalMapWithConfig_Boundaries below.)

func TestUnmarshalStructWithConfig(t *testing.T) {
	type serverConfig struct {
		Host string `env:"SERVER_HOST"`
		Port int    `env:"SERVER_PORT"`
	}

	cfg := DefaultConfig()
	cfg.DuplicateKeyPolicy = DuplicateKeyError

	var sc serverConfig
	if err := UnmarshalStructWithConfig(cfg, "SERVER_HOST=localhost\nSERVER_PORT=8080", &sc); err != nil {
		t.Fatalf("error = %v", err)
	}
	if sc.Host != "localhost" || sc.Port != 8080 {
		t.Errorf("got {%q %d}, want {localhost 8080}", sc.Host, sc.Port)
	}

	// The duplicate-key policy propagates through the struct variant.
	err := UnmarshalStructWithConfig(cfg, "SERVER_HOST=a\nSERVER_HOST=b", &sc)
	if err == nil {
		t.Fatal("error = nil, want duplicate-key error")
	}
	if !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("error = %v, want errors.Is(ErrDuplicateKey)", err)
	}
}

// TestUnmarshalMapWithConfig_Boundaries pins the contract edges unique to the
// config-driven one-shot path: RequiredKeys enforcement, the MaxKeyLength
// key rule, format selection (unknown value, auto-detection fallback), the
// case-insensitive Prefix filter, and empty input (per-format empty-input
// rows live in utils_test.go's TestUnmarshalMap, which drives the identical
// UnmarshalMap → UnmarshalMapWithConfig(DefaultConfig(), …) pipeline).
func TestUnmarshalMapWithConfig_Boundaries(t *testing.T) {
	tests := []struct {
		name    string
		cfgFn   func() Config
		doc     string
		format  FileFormat
		wantErr error // nil: the call must succeed and check must pass
		check   func(t *testing.T, m map[string]string)
	}{
		{
			// validateRequiredKeys runs after parsing in both the .env and
			// the structured parsers.
			name: "required keys missing returns ErrMissingRequired (env)",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.RequiredKeys = []string{"NEEDED"}
				return cfg
			},
			doc: "A=1\n", format: FormatEnv,
			wantErr: ErrMissingRequired,
		},
		{
			name: "required keys missing returns ErrMissingRequired (yaml)",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.RequiredKeys = []string{"NEEDED"}
				return cfg
			},
			doc: "a: 1\n", format: FormatYAML,
			wantErr: ErrMissingRequired,
		},
		{
			name: "required keys present succeeds",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.RequiredKeys = []string{"A"}
				return cfg
			},
			doc: "a: 1\n", format: FormatYAML,
			check: func(t *testing.T, m map[string]string) {
				if m["A"] != "1" {
					t.Errorf("A = %q, want %q", m["A"], "1")
				}
			},
		},
		{
			// The .env parser rejects the key (rule max_length), matching
			// ErrInvalidKey via errors.Is.
			name: "key longer than MaxKeyLength errors (env)",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.MaxKeyLength = 8
				return cfg
			},
			doc: "TOO_LONG_KEY=1\n", format: FormatEnv,
			wantErr: ErrInvalidKey,
		},
		{
			// Structured formats enforce the same limit in validateResult
			// (structured_parser.go).
			name: "key longer than MaxKeyLength errors (json)",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.MaxKeyLength = 8
				return cfg
			},
			doc: `{"TOO_LONG_KEY": 1}`, format: FormatJSON,
			wantErr: ErrInvalidKey,
		},
		{
			// Mirror of LoadFiles: an unknown format value falls back to the
			// .env parser instead of erroring.
			name:  "unknown format value falls back to the env parser",
			cfgFn: DefaultConfig,
			doc:   "A=1\n", format: FileFormat(99),
			check: func(t *testing.T, m map[string]string) {
				if m["A"] != "1" {
					t.Errorf("A = %q, want %q", m["A"], "1")
				}
			},
		},
		{
			// FormatAuto detects from the data: non-JSON/YAML data lands on
			// the .env parser.
			name:  "FormatAuto with dotenv data uses the env parser",
			cfgFn: DefaultConfig,
			doc:   "A=1\nB=two\n", format: FormatAuto,
			check: func(t *testing.T, m map[string]string) {
				if m["A"] != "1" || m["B"] != "two" {
					t.Errorf("m = {A:%q, B:%q}, want {A:%q, B:%q}", m["A"], m["B"], "1", "two")
				}
			},
		},
		{
			// The prefix is uppercased and compared case-insensitively
			// (utils.go, same rule as file loading): a lowercase "app_"
			// prefix keeps APP_X and filters OTHER.
			name: "prefix matches case-insensitively",
			cfgFn: func() Config {
				cfg := DefaultConfig()
				cfg.Prefix = "app_"
				return cfg
			},
			doc: "APP_X=1\nOTHER=2\n", format: FormatEnv,
			check: func(t *testing.T, m map[string]string) {
				if len(m) != 1 || m["APP_X"] != "1" {
					t.Errorf("m = %v, want only {APP_X:1}", m)
				}
			},
		},
		// (The three per-format "empty data returns an empty map" rows were
		// removed: UnmarshalMap delegates to UnmarshalMapWithConfig with
		// DefaultConfig, and utils_test.go's TestUnmarshalMap already runs
		// the identical empty-input pipeline for env/json/yaml.)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := UnmarshalMapWithConfig(tt.cfgFn(), tt.doc, tt.format)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want errors.Is(%v)", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			tt.check(t, m)
		})
	}
}

// TestUnmarshalStructWithConfig_TargetValidation pins the target-shape rules
// the struct variant inherits from UnmarshalInto: nil, non-pointer, and
// pointer-to-non-struct targets are rejected instead of panicking.
func TestUnmarshalStructWithConfig_TargetValidation(t *testing.T) {
	cfg := DefaultConfig()
	type configStruct struct {
		Key string `env:"KEY"`
	}

	tests := []struct {
		name   string
		target any
	}{
		{"nil target", nil},
		{"non-pointer target", configStruct{}},
		{"pointer to non-struct", new(string)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UnmarshalStructWithConfig(cfg, "KEY=value", tt.target); err == nil {
				t.Errorf("UnmarshalStructWithConfig(%s) error = nil, want error", tt.name)
			}
		})
	}
}
