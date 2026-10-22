package overview

import (
	"context"
	"database/sql"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// SnapshotExecutor is the bounded query capability of the transaction already
// opened by SQLCipherSnapshotPort. It deliberately has no commit/rollback API.
type SnapshotExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type BASAttentionProjection struct {
	Status      tammyv1.BasAttentionStatus
	Preparation *tammyv1.SourceRef
	Family      *tammyv1.SourceRef
	Frozen      *tammyv1.SourceRef
}

// BASAttentionProjector runs inside the overview port's existing read
// transaction. Implementations must return exact retained identities or error.
type BASAttentionProjector interface {
	ProjectBASAttention(context.Context, SnapshotExecutor, string, *tammyv1.ReportingPeriod) (BASAttentionProjection, error)
}

// validateBASAttentionProjection mirrors the public overview response CEL for
// this subprojection and additionally checks exact SourceRef framing before
// refs are committed to the aggregate response.
func validateBASAttentionProjection(value BASAttentionProjection) bool {
	if value.Status < tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_NOT_CREATED ||
		value.Status > tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_DISPUTED {
		return false
	}
	valid := func(ref *tammyv1.SourceRef, typ string, family bool) bool {
		if ref == nil || ref.Type != typ || !ids.IsCanonicalV7(ref.Id) || ref.Revision == 0 || len(ref.ContentHash) != 32 {
			return false
		}
		if family && ref.Revision != 1 {
			return false
		}
		return true
	}
	if value.Preparation != nil && !valid(value.Preparation, "bas_preparation", false) {
		return false
	}
	if value.Family != nil && !valid(value.Family, "bas_family", true) {
		return false
	}
	if value.Frozen != nil && !valid(value.Frozen, "bas_frozen", true) {
		return false
	}
	if value.Status == tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_REVIEW_READY &&
		(value.Preparation == nil || value.Family == nil) {
		return false
	}
	frozenStatus := value.Status == tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_FROZEN_FOR_MANUAL_HANDOFF ||
		value.Status == tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_EXTERNAL_CONFIRMATION_RECORDED ||
		value.Status == tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_DISPUTED
	if frozenStatus && (value.Preparation == nil || value.Family == nil || value.Frozen == nil) {
		return false
	}
	if value.Frozen != nil && !frozenStatus {
		return false
	}
	return true
}
