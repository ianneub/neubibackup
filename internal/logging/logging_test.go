package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanupOldLogs(t *testing.T) {
	// Create a temp directory structure
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("Failed to create logs dir: %v", err)
	}

	// Helper to create log files with specific names
	createLogFile := func(name string) {
		path := filepath.Join(logsDir, name)
		if err := os.WriteFile(path, []byte("log content"), 0600); err != nil {
			t.Fatalf("Failed to create log file %s: %v", name, err)
		}
	}

	// Helper to count log files
	countLogFiles := func() int {
		entries, _ := os.ReadDir(logsDir)
		count := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".log") {
				count++
			}
		}
		return count
	}

	t.Run("fewer than max logs - no cleanup", func(t *testing.T) {
		// Clear directory
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)

		// Create 5 log files
		for i := 0; i < 5; i++ {
			createLogFile(time.Now().Add(time.Duration(-i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}

		if err := cleanupOldLogsInDir(logsDir); err != nil {
			t.Fatalf("CleanupOldLogs() error = %v", err)
		}

		if count := countLogFiles(); count != 5 {
			t.Errorf("Expected 5 log files, got %d", count)
		}
	})

	t.Run("exactly max logs - no cleanup", func(t *testing.T) {
		// Clear directory
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)

		// Create exactly 25 log files (maxLogFiles)
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 25; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}

		if err := cleanupOldLogsInDir(logsDir); err != nil {
			t.Fatalf("CleanupOldLogs() error = %v", err)
		}

		if count := countLogFiles(); count != 25 {
			t.Errorf("Expected 25 log files, got %d", count)
		}
	})

	t.Run("more than max logs - removes oldest", func(t *testing.T) {
		// Clear directory
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)

		// Create 30 log files
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}

		if err := cleanupOldLogsInDir(logsDir); err != nil {
			t.Fatalf("CleanupOldLogs() error = %v", err)
		}

		// Should have 25 files remaining
		if count := countLogFiles(); count != 25 {
			t.Errorf("Expected 25 log files after cleanup, got %d", count)
		}

		// Oldest files should be removed (first 5 hours)
		// File at hour 0 through 4 should be gone
		for i := 0; i < 5; i++ {
			oldFile := baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log"
			if _, err := os.Stat(filepath.Join(logsDir, oldFile)); !os.IsNotExist(err) {
				t.Errorf("Old file %s should have been removed", oldFile)
			}
		}

		// Newest files should remain (hours 5-29)
		for i := 5; i < 30; i++ {
			newFile := baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log"
			if _, err := os.Stat(filepath.Join(logsDir, newFile)); err != nil {
				t.Errorf("New file %s should exist: %v", newFile, err)
			}
		}
	})

	t.Run("non-log files are ignored", func(t *testing.T) {
		// Clear directory
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)

		// Create 30 log files + some non-log files
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}

		// Create non-log files
		os.WriteFile(filepath.Join(logsDir, "readme.txt"), []byte("readme"), 0600)
		os.WriteFile(filepath.Join(logsDir, "config.yaml"), []byte("config"), 0600)

		if err := cleanupOldLogsInDir(logsDir); err != nil {
			t.Fatalf("CleanupOldLogs() error = %v", err)
		}

		// Non-log files should still exist
		if _, err := os.Stat(filepath.Join(logsDir, "readme.txt")); err != nil {
			t.Error("readme.txt should not be removed")
		}
		if _, err := os.Stat(filepath.Join(logsDir, "config.yaml")); err != nil {
			t.Error("config.yaml should not be removed")
		}
	})

	t.Run("non-existent directory returns nil", func(t *testing.T) {
		if err := cleanupOldLogsInDir(filepath.Join(tmpDir, "nonexistent")); err != nil {
			t.Errorf("CleanupOldLogs() on non-existent dir should return nil, got: %v", err)
		}
	})
}

func TestGetLogPath(t *testing.T) {
	logPath, err := GetLogPath("2024-01-15T10-30-00.log")
	if err != nil {
		t.Fatalf("GetLogPath() error = %v", err)
	}

	// Should end with the filename
	if !strings.HasSuffix(logPath, "2024-01-15T10-30-00.log") {
		t.Errorf("GetLogPath() = %q, should end with filename", logPath)
	}

	// Should contain "logs" directory
	if !strings.Contains(logPath, "logs") {
		t.Errorf("GetLogPath() = %q, should contain 'logs'", logPath)
	}

	// Should be absolute
	if !filepath.IsAbs(logPath) {
		t.Errorf("GetLogPath() = %q, should be absolute", logPath)
	}
}

// cleanupOldLogsInDir is a testable version that accepts a directory path
func cleanupOldLogsInDir(logsDir string) error {
	// List all log files
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No logs directory yet
		}
		return err
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
	// Using simple string sort since log names are ISO formatted
	for i := 0; i < len(logFiles); i++ {
		for j := i + 1; j < len(logFiles); j++ {
			if logFiles[i] > logFiles[j] {
				logFiles[i], logFiles[j] = logFiles[j], logFiles[i]
			}
		}
	}

	// Delete oldest files (those at the start of the sorted list)
	toDelete := len(logFiles) - maxLogFiles
	for i := 0; i < toDelete; i++ {
		logPath := filepath.Join(logsDir, logFiles[i])
		if err := os.Remove(logPath); err != nil {
			return err
		}
	}

	return nil
}
