package restore

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var (
	ErrPreRestoreArchiveCommand = errors.New("restore: pre-restore archive command failed")
	ErrPreRestoreAuthorization  = errors.New("restore: pre-restore archive authorization failed")
)

type PreRestoreReadAuthorization struct {
	WorkspaceID string
	ActorUserID string
	SessionID   string
}

type PreRestoreMutationAuthorization struct {
	WorkspaceID string
	ActorUserID string
	SessionID   string
	AssertionID string
	Purpose     string
	AssertedAt  time.Time
	Password    []byte
}

type PreRestoreCommandAuthorizer interface {
	AuthorizePreRestoreRead(context.Context, PreRestoreReadAuthorization) error
	AuthorizePreRestoreMutation(context.Context, PreRestoreMutationAuthorization) error
}

type PreRestoreArchiveCommandServiceConfig struct {
	WorkspaceID  string
	Repository   *PreRestoreArchiveRepository
	Authorizer   PreRestoreCommandAuthorizer
	Transactions PreRestoreArchiveTransactions
	Archives     PreRestoreEncryptedArchiveStore
	Destinations audit.ExportDestinationResolver
	NewJobID     func() (string, error)
	Now          func() time.Time
	Audit        PreRestoreCommandAudit
	hooks        *preRestoreExportHooks
	deleteHooks  *preRestoreDeleteHooks
}

type PreRestoreArchiveCommandService struct {
	workspaceID string
	repository  *PreRestoreArchiveRepository
	authorizer  PreRestoreCommandAuthorizer
	config      PreRestoreArchiveCommandServiceConfig
}

func NewPreRestoreArchiveCommandService(config PreRestoreArchiveCommandServiceConfig) (*PreRestoreArchiveCommandService, error) {
	if !ids.IsCanonicalV7(config.WorkspaceID) || config.Repository == nil || nilInterface(config.Authorizer) {
		return nil, ErrPreRestoreArchiveCommand
	}
	return &PreRestoreArchiveCommandService{workspaceID: config.WorkspaceID, repository: config.Repository,
		authorizer: config.Authorizer, config: config}, nil
}

func (service *PreRestoreArchiveCommandService) GetPreRestoreArchive(
	ctx context.Context,
	request *tammyv1.GetPreRestoreArchiveRequest,
) (*tammyv1.GetPreRestoreArchiveResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	if err := service.authorizeRead(ctx, request.Authentication); err != nil {
		return nil, err
	}
	archive, err := service.repository.Get(ctx, service.workspaceID, request.ArchiveId)
	if err != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	return &tammyv1.GetPreRestoreArchiveResponse{Archive: archive}, nil
}

func (service *PreRestoreArchiveCommandService) ListPreRestoreArchives(
	ctx context.Context,
	request *tammyv1.ListPreRestoreArchivesRequest,
) (*tammyv1.ListPreRestoreArchivesResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || request.Page == nil || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	if err := service.authorizeRead(ctx, request.Authentication); err != nil {
		return nil, err
	}
	state := tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_UNSPECIFIED
	if request.State != nil {
		state = *request.State
	}
	page, err := service.repository.List(ctx, PreRestoreArchiveList{WorkspaceID: service.workspaceID,
		State: state, PageSize: request.Page.PageSize, Cursor: request.Page.Cursor})
	if err != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	return &tammyv1.ListPreRestoreArchivesResponse{Archives: page.Archives, Page: page.Page}, nil
}

func (service *PreRestoreArchiveCommandService) authorizeRead(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	if authentication == nil || !ids.IsCanonicalV7(authentication.ActorUserId) || !ids.IsCanonicalV7(authentication.SessionId) {
		return ErrPreRestoreAuthorization
	}
	request := PreRestoreReadAuthorization{WorkspaceID: service.workspaceID,
		ActorUserID: authentication.ActorUserId, SessionID: authentication.SessionId}
	if err := service.authorizer.AuthorizePreRestoreRead(ctx, request); err != nil {
		return errors.Join(ErrPreRestoreAuthorization, err)
	}
	return nil
}

func (service *PreRestoreArchiveCommandService) authorizeMutation(
	ctx context.Context,
	command *tammyv1.CommandContext,
	password []byte,
	purpose string,
) error {
	if command == nil || command.Authentication == nil || command.FreshFactor == nil ||
		!ids.IsCanonicalV7(command.IdempotencyKey) || !ids.IsCanonicalV7(command.Authentication.ActorUserId) ||
		!ids.IsCanonicalV7(command.Authentication.SessionId) || !ids.IsCanonicalV7(command.FreshFactor.AssertionId) ||
		command.FreshFactor.Purpose != purpose || command.FreshFactor.AssertedAt == nil ||
		!command.FreshFactor.AssertedAt.IsValid() || len(password) == 0 || len(password) > 1024 || service.config.Now == nil {
		return ErrPreRestoreAuthorization
	}
	now := service.config.Now().UTC()
	assertedAt := command.FreshFactor.AssertedAt.AsTime().UTC()
	if now.IsZero() || assertedAt.After(now.Add(time.Minute)) || now.Sub(assertedAt) > 5*time.Minute {
		return ErrPreRestoreAuthorization
	}
	authorization := &PreRestoreMutationAuthorization{WorkspaceID: service.workspaceID,
		ActorUserID: command.Authentication.ActorUserId, SessionID: command.Authentication.SessionId,
		AssertionID: command.FreshFactor.AssertionId, Purpose: purpose, AssertedAt: assertedAt,
		Password: append([]byte(nil), password...)}
	defer zeroMutationAuthorization(authorization)
	if err := service.authorizer.AuthorizePreRestoreMutation(ctx, *authorization); err != nil {
		return errors.Join(ErrPreRestoreAuthorization, err)
	}
	return nil
}

type PreRestoreArchiveTransactions interface {
	Read(context.Context, func(backup.SQLExecutor) error) error
	Mutate(context.Context, func(backup.SQLExecutor) error) error
}

type PreRestoreEncryptedArchiveStore interface {
	ReadEncryptedPreRestoreArchive(context.Context, string, string, []byte) ([]byte, error)
	DeleteEncryptedPreRestoreArchive(context.Context, string, []byte) error
}

type PreRestoreCommandAudit interface {
	AppendPreRestoreArchiveCommand(context.Context, backup.SQLExecutor, *tammyv1.AuditEvent) error
}

var _ = sha256.Size

func zeroMutationAuthorization(authorization *PreRestoreMutationAuthorization) {
	if authorization != nil {
		zeroBytes(authorization.Password)
	}
}
