//go:build !windows

package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func openSecureFilePath(path string, access secureFileAccess, create bool) (*os.File, error) {
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	if len(components) == 0 || components[len(components)-1] == "" {
		return nil, errors.New("workspace: insecure file path")
	}
	parent, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(parent)
			return nil, errors.New("workspace: insecure path component")
		}
		next, openErr := unix.Openat(parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(parent)
		if openErr != nil {
			return nil, openErr
		}
		parent = next
	}
	flags := unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	switch access {
	case secureFileRead:
		flags |= unix.O_RDONLY
	case secureFileAppend:
		flags |= unix.O_WRONLY | unix.O_APPEND
	case secureFileReadWrite:
		flags |= unix.O_RDWR
	default:
		_ = unix.Close(parent)
		return nil, errors.New("workspace: invalid file access")
	}
	if create {
		flags |= unix.O_CREAT | unix.O_EXCL
	}
	fd, err := unix.Openat(parent, components[len(components)-1], flags, 0o600)
	_ = unix.Close(parent)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func validateSecureFileInfo(info os.FileInfo) error {
	if info == nil {
		return errors.New("workspace: insecure file owner or permissions")
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		metadata.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0o600 {
		return errors.New("workspace: insecure file owner or permissions")
	}
	return nil
}
