//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestExportEventRepositoryRetainsStartedAndTerminalOutcomes(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	calculation, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	calculationRef, err := newCalculationRepositoryForTest(t, tx).Append(ctx, calculation, inputsRef)
	if err != nil {
		t.Fatalf("retain export calculation target: %v", err)
	}
	target := calculationExportTargetForTest(calculationRef)
	repository := newExportEventRepositoryForTest(t, tx)

	type exportAttempt struct {
		operationKey string
		outcome      tammyv1.BasExportOutcome
		artifactHash []byte
	}
	attempts := []exportAttempt{
		{operationKey: testUUID(41001), outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_CANCELLED},
		{operationKey: testUUID(41002), outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_FAILED},
		{operationKey: testUUID(41003), outcome: tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_SUCCEEDED,
			artifactHash: bytes.Repeat([]byte{0x53}, sha256.Size)},
	}
	var retained []struct {
		ref   *tammyv1.SourceRef
		value *tammyv1.BasExportEvent
	}
	expectedVersion := uint64(0)
	for index, attempt := range attempts {
		started := exportEventForTest(organisationID, target, 41100+uint64(index*2), 100+int64(index*2),
			tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
		startedRef, err := repository.Append(ctx, organisationID, attempt.operationKey, expectedVersion, started)
		if err != nil || startedRef == nil {
			t.Fatalf("append started export attempt %d: ref=%v err=%v", index, startedRef, err)
		}
		retained = append(retained, struct {
			ref   *tammyv1.SourceRef
			value *tammyv1.BasExportEvent
		}{startedRef, started})
		expectedVersion++

		finished := exportEventForTest(organisationID, target, 41101+uint64(index*2), 101+int64(index*2),
			tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED, attempt.outcome, attempt.artifactHash)
		finishedRef, err := repository.Append(ctx, organisationID, attempt.operationKey, expectedVersion, finished)
		if err != nil || finishedRef == nil {
			t.Fatalf("append terminal export attempt %d: ref=%v err=%v", index, finishedRef, err)
		}
		retained = append(retained, struct {
			ref   *tammyv1.SourceRef
			value *tammyv1.BasExportEvent
		}{finishedRef, finished})
		expectedVersion++
	}
	if got := len(retained); got != 6 {
		t.Fatalf("retained event fixture has %d rows, want 6", got)
	}

	for _, item := range retained {
		got, err := repository.GetExact(ctx, organisationID, item.ref)
		want := proto.Clone(item.value).(*tammyv1.BasExportEvent)
		want.ContentHash = bytes.Clone(item.ref.ContentHash)
		if err != nil || !proto.Equal(got, want) {
			t.Fatalf("exact export event read %s: value=%v want=%v err=%v", item.ref.Id, got, want, err)
		}
	}

	page, err := repository.List(ctx, ExportEventListRequest{OrganisationID: organisationID, Target: target, PageSize: 10})
	if err != nil || page == nil || page.Page == nil || len(page.Events) != len(retained) {
		t.Fatalf("list complete export history: page=%#v err=%v", page, err)
	}
	for _, item := range retained {
		found := false
		for _, summary := range page.Events {
			if summary != nil && sameBASSourceRef(summary.ExportEvent, item.ref) {
				found = true
				if !proto.Equal(summary.Target, target) || summary.Format != item.value.Event.Format ||
					summary.Phase != item.value.Event.Phase || summary.GetOutcome() != item.value.Event.GetOutcome() {
					t.Fatalf("summary did not preserve bounded export facts for %s: %#v", item.ref.Id, summary)
				}
			}
		}
		if !found {
			t.Fatalf("export history omitted retained event %s", item.ref.Id)
		}
	}
	assertBASRepositoryCount(t, tx, "bas_export_events_v1", len(retained))
}

func TestExportEventRepositoryExpectedVersionIsTargetScopedAndAtomic(t *testing.T) {
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
	operationKey := testUUID(41201)

	wrongFirst := exportEventForTest(organisationID, target, 41202, 200,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	if ref, err := repository.Append(ctx, organisationID, operationKey, 1, wrongFirst); ref != nil || err == nil {
		t.Fatalf("first append with nonzero expected version was accepted: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_export_events_v1", 0)

	first := exportEventForTest(organisationID, target, 41203, 201,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	if ref, err := repository.Append(ctx, organisationID, operationKey, 0, first); ref == nil || err != nil {
		t.Fatalf("append at expected version zero: ref=%v err=%v", ref, err)
	}
	finishFirst := exportEventForTest(organisationID, target, 41208, 202,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_CANCELLED, nil)
	if ref, err := repository.Append(ctx, organisationID, operationKey, 1, finishFirst); ref == nil || err != nil {
		t.Fatalf("finish the first attempt at its next target version: ref=%v err=%v", ref, err)
	}
	wrongNext := exportEventForTest(organisationID, target, 41204, 202,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	if ref, err := repository.Append(ctx, organisationID, testUUID(41205), 3, wrongNext); ref != nil || err == nil {
		t.Fatalf("append across a target event-version gap was accepted: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_export_events_v1", 2)

	correctNext := exportEventForTest(organisationID, target, 41206, 203,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	if ref, err := repository.Append(ctx, organisationID, testUUID(41207), 2, correctNext); ref == nil || err != nil {
		t.Fatalf("append at next target event version: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_export_events_v1", 3)
}

func TestExportEventRepositoryExactHistorySurvivesLaterPreparationAndCalculation(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	calculation, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	calculations := newCalculationRepositoryForTest(t, tx)
	firstCalculationRef, err := calculations.Append(ctx, calculation, inputsRef)
	if err != nil {
		t.Fatal(err)
	}
	event := exportEventForTest(organisationID, calculationExportTargetForTest(firstCalculationRef), 41301, 300,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	eventRef, err := newExportEventRepositoryForTest(t, tx).Append(ctx, organisationID, testUUID(41302), 0, event)
	if err != nil {
		t.Fatalf("retain exact historical target event: %v", err)
	}

	owner, err := newPreparationReaderForTest(t, tx).GetExact(ctx, organisationID, calculation.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	nextPreparation := proto.Clone(owner.Preparation).(*tammyv1.BasPreparation)
	nextPreparation.Version++
	nextPreparation.UpdatedAt.Seconds++
	nextPreparation.ContentHash = nil
	nextPreparationRef := appendTypedInputBearingPreparation(t, tx, nextPreparation, calculation.Preparation, inputsRef)
	nextCalculation := proto.Clone(calculation).(*tammyv1.BasCalculation)
	nextCalculation.Version++
	nextCalculation.CreatedAt.Seconds++
	nextCalculation.ContentHash = nil
	nextCalculation.Preparation = nextPreparationRef
	secondCalculationRef, err := calculations.Append(ctx, nextCalculation, inputsRef)
	if err != nil || secondCalculationRef == nil || secondCalculationRef.Revision != firstCalculationRef.Revision+1 {
		t.Fatalf("advance preparation and calculation target: ref=%v err=%v", secondCalculationRef, err)
	}

	repository := newExportEventRepositoryForTest(t, tx)
	want := proto.Clone(event).(*tammyv1.BasExportEvent)
	want.ContentHash = bytes.Clone(eventRef.ContentHash)
	got, err := repository.GetExact(ctx, organisationID, eventRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("exact export history followed a later mutable preparation/calculation: got=%v want=%v err=%v", got, want, err)
	}
	page, err := repository.List(ctx, ExportEventListRequest{OrganisationID: organisationID,
		Target: calculationExportTargetForTest(firstCalculationRef), PageSize: 10})
	if err != nil || page == nil || len(page.Events) != 1 || !sameBASSourceRef(page.Events[0].ExportEvent, eventRef) {
		t.Fatalf("historical target-specific export listing changed after later target append: page=%#v err=%v", page, err)
	}
	commitBASRepositoryTx(t, tx)
}

func TestExportEventRepositoryScopesReferencesToOrganisationAndExactTarget(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
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
	event := exportEventForTest(organisationID, target, 41401, 400,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	eventRef, err := repository.Append(ctx, organisationID, testUUID(41402), 0, event)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := repository.GetExact(ctx, otherOrganisationID, eventRef); got != nil || err == nil {
		t.Fatalf("exact read under another organisation returned event: got=%v err=%v", got, err)
	}
	wrongHash := cloneRef(eventRef)
	wrongHash.ContentHash[0] ^= 0xff
	if got, err := repository.GetExact(ctx, organisationID, wrongHash); got != nil || err == nil {
		t.Fatalf("exact read accepted altered event hash: got=%v err=%v", got, err)
	}
	wrongID := cloneRef(eventRef)
	wrongID.Id = testUUID(41403)
	if got, err := repository.GetExact(ctx, organisationID, wrongID); got != nil || err == nil {
		t.Fatalf("exact read accepted another event ID: got=%v err=%v", got, err)
	}
	if page, err := repository.List(ctx, ExportEventListRequest{OrganisationID: otherOrganisationID, Target: target, PageSize: 10}); page != nil || err == nil {
		t.Fatalf("list under another organisation returned event history: page=%#v err=%v", page, err)
	}

	foreignEvent := exportEventForTest(otherOrganisationID, target, 41404, 401,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	if ref, err := repository.Append(ctx, otherOrganisationID, testUUID(41405), 0, foreignEvent); ref != nil || err == nil {
		t.Fatalf("append of foreign-organisation event against exact target was accepted: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_export_events_v1", 1)
}

func TestExportEventRepositoryAppendRollsBackAndSurvivesEncryptedReopen(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	setupTx := beginBASRepositoryTx(t, workspace)
	calculation, inputsRef := seedCalculationRepositoryFixture(t, setupTx, organisationID)
	calculationRef, err := newCalculationRepositoryForTest(t, setupTx).Append(ctx, calculation, inputsRef)
	if err != nil {
		t.Fatal(err)
	}
	commitBASRepositoryTx(t, setupTx)
	target := calculationExportTargetForTest(calculationRef)
	operationKey := testUUID(41501)

	rolledBack := beginBASRepositoryTx(t, workspace)
	rollbackEvent := exportEventForTest(organisationID, target, 41502, 500,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	if ref, err := newExportEventRepositoryForTest(t, rolledBack).Append(ctx, organisationID, operationKey, 0, rollbackEvent); ref == nil || err != nil {
		t.Fatalf("append inside caller-owned transaction: ref=%v err=%v", ref, err)
	}
	if err := rolledBack.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertBASRepositoryCount(t, workspace.Database, "bas_export_events_v1", 0)

	appendTx := beginBASRepositoryTx(t, workspace)
	retainedEvent := exportEventForTest(organisationID, target, 41503, 501,
		tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	eventRef, err := newExportEventRepositoryForTest(t, appendTx).Append(ctx, organisationID, operationKey, 0, retainedEvent)
	if err != nil || eventRef == nil {
		t.Fatalf("append event after rolled back transaction: ref=%v err=%v", eventRef, err)
	}
	commitBASRepositoryTx(t, appendTx)
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	want := proto.Clone(retainedEvent).(*tammyv1.BasExportEvent)
	want.ContentHash = bytes.Clone(eventRef.ContentHash)
	got, err := newExportEventRepositoryForTest(t, reopened).GetExact(ctx, organisationID, eventRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("committed export event did not survive encrypted reopen: got=%v want=%v err=%v", got, want, err)
	}
}

func TestExportEventRepositoryExactReadsFailClosedOnBodyScalarOrParentCorruption(t *testing.T) {
	for _, corruption := range []string{"event_body", "event_scalar", "calculation_parent_body"} {
		t.Run(corruption, func(t *testing.T) {
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
			event := exportEventForTest(organisationID, target, 41601, 600,
				tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
			eventRef, err := newExportEventRepositoryForTest(t, tx).Append(ctx, organisationID, testUUID(41602), 0, event)
			if err != nil {
				t.Fatal(err)
			}

			switch corruption {
			case "event_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_export_events_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_export_events_v1 SET export_proto=X'01' WHERE export_event_id=?`, eventRef.Id); err != nil {
					t.Fatal(err)
				}
			case "event_scalar":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_export_events_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_export_events_v1 SET reason='scalar tamper' WHERE export_event_id=?`, eventRef.Id); err != nil {
					t.Fatal(err)
				}
			case "calculation_parent_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_calculations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_calculations_v1 SET calculation_proto=X'01' WHERE calculation_id=? AND version=?`,
					calculationRef.Id, calculationRef.Revision); err != nil {
					t.Fatal(err)
				}
			}

			repository := newExportEventRepositoryForTest(t, tx)
			if got, err := repository.GetExact(ctx, organisationID, eventRef); got != nil || err == nil {
				t.Fatalf("exact read returned corrupted %s export or parent: value=%v err=%v", corruption, got, err)
			}
			if page, err := repository.List(ctx, ExportEventListRequest{OrganisationID: organisationID, Target: target, PageSize: 10}); page != nil || err == nil {
				t.Fatalf("list returned corrupted %s export or parent: page=%#v err=%v", corruption, page, err)
			}
		})
	}
}

func newExportEventRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *ExportEventRepository {
	t.Helper()
	repository, err := NewExportEventRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct export event repository: %v", err)
	}
	return repository
}

func calculationExportTargetForTest(calculationRef *tammyv1.SourceRef) *tammyv1.BasExportTarget {
	return &tammyv1.BasExportTarget{ExactTarget: &tammyv1.BasExportTarget_Calculation{Calculation: cloneRef(calculationRef)}}
}

func exportEventForTest(organisationID string, target *tammyv1.BasExportTarget, eventID uint64, timestampSeconds int64,
	phase tammyv1.BasExportPhase, outcome tammyv1.BasExportOutcome, artifactHash []byte) *tammyv1.BasExportEvent {
	occurredAt := timestamppb.New(time.Unix(timestampSeconds, 0).UTC())
	input := &tammyv1.BasExportEventInput{
		Target: proto.Clone(target).(*tammyv1.BasExportTarget), Format: tammyv1.BasExportFormat_BAS_EXPORT_FORMAT_PDF,
		Phase: phase, OccurredAt: occurredAt, Reason: "BAS PDF export test event",
	}
	if phase == tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED {
		input.Outcome = outcome.Enum()
		input.Reason = "BAS PDF export finished for review"
		if artifactHash != nil {
			input.ArtifactHash = bytes.Clone(artifactHash)
		}
	}
	return &tammyv1.BasExportEvent{
		ExportEventId: testUUID(eventID), OrganisationId: organisationID, Event: input,
		ActorUserId: testUUID(36), RecordedAt: timestamppb.New(time.Unix(timestampSeconds+1, 0).UTC()),
	}
}
