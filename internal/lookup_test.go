package internal

import "testing"

func TestLookupInMap(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]string
		key      string
		wantVal  string
		wantFound bool
	}{
		{
			name:       "exact match",
			data:       map[string]string{"DEEPSEEK_KEY": "sk-123"},
			key:        "DEEPSEEK_KEY",
			wantVal:    "sk-123",
			wantFound:  true,
		},
		{
			name:       "lowercase fallback to uppercase key",
			data:       map[string]string{"DEEPSEEK_KEY": "sk-123"},
			key:        "deepseek_key",
			wantVal:    "sk-123",
			wantFound:  true,
		},
		{
			name:       "dot-notation resolves to underscore key",
			data:       map[string]string{"DATABASE_HOST": "localhost"},
			key:        "database.host",
			wantVal:    "localhost",
			wantFound:  true,
		},
		{
			name:       "dot-notation mixed case",
			data:       map[string]string{"A_B_C": "value"},
			key:        "a.b.c",
			wantVal:    "value",
			wantFound:  true,
		},
		{
			name:       "dot-notation with nested path",
			data:       map[string]string{"SERVER_PORT": "8080"},
			key:        "server.port",
			wantVal:    "8080",
			wantFound:  true,
		},
		{
			name:       "missing key returns false",
			data:       map[string]string{"OTHER_KEY": "val"},
			key:        "MISSING",
			wantVal:    "",
			wantFound:  false,
		},
		{
			name:       "preserves whitespace",
			data:       map[string]string{"KEY": "  value  "},
			key:        "KEY",
			wantVal:    "  value  ",
			wantFound:  true,
		},
		{
			name:       "exact match preferred over uppercase",
			data:       map[string]string{"key": "v1", "KEY": "v2"},
			key:        "key",
			wantVal:    "v1",
			wantFound:  true,
		},
		{
			name:       "empty map returns false",
			data:       map[string]string{},
			key:        "ANY_KEY",
			wantVal:    "",
			wantFound:  false,
		},
		{
			name:       "nil map returns false",
			data:       nil,
			key:        "ANY_KEY",
			wantVal:    "",
			wantFound:  false,
		},
		{
			name:       "already uppercase key no fallback needed",
			data:       map[string]string{"MY_KEY": "val"},
			key:        "MY_KEY",
			wantVal:    "val",
			wantFound:  true,
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

func TestSplitAndTrimAt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		index    int
		wantVal  string
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

func TestResolveKeyName(t *testing.T) {
	data := map[string]bool{
		"DEEPSEEK_KEY":               true,
		"DATABASE_HOST":              true,
		"SERVER_PORT":                true,
		"SERVICE_CORS_ALLOW_ORIGINS": true,
	}

	tests := []struct {
		name      string
		key       string
		wantName  string
		wantFound bool
	}{
		{
			name:      "exact match",
			key:       "DEEPSEEK_KEY",
			wantName:  "DEEPSEEK_KEY",
			wantFound: true,
		},
		{
			name:      "lowercase fallback",
			key:       "deepseek_key",
			wantName:  "DEEPSEEK_KEY",
			wantFound: true,
		},
		{
			name:      "dot-notation resolves to underscore key",
			key:       "database.host",
			wantName:  "DATABASE_HOST",
			wantFound: true,
		},
		{
			name:      "dot-notation mixed case",
			key:       "server.port",
			wantName:  "SERVER_PORT",
			wantFound: true,
		},
		{
			name:      "multi-level dot-notation",
			key:       "service.cors.allow_origins",
			wantName:  "SERVICE_CORS_ALLOW_ORIGINS",
			wantFound: true,
		},
		{
			name:      "missing key returns false",
			key:       "NONEXISTENT",
			wantName:  "",
			wantFound: false,
		},
		{
			name:      "missing dot-notation returns false",
			key:       "unknown.path",
			wantName:  "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotFound := ResolveKeyName(tt.key, func(k string) bool {
				return data[k]
			})
			if gotFound != tt.wantFound {
				t.Errorf("ResolveKeyName(%q) found = %v, want %v", tt.key, gotFound, tt.wantFound)
			}
			if gotName != tt.wantName {
				t.Errorf("ResolveKeyName(%q) name = %q, want %q", tt.key, gotName, tt.wantName)
			}
		})
	}
}
