package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"neubibackup/internal/config"
)

// moduleBasePath is the root path of the module, used to compute relative paths for log sources.
var moduleBasePath string

func init() {
	// Determine module root from the location of this file (internal/logging/applog.go)
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		// Go up 3 levels: applog.go -> logging -> internal -> module root
		moduleBasePath = filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	}
}

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

// replaceAttr converts absolute source paths to relative paths.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.SourceKey {
		source, ok := a.Value.Any().(*slog.Source)
		if ok && source != nil {
			if rel, found := strings.CutPrefix(source.File, moduleBasePath); found {
				source.File = strings.TrimPrefix(rel, string(filepath.Separator))
			}
		}
	}
	return a
}

// splitHandler routes log messages to different handlers based on level.
// All messages go to the file handler. Messages below Error level go to stdout,
// while Error and above go to stderr.
type splitHandler struct {
	fileHandler   slog.Handler
	stdoutHandler slog.Handler
	stderrHandler slog.Handler
}

func (h *splitHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.fileHandler.Enabled(ctx, level)
}

func (h *splitHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always write to file
	if err := h.fileHandler.Handle(ctx, r); err != nil {
		return err
	}

	// Route to stdout or stderr based on level
	if r.Level >= slog.LevelError {
		return h.stderrHandler.Handle(ctx, r)
	}
	return h.stdoutHandler.Handle(ctx, r)
}

func (h *splitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &splitHandler{
		fileHandler:   h.fileHandler.WithAttrs(attrs),
		stdoutHandler: h.stdoutHandler.WithAttrs(attrs),
		stderrHandler: h.stderrHandler.WithAttrs(attrs),
	}
}

func (h *splitHandler) WithGroup(name string) slog.Handler {
	return &splitHandler{
		fileHandler:   h.fileHandler.WithGroup(name),
		stdoutHandler: h.stdoutHandler.WithGroup(name),
		stderrHandler: h.stderrHandler.WithGroup(name),
	}
}

// SetupAppLog configures application-wide logging to a persistent file.
// The log file automatically rotates when it exceeds 1MB, keeping one backup.
// All log messages go to the file. Messages below Error level also go to stdout,
// while Error and above go to stderr.
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

	handlerOpts := &slog.HandlerOptions{
		AddSource:   true,
		ReplaceAttr: replaceAttr,
	}

	// Create handlers for each destination
	fileHandler := slog.NewTextHandler(appLogWriter, handlerOpts)
	stdoutHandler := slog.NewTextHandler(os.Stdout, handlerOpts)
	stderrHandler := slog.NewTextHandler(os.Stderr, handlerOpts)

	handler := &splitHandler{
		fileHandler:   fileHandler,
		stdoutHandler: stdoutHandler,
		stderrHandler: stderrHandler,
	}

	slog.SetDefault(slog.New(handler))

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
