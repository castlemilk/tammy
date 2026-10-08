package purchases

import (
	"context"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RecordEvidence retains a source-linked review. It cannot change allocation
// amounts or move an earlier credit from suspense; release is a separate command.
func (r *Repository) RecordEvidence(ctx context.Context, request *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error) {
	if r == nil || r.executor == nil || ctx == nil || request == nil || request.CommandContext == nil || request.CommandContext.Authentication == nil || !ids.IsCanonicalV7(request.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.CommandContext.Authentication.ActorUserId) || request.ExpectedAccountingVersion == 0 || request.ExpectedAccountingVersion > math.MaxInt64 {
		return nil, ErrInvalidEvidence
	}
	now := r.now().UTC()
	if now.IsZero() {
		return nil, ErrRepository
	}
	state, err := r.getAt(ctx, request.OrganisationId, request.DocumentId, false, now)
	if err != nil {
		return nil, err
	}
	if !state.Posted || state.Posting == nil {
		return nil, ErrConflict
	}
	if state.AccountingVersion != request.ExpectedAccountingVersion || state.AccountingVersion >= math.MaxInt64 {
		return nil, ErrConflict
	}
	posting := state.Posting
	if _, err := r.ResolveEvidenceRule(ctx, posting.OrganisationId, posting.DocumentId, request.Evidence); err != nil {
		return nil, err
	}
	if err := r.verifyEvidenceSource(ctx, posting.OrganisationId, request.Evidence); err != nil {
		return nil, err
	}
	profiles, _ := organisations.NewRepository(r.executor)
	profile, err := profiles.Get(ctx, posting.OrganisationId)
	if err != nil {
		return nil, err
	}
	if profile.GstBasis != posting.GstBasis {
		return nil, invoices.ErrGSTBasisHistory
	}
	if _, err := invoices.CheckGSTBasisHistory(ctx, r.executor, posting.OrganisationId, posting.GstHistoryWatermark); err != nil {
		return nil, err
	}
	if _, err := r.evidenceOutcomes(ctx, posting, request.Evidence, now); err != nil {
		return nil, err
	}
	id, err := r.newID()
	if err != nil || !ids.IsCanonicalV7(id) {
		return nil, ErrRepository
	}
	revision, err := ReservePurchaseRevision(ctx, r.executor, request.CommandContext.IdempotencyKey, purchaseService+"RecordPurchaseTaxEvidence", false, now)
	if err != nil {
		return nil, err
	}
	evidence := &tammyv1.PurchaseTaxEvidence{Id: id, OrganisationId: posting.OrganisationId, DocumentId: posting.DocumentId, AccountingVersion: state.AccountingVersion + 1, Review: proto.Clone(request.Evidence).(*tammyv1.PurchaseEvidenceInput), ActorUserId: request.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
	if err := r.retainEvidence(ctx, posting.Id, evidence); err != nil {
		return nil, err
	}
	state.AccountingVersion = evidence.AccountingVersion
	if err := r.refreshClaimOutcomes(ctx, state, now); err != nil {
		return nil, err
	}
	response := &tammyv1.RecordPurchaseTaxEvidenceResponse{Evidence: evidence, Accounting: state}
	if err := RecordAccountingEvent(ctx, r.executor, AccountingEvent{OperationKey: request.CommandContext.IdempotencyKey, OrganisationID: posting.OrganisationId, ScopeKind: "POSTING", ScopeID: posting.Id, CommandType: purchaseService + "RecordPurchaseTaxEvidence", AccountingVersion: evidence.AccountingVersion, Revision: revision, Result: response, Now: now}); err != nil {
		return nil, err
	}
	return response, nil
}
