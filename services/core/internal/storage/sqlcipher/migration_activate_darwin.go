//go:build tammy_sqlcipher && cgo && darwin && arm64

package sqlcipher

import "os"

func activateMigratedWorkspace(stagedPath, activePath string, _ bool) error {
	return os.Rename(stagedPath, activePath)
}

func syncMigrationParent(parent string) error {
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
