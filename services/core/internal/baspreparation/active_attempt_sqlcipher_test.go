//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestBASActiveAttemptAdvancePreservesFamilyRootAndUsesFreshHashes(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	root, rootRef := seedTypedCollectingPreparation(t, tx, org)
	families, _ := NewFamilyStateRepository(tx, codec)
	familyRef, err := families.Append(ctx, org, initialTypedFamilyState(root, rootRef), nil)
	if err != nil {
		t.Fatal(err)
	}
	identities, _ := NewBranchIdentityRepository(tx, codec)
	branchRef, err := identities.Append(ctx, typedReplacementIdentity(root, rootRef, 99801))
	if err != nil {
		t.Fatal(err)
	}
	prep, prepRef := seedTypedBranchPreparation(t, tx, root, branchRef.Id, 99802, 99803)
	branches, _ := NewBranchStateRepository(tx, codec)
	_, err = branches.Append(ctx, org, &tammyv1.BasBranchState{BranchIdentity: branchRef, Version: 1,
		WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
		ActiveAttempt: expectedPreparation(prep), UpdatedAt: prep.UpdatedAt}, nil)
	if err != nil {
		t.Fatal(err)
	}
	family, err := families.GetCurrent(ctx, org, root.Family)
	if err != nil {
		t.Fatal(err)
	}
	family.Version++
	family.ContentHash = nil
	family.ActiveBranch = basExpectedBranchIdentity(branchRef, 1)
	if _, err := families.Append(ctx, org, family, familyRef); err != nil {
		t.Fatal(err)
	}
	preparations, _ := NewPreparationRepository(tx, codec)
	current, err := preparations.GetCurrent(ctx, org, prep.PreparationId)
	if err != nil {
		t.Fatal(err)
	}
	next := clonePreparationRetainedSnapshot(current)
	next.Preparation.Version++
	next.Preparation.ContentHash = nil
	nextRef, err := preparations.Append(ctx, next, prepRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := advanceBASActiveAttempt(ctx, org, root.Family, prepRef, nextRef, prep.UpdatedAt, preparations, families, branches); err != nil {
		t.Fatalf("advance exact mutable attempt and workflow: %v", err)
	}
	got, err := families.GetCurrent(ctx, org, root.Family)
	if err != nil || !sameBASSourceRef(got.RootPreparation, rootRef) || got.LatestHead.GetRetained() != nil || got.ActiveBranch.GetExpectedVersion() != 2 {
		t.Fatalf("family root/head changed while advancing its attempt: %v, %v", got, err)
	}
	branch, err := branches.GetCurrent(ctx, org, branchRef)
	if err != nil || !proto.Equal(branch.ActiveAttempt.GetRetained(), nextRef) {
		t.Fatalf("active attempt=%v, %v", branch, err)
	}
}
