//go:build windows

package network

import (
	"os/exec"
	"strings"
)

// RequestLocationPermission is a no-op on Windows.
// Windows doesn't require location permission for SSID detection.
func RequestLocationPermission() {}

// GetCurrentNetwork returns the current WiFi network information on Windows.
// Returns NetworkStatusUnknown if WiFi is off, not connected, or on error.
func GetCurrentNetwork() NetworkInfo {
	// Command: netsh wlan show interfaces
	// Output includes "SSID                   : NetworkName" when connected
	// Output includes "State                  : disconnected" when not connected

	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	output, err := cmd.Output()
	if err != nil {
		return NetworkInfo{Status: NetworkStatusUnknown}
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for "SSID" but not "BSSID" (which contains MAC address)
		if strings.HasPrefix(line, "SSID") && !strings.HasPrefix(line, "BSSID") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				ssid := strings.TrimSpace(parts[1])
				if ssid != "" {
					return NetworkInfo{
						Status: NetworkStatusConnected,
						SSID:   ssid,
					}
				}
			}
		}
	}

	return NetworkInfo{Status: NetworkStatusUnknown}
}
