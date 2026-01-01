package network

import (
	"testing"
)

func TestNetworkStatusConstants(t *testing.T) {
	// Verify constant values are distinct
	if NetworkStatusUnknown == NetworkStatusConnected {
		t.Error("NetworkStatusUnknown should differ from NetworkStatusConnected")
	}
}

func TestGetCurrentNetwork_ReturnsValidStatus(t *testing.T) {
	info := GetCurrentNetwork()

	// Should return valid status
	switch info.Status {
	case NetworkStatusUnknown, NetworkStatusConnected:
		t.Logf("Current network status: %v, SSID: %q", info.Status, info.SSID)
	default:
		t.Errorf("GetCurrentNetwork() returned invalid status: %d", info.Status)
	}

	// If connected, SSID should be non-empty
	if info.Status == NetworkStatusConnected && info.SSID == "" {
		t.Error("NetworkStatusConnected should have non-empty SSID")
	}
}

func TestNetworkInfo_ZeroValue(t *testing.T) {
	var info NetworkInfo

	// Zero value should be Unknown status
	if info.Status != NetworkStatusUnknown {
		t.Errorf("Zero value Status = %v, want NetworkStatusUnknown", info.Status)
	}

	if info.SSID != "" {
		t.Errorf("Zero value SSID = %q, want empty", info.SSID)
	}
}
