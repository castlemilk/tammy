package invoices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrLogoAsset = errors.New("invoices: logo must belong to this organisation")

func (r *Repository) requireOrganisation(ctx context.Context, organisationID string) error {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return ErrRepository
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT id FROM organisations WHERE id=?`, organisationID)
	if err != nil {
		return ErrRepository
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return ErrRepository
		}
		return ErrNotFound
	}
	return nil
}

func (r *Repository) requireLogo(ctx context.Context, organisationID string, assetID *string) error {
	if assetID == nil {
		return nil
	}
	if !ids.IsCanonicalV7(*assetID) {
		return ErrInvalidDraft
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT id FROM invoice_logo_assets_v1 WHERE organisation_id=? AND id=?`, organisationID, *assetID)
	if err != nil {
		return ErrRepository
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return ErrRepository
		}
		return ErrLogoAsset
	}
	return nil
}

func (r *Repository) GetTemplate(ctx context.Context, organisationID string) (*tammyv1.InvoiceTemplate, error) {
	if err := r.requireOrganisation(ctx, organisationID); err != nil {
		return nil, err
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT version,COALESCE(logo_asset_id,''),CASE WHEN length(template_proto)<=32768 THEN template_proto ELSE NULL END FROM invoice_templates_v1 WHERE organisation_id=?`, organisationID)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, ErrRepository
		}
		return &tammyv1.InvoiceTemplate{OrganisationId: organisationID, Defaults: &tammyv1.InvoiceTemplateInput{PaymentTermsDays: 14}}, nil
	}
	var version uint64
	var logoID string
	var encoded []byte
	if rows.Scan(&version, &logoID, &encoded) != nil {
		return nil, ErrRepository
	}
	value := &tammyv1.InvoiceTemplate{}
	if len(encoded) == 0 || proto.Unmarshal(encoded, value) != nil || value.OrganisationId != organisationID || value.Version != version || version == 0 || ValidateTemplate(value.Defaults) != nil || value.Defaults.GetLogoAssetId() != logoID || rows.Next() || rows.Err() != nil {
		return nil, ErrRepository
	}
	return value, nil
}

func (r *Repository) SaveTemplate(ctx context.Context, value *tammyv1.InvoiceTemplate, expected uint64, now time.Time) error {
	if r == nil || r.executor == nil || ctx == nil || value == nil || expected >= math.MaxInt64 || value.Version != expected+1 || ValidateTemplate(value.Defaults) != nil {
		return ErrInvalidDraft
	}
	if err := r.requireOrganisation(ctx, value.OrganisationId); err != nil {
		return err
	}
	if err := r.requireLogo(ctx, value.OrganisationId, value.Defaults.LogoAssetId); err != nil {
		return err
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(value)
	if err != nil || len(encoded) > 32768 {
		return ErrRepository
	}
	if expected == 0 {
		current, err := r.GetTemplate(ctx, value.OrganisationId)
		if err != nil {
			return err
		}
		if current.Version != 0 {
			return ErrConflict
		}
		_, err = r.executor.ExecContext(ctx, `INSERT INTO invoice_templates_v1(organisation_id,version,logo_asset_id,template_proto,updated_at) VALUES (?,1,NULLIF(?,''),?,?)`, value.OrganisationId, value.Defaults.GetLogoAssetId(), encoded, now.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return ErrRepository
		}
		return nil
	}
	result, err := r.executor.ExecContext(ctx, `UPDATE invoice_templates_v1 SET version=?,logo_asset_id=NULLIF(?,''),template_proto=?,updated_at=? WHERE organisation_id=? AND version=?`, value.Version, value.Defaults.GetLogoAssetId(), encoded, now.UTC().Format(time.RFC3339Nano), value.OrganisationId, expected)
	if err != nil {
		return ErrRepository
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ErrRepository
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) SaveLogo(ctx context.Context, logo *tammyv1.InvoiceLogo, content []byte, actorID string) error {
	if r == nil || r.executor == nil || ctx == nil || logo == nil || !ids.IsCanonicalV7(logo.Id) || !ids.IsCanonicalV7(actorID) || logo.CreatedAt == nil || logo.CreatedAt.CheckValid() != nil {
		return ErrInvalidDraft
	}
	if err := r.requireOrganisation(ctx, logo.OrganisationId); err != nil {
		return err
	}
	width, height, err := ValidateLogo(logo.MimeType, content)
	digest := sha256.Sum256(content)
	if err != nil || logo.Width != width || logo.Height != height || logo.ByteLength != uint32(len(content)) || !bytes.Equal(logo.Sha256, digest[:]) {
		return ErrInvalidDraft
	}
	_, err = r.executor.ExecContext(ctx, `INSERT INTO invoice_logo_assets_v1(id,organisation_id,mime_type,byte_length,sha256,width,height,content,created_by_user_id,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, logo.Id, logo.OrganisationId, logo.MimeType, logo.ByteLength, logo.Sha256, logo.Width, logo.Height, content, actorID, logo.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return ErrRepository
	}
	return nil
}

func (r *Repository) GetLogo(ctx context.Context, organisationID, assetID string) (*tammyv1.GetInvoiceLogoResponse, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(assetID) {
		return nil, ErrInvalidDraft
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT id,organisation_id,mime_type,byte_length,sha256,width,height,CASE WHEN length(content)<=1048576 THEN content ELSE NULL END,created_at FROM invoice_logo_assets_v1 WHERE organisation_id=? AND id=?`, organisationID, assetID)
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
	logo := &tammyv1.InvoiceLogo{}
	var content []byte
	var created string
	if rows.Scan(&logo.Id, &logo.OrganisationId, &logo.MimeType, &logo.ByteLength, &logo.Sha256, &logo.Width, &logo.Height, &content, &created) != nil {
		return nil, ErrRepository
	}
	when, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, ErrRepository
	}
	logo.CreatedAt = timestamppb.New(when)
	width, height, err := ValidateLogo(logo.MimeType, content)
	digest := sha256.Sum256(content)
	if err != nil || logo.Id != assetID || logo.OrganisationId != organisationID || logo.Width != width || logo.Height != height || logo.ByteLength != uint32(len(content)) || !bytes.Equal(logo.Sha256, digest[:]) || rows.Next() || rows.Err() != nil {
		return nil, ErrRepository
	}
	return &tammyv1.GetInvoiceLogoResponse{Logo: logo, Content: content}, nil
}
