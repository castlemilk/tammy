package accounting

import (
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestBuildOpeningJournalBalancesLedgerNormalInputs(t *testing.T) {
	organisationID := "018f0000-0000-7000-8000-000000000020"
	assetID := "018f0000-0000-7000-8000-000000000701"
	equityID := "018f0000-0000-7000-8000-000000000702"
	accounts := map[string]*tammyv1.Account{
		assetID: openingAccount(assetID, "1000", tammyv1.AccountType_ACCOUNT_TYPE_ASSET,
			tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, "balance_sheet.cash"),
		equityID: openingAccount(equityID, "3999", tammyv1.AccountType_ACCOUNT_TYPE_EQUITY,
			tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT, tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_SYSTEM, "balance_sheet.opening_equity"),
	}
	inputs := []*tammyv1.OpeningBalanceInput{
		{ClientLineId: "018f0000-0000-7000-8000-000000000703", Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT,
			AccountId: assetID, LedgerBalance: aud(125000), LatestStatementDate: &tammyv1.CivilDate{Year: 2024, Month: 6, Day: 30}, LatestStatementBalance: aud(125000)},
		{ClientLineId: "018f0000-0000-7000-8000-000000000704", Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_OPENING_EQUITY,
			AccountId: equityID, LedgerBalance: aud(125000)},
	}
	journal, flows, err := BuildOpeningJournal(organisationID, &tammyv1.CivilDate{Year: 2024, Month: 7, Day: 1}, inputs,
		accounts, "018f0000-0000-7000-8000-000000000705",
		[]string{"018f0000-0000-7000-8000-000000000706", "018f0000-0000-7000-8000-000000000707"}, 1,
		time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if journal.TotalDebits.MinorUnits != 125000 || journal.TotalCredits.MinorUnits != 125000 ||
		journal.Lines[0].Debit.MinorUnits != 125000 || journal.Lines[1].Credit.MinorUnits != 125000 ||
		flows[journal.Lines[0].Id][0].Category != CashFlowNoncash {
		t.Fatalf("journal=%#v flows=%#v", journal, flows)
	}
}

func TestValidateOpeningInputsRejectsMismatchedGSTAndWrongControlAccount(t *testing.T) {
	organisationID := "018f0000-0000-7000-8000-000000000020"
	receivableID := "018f0000-0000-7000-8000-000000000711"
	accounts := map[string]*tammyv1.Account{
		receivableID: openingAccount(receivableID, "1100", tammyv1.AccountType_ACCOUNT_TYPE_ASSET,
			tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL,
			"balance_sheet.receivables"),
	}
	input := &tammyv1.OpeningBalanceInput{ClientLineId: "018f0000-0000-7000-8000-000000000712",
		Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_CUSTOMER_OPEN_ITEM, AccountId: receivableID,
		LedgerBalance: aud(11000), OutstandingGst: aud(1000), PriorGstAttributed: aud(1001),
		OriginalIssueDate: &tammyv1.CivilDate{Year: 2024, Month: 6, Day: 1}, OriginalDueDate: &tammyv1.CivilDate{Year: 2024, Month: 7, Day: 1}}
	if err := ValidateOpeningInputs(organisationID, []*tammyv1.OpeningBalanceInput{input}, accounts); err == nil {
		t.Fatal("GST over-attribution accepted")
	}
	input.PriorGstAttributed.MinorUnits = 1000
	accounts[receivableID].ReportClassification = "balance_sheet.cash"
	if err := ValidateOpeningInputs(organisationID, []*tammyv1.OpeningBalanceInput{input}, accounts); err == nil {
		t.Fatal("wrong control account accepted")
	}
}

func openingAccount(id, code string, accountType tammyv1.AccountType, normal tammyv1.NormalBalance,
	designation tammyv1.AccountDesignation, classification string,
) *tammyv1.Account {
	return &tammyv1.Account{Id: id, OrganisationId: "018f0000-0000-7000-8000-000000000020", Version: 1,
		Code: code, Name: code, Type: accountType, NormalBalance: normal,
		Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: designation,
		ReportClassification: classification, CashFlowClassification: "operating"}
}
