//go:build !windows

package restic

import "os/exec"

// configureCmd is a no-op on non-Windows platforms.
// On macOS and Linux, exec.Command does not create console windows.
func configureCmd(cmd *exec.Cmd) {}
