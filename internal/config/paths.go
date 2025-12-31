// Package config handles configuration loading, saving, and path management.
package config

import (
	"os"
	"path/filepath"

	"neubibackup/internal/version"
)

const appDirName = "neubibackup"

// GetAppDir returns the path to the application data directory.
// For production builds, this is ~/neubibackup/ on all platforms.
// For dev builds (version="dev"), this returns .dev-data/ in the current
// working directory for persistent dev data.
// Tests can override via NEUBIBACKUP_APP_DIR environment variable.
func GetAppDir() (string, error) {
	// Allow env var override (for tests)
	if dir := os.Getenv("NEUBIBACKUP_APP_DIR"); dir != "" {
		return dir, nil
	}

	// Dev builds use .dev-data/ in current working directory
	if version.IsDev() {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, ".dev-data"), nil
	}

	// Production builds use ~/neubibackup/
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, appDirName), nil
}

// GetConfigPath returns the path to the config file.
func GetConfigPath() (string, error) {
	appDir, err := GetAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "config.yaml"), nil
}

// GetStatePath returns the path to the state file.
func GetStatePath() (string, error) {
	appDir, err := GetAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "state.yaml"), nil
}

// GetLogsDir returns the path to the logs directory.
func GetLogsDir() (string, error) {
	appDir, err := GetAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "logs"), nil
}

// GetTailscaleDir returns the path to the Tailscale state directory.
func GetTailscaleDir() (string, error) {
	appDir, err := GetAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "tailscale"), nil
}

// EnsureAppDir creates the application directory and logs subdirectory if they don't exist.
func EnsureAppDir() error {
	appDir, err := GetAppDir()
	if err != nil {
		return err
	}

	// Create app directory
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return err
	}

	// Create logs subdirectory
	logsDir, err := GetLogsDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(logsDir, 0755)
}

// ConfigExists returns true if the config file exists.
func ConfigExists() (bool, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
