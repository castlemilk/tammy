//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestJournalRepositoryPostsBalancedJournalAndProjectsTrialBalance(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	organisationRepository, _ := organisations.NewRepository(tx)
	if err := organisationRepository.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	accounts := []*tammyv1.Account{
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000401", "1001", "Operating bank", "balance_sheet.cash"),
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000402", "5001", "Office expenses", "profit_loss.operating_expense"),
	}
	accountRepository, _ := accounting.NewAccountRepository(tx)
	for _, account := range accounts {
		if err := accountRepository.Create(ctx, account, now); err != nil {
			t.Fatal(err)
		}
	}
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := repository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil || revision != 1 {
		t.Fatalf("ReserveFinancialRevision() = %d, %v", revision, err)
	}
	journal := repositoryJournal(accounts, revision, now)
	flows := map[string][]accounting.CashFlowComponent{
		journal.Lines[0].Id: {{Category: accounting.CashFlowOperating, AmountMinor: 31900}},
		journal.Lines[1].Id: {{Category: accounting.CashFlowNoncash, AmountMinor: -31900}},
	}
	if err := repository.Post(ctx, journal, "MANUAL", journal.Id, 1, nil, flows, now); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, journal.Id)
	if err != nil || stored.TotalDebits.MinorUnits != 31900 || stored.TotalCredits.MinorUnits != 31900 {
		t.Fatalf("Get() = %#v, %v", stored, err)
	}
	trial, debits, credits, projectedRevision, err := repository.TrialBalance(ctx, accountTestOrganisationID, "2026-08-10")
	if err != nil || len(trial) != 2 || debits != 31900 || credits != 31900 || projectedRevision != 1 {
		t.Fatalf("TrialBalance() = %d lines %d/%d rev%d, %v", len(trial), debits, credits, projectedRevision, err)
	}
	ledger, err := repository.GeneralLedger(ctx, accountTestOrganisationID, nil, "2026-08-01", "2026-08-31", 200)
	if err != nil || len(ledger) != 2 || ledger[0].JournalLineId != journal.Lines[0].Id ||
		ledger[1].JournalLineId != journal.Lines[1].Id {
		t.Fatalf("GeneralLedger() = %#v, %v", ledger, err)
	}
	if err := repository.Post(ctx, journal, "MANUAL", journal.Id, 1, nil, flows, now); !errors.Is(err, accounting.ErrJournalSourceConflict) {
		t.Fatalf("duplicate source error = %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE journal_lines SET debit_minor = 1 WHERE id = ?`, journal.Lines[0].Id); err == nil {
		t.Fatal("immutable journal line update succeeded")
	}
}

func postingRepositoryAccount(id, code, name, classification string) *tammyv1.Account {
	return &tammyv1.Account{Id: id, OrganisationId: accountTestOrganisationID, Version: 1, Code: code, Name: name,
		Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
		Status:               tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation:          tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: classification, CashFlowClassification: "operating"}
}

func repositoryJournal(accounts []*tammyv1.Account, revision uint64, now time.Time) *tammyv1.Journal {
	return &tammyv1.Journal{Id: "018f0000-0000-7000-8000-000000000410", OrganisationId: accountTestOrganisationID,
		Version: 1, State: tammyv1.JournalState_JOURNAL_STATE_POSTED,
		Source:      tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		PostingDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10}, Memo: "Office supplies",
		Lines: []*tammyv1.JournalLine{
			{Id: "018f0000-0000-7000-8000-000000000411", JournalId: "018f0000-0000-7000-8000-000000000410", AccountId: accounts[0].Id, Sequence: 1, Debit: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 31900}, Credit: &tammyv1.Money{CurrencyCode: "AUD"}, Description: "Bank"},
			{Id: "018f0000-0000-7000-8000-000000000412", JournalId: "018f0000-0000-7000-8000-000000000410", AccountId: accounts[1].Id, Sequence: 2, Debit: &tammyv1.Money{CurrencyCode: "AUD"}, Credit: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 31900}, Description: "Expense"},
		}, TotalDebits: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 31900},
		TotalCredits: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 31900},
		PostedAt:     timestamppb.New(now), FinancialRevision: revision}
}
