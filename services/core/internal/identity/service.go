package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Config struct {
	Repository            Repository
	Passwords             *workspace.PasswordPolicy
	Clock                 clock.Clock
	Random                io.Reader
	IDs                   *ids.Generator
	Attempts              *workspace.AttemptJournal
	FactorEncryptionKey   []byte
	OnWorkspaceLock       func()
	AdministratorRecovery AdministratorRecoveryPort
	Audit                 AuditPort
	SessionLifecycle      WorkspaceSessionLifecycle
	PasswordVerify        func([]byte, workspace.PasswordVerifier) bool
}

type AuditPort interface {
	Record(context.Context, workspace.MutationExecutor, string, string) error
}

// WorkspaceSessionLifecycle joins the durable workspace state transition to
// the identity transaction, then converges the installation catalogue after
// commit. The Within method must use only the supplied executor and must not
// call back into identity.
type WorkspaceSessionLifecycle interface {
	SessionStartedWithin(context.Context, workspace.MutationExecutor, string) error
	SessionStartedCommitted(context.Context) error
}

type AdministratorRecoveryPort interface {
	RecoverAdministrator(context.Context, *tammyv1.RecoverAdministratorRequest) (*tammyv1.User, error)
}

var errRetainedElection = errors.New("identity: retained result elected")

const retainedElectionConflictRetries = 8

type Service struct {
	mu                    sync.Mutex
	repository            Repository
	passwords             *workspace.PasswordPolicy
	clock                 clock.Clock
	random                io.Reader
	ids                   *ids.Generator
	attempts              *workspace.AttemptJournal
	factorKey             []byte
	onWorkspaceLock       func()
	administratorRecovery AdministratorRecoveryPort
	audit                 AuditPort
	sessionLifecycle      WorkspaceSessionLifecycle
	passwordVerify        func([]byte, workspace.PasswordVerifier) bool
	dummyPassword         workspace.PasswordVerifier
}

type sessionLifecycleContextKey struct{}

var _ tammyv1connect.IdentityServiceHandler = (*Service)(nil)

func NewService(config Config) (*Service, error) {
	if config.Repository == nil || config.Passwords == nil || config.Clock == nil || config.Random == nil ||
		config.IDs == nil || config.Attempts == nil || len(config.FactorEncryptionKey) != 32 || config.Audit == nil || config.SessionLifecycle == nil {
		return nil, ErrRepositoryIntegrity
	}
	dummyPassword, err := config.Passwords.Hash([]byte("tammy-fixed-dummy-password-verifier"))
	if err != nil {
		return nil, ErrRepositoryIntegrity
	}
	verify := config.PasswordVerify
	if verify == nil {
		verify = config.Passwords.Verify
	}
	service := &Service{
		repository: config.Repository, passwords: config.Passwords, clock: config.Clock,
		random: config.Random, ids: config.IDs, attempts: config.Attempts,
		factorKey: append([]byte(nil), config.FactorEncryptionKey...), onWorkspaceLock: config.OnWorkspaceLock,
		administratorRecovery: config.AdministratorRecovery, audit: config.Audit,
		sessionLifecycle: config.SessionLifecycle, passwordVerify: verify, dummyPassword: dummyPassword,
	}
	state, err := service.repository.Load(context.Background())
	if err != nil {
		zero(service.factorKey)
		zeroPasswordVerifier(&service.dummyPassword)
		return nil, err
	}
	now := service.clock.Now().UTC()
	changed := false
	for _, session := range state.Sessions {
		if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
			session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
			session.EndedAt = now
			changed = true
		}
	}
	for _, assertion := range state.Assertions {
		if !assertion.Consumed {
			assertion.Consumed = true
			changed = true
		}
	}
	if changed {
		if err := service.persistState(context.Background(), state, "sessions_invalidated_restart", "startup"); err != nil {
			zero(service.factorKey)
			zeroPasswordVerifier(&service.dummyPassword)
			return nil, err
		}
	}
	return service, nil
}

func (service *Service) Close() error {
	service.mu.Lock()
	defer service.mu.Unlock()
	state, loadErr := service.repository.Load(context.Background())
	if loadErr == nil {
		now := service.clock.Now().UTC()
		for _, session := range state.Sessions {
			if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
				session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
				session.EndedAt = now
			}
		}
		for _, assertion := range state.Assertions {
			assertion.Consumed = true
		}
		loadErr = service.persistState(context.Background(), state, "sessions_invalidated_close", "shutdown")
	}
	zero(service.factorKey)
	service.factorKey = nil
	zeroPasswordVerifier(&service.dummyPassword)
	return loadErr
}

func zeroPasswordVerifier(verifier *workspace.PasswordVerifier) {
	if verifier == nil {
		return
	}
	zero(verifier.Salt)
	zero(verifier.Digest)
	*verifier = workspace.PasswordVerifier{}
}

func samePasswordVerifier(left, right workspace.PasswordVerifier) bool {
	return left.PolicyVersion == right.PolicyVersion && left.MemoryKiB == right.MemoryKiB &&
		left.Iterations == right.Iterations && left.Parallelism == right.Parallelism &&
		len(left.Salt) == len(right.Salt) && len(left.Digest) == len(right.Digest) &&
		subtle.ConstantTimeCompare(left.Salt, right.Salt) == 1 &&
		subtle.ConstantTimeCompare(left.Digest, right.Digest) == 1
}

// persistState is the sole path for committing a state snapshot produced by
// legacy command code. Repository.Mutate reloads inside the transaction and
// records the security audit envelope before the same commit. Command paths
// that already operate directly inside Mutate do not call this helper.
func (service *Service) persistState(ctx context.Context, state repositoryState, mutation, subject string) error {
	return service.repository.Mutate(ctx, func(ctx context.Context, executor workspace.MutationExecutor, current *repositoryState) error {
		cloned, err := cloneState(state)
		if err != nil {
			return err
		}
		*current = cloned
		return service.audit.Record(ctx, executor, mutation, subject)
	})
}

func normalizedUsername(value string) string {
	return cases.Fold().String(norm.NFC.String(value))
}

func validUsername(value string) bool {
	count := len([]rune(value))
	return count >= 1 && count <= 128 && strings.TrimSpace(value) == value
}

func (service *Service) BootstrapAdministrator(ctx context.Context, username, displayName string, password []byte) (*tammyv1.User, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !validUsername(username) || displayName == "" || len([]rune(displayName)) > 128 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	key := normalizedUsername(username)
	if _, exists := state.Usernames[key]; exists {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	verifier, err := service.passwords.Hash(password)
	if err != nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	id, err := service.ids.New()
	if err != nil {
		return nil, err
	}
	record := &userRecord{ID: id, Version: 1, Username: norm.NFC.String(username), DisplayName: norm.NFC.String(displayName),
		State: tammyv1.UserState_USER_STATE_ACTIVE, Roles: []tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN}, Password: verifier}
	state.Users[id] = record
	state.Usernames[key] = id
	if err := service.persistState(ctx, state, "administrator_bootstrapped", id); err != nil {
		return nil, err
	}
	return userProjection(record, state), nil
}

func (service *Service) BootstrapAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor,
	operationID, username, displayName string, password []byte) (*tammyv1.User, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if executor == nil || !ids.IsCanonicalV7(operationID) || !validUsername(username) || displayName == "" || len([]rune(displayName)) > 128 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := loadRepositoryStateFrom(ctx, executor)
	if err != nil {
		return nil, err
	}
	digest := service.semanticDigest([]byte("bootstrap_administrator"), []byte(normalizedUsername(username)), []byte(norm.NFC.String(displayName)), password)
	if retained, ok := state.Idempotency[operationID]; ok {
		if retained.Command != "bootstrap_administrator" || retained.SemanticHash != digest {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		record := state.Users[retained.UserID]
		if record == nil {
			return nil, ErrRepositoryIntegrity
		}
		return userProjection(record, state), nil
	}
	key := normalizedUsername(username)
	if _, exists := state.Usernames[key]; exists {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	verifier, err := service.passwords.Hash(password)
	if err != nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	id, err := service.ids.New()
	if err != nil {
		return nil, err
	}
	record := &userRecord{ID: id, Version: 1, Username: norm.NFC.String(username), DisplayName: norm.NFC.String(displayName),
		State: tammyv1.UserState_USER_STATE_ACTIVE, Roles: []tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN}, Password: verifier}
	state.Users[id] = record
	state.Usernames[key] = id
	state.Idempotency[operationID] = idempotencyRecord{Command: "bootstrap_administrator", SemanticHash: digest, UserID: id}
	if err := service.audit.Record(ctx, executor, "administrator_bootstrapped", id); err != nil {
		return nil, err
	}
	if err := saveRepositoryStateTo(ctx, executor, state); err != nil {
		return nil, err
	}
	return userProjection(record, state), nil
}

func (service *Service) SignIn(ctx context.Context, request *connect.Request[tammyv1.SignInRequest]) (*connect.Response[tammyv1.SignInResponse], error) {
	if ctx.Value(sessionLifecycleContextKey{}) != nil {
		return nil, ErrRepositoryIntegrity
	}
	service.mu.Lock()
	locked := true
	defer func() {
		if locked {
			service.mu.Unlock()
		}
	}()
	if request == nil || request.Msg == nil || request.Msg.Password == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if !validUsername(request.Msg.Username) {
		service.passwordVerify(request.Msg.Password.Utf8, service.dummyPassword)
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	userID := state.Usernames[normalizedUsername(request.Msg.Username)]
	record := state.Users[userID]
	now := service.clock.Now().UTC()
	if record == nil || (record.State != tammyv1.UserState_USER_STATE_ACTIVE && record.State != tammyv1.UserState_USER_STATE_AUTHENTICATION_LOCKED) {
		service.passwordVerify(request.Msg.Password.Utf8, service.dummyPassword)
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if record.LockedUntil.After(now) {
		service.passwordVerify(request.Msg.Password.Utf8, service.dummyPassword)
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	passwordMatches := service.passwordVerify(request.Msg.Password.Utf8, record.Password)
	loadedPassword := record.Password.Clone()
	defer zeroPasswordVerifier(&loadedPassword)
	if passwordMatches {
		// The post-unlock journal anchor is stored in the same SQLCipher database,
		// so it must advance before opening the identity transaction. A journal
		// failure therefore prevents any session/workspace state from committing.
		if err := service.attempts.Success("identity_sign_in", record.ID); err != nil {
			return nil, err
		}
	}
	var response *tammyv1.SignInResponse
	authenticationFailed := false
	err = service.repository.Mutate(ctx, func(ctx context.Context, executor workspace.MutationExecutor, current *repositoryState) error {
		record = current.Users[userID]
		if record == nil || current.Usernames[normalizedUsername(request.Msg.Username)] != userID ||
			!samePasswordVerifier(loadedPassword, record.Password) || record.LockedUntil.After(now) ||
			(record.State != tammyv1.UserState_USER_STATE_ACTIVE && record.State != tammyv1.UserState_USER_STATE_AUTHENTICATION_LOCKED) {
			return ErrRepositoryConflict
		}
		if !record.LockedUntil.IsZero() {
			record.LockedUntil = time.Time{}
			record.SignInFailures = nil
			record.State = tammyv1.UserState_USER_STATE_ACTIVE
		}
		if !passwordMatches {
			cutoff := now.Add(-15 * time.Minute)
			failures := record.SignInFailures[:0]
			for _, instant := range record.SignInFailures {
				if instant.After(cutoff) {
					failures = append(failures, instant)
				}
			}
			record.SignInFailures = append(failures, now)
			if len(record.SignInFailures) >= 5 {
				record.LockedUntil = now.Add(15 * time.Minute)
				record.State = tammyv1.UserState_USER_STATE_AUTHENTICATION_LOCKED
			}
			authenticationFailed = true
			return service.audit.Record(ctx, executor, "sign_in_failed", record.ID)
		}
		record.SignInFailures = nil
		record.LockedUntil = time.Time{}
		record.State = tammyv1.UserState_USER_STATE_ACTIVE
		for _, session := range current.Sessions {
			if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
				session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
				session.EndedAt = now
			}
		}
		sessionID, err := service.ids.New()
		if err != nil {
			return err
		}
		session := &sessionRecord{ID: sessionID, UserID: record.ID, State: tammyv1.SessionState_SESSION_STATE_ACTIVE,
			CreatedAt: now, LastActive: now, ExpiresAt: now.Add(30 * time.Minute)}
		current.Sessions[sessionID] = session
		lifecycleCtx := context.WithValue(ctx, sessionLifecycleContextKey{}, true)
		if err := service.sessionLifecycle.SessionStartedWithin(lifecycleCtx, executor, sessionID); err != nil {
			return err
		}
		if err := service.audit.Record(ctx, executor, "session_started", sessionID); err != nil {
			return err
		}
		response = &tammyv1.SignInResponse{User: userProjection(record, *current), Session: sessionProjection(session)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if authenticationFailed {
		if _, err := service.attempts.Failure("identity_sign_in", record.ID, workspace.AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}); err != nil {
			return nil, err
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	service.mu.Unlock()
	locked = false
	if err := service.sessionLifecycle.SessionStartedCommitted(ctx); err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) CreateUser(ctx context.Context, request *connect.Request[tammyv1.CreateUserRequest]) (*connect.Response[tammyv1.CreateUserResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		!ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) || !validUsername(request.Msg.Username) ||
		request.Msg.DisplayName == "" || len([]rune(request.Msg.DisplayName)) > 128 || !validRoles(request.Msg.Roles) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateReadOnlyLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	if err := authorisation.Authorize(actor.Roles, authorisation.ActionManageUsers); err != nil {
		return nil, err
	}
	digest := service.semanticDigest([]byte("create_user"), []byte(normalizedUsername(request.Msg.Username)), []byte(request.Msg.DisplayName), roleBytes(request.Msg.Roles))
	operationKey := request.Msg.CommandContext.IdempotencyKey
	if retained, ok := state.Idempotency[operationKey]; ok {
		if retained.Command != "create_user" || retained.SemanticHash != digest || retained.ActorUserID != actor.ID {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		var response tammyv1.CreateUserResponse
		if err := service.openRetainedResponse(operationKey, retained.ResponseEncrypted, &response); err != nil {
			return nil, err
		}
		return connect.NewResponse(&response), nil
	}
	actor, _, err = service.authenticateLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	key := normalizedUsername(request.Msg.Username)
	if _, exists := state.Usernames[key]; exists {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	userID, err := service.ids.New()
	if err != nil {
		return nil, err
	}
	activationBytes := make([]byte, 16)
	if _, err := io.ReadFull(service.random, activationBytes); err != nil {
		return nil, err
	}
	defer zero(activationBytes)
	activation := []byte(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(activationBytes))
	defer zero(activation)
	encrypted, err := service.sealSecret(activation, "activation/"+userID)
	if err != nil {
		return nil, err
	}
	now := service.clock.Now().UTC()
	record := &userRecord{ID: userID, Version: 1, Username: norm.NFC.String(request.Msg.Username), DisplayName: norm.NFC.String(request.Msg.DisplayName),
		State: tammyv1.UserState_USER_STATE_PENDING_ACTIVATION, Roles: sortedRoles(request.Msg.Roles),
		ActivationHash: service.secretHash("activation", activation), ActivationEncrypted: encrypted, ActivationExpires: now.Add(24 * time.Hour)}
	state.Users[userID] = record
	state.Usernames[key] = userID
	response := &tammyv1.CreateUserResponse{User: userProjection(record, state),
		ActivationCode: &tammyv1.OneTimeSecretOutput{Utf8: append([]byte(nil), activation...)}, ExpiresAt: timestamppb.New(record.ActivationExpires)}
	retainedResponse, err := service.sealRetainedResponse(operationKey, response)
	if err != nil {
		return nil, err
	}
	state.Idempotency[operationKey] = idempotencyRecord{Command: "create_user", SemanticHash: digest, ActorUserID: actor.ID, UserID: userID,
		ResponseEncrypted: retainedResponse}
	if err := service.persistState(ctx, state, "user_created", userID); err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) ActivateUser(ctx context.Context, request *connect.Request[tammyv1.ActivateUserRequest]) (*connect.Response[tammyv1.ActivateUserResponse], error) {
	if ctx.Value(sessionLifecycleContextKey{}) != nil {
		return nil, ErrRepositoryIntegrity
	}
	service.mu.Lock()
	locked := true
	defer func() {
		if locked {
			service.mu.Unlock()
		}
	}()
	if request == nil || request.Msg == nil || request.Msg.ActivationCode == nil || request.Msg.NewPassword == nil || !validUsername(request.Msg.Username) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	record := state.Users[state.Usernames[normalizedUsername(request.Msg.Username)]]
	if record == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	providedHash := service.secretHash("activation", request.Msg.ActivationCode.Utf8)
	if record.State == tammyv1.UserState_USER_STATE_ACTIVE && len(record.ActivationConsumedHash) == sha256.Size &&
		subtle.ConstantTimeCompare(providedHash, record.ActivationConsumedHash) == 1 {
		session := state.Sessions[record.ActivationSessionID]
		if session != nil {
			return connect.NewResponse(&tammyv1.ActivateUserResponse{User: userProjection(record, state), Session: sessionProjection(session)}), nil
		}
	}
	if record.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION {
		if _, err := service.attempts.Failure("identity_activation", record.ID,
			workspace.AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}); err != nil {
			return nil, err
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	now := service.clock.Now().UTC()
	validCode := len(record.ActivationHash) == sha256.Size && subtle.ConstantTimeCompare(providedHash, record.ActivationHash) == 1
	if !validCode || !now.Before(record.ActivationExpires) || record.ActivationFails >= 5 {
		record.ActivationFails++
		if record.ActivationFails >= 5 || !now.Before(record.ActivationExpires) {
			zero(record.ActivationEncrypted)
			record.ActivationEncrypted = nil
			record.ActivationHash = nil
		}
		if err := service.persistState(ctx, state, "activation_failed", record.ID); err != nil {
			return nil, err
		}
		if _, err := service.attempts.Failure("identity_activation", record.ID, workspace.AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}); err != nil {
			return nil, err
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	history := append([]workspace.PasswordVerifier{record.Password}, record.PasswordHistory...)
	if service.passwords.Reused(request.Msg.NewPassword.Utf8, history) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	verifier, err := service.passwords.Hash(request.Msg.NewPassword.Utf8)
	if err != nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	defer zeroPasswordVerifier(&verifier)
	if err := service.attempts.Success("identity_activation", record.ID); err != nil {
		return nil, err
	}
	loadedVersion := record.Version
	loadedActivationHash := append([]byte(nil), record.ActivationHash...)
	defer zero(loadedActivationHash)
	var response *tammyv1.ActivateUserResponse
	err = service.repository.Mutate(ctx, func(ctx context.Context, executor workspace.MutationExecutor, current *repositoryState) error {
		record = current.Users[current.Usernames[normalizedUsername(request.Msg.Username)]]
		if record == nil || record.Version != loadedVersion || record.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION ||
			len(record.ActivationHash) != sha256.Size || subtle.ConstantTimeCompare(record.ActivationHash, loadedActivationHash) != 1 ||
			!now.Before(record.ActivationExpires) || record.ActivationFails >= 5 {
			return ErrRepositoryConflict
		}
		history := append([]workspace.PasswordVerifier{record.Password}, record.PasswordHistory...)
		if service.passwords.Reused(request.Msg.NewPassword.Utf8, history) {
			return faults.New(faults.CodeValidation, nil)
		}
		record.PasswordHistory = workspace.RetainPasswordHistory(record.Password, record.PasswordHistory, 5)
		record.Password = verifier.Clone()
		record.State = tammyv1.UserState_USER_STATE_ACTIVE
		record.Version++
		record.ActivationHash = nil
		record.ActivationConsumedHash = append([]byte(nil), providedHash...)
		zero(record.ActivationEncrypted)
		record.ActivationEncrypted = nil
		record.ActivationFails = 0
		for _, session := range current.Sessions {
			if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
				session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
				session.EndedAt = now
			}
		}
		sessionID, err := service.ids.New()
		if err != nil {
			return err
		}
		session := &sessionRecord{ID: sessionID, UserID: record.ID, State: tammyv1.SessionState_SESSION_STATE_ACTIVE,
			CreatedAt: now, LastActive: now, ExpiresAt: now.Add(30 * time.Minute)}
		current.Sessions[sessionID] = session
		record.ActivationSessionID = sessionID
		lifecycleCtx := context.WithValue(ctx, sessionLifecycleContextKey{}, true)
		if err := service.sessionLifecycle.SessionStartedWithin(lifecycleCtx, executor, sessionID); err != nil {
			return err
		}
		if err := service.audit.Record(ctx, executor, "user_activated", record.ID); err != nil {
			return err
		}
		response = &tammyv1.ActivateUserResponse{User: userProjection(record, *current), Session: sessionProjection(session)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	service.mu.Unlock()
	locked = false
	if err := service.sessionLifecycle.SessionStartedCommitted(ctx); err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) ChangePassword(ctx context.Context, request *connect.Request[tammyv1.ChangePasswordRequest]) (*connect.Response[tammyv1.ChangePasswordResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.CurrentPassword == nil ||
		request.Msg.NewPassword == nil || !ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) || request.Msg.ExpectedVersion == 0 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	operationKey := request.Msg.CommandContext.IdempotencyKey
	if retained, ok := state.Idempotency[operationKey]; ok {
		actor, _, authErr := service.authenticateRetainedPasswordReplayLocked(&state, request.Msg.CommandContext.Authentication, retained)
		if authErr != nil {
			return nil, authErr
		}
		digest := service.semanticDigest([]byte("change_password"), []byte(actor.ID),
			[]byte(request.Msg.CurrentPassword.Utf8), []byte(request.Msg.NewPassword.Utf8), []byte(fmtUint(request.Msg.ExpectedVersion)))
		if retained.Command != "change_password" || retained.SemanticHash != digest || retained.ActorUserID != actor.ID {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		var response tammyv1.ChangePasswordResponse
		if err := service.openRetainedResponse(operationKey, retained.ResponseEncrypted, &response); err != nil {
			return nil, err
		}
		return connect.NewResponse(&response), nil
	}
	actor, session, err := service.authenticateLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	if actor.Version != request.Msg.ExpectedVersion {
		return nil, faults.New(faults.CodeStaleVersion, nil)
	}
	digest := service.semanticDigest([]byte("change_password"), []byte(actor.ID),
		[]byte(request.Msg.CurrentPassword.Utf8), []byte(request.Msg.NewPassword.Utf8), []byte(fmtUint(request.Msg.ExpectedVersion)))
	if !service.passwords.Verify(request.Msg.CurrentPassword.Utf8, actor.Password) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	history := append([]workspace.PasswordVerifier{actor.Password}, actor.PasswordHistory...)
	if service.passwords.Reused(request.Msg.NewPassword.Utf8, history) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	verifier, err := service.passwords.Hash(request.Msg.NewPassword.Utf8)
	if err != nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	actor.PasswordHistory = workspace.RetainPasswordHistory(actor.Password, actor.PasswordHistory, 5)
	actor.Password = verifier
	actor.Version++
	now := service.clock.Now().UTC()
	session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
	session.EndedAt = now
	response := &tammyv1.ChangePasswordResponse{User: userProjection(actor, state), InvalidatedSession: sessionProjection(session)}
	retainedResponse, err := service.sealRetainedResponse(operationKey, response)
	if err != nil {
		return nil, err
	}
	state.Idempotency[operationKey] = idempotencyRecord{Command: "change_password", SemanticHash: digest, ActorUserID: actor.ID, UserID: actor.ID,
		SessionID: session.ID, ResponseEncrypted: retainedResponse}
	if err := service.persistState(ctx, state, "password_changed", actor.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) EnrolTOTP(ctx context.Context, request *connect.Request[tammyv1.EnrolTOTPRequest]) (*connect.Response[tammyv1.EnrolTOTPResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.CurrentPassword == nil ||
		!ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateReadOnlyLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	digest := service.semanticDigest([]byte("enrol_totp"), []byte(actor.ID), request.Msg.CurrentPassword.Utf8)
	operationKey := request.Msg.CommandContext.IdempotencyKey
	if retained, ok := state.Idempotency[operationKey]; ok {
		response, err := service.electEnrolTOTP(operationKey, digest, actor.ID, retained)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(response), nil
	}
	var response *tammyv1.EnrolTOTPResponse
	for conflictAttempt := 0; ; conflictAttempt++ {
		err = service.repository.Mutate(ctx, func(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
			actor, _, err := service.authenticateReadOnlyLocked(state, request.Msg.CommandContext.Authentication)
			if err != nil {
				return err
			}
			digest := service.semanticDigest([]byte("enrol_totp"), []byte(actor.ID), request.Msg.CurrentPassword.Utf8)
			if retained, ok := state.Idempotency[operationKey]; ok {
				response, err = service.electEnrolTOTP(operationKey, digest, actor.ID, retained)
				if err != nil {
					return err
				}
				return errRetainedElection
			}
			actor, _, err = service.authenticateLocked(state, request.Msg.CommandContext.Authentication)
			if err != nil {
				return err
			}
			if !service.passwords.Verify(request.Msg.CurrentPassword.Utf8, actor.Password) {
				return faults.New(faults.CodeAuthenticationRequired, nil)
			}
			for _, factor := range state.Factors {
				if factor.UserID == actor.ID && factor.State != tammyv1.FactorState_FACTOR_STATE_DISABLED {
					return faults.New(faults.CodeValidation, nil)
				}
			}
			factorID, err := service.ids.New()
			if err != nil {
				return err
			}
			secret, display, err := GenerateTOTPSecret(service.random)
			if err != nil {
				return err
			}
			defer zero(secret)
			defer zero(display)
			encrypted, err := service.sealSecret(secret, "totp/"+actor.ID+"/"+factorID)
			if err != nil {
				return err
			}
			factor := &factorRecord{ID: factorID, UserID: actor.ID, Version: 1,
				State: tammyv1.FactorState_FACTOR_STATE_PENDING_CONFIRMATION, CreatedAt: service.clock.Now().UTC(),
				EncryptedSecret: encrypted, LastCounter: -1}
			state.Factors[factorID] = factor
			response = &tammyv1.EnrolTOTPResponse{Factor: factorProjection(factor),
				ProvisioningSecret: &tammyv1.OneTimeSecretOutput{Utf8: append([]byte(nil), display...)}}
			retainedResponse, err := service.sealRetainedResponse(operationKey, response)
			if err != nil {
				return err
			}
			state.Idempotency[operationKey] = idempotencyRecord{Command: "enrol_totp", SemanticHash: digest, ActorUserID: actor.ID, UserID: actor.ID,
				FactorID: factorID, ResponseEncrypted: retainedResponse}
			return service.audit.Record(ctx, executor, "totp_enrolled", factorID)
		})
		if !errors.Is(err, ErrRepositoryConflict) || conflictAttempt == retainedElectionConflictRetries-1 {
			break
		}
		zeroEnrolTOTPResponse(response)
		response = nil
		time.Sleep(time.Duration(conflictAttempt+1) * 5 * time.Millisecond)
	}
	if errors.Is(err, errRetainedElection) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func zeroEnrolTOTPResponse(response *tammyv1.EnrolTOTPResponse) {
	if response != nil && response.ProvisioningSecret != nil {
		zero(response.ProvisioningSecret.Utf8)
	}
}

func (service *Service) electEnrolTOTP(operationKey, digest, actorID string,
	retained idempotencyRecord) (*tammyv1.EnrolTOTPResponse, error) {
	if retained.Command != "enrol_totp" || retained.SemanticHash != digest || retained.UserID != actorID || retained.ActorUserID != actorID {
		return nil, faults.New(faults.CodeIdempotencyConflict, nil)
	}
	var response tammyv1.EnrolTOTPResponse
	if err := service.openRetainedResponse(operationKey, retained.ResponseEncrypted, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (service *Service) ConfirmTOTP(ctx context.Context, request *connect.Request[tammyv1.ConfirmTOTPRequest]) (*connect.Response[tammyv1.ConfirmTOTPResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil || request.Msg.Code == nil || !ids.IsCanonicalV7(request.Msg.FactorId) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateLocked(&state, request.Msg.Authentication)
	if err != nil {
		return nil, err
	}
	policy := workspace.AttemptPolicy{Limit: 5, Window: 5 * time.Minute, Cooldown: 15 * time.Minute}
	factor := state.Factors[request.Msg.FactorId]
	if factor == nil || factor.UserID != actor.ID {
		if _, journalErr := service.attempts.Failure("totp_confirmation", actor.ID, policy); journalErr != nil {
			return nil, journalErr
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if factor.State != tammyv1.FactorState_FACTOR_STATE_PENDING_CONFIRMATION && factor.State != tammyv1.FactorState_FACTOR_STATE_ENABLED {
		if _, journalErr := service.attempts.Failure("totp_confirmation", actor.ID, policy); journalErr != nil {
			return nil, journalErr
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	decision, err := service.attempts.Status("totp_confirmation", actor.ID, policy)
	if err != nil || decision.CoolingDown(service.clock.Now()) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	secret, err := service.openSecret(factor.EncryptedSecret, "totp/"+factor.UserID+"/"+factor.ID)
	if err != nil {
		return nil, ErrRepositoryIntegrity
	}
	defer zero(secret)
	if factor.State == tammyv1.FactorState_FACTOR_STATE_ENABLED {
		counter, verifyErr := VerifyTOTP(secret, request.Msg.Code.Value, service.clock.Now(), factor.LastCounter-1)
		if verifyErr == nil && counter == factor.LastCounter {
			return connect.NewResponse(&tammyv1.ConfirmTOTPResponse{Factor: factorProjection(factor)}), nil
		}
		if _, journalErr := service.attempts.Failure("totp_confirmation", actor.ID, policy); journalErr != nil {
			return nil, journalErr
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	counter, verifyErr := VerifyTOTP(secret, request.Msg.Code.Value, service.clock.Now(), factor.LastCounter)
	if verifyErr != nil {
		if _, journalErr := service.attempts.Failure("totp_confirmation", actor.ID, policy); journalErr != nil {
			return nil, journalErr
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	factor.LastCounter = counter
	factor.State = tammyv1.FactorState_FACTOR_STATE_ENABLED
	factor.Version++
	if err := service.attempts.Success("totp_confirmation", actor.ID); err != nil {
		return nil, err
	}
	if err := service.persistState(ctx, state, "totp_confirmed", factor.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ConfirmTOTPResponse{Factor: factorProjection(factor)}), nil
}

func (service *Service) AssertTOTP(ctx context.Context, request *connect.Request[tammyv1.AssertTOTPRequest]) (*connect.Response[tammyv1.AssertTOTPResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil || request.Msg.Code == nil ||
		request.Msg.Purpose == "" || len(request.Msg.Purpose) > 96 {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, session, err := service.authenticateLocked(&state, request.Msg.Authentication)
	if err != nil {
		return nil, err
	}
	var factor *factorRecord
	for _, candidate := range state.Factors {
		if candidate.UserID == actor.ID && candidate.State == tammyv1.FactorState_FACTOR_STATE_ENABLED {
			factor = candidate
			break
		}
	}
	if factor == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	policy := workspace.AttemptPolicy{Limit: 5, Window: 5 * time.Minute, Cooldown: 15 * time.Minute}
	decision, err := service.attempts.Status("totp_assertion", actor.ID, policy)
	if err != nil {
		return nil, err
	}
	if decision.CoolingDown(service.clock.Now()) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	secret, err := service.openSecret(factor.EncryptedSecret, "totp/"+factor.UserID+"/"+factor.ID)
	if err != nil {
		return nil, ErrRepositoryIntegrity
	}
	defer zero(secret)
	counter, verifyErr := VerifyTOTP(secret, request.Msg.Code.Value, service.clock.Now(), factor.LastCounter)
	if verifyErr != nil {
		if _, journalErr := service.attempts.Failure("totp_assertion", actor.ID, policy); journalErr != nil {
			return nil, journalErr
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	factor.LastCounter = counter
	for _, prior := range state.Assertions {
		if prior.UserID == actor.ID && !prior.Consumed {
			prior.Consumed = true
		}
	}
	assertionID, err := service.ids.New()
	if err != nil {
		return nil, err
	}
	now := service.clock.Now().UTC()
	assertion := &assertionRecord{ID: assertionID, UserID: actor.ID, SessionID: session.ID, Purpose: request.Msg.Purpose, Asserted: now}
	state.Assertions[assertionID] = assertion
	if err := service.attempts.Success("totp_assertion", actor.ID); err != nil {
		return nil, err
	}
	if err := service.persistState(ctx, state, "totp_asserted", assertionID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.AssertTOTPResponse{FreshFactor: &tammyv1.FreshFactorContext{
		AssertionId: assertionID, Purpose: request.Msg.Purpose, AssertedAt: timestamppb.New(now),
	}}), nil
}

func (service *Service) DisableTOTP(ctx context.Context, request *connect.Request[tammyv1.DisableTOTPRequest]) (*connect.Response[tammyv1.DisableTOTPResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.CurrentPassword == nil ||
		!ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.Msg.FactorId) || request.Msg.ExpectedVersion == 0 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateReadOnlyLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	digest := service.semanticDigest([]byte("disable_totp"), []byte(actor.ID), []byte(request.Msg.FactorId),
		[]byte(fmtUint(request.Msg.ExpectedVersion)), request.Msg.CurrentPassword.Utf8)
	operationKey := request.Msg.CommandContext.IdempotencyKey
	if retained, ok := state.Idempotency[operationKey]; ok {
		response, err := service.electDisableTOTP(operationKey, digest, actor.ID, retained)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(response), nil
	}
	var response *tammyv1.DisableTOTPResponse
	for conflictAttempt := 0; ; conflictAttempt++ {
		err = service.repository.Mutate(ctx, func(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
			actor, _, err := service.authenticateReadOnlyLocked(state, request.Msg.CommandContext.Authentication)
			if err != nil {
				return err
			}
			digest := service.semanticDigest([]byte("disable_totp"), []byte(actor.ID), []byte(request.Msg.FactorId),
				[]byte(fmtUint(request.Msg.ExpectedVersion)), request.Msg.CurrentPassword.Utf8)
			if retained, ok := state.Idempotency[operationKey]; ok {
				response, err = service.electDisableTOTP(operationKey, digest, actor.ID, retained)
				if err != nil {
					return err
				}
				return errRetainedElection
			}
			actor, _, err = service.authenticateLocked(state, request.Msg.CommandContext.Authentication)
			if err != nil {
				return err
			}
			factor := state.Factors[request.Msg.FactorId]
			if factor == nil || factor.UserID != actor.ID || factor.State != tammyv1.FactorState_FACTOR_STATE_ENABLED {
				return faults.New(faults.CodeValidation, nil)
			}
			if factor.Version != request.Msg.ExpectedVersion {
				return faults.New(faults.CodeStaleVersion, nil)
			}
			if !service.passwords.Verify(request.Msg.CurrentPassword.Utf8, actor.Password) {
				return faults.New(faults.CodeAuthenticationRequired, nil)
			}
			if err := service.consumeAssertionLocked(state, request.Msg.CommandContext.Authentication, request.Msg.CommandContext.FreshFactor, "disable_totp"); err != nil {
				return err
			}
			factor.State = tammyv1.FactorState_FACTOR_STATE_DISABLED
			factor.Version++
			zero(factor.EncryptedSecret)
			factor.EncryptedSecret = nil
			for _, assertion := range state.Assertions {
				if assertion.UserID == actor.ID {
					assertion.Consumed = true
				}
			}
			response = &tammyv1.DisableTOTPResponse{Factor: factorProjection(factor)}
			retainedResponse, err := service.sealRetainedResponse(operationKey, response)
			if err != nil {
				return err
			}
			state.Idempotency[operationKey] = idempotencyRecord{Command: "disable_totp", SemanticHash: digest, ActorUserID: actor.ID, UserID: actor.ID,
				FactorID: factor.ID, ResponseEncrypted: retainedResponse}
			return service.audit.Record(ctx, executor, "totp_disabled", factor.ID)
		})
		if !errors.Is(err, ErrRepositoryConflict) || conflictAttempt == retainedElectionConflictRetries-1 {
			break
		}
		response = nil
		time.Sleep(time.Duration(conflictAttempt+1) * 5 * time.Millisecond)
	}
	if errors.Is(err, errRetainedElection) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) electDisableTOTP(operationKey, digest, actorID string,
	retained idempotencyRecord) (*tammyv1.DisableTOTPResponse, error) {
	if retained.Command != "disable_totp" || retained.SemanticHash != digest || retained.UserID != actorID || retained.ActorUserID != actorID {
		return nil, faults.New(faults.CodeIdempotencyConflict, nil)
	}
	var response tammyv1.DisableTOTPResponse
	if err := service.openRetainedResponse(operationKey, retained.ResponseEncrypted, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (service *Service) ResetUserAuthentication(ctx context.Context, request *connect.Request[tammyv1.ResetUserAuthenticationRequest]) (*connect.Response[tammyv1.ResetUserAuthenticationResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		!ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.Msg.UserId) || request.Msg.ExpectedVersion == 0 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateReadOnlyLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	if err := authorisation.Authorize(actor.Roles, authorisation.ActionManageUsers); err != nil {
		return nil, err
	}
	digest := service.semanticDigest([]byte("reset_user_authentication"), []byte(request.Msg.UserId), []byte(fmtUint(request.Msg.ExpectedVersion)))
	operationKey := request.Msg.CommandContext.IdempotencyKey
	if retained, ok := state.Idempotency[operationKey]; ok {
		if retained.Command != "reset_user_authentication" || retained.SemanticHash != digest || retained.ActorUserID != actor.ID {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		var response tammyv1.ResetUserAuthenticationResponse
		if err := service.openRetainedResponse(operationKey, retained.ResponseEncrypted, &response); err != nil {
			return nil, err
		}
		return connect.NewResponse(&response), nil
	}
	actor, _, err = service.authenticateLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	target := state.Users[request.Msg.UserId]
	if target == nil {
		return nil, faults.New(faults.CodeNotFound, nil)
	}
	if target.Version != request.Msg.ExpectedVersion {
		return nil, faults.New(faults.CodeStaleVersion, nil)
	}
	if hasRole(target.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) && activeAdministratorCount(state) <= 1 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	if err := service.consumeAssertionLocked(&state, request.Msg.CommandContext.Authentication,
		request.Msg.CommandContext.FreshFactor, "reset_user_authentication"); err != nil {
		return nil, err
	}
	activationBytes := make([]byte, 16)
	if _, err := io.ReadFull(service.random, activationBytes); err != nil {
		return nil, err
	}
	defer zero(activationBytes)
	activation := []byte(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(activationBytes))
	defer zero(activation)
	encrypted, err := service.sealSecret(activation, "activation/"+target.ID)
	if err != nil {
		return nil, err
	}
	target.State = tammyv1.UserState_USER_STATE_PENDING_ACTIVATION
	target.Version++
	target.ActivationHash = service.secretHash("activation", activation)
	target.ActivationConsumedHash = nil
	target.ActivationSessionID = ""
	zero(target.ActivationEncrypted)
	target.ActivationEncrypted = encrypted
	target.ActivationExpires = service.clock.Now().UTC().Add(24 * time.Hour)
	target.ActivationFails = 0
	target.LockedUntil = time.Time{}
	target.SignInFailures = nil
	now := service.clock.Now().UTC()
	for _, factor := range state.Factors {
		if factor.UserID == target.ID && factor.State != tammyv1.FactorState_FACTOR_STATE_DISABLED {
			factor.State = tammyv1.FactorState_FACTOR_STATE_DISABLED
			factor.Version++
			zero(factor.EncryptedSecret)
			factor.EncryptedSecret = nil
		}
	}
	for _, session := range state.Sessions {
		if session.UserID == target.ID && session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
			session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
			session.EndedAt = now
		}
	}
	for _, assertion := range state.Assertions {
		if assertion.UserID == target.ID {
			assertion.Consumed = true
		}
	}
	response := &tammyv1.ResetUserAuthenticationResponse{User: userProjection(target, state),
		ActivationCode: &tammyv1.OneTimeSecretOutput{Utf8: append([]byte(nil), activation...)}, ExpiresAt: timestamppb.New(target.ActivationExpires)}
	retainedResponse, err := service.sealRetainedResponse(operationKey, response)
	if err != nil {
		return nil, err
	}
	state.Idempotency[operationKey] = idempotencyRecord{Command: "reset_user_authentication", SemanticHash: digest, ActorUserID: actor.ID, UserID: target.ID,
		ResponseEncrypted: retainedResponse}
	if err := service.persistState(ctx, state, "user_authentication_reset", target.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) AssignRoles(ctx context.Context, request *connect.Request[tammyv1.AssignRolesRequest]) (*connect.Response[tammyv1.AssignRolesResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		!ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.Msg.UserId) ||
		request.Msg.ExpectedVersion == 0 || !validRoles(request.Msg.Roles) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateReadOnlyLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	if err := authorisation.Authorize(actor.Roles, authorisation.ActionManageUsers); err != nil {
		return nil, err
	}
	digest := service.semanticDigest([]byte("assign_roles"), []byte(request.Msg.UserId), []byte(fmtUint(request.Msg.ExpectedVersion)), roleBytes(request.Msg.Roles))
	operationKey := request.Msg.CommandContext.IdempotencyKey
	if retained, ok := state.Idempotency[operationKey]; ok {
		if retained.Command != "assign_roles" || retained.SemanticHash != digest || retained.ActorUserID != actor.ID {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		var response tammyv1.AssignRolesResponse
		if err := service.openRetainedResponse(operationKey, retained.ResponseEncrypted, &response); err != nil {
			return nil, err
		}
		return connect.NewResponse(&response), nil
	}
	actor, _, err = service.authenticateLocked(&state, request.Msg.CommandContext.Authentication)
	if err != nil {
		return nil, err
	}
	target := state.Users[request.Msg.UserId]
	if target == nil {
		return nil, faults.New(faults.CodeNotFound, nil)
	}
	if target.Version != request.Msg.ExpectedVersion {
		return nil, faults.New(faults.CodeStaleVersion, nil)
	}
	if hasRole(target.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) && !hasRole(request.Msg.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) && activeAdministratorCount(state) <= 1 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	target.Roles = sortedRoles(request.Msg.Roles)
	target.Version++
	response := &tammyv1.AssignRolesResponse{User: userProjection(target, state)}
	retainedResponse, err := service.sealRetainedResponse(operationKey, response)
	if err != nil {
		return nil, err
	}
	state.Idempotency[operationKey] = idempotencyRecord{Command: "assign_roles", SemanticHash: digest, ActorUserID: actor.ID, UserID: target.ID,
		ResponseEncrypted: retainedResponse}
	if err := service.persistState(ctx, state, "roles_assigned", target.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) ExpireIdle(ctx context.Context) error {
	service.mu.Lock()
	lockWorkspace := false
	defer func() {
		service.mu.Unlock()
		if lockWorkspace && service.onWorkspaceLock != nil {
			service.onWorkspaceLock()
		}
	}()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	now := service.clock.Now().UTC()
	expired := false
	for _, session := range state.Sessions {
		if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE && !now.Before(session.ExpiresAt) {
			session.State = tammyv1.SessionState_SESSION_STATE_EXPIRED
			session.EndedAt = now
			expired = true
		}
	}
	if expired {
		if err := service.persistState(ctx, state, "sessions_expired", "idle_timeout"); err != nil {
			return err
		}
		lockWorkspace = true
	}
	return nil
}

func (service *Service) HandleOSLock(ctx context.Context) error {
	service.mu.Lock()
	lockWorkspace := false
	defer func() {
		service.mu.Unlock()
		if lockWorkspace && service.onWorkspaceLock != nil {
			service.onWorkspaceLock()
		}
	}()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	now := service.clock.Now().UTC()
	changed := false
	for _, session := range state.Sessions {
		if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
			session.State = tammyv1.SessionState_SESSION_STATE_EXPIRED
			session.EndedAt = now
			changed = true
		}
	}
	if changed {
		if err := service.persistState(ctx, state, "sessions_expired", "os_lock"); err != nil {
			return err
		}
		lockWorkspace = true
	}
	return nil
}

func (service *Service) GetSession(ctx context.Context, request *connect.Request[tammyv1.GetSessionRequest]) (*connect.Response[tammyv1.GetSessionResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	_, session, err := service.authenticateLocked(&state, request.Msg.Authentication)
	if err != nil {
		if saveErr := service.persistState(ctx, state, "session_expired", request.Msg.Authentication.GetSessionId()); saveErr != nil {
			return nil, saveErr
		}
		return nil, err
	}
	if err := service.persistState(ctx, state, "session_touched", session.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.GetSessionResponse{Session: sessionProjection(session)}), nil
}

func (service *Service) SignOut(ctx context.Context, request *connect.Request[tammyv1.SignOutRequest]) (*connect.Response[tammyv1.SignOutResponse], error) {
	service.mu.Lock()
	lockWorkspace := false
	defer func() {
		service.mu.Unlock()
		if lockWorkspace && service.onWorkspaceLock != nil {
			service.onWorkspaceLock()
		}
	}()
	if request == nil || request.Msg == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	_, session, err := service.authenticateLocked(&state, request.Msg.Authentication)
	if err != nil {
		return nil, err
	}
	session.State = tammyv1.SessionState_SESSION_STATE_SIGNED_OUT
	session.EndedAt = service.clock.Now().UTC()
	if err := service.persistState(ctx, state, "session_signed_out", session.ID); err != nil {
		return nil, err
	}
	lockWorkspace = true
	return connect.NewResponse(&tammyv1.SignOutResponse{Session: sessionProjection(session)}), nil
}

func (service *Service) GetCurrentUser(ctx context.Context, request *connect.Request[tammyv1.GetCurrentUserRequest]) (*connect.Response[tammyv1.GetCurrentUserResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	user, _, err := service.authenticateLocked(&state, request.Msg.Authentication)
	if err != nil {
		return nil, err
	}
	if err := service.persistState(ctx, state, "session_touched", request.Msg.Authentication.GetSessionId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.GetCurrentUserResponse{User: userProjection(user, state)}), nil
}

func (service *Service) GetUser(ctx context.Context, request *connect.Request[tammyv1.GetUserRequest]) (*connect.Response[tammyv1.GetUserResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || !ids.IsCanonicalV7(request.Msg.UserId) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateLocked(&state, request.Msg.Authentication)
	if err != nil {
		return nil, err
	}
	if actor.ID != request.Msg.UserId {
		if err := authorisation.Authorize(actor.Roles, authorisation.ActionManageUsers); err != nil {
			return nil, err
		}
	}
	target := state.Users[request.Msg.UserId]
	if target == nil {
		return nil, faults.New(faults.CodeNotFound, nil)
	}
	if err := service.persistState(ctx, state, "session_touched", request.Msg.Authentication.GetSessionId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.GetUserResponse{User: userProjection(target, state)}), nil
}

func (service *Service) ListUsers(ctx context.Context, request *connect.Request[tammyv1.ListUsersRequest]) (*connect.Response[tammyv1.ListUsersResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.Page == nil || request.Msg.Page.GetCursor() != "" {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := service.authenticateLocked(&state, request.Msg.Authentication)
	if err != nil {
		return nil, err
	}
	if err := authorisation.Authorize(actor.Roles, authorisation.ActionManageUsers); err != nil {
		return nil, err
	}
	users := make([]*tammyv1.User, 0, len(state.Users))
	for _, record := range state.Users {
		if request.Msg.State != nil && record.State != *request.Msg.State {
			continue
		}
		if request.Msg.Role != nil && !hasRole(record.Roles, *request.Msg.Role) {
			continue
		}
		users = append(users, userProjection(record, state))
	}
	sort.Slice(users, func(i, j int) bool {
		left, right := normalizedUsername(users[i].Username), normalizedUsername(users[j].Username)
		if left == right {
			return users[i].Id < users[j].Id
		}
		return left < right
	})
	limit := int(request.Msg.Page.PageSize)
	if limit == 0 {
		limit = 50
	}
	if limit > 200 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	if len(users) > limit {
		users = users[:limit]
	}
	if err := service.persistState(ctx, state, "session_touched", request.Msg.Authentication.GetSessionId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ListUsersResponse{Users: users,
		Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(users))}}), nil
}

func (service *Service) RequireAdministrator(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	actor, _, err := service.authenticateLocked(&state, authentication)
	if err != nil {
		return err
	}
	if err := authorisation.Authorize(actor.Roles, authorisation.ActionManageWorkspace); err != nil {
		return err
	}
	return service.persistState(ctx, state, "session_touched", authentication.GetSessionId())
}

// RequireAdministratorReadOnly validates an active administrator session
// without extending it. Workspace command handlers use this before retained
// result election so replay, conflict, and pre-transaction failures are pure.
func (service *Service) RequireAdministratorReadOnly(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	actor, _, err := service.authenticateReadOnlyLocked(&state, authentication)
	if err != nil {
		return err
	}
	return authorisation.Authorize(actor.Roles, authorisation.ActionManageWorkspace)
}

// RequireActiveSessionReadOnly validates any active workspace user session
// without extending it or writing an audit row. It is used for command
// preflight and terminal replay paths that must remain pure.
func (service *Service) RequireActiveSessionReadOnly(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	_, _, err = service.authenticateReadOnlyLocked(&state, authentication)
	return err
}

// ValidateAdministratorReplayBinding permits only the original administrator
// session invalidated by a terminal workspace command to read that command's
// retained result. The caller supplies bindings from the retained workspace
// operation; this method never touches or reactivates the session.
func (service *Service) ValidateAdministratorReplayBinding(ctx context.Context, authentication *tammyv1.AuthenticationContext,
	boundActorUserID, boundSessionID string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if authentication == nil || boundActorUserID == "" || boundSessionID == "" ||
		authentication.ActorUserId != boundActorUserID || authentication.SessionId != boundSessionID {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	session := state.Sessions[boundSessionID]
	actor := state.Users[boundActorUserID]
	if session == nil || actor == nil || session.UserID != boundActorUserID ||
		session.State != tammyv1.SessionState_SESSION_STATE_INVALIDATED || actor.State != tammyv1.UserState_USER_STATE_ACTIVE {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return authorisation.Authorize(actor.Roles, authorisation.ActionManageWorkspace)
}

func (service *Service) RequireAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor,
	authentication *tammyv1.AuthenticationContext) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if executor == nil {
		return ErrRepositoryIntegrity
	}
	state, err := loadRepositoryStateFrom(ctx, executor)
	if err != nil {
		return err
	}
	actor, _, err := service.authenticateLocked(&state, authentication)
	if err != nil {
		return err
	}
	if err := authorisation.Authorize(actor.Roles, authorisation.ActionManageWorkspace); err != nil {
		return err
	}
	if err := service.audit.Record(ctx, executor, "session_touched", authentication.GetSessionId()); err != nil {
		return err
	}
	return saveRepositoryStateTo(ctx, executor, state)
}

// RequireActiveSessionWithin revalidates and touches any active workspace user
// session inside the caller's SQLCipher transaction. The session state and its
// audit row therefore commit or roll back with the workspace mutation.
func (service *Service) RequireActiveSessionWithin(ctx context.Context, executor workspace.MutationExecutor,
	authentication *tammyv1.AuthenticationContext) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if executor == nil {
		return ErrRepositoryIntegrity
	}
	state, err := loadRepositoryStateFrom(ctx, executor)
	if err != nil {
		return err
	}
	if _, _, err := service.authenticateLocked(&state, authentication); err != nil {
		return err
	}
	if err := service.audit.Record(ctx, executor, "session_touched", authentication.GetSessionId()); err != nil {
		return err
	}
	return saveRepositoryStateTo(ctx, executor, state)
}

func (service *Service) IsActiveAdministrator(ctx context.Context, userID string) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return false
	}
	user := state.Users[userID]
	return user != nil && user.State == tammyv1.UserState_USER_STATE_ACTIVE && hasRole(user.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN)
}

func (service *Service) IsActiveAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, userID string) (bool, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if executor == nil {
		return false, ErrRepositoryIntegrity
	}
	state, err := loadRepositoryStateFrom(ctx, executor)
	if err != nil {
		return false, err
	}
	user := state.Users[userID]
	return user != nil && user.State == tammyv1.UserState_USER_STATE_ACTIVE && hasRole(user.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN), nil
}

func (service *Service) InvalidateAllSessions(ctx context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	now := service.clock.Now().UTC()
	for _, session := range state.Sessions {
		if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
			session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
			session.EndedAt = now
		}
	}
	for _, assertion := range state.Assertions {
		assertion.Consumed = true
	}
	return service.persistState(ctx, state, "sessions_invalidated", "workspace")
}

func (service *Service) InvalidateAllSessionsWithin(ctx context.Context, executor workspace.MutationExecutor) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if executor == nil {
		return ErrRepositoryIntegrity
	}
	state, err := loadRepositoryStateFrom(ctx, executor)
	if err != nil {
		return err
	}
	now := service.clock.Now().UTC()
	for _, session := range state.Sessions {
		if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
			session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
			session.EndedAt = now
		}
	}
	for _, assertion := range state.Assertions {
		assertion.Consumed = true
	}
	if err := service.audit.Record(ctx, executor, "sessions_invalidated", "workspace"); err != nil {
		return err
	}
	return saveRepositoryStateTo(ctx, executor, state)
}

// BreakGlassResetAdministrator applies the identity half of administrator
// recovery after the workspace owner has verified the recovery secret. The
// operation is retained so a crash-safe workspace recovery replay cannot reset
// credentials or factors twice.
func (service *Service) BreakGlassResetAdministrator(ctx context.Context, operationID, username string, password []byte) (*tammyv1.User, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.breakGlassResetAdministratorLocked(ctx, nil, operationID, username, password)
}

func (service *Service) BreakGlassResetAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor,
	operationID, username string, password []byte) (*tammyv1.User, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if executor == nil {
		return nil, ErrRepositoryIntegrity
	}
	return service.breakGlassResetAdministratorLocked(ctx, executor, operationID, username, password)
}

func (service *Service) breakGlassResetAdministratorLocked(ctx context.Context, executor workspace.MutationExecutor,
	operationID, username string, password []byte) (*tammyv1.User, error) {
	if !ids.IsCanonicalV7(operationID) || !validUsername(username) || password == nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	var state repositoryState
	var err error
	if executor == nil {
		state, err = service.repository.Load(ctx)
	} else if repository, ok := service.repository.(executorRepository); ok {
		state, err = repository.LoadFrom(ctx, executor)
	} else {
		return nil, ErrRepositoryIntegrity
	}
	if err != nil {
		return nil, err
	}
	digest := service.semanticDigest([]byte("administrator_recovery"), []byte(normalizedUsername(username)), password)
	if retained, ok := state.Idempotency[operationID]; ok {
		if retained.Command != "administrator_recovery" || retained.SemanticHash != digest {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		var response tammyv1.User
		if err := service.openRetainedResponse(operationID, retained.ResponseEncrypted, &response); err != nil {
			return nil, err
		}
		return &response, nil
	}
	record := state.Users[state.Usernames[normalizedUsername(username)]]
	if record == nil || !hasRole(record.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	history := append([]workspace.PasswordVerifier{record.Password}, record.PasswordHistory...)
	if service.passwords.Reused(password, history) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	verifier, err := service.passwords.Hash(password)
	if err != nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	record.PasswordHistory = workspace.RetainPasswordHistory(record.Password, record.PasswordHistory, 5)
	record.Password = verifier
	record.State = tammyv1.UserState_USER_STATE_ACTIVE
	record.Version++
	record.ActivationHash = nil
	record.ActivationConsumedHash = nil
	record.ActivationSessionID = ""
	zero(record.ActivationEncrypted)
	record.ActivationEncrypted = nil
	record.ActivationExpires = time.Time{}
	record.ActivationFails = 0
	record.SignInFailures = nil
	record.LockedUntil = time.Time{}
	now := service.clock.Now().UTC()
	for _, factor := range state.Factors {
		if factor.UserID == record.ID && factor.State != tammyv1.FactorState_FACTOR_STATE_DISABLED {
			factor.State = tammyv1.FactorState_FACTOR_STATE_DISABLED
			factor.Version++
			zero(factor.EncryptedSecret)
			factor.EncryptedSecret = nil
		}
	}
	for _, session := range state.Sessions {
		if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
			session.State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
			session.EndedAt = now
		}
	}
	for _, assertion := range state.Assertions {
		assertion.Consumed = true
	}
	response := userProjection(record, state)
	retainedResponse, err := service.sealRetainedResponse(operationID, response)
	if err != nil {
		return nil, err
	}
	state.Idempotency[operationID] = idempotencyRecord{Command: "administrator_recovery", SemanticHash: digest,
		UserID: record.ID, ResponseEncrypted: retainedResponse}
	if executor == nil {
		err = service.persistState(ctx, state, "administrator_recovered", record.ID)
	} else {
		repository, ok := service.repository.(executorRepository)
		if !ok {
			return nil, ErrRepositoryIntegrity
		}
		if auditErr := service.audit.Record(ctx, executor, "administrator_recovered", record.ID); auditErr != nil {
			return nil, auditErr
		}
		err = repository.SaveTo(ctx, executor, state)
	}
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (service *Service) RecoverAdministrator(ctx context.Context, request *connect.Request[tammyv1.RecoverAdministratorRequest]) (*connect.Response[tammyv1.RecoverAdministratorResponse], error) {
	if request == nil || request.Msg == nil || service.administratorRecovery == nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	user, err := service.administratorRecovery.RecoverAdministrator(ctx, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.RecoverAdministratorResponse{User: cloneUser(user)}), nil
}

func hasRole(roles []tammyv1.Role, wanted tammyv1.Role) bool {
	for _, role := range roles {
		if role == wanted {
			return true
		}
	}
	return false
}

func activeAdministratorCount(state repositoryState) int {
	count := 0
	for _, user := range state.Users {
		if user.State == tammyv1.UserState_USER_STATE_ACTIVE && hasRole(user.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) {
			count++
		}
	}
	return count
}

func factorProjection(record *factorRecord) *tammyv1.Factor {
	if record == nil {
		return nil
	}
	return &tammyv1.Factor{Id: record.ID, UserId: record.UserID, Version: record.Version, State: record.State, CreatedAt: timestamppb.New(record.CreatedAt)}
}

func (service *Service) consumeAssertionLocked(state *repositoryState, authentication *tammyv1.AuthenticationContext,
	marker *tammyv1.FreshFactorContext, purpose string) error {
	if err := authorisation.ValidateFreshFactor(marker, purpose, service.clock.Now()); err != nil {
		return err
	}
	assertion := state.Assertions[marker.AssertionId]
	if assertion == nil || assertion.Consumed || authentication == nil || assertion.UserID != authentication.ActorUserId ||
		assertion.SessionID != authentication.SessionId || assertion.Purpose != purpose || !assertion.Asserted.Equal(marker.AssertedAt.AsTime()) {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	assertion.Consumed = true
	return nil
}

// ConsumeFreshFactor atomically consumes one assertion for a collaborating
// high-risk command owner such as workspace or organisations.
func (service *Service) ConsumeFreshFactor(ctx context.Context, authentication *tammyv1.AuthenticationContext,
	marker *tammyv1.FreshFactorContext, purpose string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.repository.Load(ctx)
	if err != nil {
		return err
	}
	if _, _, err := service.authenticateLocked(&state, authentication); err != nil {
		return err
	}
	if err := service.consumeAssertionLocked(&state, authentication, marker, purpose); err != nil {
		return err
	}
	return service.persistState(ctx, state, "fresh_factor_consumed", marker.AssertionId)
}

func (service *Service) ConsumeFreshFactorWithin(ctx context.Context, executor workspace.MutationExecutor,
	authentication *tammyv1.AuthenticationContext, marker *tammyv1.FreshFactorContext, purpose string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	repository, ok := service.repository.(executorRepository)
	if !ok || executor == nil {
		return ErrRepositoryIntegrity
	}
	state, err := repository.LoadFrom(ctx, executor)
	if err != nil {
		return err
	}
	if _, _, err := service.authenticateLocked(&state, authentication); err != nil {
		return err
	}
	if err := service.consumeAssertionLocked(&state, authentication, marker, purpose); err != nil {
		return err
	}
	if err := service.audit.Record(ctx, executor, "fresh_factor_consumed", marker.AssertionId); err != nil {
		return err
	}
	return repository.SaveTo(ctx, executor, state)
}

func fmtUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[position:])
}

func (service *Service) authenticateLocked(state *repositoryState, authentication *tammyv1.AuthenticationContext) (*userRecord, *sessionRecord, error) {
	user, session, err := service.authenticateReadOnlyLocked(state, authentication)
	if err != nil {
		if authentication != nil {
			session = state.Sessions[authentication.SessionId]
			now := service.clock.Now().UTC()
			if session != nil && session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE && !now.Before(session.ExpiresAt) {
				session.State = tammyv1.SessionState_SESSION_STATE_EXPIRED
				session.EndedAt = now
			}
		}
		return nil, nil, err
	}
	now := service.clock.Now().UTC()
	session.LastActive = now
	session.ExpiresAt = now.Add(30 * time.Minute)
	return user, session, nil
}

func (service *Service) authenticateReadOnlyLocked(state *repositoryState, authentication *tammyv1.AuthenticationContext) (*userRecord, *sessionRecord, error) {
	if authentication == nil {
		return nil, nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	session := state.Sessions[authentication.SessionId]
	now := service.clock.Now().UTC()
	if session == nil || session.UserID != authentication.ActorUserId || session.State != tammyv1.SessionState_SESSION_STATE_ACTIVE || !now.Before(session.ExpiresAt) {
		return nil, nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	user := state.Users[session.UserID]
	if user == nil || user.State != tammyv1.UserState_USER_STATE_ACTIVE {
		return nil, nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return user, session, nil
}

func (service *Service) authenticateRetainedPasswordReplayLocked(state *repositoryState,
	authentication *tammyv1.AuthenticationContext, retained idempotencyRecord) (*userRecord, *sessionRecord, error) {
	if authentication == nil {
		return nil, nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	actor, session, err := service.authenticateReadOnlyLocked(state, authentication)
	if err == nil {
		return actor, session, nil
	}
	if retained.ActorUserID == "" || retained.ActorUserID != authentication.ActorUserId || retained.SessionID != authentication.SessionId {
		return nil, nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	session = state.Sessions[authentication.SessionId]
	actor = state.Users[authentication.ActorUserId]
	if session == nil || actor == nil || session.UserID != actor.ID || session.State != tammyv1.SessionState_SESSION_STATE_INVALIDATED ||
		actor.State != tammyv1.UserState_USER_STATE_ACTIVE {
		return nil, nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return actor, session, nil
}

func validRoles(roles []tammyv1.Role) bool {
	if len(roles) == 0 || len(roles) > 4 {
		return false
	}
	seen := make(map[tammyv1.Role]bool, len(roles))
	for _, role := range roles {
		if role <= tammyv1.Role_ROLE_UNSPECIFIED || role > tammyv1.Role_ROLE_AUDITOR || seen[role] {
			return false
		}
		seen[role] = true
	}
	return true
}

func sortedRoles(roles []tammyv1.Role) []tammyv1.Role {
	result := append([]tammyv1.Role(nil), roles...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func roleBytes(roles []tammyv1.Role) []byte {
	roles = sortedRoles(roles)
	result := make([]byte, len(roles))
	for index, role := range roles {
		result[index] = byte(role)
	}
	return result
}

func (service *Service) secretHash(purpose string, secret []byte) []byte {
	digest := hmac.New(sha256.New, service.factorKey)
	_, _ = digest.Write([]byte("tammy.identity." + purpose + ".v1\x00"))
	_, _ = digest.Write(secret)
	return digest.Sum(nil)
}

func (service *Service) sealSecret(secret []byte, aad string) ([]byte, error) {
	block, err := aes.NewCipher(service.factorKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(service.random, nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, secret, []byte(aad))...), nil
}

func (service *Service) openSecret(encrypted []byte, aad string) ([]byte, error) {
	block, err := aes.NewCipher(service.factorKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrRepositoryIntegrity
	}
	return aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(aad))
}

func (service *Service) sealRetainedResponse(operationID string, response proto.Message) ([]byte, error) {
	if !ids.IsCanonicalV7(operationID) || response == nil {
		return nil, ErrRepositoryIntegrity
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(response)
	if err != nil {
		return nil, ErrRepositoryIntegrity
	}
	defer zero(payload)
	return service.sealSecret(payload, "idempotency/"+operationID)
}

func (service *Service) openRetainedResponse(operationID string, encrypted []byte, response proto.Message) error {
	if !ids.IsCanonicalV7(operationID) || response == nil {
		return ErrRepositoryIntegrity
	}
	payload, err := service.openSecret(encrypted, "idempotency/"+operationID)
	if err != nil {
		return ErrRepositoryIntegrity
	}
	defer zero(payload)
	if err := proto.Unmarshal(payload, response); err != nil {
		return ErrRepositoryIntegrity
	}
	return nil
}

func userProjection(record *userRecord, state repositoryState) *tammyv1.User {
	if record == nil {
		return nil
	}
	roles := append([]tammyv1.Role(nil), record.Roles...)
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	user := &tammyv1.User{Id: record.ID, Version: record.Version, Username: record.Username, DisplayName: record.DisplayName,
		State: record.State, Roles: roles}
	if record.LockedUntil.After(time.Time{}) {
		user.AuthenticationLockedUntil = timestamppb.New(record.LockedUntil)
	}
	for _, factor := range state.Factors {
		if factor.UserID == record.ID && factor.State != tammyv1.FactorState_FACTOR_STATE_DISABLED {
			value := factor.State
			user.FactorState = &value
		}
	}
	return user
}

func sessionProjection(record *sessionRecord) *tammyv1.Session {
	if record == nil {
		return nil
	}
	projection := &tammyv1.Session{Id: record.ID, UserId: record.UserID, State: record.State,
		CreatedAt: timestamppb.New(record.CreatedAt), ExpiresAt: timestamppb.New(record.ExpiresAt)}
	if !record.EndedAt.IsZero() {
		projection.EndedAt = timestamppb.New(record.EndedAt)
	}
	return projection
}

func cloneUser(user *tammyv1.User) *tammyv1.User {
	if user == nil {
		return nil
	}
	return proto.Clone(user).(*tammyv1.User)
}

func (service *Service) semanticDigest(parts ...[]byte) string {
	digest := hmac.New(sha256.New, service.factorKey)
	_, _ = digest.Write([]byte("tammy.identity.operation.v1\x00"))
	for _, part := range parts {
		_, _ = digest.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		_, _ = digest.Write(part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}
