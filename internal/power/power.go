// Package power provides battery status detection.
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
