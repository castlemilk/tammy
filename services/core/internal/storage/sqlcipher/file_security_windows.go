//go:build tammy_sqlcipher && cgo && windows && amd64

package sqlcipher

import (
	"os"
	"syscall"
)

func validateDatabaseFileSecurity(info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() || hasWindowsReparsePoint(info) {
		return ErrDatabaseIdentity
	}
	return nil
}

func validateDatabaseParentSecurity(info os.FileInfo) error {
	if info == nil || !info.IsDir() || hasWindowsReparsePoint(info) {
		return ErrDatabaseIdentity
	}
	return nil
}

func hasWindowsReparsePoint(info os.FileInfo) bool {
	metadata, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return !ok || metadata.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
