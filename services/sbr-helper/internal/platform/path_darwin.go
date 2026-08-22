//go:build darwin && arm64 && cgo

package platform

/*
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <unistd.h>

static int tammy_open_root_directory(void) {
  return open("/", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
}

static int tammy_open_child_directory(int parent, const char *name) {
  return openat(parent, name, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
}

static int tammy_open_child_regular(int parent, const char *name) {
  return openat(parent, name, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
}

static int tammy_file_identity(int fd, uint64_t *values) {
  struct stat info;
  if (fstat(fd, &info) != 0) return -1;
  values[0] = (uint64_t)info.st_dev;
  values[1] = (uint64_t)info.st_ino;
  values[2] = (uint64_t)info.st_mode;
  values[3] = (uint64_t)info.st_uid;
  values[4] = (uint64_t)info.st_size;
  values[5] = (uint64_t)info.st_mtimespec.tv_sec;
  values[6] = (uint64_t)info.st_mtimespec.tv_nsec;
  values[7] = (uint64_t)info.st_ctimespec.tv_sec;
  values[8] = (uint64_t)info.st_ctimespec.tv_nsec;
  return 0;
}

static int tammy_path_identity(int parent, const char *name, uint64_t *values) {
  struct stat info;
  if (fstatat(parent, name, &info, AT_SYMLINK_NOFOLLOW) != 0) return -1;
  values[0] = (uint64_t)info.st_dev;
  values[1] = (uint64_t)info.st_ino;
  values[2] = (uint64_t)info.st_mode;
  values[3] = (uint64_t)info.st_uid;
  values[4] = (uint64_t)info.st_size;
  values[5] = (uint64_t)info.st_mtimespec.tv_sec;
  values[6] = (uint64_t)info.st_mtimespec.tv_nsec;
  values[7] = (uint64_t)info.st_ctimespec.tv_sec;
  values[8] = (uint64_t)info.st_ctimespec.tv_nsec;
  return 0;
}

static int tammy_pread_exact(int fd, void *buffer, size_t length) {
  size_t offset = 0;
  while (offset < length) {
    ssize_t count = pread(fd, (char *)buffer + offset, length - offset, (off_t)offset);
    if (count < 0 && errno == EINTR) continue;
    if (count <= 0) return -1;
    offset += (size_t)count;
  }
  return 0;
}

static void tammy_close_descriptor(int fd) { if (fd >= 0) close(fd); }
*/
import "C"

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unsafe"
)

var (
	ErrPathAuthorityInvalid = errors.New("SBR_PATH_AUTHORITY_INVALID")
	ErrPathAuthorityChanged = errors.New("SBR_PATH_AUTHORITY_CHANGED")
	ErrPathAuthorityClosed  = errors.New("SBR_PATH_AUTHORITY_CLOSED")
)

type fileIdentity [9]uint64

type guardedComponent struct {
	name       string
	directory  bool
	descriptor int
	identity   fileIdentity
}

// PathGuard binds authority to an open descriptor chain. Revalidation checks
// that the public pathname still resolves to the same retained identities.
type PathGuard struct {
	mu         sync.Mutex
	components []guardedComponent
	regular    bool
	closed     bool
}

func openRegularNoFollow(path string) (*PathGuard, error)   { return openPathNoFollow(path, true) }
func openDirectoryNoFollow(path string) (*PathGuard, error) { return openPathNoFollow(path, false) }

func openPathNoFollow(path string, regular bool) (*PathGuard, error) {
	components, ok := lexicalPathComponents(path)
	if !ok || (regular && len(components) == 0) {
		return nil, ErrPathAuthorityInvalid
	}
	rootDescriptor := int(C.tammy_open_root_directory())
	if rootDescriptor < 0 {
		return nil, ErrPathAuthorityInvalid
	}
	rootIdentity, ok := descriptorIdentity(rootDescriptor)
	if !ok {
		C.tammy_close_descriptor(C.int(rootDescriptor))
		return nil, ErrPathAuthorityInvalid
	}
	guard := &PathGuard{components: []guardedComponent{{directory: true, descriptor: rootDescriptor, identity: rootIdentity}}, regular: regular}
	parent := rootDescriptor
	for index, name := range components {
		directory := index < len(components)-1 || !regular
		descriptor := openChild(parent, name, directory)
		if descriptor < 0 {
			_ = guard.Close()
			return nil, ErrPathAuthorityInvalid
		}
		identity, valid := descriptorIdentity(descriptor)
		pathIdentity, pathValid := childPathIdentity(parent, name)
		mode := uint32(identity[2]) & syscall.S_IFMT
		expectedMode := uint32(syscall.S_IFREG)
		if directory {
			expectedMode = uint32(syscall.S_IFDIR)
		}
		if !valid || !pathValid || !sameRetainedIdentity(pathIdentity, identity, directory) || mode != expectedMode {
			C.tammy_close_descriptor(C.int(descriptor))
			_ = guard.Close()
			return nil, ErrPathAuthorityInvalid
		}
		guard.components = append(guard.components, guardedComponent{name: strings.Clone(name), directory: directory, descriptor: descriptor, identity: identity})
		parent = descriptor
	}
	return guard, nil
}

func (g *PathGuard) Revalidate() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrPathAuthorityClosed
	}
	descriptors := make([]int, 0, len(g.components))
	defer func() {
		for index := len(descriptors) - 1; index >= 0; index-- {
			C.tammy_close_descriptor(C.int(descriptors[index]))
		}
	}()
	root := int(C.tammy_open_root_directory())
	if root < 0 {
		return ErrPathAuthorityChanged
	}
	descriptors = append(descriptors, root)
	identity, ok := descriptorIdentity(root)
	if !ok || !sameRetainedIdentity(g.components[0].identity, identity, true) {
		return ErrPathAuthorityChanged
	}
	parent := root
	for index := 1; index < len(g.components); index++ {
		expected := g.components[index]
		descriptor := openChild(parent, expected.name, expected.directory)
		if descriptor < 0 {
			return ErrPathAuthorityChanged
		}
		descriptors = append(descriptors, descriptor)
		identity, ok = descriptorIdentity(descriptor)
		pathIdentity, pathOK := childPathIdentity(parent, expected.name)
		if !ok || !pathOK || !sameRetainedIdentity(pathIdentity, identity, expected.directory) || !sameRetainedIdentity(expected.identity, identity, expected.directory) {
			return ErrPathAuthorityChanged
		}
		parent = descriptor
	}
	return nil
}

func sameRetainedIdentity(expected, actual fileIdentity, directory bool) bool {
	if !directory {
		return expected == actual
	}
	// Directory entry churn changes size and timestamps without changing the
	// retained authority. Device, inode, type/mode and owner remain security
	// relevant; path-chain re-open still detects rename or symlink swaps.
	return expected[0] == actual[0] && expected[1] == actual[1] && expected[2] == actual[2] && expected[3] == actual[3]
}

func (g *PathGuard) ReadAll(maximum int) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, ErrPathAuthorityClosed
	}
	if !g.regular || maximum < 0 {
		return nil, ErrPathAuthorityInvalid
	}
	leaf := g.components[len(g.components)-1]
	before, ok := descriptorIdentity(leaf.descriptor)
	if !ok || before != leaf.identity {
		return nil, ErrPathAuthorityChanged
	}
	if before[4] > uint64(maximum) {
		return nil, ErrPathAuthorityInvalid
	}
	data := make([]byte, int(before[4]))
	if len(data) > 0 && C.tammy_pread_exact(C.int(leaf.descriptor), unsafe.Pointer(&data[0]), C.size_t(len(data))) != 0 {
		clearBytes(data)
		return nil, ErrPathAuthorityChanged
	}
	after, ok := descriptorIdentity(leaf.descriptor)
	if !ok || after != before {
		clearBytes(data)
		return nil, ErrPathAuthorityChanged
	}
	return data, nil
}

func (g *PathGuard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	for index := len(g.components) - 1; index >= 0; index-- {
		C.tammy_close_descriptor(C.int(g.components[index].descriptor))
		g.components[index].descriptor = -1
		g.components[index].name = ""
	}
	g.closed = true
	return nil
}

func (g *PathGuard) isOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.closed
}

func openChild(parent int, name string, directory bool) int {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	if directory {
		return int(C.tammy_open_child_directory(C.int(parent), cName))
	}
	return int(C.tammy_open_child_regular(C.int(parent), cName))
}

func descriptorIdentity(descriptor int) (fileIdentity, bool) {
	var identity fileIdentity
	ok := C.tammy_file_identity(C.int(descriptor), (*C.uint64_t)(unsafe.Pointer(&identity[0]))) == 0
	return identity, ok
}

func childPathIdentity(parent int, name string) (fileIdentity, bool) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	var identity fileIdentity
	ok := C.tammy_path_identity(C.int(parent), cName, (*C.uint64_t)(unsafe.Pointer(&identity[0]))) == 0
	return identity, ok
}

func lexicalPathComponents(path string) ([]string, bool) {
	if path == "" || len(path) > 4<<10 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false
	}
	for _, value := range path {
		if unicode.IsControl(value) {
			return nil, false
		}
	}
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil, true
	}
	components := strings.Split(trimmed, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, false
		}
	}
	return components, true
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
