//go:build tammy_sqlcipher && cgo && darwin && arm64

package sqlcipher

import (
	"os"
	"syscall"
)

func lockWorkspaceFile(handle *os.File, exclusive bool) error {
	operation := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		operation = syscall.LOCK_EX | syscall.LOCK_NB
	}
	return syscall.Flock(int(handle.Fd()), operation)
}

func unlockWorkspaceFile(handle *os.File) error {
	return syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
}
