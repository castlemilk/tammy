//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

type purchaseBASActualPaymentProbe struct {
	app.CommandSQLExecutor
	keyReads           int
	bodyReads          int
	paymentReplayReads int
}

func (probe *purchaseBASActualPaymentProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "/* BAS purchase scalar keys */") {
		probe.keyReads++
	}
	if strings.Contains(query, "/* BAS purchase owner body payment replay */") {
		probe.paymentReplayReads++
	}
	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		return rows, err
	}
	columns, err := rows.Columns()
	if err == nil {
		for _, column := range columns {
			if purchaseCoverageBodyResultColumn(column) {
				probe.bodyReads++
				break
			}
		}
	}
	return rows, err
}

func purchaseBASActualPaymentFixture(t *testing.T) (*postingFixture, string) {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	fixture, _, _, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	if portion == nil || portion.GetPayment() == nil || portion.GetPayment().PaymentId == "" {
		t.Fatal("claimSetup did not retain its writer-created payment reference")
	}
	return fixture, portion.GetPayment().PaymentId
}

func TestPurchaseBASScalarPaymentActualDateIsRetainedAndCharged(t *testing.T) {
	ctx := context.Background()
	fixture, paymentID := purchaseBASActualPaymentFixture(t)
	repository := fixture.repo(fixture.executor)

	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("summarize writer-created payment date: summary=%#v err=%v", summary, err)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount || purchaseBASScalarIndependentBytes(keys) != summary.ScalarBytes {
		t.Fatalf("exact payment-date scalar read: keys=%d independent bytes=%d summary=%#v err=%v",
			len(keys), purchaseBASScalarIndependentBytes(keys), summary, err)
	}

	key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPayment, paymentID)
	purchaseBASScalarRequireText(t, key.ActualPaymentDate, "2026-09-30", "payment actual-date scalar")
	storedPaymentDate := purchaseBASScalarText(t, fixture.executor,
		`SELECT payment_date FROM purchase_payments_v1 WHERE id=? AND organisation_id=?`, paymentID, postingOrg)
	storedJournalDate := purchaseBASScalarText(t, fixture.executor,
		`SELECT journal_date FROM journals WHERE id=? AND organisation_id=?`, *key.JournalID, postingOrg)
	if storedPaymentDate != "2026-09-30" || storedJournalDate != storedPaymentDate {
		t.Fatalf("writer date cells payment=%q journal=%q; want payment date and payment-journal date both 2026-09-30",
			storedPaymentDate, storedJournalDate)
	}
	rows, err := fixture.executor.QueryContext(ctx,
		`SELECT payment_proto FROM purchase_payments_v1 WHERE id=? AND organisation_id=?`, paymentID, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	var paymentContent []byte
	if !rows.Next() || rows.Scan(&paymentContent) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		t.Fatal("missing unique retained payment body")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	retainedPayment := &tammyv1.PurchasePayment{}
	if err := proto.Unmarshal(paymentContent, retainedPayment); err != nil || retainedPayment.Id != paymentID ||
		!proto.Equal(retainedPayment.ActualPaymentDate, paymentDate(9, 30)) {
		t.Fatalf("writer payment body actual date=%v err=%v; want 2026-09-30", retainedPayment.ActualPaymentDate, err)
	}

	short, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes-1)
	var bound *purchases.SourceBoundError
	if short != nil || !errors.As(err, &bound) || bound.Resource != "BAS scalar bytes" || bound.Limit != summary.ScalarBytes-1 {
		t.Fatalf("one-byte-short scalar reservation returned keys=%t bound=%#v err=%v; want BAS scalar bytes/%d",
			short != nil, bound, err, summary.ScalarBytes-1)
	}
}

func TestPurchaseBASScalarRejectsInvalidPaymentActualDateBeforeKeyRead(t *testing.T) {
	cases := []struct {
		name, update string
	}{
		{"invalid_calendar_date", `UPDATE purchase_payments_v1 SET payment_date='2026-02-30' WHERE id=?`},
		{"non_text_storage", `UPDATE purchase_payments_v1 SET payment_date=zeroblob(8) WHERE id=?`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture, paymentID := purchaseBASActualPaymentFixture(t)
			validReservation, err := fixture.repo(fixture.executor).SummarizeBASKeys(ctx, postingOrg)
			if err != nil || validReservation == nil {
				t.Fatalf("reserve valid writer payment before mutation: %#v %v", validReservation, err)
			}
			mutatePurchaseBASScalar(t, fixture, "purchase_payments_v1", test.update, paymentID)
			probe := &purchaseBASActualPaymentProbe{CommandSQLExecutor: fixture.executor}
			repository := fixture.repo(probe)
			if got, err := repository.SummarizeBASKeys(ctx, postingOrg); got != nil || err == nil {
				t.Fatalf("invalid retained payment date summary=%#v err=%v; want rejection", got, err)
			}
			if got, err := repository.ReadBASKeys(ctx, postingOrg, *validReservation,
				validReservation.ScalarKeyCount, validReservation.ScalarBytes); got != nil || err == nil {
				t.Fatalf("invalid retained payment date key read=%t err=%v; want rejection", got != nil, err)
			}
			if probe.keyReads != 0 || probe.bodyReads != 0 {
				t.Fatalf("invalid payment date reached scalar keys/body replay: key reads=%d body reads=%d",
					probe.keyReads, probe.bodyReads)
			}
		})
	}
}

func TestPurchaseBASScalarPaymentActualDateMismatchFailsFullBodyReplay(t *testing.T) {
	ctx := context.Background()
	fixture, paymentID := purchaseBASActualPaymentFixture(t)
	originalJournalDate := purchaseBASScalarText(t, fixture.executor,
		`SELECT j.journal_date FROM purchase_payments_v1 p JOIN journals j ON j.id=p.journal_id WHERE p.id=? AND p.organisation_id=?`,
		paymentID, postingOrg)
	if originalJournalDate != "2026-09-30" {
		t.Fatalf("writer payment-journal date=%q; want 2026-09-30", originalJournalDate)
	}
	// Keep the corrupted date valid and the stored payment body/hash untouched.
	// Scalar selection must accept and expose this TEXT value; mature payment replay
	// must then reject its disagreement with PurchasePayment.ActualPaymentDate.
	mutatePurchaseBASScalar(t, fixture, "purchase_payments_v1",
		`UPDATE purchase_payments_v1 SET payment_date='2026-10-01' WHERE id=?`, paymentID)

	keysRepository := fixture.repo(fixture.executor)
	summary, err := keysRepository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("summarize valid but body-mismatched payment date: %#v %v", summary, err)
	}
	keys, err := keysRepository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil {
		t.Fatalf("read valid but body-mismatched payment date scalar: %v", err)
	}
	key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPayment, paymentID)
	purchaseBASScalarRequireText(t, key.ActualPaymentDate, "2026-10-01", "mutated payment actual-date scalar")
	if originalJournalDate == *key.ActualPaymentDate {
		t.Fatal("mutated payment actual date unexpectedly changed the independently stored journal date")
	}

	probe := &purchaseBASActualPaymentProbe{CommandSQLExecutor: fixture.executor}
	capture, err := fixture.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	if capture != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("capture accepted or partially returned payment/body date mismatch: capture=%+v err=%v", capture, err)
	}
	if probe.paymentReplayReads != 1 {
		t.Fatalf("full capture reached %d mature payment-replay queries before rejecting mismatch; want one", probe.paymentReplayReads)
	}
}
