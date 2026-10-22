package workspace

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

const rememberedLifetime = 23*time.Hour + 59*time.Minute

var (
	ErrRememberConsentRequired  = errors.New("workspace: explicit remember consent is required")
	ErrRememberedKeyUnavailable = errors.New("workspace: remembered key unavailable")
)

// SecretStore is the narrow OS credential-vault boundary. Implementations must
// never expose secret values through labels, errors, logs, or command arguments.
type SecretStore interface {
	Put(label string, secret []byte) error
	Get(label string) ([]byte, error)
	Delete(label string) error
}

type RememberedKeyManager struct {
	store SecretStore
	clock clock.Clock
}

func NewRememberedKeyManager(store SecretStore, source clock.Clock) (*RememberedKeyManager, error) {
	if store == nil || source == nil {
		return nil, ErrRememberedKeyUnavailable
	}
	return &RememberedKeyManager{store: store, clock: source}, nil
}

func NewPlatformRememberedKeyManager(source clock.Clock) (*RememberedKeyManager, error) {
	store, err := newPlatformSecretStore()
	if err != nil {
		return nil, err
	}
	return NewRememberedKeyManager(store, source)
}

func (manager *RememberedKeyManager) Remember(workspaceID string, dek []byte, consent *bool) (time.Time, error) {
	if manager == nil || workspaceID == "" || len(dek) != DEKSize || consent == nil || !*consent {
		return time.Time{}, ErrRememberConsentRequired
	}
	expires := manager.clock.Now().UTC().Add(rememberedLifetime)
	payload := make([]byte, 1+8+DEKSize)
	payload[0] = 1
	binary.BigEndian.PutUint64(payload[1:9], uint64(expires.Unix()))
	copy(payload[9:], dek)
	defer Zero(payload)
	if err := manager.store.Put(rememberedLabel(workspaceID), payload); err != nil {
		return time.Time{}, ErrRememberedKeyUnavailable
	}
	return expires, nil
}

func (manager *RememberedKeyManager) Use(workspaceID string) ([]byte, time.Time, error) {
	if manager == nil || workspaceID == "" {
		return nil, time.Time{}, ErrRememberedKeyUnavailable
	}
	payload, err := manager.store.Get(rememberedLabel(workspaceID))
	if err != nil {
		return nil, time.Time{}, ErrRememberedKeyUnavailable
	}
	defer Zero(payload)
	if len(payload) != 1+8+DEKSize || payload[0] != 1 {
		if err := manager.Forget(workspaceID); err != nil {
			return nil, time.Time{}, err
		}
		return nil, time.Time{}, ErrRememberedKeyUnavailable
	}
	expires := time.Unix(int64(binary.BigEndian.Uint64(payload[1:9])), 0).UTC()
	if !manager.clock.Now().UTC().Before(expires) {
		if err := manager.Forget(workspaceID); err != nil {
			return nil, time.Time{}, err
		}
		return nil, time.Time{}, ErrRememberedKeyUnavailable
	}
	return append([]byte(nil), payload[9:]...), expires, nil
}

func (manager *RememberedKeyManager) Forget(workspaceID string) error {
	if manager == nil || workspaceID == "" {
		return ErrRememberedKeyUnavailable
	}
	if err := manager.store.Delete(rememberedLabel(workspaceID)); err != nil {
		return fmt.Errorf("%w: %w", ErrRememberedKeyUnavailable, err)
	}
	return nil
}

func rememberedLabel(workspaceID string) string {
	return "tammy.remembered-workspace.v1/" + workspaceID
}

// MemorySecretStore is a deterministic test store, not a production fallback.
type MemorySecretStore struct {
	mu    sync.Mutex
	items map[string][]byte
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{items: make(map[string][]byte)}
}

func (store *MemorySecretStore) Put(label string, secret []byte) error {
	if store == nil || label == "" || len(secret) == 0 {
		return ErrRememberedKeyUnavailable
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if previous := store.items[label]; previous != nil {
		Zero(previous)
	}
	store.items[label] = append([]byte(nil), secret...)
	return nil
}

func (store *MemorySecretStore) Get(label string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	secret, ok := store.items[label]
	if !ok {
		return nil, ErrRememberedKeyUnavailable
	}
	return append([]byte(nil), secret...), nil
}

func (store *MemorySecretStore) Delete(label string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if secret, ok := store.items[label]; ok {
		Zero(secret)
		delete(store.items, label)
	}
	return nil
}

func (store *MemorySecretStore) Count() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.items)
}
