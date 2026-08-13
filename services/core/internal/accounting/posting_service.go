package accounting

import (
	"context"
	"errors"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrPostingService = errors.New("accounting: posting service failure")

type PostingService struct {
	commands CommandRunner
	audit    AuditFactory
	clock    func() time.Time
	newID    func() string
}

func NewPostingService(commands CommandRunner, audit AuditFactory, clock func() time.Time, newID func() string) (*PostingService, error) {
	if commands == nil || audit == nil || clock == nil || newID == nil {
		return nil, ErrPostingService
	}
	return &PostingService{commands: commands, audit: audit, clock: clock, newID: newID}, nil
}

func (service *PostingService) PostManualJournal(
	ctx context.Context,
	request *tammyv1.PostManualJournalRequest,
) (*tammyv1.PostManualJournalResponse, error) {
	if request == nil || !validCommandContext(request.CommandContext) || ValidateManualPostingIntent(PostingIntent{
		OrganisationID: request.OrganisationId, PostingDate: request.PostingDate, Memo: request.Memo, Lines: request.Lines,
	}) != nil {
		return nil, ErrPostingService
	}
	journalID := service.newID()
	lineIDs := make([]string, len(request.Lines))
	if !ids.IsCanonicalV7(journalID) {
		return nil, ErrPostingService
	}
	for index := range lineIDs {
		lineIDs[index] = service.newID()
		if !ids.IsCanonicalV7(lineIDs[index]) {
			return nil, ErrPostingService
		}
	}
	var posted *tammyv1.Journal
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationPostManualJournal,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext),
		Request: request, NewResult: func() proto.Message { return &tammyv1.PostManualJournalResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.PostManualJournalRequest)
			if !ok || repositories.Accounts == nil || repositories.Journals == nil {
				return ErrPostingService
			}
			revision, err := repositories.Journals.ReserveFinancialRevision(ctx, ownedRequest.ExpectedFinancialRevision, service.clock())
			if err != nil {
				return err
			}
			var taxFacts map[string]TaxFact
			var flows map[string][]CashFlowComponent
			posted, taxFacts, flows, err = service.buildManualJournal(ctx, repositories, ownedRequest, journalID, lineIDs, revision)
			if err != nil {
				return err
			}
			return repositories.Journals.Post(ctx, posted, "MANUAL", ownedRequest.CommandContext.IdempotencyKey, 1, taxFacts, flows, service.clock())
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.PostManualJournalRequest)
			if !ok || posted == nil {
				return app.CommandResult{}, ErrPostingService
			}
			result := &tammyv1.PostManualJournalResponse{Journal: proto.Clone(posted).(*tammyv1.Journal)}
			payload := &tammyv1.JournalPostedEvent{JournalId: posted.Id, Source: posted.Source,
				TotalDebits:  proto.Clone(posted.TotalDebits).(*tammyv1.Money),
				TotalCredits: proto.Clone(posted.TotalCredits).(*tammyv1.Money)}
			return service.audit.Build(ctx, app.OrdinaryOperationPostManualJournal, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, posted.Id, result, payload)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.PostManualJournalResponse)
	if !ok || response.Journal == nil {
		return nil, ErrPostingService
	}
	return response, nil
}

func (service *PostingService) ReverseJournal(
	ctx context.Context,
	request *tammyv1.ReverseJournalRequest,
) (*tammyv1.ReverseJournalResponse, error) {
	if request == nil || !validCommandContext(request.CommandContext) || !ids.IsCanonicalV7(request.JournalId) ||
		request.ExpectedVersion == 0 || !validCivilDate(request.ReversalDate) ||
		!canonicalText(request.Reason, 512) {
		return nil, ErrPostingService
	}
	var original, reversal *tammyv1.Journal
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationReverseJournal,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext),
		Request: request, NewResult: func() proto.Message { return &tammyv1.ReverseJournalResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.ReverseJournalRequest)
			if !ok || repositories.Journals == nil {
				return ErrPostingService
			}
			current, err := repositories.Journals.Get(ctx, ownedRequest.JournalId)
			if err != nil {
				return err
			}
			reversalID := service.newID()
			if !ids.IsCanonicalV7(reversalID) {
				return ErrPostingService
			}
			lineIDs := make([]string, len(current.Lines))
			for index := range lineIDs {
				lineIDs[index] = service.newID()
				if !ids.IsCanonicalV7(lineIDs[index]) {
					return ErrPostingService
				}
			}
			original, reversal, err = repositories.Journals.Reverse(ctx, ownedRequest.JournalId,
				ownedRequest.ExpectedVersion, ownedRequest.ReversalDate, ownedRequest.Reason,
				reversalID, lineIDs, service.clock())
			return err
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.ReverseJournalRequest)
			if !ok || original == nil || reversal == nil {
				return app.CommandResult{}, ErrPostingService
			}
			result := &tammyv1.ReverseJournalResponse{Original: proto.Clone(original).(*tammyv1.Journal),
				Reversal: proto.Clone(reversal).(*tammyv1.Journal)}
			payload := &tammyv1.JournalPostedEvent{JournalId: reversal.Id, Source: reversal.Source,
				TotalDebits:         proto.Clone(reversal.TotalDebits).(*tammyv1.Money),
				TotalCredits:        proto.Clone(reversal.TotalCredits).(*tammyv1.Money),
				ReversalOfJournalId: stringPointer(original.Id)}
			return service.audit.Build(ctx, app.OrdinaryOperationReverseJournal, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, reversal.Id, result, payload)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.ReverseJournalResponse)
	if !ok || response.Original == nil || response.Reversal == nil {
		return nil, ErrPostingService
	}
	return response, nil
}

func (service *PostingService) buildManualJournal(
	ctx context.Context,
	repositories CommandRepositories,
	request *tammyv1.PostManualJournalRequest,
	journalID string,
	lineIDs []string,
	revision uint64,
) (*tammyv1.Journal, map[string]TaxFact, map[string][]CashFlowComponent, error) {
	journal := &tammyv1.Journal{Id: journalID, OrganisationId: request.OrganisationId, Version: 1,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, Source: tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		PostingDate: proto.Clone(request.PostingDate).(*tammyv1.CivilDate), Memo: request.Memo,
		Lines: make([]*tammyv1.JournalLine, 0, len(request.Lines)), PostedAt: timestamppb.New(service.clock()),
		FinancialRevision: revision}
	accounts := make(map[string]*tammyv1.Account, len(request.Lines))
	flows := make(map[string][]CashFlowComponent, len(request.Lines))
	taxFacts := make(map[string]TaxFact)
	for index, input := range request.Lines {
		account, ok := accounts[input.AccountId]
		if !ok {
			var err error
			account, err = repositories.Accounts.Get(ctx, input.AccountId)
			if err != nil || ValidateManualPosting(account) != nil {
				return nil, nil, nil, ErrAccountNotPostable
			}
			accounts[input.AccountId] = account
		}
		line := &tammyv1.JournalLine{Id: lineIDs[index], JournalId: journalID, AccountId: input.AccountId,
			Sequence: uint32(index + 1), Debit: proto.Clone(input.Debit).(*tammyv1.Money),
			Credit: proto.Clone(input.Credit).(*tammyv1.Money), Description: input.Description}
		if input.TaxCodeId != nil {
			if repositories.TaxCodes == nil {
				return nil, nil, nil, ErrPostingService
			}
			code, err := repositories.TaxCodes.GetEffectiveTaxCode(ctx, request.OrganisationId,
				civilDateString(request.PostingDate), *input.TaxCodeId)
			if err != nil || code == nil || code.Rule == nil || code.Id != *input.TaxCodeId {
				return nil, nil, nil, ErrPostingPolicy
			}
			magnitude := input.Debit.MinorUnits + input.Credit.MinorUnits
			if input.TaxAmount.MinorUnits < -magnitude || input.TaxAmount.MinorUnits > magnitude {
				return nil, nil, nil, ErrPostingPolicy
			}
			line.TaxCodeId = cloneString(input.TaxCodeId)
			line.TaxAmount = proto.Clone(input.TaxAmount).(*tammyv1.Money)
			line.TaxRule = proto.Clone(code.Rule).(*tammyv1.SourceRef)
			fact, err := BuildNonCashTaxFact(line.Id+":tax", request.OrganisationId, line.Id, code,
				input.Debit.MinorUnits-input.Credit.MinorUnits, input.TaxAmount.MinorUnits, input.Source, input.ClientLineId)
			if err != nil {
				return nil, nil, nil, err
			}
			taxFacts[line.Id] = fact
		}
		journal.Lines = append(journal.Lines, line)
		amount := line.Debit.MinorUnits - line.Credit.MinorUnits
		category := CashFlowNoncash
		if IsCashAccount(account) {
			category = cashFlowCategory(account.CashFlowClassification)
		}
		flows[line.Id] = []CashFlowComponent{{Category: category, AmountMinor: amount}}
	}
	debits, credits, err := CheckedJournalTotals(journal.Lines)
	if err != nil {
		return nil, nil, nil, err
	}
	journal.TotalDebits, journal.TotalCredits = audMoney(debits), audMoney(credits)
	if err := ValidateJournal(journal, accounts, true); err != nil {
		return nil, nil, nil, err
	}
	return journal, taxFacts, flows, nil
}

func cashFlowCategory(value string) CashFlowCategory {
	switch value {
	case "operating":
		return CashFlowOperating
	case "investing":
		return CashFlowInvesting
	case "financing":
		return CashFlowFinancing
	case "transfer":
		return CashFlowTransfer
	default:
		return CashFlowUnspecified
	}
}
