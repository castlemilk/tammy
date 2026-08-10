package accounting

import (
	"errors"
	"math"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	testJournalID = "018f0000-0000-7000-8000-000000000301"
	testLineOneID = "018f0000-0000-7000-8000-000000000302"
	testLineTwoID = "018f0000-0000-7000-8000-000000000303"
	testAssetID   = "018f0000-0000-7000-8000-000000000304"
	testExpenseID = "018f0000-0000-7000-8000-000000000305"
)

func TestValidateJournalRequiresBalancedImmutableAUDLines(t *testing.T) {
	accounts := map[string]*tammyv1.Account{
		testAssetID:   postingAccount(testAssetID, tammyv1.AccountType_ACCOUNT_TYPE_ASSET),
		testExpenseID: postingAccount(testExpenseID, tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE),
	}
	valid := balancedJournal()
	if err := ValidateJournal(valid, accounts, true); err != nil {
		t.Fatalf("valid journal rejected: %v", err)
	}

	tests := map[string]func(*tammyv1.Journal){
		"one line":           func(j *tammyv1.Journal) { j.Lines = j.Lines[:1] },
		"unbalanced":         func(j *tammyv1.Journal) { j.Lines[1].Credit.MinorUnits-- },
		"non AUD":            func(j *tammyv1.Journal) { j.Lines[0].Debit.CurrencyCode = "USD" },
		"both sides":         func(j *tammyv1.Journal) { j.Lines[0].Credit.MinorUnits = 1 },
		"duplicate sequence": func(j *tammyv1.Journal) { j.Lines[1].Sequence = 1 },
		"wrong parent":       func(j *tammyv1.Journal) { j.Lines[0].JournalId = testLineOneID },
		"protected manual account": func(j *tammyv1.Journal) {
			accounts[testAssetID].Designation = tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL
		},
		"archived account": func(j *tammyv1.Journal) {
			accounts[testAssetID].Status = tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			localAccounts := cloneAccountMap(accounts)
			journal := cloneJournal(valid)
			mutateWithAccounts(name, mutate, journal, localAccounts)
			if err := ValidateJournal(journal, localAccounts, true); err == nil {
				t.Fatal("invalid journal accepted")
			}
		})
	}
}

func TestCheckedJournalTotalsRejectOverflow(t *testing.T) {
	lines := []*tammyv1.JournalLine{
		{Debit: aud(math.MaxInt64), Credit: aud(0)},
		{Debit: aud(1), Credit: aud(0)},
	}
	if _, _, err := CheckedJournalTotals(lines); !errors.Is(err, ErrInvalidJournal) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestBuildJournalReversalCreatesLinkedImmutableInverse(t *testing.T) {
	original := balancedJournal()
	reversed, reversal, err := BuildJournalReversal(original,
		"018f0000-0000-7000-8000-000000000320",
		[]string{"018f0000-0000-7000-8000-000000000321", "018f0000-0000-7000-8000-000000000322"},
		&tammyv1.CivilDate{Year: 2024, Month: 5, Day: 13}, "Correction", 2,
		time.Date(2024, 5, 13, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if reversed.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED || reversed.Version != 2 ||
		reversed.GetReversedByJournalId() != reversal.Id || reversal.GetReversalOfJournalId() != original.Id ||
		reversal.Lines[0].Debit.MinorUnits != original.Lines[0].Credit.MinorUnits ||
		reversal.Lines[0].Credit.MinorUnits != original.Lines[0].Debit.MinorUnits ||
		reversal.FinancialRevision != 2 {
		t.Fatalf("reversed=%#v reversal=%#v", reversed, reversal)
	}
	if original.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || original.ReversedByJournalId != nil {
		t.Fatal("original input was mutated")
	}
}

func balancedJournal() *tammyv1.Journal {
	return &tammyv1.Journal{
		Id: testJournalID, OrganisationId: "018f0000-0000-7000-8000-000000000020", Version: 1,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, Source: tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		PostingDate: &tammyv1.CivilDate{Year: 2024, Month: 5, Day: 12}, Memo: "Officeworks invoice",
		Lines: []*tammyv1.JournalLine{
			{Id: testLineOneID, JournalId: testJournalID, AccountId: testExpenseID, Sequence: 1, Debit: aud(31900), Credit: aud(0), Description: "Expense"},
			{Id: testLineTwoID, JournalId: testJournalID, AccountId: testAssetID, Sequence: 2, Debit: aud(0), Credit: aud(31900), Description: "Bank"},
		},
		TotalDebits: aud(31900), TotalCredits: aud(31900), FinancialRevision: 1,
		PostedAt: timestamppb.New(time.Date(2024, 5, 12, 1, 2, 3, 0, time.UTC)),
	}
}

func postingAccount(id string, accountType tammyv1.AccountType) *tammyv1.Account {
	return &tammyv1.Account{Id: id, OrganisationId: "018f0000-0000-7000-8000-000000000020", Version: 1,
		Code: "1000", Name: "Posting account", Type: accountType,
		NormalBalance:        tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
		Status:               tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation:          tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: "balance_sheet", CashFlowClassification: "operating"}
}

func aud(minor int64) *tammyv1.Money { return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor} }

func cloneJournal(value *tammyv1.Journal) *tammyv1.Journal {
	copy := *value
	copy.TotalDebits = aud(value.TotalDebits.MinorUnits)
	copy.TotalCredits = aud(value.TotalCredits.MinorUnits)
	copy.PostingDate = &tammyv1.CivilDate{Year: value.PostingDate.Year, Month: value.PostingDate.Month, Day: value.PostingDate.Day}
	copy.PostedAt = timestamppb.New(value.PostedAt.AsTime())
	copy.Lines = make([]*tammyv1.JournalLine, len(value.Lines))
	for index, line := range value.Lines {
		lineCopy := *line
		lineCopy.Debit = aud(line.Debit.MinorUnits)
		lineCopy.Credit = aud(line.Credit.MinorUnits)
		copy.Lines[index] = &lineCopy
	}
	return &copy
}

func cloneAccountMap(source map[string]*tammyv1.Account) map[string]*tammyv1.Account {
	result := make(map[string]*tammyv1.Account, len(source))
	for key, value := range source {
		copy := *value
		result[key] = &copy
	}
	return result
}

func mutateWithAccounts(name string, mutate func(*tammyv1.Journal), journal *tammyv1.Journal, accounts map[string]*tammyv1.Account) {
	if name == "protected manual account" {
		accounts[testAssetID].Designation = tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL
		return
	}
	if name == "archived account" {
		accounts[testAssetID].Status = tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED
		return
	}
	mutate(journal)
}
