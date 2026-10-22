package env

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// parser
// ============================================================================

// scannerBufferPool provides a pool of reusable byte slices for bufio.Scanner.
// This reduces allocations when parsing multiple files or strings.
// Each buffer is 64KB which is sufficient for most env files while being
// small enough to avoid excessive memory consumption. This size was chosen
// based on typical .env file sizes (rarely exceeding 64KB in practice).
var scannerBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 64*1024)
		return &buf
	},
}

// getScannerBuffer retrieves a byte slice from the pool for use with bufio.Scanner.
// Returns a fallback buffer if the pool returns an unexpected type.
func getScannerBuffer() *[]byte {
	buf, ok := scannerBufferPool.Get().(*[]byte)
	if !ok {
		// Fallback: create new buffer if pool returns unexpected type
		b := make([]byte, 64*1024)
		return &b
	}
	return buf
}

// putScannerBuffer returns a byte slice to the pool.
// used is the number of bytes the reader delivered into the buffer during
// this parse; only that prefix is cleared. The scanner fills the buffer
// sequentially (compacting within it when it needs room, or growing to a
// fresh slice beyond it), so bytes past min(used, len) never held file
// content and need not be zeroed — for a file smaller than the 64KB buffer
// this clears kilobytes instead of the full buffer.
//
// SECURITY: every byte that ever held file content IS cleared. The pool
// invariant is that a buffer is clean at rest: each user clears exactly the
// region it dirtied, and a freshly made buffer starts zeroed.
func putScannerBuffer(buf *[]byte, used int) {
	if buf == nil || cap(*buf) > internal.MaxPooledScannerBufferSize {
		// Don't pool very large buffers
		return
	}
	// Clear sensitive data before returning to pool
	if used > len(*buf) {
		used = len(*buf)
	}
	clear((*buf)[:used])
	scannerBufferPool.Put(buf)
}

// parser handles secure parsing of environment files.
type parser struct {
	config     Config
	validator  Validator
	auditor    FullAuditLogger
	lineParser *internal.LineParser
}

// Compile-time check that parser implements EnvParser.
var _ EnvParser = (*parser)(nil)

// newParserWithFactory creates a new parser with a ComponentFactory.
// This is more efficient as it reuses the same Validator, Auditor, and Expander
// instances instead of creating new ones.
// The caller is responsible for validating cfg before calling this function.
func newParserWithFactory(cfg Config, factory *ComponentFactory) (*parser, error) {
	if factory == nil {
		return nil, fmt.Errorf("factory cannot be nil")
	}

	p := &parser{}
	p.configure(cfg, factory)
	return p, nil
}

// configure sets up the parser with the given configuration and factory.
// This is the shared initialization logic used by newParserWithFactory.
func (p *parser) configure(cfg Config, factory *ComponentFactory) {
	validator := factory.Validator()
	auditor := factory.Auditor()

	lineParserCfg := internal.LineParserConfig{
		AllowExportPrefix: cfg.AllowExportPrefix,
		AllowYamlSyntax:   cfg.AllowYamlSyntax,
		ExpandVariables:   cfg.ExpandVariables,
	}

	// Create the LineParser with the adapter view of the validator
	// (factory.Validator(), not the raw internal field): NewLineParser wires a
	// key validator that also implements value validation automatically, so
	// the previous explicit SetValueValidator call only re-set the same
	// object through a second path.
	lp := internal.NewLineParser(lineParserCfg,
		validator,
		factory.auditor,
		factory.expander,
	)

	p.config = cfg
	p.validator = validator
	p.auditor = auditor
	p.lineParser = lp
}

// hasRequiredKeys is an optional capability implemented by validators that can
// cheaply report whether any required keys are configured. Parsers use it to
// skip building the uppercase-key index when there is nothing to validate.
//
// It is defined on the consumer side (here) rather than added to the public
// Validator interface, so existing external Validator implementations keep
// compiling without modification (they simply opt out of the fast path).
type hasRequiredKeys interface {
	HasRequiredKeys() bool
}

// needsRequiredCheck reports whether the validator has required keys to check.
// It returns true for validators that do not expose hasRequiredKeys, preserving
// the original always-validate behavior for custom implementations.
func needsRequiredCheck(v Validator) bool {
	if rc, ok := v.(hasRequiredKeys); ok {
		return rc.HasRequiredKeys()
	}
	return true
}

// Parse reads and parses environment variables from an io.Reader.
func (p *parser) Parse(r io.Reader, filename string) (map[string]string, error) {
	// Only record start time when audit is enabled — avoids time.Now() overhead
	// and string concatenation in the common (audit-disabled) case.
	var startTime time.Time
	auditEnabled := p.config.AuditEnabled
	if auditEnabled {
		startTime = time.Now()
	}

	// Wrap with secure reader
	secureRd := internal.NewSecureReader(r, p.config.MaxFileSize, p.config.MaxLineLength)
	defer internal.ReleaseSecureReader(secureRd)
	scanner := bufio.NewScanner(secureRd)

	// Use pooled buffer for scanner to reduce allocations.
	// The max token size is MaxPooledScannerBufferSize (256KB): every real
	// limit enforced by SecureReader (line length ≤ 64KB, file size ≤ 100MB
	// chunked) is below this, so the scanner never raises bufio.ErrTooLong
	// before the SecureReader's own ErrLineTooLong/ErrFileTooLarge surface.
	scannerBuf := getScannerBuffer()
	// Deferred as a closure so the byte count is read AFTER the scan loop:
	// only the region the scanner actually dirtied gets cleared on pool
	// return (see putScannerBuffer). Runs before ReleaseSecureReader (LIFO),
	// so the counters are still live.
	defer func() { putScannerBuffer(scannerBuf, int(secureRd.BytesDelivered())) }()
	scanner.Buffer(*scannerBuf, internal.MaxPooledScannerBufferSize)

	// Pre-allocate result map with optimal capacity
	// Estimate based on typical .env variable size: ~50-80 chars per variable
	// This reduces map growth operations for larger files
	// Use a conservative estimate: 1 variable per 60 characters
	initialCap := min(p.config.MaxVariables, 64)
	if sizer, ok := r.(interface{ Len() int }); ok {
		if size := sizer.Len(); size > 0 {
			estimatedVars := size / 60
			if estimatedVars > initialCap {
				initialCap = min(estimatedVars, p.config.MaxVariables)
			}
		}
	}
	result := make(map[string]string, initialCap)

	lineNum := 0
	var parseErr error
	// Keys whose current value came from a single-quoted literal: expansion
	// must skip them (bash '...' semantics) — see ParseLineBytesQuote.
	var singleQuotedKeys map[string]struct{}

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		// Strip a UTF-8 BOM from the first line: Windows editors (Notepad,
		// PowerShell Out-File) prepend one by default, and the resulting
		// "\uFEFF" + "KEY" previously failed key validation with a misleading
		// non-ASCII error on a line that visibly reads KEY=value.
		if lineNum == 1 && len(line) >= 3 && line[0] == 0xEF && line[1] == 0xBB && line[2] == 0xBF {
			line = line[3:]
		}

		// Fast path: skip empty lines and comments using byte-level operations
		// Combined trim loop for better cache locality
		start := 0
		end := len(line)
		for start < end && (line[start] == ' ' || line[start] == '\t') {
			start++
		}
		if start == end {
			continue // Empty line
		}
		for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
			end--
		}
		if start == end {
			continue // All whitespace
		}
		if line[start] == '#' {
			continue // Comment line
		}

		// Parse the line using byte-slice version to minimize allocations
		key, value, singleQuoted, err := p.lineParser.ParseLineBytesQuote(line)
		if err != nil {
			parseErr = newParseError(filename, lineNum, key, err)
			break
		}

		if key == "" {
			continue
		}

		// Check max variables — counting DISTINCT keys, matching the
		// structured parsers' semantics: a duplicate line the policy would
		// skip anyway previously tripped the limit at the exact boundary
		// and failed the whole file.
		if _, exists := result[key]; !exists && len(result) >= p.config.MaxVariables {
			_ = p.auditor.LogError(internal.ActionParse, "", "maximum variables exceeded")
			parseErr = &ValidationError{
				Field:   "variables",
				Rule:    "max_variables",
				Message: fmt.Sprintf("exceeded maximum of %d variables", p.config.MaxVariables),
			}
			break
		}

		// Handle duplicates per the configured DuplicateKeyPolicy. Inherit
		// (the default) keeps the historical behavior: skip when
		// OverwriteExisting is false, overwrite when true.
		if _, exists := result[key]; exists {
			if p.config.DuplicateKeyPolicy == DuplicateKeyError {
				_ = p.auditor.LogError(internal.ActionParse, key, "duplicate key")
				parseErr = newParseError(filename, lineNum, key, ErrDuplicateKey)
				break
			}
			skip := false
			switch p.config.DuplicateKeyPolicy {
			case DuplicateKeyFirstWins:
				skip = true
			case DuplicateKeyLastWins:
				// fall through to overwrite
			default: // DuplicateKeyInherit
				skip = !p.config.OverwriteExisting
			}
			if skip {
				_ = p.auditor.Log(internal.ActionParse, key, "duplicate key skipped", false)
				continue
			}
		}

		result[key] = value

		// Track quote provenance so expansion can exempt literal values.
		// Overwriting a key with an unquoted/double-quoted value clears the
		// exemption — the last write owns the value.
		if singleQuoted {
			if singleQuotedKeys == nil {
				singleQuotedKeys = make(map[string]struct{})
			}
			singleQuotedKeys[key] = struct{}{}
		} else if singleQuotedKeys != nil {
			delete(singleQuotedKeys, key)
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, ErrFileTooLarge) || errors.Is(err, ErrLineTooLong) {
			_ = p.auditor.LogError(internal.ActionParse, "", err.Error())
			return nil, err
		}
		return nil, newParseError(filename, lineNum, "", err)
	}

	if parseErr != nil {
		return nil, parseErr
	}

	// Validate required keys (shared logic with structured parsers).
	if err := validateRequiredKeys(p.validator, p.auditor, result); err != nil {
		return nil, err
	}

	// Expand variables if enabled. Single-quoted values are passed through
	// literally — expanding them silently corrupted secrets like
	// PASSWORD='my$ecret' (the $ecret reference resolved to empty).
	if p.config.ExpandVariables {
		expanded, err := p.lineParser.ExpandAll(result, singleQuotedKeys)
		if err != nil {
			// Wrap for file context in multi-file loads — every other
			// failure path in Parse reports the filename. ParseError.Unwrap
			// preserves errors.Is/As on the underlying *ExpansionError.
			return nil, newParseError(filename, 0, "", err)
		}
		result = expanded
	}

	if auditEnabled {
		_ = p.auditor.LogWithDuration(internal.ActionParse, "", "parsed: "+filename, true, time.Since(startTime))
	}
	return result, nil
}
