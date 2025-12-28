package app

import (
	"sync"
)

// UpdateState provides thread-safe management of update check state.
// It tracks whether an update check is in progress and stores the
// available version if an update is found.
type UpdateState struct {
	mu               sync.RWMutex
	inProgress       bool
	availableVersion string
}

// NewUpdateState creates a new UpdateState with initial values.
func NewUpdateState() *UpdateState {
	return &UpdateState{}
}

// IsInProgress returns true if an update check is currently in progress.
// This method is safe to call from any goroutine.
func (s *UpdateState) IsInProgress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inProgress
}

// SetInProgress sets the update in-progress state.
// This method is safe to call from any goroutine.
func (s *UpdateState) SetInProgress(inProgress bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inProgress = inProgress
}

// GetAvailableVersion returns the available update version.
// Returns an empty string if no update is available.
// This method is safe to call from any goroutine.
func (s *UpdateState) GetAvailableVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.availableVersion
}

// SetAvailableVersion sets the available update version.
// Pass an empty string to clear the available version.
// This method is safe to call from any goroutine.
func (s *UpdateState) SetAvailableVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.availableVersion = version
}

// TryStartUpdate attempts to start an update. If an update is already
// in progress, it returns false. Otherwise, it sets inProgress to true
// and returns true.
// This method is safe to call from any goroutine.
func (s *UpdateState) TryStartUpdate() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.inProgress {
		return false
	}

	s.inProgress = true
	return true
}

// FinishUpdate marks the update as complete by setting inProgress to false.
// This method is safe to call from any goroutine.
func (s *UpdateState) FinishUpdate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inProgress = false
}

// HasUpdate returns true if an update is available (version is non-empty).
// This method is safe to call from any goroutine.
func (s *UpdateState) HasUpdate() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.availableVersion != ""
}

// ClearAvailableVersion clears the available version.
// This method is safe to call from any goroutine.
func (s *UpdateState) ClearAvailableVersion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.availableVersion = ""
}
