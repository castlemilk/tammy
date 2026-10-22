package workspace

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

func TestRememberedWorkspaceRequiresConsentAndExpires(t *testing.T) {
	start := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	now := start
	store := NewMemorySecretStore()
	manager, err := NewRememberedKeyManager(store, clock.Func(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	dek := bytes.Repeat([]byte{0x5a}, 32)
	no := false
	if _, err := manager.Remember("workspace", dek, &no); !errors.Is(err, ErrRememberConsentRequired) {
		t.Fatalf("implicit/declined consent returned %v", err)
	}
	yes := true
	expires, err := manager.Remember("workspace", dek, &yes)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(start.Add(23*time.Hour + 59*time.Minute)) {
		t.Fatalf("expiry = %v", expires)
	}
	remembered, rememberedUntil, err := manager.Use("workspace")
	if err != nil || !bytes.Equal(remembered, dek) || !rememberedUntil.Equal(expires) {
		t.Fatalf("use remembered key: until=%v err=%v", rememberedUntil, err)
	}
	Zero(remembered)
	now = expires
	if _, _, err := manager.Use("workspace"); !errors.Is(err, ErrRememberedKeyUnavailable) {
		t.Fatalf("expired item returned %v", err)
	}
	if store.Count() != 0 {
		t.Fatal("expired item was not removed")
	}
}

func TestRememberedWorkspaceForget(t *testing.T) {
	store := NewMemorySecretStore()
	manager, err := NewRememberedKeyManager(store, clock.NewFixed(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := manager.Remember("workspace", bytes.Repeat([]byte{1}, 32), &yes); err != nil {
		t.Fatal(err)
	}
	if err := manager.Forget("workspace"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Use("workspace"); !errors.Is(err, ErrRememberedKeyUnavailable) {
		t.Fatalf("forgotten key returned %v", err)
	}
}

type deleteFailingSecretStore struct{ SecretStore }

func (deleteFailingSecretStore) Delete(string) error { return ErrRememberedKeyUnavailable }

type cleanupFailingSecretStore struct {
	SecretStore
	err error
}

func (store cleanupFailingSecretStore) Delete(string) error { return store.err }

func TestRememberedWorkspaceForgetFailsClosedOnVaultError(t *testing.T) {
	store := NewMemorySecretStore()
	manager, err := NewRememberedKeyManager(deleteFailingSecretStore{SecretStore: store}, clock.NewFixed(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := manager.Remember("workspace", bytes.Repeat([]byte{1}, 32), &yes); err != nil {
		t.Fatal(err)
	}
	if err := manager.Forget("workspace"); !errors.Is(err, ErrRememberedKeyUnavailable) {
		t.Fatalf("vault deletion failure returned %v", err)
	}
	if store.Count() != 1 {
		t.Fatal("failed deletion removed the remembered DEK")
	}
}

func TestRememberedWorkspaceExpiredCleanupFailureIsRetryable(t *testing.T) {
	start := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	now := start
	store := NewMemorySecretStore()
	cleanupErr := errors.New("credential vault unavailable")
	manager, err := NewRememberedKeyManager(cleanupFailingSecretStore{SecretStore: store, err: cleanupErr}, clock.Func(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := manager.Remember("workspace", bytes.Repeat([]byte{2}, 32), &yes); err != nil {
		t.Fatal(err)
	}
	now = start.Add(rememberedLifetime)
	if _, _, err := manager.Use("workspace"); !errors.Is(err, cleanupErr) {
		t.Fatalf("expired cleanup returned %v", err)
	}
	if store.Count() != 1 {
		t.Fatal("failed expiry cleanup discarded retry metadata")
	}
	manager.store = store
	if _, _, err := manager.Use("workspace"); !errors.Is(err, ErrRememberedKeyUnavailable) {
		t.Fatalf("expiry cleanup retry returned %v", err)
	}
	if store.Count() != 0 {
		t.Fatal("successful expiry cleanup retry retained the item")
	}
}

func TestRememberedWorkspaceMalformedCleanupFailureIsRetryable(t *testing.T) {
	store := NewMemorySecretStore()
	if err := store.Put(rememberedLabel("workspace"), []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	cleanupErr := errors.New("credential vault unavailable")
	manager, err := NewRememberedKeyManager(cleanupFailingSecretStore{SecretStore: store, err: cleanupErr}, clock.NewFixed(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Use("workspace"); !errors.Is(err, cleanupErr) {
		t.Fatalf("malformed cleanup returned %v", err)
	}
	if store.Count() != 1 {
		t.Fatal("failed malformed cleanup discarded retry metadata")
	}
	manager.store = store
	if _, _, err := manager.Use("workspace"); !errors.Is(err, ErrRememberedKeyUnavailable) {
		t.Fatalf("malformed cleanup retry returned %v", err)
	}
	if store.Count() != 0 {
		t.Fatal("successful malformed cleanup retry retained the item")
	}
}
