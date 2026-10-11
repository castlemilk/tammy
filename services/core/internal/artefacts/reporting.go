package artefacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math"
	"strconv"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const (
	maxRuleReportingCatalogueRows = 200
	maxRuleReportingCaptureBytes  = 8 * 1024 * 1024
	ruleReportingProofTag         = "tammy/artefacts/rule-bundle-reporting/v1"
)

var ErrRuleReportingBound = errors.New("artefacts: rule reporting capture exceeds its bound")

// RuleReportingBoundError identifies a finite rule-reporting bound that was
// crossed. It unwraps to ErrRuleReportingBound for callers that need only the
// typed failure category.
type RuleReportingBoundError struct {
	Resource string
	Limit    int64
}

func (err *RuleReportingBoundError) Error() string {
	if err == nil {
		return ErrRuleReportingBound.Error()
	}
	return fmt.Sprintf("%s: %s exceeds limit %d", ErrRuleReportingBound, err.Resource, err.Limit)
}

func (err *RuleReportingBoundError) Unwrap() error { return ErrRuleReportingBound }

// RuleCatalogueReportingRecord preserves one exact retained catalogue row.
// Expiry is nullable source metadata; capture does not decide rule eligibility.
type RuleCatalogueReportingRecord struct {
	ID, Code, RuleBundleID, Label, Treatment, EffectiveFrom string
	RateMillionths                                          int64
	EffectiveTo                                             *string
}

// RuleBundleReportingRecord is a complete organization-owned rule bundle
// source and its closed catalogue. Bundle.Source.ContentHash authenticates
// the original JSON bytes; ContentHash is a distinct proof over every raw SQL
// parent and catalogue field, including nullable-field presence.
type RuleBundleReportingRecord struct {
	Bundle         RuleBundle
	OrganisationID string
	BundleType     string
	SemanticSHA256 string
	RetainedAt     string
	EffectiveTo    *string
	Catalogue      []RuleCatalogueReportingRecord
	ContentHash    []byte
	PayloadBytes   int64
}

type ruleReportingParent struct {
	id, organisationID, bundleType, version, semanticSHA256 string
	rulesProto                                              []byte
	effectiveFrom                                           string
	effectiveTo                                             sql.NullString
	retainedAt                                              string
}

type ruleReportingCatalogueRow struct {
	id, code, ruleBundleID, label, treatment, effectiveFrom string
	rateMillionths                                          int64
	effectiveTo                                             sql.NullString
}

// GetForReporting reads the complete retained bundle and catalogue selected
// by an exact source reference. It reads only through the caller-owned
// executor and does not decide whether any rule applies to a reporting date.
func (repository *Repository) GetForReporting(
	ctx context.Context,
	organisationID string,
	source *tammyv1.SourceRef,
) (*RuleBundleReportingRecord, error) {
	return repository.GetForReportingWithPayloadBudget(
		ctx, organisationID, source, maxRuleReportingCaptureBytes,
	)
}

// GetForReportingWithPayloadBudget preflights every retained SQL cell and
// row allowance before loading the bundle bytes or catalogue records. The
// shared capture cap still applies when a caller supplies a larger budget.
// PayloadBytes uses the same retained-cell plus 512-byte-per-row convention.
func (repository *Repository) GetForReportingWithPayloadBudget(
	ctx context.Context,
	organisationID string,
	source *tammyv1.SourceRef,
	maximumPayloadBytes int64,
) (*RuleBundleReportingRecord, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || !validRuleReportingSource(source) || maximumPayloadBytes < 0 {
		return nil, ErrRuleRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	if maximumPayloadBytes > maxRuleReportingCaptureBytes {
		maximumPayloadBytes = maxRuleReportingCaptureBytes
	}

	parentBytes, err := repository.preflightRuleReportingParent(ctx, organisationID, source.Id)
	if err != nil {
		return nil, err
	}
	catalogueCount, catalogueBytes, err := repository.preflightRuleReportingCatalogue(ctx, organisationID, source.Id)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	if catalogueCount > maxRuleReportingCatalogueRows {
		return nil, &RuleReportingBoundError{Resource: "catalogue rows", Limit: maxRuleReportingCatalogueRows}
	}
	payloadBytes, ok := addRuleReportingBytes(parentBytes, catalogueBytes)
	if !ok {
		return nil, &RuleReportingBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	if payloadBytes > maximumPayloadBytes {
		return nil, &RuleReportingBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}

	parent, err := repository.loadRuleReportingParent(ctx, organisationID, source.Id)
	if err != nil {
		return nil, err
	}
	catalogueRows, err := repository.loadRuleReportingCatalogue(ctx, organisationID, source.Id)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	actualPayloadBytes, ok := ruleReportingPayloadBytes(parent, catalogueRows)
	if !ok {
		return nil, errors.Join(fmt.Errorf("%w: retained rule payload size overflow", ErrRuleRepository), ctx.Err())
	}
	if actualPayloadBytes != payloadBytes {
		return nil, errors.Join(fmt.Errorf("%w: retained rule source changed during reporting capture", ErrRuleRepository), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	if actualPayloadBytes > maximumPayloadBytes {
		return nil, &RuleReportingBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}

	bundle, err := verifyRuleReportingParent(parent, organisationID, source)
	if err != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	catalogue, err := verifyRuleReportingCatalogue(bundle, catalogueRows)
	if err != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	proofHash, err := ruleReportingProofHash(ctx, parent, catalogueRows)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}

	return &RuleBundleReportingRecord{
		Bundle: bundle, OrganisationID: parent.organisationID, BundleType: parent.bundleType,
		SemanticSHA256: parent.semanticSHA256, RetainedAt: parent.retainedAt,
		EffectiveTo: optionalRuleReportingString(parent.effectiveTo), Catalogue: catalogue,
		ContentHash: proofHash, PayloadBytes: actualPayloadBytes,
	}, nil
}

func validRuleReportingSource(source *tammyv1.SourceRef) bool {
	return source != nil && source.Type == "tax_rule_bundle" && ids.IsCanonicalV7(source.Id) &&
		source.Revision == 1 && len(source.ContentHash) == sha256.Size
}

func (repository *Repository) preflightRuleReportingParent(
	ctx context.Context,
	organisationID, bundleID string,
) (int64, error) {
	const size = `512 + coalesce(length(CAST(id AS BLOB)), 0) +
		coalesce(length(CAST(organisation_id AS BLOB)), 0) +
		coalesce(length(CAST(bundle_type AS BLOB)), 0) +
		coalesce(length(CAST(version AS BLOB)), 0) +
		coalesce(length(CAST(semantic_sha256 AS BLOB)), 0) +
		coalesce(length(CAST(rules_proto AS BLOB)), 0) +
		coalesce(length(CAST(effective_from AS BLOB)), 0) +
		coalesce(length(CAST(effective_to AS BLOB)), 0) +
		coalesce(length(CAST(retained_at AS BLOB)), 0)`
	query := `SELECT count(*), total(payload_bytes) FROM (
		SELECT ` + size + ` AS payload_bytes FROM rule_bundles
		WHERE organisation_id=? AND id=? LIMIT 2)`
	count, payloadBytes, err := queryRuleReportingPreflight(ctx, repository.executor, query, organisationID, bundleID)
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, errors.Join(ErrRuleRepository,
			errors.New("exact organization-owned rule bundle not found"), ctx.Err())
	}
	return payloadBytes, nil
}

func (repository *Repository) preflightRuleReportingCatalogue(
	ctx context.Context,
	organisationID, bundleID string,
) (int64, int64, error) {
	const size = `512 + coalesce(length(CAST(c.id AS BLOB)), 0) +
		coalesce(length(CAST(c.code AS BLOB)), 0) +
		coalesce(length(CAST(c.rule_bundle_id AS BLOB)), 0) +
		coalesce(length(CAST(c.label AS BLOB)), 0) +
		coalesce(length(CAST(c.rate_millionths AS BLOB)), 0) +
		coalesce(length(CAST(c.treatment AS BLOB)), 0) +
		coalesce(length(CAST(c.effective_from AS BLOB)), 0) +
		coalesce(length(CAST(c.effective_to AS BLOB)), 0)`
	query := `SELECT count(*), total(payload_bytes) FROM (
		SELECT ` + size + ` AS payload_bytes
		FROM tax_code_catalogue AS c
		JOIN rule_bundles AS b ON b.id=c.rule_bundle_id
		WHERE b.organisation_id=? AND b.id=?
		ORDER BY c.code, c.id LIMIT ?)`
	count, payloadBytes, err := queryRuleReportingPreflight(
		ctx, repository.executor, query, organisationID, bundleID, maxRuleReportingCatalogueRows+1,
	)
	if err != nil {
		return 0, 0, err
	}
	return count, payloadBytes, nil
}

func queryRuleReportingPreflight(
	ctx context.Context,
	executor interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	query string,
	args ...any,
) (int64, int64, error) {
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, ruleReportingReadError(ctx, "query retained rule preflight", err, nil)
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, errors.Join(ErrRuleRepository, err, rows.Close())
	}
	if !rows.Next() {
		rowsErr, closeErr := rows.Err(), rows.Close()
		if rowsErr == nil {
			rowsErr = errors.New("preflight returned no aggregate row")
		}
		return 0, 0, ruleReportingReadError(ctx, "read retained rule preflight", rowsErr, closeErr)
	}
	var count int64
	var payloadAsFloat float64
	if err := rows.Scan(&count, &payloadAsFloat); err != nil {
		return 0, 0, ruleReportingReadError(ctx, "scan retained rule preflight", err, rows.Close())
	}
	if rows.Next() {
		return 0, 0, ruleReportingReadError(ctx, "finish retained rule preflight",
			errors.New("preflight returned multiple aggregate rows"), rows.Close())
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return 0, 0, ruleReportingReadError(ctx, "close retained rule preflight", errors.Join(rowsErr, closeErr), nil)
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, errors.Join(ErrRuleRepository, err)
	}
	if count < 0 || payloadAsFloat < 0 || math.IsNaN(payloadAsFloat) || math.IsInf(payloadAsFloat, 0) ||
		math.Trunc(payloadAsFloat) != payloadAsFloat || payloadAsFloat > float64(maxRuleReportingCaptureBytes) {
		if payloadAsFloat > float64(maxRuleReportingCaptureBytes) {
			return 0, 0, &RuleReportingBoundError{Resource: "capture bytes", Limit: maxRuleReportingCaptureBytes}
		}
		return 0, 0, fmt.Errorf("%w: invalid retained rule preflight size", ErrRuleRepository)
	}
	return count, int64(payloadAsFloat), nil
}

func (repository *Repository) loadRuleReportingParent(
	ctx context.Context,
	organisationID, bundleID string,
) (ruleReportingParent, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, organisation_id, bundle_type, version, semantic_sha256,
		       rules_proto, effective_from, effective_to, retained_at
		FROM rule_bundles WHERE organisation_id=? AND id=?`, organisationID, bundleID)
	if err != nil {
		return ruleReportingParent{}, ruleReportingReadError(ctx, "query exact retained rule bundle", err, nil)
	}
	if err := ctx.Err(); err != nil {
		return ruleReportingParent{}, errors.Join(ErrRuleRepository, err, rows.Close())
	}
	if !rows.Next() {
		rowsErr, closeErr := rows.Err(), rows.Close()
		if rowsErr != nil {
			return ruleReportingParent{}, ruleReportingReadError(ctx, "read exact retained rule bundle", rowsErr, closeErr)
		}
		return ruleReportingParent{}, errors.Join(ErrRuleRepository,
			errors.New("exact organization-owned rule bundle disappeared after preflight"), closeErr, ctx.Err())
	}
	var parent ruleReportingParent
	if err := rows.Scan(&parent.id, &parent.organisationID, &parent.bundleType, &parent.version,
		&parent.semanticSHA256, &parent.rulesProto, &parent.effectiveFrom, &parent.effectiveTo, &parent.retainedAt); err != nil {
		return ruleReportingParent{}, ruleReportingReadError(ctx, "scan exact retained rule bundle", err, rows.Close())
	}
	parent.rulesProto = append([]byte(nil), parent.rulesProto...)
	if rows.Next() {
		return ruleReportingParent{}, ruleReportingReadError(ctx, "finish exact retained rule bundle",
			errors.New("exact retained rule bundle returned multiple rows"), rows.Close())
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return ruleReportingParent{}, ruleReportingReadError(ctx, "close exact retained rule bundle", errors.Join(rowsErr, closeErr), nil)
	}
	if err := ctx.Err(); err != nil {
		return ruleReportingParent{}, errors.Join(ErrRuleRepository, err)
	}
	return parent, nil
}

func (repository *Repository) loadRuleReportingCatalogue(
	ctx context.Context,
	organisationID, bundleID string,
) ([]ruleReportingCatalogueRow, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT c.id, c.code, c.rule_bundle_id, c.label, c.treatment,
		       c.rate_millionths, c.effective_from, c.effective_to
		FROM tax_code_catalogue AS c
		JOIN rule_bundles AS b ON b.id=c.rule_bundle_id
		WHERE b.organisation_id=? AND b.id=?
		ORDER BY c.code, c.id LIMIT ?`, organisationID, bundleID, maxRuleReportingCatalogueRows+1)
	if err != nil {
		return nil, ruleReportingReadError(ctx, "query exact rule catalogue", err, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err, rows.Close())
	}
	catalogue := make([]ruleReportingCatalogueRow, 0, maxRuleReportingCatalogueRows)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRuleRepository, err, rows.Close())
		}
		var row ruleReportingCatalogueRow
		if err := rows.Scan(&row.id, &row.code, &row.ruleBundleID, &row.label, &row.treatment,
			&row.rateMillionths, &row.effectiveFrom, &row.effectiveTo); err != nil {
			return nil, ruleReportingReadError(ctx, "scan exact rule catalogue", err, rows.Close())
		}
		catalogue = append(catalogue, row)
		if len(catalogue) > maxRuleReportingCatalogueRows {
			return nil, errors.Join(&RuleReportingBoundError{Resource: "catalogue rows", Limit: maxRuleReportingCatalogueRows}, rows.Close(), ctx.Err())
		}
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, ruleReportingReadError(ctx, "close exact rule catalogue", errors.Join(rowsErr, closeErr), nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	return catalogue, nil
}

func verifyRuleReportingParent(
	parent ruleReportingParent,
	organisationID string,
	source *tammyv1.SourceRef,
) (RuleBundle, error) {
	if parent.id != source.Id || parent.organisationID != organisationID || parent.bundleType != "AU_GST" {
		return RuleBundle{}, fmt.Errorf("%w: retained rule bundle identity or type mismatch", ErrRuleRepository)
	}
	digest, err := hex.DecodeString(parent.semanticSHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != parent.semanticSHA256 ||
		!bytes.Equal(digest, source.ContentHash) {
		return RuleBundle{}, fmt.Errorf("%w: retained rule bundle source hash mismatch", ErrRuleRepository)
	}
	bundle, err := ParseRuleBundle(parent.rulesProto, digest)
	if err != nil {
		return RuleBundle{}, fmt.Errorf("%w: parse retained rule bundle: %w", ErrRuleRepository, err)
	}
	if parent.version != bundle.Version || parent.effectiveFrom != bundle.EffectiveFrom ||
		bundle.Source == nil || bundle.Source.Type != source.Type || bundle.Source.Id != source.Id ||
		bundle.Source.Revision != source.Revision || !bytes.Equal(bundle.Source.ContentHash, source.ContentHash) {
		return RuleBundle{}, fmt.Errorf("%w: retained rule bundle metadata disagrees with its source", ErrRuleRepository)
	}
	if parent.effectiveTo.Valid && (!validRuleReportingDate(parent.effectiveTo.String) || parent.effectiveTo.String < parent.effectiveFrom) {
		return RuleBundle{}, fmt.Errorf("%w: invalid retained rule bundle effective window", ErrRuleRepository)
	}
	retainedAt, err := time.Parse(time.RFC3339Nano, parent.retainedAt)
	if err != nil || retainedAt.IsZero() || retainedAt.UTC().Format(time.RFC3339Nano) != parent.retainedAt {
		return RuleBundle{}, fmt.Errorf("%w: invalid retained rule bundle timestamp", ErrRuleRepository)
	}
	return bundle, nil
}

func verifyRuleReportingCatalogue(
	bundle RuleBundle,
	rows []ruleReportingCatalogueRow,
) ([]RuleCatalogueReportingRecord, error) {
	if len(rows) != len(bundle.TaxCodes) || len(rows) > maxRuleReportingCatalogueRows {
		return nil, fmt.Errorf("%w: retained rule catalogue is not complete", ErrRuleRepository)
	}
	expected := make(map[string]TaxCodeRule, len(bundle.TaxCodes))
	for _, rule := range bundle.TaxCodes {
		expected[rule.ID] = rule
	}
	catalogue := make([]RuleCatalogueReportingRecord, 0, len(rows))
	for _, row := range rows {
		want, ok := expected[row.id]
		if !ok || row.ruleBundleID != bundle.Source.Id || row.code != want.Code || row.label != want.Label ||
			row.treatment != treatmentName(want.Treatment) || row.rateMillionths != want.RateMillionths {
			return nil, fmt.Errorf("%w: retained rule catalogue disagrees with source bundle", ErrRuleRepository)
		}
		delete(expected, row.id)
		if !validRuleReportingDate(row.effectiveFrom) || row.effectiveFrom < bundle.EffectiveFrom ||
			(row.effectiveTo.Valid && (!validRuleReportingDate(row.effectiveTo.String) || row.effectiveTo.String < row.effectiveFrom)) {
			return nil, fmt.Errorf("%w: invalid retained rule catalogue effective window", ErrRuleRepository)
		}
		catalogue = append(catalogue, RuleCatalogueReportingRecord{
			ID: row.id, Code: row.code, RuleBundleID: row.ruleBundleID, Label: row.label,
			Treatment: row.treatment, RateMillionths: row.rateMillionths,
			EffectiveFrom: row.effectiveFrom, EffectiveTo: optionalRuleReportingString(row.effectiveTo),
		})
	}
	if len(expected) != 0 {
		return nil, fmt.Errorf("%w: retained rule catalogue is missing source rows", ErrRuleRepository)
	}
	return catalogue, nil
}

func validRuleReportingDate(value string) bool {
	if len(value) != len("2006-01-02") {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func optionalRuleReportingString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copy := value.String
	return &copy
}

func ruleReportingProofHash(
	ctx context.Context,
	parent ruleReportingParent,
	catalogue []ruleReportingCatalogueRow,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	digest := sha256.New()
	writeRuleReportingField(digest, "proof_tag", []byte(ruleReportingProofTag))
	writeRuleReportingField(digest, "parent.id", []byte(parent.id))
	writeRuleReportingField(digest, "parent.organisation_id", []byte(parent.organisationID))
	writeRuleReportingField(digest, "parent.bundle_type", []byte(parent.bundleType))
	writeRuleReportingField(digest, "parent.version", []byte(parent.version))
	writeRuleReportingField(digest, "parent.semantic_sha256", []byte(parent.semanticSHA256))
	writeRuleReportingField(digest, "parent.rules_proto", parent.rulesProto)
	writeRuleReportingField(digest, "parent.effective_from", []byte(parent.effectiveFrom))
	writeRuleReportingNullableString(digest, "parent.effective_to", parent.effectiveTo)
	writeRuleReportingField(digest, "parent.retained_at", []byte(parent.retainedAt))
	writeRuleReportingField(digest, "catalogue.count", ruleReportingInt64Bytes(int64(len(catalogue))))
	for _, row := range catalogue {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRuleRepository, err)
		}
		writeRuleReportingField(digest, "catalogue.id", []byte(row.id))
		writeRuleReportingField(digest, "catalogue.code", []byte(row.code))
		writeRuleReportingField(digest, "catalogue.rule_bundle_id", []byte(row.ruleBundleID))
		writeRuleReportingField(digest, "catalogue.label", []byte(row.label))
		writeRuleReportingField(digest, "catalogue.rate_millionths", ruleReportingInt64Bytes(row.rateMillionths))
		writeRuleReportingField(digest, "catalogue.treatment", []byte(row.treatment))
		writeRuleReportingField(digest, "catalogue.effective_from", []byte(row.effectiveFrom))
		writeRuleReportingNullableString(digest, "catalogue.effective_to", row.effectiveTo)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRuleRepository, err)
	}
	return digest.Sum(nil), nil
}

func writeRuleReportingField(digest hash.Hash, name string, value []byte) {
	writeRuleReportingFrame(digest, []byte(name))
	writeRuleReportingFrame(digest, value)
}

func writeRuleReportingNullableString(digest hash.Hash, name string, value sql.NullString) {
	writeRuleReportingFrame(digest, []byte(name))
	if !value.Valid {
		writeRuleReportingFrame(digest, []byte{0})
		return
	}
	writeRuleReportingFrame(digest, []byte{1})
	writeRuleReportingFrame(digest, []byte(value.String))
}

func writeRuleReportingFrame(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

func ruleReportingInt64Bytes(value int64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	return encoded[:]
}

func ruleReportingPayloadBytes(parent ruleReportingParent, catalogue []ruleReportingCatalogueRow) (int64, bool) {
	total := int64(0)
	if !addRuleReportingBytesInPlace(&total, 512) ||
		!addRuleReportingCell(&total, parent.id) || !addRuleReportingCell(&total, parent.organisationID) ||
		!addRuleReportingCell(&total, parent.bundleType) || !addRuleReportingCell(&total, parent.version) ||
		!addRuleReportingCell(&total, parent.semanticSHA256) || !addRuleReportingBytesInPlace(&total, int64(len(parent.rulesProto))) ||
		!addRuleReportingCell(&total, parent.effectiveFrom) ||
		(parent.effectiveTo.Valid && !addRuleReportingCell(&total, parent.effectiveTo.String)) ||
		!addRuleReportingCell(&total, parent.retainedAt) {
		return 0, false
	}
	for _, row := range catalogue {
		if !addRuleReportingBytesInPlace(&total, 512) || !addRuleReportingCell(&total, row.id) ||
			!addRuleReportingCell(&total, row.code) || !addRuleReportingCell(&total, row.ruleBundleID) ||
			!addRuleReportingCell(&total, row.label) || !addRuleReportingCell(&total, strconv.FormatInt(row.rateMillionths, 10)) ||
			!addRuleReportingCell(&total, row.treatment) || !addRuleReportingCell(&total, row.effectiveFrom) ||
			(row.effectiveTo.Valid && !addRuleReportingCell(&total, row.effectiveTo.String)) {
			return 0, false
		}
	}
	return total, true
}

func addRuleReportingBytes(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || right > int64(^uint64(0)>>1)-left {
		return 0, false
	}
	return left + right, true
}

func addRuleReportingBytesInPlace(total *int64, value int64) bool {
	updated, ok := addRuleReportingBytes(*total, value)
	if ok {
		*total = updated
	}
	return ok
}

func addRuleReportingCell(total *int64, value string) bool {
	return addRuleReportingBytesInPlace(total, int64(len(value)))
}

func ruleReportingReadError(ctx context.Context, operation string, cause, cleanup error) error {
	var wrapped error
	if cause != nil {
		wrapped = fmt.Errorf("%s: %w", operation, cause)
	}
	return errors.Join(ErrRuleRepository, wrapped, cleanup, ctx.Err())
}
