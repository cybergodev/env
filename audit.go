package env

import (
	"io"
	"log"

	"github.com/cybergodev/env/internal"
)

// ============================================================================
// Audit Types (re-exported from internal/audit for backward compatibility)
// ============================================================================

// AuditAction represents the type of action being audited.
// Use these constants with AuditLogger.Log() to record security-relevant events.
type AuditAction = internal.Action

// Audit constants for common actions.
// These are used with AuditLogger methods to categorize audit events:
//   - ActionLoad: File loading operations
//   - ActionParse: Parsing operations for env, JSON, YAML files
//   - ActionGet: Variable retrieval operations
//   - ActionSet: Variable assignment operations
//   - ActionDelete: Variable deletion operations
//   - ActionValidate: Validation operations
//   - ActionExpand: Variable expansion operations
//   - ActionSecurity: Security-related events (path validation, forbidden keys)
//   - ActionError: Error conditions
//   - ActionFileAccess: File system access operations
const (
	ActionLoad       AuditAction = internal.ActionLoad
	ActionParse      AuditAction = internal.ActionParse
	ActionGet        AuditAction = internal.ActionGet
	ActionSet        AuditAction = internal.ActionSet
	ActionDelete     AuditAction = internal.ActionDelete
	ActionValidate   AuditAction = internal.ActionValidate
	ActionExpand     AuditAction = internal.ActionExpand
	ActionSecurity   AuditAction = internal.ActionSecurity
	ActionError      AuditAction = internal.ActionError
	ActionFileAccess AuditAction = internal.ActionFileAccess
)

// AuditEvent represents a single audit log entry.
type AuditEvent = internal.Event

// AuditHandler defines the interface for audit log handlers.
type AuditHandler = internal.Handler

// JSONAuditHandler writes audit events as JSON to an io.Writer.
type JSONAuditHandler = internal.JSONHandler

// NewJSONAuditHandler creates a new JSONAuditHandler that writes audit events
// as JSON lines to the provided writer.
//
// Example:
//
//	handler := env.NewJSONAuditHandler(os.Stdout)
//	cfg := env.DefaultConfig()
//	cfg.ComponentConfig.AuditHandler = handler
//	cfg.ComponentConfig.AuditEnabled = true
//	loader, _ := env.New(cfg)
func NewJSONAuditHandler(w io.Writer) *JSONAuditHandler {
	return internal.NewJSONHandler(w)
}

// LogAuditHandler writes audit events using the standard log package.
type LogAuditHandler = internal.LogHandler

// NewLogAuditHandler creates a new LogAuditHandler that writes audit events
// using the standard log package.
//
// Example:
//
//	logger := log.New(os.Stderr, "[AUDIT] ", log.LstdFlags)
//	handler := env.NewLogAuditHandler(logger)
//	cfg.ComponentConfig.AuditHandler = handler
func NewLogAuditHandler(logger *log.Logger) *LogAuditHandler {
	return internal.NewLogHandler(logger)
}

// ChannelAuditHandler sends audit events to a channel.
type ChannelAuditHandler = internal.ChannelHandler

// NewChannelAuditHandler creates a new ChannelAuditHandler that sends audit events
// to the provided channel. The caller is responsible for managing the channel lifecycle.
//
// Example:
//
//	ch := make(chan env.AuditEvent, 100)
//	handler := env.NewChannelAuditHandler(ch)
//	go func() {
//	    for event := range ch {
//	        fmt.Println(event)
//	    }
//	}()
func NewChannelAuditHandler(ch chan<- AuditEvent) *ChannelAuditHandler {
	return internal.NewChannelHandler(ch)
}

// CloseableChannelHandler sends audit events to an owned channel with lifecycle management.
// Unlike ChannelAuditHandler which accepts an external channel, CloseableChannelHandler
// creates and owns its own buffered channel. Call Close() to shut down the handler and
// close the channel. Use Channel() to receive events.
type CloseableChannelHandler = internal.CloseableChannelHandler

// NewCloseableChannelHandler creates a new CloseableChannelHandler with a buffered
// channel of the specified size. The handler owns the channel and will close it
// when Close() is called.
//
// Example:
//
//	handler := env.NewCloseableChannelHandler(64)
//	defer handler.Close()
//	go func() {
//	    for event := range handler.Channel() {
//	        fmt.Println(event)
//	    }
//	}()
func NewCloseableChannelHandler(bufferSize int) *CloseableChannelHandler {
	return internal.NewCloseableChannelHandler(bufferSize)
}

// NopAuditHandler discards all audit events.
type NopAuditHandler = internal.NopHandler

// NewNopAuditHandler creates a new NopAuditHandler.
func NewNopAuditHandler() *NopAuditHandler {
	return internal.NewNopHandler()
}
