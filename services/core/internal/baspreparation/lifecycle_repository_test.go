//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLifecycleRepositoryHistoricalWorkflowDoesNotFollowCurrentAttempt(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	families, _ := NewFamilyStateRepository(tx, codec)
	identities, _ := NewBranchIdentityRepository(tx, codec)
	branches, _ := NewBranchStateRepository(tx, codec)
	rootPreparation, root := seedTypedCollectingPreparation(t, tx, org)
	if _, err := families.Append(ctx, org, initialTypedFamilyState(rootPreparation, root), nil); err != nil {
		t.Fatal(err)
	}
	identityRef, err := identities.Append(ctx, typedReplacementIdentity(rootPreparation, root, 9931))
	if err != nil {
		t.Fatal(err)
	}
	active, activeRef := seedTypedBranchPreparation(t, tx, rootPreparation, identityRef.Id, 9932, 9933)
	state := &tammyv1.BasBranchState{BranchIdentity: identityRef, Version: 1,
		WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
		ActiveAttempt: retainedExpectedIdentity(activeRef, 1), UpdatedAt: active.CreatedAt}
	stateRef, err := branches.Append(ctx, org, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	later := proto.Clone(active).(*tammyv1.BasPreparation)
	later.Version++
	later.ContentHash = nil
	later.UpdatedAt = proto.Clone(later.UpdatedAt).(*timestamppb.Timestamp)
	later.UpdatedAt.Seconds++
	laterRef := appendTypedCollectingPreparation(t, tx, later)
	updatedAt, _ := basTimestampSQLText(later.UpdatedAt)
	if _, err := tx.ExecContext(ctx, `UPDATE bas_preparation_heads_v1 SET current_version=2,current_hash=?,cas_version=2,updated_at=? WHERE preparation_id=?`, laterRef.ContentHash, updatedAt, active.PreparationId); err != nil {
		t.Fatal(err)
	}
	got, err := branches.GetExact(ctx, org, stateRef)
	if err != nil || !sameRef(got.ActiveAttempt.GetRetained(), activeRef) {
		t.Fatalf("historical attempt changed: %v %v", got, err)
	}
	next := proto.Clone(state).(*tammyv1.BasBranchState)
	next.Version++
	next.ContentHash = nil
	if _, err := branches.Append(ctx, org, next, stateRef); !errors.Is(err, ErrBranchStateConflict) {
		t.Fatalf("new workflow accepted stale active attempt: %v", err)
	}
	next.ActiveAttempt = retainedExpectedIdentity(laterRef, 2)
	nextRef, err := branches.Append(ctx, org, next, stateRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_branch_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_branch_versions_v1 SET branch_state_proto=X'01' WHERE version=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := branches.GetCurrent(ctx, org, identityRef); err == nil {
		t.Fatal("corrupt current workflow accepted")
	}
	if _, err := branches.GetExact(ctx, org, nextRef); err == nil {
		t.Fatal("corrupt exact workflow accepted")
	}
	if _, err := branches.GetExact(ctx, org, stateRef); err != nil {
		t.Fatalf("corrupt current workflow hid valid historical body: %v", err)
	}
}

func TestLifecycleRepositoryRejectsChangedCurrentHeadTimestampAndMissingHead(t *testing.T) {
	for _, kind := range []string{"timestamp", "missing"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			families, _ := NewFamilyStateRepository(tx, codec)
			identities, _ := NewBranchIdentityRepository(tx, codec)
			branches, _ := NewBranchStateRepository(tx, codec)
			rootPreparation, root := seedTypedCollectingPreparation(t, tx, org)
			if _, err := families.Append(ctx, org, initialTypedFamilyState(rootPreparation, root), nil); err != nil {
				t.Fatal(err)
			}
			identityRef, err := identities.Append(ctx, typedReplacementIdentity(rootPreparation, root, 9941))
			if err != nil {
				t.Fatal(err)
			}
			state := &tammyv1.BasBranchState{BranchIdentity: identityRef, Version: 1,
				WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
				ActiveAttempt: absentIdentity(), UpdatedAt: rootPreparation.CreatedAt}
			stateRef, err := branches.Append(ctx, org, state, nil)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "timestamp" {
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_branch_heads_v1_cas_guard`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_branch_heads_v1 SET updated_at='2026-10-09T00:00:00Z'`); err != nil {
					t.Fatal(err)
				}
				if _, err := branches.GetCurrent(ctx, org, identityRef); err == nil {
					t.Fatal("normalized head timestamp mismatch accepted")
				}
			} else {
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_branch_heads_v1_no_delete`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM bas_branch_heads_v1`); err != nil {
					t.Fatal(err)
				}
				if _, err := branches.Append(ctx, org, state, nil); !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("missing head treated as a new workflow: %v", err)
				}
			}
			if _, err := branches.GetExact(ctx, org, stateRef); err != nil {
				t.Fatalf("historical reads consulted mutable head: %v", err)
			}
		})
	}
}

func TestLifecycleRepositoryRetainsFamilyAndBranchThroughRollbackAndEncryptedRestart(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	families, _ := NewFamilyStateRepository(tx, codec)
	identities, _ := NewBranchIdentityRepository(tx, codec)
	branches, _ := NewBranchStateRepository(tx, codec)
	preparation, root := seedTypedCollectingPreparation(t, tx, org)
	family := initialTypedFamilyState(preparation, root)
	familyRef, err := families.Append(ctx, org, family, nil)
	if err != nil {
		t.Fatal(err)
	}
	identityRef, err := identities.Append(ctx, typedReplacementIdentity(preparation, root, 9951))
	if err != nil {
		t.Fatal(err)
	}
	state := &tammyv1.BasBranchState{BranchIdentity: identityRef, Version: 1,
		WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
		ActiveAttempt: absentIdentity(), UpdatedAt: preparation.CreatedAt}
	stateRef, err := branches.Append(ctx, org, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitBASRepositoryTx(t, tx)
	tx = beginBASRepositoryTx(t, workspace)
	families, _ = NewFamilyStateRepository(tx, codec)
	branches, _ = NewBranchStateRepository(tx, codec)
	family.Version++
	family.ActiveBranch = retainedExpectedIdentity(identityRef, 1)
	if _, err := families.Append(ctx, org, family, familyRef); err != nil {
		t.Fatal(err)
	}
	state.Version++
	if _, err := branches.Append(ctx, org, state, stateRef); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	families, _ = NewFamilyStateRepository(reopened, newBASRepositoryCursorCodec(t))
	branches, _ = NewBranchStateRepository(reopened, newBASRepositoryCursorCodec(t))
	gotFamily, err := families.GetCurrent(ctx, org, preparation.Family)
	if err != nil || gotFamily.Version != 1 || !bytes.Equal(gotFamily.ContentHash, familyRef.ContentHash) || gotFamily.ActiveBranch.GetRetained() != nil {
		t.Fatalf("family rollback/restart: %v %v", gotFamily, err)
	}
	gotBranch, err := branches.GetExact(ctx, org, stateRef)
	if err != nil || gotBranch.Version != 1 || !bytes.Equal(gotBranch.ContentHash, stateRef.ContentHash) {
		t.Fatalf("branch restart: %v %v", gotBranch, err)
	}
	assertBASRepositoryCount(t, reopened, "bas_branch_versions_v1", 1)
}

func seedTypedBranchPreparation(t *testing.T, executor app.CommandSQLExecutor, root *tammyv1.BasPreparation, branchID string, preparationID, attemptID uint64) (*tammyv1.BasPreparation, *tammyv1.SourceRef) {
	t.Helper()
	value := proto.Clone(root).(*tammyv1.BasPreparation)
	value.PreparationId, value.ContentHash = testUUID(preparationID), nil
	createdAt, _ := basTimestampSQLText(value.CreatedAt)
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_identities_v1(preparation_id,organisation_id,family_id,branch_id,attempt_id,created_at) VALUES(?,?,?,?,?,?)`,
		value.PreparationId, value.OrganisationId, value.Family.Id, branchID, testUUID(attemptID), createdAt); err != nil {
		t.Fatal(err)
	}
	ref := appendTypedCollectingPreparation(t, executor, value)
	value.ContentHash = ref.ContentHash
	updatedAt, _ := basTimestampSQLText(value.UpdatedAt)
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_heads_v1(preparation_id,organisation_id,current_version,current_hash,cas_version,updated_at) VALUES(?,?,1,?,1,?)`, value.PreparationId, value.OrganisationId, ref.ContentHash, updatedAt); err != nil {
		t.Fatal(err)
	}
	return value, ref
}

func TestLifecycleRepositoriesKeepIdentityWorkflowAndHistoricalAttemptSeparate(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	families, err := NewFamilyStateRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	identities, err := NewBranchIdentityRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	branches, err := NewBranchStateRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	preparation, root := seedTypedCollectingPreparation(t, tx, org)
	family := initialTypedFamilyState(preparation, root)
	familyRef, err := families.Append(ctx, org, family, nil)
	if err != nil {
		t.Fatal(err)
	}
	gotFamily, err := families.GetCurrent(ctx, org, preparation.Family)
	if err != nil || gotFamily.Version != 1 || !sameRef(familyRef, familyStateReference(gotFamily)) {
		t.Fatalf("family read: %v %v", gotFamily, err)
	}
	branch := typedReplacementIdentity(preparation, root, 9911)
	identityRef, err := identities.Append(ctx, branch)
	if err != nil {
		t.Fatal(err)
	}
	first := &tammyv1.BasBranchState{BranchIdentity: identityRef, Version: 1,
		WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
		ActiveAttempt: absentIdentity(), UpdatedAt: preparation.CreatedAt}
	firstRef, err := branches.Append(ctx, org, first, nil)
	if err != nil {
		t.Fatal(err)
	}
	family.Version++
	family.UpdatedAt = proto.Clone(family.UpdatedAt).(*timestamppb.Timestamp)
	family.UpdatedAt.Seconds++
	family.ActiveBranch = retainedExpectedIdentity(identityRef, 1)
	familyRef, err = families.Append(ctx, org, family, familyRef)
	if err != nil {
		t.Fatal(err)
	}
	// A current workflow change cannot redirect an exact historical empty branch.
	second := proto.Clone(first).(*tammyv1.BasBranchState)
	second.Version++
	second.UpdatedAt = proto.Clone(second.UpdatedAt).(*timestamppb.Timestamp)
	second.UpdatedAt.Seconds++
	secondRef, err := branches.Append(ctx, org, second, firstRef)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := branches.GetExact(ctx, org, firstRef)
	if err != nil || exact.Version != 1 || exact.ActiveAttempt.GetRetained() != nil {
		t.Fatalf("historical branch: %v %v", exact, err)
	}
	current, err := branches.GetCurrent(ctx, org, identityRef)
	if err != nil || !sameRef(secondRef, branchStateReference(current)) {
		t.Fatalf("current branch: %v %v", current, err)
	}
	// Family state pins a retained workflow version, independently of its live head.
	gotFamily, err = families.GetCurrent(ctx, org, preparation.Family)
	if err != nil || gotFamily.ActiveBranch.GetExpectedVersion() != 1 {
		t.Fatalf("family exact workflow pin: %v %v", gotFamily, err)
	}
	if _, err := families.GetCurrent(ctx, otherOrg, preparation.Family); err == nil {
		t.Fatal("cross-organisation family read accepted")
	}
	if _, err := identities.GetExact(ctx, otherOrg, identityRef); err == nil {
		t.Fatal("cross-organisation branch identity accepted")
	}
	if _, err := branches.GetExact(ctx, otherOrg, firstRef); err == nil {
		t.Fatal("cross-organisation workflow accepted")
	}
	third := proto.Clone(second).(*tammyv1.BasBranchState)
	third.Version++
	third.ContentHash = nil
	if _, err := branches.Append(ctx, org, third, firstRef); !errors.Is(err, ErrBranchStateConflict) {
		t.Fatalf("stale workflow write: %v", err)
	}
	staleFamily := proto.Clone(gotFamily).(*tammyv1.BasFamilyState)
	staleFamily.Version++
	staleFamily.ContentHash = nil
	wrongHash := proto.Clone(familyRef).(*tammyv1.SourceRef)
	wrongHash.ContentHash[0] ^= 0xff
	if _, err := families.Append(ctx, org, staleFamily, wrongHash); !errors.Is(err, ErrFamilyStateConflict) {
		t.Fatalf("stale family write: %v", err)
	}
	assertBASRepositoryCount(t, tx, "bas_branch_versions_v1", 2)
}

func TestLifecycleRepositoriesRejectCorruptCurrentBodiesBeforeAppend(t *testing.T) {
	for _, kind := range []string{"family_body", "family_scalar", "branch_identity", "branch_body", "branch_scalar", "root_body"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			codec := newBASRepositoryCursorCodec(t)
			families, _ := NewFamilyStateRepository(tx, codec)
			identities, _ := NewBranchIdentityRepository(tx, codec)
			branches, _ := NewBranchStateRepository(tx, codec)
			preparation, root := seedTypedCollectingPreparation(t, tx, org)
			family := initialTypedFamilyState(preparation, root)
			familyRef, err := families.Append(ctx, org, family, nil)
			if err != nil {
				t.Fatal(err)
			}
			identityRef, err := identities.Append(ctx, typedReplacementIdentity(preparation, root, 9921))
			if err != nil {
				t.Fatal(err)
			}
			state := &tammyv1.BasBranchState{BranchIdentity: identityRef, Version: 1,
				WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
				ActiveAttempt: absentIdentity(), UpdatedAt: preparation.CreatedAt}
			stateRef, err := branches.Append(ctx, org, state, nil)
			if err != nil {
				t.Fatal(err)
			}
			var statements []string
			switch kind {
			case "family_body":
				statements = []string{`DROP TRIGGER bas_family_state_v1_cas_guard`, `UPDATE bas_family_state_v1 SET family_state_proto=X'01'`}
			case "family_scalar":
				statements = []string{`DROP TRIGGER bas_family_state_v1_cas_guard`, `UPDATE bas_family_state_v1 SET overlay_state='DISPUTED'`}
			case "branch_identity":
				statements = []string{`DROP TRIGGER bas_branch_identities_v1_no_update`, `UPDATE bas_branch_identities_v1 SET branch_proto=X'01'`}
			case "branch_body":
				statements = []string{`DROP TRIGGER bas_branch_versions_v1_no_update`, `UPDATE bas_branch_versions_v1 SET branch_state_proto=X'01'`}
			case "branch_scalar":
				statements = []string{`DROP TRIGGER bas_branch_versions_v1_no_update`, `UPDATE bas_branch_versions_v1 SET workflow_state='CLOSED'`}
			case "root_body":
				statements = []string{`DROP TRIGGER bas_preparation_versions_v1_no_update`, `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01'`}
			}
			for _, statement := range statements {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "family_body" || kind == "family_scalar" || kind == "root_body" {
				if _, err := families.GetCurrent(ctx, org, preparation.Family); err == nil {
					t.Fatal("corrupt family dependency accepted")
				}
				family.Version++
				if _, err := families.Append(ctx, org, family, familyRef); err == nil {
					t.Fatal("family append healed corrupt current data")
				}
				if _, err := identities.Append(ctx, typedReplacementIdentity(preparation, root, 9922)); err == nil {
					t.Fatal("branch created under corrupt family")
				}
			} else {
				if _, err := branches.GetCurrent(ctx, org, identityRef); err == nil {
					t.Fatal("corrupt workflow dependency accepted")
				}
				state.Version++
				if _, err := branches.Append(ctx, org, state, stateRef); err == nil {
					t.Fatal("workflow append healed corrupt current data")
				}
			}
			assertBASRepositoryCount(t, tx, "bas_branch_versions_v1", 1)
		})
	}
}

func initialTypedFamilyState(preparation *tammyv1.BasPreparation, root *tammyv1.SourceRef) *tammyv1.BasFamilyState {
	return &tammyv1.BasFamilyState{Family: cloneRef(preparation.Family), Version: 1, RootPreparation: cloneRef(root),
		LatestHead: absentIdentity(), ActiveBranch: absentIdentity(),
		DisputeOverlay: tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE, UpdatedAt: preparation.CreatedAt}
}

func typedReplacementIdentity(preparation *tammyv1.BasPreparation, root *tammyv1.SourceRef, id uint64) *tammyv1.BasBranchIdentity {
	return &tammyv1.BasBranchIdentity{BranchId: testUUID(id), OrganisationId: preparation.OrganisationId, Family: cloneRef(preparation.Family),
		OriginalPurpose: tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REPLACEMENT, FamilyRootPreparation: cloneRef(root),
		RootHead: absentIdentity(), CreatedAt: preparation.CreatedAt}
}

func retainedExpectedIdentity(ref *tammyv1.SourceRef, version uint64) *tammyv1.BasExpectedIdentity {
	return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Retained{Retained: cloneRef(ref)}, ExpectedVersion: &version}
}

func assertBASRepositoryCount(t *testing.T, executor app.CommandSQLExecutor, table string, want int) {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), "SELECT count(*) FROM "+table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got int
	if !rows.Next() {
		t.Fatal("missing count")
	}
	if err := rows.Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}
