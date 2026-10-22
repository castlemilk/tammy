//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/banking"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func TestOpeningConversionAndPeriodControlsUseOneEncryptedTransaction(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	organisationRepository, _ := organisations.NewRepository(tx)
	if err := organisationRepository.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	asset := postingRepositoryAccount("018f0000-0000-7000-8000-000000000901", "1000", "Opening bank", "balance_sheet.cash")
	equity := &tammyv1.Account{Id: "018f0000-0000-7000-8000-000000000902", OrganisationId: accountTestOrganisationID,
		Version: 1, Code: "3999", Name: "Opening equity", Type: tammyv1.AccountType_ACCOUNT_TYPE_EQUITY,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT,
		Status:        tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_SYSTEM,
		ReportClassification: "balance_sheet.opening_equity", CashFlowClassification: "financing"}
	accountRepository, _ := accounting.NewAccountRepository(tx)
	for _, account := range []*tammyv1.Account{asset, equity} {
		if err := accountRepository.Create(ctx, account, now); err != nil {
			t.Fatal(err)
		}
	}
	inputs := []*tammyv1.OpeningBalanceInput{
		{ClientLineId: "018f0000-0000-7000-8000-000000000903", Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT,
			AccountId: asset.Id, LedgerBalance: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 125000},
			LatestStatementDate: &tammyv1.CivilDate{Year: 2024, Month: 6, Day: 30}, LatestStatementBalance: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 125000}},
		{ClientLineId: "018f0000-0000-7000-8000-000000000904", Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_OPENING_EQUITY,
			AccountId: equity.Id, LedgerBalance: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 125000}},
	}
	journalRepository, _ := accounting.NewJournalRepository(tx)
	revision, err := journalRepository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	journal, flows, err := accounting.BuildOpeningJournal(accountTestOrganisationID,
		&tammyv1.CivilDate{Year: 2024, Month: 7, Day: 1}, inputs,
		map[string]*tammyv1.Account{asset.Id: asset, equity.Id: equity},
		"018f0000-0000-7000-8000-000000000905",
		[]string{"018f0000-0000-7000-8000-000000000906", "018f0000-0000-7000-8000-000000000907"}, revision, now)
	if err != nil {
		t.Fatal(err)
	}
	openingRepository, _ := accounting.NewOpeningConversionRepository(tx)
	conversion, err := openingRepository.Post(ctx, "018f0000-0000-7000-8000-000000000908", accountTestOrganisationID,
		journal.PostingDate, inputs, journal, flows, make([]byte, 32), now)
	if err != nil || conversion.JournalId != journal.Id {
		t.Fatalf("Post opening = %#v, %v", conversion, err)
	}
	bankingRepository, _ := banking.NewOpeningRepository(tx)
	if err := bankingRepository.RecordOpeningFinancialAccount(ctx, conversion.Id, inputs[0], now); err != nil {
		t.Fatal(err)
	}
	periodRepository, _ := accounting.NewPeriodRepository(tx)
	period, err := periodRepository.Close(ctx, accountTestOrganisationID,
		&tammyv1.CivilDate{Year: 2024, Month: 7, Day: 1}, revision,
		"018f0000-0000-7000-8000-000000000909", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	open, err := periodRepository.IsPostingDateOpen(ctx, accountTestOrganisationID, journal.PostingDate)
	if err != nil || open {
		t.Fatalf("closed period reports open=%t, %v", open, err)
	}
	if _, err := periodRepository.Reopen(ctx, period.Id, period.Version, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	open, err = periodRepository.IsPostingDateOpen(ctx, accountTestOrganisationID, journal.PostingDate)
	if err != nil || !open {
		t.Fatalf("reopened period reports open=%t, %v", open, err)
	}
}
