//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"testing"
	"time"
)

func TestVerifyBASSharedTaxFactsNativeWriterCaptures(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 14)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	org, actor := testUUID(9901), testUUID(9902)
	seedSourceScalarOrganisation(t, ctx, tx, org, actor, now)
	// The real accounting writer retains manual evidence whose source ID is
	// the independently created purchase document. It remains a manual fact;
	// proof byte reuse does not classify it as a purchase monetary effect.
	seedSourceScalarPurchaseDocument(t, ctx, tx, org, actor, now, testUUID(9928), testUUID(9912))
	seedSourceScalarTaxFact(t, ctx, tx, org, now, testUUID(9920), testUUID(9921), testUUID(9922), testUUID(9923), testUUID(9924), testUUID(9925), testUUID(9926))
	purchaseRepo, err := purchases.NewRepository(tx, func() time.Time { return now }, func() (string, error) { return testUUID(9999), nil })
	if err != nil {
		t.Fatal(err)
	}
	accountRepo, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	beforeChanges := querySourceScalarInt(t, tx, "SELECT total_changes()")
	beforeRevisions := sourceScalarRevisionState(t, tx)
	purchaseSize, err := purchaseRepo.PreflightCoverageForBASWithPayloadBudget(ctx, org, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	accountingSize, err := accountRepo.PreflightTaxFactsForReportingWithPayloadBudget(ctx, org, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	purchaseCapture, err := purchaseRepo.CaptureCoverageForBAS(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	accountingCapture, err := accountRepo.CaptureTaxFactsForReporting(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := verifyBASSharedTaxFacts(ctx, org, purchaseCapture, accountingCapture)
	if err != nil || proof == nil || proof.Count != 1 || proof.PurchaseBytes != purchaseSize.TaxFactPayloadBytes || proof.AccountingSubsetBytes != accountingSize.BasePayloadBytes || proof.PurchaseBytes == proof.AccountingSubsetBytes {
		t.Fatalf("native raw/canonical fact proof=%#v err=%v; purchase size=%#v accounting size=%#v", proof, err, purchaseSize, accountingSize)
	}
	if accountingCapture.FactCount != 1 || len(accountingCapture.Facts) != 1 || accountingCapture.Facts[0].SourceType != "manual_evidence" {
		t.Fatalf("shared byte proof lost the independent manual fact: %#v", accountingCapture)
	}

	if err := verifyBASSharedJournalCaptureProofs(ctx, org, &invoices.BASReportingCapture{}, purchaseCapture, accountingCapture); err != nil {
		t.Fatalf("native shared complete journal proofs: %v", err)
	}
	assertSourceScalarTransactionUnchanged(t, tx, beforeChanges, beforeRevisions)
}
