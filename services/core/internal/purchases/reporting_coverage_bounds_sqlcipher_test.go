//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type purchaseCoverageHydrationProbe struct {
	app.CommandSQLExecutor
	bodyReads int
}

func (probe *purchaseCoverageHydrationProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
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
	return rows, nil
}

func purchaseCoverageBodyResultColumn(column string) bool {
	name := strings.ToLower(strings.TrimSpace(column))
	switch name {
	case "posting_proto", "payment_proto", "journal_proto", "document_proto", "candidate_proto",
		"review_proto", "response_proto", "result_proto", "outcome_proto", "event_proto", "claim_proto",
		"evidence_proto", "allocation_proto", "attribution_proto", "deferral_proto", "portion_proto",
		"payload_proto", "retained_input_proto", "source_proto", "original_bytes", "extracted_text",
		"description", "memo", "name":
		return true
	}
	if strings.HasSuffix(name, "_proto") || strings.HasSuffix(name, "_original_bytes") || strings.HasSuffix(name, "_extracted_text") ||
		strings.HasSuffix(name, "_description") || strings.HasSuffix(name, "_memo") || strings.HasSuffix(name, "_name") {
		return true
	}
	return false
}

func assertPurchaseCoverageBound(t *testing.T, err error, resource string, limit int64) {
	t.Helper()
	var bound *purchases.SourceBoundError
	if !errors.As(err, &bound) || bound.Resource != resource || int64(bound.Limit) != limit {
		t.Fatalf("bound error=%v, want resource %q at %d", err, resource, limit)
	}
}

func TestPurchaseCoverageAccepts1000MixedRootsAndRejects1001BeforeHydration(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	// The fixture starts with one reviewed, unposted document. Add 997 more so
	// that document roots, one modern posting, and one opening payable total 1000.
	for index := 0; index < 997; index++ {
		f.addDocumentReview("Bound supplier", fmt.Sprintf("ROOT-%03d", index), 0, 0, index%17 == 0)
	}
	postReportingPurchase(t, f, "ROOT-MODERN", postDate(10), 0, 0)
	legacyOpeningFixture(t, f)

	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture exactly 1000 mixed roots: capture=%v err=%v", capture, err)
	}
	if len(capture.Postings) != 1 || len(capture.LegacySources) != 999 {
		t.Fatalf("exact mixed root capacity returned %d modern and %d legacy roots", len(capture.Postings), len(capture.LegacySources))
	}
	if got := len(capture.Postings) + len(capture.LegacySources); got != 1000 {
		t.Fatalf("captured root count=%d, want 1000", got)
	}
	if capture.PayloadBytes <= 0 || capture.PayloadBytes > 8*1024*1024 {
		t.Fatalf("exact-capacity capture escaped its retained payload budget: %d", capture.PayloadBytes)
	}

	f.addDocumentReview("Bound supplier", "ROOT-OVERFLOW", 0, 0, false)
	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	partial, err := f.repo(probe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, 8*1024*1024)
	assertPurchaseCoverageBound(t, err, "purchase roots", 1000)
	if partial != nil || probe.bodyReads != 0 {
		t.Fatalf("1001 mixed roots returned partial=%v and read %d body queries before the count bound", partial != nil, probe.bodyReads)
	}
}

func appendCoveragePaymentCountFixture(t *testing.T, f *postingFixture, executor app.CommandSQLExecutor, first *tammyv1.RecordPurchasePaymentResponse, index int) {
	t.Helper()
	paymentID, _ := f.id()
	journalID, _ := f.id()
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO journals(
		id,organisation_id,version,source_type,source_id,source_revision,state,journal_date,
		description,reversal_of_journal_id,reversed_by_journal_id,total_debits_minor,
		total_credits_minor,currency_code,financial_revision,posted_at,created_at,source_kind)
		SELECT ?,organisation_id,version,source_type,?,source_revision,'DRAFT',journal_date,
		description,NULL,NULL,total_debits_minor,total_credits_minor,currency_code,
		financial_revision,NULL,created_at,source_kind FROM journals WHERE id=?`, journalID, paymentID, first.Journal.Id); err != nil {
		t.Fatal(err)
	}
	for _, line := range first.Journal.Lines {
		lineID, _ := f.id()
		if _, err := executor.ExecContext(context.Background(), `INSERT INTO journal_lines(
			id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code,memo,
			tax_code_id,tax_amount_minor,tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash)
			SELECT ?,?,line_number,account_id,debit_minor,credit_minor,currency_code,memo,
			tax_code_id,tax_amount_minor,tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash
			FROM journal_lines WHERE id=?`, lineID, journalID, line.Id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.ExecContext(context.Background(), `UPDATE journals SET state='POSTED',posted_at=? WHERE id=?`, first.Payment.RecordedAt.AsTime().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), journalID); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO purchase_payments_v1(
		id,posting_id,organisation_id,accounting_version,payment_date,amount_minor,
		currency_code,bank_account_id,journal_id,reference,payment_proto,source_hash,actor_user_id,created_at)
		SELECT ?,posting_id,organisation_id,?,payment_date,amount_minor,currency_code,
		bank_account_id,?,reference,payment_proto,source_hash,actor_user_id,created_at
		FROM purchase_payments_v1 WHERE id=?`, paymentID, index+2, journalID, first.Payment.Id); err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseCoverageCombinedReceiptLimitCountsModernAndLegacySourcesBeforeHydration(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	posting := postReportingPurchase(t, f, "RECEIPT-BOUND", postDate(10), 10000, 1000)
	first, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, posting.Accounting.AccountingVersion, 11000, postDate(20)))
	if err != nil {
		t.Fatalf("seed first modern receipt source: %v", err)
	}
	// Keep the near-cap rows inside SQL. The copied rows establish the combined
	// distinct-source boundary; a zero byte budget ensures no invalid clone is
	// later replayed or treated as a complete purchase capture.
	for index := 1; index < 999; index++ {
		appendCoveragePaymentCountFixture(t, f, tx, first, index)
	}
	f.addDocumentReview("Legacy receipt supplier", "LEGACY-RECEIPT-BOUND", 0, 0, false)
	seedLegacyCashPayment(t, f, f.document.Id)

	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	partial, err := f.repo(probe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, 0)
	assertPurchaseCoverageBound(t, err, "capture bytes", 0)
	if partial != nil || probe.bodyReads != 0 {
		t.Fatalf("1000 combined receipt sources returned partial=%v or hydrated %d bodies before the zero-budget bound", partial != nil, probe.bodyReads)
	}

	appendCoveragePaymentCountFixture(t, f, tx, first, 999)
	partial, err = f.repo(probe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, 0)
	assertPurchaseCoverageBound(t, err, "receipt sources", 1000)
	if partial != nil || probe.bodyReads != 0 {
		t.Fatalf("1001 combined receipt sources returned partial=%v or hydrated %d bodies before the count bound", partial != nil, probe.bodyReads)
	}
}

func seedCoverageCashReceiptWithTaxFactSource(t *testing.T, f *postingFixture, documentSourceID, journalSourceID string) *tammyv1.Journal {
	t.Helper()
	ctx := context.Background()
	cashID, _ := f.id()
	accounts, _ := accounting.NewAccountRepository(f.executor)
	cash := &tammyv1.Account{Id: cashID, OrganisationId: postingOrg, Version: 1, Code: "1011", Name: "Coverage operating cash", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: "balance_sheet.cash", CashFlowClassification: "operating"}
	if err := accounts.Create(ctx, cash, f.now); err != nil {
		t.Fatal(err)
	}

	writer, _ := accounting.NewJournalRepository(f.executor)
	revision, err := writer.ReserveFinancialRevision(ctx, nil, f.now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := f.id()
	debitID, _ := f.id()
	creditID, _ := f.id()
	factID, _ := f.id()
	codeID, _ := f.id()
	rule := &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: "018f0000-0000-7000-8000-000000001003", Revision: 1, ContentHash: make([]byte, 32)}
	code := &tammyv1.TaxCode{Id: codeID, Code: "GST", Treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, Rule: rule}
	documentSource := &tammyv1.SourceRef{Type: "document", Id: documentSourceID, Revision: uint64(f.document.Version), ContentHash: append([]byte(nil), f.document.Sha256...)}
	journal := &tammyv1.Journal{Id: id, OrganisationId: postingOrg, Version: 1, PostingDate: postDate(10), Memo: "Coverage cash receipt with duplicate source edge", Source: tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, FinancialRevision: revision, PostedAt: timestamppb.New(f.now), TotalDebits: postMoney(11000), TotalCredits: postMoney(11000),
		Lines: []*tammyv1.JournalLine{{Id: debitID, JournalId: id, AccountId: postingExpense, Sequence: 1, Debit: postMoney(11000), Credit: postMoney(0), TaxCodeId: &codeID, TaxAmount: postMoney(1000), TaxRule: rule},
			{Id: creditID, JournalId: id, AccountId: cashID, Sequence: 2, Debit: postMoney(0), Credit: postMoney(11000)}}}
	fact, err := accounting.BuildNonCashTaxFact(factID, postingOrg, debitID, code, 10000, 1000, documentSource, debitID)
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]accounting.TaxFact{debitID: fact}
	flows := map[string][]accounting.CashFlowComponent{debitID: {{Category: accounting.CashFlowNoncash, AmountMinor: 11000}}, creditID: {{Category: accounting.CashFlowOperating, AmountMinor: -11000}}}
	sourceRevision := uint64(postScalar(t, f.executor, `SELECT COALESCE(MAX(source_revision),0)+1 FROM journals WHERE organisation_id=? AND source_type='MANUAL' AND source_id=?`, postingOrg, journalSourceID))
	if err := writer.Post(ctx, journal, "MANUAL", journalSourceID, sourceRevision, facts, flows, f.now); err != nil {
		t.Fatal(err)
	}
	return journal
}

func TestPurchaseCoverageDeduplicatesManualReceiptSourceAndExcludesNoncashPrior(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	documentID := f.document.Id
	noncashPrior := f.seedPriorJournal(documentID, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
	cashReceipt := seedCoverageCashReceiptWithTaxFactSource(t, f, documentID, documentID)

	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture manual prior and receipt sources: capture=%v err=%v", capture, err)
	}
	if capture.ReceiptCount != 1 {
		t.Fatalf("unique cash receipt count=%d, want 1 despite duplicate source edges and one noncash prior", capture.ReceiptCount)
	}
	var root *purchases.LegacyPurchaseReportingRecord
	for _, source := range capture.LegacySources {
		if source != nil && source.Document != nil && source.Document.Id == documentID {
			root = source
			break
		}
	}
	if root == nil {
		t.Fatalf("capture omitted source document %s", documentID)
	}
	if len(root.PriorJournalIDs) != 2 || !purchaseCoverageHasID(root.PriorJournalIDs, noncashPrior.Id) || !purchaseCoverageHasID(root.PriorJournalIDs, cashReceipt.Id) {
		t.Fatalf("prior journal IDs=%v, want the two unique linked journals", root.PriorJournalIDs)
	}
	if len(root.PaymentJournalIDs) != 1 || root.PaymentJournalIDs[0] != cashReceipt.Id {
		t.Fatalf("payment journal IDs=%v, want the unique cash receipt only", root.PaymentJournalIDs)
	}

	factRows := 0
	flowRows := 0
	for _, row := range capture.Rows {
		switch row.Table {
		case "tax_facts":
			line, lineOK := retainedValue(row, "journal_line_id")
			owner, ownerOK := retainedValue(row, "organisation_id")
			sourceType, typeOK := retainedValue(row, "source_type")
			sourceID, idOK := retainedValue(row, "source_id")
			if !lineOK || line.Kind != "TEXT" || line.Text != cashReceipt.Lines[0].Id ||
				!ownerOK || owner.Kind != "TEXT" || owner.Text != postingOrg ||
				!typeOK || sourceType.Kind != "TEXT" || sourceType.Text != "document" ||
				!idOK || sourceID.Kind != "TEXT" || sourceID.Text != documentID {
				continue
			}
			revision, revisionOK := retainedValue(row, "source_revision")
			hash, hashOK := retainedValue(row, "source_content_hash")
			if !revisionOK || revision.Kind != "INTEGER" || revision.Integer != int64(f.document.Version) ||
				!hashOK || hash.Kind != "BLOB" || !bytes.Equal(hash.Bytes, f.document.Sha256) {
				t.Fatalf("retained tax-fact source proof lost exact document revision/hash: %+v", row)
			}
			factRows++
		case "cash_flow_facts":
			line, lineOK := retainedValue(row, "journal_line_id")
			owner, ownerOK := retainedValue(row, "organisation_id")
			category, categoryOK := retainedValue(row, "category")
			amount, amountOK := retainedValue(row, "amount_minor")
			if lineOK && line.Kind == "TEXT" && line.Text == cashReceipt.Lines[1].Id &&
				ownerOK && owner.Kind == "TEXT" && owner.Text == postingOrg &&
				categoryOK && category.Kind == "TEXT" && category.Text == "OPERATING" &&
				amountOK && amount.Kind == "INTEGER" && amount.Integer == -11000 {
				flowRows++
			}
		}
	}
	if factRows != 1 || flowRows != 1 {
		t.Fatalf("capture retained manual receipt support rows: tax facts=%d cash flows=%d, want one each", factRows, flowRows)
	}

	var capturedJournal *accounting.JournalReportingRecord
	proofCount := 0
	for _, proof := range capture.Journals {
		if proof != nil && proof.Journal != nil && proof.Journal.Id == cashReceipt.Id {
			proofCount++
			capturedJournal = proof
		}
	}
	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	expectedJournal, err := journalRepository.GetForReporting(ctx, postingOrg, cashReceipt.Id)
	if err != nil || expectedJournal == nil {
		t.Fatalf("read independent journal proof: proof=%v err=%v", expectedJournal, err)
	}
	var capturedHash []byte
	var capturedPayload int64
	if capturedJournal != nil {
		capturedHash = capturedJournal.ContentHash
		capturedPayload = capturedJournal.PayloadBytes
	}
	if proofCount != 1 || capturedJournal == nil || len(capturedJournal.ContentHash) != 32 ||
		!bytes.Equal(capturedJournal.ContentHash, expectedJournal.ContentHash) ||
		capturedJournal.PayloadBytes <= 0 || capturedJournal.PayloadBytes != expectedJournal.PayloadBytes {
		t.Fatalf("captured unique journal proof count/hash/payload=%d/%x/%d, getter=%x/%d", proofCount,
			capturedHash, capturedPayload, expectedJournal.ContentHash, expectedJournal.PayloadBytes)
	}
}

func purchaseCoverageHasID(values []string, id string) bool {
	for _, value := range values {
		if value == id {
			return true
		}
	}
	return false
}
