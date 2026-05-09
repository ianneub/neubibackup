package logging

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCleanupOldLogs(t *testing.T) {
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("Failed to create logs dir: %v", err)
	}

	createLogFile := func(name string) {
		path := filepath.Join(logsDir, name)
		if err := os.WriteFile(path, []byte("log content"), 0600); err != nil {
			t.Fatalf("Failed to create log file %s: %v", name, err)
		}
	}

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

	t.Run("fewer than max - no cleanup", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		for i := 0; i < 5; i++ {
			createLogFile(time.Now().Add(time.Duration(-i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		if err := cleanupOldLogsInDir(logsDir, 25); err != nil {
			t.Fatalf("err = %v", err)
		}
		if got := countLogFiles(); got != 5 {
			t.Errorf("count = %d, want 5", got)
		}
	})

	t.Run("more than max - removes oldest", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		if err := cleanupOldLogsInDir(logsDir, 25); err != nil {
			t.Fatalf("err = %v", err)
		}
		if got := countLogFiles(); got != 25 {
			t.Errorf("count = %d, want 25", got)
		}
		for i := 0; i < 5; i++ {
			oldFile := baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log"
			if _, err := os.Stat(filepath.Join(logsDir, oldFile)); !os.IsNotExist(err) {
				t.Errorf("old file %s should have been removed", oldFile)
			}
		}
	})

	t.Run("non-log files are ignored", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		os.WriteFile(filepath.Join(logsDir, "readme.txt"), []byte("readme"), 0600)

		if err := cleanupOldLogsInDir(logsDir, 25); err != nil {
			t.Fatalf("err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(logsDir, "readme.txt")); err != nil {
			t.Error("readme.txt should not be removed")
		}
	})

	t.Run("non-existent dir returns nil", func(t *testing.T) {
		if err := cleanupOldLogsInDir(filepath.Join(tmpDir, "nonexistent"), 25); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("zero falls back to default", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		if err := cleanupOldLogsInDir(logsDir, 0); err != nil {
			t.Fatalf("err = %v", err)
		}
		if got := countLogFiles(); got != DefaultMaxLogFiles {
			t.Errorf("zero-arg count = %d, want %d", got, DefaultMaxLogFiles)
		}
	})
}

func TestRetentionFor(t *testing.T) {
	cases := []struct {
		gap  time.Duration
		want int
	}{
		{0, DefaultMaxLogFiles},
		{24 * time.Hour, 25},
		{12 * time.Hour, 25},
		{1 * time.Hour, 168},
		{30 * time.Minute, 336},
		{15 * time.Minute, MaxLogFilesCap},
		{1 * time.Minute, MaxLogFilesCap},
	}
	for _, tc := range cases {
		t.Run(tc.gap.String(), func(t *testing.T) {
			if got := RetentionFor(tc.gap); got != tc.want {
				t.Errorf("RetentionFor(%s) = %d, want %d", tc.gap, got, tc.want)
			}
		})
	}
}

func TestGetLogPath(t *testing.T) {
	logPath, err := GetLogPath("2024-01-15T10-30-00.log")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasSuffix(logPath, "2024-01-15T10-30-00.log") {
		t.Errorf("path = %q, want suffix %q", logPath, "2024-01-15T10-30-00.log")
	}
	if !strings.Contains(logPath, "logs") {
		t.Errorf("path = %q, want to contain 'logs'", logPath)
	}
	if !filepath.IsAbs(logPath) {
		t.Errorf("path = %q, want absolute", logPath)
	}
}

// cleanupOldLogsInDir is a testable copy of CleanupOldLogs that operates on an
// arbitrary directory rather than the global logs dir.
func cleanupOldLogsInDir(logsDir string, maxFiles int) error {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxLogFiles
	}
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var logFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), logFileSuffix) {
			logFiles = append(logFiles, entry.Name())
		}
	}

	if len(logFiles) <= maxFiles {
		return nil
	}
	sort.Strings(logFiles)

	toDelete := len(logFiles) - maxFiles
	for i := 0; i < toDelete; i++ {
		if err := os.Remove(filepath.Join(logsDir, logFiles[i])); err != nil {
			return err
		}
	}
	return nil
}
