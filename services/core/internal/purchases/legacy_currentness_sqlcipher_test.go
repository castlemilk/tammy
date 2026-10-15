//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// These cases use the real document, manual-journal, reversal, legacy-review,
// and BAS-coverage writers/readers. They pin the distinction between a current
// cash-credit reversal in legacy classification and the coverage receipt census,
// which intentionally excludes every REVERSAL journal.
func TestLegacyCurrentnessEvidenceOnlySuppressionFollowsDirectNoTaxFactReversalChain(t *testing.T) {
	for _, test := range []struct {
		name                  string
		originalCashIsDebit   bool
		wantExcluded          bool
		wantEvidenceOnly      bool
		wantCandidateJournals []string
	}{
		{
			name:                  "ordinary cash payment reversed to cash debit",
			originalCashIsDebit:   false,
			wantExcluded:          true,
			wantEvidenceOnly:      true,
			wantCandidateJournals: []string{"original"},
		},
		{
			name:                  "cash debit refund reversed to cash credit",
			originalCashIsDebit:   true,
			wantExcluded:          false,
			wantEvidenceOnly:      false,
			wantCandidateJournals: []string{"original", "reversal"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			posted, err := f.repo(tx).PostReviewed(ctx, f.request)
			if err != nil || posted == nil || posted.Posting == nil {
				t.Fatalf("create owned purchase for evidence-only link: response=%v err=%v", posted, err)
			}

			f.addDocument("Refund lineage supplier", "REVERSAL-LINK", 10000, 1000)
			sourceDocument := proto.Clone(f.document).(*tammyv1.Document)
			original, cashAccountID := seedLegacyReversalLinkedJournal(t, f, sourceDocument.Id, test.originalCashIsDebit)
			reversed, reversal := reverseLegacyReversalLinkedJournal(t, f, original)
			assertLegacyReversalStorage(t, f, sourceDocument.Id, original, reversed, reversal, cashAccountID, test.originalCashIsDebit)

			request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
			request.Review.LinkedPostingId = proto.String(posted.Posting.Id)
			resolved, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
			if err != nil || resolved == nil || resolved.Review == nil {
				t.Fatalf("retain evidence-only review through its owned writer: response=%v err=%v", resolved, err)
			}
			counts := coverageStateCounts(t, tx)
			changes := postScalar(t, tx, "SELECT total_changes()")

			candidate, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
			if err != nil {
				t.Fatalf("read complete legacy candidates after reversal: %v", err)
			}
			value := legacyCandidate(t, candidate, sourceDocument.Id)
			wantStatus := tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
			if test.wantExcluded {
				wantStatus = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
			}
			if value.Excluded != test.wantExcluded || value.EvidenceOnly != test.wantEvidenceOnly || value.Status != wantStatus || len(value.Reviews) != 1 || !proto.Equal(value.Reviews[0], resolved.Review) {
				t.Fatalf("evidence-only result excluded=%t evidenceOnly=%t status=%s reviews=%d; want %t/%t/%s/1",
					value.Excluded, value.EvidenceOnly, value.Status, len(value.Reviews), test.wantExcluded, test.wantEvidenceOnly, wantStatus)
			}

			priorJournals := map[string]*tammyv1.Journal{"original": reversed, "reversal": reversal}
			wantPrior := make(map[string]*tammyv1.Journal, len(test.wantCandidateJournals))
			for _, name := range test.wantCandidateJournals {
				wantPrior[priorJournals[name].Id] = priorJournals[name]
			}
			assertLegacyCandidateJournalRefs(t, value.PriorSources, wantPrior)

			capture := captureCoverage(t, f.repo(tx), ctx)
			legacy := coverageLegacyDocument(t, capture, sourceDocument.Id)
			if capture.ReceiptCount != 0 || len(legacy.PaymentJournalIDs) != 0 {
				t.Fatalf("REVERSAL entered coverage receipt census: receipts=%d payment IDs=%v", capture.ReceiptCount, legacy.PaymentJournalIDs)
			}
			if len(legacy.PriorJournalIDs) != 1 || legacy.PriorJournalIDs[0] != original.Id {
				t.Fatalf("coverage changed direct source edges: prior journal IDs=%v, want %s", legacy.PriorJournalIDs, original.Id)
			}
			assertCapturedLegacyReversal(t, tx, capture, sourceDocument.Id, reversed, reversal)
			if postScalar(t, tx, "SELECT total_changes()") != changes || !reflect.DeepEqual(counts, coverageStateCounts(t, tx)) {
				t.Fatal("legacy classification or complete coverage changed retained state")
			}
		})
	}
}

func TestLegacyCurrentnessFollowsReversalOfReversal(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	posted, err := f.repo(tx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.addDocument("Transitive lineage supplier", "TWICE-REVERSED", 10000, 1000)
	documentID := f.document.Id
	original, cashID := seedLegacyReversalLinkedJournal(t, f, documentID, false)
	reversedOriginal, first := reverseLegacyReversalLinkedJournal(t, f, original)
	reversedFirst, final := reverseLegacyReversalLinkedJournal(t, f, first)
	if got := postScalar(t, tx, `SELECT SUM(debit_minor-credit_minor) FROM journal_lines WHERE journal_id=? AND account_id=?`, final.Id, cashID); got != -11000 {
		t.Fatalf("second reversal cash movement=%d, want current cash credit -11000", got)
	}
	if got := postScalar(t, tx, `SELECT count(*) FROM tax_facts f JOIN journal_lines l ON l.id=f.journal_line_id WHERE l.journal_id IN (?,?,?)`, original.Id, first.Id, final.Id); got != 0 {
		t.Fatalf("transitive direct-source fixture acquired %d tax facts", got)
	}
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
	request.Review.LinkedPostingId = proto.String(posted.Posting.Id)
	resolved, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	counts := coverageStateCounts(t, tx)
	changes := postScalar(t, tx, "SELECT total_changes()")
	values, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
	if err != nil {
		t.Fatal(err)
	}
	value := legacyCandidate(t, values, documentID)
	if value.Excluded || value.EvidenceOnly || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY || len(value.Reviews) != 1 || !proto.Equal(value.Reviews[0], resolved.Review) {
		t.Fatalf("transitive current cash credit failed to keep legacy source unresolved: %+v", value)
	}
	assertLegacyCandidateJournalRefs(t, value.PriorSources, map[string]*tammyv1.Journal{original.Id: reversedOriginal, final.Id: final})
	capture := captureCoverage(t, f.repo(tx), ctx)
	legacy := coverageLegacyDocument(t, capture, documentID)
	if capture.ReceiptCount != 0 || len(legacy.PaymentJournalIDs) != 0 || len(legacy.PriorJournalIDs) != 1 || legacy.PriorJournalIDs[0] != original.Id {
		t.Fatalf("transitive reversal changed direct receipt/source census: %+v", legacy)
	}
	for _, expected := range []*tammyv1.Journal{reversedOriginal, reversedFirst, final} {
		if proof := coverageJournal(t, capture, expected.Id); !proto.Equal(proof.Journal, expected) {
			t.Fatalf("transitive closure lost exact journal body %s: %+v", expected.Id, proof)
		}
	}
	if postScalar(t, tx, "SELECT total_changes()") != changes || !reflect.DeepEqual(counts, coverageStateCounts(t, tx)) {
		t.Fatal("transitive classification or coverage changed retained state")
	}
}

func TestLegacyCurrentnessRetainsExplicitTransitiveReversalPaymentSupport(t *testing.T) {
	for _, depth := range []int{1, 2} {
		t.Run(string(rune('0'+depth)), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			documentID := f.document.Id
			current, _ := seedLegacyReversalLinkedJournal(t, f, documentID, depth%2 == 1)
			retained := []*tammyv1.Journal{current}
			for range depth {
				reversed, next := reverseLegacyReversalLinkedJournal(t, f, current)
				retained[len(retained)-1] = reversed
				retained = append(retained, next)
				current = next
			}
			request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT)
			request.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{SourceId: current.Id, SourceVersion: current.Version, SourceHash: coverageMessageHash(t, current)}}
			saved, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
			if err != nil || saved == nil || saved.Review == nil {
				t.Fatalf("retain current cash-credit reversal support through real writer: result=%v err=%v", saved, err)
			}
			counts := coverageStateCounts(t, tx)
			changes := postScalar(t, tx, "SELECT total_changes()")
			values, err := f.repo(tx).LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
			if err != nil {
				t.Fatal(err)
			}
			value := legacyCandidate(t, values, documentID)
			if value.Excluded || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED || len(value.Reviews) != 1 || !proto.Equal(value.Reviews[0], saved.Review) {
				t.Fatalf("explicit reversal support did not remain exact and unsupported: %+v", value)
			}
			assertLegacyCandidateJournalRefs(t, value.PriorSources, map[string]*tammyv1.Journal{retained[0].Id: retained[0], current.Id: current})
			capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || capture == nil {
				t.Fatalf("complete coverage rejected real writer's exact transitive reversal payment support: capture=%v err=%v", capture, err)
			}
			legacy := coverageLegacyDocument(t, capture, documentID)
			if capture.ReceiptCount != 0 || len(legacy.PaymentJournalIDs) != 0 || len(legacy.PriorJournalIDs) != 1 || legacy.PriorJournalIDs[0] != retained[0].Id || len(legacy.Reviews) != 1 || !proto.Equal(legacy.Reviews[0], saved.Review) {
				t.Fatalf("explicit reversal support changed immutable history or direct receipt/source census: %+v", legacy)
			}
			for _, expected := range retained {
				if proof := coverageJournal(t, capture, expected.Id); !proto.Equal(proof.Journal, expected) {
					t.Fatalf("explicit support lost complete journal body %s: %+v", expected.Id, proof)
				}
			}
			if !tx.Authenticated() || postScalar(t, tx, "SELECT total_changes()") != changes || !reflect.DeepEqual(counts, coverageStateCounts(t, tx)) {
				t.Fatal("explicit support readers changed retained state or ended the caller transaction")
			}
			// Reversal changes current support applicability without rewriting
			// the exact historical payment ref retained by the earlier review.
			reversedSupport, final := reverseLegacyReversalLinkedJournal(t, f, current)
			counts = coverageStateCounts(t, tx)
			changes = postScalar(t, tx, "SELECT total_changes()")
			after, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || after == nil {
				t.Fatalf("complete coverage lost historical transitive support after its reversal: capture=%v err=%v", after, err)
			}
			historical := coverageLegacyDocument(t, after, documentID)
			if len(historical.Reviews) != 1 || !proto.Equal(historical.Reviews[0], saved.Review) || after.ReceiptCount != 0 || len(historical.PaymentJournalIDs) != 0 ||
				!proto.Equal(coverageJournal(t, after, current.Id).Journal, reversedSupport) || !proto.Equal(coverageJournal(t, after, final.Id).Journal, final) {
				t.Fatalf("historical support/ref or complete current reversal bodies changed: %+v", historical)
			}
			values, err = f.repo(tx).LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
			if err != nil {
				t.Fatal(err)
			}
			value = legacyCandidate(t, values, documentID)
			if value.Excluded || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY || len(value.Reviews) != 1 || !proto.Equal(value.Reviews[0], saved.Review) {
				t.Fatalf("reversed explicit support did not become unresolved while retaining history: %+v", value)
			}
			if !tx.Authenticated() || postScalar(t, tx, "SELECT total_changes()") != changes || !reflect.DeepEqual(counts, coverageStateCounts(t, tx)) {
				t.Fatal("historical transitive support readers changed retained state or ended the caller transaction")
			}
		})
	}
}

func seedLegacyReversalLinkedJournal(t *testing.T, f *postingFixture, sourceDocumentID string, cashIsDebit bool) (*tammyv1.Journal, string) {
	t.Helper()
	ctx := context.Background()
	cashID, _ := f.id()
	accounts, err := accounting.NewAccountRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	cash := &tammyv1.Account{Id: cashID, OrganisationId: postingOrg, Version: 1, Code: "1011", Name: "Reversal-linked operating cash", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "balance_sheet.cash", CashFlowClassification: "operating"}
	if err := accounts.Create(ctx, cash, f.now); err != nil {
		t.Fatal(err)
	}
	writer, err := accounting.NewJournalRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := writer.ReserveFinancialRevision(ctx, nil, f.now)
	if err != nil {
		t.Fatal(err)
	}
	journalID, _ := f.id()
	expenseLineID, _ := f.id()
	cashLineID, _ := f.id()
	journal := &tammyv1.Journal{Id: journalID, OrganisationId: postingOrg, Version: 1, PostingDate: postDate(10),
		Memo: "Direct document-linked cash movement without tax facts", Source: tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, FinancialRevision: revision, PostedAt: timestamppb.New(f.now),
		TotalDebits: postMoney(11000), TotalCredits: postMoney(11000)}
	var flows map[string][]accounting.CashFlowComponent
	if cashIsDebit {
		journal.Lines = []*tammyv1.JournalLine{
			{Id: cashLineID, JournalId: journalID, Sequence: 1, AccountId: cashID, Debit: postMoney(11000), Credit: postMoney(0)},
			{Id: expenseLineID, JournalId: journalID, Sequence: 2, AccountId: postingExpense, Debit: postMoney(0), Credit: postMoney(11000)},
		}
		flows = map[string][]accounting.CashFlowComponent{
			cashLineID:    {{Category: accounting.CashFlowOperating, AmountMinor: 11000}},
			expenseLineID: {{Category: accounting.CashFlowNoncash, AmountMinor: -11000}},
		}
	} else {
		journal.Lines = []*tammyv1.JournalLine{
			{Id: expenseLineID, JournalId: journalID, Sequence: 1, AccountId: postingExpense, Debit: postMoney(11000), Credit: postMoney(0)},
			{Id: cashLineID, JournalId: journalID, Sequence: 2, AccountId: cashID, Debit: postMoney(0), Credit: postMoney(11000)},
		}
		flows = map[string][]accounting.CashFlowComponent{
			expenseLineID: {{Category: accounting.CashFlowNoncash, AmountMinor: 11000}},
			cashLineID:    {{Category: accounting.CashFlowOperating, AmountMinor: -11000}},
		}
	}
	if err := writer.Post(ctx, journal, "MANUAL", sourceDocumentID, 1, nil, flows, f.now); err != nil {
		t.Fatal(err)
	}
	return journal, cashID
}

func reverseLegacyReversalLinkedJournal(t *testing.T, f *postingFixture, original *tammyv1.Journal) (*tammyv1.Journal, *tammyv1.Journal) {
	t.Helper()
	ctx := context.Background()
	writer, err := accounting.NewJournalRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	reversalID, _ := f.id()
	lineIDs := make([]string, len(original.Lines))
	for index := range lineIDs {
		lineIDs[index], _ = f.id()
	}
	reversed, reversal, err := writer.Reverse(ctx, original.Id, original.Version, postDate(11),
		"Reverse the direct document-linked cash movement", reversalID, lineIDs, f.now)
	if err != nil || reversed == nil || reversal == nil {
		t.Fatalf("reverse direct document-linked journal through accounting writer: original=%v reversal=%v err=%v", reversed, reversal, err)
	}
	storedOriginal, err := writer.Get(ctx, original.Id)
	if err != nil || !proto.Equal(storedOriginal, reversed) {
		t.Fatalf("re-read original after accounting reversal: stored=%v returned=%v err=%v", storedOriginal, reversed, err)
	}
	storedReversal, err := writer.Get(ctx, reversal.Id)
	if err != nil || !proto.Equal(storedReversal, reversal) {
		t.Fatalf("re-read reversal after accounting write: stored=%v returned=%v err=%v", storedReversal, reversal, err)
	}
	if reversed.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED || reversed.Version != original.Version+1 || reversed.GetReversedByJournalId() != reversal.Id ||
		reversal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || reversal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_REVERSAL || reversal.GetReversalOfJournalId() != original.Id {
		t.Fatalf("accounting writer returned broken reciprocal reversal: original=%+v reversal=%+v", reversed, reversal)
	}
	return reversed, reversal
}

func assertLegacyReversalStorage(t *testing.T, f *postingFixture, sourceDocumentID string, original, reversed, reversal *tammyv1.Journal, cashAccountID string, originalCashIsDebit bool) {
	t.Helper()
	if got := postScalar(t, f.executor, `SELECT count(*) FROM journals WHERE organisation_id=? AND id=? AND source_type='MANUAL' AND source_kind=0 AND source_id=? AND state='REVERSED' AND version=? AND reversed_by_journal_id=?`,
		postingOrg, original.Id, sourceDocumentID, int64(reversed.Version), reversal.Id); got != 1 {
		t.Fatalf("original direct source and reversal back-link rows=%d, want one", got)
	}
	if got := postScalar(t, f.executor, `SELECT count(*) FROM journals WHERE organisation_id=? AND id=? AND source_type='REVERSAL' AND source_kind=0 AND source_id=? AND source_revision=? AND state='POSTED' AND reversal_of_journal_id=?`,
		postingOrg, reversal.Id, original.Id, int64(original.Version), original.Id); got != 1 {
		t.Fatalf("reversal source_id and reciprocal forward-link rows=%d, want one", got)
	}
	if got := postScalar(t, f.executor, `SELECT count(*) FROM tax_facts f JOIN journal_lines l ON l.id=f.journal_line_id WHERE f.organisation_id=? AND l.journal_id IN (?,?)`,
		postingOrg, original.Id, reversal.Id); got != 0 {
		t.Fatalf("direct source/reversal unexpectedly acquired tax facts: %d", got)
	}
	wantOriginalCash := int64(-11000)
	if originalCashIsDebit {
		wantOriginalCash = 11000
	}
	for _, check := range []struct {
		journalID string
		want      int64
	}{{original.Id, wantOriginalCash}, {reversal.Id, -wantOriginalCash}} {
		got := postScalar(t, f.executor, `SELECT SUM(debit_minor-credit_minor) FROM journal_lines WHERE journal_id=? AND account_id=?`, check.journalID, cashAccountID)
		if got != check.want {
			t.Fatalf("journal %s cash movement=%d, want %d", check.journalID, got, check.want)
		}
	}
}

func assertLegacyCandidateJournalRefs(t *testing.T, refs []*tammyv1.SourceRef, want map[string]*tammyv1.Journal) {
	t.Helper()
	got := make(map[string]*tammyv1.SourceRef)
	for _, ref := range refs {
		if ref != nil && ref.Type == "journal" {
			got[ref.Id] = ref
		}
	}
	if len(got) != len(want) {
		t.Fatalf("legacy candidate journal provenance=%v, want exactly %v", got, want)
	}
	for id, journal := range want {
		ref := got[id]
		if ref == nil || ref.Revision != journal.Version || !bytes.Equal(ref.ContentHash, coverageMessageHash(t, journal)) {
			t.Fatalf("legacy candidate journal provenance for %s=%+v, want current version %d and exact body hash", id, ref, journal.Version)
		}
	}
}

func assertCapturedLegacyReversal(t *testing.T, tx app.CommandSQLExecutor, capture *purchases.PurchaseCoverageReportingCapture, sourceDocumentID string, reversed, reversal *tammyv1.Journal) {
	t.Helper()
	journals, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []*tammyv1.Journal{reversed, reversal} {
		captured := coverageJournal(t, capture, expected.Id)
		ownerProof, err := journals.GetForReporting(context.Background(), postingOrg, expected.Id)
		if err != nil || ownerProof == nil || !proto.Equal(captured.Journal, expected) || !proto.Equal(ownerProof.Journal, expected) || !bytes.Equal(captured.ContentHash, ownerProof.ContentHash) {
			t.Fatalf("coverage omitted or substituted exact journal body proof for %s: captured=%+v owner=%+v err=%v", expected.Id, captured, ownerProof, err)
		}
	}
	originalProof := coverageJournal(t, capture, reversed.Id)
	reversalProof := coverageJournal(t, capture, reversal.Id)
	if originalProof.SourceType != "MANUAL" || originalProof.SourceKind != 0 || originalProof.SourceID != sourceDocumentID || originalProof.SourceRevision != 1 || originalProof.State != "REVERSED" ||
		reversalProof.SourceType != "REVERSAL" || reversalProof.SourceKind != 0 || reversalProof.SourceID != reversed.Id || reversalProof.SourceRevision != reversed.Version-1 || reversalProof.State != "POSTED" ||
		reversalProof.Journal.GetReversalOfJournalId() != reversed.Id || originalProof.Journal.GetReversedByJournalId() != reversal.Id {
		t.Fatalf("coverage lost raw or protobuf reversal-chain provenance: original=%+v reversal=%+v", originalProof, reversalProof)
	}
}
