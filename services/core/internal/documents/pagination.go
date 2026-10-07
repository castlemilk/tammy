package documents

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidPage = errors.New("documents: invalid page")

const MaximumListResponseBytes = 4 * 1024 * 1024

// PageOptions contains an internal keyset position. EncodeCursor is supplied by
// the authenticated adapter so the byte budget includes its actual signed token.
// Internal positions must never be returned as transport cursors.
type PageOptions struct {
	Position     string
	Limit        int
	Status       tammyv1.DocumentStatus
	Accounting   tammyv1.DocumentAccountingFilter
	ReviewAsOf   time.Time
	EncodeCursor func(string) (string, error)
}

type DocumentPage struct {
	Documents    []*tammyv1.Document
	NextPosition string
}

// ListPage reads one retained keyset in the caller's encrypted transaction.
// One row of lookahead establishes whether the actual response needs a cursor.
func (repository *Repository) ListPage(ctx context.Context, organisationID string, options PageOptions) (DocumentPage, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		options.Limit < 1 || options.Limit > 200 || options.EncodeCursor == nil ||
		options.Status < tammyv1.DocumentStatus_DOCUMENT_STATUS_UNSPECIFIED || options.Status > tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		options.Accounting < tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_UNSPECIFIED || options.Accounting > tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_NEEDS_GST_REVIEW {
		return DocumentPage{}, ErrInvalidPage
	}
	if options.Accounting == tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_NEEDS_GST_REVIEW && options.ReviewAsOf.IsZero() {
		return DocumentPage{}, ErrInvalidPage
	}
	var createdAt, documentID string
	if options.Position != "" {
		var err error
		createdAt, documentID, err = decodeDocumentPosition(options.Position, organisationID)
		if err != nil {
			return DocumentPage{}, err
		}
		rows, err := repository.executor.QueryContext(ctx, `SELECT created_at FROM documents WHERE organisation_id=? AND id=?`, organisationID, documentID)
		if err != nil {
			return DocumentPage{}, fmt.Errorf("%w: verify page anchor: %v", ErrRepository, err)
		}
		var retained string
		valid := rows.Next() && rows.Scan(&retained) == nil && retained == createdAt
		readErr := rows.Err()
		closeErr := rows.Close()
		if readErr != nil || closeErr != nil {
			return DocumentPage{}, ErrRepository
		}
		if !valid {
			return DocumentPage{}, ErrInvalidPage
		}
	}
	query := documentSelect + ` AS d WHERE d.organisation_id=?`
	arguments := []any{organisationID}
	if options.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_UNSPECIFIED {
		query += ` AND d.status=?`
		status := "NEEDS_REVIEW"
		if options.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED {
			status = "REVIEWED"
		}
		arguments = append(arguments, status)
	}
	switch options.Accounting {
	case tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_UNPOSTED:
		query += ` AND NOT EXISTS (` + ownedPosting + `)`
	case tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_POSTED:
		query += ` AND EXISTS (` + ownedPosting + `)`
	case tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_NEEDS_GST_REVIEW:
		query += ` AND (` + needsGSTReview + `)`
		arguments = append(arguments, options.ReviewAsOf.UTC().Format("2006-01-02"))
	}
	if options.Position != "" {
		anchor, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return DocumentPage{}, ErrInvalidPage
		}
		key := anchor.UTC().Format("2006-01-02T15:04:05.000000000Z")
		query += ` AND (` + documentChronologicalKey + `<? OR (` + documentChronologicalKey + `=? AND d.id<?))`
		arguments = append(arguments, key, key, documentID)
	}
	query += ` ORDER BY ` + documentChronologicalKey + ` DESC,d.id DESC LIMIT ?`
	arguments = append(arguments, options.Limit+1)
	rows, err := repository.executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return DocumentPage{}, fmt.Errorf("%w: list document page: %v", ErrRepository, err)
	}
	defer rows.Close()
	read := func() (*tammyv1.Document, error) {
		if !rows.Next() {
			return nil, rows.Err()
		}
		return scanDocument(rows)
	}
	page := DocumentPage{Documents: make([]*tammyv1.Document, 0, options.Limit)}
	current, err := read()
	if err != nil {
		return DocumentPage{}, fmt.Errorf("%w: read document page: %v", ErrRepository, err)
	}
	for current != nil {
		next, err := read()
		if err != nil {
			return DocumentPage{}, fmt.Errorf("%w: read document page: %v", ErrRepository, err)
		}
		candidate := append(page.Documents, current)
		info := &tammyv1.PageInfo{ReturnedCount: uint32(len(candidate))}
		position := ""
		if next != nil {
			position = encodeDocumentPosition(organisationID, current)
			cursor, err := options.EncodeCursor(position)
			if err != nil || cursor == "" {
				return DocumentPage{}, ErrInvalidPage
			}
			info.NextCursor = &cursor
		}
		if listResponseFrameSize(&tammyv1.ListDocumentsResponse{Documents: candidate, Page: info}) > MaximumListResponseBytes {
			if len(page.Documents) == 0 {
				return DocumentPage{}, ErrRepository
			}
			page.NextPosition = encodeDocumentPosition(organisationID, page.Documents[len(page.Documents)-1])
			return page, nil
		}
		page.Documents = candidate
		page.NextPosition = position
		if len(page.Documents) == options.Limit || next == nil {
			return page, nil
		}
		current = next
	}
	return page, nil
}

// listResponseFrameSize matches the desktop protobuf IPC Any frame, including
// its type URL and both length prefixes. No estimate or base64 size is used.
func listResponseFrameSize(response *tammyv1.ListDocumentsResponse) int {
	const typeURL = "type.googleapis.com/tammy.v1.ListDocumentsResponse"
	responseBytes := proto.Size(response)
	size := protowire.SizeTag(1) + protowire.SizeBytes(len(typeURL))
	if responseBytes > 0 {
		size += protowire.SizeTag(2) + protowire.SizeBytes(responseBytes)
	}
	return size
}

func encodeDocumentPosition(organisationID string, document *tammyv1.Document) string {
	return base64.RawURLEncoding.EncodeToString([]byte("document-page-v1|" + organisationID + "|" + document.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano) + "|" + document.Id))
}

func decodeDocumentPosition(position, organisationID string) (string, string, error) {
	if position == "" || len(position) > 256 {
		return "", "", ErrInvalidPage
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(position)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != position {
		return "", "", ErrInvalidPage
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 || parts[0] != "document-page-v1" || parts[1] != organisationID || !ids.IsCanonicalV7(parts[3]) {
		return "", "", ErrInvalidPage
	}
	instant, err := time.Parse(time.RFC3339Nano, parts[2])
	if err != nil || instant.UTC().Format(time.RFC3339Nano) != parts[2] {
		return "", "", ErrInvalidPage
	}
	return parts[2], parts[3], nil
}

const ownedPosting = `SELECT 1 FROM purchase_postings_v1 p WHERE p.document_id=d.id AND p.organisation_id=d.organisation_id`

// Create retains UTC RFC3339Nano timestamps with variable fractional precision.
// Pad only the query key to nine digits: raw text is not chronological, while
// SQLite date functions round away nanoseconds. Retained metadata is unchanged.
const documentChronologicalKey = `substr(d.created_at,1,19)||'.'||substr(
 CASE WHEN substr(d.created_at,20,1)='.' THEN substr(d.created_at,21,length(d.created_at)-21) ELSE '' END
 ||'000000000',1,9)||'Z'`

// This navigation predicate is deliberately conservative. A global list has
// no BAS period or balance date, so a period-scoped legacy decision cannot hide
// a reviewed unposted source. Eligibility comes only from retained allocations.
// Supported-exception provenance is validated by the purchase domain, not by
// its SQL classification alone, and remains visible here pending that review.
const needsGSTReview = `
 (d.status='REVIEWED' AND NOT EXISTS (` + ownedPosting + `))
 OR EXISTS (
   SELECT 1 FROM purchase_postings_v1 p
   WHERE p.document_id=d.id AND p.organisation_id=d.organisation_id AND (
     (p.eligible_gst_minor>0 AND NOT EXISTS (
       SELECT 1 FROM purchase_tax_evidence_v1 e
       JOIN documents source ON source.id=e.source_document_id AND source.organisation_id=e.organisation_id
       WHERE e.posting_id=p.id AND e.organisation_id=p.organisation_id
         AND e.classification=2 AND e.held_date IS NOT NULL AND e.held_date<=?
         AND source.status='REVIEWED' AND source.version=e.source_version AND source.sha256=e.source_hash
         AND NOT EXISTS (SELECT 1 FROM purchase_tax_evidence_v1 newer
           WHERE newer.posting_id=e.posting_id AND newer.organisation_id=e.organisation_id
             AND (newer.accounting_version>e.accounting_version OR (newer.accounting_version=e.accounting_version AND newer.id>e.id)))
     ))
     OR (
       SELECT COALESCE(SUM(line.debit_minor-line.credit_minor),0)
       FROM journal_lines line JOIN journals journal ON journal.id=line.journal_id
       JOIN accounts account ON account.id=line.account_id AND account.organisation_id=journal.organisation_id
       WHERE account.code='1170' AND journal.organisation_id=p.organisation_id AND journal.state='POSTED'
         AND (
           (journal.id=p.journal_id AND journal.source_kind=6 AND journal.source_id=p.id)
           OR EXISTS (SELECT 1 FROM purchase_payments_v1 payment WHERE payment.posting_id=p.id AND payment.organisation_id=p.organisation_id
             AND payment.journal_id=journal.id AND journal.source_kind=7 AND journal.source_id=payment.id)
           OR EXISTS (SELECT 1 FROM purchase_gst_attributions_v1 attribution WHERE attribution.posting_id=p.id AND attribution.organisation_id=p.organisation_id
             AND attribution.journal_id=journal.id AND journal.source_kind=8 AND journal.source_id=attribution.operation_key)
         )
     )>0
     OR EXISTS (
       SELECT 1 FROM purchase_gst_deferral_portions_v1 portion
       WHERE portion.posting_id=p.id AND portion.organisation_id=p.organisation_id
         AND portion.deferred_gst_minor>(SELECT COALESCE(SUM(attribution.claimed_gst_minor),0) FROM purchase_gst_attributions_v1 attribution
           WHERE attribution.posting_id=portion.posting_id AND attribution.organisation_id=portion.organisation_id
             AND attribution.closes_deferral_id=portion.deferral_id AND attribution.source_kind=portion.source_kind
             AND attribution.payment_id IS portion.payment_id AND attribution.allocation_sequence=portion.allocation_sequence)
     )
   )
 )`
