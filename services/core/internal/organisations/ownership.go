package organisations

import (
	"context"
	"errors"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

var ErrOwnershipImpact = errors.New("organisations: ownership impact failed")

// OwnershipReportImpactPort rejects transfer while report/export work still
// depends on the prior owner or current verification projection.
type OwnershipReportImpactPort interface {
	RequireResolvedOwnershipImpact(context.Context, workspace.MutationExecutor, string) error
}

type OwnershipService struct {
	Reports OwnershipReportImpactPort
	Clock   func() time.Time
}

// ApplyOwnershipTransfer joins the Workspace-owned outer transaction. Session
// invalidation is deliberately performed by Workspace immediately after this
// port succeeds, using the same executor.
func (service OwnershipService) ApplyOwnershipTransfer(
	ctx context.Context,
	executor workspace.MutationExecutor,
	impact workspace.OwnershipImpact,
) error {
	if ctx == nil || executor == nil || service.Reports == nil || service.Clock == nil ||
		!ids.IsCanonicalV7(impact.WorkspaceID) || !ids.IsCanonicalV7(impact.PriorOwnerUserID) ||
		!ids.IsCanonicalV7(impact.NextOwnerUserID) || impact.PriorOwnerUserID == impact.NextOwnerUserID ||
		!impact.AcknowledgeVerificationEffect {
		return ErrOwnershipImpact
	}
	if err := service.Reports.RequireResolvedOwnershipImpact(ctx, executor, impact.WorkspaceID); err != nil {
		return err
	}
	repository, err := NewRepository(executor)
	if err != nil {
		return err
	}
	profile, err := repository.GetSole(ctx)
	if err != nil {
		return err
	}
	if profile.OwnerUserId != impact.PriorOwnerUserID {
		return ErrRepositoryConflict
	}
	profile.OwnerUserId = impact.NextOwnerUserID
	profile.Version++
	if profile.VerificationState == tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED {
		profile.VerificationState = tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED
	}
	return repository.Update(ctx, profile.Version-1, profile, service.Clock())
}
