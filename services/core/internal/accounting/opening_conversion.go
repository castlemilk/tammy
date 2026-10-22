package accounting

import (
	"errors"
	"math"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrInvalidOpeningConversion = errors.New("accounting: invalid opening conversion")

func ValidateOpeningInputs(organisationID string, inputs []*tammyv1.OpeningBalanceInput,
	accounts map[string]*tammyv1.Account,
) error {
	if !ids.IsCanonicalV7(organisationID) || len(inputs) == 0 || len(inputs) > 5000 {
		return ErrInvalidOpeningConversion
	}
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input == nil || !ids.IsCanonicalV7(input.ClientLineId) || !ids.IsCanonicalV7(input.AccountId) ||
			input.LedgerBalance == nil || input.LedgerBalance.CurrencyCode != "AUD" || input.LedgerBalance.MinorUnits == 0 {
			return ErrInvalidOpeningConversion
		}
		if _, duplicate := seen[input.ClientLineId]; duplicate {
			return ErrInvalidOpeningConversion
		}
		seen[input.ClientLineId] = struct{}{}
		account := accounts[input.AccountId]
		if account == nil || account.OrganisationId != organisationID || ValidateAccount(account) != nil ||
			account.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE || !validOpeningKind(input, account) ||
			!validOpeningGST(input) {
			return ErrInvalidOpeningConversion
		}
	}
	return nil
}

func BuildOpeningJournal(organisationID string, date *tammyv1.CivilDate, inputs []*tammyv1.OpeningBalanceInput,
	accounts map[string]*tammyv1.Account, journalID string, lineIDs []string, financialRevision uint64, now time.Time,
) (*tammyv1.Journal, map[string][]CashFlowComponent, error) {
	if len(inputs) < 2 || len(lineIDs) != len(inputs) || !validCivilDate(date) || !ids.IsCanonicalV7(journalID) ||
		financialRevision == 0 || now.IsZero() || ValidateOpeningInputs(organisationID, inputs, accounts) != nil {
		return nil, nil, ErrInvalidOpeningConversion
	}
	journal := &tammyv1.Journal{Id: journalID, OrganisationId: organisationID, Version: 1,
		State:       tammyv1.JournalState_JOURNAL_STATE_POSTED,
		Source:      tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION,
		PostingDate: proto.Clone(date).(*tammyv1.CivilDate), Memo: "Opening conversion",
		Lines: make([]*tammyv1.JournalLine, 0, len(inputs)), PostedAt: timestamppb.New(now),
		FinancialRevision: financialRevision}
	flows := make(map[string][]CashFlowComponent, len(inputs))
	seenLineIDs := make(map[string]struct{}, len(inputs))
	for index, input := range inputs {
		if !ids.IsCanonicalV7(lineIDs[index]) {
			return nil, nil, ErrInvalidOpeningConversion
		}
		if _, duplicate := seenLineIDs[lineIDs[index]]; duplicate {
			return nil, nil, ErrInvalidOpeningConversion
		}
		seenLineIDs[lineIDs[index]] = struct{}{}
		account := accounts[input.AccountId]
		debit, credit, ok := ledgerNormalSides(account.NormalBalance, input.LedgerBalance.MinorUnits)
		if !ok {
			return nil, nil, ErrInvalidOpeningConversion
		}
		line := &tammyv1.JournalLine{Id: lineIDs[index], JournalId: journalID, AccountId: input.AccountId,
			Sequence: uint32(index + 1), Debit: audMoney(debit), Credit: audMoney(credit),
			Description: openingKindName(input.Kind)}
		journal.Lines = append(journal.Lines, line)
		flows[line.Id] = []CashFlowComponent{{Category: CashFlowNoncash, AmountMinor: debit - credit}}
	}
	debits, credits, err := CheckedJournalTotals(journal.Lines)
	if err != nil || debits == 0 || debits != credits {
		return nil, nil, ErrUnbalancedJournal
	}
	journal.TotalDebits, journal.TotalCredits = audMoney(debits), audMoney(credits)
	if err := ValidateJournal(journal, accounts, false); err != nil {
		return nil, nil, err
	}
	return journal, flows, nil
}

func validOpeningKind(input *tammyv1.OpeningBalanceInput, account *tammyv1.Account) bool {
	switch input.Kind {
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_ORDINARY:
		return account.Designation == tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY &&
			input.OriginalIssueDate == nil && input.OriginalDueDate == nil && input.LatestStatementDate == nil
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_CUSTOMER_OPEN_ITEM:
		return account.ReportClassification == "balance_sheet.receivables" && validCivilDate(input.OriginalIssueDate) &&
			validCivilDate(input.OriginalDueDate)
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM:
		return account.ReportClassification == "balance_sheet.payables" && validCivilDate(input.OriginalIssueDate) &&
			validCivilDate(input.OriginalDueDate)
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT:
		return IsCashAccount(account) && validCivilDate(input.LatestStatementDate) && input.LatestStatementBalance != nil &&
			input.LatestStatementBalance.CurrencyCode == "AUD"
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_UNALLOCATED_CREDIT:
		return account.ReportClassification == "balance_sheet.receivables" || account.ReportClassification == "balance_sheet.payables"
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_OPENING_EQUITY:
		return account.ReportClassification == "balance_sheet.opening_equity" &&
			account.Designation == tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_SYSTEM
	default:
		return false
	}
}

func validOpeningGST(input *tammyv1.OpeningBalanceInput) bool {
	if input.OutstandingGst == nil && input.PriorGstAttributed == nil {
		return true
	}
	if input.OutstandingGst == nil || input.PriorGstAttributed == nil ||
		input.OutstandingGst.CurrencyCode != "AUD" || input.PriorGstAttributed.CurrencyCode != "AUD" {
		return false
	}
	outstanding, attributed := input.OutstandingGst.MinorUnits, input.PriorGstAttributed.MinorUnits
	if outstanding == math.MinInt64 || attributed == math.MinInt64 {
		return false
	}
	if outstanding < 0 {
		outstanding = -outstanding
	}
	if attributed < 0 {
		attributed = -attributed
	}
	return attributed <= outstanding
}

func ledgerNormalSides(normal tammyv1.NormalBalance, balance int64) (int64, int64, bool) {
	if balance == math.MinInt64 {
		return 0, 0, false
	}
	if balance < 0 {
		if normal == tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT {
			return 0, -balance, true
		}
		if normal == tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT {
			return -balance, 0, true
		}
		return 0, 0, false
	}
	if normal == tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT {
		return balance, 0, true
	}
	if normal == tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT {
		return 0, balance, true
	}
	return 0, 0, false
}

func openingKindName(value tammyv1.OpeningBalanceKind) string {
	return map[tammyv1.OpeningBalanceKind]string{
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_ORDINARY:           "Opening balance",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_CUSTOMER_OPEN_ITEM: "Opening customer item",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM: "Opening supplier item",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT:  "Opening financial account",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_UNALLOCATED_CREDIT: "Opening unallocated credit",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_OPENING_EQUITY:     "Opening equity",
	}[value]
}
