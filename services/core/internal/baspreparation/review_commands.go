package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func captureBASReviewCurrentness(ctx context.Context, repos CommandRepositories,
	owner *PreparationRetainedSnapshot, calculation *tammyv1.BasCalculation) (CurrentCapture, error) {
	if repos.CaptureCurrent == nil || owner == nil || owner.Preparation == nil || calculation == nil {
		return CurrentCapture{}, ErrCommandUnavailable
	}
	capture, err := repos.CaptureCurrent(ctx, owner, calculation)
	if err != nil {
		return CurrentCapture{}, err
	}
	prep := owner.Preparation
	if !validCapture(capture) || capture.InputRevision != prep.InputRevision ||
		!bytes.Equal(capture.InputHash, prep.InputHash) ||
		!bytes.Equal(capture.SourceSnapshotHash, prep.SourceSnapshotHash) ||
		!bytes.Equal(capture.SourceSnapshotHash, calculation.SourceSnapshotHash) {
		// A stale capture requires regeneration. Do not persist a validation
		// against a source snapshot that was not captured by this calculation.
		return CurrentCapture{}, ErrCommandConflict
	}
	return capture, nil
}

func basPreparationMutableForValidation(state tammyv1.BasPreparationState) bool {
	switch state {
	case tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY:
		return true
	default:
		return false
	}
}

// ValidateBasPreparation reevaluates an exact calculation only after atomically
// recapturing current source identity. Findings are copied from the immutable
// calculation, retaining their stable codes and exact source links.
func (service *Service) ValidateBasPreparation(ctx context.Context,
	request *tammyv1.ValidateBasPreparationRequest) (*tammyv1.ValidateBasPreparationResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.PreparationId) ||
		len(request.PreparationHash) != 32 || request.ExpectedVersion == 0 ||
		request.ExpectedVersion >= math.MaxInt64 || !ids.IsCanonicalV7(request.CalculationId) ||
		len(request.CalculationHash) != 32 {
		return nil, ErrInvalidCommand
	}

	var response *tammyv1.ValidateBasPreparationResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationValidateBasPreparation,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.ValidateBasPreparationResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.ValidateBasPreparationRequest)
			if !ok || repos.Preparations == nil || repos.Calculations == nil || repos.Validations == nil {
				return ErrInvalidCommand
			}
			current, err := repos.Preparations.GetCurrent(ctx, r.OrganisationId, r.PreparationId)
			if err != nil || current == nil || current.Preparation == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			prep := current.Preparation
			prepRef := preparationReference(prep)
			if prep.Version != r.ExpectedVersion || !bytes.Equal(prep.ContentHash, r.PreparationHash) ||
				!basPreparationMutableForValidation(prep.State) || prep.CurrentCalculation == nil ||
				current.SavedPaygInputs == nil ||
				prep.CurrentCalculation.Id != r.CalculationId ||
				!bytes.Equal(prep.CurrentCalculation.ContentHash, r.CalculationHash) {
				return ErrCommandConflict
			}

			calculation, err := repos.Calculations.GetExact(ctx, r.OrganisationId, prep.CurrentCalculation)
			if err != nil || calculation == nil ||
				!sameBASSourceRef(calculationReference(calculation), prep.CurrentCalculation) {
				return errors.Join(ErrCommandConflict, err)
			}
			currentCapture, err := captureBASReviewCurrentness(ctx, repos, current, calculation)
			if err != nil {
				return err
			}

			validationID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(validationID) {
				return ErrInvalidCommand
			}
			if len(calculation.Findings) > validationFindingMaximum {
				return ErrInvalidCommand
			}
			findings := make([]*tammyv1.BasValidationFinding, 0, len(calculation.Findings))
			for _, finding := range calculation.Findings {
				if finding == nil {
					return ErrInvalidCommand
				}
				findings = append(findings, cloneFinding(finding))
			}
			validation := &tammyv1.BasValidationSnapshot{
				ValidationId: validationID, Revision: 1,
				PreparationId: prep.PreparationId, PreparationHash: bytes.Clone(prepRef.ContentHash),
				CalculationId: calculation.CalculationId, CalculationHash: bytes.Clone(calculation.ContentHash),
				SourceSnapshotHash: bytes.Clone(currentCapture.SourceSnapshotHash),
				InputRevision:      prep.InputRevision, InputHash: bytes.Clone(prep.InputHash),
				Findings: findings, CreatedAt: timestamppb.New(service.now().UTC()),
			}
			validationRef, err := repos.Validations.Append(ctx, r.OrganisationId, validation, prepRef)
			if err != nil {
				return err
			}
			validation.ContentHash = bytes.Clone(validationRef.ContentHash)

			// AssessReview requires the exact current validation pointer. Build a
			// provisional collecting successor with a valid deterministic hash;
			// the final state is then derived from the pure retained-review policy.
			next := proto.Clone(prep).(*tammyv1.BasPreparation)
			next.Version++
			next.ContentHash = nil
			next.CurrentValidation = cloneBASRef(validationRef)
			next.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
			next.UpdatedAt = timestamppb.New(service.now().UTC())
			_, provisionalHash, err := sealPreparation(next)
			if err != nil {
				return errors.Join(ErrInvalidCommand, err)
			}
			next.ContentHash = bytes.Clone(provisionalHash)
			assessment, err := AssessReview(ReviewInput{
				Preparation: next, Calculation: calculation, Validation: validation,
				CurrentCapture: currentCapture,
			})
			if err != nil {
				return errors.Join(ErrInvalidCommand, err)
			}
			next.State = assessment.State
			next.ContentHash = nil
			retained := &PreparationRetainedSnapshot{
				Preparation: next, SavedPaygInputs: cloneBASRef(current.SavedPaygInputs),
				BranchID: cloneBASString(current.BranchID), AttemptID: current.AttemptID,
			}
			nextRef, err := repos.Preparations.Append(ctx, retained, prepRef)
			if err != nil {
				return err
			}
			if repos.AdvancePreparation == nil {
				return ErrCommandUnavailable
			}
			if err := repos.AdvancePreparation(ctx, r.OrganisationId, prep.Family, prepRef, nextRef, next.UpdatedAt); err != nil {
				return err
			}
			next.ContentHash = bytes.Clone(nextRef.ContentHash)
			response = &tammyv1.ValidateBasPreparationResponse{Preparation: next, Validation: validation}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_VALIDATION_RECORDED,
				Source: cloneBASRef(nextRef), Previous: cloneBASRef(prepRef),
				AffectedResources: []*tammyv1.SourceRef{
					cloneBASRef(prep.CurrentCalculation), cloneBASRef(validationRef),
				},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Preparation == nil || response.Validation == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationValidateBasPreparation,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Preparation.PreparationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.ValidateBasPreparationResponse)
	if !ok || value.Preparation == nil || value.Validation == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

// AcknowledgeBasWarnings writes only exact warnings from the current validation
// and binds every acknowledgement to that validation's source snapshot. The
// retained review policy rejects blocker states before any acknowledgement is
// appended; warnings can never alter or downgrade blocker severity.
func (service *Service) AcknowledgeBasWarnings(ctx context.Context,
	request *tammyv1.AcknowledgeBasWarningsRequest) (*tammyv1.AcknowledgeBasWarningsResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.PreparationId) ||
		len(request.PreparationHash) != 32 || request.ExpectedVersion == 0 ||
		request.ExpectedVersion >= math.MaxInt64 || !ids.IsCanonicalV7(request.ValidationId) ||
		len(request.ValidationHash) != 32 || len(request.SourceSnapshotHash) != 32 ||
		len(request.Warnings) == 0 || len(request.Warnings) > warningAcknowledgementMaximum {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.AcknowledgeBasWarningsResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationAcknowledgeBasWarnings,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.AcknowledgeBasWarningsResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.AcknowledgeBasWarningsRequest)
			if !ok || repos.Preparations == nil || repos.Calculations == nil ||
				repos.Validations == nil || repos.Acknowledgements == nil {
				return ErrInvalidCommand
			}
			current, err := repos.Preparations.GetCurrent(ctx, r.OrganisationId, r.PreparationId)
			if err != nil || current == nil || current.Preparation == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			prep := current.Preparation
			prepRef := preparationReference(prep)
			validationRef := prep.CurrentValidation
			if prep.Version != r.ExpectedVersion || !bytes.Equal(prep.ContentHash, r.PreparationHash) ||
				prep.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
				prep.CurrentCalculation == nil || validationRef == nil || current.SavedPaygInputs == nil ||
				validationRef.Id != r.ValidationId || !bytes.Equal(validationRef.ContentHash, r.ValidationHash) {
				return ErrCommandConflict
			}

			calculation, err := repos.Calculations.GetExact(ctx, r.OrganisationId, prep.CurrentCalculation)
			if err != nil || calculation == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			currentCapture, err := captureBASReviewCurrentness(ctx, repos, current, calculation)
			if err != nil {
				return err
			}
			if !bytes.Equal(r.SourceSnapshotHash, currentCapture.SourceSnapshotHash) {
				return ErrCommandConflict
			}
			retainedValidation, err := repos.Validations.GetRetained(ctx, r.OrganisationId, validationRef)
			if err != nil || retainedValidation == nil || retainedValidation.Validation == nil ||
				!sameBASSourceRef(retainedValidation.Calculation, prep.CurrentCalculation) ||
				!warningAcknowledgementCurrentCaptureMatches(current, retainedValidation) {
				return errors.Join(ErrCommandConflict, err)
			}
			validation := retainedValidation.Validation
			existing, err := repos.Acknowledgements.ListForValidation(ctx, r.OrganisationId, validationRef)
			if err != nil {
				return err
			}
			if len(existing)+len(r.Warnings) > warningAcknowledgementMaximum {
				return ErrCommandConflict
			}
			assessment, err := AssessReview(ReviewInput{
				Preparation: prep, Calculation: calculation, Validation: validation,
				Acknowledgements: existing, CurrentCapture: currentCapture,
			})
			if err != nil || assessment.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
				assessment.SourceChanged {
				return errors.Join(ErrCommandConflict, err)
			}

			findings := make(map[string]*tammyv1.BasValidationFinding, len(validation.Findings))
			for _, finding := range validation.Findings {
				if finding != nil {
					findings[finding.FindingId] = finding
				}
			}
			alreadyAcknowledged := make(map[string]bool, len(existing))
			for _, acknowledgement := range existing {
				alreadyAcknowledged[acknowledgement.Finding.Id] = true
			}
			seen := make(map[string]bool, len(r.Warnings))
			for _, warning := range r.Warnings {
				if warning == nil || !ids.IsCanonicalV7(warning.FindingId) || len(warning.FindingHash) != 32 {
					return ErrInvalidCommand
				}
				finding := findings[warning.FindingId]
				if finding == nil || finding.Severity != tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING ||
					!bytes.Equal(warning.FindingHash, canonicalFindingHash(finding)) ||
					seen[warning.FindingId] || alreadyAcknowledged[warning.FindingId] {
					return ErrCommandConflict
				}
				seen[warning.FindingId] = true
			}

			acknowledgedAt := timestamppb.New(service.now().UTC())
			acknowledgements := make([]*tammyv1.BasWarningAcknowledgement, 0, len(r.Warnings))
			for _, warning := range r.Warnings {
				finding := findings[warning.FindingId]
				ackID, err := service.newID()
				if err != nil || !ids.IsCanonicalV7(ackID) {
					return ErrInvalidCommand
				}
				acknowledgement := &tammyv1.BasWarningAcknowledgement{
					AcknowledgementId: ackID, Validation: cloneBASRef(validationRef),
					Finding: &tammyv1.SourceRef{
						Type: "bas_validation_finding", Id: finding.FindingId, Revision: validation.Revision,
						ContentHash: bytes.Clone(warning.FindingHash),
					},
					SourceSnapshotHash: bytes.Clone(validation.SourceSnapshotHash),
					ActorUserId:        r.CommandContext.Authentication.ActorUserId,
					AcknowledgedAt:     proto.Clone(acknowledgedAt).(*timestamppb.Timestamp),
				}
				_, err = repos.Acknowledgements.Append(ctx, r.OrganisationId, acknowledgement, prepRef)
				if err != nil {
					return err
				}
				acknowledgements = append(acknowledgements, acknowledgement)
			}
			response = &tammyv1.AcknowledgeBasWarningsResponse{
				Acknowledgements: acknowledgements, Preparation: proto.Clone(prep).(*tammyv1.BasPreparation),
			}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_WARNINGS_ACKNOWLEDGED,
				Source: cloneBASRef(prepRef), AffectedResources: []*tammyv1.SourceRef{cloneBASRef(validationRef), cloneBASRef(prep.CurrentCalculation)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Preparation == nil || len(response.Acknowledgements) == 0 || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationAcknowledgeBasWarnings,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Preparation.PreparationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.AcknowledgeBasWarningsResponse)
	if !ok || value.Preparation == nil || len(value.Acknowledgements) == 0 {
		return nil, ErrInvalidCommand
	}
	return value, nil
}
