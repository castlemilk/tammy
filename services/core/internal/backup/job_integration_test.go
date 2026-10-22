//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package backup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type sqlcipherBackupJobTransactions struct {
	database      *sqlcipher.Database
	readCalls     *int
	mutationCalls *int
	callbackDepth *int
}

func (transactions sqlcipherBackupJobTransactions) Read(ctx context.Context, callback func(SQLExecutor) error) error {
	if transactions.readCalls != nil {
		*transactions.readCalls++
	}
	if transactions.callbackDepth != nil {
		*transactions.callbackDepth++
		defer func() { *transactions.callbackDepth-- }()
	}
	return callback(transactions.database)
}

func (transactions sqlcipherBackupJobTransactions) Mutate(ctx context.Context, callback func(SQLExecutor) error) error {
	if transactions.mutationCalls != nil {
		*transactions.mutationCalls++
	}
	tx, err := transactions.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	if transactions.callbackDepth != nil {
		*transactions.callbackDepth++
		defer func() { *transactions.callbackDepth-- }()
	}
	if err := callback(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type backupPassphraseProviderFunc func(context.Context, string, func([]byte) error) error

type blobProjectionObserver struct {
	executor              SQLExecutor
	rawJobProjections     int
	rawCheckpointProjects int
}

type observedBlobTransactions struct {
	database *sqlcipher.Database
	observer *blobProjectionObserver
}

func (transactions observedBlobTransactions) Read(ctx context.Context, callback func(SQLExecutor) error) error {
	return callback(transactions.observer)
}

func (transactions observedBlobTransactions) Mutate(ctx context.Context, callback func(SQLExecutor) error) error {
	tx, err := transactions.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := callback(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (observer *blobProjectionObserver) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	return observer.executor.ExecContext(ctx, query, arguments...)
}

func (observer *blobProjectionObserver) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	normalized := strings.ToLower(query)
	if strings.Contains(normalized, "select id,version,operation_key,semantic_sha256,payload_proto") {
		observer.rawJobProjections++
	}
	if strings.Contains(normalized, "select sequence,checkpoint_proto,checkpoint_sha256") {
		observer.rawCheckpointProjects++
	}
	return observer.executor.QueryContext(ctx, query, arguments...)
}

func (function backupPassphraseProviderFunc) WithPassphrase(
	ctx context.Context,
	jobID string,
	callback func([]byte) error,
) error {
	return function(ctx, jobID, callback)
}

func TestBackupJobLoadRejectsOversizedBlobsBeforeRawProjection(t *testing.T) {
	for index, field := range []string{"payload_proto", "progress_proto", "result_proto"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			key := bytes.Repeat([]byte{byte(0x20 + index)}, sqlcipher.KeySize)
			defer zero(key)
			path := filepath.Join(t.TempDir(), "oversized-job.db")
			if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
				t.Fatal(err)
			}
			database, err := sqlcipher.Open(ctx, path, key)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			id := func(offset int) string { return fmt.Sprintf("018f6400-0000-7000-8000-%012x", index*10+offset+1) }
			inputHash := backupJobInputHash(id(1), id(3))
			operationHash := backupJobOperationHash(id(2), inputHash)
			if _, err := EnqueueBackupJob(ctx, database, BackupJobSpec{ID: id(0), WorkspaceID: id(1),
				OperationKey: id(2), OperationHash: operationHash[:], InputHash: inputHash[:],
				DestinationCapability: id(3), PassphraseCapability: id(4), CreatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, `DROP TRIGGER IF EXISTS jobs_proto_size_update`); err != nil {
				t.Fatal(err)
			}
			if field == "payload_proto" {
				if _, err := database.ExecContext(ctx, `DROP TRIGGER jobs_immutable_input_guard`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := database.ExecContext(ctx, `UPDATE jobs SET `+field+`=? WHERE id=?`, bytes.Repeat([]byte{0x7f}, 4097), id(0)); err != nil {
				t.Fatal(err)
			}
			observer := &blobProjectionObserver{executor: database}
			if _, err := LoadBackupJob(ctx, observer, id(0)); !errors.Is(err, ErrBackupJob) || observer.rawJobProjections != 0 {
				t.Fatalf("LoadBackupJob error=%v raw projections=%d", err, observer.rawJobProjections)
			}
			observer.rawJobProjections = 0
			if _, err := loadBackupJobByOperation(ctx, observer, id(2)); !errors.Is(err, ErrBackupJob) || observer.rawJobProjections != 0 {
				t.Fatalf("load by operation error=%v raw projections=%d", err, observer.rawJobProjections)
			}
		})
	}
}

func TestBackupJobRecoveryRejectsOversizedRowsBeforeRawProjection(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x29}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "oversized-recovery.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transactions := sqlcipherBackupJobTransactions{database: database}
	now := time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC)
	job := seedQueuedBackupJob(t, database, now, 0x291)
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		_, claimErr := claimBackupJob(ctx, executor, job.ID, now.Add(time.Second))
		return claimErr
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DROP TRIGGER IF EXISTS jobs_proto_size_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE jobs SET progress_proto=? WHERE id=?`, bytes.Repeat([]byte{0x7e}, 4097), job.ID); err != nil {
		t.Fatal(err)
	}
	observer := &blobProjectionObserver{executor: database}
	_, err = ReconstructBackupJobs(ctx, observedBlobTransactions{database: database, observer: observer},
		destinationResolver{}, now.Add(time.Minute))
	if !errors.Is(err, ErrBackupJob) || observer.rawJobProjections != 0 {
		t.Fatalf("recovery error=%v raw job projections=%d", err, observer.rawJobProjections)
	}
}

func TestBackupCheckpointLoadRejectsOversizedBlobBeforeRawProjection(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x2a}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "oversized-checkpoint.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 8, 10, 5, 30, 0, 0, time.UTC)
	base := sqlcipherBackupJobTransactions{database: database}
	job := seedPublishFencedBackupJob(t, base, now, 0x2a1, "018f0000-0000-7000-8000-0000000002a5")
	for _, trigger := range []string{"job_checkpoints_no_update", "job_checkpoints_proto_size_update"} {
		if _, err := database.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+trigger); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE job_checkpoints SET checkpoint_proto=? WHERE job_id=?`,
		bytes.Repeat([]byte{0x7d}, 1025), job.ID); err != nil {
		t.Fatal(err)
	}
	observer := &blobProjectionObserver{executor: database}
	recovered, err := ReconstructBackupJobs(ctx, observedBlobTransactions{database: database, observer: observer},
		destinationResolver{}, now.Add(time.Minute))
	if err != nil || len(recovered) != 1 ||
		recovered[0].State != tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_TERMINAL ||
		observer.rawCheckpointProjects != 0 {
		t.Fatalf("checkpoint recovery=%#v error=%v raw checkpoint projections=%d", recovered, err,
			observer.rawCheckpointProjects)
	}
}

func TestBackupJobCancellationBeforePublish(t *testing.T) {
	ctx := context.Background()
	const (
		workspaceID   = "018f0000-0000-7000-8000-000000000071"
		jobID         = "018f0000-0000-7000-8000-000000000072"
		operationKey  = "018f0000-0000-7000-8000-000000000073"
		destinationID = "018f0000-0000-7000-8000-000000000074"
		signingKeyID  = "018f0000-0000-7000-8000-000000000075"
		passphraseID  = "018f0000-0000-7000-8000-000000000076"
	)
	key := bytes.Repeat([]byte{0x31}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "jobs.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transactions := sqlcipherBackupJobTransactions{database: database}
	seed := sha256.Sum256([]byte("backup job signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	migration := sha256.Sum256([]byte("migrations"))
	lineage := AuditLineage{Generation: 1, Sequence: 0, Head: bytes.Repeat([]byte{0x41}, 32),
		Root: bytes.Repeat([]byte{0x42}, 32), SigningKeyID: signingKeyID, SigningKeyEpoch: 1,
		PublicKey: privateKey.Public().(ed25519.PublicKey)}
	providers, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 1,
		Provider: providerFunc(func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules-revision-1")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	destination := &memoryDestination{reference: destinationID}
	backupService, err := NewService(ServiceConfig{AppVersion: "0.1.0", Providers: providers,
		Snapshots: snapshotSourceFunc(func(context.Context, string, *ProviderRegistry) (CapturedSnapshot, error) {
			return CapturedSnapshot{Workspace: WorkspaceSnapshot{Database: []byte("encrypted-fixed-copy"),
				Header: []byte("header"), SchemaVersion: 3, MigrationManifestHash: migration[:]}, Lineage: lineage,
				ProviderObjects: []Object{{Path: "rules/current.pb", Provider: "rules", ProviderVersion: 1,
					Bytes: []byte("rules-revision-1")}}}, nil
		}), SnapshotPolicy: snapshotPolicyFunc(func(context.Context, WorkspaceSnapshot) error { return nil }),
		Signer: signerFunc(func(_ context.Context, request ManifestSigningRequest) ([]byte, error) {
			return ed25519.Sign(privateKey, request.Statement), nil
		}), Destinations: destinationResolver{destinationID: destination},
		Random: bytes.NewReader(bytes.Repeat([]byte{0x51}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	inputHash := backupJobInputHash(workspaceID, destinationID)
	operationHash := backupJobOperationHash(operationKey, inputHash)
	var queued BackupJobRecord
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var enqueueErr error
		queued, enqueueErr = EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: jobID, WorkspaceID: workspaceID,
			OperationKey: operationKey, OperationHash: operationHash[:], InputHash: inputHash[:],
			DestinationCapability: destinationID, PassphraseCapability: passphraseID, CreatedAt: now})
		return enqueueErr
	}); err != nil {
		t.Fatal(err)
	}
	secret := []byte("correct horse battery staple")
	worker, err := NewBackupJobWorker(BackupJobWorkerConfig{Transactions: transactions, Backups: backupService,
		Passphrases: backupPassphraseProviderFunc(func(_ context.Context, gotJobID string, callback func([]byte) error) error {
			if gotJobID != passphraseID {
				t.Fatalf("passphrase capability requested for %q", gotJobID)
			}
			return callback(secret)
		}), Now: func() time.Time { return now.Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	worker.hooks = &backupJobWorkerHooks{afterPrepared: func() error {
		return transactions.Mutate(ctx, func(executor SQLExecutor) error {
			_, err := CancelBackupJob(ctx, executor, jobID, queued.Version+2, now.Add(2*time.Minute))
			return err
		})
	}}
	projection, err := worker.Run(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_CANCELLED || len(destination.archive) != 0 {
		t.Fatalf("cancelled job=%#v destination bytes=%d", projection, len(destination.archive))
	}
	rows, err := database.QueryContext(ctx, `SELECT semantic_sha256,payload_proto,result_proto,progress_proto FROM jobs WHERE id=?`, jobID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var semantic string
	var payload, result, progress []byte
	if !rows.Next() || rows.Scan(&semantic, &payload, &result, &progress) != nil || rows.Next() || rows.Err() != nil {
		t.Fatal("persisted backup job row unavailable")
	}
	if semantic != hex.EncodeToString(operationHash[:]) || bytes.Contains(payload, secret) || bytes.Contains(result, secret) ||
		bytes.Contains(progress, secret) || bytes.Contains(payload, []byte("encrypted-fixed-copy")) ||
		bytes.Contains(result, []byte("encrypted-fixed-copy")) {
		t.Fatalf("job persisted secret/archive material: semantic=%q payload=%x result=%x progress=%x", semantic, payload, result, progress)
	}
	var checkpoint []byte
	checkpointErr := database.QueryRowContext(ctx,
		`SELECT checkpoint_proto FROM job_checkpoints WHERE job_id=? ORDER BY sequence DESC LIMIT 1`, jobID).Scan(&checkpoint)
	if checkpointErr != nil && !errors.Is(checkpointErr, sql.ErrNoRows) {
		t.Fatal(checkpointErr)
	}
	if bytes.Contains(checkpoint, secret) || bytes.Contains(checkpoint, []byte("encrypted-fixed-copy")) {
		t.Fatalf("checkpoint persisted secret/archive material: %x", checkpoint)
	}
}

func TestBackupJobRestartWithoutPassphraseRequiresReauthorization(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x32}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "restart-jobs.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transactions := sqlcipherBackupJobTransactions{database: database}
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	inputHash := backupJobInputHash("018f0000-0000-7000-8000-000000000082", "018f0000-0000-7000-8000-000000000084")
	operationHash := backupJobOperationHash("018f0000-0000-7000-8000-000000000083", inputHash)
	const jobID = "018f0000-0000-7000-8000-000000000081"
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		_, err := EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: jobID,
			WorkspaceID:  "018f0000-0000-7000-8000-000000000082",
			OperationKey: "018f0000-0000-7000-8000-000000000083", OperationHash: operationHash[:], InputHash: inputHash[:],
			DestinationCapability: "018f0000-0000-7000-8000-000000000084",
			PassphraseCapability:  "018f0000-0000-7000-8000-000000000085", CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		_, err := claimBackupJob(ctx, executor, jobID, now.Add(time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReconstructBackupJobs(ctx, transactions, destinationResolver{}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].State != tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_TERMINAL ||
		recovered[0].Progress.Stage != "SECRET_REAUTHORIZATION_UNAVAILABLE" {
		t.Fatalf("reconstructed jobs = %#v", recovered)
	}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		_, claimErr := claimBackupJob(ctx, executor, jobID, now.Add(3*time.Minute))
		return claimErr
	}); !errors.Is(err, ErrBackupJob) {
		t.Fatalf("ordinary rerun error=%v, want terminal ErrBackupJob", err)
	}
	newInputHash := backupJobInputHash("018f0000-0000-7000-8000-000000000082", "018f0000-0000-7000-8000-000000000084")
	newOperationHash := backupJobOperationHash("018f0000-0000-7000-8000-000000000087", newInputHash)
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		newJob, enqueueErr := EnqueueBackupJob(ctx, executor, BackupJobSpec{
			ID: "018f0000-0000-7000-8000-000000000086", WorkspaceID: "018f0000-0000-7000-8000-000000000082",
			OperationKey: "018f0000-0000-7000-8000-000000000087", OperationHash: newOperationHash[:], InputHash: newInputHash[:],
			DestinationCapability: "018f0000-0000-7000-8000-000000000084",
			PassphraseCapability:  "018f0000-0000-7000-8000-000000000088", CreatedAt: now.Add(4 * time.Minute)})
		if enqueueErr != nil || newJob.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_QUEUED {
			return ErrBackupJob
		}
		return nil
	}); err != nil {
		t.Fatalf("new independently authorized job: %v", err)
	}
}

func TestBackupJobDeathAfterPublishReconstructsFromDestinationHash(t *testing.T) {
	ctx := context.Background()
	const (
		workspaceID   = "018f0000-0000-7000-8000-000000000091"
		jobID         = "018f0000-0000-7000-8000-000000000092"
		operationKey  = "018f0000-0000-7000-8000-000000000093"
		destinationID = "018f0000-0000-7000-8000-000000000094"
		signingKeyID  = "018f0000-0000-7000-8000-000000000095"
		passphraseID  = "018f0000-0000-7000-8000-000000000096"
	)
	key := bytes.Repeat([]byte{0x33}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "publish-recovery.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transactions := sqlcipherBackupJobTransactions{database: database}
	seed := sha256.Sum256([]byte("publish recovery signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	migration := sha256.Sum256([]byte("publish recovery migrations"))
	lineage := AuditLineage{Generation: 2, Sequence: 0, Head: bytes.Repeat([]byte{0x61}, 32),
		Root: bytes.Repeat([]byte{0x62}, 32), SigningKeyID: signingKeyID, SigningKeyEpoch: 1,
		PublicKey: privateKey.Public().(ed25519.PublicKey)}
	providers, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 1,
		Provider: providerFunc(func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	destination := &memoryDestination{reference: destinationID}
	backupService, err := NewService(ServiceConfig{AppVersion: "0.1.0", Providers: providers,
		Snapshots: snapshotSourceFunc(func(context.Context, string, *ProviderRegistry) (CapturedSnapshot, error) {
			return CapturedSnapshot{Workspace: WorkspaceSnapshot{Database: []byte("fixed encrypted database"), Header: []byte("header"),
				SchemaVersion: 3, MigrationManifestHash: migration[:]}, Lineage: lineage,
				ProviderObjects: []Object{{Path: "rules/current.pb", Provider: "rules", ProviderVersion: 1, Bytes: []byte("rules")}}}, nil
		}), SnapshotPolicy: snapshotPolicyFunc(func(context.Context, WorkspaceSnapshot) error { return nil }),
		Signer: signerFunc(func(_ context.Context, request ManifestSigningRequest) ([]byte, error) {
			return ed25519.Sign(privateKey, request.Statement), nil
		}), Destinations: destinationResolver{destinationID: destination}, Random: bytes.NewReader(bytes.Repeat([]byte{0x63}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)
	inputHash := backupJobInputHash(workspaceID, destinationID)
	operationHash := backupJobOperationHash(operationKey, inputHash)
	var job BackupJobRecord
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: jobID, WorkspaceID: workspaceID,
			OperationKey: operationKey, OperationHash: operationHash[:], InputHash: inputHash[:],
			DestinationCapability: destinationID, PassphraseCapability: passphraseID, CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = claimBackupJob(ctx, executor, jobID, now.Add(time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := backupService.prepare(ctx, CreateRequest{WorkspaceID: workspaceID,
		DestinationCapability: destinationID, Passphrase: []byte("correct horse battery staple")})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = persistPreparedBackupJob(ctx, executor, job, prepared, now.Add(2*time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var shouldPublish bool
		var err error
		job, shouldPublish, err = beginBackupPublish(ctx, executor, jobID, now.Add(3*time.Minute))
		if err == nil && !shouldPublish {
			return ErrBackupJob
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	published, err := backupService.publish(context.WithoutCancel(ctx), prepared)
	if err != nil || !bytes.Equal(published.DestinationHash, prepared.archiveHash[:]) {
		t.Fatalf("publish result=%#v error=%v", published, err)
	}
	// Process dies here: destination bytes exist, but the SQL row is still
	// RUNNING at the irreversible publish fence.
	recovered, err := ReconstructBackupJobs(ctx, transactions,
		destinationResolver{destinationID: destination}, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].State != tammyv1.BackupJobState_BACKUP_JOB_STATE_COMPLETED ||
		!bytes.Equal(recovered[0].ManifestHash, prepared.manifestHash) {
		t.Fatalf("post-publish reconstruction = %#v", recovered)
	}
}

func TestBackupJobCancellationAfterPublishFenceConflicts(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x34}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "publish-fence.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transactions := sqlcipherBackupJobTransactions{database: database}
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	inputHash := backupJobInputHash("018f0000-0000-7000-8000-0000000000a2", "018f0000-0000-7000-8000-0000000000a4")
	operationHash := backupJobOperationHash("018f0000-0000-7000-8000-0000000000a3", inputHash)
	const jobID = "018f0000-0000-7000-8000-0000000000a1"
	var job BackupJobRecord
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: jobID,
			WorkspaceID:  "018f0000-0000-7000-8000-0000000000a2",
			OperationKey: "018f0000-0000-7000-8000-0000000000a3", OperationHash: operationHash[:], InputHash: inputHash[:],
			DestinationCapability: "018f0000-0000-7000-8000-0000000000a4",
			PassphraseCapability:  "018f0000-0000-7000-8000-0000000000a5", CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = claimBackupJob(ctx, executor, jobID, now.Add(time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	archive := []byte("encrypted archive bytes")
	archiveHash := sha256.Sum256(archive)
	prepared := &preparedBackup{workspaceID: job.Input.WorkspaceId,
		destinationCapability: job.Input.DestinationCapability, archive: archive,
		manifestHash: bytes.Repeat([]byte{0x75}, sha256.Size), archiveHash: archiveHash}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = persistPreparedBackupJob(ctx, executor, job, prepared, now.Add(2*time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var shouldPublish bool
		var err error
		job, shouldPublish, err = beginBackupPublish(ctx, executor, jobID, now.Add(3*time.Minute))
		if err == nil && !shouldPublish {
			return ErrBackupJob
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err = transactions.Mutate(ctx, func(executor SQLExecutor) error {
		_, cancelErr := CancelBackupJob(ctx, executor, jobID, job.Version, now.Add(4*time.Minute))
		return cancelErr
	})
	if !errors.Is(err, ErrBackupJobConflict) {
		t.Fatalf("post-fence cancellation error=%v, want ErrBackupJobConflict", err)
	}
	unchanged, err := LoadBackupJob(ctx, database, jobID)
	if err != nil || unchanged.Version != job.Version || !unchanged.CommitPointReached ||
		unchanged.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING {
		t.Fatalf("post-fence job changed: %#v error=%v", unchanged, err)
	}
}

type backupRecoveryDestination struct {
	reference               string
	contents                []byte
	readErr                 error
	reads                   int
	callbackDepth           *int
	readObservedTransaction bool
}

func (destination *backupRecoveryDestination) Reference() string { return destination.reference }
func (*backupRecoveryDestination) AtomicCommit(context.Context, []byte) error {
	return errors.New("unexpected recovery write")
}

func (destination *backupRecoveryDestination) ReadCommitted(context.Context) ([]byte, error) {
	destination.reads++
	if destination.callbackDepth != nil && *destination.callbackDepth != 0 {
		destination.readObservedTransaction = true
	}
	return append([]byte(nil), destination.contents...), destination.readErr
}

func TestBackupJobRecoveryIsBoundedAndStable(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x4f}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "bounded-backup-recovery.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	baseTransactions := sqlcipherBackupJobTransactions{database: database}
	createdAt := time.Date(2026, time.August, 10, 11, 0, 0, 0, time.UTC)
	workspaceID := "018f6000-0000-7000-8000-000000000001"
	destinationID := "018f6000-0000-7000-8000-000000000002"
	archive := []byte("expected encrypted archive")
	archiveHash := sha256.Sum256(archive)
	checkpoint := &tammyv1.BackupJobCheckpoint{Format: "tammy-backup-job-checkpoint-v1",
		ManifestHash: bytes.Repeat([]byte{0x76}, sha256.Size), ArchiveHash: archiveHash[:]}
	checkpointBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	checkpointHash := sha256.Sum256(checkpointBytes)
	jobIDs := make([]string, 257)
	if err := baseTransactions.Mutate(ctx, func(executor SQLExecutor) error {
		for index := range jobIDs {
			jobIDs[index] = fmt.Sprintf("018f6100-0000-7000-8000-%012x", index+1)
			operationKey := fmt.Sprintf("018f6200-0000-7000-8000-%012x", index+1)
			passphraseID := fmt.Sprintf("018f6300-0000-7000-8000-%012x", index+1)
			inputHash := backupJobInputHash(workspaceID, destinationID)
			operationHash := backupJobOperationHash(operationKey, inputHash)
			job, enqueueErr := EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: jobIDs[index], WorkspaceID: workspaceID,
				OperationKey: operationKey, OperationHash: operationHash[:], InputHash: inputHash[:],
				DestinationCapability: destinationID, PassphraseCapability: passphraseID, CreatedAt: createdAt})
			if enqueueErr != nil {
				return enqueueErr
			}
			job, claimErr := claimBackupJob(ctx, executor, job.ID, createdAt.Add(time.Second))
			if claimErr != nil {
				return claimErr
			}
			if _, insertErr := executor.ExecContext(ctx, `INSERT INTO job_checkpoints(
				job_id,sequence,checkpoint_proto,checkpoint_sha256,committed_at) VALUES(?,1,?,?,?)`, job.ID,
				checkpointBytes, hex.EncodeToString(checkpointHash[:]), createdAt.UTC().Format(time.RFC3339Nano)); insertErr != nil {
				return insertErr
			}
			result, updateErr := executor.ExecContext(ctx, `UPDATE jobs SET commit_point_reached=1,progress_proto=?,version=version+1
				WHERE id=? AND version=?`, marshalJobProgress("PUBLISHING"), job.ID, job.Version)
			if updateErr != nil || exactlyOne(result) != nil {
				return ErrBackupJob
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	readCalls, mutationCalls, callbackDepth := 0, 0, 0
	transactions := sqlcipherBackupJobTransactions{database: database, readCalls: &readCalls,
		mutationCalls: &mutationCalls, callbackDepth: &callbackDepth}
	destination := &backupRecoveryDestination{reference: destinationID, contents: archive, callbackDepth: &callbackDepth}
	resolver := recoveryDestinationResolver{destination: destination}
	first, err := ReconstructBackupJobs(ctx, transactions, resolver, createdAt.Add(time.Minute))
	if err != nil || len(first) != 256 || first[0].Id != jobIDs[0] || first[255].Id != jobIDs[255] ||
		destination.reads != 256 || destination.readObservedTransaction || readCalls != 257 || mutationCalls != 256 {
		t.Fatalf("first batch=%d range=%v..%v reads=%d read_tx=%t tx=%d/%d error=%v", len(first),
			first[0].Id, first[len(first)-1].Id, destination.reads, destination.readObservedTransaction,
			readCalls, mutationCalls, err)
	}
	remaining, err := LoadBackupJob(ctx, database, jobIDs[256])
	if err != nil || remaining.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING {
		t.Fatalf("remaining job=%#v error=%v", remaining, err)
	}
	second, err := ReconstructBackupJobs(ctx, transactions, resolver, createdAt.Add(2*time.Minute))
	if err != nil || len(second) != 1 || second[0].Id != jobIDs[256] || destination.reads != 257 ||
		destination.readObservedTransaction || readCalls != 259 || mutationCalls != 257 {
		t.Fatalf("second batch=%#v reads=%d read_tx=%t tx=%d/%d error=%v", second,
			destination.reads, destination.readObservedTransaction, readCalls, mutationCalls, err)
	}
}

func TestBackupJobDestinationRecoveryFailuresRequireReapproval(t *testing.T) {
	tests := []struct {
		name        string
		destination func(string) DestinationResolver
	}{
		{name: "missing", destination: func(string) DestinationResolver { return destinationResolver{} }},
		{name: "read_error", destination: func(reference string) DestinationResolver {
			return recoveryDestinationResolver{destination: &backupRecoveryDestination{reference: reference, readErr: errors.New("read failed")}}
		}},
		{name: "hash_mismatch", destination: func(reference string) DestinationResolver {
			return recoveryDestinationResolver{destination: &backupRecoveryDestination{reference: reference, contents: []byte("wrong archive")}}
		}},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			key := bytes.Repeat([]byte{byte(0x40 + index)}, sqlcipher.KeySize)
			defer zero(key)
			path := filepath.Join(t.TempDir(), "destination-recovery.db")
			if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
				t.Fatal(err)
			}
			database, err := sqlcipher.Open(ctx, path, key)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			transactions := sqlcipherBackupJobTransactions{database: database}
			now := time.Date(2026, 8, 9, 6, index, 0, 0, time.UTC)
			reference := fmt.Sprintf("018f0000-0000-7000-8000-%012x", 0xb4+index*10)
			job := seedPublishFencedBackupJob(t, transactions, now, 0xb1+index*10, reference)
			recovered, err := ReconstructBackupJobs(ctx, transactions, testCase.destination(reference), now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if len(recovered) != 1 || recovered[0].State != tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_TERMINAL ||
				recovered[0].Progress.Stage != "DESTINATION_REAUTHORIZATION_UNAVAILABLE" || recovered[0].ManifestHash != nil {
				t.Fatalf("recovery failure projection = %#v", recovered)
			}
			stored, err := LoadBackupJob(ctx, database, job.ID)
			if err != nil || stored.Result != nil || !stored.CommitPointReached {
				t.Fatalf("recovery failure row = %#v, %v", stored, err)
			}
		})
	}
}

func TestBackupJobRecoveryRejectsInvalidCheckpoint(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(context.Context, *sqlcipher.Database, string) error
	}{
		{name: "missing", tamper: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			if _, err := database.ExecContext(ctx, `DROP TRIGGER job_checkpoints_no_delete`); err != nil {
				return err
			}
			_, err := database.ExecContext(ctx, `DELETE FROM job_checkpoints WHERE job_id=?`, jobID)
			return err
		}},
		{name: "noncanonical", tamper: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			if _, err := database.ExecContext(ctx, `DROP TRIGGER job_checkpoints_no_update`); err != nil {
				return err
			}
			var encoded []byte
			if err := database.QueryRowContext(ctx, `SELECT checkpoint_proto FROM job_checkpoints WHERE job_id=?`, jobID).Scan(&encoded); err != nil {
				return err
			}
			encoded = protowire.AppendTag(encoded, 100, protowire.VarintType)
			encoded = protowire.AppendVarint(encoded, 1)
			digest := sha256.Sum256(encoded)
			_, err := database.ExecContext(ctx, `UPDATE job_checkpoints SET checkpoint_proto=?,checkpoint_sha256=? WHERE job_id=?`,
				encoded, hex.EncodeToString(digest[:]), jobID)
			return err
		}},
		{name: "tampered_hash", tamper: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			if _, err := database.ExecContext(ctx, `DROP TRIGGER job_checkpoints_no_update`); err != nil {
				return err
			}
			_, err := database.ExecContext(ctx, `UPDATE job_checkpoints SET checkpoint_sha256=? WHERE job_id=?`,
				bytes.Repeat([]byte{'0'}, 64), jobID)
			return err
		}},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			key := bytes.Repeat([]byte{byte(0x50 + index)}, sqlcipher.KeySize)
			defer zero(key)
			path := filepath.Join(t.TempDir(), "checkpoint-recovery.db")
			if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
				t.Fatal(err)
			}
			database, err := sqlcipher.Open(ctx, path, key)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			transactions := sqlcipherBackupJobTransactions{database: database}
			now := time.Date(2026, 8, 9, 7, index, 0, 0, time.UTC)
			reference := fmt.Sprintf("018f0000-0000-7000-8000-%012x", 0xc4+index*10)
			job := seedPublishFencedBackupJob(t, transactions, now, 0xc1+index*10, reference)
			if err := testCase.tamper(ctx, database, job.ID); err != nil {
				t.Fatal(err)
			}
			recovered, err := ReconstructBackupJobs(ctx, transactions, destinationResolver{}, now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if len(recovered) != 1 || recovered[0].State != tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_TERMINAL ||
				recovered[0].Progress.Stage != "RECOVERY_STATE_INVALID" || recovered[0].ManifestHash != nil {
				t.Fatalf("invalid-checkpoint projection = %#v", recovered)
			}
		})
	}
}

func TestBackupJobLoadRejectsForgedPersistedState(t *testing.T) {
	tests := []struct {
		name  string
		forge func(context.Context, *sqlcipher.Database, string) error
	}{
		{name: "completed_without_result", forge: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			_, err := database.ExecContext(ctx, `UPDATE jobs SET state='COMPLETED',completed_at=updated_at WHERE id=?`, jobID)
			return err
		}},
		{name: "running_with_result_before_commit_point", forge: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			var payload []byte
			if err := database.QueryRowContext(ctx, `SELECT payload_proto FROM jobs WHERE id=?`, jobID).Scan(&payload); err != nil {
				return err
			}
			input := &tammyv1.BackupJobInput{}
			if err := proto.Unmarshal(payload, input); err != nil {
				return err
			}
			result, err := proto.MarshalOptions{Deterministic: true}.Marshal(&tammyv1.BackupJobResult{
				Format: "tammy-backup-job-result-v1", ManifestHash: bytes.Repeat([]byte{1}, 32),
				DestinationHash: bytes.Repeat([]byte{2}, 32), DestinationCapability: input.DestinationCapability})
			if err != nil {
				return err
			}
			_, err = database.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',result_proto=? WHERE id=?`, result, jobID)
			return err
		}},
		{name: "version_zero", forge: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			if _, err := database.ExecContext(ctx, `PRAGMA ignore_check_constraints=ON`); err != nil {
				return err
			}
			_, err := database.ExecContext(ctx, `UPDATE jobs SET version=0 WHERE id=?`, jobID)
			return err
		}},
		{name: "operation_hash", forge: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			if _, err := database.ExecContext(ctx, `DROP TRIGGER jobs_immutable_input_guard`); err != nil {
				return err
			}
			_, err := database.ExecContext(ctx, `UPDATE jobs SET semantic_sha256=? WHERE id=?`,
				hex.EncodeToString(bytes.Repeat([]byte{0xee}, 32)), jobID)
			return err
		}},
		{name: "input_hash", forge: func(ctx context.Context, database *sqlcipher.Database, jobID string) error {
			if _, err := database.ExecContext(ctx, `DROP TRIGGER jobs_immutable_input_guard`); err != nil {
				return err
			}
			var encoded []byte
			if err := database.QueryRowContext(ctx, `SELECT payload_proto FROM jobs WHERE id=?`, jobID).Scan(&encoded); err != nil {
				return err
			}
			input := &tammyv1.BackupJobInput{}
			if err := proto.Unmarshal(encoded, input); err != nil {
				return err
			}
			input.InputHash = bytes.Repeat([]byte{0xef}, 32)
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
			if err != nil {
				return err
			}
			_, err = database.ExecContext(ctx, `UPDATE jobs SET payload_proto=? WHERE id=?`, encoded, jobID)
			return err
		}},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			key := bytes.Repeat([]byte{byte(0x60 + index)}, sqlcipher.KeySize)
			defer zero(key)
			path := filepath.Join(t.TempDir(), "forged-job.db")
			if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
				t.Fatal(err)
			}
			database, err := sqlcipher.Open(ctx, path, key)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			transactions := sqlcipherBackupJobTransactions{database: database}
			now := time.Date(2026, 8, 9, 8, index, 0, 0, time.UTC)
			id := func(offset int) string { return fmt.Sprintf("018f0000-0000-7000-8000-%012x", 0xd1+index*10+offset) }
			inputHash := backupJobInputHash(id(1), id(3))
			operationHash := backupJobOperationHash(id(2), inputHash)
			if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
				_, err := EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: id(0), WorkspaceID: id(1), OperationKey: id(2),
					OperationHash: operationHash[:], InputHash: inputHash[:], DestinationCapability: id(3),
					PassphraseCapability: id(4), CreatedAt: now})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := testCase.forge(ctx, database, id(0)); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadBackupJob(ctx, database, id(0)); !errors.Is(err, ErrBackupJob) {
				t.Fatalf("forged job load error=%v, want ErrBackupJob", err)
			}
		})
	}
}

func TestBackupJobCanonicalHashesAndReplay(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x70}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "job-replay.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	const (
		jobID         = "018f0000-0000-7000-8000-0000000000e1"
		workspaceID   = "018f0000-0000-7000-8000-0000000000e2"
		operationKey  = "018f0000-0000-7000-8000-0000000000e3"
		destinationID = "018f0000-0000-7000-8000-0000000000e4"
		passphraseID  = "018f0000-0000-7000-8000-0000000000e5"
	)
	now := time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)
	inputHash := backupJobInputHash(workspaceID, destinationID)
	operationHash := backupJobOperationHash(operationKey, inputHash)
	spec := BackupJobSpec{ID: jobID, WorkspaceID: workspaceID, OperationKey: operationKey,
		OperationHash: operationHash[:], InputHash: inputHash[:], DestinationCapability: destinationID,
		PassphraseCapability: passphraseID, CreatedAt: now}
	first, err := EnqueueBackupJob(ctx, database, spec)
	if err != nil {
		t.Fatal(err)
	}
	wrongInput := spec
	wrongInput.ID = "018f0000-0000-7000-8000-0000000000e6"
	wrongInput.InputHash = bytes.Repeat([]byte{0xff}, 32)
	if _, err := EnqueueBackupJob(ctx, database, wrongInput); !errors.Is(err, ErrBackupJob) {
		t.Fatalf("wrong input hash error=%v", err)
	}
	wrongOperation := spec
	wrongOperation.ID = "018f0000-0000-7000-8000-0000000000e7"
	wrongOperation.OperationHash = bytes.Repeat([]byte{0xfe}, 32)
	if _, err := EnqueueBackupJob(ctx, database, wrongOperation); !errors.Is(err, ErrBackupJob) {
		t.Fatalf("wrong operation hash error=%v", err)
	}
	exactReplay := spec
	exactReplay.ID = "018f0000-0000-7000-8000-0000000000e8"
	exactReplay.PassphraseCapability = "018f0000-0000-7000-8000-0000000000e9"
	replayed, err := EnqueueBackupJob(ctx, database, exactReplay)
	if err != nil || replayed.ID != first.ID || replayed.Version != first.Version {
		t.Fatalf("exact replay=%#v error=%v, want original %#v", replayed, err, first)
	}
	changedDestination := spec
	changedDestination.ID = "018f0000-0000-7000-8000-0000000000ea"
	changedDestination.DestinationCapability = "018f0000-0000-7000-8000-0000000000eb"
	changedDestinationInput := backupJobInputHash(workspaceID, changedDestination.DestinationCapability)
	changedDestination.InputHash = changedDestinationInput[:]
	changedDestinationOperation := backupJobOperationHash(operationKey, changedDestinationInput)
	changedDestination.OperationHash = changedDestinationOperation[:]
	if _, err := EnqueueBackupJob(ctx, database, changedDestination); !errors.Is(err, ErrBackupJobConflict) {
		t.Fatalf("changed destination replay error=%v", err)
	}
	changedWorkspace := spec
	changedWorkspace.ID = "018f0000-0000-7000-8000-0000000000ec"
	changedWorkspace.WorkspaceID = "018f0000-0000-7000-8000-0000000000ed"
	changedWorkspaceInput := backupJobInputHash(changedWorkspace.WorkspaceID, destinationID)
	changedWorkspace.InputHash = changedWorkspaceInput[:]
	changedWorkspaceOperation := backupJobOperationHash(operationKey, changedWorkspaceInput)
	changedWorkspace.OperationHash = changedWorkspaceOperation[:]
	if _, err := EnqueueBackupJob(ctx, database, changedWorkspace); !errors.Is(err, ErrBackupJobConflict) {
		t.Fatalf("changed workspace replay error=%v", err)
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE operation_key=?`, operationKey).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replay rows=%d error=%v", count, err)
	}
}

type observedBackupJobTransactions struct {
	inner  sqlcipherBackupJobTransactions
	active atomic.Bool
}

func (transactions *observedBackupJobTransactions) Read(ctx context.Context, callback func(SQLExecutor) error) error {
	if !transactions.active.CompareAndSwap(false, true) {
		return errors.New("nested SQL read")
	}
	defer transactions.active.Store(false)
	return transactions.inner.Read(ctx, callback)
}

func (transactions *observedBackupJobTransactions) Mutate(ctx context.Context, callback func(SQLExecutor) error) error {
	if !transactions.active.CompareAndSwap(false, true) {
		return errors.New("nested SQL mutation")
	}
	defer transactions.active.Store(false)
	return transactions.inner.Mutate(ctx, callback)
}

type transactionAwareDestination struct {
	reference string
	active    *atomic.Bool
	contents  []byte
}

func (destination *transactionAwareDestination) Reference() string { return destination.reference }
func (destination *transactionAwareDestination) AtomicCommit(_ context.Context, contents []byte) error {
	if destination.active.Load() {
		return errors.New("SQL active during destination commit")
	}
	destination.contents = append([]byte(nil), contents...)
	return nil
}
func (destination *transactionAwareDestination) ReadCommitted(context.Context) ([]byte, error) {
	if destination.active.Load() {
		return nil, errors.New("SQL active during destination read")
	}
	return append([]byte(nil), destination.contents...), nil
}

func TestBackupJobWorkerNeverSpansSQLAcrossPrepareOrPublish(t *testing.T) {
	ctx := context.Background()
	const (
		workspaceID   = "018f0000-0000-7000-8000-000000000101"
		jobID         = "018f0000-0000-7000-8000-000000000102"
		operationKey  = "018f0000-0000-7000-8000-000000000103"
		destinationID = "018f0000-0000-7000-8000-000000000104"
		passphraseID  = "018f0000-0000-7000-8000-000000000105"
		signingKeyID  = "018f0000-0000-7000-8000-000000000106"
	)
	key := bytes.Repeat([]byte{0x71}, sqlcipher.KeySize)
	defer zero(key)
	path := filepath.Join(t.TempDir(), "transaction-boundary.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transactions := &observedBackupJobTransactions{inner: sqlcipherBackupJobTransactions{database: database}}
	destination := &transactionAwareDestination{reference: destinationID, active: &transactions.active}
	resolver := recoveryDestinationResolver{destination: destination}
	seed := sha256.Sum256([]byte("transaction boundary signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	migration := sha256.Sum256([]byte("transaction boundary migrations"))
	lineage := AuditLineage{Generation: 1, Head: bytes.Repeat([]byte{0x72}, 32), Root: bytes.Repeat([]byte{0x73}, 32),
		SigningKeyID: signingKeyID, SigningKeyEpoch: 1, PublicKey: privateKey.Public().(ed25519.PublicKey)}
	providers, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 1,
		Provider: providerFunc(func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ServiceConfig{AppVersion: "0.1.0", Providers: providers,
		Snapshots: snapshotSourceFunc(func(context.Context, string, *ProviderRegistry) (CapturedSnapshot, error) {
			if transactions.active.Load() {
				return CapturedSnapshot{}, errors.New("SQL active during snapshot prepare")
			}
			return CapturedSnapshot{Workspace: WorkspaceSnapshot{Database: []byte("fixed encrypted database"), Header: []byte("header"),
				SchemaVersion: 3, MigrationManifestHash: migration[:]}, Lineage: lineage,
				ProviderObjects: []Object{{Path: "rules/current.pb", Provider: "rules", ProviderVersion: 1, Bytes: []byte("rules")}}}, nil
		}), SnapshotPolicy: snapshotPolicyFunc(func(context.Context, WorkspaceSnapshot) error { return nil }),
		Signer: signerFunc(func(_ context.Context, request ManifestSigningRequest) ([]byte, error) {
			if transactions.active.Load() {
				return nil, errors.New("SQL active during manifest signing")
			}
			return ed25519.Sign(privateKey, request.Statement), nil
		}), Destinations: resolver, Random: bytes.NewReader(bytes.Repeat([]byte{0x74}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC)
	inputHash := backupJobInputHash(workspaceID, destinationID)
	operationHash := backupJobOperationHash(operationKey, inputHash)
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		_, err := EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: jobID, WorkspaceID: workspaceID, OperationKey: operationKey,
			OperationHash: operationHash[:], InputHash: inputHash[:], DestinationCapability: destinationID,
			PassphraseCapability: passphraseID, CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := NewBackupJobWorker(BackupJobWorkerConfig{Transactions: transactions, Backups: service,
		Passphrases: backupPassphraseProviderFunc(func(_ context.Context, _ string, callback func([]byte) error) error {
			return callback([]byte("correct horse battery staple"))
		}), Now: func() time.Time { return now.Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := worker.Run(ctx, jobID)
	if err != nil || completed.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_COMPLETED || len(destination.contents) == 0 {
		t.Fatalf("transaction-boundary worker=%#v error=%v destination=%d", completed, err, len(destination.contents))
	}
}

type recoveryDestinationResolver struct {
	destination Destination
}

func (resolver recoveryDestinationResolver) Resolve(string) (Destination, error) {
	return resolver.destination, nil
}

func seedPublishFencedBackupJob(
	t *testing.T,
	transactions sqlcipherBackupJobTransactions,
	now time.Time,
	idBase int,
	destinationCapability string,
) BackupJobRecord {
	t.Helper()
	ctx := context.Background()
	id := func(offset int) string { return fmt.Sprintf("018f0000-0000-7000-8000-%012x", idBase+offset) }
	inputHash := backupJobInputHash(id(1), destinationCapability)
	operationHash := backupJobOperationHash(id(2), inputHash)
	var job BackupJobRecord
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = EnqueueBackupJob(ctx, executor, BackupJobSpec{ID: id(0), WorkspaceID: id(1), OperationKey: id(2),
			OperationHash: operationHash[:], InputHash: inputHash[:], DestinationCapability: destinationCapability,
			PassphraseCapability: id(4), CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var err error
		job, err = claimBackupJob(ctx, executor, job.ID, now.Add(time.Second))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	archiveHash := sha256.Sum256([]byte("expected encrypted archive"))
	checkpoint := &tammyv1.BackupJobCheckpoint{Format: "tammy-backup-job-checkpoint-v1",
		ManifestHash: bytes.Repeat([]byte{0x76}, sha256.Size), ArchiveHash: archiveHash[:]}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	checkpointHash := sha256.Sum256(encoded)
	if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
		if _, err := executor.ExecContext(ctx, `INSERT INTO job_checkpoints(job_id,sequence,checkpoint_proto,checkpoint_sha256,committed_at)
			VALUES(?,1,?,?,?)`, job.ID, encoded, hex.EncodeToString(checkpointHash[:]), now.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `UPDATE jobs SET commit_point_reached=1,progress_proto=?,version=version+1
			WHERE id=? AND version=?`, marshalJobProgress("PUBLISHING"), job.ID, job.Version); err != nil {
			return err
		}
		var loadErr error
		job, loadErr = LoadBackupJob(ctx, executor, job.ID)
		return loadErr
	}); err != nil {
		t.Fatal(err)
	}
	return job
}

func seedQueuedBackupJob(t *testing.T, executor SQLExecutor, now time.Time, idBase int) BackupJobRecord {
	t.Helper()
	id := func(offset int) string { return fmt.Sprintf("018f0000-0000-7000-8000-%012x", idBase+offset) }
	inputHash := backupJobInputHash(id(1), id(3))
	operationHash := backupJobOperationHash(id(2), inputHash)
	job, err := EnqueueBackupJob(context.Background(), executor, BackupJobSpec{ID: id(0), WorkspaceID: id(1),
		OperationKey: id(2), OperationHash: operationHash[:], InputHash: inputHash[:],
		DestinationCapability: id(3), PassphraseCapability: id(4), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return job
}
