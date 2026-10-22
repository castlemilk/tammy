package baspreparation

// Draft for services/core/internal/baspreparation/ordinary_commands.go.
// This file stays inside baspreparation so it can use its exact input hash and
// immutable snapshot helpers. The adapter that supplies CommandRepositories
// must construct every repository over the command coordinator's tx.

import (
	"bytes"
	"context"
	"errors"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrInvalidCommand     = errors.New("baspreparation: invalid command")
	ErrCommandConflict    = errors.New("baspreparation: command expected version conflicts with current state")
	ErrCommandUnavailable = errors.New("baspreparation: required regeneration component unavailable")
)

type CommandRepositories struct {
	CreateExternalAttempt BASExternalAttemptCreator
	ExternalHistory       *BASExternalHistoryReader
	ExternalObservations  *ExternalObservationRepository
	Resolutions           *DisputeResolutionRepository
	ExportEvents          *ExportEventRepository
	Profiles              *ProfileRepository
	Periods               *PeriodRepository
	Preparations          *PreparationRepository
	SavedPayg             *SavedPaygRepository
	Regenerate            func(context.Context, *tammyv1.RegenerateBasPreparationRequest, string, *timestamppb.Timestamp) (*RegenerationResult, error)
	Calculations          *CalculationRepository
	Validations           *ValidationRepository
	Acknowledgements      *WarningAcknowledgementRepository
	AdvancePreparation    BASPreparationWorkflowAdvance
	IncomeYearPins        *IncomeYearPinRepository
	CaptureCurrent        BASCurrentnessCapture
	Frozen                *FrozenRepository
	FamilyIdentities      *FamilyIdentityRepository
	FamilyStates          *FamilyStateRepository
	BranchIdentities      *BranchIdentityRepository
	BranchStates          *BranchStateRepository
	FreezeChains          *FreezeChainReader
	ConsumeFreshFactor    func(context.Context, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error
	// Authorization is created by the SQLCipher transaction starter. It is
	// never derived from fields in a BAS payload.
	Authorize func(context.Context, *tammyv1.AuthenticationContext, authorisation.Action) error
}

type CommandRunner interface {
	Execute(context.Context, app.OrdinaryCommand[CommandRepositories]) (proto.Message, error)
}

type AuditFactory interface {
	Build(context.Context, app.OrdinaryOperation, *tammyv1.AuthenticationContext,
		string, string, proto.Message, proto.Message) (app.CommandResult, error)
}

type RegenerationResult struct {
	Response *tammyv1.RegenerateBasPreparationResponse
	Source   *tammyv1.SourceRef
	Previous *tammyv1.SourceRef
	Affected []*tammyv1.SourceRef
}

type Service struct {
	commands CommandRunner
	audit    AuditFactory
	now      func() time.Time
	newID    func() (string, error)
}

func NewService(commands CommandRunner, audit AuditFactory, now func() time.Time,
	newID func() (string, error)) (*Service, error) {
	if commands == nil || audit == nil || now == nil || newID == nil {
		return nil, ErrInvalidCommand
	}
	return &Service{commands: commands, audit: audit, now: now, newID: newID}, nil
}

func validBASCommandContext(value *tammyv1.CommandContext) bool {
	return value != nil && value.Authentication != nil &&
		ids.IsCanonicalV7(value.IdempotencyKey) &&
		ids.IsCanonicalV7(value.Authentication.ActorUserId) &&
		ids.IsCanonicalV7(value.Authentication.SessionId)
}

func (service *Service) SetBasObligationsProfile(ctx context.Context,
	request *tammyv1.SetBasObligationsProfileRequest) (*tammyv1.SetBasObligationsProfileResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || request.Profile == nil ||
		request.ExpectedVersion >= math.MaxInt64 {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.SetBasObligationsProfileResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationSetBasObligationsProfile,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.SetBasObligationsProfileResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.SetBasObligationsProfileRequest)
			if !ok || repos.Profiles == nil {
				return ErrInvalidCommand
			}
			current, currentRef, err := repos.Profiles.CurrentForOrganisation(ctx, r.OrganisationId)
			if err != nil {
				return err
			}
			if (current == nil && r.ExpectedVersion != 0) ||
				(current != nil && (r.ExpectedVersion == 0 || current.Revision != r.ExpectedVersion)) {
				return ErrCommandConflict
			}
			profileID, version := "", uint64(1)
			var previous *tammyv1.SourceRef
			if current == nil {
				profileID, err = service.newID()
				if err != nil || !ids.IsCanonicalV7(profileID) {
					return ErrInvalidCommand
				}
			} else {
				if !validBASProfileIntervalSuccessor(current.Profile.GetEffectiveInterval(), r.Profile.GetEffectiveInterval()) {
					return ErrCommandConflict
				}
				profileID, version, previous = current.ProfileId, current.Revision+1, currentRef
			}
			value := &tammyv1.BasObligationsProfile{
				ProfileId: profileID, OrganisationId: r.OrganisationId, Revision: version,
				Profile:         proto.Clone(r.Profile).(*tammyv1.BasObligationsProfileInput),
				PreviousProfile: cloneBASRef(previous), ActorUserId: r.CommandContext.Authentication.ActorUserId,
				RecordedAt: timestamppb.New(service.now().UTC()),
			}
			if repos.IncomeYearPins == nil {
				return ErrInvalidCommand
			}
			if err := repos.IncomeYearPins.CheckProfileCompatible(ctx, r.OrganisationId, r.Profile); err != nil {
				if errors.Is(err, ErrIncomeYearPinConflict) {
					return errors.Join(ErrCommandConflict, err)
				}
				return err
			}
			ref, err := repos.Profiles.Append(ctx, value, currentRef)
			if err != nil {
				return err
			}
			value.ContentHash = bytes.Clone(ref.ContentHash)
			response = &tammyv1.SetBasObligationsProfileResponse{Profile: value}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PROFILE_SET,
				Source: cloneBASRef(ref), Previous: cloneBASRef(previous),
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Profile == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationSetBasObligationsProfile,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Profile.ProfileId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.SetBasObligationsProfileResponse)
	if !ok || value.Profile == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func (service *Service) ConfirmBasPeriod(ctx context.Context,
	request *tammyv1.ConfirmBasPeriodRequest) (*tammyv1.ConfirmBasPeriodResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || request.Period == nil ||
		request.Period.Period == nil || request.ExpectedVersion >= math.MaxInt64 {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.ConfirmBasPeriodResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationConfirmBasPeriod,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.ConfirmBasPeriodResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.ConfirmBasPeriodRequest)
			if !ok || repos.Periods == nil || repos.Profiles == nil {
				return ErrInvalidCommand
			}
			profile, effectiveProfile, err := repos.Profiles.GetEffectiveForPeriod(ctx, r.OrganisationId, r.Period.Period)
			if err != nil || !sameBASSourceRef(effectiveProfile, r.Period.Profile) {
				return errors.Join(ErrCommandConflict, err)
			}
			if err := validateBASPeriodGuidance(r.Period); err != nil {
				return errors.Join(ErrCommandConflict, err)
			}
			current, currentRef, err := repos.Periods.CurrentForScope(ctx, r.OrganisationId, r.Period)
			if err != nil {
				return err
			}
			if (current == nil && r.ExpectedVersion != 0) ||
				(current != nil && (r.ExpectedVersion == 0 || current.Version != r.ExpectedVersion)) {
				return ErrCommandConflict
			}
			periodID, version := "", uint64(1)
			var previous *tammyv1.SourceRef
			if current == nil {
				periodID, err = service.newID()
				if err != nil || !ids.IsCanonicalV7(periodID) {
					return ErrInvalidCommand
				}
			} else {
				periodID, version = current.PeriodId, current.Version+1
				previous = currentRef
			}
			value := &tammyv1.BasConfirmedPeriod{
				PeriodId: periodID, OrganisationId: r.OrganisationId, Version: version,
				Confirmation: proto.Clone(r.Period).(*tammyv1.BasPeriodConfirmationInput),
				ActorUserId:  r.CommandContext.Authentication.ActorUserId,
				ConfirmedAt:  timestamppb.New(service.now().UTC()),
			}
			assessment := AssessBASProfilePeriod(profile, value.Confirmation.Period, value.Confirmation.Window)
			value.Confirmation.SupportState = assessment.SupportState
			value.Confirmation.SupportFindings = assessment.Findings
			if len(value.Confirmation.DueDateEvidence) == 0 {
				value.Confirmation.SupportFindings = append(value.Confirmation.SupportFindings, &tammyv1.BasPeriodSupportFinding{
					StableCode: "ACTUAL_DUE_DATE_UNCONFIRMED", SafeMessage: "Retain the actual statement due-date evidence before relying on this period for a complete statement.",
					Sources: []*tammyv1.SourceRef{cloneBASRef(effectiveProfile)},
				})
				if value.Confirmation.SupportState == tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED {
					value.Confirmation.SupportState = tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_OTHER_UNSUPPORTED
				}
			}
			ref, err := repos.Periods.Append(ctx, value, currentRef)
			if err != nil {
				return err
			}
			if repos.IncomeYearPins == nil {
				return ErrInvalidCommand
			}
			if err := repos.IncomeYearPins.PinAfterConfirmedPeriod(ctx, r.OrganisationId, ref); err != nil {
				if errors.Is(err, ErrIncomeYearPinConflict) {
					return errors.Join(ErrCommandConflict, err)
				}
				return err
			}
			value.ContentHash = bytes.Clone(ref.ContentHash)
			response = &tammyv1.ConfirmBasPeriodResponse{Period: value}
			auditPayload = PeriodAuditPayload(ref, previous)
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Period == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationConfirmBasPeriod,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Period.PeriodId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.ConfirmBasPeriodResponse)
	if !ok || value.Period == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func (service *Service) SaveBasPaygInputs(ctx context.Context,
	request *tammyv1.SaveBasPaygInputsRequest) (*tammyv1.SaveBasPaygInputsResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.PreparationId) ||
		len(request.PreparationHash) != 32 || request.ExpectedVersion == 0 ||
		request.ExpectedVersion >= math.MaxInt64 || request.Paygw == nil || request.Paygi == nil {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.SaveBasPaygInputsResponse
	var payload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationSaveBasPaygInputs,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.SaveBasPaygInputsResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.SaveBasPaygInputsRequest)
			if !ok || repos.Preparations == nil || repos.SavedPayg == nil {
				return ErrInvalidCommand
			}
			current, err := repos.Preparations.GetCurrent(ctx, r.OrganisationId, r.PreparationId)
			if err != nil {
				return err
			}
			prep := current.Preparation
			currentRef := preparationReference(prep)
			if prep.Version != r.ExpectedVersion || !bytes.Equal(prep.ContentHash, r.PreparationHash) {
				return ErrCommandConflict
			}
			if prep.FrozenBas != nil || prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF ||
				prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
				prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
				prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION ||
				prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION {
				return ErrCommandConflict
			}
			inputHash, err := hashPaygInputs(r.Paygw, r.Paygi)
			if err != nil {
				return err
			}

			// Repeating the same pair preserves its immutable saved envelope and
			// preparation pointer while still producing an ordinary audited command.
			if bytes.Equal(inputHash, prep.InputHash) && current.SavedPaygInputs != nil {
				response = &tammyv1.SaveBasPaygInputsResponse{Preparation: proto.Clone(prep).(*tammyv1.BasPreparation)}
				payload = &tammyv1.BasPreparationChangedEvent{
					Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PAYG_INPUTS_SAVED,
					Source: cloneBASRef(currentRef), AffectedResources: []*tammyv1.SourceRef{cloneBASRef(current.SavedPaygInputs)},
				}
				return nil
			}

			working := current
			workingRef := currentRef
			inputRevision := prep.InputRevision
			if !bytes.Equal(inputHash, prep.InputHash) {
				next := proto.Clone(prep).(*tammyv1.BasPreparation)
				next.Version++
				next.ContentHash = nil
				next.InputRevision++
				next.InputHash = bytes.Clone(inputHash)
				next.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
				next.CurrentCalculation = nil
				next.CurrentValidation = nil
				next.FrozenBas = nil
				next.UpdatedAt = timestamppb.New(service.now().UTC())
				retained := &PreparationRetainedSnapshot{Preparation: next, BranchID: cloneBASString(current.BranchID), AttemptID: current.AttemptID}
				workingRef, err = repos.Preparations.Append(ctx, retained, currentRef)
				if err != nil {
					return err
				}
				next.ContentHash = bytes.Clone(workingRef.ContentHash)
				working = retained
				inputRevision = next.InputRevision
			}

			savedID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(savedID) {
				return ErrInvalidCommand
			}
			saved := &tammyv1.BasSavedPaygInputs{
				InputSnapshotId: savedID, OrganisationId: r.OrganisationId, PreparationId: r.PreparationId,
				Preparation: cloneBASRef(workingRef), InputRevision: inputRevision, InputHash: bytes.Clone(inputHash),
				PaygwInputs: proto.Clone(r.Paygw).(*tammyv1.BasPaygwInputs), PaygiInputs: proto.Clone(r.Paygi).(*tammyv1.BasPaygiInputs),
				ActorUserId: r.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(service.now().UTC()),
			}
			savedRef, err := repos.SavedPayg.Append(ctx, saved)
			if err != nil {
				return err
			}

			next := proto.Clone(working.Preparation).(*tammyv1.BasPreparation)
			next.Version++
			next.ContentHash = nil
			next.UpdatedAt = timestamppb.New(service.now().UTC())
			retained := &PreparationRetainedSnapshot{
				Preparation: next, SavedPaygInputs: cloneBASRef(savedRef),
				BranchID: cloneBASString(working.BranchID), AttemptID: working.AttemptID,
			}
			finalRef, err := repos.Preparations.Append(ctx, retained, workingRef)
			if err != nil {
				return err
			}
			if repos.AdvancePreparation == nil {
				return ErrCommandUnavailable
			}
			if err := repos.AdvancePreparation(ctx, r.OrganisationId, prep.Family, currentRef, finalRef, next.UpdatedAt); err != nil {
				return err
			}
			next.ContentHash = bytes.Clone(finalRef.ContentHash)
			response = &tammyv1.SaveBasPaygInputsResponse{Preparation: next}
			payload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PAYG_INPUTS_SAVED,
				Source: cloneBASRef(finalRef), Previous: cloneBASRef(currentRef),
				AffectedResources: []*tammyv1.SourceRef{cloneBASRef(savedRef)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Preparation == nil || payload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationSaveBasPaygInputs,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Preparation.PreparationId, response, payload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.SaveBasPaygInputsResponse)
	if !ok || value.Preparation == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func (service *Service) RegenerateBasPreparation(ctx context.Context,
	request *tammyv1.RegenerateBasPreparationRequest) (*tammyv1.RegenerateBasPreparationResponse, error) {
	if service == nil || request == nil ||
		!validBASCommandContext(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) ||
		request.ExpectedVersion >= math.MaxInt64 {
		return nil, ErrInvalidCommand
	}
	var result *RegenerationResult
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationRegenerateBasPreparation,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.RegenerateBasPreparationResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.RegenerateBasPreparationRequest)
			if !ok || repos.Preparations == nil || repos.Regenerate == nil {
				return ErrInvalidCommand
			}
			var err error
			if r.ExpectedVersion == 0 && r.FamilyExpectation.GetRetained() != nil {
				result, err = service.regenerateCorrectiveGenesis(ctx, repos, r)
			} else {
				result, err = repos.Regenerate(ctx, r, r.CommandContext.Authentication.ActorUserId, timestamppb.New(service.now().UTC()))
			}
			if err != nil {
				return err
			}
			if result == nil || result.Response == nil || result.Response.Preparation == nil ||
				result.Response.Calculation == nil || !validBASSourceRef(result.Source, "bas_preparation", result.Response.Preparation.PreparationId) {
				return ErrInvalidCommand
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if result == nil || result.Response == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			payload := &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PREPARATION_REGENERATED,
				Source: cloneBASRef(result.Source), Previous: cloneBASRef(result.Previous),
				AffectedResources: cloneBASRefs(result.Affected),
			}
			return service.audit.Build(ctx, app.OrdinaryOperationRegenerateBasPreparation,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				result.Response.Preparation.PreparationId, result.Response, payload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.RegenerateBasPreparationResponse)
	if !ok || value.Preparation == nil || value.Calculation == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func PeriodAuditPayload(ref, previous *tammyv1.SourceRef) *tammyv1.BasPreparationChangedEvent {
	return &tammyv1.BasPreparationChangedEvent{
		Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PERIOD_CONFIRMED,
		Source: cloneBASRef(ref), Previous: cloneBASRef(previous),
	}
}

func cloneBASRef(value *tammyv1.SourceRef) *tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*tammyv1.SourceRef)
}
func cloneBASRefs(values []*tammyv1.SourceRef) []*tammyv1.SourceRef {
	out := make([]*tammyv1.SourceRef, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, cloneBASRef(value))
		}
	}
	return out
}
func cloneBASString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
