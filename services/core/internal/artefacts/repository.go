package artefacts

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var (
	ErrRuleBundleConflict = errors.New("artefacts: retained rule bundle conflict")
	ErrRuleRepository     = errors.New("artefacts: rule repository failure")
)

type Repository struct{ executor app.CommandSQLExecutor }

func NewRepository(executor app.CommandSQLExecutor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRuleRepository
	}
	return &Repository{executor: executor}, nil
}

// Retain records an immutable bundle and its closed tax-code catalogue. Exact
// replay is harmless; any changed byte or metadata conflicts.
func (repository *Repository) Retain(ctx context.Context, organisationID string, bundle RuleBundle, now time.Time) error {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		bundle.Source == nil || !ids.IsCanonicalV7(bundle.Source.Id) || len(bundle.Source.ContentHash) != 32 || now.IsZero() {
		return ErrRuleRepository
	}
	rows, err := repository.executor.QueryContext(ctx,
		`SELECT organisation_id, version, semantic_sha256, rules_proto, effective_from FROM rule_bundles WHERE id=?`, bundle.Source.Id)
	if err != nil {
		return fmt.Errorf("%w: inspect rule bundle: %v", ErrRuleRepository, err)
	}
	if rows.Next() {
		var retainedOrganisation, version, semantic, effective string
		var retained []byte
		scanErr := rows.Scan(&retainedOrganisation, &version, &semantic, &retained, &effective)
		extra := rows.Next()
		finishErr := rows.Err()
		closeErr := rows.Close()
		if scanErr != nil || extra || finishErr != nil || closeErr != nil {
			return ErrRuleRepository
		}
		if retainedOrganisation == organisationID && version == bundle.Version &&
			semantic == hex.EncodeToString(bundle.Source.ContentHash) && effective == bundle.EffectiveFrom &&
			bytes.Equal(retained, bundle.RetainedBytes) {
			return nil
		}
		return ErrRuleBundleConflict
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("%w: inspect rule bundle row: %v", ErrRuleRepository, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("%w: close rule bundle query: %v", ErrRuleRepository, err)
	}
	if _, err := repository.executor.ExecContext(ctx, `SAVEPOINT retain_rule_bundle`); err != nil {
		return fmt.Errorf("%w: begin bundle retention: %v", ErrRuleRepository, err)
	}
	fail := func(cause error) error {
		_, rollbackErr := repository.executor.ExecContext(context.WithoutCancel(ctx), `ROLLBACK TO retain_rule_bundle`)
		_, releaseErr := repository.executor.ExecContext(context.WithoutCancel(ctx), `RELEASE retain_rule_bundle`)
		return errors.Join(cause, rollbackErr, releaseErr)
	}
	if _, err := repository.executor.ExecContext(ctx, `
		INSERT INTO rule_bundles(id, organisation_id, bundle_type, version, semantic_sha256,
			rules_proto, effective_from, effective_to, retained_at)
		VALUES(?, ?, 'AU_GST', ?, ?, ?, ?, NULL, ?)`, bundle.Source.Id, organisationID, bundle.Version,
		hex.EncodeToString(bundle.Source.ContentHash), bundle.RetainedBytes, bundle.EffectiveFrom,
		now.UTC().Format(time.RFC3339Nano)); err != nil {
		return fail(fmt.Errorf("%w: retain bundle: %v", ErrRuleRepository, err))
	}
	for _, code := range bundle.TaxCodes {
		if _, err := repository.executor.ExecContext(ctx, `
			INSERT INTO tax_code_catalogue(id, code, rule_bundle_id, label, rate_millionths,
				treatment, effective_from, effective_to)
			VALUES(?, ?, ?, ?, ?, ?, ?, NULL)`, code.ID, code.Code, bundle.Source.Id, code.Label,
			code.RateMillionths, treatmentName(code.Treatment), bundle.EffectiveFrom); err != nil {
			return fail(fmt.Errorf("%w: retain tax code: %v", ErrRuleRepository, err))
		}
	}
	if _, err := repository.executor.ExecContext(ctx, `RELEASE retain_rule_bundle`); err != nil {
		return fail(fmt.Errorf("%w: commit bundle retention: %v", ErrRuleRepository, err))
	}
	return nil
}

func (repository *Repository) ListEffectiveTaxCodes(
	ctx context.Context, organisationID, postingDate, afterCode string, limit int,
) ([]*tammyv1.TaxCode, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		len(postingDate) != 10 || limit < 1 || limit > 200 {
		return nil, ErrRuleRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT c.id, c.code, c.label, c.treatment, c.rate_millionths,
		       b.id, b.version, b.semantic_sha256
		FROM tax_code_catalogue c JOIN rule_bundles b ON b.id=c.rule_bundle_id
		WHERE b.organisation_id=? AND c.effective_from<=?
		  AND (c.effective_to IS NULL OR c.effective_to>=?) AND c.code>?
		ORDER BY c.code, c.id LIMIT ?`, organisationID, postingDate, postingDate, afterCode, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: list tax codes: %v", ErrRuleRepository, err)
	}
	defer rows.Close()
	codes := make([]*tammyv1.TaxCode, 0, limit)
	for rows.Next() {
		var code tammyv1.TaxCode
		var treatment string
		var rate int64
		var sourceID, version, semantic string
		if err := rows.Scan(&code.Id, &code.Code, &code.Label, &treatment, &rate, &sourceID, &version, &semantic); err != nil {
			return nil, ErrRuleRepository
		}
		digest, err := hex.DecodeString(semantic)
		if err != nil || len(digest) != 32 {
			return nil, ErrRuleRepository
		}
		code.Treatment = parseTreatmentName(treatment)
		code.Rate = &tammyv1.Decimal{Coefficient: rate, Scale: 6}
		code.Rule = &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: sourceID, Revision: 1, ContentHash: digest}
		if code.Treatment == tammyv1.TaxTreatment_TAX_TREATMENT_UNSPECIFIED || version == "" {
			return nil, ErrRuleRepository
		}
		codes = append(codes, proto.Clone(&code).(*tammyv1.TaxCode))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: finish tax-code list: %v", ErrRuleRepository, err)
	}
	return codes, nil
}

// GetEffectiveTaxCode is the narrow Accounting-facing rule lookup. It returns
// only the immutable typed catalogue projection, never repository internals.
func (repository *Repository) GetEffectiveTaxCode(
	ctx context.Context, organisationID, postingDate, taxCodeID string,
) (*tammyv1.TaxCode, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		!ids.IsCanonicalV7(taxCodeID) || len(postingDate) != 10 {
		return nil, ErrRuleRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT c.id, c.code, c.label, c.treatment, c.rate_millionths,
		       b.id, b.version, b.semantic_sha256
		FROM tax_code_catalogue c JOIN rule_bundles b ON b.id=c.rule_bundle_id
		WHERE b.organisation_id=? AND c.id=? AND c.effective_from<=?
		  AND (c.effective_to IS NULL OR c.effective_to>=?)`,
		organisationID, taxCodeID, postingDate, postingDate)
	if err != nil {
		return nil, fmt.Errorf("%w: get tax code: %v", ErrRuleRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrRuleRepository
	}
	var code tammyv1.TaxCode
	var treatment string
	var rate int64
	var sourceID, version, semantic string
	if err := rows.Scan(&code.Id, &code.Code, &code.Label, &treatment, &rate, &sourceID, &version, &semantic); err != nil || rows.Next() || rows.Err() != nil {
		return nil, ErrRuleRepository
	}
	digest, err := hex.DecodeString(semantic)
	if err != nil || len(digest) != 32 || version == "" {
		return nil, ErrRuleRepository
	}
	code.Treatment = parseTreatmentName(treatment)
	code.Rate = &tammyv1.Decimal{Coefficient: rate, Scale: 6}
	code.Rule = &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: sourceID, Revision: 1, ContentHash: digest}
	if code.Treatment == tammyv1.TaxTreatment_TAX_TREATMENT_UNSPECIFIED {
		return nil, ErrRuleRepository
	}
	return proto.Clone(&code).(*tammyv1.TaxCode), nil
}

func treatmentName(value tammyv1.TaxTreatment) string {
	return map[tammyv1.TaxTreatment]string{
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE:      "TAXABLE",
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE:     "GST_FREE",
		tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED:  "INPUT_TAXED",
		tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE: "OUT_OF_SCOPE",
	}[value]
}

func parseTreatmentName(value string) tammyv1.TaxTreatment {
	for enum, name := range map[tammyv1.TaxTreatment]string{
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE:      "TAXABLE",
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE:     "GST_FREE",
		tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED:  "INPUT_TAXED",
		tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE: "OUT_OF_SCOPE",
	} {
		if name == value {
			return enum
		}
	}
	return tammyv1.TaxTreatment_TAX_TREATMENT_UNSPECIFIED
}
