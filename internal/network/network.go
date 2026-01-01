// Package network provides WiFi network detection.
package network

// NetworkStatus represents the result of SSID detection.
type NetworkStatus int

const (
	// NetworkStatusUnknown indicates the SSID could not be determined.
	// This includes: no WiFi adapter, not connected, errors, unsupported platform.
	NetworkStatusUnknown NetworkStatus = iota
	// NetworkStatusConnected indicates successfully detected a WiFi SSID.
	NetworkStatusConnected
)

// NetworkInfo contains the current WiFi network information.
type NetworkInfo struct {
	Status NetworkStatus
	SSID   string // Empty if Status != NetworkStatusConnected
}
