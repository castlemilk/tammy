//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestPeriodRepositoryReadsExactHistoryIndependentlyFromCurrentHead(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	profileID, periodID := testUUID(9301), testUUID(9302)

	tx := beginBASRepositoryTx(t, workspace)
	profileRepository := newProfileRepositoryForTest(t, tx, codec)
	profileRef := appendBASProfile(t, profileRepository, validBASProfile(organisationID, profileID, 1), nil)
	periodRepository := newPeriodRepositoryForTest(t, tx, codec)
	first := validBASPeriod(organisationID, periodID, profileRef, 1,
		testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	firstRef := appendBASPeriod(t, periodRepository, first, nil)
	commitBASRepositoryTx(t, tx)

	second := nextBASPeriod(first, 2)
	tx = beginBASRepositoryTx(t, workspace)
	periodRepository = newPeriodRepositoryForTest(t, tx, codec)
	secondRef := appendBASPeriod(t, periodRepository, second, firstRef)
	commitBASRepositoryTx(t, tx)

	tx = beginBASRepositoryTx(t, workspace)
	periodRepository = newPeriodRepositoryForTest(t, tx, codec)
	old, err := periodRepository.GetExact(ctx, organisationID, firstRef)
	if err != nil || old == nil || old.Version != 1 || old.Confirmation.Reason != first.Confirmation.Reason {
		t.Fatalf("exact period version 1 = %#v, %v", old, err)
	}
	current, err := periodRepository.GetCurrent(ctx, organisationID, periodID)
	if err != nil || current == nil || current.Version != 2 || !bytes.Equal(current.ContentHash, secondRef.ContentHash) {
		t.Fatalf("current period = %#v, %v", current, err)
	}
	commitBASRepositoryTx(t, tx)

	for _, testCase := range []struct {
		name string
		org  string
		edit func(*tammyv1.SourceRef)
	}{
		{name: "other organisation", org: otherOrganisationID},
		{name: "other version", edit: func(ref *tammyv1.SourceRef) { ref.Revision++ }},
		{name: "other hash", edit: func(ref *tammyv1.SourceRef) { ref.ContentHash[0] ^= 0xff }},
		{name: "other resource", edit: func(ref *tammyv1.SourceRef) { ref.Type = "bas_obligations_profile" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ref := proto.Clone(firstRef).(*tammyv1.SourceRef)
			if testCase.edit != nil {
				testCase.edit(ref)
			}
			org := organisationID
			if testCase.org != "" {
				org = testCase.org
			}
			readTx := beginBASRepositoryTx(t, workspace)
			readRepository := newPeriodRepositoryForTest(t, readTx, codec)
			value, err := readRepository.GetExact(ctx, org, ref)
			if err == nil || value != nil {
				t.Fatalf("mismatched exact period reference returned value=%#v err=%v", value, err)
			}
			commitBASRepositoryTx(t, readTx)
		})
	}
}

func TestPeriodRepositoryFailsClosedOnMalformedOrNoncanonicalRetainedBytes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "malformed protobuf", mutate: func([]byte) []byte { return []byte{0xff} }},
		{name: "duplicate scalar encoding", mutate: func(wire []byte) []byte {
			wire = append(wire, protowire.AppendTag(nil, 3, protowire.VarintType)...)
			return protowire.AppendVarint(wire, 1)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			ctx := context.Background()
			codec := newBASRepositoryCursorCodec(t)
			tx := beginBASRepositoryTx(t, workspace)
			profileRepository := newProfileRepositoryForTest(t, tx, codec)
			profileRef := appendBASProfile(t, profileRepository, validBASProfile(organisationID, testUUID(9310), 1), nil)
			periodRepository := newPeriodRepositoryForTest(t, tx, codec)
			period := validBASPeriod(organisationID, testUUID(9311), profileRef, 1,
				testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
			ref := appendBASPeriod(t, periodRepository, period, nil)
			commitBASRepositoryTx(t, tx)

			wire, _, err := sealPeriod(period)
			if err != nil {
				t.Fatal(err)
			}
			tampered := testCase.mutate(bytes.Clone(wire))
			corruptTx := beginBASRepositoryTx(t, workspace)
			if _, err := corruptTx.ExecContext(ctx, `DROP TRIGGER bas_period_versions_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := corruptTx.ExecContext(ctx, `UPDATE bas_period_versions_v1 SET period_proto=? WHERE period_id=? AND version=1`,
				tampered, period.PeriodId); err != nil {
				t.Fatal(err)
			}
			commitBASRepositoryTx(t, corruptTx)

			readTx := beginBASRepositoryTx(t, workspace)
			readRepository := newPeriodRepositoryForTest(t, readTx, codec)
			value, err := readRepository.GetExact(ctx, organisationID, ref)
			if err == nil || value != nil {
				t.Fatalf("corrupt immutable period history returned value=%#v err=%v", value, err)
			}
			commitBASRepositoryTx(t, readTx)
		})
	}
}

func TestPeriodRepositoryRejectsDuplicateNaturalScopeAndStaleAppend(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)

	tx := beginBASRepositoryTx(t, workspace)
	profileRepository := newProfileRepositoryForTest(t, tx, codec)
	profileRef := appendBASProfile(t, profileRepository, validBASProfile(organisationID, testUUID(9320), 1), nil)
	periodRepository := newPeriodRepositoryForTest(t, tx, codec)
	first := validBASPeriod(organisationID, testUUID(9321), profileRef, 1,
		testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	firstRef := appendBASPeriod(t, periodRepository, first, nil)
	commitBASRepositoryTx(t, tx)

	second := nextBASPeriod(first, 2)
	tx = beginBASRepositoryTx(t, workspace)
	periodRepository = newPeriodRepositoryForTest(t, tx, codec)
	secondRef := appendBASPeriod(t, periodRepository, second, firstRef)
	commitBASRepositoryTx(t, tx)

	duplicateScope := validBASPeriod(organisationID, testUUID(9322), profileRef, 1,
		testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	staleVersion := nextBASPeriod(first, 3)
	for _, testCase := range []struct {
		name     string
		period   *tammyv1.BasConfirmedPeriod
		expected *tammyv1.SourceRef
	}{
		{name: "duplicate organisation and actual period scope", period: duplicateScope},
		{name: "stale expected current reference", period: staleVersion, expected: firstRef},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			appendTx := beginBASRepositoryTx(t, workspace)
			appendRepository := newPeriodRepositoryForTest(t, appendTx, codec)
			if ref, err := appendRepository.Append(ctx, testCase.period, testCase.expected); err == nil || ref != nil {
				t.Fatalf("conflicting period append returned ref=%#v err=%v", ref, err)
			}
			if err := appendTx.Rollback(); err != nil {
				t.Fatal(err)
			}

			readTx := beginBASRepositoryTx(t, workspace)
			readRepository := newPeriodRepositoryForTest(t, readTx, codec)
			current, err := readRepository.GetCurrent(ctx, organisationID, first.PeriodId)
			if err != nil || current == nil || current.Version != 2 || !bytes.Equal(current.ContentHash, secondRef.ContentHash) {
				t.Fatalf("period head after caller rollback = %#v, %v", current, err)
			}
			commitBASRepositoryTx(t, readTx)
		})
	}
}

func TestPeriodRepositoryListUsesStableFilteredSnapshotAndLatestVersion(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	fullYear := &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 1, 1), EndDate: testCivilDate(2026, 12, 31)}

	tx := beginBASRepositoryTx(t, workspace)
	profileRepository := newProfileRepositoryForTest(t, tx, codec)
	profileRef := appendBASProfile(t, profileRepository, validBASProfile(organisationID, testUUID(9330), 1), nil)
	periodRepository := newPeriodRepositoryForTest(t, tx, codec)
	quarters := []struct {
		id     string
		start  *tammyv1.CivilDate
		end    *tammyv1.CivilDate
		window tammyv1.BasPeriodWindow
	}{
		{testUUID(9331), testCivilDate(2026, 1, 1), testCivilDate(2026, 3, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR},
		{testUUID(9332), testCivilDate(2026, 4, 1), testCivilDate(2026, 6, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN},
		{testUUID(9333), testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP},
		{testUUID(9334), testCivilDate(2026, 10, 1), testCivilDate(2026, 12, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC},
	}
	var octoberRef *tammyv1.SourceRef
	var octoberPeriod *tammyv1.BasConfirmedPeriod
	for index, quarter := range quarters {
		period := validBASPeriod(organisationID, quarter.id, profileRef, 1, quarter.start, quarter.end, quarter.window)
		ref := appendBASPeriod(t, periodRepository, period, nil)
		if index == 3 {
			octoberRef, octoberPeriod = ref, period
		}
	}
	commitBASRepositoryTx(t, tx)

	pageOneTx := beginBASRepositoryTx(t, workspace)
	pageOneRepository := newPeriodRepositoryForTest(t, pageOneTx, codec)
	pageOne, err := pageOneRepository.List(ctx, PeriodListRequest{OrganisationID: organisationID, Range: fullYear, PageSize: 2})
	if err != nil || pageOne == nil || pageOne.Page == nil {
		t.Fatalf("first period page = %#v, %v", pageOne, err)
	}
	if got := pageOne.Page.ReturnedCount; got != uint32(len(pageOne.Periods)) || got != 2 || pageOne.Page.NextCursor == nil {
		t.Fatalf("first period page count/cursor = %#v rows=%d", pageOne.Page, len(pageOne.Periods))
	}
	cursor := *pageOne.Page.NextCursor
	commitBASRepositoryTx(t, pageOneTx)

	// Revisions and a new in-range scope arrive after the page watermark.
	writeTx := beginBASRepositoryTx(t, workspace)
	writeRepository := newPeriodRepositoryForTest(t, writeTx, codec)
	appendBASPeriod(t, writeRepository, nextBASPeriod(octoberPeriod, 2), octoberRef)
	november := validBASPeriod(organisationID, testUUID(9335), profileRef, 1,
		testCivilDate(2026, 11, 1), testCivilDate(2026, 11, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_MONTHLY)
	appendBASPeriod(t, writeRepository, november, nil)
	commitBASRepositoryTx(t, writeTx)

	newCodec := newBASRepositoryCursorCodec(t)
	pageTwoTx := beginBASRepositoryTx(t, workspace)
	pageTwoRepository := newPeriodRepositoryForTest(t, pageTwoTx, newCodec)
	pageTwo, err := pageTwoRepository.List(ctx, PeriodListRequest{OrganisationID: organisationID, Range: fullYear, PageSize: 2, Cursor: &cursor})
	if err != nil || pageTwo == nil || pageTwo.Page == nil {
		t.Fatalf("second period page after restart = %#v, %v", pageTwo, err)
	}
	if got := pageTwo.Page.ReturnedCount; got != uint32(len(pageTwo.Periods)) || got != 2 || pageTwo.Page.NextCursor != nil {
		t.Fatalf("second period page count/cursor = %#v rows=%d", pageTwo.Page, len(pageTwo.Periods))
	}
	byID := make(map[string]uint64, len(pageTwo.Periods))
	for _, summary := range pageTwo.Periods {
		byID[summary.Period.Id] = summary.Period.Revision
	}
	if byID[testUUID(9333)] != 1 || byID[testUUID(9334)] != 1 || len(byID) != 2 {
		t.Fatalf("second page must preserve the old period versions and exclude new in-range scope: %#v", byID)
	}
	commitBASRepositoryTx(t, pageTwoTx)

	narrowRange := &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 12, 31)}
	for _, testCase := range []struct {
		name string
		org  string
		rng  *tammyv1.BasCivilDateRange
		edit func(*string)
	}{
		{name: "different date filter", org: organisationID, rng: narrowRange},
		{name: "different organisation", org: otherOrganisationID, rng: fullYear},
		{name: "tampered HMAC token", org: organisationID, rng: fullYear, edit: func(token *string) {
			position := len(*token) / 2
			changed := []byte(*token)
			if changed[position] == 'A' {
				changed[position] = 'B'
			} else {
				changed[position] = 'A'
			}
			*token = string(changed)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			badCursor := cursor
			if testCase.edit != nil {
				testCase.edit(&badCursor)
			}
			badTx := beginBASRepositoryTx(t, workspace)
			badRepository := newPeriodRepositoryForTest(t, badTx, newCodec)
			page, err := badRepository.List(ctx, PeriodListRequest{OrganisationID: testCase.org, Range: testCase.rng, PageSize: 2, Cursor: &badCursor})
			if err == nil || page != nil {
				t.Fatalf("invalid date scope/organisation/token returned %#v, %v", page, err)
			}
			commitBASRepositoryTx(t, badTx)
		})
	}
}

func newPeriodRepositoryForTest(t *testing.T, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, codec *paging.Codec) *PeriodRepository {
	t.Helper()
	repository, err := NewPeriodRepository(executor, codec)
	if err != nil {
		t.Fatalf("construct period repository: %v", err)
	}
	return repository
}

func validBASPeriod(organisationID, periodID string, profile *tammyv1.SourceRef, version uint64,
	start, end *tammyv1.CivilDate, window tammyv1.BasPeriodWindow) *tammyv1.BasConfirmedPeriod {
	period := confirmedPeriod(periodID, organisationID, version, byte(version+31))
	period.ContentHash = nil
	period.Confirmation.Profile = proto.Clone(profile).(*tammyv1.SourceRef)
	period.Confirmation.Period = &tammyv1.BasCivilDateRange{StartDate: proto.Clone(start).(*tammyv1.CivilDate), EndDate: proto.Clone(end).(*tammyv1.CivilDate)}
	period.Confirmation.Window = window
	return period
}

func nextBASPeriod(previous *tammyv1.BasConfirmedPeriod, version uint64) *tammyv1.BasConfirmedPeriod {
	period := proto.Clone(previous).(*tammyv1.BasConfirmedPeriod)
	period.Version = version
	period.ContentHash = nil
	period.Confirmation.Reason = "Updated confirmed period snapshot"
	return period
}

func appendBASPeriod(t *testing.T, repository *PeriodRepository, period *tammyv1.BasConfirmedPeriod, expected *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	ref, err := repository.Append(context.Background(), period, expected)
	if err != nil {
		t.Fatalf("append BAS period version %d: %v", period.Version, err)
	}
	if ref == nil || ref.Type != "bas_period" || ref.Id != period.PeriodId || ref.Revision != period.Version || len(ref.ContentHash) != 32 {
		t.Fatalf("period append reference = %#v", ref)
	}
	return ref
}
