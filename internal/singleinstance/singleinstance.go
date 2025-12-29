// Package singleinstance ensures only one instance of the application runs at a time.
package singleinstance

import (
	"fmt"
	"os"
	"path/filepath"

	"neubibackup/internal/config"
)

// Lock represents an instance lock that prevents multiple instances from running.
type Lock struct {
	lockFile *os.File
	path     string
}

// Acquire attempts to acquire the single instance lock.
// Returns a Lock if successful, or an error if another instance is already running.
func Acquire() (*Lock, error) {
	appDir, err := config.GetAppDir()
	if err != nil {
		return nil, fmt.Errorf("getting app dir: %w", err)
	}

	// Ensure the directory exists
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("creating app dir: %w", err)
	}

	lockPath := filepath.Join(appDir, "neubibackup.lock")

	lock := &Lock{
		path: lockPath,
	}

	if err := lock.tryLock(); err != nil {
		return nil, err
	}

	return lock, nil
}

// Release releases the instance lock.
func (l *Lock) Release() error {
	if l.lockFile == nil {
		return nil
	}

	// Unlock and close
	if err := l.unlock(); err != nil {
		l.lockFile.Close()
		return err
	}

	if err := l.lockFile.Close(); err != nil {
		return err
	}

	// Remove the lock file
	os.Remove(l.path)

	l.lockFile = nil
	return nil
}
