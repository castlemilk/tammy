//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package reporting

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

const reportOrgID = "018f0c65-7b00-7000-8000-000000000001"
const reportRevenueID = "018f0c65-7b00-7000-8000-000000000002"
const reportBankID = "018f0c65-7b00-7000-8000-000000000003"
const reportARID = "018f0c65-7b00-7000-8000-000000000004"

func TestBASInvoiceSourcesRetainSavedFiguresAndDetectNewSources(t *testing.T) {
	tx, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportInvoice(t, tx, 10, "2026-10-02", 15000, 1050, 16050, 13050, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportInvoice(t, tx, 11, "2026-09-30", 1000, 100, 1100, 1100, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportInvoice(t, tx, 12, "2026-10-03", 500, 50, 550, 550, false, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	start, end := reportDate("2026-10-01"), reportDate("2026-12-31")
	saved, err := repository.CreateBASDraft(context.Background(), reportID(1000), reportID(1001), reportOrgID, start, end, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if saved.SalesG1.MinorUnits != 13050 || saved.GstOnSales_1A.MinorUnits != 1050 || len(saved.SalesSources) != 1 || len(saved.SourceFingerprint) != 32 || saved.SourcesChanged {
		t.Fatalf("invoice sales were not captured: %v", saved)
	}
	if saved.SalesSources[0].InvoiceId != reportID(10) || saved.SalesSources[0].InvoiceNumber != "TEST-10" || saved.SalesSources[0].CustomerName != "Customer 10" || saved.SalesSources[0].PaymentId != nil || len(saved.SalesSources[0].SourceHash) != 32 || len(saved.ReviewWarnings) == 0 {
		t.Fatalf("incomplete sales provenance or missing legacy warning: %v", saved)
	}
	addReportPayment(t, tx, 20, 10, "2026-10-04", 50, 3, 40)
	unchanged, err := repository.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil || unchanged.SourcesChanged || unchanged.SalesG1.MinorUnits != 13050 {
		t.Fatalf("non-cash receipt attributed sales again: %v %v", unchanged, err)
	}
	addReportInvoice(t, tx, 13, "2026-10-05", 1000, 100, 1100, 1100, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	current, err := repository.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if !current.SourcesChanged || current.SalesG1.MinorUnits != 13050 || current.GstOnSales_1A.MinorUnits != 1050 || len(current.SalesSources) != 1 {
		t.Fatalf("saved BAS figures changed or staleness missing: %v", current)
	}
	refreshed, err := repository.CreateBASDraft(context.Background(), reportID(1002), reportID(1003), reportOrgID, start, end, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.SalesG1.MinorUnits != 14150 || refreshed.GstOnSales_1A.MinorUnits != 1150 || refreshed.SourcesChanged {
		t.Fatalf("bad regeneration: %v", refreshed)
	}
	replay, err := repository.CreateBASDraft(context.Background(), reportID(1000), reportID(1004), reportOrgID, start, end, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil || replay.Id != saved.Id || replay.SalesG1.MinorUnits != 13050 {
		t.Fatalf("saved operation changed: %v %v", replay, err)
	}
}

func reportFixture(t *testing.T, basis tammyv1.GstBasis) (*sqlcipher.Transaction, *Repository) {
	t.Helper()
	tx, repository, _, _, _ := reportFixtureAtVersion(t, basis, 12)
	return tx, repository
}

func reportFixtureAtVersion(t *testing.T, basis tammyv1.GstBasis, version uint32) (*sqlcipher.Transaction, *Repository, *sqlcipher.Database, string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), path, key, version); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(context.Background(), path, key)
	t.Cleanup(func() { clear(key) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	tx, err := database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	reportExec(t, tx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,'report@example.test','Report','ACTIVE','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, reportID(5))
	profiles, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.Create(context.Background(), &tammyv1.Organisation{
		Id: reportOrgID, Version: 1, Abn: "51824753556", LegalName: "Test Business Pty Ltd", DisplayName: "Test",
		EntityType: "AU_PRIVATE_COMPANY", GstBasis: basis, GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6, OwnerUserId: reportID(5), VerificationState: tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "rule_bundle", Id: reportID(6), Revision: 1, ContentHash: make([]byte, 32)},
	}, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	reportExec(t, tx, `INSERT INTO accounts(id,organisation_id,code,name,account_type,normal_balance,status,designation,created_at) VALUES(?,?,'4000','Invoice sales','REVENUE','CREDIT','ACTIVE','ORDINARY','2026-10-01T00:00:00Z')`, reportRevenueID, reportOrgID)
	reportExec(t, tx, `INSERT INTO accounts(id,organisation_id,code,name,account_type,normal_balance,status,designation,created_at) VALUES(?,?,'1000','Bank','ASSET','DEBIT','ACTIVE','ORDINARY','2026-10-01T00:00:00Z')`, reportBankID, reportOrgID)
	reportExec(t, tx, `INSERT INTO accounts(id,organisation_id,code,name,account_type,normal_balance,status,designation,created_at) VALUES(?,?,'1100','Receivables','ASSET','DEBIT','ACTIVE','CONTROL','2026-10-01T00:00:00Z')`, reportARID, reportOrgID)
	repository, err := NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	return tx, repository, database, path, key
}

func addReportInvoice(t *testing.T, tx *sqlcipher.Transaction, number int, date string, net, gst, total, basGross int64, posted bool, basis tammyv1.GstBasis) {
	t.Helper()
	invoice := &tammyv1.Invoice{Id: reportID(number), OrganisationId: reportOrgID, Version: 2, State: tammyv1.InvoiceState_INVOICE_STATE_ISSUED, Draft: &tammyv1.InvoiceDraft{Number: fmt.Sprintf("TEST-%d", number), IssueDate: date, CustomerName: fmt.Sprintf("Customer %d", number)}, SubtotalMinor: net, GstMinor: gst, TotalMinor: total}
	encoded, err := proto.Marshal(invoice)
	if err != nil {
		t.Fatal(err)
	}
	reportExec(t, tx, `INSERT INTO invoices_v1(id,organisation_id,version,state,number,invoice_proto,created_at,updated_at) VALUES(?,?,2,'ISSUED',?,?,'2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, invoice.Id, reportOrgID, invoice.Draft.Number, encoded)
	if !posted {
		return
	}
	journalID := reportID(number + 10000)
	if total > 0 {
		addReportJournal(t, tx, number+10000, date, net)
	}
	digest := sha256.Sum256(encoded)
	var journal any
	if total > 0 {
		journal = journalID
	}
	reportExec(t, tx, `INSERT INTO invoice_income_postings_v1(invoice_id,organisation_id,invoice_version,invoice_date,gst_basis,revenue_account_id,journal_id,invoice_net_minor,invoice_gst_minor,invoice_total_minor,bas_sales_gross_minor,profile_version,basis_audit_sequence,financial_revision,source_hash,created_at) VALUES(?,?,2,?,?,?,?,?,?,?,?,1,0,1,?,'2026-10-01T00:00:00Z')`, invoice.Id, reportOrgID, date, int32(basis), reportRevenueID, journal, net, gst, total, basGross, digest[:])
}

func addReportJournal(t *testing.T, tx *sqlcipher.Transaction, number int, date string, amount int64) {
	addReportJournalWithCredit(t, tx, number, date, amount, reportRevenueID)
}
func addReportJournalWithCredit(t *testing.T, tx *sqlcipher.Transaction, number int, date string, amount int64, creditAccount string) {
	t.Helper()
	if amount == 0 {
		amount = 1
	}
	journal := reportID(number)
	reportExec(t, tx, `INSERT INTO journals(id,organisation_id,source_type,source_id,source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at) VALUES(?,?,'RECEIVABLE',?,1,'DRAFT',?,'Fixture',?,?,'AUD',1,'2026-10-01T00:00:00Z')`, journal, reportOrgID, journal, date, amount, amount)
	reportExec(t, tx, `INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,1,?,?,0,'AUD')`, reportID(number+20000), journal, reportBankID, amount)
	reportExec(t, tx, `INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,2,?,0,?,'AUD')`, reportID(number+30000), journal, creditAccount, amount)
	reportExec(t, tx, `UPDATE journals SET state='POSTED',posted_at='2026-10-01T00:00:00Z' WHERE id=?`, journal)
}
func reportExec(t *testing.T, tx *sqlcipher.Transaction, query string, args ...any) {
	t.Helper()
	if _, err := tx.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("fixture SQL: %v\n%s", err, query)
	}
}
func reportID(number int) string                 { return fmt.Sprintf("018f0c65-7b00-7000-8000-%012x", number) }
func reportDate(value string) *tammyv1.CivilDate { return parseCivilDate(value) }

func TestIncomeSummaryIncludesAllRecordsAndOutstandingOlderDebt(t *testing.T) {
	tx, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	for index := 0; index < 201; index++ {
		addReportInvoice(t, tx, 100+index, "2026-10-02", 100, 10, 110, 110, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	}
	addReportInvoice(t, tx, 400, "2026-09-30", 1000, 100, 1100, 1100, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportInvoice(t, tx, 401, "2026-10-03", 300, 30, 330, 330, false, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportInvoice(t, tx, 402, "2026-09-29", 500, 50, 550, 550, false, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportInvoice(t, tx, 403, "2027-01-01", 999, 0, 999, 999, false, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportJournal(t, tx, 50000, "2026-10-03", 700)
	value, err := repository.GetBusinessIncomeSummary(context.Background(), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), reportDate("2026-10-03"))
	if err != nil {
		t.Fatal(err)
	}
	if value.IssuedCount != 201 || value.BusinessIncome.MinorUnits != 20800 || value.InvoiceIncome.MinorUnits != 20100 || value.Outstanding.MinorUnits != 23210 || value.InvoiceGst.MinorUnits != 2010 || value.UnpostedIssuedCount != 2 || value.UnpostedIncome.MinorUnits != 300 {
		t.Fatalf("incomplete business income or older debt: %v", value)
	}
	if _, err := repository.GetBusinessIncomeSummary(context.Background(), reportOrgID, reportDate("2026-12-31"), reportDate("2026-10-01"), reportDate("2026-10-03")); err == nil {
		t.Fatal("reversed reporting period accepted")
	}
	if _, err := repository.GetBusinessIncomeSummary(context.Background(), reportID(9999), reportDate("2026-10-01"), reportDate("2026-12-31"), reportDate("2026-10-03")); err == nil {
		t.Fatal("unknown organisation accepted")
	}
}

func TestCashBASUsesActualPaymentDatesAndExcludesUnsupportedPurchaseCredits(t *testing.T) {
	tx, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
	addReportInvoice(t, tx, 10, "2026-09-30", 100, 10, 110, 110, true, tammyv1.GstBasis_GST_BASIS_CASH)
	addReportPurchase(t, tx, 600, "2026-10-01", 1100, 100)
	start, end := reportDate("2026-10-01"), reportDate("2026-12-31")
	first, err := repository.CreateBASDraft(context.Background(), reportID(1000), reportID(1001), reportOrgID, start, end, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.SalesG1.MinorUnits != 0 || first.GstOnSales_1A.MinorUnits != 0 || first.GstCredits_1B.MinorUnits != 0 || len(first.Sources) != 0 || first.ExcludedCashPurchaseCount != 1 || len(first.ReviewWarnings) == 0 {
		t.Fatalf("cash draft claims unpaid sales or unsupported credits: %v", first)
	}
	addReportPayment(t, tx, 20, 10, "2026-10-02", 50, 4, 50)
	addReportPayment(t, tx, 21, 10, "2027-01-02", 60, 6, 60)
	reportExec(t, tx, `UPDATE financial_revisions SET financial_revision=financial_revision+1,tax_source_revision=tax_source_revision+1 WHERE id=1`)
	old, err := repository.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil || !old.SourcesChanged || old.SalesG1.MinorUnits != 0 {
		t.Fatalf("cash receipt did not stale saved draft: %v %v", old, err)
	}
	refreshed, err := repository.CreateBASDraft(context.Background(), reportID(1002), reportID(1003), reportOrgID, start, end, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.SalesG1.MinorUnits != 50 || refreshed.GstOnSales_1A.MinorUnits != 4 || len(refreshed.SalesSources) != 1 || refreshed.SalesSources[0].GetPaymentId() != reportID(20) || civilDateString(refreshed.SalesSources[0].TaxDate) != "2026-10-02" {
		t.Fatalf("cash source attribution: %v", refreshed)
	}
	summary, err := repository.GetBusinessIncomeSummary(context.Background(), reportOrgID, start, end, reportDate("2026-10-03"))
	if err != nil {
		t.Fatal(err)
	}
	if summary.BusinessIncome.MinorUnits != 0 || summary.InvoiceIncome.MinorUnits != 0 || summary.PaymentsReceived.MinorUnits != 50 || summary.Outstanding.MinorUnits != 60 || summary.InvoiceGst.MinorUnits != 4 {
		t.Fatalf("receipt recognised revenue twice or included future receipt: %v", summary)
	}
	next, err := repository.CreateBASDraft(context.Background(), reportID(1004), reportID(1005), reportOrgID, reportDate("2027-01-01"), reportDate("2027-03-31"), time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil || next.SalesG1.MinorUnits != 60 || next.GstOnSales_1A.MinorUnits != 6 {
		t.Fatalf("cash final cents/period incorrect: %v %v", next, err)
	}
	// Revision-only changes cannot immediately stale the workpaper they created.
	reportExec(t, tx, `UPDATE financial_revisions SET financial_revision=financial_revision+1,tax_source_revision=tax_source_revision+1 WHERE id=1`)
	current, err := repository.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil || current.SourcesChanged {
		t.Fatalf("revision-only change staled immutable sources: %v %v", current, err)
	}
}

func TestBASRejectsMoreThan1000SourcesAndRetainsLegacySnapshot(t *testing.T) {
	tx, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	reportExec(t, tx, `INSERT INTO bas_workpapers(id,operation_key,organisation_id,version,period_start,period_end,status,sales_g1_minor,gst_on_sales_1a_minor,gst_credits_1b_minor,net_gst_payable_minor,created_at) VALUES(?,?,?,1,'2026-10-01','2026-12-31','DRAFT_NOT_LODGED',1234,56,7,49,'2026-10-01T00:00:00Z')`, reportID(8000), reportID(8001), reportOrgID)
	legacy, err := repository.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil || !legacy.SourcesChanged || legacy.SalesG1.MinorUnits != 1234 || legacy.GstOnSales_1A.MinorUnits != 56 || len(legacy.ReviewWarnings) == 0 {
		t.Fatalf("legacy snapshot overwritten or silently current: %v %v", legacy, err)
	}
	for index := 0; index < 1001; index++ {
		addReportInvoice(t, tx, 100+index, "2026-10-02", 100, 10, 110, 110, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	}
	_, err = repository.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrSourceBound) {
		t.Fatalf("large BAS source set silently truncated: %v", err)
	}
	// Root aggregation has no BAS provenance display bound.
	summary, err := repository.GetBusinessIncomeSummary(context.Background(), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), reportDate("2026-10-03"))
	if err != nil || summary.IssuedCount != 1001 || summary.InvoiceIncome.MinorUnits != 100100 {
		t.Fatalf("income aggregate truncated: %v %v", summary, err)
	}
	old, err := repository.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil || !old.SourcesChanged || old.SalesG1.MinorUnits != 1234 {
		t.Fatalf("bounded source failure lost saved draft: %v %v", old, err)
	}
}

func TestBASRejectsMoreThan1000ReviewedPurchases(t *testing.T) {
	tx, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	for index := 0; index < 1001; index++ {
		addReportPurchase(t, tx, 100+index, "2026-10-02", 110, 10)
	}
	_, err := repository.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrSourceBound) {
		t.Fatalf("large purchase source set silently truncated: %v", err)
	}
}

func addReportPayment(t *testing.T, tx *sqlcipher.Transaction, number, invoice int, date string, amount, gst, basGross int64) {
	t.Helper()
	addReportJournalWithCredit(t, tx, number+60000, date, amount, reportARID)
	encoded, err := proto.Marshal(&tammyv1.InvoicePayment{Id: reportID(number), OrganisationId: reportOrgID, InvoiceId: reportID(invoice), Amount: aud(amount), AllocatedGst: aud(gst), BasSalesGross: aud(basGross), PaymentDate: reportDate(date)})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	reportExec(t, tx, `INSERT INTO invoice_payment_v1(id,organisation_id,invoice_id,payment_date,amount_minor,allocated_gst_minor,bas_sales_gross_minor,bank_account_id,journal_id,reference,payment_proto,source_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,'Fixture',?,?,'2026-10-02T00:00:00Z')`, reportID(number), reportOrgID, reportID(invoice), date, amount, gst, basGross, reportBankID, reportID(number+60000), encoded, digest[:])
}
func addReportPurchase(t *testing.T, tx *sqlcipher.Transaction, number int, date string, total, gst int64) {
	t.Helper()
	reportExec(t, tx, `INSERT OR IGNORE INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,'report@example.test','Report','PENDING','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, reportID(5))
	digest := sha256.Sum256([]byte(fmt.Sprintf("source-%d", number)))
	reportExec(t, tx, `INSERT INTO documents(id,operation_key,organisation_id,version,status,source_display_name,mime_type,byte_length,sha256,original_bytes,extracted_text,supplier_name,invoice_number,document_date,subtotal_minor,gst_minor,total_minor,created_by_user_id,created_at,reviewed_at) VALUES(?,?,?,2,'REVIEWED','Fixture.png','image/png',1,?,X'01','','Supplier','PURCHASE',?,?,?,?,?,'2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, reportID(number), reportID(number+80000), reportOrgID, digest[:], date, total-gst, gst, total, reportID(5))
}

func TestBASNonCashPurchaseCreditsAndInvalidPeriods(t *testing.T) {
	tx, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportPurchase(t, tx, 600, "2026-10-01", 1100, 100)
	saved, err := repository.CreateBASDraft(context.Background(), reportID(1000), reportID(1001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if err != nil || saved.GstCredits_1B.MinorUnits != 100 || saved.NetGstPayable.MinorUnits != -100 || len(saved.Sources) != 1 || saved.ExcludedCashPurchaseCount != 0 {
		t.Fatalf("non-cash reviewed credits missing: %v %v", saved, err)
	}
	_, err = repository.CreateBASDraft(context.Background(), reportID(1002), reportID(1003), reportOrgID, &tammyv1.CivilDate{Year: 2026, Month: 2, Day: 30}, reportDate("2026-12-31"), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("invalid civil date accepted")
	}
	_, err = repository.CreateBASDraft(context.Background(), reportID(1004), reportID(1005), reportOrgID, reportDate("2026-12-31"), reportDate("2026-10-01"), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("reversed BAS period accepted")
	}
}
