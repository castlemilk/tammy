//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceMutationWaitsForWriterBeforeDependentValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	storage, err := NewSQLCipherStorageFactory(3).Create(ctx, filepath.Join(t.TempDir(), "writer-admission.db"), bytes.Repeat([]byte{0x43}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	record := workspaceRecord{ID: "01890f60-4d6d-7c12-8f02-6c9129d5b011", Version: 1, DisplayName: "Before writer"}
	mutation := WorkspaceMutation{OperationID: "01890f60-4d6d-7c12-8f02-6c9129d5b012", Kind: "TEST", WorkspaceID: record.ID, Version: record.Version}
	if err := storage.CommitWorkspaceMutation(ctx, mutation, record, nil); err != nil {
		t.Fatal(err)
	}
	writer, err := storage.Database().BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	updated := record
	updated.DisplayName = "Committed by writer"
	payload, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, `UPDATE workspace_metadata SET value=?, revision=revision+1 WHERE key=?`, payload, authoritativeWorkspaceRecordKey); err != nil {
		t.Fatal(err)
	}
	// Pure workspace reads still use the last committed WAL snapshot while the
	// writer is held; mutation admission must not change their behavior.
	before, err := storage.LoadWorkspaceRecord(ctx)
	if err != nil || before.DisplayName != record.DisplayName {
		t.Fatalf("workspace read while writer is held = %q, %v", before.DisplayName, err)
	}

	dependentRead := make(chan string, 1)
	allowDependentWrite := make(chan struct{})
	defer func() {
		select {
		case <-allowDependentWrite:
		default:
			close(allowDependentWrite)
		}
	}()
	result := make(chan error, 1)
	go func() {
		result <- storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, target *workspaceRecord) error {
			// ForgetRememberedWorkspace validates its source before calling the
			// identity dependency. Both must run after acquiring the writer slot.
			current, err := loadWorkspaceRecordFrom(ctx, executor)
			if err != nil {
				return err
			}
			dependentRead <- current.DisplayName
			select {
			case <-allowDependentWrite:
			case <-ctx.Done():
				return ctx.Err()
			}
			target.DisplayName = current.DisplayName + " and mutation"
			return nil
		})
	}()
	// Observe the second checked-out connection before measuring exclusion,
	// so an unscheduled goroutine cannot make the contention check pass.
	for storage.Database().Stats().InUse < 2 {
		select {
		case err := <-result:
			t.Fatalf("mutation completed before writer release: %v", err)
		case <-ctx.Done():
			t.Fatal("mutation did not reach writer admission")
		case <-time.After(time.Millisecond):
		}
	}
	var observed string
	var enteredBeforeRelease bool
	select {
	case observed = <-dependentRead:
		enteredBeforeRelease = true
	case err := <-result:
		t.Fatalf("mutation completed before writer release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	close(allowDependentWrite)
	select {
	case err := <-result:
		if enteredBeforeRelease || err != nil {
			t.Fatalf("dependent entered before writer release=%t, observed=%q, mutation error=%v; want admission before validation and successful commit", enteredBeforeRelease, observed, err)
		}
	case <-ctx.Done():
		t.Fatal("mutation did not finish after writer release")
	}
	if observed = <-dependentRead; observed != updated.DisplayName {
		t.Fatalf("dependent validation read %q, want latest committed value %q", observed, updated.DisplayName)
	}
	after, err := storage.LoadWorkspaceRecord(ctx)
	if err != nil || after.DisplayName != updated.DisplayName+" and mutation" {
		t.Fatalf("committed workspace = %q, %v", after.DisplayName, err)
	}
}
