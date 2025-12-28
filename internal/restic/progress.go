package restic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// BackupStatus represents a status update from restic during backup.
// Maps to restic's JSON output with message_type: "status"
type BackupStatus struct {
	MessageType      string   `json:"message_type"`
	PercentDone      float64  `json:"percent_done"`
	TotalFiles       int64    `json:"total_files"`
	FilesDone        int64    `json:"files_done"`
	TotalBytes       int64    `json:"total_bytes"`
	BytesDone        int64    `json:"bytes_done"`
	SecondsElapsed   int64    `json:"seconds_elapsed"`
	SecondsRemaining int64    `json:"seconds_remaining"`
	CurrentFiles     []string `json:"current_files"`
}

// BackupSummary represents the final summary from restic after backup completes.
// Maps to restic's JSON output with message_type: "summary"
type BackupSummary struct {
	MessageType         string  `json:"message_type"`
	FilesNew            int64   `json:"files_new"`
	FilesChanged        int64   `json:"files_changed"`
	FilesUnmodified     int64   `json:"files_unmodified"`
	DirsNew             int64   `json:"dirs_new"`
	DirsChanged         int64   `json:"dirs_changed"`
	DirsUnmodified      int64   `json:"dirs_unmodified"`
	DataBlobs           int64   `json:"data_blobs"`
	TreeBlobs           int64   `json:"tree_blobs"`
	DataAdded           int64   `json:"data_added"`
	TotalFilesProcessed int64   `json:"total_files_processed"`
	TotalBytesProcessed int64   `json:"total_bytes_processed"`
	TotalDuration       float64 `json:"total_duration"`
	SnapshotID          string  `json:"snapshot_id"`
}

// BackupError represents an error message from restic.
// Maps to restic's JSON output with message_type: "error"
type BackupError struct {
	MessageType string `json:"message_type"`
	Error       string `json:"error"`
	During      string `json:"during"`
	Item        string `json:"item"`
}

// BackupVerboseStatus represents verbose status about individual files.
// Maps to restic's JSON output with message_type: "verbose_status"
type BackupVerboseStatus struct {
	MessageType  string `json:"message_type"`
	Action       string `json:"action"` // "new", "unchanged", "modified", "scan_finished"
	Item         string `json:"item"`
	Duration     float64 `json:"duration"`
	DataSize     int64   `json:"data_size"`
	MetadataSize int64   `json:"metadata_size"`
}

// BackupProgress represents the current state of a running backup for UI display.
type BackupProgress struct {
	PercentDone    float64
	FilesProcessed int64
	TotalFiles     int64
	BytesProcessed int64
	TotalBytes     int64
}

// ProgressCallback is called when backup progress updates are available.
// Implementations must be safe to call from any goroutine.
type ProgressCallback func(BackupProgress)

// ProgressWriter wraps an io.Writer and parses JSON progress from restic.
// It filters out JSON status lines from the log output, only writing the
// final summary in a human-readable format. Progress updates are sent via callback.
type ProgressWriter struct {
	underlying   io.Writer
	callback     ProgressCallback
	lineBuffer   bytes.Buffer
	lastCallback time.Time
	minInterval  time.Duration
	mu           sync.Mutex
}

// NewProgressWriter creates a writer that parses restic JSON output.
// JSON status lines are filtered from the log output.
// The final summary is written in human-readable format.
// Progress updates are sent via callback, throttled to minInterval.
func NewProgressWriter(w io.Writer, callback ProgressCallback, minInterval time.Duration) *ProgressWriter {
	return &ProgressWriter{
		underlying:  w,
		callback:    callback,
		minInterval: minInterval,
	}
}

// Write implements io.Writer. JSON status lines are parsed for progress
// and filtered from the output. Only the summary is written to the log.
func (pw *ProgressWriter) Write(p []byte) (n int, err error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	originalLen := len(p)

	// Process using efficient line scanning with bytes.IndexByte
	// This uses SIMD/vectorized CPU instructions instead of byte-by-byte iteration
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx == -1 {
			// No newline found, buffer remaining bytes
			pw.lineBuffer.Write(p)
			break
		}

		// Append bytes up to newline to buffer and process
		pw.lineBuffer.Write(p[:idx])
		line := pw.lineBuffer.String()
		pw.lineBuffer.Reset()
		pw.processLine(line)

		// Move past the newline
		p = p[idx+1:]
	}

	// Return the original length to indicate all bytes were "consumed"
	return originalLen, nil
}

// processLine handles a complete line of output from restic.
func (pw *ProgressWriter) processLine(line string) {
	if len(line) == 0 {
		return
	}

	// Check if it's JSON
	if line[0] != '{' {
		// Not JSON, write directly to log
		fmt.Fprintln(pw.underlying, line)
		return
	}

	// Try to parse as a generic JSON message to check type
	var msg struct {
		MessageType string `json:"message_type"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		// Invalid JSON, write to log
		fmt.Fprintln(pw.underlying, line)
		return
	}

	switch msg.MessageType {
	case "status":
		// Parse and handle status (for progress callback), don't write to log
		var status BackupStatus
		if err := json.Unmarshal([]byte(line), &status); err == nil {
			pw.handleStatus(status)
		}

	case "error":
		// Parse and write error in human-readable format
		var backupErr BackupError
		if err := json.Unmarshal([]byte(line), &backupErr); err == nil {
			pw.writeError(backupErr)
		} else {
			// If parsing fails, write raw line
			fmt.Fprintln(pw.underlying, line)
		}

	case "verbose_status":
		// Parse verbose status - only log scan_finished, skip per-file status
		var verbose BackupVerboseStatus
		if err := json.Unmarshal([]byte(line), &verbose); err == nil {
			if verbose.Action == "scan_finished" {
				fmt.Fprintln(pw.underlying, "Scan finished, starting backup...")
			}
			// Skip logging individual file statuses (new, unchanged, modified)
		}

	case "summary":
		// Parse and write summary in human-readable format
		var summary BackupSummary
		if err := json.Unmarshal([]byte(line), &summary); err == nil {
			pw.writeSummary(summary)
		}

	default:
		// Unknown message type, write to log as-is
		fmt.Fprintln(pw.underlying, line)
	}
}

// handleStatus processes a parsed status message and invokes the callback.
func (pw *ProgressWriter) handleStatus(status BackupStatus) {
	// Throttle updates
	now := time.Now()
	if now.Sub(pw.lastCallback) < pw.minInterval {
		return
	}
	pw.lastCallback = now

	// Convert to BackupProgress and invoke callback
	progress := BackupProgress{
		PercentDone:    status.PercentDone,
		FilesProcessed: status.FilesDone,
		TotalFiles:     status.TotalFiles,
		BytesProcessed: status.BytesDone,
		TotalBytes:     status.TotalBytes,
	}

	if pw.callback != nil {
		pw.callback(progress)
	}
}

// writeError writes a backup error in a human-readable format.
func (pw *ProgressWriter) writeError(backupErr BackupError) {
	if backupErr.Item != "" {
		fmt.Fprintf(pw.underlying, "ERROR: %s: %s\n", backupErr.Item, backupErr.Error)
	} else {
		fmt.Fprintf(pw.underlying, "ERROR: %s\n", backupErr.Error)
	}
}

// writeSummary writes the backup summary in a human-readable format.
func (pw *ProgressWriter) writeSummary(summary BackupSummary) {
	fmt.Fprintln(pw.underlying, "")
	fmt.Fprintln(pw.underlying, "Backup Summary:")
	fmt.Fprintf(pw.underlying, "  Snapshot:    %s\n", summary.SnapshotID)
	fmt.Fprintf(pw.underlying, "  Duration:    %.1f seconds\n", summary.TotalDuration)
	fmt.Fprintf(pw.underlying, "  Files:       %d new, %d changed, %d unmodified\n",
		summary.FilesNew, summary.FilesChanged, summary.FilesUnmodified)
	fmt.Fprintf(pw.underlying, "  Dirs:        %d new, %d changed, %d unmodified\n",
		summary.DirsNew, summary.DirsChanged, summary.DirsUnmodified)
	fmt.Fprintf(pw.underlying, "  Data added:  %s\n", formatBytes(summary.DataAdded))
	fmt.Fprintf(pw.underlying, "  Processed:   %d files, %s\n",
		summary.TotalFilesProcessed, formatBytes(summary.TotalBytesProcessed))
}

// formatBytes converts bytes to a human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
