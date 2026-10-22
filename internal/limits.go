// Provides security limits for the env package.

package internal

// ============================================================================
// Default Security Limits
// ============================================================================

// Default security limits for high-security configurations.
// These values are intentionally conservative to prevent various attacks.
const (
	// DefaultMaxFileSize is the maximum allowed file size (8 MB).
	// Kept self-consistent with the per-variable limits: a document at
	// DefaultMaxVariables entries of DefaultMaxValueLength bytes each
	// (~8.2MB with keys and syntax) always fits, so a size-legal-by-
	// per-variable document never fails the file-size check instead.
	DefaultMaxFileSize int64 = 8 * 1024 * 1024

	// DefaultMaxLineLength is the maximum allowed line length.
	// Sized for single-line values that carry long credentials (browser
	// cookie exports, JWTs), which commonly reach 2-8KB; the hard limit
	// (HardMaxLineLength) remains the actual DoS ceiling.
	DefaultMaxLineLength int = 8 * 1024

	// DefaultMaxKeyLength is the maximum allowed key length.
	// Structured formats flatten nested documents into compound keys
	// (A_B_C_D...); 8 realistic path segments already produce 65-128
	// chars, so the default must accommodate them.
	DefaultMaxKeyLength int = 128

	// DefaultMaxValueLength is the maximum allowed value length.
	// On .env parse paths the LINE check is the binding constraint: a line
	// is KEY + '=' + value, so the reachable file value ceiling is
	// DefaultMaxLineLength - len(key) - 1; the full DefaultMaxValueLength
	// is reachable only via Loader.Set (long credentials: cookie exports,
	// JWTs — 2-8KB is common).
	DefaultMaxValueLength int = 8 * 1024

	// DefaultMaxVariables is the maximum number of variables per file.
	// Sized for credential-export files (one domain/entry per variable)
	// and large structured configs, which reach into the hundreds.
	DefaultMaxVariables int = 1000

	// DefaultMaxExpansionDepth is the maximum variable expansion depth.
	DefaultMaxExpansionDepth int = 5
)

// ============================================================================
// Hard Security Limits
// ============================================================================

// Hard security limits that cannot be exceeded.
// These are absolute maximums for safety and are used across all packages.
const (
	// HardMaxFileSize is the absolute maximum file size (100 MB).
	HardMaxFileSize int64 = 100 * 1024 * 1024

	// HardMaxLineLength is the absolute maximum line length.
	HardMaxLineLength int = 64 * 1024

	// HardMaxKeyLength is the absolute maximum key length.
	HardMaxKeyLength int = 1024

	// HardMaxValueLength is the absolute maximum value length.
	HardMaxValueLength int = 1024 * 1024

	// HardMaxVariables is the absolute maximum variables per file.
	HardMaxVariables int = 10000

	// HardMaxExpansionDepth is the absolute maximum expansion depth.
	HardMaxExpansionDepth int = 20

	// HardMaxYAMLTokens is the absolute maximum number of tokens the YAML
	// lexer will produce for a single document. Each token costs ~56 bytes,
	// so a file of tiny lines ("a\n") amplifies input bytes ~56× into token
	// memory — before any MaxVariables check runs (SEC-02, CWE-400). This
	// cap makes Tokenize fail fast instead of letting a size-legal file
	// allocate gigabytes. 200K tokens ≈ 11 MB, roughly 20× what a document
	// at the HardMaxVariables ceiling needs.
	HardMaxYAMLTokens int = 200_000

	// HardMaxJSONNodes is the absolute maximum number of structural JSON
	// nodes (opening brackets, commas, colons — a proxy for decoded values)
	// accepted per document. json.Unmarshal into interface{} can amplify
	// input bytes up to ~48× into parse-tree memory (SEC-02, CWE-400); the
	// pre-scan rejects oversized documents before that allocation happens.
	HardMaxJSONNodes int = 200_000
)

// Pool size limits for sync.Pool objects.
// Objects larger than these limits are not returned to pools to prevent
// memory bloat from holding onto large allocations.
const (
	// MaxPooledScannerBufferSize is the maximum capacity for pooled scanner buffers.
	// Buffers larger than this are discarded instead of pooled.
	MaxPooledScannerBufferSize = 256 * 1024 // 256KB

	// MaxPooledByteSliceSize is the maximum capacity for pooled byte slices.
	// Slices larger than this are discarded instead of pooled.
	MaxPooledByteSliceSize = 4096 // 4KB

	// MaxPooledMapSize is the maximum size for pooled map objects.
	// Maps with more entries than this are discarded instead of pooled.
	MaxPooledMapSize = 128

	// MaxPooledYAMLTokenCount is the maximum capacity for pooled YAML token
	// slices (each token is ~56 bytes). 8192 tokens (~450KB) covers documents
	// several times larger than the DefaultMaxVariables (1000) ceiling, which
	// produces roughly 3000 tokens; larger tokenizations allocate fresh slices.
	MaxPooledYAMLTokenCount = 8192
)
