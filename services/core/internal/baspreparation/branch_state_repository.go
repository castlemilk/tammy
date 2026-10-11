package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

type branchRepositoryHead struct {
	version, casVersion int64
	hash                []byte
	familyID, updatedAt string
}

func (repository *BranchStateRepository) head(ctx context.Context, org, id string) (branchRepositoryHead, bool, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT current_version,current_hash,cas_version,family_id,updated_at FROM bas_branch_heads_v1 WHERE branch_id=? AND organisation_id=?`, id, org)
	if err != nil {
		return branchRepositoryHead{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return branchRepositoryHead{}, false, errors.Join(rows.Err(), rows.Close())
	}
	var head branchRepositoryHead
	if err := rows.Scan(&head.version, &head.hash, &head.casVersion, &head.familyID, &head.updatedAt); err != nil {
		return head, false, err
	}
	if rows.Next() {
		return head, false, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return head, false, err
	}
	if head.version <= 0 || head.casVersion != head.version || len(head.hash) != sha256.Size {
		return head, false, ErrSnapshotCorrupt
	}
	return head, true, nil
}
func (repository *BranchStateRepository) GetCurrent(ctx context.Context, org string, identity *tammyv1.SourceRef) (*tammyv1.BasBranchState, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(identity, "bas_branch", "") || identity.Revision != 1 {
		return nil, ErrBranchStateRepository
	}
	head, found, err := repository.head(ctx, org, identity.Id)
	if err != nil || !found {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	value, err := repository.GetExact(ctx, org, &tammyv1.SourceRef{Type: "bas_branch_state", Id: identity.Id, Revision: uint64(head.version), ContentHash: head.hash})
	if err != nil {
		return nil, err
	}
	timeText, validTime := basTimestampSQLText(value.UpdatedAt)
	owner, err := (&BranchIdentityRepository{repository.lifecycleRepository}).GetExact(ctx, org, identity)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	if !sameBASSourceRef(value.BranchIdentity, identity) || head.familyID != owner.Family.Id || !validTime || timeText != head.updatedAt {
		return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt)
	}
	return value, nil
}

// getVersion resolves a family aggregate's exact retained workflow pin. It
// deliberately never substitutes the branch's current workflow version.
func (repository *BranchStateRepository) getVersion(ctx context.Context, org string, identity *tammyv1.SourceRef, version uint64) (*tammyv1.BasBranchState, error) {
	if !validBASSourceRef(identity, "bas_branch", "") || identity.Revision != 1 {
		return nil, ErrSnapshotCorrupt
	}
	state, err := repository.readVersion(ctx, org, identity.Id, version, nil)
	if err != nil {
		return nil, err
	}
	if !sameBASSourceRef(state.BranchIdentity, identity) {
		return nil, ErrSnapshotCorrupt
	}
	return state, nil
}
func (repository *BranchStateRepository) GetExact(ctx context.Context, org string, exact *tammyv1.SourceRef) (*tammyv1.BasBranchState, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, "bas_branch_state", "") {
		return nil, ErrBranchStateRepository
	}
	value, err := repository.readVersion(ctx, org, exact.Id, exact.Revision, exact)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	return value, nil
}
func (repository *BranchStateRepository) readVersion(ctx context.Context, org, id string, version uint64, exact *tammyv1.SourceRef) (*tammyv1.BasBranchState, error) {
	revision, validVersion := basSQLiteRevision(version)
	if !validVersion {
		return nil, ErrSnapshotCorrupt
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT v.family_id,i.identity_hash,v.workflow_state,v.active_attempt_id,
		v.active_preparation_id,v.active_attempt_version,v.active_attempt_hash,v.corrective_resolution_id,v.corrective_resolution_hash,
		v.barrier_preparation_id,v.barrier_preparation_version,v.barrier_preparation_hash,v.barrier_attempt_id,v.barrier_frozen_id,v.barrier_frozen_hash,
		v.content_hash,CASE WHEN length(v.branch_state_proto)<=? THEN v.branch_state_proto ELSE NULL END,length(v.branch_state_proto),v.created_at
		FROM bas_branch_versions_v1 v JOIN bas_branch_identities_v1 i ON i.branch_id=v.branch_id AND i.organisation_id=v.organisation_id AND i.family_id=v.family_id
		WHERE v.branch_id=? AND v.organisation_id=? AND v.version=?`, scalarSnapshotLimit, id, org, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	var familyID, workflow, updatedAt string
	var identityHash, hash, wire, resolutionHash, frozenHash []byte
	var length int64
	var active, barrier preparationReadSource
	var activeAttempt, barrierAttempt, resolutionID, frozenID sql.NullString
	if err := rows.Scan(&familyID, &identityHash, &workflow, &activeAttempt, &active.id, &active.version, &active.hash,
		&resolutionID, &resolutionHash, &barrier.id, &barrier.version, &barrier.hash, &barrierAttempt, &frozenID, &frozenHash,
		&hash, &wire, &length, &updatedAt); err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	if err := repositorySnapshotBytes(wire, length, scalarSnapshotLimit); err != nil {
		return nil, err
	}
	actual := &tammyv1.SourceRef{Type: "bas_branch_state", Id: id, Revision: version, ContentHash: hash}
	if exact != nil && !sameBASSourceRef(exact, actual) {
		return nil, ErrSnapshotCorrupt
	}
	identityRef := &tammyv1.SourceRef{Type: "bas_branch", Id: id, Revision: 1, ContentHash: identityHash}
	identity, err := (&BranchIdentityRepository{repository.lifecycleRepository}).GetExact(ctx, org, identityRef)
	if err != nil {
		return nil, err
	}
	value, err := openBranchState(wire, org, actual, identity)
	if err != nil {
		return nil, err
	}
	timeText, validTime := basTimestampSQLText(value.UpdatedAt)
	if identity.Family.Id != familyID || enumSQL(value.WorkflowState.String(), "BAS_BRANCH_WORKFLOW_STATE_") != workflow ||
		!exactExpectedPreparation(value.ActiveAttempt) || !active.matches(value.ActiveAttempt.GetRetained(), "bas_preparation") ||
		!barrier.matches(value.BarrierPreparation, "bas_preparation") || !singleSnapshotSQLMatches(resolutionID, resolutionHash, value.Resolution, "bas_external_dispute_resolution") ||
		!singleSnapshotSQLMatches(frozenID, frozenHash, value.FrozenBarrier, "bas_frozen") || !validTime || timeText != updatedAt {
		return nil, ErrSnapshotCorrupt
	}
	derivedActive, derivedBarrier, err := repository.verifyBranchParents(ctx, org, identity, value)
	if err != nil {
		return nil, err
	}
	if !optionalStringMatches(activeAttempt, derivedActive) || !optionalStringMatches(barrierAttempt, derivedBarrier) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
func singleSnapshotSQLMatches(id sql.NullString, hash []byte, ref *tammyv1.SourceRef, resource string) bool {
	if ref == nil {
		return !id.Valid && hash == nil
	}
	return id.Valid && ref.Revision == 1 && validBASSourceRef(ref, resource, id.String) && bytes.Equal(hash, ref.ContentHash)
}
func optionalStringMatches(value sql.NullString, expected string) bool {
	return (expected == "" && !value.Valid) || (expected != "" && value.Valid && value.String == expected)
}

func (repository *BranchStateRepository) Append(ctx context.Context, org string, value *tammyv1.BasBranchState, expectedCurrent *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(org) || value == nil {
		return nil, ErrBranchStateRepository
	}
	identity, err := (&BranchIdentityRepository{repository.lifecycleRepository}).GetExact(ctx, org, value.BranchIdentity)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	wire, hash, err := sealBranchState(value, identity)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	version, validVersion := basSQLiteRevision(value.Version)
	updatedAt, validTime := basTimestampSQLText(value.UpdatedAt)
	if !validVersion || !validTime {
		return nil, ErrBranchStateRepository
	}
	head, found, err := repository.head(ctx, org, identity.BranchId)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	if !found {
		rows, err := repository.executor.QueryContext(ctx, `SELECT 1 FROM bas_branch_versions_v1 WHERE branch_id=? LIMIT 1`, identity.BranchId)
		if err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		hasHistory := rows.Next()
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		if hasHistory {
			return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt)
		}
		if expectedCurrent != nil || value.Version != 1 || value.WorkflowState != tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE || value.Resolution != nil || value.FrozenBarrier != nil {
			return nil, ErrBranchStateConflict
		}
	} else {
		current, err := repository.GetCurrent(ctx, org, value.BranchIdentity)
		if err != nil {
			return nil, err
		}
		if !sameBASSourceRef(expectedCurrent, branchStateReference(current)) || head.version == math.MaxInt64 || version != head.version+1 {
			return nil, ErrBranchStateConflict
		}
	}
	activeAttempt, barrierAttempt, err := repository.verifyBranchParents(ctx, org, identity, value)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	if active := value.ActiveAttempt.GetRetained(); active != nil {
		reader := &PreparationReader{executor: repository.executor, cursors: repository.cursors}
		current, err := reader.GetCurrent(ctx, org, active.Id)
		if err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		if !sameBASSourceRef(active, preparationReference(current.Preparation)) {
			return nil, ErrBranchStateConflict
		}
	}
	activeID, activeVersion, activeHash := sqlSourceValues(value.ActiveAttempt.GetRetained())
	barrierID, barrierVersion, barrierHash := sqlSourceValues(value.BarrierPreparation)
	var activeAttemptSQL, barrierAttemptSQL, resolutionID, resolutionHash, frozenID, frozenHash any
	if activeAttempt != "" {
		activeAttemptSQL = activeAttempt
	}
	if barrierAttempt != "" {
		barrierAttemptSQL = barrierAttempt
	}
	if value.Resolution != nil {
		resolutionID, resolutionHash = value.Resolution.Id, value.Resolution.ContentHash
	}
	if value.FrozenBarrier != nil {
		frozenID, frozenHash = value.FrozenBarrier.Id, value.FrozenBarrier.ContentHash
	}
	_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_branch_versions_v1(branch_id,family_id,organisation_id,version,workflow_state,
		active_attempt_id,active_preparation_id,active_attempt_version,active_attempt_hash,corrective_resolution_id,corrective_resolution_hash,
		barrier_preparation_id,barrier_preparation_version,barrier_preparation_hash,barrier_attempt_id,barrier_frozen_id,barrier_frozen_hash,
		content_hash,branch_state_proto,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		identity.BranchId, identity.Family.Id, org, version, enumSQL(value.WorkflowState.String(), "BAS_BRANCH_WORKFLOW_STATE_"),
		activeAttemptSQL, activeID, activeVersion, activeHash, resolutionID, resolutionHash, barrierID, barrierVersion, barrierHash, barrierAttemptSQL, frozenID, frozenHash, hash, wire, updatedAt)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	if !found {
		_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_branch_heads_v1(branch_id,family_id,organisation_id,current_version,current_hash,cas_version,updated_at) VALUES(?,?,?,?,?,1,?)`,
			identity.BranchId, identity.Family.Id, org, version, hash, updatedAt)
		if err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
	} else {
		result, err := repository.executor.ExecContext(ctx, `UPDATE bas_branch_heads_v1 SET current_version=?,current_hash=?,cas_version=cas_version+1,updated_at=?
			WHERE branch_id=? AND family_id=? AND organisation_id=? AND current_version=? AND current_hash=? AND cas_version=?`,
			version, hash, updatedAt, identity.BranchId, identity.Family.Id, org, head.version, head.hash, head.casVersion)
		if err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return nil, errors.Join(ErrBranchStateConflict, err)
		}
	}
	return &tammyv1.SourceRef{Type: "bas_branch_state", Id: identity.BranchId, Revision: value.Version, ContentHash: bytes.Clone(hash)}, nil
}

func (repository *BranchStateRepository) verifyBranchParents(ctx context.Context, org string, identity *tammyv1.BasBranchIdentity, value *tammyv1.BasBranchState) (string, string, error) {
	if !exactExpectedPreparation(value.ActiveAttempt) {
		return "", "", ErrSnapshotCorrupt
	}
	var activeAttempt, barrierAttempt string
	if ref := value.ActiveAttempt.GetRetained(); ref != nil {
		retained, err := repository.preparation(ctx, org, identity.Family, ref)
		if err != nil {
			return "", "", err
		}
		if retained.BranchID == nil || *retained.BranchID != identity.BranchId {
			return "", "", ErrSnapshotCorrupt
		}
		activeAttempt = retained.AttemptID
	}
	if (value.BarrierPreparation == nil) != (value.FrozenBarrier == nil) {
		return "", "", ErrSnapshotCorrupt
	}
	if value.BarrierPreparation != nil {
		retained, err := repository.preparation(ctx, org, identity.Family, value.BarrierPreparation)
		if err != nil {
			return "", "", err
		}
		if retained.BranchID == nil || *retained.BranchID != identity.BranchId || retained.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF ||
			!sameBASSourceRef(retained.Preparation.FrozenBas, value.FrozenBarrier) {
			return "", "", ErrSnapshotCorrupt
		}
		if err := repository.verifyFrozenBarrier(ctx, org, identity.Family, retained.Preparation, value.FrozenBarrier); err != nil {
			return "", "", err
		}
		barrierAttempt = retained.AttemptID
	}
	if value.Resolution != nil {
		if err := repository.verifyCorrectiveResolution(ctx, org, identity.Family, value.Resolution); err != nil {
			return "", "", err
		}
	} else if value.WorkflowState == tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE || value.FrozenBarrier != nil {
		return "", "", ErrSnapshotCorrupt
	}
	return activeAttempt, barrierAttempt, nil
}
