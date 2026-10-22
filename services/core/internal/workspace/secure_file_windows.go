//go:build windows

package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openSecureFilePath(path string, access secureFileAccess, create bool) (*os.File, error) {
	volume := filepath.VolumeName(path)
	if volume == "" {
		return nil, errors.New("workspace: insecure file path")
	}
	rootName, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return nil, err
	}
	root, err := windows.CreateFile(rootName, windows.FILE_GENERIC_READ|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	relative := strings.TrimPrefix(path[len(volume):], `\`)
	components := strings.Split(relative, `\`)
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			_ = windows.CloseHandle(root)
			return nil, errors.New("workspace: insecure path component")
		}
		final := index == len(components)-1
		desiredAccess := uint32(windows.FILE_GENERIC_READ | windows.READ_CONTROL)
		options := uint32(windows.FILE_DIRECTORY_FILE | windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT)
		disposition := uint32(windows.FILE_OPEN)
		if final {
			options = windows.FILE_NON_DIRECTORY_FILE | windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT
			switch access {
			case secureFileRead:
			case secureFileAppend:
				desiredAccess = windows.FILE_APPEND_DATA | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE
			case secureFileReadWrite:
				desiredAccess = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.READ_CONTROL
			default:
				_ = windows.CloseHandle(root)
				return nil, errors.New("workspace: invalid file access")
			}
			if create {
				disposition = windows.FILE_CREATE
			}
		}
		name, err := windows.NewNTUnicodeString(component)
		if err != nil {
			_ = windows.CloseHandle(root)
			return nil, err
		}
		attributes := &windows.OBJECT_ATTRIBUTES{RootDirectory: root, ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
		attributes.Length = uint32(unsafe.Sizeof(*attributes))
		var next windows.Handle
		var status windows.IO_STATUS_BLOCK
		var allocation int64
		err = windows.NtCreateFile(&next, desiredAccess, attributes, &status, &allocation, 0,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, disposition, options, 0, 0)
		_ = windows.CloseHandle(root)
		if err != nil {
			return nil, err
		}
		var information windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(next, &information); err != nil ||
			information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
			(final && information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) ||
			(!final && information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0) {
			_ = windows.CloseHandle(next)
			return nil, errors.New("workspace: insecure file identity")
		}
		if final && validateSecureWindowsHandle(next) != nil {
			_ = windows.CloseHandle(next)
			return nil, errors.New("workspace: insecure file owner or permissions")
		}
		root = next
	}
	return os.NewFile(uintptr(root), path), nil
}

func validateSecureFileInfo(info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("workspace: insecure file identity")
	}
	metadata, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || metadata.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("workspace: insecure file identity")
	}
	return nil
}

func validateSecureWindowsHandle(handle windows.Handle) error {
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || descriptor == nil || !descriptor.IsValid() {
		return errors.New("workspace: invalid security descriptor")
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		return errors.New("workspace: file owner unavailable")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil || !owner.Equals(user.User.Sid) {
		return errors.New("workspace: file owner mismatch")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.New("workspace: file DACL unavailable")
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	const mutatingRights windows.ACCESS_MASK = windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA |
		windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | windows.DELETE | windows.WRITE_DAC |
		windows.WRITE_OWNER | windows.GENERIC_WRITE | windows.GENERIC_ALL
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return errors.New("workspace: invalid file DACL")
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("workspace: unsupported file DACL entry")
		}
		if ace.Mask&mutatingRights == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid == nil || (!sid.Equals(owner) && !sid.Equals(system) && !sid.Equals(administrators)) {
			return errors.New("workspace: file writable by another principal")
		}
	}
	return nil
}
