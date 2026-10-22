// Proposed services/core/internal/baspreparation/external_history_reader_sqlcipher_test.go
// Native SQLCipher tests. No direct-in-memory stand-in: these exercise the
// encrypted repository transaction and the public typed exact readers.

//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
)

func TestBASExternalHistoryReaderUsesExactFamilyAndCoreOrdinal(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)

	root, confirmed := seedBASHistoryConfirmedRoot(t, tx, org, codec)
	confirmedRef := preparationReference(confirmed.Preparation)

	families, err := NewFamilyStateRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	state, err := families.GetCurrent(ctx, org, root.Family)
	if err != nil {
		t.Fatal(err)
	}
	next := proto.Clone(state).(*tammyv1.BasFamilyState)
	next.Version++
	next.ContentHash = nil
	next.EffectiveOriginal = cloneRef(confirmedRef)
	next.LatestHead = retainedExpectedIdentity(confirmedRef, confirmedRef.Revision)
	next.UpdatedAt.Seconds++
	if _, err := families.Append(ctx, org, next, familyStateReference(state)); err != nil {
		t.Fatal(err)
	}

	// Interleave another family's event. The target family sees a gap in the
	// global ordinal and must retain the Core order rather than timestamp/ID order.
	otherFamily := validBASFamilyIdentity(otherOrg, testUUID(99100))
	familyIdentities, err := NewFamilyIdentityRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	otherFamilyRef, err := familyIdentities.Append(ctx, otherFamily)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bas_family_events_v1(
		event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at)
		VALUES(?,? ,?,'OBSERVATION',?,'2026-10-10T00:00:00Z',?,?, '2026-10-10T00:00:00Z')`,
		testUUID(99101), otherOrg, otherFamilyRef.Id, "history-interleave", []byte{1}, testHash(201)); err != nil {
		t.Fatalf("insert foreign-family ordinal gap: %v", err)
	}

	observations, err := NewExternalObservationRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	acceptance, acceptanceRef := appendExternalObservationForRepositoryTest(t, observations, org,
		confirmed.Preparation.FrozenBas, 99102,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE, 1791714000)
	_, rejectionRef := appendExternalObservationForRepositoryTest(t, observations, org,
		confirmed.Preparation.FrozenBas, 99103,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791713000)

	var lastOrdinal int64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(event_ordinal) FROM bas_family_events_v1 WHERE organisation_id=? AND family_id=?`,
		org, root.Family.Id).Scan(&lastOrdinal); err != nil || lastOrdinal <= 0 {
		t.Fatalf("read final Core event ordinal: ordinal=%d err=%v", lastOrdinal, err)
	}
	state, err = families.GetCurrent(ctx, org, root.Family)
	if err != nil {
		t.Fatal(err)
	}
	next = proto.Clone(state).(*tammyv1.BasFamilyState)
	next.Version++
	next.ContentHash = nil
	next.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	next.LastFamilyEventOrdinal = uint64(lastOrdinal)
	next.UpdatedAt.Seconds++
	if _, err := families.Append(ctx, org, next, familyStateReference(state)); err != nil {
		t.Fatal(err)
	}

	reader, err := NewBASExternalHistoryReader(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	history, err := reader.Read(ctx, org, root.Family)
	if err != nil {
		t.Fatalf("load complete retained family history: %v", err)
	}
	if len(history.Attempts) != 1 || len(history.ConfirmedDependencies) != 1 || len(history.Records) != 2 ||
		history.LastOrdinal != uint64(lastOrdinal) || history.Records[0].Ordinal >= history.Records[1].Ordinal ||
		!sameBASSourceRef(history.Records[0].Observation.FrozenBas, confirmed.Preparation.FrozenBas) ||
		!sameBASSourceRef(history.Records[0].Observation.FrozenBas, history.Records[1].Observation.FrozenBas) ||
		!sameBASSourceRef(observationReferenceFor(history.Records[0].Observation), acceptanceRef) ||
		!sameBASSourceRef(observationReferenceFor(history.Records[1].Observation), rejectionRef) {
		t.Fatalf("history omitted attempts/events or lost Core order: %+v", history)
	}
	if !history.Records[0].Observation.RecordedAt.AsTime().After(history.Records[1].Observation.RecordedAt.AsTime()) {
		t.Fatal("fixture timestamps should disagree with the retained Core append order")
	}
	if !proto.Equal(acceptance, history.Records[0].Observation) {
		want := proto.Clone(acceptance).(*tammyv1.BasExternalObservation)
		want.ContentHash = acceptanceRef.ContentHash
		if !proto.Equal(want, history.Records[0].Observation) {
			t.Fatal("the loaded complete body does not match its exact typed repository body")
		}
	}
}

func TestBASExternalHistoryReaderRejectsOmittedEventAndCrossOwner(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	familyRef, confirmed, lastOrdinal := seedBASHistoryReaderFixture(t, tx, org, codec)
	reader, err := NewBASExternalHistoryReader(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reader.Read(ctx, otherOrg, familyRef); got.Records != nil || err == nil {
		t.Fatalf("a retained family crossed organisation scope: history=%+v err=%v", got, err)
	}

	// Move one immutable event out of the selected family with deferred FK
	// checks. Its family-state watermark still names that exact final event, so
	// the complete history read must fail rather than return a shortened prefix.
	alternate := validBASFamilyIdentity(org, testUUID(99200))
	alternate.PeriodScope.StartDate = testCivilDate(2025, 1, 1)
	alternate.PeriodScope.EndDate = testCivilDate(2025, 3, 31)
	alternate.Window = tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR
	familyIdentities, err := NewFamilyIdentityRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	alternateRef, err := familyIdentities.Append(ctx, alternate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_family_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err := tx.QueryRowContext(ctx, `SELECT event_id FROM bas_family_events_v1 WHERE organisation_id=? AND family_id=? AND event_ordinal=?`,
		org, familyRef.Id, int64(lastOrdinal)).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_family_events_v1 SET family_id=? WHERE event_id=? AND organisation_id=? AND family_id=?`,
		alternateRef.Id, eventID, org, familyRef.Id); err != nil {
		t.Fatalf("tamper fixture event owner: %v", err)
	}
	if got, err := reader.Read(ctx, org, familyRef); got.Records != nil || err == nil {
		t.Fatalf("history accepted an omitted retained event: family=%v last=%d history=%+v err=%v", familyRef, lastOrdinal, got, err)
	}
	_ = confirmed // Keeps the exact seeded head visible in the fixture contract.
}

func TestBASExternalHistoryReaderZeroResultPagesAndCompleteCensusBounds(t *testing.T) {
	t.Run("zero-result pages are complete", func(t *testing.T) {
		workspace, org, _ := newBASRepositoryWorkspace(t)
		tx := beginBASRepositoryTx(t, workspace)
		defer tx.Rollback()
		ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
		fixture := seedFrozenRepositoryFixture(t, tx, org, 993, true, 0)
		frozenRef, err := NewFrozenRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		frozenRefValue, err := frozenRef.Append(ctx, org, fixture.frozen, fixture.preparationRef)
		if err != nil {
			t.Fatal(err)
		}
		preparations, err := NewPreparationReader(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		var rootHash []byte
		if err := tx.QueryRowContext(ctx, `SELECT content_hash FROM bas_preparation_versions_v1 WHERE organisation_id=? AND preparation_id=? AND version=1`,
			org, fixture.preparationRef.Id).Scan(&rootHash); err != nil {
			t.Fatal(err)
		}
		rootRef := &tammyv1.SourceRef{Type: "bas_preparation", Id: fixture.preparationRef.Id, Revision: 1, ContentHash: rootHash}
		root, err := preparations.GetExact(ctx, org, rootRef)
		if err != nil {
			t.Fatal(err)
		}
		families, err := NewFamilyStateRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := families.Append(ctx, org, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
			t.Fatal(err)
		}
		reader, err := NewBASExternalHistoryReader(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		history, err := reader.Read(ctx, org, fixture.frozen.Family)
		if err != nil || len(history.Attempts) != 0 || len(history.Records) != 0 || history.LastOrdinal != 0 {
			t.Fatalf("empty complete family history: history=%+v err=%v", history, err)
		}
		observations, err := NewExternalObservationRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		observationPage, err := observations.List(ctx, ExternalObservationListRequest{OrganisationID: org,
			Frozen: frozenRefValue, PageSize: 10})
		if err != nil || observationPage == nil || observationPage.Page == nil || len(observationPage.Observations) != 0 ||
			observationPage.Page.NextCursor != nil {
			t.Fatalf("empty observation page acquired a phantom continuation: page=%v err=%v", observationPage, err)
		}
		resolutions, err := NewDisputeResolutionRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		resolutionPage, err := resolutions.List(ctx, DisputeResolutionListRequest{OrganisationID: org,
			Family: fixture.frozen.Family, PageSize: 10})
		if err != nil || resolutionPage == nil || resolutionPage.Page == nil || len(resolutionPage.Resolutions) != 0 ||
			resolutionPage.Page.NextCursor != nil {
			t.Fatalf("empty resolution page acquired a phantom continuation: page=%v err=%v", resolutionPage, err)
		}
	})

	t.Run("identity overflow rejects the whole census", func(t *testing.T) {
		workspace, org, _ := newBASRepositoryWorkspace(t)
		tx := beginBASRepositoryTx(t, workspace)
		defer tx.Rollback()
		ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
		root, rootRef := seedTypedCollectingPreparation(t, tx, org)
		families, err := NewFamilyStateRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := families.Append(ctx, org, initialTypedFamilyState(root, rootRef), nil); err != nil {
			t.Fatal(err)
		}
		createdAt, _ := basTimestampSQLText(root.CreatedAt)
		for index := 0; index < maxExternalHistoryAttempts+1; index++ {
			if _, err := tx.ExecContext(ctx, `INSERT INTO bas_preparation_identities_v1(
				preparation_id,organisation_id,family_id,branch_id,attempt_id,created_at) VALUES(?,?,?,NULL,?,?)`,
				testUUID(uint64(400000+index)), org, root.Family.Id, testUUID(uint64(500000+index)), createdAt); err != nil {
				t.Fatalf("insert bounded identity header %d: %v", index, err)
			}
		}
		reader, err := NewBASExternalHistoryReader(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := reader.Read(ctx, org, root.Family); got.Attempts != nil || !errors.Is(err, ErrSnapshotBound) {
			t.Fatalf("partial attempt census was returned/accepted: history=%+v err=%v", got, err)
		}
	})

	t.Run("event overflow rejects before typed bodies", func(t *testing.T) {
		workspace, org, _ := newBASRepositoryWorkspace(t)
		tx := beginBASRepositoryTx(t, workspace)
		defer tx.Rollback()
		ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
		root, rootRef := seedTypedCollectingPreparation(t, tx, org)
		families, err := NewFamilyStateRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := families.Append(ctx, org, initialTypedFamilyState(root, rootRef), nil); err != nil {
			t.Fatal(err)
		}
		at := "2026-10-11T00:00:00Z"
		for index := 0; index < maxExternalHistoryRecords+1; index++ {
			if _, err := tx.ExecContext(ctx, `INSERT INTO bas_family_events_v1(
				event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at)
				VALUES(?,?,?,'OBSERVATION',?,?,?,?,?)`, testUUID(uint64(600000+index)), org, root.Family.Id,
				fmt.Sprintf("history-overflow-%04d", index), at, []byte{1}, bytes.Repeat([]byte{0x72}, 32), at); err != nil {
				t.Fatalf("insert bounded event header %d: %v", index, err)
			}
		}
		reader, err := NewBASExternalHistoryReader(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := reader.Read(ctx, org, root.Family); got.Records != nil || !errors.Is(err, ErrSnapshotBound) {
			t.Fatalf("partial event census was returned/accepted: history=%+v err=%v", got, err)
		}
	})

	t.Run("unresolved subjects above 200 reject the complete history", func(t *testing.T) {
		workspace, org, _ := newBASRepositoryWorkspace(t)
		tx := beginBASRepositoryTx(t, workspace)
		defer tx.Rollback()
		ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
		familyRef, confirmed, _ := seedBASHistoryReaderFixture(t, tx, org, codec)
		observations, err := NewExternalObservationRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		// The fixture has one acceptance and one rejection. Add 199 more
		// distinct rejections so the unresolved dispute contains 201 subjects.
		for index := 0; index < 199; index++ {
			appendExternalObservationForRepositoryTest(t, observations, org,
				confirmed.Preparation.FrozenBas, uint64(99400+index),
				tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791715000+int64(index))
		}
		var last int64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(event_ordinal) FROM bas_family_events_v1 WHERE organisation_id=? AND family_id=?`,
			org, familyRef.Id).Scan(&last); err != nil || last <= 0 {
			t.Fatalf("read final family event: ordinal=%d err=%v", last, err)
		}
		families, err := NewFamilyStateRepository(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		state, err := families.GetCurrent(ctx, org, familyRef)
		if err != nil {
			t.Fatal(err)
		}
		next := proto.Clone(state).(*tammyv1.BasFamilyState)
		next.Version++
		next.ContentHash = nil
		next.LastFamilyEventOrdinal = uint64(last)
		next.UpdatedAt.Seconds++
		if _, err := families.Append(ctx, org, next, familyStateReference(state)); err != nil {
			t.Fatal(err)
		}
		reader, err := NewBASExternalHistoryReader(tx, codec)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := reader.Read(ctx, org, familyRef); got.Records != nil || !errors.Is(err, ErrSnapshotBound) {
			t.Fatalf("loader accepted a partial unresolved dispute set: history=%+v err=%v", got, err)
		}
	})
}

// seedBASHistoryReaderFixture builds a real retained confirmed family, two
// observations and a family-state event watermark in the encrypted database.
func seedBASHistoryReaderFixture(t *testing.T, executor app.CommandSQLExecutor, org string,
	codec *paging.Codec) (*tammyv1.SourceRef, *PreparationRetainedSnapshot, uint64) {
	t.Helper()
	ctx := context.Background()
	root, confirmed := seedBASHistoryConfirmedRoot(t, executor, org, codec)
	confirmedRef := preparationReference(confirmed.Preparation)

	families, err := NewFamilyStateRepository(executor, codec)
	if err != nil {
		t.Fatal(err)
	}
	state, err := families.GetCurrent(ctx, org, root.Family)
	if err != nil {
		t.Fatal(err)
	}
	next := proto.Clone(state).(*tammyv1.BasFamilyState)
	next.Version++
	next.ContentHash = nil
	next.EffectiveOriginal = cloneRef(confirmedRef)
	next.LatestHead = retainedExpectedIdentity(confirmedRef, confirmedRef.Revision)
	next.UpdatedAt.Seconds++
	if _, err := families.Append(ctx, org, next, familyStateReference(state)); err != nil {
		t.Fatal(err)
	}
	observations, err := NewExternalObservationRepository(executor, codec)
	if err != nil {
		t.Fatal(err)
	}
	appendExternalObservationForRepositoryTest(t, observations, org, confirmed.Preparation.FrozenBas, 99301,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE, 1791714000)
	appendExternalObservationForRepositoryTest(t, observations, org, confirmed.Preparation.FrozenBas, 99302,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791713000)
	var last int64
	if err := (basQueryRow{executor: executor, ctx: ctx, query: `SELECT MAX(event_ordinal) FROM bas_family_events_v1 WHERE organisation_id=? AND family_id=?`, args: []any{org, root.Family.Id}}).Scan(&last); err != nil || last <= 0 {
		t.Fatalf("read fixture last family event: ordinal=%d err=%v", last, err)
	}
	state, err = families.GetCurrent(ctx, org, root.Family)
	if err != nil {
		t.Fatal(err)
	}
	next = proto.Clone(state).(*tammyv1.BasFamilyState)
	next.Version++
	next.ContentHash = nil
	next.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	next.LastFamilyEventOrdinal = uint64(last)
	next.UpdatedAt.Seconds++
	if _, err := families.Append(ctx, org, next, familyStateReference(state)); err != nil {
		t.Fatal(err)
	}
	return cloneRef(root.Family), confirmed, uint64(last)
}

func seedBASHistoryConfirmedRoot(t *testing.T, executor app.CommandSQLExecutor, org string, codec *paging.Codec) (*tammyv1.BasPreparation, *PreparationRetainedSnapshot) {
	t.Helper()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, executor, org, 991, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, executor).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	preparations, err := NewPreparationRepository(executor, codec)
	if err != nil {
		t.Fatal(err)
	}
	var rootHash []byte
	if err := (basQueryRow{executor: executor, ctx: ctx, query: `SELECT content_hash FROM bas_preparation_versions_v1 WHERE organisation_id=? AND preparation_id=? AND version=1`, args: []any{org, fixture.preparationRef.Id}}).Scan(&rootHash); err != nil {
		t.Fatal(err)
	}
	rootRef := &tammyv1.SourceRef{Type: "bas_preparation", Id: fixture.preparationRef.Id, Revision: 1, ContentHash: rootHash}
	root, err := preparations.GetExact(ctx, org, rootRef)
	if err != nil {
		t.Fatal(err)
	}
	families, err := NewFamilyStateRepository(executor, codec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := families.Append(ctx, org, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
		t.Fatal(err)
	}
	current, err := preparations.GetExact(ctx, org, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []tammyv1.BasPreparationState{tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED} {
		next := clonePreparationRetainedSnapshot(current)
		next.Preparation.Version++
		next.Preparation.ContentHash = nil
		next.Preparation.State = state
		next.Preparation.FrozenBas = cloneRef(frozenRef)
		ref, err := preparations.Append(ctx, next, preparationReference(current.Preparation))
		if err != nil {
			t.Fatal(err)
		}
		current, err = preparations.GetExact(ctx, org, ref)
		if err != nil {
			t.Fatal(err)
		}
	}
	return root.Preparation, current
}
