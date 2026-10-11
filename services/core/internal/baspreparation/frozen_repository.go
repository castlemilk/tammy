package baspreparation

import (
	"bytes"
	"context"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
)

var (
	ErrFrozenRepository = errors.New("baspreparation: frozen repository operation failed")
	ErrFrozenConflict   = errors.New("baspreparation: frozen approval conflicts with its reviewed owner")
)

// FrozenRepository stores an exact reviewed approval in a caller-owned
// transaction. Commands supply authority, live capture, factor consumption and
// the preparation/family CAS updates; this repository verifies retained facts.
type FrozenRepository struct {
	executor         app.CommandSQLExecutor
	cursors          *paging.Codec
	preparations     *PreparationReader
	calculations     *CalculationRepository
	validations      *ValidationRepository
	acknowledgements *WarningAcknowledgementRepository
}

func NewFrozenRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*FrozenRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrFrozenRepository
	}
	preparations, err := NewPreparationReader(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	calculations, err := NewCalculationRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	validations, err := NewValidationRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	acknowledgements, err := NewWarningAcknowledgementRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	return &FrozenRepository{executor: executor, cursors: cursors, preparations: preparations,
		calculations: calculations, validations: validations, acknowledgements: acknowledgements}, nil
}

func (repository *FrozenRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && repository.cursors != nil &&
		repository.preparations != nil && repository.calculations != nil && repository.validations != nil &&
		repository.acknowledgements != nil && ctx != nil && ctx.Err() == nil
}

func (repository *FrozenRepository) Append(ctx context.Context, organisationID string,
	value *tammyv1.BasFrozenBas, currentPreparationRef *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) || value == nil || value.OrganisationId != organisationID ||
		!validBASSourceRef(currentPreparationRef, preparationSnapshotSpec.resource, "") ||
		!sameBASSourceRef(value.Preparation, currentPreparationRef) {
		return nil, ErrFrozenRepository
	}
	wire, digest, err := sealFrozen(value)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	current, err := repository.preparations.GetCurrent(ctx, organisationID, currentPreparationRef.Id)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	if !sameBASSourceRef(preparationReference(current.Preparation), currentPreparationRef) {
		return nil, ErrFrozenConflict
	}
	ackRefs, err := repository.verifyRetainedApproval(ctx, organisationID, value, current)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, ErrFrozenConflict, err)
	}
	calculationWire, _, err := sealCalculation(value.CalculationSnapshot)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	approvedAt, ok := basTimestampSQLText(value.ApprovedAt)
	if !ok {
		return nil, ErrFrozenRepository
	}
	const savepoint = "bas_frozen_append"
	if _, err := repository.executor.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	failed := func(err error) (*tammyv1.SourceRef, error) {
		return nil, errors.Join(ErrFrozenRepository, err, rollbackRepositorySavepoint(ctx, repository.executor, savepoint))
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_frozen_bas_v1(
		frozen_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash,
		profile_id,profile_version,profile_hash,period_id,period_version,period_hash,
		calculation_id,calculation_version,calculation_hash,validation_id,validation_revision,validation_hash,
		source_snapshot_hash,approved_snapshot_hash,content_hash,calculation_snapshot_proto,frozen_proto,
		approval_actor_user_id,fresh_factor_assertion_id,approved_at,approval_reason)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.FrozenId, organisationID, value.Family.Id,
		value.Preparation.Id, int64(value.Preparation.Revision), value.Preparation.ContentHash,
		value.Profile.Id, int64(value.Profile.Revision), value.Profile.ContentHash,
		value.Period.Id, int64(value.Period.Revision), value.Period.ContentHash,
		value.Calculation.Id, int64(value.Calculation.Revision), value.Calculation.ContentHash,
		value.Validation.Id, int64(value.Validation.Revision), value.Validation.ContentHash,
		value.CalculationSnapshot.SourceSnapshotHash, value.ApprovedSnapshotHash, digest, calculationWire, wire,
		value.ApprovalActorUserId, value.FreshFactorAssertionId, approvedAt, value.ApprovalReason); err != nil {
		return failed(err)
	}
	for _, ack := range ackRefs {
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_frozen_acknowledgements_v1(
			frozen_id,frozen_hash,organisation_id,family_id,acknowledgement_id,acknowledgement_hash)
			VALUES(?,?,?,?,?,?)`, value.FrozenId, digest, organisationID, value.Family.Id, ack.Id, ack.ContentHash); err != nil {
			return failed(err)
		}
	}
	if _, err := repository.executor.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return failed(err)
	}
	return &tammyv1.SourceRef{Type: frozenSnapshotSpec.resource, Id: value.FrozenId, Revision: 1, ContentHash: bytes.Clone(digest)}, nil
}

// GetExact follows only the retained pre-freeze owner and its exact dependency
// refs. Later workflow pointers cannot alter a frozen approval's meaning.
func (repository *FrozenRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasFrozenBas, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) ||
		!validBASSourceRef(exact, frozenSnapshotSpec.resource, "") || exact.Revision != 1 {
		return nil, ErrFrozenRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT f.family_id,i.identity_hash
		FROM bas_frozen_bas_v1 f JOIN bas_family_identities_v1 i ON i.family_id=f.family_id AND i.organisation_id=f.organisation_id
		WHERE f.frozen_id=? AND f.organisation_id=?`, exact.Id, organisationID)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	var familyID string
	var familyHash []byte
	if !rows.Next() {
		return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&familyID, &familyHash); err != nil {
		return nil, errors.Join(ErrFrozenRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	family := &tammyv1.SourceRef{Type: "bas_family", Id: familyID, Revision: 1, ContentHash: bytes.Clone(familyHash)}
	proof := lifecycleRepository{repository.executor, repository.cursors}
	value, err := proof.readFrozenSnapshot(ctx, organisationID, family, exact)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, err)
	}
	owner, err := repository.preparations.GetExact(ctx, organisationID, value.Preparation)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, err)
	}
	ackRefs, err := repository.verifyRetainedApproval(ctx, organisationID, value, owner)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, err)
	}
	if err := repository.verifyAcknowledgementLinks(ctx, organisationID, exact, value.Family, ackRefs); err != nil {
		return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, err)
	}
	return value, nil
}

func (repository *FrozenRepository) verifyRetainedApproval(ctx context.Context, org string,
	value *tammyv1.BasFrozenBas, owner *PreparationRetainedSnapshot) ([]*tammyv1.SourceRef, error) {
	if owner == nil || owner.Preparation == nil || value == nil ||
		owner.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		!sameBASSourceRef(preparationReference(owner.Preparation), value.Preparation) ||
		!sameBASSourceRef(owner.Preparation.Family, value.Family) ||
		!sameBASSourceRef(owner.Preparation.Profile, value.Profile) || !sameBASSourceRef(owner.Preparation.Period, value.Period) ||
		!sameBASSourceRef(owner.Preparation.CurrentCalculation, value.Calculation) ||
		!sameBASSourceRef(owner.Preparation.CurrentValidation, value.Validation) {
		return nil, ErrSnapshotCorrupt
	}
	calculation, err := repository.calculations.GetExact(ctx, org, value.Calculation)
	if err != nil {
		return nil, err
	}
	if !proto.Equal(calculation, value.CalculationSnapshot) {
		return nil, ErrSnapshotCorrupt
	}
	validation, err := repository.validations.GetRetained(ctx, org, value.Validation)
	if err != nil {
		return nil, err
	}
	if !warningAcknowledgementCurrentCaptureMatches(owner, validation) {
		return nil, ErrSnapshotCorrupt
	}
	retained, err := repository.acknowledgements.ListForValidation(ctx, org, value.Validation)
	if err != nil {
		return nil, err
	}
	if len(retained) != len(value.Acknowledgements) {
		return nil, ErrSnapshotCorrupt
	}
	byID := make(map[string]*tammyv1.BasWarningAcknowledgement, len(retained))
	for _, ack := range retained {
		byID[ack.AcknowledgementId] = ack
	}
	validationOwner, err := repository.preparations.GetExact(ctx, org, validation.Preparation)
	if err != nil {
		return nil, err
	}
	refs := make([]*tammyv1.SourceRef, 0, len(retained))
	for _, ack := range value.Acknowledgements {
		if ack == nil || byID[ack.AcknowledgementId] == nil || !proto.Equal(ack, byID[ack.AcknowledgementId]) {
			return nil, ErrSnapshotCorrupt
		}
		delete(byID, ack.AcknowledgementId)
		_, digest, err := sealAcknowledgement(ack, validation.Validation, validationOwner.Preparation)
		if err != nil {
			return nil, err
		}
		refs = append(refs, &tammyv1.SourceRef{Type: acknowledgementSnapshotResource, Id: ack.AcknowledgementId, Revision: 1, ContentHash: bytes.Clone(digest)})
	}
	assessment, err := AssessReview(ReviewInput{Preparation: owner.Preparation, Calculation: calculation,
		Validation: validation.Validation, Acknowledgements: value.Acknowledgements,
		CurrentCapture: CurrentCapture{SourceSnapshotHash: owner.Preparation.SourceSnapshotHash,
			InputRevision: owner.Preparation.InputRevision, InputHash: owner.Preparation.InputHash, RevisionVector: calculation.RevisionVector}})
	if err != nil || !assessment.CanFreeze {
		return nil, errors.Join(ErrFreezeNotReady, err)
	}
	return refs, nil
}

func (repository *FrozenRepository) verifyAcknowledgementLinks(ctx context.Context, org string,
	frozen, family *tammyv1.SourceRef, expected []*tammyv1.SourceRef) error {
	rows, err := repository.executor.QueryContext(ctx, `SELECT frozen_hash,organisation_id,family_id,acknowledgement_id,acknowledgement_hash
		FROM bas_frozen_acknowledgements_v1 WHERE frozen_id=? ORDER BY acknowledgement_id LIMIT ?`, frozen.Id, warningAcknowledgementMaximum+1)
	if err != nil {
		return err
	}
	byID := make(map[string]*tammyv1.SourceRef, len(expected))
	for _, ref := range expected {
		byID[ref.Id] = ref
	}
	count := 0
	for rows.Next() {
		var rowOrg, rowFamily, id string
		var frozenHash, hash []byte
		if err := rows.Scan(&frozenHash, &rowOrg, &rowFamily, &id, &hash); err != nil {
			return errors.Join(err, rows.Close())
		}
		count++
		ref := byID[id]
		if count > warningAcknowledgementMaximum || ref == nil || rowOrg != org || rowFamily != family.Id ||
			!bytes.Equal(frozenHash, frozen.ContentHash) || !bytes.Equal(hash, ref.ContentHash) {
			return errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		delete(byID, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(byID) != 0 || count != len(expected) {
		return ErrSnapshotCorrupt
	}
	return nil
}
