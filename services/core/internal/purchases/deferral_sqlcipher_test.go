//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

type deferralCommandPort interface {
	Defer(context.Context, *tammyv1.DeferPurchaseGstClaimRequest) (*tammyv1.DeferPurchaseGstClaimResponse, error)
}

func deferCommand(t *testing.T, repo *purchases.Repository, request *tammyv1.DeferPurchaseGstClaimRequest) (*tammyv1.DeferPurchaseGstClaimResponse, error) {
	t.Helper()
	port, ok := any(repo).(deferralCommandPort)
	if !ok {
		t.Fatal("missing explicit deferral command")
	}
	return port.Defer(context.Background(), request)
}

func deferRequest(t *testing.T, f *postingFixture, state *tammyv1.PurchaseAccounting, portion *tammyv1.PurchaseGstSourcePortion) *tammyv1.DeferPurchaseGstClaimRequest {
	t.Helper()
	key, _ := f.id()
	r := &tammyv1.DeferPurchaseGstClaimRequest{CommandContext: testkit.CommandContext(key), OrganisationId: postingOrg, DocumentId: f.document.Id, ExpectedAccountingVersion: state.AccountingVersion, Portions: []*tammyv1.PurchaseGstSourcePortion{proto.Clone(portion).(*tammyv1.PurchaseGstSourcePortion)}, AffectedPeriod: claimPeriod(), TimingReview: claimReview(), Reason: "Defer this supported unclaimed portion for this period; retain future obligation"}
	claimElect(t, f, key, "DeferPurchaseGstClaim")
	return r
}

func TestDeferralPurchaseSupersededEvidenceCannotCreateObligation(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, basis)
			current := withdrawClaimEvidence(t, f, repo, state)
			request := deferRequest(t, f, current, portion) // Keep the superseded evidence ID.
			before := claimCounts(t, tx)
			_, err = deferCommand(t, repo, request)
			assertClaimUnchanged(t, tx, before)
			if err == nil {
				t.Fatal("superseded evidence created a future obligation after the latest review withdrew support")
			}
		})
	}
}

func TestDeferralPurchaseRetainsFutureObligationWithoutMovingSuspense(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	before := claimCounts(t, tx)
	result, claimErr := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if got := postScalar(t, tx, `SELECT count(*) FROM purchase_gst_deferrals_v1`); got != 1 {
		t.Fatalf("future claim obligation count=%d want1 (error %v)", got, claimErr)
	}
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if result.Deferral.DeferredGst.MinorUnits != 500 || result.Accounting.SuspenseGst.MinorUnits != 500 || result.Accounting.AttributedGst.MinorUnits != 0 || result.Accounting.AccountingVersion != state.AccountingVersion+1 || !proto.Equal(result.Deferral.Portions[0], portion) {
		t.Fatalf("deferral lost retained obligation: %v", result)
	}
	for _, key := range []string{"1150", "1170", `SELECT count(*) FROM journals`, `SELECT ledger_revision FROM financial_revisions WHERE id=1`, `SELECT settlement_revision FROM financial_revisions WHERE id=1`} {
		if got := claimCounts(t, tx)[key]; got != before[key] {
			t.Fatalf("deferral changed %s from%d to%d", key, before[key], got)
		}
	}
}

func TestDeferralPurchaseSurvivesRestartAndClosesOnlyLinkedPortion(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	r := deferRequest(t, f, state, portion)
	result, err := deferCommand(t, repo, r)
	if err != nil {
		t.Fatal(err)
	}
	old := append([]byte(nil), paymentEventBytes(t, tx, result.Accounting.AccountingVersion)...)
	if postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims WHERE operation_key=? AND domain_mask=8`, r.CommandContext.IdempotencyKey) != 1 {
		t.Fatal("deferral did not use one tax-only revision")
	}
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
	read, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	f.executor = read
	repo = f.repo(read)
	current, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil || !proto.Equal(current, result.Accounting) {
		t.Fatalf("restart future claim=%v %v", current, err)
	}
	claim := claimRequest(t, f, current, portion)
	claim.DeferralId = &result.Deferral.Id
	attributed, err := attributeCommand(t, repo, claim)
	if err != nil {
		t.Fatal(err)
	}
	if attributed.Attributions[0].GetClosesDeferralId() != result.Deferral.Id || !bytes.Equal(old, paymentEventBytes(t, read, result.Accounting.AccountingVersion)) {
		t.Fatal("attribution did not retain immutable deferral link")
	}
	if postScalar(t, read, `SELECT count(*) FROM purchase_gst_deferrals_v1 WHERE id=?`, result.Deferral.Id) != 1 || postScalar(t, read, `SELECT count(*) FROM purchase_gst_attributions_v1 WHERE closes_deferral_id=?`, result.Deferral.Id) != 1 {
		t.Fatal("future obligation erased or not linked")
	}
	if got, err := repo.Get(ctx, postingOrg, f.document.Id); err != nil || !proto.Equal(got, attributed.Accounting) {
		t.Fatalf("linked future claim projection=%v %v", got, err)
	}
}

func TestDeferralPurchaseInvalidFactsLeaveLedgerAndDecisionsUntouched(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*tammyv1.DeferPurchaseGstClaimRequest)
	}{
		{"blank reason", func(r *tammyv1.DeferPurchaseGstClaimRequest) { r.Reason = " " }},
		{"unknown deadline", func(r *tammyv1.DeferPurchaseGstClaimRequest) { r.Portions[0].DueDateEvidence = nil }},
		{"unsupported date", func(r *tammyv1.DeferPurchaseGstClaimRequest) { r.Portions[0].RequiredReturnDueDate = nil }},
		{"changed payment", func(r *tammyv1.DeferPurchaseGstClaimRequest) { r.Portions[0].GetPayment().PaymentHash[0] ^= 1 }},
		{"earlier affected period", func(r *tammyv1.DeferPurchaseGstClaimRequest) {
			r.AffectedPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(8, 1), PeriodEnd: paymentDate(8, 31)}
		}},
		{"future held", func(r *tammyv1.DeferPurchaseGstClaimRequest) { r.TimingReview.ReviewedAsOf = paymentDate(9, 30) }},
		{"expired", func(r *tammyv1.DeferPurchaseGstClaimRequest) {
			r.TimingReview.ReviewedAsOf = &tammyv1.CivilDate{Year: 2030, Month: 11, Day: 4}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			r := deferRequest(t, f, state, portion)
			test.change(r)
			before := claimCounts(t, tx)
			_, err = deferCommand(t, repo, r)
			assertClaimUnchanged(t, tx, before)
			if err == nil {
				t.Fatal("invalid deferral accepted")
			}
		})
	}
}

func TestDeferralPurchasePeriodSpecificDecisionAndExpiredFutureState(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	result, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	// A future period can record a new decision; a same-period decision cannot
	// replace the original retained obligation or duplicate its amount.
	repeat := deferRequest(t, f, result.Accounting, portion)
	before := claimCounts(t, tx)
	_, err = deferCommand(t, repo, repeat)
	assertClaimUnchanged(t, tx, before)
	if err == nil {
		t.Fatal("same-period deferral duplicated")
	}
	f.now = time.Date(2030, 11, 4, 12, 0, 0, 0, time.UTC)
	current, err := repo.Get(context.Background(), postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	if current.SuspenseGst.MinorUnits != 500 || current.AccountingVersion != result.Accounting.AccountingVersion || current.ReviewOutcomes.Timing != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED {
		t.Fatalf("expired outstanding obligation disappeared: %v", current)
	}
}

func TestDeferralPurchasePartialAttributionLeavesOtherPortionOutstanding(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, first := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	paid, err := repo.RecordPayment(ctx, paymentRequest(t, f, state.AccountingVersion, 5500, paymentDate(9, 29)))
	if err != nil {
		t.Fatal(err)
	}
	second := proto.Clone(first).(*tammyv1.PurchaseGstSourcePortion)
	second.Source = &tammyv1.PurchaseGstSourcePortion_Payment{Payment: &tammyv1.PurchasePaymentPortionRef{PostingId: paid.Payment.PostingId, PostingHash: paid.Accounting.Posting.SourceHash, PaymentId: paid.Payment.Id, PaymentHash: paid.Payment.SourceHash, AllocationSequence: 1}}
	second.NormalEventDate = paid.Payment.ActualPaymentDate
	r := deferRequest(t, f, paid.Accounting, first)
	r.Portions = append(r.Portions, second)
	deferred, err := deferCommand(t, repo, r)
	if err != nil {
		t.Fatal(err)
	}
	old := paymentEventBytes(t, tx, deferred.Accounting.AccountingVersion)
	claim := claimRequest(t, f, deferred.Accounting, first)
	claim.DeferralId = &deferred.Deferral.Id
	claimed, err := attributeCommand(t, repo, claim)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Accounting.SuspenseGst.MinorUnits != 500 || claimed.Accounting.AttributedGst.MinorUnits != 500 || !bytes.Equal(old, paymentEventBytes(t, tx, deferred.Accounting.AccountingVersion)) {
		t.Fatal("partial attribution changed deferral or consumed other portion")
	}
	f.now = time.Date(2030, 11, 4, 12, 0, 0, 0, time.UTC)
	current, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	if current.ReviewOutcomes.Timing != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED || current.SuspenseGst.MinorUnits != 500 {
		t.Fatal("remaining expired future portion disappeared")
	}
	verified, err := repo.VerifiedClaims(ctx, postingOrg, f.document.Id)
	if err != nil || len(verified.Deferrals) != 1 || len(verified.Attributions) != 1 || len(verified.Deferrals[0].Portions) != 2 {
		t.Fatalf("complete future history=%v %v", verified, err)
	}
	if len(verified.DeferralPortions) != 2 || verified.DeferralPortions[0].DeferredGST.MinorUnits != 500 || verified.DeferralPortions[0].UnclaimedGST.MinorUnits != 0 || verified.DeferralPortions[1].DeferredGST.MinorUnits != 500 || verified.DeferralPortions[1].UnclaimedGST.MinorUnits != 500 || verified.DeferralPortions[0].DeferralID != deferred.Deferral.Id || verified.DeferralPortions[1].Sequence != 2 || !proto.Equal(verified.DeferralPortions[1].Portion, second) {
		t.Fatalf("individual future extent duplicated or lost: %v", verified.DeferralPortions)
	}
}

func TestDeferralPurchaseLateFailureRollsBackObligationPortionsAndTaxRevision(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	r := deferRequest(t, f, state, p)
	before := claimCounts(t, tx)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	command, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deferCommand(t, f.repo(claimEventFailure{command}), r); err == nil {
		t.Fatal("late deferral failure ignored")
	}
	if postScalar(t, command, `SELECT count(*) FROM purchase_gst_deferrals_v1`) != 1 || postScalar(t, command, `SELECT count(*) FROM purchase_gst_deferral_portions_v1`) != 1 {
		t.Fatal("failure did not follow retained obligation and portion")
	}
	if err := command.Rollback(); err != nil {
		t.Fatal(err)
	}
	read, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	assertClaimUnchanged(t, read, before)
}

func TestDeferralPurchaseCorruptPortionHashCannotBeConsumed(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferred, err := deferCommand(t, repo, deferRequest(t, f, state, p))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_gst_deferral_portions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_gst_deferral_portions_v1 SET source_hash=zeroblob(32) WHERE deferral_id=?`, deferred.Deferral.Id); err != nil {
		t.Fatal(err)
	}
	before := claimCounts(t, tx)
	claim := claimRequest(t, f, deferred.Accounting, p)
	claim.DeferralId = &deferred.Deferral.Id
	_, err = attributeCommand(t, repo, claim)
	assertClaimUnchanged(t, tx, before)
	if err == nil {
		t.Fatal("corrupt obligation consumed")
	}
}

func TestDeferralPurchaseTrustedExpiryCannotBeBypassedByBackdatedReview(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	r := deferRequest(t, f, state, p)
	f.now = time.Date(2030, 11, 4, 12, 0, 0, 0, time.UTC)
	before := claimCounts(t, tx)
	_, err = deferCommand(t, repo, r)
	assertClaimUnchanged(t, tx, before)
	if err == nil {
		t.Fatal("expired future obligation accepted with old review date")
	}
}

func TestDeferralPurchaseOutstandingClaimRequiresExplicitOwnedClosingLink(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	d, err := deferCommand(t, repo, deferRequest(t, f, state, p))
	if err != nil {
		t.Fatal(err)
	}
	r := claimRequest(t, f, d.Accounting, p) // Deliberately omit selected closing decision.
	before := claimCounts(t, tx)
	_, err = attributeCommand(t, repo, r)
	assertClaimUnchanged(t, tx, before)
	if err == nil {
		t.Fatal("outstanding obligation consumed without retained closing link")
	}
}

func TestDeferralPurchaseLaterPeriodReaffirmsSameFuturePortionAndRetainsBothDecisions(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	first, err := deferCommand(t, repo, deferRequest(t, f, state, p))
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2027, 1, 7, 12, 0, 0, 0, time.UTC)
	reviewDate := &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 7}
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2027, Month: 3, Day: 31}}
	request := deferRequest(t, f, first.Accounting, p)
	request.AffectedPeriod = period
	request.TimingReview.ReviewedAsOf = reviewDate
	second, err := deferCommand(t, repo, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Accounting.SuspenseGst.MinorUnits != 500 || second.Deferral.DeferredGst.MinorUnits != 500 || proto.Equal(first.Deferral.AffectedPeriod, second.Deferral.AffectedPeriod) {
		t.Fatal("later period decision changed physical source balance")
	}
	claim := claimRequest(t, f, second.Accounting, p)
	claim.ClaimPeriod = period
	claim.ReleasePostingDate = reviewDate
	claim.TimingReview.ReviewedAsOf = reviewDate
	claim.DeferralId = &second.Deferral.Id
	claimed, err := attributeCommand(t, repo, claim)
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2030, 11, 4, 12, 0, 0, 0, time.UTC)
	current, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	if current.SuspenseGst.MinorUnits != 0 || current.AttributedGst.MinorUnits != 500 || current.ReviewOutcomes.Timing != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
		t.Fatal("older period decision created phantom expired credit")
	}
	verified, err := repo.VerifiedClaims(ctx, postingOrg, f.document.Id)
	if err != nil || len(verified.Deferrals) != 2 || len(verified.Attributions) != 1 || verified.Attributions[0].GetClosesDeferralId() != second.Deferral.Id || !proto.Equal(claimed.Attributions[0].Portion, p) {
		t.Fatalf("reaffirmed retained history=%v %v", verified, err)
	}
	if len(verified.DeferralPortions) != 2 || verified.DeferralPortions[0].DeferredGST.MinorUnits != 500 || verified.DeferralPortions[1].DeferredGST.MinorUnits != 500 || verified.DeferralPortions[0].UnclaimedGST.MinorUnits != 0 || verified.DeferralPortions[1].UnclaimedGST.MinorUnits != 0 {
		t.Fatal("reaffirmed decisions duplicated remaining physical credit")
	}
}

func TestDeferralPurchaseRehashedSamePeriodDuplicateDecisionIsRejected(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	first, err := deferCommand(t, repo, deferRequest(t, f, state, p))
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2027, 1, 7, 12, 0, 0, 0, time.UTC)
	request := deferRequest(t, f, first.Accounting, p)
	request.AffectedPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2027, Month: 3, Day: 31}}
	request.TimingReview.ReviewedAsOf = &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 7}
	second, err := deferCommand(t, repo, request)
	if err != nil {
		t.Fatal(err)
	}
	d := second.Deferral
	d.AffectedPeriod = proto.Clone(first.Deferral.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
	d.SourceHash = nil
	unsigned, _ := proto.MarshalOptions{Deterministic: true}.Marshal(d)
	sourceHash := sha256.Sum256(unsigned)
	d.SourceHash = sourceHash[:]
	retained, _ := proto.MarshalOptions{Deterministic: true}.Marshal(d)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_gst_deferrals_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_gst_deferrals_v1 SET affected_period_start='2026-10-01',affected_period_end='2026-12-31',deferral_proto=?,source_hash=? WHERE id=?`, retained, sourceHash[:], d.Id); err != nil {
		t.Fatal(err)
	}
	content, _ := proto.MarshalOptions{Deterministic: true}.Marshal(second)
	eventHash := sha256.Sum256(content)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_accounting_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_accounting_events_v1 SET result_proto=?,result_sha256=? WHERE operation_key=?`, content, eventHash[:], request.CommandContext.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, postingOrg, f.document.Id); err == nil {
		t.Fatal("rehashed duplicate same-period deferral accepted")
	}
}

func TestDeferralPurchaseRepositoryExists(t *testing.T) {
	if _, ok := any((*purchases.Repository)(nil)).(deferralCommandPort); !ok {
		t.Fatal("purchase repository cannot retain period-specific future claim obligations")
	}
}
