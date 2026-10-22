//go:build tammy_sqlcipher && cgo && windows && amd64

package sqlcipher

import (
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func activateMigratedWorkspace(stagedPath, activePath string, replacing bool) error {
	staged, err := syscall.UTF16PtrFromString(stagedPath)
	if err != nil {
		return err
	}
	active, err := syscall.UTF16PtrFromString(activePath)
	if err != nil {
		return err
	}
	flags := uintptr(moveFileWriteThrough)
	if replacing {
		flags |= moveFileReplaceExisting
	}
	result, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(staged)),
		uintptr(unsafe.Pointer(active)),
		flags,
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return callErr
	}
	return nil
}

func syncMigrationParent(string) error {
	// MoveFileExW with MOVEFILE_WRITE_THROUGH flushes the activation boundary.
	return nil
}
