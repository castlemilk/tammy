//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func paymentDate(month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: 2026, Month: month, Day: day}
}

func paymentAccountID(t *testing.T, executor app.CommandSQLExecutor, code string) string {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), `SELECT id FROM accounts WHERE organisation_id=? AND code=?`, postingOrg, code)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var id string
	if !rows.Next() || rows.Scan(&id) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("missing account %s", code)
	}
	return id
}

func paymentRequest(t *testing.T, f *postingFixture, version uint64, amount int64, date *tammyv1.CivilDate) *tammyv1.RecordPurchasePaymentRequest {
	t.Helper()
	key, _ := f.id()
	request := &tammyv1.RecordPurchasePaymentRequest{CommandContext: testkit.CommandContext(key), OrganisationId: postingOrg, DocumentId: f.document.Id, ExpectedAccountingVersion: version, ActualPaymentDate: date, Amount: postMoney(amount), BankAccountId: paymentAccountID(t, f.executor, "1000"), Reference: "Actual bank payment"}
	_, err := f.executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?,?,'tammy.v1.PurchaseService.RecordPurchasePayment',?,'v1','tammy.v1.RecordPurchasePaymentRequest',?,'ELECTED',?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, key, make([]byte, 32), f.now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func paymentBalance(t *testing.T, executor app.CommandSQLExecutor, code string) int64 {
	t.Helper()
	return postScalar(t, executor, `SELECT COALESCE(SUM(l.debit_minor-l.credit_minor),0) FROM journal_lines l JOIN journals j ON j.id=l.journal_id JOIN accounts a ON a.id=l.account_id AND a.organisation_id=j.organisation_id WHERE j.organisation_id=? AND j.state='POSTED' AND a.code=?`, postingOrg, code)
}

func paymentEventBytes(t *testing.T, executor app.CommandSQLExecutor, version uint64) []byte {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), `SELECT result_proto,result_sha256 FROM purchase_accounting_events_v1 WHERE organisation_id=? AND accounting_version=?`, postingOrg, version)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var content, hash []byte
	if !rows.Next() || rows.Scan(&content, &hash) != nil || rows.Next() || rows.Err() != nil {
		t.Fatal("missing unique event")
	}
	digest := sha256.Sum256(content)
	if !bytes.Equal(hash, digest[:]) {
		t.Fatal("invalid retained event hash")
	}
	return content
}

func assertPaymentRetained(t *testing.T, executor app.CommandSQLExecutor, response *tammyv1.RecordPurchasePaymentResponse, expectedVersion uint64) {
	t.Helper()
	p := response.Payment
	if p == nil || response.Journal == nil || response.Accounting == nil || p.AccountingVersion != expectedVersion || len(p.SourceHash) != 32 || p.JournalId != response.Journal.Id || p.FinancialRevision != response.Journal.FinancialRevision || response.Accounting.AccountingVersion != expectedVersion {
		t.Fatalf("incomplete payment response: %v", response)
	}
	copy := proto.Clone(p).(*tammyv1.PurchasePayment)
	copy.SourceHash = nil
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(copy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	if !bytes.Equal(digest[:], p.SourceHash) {
		t.Fatal("payment hash is not self-cleared canonical source bytes")
	}
	canonical, _ := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	rows, err := executor.QueryContext(context.Background(), `SELECT payment_proto,source_hash,amount_minor,payment_date,reference,created_at FROM purchase_payments_v1 WHERE id=? AND organisation_id=? AND posting_id=? AND journal_id=? AND accounting_version=?`, p.Id, postingOrg, p.PostingId, p.JournalId, expectedVersion)
	if err != nil {
		t.Fatal(err)
	}
	var retained, hash []byte
	var amount int64
	var date, reference, stamp string
	if !rows.Next() || rows.Scan(&retained, &hash, &amount, &date, &reference, &stamp) != nil || rows.Next() || rows.Err() != nil {
		t.Fatal("payment scalar/source row missing")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retained, canonical) || !bytes.Equal(hash, p.SourceHash) || amount != p.Amount.MinorUnits || date != time.Date(int(p.ActualPaymentDate.Year), time.Month(p.ActualPaymentDate.Month), int(p.ActualPaymentDate.Day), 0, 0, 0, 0, time.UTC).Format("2006-01-02") || reference != p.Reference || stamp != p.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) {
		t.Fatal("payment scalars disagree with canonical source")
	}
	if got := postScalar(t, executor, `SELECT count(*) FROM journals WHERE id=? AND organisation_id=? AND source_type='BANKING' AND source_kind=7 AND source_id=? AND source_revision=1 AND financial_revision=?`, p.JournalId, postingOrg, p.Id, p.FinancialRevision); got != 1 {
		t.Fatal("payment journal ownership missing")
	}
	if got := postScalar(t, executor, `SELECT count(*) FROM purchase_payment_allocations_v1 WHERE payment_id=? AND posting_id=? AND organisation_id=?`, p.Id, p.PostingId, postingOrg); got != int64(len(p.Allocations)) {
		t.Fatal("allocation count mismatch")
	}
	var gross, supplier, eligible int64
	for i, a := range p.Allocations {
		if a.Sequence != uint32(i+1) {
			t.Fatal("payment allocations reordered")
		}
		gross += a.Gross.MinorUnits
		supplier += a.SupplierGst.MinorUnits
		eligible += a.EligibleGst.MinorUnits
		if got := postScalar(t, executor, `SELECT count(*) FROM purchase_payment_allocations_v1 WHERE payment_id=? AND sequence=? AND posting_allocation_sequence=? AND gross_minor=? AND supplier_gst_minor=? AND eligible_gst_minor=? AND use_kind=? AND bas_classification=? AND cash_flow_classification=?`, p.Id, a.Sequence, a.Sequence, a.Gross.MinorUnits, a.SupplierGst.MinorUnits, a.EligibleGst.MinorUnits, a.Use, a.BasClassification, a.CashFlowClassification); got != 1 {
			t.Fatal("allocation delta scalar mismatch")
		}
	}
	if gross != p.Amount.MinorUnits || supplier < eligible {
		t.Fatal("payment delta conservation failed")
	}
	event := &tammyv1.RecordPurchasePaymentResponse{}
	if proto.Unmarshal(paymentEventBytes(t, executor, expectedVersion), event) != nil || !proto.Equal(event, response) {
		t.Fatal("event did not retain complete payment response")
	}
	if got := postScalar(t, executor, `SELECT count(*) FROM purchase_accounting_events_v1 e JOIN financial_revision_claims c ON c.operation_key=e.operation_key WHERE e.organisation_id=? AND e.accounting_version=? AND e.scope_kind='POSTING' AND e.scope_id=? AND e.posting_id=? AND e.document_id IS NULL AND e.command_type='tammy.v1.PurchaseService.RecordPurchasePayment' AND e.journal_id=? AND c.domain_mask=11 AND e.financial_revision=c.financial_revision AND e.ledger_revision=c.ledger_revision AND e.tax_source_revision=c.tax_source_revision AND e.settlement_revision=c.settlement_revision`, postingOrg, expectedVersion, p.PostingId, p.PostingId, p.JournalId); got != 1 {
		t.Fatal("single payment revision/event domain vector mismatch")
	}
}

func TestPurchasePaymentTwoPeriodsConserveCashAndNonCashAfterRestart(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, basis)
			repo := f.repo(tx)
			posted, err := repo.PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			initial := append([]byte(nil), paymentEventBytes(t, tx, 1)...)
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			var last *tammyv1.RecordPurchasePaymentResponse
			for i, date := range []*tammyv1.CivilDate{paymentDate(9, 30), paymentDate(10, 2)} {
				last, err = repo.RecordPayment(ctx, paymentRequest(t, f, uint64(i+1), 5500, date))
				if err != nil {
					t.Fatal(err)
				}
				assertPaymentRetained(t, tx, last, uint64(i+2))
				if !proto.Equal(last.Payment.ActualPaymentDate, date) || last.Payment.Allocations[0].EligibleGst.MinorUnits != 500 || last.Payment.Allocations[0].SupplierGst.MinorUnits != 500 || last.Payment.Allocations[0].Gross.MinorUnits != 5500 {
					t.Fatal("payment period or exact retained delta changed")
				}
				if last.Accounting.Paid.MinorUnits != int64(i+1)*5500 || last.Accounting.Outstanding.MinorUnits != int64(1-i)*5500 || last.Accounting.PaymentCount != uint64(i+1) {
					t.Fatalf("paid projection: %v", last.Accounting)
				}
				wantInput, wantDeferred := int64(1000), int64(0)
				if basis == tammyv1.GstBasis_GST_BASIS_CASH {
					wantInput = int64(i+1) * 500
					wantDeferred = int64(1-i) * 500
				}
				for code, want := range map[string]int64{"5001": 10000, "1000": -int64(i+1) * 5500, "2000": -int64(1-i) * 5500, "1150": wantInput, "1160": wantDeferred, "1170": 0} {
					if got := paymentBalance(t, tx, code); got != want {
						t.Fatalf("%s=%d want %d after payment %d", code, got, want, i+1)
					}
				}
				if last.Accounting.AttributedGst.MinorUnits != wantInput || last.Accounting.DeferredGst.MinorUnits != wantDeferred || last.Accounting.SuspenseGst.MinorUnits != 0 {
					t.Fatal("read projection differs from actual controls")
				}
			}
			if !bytes.Equal(initial, paymentEventBytes(t, tx, 1)) || posted.Accounting.Paid.MinorUnits != 0 || posted.Accounting.AccountingVersion != 1 {
				t.Fatal("payment changed immutable initial result")
			}
			for query, want := range map[string]int64{`SELECT count(*) FROM financial_revision_claims`: 3, `SELECT ledger_revision FROM financial_revisions WHERE id=1`: 3, `SELECT tax_source_revision FROM financial_revisions WHERE id=1`: 3, `SELECT settlement_revision FROM financial_revisions WHERE id=1`: 2, `SELECT count(*) FROM purchase_gst_attributions_v1`: 0} {
				if got := postScalar(t, tx, query); got != want {
					t.Fatalf("%s=%d want %d", query, got, want)
				}
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
			readTx, err := reopened.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer readTx.Rollback()
			stored, err := f.repo(readTx).Get(ctx, postingOrg, f.document.Id)
			if err != nil || !proto.Equal(stored, last.Accounting) {
				t.Fatalf("restart projection=%v error=%v", stored, err)
			}
			assertPaymentRetained(t, readTx, last, 3)
		})
	}
}

func TestPurchasePaymentMissingOrFutureEvidenceRetainsSuspense(t *testing.T) {
	for _, held := range []*tammyv1.CivilDate{nil, paymentDate(10, 1), paymentDate(10, 8)} {
		t.Run(strings.ReplaceAll(held.String(), " ", ""), func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			if held == nil {
				f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
				f.request.Evidence.Source = nil
				f.request.Evidence.HeldDate = nil
			} else {
				f.request.Evidence.HeldDate = held
			}
			repo := f.repo(tx)
			if _, err := repo.PostReviewed(context.Background(), f.request); err != nil {
				t.Fatal(err)
			}
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			response, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, 1, 5500, paymentDate(9, 30)))
			if err != nil {
				t.Fatal(err)
			}
			if response.Accounting.SuspenseGst.MinorUnits != 500 || response.Accounting.AttributedGst.MinorUnits != 0 || response.Accounting.DeferredGst.MinorUnits != 500 || paymentBalance(t, tx, "1170") != 500 || paymentBalance(t, tx, "1150") != 0 {
				t.Fatal("evidence released a historically unsupported payment")
			}
		})
	}
}

func TestPurchasePaymentRejectsInvalidBeforeMutation(t *testing.T) {
	for _, name := range []string{"no posting", "zero purchase", "zero", "negative", "foreign amount", "overpay", "advance", "bad date", "nil date", "stale", "version overflow", "foreign organisation", "foreign document", "noncash bank", "protected bank", "archived bank", "foreign bank", "basis changed", "basis history gap", "closed date", "long reference"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			if name == "zero purchase" {
				f.addDocument("Supplier", "ZERO", 0, 0)
			}
			repo := f.repo(tx)
			if name != "no posting" {
				if _, err := repo.PostReviewed(ctx, f.request); err != nil {
					t.Fatal(err)
				}
			}
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			request := paymentRequest(t, f, 1, 5500, paymentDate(9, 30))
			switch name {
			case "zero":
				request.Amount.MinorUnits = 0
			case "negative":
				request.Amount.MinorUnits = -1
			case "foreign amount":
				request.Amount.CurrencyCode = "USD"
			case "overpay":
				request.Amount.MinorUnits = 11001
			case "advance":
				request.ActualPaymentDate = postDate(9)
			case "bad date":
				request.ActualPaymentDate = paymentDate(9, 31)
			case "nil date":
				request.ActualPaymentDate = nil
			case "stale":
				request.ExpectedAccountingVersion = 2
			case "version overflow":
				request.ExpectedAccountingVersion = math.MaxUint64
			case "foreign organisation":
				request.OrganisationId = "018f0000-0000-7000-8000-000000009997"
			case "foreign document":
				request.DocumentId = "018f0000-0000-7000-8000-000000009996"
			case "foreign bank":
				request.BankAccountId = "018f0000-0000-7000-8000-000000009995"
			case "noncash bank":
				request.BankAccountId = postingExpense
			case "protected bank":
				request.BankAccountId = paymentAccountID(t, tx, "1150")
			case "archived bank":
				id, _ := f.id()
				accounts, _ := accounting.NewAccountRepository(tx)
				a := &tammyv1.Account{Id: id, OrganisationId: postingOrg, Version: 1, Code: "1001", Name: "Archived bank", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "balance_sheet.cash", CashFlowClassification: "operating"}
				if err := accounts.Create(ctx, a, f.now); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE accounts SET status='ARCHIVED',version=2 WHERE id=?`, id); err != nil {
					t.Fatal(err)
				}
				request.BankAccountId = id
			case "basis changed":
				if _, err := tx.ExecContext(ctx, `UPDATE organisations SET gst_basis=2 WHERE id=?`, postingOrg); err != nil {
					t.Fatal(err)
				}
			case "basis history gap":
				if _, err := tx.ExecContext(ctx, `UPDATE organisations SET version=2 WHERE id=?`, postingOrg); err != nil {
					t.Fatal(err)
				}
			case "closed date":
				if _, err := tx.ExecContext(ctx, `INSERT INTO accounting_periods(id,organisation_id,start_date,end_date,state,closed_at) VALUES(?,?,'2026-09-01','2026-09-30','CLOSED',?)`, "018f0000-0000-7000-8000-000000009994", postingOrg, f.now.Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
			case "long reference":
				request.Reference = strings.Repeat("x", 513)
			}
			before := map[string]int64{}
			for _, q := range []string{`SELECT count(*) FROM purchase_payments_v1`, `SELECT count(*) FROM purchase_payment_allocations_v1`, `SELECT count(*) FROM purchase_accounting_events_v1`, `SELECT count(*) FROM financial_revision_claims`, `SELECT count(*) FROM journals`, `SELECT count(*) FROM cash_flow_facts`, `SELECT financial_revision FROM financial_revisions WHERE id=1`} {
				before[q] = postScalar(t, tx, q)
			}
			if _, err := repo.RecordPayment(ctx, request); err == nil {
				t.Fatal("invalid payment accepted")
			}
			for q, want := range before {
				if got := postScalar(t, tx, q); got != want {
					t.Fatalf("rejected payment mutated %s: %d != %d", q, got, want)
				}
			}
		})
	}
}

func TestPurchasePaymentReadRejectsRetainedCorruption(t *testing.T) {
	for _, test := range []struct{ name, trigger, query string }{
		{"payment hash", "purchase_payments_v1_no_update", `UPDATE purchase_payments_v1 SET source_hash=zeroblob(32)`},
		{"payment scalar", "purchase_payments_v1_no_update", `UPDATE purchase_payments_v1 SET reference='changed'`},
		{"payment bytes", "purchase_payments_v1_no_update", `UPDATE purchase_payments_v1 SET payment_proto=x'01'`},
		{"payment missing", "purchase_payments_v1_no_delete", `DELETE FROM purchase_payments_v1`},
		{"allocation scalar", "purchase_payment_allocations_v1_no_update", `UPDATE purchase_payment_allocations_v1 SET gross_minor=5499`},
		{"allocation bytes", "purchase_payment_allocations_v1_no_update", `UPDATE purchase_payment_allocations_v1 SET allocation_proto=x'01'`},
		{"event vector", "purchase_accounting_events_v1_no_update", `UPDATE purchase_accounting_events_v1 SET settlement_revision=settlement_revision+1 WHERE accounting_version=2`},
		{"event hash", "purchase_accounting_events_v1_no_update", `UPDATE purchase_accounting_events_v1 SET result_sha256=zeroblob(32) WHERE accounting_version=2`},
		{"event missing", "purchase_accounting_events_v1_no_delete", `DELETE FROM purchase_accounting_events_v1 WHERE accounting_version=2`},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			repo := f.repo(tx)
			if _, err := repo.PostReviewed(ctx, f.request); err != nil {
				t.Fatal(err)
			}
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			if _, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 5500, paymentDate(9, 30))); err != nil {
				t.Fatal(err)
			}
			if test.name == "payment missing" {
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_payment_allocations_v1_no_delete`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM purchase_payment_allocations_v1`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+test.trigger); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, test.query); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Get(ctx, postingOrg, f.document.Id); !errors.Is(err, purchases.ErrRepository) {
				t.Fatalf("corrupt read=%v", err)
			}
			request := paymentRequest(t, f, 2, 5500, paymentDate(10, 2))
			before := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`)
			if _, err := repo.RecordPayment(ctx, request); err == nil {
				t.Fatal("command accepted corrupt retained history")
			}
			if postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`) != before {
				t.Fatal("corrupt command reserved a revision")
			}
		})
	}
}

func TestPurchasePaymentMixedClassificationsConserveNegativeCashFacts(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	accounts, _ := accounting.NewAccountRepository(tx)
	assetID, _ := f.id()
	privateID, _ := f.id()
	for _, a := range []*tammyv1.Account{
		{Id: assetID, OrganisationId: postingOrg, Version: 1, Code: "1501", Name: "Equipment", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "investing"},
		{Id: privateID, OrganisationId: postingOrg, Version: 1, Code: "3101", Name: "Drawings", Type: tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "financing"},
	} {
		if err := accounts.Create(ctx, a, f.now); err != nil {
			t.Fatal(err)
		}
	}
	expense := proto.Clone(f.request.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
	expense.Gross = postMoney(5500)
	expense.SupplierGst = postMoney(500)
	expense.EligibleGst = postMoney(400)
	expense.NoncreditableReason = "Partly noncreditable"
	asset := proto.Clone(expense).(*tammyv1.PurchaseAllocationInput)
	asset.Sequence = 2
	asset.AccountId = assetID
	asset.Gross = postMoney(3300)
	asset.SupplierGst = postMoney(300)
	asset.EligibleGst = postMoney(300)
	asset.NoncreditableReason = ""
	asset.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_CAPITAL
	asset.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_INVESTING
	private := proto.Clone(expense).(*tammyv1.PurchaseAllocationInput)
	private.Sequence = 3
	private.AccountId = privateID
	private.Gross = postMoney(2200)
	private.SupplierGst = postMoney(200)
	private.EligibleGst = postMoney(0)
	private.Use = tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE
	private.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED
	private.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING
	private.NoncreditableReason = "Private use"
	f.request.Allocations = []*tammyv1.PurchaseAllocationInput{expense, asset, private}
	repo := f.repo(tx)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	for i, date := range []*tammyv1.CivilDate{paymentDate(9, 30), paymentDate(10, 2)} {
		response, err := repo.RecordPayment(ctx, paymentRequest(t, f, uint64(i+1), 5500, date))
		if err != nil {
			t.Fatal(err)
		}
		assertPaymentRetained(t, tx, response, uint64(i+2))
		for j, want := range []int64{2750, 1650, 1100} {
			if response.Payment.Allocations[j].Gross.MinorUnits != want {
				t.Fatal("mixed allocation payment delta changed")
			}
		}
		if response.Payment.Allocations[0].EligibleGst.MinorUnits != 200 || response.Payment.Allocations[1].EligibleGst.MinorUnits != 150 || response.Payment.Allocations[2].EligibleGst.MinorUnits != 0 {
			t.Fatal("mixed/private eligibility payment delta changed")
		}
	}
	for category, want := range map[string]int64{"OPERATING": -5500, "INVESTING": -3300, "FINANCING": -2200} {
		if got := postScalar(t, tx, `SELECT COALESCE(SUM(f.amount_minor),0) FROM cash_flow_facts f JOIN journal_lines l ON l.id=f.journal_line_id JOIN journals j ON j.id=l.journal_id WHERE j.organisation_id=? AND j.source_kind=7 AND l.account_id=? AND f.category=?`, postingOrg, paymentAccountID(t, tx, "1000"), category); got != want {
			t.Fatalf("%s cash=%d want=%d", category, got, want)
		}
	}
	for code, want := range map[string]int64{"5001": 5100, "1501": 3000, "3101": 2200, "1150": 700, "1160": 0, "2000": 0, "1000": -11000} {
		if got := paymentBalance(t, tx, code); got != want {
			t.Fatalf("mixed ledger %s=%d want=%d", code, got, want)
		}
	}
	state, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil || state.Paid.MinorUnits != 11000 || state.AttributedGst.MinorUnits != 700 || state.EligibleGst.MinorUnits != 700 {
		t.Fatalf("mixed accounting=%v error=%v", state, err)
	}
}

func TestPurchasePaymentJournalOverflowRejectsBeforeRevisionReservation(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	f.addDocument("Large supplier", "MAX-GST", math.MaxInt64-1, 1)
	repo := f.repo(tx)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	before := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`)
	if _, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, math.MaxInt64, paymentDate(10, 2))); err == nil {
		t.Fatal("cash payment journal debit total overflow accepted")
	}
	if postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`) != before || postScalar(t, tx, `SELECT count(*) FROM purchase_payments_v1`) != 0 {
		t.Fatal("overflow reserved/wrote a payment")
	}
}

func TestPurchasePaymentConcurrentFinalCallerTransactionsAcceptExactlyOne(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	repo := f.repo(tx)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 5500, paymentDate(9, 30))); err != nil {
		t.Fatal(err)
	}
	requests := []*tammyv1.RecordPurchasePaymentRequest{paymentRequest(t, f, 2, 5500, paymentDate(10, 2)), paymentRequest(t, f, 2, 5500, paymentDate(10, 2))}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index, request := range requests {
		workers.Add(1)
		go func(index int, request *tammyv1.RecordPurchasePaymentRequest) {
			defer workers.Done()
			<-start
			commandTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				results <- err
				return
			}
			defer commandTx.Rollback()
			owned := *f
			owned.executor = commandTx
			owned.next = 9000 + index*100
			commandRepo, err := purchases.NewRepository(commandTx, func() time.Time { return owned.now }, owned.id)
			if err != nil {
				results <- err
				return
			}
			_, err = commandRepo.RecordPayment(ctx, request)
			if err == nil {
				err = commandTx.Commit()
			}
			results <- err
		}(index, request)
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
			t.Fatalf("unexpected concurrent transaction error: %v", err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
	}
	readTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	stored, err := f.repo(readTx).Get(ctx, postingOrg, f.document.Id)
	if err != nil || stored.Paid.MinorUnits != 11000 || stored.PaymentCount != 2 || stored.AccountingVersion != 3 || postScalar(t, readTx, `SELECT count(*) FROM financial_revision_claims`) != 3 {
		t.Fatalf("concurrent final state=%v error=%v", stored, err)
	}
}

func TestPurchasePaymentAppendedEvidenceOnlySupportsSubsequentCommand(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	review := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
	f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	f.request.Evidence.Source = nil
	f.request.Evidence.HeldDate = nil
	repo := f.repo(tx)
	posted, err := repo.PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	first, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 5500, paymentDate(9, 30)))
	if err != nil {
		t.Fatal(err)
	}
	firstEvent := append([]byte(nil), paymentEventBytes(t, tx, 2)...)
	// Retain a complete evidence command using the already-owned event/revision
	// ports. Its held date precedes the first payment, but its selection version
	// did not exist when that immutable payment command was accepted.
	review.HeldDate = paymentDate(9, 1)
	key, _ := f.id()
	evidenceID, _ := f.id()
	if _, err := tx.ExecContext(ctx, `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?,?,'tammy.v1.PurchaseService.RecordPurchaseTaxEvidence',?,'v1','tammy.v1.RecordPurchaseTaxEvidenceRequest',?,'ELECTED',?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, key, make([]byte, 32), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	revision, err := purchases.ReservePurchaseRevision(ctx, tx, key, "tammy.v1.PurchaseService.RecordPurchaseTaxEvidence", false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	evidence := &tammyv1.PurchaseTaxEvidence{Id: evidenceID, OrganisationId: postingOrg, DocumentId: f.document.Id, AccountingVersion: 3, Review: review, ActorUserId: testkit.ActorUserID, RecordedAt: timestamppb.New(f.now)}
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	evidence.SourceHash = hash[:]
	content, err = proto.MarshalOptions{Deterministic: true}.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO purchase_tax_evidence_v1(id,organisation_id,posting_id,document_id,accounting_version,classification,source_document_id,source_version,source_hash,held_date,registration_review_confirmed,business_use_review_confirmed,evidence_review_confirmed,reason,evidence_proto,record_hash,actor_user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, evidence.Id, postingOrg, posted.Posting.Id, f.document.Id, evidence.AccountingVersion, review.Classification, review.Source.SourceId, review.Source.SourceVersion, review.Source.SourceHash, "2026-09-01", true, true, true, review.Reason, content, evidence.SourceHash, evidence.ActorUserId, f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	state := proto.Clone(first.Accounting).(*tammyv1.PurchaseAccounting)
	state.AccountingVersion = 3
	state.ReviewOutcomes.Evidence = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	state.ReviewOutcomes.Timing = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW
	response := &tammyv1.RecordPurchaseTaxEvidenceResponse{Evidence: evidence, Accounting: state}
	if err := purchases.RecordAccountingEvent(ctx, tx, purchases.AccountingEvent{OperationKey: key, OrganisationID: postingOrg, ScopeKind: "POSTING", ScopeID: posted.Posting.Id, CommandType: "tammy.v1.PurchaseService.RecordPurchaseTaxEvidence", AccountingVersion: 3, Revision: revision, Result: response, Now: f.now}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil || current.AttributedGst.MinorUnits != 0 || current.SuspenseGst.MinorUnits != 500 || current.DeferredGst.MinorUnits != 500 || current.AccountingVersion != 3 {
		t.Fatalf("evidence retroactively claimed first payment: %v %v", current, err)
	}
	second, err := repo.RecordPayment(ctx, paymentRequest(t, f, 3, 5500, paymentDate(10, 2)))
	if err != nil {
		t.Fatal(err)
	}
	if second.Accounting.AttributedGst.MinorUnits != 500 || second.Accounting.SuspenseGst.MinorUnits != 500 || second.Accounting.DeferredGst.MinorUnits != 0 || paymentBalance(t, tx, "1150") != 500 || paymentBalance(t, tx, "1170") != 500 {
		t.Fatal("new payment did not select evidence visible at its command")
	}
	assertPaymentRetained(t, tx, second, 4)
	if !bytes.Equal(firstEvent, paymentEventBytes(t, tx, 2)) {
		t.Fatal("new evidence altered historical payment event")
	}
}

func TestPurchasePaymentReadRejectsRehashedFalseHistoricalResult(t *testing.T) {
	for _, name := range []string{"paid totals", "nil outcomes", "unknown outcomes", "false clear evidence"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
			f.request.Evidence.Source = nil
			f.request.Evidence.HeldDate = nil
			repo := f.repo(tx)
			if _, err := repo.PostReviewed(ctx, f.request); err != nil {
				t.Fatal(err)
			}
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			response, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 5500, paymentDate(9, 30)))
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "paid totals":
				response.Accounting.Paid.MinorUnits++
				response.Accounting.Outstanding.MinorUnits--
			case "nil outcomes":
				response.Accounting.ReviewOutcomes = nil
			case "unknown outcomes":
				response.Accounting.ReviewOutcomes.Evidence = tammyv1.PurchaseReviewStatus(99)
			case "false clear evidence":
				response.Accounting.ReviewOutcomes.Evidence = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
			}
			content, err := proto.MarshalOptions{Deterministic: true}.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(content)
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_accounting_events_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE purchase_accounting_events_v1 SET result_proto=?,result_sha256=? WHERE accounting_version=2`, content, hash[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Get(ctx, postingOrg, f.document.Id); !errors.Is(err, purchases.ErrRepository) {
				t.Fatalf("rehashed historical forgery=%v", err)
			}
		})
	}
}
