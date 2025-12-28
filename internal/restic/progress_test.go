package restic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProgressWriter_StatusJSON(t *testing.T) {
	var output bytes.Buffer
	var lastProgress BackupProgress
	var callbackCount int

	callback := func(p BackupProgress) {
		lastProgress = p
		callbackCount++
	}

	pw := NewProgressWriter(&output, callback, 0) // 0 interval for immediate callbacks

	statusJSON := `{"message_type":"status","percent_done":0.45,"total_files":1000,"files_done":450,"total_bytes":5000000000,"bytes_done":2250000000,"seconds_elapsed":120,"seconds_remaining":150}` + "\n"

	n, err := pw.Write([]byte(statusJSON))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(statusJSON) {
		t.Errorf("Write() returned %d, want %d", n, len(statusJSON))
	}

	// Status JSON should NOT be written to output
	if output.Len() > 0 {
		t.Errorf("Status JSON should not be written to output, got: %q", output.String())
	}

	// Callback should have been called
	if callbackCount != 1 {
		t.Errorf("Callback count = %d, want 1", callbackCount)
	}

	// Check progress values
	if lastProgress.PercentDone != 0.45 {
		t.Errorf("PercentDone = %v, want 0.45", lastProgress.PercentDone)
	}
	if lastProgress.FilesProcessed != 450 {
		t.Errorf("FilesProcessed = %d, want 450", lastProgress.FilesProcessed)
	}
	if lastProgress.TotalFiles != 1000 {
		t.Errorf("TotalFiles = %d, want 1000", lastProgress.TotalFiles)
	}
	if lastProgress.BytesProcessed != 2250000000 {
		t.Errorf("BytesProcessed = %d, want 2250000000", lastProgress.BytesProcessed)
	}
	if lastProgress.TotalBytes != 5000000000 {
		t.Errorf("TotalBytes = %d, want 5000000000", lastProgress.TotalBytes)
	}
}

func TestProgressWriter_SummaryJSON(t *testing.T) {
	var output bytes.Buffer

	pw := NewProgressWriter(&output, nil, 0)

	summaryJSON := `{"message_type":"summary","files_new":10,"files_changed":5,"files_unmodified":985,"dirs_new":2,"dirs_changed":1,"dirs_unmodified":100,"data_added":1048576,"total_files_processed":1000,"total_bytes_processed":5000000000,"total_duration":120.5,"snapshot_id":"abc123def"}` + "\n"

	_, err := pw.Write([]byte(summaryJSON))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Summary should be written in human-readable format
	outputStr := output.String()

	if !strings.Contains(outputStr, "Backup Summary:") {
		t.Error("Output should contain 'Backup Summary:'")
	}
	if !strings.Contains(outputStr, "abc123def") {
		t.Error("Output should contain snapshot ID")
	}
	if !strings.Contains(outputStr, "120.5 seconds") {
		t.Error("Output should contain duration")
	}
	if !strings.Contains(outputStr, "10 new, 5 changed, 985 unmodified") {
		t.Error("Output should contain file stats")
	}
}

func TestProgressWriter_ErrorJSON(t *testing.T) {
	var output bytes.Buffer

	pw := NewProgressWriter(&output, nil, 0)

	t.Run("error with item", func(t *testing.T) {
		output.Reset()
		errorJSON := `{"message_type":"error","error":"permission denied","during":"backup","item":"/etc/shadow"}` + "\n"

		_, err := pw.Write([]byte(errorJSON))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		outputStr := output.String()
		if !strings.Contains(outputStr, "ERROR:") {
			t.Error("Output should contain 'ERROR:'")
		}
		if !strings.Contains(outputStr, "/etc/shadow") {
			t.Error("Output should contain item path")
		}
		if !strings.Contains(outputStr, "permission denied") {
			t.Error("Output should contain error message")
		}
	})

	t.Run("error without item", func(t *testing.T) {
		output.Reset()
		errorJSON := `{"message_type":"error","error":"repository not found","during":"open"}` + "\n"

		_, err := pw.Write([]byte(errorJSON))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		outputStr := output.String()
		if !strings.Contains(outputStr, "ERROR:") {
			t.Error("Output should contain 'ERROR:'")
		}
		if !strings.Contains(outputStr, "repository not found") {
			t.Error("Output should contain error message")
		}
	})
}

func TestProgressWriter_VerboseStatusJSON(t *testing.T) {
	var output bytes.Buffer

	pw := NewProgressWriter(&output, nil, 0)

	t.Run("scan_finished is logged", func(t *testing.T) {
		output.Reset()
		verboseJSON := `{"message_type":"verbose_status","action":"scan_finished","item":"","duration":0,"data_size":0,"metadata_size":0}` + "\n"

		_, err := pw.Write([]byte(verboseJSON))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		if !strings.Contains(output.String(), "Scan finished") {
			t.Error("scan_finished should be logged")
		}
	})

	t.Run("file status is not logged", func(t *testing.T) {
		output.Reset()
		verboseJSON := `{"message_type":"verbose_status","action":"new","item":"/home/user/file.txt","duration":0.1,"data_size":1024,"metadata_size":128}` + "\n"

		_, err := pw.Write([]byte(verboseJSON))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		if output.Len() > 0 {
			t.Errorf("File status should not be logged, got: %q", output.String())
		}
	})
}

func TestProgressWriter_NonJSON(t *testing.T) {
	var output bytes.Buffer

	pw := NewProgressWriter(&output, nil, 0)

	// Non-JSON lines should pass through
	plainLine := "This is a plain log line\n"

	_, err := pw.Write([]byte(plainLine))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if !strings.Contains(output.String(), "This is a plain log line") {
		t.Errorf("Plain line should be written to output, got: %q", output.String())
	}
}

func TestProgressWriter_InvalidJSON(t *testing.T) {
	var output bytes.Buffer

	pw := NewProgressWriter(&output, nil, 0)

	// Invalid JSON starting with { should pass through
	invalidJSON := "{invalid json content}\n"

	_, err := pw.Write([]byte(invalidJSON))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if !strings.Contains(output.String(), "{invalid json content}") {
		t.Errorf("Invalid JSON should be written to output, got: %q", output.String())
	}
}

func TestProgressWriter_PartialLines(t *testing.T) {
	var output bytes.Buffer
	var callbackCount int

	callback := func(p BackupProgress) {
		callbackCount++
	}

	pw := NewProgressWriter(&output, callback, 0)

	// Write JSON in multiple chunks
	statusJSON := `{"message_type":"status","percent_done":0.5,"total_files":100,"files_done":50,"total_bytes":1000,"bytes_done":500}`

	// Write first half
	_, err := pw.Write([]byte(statusJSON[:len(statusJSON)/2]))
	if err != nil {
		t.Fatalf("Write() first half error = %v", err)
	}

	// No callback yet (no complete line)
	if callbackCount != 0 {
		t.Errorf("Callback should not be called yet, got %d calls", callbackCount)
	}

	// Write second half with newline
	_, err = pw.Write([]byte(statusJSON[len(statusJSON)/2:] + "\n"))
	if err != nil {
		t.Fatalf("Write() second half error = %v", err)
	}

	// Now callback should have been called
	if callbackCount != 1 {
		t.Errorf("Callback count = %d, want 1", callbackCount)
	}
}

func TestProgressWriter_MultipleLines(t *testing.T) {
	var output bytes.Buffer

	pw := NewProgressWriter(&output, nil, 0)

	// Write multiple lines at once
	lines := "Line 1\nLine 2\nLine 3\n"

	_, err := pw.Write([]byte(lines))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	outputStr := output.String()
	if !strings.Contains(outputStr, "Line 1") {
		t.Error("Output should contain 'Line 1'")
	}
	if !strings.Contains(outputStr, "Line 2") {
		t.Error("Output should contain 'Line 2'")
	}
	if !strings.Contains(outputStr, "Line 3") {
		t.Error("Output should contain 'Line 3'")
	}
}

func TestProgressWriter_Throttling(t *testing.T) {
	var output bytes.Buffer
	var callbackCount int

	callback := func(p BackupProgress) {
		callbackCount++
	}

	// Use a long throttle interval
	pw := NewProgressWriter(&output, callback, 100*time.Millisecond)

	statusJSON := `{"message_type":"status","percent_done":0.5,"total_files":100,"files_done":50,"total_bytes":1000,"bytes_done":500}` + "\n"

	// Write multiple status updates quickly
	for i := 0; i < 5; i++ {
		_, err := pw.Write([]byte(statusJSON))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// Only one callback should have been invoked due to throttling
	if callbackCount != 1 {
		t.Errorf("Callback count = %d, want 1 (throttled)", callbackCount)
	}

	// Wait for throttle to expire and write again
	time.Sleep(150 * time.Millisecond)
	_, err := pw.Write([]byte(statusJSON))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if callbackCount != 2 {
		t.Errorf("Callback count = %d, want 2 after throttle expired", callbackCount)
	}
}

func TestProgressWriter_NilCallback(t *testing.T) {
	var output bytes.Buffer

	// nil callback should not panic
	pw := NewProgressWriter(&output, nil, 0)

	statusJSON := `{"message_type":"status","percent_done":0.5,"total_files":100,"files_done":50,"total_bytes":1000,"bytes_done":500}` + "\n"

	_, err := pw.Write([]byte(statusJSON))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Should not panic, and status should not be written to output
	if output.Len() > 0 {
		t.Errorf("Status should not be written to output with nil callback")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero bytes", 0, "0 B"},
		{"500 bytes", 500, "500 B"},
		{"1023 bytes", 1023, "1023 B"},
		{"1 KB", 1024, "1.0 KB"},
		{"1.5 KB", 1536, "1.5 KB"},
		{"1 MB", 1024 * 1024, "1.0 MB"},
		{"500 MB", 500 * 1024 * 1024, "500.0 MB"},
		{"1 GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"2.5 GB", int64(2.5 * 1024 * 1024 * 1024), "2.5 GB"},
		{"1 TB", 1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestBackupStatus_UnmarshalJSON(t *testing.T) {
	jsonStr := `{"message_type":"status","percent_done":0.75,"total_files":2000,"files_done":1500,"total_bytes":10000000000,"bytes_done":7500000000,"seconds_elapsed":300,"seconds_remaining":100,"current_files":["/home/user/file1.txt","/home/user/file2.txt"]}`

	var status BackupStatus
	if err := json.Unmarshal([]byte(jsonStr), &status); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if status.MessageType != "status" {
		t.Errorf("MessageType = %q, want %q", status.MessageType, "status")
	}
	if status.PercentDone != 0.75 {
		t.Errorf("PercentDone = %v, want 0.75", status.PercentDone)
	}
	if status.TotalFiles != 2000 {
		t.Errorf("TotalFiles = %d, want 2000", status.TotalFiles)
	}
	if status.FilesDone != 1500 {
		t.Errorf("FilesDone = %d, want 1500", status.FilesDone)
	}
	if len(status.CurrentFiles) != 2 {
		t.Errorf("len(CurrentFiles) = %d, want 2", len(status.CurrentFiles))
	}
}

func TestBackupSummary_UnmarshalJSON(t *testing.T) {
	jsonStr := `{"message_type":"summary","files_new":100,"files_changed":50,"files_unmodified":850,"dirs_new":10,"dirs_changed":5,"dirs_unmodified":85,"data_blobs":200,"tree_blobs":50,"data_added":52428800,"total_files_processed":1000,"total_bytes_processed":1073741824,"total_duration":180.5,"snapshot_id":"abcdef123456"}`

	var summary BackupSummary
	if err := json.Unmarshal([]byte(jsonStr), &summary); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if summary.MessageType != "summary" {
		t.Errorf("MessageType = %q, want %q", summary.MessageType, "summary")
	}
	if summary.FilesNew != 100 {
		t.Errorf("FilesNew = %d, want 100", summary.FilesNew)
	}
	if summary.DataAdded != 52428800 {
		t.Errorf("DataAdded = %d, want 52428800", summary.DataAdded)
	}
	if summary.SnapshotID != "abcdef123456" {
		t.Errorf("SnapshotID = %q, want %q", summary.SnapshotID, "abcdef123456")
	}
	if summary.TotalDuration != 180.5 {
		t.Errorf("TotalDuration = %v, want 180.5", summary.TotalDuration)
	}
}

func TestBackupError_UnmarshalJSON(t *testing.T) {
	jsonStr := `{"message_type":"error","error":"permission denied","during":"backup","item":"/etc/shadow"}`

	var backupErr BackupError
	if err := json.Unmarshal([]byte(jsonStr), &backupErr); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if backupErr.MessageType != "error" {
		t.Errorf("MessageType = %q, want %q", backupErr.MessageType, "error")
	}
	if backupErr.Error != "permission denied" {
		t.Errorf("Error = %q, want %q", backupErr.Error, "permission denied")
	}
	if backupErr.During != "backup" {
		t.Errorf("During = %q, want %q", backupErr.During, "backup")
	}
	if backupErr.Item != "/etc/shadow" {
		t.Errorf("Item = %q, want %q", backupErr.Item, "/etc/shadow")
	}
}

