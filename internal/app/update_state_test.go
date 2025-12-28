package app

import (
	"sync"
	"testing"
)

func TestNewUpdateState(t *testing.T) {
	s := NewUpdateState()
	if s == nil {
		t.Fatal("NewUpdateState returned nil")
	}
	if s.IsInProgress() {
		t.Error("new UpdateState should not be in progress")
	}
	if s.GetAvailableVersion() != "" {
		t.Error("new UpdateState should have empty available version")
	}
}

func TestUpdateState_IsInProgress(t *testing.T) {
	s := NewUpdateState()

	// Initially not in progress
	if s.IsInProgress() {
		t.Error("expected not in progress initially")
	}

	// Set in progress
	s.SetInProgress(true)
	if !s.IsInProgress() {
		t.Error("expected in progress after SetInProgress(true)")
	}

	// Set not in progress
	s.SetInProgress(false)
	if s.IsInProgress() {
		t.Error("expected not in progress after SetInProgress(false)")
	}
}

func TestUpdateState_AvailableVersion(t *testing.T) {
	s := NewUpdateState()

	// Initially empty
	if s.GetAvailableVersion() != "" {
		t.Error("expected empty version initially")
	}
	if s.HasUpdate() {
		t.Error("expected HasUpdate() to return false initially")
	}

	// Set version
	s.SetAvailableVersion("1.2.3")
	if s.GetAvailableVersion() != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", s.GetAvailableVersion())
	}
	if !s.HasUpdate() {
		t.Error("expected HasUpdate() to return true after setting version")
	}

	// Clear version
	s.ClearAvailableVersion()
	if s.GetAvailableVersion() != "" {
		t.Error("expected empty version after ClearAvailableVersion")
	}
	if s.HasUpdate() {
		t.Error("expected HasUpdate() to return false after clearing")
	}

	// Set empty version
	s.SetAvailableVersion("2.0.0")
	s.SetAvailableVersion("")
	if s.GetAvailableVersion() != "" {
		t.Error("expected empty version after SetAvailableVersion(\"\")")
	}
}

func TestUpdateState_TryStartUpdate(t *testing.T) {
	s := NewUpdateState()

	// First try should succeed
	if !s.TryStartUpdate() {
		t.Error("first TryStartUpdate should return true")
	}
	if !s.IsInProgress() {
		t.Error("expected in progress after TryStartUpdate")
	}

	// Second try should fail
	if s.TryStartUpdate() {
		t.Error("second TryStartUpdate should return false")
	}

	// After finish, try should succeed again
	s.FinishUpdate()
	if s.IsInProgress() {
		t.Error("expected not in progress after FinishUpdate")
	}
	if !s.TryStartUpdate() {
		t.Error("TryStartUpdate after FinishUpdate should return true")
	}
}

func TestUpdateState_FinishUpdate(t *testing.T) {
	s := NewUpdateState()

	// Start update
	s.TryStartUpdate()
	if !s.IsInProgress() {
		t.Error("expected in progress after TryStartUpdate")
	}

	// Finish update
	s.FinishUpdate()
	if s.IsInProgress() {
		t.Error("expected not in progress after FinishUpdate")
	}

	// FinishUpdate when not in progress should be safe
	s.FinishUpdate()
	if s.IsInProgress() {
		t.Error("expected not in progress after second FinishUpdate")
	}
}

func TestUpdateState_ConcurrentReads(t *testing.T) {
	s := NewUpdateState()
	s.SetInProgress(true)
	s.SetAvailableVersion("1.0.0")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.IsInProgress()
			_ = s.GetAvailableVersion()
			_ = s.HasUpdate()
		}()
	}
	wg.Wait()
}

func TestUpdateState_ConcurrentReadWrite(t *testing.T) {
	s := NewUpdateState()

	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.SetInProgress(j%2 == 0)
				s.SetAvailableVersion("1.0.0")
				s.ClearAvailableVersion()
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = s.IsInProgress()
				_ = s.GetAvailableVersion()
				_ = s.HasUpdate()
			}
		}()
	}

	wg.Wait()
}

func TestUpdateState_ConcurrentTryStart(t *testing.T) {
	s := NewUpdateState()

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Multiple goroutines trying to start
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.TryStartUpdate() {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Only one should succeed
	if successCount != 1 {
		t.Errorf("expected exactly 1 successful start, got %d", successCount)
	}
}
