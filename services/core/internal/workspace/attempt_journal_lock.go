package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	attemptJournalLockTimeout = 2 * time.Second
	attemptJournalLockRetry   = 5 * time.Millisecond
)

type attemptJournalFileLock struct {
	file     *os.File
	identity os.FileInfo
	path     string
	label    string
	released bool
}

type attemptJournalLease interface {
	Release() error
	anchorLabel() string
	active() bool
}

type attemptJournalLockProvider interface {
	AcquireAttemptJournalLock(string, time.Duration) (attemptJournalLease, error)
}

type processAttemptJournalLockProvider struct{}

type processAttemptJournalLock struct {
	token    chan struct{}
	label    string
	released bool
}

var processAttemptJournalLocks = struct {
	sync.Mutex
	byAnchor map[string]chan struct{}
}{byAnchor: make(map[string]chan struct{})}

func (processAttemptJournalLockProvider) AcquireAttemptJournalLock(label string, timeout time.Duration) (attemptJournalLease, error) {
	if label == "" || timeout <= 0 {
		return nil, ErrAttemptJournalAuthentication
	}
	processAttemptJournalLocks.Lock()
	token := processAttemptJournalLocks.byAnchor[label]
	if token == nil {
		token = make(chan struct{}, 1)
		token <- struct{}{}
		processAttemptJournalLocks.byAnchor[label] = token
	}
	processAttemptJournalLocks.Unlock()
	select {
	case <-token:
		return &processAttemptJournalLock{token: token, label: label}, nil
	case <-time.After(timeout):
		return nil, ErrAttemptJournalAuthentication
	}
}

func (lock *processAttemptJournalLock) anchorLabel() string { return lock.label }
func (lock *processAttemptJournalLock) active() bool {
	return lock != nil && lock.token != nil && !lock.released
}

func (lock *processAttemptJournalLock) Release() error {
	if lock == nil || lock.token == nil || lock.released {
		return ErrAttemptJournalAuthentication
	}
	lock.released = true
	lock.token <- struct{}{}
	return nil
}

func platformAttemptJournalLockPath(label string) (string, error) {
	if label == "" {
		return "", ErrAttemptJournalAuthentication
	}
	configuration, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(configuration) {
		return "", ErrAttemptJournalAuthentication
	}
	root := filepath.Join(configuration, "Tammy", "attempt-journal-locks")
	for _, directory := range []string{filepath.Dir(root), root} {
		if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", ErrAttemptJournalAuthentication
		}
		if err := validatePlatformAttemptJournalLockDirectory(directory); err != nil {
			return "", ErrAttemptJournalAuthentication
		}
	}
	digest := sha256.Sum256([]byte(label))
	return filepath.Join(root, hex.EncodeToString(digest[:])+".lock"), nil
}

func acquireAttemptJournalFileLock(path string, timeout time.Duration) (*attemptJournalFileLock, error) {
	file, identity, err := openSecureRegularFileMode(path, 0, secureFileReadWrite, true)
	if errors.Is(err, os.ErrExist) {
		file, identity, err = openSecureRegularFileMode(path, 0, secureFileReadWrite, false)
	}
	if err != nil {
		return nil, errors.Join(ErrAttemptJournalAuthentication, err)
	}
	lock := &attemptJournalFileLock{file: file, identity: identity, path: path}
	acquireErr := retryAttemptJournalLock(func() (bool, error) {
		return tryPlatformAttemptJournalLock(file)
	}, timeout, attemptJournalLockRetry, time.Now, time.Sleep)
	if acquireErr != nil || validateSecureFilePath(path, identity) != nil {
		_ = file.Close()
		return nil, ErrAttemptJournalAuthentication
	}
	return lock, nil
}

func retryAttemptJournalLock(try func() (bool, error), timeout, retry time.Duration,
	now func() time.Time, wait func(time.Duration)) error {
	if try == nil || timeout <= 0 || retry <= 0 || now == nil || wait == nil {
		return ErrAttemptJournalAuthentication
	}
	deadline := now().Add(timeout)
	for {
		acquired, err := try()
		if err != nil {
			return ErrAttemptJournalAuthentication
		}
		if acquired {
			return nil
		}
		if !now().Before(deadline) {
			return ErrAttemptJournalAuthentication
		}
		wait(retry)
	}
}

func (lock *attemptJournalFileLock) Release() error {
	if lock == nil || lock.file == nil || lock.released {
		return ErrAttemptJournalAuthentication
	}
	lock.released = true
	pathErr := validateSecureFilePath(lock.path, lock.identity)
	unlockErr := unlockPlatformAttemptJournalLock(lock.file)
	closeErr := lock.file.Close()
	if pathErr != nil || unlockErr != nil || closeErr != nil {
		return ErrAttemptJournalAuthentication
	}
	return nil
}

func (lock *attemptJournalFileLock) anchorLabel() string { return lock.label }
func (lock *attemptJournalFileLock) active() bool {
	return lock != nil && lock.file != nil && !lock.released
}

func validAttemptJournalLease(lease attemptJournalLease, label string) bool {
	return lease != nil && label != "" && lease.anchorLabel() == label && lease.active()
}

func validPlatformAttemptJournalLease(lease attemptJournalLease, label string) bool {
	lock, ok := lease.(*attemptJournalFileLock)
	return ok && validAttemptJournalLease(lock, label)
}
