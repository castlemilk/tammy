//go:build tammy_sqlcipher && cgo && windows && amd64

package sqlcipher

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	lockFileExclusiveLock   = 0x00000002
	lockFileFailImmediately = 0x00000001
)

var (
	lockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	unlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

func lockWorkspaceFile(handle *os.File, exclusive bool) error {
	flags := uintptr(lockFileFailImmediately)
	if exclusive {
		flags |= lockFileExclusiveLock
	}
	var overlapped syscall.Overlapped
	result, _, callErr := lockFileEx.Call(
		handle.Fd(), flags, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return callErr
	}
	return nil
}

func unlockWorkspaceFile(handle *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := unlockFileEx.Call(
		handle.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return callErr
	}
	return nil
}
