//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestProfileRepositoryCannotAppendOverCorruptCurrentSnapshot(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	codec := newBASRepositoryCursorCodec(t)
	tx := beginBASRepositoryTx(t, workspace)
	repository := newProfileRepositoryForTest(t, tx, codec)
	first := validBASProfile(org, testUUID(9801), 1)
	ref := appendBASProfile(t, repository, first, nil)
	commitBASRepositoryTx(t, tx)
	tx = beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_profile_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_profile_versions_v1 SET profile_proto=X'01' WHERE profile_id=?`, first.ProfileId); err != nil {
		t.Fatal(err)
	}
	repository = newProfileRepositoryForTest(t, tx, codec)
	if next, err := repository.Append(context.Background(), nextBASProfile(first, ref, 2), ref); !errors.Is(err, ErrSnapshotCorrupt) || next != nil {
		t.Fatalf("a matching head hash cannot authorise append over corrupt retained bytes: ref=%v err=%v", next, err)
	}
	var count int
	if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_profile_versions_v1 WHERE profile_id=?`, first.ProfileId).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed append wrote history: count=%d err=%v", count, err)
	}
}

func TestPeriodRepositoryCannotAppendOverCorruptCurrentSnapshot(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	codec := newBASRepositoryCursorCodec(t)
	tx := beginBASRepositoryTx(t, workspace)
	profile := appendBASProfile(t, newProfileRepositoryForTest(t, tx, codec), validBASProfile(org, testUUID(9811), 1), nil)
	first := validBASPeriod(org, testUUID(9812), profile, 1, &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1}, &tammyv1.CivilDate{Year: 2026, Month: 12, Day: 31}, tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC)
	ref := appendBASPeriod(t, newPeriodRepositoryForTest(t, tx, codec), first, nil)
	commitBASRepositoryTx(t, tx)
	tx = beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_period_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_period_versions_v1 SET period_proto=X'01' WHERE period_id=?`, first.PeriodId); err != nil {
		t.Fatal(err)
	}
	if next, err := newPeriodRepositoryForTest(t, tx, codec).Append(context.Background(), nextBASPeriod(first, 2), ref); !errors.Is(err, ErrSnapshotCorrupt) || next != nil {
		t.Fatalf("a matching head hash cannot authorise append over corrupt period bytes: ref=%v err=%v", next, err)
	}
	var count int
	if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_period_versions_v1 WHERE period_id=?`, first.PeriodId).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed append wrote history: count=%d err=%v", count, err)
	}
}

func TestBASRepositoriesRejectNormalizedActorMismatch(t *testing.T) {
	for _, kind := range []string{"profile", "period"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			codec := newBASRepositoryCursorCodec(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			profile := validBASProfile(org, testUUID(9821), 1)
			profiles := newProfileRepositoryForTest(t, tx, codec)
			profileRef := appendBASProfile(t, profiles, profile, nil)
			if _, err := profiles.GetExact(context.Background(), org, profileRef); err != nil {
				t.Fatal(err)
			}
			actor := testUUID(72)
			if kind == "profile" {
				if profile.ActorUserId == actor {
					actor = testUUID(36)
				}
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_profile_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_profile_versions_v1 SET actor_user_id=? WHERE profile_id=?`, actor, profile.ProfileId); err != nil {
					t.Fatal(err)
				}
				if value, err := profiles.GetExact(context.Background(), org, profileRef); err == nil || value != nil {
					t.Fatal("normalized actor differs from the retained profile body")
				}
				return
			}
			period := validBASPeriod(org, testUUID(9822), profileRef, 1, testCivilDate(2026, 10, 1), testCivilDate(2026, 12, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC)
			periods := newPeriodRepositoryForTest(t, tx, codec)
			periodRef := appendBASPeriod(t, periods, period, nil)
			if _, err := periods.GetExact(context.Background(), org, periodRef); err != nil {
				t.Fatal(err)
			}
			if period.ActorUserId == actor {
				actor = testUUID(36)
			}
			if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_period_versions_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(context.Background(), `UPDATE bas_period_versions_v1 SET actor_user_id=? WHERE period_id=?`, actor, period.PeriodId); err != nil {
				t.Fatal(err)
			}
			if value, err := periods.GetExact(context.Background(), org, periodRef); err == nil || value != nil {
				t.Fatal("normalized actor differs from the retained period body")
			}
		})
	}
}

func TestPeriodAppendVerifiesExactHistoricalProfileBeforeWriting(t *testing.T) {
	for _, currentProfile := range []bool{true, false} {
		name := "historical_profile"
		if currentProfile {
			name = "current_profile"
		}
		t.Run(name, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			codec := newBASRepositoryCursorCodec(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			profiles := newProfileRepositoryForTest(t, tx, codec)
			first := validBASProfile(org, testUUID(9831), 1)
			profileRef := appendBASProfile(t, profiles, first, nil)
			if !currentProfile {
				appendBASProfile(t, profiles, nextBASProfile(first, profileRef, 2), profileRef)
			}
			if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_profile_versions_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(context.Background(), `UPDATE bas_profile_versions_v1 SET profile_proto=X'01' WHERE profile_id=? AND version=1`, first.ProfileId); err != nil {
				t.Fatal(err)
			}
			period := validBASPeriod(org, testUUID(9832), profileRef, 1, testCivilDate(2026, 10, 1), testCivilDate(2026, 12, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC)
			if ref, err := newPeriodRepositoryForTest(t, tx, codec).Append(context.Background(), period, nil); ref != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("a scoped foreign key cannot authorize a corrupt exact profile: ref=%v err=%v", ref, err)
			}
			var count int
			if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_periods_v1`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected corrupt dependency wrote a period identity: count=%d err=%v", count, err)
			}
		})
	}
}

func TestPeriodAppendUsesPinnedProfileRatherThanCurrentProfile(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	codec := newBASRepositoryCursorCodec(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	profiles := newProfileRepositoryForTest(t, tx, codec)
	first := validBASProfile(org, testUUID(9841), 1)
	profileRef := appendBASProfile(t, profiles, first, nil)
	appendBASProfile(t, profiles, nextBASProfile(first, profileRef, 2), profileRef)
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_profile_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_profile_versions_v1 SET profile_proto=X'01' WHERE profile_id=? AND version=2`, first.ProfileId); err != nil {
		t.Fatal(err)
	}
	period := validBASPeriod(org, testUUID(9842), profileRef, 1, testCivilDate(2026, 10, 1), testCivilDate(2026, 12, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC)
	ref, err := newPeriodRepositoryForTest(t, tx, codec).Append(context.Background(), period, nil)
	if err != nil || ref == nil {
		t.Fatalf("valid pinned historical profile remains usable independently of current metadata: ref=%v err=%v", ref, err)
	}
}
