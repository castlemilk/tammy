//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

type invoiceRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (r *invoiceRoute) set(service tammyv1connect.InvoiceServiceHandler) error {
	if r == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewInvoiceServiceHandler(service, append([]connect.HandlerOption(nil), r.options...)...)
	r.mu.Lock()
	r.handler = handler
	r.mu.Unlock()
	return nil
}
func (r *invoiceRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	r.mu.Lock()
	r.options = append([]connect.HandlerOption(nil), options...)
	r.mu.Unlock()
	return "/" + tammyv1connect.InvoiceServiceName + "/", r
}
func (r *invoiceRoute) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	handler := r.handler
	r.mu.RUnlock()
	if handler == nil {
		http.Error(w, "local invoices unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, req)
}

func activateInvoices(activation app.LocalWorkspaceActivation, route *invoiceRoute) error {
	observer, err := idempotency.NewObserver(activation.Database)
	if err != nil {
		return err
	}
	elector, err := idempotency.NewElector(idempotency.Config{Clock: clock.Func(activation.Now), Observe: observer})
	if err != nil {
		return err
	}
	transactions, err := app.NewCommandTransactions[invoices.CommandRepositories](activation.WorkspaceID, &invoiceTransactionStarter{activation})
	if err != nil {
		return err
	}
	coordinator, err := app.NewCoordinator(app.CoordinatorConfig[invoices.CommandRepositories]{Transactions: transactions, Authorizer: invoiceAuthorizer{activation.WorkspaceID}, Elector: elector, Auditor: invoiceCommandAuditor{activation.Now}})
	if err != nil {
		return err
	}
	service, err := invoices.NewService(coordinator, localAccountingAuditFactory{activation.WorkspaceID}, activation.Now, activation.NewID)
	if err != nil {
		return err
	}
	if len(activation.InstallationKey) < sha256.Size {
		return ErrLedgerModule
	}
	mac := hmac.New(sha256.New, activation.InstallationKey)
	_, _ = mac.Write([]byte("tammy.invoice-page.v1\x00" + activation.WorkspaceID))
	cursors, err := paging.NewCodec(mac.Sum(nil))
	if err != nil {
		return err
	}
	return route.set(&invoiceHandler{service: service, database: activation.Database, identity: activation.Identity, now: activation.Now, newID: activation.NewID, cursors: cursors})
}

type invoiceCommandTransaction struct {
	*sqlcipher.Transaction
	repositories invoices.CommandRepositories
	id           string
}

func (t *invoiceCommandTransaction) TransactionID() string { return t.id }
func (t *invoiceCommandTransaction) Repositories() invoices.CommandRepositories {
	return t.repositories
}
func (t *invoiceCommandTransaction) IdempotencyExecutor() idempotency.Executor { return t.Transaction }
func (t *invoiceCommandTransaction) AuditExecutor() app.CommandSQLExecutor     { return t.Transaction }

type invoiceTransactionStarter struct{ activation app.LocalWorkspaceActivation }

func (s *invoiceTransactionStarter) Begin(ctx context.Context) (app.OwnedCommandTransaction[invoices.CommandRepositories], error) {
	tx, err := s.activation.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	fail := func(e error) (app.OwnedCommandTransaction[invoices.CommandRepositories], error) {
		_ = tx.Rollback()
		return nil, e
	}
	repo, err := invoices.NewRepository(tx)
	if err != nil {
		return fail(err)
	}
	profiles, err := organisations.NewRepository(tx)
	if err != nil {
		return fail(err)
	}
	accounting, err := invoices.NewAccountingRepository(tx, s.activation.Now, s.activation.NewID)
	if err != nil {
		return fail(err)
	}
	id, err := s.activation.NewID()
	if err != nil {
		return fail(err)
	}
	return &invoiceCommandTransaction{Transaction: tx, id: id, repositories: invoices.CommandRepositories{Invoices: repo, Profiles: profiles, Accounting: accounting, Authorize: func(ctx context.Context, authentication *tammyv1.AuthenticationContext, action authorisation.Action) error {
		return s.activation.Identity.AuthorizeWithin(ctx, tx, authentication, action)
	}}}, nil
}

type invoiceAuthorizer struct{ workspaceID string }

func (a invoiceAuthorizer) Authorize(ctx context.Context, scope app.CommandScope[invoices.CommandRepositories], authentication *tammyv1.AuthenticationContext, action authorisation.Action) (app.AuthorizedActor, error) {
	permission := scope.Repositories().Authorize
	if permission == nil || authentication == nil || action != authorisation.ActionPostAccounting {
		return app.AuthorizedActor{}, ErrLedgerModule
	}
	if err := permission(ctx, authentication, action); err != nil {
		return app.AuthorizedActor{}, err
	}
	return app.AuthorizedActor{WorkspaceID: a.workspaceID, UserID: authentication.ActorUserId, SessionID: authentication.SessionId}, nil
}

type invoiceCommandAuditor struct{ now func() time.Time }

func (a invoiceCommandAuditor) Append(ctx context.Context, executor app.CommandSQLExecutor, event *tammyv1.AuditEvent, payload []byte) error {
	if event == nil || event.Actor == nil || event.IdempotencyKey == nil || event.Result == nil || len(payload) == 0 {
		return ErrLedgerModule
	}
	statement := `INSERT INTO invoice_command_audit_v1(operation_key,workspace_id,actor_user_id,session_id,command_type,result_sha256,payload_proto,occurred_at) VALUES (?,?,?,?,?,?,?,?)`
	switch app.OrdinaryOperation(event.CommandType) {
	case app.OrdinaryOperationSaveInvoiceDraft, app.OrdinaryOperationIssueInvoice:
	case app.OrdinaryOperationSaveInvoiceTemplate, app.OrdinaryOperationUploadInvoiceLogo:
		statement = `INSERT INTO invoice_branding_command_audit_v1(operation_key,workspace_id,actor_user_id,session_id,command_type,result_sha256,payload_proto,occurred_at) VALUES (?,?,?,?,?,?,?,?)`
	case app.OrdinaryOperationPostInvoiceIncome, app.OrdinaryOperationRecordInvoicePayment:
		statement = `INSERT INTO invoice_income_command_audit_v1(operation_key,workspace_id,actor_user_id,session_id,command_type,result_sha256,payload_proto,occurred_at) VALUES (?,?,?,?,?,?,?,?)`
	default:
		return ErrLedgerModule
	}
	_, err := executor.ExecContext(ctx, statement, *event.IdempotencyKey, event.WorkspaceId, event.Actor.ActorUserId, event.Actor.SessionId, event.CommandType, event.Result.DeterministicSha256, payload, a.now().UTC().Format(time.RFC3339Nano))
	return err
}

type invoiceHandler struct {
	tammyv1connect.UnimplementedInvoiceServiceHandler
	service  *invoices.Service
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
	now      func() time.Time
	newID    func() (string, error)
	cursors  *paging.Codec
}

func (h *invoiceHandler) SaveInvoiceDraft(ctx context.Context, r *connect.Request[tammyv1.SaveInvoiceDraftRequest]) (*connect.Response[tammyv1.SaveInvoiceDraftResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	value, err := h.service.SaveDraft(ctx, r.Msg)
	if err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(value), nil
}
func (h *invoiceHandler) IssueInvoice(ctx context.Context, r *connect.Request[tammyv1.IssueInvoiceRequest]) (*connect.Response[tammyv1.IssueInvoiceResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	value, err := h.service.Issue(ctx, r.Msg)
	if err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(value), nil
}
func (h *invoiceHandler) GetInvoice(ctx context.Context, r *connect.Request[tammyv1.GetInvoiceRequest]) (*connect.Response[tammyv1.GetInvoiceResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || r == nil || r.Msg == nil || r.Msg.Authentication == nil {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, r.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	tx, err := h.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, invoiceError(err)
	}
	defer tx.Rollback()
	repo, _ := invoices.NewRepository(tx)
	value, err := repo.Get(ctx, r.Msg.OrganisationId, r.Msg.InvoiceId)
	if err != nil {
		return nil, invoiceError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(&tammyv1.GetInvoiceResponse{Invoice: value}), nil
}
func (h *invoiceHandler) ListInvoices(ctx context.Context, r *connect.Request[tammyv1.ListInvoicesRequest]) (*connect.Response[tammyv1.ListInvoicesResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || r == nil || r.Msg == nil || r.Msg.Authentication == nil || r.Msg.Page == nil {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, r.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	limit := int(r.Msg.Page.PageSize)
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	queryHash := sha256.Sum256([]byte("tammy.v1.InvoiceService.ListInvoices\x00" + r.Msg.OrganisationId + "\x00" + r.Msg.Authentication.ActorUserId + "\x00" + strconv.Itoa(limit)))
	position := ""
	if r.Msg.Page.Cursor != nil {
		cursor, err := h.cursors.Decode(*r.Msg.Page.Cursor, queryHash)
		if err != nil || cursor.Snapshot != "invoices-v1" {
			return nil, invoiceError(invoices.ErrInvalidDraft)
		}
		position = cursor.Position
	}
	tx, err := h.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, invoiceError(err)
	}
	defer tx.Rollback()
	repo, _ := invoices.NewRepository(tx)
	values, next, err := repo.ListPage(ctx, r.Msg.OrganisationId, position, limit)
	if err != nil {
		return nil, invoiceError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, invoiceError(err)
	}
	page := &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}
	if next != "" {
		signed, err := h.cursors.Encode(paging.Cursor{Snapshot: "invoices-v1", Position: next, QueryHash: queryHash})
		if err != nil {
			return nil, invoiceError(err)
		}
		page.NextCursor = &signed
	}
	return connect.NewResponse(&tammyv1.ListInvoicesResponse{Invoices: values, Page: page}), nil
}
func invoiceError(err error) error {
	code := connect.CodeInternal
	switch {
	case errors.Is(err, invoices.ErrInvalidDraft), errors.Is(err, invoices.ErrLogoAsset), errors.Is(err, invoices.ErrPayment):
		code = connect.CodeInvalidArgument
	case errors.Is(err, invoices.ErrNotFound):
		code = connect.CodeNotFound
	case errors.Is(err, invoices.ErrConflict), errors.Is(err, invoices.ErrSourceDocument), errors.Is(err, invoices.ErrGSTBasisHistory), errors.Is(err, accounting.ErrClosedPeriod), errors.Is(err, accounting.ErrAccountNotPostable):
		code = connect.CodeFailedPrecondition
	case errors.Is(err, invoices.ErrNumberConflict), errors.Is(err, idempotency.ErrConflict):
		code = connect.CodeAlreadyExists
	}
	if fault, ok := faults.CodeOf(err); ok {
		switch fault {
		case faults.CodeAuthenticationRequired:
			code = connect.CodeUnauthenticated
		case faults.CodePermissionDenied:
			code = connect.CodePermissionDenied
		}
	}
	return connect.NewError(code, ErrLedgerModule)
}

var _ tammyv1connect.InvoiceServiceHandler = (*invoiceHandler)(nil)
