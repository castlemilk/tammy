//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestEffectiveBASProfileKeepsKnownHistoryCorrectionsAndUnknownGaps(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	repo := newProfileRepositoryForTest(t, tx, newBASRepositoryCursorCodec(t))
	first := validBASProfile(org, testUUID(9761), 1)
	first.Profile.EffectiveInterval = &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 1, 1), EndDate: testCivilDate(2026, 3, 31)}
	firstRef := appendBASProfile(t, repo, first, nil)
	second := nextBASProfile(first, firstRef, 2)
	second.Profile.EffectiveInterval = &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 5, 1), EndDate: testCivilDate(2026, 6, 30)}
	secondRef := appendBASProfile(t, repo, second, firstRef)
	third := nextBASProfile(second, secondRef, 3)
	third.Profile.EffectiveInterval = &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 7, 1)}
	thirdRef := appendBASProfile(t, repo, third, secondRef)
	correction := nextBASProfile(third, thirdRef, 4)
	correctionRef := appendBASProfile(t, repo, correction, thirdRef)
	for _, tc := range []struct {
		start, end *tammyv1.CivilDate
		want       *tammyv1.SourceRef
	}{
		{testCivilDate(2026, 1, 1), testCivilDate(2026, 3, 31), firstRef},
		{testCivilDate(2026, 5, 1), testCivilDate(2026, 6, 30), secondRef},
		{testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), correctionRef},
		{testCivilDate(2026, 4, 1), testCivilDate(2026, 4, 30), nil},
		{testCivilDate(2026, 3, 31), testCivilDate(2026, 5, 1), nil},
		{testCivilDate(2025, 10, 1), testCivilDate(2025, 12, 31), nil},
	} {
		got, ref, err := repo.GetEffectiveForPeriod(context.Background(), org, &tammyv1.BasCivilDateRange{StartDate: tc.start, EndDate: tc.end})
		if tc.want == nil {
			if got != nil || ref != nil || !errors.Is(err, ErrProfileNotEffective) {
				t.Fatalf("unknown/crossing span=%v,%v,%v", got, ref, err)
			}
			continue
		}
		if err != nil || got == nil || !sameBASSourceRef(ref, tc.want) {
			t.Fatalf("known effective span=%v,%v,%v; want %v", got, ref, err, tc.want)
		}
	}
	historical, err := repo.GetExact(context.Background(), org, thirdRef)
	if err != nil || historical == nil || historical.Profile.Reason != third.Profile.Reason || !proto.Equal(historical.Profile.EffectiveInterval, third.Profile.EffectiveInterval) {
		t.Fatalf("correction rewrote retained history: %v,%v", historical, err)
	}
	if got, ref, err := repo.GetEffectiveAt(context.Background(), otherOrg, testCivilDate(2026, 8, 1)); got != nil || ref != nil || !errors.Is(err, ErrProfileNotEffective) {
		t.Fatalf("another owner gained profile: %v,%v,%v", got, ref, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, ref, err := repo.GetEffectiveForPeriod(ctx, org, &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)}); got != nil || ref != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled effective profile=%v,%v,%v", got, ref, err)
	}
}
