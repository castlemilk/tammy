package accounting

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestVerifyAccountingInvariantsCorrelatesJournalAndCashFlow(t *testing.T) {
	journal := balancedJournal()
	accounts := map[string]*tammyv1.Account{
		testAssetID:   postingAccount(testAssetID, tammyv1.AccountType_ACCOUNT_TYPE_ASSET),
		testExpenseID: postingAccount(testExpenseID, tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE),
	}
	snapshot := InvariantSnapshot{Accounts: accounts, Journals: []*tammyv1.Journal{journal},
		TaxFacts: map[string]TaxFact{}, CashFlows: map[string][]CashFlowComponent{
			testLineOneID: {{Category: CashFlowNoncash, AmountMinor: 31900}},
			testLineTwoID: {{Category: CashFlowNoncash, AmountMinor: -31900}},
		}}
	if err := VerifyAccountingInvariants(snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.CashFlows[testLineOneID][0].AmountMinor--
	if err := VerifyAccountingInvariants(snapshot); err == nil {
		t.Fatal("cash-flow mismatch accepted")
	}
}
