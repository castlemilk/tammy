package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

func TestAttemptJournalCooldownAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts.journal")
	now := time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)
	current := now
	anchors := newMemoryAnchorStore()
	journal, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x31}, 32), clock.Func(func() time.Time { return current }), "workspace/opaque-workspace", anchors)
	if err != nil {
		t.Fatal(err)
	}
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	for attempt := 1; attempt <= 5; attempt++ {
		current = now.Add(time.Duration(attempt-1) * time.Minute)
		decision, err := journal.Failure("workspace_unlock", "opaque-workspace", policy)
		if err != nil {
			t.Fatal(err)
		}
		if decision.AttemptCount != attempt {
			t.Fatalf("attempt count %d, want %d", decision.AttemptCount, attempt)
		}
		if attempt < 5 && !decision.CooldownUntil.IsZero() {
			t.Fatalf("cooldown started early at attempt %d", attempt)
		}
	}
	decision, err := journal.Status("workspace_unlock", "opaque-workspace", policy)
	if err != nil || !decision.CooldownUntil.Equal(now.Add(19*time.Minute)) {
		t.Fatalf("cooldown = %v, err = %v", decision.CooldownUntil, err)
	}

	restarted, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x31}, 32), clock.Func(func() time.Time { return current }), "workspace/opaque-workspace", anchors)
	if err != nil {
		t.Fatal(err)
	}
	decision, err = restarted.Status("workspace_unlock", "opaque-workspace", policy)
	if err != nil || decision.AttemptCount != 5 {
		t.Fatalf("restart status = %+v, err = %v", decision, err)
	}
	current = now.Add(19 * time.Minute)
	decision, err = restarted.Status("workspace_unlock", "opaque-workspace", policy)
	if err != nil || decision.AttemptCount != 0 || !decision.CooldownUntil.IsZero() {
		t.Fatalf("expired status = %+v, err = %v", decision, err)
	}
	if err := restarted.Success("workspace_unlock", "opaque-workspace"); err != nil {
		t.Fatal(err)
	}
}

type overlappingCASAnchorStore struct {
	*MemoryAnchorStore
	mu       sync.Mutex
	arrived  int
	release  chan struct{}
	released bool
}

func (store *overlappingCASAnchorStore) Save(label string, value []byte, lease attemptJournalLease) error {
	anchor, err := decodeAttemptJournalAnchor(value)
	if err != nil {
		return err
	}
	if anchor.Sequence == 1 {
		store.mu.Lock()
		store.arrived++
		if store.arrived == 2 && !store.released {
			close(store.release)
			store.released = true
		}
		release := store.release
		store.mu.Unlock()
		select {
		case <-release:
		case <-time.After(100 * time.Millisecond):
		}
	}
	return store.MemoryAnchorStore.Save(label, value, lease)
}

func TestAttemptJournalSerializesConcurrentInstancesAcrossFileAndAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts.journal")
	key := bytes.Repeat([]byte{0x61}, 32)
	source := clock.NewFixed(time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC))
	anchors := &overlappingCASAnchorStore{MemoryAnchorStore: NewMemoryAnchorStore(), release: make(chan struct{})}
	first, err := NewAttemptJournal(path, key, source, "workspace/concurrent", anchors)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewAttemptJournal(path, key, source, "workspace/concurrent", anchors)
	if err != nil {
		t.Fatal(err)
	}
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	errorsFound := make(chan error, 2)
	for _, journal := range []*AttemptJournal{first, second} {
		go func(journal *AttemptJournal) {
			_, err := journal.Failure("workspace_unlock", "subject", policy)
			errorsFound <- err
		}(journal)
	}
	firstErr, secondErr := <-errorsFound, <-errorsFound
	if firstErr != nil || secondErr != nil {
		t.Fatalf("concurrent journal append errors=%v/%v", firstErr, secondErr)
	}
	first.Close()
	second.Close()
	restarted, err := NewAttemptJournal(path, key, source, "workspace/concurrent", anchors)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := restarted.Status("workspace_unlock", "subject", policy)
	if err != nil || decision.AttemptCount != 2 {
		t.Fatalf("restarted concurrent count=%d err=%v", decision.AttemptCount, err)
	}
}

func TestAttemptJournalSameAnchorAcrossDifferentPathsCannotFork(t *testing.T) {
	directory := t.TempDir()
	paths := []string{filepath.Join(directory, "first.journal"), filepath.Join(directory, "moved-copy.journal")}
	key := bytes.Repeat([]byte{0x62}, 32)
	source := clock.NewFixed(time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC))
	anchors := &overlappingCASAnchorStore{MemoryAnchorStore: NewMemoryAnchorStore(), release: make(chan struct{})}
	journals := make([]*AttemptJournal, 2)
	for index, path := range paths {
		journal, err := NewAttemptJournal(path, key, source, "workspace/stable-anchor", anchors)
		if err != nil {
			t.Fatal(err)
		}
		journals[index] = journal
	}
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	type result struct {
		index int
		err   error
	}
	results := make(chan result, 2)
	for index, journal := range journals {
		go func(index int, journal *AttemptJournal) {
			_, err := journal.Failure("workspace_unlock", "subject", policy)
			results <- result{index: index, err: err}
		}(index, journal)
	}
	left, right := <-results, <-results
	winner, loser := left, right
	if winner.err != nil {
		winner, loser = loser, winner
	}
	if winner.err != nil || !errors.Is(loser.err, ErrAttemptJournalAuthentication) {
		t.Fatalf("different-path results left=%v right=%v", left.err, right.err)
	}
	if _, err := os.Lstat(paths[loser.index]); !os.IsNotExist(err) {
		t.Fatalf("losing path contains an unanchored fork: %v", err)
	}
	for _, journal := range journals {
		journal.Close()
	}
	restarted, err := NewAttemptJournal(paths[winner.index], key, source, "workspace/stable-anchor", anchors)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := restarted.Status("workspace_unlock", "subject", policy)
	if err != nil || decision.AttemptCount != 1 {
		t.Fatalf("winning path count=%d err=%v", decision.AttemptCount, err)
	}
}

func TestAttemptJournalLockRetryAcquiresAndTimesOut(t *testing.T) {
	t.Run("acquires after contention", func(t *testing.T) {
		now := time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC)
		attempts := 0
		err := retryAttemptJournalLock(func() (bool, error) {
			attempts++
			return attempts == 3, nil
		}, 50*time.Millisecond, 5*time.Millisecond, func() time.Time { return now }, func(wait time.Duration) { now = now.Add(wait) })
		if err != nil || attempts != 3 {
			t.Fatalf("retry acquisition attempts=%d err=%v", attempts, err)
		}
	})

	t.Run("times out fail closed", func(t *testing.T) {
		now := time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC)
		attempts := 0
		err := retryAttemptJournalLock(func() (bool, error) {
			attempts++
			return false, nil
		}, 10*time.Millisecond, 5*time.Millisecond, func() time.Time { return now }, func(wait time.Duration) { now = now.Add(wait) })
		if !errors.Is(err, ErrAttemptJournalAuthentication) || attempts != 3 {
			t.Fatalf("lock timeout attempts=%d err=%v", attempts, err)
		}
	})
}

func TestAttemptJournalPlatformLockRecoversAfterAbandonedOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts.journal.lock")
	first, err := acquireAttemptJournalFileLock(path, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireAttemptJournalFileLock(path, 20*time.Millisecond); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("contended lock returned %v", err)
	}
	// Closing the OS handle models process abandonment: flock/LockFileEx must
	// release ownership without requiring an application-level unlock call.
	if err := first.file.Close(); err != nil {
		t.Fatal(err)
	}
	first.released = true
	recovered, err := acquireAttemptJournalFileLock(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("lock after abandoned owner: %v", err)
	}
	if err := recovered.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptJournalTamperingFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts.journal")
	key := bytes.Repeat([]byte{0x23}, 32)
	anchors := newMemoryAnchorStore()
	journal, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "identity/user", anchors)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Failure("totp_assert", "user", AttemptPolicy{Limit: 5, Window: 5 * time.Minute, Cooldown: 15 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)/2] ^= 1
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "identity/user", anchors); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("got %v, want authenticated-journal failure", err)
	}
}

func TestAttemptJournalRejectsPrefixTruncationAndOldWholeFileReplay(t *testing.T) {
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	for _, test := range []struct {
		name    string
		replace func(t *testing.T, path string, old []byte)
	}{
		{
			name: "prefix truncation",
			replace: func(t *testing.T, path string, old []byte) {
				t.Helper()
				if err := os.Truncate(path, int64(len(old))); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "old valid whole file",
			replace: func(t *testing.T, path string, old []byte) {
				t.Helper()
				if err := os.WriteFile(path, old, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attempts.journal")
			key := bytes.Repeat([]byte{0x41}, 32)
			anchors := newMemoryAnchorStore()
			journal, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := journal.Failure("workspace_unlock", "subject", policy); err != nil {
				t.Fatal(err)
			}
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := journal.Failure("workspace_unlock", "subject", policy); err != nil {
				t.Fatal(err)
			}
			test.replace(t, path, old)
			if _, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors); !errors.Is(err, ErrAttemptJournalAuthentication) {
				t.Fatalf("got %v, want rollback rejection", err)
			}
		})
	}
}

func TestAttemptJournalRejectsMissingAndMismatchedAnchor(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*memoryAnchorStore)
	}{
		{name: "missing", mutate: func(store *memoryAnchorStore) { store.removeValue() }},
		{name: "mismatched", mutate: func(store *memoryAnchorStore) { store.mutateValue() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attempts.journal")
			key := bytes.Repeat([]byte{0x42}, 32)
			anchors := newMemoryAnchorStore()
			journal, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := journal.Failure("workspace_unlock", "subject", AttemptPolicy{Limit: 5, Window: time.Minute, Cooldown: 15 * time.Minute}); err != nil {
				t.Fatal(err)
			}
			test.mutate(anchors)
			if _, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors); !errors.Is(err, ErrAttemptJournalAuthentication) {
				t.Fatalf("got %v, want anchor rejection", err)
			}
		})
	}
}

func TestAttemptJournalRecoversTornTailAtAnchoredBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts.journal")
	key := bytes.Repeat([]byte{0x43}, 32)
	anchors := newMemoryAnchorStore()
	journal, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors)
	if err != nil {
		t.Fatal(err)
	}
	policy := AttemptPolicy{Limit: 5, Window: time.Minute, Cooldown: 15 * time.Minute}
	if _, err := journal.Failure("workspace_unlock", "subject", policy); err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"sequence":2,"scope":"torn`); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := restarted.Status("workspace_unlock", "subject", policy)
	if err != nil || decision.AttemptCount != 1 {
		t.Fatalf("status = %+v, err = %v", decision, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, committed) {
		t.Fatalf("torn tail was not truncated to anchored boundary")
	}
}

func TestAttemptJournalRecoversOneDurableEntryAheadOfAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts.journal")
	key := bytes.Repeat([]byte{0x44}, 32)
	anchors := newMemoryAnchorStore()
	journal, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors)
	if err != nil {
		t.Fatal(err)
	}
	policy := AttemptPolicy{Limit: 5, Window: time.Minute, Cooldown: 15 * time.Minute}
	if _, err := journal.Failure("workspace_unlock", "subject", policy); err != nil {
		t.Fatal(err)
	}
	anchors.failNextSave(errors.New("simulated anchor update crash"))
	if _, err := journal.Failure("workspace_unlock", "subject", policy); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("got %v, want fail-closed anchor update error", err)
	}
	if _, err := journal.Status("workspace_unlock", "subject", policy); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("poisoned journal status returned %v", err)
	}

	restarted, err := NewAttemptJournal(path, key, clock.NewFixed(time.Now()), "workspace/stable-id", anchors)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := restarted.Status("workspace_unlock", "subject", policy)
	if err != nil || decision.AttemptCount != 2 {
		t.Fatalf("recovered status = %+v, err = %v", decision, err)
	}
}

func TestAttemptJournalPersistsEntryBeforeAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts.journal")
	anchors := newMemoryAnchorStore()
	anchors.beforeSave = func(value []byte) error {
		anchor, err := decodeAttemptJournalAnchor(value)
		if err != nil || anchor.Sequence == 0 {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(content), fmt.Sprintf(`"sequence":%d`, anchor.Sequence)) {
			return errors.New("anchor stored before journal entry")
		}
		return nil
	}
	journal, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x45}, 32), clock.NewFixed(time.Now()), "workspace/stable-id", anchors)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Failure("workspace_unlock", "subject", AttemptPolicy{Limit: 5, Window: time.Minute, Cooldown: 15 * time.Minute}); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptJournalAnchorStoreErrorsFailClosed(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		anchors := newMemoryAnchorStore()
		anchors.loadErr = errors.New("load failed")
		if _, err := NewAttemptJournal(filepath.Join(t.TempDir(), "attempts.journal"), bytes.Repeat([]byte{0x46}, 32), clock.NewFixed(time.Now()), "workspace/stable-id", anchors); !errors.Is(err, ErrAttemptJournalAuthentication) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("initial save", func(t *testing.T) {
		anchors := newMemoryAnchorStore()
		anchors.failNextSave(errors.New("save failed"))
		if _, err := NewAttemptJournal(filepath.Join(t.TempDir(), "attempts.journal"), bytes.Repeat([]byte{0x47}, 32), clock.NewFixed(time.Now()), "workspace/stable-id", anchors); !errors.Is(err, ErrAttemptJournalAuthentication) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestAttemptJournalRejectsSymlinkAndOversizedFile(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "attempts.journal")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		anchors := newMemoryAnchorStore()
		anchors.markInitializedWithoutValue()
		if _, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x48}, 32), clock.NewFixed(time.Now()), "workspace/stable-id", anchors); !errors.Is(err, ErrAttemptJournalAuthentication) {
			t.Fatalf("got %v, want symlink rejection", err)
		}
	})
	t.Run("oversized", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "attempts.journal")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(maxAttemptJournalFileSize + 1); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x49}, 32), clock.NewFixed(time.Now()), "workspace/stable-id", newMemoryAnchorStore()); !errors.Is(err, ErrAttemptJournalAuthentication) {
			t.Fatalf("got %v, want oversized rejection", err)
		}
	})
	t.Run("group readable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "attempts.journal")
		if err := os.WriteFile(path, nil, 0o640); err != nil {
			t.Fatal(err)
		}
		anchors := newMemoryAnchorStore()
		anchors.markInitializedWithoutValue()
		if _, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x50}, 32), clock.NewFixed(time.Now()), "workspace/stable-id", anchors); !errors.Is(err, ErrAttemptJournalAuthentication) {
			t.Fatalf("got %v, want permission rejection", err)
		}
	})
}

func TestMemoryAnchorStoreCopiesValuesAndNeverReinitializes(t *testing.T) {
	store := NewMemoryAnchorStore()
	lease, err := (processAttemptJournalLockProvider{}).AcquireAttemptJournalLock("journal", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	journal := &AttemptJournal{key: bytes.Repeat([]byte{0x51}, 32), anchorID: "memory/store"}
	value := journal.encodeAnchor(attemptJournalAnchor{})
	want := append([]byte(nil), value...)
	if err := store.Initialize("journal", value, lease); err != nil {
		t.Fatal(err)
	}
	value[0] ^= 1
	loaded, initialized, err := store.Load("journal", lease)
	if err != nil || !initialized || !bytes.Equal(loaded, want) {
		t.Fatalf("loaded %q, initialized=%v, err=%v", loaded, initialized, err)
	}
	loaded[0] ^= 1
	reloaded, initialized, err := store.Load("journal", lease)
	if err != nil || !initialized || !bytes.Equal(reloaded, want) {
		t.Fatalf("reloaded %q, initialized=%v, err=%v", reloaded, initialized, err)
	}
	if err := store.Initialize("journal", want, lease); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("reinitialization returned %v", err)
	}
	if err := store.Save("missing", want, lease); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("save of uninitialized label returned %v", err)
	}
	next := attemptJournalAnchor{Sequence: 1}
	next.TerminalMAC[0] = 1
	if err := store.Save("journal", journal.encodeAnchor(next), lease); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("journal", want, lease); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("anchor rollback returned %v", err)
	}
	store.Close()
}

func TestAnchorStoreMutationRequiresLiveLeaseAndOneConcurrentSuccessor(t *testing.T) {
	store := NewMemoryAnchorStore()
	provider := processAttemptJournalLockProvider{}
	label := "direct-anchor-contract"
	journal := &AttemptJournal{key: bytes.Repeat([]byte{0x63}, 32), anchorID: "direct/anchor"}
	genesis := journal.encodeAnchor(attemptJournalAnchor{})
	lease, err := provider.AcquireAttemptJournalLock(label, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(label, genesis, lease); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	next := attemptJournalAnchor{Sequence: 1}
	next.TerminalMAC[0] = 1
	payload := journal.encodeAnchor(next)
	if err := store.Save(label, payload, nil); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("save without lease returned %v", err)
	}
	errorsFound := make(chan error, 2)
	for range 2 {
		go func() {
			lease, err := provider.AcquireAttemptJournalLock(label, time.Second)
			if err == nil {
				err = store.Save(label, payload, lease)
				releaseErr := lease.Release()
				if err == nil {
					err = releaseErr
				}
			}
			errorsFound <- err
		}()
	}
	left, right := <-errorsFound, <-errorsFound
	if (left == nil) == (right == nil) || left != nil && !errors.Is(left, ErrAttemptJournalAuthentication) ||
		right != nil && !errors.Is(right, ErrAttemptJournalAuthentication) {
		t.Fatalf("direct concurrent saves returned %v/%v", left, right)
	}
	if err := store.Save(label, payload, lease); !errors.Is(err, ErrAttemptJournalAuthentication) {
		t.Fatalf("save with released lease returned %v", err)
	}
}

type memoryAnchorStore struct {
	mu          sync.Mutex
	initialized bool
	value       []byte
	loadErr     error
	nextSaveErr error
	beforeSave  func([]byte) error
}

func newMemoryAnchorStore() *memoryAnchorStore { return &memoryAnchorStore{} }

func (*memoryAnchorStore) allowProcessLocalAttemptJournalLock() {}

func (store *memoryAnchorStore) Load(label string, lease attemptJournalLease) ([]byte, bool, error) {
	if !validAttemptJournalLease(lease, label) {
		return nil, false, ErrAttemptJournalAuthentication
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loadErr != nil {
		return nil, false, store.loadErr
	}
	return append([]byte(nil), store.value...), store.initialized, nil
}

func (store *memoryAnchorStore) Save(label string, value []byte, lease attemptJournalLease) error {
	if !validAttemptJournalLease(lease, label) {
		return ErrAttemptJournalAuthentication
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.initialized || len(store.value) == 0 {
		return ErrAttemptJournalAuthentication
	}
	return store.saveLocked(value)
}

func (store *memoryAnchorStore) Initialize(label string, value []byte, lease attemptJournalLease) error {
	if !validAttemptJournalLease(lease, label) {
		return ErrAttemptJournalAuthentication
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.initialized {
		return ErrAttemptJournalAuthentication
	}
	return store.saveLocked(value)
}

func (store *memoryAnchorStore) saveLocked(value []byte) error {
	if store.beforeSave != nil {
		if err := store.beforeSave(value); err != nil {
			return err
		}
	}
	if store.nextSaveErr != nil {
		err := store.nextSaveErr
		store.nextSaveErr = nil
		return err
	}
	store.initialized = true
	Zero(store.value)
	store.value = append(store.value[:0], value...)
	return nil
}

func (store *memoryAnchorStore) failNextSave(err error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.nextSaveErr = err
}

func (store *memoryAnchorStore) removeValue() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.value = nil
}

func (store *memoryAnchorStore) mutateValue() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.value[len(store.value)-1] ^= 1
}

func (store *memoryAnchorStore) markInitializedWithoutValue() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.initialized = true
}
