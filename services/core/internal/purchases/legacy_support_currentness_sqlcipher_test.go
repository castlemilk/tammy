//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestLegacyCurrentnessRejectsSameVersionManualPaymentHashMismatch(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	payment := seedCoverageLegacyCashPayment(t, f, f.document.Id, "1018")
	if payment.Version != 1 {
		t.Fatalf("writer-created support journal version=%d, want 1", payment.Version)
	}
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT)
	correctHash := coverageMessageHash(t, payment)
	wrongHash := append([]byte(nil), correctHash...)
	wrongHash[0] ^= 1
	request.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{
		SourceId: payment.Id, SourceVersion: payment.Version, SourceHash: wrongHash,
	}}

	beforeReviews := postScalar(t, tx, "SELECT count(*) FROM purchase_legacy_reviews_v1")
	beforeEvents := postScalar(t, tx, "SELECT count(*) FROM purchase_accounting_events_v1")
	beforeClaims := postScalar(t, tx, "SELECT count(*) FROM financial_revision_claims")
	beforeRevision := postScalar(t, tx, "SELECT financial_revision FROM financial_revisions WHERE id=1")
	_, err = legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if !errors.Is(err, purchases.ErrInvalidLegacyReview) {
		t.Fatalf("same-version manual-payment hash mismatch error=%v, want ErrInvalidLegacyReview", err)
	}
	if postScalar(t, tx, "SELECT count(*) FROM purchase_legacy_reviews_v1") != beforeReviews ||
		postScalar(t, tx, "SELECT count(*) FROM purchase_accounting_events_v1") != beforeEvents ||
		postScalar(t, tx, "SELECT count(*) FROM financial_revision_claims") != beforeClaims ||
		postScalar(t, tx, "SELECT financial_revision FROM financial_revisions WHERE id=1") != beforeRevision {
		t.Fatal("rejected same-version support hash changed legacy history or financial revisions")
	}
}

func TestLegacyCurrentnessEvidenceOnlyLinkSurvivesPurchaseEvidenceWithdrawal(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	ownedDocument := proto.Clone(f.document).(*tammyv1.Document)
	postRequest := proto.Clone(f.request).(*tammyv1.PostReviewedPurchaseRequest)
	posted, err := f.repo(tx).PostReviewed(ctx, postRequest)
	if err != nil || posted == nil || posted.Posting == nil || posted.Accounting == nil {
		t.Fatalf("post owned purchase before adding a legacy evidence link: posting=%v err=%v", posted, err)
	}
	if posted.Accounting.ReviewOutcomes.Evidence != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
		t.Fatalf("writer-created purchase did not start evidence-ready: %+v", posted.Accounting.ReviewOutcomes)
	}

	f.addDocument("Additional transaction evidence", "EVIDENCE-LINK", 10000, 1000)
	evidenceDocument := proto.Clone(f.document).(*tammyv1.Document)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
	request.Review.LinkedPostingId = proto.String(posted.Posting.Id)
	saved, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil || saved == nil || saved.Review == nil {
		t.Fatalf("retain evidence-only link through the normal writer: review=%v err=%v", saved, err)
	}

	repo := f.repo(tx)
	beforeValues, err := repo.LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
	if err != nil {
		t.Fatal(err)
	}
	before := legacyCandidate(t, beforeValues, evidenceDocument.Id)
	if !before.Excluded || !before.EvidenceOnly || before.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR ||
		len(before.Reviews) != 1 || !proto.Equal(before.Reviews[0], saved.Review) {
		t.Fatalf("writer-created evidence-only decision did not resolve before withdrawal: %+v", before)
	}
	journalsBefore := postScalar(t, tx, "SELECT count(*) FROM journals")

	// Restore the posted source in the fixture so the existing evidence-command
	// helper targets the owned purchase rather than either legacy evidence doc.
	f.document, f.request = ownedDocument, postRequest
	withdrawn := proto.Clone(posted.Posting.Evidence).(*tammyv1.PurchaseEvidenceInput)
	withdrawn.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	withdrawn.Source, withdrawn.HeldDate = nil, nil
	withdrawn.Reason = "Withdraw prior classification pending review of the retained evidence"
	withdrawal, err := evidenceCommand(t, repo, ctx, evidenceRequest(t, f, posted.Accounting.AccountingVersion, withdrawn))
	if err != nil || withdrawal == nil || withdrawal.Accounting == nil {
		t.Fatalf("append ordinary UNKNOWN evidence withdrawal: result=%v err=%v", withdrawal, err)
	}
	if withdrawal.Accounting.ReviewOutcomes.Evidence != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE {
		t.Fatalf("UNKNOWN withdrawal did not change current purchase readiness: %+v", withdrawal.Accounting.ReviewOutcomes)
	}

	afterValues, err := repo.LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
	if err != nil {
		t.Fatal(err)
	}
	after := legacyCandidate(t, afterValues, evidenceDocument.Id)
	if !after.Excluded || !after.EvidenceOnly || after.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR ||
		len(after.Reviews) != 1 || !proto.Equal(after.Reviews[0], saved.Review) {
		t.Fatalf("purchase evidence withdrawal erased or changed the exact evidence-only legacy link: %+v", after)
	}
	links, err := repo.LegacyEvidenceForPosting(ctx, postingOrg, posted.Posting.Id,
		request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
	if err != nil || len(links) != 1 || !links[0].EvidenceOnly || !links[0].Excluded ||
		len(links[0].Reviews) != 1 || !proto.Equal(links[0].Reviews[0], saved.Review) {
		t.Fatalf("reverse reader lost the immutable evidence-only link after withdrawal: links=%v err=%v", links, err)
	}
	coverage := captureCoverage(t, repo, ctx)
	retained := coverageLegacyDocument(t, coverage, evidenceDocument.Id)
	if !retained.CurrentRoot || len(retained.Reviews) != 1 || !proto.Equal(retained.Reviews[0], saved.Review) ||
		!proto.Equal(retained.Source, legacyDocumentSource(evidenceDocument)) {
		t.Fatalf("complete coverage lost the exact current legacy evidence source and review: %+v", retained)
	}
	if postScalar(t, tx, "SELECT count(*) FROM journals") != journalsBefore {
		t.Fatal("evidence-only linkage or evidence withdrawal fabricated accounting")
	}
}
