package restore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var (
	ErrRestoreAuthorization               = errors.New("restore: authorization failed")
	ErrRestoreAuthenticationProvider      = errors.New("restore: authentication provider failed")
	errRestoreAuthenticationProofRejected = errors.New("restore: authentication proof rejected")
	errRestoreAuthenticationPortFailure   = errors.New("restore: authentication port failure")
)

const (
	RestoreAuthenticationPurpose = "workspace_restore"
	maximumRestoreProofAge       = 5 * time.Minute
)

type RestoreAuthenticationMode uint8

const (
	RestoreAuthenticationModeUnspecified RestoreAuthenticationMode = iota
	RestoreAuthenticationModeNormal
	RestoreAuthenticationModeRecovery
)

type RestoreAuthenticationExecutor interface {
	Authenticated() bool
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type RestoreAuthenticationTransactions interface {
	WithinRestoreAuthentication(context.Context, func(context.Context, RestoreAuthenticationExecutor) error) error
}

type RestoreAuthenticationRequest struct {
	Mode           RestoreAuthenticationMode
	WorkspaceID    string
	ActorUserID    string
	Purpose        string
	IssuedAt       time.Time
	ReplayKey      string
	Password       []byte
	TOTP           []byte
	RecoverySecret []byte
}

type AuthenticatedRestoreProof struct {
	Mode                           RestoreAuthenticationMode
	WorkspaceID                    string
	Purpose                        string
	IssuedAt                       time.Time
	ReplayKey                      string
	Actor                          *tammyv1.AuthenticationContext
	SessionID                      string
	ActorAuthenticated             bool
	AdministratorAuthorized        bool
	AdministratorPasswordVerified  bool
	FreshTOTPVerified              bool
	TOTPAssertionPurpose           string
	RecoveryProofVerified          bool
	AdministratorBreakGlassAudited bool
	AuthorizationID                string
	CurrentGeneration              uint64
	CurrentAuditHead               []byte
}

type RestoreAuthenticatorPort interface {
	// VerifyAndConsumeRestoreProof verifies every credential/factor and consumes
	// ReplayKey using executor. It never commits: malformed output causes its
	// caller-owned transaction to roll back the consumption atomically.
	// The Task 8 composition root must supply the Identity-owned implementation;
	// Restore Service has no fallback or audit-trust substitute.
	VerifyAndConsumeRestoreProof(context.Context, RestoreAuthenticationExecutor,
		RestoreAuthenticationRequest) (AuthenticatedRestoreProof, error)
}

type TransactionalRestoreProofVerifierConfig struct {
	Transactions  RestoreAuthenticationTransactions
	Authenticator RestoreAuthenticatorPort
	Now           func() time.Time
}

type transactionalRestoreProofVerifier struct {
	config TransactionalRestoreProofVerifierConfig
}

func NewTransactionalRestoreProofVerifier(
	config TransactionalRestoreProofVerifierConfig,
) (ProofVerifier, error) {
	if nilInterface(config.Transactions) || nilInterface(config.Authenticator) || config.Now == nil {
		return nil, ErrRestoreAuthorization
	}
	return &transactionalRestoreProofVerifier{config: config}, nil
}

func (verifier *transactionalRestoreProofVerifier) AuthorizeRestore(
	ctx context.Context,
	workspaceID string,
	proof RestoreProof,
) (*RestoreAuthorization, error) {
	if verifier == nil || ctx == nil || !ids.IsCanonicalV7(workspaceID) {
		return nil, ErrRestoreAuthorization
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRestoreAuthorization, err)
	}
	request, ok := restoreAuthenticationRequest(workspaceID, proof)
	if !ok {
		return nil, ErrRestoreAuthorization
	}
	defer zeroRestoreAuthenticationRequest(&request)
	now := verifier.config.Now().UTC()
	if now.IsZero() || !freshRestoreProof(request.IssuedAt, now) {
		return nil, ErrRestoreAuthorization
	}
	var authorization *RestoreAuthorization
	err := verifier.config.Transactions.WithinRestoreAuthentication(ctx, func(
		transactionContext context.Context,
		executor RestoreAuthenticationExecutor,
	) error {
		if transactionContext == nil || nilInterface(executor) || !executor.Authenticated() {
			return errRestoreAuthenticationProofRejected
		}
		if err := transactionContext.Err(); err != nil {
			return err
		}
		authenticated, err := verifier.config.Authenticator.VerifyAndConsumeRestoreProof(
			transactionContext, executor, request)
		if err != nil {
			if contextErr := transactionContext.Err(); contextErr != nil {
				return contextErr
			}
			return errRestoreAuthenticationPortFailure
		}
		if contextErr := transactionContext.Err(); contextErr != nil {
			return contextErr
		}
		if !validAuthenticatedRestoreEnvelope(request, authenticated, now) {
			return errRestoreAuthenticationProofRejected
		}
		authorization = &RestoreAuthorization{AuthorizationID: authenticated.AuthorizationID,
			WorkspaceID: authenticated.WorkspaceID, CurrentGeneration: authenticated.CurrentGeneration,
			CurrentAuditHead: append([]byte(nil), authenticated.CurrentAuditHead...)}
		return nil
	})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, errors.Join(ErrRestoreAuthorization, contextErr)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.Join(ErrRestoreAuthorization, err)
		}
		if errors.Is(err, errRestoreAuthenticationProofRejected) {
			return nil, ErrRestoreAuthorization
		}
		return nil, errors.Join(ErrRestoreAuthorization, ErrRestoreAuthenticationProvider)
	}
	if !validAuthorization(authorization, workspaceID) {
		return nil, ErrRestoreAuthorization
	}
	return authorization, nil
}

func restoreAuthenticationRequest(workspaceID string, proof RestoreProof) (RestoreAuthenticationRequest, bool) {
	request := RestoreAuthenticationRequest{WorkspaceID: workspaceID, Purpose: RestoreAuthenticationPurpose}
	switch value := proof.(type) {
	case *AdminTOTPProof:
		if value == nil || !ids.IsCanonicalV7(value.AdminUserID) || !ids.IsCanonicalV7(value.ReplayKey) ||
			len(value.Password) == 0 || len(value.Password) > 4096 || !totpPattern.MatchString(value.TOTP) || value.IssuedAt.IsZero() {
			return RestoreAuthenticationRequest{}, false
		}
		request.Mode = RestoreAuthenticationModeNormal
		request.ActorUserID = value.AdminUserID
		request.IssuedAt = value.IssuedAt.UTC()
		request.ReplayKey = value.ReplayKey
		request.Password = append([]byte(nil), value.Password...)
		request.TOTP = append([]byte(nil), value.TOTP...)
	case *RecoveryProof:
		if value == nil || !ids.IsCanonicalV7(value.ReplayKey) || len(value.RecoverySecret) == 0 ||
			len(value.RecoverySecret) > 4096 || value.IssuedAt.IsZero() {
			return RestoreAuthenticationRequest{}, false
		}
		request.Mode = RestoreAuthenticationModeRecovery
		request.IssuedAt = value.IssuedAt.UTC()
		request.ReplayKey = value.ReplayKey
		request.RecoverySecret = append([]byte(nil), value.RecoverySecret...)
	default:
		return RestoreAuthenticationRequest{}, false
	}
	return request, true
}

func validAuthenticatedRestoreEnvelope(
	request RestoreAuthenticationRequest,
	proof AuthenticatedRestoreProof,
	now time.Time,
) bool {
	if proof.Mode != request.Mode || proof.WorkspaceID != request.WorkspaceID || proof.Purpose != request.Purpose ||
		!proof.IssuedAt.Equal(request.IssuedAt) || !freshRestoreProof(proof.IssuedAt, now) ||
		proof.ReplayKey != request.ReplayKey || proof.Actor == nil || len(proof.Actor.ProtoReflect().GetUnknown()) != 0 ||
		!proof.ActorAuthenticated || !proof.AdministratorAuthorized || !ids.IsCanonicalV7(proof.Actor.ActorUserId) ||
		!ids.IsCanonicalV7(proof.Actor.SessionId) || proof.SessionID != proof.Actor.SessionId ||
		!ids.IsCanonicalV7(proof.AuthorizationID) || proof.CurrentGeneration == 0 || len(proof.CurrentAuditHead) != sha256.Size {
		return false
	}
	switch request.Mode {
	case RestoreAuthenticationModeNormal:
		return proof.Actor.ActorUserId == request.ActorUserID && proof.AdministratorPasswordVerified &&
			proof.FreshTOTPVerified && proof.TOTPAssertionPurpose == RestoreAuthenticationPurpose &&
			!proof.RecoveryProofVerified && !proof.AdministratorBreakGlassAudited
	case RestoreAuthenticationModeRecovery:
		return request.ActorUserID == "" && !proof.AdministratorPasswordVerified && !proof.FreshTOTPVerified &&
			proof.TOTPAssertionPurpose == "" && proof.RecoveryProofVerified && proof.AdministratorBreakGlassAudited
	default:
		return false
	}
}

func freshRestoreProof(issuedAt, now time.Time) bool {
	issuedAt = issuedAt.UTC()
	now = now.UTC()
	return !issuedAt.IsZero() && !now.IsZero() && !issuedAt.After(now) && now.Sub(issuedAt) <= maximumRestoreProofAge
}

func zeroRestoreAuthenticationRequest(request *RestoreAuthenticationRequest) {
	if request == nil {
		return
	}
	zeroBytes(request.Password)
	zeroBytes(request.TOTP)
	zeroBytes(request.RecoverySecret)
	request.Password = nil
	request.TOTP = nil
	request.RecoverySecret = nil
}

func sameRestoreAuthorization(left, right *RestoreAuthorization) bool {
	return left != nil && right != nil && left.AuthorizationID == right.AuthorizationID &&
		left.WorkspaceID == right.WorkspaceID && left.CurrentGeneration == right.CurrentGeneration &&
		subtle.ConstantTimeCompare(left.CurrentAuditHead, right.CurrentAuditHead) == 1
}
