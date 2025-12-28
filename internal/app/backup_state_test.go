package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"neubibackup/internal/restic"
)

func TestNewBackupState(t *testing.T) {
	s := NewBackupState()
	if s == nil {
		t.Fatal("NewBackupState returned nil")
	}
	if s.IsRunning() {
		t.Error("new BackupState should not be running")
	}
	if s.GetProgress() != nil {
		t.Error("new BackupState should have nil progress")
	}
}

func TestBackupState_IsRunning(t *testing.T) {
	s := NewBackupState()

	// Initially not running
	if s.IsRunning() {
		t.Error("expected not running initially")
	}

	// Set running
	s.SetRunning(true)
	if !s.IsRunning() {
		t.Error("expected running after SetRunning(true)")
	}

	// Set not running
	s.SetRunning(false)
	if s.IsRunning() {
		t.Error("expected not running after SetRunning(false)")
	}
}

func TestBackupState_Progress(t *testing.T) {
	s := NewBackupState()

	// Initially nil
	if s.GetProgress() != nil {
		t.Error("expected nil progress initially")
	}

	// Set progress
	progress := &restic.BackupProgress{
		PercentDone:    0.5,
		FilesProcessed: 100,
		TotalFiles:     200,
		BytesProcessed: 1024,
		TotalBytes:     2048,
	}
	s.SetProgress(progress)

	// Get progress should return a copy
	got := s.GetProgress()
	if got == nil {
		t.Fatal("expected non-nil progress")
	}
	if got.PercentDone != 0.5 {
		t.Errorf("PercentDone = %v, want 0.5", got.PercentDone)
	}
	if got.FilesProcessed != 100 {
		t.Errorf("FilesProcessed = %v, want 100", got.FilesProcessed)
	}
	if got.TotalFiles != 200 {
		t.Errorf("TotalFiles = %v, want 200", got.TotalFiles)
	}

	// Verify it's a copy (modifying returned value doesn't affect internal state)
	got.PercentDone = 0.9
	got2 := s.GetProgress()
	if got2.PercentDone != 0.5 {
		t.Error("GetProgress should return a copy, not a reference")
	}

	// Clear progress
	s.SetProgress(nil)
	if s.GetProgress() != nil {
		t.Error("expected nil progress after SetProgress(nil)")
	}
}

func TestBackupState_StartBackup(t *testing.T) {
	s := NewBackupState()
	parent := context.Background()

	// First start should succeed
	ctx, err := s.StartBackup(parent)
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}
	if ctx == nil {
		t.Fatal("StartBackup returned nil context")
	}
	if !s.IsRunning() {
		t.Error("expected running after StartBackup")
	}

	// Second start should fail
	_, err = s.StartBackup(parent)
	if err != ErrBackupAlreadyRunning {
		t.Errorf("expected ErrBackupAlreadyRunning, got %v", err)
	}

	// Reset and try again
	s.Reset()
	ctx2, err := s.StartBackup(parent)
	if err != nil {
		t.Fatalf("StartBackup after Reset failed: %v", err)
	}
	if ctx2 == nil {
		t.Fatal("StartBackup after Reset returned nil context")
	}
}

func TestBackupState_StopBackup(t *testing.T) {
	s := NewBackupState()

	// Stop when not running should return false
	if s.StopBackup() {
		t.Error("StopBackup should return false when not running")
	}

	// Start a backup
	ctx, err := s.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	// Stop should return true and cancel the context
	if !s.StopBackup() {
		t.Error("StopBackup should return true when running")
	}

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("context should be cancelled after StopBackup")
	}
}

func TestBackupState_Reset(t *testing.T) {
	s := NewBackupState()

	// Start a backup and set progress
	_, err := s.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}
	s.SetProgress(&restic.BackupProgress{PercentDone: 0.5})

	// Verify state
	if !s.IsRunning() {
		t.Error("expected running before Reset")
	}
	if s.GetProgress() == nil {
		t.Error("expected progress before Reset")
	}

	// Reset
	s.Reset()

	// Verify cleared
	if s.IsRunning() {
		t.Error("expected not running after Reset")
	}
	if s.GetProgress() != nil {
		t.Error("expected nil progress after Reset")
	}
	if s.GetCancel() != nil {
		t.Error("expected nil cancel after Reset")
	}
}

func TestBackupState_ContextCancellation(t *testing.T) {
	s := NewBackupState()
	parent, parentCancel := context.WithCancel(context.Background())

	ctx, err := s.StartBackup(parent)
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	// Cancel parent context
	parentCancel()

	// Child context should also be cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("child context should be cancelled when parent is cancelled")
	}
}

func TestBackupState_ConcurrentReads(t *testing.T) {
	s := NewBackupState()
	s.SetRunning(true)
	s.SetProgress(&restic.BackupProgress{PercentDone: 0.5})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.IsRunning()
			_ = s.GetProgress()
		}()
	}
	wg.Wait()
}

func TestBackupState_ConcurrentReadWrite(t *testing.T) {
	s := NewBackupState()

	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.SetRunning(j%2 == 0)
				s.SetProgress(&restic.BackupProgress{
					PercentDone: float64(j) / 100,
				})
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = s.IsRunning()
				_ = s.GetProgress()
			}
		}()
	}

	wg.Wait()
}

func TestBackupState_ConcurrentStartStop(t *testing.T) {
	s := NewBackupState()

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Multiple goroutines trying to start
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.StartBackup(context.Background())
			if err == nil {
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
