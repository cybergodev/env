// Provides a conservative duplicate-object-key detector for JSON documents.

package internal

import (
	"encoding/json"
	"strings"
)

// detectJSONDuplicateKeys reports whether data contains two identical keys
// within the same JSON object, returning the duplicated key. It backs the
// DuplicateKeyError policy (see DuplicateKeyPolicy) for standalone JSON
// documents and for inline JSON inside YAML values.
//
// It is a structural pre-scan, NOT a replacement decoder: json.Unmarshal
// still runs afterwards and remains the authority on syntax. The detector
// is deliberately conservative — on ANY structural surprise it reports "no
// duplicate found" (false) and lets Unmarshal produce the real error, so it
// can never reject a document that Unmarshal would accept with distinct
// keys. Known detection limits, all falling back to last-wins decoding:
//
//   - keys are compared DECODED (see decodeKeyEscapes), so a plain spelling
//     next to its escaped form ("é" vs "é") IS detected; a key whose
//     escapes do not decode cleanly falls back to raw-byte comparison —
//     a missed duplicate, never a false one;
//   - malformed documents report no duplicate (Unmarshal rejects them
//     anyway);
//   - nesting deeper than maxDepth aborts the scan. Production callers run
//     scanJSONLimits first, which already bounds nesting by cfg.MaxDepth,
//     so this cap is a defensive recursion guard only.
//
// Because Unmarshal resolves duplicates by decode order (last wins), a
// missed detection degrades to the DuplicateKeyInherit behavior rather than
// to a false rejection.
func detectJSONDuplicateKeys(data []byte, maxDepth int) (string, bool) {
	p := &dupScanner{data: data, maxDepth: maxDepth}
	p.skipWS()
	if p.i >= len(p.data) {
		return "", false
	}
	if !p.parseValue(0) {
		return "", false
	}
	// Trailing garbage after the root value is not a duplicate concern;
	// report what was found and let Unmarshal reject the document.
	return p.dupKey, p.found
}

// dupScanner is the scanning state for detectJSONDuplicateKeys.
type dupScanner struct {
	data     []byte
	i        int
	maxDepth int
	found    bool
	dupKey   string
}

// skipWS advances past JSON whitespace.
func (p *dupScanner) skipWS() {
	for p.i < len(p.data) {
		switch p.data[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

// scanString consumes a string (opening quote at p.i) and returns its raw
// contents without escape decoding. ok=false on an unterminated string or a
// backslash at the very end — both are syntax errors Unmarshal will report.
func (p *dupScanner) scanString() (string, bool) {
	if p.i >= len(p.data) || p.data[p.i] != '"' {
		return "", false
	}
	start := p.i + 1
	p.i++
	for p.i < len(p.data) {
		switch p.data[p.i] {
		case '\\':
			if p.i+1 >= len(p.data) {
				return "", false
			}
			p.i += 2
		case '"':
			s := string(p.data[start:p.i])
			p.i++
			return s, true
		default:
			p.i++
		}
	}
	return "", false
}

// decodeKeyEscapes normalizes a scanned key for duplicate comparison by
// decoding its JSON escapes with the decoder's own semantics: the raw
// contents are re-wrapped in quotes and run through json.Unmarshal, so
// "é" and "é" compare equal exactly when Unmarshal would decode them
// identically. Keys without a backslash return unchanged (the common case,
// zero cost); anything malformed returns the raw string — the conservative
// direction for this scanner, where a missed duplicate degrades to
// last-wins decoding rather than a false rejection.
func decodeKeyEscapes(raw string) string {
	if strings.IndexByte(raw, '\\') == -1 {
		return raw
	}
	var decoded string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &decoded); err != nil {
		return raw
	}
	return decoded
}

// scanLiteral consumes a number or the letters of true/false/null. It only
// needs to span the token; the following structural check (',' ':' '}' ']')
// plus Unmarshal validate the exact form.
func (p *dupScanner) scanLiteral() bool {
	start := p.i
	for p.i < len(p.data) {
		c := p.data[p.i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || c == '-' || c == '+' || c == '.' || c == 'E' || c == 'e' {
			p.i++
			continue
		}
		break
	}
	return p.i > start
}

// parseValue consumes one complete JSON value at any depth. Container values
// recurse through parseObject/parseArray; scalars are consumed opaquely.
func (p *dupScanner) parseValue(depth int) bool {
	if p.found {
		return true
	}
	if depth > p.maxDepth {
		return false // defensive: scanJSONLimits already bounds nesting
	}
	p.skipWS()
	if p.i >= len(p.data) {
		return false
	}
	switch p.data[p.i] {
	case '{':
		return p.parseObject(depth + 1)
	case '[':
		return p.parseArray(depth + 1)
	case '"':
		_, ok := p.scanString()
		return ok
	default:
		return p.scanLiteral()
	}
}

// parseObject consumes one object, checking its keys for duplicates.
func (p *dupScanner) parseObject(depth int) bool {
	if p.found {
		return true
	}
	p.i++ // consume '{'
	keys := make(map[string]struct{})
	p.skipWS()
	if p.i < len(p.data) && p.data[p.i] == '}' {
		p.i++
		return true
	}
	for {
		p.skipWS()
		if p.i >= len(p.data) || p.data[p.i] != '"' {
			return false // keys must be strings; Unmarshal reports the rest
		}
		key, ok := p.scanString()
		if !ok {
			return false
		}
		key = decodeKeyEscapes(key)
		if _, dup := keys[key]; dup {
			p.dupKey, p.found = key, true
			return true
		}
		keys[key] = struct{}{}

		p.skipWS()
		if p.i >= len(p.data) || p.data[p.i] != ':' {
			return false
		}
		p.i++

		if !p.parseValue(depth) {
			return false
		}
		if p.found {
			return true
		}

		p.skipWS()
		if p.i >= len(p.data) {
			return false
		}
		switch p.data[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return true
		default:
			return false
		}
	}
}

// parseArray consumes one array; elements are values and never keys.
func (p *dupScanner) parseArray(depth int) bool {
	if p.found {
		return true
	}
	p.i++ // consume '['
	p.skipWS()
	if p.i < len(p.data) && p.data[p.i] == ']' {
		p.i++
		return true
	}
	for {
		if !p.parseValue(depth) {
			return false
		}
		if p.found {
			return true
		}

		p.skipWS()
		if p.i >= len(p.data) {
			return false
		}
		switch p.data[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return true
		default:
			return false
		}
	}
}
