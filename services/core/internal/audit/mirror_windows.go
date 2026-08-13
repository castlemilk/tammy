//go:build windows

package audit

import (
	"bytes"
	"errors"
	"os/user"
	"syscall"
	"unsafe"
)

const (
	auditMirrorCredentialTypeGeneric = 1
	auditMirrorMutexWaitMilliseconds = 5000
	auditMirrorWaitObject0           = 0
	auditMirrorWaitAbandoned         = 0x80
)

var (
	auditMirrorAdvapi32     = syscall.NewLazyDLL("advapi32.dll")
	auditMirrorCredWrite    = auditMirrorAdvapi32.NewProc("CredWriteW")
	auditMirrorCredRead     = auditMirrorAdvapi32.NewProc("CredReadW")
	auditMirrorCredFree     = auditMirrorAdvapi32.NewProc("CredFree")
	auditMirrorConvertSDDL  = auditMirrorAdvapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	auditMirrorKernel32     = syscall.NewLazyDLL("kernel32.dll")
	auditMirrorCreateMutex  = auditMirrorKernel32.NewProc("CreateMutexExW")
	auditMirrorWaitForMutex = auditMirrorKernel32.NewProc("WaitForSingleObject")
	auditMirrorReleaseMutex = auditMirrorKernel32.NewProc("ReleaseMutex")
	auditMirrorCloseHandle  = auditMirrorKernel32.NewProc("CloseHandle")
)

type auditMirrorWindowsCredential struct {
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

type windowsMirrorCredentials struct{}

func NewPlatformMirrorStore() (MirrorStore, error) {
	return newEncodedMirrorStore(windowsMirrorCredentials{}), nil
}

func auditMirrorWindowsTarget(label string) (*uint16, error) {
	return syscall.UTF16PtrFromString("Tammy/AuditMirror/" + label)
}

func (windowsMirrorCredentials) put(label string, value []byte) error {
	if label == "" || len(value) == 0 || len(value) > 2560 {
		return ErrMirrorInvalid
	}
	target, err := auditMirrorWindowsTarget(label)
	if err != nil {
		return ErrMirrorInvalid
	}
	credential := auditMirrorWindowsCredential{Type: auditMirrorCredentialTypeGeneric, TargetName: target,
		CredentialBlobSize: uint32(len(value)), CredentialBlob: &value[0], Persist: 2}
	result, _, _ := auditMirrorCredWrite.Call(uintptr(unsafe.Pointer(&credential)), 0)
	if result == 0 {
		return ErrMirrorInvalid
	}
	return nil
}

func (windowsMirrorCredentials) get(label string) ([]byte, error) {
	target, err := auditMirrorWindowsTarget(label)
	if err != nil {
		return nil, ErrMirrorInvalid
	}
	var pointer *auditMirrorWindowsCredential
	result, _, callErr := auditMirrorCredRead.Call(uintptr(unsafe.Pointer(target)), auditMirrorCredentialTypeGeneric,
		0, uintptr(unsafe.Pointer(&pointer)))
	if result == 0 {
		if errors.Is(callErr, syscall.ERROR_NOT_FOUND) {
			return nil, ErrMirrorMissing
		}
		return nil, ErrMirrorInvalid
	}
	if pointer == nil || pointer.CredentialBlob == nil || pointer.CredentialBlobSize == 0 || pointer.CredentialBlobSize > 2560 {
		if pointer != nil {
			auditMirrorCredFree.Call(uintptr(unsafe.Pointer(pointer)))
		}
		return nil, ErrMirrorInvalid
	}
	defer auditMirrorCredFree.Call(uintptr(unsafe.Pointer(pointer)))
	return append([]byte(nil), unsafe.Slice(pointer.CredentialBlob, int(pointer.CredentialBlobSize))...), nil
}

func (credentials windowsMirrorCredentials) compareAndSwap(label string, expected, replacement []byte) (bool, error) {
	if label == "" || len(replacement) == 0 || len(replacement) > 2560 {
		return false, ErrMirrorInvalid
	}
	var swapped bool
	err := withWindowsMirrorMutex(label, func() error {
		current, err := credentials.get(label)
		if errors.Is(err, ErrMirrorMissing) {
			if expected != nil {
				return nil
			}
		} else if err != nil {
			return err
		} else {
			if bytes.Equal(current, replacement) {
				swapped = true
				return nil
			}
			if expected == nil || !bytes.Equal(current, expected) {
				return nil
			}
		}
		if err := credentials.put(label, replacement); err != nil {
			return err
		}
		swapped = true
		return nil
	})
	return swapped, err
}

func withWindowsMirrorMutex(label string, operation func() error) error {
	if label == "" || operation == nil {
		return ErrMirrorInvalid
	}
	currentUser, err := user.Current()
	if err != nil || currentUser.Uid == "" {
		return ErrMirrorInvalid
	}
	contract, err := newWindowsMirrorMutexContract(currentUser.Uid, label)
	if err != nil {
		return ErrMirrorInvalid
	}
	attributes, releaseAttributes, err := newWindowsMirrorSecurityAttributes(contract)
	if err != nil {
		return ErrMirrorInvalid
	}
	defer releaseAttributes()
	name, err := syscall.UTF16PtrFromString(contract.name)
	if err != nil {
		return ErrMirrorInvalid
	}
	handle, _, _ := auditMirrorCreateMutex.Call(uintptr(unsafe.Pointer(attributes)), uintptr(unsafe.Pointer(name)),
		0, uintptr(contract.access))
	if handle == 0 {
		return ErrMirrorInvalid
	}
	defer auditMirrorCloseHandle.Call(handle)
	waitResult, _, _ := auditMirrorWaitForMutex.Call(handle, auditMirrorMutexWaitMilliseconds)
	if waitResult != auditMirrorWaitObject0 && waitResult != auditMirrorWaitAbandoned {
		return ErrMirrorInvalid
	}
	defer auditMirrorReleaseMutex.Call(handle)
	// An abandoned mutex transfers ownership here. compareAndSwap deliberately
	// re-reads Credential Manager state inside operation before deciding to write.
	return operation()
}

func newWindowsMirrorSecurityAttributes(contract windowsMirrorMutexContract) (*syscall.SecurityAttributes, func(), error) {
	if contract.sddl == "" || contract.access != windowsMirrorMutexAccess {
		return nil, nil, ErrMirrorInvalid
	}
	descriptorText, err := syscall.UTF16PtrFromString(contract.sddl)
	if err != nil {
		return nil, nil, ErrMirrorInvalid
	}
	var descriptor uintptr
	result, _, _ := auditMirrorConvertSDDL.Call(uintptr(unsafe.Pointer(descriptorText)), 1,
		uintptr(unsafe.Pointer(&descriptor)), 0)
	if result == 0 || descriptor == 0 {
		return nil, nil, ErrMirrorInvalid
	}
	attributes := &syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})),
		SecurityDescriptor: descriptor}
	release := func() {
		if descriptor != 0 {
			_, _ = syscall.LocalFree(syscall.Handle(descriptor))
			descriptor = 0
		}
	}
	return attributes, release, nil
}
