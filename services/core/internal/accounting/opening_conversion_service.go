package accounting

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var (
	ErrOpeningRepository = errors.New("accounting: opening repository failure")
	ErrOpeningConflict   = errors.New("accounting: opening conversion conflict")
	ErrOpeningService    = errors.New("accounting: opening conversion service failure")
)

type OpeningStore interface {
	Post(context.Context, string, string, *tammyv1.CivilDate, []*tammyv1.OpeningBalanceInput,
		*tammyv1.Journal, map[string][]CashFlowComponent, []byte, time.Time) (*tammyv1.OpeningConversion, error)
	Get(context.Context, string) (*tammyv1.OpeningConversion, error)
	MarkReplaced(context.Context, string, uint64, string) (*tammyv1.OpeningConversion, error)
}

type OpeningConversionRepository struct{ executor app.CommandSQLExecutor }

func NewOpeningConversionRepository(executor app.CommandSQLExecutor) (*OpeningConversionRepository, error) {
	if executor == nil {
		return nil, ErrOpeningRepository
	}
	return &OpeningConversionRepository{executor: executor}, nil
}

func (repository *OpeningConversionRepository) Post(ctx context.Context, conversionID, organisationID string,
	date *tammyv1.CivilDate, inputs []*tammyv1.OpeningBalanceInput, journal *tammyv1.Journal,
	flows map[string][]CashFlowComponent, sourceHash []byte, now time.Time,
) (*tammyv1.OpeningConversion, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(conversionID) ||
		!ids.IsCanonicalV7(organisationID) || !validCivilDate(date) || len(inputs) < 2 || len(inputs) != len(journal.GetLines()) ||
		len(sourceHash) != sha256.Size || now.IsZero() {
		return nil, ErrOpeningRepository
	}
	_, err := repository.executor.ExecContext(ctx, `
		INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256,
			journal_id, version, replaced_by_id, financial_revision, created_at)
		VALUES (?, ?, ?, 'DRAFT', ?, NULL, 1, NULL, NULL, ?)`, conversionID, organisationID,
		civilDateString(date), hex.EncodeToString(sourceHash), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, ErrOpeningConflict
	}
	accountRepository, _ := NewAccountRepository(repository.executor)
	for index, input := range inputs {
		account, err := accountRepository.Get(ctx, input.AccountId)
		if err != nil {
			return nil, err
		}
		debit, credit, ok := ledgerNormalSides(account.NormalBalance, input.LedgerBalance.MinorUnits)
		if !ok {
			return nil, ErrOpeningRepository
		}
		var sourceType, sourceID, sourceRevision, sourceContentHash any
		if input.Source != nil {
			sourceType, sourceID, sourceRevision, sourceContentHash = input.Source.Type, input.Source.Id, input.Source.Revision, input.Source.ContentHash
		}
		_, err = repository.executor.ExecContext(ctx, `
			INSERT INTO opening_items(id, conversion_id, account_id, item_kind, debit_minor, credit_minor,
				currency_code, source_type, source_id, source_revision, source_content_hash,
				original_issue_date, original_due_date, outstanding_gst_minor, prior_gst_attributed_minor,
				latest_statement_date, latest_statement_balance_minor)
			VALUES (?, ?, ?, ?, ?, ?, 'AUD', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ClientLineId, conversionID, input.AccountId, openingKindStorageName(input.Kind), debit, credit,
			sourceType, sourceID, sourceRevision, sourceContentHash, nullableCivilDate(input.OriginalIssueDate),
			nullableCivilDate(input.OriginalDueDate), nullableMoney(input.OutstandingGst), nullableMoney(input.PriorGstAttributed),
			nullableCivilDate(input.LatestStatementDate), nullableMoney(input.LatestStatementBalance))
		if err != nil {
			return nil, fmt.Errorf("%w: insert opening item: %v", ErrOpeningRepository, err)
		}
		_ = index
	}
	journalRepository, err := NewJournalRepository(repository.executor)
	if err != nil {
		return nil, err
	}
	if err := journalRepository.Post(ctx, journal, "OPENING", conversionID, 1, nil, flows, now); err != nil {
		return nil, err
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE opening_conversions SET state='POSTED', journal_id=?, financial_revision=?
		WHERE id=? AND state='DRAFT'`, journal.Id, journal.FinancialRevision, conversionID)
	if err != nil {
		return nil, fmt.Errorf("%w: post opening conversion: %v", ErrOpeningRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, ErrOpeningConflict
	}
	return &tammyv1.OpeningConversion{Id: conversionID, OrganisationId: organisationID, Version: 1,
		State: tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_POSTED, ConversionDate: cloneCivilDate(date),
		JournalId: journal.Id, FinancialRevision: journal.FinancialRevision}, nil
}

func (repository *OpeningConversionRepository) Get(ctx context.Context, conversionID string) (*tammyv1.OpeningConversion, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(conversionID) {
		return nil, ErrOpeningRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, organisation_id, version, state, conversion_date, journal_id, replaced_by_id, financial_revision
		FROM opening_conversions WHERE id=?`, conversionID)
	if err != nil {
		return nil, ErrOpeningRepository
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrOpeningConflict
	}
	var projection tammyv1.OpeningConversion
	var state, date string
	var journalID, replacedBy sql.NullString
	var revision sql.NullInt64
	if err := rows.Scan(&projection.Id, &projection.OrganisationId, &projection.Version, &state, &date,
		&journalID, &replacedBy, &revision); err != nil || rows.Next() || rows.Err() != nil {
		return nil, ErrOpeningRepository
	}
	projection.ConversionDate = parseCivilDate(date)
	if state == "POSTED" {
		projection.State = tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_POSTED
	} else if state == "REPLACED" {
		projection.State = tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_REPLACED
	}
	if journalID.Valid {
		projection.JournalId = journalID.String
	}
	if replacedBy.Valid {
		projection.ReplacedById = stringPointer(replacedBy.String)
	}
	if revision.Valid {
		projection.FinancialRevision = uint64(revision.Int64)
	}
	if projection.State == tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_UNSPECIFIED ||
		projection.ConversionDate == nil || projection.JournalId == "" || projection.FinancialRevision == 0 {
		return nil, ErrOpeningRepository
	}
	return &projection, nil
}

func (repository *OpeningConversionRepository) MarkReplaced(ctx context.Context, conversionID string,
	expectedVersion uint64, replacementID string,
) (*tammyv1.OpeningConversion, error) {
	if !ids.IsCanonicalV7(replacementID) || expectedVersion == 0 {
		return nil, ErrOpeningRepository
	}
	current, err := repository.Get(ctx, conversionID)
	if err != nil || current.Version != expectedVersion ||
		current.State != tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_POSTED {
		return nil, ErrOpeningConflict
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE opening_conversions SET state='REPLACED', version=version+1, replaced_by_id=?
		WHERE id=? AND version=? AND state='POSTED' AND replaced_by_id IS NULL`, replacementID, conversionID, expectedVersion)
	if err != nil {
		return nil, ErrOpeningRepository
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, ErrOpeningConflict
	}
	current.State = tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_REPLACED
	current.Version++
	current.ReplacedById = stringPointer(replacementID)
	return current, nil
}

type OpeningConversionService struct {
	commands CommandRunner
	audit    AuditFactory
	clock    func() time.Time
	newID    func() string
}

func NewOpeningConversionService(commands CommandRunner, audit AuditFactory, clock func() time.Time,
	newID func() string,
) (*OpeningConversionService, error) {
	if commands == nil || audit == nil || clock == nil || newID == nil {
		return nil, ErrOpeningService
	}
	return &OpeningConversionService{commands: commands, audit: audit, clock: clock, newID: newID}, nil
}

func (service *OpeningConversionService) PostOpeningConversion(ctx context.Context,
	request *tammyv1.PostOpeningConversionRequest,
) (*tammyv1.PostOpeningConversionResponse, error) {
	if request == nil || !validCommandContext(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) ||
		!validCivilDate(request.ConversionDate) || len(request.Balances) < 2 || len(request.Balances) > 5000 {
		return nil, ErrOpeningService
	}
	conversionID, journalID := service.newID(), service.newID()
	lineIDs := make([]string, len(request.Balances))
	if !ids.IsCanonicalV7(conversionID) || !ids.IsCanonicalV7(journalID) {
		return nil, ErrOpeningService
	}
	for index := range lineIDs {
		lineIDs[index] = service.newID()
		if !ids.IsCanonicalV7(lineIDs[index]) {
			return nil, ErrOpeningService
		}
	}
	var conversion *tammyv1.OpeningConversion
	var journal *tammyv1.Journal
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationPostOpeningConversion,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext), Request: request,
		NewResult: func() proto.Message { return &tammyv1.PostOpeningConversionResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.PostOpeningConversionRequest)
			if !ok || repositories.Accounts == nil || repositories.Journals == nil || repositories.Openings == nil {
				return ErrOpeningService
			}
			revision, err := repositories.Journals.ReserveFinancialRevision(ctx, ownedRequest.ExpectedFinancialRevision, service.clock())
			if err != nil {
				return err
			}
			accounts := make(map[string]*tammyv1.Account)
			for _, input := range ownedRequest.Balances {
				if _, exists := accounts[input.AccountId]; exists {
					continue
				}
				account, err := repositories.Accounts.Get(ctx, input.AccountId)
				if err != nil {
					return err
				}
				accounts[input.AccountId] = account
			}
			var flows map[string][]CashFlowComponent
			journal, flows, err = BuildOpeningJournal(ownedRequest.OrganisationId, ownedRequest.ConversionDate,
				ownedRequest.Balances, accounts, journalID, lineIDs, revision, service.clock())
			if err != nil {
				return err
			}
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(ownedRequest)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(encoded)
			conversion, err = repositories.Openings.Post(ctx, conversionID, ownedRequest.OrganisationId,
				ownedRequest.ConversionDate, ownedRequest.Balances, journal, flows, digest[:], service.clock())
			if err != nil {
				return err
			}
			for _, input := range ownedRequest.Balances {
				switch input.Kind {
				case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_CUSTOMER_OPEN_ITEM:
					if repositories.Sales == nil {
						return ErrOpeningService
					}
					if err := repositories.Sales.RecordOpeningReceivable(ctx, conversionID, input, service.clock()); err != nil {
						return err
					}
				case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM:
					if repositories.Purchases == nil {
						return ErrOpeningService
					}
					if err := repositories.Purchases.RecordOpeningPayable(ctx, conversionID, input, service.clock()); err != nil {
						return err
					}
				case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT:
					if repositories.Banking == nil {
						return ErrOpeningService
					}
					if err := repositories.Banking.RecordOpeningFinancialAccount(ctx, conversionID, input, service.clock()); err != nil {
						return err
					}
				case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_UNALLOCATED_CREDIT:
					if accounts[input.AccountId].ReportClassification == "balance_sheet.payables" {
						if repositories.Purchases == nil {
							return ErrOpeningService
						}
						if err := repositories.Purchases.RecordOpeningPayable(ctx, conversionID, input, service.clock()); err != nil {
							return err
						}
					} else {
						if repositories.Sales == nil {
							return ErrOpeningService
						}
						if err := repositories.Sales.RecordOpeningReceivable(ctx, conversionID, input, service.clock()); err != nil {
							return err
						}
					}
				}
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.PostOpeningConversionRequest)
			if !ok || conversion == nil || journal == nil {
				return app.CommandResult{}, ErrOpeningService
			}
			result := &tammyv1.PostOpeningConversionResponse{Conversion: proto.Clone(conversion).(*tammyv1.OpeningConversion),
				Journal: proto.Clone(journal).(*tammyv1.Journal)}
			payload := &tammyv1.OpeningConversionChangedEvent{ConversionId: conversion.Id,
				ToState: conversion.State, JournalId: journal.Id}
			return service.audit.Build(ctx, app.OrdinaryOperationPostOpeningConversion, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, conversion.Id, result, payload)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.PostOpeningConversionResponse)
	if !ok || response.Conversion == nil || response.Journal == nil {
		return nil, ErrOpeningService
	}
	return response, nil
}

func (service *OpeningConversionService) ReplaceOpeningConversion(ctx context.Context,
	request *tammyv1.ReplaceOpeningConversionRequest,
) (*tammyv1.ReplaceOpeningConversionResponse, error) {
	if request == nil || !validCommandContext(request.CommandContext) || request.CommandContext.FreshFactor == nil ||
		!ids.IsCanonicalV7(request.OpeningConversionId) || request.ExpectedVersion == 0 ||
		!validCivilDate(request.ReplacementDate) || len(request.Balances) < 2 || len(request.Balances) > 5000 ||
		!canonicalText(request.Reason, 512) {
		return nil, ErrOpeningService
	}
	replacementID, replacementJournalID := service.newID(), service.newID()
	replacementLineIDs := make([]string, len(request.Balances))
	if !ids.IsCanonicalV7(replacementID) || !ids.IsCanonicalV7(replacementJournalID) {
		return nil, ErrOpeningService
	}
	for index := range replacementLineIDs {
		replacementLineIDs[index] = service.newID()
		if !ids.IsCanonicalV7(replacementLineIDs[index]) {
			return nil, ErrOpeningService
		}
	}
	var predecessor, replacement *tammyv1.OpeningConversion
	var reversalJournal, replacementJournal *tammyv1.Journal
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationReplaceOpeningConversion,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext), Request: request,
		NewResult: func() proto.Message { return &tammyv1.ReplaceOpeningConversionResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.ReplaceOpeningConversionRequest)
			if !ok || repositories.Accounts == nil || repositories.Journals == nil || repositories.Openings == nil ||
				repositories.Factors == nil || repositories.OpeningImpact == nil {
				return ErrOpeningService
			}
			if err := repositories.Factors.Consume(ctx, ownedRequest.CommandContext.FreshFactor, "replace_opening_conversion"); err != nil {
				return err
			}
			if err := repositories.OpeningImpact.RequireOpeningReplacementAllowed(ctx, ownedRequest.OpeningConversionId, ownedRequest.Reason); err != nil {
				return err
			}
			current, err := repositories.Openings.Get(ctx, ownedRequest.OpeningConversionId)
			if err != nil || current.Version != ownedRequest.ExpectedVersion ||
				current.State != tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_POSTED {
				return ErrOpeningConflict
			}
			originalJournal, err := repositories.Journals.Get(ctx, current.JournalId)
			if err != nil {
				return err
			}
			reversalID := service.newID()
			reversalLineIDs := make([]string, len(originalJournal.Lines))
			if !ids.IsCanonicalV7(reversalID) {
				return ErrOpeningService
			}
			for index := range reversalLineIDs {
				reversalLineIDs[index] = service.newID()
				if !ids.IsCanonicalV7(reversalLineIDs[index]) {
					return ErrOpeningService
				}
			}
			_, reversalJournal, err = repositories.Journals.Reverse(ctx, originalJournal.Id, originalJournal.Version,
				ownedRequest.ReplacementDate, ownedRequest.Reason, reversalID, reversalLineIDs, service.clock())
			if err != nil {
				return err
			}
			revision, err := repositories.Journals.ReserveFinancialRevision(ctx, nil, service.clock())
			if err != nil {
				return err
			}
			accounts := make(map[string]*tammyv1.Account)
			for _, input := range ownedRequest.Balances {
				if _, exists := accounts[input.AccountId]; exists {
					continue
				}
				account, err := repositories.Accounts.Get(ctx, input.AccountId)
				if err != nil {
					return err
				}
				accounts[input.AccountId] = account
			}
			var flows map[string][]CashFlowComponent
			replacementJournal, flows, err = BuildOpeningJournal(current.OrganisationId, ownedRequest.ReplacementDate,
				ownedRequest.Balances, accounts, replacementJournalID, replacementLineIDs, revision, service.clock())
			if err != nil {
				return err
			}
			predecessor, err = repositories.Openings.MarkReplaced(ctx, current.Id, current.Version, replacementID)
			if err != nil {
				return err
			}
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(ownedRequest)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(encoded)
			replacement, err = repositories.Openings.Post(ctx, replacementID, current.OrganisationId,
				ownedRequest.ReplacementDate, ownedRequest.Balances, replacementJournal, flows, digest[:], service.clock())
			if err != nil {
				return err
			}
			return service.recordModuleOpenings(ctx, repositories, accounts, replacementID, ownedRequest.Balances)
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.ReplaceOpeningConversionRequest)
			if !ok || predecessor == nil || replacement == nil || reversalJournal == nil || replacementJournal == nil {
				return app.CommandResult{}, ErrOpeningService
			}
			result := &tammyv1.ReplaceOpeningConversionResponse{Predecessor: proto.Clone(predecessor).(*tammyv1.OpeningConversion),
				Replacement:        proto.Clone(replacement).(*tammyv1.OpeningConversion),
				ReversalJournal:    proto.Clone(reversalJournal).(*tammyv1.Journal),
				ReplacementJournal: proto.Clone(replacementJournal).(*tammyv1.Journal)}
			from := tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_POSTED
			payload := &tammyv1.OpeningConversionChangedEvent{ConversionId: predecessor.Id, FromState: &from,
				ToState: predecessor.State, JournalId: replacementJournal.Id}
			return service.audit.Build(ctx, app.OrdinaryOperationReplaceOpeningConversion, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, replacement.Id, result, payload)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.ReplaceOpeningConversionResponse)
	if !ok || response.Predecessor == nil || response.Replacement == nil || response.ReversalJournal == nil || response.ReplacementJournal == nil {
		return nil, ErrOpeningService
	}
	return response, nil
}

func (service *OpeningConversionService) recordModuleOpenings(ctx context.Context, repositories CommandRepositories,
	accounts map[string]*tammyv1.Account, conversionID string, inputs []*tammyv1.OpeningBalanceInput,
) error {
	for _, input := range inputs {
		switch input.Kind {
		case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_CUSTOMER_OPEN_ITEM:
			if repositories.Sales == nil {
				return ErrOpeningService
			}
			if err := repositories.Sales.RecordOpeningReceivable(ctx, conversionID, input, service.clock()); err != nil {
				return err
			}
		case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM:
			if repositories.Purchases == nil {
				return ErrOpeningService
			}
			if err := repositories.Purchases.RecordOpeningPayable(ctx, conversionID, input, service.clock()); err != nil {
				return err
			}
		case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT:
			if repositories.Banking == nil {
				return ErrOpeningService
			}
			if err := repositories.Banking.RecordOpeningFinancialAccount(ctx, conversionID, input, service.clock()); err != nil {
				return err
			}
		case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_UNALLOCATED_CREDIT:
			if accounts[input.AccountId].ReportClassification == "balance_sheet.payables" {
				if repositories.Purchases == nil {
					return ErrOpeningService
				}
				if err := repositories.Purchases.RecordOpeningPayable(ctx, conversionID, input, service.clock()); err != nil {
					return err
				}
			} else {
				if repositories.Sales == nil {
					return ErrOpeningService
				}
				if err := repositories.Sales.RecordOpeningReceivable(ctx, conversionID, input, service.clock()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func openingKindStorageName(value tammyv1.OpeningBalanceKind) string {
	return map[tammyv1.OpeningBalanceKind]string{
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_ORDINARY:           "ORDINARY",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_CUSTOMER_OPEN_ITEM: "CUSTOMER_OPEN_ITEM",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM: "SUPPLIER_OPEN_ITEM",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT:  "FINANCIAL_ACCOUNT",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_UNALLOCATED_CREDIT: "UNALLOCATED_CREDIT",
		tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_OPENING_EQUITY:     "OPENING_EQUITY",
	}[value]
}

func nullableCivilDate(value *tammyv1.CivilDate) any {
	if value == nil {
		return nil
	}
	return civilDateString(value)
}

func nullableMoney(value *tammyv1.Money) any {
	if value == nil {
		return nil
	}
	return value.MinorUnits
}
