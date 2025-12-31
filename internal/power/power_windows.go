//go:build windows

package power

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
)

// systemPowerStatus represents the SYSTEM_POWER_STATUS structure from Windows API.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

const (
	acLineOffline = 0
	acLineOnline  = 1
)

// GetBatteryStatus checks the current power source on Windows.
// Returns BatteryStatusUnknown if the status cannot be determined.
func GetBatteryStatus() BatteryStatus {
	var status systemPowerStatus
	ret, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return BatteryStatusUnknown
	}

	switch status.ACLineStatus {
	case acLineOnline:
		return BatteryStatusOnAC
	case acLineOffline:
		return BatteryStatusOnBattery
	default:
		return BatteryStatusUnknown
	}
}
