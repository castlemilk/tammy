package purchases

import (
	"context"
	"reflect"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

// Service delegates every mutation to the existing command coordinator. The
// coordinator owns authorization, durable election, audit and one commit.
type Service struct {
	commands CommandRunner
	audit    AuditFactory
}

func NewService(commands CommandRunner, audit AuditFactory) (*Service, error) {
	if purchaseDependencyNil(commands) || purchaseDependencyNil(audit) {
		return nil, ErrRepository
	}
	return &Service{commands: commands, audit: audit}, nil
}

type purchaseCommandRequest interface {
	proto.Message
	GetCommandContext() *tammyv1.CommandContext
	GetOrganisationId() string
}

func executePurchaseCommand[Request purchaseCommandRequest, Response proto.Message](s *Service, ctx context.Context, request Request, operation app.OrdinaryOperation, newResult func() Response, work func(context.Context, CommandPort, Request) (Response, error), describe func(Response) (string, proto.Message, error)) (Response, error) {
	var zero Response
	if s == nil || purchaseDependencyNil(s.commands) || purchaseDependencyNil(s.audit) || ctx == nil || !request.ProtoReflect().IsValid() || !ids.IsCanonicalV7(request.GetOrganisationId()) {
		return zero, ErrInvalidCommand
	}
	commandContext := request.GetCommandContext()
	if commandContext == nil || commandContext.Authentication == nil || !ids.IsCanonicalV7(commandContext.IdempotencyKey) || !ids.IsCanonicalV7(commandContext.Authentication.ActorUserId) || !ids.IsCanonicalV7(commandContext.Authentication.SessionId) {
		return zero, ErrInvalidCommand
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	var saved Response
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{Operation: operation, OperationKey: commandContext.IdempotencyKey, Authentication: commandContext.Authentication, Request: request, NewResult: func() proto.Message { return newResult() }, SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
		if purchaseDependencyNil(repositories.Purchases) {
			return ErrRepository
		}
		typed, ok := owned.(Request)
		if !ok || !typed.ProtoReflect().IsValid() {
			return ErrRepository
		}
		var err error
		saved, err = work(ctx, repositories.Purchases, typed)
		return err
	}, BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
		typed, ok := owned.(Request)
		if !ok || purchaseDependencyNil(saved) || !saved.ProtoReflect().IsValid() || !purchaseResultFits(saved) {
			return app.CommandResult{}, ErrRepository
		}
		resource, payload, err := describe(saved)
		if err != nil || !ids.IsCanonicalV7(resource) || purchaseDependencyNil(payload) {
			return app.CommandResult{}, ErrRepository
		}
		c := typed.GetCommandContext()
		return s.audit.Build(ctx, operation, c.Authentication, c.IdempotencyKey, resource, saved, payload)
	}})
	if err != nil {
		return zero, err
	}
	response, ok := result.(Response)
	if !ok || purchaseDependencyNil(response) || !response.ProtoReflect().IsValid() || !purchaseResultFits(response) {
		return zero, ErrRepository
	}
	resource, payload, err := describe(response)
	if err != nil || !ids.IsCanonicalV7(resource) || purchaseDependencyNil(payload) {
		return zero, ErrRepository
	}
	return response, nil
}

func purchaseDependencyNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}
func (s *Service) PostReviewed(ctx context.Context, request *tammyv1.PostReviewedPurchaseRequest) (*tammyv1.PostReviewedPurchaseResponse, error) {
	if request == nil || !ids.IsCanonicalV7(request.DocumentId) {
		return nil, ErrInvalidCommand
	}
	return executePurchaseCommand(s, ctx, request, app.OrdinaryOperationPostReviewedPurchase, func() *tammyv1.PostReviewedPurchaseResponse { return &tammyv1.PostReviewedPurchaseResponse{} }, func(ctx context.Context, port CommandPort, owned *tammyv1.PostReviewedPurchaseRequest) (*tammyv1.PostReviewedPurchaseResponse, error) {
		return port.PostReviewed(ctx, owned)
	}, func(value *tammyv1.PostReviewedPurchaseResponse) (string, proto.Message, error) {
		if value == nil || value.Posting == nil || value.Accounting == nil {
			return "", nil, ErrRepository
		}
		return value.Posting.Id, value.Posting, nil
	})
}
func (s *Service) RecordPayment(ctx context.Context, request *tammyv1.RecordPurchasePaymentRequest) (*tammyv1.RecordPurchasePaymentResponse, error) {
	if request == nil || !ids.IsCanonicalV7(request.DocumentId) {
		return nil, ErrInvalidCommand
	}
	return executePurchaseCommand(s, ctx, request, app.OrdinaryOperationRecordPurchasePayment, func() *tammyv1.RecordPurchasePaymentResponse { return &tammyv1.RecordPurchasePaymentResponse{} }, func(ctx context.Context, port CommandPort, owned *tammyv1.RecordPurchasePaymentRequest) (*tammyv1.RecordPurchasePaymentResponse, error) {
		return port.RecordPayment(ctx, owned)
	}, func(value *tammyv1.RecordPurchasePaymentResponse) (string, proto.Message, error) {
		if value == nil || value.Payment == nil || value.Accounting == nil {
			return "", nil, ErrRepository
		}
		return value.Payment.Id, value.Payment, nil
	})
}
func (s *Service) RecordEvidence(ctx context.Context, request *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error) {
	if request == nil || !ids.IsCanonicalV7(request.DocumentId) {
		return nil, ErrInvalidCommand
	}
	return executePurchaseCommand(s, ctx, request, app.OrdinaryOperationRecordPurchaseTaxEvidence, func() *tammyv1.RecordPurchaseTaxEvidenceResponse { return &tammyv1.RecordPurchaseTaxEvidenceResponse{} }, func(ctx context.Context, port CommandPort, owned *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error) {
		return port.RecordEvidence(ctx, owned)
	}, func(value *tammyv1.RecordPurchaseTaxEvidenceResponse) (string, proto.Message, error) {
		if value == nil || value.Evidence == nil || value.Accounting == nil {
			return "", nil, ErrRepository
		}
		return value.Evidence.Id, value.Evidence, nil
	})
}
func (s *Service) Attribute(ctx context.Context, request *tammyv1.AttributePurchaseGstRequest) (*tammyv1.AttributePurchaseGstResponse, error) {
	if request == nil || !ids.IsCanonicalV7(request.DocumentId) {
		return nil, ErrInvalidCommand
	}
	return executePurchaseCommand(s, ctx, request, app.OrdinaryOperationAttributePurchaseGst, func() *tammyv1.AttributePurchaseGstResponse { return &tammyv1.AttributePurchaseGstResponse{} }, func(ctx context.Context, port CommandPort, owned *tammyv1.AttributePurchaseGstRequest) (*tammyv1.AttributePurchaseGstResponse, error) {
		return port.Attribute(ctx, owned)
	}, func(value *tammyv1.AttributePurchaseGstResponse) (string, proto.Message, error) {
		if value == nil || value.Accounting == nil || value.Journal == nil || len(value.Attributions) == 0 || value.Attributions[0] == nil {
			return "", nil, ErrRepository
		}
		return value.Attributions[0].Id, value, nil
	})
}
func (s *Service) Defer(ctx context.Context, request *tammyv1.DeferPurchaseGstClaimRequest) (*tammyv1.DeferPurchaseGstClaimResponse, error) {
	if request == nil || !ids.IsCanonicalV7(request.DocumentId) {
		return nil, ErrInvalidCommand
	}
	return executePurchaseCommand(s, ctx, request, app.OrdinaryOperationDeferPurchaseGstClaim, func() *tammyv1.DeferPurchaseGstClaimResponse { return &tammyv1.DeferPurchaseGstClaimResponse{} }, func(ctx context.Context, port CommandPort, owned *tammyv1.DeferPurchaseGstClaimRequest) (*tammyv1.DeferPurchaseGstClaimResponse, error) {
		return port.Defer(ctx, owned)
	}, func(value *tammyv1.DeferPurchaseGstClaimResponse) (string, proto.Message, error) {
		if value == nil || value.Deferral == nil || value.Accounting == nil {
			return "", nil, ErrRepository
		}
		return value.Deferral.Id, value.Deferral, nil
	})
}
func (s *Service) ResolveLegacy(ctx context.Context, request *tammyv1.ResolveLegacyPurchaseReviewRequest) (*tammyv1.ResolveLegacyPurchaseReviewResponse, error) {
	if request == nil {
		return nil, ErrInvalidCommand
	}
	return executePurchaseCommand(s, ctx, request, app.OrdinaryOperationResolveLegacyPurchaseReview, func() *tammyv1.ResolveLegacyPurchaseReviewResponse {
		return &tammyv1.ResolveLegacyPurchaseReviewResponse{}
	}, func(ctx context.Context, port CommandPort, owned *tammyv1.ResolveLegacyPurchaseReviewRequest) (*tammyv1.ResolveLegacyPurchaseReviewResponse, error) {
		return port.ResolveLegacy(ctx, owned)
	}, func(value *tammyv1.ResolveLegacyPurchaseReviewResponse) (string, proto.Message, error) {
		if value == nil || value.Review == nil {
			return "", nil, ErrRepository
		}
		return value.Review.Id, value.Review, nil
	})
}
