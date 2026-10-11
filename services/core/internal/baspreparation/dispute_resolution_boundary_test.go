//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestDisputeResolutionRepositoryRetainsTwoHundredExactSubjects(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 93, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	observations := newExternalObservationRepositoryForTest(t, tx)
	var subjects []*tammyv1.SourceRef
	for index := uint64(0); index < 200; index++ {
		_, exact := appendExternalObservationForRepositoryTest(t, observations, org, frozen, 72000+index,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791714600+int64(index))
		subjects = append(subjects, exact)
	}
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	value, exact := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family, subjects, 72200, 1791715000)
	got, err := repository.GetExact(ctx, org, exact)
	if err != nil || got == nil || len(got.Resolution.DisputedObservations) != 200 {
		t.Fatalf("lost retained boundary subjects: value=%v err=%v", got, err)
	}
	assertBASRepositoryCount(t, tx, "bas_dispute_resolution_observations_v1", 200)
	tooMany := proto.Clone(value).(*tammyv1.BasExternalDisputeResolution)
	tooMany.ResolutionId = testUUID(72201)
	tooMany.Resolution.DisputedObservations = append(tooMany.Resolution.DisputedObservations, cloneRef(subjects[0]))
	if ref, err := repository.Append(ctx, org, fixture.frozen.Family, testUUID(72202), testUUID(72203), tooMany); ref != nil || err == nil {
		t.Fatalf("accepted 201 resolution subjects: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 201)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolutions_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolution_observations_v1", 200)
}

func TestDisputeResolutionRepositoryUsesFinalFamilyEventCapacity(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 94, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	observations := newExternalObservationRepositoryForTest(t, tx)
	base, subject := appendExternalObservationForRepositoryTest(t, observations, org, frozen, 73000,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791715100)
	// Seed structurally valid typed facts without repeated parent readbacks. This
	// exercises physical storage capacity; Task 8 applies the live policy census.
	for index := uint64(1); index < 999; index++ {
		value := proto.Clone(base).(*tammyv1.BasExternalObservation)
		value.ObservationId = testUUID(73000 + index)
		wire, hash, err := sealObservation(value)
		if err != nil {
			t.Fatal(err)
		}
		eventID := testUUID(75000 + index)
		claimedAt, _ := basTimestampSQLText(value.GetObservation().ClaimedEventAt)
		recordedAt, _ := basTimestampSQLText(value.RecordedAt)
		if _, err := tx.ExecContext(ctx, `INSERT INTO bas_family_events_v1(
			event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at)
			VALUES(?,?,?,'OBSERVATION',?,?,?,?,?)`, eventID, org, fixture.frozen.Family.Id, value.ObservationId, claimedAt, wire, hash, recordedAt); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bas_external_observations_v1(
			observation_id,event_id,organisation_id,family_id,frozen_id,frozen_hash,event_kind,channel,claimed_event_at,
			external_reference,reason,trust,actor_user_id,recorded_at,content_hash,observation_proto)
			SELECT ?,?,organisation_id,family_id,frozen_id,frozen_hash,event_kind,channel,claimed_event_at,
			external_reference,reason,trust,actor_user_id,recorded_at,?,? FROM bas_external_observations_v1 WHERE observation_id=?`,
			value.ObservationId, eventID, hash, wire, subject.Id); err != nil {
			t.Fatal(err)
		}
	}
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	_, exact := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family, []*tammyv1.SourceRef{subject}, 76000, 1791715200)
	if got, err := repository.GetExact(ctx, org, exact); got == nil || err != nil {
		t.Fatalf("final retained event is unreadable: %v", err)
	}
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 1000)
	value := disputeResolutionForRepositoryTest(fixture.frozen.Family, []*tammyv1.SourceRef{subject}, 76001, 1791715300)
	if ref, err := repository.Append(ctx, org, fixture.frozen.Family, testUUID(76002), testUUID(76003), value); ref != nil || !errors.Is(err, ErrSnapshotBound) {
		t.Fatalf("accepted resolution above physical family capacity: ref=%v err=%v", ref, err)
	}
	observation := externalObservationForRepositoryTest(org, frozen, 76004, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791715300)
	if ref, err := observations.Append(ctx, org, testUUID(76005), testUUID(76006), observation); ref != nil || !errors.Is(err, ErrSnapshotBound) {
		t.Fatalf("accepted observation above physical family capacity: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 1000)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolutions_v1", 1)
}

func TestDisputeResolutionRepositoryDuplicateIdentityRollsBackNewCoreEvent(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 95, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subject := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx), org, frozen, 77000,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791715400)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	value, exact := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family, []*tammyv1.SourceRef{subject}, 77001, 1791715500)
	if ref, err := repository.Append(ctx, org, fixture.frozen.Family, testUUID(77002), testUUID(77003), value); ref != nil || err == nil {
		t.Fatalf("replaced retained resolution identity: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 2)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolutions_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolution_observations_v1", 1)
	if got, err := repository.GetExact(ctx, org, exact); got == nil || err != nil {
		t.Fatalf("failed duplicate damaged original resolution: %v", err)
	}
}
