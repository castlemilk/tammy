package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var (
	ErrFamilyStateRepository    = errors.New("baspreparation: family state repository operation failed")
	ErrFamilyStateConflict      = errors.New("baspreparation: family state conflicts with the retained current version")
	ErrBranchIdentityRepository = errors.New("baspreparation: branch identity repository operation failed")
	ErrBranchIdentityConflict   = errors.New("baspreparation: branch identity conflicts with the retained family")
	ErrBranchStateRepository    = errors.New("baspreparation: branch workflow repository operation failed")
	ErrBranchStateConflict      = errors.New("baspreparation: branch workflow conflicts with the retained current version")
)

// Lifecycle repositories use a caller-owned transaction. The command owns
// authorisation, policy, audit, replay, and rollback of any failed write.
type lifecycleRepository struct {
	executor app.CommandSQLExecutor
	cursors  *paging.Codec
}

type FamilyStateRepository struct{ lifecycleRepository }
type BranchIdentityRepository struct{ lifecycleRepository }
type BranchStateRepository struct{ lifecycleRepository }

func NewFamilyStateRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*FamilyStateRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrFamilyStateRepository
	}
	return &FamilyStateRepository{lifecycleRepository{executor, cursors}}, nil
}
func NewBranchIdentityRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*BranchIdentityRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrBranchIdentityRepository
	}
	return &BranchIdentityRepository{lifecycleRepository{executor, cursors}}, nil
}
func NewBranchStateRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*BranchStateRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrBranchStateRepository
	}
	return &BranchStateRepository{lifecycleRepository{executor, cursors}}, nil
}
func (repository *lifecycleRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && repository.cursors != nil && ctx != nil && ctx.Err() == nil
}
func familyStateReference(value *tammyv1.BasFamilyState) *tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_family_state", Id: value.Family.GetId(), Revision: value.Version, ContentHash: bytes.Clone(value.ContentHash)}
}
func branchStateReference(value *tammyv1.BasBranchState) *tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_branch_state", Id: value.BranchIdentity.GetId(), Revision: value.Version, ContentHash: bytes.Clone(value.ContentHash)}
}

func exactExpectedPreparation(value *tammyv1.BasExpectedIdentity) bool {
	if value == nil {
		return false
	}
	if ref := value.GetRetained(); ref != nil {
		return validBASSourceRef(ref, "bas_preparation", "") && value.ExpectedVersion != nil && value.GetExpectedVersion() == ref.Revision
	}
	_, absent := value.ExpectedState.(*tammyv1.BasExpectedIdentity_Absent)
	return absent && value.GetAbsent() && value.ExpectedVersion == nil
}
func exactExpectedBranch(value *tammyv1.BasExpectedIdentity) bool {
	if value == nil {
		return false
	}
	if ref := value.GetRetained(); ref != nil {
		_, validVersion := basSQLiteRevision(value.GetExpectedVersion())
		return validBASSourceRef(ref, "bas_branch", "") && ref.Revision == 1 && value.ExpectedVersion != nil && validVersion
	}
	_, absent := value.ExpectedState.(*tammyv1.BasExpectedIdentity_Absent)
	return absent && value.GetAbsent() && value.ExpectedVersion == nil
}
func sameOptionalRef(left, right *tammyv1.SourceRef) bool {
	return (left == nil && right == nil) || sameBASSourceRef(left, right)
}
func sqlSourceValues(value *tammyv1.SourceRef) (any, any, any) {
	if value == nil {
		return nil, nil, nil
	}
	return value.Id, int64(value.Revision), bytes.Clone(value.ContentHash)
}
func repositorySnapshotBytes(wire []byte, length, limit int64) error {
	if length > limit {
		return ErrSnapshotBound
	}
	if length <= 0 || int64(len(wire)) != length {
		return ErrSnapshotCorrupt
	}
	return nil
}

func (repository *lifecycleRepository) family(ctx context.Context, org string, ref *tammyv1.SourceRef) (*tammyv1.BasFamilyIdentity, error) {
	identities := &FamilyIdentityRepository{executor: repository.executor}
	return identities.GetExact(ctx, org, ref)
}
func (repository *lifecycleRepository) preparation(ctx context.Context, org string, family *tammyv1.SourceRef, ref *tammyv1.SourceRef) (*PreparationRetainedSnapshot, error) {
	reader := &PreparationReader{executor: repository.executor, cursors: repository.cursors}
	value, err := reader.GetExact(ctx, org, ref)
	if err != nil {
		return nil, err
	}
	if !sameBASSourceRef(value.Preparation.Family, family) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
func (repository *lifecycleRepository) chainParents(ctx context.Context, org string, family, root, original, head *tammyv1.SourceRef) error {
	if root == nil || root.Revision != 1 || (original == nil) != (head == nil) {
		return ErrSnapshotCorrupt
	}
	retainedRoot, err := repository.preparation(ctx, org, family, root)
	if err != nil {
		return err
	}
	if retainedRoot.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING || retainedRoot.Preparation.FrozenBas != nil || retainedRoot.BranchID != nil {
		return ErrSnapshotCorrupt
	}
	for _, ref := range []*tammyv1.SourceRef{original, head} {
		if ref == nil {
			continue
		}
		retained, err := repository.preparation(ctx, org, family, ref)
		if err != nil {
			return err
		}
		if retained.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED || retained.Preparation.FrozenBas == nil {
			return ErrSnapshotCorrupt
		}
		if err := repository.verifyFrozenBarrier(ctx, org, family, retained.Preparation, retained.Preparation.FrozenBas); err != nil {
			return err
		}
	}
	return nil
}

func enumSQL(value, prefix string) string { return strings.TrimPrefix(value, prefix) }
func liveBranchState(state tammyv1.BasBranchWorkflowState) bool {
	return state == tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE || state == tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE
}
