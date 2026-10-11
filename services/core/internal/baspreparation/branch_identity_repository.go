package baspreparation

import (
	"bytes"
	"context"
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// Append retains immutable lineage after independently checking the current
// family body. Reads verify historical parents without consulting that body.
func (repository *BranchIdentityRepository) Append(ctx context.Context, value *tammyv1.BasBranchIdentity) (*tammyv1.SourceRef, error) {
	if repository == nil || !repository.ready(ctx) || value == nil {
		return nil, ErrBranchIdentityRepository
	}
	wire, hash, err := sealBranchIdentity(value)
	if err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	if err := repository.verifyIdentityParents(ctx, value); err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	family, err := (&FamilyStateRepository{repository.lifecycleRepository}).GetCurrent(ctx, value.OrganisationId, value.Family)
	if err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	if !sameBASSourceRef(value.FamilyRootPreparation, family.RootPreparation) || !sameOptionalRef(value.RootHead.GetRetained(), family.LatestHead.GetRetained()) ||
		!sameOptionalRef(value.EffectiveOriginal, family.EffectiveOriginal) {
		return nil, ErrBranchIdentityConflict
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT organisation_id,identity_hash FROM bas_branch_identities_v1 WHERE branch_id=?`, value.BranchId)
	if err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	if rows.Next() {
		var org string
		var storedHash []byte
		if err := rows.Scan(&org, &storedHash); err != nil {
			rows.Close()
			return nil, errors.Join(ErrBranchIdentityRepository, err)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, errors.Join(ErrBranchIdentityRepository, err)
		}
		if _, err := repository.GetExact(ctx, org, &tammyv1.SourceRef{Type: "bas_branch", Id: value.BranchId, Revision: 1, ContentHash: storedHash}); err != nil {
			return nil, err
		}
		return nil, ErrBranchIdentityConflict
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	createdAt, validTime := basTimestampSQLText(value.CreatedAt)
	if !validTime {
		return nil, ErrBranchIdentityRepository
	}
	rootID, rootVersion, rootHash := sqlSourceValues(value.FamilyRootPreparation)
	headID, headVersion, headHash := sqlSourceValues(value.RootHead.GetRetained())
	originalID, originalVersion, originalHash := sqlSourceValues(value.EffectiveOriginal)
	_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_branch_identities_v1(branch_id,family_id,organisation_id,original_purpose,identity_hash,branch_proto,
		family_root_preparation_id,family_root_version,family_root_hash,root_preparation_id,root_version,root_hash,
		effective_original_preparation_id,effective_original_version,effective_original_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.BranchId, value.Family.Id, value.OrganisationId, enumSQL(value.OriginalPurpose.String(), "BAS_BRANCH_PURPOSE_"), hash, wire,
		rootID, rootVersion, rootHash, headID, headVersion, headHash, originalID, originalVersion, originalHash, createdAt)
	if err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	return &tammyv1.SourceRef{Type: "bas_branch", Id: value.BranchId, Revision: 1, ContentHash: bytes.Clone(hash)}, nil
}
func (repository *BranchIdentityRepository) GetExact(ctx context.Context, org string, exact *tammyv1.SourceRef) (*tammyv1.BasBranchIdentity, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, "bas_branch", "") || exact.Revision != 1 {
		return nil, ErrBranchIdentityRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT family_id,original_purpose,identity_hash,
		CASE WHEN length(branch_proto)<=? THEN branch_proto ELSE NULL END,length(branch_proto),
		family_root_preparation_id,family_root_version,family_root_hash,root_preparation_id,root_version,root_hash,
		effective_original_preparation_id,effective_original_version,effective_original_hash,created_at
		FROM bas_branch_identities_v1 WHERE branch_id=? AND organisation_id=?`, scalarSnapshotLimit, exact.Id, org)
	if err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.Join(ErrBranchIdentityRepository, rows.Err(), rows.Close())
	}
	var familyID, purpose, createdAt string
	var hash, wire []byte
	var length int64
	var root, head, original preparationReadSource
	if err := rows.Scan(&familyID, &purpose, &hash, &wire, &length, &root.id, &root.version, &root.hash,
		&head.id, &head.version, &head.hash, &original.id, &original.version, &original.hash, &createdAt); err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	if rows.Next() {
		return nil, errors.Join(ErrBranchIdentityRepository, ErrSnapshotCorrupt)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	if err := repositorySnapshotBytes(wire, length, scalarSnapshotLimit); err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	if !bytes.Equal(hash, exact.ContentHash) {
		return nil, errors.Join(ErrBranchIdentityRepository, ErrSnapshotCorrupt)
	}
	value, err := openBranchIdentity(wire, org, exact)
	if err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	timeText, validTime := basTimestampSQLText(value.CreatedAt)
	if familyID != value.Family.Id || enumSQL(value.OriginalPurpose.String(), "BAS_BRANCH_PURPOSE_") != purpose || !validTime || timeText != createdAt ||
		!root.matches(value.FamilyRootPreparation, "bas_preparation") || !exactExpectedPreparation(value.RootHead) ||
		!head.matches(value.RootHead.GetRetained(), "bas_preparation") || !original.matches(value.EffectiveOriginal, "bas_preparation") {
		return nil, errors.Join(ErrBranchIdentityRepository, ErrSnapshotCorrupt)
	}
	if err := repository.verifyIdentityParents(ctx, value); err != nil {
		return nil, errors.Join(ErrBranchIdentityRepository, err)
	}
	return value, nil
}
func (repository *BranchIdentityRepository) verifyIdentityParents(ctx context.Context, value *tammyv1.BasBranchIdentity) error {
	if !exactExpectedPreparation(value.RootHead) {
		return ErrSnapshotCorrupt
	}
	if _, err := repository.family(ctx, value.OrganisationId, value.Family); err != nil {
		return err
	}
	return repository.chainParents(ctx, value.OrganisationId, value.Family, value.FamilyRootPreparation, value.EffectiveOriginal, value.RootHead.GetRetained())
}
