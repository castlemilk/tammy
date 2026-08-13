//go:build windows

package workspace

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func tryPlatformAttemptJournalLock(file *os.File) (bool, error) {
	if file == nil {
		return false, ErrAttemptJournalAuthentication
	}
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return err == nil, err
}

func unlockPlatformAttemptJournalLock(file *os.File) error {
	if file == nil {
		return ErrAttemptJournalAuthentication
	}
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped))
}

func validatePlatformAttemptJournalLockDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info == nil || !info.IsDir() {
		return ErrAttemptJournalAuthentication
	}
	metadata, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || metadata.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrAttemptJournalAuthentication
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrAttemptJournalAuthentication
	}
	handle, err := windows.CreateFile(name, windows.FILE_GENERIC_READ|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return ErrAttemptJournalAuthentication
	}
	defer windows.CloseHandle(handle)
	return validateSecureWindowsHandle(handle)
}
