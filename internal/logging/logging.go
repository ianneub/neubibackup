// Package logging manages backup log files with automatic cleanup.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"neubibackup/internal/config"
)

const (
	maxLogFiles    = 25
	logTimeFormat  = "2006-01-02T15-04-05"
	logFileSuffix  = ".log"
)

// CreateLogFile creates a new log file with the current timestamp.
// Returns the opened file which the caller must close when done.
func CreateLogFile() (*os.File, error) {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return nil, fmt.Errorf("get logs dir: %w", err)
	}

	// Ensure logs directory exists
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	// Generate filename with current timestamp
	filename := time.Now().Format(logTimeFormat) + logFileSuffix
	logPath := filepath.Join(logsDir, filename)

	// Create the log file
	file, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	return file, nil
}

// CleanupOldLogs removes all but the most recent maxLogFiles log files.
func CleanupOldLogs() error {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return fmt.Errorf("get logs dir: %w", err)
	}

	// List all log files
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No logs directory yet
		}
		return fmt.Errorf("read logs dir: %w", err)
	}

	// Filter to only .log files
	var logFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), logFileSuffix) {
			logFiles = append(logFiles, entry.Name())
		}
	}

	// If we have fewer than the max, nothing to do
	if len(logFiles) <= maxLogFiles {
		return nil
	}

	// Sort by name (which sorts by timestamp due to ISO format)
	sort.Strings(logFiles)

	// Delete oldest files (those at the start of the sorted list)
	toDelete := len(logFiles) - maxLogFiles
	for i := 0; i < toDelete; i++ {
		logPath := filepath.Join(logsDir, logFiles[i])
		if err := os.Remove(logPath); err != nil {
			return fmt.Errorf("remove old log %s: %w", logFiles[i], err)
		}
	}

	return nil
}

// GetLogPath returns the full path for a log file given its filename.
func GetLogPath(filename string) (string, error) {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(logsDir, filename), nil
}
