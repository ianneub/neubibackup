package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"neubibackup/internal/config"
)

const (
	appLogFilename    = "app.log"
	appLogOldFilename = "app.log.old"
	maxAppLogSize     = 1 * 1024 * 1024 // 1 MB
)

// appLogFile holds the open app log file handle.
var appLogFile *os.File

// rotatingWriter wraps a file and rotates it when it exceeds maxSize.
type rotatingWriter struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
	oldPath  string
	maxSize  int64
	size     int64
}

// Write implements io.Writer with automatic rotation.
func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if rotation is needed
	if w.size+int64(len(p)) > w.maxSize {
		w.rotate()
	}

	n, err = w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate closes the current file, renames it to .old, and creates a new one.
func (w *rotatingWriter) rotate() {
	// Close current file
	w.file.Close()

	// Remove old backup if it exists, then rename current to .old
	os.Remove(w.oldPath)
	os.Rename(w.filePath, w.oldPath)

	// Create new file
	file, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// If we can't create a new file, try to reopen the old one
		file, _ = os.OpenFile(w.oldPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	}
	w.file = file
	w.size = 0
}

// Sync flushes the file to disk.
func (w *rotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Sync()
	}
	return nil
}

// Close syncs and closes the underlying file.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		// Sync before closing to ensure all logs are flushed
		_ = w.file.Sync()
		return w.file.Close()
	}
	return nil
}

// appLogWriter is the rotating writer instance
var appLogWriter *rotatingWriter

// SetupAppLog configures application-wide logging to a persistent file.
// The log file automatically rotates when it exceeds 1MB, keeping one backup.
// This is especially useful on Windows where -H windowsgui hides console output.
// Returns a cleanup function that should be called when the app exits.
func SetupAppLog() (cleanup func(), err error) {
	appDir, err := config.GetAppDir()
	if err != nil {
		return nil, fmt.Errorf("get app dir: %w", err)
	}

	// Ensure app directory exists
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("create app dir: %w", err)
	}

	logPath := filepath.Join(appDir, appLogFilename)
	oldPath := filepath.Join(appDir, appLogOldFilename)

	// Get current file size if it exists
	var currentSize int64
	if info, err := os.Stat(logPath); err == nil {
		currentSize = info.Size()
	}

	// Open file for appending
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open app log: %w", err)
	}

	appLogFile = file
	appLogWriter = &rotatingWriter{
		file:     file,
		filePath: logPath,
		oldPath:  oldPath,
		maxSize:  maxAppLogSize,
		size:     currentSize,
	}

	// Configure log package to write to both rotating file and stderr
	// (stderr is useful when running from terminal, file is always available)
	log.SetOutput(io.MultiWriter(appLogWriter, os.Stderr))
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return func() {
		if appLogWriter != nil {
			appLogWriter.Close()
			appLogWriter = nil
		}
	}, nil
}

// GetAppLogPath returns the path to the application log file.
func GetAppLogPath() (string, error) {
	appDir, err := config.GetAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, appLogFilename), nil
}
