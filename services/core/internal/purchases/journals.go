package purchases

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrPurchaseJournal = errors.New("purchases: invalid owned journal")

// JournalSpec is supplied by the transaction owner. Accounts and source identity
// are server-loaded; readiness must come from the historical evidence selection.
type JournalSpec struct {
	OrganisationID, SourceID          string
	SourceRevision, FinancialRevision uint64
	Date                              *tammyv1.CivilDate
	Memo                              string
	Accounts                          map[string]*tammyv1.Account
	Now                               time.Time
	NewID                             func() (string, error)
}

func BuildPurchaseJournal(spec JournalSpec, input ReviewedPurchaseInput, readiness EvidenceReadiness) (*tammyv1.Journal, map[string][]accounting.CashFlowComponent, error) {
	if !validJournalSpec(spec) || input.OrganisationID != spec.OrganisationID || !validReadiness(readiness) ||
		input.Document == nil || input.Document.Candidate == nil || !proto.Equal(spec.Date, input.Document.Candidate.DocumentDate) {
		return nil, nil, ErrPurchaseJournal
	}
	input.Accounts = spec.Accounts
	amounts, err := ValidateReviewedPurchase(input)
	if err != nil {
		return nil, nil, err
	}
	if amounts.Gross == 0 {
		return nil, nil, nil
	}
	journal, flows, err := newPurchaseJournal(spec, tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE)
	if err != nil {
		return nil, nil, err
	}
	for i, allocation := range input.Allocations {
		if err := addPurchaseLine(spec, journal, flows, allocation.AccountId, amounts.Allocations[i].Gross-amounts.Allocations[i].EligibleGST, 0); err != nil {
			return nil, nil, err
		}
	}
	if amounts.EligibleGST > 0 {
		code := "1160"
		if input.GSTBasis != tammyv1.GstBasis_GST_BASIS_CASH {
			code = claimControl(readiness)
		}
		control, err := purchaseControl(spec, code)
		if err != nil {
			return nil, nil, err
		}
		if err := addPurchaseLine(spec, journal, flows, control.Id, amounts.EligibleGST, 0); err != nil {
			return nil, nil, err
		}
	}
	payable, err := purchaseControl(spec, "2000")
	if err != nil {
		return nil, nil, err
	}
	if err := addPurchaseLine(spec, journal, flows, payable.Id, 0, amounts.Gross); err != nil {
		return nil, nil, err
	}
	return finishPurchaseJournal(spec, journal, flows)
}

func BuildPurchasePaymentJournal(spec JournalSpec, posting *tammyv1.PurchasePosting, paidBefore, amount int64,
	bank *tammyv1.Account, readiness EvidenceReadiness,
) (*tammyv1.Journal, map[string][]accounting.CashFlowComponent, []AllocationAmounts, error) {
	if !validJournalSpec(spec) || !validReadiness(readiness) || posting == nil || posting.OrganisationId != spec.OrganisationID ||
		!validPurchaseDate(posting.InvoiceDate) || purchaseDateKey(spec.Date) < purchaseDateKey(posting.InvoiceDate) ||
		!validPurchaseMoney(posting.Gross) || !validPurchaseMoney(posting.SupplierGst) || !validPurchaseMoney(posting.EligibleGst) ||
		(posting.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH && posting.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH && posting.GstBasis != tammyv1.GstBasis_GST_BASIS_UNSPECIFIED) ||
		bank == nil || accounting.ValidateAccount(bank) != nil || bank.OrganisationId != spec.OrganisationID || bank.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE || !accounting.IsCashAccount(bank) || bank.Type != tammyv1.AccountType_ACCOUNT_TYPE_ASSET {
		return nil, nil, nil, ErrPurchaseJournal
	}
	if bank.Designation != tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY {
		if bank.Designation != tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL || bank.Code != "1000" {
			return nil, nil, nil, accounting.ErrAccountNotPostable
		}
		installed, err := purchaseControl(spec, "1000")
		if err != nil || installed.Id != bank.Id {
			return nil, nil, nil, accounting.ErrAccountNotPostable
		}
	}
	lines := make([]AllocationAmounts, len(posting.Allocations))
	var gross, supplier, eligible int64
	for i, allocation := range posting.Allocations {
		if allocation == nil || allocation.Sequence != uint32(i+1) || !validPurchaseMoney(allocation.Gross) || !validPurchaseMoney(allocation.SupplierGst) || !validPurchaseMoney(allocation.EligibleGst) ||
			(allocation.CashFlowClassification != tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING && allocation.CashFlowClassification != tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_INVESTING && allocation.CashFlowClassification != tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING) {
			return nil, nil, nil, ErrPurchaseJournal
		}
		lines[i] = AllocationAmounts{allocation.Gross.MinorUnits, allocation.SupplierGst.MinorUnits, allocation.EligibleGst.MinorUnits}
		if !addPurchaseAmount(&gross, lines[i].Gross) || !addPurchaseAmount(&supplier, lines[i].SupplierGST) || !addPurchaseAmount(&eligible, lines[i].EligibleGST) {
			return nil, nil, nil, ErrPurchaseJournal
		}
	}
	if gross != posting.Gross.MinorUnits || supplier != posting.SupplierGst.MinorUnits || eligible != posting.EligibleGst.MinorUnits || supplier > 0 && posting.GstBasis == tammyv1.GstBasis_GST_BASIS_UNSPECIFIED {
		return nil, nil, nil, ErrPurchaseJournal
	}
	deltas, err := PaymentAllocationDeltas(lines, paidBefore, amount, gross)
	if err != nil {
		return nil, nil, nil, err
	}
	journal, flows, err := newPurchaseJournal(spec, tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT)
	if err != nil {
		return nil, nil, nil, err
	}
	payable, err := purchaseControl(spec, "2000")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := addPurchaseLine(spec, journal, flows, payable.Id, amount, 0); err != nil {
		return nil, nil, nil, err
	}
	if err := addPurchaseLine(spec, journal, flows, bank.Id, 0, amount); err != nil {
		return nil, nil, nil, err
	}
	bankLine := journal.Lines[len(journal.Lines)-1]
	var categories [3]int64
	var eligibleDelta int64
	for i, delta := range deltas {
		categories[int(posting.Allocations[i].CashFlowClassification)-1] += delta.Gross
		eligibleDelta += delta.EligibleGST
	}
	components := make([]accounting.CashFlowComponent, 0, 3)
	for i, extent := range categories {
		if extent > 0 {
			components = append(components, accounting.CashFlowComponent{Category: accounting.CashFlowCategory(i + 1), AmountMinor: -extent})
		}
	}
	flows[bankLine.Id] = components
	if posting.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH && eligibleDelta > 0 {
		deferred, err := purchaseControl(spec, "1160")
		if err != nil {
			return nil, nil, nil, err
		}
		claim, err := purchaseControl(spec, claimControl(readiness))
		if err != nil {
			return nil, nil, nil, err
		}
		if err := addPurchaseLine(spec, journal, flows, claim.Id, eligibleDelta, 0); err != nil {
			return nil, nil, nil, err
		}
		if err := addPurchaseLine(spec, journal, flows, deferred.Id, 0, eligibleDelta); err != nil {
			return nil, nil, nil, err
		}
	}
	completed, flows, err := finishPurchaseJournal(spec, journal, flows)
	return completed, flows, deltas, err
}

func BuildPurchaseGSTReleaseJournal(spec JournalSpec, amount int64) (*tammyv1.Journal, map[string][]accounting.CashFlowComponent, error) {
	if !validJournalSpec(spec) || amount <= 0 {
		return nil, nil, ErrPurchaseJournal
	}
	journal, flows, err := newPurchaseJournal(spec, tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION)
	if err != nil {
		return nil, nil, err
	}
	input, err := purchaseControl(spec, "1150")
	if err != nil {
		return nil, nil, err
	}
	suspense, err := purchaseControl(spec, "1170")
	if err != nil {
		return nil, nil, err
	}
	if err := addPurchaseLine(spec, journal, flows, input.Id, amount, 0); err != nil {
		return nil, nil, err
	}
	if err := addPurchaseLine(spec, journal, flows, suspense.Id, 0, amount); err != nil {
		return nil, nil, err
	}
	return finishPurchaseJournal(spec, journal, flows)
}

func validJournalSpec(spec JournalSpec) bool {
	return ids.IsCanonicalV7(spec.OrganisationID) && ids.IsCanonicalV7(spec.SourceID) && spec.SourceRevision > 0 && spec.SourceRevision <= math.MaxInt64 &&
		spec.FinancialRevision > 0 && spec.FinancialRevision <= math.MaxInt64 && validPurchaseDate(spec.Date) && purchaseReviewText(spec.Memo, 512, false) && !spec.Now.IsZero() && spec.NewID != nil
}
func validReadiness(readiness EvidenceReadiness) bool {
	return readiness >= EvidenceNormalSupported && readiness <= EvidenceUnsupportedException
}
func claimControl(readiness EvidenceReadiness) string {
	if readiness == EvidenceNormalSupported {
		return "1150"
	}
	return "1170"
}

func purchaseControl(spec JournalSpec, code string) (*tammyv1.Account, error) {
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		return nil, err
	}
	for _, expected := range template.Accounts {
		if expected.Code != code {
			continue
		}
		var found *tammyv1.Account
		for id, account := range spec.Accounts {
			if account == nil || account.Code != code {
				continue
			}
			if found != nil || account.Id != id || accounting.ValidateAccount(account) != nil || account.OrganisationId != spec.OrganisationID || account.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE ||
				account.Designation != tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL || account.Type != expected.Type || account.NormalBalance != expected.NormalBalance ||
				account.ReportClassification != expected.ReportClassification || account.CashFlowClassification != expected.CashFlowClassification {
				return nil, accounting.ErrAccountNotPostable
			}
			found = account
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, accounting.ErrAccountNotPostable
}

func newPurchaseJournal(spec JournalSpec, source tammyv1.JournalSource) (*tammyv1.Journal, map[string][]accounting.CashFlowComponent, error) {
	id, err := spec.NewID()
	if err != nil || !ids.IsCanonicalV7(id) {
		return nil, nil, ErrPurchaseJournal
	}
	return &tammyv1.Journal{Id: id, OrganisationId: spec.OrganisationID, Version: 1, Source: source, State: tammyv1.JournalState_JOURNAL_STATE_POSTED, PostingDate: proto.Clone(spec.Date).(*tammyv1.CivilDate), Memo: spec.Memo, FinancialRevision: spec.FinancialRevision, PostedAt: timestamppb.New(spec.Now)}, map[string][]accounting.CashFlowComponent{}, nil
}
func addPurchaseLine(spec JournalSpec, journal *tammyv1.Journal, flows map[string][]accounting.CashFlowComponent, account string, debit, credit int64) error {
	if debit == 0 && credit == 0 {
		return nil
	}
	id, err := spec.NewID()
	if err != nil || !ids.IsCanonicalV7(id) {
		return ErrPurchaseJournal
	}
	journal.Lines = append(journal.Lines, &tammyv1.JournalLine{Id: id, JournalId: journal.Id, AccountId: account, Sequence: uint32(len(journal.Lines) + 1), Debit: purchaseJournalMoney(debit), Credit: purchaseJournalMoney(credit)})
	flows[id] = []accounting.CashFlowComponent{{Category: accounting.CashFlowNoncash, AmountMinor: debit - credit}}
	return nil
}
func finishPurchaseJournal(spec JournalSpec, journal *tammyv1.Journal, flows map[string][]accounting.CashFlowComponent) (*tammyv1.Journal, map[string][]accounting.CashFlowComponent, error) {
	debit, credit, err := accounting.CheckedJournalTotals(journal.Lines)
	if err != nil {
		return nil, nil, err
	}
	journal.TotalDebits = purchaseJournalMoney(debit)
	journal.TotalCredits = purchaseJournalMoney(credit)
	if err := accounting.ValidateJournal(journal, spec.Accounts, false); err != nil {
		return nil, nil, err
	}
	for _, line := range journal.Lines {
		if err := accounting.ValidateCashFlowAllocation(line, accounting.IsCashAccount(spec.Accounts[line.AccountId]), flows[line.Id]); err != nil {
			return nil, nil, err
		}
	}
	return journal, flows, nil
}
func purchaseJournalMoney(amount int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: amount}
}

// JournalWriter posts within its caller's authenticated transaction. The caller
// reserves the revision vector once and persists the owned source and event.
type JournalWriter struct {
	executor             app.CommandSQLExecutor
	now                  func() time.Time
	newID                func() (string, error)
	reportingOwnerGuards bool
}

func NewJournalWriter(executor app.CommandSQLExecutor, now func() time.Time, newID func() (string, error)) (*JournalWriter, error) {
	if executor == nil || now == nil || newID == nil {
		return nil, ErrPurchaseJournal
	}
	return &JournalWriter{executor: executor, now: now, newID: newID}, nil
}

func (writer *JournalWriter) IsPostingDateOpen(ctx context.Context, organisationID string, date *tammyv1.CivilDate) error {
	if writer == nil || writer.executor == nil || ctx == nil {
		return ErrPurchaseJournal
	}
	periods, err := accounting.NewPeriodRepository(writer.executor)
	if err != nil {
		return err
	}
	open, err := periods.IsPostingDateOpen(ctx, organisationID, date)
	if err != nil {
		return err
	}
	if !open {
		return accounting.ErrClosedPeriod
	}
	return nil
}

func (writer *JournalWriter) PostPurchase(ctx context.Context, sourceID string, input ReviewedPurchaseInput, readiness EvidenceReadiness, revision uint64) (*tammyv1.Journal, error) {
	if input.Document == nil || input.Document.Candidate == nil {
		return nil, ErrPurchaseJournal
	}
	date := input.Document.Candidate.DocumentDate
	if err := writer.IsPostingDateOpen(ctx, input.OrganisationID, date); err != nil {
		return nil, err
	}
	spec, err := writer.spec(ctx, input.OrganisationID, sourceID, date, revision, input.Accounts)
	if err != nil {
		return nil, err
	}
	journal, flows, err := BuildPurchaseJournal(spec, input, readiness)
	if err != nil || journal == nil {
		return journal, err
	}
	return journal, writer.post(ctx, spec, journal, flows, "PAYABLE")
}

func (writer *JournalWriter) PostPayment(ctx context.Context, paymentID string, posting *tammyv1.PurchasePosting, paidBefore, amount int64, date *tammyv1.CivilDate, bankID string, readiness EvidenceReadiness, revision uint64) (*tammyv1.Journal, []AllocationAmounts, error) {
	if posting == nil {
		return nil, nil, ErrPurchaseJournal
	}
	if err := writer.IsPostingDateOpen(ctx, posting.OrganisationId, date); err != nil {
		return nil, nil, err
	}
	accounts, _ := accounting.NewAccountRepository(writer.executor)
	bank, err := accounts.Get(ctx, bankID)
	if err != nil {
		return nil, nil, err
	}
	spec, err := writer.spec(ctx, posting.OrganisationId, paymentID, date, revision, map[string]*tammyv1.Account{bank.Id: bank})
	if err != nil {
		return nil, nil, err
	}
	journal, flows, deltas, err := BuildPurchasePaymentJournal(spec, posting, paidBefore, amount, bank, readiness)
	if err != nil {
		return nil, nil, err
	}
	return journal, deltas, writer.post(ctx, spec, journal, flows, "BANKING")
}

func (writer *JournalWriter) ReleaseSuspense(ctx context.Context, organisationID, operationKey string, date *tammyv1.CivilDate, amount int64, revision uint64) (*tammyv1.Journal, error) {
	if err := writer.IsPostingDateOpen(ctx, organisationID, date); err != nil {
		return nil, err
	}
	spec, err := writer.spec(ctx, organisationID, operationKey, date, revision, nil)
	if err != nil {
		return nil, err
	}
	journal, flows, err := BuildPurchaseGSTReleaseJournal(spec, amount)
	if err != nil {
		return nil, err
	}
	return journal, writer.post(ctx, spec, journal, flows, "TAX")
}

func (writer *JournalWriter) spec(ctx context.Context, organisationID, sourceID string, date *tammyv1.CivilDate, revision uint64, base map[string]*tammyv1.Account) (JournalSpec, error) {
	spec := JournalSpec{OrganisationID: organisationID, SourceID: sourceID, SourceRevision: 1, FinancialRevision: revision, Date: date, Memo: "Purchase accounting", Accounts: map[string]*tammyv1.Account{}, Now: writer.now(), NewID: writer.newID}
	if !validJournalSpec(spec) {
		return JournalSpec{}, ErrPurchaseJournal
	}
	for id, account := range base {
		spec.Accounts[id] = account
	}
	query := `SELECT id FROM accounts WHERE organisation_id=? AND code IN ('2000','1150','1160','1170')`
	args := []any{organisationID}
	if writer.reportingOwnerGuards {
		query, args = postingReportingReadSQL(purchaseReportingControlIDReplayQuery(organisationID))
	}
	rows, err := writer.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return JournalSpec{}, err
	}
	var controlIDs []string
	for rows.Next() {
		var id string
		var scanErr error
		if writer.reportingOwnerGuards {
			scanErr = (purchaseReportingOwnerRow{rows: rows}).Scan(&id)
		} else {
			scanErr = rows.Scan(&id)
		}
		if scanErr != nil {
			_ = rows.Close()
			return JournalSpec{}, scanErr
		}
		controlIDs = append(controlIDs, id)
	}
	finishErr, closeErr := rows.Err(), rows.Close()
	if finishErr != nil || closeErr != nil {
		return JournalSpec{}, errors.Join(finishErr, closeErr)
	}
	accounts, _ := accounting.NewAccountRepository(writer.executor)
	for _, id := range controlIDs {
		var account *tammyv1.Account
		var err error
		if writer.reportingOwnerGuards {
			account, err = accounts.GetForReporting(ctx, organisationID, id)
		} else {
			account, err = accounts.Get(ctx, id)
		}
		if err != nil {
			return JournalSpec{}, err
		}
		spec.Accounts[id] = account
	}
	return spec, nil
}
func (writer *JournalWriter) post(ctx context.Context, spec JournalSpec, journal *tammyv1.Journal, flows map[string][]accounting.CashFlowComponent, sourceType string) error {
	repository, err := accounting.NewJournalRepository(writer.executor)
	if err != nil {
		return err
	}
	return repository.Post(ctx, journal, sourceType, spec.SourceID, spec.SourceRevision, nil, flows, spec.Now)
}
