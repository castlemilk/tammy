//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package overview

import (
	"context"
	"database/sql"
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

// SQLCipherSnapshotPort reads the complete attention projection through one
// read-only encrypted transaction. Modules not installed in the current slice
// report their honest zero state.
type SQLCipherSnapshotPort struct {
	database    *sqlcipher.Database
	workspaceID string
}

func NewSQLCipherSnapshotPort(database *sqlcipher.Database, workspaceID string) (*SQLCipherSnapshotPort, error) {
	if database == nil || !ids.IsCanonicalV7(workspaceID) {
		return nil, ErrOverview
	}
	return &SQLCipherSnapshotPort{database: database, workspaceID: workspaceID}, nil
}

func (port *SQLCipherSnapshotPort) Attention(ctx context.Context, organisationID string) (Snapshot, error) {
	if port == nil || port.database == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return Snapshot{}, ErrOverview
	}
	tx, err := port.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM organisations ORDER BY id LIMIT 2`)
	if err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	var retainedOrganisationID string
	if rows.Next() {
		if rows.Scan(&retainedOrganisationID) != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			return Snapshot{}, ErrOverview
		}
	}
	if err := rows.Close(); err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	if retainedOrganisationID == "" {
		if organisationID != port.workspaceID {
			return Snapshot{}, ErrOverview
		}
	} else if organisationID != retainedOrganisationID {
		return Snapshot{}, ErrOverview
	}

	var revisions RevisionVector
	if err := tx.QueryRowContext(ctx, `
		SELECT financial_revision, ledger_revision, settlement_revision,
		       banking_revision, tax_source_revision,
		       organisation_profile_revision, rule_bundle_revision
		FROM financial_revisions WHERE id = 1`).Scan(
		&revisions.Financial, &revisions.Ledger, &revisions.Settlement,
		&revisions.Banking, &revisions.TaxSource, &revisions.Organisation, &revisions.Rules,
	); err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	var documentsNeedingReview, documentsReviewed uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT
			coalesce(sum(CASE WHEN status = 'NEEDS_REVIEW' THEN 1 ELSE 0 END), 0),
			coalesce(sum(CASE WHEN status = 'REVIEWED' THEN 1 ELSE 0 END), 0)
		FROM documents WHERE organisation_id = ?`, organisationID).Scan(
		&documentsNeedingReview, &documentsReviewed,
	); err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	documentRows, err := tx.QueryContext(ctx, `
		SELECT id, version, sha256, source_display_name
		FROM documents
		WHERE organisation_id = ? AND status = 'NEEDS_REVIEW'
		ORDER BY created_at DESC, id DESC LIMIT 8`, organisationID)
	if err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	items := make([]*tammyv1.AttentionItem, 0, 8)
	for documentRows.Next() {
		item := &tammyv1.AttentionItem{Kind: tammyv1.AttentionItemKind_ATTENTION_ITEM_KIND_DOCUMENT_REVIEW,
			Resource: &tammyv1.SourceRef{Type: "document"}}
		if err := documentRows.Scan(&item.Resource.Id, &item.Resource.Revision, &item.Resource.ContentHash, &item.Label); err != nil {
			_ = documentRows.Close()
			return Snapshot{}, errors.Join(ErrOverview, err)
		}
		items = append(items, item)
	}
	if err := documentRows.Err(); err != nil {
		_ = documentRows.Close()
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	if err := documentRows.Close(); err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, errors.Join(ErrOverview, err)
	}
	return Snapshot{
		DocumentsNeedingReview: documentsNeedingReview,
		DocumentsReviewed:      documentsReviewed,
		BASStatus:              tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_NOT_CREATED,
		Items:                  items,
		Revisions:              revisions,
	}, nil
}
