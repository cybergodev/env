package env

import "github.com/cybergodev/env/internal"

// DuplicateKeyPolicy controls how duplicate keys within a single source
// document are resolved, selected per Loader via ParsingConfig.
// DuplicateKeyPolicy. It is an alias of the internal policy type so values
// flow between Config and the parsers without conversion.
//
// Two related behaviors are NOT governed by this policy:
//
//   - Cross-file merging follows file order and OverwriteExisting: with the
//     default (false) the first file to set a key wins; with true, later
//     files override earlier ones (any formats — the flattened key spaces
//     are unified and case-normalized to uppercase).
//   - Flat-key collisions in YAML/JSON are ALWAYS an error: when two
//     different source paths collapse onto the same flattened key (e.g.
//     "a_b" next to "a: {b: 2}", or a literal "a_0" next to an array "a"),
//     the previous winner depended on map iteration order and could differ
//     between runs. The error matches ErrDuplicateKey via errors.Is.
//
// Case note: YAML/JSON keys are uppercased when flattened, while .env keys
// are stored as written — so keys differing only in case (a .env "db_host"
// and a YAML "db: {host: ...}" → "DB_HOST") do NOT collide and coexist in
// the store. Lookups try the exact key first, then its uppercase form (see
// Lookup).
type DuplicateKeyPolicy = internal.DuplicateKeyPolicy

const (
	// DuplicateKeyInherit keeps each format's historical duplicate-key
	// behavior. This is the zero value and the default:
	//   - .env: follows OverwriteExisting (first wins when false, last wins
	//     when true);
	//   - YAML: last occurrence wins;
	//   - JSON: last occurrence wins (encoding/json decode order).
	DuplicateKeyInherit = internal.DuplicateKeyInherit

	// DuplicateKeyFirstWins keeps the first occurrence of a duplicate key
	// for .env and YAML documents. For JSON data (including inline JSON
	// inside YAML), a document whose duplicates are detected by the same
	// pre-scan DuplicateKeyError uses is rejected with an explicit error —
	// encoding/json discards earlier occurrences during decoding, so the
	// policy cannot be honored for it. JSON documents WITHOUT duplicates
	// load unchanged: they resolve identically under every policy.
	DuplicateKeyFirstWins = internal.DuplicateKeyFirstWins

	// DuplicateKeyLastWins lets the last occurrence of a duplicate key win
	// for every format, independent of OverwriteExisting.
	DuplicateKeyLastWins = internal.DuplicateKeyLastWins

	// DuplicateKeyError rejects any document containing duplicate keys in
	// the same object/mapping (or the same .env file). The returned error
	// matches ErrDuplicateKey via errors.Is. For JSON data — including
	// inline JSON inside YAML — duplicates are found by a conservative
	// structural pre-scan that compares raw key bytes; keys that decode to
	// the same text but are spelled with different escape sequences are not
	// detected and fall back to last-wins.
	DuplicateKeyError = internal.DuplicateKeyError
)
