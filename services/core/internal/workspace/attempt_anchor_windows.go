//go:build windows

package workspace

import (
	"bytes"
	"errors"
	"syscall"
	"time"
	"unsafe"
)

type windowsAnchorStore struct{}

const platformAnchorMarkerSuffix = "/initialized"

func NewPlatformAnchorStore() (AnchorStore, error) {
	return windowsAnchorStore{}, nil
}

func (windowsAnchorStore) AcquireAttemptJournalLock(label string, timeout time.Duration) (attemptJournalLease, error) {
	path, err := platformAttemptJournalLockPath(label)
	if err != nil {
		return nil, err
	}
	lock, err := acquireAttemptJournalFileLock(path, timeout)
	if err != nil {
		return nil, err
	}
	lock.label = label
	return lock, nil
}

func anchorWindowsTarget(label string) (*uint16, error) {
	if label == "" {
		return nil, ErrAttemptJournalAuthentication
	}
	return syscall.UTF16PtrFromString("Tammy/AttemptJournalAnchor/v1/" + label)
}

func readWindowsAnchorValue(label string) ([]byte, bool, error) {
	target, err := anchorWindowsTarget(label)
	if err != nil {
		return nil, false, ErrAttemptJournalAuthentication
	}
	var pointer *windowsCredential
	result, _, callErr := credReadW.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0, uintptr(unsafe.Pointer(&pointer)))
	if result == 0 {
		if errors.Is(callErr, syscall.ERROR_NOT_FOUND) {
			return nil, false, nil
		}
		return nil, false, ErrAttemptJournalAuthentication
	}
	if pointer == nil || pointer.CredentialBlob == nil || pointer.CredentialBlobSize == 0 {
		if pointer != nil {
			credFree.Call(uintptr(unsafe.Pointer(pointer)))
		}
		return nil, true, ErrAttemptJournalAuthentication
	}
	defer credFree.Call(uintptr(unsafe.Pointer(pointer)))
	return append([]byte(nil), unsafe.Slice(pointer.CredentialBlob, int(pointer.CredentialBlobSize))...), true, nil
}

func writeWindowsAnchorValue(label string, value []byte) error {
	if len(value) == 0 {
		return ErrAttemptJournalAuthentication
	}
	target, err := anchorWindowsTarget(label)
	if err != nil {
		return ErrAttemptJournalAuthentication
	}
	credential := windowsCredential{
		Type: credentialTypeGeneric, TargetName: target,
		CredentialBlobSize: uint32(len(value)), CredentialBlob: &value[0], Persist: 2,
	}
	result, _, _ := credWriteW.Call(uintptr(unsafe.Pointer(&credential)), 0)
	if result == 0 {
		return ErrAttemptJournalAuthentication
	}
	return nil
}

func (windowsAnchorStore) Load(label string, lease attemptJournalLease) ([]byte, bool, error) {
	if label == "" || !validPlatformAttemptJournalLease(lease, label) {
		return nil, false, ErrAttemptJournalAuthentication
	}
	marker, markerFound, err := readWindowsAnchorValue(label + platformAnchorMarkerSuffix)
	if err != nil || (markerFound && !bytes.Equal(marker, []byte{1})) {
		Zero(marker)
		return nil, markerFound, ErrAttemptJournalAuthentication
	}
	Zero(marker)
	value, valueFound, err := readWindowsAnchorValue(label)
	if err != nil || (valueFound && len(value) != attemptJournalAnchorSize) {
		Zero(value)
		return nil, markerFound || valueFound, ErrAttemptJournalAuthentication
	}
	if markerFound {
		return value, true, nil
	}
	if !valueFound {
		return nil, false, nil
	}
	// Credential Manager can persist the create before the marker on a crash;
	// repair only that ordering window before the journal is allowed to open.
	if err := writeWindowsAnchorValue(label+platformAnchorMarkerSuffix, []byte{1}); err != nil {
		Zero(value)
		return nil, true, err
	}
	return value, true, nil
}

func (windowsAnchorStore) Initialize(label string, value []byte, lease attemptJournalLease) error {
	if label == "" || !validPlatformAttemptJournalLease(lease, label) || len(value) != attemptJournalAnchorSize {
		return ErrAttemptJournalAuthentication
	}
	previous, found, err := (windowsAnchorStore{}).Load(label, lease)
	Zero(previous)
	if err != nil || found {
		return ErrAttemptJournalAuthentication
	}
	// Credential Manager has no create-if-absent primitive. Tammy runs one core
	// per installation; this read-before-write boundary rejects reinitialization,
	// while Credential Manager supplies OS-account protection and durability.
	if err := writeWindowsAnchorValue(label, value); err != nil {
		return err
	}
	return writeWindowsAnchorValue(label+platformAnchorMarkerSuffix, []byte{1})
}

func (windowsAnchorStore) Save(label string, value []byte, lease attemptJournalLease) error {
	if label == "" || !validPlatformAttemptJournalLease(lease, label) || len(value) != attemptJournalAnchorSize {
		return ErrAttemptJournalAuthentication
	}
	previous, found, err := (windowsAnchorStore{}).Load(label, lease)
	validPrevious := len(previous) == attemptJournalAnchorSize
	current, currentErr := decodeAttemptJournalAnchor(previous)
	next, nextErr := decodeAttemptJournalAnchor(value)
	Zero(previous)
	if err != nil || !found || !validPrevious || currentErr != nil || nextErr != nil || next.Sequence != current.Sequence+1 {
		return ErrAttemptJournalAuthentication
	}
	return writeWindowsAnchorValue(label, value)
}
