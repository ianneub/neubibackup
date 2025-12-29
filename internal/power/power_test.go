package power

import (
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	callCount := 0
	callback := func() {
		callCount++
	}

	watcher := New(callback)

	if watcher == nil {
		t.Fatal("New() returned nil")
	}
	if watcher.callback == nil {
		t.Error("New() should set callback")
	}
	if watcher.stop == nil {
		t.Error("New() should initialize stop channel")
	}
}

func TestWatcher_CallbackStored(t *testing.T) {
	var callbackCalled bool
	callback := func() {
		callbackCalled = true
	}

	watcher := New(callback)

	// Verify callback is stored and callable
	if watcher.callback == nil {
		t.Fatal("callback not stored")
	}

	watcher.callback()

	if !callbackCalled {
		t.Error("callback was not invoked")
	}
}

func TestWatcher_Stop(t *testing.T) {
	watcher := New(func() {})

	// Stop should close the stop channel
	watcher.Stop()

	// Verify channel is closed by trying to receive
	select {
	case <-watcher.stop:
		// Expected - channel is closed
	default:
		t.Error("Stop() should close the stop channel")
	}
}

func TestWatcher_StopMultipleTimes(t *testing.T) {
	watcher := New(func() {})

	// First stop should work
	watcher.Stop()

	// Second stop should panic (closing closed channel)
	// This test documents current behavior
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on double Stop()")
		}
	}()

	watcher.Stop()
}

func TestNew_WithNilCallback(t *testing.T) {
	watcher := New(nil)

	if watcher == nil {
		t.Fatal("New(nil) should still return a watcher")
	}
	if watcher.callback != nil {
		t.Error("callback should be nil")
	}
}

func TestWatcher_ConcurrentAccess(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	callback := func() {
		mu.Lock()
		callCount++
		mu.Unlock()
	}

	watcher := New(callback)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			watcher.callback()
		}()
	}
	wg.Wait()

	mu.Lock()
	if callCount != 10 {
		t.Errorf("expected 10 callback calls, got %d", callCount)
	}
	mu.Unlock()
}
