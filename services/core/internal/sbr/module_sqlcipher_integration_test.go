//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sbr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type integrationIdentity struct{}

func (integrationIdentity) RequireAdministratorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext) error {
	return nil
}
func (integrationIdentity) RequireActiveSessionReadOnly(context.Context, *tammyv1.AuthenticationContext) error {
	return nil
}

func (integrationIdentity) AuthorizeWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, authorisation.Action) error {
	return nil
}
func (integrationIdentity) ConsumeFreshFactorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error {
	return nil
}
func (integrationIdentity) ValidateAuthorizationWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, authorisation.Action) error {
	return nil
}
func (integrationIdentity) ValidateFreshFactorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error {
	return nil
}

type singleUseSQLFactorIdentity struct{}

func (singleUseSQLFactorIdentity) RequireAdministratorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext) error {
	return nil
}
func (singleUseSQLFactorIdentity) RequireActiveSessionReadOnly(context.Context, *tammyv1.AuthenticationContext) error {
	return nil
}
func (singleUseSQLFactorIdentity) AuthorizeWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, authorisation.Action) error {
	return nil
}
func (singleUseSQLFactorIdentity) ConsumeFreshFactorWithin(ctx context.Context, executor MutationExecutor, _ *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, _ string) error {
	result, err := executor.ExecContext(ctx, `UPDATE sbr_test_factors SET consumed=1 WHERE assertion_id=? AND consumed=0`, factor.GetAssertionId())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New("fresh factor already consumed")
	}
	return nil
}
func (singleUseSQLFactorIdentity) ValidateAuthorizationWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, authorisation.Action) error {
	return nil
}
func (singleUseSQLFactorIdentity) ValidateFreshFactorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error {
	return nil
}

type integrationHelper struct {
	mu         sync.Mutex
	requests   []HelperRequest
	credential CredentialMetadata
	product    ProductState
	productFP  [sha256.Size]byte
	pending    int
}

type gatedIntegrationHelper struct {
	delegate *integrationHelper
	mu       sync.Mutex
	enabled  bool
	entered  chan struct{}
	release  chan struct{}
}

func (helper *gatedIntegrationHelper) Execute(ctx context.Context, request HelperRequest) (HelperResult, error) {
	helper.mu.Lock()
	enabled := helper.enabled && request.Operation == HelperOperationPrepareMutation
	helper.mu.Unlock()
	if enabled {
		helper.entered <- struct{}{}
		select {
		case <-helper.release:
		case <-ctx.Done():
			return HelperResult{}, ctx.Err()
		}
	}
	return helper.delegate.Execute(ctx, request)
}

func (helper *gatedIntegrationHelper) Enable() {
	helper.mu.Lock()
	defer helper.mu.Unlock()
	helper.enabled = true
}

func (helper *integrationHelper) Execute(_ context.Context, request HelperRequest) (HelperResult, error) {
	helper.mu.Lock()
	defer helper.mu.Unlock()
	helper.requests = append(helper.requests, request.Clone())
	switch request.Operation {
	case HelperOperationPrepareMutation:
		helper.pending++
		pending := []string{
			"018f0000-0000-7000-8000-000000000781", "018f0000-0000-7000-8000-000000000782",
			"018f0000-0000-7000-8000-000000000783", "018f0000-0000-7000-8000-000000000784",
		}[helper.pending-1]
		if request.MutationKind == MutationImportCredential || request.MutationKind == MutationReplaceCredential {
			seed := byte(0x91 + helper.pending)
			helper.credential = CredentialMetadata{Fingerprint: digest(seed), CanonicalABN: testABN,
				Issuer: "Synthetic Test Issuer", Serial: "SIM-001", ComponentVersion: "simulator-v1",
				CreatedAt: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2027, 8, 23, 0, 0, 0, 0, time.UTC),
				State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
		}
		credential := helper.credential
		if request.MutationKind == MutationRemoveCredential || request.MutationKind == MutationImportProductID || request.MutationKind == MutationRemoveProductID {
			credential = CredentialMetadata{}
		}
		if request.MutationKind == MutationImportProductID {
			helper.product, helper.productFP = ProductPresent, digest(0x9d)
		}
		if request.MutationKind == MutationRemoveProductID {
			helper.product, helper.productFP = ProductMissing, [sha256.Size]byte{}
		}
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomePending, ResultCode: HelperResultNone,
			PendingID: pending, Credential: credential, ProductState: helper.product, ProductFingerprint: helper.productFP,
			ProfileFingerprint:      sha256.Sum256([]byte("profile")),
			RegistrationFingerprint: sha256.Sum256([]byte("registration")),
			ComponentFingerprint:    sha256.Sum256([]byte("component")), ComponentVersion: "simulator-v1"}, nil
	case HelperOperationCommitMutation, HelperOperationAbortMutation:
		resultCode := HelperResultMutationCommitted
		if request.Operation == HelperOperationAbortMutation {
			resultCode = HelperResultMutationAborted
		}
		credential := CredentialMetadata{}
		product, productFingerprint := ProductState(0), [sha256.Size]byte{}
		if request.Operation == HelperOperationCommitMutation &&
			(request.MutationKind == MutationImportCredential || request.MutationKind == MutationReplaceCredential) {
			credential = helper.credential
		}
		if request.Operation == HelperOperationCommitMutation &&
			(request.MutationKind == MutationImportProductID || request.MutationKind == MutationRemoveProductID) {
			product, productFingerprint = helper.product, helper.productFP
		}
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: resultCode,
			Credential: credential, ProductState: product, ProductFingerprint: productFingerprint,
			ProfileFingerprint: request.ProfileFingerprint, RegistrationFingerprint: request.RegistrationFingerprint,
			ComponentFingerprint: request.ComponentFingerprint, ComponentVersion: request.ComponentVersion}, nil
	case HelperOperationStatus, HelperOperationUnlock:
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultReady, Credential: helper.credential,
			ProfileFingerprint: request.ProfileFingerprint, RegistrationFingerprint: request.RegistrationFingerprint,
			ComponentFingerprint: request.ComponentFingerprint, ComponentVersion: request.ComponentVersion}, nil
	case HelperOperationFixture:
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultFixtureSelected, FixtureState: TransportAccepted}, nil
	default:
		return HelperResult{Outcome: HelperOutcomeFailed, StableCode: "SBR_HELPER_PROTOCOL_ERROR"}, nil
	}
}

type integrationIDs struct {
	mu   sync.Mutex
	next uint64
}

type inMemoryHandlerTransport struct{ handler http.Handler }

func (transport inMemoryHandlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	result := recorder.Result()
	payload, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, err
	}
	_ = result.Body.Close()
	result.Body = io.NopCloser(bytes.NewReader(payload))
	result.Request = request
	return result, nil
}

func (generator *integrationIDs) New() (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	generator.next++
	return fmt.Sprintf("018f0000-0000-7000-8000-%012x", generator.next+0x800), nil
}

func TestGeneratedConnectClientsExerciseAllNineSbrRPCsOverSQLCipher(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	profileHash, registrationHash, componentHash := sha256.Sum256([]byte("profile")), sha256.Sum256([]byte("registration")), sha256.Sum256([]byte("component"))
	helper := &integrationHelper{}
	generator := &integrationIDs{}
	store := newSQLServiceStore(repository, testWorkspaceID, func() time.Time { return now }, generator.New)
	service, err := NewService(ServiceConfig{WorkspaceID: testWorkspaceID, Identity: integrationIdentity{},
		Organisation: fakeOrganisation{binding: OrganisationBinding{OrganisationID: testOrganisation, CanonicalABN: testABN, VerificationExpiresAt: now.Add(time.Hour)}},
		Profiles: fakeProfile{profile: RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
			ComponentVersion:   "simulator-v1",
			ProfileFingerprint: profileHash, RegistrationFingerprint: registrationHash, ComponentFingerprint: componentHash, AuthenticatedUntil: now.Add(time.Hour)}},
		Helper: helper, Units: sqlUnitOfWork{database: database}, Store: store, Now: func() time.Time { return now }, NewID: generator.New,
		Audit:           discardAudit{},
		InstallationKey: bytes.Repeat([]byte{0x44}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	_, handler := tammyv1connect.NewSbrServiceHandler(service)
	httpClient := &http.Client{Transport: inMemoryHandlerTransport{handler: handler}}
	client := tammyv1connect.NewSbrServiceClient(httpClient, "http://tammy.local")
	auth := &tammyv1.AuthenticationContext{ActorUserId: serviceUserID, SessionId: serviceSessionID}
	commandFor := func(purpose string) *tammyv1.CommandContext {
		keys := map[string]string{
			PurposeImportMachineCredential:  "018f0000-0000-7000-8000-000000000901",
			PurposeUnlockMachineCredential:  "018f0000-0000-7000-8000-000000000902",
			PurposeReplaceMachineCredential: "018f0000-0000-7000-8000-000000000903",
			PurposeImportProductID:          "018f0000-0000-7000-8000-000000000904",
			PurposeRemoveProductID:          "018f0000-0000-7000-8000-000000000905",
			PurposeUseMachineCredential:     "018f0000-0000-7000-8000-000000000906",
			PurposeRemoveMachineCredential:  "018f0000-0000-7000-8000-000000000907",
		}
		return &tammyv1.CommandContext{IdempotencyKey: keys[purpose], Authentication: auth,
			FreshFactor: &tammyv1.FreshFactorContext{AssertionId: servicePendingID, Purpose: purpose, AssertedAt: timestamppb.New(now)}}
	}

	if _, err := client.GetSbrReadiness(ctx, connect.NewRequest(&tammyv1.GetSbrReadinessRequest{Authentication: auth})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ImportMachineCredential(ctx, connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{CommandContext: commandFor(PurposeImportMachineCredential), SelectedLocalPath: "/tmp/synthetic-machine-credential.p12", Password: []byte("synthetic-password")})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetMachineCredentialStatus(ctx, connect.NewRequest(&tammyv1.GetMachineCredentialStatusRequest{Authentication: auth})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UnlockMachineCredential(ctx, connect.NewRequest(&tammyv1.UnlockMachineCredentialRequest{CommandContext: commandFor(PurposeUnlockMachineCredential), Password: []byte("synthetic-password")})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReplaceMachineCredential(ctx, connect.NewRequest(&tammyv1.ReplaceMachineCredentialRequest{CommandContext: commandFor(PurposeReplaceMachineCredential), SelectedLocalPath: "/tmp/replacement-synthetic.p12", Password: []byte("replacement-password")})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ImportSbrProductId(ctx, connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{CommandContext: commandFor(PurposeImportProductID), ProductIdValue: "synthetic-product-id", EvteProductIdentifier: "SIM.PRODUCT", EvteServiceIdentifier: "SIM.SERVICE"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("ImportSbrProductId error=%v", err)
	}
	if _, err := client.RemoveSbrProductId(ctx, connect.NewRequest(&tammyv1.RemoveSbrProductIdRequest{CommandContext: commandFor(PurposeRemoveProductID), EvteProductIdentifier: "SIM.PRODUCT", EvteServiceIdentifier: "SIM.SERVICE"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("RemoveSbrProductId error=%v", err)
	}
	if _, err := client.RunSbrReadinessFixture(ctx, connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{CommandContext: commandFor(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID})); err != nil {
		t.Fatal(err)
	}
	helper.mu.Lock()
	afterFirstFixture := len(helper.requests)
	helper.mu.Unlock()
	if _, err := client.RunSbrReadinessFixture(ctx, connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{CommandContext: commandFor(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID})); err != nil {
		t.Fatalf("fixture replay: %v", err)
	}
	helper.mu.Lock()
	afterReplay := len(helper.requests)
	helper.mu.Unlock()
	if afterReplay != afterFirstFixture {
		t.Fatalf("fixture replay dispatched helper: before=%d after=%d", afterFirstFixture, afterReplay)
	}
	if _, err := client.RunSbrReadinessFixture(ctx, connect.NewRequest(&tammyv1.RunSbrReadinessFixtureRequest{CommandContext: commandFor(PurposeUseMachineCredential), FixtureId: ReadinessFixtureID, FailureCase: tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT})); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("fixture idempotency conflict=%v", err)
	}
	if _, err := client.RemoveMachineCredential(ctx, connect.NewRequest(&tammyv1.RemoveMachineCredentialRequest{CommandContext: commandFor(PurposeRemoveMachineCredential)})); err != nil {
		t.Fatal(err)
	}

	helper.mu.Lock()
	requests := append([]HelperRequest(nil), helper.requests...)
	helper.mu.Unlock()
	wantScope := DeriveOpaqueScope(bytes.Repeat([]byte{0x44}, sha256.Size), testWorkspaceID, testOrganisation, testABN)
	if len(requests) == 0 {
		t.Fatal("helper received no authenticated requests")
	}
	for _, request := range requests {
		if request.WorkspaceID != testWorkspaceID || request.OrganisationID != testOrganisation || request.CanonicalABN != testABN || !bytes.Equal(request.OpaqueScope, wantScope[:]) {
			t.Fatalf("helper received non-server-derived binding: %+v", request)
		}
	}
}

func TestGeneratedConnectProductRPCsSucceedWithAuthenticatedEVTEFakeAndPersistAcrossRestart(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	profileHash, registrationHash, componentHash := sha256.Sum256([]byte("profile")), sha256.Sum256([]byte("registration")), sha256.Sum256([]byte("component"))
	profile := RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE, ComponentVersion: "simulator-v1",
		ProfileFingerprint: profileHash, RegistrationFingerprint: registrationHash,
		ComponentFingerprint: componentHash, AuthenticatedUntil: now.Add(time.Hour), EndpointProfile: []byte("authenticated-evte-fixture"),
		ExpectedProductIdentifier: "EVTE.PRODUCT", ExpectedServiceID: "EVTE.SERVICE",
		ProductScopeFingerprint: authenticatedProductScopeFingerprint("EVTE.PRODUCT", "EVTE.SERVICE")}
	helper := &integrationHelper{}
	generator := &integrationIDs{}
	store := newSQLServiceStore(repository, testWorkspaceID, func() time.Time { return now }, generator.New)
	service, err := NewService(ServiceConfig{WorkspaceID: testWorkspaceID, Identity: integrationIdentity{},
		Organisation: fakeOrganisation{binding: OrganisationBinding{OrganisationID: testOrganisation, CanonicalABN: testABN, VerificationExpiresAt: now.Add(time.Hour)}},
		Profiles:     fakeProfile{profile: profile}, Helper: helper, Units: sqlUnitOfWork{database: database}, Store: store,
		Now: func() time.Time { return now }, NewID: generator.New, Audit: discardAudit{}, InstallationKey: bytes.Repeat([]byte{0x46}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	_, handler := tammyv1connect.NewSbrServiceHandler(service)
	client := tammyv1connect.NewSbrServiceClient(&http.Client{Transport: inMemoryHandlerTransport{handler: handler}}, "http://tammy.local")
	auth := &tammyv1.AuthenticationContext{ActorUserId: serviceUserID, SessionId: serviceSessionID}
	commandFor := func(key, purpose string) *tammyv1.CommandContext {
		return &tammyv1.CommandContext{IdempotencyKey: key, Authentication: auth,
			FreshFactor: &tammyv1.FreshFactorContext{AssertionId: servicePendingID, Purpose: purpose, AssertedAt: timestamppb.New(now)}}
	}
	if _, err := client.ImportMachineCredential(ctx, connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
		CommandContext:    commandFor("018f0000-0000-7000-8000-000000000a11", PurposeImportMachineCredential),
		SelectedLocalPath: "/tmp/synthetic-evte-machine-credential.p12"})); err != nil {
		t.Fatal(err)
	}
	helperRequestsBeforeMismatch := len(helper.requests)
	if _, err := client.ImportSbrProductId(ctx, connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{
		CommandContext: commandFor("018f0000-0000-7000-8000-000000000a14", PurposeImportProductID),
		ProductIdValue: "synthetic-product-id", EvteProductIdentifier: "EVTE.PRODUCT", EvteServiceIdentifier: "EVTE.OTHER"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("cross-service generated Product RPC error = %v", err)
	}
	if len(helper.requests) != helperRequestsBeforeMismatch {
		t.Fatalf("cross-service Product RPC dispatched helper: before=%d after=%d", helperRequestsBeforeMismatch, len(helper.requests))
	}
	imported, err := client.ImportSbrProductId(ctx, connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{
		CommandContext: commandFor("018f0000-0000-7000-8000-000000000a12", PurposeImportProductID),
		ProductIdValue: "synthetic-product-id", EvteProductIdentifier: "EVTE.PRODUCT", EvteServiceIdentifier: "EVTE.SERVICE"}))
	if err != nil || imported.Msg.ProductIdState != tammyv1.ProductIdState_PRODUCT_ID_STATE_PRESENT {
		t.Fatalf("ImportSbrProductId() = %v, %v", imported, err)
	}
	reopened := newSQLServiceStore(repository, testWorkspaceID, func() time.Time { return now }, generator.New)
	binding := OrganisationBinding{OrganisationID: testOrganisation, CanonicalABN: testABN, VerificationExpiresAt: now.Add(time.Hour)}
	if state := reopened.ProductState(ctx, binding, profile); state != ProductPresent {
		t.Fatalf("reopened Product state = %v, want present", state)
	}
	removed, err := client.RemoveSbrProductId(ctx, connect.NewRequest(&tammyv1.RemoveSbrProductIdRequest{
		CommandContext:        commandFor("018f0000-0000-7000-8000-000000000a13", PurposeRemoveProductID),
		EvteProductIdentifier: "EVTE.PRODUCT", EvteServiceIdentifier: "EVTE.SERVICE"}))
	if err != nil || removed.Msg.ProductIdState != tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING {
		t.Fatalf("RemoveSbrProductId() = %v, %v", removed, err)
	}
	if state := reopened.ProductState(ctx, binding, profile); state != ProductMissing {
		t.Fatalf("removed Product state = %v, want missing", state)
	}
}

func TestConcurrentMutationsSharingFreshFactorReserveBeforeHelperDispatch(t *testing.T) {
	for _, test := range []struct {
		name      string
		profile   func(RuntimeProfile) RuntimeProfile
		bootstrap bool
		call      func(*Service, *tammyv1.CommandContext) error
	}{
		{name: "credential", profile: func(profile RuntimeProfile) RuntimeProfile { return profile }, call: func(service *Service, command *tammyv1.CommandContext) error {
			_, err := service.ImportMachineCredential(context.Background(), connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
				CommandContext: command, SelectedLocalPath: "/tmp/synthetic-concurrent-credential.p12"}))
			return err
		}},
		{name: "Product", bootstrap: true, profile: func(profile RuntimeProfile) RuntimeProfile {
			return BindAuthenticatedProductScope(evteProfile(profile, "EVTE.PRODUCT", "EVTE.SERVICE"), "EVTE.PRODUCT", "EVTE.SERVICE")
		}, call: func(service *Service, command *tammyv1.CommandContext) error {
			_, err := service.ImportSbrProductId(context.Background(), connect.NewRequest(&tammyv1.ImportSbrProductIdRequest{
				CommandContext: command, ProductIdValue: "synthetic-product", EvteProductIdentifier: "EVTE.PRODUCT", EvteServiceIdentifier: "EVTE.SERVICE"}))
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repository, database, _ := newRepositoryHarness(t)
			if _, err := database.ExecContext(ctx, `CREATE TABLE sbr_test_factors(assertion_id TEXT PRIMARY KEY, consumed INTEGER NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			for _, assertion := range []string{"bootstrap-factor", "shared-factor"} {
				if _, err := database.ExecContext(ctx, `INSERT INTO sbr_test_factors(assertion_id,consumed) VALUES (?,0)`, assertion); err != nil {
					t.Fatal(err)
				}
			}
			now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
			profile := test.profile(RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
				ComponentVersion: "simulator-v1", ProfileFingerprint: sha256.Sum256([]byte("profile")),
				RegistrationFingerprint: sha256.Sum256([]byte("registration")), ComponentFingerprint: sha256.Sum256([]byte("component")),
				AuthenticatedUntil: now.Add(time.Hour)})
			baseHelper := &integrationHelper{}
			helper := &gatedIntegrationHelper{delegate: baseHelper, entered: make(chan struct{}, 2), release: make(chan struct{})}
			generator := &integrationIDs{}
			service, err := NewService(ServiceConfig{WorkspaceID: testWorkspaceID, Identity: singleUseSQLFactorIdentity{},
				Organisation: fakeOrganisation{binding: OrganisationBinding{OrganisationID: testOrganisation, CanonicalABN: testABN, VerificationExpiresAt: now.Add(time.Hour)}},
				Profiles:     fakeProfile{profile: profile}, Helper: helper, Units: sqlUnitOfWork{database: database},
				Store: newSQLServiceStore(repository, testWorkspaceID, func() time.Time { return now }, generator.New),
				Now:   func() time.Time { return now }, NewID: generator.New, Audit: discardAudit{}, InstallationKey: bytes.Repeat([]byte{0x51}, sha256.Size)})
			if err != nil {
				t.Fatal(err)
			}
			commandFor := func(key, assertion, purpose string) *tammyv1.CommandContext {
				return &tammyv1.CommandContext{IdempotencyKey: key,
					Authentication: &tammyv1.AuthenticationContext{ActorUserId: serviceUserID, SessionId: serviceSessionID},
					FreshFactor:    &tammyv1.FreshFactorContext{AssertionId: assertion, Purpose: purpose, AssertedAt: timestamppb.New(now)}}
			}
			if test.bootstrap {
				if _, err := service.ImportMachineCredential(ctx, connect.NewRequest(&tammyv1.ImportMachineCredentialRequest{
					CommandContext:    commandFor("bootstrap-command", "bootstrap-factor", PurposeImportMachineCredential),
					SelectedLocalPath: "/tmp/synthetic-bootstrap-credential.p12"})); err != nil {
					t.Fatalf("bootstrap import: %v", err)
				}
			}
			helper.Enable()
			purpose := PurposeImportMachineCredential
			if test.bootstrap {
				purpose = PurposeImportProductID
			}
			errorsFound := make(chan error, 2)
			for _, key := range []string{"concurrent-command-a", "concurrent-command-b"} {
				command := commandFor(key, "shared-factor", purpose)
				go func() { errorsFound <- test.call(service, command) }()
			}
			entered := 0
			deadline := time.NewTimer(250 * time.Millisecond)
			for entered < 2 {
				select {
				case <-helper.entered:
					entered++
				case <-deadline.C:
					entered = 2
				}
			}
			if !deadline.Stop() {
				select {
				case <-deadline.C:
				default:
				}
			}
			close(helper.release)
			successes, denied := 0, 0
			for range 2 {
				err := <-errorsFound
				if err == nil {
					successes++
				} else if connect.CodeOf(err) == connect.CodePermissionDenied {
					denied++
				}
			}
			if successes != 1 || denied != 1 {
				t.Fatalf("concurrent results: success=%d permission-denied=%d", successes, denied)
			}
			baseHelper.mu.Lock()
			prepared := 0
			for _, request := range baseHelper.requests {
				if request.Operation == HelperOperationPrepareMutation && ((!test.bootstrap && request.MutationKind == MutationImportCredential) ||
					(test.bootstrap && request.MutationKind == MutationImportProductID)) {
					prepared++
				}
			}
			baseHelper.mu.Unlock()
			if prepared != 1 {
				t.Fatalf("helper PREPARE dispatches = %d, want 1", prepared)
			}
			var commands int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_commands_v1`).Scan(&commands); err != nil {
				t.Fatal(err)
			}
			wantCommands := 1
			if test.bootstrap {
				wantCommands++
			}
			if commands != wantCommands {
				t.Fatalf("durable commands = %d, want %d", commands, wantCommands)
			}
		})
	}
}

func TestSbrModuleActivationReconcilesDurableStagedMutationUntilAbortAcknowledged(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0xa1)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-000000000a01"
	pendingID := "018f0000-0000-7000-8000-000000000a02"
	if err := repository.PrepareMutation(ctx, Mutation{OperationID: operationID, Key: key,
		Kind: MutationReplaceCredential, State: MutationPrepared, MetadataHash: digest(0xa2),
		CreatedAt: testTime, UpdatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkMutationStaged(ctx, key, operationID, pendingID, testTime); err != nil {
		t.Fatal(err)
	}

	profileHash, registrationHash, componentHash := digest(0xa3), digest(0xa4), digest(0xa5)
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		switch request.Operation {
		case HelperOperationReconcileMutation:
			return HelperResult{RequestID: operationID, Outcome: HelperOutcomePending, ResultCode: HelperResultRecoveryRequired,
				PendingID: pendingID, ProfileFingerprint: profileHash, RegistrationFingerprint: registrationHash,
				ComponentFingerprint: componentHash, ComponentVersion: "simulator-v1"}, nil
		case HelperOperationAbortMutation:
			return HelperResult{RequestID: operationID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationAborted,
				ProfileFingerprint: profileHash, RegistrationFingerprint: registrationHash,
				ComponentFingerprint: componentHash, ComponentVersion: "simulator-v1"}, nil
		default:
			return HelperResult{}, fmt.Errorf("unexpected recovery operation %d", request.Operation)
		}
	}}
	module, err := NewSbrModule(ModuleConfig{Helper: helper, Profiles: fakeProfile{profile: RuntimeProfile{
		Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR, ComponentVersion: "simulator-v1", ProfileFingerprint: profileHash,
		RegistrationFingerprint: registrationHash, ComponentFingerprint: componentHash,
		AuthenticatedUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}}, Audit: discardAudit{},
		InstallationKey: bytes.Repeat([]byte{0x55}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	ids := &integrationIDs{}
	if err := module.Activate(app.LocalWorkspaceActivation{Database: database, WorkspaceID: testWorkspaceID,
		Identity: integrationIdentity{}, Now: func() time.Time { return time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC) },
		NewID: ids.New}); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if len(helper.requests) != 2 || helper.requests[0].Operation != HelperOperationReconcileMutation ||
		helper.requests[1].Operation != HelperOperationAbortMutation {
		t.Fatalf("recovery helper operations = %+v", helper.requests)
	}
	stored, err := repository.GetMutation(ctx, key, operationID)
	if err != nil || stored.State != MutationAborted || stored.PendingID != "" {
		t.Fatalf("recovered mutation = %+v, error = %v", stored, err)
	}
	second, err := NewSbrModule(ModuleConfig{Helper: helper, Profiles: fakeProfile{profile: RuntimeProfile{
		Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR, ComponentVersion: "simulator-v1", ProfileFingerprint: profileHash,
		RegistrationFingerprint: registrationHash, ComponentFingerprint: componentHash,
		AuthenticatedUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}}, Audit: discardAudit{},
		InstallationKey: bytes.Repeat([]byte{0x55}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Activate(app.LocalWorkspaceActivation{Database: database, WorkspaceID: testWorkspaceID,
		Identity: integrationIdentity{}, Now: func() time.Time { return time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC) },
		NewID: ids.New}); err != nil {
		t.Fatalf("second Activate() error = %v", err)
	}
	if len(helper.requests) != 2 {
		t.Fatalf("settled mutation was reconciled again: %+v", helper.requests)
	}
}

func TestDurableRecoveryRejectsMismatchedAbortAckAndRetriesOnRestart(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0xb1)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-000000000b01"
	pendingID := "018f0000-0000-7000-8000-000000000b02"
	if err := repository.PrepareMutation(ctx, Mutation{OperationID: operationID, Key: key,
		Kind: MutationRemoveCredential, State: MutationPrepared, MetadataHash: digest(0xb2),
		CreatedAt: testTime, UpdatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkMutationStaged(ctx, key, operationID, pendingID, testTime); err != nil {
		t.Fatal(err)
	}
	profile := RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
		ComponentVersion: "simulator-v1", ProfileFingerprint: digest(0xb3), RegistrationFingerprint: digest(0xb4),
		ComponentFingerprint: digest(0xb5), AuthenticatedUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	helper := &fakeHelper{}
	reconcile := func(request HelperRequest) HelperResult {
		return HelperResult{RequestID: operationID, Outcome: HelperOutcomePending, ResultCode: HelperResultRecoveryRequired,
			PendingID: pendingID, ProfileFingerprint: profile.ProfileFingerprint,
			RegistrationFingerprint: profile.RegistrationFingerprint, ComponentFingerprint: profile.ComponentFingerprint,
			ComponentVersion: profile.ComponentVersion}
	}
	helper.execute = func(request HelperRequest) (HelperResult, error) {
		if request.Operation == HelperOperationReconcileMutation {
			return reconcile(request), nil
		}
		return HelperResult{RequestID: serviceUserID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationAborted,
			ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
			ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}, nil
	}
	now := func() time.Time { return time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC) }
	if err := recoverDurableMutations(ctx, repository, testWorkspaceID, fakeProfile{profile: profile}, helper, discardAudit{},
		bytes.Repeat([]byte{0x65}, sha256.Size), now); !errors.Is(err, ErrService) {
		t.Fatalf("mismatched abort ack error = %v, want ErrService", err)
	}
	stored, err := repository.GetMutation(ctx, key, operationID)
	if err != nil || stored.State != MutationAborting {
		t.Fatalf("mutation after rejected ack = %+v, error = %v", stored, err)
	}
	helper.execute = func(request HelperRequest) (HelperResult, error) {
		if request.Operation == HelperOperationReconcileMutation {
			return reconcile(request), nil
		}
		return HelperResult{RequestID: operationID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationAborted,
			ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
			ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}, nil
	}
	if err := recoverDurableMutations(ctx, repository, testWorkspaceID, fakeProfile{profile: profile}, helper, discardAudit{},
		bytes.Repeat([]byte{0x65}, sha256.Size), now); err != nil {
		t.Fatalf("recovery retry error = %v", err)
	}
	stored, err = repository.GetMutation(ctx, key, operationID)
	if err != nil || stored.State != MutationAborted || stored.PendingID != "" {
		t.Fatalf("recovered mutation = %+v, error = %v", stored, err)
	}
}

func TestSbrModuleActivationRecoversAbortAndCoreCommittedStates(t *testing.T) {
	for _, test := range []struct {
		name      string
		kind      MutationKind
		prepare   func(context.Context, *SQLCipherRepository, BindingKey, string, string) error
		wantOp    HelperOperation
		wantState MutationState
	}{
		{name: "abort required", kind: MutationRemoveCredential, prepare: func(ctx context.Context, repository *SQLCipherRepository, key BindingKey, operationID, pendingID string) error {
			return repository.AbortMutation(ctx, key, operationID, testTime)
		}, wantOp: HelperOperationAbortMutation, wantState: MutationAborted},
		{name: "aborting", kind: MutationRemoveCredential, prepare: func(ctx context.Context, repository *SQLCipherRepository, key BindingKey, operationID, pendingID string) error {
			if err := repository.AbortMutation(ctx, key, operationID, testTime); err != nil {
				return err
			}
			return repository.MarkMutationAbortDispatched(ctx, key, operationID, testTime)
		}, wantOp: HelperOperationAbortMutation, wantState: MutationAborted},
		{name: "core committed", kind: MutationRemoveCredential, prepare: func(ctx context.Context, repository *SQLCipherRepository, key BindingKey, operationID, pendingID string) error {
			command, err := repository.GetCommandByOperation(ctx, key.WorkspaceID, operationID)
			if err != nil {
				return err
			}
			mutation, err := repository.GetMutation(ctx, key, operationID)
			if err != nil {
				return err
			}
			return repository.CommitMutation(ctx, key, operationID, testMutationCommit(mutation, command))
		}, wantOp: HelperOperationCommitMutation, wantState: MutationHelperCommitted},
		{name: "product core committed", kind: MutationImportProductID, prepare: func(ctx context.Context, repository *SQLCipherRepository, key BindingKey, operationID, pendingID string) error {
			command, err := repository.GetCommandByOperation(ctx, key.WorkspaceID, operationID)
			if err != nil {
				return err
			}
			mutation, err := repository.GetMutation(ctx, key, operationID)
			if err != nil {
				return err
			}
			return repository.CommitMutation(ctx, key, operationID, testMutationCommit(mutation, command))
		}, wantOp: HelperOperationCommitMutation, wantState: MutationHelperCommitted},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repository, database, _ := newRepositoryHarness(t)
			key := testBindingKey(byte(0xc0 + len(test.name)))
			putTestBinding(t, repository, key)
			operationID := fmt.Sprintf("018f0000-0000-7000-8000-%012x", 0xc00+len(test.name))
			pendingID := fmt.Sprintf("018f0000-0000-7000-8000-%012x", 0xd00+len(test.name))
			mutation := Mutation{OperationID: operationID, Key: key,
				Kind: test.kind, State: MutationPrepared, MetadataHash: digest(0xc2),
				CreatedAt: testTime, UpdatedAt: testTime}
			prepareTestCommandMutation(t, repository, mutation)
			if err := repository.MarkMutationStaged(ctx, key, operationID, pendingID, testTime); err != nil {
				t.Fatal(err)
			}
			if err := test.prepare(ctx, repository, key, operationID, pendingID); err != nil {
				t.Fatal(err)
			}
			profile := RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
				ComponentVersion: "simulator-v1", ProfileFingerprint: digest(0xc3), RegistrationFingerprint: digest(0xc4),
				ComponentFingerprint: digest(0xc5), AuthenticatedUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpectedProductIdentifier: "TEST.PRODUCT", ExpectedServiceID: "TEST.SERVICE"}
			if test.kind == MutationImportProductID {
				profile.Environment = tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE
				profile.ProductScopeFingerprint = authenticatedProductScopeFingerprint(profile.ExpectedProductIdentifier, profile.ExpectedServiceID)
			}
			helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
				if test.kind == MutationImportProductID &&
					(request.ProductIdentifier != profile.ExpectedProductIdentifier || request.ServiceIdentifier != profile.ExpectedServiceID) {
					t.Fatalf("recovery Product scope = %q/%q, want %q/%q", request.ProductIdentifier, request.ServiceIdentifier,
						profile.ExpectedProductIdentifier, profile.ExpectedServiceID)
				}
				base := HelperResult{RequestID: operationID, ProfileFingerprint: profile.ProfileFingerprint,
					RegistrationFingerprint: profile.RegistrationFingerprint, ComponentFingerprint: profile.ComponentFingerprint,
					ComponentVersion: profile.ComponentVersion}
				if request.Operation == HelperOperationReconcileMutation {
					base.Outcome, base.ResultCode, base.PendingID = HelperOutcomePending, HelperResultRecoveryRequired, pendingID
					return base, nil
				}
				base.Outcome = HelperOutcomeOK
				if request.Operation == HelperOperationAbortMutation {
					base.ResultCode = HelperResultMutationAborted
				} else if request.Operation == HelperOperationCommitMutation {
					base.ResultCode = HelperResultMutationCommitted
					if test.kind == MutationImportProductID {
						base.ProductState, base.ProductFingerprint = ProductPresent, digest(0xf2)
					}
				} else {
					return HelperResult{}, fmt.Errorf("unexpected recovery operation %d", request.Operation)
				}
				return base, nil
			}}
			module, err := NewSbrModule(ModuleConfig{Helper: helper, Profiles: fakeProfile{profile: profile},
				Audit: discardAudit{}, InstallationKey: bytes.Repeat([]byte{0x66}, sha256.Size)})
			if err != nil {
				t.Fatal(err)
			}
			ids := &integrationIDs{}
			if err := module.Activate(app.LocalWorkspaceActivation{Database: database, WorkspaceID: testWorkspaceID,
				Identity: integrationIdentity{}, Now: func() time.Time { return time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC) },
				NewID: ids.New}); err != nil {
				t.Fatalf("Activate() error = %v", err)
			}
			if len(helper.requests) != 2 || helper.requests[0].Operation != HelperOperationReconcileMutation || helper.requests[1].Operation != test.wantOp {
				t.Fatalf("recovery operations = %+v", helper.requests)
			}
			stored, err := repository.GetMutation(ctx, key, operationID)
			if err != nil || stored.State != test.wantState || stored.PendingID != "" {
				t.Fatalf("recovered mutation = %+v, error = %v", stored, err)
			}
		})
	}
}

func TestRecoveryFinalizesCoreCommitWhenVaultWasAlreadyPromotedAndThenStaysTerminal(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0xe8)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-000000000e81"
	pendingID := "018f0000-0000-7000-8000-000000000e82"
	mutation := Mutation{OperationID: operationID, Key: key, Kind: MutationRemoveCredential,
		State: MutationPrepared, MetadataHash: digest(0xe8), CreatedAt: testTime, UpdatedAt: testTime}
	command := prepareTestCommandMutation(t, repository, mutation)
	if err := repository.MarkMutationStaged(ctx, key, operationID, pendingID, testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitMutation(ctx, key, operationID, testMutationCommit(mutation, command)); err != nil {
		t.Fatal(err)
	}
	if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingActive {
		t.Fatalf("binding changed before recovered helper ack = %#v, %v", binding, err)
	}
	profile := RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
		ComponentVersion: "simulator-v1", ProfileFingerprint: digest(0xe9), RegistrationFingerprint: digest(0xea),
		ComponentFingerprint: digest(0xeb), AuthenticatedUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		if request.Operation != HelperOperationReconcileMutation {
			return HelperResult{}, fmt.Errorf("helper called after vault promotion with operation %d", request.Operation)
		}
		return HelperResult{RequestID: operationID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted,
			ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
			ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}, nil
	}}
	now := func() time.Time { return time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC) }
	if err := recoverDurableMutations(ctx, repository, testWorkspaceID, fakeProfile{profile: profile}, helper,
		discardAudit{}, bytes.Repeat([]byte{0xee}, sha256.Size), now); err != nil {
		t.Fatal(err)
	}
	if len(helper.requests) != 1 || helper.requests[0].Operation != HelperOperationReconcileMutation {
		t.Fatalf("already-promoted recovery helper calls = %+v", helper.requests)
	}
	if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingRemoved {
		t.Fatalf("binding after recovered ack = %#v, %v", binding, err)
	}
	if stored, err := repository.GetCommand(ctx, command.Scope, command.IdempotencyKey); err != nil || stored.State != CommandCompleted {
		t.Fatalf("command after recovered ack = %#v, %v", stored, err)
	}
	recoverable, err := repository.ListRecoverableMutations(ctx, testWorkspaceID)
	if err != nil || len(recoverable) != 0 {
		t.Fatalf("terminal mutation remained recoverable = %+v, %v", recoverable, err)
	}
	terminalHelper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		return HelperResult{}, fmt.Errorf("terminal mutation unexpectedly invoked helper operation %d", request.Operation)
	}}
	if err := recoverDurableMutations(ctx, repository, testWorkspaceID, fakeProfile{profile: profile}, terminalHelper,
		discardAudit{}, bytes.Repeat([]byte{0xee}, sha256.Size), now); err != nil {
		t.Fatal(err)
	}
	if len(terminalHelper.requests) != 0 {
		t.Fatalf("terminal mutation invoked helper: %+v", terminalHelper.requests)
	}
}

func TestRecoveryRejectsCommittedReceiptDifferentFromPersistedPendingEffect(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0xf1)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-000000000f11"
	pendingID := "018f0000-0000-7000-8000-000000000f12"
	mutation := Mutation{OperationID: operationID, Key: key, Kind: MutationReplaceCredential,
		State: MutationPrepared, MetadataHash: digest(0xf1), CreatedAt: testTime, UpdatedAt: testTime}
	command := prepareTestCommandMutation(t, repository, mutation)
	if err := repository.MarkMutationStaged(ctx, key, operationID, pendingID, testTime); err != nil {
		t.Fatal(err)
	}
	commit := testMutationCommit(mutation, command)
	expires, err := time.Parse(time.RFC3339Nano, commit.NewBinding.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	commit.Command.Credential = CredentialMetadata{Fingerprint: commit.NewBinding.Key.CredentialFingerprint,
		CanonicalABN: key.CanonicalABN, ComponentVersion: commit.NewBinding.ComponentVersion, ExpiresAt: expires,
		State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	if err := repository.CommitMutation(ctx, key, operationID, commit); err != nil {
		t.Fatal(err)
	}
	profile := RuntimeProfile{Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
		ComponentVersion: "simulator-v1", ProfileFingerprint: digest(0xf2), RegistrationFingerprint: digest(0xf3),
		ComponentFingerprint: digest(0xf4), AuthenticatedUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	helper := &fakeHelper{execute: func(request HelperRequest) (HelperResult, error) {
		if request.Operation == HelperOperationReconcileMutation {
			return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomePending, ResultCode: HelperResultRecoveryRequired,
				PendingID: pendingID, ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
				ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}, nil
		}
		wrong := commit.Command.Credential
		wrong.Fingerprint = digest(0xff)
		return HelperResult{RequestID: request.RequestID, Outcome: HelperOutcomeOK, ResultCode: HelperResultMutationCommitted,
			Credential: wrong, ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
			ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}, nil
	}}
	if err := recoverDurableMutations(ctx, repository, testWorkspaceID, fakeProfile{profile: profile}, helper, discardAudit{},
		bytes.Repeat([]byte{0xfa}, sha256.Size), func() time.Time { return time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC) }); !errors.Is(err, ErrService) {
		t.Fatalf("mismatched committed receipt recovery error = %v, want ErrService", err)
	}
	if stored, err := repository.GetMutation(ctx, key, operationID); err != nil || stored.State != MutationReconcileRequired {
		t.Fatalf("mismatched receipt finalized mutation = %+v, %v", stored, err)
	}
	if current, err := repository.GetCurrentBinding(ctx, withFingerprint(key, [sha256.Size]byte{})); err != nil || current.Key != key {
		t.Fatalf("mismatched receipt changed visible binding = %+v, %v", current, err)
	}
}
