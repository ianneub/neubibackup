package app

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// configReloadDebounce is how long the config file must stay quiet before a
// reload. One editor save produces several events (create, write, chmod), and
// reloading on the first could read a half-written file.
const configReloadDebounce = 300 * time.Millisecond

// watchConfig calls onChange after the file at path is written in place or
// replaced, once events for it have been quiet for debounce. It blocks until
// ctx is cancelled or w is closed.
//
// It watches the parent directory rather than the file itself: editors such
// as vim save by writing a new file and renaming it over the old one, which
// drops a watch on the file (the old inode is gone) and reports REMOVE rather
// than WRITE. A directory watch sees the replacement arrive as CREATE.
func watchConfig(ctx context.Context, w *fsnotify.Watcher, path string, debounce time.Duration, onChange func()) error {
	path = filepath.Clean(path)
	if err := w.Add(filepath.Dir(path)); err != nil {
		return err
	}

	var timer *time.Timer
	var fire <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			// Other files share the directory (state.yaml, app.log); ignore them.
			if filepath.Clean(event.Name) != path || !event.Op.Has(fsnotify.Write|fsnotify.Create) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(debounce)
			} else {
				timer.Reset(debounce)
			}
			fire = timer.C
		case <-fire:
			fire = nil
			onChange()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			slog.Error("Config watcher error", "error", err)
		}
	}
}
