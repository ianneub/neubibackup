// Package power provides system wake detection.
package power

// WakeCallback is called when the system wakes from sleep.
type WakeCallback func()

// Watcher monitors system power events.
type Watcher struct {
	callback WakeCallback
	stop     chan struct{}
}

// New creates a new power watcher.
func New(callback WakeCallback) *Watcher {
	return &Watcher{
		callback: callback,
		stop:     make(chan struct{}),
	}
}

// Stop stops the power watcher.
func (w *Watcher) Stop() {
	close(w.stop)
}
