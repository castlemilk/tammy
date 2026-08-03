//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"errors"
	"os"
	"sync"
)

var ErrWorkspaceLocked = errors.New("sqlcipher: workspace is locked by another operation")

// workspaceFileLock uses a stable, empty sidecar solely for cross-process lock
// identity. It never stores a path, key, evidence, or accounting data.
type workspaceFileLock struct {
	closeErr  error
	closeOnce sync.Once
	handle    *os.File
}

func acquireWorkspaceFileLock(databasePath string, exclusive bool) (*workspaceFileLock, error) {
	lockPath := databasePath + ".lock"
	initial, statErr := os.Lstat(lockPath)
	flags := os.O_RDWR
	if os.IsNotExist(statErr) {
		flags |= os.O_CREATE | os.O_EXCL
	} else if statErr != nil || validateDatabaseFileSecurity(initial) != nil || initial.Size() != 0 {
		return nil, ErrDatabasePermissions
	}
	handle, err := os.OpenFile(lockPath, flags, 0o600)
	if os.IsExist(err) && flags&os.O_EXCL != 0 {
		initial, statErr = os.Lstat(lockPath)
		if statErr != nil || validateDatabaseFileSecurity(initial) != nil || initial.Size() != 0 {
			return nil, ErrDatabasePermissions
		}
		handle, err = os.OpenFile(lockPath, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, ErrDatabasePermissions
	}
	identity, err := handle.Stat()
	if err != nil || validateDatabaseFileSecurity(identity) != nil || identity.Size() != 0 ||
		(initial != nil && !os.SameFile(initial, identity)) {
		_ = handle.Close()
		return nil, ErrDatabaseIdentity
	}
	if err := lockWorkspaceFile(handle, exclusive); err != nil {
		_ = handle.Close()
		return nil, ErrWorkspaceLocked
	}
	return &workspaceFileLock{handle: handle}, nil
}

func (lock *workspaceFileLock) close() error {
	if lock == nil {
		return nil
	}
	lock.closeOnce.Do(func() {
		lock.closeErr = errors.Join(unlockWorkspaceFile(lock.handle), lock.handle.Close())
	})
	return lock.closeErr
}
