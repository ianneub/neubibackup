//go:build windows

package idle

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

// LASTINPUTINFO structure for Windows API
type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// getIdleTime returns the duration since the last user input on Windows.
// Uses GetLastInputInfo and GetTickCount Windows APIs.
func getIdleTime() time.Duration {
	var lii lastInputInfo
	lii.cbSize = uint32(unsafe.Sizeof(lii))

	ret, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if ret == 0 {
		// Error: assume user is active (fail-safe)
		return 0
	}

	tickCount, _, _ := procGetTickCount.Call()

	// Calculate idle time in milliseconds
	// Handle potential tick count wraparound (every ~49.7 days)
	currentTick := uint32(tickCount)
	idleMs := currentTick - lii.dwTime

	return time.Duration(idleMs) * time.Millisecond
}
