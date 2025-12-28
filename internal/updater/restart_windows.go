//go:build windows

// Package updater provides automatic update checking and installation.
package updater

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Windows process creation flags.
// See: https://docs.microsoft.com/en-us/windows/win32/procthread/process-creation-flags
const (
	createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	createNoWindow        = 0x08000000 // CREATE_NO_WINDOW
)

// Restart relaunches the application after an update.
// On Windows, we spawn a new detached process and exit the current one.
// The new process will use the updated binary since go-selfupdate
// renames the old exe and puts the new one in place.
func Restart() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// On Windows, we spawn a detached child process and exit.
	// CREATE_NEW_PROCESS_GROUP ensures the new process survives our exit.
	// CREATE_NO_WINDOW prevents a console window from appearing.
	cmd := exec.Command(executable)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNoWindow,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawning new process: %w", err)
	}

	// Exit current process - the new process is now running
	os.Exit(0)

	return nil // Never reached
}
