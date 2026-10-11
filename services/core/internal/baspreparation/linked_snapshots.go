package baspreparation

import (
	"bytes"
	"crypto/sha256"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func verifiedPreparation(value *tammyv1.BasPreparation) bool {
	if value == nil || len(value.ContentHash) != sha256.Size {
		return false
	}
	_, _, err := sealPreparation(value)
	return err == nil
}

func validationSnapshotSpec(owner *tammyv1.BasPreparation) snapshotSpec[*tammyv1.BasValidationSnapshot] {
	return snapshotSpec[*tammyv1.BasValidationSnapshot]{
		resource: "bas_validation", maxBytes: 1024 * 1024,
		newMessage: func() *tammyv1.BasValidationSnapshot { return &tammyv1.BasValidationSnapshot{} },
		identity: func(value *tammyv1.BasValidationSnapshot) (string, string, uint64) {
			return value.ValidationId, owner.GetOrganisationId(), value.Revision
		},
		getHash: func(value *tammyv1.BasValidationSnapshot) []byte { return value.ContentHash },
		setHash: func(value *tammyv1.BasValidationSnapshot, hash []byte) { value.ContentHash = hash },
		bind: func(value *tammyv1.BasValidationSnapshot) bool {
			return verifiedPreparation(owner) && value.PreparationId == owner.PreparationId &&
				bytes.Equal(value.PreparationHash, owner.ContentHash) && owner.CurrentCalculation != nil &&
				owner.CurrentCalculation.Type == "bas_calculation" && value.CalculationId == owner.CurrentCalculation.Id &&
				bytes.Equal(value.CalculationHash, owner.CurrentCalculation.ContentHash) &&
				value.InputRevision == owner.InputRevision && bytes.Equal(value.InputHash, owner.InputHash) &&
				bytes.Equal(value.SourceSnapshotHash, owner.SourceSnapshotHash)
		},
	}
}

func sealValidation(value *tammyv1.BasValidationSnapshot, owner *tammyv1.BasPreparation) ([]byte, []byte, error) {
	return sealSnapshot(value, validationSnapshotSpec(owner))
}
func openValidation(wire []byte, org string, exact *tammyv1.SourceRef, owner *tammyv1.BasPreparation) (*tammyv1.BasValidationSnapshot, error) {
	return openSnapshot(wire, org, exact, validationSnapshotSpec(owner))
}

func acknowledgementSnapshotSpec(validation *tammyv1.BasValidationSnapshot, owner *tammyv1.BasPreparation) snapshotSpec[*tammyv1.BasWarningAcknowledgement] {
	return snapshotSpec[*tammyv1.BasWarningAcknowledgement]{
		resource: "bas_warning_acknowledgement", maxBytes: scalarSnapshotLimit, externalHash: true,
		newMessage: func() *tammyv1.BasWarningAcknowledgement { return &tammyv1.BasWarningAcknowledgement{} },
		identity: func(value *tammyv1.BasWarningAcknowledgement) (string, string, uint64) {
			return value.AcknowledgementId, owner.GetOrganisationId(), 1
		},
		getHash: func(*tammyv1.BasWarningAcknowledgement) []byte { return nil },
		setHash: func(*tammyv1.BasWarningAcknowledgement, []byte) {},
		bind: func(value *tammyv1.BasWarningAcknowledgement) bool {
			if validation == nil || len(validation.ContentHash) != sha256.Size {
				return false
			}
			_, hash, err := sealValidation(validation, owner)
			if err != nil || !sameRef(value.Validation, &tammyv1.SourceRef{Type: "bas_validation", Id: validation.ValidationId, Revision: validation.Revision, ContentHash: hash}) ||
				!bytes.Equal(value.SourceSnapshotHash, validation.SourceSnapshotHash) {
				return false
			}
			matches := 0
			for _, finding := range validation.Findings {
				if finding != nil && sameRef(value.Finding, &tammyv1.SourceRef{Type: "bas_validation_finding", Id: finding.FindingId, Revision: validation.Revision, ContentHash: canonicalFindingHash(finding)}) {
					if finding.Severity != tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING {
						return false
					}
					matches++
				}
			}
			return matches == 1
		},
	}
}

func sealAcknowledgement(value *tammyv1.BasWarningAcknowledgement, validation *tammyv1.BasValidationSnapshot, owner *tammyv1.BasPreparation) ([]byte, []byte, error) {
	return sealSnapshot(value, acknowledgementSnapshotSpec(validation, owner))
}
func openAcknowledgement(wire []byte, org string, exact *tammyv1.SourceRef, validation *tammyv1.BasValidationSnapshot, owner *tammyv1.BasPreparation) (*tammyv1.BasWarningAcknowledgement, error) {
	return openSnapshot(wire, org, exact, acknowledgementSnapshotSpec(validation, owner))
}

func resolutionSnapshotSpec(identity *tammyv1.BasFamilyIdentity) snapshotSpec[*tammyv1.BasExternalDisputeResolution] {
	return snapshotSpec[*tammyv1.BasExternalDisputeResolution]{
		resource: "bas_external_dispute_resolution", maxBytes: 128 * 1024,
		newMessage: func() *tammyv1.BasExternalDisputeResolution { return &tammyv1.BasExternalDisputeResolution{} },
		identity: func(value *tammyv1.BasExternalDisputeResolution) (string, string, uint64) {
			return value.ResolutionId, identity.GetOrganisationId(), 1
		},
		getHash: func(value *tammyv1.BasExternalDisputeResolution) []byte { return value.ContentHash },
		setHash: func(value *tammyv1.BasExternalDisputeResolution, hash []byte) { value.ContentHash = hash },
		bind: func(value *tammyv1.BasExternalDisputeResolution) bool {
			if identity == nil || len(identity.ContentHash) != sha256.Size {
				return false
			}
			_, hash, err := sealFamilyIdentity(identity)
			return err == nil && sameRef(value.Family, &tammyv1.SourceRef{Type: "bas_family", Id: identity.FamilyId, Revision: 1, ContentHash: hash})
		},
	}
}

func sealResolution(value *tammyv1.BasExternalDisputeResolution, identity *tammyv1.BasFamilyIdentity) ([]byte, []byte, error) {
	return sealSnapshot(value, resolutionSnapshotSpec(identity))
}
func openResolution(wire []byte, org string, exact *tammyv1.SourceRef, identity *tammyv1.BasFamilyIdentity) (*tammyv1.BasExternalDisputeResolution, error) {
	return openSnapshot(wire, org, exact, resolutionSnapshotSpec(identity))
}

var exportSnapshotSpec = snapshotSpec[*tammyv1.BasExportEvent]{
	resource: "bas_export_event", maxBytes: 128 * 1024,
	newMessage: func() *tammyv1.BasExportEvent { return &tammyv1.BasExportEvent{} },
	identity: func(value *tammyv1.BasExportEvent) (string, string, uint64) {
		return value.ExportEventId, value.OrganisationId, 1
	},
	getHash: func(value *tammyv1.BasExportEvent) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasExportEvent, hash []byte) { value.ContentHash = hash },
}

func sealExport(value *tammyv1.BasExportEvent) ([]byte, []byte, error) {
	return sealSnapshot(value, exportSnapshotSpec)
}
func openExport(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasExportEvent, error) {
	return openSnapshot(wire, org, exact, exportSnapshotSpec)
}
