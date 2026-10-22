package internal

import (
	"strings"
	"testing"
)

// isResolutionCases is the shared scenario matrix for the key-resolution
// pipeline (exact / lowercase / literal-dotted / dot-to-underscore /
// mixed-case dotted / multi-level / missing). TestLookupInMap and
// TestResolveKeyName both run every row: LookupInMap's data map is generated
// from wantName, so the two entry points stay pinned to identical
// storage-key outcomes.
var isResolutionCases = []struct {
	key       string
	wantName  string
	wantFound bool
}{
	{key: "DEEPSEEK_KEY", wantName: "DEEPSEEK_KEY", wantFound: true},                             // exact match
	{key: "deepseek_key", wantName: "DEEPSEEK_KEY", wantFound: true},                             // lowercase fallback
	{key: "apnews.com", wantName: "APNEWS.COM", wantFound: true},                                 // literal dotted key
	{key: "database.host", wantName: "DATABASE_HOST", wantFound: true},                           // dot to underscore
	{key: "a.b.c", wantName: "A_B_C", wantFound: true},                                           // mixed-case dotted
	{key: "service.cors.allow_origins", wantName: "SERVICE_CORS_ALLOW_ORIGINS", wantFound: true}, // multi-level
	{key: "MISSING", wantName: "", wantFound: false},                                             // missing simple key
	{key: "unknown.path", wantName: "", wantFound: false},                                        // missing dotted key
}

func TestLookupInMap(t *testing.T) {
	// Shared pipeline matrix: the map holds exactly wantName (plus one
	// unrelated key), so the literal, uppercase and dot-notation candidates
	// are all exercised for every scenario.
	for _, tc := range isResolutionCases {
		t.Run(tc.key, func(t *testing.T) {
			data := map[string]string{"OTHER_KEY": "other"}
			wantVal := ""
			if tc.wantFound {
				wantVal = "value-of-" + tc.wantName
				data[tc.wantName] = wantVal
			}
			gotVal, gotFound := LookupInMap(data, tc.key)
			if gotFound != tc.wantFound {
				t.Errorf("LookupInMap(%q) found = %v, want %v", tc.key, gotFound, tc.wantFound)
			}
			if gotVal != wantVal {
				t.Errorf("LookupInMap(%q) value = %q, want %q", tc.key, gotVal, wantVal)
			}
		})
	}

	// Rows unique to value lookup: candidate precedence and value handling.
	tests := []struct {
		name      string
		data      map[string]string
		key       string
		wantVal   string
		wantFound bool
	}{
		{
			name:      "literal dotted key exact match",
			data:      map[string]string{"APNEWS.COM": "cookie"},
			key:       "APNEWS.COM",
			wantVal:   "cookie",
			wantFound: true,
		},
		{
			name:      "literal dotted key preferred over underscore form",
			data:      map[string]string{"APNEWS.COM": "literal", "APNEWS_COM": "nested"},
			key:       "apnews.com",
			wantVal:   "literal",
			wantFound: true,
		},
		{
			name:      "dotted lookup falls back to underscore when literal absent",
			data:      map[string]string{"APNEWS_COM": "nested"},
			key:       "apnews.com",
			wantVal:   "nested",
			wantFound: true,
		},
		{
			name:      "preserves whitespace",
			data:      map[string]string{"KEY": "  value  "},
			key:       "KEY",
			wantVal:   "  value  ",
			wantFound: true,
		},
		{
			name:      "exact match preferred over uppercase",
			data:      map[string]string{"key": "v1", "KEY": "v2"},
			key:       "key",
			wantVal:   "v1",
			wantFound: true,
		},
		{
			name:      "empty map returns false",
			data:      map[string]string{},
			key:       "ANY_KEY",
			wantVal:   "",
			wantFound: false,
		},
		{
			name:      "nil map returns false",
			data:      nil,
			key:       "ANY_KEY",
			wantVal:   "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotFound := LookupInMap(tt.data, tt.key)
			if gotFound != tt.wantFound {
				t.Errorf("LookupInMap(%q) found = %v, want %v", tt.key, gotFound, tt.wantFound)
			}
			if gotVal != tt.wantVal {
				t.Errorf("LookupInMap(%q) value = %q, want %q", tt.key, gotVal, tt.wantVal)
			}
		})
	}
}

func TestResolveKeyName(t *testing.T) {
	// Shared pipeline matrix: exists reports exactly wantName as stored.
	for _, tc := range isResolutionCases {
		t.Run(tc.key, func(t *testing.T) {
			exists := map[string]bool{"OTHER_KEY": true}
			if tc.wantFound {
				exists[tc.wantName] = true
			}
			gotName, gotFound := ResolveKeyName(tc.key, func(k string) bool { return exists[k] })
			if gotFound != tc.wantFound {
				t.Errorf("ResolveKeyName(%q) found = %v, want %v", tc.key, gotFound, tc.wantFound)
			}
			if gotName != tc.wantName {
				t.Errorf("ResolveKeyName(%q) name = %q, want %q", tc.key, gotName, tc.wantName)
			}
		})
	}

	// Unique to name resolution: which stored key wins when both the literal
	// dotted form and the underscore form exist.
	t.Run("literal dotted key preferred over underscore form", func(t *testing.T) {
		exists := map[string]bool{"APNEWS.COM": true, "APNEWS_COM": true}
		gotName, gotFound := ResolveKeyName("APNEWS.COM", func(k string) bool { return exists[k] })
		if !gotFound || gotName != "APNEWS.COM" {
			t.Errorf("ResolveKeyName(APNEWS.COM) = (%q, %v), want (%q, true)", gotName, gotFound, "APNEWS.COM")
		}
	})
}

func TestResolveBaseKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want []string
	}{
		{
			name: "lowercase key resolves to itself then uppercase",
			key:  "host",
			want: []string{"host", "HOST"},
		},
		{
			name: "already-uppercase key resolves to itself only",
			key:  "HOST",
			want: []string{"HOST"},
		},
		{
			name: "literal dotted key keeps literal forms before underscore form",
			key:  "apnews.com",
			want: []string{"apnews.com", "APNEWS.COM", "APNEWS_COM"},
		},
		{
			name: "dotted key with numeric index adds bracket candidate",
			key:  "servers.0",
			want: []string{"servers.0", "SERVERS.0", "SERVERS_0", "SERVERS[0]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBaseKeys(tt.key)
			if len(got) != len(tt.want) {
				t.Fatalf("ResolveBaseKeys(%q) = %v, want %v", tt.key, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("ResolveBaseKeys(%q)[%d] = %q, want %q", tt.key, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitAndTrimAt(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		index     int
		wantVal   string
		wantFound bool
	}{
		{"first element", "a,b,c", 0, "a", true},
		{"second element", "a,b,c", 1, "b", true},
		{"third element", "a,b,c", 2, "c", true},
		{"out of range", "a,b", 5, "", false},
		{"negative index", "a,b", -1, "", false},
		{"empty string", "", 0, "", false},
		{"with whitespace", "  a  ,  b  ,  c  ", 1, "b", true},
		{"skips empty parts", "a,,b,,,c", 1, "b", true},
		{"single element", "only", 0, "only", true},
		{"trailing comma", "a,b,", 1, "b", true},
		{"leading comma", ",a,b", 0, "a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotFound := SplitAndTrimAt(tt.input, tt.index)
			if gotFound != tt.wantFound {
				t.Errorf("SplitAndTrimAt(%q, %d) found = %v, want %v", tt.input, tt.index, gotFound, tt.wantFound)
			}
			if gotVal != tt.wantVal {
				t.Errorf("SplitAndTrimAt(%q, %d) = %q, want %q", tt.input, tt.index, gotVal, tt.wantVal)
			}
		})
	}
}

func TestResolveKey_CommaSeparatedFallback(t *testing.T) {
	data := map[string]string{
		"HOSTS": "host1,host2,host3",
	}

	tests := []struct {
		name      string
		key       string
		wantVal   string
		wantFound bool
	}{
		{"comma index 0", "hosts.0", "host1", true},
		{"comma index 1", "hosts.1", "host2", true},
		{"comma index 2", "hosts.2", "host3", true},
		{"comma index out of range", "hosts.5", "", false},
		{"non-indexed dot key", "hosts", "host1,host2,host3", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotFound := ResolveKey(tt.key, func(k string) (string, bool) {
				v, ok := data[k]
				return v, ok
			})
			if gotFound != tt.wantFound {
				t.Errorf("ResolveKey(%q) found = %v, want %v", tt.key, gotFound, tt.wantFound)
			}
			if gotVal != tt.wantVal {
				t.Errorf("ResolveKey(%q) = %q, want %q", tt.key, gotVal, tt.wantVal)
			}
		})
	}
}

// ============================================================================
// ResolvePath / ExtractNumericIndex (merged from path_resolver_test.go:
// lookup.go drives path_resolver.go, so both halves of the resolution
// pipeline live in one file)
// ============================================================================

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name:     "simple nested path",
			path:     "database.host",
			expected: []string{"DATABASE_HOST"},
		},
		{
			name:     "three level nested path",
			path:     "a.b.c",
			expected: []string{"A_B_C"},
		},
		{
			name:     "array index access",
			path:     "servers.0",
			expected: []string{"SERVERS_0", "SERVERS[0]"},
		},
		{
			name:     "nested array element",
			path:     "servers.0.host",
			expected: []string{"SERVERS_0_HOST", "SERVERS[0]_HOST"},
		},
		{
			name:     "multiple array indices",
			path:     "matrix.0.1",
			expected: []string{"MATRIX_0_1", "MATRIX[0][1]"},
		},
		{
			name:     "simple uppercase key unchanged",
			path:     "DATABASE_HOST",
			expected: []string{"DATABASE_HOST"},
		},
		{
			name:     "simple lowercase key returns both cases",
			path:     "database_host",
			expected: []string{"database_host", "DATABASE_HOST"},
		},
		{
			name:     "simple mixed case key returns both",
			path:     "autoUpdatesChannel",
			expected: []string{"autoUpdatesChannel", "AUTOUPDATESCHANNEL"},
		},
		{
			name:     "deep nesting with array",
			path:     "config.database.connections.0.port",
			expected: []string{"CONFIG_DATABASE_CONNECTIONS_0_PORT", "CONFIG_DATABASE_CONNECTIONS[0]_PORT"},
		},
		// Boundary rows pinning ResolvePath's handling of edge-case shapes.
		{
			name:     "empty path resolves to single empty candidate",
			path:     "",
			expected: []string{""},
		},
		{
			name:     "leading dot becomes leading underscore",
			path:     ".env",
			expected: []string{"_ENV"},
		},
		{
			name:     "consecutive dots become consecutive underscores",
			path:     "a..b",
			expected: []string{"A__B"},
		},
		{
			// Pins current behavior: a leading-zero index is still a numeric
			// part, so both the underscore and bracket candidates are built
			// with the zeros verbatim.
			name:     "leading-zero index",
			path:     "servers.00",
			expected: []string{"SERVERS_00", "SERVERS[00]"},
		},
		{
			// Pins current behavior: ToUpperASCII is ASCII-only, so non-ASCII
			// bytes pass through unchanged (the lowercase accent stays
			// lowercase).
			name:     "non-ASCII part preserved as-is",
			path:     "café.host",
			expected: []string{"CAFé_HOST"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolvePath(tt.path)
			if len(result) != len(tt.expected) {
				t.Errorf("ResolvePath() returned %d candidates, expected %d", len(result), len(tt.expected))
				t.Errorf("got: %v", result)
				t.Errorf("want: %v", tt.expected)
				return
			}
			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("ResolvePath()[%d] = %q, expected %q", i, result[i], exp)
				}
			}
		})
	}
}

func TestIsNumericIndex(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0", true},
		{"1", true},
		{"123", true},
		{"", false},
		{"abc", false},
		{"1a", false},
		{"a1", false},
		{"-1", false},
		{"1.5", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isNumericIndex(tt.input)
			if result != tt.expected {
				t.Errorf("isNumericIndex(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractNumericIndex(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantBase  string
		wantIndex int
		wantOK    bool
	}{
		{
			name:      "simple indexed path",
			path:      "servers.0",
			wantBase:  "servers",
			wantIndex: 0,
			wantOK:    true,
		},
		{
			name:      "nested indexed path",
			path:      "service.cors.origins.0",
			wantBase:  "service.cors.origins",
			wantIndex: 0,
			wantOK:    true,
		},
		{
			name:      "multi-digit index",
			path:      "items.123",
			wantBase:  "items",
			wantIndex: 123,
			wantOK:    true,
		},
		{
			name:      "deep nesting with index",
			path:      "config.database.connections.5",
			wantBase:  "config.database.connections",
			wantIndex: 5,
			wantOK:    true,
		},
		{
			name:      "max safe index boundary (2^31-1) accepted",
			path:      "KEY.2147483647",
			wantBase:  "KEY",
			wantIndex: 2147483647,
			wantOK:    true,
		},
		{
			name:      "index above maxSafeIndex rejected",
			path:      "KEY.2147483648",
			wantBase:  "",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "path without index",
			path:      "database.host",
			wantBase:  "",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "simple key without dot",
			path:      "hostname",
			wantBase:  "",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "single numeric key",
			path:      "123",
			wantBase:  "",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "empty string",
			path:      "",
			wantBase:  "",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "trailing dot",
			path:      "servers.",
			wantBase:  "",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "non-numeric suffix",
			path:      "servers.host",
			wantBase:  "",
			wantIndex: -1,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase, gotIndex, gotOK := ExtractNumericIndex(tt.path)
			if gotBase != tt.wantBase || gotIndex != tt.wantIndex || gotOK != tt.wantOK {
				t.Errorf("ExtractNumericIndex(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.path, gotBase, gotIndex, gotOK, tt.wantBase, tt.wantIndex, tt.wantOK)
			}
		})
	}
}

// TestResolvePath_HardMaxKeyLength pins the memory-exhaustion guard at both
// boundaries: a path of exactly HardMaxKeyLength resolves normally, while one
// byte more resolves to no candidates.
func TestResolvePath_HardMaxKeyLength(t *testing.T) {
	exact := strings.Repeat("A", HardMaxKeyLength)
	if got := ResolvePath(exact); len(got) != 1 || got[0] != exact {
		t.Errorf("ResolvePath(exact %d-byte key) = %v, want single %d-byte candidate", HardMaxKeyLength, got, HardMaxKeyLength)
	}
	if got := ResolvePath(strings.Repeat("A", HardMaxKeyLength+1)); got != nil {
		t.Errorf("ResolvePath(overlong path) = %v, want nil", got)
	}
}

// TestExtractNumericIndex_OverflowBranches covers the two integer-overflow
// guards: digit strings longer than the 10-digit length cap, and values that
// exceed maxSafeIndex during accumulation.
func TestExtractNumericIndex_OverflowBranches(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"eleven-digit index exceeds length cap", "KEY.12345678901"},
		{"ten digits above maxSafeIndex", "KEY.9999999999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			basePath, index, ok := ExtractNumericIndex(tt.in)
			if ok || index != -1 || basePath != "" {
				t.Errorf("ExtractNumericIndex(%q) = (%q, %d, %v), want (\"\", -1, false)", tt.in, basePath, index, ok)
			}
		})
	}
}
