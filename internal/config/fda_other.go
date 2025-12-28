//go:build !darwin

package config

// HasFullDiskAccess always returns true on non-macOS platforms.
func HasFullDiskAccess() bool {
	return true
}

// OpenFullDiskAccessSettings is a no-op on non-macOS platforms.
func OpenFullDiskAccessSettings() error {
	return nil
}
