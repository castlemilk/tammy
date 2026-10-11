package baspreparation

import (
	"bytes"
	"context"
)

// A preparation append verifies the complete retained validation, all findings
// and exact historical parents through the same reader used by query commands.
func (repository *PreparationRepository) verifyValidationProof(ctx context.Context, retained *PreparationRetainedSnapshot) error {
	prep := retained.Preparation
	validations, err := NewValidationRepository(repository.executor, repository.reader.cursors)
	if err != nil {
		return err
	}
	proof, err := validations.GetRetained(ctx, prep.OrganisationId, prep.CurrentValidation)
	if err != nil {
		return err
	}
	if proof.Preparation.Id != prep.PreparationId || proof.Preparation.Revision >= prep.Version ||
		!sameBASSourceRef(proof.Family, prep.Family) || !sameBASSourceRef(proof.Calculation, prep.CurrentCalculation) ||
		!sameBASSourceRef(proof.SavedPaygInputs, retained.SavedPaygInputs) || proof.Validation.InputRevision != prep.InputRevision ||
		!bytes.Equal(proof.Validation.InputHash, prep.InputHash) || !bytes.Equal(proof.Validation.SourceSnapshotHash, prep.SourceSnapshotHash) {
		return ErrSnapshotCorrupt
	}
	return nil
}
