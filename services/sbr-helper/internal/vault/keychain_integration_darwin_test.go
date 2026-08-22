//go:build darwin && cgo && tammy_sbr_keychain_integration

package vault

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeychainSyntheticCredentialLifecycle(t *testing.T) {
	if os.Getenv("TAMMY_SBR_KEYCHAIN_INTEGRATION") != "1" {
		t.Skip("set TAMMY_SBR_KEYCHAIN_INTEGRATION=1 to run the isolated synthetic Keychain test")
	}
	suffix := "integration." + strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", "")
	directory, err := os.MkdirTemp("/private/tmp", "tammy-sbr-vault-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	keychainPath := filepath.Join("/private/tmp", fmt.Sprintf("tsv-%d.keychain", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(keychainPath) })
	if output, createErr := exec.Command("/usr/bin/security", "create-keychain", "-p", "synthetic-test-keychain-password", keychainPath).CombinedOutput(); createErr != nil {
		t.Fatalf("create isolated synthetic Keychain: %v (%s)", createErr, strings.TrimSpace(string(output)))
	}
	if output, unlockErr := exec.Command("/usr/bin/security", "unlock-keychain", "-p", "synthetic-test-keychain-password", keychainPath).CombinedOutput(); unlockErr != nil {
		t.Fatalf("unlock isolated synthetic Keychain: %v (%s)", unlockErr, strings.TrimSpace(string(output)))
	}
	store, err := openIsolatedDevelopmentKeychainStore(keychainPath, suffix)
	if err != nil {
		if status, ok := err.(interface{ status() int32 }); ok {
			t.Fatalf("isolated Keychain create: %v (OSStatus %d)", err, status.status())
		}
		t.Fatal(err)
	}
	wipeEvents := 0
	native := store.native.(securityFrameworkNative)
	native.wipeObserver = func(cleared bool) {
		wipeEvents++
		if !cleared {
			t.Error("helper-owned native CFData buffer was not cleared")
		}
	}
	store.native = native
	t.Cleanup(func() {
		if err := store.closeIsolated(); err != nil {
			t.Errorf("isolated Keychain cleanup: %v", err)
		}
	})
	policy := store.policy
	probe := "tammy.sbr.development/integration-probe"
	if err := store.Create(probe, []byte("synthetic-probe"), policy); err != nil {
		if status, ok := err.(interface{ status() int32 }); ok {
			t.Fatalf("enabled isolated Keychain integration unavailable: %v (OSStatus %d)", err, status.status())
		}
		t.Fatalf("enabled isolated Keychain integration unavailable: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(probe) })
	casProbe := "tammy.sbr.development/integration-cas-probe"
	original := []byte("synthetic-original")
	if err := store.Create(casProbe, original, policy); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(casProbe) })
	wrongDigest := strings.Repeat("0", 64)
	if err := store.CompareAndReplace(casProbe, wrongDigest, []byte("synthetic-newer"), policy); !errors.Is(err, ErrVaultCASConflict) {
		t.Fatalf("wrong-digest native replace = %v", err)
	}
	if err := store.CompareAndDelete(casProbe, wrongDigest); !errors.Is(err, ErrVaultCASConflict) {
		t.Fatalf("wrong-digest native delete = %v", err)
	}
	unchanged, err := store.Read(casProbe)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != string(original) {
		t.Fatal("wrong-digest native CAS changed the stored value")
	}
	clear(unchanged)
	clear(original)

	credentialPath := filepath.Join(directory, "synthetic-credential.p12")
	if err := os.WriteFile(credentialPath, []byte("synthetic-credential-format"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := newVault(vaultConfig{Store: store, Channel: developmentChannel(), Component: &syntheticComponent{}}, rand.Reader)
	if err != nil {
		t.Fatalf("enabled isolated Keychain integration initialisation failed: %v", err)
	}
	t.Cleanup(v.Close)
	operation := "018f0000-0000-7000-8000-000000000071"
	replace := "018f0000-0000-7000-8000-000000000072"
	remove := "018f0000-0000-7000-8000-000000000073"
	credentialAccount, accountErr := v.credentialAccount(testScope())
	if accountErr != nil {
		t.Fatal(accountErr)
	}
	marker, err := store.Read(v.prefix() + "installation-generation")
	if err != nil {
		t.Fatal(err)
	}
	generationAccounts := v.installationGenerationAccounts(string(marker))
	clear(marker)
	owned := append(generationAccounts, v.prefix()+"installation-generation", credentialAccount, v.pendingAccount(operation), v.pendingAccount(replace), v.pendingAccount(remove), v.reservationAccount(credentialAccount))
	for _, account := range owned {
		account := account
		t.Cleanup(func() { _ = store.Delete(account) })
	}
	if _, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: operation, Scope: testScope(), SelectedPath: credentialPath, Password: []byte("synthetic-password")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(operation); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Unlock(testScope(), []byte("synthetic-password")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.StageReplace(Mutation{Kind: ReplaceCredentialMutation, OperationID: replace, Scope: testScope(), SelectedPath: credentialPath, Password: []byte("synthetic-password")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(replace); err != nil {
		t.Fatal(err)
	}
	if err := v.StageDelete(Mutation{Kind: RemoveCredentialMutation, OperationID: remove, Scope: testScope()}); err != nil {
		t.Fatal(err)
	}
	pendingDelete, err := v.readPending(remove)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompareAndDelete(pendingDelete.Account, pendingDelete.ExpectedDigest); err != nil {
		t.Fatal(err)
	}
	clear(pendingDelete.Envelope)
	reopened, err := newVault(vaultConfig{Store: store, Channel: developmentChannel(), Component: &syntheticComponent{}}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if err := reopened.Promote(remove); err != nil {
		t.Fatalf("native delete crash reconciliation = %v", err)
	}
	if _, err := v.ReadMetadata(testScope()); !errors.Is(err, ErrVaultMissing) {
		t.Fatalf("credential remains after delete: %v", err)
	}
	if wipeEvents < 3 {
		t.Fatalf("native Keychain lifecycle reported only %d helper-owned buffer wipes", wipeEvents)
	}
}
