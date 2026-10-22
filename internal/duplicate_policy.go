// Provides the duplicate-key resolution policy shared by the .env, YAML
// and JSON parsers.

package internal

// DuplicateKeyPolicy controls how duplicate keys within a single source
// document are resolved. It does not govern cross-file merging (that is
// OverwriteExisting in the root package) nor the always-fatal flat-key
// collision check in the flatteners (see FlattenYAML/FlattenJSON).
//
// The zero value (DuplicateKeyInherit) preserves each format's historical
// behavior exactly, so existing configurations are unaffected:
//
//   - .env: follows OverwriteExisting — first occurrence wins when false
//     (the default), last occurrence wins when true;
//   - YAML: last occurrence wins (encoding order), regardless of
//     OverwriteExisting;
//   - JSON: last occurrence wins (encoding/json decode order), regardless of
//     OverwriteExisting.
type DuplicateKeyPolicy int

const (
	// DuplicateKeyInherit keeps each format's historical duplicate-key
	// behavior (see the type doc). This is the zero value and the default.
	DuplicateKeyInherit DuplicateKeyPolicy = iota
	// DuplicateKeyFirstWins keeps the first occurrence of a duplicate key.
	// Supported for .env and YAML documents. For JSON data — including
	// inline JSON inside YAML — a document whose duplicates are detected by
	// the pre-scan (see DuplicateKeyError) is rejected with an explicit
	// error, because encoding/json discards earlier occurrences during
	// decoding and the policy cannot be honored; JSON documents WITHOUT
	// duplicates load unchanged (they resolve identically under every
	// policy).
	DuplicateKeyFirstWins
	// DuplicateKeyLastWins lets the last occurrence of a duplicate key win
	// for every format, independent of OverwriteExisting.
	DuplicateKeyLastWins
	// DuplicateKeyError rejects any document containing two identical keys
	// in the same object/mapping (or the same .env file) with an error
	// matching ErrDuplicateKey via errors.Is. For JSON data — including
	// inline JSON inside YAML — duplicates are found by a conservative
	// structural pre-scan that compares raw key bytes; keys that decode to
	// the same text but are spelled with different escape sequences are not
	// detected and fall back to last-wins.
	DuplicateKeyError
)

// String returns a human-readable name for the policy.
func (p DuplicateKeyPolicy) String() string {
	switch p {
	case DuplicateKeyInherit:
		return "inherit"
	case DuplicateKeyFirstWins:
		return "first-wins"
	case DuplicateKeyLastWins:
		return "last-wins"
	case DuplicateKeyError:
		return "error"
	default:
		return "unknown"
	}
}
