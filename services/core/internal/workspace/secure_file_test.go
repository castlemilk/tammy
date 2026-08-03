package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecureRegularFileOpenRejectsIntermediateSymlink(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDirectory, "state"), []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openSecureRegularFile(filepath.Join(linkedDirectory, "state"), 1024); err == nil {
		t.Fatal("intermediate symlink was followed")
	}
}

func TestSecureRegularFileIdentityDetectsPathSubstitution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, identity, err := openSecureRegularFile(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSecureFilePath(path, identity); err == nil {
		t.Fatal("pathname substitution was accepted")
	}
}
