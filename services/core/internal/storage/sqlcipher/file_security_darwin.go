//go:build tammy_sqlcipher && cgo && darwin && arm64

package sqlcipher

import (
	"os"
	"syscall"
)

func validateDatabaseFileSecurity(info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ErrDatabaseIdentity
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0o600 {
		return ErrDatabasePermissions
	}
	return nil
}

func validateDatabaseParentSecurity(info os.FileInfo) error {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrDatabaseIdentity
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0o022 != 0 {
		return ErrDatabasePermissions
	}
	return nil
}
