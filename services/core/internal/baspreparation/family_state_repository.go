package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// Append atomically creates or compare-and-swaps the mutable family aggregate.
// expectedCurrent is a bas_family_state reference, not its immutable family ID.
func (repository *FamilyStateRepository) Append(ctx context.Context, org string, value *tammyv1.BasFamilyState, expectedCurrent *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(org) || value == nil {
		return nil, ErrFamilyStateRepository
	}
	identity, err := repository.family(ctx, org, value.Family)
	if err != nil {
		return nil, errors.Join(ErrFamilyStateRepository, err)
	}
	wire, hash, err := sealFamilyState(value, identity)
	if err != nil {
		return nil, errors.Join(ErrFamilyStateRepository, err)
	}
	version, validVersion := basSQLiteRevision(value.Version)
	updatedAt, validTime := basTimestampSQLText(value.UpdatedAt)
	if !validVersion || !validTime || value.LastFamilyEventOrdinal > math.MaxInt64 || !exactExpectedPreparation(value.LatestHead) || !exactExpectedBranch(value.ActiveBranch) {
		return nil, ErrFamilyStateRepository
	}
	current, found, err := repository.loadCurrent(ctx, org, value.Family)
	if err != nil {
		return nil, errors.Join(ErrFamilyStateRepository, err)
	}
	if !found {
		if expectedCurrent != nil || value.Version != 1 || value.EffectiveOriginal != nil || value.LatestHead.GetRetained() != nil || value.ActiveBranch.GetRetained() != nil || value.LastFamilyEventOrdinal != 0 || value.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE {
			return nil, ErrFamilyStateConflict
		}
	} else if !sameBASSourceRef(expectedCurrent, familyStateReference(current)) || current.Version == math.MaxInt64 || value.Version != current.Version+1 ||
		!sameBASSourceRef(value.RootPreparation, current.RootPreparation) || value.LastFamilyEventOrdinal < current.LastFamilyEventOrdinal ||
		(current.LatestHead.GetRetained() != nil && value.LatestHead.GetRetained() == nil) ||
		(current.EffectiveOriginal != nil && !sameBASSourceRef(value.EffectiveOriginal, current.EffectiveOriginal)) {
		return nil, ErrFamilyStateConflict
	}
	if err := repository.verifyFamilyParents(ctx, org, value); err != nil {
		return nil, errors.Join(ErrFamilyStateRepository, err)
	}
	rootID, rootVersion, rootHash := sqlSourceValues(value.RootPreparation)
	originalID, originalVersion, originalHash := sqlSourceValues(value.EffectiveOriginal)
	headID, headVersion, headHash := sqlSourceValues(value.LatestHead.GetRetained())
	var branchID, branchIdentityHash, branchVersion, branchHash any
	if ref := value.ActiveBranch.GetRetained(); ref != nil {
		state, err := (&BranchStateRepository{repository.lifecycleRepository}).getVersion(ctx, org, ref, value.ActiveBranch.GetExpectedVersion())
		if err != nil {
			return nil, errors.Join(ErrFamilyStateRepository, err)
		}
		branchID, branchIdentityHash, branchVersion, branchHash = ref.Id, ref.ContentHash, int64(state.Version), state.ContentHash
	}
	args := []any{rootID, rootVersion, rootHash, originalID, originalVersion, originalHash, headID, headVersion, headHash,
		branchID, branchIdentityHash, branchVersion, branchHash, enumSQL(value.DisputeOverlay.String(), "BAS_DISPUTE_OVERLAY_STATE_"), wire, hash,
		version, int64(value.LastFamilyEventOrdinal), updatedAt, value.Family.Id, org}
	if !found {
		_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_family_state_v1(
			root_preparation_id,root_version,root_hash,effective_original_preparation_id,effective_original_version,effective_original_hash,
			head_preparation_id,head_version,head_hash,active_branch_id,active_branch_identity_hash,active_branch_version,active_branch_hash,
			overlay_state,family_state_proto,state_hash,cas_version,last_family_event_ordinal,updated_at,family_id,organisation_id)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	} else {
		args = append(args, int64(current.Version), current.ContentHash)
		result, updateErr := repository.executor.ExecContext(ctx, `UPDATE bas_family_state_v1 SET
			root_preparation_id=?,root_version=?,root_hash=?,effective_original_preparation_id=?,effective_original_version=?,effective_original_hash=?,
			head_preparation_id=?,head_version=?,head_hash=?,active_branch_id=?,active_branch_identity_hash=?,active_branch_version=?,active_branch_hash=?,
			overlay_state=?,family_state_proto=?,state_hash=?,cas_version=?,last_family_event_ordinal=?,updated_at=?
			WHERE family_id=? AND organisation_id=? AND cas_version=? AND state_hash=?`, args...)
		if updateErr != nil {
			return nil, errors.Join(ErrFamilyStateRepository, updateErr)
		}
		changed, affectedErr := result.RowsAffected()
		if affectedErr != nil || changed != 1 {
			return nil, errors.Join(ErrFamilyStateConflict, affectedErr)
		}
	}
	if err != nil {
		return nil, errors.Join(ErrFamilyStateRepository, err)
	}
	return &tammyv1.SourceRef{Type: "bas_family_state", Id: value.Family.Id, Revision: value.Version, ContentHash: bytes.Clone(hash)}, nil
}

// Family state is mutable; only its current body is exposed. Historical chain
// facts are retained by their own repositories and never resolved through it.
func (repository *FamilyStateRepository) GetCurrent(ctx context.Context, org string, family *tammyv1.SourceRef) (*tammyv1.BasFamilyState, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(org) {
		return nil, ErrFamilyStateRepository
	}
	value, found, err := repository.loadCurrent(ctx, org, family)
	if err != nil || !found {
		return nil, errors.Join(ErrFamilyStateRepository, err)
	}
	return value, nil
}

func (repository *FamilyStateRepository) loadCurrent(ctx context.Context, org string, family *tammyv1.SourceRef) (*tammyv1.BasFamilyState, bool, error) {
	identity, err := repository.family(ctx, org, family)
	if err != nil {
		return nil, false, err
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT root_preparation_id,root_version,root_hash,
		effective_original_preparation_id,effective_original_version,effective_original_hash,head_preparation_id,head_version,head_hash,
		active_branch_id,active_branch_identity_hash,active_branch_version,active_branch_hash,overlay_state,
		CASE WHEN length(family_state_proto)<=? THEN family_state_proto ELSE NULL END,length(family_state_proto),state_hash,cas_version,last_family_event_ordinal,updated_at
		FROM bas_family_state_v1 WHERE family_id=? AND organisation_id=?`, scalarSnapshotLimit, family.Id, org)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, errors.Join(rows.Err(), rows.Close())
	}
	var root, original, head preparationReadSource
	var branchID sql.NullString
	var branchVersion sql.NullInt64
	var branchIdentityHash, branchHash, wire, hash []byte
	var overlay, updatedAt string
	var length, version, ordinal int64
	if err := rows.Scan(&root.id, &root.version, &root.hash, &original.id, &original.version, &original.hash, &head.id, &head.version, &head.hash,
		&branchID, &branchIdentityHash, &branchVersion, &branchHash, &overlay, &wire, &length, &hash, &version, &ordinal, &updatedAt); err != nil {
		return nil, false, err
	}
	if rows.Next() {
		return nil, false, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, false, err
	}
	if err := repositorySnapshotBytes(wire, length, scalarSnapshotLimit); err != nil {
		return nil, false, err
	}
	if version <= 0 || ordinal < 0 {
		return nil, false, ErrSnapshotCorrupt
	}
	exact := &tammyv1.SourceRef{Type: "bas_family_state", Id: family.Id, Revision: uint64(version), ContentHash: hash}
	value, err := openFamilyState(wire, org, exact, identity)
	if err != nil {
		return nil, false, err
	}
	timeText, validTime := basTimestampSQLText(value.UpdatedAt)
	if !root.matches(value.RootPreparation, "bas_preparation") || !original.matches(value.EffectiveOriginal, "bas_preparation") ||
		!exactExpectedPreparation(value.LatestHead) || !head.matches(value.LatestHead.GetRetained(), "bas_preparation") || !exactExpectedBranch(value.ActiveBranch) ||
		!validTime || timeText != updatedAt || value.LastFamilyEventOrdinal != uint64(ordinal) || enumSQL(value.DisputeOverlay.String(), "BAS_DISPUTE_OVERLAY_STATE_") != overlay {
		return nil, false, ErrSnapshotCorrupt
	}
	if ref := value.ActiveBranch.GetRetained(); ref != nil {
		if !branchID.Valid || !branchVersion.Valid || branchVersion.Int64 <= 0 || ref.Id != branchID.String ||
			!bytes.Equal(ref.ContentHash, branchIdentityHash) || value.ActiveBranch.GetExpectedVersion() != uint64(branchVersion.Int64) {
			return nil, false, ErrSnapshotCorrupt
		}
		state, err := (&BranchStateRepository{repository.lifecycleRepository}).getVersion(ctx, org, ref, uint64(branchVersion.Int64))
		if err != nil {
			return nil, false, err
		}
		if !bytes.Equal(state.ContentHash, branchHash) {
			return nil, false, ErrSnapshotCorrupt
		}
	} else if branchID.Valid || branchVersion.Valid || branchIdentityHash != nil || branchHash != nil {
		return nil, false, ErrSnapshotCorrupt
	}
	if err := repository.verifyFamilyParents(ctx, org, value); err != nil {
		return nil, false, err
	}
	return value, true, nil
}
func (repository *FamilyStateRepository) verifyFamilyParents(ctx context.Context, org string, value *tammyv1.BasFamilyState) error {
	if err := repository.chainParents(ctx, org, value.Family, value.RootPreparation, value.EffectiveOriginal, value.LatestHead.GetRetained()); err != nil {
		return err
	}
	if ref := value.ActiveBranch.GetRetained(); ref != nil {
		state, err := (&BranchStateRepository{repository.lifecycleRepository}).getVersion(ctx, org, ref, value.ActiveBranch.GetExpectedVersion())
		if err != nil {
			return err
		}
		identity, err := (&BranchIdentityRepository{repository.lifecycleRepository}).GetExact(ctx, org, ref)
		if err != nil {
			return err
		}
		if !sameBASSourceRef(identity.Family, value.Family) || !sameBASSourceRef(identity.FamilyRootPreparation, value.RootPreparation) || !liveBranchState(state.WorkflowState) {
			return ErrSnapshotCorrupt
		}
	}
	if value.LastFamilyEventOrdinal != 0 {
		return repository.verifyFamilyEvent(ctx, org, value.Family, value.LastFamilyEventOrdinal)
	}
	return nil
}
