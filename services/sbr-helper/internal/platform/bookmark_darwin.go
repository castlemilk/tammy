//go:build darwin && arm64 && cgo

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#include <Foundation/Foundation.h>
#include <stdlib.h>

static void* tammy_resolve_bookmark(const void *bytes, size_t length, int *stale, char **path) {
  @autoreleasepool {
    NSData *data = [NSData dataWithBytes:bytes length:length];
    BOOL wasStale = NO;
    NSError *error = nil;
    NSURL *url = [NSURL URLByResolvingBookmarkData:data
      options:(NSURLBookmarkResolutionWithSecurityScope | NSURLBookmarkResolutionWithoutUI)
      relativeToURL:nil bookmarkDataIsStale:&wasStale error:&error];
    if (url == nil || error != nil || !url.fileURL) return NULL;
    const char *fileSystemPath = url.fileSystemRepresentation;
    if (fileSystemPath == NULL) return NULL;
    *path = strdup(fileSystemPath);
    if (*path == NULL) return NULL;
    *stale = wasStale ? 1 : 0;
    return (void *)[url retain];
  }
}

static int tammy_start_access(void *handle) {
  @autoreleasepool { return [(NSURL *)handle startAccessingSecurityScopedResource] ? 1 : 0; }
}

static void tammy_stop_access(void *handle) {
  @autoreleasepool { [(NSURL *)handle stopAccessingSecurityScopedResource]; }
}

static void tammy_release_url(void *handle) {
  @autoreleasepool { [(NSURL *)handle release]; }
}
*/
import "C"

import (
	"errors"
	"strings"
	"sync"
	"unsafe"
)

var ErrBookmarkInvalid = errors.New("SBR_BOOKMARK_INVALID")

type bridgeResolution struct {
	handle         bookmarkHandle
	path           string
	stale          bool
	securityScoped bool
}

type bookmarkHandle interface{ bookmarkHandle() }

type bookmarkBridge interface {
	Resolve([]byte) (bridgeResolution, error)
	Start(bookmarkHandle) bool
	Stop(bookmarkHandle)
	Release(bookmarkHandle)
}

type darwinBookmarkBridge struct{}
type darwinBookmarkHandle struct{ pointer unsafe.Pointer }

func (darwinBookmarkHandle) bookmarkHandle() {}

func (darwinBookmarkBridge) Resolve(bookmark []byte) (bridgeResolution, error) {
	var stale C.int
	var path *C.char
	handle := C.tammy_resolve_bookmark(unsafe.Pointer(&bookmark[0]), C.size_t(len(bookmark)), &stale, &path)
	if handle == nil {
		return bridgeResolution{}, ErrBookmarkInvalid
	}
	defer C.free(unsafe.Pointer(path))
	return bridgeResolution{handle: darwinBookmarkHandle{pointer: handle}, path: C.GoString(path), stale: stale != 0, securityScoped: true}, nil
}

func (darwinBookmarkBridge) Start(handle bookmarkHandle) bool {
	value, ok := handle.(darwinBookmarkHandle)
	return ok && C.tammy_start_access(value.pointer) != 0
}
func (darwinBookmarkBridge) Stop(handle bookmarkHandle) {
	if value, ok := handle.(darwinBookmarkHandle); ok {
		C.tammy_stop_access(value.pointer)
	}
}
func (darwinBookmarkBridge) Release(handle bookmarkHandle) {
	if value, ok := handle.(darwinBookmarkHandle); ok {
		C.tammy_release_url(value.pointer)
	}
}

type BookmarkResolver struct{ bridge bookmarkBridge }

func NewBookmarkResolver() *BookmarkResolver {
	return &BookmarkResolver{bridge: darwinBookmarkBridge{}}
}

type ScopedFile struct {
	path   string
	handle bookmarkHandle
	bridge bookmarkBridge
	guard  *PathGuard
	once   sync.Once
	mu     sync.RWMutex
}

func (f *ScopedFile) Path() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return strings.Clone(f.path)
}

func (f *ScopedFile) Close() error {
	f.once.Do(func() {
		_ = f.guard.Close()
		f.bridge.Stop(f.handle)
		f.bridge.Release(f.handle)
		f.mu.Lock()
		f.handle = nil
		f.path = ""
		f.mu.Unlock()
	})
	return nil
}

func (f *ScopedFile) Revalidate() error                   { return f.guard.Revalidate() }
func (f *ScopedFile) ReadAll(maximum int) ([]byte, error) { return f.guard.ReadAll(maximum) }

func (r *BookmarkResolver) Resolve(bookmark []byte, selectedPath string) (*ScopedFile, error) {
	_, selectedPathValid := lexicalPathComponents(selectedPath)
	if r == nil || r.bridge == nil || len(bookmark) == 0 || len(bookmark) > 64<<10 || !selectedPathValid {
		return nil, ErrBookmarkInvalid
	}
	resolution, err := r.bridge.Resolve(bookmark)
	if err != nil {
		return nil, ErrBookmarkInvalid
	}
	_, resolvedPathValid := lexicalPathComponents(resolution.path)
	if resolution.handle == nil || resolution.stale || !resolution.securityScoped || !resolvedPathValid {
		if resolution.handle != nil {
			r.bridge.Release(resolution.handle)
		}
		return nil, ErrBookmarkInvalid
	}
	if !r.bridge.Start(resolution.handle) {
		r.bridge.Release(resolution.handle)
		return nil, ErrBookmarkInvalid
	}
	if resolution.path != selectedPath {
		r.bridge.Stop(resolution.handle)
		r.bridge.Release(resolution.handle)
		return nil, ErrBookmarkInvalid
	}
	guard, err := openRegularNoFollow(resolution.path)
	if err != nil {
		r.bridge.Stop(resolution.handle)
		r.bridge.Release(resolution.handle)
		return nil, ErrBookmarkInvalid
	}
	access := &ScopedFile{path: strings.Clone(resolution.path), handle: resolution.handle, bridge: r.bridge, guard: guard}
	return access, nil
}
