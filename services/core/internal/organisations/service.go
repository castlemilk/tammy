package organisations

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrService = errors.New("organisations: service failure")

type ProfileRepository interface {
	Create(context.Context, *tammyv1.Organisation, time.Time) error
	Get(context.Context, string) (*tammyv1.Organisation, error)
	Update(context.Context, uint64, *tammyv1.Organisation, time.Time) error
}

// InitialSetupPort installs only versioned module-owned artefacts and chart
// projections. It is transaction-owned and carries no filesystem capability.
type InitialSetupPort interface {
	Install(context.Context, string, time.Time) error
}

// FreshFactorConsumer consumes replay-sensitive assertions in the same caller
// transaction as the elected organisation mutation.
type FreshFactorConsumer interface {
	Consume(context.Context, *tammyv1.FreshFactorContext, string) error
}

type CommandRepositories struct {
	Profiles ProfileRepository
	Setup    InitialSetupPort
	Factors  FreshFactorConsumer
	Evidence OrganisationEvidenceIntakePort
}

func (service *Service) RecordEntityVerification(
	ctx context.Context,
	request *tammyv1.RecordEntityVerificationRequest,
) (*tammyv1.RecordEntityVerificationResponse, error) {
	if service == nil || service.commands == nil || service.audit == nil || ctx == nil || request == nil ||
		request.CommandContext == nil || request.CommandContext.Authentication == nil {
		return nil, ErrService
	}
	verificationID, evidenceID := service.newID(), service.newID()
	if !ids.IsCanonicalV7(verificationID) || !ids.IsCanonicalV7(evidenceID) {
		return nil, ErrService
	}
	var record VerificationRecord
	var updated *tammyv1.Organisation
	var previousState tammyv1.OrganisationVerificationState
	command := app.OrdinaryCommand[CommandRepositories]{
		Operation: app.OrdinaryOperationRecordEntityVerification, OperationKey: request.CommandContext.IdempotencyKey,
		Authentication: proto.Clone(request.CommandContext.Authentication).(*tammyv1.AuthenticationContext), Request: request,
		NewResult: func() proto.Message { return &tammyv1.RecordEntityVerificationResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.RecordEntityVerificationRequest)
			if !ok || repositories.Profiles == nil || repositories.Evidence == nil {
				return ErrService
			}
			profile, err := repositories.Profiles.Get(ctx, ownedRequest.OrganisationId)
			if err != nil {
				return err
			}
			if profile.Version != ownedRequest.ExpectedVersion || !ValidVerificationTransition(profile.VerificationState, ownedRequest.Outcome) {
				return ErrRepositoryConflict
			}
			record, err = BuildVerificationRecord(ownedRequest, verificationID, evidenceID,
				ownedRequest.CommandContext.Authentication.ActorUserId, service.clock())
			if err != nil {
				return err
			}
			if err := repositories.Evidence.Save(ctx, record); err != nil {
				return err
			}
			previousState = profile.VerificationState
			updated = proto.Clone(profile).(*tammyv1.Organisation)
			updated.Version++
			updated.VerificationState = ownedRequest.Outcome
			return repositories.Profiles.Update(ctx, profile.Version, updated, service.clock())
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.RecordEntityVerificationRequest)
			if !ok || record.Verification == nil || updated == nil {
				return app.CommandResult{}, ErrService
			}
			result := &tammyv1.RecordEntityVerificationResponse{Verification: proto.Clone(record.Verification).(*tammyv1.EntityVerification),
				Organisation: proto.Clone(updated).(*tammyv1.Organisation)}
			payload := &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_EntityVerificationChanged{
				EntityVerificationChanged: &tammyv1.EntityVerificationChangedEvent{VerificationId: verificationID,
					OrganisationId: updated.Id, FromState: previousState, ToState: updated.VerificationState,
					EvidenceHash: append([]byte(nil), ownedRequest.Evidence.ContentHash...)}}}
			return service.audit.Build(ctx, app.OrdinaryOperationRecordEntityVerification,
				ownedRequest.CommandContext.Authentication, ownedRequest.CommandContext.IdempotencyKey,
				verificationID, result, payload)
		},
	}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.RecordEntityVerificationResponse)
	if !ok || response.Verification == nil || response.Organisation == nil {
		return nil, ErrService
	}
	return response, nil
}

type CommandRunner interface {
	Execute(context.Context, app.OrdinaryCommand[CommandRepositories]) (proto.Message, error)
}

// AuditFactory owns core-authored audit envelope metadata while the domain
// supplies the exact closed payload. Implementations are composition-scoped.
type AuditFactory interface {
	Build(context.Context, app.OrdinaryOperation, *tammyv1.AuthenticationContext,
		string, string, proto.Message, *tammyv1.AuditEventPayload) (app.CommandResult, error)
}

type ServiceConfig struct {
	Commands CommandRunner
	Audit    AuditFactory
	Clock    func() time.Time
	NewID    func() string
}

type Service struct {
	commands CommandRunner
	audit    AuditFactory
	clock    func() time.Time
	newID    func() string
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Commands == nil || config.Audit == nil || config.Clock == nil || config.NewID == nil {
		return nil, ErrService
	}
	return &Service{commands: config.Commands, audit: config.Audit, clock: config.Clock, newID: config.NewID}, nil
}

func (service *Service) CreateOrganisation(
	ctx context.Context,
	request *tammyv1.CreateOrganisationRequest,
) (*tammyv1.CreateOrganisationResponse, error) {
	if service == nil || service.commands == nil || service.audit == nil || ctx == nil || request == nil ||
		request.CommandContext == nil || request.CommandContext.Authentication == nil {
		return nil, ErrService
	}
	organisationID := service.newID()
	if !ids.IsCanonicalV7(organisationID) {
		return nil, ErrService
	}
	var created *tammyv1.Organisation
	command := app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationCreateOrganisation,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: proto.Clone(request.CommandContext.Authentication).(*tammyv1.AuthenticationContext),
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.CreateOrganisationResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.CreateOrganisationRequest)
			if !ok || repositories.Profiles == nil || repositories.Setup == nil {
				return ErrService
			}
			profile, err := CreateProfile(ownedRequest, organisationID, ownedRequest.CommandContext.Authentication.ActorUserId)
			if err != nil {
				return err
			}
			now := service.clock()
			if now.IsZero() || repositories.Profiles.Create(ctx, profile, now) != nil {
				return ErrService
			}
			if err := repositories.Setup.Install(ctx, organisationID, now); err != nil {
				return err
			}
			created = profile
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.CreateOrganisationRequest)
			if !ok || created == nil {
				return app.CommandResult{}, ErrService
			}
			result := &tammyv1.CreateOrganisationResponse{Organisation: proto.Clone(created).(*tammyv1.Organisation)}
			payload := organisationChangedPayload(created.Id, 1, 1, []string{
				"abn", "active_tax_rule_bundle", "display_name", "entity_type", "financial_year_end_month",
				"gst_basis", "gst_reporting_frequency", "legal_name", "owner_user_id",
			}, false)
			return service.audit.Build(ctx, app.OrdinaryOperationCreateOrganisation,
				ownedRequest.CommandContext.Authentication, ownedRequest.CommandContext.IdempotencyKey,
				created.Id, result, payload)
		},
	}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.CreateOrganisationResponse)
	if !ok || response.Organisation == nil {
		return nil, ErrService
	}
	return response, nil
}

func (service *Service) UpdateOrganisation(
	ctx context.Context,
	request *tammyv1.UpdateOrganisationRequest,
) (*tammyv1.UpdateOrganisationResponse, error) {
	if service == nil || service.commands == nil || service.audit == nil || ctx == nil || request == nil ||
		request.CommandContext == nil || request.CommandContext.Authentication == nil {
		return nil, ErrService
	}
	var before, updated *tammyv1.Organisation
	var changed []string
	command := app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationUpdateOrganisation,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: proto.Clone(request.CommandContext.Authentication).(*tammyv1.AuthenticationContext),
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.UpdateOrganisationResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.UpdateOrganisationRequest)
			if !ok || repositories.Profiles == nil || ValidateUpdateSecurity(ownedRequest, service.clock()) != nil {
				return ErrService
			}
			loaded, err := repositories.Profiles.Get(ctx, ownedRequest.OrganisationId)
			if err != nil {
				return err
			}
			if loaded.Version != ownedRequest.ExpectedVersion {
				return ErrRepositoryConflict
			}
			candidate, selected, err := applyOrganisationPatch(loaded, ownedRequest)
			if err != nil {
				return err
			}
			if updateRequiresFreshFactor(selected) {
				if repositories.Factors == nil {
					return ErrFreshFactorRequired
				}
				if err := repositories.Factors.Consume(ctx, ownedRequest.CommandContext.FreshFactor, UpdateHighRiskPurpose); err != nil {
					return err
				}
			}
			if err := repositories.Profiles.Update(ctx, loaded.Version, candidate, service.clock()); err != nil {
				return err
			}
			before, updated, changed = loaded, candidate, selected
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.UpdateOrganisationRequest)
			if !ok || before == nil || updated == nil {
				return app.CommandResult{}, ErrService
			}
			result := &tammyv1.UpdateOrganisationResponse{Organisation: proto.Clone(updated).(*tammyv1.Organisation)}
			payload := organisationChangedPayload(updated.Id, before.Version, updated.Version, changed,
				before.VerificationState != updated.VerificationState)
			return service.audit.Build(ctx, app.OrdinaryOperationUpdateOrganisation,
				ownedRequest.CommandContext.Authentication, ownedRequest.CommandContext.IdempotencyKey,
				updated.Id, result, payload)
		},
	}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.UpdateOrganisationResponse)
	if !ok || response.Organisation == nil {
		return nil, ErrService
	}
	return response, nil
}

func applyOrganisationPatch(
	before *tammyv1.Organisation,
	request *tammyv1.UpdateOrganisationRequest,
) (*tammyv1.Organisation, []string, error) {
	if !validOrganisation(before) || request == nil || request.Patch == nil || request.UpdateMask == nil ||
		before.Id != request.OrganisationId || before.Version != request.ExpectedVersion {
		return nil, nil, ErrInvalidOrganisation
	}
	after := proto.Clone(before).(*tammyv1.Organisation)
	paths := append([]string(nil), request.UpdateMask.Paths...)
	sort.Strings(paths)
	for _, path := range paths {
		switch path {
		case "abn":
			after.Abn = request.Patch.Abn
			after.VerificationState = tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED
		case "legal_name":
			after.LegalName = request.Patch.LegalName
			after.VerificationState = tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED
		case "display_name":
			after.DisplayName = request.Patch.DisplayName
		case "entity_type":
			after.EntityType = request.Patch.EntityType
			after.VerificationState = tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED
		case "gst_basis":
			after.GstBasis = request.Patch.GstBasis
		case "gst_reporting_frequency":
			after.GstReportingFrequency = request.Patch.GstReportingFrequency
		case "financial_year_end_month":
			after.FinancialYearEndMonth = request.Patch.FinancialYearEndMonth
		case "active_tax_rule_bundle":
			if request.Patch.ActiveTaxRuleBundle == nil {
				return nil, nil, ErrInvalidOrganisation
			}
			after.ActiveTaxRuleBundle = proto.Clone(request.Patch.ActiveTaxRuleBundle).(*tammyv1.SourceRef)
		default:
			return nil, nil, ErrInvalidOrganisation
		}
	}
	after.Version++
	if !validOrganisation(after) {
		return nil, nil, ErrInvalidOrganisation
	}
	return after, paths, nil
}

func updateRequiresFreshFactor(paths []string) bool {
	for _, path := range paths {
		switch path {
		case "abn", "legal_name", "entity_type", "gst_basis", "gst_reporting_frequency", "active_tax_rule_bundle":
			return true
		}
	}
	return false
}

func organisationChangedPayload(
	organisationID string,
	previousVersion, newVersion uint64,
	changedFields []string,
	verificationSuperseded bool,
) *tammyv1.AuditEventPayload {
	return &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_OrganisationChanged{
		OrganisationChanged: &tammyv1.OrganisationChangedEvent{OrganisationId: organisationID,
			PreviousVersion: previousVersion, NewVersion: newVersion,
			ChangedFields: append([]string(nil), changedFields...), VerificationSuperseded: verificationSuperseded},
	}}
}
