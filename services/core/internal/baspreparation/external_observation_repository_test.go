//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestExternalObservationRepositoryRetainsExactFrozenHistoryAcrossOwnerAdvanceAndReopen(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 61, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil || frozenRef == nil {
		t.Fatalf("append exact frozen observation parent: ref=%v err=%v", frozenRef, err)
	}

	families, err := NewFamilyStateRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	// The frozen fixture retains family identity and preparation history; seed its
	// workflow aggregate explicitly before asserting that storage leaves it alone.
	rows, err := tx.QueryContext(ctx, "SELECT content_hash FROM bas_preparation_versions_v1 WHERE organisation_id=? AND preparation_id=? AND version=1", organisationID, fixture.preparationRef.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("missing collecting family root")
	}
	var rootHash []byte
	if err := rows.Scan(&rootHash); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	root := &tammyv1.SourceRef{Type: "bas_preparation", Id: fixture.preparationRef.Id, Revision: 1, ContentHash: rootHash}
	rootOwner, err := newPreparationReaderForTest(t, tx).GetExact(ctx, organisationID, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := families.Append(ctx, organisationID, initialTypedFamilyState(rootOwner.Preparation, root), nil); err != nil {
		t.Fatal(err)
	}
	familyStateBefore, err := families.GetCurrent(ctx, organisationID, fixture.frozen.Family)
	if err != nil {
		t.Fatalf("read family state before observation: %v", err)
	}
	repository := newExternalObservationRepositoryForTest(t, tx)
	value := externalObservationForRepositoryTest(organisationID, frozenRef, 62001,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791712800)
	before := proto.Clone(value).(*tammyv1.BasExternalObservation)
	observationRef, err := repository.Append(ctx, organisationID, testUUID(62101), testUUID(62201), value)
	if err != nil || observationRef == nil {
		t.Fatalf("append user-recorded observation: ref=%v err=%v", observationRef, err)
	}
	if !proto.Equal(value, before) {
		t.Fatal("append mutated the caller-owned observation")
	}
	want := proto.Clone(before).(*tammyv1.BasExternalObservation)
	want.ContentHash = observationRef.ContentHash
	if observationRef.Type != "bas_external_observation" || observationRef.Revision != 1 || len(observationRef.ContentHash) != 32 {
		t.Fatalf("append returned a non-exact observation reference: %v", observationRef)
	}
	got, err := repository.GetExact(ctx, organisationID, observationRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("exact observation read: value=%v err=%v", got, err)
	}
	familyStateAfter, err := families.GetCurrent(ctx, organisationID, fixture.frozen.Family)
	if err != nil || !proto.Equal(familyStateAfter, familyStateBefore) {
		t.Fatalf("storage append must not apply family workflow policy: state=%v err=%v", familyStateAfter, err)
	}

	request := ExternalObservationListRequest{OrganisationID: organisationID, Frozen: frozenRef, PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || page.Page == nil || len(page.Observations) != 1 || page.Page.ReturnedCount != 1 || page.Page.NextCursor != nil {
		t.Fatalf("list history for exact frozen target: page=%v err=%v", page, err)
	}
	if !sameBASSourceRef(page.Observations[0].Observation, observationRef) ||
		!sameBASSourceRef(page.Observations[0].FrozenBas, frozenRef) ||
		page.Observations[0].EventKind == nil || *page.Observations[0].EventKind != before.GetObservation().EventKind ||
		page.Observations[0].CorrectionKind != nil || page.Observations[0].Trust != before.Trust {
		t.Fatalf("ordinary observation summary lost its exact fields: %v", page.Observations[0])
	}
	if err := protovalidate.Validate(page.Observations[0]); err != nil || !proto.Equal(page.Observations[0].RecordedAt, before.RecordedAt) {
		t.Fatalf("ordinary summary violates its contract or lost recorded time: %v", err)
	}

	// An exact historical observation remains readable and listable when the
	// preparation owner's current validation pointer advances.
	newValidationRef := appendWarningAcknowledgementValidationRevision(t, tx, organisationID, fixture.ackFixture)
	newPreparationRef := appendWarningAcknowledgementCurrentOwner(t, tx, organisationID,
		fixture.preparationRef, newValidationRef)
	if sameBASSourceRef(newPreparationRef, fixture.preparationRef) {
		t.Fatal("fixture did not advance the current preparation owner")
	}
	got, err = repository.GetExact(ctx, organisationID, observationRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("historical observation changed after current owner advanced: value=%v err=%v", got, err)
	}
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Observations) != 1 || !sameBASSourceRef(page.Observations[0].Observation, observationRef) {
		t.Fatalf("historical observation list changed after current owner advanced: page=%v err=%v", page, err)
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
	repository = newExternalObservationRepositoryForTest(t, reopened)
	got, err = repository.GetExact(ctx, organisationID, observationRef)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("exact observation did not survive encrypted reopen: value=%v err=%v", got, err)
	}
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Observations) != 1 || !sameBASSourceRef(page.Observations[0].Observation, observationRef) {
		t.Fatalf("frozen observation history did not survive encrypted reopen: page=%v err=%v", page, err)
	}
}

func TestExternalObservationRepositoryListsChainedPartialCorrections(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 62, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatalf("append frozen correction parent: %v", err)
	}
	repository := newExternalObservationRepositoryForTest(t, tx)
	original, originalRef := appendExternalObservationForRepositoryTest(t, repository, organisationID, frozenRef, 62301,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE, 1791712900)
	if original == nil || originalRef == nil {
		t.Fatal("test did not retain the original observation")
	}

	// A replacement may provide only a corrected event kind. Its exact subject
	// remains the earlier ordinary observation, and its frozen target is pinned.
	firstCorrection := externalCorrectionForRepositoryTest(organisationID, frozenRef, 62302, originalRef,
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION.Enum(), nil, 1791713000)
	firstCorrectionRef, err := repository.Append(ctx, organisationID, testUUID(62402), testUUID(62502), firstCorrection)
	if err != nil || firstCorrectionRef == nil {
		t.Fatalf("append first partial correction: ref=%v err=%v", firstCorrectionRef, err)
	}
	firstCorrectionWant := proto.Clone(firstCorrection).(*tammyv1.BasExternalObservation)
	firstCorrectionWant.ContentHash = firstCorrectionRef.ContentHash
	if firstCorrection.GetCorrection().CorrectedClaimedEventAt != nil || firstCorrection.GetCorrection().CorrectedEventKind == nil {
		t.Fatal("fixture did not preserve event-kind-only correction presence")
	}

	// A correction can target the earlier correction and provide only a corrected
	// claim time; this keeps the full chain append-only and its optional fields exact.
	secondCorrection := externalCorrectionForRepositoryTest(organisationID, frozenRef, 62303, firstCorrectionRef,
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION, nil,
		timestamppb.New(time.Unix(1791712600, 0).UTC()), 1791713100)
	secondCorrectionRef, err := repository.Append(ctx, organisationID, testUUID(62403), testUUID(62503), secondCorrection)
	if err != nil || secondCorrectionRef == nil {
		t.Fatalf("append chained time-only correction: ref=%v err=%v", secondCorrectionRef, err)
	}
	secondCorrectionWant := proto.Clone(secondCorrection).(*tammyv1.BasExternalObservation)
	secondCorrectionWant.ContentHash = secondCorrectionRef.ContentHash
	if secondCorrection.GetCorrection().CorrectedEventKind != nil || secondCorrection.GetCorrection().CorrectedClaimedEventAt == nil {
		t.Fatal("fixture did not preserve claim-time-only correction presence")
	}

	for _, retained := range []struct {
		ref  *tammyv1.SourceRef
		want *tammyv1.BasExternalObservation
	}{{originalRef, withObservationRepositoryHash(original, originalRef)},
		{firstCorrectionRef, firstCorrectionWant}, {secondCorrectionRef, secondCorrectionWant}} {
		got, err := repository.GetExact(ctx, organisationID, retained.ref)
		if err != nil || !proto.Equal(got, retained.want) {
			t.Fatalf("exact correction-chain read %s: value=%v err=%v", retained.ref.Id, got, err)
		}
	}

	page, err := repository.List(ctx, ExternalObservationListRequest{OrganisationID: organisationID, Frozen: frozenRef, PageSize: 200})
	if err != nil || page == nil || page.Page == nil || page.Page.ReturnedCount != 3 || len(page.Observations) != 3 || page.Page.NextCursor != nil {
		t.Fatalf("list ordinary observation and chained corrections: page=%v err=%v", page, err)
	}
	summaries := make(map[string]*tammyv1.BasExternalObservationSummary, len(page.Observations))
	for _, summary := range page.Observations {
		if summary == nil || summary.Observation == nil {
			t.Fatalf("history page contains an incomplete summary: %v", summary)
		}
		if err := protovalidate.Validate(summary); err != nil {
			t.Fatalf("ordinary/correction summary violates its contract: %v", err)
		}
		summaries[summary.Observation.Id] = summary
	}
	ordinary := summaries[originalRef.Id]
	first := summaries[firstCorrectionRef.Id]
	second := summaries[secondCorrectionRef.Id]
	if ordinary == nil || ordinary.EventKind == nil || *ordinary.EventKind != original.GetObservation().EventKind || ordinary.CorrectionKind != nil || ordinary.CorrectsObservation != nil {
		t.Fatalf("ordinary event summary is malformed: %v", ordinary)
	}
	if first == nil || first.EventKind != nil || first.CorrectionKind == nil || *first.CorrectionKind != firstCorrection.GetCorrection().CorrectionKind ||
		!sameBASSourceRef(first.CorrectsObservation, originalRef) {
		t.Fatalf("first correction summary lost its exact subject: %v", first)
	}
	if second == nil || second.EventKind != nil || second.CorrectionKind == nil || *second.CorrectionKind != secondCorrection.GetCorrection().CorrectionKind ||
		!sameBASSourceRef(second.CorrectsObservation, firstCorrectionRef) {
		t.Fatalf("chained correction summary lost its exact subject: %v", second)
	}
}

func TestExternalObservationRepositoryCursorPinsFrozenSnapshotAcrossLateAppend(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 63, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	repository := newExternalObservationRepositoryForTest(t, tx)
	_, firstRef := appendExternalObservationForRepositoryTest(t, repository, organisationID, frozenRef, 62601,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791713200)
	_, secondRef := appendExternalObservationForRepositoryTest(t, repository, organisationID, frozenRef, 62602,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN, 1791713300)
	request := ExternalObservationListRequest{OrganisationID: organisationID, Frozen: frozenRef, PageSize: 1}
	firstPage, err := repository.List(ctx, request)
	if err != nil || firstPage == nil || firstPage.Page == nil || len(firstPage.Observations) != 1 || firstPage.Page.NextCursor == nil {
		t.Fatalf("first frozen history page: page=%v err=%v", firstPage, err)
	}
	thirdValue := externalObservationForRepositoryTest(organisationID, frozenRef, 62603,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT, 1791713400)
	thirdRef, err := repository.Append(ctx, organisationID, testUUID(62703), testUUID(62803), thirdValue)
	if err != nil {
		t.Fatalf("append event after signed history watermark: %v", err)
	}
	request.Cursor = firstPage.Page.NextCursor
	secondPage, err := repository.List(ctx, request)
	if err != nil || secondPage == nil || secondPage.Page == nil || len(secondPage.Observations) != 1 || secondPage.Page.NextCursor != nil {
		t.Fatalf("continue pinned frozen history page: page=%v err=%v", secondPage, err)
	}
	seen := map[string]bool{firstPage.Observations[0].Observation.Id: true, secondPage.Observations[0].Observation.Id: true}
	if !seen[firstRef.Id] || !seen[secondRef.Id] || seen[thirdRef.Id] {
		t.Fatalf("late append changed the pinned history: first=%v second=%v late=%v", firstPage, secondPage, thirdRef)
	}
	fresh, err := repository.List(ctx, ExternalObservationListRequest{OrganisationID: organisationID, Frozen: frozenRef, PageSize: 200})
	if err != nil || fresh == nil || len(fresh.Observations) != 3 {
		t.Fatalf("fresh list should include the late append: page=%v err=%v", fresh, err)
	}
}

func TestExternalObservationRepositoryRejectsCrossScopeAndInvalidCorrectionSubjects(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 64, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	repository := newExternalObservationRepositoryForTest(t, tx)
	originalObservation, observationRef := appendExternalObservationForRepositoryTest(t, repository, organisationID, frozenRef, 62901,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791713500)

	if got, err := repository.GetExact(ctx, otherOrganisationID, observationRef); got != nil || err == nil {
		t.Fatalf("exact observation read crossed organisations: value=%v err=%v", got, err)
	}
	wrongHash := cloneRef(observationRef)
	wrongHash.ContentHash[0] ^= 0xff
	if got, err := repository.GetExact(ctx, organisationID, wrongHash); got != nil || err == nil {
		t.Fatalf("exact observation read accepted a changed hash: value=%v err=%v", got, err)
	}
	if page, err := repository.List(ctx, ExternalObservationListRequest{OrganisationID: otherOrganisationID, Frozen: frozenRef, PageSize: 20}); page != nil || err == nil {
		t.Fatalf("frozen history list crossed organisations: page=%v err=%v", page, err)
	}
	if ref, err := repository.Append(ctx, otherOrganisationID, testUUID(63001), testUUID(63101), originalObservation); ref != nil || err == nil {
		t.Fatalf("append resolved an exact frozen parent outside its organisation: ref=%v err=%v", ref, err)
	}
	wrongFrozenHash := cloneRef(frozenRef)
	wrongFrozenHash.ContentHash[0] ^= 0xff
	if page, err := repository.List(ctx, ExternalObservationListRequest{OrganisationID: organisationID, Frozen: wrongFrozenHash, PageSize: 20}); page != nil || err == nil {
		t.Fatalf("frozen history list accepted a changed parent hash: page=%v err=%v", page, err)
	}

	wrongOrgBody := externalObservationForRepositoryTest(otherOrganisationID, frozenRef, 62902,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN, 1791713600)
	if ref, err := repository.Append(ctx, organisationID, testUUID(63002), testUUID(63102), wrongOrgBody); ref != nil || err == nil {
		t.Fatalf("append accepted an observation with a foreign organisation: ref=%v err=%v", ref, err)
	}
	wrongFrozen := externalObservationForRepositoryTest(organisationID, wrongFrozenHash, 62903,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN, 1791713700)
	if ref, err := repository.Append(ctx, organisationID, testUUID(63003), testUUID(63103), wrongFrozen); ref != nil || err == nil {
		t.Fatalf("append accepted an altered exact frozen parent: ref=%v err=%v", ref, err)
	}

	missingSubject := &tammyv1.SourceRef{Type: "bas_external_observation", Id: testUUID(63201), Revision: 1, ContentHash: testHash(201)}
	foreignCorrection := externalCorrectionForRepositoryTest(organisationID, frozenRef, 62904, missingSubject,
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil, 1791713800)
	if ref, err := repository.Append(ctx, organisationID, testUUID(63004), testUUID(63104), foreignCorrection); ref != nil || err == nil {
		t.Fatalf("append accepted a correction whose exact subject is not retained in this family: ref=%v err=%v", ref, err)
	}
	wrongSubjectHash := cloneRef(observationRef)
	wrongSubjectHash.ContentHash[0] ^= 0xff
	changedHashCorrection := externalCorrectionForRepositoryTest(organisationID, frozenRef, 62906, wrongSubjectHash,
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil, 1791713950)
	if ref, err := repository.Append(ctx, organisationID, testUUID(63006), testUUID(63106), changedHashCorrection); ref != nil || err == nil {
		t.Fatalf("append accepted a correction with an altered exact subject hash: ref=%v err=%v", ref, err)
	}

	mismatchedSubject := externalCorrectionForRepositoryTest(organisationID, frozenRef, 62905, observationRef,
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil, 1791713900)
	mismatchedSubject.CorrectsObservation = cloneRef(missingSubject)
	if ref, err := repository.Append(ctx, organisationID, testUUID(63005), testUUID(63105), mismatchedSubject); ref != nil || err == nil {
		t.Fatalf("append accepted disagreement between the normalized correction subject and body link: ref=%v err=%v", ref, err)
	}

	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_external_observations_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_external_observation_corrections_v1", 0)
}

func TestExternalObservationRepositoryExactReadRejectsCorruptBodyScalarEventAndParents(t *testing.T) {
	for index, kind := range []string{"observation_body", "normalized_scalar", "family_event_body", "frozen_parent", "correction_parent", "correction_claimed_at"} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedFrozenRepositoryFixture(t, tx, organisationID, uint64(70+index), true, 0)
			frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
			if err != nil {
				t.Fatalf("append frozen corruption parent: %v", err)
			}
			repository := newExternalObservationRepositoryForTest(t, tx)
			_, originalRef := appendExternalObservationForRepositoryTest(t, repository, organisationID, frozenRef, 63301,
				tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE, 1791714000)
			readRef := originalRef
			if kind == "correction_parent" || kind == "correction_claimed_at" {
				correction := externalCorrectionForRepositoryTest(organisationID, frozenRef, 63302, originalRef,
					tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
					tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION.Enum(), nil, 1791714100)
				readRef, err = repository.Append(ctx, organisationID, testUUID(63402), testUUID(63502), correction)
				if err != nil {
					t.Fatalf("append correction before parent corruption: %v", err)
				}
			}

			switch kind {
			case "correction_claimed_at":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_family_events_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_family_events_v1 SET claimed_at='2026-10-01T00:00:00Z' WHERE event_id=(SELECT event_id FROM bas_external_observation_corrections_v1 WHERE correction_id=?)`, readRef.Id); err != nil {
					t.Fatal(err)
				}
			case "observation_body", "normalized_scalar", "correction_parent":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_external_observations_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if kind == "normalized_scalar" {
					if _, err := tx.ExecContext(ctx, `UPDATE bas_external_observations_v1 SET channel='PAPER' WHERE observation_id=?`, originalRef.Id); err != nil {
						t.Fatal(err)
					}
				} else if _, err := tx.ExecContext(ctx, `UPDATE bas_external_observations_v1 SET observation_proto=X'01' WHERE observation_id=?`, originalRef.Id); err != nil {
					t.Fatal(err)
				}
			case "family_event_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_family_events_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_family_events_v1 SET event_proto=X'01' WHERE event_id=(SELECT event_id FROM bas_external_observations_v1 WHERE observation_id=?)`, originalRef.Id); err != nil {
					t.Fatal(err)
				}
			case "frozen_parent":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_frozen_bas_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_frozen_bas_v1 SET frozen_proto=X'01' WHERE frozen_id=?`, frozenRef.Id); err != nil {
					t.Fatal(err)
				}
			}

			if got, err := repository.GetExact(ctx, organisationID, readRef); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("exact read accepted %s corruption: value=%v err=%v", kind, got, err)
			}
		})
	}
}

func TestExternalObservationRepositoryCancelledAppendRollsBackOnlyItsSavepoint(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 76, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	wantLegalName := writeCallerWorkBeforeRepositoryAppend(t, tx, organisationID, "observation")
	appendCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	executor := &cancelAfterRepositoryParentInsertExecutor{
		CommandSQLExecutor: tx,
		parentTable:        "bas_family_events_v1",
		cancel:             cancel,
	}
	value := externalObservationForRepositoryTest(organisationID, frozenRef, 63601,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791714200)
	ref, err := newExternalObservationRepositoryForTest(t, executor).Append(appendCtx, organisationID,
		testUUID(63701), testUUID(63801), value)
	if ref != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after family event insert should abort append: ref=%v err=%v", ref, err)
	}
	if !executor.parentInserted {
		t.Fatal("test did not cancel after inserting the Core family event")
	}
	assertBoundedUncancelledSavepointCleanup(t, executor)
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 0)
	assertBASRepositoryCount(t, tx, "bas_external_observations_v1", 0)
	assertBASRepositoryCount(t, tx, "bas_external_observation_corrections_v1", 0)
	assertCallerWorkCanCommit(t, workspace.Database, tx, organisationID, wantLegalName, "observation")
}

func TestExternalObservationRepositoryOperationKeyByteBoundary(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 91, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	repository := newExternalObservationRepositoryForTest(t, tx)
	for index, key := range []string{strings.Repeat("é", 129), strings.Repeat("🙂", 65)} {
		value := externalObservationForRepositoryTest(org, frozen, uint64(69100+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791714200)
		ref, err := repository.Append(ctx, org, key, testUUID(uint64(69200+index)), value)
		if ref != nil || !errors.Is(err, ErrExternalObservationRepository) {
			t.Fatalf("accepted %d-byte operation key: ref=%v err=%v", len(key), ref, err)
		}
		assertBASRepositoryCount(t, tx, "bas_family_events_v1", 0)
		assertBASRepositoryCount(t, tx, "bas_external_observations_v1", 0)
	}
	value := externalObservationForRepositoryTest(org, frozen, 69102,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791714200)
	ref, err := repository.Append(ctx, org, strings.Repeat("é", 128), testUUID(69202), value)
	if err != nil || ref == nil {
		t.Fatalf("rejected exact 256-byte operation key: ref=%v err=%v", ref, err)
	}
	got, err := repository.GetExact(ctx, org, ref)
	if err != nil || !proto.Equal(got, withObservationRepositoryHash(value, ref)) {
		t.Fatalf("256-byte key wrote an unreadable observation: value=%v err=%v", got, err)
	}
}

func appendExternalObservationForRepositoryTest(t *testing.T, repository *ExternalObservationRepository,
	organisationID string, frozenRef *tammyv1.SourceRef, id uint64, kind tammyv1.BasExternalEventKind,
	recordedAt int64) (*tammyv1.BasExternalObservation, *tammyv1.SourceRef) {
	t.Helper()
	value := externalObservationForRepositoryTest(organisationID, frozenRef, id, kind, recordedAt)
	ref, err := repository.Append(context.Background(), organisationID, testUUID(64000+id), testUUID(65000+id), value)
	if err != nil || ref == nil {
		t.Fatalf("append test observation %d: ref=%v err=%v", id, ref, err)
	}
	return value, ref
}

func externalObservationForRepositoryTest(organisationID string, frozenRef *tammyv1.SourceRef, id uint64,
	kind tammyv1.BasExternalEventKind, recordedAt int64) *tammyv1.BasExternalObservation {
	return &tammyv1.BasExternalObservation{
		ObservationId: testUUID(id), OrganisationId: organisationID, FrozenBas: cloneRef(frozenRef),
		Payload: &tammyv1.BasExternalObservation_Observation{Observation: &tammyv1.BasExternalObservationInput{
			EventKind: kind, Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_ONLINE_PORTAL,
			ClaimedEventAt: timestamppb.New(time.Unix(recordedAt-300, 0).UTC()),
			Evidence:       []*tammyv1.SourceRef{sourceRef("bas_external_document", testUUID(66000+id), 1, byte(id%255))},
			Reason:         "User-recorded external event retained for the exact frozen statement",
		}},
		Trust:       tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED,
		ActorUserId: testUUID(72), RecordedAt: timestamppb.New(time.Unix(recordedAt, 0).UTC()),
	}
}

func externalCorrectionForRepositoryTest(organisationID string, frozenRef *tammyv1.SourceRef, id uint64,
	subject *tammyv1.SourceRef, kind tammyv1.BasCorrectionKind, correctedKind *tammyv1.BasExternalEventKind,
	correctedAt *timestamppb.Timestamp, recordedAt int64) *tammyv1.BasExternalObservation {
	return &tammyv1.BasExternalObservation{
		ObservationId: testUUID(id), OrganisationId: organisationID, FrozenBas: cloneRef(frozenRef),
		Payload: &tammyv1.BasExternalObservation_Correction{Correction: &tammyv1.BasExternalObservationCorrectionInput{
			DisputedObservation: cloneRef(subject), CorrectionKind: kind, CorrectedEventKind: correctedKind,
			CorrectedClaimedEventAt: correctedAt, Reason: "Correct an earlier user-recorded external assertion",
		}},
		Trust:       tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED,
		ActorUserId: testUUID(72), RecordedAt: timestamppb.New(time.Unix(recordedAt, 0).UTC()),
		CorrectsObservation: cloneRef(subject),
	}
}

func withObservationRepositoryHash(value *tammyv1.BasExternalObservation, exact *tammyv1.SourceRef) *tammyv1.BasExternalObservation {
	retained := proto.Clone(value).(*tammyv1.BasExternalObservation)
	retained.ContentHash = exact.ContentHash
	return retained
}

func newExternalObservationRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *ExternalObservationRepository {
	t.Helper()
	repository, err := NewExternalObservationRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct external observation repository: %v", err)
	}
	return repository
}
