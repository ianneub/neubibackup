package power

import (
	"testing"
)

func TestBatteryStatusConstants(t *testing.T) {
	// Verify constant values are distinct
	if BatteryStatusUnknown == BatteryStatusOnAC {
		t.Error("BatteryStatusUnknown should differ from BatteryStatusOnAC")
	}
	if BatteryStatusOnAC == BatteryStatusOnBattery {
		t.Error("BatteryStatusOnAC should differ from BatteryStatusOnBattery")
	}
	if BatteryStatusUnknown == BatteryStatusOnBattery {
		t.Error("BatteryStatusUnknown should differ from BatteryStatusOnBattery")
	}
}

func TestGetBatteryStatus_ReturnsValidStatus(t *testing.T) {
	status := GetBatteryStatus()

	// Should return one of the valid statuses
	switch status {
	case BatteryStatusUnknown, BatteryStatusOnAC, BatteryStatusOnBattery:
		// Valid - log for information
		t.Logf("Current battery status: %v", status)
	default:
		t.Errorf("GetBatteryStatus() returned invalid status: %d", status)
	}
}
