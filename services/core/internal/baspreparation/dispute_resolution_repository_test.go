//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDisputeResolutionRepositoryAppendExactAndCursorRetainHistoricalProof(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 81, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil || frozenRef == nil {
		t.Fatalf("retain resolution subject frozen approval: ref=%v err=%v", frozenRef, err)
	}
	observations := newExternalObservationRepositoryForTest(t, tx)
	_, subjectRef := appendExternalObservationForRepositoryTest(t, observations, organisationID, frozenRef, 68501,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791712800)

	repository := newDisputeResolutionRepositoryForTest(t, tx)
	first, firstRef := appendDisputeResolutionForRepositoryTest(t, repository, organisationID,
		fixture.frozen.Family, []*tammyv1.SourceRef{subjectRef}, 68601, 1791713000)
	second, secondRef := appendDisputeResolutionForRepositoryTest(t, repository, organisationID,
		fixture.frozen.Family, []*tammyv1.SourceRef{subjectRef}, 68602, 1791713100)
	assertBASRepositoryCount(t, tx, "bas_family_state_v1", 0)
	for _, retained := range []struct {
		value *tammyv1.BasExternalDisputeResolution
		ref   *tammyv1.SourceRef
	}{{first, firstRef}, {second, secondRef}} {
		want := proto.Clone(retained.value).(*tammyv1.BasExternalDisputeResolution)
		want.ContentHash = append([]byte(nil), retained.ref.ContentHash...)
		got, err := repository.GetExact(ctx, organisationID, retained.ref)
		if err != nil || !proto.Equal(got, want) {
			t.Fatalf("exact resolution read %s: value=%v want=%v err=%v", retained.ref.Id, got, want, err)
		}
	}
	wrongOrganisation := testUUID(9102)
	if got, err := repository.GetExact(ctx, wrongOrganisation, firstRef); got != nil || err == nil {
		t.Fatalf("exact resolution read crossed organisation scope: value=%v err=%v", got, err)
	}
	wrongHash := cloneRef(firstRef)
	wrongHash.ContentHash[0] ^= 0xff
	if got, err := repository.GetExact(ctx, organisationID, wrongHash); got != nil || err == nil {
		t.Fatalf("exact resolution read accepted a stale content hash: value=%v err=%v", got, err)
	}
	wrongType := cloneRef(firstRef)
	wrongType.Type = "bas_calculation"
	if got, err := repository.GetExact(ctx, organisationID, wrongType); got != nil || err == nil {
		t.Fatalf("exact resolution read accepted a different resource type: value=%v err=%v", got, err)
	}
	if first.Resolution.Outcome != tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN ||
		len(first.Resolution.AffectedConfirmedChain) != 0 || len(first.Resolution.DependentChainManifestHash) != 32 {
		t.Fatal("fixture must represent a structurally valid upheld pre-head resolution with an explicit empty dependency manifest")
	}

	request := DisputeResolutionListRequest{OrganisationID: organisationID, Family: fixture.frozen.Family, PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || page.Page == nil || page.Page.ReturnedCount != 1 || len(page.Resolutions) != 1 || page.Page.NextCursor == nil {
		t.Fatalf("first exact-family resolution page: page=%v err=%v", page, err)
	}
	if err := protovalidate.Validate(page.Resolutions[0]); err != nil ||
		!sameBASSourceRef(page.Resolutions[0].Resolution, firstRef) ||
		!sameBASSourceRef(page.Resolutions[0].Family, fixture.frozen.Family) ||
		page.Resolutions[0].Outcome != first.Resolution.Outcome ||
		!proto.Equal(page.Resolutions[0].ResolvedAt, first.ResolvedAt) {
		t.Fatalf("resolution summary lost exact retained fields or violates its contract: summary=%v err=%v", page.Resolutions[0], err)
	}
	cursor := *page.Page.NextCursor
	appendDisputeResolutionForRepositoryTest(t, repository, organisationID,
		fixture.frozen.Family, []*tammyv1.SourceRef{subjectRef}, 68603, 1791713200)
	page, err = repository.List(ctx, DisputeResolutionListRequest{OrganisationID: organisationID,
		Family: fixture.frozen.Family, PageSize: 1, Cursor: &cursor})
	if err != nil || page == nil || page.Page == nil || page.Page.ReturnedCount != 1 || len(page.Resolutions) != 1 ||
		!sameBASSourceRef(page.Resolutions[0].Resolution, secondRef) || page.Page.NextCursor != nil {
		t.Fatalf("cursor should retain the original family watermark across a later append: page=%v err=%v", page, err)
	}

	// Moving the mutable owner to a later review-ready version must not redirect
	// exact historical resolution reads or its family history.
	newValidationRef := appendWarningAcknowledgementValidationRevision(t, tx, organisationID, fixture.ackFixture)
	newOwnerRef := appendWarningAcknowledgementCurrentOwner(t, tx, organisationID, fixture.preparationRef, newValidationRef)
	if sameBASSourceRef(newOwnerRef, fixture.preparationRef) {
		t.Fatal("fixture did not advance its preparation owner")
	}
	got, err := repository.GetExact(ctx, organisationID, firstRef)
	want := proto.Clone(first).(*tammyv1.BasExternalDisputeResolution)
	want.ContentHash = append([]byte(nil), firstRef.ContentHash...)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("historical resolution redirected after current owner advanced: value=%v want=%v err=%v", got, want, err)
	}
	page, err = repository.List(ctx, DisputeResolutionListRequest{OrganisationID: organisationID,
		Family: fixture.frozen.Family, PageSize: 10})
	if err != nil || page == nil || len(page.Resolutions) != 3 {
		t.Fatalf("exact family history changed after owner advance: page=%v err=%v", page, err)
	}
	allPage, err := repository.List(ctx, DisputeResolutionListRequest{OrganisationID: organisationID, PageSize: 10})
	if err != nil || allPage == nil || len(allPage.Resolutions) != 3 {
		t.Fatalf("organisation-wide resolution history without a family filter: page=%v err=%v", allPage, err)
	}
}

func TestDisputeResolutionRepositoryRejectsUnboundWritesWithoutRetainedRows(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 82, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subjectRef := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		organisationID, frozenRef, 68701, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE, 1791713300)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	assertNoNewResolutionRows := func() {
		t.Helper()
		assertBASRepositoryCount(t, tx, "bas_family_events_v1", 1)
		assertBASRepositoryCount(t, tx, "bas_dispute_resolutions_v1", 0)
		assertBASRepositoryCount(t, tx, "bas_dispute_resolution_observations_v1", 0)
		assertBASRepositoryCount(t, tx, "bas_dispute_resolution_preparations_v1", 0)
	}

	wrongFamily := cloneRef(fixture.frozen.Family)
	wrongFamily.ContentHash[0] ^= 0xff
	value := disputeResolutionForRepositoryTest(fixture.frozen.Family, []*tammyv1.SourceRef{subjectRef}, 68801, 1791713400)
	if ref, err := repository.Append(ctx, organisationID, wrongFamily, testUUID(68811), testUUID(68821), value); ref != nil || err == nil {
		t.Fatalf("append with a stale family identity argument was accepted: ref=%v err=%v", ref, err)
	}
	assertNoNewResolutionRows()

	wrongOrganisation := testUUID(68831)
	if ref, err := repository.Append(ctx, wrongOrganisation, fixture.frozen.Family, testUUID(68832), testUUID(68833), value); ref != nil || err == nil {
		t.Fatalf("append under a different organisation was accepted: ref=%v err=%v", ref, err)
	}
	assertNoNewResolutionRows()

	for _, mutate := range []struct {
		name string
		edit func(*tammyv1.BasExternalDisputeResolution)
	}{
		{name: "unknown subject hash", edit: func(candidate *tammyv1.BasExternalDisputeResolution) {
			candidate.Resolution.DisputedObservations[0].ContentHash[0] ^= 0xff
		}},
		{name: "duplicate subject", edit: func(candidate *tammyv1.BasExternalDisputeResolution) {
			candidate.Resolution.DisputedObservations = append(candidate.Resolution.DisputedObservations,
				cloneRef(candidate.Resolution.DisputedObservations[0]))
		}},
		{name: "mismatched dependency manifest", edit: func(candidate *tammyv1.BasExternalDisputeResolution) {
			candidate.Resolution.DependentChainManifestHash[0] ^= 0xff
		}},
		{name: "body family hash", edit: func(candidate *tammyv1.BasExternalDisputeResolution) {
			candidate.Family.ContentHash[0] ^= 0xff
		}},
		{name: "body family type", edit: func(candidate *tammyv1.BasExternalDisputeResolution) {
			candidate.Family.Type = "bas_period"
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			candidate := disputeResolutionForRepositoryTest(fixture.frozen.Family,
				[]*tammyv1.SourceRef{subjectRef}, 68840, 1791713500)
			mutate.edit(candidate)
			if ref, err := repository.Append(ctx, organisationID, fixture.frozen.Family,
				testUUID(68841), testUUID(68842), candidate); ref != nil || err == nil {
				t.Fatalf("append with %s was accepted: ref=%v err=%v", mutate.name, ref, err)
			}
			assertNoNewResolutionRows()
		})
	}

	if _, err := repository.List(ctx, DisputeResolutionListRequest{OrganisationID: organisationID,
		Family: fixture.frozen.Family, PageSize: 201}); err == nil {
		t.Fatal("resolution list accepted a page larger than the bounded API maximum")
	}
}

func TestDisputeResolutionRepositoryConstructorRejectsMissingDependencies(t *testing.T) {
	workspace, _, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	codec := newBASRepositoryCursorCodec(t)
	if repository, err := NewDisputeResolutionRepository(nil, codec); err == nil || repository != nil {
		t.Fatalf("constructor accepted a nil SQL executor: repository=%v err=%v", repository, err)
	}
	if repository, err := NewDisputeResolutionRepository(tx, nil); err == nil || repository != nil {
		t.Fatalf("constructor accepted a nil cursor codec: repository=%v err=%v", repository, err)
	}
}

func TestDisputeResolutionRepositoryRetainsExactConfirmedDependencyLinks(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	root, rootRef := seedTypedCollectingPreparation(t, tx, organisationID)
	codec := newBASRepositoryCursorCodec(t)
	preparations, err := NewPreparationRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	confirmedRef := appendTypedConfirmedSuccessor(t, tx, preparations, ctx,
		&PreparationRetainedSnapshot{Preparation: root}, rootRef)
	confirmed, err := newPreparationReaderForTest(t, tx).GetExact(ctx, organisationID, confirmedRef)
	if err != nil || confirmed == nil || confirmed.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED || confirmed.Preparation.FrozenBas == nil {
		t.Fatalf("fixture did not create an exact confirmed frozen dependency: retained=%v err=%v", confirmed, err)
	}
	frozenRef := cloneRef(confirmed.Preparation.FrozenBas)
	_, subjectRef := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		organisationID, frozenRef, 69101, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791714000)
	value := disputeResolutionForRepositoryTest(root.Family, []*tammyv1.SourceRef{subjectRef}, 69102, 1791714100)
	value.Resolution.Outcome = tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION
	value.Resolution.AffectedConfirmedChain = []*tammyv1.SourceRef{cloneRef(confirmedRef)}
	value.Resolution.DependentChainManifestHash = dependencyRefsManifest(value.Resolution.AffectedConfirmedChain)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	ref, err := repository.Append(ctx, organisationID, root.Family, testUUID(69103), testUUID(69104), value)
	if err != nil || ref == nil {
		t.Fatalf("append corrective resolution with exact confirmed dependency: ref=%v err=%v", ref, err)
	}
	value.ContentHash = append([]byte(nil), ref.ContentHash...)
	got, err := repository.GetExact(ctx, organisationID, ref)
	if err != nil || !proto.Equal(got, value) {
		t.Fatalf("exact corrective resolution lost its dependency manifest: value=%v want=%v err=%v", got, value, err)
	}
	var preparationID string
	var preparationVersion int64
	var preparationHash []byte
	if err := tx.QueryRowContext(ctx, `SELECT preparation_id,preparation_version,preparation_hash
		FROM bas_dispute_resolution_preparations_v1 WHERE resolution_id=? AND organisation_id=? AND family_id=?`,
		ref.Id, organisationID, root.Family.Id).Scan(&preparationID, &preparationVersion, &preparationHash); err != nil {
		t.Fatalf("read normalized exact confirmed-chain link: %v", err)
	}
	if preparationID != confirmedRef.Id || uint64(preparationVersion) != confirmedRef.Revision ||
		!bytes.Equal(preparationHash, confirmedRef.ContentHash) {
		t.Fatalf("normalized confirmed-chain link changed its exact reference: got %s/%d, want %s/%d", preparationID, preparationVersion, confirmedRef.Id, confirmedRef.Revision)
	}
}

func TestDisputeResolutionRepositoryVerifiesDistinctDependencyFrozenParent(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 92, true, 0)
	subjectFrozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	dependencyFrozen := appendFrozenPageApproval(t, tx, org, fixture, 9201)
	preparations, err := NewPreparationRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	current, err := preparations.GetCurrent(ctx, org, fixture.preparationRef.Id)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := clonePreparationRetainedSnapshot(current)
	confirmed.Preparation.Version++
	confirmed.Preparation.ContentHash = nil
	confirmed.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	confirmed.Preparation.FrozenBas = cloneRef(dependencyFrozen)
	confirmedRef, err := preparations.Append(ctx, confirmed, preparationReference(current.Preparation))
	if err != nil {
		t.Fatal(err)
	}
	observations := newExternalObservationRepositoryForTest(t, tx)
	_, subject := appendExternalObservationForRepositoryTest(t, observations, org, subjectFrozen, 70001,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791714400)
	value := disputeResolutionForRepositoryTest(fixture.frozen.Family, []*tammyv1.SourceRef{subject}, 70002, 1791714500)
	value.Resolution.AffectedConfirmedChain = []*tammyv1.SourceRef{confirmedRef}
	value.Resolution.DependentChainManifestHash = dependencyRefsManifest(value.Resolution.AffectedConfirmedChain)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	exact, err := repository.Append(ctx, org, fixture.frozen.Family, testUUID(70003), testUUID(70004), value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_frozen_bas_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_frozen_bas_v1 SET frozen_proto=X'01' WHERE frozen_id=?`, dependencyFrozen.Id); err != nil {
		t.Fatal(err)
	}
	if got, err := observations.GetExact(ctx, org, subject); got == nil || err != nil {
		t.Fatalf("dependency corruption also damaged the independent subject parent: %v", err)
	}
	if got, err := repository.GetExact(ctx, org, exact); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Errorf("resolution exact read accepted a corrupt dependency frozen parent: value=%v err=%v", got, err)
	}
	value.ResolutionId = testUUID(70005)
	if got, err := repository.Append(ctx, org, fixture.frozen.Family, testUUID(70006), testUUID(70007), value); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Errorf("resolution append accepted a corrupt dependency frozen parent: ref=%v err=%v", got, err)
	}
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 2)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolutions_v1", 1)
}

func TestDisputeResolutionRepositoryExactReadRejectsCorruptNormalizedSubjectLink(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 83, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subjectRef := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		organisationID, frozenRef, 68901, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791713600)
	_, otherSubject := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		organisationID, frozenRef, 68903, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 1791713650)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	_, resolutionRef := appendDisputeResolutionForRepositoryTest(t, repository, organisationID,
		fixture.frozen.Family, []*tammyv1.SourceRef{subjectRef}, 68902, 1791713700)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_dispute_resolution_observations_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_dispute_resolution_observations_v1 SET subject_id=?,subject_hash=?,observation_id=?,observation_hash=? WHERE resolution_id=?`,
		otherSubject.Id, otherSubject.ContentHash, otherSubject.Id, otherSubject.ContentHash, resolutionRef.Id); err != nil {
		t.Fatal(err)
	}
	if got, err := repository.GetExact(ctx, organisationID, resolutionRef); got != nil || err == nil {
		t.Fatalf("exact read accepted a normalized subject link that disagrees with the immutable body: value=%v err=%v", got, err)
	}
}

func TestDisputeResolutionRepositoryCancelledAppendRollsBackOnlyItsSavepoint(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, organisationID, 84, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, tx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subjectRef := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		organisationID, frozenRef, 69001, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791713800)
	wantLegalName := writeCallerWorkBeforeRepositoryAppend(t, tx, organisationID, "dispute resolution")
	appendCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	executor := &cancelAfterRepositoryParentInsertExecutor{CommandSQLExecutor: tx,
		parentTable: "bas_family_events_v1", cancel: cancel}
	value := disputeResolutionForRepositoryTest(fixture.frozen.Family, []*tammyv1.SourceRef{subjectRef}, 69002, 1791713900)
	ref, err := newDisputeResolutionRepositoryForTest(t, executor).Append(appendCtx, organisationID,
		fixture.frozen.Family, testUUID(69003), testUUID(69004), value)
	if ref != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after Core event insert should abort the resolution append: ref=%v err=%v", ref, err)
	}
	if !executor.parentInserted {
		t.Fatal("test did not cancel after inserting the Core family event")
	}
	assertBoundedUncancelledSavepointCleanup(t, executor)
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolutions_v1", 0)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolution_observations_v1", 0)
	assertBASRepositoryCount(t, tx, "bas_dispute_resolution_preparations_v1", 0)
	assertCallerWorkCanCommit(t, workspace.Database, tx, organisationID, wantLegalName, "dispute resolution")
}

func TestDisputeResolutionRepositoryHonoursCallerRollback(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	setupTx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, setupTx, organisationID, 85, true, 0)
	frozenRef, err := newFrozenRepositoryForTest(t, setupTx).Append(ctx, organisationID, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subjectRef := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, setupTx),
		organisationID, frozenRef, 69201, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791714200)
	commitBASRepositoryTx(t, setupTx)

	appendTx := beginBASRepositoryTx(t, workspace)
	value := disputeResolutionForRepositoryTest(fixture.frozen.Family, []*tammyv1.SourceRef{subjectRef}, 69202, 1791714300)
	ref, err := newDisputeResolutionRepositoryForTest(t, appendTx).Append(ctx, organisationID,
		fixture.frozen.Family, testUUID(69203), testUUID(69204), value)
	if err != nil || ref == nil {
		t.Fatalf("append resolution in caller-owned transaction: ref=%v err=%v", ref, err)
	}
	if err := appendTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertBASRepositoryCount(t, workspace.Database, "bas_family_events_v1", 1)
	assertBASRepositoryCount(t, workspace.Database, "bas_dispute_resolutions_v1", 0)
	assertBASRepositoryCount(t, workspace.Database, "bas_dispute_resolution_observations_v1", 0)
	if got, err := newDisputeResolutionRepositoryForTest(t, workspace.Database).GetExact(ctx, organisationID, ref); got != nil || err == nil {
		t.Fatalf("caller rollback left a resolution available to exact reads: value=%v err=%v", got, err)
	}
}

func appendDisputeResolutionForRepositoryTest(t *testing.T, repository *DisputeResolutionRepository,
	organisationID string, family *tammyv1.SourceRef, subjects []*tammyv1.SourceRef, id uint64, resolvedAt int64) (*tammyv1.BasExternalDisputeResolution, *tammyv1.SourceRef) {
	t.Helper()
	value := disputeResolutionForRepositoryTest(family, subjects, id, resolvedAt)
	ref, err := repository.Append(context.Background(), organisationID, family,
		testUUID(70000+id), testUUID(71000+id), value)
	if err != nil || ref == nil {
		t.Fatalf("append test dispute resolution %d: ref=%v err=%v", id, ref, err)
	}
	return value, ref
}

func disputeResolutionForRepositoryTest(family *tammyv1.SourceRef, subjects []*tammyv1.SourceRef,
	id uint64, resolvedAt int64) *tammyv1.BasExternalDisputeResolution {
	return &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(id), Family: cloneRef(family), ActorUserId: testUUID(72),
		ResolvedAt: timestamppb.New(time.Unix(resolvedAt, 0).UTC()),
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome:                    tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations:       cloneRefs(subjects),
			AffectedConfirmedChain:     []*tammyv1.SourceRef{},
			DependentChainManifestHash: dependencyRefsManifest(nil),
			Reason:                     "Retain an exact reviewed resolution for repository history",
		},
	}
}

func newDisputeResolutionRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *DisputeResolutionRepository {
	t.Helper()
	repository, err := NewDisputeResolutionRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct dispute resolution repository: %v", err)
	}
	return repository
}
