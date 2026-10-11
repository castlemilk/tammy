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
)

func TestValidationRepositoryRetainsCompleteSnapshotAndExactOwners(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	fixture := seedValidationRepositoryFixture(t, tx, org)
	repository := newValidationRepositoryForTest(t, tx)
	before := proto.Clone(fixture.validation).(*tammyv1.BasValidationSnapshot)

	validationRef, err := repository.Append(ctx, org, fixture.validation, fixture.ownerRef)
	if err != nil || validationRef == nil {
		t.Fatalf("append complete validation: ref=%v err=%v", validationRef, err)
	}
	if !proto.Equal(fixture.validation, before) {
		t.Fatal("append mutated the caller-owned validation")
	}
	expected := proto.Clone(before).(*tammyv1.BasValidationSnapshot)
	expected.ContentHash = validationRef.ContentHash
	got, err := repository.GetExact(ctx, org, validationRef)
	if err != nil || !proto.Equal(got, expected) {
		t.Fatalf("exact validation read: value=%v err=%v", got, err)
	}
	retained, err := repository.GetRetained(ctx, org, validationRef)
	if err != nil || retained == nil || !proto.Equal(retained.Validation, expected) ||
		!sameBASSourceRef(retained.Preparation, fixture.ownerRef) ||
		!sameBASSourceRef(retained.SavedPaygInputs, fixture.inputsRef) ||
		!sameBASSourceRef(retained.Family, fixture.owner.Preparation.Family) ||
		!sameBASSourceRef(retained.Calculation, fixture.calculationRef) {
		t.Fatalf("validation did not retain its exact owner, input, family and calculation references: value=%v err=%v", retained, err)
	}
	assertBASRepositoryCount(t, tx, "bas_validations_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_validation_findings_v1", len(expected.Findings))
	if value, err := repository.GetExact(ctx, otherOrg, validationRef); err == nil || value != nil {
		t.Fatal("validation exact reads must stay organisation scoped")
	}
	wrongHash := proto.Clone(validationRef).(*tammyv1.SourceRef)
	wrongHash.ContentHash[0] ^= 0xff
	if value, err := repository.GetExact(ctx, org, wrongHash); err == nil || value != nil {
		t.Fatal("validation exact reads must verify the caller's complete reference")
	}

	// A later workflow version must not redirect the immutable validation's
	// preparation link to the current preparation body.
	preparations, err := NewPreparationRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	next := clonePreparationRetainedSnapshot(fixture.owner)
	next.Preparation.Version++
	next.Preparation.UpdatedAt.Seconds++
	next.Preparation.ContentHash = nil
	next.Preparation.CurrentValidation = validationRef
	next.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY
	reviewReadyRef, err := preparations.Append(ctx, next, fixture.ownerRef)
	if err != nil {
		t.Fatalf("link retained validation to review-ready preparation: %v", err)
	}
	current, err := preparations.GetCurrent(ctx, org, fixture.owner.Preparation.PreparationId)
	if err != nil || !sameBASSourceRef(preparationReference(current.Preparation), reviewReadyRef) {
		t.Fatalf("review-ready successor: value=%v err=%v", current, err)
	}
	retained, err = repository.GetRetained(ctx, org, validationRef)
	if err != nil || retained == nil || !sameBASSourceRef(retained.Preparation, fixture.ownerRef) {
		t.Fatalf("historical validation owner changed with current preparation: value=%v err=%v", retained, err)
	}
	commitBASRepositoryTx(t, tx)
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(context.Background(), workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repository = newValidationRepositoryForTest(t, reopened)
	retained, err = repository.GetRetained(ctx, org, validationRef)
	if err != nil || retained == nil || !proto.Equal(retained.Validation, expected) ||
		!sameBASSourceRef(retained.Preparation, fixture.ownerRef) ||
		!sameBASSourceRef(retained.Calculation, fixture.calculationRef) {
		t.Fatalf("exact validation lineage survived encrypted reopen: value=%v err=%v", retained, err)
	}
}

func TestValidationRepositoryRejectsMismatchedStaleOrCorruptCapture(t *testing.T) {
	for _, kind := range []string{
		"preparation_hash", "calculation_hash", "source_hash", "input_revision", "input_hash", "claimed_hash",
		"wrong_scope", "stale_current_owner", "corrupt_owner", "corrupt_calculation", "corrupt_saved_inputs",
	} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, otherOrg := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedValidationRepositoryFixture(t, tx, org)
			candidate := proto.Clone(fixture.validation).(*tammyv1.BasValidationSnapshot)
			appendOrg := org
			switch kind {
			case "preparation_hash":
				candidate.PreparationHash[0] ^= 0xff
			case "calculation_hash":
				candidate.CalculationHash[0] ^= 0xff
			case "source_hash":
				candidate.SourceSnapshotHash[0] ^= 0xff
			case "input_revision":
				candidate.InputRevision++
			case "input_hash":
				candidate.InputHash[0] ^= 0xff
			case "claimed_hash":
				candidate.ContentHash = testHash(191)
			case "wrong_scope":
				appendOrg = otherOrg
			case "stale_current_owner":
				preparations, err := NewPreparationRepository(tx, newBASRepositoryCursorCodec(t))
				if err != nil {
					t.Fatal(err)
				}
				next := clonePreparationRetainedSnapshot(fixture.owner)
				next.Preparation.Version++
				next.Preparation.UpdatedAt.Seconds++
				next.Preparation.ContentHash = nil
				if _, err := preparations.Append(ctx, next, fixture.ownerRef); err != nil {
					t.Fatalf("advance current owner before stale append: %v", err)
				}
			case "corrupt_owner":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=?`,
					fixture.ownerRef.Id, int64(fixture.ownerRef.Revision)); err != nil {
					t.Fatal(err)
				}
			case "corrupt_calculation":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_calculations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_calculations_v1 SET calculation_proto=X'01' WHERE calculation_id=? AND version=?`,
					fixture.calculationRef.Id, int64(fixture.calculationRef.Revision)); err != nil {
					t.Fatal(err)
				}
			case "corrupt_saved_inputs":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_payg_input_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_payg_input_versions_v1 SET input_proto=X'01' WHERE input_snapshot_id=?`, fixture.inputsRef.Id); err != nil {
					t.Fatal(err)
				}
			}

			repository := newValidationRepositoryForTest(t, tx)
			ref, err := repository.Append(ctx, appendOrg, candidate, fixture.ownerRef)
			if ref != nil || err == nil {
				t.Fatalf("invalid %s validation capture was retained: ref=%v err=%v", kind, ref, err)
			}
			if kind == "stale_current_owner" && !errors.Is(err, ErrValidationConflict) {
				t.Fatalf("stale current owner should conflict, got %v", err)
			}
			if kind == "corrupt_owner" || kind == "corrupt_calculation" || kind == "corrupt_saved_inputs" {
				if !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("corrupt exact parent should fail closed, got %v", err)
				}
			}
			assertBASRepositoryCount(t, tx, "bas_validations_v1", 0)
			assertBASRepositoryCount(t, tx, "bas_validation_findings_v1", 0)
		})
	}
}

func TestValidationRepositoryVerifiesConsecutivePriorAndStableOwner(t *testing.T) {
	for _, kind := range []string{
		"valid_next_revision", "same_owner_after_metadata_successor", "different_preparation", "corrupt_prior_body",
		"corrupt_prior_finding", "missing_prior_revision", "skipped_revision",
	} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedValidationRepositoryFixture(t, tx, org)
			repository := newValidationRepositoryForTest(t, tx)
			firstRef, err := repository.Append(ctx, org, fixture.validation, fixture.ownerRef)
			if err != nil {
				t.Fatalf("append first validation revision: %v", err)
			}
			candidate := nextValidationRevision(fixture.validation)
			candidateOwner := fixture.ownerRef
			switch kind {
			case "same_owner_after_metadata_successor":
				preparations, err := NewPreparationRepository(tx, newBASRepositoryCursorCodec(t))
				if err != nil {
					t.Fatal(err)
				}
				next := clonePreparationRetainedSnapshot(fixture.owner)
				next.Preparation.Version++
				next.Preparation.UpdatedAt.Seconds++
				next.Preparation.ContentHash = nil
				candidateOwner, err = preparations.Append(ctx, next, fixture.ownerRef)
				if err != nil {
					t.Fatal(err)
				}
				candidate.PreparationHash = append([]byte(nil), candidateOwner.ContentHash...)
			case "different_preparation":
				siblingRef, siblingInputsRef, err := appendSiblingCalculationOwner(t, tx, fixture.owner.Preparation)
				if err != nil {
					t.Fatal(err)
				}
				siblingCalculation := proto.Clone(fixture.calculation).(*tammyv1.BasCalculation)
				siblingCalculation.CalculationId = testUUID(9922)
				siblingCalculation.ContentHash = nil
				siblingCalculation.Preparation = siblingRef
				siblingCalculations, err := NewCalculationRepository(tx, newBASRepositoryCursorCodec(t))
				if err != nil {
					t.Fatal(err)
				}
				siblingCalculationRef, err := siblingCalculations.Append(ctx, siblingCalculation, siblingInputsRef)
				if err != nil {
					t.Fatalf("retain sibling calculation: %v", err)
				}
				siblingOwner, err := newPreparationReaderForTest(t, tx).GetExact(ctx, org, siblingRef)
				if err != nil {
					t.Fatal(err)
				}
				next := clonePreparationRetainedSnapshot(siblingOwner)
				next.Preparation.Version++
				next.Preparation.UpdatedAt.Seconds++
				next.Preparation.ContentHash = nil
				next.Preparation.CurrentCalculation = siblingCalculationRef
				candidateOwner, err = newPreparationRepositoryForValidationTest(t, tx).Append(ctx, next, siblingRef)
				if err != nil {
					t.Fatalf("link sibling calculation to preparation: %v", err)
				}
				candidate.PreparationId = candidateOwner.Id
				candidate.PreparationHash = append([]byte(nil), candidateOwner.ContentHash...)
				candidate.CalculationId = siblingCalculationRef.Id
				candidate.CalculationHash = append([]byte(nil), siblingCalculationRef.ContentHash...)
			case "corrupt_prior_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_validations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_validations_v1 SET validation_proto=X'01' WHERE validation_id=? AND revision=1`, firstRef.Id); err != nil {
					t.Fatal(err)
				}
			case "corrupt_prior_finding":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_validation_findings_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_validation_findings_v1 SET finding_proto=X'01' WHERE validation_id=? AND revision=1`, firstRef.Id); err != nil {
					t.Fatal(err)
				}
			case "missing_prior_revision":
				second := nextValidationRevision(fixture.validation)
				if _, err := repository.Append(ctx, org, second, fixture.ownerRef); err != nil {
					t.Fatalf("append second validation revision before creating a retained-history gap: %v", err)
				}
				candidate = nextValidationRevision(second)
				for _, statement := range []string{
					`DROP TRIGGER bas_validation_findings_v1_no_delete`,
					`DROP TRIGGER bas_validations_v1_no_delete`,
					`DELETE FROM bas_validation_findings_v1 WHERE validation_id=? AND revision=1`,
					`DELETE FROM bas_validations_v1 WHERE validation_id=? AND revision=1`,
				} {
					if len(statement) > 6 && statement[:6] == "DELETE" {
						if _, err := tx.ExecContext(ctx, statement, firstRef.Id); err != nil {
							t.Fatal(err)
						}
					} else if _, err := tx.ExecContext(ctx, statement); err != nil {
						t.Fatal(err)
					}
				}
			case "skipped_revision":
				candidate.Revision = 3
			}

			ref, err := repository.Append(ctx, org, candidate, candidateOwner)
			if kind == "valid_next_revision" || kind == "same_owner_after_metadata_successor" {
				if err != nil || ref == nil || ref.Revision != 2 {
					t.Fatalf("valid same-owner next revision: ref=%v err=%v", ref, err)
				}
				if got, err := repository.GetExact(ctx, org, ref); err != nil || got.Revision != 2 {
					t.Fatalf("read next validation revision: value=%v err=%v", got, err)
				}
				return
			}
			if ref != nil || err == nil {
				t.Fatalf("invalid %s validation history was extended: ref=%v err=%v", kind, ref, err)
			}
			if (kind == "corrupt_prior_body" || kind == "corrupt_prior_finding") && !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt prior validation proof should fail closed, got %v", err)
			}
			if kind == "different_preparation" || kind == "missing_prior_revision" || kind == "skipped_revision" {
				if !errors.Is(err, ErrValidationConflict) {
					t.Fatalf("changed or incomplete validation identity should conflict, got %v", err)
				}
			}
			assertBASRepositoryCount(t, tx, "bas_validations_v1", 1)
		})
	}
}

func TestValidationRepositoryReadsEveryNormalizedFinding(t *testing.T) {
	for _, kind := range []string{"missing", "extra", "changed_scalar", "corrupt_body"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedValidationRepositoryFixture(t, tx, org)
			repository := newValidationRepositoryForTest(t, tx)
			ref, err := repository.Append(ctx, org, fixture.validation, fixture.ownerRef)
			if err != nil {
				t.Fatal(err)
			}
			findingID := fixture.validation.Findings[0].FindingId
			switch kind {
			case "missing":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_validation_findings_v1_no_delete`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM bas_validation_findings_v1 WHERE validation_id=? AND revision=? AND finding_id=?`, ref.Id, int64(ref.Revision), findingID); err != nil {
					t.Fatal(err)
				}
			case "extra":
				finding := &tammyv1.BasValidationFinding{FindingId: testUUID(9933), Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION,
					StableCode: "UNRETAINED_EXTRA", SafeMessage: "An extra normalized finding must not be hidden"}
				if _, err := tx.ExecContext(ctx, `INSERT INTO bas_validation_findings_v1(validation_id,organisation_id,family_id,revision,validation_hash,finding_id,finding_hash,severity,stable_code,safe_message,finding_proto)
					VALUES(?,?,?,?,?,?,?,?,?,?,?)`, ref.Id, org, fixture.owner.Preparation.Family.Id, int64(ref.Revision), ref.ContentHash,
					finding.FindingId, canonicalFindingHash(finding), enumSQL(finding.Severity.String(), "BAS_FINDING_SEVERITY_"), finding.StableCode,
					finding.SafeMessage, typedWorkflowWire(t, finding)); err != nil {
					t.Fatal(err)
				}
			case "changed_scalar", "corrupt_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_validation_findings_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				query, args := `UPDATE bas_validation_findings_v1 SET safe_message='Different retained text' WHERE validation_id=? AND revision=? AND finding_id=?`, []any{ref.Id, int64(ref.Revision), findingID}
				if kind == "corrupt_body" {
					query = `UPDATE bas_validation_findings_v1 SET finding_proto=X'01' WHERE validation_id=? AND revision=? AND finding_id=?`
				}
				if _, err := tx.ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			if value, err := repository.GetExact(ctx, org, ref); err == nil || value != nil {
				t.Fatalf("%s normalized finding corruption was silently accepted: value=%v", kind, value)
			}
		})
	}
}

func TestValidationRepositoryUsesSavepointAndCallerOwnedTransaction(t *testing.T) {
	t.Run("finding_insert_failure_rolls_back_partial_validation", func(t *testing.T) {
		workspace, org, _ := newBASRepositoryWorkspace(t)
		tx := beginBASRepositoryTx(t, workspace)
		defer tx.Rollback()
		fixture := seedValidationRepositoryFixture(t, tx, org)
		if _, err := tx.ExecContext(context.Background(), `CREATE TRIGGER test_reject_validation_finding BEFORE INSERT ON bas_validation_findings_v1 BEGIN SELECT RAISE(ABORT,'forced validation finding failure'); END`); err != nil {
			t.Fatal(err)
		}
		if ref, err := newValidationRepositoryForTest(t, tx).Append(context.Background(), org, fixture.validation, fixture.ownerRef); ref != nil || err == nil {
			t.Fatalf("forced finding insert failure: ref=%v err=%v", ref, err)
		}
		assertBASRepositoryCount(t, tx, "bas_validations_v1", 0)
		assertBASRepositoryCount(t, tx, "bas_validation_findings_v1", 0)
	})

	t.Run("caller_transaction_rollback", func(t *testing.T) {
		workspace, org, _ := newBASRepositoryWorkspace(t)
		tx := beginBASRepositoryTx(t, workspace)
		fixture := seedValidationRepositoryFixture(t, tx, org)
		if ref, err := newValidationRepositoryForTest(t, tx).Append(context.Background(), org, fixture.validation, fixture.ownerRef); err != nil || ref == nil {
			t.Fatalf("append before caller rollback: ref=%v err=%v", ref, err)
		}
		assertBASRepositoryCount(t, tx, "bas_validations_v1", 1)
		assertBASRepositoryCount(t, tx, "bas_validation_findings_v1", len(fixture.validation.Findings))
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		assertBASRepositoryCount(t, workspace.Database, "bas_validations_v1", 0)
		assertBASRepositoryCount(t, workspace.Database, "bas_validation_findings_v1", 0)
	})
}

type validationRepositoryFixture struct {
	owner          *PreparationRetainedSnapshot
	ownerRef       *tammyv1.SourceRef
	inputsRef      *tammyv1.SourceRef
	calculation    *tammyv1.BasCalculation
	calculationRef *tammyv1.SourceRef
	validation     *tammyv1.BasValidationSnapshot
}

func seedValidationRepositoryFixture(t *testing.T, executor app.CommandSQLExecutor, org string) validationRepositoryFixture {
	t.Helper()
	ctx := context.Background()
	calculation, inputsRef := seedCalculationRepositoryFixture(t, executor, org)
	calculations, err := NewCalculationRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	calculationRef, err := calculations.Append(ctx, calculation, inputsRef)
	if err != nil {
		t.Fatalf("retain validation calculation: %v", err)
	}
	preparations, err := NewPreparationRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := preparations.GetExact(ctx, org, calculation.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	next := clonePreparationRetainedSnapshot(owner)
	next.Preparation.Version++
	next.Preparation.UpdatedAt.Seconds++
	next.Preparation.ContentHash = nil
	next.Preparation.CurrentCalculation = calculationRef
	ownerRef, err := preparations.Append(ctx, next, calculation.Preparation)
	if err != nil {
		t.Fatalf("retain calc-bearing validation owner: %v", err)
	}
	owner, err = preparations.GetExact(ctx, org, ownerRef)
	if err != nil {
		t.Fatal(err)
	}
	validation := proto.Clone(readyReviewInput().Validation).(*tammyv1.BasValidationSnapshot)
	validation.ValidationId = testUUID(9931)
	validation.Revision = 1
	validation.ContentHash = nil
	validation.PreparationId = owner.Preparation.PreparationId
	validation.PreparationHash = append([]byte(nil), ownerRef.ContentHash...)
	validation.CalculationId = calculationRef.Id
	validation.CalculationHash = append([]byte(nil), calculationRef.ContentHash...)
	validation.SourceSnapshotHash = append([]byte(nil), owner.Preparation.SourceSnapshotHash...)
	validation.InputRevision = owner.Preparation.InputRevision
	validation.InputHash = append([]byte(nil), owner.Preparation.InputHash...)
	validation.Findings = []*tammyv1.BasValidationFinding{
		{FindingId: testUUID(9932), Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING,
			StableCode: "SOURCE_REVIEW_REQUIRED", SafeMessage: "Review this exact retained source before approval"},
		{FindingId: testUUID(9934), Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION,
			StableCode: "COVERAGE_COMPLETE", SafeMessage: "All supported source rows are represented"},
	}
	return validationRepositoryFixture{owner: owner, ownerRef: ownerRef, inputsRef: inputsRef,
		calculation: calculation, calculationRef: calculationRef, validation: validation}
}

func nextValidationRevision(value *tammyv1.BasValidationSnapshot) *tammyv1.BasValidationSnapshot {
	next := proto.Clone(value).(*tammyv1.BasValidationSnapshot)
	next.Revision++
	next.ContentHash = nil
	next.CreatedAt.Seconds++
	return next
}

func newValidationRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *ValidationRepository {
	t.Helper()
	repository, err := NewValidationRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct validation repository: %v", err)
	}
	return repository
}

func newPreparationRepositoryForValidationTest(t *testing.T, executor app.CommandSQLExecutor) *PreparationRepository {
	t.Helper()
	repository, err := NewPreparationRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct preparation repository: %v", err)
	}
	return repository
}
