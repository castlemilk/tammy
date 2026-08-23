package organisations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var (
	ErrOrganisationNotFound = errors.New("organisations: organisation not found")
	ErrOrganisationExists   = errors.New("organisations: workspace already has an organisation")
	ErrRepositoryConflict   = errors.New("organisations: stale organisation version")
	ErrRepository           = errors.New("organisations: repository failure")
)

// Repository is scoped to the caller-owned command transaction. It has no
// transaction terminal capability and never owns the workspace database.
type Repository struct {
	executor app.CommandSQLExecutor
}

func NewRepository(executor app.CommandSQLExecutor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	return &Repository{executor: executor}, nil
}

func (repository *Repository) Create(ctx context.Context, profile *tammyv1.Organisation, now time.Time) error {
	if repository == nil || repository.executor == nil || ctx == nil || !validOrganisation(profile) || now.IsZero() {
		return ErrRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT count(*) FROM organisations`)
	if err != nil {
		return fmt.Errorf("%w: count organisations: %v", ErrRepository, err)
	}
	var count int
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return ErrRepository
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("%w: close organisation count: %v", ErrRepository, err)
	}
	if count != 0 {
		return ErrOrganisationExists
	}
	_, err = repository.executor.ExecContext(ctx, `
		INSERT INTO organisations(
			id, legal_name, display_name, entity_type, trading_name, abn,
			gst_basis, gst_reporting_frequency, financial_year_end_month,
			owner_user_id, active_tax_rule_type, active_tax_rule_id,
			active_tax_rule_revision, active_tax_rule_content_hash,
			status, verification_state, version, created_at, updated_at
		) VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ACTIVE', ?, ?, ?, NULL)`,
		profile.Id, profile.LegalName, profile.DisplayName, profile.EntityType, profile.Abn,
		int32(profile.GstBasis), int32(profile.GstReportingFrequency), profile.FinancialYearEndMonth,
		profile.OwnerUserId, profile.ActiveTaxRuleBundle.Type, profile.ActiveTaxRuleBundle.Id,
		profile.ActiveTaxRuleBundle.Revision, profile.ActiveTaxRuleBundle.ContentHash,
		verificationStateName(profile.VerificationState), profile.Version, now.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("%w: insert organisation: %v", ErrRepository, err)
	}
	return nil
}

func (repository *Repository) Get(ctx context.Context, organisationID string) (*tammyv1.Organisation, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, version, abn, legal_name, display_name, entity_type,
		       gst_basis, gst_reporting_frequency, financial_year_end_month,
		       verification_state, owner_user_id, active_tax_rule_type,
		       active_tax_rule_id, active_tax_rule_revision, active_tax_rule_content_hash
		FROM organisations WHERE id = ?`, organisationID)
	if err != nil {
		return nil, fmt.Errorf("%w: query organisation: %v", ErrRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("%w: read organisation: %v", ErrRepository, err)
		}
		return nil, ErrOrganisationNotFound
	}
	profile := &tammyv1.Organisation{ActiveTaxRuleBundle: &tammyv1.SourceRef{}}
	var gstBasis, frequency int32
	var verification string
	if err := rows.Scan(
		&profile.Id, &profile.Version, &profile.Abn, &profile.LegalName, &profile.DisplayName,
		&profile.EntityType, &gstBasis, &frequency, &profile.FinancialYearEndMonth,
		&verification, &profile.OwnerUserId, &profile.ActiveTaxRuleBundle.Type,
		&profile.ActiveTaxRuleBundle.Id, &profile.ActiveTaxRuleBundle.Revision,
		&profile.ActiveTaxRuleBundle.ContentHash,
	); err != nil {
		return nil, fmt.Errorf("%w: scan organisation: %v", ErrRepository, err)
	}
	if rows.Next() {
		return nil, ErrRepository
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: finish organisation query: %v", ErrRepository, err)
	}
	profile.GstBasis = tammyv1.GstBasis(gstBasis)
	profile.GstReportingFrequency = tammyv1.GstReportingFrequency(frequency)
	profile.VerificationState = parseVerificationState(verification)
	if !validOrganisation(profile) {
		return nil, ErrRepository
	}
	return profile, nil
}

func (repository *Repository) GetSole(ctx context.Context) (*tammyv1.Organisation, error) {
	if repository == nil || repository.executor == nil || ctx == nil {
		return nil, ErrRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT id FROM organisations ORDER BY id LIMIT 2`)
	if err != nil {
		return nil, fmt.Errorf("%w: query sole organisation: %v", ErrRepository, err)
	}
	if !rows.Next() {
		_ = rows.Close()
		return nil, ErrOrganisationNotFound
	}
	var id string
	if err := rows.Scan(&id); err != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return nil, ErrRepository
	}
	if err := rows.Close(); err != nil {
		return nil, ErrRepository
	}
	return repository.Get(ctx, id)
}

func (repository *Repository) Update(
	ctx context.Context,
	expectedVersion uint64,
	profile *tammyv1.Organisation,
	now time.Time,
) error {
	if repository == nil || repository.executor == nil || ctx == nil || expectedVersion == 0 ||
		profile == nil || profile.Version != expectedVersion+1 || !validOrganisation(profile) || now.IsZero() {
		return ErrRepository
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE organisations SET
			legal_name = ?, display_name = ?, entity_type = ?, abn = ?,
			gst_basis = ?, gst_reporting_frequency = ?, financial_year_end_month = ?,
			owner_user_id = ?, active_tax_rule_type = ?, active_tax_rule_id = ?,
			active_tax_rule_revision = ?, active_tax_rule_content_hash = ?,
			verification_state = ?, version = ?, updated_at = ?
		WHERE id = ? AND version = ?`,
		profile.LegalName, profile.DisplayName, profile.EntityType, profile.Abn,
		int32(profile.GstBasis), int32(profile.GstReportingFrequency), profile.FinancialYearEndMonth,
		profile.OwnerUserId, profile.ActiveTaxRuleBundle.Type, profile.ActiveTaxRuleBundle.Id,
		profile.ActiveTaxRuleBundle.Revision, profile.ActiveTaxRuleBundle.ContentHash,
		verificationStateName(profile.VerificationState), profile.Version,
		now.UTC().Format(time.RFC3339Nano), profile.Id, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("%w: update organisation: %v", ErrRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: count organisation update: %v", ErrRepository, err)
	}
	if affected != 1 {
		return ErrRepositoryConflict
	}
	return nil
}

func validOrganisation(profile *tammyv1.Organisation) bool {
	return profile != nil && ids.IsCanonicalV7(profile.Id) && profile.Version > 0 &&
		ValidateABN(profile.Abn) == nil && boundedCanonicalText(profile.LegalName, 256) &&
		boundedCanonicalText(profile.DisplayName, 256) && boundedCanonicalText(profile.EntityType, 96) &&
		validGSTConfiguration(profile.GstBasis, profile.GstReportingFrequency) &&
		profile.FinancialYearEndMonth >= 1 && profile.FinancialYearEndMonth <= 12 &&
		validVerificationState(profile.VerificationState) && ids.IsCanonicalV7(profile.OwnerUserId) &&
		validSourceRef(profile.ActiveTaxRuleBundle)
}

func validVerificationState(state tammyv1.OrganisationVerificationState) bool {
	switch state {
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED,
		tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_FAILED,
		tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_EXPIRED,
		tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED:
		return true
	default:
		return false
	}
}

func verificationStateName(state tammyv1.OrganisationVerificationState) string {
	switch state {
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED:
		return "UNVERIFIED"
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED:
		return "VERIFIED"
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_FAILED:
		return "FAILED"
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_EXPIRED:
		return "EXPIRED"
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED:
		return "SUPERSEDED"
	default:
		return ""
	}
}

func parseVerificationState(value string) tammyv1.OrganisationVerificationState {
	switch value {
	case "UNVERIFIED":
		return tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED
	case "VERIFIED":
		return tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED
	case "FAILED":
		return tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_FAILED
	case "EXPIRED":
		return tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_EXPIRED
	case "SUPERSEDED":
		return tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED
	default:
		return tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNSPECIFIED
	}
}
