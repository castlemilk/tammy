//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func electIncomeTestCommand(t *testing.T, db *sqlcipher.Database, key string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `INSERT OR IGNORE INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?,?,?,?,'v1','invoice-test',zeroblob(32),'ELECTED','2026-10-02')`, orgID, userID, "tammy.v1.InvoiceService.IssueInvoice", key); err != nil {
		t.Fatal(err)
	}
}
func accountingFixture(t *testing.T, basis tammyv1.GstBasis, zero bool) (*sqlcipher.Database, *tammyv1.Invoice, *tammyv1.Organisation, func() (string, error)) {
	t.Helper()
	ctx := context.Background()
	db := invoiceDB(t)
	if _, err := db.ExecContext(ctx, `UPDATE organisations SET gst_basis=? WHERE id=?`, basis, orgID); err != nil {
		t.Fatal(err)
	}
	accounts, _ := accounting.NewAccountRepository(db)
	template, _ := accounting.LoadAUSmallBusinessV1()
	if err := accounts.InstallTemplate(ctx, orgID, template, time.Now()); err != nil {
		t.Fatal(err)
	}
	invoice := savedInvoice()
	invoice.Draft.Lines = []*tammyv1.InvoiceLineInput{{Description: "Taxable", QuantityMilli: 1000, UnitPriceMinor: 101, UnitCostMinor: ptr(int64(9000)), TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE}, {Description: "GST-free", QuantityMilli: 1000, UnitPriceMinor: 103, TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE}, {Description: "Input taxed", QuantityMilli: 1000, UnitPriceMinor: 107, TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED}, {Description: "Out of scope", QuantityMilli: 1000, UnitPriceMinor: 109, TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE}}
	if zero {
		for _, line := range invoice.Draft.Lines {
			line.UnitPriceMinor = 0
		}
	}
	totals, _ := Calculate(invoice.Draft)
	invoice.SubtotalMinor = totals.SubtotalMinor
	invoice.GstMinor = totals.GstMinor
	invoice.TotalMinor = totals.TotalMinor
	invoice.CostMinor = totals.CostMinor
	invoice.LineTotals = totals.LineTotals
	repo, _ := NewRepository(db)
	if err := repo.Save(ctx, invoice, 0); err != nil {
		t.Fatal(err)
	}
	invoice.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	invoice.Version = 2
	if err := repo.Save(ctx, invoice, 1); err != nil {
		t.Fatal(err)
	}
	profiles, _ := organisations.NewRepository(db)
	profile, err := profiles.Get(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"018f0000-0000-7000-8000-000000000801", "018f0000-0000-7000-8000-000000000802", "018f0000-0000-7000-8000-000000000803"} {
		electIncomeTestCommand(t, db, key)
	}
	for index := 0; index < 4; index++ {
		electIncomeTestCommand(t, db, fmt.Sprintf("018f0000-0000-7000-8000-%012x", 2000+index))
	}
	var identifier uint64 = 1000
	return db, invoice, profile, func() (string, error) {
		identifier++
		return fmt.Sprintf("018f0000-0000-7000-8000-%012x", identifier), nil
	}
}
func TestInvoiceAccountingSQLCipherPostsIncomeAndCashOrNoncashGSTAndReceipts(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			db, invoice, profile, newID := accountingFixture(t, basis, false)
			ctx := context.Background()
			now := func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
			tx, _ := db.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
			repo, _ := NewAccountingRepository(tx, now, newID)
			linked, err := repo.Post(ctx, invoice, profile, "018f0000-0000-7000-8000-000000000801")
			if err != nil {
				t.Fatal(err)
			}
			if !linked.Posted || linked.Version != 1 || linked.OutstandingMinor != 430 {
				t.Fatalf("posting=%v", linked)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			// Reload the transaction-scoped repository after commit, like restart/read.
			read, _ := NewAccountingRepository(db, now, newID)
			loaded, err := read.Get(ctx, orgID, invoice.Id)
			if err != nil || !proto.Equal(loaded, linked) {
				t.Fatalf("retained=%v error=%v", loaded, err)
			}
			journals, _ := accounting.NewJournalRepository(db)
			journal, err := journals.Get(ctx, linked.GetJournalId())
			if err != nil || journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_RECEIVABLE || journal.TotalDebits.MinorUnits != 430 || journal.TotalCredits.MinorUnits != 430 {
				t.Fatalf("income journal=%v error=%v", journal, err)
			}
			var net, gst, bas int64
			if err = db.QueryRowContext(ctx, `SELECT invoice_net_minor,invoice_gst_minor,bas_sales_gross_minor FROM invoice_income_postings_v1`).Scan(&net, &gst, &bas); err != nil || net != 420 || gst != 10 || bas != 321 {
				t.Fatalf("retained financial facts=%d/%d/%d error=%v", net, gst, bas, err)
			}
			if _, _, err = journals.Reverse(ctx, journal.Id, journal.Version, &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 2}, "Unsafe generic reversal", "018f0000-0000-7000-8000-000000000850", nil, now()); !errors.Is(err, accounting.ErrJournalSourceConflict) {
				t.Fatalf("generic reversal accepted=%v", err)
			}
			var receivedGST, receivedBAS int64
			for index, amount := range []int64{1, 17, 111, 301} {
				tx, _ = db.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
				repo, _ = NewAccountingRepository(tx, now, newID)
				state, payment, err := repo.RecordPayment(ctx, invoice, profile, &tammyv1.RecordInvoicePaymentRequest{CommandContext: &tammyv1.CommandContext{IdempotencyKey: fmt.Sprintf("018f0000-0000-7000-8000-%012x", 2000+index)}, OrganisationId: orgID, InvoiceId: invoice.Id, ExpectedAccountingVersion: uint64(index + 1), PaymentDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 2}, Amount: incomeMoney(amount), BankAccountId: templateBankID(), Reference: "Actual bank receipt"})
				if err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
				receivedGST += payment.AllocatedGst.MinorUnits
				receivedBAS += payment.BasSalesGross.MinorUnits
				if state.Version != uint64(index+2) || state.PaymentCount != uint64(index+1) {
					t.Fatalf("payment version=%v", state)
				}
				if index == 3 && (state.OutstandingMinor != 0 || state.ReceivedMinor != 430 || receivedGST != 10 || receivedBAS != 321) {
					t.Fatalf("final settlement=%v allocated GST=%d BAS=%d", state, receivedGST, receivedBAS)
				}
			}
			var revenue, bank, receivable, current, deferred int64
			if err = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN a.account_type='REVENUE' THEN l.credit_minor-l.debit_minor ELSE 0 END),0),COALESCE(SUM(CASE WHEN a.code='1000' THEN l.debit_minor-l.credit_minor ELSE 0 END),0),COALESCE(SUM(CASE WHEN a.code='1100' THEN l.debit_minor-l.credit_minor ELSE 0 END),0),COALESCE(SUM(CASE WHEN a.code='2100' THEN l.credit_minor-l.debit_minor ELSE 0 END),0),COALESCE(SUM(CASE WHEN a.code='2110' THEN l.credit_minor-l.debit_minor ELSE 0 END),0) FROM journal_lines l JOIN accounts a ON a.id=l.account_id`).Scan(&revenue, &bank, &receivable, &current, &deferred); err != nil || revenue != 420 || bank != 430 || receivable != 0 || current != 10 || deferred != 0 {
				t.Fatalf("actual balances revenue/bank/AR/current/deferred=%d/%d/%d/%d/%d error=%v", revenue, bank, receivable, current, deferred, err)
			}
			if _, err = db.ExecContext(ctx, `UPDATE invoice_income_postings_v1 SET invoice_net_minor=0`); err == nil {
				t.Fatal("posting source mutable")
			}
			if _, err = db.ExecContext(ctx, `DELETE FROM invoice_payment_v1`); err == nil {
				t.Fatal("receipt source deletable")
			}
			if err = db.IntegrityCheck(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func templateBankID() string { return "018f0000-0000-7000-8000-000000000101" }

func TestInvoiceAccountingSQLCipherRejectsDuplicateStaleOverpaymentAndForeignReads(t *testing.T) {
	db, invoice, profile, newID := accountingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH, false)
	ctx := context.Background()
	now := time.Now
	tx, _ := db.BeginEncryptedTx(ctx, nil)
	repo, _ := NewAccountingRepository(tx, now, newID)
	if _, err := repo.Post(ctx, invoice, profile, "018f0000-0000-7000-8000-000000000801"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		amount  int64
		version uint64
		date    *tammyv1.CivilDate
		bank    string
		want    error
	}{{"overpayment", 431, 1, &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 2}, templateBankID(), ErrPayment}, {"stale", 1, 2, &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 2}, templateBankID(), ErrConflict}, {"before issue", 1, 1, &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 29}, templateBankID(), ErrPayment}, {"wrong bank", 1, 1, &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 2}, "018f0000-0000-7000-8000-000000000102", ErrPayment}} {
		t.Run(test.name, func(t *testing.T) {
			tx, _ := db.BeginEncryptedTx(ctx, nil)
			defer tx.Rollback()
			repo, _ := NewAccountingRepository(tx, now, newID)
			_, _, err := repo.RecordPayment(ctx, invoice, profile, &tammyv1.RecordInvoicePaymentRequest{CommandContext: &tammyv1.CommandContext{IdempotencyKey: "018f0000-0000-7000-8000-000000000802"}, ExpectedAccountingVersion: test.version, PaymentDate: test.date, Amount: incomeMoney(test.amount), BankAccountId: test.bank})
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
	tx, _ = db.BeginEncryptedTx(ctx, nil)
	repo, _ = NewAccountingRepository(tx, now, newID)
	if _, err := repo.Post(ctx, invoice, profile, "018f0000-0000-7000-8000-000000000803"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate=%v", err)
	}
	_ = tx.Rollback()
	read, _ := NewAccountingRepository(db, now, newID)
	if _, err := read.Get(ctx, "018f0000-0000-7000-8000-000000000999", invoice.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign=%v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_payment_v1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid receipt retained=%d error=%v", count, err)
	}
}

func TestInvoiceAccountingSQLCipherZeroRetainsSourceWithoutFabricatedJournal(t *testing.T) {
	db, invoice, profile, newID := accountingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, true)
	ctx := context.Background()
	tx, _ := db.BeginEncryptedTx(ctx, nil)
	repo, _ := NewAccountingRepository(tx, time.Now, newID)
	result, err := repo.Post(ctx, invoice, profile, "018f0000-0000-7000-8000-000000000801")
	if err != nil || !result.Posted || result.JournalId != nil || result.OutstandingMinor != 0 {
		t.Fatalf("zero posting=%v error=%v", result, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM journals`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("zero created fabricated journal=%d error=%v", count, err)
	}
}

func TestInvoiceAccountingSQLCipherZeroRejectsClosedPeriodAndRollsBack(t *testing.T) {
	db, invoice, profile, newID := accountingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, true)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO accounting_periods(id,organisation_id,start_date,end_date,state,version,closed_at,reopened_at) VALUES('018f0000-0000-7000-8000-000000000900',?,'0001-01-01','2026-09-30','CLOSED',1,'2026-10-02',NULL)`, orgID); err != nil {
		t.Fatal(err)
	}
	tx, _ := db.BeginEncryptedTx(ctx, nil)
	repo, _ := NewAccountingRepository(tx, time.Now, newID)
	if _, err := repo.Post(ctx, invoice, profile, "018f0000-0000-7000-8000-000000000801"); !errors.Is(err, accounting.ErrClosedPeriod) {
		t.Fatalf("zero closed period=%v", err)
	}
	_ = tx.Rollback()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_income_postings_v1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("closed source retained=%d error=%v", count, err)
	}
}

func TestInvoiceAccountingSQLCipherTotalsIncludeMoreThan200Receipts(t *testing.T) {
	db, invoice, profile, newID := accountingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH, false)
	ctx := context.Background()
	tx, _ := db.BeginEncryptedTx(ctx, nil)
	repo, _ := NewAccountingRepository(tx, time.Now, newID)
	if _, err := repo.Post(ctx, invoice, profile, "018f0000-0000-7000-8000-000000000801"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 201; index++ {
		key := fmt.Sprintf("018f0000-0000-7000-8000-%012x", 3000+index)
		electIncomeTestCommand(t, db, key)
		tx, _ = db.BeginEncryptedTx(ctx, nil)
		repo, _ = NewAccountingRepository(tx, time.Now, newID)
		state, _, err := repo.RecordPayment(ctx, invoice, profile, &tammyv1.RecordInvoicePaymentRequest{CommandContext: &tammyv1.CommandContext{IdempotencyKey: key}, OrganisationId: orgID, InvoiceId: invoice.Id, ExpectedAccountingVersion: uint64(index + 1), PaymentDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 2}, Amount: incomeMoney(1), BankAccountId: templateBankID()})
		if err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if index == 200 && (state.ReceivedMinor != 201 || state.PaymentCount != 201 || state.Version != 202 || len(state.Payments) != 200 || state.OutstandingMinor != 229) {
			t.Fatalf("bounded display lost aggregate receipts=%v", state)
		}
	}
}

func TestInvoiceGSTBasisHistoryAcceptsNameAndVerificationChangesButRejectsTransitionsOrGaps(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		initial  tammyv1.GstBasis
		versions []uint64
		want     error
	}{{"name and verification only", tammyv1.GstBasis_GST_BASIS_NON_CASH, []uint64{1, 2, 3}, nil}, {"basis transition", tammyv1.GstBasis_GST_BASIS_CASH, []uint64{1, 2, 3}, ErrGSTBasisHistory}, {"missing history", tammyv1.GstBasis_GST_BASIS_NON_CASH, []uint64{1, 3}, ErrGSTBasisHistory}} {
		t.Run(scenario.name, func(t *testing.T) {
			db, _, profile, _ := accountingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, false)
			ctx := context.Background()
			if _, err := db.ExecContext(ctx, `UPDATE organisations SET version=3 WHERE id=?`, orgID); err != nil {
				t.Fatal(err)
			}
			for _, version := range scenario.versions {
				snapshot := proto.Clone(profile).(*tammyv1.Organisation)
				snapshot.Version = version
				if version == 1 {
					snapshot.GstBasis = scenario.initial
				}
				var result proto.Message
				var name string
				if version == 1 {
					result = &tammyv1.CreateOrganisationResponse{Organisation: snapshot}
					name = "tammy.v1.CreateOrganisationResponse"
				} else if version == 2 {
					result = &tammyv1.UpdateOrganisationResponse{Organisation: snapshot}
					name = "tammy.v1.UpdateOrganisationResponse"
				} else {
					result = &tammyv1.RecordEntityVerificationResponse{Organisation: snapshot}
					name = "tammy.v1.RecordEntityVerificationResponse"
				}
				content, _ := proto.Marshal(result)
				_, err := db.ExecContext(ctx, `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,result_type,result_proto,outcome,created_at,completed_at) VALUES(?,?,?,?,'v1','profile-test',zeroblob(32),?,?,'COMMITTED','2026-10-02','2026-10-02')`, orgID, userID, "tammy.v1.OrganisationService.UpdateOrganisation", fmt.Sprintf("018f0000-0000-7000-8000-%012x", 4000+version), name, content)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := CheckGSTBasisHistory(ctx, db, orgID, 0)
			if !errors.Is(err, scenario.want) {
				t.Fatalf("basis history=%v want=%v", err, scenario.want)
			}
		})
	}
}
