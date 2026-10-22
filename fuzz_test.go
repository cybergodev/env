// Fuzz regression targets for the three parse paths, added by the [SEC-003]
// panic-safety audit (40s per target, ~13.6M executions total, zero panics
// or hangs found at audit time). A normal `go test` run executes only the
// seed corpus below; mutation testing requires an explicit
// `go test -fuzz FuzzParseEnv -fuzztime 30s` (one target per run).
package env_test

import (
	"testing"

	"github.com/cybergodev/env"
)

// FuzzParseEnv exercises the .env path end to end: LineParser (quotes,
// export prefix, YAML-ish values), variable expansion (${VAR}, defaults,
// required refs) and validation.
func FuzzParseEnv(f *testing.F) {
	f.Add([]byte("A=1\nB=${A}\n"))
	f.Add([]byte("export X='q'\n#comment\nY=\"z\"\n"))
	f.Add([]byte("A=${B:-${C}}\nB=${C:?req}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		_, _ = env.UnmarshalMap(string(data), env.FormatEnv)
	})
}

// FuzzParseJSON exercises the JSON flatten path: nested objects, arrays,
// escapes, duplicate-key pre-scan.
func FuzzParseJSON(f *testing.F) {
	f.Add([]byte(`{"a":{"b":[1,2,{"c":"d"}]}}`))
	f.Add([]byte(`{"k":"v","n":null,"f":1.5}`))
	f.Add([]byte(`{"s":"\u0041\t\\","arr":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		_, _ = env.UnmarshalMap(string(data), env.FormatJSON)
	})
}

// FuzzParseYAML exercises the hand-rolled YAML lexer and parser plus the
// flattener: indentation, flow style, quoted and folded scalars, documents.
func FuzzParseYAML(f *testing.F) {
	f.Add([]byte("a:\n  b: 1\n  c: [x, y]\n"))
	f.Add([]byte("k: 'q'\nn: >\n  folded\nz: {a: 1}\n"))
	f.Add([]byte("- 1\n- 2\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		_, _ = env.UnmarshalMap(string(data), env.FormatYAML)
	})
}
