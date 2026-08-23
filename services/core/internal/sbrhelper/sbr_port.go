package sbrhelper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/sbr"
	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
)

type launcherPort interface {
	LaunchStaged(context.Context, *sbrprofile.StagedResources, Request) (Response, error)
}

// SBRPort is the narrow authenticated boundary between the core service and
// the one-shot helper launcher. It owns the private request and never returns
// transient input fields to the caller.
type SBRPort struct {
	launcher    launcherPort
	profilePath string
	now         func() time.Time
}

type AuthenticatedProfilePort struct {
	profilePath string
	locator     sbrprofile.ResourceLocator
	launcher    launcherPort
	now         func() time.Time
}

func NewAuthenticatedProfilePort(profilePath string, locator sbrprofile.ResourceLocator) (*AuthenticatedProfilePort, error) {
	if locator == nil || !filepath.IsAbs(profilePath) || filepath.Clean(profilePath) != profilePath {
		return nil, errors.New("sbr profile unavailable")
	}
	return &AuthenticatedProfilePort{profilePath: profilePath, locator: locator, launcher: NewLauncher(locator), now: time.Now}, nil
}

func (port *AuthenticatedProfilePort) Current(ctx context.Context, now time.Time) (sbr.RuntimeProfile, error) {
	if port == nil || port.locator == nil || ctx == nil || now.IsZero() {
		return sbr.RuntimeProfile{}, errors.New("sbr profile unavailable")
	}
	staged, err := sbrprofile.AuthenticateAndStage(ctx, port.profilePath, port.locator, now.UTC())
	if err != nil {
		return sbr.RuntimeProfile{}, err
	}
	owned := true
	defer func() {
		if owned {
			_ = staged.Close()
		}
	}()
	profileBytes, err := hex.DecodeString(staged.Profile.SHA256)
	if err != nil || len(profileBytes) != sha256.Size {
		return sbr.RuntimeProfile{}, errors.New("sbr profile fingerprint invalid")
	}
	expiresAt, err := time.Parse(time.RFC3339, staged.Profile.Profile.ExpiresAt)
	if err != nil || !expiresAt.After(now.UTC()) {
		return sbr.RuntimeProfile{}, errors.New("sbr profile expired")
	}
	var profileFingerprint [sha256.Size]byte
	copy(profileFingerprint[:], profileBytes)
	var environment tammyv1.SbrEnvironment
	var componentVersion string
	var registrationFingerprint, componentFingerprint [sha256.Size]byte
	switch staged.Profile.Profile.Environment {
	case "SIMULATOR":
		environment = tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR
		componentVersion = "tammy-sbr-simulator-v1"
		registrationInput := append([]byte("tammy.sbr.simulator.registration.v1\x00"), staged.Profile.Canonical...)
		componentInput := append([]byte("tammy.sbr.simulator.component.v1\x00"), staged.Profile.Canonical...)
		registrationFingerprint, componentFingerprint = sha256.Sum256(registrationInput), sha256.Sum256(componentInput)
		clear(registrationInput)
		clear(componentInput)
	case "EVTE":
		environment = tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE
		registrationBytes, registrationErr := hex.DecodeString(staged.Profile.Profile.RegistrationManifestSHA256)
		componentBytes, componentErr := hex.DecodeString(staged.Profile.Profile.ComponentManifestSHA256)
		var ok bool
		componentVersion, ok = staged.AuthenticatedComponentVersion()
		if registrationErr != nil || componentErr != nil || len(registrationBytes) != sha256.Size ||
			len(componentBytes) != sha256.Size || !ok {
			return sbr.RuntimeProfile{}, errors.New("sbr EVTE component unavailable")
		}
		copy(registrationFingerprint[:], registrationBytes)
		copy(componentFingerprint[:], componentBytes)
	default:
		return sbr.RuntimeProfile{}, errors.New("sbr profile unavailable")
	}
	profile := sbr.RuntimeProfile{Environment: environment,
		ComponentVersion: componentVersion, ProfileFingerprint: profileFingerprint,
		RegistrationFingerprint: registrationFingerprint, ComponentFingerprint: componentFingerprint,
		AuthenticatedUntil: expiresAt.UTC(), EndpointProfile: append([]byte(nil), staged.EndpointProfile...)}
	if environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE {
		conformance, ok := staged.AuthenticatedConformance()
		if !ok {
			return sbr.RuntimeProfile{}, errors.New("sbr EVTE conformance unavailable")
		}
		profile.Conformance = sbr.Conformance(conformance)
		scope, ok := staged.AuthenticatedProductIDScope()
		if !ok {
			return sbr.RuntimeProfile{}, errors.New("sbr EVTE Product scope unavailable")
		}
		profile = sbr.BindAuthenticatedProductScope(profile, scope.ProductIdentifier, scope.ServiceID)
	}
	owned = false
	return sbr.BindRuntimeProfileLease(profile, &authenticatedProfileLease{port: &SBRPort{launcher: port.launcher, now: port.now}, staged: staged}), nil
}

type authenticatedProfileLease struct {
	mu     sync.Mutex
	port   *SBRPort
	staged *sbrprofile.StagedResources
	closed bool
}

func (lease *authenticatedProfileLease) Execute(ctx context.Context, request sbr.HelperRequest) (sbr.HelperResult, error) {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.port == nil || lease.staged == nil {
		return sbr.HelperResult{}, errors.New("sbr profile lease closed")
	}
	return lease.port.executeStaged(ctx, lease.staged, request)
}

func (lease *authenticatedProfileLease) Close() error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed {
		return nil
	}
	lease.closed = true
	if lease.staged == nil {
		return nil
	}
	err := lease.staged.Close()
	lease.staged = nil
	return err
}

func NewSBRPort(launcher launcherPort, profilePath string, now func() time.Time) (*SBRPort, error) {
	if launcher == nil || now == nil || !filepath.IsAbs(profilePath) || filepath.Clean(profilePath) != profilePath {
		return nil, errors.New("sbr helper port unavailable")
	}
	return &SBRPort{launcher: launcher, profilePath: profilePath, now: now}, nil
}

func (port *SBRPort) Execute(ctx context.Context, source sbr.HelperRequest) (sbr.HelperResult, error) {
	return sbr.HelperResult{}, errors.New("sbr authenticated profile lease required")
}

func (port *SBRPort) executeStaged(ctx context.Context, staged *sbrprofile.StagedResources, source sbr.HelperRequest) (sbr.HelperResult, error) {
	if port == nil || port.launcher == nil || ctx == nil {
		return sbr.HelperResult{}, errors.New("sbr helper unavailable")
	}
	var simulatorCase SimulatorCase
	if source.Operation == sbr.HelperOperationFixture {
		var validSimulatorCase bool
		simulatorCase, validSimulatorCase = simulatorProtocolCase(source.FixtureFailureCase)
		if !validSimulatorCase {
			return sbr.HelperResult{}, errors.New("sbr helper fixture case invalid")
		}
	} else if source.FixtureFailureCase != tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_UNSPECIFIED {
		return sbr.HelperResult{}, errors.New("sbr helper fixture case invalid")
	}
	request := Request{ProtocolVersion: ProtocolVersion, RequestID: source.RequestID,
		Operation: Operation(source.Operation), DeadlineMillis: port.now().Add(30 * time.Second).UnixMilli(),
		Environment: Environment(source.Environment), WorkspaceID: source.WorkspaceID,
		OrganisationID: source.OrganisationID, CanonicalABN: source.CanonicalABN,
		OpaqueScope: append([]byte(nil), source.OpaqueScope...), OperationID: source.OperationID,
		MutationKind: protocolMutationKind(source.MutationKind), SelectedLocalPath: source.SelectedLocalPath,
		Bookmark: append([]byte(nil), source.Bookmark...), TransientPassword: append([]byte(nil), source.Password...),
		TransientProductID: append([]byte(nil), source.ProductID...), ProductScope: source.ProductIdentifier,
		ServiceID: source.ServiceIdentifier, EndpointProfile: append([]byte(nil), source.EndpointProfile...),
		SimulatorCase: simulatorCase, ProfileFingerprint: bytes.Clone(source.ProfileFingerprint[:]),
		RegistrationFingerprint: bytes.Clone(source.RegistrationFingerprint[:]), ComponentFingerprint: bytes.Clone(source.ComponentFingerprint[:]),
		ComponentVersion: source.ComponentVersion}
	defer request.ClearSecrets()
	response, err := port.launcher.LaunchStaged(ctx, staged, request)
	if err != nil {
		if errors.Is(err, errMalformedHelperResponse) {
			return sbr.HelperResult{}, sbr.ErrHelperMalformedResponse
		}
		var stable interface{ Code() string }
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &stable) && stable.Code() == string(StableErrorDeadlineExpired) {
			return sbr.HelperResult{}, sbr.ErrHelperDeadlineExpired
		}
		return sbr.HelperResult{}, err
	}
	if response.RequestID != source.RequestID {
		return sbr.HelperResult{}, errors.New("sbr helper response mismatch")
	}
	if response.Outcome == OutcomeError {
		if response.StableErrorCode == StableErrorDeadlineExpired {
			return sbr.HelperResult{}, sbr.ErrHelperDeadlineExpired
		}
		return sbr.HelperResult{}, errors.New(string(response.StableErrorCode))
	}
	if len(response.ProfileFingerprint) != sha256.Size || len(response.RegistrationFingerprint) != sha256.Size ||
		len(response.ComponentFingerprint) != sha256.Size || response.ComponentVersion == "" ||
		!bytes.Equal(response.ProfileFingerprint, source.ProfileFingerprint[:]) ||
		!bytes.Equal(response.RegistrationFingerprint, source.RegistrationFingerprint[:]) ||
		!bytes.Equal(response.ComponentFingerprint, source.ComponentFingerprint[:]) ||
		response.ComponentVersion != source.ComponentVersion {
		return sbr.HelperResult{}, errors.New("sbr helper response profile mismatch")
	}
	result := sbr.HelperResult{RequestID: response.RequestID, Outcome: sbr.HelperOutcome(response.Outcome),
		ResultCode: sbr.HelperResultCode(response.RedactedResult), PendingID: response.PendingItemID,
		FixtureFailureCase: source.FixtureFailureCase}
	copy(result.ProfileFingerprint[:], response.ProfileFingerprint)
	copy(result.RegistrationFingerprint[:], response.RegistrationFingerprint)
	copy(result.ComponentFingerprint[:], response.ComponentFingerprint)
	result.ComponentVersion = response.ComponentVersion
	if len(response.CredentialFingerprint) == sha256.Size {
		copy(result.Credential.Fingerprint[:], response.CredentialFingerprint)
		result.Credential.CanonicalABN = response.CanonicalABN
		if response.CredentialCreatedMillis > 0 {
			result.Credential.CreatedAt = time.UnixMilli(response.CredentialCreatedMillis).UTC()
		}
		result.Credential.ExpiresAt = time.UnixMilli(response.CredentialExpiresMillis).UTC()
		result.Credential.ComponentVersion = response.ComponentVersion
		result.Credential.State = tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT
	}
	result.ProductState = sbr.ProductState(response.ProductState)
	if len(response.ProductFingerprint) == sha256.Size {
		copy(result.ProductFingerprint[:], response.ProductFingerprint)
	}
	if source.Operation == sbr.HelperOperationFixture {
		if response.SimulatorCase != request.SimulatorCase {
			return sbr.HelperResult{}, errors.New("sbr helper fixture case mismatch")
		}
		switch response.SimulatorState {
		case SimulatorStateAccepted:
			result.FixtureState = sbr.TransportAccepted
		case SimulatorStateMaybeSent:
			result.FixtureState = sbr.TransportMaybeSent
		case SimulatorStateNotStarted:
			result.FixtureState = sbr.TransportNotStarted
		case SimulatorStateFailed:
			result.FixtureState = sbr.TransportFailed
		case SimulatorStateUnknown:
			result.FixtureState = sbr.TransportUnknown
		default:
			return sbr.HelperResult{}, errors.New("sbr helper fixture response invalid")
		}
	}
	return result, nil
}

func protocolMutationKind(kind sbr.MutationKind) MutationKind {
	switch kind {
	case sbr.MutationImportCredential:
		return MutationImportCredential
	case sbr.MutationReplaceCredential:
		return MutationReplaceCredential
	case sbr.MutationRemoveCredential:
		return MutationRemoveCredential
	case sbr.MutationImportProductID:
		return MutationImportProductID
	case sbr.MutationRemoveProductID:
		return MutationRemoveProductID
	default:
		return 0
	}
}

func simulatorProtocolCase(value tammyv1.SbrReadinessFixtureFailure) (SimulatorCase, bool) {
	switch value {
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_UNSPECIFIED:
		return SimulatorAccepted, true
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_NOT_STARTED:
		return SimulatorNotStarted, true
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MAYBE_SENT:
		return SimulatorMaybeSent, true
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE:
		return SimulatorMalformedResponse, true
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH:
		return SimulatorHelperDeath, true
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT:
		return SimulatorTimeout, true
	default:
		return 0, false
	}
}
