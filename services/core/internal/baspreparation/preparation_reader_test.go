//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestPreparationReaderHistoricalSnapshotIsIndependentOfCurrentPointer(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	codec := newBASRepositoryCursorCodec(t)
	reader, err := NewPreparationReader(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	first, ref := seedTypedCollectingPreparation(t, tx, org)
	second := proto.Clone(first).(*tammyv1.BasPreparation)
	second.Version++
	second.UpdatedAt.Seconds++
	second.ContentHash = nil
	secondRef := appendTypedCollectingPreparation(t, tx, second)
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_heads_v1 SET current_version=2,current_hash=?,cas_version=2,updated_at=? WHERE preparation_id=?`,
		secondRef.ContentHash, second.UpdatedAt.AsTime().Format("2006-01-02T15:04:05Z07:00"), first.PreparationId); err != nil {
		t.Fatal(err)
	}
	old, err := reader.GetExact(context.Background(), org, ref)
	if err != nil || old == nil || !proto.Equal(old.Preparation, first) || old.SavedPaygInputs != nil {
		t.Fatalf("exact historical preparation: retained=%v err=%v", old, err)
	}
	current, err := reader.GetCurrent(context.Background(), org, first.PreparationId)
	if err != nil || current == nil || current.Preparation.Version != 2 || !sameRef(preparationReference(current.Preparation), secondRef) {
		t.Fatalf("independent current preparation: retained=%v err=%v", current, err)
	}
	if _, err := reader.GetExact(context.Background(), otherOrg, ref); err == nil {
		t.Fatal("historical owner must be scoped to the organisation")
	}
	wrongHash := proto.Clone(ref).(*tammyv1.SourceRef)
	wrongHash.ContentHash[0] ^= 0xff
	if _, err := reader.GetExact(context.Background(), org, wrongHash); err == nil {
		t.Fatal("historical reads require the exact retained content hash")
	}
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=2`, first.PreparationId); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetCurrent(context.Background(), org, first.PreparationId); err == nil {
		t.Fatal("matching current pointer cannot conceal a corrupt current body")
	}
	if _, err := reader.GetExact(context.Background(), org, ref); err != nil {
		t.Fatalf("corrupt current body must not hide a valid historical owner: %v", err)
	}
}

func TestPreparationReaderRejectsNormalizedAndParentCorruption(t *testing.T) {
	for _, kind := range []string{"source_hash", "state", "family", "profile", "period"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, ref := seedTypedCollectingPreparation(t, tx, org)
			switch kind {
			case "source_hash", "state":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				query, argument := `UPDATE bas_preparation_versions_v1 SET source_snapshot_hash=? WHERE preparation_id=?`, any(testHash(99))
				if kind == "state" {
					query, argument = `UPDATE bas_preparation_versions_v1 SET state=? WHERE preparation_id=?`, "BLOCKED"
				}
				if _, err := tx.ExecContext(context.Background(), query, argument, value.PreparationId); err != nil {
					t.Fatal(err)
				}
			case "family":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_family_identities_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_family_identities_v1 SET identity_proto=X'01' WHERE family_id=?`, value.Family.Id); err != nil {
					t.Fatal(err)
				}
			case "profile":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_profile_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_profile_versions_v1 SET profile_proto=X'01' WHERE profile_id=?`, value.Profile.Id); err != nil {
					t.Fatal(err)
				}
			case "period":
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_period_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(context.Background(), `UPDATE bas_period_versions_v1 SET period_proto=X'01' WHERE period_id=?`, value.Period.Id); err != nil {
					t.Fatal(err)
				}
			}
			reader, err := NewPreparationReader(tx, newBASRepositoryCursorCodec(t))
			if err != nil {
				t.Fatal(err)
			}
			if body, err := reader.GetExact(context.Background(), org, ref); err == nil || body != nil {
				t.Fatal("a corrupt normalized row or exact retained parent cannot be used as a verified owner")
			}
		})
	}
}

func seedTypedCollectingPreparation(t *testing.T, executor app.CommandSQLExecutor, org string) (*tammyv1.BasPreparation, *tammyv1.SourceRef) {
	t.Helper()
	codec := newBASRepositoryCursorCodec(t)
	profile := validBASProfile(org, testUUID(9891), 1)
	profileRef := appendBASProfile(t, newProfileRepositoryForTest(t, executor, codec), profile, nil)
	period := validBASPeriod(org, testUUID(9892), profileRef, 1, testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	periodRef := appendBASPeriod(t, newPeriodRepositoryForTest(t, executor, codec), period, nil)
	family := validBASFamilyIdentity(org, testUUID(9893))
	familyRef, err := newFamilyIdentityRepositoryForTest(t, executor).Append(context.Background(), family)
	if err != nil {
		t.Fatal(err)
	}
	value := proto.Clone(readyReviewInput().Preparation).(*tammyv1.BasPreparation)
	value.PreparationId, value.OrganisationId = testUUID(9894), org
	value.Family, value.Profile, value.Period = familyRef, profileRef, periodRef
	value.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
	value.CurrentCalculation, value.CurrentValidation, value.FrozenBas = nil, nil, nil
	value.Predecessor, value.SupersededBy, value.ContentHash = nil, nil, nil
	value.Version = 1
	value.InputRevision = 1
	value.InputHash, err = hashPaygInputs(readyReviewInput().Calculation.PaygwInputs, readyReviewInput().Calculation.PaygiInputs)
	if err != nil {
		t.Fatal(err)
	}
	createdAt, _ := basTimestampSQLText(value.CreatedAt)
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_identities_v1(preparation_id,organisation_id,family_id,attempt_id,created_at) VALUES(?,?,?,?,?)`,
		value.PreparationId, org, familyRef.Id, testUUID(9895), createdAt); err != nil {
		t.Fatal(err)
	}
	ref := appendTypedCollectingPreparation(t, executor, value)
	value.ContentHash = ref.ContentHash
	updatedAt, _ := basTimestampSQLText(value.UpdatedAt)
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_heads_v1(preparation_id,organisation_id,current_version,current_hash,cas_version,updated_at) VALUES(?,?,1,?,1,?)`,
		value.PreparationId, org, ref.ContentHash, updatedAt); err != nil {
		t.Fatal(err)
	}
	return value, ref
}

func appendTypedCollectingPreparation(t *testing.T, executor app.CommandSQLExecutor, value *tammyv1.BasPreparation) *tammyv1.SourceRef {
	t.Helper()
	wire, hash, err := sealPreparation(value)
	if err != nil {
		t.Fatal(err)
	}
	createdAt, _ := basTimestampSQLText(value.CreatedAt)
	updatedAt, _ := basTimestampSQLText(value.UpdatedAt)
	if _, err := executor.ExecContext(context.Background(), `INSERT INTO bas_preparation_versions_v1(
		preparation_id,organisation_id,family_id,version,profile_id,profile_version,profile_hash,period_id,period_version,period_hash,
		input_revision,input_hash,dispute_overlay,source_snapshot_hash,state,content_hash,preparation_proto,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'NONE',?,'COLLECTING',?,?,?,?)`,
		value.PreparationId, value.OrganisationId, value.Family.Id, value.Version,
		value.Profile.Id, value.Profile.Revision, value.Profile.ContentHash, value.Period.Id, value.Period.Revision, value.Period.ContentHash,
		value.InputRevision, value.InputHash, value.SourceSnapshotHash, hash, wire, createdAt, updatedAt); err != nil {
		t.Fatal(err)
	}
	return &tammyv1.SourceRef{Type: "bas_preparation", Id: value.PreparationId, Revision: value.Version, ContentHash: hash}
}
