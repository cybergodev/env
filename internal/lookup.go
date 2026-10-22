// Provides key lookup utilities for plain map[string]string.

package internal

import "strings"

// LookupFunc is a function that retrieves a value by key.
// Returns (value, true) if found, ("", false) otherwise.
type LookupFunc func(key string) (string, bool)

// ResolveKey resolves a key using the standard resolution strategy:
//  1. Exact match
//  2. Uppercase fallback
//  3. Dot-notation resolution via ResolvePath (e.g., "database.host" -> "DATABASE_HOST")
//  4. Comma-separated indexed access fallback (e.g., "list.0" -> first element of "list")
//
// Steps 1-2 run for every key, dotted or not: stored keys may themselves
// contain dots (a YAML/JSON top-level key like "apnews.com" is flattened
// verbatim to "APNEWS.COM"), and the dot-notation candidates in step 3
// only ever contain the underscore-joined form — without the literal match
// such keys would be unreachable.
//
// Returns the stored value as-is (no trimming). Values are trimmed during parsing,
// so this is consistent with All()/ToMap().
// Returns (value, true) if found, ("", false) otherwise.
func ResolveKey(key string, lookup LookupFunc) (string, bool) {
	if value, ok := lookup(key); ok {
		return value, true
	}
	upper := ToUpperASCII(key)
	if upper != key {
		if value, ok := lookup(upper); ok {
			return value, true
		}
	}

	// Fast path: simple keys (no dots) have no further resolution strategy
	if strings.IndexByte(key, '.') == -1 {
		return "", false
	}

	// Dot-notation: resolve path to candidate keys
	candidates := ResolvePath(key)
	for _, candidate := range candidates {
		if value, ok := lookup(candidate); ok {
			return value, true
		}
	}

	// Fallback to comma-separated values for indexed access
	if basePath, index, hasIndex := ExtractNumericIndex(key); hasIndex {
		baseCandidates := ResolvePath(basePath)
		for _, baseCandidate := range baseCandidates {
			if value, ok := lookup(baseCandidate); ok {
				if elem, found := SplitAndTrimAt(value, index); found {
					return elem, true
				}
			}
		}
	}

	return "", false
}

// ResolveBaseKeys returns the storage-key candidates for key in lookup
// resolution order: the literal key, its uppercase form, then the
// dot-notation candidates from ResolvePath (underscore- and bracket-joined).
// Duplicates are removed preserving first occurrence.
//
// GetSliceFrom uses this to probe indexed keys (KEY_0, KEY_1, ...) and the
// comma-separated fallback against the same base-key order that
// Lookup/ResolveKey use for scalar values, so keys that literally contain
// dots (a YAML "apnews.com" array flattens to APNEWS.COM_0, APNEWS.COM_1)
// resolve exactly like nested paths (database.hosts -> DATABASE_HOST_0).
func ResolveBaseKeys(key string) []string {
	candidates := make([]string, 0, 4)
	add := func(k string) {
		for _, existing := range candidates {
			if existing == k {
				return
			}
		}
		candidates = append(candidates, k)
	}
	add(key)
	if upper := ToUpperASCII(key); upper != key {
		add(upper)
	}
	for _, c := range ResolvePath(key) {
		add(c)
	}
	return candidates
}

// SplitAndTrimAt returns the element at the given index from a comma-separated string.
// It iterates without allocation, returning the trimmed element at the specified index.
// Returns empty string and false if the index is out of bounds or negative.
func SplitAndTrimAt(s string, index int) (string, bool) {
	if index < 0 {
		return "", false
	}
	currentIdx := 0
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				trimmed := TrimSpace(s[start:i])
				if trimmed != "" {
					if currentIdx == index {
						return trimmed, true
					}
					currentIdx++
				}
			}
			start = i + 1
		}
	}
	return "", false
}

// LookupInMap resolves a key in a map using the same resolution strategy as ResolveKey.
// Returns the stored value as-is (no trimming).
// Returns (value, true) if found, ("", false) otherwise.
func LookupInMap(data map[string]string, key string) (string, bool) {
	return ResolveKey(key, func(k string) (string, bool) {
		v, ok := data[k]
		return v, ok
	})
}

// ResolveKeyName resolves a key using the same strategy as ResolveKey,
// but returns the matched storage key name instead of the value.
// This is useful for operations that need the key name (like GetSecure)
// rather than the value itself.
//
// Resolution strategy:
//  1. Exact match (including literal keys containing dots, e.g. "APNEWS.COM")
//  2. Uppercase fallback
//  3. Dot-notation resolution via ResolvePath
//
// Note: Does NOT include the comma-separated indexed access fallback,
// since that strategy produces virtual values, not real storage keys.
func ResolveKeyName(key string, exists func(string) bool) (string, bool) {
	if exists(key) {
		return key, true
	}
	upper := ToUpperASCII(key)
	if upper != key && exists(upper) {
		return upper, true
	}

	// Fast path: simple keys (no dots) have no further resolution strategy
	if strings.IndexByte(key, '.') == -1 {
		return "", false
	}

	// Dot-notation: resolve path to candidate keys
	candidates := ResolvePath(key)
	for _, candidate := range candidates {
		if exists(candidate) {
			return candidate, true
		}
	}
	return "", false
}
