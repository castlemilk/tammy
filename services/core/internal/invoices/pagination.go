package invoices

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

// ListPage uses the immutable creation timestamp and invoice ID as a keyset.
// Cursors carry their organisation and must reference its retained anchor,
// preventing a foreign organisation's cursor from selecting an invoice page.
func (r *Repository) ListPage(ctx context.Context, org, cursor string, limit int) ([]*tammyv1.Invoice, string, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || limit < 1 || limit > 200 {
		return nil, "", ErrInvalidDraft
	}
	afterDate, afterID := "", ""
	if cursor != "" {
		var err error
		afterDate, afterID, err = decodeInvoiceCursor(org, cursor)
		if err != nil {
			return nil, "", err
		}
		rows, err := r.executor.QueryContext(ctx, `SELECT created_at FROM invoices_v1 WHERE organisation_id=? AND id=?`, org, afterID)
		if err != nil {
			return nil, "", ErrRepository
		}
		var retained string
		if !rows.Next() {
			finish := rows.Err()
			_ = rows.Close()
			if finish != nil {
				return nil, "", ErrRepository
			}
			return nil, "", ErrInvalidDraft
		}
		if rows.Scan(&retained) != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			return nil, "", ErrRepository
		}
		if rows.Close() != nil {
			return nil, "", ErrRepository
		}
		if retained != afterDate {
			return nil, "", ErrInvalidDraft
		}
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT id,organisation_id,version,state,number,invoice_proto FROM invoices_v1 WHERE organisation_id=? AND (?='' OR created_at<? OR (created_at=? AND id<?)) ORDER BY created_at DESC,id DESC LIMIT ?`, org, afterDate, afterDate, afterDate, afterID, limit+1)
	if err != nil {
		return nil, "", ErrRepository
	}
	defer rows.Close()
	result := make([]*tammyv1.Invoice, 0, limit)
	const maximumInvoicePageBytes = 512 * 1024
	var pageBytes int
	hasMore := false
	for rows.Next() {
		value, err := scanInvoice(rows)
		if err != nil {
			return nil, "", err
		}
		encodedSize := proto.Size(value) + 16
		if len(result) >= limit || len(result) > 0 && pageBytes+encodedSize > maximumInvoicePageBytes {
			hasMore = true
			break
		}
		result = append(result, value)
		pageBytes += encodedSize
	}
	if rows.Err() != nil {
		return nil, "", ErrRepository
	}
	next := ""
	if hasMore {
		last := result[len(result)-1]
		next = encodeInvoiceCursor(org, last.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano), last.Id)
	}
	return result, next, nil
}
func encodeInvoiceCursor(org, created, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("invoice-page-v1|" + org + "|" + created + "|" + id))
}
func decodeInvoiceCursor(org, cursor string) (string, string, error) {
	if len(cursor) > 256 {
		return "", "", ErrInvalidDraft
	}
	content, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(content) != cursor {
		return "", "", ErrInvalidDraft
	}
	parts := strings.Split(string(content), "|")
	if len(parts) != 4 || parts[0] != "invoice-page-v1" || parts[1] != org || !ids.IsCanonicalV7(parts[3]) {
		return "", "", ErrInvalidDraft
	}
	created, err := time.Parse(time.RFC3339Nano, parts[2])
	if err != nil || created.UTC().Format(time.RFC3339Nano) != parts[2] {
		return "", "", ErrInvalidDraft
	}
	return parts[2], parts[3], nil
}
