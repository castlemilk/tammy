//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func seedPurchaseBASScalarFullModernFixture(t *testing.T, tx app.CommandSQLExecutor) *postingFixture {
	t.Helper()
	f, repository, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferral, err := deferCommand(t, repository, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatalf("retain full-claim deferral: %v", err)
	}
	claim := claimRequest(t, f, deferral.Accounting, portion)
	claim.DeferralId = proto.String(deferral.Deferral.Id)
	if _, err := attributeCommand(t, repository, claim); err != nil {
		t.Fatalf("retain source attribution: %v", err)
	}
	return f
}

func TestPurchaseBASScalarReadCancellationAfterKeyQueryIsReadOnly(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := seedPurchaseBASScalarFullModernFixture(t, tx)
	repository := f.repo(tx)
	reservation, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || reservation == nil || reservation.ScalarKeyCount == 0 {
		t.Fatalf("reserve nonempty cancellation fixture: %#v %v", reservation, err)
	}
	before := purchaseBASScalarState(t, tx)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	limitedRepository := f.repo(probe)
	if got, err := limitedRepository.ReadBASKeys(ctx, postingOrg, *reservation, reservation.ScalarKeyCount-1, reservation.ScalarBytes); got != nil {
		t.Fatalf("one-short key bound returned %d keys", len(got))
	} else {
		assertPurchaseCoverageBound(t, err, "BAS scalar keys", reservation.ScalarKeyCount-1)
	}
	if got, err := limitedRepository.ReadBASKeys(ctx, postingOrg, *reservation, reservation.ScalarKeyCount, reservation.ScalarBytes-1); got != nil {
		t.Fatalf("one-short byte bound returned %d keys", len(got))
	} else {
		assertPurchaseCoverageBound(t, err, "BAS scalar bytes", reservation.ScalarBytes-1)
	}
	if probe.summaryReads != 0 || probe.keyReads != 0 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("one-short reservation bound read summary/keys/bodies or changed caller transaction state")
	}
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	probe.afterKeyQuery = cancel
	got, err := f.repo(probe).ReadBASKeys(cancelled, postingOrg, *reservation, reservation.ScalarKeyCount, reservation.ScalarBytes)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel after key query returned keys=%d err=%v", len(got), err)
	}
	if probe.keyReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("cancelled scalar read returned bodies, changed retained state, or ended caller transaction")
	}
}

func TestPurchaseBASScalarKeysSurviveEncryptedReopenAndCallerRollback(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := seedPurchaseBASScalarFullModernFixture(t, tx)
	originalRepository := f.repo(tx)
	originalSummary, err := originalRepository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || originalSummary == nil {
		t.Fatalf("reserve original encrypted source: %#v %v", originalSummary, err)
	}
	originalKeys, err := originalRepository.ReadBASKeys(ctx, postingOrg, *originalSummary, originalSummary.ScalarKeyCount, originalSummary.ScalarBytes)
	if err != nil {
		t.Fatalf("read original encrypted keys: %v", err)
	}
	originalCounts := coverageStateCounts(t, tx)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	readTx, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	reopenedRepository := f.repo(readTx)
	reopenedSummary, err := reopenedRepository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || reopenedSummary == nil || !reflect.DeepEqual(originalSummary, reopenedSummary) {
		t.Fatalf("reopened scalar reservation=%#v err=%v; original=%#v", reopenedSummary, err, originalSummary)
	}
	reopenedKeys, err := reopenedRepository.ReadBASKeys(ctx, postingOrg, *reopenedSummary, reopenedSummary.ScalarKeyCount, reopenedSummary.ScalarBytes)
	if err != nil || !reflect.DeepEqual(originalKeys, reopenedKeys) {
		t.Fatalf("encrypted reopen keys changed: err=%v", err)
	}
	if got := coverageStateCounts(t, readTx); !reflect.DeepEqual(originalCounts, got) {
		t.Fatalf("reopened read changed retained state: before=%v after=%v", originalCounts, got)
	}

	// A caller-owned unreviewed document remains visible to scalar reads in its
	// transaction, then disappears after Rollback without changing the baseline.
	f.executor = readTx
	f.addDocumentReview("Caller draft", "SCALAR-CALLER-DRAFT", 0, 0, false)
	draftID := f.document.Id
	draftSummary, err := f.repo(readTx).SummarizeBASKeys(ctx, postingOrg)
	if err != nil || draftSummary == nil || draftSummary.RootCount != reopenedSummary.RootCount+1 ||
		draftSummary.ScalarKeyCount != reopenedSummary.ScalarKeyCount+1 {
		t.Fatalf("caller draft scalar summary=%#v err=%v; baseline=%#v", draftSummary, err, reopenedSummary)
	}
	draftKeys, err := f.repo(readTx).ReadBASKeys(ctx, postingOrg, *draftSummary, draftSummary.ScalarKeyCount, draftSummary.ScalarBytes)
	if err != nil {
		t.Fatalf("read caller draft key: %v", err)
	}
	foundDraft := false
	for _, key := range draftKeys {
		if key.Kind == purchases.BASScalarDocument && key.ID == draftID {
			foundDraft = key.IsRoot
		}
	}
	if !foundDraft {
		t.Fatal("caller-owned unreviewed document was not visible as a root scalar key")
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	nextTx, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer nextTx.Rollback()
	nextRepository := f.repo(nextTx)
	nextSummary, err := nextRepository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || nextSummary == nil || !reflect.DeepEqual(originalSummary, nextSummary) {
		t.Fatalf("post-rollback scalar reservation=%#v err=%v; original=%#v", nextSummary, err, originalSummary)
	}
	nextKeys, err := nextRepository.ReadBASKeys(ctx, postingOrg, *nextSummary, nextSummary.ScalarKeyCount, nextSummary.ScalarBytes)
	if err != nil || !reflect.DeepEqual(originalKeys, nextKeys) {
		t.Fatalf("post-rollback scalar keys changed: err=%v", err)
	}
	if postScalar(t, nextTx, `SELECT count(*) FROM documents WHERE id=?`, draftID) != 0 ||
		!reflect.DeepEqual(originalCounts, coverageStateCounts(t, nextTx)) {
		t.Fatal("caller draft survived rollback or changed committed source counts")
	}
}
