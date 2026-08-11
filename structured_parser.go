package env

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cybergodev/env/internal"
)

// structuredParserConfig holds common configuration for structured file parsers (JSON/YAML).
type structuredParserConfig struct {
	config    Config
	validator Validator
	auditor   FullAuditLogger
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
	data, err := io.ReadAll(secureRd)
	if err != nil {
		if errors.Is(err, internal.ErrFileTooLarge) || errors.Is(err, internal.ErrLineTooLong) {
			_ = c.auditor.LogError(internal.ActionParse, "", "file exceeds size limit")
			return nil, &FileError{Path: filename, Op: "size_check", Err: err}
		}
		return nil, err
	}

	result, err := flattenFn(data)
	if err != nil {
		_ = c.auditor.LogError(internal.ActionParse, "", "invalid "+formatName)
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

// validateResult validates parsed key-value pairs from structured files (JSON/YAML).
func (c *structuredParserConfig) validateResult(result map[string]string, format string) error {
	if len(result) > c.config.MaxVariables {
		_ = c.auditor.LogError(internal.ActionParse, "", "maximum variables exceeded")
		return &ValidationError{
			Field:   "variables",
			Message: fmt.Sprintf("exceeded maximum of %d variables", c.config.MaxVariables),
		}
	}

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
		if c.config.ValidateValues {
			if err := c.validator.ValidateValue(val); err != nil {
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
