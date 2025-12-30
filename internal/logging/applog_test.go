package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRotatingWriter_Write(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")
	oldPath := filepath.Join(tmpDir, "test.log.old")

	// Create the log file
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	// Create rotating writer with small max size for testing
	rw := &rotatingWriter{
		file:     file,
		filePath: logPath,
		oldPath:  oldPath,
		maxSize:  100, // 100 bytes for easy testing
		size:     0,
	}
	defer rw.Close()

	t.Run("writes data correctly", func(t *testing.T) {
		data := []byte("Hello, World!")
		n, err := rw.Write(data)
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if n != len(data) {
			t.Errorf("Write() wrote %d bytes, want %d", n, len(data))
		}

		// Verify file contents
		content, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("Failed to read log file: %v", err)
		}
		if string(content) != "Hello, World!" {
			t.Errorf("File content = %q, want %q", string(content), "Hello, World!")
		}
	})

	t.Run("tracks size correctly", func(t *testing.T) {
		// Reset for this test
		rw.size = 0
		os.Truncate(logPath, 0)

		data := []byte("12345")
		rw.Write(data)
		if rw.size != 5 {
			t.Errorf("size = %d, want 5", rw.size)
		}

		rw.Write(data)
		if rw.size != 10 {
			t.Errorf("size = %d, want 10", rw.size)
		}
	})
}

func TestRotatingWriter_Rotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")
	oldPath := filepath.Join(tmpDir, "test.log.old")

	// Create the log file
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	// Create rotating writer with small max size
	rw := &rotatingWriter{
		file:     file,
		filePath: logPath,
		oldPath:  oldPath,
		maxSize:  50, // 50 bytes
		size:     0,
	}
	defer rw.Close()

	t.Run("rotates when exceeding max size", func(t *testing.T) {
		// Write data that will fit
		firstData := strings.Repeat("A", 40)
		rw.Write([]byte(firstData))

		// Verify no rotation yet
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Error("Old file should not exist yet")
		}

		// Write data that will trigger rotation
		secondData := strings.Repeat("B", 20)
		rw.Write([]byte(secondData))

		// Verify rotation happened
		if _, err := os.Stat(oldPath); err != nil {
			t.Errorf("Old file should exist after rotation: %v", err)
		}

		// Old file should contain the first data
		oldContent, err := os.ReadFile(oldPath)
		if err != nil {
			t.Fatalf("Failed to read old file: %v", err)
		}
		if string(oldContent) != firstData {
			t.Errorf("Old file content = %q, want %q", string(oldContent), firstData)
		}

		// New file should contain the second data
		newContent, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("Failed to read new file: %v", err)
		}
		if string(newContent) != secondData {
			t.Errorf("New file content = %q, want %q", string(newContent), secondData)
		}

		// Size should be reset
		if rw.size != int64(len(secondData)) {
			t.Errorf("size after rotation = %d, want %d", rw.size, len(secondData))
		}
	})

	t.Run("overwrites old backup on second rotation", func(t *testing.T) {
		// Reset
		rw.size = 0
		os.Truncate(logPath, 0)
		os.WriteFile(oldPath, []byte("old backup"), 0644)

		// Write data that triggers rotation
		newData := strings.Repeat("C", 60)
		rw.Write([]byte(newData))

		// Old backup should be overwritten
		oldContent, _ := os.ReadFile(oldPath)
		if string(oldContent) == "old backup" {
			t.Error("Old backup should have been overwritten")
		}
	})
}

func TestRotatingWriter_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")
	oldPath := filepath.Join(tmpDir, "test.log.old")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	rw := &rotatingWriter{
		file:     file,
		filePath: logPath,
		oldPath:  oldPath,
		maxSize:  1000,
		size:     0,
	}
	defer rw.Close()

	// Write concurrently from multiple goroutines
	var wg sync.WaitGroup
	numGoroutines := 10
	writesPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				rw.Write([]byte("X"))
			}
		}(i)
	}

	wg.Wait()

	// Should not panic or deadlock - if we get here, concurrency is working
	// Total writes should be tracked (may have rotations)
	totalWrites := numGoroutines * writesPerGoroutine
	t.Logf("Completed %d concurrent writes", totalWrites)
}

func TestRotatingWriter_Close(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")
	oldPath := filepath.Join(tmpDir, "test.log.old")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	rw := &rotatingWriter{
		file:     file,
		filePath: logPath,
		oldPath:  oldPath,
		maxSize:  100,
		size:     0,
	}

	// Write something
	rw.Write([]byte("test"))

	// Close should not error
	if err := rw.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Close again should not panic (nil file)
	rw.file = nil
	if err := rw.Close(); err != nil {
		t.Errorf("Close() on nil file error = %v", err)
	}
}

func TestGetAppLogPath(t *testing.T) {
	path, err := GetAppLogPath()
	if err != nil {
		t.Fatalf("GetAppLogPath() error = %v", err)
	}

	// Should end with app.log
	if !strings.HasSuffix(path, "app.log") {
		t.Errorf("GetAppLogPath() = %q, should end with 'app.log'", path)
	}

	// Should be absolute
	if !filepath.IsAbs(path) {
		t.Errorf("GetAppLogPath() = %q, should be absolute", path)
	}

	// Should contain neubibackup directory
	if !strings.Contains(path, "neubibackup") {
		t.Errorf("GetAppLogPath() = %q, should contain 'neubibackup'", path)
	}
}

func TestReplaceAttr(t *testing.T) {
	tests := []struct {
		name     string
		attr     slog.Attr
		wantFile string
	}{
		{
			name: "converts absolute path to relative",
			attr: slog.Any(slog.SourceKey, &slog.Source{
				File: moduleBasePath + string(filepath.Separator) + "internal" + string(filepath.Separator) + "backup" + string(filepath.Separator) + "orchestrator.go",
				Line: 42,
			}),
			wantFile: "internal" + string(filepath.Separator) + "backup" + string(filepath.Separator) + "orchestrator.go",
		},
		{
			name: "leaves non-module paths unchanged",
			attr: slog.Any(slog.SourceKey, &slog.Source{
				File: "/some/other/path/file.go",
				Line: 10,
			}),
			wantFile: "/some/other/path/file.go",
		},
		{
			name: "handles non-source attributes",
			attr: slog.String("message", "test message"),
			wantFile: "", // Not a source attr, so no file to check
		},
		{
			name: "handles nil source",
			attr: slog.Any(slog.SourceKey, (*slog.Source)(nil)),
			wantFile: "", // nil source
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceAttr(nil, tt.attr)

			if tt.attr.Key != slog.SourceKey {
				// Non-source attributes should be unchanged
				if result.Key != tt.attr.Key {
					t.Errorf("replaceAttr() changed non-source attr key")
				}
				return
			}

			source, ok := result.Value.Any().(*slog.Source)
			if !ok || source == nil {
				if tt.wantFile != "" {
					t.Errorf("replaceAttr() returned nil source, want file %q", tt.wantFile)
				}
				return
			}

			if source.File != tt.wantFile {
				t.Errorf("replaceAttr() file = %q, want %q", source.File, tt.wantFile)
			}
		})
	}
}

func TestSplitHandler_LevelRouting(t *testing.T) {
	var fileBuf, stdoutBuf, stderrBuf bytes.Buffer

	// Use Debug level so all messages are logged
	handlerOpts := &slog.HandlerOptions{
		AddSource: false, // Disable source for simpler output
		Level:     slog.LevelDebug,
	}

	handler := &splitHandler{
		fileHandler:   slog.NewTextHandler(&fileBuf, handlerOpts),
		stdoutHandler: slog.NewTextHandler(&stdoutBuf, handlerOpts),
		stderrHandler: slog.NewTextHandler(&stderrBuf, handlerOpts),
	}

	logger := slog.New(handler)
	ctx := context.Background()

	tests := []struct {
		name         string
		logFunc      func()
		wantInFile   bool
		wantInStdout bool
		wantInStderr bool
		message      string
	}{
		{
			name:         "Debug goes to file and stdout",
			logFunc:      func() { logger.Debug("debug message") },
			wantInFile:   true,
			wantInStdout: true,
			wantInStderr: false,
			message:      "debug message",
		},
		{
			name:         "Info goes to file and stdout",
			logFunc:      func() { logger.InfoContext(ctx, "info message") },
			wantInFile:   true,
			wantInStdout: true,
			wantInStderr: false,
			message:      "info message",
		},
		{
			name:         "Warn goes to file and stdout",
			logFunc:      func() { logger.Warn("warn message") },
			wantInFile:   true,
			wantInStdout: true,
			wantInStderr: false,
			message:      "warn message",
		},
		{
			name:         "Error goes to file and stderr",
			logFunc:      func() { logger.Error("error message") },
			wantInFile:   true,
			wantInStdout: false,
			wantInStderr: true,
			message:      "error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear buffers
			fileBuf.Reset()
			stdoutBuf.Reset()
			stderrBuf.Reset()

			tt.logFunc()

			fileContent := fileBuf.String()
			stdoutContent := stdoutBuf.String()
			stderrContent := stderrBuf.String()

			if tt.wantInFile && !strings.Contains(fileContent, tt.message) {
				t.Errorf("file buffer should contain %q, got %q", tt.message, fileContent)
			}
			if !tt.wantInFile && strings.Contains(fileContent, tt.message) {
				t.Errorf("file buffer should NOT contain %q, got %q", tt.message, fileContent)
			}

			if tt.wantInStdout && !strings.Contains(stdoutContent, tt.message) {
				t.Errorf("stdout buffer should contain %q, got %q", tt.message, stdoutContent)
			}
			if !tt.wantInStdout && strings.Contains(stdoutContent, tt.message) {
				t.Errorf("stdout buffer should NOT contain %q, got %q", tt.message, stdoutContent)
			}

			if tt.wantInStderr && !strings.Contains(stderrContent, tt.message) {
				t.Errorf("stderr buffer should contain %q, got %q", tt.message, stderrContent)
			}
			if !tt.wantInStderr && strings.Contains(stderrContent, tt.message) {
				t.Errorf("stderr buffer should NOT contain %q, got %q", tt.message, stderrContent)
			}
		})
	}
}

func TestSplitHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	handlerOpts := &slog.HandlerOptions{AddSource: false}

	handler := &splitHandler{
		fileHandler:   slog.NewTextHandler(&buf, handlerOpts),
		stdoutHandler: slog.NewTextHandler(&bytes.Buffer{}, handlerOpts),
		stderrHandler: slog.NewTextHandler(&bytes.Buffer{}, handlerOpts),
	}

	// Add attributes
	newHandler := handler.WithAttrs([]slog.Attr{slog.String("service", "test")})
	logger := slog.New(newHandler)

	logger.Info("test message")

	content := buf.String()
	if !strings.Contains(content, "service=test") {
		t.Errorf("WithAttrs() attributes not propagated, got %q", content)
	}
}

func TestSplitHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	handlerOpts := &slog.HandlerOptions{AddSource: false}

	handler := &splitHandler{
		fileHandler:   slog.NewTextHandler(&buf, handlerOpts),
		stdoutHandler: slog.NewTextHandler(&bytes.Buffer{}, handlerOpts),
		stderrHandler: slog.NewTextHandler(&bytes.Buffer{}, handlerOpts),
	}

	// Add group
	newHandler := handler.WithGroup("mygroup")
	logger := slog.New(newHandler)

	logger.Info("test message", "key", "value")

	content := buf.String()
	if !strings.Contains(content, "mygroup.key=value") {
		t.Errorf("WithGroup() not applied, got %q", content)
	}
}

func TestSplitHandler_Enabled(t *testing.T) {
	var buf bytes.Buffer

	// Create handler with default level (Info)
	handlerOpts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	handler := &splitHandler{
		fileHandler:   slog.NewTextHandler(&buf, handlerOpts),
		stdoutHandler: slog.NewTextHandler(&bytes.Buffer{}, handlerOpts),
		stderrHandler: slog.NewTextHandler(&bytes.Buffer{}, handlerOpts),
	}

	ctx := context.Background()

	tests := []struct {
		level   slog.Level
		enabled bool
	}{
		{slog.LevelDebug, false},
		{slog.LevelInfo, true},
		{slog.LevelWarn, true},
		{slog.LevelError, true},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			got := handler.Enabled(ctx, tt.level)
			if got != tt.enabled {
				t.Errorf("Enabled(%s) = %v, want %v", tt.level, got, tt.enabled)
			}
		})
	}
}

func TestModuleBasePath(t *testing.T) {
	// moduleBasePath should be set during init
	if moduleBasePath == "" {
		t.Error("moduleBasePath should be set during init")
	}

	// Should be an absolute path
	if !filepath.IsAbs(moduleBasePath) {
		t.Errorf("moduleBasePath = %q, should be absolute", moduleBasePath)
	}

	// Should exist
	if _, err := os.Stat(moduleBasePath); err != nil {
		t.Errorf("moduleBasePath = %q does not exist: %v", moduleBasePath, err)
	}
}
