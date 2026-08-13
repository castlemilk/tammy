package accounting

import (
	"context"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

type postingCommandRunner struct {
	repositories CommandRepositories
	operation    app.OrdinaryOperation
}

func (runner *postingCommandRunner) Execute(ctx context.Context, command app.OrdinaryCommand[CommandRepositories]) (proto.Message, error) {
	runner.operation = command.Operation
	if err := command.SaveSource(ctx, runner.repositories, command.Request); err != nil {
		return nil, err
	}
	result, err := command.BuildResult(ctx, runner.repositories, command.Request)
	return result.Result, err
}

type postingAccountStore struct{ accounts map[string]*tammyv1.Account }

func (store postingAccountStore) Create(context.Context, *tammyv1.Account, time.Time) error {
	return nil
}
func (store postingAccountStore) Update(context.Context, uint64, *tammyv1.Account, time.Time) error {
	return nil
}
func (store postingAccountStore) Get(_ context.Context, id string) (*tammyv1.Account, error) {
	account := store.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	return proto.Clone(account).(*tammyv1.Account), nil
}

type postingJournalStore struct {
	journal *tammyv1.Journal
	flows   map[string][]CashFlowComponent
}

func (store *postingJournalStore) ReserveFinancialRevision(context.Context, *uint64, time.Time) (uint64, error) {
	return 7, nil
}
func (store *postingJournalStore) Post(_ context.Context, journal *tammyv1.Journal, _, _ string, _ uint64,
	_ map[string]TaxFact, flows map[string][]CashFlowComponent, _ time.Time,
) error {
	store.journal = proto.Clone(journal).(*tammyv1.Journal)
	store.flows = flows
	return nil
}
func (store *postingJournalStore) Get(context.Context, string) (*tammyv1.Journal, error) {
	return proto.Clone(store.journal).(*tammyv1.Journal), nil
}
func (store *postingJournalStore) Reverse(context.Context, string, uint64, *tammyv1.CivilDate, string, string, []string, time.Time) (*tammyv1.Journal, *tammyv1.Journal, error) {
	return nil, nil, ErrJournalNotFound
}

type postingAuditFactory struct{}

func (postingAuditFactory) Build(_ context.Context, _ app.OrdinaryOperation, _ *tammyv1.AuthenticationContext,
	_, _ string, result proto.Message, _ proto.Message,
) (app.CommandResult, error) {
	return app.CommandResult{Result: result}, nil
}

func TestPostingServiceRoutesBalancedManualJournalThroughClosedCoordinator(t *testing.T) {
	organisationID := "018f0000-0000-7000-8000-000000000020"
	expenseID := "018f0000-0000-7000-8000-000000000501"
	bankID := "018f0000-0000-7000-8000-000000000502"
	accounts := postingAccountStore{accounts: map[string]*tammyv1.Account{
		expenseID: {Id: expenseID, OrganisationId: organisationID, Version: 1, Code: "5000", Name: "Office expenses",
			Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
			Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
			ReportClassification: "profit_loss.operating_expense", CashFlowClassification: "operating"},
		bankID: {Id: bankID, OrganisationId: organisationID, Version: 1, Code: "1000", Name: "Bank",
			Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
			Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
			ReportClassification: "balance_sheet.cash", CashFlowClassification: "operating"},
	}}
	journalStore := &postingJournalStore{}
	runner := &postingCommandRunner{repositories: CommandRepositories{Accounts: accounts, Journals: journalStore}}
	ids := []string{"018f0000-0000-7000-8000-000000000510", "018f0000-0000-7000-8000-000000000511", "018f0000-0000-7000-8000-000000000512"}
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	service, err := NewPostingService(runner, postingAuditFactory{}, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	if err != nil {
		t.Fatal(err)
	}
	request := &tammyv1.PostManualJournalRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "018f0000-0000-7000-8000-000000000520",
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: "018f0000-0000-7000-8000-000000000521", SessionId: "018f0000-0000-7000-8000-000000000522"}},
		OrganisationId: organisationID, PostingDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10}, Memo: "Office supplies",
		Lines: []*tammyv1.ManualJournalLineInput{
			{ClientLineId: "018f0000-0000-7000-8000-000000000523", AccountId: expenseID, Debit: aud(31900), Credit: aud(0), Description: "Expense"},
			{ClientLineId: "018f0000-0000-7000-8000-000000000524", AccountId: bankID, Debit: aud(0), Credit: aud(31900), Description: "Bank"},
		}}
	response, err := service.PostManualJournal(context.Background(), request)
	if err != nil || response.Journal == nil || response.Journal.FinancialRevision != 7 ||
		response.Journal.TotalDebits.MinorUnits != 31900 || runner.operation != app.OrdinaryOperationPostManualJournal {
		t.Fatalf("PostManualJournal() = %#v, %v; operation=%s", response, err, runner.operation)
	}
	if got := journalStore.flows[response.Journal.Lines[0].Id][0]; got.Category != CashFlowNoncash || got.AmountMinor != 31900 {
		t.Fatalf("expense flow = %#v", got)
	}
	if got := journalStore.flows[response.Journal.Lines[1].Id][0]; got.Category != CashFlowOperating || got.AmountMinor != -31900 {
		t.Fatalf("bank flow = %#v", got)
	}
}
