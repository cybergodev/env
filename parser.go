package env

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
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
	New: func() interface{} {
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
func putScannerBuffer(buf *[]byte) {
	if buf == nil || cap(*buf) > internal.MaxPooledScannerBufferSize {
		// Don't pool very large buffers
		return
	}
	// Clear sensitive data before returning to pool
	clear(*buf)
	scannerBufferPool.Put(buf)
}

// parser handles secure parsing of environment files.
type parser struct {
	config      Config
	validator   Validator
	auditor     FullAuditLogger
	lineParser  *internal.LineParser
	factory     *ComponentFactory
	ownsFactory bool
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
	p.configure(cfg, factory, false)
	return p, nil
}

// configure sets up the parser with the given configuration and factory.
// This is the shared initialization logic used by both newParserWithFactory and reset.
func (p *parser) configure(cfg Config, factory *ComponentFactory, ownsFactory bool) {
	validator := factory.Validator()
	auditor := factory.Auditor()

	lineParserCfg := internal.LineParserConfig{
		AllowExportPrefix: cfg.AllowExportPrefix,
		AllowYamlSyntax:   cfg.AllowYamlSyntax,
		OverwriteExisting: cfg.OverwriteExisting,
		MaxVariables:      cfg.MaxVariables,
		ExpandVariables:   cfg.ExpandVariables,
	}

	// Create LineParser with interfaces instead of concrete types.
	// The factory provides adapter methods that return internal interfaces.
	lp := internal.NewLineParser(lineParserCfg,
		factory.LineParserValidator(),
		factory.LineParserAuditor(),
		factory.LineParserExpander(),
	)
	// Set value validator if the validator also implements LineValueValidator
	if vv, ok := validator.(internal.LineValueValidator); ok {
		lp.SetValueValidator(vv)
	}

	p.config = cfg
	p.validator = validator
	p.auditor = auditor
	p.lineParser = lp
	p.factory = factory
	p.ownsFactory = ownsFactory
}

// Parse reads and parses environment variables from an io.Reader.
func (p *parser) Parse(r io.Reader, filename string) (map[string]string, error) {
	startTime := time.Now()

	// Wrap with secure reader
	secureRd := internal.NewSecureReader(r, p.config.MaxFileSize, p.config.MaxLineLength)
	scanner := bufio.NewScanner(secureRd)

	// Use pooled buffer for scanner to reduce allocations
	scannerBuf := getScannerBuffer()
	defer putScannerBuffer(scannerBuf)
	scanner.Buffer(*scannerBuf, cap(*scannerBuf))

	// Pre-allocate result map with optimal capacity
	// Estimate based on typical .env variable size: ~50-80 chars per variable
	// This reduces map growth operations for larger files
	// Use a conservative estimate: 1 variable per 60 characters
	initialCap := min(p.config.MaxVariables, 64)
	if seeker, ok := r.(interface{ Len() int }); ok {
		if size := seeker.Len(); size > 0 {
			estimatedVars := size / 60
			if estimatedVars > initialCap {
				initialCap = min(estimatedVars, p.config.MaxVariables)
			}
		}
	}
	result := make(map[string]string, initialCap)

	lineNum := 0
	var parseErr error

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

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
		key, value, err := p.lineParser.ParseLineBytes(line)
		if err != nil {
			parseErr = newParseError(filename, lineNum, key, err)
			break
		}

		if key == "" {
			continue
		}

		// Check max variables
		if len(result) >= p.config.MaxVariables {
			_ = p.auditor.LogError(internal.ActionParse, "", "maximum variables exceeded")
			parseErr = &ValidationError{
				Field:   "variables",
				Message: fmt.Sprintf("exceeded maximum of %d variables", p.config.MaxVariables),
			}
			break
		}

		// Handle overwrites
		if _, exists := result[key]; exists && !p.config.OverwriteExisting {
			_ = p.auditor.Log(internal.ActionParse, key, "duplicate key skipped", false)
			continue
		}

		result[key] = value
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

	// Validate required keys (using pooled map to reduce allocations)
	upperKeys := internal.KeysToUpperPooled(result)
	err := p.validator.ValidateRequired(upperKeys)
	internal.PutKeysToUpperMap(upperKeys)
	if err != nil {
		_ = p.auditor.LogError(internal.ActionValidate, "", err.Error())
		return nil, err
	}

	// Expand variables if enabled
	if p.config.ExpandVariables {
		expanded, err := p.lineParser.ExpandAll(result)
		if err != nil {
			return nil, err
		}
		result = expanded
	}

	_ = p.auditor.LogWithDuration(internal.ActionParse, "", "parsed: "+filename, true, time.Since(startTime))
	return result, nil
}

// Close releases resources held by the parser.
// If the parser owns its ComponentFactory, it will also close the factory.
func (p *parser) Close() error {
	if p.ownsFactory && p.factory != nil {
		return p.factory.Close()
	}
	return nil
}

// parseString parses environment variables from a string.
// This is an internal function used by UnmarshalMap.
// Uses a shared ComponentFactory to avoid per-call allocation overhead.
func parseString(s string) (map[string]string, error) {
	cfg := DefaultConfig()
	factory := getSharedParseFactory()

	p, err := newParserWithFactory(cfg, factory)
	if err != nil {
		return nil, err
	}

	result, err := p.Parse(strings.NewReader(s), "")
	return result, err
}

// sharedParseFactory caches a ComponentFactory for parseString to reuse.
// The factory's auditor handler is internal.DefaultHandler() (Nop), which is stateless and safe to share.
var (
	sharedParseFactory     *ComponentFactory
	sharedParseFactoryOnce sync.Once
)

// getSharedParseFactory returns a lazily-initialized shared ComponentFactory.
func getSharedParseFactory() *ComponentFactory {
	sharedParseFactoryOnce.Do(func() {
		cfg := DefaultConfig()
		sharedParseFactory = cfg.buildComponentFactory()
	})
	return sharedParseFactory
}
