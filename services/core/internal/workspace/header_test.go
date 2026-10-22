package workspace

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
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

func TestHeaderAuditMetadataIsBackwardCompatibleAndRejectsPartialState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.header")
	store, err := NewHeaderStore(path, bytes.Repeat([]byte{0x41}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	legacy := HeaderSlot{Version: 1, OperationID: "create", WorkspaceID: "workspace",
		PassphraseWrap: WrappedKey{Ciphertext: []byte("non-secret-fixture")}}
	if err := store.Initialize(legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Elect(func(operationID string, version uint64) bool {
		return operationID == "create" && version == 1
	})
	if err != nil || loaded.Audit != nil {
		t.Fatalf("legacy election=%#v err=%v", loaded, err)
	}
	partial := legacy.Clone()
	partial.Version = 2
	partial.OperationID = "partial-audit"
	partial.Audit = &AuditHeaderMetadata{SigningKeyID: "audit-key-v1"}
	if err := store.Prepare(partial); err == nil {
		t.Fatal("partial audit header metadata was accepted")
	}
	complete := partial.Clone()
	complete.OperationID = "complete-audit"
	salt := bytes.Repeat([]byte{0x11}, 32)
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy-audit-v1"))
	_, _ = digest.Write([]byte(complete.WorkspaceID))
	_, _ = digest.Write(salt)
	oldPrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize))
	complete.Audit = &AuditHeaderMetadata{
		ChainSalt: salt, GenesisHash: digest.Sum(nil),
		SigningPublicKey: oldPrivate.Public().(ed25519.PublicKey), SigningKeyID: "audit-key-v1",
	}
	if err := store.Prepare(complete); err != nil {
		t.Fatal(err)
	}
	elected, err := store.Elect(func(operationID string, version uint64) bool {
		return operationID == "complete-audit" && version == 2
	})
	if err != nil || elected.Audit == nil || !bytes.Equal(elected.Audit.SigningPublicKey, complete.Audit.SigningPublicKey) {
		t.Fatalf("complete audit metadata election=%#v err=%v", elected.Audit, err)
	}
	changedKey := elected.Clone()
	changedKey.Version++
	changedKey.OperationID = "changed-audit-key"
	changedKey.Audit.SigningPublicKey[0] ^= 0xff
	if err := store.Prepare(changedKey); !errors.Is(err, ErrHeaderVersion) {
		t.Fatalf("changed audit key metadata error=%v, want ErrHeaderVersion", err)
	}
	rotated := elected.Clone()
	rotated.Version++
	rotated.OperationID = "rotated-audit-key"
	newPrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x32}, ed25519.SeedSize))
	rotated.Audit.SigningPublicKey = newPrivate.Public().(ed25519.PublicKey)
	rotated.Audit.SigningKeyID = "audit-key-v2"
	rotated.Audit.PreviousSigningKeyID = elected.Audit.SigningKeyID
	digestRotation := auditSigningKeyRotationDigest(rotated.WorkspaceID, elected.Audit.SigningKeyID,
		rotated.Audit.SigningKeyID, rotated.Audit.SigningPublicKey)
	rotated.Audit.RotationSignature = ed25519.Sign(oldPrivate, digestRotation[:])
	if err := store.Prepare(rotated); err != nil {
		t.Fatalf("cross-signed audit key rotation error=%v", err)
	}
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
