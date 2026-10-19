//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The probe is attached only after fixture setup. During a reservation, every
// SQL read must be one of the three aggregate-only queries; key, parent-ID,
// preflight, body, and write queries are all counted as violations.
type basSourceAggregateProbe struct {
	*sqlcipher.Transaction
	invoiceSummaries, purchaseSummaries, accountingSummaries int
	nonAggregateReads, writes                                int
}

func (p *basSourceAggregateProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	switch {
	case strings.Contains(query, "/* BAS invoice scalar summary */"):
		p.invoiceSummaries++
	case strings.Contains(query, "/* BAS purchase scalar summary */"):
		p.purchaseSummaries++
	case strings.Contains(query, "/* BAS tax fact scalar summary */"):
		p.accountingSummaries++
	default:
		p.nonAggregateReads++
	}
	return p.Transaction.QueryContext(ctx, query, args...)
}

func (p *basSourceAggregateProbe) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	p.writes++
	return p.Transaction.ExecContext(ctx, query, args...)
}

func TestReserveBASSourceScalarsNativeSQLCipherEmptyAndMixed(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 14)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	organisationID, actorID := testUUID(9901), testUUID(9902)
	seedSourceScalarOrganisation(t, ctx, tx, organisationID, actorID, now)

	probe := &basSourceAggregateProbe{Transaction: tx}
	invoicePort, err := invoices.NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	purchaseRepository, err := purchases.NewRepository(probe, func() time.Time { return now }, func() (string, error) {
		return testUUID(9999), nil // unused by these read-only summary methods
	})
	if err != nil {
		t.Fatal(err)
	}
	accountingRepository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	ports := BASSourceScalarPorts{Invoices: invoicePort, Purchases: purchaseRepository, Accounting: accountingRepository}

	// Empty path proves all three real readers execute aggregate-only queries
	// against one still-open native SQLCipher transaction.
	beforeWrites := querySourceScalarInt(t, tx, "SELECT total_changes()")
	beforeRevisions := sourceScalarRevisionState(t, tx)
	empty, err := ReserveBASSourceScalars(ctx, organisationID, ports, 8*1024*1024)
	if err != nil || empty == nil || empty.RootCount != 0 || empty.ReceiptCount != 0 ||
		empty.ScalarKeyCount != 0 || empty.ScalarBytes != 0 || empty.Accounting.RootCount != 0 || empty.Accounting.ReceiptCount != 0 {
		t.Fatalf("empty native reservation = %#v, %v", empty, err)
	}
	assertBASSourceAggregateOnly(t, probe)
	assertSourceScalarTransactionUnchanged(t, tx, beforeWrites, beforeRevisions)

	// Add real rows through each owner: an invoice repository draft, a
	// documents.Repository document that is visible in purchase scalar roots,
	// and a posted accounting journal with a TaxFact. These are census rows,
	// not proof that their eventual BAS dispositions are eligible.
	seedSourceScalarInvoice(t, ctx, tx, organisationID, now, testUUID(9910))
	seedSourceScalarPurchaseDocument(t, ctx, tx, organisationID, actorID, now, testUUID(9911), testUUID(9912))
	seedSourceScalarTaxFact(t, ctx, tx, organisationID, now,
		testUUID(9920), testUUID(9921), testUUID(9922), testUUID(9923), testUUID(9924), testUUID(9925), testUUID(9926))

	// Only measure the coordinator call; fixture writes and these independent
	// owner-summary oracles are deliberately outside the probe window.
	wantInvoice, err := invoices.NewReportingPort(tx)
	if err != nil {
		t.Fatal(err)
	}
	wantPurchase, err := purchases.NewRepository(tx, func() time.Time { return now }, func() (string, error) {
		return testUUID(9998), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantAccounting, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	invoiceSummary, err := wantInvoice.SummarizeBASKeys(ctx, organisationID)
	if err != nil {
		t.Fatal(err)
	}
	purchaseSummary, err := wantPurchase.SummarizeBASKeys(ctx, organisationID)
	if err != nil {
		t.Fatal(err)
	}
	accountingSummary, err := wantAccounting.SummarizeTaxFactBASKeys(ctx, organisationID)
	if err != nil {
		t.Fatal(err)
	}
	if invoiceSummary.RootCount != 1 || invoiceSummary.ScalarKeyCount != 1 || purchaseSummary.RootCount != 1 ||
		purchaseSummary.FamilyCounts[purchases.BASScalarDocument-1] != 1 || purchaseSummary.ScalarKeyCount != 1 || accountingSummary.ScalarKeyCount != 1 {
		t.Fatalf("mixed fixture differs from one draft, one unreviewed document and one fact: invoice=%#v purchase=%#v accounting=%#v",
			invoiceSummary, purchaseSummary, accountingSummary)
	}

	probe.invoiceSummaries, probe.purchaseSummaries, probe.accountingSummaries = 0, 0, 0
	probe.nonAggregateReads, probe.writes = 0, 0
	beforeWrites = querySourceScalarInt(t, tx, "SELECT total_changes()")
	beforeRevisions = sourceScalarRevisionState(t, tx)
	got, err := ReserveBASSourceScalars(ctx, organisationID, ports, 8*1024*1024)
	if err != nil || got == nil {
		t.Fatalf("mixed native reservation = %#v, %v", got, err)
	}
	if got.RootCount != 2 || got.ReceiptCount != 0 || got.ScalarKeyCount != 3 {
		t.Fatalf("mixed source identity counts = %#v; want two roots, no receipts and three physical keys", got)
	}
	if got.Invoices != *invoiceSummary || got.Purchases != *purchaseSummary || got.Accounting != *accountingSummary ||
		got.RootCount != invoiceSummary.RootCount+purchaseSummary.RootCount ||
		got.ReceiptCount != invoiceSummary.ReceiptCount+purchaseSummary.ReceiptCount ||
		got.ScalarKeyCount != invoiceSummary.ScalarKeyCount+purchaseSummary.ScalarKeyCount+accountingSummary.ScalarKeyCount ||
		got.ScalarBytes != invoiceSummary.ScalarBytes+purchaseSummary.ScalarBytes+accountingSummary.ScalarBytes {
		t.Fatalf("mixed reservation changed module summaries or shared arithmetic: got=%#v", got)
	}
	assertBASSourceAggregateOnly(t, probe)
	assertSourceScalarTransactionUnchanged(t, tx, beforeWrites, beforeRevisions)
	for _, budget := range []int64{got.ScalarBytes, got.ScalarBytes - 1} {
		probe.invoiceSummaries, probe.purchaseSummaries, probe.accountingSummaries = 0, 0, 0
		probe.nonAggregateReads, probe.writes = 0, 0
		reserved, err := ReserveBASSourceScalars(ctx, organisationID, ports, budget)
		if budget == got.ScalarBytes {
			if err != nil || reserved == nil || *reserved != *got {
				t.Fatalf("native exact scalar budget = %#v, %v", reserved, err)
			}
		} else {
			assertSourceScalarBound(t, reserved, err, "scalar bytes", budget)
		}
		assertBASSourceAggregateOnly(t, probe)
		assertSourceScalarTransactionUnchanged(t, tx, beforeWrites, beforeRevisions)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("caller rollback after reservation: %v", err)
	}
	for _, table := range []string{"invoices_v1", "documents", "tax_facts"} {
		var count int64
		if err := workspace.Database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("caller rollback retained %s rows=%d: %v", table, count, err)
		}
	}
}

func seedSourceScalarOrganisation(t *testing.T, ctx context.Context, tx *sqlcipher.Transaction,
	organisationID, actorID string, now time.Time) {
	t.Helper()
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,?,?,'ACTIVE',?,?)`,
		actorID, actorID+"@example.test", "BAS source scalar owner", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	repository, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	organisation := &tammyv1.Organisation{
		Id: organisationID, Version: 1, Abn: "51824753556", LegalName: "Scalar Pty Ltd", DisplayName: "Scalar",
		EntityType: "AU_PRIVATE_COMPANY", GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6, OwnerUserId: actorID,
		VerificationState:   tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "rule_bundle", Id: testUUID(9903), Revision: 1, ContentHash: make([]byte, 32)},
	}
	if err := repository.Create(ctx, organisation, now); err != nil {
		t.Fatal(err)
	}
}

func seedSourceScalarInvoice(t *testing.T, ctx context.Context, tx *sqlcipher.Transaction,
	organisationID string, now time.Time, invoiceID string) {
	t.Helper()
	repository, err := invoices.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	draft := &tammyv1.InvoiceDraft{
		Number: "SCALAR-MIXED-INVOICE", IssueDate: "2026-08-10", DueDate: "2026-08-10",
		CustomerName: "Customer", CustomerAddress: "1 High Street", SellerAddress: "27 Cambridge Road",
		Lines: []*tammyv1.InvoiceLineInput{{Description: "Consulting", QuantityMilli: 1000, UnitPriceMinor: 100,
			TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE}},
	}
	totals, err := invoices.Calculate(draft)
	if err != nil {
		t.Fatal(err)
	}
	invoice := &tammyv1.Invoice{
		Id: invoiceID, OrganisationId: organisationID, Version: 1,
		State: tammyv1.InvoiceState_INVOICE_STATE_DRAFT, SellerName: "Scalar Pty Ltd", SellerAbn: "51824753556",
		Draft: draft, SubtotalMinor: totals.SubtotalMinor, GstMinor: totals.GstMinor, TotalMinor: totals.TotalMinor,
		CostMinor: totals.CostMinor, LineTotals: totals.LineTotals,
		CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
	}
	if err := repository.Save(ctx, invoice, 0); err != nil {
		t.Fatal(err)
	}
}

func seedSourceScalarPurchaseDocument(t *testing.T, ctx context.Context, tx *sqlcipher.Transaction,
	organisationID, actorID string, now time.Time, documentID, operationKey string) {
	t.Helper()
	original := []byte("native BAS scalar document")
	digest := sha256.Sum256(original)
	repository, err := documents.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	document := &tammyv1.Document{
		Id: documentID, OrganisationId: organisationID, Version: 1,
		Status:            tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW,
		SourceDisplayName: "scalar-document.pdf", MimeType: "application/pdf", ByteLength: uint64(len(original)), Sha256: digest[:],
		Candidate: &tammyv1.DocumentCandidate{
			SupplierName: "Scalar Supplier", InvoiceNumber: "S-100", DocumentDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
			Subtotal: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 100},
			Gst:      &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 0},
			Total:    &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 100},
		},
		CreatedAt: timestamppb.New(now),
	}
	if _, err := repository.Create(ctx, operationKey, actorID, document, original); err != nil {
		t.Fatal(err)
	}
}

func seedSourceScalarTaxFact(t *testing.T, ctx context.Context, tx *sqlcipher.Transaction,
	organisationID string, now time.Time, debitAccountID, creditAccountID, journalID, debitLineID, creditLineID, taxCodeID, factID string) {
	t.Helper()
	accountRepository, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	accounts := []*tammyv1.Account{
		{Id: debitAccountID, OrganisationId: organisationID, Version: 1, Code: "1001", Name: "Operating bank",
			Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
			Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
			ReportClassification: "balance_sheet.cash", CashFlowClassification: "operating"},
		{Id: creditAccountID, OrganisationId: organisationID, Version: 1, Code: "5001", Name: "Office expenses",
			Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
			Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
			ReportClassification: "profit_loss.operating_expense", CashFlowClassification: "operating"},
	}
	for _, account := range accounts {
		if err := accountRepository.Create(ctx, account, now); err != nil {
			t.Fatal(err)
		}
	}
	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := journalRepository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	ruleHash := make([]byte, 32)
	rule := &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: testUUID(9927), Revision: 1, ContentHash: ruleHash}
	taxCode := &tammyv1.TaxCode{Id: taxCodeID, Code: "GST",
		Treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, Rule: rule}
	sourceHash := sha256.Sum256([]byte("manual tax evidence"))
	sourceID := testUUID(9928)
	fact, err := accounting.BuildNonCashTaxFact(factID, organisationID, creditLineID, taxCode, 1000, 100,
		&tammyv1.SourceRef{Type: "manual_evidence", Id: sourceID, Revision: 1, ContentHash: sourceHash[:]}, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	money := func(minor int64) *tammyv1.Money { return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor} }
	journal := &tammyv1.Journal{
		Id: journalID, OrganisationId: organisationID, Version: 1,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, Source: tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		PostingDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10}, Memo: "Scalar fixture manual journal",
		Lines: []*tammyv1.JournalLine{
			{Id: debitLineID, JournalId: journalID, AccountId: debitAccountID, Sequence: 1,
				Debit: money(1100), Credit: money(0), Description: "Cash"},
			{Id: creditLineID, JournalId: journalID, AccountId: creditAccountID, Sequence: 2,
				Debit: money(0), Credit: money(1100), Description: "Expense",
				TaxCodeId: &taxCodeID, TaxAmount: money(100), TaxRule: rule},
		},
		TotalDebits: money(1100), TotalCredits: money(1100),
		PostedAt: timestamppb.New(now), FinancialRevision: revision,
	}
	flows := map[string][]accounting.CashFlowComponent{
		debitLineID:  {{Category: accounting.CashFlowOperating, AmountMinor: 1100}},
		creditLineID: {{Category: accounting.CashFlowNoncash, AmountMinor: -1100}},
	}
	if err := journalRepository.Post(ctx, journal, "MANUAL", journalID, 1,
		map[string]accounting.TaxFact{creditLineID: fact}, flows, now); err != nil {
		t.Fatal(err)
	}
}

func assertBASSourceAggregateOnly(t *testing.T, p *basSourceAggregateProbe) {
	t.Helper()
	if p.invoiceSummaries != 1 || p.purchaseSummaries != 1 || p.accountingSummaries != 1 ||
		p.nonAggregateReads != 0 || p.writes != 0 {
		t.Fatalf("aggregate/key-parent/body/write query counts = %d/%d/%d/%d/%d; want 1/1/1/0/0",
			p.invoiceSummaries, p.purchaseSummaries, p.accountingSummaries, p.nonAggregateReads, p.writes)
	}
}

func assertSourceScalarTransactionUnchanged(t *testing.T, tx *sqlcipher.Transaction,
	beforeWrites int64, beforeRevisions [7]int64) {
	t.Helper()
	afterWrites := querySourceScalarInt(t, tx, "SELECT total_changes()")
	afterRevisions := sourceScalarRevisionState(t, tx)
	if afterWrites != beforeWrites || afterRevisions != beforeRevisions || !tx.Authenticated() {
		t.Fatalf("reservation changed retained state or transaction: writes %d -> %d, revisions %v -> %v, authenticated=%t",
			beforeWrites, afterWrites, beforeRevisions, afterRevisions, tx.Authenticated())
	}
}

func querySourceScalarInt(t *testing.T, tx *sqlcipher.Transaction, query string) int64 {
	t.Helper()
	var value int64
	if err := tx.QueryRowContext(context.Background(), query).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func sourceScalarRevisionState(t *testing.T, tx *sqlcipher.Transaction) [7]int64 {
	t.Helper()
	var state [7]int64
	if err := tx.QueryRowContext(context.Background(), `SELECT financial_revision,ledger_revision,settlement_revision,
        banking_revision,tax_source_revision,organisation_profile_revision,rule_bundle_revision
        FROM financial_revisions WHERE id=1`).Scan(&state[0], &state[1], &state[2], &state[3], &state[4], &state[5], &state[6]); err != nil {
		t.Fatal(err)
	}
	return state
}
