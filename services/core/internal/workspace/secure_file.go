package workspace

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

const (
	maxHeaderFileSize             int64 = 1 << 20
	maxAttemptJournalFileSize     int64 = 16 << 20
	maxWorkspaceCatalogueFileSize int64 = 16 << 20
)

type secureFileAccess uint8

const (
	secureFileRead secureFileAccess = iota + 1
	secureFileAppend
	secureFileReadWrite
)

func openSecureRegularFile(path string, maximum int64) (*os.File, os.FileInfo, error) {
	return openSecureRegularFileMode(path, maximum, secureFileRead, false)
}

func openSecureRegularFileMode(path string, maximum int64, access secureFileAccess, create bool) (*os.File, os.FileInfo, error) {
	if maximum < 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, errors.New("workspace: insecure file path")
	}
	file, err := openSecureFilePath(path, access, create)
	if err != nil {
		return nil, nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	identity, err := file.Stat()
	if err != nil || validateSecureFileInfo(identity) != nil || identity.Size() < 0 || identity.Size() > maximum ||
		validateSecureFilePath(path, identity) != nil {
		_ = file.Close()
		return nil, nil, errors.New("workspace: insecure file")
	}
	return file, identity, nil
}

func validateSecureFilePath(path string, identity os.FileInfo) error {
	current, err := os.Lstat(path)
	if err != nil || validateSecureFileInfo(current) != nil || !os.SameFile(current, identity) {
		return errors.New("workspace: file identity changed")
	}
	return nil
}

func readSecureRegularFile(path string, maximum int64) ([]byte, os.FileInfo, error) {
	file, identity, err := openSecureRegularFile(path, maximum)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(payload)) > maximum {
		return nil, nil, errors.New("workspace: bounded file read failed")
	}
	after, err := file.Stat()
	if err != nil || validateSecureFileInfo(after) != nil || !os.SameFile(identity, after) ||
		after.Size() != identity.Size() || int64(len(payload)) != identity.Size() || validateSecureFilePath(path, identity) != nil {
		return nil, nil, errors.New("workspace: file changed while reading")
	}
	return payload, identity, nil
}

func appendSecureRegularFile(path string, expected os.FileInfo, payload []byte, maximum int64) (os.FileInfo, int64, error) {
	file, identity, err := openSecureRegularFileMode(path, maximum, secureFileAppend, expected == nil)
	if err != nil {
		return nil, 0, err
	}
	if expected != nil && !os.SameFile(expected, identity) {
		_ = file.Close()
		return nil, 0, errors.New("workspace: journal identity changed")
	}
	if int64(len(payload)) > maximum-identity.Size() {
		_ = file.Close()
		return nil, 0, errors.New("workspace: journal size limit exceeded")
	}
	written, writeErr := file.Write(payload)
	if writeErr == nil && written != len(payload) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return nil, 0, errors.Join(writeErr, syncErr, closeErr)
	}
	if err := validateSecureFilePath(path, identity); err != nil {
		return nil, 0, err
	}
	return identity, identity.Size() + int64(len(payload)), nil
}

func truncateSecureRegularFile(path string, expected os.FileInfo, size int64, maximum int64) error {
	file, identity, err := openSecureRegularFileMode(path, maximum, secureFileReadWrite, false)
	if err != nil {
		return err
	}
	if expected == nil || !os.SameFile(expected, identity) {
		_ = file.Close()
		return errors.New("workspace: journal identity changed")
	}
	truncateErr := file.Truncate(size)
	syncErr := file.Sync()
	closeErr := file.Close()
	if truncateErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(truncateErr, syncErr, closeErr)
	}
	return validateSecureFilePath(path, identity)
}
