//go:build windows

package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// ErrAlreadyRunning indicates another instance is already running.
var ErrAlreadyRunning = errors.New("another instance of NeubiBackup is already running")

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
	procCloseHandle = kernel32.NewProc("CloseHandle")
)

const (
	errorAlreadyExists = 183
)

// mutexHandle holds the Windows mutex handle
var mutexHandle syscall.Handle

// tryLock attempts to create a named mutex to ensure single instance.
func (l *Lock) tryLock() error {
	// Create a unique mutex name for NeubiBackup
	mutexName, err := syscall.UTF16PtrFromString("Global\\NeubiBackupSingleInstance")
	if err != nil {
		return err
	}

	// CreateMutexW(lpMutexAttributes, bInitialOwner, lpName)
	ret, _, err := procCreateMutex.Call(
		0,                            // default security attributes
		1,                            // bInitialOwner = TRUE
		uintptr(unsafe.Pointer(mutexName)),
	)

	if ret == 0 {
		return fmt.Errorf("CreateMutex failed: %w", err)
	}

	mutexHandle = syscall.Handle(ret)

	// Check if the mutex already existed
	if err == syscall.Errno(errorAlreadyExists) {
		// Another instance is running
		procCloseHandle.Call(uintptr(mutexHandle))
		mutexHandle = 0
		return ErrAlreadyRunning
	}

	// Also create a lock file for consistency and to store PID
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		procCloseHandle.Call(uintptr(mutexHandle))
		mutexHandle = 0
		return err
	}

	// Write our PID to the lock file for debugging purposes
	f.Truncate(0)
	f.Seek(0, 0)
	f.WriteString(fmt.Sprintf("%d\n", os.Getpid()))

	l.lockFile = f
	return nil
}

// unlock releases the mutex.
func (l *Lock) unlock() error {
	if mutexHandle != 0 {
		procCloseHandle.Call(uintptr(mutexHandle))
		mutexHandle = 0
	}
	return nil
}
