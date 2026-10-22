//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type commandAuthorizerStub struct {
	readCalls     int
	mutationCalls int
	wantPassword  string
	now           time.Time
	readErr       error
}

func (authorizer *commandAuthorizerStub) AuthorizePreRestoreRead(context.Context, PreRestoreReadAuthorization) error {
	authorizer.readCalls++
	return authorizer.readErr
}

func (authorizer *commandAuthorizerStub) AuthorizePreRestoreMutation(_ context.Context, authorization PreRestoreMutationAuthorization) error {
	authorizer.mutationCalls++
	if authorizer.wantPassword != "" && string(authorization.Password) != authorizer.wantPassword {
		return ErrPreRestoreAuthorization
	}
	if !authorizer.now.IsZero() && authorizer.now.Sub(authorization.AssertedAt) > 5*time.Minute {
		return ErrPreRestoreAuthorization
	}
	return nil
}

type commandTransactions struct {
	database      *sqlcipher.Database
	mutationDepth *int
	readCalls     *int
	mutationCalls *int
	callbackDepth *int
}

func (transactions commandTransactions) Read(ctx context.Context, callback func(backup.SQLExecutor) error) error {
	if transactions.readCalls != nil {
		*transactions.readCalls++
	}
	if transactions.callbackDepth != nil {
		*transactions.callbackDepth++
		defer func() { *transactions.callbackDepth-- }()
	}
	return callback(transactions.database)
}

func (transactions commandTransactions) Mutate(ctx context.Context, callback func(backup.SQLExecutor) error) error {
	if transactions.mutationCalls != nil {
		*transactions.mutationCalls++
	}
	if transactions.mutationDepth != nil {
		*transactions.mutationDepth++
		defer func() { *transactions.mutationDepth-- }()
	}
	transaction, err := transactions.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	if transactions.callbackDepth != nil {
		*transactions.callbackDepth++
		defer func() { *transactions.callbackDepth-- }()
	}
	if err := callback(transaction); err != nil {
		_ = transaction.Rollback()
		return err
	}
	return transaction.Commit()
}

type commandArchiveStore struct {
	bytesByID                 map[string][]byte
	deleteErr                 error
	deleteCalls               int
	mutationDepth             *int
	deleteObservedTransaction bool
}

func (store *commandArchiveStore) ReadEncryptedPreRestoreArchive(_ context.Context, _ string, archiveID string, expectedHash []byte) ([]byte, error) {
	contents := store.bytesByID[archiveID]
	digest := sha256.Sum256(contents)
	if len(contents) == 0 || !bytes.Equal(digest[:], expectedHash) {
		return nil, ErrPreRestoreArchiveCommand
	}
	return append([]byte(nil), contents...), nil
}

func (store *commandArchiveStore) DeleteEncryptedPreRestoreArchive(_ context.Context, archiveID string, expectedHash []byte) error {
	store.deleteCalls++
	if store.mutationDepth != nil && *store.mutationDepth != 0 {
		store.deleteObservedTransaction = true
	}
	if store.deleteErr != nil {
		return store.deleteErr
	}
	contents := store.bytesByID[archiveID]
	if len(contents) == 0 {
		return nil
	}
	digest := sha256.Sum256(contents)
	if len(contents) == 0 || !bytes.Equal(digest[:], expectedHash) {
		return ErrPreRestoreArchiveCommand
	}
	delete(store.bytesByID, archiveID)
	return nil
}

type commandDestination struct {
	mu                      sync.Mutex
	reference               string
	contents                []byte
	commits                 int
	reads                   int
	callbackDepth           *int
	readObservedTransaction bool
}

func (destination *commandDestination) Reference() string { return destination.reference }

func (destination *commandDestination) AtomicCommit(_ context.Context, contents []byte) error {
	destination.mu.Lock()
	defer destination.mu.Unlock()
	destination.contents = append([]byte(nil), contents...)
	destination.commits++
	return nil
}

func (destination *commandDestination) ReadCommitted(context.Context) ([]byte, error) {
	destination.mu.Lock()
	defer destination.mu.Unlock()
	destination.reads++
	if destination.callbackDepth != nil && *destination.callbackDepth != 0 {
		destination.readObservedTransaction = true
	}
	if len(destination.contents) == 0 {
		return nil, errors.New("missing destination")
	}
	return append([]byte(nil), destination.contents...), nil
}

func TestPreRestoreExportRecoveryIsBoundedAndStable(t *testing.T) {
	fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))
	defer fixture.close()
	ctx := context.Background()
	destinationID := "018f7000-0000-7000-8000-000000000001"
	destinationHash := sha256.Sum256(fixture.encryptedArchive)
	progress, err := marshalPreRestoreProgress("VERIFIED")
	if err != nil {
		t.Fatal(err)
	}
	jobIDs := make([]string, 257)
	baseTransactions := commandTransactions{database: fixture.database}
	if err := baseTransactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		for index := range jobIDs {
			jobIDs[index] = fmt.Sprintf("018f7100-0000-7000-8000-%012x", index+1)
			operationKey := fmt.Sprintf("018f7200-0000-7000-8000-%012x", index+1)
			inputHash := preRestoreExportInputHash(fixture.workspaceID, fixture.archiveID, 1, destinationID)
			instant := formatPreRestoreTime(fixture.now)
			if _, insertErr := executor.ExecContext(ctx, `INSERT INTO pre_restore_archive_export_jobs_v1(
				job_id,workspace_id,archive_id,archive_version,operation_key,version,state,input_hash,destination_capability,
				destination_hash,progress_proto,commit_point_reached,created_at,updated_at)
				VALUES(?,?,?,?,?,3,3,?,?,?,?,1,?,?)`, jobIDs[index], fixture.workspaceID, fixture.archiveID, 1,
				operationKey, inputHash[:], destinationID, destinationHash[:], progress, instant, instant); insertErr != nil {
				return insertErr
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	readCalls, mutationCalls, callbackDepth := 0, 0, 0
	destination := &commandDestination{reference: destinationID, contents: append([]byte(nil), fixture.encryptedArchive...),
		callbackDepth: &callbackDepth}
	fixture.commands.config.Transactions = commandTransactions{database: fixture.database, readCalls: &readCalls,
		mutationCalls: &mutationCalls, callbackDepth: &callbackDepth}
	fixture.commands.config.Destinations = commandDestinationResolver{destinationID: destination}
	fixture.commands.config.Now = func() time.Time { return fixture.now.Add(time.Minute) }
	first, err := fixture.commands.RecoverPreRestoreArchiveExports(ctx)
	if err != nil || len(first) != 256 || first[0].Id != jobIDs[0] || first[255].Id != jobIDs[255] ||
		destination.reads != 256 || destination.readObservedTransaction || readCalls != 257 || mutationCalls != 256 {
		t.Fatalf("first batch=%d range=%v..%v reads=%d read_tx=%t tx=%d/%d error=%v", len(first),
			first[0].Id, first[len(first)-1].Id, destination.reads, destination.readObservedTransaction,
			readCalls, mutationCalls, err)
	}
	remaining, err := loadPreRestoreExportJob(ctx, fixture.database, jobIDs[256])
	if err != nil || remaining.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_VERIFIED {
		t.Fatalf("remaining job=%#v error=%v", remaining, err)
	}
	second, err := fixture.commands.RecoverPreRestoreArchiveExports(ctx)
	if err != nil || len(second) != 1 || second[0].Id != jobIDs[256] || destination.reads != 257 ||
		destination.readObservedTransaction || readCalls != 259 || mutationCalls != 257 {
		t.Fatalf("second batch=%#v reads=%d read_tx=%t tx=%d/%d error=%v", second,
			destination.reads, destination.readObservedTransaction, readCalls, mutationCalls, err)
	}
}

type commandDestinationResolver map[string]*commandDestination

func (resolver commandDestinationResolver) Resolve(reference string) (audit.ExportDestination, error) {
	destination := resolver[reference]
	if destination == nil {
		return nil, errors.New("destination approval missing")
	}
	return destination, nil
}

type commandAuditStub struct {
	calls int
	event *tammyv1.AuditEvent
}

func (auditStub *commandAuditStub) AppendPreRestoreArchiveCommand(_ context.Context, _ backup.SQLExecutor,
	event *tammyv1.AuditEvent,
) error {
	auditStub.calls++
	auditStub.event = proto.Clone(event).(*tammyv1.AuditEvent)
	return nil
}

func TestPreRestoreArchiveCommands(t *testing.T) {
	t.Run("list_after_restart", func(t *testing.T) {
		ctx := context.Background()
		workspaceID := "018f0000-0000-7000-8000-000000000001"
		path := filepath.Join(t.TempDir(), "workspace.db")
		key := bytes.Repeat([]byte{0x31}, sqlcipher.KeySize)
		defer zeroBytes(key)
		if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
			t.Fatal(err)
		}
		database, err := sqlcipher.Open(ctx, path, key)
		if err != nil {
			t.Fatal(err)
		}
		createdAt := time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC)
		if err := PersistPreRestoreArchive(ctx, database, PreRestoreArchiveRecord{WorkspaceID: workspaceID,
			OperationID: "018f0000-0000-7000-8000-000000000091", ArchiveID: "018f0000-0000-7000-8000-000000000081",
			Version: 1, State: tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE,
			CreatedAt: createdAt, DeletionEligibleAt: createdAt.AddDate(1, 0, 0), ContentHash: bytes.Repeat([]byte{1}, 32),
			SourceGeneration: 5, EncryptedByteLength: 4096}); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		database, err = sqlcipher.Open(ctx, path, key)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		codec, err := paging.NewCodec(bytes.Repeat([]byte{0x55}, 32))
		if err != nil {
			t.Fatal(err)
		}
		repository, err := NewPreRestoreArchiveRepository(database, codec)
		if err != nil {
			t.Fatal(err)
		}
		authorizer := &commandAuthorizerStub{}
		commands, err := NewPreRestoreArchiveCommandService(PreRestoreArchiveCommandServiceConfig{
			WorkspaceID: workspaceID, Repository: repository, Authorizer: authorizer,
		})
		if err != nil {
			t.Fatal(err)
		}
		response, err := commands.ListPreRestoreArchives(ctx, &tammyv1.ListPreRestoreArchivesRequest{
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: "018f0000-0000-7000-8000-000000000010",
				SessionId: "018f0000-0000-7000-8000-000000000011"}, Page: &tammyv1.PageRequest{PageSize: 10}})
		if err != nil || response == nil || len(response.Archives) != 1 || response.Archives[0].Id != "018f0000-0000-7000-8000-000000000081" ||
			authorizer.readCalls != 1 {
			t.Fatalf("ListPreRestoreArchives()=%#v calls=%d error=%v", response, authorizer.readCalls, err)
		}
	})

	t.Run("multiple_restore_versions", func(t *testing.T) {
		ctx := context.Background()
		workspaceID := "018f0000-0000-7000-8000-000000000001"
		path := filepath.Join(t.TempDir(), "workspace.db")
		key := bytes.Repeat([]byte{0x32}, sqlcipher.KeySize)
		defer zeroBytes(key)
		if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
			t.Fatal(err)
		}
		database, err := sqlcipher.Open(ctx, path, key)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		createdAt := time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC)
		for index, archiveID := range []string{"018f0000-0000-7000-8000-000000000081", "018f0000-0000-7000-8000-000000000082"} {
			if err := PersistPreRestoreArchive(ctx, database, PreRestoreArchiveRecord{WorkspaceID: workspaceID,
				OperationID: []string{"018f0000-0000-7000-8000-000000000091", "018f0000-0000-7000-8000-000000000092"}[index],
				ArchiveID:   archiveID, Version: 1,
				State:              tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE,
				CreatedAt:          createdAt.Add(time.Duration(index) * time.Hour),
				DeletionEligibleAt: createdAt.Add(time.Duration(index)*time.Hour).AddDate(1, 0, 0),
				ContentHash:        bytes.Repeat([]byte{byte(index + 1)}, 32), SourceGeneration: uint64(5 + index),
				EncryptedByteLength: 4096}); err != nil {
				t.Fatal(err)
			}
		}
		codec, _ := paging.NewCodec(bytes.Repeat([]byte{0x55}, 32))
		repository, _ := NewPreRestoreArchiveRepository(database, codec)
		authorizer := &commandAuthorizerStub{}
		commands, err := NewPreRestoreArchiveCommandService(PreRestoreArchiveCommandServiceConfig{
			WorkspaceID: workspaceID, Repository: repository, Authorizer: authorizer})
		if err != nil {
			t.Fatal(err)
		}
		response, err := commands.ListPreRestoreArchives(ctx, &tammyv1.ListPreRestoreArchivesRequest{
			Authentication: commandAuthentication(), Page: &tammyv1.PageRequest{PageSize: 10}})
		if err != nil || len(response.Archives) != 2 || response.Archives[0].SourceGeneration != 5 ||
			response.Archives[1].SourceGeneration != 6 || response.Archives[0].Id == response.Archives[1].Id {
			t.Fatalf("multiple restore archives=%#v error=%v", response, err)
		}
	})

	t.Run("export_authorized", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Destinations = commandDestinationResolver{destinationID: destination}
		fixture.commands.config.NewJobID = func() (string, error) { return "018f0000-0000-7000-8000-0000000000b1", nil }
		fixture.commands.config.Now = func() time.Time { return fixture.now }
		response, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000a1", fixture.now, "administrator-password"))
		if err != nil || response == nil || response.Job == nil ||
			response.Job.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED {
			t.Fatalf("ExportPreRestoreArchive()=%#v error=%v", response, err)
		}
		completed, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), response.Job.Id)
		if err != nil || completed.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED ||
			len(completed.DestinationHash) != 32 || !bytes.Equal(destination.contents, fixture.encryptedArchive) ||
			fixture.authorizer.mutationCalls != 1 {
			t.Fatalf("RunPreRestoreArchiveExport()=%#v destination=%x calls=%d error=%v",
				completed, destination.contents, fixture.authorizer.mutationCalls, err)
		}
	})

	t.Run("export_wrong_password", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		operationKey := "018f0000-0000-7000-8000-0000000000a2"
		response, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, operationKey, fixture.now, "wrong-password"))
		if response != nil || !errors.Is(err, ErrPreRestoreAuthorization) || len(destination.contents) != 0 {
			t.Fatalf("wrong-password response=%#v destination=%x error=%v", response, destination.contents, err)
		}
		if _, err := loadPreRestoreExportJobByOperation(context.Background(), fixture.database, operationKey); !errors.Is(err, ErrPreRestoreExportJob) {
			t.Fatalf("wrong-password persisted job error=%v", err)
		}
	})

	t.Run("export_stale_totp", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		operationKey := "018f0000-0000-7000-8000-0000000000a3"
		request := exportPreRestoreRequest(fixture.archiveID, destinationID, operationKey,
			fixture.now.Add(-6*time.Minute), "administrator-password")
		response, err := fixture.commands.ExportPreRestoreArchive(context.Background(), request)
		if response != nil || !errors.Is(err, ErrPreRestoreAuthorization) || len(destination.contents) != 0 {
			t.Fatalf("stale-TOTP response=%#v destination=%x error=%v", response, destination.contents, err)
		}
		if _, err := loadPreRestoreExportJobByOperation(context.Background(), fixture.database, operationKey); !errors.Is(err, ErrPreRestoreExportJob) {
			t.Fatalf("stale-TOTP persisted job error=%v", err)
		}
	})

	t.Run("cancel_before_rename", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		exported, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000a4", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		cancelled, err := fixture.commands.CancelPreRestoreArchiveExport(context.Background(),
			&tammyv1.CancelPreRestoreArchiveExportRequest{CommandContext: &tammyv1.CommandContext{
				IdempotencyKey: "018f0000-0000-7000-8000-0000000000c1", Authentication: commandAuthentication()},
				JobId: exported.Job.Id, ExpectedVersion: exported.Job.Version})
		if err != nil || cancelled == nil || cancelled.Job == nil ||
			cancelled.Job.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED ||
			cancelled.Job.Version != exported.Job.Version+1 || len(destination.contents) != 0 {
			t.Fatalf("CancelPreRestoreArchiveExport()=%#v destination=%x error=%v", cancelled, destination.contents, err)
		}
		if completed, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id); completed != nil || !errors.Is(err, ErrPreRestoreExportJobConflict) || len(destination.contents) != 0 {
			t.Fatalf("run-after-cancel job=%#v destination=%x error=%v", completed, destination.contents, err)
		}
	})

	t.Run("cancel_writing_before_commit_point", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		exported, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000b1", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		fixture.commands.config.hooks = &preRestoreExportHooks{beforeAtomicCommitFence: func(job preRestoreExportJobRecord) error {
			cancelled, cancelErr := fixture.commands.CancelPreRestoreArchiveExport(context.Background(),
				&tammyv1.CancelPreRestoreArchiveExportRequest{CommandContext: &tammyv1.CommandContext{
					IdempotencyKey: "018f0000-0000-7000-8000-0000000000c2", Authentication: commandAuthentication()},
					JobId: job.ID, ExpectedVersion: job.Version})
			if cancelErr != nil || cancelled == nil || cancelled.Job == nil ||
				cancelled.Job.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED {
				t.Fatalf("writing cancellation=%#v error=%v", cancelled, cancelErr)
			}
			return nil
		}}
		result, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id)
		if err != nil || result == nil ||
			result.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED ||
			destination.commits != 0 || len(destination.contents) != 0 {
			t.Fatalf("cancel-before-fence result=%#v commits=%d destination=%x error=%v",
				result, destination.commits, destination.contents, err)
		}
	})

	t.Run("cancel_loses_after_commit_point", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		exported, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000b2", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		fixture.commands.config.hooks = &preRestoreExportHooks{afterCommitPoint: func(job preRestoreExportJobRecord) error {
			cancelled, cancelErr := fixture.commands.CancelPreRestoreArchiveExport(context.Background(),
				&tammyv1.CancelPreRestoreArchiveExportRequest{CommandContext: &tammyv1.CommandContext{
					IdempotencyKey: "018f0000-0000-7000-8000-0000000000c3", Authentication: commandAuthentication()},
					JobId: job.ID, ExpectedVersion: job.Version})
			if cancelled != nil || !errors.Is(cancelErr, ErrPreRestoreExportJobConflict) {
				t.Fatalf("post-fence cancellation=%#v error=%v", cancelled, cancelErr)
			}
			return nil
		}}
		result, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id)
		if err != nil || result == nil ||
			result.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED ||
			destination.commits != 1 || !bytes.Equal(destination.contents, fixture.encryptedArchive) {
			t.Fatalf("commit-point winner result=%#v commits=%d destination=%x error=%v",
				result, destination.commits, destination.contents, err)
		}
	})

	t.Run("death_after_rename", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		fixture.commands.config.hooks = &preRestoreExportHooks{afterDestinationRename: func() error {
			return errors.New("injected process death after destination rename")
		}}
		exported, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000a5", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		if result, runErr := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id); result != nil || !errors.Is(runErr, ErrPreRestoreExportJob) ||
			!bytes.Equal(destination.contents, fixture.encryptedArchive) || destination.commits != 1 {
			t.Fatalf("death-after-rename result=%#v commits=%d destination=%x error=%v",
				result, destination.commits, destination.contents, runErr)
		}
		restarted, err := NewPreRestoreArchiveCommandService(PreRestoreArchiveCommandServiceConfig{
			WorkspaceID: fixture.workspaceID, Repository: fixture.commands.repository, Authorizer: fixture.authorizer,
			Transactions: commandTransactions{database: fixture.database}, Archives: fixture.store,
			Destinations: commandDestinationResolver{destinationID: destination}, Now: func() time.Time {
				return fixture.now.Add(time.Minute)
			}})
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := restarted.RecoverPreRestoreArchiveExports(context.Background())
		if err != nil || len(recovered) != 1 ||
			recovered[0].State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED ||
			!bytes.Equal(recovered[0].DestinationHash, sha256Digest(fixture.encryptedArchive)) || destination.commits != 1 {
			t.Fatalf("RecoverPreRestoreArchiveExports()=%#v commits=%d error=%v", recovered, destination.commits, err)
		}
	})

	t.Run("destination_hash_recovery", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		fixture.commands.config.hooks = &preRestoreExportHooks{afterDestinationRename: func() error {
			return errors.New("injected process death after destination rename")
		}}
		exported, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000a6", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id); !errors.Is(err, ErrPreRestoreExportJob) {
			t.Fatalf("death injection error=%v", err)
		}
		destination.mu.Lock()
		destination.contents = []byte("tampered committed destination")
		destination.mu.Unlock()
		fixture.commands.config.hooks = nil
		recovered, err := fixture.commands.RecoverPreRestoreArchiveExports(context.Background())
		if err != nil || len(recovered) != 1 ||
			recovered[0].State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
			recovered[0].Progress == nil || recovered[0].Progress.Stage != "DESTINATION_REAPPROVAL_REQUIRED" ||
			len(recovered[0].DestinationHash) != 0 || destination.commits != 1 {
			t.Fatalf("hash-mismatch recovery=%#v commits=%d error=%v", recovered, destination.commits, err)
		}
		if result, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id); result != nil || !errors.Is(err, ErrPreRestoreExportJobConflict) || destination.commits != 1 {
			t.Fatalf("retry-without-reapproval result=%#v commits=%d error=%v", result, destination.commits, err)
		}
	})

	t.Run("retry_destination_reapproval", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		oldDestinationID := "018f0000-0000-7000-8000-0000000000d1"
		oldDestination := &commandDestination{reference: oldDestinationID}
		configurePreRestoreExportFixture(fixture, oldDestinationID, oldDestination)
		fixture.commands.config.hooks = &preRestoreExportHooks{afterDestinationRename: func() error {
			return errors.New("injected process death after destination rename")
		}}
		exported, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, oldDestinationID, "018f0000-0000-7000-8000-0000000000a7", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id); !errors.Is(err, ErrPreRestoreExportJob) {
			t.Fatalf("death injection error=%v", err)
		}
		oldDestination.mu.Lock()
		oldDestination.contents = []byte("tampered committed destination")
		oldDestination.mu.Unlock()
		fixture.commands.config.hooks = nil
		recovered, err := fixture.commands.RecoverPreRestoreArchiveExports(context.Background())
		if err != nil || len(recovered) != 1 {
			t.Fatalf("recovery=%#v error=%v", recovered, err)
		}
		newDestinationID := "018f0000-0000-7000-8000-0000000000d2"
		newDestination := &commandDestination{reference: newDestinationID}
		fixture.commands.config.Destinations = commandDestinationResolver{
			oldDestinationID: oldDestination, newDestinationID: newDestination,
		}
		retry := retryPreRestoreRequest(exported.Job.Id, recovered[0].Version, newDestinationID,
			"018f0000-0000-7000-8000-0000000000e1", fixture.now, "administrator-password")
		queued, err := fixture.commands.RetryPreRestoreArchiveExport(context.Background(), retry)
		if err != nil || queued == nil || queued.Job == nil ||
			queued.Job.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED ||
			queued.Job.Version != recovered[0].Version+1 || oldDestination.commits != 1 || newDestination.commits != 0 {
			t.Fatalf("RetryPreRestoreArchiveExport()=%#v old=%d new=%d error=%v",
				queued, oldDestination.commits, newDestination.commits, err)
		}
		replayed, err := fixture.commands.RetryPreRestoreArchiveExport(context.Background(), retry)
		if err != nil || !proto.Equal(replayed.Job, queued.Job) {
			t.Fatalf("retry replay=%#v want=%#v error=%v", replayed, queued, err)
		}
		completed, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), queued.Job.Id)
		if err != nil || completed.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED ||
			oldDestination.commits != 1 || newDestination.commits != 1 {
			t.Fatalf("reauthorized run=%#v old=%d new=%d error=%v",
				completed, oldDestination.commits, newDestination.commits, err)
		}
	})

	t.Run("delete_before_12_months", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		commandNow := fixture.now.AddDate(0, -1, 0)
		fixture.authorizer.now = commandNow
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Now = func() time.Time { return commandNow }
		fixture.commands.config.Audit = &commandAuditStub{}
		response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), deletePreRestoreRequest(
			fixture.archiveID, 1, "018f0000-0000-7000-8000-0000000000f1", commandNow,
			"administrator-password", "Retention period has not elapsed"))
		if response != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) {
			t.Fatalf("early delete response=%#v error=%v", response, err)
		}
		record, err := loadPreRestoreArchiveRecord(context.Background(), fixture.database, fixture.workspaceID, fixture.archiveID)
		if err != nil || record.Version != 1 ||
			record.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE ||
			len(fixture.store.bytesByID[fixture.archiveID]) == 0 {
			t.Fatalf("early delete changed record=%#v bytes=%d error=%v",
				record, len(fixture.store.bytesByID[fixture.archiveID]), err)
		}
	})

	t.Run("delete_after_12_months", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		auditStub := &commandAuditStub{}
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Now = func() time.Time { return fixture.now }
		fixture.commands.config.Audit = auditStub
		response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), deletePreRestoreRequest(
			fixture.archiveID, 1, "018f0000-0000-7000-8000-0000000000f2", fixture.now,
			"administrator-password", "Retention elapsed; predecessor evidence no longer required"))
		if err != nil || response == nil || response.Archive == nil || response.Archive.Version != 3 ||
			response.Archive.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED ||
			response.Archive.DeletedAt == nil || !response.Archive.DeletedAt.AsTime().Equal(fixture.now) ||
			len(fixture.store.bytesByID[fixture.archiveID]) != 0 || auditStub.calls != 1 || auditStub.event == nil ||
			auditStub.event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PRE_RESTORE_ARCHIVE_CHANGED {
			t.Fatalf("eligible delete response=%#v bytes=%d audit=%#v calls=%d error=%v",
				response, len(fixture.store.bytesByID[fixture.archiveID]), auditStub.event, auditStub.calls, err)
		}
	})

	t.Run("delete_reason_intent_is_immutable", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		configurePreRestoreDeleteFixture(fixture, &commandAuditStub{})
		fixture.commands.config.deleteHooks = &preRestoreDeleteHooks{afterPendingCommit: func() error {
			return errors.New("stop at durable pending intent")
		}}
		operationKey := "018f0000-0000-7000-8000-0000000000e8"
		if _, err := fixture.commands.DeletePreRestoreArchive(context.Background(), deletePreRestoreRequest(
			fixture.archiveID, 1, operationKey, fixture.now, "administrator-password", "Immutable retained reason")); !errors.Is(err, ErrPreRestoreArchiveCommand) {
			t.Fatalf("pending intent error=%v", err)
		}
		if _, err := fixture.database.ExecContext(context.Background(),
			`UPDATE pre_restore_archive_commands_v1 SET deletion_reason=? WHERE operation_key=?`, "mutated", operationKey); err == nil {
			t.Fatal("persisted deletion reason mutation succeeded")
		}
	})

	t.Run("delete_crash_recovery", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		auditStub := &commandAuditStub{}
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Now = func() time.Time { return fixture.now }
		fixture.commands.config.Audit = auditStub
		fixture.commands.config.deleteHooks = &preRestoreDeleteHooks{afterPendingCommit: func() error {
			return errors.New("injected process death after DELETE_PENDING commit")
		}}
		request := deletePreRestoreRequest(fixture.archiveID, 1,
			"018f0000-0000-7000-8000-0000000000f3", fixture.now, "administrator-password", "Crash recovery cleanup")
		if response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); response != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) {
			t.Fatalf("death-after-pending response=%#v error=%v", response, err)
		}
		pending, err := loadPreRestoreArchiveRecord(context.Background(), fixture.database, fixture.workspaceID, fixture.archiveID)
		if err != nil || pending.Version != 2 ||
			pending.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING ||
			len(fixture.store.bytesByID[fixture.archiveID]) == 0 || auditStub.calls != 0 {
			t.Fatalf("pending=%#v bytes=%d audit=%d error=%v",
				pending, len(fixture.store.bytesByID[fixture.archiveID]), auditStub.calls, err)
		}
		fixture.commands.config.deleteHooks = nil
		recovered, err := fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background())
		if err != nil || len(recovered) != 1 || recovered[0].Archive == nil ||
			recovered[0].Archive.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED ||
			len(fixture.store.bytesByID[fixture.archiveID]) != 0 || auditStub.calls != 1 {
			t.Fatalf("delete recovery=%#v bytes=%d audit=%d error=%v",
				recovered, len(fixture.store.bytesByID[fixture.archiveID]), auditStub.calls, err)
		}
		recovered, err = fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background())
		if err != nil || len(recovered) != 0 || auditStub.calls != 1 {
			t.Fatalf("second recovery=%#v audit=%d error=%v", recovered, auditStub.calls, err)
		}
		replayed, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request)
		if err != nil || !proto.Equal(replayed, recoveredDeleteResponse(fixture, fixture.archiveID)) || auditStub.calls != 1 {
			t.Fatalf("completed replay=%#v audit=%d error=%v", replayed, auditStub.calls, err)
		}
	})

	t.Run("delete_remove_failure_recovery", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		depth := 0
		fixture.store.mutationDepth = &depth
		fixture.store.deleteErr = errors.New("injected encrypted-file remove or directory fsync failure")
		auditStub := &commandAuditStub{}
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database, mutationDepth: &depth}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Now = func() time.Time { return fixture.now }
		fixture.commands.config.Audit = auditStub
		request := deletePreRestoreRequest(fixture.archiveID, 1,
			"018f0000-0000-7000-8000-0000000000f4", fixture.now, "administrator-password", "Recover failed secure removal")
		if response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); response != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) {
			t.Fatalf("remove failure response=%#v error=%v", response, err)
		}
		pending, err := loadPreRestoreArchiveRecord(context.Background(), fixture.database, fixture.workspaceID, fixture.archiveID)
		if err != nil || pending.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING ||
			len(fixture.store.bytesByID[fixture.archiveID]) == 0 || fixture.store.deleteCalls != 1 ||
			fixture.store.deleteObservedTransaction || auditStub.calls != 0 {
			t.Fatalf("remove failure pending=%#v bytes=%d deletes=%d inTx=%t audit=%d error=%v", pending,
				len(fixture.store.bytesByID[fixture.archiveID]), fixture.store.deleteCalls,
				fixture.store.deleteObservedTransaction, auditStub.calls, err)
		}
		fixture.store.deleteErr = nil
		recovered, err := fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background())
		if err != nil || len(recovered) != 1 || len(fixture.store.bytesByID[fixture.archiveID]) != 0 ||
			fixture.store.deleteCalls != 2 || fixture.store.deleteObservedTransaction || auditStub.calls != 1 {
			t.Fatalf("remove recovery=%#v bytes=%d deletes=%d inTx=%t audit=%d error=%v", recovered,
				len(fixture.store.bytesByID[fixture.archiveID]), fixture.store.deleteCalls,
				fixture.store.deleteObservedTransaction, auditStub.calls, err)
		}
	})

	t.Run("delete_death_after_remove", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		auditStub := &commandAuditStub{}
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Now = func() time.Time { return fixture.now }
		fixture.commands.config.Audit = auditStub
		fixture.commands.config.deleteHooks = &preRestoreDeleteHooks{afterFileRemoval: func() error {
			return errors.New("injected process death after secure removal")
		}}
		request := deletePreRestoreRequest(fixture.archiveID, 1,
			"018f0000-0000-7000-8000-0000000000f5", fixture.now, "administrator-password", "Resume after secure removal")
		if response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); response != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) {
			t.Fatalf("death-after-remove response=%#v error=%v", response, err)
		}
		pending, err := loadPreRestoreArchiveRecord(context.Background(), fixture.database, fixture.workspaceID, fixture.archiveID)
		if err != nil || pending.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING ||
			len(fixture.store.bytesByID[fixture.archiveID]) != 0 || auditStub.calls != 0 {
			t.Fatalf("death-after-remove pending=%#v bytes=%d audit=%d error=%v", pending,
				len(fixture.store.bytesByID[fixture.archiveID]), auditStub.calls, err)
		}
		fixture.commands.config.deleteHooks = nil
		recovered, err := fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background())
		if err != nil || len(recovered) != 1 || recovered[0].Archive.State !=
			tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED || auditStub.calls != 1 {
			t.Fatalf("death-after-remove recovery=%#v audit=%d error=%v", recovered, auditStub.calls, err)
		}
	})

	t.Run("delete_forged_pending_intent", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		auditStub := &commandAuditStub{}
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Now = func() time.Time { return fixture.now }
		fixture.commands.config.Audit = auditStub
		fixture.commands.config.deleteHooks = &preRestoreDeleteHooks{afterPendingCommit: func() error {
			return errors.New("stop at durable pending intent")
		}}
		operationKey := "018f0000-0000-7000-8000-0000000000f6"
		request := deletePreRestoreRequest(fixture.archiveID, 1, operationKey, fixture.now,
			"administrator-password", "Forged pending intent must fail closed")
		if _, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); !errors.Is(err, ErrPreRestoreArchiveCommand) {
			t.Fatalf("pending intent error=%v", err)
		}
		intent, err := loadPreRestoreDeleteIntent(context.Background(), fixture.database, operationKey)
		if err != nil {
			t.Fatal(err)
		}
		intent.AuditEvent.Payload.GetPreRestoreArchiveChanged().ArchiveId = "018f0000-0000-7000-8000-000000000099"
		forged, err := proto.MarshalOptions{Deterministic: true}.Marshal(intent.AuditEvent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.database.ExecContext(context.Background(),
			`DROP TRIGGER pre_restore_archive_commands_v1_complete_only`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.database.ExecContext(context.Background(),
			`UPDATE pre_restore_archive_commands_v1 SET audit_event_proto=? WHERE operation_key=?`, forged, operationKey); err != nil {
			t.Fatal(err)
		}
		fixture.commands.config.deleteHooks = nil
		if recovered, err := fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background()); recovered != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) ||
			len(fixture.store.bytesByID[fixture.archiveID]) == 0 || fixture.store.deleteCalls != 0 || auditStub.calls != 0 {
			t.Fatalf("forged recovery=%#v bytes=%d deletes=%d audit=%d error=%v", recovered,
				len(fixture.store.bytesByID[fixture.archiveID]), fixture.store.deleteCalls, auditStub.calls, err)
		}
	})

	t.Run("delete_recovery_is_bounded", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		auditStub := &commandAuditStub{}
		fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
		fixture.commands.config.Archives = fixture.store
		fixture.commands.config.Now = func() time.Time { return fixture.now }
		fixture.commands.config.Audit = auditStub
		createdAt := fixture.now.AddDate(-1, 0, -1)
		for index := 0; index < 257; index++ {
			archiveID := fmt.Sprintf("018f1000-0000-7000-8000-%012x", index+1)
			restoreOperationID := fmt.Sprintf("018f2000-0000-7000-8000-%012x", index+1)
			key := fmt.Sprintf("018f3000-0000-7000-8000-%012x", index+1)
			contents := []byte(fmt.Sprintf("encrypted predecessor archive %d", index))
			contentHash := sha256.Sum256(contents)
			reason := "bounded cleanup"
			reasonHash := sha256.Sum256([]byte(reason))
			inputHash := preRestoreDeleteInputHash(fixture.workspaceID, archiveID, 1, reason)
			if _, insertErr := fixture.database.ExecContext(context.Background(), `INSERT INTO pre_restore_archives_v1(
				archive_id,workspace_id,operation_id,version,state,created_at,deletion_eligible_at,content_hash,
				source_generation,encrypted_byte_length,deletion_reason_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, archiveID,
				fixture.workspaceID, restoreOperationID, 2,
				int32(tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING), formatPreRestoreTime(createdAt),
				formatPreRestoreTime(createdAt.AddDate(1, 0, 0)), contentHash[:], index+1, len(contents), reasonHash[:]); insertErr != nil {
				t.Fatal(insertErr)
			}
			event := &tammyv1.AuditEvent{WorkspaceId: fixture.workspaceID,
				Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PRE_RESTORE_ARCHIVE_CHANGED,
				OccurredAt: timestamppb.New(fixture.now), Actor: commandAuthentication(),
				CommandType: "DELETE_PRE_RESTORE_ARCHIVE", IdempotencyKey: &key,
				BeforeSemanticHash: contentHash[:], AfterSemanticHash: reasonHash[:],
				Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_PreRestoreArchiveChanged{
					PreRestoreArchiveChanged: &tammyv1.PreRestoreArchiveChangedEvent{ArchiveId: archiveID,
						FromState: tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE.Enum(),
						ToState:   tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED, ContentHash: contentHash[:]}}}}
			eventBytes, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(event)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if _, insertErr := fixture.database.ExecContext(context.Background(), `INSERT INTO pre_restore_archive_commands_v1(
				operation_key,workspace_id,archive_id,expected_archive_version,command_type,deletion_reason,input_hash,audit_event_proto,
				status,version,created_at,updated_at) VALUES(?,?,?,?, 'DELETE',?,?,?,1,1,?,?)`, key,
				fixture.workspaceID, archiveID, 1, reason, inputHash[:], eventBytes,
				formatPreRestoreTime(fixture.now), formatPreRestoreTime(fixture.now)); insertErr != nil {
				t.Fatal(insertErr)
			}
			fixture.store.bytesByID[archiveID] = contents
		}
		first, err := fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background())
		if err != nil || len(first) != 256 || fixture.store.deleteCalls != 256 || auditStub.calls != 256 {
			t.Fatalf("first bounded recovery=%d deletes=%d audit=%d error=%v",
				len(first), fixture.store.deleteCalls, auditStub.calls, err)
		}
		second, err := fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background())
		if err != nil || len(second) != 1 || fixture.store.deleteCalls != 257 || auditStub.calls != 257 {
			t.Fatalf("second bounded recovery=%d deletes=%d audit=%d error=%v",
				len(second), fixture.store.deleteCalls, auditStub.calls, err)
		}
	})

	t.Run("delete_rejection_boundaries", func(t *testing.T) {
		t.Run("missing_reason", func(t *testing.T) {
			fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
			defer fixture.close()
			configurePreRestoreDeleteFixture(fixture, &commandAuditStub{})
			request := deletePreRestoreRequest(fixture.archiveID, 1,
				"018f0000-0000-7000-8000-0000000000f8", fixture.now, "administrator-password", "")
			if response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); response != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) || fixture.store.deleteCalls != 0 {
				t.Fatalf("missing-reason response=%#v deletes=%d error=%v", response, fixture.store.deleteCalls, err)
			}
		})
		t.Run("stale_version", func(t *testing.T) {
			fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
			defer fixture.close()
			configurePreRestoreDeleteFixture(fixture, &commandAuditStub{})
			request := deletePreRestoreRequest(fixture.archiveID, 2,
				"018f0000-0000-7000-8000-0000000000f9", fixture.now, "administrator-password", "Stale version")
			if response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); response != nil || !errors.Is(err, ErrPreRestoreExportJobConflict) || fixture.store.deleteCalls != 0 {
				t.Fatalf("stale-version response=%#v deletes=%d error=%v", response, fixture.store.deleteCalls, err)
			}
		})
		t.Run("active_export", func(t *testing.T) {
			fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
			defer fixture.close()
			destinationID := "018f0000-0000-7000-8000-0000000000d1"
			configurePreRestoreExportFixture(fixture, destinationID, &commandDestination{reference: destinationID})
			fixture.commands.config.Audit = &commandAuditStub{}
			if _, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
				fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000aa", fixture.now,
				"administrator-password")); err != nil {
				t.Fatal(err)
			}
			request := deletePreRestoreRequest(fixture.archiveID, 1,
				"018f0000-0000-7000-8000-0000000000fa", fixture.now, "administrator-password", "Active export blocks deletion")
			if response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); response != nil || !errors.Is(err, ErrPreRestoreExportJobConflict) || fixture.store.deleteCalls != 0 {
				t.Fatalf("active-export response=%#v deletes=%d error=%v", response, fixture.store.deleteCalls, err)
			}
		})
	})

	t.Run("replay_conflict", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		auditStub := &commandAuditStub{}
		configurePreRestoreDeleteFixture(fixture, auditStub)
		request := deletePreRestoreRequest(fixture.archiveID, 1,
			"018f0000-0000-7000-8000-0000000000fb", fixture.now, "administrator-password", "Exact replay election")
		first, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request)
		if err != nil || !proto.Equal(replayed, first) || fixture.store.deleteCalls != 1 || auditStub.calls != 1 {
			t.Fatalf("exact replay=%#v first=%#v deletes=%d audit=%d error=%v",
				replayed, first, fixture.store.deleteCalls, auditStub.calls, err)
		}
		changed := proto.Clone(request).(*tammyv1.DeletePreRestoreArchiveRequest)
		changed.Reason = "Changed semantic request"
		if response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), changed); response != nil || !errors.Is(err, ErrPreRestoreExportJobConflict) || fixture.store.deleteCalls != 1 ||
			auditStub.calls != 1 {
			t.Fatalf("changed replay=%#v deletes=%d audit=%d error=%v",
				response, fixture.store.deleteCalls, auditStub.calls, err)
		}
	})

	t.Run("export_job_discovery_after_restart", func(t *testing.T) {
		fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
		defer fixture.close()
		destinationID := "018f0000-0000-7000-8000-0000000000d1"
		destination := &commandDestination{reference: destinationID}
		configurePreRestoreExportFixture(fixture, destinationID, destination)
		exported, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000ac", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.commands.RunPreRestoreArchiveExport(context.Background(), exported.Job.Id); err != nil {
			t.Fatal(err)
		}
		fixture.commands.config.NewJobID = func() (string, error) { return "018f0000-0000-7000-8000-0000000000b2", nil }
		secondExport, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000ad", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		restarted, err := NewPreRestoreArchiveCommandService(PreRestoreArchiveCommandServiceConfig{
			WorkspaceID: fixture.workspaceID, Repository: fixture.commands.repository, Authorizer: fixture.authorizer,
			Transactions: commandTransactions{database: fixture.database}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := restarted.GetPreRestoreArchiveExportJob(context.Background(),
			&tammyv1.GetPreRestoreArchiveExportJobRequest{Authentication: commandAuthentication(), JobId: exported.Job.Id})
		if err != nil || got == nil || got.Job == nil ||
			got.Job.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED {
			t.Fatalf("GetPreRestoreArchiveExportJob()=%#v error=%v", got, err)
		}
		listed, err := restarted.ListPreRestoreArchiveExportJobs(context.Background(),
			&tammyv1.ListPreRestoreArchiveExportJobsRequest{Authentication: commandAuthentication(),
				Page: &tammyv1.PageRequest{PageSize: 1}})
		if err != nil || listed == nil || len(listed.Jobs) != 1 || listed.Page == nil ||
			listed.Page.ReturnedCount != 1 || listed.Page.NextCursor == nil {
			t.Fatalf("ListPreRestoreArchiveExportJobs()=%#v error=%v", listed, err)
		}
		tampered := *listed.Page.NextCursor
		tampered = tampered[:len(tampered)-1] + "A"
		if response, err := restarted.ListPreRestoreArchiveExportJobs(context.Background(),
			&tammyv1.ListPreRestoreArchiveExportJobsRequest{Authentication: commandAuthentication(), Page: &tammyv1.PageRequest{
				PageSize: 1, Cursor: &tampered}}); response != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) {
			t.Fatalf("tampered export cursor response=%#v error=%v", response, err)
		}
		fixture.commands.config.NewJobID = func() (string, error) { return "018f0000-0000-7000-8000-0000000000b3", nil }
		thirdExport, err := fixture.commands.ExportPreRestoreArchive(context.Background(), exportPreRestoreRequest(
			fixture.archiveID, destinationID, "018f0000-0000-7000-8000-0000000000ae", fixture.now,
			"administrator-password"))
		if err != nil {
			t.Fatal(err)
		}
		continued, err := restarted.ListPreRestoreArchiveExportJobs(context.Background(),
			&tammyv1.ListPreRestoreArchiveExportJobsRequest{Authentication: commandAuthentication(), Page: &tammyv1.PageRequest{
				PageSize: 1, Cursor: listed.Page.NextCursor}})
		if err != nil || len(continued.Jobs) != 1 || continued.Jobs[0].Id != secondExport.Job.Id ||
			continued.Jobs[0].Id == thirdExport.Job.Id || continued.Page.NextCursor != nil {
			t.Fatalf("stable continuation=%#v second=%s appended=%s error=%v",
				continued, secondExport.Job.Id, thirdExport.Job.Id, err)
		}
		fixture.authorizer.readErr = ErrPreRestoreAuthorization
		if response, err := restarted.GetPreRestoreArchiveExportJob(context.Background(),
			&tammyv1.GetPreRestoreArchiveExportJobRequest{Authentication: commandAuthentication(), JobId: exported.Job.Id}); response != nil || !errors.Is(err, ErrPreRestoreAuthorization) {
			t.Fatalf("denied job discovery response=%#v error=%v", response, err)
		}
	})
}

func commandAuthentication() *tammyv1.AuthenticationContext {
	return &tammyv1.AuthenticationContext{ActorUserId: "018f0000-0000-7000-8000-000000000010",
		SessionId: "018f0000-0000-7000-8000-000000000011"}
}

func TestPreRestoreDeleteRejectsForgedPersistedSemantics(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *preRestoreCommandFixture, string)
	}{
		{name: "input_hash", mutate: func(t *testing.T, fixture *preRestoreCommandFixture, operationKey string) {
			if _, err := fixture.database.ExecContext(context.Background(), `DROP TRIGGER pre_restore_archive_commands_v1_complete_only`); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.database.ExecContext(context.Background(),
				`UPDATE pre_restore_archive_commands_v1 SET input_hash=? WHERE operation_key=?`, bytes.Repeat([]byte{0xf1}, sha256.Size), operationKey); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "audit_reason_hash", mutate: func(t *testing.T, fixture *preRestoreCommandFixture, operationKey string) {
			intent, err := loadPreRestoreDeleteIntent(context.Background(), fixture.database, operationKey)
			if err != nil {
				t.Fatal(err)
			}
			intent.AuditEvent.AfterSemanticHash = bytes.Repeat([]byte{0xf2}, sha256.Size)
			forged, err := proto.MarshalOptions{Deterministic: true}.Marshal(intent.AuditEvent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.database.ExecContext(context.Background(), `DROP TRIGGER pre_restore_archive_commands_v1_complete_only`); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.database.ExecContext(context.Background(),
				`UPDATE pre_restore_archive_commands_v1 SET audit_event_proto=? WHERE operation_key=?`, forged, operationKey); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "archive_reason_hash", mutate: func(t *testing.T, fixture *preRestoreCommandFixture, _ string) {
			if _, err := fixture.database.ExecContext(context.Background(), `DROP TRIGGER pre_restore_archives_v1_linked_update_only`); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.database.ExecContext(context.Background(),
				`UPDATE pre_restore_archives_v1 SET deletion_reason_hash=? WHERE archive_id=?`, bytes.Repeat([]byte{0xf3}, sha256.Size), fixture.archiveID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "canonical_reason", mutate: func(t *testing.T, fixture *preRestoreCommandFixture, operationKey string) {
			if _, err := fixture.database.ExecContext(context.Background(), `DROP TRIGGER pre_restore_archive_commands_v1_complete_only`); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.database.ExecContext(context.Background(),
				`UPDATE pre_restore_archive_commands_v1 SET deletion_reason='forged persisted reason' WHERE operation_key=?`, operationKey); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
			defer fixture.close()
			auditStub := &commandAuditStub{}
			configurePreRestoreDeleteFixture(fixture, auditStub)
			fixture.commands.config.deleteHooks = &preRestoreDeleteHooks{afterPendingCommit: func() error {
				return errors.New("stop at durable pending intent")
			}}
			operationKey := "018f0000-0000-7000-8000-0000000000d5"
			request := deletePreRestoreRequest(fixture.archiveID, 1, operationKey, fixture.now,
				"administrator-password", "Canonical persisted deletion reason")
			if _, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); !errors.Is(err, ErrPreRestoreArchiveCommand) {
				t.Fatalf("pending intent error=%v", err)
			}
			test.mutate(t, fixture, operationKey)
			fixture.commands.config.deleteHooks = nil
			recovered, err := fixture.commands.RecoverPreRestoreArchiveDeletes(context.Background())
			if recovered != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) || fixture.store.deleteCalls != 0 ||
				len(fixture.store.bytesByID[fixture.archiveID]) == 0 || auditStub.calls != 0 {
				t.Fatalf("forged recovery=%#v deletes=%d bytes=%d audit=%d error=%v", recovered,
					fixture.store.deleteCalls, len(fixture.store.bytesByID[fixture.archiveID]), auditStub.calls, err)
			}
		})
	}
}

func TestPreRestoreDeleteRejectsForgedCompletedResult(t *testing.T) {
	fixture := newPreRestoreCommandFixture(t, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
	defer fixture.close()
	configurePreRestoreDeleteFixture(fixture, &commandAuditStub{})
	operationKey := "018f0000-0000-7000-8000-0000000000d6"
	request := deletePreRestoreRequest(fixture.archiveID, 1, operationKey, fixture.now,
		"administrator-password", "Completed tombstone must remain exact")
	if _, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	intent, err := loadPreRestoreDeleteIntent(context.Background(), fixture.database, operationKey)
	if err != nil {
		t.Fatal(err)
	}
	forged := proto.Clone(intent.Result).(*tammyv1.DeletePreRestoreArchiveResponse)
	forged.Archive.Version++
	resultBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(context.Background(), `DROP TRIGGER pre_restore_archive_commands_v1_complete_only`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(context.Background(),
		`UPDATE pre_restore_archive_commands_v1 SET result_proto=? WHERE operation_key=?`, resultBytes, operationKey); err != nil {
		t.Fatal(err)
	}
	response, err := fixture.commands.DeletePreRestoreArchive(context.Background(), request)
	if response != nil || !errors.Is(err, ErrPreRestoreArchiveCommand) || fixture.store.deleteCalls != 1 {
		t.Fatalf("forged completed replay=%#v deletes=%d error=%v", response, fixture.store.deleteCalls, err)
	}
}

type preRestoreCommandFixture struct {
	database         *sqlcipher.Database
	key              []byte
	now              time.Time
	workspaceID      string
	archiveID        string
	encryptedArchive []byte
	store            *commandArchiveStore
	authorizer       *commandAuthorizerStub
	commands         *PreRestoreArchiveCommandService
}

func newPreRestoreCommandFixture(t *testing.T, now time.Time) *preRestoreCommandFixture {
	t.Helper()
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	archiveID := "018f0000-0000-7000-8000-000000000081"
	key := bytes.Repeat([]byte{0x33}, sqlcipher.KeySize)
	path := filepath.Join(t.TempDir(), "workspace.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	encryptedArchive := bytes.Repeat([]byte("encrypted-pre-restore-archive"), 100)
	digest := sha256.Sum256(encryptedArchive)
	createdAt := now.AddDate(-1, 0, -1)
	if err := PersistPreRestoreArchive(ctx, database, PreRestoreArchiveRecord{WorkspaceID: workspaceID,
		OperationID: "018f0000-0000-7000-8000-000000000091", ArchiveID: archiveID, Version: 1,
		State: tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE, CreatedAt: createdAt,
		DeletionEligibleAt: createdAt.AddDate(1, 0, 0), ContentHash: digest[:], SourceGeneration: 5,
		EncryptedByteLength: uint64(len(encryptedArchive))}); err != nil {
		t.Fatal(err)
	}
	codec, _ := paging.NewCodec(bytes.Repeat([]byte{0x55}, 32))
	repository, _ := NewPreRestoreArchiveRepository(database, codec)
	authorizer := &commandAuthorizerStub{wantPassword: "administrator-password", now: now}
	commands, err := NewPreRestoreArchiveCommandService(PreRestoreArchiveCommandServiceConfig{
		WorkspaceID: workspaceID, Repository: repository, Authorizer: authorizer})
	if err != nil {
		t.Fatal(err)
	}
	store := &commandArchiveStore{bytesByID: map[string][]byte{archiveID: encryptedArchive}}
	return &preRestoreCommandFixture{database: database, key: key, now: now, workspaceID: workspaceID,
		archiveID: archiveID, encryptedArchive: encryptedArchive, store: store, authorizer: authorizer, commands: commands}
}

func (fixture *preRestoreCommandFixture) close() {
	_ = fixture.database.Close()
	zeroBytes(fixture.key)
}

func configurePreRestoreExportFixture(fixture *preRestoreCommandFixture, destinationID string, destination *commandDestination) {
	fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
	fixture.commands.config.Archives = fixture.store
	fixture.commands.config.Destinations = commandDestinationResolver{destinationID: destination}
	fixture.commands.config.NewJobID = func() (string, error) { return "018f0000-0000-7000-8000-0000000000b1", nil }
	fixture.commands.config.Now = func() time.Time { return fixture.now }
}

func configurePreRestoreDeleteFixture(fixture *preRestoreCommandFixture, auditStub *commandAuditStub) {
	fixture.commands.config.Transactions = commandTransactions{database: fixture.database}
	fixture.commands.config.Archives = fixture.store
	fixture.commands.config.Now = func() time.Time { return fixture.now }
	fixture.commands.config.Audit = auditStub
}

func exportPreRestoreRequest(archiveID, destinationID, operationKey string, now time.Time, password string) *tammyv1.ExportPreRestoreArchiveRequest {
	return &tammyv1.ExportPreRestoreArchiveRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: operationKey, Authentication: commandAuthentication(), FreshFactor: &tammyv1.FreshFactorContext{
			AssertionId: "018f0000-0000-7000-8000-000000000012", Purpose: "export_pre_restore_archive",
			AssertedAt: timestamppb.New(now)}}, ArchiveId: archiveID, ExpectedVersion: 1,
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte(password)},
		Destination:           &tammyv1.ApprovedFileRef{CapabilityId: destinationID}}
}

func retryPreRestoreRequest(jobID string, expectedVersion uint64, destinationID, operationKey string,
	now time.Time, password string,
) *tammyv1.RetryPreRestoreArchiveExportRequest {
	return &tammyv1.RetryPreRestoreArchiveExportRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: operationKey, Authentication: commandAuthentication(), FreshFactor: &tammyv1.FreshFactorContext{
			AssertionId: "018f0000-0000-7000-8000-000000000013", Purpose: "retry_pre_restore_archive_export",
			AssertedAt: timestamppb.New(now)}}, JobId: jobID, ExpectedVersion: expectedVersion,
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte(password)},
		Destination:           &tammyv1.ApprovedFileRef{CapabilityId: destinationID}}
}

func deletePreRestoreRequest(archiveID string, expectedVersion uint64, operationKey string, now time.Time,
	password, reason string,
) *tammyv1.DeletePreRestoreArchiveRequest {
	return &tammyv1.DeletePreRestoreArchiveRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: operationKey, Authentication: commandAuthentication(), FreshFactor: &tammyv1.FreshFactorContext{
			AssertionId: "018f0000-0000-7000-8000-000000000014", Purpose: "delete_pre_restore_archive",
			AssertedAt: timestamppb.New(now)}}, ArchiveId: archiveID, ExpectedVersion: expectedVersion,
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte(password)}, Reason: reason}
}

func sha256Digest(contents []byte) []byte {
	digest := sha256.Sum256(contents)
	return digest[:]
}

func recoveredDeleteResponse(fixture *preRestoreCommandFixture, archiveID string) *tammyv1.DeletePreRestoreArchiveResponse {
	archive, _ := fixture.commands.repository.Get(context.Background(), fixture.workspaceID, archiveID)
	return &tammyv1.DeletePreRestoreArchiveResponse{Archive: archive}
}
