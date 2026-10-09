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
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type purchaseRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (r *purchaseRoute) set(service tammyv1connect.PurchaseServiceHandler) error {
	if r == nil || service == nil {
		return ErrLedgerModule
	}
	_, handler := tammyv1connect.NewPurchaseServiceHandler(service, append([]connect.HandlerOption(nil), r.options...)...)
	r.mu.Lock()
	r.handler = handler
	r.mu.Unlock()
	return nil
}
func (r *purchaseRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	r.mu.Lock()
	r.options = append([]connect.HandlerOption(nil), options...)
	r.mu.Unlock()
	return "/" + tammyv1connect.PurchaseServiceName + "/", r
}
func (r *purchaseRoute) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	r.mu.RLock()
	handler := r.handler
	r.mu.RUnlock()
	if handler == nil {
		http.Error(w, "local purchases unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, request)
}

func activatePurchases(activation app.LocalWorkspaceActivation, route *purchaseRoute) error {
	if len(activation.InstallationKey) < sha256.Size {
		return ErrLedgerModule
	}
	// Installing a pinned rule is retained configuration, never an accounting
	// command or an automatic purchase/evidence/credit backfill.
	tx, err := activation.Database.BeginEncryptedTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	repo, err := purchases.NewRepository(tx, activation.Now, activation.NewID)
	if err != nil {
		return err
	}
	if err := repo.RetainEvidenceRules(context.Background()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	observer, err := idempotency.NewObserver(activation.Database)
	if err != nil {
		return err
	}
	elector, err := idempotency.NewElector(idempotency.Config{Clock: clock.Func(activation.Now), Observe: observer})
	if err != nil {
		return err
	}
	ordinary, err := newOrdinaryPurchaseAuditAppender(activation)
	if err != nil {
		return err
	}
	transactions, err := app.NewCommandTransactions[purchases.CommandRepositories](activation.WorkspaceID, &purchaseTransactionStarter{activation: activation, ordinary: ordinary})
	if err != nil {
		return err
	}
	coordinator, err := app.NewCoordinator(app.CoordinatorConfig[purchases.CommandRepositories]{Transactions: transactions, Authorizer: purchaseAuthorizer{workspaceID: activation.WorkspaceID, ordinary: ordinary}, Elector: elector, Auditor: purchaseCommandAuditor{now: activation.Now, ordinary: ordinary}})
	if err != nil {
		return err
	}
	service, err := purchases.NewService(coordinator, localAccountingAuditFactory{activation.WorkspaceID})
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, activation.InstallationKey)
	_, _ = mac.Write([]byte("tammy.purchase-payment-page.v1\x00" + activation.WorkspaceID))
	cursors, err := paging.NewCodec(mac.Sum(nil))
	if err != nil {
		return err
	}
	return route.set(&purchaseHandler{service: service, database: activation.Database, identity: activation.Identity, now: activation.Now, newID: activation.NewID, cursors: cursors})
}

type purchaseCommandTransaction struct {
	*sqlcipher.Transaction
	repositories purchases.CommandRepositories
	id           string
	lifecycle    workspace.MutationLifecycle
	ctx          context.Context
}

func (t *purchaseCommandTransaction) TransactionID() string { return t.id }
func (t *purchaseCommandTransaction) Repositories() purchases.CommandRepositories {
	return t.repositories
}
func (t *purchaseCommandTransaction) IdempotencyExecutor() idempotency.Executor { return t.Transaction }
func (t *purchaseCommandTransaction) AuditExecutor() app.CommandSQLExecutor     { return t.lifecycle }
func (t *purchaseCommandTransaction) Commit() error {
	if err := t.Transaction.Commit(); err != nil {
		return err
	}
	return t.lifecycle.Publish(t.ctx)
}

type purchaseTransactionStarter struct {
	activation app.LocalWorkspaceActivation
	ordinary   *ordinaryPurchaseAuditAppender
}

func (s *purchaseTransactionStarter) Begin(ctx context.Context) (app.OwnedCommandTransaction[purchases.CommandRepositories], error) {
	tx, err := s.activation.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	fail := func(err error) (app.OwnedCommandTransaction[purchases.CommandRepositories], error) {
		_ = tx.Rollback()
		return nil, err
	}
	repo, err := purchases.NewRepository(tx, s.activation.Now, s.activation.NewID)
	if err != nil {
		return fail(err)
	}
	id, err := s.activation.NewID()
	if err != nil {
		return fail(err)
	}
	return &purchaseCommandTransaction{Transaction: tx, id: id, lifecycle: workspace.NewMutationLifecycle(tx), ctx: ctx, repositories: purchases.CommandRepositories{Purchases: repo, Authorize: func(ctx context.Context, auth *tammyv1.AuthenticationContext, action authorisation.Action) error {
		if err := s.activation.Identity.AuthorizeWithin(ctx, tx, auth, action); err != nil {
			return err
		}
		return s.ordinary.RequireCoverageWithin(ctx, tx)
	}}}, nil
}

type purchaseAuthorizer struct {
	workspaceID string
	ordinary    *ordinaryPurchaseAuditAppender
}

func (a purchaseAuthorizer) Authorize(ctx context.Context, scope app.CommandScope[purchases.CommandRepositories], auth *tammyv1.AuthenticationContext, action authorisation.Action) (app.AuthorizedActor, error) {
	permission := scope.Repositories().Authorize
	if permission == nil || auth == nil || (action != authorisation.ActionPostAccounting && action != authorisation.ActionPrepareTax) {
		return app.AuthorizedActor{}, ErrLedgerModule
	}
	if err := permission(ctx, auth, action); err != nil {
		return app.AuthorizedActor{}, err
	}
	if err := a.ordinary.RequireCoverage(); err != nil {
		return app.AuthorizedActor{}, err
	}
	return app.AuthorizedActor{WorkspaceID: a.workspaceID, UserID: auth.ActorUserId, SessionID: auth.SessionId}, nil
}

type purchaseCommandAuditor struct {
	now      func() time.Time
	ordinary *ordinaryPurchaseAuditAppender
}

func (a purchaseCommandAuditor) Append(ctx context.Context, executor app.CommandSQLExecutor, event *tammyv1.AuditEvent, payload []byte) error {
	if a.now == nil || event == nil || event.Actor == nil || event.IdempotencyKey == nil || event.Result == nil || len(payload) == 0 || len(payload) > 1<<20 {
		return ErrLedgerModule
	}
	switch app.OrdinaryOperation(event.CommandType) {
	case app.OrdinaryOperationPostReviewedPurchase, app.OrdinaryOperationRecordPurchasePayment, app.OrdinaryOperationRecordPurchaseTaxEvidence, app.OrdinaryOperationAttributePurchaseGst, app.OrdinaryOperationDeferPurchaseGstClaim, app.OrdinaryOperationResolveLegacyPurchaseReview:
	default:
		return ErrLedgerModule
	}
	// The accepted event owns organisation identity. An INSERT .. SELECT avoids
	// trusting renderer scope or decoding evidence content to create the audit.
	result, err := executor.ExecContext(ctx, `INSERT INTO purchase_command_audit_v1(operation_key,organisation_id,workspace_id,actor_user_id,session_id,command_type,result_sha256,payload_proto,occurred_at) SELECT operation_key,organisation_id,?,?,?,?,?,?,? FROM purchase_accounting_events_v1 WHERE operation_key=? AND command_type=? AND result_sha256=?`, event.WorkspaceId, event.Actor.ActorUserId, event.Actor.SessionId, event.CommandType, event.Result.DeterministicSha256, payload, a.now().UTC().Format(time.RFC3339Nano), *event.IdempotencyKey, event.CommandType, event.Result.DeterministicSha256)
	if err == nil {
		count, countErr := result.RowsAffected()
		if countErr != nil || count != 1 {
			return ErrLedgerModule
		}
	}
	if err != nil {
		return err
	}
	return a.ordinary.Append(ctx, executor, event)
}

type purchaseHandler struct {
	tammyv1connect.UnimplementedPurchaseServiceHandler
	service  *purchases.Service
	database *sqlcipher.Database
	identity app.LocalModuleIdentity
	now      func() time.Time
	newID    func() (string, error)
	cursors  *paging.Codec
}

func (h *purchaseHandler) GetPurchaseAccounting(ctx context.Context, r *connect.Request[tammyv1.GetPurchaseAccountingRequest]) (*connect.Response[tammyv1.GetPurchaseAccountingResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || r == nil || r.Msg == nil || r.Msg.Authentication == nil {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	if !ids.IsCanonicalV7(r.Msg.OrganisationId) || !ids.IsCanonicalV7(r.Msg.DocumentId) {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, r.Msg.Authentication); err != nil {
		return nil, h.failure(err)
	}
	tx, err := h.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, h.failure(err)
	}
	defer tx.Rollback()
	if err := h.identity.ValidateAuthorizationWithin(ctx, tx, r.Msg.Authentication, authorisation.ActionReadFinancial); err != nil {
		return nil, h.failure(err)
	}
	repo, err := purchases.NewRepository(tx, h.now, h.newID)
	if err != nil {
		return nil, h.failure(err)
	}
	state, evidence, deferral, err := repo.GetWithLatestDecisions(ctx, r.Msg.OrganisationId, r.Msg.DocumentId)
	if err != nil {
		return nil, h.failure(err)
	}
	var matches []*tammyv1.PurchaseDuplicateMatch
	if !state.Posted {
		matches, err = repo.DuplicateMatches(ctx, r.Msg.OrganisationId, r.Msg.DocumentId)
		if err != nil {
			return nil, h.failure(err)
		}
	}
	response := &tammyv1.GetPurchaseAccountingResponse{Accounting: state, LatestEvidence: evidence, LatestDeferral: deferral, DuplicateCandidates: matches}
	if !purchaseFrameFits(response) {
		return nil, h.failure(purchases.ErrSourceBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(response), nil
}
func (h *purchaseHandler) ListPurchasePayments(ctx context.Context, r *connect.Request[tammyv1.ListPurchasePaymentsRequest]) (*connect.Response[tammyv1.ListPurchasePaymentsResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || h.cursors == nil || r == nil || r.Msg == nil || r.Msg.Authentication == nil || r.Msg.Page == nil {
		return nil, h.failure(purchases.ErrInvalidPage)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, r.Msg.Authentication); err != nil {
		return nil, h.failure(err)
	}
	limit := int(r.Msg.Page.PageSize)
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, h.failure(purchases.ErrInvalidPage)
	}
	// Session binding prevents a token becoming a capability after sign-out.
	queryHash := sha256.Sum256([]byte("tammy.v1.PurchaseService.ListPurchasePayments\x00" + r.Msg.OrganisationId + "\x00" + r.Msg.DocumentId + "\x00" + r.Msg.Authentication.ActorUserId + "\x00" + r.Msg.Authentication.SessionId + "\x00" + strconv.Itoa(limit)))
	position := ""
	if r.Msg.Page.Cursor != nil {
		cursor, err := h.cursors.Decode(*r.Msg.Page.Cursor, queryHash)
		if err != nil || cursor.Snapshot != "purchase-payments-v1" {
			return nil, h.failure(purchases.ErrInvalidPage)
		}
		position = cursor.Position
	}
	encode := func(position string) (string, error) {
		return h.cursors.Encode(paging.Cursor{Snapshot: "purchase-payments-v1", Position: position, QueryHash: queryHash})
	}
	tx, err := h.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, h.failure(err)
	}
	defer tx.Rollback()
	if err := h.identity.ValidateAuthorizationWithin(ctx, tx, r.Msg.Authentication, authorisation.ActionReadFinancial); err != nil {
		return nil, h.failure(err)
	}
	repo, err := purchases.NewRepository(tx, h.now, h.newID)
	if err != nil {
		return nil, h.failure(err)
	}
	page, err := repo.ListPage(ctx, r.Msg.OrganisationId, r.Msg.DocumentId, purchases.PageOptions{Position: position, Limit: limit, EncodeCursor: encode})
	if err != nil {
		return nil, h.failure(err)
	}
	info := &tammyv1.PageInfo{ReturnedCount: uint32(len(page.Payments))}
	if page.NextPosition != "" {
		token, err := encode(page.NextPosition)
		if err != nil {
			return nil, h.failure(purchases.ErrInvalidPage)
		}
		info.NextCursor = &token
	}
	response := &tammyv1.ListPurchasePaymentsResponse{Payments: page.Payments, Page: info}
	if !purchaseFrameFits(response) {
		return nil, h.failure(purchases.ErrSourceBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(response), nil
}
func purchaseFrameFits(message proto.Message) bool {
	frame, err := anypb.New(message)
	return err == nil && proto.Size(frame) <= purchases.MaximumListResponseBytes
}

func (h *purchaseHandler) PostReviewedPurchase(ctx context.Context, r *connect.Request[tammyv1.PostReviewedPurchaseRequest]) (*connect.Response[tammyv1.PostReviewedPurchaseResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	value, err := h.service.PostReviewed(ctx, r.Msg)
	if err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(value), nil
}
func (h *purchaseHandler) RecordPurchasePayment(ctx context.Context, r *connect.Request[tammyv1.RecordPurchasePaymentRequest]) (*connect.Response[tammyv1.RecordPurchasePaymentResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	value, err := h.service.RecordPayment(ctx, r.Msg)
	if err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(value), nil
}
func (h *purchaseHandler) RecordPurchaseTaxEvidence(ctx context.Context, r *connect.Request[tammyv1.RecordPurchaseTaxEvidenceRequest]) (*connect.Response[tammyv1.RecordPurchaseTaxEvidenceResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	value, err := h.service.RecordEvidence(ctx, r.Msg)
	if err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(value), nil
}
func (h *purchaseHandler) AttributePurchaseGst(ctx context.Context, r *connect.Request[tammyv1.AttributePurchaseGstRequest]) (*connect.Response[tammyv1.AttributePurchaseGstResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	value, err := h.service.Attribute(ctx, r.Msg)
	if err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(value), nil
}
func (h *purchaseHandler) DeferPurchaseGstClaim(ctx context.Context, r *connect.Request[tammyv1.DeferPurchaseGstClaimRequest]) (*connect.Response[tammyv1.DeferPurchaseGstClaimResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	value, err := h.service.Defer(ctx, r.Msg)
	if err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(value), nil
}
func (h *purchaseHandler) ResolveLegacyPurchaseReview(ctx context.Context, r *connect.Request[tammyv1.ResolveLegacyPurchaseReviewRequest]) (*connect.Response[tammyv1.ResolveLegacyPurchaseReviewResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, h.failure(purchases.ErrInvalidCommand)
	}
	value, err := h.service.ResolveLegacy(ctx, r.Msg)
	if err != nil {
		return nil, h.failure(err)
	}
	return connect.NewResponse(value), nil
}
func (h *purchaseHandler) failure(err error) error {
	if h == nil {
		return purchaseError(err)
	}
	return purchaseError(err, h.newID)
}

func purchaseError(err error, newID ...func() (string, error)) error {
	code := connect.CodeInternal
	switch {
	case errors.Is(err, context.Canceled):
		code = connect.CodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		code = connect.CodeDeadlineExceeded
	case errors.Is(err, purchases.ErrNotFound), errors.Is(err, accounting.ErrAccountNotFound):
		code = connect.CodeNotFound
	case errors.Is(err, idempotency.ErrConflict):
		code = connect.CodeAlreadyExists
	case errors.Is(err, purchases.ErrInvalidCommand), errors.Is(err, purchases.ErrInvalidPage), errors.Is(err, purchases.ErrInvalidReviewedPurchase), errors.Is(err, purchases.ErrInvalidAllocation), errors.Is(err, purchases.ErrInvalidEvidence), errors.Is(err, purchases.ErrInvalidClaimTiming), errors.Is(err, purchases.ErrInvalidLegacyReview), errors.Is(err, purchases.ErrInvalidDuplicateIdentity), errors.Is(err, purchases.ErrInvalidDuplicateReview), errors.Is(err, purchases.ErrPayment):
		code = connect.CodeInvalidArgument
	case errors.Is(err, app.ErrLocalAuditCoverageUnavailable), errors.Is(err, purchases.ErrConflict), errors.Is(err, purchases.ErrDuplicatePurchase), errors.Is(err, purchases.ErrPriorAccounting), errors.Is(err, purchases.ErrUnsupportedException), errors.Is(err, purchases.ErrPurchaseEligibility), errors.Is(err, purchases.ErrPurchaseAccount), errors.Is(err, purchases.ErrSourceBound), errors.Is(err, purchases.ErrClaimBalance), errors.Is(err, invoices.ErrGSTBasisHistory), errors.Is(err, accounting.ErrClosedPeriod), errors.Is(err, accounting.ErrAccountNotPostable):
		code = connect.CodeFailedPrecondition
	}
	if fault, ok := faults.CodeOf(err); ok {
		switch fault {
		case faults.CodeAuthenticationRequired:
			code = connect.CodeUnauthenticated
		case faults.CodePermissionDenied:
			code = connect.CodePermissionDenied
		}
	}
	stable, summary, remediation := purchaseCorrection(err)
	if stable == "" || len(newID) == 0 || newID[0] == nil {
		return connect.NewError(code, ErrLedgerModule)
	}
	correlation, idErr := newID[0]()
	if idErr != nil || !ids.IsCanonicalV7(correlation) {
		return connect.NewError(code, ErrLedgerModule)
	}
	category := tammyv1.ErrorCategory_ERROR_CATEGORY_VALIDATION
	if code == connect.CodeFailedPrecondition {
		category = tammyv1.ErrorCategory_ERROR_CATEGORY_CONFLICT
	}
	context := &tammyv1.ErrorContext{Code: stable, Category: category, SafeSummary: summary, Remediation: remediation, Retry: tammyv1.RetryClassification_RETRY_CLASSIFICATION_AFTER_USER_ACTION, CorrelationId: correlation}
	result := connect.NewError(code, errors.New(summary))
	detail, detailErr := connect.NewErrorDetail(&tammyv1.ValidationErrorDetail{Context: context})
	if detailErr == nil {
		result.AddDetail(detail)
	}
	return result
}

func purchaseCorrection(err error) (string, string, string) {
	switch {
	case errors.Is(err, app.ErrLocalAuditCoverageUnavailable):
		return "PURCHASE_AUDIT_COVERAGE_UNAVAILABLE", "Purchase recording is unavailable because this workspace has no verified audit coverage.", "Use a workspace with verified audit coverage; this app cannot establish coverage for this workspace."
	case errors.Is(err, purchases.ErrPayment):
		return "PURCHASE_PAYMENT_INVALID", "The payment amount, date or bank account needs review.", "Use a positive AUD amount within the outstanding balance, an actual payment date on or after the invoice, and an active bank account."
	case errors.Is(err, purchases.ErrConflict):
		return "PURCHASE_STALE", "The purchase changed since this review.", "Refresh the purchase and review its current version before submitting again."
	case errors.Is(err, purchases.ErrDuplicatePurchase):
		return "PURCHASE_DUPLICATE", "This source matches an existing purchase.", "Review the existing purchase and retained duplicate evidence before posting."
	case errors.Is(err, purchases.ErrPriorAccounting):
		return "PURCHASE_PRIOR_ACCOUNTING", "This source has existing accounting or claim provenance.", "Review the retained accounting, opening balance or linked purchase before recording another transaction."
	case errors.Is(err, purchases.ErrUnsupportedException):
		return "PURCHASE_EVIDENCE_UNSUPPORTED", "The tax-invoice exception is unsupported.", "Review a tax invoice or the supported whole-transaction rule and owned supporting records."
	case errors.Is(err, accounting.ErrClosedPeriod):
		return "PURCHASE_PERIOD_CLOSED", "The selected posting period is closed.", "Review an allowable open period; preserve the actual transaction dates."
	case errors.Is(err, invoices.ErrGSTBasisHistory):
		return "PURCHASE_GST_BASIS_CHANGED", "The retained GST basis history needs review.", "Review the organisation GST basis and its complete change history before continuing."
	case errors.Is(err, purchases.ErrInvalidClaimTiming):
		return "PURCHASE_CLAIM_TIMING_INVALID", "The selected GST claim timing needs review.", "Check the owned source dates, held evidence, actual required-return due date and supported claim period."
	case errors.Is(err, purchases.ErrClaimBalance):
		return "PURCHASE_CLAIM_BALANCE_UNAVAILABLE", "The selected source has no unclaimed eligible suspense.", "Refresh the retained source and choose eligible suspense that has not already been attributed."
	case errors.Is(err, purchases.ErrPurchaseEligibility):
		return "PURCHASE_ELIGIBILITY_REQUIRED", "The purchase eligibility review is incomplete.", "Confirm registration and business use with retained supporting evidence."
	case errors.Is(err, purchases.ErrPurchaseAccount):
		return "PURCHASE_ACCOUNT_INVALID", "An allocation account is unsuitable.", "Use an active owned expense or asset account for business costs, or a suitable private-use account."
	case errors.Is(err, purchases.ErrSourceBound):
		return "PURCHASE_VOLUME_UNSUPPORTED", "The complete source set exceeds the supported limit.", "Keep the actual reporting period and review the unsupported volume before continuing."
	default:
		return "", "", ""
	}
}

var _ tammyv1connect.PurchaseServiceHandler = (*purchaseHandler)(nil)
