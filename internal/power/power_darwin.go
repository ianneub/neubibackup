//go:build darwin

package power

import (
	"os/exec"
	"strings"
)

// GetBatteryStatus checks the current power source on macOS.
// Returns BatteryStatusUnknown if the status cannot be determined.
func GetBatteryStatus() BatteryStatus {
	cmd := exec.Command("pmset", "-g", "batt")
	output, err := cmd.Output()
	if err != nil {
		return BatteryStatusUnknown
	}

	// Parse output - first line contains power source
	// Examples:
	//   "Now drawing from 'AC Power'"
	//   "Now drawing from 'Battery Power'"
	s := string(output)
	if strings.Contains(s, "'AC Power'") {
		return BatteryStatusOnAC
	}
	if strings.Contains(s, "'Battery Power'") {
		return BatteryStatusOnBattery
	}

	return BatteryStatusUnknown
}
