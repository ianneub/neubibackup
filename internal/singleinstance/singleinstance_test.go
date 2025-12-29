package singleinstance

import (
	"errors"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	// Acquire the lock
	lock, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Verify we got a lock
	if lock == nil {
		t.Fatal("Acquire() returned nil lock")
	}

	// Release the lock
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestDoubleAcquireFails(t *testing.T) {
	// Acquire the first lock
	lock1, err := Acquire()
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}
	defer lock1.Release()

	// Try to acquire a second lock - should fail
	lock2, err := Acquire()
	if err == nil {
		lock2.Release()
		t.Fatal("Second Acquire() should have failed")
	}

	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Expected ErrAlreadyRunning, got: %v", err)
	}
}

func TestAcquireAfterRelease(t *testing.T) {
	// Acquire and release the first lock
	lock1, err := Acquire()
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}
	if err := lock1.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Acquire a second lock - should succeed
	lock2, err := Acquire()
	if err != nil {
		t.Fatalf("Second Acquire() after release error = %v", err)
	}
	defer lock2.Release()
}

func TestDoubleRelease(t *testing.T) {
	lock, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// First release should succeed
	if err := lock.Release(); err != nil {
		t.Fatalf("First Release() error = %v", err)
	}

	// Second release should be a no-op (not error)
	if err := lock.Release(); err != nil {
		t.Fatalf("Second Release() error = %v", err)
	}
}
