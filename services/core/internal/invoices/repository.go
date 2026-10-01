package invoices

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var (
	ErrRepository     = errors.New("invoices: repository failure")
	ErrNotFound       = errors.New("invoices: not found")
	ErrConflict       = errors.New("invoices: stale version or issued document")
	ErrNumberConflict = errors.New("invoices: invoice number already used")
	ErrSourceDocument = errors.New("invoices: source document must be reviewed in this organisation")
)

type Repository struct{ executor app.CommandSQLExecutor }

func NewRepository(executor app.CommandSQLExecutor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	return &Repository{executor}, nil
}
func (r *Repository) Save(ctx context.Context, v *tammyv1.Invoice, expected uint64) error {
	if r == nil || r.executor == nil || ctx == nil || !validInvoice(v) || v.Version != expected+1 || (expected == 0 && v.State != tammyv1.InvoiceState_INVOICE_STATE_DRAFT) {
		return ErrRepository
	}
	if err := r.requireLogo(ctx, v.OrganisationId, v.LogoAssetId); err != nil {
		return err
	}
	for _, line := range v.Draft.Lines {
		if line.SourceDocumentId == nil {
			continue
		}
		rows, err := r.executor.QueryContext(ctx, `SELECT status FROM documents WHERE id=? AND organisation_id=?`, *line.SourceDocumentId, v.OrganisationId)
		if err != nil {
			return ErrRepository
		}
		var status string
		found := rows.Next()
		if found {
			err = rows.Scan(&status)
		}
		readErr := rows.Err()
		_ = rows.Close()
		if err != nil || readErr != nil {
			return ErrRepository
		}
		if !found || status != "REVIEWED" {
			return ErrSourceDocument
		}
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(v)
	if err != nil || len(encoded) > 131072 {
		return ErrRepository
	}
	var affected int64
	if expected == 0 {
		result, e := r.executor.ExecContext(ctx, `INSERT INTO invoices_v1(id,organisation_id,version,state,number,invoice_proto,created_at,updated_at) VALUES (?,?,1,'DRAFT',?,?,?,?)`, v.Id, v.OrganisationId, v.Draft.Number, encoded, v.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano), v.UpdatedAt.AsTime().UTC().Format(time.RFC3339Nano))
		err = e
		if err == nil {
			affected, err = result.RowsAffected()
		}
	} else {
		state := "DRAFT"
		if v.State == tammyv1.InvoiceState_INVOICE_STATE_ISSUED {
			state = "ISSUED"
		}
		result, e := r.executor.ExecContext(ctx, `UPDATE invoices_v1 SET version=?,state=?,number=?,invoice_proto=?,updated_at=? WHERE id=? AND organisation_id=? AND version=? AND state='DRAFT'`, v.Version, state, v.Draft.Number, encoded, v.UpdatedAt.AsTime().UTC().Format(time.RFC3339Nano), v.Id, v.OrganisationId, expected)
		err = e
		if err == nil {
			affected, err = result.RowsAffected()
		}
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "invoices_v1.organisation_id, invoices_v1.number") {
			return ErrNumberConflict
		}
		return ErrRepository
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}
func (r *Repository) Get(ctx context.Context, org, id string) (*tammyv1.Invoice, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(id) {
		return nil, ErrRepository
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT id,organisation_id,version,state,number,invoice_proto FROM invoices_v1 WHERE id=? AND organisation_id=?`, id, org)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, ErrRepository
		}
		return nil, ErrNotFound
	}
	v, err := scanInvoice(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return nil, ErrRepository
	}
	return v, nil
}
func (r *Repository) List(ctx context.Context, org string, limit int) ([]*tammyv1.Invoice, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || limit < 1 || limit > 200 {
		return nil, ErrRepository
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT id,organisation_id,version,state,number,invoice_proto FROM invoices_v1 WHERE organisation_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, org, limit)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	result := make([]*tammyv1.Invoice, 0, limit)
	for rows.Next() {
		v, err := scanInvoice(rows)
		if err != nil {
			return nil, ErrRepository
		}
		result = append(result, v)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return result, nil
}
func scanInvoice(row interface{ Scan(...any) error }) (*tammyv1.Invoice, error) {
	var id, org, state, number string
	var version uint64
	var encoded []byte
	if err := row.Scan(&id, &org, &version, &state, &number, &encoded); err != nil {
		return nil, ErrRepository
	}
	v := &tammyv1.Invoice{}
	if proto.Unmarshal(encoded, v) != nil || !validInvoice(v) || v.Id != id || v.OrganisationId != org || v.Version != version || v.Draft.Number != number || v.State.String() != "INVOICE_STATE_"+state {
		return nil, ErrRepository
	}
	return v, nil
}
func validInvoice(v *tammyv1.Invoice) bool {
	if v == nil || !ids.IsCanonicalV7(v.Id) || !ids.IsCanonicalV7(v.OrganisationId) || v.Version < 1 || !text(v.SellerName, 256, true) || organisations.ValidateABN(v.SellerAbn) != nil || v.CreatedAt == nil || v.CreatedAt.CheckValid() != nil || v.UpdatedAt == nil || v.UpdatedAt.CheckValid() != nil || v.UpdatedAt.AsTime().Before(v.CreatedAt.AsTime()) || (v.State != tammyv1.InvoiceState_INVOICE_STATE_DRAFT && v.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED) {
		return false
	}
	if v.LogoAssetId != nil && !ids.IsCanonicalV7(*v.LogoAssetId) {
		return false
	}
	computed, err := Calculate(v.Draft)
	if err != nil || computed.SubtotalMinor != v.SubtotalMinor || computed.GstMinor != v.GstMinor || computed.TotalMinor != v.TotalMinor || computed.CostMinor != v.CostMinor || len(computed.LineTotals) != len(v.LineTotals) {
		return false
	}
	for i, line := range computed.LineTotals {
		if !proto.Equal(line, v.LineTotals[i]) {
			return false
		}
	}
	return v.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED || ValidateIssue(v.Draft) == nil
}
