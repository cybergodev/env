// Package main demonstrates audit logging with JSON, Log, and Channel handlers.
// Enable audit logging for compliance, security monitoring, and debugging.
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"

	"github.com/cybergodev/env"
)

func main() {
	demonstrateJSONAudit()

	demonstrateChannelAudit()
}

// demonstrateJSONAudit captures audit events as JSON lines in a buffer.
func demonstrateJSONAudit() {
	fmt.Println("=== JSON Audit Handler ===")
	var buf bytes.Buffer // in production, use a file

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditEnabled = true
	cfg.AuditHandler = env.NewJSONAuditHandler(&buf)

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// Operations are audited automatically.
	_ = loader.GetString("APP_NAME")         // read event
	_ = loader.Set("TEST_VAR", "test_value") // write event
	_ = loader.Delete("TEST_VAR")            // delete event

	fmt.Println("  Captured audit events:")
	fmt.Print(buf.String())
}

// demonstrateChannelAudit routes events to a channel for async processing.
func demonstrateChannelAudit() {
	fmt.Println("=== Channel Audit Handler ===")

	handler := env.NewCloseableChannelHandler(64)
	defer handler.Close()

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditEnabled = true
	cfg.AuditHandler = handler

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// Collect events in a goroutine.
	events := handler.Channel()
	done := make(chan int)
	go func() {
		count := 0
		for range events {
			count++
		}
		done <- count
	}()

	// Trigger some reads.
	_ = loader.GetString("APP_NAME")
	_ = loader.GetString("DB_HOST")

	// Close the handler to stop the channel, then print the count.
	_ = handler.Close()
	_ = loader.Close()
	fmt.Printf("  Received %d audit events\n", <-done)

	// Also show the Log handler for stderr-based audit output.
	demonstrateLogAudit()
}

// demonstrateLogAudit uses the standard log package for audit output.
func demonstrateLogAudit() {
	fmt.Println("\n=== Log Audit Handler ===")
	logger := log.New(os.Stderr, "[AUDIT] ", log.LstdFlags)

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditEnabled = true
	cfg.AuditHandler = env.NewLogAuditHandler(logger)

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	_ = loader.GetString("APP_NAME") // appears as [AUDIT] line on stderr
	fmt.Println("  (audit lines written to stderr)")
}
