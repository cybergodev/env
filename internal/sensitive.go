// Package internal provides sensitive key detection and masking utilities.
package internal

import (
	"fmt"
	"sort"
	"strings"
)

// Patterns defines patterns that indicate sensitive data.
// This is the single source of truth for sensitive key detection.
// Keys containing these patterns (case-insensitive) will be masked in logs and errors.
var Patterns = []string{
	// Authentication & Authorization
	"PASSWORD",
	"SECRET",
	"TOKEN",
	"AUTH",
	"CREDENTIAL",
	"PASSPHRASE",
	"SESSION",
	"COOKIE",

	// API & Keys
	"API_KEY",
	"APIKEY",
	"ACCESS_KEY",
	"SECRET_KEY",
	"PRIVATE_KEY",
	"PUBLIC_KEY", // May contain paired private key context

	// Encryption & Security
	"PRIVATE",
	"ENCRYPTION_KEY",
	"ENCRYPT_KEY",
	"DECRYPT_KEY",
	"SIGNING_KEY",
	"SIGN_KEY",
	"VERIFY_KEY",

	// Financial & Personal (PII)
	"SSN",
	"SOCIAL_SECURITY",
	"CREDIT_CARD",
	"CARD_NUMBER",
	"CVV",
	"CVC",
	"CCV",
	"PAN", // Primary Account Number

	// Crypto & Blockchain
	"MNEMONIC",
	"SEED",
	"RECOVERY",
	"WALLET",
	"PRIVATE_ADDRESS",

	// Database & Infrastructure
	"CONNECTION_STRING",
	"CONN_STRING",
	"DATABASE_URL",
	"DB_PASSWORD",

	// Cloud & Services
	"AWS_SECRET",
	"AZURE_KEY",
	"GCP_KEY",
	"SERVICE_ACCOUNT",
}

// upperPatterns contains pre-uppercased patterns for faster matching.
// This avoids calling strings.ToUpper for each pattern during IsKey.
var upperPatterns = Patterns // Already uppercase

// sanitizePatterns contains lowercase patterns for sanitizing logs.
// These patterns are used to detect and mask sensitive key=value pairs.
var sanitizePatterns = []string{
	// Authentication
	"password=",
	"secret=",
	"token=",
	"auth=",
	"credential=",
	"passphrase=",
	"session=",
	"cookie=",

	// API & Keys
	"api_key=",
	"apikey=",
	"access_key=",
	"secret_key=",
	"private_key=",
	"public_key=",

	// Encryption
	"encrypt_key=",
	"decrypt_key=",
	"signing_key=",

	// Financial (PII)
	"ssn=",
	"credit_card=",
	"card_number=",
	"cvv=",
	"cvc=",

	// Crypto
	"mnemonic=",
	"seed=",
	"recovery=",
	"wallet=",

	// Database
	"connection_string=",
	"database_url=",
	"db_password=",
}

// IsKey determines if a key likely contains sensitive data.
// Uses byte-level comparison to avoid allocations from strings.ToUpper.
func IsKey(key string) bool {
	if len(key) == 0 {
		return false
	}

	// Check each pattern using zero-allocation case-insensitive matching
	for _, pattern := range upperPatterns {
		if containsIgnoreCase(key, pattern) {
			return true
		}
	}
	return false
}

// containsIgnoreCase checks if s contains pattern (case-insensitive, ASCII only).
// Pattern must be uppercase. This function avoids any heap allocations for ASCII input.
// For non-ASCII input, falls back to strings.Contains + strings.ToUpper for correctness.
func containsIgnoreCase(s, pattern string) bool {
	patternLen := len(pattern)
	sLen := len(s)
	if patternLen > sLen || patternLen == 0 {
		return false
	}

	// Quick scan for non-ASCII — exit early to fallback path
	for i := 0; i < sLen; i++ {
		if s[i] >= 0x80 {
			return strings.Contains(strings.ToUpper(s), pattern)
		}
	}

	// ASCII-only fast path: slide window through s
	for i := 0; i <= sLen-patternLen; i++ {
		match := true
		for j := 0; j < patternLen; j++ {
			c := s[i+j]
			if c >= 'a' && c <= 'z' {
				c -= 32
			}
			if c != pattern[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// maxValueDisplayLen is the maximum length of non-sensitive values displayed in logs.
const maxValueDisplayLen = 20

// MaskValue masks a value based on its sensitivity.
// This is a utility function for general value masking.
func MaskValue(key, value string) string {
	if IsKey(key) {
		return fmt.Sprintf("[MASKED:%d chars]", len(value))
	}
	if len(value) <= maxValueDisplayLen {
		return value
	}
	return value[:maxValueDisplayLen-3] + "..."
}

// MaskKey masks a key name for logging purposes.
func MaskKey(key string) string {
	return DefaultMaskKey(key)
}

// MaskInString masks potentially sensitive content in a string.
func MaskInString(s string) string {
	const maxLen = 50
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// SanitizeForLog removes potentially sensitive information from a string.
// It scans for patterns that might indicate sensitive data and masks them.
//
// Performance: Uses single-pass scanning with strings.Builder to avoid
// O(n*m) complexity from multiple string scans and allocations.
func SanitizeForLog(s string) string {
	if len(s) == 0 {
		return s
	}

	// Convert to lowercase for pattern matching
	lowerS := strings.ToLower(s)

	// Find all replacements needed (pattern end position -> mask position)
	// Use a slice of structs to avoid map allocation for small number of matches
	type replacement struct {
		valueStart int // start of value to mask (after pattern)
		valueEnd   int // end of value to mask
	}
	var replacements []replacement

	// Single pass: find all pattern matches
	for _, pattern := range sanitizePatterns {
		patternLen := len(pattern)
		searchStart := 0

		for {
			idx := strings.Index(lowerS[searchStart:], pattern)
			if idx == -1 {
				break
			}
			idx += searchStart // Adjust to absolute position

			// Find the value after the pattern
			valueStart := idx + patternLen
			valueEnd := valueStart

			// Find end of value (space, newline, tab, or end of string)
			for valueEnd < len(s) && s[valueEnd] != ' ' && s[valueEnd] != '\n' && s[valueEnd] != '\t' {
				valueEnd++
			}

			if valueEnd > valueStart {
				replacements = append(replacements, replacement{valueStart, valueEnd})
			}

			// Continue searching after this match
			searchStart = valueStart + 1
			if searchStart >= len(lowerS) {
				break
			}
		}
	}

	// If no replacements needed, just remove control characters
	if len(replacements) == 0 {
		return removeControlChars(s)
	}

	// Sort replacements by position (they may be out of order due to multiple patterns)
	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].valueStart < replacements[j].valueStart
	})

	// Build result with strings.Builder
	var result strings.Builder
	result.Grow(len(s))

	lastEnd := 0
	for _, r := range replacements {
		// Skip overlapping replacements
		if r.valueStart < lastEnd {
			continue
		}
		// Write unchanged portion
		result.WriteString(s[lastEnd:r.valueStart])
		// Write mask
		result.WriteString("[MASKED]")
		lastEnd = r.valueEnd
	}
	// Write remaining portion
	if lastEnd < len(s) {
		result.WriteString(s[lastEnd:])
	}

	return removeControlChars(result.String())
}

// removeControlChars removes control characters except newline and tab.
func removeControlChars(s string) string {
	// Fast path: check if any control characters exist
	hasControl := false
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 && s[i] != '\n' && s[i] != '\t' {
			hasControl = true
			break
		}
	}
	if !hasControl {
		return s
	}

	// Slow path: remove control characters
	var result strings.Builder
	result.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x20 || s[i] == '\n' || s[i] == '\t' {
			result.WriteByte(s[i])
		}
	}
	return result.String()
}
