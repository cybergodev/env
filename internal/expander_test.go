package internal

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// imLookup adapts a map into an Expander lookup function, replacing the
// repeated inline closures that duplicated this shape across the expansion
// tests. imLookup(nil) is the "everything unset" lookup.
func imLookup(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

// imNewLineParser builds a LineParser wired with the standard test validator
// (MaxKeyLength 64, MaxValueLength 1024), a no-op auditor, and an expander
// resolving from vars in the given mode. It replaces the four copies of the
// NewValidator+NewAuditor+NewExpander+NewLineParser boilerplate previously
// repeated across parser_test.go and expander_test.go (one package).
func imNewLineParser(vars map[string]string, mode Mode, cfg LineParserConfig) *LineParser {
	v := NewValidator(ValidatorConfig{MaxKeyLength: 64, MaxValueLength: 1024})
	a := NewAuditor(nil, nil, nil, false)
	e := NewExpander(ExpanderConfig{
		MaxDepth: 5,
		Lookup:   imLookup(vars),
		Mode:     mode,
	})
	return NewLineParser(cfg, v, a, e)
}

// TestExpanderExpand is the single Expand-semantics table: the merge of the
// former TestExpanderDefaultValues, TestExpanderQuestionOperator,
// TestExpanderMode, TestExpand_SingleVarPreservesLiteralSuffix and
// TestExpander_DollarEdgeCases.
//
// Every row sets mode explicitly instead of relying on a "zero value means
// ModeAll" default: ModeEnv IS the zero value of Mode, so such a default
// would silently turn every ModeEnv row into a ModeAll row and make the
// ModeEnv pins untestable.
func TestExpanderExpand(t *testing.T) {
	tests := []struct {
		name    string
		mode    Mode
		vars    map[string]string
		input   string
		want    string
		wantErr bool
	}{
		// Plain $VAR / ${VAR} references.
		{name: "no variables", mode: ModeAll, input: "plain text", want: "plain text"},
		{name: "simple variable", mode: ModeAll, vars: map[string]string{"VAR1": "value1"}, input: "$VAR1", want: "value1"},
		{name: "braced variable", mode: ModeAll, vars: map[string]string{"VAR1": "value1"}, input: "${VAR1}", want: "value1"},
		{name: "nested variable", mode: ModeAll, vars: map[string]string{"NESTED": "$VAR1", "VAR1": "value1"}, input: "$NESTED", want: "value1"},
		{name: "undefined variable", mode: ModeAll, input: "$UNDEFINED", want: ""},
		{name: "mixed content", mode: ModeAll, vars: map[string]string{"VAR1": "value1"}, input: "prefix_${VAR1}_suffix", want: "prefix_value1_suffix"},
		{name: "multiple variables", mode: ModeAll, vars: map[string]string{"VAR1": "value1", "VAR2": "value2"}, input: "$VAR1 ${VAR2}", want: "value1 value2"},
		{name: "variable at end", mode: ModeAll, vars: map[string]string{"VAR1": "value1"}, input: "prefix_$VAR1", want: "prefix_value1"},
		{name: "variable in middle", mode: ModeAll, vars: map[string]string{"VAR1": "value1"}, input: "start_${VAR1}_end", want: "start_value1_end"},
		{name: "dollar with non-var char", mode: ModeAll, input: "$!", want: "$!"},
		{name: "empty braces", mode: ModeAll, input: "${}", want: "{}"},
		{name: "unclosed brace", mode: ModeAll, input: "${VAR", want: "${VAR"},
		{name: "invalid key in braces", mode: ModeAll, input: "${123BAD}", want: "${123BAD}"},

		// :- / := default operators.
		{name: "default used when unset", mode: ModeAll, input: "${UNSET:-default}", want: "default"},
		{name: "default not used when set", mode: ModeAll, vars: map[string]string{"VAR": "actual"}, input: "${VAR:-default}", want: "actual"},
		// PIN: an empty string is a valid explicit value and must NOT trigger :-.
		{name: "explicit empty does not trigger default", mode: ModeAll, vars: map[string]string{"EMPTY": ""}, input: "${EMPTY:-default}", want: ""},
		{name: "assign default", mode: ModeAll, input: "${UNSET:=assigned}", want: "assigned"},
		{name: "assign not performed when already set", mode: ModeAll, vars: map[string]string{"VAR": "actual"}, input: "${VAR:=newval}", want: "actual"},
		{name: "simple default without vars", mode: ModeAll, input: "${UNSET:-simple_default}", want: "simple_default"},
		{name: "nested variable in default value", mode: ModeAll, vars: map[string]string{"VAR2": "value2"}, input: "${MISSING:-${VAR2}}", want: "value2"},
		// BOUNDARY: := does not substitute for a set-but-empty variable
		// either — the lookup succeeds, so the default branch is never
		// consulted (PIN, expander.go).
		{name: "assign does not substitute for set-empty", mode: ModeAll, vars: map[string]string{"EMPTY": ""}, input: "${EMPTY:=x}", want: ""},

		// :? required operator.
		{name: "required operator set variable returns value", mode: ModeAll, vars: map[string]string{"REQUIRED": "value"}, input: "${REQUIRED:?Variable is required}", want: "value"},
		{name: "required operator empty value errors", mode: ModeAll, vars: map[string]string{"REQUIRED": ""}, input: "${REQUIRED:?Variable is required}", wantErr: true},
		// BOUNDARY: an empty :? message still produces a required-variable
		// error / success (the message is never echoed — SEC-05).
		{name: "required operator empty message ok when set", mode: ModeAll, vars: map[string]string{"SET": "v"}, input: "${SET:?}", want: "v"},
		{name: "required operator empty message errors when unset", mode: ModeAll, input: "${UNSET:?}", wantErr: true},

		// Modes.
		{name: "ModeNone returns unchanged", mode: ModeNone, vars: map[string]string{"VAR": "value"}, input: "$VAR", want: "$VAR"},
		{name: "ModeEnv expands", mode: ModeEnv, vars: map[string]string{"VAR": "value"}, input: "$VAR", want: "value"},
		{name: "ModeAll expands", mode: ModeAll, vars: map[string]string{"VAR": "value"}, input: "$VAR", want: "value"},
		// Brace operators are part of POSIX-style expansion, so they apply
		// in ModeEnv too; only ModeNone turns expansion off entirely (see
		// the Mode documentation — ModeAll is behaviorally identical).
		{name: "ModeEnv still expands defaults", mode: ModeEnv, input: "${VAR:-def}", want: "def"},

		// Single-variable fast paths must preserve literal suffixes
		// (regression: they previously returned "" for the whole string
		// when the variable was unset, dropping the suffix).
		{name: "unset $VAR with suffix", mode: ModeAll, input: "$FOO bar", want: " bar"},
		{name: "unset ${VAR} with suffix", mode: ModeAll, input: "${FOO} bar", want: " bar"},
		{name: "unset $VAR whole string", mode: ModeAll, input: "$FOO", want: ""},
		{name: "unset ${VAR} whole string", mode: ModeAll, input: "${FOO}", want: ""},
		{name: "invalid key with suffix", mode: ModeAll, input: "${1BAD} tail", want: "${1BAD} tail"},
		{name: "empty braces with suffix", mode: ModeAll, input: "${} tail", want: "{} tail"},
		{name: "set $VAR with suffix", mode: ModeAll, vars: map[string]string{"FOO": "v"}, input: "$FOO bar", want: "v bar"},
		{name: "default with suffix", mode: ModeAll, input: "${BAZ:-def} tail", want: "def tail"},
		{name: "general path consistency", mode: ModeAll, input: "$FOO bar$X", want: " bar"},

		// $$ escapes and stray dollars.
		{name: "escaped dollar", mode: ModeAll, input: "cost: $$5", want: "cost: $5"},
		{name: "lone dollar", mode: ModeAll, input: "$", want: "$"},
		{name: "trailing dollar", mode: ModeAll, input: "value$", want: "value$"},
		{name: "double dollar alone", mode: ModeAll, input: "$$", want: "$"},
		// Moved from the former TestExpanderExpand: proves the $$ escape
		// prevents expansion even when the following name IS set.
		{name: "escaped dollar before name", mode: ModeAll, vars: map[string]string{"VAR1": "value1"}, input: "$$VAR1", want: "$VAR1"},

		// BOUNDARY: nested braced defaults — findClosingBrace depth counting.
		{name: "nested braced default both set", mode: ModeAll, vars: map[string]string{"A": "va", "B": "vb"}, input: "${A:-${B:-x}}", want: "va"},
		{name: "nested braced default outer set inner unset", mode: ModeAll, vars: map[string]string{"A": "va"}, input: "${A:-${B:-x}}", want: "va"},
		{name: "nested braced default outer unset inner set", mode: ModeAll, vars: map[string]string{"B": "vb"}, input: "${A:-${B:-x}}", want: "vb"},
		{name: "nested braced default both unset", mode: ModeAll, input: "${A:-${B:-x}}", want: "x"},
		// PIN: an unbalanced inner brace makes findClosingBrace return -1
		// and the whole reference is emitted verbatim — even when the outer
		// key is set, the reference is never resolved.
		{name: "malformed inner brace returned verbatim", mode: ModeAll, vars: map[string]string{"A": "va"}, input: "${A:-${B}", want: "${A:-${B}"},

		// BOUNDARY: >128 variable references so the estimated capacity
		// (len(s) + 16*varCount) exceeds 2048 and the capped-allocation
		// branch of expandWithDepth runs. The result must be unaffected.
		{name: "capacity estimate capped above 2048", mode: ModeAll, vars: map[string]string{"A": "v"}, input: strings.Repeat("$A ", 130), want: strings.Repeat("v ", 130)},

		// BOUNDARY: unicode. Non-ASCII bytes are not variable-name
		// characters (isVarChar), so $中文 is never a reference even when a
		// key of that name exists (PIN); unicode values pass through.
		{name: "non-ASCII variable name is not a reference", mode: ModeAll, vars: map[string]string{"中文": "x"}, input: "$中文", want: "$中文"},
		{name: "unicode value passes through", mode: ModeAll, vars: map[string]string{"VAR": "值🎉"}, input: "$VAR", want: "值🎉"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := NewExpander(ExpanderConfig{
				MaxDepth: 5,
				Lookup:   imLookup(tt.vars),
				Mode:     tt.mode,
			})

			got, err := exp.Expand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Expand(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestExpander_ErrExpansionDepthMatching verifies that real expansion errors
// classify correctly via errors.Is: depth-limit and cycle errors match
// ErrExpansionDepth, while the ${VAR:?} required-variable error does not
// (it carries ExpansionRequiredKind). Also pins the exact depth boundary:
// a chain of N variables expands its leaf value at depth N, and
// expandWithDepth rejects depth >= maxDepth BEFORE its no-reference fast
// path — so a chain of exactly maxDepth variables errors while a chain of
// maxDepth-1 still succeeds (maxDepth is an exclusive bound).
func TestExpander_ErrExpansionDepthMatching(t *testing.T) {
	tests := []struct {
		name      string
		vars      map[string]string
		maxDepth  int
		input     string
		want      string
		wantErrIs error
	}{
		{
			name:      "depth limit exceeded",
			vars:      map[string]string{"A": "$B", "B": "$C", "C": "$D", "D": "$E", "E": "final"},
			maxDepth:  2,
			input:     "$A",
			wantErrIs: ErrExpansionDepth,
		},
		{
			name:      "cycle matches ErrExpansionDepth",
			vars:      map[string]string{"A": "$B", "B": "$A"},
			maxDepth:  10,
			input:     "$A",
			wantErrIs: ErrExpansionDepth,
		},
		{
			// BOUNDARY (off-by-one): 4 = maxDepth-1 chained variables — the
			// leaf "final" is expanded at depth 4 < 5, so the chain succeeds.
			name:     "chain of maxDepth-1 variables succeeds",
			vars:     map[string]string{"A1": "$A2", "A2": "$A3", "A3": "$A4", "A4": "final"},
			maxDepth: 5,
			input:    "$A1",
			want:     "final",
		},
		{
			// BOUNDARY (off-by-one): 5 = maxDepth chained variables — the
			// leaf reaches expandWithDepth at depth 5 >= 5 and errors even
			// though it contains no references itself.
			name:      "chain of maxDepth variables errors",
			vars:      map[string]string{"A1": "$A2", "A2": "$A3", "A3": "$A4", "A4": "$A5", "A5": "final"},
			maxDepth:  5,
			input:     "$A1",
			wantErrIs: ErrExpansionDepth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := NewExpander(ExpanderConfig{
				MaxDepth: tt.maxDepth,
				Lookup:   imLookup(tt.vars),
				Mode:     ModeAll,
			})
			got, err := exp.Expand(tt.input)
			if tt.wantErrIs != nil {
				if err == nil {
					t.Fatalf("Expand(%q) error = nil, want match for %v", tt.input, tt.wantErrIs)
				}
				if !errors.Is(err, tt.wantErrIs) {
					t.Errorf("Expand(%q) error = %v, want errors.Is(%v)", tt.input, err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Expand(%q) error = %v, want nil", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	// Required-variable error: must NOT match ErrExpansionDepth (inverted
	// direction — asserts the absence of a match — so it is not a table row).
	reqExp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})
	_, reqErr := reqExp.Expand("${REQUIRED:?Variable is required}")
	if reqErr == nil {
		t.Fatal("expected required-variable error")
	}
	if errors.Is(reqErr, ErrExpansionDepth) {
		t.Errorf("required-variable error must NOT match ErrExpansionDepth, got %v", reqErr)
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
		{name: "single-colon ref is not an edge", vars: map[string]string{"A": "${A:x}"}, hasCycle: false},
		{name: "single-colon default across two keys", vars: map[string]string{"URL": "${HOST:localhost:8080}", "HOST": "$URL"}, hasCycle: false},
		{name: "valid operator still closes a cycle", vars: map[string]string{"A": "${A:-x}"}, hasCycle: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := DetectCycle(tt.vars, nil)
			if found != tt.hasCycle {
				t.Errorf("DetectCycle() found = %v, want %v", found, tt.hasCycle)
			}
		})
	}
}

// TestExpandAllInMap_NoFalseCycleFromSingleColon pins the DetectCycle fix
// end to end: a single-colon ${A:x} is NOT a variable reference (the
// expander treats it as an invalid key and emits it verbatim), so it must
// not create a phantom edge that closes a loop and rejects the file.
// DetectCycle previously split at ANY colon — unlike parseBraceOperator,
// which only splits at the valid :-,:=,:? operators.
func TestExpandAllInMap_NoFalseCycleFromSingleColon(t *testing.T) {
	e := NewExpander(ExpanderConfig{
		MaxDepth: 10,
		Lookup:   imLookup(nil),
		Mode:     ModeAll,
	})

	// Direct phantom self-reference: expansion resolves to the literal.
	out, err := e.ExpandAllInMap(map[string]string{"A": "${A:x}"}, nil)
	if err != nil {
		t.Fatalf("ExpandAllInMap error = %v, want nil (single colon is not an operator)", err)
	}
	if got := out["A"]; got != "${A:x}" {
		t.Errorf("A = %q, want the verbatim %q", got, "${A:x}")
	}

	// Phantom edge that used to close a loop across two keys.
	out, err = e.ExpandAllInMap(map[string]string{
		"URL":  "${HOST:localhost:8080}",
		"HOST": "$URL",
	}, nil)
	if err != nil {
		t.Fatalf("ExpandAllInMap error = %v, want nil", err)
	}
	if got := out["HOST"]; got != "${HOST:localhost:8080}" {
		t.Errorf("HOST = %q, want the expanded reference %q", got, "${HOST:localhost:8080}")
	}

	// Control: a REAL cycle through a valid operator is still rejected,
	// now with a message that names the cause instead of "(0/0)".
	_, err = e.ExpandAllInMap(map[string]string{"A": "${A:-x}"}, nil)
	if err == nil {
		t.Fatal("expected a cycle error for ${A:-x}")
	}
	var ee *ExpansionError
	if !errors.As(err, &ee) {
		t.Fatalf("want *ExpansionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("error = %v, want it to name the cycle", err)
	}
}

func TestExpanderConfig(t *testing.T) {
	lookup := imLookup(map[string]string{"VAR": "value"})

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

// imExpandAllCase is one row of the ExpandAll case table shared by the three
// entry points: Expander.ExpandAllInMap directly, LineParser.ExpandAll with
// the concrete *Expander, and LineParser.ExpandAll with a non-*Expander
// LineExpander (the interface fallback).
type imExpandAllCase struct {
	name     string
	mode     Mode
	external map[string]string // resolved via the expander's lookup, not the map
	vars     map[string]string // the map being expanded
	expected map[string]string
	wantErr  bool
	// skipInterface marks cases that need *Expander semantics the generic
	// interface fallback does not implement: the ModeNone gate and cycle
	// detection (expandAllUsingInterface has neither).
	skipInterface bool
}

// imExpandAllCases is the shared ExpandAll case table (one package-level var
// consumed by the three thin wrappers in expander_test.go and parser_test.go).
var imExpandAllCases = []imExpandAllCase{
	{
		name:          "ModeNone returns original",
		mode:          ModeNone,
		vars:          map[string]string{"A": "$B"},
		expected:      map[string]string{"A": "$B"},
		skipInterface: true,
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
		name:     "external lookup resolves references missing from the map",
		mode:     ModeAll,
		external: map[string]string{"BASE": "value"},
		vars:     map[string]string{"KEY": "$BASE"},
		expected: map[string]string{"KEY": "value"},
	},
	{
		name:          "cycle detected",
		mode:          ModeAll,
		vars:          map[string]string{"A": "$B", "B": "$A"},
		wantErr:       true,
		skipInterface: true,
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

// TestExpandAllInMap runs the shared ExpandAll case table against
// Expander.ExpandAllInMap directly.
func TestExpandAllInMap(t *testing.T) {
	for _, tt := range imExpandAllCases {
		t.Run(tt.name, func(t *testing.T) {
			exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(tt.external), Mode: tt.mode})
			result, err := exp.ExpandAllInMap(tt.vars, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExpandAllInMap() error = %v, wantErr %v", err, tt.wantErr)
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

// TestExpandAllInMap_FastPathIdentity pins that a map containing no '$' in
// any value returns the ORIGINAL map object (pointer identity, not a copy),
// and that ModeNone does the same without even scanning — the documented
// allocation-avoiding fast path of ExpandAllInMap.
func TestExpandAllInMap_FastPathIdentity(t *testing.T) {
	// Maps are not comparable with ==; identity is asserted via the map
	// header pointer (same object → same pointer).
	for _, mode := range []Mode{ModeEnv, ModeAll} {
		vars := map[string]string{"A": "plain", "B": "text"}
		exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: mode})
		result, err := exp.ExpandAllInMap(vars, nil)
		if err != nil {
			t.Fatalf("mode %v: ExpandAllInMap() error = %v", mode, err)
		}
		if reflect.ValueOf(result).Pointer() != reflect.ValueOf(vars).Pointer() {
			t.Errorf("mode %v: ExpandAllInMap() returned a new map, want the original object", mode)
		}
	}

	none := map[string]string{"A": "$B"}
	exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeNone})
	result, err := exp.ExpandAllInMap(none, nil)
	if err != nil {
		t.Fatalf("ModeNone: ExpandAllInMap() error = %v", err)
	}
	if reflect.ValueOf(result).Pointer() != reflect.ValueOf(none).Pointer() {
		t.Errorf("ModeNone: ExpandAllInMap() returned a new map, want the original object")
	}
}

// TestExpandAllInMap_AssignOperator pins the POSIX-style ':=' semantics of
// ExpandAllInMap (GEN-001): on an unset variable the expanded default is
// assigned — visible to references in keys sorted later in the pass and
// materialized in the returned map. Set variables (map or external) are
// never reassigned, competing defaults resolve deterministically by sorted
// key order, and a standalone Expander degrades ':=' to ':-'.
func TestExpandAllInMap_AssignOperator(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]string
		external map[string]string
		expected map[string]string
	}{
		{
			name: "assignment visible to later reference and materialized",
			vars: map[string]string{
				"A": "${V:=x}",
				"B": "${V}",
			},
			external: nil,
			expected: map[string]string{"A": "x", "B": "x", "V": "x"},
		},
		{
			name:     "variable set in the map is not reassigned",
			vars:     map[string]string{"A": "${V:=x}", "V": "5"},
			external: nil,
			expected: map[string]string{"A": "5", "V": "5"},
		},
		{
			name:     "variable set externally is not reassigned",
			vars:     map[string]string{"A": "${V:=x}"},
			external: map[string]string{"V": "env"},
			expected: map[string]string{"A": "env"},
		},
		{
			name: "competing defaults resolve by sorted key order",
			vars: map[string]string{
				"A": "${V:=1}",
				"B": "${V:=2}",
			},
			external: nil,
			expected: map[string]string{"A": "1", "B": "1", "V": "1"},
		},
		{
			name:     "default is expanded before assignment",
			vars:     map[string]string{"A": "${V:=${W:-d}}", "W": "w"},
			external: nil,
			expected: map[string]string{"A": "w", "V": "w", "W": "w"},
		},
		{
			name:     "suffix after assignment still resolves",
			vars:     map[string]string{"A": "${V:=x}/tail"},
			external: nil,
			expected: map[string]string{"A": "x/tail", "V": "x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(tt.external), Mode: ModeAll})
			result, err := exp.ExpandAllInMap(tt.vars, nil)
			if err != nil {
				t.Fatalf("ExpandAllInMap() error = %v", err)
			}
			if len(result) != len(tt.expected) {
				t.Errorf("len(result) = %d, want %d (result: %v)", len(result), len(tt.expected), result)
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("ExpandAllInMap()[%q] = %q, want %q", k, result[k], v)
				}
			}
		})
	}

	// Standalone Expand has no map context: ':=' degrades to ':-'.
	exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})
	got, err := exp.Expand("${V:=x}")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if got != "x" {
		t.Errorf("Expand() = %q, want %q", got, "x")
	}
}

// TestExpandAllInMap_NoFalseCycleFromDefaultText pins the DetectCycle fix for
// references inside operator default text: B=x / A=${B:-$C} / C=$A expands
// cleanly (A=x, C=x). The $C in the default is conditional, but the scan
// previously kept walking inside the braces and turned it into a hard edge,
// rejecting valid files as cycles.
func TestExpandAllInMap_NoFalseCycleFromDefaultText(t *testing.T) {
	e := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})

	out, err := e.ExpandAllInMap(map[string]string{
		"B": "x",
		"A": "${B:-$C}",
		"C": "$A",
	}, nil)
	if err != nil {
		t.Fatalf("ExpandAllInMap error = %v, want nil (default text is conditional)", err)
	}
	if got := out["A"]; got != "x" {
		t.Errorf("A = %q, want %q", got, "x")
	}
	if got := out["C"]; got != "x" {
		t.Errorf("C = %q, want %q", got, "x")
	}
}

// TestExpandAllInMap_NoExpandSet pins the literal (single-quoted) exemption:
// exempt values pass through untouched yet still answer lookups, and the text
// they contribute is never rescanned for further references.
func TestExpandAllInMap_NoExpandSet(t *testing.T) {
	e := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})

	out, err := e.ExpandAllInMap(map[string]string{
		"PW":  "my$ecret",
		"REF": "$PW",
	}, map[string]struct{}{"PW": {}})
	if err != nil {
		t.Fatalf("ExpandAllInMap error = %v, want nil", err)
	}
	if got := out["PW"]; got != "my$ecret" {
		t.Errorf("PW = %q, want literal %q", got, "my$ecret")
	}
	if got := out["REF"]; got != "my$ecret" {
		t.Errorf("REF = %q, want %q inserted literally", got, "my$ecret")
	}
}

// TestLineParserExpandAll runs the shared ExpandAll case table through
// LineParser.ExpandAll with the concrete *Expander; error rows additionally
// assert the error is a typed *ExpansionError (the cycle-detection branch).
func TestLineParserExpandAll(t *testing.T) {
	for _, tt := range imExpandAllCases {
		t.Run(tt.name, func(t *testing.T) {
			lp := imNewLineParser(tt.external, tt.mode, LineParserConfig{})
			result, err := lp.ExpandAll(tt.vars, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExpandAll() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var expErr *ExpansionError
				if !errors.As(err, &expErr) {
					t.Errorf("error type = %T, want *ExpansionError", err)
				}
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("ExpandAll()[%q] = %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

// TestBuildChain_MasksSensitiveKeys verifies that sensitive keys are masked
// in the expansion chain for error messages.
func TestBuildChain_MasksSensitiveKeys(t *testing.T) {
	exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})

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
			shouldContain:    "[MASKED]",
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
			shouldContain:    "[MASKED]",
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

// TestExpander_RequiredOperator pins the ${VAR:?msg} semantics: the set case
// must recursively expand the value like the plain ${VAR} path (regression:
// the branch previously returned the raw value, so with HOST=$BASE the
// reference ${HOST:?required} expanded to the literal "$BASE"), and mutually
// required variables (A="${B:?}", B="${A:?}") surface a cycle error instead
// of recursing until the depth limit.
func TestExpander_RequiredOperator(t *testing.T) {
	t.Run("set value is recursively expanded", func(t *testing.T) {
		lookup := imLookup(map[string]string{"HOST": "$BASE", "BASE": "example.com"})
		exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: lookup, Mode: ModeAll})

		// General path: reference with a literal suffix.
		got, err := exp.Expand("${HOST:?required}/x")
		if err != nil {
			t.Fatalf("Expand() error = %v", err)
		}
		if got != "example.com/x" {
			t.Errorf("Expand(${HOST:?required}/x) = %q, want %q", got, "example.com/x")
		}

		// Fast path: the reference is the entire input.
		got, err = exp.Expand("${HOST:?required}")
		if err != nil {
			t.Fatalf("Expand() error = %v", err)
		}
		if got != "example.com" {
			t.Errorf("Expand(${HOST:?required}) = %q, want %q", got, "example.com")
		}

		// Plain ${VAR} path for parity.
		got, err = exp.Expand("${HOST}/x")
		if err != nil {
			t.Fatalf("Expand() error = %v", err)
		}
		if got != "example.com/x" {
			t.Errorf("Expand(${HOST}/x) = %q, want %q", got, "example.com/x")
		}
	})

	t.Run("unset value surfaces a required error mid-string", func(t *testing.T) {
		exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})
		if _, err := exp.Expand("prefix_${MISSING:?req}"); err == nil {
			t.Error("expected required-variable error for a mid-string ${MISSING:?}, got nil")
		}
	})

	t.Run("mutually required variables surface a cycle", func(t *testing.T) {
		lookup := imLookup(map[string]string{"A": "${B:?}", "B": "${A:?}"})
		exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: lookup, Mode: ModeAll})
		if _, err := exp.Expand("${A:?required}"); err == nil {
			t.Error("expected cycle error for mutually-required variables, got nil")
		}
	})
}

// TestExpander_LiteralInsertion pins the literal (single-quoted source)
// insertion branches through ExpandAllInMap — the only expander whose
// literal set is non-nil — across every reference shape: braced and simple
// general paths, the required operator's literal arm, and the single-var
// fast paths with and without a suffix. A literal's '$' runs are data the
// expander never follows.
func TestExpander_LiteralInsertion(t *testing.T) {
	exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})
	noExpand := map[string]struct{}{"PW": {}}
	const literalValue = "$Literal $TEXT"

	vars := map[string]string{
		"PW": literalValue, // the literal itself: inserted verbatim
		"K1": "x${PW}y",    // braced general path
		"K2": "x$PW y",     // simple-variable general path
		"K3": "${PW:?req}", // required operator, set literal value
		"K4": "$PW tail",   // single-var fast path with suffix
		"K5": "$PW",        // single-var fast path, whole string
	}
	got, err := exp.ExpandAllInMap(vars, noExpand)
	if err != nil {
		t.Fatalf("ExpandAllInMap() error = %v", err)
	}

	want := map[string]string{
		"PW": literalValue,
		"K1": "x" + literalValue + "y",
		"K2": "x" + literalValue + " y",
		"K3": literalValue,
		"K4": literalValue + " tail",
		"K5": literalValue,
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("ExpandAllInMap() %s = %q, want %q (literal inserted verbatim)", key, got[key], w)
		}
	}
}

// TestExpander_EscapeAndSuffixBoundaries collects the remaining expansion
// boundaries: the escaped-dollar general path mid-string, the fast-path
// value-with-references-plus-suffix fallback, the MaxValueLength clamp in
// NewExpander, and the expansion pools' defensive branches.
func TestExpander_EscapeAndSuffixBoundaries(t *testing.T) {
	t.Run("escaped dollar mid-string collapses to one dollar", func(t *testing.T) {
		exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(map[string]string{"VAR": "value1"}), Mode: ModeAll})
		got, err := exp.Expand("cost: $$5 $VAR")
		if err != nil {
			t.Fatalf("Expand() error = %v", err)
		}
		if got != "cost: $5 value1" {
			t.Errorf("Expand() = %q, want %q", got, "cost: $5 value1")
		}
	})

	t.Run("referencing value with a suffix re-expands the whole input", func(t *testing.T) {
		// $FOO x: FOO's value contains a reference, and the input has a
		// suffix, so the fast path falls back to full expansion with a
		// cleared visited map.
		exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(map[string]string{"FOO": "${BAR}", "BAR": "v"}), Mode: ModeAll})
		got, err := exp.Expand("$FOO x")
		if err != nil {
			t.Fatalf("Expand() error = %v", err)
		}
		if got != "v x" {
			t.Errorf("Expand() = %q, want %q", got, "v x")
		}
	})

	t.Run("MaxValueLength is clamped to the hard maximum", func(t *testing.T) {
		exp := NewExpander(ExpanderConfig{MaxValueLength: HardMaxValueLength + 1})
		if exp.maxValueLen != HardMaxValueLength {
			t.Errorf("maxValueLen = %d, want %d (clamped to HardMaxValueLength)", exp.maxValueLen, HardMaxValueLength)
		}
	})

	t.Run("pool defensive branches", func(t *testing.T) {
		// nil returns are no-ops; poisoned pools fall back to fresh maps.
		putVisitedMap(nil)
		putLookupCache(nil)
		for i := 0; i < 4; i++ {
			visitedMapPool.Put(new(int))
			lookupCachePool.Put(new(int))
		}
		if m := getVisitedMap(); m == nil {
			t.Error("getVisitedMap() = nil for poisoned pool, want a fresh map")
		} else {
			putVisitedMap(m)
		}
		if c := getLookupCache(); c == nil {
			t.Error("getLookupCache() = nil for poisoned pool, want a fresh map")
		} else {
			putLookupCache(c)
		}
	})
}

// TestDetectCycle_DeepChainNoCycle pins DetectCycle's recursion guard: an
// acyclic chain longer than the 1000-deep DFS cap reports no cycle (the
// expansion depth limit catches real problems during expansion) instead of
// overflowing the stack.
func TestDetectCycle_DeepChainNoCycle(t *testing.T) {
	vars := make(map[string]string, 1100)
	for i := 1; i < 1100; i++ {
		vars[fmt.Sprintf("A%d", i)] = "$" + fmt.Sprintf("A%d", i+1)
	}
	vars["A1100"] = "x"

	if key, found := DetectCycle(vars, nil); found {
		t.Errorf("DetectCycle(1100-link acyclic chain) = (%q, true), want no cycle", key)
	}
}

// TestExpander_AmplificationBounded pins the GEN-001 fix: a multi-reference
// DAG (V1=${V2}.${V2}) doubles on every level without ever cycling, so the
// depth and cycle checks cannot see it. The builder inside expandWithDepth is
// bounded by ExpanderConfig.MaxValueLength (clamped to at least
// DefaultMaxValueLength) and aborts the moment it crosses the limit, instead
// of materializing the 2^depth amplification first.
func TestExpander_AmplificationBounded(t *testing.T) {
	// 1024-byte seed; each level doubles plus one separator byte:
	// A=1024, B=2049, C=4099, D=8199 — D crosses the 8192 default limit at
	// depth 4, well below the depth cap, so only the length bound can stop it.
	seed := strings.Repeat("x", 1024)
	vars := map[string]string{
		"A": seed,
		"B": "${A}.${A}",
		"C": "${B}.${B}",
		"D": "${C}.${C}",
	}
	exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(vars), Mode: ModeAll})

	_, err := exp.Expand("$D")
	if err == nil {
		t.Fatal("Expand($D) error = nil, want max-length violation (D expands to 8199 > 8192)")
	}
	var vErr *ValidationError
	if !errors.As(err, &vErr) || vErr.Rule != "max_length" {
		t.Errorf("Expand($D) error = %v, want *ValidationError with rule max_length", err)
	}

	// C stays under the bound and must keep expanding normally.
	got, err := exp.Expand("$C")
	if err != nil {
		t.Fatalf("Expand($C) error = %v, want nil (4099 bytes is under the 8192 limit)", err)
	}
	if len(got) != 4099 {
		t.Errorf("len(Expand($C)) = %d, want 4099", len(got))
	}
}

// TestExpander_ExpandedValueAtLimitPasses pins the boundary: an expanded value
// of exactly MaxValueLength bytes is allowed (the check is strictly
// greater-than, mirroring the raw-value boundary the validator enforces).
func TestExpander_ExpandedValueAtLimitPasses(t *testing.T) {
	// 4096 + 4096 = 8192 == DefaultMaxValueLength exactly.
	vars := map[string]string{"A": strings.Repeat("x", 4096), "B": "${A}${A}"}
	exp := NewExpander(ExpanderConfig{Lookup: imLookup(vars), Mode: ModeAll})

	got, err := exp.Expand("$B")
	if err != nil {
		t.Fatalf("Expand($B) error = %v, want nil (exactly at the limit)", err)
	}
	if len(got) != DefaultMaxValueLength {
		t.Errorf("len(Expand($B)) = %d, want %d", len(got), DefaultMaxValueLength)
	}
}

// TestExpandAllInMap_FastPathBounded pins the MaxValueLength bound on the
// single-variable fast paths (boundValue): an external lookup value larger
// than the limit must error exactly like the general path. KEY=$BIG
// previously returned the oversized value unchecked while KEY=$BIG x errored.
func TestExpandAllInMap_FastPathBounded(t *testing.T) {
	big := strings.Repeat("x", 100)
	lookups := imLookup(map[string]string{"BIG": big, "OK": "fine"})
	e := NewExpander(ExpanderConfig{MaxDepth: 5, MaxValueLength: 32, Lookup: lookups, Mode: ModeAll})

	for _, expr := range []string{"$BIG", "${BIG}"} {
		out, err := e.ExpandAllInMap(map[string]string{"K": expr}, nil)
		if err == nil {
			t.Errorf("ExpandAllInMap(%q) succeeded with %d-byte value, want max-length error", expr, len(out["K"]))
		}
	}
	// Control: within-limit values still pass through both fast paths.
	out, err := e.ExpandAllInMap(map[string]string{"K": "$OK"}, nil)
	if err != nil {
		t.Fatalf("ExpandAllInMap($OK) error = %v", err)
	}
	if out["K"] != "fine" {
		t.Errorf("K = %q, want %q", out["K"], "fine")
	}
}

// TestExpandAllInMap_LiteralReferencedBackward pins the dfs guard inside
// DetectCycle: a literal value referenced from a value that is itself
// referenced back (A literal "$B", B=$A) is NOT a cycle — B expands to A's
// literal text and stops. The outer-loop skip alone was not enough; dfs(B)
// scanned A's literal '$' run when reached through the reference and closed
// a false cycle that rejected the whole file.
// TestExpandAllInMap_AssignHardLimit pins the GEN-001 retrospective fix:
// ':=' assignments create variables beyond the parsed set and are bounded
// by HardMaxVariables — a crafted document cannot bypass the parse-time
// MaxVariables check through many small assignment references.
func TestExpandAllInMap_AssignHardLimit(t *testing.T) {
	const perKey = 25
	nKeys := (HardMaxVariables+500)/perKey + 1
	vars := make(map[string]string, nKeys)
	for k := 0; k < nKeys; k++ {
		var b strings.Builder
		for i := 0; i < perKey; i++ {
			fmt.Fprintf(&b, "${K%d_%d:=x} ", k, i)
		}
		vars[fmt.Sprintf("SRC%d", k)] = b.String()
	}

	exp := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})
	_, err := exp.ExpandAllInMap(vars, nil)
	if err == nil {
		t.Fatalf("ExpandAllInMap() = nil error, want the hard variable limit (assignments: %d)", nKeys*perKey)
	}
	if !strings.Contains(err.Error(), "hard variable limit") {
		t.Errorf("error = %v, want it to name the hard variable limit", err)
	}
}

func TestExpandAllInMap_LiteralReferencedBackward(t *testing.T) {
	e := NewExpander(ExpanderConfig{MaxDepth: 5, Lookup: imLookup(nil), Mode: ModeAll})

	out, err := e.ExpandAllInMap(map[string]string{
		"A": "$B",
		"B": "$A", // literal in the source (single-quoted); quotes stripped at parse
	}, map[string]struct{}{"B": {}})
	if err != nil {
		t.Fatalf("ExpandAllInMap error = %v, want nil (literal breaks the loop)", err)
	}
	if got := out["A"]; got != "$A" {
		t.Errorf("A = %q, want %q inserted literally", got, "$A")
	}
	if got := out["B"]; got != "$A" {
		t.Errorf("B = %q, want literal %q", got, "$A")
	}
}
