//go:build windows

package restic

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW is a Windows process creation flag that prevents
// the creation of a console window when starting a new process.
// See: https://docs.microsoft.com/en-us/windows/win32/procthread/process-creation-flags
const createNoWindow = 0x08000000

// configureCmd adds Windows-specific flags to hide console windows.
// On Windows, exec.Command spawns a visible console window by default.
// The CREATE_NO_WINDOW flag prevents this for GUI applications.
func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
	}
}
