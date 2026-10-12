//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"fmt"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func TestPurchaseCoveragePublicPreflightPreservesPreviousFingerprint(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	seedCoverageCashReceiptWithTaxFactSource(t, f, f.document.Id, f.document.Id)
	posted := postReportingPurchase(t, f, "PREFLIGHT-COMPAT", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if _, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, posted.Accounting.AccountingVersion, 11000, postDate(20))); err != nil {
		t.Fatal(err)
	}
	legacyOpeningFixture(t, f)
	capture := captureCoverage(t, f.repo(tx), ctx)
	// Recorded through the previous reader before the public preflight refactor.
	const previousFingerprint = "2e032c3b84dc37334b0c87190544922376582579e46526794a82c59450a937a3"
	if got := fmt.Sprintf("%x", capture.Fingerprint); got != previousFingerprint || capture.PayloadBytes != 40979 {
		t.Fatalf("fingerprint=%s payload=%d; want previous fingerprint %s and payload 40979", got, capture.PayloadBytes, previousFingerprint)
	}
}
