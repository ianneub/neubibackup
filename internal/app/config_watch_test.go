package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

const testDebounce = 50 * time.Millisecond

// startConfigWatch runs watchConfig on dir/config.yaml and returns a channel
// that receives the file's contents each time onChange fires.
func startConfigWatch(t *testing.T) (path string, changes <-chan string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "config.yaml")
	writeFile(t, path, "version: 1\n")

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan string, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := watchConfig(ctx, w, path, testDebounce, func() {
			b, _ := os.ReadFile(path)
			ch <- string(b)
		})
		if err != nil {
			t.Errorf("watchConfig: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		w.Close()
	})
	// Let the watch get established before the test mutates files.
	time.Sleep(50 * time.Millisecond)
	return path, ch
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func expectChange(t *testing.T, changes <-chan string, want string) {
	t.Helper()
	select {
	case got := <-changes:
		if got != want {
			t.Errorf("onChange saw %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("onChange not called; want reload with %q", want)
	}
}

func expectNoChange(t *testing.T, changes <-chan string) {
	t.Helper()
	select {
	case got := <-changes:
		t.Errorf("unexpected onChange (file = %q)", got)
	case <-time.After(6 * testDebounce):
	}
}

// saveByRename mimics editors that write a temp file and rename it over the
// original, replacing the file's inode.
func saveByRename(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".tmp"
	writeFile(t, tmp, content)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

func TestWatchConfigInPlaceWrite(t *testing.T) {
	path, changes := startConfigWatch(t)

	writeFile(t, path, "version: 2\n")
	expectChange(t, changes, "version: 2\n")
	expectNoChange(t, changes)
}

// Regression: watching the file itself stopped reloading after the first
// editor save, because the replaced inode took the watch with it.
func TestWatchConfigSurvivesSaveByRename(t *testing.T) {
	path, changes := startConfigWatch(t)

	saveByRename(t, path, "version: 2\n")
	expectChange(t, changes, "version: 2\n")

	saveByRename(t, path, "version: 3\n")
	expectChange(t, changes, "version: 3\n")

	writeFile(t, path, "version: 4\n")
	expectChange(t, changes, "version: 4\n")
}

// Vim with backupcopy=no renames the original away, creates a new empty
// file, then writes it. That's several events for one save: expect a single
// reload that sees the complete file.
func TestWatchConfigVimStyleSaveReloadsOnceWithFullContent(t *testing.T) {
	path, changes := startConfigWatch(t)

	backup := path + "~"
	if err := os.Rename(path, backup); err != nil {
		t.Fatalf("rename to backup: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.WriteString("version: 2\n")
	f.Sync()
	f.WriteString("log_level: debug\n")
	f.Close()
	os.Remove(backup)

	expectChange(t, changes, "version: 2\nlog_level: debug\n")
	expectNoChange(t, changes)
}

func TestWatchConfigIgnoresOtherFilesInDir(t *testing.T) {
	path, changes := startConfigWatch(t)
	dir := filepath.Dir(path)

	writeFile(t, filepath.Join(dir, "state.yaml"), "backup: {}\n")
	writeFile(t, filepath.Join(dir, "app.log"), "log line\n")
	saveByRename(t, filepath.Join(dir, "exclude.txt"), "*.tmp\n")
	os.Remove(filepath.Join(dir, "state.yaml"))

	expectNoChange(t, changes)
}

func TestWatchConfigReturnsOnCancel(t *testing.T) {
	dir := t.TempDir()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- watchConfig(ctx, w, filepath.Join(dir, "config.yaml"), testDebounce, func() {})
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("watchConfig returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchConfig did not return after cancel")
	}
}

func TestWatchConfigMissingDirReturnsError(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	missing := filepath.Join(t.TempDir(), "nope", "config.yaml")
	if err := watchConfig(context.Background(), w, missing, testDebounce, func() {}); err == nil {
		t.Fatal("watchConfig: expected error for missing directory, got nil")
	}
}
