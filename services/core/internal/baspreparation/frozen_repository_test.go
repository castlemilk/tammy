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

func TestFrozenRepositoryAppendAndExactRetainApprovalAfterCurrentOwnerAdvances(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 1, true, 0)
	repository := newFrozenRepositoryForTest(t, tx)
	before := proto.Clone(fixture.frozen).(*tammyv1.BasFrozenBas)
	frozenRef, err := repository.Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil || frozenRef == nil {
		t.Fatalf("append exact current review-ready approval: ref=%v err=%v", frozenRef, err)
	}
	if !proto.Equal(fixture.frozen, before) {
		t.Fatal("append mutated caller-owned frozen approval")
	}

	want := proto.Clone(before).(*tammyv1.BasFrozenBas)
	want.ContentHash = frozenRef.ContentHash
	got, err := repository.GetExact(ctx, organisationID, frozenRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("get exact retained approval: value=%v err=%v", got, err)
	}

	// Advancing the current validation and preparation pointer must not redirect
	// the exact frozen record to the newer review-ready owner.
	newValidationRef := appendWarningAcknowledgementValidationRevision(t, tx, organisationID, fixture.ackFixture)
	newPreparationRef := appendWarningAcknowledgementCurrentOwner(t, tx, organisationID,
		fixture.preparationRef, newValidationRef)
	if sameBASSourceRef(newPreparationRef, fixture.preparationRef) {
		t.Fatal("fixture did not advance the current preparation")
	}
	got, err = repository.GetExact(ctx, organisationID, frozenRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("historical approval changed after current owner advanced: value=%v err=%v", got, err)
	}
	commitBASRepositoryTx(t, tx)

	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = newFrozenRepositoryForTest(t, reopened).GetExact(ctx, organisationID, frozenRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("exact approval did not survive encrypted reopen: value=%v err=%v", got, err)
	}
}

func TestFrozenRepositoryAppendRespectsCallerRollback(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	setupTx := beginBASRepositoryTx(t, workspace)
	fixture := seedFrozenRepositoryFixture(t, setupTx, organisationID, 2, true, 0)
	commitBASRepositoryTx(t, setupTx)

	appendTx := beginBASRepositoryTx(t, workspace)
	frozenRef, err := newFrozenRepositoryForTest(t, appendTx).Append(ctx, organisationID,
		fixture.frozen, fixture.preparationRef)
	if err != nil || frozenRef == nil {
		_ = appendTx.Rollback()
		t.Fatalf("append inside caller transaction: ref=%v err=%v", frozenRef, err)
	}
	if err := appendTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertBASRepositoryCount(t, workspace.Database, "bas_frozen_bas_v1", 0)
	assertBASRepositoryCount(t, workspace.Database, "bas_frozen_acknowledgements_v1", 0)
}

func TestFrozenRepositoryAppendRejectsStaleOrIneligibleApproval(t *testing.T) {
	for _, kind := range []string{"stale_owner", "body_owner_mismatch", "unacknowledged_warning", "blocker", "unsupported"} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			var severity tammyv1.BasFindingSeverity
			acknowledge := true
			if kind == "unacknowledged_warning" {
				acknowledge = false
			}
			if kind == "blocker" {
				severity = tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER
				acknowledge = false
			}
			if kind == "unsupported" {
				severity = tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED
				acknowledge = false
			}
			fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 3, acknowledge, severity)
			repository := newFrozenRepositoryForTest(t, tx)

			currentRef := fixture.preparationRef
			if kind == "stale_owner" || kind == "body_owner_mismatch" {
				newValidationRef := appendWarningAcknowledgementValidationRevision(t, tx, organisationID, fixture.ackFixture)
				currentRef = appendWarningAcknowledgementCurrentOwner(t, tx, organisationID,
					fixture.preparationRef, newValidationRef)
			}
			if kind == "body_owner_mismatch" {
				current := newPreparationReaderForTest(t, tx)
				owner, err := current.GetExact(ctx, organisationID, currentRef)
				if err != nil {
					t.Fatal(err)
				}
				if ref, err := repository.Append(ctx, organisationID, fixture.frozen, preparationReference(owner.Preparation)); ref != nil || err == nil {
					t.Fatalf("frozen body naming an older owner was accepted: ref=%v err=%v", ref, err)
				}
			} else if ref, err := repository.Append(ctx, organisationID, fixture.frozen, currentRef); ref != nil || err == nil {
				t.Fatalf("ineligible or stale frozen approval %q was accepted: ref=%v err=%v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_frozen_bas_v1", 0)
			assertBASRepositoryCount(t, tx, "bas_frozen_acknowledgements_v1", 0)
		})
	}
}

func TestFrozenRepositoryExactReadRejectsCorruptFrozenOrRetainedParents(t *testing.T) {
	for _, kind := range []string{"wrong_organisation", "wrong_hash", "normalized_approval", "frozen_body", "calculation_parent", "validation_finding_scalar", "acknowledgement_body", "missing_acknowledgement_link"} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 4, true, 0)
			repository := newFrozenRepositoryForTest(t, tx)
			frozenRef, err := repository.Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
			if err != nil || frozenRef == nil {
				t.Fatalf("append corruption fixture: ref=%v err=%v", frozenRef, err)
			}

			switch kind {
			case "wrong_organisation", "wrong_hash":
				readOrg, exact := organisationID, cloneRef(frozenRef)
				if kind == "wrong_organisation" {
					readOrg = otherOrganisationID
				} else {
					exact.ContentHash[0] ^= 0xff
				}
				if got, err := repository.GetExact(ctx, readOrg, exact); got != nil || err == nil {
					t.Fatalf("invalid exact approval identity was accepted: value=%v err=%v", got, err)
				}
				return
			case "normalized_approval":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_frozen_bas_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_frozen_bas_v1 SET approval_reason='Changed normalized approval reason' WHERE frozen_id=?`, frozenRef.Id); err != nil {
					t.Fatal(err)
				}
			case "frozen_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_frozen_bas_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_frozen_bas_v1 SET frozen_proto=X'01' WHERE frozen_id=?`, frozenRef.Id); err != nil {
					t.Fatal(err)
				}
			case "calculation_parent":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_calculations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_calculations_v1 SET calculation_proto=X'01' WHERE calculation_id=? AND version=?`,
					fixture.frozen.Calculation.Id, fixture.frozen.Calculation.Revision); err != nil {
					t.Fatal(err)
				}
			case "validation_finding_scalar":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_validation_findings_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_validation_findings_v1 SET stable_code='TAMPERED_FROZEN_FINDING' WHERE validation_id=? AND revision=? AND finding_id=?`,
					fixture.frozen.Validation.Id, fixture.frozen.Validation.Revision, fixture.frozen.Acknowledgements[0].Finding.Id); err != nil {
					t.Fatal(err)
				}
			case "acknowledgement_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_warning_acknowledgements_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_warning_acknowledgements_v1 SET acknowledgement_proto=X'01' WHERE acknowledgement_id=?`,
					fixture.frozen.Acknowledgements[0].AcknowledgementId); err != nil {
					t.Fatal(err)
				}
			case "missing_acknowledgement_link":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_frozen_acknowledgements_v1_no_delete`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM bas_frozen_acknowledgements_v1 WHERE frozen_id=?`, frozenRef.Id); err != nil {
					t.Fatal(err)
				}
			}

			got, err := repository.GetExact(ctx, organisationID, frozenRef)
			if got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt %s approval or parent became readable: value=%v err=%v", kind, got, err)
			}
		})
	}
}

func TestFrozenRepositoryCancellationRollsBackOnlyItsSavepoint(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 8, true, 0)
	wantLegalName := writeCallerWorkBeforeRepositoryAppend(t, tx, organisationID, "frozen")

	appendCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	executor := &cancelAfterRepositoryParentInsertExecutor{
		CommandSQLExecutor: tx,
		parentTable:        "bas_frozen_bas_v1",
		cancel:             cancel,
	}
	frozenRef, err := newFrozenRepositoryForTest(t, executor).Append(appendCtx, organisationID,
		fixture.frozen, fixture.preparationRef)
	if frozenRef != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after frozen parent insert should abort the append: ref=%v err=%v", frozenRef, err)
	}
	if !executor.parentInserted {
		t.Fatal("test did not cancel after inserting the frozen parent")
	}
	assertBoundedUncancelledSavepointCleanup(t, executor)
	assertBASRepositoryCount(t, tx, "bas_frozen_bas_v1", 0)
	assertBASRepositoryCount(t, tx, "bas_frozen_acknowledgements_v1", 0)
	assertCallerWorkCanCommit(t, workspace.Database, tx, organisationID, wantLegalName, "frozen")
}

type frozenRepositoryTestFixture struct {
	ackFixture     warningAcknowledgementRepositoryFixture
	preparationRef *tammyv1.SourceRef
	frozen         *tammyv1.BasFrozenBas
}

func seedFrozenRepositoryFixture(t *testing.T, executor app.CommandSQLExecutor, organisationID string,
	fixtureNumber uint64, acknowledge bool, severity tammyv1.BasFindingSeverity) frozenRepositoryTestFixture {
	t.Helper()
	ctx := context.Background()
	base := seedValidationRepositoryFixture(t, executor, organisationID)
	validation := proto.Clone(base.validation).(*tammyv1.BasValidationSnapshot)
	if severity != 0 {
		validation.Findings[0].Severity = severity
		validation.Findings[0].StableCode = "FROZEN_REPOSITORY_BLOCKING_FINDING"
	}
	ackFixture := appendWarningAcknowledgementValidationOwner(t, executor, organisationID, base, validation)
	if acknowledge {
		acknowledgement := warningAcknowledgementForTest(ackFixture.validation, ackFixture.validationRef,
			ackFixture.validation.Findings[0], 32000+fixtureNumber)
		if _, err := newWarningAcknowledgementRepositoryForTest(t, executor).Append(ctx, organisationID,
			acknowledgement, ackFixture.currentPreparationRef); err != nil {
			t.Fatalf("retain frozen approval warning acknowledgement: %v", err)
		}
	}
	owner, err := newPreparationReaderForTest(t, executor).GetExact(ctx, organisationID, ackFixture.currentPreparationRef)
	if err != nil {
		t.Fatalf("read current review-ready frozen owner: %v", err)
	}
	calculation, err := newCalculationRepositoryForTest(t, executor).GetExact(ctx, organisationID,
		owner.Preparation.CurrentCalculation)
	if err != nil {
		t.Fatalf("read frozen approval calculation: %v", err)
	}
	validationSnapshot, err := newValidationRepositoryForTest(t, executor).GetRetained(ctx,
		organisationID, owner.Preparation.CurrentValidation)
	if err != nil {
		t.Fatalf("read frozen approval validation: %v", err)
	}
	acknowledgements, err := newWarningAcknowledgementRepositoryForTest(t, executor).ListForValidation(ctx,
		organisationID, owner.Preparation.CurrentValidation)
	if err != nil {
		t.Fatalf("read frozen approval acknowledgements: %v", err)
	}
	factorID, sessionID := testUUID(34000+fixtureNumber), testUUID(35000+fixtureNumber)
	if _, err := executor.ExecContext(ctx, `INSERT INTO application_sessions(id,user_id,state,created_at,last_active_at,expires_at)
		VALUES(?,?,1,'2026-10-11T00:00:00Z','2026-10-11T00:00:00Z','2026-10-12T00:00:00Z')`, sessionID, testUUID(72)); err != nil {
		t.Fatalf("insert structural application session: %v", err)
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO factor_assertions(id,user_id,session_id,purpose,asserted_at,consumed)
		VALUES(?,?,?,'bas_freeze_manual_handoff','2026-10-11T00:00:00Z',0)`, factorID, testUUID(72), sessionID); err != nil {
		t.Fatalf("insert structural fresh factor assertion: %v", err)
	}
	frozen := &tammyv1.BasFrozenBas{
		FrozenId: testUUID(33000 + fixtureNumber), OrganisationId: organisationID,
		Preparation: cloneRef(ackFixture.currentPreparationRef), Family: cloneRef(owner.Preparation.Family),
		Period: cloneRef(owner.Preparation.Period), Profile: cloneRef(owner.Preparation.Profile),
		Calculation: cloneRef(owner.Preparation.CurrentCalculation), Validation: cloneRef(owner.Preparation.CurrentValidation),
		CalculationSnapshot: calculation, Acknowledgements: acknowledgements,
		ApprovedSnapshotHash: testHash(byte(100 + fixtureNumber%100)), ApprovalActorUserId: testUUID(72),
		FreshFactorAssertionId: factorID, ApprovedAt: proto.Clone(owner.Preparation.UpdatedAt).(*timestamppb.Timestamp),
		ApprovalReason: "Retained exact approval for frozen repository tests",
	}
	if !sameBASSourceRef(frozen.Calculation, calculationReference(calculation)) ||
		!sameBASSourceRef(frozen.Validation, validationReference(validationSnapshot.Validation)) {
		t.Fatal("frozen repository fixture lost exact calculation or validation references")
	}
	return frozenRepositoryTestFixture{ackFixture: ackFixture,
		preparationRef: cloneRef(ackFixture.currentPreparationRef), frozen: frozen}
}

func newFrozenRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *FrozenRepository {
	t.Helper()
	repository, err := NewFrozenRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct frozen repository: %v", err)
	}
	return repository
}
