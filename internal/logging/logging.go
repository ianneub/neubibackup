// Package logging manages backup log files with automatic cleanup.
package logging

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"neubibackup/internal/config"
)

const (
	// DefaultMaxLogFiles is used when the caller doesn't compute a per-schedule
	// retention. Roughly one daily backup for ~25 days.
	DefaultMaxLogFiles = 25

	// MaxLogFilesCap is the hard ceiling on retained logs.
	MaxLogFilesCap = 500

	logTimeFormat = "2006-01-02T15-04-05"
	logFileSuffix = ".log"
)

// CreateLogFile creates a new log file with the current timestamp.
// Returns the opened file which the caller must close when done.
func CreateLogFile() (*os.File, error) {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return nil, fmt.Errorf("get logs dir: %w", err)
	}

	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	filename := time.Now().Format(logTimeFormat) + logFileSuffix
	logPath := filepath.Join(logsDir, filename)

	file, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	return file, nil
}

// CleanupOldLogs removes all but the most recent maxFiles log files.
// If maxFiles <= 0, it falls back to DefaultMaxLogFiles.
func CleanupOldLogs(maxFiles int) error {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxLogFiles
	}
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return fmt.Errorf("get logs dir: %w", err)
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read logs dir: %w", err)
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
		logPath := filepath.Join(logsDir, logFiles[i])
		if err := os.Remove(logPath); err != nil {
			return fmt.Errorf("remove old log %s: %w", logFiles[i], err)
		}
	}

	return nil
}

// RetentionFor computes the number of log files to retain so that frequent
// backups (e.g. hourly) keep ~7 days of logs while daily backups keep the
// historical default of 25. Capped at MaxLogFilesCap.
func RetentionFor(minGap time.Duration) int {
	if minGap <= 0 {
		return DefaultMaxLogFiles
	}
	week := (7 * 24 * time.Hour).Seconds()
	want := int(math.Ceil(week / minGap.Seconds()))
	if want < DefaultMaxLogFiles {
		want = DefaultMaxLogFiles
	}
	if want > MaxLogFilesCap {
		want = MaxLogFilesCap
	}
	return want
}

// GetLogPath returns the full path for a log file given its filename.
func GetLogPath(filename string) (string, error) {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(logsDir, filename), nil
}
