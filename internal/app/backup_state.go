package app

import (
	"context"
	"errors"
	"sync"

	"neubibackup/internal/restic"
)

// ErrBackupAlreadyRunning is returned when attempting to start a backup while one is already running.
var ErrBackupAlreadyRunning = errors.New("backup is already running")

// BackupState provides thread-safe management of backup execution state.
// It tracks whether a backup is running, provides cancellation support,
// and stores progress information for UI updates.
type BackupState struct {
	mu       sync.RWMutex
	running  bool
	cancel   context.CancelFunc
	progress *restic.BackupProgress
}

// NewBackupState creates a new BackupState with initial values.
func NewBackupState() *BackupState {
	return &BackupState{}
}

// IsRunning returns true if a backup is currently in progress.
// This method is safe to call from any goroutine.
func (s *BackupState) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// SetRunning sets the backup running state.
// This method is safe to call from any goroutine.
func (s *BackupState) SetRunning(running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = running
}

// GetProgress returns a copy of the current backup progress.
// Returns nil if no progress is available.
// This method is safe to call from any goroutine.
func (s *BackupState) GetProgress() *restic.BackupProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.progress == nil {
		return nil
	}
	// Return a copy to ensure thread safety
	progressCopy := *s.progress
	return &progressCopy
}

// SetProgress updates the current backup progress.
// Pass nil to clear the progress.
// This method is safe to call from any goroutine.
func (s *BackupState) SetProgress(p *restic.BackupProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p == nil {
		s.progress = nil
		return
	}
	// Store a copy to ensure thread safety
	progressCopy := *p
	s.progress = &progressCopy
}

// StartBackup attempts to start a backup. If a backup is already running,
// it returns ErrBackupAlreadyRunning. Otherwise, it creates a new context
// derived from the parent context and returns it along with nil error.
// The caller should use the returned context for the backup operation.
// This method is safe to call from any goroutine.
func (s *BackupState) StartBackup(parent context.Context) (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil, ErrBackupAlreadyRunning
	}

	ctx, cancel := context.WithCancel(parent)
	s.running = true
	s.cancel = cancel
	s.progress = nil

	return ctx, nil
}

// StopBackup attempts to stop a running backup by calling its cancel function.
// Returns true if a backup was running and was cancelled, false otherwise.
// This method is safe to call from any goroutine.
func (s *BackupState) StopBackup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.cancel == nil {
		return false
	}

	s.cancel()
	return true
}

// Reset clears all backup state. This should be called when a backup completes
// (successfully or with error) to prepare for the next backup.
// This method is safe to call from any goroutine.
func (s *BackupState) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = false
	s.cancel = nil
	s.progress = nil
}

// GetCancel returns the current cancel function.
// Returns nil if no backup is running.
// This method is primarily for migration purposes and should be used sparingly.
func (s *BackupState) GetCancel() context.CancelFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cancel
}
