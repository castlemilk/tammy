package baspreparation

import (
	"bytes"
	"context"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var (
	ErrWarningAcknowledgementRepository = errors.New("baspreparation: warning acknowledgement repository operation failed")
	ErrWarningAcknowledgementConflict   = errors.New("baspreparation: warning acknowledgement conflicts with its retained validation")
)

const warningAcknowledgementMaximum = 200

// WarningAcknowledgementRepository retains a user's acknowledgement of one
// exact warning finding. Exact reads remain pinned to the historical
// validation even after the preparation advances to a later validation.
type WarningAcknowledgementRepository struct {
	executor     app.CommandSQLExecutor
	cursors      *paging.Codec
	preparations *PreparationReader
	validations  *ValidationRepository
}

func NewWarningAcknowledgementRepository(executor app.CommandSQLExecutor,
	cursors *paging.Codec) (*WarningAcknowledgementRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrWarningAcknowledgementRepository
	}
	preparations, err := NewPreparationReader(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	validations, err := NewValidationRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	return &WarningAcknowledgementRepository{executor: executor, cursors: cursors,
		preparations: preparations, validations: validations}, nil
}

func (repository *WarningAcknowledgementRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && repository.cursors != nil &&
		repository.preparations != nil && repository.validations != nil && ctx != nil && ctx.Err() == nil
}

// Append verifies both the exact current owner and the complete retained
// validation before inserting one immutable acknowledgement. It never takes
// ownership of, commits, or rolls back the caller's transaction.
func (repository *WarningAcknowledgementRepository) Append(ctx context.Context, organisationID string,
	value *tammyv1.BasWarningAcknowledgement, currentPreparationRef *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) || value == nil ||
		!validBASSourceRef(currentPreparationRef, preparationSnapshotSpec.resource, "") ||
		!validBASSourceRef(value.Validation, validationSnapshotResource, "") ||
		!validBASSourceRef(value.Finding, "bas_validation_finding", "") ||
		!ids.IsCanonicalV7(value.AcknowledgementId) || !ids.IsCanonicalV7(value.ActorUserId) {
		return nil, ErrWarningAcknowledgementRepository
	}

	current, err := repository.preparations.GetCurrent(ctx, organisationID, currentPreparationRef.Id)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	if !sameBASSourceRef(currentPreparationRef, preparationReference(current.Preparation)) ||
		current.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		!sameBASSourceRef(current.Preparation.CurrentValidation, value.Validation) {
		return nil, ErrWarningAcknowledgementConflict
	}

	validation, err := repository.validations.GetRetained(ctx, organisationID, value.Validation)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	if !warningAcknowledgementCurrentCaptureMatches(current, validation) {
		return nil, ErrWarningAcknowledgementConflict
	}
	validationOwner, err := repository.preparations.GetExact(ctx, organisationID, validation.Preparation)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	wire, digest, err := sealAcknowledgement(value, validation.Validation, validationOwner.Preparation)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	acknowledgedAt, ok := basTimestampSQLText(value.AcknowledgedAt)
	if !ok {
		return nil, ErrWarningAcknowledgementRepository
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_warning_acknowledgements_v1(
		acknowledgement_id,organisation_id,family_id,preparation_id,validation_id,validation_revision,validation_hash,
		finding_id,finding_hash,source_snapshot_hash,actor_user_id,acknowledged_at,content_hash,acknowledgement_proto)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.AcknowledgementId, organisationID, validationOwner.Preparation.Family.Id,
		validation.Validation.PreparationId, value.Validation.Id, int64(value.Validation.Revision), value.Validation.ContentHash,
		value.Finding.Id, value.Finding.ContentHash, value.SourceSnapshotHash, value.ActorUserId, acknowledgedAt, digest, wire); err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	return &tammyv1.SourceRef{Type: acknowledgementSnapshotResource, Id: value.AcknowledgementId,
		Revision: 1, ContentHash: bytes.Clone(digest)}, nil
}

// GetExact verifies the acknowledgement body, normalized columns, full
// validation and finding rows, and the validation's exact historical owner.
// It intentionally does not consult the preparation's current pointer.
func (repository *WarningAcknowledgementRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasWarningAcknowledgement, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) ||
		!validBASSourceRef(exact, acknowledgementSnapshotResource, "") || exact.Revision != 1 {
		return nil, ErrWarningAcknowledgementRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT acknowledgement_id,organisation_id,family_id,preparation_id,
		validation_id,validation_revision,validation_hash,finding_id,finding_hash,source_snapshot_hash,actor_user_id,
		acknowledged_at,content_hash,CASE WHEN length(acknowledgement_proto)<=? THEN acknowledgement_proto ELSE NULL END,
		length(acknowledgement_proto)
		FROM bas_warning_acknowledgements_v1 WHERE organisation_id=? AND acknowledgement_id=?`,
		scalarSnapshotLimit, organisationID, exact.Id)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	var record warningAcknowledgementReadRecord
	if !rows.Next() {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&record.id, &record.organisationID, &record.familyID, &record.preparationID,
		&record.validationID, &record.validationRevision, &record.validationHash, &record.findingID,
		&record.findingHash, &record.sourceSnapshotHash, &record.actorUserID, &record.acknowledgedAt,
		&record.contentHash, &record.wire, &record.wireLength); err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	if err := repositorySnapshotBytes(record.wire, record.wireLength, scalarSnapshotLimit); err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	if record.validationRevision <= 0 || !ids.IsCanonicalV7(record.validationID) {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, ErrSnapshotCorrupt)
	}
	validationRef := &tammyv1.SourceRef{Type: validationSnapshotResource, Id: record.validationID,
		Revision: uint64(record.validationRevision), ContentHash: bytes.Clone(record.validationHash)}
	validation, err := repository.validations.GetRetained(ctx, organisationID, validationRef)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	owner, err := repository.preparations.GetExact(ctx, organisationID, validation.Preparation)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	value, err := openAcknowledgement(record.wire, organisationID, exact, validation.Validation, owner.Preparation)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	acknowledgedAt, ok := basTimestampSQLText(value.AcknowledgedAt)
	if !ok || acknowledgedAt != record.acknowledgedAt || record.id != value.AcknowledgementId ||
		record.organisationID != organisationID || record.familyID != owner.Preparation.Family.Id ||
		record.preparationID != validation.Validation.PreparationId ||
		record.validationID != value.Validation.Id || record.validationRevision != int64(value.Validation.Revision) ||
		!bytes.Equal(record.validationHash, value.Validation.ContentHash) || record.findingID != value.Finding.Id ||
		!bytes.Equal(record.findingHash, value.Finding.ContentHash) ||
		!bytes.Equal(record.sourceSnapshotHash, value.SourceSnapshotHash) || record.actorUserID != value.ActorUserId ||
		!bytes.Equal(record.contentHash, exact.ContentHash) {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, ErrSnapshotCorrupt)
	}
	return value, nil
}

// ListForValidation returns the complete bounded acknowledgement set for one
// exact historical validation. Rows are closed before GetExact recursively
// verifies their retained validation/finding parents.
func (repository *WarningAcknowledgementRepository) ListForValidation(ctx context.Context, organisationID string,
	validationRef *tammyv1.SourceRef) ([]*tammyv1.BasWarningAcknowledgement, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) ||
		!validBASSourceRef(validationRef, validationSnapshotResource, "") {
		return nil, ErrWarningAcknowledgementRepository
	}
	validation, err := repository.validations.GetRetained(ctx, organisationID, validationRef)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT acknowledgement_id,content_hash
		FROM bas_warning_acknowledgements_v1 WHERE organisation_id=? AND family_id=? AND validation_id=? AND validation_revision=? AND validation_hash=?
		ORDER BY finding_id,acknowledgement_id LIMIT ?`, organisationID, validation.Family.Id, validationRef.Id,
		int64(validationRef.Revision), validationRef.ContentHash, warningAcknowledgementMaximum+1)
	if err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	refs := make([]*tammyv1.SourceRef, 0, warningAcknowledgementMaximum+1)
	for rows.Next() {
		var id string
		var contentHash []byte
		if err := rows.Scan(&id, &contentHash); err != nil {
			return nil, errors.Join(ErrWarningAcknowledgementRepository, err, rows.Close())
		}
		if !ids.IsCanonicalV7(id) || len(contentHash) != 32 {
			return nil, errors.Join(ErrWarningAcknowledgementRepository, ErrSnapshotCorrupt, rows.Close())
		}
		refs = append(refs, &tammyv1.SourceRef{Type: acknowledgementSnapshotResource, Id: id,
			Revision: 1, ContentHash: bytes.Clone(contentHash)})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
	}
	if len(refs) > warningAcknowledgementMaximum {
		return nil, errors.Join(ErrWarningAcknowledgementRepository, ErrSnapshotBound)
	}
	result := make([]*tammyv1.BasWarningAcknowledgement, 0, len(refs))
	for _, exact := range refs {
		value, err := repository.GetExact(ctx, organisationID, exact)
		if err != nil {
			return nil, errors.Join(ErrWarningAcknowledgementRepository, err)
		}
		if !sameBASSourceRef(value.Validation, validationRef) {
			return nil, errors.Join(ErrWarningAcknowledgementRepository, ErrSnapshotCorrupt)
		}
		result = append(result, value)
	}
	return result, nil
}

func warningAcknowledgementCurrentCaptureMatches(current *PreparationRetainedSnapshot,
	validation *ValidationRetainedSnapshot) bool {
	if current == nil || current.Preparation == nil || validation == nil || validation.Validation == nil ||
		!validBASSourceRef(validation.Preparation, preparationSnapshotSpec.resource, "") ||
		!validBASSourceRef(validation.Family, familyIdentitySnapshotSpec.resource, "") ||
		!validBASSourceRef(validation.Calculation, calculationSnapshotSpec.resource, "") ||
		!validBASSourceRef(validation.SavedPaygInputs, savedPaygSnapshotSpec.resource, "") {
		return false
	}
	prep := current.Preparation
	value := validation.Validation
	return prep.PreparationId == value.PreparationId &&
		prep.PreparationId == validation.Preparation.Id &&
		sameBASSourceRef(prep.Family, validation.Family) &&
		sameBASSourceRef(prep.CurrentCalculation, validation.Calculation) &&
		sameBASSourceRef(current.SavedPaygInputs, validation.SavedPaygInputs) &&
		prep.InputRevision == value.InputRevision && bytes.Equal(prep.InputHash, value.InputHash) &&
		bytes.Equal(prep.SourceSnapshotHash, value.SourceSnapshotHash)
}

type warningAcknowledgementReadRecord struct {
	id, organisationID, familyID, preparationID, validationID, findingID string
	actorUserID, acknowledgedAt                                          string
	validationRevision, wireLength                                       int64
	validationHash, findingHash, sourceSnapshotHash, contentHash, wire   []byte
}

const (
	validationSnapshotResource      = "bas_validation"
	acknowledgementSnapshotResource = "bas_warning_acknowledgement"
)
