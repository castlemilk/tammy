//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestExportEventRepositoryRetainsExactFrozenTargetAfterPreparationAdvances(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 42001, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil || frozenRef == nil {
		t.Fatalf("retain exact frozen export target: ref=%v err=%v", frozenRef, err)
	}
	target := frozenExportTargetForBoundaryTest(frozenRef)
	repository := newExportEventRepositoryForTest(t, tx)
	operationKey := testUUID(42002)
	started := exportEventForTest(organisationID, target, 42003, 700,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	startedRef, err := repository.Append(ctx, organisationID, operationKey, 0, started)
	if err != nil || startedRef == nil {
		t.Fatalf("append started event for frozen target: ref=%v err=%v", startedRef, err)
	}
	finished := exportEventForTest(organisationID, target, 42004, 701,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_SUCCEEDED,
		bytes.Repeat([]byte{0x5a}, sha256.Size))
	finishedRef, err := repository.Append(ctx, organisationID, operationKey, 1, finished)
	if err != nil || finishedRef == nil {
		t.Fatalf("append completed event for frozen target: ref=%v err=%v", finishedRef, err)
	}

	newValidationRef := appendWarningAcknowledgementValidationRevision(t, tx, organisationID, fixture.ackFixture)
	newPreparationRef := appendWarningAcknowledgementCurrentOwner(t, tx, organisationID, fixture.preparationRef, newValidationRef)
	if sameBASSourceRef(newPreparationRef, fixture.preparationRef) {
		t.Fatal("fixture did not advance the current preparation")
	}

	for _, retained := range []struct {
		ref   *tammyv1.SourceRef
		value *tammyv1.BasExportEvent
	}{{startedRef, started}, {finishedRef, finished}} {
		got, err := repository.GetExact(ctx, organisationID, retained.ref)
		want := proto.Clone(retained.value).(*tammyv1.BasExportEvent)
		want.ContentHash = bytes.Clone(retained.ref.ContentHash)
		if err != nil || !proto.Equal(got, want) {
			t.Fatalf("exact frozen-target event changed after preparation advance: got=%v want=%v err=%v", got, want, err)
		}
	}
	page, err := repository.List(ctx, ExportEventListRequest{OrganisationID: organisationID, Target: target, PageSize: 10})
	if err != nil || page == nil || len(page.Events) != 2 ||
		!sameBASSourceRef(page.Events[0].ExportEvent, startedRef) || !sameBASSourceRef(page.Events[1].ExportEvent, finishedRef) {
		t.Fatalf("frozen-target history changed after current preparation advanced: page=%#v err=%v", page, err)
	}
	commitBASRepositoryTx(t, tx)
}

func TestExportEventRepositoryCanAppendToOlderExactCalculationAfterNewerCapture(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	calculation, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	calculations := newCalculationRepositoryForTest(t, tx)
	olderCalculationRef, err := calculations.Append(ctx, calculation, inputsRef)
	if err != nil {
		t.Fatalf("retain original calculation target: %v", err)
	}
	newerCalculationRef := appendNewerCalculationForExportBoundaryTest(t, tx, organisationID, calculation, inputsRef)
	if newerCalculationRef.Revision <= olderCalculationRef.Revision {
		t.Fatalf("fixture did not advance calculation target: old=%v new=%v", olderCalculationRef, newerCalculationRef)
	}

	target := calculationExportTargetForTest(olderCalculationRef)
	event := exportEventForTest(organisationID, target, 42101, 710,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	eventRef, err := newExportEventRepositoryForTest(t, tx).Append(ctx, organisationID, testUUID(42102), 0, event)
	if err != nil || eventRef == nil {
		t.Fatalf("append using the older exact calculation after a newer capture: ref=%v err=%v", eventRef, err)
	}
	got, err := newExportEventRepositoryForTest(t, tx).GetExact(ctx, organisationID, eventRef)
	want := proto.Clone(event).(*tammyv1.BasExportEvent)
	want.ContentHash = bytes.Clone(eventRef.ContentHash)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("older exact calculation export did not remain readable: got=%v want=%v err=%v", got, want, err)
	}
	page, err := newExportEventRepositoryForTest(t, tx).List(ctx, ExportEventListRequest{OrganisationID: organisationID,
		Target: target, PageSize: 10})
	if err != nil || page == nil || len(page.Events) != 1 || !sameBASSourceRef(page.Events[0].ExportEvent, eventRef) {
		t.Fatalf("older exact calculation history was redirected by newer target: page=%#v err=%v", page, err)
	}
	commitBASRepositoryTx(t, tx)
}

func TestExportEventRepositoryRejectsInvalidOptionalPhaseOutcomeAndDigestCombinations(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	calculation, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	calculationRef, err := newCalculationRepositoryForTest(t, tx).Append(ctx, calculation, inputsRef)
	if err != nil {
		t.Fatal(err)
	}
	target := calculationExportTargetForTest(calculationRef)
	repository := newExportEventRepositoryForTest(t, tx)
	validDigest := bytes.Repeat([]byte{0x61}, sha256.Size)
	cases := []struct {
		name    string
		phase   tammyv1.BasExportPhase
		outcome tammyv1.BasExportOutcome
		digest  []byte
		mutate  func(*tammyv1.BasExportEvent)
	}{
		{name: "started_present_unspecified_outcome", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED,
			mutate: func(value *tammyv1.BasExportEvent) {
				value.Event.Outcome = tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED.Enum()
			}},
		{name: "started_present_digest", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED,
			mutate: func(value *tammyv1.BasExportEvent) { value.Event.ArtifactHash = bytes.Clone(validDigest) }},
		{name: "started_present_empty_digest", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED,
			mutate: func(value *tammyv1.BasExportEvent) { value.Event.ArtifactHash = []byte{} }},
		{name: "finished_missing_outcome", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED,
			outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_CANCELLED,
			mutate:  func(value *tammyv1.BasExportEvent) { value.Event.Outcome = nil }},
		{name: "finished_present_unspecified_outcome", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED,
			outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED},
		{name: "cancelled_with_digest", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED,
			outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_CANCELLED, digest: validDigest},
		{name: "failed_with_digest", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED,
			outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_FAILED, digest: validDigest},
		{name: "succeeded_missing_digest", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED,
			outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_SUCCEEDED},
		{name: "succeeded_short_digest", phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED,
			outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_SUCCEEDED, digest: bytes.Repeat([]byte{0x62}, sha256.Size-1)},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			event := exportEventForTest(organisationID, target, 42200+uint64(index), 720+int64(index),
				test.phase, test.outcome, test.digest)
			if test.mutate != nil {
				test.mutate(event)
			}
			if ref, err := repository.Append(ctx, organisationID, testUUID(42300+uint64(index)), 0, event); ref != nil || err == nil {
				t.Fatalf("invalid phase/outcome/digest combination was accepted: ref=%v err=%v", ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_export_events_v1", 0)
		})
	}
}

func TestExportEventRepositoryExactReadRejectsNormalizedTargetAndStatusTampering(t *testing.T) {
	for _, kind := range []string{"target", "status"} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			calculation, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
			calculations := newCalculationRepositoryForTest(t, tx)
			firstRef, err := calculations.Append(ctx, calculation, inputsRef)
			if err != nil {
				t.Fatal(err)
			}
			listTarget := calculationExportTargetForTest(firstRef)
			var secondRef *tammyv1.SourceRef
			if kind == "target" {
				secondRef = appendNewerCalculationForExportBoundaryTest(t, tx, organisationID, calculation, inputsRef)
			}
			event := exportEventForTest(organisationID, calculationExportTargetForTest(firstRef), 42401, 740,
				tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
			eventRef, err := newExportEventRepositoryForTest(t, tx).Append(ctx, organisationID, testUUID(42402), 0, event)
			if err != nil {
				t.Fatalf("append normalized corruption fixture: %v", err)
			}
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_export_events_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "target":
				if _, err := tx.ExecContext(ctx, `UPDATE bas_export_events_v1 SET calculation_version=?,calculation_hash=? WHERE export_event_id=?`,
					int64(secondRef.Revision), secondRef.ContentHash, eventRef.Id); err != nil {
					t.Fatalf("tamper normalized exact target: %v", err)
				}
				if got, err := newExportEventRepositoryForTest(t, tx).List(ctx, ExportEventListRequest{OrganisationID: organisationID, Target: listTarget, PageSize: 10}); got != nil || err == nil {
					t.Fatalf("silently omitted corrupt original export target: %v %v", got, err)
				}
				listTarget = calculationExportTargetForTest(secondRef)
			case "status":
				if _, err := tx.ExecContext(ctx, `UPDATE bas_export_events_v1 SET phase='FINISHED',outcome='CANCELLED' WHERE export_event_id=?`, eventRef.Id); err != nil {
					t.Fatalf("tamper normalized phase/outcome: %v", err)
				}
			}
			repository := newExportEventRepositoryForTest(t, tx)
			if got, err := repository.GetExact(ctx, organisationID, eventRef); got != nil || err == nil {
				t.Fatalf("exact read accepted normalized %s tampering: value=%v err=%v", kind, got, err)
			}
			if page, err := repository.List(ctx, ExportEventListRequest{OrganisationID: organisationID, Target: listTarget, PageSize: 10}); page != nil || err == nil {
				t.Fatalf("list accepted normalized %s tampering: page=%#v err=%v", kind, page, err)
			}
		})
	}
}

func frozenExportTargetForBoundaryTest(reference *tammyv1.SourceRef) *tammyv1.BasExportTarget {
	return &tammyv1.BasExportTarget{ExactTarget: &tammyv1.BasExportTarget_FrozenBas{FrozenBas: cloneRef(reference)}}
}

func appendNewerCalculationForExportBoundaryTest(t *testing.T, executor app.CommandSQLExecutor, organisationID string,
	calculation *tammyv1.BasCalculation, inputsRef *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	ctx := context.Background()
	owner, err := newPreparationReaderForTest(t, executor).GetExact(ctx, organisationID, calculation.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	nextPreparation := proto.Clone(owner.Preparation).(*tammyv1.BasPreparation)
	nextPreparation.Version++
	nextPreparation.UpdatedAt.Seconds++
	nextPreparation.ContentHash = nil
	nextPreparationRef := appendTypedInputBearingPreparation(t, executor, nextPreparation, calculation.Preparation, inputsRef)
	nextCalculation := proto.Clone(calculation).(*tammyv1.BasCalculation)
	nextCalculation.Version++
	nextCalculation.CreatedAt.Seconds++
	nextCalculation.ContentHash = nil
	nextCalculation.Preparation = nextPreparationRef
	ref, err := newCalculationRepositoryForTest(t, executor).Append(ctx, nextCalculation, inputsRef)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
