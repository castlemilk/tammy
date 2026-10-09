package purchases

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

const serviceTestOrg = "018f0000-0000-7000-8000-000000008001"
const serviceTestDocument = "018f0000-0000-7000-8000-000000008002"
const serviceTestActor = "018f0000-0000-7000-8000-000000008003"
const serviceTestSession = "018f0000-0000-7000-8000-000000008004"
const serviceTestKey = "018f0000-0000-7000-8000-000000008005"

var errServiceTest = errors.New("purchase command failed")

func serviceTestContext() *tammyv1.CommandContext {
	return &tammyv1.CommandContext{IdempotencyKey: serviceTestKey, Authentication: &tammyv1.AuthenticationContext{ActorUserId: serviceTestActor, SessionId: serviceTestSession}}
}

type oversizedServicePort struct{ serviceTestPort }

func (p *oversizedServicePort) PostReviewed(context.Context, *tammyv1.PostReviewedPurchaseRequest) (*tammyv1.PostReviewedPurchaseResponse, error) {
	p.calls++
	return &tammyv1.PostReviewedPurchaseResponse{Posting: &tammyv1.PurchasePosting{Id: serviceTestDocument, SupplierName: strings.Repeat("x", MaximumListResponseBytes)}, Accounting: &tammyv1.PurchaseAccounting{DocumentId: serviceTestDocument}}, nil
}

func TestPurchaseServiceRejectsOversizedResultBeforeAuditAndRetainedReplay(t *testing.T) {
	port := &oversizedServicePort{}
	audit := &serviceTestAudit{}
	runner := &serviceTestRunner{port: port}
	s, err := NewService(runner, audit)
	if err != nil {
		t.Fatal(err)
	}
	request := &tammyv1.PostReviewedPurchaseRequest{CommandContext: serviceTestContext(), OrganisationId: serviceTestOrg, DocumentId: serviceTestDocument}
	if _, err := s.PostReviewed(context.Background(), request); err == nil || audit.calls != 0 {
		t.Fatalf("oversized actual response reached audit: err=%v audits=%d", err, audit.calls)
	}
	runner.replay, _ = port.PostReviewed(context.Background(), request)
	before := port.calls
	if _, err := s.PostReviewed(context.Background(), request); err == nil || port.calls != before || audit.calls != 0 {
		t.Fatalf("oversized replay exposed: err=%v calls=%d/%d audits=%d", err, port.calls, before, audit.calls)
	}
}

type serviceTestPort struct {
	calls int
	err   error
}

func (p *serviceTestPort) PostReviewed(_ context.Context, _ *tammyv1.PostReviewedPurchaseRequest) (*tammyv1.PostReviewedPurchaseResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &tammyv1.PostReviewedPurchaseResponse{Posting: &tammyv1.PurchasePosting{Id: serviceTestDocument}, Accounting: &tammyv1.PurchaseAccounting{DocumentId: serviceTestDocument}, Journal: &tammyv1.Journal{Id: serviceTestDocument}}, nil
}
func (p *serviceTestPort) RecordPayment(_ context.Context, _ *tammyv1.RecordPurchasePaymentRequest) (*tammyv1.RecordPurchasePaymentResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &tammyv1.RecordPurchasePaymentResponse{Payment: &tammyv1.PurchasePayment{Id: serviceTestDocument}, Accounting: &tammyv1.PurchaseAccounting{DocumentId: serviceTestDocument}, Journal: &tammyv1.Journal{Id: serviceTestDocument}}, nil
}
func (p *serviceTestPort) RecordEvidence(_ context.Context, _ *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &tammyv1.RecordPurchaseTaxEvidenceResponse{Evidence: &tammyv1.PurchaseTaxEvidence{Id: serviceTestDocument}, Accounting: &tammyv1.PurchaseAccounting{DocumentId: serviceTestDocument}}, nil
}
func (p *serviceTestPort) Attribute(_ context.Context, _ *tammyv1.AttributePurchaseGstRequest) (*tammyv1.AttributePurchaseGstResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &tammyv1.AttributePurchaseGstResponse{Attributions: []*tammyv1.PurchaseGstAttribution{{Id: serviceTestDocument}}, Journal: &tammyv1.Journal{Id: serviceTestDocument}, Accounting: &tammyv1.PurchaseAccounting{DocumentId: serviceTestDocument}}, nil
}
func (p *serviceTestPort) Defer(_ context.Context, _ *tammyv1.DeferPurchaseGstClaimRequest) (*tammyv1.DeferPurchaseGstClaimResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &tammyv1.DeferPurchaseGstClaimResponse{Deferral: &tammyv1.PurchaseGstDeferral{Id: serviceTestDocument}, Accounting: &tammyv1.PurchaseAccounting{DocumentId: serviceTestDocument}}, nil
}
func (p *serviceTestPort) ResolveLegacy(_ context.Context, _ *tammyv1.ResolveLegacyPurchaseReviewRequest) (*tammyv1.ResolveLegacyPurchaseReviewResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &tammyv1.ResolveLegacyPurchaseReviewResponse{Review: &tammyv1.LegacyPurchaseReview{Id: serviceTestDocument}}, nil
}

type serviceTestRunner struct {
	port   CommandPort
	replay proto.Message
	last   app.OrdinaryCommand[CommandRepositories]
	calls  int
}

func (r *serviceTestRunner) Execute(ctx context.Context, command app.OrdinaryCommand[CommandRepositories]) (proto.Message, error) {
	r.calls++
	r.last = command
	if r.replay != nil {
		return proto.Clone(r.replay), nil
	}
	owned := proto.Clone(command.Request)
	repos := CommandRepositories{Purchases: r.port}
	if err := command.SaveSource(ctx, repos, owned); err != nil {
		return nil, err
	}
	outcome, err := command.BuildResult(ctx, repos, owned)
	return outcome.Result, err
}

type serviceTestAudit struct {
	calls         int
	operation     app.OrdinaryOperation
	key, resource string
	auth          *tammyv1.AuthenticationContext
	payload       proto.Message
}

func (a *serviceTestAudit) Build(_ context.Context, operation app.OrdinaryOperation, auth *tammyv1.AuthenticationContext, key, resource string, result, payload proto.Message) (app.CommandResult, error) {
	a.calls++
	a.operation, a.key, a.resource, a.auth, a.payload = operation, key, resource, proto.Clone(auth).(*tammyv1.AuthenticationContext), proto.Clone(payload)
	return app.CommandResult{Result: result, ResourceID: resource}, nil
}
func TestPurchaseServiceUsesElectedRepositoriesAndRetainedReplay(t *testing.T) {
	for _, test := range []struct {
		name   string
		invoke func(*Service, *tammyv1.CommandContext) (proto.Message, error)
	}{
		{"PostReviewedPurchase", func(s *Service, c *tammyv1.CommandContext) (proto.Message, error) {
			return s.PostReviewed(context.Background(), &tammyv1.PostReviewedPurchaseRequest{CommandContext: c, OrganisationId: serviceTestOrg, DocumentId: serviceTestDocument})
		}},
		{"RecordPurchasePayment", func(s *Service, c *tammyv1.CommandContext) (proto.Message, error) {
			return s.RecordPayment(context.Background(), &tammyv1.RecordPurchasePaymentRequest{CommandContext: c, OrganisationId: serviceTestOrg, DocumentId: serviceTestDocument})
		}},
		{"RecordPurchaseTaxEvidence", func(s *Service, c *tammyv1.CommandContext) (proto.Message, error) {
			return s.RecordEvidence(context.Background(), &tammyv1.RecordPurchaseTaxEvidenceRequest{CommandContext: c, OrganisationId: serviceTestOrg, DocumentId: serviceTestDocument})
		}},
		{"AttributePurchaseGst", func(s *Service, c *tammyv1.CommandContext) (proto.Message, error) {
			return s.Attribute(context.Background(), &tammyv1.AttributePurchaseGstRequest{CommandContext: c, OrganisationId: serviceTestOrg, DocumentId: serviceTestDocument})
		}},
		{"DeferPurchaseGstClaim", func(s *Service, c *tammyv1.CommandContext) (proto.Message, error) {
			return s.Defer(context.Background(), &tammyv1.DeferPurchaseGstClaimRequest{CommandContext: c, OrganisationId: serviceTestOrg, DocumentId: serviceTestDocument})
		}},
		{"ResolveLegacyPurchaseReview", func(s *Service, c *tammyv1.CommandContext) (proto.Message, error) {
			return s.ResolveLegacy(context.Background(), &tammyv1.ResolveLegacyPurchaseReviewRequest{CommandContext: c, OrganisationId: serviceTestOrg})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := &serviceTestPort{}
			runner := &serviceTestRunner{port: port}
			audit := &serviceTestAudit{}
			service, err := NewService(runner, audit)
			if err != nil {
				t.Fatal(err)
			}
			c := serviceTestContext()
			result, err := test.invoke(service, c)
			if err != nil || result == nil || port.calls != 1 || audit.calls != 1 {
				t.Fatalf("result=%v error=%v source calls=%d audits=%d", result, err, port.calls, audit.calls)
			}
			operation := app.OrdinaryOperation("tammy.v1.PurchaseService." + test.name)
			if runner.last.Operation != operation || runner.last.OperationKey != serviceTestKey || !proto.Equal(runner.last.Authentication, c.Authentication) || audit.operation != operation || audit.key != serviceTestKey || audit.resource != serviceTestDocument || !proto.Equal(audit.auth, c.Authentication) || audit.payload == nil {
				t.Fatalf("command/audit lost typed provenance: command=%+v audit=%+v", runner.last, audit)
			}
			runner.replay = proto.Clone(result)
			replay, err := test.invoke(service, c)
			if err != nil || !proto.Equal(replay, result) || port.calls != 1 || audit.calls != 1 {
				t.Fatalf("retained replay executed another source: result=%v error=%v calls=%d audits=%d", replay, err, port.calls, audit.calls)
			}
			runner.replay = nil
			port.err = errServiceTest
			if _, err := test.invoke(service, c); !errors.Is(err, errServiceTest) || audit.calls != 1 {
				t.Fatalf("source error=%v audits=%d", err, audit.calls)
			}
			for _, invalid := range []*tammyv1.CommandContext{nil, {}, {Authentication: c.Authentication, IdempotencyKey: "bad"}, {Authentication: &tammyv1.AuthenticationContext{ActorUserId: serviceTestActor}, IdempotencyKey: serviceTestKey}} {
				before := runner.calls
				if _, err := test.invoke(service, invalid); err == nil || runner.calls != before {
					t.Fatalf("invalid context reached coordinator: %v calls=%d/%d", err, runner.calls, before)
				}
			}
		})
	}
}
func TestPurchaseServiceRejectsInvalidComposition(t *testing.T) {
	var runner *serviceTestRunner
	var audit *serviceTestAudit
	for _, dependencies := range []struct {
		runner CommandRunner
		audit  AuditFactory
	}{{nil, &serviceTestAudit{}}, {&serviceTestRunner{}, nil}, {runner, &serviceTestAudit{}}, {&serviceTestRunner{}, audit}} {
		if s, err := NewService(dependencies.runner, dependencies.audit); s != nil || err == nil {
			t.Fatalf("invalid composition=%v,%v", s, err)
		}
	}
}
