package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func purchaseReportingReplayOwnerGuard(organisationID, documentID string) *postingReportingOwnerGuard {
	guard := purchaseReportingRootOwnerGuard(organisationID, false)
	guard.invalid += ` OR NOT EXISTS(SELECT 1 FROM documents AS owned_document
		WHERE owned_document.id=? AND typeof(owned_document.organisation_id)='text'
		AND owned_document.organisation_id=?)`
	guard.args = append(guard.args, documentID, organisationID)
	return guard
}

// Reporting replays the mature document decoder without first calling the
// ordinary unscoped document Get, which itself selects extracted_text.
func (r *Repository) reportingDocument(ctx context.Context, organisationID, documentID string) (*tammyv1.Document, error) {
	query := postingReportingQuery{
		table: "document replay", alias: "d",
		columns: strings.Fields("id organisation_id version status source_display_name mime_type byte_length sha256 extracted_text supplier_name invoice_number document_date subtotal_minor gst_minor total_minor created_at reviewed_at original_bytes"),
		from:    "documents AS d", where: "d.id=? AND d.organisation_id=?",
		args: []any{documentID, organisationID}, orderBy: "d.id",
		ownerGuard: purchaseReportingReplayOwnerGuard(organisationID, documentID),
	}
	sqlText, args := postingReportingReadSQL(query)
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, errors.Join(ErrRepository, ctx.Err())
	}
	row := &purchaseReportingDocumentRow{rows: rows}
	document, err := documents.ScanRetainedDocument(row)
	if err != nil {
		return nil, errors.Join(ErrRepository, ctx.Err())
	}
	digest := sha256.Sum256(row.originalBytes)
	if !bytes.Equal(digest[:], document.Sha256) || uint64(len(row.originalBytes)) != document.ByteLength {
		return nil, ErrRepository
	}
	return document, nil
}

type purchaseReportingDocumentRow struct {
	rows          *sql.Rows
	originalBytes []byte
}

// Consume the owner sentinel and original bytes around the decoder's 17
// destinations in one scan; the guarded SQL masks all 18 payload cells.
func (row *purchaseReportingDocumentRow) Scan(destinations ...any) error {
	var invalidOwner int64
	values := append([]any{&invalidOwner}, destinations...)
	values = append(values, &row.originalBytes)
	if err := (purchaseRevisionRow{rows: row.rows}).Scan(values...); err != nil || invalidOwner != 0 {
		return ErrRepository
	}
	return nil
}

func purchaseReportingPostingReplayQuery(organisationID, documentID string) postingReportingQuery {
	return postingReportingQuery{
		table: "posting replay", alias: "p",
		columns: []string{"posting_proto", "source_hash", "reviewed_source_proto", "reviewed_snapshot_hash"},
		from:    "purchase_postings_v1 AS p", where: "p.organisation_id=? AND p.document_id=?",
		args: []any{organisationID, documentID}, orderBy: "p.id",
		ownerGuard: purchaseReportingReplayOwnerGuard(organisationID, documentID),
	}
}
