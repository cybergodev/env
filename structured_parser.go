package env

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cybergodev/env/internal"
)

// utf8BOM is the UTF-8 byte-order mark stripped from structured documents
// before parsing (see structuredParseResult).
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// readerBufferPool provides reusable read buffers for structured parsers.
// io.ReadAll starts at 512 bytes and doubles, which costs ~a dozen growth
// allocations for typical config files; starting from a pooled 64KB buffer
// removes that churn entirely.
var readerBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, 64*1024)
		return &buf
	},
}

// maxPooledReaderBufferSize caps the buffers kept in readerBufferPool so a
// single huge file cannot inflate every pooled buffer permanently.
const maxPooledReaderBufferSize = 1024 * 1024

// getReaderBuffer retrieves a pooled read buffer.
func getReaderBuffer() *[]byte {
	buf, ok := readerBufferPool.Get().(*[]byte)
	if !ok {
		b := make([]byte, 0, 64*1024)
		return &b
	}
	return buf
}

// putReaderBuffer clears and returns a read buffer to the pool.
// SECURITY: the buffer holds raw (unmasked) file contents, which may include
// secrets — clear it before pooling, mirroring putScannerBuffer.
func putReaderBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	clear(*buf)
	if cap(*buf) <= maxPooledReaderBufferSize {
		readerBufferPool.Put(buf)
	}
}

// readAllInto reads r fully into buf's capacity, growing it as needed, and
// returns the accumulated slice. It is io.ReadAll seeded with a pooled buffer.
// On error it returns the partially-filled buffer alongside the error: the
// bytes read so far live in the caller's pooled array and must be cleared
// before that buffer is reused (see putReaderBuffer).
func readAllInto(buf []byte, r io.Reader) ([]byte, error) {
	for {
		if len(buf) == cap(buf) {
			// Grow: double the capacity, starting from a sane floor.
			newCap := 2 * cap(buf)
			if newCap == 0 {
				newCap = 512
			}
			grown := make([]byte, len(buf), newCap)
			copy(grown, buf)
			// SECURITY (SEC-008): the old array holds raw, unmasked file
			// content (possibly secrets); clear it before it is discarded so a
			// plaintext copy does not linger in memory until GC.
			clear(buf)
			buf = grown
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err != nil {
			if err == io.EOF {
				return buf, nil
			}
			return buf, err
		}
	}
}

// structuredParserConfig holds common configuration for structured file parsers (JSON/YAML).
type structuredParserConfig struct {
	config    Config
	validator Validator
	auditor   FullAuditLogger
}

// newStructuredParserConfig assembles the shared structured-parser wiring
// from a Config and factory — previously copy-pasted in both format
// constructor wrappers.
func newStructuredParserConfig(cfg Config, factory *ComponentFactory) structuredParserConfig {
	return structuredParserConfig{
		config:    cfg,
		validator: factory.Validator(),
		auditor:   factory.Auditor(),
	}
}

// normalizeStructuredDepth applies the shared depth fallback for the
// JSONMaxDepth/YAMLMaxDepth fields (<= 0 means the structured default) —
// previously duplicated in both format constructors.
func normalizeStructuredDepth(depth int) int {
	if depth <= 0 {
		return defaultStructuredMaxDepth
	}
	return depth
}

// structuredParseResult wraps common SecureReader + validation logic for JSON and YAML parsers.
// flattenFn receives raw bytes and returns the flattened key-value map.
func (c *structuredParserConfig) structuredParseResult(
	r io.Reader, filename, formatName string,
	flattenFn func(data []byte) (map[string]string, error),
) (map[string]string, error) {
	// Only record start time when audit is enabled — avoids time.Now() syscall
	// in the common (audit-disabled) case, consistent with the .env parser.
	var start time.Time
	if c.config.AuditEnabled {
		start = time.Now()
	}

	secureRd := internal.NewSecureReader(r, c.config.MaxFileSize, c.config.MaxLineLength)
	// Return the pooled reader on every path, matching the .env parser
	// (parser.go). Without this, each JSON/YAML parse dropped one pooled
	// SecureReader struct instead of recycling it, and NewSecureReader's
	// "must be returned to the pool" contract was silently violated.
	defer internal.ReleaseSecureReader(secureRd)
	bufPtr := getReaderBuffer()
	data, err := readAllInto((*bufPtr)[:0], secureRd)
	// Track the (possibly grown, possibly partially-filled) slice back through
	// the pool pointer on BOTH paths, so the deferred putReaderBuffer clears
	// and pools the full buffer. On error, data holds the bytes read before
	// the failure — raw, unmasked file contents. The previous error path
	// pooled the untouched original pointer instead, whose zero length made
	// clear() a no-op and left those secrets resident in the pool's array.
	*bufPtr = data
	defer putReaderBuffer(bufPtr)
	if err != nil {
		if errors.Is(err, internal.ErrFileTooLarge) || errors.Is(err, internal.ErrLineTooLong) {
			_ = c.auditor.LogError(internal.ActionParse, "", err.Error())
			return nil, &FileError{Path: filename, Op: "size_check", Err: err}
		}
		// Generic read failures were previously invisible to the audit
		// trail entirely (the .env parser logs its equivalents).
		_ = c.auditor.LogError(internal.ActionParse, "", "read failed: "+MaskSensitiveInString(err.Error()))
		return nil, err
	}

	// Strip a leading UTF-8 BOM (EF BB BF): Windows editors prepend one by
	// default; json.Unmarshal rejects a BOM-prefixed document outright and
	// the YAML lexer would fold the bytes into the first key. Quoted values
	// elsewhere in the document are unaffected — only the byte-order mark
	// at offset 0 is removed.
	data = bytes.TrimPrefix(data, utf8BOM)

	result, err := flattenFn(data)
	if err != nil {
		// Carry the cause (masked): an operator relying on the audit stream
		// previously saw a bare "invalid JSON"/"invalid YAML" with no hint
		// whether it was syntax, a flat-key collision, or depth.
		_ = c.auditor.LogError(internal.ActionParse, "", "invalid "+formatName+": "+MaskSensitiveInString(err.Error()))
		return nil, err
	}

	if err := c.validateResult(result, formatName); err != nil {
		return nil, err
	}

	if c.config.AuditEnabled {
		_ = c.auditor.LogWithDuration(internal.ActionParse, "", "parsed "+formatName+": "+filename, true, time.Since(start))
	}
	return result, nil
}

// keyPolicyValidator is an optional capability of validators that can check a
// key against the allowed-keys / forbidden-keys policy independently of the
// format-specific key pattern. It is defined here (consumer side) so existing
// external Validator implementations keep compiling; they simply opt out of
// the structured-format policy check.
type keyPolicyValidator interface {
	ValidateKeyPolicy(key string) error
}

// validateResult validates parsed key-value pairs from structured files (JSON/YAML).
func (c *structuredParserConfig) validateResult(result map[string]string, format string) error {
	if len(result) > c.config.MaxVariables {
		_ = c.auditor.LogError(internal.ActionParse, "", "maximum variables exceeded")
		return &ValidationError{
			Field:   "variables",
			Rule:    "max_variables",
			Message: fmt.Sprintf("exceeded maximum of %d variables", c.config.MaxVariables),
		}
	}

	// Structured formats use their own key pattern (IsValidJSONKey) but must
	// still enforce the allowed/forbidden key policy, mirroring the .env
	// parser's ValidateKey. Without this, a JSON/YAML file could set PATH or
	// LD_PRELOAD and have it applied to the process environment.
	policy, hasPolicy := c.validator.(keyPolicyValidator)

	for key, val := range result {
		// Enforce MaxKeyLength for structured formats, consistent with the
		// .env parser's ValidateKey which checks length first.
		if len(key) > c.config.MaxKeyLength {
			_ = c.auditor.LogError(internal.ActionParse, key, "key exceeds maximum length")
			return &ValidationError{
				Field:   "key",
				Value:   MaskSensitiveInString(key),
				Rule:    "max_length",
				Message: "key exceeds maximum length",
			}
		}
		if !internal.IsValidJSONKey(key) {
			_ = c.auditor.LogError(internal.ActionParse, key, "key does not match "+format+" key pattern")
			return &ValidationError{
				Field:   "key",
				Value:   MaskSensitiveInString(key),
				Rule:    "pattern",
				Message: "key does not match required pattern",
			}
		}
		if hasPolicy {
			if err := policy.ValidateKeyPolicy(key); err != nil {
				_ = c.auditor.LogError(internal.ActionParse, key, "key rejected by policy")
				return err
			}
		}
		if c.config.ValidateValues {
			if err := c.validator.ValidateValue(val); err != nil {
				_ = c.auditor.LogError(internal.ActionParse, key, err.Error())
				return err
			}
		} else if bv, ok := c.validator.(basicValueValidator); ok {
			// Fail-closed baseline: value checks run on structured loads even
			// with ValidateValues disabled, at parity with the .env parse
			// path (which validates unconditionally). For the built-in
			// validator the baseline equals the full check; UTF-8 follows the
			// ValidateUTF8 flag alone.
			if err := bv.ValidateValueBasic(val); err != nil {
				_ = c.auditor.LogError(internal.ActionParse, key, err.Error())
				return err
			}
		}
	}

	return validateRequiredKeys(c.validator, c.auditor, result)
}

// validateRequiredKeys is the shared required-key validation logic used by
// both the .env parser and the structured (JSON/YAML) parsers. It skips the
// uppercase-key index entirely when no required keys are configured.
func validateRequiredKeys(validator Validator, auditor FullAuditLogger, result map[string]string) error {
	if !needsRequiredCheck(validator) {
		return nil
	}
	upperKeys := internal.KeysToUpperPooled(result)
	err := validator.ValidateRequired(upperKeys)
	internal.PutKeysToUpperMap(upperKeys)
	if err != nil {
		_ = auditor.LogError(internal.ActionValidate, "", err.Error())
		return err
	}
	return nil
}
