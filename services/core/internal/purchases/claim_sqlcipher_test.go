//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

type attributionCommandPort interface {
	Attribute(context.Context, *tammyv1.AttributePurchaseGstRequest) (*tammyv1.AttributePurchaseGstResponse, error)
}

func claimPeriod() *tammyv1.PurchaseClaimPeriod {
	return &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(10, 1), PeriodEnd: paymentDate(12, 31)}
}

func claimReview() *tammyv1.PurchaseClaimTimingReview {
	return &tammyv1.PurchaseClaimTimingReview{ReviewedAsOf: paymentDate(10, 7), AllowablePeriodConfirmed: true, OrdinaryTimeLimitConfirmed: true, EvidenceHeldConfirmed: true, Reason: "Reviewed actual required-return due date and allowable claim period against retained ATO notice"}
}

// The notice is separate retained manual supporting evidence. Its candidate
// date is deliberately different from the reviewed actual return due date.
func claimNotice(t *testing.T, f *postingFixture) *tammyv1.PurchaseRetainedSourceRef {
	t.Helper()
	doc, request := f.document, f.request
	f.addDocument("Australian Taxation Office", "Required return notice", 0, 0)
	ref := &tammyv1.PurchaseRetainedSourceRef{SourceId: f.document.Id, SourceVersion: f.document.Version, SourceHash: append([]byte(nil), f.document.Sha256...)}
	f.document, f.request = doc, request
	return ref
}

func claimSetup(t *testing.T, tx app.CommandSQLExecutor, basis tammyv1.GstBasis) (*postingFixture, *purchases.Repository, *tammyv1.PurchaseAccounting, *tammyv1.PurchaseGstSourcePortion) {
	t.Helper()
	f := newPostingFixture(t, tx, basis)
	ready := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
	f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	f.request.Evidence.Source, f.request.Evidence.HeldDate = nil, nil
	repo := f.repo(tx)
	posted, err := repo.PostReviewed(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	state := posted.Accounting
	portion := &tammyv1.PurchaseGstSourcePortion{NormalEventDate: posted.Posting.InvoiceDate, EarliestOtherwiseEligiblePeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(7, 1), PeriodEnd: paymentDate(9, 30)}, RequiredReturnDueDate: paymentDate(11, 3), OrdinaryLastClaimDate: &tammyv1.CivilDate{Year: 2030, Month: 11, Day: 3}}
	if basis == tammyv1.GstBasis_GST_BASIS_CASH {
		paid, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, 1, 5500, paymentDate(9, 30)))
		if err != nil {
			t.Fatal(err)
		}
		state = paid.Accounting
		portion.Source = &tammyv1.PurchaseGstSourcePortion_Payment{Payment: &tammyv1.PurchasePaymentPortionRef{PostingId: posted.Posting.Id, PostingHash: posted.Posting.SourceHash, PaymentId: paid.Payment.Id, PaymentHash: paid.Payment.SourceHash, AllocationSequence: 1}}
		portion.NormalEventDate = paid.Payment.ActualPaymentDate
		if !proto.Equal(paid.Payment.ActualPaymentDate, paymentDate(9, 30)) {
			t.Fatal("cash payment lost actual date")
		}
	} else {
		portion.Source = &tammyv1.PurchaseGstSourcePortion_Posting{Posting: &tammyv1.PurchasePostingPortionRef{PostingId: posted.Posting.Id, PostingHash: posted.Posting.SourceHash, AllocationSequence: 1}}
	}
	before := paymentBalance(t, tx, "1150")
	ready.HeldDate = paymentDate(10, 1)
	evidenced, err := evidenceCommand(t, repo, context.Background(), evidenceRequest(t, f, state.AccountingVersion, ready))
	if err != nil {
		t.Fatal(err)
	}
	if paymentBalance(t, tx, "1150") != before || evidenced.Accounting.AttributedGst.MinorUnits != 0 {
		t.Fatal("evidence append prematurely claimed suspense")
	}
	portion.EvidenceId = evidenced.Evidence.Id
	portion.EvidenceHeldDate = ready.HeldDate
	portion.DueDateEvidence = claimNotice(t, f)
	return f, repo, evidenced.Accounting, portion
}

func claimRequest(t *testing.T, f *postingFixture, state *tammyv1.PurchaseAccounting, portion *tammyv1.PurchaseGstSourcePortion) *tammyv1.AttributePurchaseGstRequest {
	t.Helper()
	key, _ := f.id()
	r := &tammyv1.AttributePurchaseGstRequest{CommandContext: testkit.CommandContext(key), OrganisationId: postingOrg, DocumentId: f.document.Id, ExpectedAccountingVersion: state.AccountingVersion, Portions: []*tammyv1.PurchaseGstSourcePortion{proto.Clone(portion).(*tammyv1.PurchaseGstSourcePortion)}, ClaimPeriod: claimPeriod(), ReleasePostingDate: paymentDate(10, 7), TimingReview: claimReview()}
	claimElect(t, f, key, "AttributePurchaseGst")
	return r
}
func claimElect(t *testing.T, f *postingFixture, key, method string) {
	t.Helper()
	if _, err := f.executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?,?,?,?,'v1',?,?,'ELECTED',?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, "tammy.v1.PurchaseService."+method, key, "tammy.v1."+method+"Request", make([]byte, 32), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}
func claimCounts(t *testing.T, tx app.CommandSQLExecutor) map[string]int64 {
	t.Helper()
	result := evidenceStateCounts(t, tx)
	for _, table := range []string{"purchase_gst_attributions_v1", "purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1"} {
		result[table] = postScalar(t, tx, "SELECT count(*) FROM "+table)
	}
	result["1150"] = paymentBalance(t, tx, "1150")
	result["1170"] = paymentBalance(t, tx, "1170")
	return result
}
func assertClaimUnchanged(t *testing.T, tx app.CommandSQLExecutor, before map[string]int64) {
	t.Helper()
	after := claimCounts(t, tx)
	for key, want := range before {
		if after[key] != want {
			t.Fatalf("rejected claim changed %s from %d to %d", key, want, after[key])
		}
	}
}

func TestAttributePurchaseWrongOwnedSourceCannotMoveSuspense(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	r := claimRequest(t, f, state, portion)
	r.Portions[0].GetPayment().PaymentHash[0] ^= 1
	before := claimCounts(t, tx)
	_, claimErr := attributeCommand(t, repo, r)
	assertClaimUnchanged(t, tx, before)
	if claimErr == nil {
		t.Fatal("changed source hash accepted")
	}
}

func withdrawClaimEvidence(t *testing.T, f *postingFixture, repo *purchases.Repository, state *tammyv1.PurchaseAccounting) *tammyv1.PurchaseAccounting {
	t.Helper()
	withdrawn := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
	withdrawn.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	withdrawn.Source, withdrawn.HeldDate = nil, nil
	withdrawn.Reason = "Withdraw prior classification pending review of the retained evidence"
	result, err := evidenceCommand(t, repo, context.Background(), evidenceRequest(t, f, state.AccountingVersion, withdrawn))
	if err != nil {
		t.Fatal(err)
	}
	if result.Accounting.ReviewOutcomes.Evidence != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE {
		t.Fatalf("withdrawn evidence did not require new support: %v", result.Accounting.ReviewOutcomes)
	}
	return result.Accounting
}

func TestAttributePurchaseSupersededEvidenceCannotMoveSuspense(t *testing.T) {
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
			request := claimRequest(t, f, current, portion) // Keep the superseded evidence ID.
			before := claimCounts(t, tx)
			_, err = attributeCommand(t, repo, request)
			assertClaimUnchanged(t, tx, before)
			if err == nil {
				t.Fatal("superseded evidence moved suspense after the latest review withdrew support")
			}
		})
	}
}

func TestAttributePurchaseLaterEvidenceWithdrawalPreservesHistoricalClaim(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	claimed, err := attributeCommand(t, repo, claimRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	old := paymentEventBytes(t, tx, claimed.Accounting.AccountingVersion)
	current := withdrawClaimEvidence(t, f, repo, claimed.Accounting)
	if current.AttributedGst.MinorUnits != 500 || current.SuspenseGst.MinorUnits != 0 || paymentBalance(t, tx, "1150") != 500 || !bytes.Equal(old, paymentEventBytes(t, tx, claimed.Accounting.AccountingVersion)) {
		t.Fatal("later evidence withdrawal changed the previously accepted claim")
	}
	retained, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil || !proto.Equal(retained, current) {
		t.Fatalf("historical claim did not replay at its evidence cutoff: %v %v", retained, err)
	}
	verified, err := repo.VerifiedClaims(ctx, postingOrg, f.document.Id)
	if err != nil || len(verified.Attributions) != 1 || !proto.Equal(verified.Attributions[0], claimed.Attributions[0]) {
		t.Fatalf("later review invalidated immutable claim provenance: %v %v", verified, err)
	}
}

func TestAttributePurchaseReleasesOnlySelectedOwnedExtentOnceAndRestarts(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, basis)
			old := paymentEventBytes(t, tx, state.AccountingVersion)
			before := claimCounts(t, tx)
			request := claimRequest(t, f, state, portion)
			result, err := attributeCommand(t, repo, request)
			if err != nil {
				t.Fatal(err)
			}
			amount := int64(1000)
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				amount = 500
			}
			if got := paymentBalance(t, tx, "1150"); got != amount {
				t.Fatalf("explicit attribution input GST=%d want %d", got, amount)
			}
			if got := paymentBalance(t, tx, "1170"); got != 0 {
				t.Fatalf("explicit attribution suspense=%d want 0", got)
			}
			if len(result.Attributions) != 1 || result.Accounting.AccountingVersion != state.AccountingVersion+1 || result.Accounting.AttributedGst.MinorUnits != amount || result.Accounting.SuspenseGst.MinorUnits != 0 || result.Attributions[0].ClaimedGst.MinorUnits != amount || !proto.Equal(result.Attributions[0].Portion, portion) || result.Journal.Id != result.Attributions[0].JournalId {
				t.Fatalf("lost retained attribution: %v", result)
			}
			if !bytes.Equal(old, paymentEventBytes(t, tx, state.AccountingVersion)) {
				t.Fatal("claim rewrote historical evidence/payment event")
			}
			if got := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims WHERE operation_key=? AND domain_mask=9`, request.CommandContext.IdempotencyKey); got != 1 {
				t.Fatal("claim did not reserve ledger+tax once")
			}
			if postScalar(t, tx, `SELECT settlement_revision FROM financial_revisions WHERE id=1`) != before[`SELECT settlement_revision FROM financial_revisions WHERE id=1`] {
				t.Fatal("claim changed settlement revision")
			}
			current, err := repo.Get(ctx, postingOrg, f.document.Id)
			if err != nil || !proto.Equal(current, result.Accounting) {
				t.Fatalf("claim projection=%v %v", current, err)
			}
			repeat := claimRequest(t, f, result.Accounting, portion)
			unchanged := claimCounts(t, tx)
			_, err = attributeCommand(t, repo, repeat)
			assertClaimUnchanged(t, tx, unchanged)
			if err == nil {
				t.Fatal("source claimed twice")
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
			retained, err := f.repo(read).Get(ctx, postingOrg, f.document.Id)
			if err != nil || !proto.Equal(retained, result.Accounting) {
				t.Fatalf("restart claim=%v %v", retained, err)
			}
		})
	}
}

func TestAttributePurchaseInvalidFactsRejectBeforeAccounting(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*tammyv1.AttributePurchaseGstRequest)
	}{
		{"wrong payment hash", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].GetPayment().PaymentHash[0] ^= 1 }},
		{"wrong posting hash", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].GetPayment().PostingHash[0] ^= 1 }},
		{"wrong sequence", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].GetPayment().AllocationSequence = 2 }},
		{"posting source on cash", func(r *tammyv1.AttributePurchaseGstRequest) {
			p := r.Portions[0].GetPayment()
			r.Portions[0].Source = &tammyv1.PurchaseGstSourcePortion_Posting{Posting: &tammyv1.PurchasePostingPortionRef{PostingId: p.PostingId, PostingHash: p.PostingHash, AllocationSequence: 1}}
		}},
		{"changed normal date", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].NormalEventDate = paymentDate(9, 29) }},
		{"changed held date", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].EvidenceHeldDate = paymentDate(9, 30) }},
		{"period before event", func(r *tammyv1.AttributePurchaseGstRequest) {
			r.ClaimPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(9, 1), PeriodEnd: paymentDate(9, 29)}
			r.ReleasePostingDate = paymentDate(9, 29)
		}},
		{"release before event", func(r *tammyv1.AttributePurchaseGstRequest) {
			r.ClaimPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(9, 1), PeriodEnd: paymentDate(10, 31)}
			r.ReleasePostingDate = paymentDate(9, 29)
		}},
		{"release outside period", func(r *tammyv1.AttributePurchaseGstRequest) { r.ReleasePostingDate = paymentDate(9, 30) }},
		{"future held at review", func(r *tammyv1.AttributePurchaseGstRequest) { r.TimingReview.ReviewedAsOf = paymentDate(9, 30) }},
		{"future review clock", func(r *tammyv1.AttributePurchaseGstRequest) { r.TimingReview.ReviewedAsOf = paymentDate(10, 8) }},
		{"invoice masquerades as due proof", func(r *tammyv1.AttributePurchaseGstRequest) {
			r.Portions[0].DueDateEvidence = &tammyv1.PurchaseRetainedSourceRef{SourceId: r.DocumentId, SourceVersion: 2, SourceHash: make([]byte, 32)}
		}},
		{"missing due proof", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].DueDateEvidence = nil }},
		{"stale due proof", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].DueDateEvidence.SourceVersion++ }},
		{"changed due hash", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].DueDateEvidence.SourceHash[0] ^= 1 }},
		{"period not containing event", func(r *tammyv1.AttributePurchaseGstRequest) {
			r.Portions[0].EarliestOtherwiseEligiblePeriod = claimPeriod()
		}},
		{"due before earliest end", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].RequiredReturnDueDate = paymentDate(9, 29) }},
		{"nominal anniversary mismatch", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].OrdinaryLastClaimDate.Day = 4 }},
		{"missing actual due date", func(r *tammyv1.AttributePurchaseGstRequest) { r.Portions[0].RequiredReturnDueDate = nil }},
		{"duplicate portion", func(r *tammyv1.AttributePurchaseGstRequest) {
			r.Portions = append(r.Portions, proto.Clone(r.Portions[0]).(*tammyv1.PurchaseGstSourcePortion))
		}},
		{"unconfirmed allowable period", func(r *tammyv1.AttributePurchaseGstRequest) { r.TimingReview.AllowablePeriodConfirmed = false }},
		{"unconfirmed actual due review", func(r *tammyv1.AttributePurchaseGstRequest) { r.TimingReview.OrdinaryTimeLimitConfirmed = false }},
		{"stale version", func(r *tammyv1.AttributePurchaseGstRequest) { r.ExpectedAccountingVersion-- }},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			r := claimRequest(t, f, state, portion)
			test.change(r)
			before := claimCounts(t, tx)
			_, err = attributeCommand(t, repo, r)
			assertClaimUnchanged(t, tx, before)
			if err == nil {
				t.Fatal("invalid reviewed claim accepted")
			}
		})
	}
}

// Corruption includes a recomputed hash: complete historical accounting and
// row/source equality must still reject a forged retained response.
func TestAttributePurchaseRehashedHistoricalResultIsRejected(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	r := claimRequest(t, f, state, portion)
	result, err := attributeCommand(t, repo, r)
	if err != nil {
		t.Fatal(err)
	}
	result.Accounting.SuspenseGst.MinorUnits++
	content, _ := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	hash := sha256.Sum256(content)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_accounting_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_accounting_events_v1 SET result_proto=?,result_sha256=? WHERE operation_key=?`, content, hash[:], r.CommandContext.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, postingOrg, f.document.Id); err == nil {
		t.Fatal("rehashed forged claim result accepted")
	}
}

func TestAttributePurchaseTwoPaymentSourcesKeepDistinctDatesAndRequiredReturns(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, first := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	// Evidence is held on1Oct; the second actual payment is earlier than that
	// date, so both selected sources remain suspense even after evidence append.
	paid, err := repo.RecordPayment(ctx, paymentRequest(t, f, state.AccountingVersion, 5500, paymentDate(9, 29)))
	if err != nil {
		t.Fatal(err)
	}
	second := proto.Clone(first).(*tammyv1.PurchaseGstSourcePortion)
	second.Source = &tammyv1.PurchaseGstSourcePortion_Payment{Payment: &tammyv1.PurchasePaymentPortionRef{PostingId: paid.Payment.PostingId, PostingHash: paid.Accounting.Posting.SourceHash, PaymentId: paid.Payment.Id, PaymentHash: paid.Payment.SourceHash, AllocationSequence: 1}}
	second.NormalEventDate = paid.Payment.ActualPaymentDate
	second.RequiredReturnDueDate = paymentDate(11, 17)
	second.OrdinaryLastClaimDate = &tammyv1.CivilDate{Year: 2030, Month: 11, Day: 17}
	r := claimRequest(t, f, paid.Accounting, first)
	r.Portions = append(r.Portions, second)
	before := postScalar(t, tx, `SELECT count(*) FROM journals`)
	result, err := attributeCommand(t, repo, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Attributions) != 2 || result.Attributions[0].AccountingVersion != result.Attributions[1].AccountingVersion || result.Attributions[0].JournalId != result.Attributions[1].JournalId || result.Attributions[0].ClaimedGst.MinorUnits != 500 || result.Attributions[1].ClaimedGst.MinorUnits != 500 || postScalar(t, tx, `SELECT count(*) FROM journals`) != before+1 {
		t.Fatalf("multi-source claim did not share one journal/version: %v", result)
	}
	if !proto.Equal(result.Attributions[0].Portion, first) || !proto.Equal(result.Attributions[1].Portion, second) {
		t.Fatal("source event or actual due date merged")
	}
	if current, err := repo.Get(ctx, postingOrg, f.document.Id); err != nil || !proto.Equal(current, result.Accounting) {
		t.Fatalf("multi-source replay=%v %v", current, err)
	}
}

func TestAttributePurchaseAutomaticClaimCannotBeClaimedOrDeferredAgain(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	repo := f.repo(tx)
	posted, err := repo.PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	paid, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 5500, paymentDate(9, 30)))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM purchase_tax_evidence_v1 WHERE accounting_version=1`)
	if err != nil {
		t.Fatal(err)
	}
	var evidenceID string
	if !rows.Next() || rows.Scan(&evidenceID) != nil {
		t.Fatal("missing initial evidence")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	p := &tammyv1.PurchaseGstSourcePortion{Source: &tammyv1.PurchaseGstSourcePortion_Payment{Payment: &tammyv1.PurchasePaymentPortionRef{PostingId: posted.Posting.Id, PostingHash: posted.Posting.SourceHash, PaymentId: paid.Payment.Id, PaymentHash: paid.Payment.SourceHash, AllocationSequence: 1}}, NormalEventDate: paid.Payment.ActualPaymentDate, EarliestOtherwiseEligiblePeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(7, 1), PeriodEnd: paymentDate(9, 30)}, RequiredReturnDueDate: paymentDate(11, 3), DueDateEvidence: claimNotice(t, f), OrdinaryLastClaimDate: &tammyv1.CivilDate{Year: 2030, Month: 11, Day: 3}, EvidenceId: evidenceID, EvidenceHeldDate: postDate(10)}
	before := claimCounts(t, tx)
	_, err = attributeCommand(t, repo, claimRequest(t, f, paid.Accounting, p))
	assertClaimUnchanged(t, tx, before)
	if err == nil {
		t.Fatal("automatic credit claimed twice")
	}
	_, err = deferCommand(t, repo, deferRequest(t, f, paid.Accounting, p))
	assertClaimUnchanged(t, tx, before)
	if err == nil {
		t.Fatal("automatic credit deferred")
	}
}

func TestAttributePurchaseClosedPeriodAndExpiredLimitCannotMoveSuspense(t *testing.T) {
	for _, kind := range []string{"closed", "expired", "future support review", "unreviewed proof"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			r := claimRequest(t, f, state, p)
			switch kind {
			case "closed":
				if _, err := tx.ExecContext(ctx, `INSERT INTO accounting_periods(id,organisation_id,start_date,end_date,state,closed_at) VALUES(?,?,'2026-10-01','2026-10-07','CLOSED',?)`, "018f0000-0000-7000-8000-000000009996", postingOrg, f.now.Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
			case "expired":
				f.now = time.Date(2030, 11, 4, 12, 0, 0, 0, time.UTC)
				r.TimingReview.ReviewedAsOf = &tammyv1.CivilDate{Year: 2030, Month: 11, Day: 4}
			case "future support review":
				r.TimingReview.ReviewedAsOf = paymentDate(10, 6)
			case "unreviewed proof":
				doc, request := f.document, f.request
				f.addDocumentReview("ATO", "unreviewed notice", 0, 0, false)
				r.Portions[0].DueDateEvidence = &tammyv1.PurchaseRetainedSourceRef{SourceId: f.document.Id, SourceVersion: 1, SourceHash: f.document.Sha256}
				f.document, f.request = doc, request
			}
			before := claimCounts(t, tx)
			_, err = attributeCommand(t, repo, r)
			assertClaimUnchanged(t, tx, before)
			if err == nil {
				t.Fatal("unsupported claim accepted")
			}
		})
	}
}

type claimEventFailure struct{ app.CommandSQLExecutor }

func (e claimEventFailure) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "INSERT INTO purchase_accounting_events_v1") {
		return nil, errors.New("injected claim event write failure")
	}
	return e.CommandSQLExecutor.ExecContext(ctx, query, args...)
}
func TestAttributePurchasePartialFailureRollsBackJournalSourcesAndRevision(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	r := claimRequest(t, f, state, p)
	before := claimCounts(t, tx)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	command, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := attributeCommand(t, f.repo(claimEventFailure{command}), r); err == nil {
		t.Fatal("late failure ignored")
	}
	if postScalar(t, command, `SELECT count(*) FROM purchase_gst_attributions_v1`) != 1 || paymentBalance(t, command, "1150") != 500 {
		t.Fatal("failure did not follow actual claim journal and retained source")
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
	if retained, err := f.repo(read).Get(ctx, postingOrg, f.document.Id); err != nil || !proto.Equal(retained, state) {
		t.Fatalf("rollback projection=%v %v", retained, err)
	}
}

func TestAttributePurchaseConcurrentCallerClaimsOrDefersExactlyOne(t *testing.T) {
	for _, deferring := range []bool{false, true} {
		t.Run(map[bool]string{false: "two claims", true: "claim and deferral"}[deferring], func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			f, _, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			first := claimRequest(t, f, state, p)
			second := claimRequest(t, f, state, p)
			deferred := deferRequest(t, f, state, p)
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var workers sync.WaitGroup
			for index := 0; index < 2; index++ {
				workers.Add(1)
				go func(index int) {
					defer workers.Done()
					<-start
					// Match the production command's immediate write transaction so
					// the loser observes the committed accounting version.
					command, err := workspace.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
					if err != nil {
						results <- err
						return
					}
					defer command.Rollback()
					owned := *f
					owned.executor = command
					owned.next = 9400 + index*100
					repo, err := purchases.NewRepository(command, func() time.Time { return owned.now }, owned.id)
					if err != nil {
						results <- err
						return
					}
					if index == 1 && deferring {
						_, err = repo.Defer(ctx, deferred)
					} else {
						request := first
						if index == 1 {
							request = second
						}
						_, err = repo.Attribute(ctx, request)
					}
					if err == nil {
						err = command.Commit()
					}
					results <- err
				}(index)
			}
			close(start)
			workers.Wait()
			close(results)
			accepted, conflicts := 0, 0
			for err := range results {
				if err == nil {
					accepted++
				} else if errors.Is(err, purchases.ErrConflict) {
					conflicts++
				} else {
					t.Fatalf("unexpected concurrent result: %v", err)
				}
			}
			if accepted != 1 || conflicts != 1 {
				t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
			}
			read, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer read.Rollback()
			retained, err := f.repo(read).Get(ctx, postingOrg, f.document.Id)
			if err != nil || retained.AccountingVersion != state.AccountingVersion+1 || postScalar(t, read, `SELECT count(*) FROM purchase_gst_attributions_v1`)+postScalar(t, read, `SELECT count(*) FROM purchase_gst_deferrals_v1`) != 1 {
				t.Fatalf("concurrent history=%v %v", retained, err)
			}
		})
	}
}

func TestAttributePurchaseTrustedExpiryCannotBeBypassedByBackdatedReview(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	r := claimRequest(t, f, state, p)
	// Leave client reviewed-as-of and release dates backdated before the limit.
	// The current trusted clock must still prevent a new expired claim event.
	f.now = time.Date(2030, 11, 4, 12, 0, 0, 0, time.UTC)
	before := claimCounts(t, tx)
	_, err = attributeCommand(t, repo, r)
	assertClaimUnchanged(t, tx, before)
	if err == nil {
		t.Fatal("trusted expiry bypassed using old review date")
	}
}

func TestAttributePurchaseRehashedActorCannotChangeElectedCommandOwner(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, p := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	request := claimRequest(t, f, state, p)
	result, err := attributeCommand(t, repo, request)
	if err != nil {
		t.Fatal(err)
	}
	forgedActor := "018f0000-0000-7000-8000-000000009999"
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,'forged@example.test','Different owner','ACTIVE',?,?)`, forgedActor, f.now.Format(time.RFC3339Nano), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	a := result.Attributions[0]
	a.ActorUserId = forgedActor
	a.SourceHash = nil
	unsigned, _ := proto.MarshalOptions{Deterministic: true}.Marshal(a)
	sourceHash := sha256.Sum256(unsigned)
	a.SourceHash = sourceHash[:]
	retained, _ := proto.MarshalOptions{Deterministic: true}.Marshal(a)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_gst_attributions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_gst_attributions_v1 SET attribution_proto=?,source_hash=?,actor_user_id=? WHERE id=?`, retained, sourceHash[:], forgedActor, a.Id); err != nil {
		t.Fatal(err)
	}
	content, _ := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	eventHash := sha256.Sum256(content)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_accounting_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_accounting_events_v1 SET result_proto=?,result_sha256=? WHERE operation_key=?`, content, eventHash[:], request.CommandContext.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, postingOrg, f.document.Id); err == nil {
		t.Fatal("claimed credit accepted a rehashed actor different from elected command owner")
	}
}

func TestAttributePurchaseRepositoryExists(t *testing.T) {
	if _, ok := any((*purchases.Repository)(nil)).(attributionCommandPort); !ok {
		t.Fatal("purchase repository cannot explicitly attribute retained suspense portions")
	}
}

func attributeCommand(t *testing.T, repo *purchases.Repository, request *tammyv1.AttributePurchaseGstRequest) (*tammyv1.AttributePurchaseGstResponse, error) {
	t.Helper()
	port, ok := any(repo).(attributionCommandPort)
	if !ok {
		t.Fatal("missing explicit purchase attribution command")
	}
	return port.Attribute(context.Background(), request)
}
