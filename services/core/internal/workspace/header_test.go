package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHeaderStoreCrashElection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.header")
	key := bytes.Repeat([]byte{0x42}, 32)
	store, err := NewHeaderStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	first := HeaderSlot{Version: 1, OperationID: "create", WorkspaceID: "workspace", PassphraseWrap: WrappedKey{Ciphertext: []byte("first")}}
	if err := store.Initialize(first); err != nil {
		t.Fatal(err)
	}
	committed := map[string]uint64{"create": 1}
	match := func(operationID string, version uint64) bool { return committed[operationID] == version }

	second := HeaderSlot{Version: 2, OperationID: "change", WorkspaceID: "workspace", PassphraseWrap: WrappedKey{Ciphertext: []byte("second")}}
	if err := store.Prepare(second); err != nil {
		t.Fatal(err)
	}
	t.Run("crash before database audit commit keeps prior slot", func(t *testing.T) {
		elected, err := store.Elect(match)
		if err != nil || elected.Version != 1 {
			t.Fatalf("elected version %d: %v", elected.Version, err)
		}
	})

	committed["change"] = 2
	t.Run("crash before activation completes matching slot once", func(t *testing.T) {
		elected, err := store.Elect(match)
		if err != nil || elected.Version != 2 {
			t.Fatalf("elected version %d: %v", elected.Version, err)
		}
		reopened, err := NewHeaderStore(path, key)
		if err != nil {
			t.Fatal(err)
		}
		elected, err = reopened.Elect(match)
		if err != nil || elected.Version != 2 {
			t.Fatalf("re-elected version %d: %v", elected.Version, err)
		}
	})

	t.Run("tampering fails closed", func(t *testing.T) {
		content, err := readHeaderFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content.Slots[content.Active].Version++
		if err := writeHeaderFile(path, content); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Elect(match); !errors.Is(err, ErrHeaderAuthentication) {
			t.Fatalf("got %v, want authenticated-header failure", err)
		}
	})
}

func TestHeaderStoreRejectsInsecureOrOversizedFile(t *testing.T) {
	key := bytes.Repeat([]byte{0x43}, 32)
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, path string) {
				t.Helper()
				target := path + ".target"
				if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "group readable",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(`{}`), 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "oversized",
			setup: func(t *testing.T, path string) {
				t.Helper()
				file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(maxHeaderFileSize + 1); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace.header")
			test.setup(t, path)
			store, err := NewHeaderStore(path, key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Slots(); !errors.Is(err, ErrHeaderAuthentication) {
				t.Fatalf("got %v, want authenticated-header failure", err)
			}
		})
	}
}
