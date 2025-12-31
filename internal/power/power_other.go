//go:build !darwin && !windows

package power

// GetBatteryStatus always returns unknown on unsupported platforms.
// This allows backups to proceed normally (fail-open behavior).
func GetBatteryStatus() BatteryStatus {
	return BatteryStatusUnknown
}

// Start is a no-op on unsupported platforms.
func (w *Watcher) Start() {
	// No wake detection on this platform
}
