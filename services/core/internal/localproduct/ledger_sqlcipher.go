//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Package localproduct composes local business modules over one active
// encrypted workspace without exposing database capabilities to transport.
package localproduct

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	"github.com/tammyapp/tammy/services/core/internal/banking"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/reporting"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrLedgerModule = errors.New("local product: ledger module unavailable")

type LedgerModule struct {
	organisationRoute *organisationRoute
	accountingRoute   *accountingRoute
	bankingRoute      *bankingRoute
	documentRoute     *documentRoute
	taxRoute          *taxRoute
	mu                sync.RWMutex
	db                *sqlcipher.Database
}

func NewLedgerModule() (*LedgerModule, error) {
	return &LedgerModule{
		organisationRoute: &organisationRoute{},
		accountingRoute:   &accountingRoute{},
		bankingRoute:      &bankingRoute{},
		documentRoute:     &documentRoute{},
		taxRoute:          &taxRoute{},
	}, nil
}

func (module *LedgerModule) HandlerFactories() []transport.GeneratedHandlerFactory {
	if module == nil || module.organisationRoute == nil || module.accountingRoute == nil || module.bankingRoute == nil ||
		module.documentRoute == nil || module.taxRoute == nil {
		return nil
	}
	return []transport.GeneratedHandlerFactory{
		module.organisationRoute.factory,
		module.accountingRoute.factory,
		module.bankingRoute.factory,
		module.documentRoute.factory,
		module.taxRoute.factory,
	}
}

func (module *LedgerModule) Activate(activation app.LocalWorkspaceActivation) error {
	if module == nil || module.organisationRoute == nil || module.accountingRoute == nil || module.bankingRoute == nil ||
		module.documentRoute == nil || module.taxRoute == nil ||
		activation.Database == nil || activation.Identity == nil ||
		activation.Now == nil || activation.NewID == nil {
		return ErrLedgerModule
	}
	observer, err := idempotency.NewObserver(activation.Database)
	if err != nil {
		return ErrLedgerModule
	}
	elector, err := idempotency.NewElector(idempotency.Config{
		Clock: clock.Func(activation.Now), Observe: observer,
	})
	if err != nil {
		return ErrLedgerModule
	}
	starter := &organisationTransactionStarter{activation: activation}
	transactions, err := app.NewCommandTransactions[organisations.CommandRepositories](activation.WorkspaceID, starter)
	if err != nil {
		return ErrLedgerModule
	}
	coordinator, err := app.NewCoordinator(app.CoordinatorConfig[organisations.CommandRepositories]{
		Transactions: transactions,
		Authorizer:   organisationAuthorizer{workspaceID: activation.WorkspaceID},
		Elector:      elector,
		Auditor:      localCommandAuditor{},
	})
	if err != nil {
		return ErrLedgerModule
	}
	accountingTransactions, err := app.NewCommandTransactions[accounting.CommandRepositories](
		activation.WorkspaceID,
		&accountingTransactionStarter{activation: activation},
	)
	if err != nil {
		return ErrLedgerModule
	}
	accountingCoordinator, err := app.NewCoordinator(app.CoordinatorConfig[accounting.CommandRepositories]{
		Transactions: accountingTransactions,
		Authorizer:   accountingAuthorizer{workspaceID: activation.WorkspaceID},
		Elector:      elector,
		Auditor:      localCommandAuditor{},
	})
	if err != nil {
		return ErrLedgerModule
	}
	accountService, err := accounting.NewService(
		accountingCoordinator,
		localAccountingAuditFactory{workspaceID: activation.WorkspaceID},
		activation.Now,
		func() string {
			identifier, idErr := activation.NewID()
			if idErr != nil {
				return ""
			}
			return identifier
		},
	)
	if err != nil {
		return ErrLedgerModule
	}
	postingService, err := accounting.NewPostingService(
		accountingCoordinator,
		localAccountingAuditFactory{workspaceID: activation.WorkspaceID},
		activation.Now,
		func() string {
			identifier, idErr := activation.NewID()
			if idErr != nil {
				return ""
			}
			return identifier
		},
	)
	if err != nil {
		return ErrLedgerModule
	}
	service, err := organisations.NewService(organisations.ServiceConfig{
		Commands: coordinator,
		Audit:    localAuditFactory{workspaceID: activation.WorkspaceID},
		Clock:    activation.Now,
		NewID: func() string {
			identifier, idErr := activation.NewID()
			if idErr != nil {
				return ""
			}
			return identifier
		},
	})
	if err != nil {
		return ErrLedgerModule
	}
	handler := &organisationHandler{service: service, database: activation.Database, identity: activation.Identity}
	if err := module.organisationRoute.set(handler); err != nil {
		return err
	}
	if err := module.accountingRoute.set(&accountingHandler{
		accounts: accountService,
		database: activation.Database,
		identity: activation.Identity,
		posting:  postingService,
	}); err != nil {
		return err
	}
	if err := module.documentRoute.set(&documentHandler{
		database: activation.Database,
		identity: activation.Identity,
		now:      activation.Now,
		newID:    activation.NewID,
	}); err != nil {
		return err
	}
	if err := module.bankingRoute.set(&bankingHandler{
		database: activation.Database,
		identity: activation.Identity,
		now:      activation.Now,
		newID:    activation.NewID,
	}); err != nil {
		return err
	}
	if err := module.taxRoute.set(&taxHandler{
		database: activation.Database,
		identity: activation.Identity,
		now:      activation.Now,
		newID:    activation.NewID,
	}); err != nil {
		return err
	}
	module.mu.Lock()
	module.db = activation.Database
	module.mu.Unlock()
	return nil
}

type bankingRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *bankingRoute) set(service tammyv1connect.BankingServiceHandler) error {
	if route == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewBankingServiceHandler(service, append([]connect.HandlerOption(nil), route.options...)...)
	route.mu.Lock()
	route.handler = handler
	route.mu.Unlock()
	return nil
}

func (route *bankingRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.BankingServiceName + "/", route
}

func (route *bankingRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local banking unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

type bankingHandler struct {
	tammyv1connect.UnimplementedBankingServiceHandler
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
	now      func() time.Time
	newID    func() (string, error)
}

func (handler *bankingHandler) ImportBankStatement(ctx context.Context, request *connect.Request[tammyv1.ImportBankStatementRequest]) (*connect.Response[tammyv1.ImportBankStatementResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || handler.now == nil || handler.newID == nil ||
		request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		request.Msg.CommandContext.Authentication == nil || request.Msg.OpeningBalance == nil || len(request.Msg.Lines) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	if err := handler.identity.RequireAdministratorWithin(ctx, tx, request.Msg.CommandContext.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	importID, err := handler.newID()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	lineIDs := make([]string, len(request.Msg.Lines))
	for index := range lineIDs {
		lineIDs[index], err = handler.newID()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
		}
	}
	repository, err := banking.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	instant := handler.now()
	statementImport, err := repository.ImportStatement(ctx, request.Msg.CommandContext.IdempotencyKey,
		request.Msg.OrganisationId, importID, lineIDs, request.Msg.OpeningBalance.MinorUnits, request.Msg.Lines, instant)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if statementImport.Id == importID {
		if err := bumpBankingRevision(ctx, tx, instant); err != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.ImportBankStatementResponse{StatementImport: statementImport}), nil
}

func (handler *bankingHandler) ListBankStatementLines(ctx context.Context, request *connect.Request[tammyv1.ListBankStatementLinesRequest]) (*connect.Response[tammyv1.ListBankStatementLinesResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil || request.Msg == nil ||
		request.Msg.Authentication == nil || request.Msg.Page == nil || request.Msg.Page.Cursor != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	limit := int(request.Msg.Page.PageSize)
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := banking.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	lines, err := repository.ListLines(ctx, request.Msg.OrganisationId, limit)
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.ListBankStatementLinesResponse{Lines: lines, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(lines))}}), nil
}

func (handler *bankingHandler) MatchBankStatementLine(ctx context.Context, request *connect.Request[tammyv1.MatchBankStatementLineRequest]) (*connect.Response[tammyv1.MatchBankStatementLineResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || handler.now == nil || request == nil || request.Msg == nil ||
		request.Msg.CommandContext == nil || request.Msg.CommandContext.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	if err := handler.identity.RequireAdministratorWithin(ctx, tx, request.Msg.CommandContext.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	repository, _ := banking.NewRepository(tx)
	line, err := repository.MatchLine(ctx, request.Msg.LineId, request.Msg.ExpectedVersion, request.Msg.MatchReference)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	if err := bumpBankingRevision(ctx, tx, handler.now()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.MatchBankStatementLineResponse{Line: line}), nil
}

func (handler *bankingHandler) CompleteBankReconciliation(ctx context.Context, request *connect.Request[tammyv1.CompleteBankReconciliationRequest]) (*connect.Response[tammyv1.CompleteBankReconciliationResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || handler.now == nil || handler.newID == nil ||
		request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.CommandContext.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	if err := handler.identity.RequireAdministratorWithin(ctx, tx, request.Msg.CommandContext.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	id, err := handler.newID()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	repository, _ := banking.NewRepository(tx)
	instant := handler.now()
	count, closing, err := repository.CompleteReconciliation(ctx, request.Msg.CommandContext.IdempotencyKey, id, request.Msg.OrganisationId, instant)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	if err := bumpBankingRevision(ctx, tx, instant); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.CompleteBankReconciliationResponse{ReconciledLineCount: count, ClosingBalance: localAUD(closing)}), nil
}

func (handler *bankingHandler) GetBankingSummary(ctx context.Context, request *connect.Request[tammyv1.GetBankingSummaryRequest]) (*connect.Response[tammyv1.GetBankingSummaryResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil || request.Msg == nil || request.Msg.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, _ := banking.NewRepository(tx)
	total, unmatched, unreconciled, closing, err := repository.Summary(ctx, request.Msg.OrganisationId)
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetBankingSummaryResponse{ImportedLineCount: total, UnmatchedLineCount: unmatched, UnreconciledLineCount: unreconciled, LatestClosingBalance: localAUD(closing)}), nil
}

type taxRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *taxRoute) set(service tammyv1connect.TaxServiceHandler) error {
	if route == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewTaxServiceHandler(service, append([]connect.HandlerOption(nil), route.options...)...)
	route.mu.Lock()
	route.handler = handler
	route.mu.Unlock()
	return nil
}

func (route *taxRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.TaxServiceName + "/", route
}

func (route *taxRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local BAS unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

type taxHandler struct {
	tammyv1connect.UnimplementedTaxServiceHandler
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
	now      func() time.Time
	newID    func() (string, error)
}

func (handler *taxHandler) CreateBasDraft(ctx context.Context, request *connect.Request[tammyv1.CreateBasDraftRequest]) (*connect.Response[tammyv1.CreateBasDraftResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || handler.now == nil || handler.newID == nil ||
		request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.CommandContext.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	if err := handler.identity.RequireAdministratorWithin(ctx, tx, request.Msg.CommandContext.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	id, err := handler.newID()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	repository, _ := reporting.NewRepository(tx)
	instant := handler.now()
	workpaper, err := repository.CreateBASDraft(ctx, request.Msg.CommandContext.IdempotencyKey, id, request.Msg.OrganisationId, request.Msg.PeriodStart, request.Msg.PeriodEnd, instant)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	if workpaper.Id == id {
		if err := bumpTaxSourceRevision(ctx, tx, instant); err != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.CreateBasDraftResponse{Workpaper: workpaper}), nil
}

func (handler *taxHandler) GetCurrentBasDraft(ctx context.Context, request *connect.Request[tammyv1.GetCurrentBasDraftRequest]) (*connect.Response[tammyv1.GetCurrentBasDraftResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil || request.Msg == nil || request.Msg.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, _ := reporting.NewRepository(tx)
	workpaper, err := repository.GetCurrentBASDraft(ctx, request.Msg.OrganisationId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrLedgerModule)
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetCurrentBasDraftResponse{Workpaper: workpaper}), nil
}

func localAUD(minor int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor}
}

func bumpBankingRevision(ctx context.Context, executor app.CommandSQLExecutor, instant time.Time) error {
	_, err := executor.ExecContext(ctx, `
		UPDATE financial_revisions
		SET financial_revision = financial_revision + 1,
		    banking_revision = banking_revision + 1,
		    updated_at = ?
		WHERE id = 1`, instant.UTC().Format(time.RFC3339Nano))
	return err
}

func bumpTaxSourceRevision(ctx context.Context, executor app.CommandSQLExecutor, instant time.Time) error {
	_, err := executor.ExecContext(ctx, `
		UPDATE financial_revisions
		SET financial_revision = financial_revision + 1,
		    tax_source_revision = tax_source_revision + 1,
		    updated_at = ?
		WHERE id = 1`, instant.UTC().Format(time.RFC3339Nano))
	return err
}

type documentRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *documentRoute) set(service tammyv1connect.DocumentServiceHandler) error {
	if route == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewDocumentServiceHandler(
		service,
		append([]connect.HandlerOption(nil), route.options...)...,
	)
	route.mu.Lock()
	route.handler = handler
	route.mu.Unlock()
	return nil
}

func (route *documentRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.DocumentServiceName + "/", route
}

func (route *documentRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local documents unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

type documentHandler struct {
	tammyv1connect.UnimplementedDocumentServiceHandler
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
	now      func() time.Time
	newID    func() (string, error)
}

func (handler *documentHandler) IngestDocument(
	ctx context.Context,
	request *connect.Request[tammyv1.IngestDocumentRequest],
) (*connect.Response[tammyv1.IngestDocumentResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || handler.now == nil || handler.newID == nil ||
		request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		request.Msg.CommandContext.Authentication == nil || request.Msg.Candidate == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	authentication := request.Msg.CommandContext.Authentication
	if err := handler.identity.RequireAdministratorWithin(ctx, tx, authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	documentID, err := handler.newID()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	digest := sha256.Sum256(request.Msg.Original)
	instant := handler.now()
	document := &tammyv1.Document{
		Id:                documentID,
		OrganisationId:    request.Msg.OrganisationId,
		Version:           1,
		Status:            tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW,
		SourceDisplayName: request.Msg.SourceDisplayName,
		MimeType:          request.Msg.MimeType,
		ByteLength:        uint64(len(request.Msg.Original)),
		Sha256:            append([]byte(nil), digest[:]...),
		ExtractedText:     request.Msg.ExtractedText,
		Candidate:         proto.Clone(request.Msg.Candidate).(*tammyv1.DocumentCandidate),
		CreatedAt:         timestamppb.New(instant),
	}
	repository, err := documents.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	retained, err := repository.Create(
		ctx,
		request.Msg.CommandContext.IdempotencyKey,
		authentication.ActorUserId,
		document,
		request.Msg.Original,
	)
	if err != nil {
		code := connect.CodeInvalidArgument
		if errors.Is(err, documents.ErrDuplicateSource) {
			code = connect.CodeAlreadyExists
		}
		return nil, connect.NewError(code, ErrLedgerModule)
	}
	if retained.Id == documentID {
		if err := bumpTaxSourceRevision(ctx, tx, instant); err != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.IngestDocumentResponse{Document: retained}), nil
}

func (handler *documentHandler) GetDocument(
	ctx context.Context,
	request *connect.Request[tammyv1.GetDocumentRequest],
) (*connect.Response[tammyv1.GetDocumentResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil ||
		request.Msg == nil || request.Msg.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := documents.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	document, err := repository.Get(ctx, request.Msg.DocumentId)
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetDocumentResponse{Document: document}), nil
}

func (handler *documentHandler) ListDocuments(
	ctx context.Context,
	request *connect.Request[tammyv1.ListDocumentsRequest],
) (*connect.Response[tammyv1.ListDocumentsResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil ||
		request.Msg == nil || request.Msg.Authentication == nil || request.Msg.Page == nil || request.Msg.Page.Cursor != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	pageSize := int(request.Msg.Page.PageSize)
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 200 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := documents.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	items, err := repository.List(ctx, request.Msg.OrganisationId, pageSize)
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.ListDocumentsResponse{
		Documents: items,
		Page:      &tammyv1.PageInfo{ReturnedCount: uint32(len(items))},
	}), nil
}

func (handler *documentHandler) SaveDocumentReview(
	ctx context.Context,
	request *connect.Request[tammyv1.SaveDocumentReviewRequest],
) (*connect.Response[tammyv1.SaveDocumentReviewResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || handler.now == nil ||
		request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		request.Msg.CommandContext.Authentication == nil || request.Msg.Candidate == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	if err := handler.identity.RequireAdministratorWithin(ctx, tx, request.Msg.CommandContext.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	repository, err := documents.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	instant := handler.now()
	document, err := repository.SaveReview(
		ctx,
		request.Msg.DocumentId,
		request.Msg.ExpectedVersion,
		request.Msg.Candidate,
		instant,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	if err := bumpTaxSourceRevision(ctx, tx, instant); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.SaveDocumentReviewResponse{Document: document}), nil
}

type accountingRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *accountingRoute) set(service tammyv1connect.AccountingServiceHandler) error {
	if route == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewAccountingServiceHandler(
		service,
		append([]connect.HandlerOption(nil), route.options...)...,
	)
	route.mu.Lock()
	route.handler = handler
	route.mu.Unlock()
	return nil
}

func (route *accountingRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.AccountingServiceName + "/", route
}

func (route *accountingRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local accounting unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

type accountingHandler struct {
	tammyv1connect.UnimplementedAccountingServiceHandler
	accounts *accounting.Service
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
	posting  *accounting.PostingService
}

func (handler *accountingHandler) CreateAccount(
	ctx context.Context,
	request *connect.Request[tammyv1.CreateAccountRequest],
) (*connect.Response[tammyv1.CreateAccountResponse], error) {
	if handler == nil || handler.accounts == nil || request == nil || request.Msg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	response, err := handler.accounts.CreateAccount(ctx, request.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	return connect.NewResponse(response), nil
}

func (handler *accountingHandler) UpdateAccount(
	ctx context.Context,
	request *connect.Request[tammyv1.UpdateAccountRequest],
) (*connect.Response[tammyv1.UpdateAccountResponse], error) {
	if handler == nil || handler.accounts == nil || request == nil || request.Msg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	response, err := handler.accounts.UpdateAccount(ctx, request.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	return connect.NewResponse(response), nil
}

func (handler *accountingHandler) SetAccountStatus(
	ctx context.Context,
	request *connect.Request[tammyv1.SetAccountStatusRequest],
) (*connect.Response[tammyv1.SetAccountStatusResponse], error) {
	if handler == nil || handler.accounts == nil || request == nil || request.Msg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	response, err := handler.accounts.SetAccountStatus(ctx, request.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	return connect.NewResponse(response), nil
}

func (handler *accountingHandler) PostManualJournal(
	ctx context.Context,
	request *connect.Request[tammyv1.PostManualJournalRequest],
) (*connect.Response[tammyv1.PostManualJournalResponse], error) {
	if handler == nil || handler.posting == nil || request == nil || request.Msg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	response, err := handler.posting.PostManualJournal(ctx, request.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	return connect.NewResponse(response), nil
}

func (handler *accountingHandler) GetAccount(
	ctx context.Context,
	request *connect.Request[tammyv1.GetAccountRequest],
) (*connect.Response[tammyv1.GetAccountResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil ||
		request.Msg == nil || request.Msg.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := accounting.NewAccountRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	account, err := repository.Get(ctx, request.Msg.AccountId)
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetAccountResponse{Account: account}), nil
}

func (handler *accountingHandler) ListAccounts(
	ctx context.Context,
	request *connect.Request[tammyv1.ListAccountsRequest],
) (*connect.Response[tammyv1.ListAccountsResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil ||
		request.Msg == nil || request.Msg.Authentication == nil || request.Msg.Page == nil ||
		request.Msg.Page.Cursor != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	pageSize := int(request.Msg.Page.PageSize)
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 200 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := accounting.NewAccountRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	accounts, err := repository.List(ctx, request.Msg.OrganisationId, "", "", 200)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	filtered := filterAccounts(accounts, request.Msg)
	if len(filtered) > pageSize {
		filtered = filtered[:pageSize]
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.ListAccountsResponse{
		Accounts: filtered,
		Page:     &tammyv1.PageInfo{ReturnedCount: uint32(len(filtered))},
	}), nil
}

func (handler *accountingHandler) GetJournal(
	ctx context.Context,
	request *connect.Request[tammyv1.GetJournalRequest],
) (*connect.Response[tammyv1.GetJournalResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil ||
		request.Msg == nil || request.Msg.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	journal, err := repository.Get(ctx, request.Msg.JournalId)
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetJournalResponse{Journal: journal}), nil
}

func (handler *accountingHandler) ListJournals(
	ctx context.Context,
	request *connect.Request[tammyv1.ListJournalsRequest],
) (*connect.Response[tammyv1.ListJournalsResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil ||
		request.Msg == nil || request.Msg.Authentication == nil || request.Msg.Page == nil ||
		request.Msg.Page.Cursor != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	pageSize := int(request.Msg.Page.PageSize)
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 200 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	startDate := ""
	if request.Msg.StartDate != nil {
		startDate = fmt.Sprintf("%04d-%02d-%02d", request.Msg.StartDate.Year, request.Msg.StartDate.Month, request.Msg.StartDate.Day)
	}
	endDate := ""
	if request.Msg.EndDate != nil {
		endDate = fmt.Sprintf("%04d-%02d-%02d", request.Msg.EndDate.Year, request.Msg.EndDate.Month, request.Msg.EndDate.Day)
	}
	journals, err := repository.List(ctx, request.Msg.OrganisationId, startDate, endDate, 200)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	filtered := filterJournals(journals, request.Msg)
	if len(filtered) > pageSize {
		filtered = filtered[:pageSize]
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.ListJournalsResponse{
		Journals: filtered,
		Page:     &tammyv1.PageInfo{ReturnedCount: uint32(len(filtered))},
	}), nil
}

func filterJournals(journals []*tammyv1.Journal, request *tammyv1.ListJournalsRequest) []*tammyv1.Journal {
	filtered := make([]*tammyv1.Journal, 0, len(journals))
	for _, journal := range journals {
		if journal == nil || request.State != nil && journal.State != request.GetState() ||
			request.Source != nil && journal.Source != request.GetSource() {
			continue
		}
		filtered = append(filtered, journal)
	}
	return filtered
}

func (handler *accountingHandler) GetTrialBalance(
	ctx context.Context,
	request *connect.Request[tammyv1.GetTrialBalanceRequest],
) (*connect.Response[tammyv1.GetTrialBalanceResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil ||
		request.Msg == nil || request.Msg.Authentication == nil || request.Msg.AsOfDate == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	asOf := fmt.Sprintf("%04d-%02d-%02d", request.Msg.AsOfDate.Year, request.Msg.AsOfDate.Month, request.Msg.AsOfDate.Day)
	lines, debits, credits, revision, err := repository.TrialBalance(ctx, request.Msg.OrganisationId, asOf)
	if err != nil || request.Msg.ExpectedFinancialRevision != nil &&
		revision != request.Msg.GetExpectedFinancialRevision() || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetTrialBalanceResponse{
		Lines: lines, TotalDebits: localAUDMoney(debits), TotalCredits: localAUDMoney(credits),
		FinancialRevision: revision,
	}), nil
}

func localAUDMoney(minor int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor}
}

func filterAccounts(accounts []*tammyv1.Account, request *tammyv1.ListAccountsRequest) []*tammyv1.Account {
	query := strings.ToLower(strings.TrimSpace(request.GetQuery()))
	filtered := make([]*tammyv1.Account, 0, len(accounts))
	for _, account := range accounts {
		if account == nil || request.Status != nil && account.Status != request.GetStatus() ||
			request.Type != nil && account.Type != request.GetType() {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(account.Code), query) &&
			!strings.Contains(strings.ToLower(account.Name), query) {
			continue
		}
		filtered = append(filtered, account)
	}
	return filtered
}

// InstalledAccountCount is a narrow diagnostic used by the local integration
// proof. Product reads use generated Accounting RPCs as they are enabled.
func (module *LedgerModule) InstalledAccountCount(ctx context.Context) int {
	module.mu.RLock()
	database := module.db
	module.mu.RUnlock()
	if database == nil || ctx == nil {
		return 0
	}
	var count int
	if database.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&count) != nil {
		return 0
	}
	return count
}

type organisationRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *organisationRoute) set(service tammyv1connect.OrganisationServiceHandler) error {
	if route == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewOrganisationServiceHandler(service, append([]connect.HandlerOption(nil), route.options...)...)
	route.mu.Lock()
	route.handler = handler
	route.mu.Unlock()
	return nil
}

func (route *organisationRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.OrganisationServiceName + "/", route
}

func (route *organisationRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local organisation unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

type organisationHandler struct {
	tammyv1connect.UnimplementedOrganisationServiceHandler
	service  *organisations.Service
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
}

func (handler *organisationHandler) CreateOrganisation(ctx context.Context, request *connect.Request[tammyv1.CreateOrganisationRequest]) (*connect.Response[tammyv1.CreateOrganisationResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	response, err := handler.service.CreateOrganisation(ctx, request.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	return connect.NewResponse(response), nil
}

func (handler *organisationHandler) UpdateOrganisation(ctx context.Context, request *connect.Request[tammyv1.UpdateOrganisationRequest]) (*connect.Response[tammyv1.UpdateOrganisationResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	response, err := handler.service.UpdateOrganisation(ctx, request.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	return connect.NewResponse(response), nil
}

func (handler *organisationHandler) GetOrganisation(ctx context.Context, request *connect.Request[tammyv1.GetOrganisationRequest]) (*connect.Response[tammyv1.GetOrganisationResponse], error) {
	if handler == nil || handler.database == nil || handler.identity == nil || request == nil || request.Msg == nil ||
		request.Msg.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := handler.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := organisations.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	profile, err := repository.Get(ctx, request.Msg.OrganisationId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrLedgerModule)
	}
	evidenceRepository, err := organisations.NewEvidenceRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	verification, err := evidenceRepository.GetCurrentMetadata(ctx, request.Msg.OrganisationId)
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetOrganisationResponse{Organisation: profile, CurrentVerification: verification}), nil
}

type organisationCommandTransaction struct {
	*sqlcipher.Transaction
	repositories organisations.CommandRepositories
	id           string
}

type accountingCommandTransaction struct {
	*sqlcipher.Transaction
	repositories accounting.CommandRepositories
	id           string
}

func (transaction *accountingCommandTransaction) TransactionID() string { return transaction.id }
func (transaction *accountingCommandTransaction) Repositories() accounting.CommandRepositories {
	return transaction.repositories
}
func (transaction *accountingCommandTransaction) IdempotencyExecutor() idempotency.Executor {
	return transaction.Transaction
}
func (transaction *accountingCommandTransaction) AuditExecutor() app.CommandSQLExecutor {
	return transaction.Transaction
}

type accountingTransactionStarter struct{ activation app.LocalWorkspaceActivation }

func (starter *accountingTransactionStarter) Begin(ctx context.Context) (app.OwnedCommandTransaction[accounting.CommandRepositories], error) {
	if starter == nil || starter.activation.Database == nil {
		return nil, ErrLedgerModule
	}
	tx, err := starter.activation.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (app.OwnedCommandTransaction[accounting.CommandRepositories], error) {
		_ = tx.Rollback()
		return nil, cause
	}
	accounts, err := accounting.NewAccountRepository(tx)
	if err != nil {
		return fail(err)
	}
	journals, err := accounting.NewJournalRepository(tx)
	if err != nil {
		return fail(err)
	}
	rules, err := artefacts.NewRepository(tx)
	if err != nil {
		return fail(err)
	}
	identifier, err := starter.activation.NewID()
	if err != nil {
		return fail(err)
	}
	factor := &organisationFactor{identity: starter.activation.Identity, executor: tx}
	return &accountingCommandTransaction{
		Transaction: tx,
		repositories: accounting.CommandRepositories{
			Accounts: accounts,
			Journals: journals,
			TaxCodes: rules,
			Factors:  factor,
		},
		id: identifier,
	}, nil
}

func (transaction *organisationCommandTransaction) TransactionID() string { return transaction.id }
func (transaction *organisationCommandTransaction) Repositories() organisations.CommandRepositories {
	return transaction.repositories
}
func (transaction *organisationCommandTransaction) IdempotencyExecutor() idempotency.Executor {
	return transaction.Transaction
}
func (transaction *organisationCommandTransaction) AuditExecutor() app.CommandSQLExecutor {
	return transaction.Transaction
}

type organisationTransactionStarter struct{ activation app.LocalWorkspaceActivation }

func (starter *organisationTransactionStarter) Begin(ctx context.Context) (app.OwnedCommandTransaction[organisations.CommandRepositories], error) {
	if starter == nil || starter.activation.Database == nil {
		return nil, ErrLedgerModule
	}
	tx, err := starter.activation.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (app.OwnedCommandTransaction[organisations.CommandRepositories], error) {
		_ = tx.Rollback()
		return nil, cause
	}
	profiles, err := organisations.NewRepository(tx)
	if err != nil {
		return fail(err)
	}
	accounts, err := accounting.NewAccountRepository(tx)
	if err != nil {
		return fail(err)
	}
	rules, err := artefacts.NewRepository(tx)
	if err != nil {
		return fail(err)
	}
	factor := &organisationFactor{identity: starter.activation.Identity, executor: tx}
	identifier, err := starter.activation.NewID()
	if err != nil {
		return fail(err)
	}
	repositories := organisations.CommandRepositories{
		Profiles: profiles,
		Setup:    accounting.InitialSetup{Accounts: accounts, Rules: rules},
		Factors:  factor,
	}
	return &organisationCommandTransaction{Transaction: tx, repositories: repositories, id: identifier}, nil
}

type organisationFactor struct {
	identity       app.LocalModuleIdentity
	executor       workspace.MutationExecutor
	authentication *tammyv1.AuthenticationContext
}

func (factor *organisationFactor) Consume(ctx context.Context, marker *tammyv1.FreshFactorContext, purpose string) error {
	if factor == nil || factor.identity == nil || factor.executor == nil || factor.authentication == nil {
		return ErrLedgerModule
	}
	return factor.identity.ConsumeFreshFactorWithin(ctx, factor.executor, factor.authentication, marker, purpose)
}

type organisationAuthorizer struct{ workspaceID string }

func (authorizer organisationAuthorizer) Authorize(ctx context.Context, scope app.CommandScope[organisations.CommandRepositories], authentication *tammyv1.AuthenticationContext, action authorisation.Action) (app.AuthorizedActor, error) {
	factor, ok := scope.Repositories().Factors.(*organisationFactor)
	if !ok || factor == nil || action != authorisation.ActionManageOrg || authentication == nil {
		return app.AuthorizedActor{}, ErrLedgerModule
	}
	if err := factor.identity.RequireAdministratorWithin(ctx, factor.executor, authentication); err != nil {
		return app.AuthorizedActor{}, err
	}
	factor.authentication = proto.Clone(authentication).(*tammyv1.AuthenticationContext)
	return app.AuthorizedActor{WorkspaceID: authorizer.workspaceID, UserID: authentication.ActorUserId, SessionID: authentication.SessionId}, nil
}

type accountingAuthorizer struct{ workspaceID string }

func (authorizer accountingAuthorizer) Authorize(
	ctx context.Context,
	scope app.CommandScope[accounting.CommandRepositories],
	authentication *tammyv1.AuthenticationContext,
	action authorisation.Action,
) (app.AuthorizedActor, error) {
	factor, ok := scope.Repositories().Factors.(*organisationFactor)
	if !ok || factor == nil || authentication == nil ||
		(action != authorisation.ActionManageAccounts && action != authorisation.ActionPostAccounting) {
		return app.AuthorizedActor{}, ErrLedgerModule
	}
	if err := factor.identity.RequireAdministratorWithin(ctx, factor.executor, authentication); err != nil {
		return app.AuthorizedActor{}, err
	}
	factor.authentication = proto.Clone(authentication).(*tammyv1.AuthenticationContext)
	return app.AuthorizedActor{
		WorkspaceID: authorizer.workspaceID,
		UserID:      authentication.ActorUserId,
		SessionID:   authentication.SessionId,
	}, nil
}

type localCommandAuditor struct{}

func (localCommandAuditor) Append(_ context.Context, _ app.CommandSQLExecutor, event *tammyv1.AuditEvent, payload []byte) error {
	if event == nil || len(payload) == 0 {
		return ErrLedgerModule
	}
	return nil
}

type localAuditFactory struct{ workspaceID string }

func (factory localAuditFactory) Build(_ context.Context, operation app.OrdinaryOperation, authentication *tammyv1.AuthenticationContext,
	operationKey, resourceID string, result proto.Message, payload *tammyv1.AuditEventPayload,
) (app.CommandResult, error) {
	return buildLocalAudit(factory.workspaceID, operation, authentication, operationKey, resourceID, result, payload)
}

type localAccountingAuditFactory struct{ workspaceID string }

func (factory localAccountingAuditFactory) Build(
	_ context.Context,
	operation app.OrdinaryOperation,
	authentication *tammyv1.AuthenticationContext,
	operationKey, resourceID string,
	result proto.Message,
	payload proto.Message,
) (app.CommandResult, error) {
	return buildLocalAudit(factory.workspaceID, operation, authentication, operationKey, resourceID, result, payload)
}

func buildLocalAudit(
	workspaceID string,
	operation app.OrdinaryOperation,
	authentication *tammyv1.AuthenticationContext,
	operationKey, resourceID string,
	result proto.Message,
	payload proto.Message,
) (app.CommandResult, error) {
	if authentication == nil || result == nil || payload == nil {
		return app.CommandResult{}, ErrLedgerModule
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		return app.CommandResult{}, ErrLedgerModule
	}
	digest := sha256.Sum256(encoded)
	payloadBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return app.CommandResult{}, ErrLedgerModule
	}
	return app.CommandResult{
		Result: result, ResourceID: resourceID, AuditPayload: payloadBytes,
		AuditEvent: &tammyv1.AuditEvent{
			WorkspaceId: workspaceID,
			Actor:       proto.Clone(authentication).(*tammyv1.AuthenticationContext),
			CommandType: string(operation), IdempotencyKey: &operationKey,
			Result: &tammyv1.AuditResultMetadata{
				TypeName: string(result.ProtoReflect().Descriptor().FullName()), DeterministicSha256: digest[:], OutcomeCode: "SUCCESS",
			},
		},
	}, nil
}

var _ app.LocalWorkspaceModule = (*LedgerModule)(nil)
var _ tammyv1connect.OrganisationServiceHandler = (*organisationHandler)(nil)
var _ tammyv1connect.AccountingServiceHandler = (*accountingHandler)(nil)
