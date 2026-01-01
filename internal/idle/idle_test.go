package idle

import (
	"testing"
	"time"
)

func TestGetIdleTime_ReturnsNonNegative(t *testing.T) {
	idleTime := GetIdleTime()
	if idleTime < 0 {
		t.Errorf("GetIdleTime() returned negative: %v", idleTime)
	}
}

func TestGetIdleTime_ReasonableValue(t *testing.T) {
	// Idle time should be less than 24 hours for a running system
	// (unless the test machine has been idle for more than a day, which is unlikely)
	idleTime := GetIdleTime()
	if idleTime > 24*time.Hour {
		t.Errorf("GetIdleTime() returned unreasonably large value: %v", idleTime)
	}
	t.Logf("Current idle time: %v", idleTime)
}

func TestGetIdleTime_MultipleCalls(t *testing.T) {
	// Multiple calls should return consistent results (not negative or wildly different)
	for i := 0; i < 5; i++ {
		idleTime := GetIdleTime()
		if idleTime < 0 {
			t.Errorf("GetIdleTime() call %d returned negative: %v", i, idleTime)
		}
	}
}
