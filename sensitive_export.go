package env

import (
	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Masking Utilities (re-exported from internal/sensitive)
// ============================================================================

// IsSensitiveKey determines if a key likely contains sensitive data.
func IsSensitiveKey(key string) bool {
	return internal.IsKey(key)
}

// MaskValue masks a value based on its sensitivity.
// This is a utility function for general value masking.
func MaskValue(key, value string) string {
	return internal.MaskValue(key, value)
}

// MaskKey masks a key name for logging purposes.
func MaskKey(key string) string {
	return internal.MaskKey(key)
}

// MaskSensitiveInString masks potentially sensitive content in a string.
func MaskSensitiveInString(s string) string {
	return internal.MaskInString(s)
}

// SanitizeForLog removes potentially sensitive information from a string.
// It scans for patterns that might indicate sensitive data and masks them.
func SanitizeForLog(s string) string {
	return internal.SanitizeForLog(s)
}
