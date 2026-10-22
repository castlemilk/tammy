//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestIncomeYearPinRepositoryPinsAfterFirstStatementAndRejectsLaterChanges(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspaceAtVersion15(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	const year2026 = "01800000-0000-7000-8000-000000009551"
	const year2027 = "01800000-0000-7000-8000-000000009552"

	tx := beginBASRepositoryTx(t, workspace)
	profiles := newProfileRepositoryForTest(t, tx, codec)
	firstProfile := pinProfileForTest(organisationID, testUUID(9553), 1, year2026,
		testCivilDate(2026, 7, 1), testCivilDate(2027, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT,
		testCivilDate(2026, 1, 1))
	firstProfileRef := appendBASProfile(t, profiles, firstProfile, nil)
	periods := newPeriodRepositoryForTest(t, tx, codec)
	firstPeriod := validBASPeriod(organisationID, testUUID(9554), firstProfileRef, 1,
		testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	firstPeriodRef := appendBASPeriod(t, periods, firstPeriod, nil)
	pins, err := NewIncomeYearPinRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	if err := pins.PinAfterConfirmedPeriod(ctx, organisationID, firstPeriodRef); err != nil {
		t.Fatalf("first confirmed period pins selected method: %v", err)
	}
	commitBASRepositoryTx(t, tx)

	// The exact retained period is not visible through another organization ID.
	tx = beginBASRepositoryTx(t, workspace)
	pins, _ = NewIncomeYearPinRepository(tx, codec)
	if err := pins.PinAfterConfirmedPeriod(ctx, otherOrganisationID, firstPeriodRef); err == nil {
		t.Fatal("another organization resolved the first organization's confirmed period")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// A second statement in the same exact year/method is idempotent: it does
	// not append another pin and it rechecks the first period/profile proof.
	tx = beginBASRepositoryTx(t, workspace)
	periods = newPeriodRepositoryForTest(t, tx, codec)
	secondPeriod := validBASPeriod(organisationID, testUUID(9555), firstProfileRef, 1,
		testCivilDate(2026, 10, 1), testCivilDate(2026, 12, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC)
	secondPeriodRef := appendBASPeriod(t, periods, secondPeriod, nil)
	pins, _ = NewIncomeYearPinRepository(tx, codec)
	if err := pins.PinAfterConfirmedPeriod(ctx, organisationID, secondPeriodRef); err != nil {
		t.Fatalf("same year/method was rejected: %v", err)
	}
	commitBASRepositoryTx(t, tx)

	// The profile may be corrected before the first statement, but after pinning
	// neither a method change nor a different overlapping year identity is accepted.
	rateProfile := nextBASProfile(firstProfile, firstProfileRef, 2)
	rateProfile.Profile.Paygi.Method = tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE
	tx = beginBASRepositoryTx(t, workspace)
	pins, _ = NewIncomeYearPinRepository(tx, codec)
	if err := pins.CheckProfileCompatible(ctx, organisationID, rateProfile.Profile); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("conflicting method profile accepted after pin: %v", err)
	}
	overlappingYear := proto.Clone(firstProfile).(*tammyv1.BasObligationsProfile)
	overlappingYear.Profile.Paygi.ConfirmedIncomeYear.IncomeYearId = testUUID(9556)
	overlappingYear.Profile.Paygi.ConfirmedIncomeYear.Dates = &tammyv1.BasCivilDateRange{
		StartDate: testCivilDate(2026, 10, 1), EndDate: testCivilDate(2027, 9, 30)}
	if err := pins.CheckProfileCompatible(ctx, organisationID, overlappingYear.Profile); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("overlapping alternate year identity accepted: %v", err)
	}
	edgeOverlap := proto.Clone(firstProfile).(*tammyv1.BasObligationsProfile)
	edgeOverlap.Profile.EffectiveInterval.StartDate = testCivilDate(2027, 7, 1)
	edgeOverlap.Profile.Paygi.ConfirmedIncomeYear.IncomeYearId = testUUID(9567)
	edgeOverlap.Profile.Paygi.ConfirmedIncomeYear.Dates = &tammyv1.BasCivilDateRange{
		StartDate: testCivilDate(2027, 6, 30), EndDate: testCivilDate(2028, 6, 29)}
	if err := pins.CheckProfileCompatible(ctx, organisationID, edgeOverlap.Profile); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("different year overlapping only outside the profile interval was accepted: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// Defensive repository enforcement still rejects a period appended by a
	// stale caller that bypasses Set's compatibility check. Caller rollback
	// removes that period and the conflicting profile version.
	tx = beginBASRepositoryTx(t, workspace)
	profiles = newProfileRepositoryForTest(t, tx, codec)
	rateProfileRef := appendBASProfile(t, profiles, rateProfile, firstProfileRef)
	periods = newPeriodRepositoryForTest(t, tx, codec)
	conflictingPeriod := validBASPeriod(organisationID, testUUID(9557), rateProfileRef, 1,
		testCivilDate(2027, 1, 1), testCivilDate(2027, 3, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR)
	conflictingRef := appendBASPeriod(t, periods, conflictingPeriod, nil)
	pins, _ = NewIncomeYearPinRepository(tx, codec)
	if err := pins.PinAfterConfirmedPeriod(ctx, organisationID, conflictingRef); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("conflicting confirmed statement method accepted: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// A new nonoverlapping year may select a different method. The profile,
	// period and second pin are all written on this caller-owned transaction.
	tx = beginBASRepositoryTx(t, workspace)
	profiles = newProfileRepositoryForTest(t, tx, codec)
	nextProfile := nextBASProfile(firstProfile, firstProfileRef, 2)
	nextProfile.Profile.EffectiveInterval = &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2027, 7, 1)}
	nextProfile.Profile.Paygi.Method = tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE
	nextProfile.Profile.Paygi.ConfirmedIncomeYear = &tammyv1.BasIncomeYearIdentity{
		IncomeYearId: year2027,
		Dates:        &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2027, 7, 1), EndDate: testCivilDate(2028, 6, 30)},
		Confirmation: &tammyv1.SourceRef{Type: "ato_income_year", Id: testUUID(9558), Revision: 1, ContentHash: testHash(58)},
	}
	nextProfileRef := appendBASProfile(t, profiles, nextProfile, firstProfileRef)
	periods = newPeriodRepositoryForTest(t, tx, codec)
	nextYearPeriod := validBASPeriod(organisationID, testUUID(9559), nextProfileRef, 1,
		testCivilDate(2027, 7, 1), testCivilDate(2027, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	nextYearRef := appendBASPeriod(t, periods, nextYearPeriod, nil)
	pins, _ = NewIncomeYearPinRepository(tx, codec)
	if err := pins.PinAfterConfirmedPeriod(ctx, organisationID, nextYearRef); err != nil {
		t.Fatalf("different nonoverlapping year/method was rejected: %v", err)
	}
	commitBASRepositoryTx(t, tx)

	var count int64
	if err := workspace.Database.QueryRowContext(ctx, `SELECT count(*) FROM bas_income_year_pins_v1 WHERE organisation_id=?`, organisationID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("retained income-year pins=%d, err=%v; want first statement and next year only", count, err)
	}
	var periodCount, profileCount int64
	if err := workspace.Database.QueryRowContext(ctx, `SELECT count(*) FROM bas_period_versions_v1 WHERE organisation_id=?`, organisationID).Scan(&periodCount); err != nil || periodCount != 3 {
		t.Fatalf("periods after failed caller rollback=%d, err=%v; want two accepted periods plus next year", periodCount, err)
	}
	if err := workspace.Database.QueryRowContext(ctx, `SELECT count(*) FROM bas_profile_versions_v1 WHERE organisation_id=?`, organisationID).Scan(&profileCount); err != nil || profileCount != 2 {
		t.Fatalf("profiles after failed caller rollback=%d, err=%v; want one profile per accepted year", profileCount, err)
	}

	// The same external income-year ID/date range is independent in another org.
	tx = beginBASRepositoryTx(t, workspace)
	profiles = newProfileRepositoryForTest(t, tx, codec)
	otherProfile := pinProfileForTest(otherOrganisationID, testUUID(9564), 1, year2026,
		testCivilDate(2026, 7, 1), testCivilDate(2027, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE,
		testCivilDate(2026, 1, 1))
	otherProfileRef := appendBASProfile(t, profiles, otherProfile, nil)
	periods = newPeriodRepositoryForTest(t, tx, codec)
	otherPeriod := validBASPeriod(otherOrganisationID, testUUID(9565), otherProfileRef, 1,
		testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	otherPeriodRef := appendBASPeriod(t, periods, otherPeriod, nil)
	pins, _ = NewIncomeYearPinRepository(tx, codec)
	if err := pins.PinAfterConfirmedPeriod(ctx, otherOrganisationID, otherPeriodRef); err != nil {
		t.Fatalf("another organization could not independently pin its income year: %v", err)
	}
	commitBASRepositoryTx(t, tx)
	var otherCount int64
	if err := workspace.Database.QueryRowContext(ctx, `SELECT count(*) FROM bas_income_year_pins_v1 WHERE organisation_id=?`, otherOrganisationID).Scan(&otherCount); err != nil || otherCount != 1 {
		t.Fatalf("other organization pins=%d err=%v; want isolated pin", otherCount, err)
	}
	for _, column := range []string{"profile", "pinned_at"} {
		t.Run("first-proof-"+column, func(t *testing.T) {
			proofTx := beginBASRepositoryTx(t, workspace)
			defer proofTx.Rollback()
			if _, err := proofTx.ExecContext(ctx, `DROP TRIGGER bas_income_year_pins_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if column == "profile" {
				_, err = proofTx.ExecContext(ctx, `UPDATE bas_income_year_pins_v1 SET first_profile_id=?,first_profile_version=?,first_profile_hash=? WHERE organisation_id=? AND income_year_id=?`,
					nextProfileRef.Id, nextProfileRef.Revision, nextProfileRef.ContentHash, organisationID, year2026)
			} else {
				_, err = proofTx.ExecContext(ctx, `UPDATE bas_income_year_pins_v1 SET pinned_at='2026-10-23T00:00:00Z' WHERE organisation_id=? AND income_year_id=?`, organisationID, year2026)
			}
			if err != nil {
				t.Fatal(err)
			}
			proofPins, err := NewIncomeYearPinRepository(proofTx, codec)
			if err != nil {
				t.Fatal(err)
			}
			choice := proto.Clone(firstProfile.Profile).(*tammyv1.BasObligationsProfileInput)
			choice.EffectiveInterval.EndDate = testCivilDate(2027, 6, 30)
			if err := proofPins.CheckProfileCompatible(ctx, organisationID, choice); !errors.Is(err, ErrIncomeYearPinRepository) {
				t.Fatalf("changed normalized %s was detached from its first statement proof: %v", column, err)
			}
		})
	}

	// Immutability and overlap triggers defend the normalized rows against SQL
	// paths that bypass this typed repository.
	if _, err := workspace.Database.ExecContext(ctx, `UPDATE bas_income_year_pins_v1 SET method=3 WHERE organisation_id=? AND income_year_id=?`, organisationID, year2026); err == nil {
		t.Fatal("direct pin update bypassed append-only trigger")
	}
	if _, err := workspace.Database.ExecContext(ctx, `DELETE FROM bas_income_year_pins_v1 WHERE organisation_id=? AND income_year_id=?`, organisationID, year2026); err == nil {
		t.Fatal("direct pin delete bypassed retention trigger")
	}
	if _, err := workspace.Database.ExecContext(ctx, `INSERT INTO bas_income_year_pins_v1(
		organisation_id,income_year_id,income_year_start,income_year_end,method,
		first_period_id,first_period_version,first_period_hash,first_profile_id,first_profile_version,first_profile_hash,pinned_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, organisationID, testUUID(9563), "2026-10-01", "2027-09-30", int64(tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT),
		firstPeriodRef.Id, int64(firstPeriodRef.Revision), firstPeriodRef.ContentHash,
		firstProfileRef.Id, int64(firstProfileRef.Revision), firstProfileRef.ContentHash, "2026-10-22T00:00:00Z"); err == nil {
		t.Fatal("direct overlapping year identity bypassed trigger")
	}
}

func TestIncomeYearPinRepositoryPreservesCancellation(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspaceAtVersion15(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	repository, err := NewIncomeYearPinRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repository.CheckProfileCompatible(ctx, org, validBASProfile(org, testUUID(9568), 1).Profile); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled profile method check lost cancellation: %v", err)
	}
	if err := repository.PinAfterConfirmedPeriod(ctx, org, &tammyv1.SourceRef{Type: "bas_period", Id: testUUID(9569), Revision: 1, ContentHash: testHash(69)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pin lost cancellation: %v", err)
	}
}

type incomeYearFailureExecutor struct {
	app.CommandSQLExecutor
	queryMatch string
	execMatch  string
	cancel     context.CancelFunc
	failure    error
}

func (executor incomeYearFailureExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if executor.queryMatch != "" && strings.Contains(query, executor.queryMatch) {
		executor.cancel()
		return nil, ctx.Err()
	}
	return executor.CommandSQLExecutor.QueryContext(ctx, query, args...)
}

func (executor incomeYearFailureExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if executor.execMatch != "" && strings.Contains(query, executor.execMatch) {
		return nil, executor.failure
	}
	return executor.CommandSQLExecutor.ExecContext(ctx, query, args...)
}

func TestIncomeYearPinRepositoryPreservesDuringReadAndWriteFailures(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspaceAtVersion15(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	codec := newBASRepositoryCursorCodec(t)
	profile := pinProfileForTest(org, testUUID(9572), 1, testUUID(9573), testCivilDate(2026, 7, 1),
		testCivilDate(2027, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT, testCivilDate(2026, 1, 1))
	profileRef := appendBASProfile(t, newProfileRepositoryForTest(t, tx, codec), profile, nil)
	periodRef := appendBASPeriod(t, newPeriodRepositoryForTest(t, tx, codec), validBASPeriod(org, testUUID(9574), profileRef, 1,
		testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP), nil)
	for _, match := range []string{"bas_income_year_pins_v1", "bas_period_versions_v1", "bas_profile_versions_v1"} {
		t.Run(match, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repository, err := NewIncomeYearPinRepository(incomeYearFailureExecutor{CommandSQLExecutor: tx, queryMatch: match, cancel: cancel}, codec)
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.PinAfterConfirmedPeriod(ctx, org, periodRef); !errors.Is(err, context.Canceled) || errors.Is(err, ErrIncomeYearPinConflict) {
				t.Fatalf("during-read cancellation lost or misclassified: %v", err)
			}
		})
	}
	failure := errors.New("injected storage failure")
	repository, err := NewIncomeYearPinRepository(incomeYearFailureExecutor{CommandSQLExecutor: tx, execMatch: "INSERT INTO bas_income_year_pins_v1", failure: failure}, codec)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.PinAfterConfirmedPeriod(context.Background(), org, periodRef); !errors.Is(err, ErrIncomeYearPinRepository) || !errors.Is(err, failure) || errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("storage failure lost or reported as an input conflict: %v", err)
	}
}

func newBASRepositoryWorkspaceAtVersion15(t *testing.T) (*testkit.EncryptedWorkspace, string, string) {
	t.Helper()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 15)
	organisationID, otherOrganisationID := testUUID(9560), testUUID(9566)
	seedBASRepositoryOrganisation(t, workspace, organisationID)
	if _, err := workspace.Database.ExecContext(context.Background(), `DROP TRIGGER organisations_singleton_insert`); err != nil {
		t.Fatal(err)
	}
	seedBASRepositoryOrganisation(t, workspace, otherOrganisationID)
	if _, err := workspace.Database.ExecContext(context.Background(), `CREATE TRIGGER organisations_singleton_insert BEFORE INSERT ON organisations WHEN EXISTS(SELECT 1 FROM organisations) BEGIN SELECT RAISE(ABORT,'workspace already has an organisation'); END`); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{testUUID(36), testUUID(72)} {
		if _, err := workspace.Database.ExecContext(context.Background(), `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,?,?,'ACTIVE',?,?)`,
			actor, actor+"@example.invalid", "Income-year test actor", "2026-10-11T00:00:00Z", "2026-10-11T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	return workspace, organisationID, otherOrganisationID
}

func pinProfileForTest(org, profileID string, revision uint64, yearID string, yearStart, yearEnd *tammyv1.CivilDate,
	method tammyv1.BasPaygiMethod, effectiveStart *tammyv1.CivilDate) *tammyv1.BasObligationsProfile {
	profile := validBASProfile(org, profileID, revision)
	profile.Profile.EffectiveInterval = &tammyv1.BasEffectiveInterval{StartDate: proto.Clone(effectiveStart).(*tammyv1.CivilDate)}
	profile.Profile.Paygi.Applicability = &tammyv1.BasObligationAnswer{
		Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
		Reason:        "PAYGI instalment obligation applies", Evidence: []*tammyv1.SourceRef{{Type: "ato_notice", Id: testUUID(9561), Revision: 1, ContentHash: testHash(61)}},
	}
	profile.Profile.Paygi.Method = method
	profile.Profile.Paygi.Cadence = tammyv1.BasCadence_BAS_CADENCE_QUARTERLY
	profile.Profile.Paygi.ConfirmedIncomeYear = &tammyv1.BasIncomeYearIdentity{
		IncomeYearId: yearID, Dates: &tammyv1.BasCivilDateRange{StartDate: proto.Clone(yearStart).(*tammyv1.CivilDate), EndDate: proto.Clone(yearEnd).(*tammyv1.CivilDate)},
		Confirmation: &tammyv1.SourceRef{Type: "ato_income_year", Id: testUUID(9562), Revision: 1, ContentHash: testHash(62)},
	}
	return profile
}
