// Provides JSON flattening utilities.

package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// JSONFlattenConfig holds configuration for JSON flattening.
type JSONFlattenConfig struct {
	// KeyDelimiter is the delimiter for nested keys (default: "_").
	KeyDelimiter string
	// ArrayIndexFormat controls how array indices are formatted.
	// "underscore": KEY_0, KEY_1, etc.
	ArrayIndexFormat string
	// NullAsEmpty converts null values to empty strings (default: true).
	NullAsEmpty bool
	// NumberAsString converts numbers to strings (default: true).
	NumberAsString bool
	// BoolAsString converts booleans to strings (default: true).
	BoolAsString bool
	// MaxDepth limits the maximum nesting depth to prevent stack overflow.
	MaxDepth int
	// DuplicateKey resolves duplicate keys inside the document (see
	// DuplicateKeyPolicy). The zero value keeps the historical behavior:
	// encoding/json resolves duplicate object keys by decode order (last
	// occurrence wins).
	DuplicateKey DuplicateKeyPolicy
}

// FlattenJSON converts nested JSON data to a flat map of string key-value pairs.
// Keys are converted to uppercase with the configured delimiter.
func FlattenJSON(data []byte, cfg JSONFlattenConfig) (map[string]string, error) {
	// A document that is empty or contains only whitespace flattens to an
	// empty map, consistent with the .env and YAML paths (a bare "\n" would
	// otherwise surface as a JSON syntax error).
	if len(bytes.TrimSpace(data)) == 0 {
		return make(map[string]string), nil
	}

	// SECURITY: Pre-validate JSON nesting depth and node count BEFORE
	// json.Unmarshal so that MaxDepth and the node cap are enforced as
	// fail-fast boundaries. Without this, json.Unmarshal recursively decodes
	// the entire document into nested interface{} values before flattenValue
	// can enforce the limit, defeating its purpose and risking excessive
	// allocation (or goroutine-stack exhaustion on a document crafted
	// entirely of nested brackets at the size ceiling). The YAML inline
	// path (yaml_flatten.go) uses the same scanJSONLimits check.
	depthExceeded, nodesExceeded := scanJSONLimits(data, 0, cfg.MaxDepth, HardMaxJSONNodes)
	if depthExceeded {
		return nil, &JSONError{
			Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", cfg.MaxDepth),
		}
	}
	if nodesExceeded {
		return nil, &JSONError{
			Message: fmt.Sprintf("maximum node count exceeded (%d)", HardMaxJSONNodes),
		}
	}

	// Duplicate-key policy. Enforced before decoding (fail fast — the whole
	// document is JSON here, unlike the YAML inline path whose maybe-JSON
	// scalars check after a successful parse). A document WITHOUT duplicates
	// resolves identically under every policy and is never rejected here;
	// FirstWins rejects only documents the pre-scan proves to contain
	// duplicates (encoding/json discards earlier occurrences during decoding,
	// so the policy cannot be honored for them).
	switch cfg.DuplicateKey {
	case DuplicateKeyFirstWins:
		if key, dup := detectJSONDuplicateKeys(data, cfg.MaxDepth); dup {
			return nil, &JSONError{
				Message: fmt.Sprintf(
					"duplicate object key %q cannot be resolved with DuplicateKeyFirstWins "+
						"(encoding/json discards earlier occurrences); use LastWins or Error", key),
				Err: ErrDuplicateKey,
			}
		}
	case DuplicateKeyError:
		if key, dup := detectJSONDuplicateKeys(data, cfg.MaxDepth); dup {
			return nil, &JSONError{
				Message: fmt.Sprintf("duplicate object key %q", key),
				Err:     ErrDuplicateKey,
			}
		}
	}

	// Parse JSON
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &JSONError{
			Path:    "",
			Message: "invalid JSON syntax",
			Err:     err,
		}
	}

	// Estimate capacity from input size to reduce map growth.
	// Average JSON key-value pair is ~30-50 bytes of raw JSON, producing one flat entry.
	estSize := len(data) / 40
	if estSize < 4 {
		estSize = 4
	}
	result := make(map[string]string, estSize)
	if err := flattenValue(raw, "", cfg, result, 0); err != nil {
		return nil, err
	}

	return result, nil
}

// scanJSONLimits pre-scans JSON bytes and reports whether the bracket
// nesting (starting at startDepth) exceeds maxDepth, or the structural node
// count (opening brackets, commas and colons — a proxy for the number of
// decoded values) exceeds maxNodes. String contents are skipped so brackets
// and commas inside JSON strings are not counted. Both counters are
// conservative (they may over-count on malformed input), which is safe — the
// goal is to reject oversized input before json.Unmarshal allocates a parse
// tree whose memory is disproportionate to the input byte size, not to
// compute exact counts.
//
// Called BEFORE json.Unmarshal so MaxDepth and the node cap act as fail-fast
// boundaries rather than checks that run only after a full recursive parse.
// Shared by the standalone JSON path (FlattenJSON) and the
// inline-JSON-in-YAML path (yaml_flatten.go).
func scanJSONLimits(data []byte, startDepth, maxDepth, maxNodes int) (depthExceeded, nodesExceeded bool) {
	nesting := 0
	nodes := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '{', '[':
			nodes++
			if nesting++; startDepth+nesting > maxDepth {
				return true, false
			}
		case '}', ']':
			if nesting--; nesting < 0 {
				nesting = 0
			}
		case ',', ':':
			nodes++
		case '"':
			// Skip string body so brackets inside strings are not counted.
			i++
			for i < len(data) {
				if data[i] == '\\' {
					i++ // skip escaped char
				} else if data[i] == '"' {
					break
				}
				i++
			}
		}
		if nodes > maxNodes {
			return false, true
		}
	}
	return false, false
}

// jsonDepthError builds the standalone path's depth-limit error.
func jsonDepthError(path string, maxDepth int) error {
	return &JSONError{
		Path:    path,
		Message: fmt.Sprintf("maximum nesting depth exceeded (%d)", maxDepth),
	}
}

// jsonUnsupportedError builds the terminal-type error (unreachable through
// json.Unmarshal; exercised by tests with foreign types).
func jsonUnsupportedError(path string, v interface{}) error {
	return &JSONError{
		Path:    path,
		Message: fmt.Sprintf("unsupported JSON type: %T", v),
	}
}

// flattenValue recursively flattens a JSON value through the shared
// decoded-JSON walker (see flatten_keys.go); the format-specific parts are
// injected as setFlatJSONValue (leaf store + collision check) and the two
// error constructors above.
func flattenValue(value interface{}, prefix string, cfg JSONFlattenConfig, result map[string]string, depth int) error {
	return flattenDecoded(value, prefix, decodedFlattenOpts{
		maxDepth:       cfg.MaxDepth,
		nullAsEmpty:    cfg.NullAsEmpty,
		numberAsString: cfg.NumberAsString,
		delim:          cfg.KeyDelimiter,
		indexFmt:       cfg.ArrayIndexFormat,
	}, result, depth, setFlatJSONValue, jsonDepthError, jsonUnsupportedError)
}

// setFlatJSONValue stores a flattened leaf and rejects flat-key collisions:
// two DIFFERENT source paths collapsing onto the same flattened key (e.g.
// {"a_b":1,"A":{"b":2}}, or a literal "a_0" next to an array "a"). Before
// this check the winner depended on Go map iteration order, so the same file
// could produce different values across runs. Collisions are always an
// error — there is no previous behavior worth preserving. Mirrors
// setFlatYAMLValue in yaml_flatten.go.
func setFlatJSONValue(result map[string]string, prefix, value string) error {
	if _, exists := result[prefix]; exists {
		return &JSONError{
			Path: prefix,
			Message: fmt.Sprintf(
				"flat-key collision on %q: two different source paths collapse to the same flattened key",
				prefix),
			Err: ErrDuplicateKey,
		}
	}
	result[prefix] = value
	return nil
}

// Key construction goes through the shared builders in flatten_keys.go
// (buildFlatKey / buildFlatArrayIndex), invoked by flattenDecoded.
