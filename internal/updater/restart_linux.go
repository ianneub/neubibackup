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

// launchPath is where the binary lived when this process started. Restart
// can't ask os.Executable after an update: go-selfupdate renames the running
// binary to .<name>.old, moves the new one into place and deletes the .old
// file, and /proc/self/exe follows the running inode to that deleted name.
var launchPath, launchPathErr = os.Executable()

// Restart relaunches the application after an update.
// On Linux, we exec the updated binary in place of the current process.
// Keeping the same PID means whatever launched us (the XDG autostart unit,
// a systemd user service, a terminal) keeps tracking the app, and the
// single-instance lock is released on exec because its fd is close-on-exec.
func Restart() error {
	if launchPathErr != nil {
		return fmt.Errorf("getting executable path: %w", launchPathErr)
	}

	// Only returns on failure.
	if err := execFn(launchPath, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", launchPath, err)
	}
	return nil
}
