//go:build windows

package audit

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsMirrorMutexMaterializesExplicitNonInheritedSecurityAttributes(t *testing.T) {
	contract, err := newWindowsMirrorMutexContract("S-1-5-21-111111111-222222222-333333333-1001",
		"01890f60-4d6d-7c12-8f02-6c9129d5b001")
	if err != nil {
		t.Fatal(err)
	}
	attributes, release, err := newWindowsMirrorSecurityAttributes(contract)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if attributes == nil || attributes.Length != uint32(unsafe.Sizeof(syscall.SecurityAttributes{})) ||
		attributes.SecurityDescriptor == 0 || attributes.InheritHandle != 0 {
		t.Fatalf("security attributes=%#v", attributes)
	}
}
