package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func retainedSnapshotReview(t *testing.T) (*tammyv1.BasPreparation, *tammyv1.BasValidationSnapshot) {
	t.Helper()
	review := readyReviewInput()
	owner := review.Preparation
	owner.ContentHash = nil
	owner.CurrentValidation = nil
	wire, hash, err := sealPreparation(owner)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = openPreparation(wire, owner.OrganisationId, &tammyv1.SourceRef{Type: "bas_preparation", Id: owner.PreparationId, Revision: owner.Version, ContentHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	validation := review.Validation
	validation.ContentHash = nil
	validation.PreparationHash = bytes.Clone(owner.ContentHash)
	validation.Findings = []*tammyv1.BasValidationFinding{finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING, "EVIDENCE_REVIEW")}
	return owner, validation
}

func TestSnapshotValidationUsesExactHistoricalOwnerAndNeverCallerClaimedScope(t *testing.T) {
	owner, validation := retainedSnapshotReview(t)
	seal := func(value *tammyv1.BasValidationSnapshot) ([]byte, []byte, error) {
		return sealValidation(value, owner)
	}
	open := func(wire []byte, org string, ref *tammyv1.SourceRef) (*tammyv1.BasValidationSnapshot, error) {
		return openValidation(wire, org, ref, owner)
	}
	assertTypedSnapshotRoundTrip(t, validation, validation.ValidationId, owner.OrganisationId, "bas_validation", validation.Revision, seal, open)
	for _, change := range []func(*tammyv1.BasValidationSnapshot){
		func(value *tammyv1.BasValidationSnapshot) { value.PreparationId = testUUID(3301) },
		func(value *tammyv1.BasValidationSnapshot) { value.PreparationHash[0] ^= 0xff },
		func(value *tammyv1.BasValidationSnapshot) { value.CalculationHash[0] ^= 0xff },
		func(value *tammyv1.BasValidationSnapshot) { value.InputRevision++ },
		func(value *tammyv1.BasValidationSnapshot) { value.SourceSnapshotHash[0] ^= 0xff },
	} {
		changed := proto.Clone(validation).(*tammyv1.BasValidationSnapshot)
		change(changed)
		if _, _, err := seal(changed); err == nil {
			t.Fatal("validation accepted another historical input owner")
		}
	}
	corruptOwner := proto.Clone(owner).(*tammyv1.BasPreparation)
	corruptOwner.OrganisationId = testUUID(3302)
	if _, _, err := sealValidation(validation, corruptOwner); err == nil {
		t.Fatal("unverified owner scope was accepted")
	}
}

func TestSnapshotAcknowledgementHashesWholeBodyAndBindsExactWarning(t *testing.T) {
	owner, validation := retainedSnapshotReview(t)
	wire, hash, err := sealValidation(validation, owner)
	if err != nil {
		t.Fatal(err)
	}
	validation, err = openValidation(wire, owner.OrganisationId, &tammyv1.SourceRef{Type: "bas_validation", Id: validation.ValidationId, Revision: validation.Revision, ContentHash: hash}, owner)
	if err != nil {
		t.Fatal(err)
	}
	ack := acknowledgement(validation, validation.Findings[0])
	ack.Validation.ContentHash = bytes.Clone(hash)
	seal := func(value *tammyv1.BasWarningAcknowledgement) ([]byte, []byte, error) {
		return sealAcknowledgement(value, validation, owner)
	}
	open := func(wire []byte, org string, ref *tammyv1.SourceRef) (*tammyv1.BasWarningAcknowledgement, error) {
		return openAcknowledgement(wire, org, ref, validation, owner)
	}
	assertTypedSnapshotRoundTrip(t, ack, ack.AcknowledgementId, owner.OrganisationId, "bas_warning_acknowledgement", 1, seal, open)
	ackWire, ackHash, err := seal(ack)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(ackWire)
	if !bytes.Equal(ackHash, want[:]) {
		t.Fatal("a message without its own hash must hash every retained byte")
	}
	for _, change := range []func(*tammyv1.BasWarningAcknowledgement){
		func(value *tammyv1.BasWarningAcknowledgement) { value.Validation.ContentHash[0] ^= 0xff },
		func(value *tammyv1.BasWarningAcknowledgement) { value.Finding.ContentHash[0] ^= 0xff },
		func(value *tammyv1.BasWarningAcknowledgement) { value.Finding.Revision++ },
		func(value *tammyv1.BasWarningAcknowledgement) { value.SourceSnapshotHash[0] ^= 0xff },
	} {
		changed := proto.Clone(ack).(*tammyv1.BasWarningAcknowledgement)
		change(changed)
		if _, _, err := seal(changed); err == nil {
			t.Fatal("acknowledgement accepted an unbound warning")
		}
	}
	blocker := proto.Clone(validation).(*tammyv1.BasValidationSnapshot)
	blocker.ContentHash = nil
	blocker.Findings[0].Severity = tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER
	blockerWire, blockerHash, err := sealValidation(blocker, owner)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err = openValidation(blockerWire, owner.OrganisationId, &tammyv1.SourceRef{Type: "bas_validation", Id: blocker.ValidationId, Revision: blocker.Revision, ContentHash: blockerHash}, owner)
	if err != nil {
		t.Fatal(err)
	}
	blockerAck := acknowledgement(blocker, blocker.Findings[0])
	blockerAck.Validation.ContentHash = blockerHash
	if _, _, err := sealAcknowledgement(blockerAck, blocker, owner); err == nil {
		t.Fatal("retained acknowledgement waived a blocker")
	}
}

func TestSnapshotResolutionBindsVerifiedFamilyWhileRetainingAffectedChain(t *testing.T) {
	input := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN)
	identity := &tammyv1.BasFamilyIdentity{FamilyId: input.Chain.Family.Reference.Id, OrganisationId: input.Chain.Family.Scope.OrganisationID,
		PeriodScope: &tammyv1.BasCivilDateRange{StartDate: input.Chain.Family.Scope.Start, EndDate: input.Chain.Family.Scope.End},
		Window:      input.Chain.Family.Scope.Window, CreatedAt: input.History.Records[0].Observation.RecordedAt}
	wire, hash, err := sealFamilyIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	familyRef := &tammyv1.SourceRef{Type: "bas_family", Id: identity.FamilyId, Revision: 1, ContentHash: hash}
	identity, err = openFamilyIdentity(wire, identity.OrganisationId, familyRef)
	if err != nil {
		t.Fatal(err)
	}
	value := &tammyv1.BasExternalDisputeResolution{ResolutionId: testUUID(3401), Family: familyRef, Resolution: input.Resolution,
		ActorUserId: testUUID(3402), ResolvedAt: input.History.Records[0].Observation.RecordedAt}
	seal := func(value *tammyv1.BasExternalDisputeResolution) ([]byte, []byte, error) {
		return sealResolution(value, identity)
	}
	open := func(wire []byte, org string, ref *tammyv1.SourceRef) (*tammyv1.BasExternalDisputeResolution, error) {
		return openResolution(wire, org, ref, identity)
	}
	assertTypedSnapshotRoundTrip(t, value, value.ResolutionId, identity.OrganisationId, "bas_external_dispute_resolution", 1, seal, open)
	changed := proto.Clone(value).(*tammyv1.BasExternalDisputeResolution)
	changed.Family.ContentHash[0] ^= 0xff
	if _, _, err := seal(changed); err == nil {
		t.Fatal("resolution accepted another retained family identity")
	}
}

func TestSnapshotExportEventRetainsExactTargetAndCanonicalOutcome(t *testing.T) {
	prep := readyReviewInput().Preparation
	value := &tammyv1.BasExportEvent{ExportEventId: testUUID(3501), OrganisationId: prep.OrganisationId,
		Event: &tammyv1.BasExportEventInput{Target: &tammyv1.BasExportTarget{ExactTarget: &tammyv1.BasExportTarget_Calculation{Calculation: prep.CurrentCalculation}},
			Format: tammyv1.BasExportFormat_BAS_EXPORT_FORMAT_CSV, Phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, OccurredAt: prep.CreatedAt},
		ActorUserId: testUUID(3502), RecordedAt: prep.UpdatedAt}
	assertTypedSnapshotRoundTrip(t, value, value.ExportEventId, value.OrganisationId, "bas_export_event", 1, sealExport, openExport)
	value.Event.Target.GetCalculation().Type = "bas_preparation"
	if _, _, err := sealExport(value); err == nil {
		t.Fatal("export accepted an inexact mutable target")
	}
}
