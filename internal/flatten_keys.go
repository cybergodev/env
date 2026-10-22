// Provides the shared flat-key constructors and the shared decoded-JSON
// value walker used by the JSON and YAML flatteners. Both flatten nested
// documents into flat maps using the same "PREFIX<delim>KEY" and
// "PREFIX<delim|bracket>INDEX" conventions; keeping the constructors —
// and the value walk — in one place prevents the two formats from
// drifting apart.

package internal

import (
	"fmt"
	"strconv"
	"strings"
)

// buildFlatKey joins prefix and an uppercased key with the delimiter.
// The uppercase fold and the join are fused into one exact-size allocation:
// the previous two-step form (ToUpperASCII, then prefix+delimiter+key
// concatenation) allocated twice whenever the key contained lowercase
// bytes — once per flattened key on every JSON/YAML parse, which the
// allocation profiles showed as a fifth of YAML parse objects.
func buildFlatKey(prefix, key, delimiter string) string {
	if prefix == "" {
		return ToUpperASCII(key)
	}
	var b strings.Builder
	b.Grow(len(prefix) + len(delimiter) + len(key))
	b.WriteString(prefix)
	b.WriteString(delimiter)
	writeUpperASCII(&b, key)
	return b.String()
}

// writeUpperASCII appends the ASCII-uppercased bytes of s to b, mirroring
// ToUpperASCII's byte-level rules: lowercase a-z fold to A-Z, everything
// else (including non-ASCII bytes) passes through unchanged.
func writeUpperASCII(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		b.WriteByte(c)
	}
}

// buildFlatArrayIndex constructs a key for array elements. indexFormat
// "bracket" renders "prefix[index]"; anything else (the "underscore"
// default) renders "prefix<delimiter>index".
func buildFlatArrayIndex(prefix string, index int, delimiter, indexFormat string) string {
	indexStr := strconv.Itoa(index)
	if indexFormat == "bracket" {
		return prefix + "[" + indexStr + "]"
	}
	return prefix + delimiter + indexStr
}

// decodedFlattenOpts carries the per-format knobs of the shared decoded-JSON
// value walk.
type decodedFlattenOpts struct {
	maxDepth       int
	nullAsEmpty    bool
	numberAsString bool
	delim          string
	indexFmt       string
}

// flattenDecoded walks a decoded JSON value tree (any of the six types
// json.Unmarshal produces) and stores flattened leaves via store, which also
// enforces the flat-key collision check. It is the single implementation
// behind both the standalone JSON flattener (FlattenJSON) and the
// inline-JSON-in-YAML path, which previously maintained separate ~50-line
// copies that had already drifted (empty-container handling, the default
// case). Per-format behavior is injected via store (setFlatJSONValue /
// setFlatYAMLValue), mkErr (the format's depth-limit error) and mkUnsupported
// (the format's terminal-type error, unreachable through json.Unmarshal).
//
// prefix is "" only for the standalone path's root document value; an empty
// prefix is not a storable key, so root-level scalars and an empty root
// container are skipped. Empty containers under a real key store "" (a key
// present in the document exists in the store), mirroring null handling.
func flattenDecoded(value interface{}, prefix string, opts decodedFlattenOpts, result map[string]string, depth int,
	store func(map[string]string, string, string) error,
	mkErr func(path string, maxDepth int) error,
	mkUnsupported func(path string, v interface{}) error) error {

	if depth >= opts.maxDepth {
		return mkErr(prefix, opts.maxDepth)
	}

	switch v := value.(type) {
	case nil:
		if prefix == "" {
			return nil
		}
		if opts.nullAsEmpty {
			return store(result, prefix, "")
		}
		return store(result, prefix, "null")

	case bool:
		// BoolAsString does not apply: a decoded JSON bool has no raw text
		// to preserve, so both settings render the canonical form.
		if prefix == "" {
			return nil
		}
		return store(result, prefix, strconv.FormatBool(v))

	case float64:
		if prefix == "" {
			return nil
		}
		if opts.numberAsString {
			// Format as integer if it's a whole number
			if v == float64(int64(v)) {
				return store(result, prefix, strconv.FormatInt(int64(v), 10))
			}
			return store(result, prefix, strconv.FormatFloat(v, 'f', -1, 64))
		}
		return store(result, prefix, fmt.Sprintf("%v", v))

	case string:
		if prefix == "" {
			return nil
		}
		return store(result, prefix, v)

	case []interface{}:
		if len(v) == 0 {
			if prefix == "" {
				return nil
			}
			return store(result, prefix, "")
		}
		for i, item := range v {
			itemPrefix := buildFlatArrayIndex(prefix, i, opts.delim, opts.indexFmt)
			if err := flattenDecoded(item, itemPrefix, opts, result, depth+1, store, mkErr, mkUnsupported); err != nil {
				return err
			}
		}

	case map[string]interface{}:
		if len(v) == 0 {
			if prefix == "" {
				return nil
			}
			return store(result, prefix, "")
		}
		for key, val := range v {
			keyPrefix := buildFlatKey(prefix, key, opts.delim)
			if err := flattenDecoded(val, keyPrefix, opts, result, depth+1, store, mkErr, mkUnsupported); err != nil {
				return err
			}
		}

	default:
		// json.Unmarshal only produces the six types above, so this is a
		// programming error upstream — fail closed in BOTH formats (the old
		// YAML copy silently stored a "%v" rendering here).
		return mkUnsupported(prefix, value)
	}

	return nil
}
