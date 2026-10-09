package purchases

import (
	"context"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

// CommandPort is supplied by the encrypted transaction adapter. Its methods
// append owned sources and their journals within that same transaction.
type CommandPort interface {
	PostReviewed(context.Context, *tammyv1.PostReviewedPurchaseRequest) (*tammyv1.PostReviewedPurchaseResponse, error)
	RecordPayment(context.Context, *tammyv1.RecordPurchasePaymentRequest) (*tammyv1.RecordPurchasePaymentResponse, error)
	RecordEvidence(context.Context, *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error)
	Attribute(context.Context, *tammyv1.AttributePurchaseGstRequest) (*tammyv1.AttributePurchaseGstResponse, error)
	Defer(context.Context, *tammyv1.DeferPurchaseGstClaimRequest) (*tammyv1.DeferPurchaseGstClaimResponse, error)
	ResolveLegacy(context.Context, *tammyv1.ResolveLegacyPurchaseReviewRequest) (*tammyv1.ResolveLegacyPurchaseReviewResponse, error)
}
type CommandRepositories struct {
	Purchases CommandPort
	// Authorization is verified in the elected transaction, never client supplied.
	Authorize func(context.Context, *tammyv1.AuthenticationContext, authorisation.Action) error
}
type CommandRunner interface {
	Execute(context.Context, app.OrdinaryCommand[CommandRepositories]) (proto.Message, error)
}
type AuditFactory interface {
	Build(context.Context, app.OrdinaryOperation, *tammyv1.AuthenticationContext, string, string, proto.Message, proto.Message) (app.CommandResult, error)
}
