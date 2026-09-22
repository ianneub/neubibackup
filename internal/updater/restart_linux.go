//go:build linux

// Package updater provides automatic update checking and installation.
package updater

import (
	"fmt"
	"os"
	"syscall"
)

// execFn replaces the current process image. A package-level var so tests
// can observe the call instead of actually re-executing the test binary.
var execFn = syscall.Exec

// Restart relaunches the application after an update.
// On Linux, we exec the updated binary in place of the current process.
// Keeping the same PID means whatever launched us (the XDG autostart unit,
// a systemd user service, a terminal) keeps tracking the app, and the
// single-instance lock is released on exec because its fd is close-on-exec.
func Restart() error {
	// After go-selfupdate renames the new binary into place, /proc/self/exe
	// points at the deleted old inode; os.Executable strips the " (deleted)"
	// suffix, so this is the path of the new binary.
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// Only returns on failure.
	if err := execFn(executable, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", executable, err)
	}
	return nil
}
