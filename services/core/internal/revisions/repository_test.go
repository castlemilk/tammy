//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package revisions_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/revisions"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const (
	revisionOperationOne   = "018f0000-0000-7000-8000-000000000070"
	revisionOperationTwo   = "018f0000-0000-7000-8000-000000000071"
	revisionOperationThree = "018f0000-0000-7000-8000-000000000072"
)

func TestRevisionUnitOfWorkBumpsFinancialAndSelectedDomainsOnce(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	ctx := context.Background()
	tx, err := workspace.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := clock.NewFixed(time.Date(2026, 8, 4, 10, 11, 12, 13, time.UTC))
	repository, err := revisions.New(tx, source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := repository.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Financial != 0 {
		t.Fatalf("read changed financial revision: %#v", before)
	}
	seedIdempotency(t, tx, revisionOperationOne)
	domains := revisions.Domains{Ledger: true, TaxSource: true}
	after, replay, err := repository.Bump(ctx, revisionOperationOne, domains)
	if err != nil {
		t.Fatal(err)
	}
	if replay {
		t.Fatal("first bump reported a replay")
	}
	if after.Financial != 1 || after.Ledger != 1 || after.TaxSource != 1 || after.Banking != 0 {
		t.Fatalf("unexpected revision vector: %#v", after)
	}
	secondRepository, err := revisions.New(tx, source)
	if err != nil {
		t.Fatal(err)
	}
	retained, replay, err := secondRepository.Bump(ctx, revisionOperationOne, domains)
	if err != nil || !replay || retained.Financial != after.Financial {
		t.Fatalf("second constructor replay = %#v, replay=%t, error=%v", retained, replay, err)
	}
	if _, replay, err := secondRepository.Bump(ctx, revisionOperationOne, revisions.Domains{Banking: true}); !replay || !errors.Is(err, revisions.ErrReplayConflict) {
		t.Fatalf("changed-domain replay = replay:%t error:%v", replay, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	replayTx, err := workspace.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayRepository, err := revisions.New(replayTx, source)
	if err != nil {
		t.Fatal(err)
	}
	retained, replay, err = replayRepository.Bump(ctx, revisionOperationOne, domains)
	if err != nil || !replay || retained.Financial != 1 {
		t.Fatalf("committed replay = %#v, replay=%t, error=%v", retained, replay, err)
	}
	if err := replayTx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, workspace, 1, 1, 1)
}

func TestRevisionVectorPersistsEveryDomain(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	ctx := context.Background()
	tx, err := workspace.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := revisions.New(tx, clock.NewFixed(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	seedIdempotency(t, tx, revisionOperationTwo)
	snapshot, replay, err := repository.Bump(ctx, revisionOperationTwo, revisions.Domains{
		Ledger: true, Settlement: true, Banking: true, TaxSource: true,
		OrganisationProfile: true, RuleBundle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay {
		t.Fatal("new full-vector bump reported replay")
	}
	if snapshot.Financial != 1 || snapshot.Ledger != 1 || snapshot.Settlement != 1 ||
		snapshot.Banking != 1 || snapshot.TaxSource != 1 ||
		snapshot.OrganisationProfile != 1 || snapshot.RuleBundle != 1 {
		t.Fatalf("full revision vector = %#v", snapshot)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionRollbackAndEmptySelectionDoNotIncrement(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	ctx := context.Background()
	tx, err := workspace.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := revisions.New(tx, clock.NewFixed(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Bump(ctx, revisionOperationThree, revisions.Domains{}); !errors.Is(err, revisions.ErrNoDomain) {
		t.Fatalf("empty bump error = %v", err)
	}
	seedIdempotency(t, tx, revisionOperationThree)
	if _, replay, err := repository.Bump(ctx, revisionOperationThree, revisions.Domains{Settlement: true}); err != nil || replay {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, workspace, 0, 0, 0)
}

func seedIdempotency(t *testing.T, transaction *sql.Tx, operationKey string) {
	t.Helper()
	_, err := transaction.Exec(`
		INSERT INTO idempotency_records(
			operation_key, command_type, semantic_sha256, result_type,
			result_proto, state, created_at
		) VALUES (?, 'tammy.v1.TestCommand', ?, 'tammy.v1.TestResult', ?, 'ELECTED', ?)`,
		operationKey, "0000000000000000000000000000000000000000000000000000000000000000",
		[]byte{1}, "2026-08-04T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
}

func assertSnapshot(t *testing.T, workspace *testkit.EncryptedWorkspace, financial, ledger, tax uint64) {
	t.Helper()
	tx, err := workspace.Database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	repository, err := revisions.New(tx, clock.NewFixed(time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Financial != financial || snapshot.Ledger != ledger || snapshot.TaxSource != tax {
		t.Fatalf("revision vector = %#v", snapshot)
	}
}
