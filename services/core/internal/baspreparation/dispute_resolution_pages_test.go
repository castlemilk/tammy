//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestDisputeResolutionRepositoryPagesPinCoreOrdinalAndFamilyFilter(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 91, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subject := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		org, frozen, 74101, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791712900)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	_, first := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
		[]*tammyv1.SourceRef{subject}, 74201, 1791713200)
	_, second := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
		[]*tammyv1.SourceRef{subject}, 74202, 1791713100)
	_, third := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
		[]*tammyv1.SourceRef{subject}, 74203, 1791713000)
	_, fourth := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
		[]*tammyv1.SourceRef{subject}, 74204, 1791712800)
	otherFamily := appendOtherDisputePageFamily(t, tx, org, 74301)
	commitBASRepositoryTx(t, tx)

	// Gaps in the durable key and VACUUM must not change append order or cursor
	// anchors. A later append with an earlier claimed time stays beyond the pin.
	if _, err := workspace.Database.ExecContext(ctx, `DROP TRIGGER bas_family_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, `UPDATE bas_family_events_v1 SET event_ordinal=event_ordinal*10 WHERE organisation_id=?`, org); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, `CREATE TRIGGER bas_family_events_v1_no_update BEFORE UPDATE ON bas_family_events_v1 BEGIN SELECT RAISE(ABORT,'BAS family event is immutable'); END`); err != nil {
		t.Fatal(err)
	}
	repository = newDisputeResolutionRepositoryForTest(t, workspace.Database)
	request := DisputeResolutionListRequest{OrganisationID: org, Family: fixture.frozen.Family, PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || page.Page == nil || page.Page.ReturnedCount != 1 || len(page.Resolutions) != 1 || page.Page.NextCursor == nil ||
		!sameBASSourceRef(page.Resolutions[0].Resolution, first) {
		t.Fatalf("first append-ordered family page: page=%v err=%v", page, err)
	}
	if err := protovalidate.Validate(page.Resolutions[0]); err != nil ||
		!sameBASSourceRef(page.Resolutions[0].Family, fixture.frozen.Family) ||
		page.Resolutions[0].Outcome != tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN {
		t.Fatalf("resolution summary is incomplete or invalid: summary=%v err=%v", page.Resolutions[0], err)
	}
	cursor := *page.Page.NextCursor
	for _, kind := range []string{"organisation", "family_hash", "other_family", "unfiltered", "tamper", "size"} {
		bad := request
		bad.Family = cloneRef(request.Family)
		bad.Cursor = &cursor
		switch kind {
		case "organisation":
			bad.OrganisationID = otherOrg
		case "family_hash":
			bad.Family.ContentHash[0] ^= 0xff
		case "other_family":
			bad.Family = otherFamily
		case "unfiltered":
			bad.Family = nil
		case "tamper":
			token := cursor + "!"
			bad.Cursor = &token
		case "size":
			bad.PageSize = 201
		}
		if got, err := repository.List(ctx, bad); got != nil || err == nil {
			t.Fatalf("accepted %s cursor: page=%v err=%v", kind, got, err)
		}
	}

	// An exact family filter still advances through organization-wide batches.
	// The unrelated rows produce zero-result pages with valid continuations.
	emptyRequest := DisputeResolutionListRequest{OrganisationID: org, Family: otherFamily, PageSize: 1}
	empty, err := repository.List(ctx, emptyRequest)
	if err != nil || empty == nil || empty.Page == nil || len(empty.Resolutions) != 0 || empty.Page.ReturnedCount != 0 || empty.Page.NextCursor == nil {
		t.Fatalf("unmatched family batch must retain continuation: page=%v err=%v", empty, err)
	}
	emptyPages := 1
	for empty.Page.NextCursor != nil {
		if emptyPages >= 6 {
			t.Fatal("unmatched family cursor did not terminate")
		}
		emptyRequest.Cursor = empty.Page.NextCursor
		empty, err = repository.List(ctx, emptyRequest)
		if err != nil || empty == nil || empty.Page == nil || len(empty.Resolutions) != 0 || empty.Page.ReturnedCount != 0 {
			t.Fatalf("unmatched family continuation: page=%v err=%v", empty, err)
		}
		emptyPages++
	}
	if emptyPages < 2 {
		t.Fatalf("family filter did not scan unrelated Core events: pages=%d", emptyPages)
	}

	lateTx := beginBASRepositoryTx(t, workspace)
	lateRepository := newDisputeResolutionRepositoryForTest(t, lateTx)
	_, late := appendDisputeResolutionForRepositoryTest(t, lateRepository, org, fixture.frozen.Family,
		[]*tammyv1.SourceRef{subject}, 74205, 1791700000)
	commitBASRepositoryTx(t, lateTx)
	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	repository = newDisputeResolutionRepositoryForTest(t, reopened)
	wantPinned := []*tammyv1.SourceRef{second, third, fourth}
	for index, want := range wantPinned {
		request.Cursor = &cursor
		page, err = repository.List(ctx, request)
		if err != nil || page == nil || page.Page == nil || len(page.Resolutions) != 1 || page.Page.ReturnedCount != 1 ||
			!sameBASSourceRef(page.Resolutions[0].Resolution, want) {
			t.Fatalf("pinned resolution page %d after VACUUM/reopen: page=%v err=%v", index+2, page, err)
		}
		if index == len(wantPinned)-1 && page.Page.NextCursor != nil {
			t.Fatalf("last pinned page retained an unnecessary cursor: page=%v", page)
		}
		if index < len(wantPinned)-1 && page.Page.NextCursor == nil {
			t.Fatalf("pinned history ended before page %d", index+3)
		}
		if page.Page.NextCursor != nil {
			cursor = *page.Page.NextCursor
		}
	}
	if late == nil {
		t.Fatal("late resolution fixture was not retained")
	}
	fresh, err := repository.List(ctx, DisputeResolutionListRequest{OrganisationID: org, Family: fixture.frozen.Family, PageSize: 10})
	if err != nil || fresh == nil || len(fresh.Resolutions) != 5 ||
		!sameBASSourceRef(fresh.Resolutions[0].Resolution, first) ||
		!sameBASSourceRef(fresh.Resolutions[4].Resolution, late) ||
		fresh.Resolutions[4].ResolvedAt.Seconds >= fresh.Resolutions[0].ResolvedAt.Seconds {
		t.Fatalf("fresh page must append a backdated resolution after existing history: page=%v err=%v", fresh, err)
	}
	all, err := repository.List(ctx, DisputeResolutionListRequest{OrganisationID: org, PageSize: 10})
	if err != nil || all == nil || len(all.Resolutions) != 5 {
		t.Fatalf("unfiltered organization history: page=%v err=%v", all, err)
	}
}

func TestDisputeResolutionRepositoryPagesRejectCorruptEndpointAnchorReturnedAndLookahead(t *testing.T) {
	for _, kind := range []string{"snapshot_endpoint", "anchor", "returned", "lookahead"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedFrozenRepositoryFixture(t, tx, org, 92, true, 0)
			frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
			if err != nil {
				t.Fatal(err)
			}
			_, subject := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
				org, frozen, 74401, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791714000)
			repository := newDisputeResolutionRepositoryForTest(t, tx)
			_, first := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
				[]*tammyv1.SourceRef{subject}, 74501, 1791714100)
			_, second := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
				[]*tammyv1.SourceRef{subject}, 74502, 1791714200)
			_, third := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
				[]*tammyv1.SourceRef{subject}, 74503, 1791714300)
			request := DisputeResolutionListRequest{OrganisationID: org, Family: fixture.frozen.Family, PageSize: 1}
			var cursor *string
			id := second.Id
			if kind == "snapshot_endpoint" {
				id = third.Id
			}
			if kind != "lookahead" {
				page, err := repository.List(ctx, request)
				if err != nil || page == nil || page.Page.NextCursor == nil {
					t.Fatalf("initial page: page=%v err=%v", page, err)
				}
				cursor = page.Page.NextCursor
				request.Cursor = cursor
			}
			if kind == "anchor" {
				id = first.Id
			}
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_family_events_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE bas_family_events_v1 SET event_proto=X'01'
				WHERE event_id=(SELECT event_id FROM bas_dispute_resolutions_v1 WHERE resolution_id=?)`, id); err != nil {
				t.Fatal(err)
			}
			if got, err := repository.List(ctx, request); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("accepted corrupt %s: page=%v err=%v", kind, got, err)
			}
		})
	}
}

func TestDisputeResolutionRepositoryListDoesNotHideCoreEventsWithoutChildren(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 93, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subject := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		org, frozen, 74601, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791714400)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	_, original := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
		[]*tammyv1.SourceRef{subject}, 74701, 1791714500)
	if _, err := tx.ExecContext(ctx, `INSERT INTO bas_family_events_v1(
		event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at)
		SELECT ?,organisation_id,family_id,event_kind,?,claimed_at,event_proto,body_hash,created_at
		FROM bas_family_events_v1 WHERE event_id=(SELECT event_id FROM bas_dispute_resolutions_v1 WHERE resolution_id=?)`,
		testUUID(74801), testUUID(74802), original.Id); err != nil {
		t.Fatalf("insert orphan Core resolution event: %v", err)
	}
	if page, err := repository.List(ctx, DisputeResolutionListRequest{OrganisationID: org,
		Family: fixture.frozen.Family, PageSize: 1}); page != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Fatalf("Core event without a normalized child was silently omitted: page=%v err=%v", page, err)
	}
}

func TestDisputeResolutionRepositoryFamilyFilterDetectsNormalizedFamilyRoutingCorruption(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 94, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	_, subject := appendExternalObservationForRepositoryTest(t, newExternalObservationRepositoryForTest(t, tx),
		org, frozen, 74901, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, 1791714600)
	repository := newDisputeResolutionRepositoryForTest(t, tx)
	_, resolution := appendDisputeResolutionForRepositoryTest(t, repository, org, fixture.frozen.Family,
		[]*tammyv1.SourceRef{subject}, 75001, 1791714700)
	otherFamily := appendOtherDisputePageFamily(t, tx, org, 75002)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_dispute_resolutions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_dispute_resolutions_v1 SET family_id=? WHERE resolution_id=?`, otherFamily.Id, resolution.Id); err != nil {
		t.Fatalf("corrupt normalized family route: %v", err)
	}
	if page, err := repository.List(ctx, DisputeResolutionListRequest{OrganisationID: org,
		Family: fixture.frozen.Family, PageSize: 1}); page != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Fatalf("family-filtered history silently omitted a body-pinned resolution: page=%v err=%v", page, err)
	}
}

func appendOtherDisputePageFamily(t *testing.T, executor app.CommandSQLExecutor, org string, id uint64) *tammyv1.SourceRef {
	t.Helper()
	family := validBASFamilyIdentity(org, testUUID(id))
	family.PeriodScope.StartDate = testCivilDate(2026, 10, 1)
	family.PeriodScope.EndDate = testCivilDate(2026, 12, 31)
	family.Window = tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC
	ref, err := newFamilyIdentityRepositoryForTest(t, executor).Append(context.Background(), family)
	if err != nil || ref == nil {
		t.Fatalf("append second family identity: ref=%v err=%v", ref, err)
	}
	return ref
}
