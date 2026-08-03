//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

func TestFileRepositoryRejectsInsecureOrOversizedCatalogue(t *testing.T) {
	key := bytes.Repeat([]byte{0x51}, 32)
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, path string) {
				t.Helper()
				target := path + ".target"
				if err := os.WriteFile(target, []byte("not a catalogue"), 0o600); err != nil {
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
				if err := os.WriteFile(path, []byte("not a catalogue"), 0o640); err != nil {
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
				if err := file.Truncate(maxWorkspaceCatalogueFileSize + 1); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
			test.setup(t, path)
			repository, err := NewFileRepository(path, key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repository.loadLocked(); !errors.Is(err, ErrWorkspaceNotFound) {
				t.Fatalf("got %v, want catalogue rejection", err)
			}
		})
	}
}

func TestSQLAnchorStorePersistsJournalAnchorAcrossRestart(t *testing.T) {
	ctx := context.Background()
	storage, err := NewSQLCipherStorageFactory(2).Create(ctx, filepath.Join(t.TempDir(), "identity-anchor.db"), bytes.Repeat([]byte{0x52}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	anchors, err := NewSQLAnchorStore(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "identity-attempts.journal")
	journal, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x53}, 32), clock.NewFixed(time.Now()), "identity/user", anchors)
	if err != nil {
		t.Fatal(err)
	}
	policy := AttemptPolicy{Limit: 5, Window: 5 * time.Minute, Cooldown: 15 * time.Minute}
	if _, err := journal.Failure("totp_assert", "user", policy); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewAttemptJournal(path, bytes.Repeat([]byte{0x53}, 32), clock.NewFixed(time.Now()), "identity/user", anchors)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := restarted.Status("totp_assert", "user", policy)
	if err != nil || decision.AttemptCount != 1 {
		t.Fatalf("restart status = %+v, err = %v", decision, err)
	}
}

type subprocessBlockingSQLAnchorStore struct {
	*SQLAnchorStore
	allowPath string
	loads     int
	readyPath string
}

func (store *subprocessBlockingSQLAnchorStore) Load(label string, lease attemptJournalLease) ([]byte, bool, error) {
	value, initialized, err := store.SQLAnchorStore.Load(label, lease)
	store.loads++
	if err != nil || store.loads != 2 {
		return value, initialized, err
	}
	if err := os.WriteFile(store.readyPath, []byte("ready"), 0o600); err != nil {
		return nil, false, err
	}
	if err := waitForSubprocessFile(store.allowPath, 5*time.Second); err != nil {
		return nil, false, err
	}
	return value, initialized, nil
}

func TestSQLAnchorStoreSubprocessHelper(t *testing.T) {
	mode := os.Getenv("TAMMY_SQL_ANCHOR_HELPER")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	databasePath := os.Getenv("TAMMY_SQL_ANCHOR_DATABASE")
	journalPath := os.Getenv("TAMMY_SQL_ANCHOR_JOURNAL")
	anchorID := os.Getenv("TAMMY_SQL_ANCHOR_ID")
	key := bytes.Repeat([]byte{0x72}, 32)
	storage, err := NewSQLCipherStorageFactory(2).Open(context.Background(), databasePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	anchors, err := NewSQLAnchorStore(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	if mode == "crash" {
		provider, ok := any(anchors).(attemptJournalLockProvider)
		if !ok {
			t.Fatal("SQL anchor store has no interprocess lease provider")
		}
		journal := &AttemptJournal{key: bytes.Repeat([]byte{0x73}, 32), anchorID: anchorID}
		label := journal.deriveAnchorLabel()
		lease, err := provider.AcquireAttemptJournalLock(label, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Release()
		file, err := os.OpenFile(journalPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("{\"torn")); err != nil {
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("TAMMY_SQL_ANCHOR_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	blocking := &subprocessBlockingSQLAnchorStore{SQLAnchorStore: anchors,
		allowPath: os.Getenv("TAMMY_SQL_ANCHOR_ALLOW"), readyPath: os.Getenv("TAMMY_SQL_ANCHOR_READY")}
	journal, err := NewAttemptJournal(journalPath, bytes.Repeat([]byte{0x73}, 32),
		clock.NewFixed(time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)), anchorID, blocking)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	policy := AttemptPolicy{Limit: 5, Window: 5 * time.Minute, Cooldown: 15 * time.Minute}
	if _, err := journal.Failure("totp_assert", "user", policy); err != nil {
		t.Fatal(err)
	}
}

func TestSQLAnchorStoreSerializesSubprocessAttempts(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "identity-anchor.db")
	journalPath := filepath.Join(directory, "identity-attempts.journal")
	anchorID := "identity/subprocess/" + filepath.Base(directory)
	key := bytes.Repeat([]byte{0x72}, 32)
	storage, err := NewSQLCipherStorageFactory(2).Create(context.Background(), databasePath, key)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := NewSQLAnchorStore(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewAttemptJournal(journalPath, bytes.Repeat([]byte{0x73}, 32), clock.NewFixed(time.Now()), anchorID, anchors)
	if err != nil {
		t.Fatal(err)
	}
	journal.Close()
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	type child struct {
		command   *exec.Cmd
		allowPath string
		output    *bytes.Buffer
		readyPath string
	}
	children := make([]child, 2)
	for index := range children {
		children[index].readyPath = filepath.Join(directory, "ready-"+string(rune('0'+index)))
		children[index].allowPath = filepath.Join(directory, "allow-"+string(rune('0'+index)))
		command := exec.Command(os.Args[0], "-test.run=^TestSQLAnchorStoreSubprocessHelper$")
		children[index].output = new(bytes.Buffer)
		command.Stdout = children[index].output
		command.Stderr = children[index].output
		command.Env = append(os.Environ(),
			"TAMMY_SQL_ANCHOR_HELPER=attempt",
			"TAMMY_SQL_ANCHOR_DATABASE="+databasePath,
			"TAMMY_SQL_ANCHOR_JOURNAL="+journalPath,
			"TAMMY_SQL_ANCHOR_ID="+anchorID,
			"TAMMY_SQL_ANCHOR_READY="+children[index].readyPath,
			"TAMMY_SQL_ANCHOR_ALLOW="+children[index].allowPath,
		)
		children[index].command = command
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	first := waitForFirstSubprocessFile(t, []string{children[0].readyPath, children[1].readyPath}, 5*time.Second)
	other := 1 - first
	if err := waitForSubprocessFile(children[other].readyPath, 150*time.Millisecond); err == nil {
		t.Fatal("both subprocesses crossed SQL anchor reload concurrently")
	}
	if err := os.WriteFile(children[first].allowPath, []byte("allow"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := children[first].command.Wait(); err != nil {
		t.Fatalf("first subprocess: %v output=%s", err, children[first].output.String())
	}
	if err := waitForSubprocessFile(children[other].readyPath, 5*time.Second); err != nil {
		_ = children[other].command.Process.Kill()
		waitErr := children[other].command.Wait()
		t.Fatalf("%v: child=%v output=%s", err, waitErr, children[other].output.String())
	}
	if err := os.WriteFile(children[other].allowPath, []byte("allow"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := children[other].command.Wait(); err != nil {
		t.Fatalf("second subprocess: %v", err)
	}

	storage, err = NewSQLCipherStorageFactory(2).Open(context.Background(), databasePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	anchors, err = NewSQLAnchorStore(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewAttemptJournal(journalPath, bytes.Repeat([]byte{0x73}, 32),
		clock.NewFixed(time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)), anchorID, anchors)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := restarted.Status("totp_assert", "user", AttemptPolicy{Limit: 5, Window: 5 * time.Minute, Cooldown: 15 * time.Minute})
	if err != nil || decision.AttemptCount != 2 {
		t.Fatalf("serialized subprocess count=%d err=%v", decision.AttemptCount, err)
	}
}

func TestSQLAnchorStoreRecoversAbandonedSubprocessLeaseAndTornAppend(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "identity-anchor.db")
	journalPath := filepath.Join(directory, "identity-attempts.journal")
	readyPath := filepath.Join(directory, "crash-ready")
	anchorID := "identity/crash/" + filepath.Base(directory)
	key := bytes.Repeat([]byte{0x72}, 32)
	storage, err := NewSQLCipherStorageFactory(2).Create(context.Background(), databasePath, key)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := NewSQLAnchorStore(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewAttemptJournal(journalPath, bytes.Repeat([]byte{0x73}, 32), clock.NewFixed(time.Now()), anchorID, anchors)
	if err != nil {
		t.Fatal(err)
	}
	journal.Close()
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestSQLAnchorStoreSubprocessHelper$")
	command.Env = append(os.Environ(),
		"TAMMY_SQL_ANCHOR_HELPER=crash",
		"TAMMY_SQL_ANCHOR_DATABASE="+databasePath,
		"TAMMY_SQL_ANCHOR_JOURNAL="+journalPath,
		"TAMMY_SQL_ANCHOR_ID="+anchorID,
		"TAMMY_SQL_ANCHOR_READY="+readyPath,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if err := waitForSubprocessFile(readyPath, 5*time.Second); err != nil {
		_ = command.Process.Kill()
		t.Fatal(err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	storage, err = NewSQLCipherStorageFactory(2).Open(context.Background(), databasePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	anchors, err = NewSQLAnchorStore(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewAttemptJournal(journalPath, bytes.Repeat([]byte{0x73}, 32), clock.NewFixed(time.Now()), anchorID, anchors)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("torn append not truncated: size=%d", info.Size())
	}
	decision, err := restarted.Failure("totp_assert", "user", AttemptPolicy{Limit: 5, Window: 5 * time.Minute, Cooldown: 15 * time.Minute})
	if err != nil || decision.AttemptCount != 1 {
		t.Fatalf("post-crash attempt count=%d err=%v", decision.AttemptCount, err)
	}
}

func waitForFirstSubprocessFile(t *testing.T, paths []string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for index, path := range paths {
			if _, err := os.Stat(path); err == nil {
				return index
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("subprocess did not reach anchor reload")
	return -1
}

func waitForSubprocessFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errors.New("subprocess synchronization timeout")
}
