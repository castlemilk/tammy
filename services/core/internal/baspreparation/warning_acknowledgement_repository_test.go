//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWarningAcknowledgementRepositoryRetainsExactWarningAcrossCurrentValidationChange(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	baseFixture := seedValidationRepositoryFixture(t, tx, organisationID)
	fixture := appendWarningAcknowledgementValidationOwner(t, tx, organisationID, baseFixture, baseFixture.validation)
	repository := newWarningAcknowledgementRepositoryForTest(t, tx)
	acknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 20001)
	before := proto.Clone(acknowledgement).(*tammyv1.BasWarningAcknowledgement)
	acknowledgementRef, err := repository.Append(ctx, organisationID, acknowledgement, fixture.currentPreparationRef)
	if err != nil || acknowledgementRef == nil {
		t.Fatalf("append exact warning acknowledgement: ref=%v err=%v", acknowledgementRef, err)
	}
	if !proto.Equal(acknowledgement, before) {
		t.Fatal("append mutated the caller-owned warning acknowledgement")
	}
	got, err := repository.GetExact(ctx, organisationID, acknowledgementRef)
	if err != nil || !proto.Equal(got, before) {
		t.Fatalf("get exact acknowledgement: value=%v err=%v", got, err)
	}
	listed, err := repository.ListForValidation(ctx, organisationID, fixture.validationRef)
	if err != nil || len(listed) != 1 || !proto.Equal(listed[0], before) {
		t.Fatalf("list exact validation acknowledgements: values=%v err=%v", listed, err)
	}

	// Move the current owner to a newer validation. Exact acknowledgement reads
	// must continue to follow the immutable validation and finding it names.
	newValidationRef := appendWarningAcknowledgementValidationRevision(t, tx, organisationID, fixture)
	newPreparationRef := appendWarningAcknowledgementCurrentOwner(t, tx, organisationID,
		fixture.currentPreparationRef, newValidationRef)
	if sameBASSourceRef(newPreparationRef, fixture.currentPreparationRef) {
		t.Fatal("fixture did not advance the current preparation pointer")
	}
	got, err = repository.GetExact(ctx, organisationID, acknowledgementRef)
	if err != nil || !proto.Equal(got, before) {
		t.Fatalf("historical acknowledgement changed after current validation advanced: value=%v err=%v", got, err)
	}
	listed, err = repository.ListForValidation(ctx, organisationID, fixture.validationRef)
	if err != nil || len(listed) != 1 || !proto.Equal(listed[0], before) {
		t.Fatalf("historical validation acknowledgement disappeared after pointer advance: values=%v err=%v", listed, err)
	}
	commitBASRepositoryTx(t, tx)

	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repository = newWarningAcknowledgementRepositoryForTest(t, reopened)
	got, err = repository.GetExact(ctx, organisationID, acknowledgementRef)
	if err != nil || !proto.Equal(got, before) {
		t.Fatalf("exact acknowledgement survived encrypted reopen: value=%v err=%v", got, err)
	}
	listed, err = repository.ListForValidation(ctx, organisationID, fixture.validationRef)
	if err != nil || len(listed) != 1 || !proto.Equal(listed[0], before) {
		t.Fatalf("validation acknowledgement list survived encrypted reopen: values=%v err=%v", listed, err)
	}
}

func TestWarningAcknowledgementRepositoryRejectsStaleCurrentValidation(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	baseFixture := seedValidationRepositoryFixture(t, tx, organisationID)
	fixture := appendWarningAcknowledgementValidationOwner(t, tx, organisationID, baseFixture, baseFixture.validation)
	newValidationRef := appendWarningAcknowledgementValidationRevision(t, tx, organisationID, fixture)
	newPreparationRef := appendWarningAcknowledgementCurrentOwner(t, tx, organisationID,
		fixture.currentPreparationRef, newValidationRef)
	if sameBASSourceRef(newPreparationRef, fixture.currentPreparationRef) {
		t.Fatal("fixture did not advance the current validation")
	}

	acknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 20002)
	ref, err := newWarningAcknowledgementRepositoryForTest(t, tx).Append(ctx, organisationID,
		acknowledgement, newPreparationRef)
	if ref != nil || err == nil {
		t.Fatalf("stale validation acknowledgement was accepted: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_warning_acknowledgements_v1", 0)
}

func TestWarningAcknowledgementRepositoryScopesOperationsByOrganisation(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	baseFixture := seedValidationRepositoryFixture(t, tx, organisationID)
	fixture := appendWarningAcknowledgementValidationOwner(t, tx, organisationID, baseFixture, baseFixture.validation)
	repository := newWarningAcknowledgementRepositoryForTest(t, tx)
	acknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 23001)
	acknowledgementRef, err := repository.Append(ctx, organisationID, acknowledgement, fixture.currentPreparationRef)
	if err != nil || acknowledgementRef == nil {
		t.Fatalf("append positive organisation-scoped fixture: ref=%v err=%v", acknowledgementRef, err)
	}

	otherOrganisationID := testUUID(23002)
	foreignAcknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 23003)
	if ref, err := repository.Append(ctx, otherOrganisationID, foreignAcknowledgement, fixture.currentPreparationRef); ref != nil || err == nil {
		t.Fatalf("append using another organisation was accepted: ref=%v err=%v", ref, err)
	}
	if got, err := repository.GetExact(ctx, otherOrganisationID, acknowledgementRef); got != nil || err == nil {
		t.Fatalf("exact read using another organisation returned the acknowledgement: value=%v err=%v", got, err)
	}
	if listed, err := repository.ListForValidation(ctx, otherOrganisationID, fixture.validationRef); listed != nil || err == nil {
		t.Fatalf("validation list using another organisation returned acknowledgements: values=%v err=%v", listed, err)
	}
	assertBASRepositoryCount(t, tx, "bas_warning_acknowledgements_v1", 1)
}

func TestWarningAcknowledgementRepositoryRequiresExactCurrentOwnerRef(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	baseFixture := seedValidationRepositoryFixture(t, tx, organisationID)
	fixture := appendWarningAcknowledgementValidationOwner(t, tx, organisationID, baseFixture, baseFixture.validation)
	repository := newWarningAcknowledgementRepositoryForTest(t, tx)
	oldRevisionRef := cloneRef(fixture.ownerRef)
	if oldRevisionRef.Id != fixture.currentPreparationRef.Id || oldRevisionRef.Revision >= fixture.currentPreparationRef.Revision {
		t.Fatalf("fixture lacks a stale revision for the same preparation: old=%v current=%v", oldRevisionRef, fixture.currentPreparationRef)
	}
	alteredHashRef := cloneRef(fixture.currentPreparationRef)
	alteredHashRef.ContentHash[0] ^= 0xff

	for index, staleRef := range []*tammyv1.SourceRef{oldRevisionRef, alteredHashRef} {
		acknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
			fixture.validation.Findings[0], uint64(23010+index))
		if ref, err := repository.Append(ctx, organisationID, acknowledgement, staleRef); ref != nil || err == nil {
			t.Fatalf("append using stale current-owner ref %d was accepted: ref=%v err=%v", index, ref, err)
		}
		assertBASRepositoryCount(t, tx, "bas_warning_acknowledgements_v1", 0)
	}

	validAcknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 23012)
	if ref, err := repository.Append(ctx, organisationID, validAcknowledgement, fixture.currentPreparationRef); ref == nil || err != nil {
		t.Fatalf("exact current-owner positive control was rejected: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_warning_acknowledgements_v1", 1)
}

func TestWarningAcknowledgementRepositoryRejectsNonWarningAndCrossValidationFinding(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	baseFixture := seedValidationRepositoryFixture(t, tx, organisationID)
	fixture := appendWarningAcknowledgementValidationOwner(t, tx, organisationID, baseFixture, baseFixture.validation)
	repository := newWarningAcknowledgementRepositoryForTest(t, tx)

	nonWarningAcknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[1], 23020)
	if ref, err := repository.Append(ctx, organisationID, nonWarningAcknowledgement, fixture.currentPreparationRef); ref != nil || err == nil {
		t.Fatalf("acknowledgement of an information finding was accepted: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_warning_acknowledgements_v1", 0)

	otherValidation := proto.Clone(fixture.validation).(*tammyv1.BasValidationSnapshot)
	otherValidation.ValidationId = testUUID(23021)
	otherValidation.Revision = 1
	otherValidation.ContentHash = nil
	otherValidation.PreparationHash = bytes.Clone(fixture.currentPreparationRef.ContentHash)
	otherValidation.CreatedAt.Seconds++
	otherValidation.Findings[0].FindingId = testUUID(23022)
	otherValidation.Findings[0].StableCode = "OTHER_VALIDATION_WARNING"
	otherValidationRef, err := newValidationRepositoryForTest(t, tx).Append(ctx, organisationID,
		otherValidation, fixture.currentPreparationRef)
	if err != nil || otherValidationRef == nil {
		t.Fatalf("append positive cross-validation fixture: ref=%v err=%v", otherValidationRef, err)
	}
	crossValidationAcknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 23023)
	crossValidationAcknowledgement.Finding = &tammyv1.SourceRef{Type: "bas_validation_finding",
		Id: otherValidation.Findings[0].FindingId, Revision: otherValidation.Revision,
		ContentHash: canonicalFindingHash(otherValidation.Findings[0])}
	if ref, err := repository.Append(ctx, organisationID, crossValidationAcknowledgement,
		fixture.currentPreparationRef); ref != nil || err == nil {
		t.Fatalf("acknowledgement using another validation's finding was accepted: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_warning_acknowledgements_v1", 0)

	validAcknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 23024)
	if ref, err := repository.Append(ctx, organisationID, validAcknowledgement, fixture.currentPreparationRef); ref == nil || err != nil {
		t.Fatalf("matching warning positive control was rejected: ref=%v err=%v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_warning_acknowledgements_v1", 1)
}

func TestWarningAcknowledgementRepositorySupportsTwoHundredWarnings(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	base := seedValidationRepositoryFixture(t, tx, organisationID)
	validation := proto.Clone(base.validation).(*tammyv1.BasValidationSnapshot)
	validation.Findings = make([]*tammyv1.BasValidationFinding, 200)
	for index := range validation.Findings {
		validation.Findings[index] = &tammyv1.BasValidationFinding{
			FindingId: testUUID(uint64(21000 + index)), Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING,
			StableCode: "WARNING_" + strconv.Itoa(index), SafeMessage: "Review this exact retained warning",
		}
	}
	fixture := appendWarningAcknowledgementValidationOwner(t, tx, organisationID, base, validation)
	repository := newWarningAcknowledgementRepositoryForTest(t, tx)
	wantIDs := make(map[string]bool, len(validation.Findings))
	for index, finding := range validation.Findings {
		acknowledgementID := uint64(22000 + index)
		acknowledgement := warningAcknowledgementForTest(validation, fixture.validationRef, finding, acknowledgementID)
		if _, err := repository.Append(ctx, organisationID, acknowledgement, fixture.currentPreparationRef); err != nil {
			t.Fatalf("append warning acknowledgement %d of 200: %v", index+1, err)
		}
		wantIDs[finding.FindingId] = true
	}
	listed, err := repository.ListForValidation(ctx, organisationID, fixture.validationRef)
	if err != nil || len(listed) != 200 {
		t.Fatalf("list all bounded warning acknowledgements: count=%d err=%v", len(listed), err)
	}
	for _, acknowledgement := range listed {
		if acknowledgement == nil || acknowledgement.Finding == nil || !wantIDs[acknowledgement.Finding.Id] {
			t.Fatalf("list returned an acknowledgement outside the exact 200-warning validation: %#v", acknowledgement)
		}
		delete(wantIDs, acknowledgement.Finding.Id)
	}
	if len(wantIDs) != 0 {
		t.Fatalf("list omitted %d retained warning acknowledgements", len(wantIDs))
	}
}

func TestWarningAcknowledgementRepositoryRejectsCorruptBodyAndParentFinding(t *testing.T) {
	for _, kind := range []string{"normalized_scalar", "body", "parent_finding"} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			baseFixture := seedValidationRepositoryFixture(t, tx, organisationID)
			fixture := appendWarningAcknowledgementValidationOwner(t, tx, organisationID, baseFixture, baseFixture.validation)
			acknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
				fixture.validation.Findings[0], 20003)
			repository := newWarningAcknowledgementRepositoryForTest(t, tx)
			acknowledgementRef, err := repository.Append(ctx, organisationID, acknowledgement, fixture.currentPreparationRef)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "normalized_scalar":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_warning_acknowledgements_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_warning_acknowledgements_v1 SET actor_user_id=? WHERE acknowledgement_id=?`,
					testUUID(36), acknowledgementRef.Id); err != nil {
					t.Fatal(err)
				}
			case "body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_warning_acknowledgements_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_warning_acknowledgements_v1 SET acknowledgement_proto=X'01' WHERE acknowledgement_id=?`,
					acknowledgementRef.Id); err != nil {
					t.Fatal(err)
				}
			case "parent_finding":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_validation_findings_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_validation_findings_v1 SET finding_proto=X'01' WHERE validation_id=? AND revision=? AND finding_id=?`,
					fixture.validationRef.Id, fixture.validationRef.Revision, acknowledgement.Finding.Id); err != nil {
					t.Fatal(err)
				}
			}

			got, err := repository.GetExact(ctx, organisationID, acknowledgementRef)
			if got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt %s acknowledgement or parent became readable: value=%v err=%v", kind, got, err)
			}
			listed, err := repository.ListForValidation(ctx, organisationID, fixture.validationRef)
			if listed != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("list skipped corrupt %s acknowledgement or parent: values=%v err=%v", kind, listed, err)
			}
		})
	}
}

func TestWarningAcknowledgementRepositoryRespectsCallerRollback(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	setupTx := beginBASRepositoryTx(t, workspace)
	baseFixture := seedValidationRepositoryFixture(t, setupTx, organisationID)
	fixture := appendWarningAcknowledgementValidationOwner(t, setupTx, organisationID, baseFixture, baseFixture.validation)
	commitBASRepositoryTx(t, setupTx)

	acknowledgementTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgement := warningAcknowledgementForTest(fixture.validation, fixture.validationRef,
		fixture.validation.Findings[0], 20004)
	if ref, err := newWarningAcknowledgementRepositoryForTest(t, acknowledgementTx).Append(ctx, organisationID,
		acknowledgement, fixture.currentPreparationRef); err != nil || ref == nil {
		_ = acknowledgementTx.Rollback()
		t.Fatalf("append inside caller transaction: ref=%v err=%v", ref, err)
	}
	if err := acknowledgementTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertBASRepositoryCount(t, workspace.Database, "bas_warning_acknowledgements_v1", 0)
}

type warningAcknowledgementRepositoryFixture struct {
	validationRepositoryFixture
	validationRef         *tammyv1.SourceRef
	currentPreparationRef *tammyv1.SourceRef
}

func appendWarningAcknowledgementValidationOwner(t *testing.T, executor app.CommandSQLExecutor, organisationID string,
	base validationRepositoryFixture, validation *tammyv1.BasValidationSnapshot) warningAcknowledgementRepositoryFixture {
	t.Helper()
	ctx := context.Background()
	validationRef, err := newValidationRepositoryForTest(t, executor).Append(ctx, organisationID, validation, base.ownerRef)
	if err != nil {
		t.Fatalf("append validation for warning acknowledgement fixture: %v", err)
	}
	next := clonePreparationRetainedSnapshot(base.owner)
	next.Preparation.Version++
	next.Preparation.UpdatedAt.Seconds++
	next.Preparation.ContentHash = nil
	next.Preparation.CurrentValidation = cloneRef(validationRef)
	next.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY
	currentPreparationRef, err := newPreparationRepositoryForValidationTest(t, executor).Append(ctx, next, base.ownerRef)
	if err != nil {
		t.Fatalf("link validation to review-ready acknowledgement owner: %v", err)
	}
	retainedValidation, err := newValidationRepositoryForTest(t, executor).GetExact(ctx, organisationID, validationRef)
	if err != nil {
		t.Fatal(err)
	}
	base.validation = retainedValidation
	return warningAcknowledgementRepositoryFixture{validationRepositoryFixture: base,
		validationRef: validationRef, currentPreparationRef: currentPreparationRef}
}

func appendWarningAcknowledgementValidationRevision(t *testing.T, executor app.CommandSQLExecutor,
	organisationID string, fixture warningAcknowledgementRepositoryFixture) *tammyv1.SourceRef {
	t.Helper()
	ctx := context.Background()
	next := nextValidationRevision(fixture.validation)
	next.PreparationHash = bytes.Clone(fixture.currentPreparationRef.ContentHash)
	next.ContentHash = nil
	ref, err := newValidationRepositoryForTest(t, executor).Append(ctx, organisationID, next, fixture.currentPreparationRef)
	if err != nil {
		t.Fatalf("append newer validation for warning acknowledgement fixture: %v", err)
	}
	return ref
}

func appendWarningAcknowledgementCurrentOwner(t *testing.T, executor app.CommandSQLExecutor, organisationID string,
	currentPreparationRef, currentValidationRef *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	ctx := context.Background()
	preparations := newPreparationRepositoryForValidationTest(t, executor)
	current, err := preparations.GetExact(ctx, organisationID, currentPreparationRef)
	if err != nil {
		t.Fatalf("read current warning acknowledgement preparation: %v", err)
	}
	next := clonePreparationRetainedSnapshot(current)
	next.Preparation.Version++
	next.Preparation.UpdatedAt.Seconds++
	next.Preparation.ContentHash = nil
	next.Preparation.CurrentValidation = cloneRef(currentValidationRef)
	next.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY
	ref, err := preparations.Append(ctx, next, currentPreparationRef)
	if err != nil {
		t.Fatalf("advance warning acknowledgement preparation validation: %v", err)
	}
	return ref
}

func warningAcknowledgementForTest(validation *tammyv1.BasValidationSnapshot, validationRef *tammyv1.SourceRef,
	finding *tammyv1.BasValidationFinding, acknowledgementNumber uint64) *tammyv1.BasWarningAcknowledgement {
	return &tammyv1.BasWarningAcknowledgement{
		AcknowledgementId: testUUID(acknowledgementNumber), Validation: cloneRef(validationRef),
		Finding: &tammyv1.SourceRef{Type: "bas_validation_finding", Id: finding.FindingId,
			Revision: validation.Revision, ContentHash: canonicalFindingHash(finding)},
		SourceSnapshotHash: bytes.Clone(validation.SourceSnapshotHash), ActorUserId: testUUID(72),
		AcknowledgedAt: timestamppb.New(time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)),
	}
}

func newWarningAcknowledgementRepositoryForTest(t *testing.T, executor app.CommandSQLExecutor) *WarningAcknowledgementRepository {
	t.Helper()
	repository, err := NewWarningAcknowledgementRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatalf("construct warning acknowledgement repository: %v", err)
	}
	return repository
}
