package audit

import (
	"context"
	"errors"
	"regexp"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var (
	ErrTrustProofProvider = errors.New("audit: trust proof provider failed")
	ErrEvidenceProvider   = errors.New("audit: evidence provider failed")
)

// AuthenticatedTrustProof is the typed, provider-authenticated proof envelope.
// The adapter requires exactly the fields belonging to Kind and rejects mixed
// evidence rather than silently ignoring it.
type AuthenticatedTrustProof struct {
	Kind                           TrustProofKind
	Actor                          *tammyv1.AuthenticationContext
	PassphraseVerified             bool
	AdministratorPasswordVerified  bool
	FreshTOTPVerified              bool
	RecoveryProofVerified          bool
	AdministratorBreakGlassAudited bool
}

type AuthenticatedTrustProofPort interface {
	VerifyTrustProof(context.Context, Executor, TrustProofKind) (AuthenticatedTrustProof, error)
}

type authenticatedTrustProofVerifier struct {
	port AuthenticatedTrustProofPort
}

func NewAuthenticatedTrustProofVerifier(port AuthenticatedTrustProofPort) (TrustProofVerifier, error) {
	if nilInterface(port) {
		return nil, ErrTrustProof
	}
	return &authenticatedTrustProofVerifier{port: port}, nil
}

func (verifier *authenticatedTrustProofVerifier) Verify(
	ctx context.Context,
	executor Executor,
	kind TrustProofKind,
) (TrustApproval, error) {
	if verifier == nil || nilInterface(verifier.port) || ctx == nil || nilInterface(executor) ||
		(kind != TrustProofNormal && kind != TrustProofRecoveryBreakGlass) {
		return TrustApproval{}, ErrTrustProof
	}
	if err := ctx.Err(); err != nil {
		return TrustApproval{}, errors.Join(ErrTrustProof, err)
	}
	proof, err := verifier.port.VerifyTrustProof(ctx, executor, kind)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return TrustApproval{}, errors.Join(ErrTrustProof, contextErr)
		}
		return TrustApproval{}, errors.Join(ErrTrustProof, ErrTrustProofProvider)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return TrustApproval{}, errors.Join(ErrTrustProof, contextErr)
	}
	if !validAuthenticatedTrustProof(kind, proof) {
		return TrustApproval{}, ErrTrustProof
	}
	return TrustApproval{
		Actor:                          proto.Clone(proof.Actor).(*tammyv1.AuthenticationContext),
		PassphraseVerified:             proof.PassphraseVerified,
		AdministratorPasswordVerified:  proof.AdministratorPasswordVerified,
		FreshTOTPVerified:              proof.FreshTOTPVerified,
		RecoveryProofVerified:          proof.RecoveryProofVerified,
		AdministratorBreakGlassAudited: proof.AdministratorBreakGlassAudited,
	}, nil
}

func validAuthenticatedTrustProof(kind TrustProofKind, proof AuthenticatedTrustProof) bool {
	if proof.Kind != kind || proof.Actor == nil || len(proof.Actor.ProtoReflect().GetUnknown()) != 0 ||
		!ids.IsCanonicalV7(proof.Actor.ActorUserId) || !ids.IsCanonicalV7(proof.Actor.SessionId) {
		return false
	}
	switch kind {
	case TrustProofNormal:
		return proof.PassphraseVerified && proof.AdministratorPasswordVerified && proof.FreshTOTPVerified &&
			!proof.RecoveryProofVerified && !proof.AdministratorBreakGlassAudited
	case TrustProofRecoveryBreakGlass:
		return proof.RecoveryProofVerified && proof.AdministratorBreakGlassAudited &&
			!proof.PassphraseVerified && !proof.AdministratorPasswordVerified && !proof.FreshTOTPVerified
	default:
		return false
	}
}

var evidenceProviderNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// EvidenceCollectionPort is the narrow production source for optional
// evidence members. The adapter owns validation and copying.
type EvidenceCollectionPort interface {
	CollectEvidence(context.Context, ExportJob) ([]EvidenceObject, error)
}

type EvidenceProviderRegistration struct {
	Name     string
	Provider EvidenceCollectionPort
}

// EvidenceProviderRegistry is immutable after construction. Its snapshots
// never expose the registry's internal map to worker callers.
type EvidenceProviderRegistry struct {
	providers map[string]EvidenceProvider
}

func NewEvidenceProviderRegistry(registrations []EvidenceProviderRegistration) (*EvidenceProviderRegistry, error) {
	if len(registrations) == 0 || len(registrations) > maxEvidenceArchiveMembers {
		return nil, ErrEvidenceProvider
	}
	providers := make(map[string]EvidenceProvider, len(registrations))
	for _, registration := range registrations {
		if !evidenceProviderNamePattern.MatchString(registration.Name) || nilInterface(registration.Provider) {
			return nil, ErrEvidenceProvider
		}
		if _, duplicate := providers[registration.Name]; duplicate {
			return nil, ErrEvidenceProvider
		}
		providers[registration.Name] = &validatedEvidenceProvider{port: registration.Provider}
	}
	return &EvidenceProviderRegistry{providers: providers}, nil
}

func (registry *EvidenceProviderRegistry) workerProviders() map[string]EvidenceProvider {
	if registry == nil {
		return nil
	}
	providers := make(map[string]EvidenceProvider, len(registry.providers))
	for name, provider := range registry.providers {
		providers[name] = provider
	}
	return providers
}

type validatedEvidenceProvider struct {
	port EvidenceCollectionPort
}

func (provider *validatedEvidenceProvider) Collect(ctx context.Context, job ExportJob) ([]EvidenceObject, error) {
	if provider == nil || nilInterface(provider.port) || ctx == nil {
		return nil, ErrEvidenceProvider
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrEvidenceProvider, err)
	}
	objects, err := provider.port.CollectEvidence(ctx, job)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, errors.Join(ErrEvidenceProvider, contextErr)
	}
	if err != nil {
		return nil, ErrEvidenceProvider
	}
	if err := preflightEvidenceObjects(objects, workerEvidenceReservedMembers, workerEvidenceReservedBytes); err != nil {
		return nil, errors.Join(ErrEvidenceProvider, err)
	}
	owned := make([]EvidenceObject, len(objects))
	for index := range objects {
		owned[index] = EvidenceObject{Path: objects[index].Path, Bytes: append([]byte(nil), objects[index].Bytes...)}
	}
	return owned, nil
}
