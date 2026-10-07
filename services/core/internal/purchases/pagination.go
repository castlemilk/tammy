package purchases

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidPage = errors.New("purchases: invalid payment page")

const MaximumListResponseBytes = 4 * 1024 * 1024

// PageOptions carries an internal retained keyset. The authenticated adapter
// binds and signs transport tokens; its actual token is included in the budget.
type PageOptions struct {
	Position     string
	Limit        int
	EncodeCursor func(string) (string, error)
}

type PaymentPage struct {
	Payments     []*tammyv1.PurchasePayment
	NextPosition string
}

func (r *Repository) ListPage(ctx context.Context, org, documentID string, options PageOptions) (PaymentPage, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(documentID) || options.Limit < 1 || options.Limit > 200 || options.EncodeCursor == nil {
		return PaymentPage{}, ErrInvalidPage
	}
	// This verifies every retained source/event before exposing any history.
	// The accounting aggregation is independent of the page limit and keyset.
	state, err := r.Get(ctx, org, documentID)
	if err != nil {
		return PaymentPage{}, err
	}
	page := PaymentPage{Payments: make([]*tammyv1.PurchasePayment, 0, options.Limit)}
	if !state.Posted {
		if options.Position != "" {
			return PaymentPage{}, ErrInvalidPage
		}
		return page, nil
	}
	posting := state.Posting
	var stamp, paymentID string
	if options.Position != "" {
		stamp, paymentID, err = decodePaymentPosition(options.Position, org, documentID, posting.Id)
		if err != nil {
			return PaymentPage{}, err
		}
		rows, err := r.executor.QueryContext(ctx, `SELECT created_at FROM purchase_payments_v1 WHERE id=? AND organisation_id=? AND posting_id=?`, paymentID, org, posting.Id)
		if err != nil {
			return PaymentPage{}, ErrRepository
		}
		var retained string
		if (purchaseRevisionRow{rows: rows}).Scan(&retained) != nil || retained != stamp {
			return PaymentPage{}, ErrInvalidPage
		}
	}
	query := `SELECT p.id,p.accounting_version,p.payment_date,p.amount_minor,p.currency_code,p.bank_account_id,p.journal_id,p.reference,p.payment_proto,p.source_hash,p.actor_user_id,p.created_at FROM purchase_payments_v1 p WHERE p.organisation_id=? AND p.posting_id=?`
	args := []any{org, posting.Id}
	if options.Position != "" {
		instant, _ := time.Parse(time.RFC3339Nano, stamp)
		key := instant.UTC().Format("2006-01-02T15:04:05.000000000Z")
		query += ` AND (` + paymentChronologicalKey + `<? OR (` + paymentChronologicalKey + `=? AND p.id<?))`
		args = append(args, key, key, paymentID)
	}
	query += ` ORDER BY ` + paymentChronologicalKey + ` DESC,p.id DESC LIMIT ?`
	args = append(args, options.Limit+1)
	rows, err := r.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return PaymentPage{}, ErrRepository
	}
	defer rows.Close()
	read := func() (*tammyv1.PurchasePayment, error) {
		if !rows.Next() {
			return nil, rows.Err()
		}
		return scanPayment(rows, posting)
	}
	current, err := read()
	if err != nil {
		return PaymentPage{}, err
	}
	for current != nil {
		next, err := read()
		if err != nil {
			return PaymentPage{}, err
		}
		candidate := append(page.Payments, current)
		info := &tammyv1.PageInfo{ReturnedCount: uint32(len(candidate))}
		position := ""
		if next != nil {
			position = encodePaymentPosition(current)
			token, err := options.EncodeCursor(position)
			if err != nil || token == "" {
				return PaymentPage{}, ErrInvalidPage
			}
			info.NextCursor = &token
		}
		if paymentResponseFrameSize(&tammyv1.ListPurchasePaymentsResponse{Payments: candidate, Page: info}) > MaximumListResponseBytes {
			if len(page.Payments) == 0 {
				return PaymentPage{}, ErrRepository
			}
			page.NextPosition = encodePaymentPosition(page.Payments[len(page.Payments)-1])
			return page, nil
		}
		page.Payments = candidate
		page.NextPosition = position
		if len(page.Payments) == options.Limit || next == nil {
			return page, nil
		}
		current = next
	}
	return page, nil
}

func paymentResponseFrameSize(response *tammyv1.ListPurchasePaymentsResponse) int {
	const typeURL = "type.googleapis.com/tammy.v1.ListPurchasePaymentsResponse"
	content := proto.Size(response)
	size := protowire.SizeTag(1) + protowire.SizeBytes(len(typeURL))
	if content > 0 {
		size += protowire.SizeTag(2) + protowire.SizeBytes(content)
	}
	return size
}

func encodePaymentPosition(payment *tammyv1.PurchasePayment) string {
	return base64.RawURLEncoding.EncodeToString([]byte("purchase-payment-page-v1|" + payment.OrganisationId + "|" + payment.DocumentId + "|" + payment.PostingId + "|" + payment.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) + "|" + payment.Id))
}

func decodePaymentPosition(position, org, document, posting string) (string, string, error) {
	if position == "" || len(position) > 384 {
		return "", "", ErrInvalidPage
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(position)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != position {
		return "", "", ErrInvalidPage
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 6 || parts[0] != "purchase-payment-page-v1" || parts[1] != org || parts[2] != document || parts[3] != posting || !ids.IsCanonicalV7(parts[5]) {
		return "", "", ErrInvalidPage
	}
	instant, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil || instant.UTC().Format(time.RFC3339Nano) != parts[4] {
		return "", "", ErrInvalidPage
	}
	return parts[4], parts[5], nil
}

// RFC3339Nano retains variable fractional precision. Pad only the query key so
// chronology is exact within the same second without changing retained bytes.
const paymentChronologicalKey = `substr(p.created_at,1,19)||'.'||substr(
 CASE WHEN substr(p.created_at,20,1)='.' THEN substr(p.created_at,21,length(p.created_at)-21) ELSE '' END
 ||'000000000',1,9)||'Z'`
