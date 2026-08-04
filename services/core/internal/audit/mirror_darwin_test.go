//go:build darwin && cgo

package audit

import "testing"

func TestDarwinMirrorAdapterUsesNativeCredentialStore(t *testing.T) {
	store, err := NewPlatformMirrorStore()
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("native mirror store is nil")
	}
}
