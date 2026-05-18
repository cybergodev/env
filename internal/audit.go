// Package internal provides audit logging functionality.
package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Compile-time checks that handlers implement io.Closer.
var (
	_ io.Closer = (*JSONHandler)(nil)
	_ io.Closer = (*LogHandler)(nil)
	_ io.Closer = (*ChannelHandler)(nil)
	_ io.Closer = (*NopHandler)(nil)
	_ io.Closer = (*CloseableChannelHandler)(nil)
	_ io.Closer = (*BufferedHandler)(nil)
	_ io.Closer = (*Auditor)(nil)
)

// Action represents the type of action being audited.
type Action string

// Audit action constants.
const (
	ActionLoad       Action = "load"
	ActionParse      Action = "parse"
	ActionGet        Action = "get"
	ActionSet        Action = "set"
	ActionDelete     Action = "delete"
	ActionValidate   Action = "validate"
	ActionExpand     Action = "expand"
	ActionSecurity   Action = "security"
	ActionError      Action = "error"
	ActionFileAccess Action = "file_access"
)

// Event represents a single audit log entry.
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Action    Action    `json:"action"`
	Key       string    `json:"key,omitempty"`
	File      string    `json:"file,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Success   bool      `json:"success"`
	Masked    bool      `json:"masked,omitempty"`
	Details   string    `json:"details,omitempty"`
	Duration  int64     `json:"duration_ns,omitempty"`
}

// Handler defines the interface for audit log handlers.
type Handler interface {
	Log(event Event) error
	Close() error
}

// JSONHandler writes audit events as JSON to an io.Writer.
type JSONHandler struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewJSONHandler creates a new JSONHandler.
func NewJSONHandler(w io.Writer) *JSONHandler {
	return &JSONHandler{writer: w}
}

// Log writes an audit event as JSON.
func (h *JSONHandler) Log(event Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal audit event: %w", err)
	}
	_, err = h.writer.Write(append(data, '\n'))
	return err
}

// Close implements Handler.
func (h *JSONHandler) Close() error {
	if closer, ok := h.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// LogHandler writes audit events using the standard log package.
type LogHandler struct {
	mu     sync.Mutex
	logger *log.Logger
}

// NewLogHandler creates a new LogHandler.
func NewLogHandler(logger *log.Logger) *LogHandler {
	if logger == nil {
		logger = log.New(os.Stderr, "[AUDIT] ", log.LstdFlags)
	}
	return &LogHandler{logger: logger}
}

// Log writes an audit event using the logger.
func (h *LogHandler) Log(event Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var msg string
	if event.Key != "" {
		msg = fmt.Sprintf("action=%s key=%s success=%v reason=%q",
			event.Action, event.Key, event.Success, event.Reason)
	} else {
		msg = fmt.Sprintf("action=%s success=%v reason=%q",
			event.Action, event.Success, event.Reason)
	}
	if event.File != "" {
		msg += fmt.Sprintf(" file=%s", event.File)
	}
	if event.Duration > 0 {
		if event.Duration < 1e6 {
			msg += fmt.Sprintf(" duration=%dμs", event.Duration/1e3)
		} else {
			msg += fmt.Sprintf(" duration=%.2fms", float64(event.Duration)/1e6)
		}
	}
	h.logger.Println(msg)
	return nil
}

// Close implements Handler.
func (h *LogHandler) Close() error {
	return nil
}

// ChannelHandler sends audit events to a channel.
//
// Channel Ownership: This handler does NOT own the channel. The caller is
// responsible for closing the channel when done. The handler's Close() method
// does nothing because closing a send-only channel (chan<-) would panic if
// the caller hasn't finished receiving.
//
// Blocking Behavior: This handler blocks if the channel buffer is full.
// Use a buffered channel if non-blocking behavior is required.
//
// Example:
//
//	ch := make(chan Event, 100)
//	handler := NewChannelHandler(ch)
//	// Start consumer goroutine
//	go func() {
//	    for event := range ch {
//	        process(event)
//	    }
//	}()
//	// ... use handler ...
//	handler.Close() // Does NOT close ch
//	close(ch)       // Caller must close the channel to signal EOF to receiver
type ChannelHandler struct {
	ch chan<- Event
}

// NewChannelHandler creates a new ChannelHandler that sends events to the
// provided channel. The caller retains ownership of the channel and must
// close it when finished to signal EOF to receivers.
func NewChannelHandler(ch chan<- Event) *ChannelHandler {
	return &ChannelHandler{ch: ch}
}

// Log sends an audit event to the channel.
// This method blocks if the channel is full.
func (h *ChannelHandler) Log(event Event) error {
	h.ch <- event
	return nil
}

// Close implements Handler.
// NOTE: This method does NOT close the underlying channel because the handler
// does not own it. The caller must close the channel separately.
func (h *ChannelHandler) Close() error {
	return nil
}

// CloseableChannelHandler sends audit events to a channel and owns the channel
// lifecycle. Unlike ChannelHandler, this handler creates its own channel and
// closes it when Close() is called.
//
// This is useful when you want the handler to manage the complete lifecycle
// of the channel, ensuring receivers are properly signaled when the handler
// is closed.
//
// Example:
//
//	handler := NewCloseableChannelHandler(100)
//	// Get the channel for receiving
//	ch := handler.Channel()
//	// Start consumer goroutine
//	go func() {
//	    for event := range ch {
//	        process(event)
//	    }
//	    fmt.Println("Channel closed, consumer exiting")
//	}()
//	// ... use handler ...
//	handler.Close() // Closes the channel, consumer goroutine exits gracefully
type CloseableChannelHandler struct {
	ch      chan Event
	done    chan struct{} // closed in Close() to unblock Log() without panic
	closeMu sync.Mutex
}

// NewCloseableChannelHandler creates a new CloseableChannelHandler with a
// buffered channel of the specified size. The handler owns the channel and
// will close it when Close() is called.
func NewCloseableChannelHandler(bufferSize int) *CloseableChannelHandler {
	if bufferSize < 0 {
		bufferSize = 0
	}
	return &CloseableChannelHandler{
		ch:   make(chan Event, bufferSize),
		done: make(chan struct{}),
	}
}

// Channel returns the underlying channel for receiving events.
// The returned channel will be closed when Close() is called.
func (h *CloseableChannelHandler) Channel() <-chan Event {
	return h.ch
}

// Log sends an audit event to the channel.
// Returns an error if the handler has been closed or the channel is full.
// Holds closeMu during the send to prevent Close() from closing the channel
// concurrently, which would cause a send-on-closed-channel panic.
func (h *CloseableChannelHandler) Log(event Event) error {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()

	select {
	case <-h.done:
		return fmt.Errorf("handler is closed")
	default:
	}

	select {
	case h.ch <- event:
		return nil
	default:
		return fmt.Errorf("audit channel full, event dropped")
	}
}

// Close implements Handler.
// Closes done first to unblock any waiting Log calls, then closes the event channel.
// Safe to call multiple times.
func (h *CloseableChannelHandler) Close() error {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()
	select {
	case <-h.done:
		return nil // Already closed
	default:
		close(h.done)
		close(h.ch)
		return nil
	}
}

// IsClosed returns true if the handler has been closed.
func (h *CloseableChannelHandler) IsClosed() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

// NopHandler discards all audit events.
type NopHandler struct{}

// NewNopHandler creates a new NopHandler.
func NewNopHandler() *NopHandler {
	return &NopHandler{}
}

// Log does nothing.
func (h *NopHandler) Log(event Event) error {
	return nil
}

// Close does nothing.
func (h *NopHandler) Close() error {
	return nil
}

// IsSensitiveFunc is a function type that determines if a key is sensitive.
type IsSensitiveFunc func(key string) bool

// MaskerFunc is a function type that masks a key-value pair.
type MaskerFunc func(key, value string) string

// Auditor provides audit logging functionality.
type Auditor struct {
	handler     Handler
	masker      MaskerFunc
	isSensitive IsSensitiveFunc
	enabled     atomic.Bool
	mu          sync.RWMutex
}

// auditEventPool provides a pool of reusable Event structs.
// This reduces allocations for high-frequency audit logging.
var auditEventPool = sync.Pool{
	New: func() any {
		return &Event{}
	},
}

// getAuditEvent retrieves an Event from the pool.
func getAuditEvent() *Event {
	ev, ok := auditEventPool.Get().(*Event)
	if !ok {
		return &Event{}
	}
	return ev
}

// putAuditEvent returns an Event to the pool after resetting it.
func putAuditEvent(ev *Event) {
	if ev == nil {
		return
	}
	// Clear the event to allow GC to collect referenced strings
	*ev = Event{}
	auditEventPool.Put(ev)
}

// NewAuditor creates a new Auditor with the specified handler.
func NewAuditor(handler Handler, isSensitive IsSensitiveFunc, masker MaskerFunc, enabled bool) *Auditor {
	if handler == nil {
		handler = NewNopHandler()
	}
	if isSensitive == nil {
		isSensitive = func(key string) bool { return false }
	}
	if masker == nil {
		masker = func(key, value string) string { return value }
	}
	a := &Auditor{
		handler:     handler,
		masker:      masker,
		isSensitive: isSensitive,
	}
	a.enabled.Store(enabled)
	return a
}

// Log records an audit event.
func (a *Auditor) Log(action Action, key, reason string, success bool) error {
	// Fast path: atomic read avoids lock acquisition when auditing is disabled.
	if !a.enabled.Load() {
		return nil
	}
	a.mu.RLock()
	// Re-check under RLock: SetEnabled acquires the write lock, so this ensures
	// we see a consistent state and don't log after a concurrent SetEnabled(false).
	if !a.enabled.Load() {
		a.mu.RUnlock()
		return nil
	}
	// Get pooled event and populate it
	event := getAuditEvent()
	event.Timestamp = time.Now()
	event.Action = action
	event.Key = a.maskKey(key)
	event.File = ""
	event.Reason = reason
	event.Success = success
	event.Details = ""
	event.Duration = 0
	event.Masked = key != "" && a.isSensitive(key)
	err := a.handler.Log(*event)
	putAuditEvent(event)
	a.mu.RUnlock()
	return err
}

// LogWithFile records an audit event with file information.
func (a *Auditor) LogWithFile(action Action, key, file, reason string, success bool) error {
	// Fast path: atomic read avoids lock acquisition when auditing is disabled.
	if !a.enabled.Load() {
		return nil
	}
	a.mu.RLock()
	// Re-check under RLock: SetEnabled acquires the write lock, so this ensures
	// we see a consistent state and don't log after a concurrent SetEnabled(false).
	if !a.enabled.Load() {
		a.mu.RUnlock()
		return nil
	}
	event := getAuditEvent()
	event.Timestamp = time.Now()
	event.Action = action
	event.Key = a.maskKey(key)
	event.File = file
	event.Reason = reason
	event.Success = success
	event.Masked = key != "" && a.isSensitive(key)
	err := a.handler.Log(*event)
	putAuditEvent(event)
	a.mu.RUnlock()
	return err
}

// LogWithDuration records an audit event with timing information.
func (a *Auditor) LogWithDuration(action Action, key, reason string, success bool, duration time.Duration) error {
	// Fast path: atomic read avoids lock acquisition when auditing is disabled.
	if !a.enabled.Load() {
		return nil
	}
	a.mu.RLock()
	// Re-check under RLock: SetEnabled acquires the write lock, so this ensures
	// we see a consistent state and don't log after a concurrent SetEnabled(false).
	if !a.enabled.Load() {
		a.mu.RUnlock()
		return nil
	}
	event := getAuditEvent()
	event.Timestamp = time.Now()
	event.Action = action
	event.Key = a.maskKey(key)
	event.Reason = reason
	event.Success = success
	event.Duration = duration.Nanoseconds()
	event.Masked = key != "" && a.isSensitive(key)
	err := a.handler.Log(*event)
	putAuditEvent(event)
	a.mu.RUnlock()
	return err
}

// LogError records an error event.
func (a *Auditor) LogError(action Action, key, errMsg string) error {
	return a.Log(action, key, errMsg, false)
}

// LogSecurity records a security-related event.
func (a *Auditor) LogSecurity(key, reason string) error {
	return a.Log(ActionSecurity, key, reason, false)
}

// SetEnabled enables or disables audit logging.
func (a *Auditor) SetEnabled(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled.Store(enabled)
}

// IsEnabled returns whether audit logging is enabled.
func (a *Auditor) IsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled.Load()
}

// Close closes the underlying handler.
func (a *Auditor) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.handler.Close()
}

// maskKey masks a key for audit logging.
func (a *Auditor) maskKey(key string) string {
	if key == "" {
		return ""
	}
	if a.isSensitive(key) {
		return a.masker(key, key)
	}
	return key
}

// DefaultHandler returns the default audit handler (writes to stderr).
func DefaultHandler() Handler {
	return NewLogHandler(nil)
}

// ============================================================================
// BufferedHandler
// ============================================================================

// BufferedHandlerConfig holds configuration for BufferedHandler.
type BufferedHandlerConfig struct {
	// Handler is the underlying handler to write to (required)
	Handler Handler

	// BufferSize is the maximum number of events to buffer before auto-flush
	// Default: 100
	BufferSize int

	// FlushInterval is the maximum time to wait before auto-flush
	// Set to 0 to disable time-based auto-flush
	// Default: 5 seconds
	FlushInterval time.Duration

	// OnError is called when an error occurs during flush
	// If nil, errors are silently ignored
	OnError func(error)
}

// BufferedHandler buffers audit events and writes them in batches.
// This significantly reduces I/O overhead for high-frequency operations.
//
// Features:
//   - Batched writes reduce system call overhead
//   - Time-based auto-flush ensures events are written promptly
//   - Thread-safe for concurrent use
//   - Graceful shutdown on Close() with guaranteed goroutine exit
//
// Example:
//
//	underlying := NewJSONHandler(file)
//	buffered := NewBufferedHandler(BufferedHandlerConfig{
//	    Handler:       underlying,
//	    BufferSize:    100,
//	    FlushInterval: 5 * time.Second,
//	})
//	defer buffered.Close()
type BufferedHandler struct {
	mu       sync.Mutex
	handler  Handler
	buffer   []Event
	size     int
	interval time.Duration
	onError  func(error)

	stopCh  chan struct{}
	flushCh chan struct{}
	stopped bool
	wg      sync.WaitGroup
}

// Default values for BufferedHandler.
const (
	DefaultBufferSize    = 100
	DefaultFlushInterval = 5 * time.Second
)

// NewBufferedHandler creates a new BufferedHandler with the given configuration.
func NewBufferedHandler(cfg BufferedHandlerConfig) *BufferedHandler {
	if cfg.Handler == nil {
		cfg.Handler = NewNopHandler()
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = DefaultBufferSize
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = DefaultFlushInterval
	}

	h := &BufferedHandler{
		handler:  cfg.Handler,
		buffer:   make([]Event, 0, cfg.BufferSize),
		size:     cfg.BufferSize,
		interval: cfg.FlushInterval,
		onError:  cfg.OnError,
		stopCh:   make(chan struct{}),
		flushCh:  make(chan struct{}, 1),
	}

	// Start background flush goroutine if interval is set
	if h.interval > 0 {
		h.wg.Add(1)
		go h.flushLoop()
	}

	return h
}

// flushLoop periodically flushes the buffer.
func (h *BufferedHandler) flushLoop() {
	defer h.wg.Done()
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.Flush()
		case <-h.flushCh:
			// Manual flush requested
			h.Flush()
		}
	}
}

// Log adds an event to the buffer.
// If the buffer is full, it triggers an automatic flush.
func (h *BufferedHandler) Log(event Event) error {
	h.mu.Lock()

	if h.stopped {
		h.mu.Unlock()
		return fmt.Errorf("buffered handler is closed")
	}

	h.buffer = append(h.buffer, event)
	shouldFlush := len(h.buffer) >= h.size

	h.mu.Unlock()

	// Flush outside of lock to allow concurrent Log() calls
	if shouldFlush {
		return h.Flush()
	}
	return nil
}

// Flush writes all buffered events to the underlying handler.
// It clears the buffer after successful write.
// This method is safe for concurrent use.
func (h *BufferedHandler) Flush() error {
	h.mu.Lock()

	if len(h.buffer) == 0 {
		h.mu.Unlock()
		return nil
	}

	// Take ownership of buffer and create new one
	events := h.buffer
	h.buffer = make([]Event, 0, h.size)

	// Release lock before I/O to allow concurrent Log() calls
	h.mu.Unlock()

	var lastErr error
	for _, event := range events {
		if err := h.handler.Log(event); err != nil {
			lastErr = err
			if h.onError != nil {
				h.onError(err)
			}
		}
	}

	return lastErr
}

// RequestFlush signals that a flush should be performed soon.
// This is useful for triggering a flush from another goroutine
// without waiting for the flush to complete.
func (h *BufferedHandler) RequestFlush() {
	select {
	case h.flushCh <- struct{}{}:
	default:
		// Flush already requested
	}
}

// Close flushes any remaining events and stops the background flush goroutine.
func (h *BufferedHandler) Close() error {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil
	}
	h.stopped = true
	h.mu.Unlock()

	// Stop background goroutine
	close(h.stopCh)
	h.wg.Wait()

	// Final flush
	if err := h.Flush(); err != nil {
		// Still close underlying handler even if flush fails
		_ = h.handler.Close()
		return err
	}

	return h.handler.Close()
}

// BufferLen returns the current number of events in the buffer.
func (h *BufferedHandler) BufferLen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.buffer)
}

// IsFull returns true if the buffer is at capacity.
func (h *BufferedHandler) IsFull() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.buffer) >= h.size
}
