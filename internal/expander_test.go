package internal

import (
	"regexp"
	"strings"
	"testing"
)

func TestExpanderExpand(t *testing.T) {
	lookup := func(key string) (string, bool) {
		vars := map[string]string{
			"VAR1":   "value1",
			"VAR2":   "value2",
			"NESTED": "$VAR1",
		}
		v, ok := vars[key]
		return v, ok
	}

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{name: "no variables", input: "plain text", expected: "plain text"},
		{name: "simple variable", input: "$VAR1", expected: "value1"},
		{name: "braced variable", input: "${VAR1}", expected: "value1"},
		{name: "nested variable", input: "$NESTED", expected: "value1"},
		{name: "undefined variable", input: "$UNDEFINED", expected: ""},
		{name: "escaped dollar", input: "$$VAR1", expected: "$VAR1"},
		{name: "escaped dollar only", input: "$$", expected: "$"},
		{name: "mixed content", input: "prefix_${VAR1}_suffix", expected: "prefix_value1_suffix"},
		{name: "multiple variables", input: "$VAR1 ${VAR2}", expected: "value1 value2"},
		{name: "variable at end", input: "prefix_$VAR1", expected: "prefix_value1"},
		{name: "variable in middle", input: "start_${VAR1}_end", expected: "start_value1_end"},
		{name: "dollar at end of string", input: "text$", expected: "text$"},
		{name: "dollar with non-var char", input: "$!", expected: "$!"},
		{name: "empty braces", input: "${}", expected: "{}"},
		{name: "unclosed brace", input: "${VAR", expected: "${VAR"},
		{name: "invalid key in braces", input: "${123BAD}", expected: "${123BAD}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := NewExpander(ExpanderConfig{
				MaxDepth: 5,
				Lookup:   lookup,
				Mode:     ModeAll,
			})

			result, err := exp.Expand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Expand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("Expand() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExpanderDefaultValues(t *testing.T) {
	lookup := func(key string) (string, bool) {
		vars := map[string]string{
			"VAR": "actual",
		}
		v, ok := vars[key]
		return v, ok
	}

	exp := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Lookup:   lookup,
		Mode:     ModeAll,
	})

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{name: "default used when unset", input: "${UNSET:-default}", expected: "default"},
		{name: "default not used when set", input: "${VAR:-default}", expected: "actual"},
		{name: "assign default", input: "${UNSET:=assigned}", expected: "assigned"},
		{name: "simple default without vars", input: "${UNSET:-simple_default}", expected: "simple_default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := exp.Expand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Expand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("Expand() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExpanderQuestionOperator(t *testing.T) {
	tests := []struct {
		name    string
		lookup  func(string) (string, bool)
		input   string
		want    string
		wantErr bool
	}{
		{
			name:   "unset variable errors",
			lookup: func(string) (string, bool) { return "", false },
			input:  "${REQUIRED:?Variable is required}",
			wantErr: true,
		},
		{
			name:   "set variable returns value",
			lookup: func(string) (string, bool) { return "value", true },
			input:  "${REQUIRED:?Variable is required}",
			want:   "value",
		},
		{
			name:   "empty value errors",
			lookup: func(string) (string, bool) { return "", true },
			input:  "${REQUIRED:?Variable is required}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := NewExpander(ExpanderConfig{
				MaxDepth: 5,
				Lookup:   tt.lookup,
				Mode:     ModeAll,
			})

			result, err := exp.Expand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Expand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.want {
				t.Errorf("Expand() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestExpanderDepthLimit(t *testing.T) {
	lookup := func(key string) (string, bool) {
		vars := map[string]string{
			"A": "$B", "B": "$C", "C": "$D",
			"D": "$E", "E": "$F", "F": "$G", "G": "final",
		}
		v, ok := vars[key]
		return v, ok
	}

	exp := NewExpander(ExpanderConfig{MaxDepth: 3, Lookup: lookup, Mode: ModeAll})
	_, err := exp.Expand("$A")
	if err == nil {
		t.Error("expected depth limit error")
	}
}

func TestExpanderCycleDetection(t *testing.T) {
	lookup := func(key string) (string, bool) {
		vars := map[string]string{"A": "$B", "B": "$A"}
		v, ok := vars[key]
		return v, ok
	}

	exp := NewExpander(ExpanderConfig{MaxDepth: 10, Lookup: lookup, Mode: ModeAll})
	_, err := exp.Expand("$A")
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestDetectCycle(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]string
		hasCycle bool
	}{
		{name: "empty map", vars: map[string]string{}, hasCycle: false},
		{name: "no cycle", vars: map[string]string{"A": "value", "B": "$A"}, hasCycle: false},
		{name: "direct cycle", vars: map[string]string{"A": "$A"}, hasCycle: true},
		{name: "indirect cycle", vars: map[string]string{"A": "$B", "B": "$C", "C": "$A"}, hasCycle: true},
		{name: "self reference", vars: map[string]string{"A": "$A"}, hasCycle: true},
		{name: "complex nesting no cycle", vars: map[string]string{"A": "${B}", "B": "$C", "C": "value"}, hasCycle: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := DetectCycle(tt.vars)
			if found != tt.hasCycle {
				t.Errorf("DetectCycle() found = %v, want %v", found, tt.hasCycle)
			}
		})
	}
}

func TestExpanderMode(t *testing.T) {
	lookup := func(key string) (string, bool) { return "value", true }

	tests := []struct {
		name     string
		mode     Mode
		input    string
		expected string
	}{
		{name: "ModeNone returns unchanged", mode: ModeNone, input: "$VAR", expected: "$VAR"},
		{name: "ModeEnv expands", mode: ModeEnv, input: "$VAR", expected: "value"},
		{name: "ModeAll expands", mode: ModeAll, input: "$VAR", expected: "value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := NewExpander(ExpanderConfig{
				MaxDepth: 5,
				Lookup:   lookup,
				Mode:     tt.mode,
			})
			result, err := exp.Expand(tt.input)
			if err != nil {
				t.Errorf("Expand() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("Expand() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExpanderConfig(t *testing.T) {
	lookup := func(key string) (string, bool) { return "value", true }

	t.Run("nil lookup", func(t *testing.T) {
		exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: nil, Mode: ModeAll})
		result, err := exp.Expand("$VAR")
		if err != nil {
			t.Errorf("Expand() error = %v", err)
		}
		if result != "" {
			t.Errorf("Expand() = %q, want empty", result)
		}
	})

	t.Run("zero max depth uses default", func(t *testing.T) {
		exp := NewExpander(ExpanderConfig{MaxDepth: 0, Lookup: lookup, Mode: ModeAll})
		result, err := exp.Expand("$VAR")
		if err != nil {
			t.Errorf("Expand() error = %v", err)
		}
		if result != "value" {
			t.Errorf("Expand() = %q, want \"value\"", result)
		}
	})

	t.Run("hard max depth cap", func(t *testing.T) {
		exp := NewExpander(ExpanderConfig{MaxDepth: HardMaxExpansionDepth + 1000, Lookup: lookup, Mode: ModeAll})
		if exp.maxDepth > HardMaxExpansionDepth {
			t.Errorf("maxDepth = %d, should be capped at %d", exp.maxDepth, HardMaxExpansionDepth)
		}
	})

	t.Run("custom key pattern", func(t *testing.T) {
		customLookup := func(key string) (string, bool) {
			if key == "my.custom.key" {
				return "value", true
			}
			return "", false
		}
		exp := NewExpander(ExpanderConfig{
			MaxDepth:   5,
			Lookup:     customLookup,
			Mode:       ModeAll,
			KeyPattern: regexp.MustCompile(`^[a-z][a-z0-9.]*$`),
		})
		result, err := exp.Expand("${my.custom.key}")
		if err != nil {
			t.Errorf("Expand() error = %v", err)
		}
		if result != "value" {
			t.Errorf("Expand() = %q, want \"value\"", result)
		}
	})
}

func TestExpansionError_Message(t *testing.T) {
	err := &ExpansionError{
		Key:   "TEST_KEY",
		Depth: 10,
		Limit: 5,
		Chain: "A -> B -> C",
	}

	msg := err.Error()
	if msg == "" {
		t.Error("Error() should return non-empty message")
	}
	if !strings.Contains(msg, "depth") || !strings.Contains(msg, "limit") {
		t.Errorf("Error message should contain depth and limit info, got: %s", msg)
	}
}

func TestLineParserExpandAll(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxKeyLength: 64, MaxValueLength: 1024})
	a := NewAuditor(nil, nil, nil, false)
	e := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Lookup: func(key string) (string, bool) {
			vars := map[string]string{"BASE": "value"}
			v, ok := vars[key]
			return v, ok
		},
		Mode: ModeAll,
	})

	lp := NewLineParser(LineParserConfig{ExpandVariables: true}, v, a, e)
	result, err := lp.ExpandAll(map[string]string{"KEY": "$BASE"})
	if err != nil {
		t.Errorf("ExpandAll() error = %v", err)
	}
	if result["KEY"] != "value" {
		t.Errorf("ExpandAll() = %v, want KEY=value", result)
	}
}

func TestExpandAllInMap(t *testing.T) {
	tests := []struct {
		name     string
		mode     Mode
		vars     map[string]string
		expected map[string]string
		wantErr  bool
	}{
		{
			name:     "ModeNone returns original",
			mode:     ModeNone,
			vars:     map[string]string{"A": "$B"},
			expected: map[string]string{"A": "$B"},
		},
		{
			name:     "no expansion needed",
			mode:     ModeAll,
			vars:     map[string]string{"A": "plain", "B": "text"},
			expected: map[string]string{"A": "plain", "B": "text"},
		},
		{
			name:     "expands variables",
			mode:     ModeAll,
			vars:     map[string]string{"BASE": "val", "REF": "$BASE"},
			expected: map[string]string{"BASE": "val", "REF": "val"},
		},
		{
			name:    "cycle detected",
			mode:    ModeAll,
			vars:    map[string]string{"A": "$B", "B": "$A"},
			wantErr: true,
		},
		{
			name:     "empty map",
			mode:     ModeAll,
			vars:     map[string]string{},
			expected: map[string]string{},
		},
		{
			name:     "cross-reference with braced",
			mode:     ModeAll,
			vars:     map[string]string{"HOST": "localhost", "URL": "http://${HOST}:8080"},
			expected: map[string]string{"HOST": "localhost", "URL": "http://localhost:8080"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) { return "", false }
			exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: lookup, Mode: tt.mode})
			result, err := exp.ExpandAllInMap(tt.vars)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExpandAllInMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("ExpandAllInMap()[%q] = %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

// TestBuildChain_MasksSensitiveKeys verifies that sensitive keys are masked
// in the expansion chain for error messages.
func TestBuildChain_MasksSensitiveKeys(t *testing.T) {
	lookup := func(key string) (string, bool) { return "", false }

	exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: lookup, Mode: ModeAll})

	tests := []struct {
		name             string
		visited          map[string]bool
		shouldContain    string
		shouldNotContain string
	}{
		{
			name:             "sensitive key PASSWORD is masked",
			visited:          map[string]bool{"HOST": true, "DB_PASSWORD": true, "PORT": true},
			shouldContain:    "HOST",
			shouldNotContain: "DB_PASSWORD",
		},
		{
			name:             "sensitive key SECRET is masked",
			visited:          map[string]bool{"APP_SECRET": true, "TOKEN": true},
			shouldContain:    "AP***",
			shouldNotContain: "APP_SECRET",
		},
		{
			name:             "non-sensitive keys are not masked",
			visited:          map[string]bool{"VAR1": true, "VAR2": true},
			shouldContain:    "VAR1",
			shouldNotContain: "",
		},
		{
			name:             "empty visited returns empty",
			visited:          map[string]bool{},
			shouldContain:    "",
			shouldNotContain: "anything",
		},
		{
			name:             "API_KEY is masked (sensitive)",
			visited:          map[string]bool{"API_KEY": true},
			shouldContain:    "AP***",
			shouldNotContain: "API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := exp.buildChain(tt.visited)
			if tt.shouldContain != "" && !strings.Contains(chain, tt.shouldContain) {
				t.Errorf("buildChain() = %q, should contain %q", chain, tt.shouldContain)
			}
			if tt.shouldNotContain != "" && strings.Contains(chain, tt.shouldNotContain) {
				t.Errorf("buildChain() = %q, should NOT contain %q", chain, tt.shouldNotContain)
			}
		})
	}
}
