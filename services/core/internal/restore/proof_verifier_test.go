package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

type restoreAuthenticationExecutorHarness struct{ authenticated bool }

func (executor restoreAuthenticationExecutorHarness) Authenticated() bool {
	return executor.authenticated
}
func (restoreAuthenticationExecutorHarness) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (restoreAuthenticationExecutorHarness) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}

type restoreAuthenticationTransactionsHarness struct {
	executor   RestoreAuthenticationExecutor
	committed  int
	rolledBack int
}

func (harness *restoreAuthenticationTransactionsHarness) WithinRestoreAuthentication(
	ctx context.Context,
	work func(context.Context, RestoreAuthenticationExecutor) error,
) error {
	if err := work(ctx, harness.executor); err != nil {
		harness.rolledBack++
		return err
	}
	harness.committed++
	return nil
}

type restoreAuthenticatorPortFunc func(
	context.Context,
	RestoreAuthenticationExecutor,
	RestoreAuthenticationRequest,
) (AuthenticatedRestoreProof, error)

func (function restoreAuthenticatorPortFunc) VerifyAndConsumeRestoreProof(
	ctx context.Context,
	executor RestoreAuthenticationExecutor,
	request RestoreAuthenticationRequest,
) (AuthenticatedRestoreProof, error) {
	return function(ctx, executor, request)
}

func TestTransactionalRestoreProofVerifierAuthorizesExactNormalAndRecoveryModes(t *testing.T) {
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	workspaceID := "018f0000-0000-7000-8000-000000000011"
	actorID := "018f0000-0000-7000-8000-000000000012"
	sessionID := "018f0000-0000-7000-8000-000000000013"
	replayKey := "018f0000-0000-7000-8000-000000000014"
	authorizationID := "018f0000-0000-7000-8000-000000000015"
	issuedAt := now.Add(-4 * time.Minute)
	for _, testCase := range []struct {
		name  string
		proof RestoreProof
		mode  RestoreAuthenticationMode
	}{
		{name: "normal", mode: RestoreAuthenticationModeNormal, proof: &AdminTOTPProof{AdminUserID: actorID,
			Password: []byte("administrator-password"), TOTP: "123456", IssuedAt: issuedAt, ReplayKey: replayKey}},
		{name: "recovery", mode: RestoreAuthenticationModeRecovery, proof: &RecoveryProof{
			RecoverySecret: []byte("recovery-secret"), IssuedAt: issuedAt, ReplayKey: replayKey}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			transactions := &restoreAuthenticationTransactionsHarness{executor: restoreAuthenticationExecutorHarness{authenticated: true}}
			var providerSecret []byte
			verifier, err := NewTransactionalRestoreProofVerifier(TransactionalRestoreProofVerifierConfig{
				Transactions: transactions, Now: func() time.Time { return now },
				Authenticator: restoreAuthenticatorPortFunc(func(_ context.Context, executor RestoreAuthenticationExecutor,
					request RestoreAuthenticationRequest,
				) (AuthenticatedRestoreProof, error) {
					if !executor.Authenticated() || request.Mode != testCase.mode || request.WorkspaceID != workspaceID ||
						request.Purpose != RestoreAuthenticationPurpose || !request.IssuedAt.Equal(issuedAt) || request.ReplayKey != replayKey {
						t.Fatalf("authentication request=%#v executor_authenticated=%t", request, executor.Authenticated())
					}
					providerSecret = request.Password
					if testCase.mode == RestoreAuthenticationModeRecovery {
						providerSecret = request.RecoverySecret
					}
					return validAuthenticatedRestoreProof(testCase.mode, workspaceID, actorID, sessionID,
						replayKey, authorizationID, issuedAt), nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			authorization, err := verifier.AuthorizeRestore(context.Background(), workspaceID, testCase.proof)
			if err != nil || authorization == nil || authorization.AuthorizationID != authorizationID ||
				authorization.WorkspaceID != workspaceID || authorization.CurrentGeneration != 5 ||
				!bytes.Equal(authorization.CurrentAuditHead, bytes.Repeat([]byte{0x51}, sha256.Size)) {
				t.Fatalf("authorization=%#v error=%v", authorization, err)
			}
			if transactions.committed != 1 || transactions.rolledBack != 0 {
				t.Fatalf("transaction commits=%d rollbacks=%d", transactions.committed, transactions.rolledBack)
			}
			for _, value := range providerSecret {
				if value != 0 {
					t.Fatal("transaction-owned provider secret copy was not zeroed")
				}
			}
		})
	}
}

func TestTransactionalRestoreProofVerifierRejectsMalformedProviderEnvelopeInsideTransaction(t *testing.T) {
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	workspaceID := "018f0000-0000-7000-8000-000000000011"
	actorID := "018f0000-0000-7000-8000-000000000012"
	sessionID := "018f0000-0000-7000-8000-000000000013"
	replayKey := "018f0000-0000-7000-8000-000000000014"
	authorizationID := "018f0000-0000-7000-8000-000000000015"
	issuedAt := now.Add(-time.Minute)
	base := validAuthenticatedRestoreProof(RestoreAuthenticationModeNormal, workspaceID, actorID, sessionID,
		replayKey, authorizationID, issuedAt)
	for _, testCase := range []struct {
		name   string
		mutate func(*AuthenticatedRestoreProof)
	}{
		{name: "mixed", mutate: func(proof *AuthenticatedRestoreProof) { proof.RecoveryProofVerified = true }},
		{name: "partial", mutate: func(proof *AuthenticatedRestoreProof) { proof.FreshTOTPVerified = false }},
		{name: "wrong_kind", mutate: func(proof *AuthenticatedRestoreProof) { proof.Mode = RestoreAuthenticationModeRecovery }},
		{name: "wrong_workspace", mutate: func(proof *AuthenticatedRestoreProof) { proof.WorkspaceID = "018f0000-0000-7000-8000-000000000099" }},
		{name: "wrong_actor", mutate: func(proof *AuthenticatedRestoreProof) {
			proof.Actor.ActorUserId = "018f0000-0000-7000-8000-000000000099"
		}},
		{name: "wrong_session", mutate: func(proof *AuthenticatedRestoreProof) { proof.SessionID = "018f0000-0000-7000-8000-000000000099" }},
		{name: "wrong_purpose", mutate: func(proof *AuthenticatedRestoreProof) { proof.Purpose = "audit_export" }},
		{name: "stale", mutate: func(proof *AuthenticatedRestoreProof) { proof.IssuedAt = now.Add(-6 * time.Minute) }},
		{name: "future", mutate: func(proof *AuthenticatedRestoreProof) { proof.IssuedAt = now.Add(time.Second) }},
		{name: "unauthenticated_actor", mutate: func(proof *AuthenticatedRestoreProof) { proof.ActorAuthenticated = false }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			transactions := &restoreAuthenticationTransactionsHarness{executor: restoreAuthenticationExecutorHarness{authenticated: true}}
			verifier, err := NewTransactionalRestoreProofVerifier(TransactionalRestoreProofVerifierConfig{
				Transactions: transactions, Now: func() time.Time { return now },
				Authenticator: restoreAuthenticatorPortFunc(func(context.Context, RestoreAuthenticationExecutor,
					RestoreAuthenticationRequest,
				) (AuthenticatedRestoreProof, error) {
					proof := base
					proof.Actor = &tammyv1.AuthenticationContext{ActorUserId: base.Actor.ActorUserId, SessionId: base.Actor.SessionId}
					testCase.mutate(&proof)
					return proof, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := verifier.AuthorizeRestore(context.Background(), workspaceID, &AdminTOTPProof{
				AdminUserID: actorID, Password: []byte("administrator-password"), TOTP: "123456",
				IssuedAt: issuedAt, ReplayKey: replayKey})
			if result != nil || !errors.Is(err, ErrRestoreAuthorization) || transactions.committed != 0 || transactions.rolledBack != 1 {
				t.Fatalf("result=%#v error=%v commits=%d rollbacks=%d", result, err,
					transactions.committed, transactions.rolledBack)
			}
		})
	}
}

func TestTransactionalRestoreProofVerifierRejectsInputTimeAndHidesProviderDetails(t *testing.T) {
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	workspaceID := "018f0000-0000-7000-8000-000000000011"
	actorID := "018f0000-0000-7000-8000-000000000012"
	replayKey := "018f0000-0000-7000-8000-000000000014"
	for _, issuedAt := range []time.Time{now.Add(-5*time.Minute - time.Nanosecond), now.Add(time.Nanosecond)} {
		called := false
		transactions := &restoreAuthenticationTransactionsHarness{executor: restoreAuthenticationExecutorHarness{authenticated: true}}
		verifier, err := NewTransactionalRestoreProofVerifier(TransactionalRestoreProofVerifierConfig{
			Transactions: transactions, Now: func() time.Time { return now },
			Authenticator: restoreAuthenticatorPortFunc(func(context.Context, RestoreAuthenticationExecutor,
				RestoreAuthenticationRequest,
			) (AuthenticatedRestoreProof, error) {
				called = true
				return AuthenticatedRestoreProof{}, nil
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := verifier.AuthorizeRestore(context.Background(), workspaceID, &AdminTOTPProof{
			AdminUserID: actorID, Password: []byte("administrator-password"), TOTP: "123456",
			IssuedAt: issuedAt, ReplayKey: replayKey}); result != nil || !errors.Is(err, ErrRestoreAuthorization) ||
			called || transactions.committed != 0 || transactions.rolledBack != 0 {
			t.Fatalf("issued_at=%s result=%#v error=%v called=%t commits=%d rollbacks=%d", issuedAt,
				result, err, called, transactions.committed, transactions.rolledBack)
		}
	}

	providerDetail := errors.New("provider detail must not escape")
	transactions := &restoreAuthenticationTransactionsHarness{executor: restoreAuthenticationExecutorHarness{authenticated: true}}
	verifier, err := NewTransactionalRestoreProofVerifier(TransactionalRestoreProofVerifierConfig{
		Transactions: transactions, Now: func() time.Time { return now },
		Authenticator: restoreAuthenticatorPortFunc(func(context.Context, RestoreAuthenticationExecutor,
			RestoreAuthenticationRequest,
		) (AuthenticatedRestoreProof, error) {
			return AuthenticatedRestoreProof{}, providerDetail
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	proof := &AdminTOTPProof{AdminUserID: actorID, Password: []byte("administrator-password"), TOTP: "123456",
		IssuedAt: now.Add(-time.Minute), ReplayKey: replayKey}
	if result, err := verifier.AuthorizeRestore(context.Background(), workspaceID, proof); result != nil ||
		!errors.Is(err, ErrRestoreAuthorization) || !errors.Is(err, ErrRestoreAuthenticationProvider) ||
		errors.Is(err, providerDetail) || transactions.committed != 0 || transactions.rolledBack != 1 {
		t.Fatalf("provider result=%#v error=%v commits=%d rollbacks=%d", result, err,
			transactions.committed, transactions.rolledBack)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := verifier.AuthorizeRestore(canceled, workspaceID, proof); result != nil ||
		!errors.Is(err, ErrRestoreAuthorization) || !errors.Is(err, context.Canceled) ||
		errors.Is(err, ErrRestoreAuthenticationProvider) {
		t.Fatalf("canceled result=%#v error=%v", result, err)
	}
}

func TestRestoreServiceConstructionRequiresProductionProofVerifierWithoutFallback(t *testing.T) {
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	transactions := &restoreAuthenticationTransactionsHarness{executor: restoreAuthenticationExecutorHarness{authenticated: true}}
	verifier, err := NewTransactionalRestoreProofVerifier(TransactionalRestoreProofVerifierConfig{
		Transactions: transactions, Now: func() time.Time { return now },
		Authenticator: restoreAuthenticatorPortFunc(func(context.Context, RestoreAuthenticationExecutor,
			RestoreAuthenticationRequest,
		) (AuthenticatedRestoreProof, error) {
			return AuthenticatedRestoreProof{}, ErrRestoreAuthorization
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "workspace", Version: 1,
		Validator: validatorFunc(func(context.Context, ValidationInput) error { return nil })}})
	if err != nil {
		t.Fatal(err)
	}
	harness := new(restoreOrchestrationHarness)
	config := harness.config(registry)
	config.Proofs = verifier
	if service, err := NewService(config); err != nil || service == nil {
		t.Fatalf("production verifier service=%#v error=%v", service, err)
	}
	config.Proofs = nil
	if service, err := NewService(config); service != nil || !errors.Is(err, ErrRestore) {
		t.Fatalf("nil verifier service=%#v error=%v", service, err)
	}
}

func validAuthenticatedRestoreProof(mode RestoreAuthenticationMode, workspaceID, actorID, sessionID,
	replayKey, authorizationID string, issuedAt time.Time,
) AuthenticatedRestoreProof {
	proof := AuthenticatedRestoreProof{Mode: mode, WorkspaceID: workspaceID, Purpose: RestoreAuthenticationPurpose,
		IssuedAt: issuedAt, ReplayKey: replayKey, Actor: &tammyv1.AuthenticationContext{ActorUserId: actorID, SessionId: sessionID},
		SessionID: sessionID, ActorAuthenticated: true, AdministratorAuthorized: true,
		AuthorizationID: authorizationID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x51}, sha256.Size)}
	if mode == RestoreAuthenticationModeNormal {
		proof.AdministratorPasswordVerified = true
		proof.FreshTOTPVerified = true
		proof.TOTPAssertionPurpose = RestoreAuthenticationPurpose
	} else {
		proof.RecoveryProofVerified = true
		proof.AdministratorBreakGlassAudited = true
	}
	return proof
}
