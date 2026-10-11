package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var (
	ErrValidationRepository = errors.New("baspreparation: validation repository operation failed")
	ErrValidationConflict   = errors.New("baspreparation: validation conflicts with its retained capture owner")
)

const validationSnapshotLimit = 1024 * 1024
const validationFindingMaximum = 200

// ValidationRetainedSnapshot exposes the exact normalized owners that are not
// included in the public validation body. They always refer to historical
// revisions, regardless of the preparation's current workflow pointer.
type ValidationRetainedSnapshot struct {
	Validation      *tammyv1.BasValidationSnapshot
	Preparation     *tammyv1.SourceRef
	SavedPaygInputs *tammyv1.SourceRef
	Family          *tammyv1.SourceRef
	Calculation     *tammyv1.SourceRef
}

// ValidationRepository retains a complete validation and its normalized
// findings. Source evaluation and transaction completion remain command-owned.
type ValidationRepository struct {
	executor     app.CommandSQLExecutor
	cursors      *paging.Codec
	preparations *PreparationReader
	calculations *CalculationRepository
	savedPayg    *SavedPaygRepository
}

func NewValidationRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*ValidationRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrValidationRepository
	}
	preparations, err := NewPreparationReader(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	calculations, err := NewCalculationRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	savedPayg, err := NewSavedPaygRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	return &ValidationRepository{executor: executor, cursors: cursors, preparations: preparations,
		calculations: calculations, savedPayg: savedPayg}, nil
}

func (repository *ValidationRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && repository.cursors != nil &&
		repository.preparations != nil && repository.calculations != nil && repository.savedPayg != nil && ctx != nil && ctx.Err() == nil
}

// Append verifies the exact current capture before writing any rows. A
// savepoint rolls back partial finding writes without committing the caller's
// transaction or discarding earlier command work.
func (repository *ValidationRepository) Append(ctx context.Context, organisationID string,
	value *tammyv1.BasValidationSnapshot, ownerRef *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) || value == nil ||
		!validBASSourceRef(ownerRef, preparationSnapshotSpec.resource, "") {
		return nil, ErrValidationRepository
	}
	owner, err := repository.preparations.GetCurrent(ctx, organisationID, ownerRef.Id)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	if !sameBASSourceRef(ownerRef, preparationReference(owner.Preparation)) {
		return nil, ErrValidationConflict
	}
	switch owner.Preparation.State {
	case tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY:
	default:
		return nil, ErrValidationConflict
	}
	wire, digest, err := sealValidation(value, owner.Preparation)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	if err := repository.verifyCapture(ctx, owner, owner.Preparation.CurrentCalculation, owner.SavedPaygInputs); err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	findingWires, err := prepareValidationFindings(value.Findings)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	createdAt, ok := basTimestampSQLText(value.CreatedAt)
	revision, validRevision := basSQLiteRevision(value.Revision)
	if !ok || !validRevision {
		return nil, ErrValidationRepository
	}
	if err := repository.requireNextRevision(ctx, organisationID, value, owner); err != nil {
		return nil, err
	}
	savepoint := "bas_validation_append_" + strings.ReplaceAll(value.ValidationId, "-", "_")
	if _, err := repository.executor.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	rollback := func() error {
		return rollbackRepositorySavepoint(ctx, repository.executor, savepoint)
	}
	prep, calculation, inputs := owner.Preparation, owner.Preparation.CurrentCalculation, owner.SavedPaygInputs
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_validations_v1(
		validation_id,organisation_id,family_id,preparation_id,preparation_version,revision,content_hash,preparation_hash,
		calculation_id,calculation_version,calculation_hash,source_snapshot_hash,input_revision,input_hash,input_snapshot_id,input_content_hash,
		validation_proto,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ValidationId, organisationID, prep.Family.Id, prep.PreparationId, int64(prep.Version), revision, digest, prep.ContentHash,
		calculation.Id, int64(calculation.Revision), calculation.ContentHash, value.SourceSnapshotHash,
		int64(value.InputRevision), value.InputHash, inputs.Id, inputs.ContentHash, wire, createdAt); err != nil {
		return nil, errors.Join(ErrValidationRepository, err, rollback())
	}
	for index, finding := range value.Findings {
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_validation_findings_v1(
			validation_id,organisation_id,family_id,revision,validation_hash,finding_id,finding_hash,severity,stable_code,safe_message,finding_proto)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, value.ValidationId, organisationID, prep.Family.Id, revision, digest,
			finding.FindingId, canonicalFindingHash(finding), enumSQL(finding.Severity.String(), "BAS_FINDING_SEVERITY_"),
			finding.StableCode, finding.SafeMessage, findingWires[index]); err != nil {
			return nil, errors.Join(ErrValidationRepository, err, rollback())
		}
	}
	if _, err := repository.executor.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return nil, errors.Join(ErrValidationRepository, err, rollback())
	}
	return &tammyv1.SourceRef{Type: "bas_validation", Id: value.ValidationId, Revision: value.Revision, ContentHash: bytes.Clone(digest)}, nil
}

func (repository *ValidationRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasValidationSnapshot, error) {
	retained, err := repository.GetRetained(ctx, organisationID, exact)
	if err != nil {
		return nil, err
	}
	return retained.Validation, nil
}

func (repository *ValidationRepository) GetRetained(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*ValidationRetainedSnapshot, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) || !validBASSourceRef(exact, "bas_validation", "") {
		return nil, ErrValidationRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT family_id,preparation_id,preparation_version,preparation_hash,revision,content_hash,
		calculation_id,calculation_version,calculation_hash,source_snapshot_hash,input_revision,input_hash,input_snapshot_id,input_content_hash,
		CASE WHEN length(validation_proto)<=? THEN validation_proto ELSE NULL END,length(validation_proto),created_at
		FROM bas_validations_v1 WHERE organisation_id=? AND validation_id=? AND revision=?`, validationSnapshotLimit, organisationID, exact.Id, int64(exact.Revision))
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.Join(ErrValidationRepository, rows.Err(), rows.Close())
	}
	var familyID, createdAt string
	var preparation, calculation, input preparationReadSource
	var revision, length int64
	var hash, sourceHash, inputHash, wire []byte
	if err := rows.Scan(&familyID, &preparation.id, &preparation.version, &preparation.hash, &revision, &hash,
		&calculation.id, &calculation.version, &calculation.hash, &sourceHash, &input.version, &inputHash, &input.id, &input.hash,
		&wire, &length, &createdAt); err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	if rows.Next() {
		return nil, errors.Join(ErrValidationRepository, ErrSnapshotCorrupt)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	if err := repositorySnapshotBytes(wire, length, validationSnapshotLimit); err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	ownerRef := validationSourceRef(preparation, "bas_preparation")
	calculationRef := validationSourceRef(calculation, "bas_calculation")
	inputRef := validationSourceRef(input, "bas_saved_payg_inputs")
	if !ids.IsCanonicalV7(familyID) || revision != int64(exact.Revision) || !bytes.Equal(hash, exact.ContentHash) ||
		!validBASSourceRef(ownerRef, "bas_preparation", "") || !validBASSourceRef(calculationRef, "bas_calculation", "") ||
		!validBASSourceRef(inputRef, "bas_saved_payg_inputs", "") {
		return nil, errors.Join(ErrValidationRepository, ErrSnapshotCorrupt)
	}
	owner, err := repository.preparations.GetExact(ctx, organisationID, ownerRef)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, ErrSnapshotCorrupt, err)
	}
	if owner.Preparation.Family.Id != familyID || !sameBASSourceRef(calculationRef, owner.Preparation.CurrentCalculation) ||
		!sameBASSourceRef(inputRef, owner.SavedPaygInputs) || uint64(input.version.Int64) != owner.Preparation.InputRevision ||
		!bytes.Equal(inputHash, owner.Preparation.InputHash) || !bytes.Equal(sourceHash, owner.Preparation.SourceSnapshotHash) {
		return nil, errors.Join(ErrValidationRepository, ErrSnapshotCorrupt)
	}
	value, err := openValidation(wire, organisationID, exact, owner.Preparation)
	if err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	valueTime, validTime := basTimestampSQLText(value.CreatedAt)
	if !validTime || valueTime != createdAt {
		return nil, errors.Join(ErrValidationRepository, ErrSnapshotCorrupt)
	}
	if err := repository.verifyCapture(ctx, owner, calculationRef, inputRef); err != nil {
		return nil, errors.Join(ErrValidationRepository, ErrSnapshotCorrupt, err)
	}
	if err := repository.verifyFindingRows(ctx, organisationID, familyID, exact, value.Findings); err != nil {
		return nil, errors.Join(ErrValidationRepository, err)
	}
	return &ValidationRetainedSnapshot{Validation: value, Preparation: ownerRef, SavedPaygInputs: inputRef,
		Family: cloneRef(owner.Preparation.Family), Calculation: calculationRef}, nil
}

func validationSourceRef(source preparationReadSource, resource string) *tammyv1.SourceRef {
	if !source.id.Valid || !source.version.Valid || source.version.Int64 <= 0 {
		return nil
	}
	return &tammyv1.SourceRef{Type: resource, Id: source.id.String, Revision: uint64(source.version.Int64), ContentHash: bytes.Clone(source.hash)}
}

func (repository *ValidationRepository) verifyCapture(ctx context.Context, owner *PreparationRetainedSnapshot,
	calculationRef, inputRef *tammyv1.SourceRef) error {
	if owner == nil || owner.Preparation == nil || !validBASSourceRef(calculationRef, "bas_calculation", "") ||
		!validBASSourceRef(inputRef, "bas_saved_payg_inputs", "") || !sameBASSourceRef(calculationRef, owner.Preparation.CurrentCalculation) ||
		!sameBASSourceRef(inputRef, owner.SavedPaygInputs) {
		return ErrValidationConflict
	}
	calculation, err := repository.calculations.GetExact(ctx, owner.Preparation.OrganisationId, calculationRef)
	if err != nil {
		return err
	}
	prep := owner.Preparation
	if calculation.Preparation.Id != prep.PreparationId || calculation.Preparation.Revision >= prep.Version ||
		!sameBASSourceRef(calculation.Family, prep.Family) || !sameBASSourceRef(calculation.Profile, prep.Profile) ||
		!sameBASSourceRef(calculation.Period, prep.Period) || calculation.InputRevision != prep.InputRevision ||
		!bytes.Equal(calculation.InputHash, prep.InputHash) || !bytes.Equal(calculation.SourceSnapshotHash, prep.SourceSnapshotHash) {
		return ErrValidationConflict
	}
	capture, err := repository.preparations.GetExact(ctx, prep.OrganisationId, calculation.Preparation)
	if err != nil {
		return err
	}
	if !sameBASSourceRef(capture.SavedPaygInputs, inputRef) {
		return ErrValidationConflict
	}
	if _, err := repository.savedPayg.GetExact(ctx, prep.OrganisationId, inputRef); err != nil {
		return err
	}
	return nil
}

func prepareValidationFindings(values []*tammyv1.BasValidationFinding) ([][]byte, error) {
	if len(values) > validationFindingMaximum {
		return nil, ErrSnapshotBound
	}
	wires := make([][]byte, len(values))
	seen := make(map[string]bool, len(values))
	for index, value := range values {
		if !validFinding(value) || seen[value.FindingId] {
			return nil, ErrSnapshotCorrupt
		}
		seen[value.FindingId] = true
		wire, err := marshalCalculationFrame(value, scalarSnapshotLimit)
		if err != nil {
			return nil, err
		}
		wires[index] = wire
	}
	return wires, nil
}

func (repository *ValidationRepository) verifyFindingRows(ctx context.Context, organisationID, familyID string,
	exact *tammyv1.SourceRef, findings []*tammyv1.BasValidationFinding) error {
	wires, err := prepareValidationFindings(findings)
	if err != nil {
		return err
	}
	expected := make(map[string]int, len(findings))
	for index, finding := range findings {
		expected[finding.FindingId] = index
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT organisation_id,family_id,finding_id,finding_hash,severity,stable_code,safe_message,validation_hash,
		CASE WHEN length(finding_proto)<=? THEN finding_proto ELSE NULL END,length(finding_proto)
		FROM bas_validation_findings_v1 WHERE validation_id=? AND revision=? ORDER BY finding_id LIMIT 201`,
		scalarSnapshotLimit, exact.Id, int64(exact.Revision))
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > validationFindingMaximum {
			return ErrSnapshotBound
		}
		var org, family, id, severity, code, message string
		var findingHash, validationHash, wire []byte
		var length int64
		if err := rows.Scan(&org, &family, &id, &findingHash, &severity, &code, &message, &validationHash, &wire, &length); err != nil {
			return err
		}
		if err := repositorySnapshotBytes(wire, length, scalarSnapshotLimit); err != nil {
			return err
		}
		index, exists := expected[id]
		if !exists || org != organisationID || family != familyID {
			return ErrSnapshotCorrupt
		}
		finding := findings[index]
		if !bytes.Equal(findingHash, canonicalFindingHash(finding)) || !bytes.Equal(validationHash, exact.ContentHash) ||
			enumSQL(finding.Severity.String(), "BAS_FINDING_SEVERITY_") != severity || finding.StableCode != code || finding.SafeMessage != message ||
			!bytes.Equal(wires[index], wire) {
			return ErrSnapshotCorrupt
		}
		delete(expected, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(expected) != 0 {
		return ErrSnapshotCorrupt
	}
	return nil
}

func (repository *ValidationRepository) requireNextRevision(ctx context.Context, organisationID string,
	value *tammyv1.BasValidationSnapshot, owner *PreparationRetainedSnapshot) error {
	rows, err := repository.executor.QueryContext(ctx, `SELECT COALESCE(MAX(revision),0),COUNT(*),COALESCE(MIN(revision),0),
		COALESCE(MIN(organisation_id),''),COALESCE(MAX(organisation_id),''),COALESCE(MIN(family_id),''),COALESCE(MAX(family_id),''),
		COALESCE(MIN(preparation_id),''),COALESCE(MAX(preparation_id),''),
		(SELECT content_hash FROM bas_validations_v1 WHERE validation_id=? ORDER BY revision DESC LIMIT 1)
		FROM bas_validations_v1 WHERE validation_id=?`, value.ValidationId, value.ValidationId)
	if err != nil {
		return errors.Join(ErrValidationRepository, err)
	}
	if !rows.Next() {
		return errors.Join(ErrValidationRepository, rows.Err(), rows.Close())
	}
	var maximum, count, minimum int64
	var minimumOrg, maximumOrg, minimumFamily, maximumFamily, minimumPreparation, maximumPreparation string
	var priorHash []byte
	if err := rows.Scan(&maximum, &count, &minimum, &minimumOrg, &maximumOrg, &minimumFamily, &maximumFamily,
		&minimumPreparation, &maximumPreparation, &priorHash); err != nil || rows.Next() || rows.Err() != nil {
		return errors.Join(ErrValidationRepository, err, rows.Err(), rows.Close())
	}
	if err := rows.Close(); err != nil {
		return errors.Join(ErrValidationRepository, err)
	}
	if maximum < 0 || count < 0 || minimum < 0 || count != maximum || (count != 0 && minimum != 1) ||
		maximum == math.MaxInt64 || value.Revision != uint64(maximum+1) {
		return ErrValidationConflict
	}
	if count == 0 {
		return nil
	}
	if minimumOrg != organisationID || maximumOrg != organisationID || minimumFamily != owner.Preparation.Family.Id ||
		maximumFamily != owner.Preparation.Family.Id || minimumPreparation != owner.Preparation.PreparationId || maximumPreparation != owner.Preparation.PreparationId {
		return ErrValidationConflict
	}
	if _, err := repository.GetRetained(ctx, organisationID, &tammyv1.SourceRef{Type: "bas_validation", Id: value.ValidationId,
		Revision: uint64(maximum), ContentHash: bytes.Clone(priorHash)}); err != nil {
		return errors.Join(ErrValidationRepository, err)
	}
	return nil
}
