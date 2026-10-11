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

func TestCalculationRepositoryRetainsCompleteCalculationAndCoverage(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
	before := proto.Clone(value).(*tammyv1.BasCalculation)
	repository := newCalculationRepositoryForTest(t, tx)

	exact, err := repository.Append(context.Background(), value, inputsRef)
	if err != nil || exact == nil {
		t.Fatalf("append complete calculation: ref=%v err=%v", exact, err)
	}
	if !proto.Equal(value, before) {
		t.Fatal("append mutated the caller-owned calculation")
	}
	retained, err := repository.GetExact(context.Background(), org, exact)
	if err != nil || retained == nil {
		t.Fatalf("exact calculation read: value=%v err=%v", retained, err)
	}
	expected := proto.Clone(before).(*tammyv1.BasCalculation)
	expected.ContentHash = exact.ContentHash
	if !proto.Equal(retained, expected) {
		t.Fatal("exact calculation read did not preserve the complete typed body")
	}
	if len(retained.Coverage) != 3 || retained.Coverage[0].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE ||
		retained.Coverage[1].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON ||
		retained.Coverage[2].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED {
		t.Fatalf("complete ordered coverage was not retained: %#v", retained.Coverage)
	}
	var rows int
	if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_calculation_coverage_v1 WHERE calculation_id=? AND calculation_version=?`,
		value.CalculationId, value.Version).Scan(&rows); err != nil || rows != len(value.Coverage) {
		t.Fatalf("coverage rows=%d want=%d err=%v", rows, len(value.Coverage), err)
	}
	if body, err := repository.GetExact(context.Background(), otherOrg, exact); err == nil || body != nil {
		t.Fatal("calculation exact reads must stay organisation scoped")
	}
	wrongHash := proto.Clone(exact).(*tammyv1.SourceRef)
	wrongHash.ContentHash[0] ^= 0xff
	if body, err := repository.GetExact(context.Background(), org, wrongHash); err == nil || body != nil {
		t.Fatal("calculation exact reads must verify the caller's complete reference")
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
	retainedAfterReopen, err := newCalculationRepositoryForTest(t, reopened).GetExact(context.Background(), org, exact)
	if err != nil || retainedAfterReopen == nil || !proto.Equal(retainedAfterReopen, expected) {
		t.Fatalf("complete calculation survived encrypted workspace reopen: value=%v err=%v", retainedAfterReopen, err)
	}
}

func TestCalculationRepositoryRejectsStaleOrCorruptCaptureBeforeAnyWrite(t *testing.T) {
	for _, kind := range []string{"stale_current_preparation", "corrupt_exact_preparation"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
			if kind == "stale_current_preparation" {
				// The immutable calculation still names input-bearing version two after
				// the mutable preparation head advances to version three.
				currentOwner, err := newPreparationReaderForTest(t, tx).GetExact(context.Background(), org, value.Preparation)
				if err != nil {
					t.Fatal(err)
				}
				next := proto.Clone(currentOwner.Preparation).(*tammyv1.BasPreparation)
				next.Version++
				next.UpdatedAt.Seconds++
				next.ContentHash = nil
				appendTypedInputBearingPreparation(t, tx, next, value.Preparation, inputsRef)
			} else {
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=?`,
					value.Preparation.Id, value.Preparation.Revision); err != nil {
					t.Fatal(err)
				}
			}

			repository := newCalculationRepositoryForTest(t, tx)
			ref, err := repository.Append(context.Background(), value, inputsRef)
			if ref != nil || err == nil {
				t.Fatalf("invalid capture owner was retained: ref=%v err=%v", ref, err)
			}
			if kind == "stale_current_preparation" && !errors.Is(err, ErrCalculationConflict) {
				t.Fatalf("stale capture should be a calculation conflict, got %v", err)
			}
			if kind == "corrupt_exact_preparation" && !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt exact capture bytes should fail closed, got %v", err)
			}
			assertBASRepositoryCount(t, tx, "bas_calculations_v1", 0)
			assertBASRepositoryCount(t, tx, "bas_calculation_coverage_v1", 0)
		})
	}
}

func TestCalculationRepositoryRequiresMatchingCapturedSourceAndPaygInputs(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
	repository := newCalculationRepositoryForTest(t, tx)

	for _, kind := range []string{"family", "profile", "period", "source_snapshot", "input_revision", "input_hash", "paygw_pair", "paygi_pair", "saved_input_reference", "nil_coverage_source", "nil_lineage_reference"} {
		t.Run(kind, func(t *testing.T) {
			candidate := proto.Clone(value).(*tammyv1.BasCalculation)
			exactInputs := proto.Clone(inputsRef).(*tammyv1.SourceRef)
			switch kind {
			case "family":
				candidate.Family.ContentHash[0] ^= 0xff
			case "profile":
				candidate.Profile.ContentHash[0] ^= 0xff
			case "period":
				candidate.Period.Revision++
			case "source_snapshot":
				candidate.SourceSnapshotHash[0] ^= 0xff
			case "input_revision":
				candidate.InputRevision++
			case "input_hash":
				candidate.InputHash[0] ^= 0xff
			case "paygw_pair":
				candidate.PaygwInputs.ReviewReason = "A different reviewed withholding pair"
			case "paygi_pair":
				candidate.PaygiInputs.ReviewReason = "A different reviewed instalment pair"
			case "saved_input_reference":
				exactInputs.Id = testUUID(9979)
			case "nil_coverage_source":
				candidate.Coverage[0].Source = nil
			case "nil_lineage_reference":
				candidate.Coverage[0].Lineage = []*tammyv1.SourceRef{nil}
			}
			ref, err := repository.Append(context.Background(), candidate, exactInputs)
			if ref != nil || err == nil {
				t.Fatalf("mismatched %s capture was retained: ref=%v err=%v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_calculations_v1", 0)
			assertBASRepositoryCount(t, tx, "bas_calculation_coverage_v1", 0)
		})
	}
}

func TestCalculationRepositoryAppendVerifiesPriorVersionAndStableScope(t *testing.T) {
	for _, kind := range []string{"valid_next_version", "corrupt_prior_body", "different_preparation", "missing_prior_version"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
			repository := newCalculationRepositoryForTest(t, tx)
			firstRef, err := repository.Append(context.Background(), value, inputsRef)
			if err != nil {
				t.Fatalf("append first calculation version: %v", err)
			}

			candidate := proto.Clone(value).(*tammyv1.BasCalculation)
			candidate.Version = 2
			candidate.CreatedAt.Seconds++
			candidate.ContentHash = nil
			candidateInputs := inputsRef
			switch kind {
			case "corrupt_prior_body":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_calculations_v1 SET calculation_proto=X'01' WHERE calculation_id=? AND version=1`, firstRef.Id); err != nil {
					t.Fatal(err)
				}
			case "different_preparation":
				owner, err := newPreparationReaderForTest(t, tx).GetExact(context.Background(), org, value.Preparation)
				if err != nil {
					t.Fatal(err)
				}
				candidate.Preparation, candidateInputs, err = appendSiblingCalculationOwner(t, tx, owner.Preparation)
				if err != nil {
					t.Fatal(err)
				}
			case "missing_prior_version":
				if _, err := repository.Append(context.Background(), candidate, candidateInputs); err != nil {
					t.Fatalf("append second calculation version before creating a retained-history gap: %v", err)
				}
				candidate.Version = 3
				candidate.CreatedAt.Seconds++
				for _, statement := range []string{
					`DROP TRIGGER bas_calculation_coverage_v1_no_delete`,
					`DROP TRIGGER bas_calculations_v1_no_delete`,
					`DELETE FROM bas_calculation_coverage_v1 WHERE calculation_id=? AND calculation_version=1`,
					`DELETE FROM bas_calculations_v1 WHERE calculation_id=? AND version=1`,
				} {
					if statement == `DELETE FROM bas_calculation_coverage_v1 WHERE calculation_id=? AND calculation_version=1` ||
						statement == `DELETE FROM bas_calculations_v1 WHERE calculation_id=? AND version=1` {
						if _, err := tx.ExecContext(context.Background(), statement, firstRef.Id); err != nil {
							t.Fatal(err)
						}
						continue
					}
					if _, err := tx.ExecContext(context.Background(), statement); err != nil {
						t.Fatal(err)
					}
				}
			}

			ref, err := repository.Append(context.Background(), candidate, candidateInputs)
			if kind == "valid_next_version" {
				if err != nil || ref == nil || ref.Revision != 2 {
					t.Fatalf("valid second version for the same exact owner: ref=%v err=%v", ref, err)
				}
				if retained, err := repository.GetExact(context.Background(), org, ref); err != nil || retained.Version != 2 {
					t.Fatalf("exact second version read: value=%v err=%v", retained, err)
				}
				return
			}
			if ref != nil || err == nil {
				t.Fatalf("invalid %s history was extended: ref=%v err=%v", kind, ref, err)
			}
			if kind == "corrupt_prior_body" && !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt prior body should fail closed, got %v", err)
			}
			if (kind == "different_preparation" || kind == "missing_prior_version") && !errors.Is(err, ErrCalculationConflict) {
				t.Fatalf("changed or incomplete calculation identity should conflict, got %v", err)
			}
			want := 1
			assertBASRepositoryCount(t, tx, "bas_calculations_v1", want)
		})
	}
}

func TestCalculationRepositoryAllowsNextVersionFromNewPreparationRevision(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
	repository := newCalculationRepositoryForTest(t, tx)
	if _, err := repository.Append(context.Background(), value, inputsRef); err != nil {
		t.Fatalf("append first calculation version: %v", err)
	}
	owner, err := newPreparationReaderForTest(t, tx).GetExact(context.Background(), org, value.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	nextPreparation := proto.Clone(owner.Preparation).(*tammyv1.BasPreparation)
	nextPreparation.Version++
	nextPreparation.UpdatedAt.Seconds++
	nextPreparation.ContentHash = nil
	nextPreparationRef := appendTypedInputBearingPreparation(t, tx, nextPreparation, value.Preparation, inputsRef)
	nextPreparation.ContentHash = append([]byte(nil), nextPreparationRef.ContentHash...)

	nextCalculation := proto.Clone(value).(*tammyv1.BasCalculation)
	nextCalculation.Version++
	nextCalculation.CreatedAt.Seconds++
	nextCalculation.ContentHash = nil
	nextCalculation.Preparation = nextPreparationRef
	ref, err := repository.Append(context.Background(), nextCalculation, inputsRef)
	if err != nil || ref == nil || ref.Revision != 2 {
		t.Fatalf("append the next calculation against a newer exact version of the same preparation: ref=%v err=%v", ref, err)
	}
	if retained, err := repository.GetExact(context.Background(), org, ref); err != nil || retained == nil ||
		!sameBASSourceRef(retained.Preparation, nextPreparationRef) {
		t.Fatalf("read calculation with newer exact preparation owner: value=%v err=%v", retained, err)
	}
}

func TestCalculationRepositoryExactHistoricalReadIgnoresMutableCurrentPointers(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
	calculationRepository := newCalculationRepositoryForTest(t, tx)
	calculationRef, err := calculationRepository.Append(context.Background(), value, inputsRef)
	if err != nil {
		t.Fatal(err)
	}

	profiles, err := NewProfileRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	oldProfile, err := profiles.GetExact(context.Background(), org, value.Profile)
	if err != nil {
		t.Fatal(err)
	}
	newProfileRef := appendBASProfile(t, profiles, nextBASProfile(oldProfile, value.Profile, oldProfile.Revision+1), value.Profile)

	periods, err := NewPeriodRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	oldPeriod, err := periods.GetExact(context.Background(), org, value.Period)
	if err != nil {
		t.Fatal(err)
	}
	newPeriod := nextBASPeriod(oldPeriod, oldPeriod.Version+1)
	newPeriod.Confirmation.Profile = proto.Clone(newProfileRef).(*tammyv1.SourceRef)
	appendBASPeriod(t, periods, newPeriod, value.Period)

	owner, err := newPreparationReaderForTest(t, tx).GetExact(context.Background(), org, value.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	nextPreparation := proto.Clone(owner.Preparation).(*tammyv1.BasPreparation)
	nextPreparation.Version++
	nextPreparation.UpdatedAt.Seconds++
	nextPreparation.ContentHash = nil
	appendTypedInputBearingPreparation(t, tx, nextPreparation, value.Preparation, inputsRef)
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=?`,
		nextPreparation.PreparationId, nextPreparation.Version); err != nil {
		t.Fatal(err)
	}

	retained, err := calculationRepository.GetExact(context.Background(), org, calculationRef)
	if err != nil || retained == nil || retained.CalculationId != value.CalculationId {
		t.Fatalf("historical calculation should verify its exact older preparation/profile/period owners: value=%v err=%v", retained, err)
	}
}

func TestCalculationRepositoryRejectsNormalizedNestedAndCoverageCorruption(t *testing.T) {
	for _, kind := range []string{"normalized_scalar", "nested_result_frame", "nested_body_frame", "coverage_scalar", "coverage_body", "missing_coverage_row"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
			repository := newCalculationRepositoryForTest(t, tx)
			ref, err := repository.Append(context.Background(), value, inputsRef)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "normalized_scalar":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_calculations_v1 SET rule_version='rewritten-rules' WHERE calculation_id=?`, ref.Id); err != nil {
					t.Fatal(err)
				}
			case "nested_result_frame":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_calculations_v1 SET result_proto=X'01' WHERE calculation_id=?`, ref.Id); err != nil {
					t.Fatal(err)
				}
			case "nested_body_frame":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_calculations_v1 SET calculation_proto=X'01' WHERE calculation_id=?`, ref.Id); err != nil {
					t.Fatal(err)
				}
			case "coverage_scalar":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_coverage_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_calculation_coverage_v1 SET reason='Changed coverage reason' WHERE calculation_id=? AND sequence=2`, ref.Id); err != nil {
					t.Fatal(err)
				}
			case "coverage_body":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_coverage_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_calculation_coverage_v1 SET evidence_proto=X'01' WHERE calculation_id=? AND sequence=1`, ref.Id); err != nil {
					t.Fatal(err)
				}
			case "missing_coverage_row":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_coverage_v1_no_delete`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `DELETE FROM bas_calculation_coverage_v1 WHERE calculation_id=? AND sequence=2`, ref.Id); err != nil {
					t.Fatal(err)
				}
			}
			if retained, err := repository.GetExact(context.Background(), org, ref); err == nil || retained != nil {
				t.Fatalf("corrupt %s calculation storage became readable: value=%v", kind, retained)
			}
		})
	}
}

func seedCalculationRepositoryFixture(t *testing.T, executor app.CommandSQLExecutor, organisationID string) (*tammyv1.BasCalculation, *tammyv1.SourceRef) {
	t.Helper()
	owner, ownerRef := seedTypedCollectingPreparation(t, executor, organisationID)
	savedInputs := savedPaygInputsForOwner(t, owner, ownerRef)
	savedRepository, err := NewSavedPaygRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	inputsRef, err := savedRepository.Append(context.Background(), savedInputs)
	if err != nil {
		t.Fatalf("retain exact calculation PAYG inputs: %v", err)
	}
	inputOwner := proto.Clone(owner).(*tammyv1.BasPreparation)
	inputOwner.Version++
	inputOwner.UpdatedAt.Seconds++
	inputOwner.ContentHash = nil
	ownerRef = appendTypedInputBearingPreparation(t, executor, inputOwner, ownerRef, inputsRef)
	inputOwner.ContentHash = append([]byte(nil), ownerRef.ContentHash...)
	owner = inputOwner

	profiles, err := NewProfileRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	profileSnapshot, err := profiles.GetExact(context.Background(), organisationID, owner.Profile)
	if err != nil {
		t.Fatal(err)
	}
	periods, err := NewPeriodRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	periodSnapshot, err := periods.GetExact(context.Background(), organisationID, owner.Period)
	if err != nil {
		t.Fatal(err)
	}

	value := proto.Clone(readyReviewInput().Calculation).(*tammyv1.BasCalculation)
	value.CalculationId = testUUID(9951)
	value.OrganisationId = organisationID
	value.OrganisationSnapshot.Id = organisationID
	value.OrganisationIdentity.Id = organisationID
	value.WorkpaperSnapshot.OrganisationId = organisationID
	value.Version = 1
	value.ContentHash = nil
	value.Preparation = proto.Clone(ownerRef).(*tammyv1.SourceRef)
	value.Family = proto.Clone(owner.Family).(*tammyv1.SourceRef)
	value.Period = proto.Clone(owner.Period).(*tammyv1.SourceRef)
	value.Profile = proto.Clone(owner.Profile).(*tammyv1.SourceRef)
	value.SourceSnapshotHash = append([]byte(nil), owner.SourceSnapshotHash...)
	value.InputRevision = owner.InputRevision
	value.InputHash = append([]byte(nil), owner.InputHash...)
	value.PeriodSnapshot = periodSnapshot
	value.ProfileSnapshot = profileSnapshot
	value.PaygwInputs = proto.Clone(savedInputs.PaygwInputs).(*tammyv1.BasPaygwInputs)
	value.PaygiInputs = proto.Clone(savedInputs.PaygiInputs).(*tammyv1.BasPaygiInputs)
	value.Coverage = []*tammyv1.BasSourceCoverage{
		{
			Source: valueSourceRef("invoice", 9952), CandidateKind: tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_INVOICE_POSTING,
			Disposition:    tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE,
			EligibleAmount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 12500}, TaxDate: testCivilDate(2026, 8, 14),
			Lineage: []*tammyv1.SourceRef{valueSourceRef("invoice", 9953)}, Evidence: []*tammyv1.SourceRef{valueSourceRef("document", 9954)},
			ClassificationCode: "GST_SALES", Reason: "Eligible reviewed invoice posting",
		},
		{
			Source: valueSourceRef("purchase_settlement", 9955), CandidateKind: tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_PURCHASE_SETTLEMENT,
			Disposition: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
			Lineage:     []*tammyv1.SourceRef{valueSourceRef("purchase", 9956)}, ClassificationCode: "OUT_OF_PERIOD",
			Reason: "Settlement belongs to a different tax period",
		},
		{
			Source: valueSourceRef("manual_tax_fact", 9957), CandidateKind: tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
			Disposition: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED,
			Evidence:    []*tammyv1.SourceRef{valueSourceRef("document", 9958)}, ClassificationCode: "UNRESOLVED",
			Reason: "Source classification needs review",
		},
	}
	return value, inputsRef
}

func appendSiblingCalculationOwner(t *testing.T, executor app.CommandSQLExecutor,
	owner *tammyv1.BasPreparation) (*tammyv1.SourceRef, *tammyv1.SourceRef, error) {
	t.Helper()
	preparation := proto.Clone(owner).(*tammyv1.BasPreparation)
	preparation.PreparationId = testUUID(9981)
	preparation.Version = 1
	preparation.ContentHash = nil
	preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
	preparation.CurrentCalculation, preparation.CurrentValidation, preparation.FrozenBas = nil, nil, nil
	preparation.Predecessor, preparation.SupersededBy = nil, nil
	createdAt, ok := basTimestampSQLText(preparation.CreatedAt)
	if !ok {
		return nil, nil, ErrSnapshotCorrupt
	}
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_identities_v1(
		preparation_id,organisation_id,family_id,attempt_id,created_at) VALUES(?,?,?,?,?)`,
		preparation.PreparationId, preparation.OrganisationId, preparation.Family.Id, testUUID(9982), createdAt); err != nil {
		return nil, nil, err
	}
	preparationRef := appendTypedCollectingPreparation(t, executor, preparation)
	preparation.ContentHash = append([]byte(nil), preparationRef.ContentHash...)
	updatedAt, ok := basTimestampSQLText(preparation.UpdatedAt)
	if !ok {
		return nil, nil, ErrSnapshotCorrupt
	}
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_heads_v1(
		preparation_id,organisation_id,current_version,current_hash,cas_version,updated_at) VALUES(?,?,1,?,1,?)`,
		preparation.PreparationId, preparation.OrganisationId, preparationRef.ContentHash, updatedAt); err != nil {
		return nil, nil, err
	}
	savedInputs := savedPaygInputsForOwner(t, preparation, preparationRef)
	savedInputs.InputSnapshotId = testUUID(9983)
	savedRepository, err := NewSavedPaygRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		return nil, nil, err
	}
	inputsRef, err := savedRepository.Append(context.Background(), savedInputs)
	if err != nil {
		return nil, nil, err
	}
	inputOwner := proto.Clone(preparation).(*tammyv1.BasPreparation)
	inputOwner.Version++
	inputOwner.UpdatedAt.Seconds++
	inputOwner.ContentHash = nil
	inputOwnerRef := appendTypedInputBearingPreparation(t, executor, inputOwner, preparationRef, inputsRef)
	return inputOwnerRef, inputsRef, nil
}

func valueSourceRef(resource string, id uint64) *tammyv1.SourceRef {
	return sourceRef(resource, testUUID(id), 1, byte(id))
}

func newCalculationRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *CalculationRepository {
	t.Helper()
	repository, err := NewCalculationRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct calculation repository: %v", err)
	}
	return repository
}

func newPreparationReaderForTest(t *testing.T, executor app.CommandSQLExecutor) *PreparationReader {
	t.Helper()
	reader, err := NewPreparationReader(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct preparation reader: %v", err)
	}
	return reader
}

func appendTypedInputBearingPreparation(t *testing.T, executor app.CommandSQLExecutor, value *tammyv1.BasPreparation,
	expectedCurrent, savedInputs *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	wire, hash, err := sealPreparation(value)
	if err != nil {
		t.Fatal(err)
	}
	version, ok := basSQLiteRevision(value.Version)
	if !ok || !validBASSourceRef(expectedCurrent, preparationSnapshotSpec.resource, value.PreparationId) ||
		!validBASSourceRef(savedInputs, savedPaygSnapshotSpec.resource, "") {
		t.Fatal("invalid input-bearing preparation fixture refs")
	}
	createdAt, _ := basTimestampSQLText(value.CreatedAt)
	updatedAt, _ := basTimestampSQLText(value.UpdatedAt)
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_versions_v1(
		preparation_id,organisation_id,family_id,version,profile_id,profile_version,profile_hash,period_id,period_version,period_hash,
		input_snapshot_id,input_revision,input_hash,input_content_hash,dispute_overlay,source_snapshot_hash,state,content_hash,preparation_proto,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?)`,
		value.PreparationId, value.OrganisationId, value.Family.Id, version,
		value.Profile.Id, value.Profile.Revision, value.Profile.ContentHash, value.Period.Id, value.Period.Revision, value.Period.ContentHash,
		savedInputs.Id, value.InputRevision, value.InputHash, savedInputs.ContentHash,
		enumSQL(value.DisputeOverlay.String(), "BAS_DISPUTE_OVERLAY_STATE_"), value.SourceSnapshotHash,
		enumSQL(value.State.String(), "BAS_PREPARATION_STATE_"), hash, wire, createdAt, updatedAt); err != nil {
		t.Fatalf("insert input-bearing preparation: %v", err)
	}
	result, err := executor.ExecContext(context.Background(), `UPDATE bas_preparation_heads_v1
		SET current_version=?,current_hash=?,cas_version=cas_version+1,updated_at=?
		WHERE preparation_id=? AND organisation_id=? AND current_version=? AND current_hash=? AND cas_version=?`,
		version, hash, updatedAt, value.PreparationId, value.OrganisationId,
		int64(expectedCurrent.Revision), expectedCurrent.ContentHash, int64(expectedCurrent.Revision))
	if err != nil {
		t.Fatalf("advance input-bearing preparation pointer: %v", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		t.Fatalf("input-bearing preparation pointer changed=%d err=%v", changed, err)
	}
	return &tammyv1.SourceRef{Type: preparationSnapshotSpec.resource, Id: value.PreparationId,
		Revision: value.Version, ContentHash: append([]byte(nil), hash...)}
}
