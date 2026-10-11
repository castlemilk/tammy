//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func TestFamilyIdentityRepositoryRetainsOneFamilyPerActualScope(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	repository := newFamilyIdentityRepositoryForTest(t, tx)
	value := validBASFamilyIdentity(org, testUUID(9851))
	before := proto.Clone(value)
	ref, err := repository.Append(context.Background(), value)
	if err != nil || ref == nil || ref.Type != "bas_family" || ref.Revision != 1 {
		t.Fatalf("retain initial family: ref=%v err=%v", ref, err)
	}
	if !proto.Equal(before, value) {
		t.Fatal("family append changed caller-owned bytes")
	}
	retained, err := repository.GetExact(context.Background(), org, ref)
	if err != nil || retained.FamilyId != value.FamilyId {
		t.Fatalf("read exact family: value=%v err=%v", retained, err)
	}
	scope := familyScopeForIdentity(value)
	found, exists, err := repository.FindByScope(context.Background(), scope)
	if err != nil || !exists || !proto.Equal(found, retained) {
		t.Fatalf("actual scope must select the exact retained family: value=%v exists=%v err=%v", found, exists, err)
	}
	duplicate := proto.Clone(value).(*tammyv1.BasFamilyIdentity)
	duplicate.FamilyId = testUUID(9852)
	duplicate.CreatedAt.Seconds++
	if other, err := repository.Append(context.Background(), duplicate); other != nil || !errors.Is(err, ErrFamilyIdentityConflict) {
		t.Fatalf("a new identity/time cannot split an existing actual statement scope: ref=%v err=%v", other, err)
	}
	duplicate.OrganisationId = otherOrg
	if _, err := repository.Append(context.Background(), duplicate); err != nil {
		t.Fatalf("another organisation has an independent actual period scope: %v", err)
	}
	missing := scope
	missing.Window = tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_CUSTOM
	if value, exists, err := repository.FindByScope(context.Background(), missing); err != nil || exists || value != nil {
		t.Fatalf("missing scope must remain explicit absence: value=%v exists=%v err=%v", value, exists, err)
	}
	if value, err := repository.GetExact(context.Background(), otherOrg, ref); value != nil || err == nil {
		t.Fatal("exact family reads must be organisation-scoped")
	}
	for _, change := range []func(*tammyv1.SourceRef){
		func(exact *tammyv1.SourceRef) { exact.ContentHash[0] ^= 0xff },
		func(exact *tammyv1.SourceRef) { exact.Revision++ },
		func(exact *tammyv1.SourceRef) { exact.Type = "bas_family_state" },
	} {
		exact := proto.Clone(ref).(*tammyv1.SourceRef)
		change(exact)
		if value, err := repository.GetExact(context.Background(), org, exact); value != nil || err == nil {
			t.Fatal("an inexact family reference was accepted")
		}
	}
}

func TestFamilyIdentityRepositoryLeavesCommitAndRollbackWithCaller(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	value := validBASFamilyIdentity(org, testUUID(9861))
	tx := beginBASRepositoryTx(t, workspace)
	repository := newFamilyIdentityRepositoryForTest(t, tx)
	if _, err := repository.Append(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx = beginBASRepositoryTx(t, workspace)
	repository = newFamilyIdentityRepositoryForTest(t, tx)
	if value, exists, err := repository.FindByScope(context.Background(), familyScopeForIdentity(value)); err != nil || exists || value != nil {
		t.Fatalf("caller rollback must remove the family: value=%v exists=%v err=%v", value, exists, err)
	}
	ref, err := repository.Append(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	commitBASRepositoryTx(t, tx)
	tx = beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	repository = newFamilyIdentityRepositoryForTest(t, tx)
	if _, err := repository.GetExact(context.Background(), org, ref); err != nil {
		t.Fatalf("caller commit must retain the exact immutable family: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_family_identities_v1 SET reporting_window='CUSTOM' WHERE family_id=?`, value.FamilyId); err == nil {
		t.Fatal("retained family scope was mutable")
	}
	if _, err := tx.ExecContext(context.Background(), `DELETE FROM bas_family_identities_v1 WHERE family_id=?`, value.FamilyId); err == nil {
		t.Fatal("retained family was deletable")
	}
}

func TestFamilyIdentityRepositoryFailsClosedOnBodyAndNormalizedCorruption(t *testing.T) {
	for _, kind := range []string{"truncated", "normalized_scope", "unknown_fields", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			repository := newFamilyIdentityRepositoryForTest(t, tx)
			value := validBASFamilyIdentity(org, testUUID(9871))
			ref, err := repository.Append(context.Background(), value)
			if err != nil {
				t.Fatal(err)
			}
			for _, trigger := range []string{"bas_family_identities_v1_no_update", "bas_family_identities_v1_no_conflicting_insert"} {
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER `+trigger); err != nil {
					t.Fatal(err)
				}
			}
			var updateErr error
			switch kind {
			case "truncated":
				_, updateErr = tx.ExecContext(context.Background(), `UPDATE bas_family_identities_v1 SET identity_proto=X'01' WHERE family_id=?`, value.FamilyId)
			case "normalized_scope":
				_, updateErr = tx.ExecContext(context.Background(), `UPDATE bas_family_identities_v1 SET period_end='2026-09-29' WHERE family_id=?`, value.FamilyId)
			case "unknown_fields":
				wire, _, err := sealFamilyIdentity(value)
				if err != nil {
					t.Fatal(err)
				}
				wire = append(wire, 0xf8, 0x07, 0x01)
				_, updateErr = tx.ExecContext(context.Background(), `UPDATE bas_family_identities_v1 SET identity_proto=? WHERE family_id=?`, wire, value.FamilyId)
			case "oversize":
				if _, err := tx.ExecContext(context.Background(), `PRAGMA ignore_check_constraints=ON`); err != nil {
					t.Fatal(err)
				}
				_, updateErr = tx.ExecContext(context.Background(), `UPDATE bas_family_identities_v1 SET identity_proto=zeroblob(65537) WHERE family_id=?`, value.FamilyId)
			}
			if updateErr != nil {
				t.Fatal(updateErr)
			}
			if body, err := repository.GetExact(context.Background(), org, ref); err == nil || body != nil {
				t.Fatal("exact family read accepted corrupt retained data")
			}
			lookupScope := familyScopeForIdentity(value)
			if kind == "normalized_scope" {
				lookupScope.End = testCivilDate(2026, 9, 29)
			}
			if body, exists, err := repository.FindByScope(context.Background(), lookupScope); err == nil || body != nil || exists {
				t.Fatal("scope lookup concealed corrupt retained data as valid identity or absence")
			}
			if ref, err := repository.Append(context.Background(), value); err == nil || ref != nil {
				t.Fatal("append cannot heal or replace a corrupt retained identity")
			}
		})
	}
}

func TestFamilyIdentityRepositoryReadAfterEncryptedWorkspaceReopen(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	repository := newFamilyIdentityRepositoryForTest(t, tx)
	value := validBASFamilyIdentity(org, testUUID(9881))
	ref, err := repository.Append(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	commitBASRepositoryTx(t, tx)
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(context.Background(), workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repository = newFamilyIdentityRepositoryForTest(t, reopened)
	if body, err := repository.GetExact(context.Background(), org, ref); err != nil || body == nil || body.FamilyId != value.FamilyId {
		t.Fatalf("immutable family survived encrypted reopen: value=%v err=%v", body, err)
	}
	if body, exists, err := repository.FindByScope(context.Background(), familyScopeForIdentity(value)); err != nil || !exists || body == nil || body.FamilyId != value.FamilyId {
		t.Fatalf("exact actual-scope family survived encrypted reopen: value=%v exists=%v err=%v", body, exists, err)
	}
}

func newFamilyIdentityRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *FamilyIdentityRepository {
	t.Helper()
	repository, err := NewFamilyIdentityRepository(executor)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func validBASFamilyIdentity(org, id string) *tammyv1.BasFamilyIdentity {
	return &tammyv1.BasFamilyIdentity{FamilyId: id, OrganisationId: org,
		PeriodScope: &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)},
		Window:      tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, CreatedAt: readyReviewInput().Preparation.CreatedAt}
}

func familyScopeForIdentity(value *tammyv1.BasFamilyIdentity) FamilyScope {
	return FamilyScope{OrganisationID: value.OrganisationId, Start: value.PeriodScope.StartDate, End: value.PeriodScope.EndDate, Window: value.Window}
}
