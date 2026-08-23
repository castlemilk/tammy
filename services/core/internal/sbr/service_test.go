package sbr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	serviceWorkspaceID    = "018f5f48-7f01-7b6e-86df-8b89f45e1001"
	serviceOrganisationID = "018f5f48-7f01-7b6e-86df-8b89f45e1002"
	serviceUserID         = "018f5f48-7f01-7b6e-86df-8b89f45e1003"
	serviceSessionID      = "018f5f48-7f01-7b6e-86df-8b89f45e1004"
	serviceCommandID      = "018f5f48-7f01-7b6e-86df-8b89f45e1005"
	serviceOperationID    = "018f5f48-7f01-7b6e-86df-8b89f45e1006"
	servicePendingID      = "018f5f48-7f01-7b6e-86df-8b89f45e1007"
	serviceABN            = "11000000560"
)

type fakeIdentity struct {
	mu           sync.Mutex
	actions      []authorisation.Action
	purposes     []string
	authorizeErr error
	factorErr    error
	consumeOnce  bool
	consumed     bool
}

func (identity *fakeIdentity) AuthorizeWithin(_ context.Context, _ MutationExecutor, _ *tammyv1.AuthenticationContext, action authorisation.Action) error {
	identity.mu.Lock()
	defer identity.mu.Unlock()
	identity.actions = append(identity.actions, action)
	return identity.authorizeErr
}

func (identity *fakeIdentity) ConsumeFreshFactorWithin(_ context.Context, _ MutationExecutor, _ *tammyv1.AuthenticationContext, _ *tammyv1.FreshFactorContext, purpose string) error {
	identity.mu.Lock()
	defer identity.mu.Unlock()
	identity.purposes = append(identity.purposes, purpose)
	if identity.consumeOnce && identity.consumed {
		return errors.New("fresh factor already consumed")
	}
	if identity.factorErr == nil {
		identity.consumed = true
	}
	return identity.factorErr
}

func (identity *fakeIdentity) ValidateAuthorizationWithin(_ context.Context, _ MutationExecutor, _ *tammyv1.AuthenticationContext, action authorisation.Action) error {
	identity.mu.Lock()
	defer identity.mu.Unlock()
	identity.actions = append(identity.actions, action)
	return identity.authorizeErr
}

func (identity *fakeIdentity) ValidateFreshFactorWithin(_ context.Context, _ MutationExecutor, _ *tammyv1.AuthenticationContext, _ *tammyv1.FreshFactorContext, _ string) error {
	identity.mu.Lock()
	defer identity.mu.Unlock()
	return identity.factorErr
}

type fakeOrganisation struct {
	binding OrganisationBinding
	err     error
}

func (port fakeOrganisation) Current(context.Context, QueryExecutor, time.Time) (OrganisationBinding, error) {
	return port.binding, port.err
}

type fakeProfile struct {
	profile RuntimeProfile
	err     error
}

func (port fakeProfile) Current(context.Context, time.Time) (RuntimeProfile, error) {
	return port.profile, port.err
}

type observingProfileLease struct {
	helper *fakeHelper
	closed int
}

func (lease *observingProfileLease) Execute(ctx context.Context, request HelperRequest) (HelperResult, error) {
	return lease.helper.Execute(ctx, request)
}

func (lease *observingProfileLease) Close() error {
	lease.closed++
	return nil
}

type fakeHelper struct {
	mu       sync.Mutex
	requests []HelperRequest
	result   HelperResult
	err      error
	execute  func(HelperRequest) (HelperResult, error)
}

type fakeAudit struct {
	records []AuditRecord
	failOn  AuditAction
}

func (audit *fakeAudit) Record(_ context.Context, _ MutationExecutor, record AuditRecord) error {
	audit.records = append(audit.records, record)
	if record.Action == audit.failOn {
		return errors.New("audit unavailable")
	}
	return nil
}

func (helper *fakeHelper) Execute(_ context.Context, request HelperRequest) (HelperResult, error) {
	helper.mu.Lock()
	helper.requests = append(helper.requests, request.Clone())
	helper.mu.Unlock()
	if helper.execute != nil {
		return helper.execute(request.Clone())
	}
	return helper.result.Clone(), helper.err
}

type fakeUnitOfWork struct{}

func (fakeUnitOfWork) Inspect(ctx context.Context, work func(context.Context, QueryExecutor) error) error {
	return work(ctx, fakeExecutor{})
}

func (fakeUnitOfWork) Mutate(ctx context.Context, work func(context.Context, MutationExecutor) error) error {
	return work(ctx, fakeExecutor{})
}

type fakeExecutor struct{}

func (fakeExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}
func (fakeExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unused")
}

type captureAuditExecutor struct{ args []any }

func (executor *captureAuditExecutor) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	executor.args = append([]any(nil), args...)
	if payload, ok := executor.args[5].([]byte); ok {
		executor.args[5] = bytes.Clone(payload)
	}
	return fakeResult{}, nil
}
func (*captureAuditExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unused")
}

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

func testService(t *testing.T, helper *fakeHelper, identity *fakeIdentity) *Service {
	t.Helper()
	now := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	profileHash := sha256.Sum256([]byte("profile"))
	registrationHash := sha256.Sum256([]byte("registration"))
	componentHash := sha256.Sum256([]byte("component"))
	service, err := NewService(ServiceConfig{
		WorkspaceID: serviceWorkspaceID,
		Identity:    identity,
		Organisation: fakeOrganisation{binding: OrganisationBinding{
			OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
			VerificationExpiresAt: now.Add(time.Hour),
		}},
		Profiles: fakeProfile{profile: RuntimeProfile{
			Environment:        tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
			Conformance:        ConformanceSimulator,
			ComponentVersion:   "sim-v1",
			ProfileFingerprint: profileHash, RegistrationFingerprint: registrationHash,
			ComponentFingerprint: componentHash, AuthenticatedUntil: now.Add(time.Hour),
		}},
		Helper: helper, Units: fakeUnitOfWork{}, Now: func() time.Time { return now },
		Audit:           discardAudit{},
		NewID:           func() (string, error) { return serviceOperationID, nil },
		InstallationKey: bytes.Repeat([]byte{0x42}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func command(purpose string) *tammyv1.CommandContext {
	return &tammyv1.CommandContext{
		IdempotencyKey: serviceCommandID,
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: serviceUserID, SessionId: serviceSessionID},
		FreshFactor: &tammyv1.FreshFactorContext{AssertionId: servicePendingID, Purpose: purpose,
			AssertedAt: timestamppb.New(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC))},
	}
}

func evteProfile(profile RuntimeProfile, productIdentifier, serviceID string) RuntimeProfile {
	profile.Environment = tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE
	profile.Conformance = ConformancePre
	profile.ExpectedProductIdentifier = productIdentifier
	profile.ExpectedServiceID = serviceID
	profile.ProductScopeFingerprint = authenticatedProductScopeFingerprint(productIdentifier, serviceID)
	return profile
}

func TestValidCurrentRejectsMissingAndUnknownAuthenticatedConformance(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	binding, profile, err := service.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		environment tammyv1.SbrEnvironment
		conformance Conformance
	}{
		{name: "simulator missing", environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR},
		{name: "simulator unknown", environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR, conformance: "UNKNOWN"},
		{name: "EVTE missing", environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE},
		{name: "EVTE unknown", environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE, conformance: "UNKNOWN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := profile
			candidate.Environment = test.environment
			candidate.Conformance = test.conformance
			if test.environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE {
				candidate.ExpectedProductIdentifier = "product"
				candidate.ExpectedServiceID = "service"
				candidate.ProductScopeFingerprint = authenticatedProductScopeFingerprint("product", "service")
			}
			if validCurrent(binding, candidate, service.now()) {
				t.Fatal("invalid authenticated conformance was accepted")
			}
		})
	}
}

func validPreparedHelperResult(metadata CredentialMetadata) HelperResult {
	return HelperResult{RequestID: serviceOperationID, Outcome: HelperOutcomePending, ResultCode: HelperResultNone,
		PendingID: servicePendingID, Credential: metadata,
		ProfileFingerprint: sha256.Sum256([]byte("profile")), RegistrationFingerprint: sha256.Sum256([]byte("registration")),
		ComponentFingerprint: sha256.Sum256([]byte("component")), ComponentVersion: "sim-v1"}
}

func TestStatusAndUnlockHelperResponsesRequireExactClosedAuthenticatedEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN, VerificationExpiresAt: now.Add(time.Hour)}
	profile := RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR, ComponentVersion: "sim-v1",
		ProfileFingerprint: sha256.Sum256([]byte("profile")), RegistrationFingerprint: sha256.Sum256([]byte("registration")),
		ComponentFingerprint: sha256.Sum256([]byte("component")), AuthenticatedUntil: now.Add(time.Hour)}
	credential := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), ComponentVersion: profile.ComponentVersion,
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	request := HelperRequest{RequestID: serviceOperationID, ProfileFingerprint: profile.ProfileFingerprint,
		RegistrationFingerprint: profile.RegistrationFingerprint, ComponentFingerprint: profile.ComponentFingerprint,
		ComponentVersion: profile.ComponentVersion}
	base := HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultReady, Credential: credential,
		ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
		ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}
	if !validStatusHelperResponse(request, base, binding, profile, now) || !validUnlockHelperResponse(request, base, binding, profile, now) {
		t.Fatal("valid closed authenticated helper response rejected")
	}
	mutations := []func(*HelperResult){
		func(r *HelperResult) { r.RequestID = serviceUserID }, func(r *HelperResult) { r.PendingID = servicePendingID },
		func(r *HelperResult) { r.StableCode = "SBR_HELPER_PROTOCOL_ERROR" }, func(r *HelperResult) { r.ProfileFingerprint = [sha256.Size]byte{} },
		func(r *HelperResult) { r.RegistrationFingerprint = sha256.Sum256([]byte("wrong")) },
		func(r *HelperResult) { r.ComponentFingerprint = sha256.Sum256([]byte("wrong")) }, func(r *HelperResult) { r.ComponentVersion = "wrong" },
		func(r *HelperResult) { r.Credential.CanonicalABN = "11000000561" }, func(r *HelperResult) { r.Credential.CreatedAt = time.Time{} },
		func(r *HelperResult) { r.Credential.ExpiresAt = now }, func(r *HelperResult) { r.ProductState = ProductPresent },
		func(r *HelperResult) { r.FixtureState = TransportAccepted },
	}
	for index, mutate := range mutations {
		candidate := base
		mutate(&candidate)
		if validStatusHelperResponse(request, candidate, binding, profile, now) || validUnlockHelperResponse(request, candidate, binding, profile, now) {
			t.Fatalf("contradictory response case %d accepted: %+v", index, candidate)
		}
	}
}

func TestCommittedMutationValidationRequiresExactPreparedEffectAndAuthenticatedProfile(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	profile := service.profiles.(fakeProfile).profile
	credential := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: profile.ComponentVersion, CreatedAt: service.now().Add(-time.Hour), ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	request := service.helperRequest(HelperOperationCommitMutation, service.organisation.(fakeOrganisation).binding, profile)
	request.RequestID, request.OperationID, request.PendingID, request.MutationKind = serviceOperationID, serviceOperationID, servicePendingID, MutationImportCredential
	prepared := HelperResult{Credential: credential}
	base := HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted,
		Credential: credential, ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
		ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}
	if !validCommittedCredentialResponse(request, base, prepared, profile) {
		t.Fatal("exact committed credential receipt rejected")
	}
	for _, mutate := range []func(*HelperResult){
		func(result *HelperResult) { result.Credential.Fingerprint = sha256.Sum256([]byte("other")) },
		func(result *HelperResult) { result.ProfileFingerprint = sha256.Sum256([]byte("other")) },
		func(result *HelperResult) { result.PendingID = servicePendingID },
		func(result *HelperResult) { result.ProductState = ProductPresent },
		func(result *HelperResult) { result.FixtureState = TransportAccepted },
	} {
		candidate := base
		mutate(&candidate)
		if validCommittedCredentialResponse(request, candidate, prepared, profile) {
			t.Fatalf("contradictory committed credential receipt accepted: %+v", candidate)
		}
	}
	invalidRequest := request
	invalidRequest.RequestID = "not-a-uuidv7"
	if validCommittedCredentialResponse(invalidRequest, base, prepared, profile) {
		t.Fatal("committed credential receipt with non-canonical request ID accepted")
	}
	invalidRequest = request
	invalidRequest.Operation = HelperOperationStatus
	if validCommittedCredentialResponse(invalidRequest, base, prepared, profile) {
		t.Fatal("committed credential receipt for non-mutation operation accepted")
	}
	invalidRequest = request
	invalidRequest.ProductIdentifier = "unrelated-product"
	if validCommittedCredentialResponse(invalidRequest, base, prepared, profile) {
		t.Fatal("committed credential receipt with Product scope accepted")
	}

	productProfile := evteProfile(profile, "EVTE.PRODUCT", "EVTE.SERVICE")
	request = service.helperRequest(HelperOperationCommitMutation, service.organisation.(fakeOrganisation).binding, productProfile)
	request.RequestID, request.OperationID, request.PendingID, request.MutationKind = serviceOperationID, serviceOperationID, servicePendingID, MutationImportProductID
	request.ProductIdentifier, request.ServiceIdentifier = productProfile.ExpectedProductIdentifier, productProfile.ExpectedServiceID
	productFingerprint := sha256.Sum256([]byte("product"))
	prepared = HelperResult{ProductState: ProductPresent, ProductFingerprint: productFingerprint}
	base = HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted,
		ProductState: ProductPresent, ProductFingerprint: productFingerprint, ProfileFingerprint: productProfile.ProfileFingerprint,
		RegistrationFingerprint: productProfile.RegistrationFingerprint, ComponentFingerprint: productProfile.ComponentFingerprint,
		ComponentVersion: productProfile.ComponentVersion}
	if !validCommittedProductResponse(request, base, prepared, productProfile) {
		t.Fatal("exact committed Product receipt rejected")
	}
	base.Credential = credential
	if validCommittedProductResponse(request, base, prepared, productProfile) {
		t.Fatal("committed Product receipt with credential metadata accepted")
	}
	base.Credential = CredentialMetadata{}
	request.ServiceIdentifier = "OTHER.SERVICE"
	if validCommittedProductResponse(request, base, prepared, productProfile) {
		t.Fatal("committed Product receipt for another service accepted")
	}
}

func TestImportDerivesOpaqueScopeAndUsesExactAuthorizationPurpose(t *testing.T) {
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		if request.Operation == HelperOperationCommitMutation {
			return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted}, nil
		}
		return validPreparedHelperResult(metadata), nil
	}}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	_, _ = service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
		CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/synthetic.p12", Password: []byte("transient"),
	}))
	if len(identity.actions) != 2 || identity.actions[0] != authorisation.ActionImportSBRMachineCredential || identity.actions[1] != authorisation.ActionImportSBRMachineCredential {
		t.Fatalf("actions = %v", identity.actions)
	}
	if len(identity.purposes) != 1 || identity.purposes[0] != PurposeImportMachineCredential {
		t.Fatalf("purposes = %v", identity.purposes)
	}
	if len(helper.requests) < 1 {
		t.Fatalf("helper requests = %d", len(helper.requests))
	}
	request := helper.requests[0]
	wantScope := DeriveOpaqueScope(bytes.Repeat([]byte{0x42}, 32), serviceWorkspaceID, serviceOrganisationID, serviceABN)
	if request.WorkspaceID != serviceWorkspaceID || request.OrganisationID != serviceOrganisationID || request.CanonicalABN != serviceABN ||
		!bytes.Equal(request.OpaqueScope, wantScope[:]) {
		t.Fatalf("helper binding was not server derived: %+v", request)
	}
	if request.SelectedLocalPath != "/tmp/synthetic.p12" || string(request.Password) != "transient" {
		t.Fatalf("bounded transient fields not forwarded")
	}
}

func TestImportRejectsHelperCredentialABNMismatchBeforeCommit(t *testing.T) {
	prepared := validPreparedHelperResult(CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: "53004085616",
		ComponentVersion: "sim-v1", ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC), State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT})
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		if request.Operation == HelperOperationAbortMutation {
			return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationAborted}, nil
		}
		return prepared, nil
	}}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	_, err := service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
		CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/synthetic.p12",
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("error = %v", err)
	}
	if len(identity.purposes) != 1 || identity.purposes[0] != PurposeImportMachineCredential {
		t.Fatalf("dispatched invalid helper response factor reservation = %v", identity.purposes)
	}
	if len(helper.requests) != 2 || helper.requests[1].Operation != HelperOperationAbortMutation {
		t.Fatalf("helper operations = %+v", helper.requests)
	}
}

func TestImportRejectsEveryMalformedPreparedHelperResponseAndAbortsPendingItem(t *testing.T) {
	profileFingerprint := sha256.Sum256([]byte("profile"))
	registrationFingerprint := sha256.Sum256([]byte("registration"))
	componentFingerprint := sha256.Sum256([]byte("component"))
	valid := HelperResult{RequestID: serviceOperationID, Outcome: HelperOutcomePending,
		ResultCode: HelperResultNone, PendingID: servicePendingID,
		Credential: CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
			ComponentVersion: "sim-v1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC), State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT},
		ProfileFingerprint: profileFingerprint, RegistrationFingerprint: registrationFingerprint,
		ComponentFingerprint: componentFingerprint, ComponentVersion: "sim-v1"}
	for _, test := range []struct {
		name   string
		mutate func(*HelperResult)
	}{
		{name: "wrong request id", mutate: func(result *HelperResult) { result.RequestID = servicePendingID }},
		{name: "closed result contradiction", mutate: func(result *HelperResult) { result.ResultCode = HelperResultMutationCommitted }},
		{name: "missing pending id", mutate: func(result *HelperResult) { result.PendingID = "" }},
		{name: "zero credential fingerprint", mutate: func(result *HelperResult) { result.Credential.Fingerprint = [sha256.Size]byte{} }},
		{name: "wrong canonical ABN", mutate: func(result *HelperResult) { result.Credential.CanonicalABN = "53004085616" }},
		{name: "expired credential", mutate: func(result *HelperResult) { result.Credential.ExpiresAt = time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC) }},
		{name: "created after expiry", mutate: func(result *HelperResult) { result.Credential.CreatedAt = time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{name: "component version mismatch", mutate: func(result *HelperResult) { result.ComponentVersion = "other" }},
		{name: "profile fingerprint mismatch", mutate: func(result *HelperResult) { result.ProfileFingerprint = sha256.Sum256([]byte("other")) }},
		{name: "registration fingerprint mismatch", mutate: func(result *HelperResult) { result.RegistrationFingerprint = sha256.Sum256([]byte("other")) }},
		{name: "component fingerprint mismatch", mutate: func(result *HelperResult) { result.ComponentFingerprint = sha256.Sum256([]byte("other")) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := valid

			test.mutate(&result)
			helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
				if request.Operation == HelperOperationAbortMutation {
					return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK,
						ResultCode: HelperResultMutationAborted}, nil
				}
				return result, nil
			}}
			service := testService(t, helper, &fakeIdentity{})
			_, err := service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
				CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/synthetic.p12",
			}))
			if connect.CodeOf(err) != connect.CodeFailedPrecondition && connect.CodeOf(err) != connect.CodeUnavailable {
				t.Fatalf("error = %v", err)
			}
			if result.PendingID != "" {
				if got := helper.requests[len(helper.requests)-1].Operation; got != HelperOperationAbortMutation {
					t.Fatalf("last helper operation = %d, want abort", got)
				}
			}
		})
	}
}

func TestAllMutationPurposesAreDistinct(t *testing.T) {
	purposes := []string{PurposeImportMachineCredential, PurposeUnlockMachineCredential,
		PurposeReplaceMachineCredential, PurposeRemoveMachineCredential,
		PurposeImportProductID, PurposeRemoveProductID, PurposeUseMachineCredential}
	seen := map[string]bool{}
	for _, purpose := range purposes {
		if purpose == "" || seen[purpose] {
			t.Fatalf("purpose is empty or duplicated: %q", purpose)
		}
		seen[purpose] = true
	}
}

func TestFixtureIsClosedToSimulatorAndFixedIdentifier(t *testing.T) {
	helper := &fakeHelper{result: HelperResult{Outcome: HelperOutcomeOK, FixtureState: TransportAccepted}}
	service := testService(t, helper, &fakeIdentity{})
	_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: command(PurposeUseMachineCredential), FixtureId: "BAS-2026-Q4",
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || len(helper.requests) != 0 {
		t.Fatalf("err=%v helper=%d", err, len(helper.requests))
	}
}

func TestServiceClosesAuthenticatedProfileLeaseOnReadOnlySuccess(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	port := service.profiles.(fakeProfile)
	lease := &observingProfileLease{helper: service.helper.(*fakeHelper)}
	port.profile = BindRuntimeProfileLease(port.profile, lease)
	service.profiles = port
	if _, err := service.GetSbrReadiness(context.Background(), connect.NewRequest(&tammyv1.GetSbrReadinessRequest{
		Authentication: command("").Authentication,
	})); err != nil {
		t.Fatal(err)
	}
	if lease.closed != 1 {
		t.Fatalf("profile lease close count = %d, want 1", lease.closed)
	}
}

func TestAuthorizationOrFreshFactorFailureNeverReachesHelper(t *testing.T) {
	for _, name := range []string{"wrong purpose", "stale", "consumed"} {
		t.Run(name, func(t *testing.T) {
			helper := &fakeHelper{}
			service := testService(t, helper, &fakeIdentity{authorizeErr: errors.New(name), factorErr: errors.New(name)})
			_, err := service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
				CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/synthetic.p12",
			}))
			if connect.CodeOf(err) != connect.CodePermissionDenied || len(helper.requests) != 0 {
				t.Fatalf("error=%v helper requests=%d", err, len(helper.requests))
			}
		})
	}
}

func TestMutationPreconditionsDoNotTouchFreshFactor(t *testing.T) {
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: time.Date(2026, 6, 30, 1, 0, 0, 0, time.UTC)}
	credential := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	for _, test := range []struct {
		name      string
		call      func(*Service) error
		configure func(*Service)
		wantAuth  bool
	}{
		{
			name:      "missing authenticated profile",
			configure: func(service *Service) { service.profiles = fakeProfile{err: errors.New("missing")} },
			call: func(service *Service) error {
				_, err := service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
					CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/synthetic.p12",
				}))
				return err
			},
			wantAuth: true,
		},
		{
			name: "missing current credential",
			call: func(service *Service) error {
				_, err := service.ReplaceMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ReplaceMachineCredentialRequest{
					CommandContext: command(PurposeReplaceMachineCredential), SelectedLocalPath: "/tmp/replacement.p12",
				}))
				return err
			},
		},
		{
			name: "stale credential state",
			configure: func(service *Service) {
				service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
					metadata: credential, state: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_EXPIRED,
				}
			},
			call: func(service *Service) error {
				_, err := service.RemoveMachineCredential(context.Background(), connect.NewRequest(&tammyv1.RemoveMachineCredentialRequest{
					CommandContext: command(PurposeRemoveMachineCredential),
				}))
				return err
			},
		},
		{
			name: "product mutation in simulator",
			configure: func(service *Service) {
				service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{metadata: credential, state: credential.State}
			},
			call: func(service *Service) error {
				_, err := service.ImportSbrProductId(context.Background(), connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{
					CommandContext: command(PurposeImportProductID), ProductIdValue: "synthetic-product",
					EvteProductIdentifier: "product", EvteServiceIdentifier: "service",
				}))
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := &fakeIdentity{}
			service := testService(t, &fakeHelper{}, identity)
			if test.configure != nil {
				test.configure(service)
			}
			if err := test.call(service); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("error = %v", err)
			}
			wantActions := 0
			if test.wantAuth {
				wantActions = 1
			}
			if len(identity.actions) != wantActions || len(identity.purposes) != 0 {
				t.Fatalf("precondition failure touched authorization: actions=%v purposes=%v", identity.actions, identity.purposes)
			}
		})
	}
}

func TestExactMutationReplayReturnsOwnedResultWithoutHelperOrFreshFactor(t *testing.T) {
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", CreatedAt: time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC), State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		switch request.Operation {
		case HelperOperationPrepareMutation:
			return validPreparedHelperResult(metadata), nil
		case HelperOperationCommitMutation:
			return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted,
				Credential: metadata, ProfileFingerprint: request.ProfileFingerprint, RegistrationFingerprint: request.RegistrationFingerprint,
				ComponentFingerprint: request.ComponentFingerprint, ComponentVersion: request.ComponentVersion}, nil
		default:
			return HelperResult{}, errors.New("unexpected helper operation")
		}
	}}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	invoke := func() (*connect.Response[tammyv1.ImportMachineCredentialResponse], error) {
		return service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
			CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/synthetic.p12",
			SecurityScopedBookmark: []byte("bookmark"), Password: []byte("transient"),
		}))
	}
	first, err := invoke()
	if err != nil {
		t.Fatalf("first import error = %v", err)
	}
	second, err := invoke()
	if err != nil {
		t.Fatalf("replay import error = %v", err)
	}
	if !proto.Equal(first.Msg, second.Msg) {
		t.Fatalf("replay result differs: first=%v second=%v", first.Msg, second.Msg)
	}
	if got, want := len(helper.requests), 2; got != want {
		t.Fatalf("helper requests = %d, want %d", got, want)
	}
	if got, want := len(identity.purposes), 1; got != want {
		t.Fatalf("fresh-factor consumptions = %d, want %d", got, want)
	}
}

func TestMutationIdempotencyKeyRejectsDifferentSemanticsWithoutTouchingFactor(t *testing.T) {
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		if request.Operation == HelperOperationPrepareMutation {
			return validPreparedHelperResult(metadata), nil
		}
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted,
			Credential: metadata, ProfileFingerprint: request.ProfileFingerprint, RegistrationFingerprint: request.RegistrationFingerprint,
			ComponentFingerprint: request.ComponentFingerprint, ComponentVersion: request.ComponentVersion}, nil
	}}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	_, err := service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
		CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/first.p12",
	}))
	if err != nil {
		t.Fatalf("first import error = %v", err)
	}
	_, err = service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
		CommandContext: command(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/different.p12",
	}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("conflicting replay error = %v", err)
	}
	if got, want := len(identity.purposes), 1; got != want {
		t.Fatalf("fresh-factor consumptions = %d, want %d", got, want)
	}
	if got, want := len(helper.requests), 2; got != want {
		t.Fatalf("helper requests = %d, want %d", got, want)
	}
}

func TestFixtureReplayIsElectedBeforeFreshFactorConsumption(t *testing.T) {
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultFixtureSelected,
			FixtureFailureCase: request.FixtureFailureCase, FixtureState: TransportAccepted}, nil
	}}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	invoke := func() (*connect.Response[tammyv1.RunSbrReadinessFixtureResponse], error) {
		return service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
			CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
			FailureCase: tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_UNSPECIFIED,
		}))
	}
	first, err := invoke()
	if err != nil {
		t.Fatalf("first fixture error = %v", err)
	}
	second, err := invoke()
	if err != nil {
		t.Fatalf("fixture replay error = %v", err)
	}
	if first.Msg.Result.GetOutcome() != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_ACCEPTED ||
		second.Msg.Result.GetOutcome() != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_EXACT_REPLAY ||
		!proto.Equal(first.Msg.Result.GetReadiness(), second.Msg.Result.GetReadiness()) {
		t.Fatalf("fixture/replay outcomes = first=%v second=%v", first.Msg, second.Msg)
	}
	if got, want := len(identity.purposes), 1; got != want {
		t.Fatalf("fresh-factor consumptions = %d, want %d", got, want)
	}
	if got, want := len(helper.requests), 1; got != want {
		t.Fatalf("helper requests = %d, want %d", got, want)
	}
	conflictRequest := &tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
		FailureCase: tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT,
	}
	conflict, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(conflictRequest))
	if err != nil || conflict.Msg.Result.GetOutcome() != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_IDEMPOTENCY_CONFLICT {
		t.Fatalf("fixture conflict = %v, %v", conflict, err)
	}
	if got, want := len(identity.purposes), 1; got != want {
		t.Fatalf("fresh-factor consumptions after conflict = %d, want %d", got, want)
	}
	if got, want := len(helper.requests), 1; got != want {
		t.Fatalf("helper requests after conflict = %d, want %d", got, want)
	}
}

func TestFixtureAuthorizationPrecedesDurableElection(t *testing.T) {
	identity := &fakeIdentity{authorizeErr: errors.New("expired session or wrong role")}
	service := testService(t, &fakeHelper{}, identity)
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	store := service.store.(*memoryServiceStore)
	store.bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}

	_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("unauthorized fixture error = %v, want permission denied", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.fixtures) != 0 {
		t.Fatalf("unauthorized fixture reserved durable election: %+v", store.fixtures)
	}
}

func TestFixtureProfileAuditOccursOnlyAfterAuthorizationAndCoversReplay(t *testing.T) {
	t.Run("rejected profile after authorization", func(t *testing.T) {
		identity := &fakeIdentity{}
		audit := &fakeAudit{}
		service := testService(t, &fakeHelper{}, identity)
		service.audit = audit
		service.profiles = fakeProfile{err: errors.New("profile unavailable")}
		_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
			CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
		}))
		if connect.CodeOf(err) != connect.CodeFailedPrecondition || len(identity.actions) != 1 {
			t.Fatalf("profile failure error=%v authorization actions=%v", err, identity.actions)
		}
		if len(audit.records) != 1 || audit.records[0].Action != AuditProfileRejected {
			t.Fatalf("profile rejection audit = %+v", audit.records)
		}
	})

	t.Run("rejected profile audit failure is closed", func(t *testing.T) {
		identity := &fakeIdentity{}
		service := testService(t, &fakeHelper{}, identity)
		service.audit = &fakeAudit{failOn: AuditProfileRejected}
		service.profiles = fakeProfile{err: errors.New("profile unavailable")}
		_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
			CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
		}))
		if connect.CodeOf(err) != connect.CodeInternal || len(identity.actions) != 1 {
			t.Fatalf("rejected profile audit failure error=%v actions=%v", err, identity.actions)
		}
	})

	t.Run("accepted profile on dispatch and replay", func(t *testing.T) {
		helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
			return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultFixtureSelected,
				FixtureFailureCase: request.FixtureFailureCase, FixtureState: TransportAccepted}, nil
		}}
		audit := &fakeAudit{}
		service := testService(t, helper, &fakeIdentity{})
		service.audit = audit
		binding := service.organisation.(fakeOrganisation).binding
		profile := service.profiles.(fakeProfile).profile
		metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
			ComponentVersion: profile.ComponentVersion, ExpiresAt: service.now().Add(time.Hour),
			State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
		service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
			metadata: metadata, profile: profile, state: metadata.State,
		}
		invoke := func() error {
			_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
				CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
			}))
			return err
		}
		if err := invoke(); err != nil {
			t.Fatal(err)
		}
		if err := invoke(); err != nil {
			t.Fatal(err)
		}
		accepted := 0
		for _, record := range audit.records {
			if record.Action == AuditProfileAccepted {
				accepted++
			}
		}
		if accepted != 2 {
			t.Fatalf("profile accepted audit count = %d, want dispatch+replay; records=%+v", accepted, audit.records)
		}
	})
}

func TestFixtureReplayReauthorizesRoleWithoutFreshFactor(t *testing.T) {
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultFixtureSelected,
			FixtureFailureCase: request.FixtureFailureCase, FixtureState: TransportAccepted}, nil
	}}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	request := &tammyv1.RunSbrReadinessFixtureRequest{CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID}
	if _, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(request)); err != nil {
		t.Fatalf("first fixture error = %v", err)
	}
	identity.authorizeErr = errors.New("expired session or wrong role")
	request.CommandContext = command(PurposeUseMachineCredential)
	_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(request))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("unauthorized fixture replay error = %v, want permission denied", err)
	}
	if got, want := len(identity.purposes), 1; got != want {
		t.Fatalf("fixture replay factor consumptions = %d, want %d", got, want)
	}
	if got, want := len(helper.requests), 1; got != want {
		t.Fatalf("fixture replay helper dispatches = %d, want %d", got, want)
	}
}

func TestFixtureConflictRejectsDifferentAuthorizedActorWithoutFreshFactor(t *testing.T) {
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultFixtureSelected,
			FixtureFailureCase: request.FixtureFailureCase, FixtureState: TransportAccepted}, nil
	}}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	request := &tammyv1.RunSbrReadinessFixtureRequest{CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID}
	if _, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(request)); err != nil {
		t.Fatalf("first fixture error = %v", err)
	}
	request.CommandContext = command(PurposeUseMachineCredential)
	request.CommandContext.Authentication.ActorUserId = "018f5f48-7f01-7b6e-86df-8b89f45e1016"
	request.FailureCase = tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT
	_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(request))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("cross-actor fixture conflict error = %v, want permission denied", err)
	}
	if got, want := len(identity.purposes), 1; got != want {
		t.Fatalf("cross-actor fixture replay factor consumptions = %d, want %d", got, want)
	}
	if got, want := len(helper.requests), 1; got != want {
		t.Fatalf("cross-actor fixture replay helper dispatches = %d, want %d", got, want)
	}
}

func TestUnlockAndFixtureConsumeFactorBeforeHelperAndNeverRefundIt(t *testing.T) {
	for _, test := range []struct {
		name      string
		helperErr error
		wantError bool
		call      func(*Service) error
	}{
		{name: "unlock helper death", helperErr: errors.New("helper died after reservation"), wantError: true, call: func(service *Service) error {
			_, err := service.UnlockMachineCredential(context.Background(), connect.NewRequest(&tammyv1.UnlockMachineCredentialRequest{
				CommandContext: command(PurposeUnlockMachineCredential), Password: []byte("transient"),
			}))
			return err
		}},
		{name: "unlock malformed response", wantError: true, call: func(service *Service) error {
			_, err := service.UnlockMachineCredential(context.Background(), connect.NewRequest(&tammyv1.UnlockMachineCredentialRequest{
				CommandContext: command(PurposeUnlockMachineCredential), Password: []byte("transient"),
			}))
			return err
		}},
		{name: "fixture helper death", helperErr: errors.New("helper died after reservation"), call: func(service *Service) error {
			_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
				CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
			}))
			return err
		}},
		{name: "fixture malformed response", call: func(service *Service) error {
			_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
				CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
			}))
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := &fakeIdentity{}
			helper := &fakeHelper{result: HelperResult{Outcome: HelperOutcomeOK}, err: test.helperErr}
			service := testService(t, helper, identity)
			binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
				VerificationExpiresAt: service.now().Add(time.Hour)}
			metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
				ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
				State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
			service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
				metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
			}
			if err := test.call(service); (err != nil) != test.wantError {
				t.Fatalf("helper outcome error = %v, wantError=%t", err, test.wantError)
			}
			if got, want := len(identity.purposes), 1; got != want {
				t.Fatalf("fresh-factor consumptions after helper outcome = %d, want %d", got, want)
			}
		})
	}
}

func TestConcurrentUnlocksWithSameFactorDispatchHelperExactlyOnce(t *testing.T) {
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", CreatedAt: time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultReady, Credential: metadata,
			ProfileFingerprint: request.ProfileFingerprint, RegistrationFingerprint: request.RegistrationFingerprint,
			ComponentFingerprint: request.ComponentFingerprint, ComponentVersion: request.ComponentVersion}, nil
	}}
	identity := &fakeIdentity{consumeOnce: true}
	service := testService(t, helper, identity)
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	var idMu sync.Mutex
	ids := []string{"018f5f48-7f01-7b6e-86df-8b89f45e1026", "018f5f48-7f01-7b6e-86df-8b89f45e1027"}
	service.newID = func() (string, error) {
		idMu.Lock()
		defer idMu.Unlock()
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			commandContext := command(PurposeUnlockMachineCredential)
			commandContext.IdempotencyKey = []string{
				"018f5f48-7f01-7b6e-86df-8b89f45e1036",
				"018f5f48-7f01-7b6e-86df-8b89f45e1037",
			}[index]
			_, err := service.UnlockMachineCredential(context.Background(), connect.NewRequest(&tammyv1.UnlockMachineCredentialRequest{
				CommandContext: commandContext, Password: []byte("transient"),
			}))
			results <- err
		}()
	}
	close(start)
	var succeeded int
	for index := 0; index < 2; index++ {
		if err := <-results; err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful concurrent unlocks = %d, want 1", succeeded)
	}
	if got, want := len(helper.requests), 1; got != want {
		t.Fatalf("concurrent helper dispatches = %d, want %d", got, want)
	}
}

func TestExpiredIndependentVerificationBlocksHelper(t *testing.T) {
	helper := &fakeHelper{}
	service := testService(t, helper, &fakeIdentity{})
	service.organisation = fakeOrganisation{binding: OrganisationBinding{OrganisationID: serviceOrganisationID,
		CanonicalABN: serviceABN, VerificationExpiresAt: service.now().Add(-time.Nanosecond)}}
	_, err := service.GetSbrReadiness(context.Background(), connect.NewRequest(&tammyv1.GetSbrReadinessRequest{
		Authentication: command("").Authentication,
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || len(helper.requests) != 0 {
		t.Fatalf("error=%v helper requests=%d", err, len(helper.requests))
	}
}

func TestProductStateProjectionIsClosed(t *testing.T) {
	for state, want := range map[ProductState]tammyv1.ProductIdState{
		ProductMissing:      tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING,
		ProductPresent:      tammyv1.ProductIdState_PRODUCT_ID_STATE_PRESENT,
		ProductInaccessible: tammyv1.ProductIdState_PRODUCT_ID_STATE_INACCESSIBLE,
	} {
		if got := productProjection(state); got != want {
			t.Fatalf("state=%v projection=%v want=%v", state, got, want)
		}
	}
}

func TestReadinessProjectsOnlyAuthenticatedEVTEProductAndServiceScope(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	profile := evteProfile(service.profiles.(fakeProfile).profile, "EVTE.PRODUCT", "EVTE.SERVICE")
	readiness := service.readiness(context.Background(), binding, profile, serviceBinding{}, false)
	if readiness.EvteProductIdentifier != profile.ExpectedProductIdentifier ||
		readiness.EvteServiceIdentifier != profile.ExpectedServiceID {
		t.Fatalf("readiness product scope = %q/%q", readiness.EvteProductIdentifier, readiness.EvteServiceIdentifier)
	}
}

func TestReadinessNeverProjectsEVTEScopeForSimulator(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	profile := service.profiles.(fakeProfile).profile
	profile.ExpectedProductIdentifier = "MUST.NOT.LEAK"
	profile.ExpectedServiceID = "MUST.NOT.LEAK"
	readiness := service.readiness(context.Background(), binding, profile, serviceBinding{}, false)
	if readiness.EvteProductIdentifier != "" || readiness.EvteServiceIdentifier != "" {
		t.Fatalf("simulator readiness projected EVTE scope = %q/%q", readiness.EvteProductIdentifier, readiness.EvteServiceIdentifier)
	}
}

func TestProductStateIsBoundToExactAuthenticatedProductAndServiceScope(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	store := service.store.(*memoryServiceStore)
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	profileA := evteProfile(service.profiles.(fakeProfile).profile, "EVTE.PRODUCT", "EVTE.SERVICE.A")
	profileB := evteProfile(profileA, "EVTE.PRODUCT", "EVTE.SERVICE.B")

	store.SetProductState(context.Background(), binding, profileA, ProductPresent, profileA.ProductScopeFingerprint, sha256.Sum256([]byte("product")))
	if got := store.ProductState(context.Background(), binding, profileA); got != ProductPresent {
		t.Fatalf("exact authenticated scope state = %v, want present", got)
	}
	if got := store.ProductState(context.Background(), binding, profileB); got != ProductMissing {
		t.Fatalf("cross-service authenticated scope state = %v, want missing", got)
	}
}

func TestProductMutationRejectsClientScopeDifferentFromAuthenticatedProfileBeforeFactorOrHelper(t *testing.T) {
	helper := &fakeHelper{}
	identity := &fakeIdentity{}
	service := testService(t, helper, identity)
	profile := evteProfile(service.profiles.(fakeProfile).profile, "EVTE.PRODUCT", "EVTE.SERVICE")
	service.profiles = fakeProfile{profile: profile}
	binding := service.organisation.(fakeOrganisation).binding
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: profile.ComponentVersion, ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: profile, state: metadata.State, bindingState: "ACTIVE",
	}

	_, err := service.ImportSbrProductId(context.Background(), connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{
		CommandContext: command(PurposeImportProductID), ProductIdValue: "synthetic-product",
		EvteProductIdentifier: profile.ExpectedProductIdentifier, EvteServiceIdentifier: "EVTE.OTHER",
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("cross-service Product mutation error = %v, want failed precondition", err)
	}
	if len(identity.purposes) != 0 || len(helper.requests) != 0 {
		t.Fatalf("scope mismatch reached factor/helper: purposes=%v helper=%d", identity.purposes, len(helper.requests))
	}
	store := service.store.(*memoryServiceStore)
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.commands) != 0 || len(store.pending) != 0 {
		t.Fatalf("scope mismatch won durable election: commands=%d pending=%d", len(store.commands), len(store.pending))
	}
}

func TestProductMutationUsesOnlyValidatedHelperState(t *testing.T) {
	for _, test := range []struct {
		name      string
		kind      MutationKind
		wantState ProductState
		call      func(*Service) (tammyv1.ProductIdState, error)
	}{
		{name: "import present", kind: MutationImportProductID, wantState: ProductPresent, call: func(service *Service) (tammyv1.ProductIdState, error) {
			response, err := service.ImportSbrProductId(context.Background(), connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{
				CommandContext: command(PurposeImportProductID), ProductIdValue: "synthetic-product",
				EvteProductIdentifier: "product", EvteServiceIdentifier: "service"}))
			if err != nil {
				return 0, err
			}
			return response.Msg.ProductIdState, nil
		}},
		{name: "remove missing", kind: MutationRemoveProductID, wantState: ProductMissing, call: func(service *Service) (tammyv1.ProductIdState, error) {
			response, err := service.RemoveSbrProductId(context.Background(), connect.NewRequest(&tammyv1.RemoveSbrProductIdRequest{
				CommandContext: command(PurposeRemoveProductID), EvteProductIdentifier: "product", EvteServiceIdentifier: "service"}))
			if err != nil {
				return 0, err
			}
			return response.Msg.ProductIdState, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			productFingerprint := sha256.Sum256([]byte("validated-product"))
			var service *Service
			helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
				switch request.Operation {
				case HelperOperationPrepareMutation:
					fingerprint := productFingerprint
					if test.wantState == ProductMissing {
						fingerprint = [sha256.Size]byte{}
					}
					return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomePending, PendingID: servicePendingID,
						ProductState: test.wantState, ProductFingerprint: fingerprint, ProfileFingerprint: service.profiles.(fakeProfile).profile.ProfileFingerprint,
						RegistrationFingerprint: service.profiles.(fakeProfile).profile.RegistrationFingerprint,
						ComponentFingerprint:    service.profiles.(fakeProfile).profile.ComponentFingerprint,
						ComponentVersion:        service.profiles.(fakeProfile).profile.ComponentVersion}, nil
				case HelperOperationCommitMutation:
					if request.ProductIdentifier != "product" || request.ServiceIdentifier != "service" {
						t.Fatalf("commit Product scope = %q/%q, want product/service", request.ProductIdentifier, request.ServiceIdentifier)
					}
					return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted,
						ProductState: test.wantState, ProductFingerprint: func() [sha256.Size]byte {
							if test.wantState == ProductMissing {
								return [sha256.Size]byte{}
							}
							return productFingerprint
						}(), ProfileFingerprint: request.ProfileFingerprint, RegistrationFingerprint: request.RegistrationFingerprint,
						ComponentFingerprint: request.ComponentFingerprint, ComponentVersion: request.ComponentVersion}, nil
				default:
					return HelperResult{}, errors.New("unexpected helper operation")
				}
			}}
			identity := &fakeIdentity{}
			service = testService(t, helper, identity)
			profile := evteProfile(service.profiles.(fakeProfile).profile, "product", "service")
			service.profiles = fakeProfile{profile: profile}
			binding := service.organisation.(fakeOrganisation).binding
			metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
				ComponentVersion: profile.ComponentVersion, ExpiresAt: service.now().Add(time.Hour),
				State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
			service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{metadata: metadata, profile: profile, state: metadata.State}
			got, err := test.call(service)
			if err != nil || got != productProjection(test.wantState) {
				t.Fatalf("Product mutation state = %v, error = %v", got, err)
			}
			if len(helper.requests) != 2 {
				t.Fatalf("helper requests = %d, want prepare+commit", len(helper.requests))
			}
		})
	}
}

func TestProductMutationRejectsMalformedPreparedHelperEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*HelperResult)
	}{
		{name: "missing closed state", mutate: func(result *HelperResult) { result.ProductState = 0 }},
		{name: "zero Product fingerprint", mutate: func(result *HelperResult) { result.ProductFingerprint = [sha256.Size]byte{} }},
		{name: "wrong request", mutate: func(result *HelperResult) { result.RequestID = serviceUserID }},
		{name: "wrong profile", mutate: func(result *HelperResult) { result.ProfileFingerprint = sha256.Sum256([]byte("wrong")) }},
		{name: "wrong registration", mutate: func(result *HelperResult) { result.RegistrationFingerprint = sha256.Sum256([]byte("wrong")) }},
		{name: "wrong component", mutate: func(result *HelperResult) { result.ComponentFingerprint = sha256.Sum256([]byte("wrong")) }},
		{name: "wrong component version", mutate: func(result *HelperResult) { result.ComponentVersion = "wrong" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var service *Service
			helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
				if request.Operation == HelperOperationAbortMutation {
					return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationAborted}, nil
				}
				if request.Operation != HelperOperationPrepareMutation {
					return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted}, nil
				}
				profile := service.profiles.(fakeProfile).profile
				result := HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomePending, PendingID: servicePendingID,
					ProductState: ProductPresent, ProductFingerprint: sha256.Sum256([]byte("product")),
					ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
					ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}
				test.mutate(&result)
				return result, nil
			}}
			identity := &fakeIdentity{}
			service = testService(t, helper, identity)
			profile := evteProfile(service.profiles.(fakeProfile).profile, "product", "service")
			service.profiles = fakeProfile{profile: profile}
			binding := service.organisation.(fakeOrganisation).binding
			metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
				ComponentVersion: profile.ComponentVersion, ExpiresAt: service.now().Add(time.Hour),
				State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
			service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{metadata: metadata, profile: profile, state: metadata.State}
			_, err := service.ImportSbrProductId(context.Background(), connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{
				CommandContext: command(PurposeImportProductID), ProductIdValue: "synthetic-product",
				EvteProductIdentifier: "product", EvteServiceIdentifier: "service"}))
			if connect.CodeOf(err) != connect.CodeUnavailable {
				t.Fatalf("malformed Product result error = %v, want unavailable", err)
			}
			if len(helper.requests) != 2 || helper.requests[1].Operation != HelperOperationAbortMutation {
				t.Fatalf("helper requests = %+v, want prepare then abort", helper.requests)
			}
			if len(identity.purposes) != 1 || identity.purposes[0] != PurposeImportProductID {
				t.Fatalf("dispatched malformed helper response factor reservation = %v", identity.purposes)
			}
		})
	}
}

func TestUsableCredentialBindingRequiresExactPersistedProfileAndPresentUnexpiredState(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	binding, profile, err := service.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: binding.CanonicalABN,
		ComponentVersion: profile.ComponentVersion, ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	valid := serviceBinding{metadata: metadata, profile: profile, state: metadata.State}
	if !usableCredentialBinding(valid, binding, profile, service.now()) {
		t.Fatal("exact current binding was rejected")
	}
	for _, test := range []struct {
		name   string
		mutate func(*serviceBinding)
	}{
		{name: "missing persisted profile", mutate: func(stored *serviceBinding) { stored.profile = RuntimeProfile{} }},
		{name: "profile mismatch", mutate: func(stored *serviceBinding) { stored.profile.ProfileFingerprint = sha256.Sum256([]byte("other")) }},
		{name: "registration mismatch", mutate: func(stored *serviceBinding) { stored.profile.RegistrationFingerprint = sha256.Sum256([]byte("other")) }},
		{name: "component mismatch", mutate: func(stored *serviceBinding) { stored.profile.ComponentFingerprint = sha256.Sum256([]byte("other")) }},
		{name: "environment mismatch", mutate: func(stored *serviceBinding) { stored.profile.Environment = tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE }},
		{name: "reimport required", mutate: func(stored *serviceBinding) {
			stored.state = tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_INACCESSIBLE
		}},
		{name: "revoked", mutate: func(stored *serviceBinding) {
			stored.state = tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_REVOKED
		}},
		{name: "expired state", mutate: func(stored *serviceBinding) {
			stored.state = tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_EXPIRED
		}},
		{name: "expired timestamp", mutate: func(stored *serviceBinding) { stored.metadata.ExpiresAt = service.now().Add(-time.Nanosecond) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := valid
			test.mutate(&stored)
			if usableCredentialBinding(stored, binding, profile, service.now()) {
				t.Fatal("stale or cross-bound credential was accepted")
			}
		})
	}
}

func TestTransientCredentialAndProductInputsAreClearedBeforeValidationReturns(t *testing.T) {
	service := testService(t, &fakeHelper{}, &fakeIdentity{})
	bookmarkBacking := []byte("bookmark-secret")
	passwordBacking := []byte("password-secret")
	importRequest := &tammyv1.ImportMachineCredentialRequest{SelectedLocalPath: "/private/credential.p12",
		SecurityScopedBookmark: bookmarkBacking, Password: passwordBacking}
	_, err := service.ImportMachineCredential(context.Background(), connect.NewRequest(importRequest))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("ImportMachineCredential() error = %v", err)
	}
	if importRequest.SelectedLocalPath != "" || importRequest.SecurityScopedBookmark != nil || importRequest.Password != nil {
		t.Fatalf("import protobuf retained transient fields: %+v", importRequest)
	}
	if !bytes.Equal(bookmarkBacking, make([]byte, len(bookmarkBacking))) || !bytes.Equal(passwordBacking, make([]byte, len(passwordBacking))) {
		t.Fatal("import protobuf backing buffers were not zeroed")
	}

	unlockBacking := []byte("unlock-secret")
	unlockRequest := &tammyv1.UnlockMachineCredentialRequest{Password: unlockBacking}
	_, err = service.UnlockMachineCredential(context.Background(), connect.NewRequest(unlockRequest))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || unlockRequest.Password != nil ||
		!bytes.Equal(unlockBacking, make([]byte, len(unlockBacking))) {
		t.Fatalf("unlock transient cleanup failed: request=%+v error=%v", unlockRequest, err)
	}

	productRequest := &tammyv1.ImportSbrProductIdRequest{ProductIdValue: "product-secret",
		EvteProductIdentifier: "scope-secret", EvteServiceIdentifier: "service-secret"}
	_, err = service.ImportSbrProductId(context.Background(), connect.NewRequest(productRequest))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || productRequest.ProductIdValue != "" ||
		productRequest.EvteProductIdentifier != "" || productRequest.EvteServiceIdentifier != "" {
		t.Fatalf("Product transient cleanup failed: request=%+v error=%v", productRequest, err)
	}
}

func TestUnlockAndFixtureAuditFailuresFailClosedBeforeSuccessOrDispatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		action AuditAction
		call   func(*Service) error
	}{
		{name: "unlock completed audit", action: AuditCredentialUnlocked, call: func(service *Service) error {
			_, err := service.UnlockMachineCredential(context.Background(), connect.NewRequest(&tammyv1.UnlockMachineCredentialRequest{
				CommandContext: command(PurposeUnlockMachineCredential), Password: []byte("transient"),
			}))
			return err
		}},
		{name: "fixture prepared audit", action: AuditFixturePrepared, call: func(service *Service) error {
			_, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
				CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
			}))
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
				ComponentVersion: "sim-v1", CreatedAt: time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC),
				State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
			helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
				result := HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, Credential: metadata,
					ProfileFingerprint: request.ProfileFingerprint, RegistrationFingerprint: request.RegistrationFingerprint,
					ComponentFingerprint: request.ComponentFingerprint, ComponentVersion: request.ComponentVersion}
				if request.Operation == HelperOperationFixture {
					result.ResultCode, result.FixtureState, result.FixtureFailureCase = HelperResultFixtureSelected, TransportAccepted, request.FixtureFailureCase
				} else {
					result.ResultCode = HelperResultReady
				}
				return result, nil
			}}
			service := testService(t, helper, &fakeIdentity{})
			audit := &fakeAudit{failOn: test.action}
			service.audit = audit
			binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
				VerificationExpiresAt: service.now().Add(time.Hour)}
			service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
				metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
			}
			if err := test.call(service); connect.CodeOf(err) != connect.CodeInternal {
				t.Fatalf("error = %v", err)
			}
			if test.action == AuditFixturePrepared && len(helper.requests) != 0 {
				t.Fatalf("fixture helper dispatched despite audit failure: %+v", helper.requests)
			}
		})
	}
}

func TestRedactedSQLAuditAppenderMarshalsOnlyClosedFields(t *testing.T) {
	now := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	appender, err := NewRedactedSQLAuditAppender(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	executor := &captureAuditExecutor{}
	credential := sha256.Sum256([]byte("credential"))
	profile := sha256.Sum256([]byte("profile"))
	component := sha256.Sum256([]byte("component"))
	if err := appender.Record(context.Background(), executor, AuditRecord{Action: AuditCredentialImported,
		CredentialFingerprint: credential, ProfileFingerprint: profile, ComponentFingerprint: component,
		StatusCode: "SBR_CREDENTIAL_IMPORTED"}); err != nil {
		t.Fatal(err)
	}
	if len(executor.args) != 7 {
		t.Fatalf("audit SQL args = %d", len(executor.args))
	}
	encoded, ok := executor.args[5].([]byte)
	if !ok {
		t.Fatalf("audit payload type = %T", executor.args[5])
	}
	var event tammyv1.SbrAuditEvent
	if err := proto.Unmarshal(encoded, &event); err != nil {
		t.Fatal(err)
	}
	if event.Action != tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_IMPORTED ||
		event.StatusCode != "SBR_CREDENTIAL_IMPORTED" || !bytes.Equal(event.CredentialFingerprint, credential[:]) ||
		!bytes.Equal(event.ProfileFingerprint, profile[:]) || !bytes.Equal(event.ComponentFingerprint, component[:]) {
		t.Fatalf("closed audit payload = %+v", &event)
	}
	for _, forbidden := range []string{"/tmp/credential.p12", "transient-password", "product-secret", "https://evte.invalid"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("audit payload retained forbidden field %q", forbidden)
		}
	}
}

func TestFixtureNeverRedispatchesUncertainSemanticOperationUnderNewKey(t *testing.T) {
	helper := &fakeHelper{err: errors.New("helper died after dispatch")}
	service := testService(t, helper, &fakeIdentity{})
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	requestFor := func(key string) *tammyv1.RunSbrReadinessFixtureRequest {
		commandContext := command(PurposeUseMachineCredential)
		commandContext.IdempotencyKey = key
		return &tammyv1.RunSbrReadinessFixtureRequest{CommandContext: commandContext, FixtureId: ReadinessFixtureID}
	}
	first, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(requestFor(serviceCommandID)))
	if err != nil || first.Msg.Result.GetOutcome() != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_HELPER_DEATH {
		t.Fatalf("first uncertain fixture = %v, %v", first, err)
	}
	unknown, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(requestFor("018f5f48-7f01-7b6e-86df-8b89f45e1015")))
	if err != nil || unknown.Msg.Result.GetOutcome() != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_UNKNOWN {
		t.Fatalf("new-key uncertain fixture = %v, %v", unknown, err)
	}
	if got, want := len(helper.requests), 1; got != want {
		t.Fatalf("helper dispatches = %d, want %d", got, want)
	}
}

func TestFixtureMapsAuthenticatedHelperDeadlineToTimeoutOutcome(t *testing.T) {
	helper := &fakeHelper{err: ErrHelperDeadlineExpired}
	service := testService(t, helper, &fakeIdentity{})
	audit := &fakeAudit{}
	service.audit = audit
	binding := service.organisation.(fakeOrganisation).binding
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	response, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
		FailureCase: tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT,
	}))
	if err != nil || response.Msg.Result.Outcome != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_TIMEOUT {
		t.Fatalf("deadline fixture response = %v, error = %v", response, err)
	}
	store := service.store.(*memoryServiceStore)
	store.mu.Lock()
	for _, fixture := range store.fixtures {
		if fixture.State != TransportMaybeSent {
			t.Fatalf("deadline fixture durable state = %s, want MAYBE_SENT", fixture.State)
		}
	}
	store.mu.Unlock()
	last := audit.records[len(audit.records)-1]
	if last.Action != AuditFixtureUnknown || last.StatusCode != "SBR_HELPER_FIXTURE_TIMEOUT" {
		t.Fatalf("deadline fixture audit = %+v", last)
	}
}

func TestFixtureMapsAuthenticatedMalformedHelperResponseToFailedOutcome(t *testing.T) {
	helper := &fakeHelper{err: ErrHelperMalformedResponse}
	service := testService(t, helper, &fakeIdentity{})
	audit := &fakeAudit{}
	service.audit = audit
	binding := service.organisation.(fakeOrganisation).binding
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	response, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
		FailureCase: tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH,
	}))
	if err != nil || response.Msg.Result.Succeeded ||
		response.Msg.Result.Outcome != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_MALFORMED_RESPONSE {
		t.Fatalf("malformed fixture response = %v, error = %v", response, err)
	}
	store := service.store.(*memoryServiceStore)
	store.mu.Lock()
	for _, fixture := range store.fixtures {
		if fixture.State != TransportFailed {
			t.Fatalf("malformed fixture durable state = %+v", fixture)
		}
	}
	store.mu.Unlock()
	last := audit.records[len(audit.records)-1]
	if last.Action != AuditFixtureCompleted || last.StatusCode != "SBR_HELPER_FIXTURE_REJECTED" {
		t.Fatalf("malformed fixture audit = %+v", last)
	}
}

func TestFixtureDoesNotInferHelperDeathOrTimeoutFromASelectedCaseEcho(t *testing.T) {
	for _, failure := range []tammyv1.SbrReadinessFixtureFailure{
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH,
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT,
	} {
		t.Run(failure.String(), func(t *testing.T) {
			helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
				return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK,
					ResultCode: HelperResultFixtureSelected, FixtureFailureCase: request.FixtureFailureCase,
					FixtureState: TransportMaybeSent}, nil
			}}
			service := testService(t, helper, &fakeIdentity{})
			binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
				VerificationExpiresAt: service.now().Add(time.Hour)}
			metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
				ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
				State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
			service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
				metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
			}

			response, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(
				&tammyv1.RunSbrReadinessFixtureRequest{CommandContext: command(PurposeUseMachineCredential),
					FixtureId: ReadinessFixtureID, FailureCase: failure},
			))
			if err != nil || response.Msg.Result.GetOutcome() != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_MALFORMED_RESPONSE {
				t.Fatalf("selected-case echo response = %v, error = %v", response, err)
			}
		})
	}
}

func TestFixturePersistsValidatedHelperTerminalStateNotRequestedCase(t *testing.T) {
	helper := &fakeHelper{result: HelperResult{RequestID: serviceOperationID, Outcome: HelperOutcomeOK,
		ResultCode: HelperResultFixtureSelected, FixtureFailureCase: tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE,
		FixtureState: TransportFailed}}
	service := testService(t, helper, &fakeIdentity{})
	binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
		VerificationExpiresAt: service.now().Add(time.Hour)}
	metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
		metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
	}
	response, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID,
		FailureCase: tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE,
	}))
	if err != nil || response.Msg.Result.Succeeded ||
		response.Msg.Result.GetOutcome() != tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_MALFORMED_RESPONSE {
		t.Fatalf("response = %v, error = %v", response, err)
	}
	store := service.store.(*memoryServiceStore)
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, record := range store.fixtures {
		if record.State != TransportFailed {
			t.Fatalf("durable state = %s, want %s", record.State, TransportFailed)
		}
	}
}

func TestAuditPayloadContainsOnlyClosedCodesAndFingerprints(t *testing.T) {
	credential, profile, component := sha256.Sum256([]byte("credential")), sha256.Sum256([]byte("profile")), sha256.Sum256([]byte("component"))
	for _, action := range []AuditAction{AuditCredentialImported, AuditCredentialUnlocked, AuditCredentialUsed,
		AuditCredentialFailed, AuditCredentialExpired, AuditCredentialReplaced, AuditCredentialRemoved,
		AuditCredentialSuspectedCompromise, AuditProductIDChanged, AuditProfileAccepted, AuditProfileRejected,
		AuditFixturePrepared, AuditFixtureDispatching, AuditFixtureCompleted, AuditFixtureUnknown} {
		payload, err := BuildAuditPayload(AuditRecord{Action: action, CredentialFingerprint: credential,
			ProfileFingerprint: profile, ComponentFingerprint: component, StatusCode: string(action)})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := proto.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"/tmp/credential.p12", "password", "product-id", "https://endpoint.invalid", serviceABN} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("%s audit retained %q", action, secret)
			}
		}
	}
}

func TestFixtureFailureCasesAreDurableAndNeverReportSuccess(t *testing.T) {
	for failure, want := range map[tammyv1.SbrReadinessFixtureFailure]TransportState{
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_NOT_STARTED:        TransportNotStarted,
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MAYBE_SENT:         TransportMaybeSent,
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE: TransportFailed,
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH:       TransportMaybeSent,
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT:            TransportMaybeSent,
	} {
		t.Run(failure.String(), func(t *testing.T) {
			wantOutcome := map[tammyv1.SbrReadinessFixtureFailure]tammyv1.SbrReadinessFixtureOutcome{
				tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_NOT_STARTED:        tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_NOT_STARTED,
				tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MAYBE_SENT:         tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_MAYBE_SENT,
				tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE: tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_MALFORMED_RESPONSE,
				tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH:       tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_HELPER_DEATH,
				tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT:            tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_TIMEOUT,
			}[failure]
			helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
				switch failure {
				case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE:
					return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK}, nil
				case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH:
					return HelperResult{}, errors.New("helper process ended")
				case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT:
					return HelperResult{}, ErrHelperDeadlineExpired
				}
				return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultFixtureSelected,
					FixtureFailureCase: request.FixtureFailureCase, FixtureState: want}, nil
			}}
			service := testService(t, helper, &fakeIdentity{})
			binding := OrganisationBinding{OrganisationID: serviceOrganisationID, CanonicalABN: serviceABN,
				VerificationExpiresAt: service.now().Add(time.Hour)}
			metadata := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
				ComponentVersion: "sim-v1", ExpiresAt: service.now().Add(time.Hour), State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
			service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{
				metadata: metadata, profile: service.profiles.(fakeProfile).profile, state: metadata.State,
			}
			request := &tammyv1.RunSbrReadinessFixtureRequest{CommandContext: command(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID, FailureCase: failure}
			response, err := service.RunSbrReadinessFixture(context.Background(), connect.NewRequest(request))
			if err != nil || response.Msg.Result.Succeeded || response.Msg.Result.GetOutcome() != wantOutcome {
				t.Fatalf("response=%v error=%v", response, err)
			}
			store := service.store.(*memoryServiceStore)
			store.mu.Lock()
			defer store.mu.Unlock()
			for _, record := range store.fixtures {
				if record.State != want {
					t.Fatalf("state=%s want=%s", record.State, want)
				}
			}
		})
	}
}

func TestMutationReplayAlwaysReauthorizesCurrentActorWithoutRequiringFreshFactor(t *testing.T) {
	credential := CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN,
		ComponentVersion: "sim-v1", ExpiresAt: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC),
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	for _, test := range []struct {
		name    string
		kind    MutationKind
		purpose string
		setup   func(*Service) (OrganisationBinding, RuntimeProfile, [sha256.Size]byte)
		call    func(*Service, *tammyv1.CommandContext) error
	}{
		{name: "import credential", kind: MutationImportCredential, purpose: PurposeImportMachineCredential,
			setup: func(service *Service) (OrganisationBinding, RuntimeProfile, [sha256.Size]byte) {
				binding, profile, _ := service.current(context.Background())
				return binding, profile, mutationSemanticHash(MutationImportCredential, binding, profile, "/fixture.p12", nil, nil, nil, "", "")
			}, call: func(service *Service, command *tammyv1.CommandContext) error {
				_, err := service.credentialMutation(context.Background(), command, authorisation.ActionImportSBRMachineCredential,
					PurposeImportMachineCredential, MutationImportCredential, "/fixture.p12", nil, nil)
				return err
			}},
		{name: "replace credential", kind: MutationReplaceCredential, purpose: PurposeReplaceMachineCredential,
			setup: func(service *Service) (OrganisationBinding, RuntimeProfile, [sha256.Size]byte) {
				binding, profile, _ := service.current(context.Background())
				service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{metadata: credential, profile: profile, state: credential.State}
				return binding, profile, mutationSemanticHash(MutationReplaceCredential, binding, profile, "/fixture.p12", nil, nil, nil, "", "")
			}, call: func(service *Service, command *tammyv1.CommandContext) error {
				_, err := service.credentialMutation(context.Background(), command, authorisation.ActionReplaceSBRMachineCredential,
					PurposeReplaceMachineCredential, MutationReplaceCredential, "/fixture.p12", nil, nil)
				return err
			}},
		{name: "remove credential", kind: MutationRemoveCredential, purpose: PurposeRemoveMachineCredential,
			setup: func(service *Service) (OrganisationBinding, RuntimeProfile, [sha256.Size]byte) {
				binding, profile, _ := service.current(context.Background())
				service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{metadata: credential, profile: profile, state: credential.State}
				return binding, profile, mutationSemanticHash(MutationRemoveCredential, binding, profile, "", nil, nil, nil, "", "")
			}, call: func(service *Service, command *tammyv1.CommandContext) error {
				_, err := service.credentialMutation(context.Background(), command, authorisation.ActionRemoveSBRMachineCredential,
					PurposeRemoveMachineCredential, MutationRemoveCredential, "", nil, nil)
				return err
			}},
		{name: "import product", kind: MutationImportProductID, purpose: PurposeImportProductID,
			setup: func(service *Service) (OrganisationBinding, RuntimeProfile, [sha256.Size]byte) {
				profilePort := service.profiles.(fakeProfile)
				profilePort.profile = evteProfile(profilePort.profile, "product", "service")
				service.profiles = profilePort
				binding, profile, _ := service.current(context.Background())
				service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{metadata: credential, profile: profile, state: credential.State}
				return binding, profile, mutationSemanticHash(MutationImportProductID, binding, profile, "", nil, nil, []byte("product-id"), "product", "service")
			}, call: func(service *Service, command *tammyv1.CommandContext) error {
				_, err := service.productMutation(context.Background(), command, PurposeImportProductID, MutationImportProductID,
					[]byte("product-id"), "product", "service")
				return err
			}},
		{name: "remove product", kind: MutationRemoveProductID, purpose: PurposeRemoveProductID,
			setup: func(service *Service) (OrganisationBinding, RuntimeProfile, [sha256.Size]byte) {
				profilePort := service.profiles.(fakeProfile)
				profilePort.profile = evteProfile(profilePort.profile, "product", "service")
				service.profiles = profilePort
				binding, profile, _ := service.current(context.Background())
				service.store.(*memoryServiceStore).bindings[organisationStoreKey(binding)] = serviceBinding{metadata: credential, profile: profile, state: credential.State}
				return binding, profile, mutationSemanticHash(MutationRemoveProductID, binding, profile, "", nil, nil, nil, "product", "service")
			}, call: func(service *Service, command *tammyv1.CommandContext) error {
				_, err := service.productMutation(context.Background(), command, PurposeRemoveProductID, MutationRemoveProductID,
					nil, "product", "service")
				return err
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := &fakeIdentity{}
			service := testService(t, &fakeHelper{}, identity)
			binding, _, semantic := test.setup(service)
			service.store.(*memoryServiceStore).commands[commandStoreKey(binding, serviceCommandID)] = commandResult{
				OperationID: serviceOperationID, ActorUserID: serviceUserID, Kind: test.kind, Semantic: semantic, Completed: true,
				Credential: credential, Product: ProductPresent,
			}

			identity.authorizeErr = errors.New("expired session or wrong role")
			if err := test.call(service, command(test.purpose)); connect.CodeOf(err) != connect.CodePermissionDenied {
				t.Fatalf("unauthorized replay error = %v, want permission denied", err)
			}
			if len(identity.purposes) != 0 {
				t.Fatalf("replay consumed fresh factor: %v", identity.purposes)
			}
		})
	}
}

func TestMutationReplayRejectsDifferentCurrentlyAuthorizedActor(t *testing.T) {
	identity := &fakeIdentity{}
	service := testService(t, &fakeHelper{}, identity)
	binding, profile, err := service.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	semantic := mutationSemanticHash(MutationImportCredential, binding, profile, "/fixture.p12", nil, nil, nil, "", "")
	service.store.(*memoryServiceStore).commands[commandStoreKey(binding, serviceCommandID)] = commandResult{
		OperationID: serviceOperationID, ActorUserID: serviceUserID, Kind: MutationImportCredential, Semantic: semantic, Completed: true,
		Credential: CredentialMetadata{Fingerprint: sha256.Sum256([]byte("credential")), CanonicalABN: serviceABN},
	}
	otherActor := command(PurposeImportMachineCredential)
	otherActor.Authentication.ActorUserId = "018f5f48-7f01-7b6e-86df-8b89f45e1099"
	_, err = service.credentialMutation(context.Background(), otherActor, authorisation.ActionImportSBRMachineCredential,
		PurposeImportMachineCredential, MutationImportCredential, "/fixture.p12", nil, nil)
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("cross-actor replay error = %v, want permission denied", err)
	}
}
