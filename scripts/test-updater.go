//go:build ignore

// Test script for the updater package.
// Run with: go run scripts/test-updater.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"neubibackup/internal/updater"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run scripts/test-updater.go <current-version> <owner> <repo>")
		fmt.Println("Example: go run scripts/test-updater.go v0.0.1 neubibackup neubibackup_go")
		os.Exit(1)
	}

	currentVersion := os.Args[1]
	owner := os.Args[2]
	repo := os.Args[3]

	fmt.Printf("Testing updater with:\n")
	fmt.Printf("  Current version: %s\n", currentVersion)
	fmt.Printf("  Repository: %s/%s\n", owner, repo)
	fmt.Println()

	u := updater.New(currentVersion, owner, repo)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Checking for updates...")
	newVersion, available, err := u.CheckForUpdate(ctx)
	if err != nil {
		log.Fatalf("Error checking for updates: %v", err)
	}

	if available {
		fmt.Printf("✅ Update available: %s\n", newVersion)
		fmt.Println()
		fmt.Println("To test the full update flow, you would need to:")
		fmt.Println("1. Build the app with the lower version")
		fmt.Println("2. Run it and click 'Check for Updates'")
		fmt.Println("3. Click to install the update")
	} else {
		fmt.Println("ℹ️  No update available (current version is latest or no releases found)")
	}
}
