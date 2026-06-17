//go:build examples

// Package main demonstrates audit logging capabilities.
// Audit logging is essential for compliance and security monitoring.
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

	demonstrateLogAudit()

	demonstrateProductionAudit()
}

func demonstrateJSONAudit() {
	fmt.Println("=== JSON Audit Handler ===")
	// Create a buffer to capture JSON output
	// In production, use a file instead of bytes.Buffer
	var buf bytes.Buffer
	auditHandler := env.NewJSONAuditHandler(&buf)

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditEnabled = true
	cfg.AuditHandler = auditHandler

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// Trigger operations so the audit handler records them.
	// The GetString return value is intentionally unused — the call exists
	// only to produce an audit event.
	_ = loader.GetString("APP_NAME")
	if err := loader.Set("TEST_VAR", "test_value"); err != nil {
		log.Printf("Set failed: %v", err)
	}
	if err := loader.Delete("TEST_VAR"); err != nil {
		log.Printf("Delete failed: %v", err)
	}

	fmt.Println("Captured JSON audit logs:")
	fmt.Println(buf.String())
}

func demonstrateLogAudit() {
	fmt.Println("\n=== Log Audit Handler ===")
	// Use standard log for audit output
	logger := log.New(os.Stdout, "[AUDIT] ", log.LstdFlags)
	auditHandler := env.NewLogAuditHandler(logger)

	cfg := env.DefaultConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditEnabled = true
	cfg.AuditHandler = auditHandler

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// These reads are logged with the [AUDIT] prefix. Return values are
	// unused — the calls exist only to produce audit events.
	_ = loader.GetString("APP_NAME")
	_ = loader.GetString("DB_HOST")
}

func demonstrateProductionAudit() {
	fmt.Println("\n=== Production Audit Setup ===")
	// Production setup with file-based audit logging
	auditFile, err := os.CreateTemp("", "audit-*.json")
	if err != nil {
		log.Fatalf("Failed to create audit file: %v", err)
	}
	defer os.Remove(auditFile.Name())
	defer auditFile.Close()

	// ProductionConfig enables audit by default
	cfg := env.ProductionConfig()
	cfg.Filenames = []string{"examples/data/config.env"}
	cfg.AuditHandler = env.NewJSONAuditHandler(auditFile)

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	fmt.Printf("Audit logs written to: %s\n", auditFile.Name())
	fmt.Printf("Variables loaded: %d\n", loader.Len())
}
