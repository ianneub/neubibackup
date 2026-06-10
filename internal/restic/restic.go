// Package restic provides embedded restic binary management and execution.
package restic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	extractedPath string
	extractOnce   sync.Once
	extractErr    error
)

// GetBinaryPath returns the path to the extracted restic binary.
// The binary is extracted once and cached for subsequent calls.
func GetBinaryPath() (string, error) {
	extractOnce.Do(func() {
		extractedPath, extractErr = extractBinary()
	})
	return extractedPath, extractErr
}

func extractBinary() (string, error) {
	if len(resticBinary) == 0 {
		return "", fmt.Errorf("restic binary not embedded for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Create a unique filename based on binary hash
	hash := sha256.Sum256(resticBinary)
	hashStr := hex.EncodeToString(hash[:8])

	// Determine binary name
	binaryName := "restic"
	if runtime.GOOS == "windows" {
		binaryName = "restic.exe"
	}

	// Extract to temp directory
	tempDir := os.TempDir()
	extractDir := filepath.Join(tempDir, "neubibackup-restic-"+hashStr)

	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("create extract dir: %w", err)
	}

	binaryPath := filepath.Join(extractDir, binaryName)

	// Check if already extracted
	if info, err := os.Stat(binaryPath); err == nil && info.Size() == int64(len(resticBinary)) {
		return binaryPath, nil
	}

	// Write binary
	if err := os.WriteFile(binaryPath, resticBinary, 0755); err != nil {
		return "", fmt.Errorf("write binary: %w", err)
	}

	return binaryPath, nil
}

// Version returns the restic version string.
const Version = "0.19.0"
