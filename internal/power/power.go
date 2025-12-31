// Package power provides system wake detection and battery status.
package power

// BatteryStatus represents the current power source.
type BatteryStatus int

const (
	// BatteryStatusUnknown indicates the power source could not be determined.
	BatteryStatusUnknown BatteryStatus = iota
	// BatteryStatusOnAC indicates the system is running on AC power.
	BatteryStatusOnAC
	// BatteryStatusOnBattery indicates the system is running on battery power.
	BatteryStatusOnBattery
)

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
