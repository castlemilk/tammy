//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Package localproduct composes local business modules over one active
// encrypted workspace without exposing database capabilities to transport.
package localproduct

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

var ErrLedgerModule = errors.New("local product: ledger module unavailable")

type LedgerModule struct {
	organisationRoute *organisationRoute
	accountingRoute   *accountingRoute
	mu                sync.RWMutex
	db                *sqlcipher.Database
}

func NewLedgerModule() (*LedgerModule, error) {
	return &LedgerModule{
		organisationRoute: &organisationRoute{},
		accountingRoute:   &accountingRoute{},
	}, nil
}

func (module *LedgerModule) HandlerFactories() []transport.GeneratedHandlerFactory {
	if module == nil || module.organisationRoute == nil || module.accountingRoute == nil {
		return nil
	}
	return []transport.GeneratedHandlerFactory{
		module.organisationRoute.factory,
		module.accountingRoute.factory,
	}
}

func (module *LedgerModule) Activate(activation app.LocalWorkspaceActivation) error {
	if module == nil || module.organisationRoute == nil || module.accountingRoute == nil ||
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
		database: activation.Database,
		identity: activation.Identity,
	}); err != nil {
		return err
	}
	module.mu.Lock()
	module.db = activation.Database
	module.mu.Unlock()
	return nil
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
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
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
	if err != nil || tx.Commit() != nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetOrganisationResponse{Organisation: profile}), nil
}

type organisationCommandTransaction struct {
	*sqlcipher.Transaction
	repositories organisations.CommandRepositories
	id           string
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
			WorkspaceId: factory.workspaceID,
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
