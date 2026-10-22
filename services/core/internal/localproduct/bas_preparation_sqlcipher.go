//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type basPreparationRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *basPreparationRoute) set(service tammyv1connect.BasPreparationServiceHandler) error {
	if route == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewBasPreparationServiceHandler(service,
		append([]connect.HandlerOption(nil), route.options...)...)
	route.mu.Lock()
	route.handler = handler
	route.mu.Unlock()
	return nil
}
func (route *basPreparationRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.BasPreparationServiceName + "/", route
}
func (route *basPreparationRoute) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(w, "local BAS preparation unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, request)
}

func activateBASPreparation(activation app.LocalWorkspaceActivation, route *basPreparationRoute) error {
	if route == nil || len(activation.InstallationKey) < sha256.Size ||
		activation.Database == nil || activation.Identity == nil || activation.Now == nil ||
		activation.NewID == nil || !ids.IsCanonicalV7(activation.WorkspaceID) {
		return ErrLedgerModule
	}
	mac := hmac.New(sha256.New, activation.InstallationKey)
	_, _ = mac.Write([]byte("tammy.bas-preparation-page.v1\x00" + activation.WorkspaceID))
	cursors, err := paging.NewCodec(mac.Sum(nil))
	if err != nil {
		return ErrLedgerModule
	}
	observer, err := idempotency.NewObserver(activation.Database)
	if err != nil {
		return err
	}
	elector, err := idempotency.NewElector(idempotency.Config{Clock: clock.Func(activation.Now), Observe: observer})
	if err != nil {
		return err
	}
	ordinary, err := newOrdinaryBASAuditAppender(activation)
	if err != nil {
		return err
	}
	transactions, err := app.NewCommandTransactions[baspreparation.CommandRepositories](
		activation.WorkspaceID, &basPreparationTransactionStarter{activation: activation, cursors: cursors, ordinary: ordinary})
	if err != nil {
		return err
	}
	coordinator, err := app.NewCoordinator(app.CoordinatorConfig[baspreparation.CommandRepositories]{
		Transactions: transactions,
		Authorizer:   basPreparationAuthorizer{workspaceID: activation.WorkspaceID, ordinary: ordinary},
		Elector:      elector,
		Auditor:      basPreparationCommandAuditor{port: ordinary},
	})
	if err != nil {
		return err
	}
	service, err := baspreparation.NewService(coordinator,
		basPreparationAuditFactory{workspaceID: activation.WorkspaceID,
			fingerprint: append([]byte(nil), activation.AuditSchemaFingerprint...),
			now:         activation.Now, newID: activation.NewID},
		activation.Now, activation.NewID)
	if err != nil {
		return err
	}
	return route.set(&basPreparationHandler{service: service, database: activation.Database, identity: activation.Identity, cursors: cursors, now: activation.Now, newID: activation.NewID})
}

type basPreparationCommandTransaction struct {
	*sqlcipher.Transaction
	repositories baspreparation.CommandRepositories
	id           string
	lifecycle    workspace.MutationLifecycle
	ctx          context.Context
}

func (transaction *basPreparationCommandTransaction) TransactionID() string { return transaction.id }
func (transaction *basPreparationCommandTransaction) Repositories() baspreparation.CommandRepositories {
	return transaction.repositories
}
func (transaction *basPreparationCommandTransaction) IdempotencyExecutor() idempotency.Executor {
	return transaction.Transaction
}
func (transaction *basPreparationCommandTransaction) AuditExecutor() app.CommandSQLExecutor {
	return transaction.lifecycle
}
func (transaction *basPreparationCommandTransaction) Commit() error {
	if err := transaction.Transaction.Commit(); err != nil {
		return err
	}
	return transaction.lifecycle.Publish(transaction.ctx)
}

type basPreparationTransactionStarter struct {
	activation app.LocalWorkspaceActivation
	cursors    *paging.Codec
	ordinary   *ordinaryBASAuditAppender
}

func (starter *basPreparationTransactionStarter) Begin(ctx context.Context) (
	app.OwnedCommandTransaction[baspreparation.CommandRepositories], error) {
	tx, err := starter.activation.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	fail := func(err error) (app.OwnedCommandTransaction[baspreparation.CommandRepositories], error) {
		_ = tx.Rollback()
		return nil, err
	}
	profiles, err := baspreparation.NewProfileRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	periods, err := baspreparation.NewPeriodRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	preparations, err := baspreparation.NewPreparationRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	savedPayg, err := baspreparation.NewSavedPaygRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	capture, err := baspreparation.NewBASCommandCapture(tx, starter.cursors, starter.activation.Now, starter.activation.NewID)
	if err != nil {
		return fail(err)
	}
	calculations, err := baspreparation.NewCalculationRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	validations, err := baspreparation.NewValidationRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	acknowledgements, err := baspreparation.NewWarningAcknowledgementRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	frozen, err := baspreparation.NewFrozenRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	familyIdentities, err := baspreparation.NewFamilyIdentityRepository(tx)
	if err != nil {
		return fail(err)
	}
	familyStates, err := baspreparation.NewFamilyStateRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	branchIdentities, err := baspreparation.NewBranchIdentityRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	branchStates, err := baspreparation.NewBranchStateRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	freezeChains, err := baspreparation.NewFreezeChainReader(familyIdentities, familyStates, branchIdentities,
		branchStates, preparations)
	if err != nil {
		return fail(err)
	}
	externalHistory, err := baspreparation.NewBASExternalHistoryReader(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	observations, err := baspreparation.NewExternalObservationRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	resolutions, err := baspreparation.NewDisputeResolutionRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	exportEvents, err := baspreparation.NewExportEventRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	incomeYearPins, err := baspreparation.NewIncomeYearPinRepository(tx, starter.cursors)
	if err != nil {
		return fail(err)
	}
	transactionID, err := starter.activation.NewID()
	if err != nil || !ids.IsCanonicalV7(transactionID) {
		return fail(ErrLedgerModule)
	}
	lifecycle := workspace.NewMutationLifecycle(tx)
	repositories := baspreparation.CommandRepositories{
		ExternalHistory: externalHistory, ExternalObservations: observations, Resolutions: resolutions, ExportEvents: exportEvents,
		Profiles: profiles, Periods: periods, Preparations: preparations, SavedPayg: savedPayg, Regenerate: capture.Regenerate, CreateExternalAttempt: capture.CreateExternalAttempt,
		CaptureCurrent: capture.CaptureCurrent, IncomeYearPins: incomeYearPins, Calculations: calculations,
		Validations: validations, Acknowledgements: acknowledgements, Frozen: frozen,
		FamilyIdentities: familyIdentities, FamilyStates: familyStates, BranchIdentities: branchIdentities,
		BranchStates: branchStates, FreezeChains: freezeChains, AdvancePreparation: capture.AdvancePreparation,
		Authorize: func(ctx context.Context, authentication *tammyv1.AuthenticationContext,
			action authorisation.Action) error {
			if err := starter.activation.Identity.AuthorizeWithin(ctx, tx, authentication, action); err != nil {
				return err
			}
			return starter.ordinary.RequireCoverageWithin(ctx, tx)
		},
		ConsumeFreshFactor: func(ctx context.Context, authentication *tammyv1.AuthenticationContext,
			factor *tammyv1.FreshFactorContext, purpose string) error {
			if !isBASApprovalPurpose(purpose) {
				return ErrLedgerModule
			}
			return starter.activation.Identity.ConsumeFreshFactorWithin(ctx, tx, authentication, factor, purpose)
		},
	}
	return &basPreparationCommandTransaction{Transaction: tx, repositories: repositories, id: transactionID,
		lifecycle: lifecycle, ctx: ctx}, nil
}

type basPreparationAuthorizer struct {
	workspaceID string
	ordinary    *ordinaryBASAuditAppender
}

func (authorizer basPreparationAuthorizer) Authorize(ctx context.Context,
	scope app.CommandScope[baspreparation.CommandRepositories],
	authentication *tammyv1.AuthenticationContext, action authorisation.Action) (app.AuthorizedActor, error) {
	permission := scope.Repositories().Authorize
	if permission == nil || authentication == nil || !isBASPreparationAction(action) {
		return app.AuthorizedActor{}, ErrLedgerModule
	}
	if err := permission(ctx, authentication, action); err != nil {
		return app.AuthorizedActor{}, err
	}
	if err := authorizer.ordinary.RequireCoverage(); err != nil {
		return app.AuthorizedActor{}, err
	}
	return app.AuthorizedActor{WorkspaceID: authorizer.workspaceID, UserID: authentication.ActorUserId,
		SessionID: authentication.SessionId}, nil
}

func isBASPreparationAction(action authorisation.Action) bool {
	switch action {
	case authorisation.ActionCorrectBASExternalObservation, authorisation.ActionReadFinancial, authorisation.ActionPrepareTax,
		authorisation.ActionApproveBASFreeze,
		authorisation.ActionApproveBASExternalConfirmation,
		authorisation.ActionApproveBASUnusedReplacement,
		authorisation.ActionApproveBASDisputeResolution,
		authorisation.ActionApproveBASRejectedReplacement:
		return true
	default:
		return false
	}
}

func isBASApprovalPurpose(purpose string) bool {
	switch purpose {
	case authorisation.PurposeBASFreezeManualHandoff,
		authorisation.PurposeBASRecordExternalConfirmation,
		authorisation.PurposeBASReplaceUnusedAttempt,
		authorisation.PurposeBASResolveExternalDispute,
		authorisation.PurposeBASReplaceRejectedAttempt:
		return true
	default:
		return false
	}
}

type basPreparationHandler struct {
	tammyv1connect.UnimplementedBasPreparationServiceHandler
	service  *baspreparation.Service
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
	cursors  *paging.Codec
	now      func() time.Time
	newID    func() (string, error)
}

func (handler *basPreparationHandler) SetBasObligationsProfile(ctx context.Context,
	request *connect.Request[tammyv1.SetBasObligationsProfileRequest]) (*connect.Response[tammyv1.SetBasObligationsProfileResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.SetBasObligationsProfile(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}
func (handler *basPreparationHandler) FreezeBasForManualHandoff(ctx context.Context,
	request *connect.Request[tammyv1.FreezeBasForManualHandoffRequest]) (*connect.Response[tammyv1.FreezeBasForManualHandoffResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.FreezeBasForManualHandoff(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}

func (handler *basPreparationHandler) ConfirmBasPeriod(ctx context.Context,
	request *connect.Request[tammyv1.ConfirmBasPeriodRequest]) (*connect.Response[tammyv1.ConfirmBasPeriodResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.ConfirmBasPeriod(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}
func (handler *basPreparationHandler) SaveBasPaygInputs(ctx context.Context,
	request *connect.Request[tammyv1.SaveBasPaygInputsRequest]) (*connect.Response[tammyv1.SaveBasPaygInputsResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.SaveBasPaygInputs(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}
func (handler *basPreparationHandler) RegenerateBasPreparation(ctx context.Context,
	request *connect.Request[tammyv1.RegenerateBasPreparationRequest]) (*connect.Response[tammyv1.RegenerateBasPreparationResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.RegenerateBasPreparation(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}
func basPreparationConnectError(err error) error {
	if err == nil {
		return nil
	}
	if fault, ok := faults.CodeOf(err); ok {
		switch fault {
		case faults.CodeAuthenticationRequired:
			return connect.NewError(connect.CodeUnauthenticated, errors.New("BAS authentication required"))
		case faults.CodePermissionDenied:
			return connect.NewError(connect.CodePermissionDenied, errors.New("BAS permission denied"))
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	case errors.Is(err, baspreparation.ErrSourceBound), errors.Is(err, baspreparation.ErrSnapshotBound):
		return connect.NewError(connect.CodeResourceExhausted, errors.New("The complete BAS source exceeds the supported capture capacity. Retain the actual statement period and review source records or contact support."))
	case errors.Is(err, idempotency.ErrConflict):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("BAS command key has different retained inputs"))
	case errors.Is(err, baspreparation.ErrExportEventConflict), errors.Is(err, baspreparation.ErrCommandConflict),
		errors.Is(err, baspreparation.ErrProfileNotEffective),
		errors.Is(err, baspreparation.ErrProfileConflict),
		errors.Is(err, baspreparation.ErrPeriodConflict),
		errors.Is(err, baspreparation.ErrPreparationConflict),
		errors.Is(err, baspreparation.ErrSavedPaygConflict), errors.Is(err, baspreparation.ErrValidationConflict), errors.Is(err, baspreparation.ErrWarningAcknowledgementConflict):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, baspreparation.ErrFreezeNotReady):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, baspreparation.ErrInvalidCommand):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, errors.New("BAS command failed"))
	}
}

func (handler *basPreparationHandler) ValidateBasPreparation(ctx context.Context, request *connect.Request[tammyv1.ValidateBasPreparationRequest]) (*connect.Response[tammyv1.ValidateBasPreparationResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	result, err := handler.service.ValidateBasPreparation(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(result), nil
}

func (handler *basPreparationHandler) AcknowledgeBasWarnings(ctx context.Context, request *connect.Request[tammyv1.AcknowledgeBasWarningsRequest]) (*connect.Response[tammyv1.AcknowledgeBasWarningsResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	result, err := handler.service.AcknowledgeBasWarnings(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(result), nil
}

func (handler *basPreparationHandler) RecordExternalBasObservation(ctx context.Context, request *connect.Request[tammyv1.RecordExternalBasObservationRequest]) (*connect.Response[tammyv1.RecordExternalBasObservationResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.RecordExternalBasObservation(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}

func (handler *basPreparationHandler) CorrectExternalBasObservation(ctx context.Context, request *connect.Request[tammyv1.CorrectExternalBasObservationRequest]) (*connect.Response[tammyv1.CorrectExternalBasObservationResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.CorrectExternalBasObservation(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}

func (handler *basPreparationHandler) ResolveExternalBasDispute(ctx context.Context, request *connect.Request[tammyv1.ResolveExternalBasDisputeRequest]) (*connect.Response[tammyv1.ResolveExternalBasDisputeResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.ResolveExternalBasDispute(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}

func (handler *basPreparationHandler) RecordBasExportEvent(ctx context.Context, request *connect.Request[tammyv1.RecordBasExportEventRequest]) (*connect.Response[tammyv1.RecordBasExportEventResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.RecordBasExportEvent(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}

func (handler *basPreparationHandler) CreateBasReplacement(ctx context.Context, request *connect.Request[tammyv1.CreateBasReplacementRequest]) (*connect.Response[tammyv1.CreateBasReplacementResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.CreateBasReplacement(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}

func (handler *basPreparationHandler) CreateBasRevision(ctx context.Context, request *connect.Request[tammyv1.CreateBasRevisionRequest]) (*connect.Response[tammyv1.CreateBasRevisionResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.CreateBasRevision(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}

func (handler *basPreparationHandler) ReplaceRejectedBasAttempt(ctx context.Context, request *connect.Request[tammyv1.ReplaceRejectedBasAttemptRequest]) (*connect.Response[tammyv1.ReplaceRejectedBasAttemptResponse], error) {
	if handler == nil || handler.service == nil || request == nil || request.Msg == nil {
		return nil, basPreparationConnectError(baspreparation.ErrInvalidCommand)
	}
	value, err := handler.service.ReplaceRejectedBasAttempt(ctx, request.Msg)
	if err != nil {
		return nil, basPreparationConnectError(err)
	}
	return connect.NewResponse(value), nil
}
