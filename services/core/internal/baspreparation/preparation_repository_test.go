//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPreparationRepositoryRetainsVersionsAndRejectsStaleOrChangedIdentity(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	repository, err := NewPreparationRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	initial := typedPreparationForAppend(t, tx, org)
	firstRef, err := repository.Append(ctx, initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.GetExact(ctx, org, firstRef)
	if err != nil || first.Preparation.Version != 1 || first.AttemptID != initial.AttemptID || first.SavedPaygInputs != nil {
		t.Fatalf("initial retained preparation: %v %v", first, err)
	}
	next := clonePreparationRetainedSnapshot(first)
	next.Preparation.Version++
	next.Preparation.ContentHash = nil
	next.Preparation.UpdatedAt.Seconds++
	secondRef, err := repository.Append(ctx, next, firstRef)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.GetCurrent(ctx, org, initial.Preparation.PreparationId)
	if err != nil || !sameRef(preparationReference(current.Preparation), secondRef) {
		t.Fatalf("current preparation: %v %v", current, err)
	}
	if _, err := repository.GetExact(ctx, otherOrg, firstRef); err == nil {
		t.Fatal("cross-organisation exact read accepted")
	}
	if _, err := repository.Append(ctx, next, firstRef); !errors.Is(err, ErrPreparationConflict) {
		t.Fatalf("stale preparation append: %v", err)
	}
	for _, kind := range []string{"attempt", "created_at", "branch", "predecessor"} {
		changed := clonePreparationRetainedSnapshot(current)
		changed.Preparation.Version++
		changed.Preparation.ContentHash = nil
		switch kind {
		case "attempt":
			changed.AttemptID = testUUID(9991)
		case "created_at":
			changed.Preparation.CreatedAt.Seconds++
		case "branch":
			id := testUUID(9992)
			changed.BranchID = &id
		case "predecessor":
			changed.Preparation.Predecessor = cloneRef(firstRef)
			changed.Preparation.Predecessor.Id = testUUID(9994)
		}
		if _, err := repository.Append(ctx, changed, secondRef); !errors.Is(err, ErrPreparationConflict) {
			t.Fatalf("changed immutable %s accepted: %v", kind, err)
		}
	}
	assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 2)
	if old, err := repository.GetExact(ctx, org, firstRef); err != nil || old.Preparation.Version != 1 {
		t.Fatalf("historical version redirected: %v %v", old, err)
	}
}

func TestPreparationRepositoryAppendsExactSavedInputOwnerAndSuccessor(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	repository, _ := NewPreparationRepository(tx, codec)
	initial := typedPreparationForAppend(t, tx, org)
	firstRef, err := repository.Append(ctx, initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := repository.GetExact(ctx, org, firstRef)
	if err != nil {
		t.Fatal(err)
	}
	inputs, _ := NewSavedPaygRepository(tx, codec)
	firstInput := savedPaygInputsForOwner(t, owner.Preparation, firstRef)
	firstInputRef, err := inputs.Append(ctx, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	second := clonePreparationRetainedSnapshot(owner)
	second.Preparation.Version++
	second.Preparation.ContentHash = nil
	second.SavedPaygInputs = firstInputRef
	secondRef, err := repository.Append(ctx, second, firstRef)
	if err != nil {
		t.Fatal(err)
	}
	// Changed pairs acquire a new collecting owner before retaining the pair.
	newInput := proto.Clone(firstInput).(*tammyv1.BasSavedPaygInputs)
	newInput.PaygwInputs.ReviewReason = "Reviewed additional retained PAYGW evidence"
	newHash, err := hashPaygInputs(newInput.PaygwInputs, newInput.PaygiInputs)
	if err != nil {
		t.Fatal(err)
	}
	third := clonePreparationRetainedSnapshot(second)
	third.Preparation.Version++
	third.Preparation.InputRevision++
	third.Preparation.InputHash = newHash
	third.Preparation.ContentHash = nil
	third.SavedPaygInputs = nil
	thirdRef, err := repository.Append(ctx, third, secondRef)
	if err != nil {
		t.Fatal(err)
	}
	newInput.InputSnapshotId, newInput.Preparation, newInput.InputRevision, newInput.InputHash, newInput.ContentHash = testUUID(9993), thirdRef, third.Preparation.InputRevision, newHash, nil
	secondInputRef, err := inputs.Append(ctx, newInput)
	if err != nil {
		t.Fatal(err)
	}
	fourth := clonePreparationRetainedSnapshot(third)
	fourth.Preparation.Version++
	fourth.Preparation.ContentHash = nil
	fourth.SavedPaygInputs = secondInputRef
	if _, err := repository.Append(ctx, fourth, thirdRef); err != nil {
		t.Fatal(err)
	}
	old, err := repository.GetExact(ctx, org, secondRef)
	if err != nil || !sameRef(old.SavedPaygInputs, firstInputRef) || old.Preparation.InputRevision != 1 {
		t.Fatalf("old saved input pointer changed: %v %v", old, err)
	}
	wrong := clonePreparationRetainedSnapshot(fourth)
	wrong.Preparation.Version++
	wrong.Preparation.ContentHash = nil
	wrong.SavedPaygInputs = firstInputRef
	current, err := repository.GetCurrent(ctx, org, wrong.Preparation.PreparationId)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Append(ctx, wrong, preparationReference(current.Preparation)); err == nil {
		t.Fatal("an earlier saved pair was attached to a new input revision")
	}
	assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 4)
}

func TestPreparationRepositoryRejectsCorruptCurrentOrSavedParentBeforeWrite(t *testing.T) {
	for _, kind := range []string{"current_body", "head_timestamp", "saved_body", "family_body"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			repository, _ := NewPreparationRepository(tx, codec)
			initial := typedPreparationForAppend(t, tx, org)
			firstRef, err := repository.Append(ctx, initial, nil)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := repository.GetExact(ctx, org, firstRef)
			if err != nil {
				t.Fatal(err)
			}
			next := clonePreparationRetainedSnapshot(owner)
			next.Preparation.Version++
			next.Preparation.ContentHash = nil
			var statements []string
			switch kind {
			case "current_body":
				statements = []string{`DROP TRIGGER bas_preparation_versions_v1_no_update`, `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01'`}
			case "head_timestamp":
				statements = []string{`DROP TRIGGER bas_preparation_heads_v1_cas_guard`, `UPDATE bas_preparation_heads_v1 SET updated_at='2026-10-10T00:00:00Z'`}
			case "family_body":
				statements = []string{`DROP TRIGGER bas_family_identities_v1_no_update`, `UPDATE bas_family_identities_v1 SET identity_proto=X'01'`}
			case "saved_body":
				inputs, _ := NewSavedPaygRepository(tx, codec)
				next.SavedPaygInputs, err = inputs.Append(ctx, savedPaygInputsForOwner(t, owner.Preparation, firstRef))
				if err != nil {
					t.Fatal(err)
				}
				statements = []string{`DROP TRIGGER bas_payg_input_versions_v1_no_update`, `UPDATE bas_payg_input_versions_v1 SET input_proto=X'01'`}
			}
			for _, statement := range statements {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if ref, err := repository.Append(ctx, next, firstRef); ref != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt %s healed: %v %v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 1)
		})
	}
}

func TestPreparationRepositoryRollbackAndEncryptedReopen(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	repository, _ := NewPreparationRepository(tx, codec)
	initial := typedPreparationForAppend(t, tx, org)
	ref, err := repository.Append(ctx, initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitBASRepositoryTx(t, tx)
	tx = beginBASRepositoryTx(t, workspace)
	repository, _ = NewPreparationRepository(tx, codec)
	current, err := repository.GetCurrent(ctx, org, initial.Preparation.PreparationId)
	if err != nil {
		t.Fatal(err)
	}
	next := clonePreparationRetainedSnapshot(current)
	next.Preparation.Version++
	next.Preparation.ContentHash = nil
	if _, err := repository.Append(ctx, next, ref); err != nil {
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
	repository, _ = NewPreparationRepository(reopened, codec)
	got, err := repository.GetCurrent(ctx, org, initial.Preparation.PreparationId)
	if err != nil || !sameRef(preparationReference(got.Preparation), ref) {
		t.Fatalf("committed version after rollback/reopen: %v %v", got, err)
	}
	assertBASRepositoryCount(t, reopened, "bas_preparation_versions_v1", 1)
}

func TestPreparationRepositoryRejectsMissingCurrentHeadWithoutHealing(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	repository, _ := NewPreparationRepository(tx, codec)
	initial := typedPreparationForAppend(t, tx, org)
	first, err := repository.Append(ctx, initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`DROP TRIGGER bas_preparation_heads_v1_no_delete`, `DELETE FROM bas_preparation_heads_v1`} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	next := clonePreparationRetainedSnapshot(initial)
	next.Preparation.Version++
	if ref, err := repository.Append(ctx, next, first); ref != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Fatalf("missing head healed: %v %v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_preparation_heads_v1", 0)
}

func TestPreparationRepositoryRequiresPinnedBranchPredecessor(t *testing.T) {
	for _, kind := range []string{"exact_root", "exact_root_empty_workflow", "missing", "unrelated"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			repository, _ := NewPreparationRepository(tx, codec)
			root := typedPreparationForAppend(t, tx, org)
			rootRef, err := repository.Append(ctx, root, nil)
			if err != nil {
				t.Fatal(err)
			}
			root, err = repository.GetExact(ctx, org, rootRef)
			if err != nil {
				t.Fatal(err)
			}
			families, _ := NewFamilyStateRepository(tx, codec)
			if _, err := families.Append(ctx, org, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
				t.Fatal(err)
			}
			identities, _ := NewBranchIdentityRepository(tx, codec)
			branch, err := identities.Append(ctx, typedReplacementIdentity(root.Preparation, rootRef, 9911))
			if err != nil {
				t.Fatal(err)
			}
			next := clonePreparationRetainedSnapshot(root)
			next.Preparation.PreparationId = testUUID(9912)
			next.Preparation.ContentHash = nil
			next.AttemptID = testUUID(9913)
			next.BranchID = &branch.Id
			next.Preparation.Predecessor = cloneRef(rootRef)
			expectedCount := 1
			switch kind {
			case "exact_root_empty_workflow":
				workflows, _ := NewBranchStateRepository(tx, codec)
				if _, err := workflows.Append(ctx, org, &tammyv1.BasBranchState{BranchIdentity: branch, Version: 1, WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE, ActiveAttempt: absentIdentity(), UpdatedAt: root.Preparation.UpdatedAt}, nil); err != nil {
					t.Fatal(err)
				}
			case "missing":
				next.Preparation.Predecessor = nil
			case "unrelated":
				_, unrelated := seedTypedBranchPreparation(t, tx, root.Preparation, branch.Id, 9914, 9915)
				next.Preparation.Predecessor = unrelated
				expectedCount++
			}
			ref, err := repository.Append(ctx, next, nil)
			if kind == "exact_root" || kind == "exact_root_empty_workflow" {
				if err != nil {
					t.Fatal(err)
				}
				got, err := repository.GetExact(ctx, org, ref)
				if err != nil || !sameRef(got.Preparation.Predecessor, rootRef) {
					t.Fatalf("exact branch predecessor: %v %v", got, err)
				}
				return
			}
			if ref != nil || !errors.Is(err, ErrPreparationConflict) {
				t.Fatalf("%s branch predecessor accepted: %v %v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", expectedCount)
		})
	}
}

func TestPreparationRepositoryRetryRetainsExactTerminalActiveAttempt(t *testing.T) {
	for _, kind := range []string{"exact_terminal", "stale_attempt", "root_instead", "missing_workflow_head"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			repository, _ := NewPreparationRepository(tx, codec)
			root := typedPreparationForAppend(t, tx, org)
			rootRef, err := repository.Append(ctx, root, nil)
			if err != nil {
				t.Fatal(err)
			}
			root, err = repository.GetExact(ctx, org, rootRef)
			if err != nil {
				t.Fatal(err)
			}
			families, _ := NewFamilyStateRepository(tx, codec)
			if _, err := families.Append(ctx, org, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
				t.Fatal(err)
			}
			identities, _ := NewBranchIdentityRepository(tx, codec)
			branch, err := identities.Append(ctx, typedReplacementIdentity(root.Preparation, rootRef, 9911))
			if err != nil {
				t.Fatal(err)
			}
			frozen, _ := seedTypedFrozenWorkflowAttempt(t, tx, root.Preparation, branch.Id)
			first, err := repository.GetCurrent(ctx, org, frozen.PreparationId)
			if err != nil {
				t.Fatal(err)
			}
			firstRef := preparationReference(first.Preparation)
			workflows, _ := NewBranchStateRepository(tx, codec)
			workflow := &tammyv1.BasBranchState{BranchIdentity: branch, Version: 1, WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE, ActiveAttempt: retainedExpectedIdentity(firstRef, first.Preparation.Version), UpdatedAt: first.Preparation.UpdatedAt}
			workflowRef, err := workflows.Append(ctx, org, workflow, nil)
			if err != nil {
				t.Fatal(err)
			}
			terminal := clonePreparationRetainedSnapshot(first)
			terminal.Preparation.Version++
			terminal.Preparation.ContentHash = nil
			terminal.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
			terminalRef, err := repository.Append(ctx, terminal, firstRef)
			if err != nil {
				t.Fatal(err)
			}
			retry := clonePreparationRetainedSnapshot(first)
			retry.Preparation.PreparationId, retry.AttemptID, retry.Preparation.ContentHash = testUUID(9914), testUUID(9915), nil
			retry.Preparation.Version = 1
			retry.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
			retry.Preparation.CurrentCalculation = nil
			retry.Preparation.CurrentValidation = nil
			retry.Preparation.FrozenBas = nil
			retry.Preparation.SupersededBy = nil
			retry.SavedPaygInputs = nil
			retry.Preparation.Predecessor = terminalRef
			switch kind {
			case "stale_attempt":
				retry.Preparation.Predecessor = firstRef
			case "root_instead":
				retry.Preparation.Predecessor = rootRef
			case "missing_workflow_head":
				for _, statement := range []string{`DROP TRIGGER bas_branch_heads_v1_no_delete`, `DELETE FROM bas_branch_heads_v1`} {
					if _, err := tx.ExecContext(ctx, statement); err != nil {
						t.Fatal(err)
					}
				}
			}
			ref, err := repository.Append(ctx, retry, nil)
			if kind == "exact_terminal" {
				if err != nil {
					t.Fatal(err)
				}
				workflow.Version++
				workflow.ContentHash = nil
				workflow.ActiveAttempt = retainedExpectedIdentity(ref, 1)
				if _, err := workflows.Append(ctx, org, workflow, workflowRef); err != nil {
					t.Fatal(err)
				}
				got, err := repository.GetExact(ctx, org, ref)
				if err != nil || !sameRef(got.Preparation.Predecessor, terminalRef) {
					t.Fatalf("exact terminal retry: %v %v", got, err)
				}
				return
			}
			want := ErrPreparationConflict
			if kind == "missing_workflow_head" {
				want = ErrSnapshotCorrupt
			}
			if ref != nil || !errors.Is(err, want) {
				t.Fatalf("%s retry lineage accepted: %v %v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 7)
		})
	}
}

func TestPreparationRepositoryPreservesFrozenInputsAndVerifiesAllApprovalParents(t *testing.T) {
	for _, kind := range []string{"valid", "changed_inputs", "calculation_body", "validation_body", "extra_finding", "approval_body"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			root, rootRef := seedTypedCollectingPreparation(t, tx, org)
			families, _ := NewFamilyStateRepository(tx, codec)
			if _, err := families.Append(ctx, org, initialTypedFamilyState(root, rootRef), nil); err != nil {
				t.Fatal(err)
			}
			branches, _ := NewBranchIdentityRepository(tx, codec)
			branch, err := branches.Append(ctx, typedReplacementIdentity(root, rootRef, 9961))
			if err != nil {
				t.Fatal(err)
			}
			frozen, _ := seedTypedFrozenWorkflowAttempt(t, tx, root, branch.Id)
			repository, _ := NewPreparationRepository(tx, codec)
			current, err := repository.GetCurrent(ctx, org, frozen.PreparationId)
			if err != nil {
				t.Fatal(err)
			}
			next := clonePreparationRetainedSnapshot(current)
			next.Preparation.Version++
			next.Preparation.ContentHash = nil
			next.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
			var statements []string
			switch kind {
			case "changed_inputs":
				next.Preparation.InputHash = testHash(201)
			case "calculation_body":
				statements = []string{`DROP TRIGGER bas_calculations_v1_no_update`, `UPDATE bas_calculations_v1 SET calculation_proto=X'01'`}
			case "validation_body":
				statements = []string{`DROP TRIGGER bas_validations_v1_no_update`, `UPDATE bas_validations_v1 SET validation_proto=X'01'`}
			case "approval_body":
				statements = []string{`DROP TRIGGER bas_frozen_bas_v1_no_update`, `UPDATE bas_frozen_bas_v1 SET frozen_proto=X'01'`}
			case "extra_finding":
				finding := &tammyv1.BasValidationFinding{FindingId: testUUID(9996), Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING, StableCode: "UNRETAINED_WARNING", SafeMessage: "Unretained warning must not be silently omitted"}
				validation := next.Preparation.CurrentValidation
				insertTypedWorkflowRow(t, tx, "bas_validation_findings_v1", []string{"validation_id", "organisation_id", "family_id", "revision", "validation_hash", "finding_id", "finding_hash", "severity", "stable_code", "safe_message", "finding_proto"},
					[]any{validation.Id, org, next.Preparation.Family.Id, validation.Revision, validation.ContentHash, finding.FindingId, canonicalFindingHash(finding), "WARNING", finding.StableCode, finding.SafeMessage, typedWorkflowWire(t, finding)})
			}
			for _, statement := range statements {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			ref, err := repository.Append(ctx, next, preparationReference(current.Preparation))
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				got, err := repository.GetExact(ctx, org, ref)
				if err != nil || !sameRef(got.Preparation.FrozenBas, current.Preparation.FrozenBas) || !sameRef(got.SavedPaygInputs, current.SavedPaygInputs) {
					t.Fatalf("frozen metadata successor: %v %v", got, err)
				}
				assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 7)
				return
			}
			want := ErrSnapshotCorrupt
			if kind == "changed_inputs" {
				want = ErrPreparationConflict
			}
			if ref != nil || !errors.Is(err, want) {
				t.Fatalf("invalid %s advanced frozen preparation: %v %v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 6)
		})
	}
}

func TestPreparationRepositoryReviewSuccessorRequiresEveryRetainedFinding(t *testing.T) {
	for _, kind := range []string{"valid", "missing_finding", "changed_message", "changed_wire"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			calculation, inputRef := seedCalculationRepositoryFixture(t, tx, org)
			calculations, _ := NewCalculationRepository(tx, codec)
			calculationRef, err := calculations.Append(ctx, calculation, inputRef)
			if err != nil {
				t.Fatal(err)
			}
			repository, _ := NewPreparationRepository(tx, codec)
			owner, err := repository.GetExact(ctx, org, calculation.Preparation)
			if err != nil {
				t.Fatal(err)
			}
			next := clonePreparationRetainedSnapshot(owner)
			next.Preparation.Version++
			next.Preparation.ContentHash = nil
			next.Preparation.CurrentCalculation = calculationRef
			ownerRef, err := repository.Append(ctx, next, calculation.Preparation)
			if err != nil {
				t.Fatal(err)
			}
			owner, err = repository.GetExact(ctx, org, ownerRef)
			if err != nil {
				t.Fatal(err)
			}
			validation := proto.Clone(readyReviewInput().Validation).(*tammyv1.BasValidationSnapshot)
			validation.PreparationId, validation.PreparationHash = owner.Preparation.PreparationId, ownerRef.ContentHash
			validation.CalculationId, validation.CalculationHash = calculationRef.Id, calculationRef.ContentHash
			validation.InputRevision, validation.InputHash, validation.SourceSnapshotHash = owner.Preparation.InputRevision, owner.Preparation.InputHash, owner.Preparation.SourceSnapshotHash
			validation.ContentHash = nil
			validation.Findings = []*tammyv1.BasValidationFinding{{FindingId: testUUID(9997), Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING, StableCode: "SOURCE_REVIEW_REQUIRED", SafeMessage: "Review the retained evidence before approval"}}
			wire, hash, err := sealValidation(validation, owner.Preparation)
			if err != nil {
				t.Fatal(err)
			}
			createdAt, _ := basTimestampSQLText(validation.CreatedAt)
			validationRef := &tammyv1.SourceRef{Type: "bas_validation", Id: validation.ValidationId, Revision: validation.Revision, ContentHash: hash}
			insertTypedWorkflowRow(t, tx, "bas_validations_v1", []string{"validation_id", "organisation_id", "family_id", "preparation_id", "preparation_version", "revision", "content_hash", "preparation_hash", "calculation_id", "calculation_version", "calculation_hash", "source_snapshot_hash", "input_revision", "input_hash", "input_snapshot_id", "input_content_hash", "validation_proto", "created_at"},
				[]any{validation.ValidationId, org, owner.Preparation.Family.Id, owner.Preparation.PreparationId, owner.Preparation.Version, validation.Revision, hash, ownerRef.ContentHash, calculationRef.Id, calculationRef.Revision, calculationRef.ContentHash, validation.SourceSnapshotHash, validation.InputRevision, validation.InputHash, inputRef.Id, inputRef.ContentHash, wire, createdAt})
			finding := validation.Findings[0]
			insertTypedWorkflowRow(t, tx, "bas_validation_findings_v1", []string{"validation_id", "organisation_id", "family_id", "revision", "validation_hash", "finding_id", "finding_hash", "severity", "stable_code", "safe_message", "finding_proto"},
				[]any{validation.ValidationId, org, owner.Preparation.Family.Id, validation.Revision, hash, finding.FindingId, canonicalFindingHash(finding), "WARNING", finding.StableCode, finding.SafeMessage, typedWorkflowWire(t, finding)})
			var statements []string
			switch kind {
			case "missing_finding":
				statements = []string{`DROP TRIGGER bas_validation_findings_v1_no_delete`, `DELETE FROM bas_validation_findings_v1`}
			case "changed_message":
				statements = []string{`DROP TRIGGER bas_validation_findings_v1_no_update`, `UPDATE bas_validation_findings_v1 SET safe_message='Different retained warning'`}
			case "changed_wire":
				statements = []string{`DROP TRIGGER bas_validation_findings_v1_no_update`, `UPDATE bas_validation_findings_v1 SET finding_proto=X'01'`}
			}
			for _, statement := range statements {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			next = clonePreparationRetainedSnapshot(owner)
			next.Preparation.Version++
			next.Preparation.ContentHash = nil
			next.Preparation.CurrentValidation = validationRef
			next.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY
			ref, err := repository.Append(ctx, next, ownerRef)
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				got, err := repository.GetCurrent(ctx, org, next.Preparation.PreparationId)
				if err != nil || !sameRef(preparationReference(got.Preparation), ref) || !sameRef(got.Preparation.CurrentValidation, validationRef) {
					t.Fatalf("retained review successor: %v %v", got, err)
				}
				return
			}
			if ref != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("invalid %s advanced review: %v %v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_preparation_versions_v1", 3)
		})
	}
}

func typedPreparationForAppend(t *testing.T, executor app.CommandSQLExecutor, org string) *PreparationRetainedSnapshot {
	t.Helper()
	codec := newBASRepositoryCursorCodec(t)
	profileRef := appendBASProfile(t, newProfileRepositoryForTest(t, executor, codec), validBASProfile(org, testUUID(9891), 1), nil)
	periodRef := appendBASPeriod(t, newPeriodRepositoryForTest(t, executor, codec), validBASPeriod(org, testUUID(9892), profileRef, 1, testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP), nil)
	familyRef, err := newFamilyIdentityRepositoryForTest(t, executor).Append(context.Background(), validBASFamilyIdentity(org, testUUID(9893)))
	if err != nil {
		t.Fatal(err)
	}
	value := proto.Clone(readyReviewInput().Preparation).(*tammyv1.BasPreparation)
	value.PreparationId, value.OrganisationId, value.Family, value.Profile, value.Period = testUUID(9894), org, familyRef, profileRef, periodRef
	value.Version, value.InputRevision = 1, 1
	value.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
	value.CurrentCalculation, value.CurrentValidation, value.FrozenBas, value.Predecessor, value.SupersededBy, value.ContentHash = nil, nil, nil, nil, nil, nil
	value.InputHash, err = hashPaygInputs(readyReviewInput().Calculation.PaygwInputs, readyReviewInput().Calculation.PaygiInputs)
	if err != nil {
		t.Fatal(err)
	}
	return &PreparationRetainedSnapshot{Preparation: value, AttemptID: testUUID(9895)}
}

func clonePreparationRetainedSnapshot(value *PreparationRetainedSnapshot) *PreparationRetainedSnapshot {
	cloned := &PreparationRetainedSnapshot{Preparation: proto.Clone(value.Preparation).(*tammyv1.BasPreparation), SavedPaygInputs: cloneRef(value.SavedPaygInputs), AttemptID: value.AttemptID}
	if value.BranchID != nil {
		id := *value.BranchID
		cloned.BranchID = &id
	}
	// Callers own their timestamps even when a fixture reused a pointer.
	cloned.Preparation.CreatedAt = proto.Clone(value.Preparation.CreatedAt).(*timestamppb.Timestamp)
	cloned.Preparation.UpdatedAt = proto.Clone(value.Preparation.UpdatedAt).(*timestamppb.Timestamp)
	return cloned
}
