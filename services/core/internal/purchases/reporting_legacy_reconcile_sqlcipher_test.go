//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

type basLegacyReconcileInputs struct {
	states    []purchases.BASDocumentState
	keys      []purchases.BASScalarKey
	decisions []*tammyv1.LegacyPurchaseReview
	cash      accounting.BASLegacyCashRead
}

func readBASLegacyReconcileInputs(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture) basLegacyReconcileInputs {
	t.Helper()
	ctx := context.Background()
	repo := f.repo(tx)
	keySummary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || keySummary == nil {
		t.Fatalf("reserve purchase keys: summary=%v err=%v", keySummary, err)
	}
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *keySummary, keySummary.ScalarKeyCount, keySummary.ScalarBytes)
	if err != nil {
		t.Fatalf("read purchase keys: %v", err)
	}
	stateSummary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || stateSummary == nil {
		t.Fatalf("reserve document states: summary=%v err=%v", stateSummary, err)
	}
	stateRead, err := repo.ReadBASDocumentStates(ctx, postingOrg, *stateSummary, stateSummary.DocumentCount, stateSummary.StateBytes)
	if err != nil || stateRead == nil {
		t.Fatalf("read document states: read=%v err=%v", stateRead, err)
	}
	decisionSummary, err := repo.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || decisionSummary == nil {
		t.Fatalf("reserve legacy decisions: summary=%v err=%v", decisionSummary, err)
	}
	decisionRead, err := repo.ReadBASLegacyDecisions(ctx, postingOrg, *decisionSummary, decisionSummary.RecordCount, decisionSummary.DecisionBytes)
	if err != nil || decisionRead == nil {
		t.Fatalf("read legacy decisions: read=%v err=%v", decisionRead, err)
	}

	rootSet := make(map[string]struct{})
	for _, state := range stateRead.Documents {
		if state.PostingID == nil {
			rootSet[state.ID] = struct{}{}
		}
	}
	for _, key := range keys {
		if key.Kind == purchases.BASScalarOpeningPayable && key.IsRoot {
			rootSet[key.ID] = struct{}{}
		}
	}
	rootIDs := make([]string, 0, len(rootSet))
	for id := range rootSet {
		rootIDs = append(rootIDs, id)
	}
	sort.Strings(rootIDs)
	journals, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	cashQuery := accounting.BASLegacyCashQuery{OrganisationID: postingOrg, SourceIDs: rootIDs}
	cashSummary, err := journals.SummarizeBASLegacyCash(ctx, cashQuery)
	if err != nil || cashSummary == nil {
		t.Fatalf("reserve legacy cash: summary=%v err=%v", cashSummary, err)
	}
	cashRead, err := journals.ReadBASLegacyCash(ctx, cashQuery, *cashSummary, cashSummary.RowCount, cashSummary.Bytes)
	if err != nil || cashRead == nil {
		t.Fatalf("read legacy cash: read=%v err=%v", cashRead, err)
	}
	return basLegacyReconcileInputs{states: stateRead.Documents, keys: keys, decisions: decisionRead.Reviews, cash: *cashRead}
}

func cloneBASLegacyDiscovery(value *purchases.BASLegacyCandidateDiscovery) *purchases.BASLegacyCandidateDiscovery {
	if value == nil {
		return nil
	}
	out := &purchases.BASLegacyCandidateDiscovery{
		Nominations: make([]purchases.BASLegacyNomination, len(value.Nominations)),
		Candidates:  append([]purchases.BASLegacyCandidateKey(nil), value.Candidates...),
	}
	for index, nomination := range value.Nominations {
		out.Nominations[index] = nomination
		out.Nominations[index].DocumentRawSHA256 = bytes.Clone(nomination.DocumentRawSHA256)
		out.Nominations[index].ProvisionalDecisionIDs = append([]string(nil), nomination.ProvisionalDecisionIDs...)
	}
	return out
}

func TestBASLegacyPublicNominationReconcilesWriterCaptureAndFreezesMembership(t *testing.T) {
	ctx := context.Background()
	tx, f, request, _ := purchaseBASLegacyDecisionFixture(t)
	inputs := readBASLegacyReconcileInputs(t, tx, f)
	period := proto.Clone(request.Review.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
	balance := proto.Clone(request.Review.ReportingBalanceDate).(*tammyv1.CivilDate)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := purchases.NominateBASLegacyCandidates(cancelled, postingOrg, period, balance,
		inputs.states, inputs.keys, inputs.decisions, inputs.cash, 1000); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled public nomination = %#v, %v", got, err)
	}

	discovery, err := purchases.NominateBASLegacyCandidates(ctx, postingOrg, period, balance,
		inputs.states, inputs.keys, inputs.decisions, inputs.cash, 1000)
	if err != nil || discovery == nil || len(discovery.Nominations) == 0 {
		t.Fatalf("writer-backed public nomination = %#v, %v", discovery, err)
	}
	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture full source proof after nomination: capture=%v err=%v", capture, err)
	}
	frozen := cloneBASLegacyDiscovery(discovery)
	beforeHashes := make(map[string][]byte, len(inputs.states))
	for _, state := range inputs.states {
		beforeHashes[state.ID] = bytes.Clone(state.ContentHash)
	}
	if len(discovery.Nominations[0].DocumentRawSHA256) > 0 {
		discovery.Nominations[0].DocumentRawSHA256[0] ^= 0xff
	}
	for _, state := range inputs.states {
		if !bytes.Equal(state.ContentHash, beforeHashes[state.ID]) {
			t.Fatalf("public nomination aliased the caller's raw document hash for %s", state.ID)
		}
	}
	if err := purchases.ReconcileBASLegacyCandidates(ctx, postingOrg, period, balance, inputs.states,
		inputs.keys, inputs.decisions, inputs.cash, frozen, capture); err != nil {
		t.Fatalf("reconcile complete writer-backed capture: %v", err)
	}

	changedSet := cloneBASLegacyDiscovery(frozen)
	changedSet.Candidates = nil
	if err := purchases.ReconcileBASLegacyCandidates(ctx, postingOrg, period, balance, inputs.states,
		inputs.keys, inputs.decisions, inputs.cash, changedSet, capture); err == nil {
		t.Fatal("reconciliation accepted a changed frozen candidate set")
	}

	// The full reader's current candidate is the reviewed-snapshot source of
	// truth. Changing it after the scalar nomination must fail reconciliation;
	// the selector set must not widen, narrow, or silently adopt the new hash.
	targetDocumentID := request.Review.Source.GetDocument().GetDocumentId()
	const changedInvoice = "coherent-candidate-hash-drift"
	decodedDocumentFound := false
	for index := range capture.Documents {
		doc := capture.Documents[index].Document
		if doc != nil && doc.Id == targetDocumentID {
			doc.Candidate.InvoiceNumber = changedInvoice
			decodedDocumentFound = true
		}
	}
	var legacyRecord *purchases.LegacyPurchaseReportingRecord
	for _, record := range capture.LegacySources {
		if record != nil && record.Document != nil && record.Document.Id == targetDocumentID {
			record.Document.Candidate.InvoiceNumber = changedInvoice
			record.Source = basLegacySourceForCapturedDocument(t, record.Document)
			legacyRecord = record
		}
	}
	if !decodedDocumentFound || legacyRecord == nil ||
		!basLegacySetCapturedDocumentText(capture, targetDocumentID, "invoice_number", changedInvoice) {
		t.Fatal("fixture's current legacy document was not present in a coherent full capture")
	}
	oldReviewedHash := bytes.Clone(request.Review.Source.GetDocument().ReviewedSnapshotHash)
	newReviewedHash := legacyRecord.Source.GetDocument().ReviewedSnapshotHash
	if bytes.Equal(oldReviewedHash, newReviewedHash) {
		t.Fatal("candidate mutation did not change the current reviewed snapshot hash")
	}
	if len(legacyRecord.Reviews) != 1 || legacyRecord.Reviews[0].Review == nil ||
		!bytes.Equal(legacyRecord.Reviews[0].Review.Source.GetDocument().ReviewedSnapshotHash, oldReviewedHash) ||
		legacyRecord.Reviews[0].Review.Source.GetDocument().ReviewedVersion != legacyRecord.Document.Version ||
		!bytes.Equal(legacyRecord.Reviews[0].Review.Source.GetDocument().OriginalContentHash, legacyRecord.Document.Sha256) {
		t.Fatal("fixture no longer isolates the current version/raw-hash decision with its historical candidate hash")
	}
	if err := purchases.ReconcileBASLegacyCandidates(ctx, postingOrg, period, balance, inputs.states,
		inputs.keys, inputs.decisions, inputs.cash, frozen, capture); err == nil {
		t.Fatal("reconciliation accepted a current reviewed-snapshot hash mismatch")
	}

}

func TestBASLegacyOpeningReconciliationUsesRetainedInputHash(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	itemID, _ := coverageOpeningWithDocument(t, f, f.document, false)
	openingReview := coverageReviewOpening(t, f, itemID)
	inputs := readBASLegacyReconcileInputs(t, tx, f)
	period := proto.Clone(openingReview.Review.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
	balance := proto.Clone(openingReview.Review.ReportingBalanceDate).(*tammyv1.CivilDate)
	discovery, err := purchases.NominateBASLegacyCandidates(ctx, postingOrg, period, balance,
		inputs.states, inputs.keys, inputs.decisions, inputs.cash, 1000)
	if err != nil || discovery == nil {
		t.Fatalf("nominate opening root: discovery=%v err=%v", discovery, err)
	}
	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture opening source proof after nomination: capture=%v err=%v", capture, err)
	}
	if err := purchases.ReconcileBASLegacyCandidates(ctx, postingOrg, period, balance, inputs.states,
		inputs.keys, inputs.decisions, inputs.cash, discovery, capture); err != nil {
		t.Fatalf("reconcile opening source: %v", err)
	}

	openingRepo, err := purchases.NewOpeningRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	opening, err := openingRepo.GetOpeningPayable(ctx, postingOrg, itemID)
	if err != nil || opening == nil {
		t.Fatalf("read opening retained input: opening=%v err=%v", opening, err)
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(opening.Input)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(encoded)
	if !bytes.Equal(want[:], opening.SourceHash) || bytes.Equal(opening.SourceHash, opening.ConversionHash) {
		t.Fatal("opening source identity was not derived from retained input independently of conversion source_sha256")
	}
	var found bool
	for _, record := range capture.LegacySources {
		if record != nil && record.Opening != nil && record.Opening.ID == itemID {
			found = record.Source.GetOpeningPayable() != nil &&
				bytes.Equal(record.Source.GetOpeningPayable().SourceHash, want[:])
		}
	}
	if !found {
		t.Fatal("captured opening legacy source does not carry the retained-input hash")
	}
}

func basLegacySourceForCapturedDocument(t *testing.T, document *tammyv1.Document) *tammyv1.LegacyPurchaseSourceRef {
	t.Helper()
	if document == nil || document.Candidate == nil {
		t.Fatal("captured document candidate is missing")
	}
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(document.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	return &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_Document{
		Document: &tammyv1.LegacyPurchaseDocumentRef{
			DocumentId: document.Id, ReviewedVersion: document.Version,
			OriginalContentHash: bytes.Clone(document.Sha256), ReviewedSnapshotHash: bytes.Clone(digest[:]),
		},
	}}
}

func basLegacySetCapturedDocumentText(capture *purchases.PurchaseCoverageReportingCapture, documentID, column, value string) bool {
	matches := 0
	for rowIndex := range capture.Rows {
		row := &capture.Rows[rowIndex]
		if row.Table != "documents" || len(row.Columns) != len(row.Values) {
			continue
		}
		id, idFound := "", false
		columnIndex := -1
		for index, name := range row.Columns {
			cell := row.Values[index]
			if name == "id" && cell.Kind == "TEXT" {
				id, idFound = cell.Text, true
			}
			if name == column {
				columnIndex = index
			}
		}
		if !idFound || id != documentID {
			continue
		}
		if columnIndex < 0 || row.Values[columnIndex].Kind != "TEXT" {
			return false
		}
		row.Values[columnIndex].Text = value
		matches++
	}
	return matches == 1
}

// Complete test additions for reporting_legacy_reconcile_sqlcipher_test.go.
// Its current imports already include accounting, context, crypto/sha256,
// errors, sort, testing, purchases, tammyv1, sqlcipher, testkit, and proto.

func TestBASLegacyReconciliationRetainsCompleteWriterDecisionHistory(t *testing.T) {
	ctx := context.Background()
	tx, f, firstRequest, firstRecord := purchaseBASLegacyDecisionFixture(t)

	secondRequest := electNextLegacy(t, f, firstRequest)
	secondRequest.Review.AffectedPeriod = &tammyv1.PurchaseClaimPeriod{
		PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		PeriodEnd:   &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 31},
	}
	secondRequest.Review.ReportingBalanceDate = &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 31}
	secondRequest.Review.Reason = "Retained earlier-period historical decision"
	secondResult, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, secondRequest)
	if err != nil || secondResult == nil || secondResult.Review == nil {
		t.Fatalf("append second writer-backed legacy decision: result=%v err=%v", secondResult, err)
	}

	inputs := readBASLegacyReconcileInputs(t, tx, f)
	period := proto.Clone(firstRequest.Review.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
	balance := proto.Clone(firstRequest.Review.ReportingBalanceDate).(*tammyv1.CivilDate)
	discovery, err := purchases.NominateBASLegacyCandidates(ctx, postingOrg, period, balance,
		inputs.states, inputs.keys, inputs.decisions, inputs.cash, 1000)
	if err != nil || discovery == nil {
		t.Fatalf("nominate complete current-source history: discovery=%v err=%v", discovery, err)
	}
	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture complete history after nomination: capture=%v err=%v", capture, err)
	}

	documentID := firstRequest.Review.Source.GetDocument().GetDocumentId()
	var nomination *purchases.BASLegacyNomination
	for index := range discovery.Nominations {
		if discovery.Nominations[index].Key.SourceID == documentID {
			nomination = &discovery.Nominations[index]
			break
		}
	}
	if nomination == nil || nomination.ProvisionalEvidenceOnly {
		t.Fatalf("noncreditable document nomination was lost or suppressed: %#v", nomination)
	}
	wantIDs := []string{firstRecord.Id, secondResult.Review.Id}
	sort.Strings(wantIDs)
	if len(nomination.ProvisionalDecisionIDs) != len(wantIDs) ||
		nomination.ProvisionalDecisionIDs[0] != wantIDs[0] || nomination.ProvisionalDecisionIDs[1] != wantIDs[1] {
		t.Fatalf("nomination history IDs=%v, want complete sorted IDs %v", nomination.ProvisionalDecisionIDs, wantIDs)
	}
	var candidateFound bool
	for _, candidate := range discovery.Candidates {
		if candidate.Kind == purchases.BASLegacyCandidateDocument && candidate.SourceID == documentID {
			candidateFound = true
		}
	}
	if !candidateFound {
		t.Fatal("unsuppressed current document is missing from semantic candidates")
	}

	var captured *purchases.LegacyPurchaseReportingRecord
	for _, record := range capture.LegacySources {
		if record != nil && record.Document != nil && record.Document.Id == documentID {
			captured = record
			break
		}
	}
	if captured == nil || len(captured.Reviews) != 2 ||
		!proto.Equal(captured.Reviews[0], firstRecord) || !proto.Equal(captured.Reviews[1], secondResult.Review) {
		t.Fatalf("full source capture did not retain both exact decisions in accounting-version order: %#v", captured)
	}
	if !purchases.LegacyReviewApplies(firstRecord, captured.Source, period, balance) {
		t.Fatal("fixture's August decision should apply to the August query")
	}
	if purchases.LegacyReviewApplies(secondResult.Review, captured.Source, period, balance) {
		t.Fatal("July decision should remain in history but not apply to the August query")
	}
	if err := purchases.ReconcileBASLegacyCandidates(ctx, postingOrg, period, balance, inputs.states,
		inputs.keys, inputs.decisions, inputs.cash, discovery, capture); err != nil {
		t.Fatalf("reconcile complete writer-backed decision history: %v", err)
	}
}

func seedBASLegacyDraftJournal(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture,
	sourceID, journalDate string, sourceRevision uint64) string {
	t.Helper()
	ctx := context.Background()
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	financialRevision, err := repository.ReserveFinancialRevision(ctx, nil, f.now)
	if err != nil || financialRevision == 0 {
		t.Fatalf("reserve valid DRAFT journal financial revision: revision=%v err=%v", financialRevision, err)
	}
	journalID, _ := f.id()
	debitLineID, _ := f.id()
	creditLineID, _ := f.id()
	createdAt := f.now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	if _, err := tx.ExecContext(ctx, `INSERT INTO journals(
		id,organisation_id,version,source_type,source_kind,source_id,source_revision,state,journal_date,
		description,reversal_of_journal_id,reversed_by_journal_id,total_debits_minor,total_credits_minor,
		currency_code,financial_revision,posted_at,created_at)
		VALUES(?,?,1,'MANUAL',0,?,?,'DRAFT',?,?,NULL,NULL,11000,11000,'AUD',?,NULL,?)`,
		journalID, postingOrg, sourceID, int64(sourceRevision), journalDate,
		"Retained legacy DRAFT date-boundary fixture", financialRevision, createdAt); err != nil {
		t.Fatalf("insert schema-valid DRAFT journal: %v", err)
	}
	cashAccountID := paymentAccountID(t, tx, "1000")
	if _, err := tx.ExecContext(ctx, `INSERT INTO journal_lines(
		id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code,memo)
		VALUES(?,?,1,?,11000,0,'AUD',?)`, debitLineID, journalID, postingExpense, "DRAFT balancing debit"); err != nil {
		t.Fatalf("insert DRAFT debit line: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO journal_lines(
		id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code,memo)
		VALUES(?,?,2,?,0,11000,'AUD',?)`, creditLineID, journalID, cashAccountID, "DRAFT cash credit"); err != nil {
		t.Fatalf("insert DRAFT cash line: %v", err)
	}
	return journalID
}

func TestBASLegacyDRAFTCashAndYearOneDateSurviveNominationAndFullCapture(t *testing.T) {
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
		t.Fatalf("create a real linked modern purchase for evidence-only decision: response=%v err=%v", posted, err)
	}

	// Keep a separate current legacy document root. legacyRequest adds a third,
	// reviewed document for supporting evidence.
	f.addDocument("Legacy statement", "LEGACY-CASH", 0, 0)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
	request.Review.LinkedPostingId = proto.String(posted.Posting.Id)
	response, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil || response == nil || response.Review == nil {
		t.Fatalf("create writer-backed evidence-only decision: response=%v err=%v", response, err)
	}
	documentID := request.Review.Source.GetDocument().GetDocumentId()
	inPeriodDraftID := seedBASLegacyDraftJournal(t, tx, f, documentID, "2026-08-15", 1)
	yearOneDraftID := seedBASLegacyDraftJournal(t, tx, f, documentID, "0001-01-01", 2)

	inputs := readBASLegacyReconcileInputs(t, tx, f)
	period := proto.Clone(request.Review.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
	balance := proto.Clone(request.Review.ReportingBalanceDate).(*tammyv1.CivilDate)
	discovery, err := purchases.NominateBASLegacyCandidates(ctx, postingOrg, period, balance,
		inputs.states, inputs.keys, inputs.decisions, inputs.cash, 1000)
	if err != nil || discovery == nil {
		t.Fatalf("nominate DRAFT cash and year-one source date: discovery=%v err=%v", discovery, err)
	}
	var nomination *purchases.BASLegacyNomination
	for index := range discovery.Nominations {
		if discovery.Nominations[index].Key.SourceID == documentID {
			nomination = &discovery.Nominations[index]
			break
		}
	}
	if nomination == nil || !nomination.ProvisionalEvidenceOnly {
		t.Fatalf("in-period DRAFT cash incorrectly blocked exact evidence-only suppression: %#v", nomination)
	}
	for _, candidate := range discovery.Candidates {
		if candidate.Kind == purchases.BASLegacyCandidateDocument && candidate.SourceID == documentID {
			t.Fatal("DRAFT cash root became a semantic candidate instead of remaining provisionally suppressed")
		}
	}
	cashByID := make(map[string]accounting.BASLegacyCashJournal)
	for _, journal := range inputs.cash.Journals {
		cashByID[journal.ID] = journal
	}
	for _, tc := range []struct{ id, date string }{
		{id: inPeriodDraftID, date: "2026-08-15"},
		{id: yearOneDraftID, date: "0001-01-01"},
	} {
		journal, ok := cashByID[tc.id]
		if !ok || journal.State != "DRAFT" || journal.JournalDate != tc.date {
			t.Fatalf("cash selector omitted or changed DRAFT source %s: got=%#v present=%t", tc.id, journal, ok)
		}
	}

	// Body hydration follows nomination; both raw scalar and full proto retain
	// DRAFT, including the year-0001 civil date.
	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture DRAFT source proof after nomination: capture=%v err=%v", capture, err)
	}
	proofsByID := make(map[string]*accounting.JournalReportingRecord)
	for _, proof := range capture.Journals {
		if proof != nil && proof.Journal != nil {
			proofsByID[proof.Journal.Id] = proof
		}
	}
	for _, tc := range []struct {
		id   string
		date *tammyv1.CivilDate
	}{
		{id: inPeriodDraftID, date: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 15}},
		{id: yearOneDraftID, date: &tammyv1.CivilDate{Year: 1, Month: 1, Day: 1}},
	} {
		proof := proofsByID[tc.id]
		if proof == nil || proof.State != "DRAFT" || proof.Journal.State != tammyv1.JournalState_JOURNAL_STATE_UNSPECIFIED ||
			proof.Journal.PostingDate == nil || !proto.Equal(proof.Journal.PostingDate, tc.date) {
			t.Fatalf("full capture did not preserve raw/protobuf DRAFT date: proof=%#v", proof)
		}
	}
	if err := purchases.ReconcileBASLegacyCandidates(ctx, postingOrg, period, balance, inputs.states,
		inputs.keys, inputs.decisions, inputs.cash, discovery, capture); err != nil {
		t.Fatalf("reconcile DRAFT cash/date source proofs: %v", err)
	}
}
