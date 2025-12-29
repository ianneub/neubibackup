//go:build !windows

package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrAlreadyRunning indicates another instance is already running.
var ErrAlreadyRunning = errors.New("another instance of NeubiBackup is already running")

// tryLock attempts to acquire an exclusive lock on the lock file.
func (l *Lock) tryLock() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}

	// Try to acquire an exclusive lock (non-blocking)
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrAlreadyRunning
		}
		return err
	}

	// Write our PID to the lock file for debugging purposes
	f.Truncate(0)
	f.Seek(0, 0)
	f.WriteString(fmt.Sprintf("%d\n", os.Getpid()))

	l.lockFile = f
	return nil
}

// unlock releases the file lock.
func (l *Lock) unlock() error {
	if l.lockFile == nil {
		return nil
	}
	return syscall.Flock(int(l.lockFile.Fd()), syscall.LOCK_UN)
}
