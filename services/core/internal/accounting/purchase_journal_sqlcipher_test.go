//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/revisions"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPurchaseJournalAdmissionListingAndOwnedReversalGuard(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	organisationsRepo, _ := organisations.NewRepository(tx)
	if err := organisationsRepo.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	accounts := []*tammyv1.Account{
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000401", "5001", "Office expenses", "custom.account"),
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000402", "5002", "Other expenses", "custom.account"),
	}
	accountsRepo, _ := accounting.NewAccountRepository(tx)
	for _, account := range accounts {
		if err := accountsRepo.Create(ctx, account, now); err != nil {
			t.Fatal(err)
		}
	}
	repository, _ := accounting.NewJournalRepository(tx)
	sources := []struct {
		sql    string
		public tammyv1.JournalSource
		kind   int
	}{
		{"PAYABLE", tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE, 6},
		{"BANKING", tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT, 7},
		{"TAX", tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION, 8},
		{"BANKING", tammyv1.JournalSource_JOURNAL_SOURCE_INVOICE_PAYMENT, 0},
		{"RECEIVABLE", tammyv1.JournalSource_JOURNAL_SOURCE_RECEIVABLE, 0},
	}
	for index, source := range sources {
		revision, err := repository.ReserveFinancialRevision(ctx, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		journal := repositoryJournal(accounts, revision, now)
		journal.Id = fmt.Sprintf("018f0000-0000-7000-8000-%012d", 510+index*3)
		journal.Source = source.public
		flows := map[string][]accounting.CashFlowComponent{}
		for lineIndex, line := range journal.Lines {
			line.Id = fmt.Sprintf("018f0000-0000-7000-8000-%012d", 511+index*3+lineIndex)
			line.JournalId = journal.Id
			flows[line.Id] = []accounting.CashFlowComponent{{Category: accounting.CashFlowNoncash, AmountMinor: line.Debit.MinorUnits - line.Credit.MinorUnits}}
		}
		if err := repository.Post(ctx, journal, source.sql, journal.Id, 1, nil, flows, now); err != nil {
			t.Fatalf("post %s: %v", source.public, err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT source_kind FROM journals WHERE id=?`, journal.Id)
		if err != nil {
			t.Fatal(err)
		}
		var kind int
		if !rows.Next() || rows.Scan(&kind) != nil || kind != source.kind {
			t.Fatalf("stored source kind = %d, want %d", kind, source.kind)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		stored, err := repository.Get(ctx, journal.Id)
		if err != nil || stored.Source != source.public {
			t.Fatalf("get %s: %#v, %v", source.public, stored, err)
		}
		if _, _, err := repository.Reverse(ctx, journal.Id, 1, journal.PostingDate, "Reverse", "018f0000-0000-7000-8000-000000000599", []string{"018f0000-0000-7000-8000-000000000597", "018f0000-0000-7000-8000-000000000598"}, now); !errors.Is(err, accounting.ErrJournalSourceConflict) {
			t.Fatalf("generic reversal %s: %v", source.public, err)
		}
		rows, err = tx.QueryContext(ctx, `SELECT financial_revision FROM financial_revisions WHERE id=1`)
		if err != nil {
			t.Fatal(err)
		}
		var after uint64
		if !rows.Next() || rows.Scan(&after) != nil || after != revision {
			t.Fatalf("blocked reversal changed revision: %d -> %d", revision, after)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repository.List(ctx, accountTestOrganisationID, "", "", 200)
	if err != nil || len(page) != len(sources) {
		t.Fatalf("list = %d journals, %v", len(page), err)
	}
	for index, journal := range page {
		if journal.Source != sources[len(sources)-1-index].public {
			t.Fatalf("listed source = %s", journal.Source)
		}
	}
	journal := repositoryJournal(accounts, 1, now)
	journal.Source = tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT
	if err := repository.Post(ctx, journal, "PAYABLE", journal.Id, 1, nil, nil, now); !errors.Is(err, accounting.ErrJournalRepository) {
		t.Fatalf("mismatched source pair: %v", err)
	}
}

func TestPurchaseJournalRollbackRetainsNoJournalKindOrCashFlow(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	orgRepo, _ := organisations.NewRepository(tx)
	if err := orgRepo.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	accounts := []*tammyv1.Account{postingRepositoryAccount("018f0000-0000-7000-8000-000000000401", "5001", "Expense", "custom.account"), postingRepositoryAccount("018f0000-0000-7000-8000-000000000402", "5002", "Other", "custom.account")}
	accountRepo, _ := accounting.NewAccountRepository(tx)
	for _, account := range accounts {
		if err := accountRepo.Create(ctx, account, now); err != nil {
			t.Fatal(err)
		}
	}
	repository, _ := accounting.NewJournalRepository(&purchaseCashFlowFailure{CommandSQLExecutor: tx})
	revision, err := repository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	journal := repositoryJournal(accounts, revision, now)
	journal.Source = tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE
	flows := map[string][]accounting.CashFlowComponent{journal.Lines[0].Id: {{Category: accounting.CashFlowNoncash, AmountMinor: 31900}}, journal.Lines[1].Id: {{Category: accounting.CashFlowNoncash, AmountMinor: -31900}}}
	if err := repository.Post(ctx, journal, "PAYABLE", journal.Id, 1, nil, flows, now); !errors.Is(err, accounting.ErrJournalRepository) {
		t.Fatalf("injected cash-flow insertion failure = %v", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT (SELECT count(*) FROM journals WHERE source_kind=6),(SELECT count(*) FROM cash_flow_facts)`)
	if err != nil {
		t.Fatal(err)
	}
	var journalCount, flowCount int
	if !rows.Next() || rows.Scan(&journalCount, &flowCount) != nil || journalCount != 1 || flowCount != 1 {
		t.Fatalf("failure did not follow a partial insert: journal=%d flow=%d", journalCount, flowCount)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	verify, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Rollback()
	for _, query := range []string{`SELECT count(*) FROM journals WHERE source_kind IN (6,7,8)`, `SELECT count(*) FROM cash_flow_facts`, `SELECT financial_revision FROM financial_revisions WHERE id=1`} {
		rows, err := verify.QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if !rows.Next() || rows.Scan(&count) != nil || count != 0 {
			t.Fatalf("rollback left %d: %s", count, query)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

type purchaseCashFlowFailure struct {
	app.CommandSQLExecutor
	inserts int
}

func (executor *purchaseCashFlowFailure) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "INSERT INTO cash_flow_facts") {
		executor.inserts++
		if executor.inserts == 2 {
			return nil, errors.New("injected cash flow failure")
		}
	}
	return executor.CommandSQLExecutor.ExecContext(ctx, query, args...)
}

func TestPurchaseJournalWriterPersistsCashFactsAndChecksClosedZeroAndReleaseDates(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	orgRepo, _ := organisations.NewRepository(tx)
	if err := orgRepo.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	accountRepo, _ := accounting.NewAccountRepository(tx)
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	if err := accountRepo.InstallTemplate(ctx, accountTestOrganisationID, template, now); err != nil {
		t.Fatal(err)
	}
	expense := &tammyv1.Account{Id: "018f0000-0000-7000-8000-000000000651", OrganisationId: accountTestOrganisationID, Version: 1, Code: "5001", Name: "Expense", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "operating"}
	if err := accountRepo.Create(ctx, expense, now); err != nil {
		t.Fatal(err)
	}
	date := &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10}
	docID := "018f0000-0000-7000-8000-000000000650"
	money := func(amount int64) *tammyv1.Money { return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: amount} }
	input := purchases.ReviewedPurchaseInput{OrganisationID: accountTestOrganisationID, DocumentID: docID, ReviewedVersion: 2, GSTBasis: tammyv1.GstBasis_GST_BASIS_CASH, GSTProfileVersion: 1, Accounts: map[string]*tammyv1.Account{expense.Id: expense},
		Document:    &tammyv1.Document{Id: docID, OrganisationId: accountTestOrganisationID, Version: 2, Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, Sha256: make([]byte, 32), ReviewedAt: timestamppb.New(now), Candidate: &tammyv1.DocumentCandidate{SupplierName: "Supplier", InvoiceNumber: "COST-1", DocumentDate: date, Subtotal: money(10000), Gst: money(1000), Total: money(11000)}},
		Allocations: []*tammyv1.PurchaseAllocationInput{{AccountId: expense.Id, AccountVersion: 1, Sequence: 1, Gross: money(11000), SupplierGst: money(1000), EligibleGst: money(1000), Use: tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS, BasClassification: tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL, CashFlowClassification: tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING, EligibilityReviewConfirmed: true, EvidenceReviewConfirmed: true}},
		Evidence:    &tammyv1.PurchaseEvidenceInput{Classification: tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN, RegistrationReviewConfirmed: true, BusinessUseReviewConfirmed: true, EvidenceReviewConfirmed: true, Reason: "Awaiting tax invoice"}}
	next := 660
	newID := func() (string, error) { next++; return fmt.Sprintf("018f0000-0000-7000-8000-%012d", next), nil }
	writer, err := purchases.NewJournalWriter(tx, func() time.Time { return now }, newID)
	if err != nil {
		t.Fatal(err)
	}
	postingKey := "018f0000-0000-7000-8000-000000000640"
	seedPurchaseElection(t, tx, postingKey, "PostReviewedPurchase")
	revision, err := purchases.ReservePurchaseRevision(ctx, tx, postingKey, "tammy.v1.PurchaseService.PostReviewedPurchase", true, now)
	if err != nil {
		t.Fatal(err)
	}
	postingJournal, err := writer.PostPurchase(ctx, docID, input, purchases.EvidenceMissing, revision.Financial)
	if err != nil {
		t.Fatal(err)
	}
	if postingJournal.FinancialRevision != 1 || revision.Ledger != 1 || revision.TaxSource != 1 || revision.Settlement != 0 {
		t.Fatalf("posting vector = %#v", revision)
	}
	accounts, err := accountRepo.List(ctx, accountTestOrganisationID, "", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	var bankID string
	for _, account := range accounts {
		if account.Code == "1000" {
			bankID = account.Id
		}
	}
	posting := &tammyv1.PurchasePosting{Id: docID, OrganisationId: accountTestOrganisationID, InvoiceDate: date, GstBasis: input.GSTBasis, Gross: money(11000), SupplierGst: money(1000), EligibleGst: money(1000), Allocations: input.Allocations}
	paymentKey := "018f0000-0000-7000-8000-000000000641"
	seedPurchaseElection(t, tx, paymentKey, "RecordPurchasePayment")
	revision, err = purchases.ReservePurchaseRevision(ctx, tx, paymentKey, "tammy.v1.PurchaseService.RecordPurchasePayment", true, now)
	if err != nil {
		t.Fatal(err)
	}
	payment, _, err := writer.PostPayment(ctx, paymentKey, posting, 0, 5500, date, bankID, purchases.EvidenceMissing, revision.Financial)
	if err != nil {
		t.Fatal(err)
	}
	if payment.FinancialRevision != 2 || revision.Ledger != 2 || revision.TaxSource != 2 || revision.Settlement != 1 {
		t.Fatalf("payment vector = %#v", revision)
	}
	rows, err := tx.QueryContext(ctx, `SELECT f.category,f.amount_minor FROM cash_flow_facts f JOIN journal_lines l ON l.id=f.journal_line_id WHERE l.journal_id=? AND l.account_id=?`, payment.Id, bankID)
	if err != nil {
		t.Fatal(err)
	}
	var category string
	var amount int64
	if !rows.Next() || rows.Scan(&category, &amount) != nil || category != "OPERATING" || amount != -5500 || rows.Next() {
		t.Fatalf("persisted payment cash facts: %s %d", category, amount)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	releaseKey := "018f0000-0000-7000-8000-000000000642"
	seedPurchaseElection(t, tx, releaseKey, "AttributePurchaseGst")
	revision, err = purchases.ReservePurchaseRevision(ctx, tx, releaseKey, "tammy.v1.PurchaseService.AttributePurchaseGst", true, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ReleaseSuspense(ctx, accountTestOrganisationID, releaseKey, date, 250, revision.Financial); err != nil {
		t.Fatal(err)
	}
	for _, value := range []*tammyv1.Money{input.Document.Candidate.Subtotal, input.Document.Candidate.Gst, input.Document.Candidate.Total, input.Allocations[0].Gross, input.Allocations[0].SupplierGst, input.Allocations[0].EligibleGst} {
		value.MinorUnits = 0
	}
	zeroKey := "018f0000-0000-7000-8000-000000000644"
	seedPurchaseElection(t, tx, zeroKey, "PostReviewedPurchase")
	revision, err = purchases.ReservePurchaseRevision(ctx, tx, zeroKey, "tammy.v1.PurchaseService.PostReviewedPurchase", false, now)
	if err != nil {
		t.Fatal(err)
	}
	if zero, err := writer.PostPurchase(ctx, zeroKey, input, purchases.EvidenceMissing, revision.Financial); err != nil || zero != nil || revision.Financial != 4 || revision.Ledger != 3 || revision.TaxSource != 4 || revision.Settlement != 1 {
		t.Fatalf("zero purchase reservation = %#v journal=%v err=%v", revision, zero, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO accounting_periods(id,organisation_id,start_date,end_date,state,version,closed_at) VALUES(?,?,'2026-07-01','2026-08-10','CLOSED',1,?)`, "018f0000-0000-7000-8000-000000000643", accountTestOrganisationID, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.PostPurchase(ctx, postingKey, input, purchases.EvidenceMissing, revision.Financial); !errors.Is(err, accounting.ErrClosedPeriod) {
		t.Fatalf("closed zero purchase = %v", err)
	}
	if _, _, err := writer.PostPayment(ctx, paymentKey, posting, 0, 1, date, bankID, purchases.EvidenceMissing, revision.Financial); !errors.Is(err, accounting.ErrClosedPeriod) {
		t.Fatalf("closed payment = %v", err)
	}
	if _, err := writer.ReleaseSuspense(ctx, accountTestOrganisationID, releaseKey, date, 1, revision.Financial); !errors.Is(err, accounting.ErrClosedPeriod) {
		t.Fatalf("closed release = %v", err)
	}
	rows, err = tx.QueryContext(ctx, `SELECT financial_revision,ledger_revision,tax_source_revision,settlement_revision FROM financial_revisions WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	var financial, ledger, tax, settlement uint64
	if !rows.Next() || rows.Scan(&financial, &ledger, &tax, &settlement) != nil || financial != 4 || ledger != 3 || tax != 4 || settlement != 1 {
		t.Fatalf("writers reserved an extra revision: %d/%d/%d/%d", financial, ledger, tax, settlement)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}

func seedPurchaseElection(t *testing.T, executor app.CommandSQLExecutor, key, command string) {
	t.Helper()
	_, err := executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?, ?, ?, ?, 'v1', ?, ?, 'ELECTED', '2026-08-10T12:00:00Z')`, "018f0000-0000-7000-8000-000000000099", testkit.ActorUserID, "tammy.v1.PurchaseService."+command, key, "tammy.v1."+command+"Request", make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseRevisionRetainsSharedVectorAndRejectsCounterfeitEvent(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	orgRepo, _ := organisations.NewRepository(tx)
	if err := orgRepo.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,'purchase@example.test','Owner','ACTIVE',?,?)`, testkit.ActorUserID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	docID := "018f0000-0000-7000-8000-000000000630"
	if _, err := tx.ExecContext(ctx, `INSERT INTO documents(id,operation_key,organisation_id,version,status,source_display_name,mime_type,byte_length,sha256,original_bytes,extracted_text,supplier_name,invoice_number,subtotal_minor,gst_minor,total_minor,created_by_user_id,created_at,reviewed_at) VALUES(?,?,?,1,'NEEDS_REVIEW','cost.pdf','application/pdf',1,zeroblob(32),x'01','','Supplier','COST-1',100,10,110,?,?,NULL)`, docID, docID, accountTestOrganisationID, testkit.ActorUserID, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	key := "018f0000-0000-7000-8000-000000000631"
	seedPurchaseElection(t, tx, key, "ResolveLegacyPurchaseReview")
	command := "tammy.v1.PurchaseService.ResolveLegacyPurchaseReview"
	event := purchases.AccountingEvent{OperationKey: key, OrganisationID: accountTestOrganisationID, ScopeKind: "DOCUMENT", ScopeID: docID, CommandType: command, AccountingVersion: 1, Revision: revisions.Snapshot{Financial: 1, TaxSource: 1}, Result: &tammyv1.ResolveLegacyPurchaseReviewResponse{Review: &tammyv1.LegacyPurchaseReview{Id: key}}, Now: now}
	if err := purchases.RecordAccountingEvent(ctx, tx, event); !errors.Is(err, purchases.ErrPurchaseRevision) {
		t.Fatalf("event without owned reservation = %v", err)
	}
	snapshot, err := purchases.ReservePurchaseRevision(ctx, tx, key, command, false, now)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := purchases.ReservePurchaseRevision(ctx, tx, key, command, false, now.Add(time.Hour))
	if err != nil || snapshot != replayed {
		t.Fatalf("reservation replay = %#v,%v", replayed, err)
	}
	if _, err := purchases.ReservePurchaseRevision(ctx, tx, key, "tammy.v1.PurchaseService.PostReviewedPurchase", true, now); !errors.Is(err, revisions.ErrReplayConflict) {
		t.Fatalf("changed-domain replay = %v", err)
	}
	event.Revision = snapshot
	event.Revision.Financial++
	if err := purchases.RecordAccountingEvent(ctx, tx, event); !errors.Is(err, purchases.ErrPurchaseRevision) {
		t.Fatalf("counterfeit vector = %v", err)
	}
	event.Revision = snapshot
	wrongDomains := event
	wrongDomains.CommandType = "tammy.v1.PurchaseService.PostReviewedPurchase"
	wrongDomains.ScopeKind = "POSTING"
	wrongDomains.Result = &tammyv1.PostReviewedPurchaseResponse{Accounting: &tammyv1.PurchaseAccounting{OrganisationId: accountTestOrganisationID}}
	wrongDomains.JournalID = &docID
	if err := purchases.RecordAccountingEvent(ctx, tx, wrongDomains); !errors.Is(err, purchases.ErrPurchaseRevision) {
		t.Fatalf("counterfeit domain mask = %v", err)
	}
	if err := purchases.RecordAccountingEvent(ctx, tx, event); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT financial_revision,ledger_revision,tax_source_revision,settlement_revision,result_proto FROM purchase_accounting_events_v1 WHERE operation_key=?`, key)
	if err != nil {
		t.Fatal(err)
	}
	var financial, ledger, tax, settlement uint64
	var content []byte
	if !rows.Next() || rows.Scan(&financial, &ledger, &tax, &settlement, &content) != nil || financial != 1 || ledger != 0 || tax != 1 || settlement != 0 {
		t.Fatalf("retained event vector = %d/%d/%d/%d", financial, ledger, tax, settlement)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	result := &tammyv1.ResolveLegacyPurchaseReviewResponse{}
	if err := proto.Unmarshal(content, result); err != nil || !proto.Equal(result, event.Result) {
		t.Fatalf("retained event result = %v,%v", result, err)
	}
	if err := purchases.RecordAccountingEvent(ctx, tx, event); err == nil {
		t.Fatal("immutable event replay replaced original")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_accounting_events_v1 SET ledger_revision=1 WHERE operation_key=?`, key); err == nil {
		t.Fatal("event vector mutation succeeded")
	}
}

func TestPurchaseRevisionOverflowRollsBackReservationAndClaim(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	key := "018f0000-0000-7000-8000-000000000632"
	seedPurchaseElection(t, tx, key, "ResolveLegacyPurchaseReview")
	if _, err := tx.ExecContext(ctx, `UPDATE financial_revisions SET financial_revision=? WHERE id=1`, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if _, err := purchases.ReservePurchaseRevision(ctx, tx, key, "tammy.v1.PurchaseService.ResolveLegacyPurchaseReview", false, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("financial revision overflow accepted")
	}
	rows, err := tx.QueryContext(ctx, `SELECT financial_revision,tax_source_revision,(SELECT count(*) FROM financial_revision_claims) FROM financial_revisions WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	var financial int64
	var tax, claims int
	if !rows.Next() || rows.Scan(&financial, &tax, &claims) != nil || financial != math.MaxInt64 || tax != 0 || claims != 0 {
		t.Fatalf("failed reservation left financial=%d tax=%d claims=%d", financial, tax, claims)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseRevisionEventRequiresLinkedJournalReservation(t *testing.T) {
	for _, test := range []struct{ journal, posting bool }{{false, false}, {true, false}, {false, true}} {
		mismatched := test.journal
		t.Run(fmt.Sprintf("journal=%t/posting=%t", test.journal, test.posting), func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
			orgRepo, _ := organisations.NewRepository(tx)
			if err := orgRepo.Create(ctx, accountTestOrganisation(), now); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,'purchase@example.test','Owner','ACTIVE',?,?)`, testkit.ActorUserID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			accountRepo, _ := accounting.NewAccountRepository(tx)
			template, err := accounting.LoadAUSmallBusinessV1()
			if err != nil {
				t.Fatal(err)
			}
			if err := accountRepo.InstallTemplate(ctx, accountTestOrganisationID, template, now); err != nil {
				t.Fatal(err)
			}
			docID := "018f0000-0000-7000-8000-000000000620"
			postingID := "018f0000-0000-7000-8000-000000000621"
			// The zero purchase is a valid retained source without a posting journal.
			// This isolates the command/journal revision check from tax-portion policy.
			if _, err := tx.ExecContext(ctx, `INSERT INTO documents(id,operation_key,organisation_id,version,status,source_display_name,mime_type,byte_length,sha256,original_bytes,extracted_text,supplier_name,invoice_number,document_date,subtotal_minor,gst_minor,total_minor,created_by_user_id,created_at,reviewed_at) VALUES(?,?,?,2,'REVIEWED','cost.pdf','application/pdf',1,zeroblob(32),x'01','','Supplier','COST-1','2026-08-10',0,0,0,?,?,?)`, docID, docID, accountTestOrganisationID, testkit.ActorUserID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			postingKey := "018f0000-0000-7000-8000-000000000622"
			seedPurchaseElection(t, tx, postingKey, "PostReviewedPurchase")
			initial, err := purchases.ReservePurchaseRevision(ctx, tx, postingKey, "tammy.v1.PurchaseService.PostReviewedPurchase", false, now)
			if err != nil {
				t.Fatal(err)
			}
			postingRevision := initial.Financial
			if test.posting {
				postingRevision++
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO purchase_postings_v1(id,organisation_id,document_id,reviewed_version,original_content_hash,reviewed_snapshot_hash,reviewed_source_proto,supplier_name,invoice_number,invoice_date,gst_basis,gst_profile_version,gst_history_watermark,gross_minor,supplier_gst_minor,eligible_gst_minor,currency_code,journal_id,financial_revision,duplicate_decision,duplicate_reason,prior_accounting_decision,prior_accounting_reason,normalized_supplier,normalized_invoice,source_hash,posting_proto,actor_user_id,created_at) VALUES(?,?,?,2,zeroblob(32),zeroblob(32),x'01','Supplier','COST-1','2026-08-10',1,1,0,0,0,0,'AUD',NULL,?,1,'',1,'Reviewed','supplier','cost-1',zeroblob(32),x'01',?,?)`, postingID, accountTestOrganisationID, docID, postingRevision, testkit.ActorUserID, now.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			initialEvent := purchases.AccountingEvent{OperationKey: postingKey, OrganisationID: accountTestOrganisationID, ScopeKind: "POSTING", ScopeID: postingID, CommandType: "tammy.v1.PurchaseService.PostReviewedPurchase", AccountingVersion: 1, Revision: initial, Result: &tammyv1.PostReviewedPurchaseResponse{Posting: &tammyv1.PurchasePosting{Id: postingID}}, Now: now}
			err = purchases.RecordAccountingEvent(ctx, tx, initialEvent)
			if test.posting {
				if !errors.Is(err, purchases.ErrPurchaseRevision) {
					t.Fatalf("posting/event revision mismatch accepted: posting=%d event=%d err=%v", postingRevision, initial.Financial, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			key := "018f0000-0000-7000-8000-000000000623"
			seedPurchaseElection(t, tx, key, "AttributePurchaseGst")
			snapshot, err := purchases.ReservePurchaseRevision(ctx, tx, key, "tammy.v1.PurchaseService.AttributePurchaseGst", true, now)
			if err != nil {
				t.Fatal(err)
			}
			next := 800
			writer, err := purchases.NewJournalWriter(tx, func() time.Time { return now }, func() (string, error) { next++; return fmt.Sprintf("018f0000-0000-7000-8000-%012d", next), nil })
			if err != nil {
				t.Fatal(err)
			}
			journalRevision := snapshot.Financial
			if mismatched {
				journalRevision++
			}
			journal, err := writer.ReleaseSuspense(ctx, accountTestOrganisationID, key, &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10}, 100, journalRevision)
			if err != nil {
				t.Fatal(err)
			}
			event := purchases.AccountingEvent{OperationKey: key, OrganisationID: accountTestOrganisationID, ScopeKind: "POSTING", ScopeID: postingID, CommandType: "tammy.v1.PurchaseService.AttributePurchaseGst", AccountingVersion: 2, Revision: snapshot, Result: &tammyv1.AttributePurchaseGstResponse{Journal: journal}, JournalID: &journal.Id, Now: now}
			err = purchases.RecordAccountingEvent(ctx, tx, event)
			if mismatched {
				if !errors.Is(err, purchases.ErrPurchaseRevision) {
					t.Fatalf("journal/event revision mismatch accepted: journal=%d event=%d err=%v", journalRevision, snapshot.Financial, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			rows, err := tx.QueryContext(ctx, `SELECT count(*) FROM purchase_accounting_events_v1 WHERE operation_key=?`, key)
			if err != nil {
				t.Fatal(err)
			}
			var count int
			if !rows.Next() || rows.Scan(&count) != nil || (mismatched && count != 0) || (!mismatched && count != 1) {
				t.Fatalf("retained release event count = %d", count)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
