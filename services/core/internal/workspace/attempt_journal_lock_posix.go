//go:build !windows

package workspace

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func validatePlatformAttemptJournalLockDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return ErrAttemptJournalAuthentication
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != uint32(os.Geteuid()) {
		return ErrAttemptJournalAuthentication
	}
	return nil
}

func tryPlatformAttemptJournalLock(file *os.File) (bool, error) {
	if file == nil {
		return false, ErrAttemptJournalAuthentication
	}
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func unlockPlatformAttemptJournalLock(file *os.File) error {
	if file == nil {
		return ErrAttemptJournalAuthentication
	}
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
