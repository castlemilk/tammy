package workspace

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPinnedPasswordDenylist(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	path := filepath.Join(filepath.Dir(filename), "../../../../compliance/passwords/tammy-common-passwords-v1.txt")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	policy, err := LoadPasswordDenylist(file, 10_000, "c63d5e4ccc31344d662583cc39ca4bd5bd20517ff1d24501f0c4e0c22d9b722a", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Hash([]byte("MAILCREATED5240")); !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("case-folded pinned password returned %v", err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	policy, err := NewPasswordPolicy([]string{"forbidden password"}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("normalises NFC without trimming", func(t *testing.T) {
		composed := []byte("  très-long-password  ")
		decomposed := []byte("  tre\u0300s-long-password  ")
		verifier, err := policy.Hash(decomposed)
		if err != nil {
			t.Fatal(err)
		}
		if !policy.Verify(composed, verifier) {
			t.Fatal("canonically equivalent password was not accepted")
		}
		if policy.Verify(bytes.TrimSpace(composed), verifier) {
			t.Fatal("password was silently trimmed")
		}
	})

	t.Run("enforces exact bounds and case folded denylist", func(t *testing.T) {
		for name, secret := range map[string][]byte{
			"fourteen code points":                []byte(strings.Repeat("a", 14)),
			"one hundred twenty nine code points": []byte(strings.Repeat("a", 129)),
			"over 1024 bytes":                     []byte(strings.Repeat("界", 342)),
			"case folded denylist":                []byte("FORBIDDEN PASSWORD"),
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := policy.Hash(secret); !errors.Is(err, ErrPasswordPolicy) {
					t.Fatalf("got %v, want password policy failure", err)
				}
			})
		}
	})

	t.Run("uses exact Argon2id parameters and history", func(t *testing.T) {
		verifier, err := policy.Hash([]byte("correct horse battery staple"))
		if err != nil {
			t.Fatal(err)
		}
		if verifier.MemoryKiB != 64*1024 || verifier.Iterations != 3 || verifier.Parallelism != 1 ||
			len(verifier.Salt) != 16 || len(verifier.Digest) != 32 {
			t.Fatalf("unexpected verifier parameters: %+v", verifier)
		}
		if !policy.Reused([]byte("correct horse battery staple"), []PasswordVerifier{verifier}) {
			t.Fatal("password history reuse was not detected")
		}
	})
}

func TestWorkspaceKeyEnvelope(t *testing.T) {
	policy, err := NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("correct horse battery staple")
	material, display, err := GenerateKeyMaterial(policy, passphrase, "workspace-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	defer Zero(display)

	if len(material.DEK) != 32 || len(display) != 64 || bytes.Contains(material.RecoveryWrap.Ciphertext, display) {
		t.Fatalf("unexpected key material shape")
	}
	if _, err := open(material.PassphraseWrap.Verifier.Digest, material.PassphraseWrap.Nonce,
		material.PassphraseWrap.Ciphertext, wrapAAD("passphrase", "workspace-1", 1)); err == nil {
		t.Fatal("persisted password verifier decrypted the workspace DEK")
	}
	groups, err := ParseRecoveryGroups(display)
	if err != nil || len(groups) != 13 {
		t.Fatalf("parse grouped recovery secret: groups=%d err=%v", len(groups), err)
	}
	fromPassphrase, err := UnwrapWithPassphrase(policy, passphrase, material.PassphraseWrap, "workspace-1", 1)
	if err != nil || !bytes.Equal(fromPassphrase, material.DEK) {
		t.Fatalf("passphrase unwrap failed: %v", err)
	}
	defer Zero(fromPassphrase)
	fromRecovery, err := UnwrapWithRecovery(display, material.RecoveryWrap, "workspace-1", 1)
	if err != nil || !bytes.Equal(fromRecovery, material.DEK) {
		t.Fatalf("recovery unwrap failed: %v", err)
	}
	defer Zero(fromRecovery)
	if _, err := UnwrapWithPassphrase(policy, []byte("wrong-but-long-enough"), material.PassphraseWrap, "workspace-1", 1); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("wrong passphrase returned %v", err)
	}
	if _, err := UnwrapWithRecovery([]byte(strings.Repeat("A", 64)), material.RecoveryWrap, "workspace-1", 1); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("wrong recovery secret returned %v", err)
	}
}
