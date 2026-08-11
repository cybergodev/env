// Package main demonstrates concurrent access to a Loader.
// All Loader methods are safe for concurrent use via sharded locking.
package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/cybergodev/env"
)

func main() {
	cfg := env.DevelopmentConfig() // OverwriteExisting=true so Set always succeeds
	cfg.Filenames = []string{"examples/data/config.env"}

	loader, err := env.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}
	defer loader.Close()

	// Concurrent reads — many goroutines reading different keys simultaneously.
	demonstrateConcurrentReads(loader, 10)

	// Concurrent writes — goroutines setting different keys.
	demonstrateConcurrentWrites(loader, 10)

	// Mixed reads and writes on the same loader.
	demonstrateMixedAccess(loader, 20)

	fmt.Println("\nAll concurrent operations completed without races.")
}

func demonstrateConcurrentReads(loader *env.Loader, n int) {
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = loader.GetString("APP_NAME")
			_ = loader.GetInt("APP_PORT")
			_, _ = loader.Lookup("DB_HOST")
		}(i)
	}
	wg.Wait()
	fmt.Printf("  %d concurrent readers: OK\n", n)
}

func demonstrateConcurrentWrites(loader *env.Loader, n int) {
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("CONCURRENT_%d", id)
			if err := loader.Set(key, fmt.Sprintf("value_%d", id)); err != nil {
				log.Printf("Set %s: %v", key, err)
			}
		}(i)
	}
	wg.Wait()
	fmt.Printf("  %d concurrent writers: OK (%d total keys)\n", n, loader.Len())
}

func demonstrateMixedAccess(loader *env.Loader, n int) {
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(2)

		// Reader
		go func() {
			defer wg.Done()
			_ = loader.GetString("APP_NAME")
		}()

		// Writer
		go func(id int) {
			defer wg.Done()
			_ = loader.Set(fmt.Sprintf("MIXED_%d", id), "x")
		}(i)
	}
	wg.Wait()
	fmt.Printf("  %d mixed read/write goroutines: OK\n", n*2)
}
