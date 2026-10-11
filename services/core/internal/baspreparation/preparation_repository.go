package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
)

var (
	ErrPreparationRepository = errors.New("baspreparation: preparation repository operation failed")
	ErrPreparationConflict   = errors.New("baspreparation: preparation conflicts with the retained current version")
)

// PreparationRepository appends retained versions and advances their exact
// current pointer in the caller's transaction. Policy, audit and approval stay
// command-owned; any failed write requires the caller to roll back.
type PreparationRepository struct {
	executor app.CommandSQLExecutor
	reader   *PreparationReader
}

func NewPreparationRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*PreparationRepository, error) {
	reader, err := NewPreparationReader(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	return &PreparationRepository{executor: executor, reader: reader}, nil
}

func (repository *PreparationRepository) GetExact(ctx context.Context, org string, exact *tammyv1.SourceRef) (*PreparationRetainedSnapshot, error) {
	if repository == nil || repository.reader == nil {
		return nil, ErrPreparationRepository
	}
	return repository.reader.GetExact(ctx, org, exact)
}
func (repository *PreparationRepository) GetCurrent(ctx context.Context, org, id string) (*PreparationRetainedSnapshot, error) {
	if repository == nil || repository.reader == nil {
		return nil, ErrPreparationRepository
	}
	return repository.reader.GetCurrent(ctx, org, id)
}

func (repository *PreparationRepository) Append(ctx context.Context, retained *PreparationRetainedSnapshot, expectedCurrent *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if repository == nil || repository.reader == nil || !repository.reader.ready(ctx) || retained == nil || retained.Preparation == nil {
		return nil, ErrPreparationRepository
	}
	value := retained.Preparation
	wire, hash, err := sealPreparation(value)
	if err != nil {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	sealed := proto.Clone(value).(*tammyv1.BasPreparation)
	sealed.ContentHash = hash
	_, validInputRevision := basSQLiteRevision(value.InputRevision)
	createdAt, validCreatedAt := basTimestampSQLText(value.CreatedAt)
	updatedAt, validUpdatedAt := basTimestampSQLText(value.UpdatedAt)
	if !validPreparationIdentity(sealed) || !validInputRevision || !validCreatedAt || !validUpdatedAt ||
		!ids.IsCanonicalV7(retained.AttemptID) || (retained.BranchID != nil && !ids.IsCanonicalV7(*retained.BranchID)) ||
		!validPreparationPayload(retained) {
		return nil, ErrPreparationRepository
	}
	if err := repository.reader.verifyScopeOwners(ctx, value); err != nil {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	exists, err := repository.identityExists(ctx, value.PreparationId)
	if err != nil {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	if exists {
		current, err := repository.reader.GetCurrent(ctx, value.OrganisationId, value.PreparationId)
		if err != nil {
			return nil, errors.Join(ErrPreparationRepository, ErrSnapshotCorrupt, err)
		}
		prior := current.Preparation
		if !validBASSourceRef(expectedCurrent, "bas_preparation", value.PreparationId) ||
			!sameBASSourceRef(expectedCurrent, preparationReference(prior)) || value.Version != prior.Version+1 ||
			!sameBASSourceRef(value.Family, prior.Family) || retained.AttemptID != current.AttemptID ||
			!samePreparationBranch(retained.BranchID, current.BranchID) || !proto.Equal(value.CreatedAt, prior.CreatedAt) ||
			!sameOptionalRef(value.Predecessor, prior.Predecessor) || !validPreparationInputSuccessor(retained, current) {
			return nil, ErrPreparationConflict
		}
	} else {
		if expectedCurrent != nil || value.Version != 1 || value.InputRevision != 1 || value.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING ||
			retained.SavedPaygInputs != nil || value.CurrentCalculation != nil || value.CurrentValidation != nil || value.FrozenBas != nil || value.SupersededBy != nil {
			return nil, ErrPreparationConflict
		}
		if err := repository.verifyNewLineage(ctx, retained); err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
	}
	if err := repository.verifyPreparationDependencies(ctx, retained); err != nil {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	if !exists {
		var branch, predecessor any
		if retained.BranchID != nil {
			branch = *retained.BranchID
		}
		if value.Predecessor != nil {
			predecessor = value.Predecessor.Id
		}
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_preparation_identities_v1(preparation_id,organisation_id,family_id,branch_id,attempt_id,predecessor_id,created_at) VALUES(?,?,?,?,?,?,?)`,
			value.PreparationId, value.OrganisationId, value.Family.Id, branch, retained.AttemptID, predecessor, createdAt); err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
	}
	inputID, _, inputContentHash := sqlSourceValues(retained.SavedPaygInputs)
	calcID, calcVersion, calcHash := sqlSourceValues(value.CurrentCalculation)
	validationID, validationVersion, validationHash := sqlSourceValues(value.CurrentValidation)
	predID, predVersion, predHash := sqlSourceValues(value.Predecessor)
	nextID, nextVersion, nextHash := sqlSourceValues(value.SupersededBy)
	freezeID, _, freezeHash := sqlSourceValues(value.FrozenBas)
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,
		profile_id,profile_version,profile_hash,period_id,period_version,period_hash,input_snapshot_id,input_revision,input_hash,input_content_hash,
		calculation_id,calculation_version,calculation_hash,validation_id,validation_revision,validation_hash,predecessor_id,predecessor_version,predecessor_hash,
		superseded_by_preparation_id,superseded_by_version,superseded_by_hash,dispute_overlay,source_snapshot_hash,state,freeze_id,freeze_hash,content_hash,preparation_proto,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.PreparationId, value.OrganisationId, value.Family.Id, int64(value.Version), value.Profile.Id, int64(value.Profile.Revision), value.Profile.ContentHash,
		value.Period.Id, int64(value.Period.Revision), value.Period.ContentHash, inputID, int64(value.InputRevision), value.InputHash, inputContentHash,
		calcID, calcVersion, calcHash, validationID, validationVersion, validationHash, predID, predVersion, predHash, nextID, nextVersion, nextHash,
		enumSQL(value.DisputeOverlay.String(), "BAS_DISPUTE_OVERLAY_STATE_"), value.SourceSnapshotHash, enumSQL(value.State.String(), "BAS_PREPARATION_STATE_"), freezeID, freezeHash, hash, wire, createdAt, updatedAt); err != nil {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	if !exists {
		_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_preparation_heads_v1(preparation_id,organisation_id,current_version,current_hash,cas_version,updated_at) VALUES(?,?,1,?,1,?)`, value.PreparationId, value.OrganisationId, hash, updatedAt)
	} else {
		var result sql.Result
		result, err = repository.executor.ExecContext(ctx, `UPDATE bas_preparation_heads_v1 SET current_version=?,current_hash=?,cas_version=cas_version+1,updated_at=? WHERE preparation_id=? AND organisation_id=? AND current_version=? AND current_hash=? AND cas_version=?`,
			int64(value.Version), hash, updatedAt, value.PreparationId, value.OrganisationId, int64(expectedCurrent.Revision), expectedCurrent.ContentHash, int64(expectedCurrent.Revision))
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected != 1 {
				err = ErrPreparationConflict
			}
		}
	}
	if err != nil {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	return &tammyv1.SourceRef{Type: "bas_preparation", Id: value.PreparationId, Revision: value.Version, ContentHash: bytes.Clone(hash)}, nil
}

func (repository *PreparationRepository) identityExists(ctx context.Context, id string) (bool, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT 1 FROM bas_preparation_identities_v1 WHERE preparation_id=?`, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	exists := rows.Next()
	return exists, errors.Join(rows.Err(), rows.Close())
}

func samePreparationBranch(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func validPreparationPayload(retained *PreparationRetainedSnapshot) bool {
	value := retained.Preparation
	for _, item := range []struct {
		ref  *tammyv1.SourceRef
		kind string
	}{
		{value.Family, "bas_family"}, {value.Profile, "bas_obligations_profile"}, {value.Period, "bas_period"},
		{value.CurrentCalculation, "bas_calculation"}, {value.CurrentValidation, "bas_validation"},
		{value.FrozenBas, "bas_frozen"}, {value.Predecessor, "bas_preparation"}, {value.SupersededBy, "bas_preparation"},
		{retained.SavedPaygInputs, "bas_saved_payg_inputs"},
	} {
		if item.ref != nil && !validBASSourceRef(item.ref, item.kind, "") {
			return false
		}
	}
	if value.Family.Revision != 1 || (value.FrozenBas != nil && value.FrozenBas.Revision != 1) ||
		(value.Predecessor != nil && value.Predecessor.Id == value.PreparationId) || (value.SupersededBy != nil && value.SupersededBy.Id == value.PreparationId) ||
		(value.CurrentValidation != nil && value.CurrentCalculation == nil) || ((value.CurrentCalculation != nil || value.CurrentValidation != nil) && retained.SavedPaygInputs == nil) {
		return false
	}
	if value.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY && (value.CurrentCalculation == nil || value.CurrentValidation == nil) {
		return false
	}
	if value.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF || value.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
		return value.CurrentCalculation != nil && value.CurrentValidation != nil && value.FrozenBas != nil
	}
	switch value.State {
	case tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION:
		return value.CurrentCalculation != nil && value.CurrentValidation != nil && value.FrozenBas != nil
	}
	return value.FrozenBas == nil || (value.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING && value.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED && value.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY)
}

func validPreparationInputSuccessor(next, prior *PreparationRetainedSnapshot) bool {
	n, p := next.Preparation, prior.Preparation
	if n.InputRevision == p.InputRevision {
		if !bytes.Equal(n.InputHash, p.InputHash) {
			return false
		}
	} else if n.InputRevision != p.InputRevision+1 || bytes.Equal(n.InputHash, p.InputHash) ||
		n.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING || next.SavedPaygInputs != nil || n.CurrentCalculation != nil || n.CurrentValidation != nil || n.FrozenBas != nil {
		return false
	}
	if p.FrozenBas != nil || p.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION || p.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION || p.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION {
		return sameBASSourceRef(n.Profile, p.Profile) && sameBASSourceRef(n.Period, p.Period) && n.InputRevision == p.InputRevision && bytes.Equal(n.InputHash, p.InputHash) &&
			sameOptionalRef(next.SavedPaygInputs, prior.SavedPaygInputs) && sameOptionalRef(n.CurrentCalculation, p.CurrentCalculation) && sameOptionalRef(n.CurrentValidation, p.CurrentValidation) &&
			sameOptionalRef(n.FrozenBas, p.FrozenBas) && bytes.Equal(n.SourceSnapshotHash, p.SourceSnapshotHash)
	}
	return true
}

func (repository *PreparationRepository) verifyNewLineage(ctx context.Context, value *PreparationRetainedSnapshot) error {
	prep := value.Preparation
	if value.BranchID == nil {
		if prep.Predecessor != nil {
			return ErrPreparationConflict
		}
		rows, err := repository.executor.QueryContext(ctx, `SELECT 1 FROM bas_preparation_identities_v1 WHERE organisation_id=? AND family_id=? AND branch_id IS NULL LIMIT 1`, prep.OrganisationId, prep.Family.Id)
		if err != nil {
			return err
		}
		duplicate := rows.Next()
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if duplicate {
			return ErrPreparationConflict
		}
	} else {
		rows, err := repository.executor.QueryContext(ctx, `SELECT identity_hash FROM bas_branch_identities_v1 WHERE branch_id=? AND organisation_id=? AND family_id=?`, *value.BranchID, prep.OrganisationId, prep.Family.Id)
		if err != nil {
			return err
		}
		var hash []byte
		if !rows.Next() {
			rows.Close()
			return ErrPreparationConflict
		}
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return err
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		branches, _ := NewBranchIdentityRepository(repository.executor, repository.reader.cursors)
		identity, err := branches.GetExact(ctx, prep.OrganisationId, &tammyv1.SourceRef{Type: "bas_branch", Id: *value.BranchID, Revision: 1, ContentHash: hash})
		if err != nil {
			return err
		}
		if err := repository.verifyBranchPredecessor(ctx, prep, identity); err != nil {
			return err
		}
	}
	return nil
}

func (repository *PreparationRepository) verifyBranchPredecessor(ctx context.Context, prep *tammyv1.BasPreparation, identity *tammyv1.BasBranchIdentity) error {
	if prep.Predecessor == nil {
		return ErrPreparationConflict
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM bas_branch_heads_v1 WHERE organisation_id=? AND branch_id=?),
		EXISTS(SELECT 1 FROM bas_branch_versions_v1 WHERE organisation_id=? AND branch_id=?)`, prep.OrganisationId, identity.BranchId, prep.OrganisationId, identity.BranchId)
	if err != nil {
		return err
	}
	var hasHead, hasHistory bool
	if !rows.Next() {
		rows.Close()
		return ErrSnapshotCorrupt
	}
	if err := rows.Scan(&hasHead, &hasHistory); err != nil {
		rows.Close()
		return err
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if !hasHead {
		if hasHistory {
			return ErrSnapshotCorrupt
		}
		return repository.verifyInitialBranchPredecessor(ctx, prep, identity)
	}
	branches, _ := NewBranchStateRepository(repository.executor, repository.reader.cursors)
	workflow, err := branches.GetCurrent(ctx, prep.OrganisationId, &tammyv1.SourceRef{Type: "bas_branch", Id: identity.BranchId, Revision: 1, ContentHash: identity.ContentHash})
	if err != nil {
		return err
	}
	if !liveBranchState(workflow.WorkflowState) {
		return ErrPreparationConflict
	}
	if workflow.ActiveAttempt.GetRetained() == nil {
		// A reserved empty workflow can acquire its first attempt. If a prior
		// workflow had an attempt, retain that retry lineage instead of treating
		// the empty pointer as a new branch or forgetting its terminal owner.
		rows, err := repository.executor.QueryContext(ctx, `SELECT version,content_hash FROM bas_branch_versions_v1 WHERE organisation_id=? AND branch_id=? AND active_preparation_id IS NOT NULL ORDER BY version DESC LIMIT 1`, prep.OrganisationId, identity.BranchId)
		if err != nil {
			return err
		}
		if !rows.Next() {
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				return err
			}
			return repository.verifyInitialBranchPredecessor(ctx, prep, identity)
		}
		var version int64
		var hash []byte
		if err := rows.Scan(&version, &hash); err != nil {
			rows.Close()
			return err
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		workflow, err = branches.GetExact(ctx, prep.OrganisationId, &tammyv1.SourceRef{Type: "bas_branch_state", Id: identity.BranchId, Revision: uint64(version), ContentHash: hash})
		if err != nil {
			return err
		}
		if workflow.ActiveAttempt.GetRetained() == nil {
			return ErrSnapshotCorrupt
		}
	}
	prior, err := repository.reader.GetCurrent(ctx, prep.OrganisationId, workflow.ActiveAttempt.GetRetained().Id)
	if err != nil {
		return err
	}
	if !sameBASSourceRef(prep.Predecessor, preparationReference(prior.Preparation)) ||
		(prior.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION && prior.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION) {
		return ErrPreparationConflict
	}
	return nil
}

func (repository *PreparationRepository) verifyInitialBranchPredecessor(ctx context.Context, prep *tammyv1.BasPreparation, identity *tammyv1.BasBranchIdentity) error {
	if rootHead := identity.RootHead.GetRetained(); rootHead != nil {
		if !sameBASSourceRef(prep.Predecessor, rootHead) {
			return ErrPreparationConflict
		}
		return nil
	}
	// Before the first confirmed head, replacement lineage stays anchored
	// to the exact current version of the immutable collecting root.
	current, err := repository.reader.GetCurrent(ctx, prep.OrganisationId, identity.FamilyRootPreparation.Id)
	if err != nil {
		return err
	}
	if !sameBASSourceRef(prep.Predecessor, preparationReference(current.Preparation)) {
		return ErrPreparationConflict
	}
	return nil
}

func (repository *PreparationRepository) verifyPreparationDependencies(ctx context.Context, retained *PreparationRetainedSnapshot) error {
	value := retained.Preparation
	for _, ref := range []*tammyv1.SourceRef{value.Predecessor, value.SupersededBy} {
		if ref == nil {
			continue
		}
		parent, err := repository.reader.GetExact(ctx, value.OrganisationId, ref)
		if err != nil {
			return err
		}
		if !sameBASSourceRef(parent.Preparation.Family, value.Family) {
			return ErrSnapshotCorrupt
		}
	}
	if retained.SavedPaygInputs != nil {
		inputs, _ := NewSavedPaygRepository(repository.executor, repository.reader.cursors)
		pair, err := inputs.GetExact(ctx, value.OrganisationId, retained.SavedPaygInputs)
		if err != nil {
			return err
		}
		if pair.PreparationId != value.PreparationId || pair.Preparation.Revision >= value.Version || pair.InputRevision != value.InputRevision || !bytes.Equal(pair.InputHash, value.InputHash) {
			return ErrPreparationConflict
		}
	}
	if value.CurrentCalculation != nil {
		calculations, err := NewCalculationRepository(repository.executor, repository.reader.cursors)
		if err != nil {
			return err
		}
		calc, err := calculations.GetExact(ctx, value.OrganisationId, value.CurrentCalculation)
		if err != nil {
			return err
		}
		if calc.Preparation.Id != value.PreparationId || !sameBASSourceRef(calc.Family, value.Family) || calc.Preparation.Revision >= value.Version ||
			!sameBASSourceRef(calc.Profile, value.Profile) || !sameBASSourceRef(calc.Period, value.Period) || calc.InputRevision != value.InputRevision ||
			!bytes.Equal(calc.InputHash, value.InputHash) || !bytes.Equal(calc.SourceSnapshotHash, value.SourceSnapshotHash) {
			return ErrPreparationConflict
		}
		// A same-pair envelope still has its own actor/time identity. Verify the
		// exact retained envelope attached to the capture, not just its pair hash.
		capture, err := repository.reader.GetExact(ctx, value.OrganisationId, calc.Preparation)
		if err != nil {
			return err
		}
		if !sameBASSourceRef(capture.SavedPaygInputs, retained.SavedPaygInputs) {
			return ErrPreparationConflict
		}
	}
	if value.CurrentValidation != nil {
		if err := repository.verifyValidationProof(ctx, retained); err != nil {
			return err
		}
	}
	if value.FrozenBas != nil {
		proof := lifecycleRepository{repository.executor, repository.reader.cursors}
		if err := proof.verifyFrozenBarrier(ctx, value.OrganisationId, value.Family, value, value.FrozenBas); err != nil {
			return err
		}
	}
	return nil
}
