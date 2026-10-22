package organisations

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type serviceCommandRunner struct {
	repositories CommandRepositories
	operation    app.OrdinaryOperation
}

func (runner *serviceCommandRunner) Execute(ctx context.Context, command app.OrdinaryCommand[CommandRepositories]) (proto.Message, error) {
	runner.operation = command.Operation
	if command.SaveSource != nil {
		if err := command.SaveSource(ctx, runner.repositories, command.Request); err != nil {
			return nil, err
		}
	}
	result, err := command.BuildResult(ctx, runner.repositories, command.Request)
	return result.Result, err
}

type serviceProfileRepository struct{ profile *tammyv1.Organisation }

func (repository *serviceProfileRepository) Create(_ context.Context, profile *tammyv1.Organisation, _ time.Time) error {
	repository.profile = proto.Clone(profile).(*tammyv1.Organisation)
	return nil
}
func (repository *serviceProfileRepository) Get(context.Context, string) (*tammyv1.Organisation, error) {
	return proto.Clone(repository.profile).(*tammyv1.Organisation), nil
}
func (repository *serviceProfileRepository) Update(_ context.Context, _ uint64, profile *tammyv1.Organisation, _ time.Time) error {
	repository.profile = proto.Clone(profile).(*tammyv1.Organisation)
	return nil
}

type serviceSetup struct{ calls int }

func (setup *serviceSetup) Install(context.Context, string, time.Time) error {
	setup.calls++
	return nil
}

type serviceFactor struct{ calls int }

func (factor *serviceFactor) Consume(context.Context, *tammyv1.FreshFactorContext, string) error {
	factor.calls++
	return nil
}

type serviceAuditFactory struct{ workspaceID string }

func (factory serviceAuditFactory) Build(
	_ context.Context, operation app.OrdinaryOperation, authentication *tammyv1.AuthenticationContext,
	operationKey, resourceID string, result proto.Message, payload *tammyv1.AuditEventPayload,
) (app.CommandResult, error) {
	encoded, _ := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	digest := sha256.Sum256(encoded)
	payloadBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	return app.CommandResult{Result: result, ResourceID: resourceID, AuditPayload: payloadBytes,
		AuditEvent: &tammyv1.AuditEvent{WorkspaceId: factory.workspaceID, Actor: proto.Clone(authentication).(*tammyv1.AuthenticationContext),
			CommandType: string(operation), IdempotencyKey: &operationKey,
			Result: &tammyv1.AuditResultMetadata{TypeName: string(result.ProtoReflect().Descriptor().FullName()), DeterministicSha256: digest[:], OutcomeCode: "SUCCESS"}}}, nil
}

func TestServiceRoutesCreateAndHighRiskUpdateThroughClosedCoordinator(t *testing.T) {
	profileRepository := &serviceProfileRepository{}
	setup := &serviceSetup{}
	factor := &serviceFactor{}
	runner := &serviceCommandRunner{repositories: CommandRepositories{Profiles: profileRepository, Setup: setup, Factors: factor}}
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	ids := []string{"018f0000-0000-7000-8000-000000000020"}
	service, err := NewService(ServiceConfig{Commands: runner, Audit: serviceAuditFactory{workspaceID: "018f0000-0000-7000-8000-000000000001"},
		Clock: func() time.Time { return now }, NewID: func() string { id := ids[0]; ids = ids[1:]; return id }})
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: "018f0000-0000-7000-8000-000000000010", SessionId: "018f0000-0000-7000-8000-000000000011"}
	create := &tammyv1.CreateOrganisationRequest{CommandContext: &tammyv1.CommandContext{IdempotencyKey: "018f0000-0000-7000-8000-000000000012", Authentication: auth},
		Abn: "51824753556", LegalName: "Tammy Pty Ltd", DisplayName: "Tammy", EntityType: "AU_PRIVATE_COMPANY",
		GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH, GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6, ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "rule_bundle", Id: "018f0000-0000-7000-8000-000000000030", Revision: 1, ContentHash: make([]byte, 32)}}
	created, err := service.CreateOrganisation(context.Background(), create)
	if err != nil || created.Organisation == nil || setup.calls != 1 || runner.operation != app.OrdinaryOperationCreateOrganisation {
		t.Fatalf("CreateOrganisation() = %#v, %v; setup=%d operation=%s", created, err, setup.calls, runner.operation)
	}

	freshAt := now
	update := &tammyv1.UpdateOrganisationRequest{CommandContext: &tammyv1.CommandContext{IdempotencyKey: "018f0000-0000-7000-8000-000000000013", Authentication: auth,
		FreshFactor: &tammyv1.FreshFactorContext{AssertionId: "018f0000-0000-7000-8000-000000000014", Purpose: UpdateHighRiskPurpose, AssertedAt: nil}}, OrganisationId: created.Organisation.Id,
		ExpectedVersion: 1, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"legal_name"}}, Patch: &tammyv1.OrganisationPatch{LegalName: "Tammy Holdings Pty Ltd"}}
	update.CommandContext.FreshFactor.AssertedAt = timestamppb.New(freshAt)
	reason := "registered name updated"
	update.Reason = &reason
	updated, err := service.UpdateOrganisation(context.Background(), update)
	if err != nil || updated.Organisation.LegalName != "Tammy Holdings Pty Ltd" || factor.calls != 1 ||
		runner.operation != app.OrdinaryOperationUpdateOrganisation {
		t.Fatalf("UpdateOrganisation() = %#v, %v; factors=%d operation=%s", updated, err, factor.calls, runner.operation)
	}
}
