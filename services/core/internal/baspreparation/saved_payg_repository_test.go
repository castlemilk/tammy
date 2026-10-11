//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestSavedPaygRepositoryRetainsExactPairAndHistoricalOwner(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	owner, ownerRef := seedTypedCollectingPreparation(t, tx, org)
	repository, err := NewSavedPaygRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	value := savedPaygInputsForOwner(t, owner, ownerRef)
	before := proto.Clone(value)
	ref, err := repository.Append(context.Background(), value)
	if err != nil || ref == nil {
		t.Fatalf("retain reviewed pair: ref=%v err=%v", ref, err)
	}
	if !proto.Equal(before, value) {
		t.Fatal("retention mutated caller-owned evidence")
	}
	got, err := repository.GetExact(context.Background(), org, ref)
	if err != nil || got == nil || !sameRef(got.Preparation, ownerRef) || !bytesEqualPaygInputs(got, value) {
		t.Fatalf("exact reviewed pair and owner: value=%v err=%v", got, err)
	}
	if _, err := repository.GetExact(context.Background(), otherOrg, ref); err == nil {
		t.Fatal("PAYG evidence cannot cross organisation scope")
	}
	if _, err := repository.Append(context.Background(), value); err == nil {
		t.Fatal("a retained input revision cannot be overwritten by another append")
	}
	// A new current metadata version cannot change the exact historical owner.
	next := proto.Clone(owner).(*tammyv1.BasPreparation)
	next.Version++
	next.UpdatedAt.Seconds++
	next.ContentHash = nil
	nextRef := appendTypedCollectingPreparation(t, tx, next)
	updatedAt, _ := basTimestampSQLText(next.UpdatedAt)
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_heads_v1 SET current_version=2,current_hash=?,cas_version=2,updated_at=? WHERE preparation_id=?`, nextRef.ContentHash, updatedAt, owner.PreparationId); err != nil {
		t.Fatal(err)
	}
	if got, err := repository.GetExact(context.Background(), org, ref); err != nil || !sameRef(got.Preparation, ownerRef) {
		t.Fatalf("metadata updates cannot redirect retained PAYG evidence to a current preparation: value=%v err=%v", got, err)
	}
	stale := proto.Clone(value).(*tammyv1.BasSavedPaygInputs)
	stale.InputSnapshotId = testUUID(9902)
	if ref, err := repository.Append(context.Background(), stale); ref != nil || !errors.Is(err, ErrSavedPaygConflict) {
		t.Fatalf("new evidence requires the exact current owner: ref=%v err=%v", ref, err)
	}
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=2`, owner.PreparationId); err != nil {
		t.Fatal(err)
	}
	if got, err := repository.GetExact(context.Background(), org, ref); err != nil || !sameRef(got.Preparation, ownerRef) {
		t.Fatalf("corrupt current metadata must not redirect or hide valid historical evidence: value=%v err=%v", got, err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=1`, owner.PreparationId); err != nil {
		t.Fatal(err)
	}
	if got, err := repository.GetExact(context.Background(), org, ref); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Fatalf("saved evidence cannot conceal corruption of its exact historical owner: value=%v err=%v", got, err)
	}
}

func TestSavedPaygRepositoryRejectsCorruptOwnerBeforeAnyWrite(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	owner, ownerRef := seedTypedCollectingPreparation(t, tx, org)
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=?`, owner.PreparationId); err != nil {
		t.Fatal(err)
	}
	repository, err := NewSavedPaygRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	if ref, err := repository.Append(context.Background(), savedPaygInputsForOwner(t, owner, ownerRef)); ref != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Fatalf("matching owner pointer cannot authorize corrupt retained bytes: ref=%v err=%v", ref, err)
	}
	var count int
	if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_payg_input_versions_v1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected input append left retained facts: count=%d err=%v", count, err)
	}
}

func TestSavedPaygRepositoryRequiresOwnersExactStableInputIdentity(t *testing.T) {
	for _, kind := range []string{"revision", "pair_hash"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			owner, ownerRef := seedTypedCollectingPreparation(t, tx, org)
			value := savedPaygInputsForOwner(t, owner, ownerRef)
			if kind == "revision" {
				value.InputRevision++
			} else {
				value.PaygwInputs.ReviewReason = "Reviewed a different retained PAYGW fact set"
				var err error
				value.InputHash, err = hashPaygInputs(value.PaygwInputs, value.PaygiInputs)
				if err != nil {
					t.Fatal(err)
				}
			}
			repository, err := NewSavedPaygRepository(tx, newBASRepositoryCursorCodec(t))
			if err != nil {
				t.Fatal(err)
			}
			if ref, err := repository.Append(context.Background(), value); ref != nil || !errors.Is(err, ErrSavedPaygConflict) {
				t.Fatalf("saved pair must match the exact owner stable input identity before writing: ref=%v err=%v", ref, err)
			}
			var count int
			if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_payg_input_versions_v1`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected input identity wrote facts: count=%d err=%v", count, err)
			}
		})
	}
}

func TestSavedPaygRepositoryRejectsNormalizedActorAndBodyCorruption(t *testing.T) {
	for _, kind := range []string{"actor", "body"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			owner, ownerRef := seedTypedCollectingPreparation(t, tx, org)
			repository, err := NewSavedPaygRepository(tx, newBASRepositoryCursorCodec(t))
			if err != nil {
				t.Fatal(err)
			}
			ref, err := repository.Append(context.Background(), savedPaygInputsForOwner(t, owner, ownerRef))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_payg_input_versions_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			query, argument := `UPDATE bas_payg_input_versions_v1 SET actor_user_id=? WHERE input_snapshot_id=?`, any(testUUID(72))
			if kind == "body" {
				query, argument = `UPDATE bas_payg_input_versions_v1 SET input_proto=? WHERE input_snapshot_id=?`, []byte{1}
			}
			if _, err := tx.ExecContext(context.Background(), query, argument, ref.Id); err != nil {
				t.Fatal(err)
			}
			if value, err := repository.GetExact(context.Background(), org, ref); err == nil || value != nil {
				t.Fatal("corrupt PAYG snapshot cannot become a verified calculation source")
			}
		})
	}
}

func savedPaygInputsForOwner(t *testing.T, owner *tammyv1.BasPreparation, ownerRef *tammyv1.SourceRef) *tammyv1.BasSavedPaygInputs {
	t.Helper()
	review := readyReviewInput()
	hash, err := hashPaygInputs(review.Calculation.PaygwInputs, review.Calculation.PaygiInputs)
	if err != nil {
		t.Fatal(err)
	}
	return &tammyv1.BasSavedPaygInputs{InputSnapshotId: testUUID(9901), OrganisationId: owner.OrganisationId,
		PreparationId: owner.PreparationId, Preparation: proto.Clone(ownerRef).(*tammyv1.SourceRef),
		InputRevision: owner.InputRevision, InputHash: hash,
		PaygwInputs: review.Calculation.PaygwInputs, PaygiInputs: review.Calculation.PaygiInputs,
		ActorUserId: testUUID(36), RecordedAt: owner.UpdatedAt}
}

func bytesEqualPaygInputs(left, right *tammyv1.BasSavedPaygInputs) bool {
	return proto.Equal(left.PaygwInputs, right.PaygwInputs) && proto.Equal(left.PaygiInputs, right.PaygiInputs)
}
