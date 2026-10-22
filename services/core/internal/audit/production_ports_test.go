package audit

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

type typedPortExecutor struct{}

func (typedPortExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (typedPortExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}

type trustProofPortFunc func(context.Context, Executor, TrustProofKind) (AuthenticatedTrustProof, error)

func (function trustProofPortFunc) VerifyTrustProof(
	ctx context.Context,
	executor Executor,
	kind TrustProofKind,
) (AuthenticatedTrustProof, error) {
	return function(ctx, executor, kind)
}

func TestAuthenticatedTrustProofVerifierEnforcesExactProofSemantics(t *testing.T) {
	actor := &tammyv1.AuthenticationContext{
		ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
		SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
	}
	testCases := []struct {
		name  string
		kind  TrustProofKind
		proof AuthenticatedTrustProof
		ok    bool
	}{
		{name: "normal", kind: TrustProofNormal, ok: true, proof: AuthenticatedTrustProof{Kind: TrustProofNormal, Actor: actor,
			PassphraseVerified: true, AdministratorPasswordVerified: true, FreshTOTPVerified: true}},
		{name: "recovery", kind: TrustProofRecoveryBreakGlass, ok: true, proof: AuthenticatedTrustProof{Kind: TrustProofRecoveryBreakGlass, Actor: actor,
			RecoveryProofVerified: true, AdministratorBreakGlassAudited: true}},
		{name: "mixed", kind: TrustProofNormal, proof: AuthenticatedTrustProof{Kind: TrustProofNormal, Actor: actor,
			PassphraseVerified: true, AdministratorPasswordVerified: true, FreshTOTPVerified: true, RecoveryProofVerified: true}},
		{name: "partial", kind: TrustProofNormal, proof: AuthenticatedTrustProof{Kind: TrustProofNormal, Actor: actor,
			PassphraseVerified: true, AdministratorPasswordVerified: true}},
		{name: "wrong kind", kind: TrustProofNormal, proof: AuthenticatedTrustProof{Kind: TrustProofRecoveryBreakGlass, Actor: actor,
			PassphraseVerified: true, AdministratorPasswordVerified: true, FreshTOTPVerified: true}},
		{name: "unknown kind", kind: TrustProofKind(99), proof: AuthenticatedTrustProof{Kind: TrustProofKind(99), Actor: actor}},
		{name: "unauthenticated actor", kind: TrustProofNormal, proof: AuthenticatedTrustProof{Kind: TrustProofNormal,
			Actor: &tammyv1.AuthenticationContext{ActorUserId: actor.ActorUserId}, PassphraseVerified: true,
			AdministratorPasswordVerified: true, FreshTOTPVerified: true}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter, err := NewAuthenticatedTrustProofVerifier(trustProofPortFunc(func(
				context.Context, Executor, TrustProofKind,
			) (AuthenticatedTrustProof, error) {
				return testCase.proof, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			approval, err := adapter.Verify(context.Background(), typedPortExecutor{}, testCase.kind)
			if testCase.ok {
				if err != nil || !validTrustApproval(testCase.kind, approval) || approval.Actor == actor {
					t.Fatalf("approval=%#v error=%v", approval, err)
				}
				return
			}
			if !errors.Is(err, ErrTrustProof) {
				t.Fatalf("error=%v, want ErrTrustProof", err)
			}
		})
	}
}

func TestAuthenticatedTrustProofVerifierClassifiesProviderAndCancellationErrors(t *testing.T) {
	providerFailure := errors.New("provider detail must not escape")
	adapter, err := NewAuthenticatedTrustProofVerifier(trustProofPortFunc(func(
		context.Context, Executor, TrustProofKind,
	) (AuthenticatedTrustProof, error) {
		return AuthenticatedTrustProof{}, providerFailure
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Verify(context.Background(), typedPortExecutor{}, TrustProofNormal); !errors.Is(err, ErrTrustProof) ||
		!errors.Is(err, ErrTrustProofProvider) || errors.Is(err, providerFailure) {
		t.Fatalf("provider error=%v, want stable audit-owned classification only", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Verify(ctx, typedPortExecutor{}, TrustProofNormal); !errors.Is(err, ErrTrustProof) ||
		!errors.Is(err, context.Canceled) || errors.Is(err, ErrTrustProofProvider) {
		t.Fatalf("cancellation error=%v, want trust proof plus context cancellation", err)
	}
}

type evidencePortFunc func(context.Context, ExportJob) ([]EvidenceObject, error)

func (function evidencePortFunc) CollectEvidence(ctx context.Context, job ExportJob) ([]EvidenceObject, error) {
	return function(ctx, job)
}

func TestEvidenceProviderRegistryIsImmutableAndPreflightsBeforeCopy(t *testing.T) {
	providerObjects := []EvidenceObject{{Path: "provider/receipt.json", Bytes: []byte("retained")}}
	registrations := []EvidenceProviderRegistration{{Name: "audit_chain", Provider: evidencePortFunc(func(
		context.Context, ExportJob,
	) ([]EvidenceObject, error) {
		return providerObjects, nil
	})}}
	registry, err := NewEvidenceProviderRegistry(registrations)
	if err != nil {
		t.Fatal(err)
	}
	registrations[0].Name = "mutated"
	providers := registry.workerProviders()
	provider := providers["audit_chain"]
	if provider == nil || providers["mutated"] != nil {
		t.Fatalf("provider snapshot=%v, want immutable audit_chain registration", providers)
	}
	objects, err := provider.Collect(context.Background(), ExportJob{})
	if err != nil {
		t.Fatal(err)
	}
	providerObjects[0].Bytes[0] = 'X'
	if string(objects[0].Bytes) != "retained" {
		t.Fatalf("collected bytes=%q, want owned copy", objects[0].Bytes)
	}

	providerObjects = []EvidenceObject{{Path: "manifest.json", Bytes: []byte("reserved")}}
	if _, err := provider.Collect(context.Background(), ExportJob{}); !errors.Is(err, ErrEvidenceArchive) ||
		!errors.Is(err, ErrEvidenceProvider) {
		t.Fatalf("invalid object error=%v, want provider and evidence archive classification", err)
	}
}

func TestEvidenceProviderAdapterClassifiesCancellationAndProviderErrors(t *testing.T) {
	providerFailure := errors.New("provider detail must not escape")
	registry, err := NewEvidenceProviderRegistry([]EvidenceProviderRegistration{{Name: "audit_chain", Provider: evidencePortFunc(func(
		context.Context, ExportJob,
	) ([]EvidenceObject, error) {
		return nil, providerFailure
	})}})
	if err != nil {
		t.Fatal(err)
	}
	provider := registry.workerProviders()["audit_chain"]
	if _, err := provider.Collect(context.Background(), ExportJob{}); !errors.Is(err, ErrEvidenceProvider) || errors.Is(err, providerFailure) {
		t.Fatalf("provider error=%v, want stable audit-owned classification only", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Collect(ctx, ExportJob{}); !errors.Is(err, ErrEvidenceProvider) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v, want evidence provider plus context cancellation", err)
	}
}

type registryWorkerTransactions struct{}

func (registryWorkerTransactions) Read(_ context.Context, callback func(ServiceTransaction) error) error {
	return callback(nil)
}

func (registryWorkerTransactions) Mutate(_ context.Context, callback func(ServiceTransaction) error) error {
	return callback(nil)
}

type registryWorkerDestinations struct{}

func (registryWorkerDestinations) Resolve(string) (ExportDestination, error) {
	return nil, ErrExportJob
}

type registryWorkerDEKProvider struct{}

func (registryWorkerDEKProvider) Acquire(context.Context, string) (EvidenceExportDEKLease, error) {
	return registryWorkerDEKLease{}, nil
}

type registryWorkerDEKLease struct{}

func (registryWorkerDEKLease) WithDEK(callback func([]byte) error) error {
	return callback(make([]byte, 32))
}
func (registryWorkerDEKLease) Close() {}

func TestEvidenceExportWorkerConstructsFromImmutableProviderRegistry(t *testing.T) {
	registry, err := NewEvidenceProviderRegistry([]EvidenceProviderRegistration{{Name: "audit_chain", Provider: evidencePortFunc(func(
		context.Context, ExportJob,
	) ([]EvidenceObject, error) {
		return nil, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions: registryWorkerTransactions{}, Destinations: registryWorkerDestinations{},
		ProviderRegistry: registry, DEKProvider: registryWorkerDEKProvider{},
		Clock: clock.NewFixed(time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)), Gate: NewWriteGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.providers["audit_chain"] == nil || len(worker.providers) != 1 {
		t.Fatalf("worker providers=%v, want immutable registry snapshot", worker.providers)
	}
}
