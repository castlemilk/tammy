package invoices

import (
	"context"
	"crypto/sha256"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ProfileStore interface {
	Get(context.Context, string) (*tammyv1.Organisation, error)
}
type CommandRepositories struct {
	Invoices   *Repository
	Profiles   ProfileStore
	Accounting AccountingPort
	// Authorization is supplied by the transaction adapter, never by the caller.
	Authorize func(context.Context, *tammyv1.AuthenticationContext, authorisation.Action) error
}
type CommandRunner interface {
	Execute(context.Context, app.OrdinaryCommand[CommandRepositories]) (proto.Message, error)
}
type AuditFactory interface {
	Build(context.Context, app.OrdinaryOperation, *tammyv1.AuthenticationContext, string, string, proto.Message, proto.Message) (app.CommandResult, error)
}
type Service struct {
	commands CommandRunner
	audit    AuditFactory
	now      func() time.Time
	newID    func() (string, error)
}

func NewService(commands CommandRunner, audit AuditFactory, now func() time.Time, newID func() (string, error)) (*Service, error) {
	if commands == nil || audit == nil || now == nil || newID == nil {
		return nil, ErrRepository
	}
	return &Service{commands, audit, now, newID}, nil
}
func validCommand(c *tammyv1.CommandContext) bool {
	return c != nil && c.Authentication != nil && ids.IsCanonicalV7(c.IdempotencyKey) && ids.IsCanonicalV7(c.Authentication.ActorUserId) && ids.IsCanonicalV7(c.Authentication.SessionId)
}

func (s *Service) SaveDraft(ctx context.Context, request *tammyv1.SaveInvoiceDraftRequest) (*tammyv1.SaveInvoiceDraftResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) || request.ExpectedVersion >= math.MaxInt64 || (request.InvoiceId == "" && request.ExpectedVersion != 0) || (request.InvoiceId != "" && (!ids.IsCanonicalV7(request.InvoiceId) || request.ExpectedVersion == 0)) || (request.LogoAssetId != nil && *request.LogoAssetId != "" && !ids.IsCanonicalV7(*request.LogoAssetId)) {
		return nil, ErrInvalidDraft
	}
	if _, err := Calculate(request.Draft); err != nil {
		return nil, err
	}
	var saved *tammyv1.Invoice
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationSaveInvoiceDraft, OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication, Request: request, NewResult: func() proto.Message { return &tammyv1.SaveInvoiceDraftResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.SaveInvoiceDraftRequest)
			if repos.Invoices == nil || repos.Profiles == nil {
				return ErrRepository
			}
			profile, err := repos.Profiles.Get(ctx, r.OrganisationId)
			if err != nil {
				return ErrNotFound
			}
			saved, err = Calculate(r.Draft)
			if err != nil {
				return err
			}
			now := timestamppb.New(s.now())
			saved.Id = r.InvoiceId
			saved.OrganisationId = r.OrganisationId
			saved.Version = r.ExpectedVersion + 1
			saved.State = tammyv1.InvoiceState_INVOICE_STATE_DRAFT
			saved.SellerName = profile.LegalName
			saved.SellerAbn = profile.Abn
			saved.Draft = proto.Clone(r.Draft).(*tammyv1.InvoiceDraft)
			saved.UpdatedAt = now
			saved.CreatedAt = proto.Clone(now).(*timestamppb.Timestamp)
			if r.InvoiceId == "" {
				saved.Id, err = s.newID()
				if err != nil {
					return ErrRepository
				}
			} else {
				before, err := repos.Invoices.Get(ctx, r.OrganisationId, r.InvoiceId)
				if err != nil {
					return err
				}
				if before.State != tammyv1.InvoiceState_INVOICE_STATE_DRAFT || before.Version != r.ExpectedVersion {
					return ErrConflict
				}
				saved.CreatedAt = before.CreatedAt
				// Older clients omit this field; keep the reviewed logo snapshot.
				saved.LogoAssetId = before.LogoAssetId
			}
			if r.LogoAssetId != nil {
				saved.LogoAssetId = nil
				if *r.LogoAssetId != "" {
					saved.LogoAssetId = proto.String(*r.LogoAssetId)
				}
			}
			return repos.Invoices.Save(ctx, saved, r.ExpectedVersion)
		}, BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.SaveInvoiceDraftRequest)
			if saved == nil {
				return app.CommandResult{}, ErrRepository
			}
			result := &tammyv1.SaveInvoiceDraftResponse{Invoice: saved}
			return s.audit.Build(ctx, app.OrdinaryOperationSaveInvoiceDraft, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, saved.Id, result, saved)
		}})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.SaveInvoiceDraftResponse)
	if !ok || response.Invoice == nil {
		return nil, ErrRepository
	}
	return response, nil
}

func (s *Service) SaveTemplate(ctx context.Context, request *tammyv1.SaveInvoiceTemplateRequest) (*tammyv1.SaveInvoiceTemplateResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) || request.ExpectedVersion >= math.MaxInt64 || ValidateTemplate(request.Defaults) != nil {
		return nil, ErrInvalidDraft
	}
	var saved *tammyv1.InvoiceTemplate
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation: app.OrdinaryOperationSaveInvoiceTemplate, OperationKey: request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication, Request: request,
		NewResult: func() proto.Message { return &tammyv1.SaveInvoiceTemplateResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.SaveInvoiceTemplateRequest)
			if repos.Invoices == nil {
				return ErrRepository
			}
			saved = &tammyv1.InvoiceTemplate{OrganisationId: r.OrganisationId, Version: r.ExpectedVersion + 1, Defaults: proto.Clone(r.Defaults).(*tammyv1.InvoiceTemplateInput)}
			return repos.Invoices.SaveTemplate(ctx, saved, r.ExpectedVersion, s.now())
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.SaveInvoiceTemplateRequest)
			if saved == nil {
				return app.CommandResult{}, ErrRepository
			}
			return s.audit.Build(ctx, app.OrdinaryOperationSaveInvoiceTemplate, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, saved.OrganisationId, &tammyv1.SaveInvoiceTemplateResponse{Template: saved}, saved)
		},
	})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.SaveInvoiceTemplateResponse)
	if !ok || response.Template == nil {
		return nil, ErrRepository
	}
	return response, nil
}

func (s *Service) UploadLogo(ctx context.Context, request *tammyv1.UploadInvoiceLogoRequest) (*tammyv1.UploadInvoiceLogoResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) {
		return nil, ErrInvalidDraft
	}
	if _, _, err := ValidateLogo(request.MimeType, request.Content); err != nil {
		return nil, err
	}
	var saved *tammyv1.InvoiceLogo
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation: app.OrdinaryOperationUploadInvoiceLogo, OperationKey: request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication, Request: request,
		NewResult: func() proto.Message { return &tammyv1.UploadInvoiceLogoResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.UploadInvoiceLogoRequest)
			if repos.Invoices == nil {
				return ErrRepository
			}
			width, height, err := ValidateLogo(r.MimeType, r.Content)
			if err != nil {
				return err
			}
			identifier, err := s.newID()
			if err != nil {
				return ErrRepository
			}
			digest := sha256.Sum256(r.Content)
			saved = &tammyv1.InvoiceLogo{Id: identifier, OrganisationId: r.OrganisationId, MimeType: r.MimeType, ByteLength: uint32(len(r.Content)), Sha256: digest[:], Width: width, Height: height, CreatedAt: timestamppb.New(s.now())}
			return repos.Invoices.SaveLogo(ctx, saved, r.Content, r.CommandContext.Authentication.ActorUserId)
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.UploadInvoiceLogoRequest)
			if saved == nil {
				return app.CommandResult{}, ErrRepository
			}
			// Audit and idempotency results contain metadata, never logo bytes.
			return s.audit.Build(ctx, app.OrdinaryOperationUploadInvoiceLogo, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, saved.Id, &tammyv1.UploadInvoiceLogoResponse{Logo: saved}, saved)
		},
	})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.UploadInvoiceLogoResponse)
	if !ok || response.Logo == nil {
		return nil, ErrRepository
	}
	return response, nil
}
func (s *Service) Issue(ctx context.Context, request *tammyv1.IssueInvoiceRequest) (*tammyv1.IssueInvoiceResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.InvoiceId) || request.ExpectedVersion < 1 {
		return nil, ErrInvalidDraft
	}
	var issued *tammyv1.Invoice
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationIssueInvoice, OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication, Request: request, NewResult: func() proto.Message { return &tammyv1.IssueInvoiceResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.IssueInvoiceRequest)
			if repos.Invoices == nil || repos.Profiles == nil || repos.Accounting == nil {
				return ErrRepository
			}
			var err error
			issued, err = repos.Invoices.Get(ctx, r.OrganisationId, r.InvoiceId)
			if err != nil {
				return err
			}
			if issued.State != tammyv1.InvoiceState_INVOICE_STATE_DRAFT || issued.Version != r.ExpectedVersion {
				return ErrConflict
			}
			if err := ValidateIssue(issued.Draft); err != nil {
				return err
			}
			profile, err := repos.Profiles.Get(ctx, r.OrganisationId)
			if err != nil {
				return ErrNotFound
			}
			// Issue exactly the seller identity reviewed with the saved draft. A
			// profile change requires another save/review, never a silent swap.
			if issued.SellerName != profile.LegalName || issued.SellerAbn != profile.Abn {
				return ErrConflict
			}
			issued.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
			issued.Version++
			issued.UpdatedAt = timestamppb.New(s.now())
			if err := repos.Invoices.Save(ctx, issued, r.ExpectedVersion); err != nil {
				return err
			}
			_, err = repos.Accounting.Post(ctx, issued, profile, r.CommandContext.IdempotencyKey)
			return err
		}, BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.IssueInvoiceRequest)
			if issued == nil {
				return app.CommandResult{}, ErrRepository
			}
			result := &tammyv1.IssueInvoiceResponse{Invoice: issued}
			return s.audit.Build(ctx, app.OrdinaryOperationIssueInvoice, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, issued.Id, result, issued)
		}})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.IssueInvoiceResponse)
	if !ok || response.Invoice == nil {
		return nil, ErrRepository
	}
	return response, nil
}
