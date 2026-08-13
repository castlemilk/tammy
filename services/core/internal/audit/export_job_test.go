//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAuditExportJobPersistsFencedLifecycleAndCancelsBeforeRename(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC)
	archive, key := buildEvidenceArchiveFixtureWithKey(t)
	persistExportTestKey(t, ctx, database, key)
	spec := ExportJobSpec{
		ID: "01890f60-4d6d-7c12-8f02-6c9129d5b010", WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b011", OperationHash: bytes.Repeat([]byte{0x61}, 32),
		InputHash: bytes.Repeat([]byte{0x62}, 32), Filter: &tammyv1.AuditEventFilter{}, SnapshotGeneration: 1,
		SnapshotSequence: 1, SnapshotHead: bytes.Repeat([]byte{0x63}, 32), DestinationProvider: "approved_file",
		EvidenceProvider:      "audit_chain",
		DestinationCapability: "01890f60-4d6d-7c12-8f02-6c9129d5b012",
		Progress:              &tammyv1.JobProgress{Stage: "COLLECTING", TotalUnits: 1}, CreatedAt: now,
	}
	rolledBack, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if _, err := EnqueueExportJob(ctx, rolledBack, spec); err != nil {
		t.Fatal(err)
	}
	if err := rolledBack.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_export_jobs_v1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("enqueue committed independently: count=%d err=%v", count, err)
	}
	enqueue, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err := EnqueueExportJob(ctx, enqueue, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := enqueue.Commit(); err != nil {
		t.Fatal(err)
	}
	claim, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err = ClaimExportJob(ctx, claim, job.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Commit(); err != nil {
		t.Fatal(err)
	}
	if job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_RUNNING || job.Version != 2 || job.Attempt != 1 {
		t.Fatalf("claimed job = %#v", job)
	}
	authorize, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err = AuthorizeExportDestination(ctx, authorize, job, archive, "01890f60-4d6d-7c12-8f02-6c9129d5b012",
		&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", CompletedUnits: 1, TotalUnits: 1}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := authorize.Commit(); err != nil {
		t.Fatal(err)
	}
	cancel, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err := RequestExportCancellation(ctx, cancel, job.ID, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := cancel.Commit(); err != nil {
		t.Fatal(err)
	}
	destination := &memoryExportDestination{reference: job.ResultRef}
	sqlActive := false
	transactions := &auditServiceTransactions{database: database, workspaceID: job.WorkspaceID, sqlActive: &sqlActive}
	destination.sqlActive = &sqlActive
	job, err = CommitAuthorizedExport(ctx, transactions, writableAuditGate(), job.ID, archive, destination, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED || destination.commitCalls != 0 {
		t.Fatalf("cancelled job=%#v destination calls=%d", job, destination.commitCalls)
	}
}

func TestAuditExportCancellationRejectsCompletedAndCommitPointJobsWithoutMutation(t *testing.T) {
	testCases := []struct {
		name  string
		jobID string
		state string
		stage string
	}{
		{name: "completed", jobID: "01890f60-4d6d-7c12-8f02-6c9129d5b013", state: "COMPLETED", stage: "COMPLETED"},
		{name: "destination committing", jobID: "01890f60-4d6d-7c12-8f02-6c9129d5b016", state: "RUNNING", stage: "DESTINATION_COMMITTING"},
		{name: "commit destination reapproval", jobID: "01890f60-4d6d-7c12-8f02-6c9129d5b019", state: "WAITING_FOR_INPUT", stage: "COMMIT_DESTINATION_REAPPROVAL"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			now := time.Date(2026, 8, 4, 4, 30, 0, 0, time.UTC)
			job := createClaimedExportJob(t, ctx, database, testCase.jobID, now)
			completedAt := any(nil)
			if testCase.state == "COMPLETED" {
				completedAt = formatTimestamp(now.Add(time.Minute))
			}
			if _, err := database.ExecContext(ctx, `UPDATE audit_export_jobs_v1 SET state=?, stage=?, completed_at=?, updated_at=?, version=version+1 WHERE id=?`,
				testCase.state, testCase.stage, completedAt, formatTimestamp(now.Add(time.Minute)), job.ID); err != nil {
				t.Fatal(err)
			}
			before, err := LoadExportJob(ctx, database, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			cancel, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
			if err != nil {
				t.Fatal(err)
			}
			cancelErr := RequestExportCancellation(ctx, cancel, job.ID, now.Add(2*time.Minute))
			if !errors.Is(cancelErr, ErrExportCommitAlreadyCompleted) {
				_ = cancel.Rollback()
				t.Fatalf("cancel error=%v, want COMMIT_ALREADY_COMPLETED", cancelErr)
			}
			if err := cancel.Commit(); err != nil {
				t.Fatal(err)
			}
			after, err := LoadExportJob(ctx, database, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Version != before.Version || after.State != before.State || after.Stage != before.Stage ||
				after.CancellationRequested || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("protected job mutated: before=%#v after=%#v", before, after)
			}
		})
	}
}

func TestAuditExportJobStartupReconstructsAndRecoversCommittedDestination(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC)
	archive, key := buildEvidenceArchiveFixtureWithKey(t)
	persistExportTestKey(t, ctx, database, key)
	job := createClaimedExportJob(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b020", now)
	authorize, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err := AuthorizeExportDestination(ctx, authorize, job, archive, "01890f60-4d6d-7c12-8f02-6c9129d5b022",
		&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", CompletedUnits: 1, TotalUnits: 1}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := authorize.Commit(); err != nil {
		t.Fatal(err)
	}
	destination := &memoryExportDestination{reference: job.ResultRef}
	if err := destination.AtomicCommit(ctx, archive); err != nil {
		t.Fatal(err)
	}
	transactions := &auditServiceTransactions{database: database, workspaceID: job.WorkspaceID}
	results, err := ReconstructExportJobs(ctx, transactions, memoryDestinationResolver{destinations: map[string]*memoryExportDestination{job.ResultRef: destination}}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED || !results[0].RenameCommitted {
		t.Fatalf("recovered jobs = %#v", results)
	}
	loaded, err := LoadExportJob(ctx, database, job.ID)
	if err != nil || !bytes.Equal(loaded.DestinationHash, job.ArchiveHash) {
		t.Fatalf("recovered destination hash = %x err=%v", loaded.DestinationHash, err)
	}
}

func TestAuditExportCommitElectionWinsCancellationRaceAndNeverCancelsCommittedDestination(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 5, 30, 0, 0, time.UTC)
	archive, key := buildEvidenceArchiveFixtureWithKey(t)
	persistExportTestKey(t, ctx, database, key)
	job := createClaimedExportJob(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b024", now)
	authorize, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err := AuthorizeExportDestination(ctx, authorize, job, archive, "01890f60-4d6d-7c12-8f02-6c9129d5b026",
		&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", CompletedUnits: 1, TotalUnits: 1}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := authorize.Commit(); err != nil {
		t.Fatal(err)
	}
	transactions := &auditServiceTransactions{database: database, workspaceID: job.WorkspaceID}
	destination := &memoryExportDestination{reference: job.ResultRef}
	commitElected := make(chan struct{})
	releaseCommit := make(chan struct{})
	destination.beforeCommit = func() error {
		close(commitElected)
		<-releaseCommit
		return nil
	}
	type commitResult struct {
		job ExportJob
		err error
	}
	commitResults := make(chan commitResult, 1)
	go func() {
		completed, commitErr := CommitAuthorizedExport(ctx, transactions, writableAuditGate(), job.ID, archive, destination, now.Add(3*time.Minute))
		commitResults <- commitResult{job: completed, err: commitErr}
	}()
	<-commitElected
	cancelErr := transactions.Mutate(ctx, func(transaction ServiceTransaction) error {
		return RequestExportCancellation(ctx, transaction, job.ID, now.Add(2*time.Minute))
	})
	close(releaseCommit)
	if !errors.Is(cancelErr, ErrExportCommitAlreadyCompleted) {
		t.Fatalf("racing cancellation error=%v, want COMMIT_ALREADY_COMPLETED", cancelErr)
	}
	result := <-commitResults
	if result.err != nil {
		t.Fatal(result.err)
	}
	completed := result.job
	if completed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED ||
		completed.CancellationRequested || !completed.RenameCommitted || len(destination.content) == 0 {
		t.Fatalf("commit/cancel election=%#v destination=%d", completed, len(destination.content))
	}
}

func TestAuditExportCrashAfterRenameReconstructsFromDestinationWithoutCancelling(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 5, 45, 0, 0, time.UTC)
	archive, key := buildEvidenceArchiveFixtureWithKey(t)
	persistExportTestKey(t, ctx, database, key)
	job := createClaimedExportJob(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b027", now)
	authorize, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err := AuthorizeExportDestination(ctx, authorize, job, archive, "01890f60-4d6d-7c12-8f02-6c9129d5b029",
		&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", CompletedUnits: 1, TotalUnits: 1}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := authorize.Commit(); err != nil {
		t.Fatal(err)
	}
	signingKeyID := job.SigningKeyID
	if signingKeyID == "" {
		t.Fatal("authorized archive did not retain its signing key")
	}
	transactions := &auditServiceTransactions{database: database, workspaceID: job.WorkspaceID}
	destination := &memoryExportDestination{reference: job.ResultRef, commitErrAfterWrite: errors.New("process died after rename")}
	if _, err := CommitAuthorizedExport(ctx, transactions, writableAuditGate(), job.ID, archive, destination, now.Add(2*time.Minute)); err == nil {
		t.Fatal("post-rename death was not surfaced")
	}
	interrupted, err := LoadExportJob(ctx, database, job.ID)
	if err != nil || interrupted.Stage != "DESTINATION_COMMITTING" || interrupted.State == tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED {
		t.Fatalf("interrupted job=%#v err=%v", interrupted, err)
	}
	waiting, err := ReconstructExportJobs(ctx, transactions, memoryDestinationResolver{}, now.Add(3*time.Minute))
	if err != nil || len(waiting) != 1 || waiting[0].Stage != "COMMIT_DESTINATION_REAPPROVAL" {
		t.Fatalf("uncertain commit waiting=%#v err=%v", waiting, err)
	}
	cancel, _ := database.BeginEncryptedTx(ctx, nil)
	if err := RequestExportCancellation(ctx, cancel, job.ID, now.Add(4*time.Minute)); !errors.Is(err, ErrExportCommitAlreadyCompleted) {
		_ = cancel.Rollback()
		t.Fatalf("uncertain commit cancellation error=%v, want COMMIT_ALREADY_COMPLETED", err)
	}
	if err := cancel.Commit(); err != nil {
		t.Fatal(err)
	}
	uncancelled, _ := LoadExportJob(ctx, database, job.ID)
	if uncancelled.State == tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED {
		t.Fatal("uncertain post-rename job was cancelled")
	}
	reapprove, _ := database.BeginEncryptedTx(ctx, nil)
	reapproved, err := ReapproveExportDestination(ctx, reapprove, job.ID, uncancelled.Version, job.ResultRef,
		&tammyv1.JobProgress{Stage: "DESTINATION_COMMITTING", TotalUnits: 1, CompletedUnits: 1}, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := reapprove.Commit(); err != nil {
		t.Fatal(err)
	}
	if reapproved.Stage != "COLLECTING" || reapproved.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED ||
		reapproved.SigningKeyID != signingKeyID || exportJobProjection(reapproved).GetSigningKeyId() != signingKeyID {
		t.Fatalf("commit reapproval=%#v", reapproved)
	}
	destination.commitErrAfterWrite = nil
	claim, _ := database.BeginEncryptedTx(ctx, nil)
	reapproved, err = ClaimExportJob(ctx, claim, job.ID, now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Commit(); err != nil {
		t.Fatal(err)
	}
	authorize, _ = database.BeginEncryptedTx(ctx, nil)
	reapproved, err = AuthorizeExportDestination(ctx, authorize, reapproved, archive, destination.reference,
		&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", TotalUnits: 1, CompletedUnits: 1}, now.Add(7*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := authorize.Commit(); err != nil {
		t.Fatal(err)
	}
	recovered, err := CommitAuthorizedExport(ctx, transactions, writableAuditGate(), job.ID, archive, destination, now.Add(8*time.Minute))
	if err != nil || recovered.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED || destination.commitCalls != 1 {
		t.Fatalf("recovered=%#v commits=%d err=%v", recovered, destination.commitCalls, err)
	}
}

func TestAuditExportBeforeRenameFailureRestartsThroughQueuedReapprovalToCompletionOrCancellation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		jobID  string
		cancel bool
	}{
		{name: "complete", jobID: "01890f60-4d6d-7c12-8f02-6c9129d5b02a"},
		{name: "cancel", jobID: "01890f60-4d6d-7c12-8f02-6c9129d5b02d", cancel: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			now := time.Date(2026, 8, 4, 5, 50, 0, 0, time.UTC)
			archive, key := buildEvidenceArchiveFixtureWithKey(t)
			persistExportTestKey(t, ctx, database, key)
			job := createClaimedExportJob(t, ctx, database, testCase.jobID, now)
			authorize, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
			job, err := AuthorizeExportDestination(ctx, authorize, job, archive, job.DestinationCapability,
				&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", CompletedUnits: 1, TotalUnits: 1}, now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if err := authorize.Commit(); err != nil {
				t.Fatal(err)
			}
			signingKeyID := job.SigningKeyID
			transactions := &auditServiceTransactions{database: database, workspaceID: job.WorkspaceID}
			destination := &memoryExportDestination{reference: job.ResultRef, commitErrBeforeWrite: errors.New("rename unavailable")}
			if _, err := CommitAuthorizedExport(ctx, transactions, writableAuditGate(), job.ID, archive, destination, now.Add(2*time.Minute)); err == nil {
				t.Fatal("before-rename failure was not surfaced")
			}
			if len(destination.content) != 0 {
				t.Fatal("before-rename failure wrote destination bytes")
			}
			waiting, err := ReconstructExportJobs(ctx, transactions, memoryDestinationResolver{}, now.Add(3*time.Minute))
			if err != nil || len(waiting) != 1 || waiting[0].State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_WAITING_FOR_INPUT ||
				waiting[0].Stage != "COMMIT_DESTINATION_REAPPROVAL" {
				t.Fatalf("waiting=%#v err=%v", waiting, err)
			}
			reapprove, _ := database.BeginEncryptedTx(ctx, nil)
			reapproved, err := ReapproveExportDestination(ctx, reapprove, job.ID, waiting[0].Version, destination.reference,
				&tammyv1.JobProgress{Stage: "COLLECTING", TotalUnits: 1}, now.Add(4*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if err := reapprove.Commit(); err != nil {
				t.Fatal(err)
			}
			if reapproved.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED || reapproved.Stage != "COLLECTING" ||
				reapproved.SigningKeyID != signingKeyID {
				t.Fatalf("reapproved transition=%#v", reapproved)
			}
			if testCase.cancel {
				cancel, _ := database.BeginEncryptedTx(ctx, nil)
				if err := RequestExportCancellation(ctx, cancel, job.ID, now.Add(5*time.Minute)); err != nil {
					t.Fatal(err)
				}
				if err := cancel.Commit(); err != nil {
					t.Fatal(err)
				}
				cancelled, err := LoadExportJob(ctx, database, job.ID)
				if err != nil || cancelled.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED || len(destination.content) != 0 {
					t.Fatalf("cancelled=%#v bytes=%d err=%v", cancelled, len(destination.content), err)
				}
				return
			}
			destination.commitErrBeforeWrite = nil
			claim, _ := database.BeginEncryptedTx(ctx, nil)
			resumed, err := ClaimExportJob(ctx, claim, job.ID, now.Add(5*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if err := claim.Commit(); err != nil {
				t.Fatal(err)
			}
			authorize, _ = database.BeginEncryptedTx(ctx, nil)
			resumed, err = AuthorizeExportDestination(ctx, authorize, resumed, archive, destination.reference,
				&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", CompletedUnits: 1, TotalUnits: 1}, now.Add(6*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if err := authorize.Commit(); err != nil {
				t.Fatal(err)
			}
			completed, err := CommitAuthorizedExport(ctx, transactions, writableAuditGate(), resumed.ID, archive, destination, now.Add(7*time.Minute))
			if err != nil || completed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED ||
				!completed.RenameCommitted || len(destination.content) == 0 {
				t.Fatalf("completed=%#v bytes=%d err=%v", completed, len(destination.content), err)
			}
		})
	}
}

func TestAuditExportJobStartupRequeuesUncheckpointedRunningWork(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	job := createClaimedExportJob(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b030", now)
	transactions := &auditServiceTransactions{database: database, workspaceID: job.WorkspaceID}
	results, err := ReconstructExportJobs(ctx, transactions, memoryDestinationResolver{}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != job.ID || results[0].State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED {
		t.Fatalf("reconstructed jobs = %#v", results)
	}
}

func TestAuditExportDestinationReapprovalIsExplicitAndFenced(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 6, 10, 0, 0, time.UTC)
	archive, key := buildEvidenceArchiveFixtureWithKey(t)
	persistExportTestKey(t, ctx, database, key)
	job := createClaimedExportJob(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b034", now)
	authorize, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err := AuthorizeExportDestination(ctx, authorize, job, archive, "01890f60-4d6d-7c12-8f02-6c9129d5b036",
		&tammyv1.JobProgress{Stage: "ARCHIVE_VERIFIED", TotalUnits: 1, CompletedUnits: 1}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := authorize.Commit(); err != nil {
		t.Fatal(err)
	}
	signingKeyID := job.SigningKeyID
	transactions := &auditServiceTransactions{database: database, workspaceID: job.WorkspaceID}
	reconstructed, err := ReconstructExportJobs(ctx, transactions, memoryDestinationResolver{}, now.Add(2*time.Minute))
	if err != nil || len(reconstructed) != 1 || reconstructed[0].State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_WAITING_FOR_INPUT {
		t.Fatalf("waiting=%#v err=%v", reconstructed, err)
	}
	newReference := "01890f60-4d6d-7c12-8f02-6c9129d5b038"
	reapprove, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	reapproved, err := ReapproveExportDestination(ctx, reapprove, job.ID, reconstructed[0].Version, newReference,
		&tammyv1.JobProgress{Stage: "COLLECTING", TotalUnits: 1}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := reapprove.Commit(); err != nil {
		t.Fatal(err)
	}
	if reapproved.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED || reapproved.ResultRef != newReference ||
		reapproved.SigningKeyID != signingKeyID || exportJobProjection(reapproved).GetSigningKeyId() != signingKeyID {
		t.Fatalf("reapproved=%#v", reapproved)
	}
}

func TestAuditExportJobRetryableFailureRequiresExplicitFencedRetry(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 6, 30, 0, 0, time.UTC)
	job := createClaimedExportJob(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b040", now)
	fail, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err := FailExportJob(ctx, fail, job.ID, job.Version, true, "TEMPORARY_DESTINATION_FAILURE", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := fail.Commit(); err != nil {
		t.Fatal(err)
	}
	if job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE {
		t.Fatalf("failed job=%#v", job)
	}
	retry, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err = RetryExportJob(ctx, retry, job.ID, job.Version, &tammyv1.JobProgress{Stage: "COLLECTING", TotalUnits: 1}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := retry.Commit(); err != nil {
		t.Fatal(err)
	}
	claim, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err = ClaimExportJob(ctx, claim, job.ID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Commit(); err != nil {
		t.Fatal(err)
	}
	if job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_RUNNING || job.Attempt != 2 {
		t.Fatalf("retried job=%#v", job)
	}
}

type recordingEvidenceProvider struct {
	job      ExportJob
	evidence []EvidenceObject
}

func (provider *recordingEvidenceProvider) Collect(_ context.Context, job ExportJob) ([]EvidenceObject, error) {
	provider.job = job
	if provider.evidence != nil {
		return provider.evidence, nil
	}
	return []EvidenceObject{{Path: "evidence/provider-selection.bin", Bytes: []byte("selected evidence")}}, nil
}

type recordingEvidenceExportDEKProvider struct {
	dek                  []byte
	acquiredWorkspaceIDs []string
	lease                *recordingEvidenceExportDEKLease
	acquireErr           error
	leaseWithAcquireErr  bool
	acquireTypedNilLease bool
	withDEKErr           error
	beforeWithDEK        func()
}

func (provider *recordingEvidenceExportDEKProvider) Acquire(_ context.Context, workspaceID string) (EvidenceExportDEKLease, error) {
	provider.acquiredWorkspaceIDs = append(provider.acquiredWorkspaceIDs, workspaceID)
	if provider.leaseWithAcquireErr {
		provider.lease = &recordingEvidenceExportDEKLease{dek: append([]byte(nil), provider.dek...)}
		return provider.lease, provider.acquireErr
	}
	if provider.acquireErr != nil {
		return nil, provider.acquireErr
	}
	if provider.acquireTypedNilLease {
		var lease *recordingEvidenceExportDEKLease
		return lease, nil
	}
	provider.lease = &recordingEvidenceExportDEKLease{
		dek: append([]byte(nil), provider.dek...), withDEKErr: provider.withDEKErr, beforeWithDEK: provider.beforeWithDEK,
	}
	return provider.lease, nil
}

type recordingEvidenceExportDEKLease struct {
	dek           []byte
	withDEKCalls  int
	closeCalls    int
	closed        bool
	withDEKErr    error
	beforeWithDEK func()
}

func (lease *recordingEvidenceExportDEKLease) WithDEK(callback func([]byte) error) error {
	lease.withDEKCalls++
	if lease.closed {
		return errors.New("test DEK lease is closed")
	}
	if lease.beforeWithDEK != nil {
		lease.beforeWithDEK()
	}
	if lease.withDEKErr != nil {
		return lease.withDEKErr
	}
	return callback(lease.dek)
}

func (lease *recordingEvidenceExportDEKLease) Close() {
	lease.closeCalls++
	lease.closed = true
	Zero(lease.dek)
}

func TestEvidenceExportWorkerRetainsProviderInsteadOfRawDEK(t *testing.T) {
	if _, retained := reflect.TypeOf(EvidenceExportWorkerConfig{}).FieldByName("DEK"); retained {
		t.Fatal("worker config retains a raw DEK field")
	}
	workerType := reflect.TypeOf(EvidenceExportWorker{})
	if _, retained := workerType.FieldByName("dek"); retained {
		t.Fatal("worker retains a raw DEK field")
	}
	if _, retained := reflect.TypeOf(EvidenceExportWorkerConfig{}).FieldByName("Descriptors"); retained {
		t.Fatal("worker config retains a single descriptor blob instead of loading the historical registry")
	}
	if _, retained := workerType.FieldByName("descriptors"); retained {
		t.Fatal("worker retains a single descriptor blob instead of loading the historical registry")
	}
	providerField, present := workerType.FieldByName("dekProvider")
	if !present || providerField.Type != reflect.TypeOf((*EvidenceExportDEKProvider)(nil)).Elem() {
		t.Fatalf("worker DEK provider field = %#v, present=%t", providerField, present)
	}
	var typedNilProvider *recordingEvidenceExportDEKProvider
	if _, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions:      &auditServiceTransactions{},
		Destinations:      memoryDestinationResolver{},
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:       typedNilProvider,
		Clock:             clock.Func(func() time.Time { return time.Now() }),
		Gate:              NewWriteGate(),
	}); !errors.Is(err, ErrExportJob) {
		t.Fatalf("typed-nil DEK provider constructor error=%v, want ErrExportJob", err)
	}
}

func TestEvidenceExportWorkerBuildsPersistedSnapshotAndCommitsThroughSelectedProviders(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	descriptors := testAuditDescriptorSet(t)
	seedAuditChainWithFingerprint(t, ctx, database, descriptors)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	now := time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC)
	key, _, err := GenerateSigningKey(workspaceID, dek, now.Add(-time.Hour), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	persistExportTestKey(t, ctx, database, key)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b081")
	request.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b082"
	actor := "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	start, end := uint64(2), uint64(3)
	request.Filter = &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
	}, ActorUserId: &actor, FromTime: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 5, 0, time.UTC)),
		ToTime: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 6, 0, time.UTC)), StartSequence: &start, EndSequence: &end}
	persistedFilter := proto.Clone(request.Filter).(*tammyv1.AuditEventFilter)
	created, err := service.ExportEvidence(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	request.Filter = &tammyv1.AuditEventFilter{}
	destination := &memoryExportDestination{reference: request.Destination.CapabilityId}
	provider := &recordingEvidenceProvider{}
	dekProvider := &recordingEvidenceExportDEKProvider{dek: dek}
	transactions := &auditServiceTransactions{database: database, workspaceID: workspaceID}
	gate := NewWriteGate()
	resolver := memoryDestinationResolver{
		destinations: map[string]*memoryExportDestination{destination.reference: destination},
		beforeResolve: func() error {
			if dekProvider.lease == nil || !dekProvider.lease.closed {
				return errors.New("destination resolution began before the DEK lease closed")
			}
			return nil
		},
	}
	config := EvidenceExportWorkerConfig{Transactions: transactions,
		Destinations:      resolver,
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": provider},
		DEKProvider:       dekProvider, Clock: clock.Func(func() time.Time { return now }), Gate: gate}
	invalidConfig := config
	invalidConfig.DEKProvider = nil
	if _, err := NewEvidenceExportWorker(invalidConfig); !errors.Is(err, ErrExportJob) {
		t.Fatalf("nil DEK provider constructor error=%v, want ErrExportJob", err)
	}
	worker, err := NewEvidenceExportWorker(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Run(ctx, created.Msg.Job.Id); !errors.Is(err, ErrWriteGate) {
		t.Fatalf("locked worker error=%v, want ErrWriteGate", err)
	}
	if len(dekProvider.acquiredWorkspaceIDs) != 0 {
		t.Fatalf("locked worker acquired DEK for workspaces %v", dekProvider.acquiredWorkspaceIDs)
	}
	lockedJob, err := LoadExportJob(ctx, database, created.Msg.Job.Id)
	if err != nil || lockedJob.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED || destination.commitCalls != 0 {
		t.Fatalf("locked worker job=%#v destination_calls=%d err=%v", lockedJob, destination.commitCalls, err)
	}
	gate.set(true, true)
	completed, err := worker.Run(ctx, created.Msg.Job.Id)
	if err != nil {
		failed, _ := LoadExportJob(ctx, database, created.Msg.Job.Id)
		t.Fatalf("worker error=%v persisted_state=%v stage=%s version=%d", err, failed.State, failed.Stage, failed.Version)
	}
	if completed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED || !completed.RenameCommitted ||
		provider.job.ID != completed.ID || provider.job.SnapshotSequence != 3 || !proto.Equal(provider.job.Filter, persistedFilter) || len(destination.content) == 0 {
		t.Fatalf("worker completed=%#v provider=%#v destination=%d", completed, provider.job, len(destination.content))
	}
	if len(dekProvider.acquiredWorkspaceIDs) != 1 || dekProvider.acquiredWorkspaceIDs[0] != workspaceID ||
		dekProvider.lease == nil || dekProvider.lease.withDEKCalls != 1 || dekProvider.lease.closeCalls != 1 || !dekProvider.lease.closed ||
		!bytes.Equal(dekProvider.lease.dek, make([]byte, len(dekProvider.lease.dek))) {
		t.Fatalf("DEK lease workspace=%v lease=%#v", dekProvider.acquiredWorkspaceIDs, dekProvider.lease)
	}
	verification, err := VerifyEvidenceArchive(destination.content)
	if err != nil || verification.EventCount != 1 {
		t.Fatalf("filtered verification=%#v err=%v", verification, err)
	}
	members := readArchiveMembers(t, destination.content)
	if _, leaked := members["events/00000000000000000001/event.pb"]; leaked {
		t.Fatal("sequence 1 outside the persisted filter leaked into exported event artifacts")
	}
	if _, leaked := members["events/00000000000000000002/event.pb"]; leaked {
		t.Fatal("sequence 2 outside the persisted filter leaked into exported event artifacts")
	}
	if len(members["events/00000000000000000003/event.pb"]) == 0 || len(members["chain/heads.bin"]) != 3*sha256.Size {
		t.Fatalf("filtered members missing selected event or snapshot proof: %v", members)
	}
}

func TestEvidenceExportWorkerStreamsBoundedPagesForLargeSparseSnapshot(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	matchingActor := "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	descriptors := testAuditDescriptorSet(t)
	selectedSequences := seedLargeAuditChainWithFingerprint(t, ctx, database, descriptors, 300, matchingActor, 37)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	now := time.Date(2026, 8, 4, 7, 15, 0, 0, time.UTC)
	key, _, err := GenerateSigningKey(workspaceID, dek, now.Add(-time.Hour), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	persistExportTestKey(t, ctx, database, key)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b083")
	request.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b084"
	request.Filter = &tammyv1.AuditEventFilter{
		ActorUserId: &matchingActor,
		EventTypes: []tammyv1.AuditEventType{
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		},
	}
	created, err := service.ExportEvidence(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	destination := &memoryExportDestination{reference: request.Destination.CapabilityId}
	transactions := &boundedAuditQueryTransactions{auditServiceTransactions: &auditServiceTransactions{
		database: database, workspaceID: workspaceID,
	}}
	worker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions: transactions,
		Destinations: memoryDestinationResolver{destinations: map[string]*memoryExportDestination{
			destination.reference: destination,
		}},
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:       &recordingEvidenceExportDEKProvider{dek: dek},
		Clock:             clock.Func(func() time.Time { return now }),
		Gate:              writableAuditGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := worker.Run(ctx, created.Msg.Job.Id)
	if err != nil {
		persisted, _ := LoadExportJob(ctx, database, created.Msg.Job.Id)
		t.Fatalf("bounded worker error=%v persisted_state=%v stage=%s", err, persisted.State, persisted.Stage)
	}
	if completed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED || destination.commitCalls != 1 {
		t.Fatalf("completed=%#v destination calls=%d", completed, destination.commitCalls)
	}
	auditQueries := 0
	for _, query := range transactions.sql {
		if strings.Contains(query, "FROM audit_events_v1") {
			auditQueries++
		}
	}
	if auditQueries < 2 {
		t.Fatalf("large snapshot used only %d bounded audit-event queries: %q", auditQueries, transactions.sql)
	}
	verification, err := VerifyEvidenceArchive(destination.content)
	if err != nil || verification.EventCount != uint64(len(selectedSequences)) {
		t.Fatalf("verification=%#v selected=%v err=%v", verification, selectedSequences, err)
	}
	members := readArchiveMembers(t, destination.content)
	for _, sequence := range selectedSequences {
		path := fmt.Sprintf("events/%020d/event.pb", sequence)
		if len(members[path]) == 0 {
			t.Fatalf("selected sequence %d is absent from archive", sequence)
		}
	}
	for _, sequence := range []uint64{1, 36, 38, 299, 300} {
		path := fmt.Sprintf("events/%020d/event.pb", sequence)
		if _, leaked := members[path]; leaked {
			t.Fatalf("excluded sequence %d leaked into archive", sequence)
		}
	}
	if len(members["chain/heads.bin"]) != 300*sha256.Size {
		t.Fatalf("snapshot proof bytes=%d, want %d", len(members["chain/heads.bin"]), 300*sha256.Size)
	}
}

func TestEvidenceExportWorkerCleansSpoolAndExcludesPrivateBytesOnSignerFailure(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	matchingActor := "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	descriptors := testAuditDescriptorSet(t)
	seedLargeAuditChainWithFingerprint(t, ctx, database, descriptors, 3, matchingActor, 3)
	retained, err := LoadStoredEvents(ctx, database, workspaceID, 1, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(retained[0].PayloadProto, retained[1].PayloadProto) ||
		bytes.Equal(retained[0].PayloadProto, retained[2].PayloadProto) ||
		bytes.Equal(retained[1].PayloadProto, retained[2].PayloadProto) {
		t.Fatal("spool privacy fixture requires a unique retained payload_proto for every sequence")
	}
	dek := bytes.Repeat([]byte{0x5a}, 32)
	now := time.Date(2026, 8, 4, 7, 20, 0, 0, time.UTC)
	key, _, err := GenerateSigningKey(workspaceID, dek, now.Add(-time.Hour), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	persistExportTestKey(t, ctx, database, key)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b085")
	request.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b086"
	startSequence, endSequence := uint64(3), uint64(3)
	request.Filter = &tammyv1.AuditEventFilter{
		EventTypes: []tammyv1.AuditEventType{
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		},
		StartSequence: &startSequence,
		EndSequence:   &endSequence,
	}
	created, err := service.ExportEvidence(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	spoolParent := t.TempDir()
	forbidden := map[string][]byte{
		"excluded sequence 1 event_proto":         retained[0].EventProto,
		"excluded sequence 1 payload_proto":       retained[0].PayloadProto,
		"excluded sequence 1 actor opening":       retained[0].Event.CommitmentOpenings.ActorUserIdBlinding,
		"excluded sequence 2 event_proto":         retained[1].EventProto,
		"excluded sequence 2 payload_proto":       retained[1].PayloadProto,
		"excluded sequence 2 actor opening":       retained[1].Event.CommitmentOpenings.ActorUserIdBlinding,
		"selected event hidden metadata opening":  retained[2].Event.CommitmentOpenings.HiddenMetadataBlinding,
		"selected event non-filter actor opening": retained[2].Event.CommitmentOpenings.ActorUserIdBlinding,
	}
	inspectionCalls := 0
	dekProvider := &recordingEvidenceExportDEKProvider{
		dek:        dek,
		withDEKErr: errors.New("test signer failure after spooling"),
		beforeWithDEK: func() {
			inspectionCalls++
			assertSpoolExcludesPrivateBytes(t, spoolParent, forbidden)
		},
	}
	destination := &memoryExportDestination{reference: request.Destination.CapabilityId}
	config := EvidenceExportWorkerConfig{
		Transactions: &auditServiceTransactions{database: database, workspaceID: workspaceID},
		Destinations: memoryDestinationResolver{destinations: map[string]*memoryExportDestination{
			destination.reference: destination,
		}},
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:       dekProvider,
		Clock:             clock.Func(func() time.Time { return now }),
		Gate:              writableAuditGate(),
	}
	spoolConfigPresent := setWorkerSpoolParentDirectory(&config, spoolParent)
	worker, err := NewEvidenceExportWorker(config)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := worker.Run(ctx, created.Msg.Job.Id)
	if !errors.Is(err, ErrExportJob) || err.Error() != ErrExportJob.Error() {
		t.Fatalf("worker error=%q, want generic ErrExportJob", err)
	}
	if !spoolConfigPresent {
		t.Error("EvidenceExportWorkerConfig is missing SpoolParentDirectory")
	}
	if inspectionCalls != 1 {
		t.Errorf("signer callback inspections=%d, want 1", inspectionCalls)
	}
	entries, readErr := os.ReadDir(spoolParent)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("spool parent retained %d entries after signer failure: %v", len(entries), entries)
	}
	persisted, loadErr := LoadExportJob(ctx, database, created.Msg.Job.Id)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if failed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
		persisted.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
		persisted.Stage != "ARCHIVE_BUILD_FAILED" || destination.commitCalls != 0 || len(destination.content) != 0 {
		t.Errorf("failed=%#v persisted=%#v destination=%#v", failed, persisted, destination)
	}
	if dekProvider.lease == nil || dekProvider.lease.withDEKCalls != 1 || dekProvider.lease.closeCalls != 1 ||
		!dekProvider.lease.closed || !bytes.Equal(dekProvider.lease.dek, make([]byte, len(dekProvider.lease.dek))) {
		t.Errorf("released lease=%#v", dekProvider.lease)
	}
}

func TestEvidenceExportWorkerCleansSpoolWhenLaterSnapshotPageIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	descriptors := testAuditDescriptorSet(t)
	seedLargeAuditChainWithFingerprint(t, ctx, database, descriptors, 300,
		"01890f60-4d6d-7c12-8f02-6c9129d5b006", 37)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	now := time.Date(2026, 8, 4, 7, 25, 0, 0, time.UTC)
	key, _, err := GenerateSigningKey(workspaceID, dek, now.Add(-time.Hour), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	persistExportTestKey(t, ctx, database, key)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b087")
	request.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b088"
	created, err := service.ExportEvidence(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	spoolParent := t.TempDir()
	transactions := &boundedAuditQueryTransactions{auditServiceTransactions: &auditServiceTransactions{
		database: database, workspaceID: workspaceID,
	}, beginReadWithoutCancel: true}
	transactions.afterAuditQuery = func(queryNumber int) {
		if queryNumber == 3 {
			cancel()
		}
	}
	destination := &memoryExportDestination{reference: request.Destination.CapabilityId}
	dekProvider := &recordingEvidenceExportDEKProvider{dek: dek}
	worker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions: transactions,
		Destinations: memoryDestinationResolver{destinations: map[string]*memoryExportDestination{
			destination.reference: destination,
		}},
		EvidenceProviders:    map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:          dekProvider,
		Clock:                clock.Func(func() time.Time { return now }),
		Gate:                 writableAuditGate(),
		SpoolParentDirectory: spoolParent,
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := worker.Run(ctx, created.Msg.Job.Id)
	if !errors.Is(err, ErrExportJob) || err.Error() != ErrExportJob.Error() {
		t.Fatalf("cancelled worker error=%q, want generic ErrExportJob", err)
	}
	if transactions.auditQueryCount != 3 {
		t.Errorf("audit queries=%d, want cancellation during query 3", transactions.auditQueryCount)
	}
	entries, readErr := os.ReadDir(spoolParent)
	if readErr != nil {
		t.Fatal(readErr)
	}
	persisted, loadErr := LoadExportJob(context.Background(), database, created.Msg.Job.Id)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if failed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
		persisted.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
		persisted.Stage != "SNAPSHOT_COLLECTION_FAILED" || len(entries) != 0 ||
		len(dekProvider.acquiredWorkspaceIDs) != 0 || destination.commitCalls != 0 || len(destination.content) != 0 {
		t.Errorf("failed=%#v persisted=%#v spool=%v DEK=%v destination=%#v", failed, persisted, entries,
			dekProvider.acquiredWorkspaceIDs, destination)
	}
}

func TestNewEvidenceExportWorkerRejectsSymlinkSpoolParent(t *testing.T) {
	realParent := t.TempDir()
	symlinkParent := filepath.Join(t.TempDir(), "spool-link")
	if err := os.Symlink(realParent, symlinkParent); err != nil {
		t.Fatal(err)
	}
	_, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions:         &auditServiceTransactions{},
		Destinations:         memoryDestinationResolver{},
		EvidenceProviders:    map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:          &recordingEvidenceExportDEKProvider{dek: bytes.Repeat([]byte{0x5a}, 32)},
		Clock:                clock.Func(func() time.Time { return time.Now() }),
		Gate:                 NewWriteGate(),
		SpoolParentDirectory: symlinkParent,
	})
	if !errors.Is(err, ErrExportJob) {
		t.Fatalf("symlink spool parent constructor error=%v, want ErrExportJob", err)
	}
}

func setWorkerSpoolParentDirectory(config *EvidenceExportWorkerConfig, path string) bool {
	field := reflect.ValueOf(config).Elem().FieldByName("SpoolParentDirectory")
	if !field.IsValid() || field.Kind() != reflect.String || !field.CanSet() {
		return false
	}
	field.SetString(path)
	return true
}

func assertSpoolExcludesPrivateBytes(t *testing.T, parent string, forbidden map[string][]byte) {
	t.Helper()
	regularFiles := 0
	err := filepath.Walk(parent, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		regularFiles++
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for label, privateBytes := range forbidden {
			if len(privateBytes) != 0 && bytes.Contains(content, privateBytes) {
				t.Errorf("spool file %q contains %s", path, label)
			}
		}
		return nil
	})
	if err != nil {
		t.Errorf("inspect spool: %v", err)
	}
	if regularFiles == 0 {
		t.Error("signer callback observed no spool files")
	}
}

func TestEvidenceExportWorkerUsesCurrentAuthenticatedSigningLineageDespiteRetiredJobPin(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	descriptors := testAuditDescriptorSet(t)
	seedAuditChainWithFingerprint(t, ctx, database, descriptors)
	fingerprint := sha256.Sum256(descriptors)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	now := time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC)
	root, _, err := GenerateSigningKey(workspaceID, dek, now.Add(-time.Hour),
		bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	persistExportTestKey(t, ctx, database, root)
	header, err := LoadChainHeader(ctx, database, workspaceID, 0)
	if err != nil {
		t.Fatal(err)
	}
	mirror := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID,
		Generation: header.Generation, Sequence: header.CurrentSequence, Head: append([]byte(nil), header.CurrentHead[:]...)}}
	gate := writableAuditGate()
	appender, err := NewMirroringAppender(mirror, gate)
	if err != nil {
		t.Fatal(err)
	}
	rotation := beginSigningKeyRotationTransaction(t, ctx, database)
	rotationEvent := signingKeyRotationEventTemplate("01890f60-4d6d-7c12-8f02-6c9129d5b0b0", workspaceID)
	rotationEvent.PayloadSchemaFingerprint = fingerprint[:]
	rotated, err := appender.RotateSigningKey(ctx, rotation, SigningKeyRotationInput{
		ExpectedHeader: header,
		ExpectedState: SigningKeyState{WorkspaceID: workspaceID, RootKeyID: root.KeyID,
			ActiveKeyID: root.KeyID, ActiveEpoch: 1},
		DEK: dek, RotatedAt: now.Add(-30 * time.Minute),
		Random: bytes.NewReader(bytes.Repeat([]byte{0x7c}, 128)), Event: rotationEvent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rotation.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}

	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b0b1")
	request.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b0b2"
	created, err := service.ExportEvidence(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE audit_export_jobs_v1 SET signing_key_id=? WHERE id=?`,
		root.KeyID, created.Msg.Job.Id); err != nil {
		t.Fatal(err)
	}
	destination := &memoryExportDestination{reference: request.Destination.CapabilityId}
	dekProvider := &recordingEvidenceExportDEKProvider{dek: dek}
	worker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions: &auditServiceTransactions{database: database, workspaceID: workspaceID},
		Destinations: memoryDestinationResolver{destinations: map[string]*memoryExportDestination{
			destination.reference: destination,
		}},
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:       dekProvider, Clock: clock.Func(func() time.Time { return now }), Gate: writableAuditGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := worker.Run(ctx, created.Msg.Job.Id)
	if err != nil {
		persisted, _ := LoadExportJob(ctx, database, created.Msg.Job.Id)
		t.Fatalf("worker with retired job pin failed: err=%v state=%v stage=%q", err, persisted.State, persisted.Stage)
	}
	verification, err := VerifyEvidenceArchive(destination.content)
	if err != nil || completed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED ||
		verification.Manifest.SigningKeyId != rotated.Successor.KeyID ||
		verification.Manifest.RootSigningKeyId != root.KeyID || verification.Manifest.SigningKeyEpoch != 2 {
		t.Fatalf("current-lineage archive completed=%v manifest=%#v err=%v",
			completed.State, verification.Manifest, err)
	}
	members := readArchiveMembers(t, destination.content)
	chain := new(tammyv1.AuditSigningKeyChain)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(members[signingKeyChainArchivePath], chain); err != nil ||
		len(chain.Keys) != 2 || len(chain.Links) != 1 || chain.Keys[0].KeyId != root.KeyID ||
		chain.Keys[1].KeyId != rotated.Successor.KeyID || chain.Keys[1].Epoch != 2 {
		t.Fatalf("archived signing lineage=%#v err=%v", chain, err)
	}
	if len(dekProvider.acquiredWorkspaceIDs) != 1 {
		t.Fatalf("current signing key was not leased exactly once: %v", dekProvider.acquiredWorkspaceIDs)
	}

	tamperedRequest := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b0b3")
	tamperedRequest.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b0b4"
	tamperedJob, err := service.ExportEvidence(ctx, connect.NewRequest(tamperedRequest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DROP TRIGGER audit_signing_keys_v1_retire_only`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE audit_signing_keys_v1
		SET successor_possession_signature=zeroblob(64) WHERE workspace_id=? AND key_id=?`,
		workspaceID, rotated.Successor.KeyID); err != nil {
		t.Fatal(err)
	}
	tamperedDestination := &memoryExportDestination{reference: tamperedRequest.Destination.CapabilityId}
	tamperedDEKProvider := &recordingEvidenceExportDEKProvider{dek: dek}
	tamperedWorker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions: &auditServiceTransactions{database: database, workspaceID: workspaceID},
		Destinations: memoryDestinationResolver{destinations: map[string]*memoryExportDestination{
			tamperedDestination.reference: tamperedDestination,
		}},
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:       tamperedDEKProvider, Clock: clock.Func(func() time.Time { return now }), Gate: writableAuditGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := tamperedWorker.Run(ctx, tamperedJob.Msg.Job.Id)
	if !errors.Is(err, ErrExportJob) || failed.Stage != "SNAPSHOT_COLLECTION_FAILED" ||
		len(tamperedDEKProvider.acquiredWorkspaceIDs) != 0 || tamperedDestination.commitCalls != 0 {
		t.Fatalf("tampered-lineage worker failed=%#v err=%v leases=%v commits=%d", failed, err,
			tamperedDEKProvider.acquiredWorkspaceIDs, tamperedDestination.commitCalls)
	}
}

func TestEvidenceExportWorkerLoadsEveryHistoricalDescriptorFromSnapshotRegistry(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	currentDescriptors := testAuditDescriptorSet(t)
	historicalDescriptors := testEvolvedAuditDescriptorSet(t, currentDescriptors)
	currentFingerprint, historicalFingerprint := seedHistoricalAuditChain(t, ctx, database,
		currentDescriptors, historicalDescriptors)
	now := time.Date(2026, 8, 4, 7, 15, 0, 0, time.UTC)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	key, _, err := GenerateSigningKey(workspaceID, dek, now.Add(-time.Hour),
		bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	persistExportTestKey(t, ctx, database, key)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b0a1")
	request.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b0a2"
	created, err := service.ExportEvidence(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	destination := &memoryExportDestination{reference: request.Destination.CapabilityId}
	worker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions:      &auditServiceTransactions{database: database, workspaceID: workspaceID},
		Destinations:      memoryDestinationResolver{destinations: map[string]*memoryExportDestination{destination.reference: destination}},
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": &recordingEvidenceProvider{}},
		DEKProvider:       &recordingEvidenceExportDEKProvider{dek: dek},
		Clock:             clock.Func(func() time.Time { return now }),
		Gate:              writableAuditGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := worker.Run(ctx, created.Msg.Job.Id)
	if err != nil {
		persisted, loadErr := LoadExportJob(ctx, database, created.Msg.Job.Id)
		t.Fatalf("worker error=%v persisted stage=%q state=%v load_err=%v", err, persisted.Stage, persisted.State, loadErr)
	}
	verification, err := VerifyEvidenceArchive(destination.content)
	if err != nil || verification.EventCount != 2 || completed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED {
		t.Fatalf("historical worker completed=%v verification=%#v err=%v", completed.State, verification, err)
	}
	members := readArchiveMembers(t, destination.content)
	for _, fingerprint := range [][sha256.Size]byte{currentFingerprint, historicalFingerprint} {
		if len(members[descriptorArchivePath(fingerprint)]) == 0 {
			t.Fatalf("worker archive omitted descriptor %x", fingerprint)
		}
	}
}

func TestEvidenceExportWorkerReleasesDEKLeaseAndFailsClosed(t *testing.T) {
	secretError := errors.New("locked provider contained secret-dek-error-detail")
	testCases := []struct {
		name               string
		provider           *recordingEvidenceExportDEKProvider
		cancelDuringLease  bool
		invalidBeforeSign  bool
		wantAcquireCalls   int
		wantWithDEKCalls   int
		wantCloseCalls     int
		wantLeaseAllocated bool
	}{
		{name: "archive signing failure", provider: &recordingEvidenceExportDEKProvider{dek: bytes.Repeat([]byte{0x5a}, 31)},
			wantAcquireCalls: 1, wantWithDEKCalls: 1, wantCloseCalls: 1, wantLeaseAllocated: true},
		{name: "lease callback failure", provider: &recordingEvidenceExportDEKProvider{dek: bytes.Repeat([]byte{0x5a}, 32), withDEKErr: secretError},
			wantAcquireCalls: 1, wantWithDEKCalls: 1, wantCloseCalls: 1, wantLeaseAllocated: true},
		{name: "context cancelled after acquire", provider: &recordingEvidenceExportDEKProvider{dek: bytes.Repeat([]byte{0x5a}, 32)},
			cancelDuringLease: true, wantAcquireCalls: 1, wantWithDEKCalls: 1, wantCloseCalls: 1, wantLeaseAllocated: true},
		{name: "locked provider", provider: &recordingEvidenceExportDEKProvider{acquireErr: secretError}, wantAcquireCalls: 1},
		{name: "lease returned with acquire error", provider: &recordingEvidenceExportDEKProvider{
			dek: bytes.Repeat([]byte{0x6b}, 32), acquireErr: secretError, leaseWithAcquireErr: true,
		}, wantAcquireCalls: 1, wantCloseCalls: 1, wantLeaseAllocated: true},
		{name: "typed nil lease", provider: &recordingEvidenceExportDEKProvider{acquireTypedNilLease: true}, wantAcquireCalls: 1},
		{name: "invalid before signing", provider: &recordingEvidenceExportDEKProvider{dek: bytes.Repeat([]byte{0x5a}, 32)}, invalidBeforeSign: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if testCase.cancelDuringLease {
				testCase.provider.beforeWithDEK = cancel
			}
			var evidenceProviders []EvidenceProvider
			if testCase.invalidBeforeSign {
				evidenceProviders = append(evidenceProviders, &recordingEvidenceProvider{
					evidence: []EvidenceObject{{Path: "manifest.json", Bytes: []byte("reserved pre-signing member")}},
				})
			}
			worker, database, destination, jobID, workspaceID := newFailingEvidenceExportWorkerFixture(t, ctx, testCase.provider, evidenceProviders...)
			failed, err := worker.Run(ctx, jobID)
			if !errors.Is(err, ErrExportJob) || err.Error() != ErrExportJob.Error() {
				t.Fatalf("worker error=%q, want generic ErrExportJob", err)
			}
			persisted, loadErr := LoadExportJob(context.Background(), database, jobID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if failed.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
				persisted.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
				persisted.Stage != "ARCHIVE_BUILD_FAILED" || destination.commitCalls != 0 || len(destination.content) != 0 {
				t.Fatalf("failed=%#v persisted=%#v destination=%#v", failed, persisted, destination)
			}
			if len(testCase.provider.acquiredWorkspaceIDs) != testCase.wantAcquireCalls ||
				(testCase.wantAcquireCalls == 1 && testCase.provider.acquiredWorkspaceIDs[0] != workspaceID) {
				t.Fatalf("acquired workspaces=%v, want calls=%d workspace=%q", testCase.provider.acquiredWorkspaceIDs, testCase.wantAcquireCalls, workspaceID)
			}
			if (testCase.provider.lease != nil) != testCase.wantLeaseAllocated {
				t.Fatalf("lease=%#v, want allocated=%t", testCase.provider.lease, testCase.wantLeaseAllocated)
			}
			if testCase.provider.lease != nil {
				lease := testCase.provider.lease
				if lease.withDEKCalls != testCase.wantWithDEKCalls || lease.closeCalls != testCase.wantCloseCalls || !lease.closed ||
					!bytes.Equal(lease.dek, make([]byte, len(lease.dek))) {
					t.Fatalf("released lease=%#v", lease)
				}
			}
		})
	}
}

func newFailingEvidenceExportWorkerFixture(
	t *testing.T,
	ctx context.Context,
	dekProvider EvidenceExportDEKProvider,
	evidenceProviders ...EvidenceProvider,
) (*EvidenceExportWorker, *sqlcipher.Database, *memoryExportDestination, string, string) {
	t.Helper()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	descriptors := testAuditDescriptorSet(t)
	seedAuditChainWithFingerprint(t, ctx, database, descriptors)
	validDEK := bytes.Repeat([]byte{0x5a}, 32)
	now := time.Date(2026, 8, 4, 7, 30, 0, 0, time.UTC)
	key, _, err := GenerateSigningKey(workspaceID, validDEK, now.Add(-time.Hour), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	persistExportTestKey(t, ctx, database, key)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b091")
	request.Destination.CapabilityId = "01890f60-4d6d-7c12-8f02-6c9129d5b092"
	actor := "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	start, end := uint64(2), uint64(3)
	request.Filter = &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
	}, ActorUserId: &actor, FromTime: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 5, 0, time.UTC)),
		ToTime: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 6, 0, time.UTC)), StartSequence: &start, EndSequence: &end}
	created, err := service.ExportEvidence(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	destination := &memoryExportDestination{reference: request.Destination.CapabilityId}
	evidenceProvider := EvidenceProvider(&recordingEvidenceProvider{})
	if len(evidenceProviders) != 0 {
		evidenceProvider = evidenceProviders[0]
	}
	worker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions:      &auditServiceTransactions{database: database, workspaceID: workspaceID},
		Destinations:      memoryDestinationResolver{destinations: map[string]*memoryExportDestination{destination.reference: destination}},
		EvidenceProviders: map[string]EvidenceProvider{"audit_chain": evidenceProvider},
		DEKProvider:       dekProvider,
		Clock:             clock.Func(func() time.Time { return now }),
		Gate:              writableAuditGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker, database, destination, created.Msg.Job.Id, workspaceID
}

func seedAuditChainWithFingerprint(t *testing.T, ctx context.Context, database *sqlcipher.Database, descriptors []byte) {
	t.Helper()
	transaction, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	fingerprint, err := PersistDescriptorSet(ctx, transaction, descriptors, time.Date(2026, 8, 4, 0, 59, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	if err := InitializeChain(ctx, transaction, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"01890f60-4d6d-7c12-8f02-6c9129d5b071", "01890f60-4d6d-7c12-8f02-6c9129d5b072", "01890f60-4d6d-7c12-8f02-6c9129d5b073"} {
		event, payload := integrationAuditEvent(id)
		event.PayloadSchemaFingerprint = fingerprint[:]
		event.OccurredAt = timestamppb.New(time.Date(2026, 8, 4, 1, 2, 4+index, 0, time.UTC))
		if index == 1 {
			event.Actor.ActorUserId = "01890f60-4d6d-7c12-8f02-6c9129d5b099"
		}
		if _, err := appendStoredEventForTest(ctx, transaction, event, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func seedLargeAuditChainWithFingerprint(
	t *testing.T,
	ctx context.Context,
	database *sqlcipher.Database,
	descriptors []byte,
	eventCount int,
	matchingActor string,
	matchingStride int,
) []uint64 {
	t.Helper()
	if eventCount <= 0 || matchingStride <= 0 {
		t.Fatal("large export fixture requires positive bounds")
	}
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := PersistDescriptorSet(ctx, transaction, descriptors, time.Date(2026, 8, 4, 0, 59, 0, 0, time.UTC))
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	events := make([]StoredEvent, 0, eventCount)
	selected := make([]uint64, 0, eventCount/matchingStride)
	previous := genesis
	for index := 1; index <= eventCount; index++ {
		stored := verifierEvent(uint64(index), previous)
		stored.Event.Id = fmt.Sprintf("01890f60-4d6d-7c12-8f02-%012x", index)
		stored.Event.PayloadSchemaFingerprint = fingerprint[:]
		stored.Event.GetPayload().GetWorkspaceStateChanged().ReasonCode = fmt.Sprintf("SIGNED_IN_%03d", index)
		stored.PayloadProto, err = proto.MarshalOptions{Deterministic: true}.Marshal(
			stored.Event.GetPayload().GetWorkspaceStateChanged(),
		)
		if err != nil {
			_ = transaction.Rollback()
			t.Fatalf("marshal event %d payload: %v", index, err)
		}
		if index%matchingStride == 0 {
			stored.Event.Actor.ActorUserId = matchingActor
			selected = append(selected, uint64(index))
		} else {
			stored.Event.Actor.ActorUserId = "01890f60-4d6d-7c12-8f02-6c9129d5b099"
		}
		stored, err = reconstructEventWithStoredOpenings(previous, stored.Event, stored.PayloadProto)
		if err != nil {
			_ = transaction.Rollback()
			t.Fatalf("prepare event %d: %v", index, err)
		}
		events = append(events, stored)
		copy(previous[:], stored.Event.EventHash)
	}
	if err := InitializeChain(ctx, transaction, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentSequence: uint64(eventCount), CurrentHead: previous,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	for index := range events {
		stored := events[index]
		if _, err := transaction.ExecContext(ctx, `INSERT INTO audit_events_v1(
			workspace_id, generation, sequence, event_id, event_type, occurred_at, actor_user_id, session_id,
			command_type, affected_resources_proto, payload_type, payload_schema_fingerprint, payload_proto,
			payload_json, canonical_event, event_proto, previous_hash, event_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, stored.Event.WorkspaceId,
			stored.Event.Generation, stored.Event.Sequence, stored.Event.Id, int32(stored.Event.Type),
			formatTimestamp(stored.Event.OccurredAt.AsTime()), stored.Event.Actor.ActorUserId, stored.Event.Actor.SessionId,
			stored.Event.CommandType, stored.AffectedResourcesProto, stored.PayloadType,
			stored.Event.PayloadSchemaFingerprint, stored.PayloadProto, stored.PayloadJSON, stored.CanonicalEvent,
			stored.EventProto, stored.Event.PreviousHash, stored.Event.EventHash); err != nil {
			_ = transaction.Rollback()
			t.Fatalf("insert event %d: %v", index+1, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return selected
}

func seedHistoricalAuditChain(t *testing.T, ctx context.Context, database *sqlcipher.Database,
	currentDescriptors, historicalDescriptors []byte) ([sha256.Size]byte, [sha256.Size]byte) {
	t.Helper()
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 4, 0, 58, 0, 0, time.UTC)
	currentFingerprint, err := PersistDescriptorSet(ctx, transaction, currentDescriptors, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	historicalFingerprint, err := PersistDescriptorSet(ctx, transaction, historicalDescriptors, createdAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	if err := InitializeChain(ctx, transaction, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	for index, fixture := range []struct {
		id          string
		fingerprint [sha256.Size]byte
	}{
		{id: "01890f60-4d6d-7c12-8f02-6c9129d5b0a3", fingerprint: currentFingerprint},
		{id: "01890f60-4d6d-7c12-8f02-6c9129d5b0a4", fingerprint: historicalFingerprint},
	} {
		event, payload := integrationAuditEvent(fixture.id)
		event.PayloadSchemaFingerprint = fixture.fingerprint[:]
		event.OccurredAt = timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3+index, 0, time.UTC))
		if _, err := appendStoredEventForTest(ctx, transaction, event, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return currentFingerprint, historicalFingerprint
}

func createClaimedExportJob(t *testing.T, ctx context.Context, database *sqlcipher.Database, id string, now time.Time) ExportJob {
	t.Helper()
	transaction, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err := EnqueueExportJob(ctx, transaction, ExportJobSpec{ID: id, WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		OperationKey: id[:len(id)-1] + "1", OperationHash: bytes.Repeat([]byte{0x71}, 32), InputHash: bytes.Repeat([]byte{0x72}, 32),
		Filter: &tammyv1.AuditEventFilter{}, SnapshotGeneration: 1, SnapshotSequence: 1, SnapshotHead: bytes.Repeat([]byte{0x73}, 32),
		DestinationProvider: "approved_file", DestinationCapability: id[:len(id)-1] + "2",
		EvidenceProvider: "audit_chain",
		Progress:         &tammyv1.JobProgress{Stage: "COLLECTING", TotalUnits: 1}, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	claim, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	job, err = ClaimExportJob(ctx, claim, job.ID, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Commit(); err != nil {
		t.Fatal(err)
	}
	return job
}

func persistExportTestKey(t *testing.T, ctx context.Context, database *sqlcipher.Database, key SigningKeyRecord) {
	t.Helper()
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistSigningKey(ctx, transaction, key); err != nil {
		t.Fatal(err)
	}
	if err := InitializeSigningKeyState(ctx, transaction, key); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

type memoryExportDestination struct {
	reference            string
	content              []byte
	commitCalls          int
	sqlActive            *bool
	beforeCommit         func() error
	commitErrBeforeWrite error
	commitErrAfterWrite  error
}

func writableAuditGate() *WriteGate {
	gate := NewWriteGate()
	gate.set(true, true)
	return gate
}

func (destination *memoryExportDestination) Reference() string { return destination.reference }
func (destination *memoryExportDestination) AtomicCommit(_ context.Context, archive []byte) error {
	if destination.sqlActive != nil && *destination.sqlActive {
		return errors.New("destination commit overlapped SQL transaction")
	}
	if destination.beforeCommit != nil {
		if err := destination.beforeCommit(); err != nil {
			return err
		}
	}
	destination.commitCalls++
	if destination.commitErrBeforeWrite != nil {
		return destination.commitErrBeforeWrite
	}
	destination.content = append([]byte(nil), archive...)
	return destination.commitErrAfterWrite
}
func (destination *memoryExportDestination) ReadCommitted(context.Context) ([]byte, error) {
	if destination.sqlActive != nil && *destination.sqlActive {
		return nil, errors.New("destination read overlapped SQL transaction")
	}
	if len(destination.content) == 0 {
		return nil, errors.New("not committed")
	}
	return append([]byte(nil), destination.content...), nil
}

type memoryDestinationResolver struct {
	destinations  map[string]*memoryExportDestination
	beforeResolve func() error
}

func (resolver memoryDestinationResolver) Resolve(reference string) (ExportDestination, error) {
	if resolver.beforeResolve != nil {
		if err := resolver.beforeResolve(); err != nil {
			return nil, err
		}
	}
	destination := resolver.destinations[reference]
	if destination == nil {
		return nil, errors.New("not found")
	}
	return destination, nil
}
