//go:build darwin && arm64 && cgo

package platform

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeBookmarkBridge struct {
	resolution                                        bridgeResolution
	resolveErr                                        error
	startOK                                           bool
	resolveCalls, startCalls, stopCalls, releaseCalls int
}

type fakeBookmarkHandle uint64

func (fakeBookmarkHandle) bookmarkHandle() {}

func (b *fakeBookmarkBridge) Resolve([]byte) (bridgeResolution, error) {
	b.resolveCalls++
	return b.resolution, b.resolveErr
}
func (b *fakeBookmarkBridge) Start(bookmarkHandle) bool { b.startCalls++; return b.startOK }
func (b *fakeBookmarkBridge) Stop(bookmarkHandle)       { b.stopCalls++ }
func (b *fakeBookmarkBridge) Release(bookmarkHandle)    { b.releaseCalls++ }

func TestBookmarkResolverBalancesAccessAndOwnsPath(t *testing.T) {
	path := filepath.Join(secureTempDir(t), "credential.p12")
	if err := os.WriteFile(path, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	bridge := &fakeBookmarkBridge{resolution: bridgeResolution{handle: fakeBookmarkHandle(7), path: path, securityScoped: true}, startOK: true}
	access, err := (&BookmarkResolver{bridge: bridge}).Resolve([]byte("bookmark"), path)
	if err != nil {
		t.Fatal(err)
	}
	if access.Path() != path || bridge.resolveCalls != 1 || bridge.startCalls != 1 || bridge.stopCalls != 0 || bridge.releaseCalls != 0 {
		t.Fatalf("access=%#v bridge=%#v", access, bridge)
	}
	got, err := access.ReadAll(64)
	if err != nil || !bytes.Equal(got, []byte("synthetic")) {
		t.Fatalf("descriptor read=%q, %v", got, err)
	}
	if err := access.Close(); err != nil {
		t.Fatal(err)
	}
	if err := access.Close(); err != nil {
		t.Fatal(err)
	}
	if bridge.stopCalls != 1 || bridge.releaseCalls != 1 {
		t.Fatalf("unbalanced close: %#v", bridge)
	}
}

func TestBookmarkResolverClosesEveryPostStartFailure(t *testing.T) {
	selected := filepath.Join(secureTempDir(t), "selected.p12")
	other := filepath.Join(secureTempDir(t), "other.p12")
	if err := os.WriteFile(selected, []byte("selected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	bridge := &fakeBookmarkBridge{resolution: bridgeResolution{handle: fakeBookmarkHandle(9), path: other, securityScoped: true}, startOK: true}
	_, err := (&BookmarkResolver{bridge: bridge}).Resolve([]byte("bookmark"), selected)
	if !errors.Is(err, ErrBookmarkInvalid) || bridge.startCalls != 1 || bridge.stopCalls != 1 || bridge.releaseCalls != 1 || strings.Contains(err.Error(), selected) {
		t.Fatalf("error=%v bridge=%#v", err, bridge)
	}

	symlink := filepath.Join(secureTempDir(t), "credential-link.p12")
	if err := os.Symlink(selected, symlink); err != nil {
		t.Fatal(err)
	}
	bridge = &fakeBookmarkBridge{resolution: bridgeResolution{handle: fakeBookmarkHandle(10), path: symlink, securityScoped: true}, startOK: true}
	_, err = (&BookmarkResolver{bridge: bridge}).Resolve([]byte("bookmark"), symlink)
	if !errors.Is(err, ErrBookmarkInvalid) || bridge.stopCalls != 1 || bridge.releaseCalls != 1 {
		t.Fatalf("symlink error=%v bridge=%#v", err, bridge)
	}
}

func TestBookmarkResolverRejectsStaleUnscopedInvalidAndFailedStartWithoutLeaks(t *testing.T) {
	path := filepath.Join(secureTempDir(t), "credential.p12")
	if err := os.WriteFile(path, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []fakeBookmarkBridge{
		{resolution: bridgeResolution{handle: fakeBookmarkHandle(1), path: path, stale: true, securityScoped: true}, startOK: true},
		{resolution: bridgeResolution{handle: fakeBookmarkHandle(2), path: path, securityScoped: false}, startOK: true},
		{resolution: bridgeResolution{handle: fakeBookmarkHandle(3), path: path, securityScoped: true}, startOK: false},
		{resolveErr: errors.New("raw path detail")},
	}
	for index := range tests {
		bridge := &tests[index]
		_, err := (&BookmarkResolver{bridge: bridge}).Resolve([]byte("bookmark"), path)
		if !errors.Is(err, ErrBookmarkInvalid) || err.Error() != "SBR_BOOKMARK_INVALID" {
			t.Fatalf("case %d error=%v", index, err)
		}
		if index < 2 && (bridge.startCalls != 0 || bridge.stopCalls != 0 || bridge.releaseCalls != 1) {
			t.Fatalf("case %d leaked: %#v", index, bridge)
		}
		if index == 2 && (bridge.startCalls != 1 || bridge.stopCalls != 0 || bridge.releaseCalls != 1) {
			t.Fatalf("failed start lifecycle: %#v", bridge)
		}
		if index == 3 && bridge.releaseCalls != 0 {
			t.Fatalf("resolve error released unknown handle: %#v", bridge)
		}
	}
	for _, bookmark := range [][]byte{nil, make([]byte, (64<<10)+1)} {
		bridge := &fakeBookmarkBridge{}
		_, err := (&BookmarkResolver{bridge: bridge}).Resolve(bookmark, path)
		if !errors.Is(err, ErrBookmarkInvalid) || bridge.resolveCalls != 0 {
			t.Fatalf("bookmark bounds error=%v bridge=%#v", err, bridge)
		}
	}
}

func TestBookmarkResolverBindsConsumptionToRetainedDescriptor(t *testing.T) {
	root := secureTempDir(t)
	authority := filepath.Join(root, "authority")
	if err := os.Mkdir(authority, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(authority, "credential.p12")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	bridge := &fakeBookmarkBridge{resolution: bridgeResolution{handle: fakeBookmarkHandle(11), path: path, securityScoped: true}, startOK: true}
	access, err := (&BookmarkResolver{bridge: bridge}).Resolve([]byte("bookmark"), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(authority, filepath.Join(root, "original-authority")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(authority, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := access.Revalidate(); !errors.Is(err, ErrPathAuthorityChanged) {
		t.Fatalf("revalidation = %v", err)
	}
	got, err := access.ReadAll(64)
	if err != nil || !bytes.Equal(got, []byte("inside")) {
		t.Fatalf("retained read = %q, %v", got, err)
	}
	if err := access.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := access.ReadAll(64); !errors.Is(err, ErrPathAuthorityClosed) {
		t.Fatalf("read after close = %v", err)
	}
	if bridge.stopCalls != 1 || bridge.releaseCalls != 1 {
		t.Fatalf("unbalanced close: %#v", bridge)
	}
}
