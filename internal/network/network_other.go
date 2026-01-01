//go:build !darwin && !windows

package network

// RequestLocationPermission is a no-op on unsupported platforms.
func RequestLocationPermission() {}

// GetCurrentNetwork always returns unknown on unsupported platforms.
// This allows backups to proceed normally (fail-open behavior).
func GetCurrentNetwork() NetworkInfo {
	return NetworkInfo{Status: NetworkStatusUnknown}
}
