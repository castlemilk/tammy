//go:build windows

package workspace

import (
	"syscall"
	"unsafe"
)

const credentialTypeGeneric = 1

var (
	advapi32   = syscall.NewLazyDLL("advapi32.dll")
	credWriteW = advapi32.NewProc("CredWriteW")
	credReadW  = advapi32.NewProc("CredReadW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

type windowsCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsSecretStore struct{}

func newPlatformSecretStore() (SecretStore, error) { return windowsSecretStore{}, nil }

func windowsTarget(label string) (*uint16, error) {
	return syscall.UTF16PtrFromString("Tammy/RememberedWorkspace/" + label)
}

func (windowsSecretStore) Put(label string, secret []byte) error {
	if label == "" || len(secret) == 0 || len(secret) > 2560 {
		return ErrRememberedKeyUnavailable
	}
	target, err := windowsTarget(label)
	if err != nil {
		return ErrRememberedKeyUnavailable
	}
	credential := windowsCredential{
		Type: credentialTypeGeneric, TargetName: target,
		CredentialBlobSize: uint32(len(secret)), CredentialBlob: &secret[0], Persist: 2,
	}
	result, _, _ := credWriteW.Call(uintptr(unsafe.Pointer(&credential)), 0)
	if result == 0 {
		return ErrRememberedKeyUnavailable
	}
	return nil
}

func (windowsSecretStore) Get(label string) ([]byte, error) {
	target, err := windowsTarget(label)
	if err != nil {
		return nil, ErrRememberedKeyUnavailable
	}
	var pointer *windowsCredential
	result, _, _ := credReadW.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0, uintptr(unsafe.Pointer(&pointer)))
	if result == 0 || pointer == nil || pointer.CredentialBlob == nil || pointer.CredentialBlobSize == 0 {
		return nil, ErrRememberedKeyUnavailable
	}
	defer credFree.Call(uintptr(unsafe.Pointer(pointer)))
	return append([]byte(nil), unsafe.Slice(pointer.CredentialBlob, int(pointer.CredentialBlobSize))...), nil
}

func (windowsSecretStore) Delete(label string) error {
	target, err := windowsTarget(label)
	if err != nil {
		return ErrRememberedKeyUnavailable
	}
	result, _, callErr := credDelete.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0)
	if result == 0 && callErr != syscall.ERROR_NOT_FOUND {
		return ErrRememberedKeyUnavailable
	}
	return nil
}
