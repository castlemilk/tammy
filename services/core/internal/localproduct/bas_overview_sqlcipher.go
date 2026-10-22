//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/overview"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
)

// basOverviewProjector shares the BAS reader cursor domain used by the normal
// local BAS handlers. It has no database handle; every query is supplied the
// overview port's already-open read transaction.
type basOverviewProjector struct {
	cursors *paging.Codec
}

func newBASOverviewProjector(workspaceID string, installationKey []byte) (*basOverviewProjector, error) {
	if !ids.IsCanonicalV7(workspaceID) || len(installationKey) < sha256.Size {
		return nil, ErrLedgerModule
	}
	mac := hmac.New(sha256.New, installationKey)
	_, _ = mac.Write([]byte("tammy.bas-preparation-page.v1\x00" + workspaceID))
	cursors, err := paging.NewCodec(mac.Sum(nil))
	if err != nil {
		return nil, errors.Join(ErrLedgerModule, err)
	}
	return &basOverviewProjector{cursors: cursors}, nil
}

func (projector *basOverviewProjector) ProjectBASAttention(ctx context.Context, executor overview.SnapshotExecutor,
	organisationID string, period *tammyv1.ReportingPeriod) (overview.BASAttentionProjection, error) {
	if projector == nil || projector.cursors == nil || ctx == nil || ctx.Err() != nil || executor == nil ||
		!ids.IsCanonicalV7(organisationID) {
		return overview.BASAttentionProjection{}, ErrLedgerModule
	}
	current, found, err := currentBASPeriodForDates(ctx, executor, projector.cursors, organisationID, period)
	if err != nil {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err, ctx.Err())
	}
	if !found {
		return overview.BASAttentionProjection{Status: tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED}, nil
	}
	scope, err := baspreparation.FamilyScopeFromPeriod(current)
	if err != nil || scope.OrganisationID != organisationID {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err)
	}
	families, err := baspreparation.NewFamilyIdentityRepository(executor)
	if err != nil {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err)
	}
	identity, found, err := families.FindByScope(ctx, scope)
	if err != nil {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err, ctx.Err())
	}
	if !found {
		return overview.BASAttentionProjection{Status: tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_NOT_CREATED}, nil
	}
	reader, err := baspreparation.NewBASExternalHistoryReader(executor, projector.cursors)
	if err != nil {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err)
	}
	familyRef := &tammyv1.SourceRef{Type: "bas_family", Id: identity.FamilyId, Revision: 1,
		ContentHash: append([]byte(nil), identity.ContentHash...)}
	history, err := reader.Read(ctx, organisationID, familyRef)
	if err != nil {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err, ctx.Err())
	}
	preparations, err := baspreparation.NewPreparationRepository(executor, projector.cursors)
	if err != nil {
		return overview.BASAttentionProjection{}, err
	}
	root, err := preparations.GetCurrent(ctx, organisationID, history.Chain.RootPreparation.PreparationId)
	if err != nil || root == nil {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err)
	}
	projection, err := projectBASHistory(history, organisationID, root.Preparation)
	if err != nil {
		return overview.BASAttentionProjection{}, errors.Join(ErrLedgerModule, err)
	}
	return projection, nil
}

// currentBASPeriodForDates performs a fixed, date-exact identity selector first,
// rejects ambiguity rather than inferring a window, then delegates current-head,
// hash, protobuf and owner verification to PeriodRepository.GetCurrent.
func currentBASPeriodForDates(ctx context.Context, executor overview.SnapshotExecutor, cursors *paging.Codec,
	organisationID string, requested *tammyv1.ReportingPeriod) (*tammyv1.BasConfirmedPeriod, bool, error) {
	if ctx == nil || ctx.Err() != nil || executor == nil || cursors == nil || !ids.IsCanonicalV7(organisationID) ||
		requested == nil {
		return nil, false, ErrLedgerModule
	}
	start, ok := overviewBASCivilDateSQL(requested.StartDate)
	if !ok {
		return nil, false, ErrLedgerModule
	}
	end, ok := overviewBASCivilDateSQL(requested.EndDate)
	if !ok || start > end {
		return nil, false, ErrLedgerModule
	}
	rows, err := executor.QueryContext(ctx, `SELECT
		CASE WHEN typeof(period_id)='text' AND length(period_id)<=128 THEN period_id END,
		COALESCE(typeof(period_id),'null'), COALESCE(length(period_id),0)
		FROM bas_periods_v1
		WHERE organisation_id=? AND period_start=? AND period_end=?
		ORDER BY period_id LIMIT 2`, organisationID, start, end)
	if err != nil {
		return nil, false, errors.Join(ErrLedgerModule, err)
	}
	periodIDs := make([]string, 0, 2)
	for rows.Next() {
		var id sql.NullString
		var storageType string
		var storageLength int64
		if err := rows.Scan(&id, &storageType, &storageLength); err != nil {
			return nil, false, errors.Join(ErrLedgerModule, err, rows.Close())
		}
		if storageType != "text" || !id.Valid || storageLength <= 0 || storageLength > 128 || !ids.IsCanonicalV7(id.String) {
			return nil, false, errors.Join(ErrLedgerModule, rows.Close())
		}
		periodIDs = append(periodIDs, id.String)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, false, errors.Join(ErrLedgerModule, err)
	}
	if len(periodIDs) == 0 {
		return nil, false, nil
	}
	if len(periodIDs) != 1 {
		return nil, false, ErrLedgerModule
	}
	periods, err := baspreparation.NewPeriodRepository(executor, cursors)
	if err != nil {
		return nil, false, errors.Join(ErrLedgerModule, err)
	}
	current, err := periods.GetCurrent(ctx, organisationID, periodIDs[0])
	if err != nil || current == nil || current.OrganisationId != organisationID || current.PeriodId != periodIDs[0] ||
		current.Confirmation == nil || current.Confirmation.Period == nil {
		return nil, false, errors.Join(ErrLedgerModule, err)
	}
	bodyStart, startOK := overviewBASCivilDateSQL(current.Confirmation.Period.StartDate)
	bodyEnd, endOK := overviewBASCivilDateSQL(current.Confirmation.Period.EndDate)
	if !startOK || !endOK || bodyStart != start || bodyEnd != end {
		return nil, false, ErrLedgerModule
	}
	return current, true, nil
}

func overviewBASCivilDateSQL(value *tammyv1.CivilDate) (string, bool) {
	if value == nil || value.Year < 1 || value.Year > 9999 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return "", false
	}
	date := time.Date(int(value.Year), time.Month(value.Month), int(value.Day), 0, 0, 0, 0, time.UTC)
	if date.Year() != int(value.Year) || int(date.Month()) != int(value.Month) || date.Day() != int(value.Day) {
		return "", false
	}
	return fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day), true
}

func projectBASHistory(history baspreparation.ExternalHistoryInput, organisationID string, currentRoot *tammyv1.BasPreparation) (overview.BASAttentionProjection, error) {
	chain := history.Chain
	if chain.Family == nil || chain.Family.Reference == nil || chain.Family.Scope.OrganisationID != organisationID ||
		chain.RootPreparation == nil || chain.RootPreparation.OrganisationId != organisationID {
		return overview.BASAttentionProjection{}, ErrLedgerModule
	}
	family := chain.Family.Reference
	if family.Type != "bas_family" || !ids.IsCanonicalV7(family.Id) || family.Revision != 1 || len(family.ContentHash) != sha256.Size {
		return overview.BASAttentionProjection{}, ErrLedgerModule
	}

	selected := (*tammyv1.BasPreparation)(nil)
	status := tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED
	var frozen *tammyv1.SourceRef
	switch chain.DisputeOverlay {
	case tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED:
		// A dispute is about the retained confirmed head. The reader has already
		// reconciled its entire external-history and frozen proof.
		selected = chain.LatestHead
		status = tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_DISPUTED
		if selected == nil {
			return overview.BASAttentionProjection{}, ErrLedgerModule
		}
		frozen = selected.FrozenBas
	case tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED:
		// The corrective branch is the current action; do not link the old frozen
		// handoff as if it were the active preparation.
		if chain.Branch == nil {
			return overview.BASAttentionProjection{}, ErrLedgerModule
		}
		selected = chain.Branch.ActiveAttempt
		if selected == nil {
			selected = chain.LatestHead
		} else {
			status, frozen = overviewBASAttemptState(selected)
		}
	case tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE,
		tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD:
		if chain.Branch != nil {
			selected = chain.Branch.ActiveAttempt
		}
		if selected == nil {
			selected = chain.LatestHead
		}
		if selected == nil {
			selected = currentRoot
		}
		if selected == nil {
			return overview.BASAttentionProjection{}, ErrLedgerModule
		}
		status, frozen = overviewBASAttemptState(selected)
	default:
		return overview.BASAttentionProjection{}, ErrLedgerModule
	}
	if selected == nil || !ids.IsCanonicalV7(selected.PreparationId) || selected.Version == 0 || len(selected.ContentHash) != sha256.Size ||
		selected.OrganisationId != organisationID || selected.Family == nil || selected.Family.Type != family.Type ||
		selected.Family.Id != family.Id || selected.Family.Revision != family.Revision ||
		!bytes.Equal(selected.Family.ContentHash, family.ContentHash) {
		return overview.BASAttentionProjection{}, ErrLedgerModule
	}
	preparation := &tammyv1.SourceRef{Type: "bas_preparation", Id: selected.PreparationId,
		Revision: selected.Version, ContentHash: append([]byte(nil), selected.ContentHash...)}
	if frozen != nil && (frozen.Type != "bas_frozen" || !ids.IsCanonicalV7(frozen.Id) || frozen.Revision != 1 || len(frozen.ContentHash) != sha256.Size) {
		return overview.BASAttentionProjection{}, ErrLedgerModule
	}
	result := overview.BASAttentionProjection{Status: status,
		Preparation: preparation, Family: proto.Clone(family).(*tammyv1.SourceRef)}
	if frozen != nil {
		result.Frozen = proto.Clone(frozen).(*tammyv1.SourceRef)
	}
	return result, nil
}

func overviewBASAttemptState(preparation *tammyv1.BasPreparation) (tammyv1.BasAttentionStatus, *tammyv1.SourceRef) {
	switch preparation.State {
	case tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY:
		return tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_REVIEW_READY, nil
	case tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF:
		return tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_FROZEN_FOR_MANUAL_HANDOFF, preparation.FrozenBas
	case tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED:
		return tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_EXTERNAL_CONFIRMATION_RECORDED, preparation.FrozenBas
	default:
		return tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED, nil
	}
}
