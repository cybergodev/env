// Package internal provides path resolution utilities for dot-notation key access.
package internal

import (
	"strings"
)

// ResolvePath converts a dot-notation path to candidate storage keys.
// It is used to look up nested values that were flattened during JSON/YAML parsing.
//
// For dot-notation paths (containing "."):
//   - Returns only the converted uppercase storage keys
//   - "database.host" -> ["DATABASE_HOST"]
//   - "servers.0.host" -> ["SERVERS_0_HOST", "SERVERS[0]_HOST"]
//
// For simple keys (no dots):
//   - Returns both the original key and uppercase version
//   - "HOST" -> ["HOST", "HOST"] (same key)
//   - "host" -> ["host", "HOST"] (try original first, then uppercase)
//
// Note: For simple keys, callers should use direct Get() with the key and its
// uppercase version to avoid this allocation entirely.
//
// SECURITY: Paths exceeding HardMaxKeyLength are rejected to prevent memory exhaustion attacks.
func ResolvePath(path string) []string {
	// SECURITY: Reject excessively long paths to prevent memory exhaustion
	// Uses HardMaxKeyLength from limits.go to ensure consistency across the codebase
	if len(path) > HardMaxKeyLength {
		return nil
	}
	// Fast path for simple keys (no dots)
	// Use IndexByte which is SIMD-optimized
	dotIdx := strings.IndexByte(path, '.')
	if dotIdx == -1 {
		upper := ToUpperASCII(path)
		if upper == path {
			// Return single-element slice
			return []string{path}
		}
		// Return two-element slice with both candidates
		return []string{path, upper}
	}

	// Count dots to pre-allocate slice
	dotCount := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			dotCount++
		}
	}

	// Pre-allocate result builder with estimated size
	// Each part becomes uppercase and is joined with underscore
	estimatedLen := len(path) + dotCount*2 // Account for potential case changes and separators

	// Build converted key directly without intermediate slice
	result := GetBuilder()
	defer PutBuilder(result)
	result.Grow(estimatedLen)

	hasNumeric := false
	start := 0
	partIndex := 0

	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			part := path[start:i]
			if partIndex > 0 {
				result.WriteByte('_')
			}
			if isNumericIndex(part) {
				result.WriteString(part)
				hasNumeric = true
			} else {
				result.WriteString(ToUpperASCII(part))
			}
			start = i + 1
			partIndex++
		}
	}

	converted := result.String()

	// If no numeric parts, return single candidate
	if !hasNumeric {
		return []string{converted}
	}

	// Build bracket format key: numeric parts render as [index] with no
	// surrounding separator; non-numeric parts are joined with underscore.
	// (An earlier implementation tracked prevWasNumeric, but all three of
	// its branches emitted identical bytes, so the tracking was dead logic.)
	bracketResult := GetBuilder()
	defer PutBuilder(bracketResult)
	bracketResult.Grow(estimatedLen + dotCount*2) // Extra space for brackets

	start = 0
	partIndex = 0

	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			part := path[start:i]
			if isNumericIndex(part) {
				bracketResult.WriteByte('[')
				bracketResult.WriteString(part)
				bracketResult.WriteByte(']')
			} else {
				if partIndex > 0 {
					bracketResult.WriteByte('_')
				}
				bracketResult.WriteString(ToUpperASCII(part))
			}
			start = i + 1
			partIndex++
		}
	}

	bracketKey := bracketResult.String()
	if bracketKey == converted {
		return []string{converted}
	}
	return []string{converted, bracketKey}
}

// isNumericIndex checks if a string represents a non-negative integer.
func isNumericIndex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ExtractNumericIndex extracts a numeric index from the end of a path.
// Returns: (basePath, index, true) if path ends with numeric index
// Returns: ("", -1, false) otherwise
//
// Examples:
//
//	"servers.0" -> ("servers", 0, true)
//	"service.cors.origins.0" -> ("service.cors.origins", 0, true)
//	"database.host" -> ("", -1, false)
func ExtractNumericIndex(path string) (basePath string, index int, ok bool) {
	// Find the last dot in the path
	lastDot := strings.LastIndex(path, ".")
	if lastDot < 0 {
		// No dot found, check if entire path is numeric
		if isNumericIndex(path) {
			return "", -1, false // Single numeric is not a valid indexed path
		}
		return "", -1, false
	}

	// Extract the potential index part after the last dot
	indexPart := path[lastDot+1:]
	if !isNumericIndex(indexPart) {
		return "", -1, false
	}

	// SECURITY: Prevent integer overflow for very long numeric indices
	// Max int on 64-bit systems is ~9 quintillion (19 digits)
	// We use 10 digits as safe limit (covers indices up to ~4 billion)
	// On 32-bit systems, max int is ~2 billion, so we need overflow checking
	const maxIndexLen = 10
	if len(indexPart) > maxIndexLen {
		return "", -1, false
	}

	// Parse the index with overflow protection
	// Use int64 to safely detect overflow on both 32-bit and 64-bit systems
	const maxSafeIndex = 1<<31 - 1 // Max int32, safe for all platforms
	var idx int64
	for i := 0; i < len(indexPart); i++ {
		digit := int64(indexPart[i] - '0')
		idx = idx*10 + digit
		// Check for overflow during accumulation
		if idx > maxSafeIndex {
			return "", -1, false
		}
	}

	return path[:lastDot], int(idx), true
}
