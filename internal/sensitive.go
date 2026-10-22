// Provides sensitive key detection and masking utilities.

package internal

import (
	"sort"
	"strings"
)

// Patterns defines patterns that indicate sensitive data.
// This is the single source of truth for sensitive key detection.
// Keys containing these patterns (case-insensitive) will be masked in logs and errors.
var Patterns = []string{
	// Authentication & Authorization
	"PASSWORD",
	"PASSWD", // distinct spelling: PASSWORD does not contain PASSWD
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
	"KEYSTORE",

	// Encryption & Security
	"PRIVATE",
	"ENCRYPTION_KEY",
	"ENCRYPT_KEY",
	"DECRYPT_KEY",
	"SIGNING_KEY",
	"SIGN_KEY",
	"VERIFY_KEY",
	"CERT", // covers CERTIFICATE and certificate-path keys
	"SALT",
	"NONCE",

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
	"DSN",

	// Password/PII spellings not covered by PASSWORD above (SEC-006).
	"PWD",      // MYSQL_PWD, DB_PWD, PWD — distinct spelling: PASSWORD lacks PWD
	"USERNAME", // PII identifier, alongside SSN / CREDIT_CARD below

	// Cloud & Services
	"AWS_SECRET",
	"AZURE_KEY",
	"GCP_KEY",
	"SERVICE_ACCOUNT",
}

// wordAnchoredPatterns names patterns that IsKey matches only at identifier
// boundaries rather than as substrings (SEC-006). Short tokens like AUTH,
// PAN and PWD over-match ordinary words as substrings — AUTHOR/AUTHORITY,
// COMPANY/PANCAKE/EXPAND/JAPAN/SPANISH, MONKEY — turning benign keys into
// false "sensitive" hits that degrade audit-log readability. They stay in
// Patterns (AUTH, PAN, PWD) so SanitizeForLog keeps deriving their "key="
// forms, where over-masking is the documented safe direction. USERNAME is
// word-anchored too: it lives in Patterns for the "username=" sanitize form,
// but as a full word it over-matches nothing, and anchoring it keeps IsKey
// from flagging a bare "USERNAME" that is actually a benign identifier.
var wordAnchoredPatterns = []string{"AUTH", "PAN", "PWD", "USERNAME"}

// wordAnchoredSet is the membership view of wordAnchoredPatterns, used to skip
// those patterns during IsKey's substring pass (they are re-checked at word
// boundaries afterward).
var wordAnchoredSet = func() map[string]bool {
	m := make(map[string]bool, len(wordAnchoredPatterns))
	for _, p := range wordAnchoredPatterns {
		m[p] = true
	}
	return m
}()

// Patterns are already uppercase, so IsKey uses them directly for
// case-insensitive matching without an additional ToUpper pass.

// sanitizeEqualsPatterns holds the lowercase "key=" forms used to detect and
// mask sensitive key=value pairs in logs and errors. It is DERIVED from
// Patterns so the two lists cannot drift: the hand-maintained copy was
// missing ENCRYPTION_KEY, SIGN_KEY, VERIFY_KEY, SOCIAL_SECURITY, CCV, PAN,
// PRIVATE_ADDRESS, CONN_STRING, AZURE_KEY, GCP_KEY and SERVICE_ACCOUNT, so
// "encryption_key=hunter2"-style pairs passed through SanitizeForLog
// unmasked. Over-masking from the mechanical derivation (e.g. "pan=" inside
// "japan=", "private=" on ordinary keys) follows the stated policy: masking
// is best-effort and erring toward [MASKED] is the safe direction.
var sanitizeEqualsPatterns = buildSanitizeEqualsPatterns()

func buildSanitizeEqualsPatterns() []string {
	out := make([]string, 0, len(Patterns)+2)
	for _, p := range Patterns {
		out = append(out, strings.ToLower(p)+"=")
	}
	// Deliberate extras beyond the key list: broader spellings the patterns
	// do not contain and connection-string fields commonly appearing under
	// otherwise non-sensitive key names.
	return append(out, "authorization=", "pwd=")
}

// sanitizePatterns extends sanitizeEqualsPatterns for SanitizeForLog with a
// colon-separated form of every entry (YAML and plain-text "password: ..."
// pairs — the '=' form alone left structured dumps unmasked) and the HTTP
// Authorization bearer scheme ("Bearer <token>"). Over-masking ordinary
// colon pairs (e.g. a config "seed: 42") is accepted: masking is
// best-effort and erring toward [MASKED] is the safe direction.
var sanitizePatterns = buildSanitizePatterns()

func buildSanitizePatterns() []string {
	patterns := make([]string, 0, 2*len(sanitizeEqualsPatterns)+1)
	patterns = append(patterns, sanitizeEqualsPatterns...)
	for _, p := range sanitizeEqualsPatterns {
		patterns = append(patterns, strings.TrimSuffix(p, "=")+":")
	}
	return append(patterns, "bearer ")
}

// IsKey determines if a key likely contains sensitive data.
// Uses byte-level comparison to avoid allocations from strings.ToUpper.
//
// SECURITY (SEC-006): short patterns in wordAnchoredPatterns (AUTH, PAN, PWD,
// USERNAME) are matched only at identifier boundaries, so ordinary words that
// embed them (AUTHOR, COMPANY, MONKEY, …) are no longer misclassified as
// sensitive; all other patterns still match as substrings.
func IsKey(key string) bool {
	if len(key) == 0 {
		return false
	}

	// Check each pattern using zero-allocation case-insensitive matching.
	// Word-anchored patterns are skipped here and re-checked below at
	// identifier boundaries.
	for _, pattern := range Patterns {
		if !wordAnchoredSet[pattern] && containsIgnoreCase(key, pattern) {
			return true
		}
	}

	// Word-anchored patterns: match only when the pattern occupies an
	// identifier boundary (preceded and followed by a non-identifier byte).
	for _, pattern := range wordAnchoredPatterns {
		if containsWordIgnoreCase(key, pattern) {
			return true
		}
	}
	return false
}

// isWordByte reports whether c continues an identifier run, so a pattern is
// "word-anchored" when it is bounded on both sides by non-word bytes. '_' is
// deliberately NOT a word byte: env-var naming uses '_' as a separator, so
// DB_AUTH / MYSQL_PWD / PAN_NUMBER must still match (the pattern follows an
// underscore), while AUTHOR / MONKEY must not (the pattern is followed by a
// letter).
func isWordByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// containsWordIgnoreCase reports whether s contains pattern (case-insensitive,
// ASCII only) at an identifier boundary: neither the byte before the match nor
// the byte after it is an identifier byte. The boundary check makes AUTH match
// "AUTH_TOKEN" and "DB_AUTH" but not "AUTHOR"/"AUTHORITY", and PWD match
// "MYSQL_PWD" but not "PWD" inside an arbitrary run.
func containsWordIgnoreCase(s, pattern string) bool {
	patternLen := len(pattern)
	sLen := len(s)
	if patternLen > sLen || patternLen == 0 {
		return false
	}

	for i := 0; i <= sLen-patternLen; i++ {
		// Preceding byte must be an identifier boundary.
		if i > 0 && isWordByte(s[i-1]) {
			continue
		}
		// Following byte must be an identifier boundary.
		end := i + patternLen
		if end < sLen && isWordByte(s[end]) {
			continue
		}
		// Case-insensitive ASCII comparison over the window.
		match := true
		for j := 0; j < patternLen; j++ {
			c := s[i+j]
			if c >= 0x80 {
				match = false
				break
			}
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

// containsIgnoreCase checks if s contains pattern (case-insensitive, ASCII only).
// Pattern must be uppercase. This function avoids any heap allocations for ASCII input.
// For non-ASCII input, falls back to strings.Contains + strings.ToUpper for correctness.
func containsIgnoreCase(s, pattern string) bool {
	patternLen := len(pattern)
	sLen := len(s)
	if patternLen > sLen || patternLen == 0 {
		return false
	}

	// ASCII-only fast path: slide window through s.
	// Non-ASCII detection is folded into the comparison loop to avoid a
	// separate O(n) pre-scan — for ASCII strings (the common case) this
	// halves the number of bytes examined.
	for i := 0; i <= sLen-patternLen; i++ {
		match := true
		for j := 0; j < patternLen; j++ {
			c := s[i+j]
			if c >= 0x80 {
				// Non-ASCII: fall back to Unicode-aware matching
				return strings.Contains(strings.ToUpper(s), pattern)
			}
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

// indexFoldASCII returns the byte offset of the first case-insensitive ASCII
// occurrence of pattern (which must be lowercase ASCII) in s at or after from,
// or -1 if there is none. Matching runs directly on s, so the returned index
// is always a valid index into the ORIGINAL string — unlike an index computed
// on a strings.ToLower copy, which can be misaligned when non-ASCII runes
// change length when lowercased. Non-ASCII bytes in s never match.
func indexFoldASCII(s, pattern string, from int) int {
	patternLen := len(pattern)
	if from < 0 {
		from = 0
	}
	for i := from; i+patternLen <= len(s); i++ {
		match := true
		for j := 0; j < patternLen; j++ {
			c := s[i+j]
			if c >= 0x80 {
				match = false
				break
			}
			if c >= 'A' && c <= 'Z' {
				c += 32
			}
			if c != pattern[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// maxValueDisplayLen is the maximum length of non-sensitive values displayed in logs.
const maxValueDisplayLen = 20

// MaskValue masks a value based on its sensitivity.
// This is a utility function for general value masking.
// Non-sensitive values run through SanitizeForLog, mirroring MaskInString's
// SEC-04 posture: truncation alone let "password=..." pairs pass unmasked.
//
// SECURITY (SEC-004): the sensitive branch previously rendered the exact
// length ("[MASKED:23 chars]") — a password-length oracle in logs and errors.
// It now renders the fixed "[MASKED]" form, matching SanitizeForLog, so the
// mask reveals neither the value nor its length.
func MaskValue(key, value string) string {
	if IsKey(key) {
		return "[MASKED]"
	}
	if len(value) <= maxValueDisplayLen {
		return SanitizeForLog(value)
	}
	return SanitizeForLog(value[:maxValueDisplayLen-3] + "...")
}

// MaskKey masks a key name for logging purposes.
func MaskKey(key string) string {
	return DefaultMaskKey(key)
}

// MaskInString masks potentially sensitive content in a string.
// Long strings are truncated to 50 characters, then key=value style secrets
// are masked via SanitizeForLog — truncation alone let "password=..." pairs
// through unmasked (SEC-04), leaving raw secrets in ValidationError.Value
// and ParseError.Content fields that Error() does not print but callers can
// still inspect via errors.As.
func MaskInString(s string) string {
	const maxLen = 50
	if len(s) > maxLen {
		s = s[:maxLen-3] + "..."
	}
	return SanitizeForLog(s)
}

// SanitizeForLog removes potentially sensitive information from a string.
// It scans for patterns that might indicate sensitive data and masks them.
//
// Value boundaries (GEN-001: values containing spaces were previously cut
// at the first space, leaving "password=my secret phrase" mostly unmasked):
//   - a quoted value ("..." or '...') is masked through its closing quote,
//     so interior spaces and separators stay covered;
//   - an unquoted value is masked to the end of the line or to the start of
//     the NEXT key=value / key: pair (an identifier at a word boundary
//     followed by '=' or ':'), whichever comes first — separators inside
//     the value itself ("password=a=b") keep masking.
//
// SECURITY: Pattern matching operates directly on the original bytes via
// indexFoldASCII. It must NOT precompute strings.ToLower(s) and reuse those
// indices: ToLower can change the byte length of non-ASCII input (e.g. U+023A
// expands from 2 to 3 bytes when lowercased), so indices derived from the
// lowercased copy point into the wrong region of the original string — and
// when they run past len(s), the replacement is silently skipped and the
// secret passes through unmasked.
func SanitizeForLog(s string) string {
	if len(s) == 0 {
		return s
	}

	// Find all replacements needed (pattern end position -> mask position)
	// Use a slice of structs to avoid map allocation for small number of matches
	type replacement struct {
		valueStart int // start of value to mask (after pattern)
		valueEnd   int // end of value to mask
	}
	var replacements []replacement

	// Find all pattern matches
	for _, pattern := range sanitizePatterns {
		patternLen := len(pattern)
		searchStart := 0

		for {
			idx := indexFoldASCII(s, pattern, searchStart)
			if idx == -1 {
				break
			}

			// Find the value after the pattern. Colon-separated (YAML)
			// forms allow horizontal whitespace between the colon and the
			// value; skip it so "password: hunter2" masks the same value
			// "password=hunter2" would.
			valueStart := idx + patternLen
			if pattern[patternLen-1] == ':' {
				for valueStart < len(s) && (s[valueStart] == ' ' || s[valueStart] == '\t') {
					valueStart++
				}
			}
			valueEnd := valueStart
			if valueStart < len(s) && (s[valueStart] == '"' || s[valueStart] == '\'') {
				// Quoted value: mask through the matching closing quote. A
				// newline means the quote never closed — stop there and let
				// the fallback region stand (values do not span lines).
				quote := s[valueStart]
				valueEnd++
				for valueEnd < len(s) && s[valueEnd] != quote && s[valueEnd] != '\n' {
					valueEnd++
				}
				if valueEnd < len(s) && s[valueEnd] == quote {
					valueEnd++ // include the closing quote in the mask
				}
			} else {
				// Unquoted value: mask to the end of the line or to the
				// start of the next key=value / key: pair — an '=' or ':'
				// whose preceding identifier begins after whitespace. An
				// interior separator with no such boundary ("password=a=b")
				// stays part of the masked value.
				for valueEnd < len(s) {
					c := s[valueEnd]
					if c == '\n' {
						break
					}
					if c == '=' || c == ':' {
						k := valueEnd - 1
						for k >= valueStart && isMaskWordByte(s[k]) {
							k--
						}
						if k >= valueStart && (s[k] == ' ' || s[k] == '\t') {
							// The next pair starts at the identifier, so the
							// value ends before the whitespace gap in front
							// of it — the key itself must stay visible.
							valueEnd = k
							break
						}
					}
					valueEnd++
				}
				// The whitespace run before the next pair belongs to the
				// gap, not the secret.
				for valueEnd > valueStart && (s[valueEnd-1] == ' ' || s[valueEnd-1] == '\t') {
					valueEnd--
				}
			}

			if valueEnd > valueStart {
				replacements = append(replacements, replacement{valueStart, valueEnd})
			}

			// Continue searching after this match
			searchStart = valueStart + 1
			if searchStart >= len(s) {
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

// isMaskWordByte reports whether c can appear in an identifier that
// SanitizeForLog treats as the key of a following key=value pair.
func isMaskWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' ||
		c == '_' || c == '-' || c == '.'
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
