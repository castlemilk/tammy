//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type legacyResolver interface {
	ResolveLegacy(context.Context, *tammyv1.ResolveLegacyPurchaseReviewRequest) (*tammyv1.ResolveLegacyPurchaseReviewResponse, error)
	ReadLegacy(context.Context, string, *tammyv1.LegacyPurchaseSourceRef) ([]*tammyv1.LegacyPurchaseReview, error)
}

func legacyCandidate(t *testing.T, values []*purchases.LegacyCandidate, id string) *purchases.LegacyCandidate {
	t.Helper()
	for _, value := range values {
		if value.Source.GetDocument().GetDocumentId() == id || value.Source.GetOpeningPayable().GetOpeningPayableId() == id {
			return value
		}
	}
	t.Fatalf("complete source discovery omitted %s", id)
	return nil
}

func seedLegacyCashPayment(t *testing.T, f *postingFixture, source string) *tammyv1.Journal {
	t.Helper()
	ctx := context.Background()
	cash, _ := f.id()
	accounts, _ := accounting.NewAccountRepository(f.executor)
	account := &tammyv1.Account{Id: cash, OrganisationId: postingOrg, Version: 1, Code: "1011", Name: "Historical operating cash", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: "balance_sheet.cash", CashFlowClassification: "operating"}
	if err := accounts.Create(ctx, account, f.now); err != nil {
		t.Fatal(err)
	}
	writer, _ := accounting.NewJournalRepository(f.executor)
	revision, err := writer.ReserveFinancialRevision(ctx, nil, f.now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := f.id()
	debit, _ := f.id()
	credit, _ := f.id()
	journal := &tammyv1.Journal{Id: id, OrganisationId: postingOrg, Version: 1, PostingDate: postDate(10), Memo: "Reviewed historical payment source", Source: tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, FinancialRevision: revision, PostedAt: timestamppb.New(f.now), TotalDebits: postMoney(11000), TotalCredits: postMoney(11000),
		Lines: []*tammyv1.JournalLine{{Id: debit, JournalId: id, Sequence: 1, AccountId: postingExpense, Debit: postMoney(11000), Credit: postMoney(0)},
			{Id: credit, JournalId: id, Sequence: 2, AccountId: cash, Debit: postMoney(0), Credit: postMoney(11000)}}}
	flows := map[string][]accounting.CashFlowComponent{debit: {{Category: accounting.CashFlowNoncash, AmountMinor: 11000}}, credit: {{Category: accounting.CashFlowOperating, AmountMinor: -11000}}}
	if err := writer.Post(ctx, journal, "MANUAL", source, 1, nil, flows, f.now); err != nil {
		t.Fatal(err)
	}
	if postScalar(t, f.executor, `SELECT SUM(debit_minor-credit_minor) FROM journal_lines WHERE journal_id=? AND account_id=?`, id, cash) != -11000 {
		t.Fatal("historical payment did not retain actual cash credit")
	}
	if postScalar(t, f.executor, `SELECT SUM(amount_minor) FROM cash_flow_facts WHERE journal_line_id=? AND category='OPERATING'`, credit) != -11000 {
		t.Fatal("historical payment lost operating cash-flow movement")
	}
	return journal
}

func TestLegacyDiscoveryIncludesOldUndatedAndCompleteDocumentHistory(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	original := f.document.Id
	f.date = &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 10}
	f.addDocument("Older supplier", "OLD", 10000, 1000)
	old := f.document.Id
	f.date = nil
	f.addDocumentReview("Undated supplier", "UNDATED", 10000, 1000, false)
	undated := f.document.Id
	docs, _ := documents.NewRepository(tx)
	candidate := proto.Clone(f.document.Candidate).(*tammyv1.DocumentCandidate)
	candidate.DocumentDate = nil
	if _, err := docs.SaveReview(ctx, undated, f.document.Version, candidate, f.now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		f.addDocument("Distinct supplier", f.document.Id, 0, 0)
	}
	r := f.repo(tx)
	values, err := r.LegacyCandidates(ctx, postingOrg, &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}, postDate(31))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 208 {
		t.Fatalf("source discovery applied browser-page cap: %d", len(values))
	}
	for _, id := range []string{original, old, undated} {
		value := legacyCandidate(t, values, id)
		if value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY || value.Excluded || value.CandidateGST != 1000 {
			t.Fatalf("unresolved legacy=%v", value)
		}
	}
	if legacyCandidate(t, values, undated).Document.Candidate.DocumentDate != nil {
		t.Fatal("undated source acquired invented date")
	}
}

func TestLegacyExpenseDoesNotProveClaimAndPriorQuarterPaymentStaysUnsupported(t *testing.T) {
	for _, paid := range []bool{false, true} {
		t.Run(map[bool]string{false: "expense unknown credit", true: "prior quarter invoice paid now"}[paid], func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			f.date = &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 10}
			f.addDocument("Old supplier", "PRIOR", 10000, 1000)
			source := f.document.Id
			var journal *tammyv1.Journal
			if paid {
				journal = seedLegacyCashPayment(t, f, source)
			} else {
				journal = f.seedPriorJournal(source, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
			}
			r := f.repo(tx)
			period := &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
			values, err := r.LegacyCandidates(ctx, postingOrg, period, postDate(31))
			if err != nil {
				t.Fatal(err)
			}
			value := legacyCandidate(t, values, source)
			expected := tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
			if paid {
				expected = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
			}
			if value.Status != expected || value.Excluded || len(value.PriorSources) != 1 {
				t.Fatalf("legacy journal misclassified as payment/claim: %v", value)
			}
			if paid {
				request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT)
				content, _ := proto.MarshalOptions{Deterministic: true}.Marshal(journal)
				hash := sha256.Sum256(content)
				request.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{SourceId: journal.Id, SourceVersion: journal.Version, SourceHash: hash[:]}}
				response, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				if response.Review.Review.Disposition != tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT {
					t.Fatal("known payment converted to a resolved waiver")
				}
				values, err = r.LegacyCandidates(ctx, postingOrg, period, postDate(31))
				if err != nil {
					t.Fatal(err)
				}
				value = legacyCandidate(t, values, source)
				if value.Excluded || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED {
					t.Fatal("unsupported settlement excluded actual payment candidate")
				}
			}
		})
	}
}

func TestLegacyClaimedReviewDoesNotHideNewCashPayment(t *testing.T) {
	for _, test := range []struct {
		name         string
		basis        tammyv1.GstBasis
		claimedMonth int32
		corroborate  bool
		wantExcluded bool
	}{
		{"cash earlier claim without payment history", tammyv1.GstBasis_GST_BASIS_CASH, 6, false, false},
		{"non-cash claim before settlement", tammyv1.GstBasis_GST_BASIS_NON_CASH, 6, false, true},
		{"cash matching period without payment history", tammyv1.GstBasis_GST_BASIS_CASH, 8, false, false},
		{"cash matching period with corroborating payment history", tammyv1.GstBasis_GST_BASIS_CASH, 8, true, true},
		{"cash earlier claim contradicted by payment history", tammyv1.GstBasis_GST_BASIS_CASH, 6, true, false},
		{"cash later claim with corroborating payment history", tammyv1.GstBasis_GST_BASIS_CASH, 9, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, test.basis)
			f.date = &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 10}
			f.addDocument("Old supplier", "CLAIMED-PRIOR", 10000, 1000)
			request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED)
			source := request.Review.Source.GetDocument().DocumentId
			request.Review.PreviouslyClaimedPeriod = &tammyv1.PurchaseClaimPeriod{
				PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: test.claimedMonth, Day: 1},
				PeriodEnd:   &tammyv1.CivilDate{Year: 2026, Month: test.claimedMonth, Day: 30},
			}
			if test.claimedMonth == 8 {
				request.Review.PreviouslyClaimedPeriod = proto.Clone(request.Review.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
			}
			if test.claimedMonth == 9 {
				request.Review.ReportingBalanceDate = &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}
			}
			request.Review.ClaimEvidence = proto.Clone(request.Review.SupportingEvidence[0]).(*tammyv1.PurchaseRetainedSourceRef)
			r := f.repo(tx)
			original, err := r.ResolveLegacy(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			values, err := r.LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
			if err != nil {
				t.Fatal(err)
			}
			if value := legacyCandidate(t, values, source); !value.Excluded || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
				t.Fatal("retained claim evidence was unresolved before a contradictory payment was known")
			}
			payment := seedLegacyCashPayment(t, f, source)
			content, err := proto.MarshalOptions{Deterministic: true}.Marshal(payment)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(content)
			if test.corroborate {
				corroboration := electNextLegacy(t, f, request)
				corroboration.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{SourceId: payment.Id, SourceVersion: payment.Version, SourceHash: hash[:]}}
				corroboration.Review.Reason = "Retained payment and claim evidence reviewed together"
				if _, err := r.ResolveLegacy(ctx, corroboration); err != nil {
					t.Fatal(err)
				}
			}
			values, err = r.LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
			if err != nil {
				t.Fatal(err)
			}
			value := legacyCandidate(t, values, source)
			wantStatus := tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
			if test.wantExcluded {
				wantStatus = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
			}
			if value.Excluded != test.wantExcluded || value.Status != wantStatus {
				t.Fatalf("new known payment hidden by claimed review: excluded=%v status=%v", value.Excluded, value.Status)
			}
			matched := false
			for _, ref := range value.PriorSources {
				if ref.Type == "journal" && ref.Id == payment.Id && ref.Revision == payment.Version && bytes.Equal(ref.ContentHash, hash[:]) {
					matched = true
				}
			}
			if !matched || !proto.Equal(value.Reviews[0], original.Review) {
				t.Fatal("new payment lost provenance or rewrote the original claim review")
			}
			if postScalar(t, tx, `SELECT count(*) FROM journals`) != 1 || postScalar(t, tx, `SELECT count(*) FROM purchase_postings_v1`) != 0 || postScalar(t, tx, `SELECT count(*) FROM purchase_payments_v1`) != 0 || postScalar(t, tx, `SELECT count(*) FROM purchase_gst_attributions_v1`) != 0 {
				t.Fatal("legacy claim review fabricated purchase accounting or another financial effect")
			}
			if postScalar(t, tx, `SELECT COALESCE(SUM(l.debit_minor-l.credit_minor),0) FROM journal_lines l JOIN accounts a ON a.id=l.account_id WHERE a.code IN ('1150','1160','1170','2000')`) != 0 {
				t.Fatal("legacy claim review changed protected payable or input GST controls")
			}
		})
	}
}

func TestLegacyEvidenceOnlyAndSemanticDuplicateRemainDistinctTypedFacts(t *testing.T) {
	for _, evidenceOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "semantic duplicate", true: "evidence-only link"}[evidenceOnly], func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			firstDoc := proto.Clone(f.document).(*tammyv1.Document)
			posting, err := f.repo(tx).PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			if evidenceOnly {
				f.addDocument("Additional supplier evidence", "EVIDENCE", 10000, 1000)
			} else {
				f.addDocument("SUPPLIER", "cost-1", 50000, 5000)
			}
			source := f.document.Id
			disposition := tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE
			if evidenceOnly {
				disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE
			}
			request := legacyRequest(t, f, disposition)
			request.Review.LinkedPostingId = proto.String(posting.Posting.Id)
			if !evidenceOnly {
				request.Review.SupportingEvidence = []*tammyv1.PurchaseRetainedSourceRef{{SourceId: firstDoc.Id, SourceVersion: firstDoc.Version, SourceHash: firstDoc.Sha256}}
			}
			if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request); err != nil {
				t.Fatal(err)
			}
			values, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}, postDate(31))
			if err != nil {
				t.Fatal(err)
			}
			value := legacyCandidate(t, values, source)
			if !value.Excluded || value.EvidenceOnly != evidenceOnly || len(value.Reviews) != 1 {
				t.Fatalf("typed source decision lost: %v", value)
			}
			if postScalar(t, tx, `SELECT count(*) FROM purchase_postings_v1`) != 1 || postScalar(t, tx, `SELECT count(*) FROM journals`) != 1 {
				t.Fatal("review reposted existing accounting")
			}
		})
	}
}

func TestLegacyReviewContradictoryNewPaymentAndOutsideScopeStayVisible(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	source := request.Review.Source.GetDocument().DocumentId
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request); err != nil {
		t.Fatal(err)
	}
	r := f.repo(tx)
	values, err := r.LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, postDate(31))
	if err != nil {
		t.Fatal(err)
	}
	if !legacyCandidate(t, values, source).Excluded {
		t.Fatal("explicit supported unpaid review not applied")
	}
	nextPeriod := &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}}
	values, err = r.LegacyCandidates(ctx, postingOrg, nextPeriod, nextPeriod.PeriodEnd)
	if err != nil {
		t.Fatal(err)
	}
	if legacyCandidate(t, values, source).Excluded {
		t.Fatal("earlier review became blanket later-period waiver")
	}
	seedLegacyCashPayment(t, f, source)
	values, err = r.LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, postDate(31))
	if err != nil {
		t.Fatal(err)
	}
	value := legacyCandidate(t, values, source)
	if value.Excluded || value.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR || len(value.Reviews) != 1 {
		t.Fatal("new contradictory payment hidden or history overwritten")
	}
}

func TestLegacyReviewRestartAndCorruptHistoryRejection(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	response, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	read, err := db.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	values, err := legacyRepo(t, f, read).ReadLegacy(ctx, postingOrg, request.Review.Source)
	if err != nil || len(values) != 1 || !proto.Equal(values[0], response.Review) {
		t.Fatalf("restart changed typed review: %v %v", values, err)
	}
	if _, err := read.ExecContext(ctx, `DROP TRIGGER purchase_legacy_reviews_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := read.ExecContext(ctx, `UPDATE purchase_legacy_reviews_v1 SET reason='Corrupt scalar reason'`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyRepo(t, f, read).ReadLegacy(ctx, postingOrg, request.Review.Source); err == nil {
		t.Fatal("canonical review/scalar discrepancy accepted")
	}
}

func legacyRepo(t *testing.T, f *postingFixture, executor app.CommandSQLExecutor) legacyResolver {
	t.Helper()
	r, ok := any(f.repo(executor)).(legacyResolver)
	if !ok {
		t.Fatal("legacy review repository cannot retain and read explicit source reviews")
	}
	return r
}

func legacyDocumentSource(doc *tammyv1.Document) *tammyv1.LegacyPurchaseSourceRef {
	content, _ := proto.MarshalOptions{Deterministic: true}.Marshal(doc.Candidate)
	digest := sha256.Sum256(content)
	return &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_Document{Document: &tammyv1.LegacyPurchaseDocumentRef{DocumentId: doc.Id, ReviewedVersion: doc.Version, OriginalContentHash: doc.Sha256, ReviewedSnapshotHash: digest[:]}}}
}

func legacyRequest(t *testing.T, f *postingFixture, disposition tammyv1.LegacyPurchaseDisposition) *tammyv1.ResolveLegacyPurchaseReviewRequest {
	t.Helper()
	source := legacyDocumentSource(f.document)
	f.addDocument("Statement evidence", "SUPPORT", 0, 0)
	support := &tammyv1.PurchaseRetainedSourceRef{SourceId: f.document.Id, SourceVersion: f.document.Version, SourceHash: f.document.Sha256}
	operation, _ := f.id()
	f.now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := &tammyv1.ResolveLegacyPurchaseReviewRequest{OrganisationId: postingOrg, CommandContext: testkit.CommandContext(operation), Review: &tammyv1.LegacyPurchaseReviewInput{Source: source,
		AffectedPeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}, ReportingBalanceDate: postDate(31),
		Disposition: disposition, SupportingEvidence: []*tammyv1.PurchaseRetainedSourceRef{support}, Reason: "Reviewed retained statement and transaction provenance"}}
	if _, err := f.executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?,?,'tammy.v1.PurchaseService.ResolveLegacyPurchaseReview',?,'v1','tammy.v1.ResolveLegacyPurchaseReviewRequest',?,'ELECTED',?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, operation, make([]byte, 32), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return request
}

func electNextLegacy(t *testing.T, f *postingFixture, previous *tammyv1.ResolveLegacyPurchaseReviewRequest) *tammyv1.ResolveLegacyPurchaseReviewRequest {
	t.Helper()
	request := proto.Clone(previous).(*tammyv1.ResolveLegacyPurchaseReviewRequest)
	operation, _ := f.id()
	request.CommandContext = testkit.CommandContext(operation)
	if _, err := f.executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?,?,'tammy.v1.PurchaseService.ResolveLegacyPurchaseReview',?,'v1','tammy.v1.ResolveLegacyPurchaseReviewRequest',?,'ELECTED',?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, operation, make([]byte, 32), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestLegacyConflictingEvidenceOnlyLinksKeepSourceUnresolved(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	first, err := f.repo(tx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.addDocument("Different transaction", "COST-2", 10000, 1000)
	second, err := f.repo(tx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.addDocument("Retained purchase evidence", "EVIDENCE-LINK", 10000, 1000)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
	source := request.Review.Source.GetDocument().DocumentId
	request.Review.LinkedPostingId = proto.String(first.Posting.Id)
	r := legacyRepo(t, f, tx)
	if _, err := r.ResolveLegacy(ctx, request); err != nil {
		t.Fatal(err)
	}
	conflicting := electNextLegacy(t, f, request)
	conflicting.Review.LinkedPostingId = proto.String(second.Posting.Id)
	conflicting.Review.Reason = "Retained review links the same evidence to a different purchase"
	if _, err := r.ResolveLegacy(ctx, conflicting); err != nil {
		t.Fatal(err)
	}
	values, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
	if err != nil {
		t.Fatal(err)
	}
	value := legacyCandidate(t, values, source)
	if value.Excluded || value.EvidenceOnly || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY || len(value.Reviews) != 2 {
		t.Fatalf("conflicting owned links hidden: %v", value)
	}
	for _, postingID := range []string{first.Posting.Id, second.Posting.Id} {
		links, err := f.repo(tx).LegacyEvidenceForPosting(ctx, postingOrg, postingID, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
		if err != nil || len(links) != 0 {
			t.Fatalf("conflicting evidence enriched owned purchase: %v %v", links, err)
		}
	}
	if postScalar(t, tx, `SELECT count(*) FROM journals`) != 2 || postScalar(t, tx, `SELECT count(*) FROM purchase_postings_v1`) != 2 || postScalar(t, tx, `SELECT SUM(l.debit_minor-l.credit_minor) FROM journal_lines l JOIN accounts a ON a.id=l.account_id WHERE a.code='1150'`) != 2000 {
		t.Fatal("legacy link history changed existing purchase ledger credits")
	}
}

func TestLegacyConsistentEvidenceLinkCanEnrichOwnedPurchaseWithoutCredit(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	posting, err := f.repo(tx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.addDocument("Additional evidence", "CONSISTENT-EVIDENCE", 10000, 1000)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
	request.Review.LinkedPostingId = proto.String(posting.Posting.Id)
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request); err != nil {
		t.Fatal(err)
	}
	second := electNextLegacy(t, f, request)
	second.Review.Reason = "Further retained evidence confirms the same purchase identity"
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, second); err != nil {
		t.Fatal(err)
	}
	type reverseReader interface {
		LegacyEvidenceForPosting(context.Context, string, string, *tammyv1.PurchaseClaimPeriod, *tammyv1.CivilDate) ([]*purchases.LegacyCandidate, error)
	}
	reader, ok := any(f.repo(tx)).(reverseReader)
	if !ok {
		t.Fatal("owned purchase cannot read consistent legacy evidence links")
	}
	values, err := reader.LegacyEvidenceForPosting(ctx, postingOrg, posting.Posting.Id, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || !values[0].EvidenceOnly || !values[0].Excluded || len(values[0].Reviews) != 2 || values[0].Source.GetDocument().DocumentId != request.Review.Source.GetDocument().DocumentId {
		t.Fatalf("consistent reverse evidence provenance=%v", values)
	}
	if postScalar(t, tx, `SELECT SUM(l.debit_minor-l.credit_minor) FROM journal_lines l JOIN accounts a ON a.id=l.account_id WHERE a.code='1150'`) != 1000 || postScalar(t, tx, `SELECT count(*) FROM journals`) != 1 || postScalar(t, tx, `SELECT count(*) FROM purchase_gst_attributions_v1`) != 0 {
		t.Fatal("evidence enrichment fabricated a new credit")
	}
	if _, err := reader.LegacyEvidenceForPosting(ctx, "018f0000-0000-7000-8000-000000009999", posting.Posting.Id, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate); err == nil {
		t.Fatal("foreign owned purchase reverse provenance accepted")
	}
}

func TestLegacyEvidenceOnlyAndUnsupportedSettlementHistoryNeverExcludesContradiction(t *testing.T) {
	for _, unsupportedFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "link then settlement", true: "settlement then link"}[unsupportedFirst], func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			posting, err := f.repo(tx).PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			f.addDocument("Retained purchase evidence", "EVIDENCE-LINK", 10000, 1000)
			request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
			source := request.Review.Source.GetDocument().DocumentId
			request.Review.LinkedPostingId = proto.String(posting.Posting.Id)
			payment := seedLegacyCashPayment(t, f, source)
			content, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payment)
			hash := sha256.Sum256(content)
			unsupported := proto.Clone(request).(*tammyv1.ResolveLegacyPurchaseReviewRequest)
			unsupported.Review.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT
			unsupported.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{SourceId: payment.Id, SourceVersion: payment.Version, SourceHash: hash[:]}}
			unsupported.Review.Reason = "Known historical payment cannot be settled through an owned source"
			r := legacyRepo(t, f, tx)
			initial, next := request, unsupported
			if unsupportedFirst {
				initial, next = unsupported, request
			}
			if _, err := r.ResolveLegacy(ctx, initial); err != nil {
				t.Fatal(err)
			}
			next = electNextLegacy(t, f, next)
			if _, err := r.ResolveLegacy(ctx, next); err != nil {
				t.Fatal(err)
			}
			values, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
			if err != nil {
				t.Fatal(err)
			}
			value := legacyCandidate(t, values, source)
			if value.Excluded || value.EvidenceOnly || value.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR || len(value.Reviews) != 2 {
				t.Fatalf("unsupported retained settlement hidden by link: %v", value)
			}
			if postScalar(t, tx, `SELECT count(*) FROM purchase_payments_v1`) != 0 || postScalar(t, tx, `SELECT count(*) FROM journals`) != 2 {
				t.Fatal("legacy contradiction invented an owned settlement or journal")
			}
		})
	}
}

func TestLegacyReviewedSourceBoundIgnoresUnreviewedIntake(t *testing.T) {
	for _, reviewed := range []bool{false, true} {
		t.Run(map[bool]string{false: "1001 unreviewed intake", true: "1001 reviewed sources"}[reviewed], func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			for index := 0; index < 1000; index++ {
				f.addDocumentReview("Bounded source", f.document.Id, 0, 0, reviewed)
			}
			values, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}, postDate(31))
			if reviewed {
				if !errors.Is(err, purchases.ErrSourceBound) || values != nil {
					t.Fatalf("over-bound reviewed collection did not fail wholly: %d %v", len(values), err)
				}
				return
			}
			if err != nil || len(values) != 1 {
				t.Fatalf("unreviewed intake consumed legacy source bound: %d %v", len(values), err)
			}
			if values[0].Document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || values[0].Source.GetDocument().ReviewedVersion != 2 {
				t.Fatal("intake was presented as reviewed legacy provenance")
			}
		})
	}
}

func TestLegacyReviewRepositoryExists(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	if _, ok := any(f.repo(tx)).(interface {
		ResolveLegacy(context.Context, *tammyv1.ResolveLegacyPurchaseReviewRequest) (*tammyv1.ResolveLegacyPurchaseReviewResponse, error)
	}); !ok {
		t.Fatal("legacy review repository cannot retain an explicit source review")
	}
}

func TestLegacyReviewRetainsTaxOnlyDecisionAndFullResult(t *testing.T) {
	for _, disposition := range []tammyv1.LegacyPurchaseDisposition{tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED} {
		t.Run(disposition.String(), func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			request := legacyRequest(t, f, disposition)
			if disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED {
				request.Review.PreviouslyClaimedPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
				request.Review.ClaimEvidence = proto.Clone(request.Review.SupportingEvidence[0]).(*tammyv1.PurchaseRetainedSourceRef)
			}
			r := legacyRepo(t, f, tx)
			response, err := r.ResolveLegacy(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if response.GetReview().SourceRevision != 1 || !proto.Equal(response.Review.Review, request.Review) {
				t.Fatalf("review=%v", response)
			}
			values, err := r.ReadLegacy(context.Background(), postingOrg, request.Review.Source)
			if err != nil || len(values) != 1 || !proto.Equal(values[0], response.Review) {
				t.Fatalf("retained history=%v err=%v", values, err)
			}
			for _, table := range []string{"journals", "purchase_postings_v1", "purchase_payments_v1", "purchase_gst_attributions_v1"} {
				if got := postScalar(t, tx, "SELECT count(*) FROM "+table); got != 0 {
					t.Fatalf("legacy review created %s: %d", table, got)
				}
			}
			if postScalar(t, tx, "SELECT count(*) FROM financial_revision_claims") != 1 || postScalar(t, tx, "SELECT ledger_revision FROM financial_revisions") != 0 || postScalar(t, tx, "SELECT settlement_revision FROM financial_revisions") != 0 || postScalar(t, tx, "SELECT tax_source_revision FROM financial_revisions") != 1 {
				t.Fatal("legacy review did not reserve exactly one tax-only revision")
			}
			rows, err := tx.QueryContext(context.Background(), `SELECT result_proto FROM purchase_accounting_events_v1 WHERE scope_kind='DOCUMENT' AND scope_id=? AND accounting_version=1`, request.Review.Source.GetDocument().DocumentId)
			if err != nil {
				t.Fatal(err)
			}
			var content []byte
			if !rows.Next() || rows.Scan(&content) != nil {
				t.Fatal("missing scoped accounting event")
			}
			rows.Close()
			stored := &tammyv1.ResolveLegacyPurchaseReviewResponse{}
			if proto.Unmarshal(content, stored) != nil || !proto.Equal(stored, response) {
				t.Fatal("accounting event omitted full review result")
			}
			if _, err := r.ResolveLegacy(context.Background(), request); err == nil {
				t.Fatal("same accepted review was appended again")
			}
		})
	}
}

func TestLegacyReviewRejectsUnsupportedFactsWithoutEffects(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.ResolveLegacyPurchaseReviewRequest)
	}{
		{"wrong organisation", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.OrganisationId = "018f0000-0000-7000-8000-000000009999"
		}},
		{"stale source", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) { r.Review.Source.GetDocument().ReviewedVersion-- }},
		{"source version overflow", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.Source.GetDocument().ReviewedVersion = math.MaxUint64
		}},
		{"source hash", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.Source.GetDocument().OriginalContentHash = make([]byte, 32)
		}},
		{"reviewed hash", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.Source.GetDocument().ReviewedSnapshotHash = make([]byte, 32)
		}},
		{"missing balance date", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) { r.Review.ReportingBalanceDate = nil }},
		{"unsupported waiver", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT
		}},
		{"zero waiver", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) { r.Review.Disposition = 0 }},
		{"no supporting history", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) { r.Review.SupportingEvidence = nil }},
		{"evidence hash", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.SupportingEvidence[0].SourceHash = make([]byte, 32)
		}},
		{"claimed without period", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED
			r.Review.ClaimEvidence = r.Review.SupportingEvidence[0]
		}},
		{"claimed without evidence", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED
			r.Review.PreviouslyClaimedPeriod = r.Review.AffectedPeriod
		}},
		{"evidence only without purchase", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE
		}},
		{"unknown source payment", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.SupportingPayments = r.Review.SupportingEvidence
		}},
		{"invalid period", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) {
			r.Review.AffectedPeriod.PeriodStart = postDate(31)
			r.Review.AffectedPeriod.PeriodEnd = postDate(1)
		}},
		{"no reason", func(r *tammyv1.ResolveLegacyPurchaseReviewRequest) { r.Review.Reason = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
			test.mutate(request)
			if _, err := legacyRepo(t, f, tx).ResolveLegacy(context.Background(), request); err == nil {
				t.Fatal("unsupported legacy review accepted")
			}
			if postScalar(t, tx, `SELECT count(*) FROM purchase_legacy_reviews_v1`) != 0 || postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`) != 0 || postScalar(t, tx, `SELECT count(*) FROM purchase_accounting_events_v1`) != 0 {
				t.Fatal("rejected review changed retained sources/revisions")
			}
		})
	}
}

func legacyOpeningFixture(t *testing.T, f *postingFixture) string {
	t.Helper()
	ctx := context.Background()
	rows, err := f.executor.QueryContext(ctx, `SELECT id FROM accounts WHERE organisation_id=? AND code='2000'`, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	var payable string
	if !rows.Next() || rows.Scan(&payable) != nil {
		t.Fatal("missing payable control")
	}
	rows.Close()
	conversion, _ := f.id()
	item, _ := f.id()
	journal := f.seedPriorJournal(conversion, tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION, postingExpense, payable)
	if _, err := f.executor.ExecContext(ctx, `INSERT INTO opening_conversions(id,organisation_id,conversion_date,state,source_sha256,version,created_at) VALUES(?,?,'2026-08-10','DRAFT',?,1,?)`, conversion, postingOrg, strings.Repeat("a", 64), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	input := &tammyv1.OpeningBalanceInput{ClientLineId: item, AccountId: payable, Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM, LedgerBalance: postMoney(11000), OriginalIssueDate: postDate(10), OriginalDueDate: postDate(20), OutstandingGst: postMoney(1000), PriorGstAttributed: postMoney(0)}
	if _, err := f.executor.ExecContext(ctx, `INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,original_issue_date,original_due_date,outstanding_gst_minor,prior_gst_attributed_minor) VALUES(?,?,?,'SUPPLIER_OPEN_ITEM',0,11000,'AUD','2026-08-10','2026-08-20',1000,0)`, item, conversion, payable); err != nil {
		t.Fatal(err)
	}
	opening, _ := purchases.NewOpeningRepository(f.executor)
	if err := opening.RecordOpeningPayable(ctx, conversion, input, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.ExecContext(ctx, `UPDATE opening_conversions SET state='POSTED',journal_id=?,financial_revision=? WHERE id=?`, journal.Id, journal.FinancialRevision, conversion); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestLegacyOpeningWithoutDocumentRetainsConversionAccountingAndReview(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	id := legacyOpeningFixture(t, f)
	opening, _ := purchases.NewOpeningRepository(tx)
	value, err := opening.GetOpeningPayable(context.Background(), postingOrg, id)
	if err != nil {
		t.Fatal(err)
	}
	if value.Input.Source != nil || value.ID != id || value.ConversionID == "" || value.JournalID == "" || value.SourceVersion != 1 || len(value.SourceHash) != 32 {
		t.Fatalf("opening projection=%v", value)
	}
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	request.Review.Source = value.LegacySource()
	beforeJournals := postScalar(t, tx, `SELECT count(*) FROM journals`)
	r := legacyRepo(t, f, tx)
	response, err := r.ResolveLegacy(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Review.Review.Source.GetDocument() != nil || response.Review.Review.Source.GetOpeningPayable().OpeningPayableId != id {
		t.Fatal("opening review invented document provenance")
	}
	if postScalar(t, tx, `SELECT count(*) FROM journals`) != beforeJournals || postScalar(t, tx, `SELECT count(*) FROM purchase_postings_v1`) != 0 {
		t.Fatal("opening review reposted accounting")
	}
	if postScalar(t, tx, `SELECT count(*) FROM purchase_accounting_events_v1 WHERE scope_kind='OPENING_PAYABLE' AND scope_id=?`, id) != 1 {
		t.Fatal("opening review has wrong scope")
	}
	request.Review.Source.GetOpeningPayable().SourceHash = make([]byte, 32)
	if _, err := r.ResolveLegacy(context.Background(), request); !errors.Is(err, purchases.ErrConflict) {
		t.Fatalf("opening changed hash not rejected as conflict: %v", err)
	}
}

func TestLegacyOpeningKnownPaymentRetainsHashProvenanceForFingerprint(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	id := legacyOpeningFixture(t, f)
	payment := seedLegacyCashPayment(t, f, id)
	values, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}, postDate(31))
	if err != nil {
		t.Fatal(err)
	}
	value := legacyCandidate(t, values, id)
	if value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED || value.Excluded {
		t.Fatal("known opening payment became an unsupported-period waiver")
	}
	content, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payment)
	hash := sha256.Sum256(content)
	matched := false
	for _, ref := range value.PriorSources {
		if ref.Type == "journal" && ref.Id == payment.Id && ref.Revision == payment.Version && bytes.Equal(ref.ContentHash, hash[:]) {
			matched = true
		}
	}
	if !matched {
		t.Fatal("newly known opening payment omitted exact journal hash provenance")
	}
}
