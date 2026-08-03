//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type testCapabilities map[string]string

func (capabilities testCapabilities) Resolve(_ context.Context, reference *tammyv1.ApprovedFileRef, _ CapabilityKind) (string, error) {
	path, ok := capabilities[reference.GetCapabilityId()]
	if !ok {
		return "", errors.New("unknown capability")
	}
	return path, nil
}

type testWorkspaceIdentity struct {
	now                    *time.Time
	adminRecovered         *bool
	adminRecoveryMutations *int
	adminRecoveryResults   map[string]*tammyv1.User
	adminRecoverySemantics map[string]string
	invalidations          *int
	bootstrapCalls         *int
	assertions             map[string]*testAssertion
	adminRecoveryErr       error
	activeSessionErr       error
	activeSessionReads     *int
	activeSessionMutations *int
}

type testAssertion struct {
	purpose  string
	asserted time.Time
	consumed bool
}

func (identity testWorkspaceIdentity) BootstrapAdministrator(context.Context, string, string, []byte) (*tammyv1.User, error) {
	if identity.bootstrapCalls != nil {
		(*identity.bootstrapCalls)++
	}
	return &tammyv1.User{Id: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", Version: 1,
		State: tammyv1.UserState_USER_STATE_ACTIVE, Roles: []tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN}}, nil
}
func (identity testWorkspaceIdentity) BootstrapAdministratorWithin(ctx context.Context, _ MutationExecutor, _ string,
	username, displayName string, password []byte) (*tammyv1.User, error) {
	return identity.BootstrapAdministrator(ctx, username, displayName, password)
}
func (identity testWorkspaceIdentity) BreakGlassResetAdministrator(_ context.Context, operationID string, username string, password []byte) (*tammyv1.User, error) {
	if identity.adminRecoveryErr != nil {
		return nil, identity.adminRecoveryErr
	}
	semantic := username + "\x00" + string(password)
	if retained := identity.adminRecoveryResults[operationID]; retained != nil {
		if identity.adminRecoverySemantics[operationID] != semantic {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		return proto.Clone(retained).(*tammyv1.User), nil
	}
	if identity.adminRecovered != nil {
		*identity.adminRecovered = true
	}
	if identity.adminRecoveryMutations != nil {
		*identity.adminRecoveryMutations++
	}
	result := &tammyv1.User{Id: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", Version: 2, Username: username,
		State: tammyv1.UserState_USER_STATE_ACTIVE, Roles: []tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN}}
	identity.adminRecoveryResults[operationID] = proto.Clone(result).(*tammyv1.User)
	identity.adminRecoverySemantics[operationID] = semantic
	return result, nil
}
func (identity testWorkspaceIdentity) BreakGlassResetAdministratorWithin(ctx context.Context, _ MutationExecutor, operationID, username string, password []byte) (*tammyv1.User, error) {
	return identity.BreakGlassResetAdministrator(ctx, operationID, username, password)
}
func (testWorkspaceIdentity) RequireAdministrator(_ context.Context, authentication *tammyv1.AuthenticationContext) error {
	if authentication == nil {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return nil
}
func (identity testWorkspaceIdentity) RequireAdministratorReadOnly(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	return identity.RequireAdministrator(ctx, authentication)
}
func (testWorkspaceIdentity) ValidateAdministratorReplayBinding(_ context.Context, authentication *tammyv1.AuthenticationContext,
	boundActorUserID, boundSessionID string) error {
	if authentication == nil || authentication.ActorUserId != boundActorUserID || authentication.SessionId != boundSessionID {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return nil
}
func (identity testWorkspaceIdentity) RequireAdministratorWithin(ctx context.Context, _ MutationExecutor, authentication *tammyv1.AuthenticationContext) error {
	return identity.RequireAdministrator(ctx, authentication)
}
func (identity testWorkspaceIdentity) RequireActiveSessionReadOnly(_ context.Context, authentication *tammyv1.AuthenticationContext) error {
	if identity.activeSessionReads != nil {
		(*identity.activeSessionReads)++
	}
	if identity.activeSessionErr != nil {
		return identity.activeSessionErr
	}
	if authentication == nil {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return nil
}
func (identity testWorkspaceIdentity) RequireActiveSessionWithin(_ context.Context, _ MutationExecutor,
	authentication *tammyv1.AuthenticationContext) error {
	if identity.activeSessionMutations != nil {
		(*identity.activeSessionMutations)++
	}
	if identity.activeSessionErr != nil {
		return identity.activeSessionErr
	}
	if authentication == nil {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return nil
}
func (identity testWorkspaceIdentity) ConsumeFreshFactor(_ context.Context, _ *tammyv1.AuthenticationContext, marker *tammyv1.FreshFactorContext, purpose string) error {
	if err := authorisation.ValidateFreshFactor(marker, purpose, *identity.now); err != nil {
		return err
	}
	assertion := identity.assertions[marker.AssertionId]
	if assertion == nil || assertion.consumed || assertion.purpose != purpose || !assertion.asserted.Equal(marker.AssertedAt.AsTime()) {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	assertion.consumed = true
	return nil
}
func (identity testWorkspaceIdentity) ConsumeFreshFactorWithin(ctx context.Context, _ MutationExecutor,
	authentication *tammyv1.AuthenticationContext, marker *tammyv1.FreshFactorContext, purpose string) error {
	return identity.ConsumeFreshFactor(ctx, authentication, marker, purpose)
}
func (testWorkspaceIdentity) IsActiveAdministrator(context.Context, string) bool { return true }
func (testWorkspaceIdentity) IsActiveAdministratorWithin(context.Context, MutationExecutor, string) (bool, error) {
	return true, nil
}
func (identity testWorkspaceIdentity) InvalidateAllSessions(context.Context) error {
	if identity.invalidations != nil {
		*identity.invalidations++
	}
	return nil
}
func (identity testWorkspaceIdentity) InvalidateAllSessionsWithin(ctx context.Context, _ MutationExecutor) error {
	return identity.InvalidateAllSessions(ctx)
}

func (identity testWorkspaceIdentity) issueFactor(id, purpose string, asserted time.Time) *tammyv1.FreshFactorContext {
	identity.assertions[id] = &testAssertion{purpose: purpose, asserted: asserted}
	return factorMarker(id, purpose, asserted)
}

type testMutationAudit struct {
	counts map[string]int
	fail   error
}

type trackingSecretStore struct {
	delegate    *MemorySecretStore
	deleteErr   error
	deleteCalls int
}

func newTrackingSecretStore() *trackingSecretStore {
	return &trackingSecretStore{delegate: NewMemorySecretStore()}
}

func (store *trackingSecretStore) Put(label string, secret []byte) error {
	return store.delegate.Put(label, secret)
}

func (store *trackingSecretStore) Get(label string) ([]byte, error) {
	return store.delegate.Get(label)
}

func (store *trackingSecretStore) Delete(label string) error {
	store.deleteCalls++
	if store.deleteErr != nil {
		err := store.deleteErr
		store.deleteErr = nil
		return err
	}
	return store.delegate.Delete(label)
}

func (store *trackingSecretStore) Count() int { return store.delegate.Count() }

func (audit *testMutationAudit) AppendWorkspaceMutation(_ context.Context, _ MutationExecutor, mutation WorkspaceMutation) error {
	if audit.fail != nil {
		return audit.fail
	}
	audit.counts[mutation.Kind]++
	return nil
}

type testOrganisationImpact struct {
	calls int
	fail  error
}

type testFailureCheckpoints struct {
	failures map[string]error
	hooks    map[string]func()
}

type failNextWorkspaceSaveRepository struct {
	Repository
	failNext error
}

type saveFailingAnchorStore struct {
	AnchorStore
	err error
}

func (store saveFailingAnchorStore) Save(string, []byte, attemptJournalLease) error {
	return store.err
}

func (repository *failNextWorkspaceSaveRepository) Save(ctx context.Context, record workspaceRecord) error {
	if repository.failNext != nil {
		err := repository.failNext
		repository.failNext = nil
		return err
	}
	return repository.Repository.Save(ctx, record)
}

type failNextNormalizeRepository struct {
	Repository
	failNext error
}

func (repository *failNextNormalizeRepository) NormalizeOpen(ctx context.Context) error {
	if repository.failNext != nil {
		err := repository.failNext
		repository.failNext = nil
		return err
	}
	return repository.Repository.NormalizeOpen(ctx)
}

func (checkpoints *testFailureCheckpoints) Check(name string) error {
	if hook := checkpoints.hooks[name]; hook != nil {
		delete(checkpoints.hooks, name)
		hook()
	}
	err := checkpoints.failures[name]
	delete(checkpoints.failures, name)
	return err
}

func (impact *testOrganisationImpact) ApplyOwnershipTransfer(_ context.Context, _ MutationExecutor, _ OwnershipImpact) error {
	if impact.fail != nil {
		return impact.fail
	}
	impact.calls++
	return nil
}

type workspaceHarness struct {
	service                *Service
	repository             Repository
	config                 Config
	now                    *time.Time
	databasePath           string
	attemptPath            string
	attemptKey             []byte
	attemptAnchorID        string
	attemptAnchors         AnchorStore
	adminRecovered         *bool
	adminRecoveryMutations *int
	invalidations          *int
	bootstrapCalls         *int
	activeSessionReads     *int
	activeSessionMutations *int
	identity               *testWorkspaceIdentity
	audit                  *testMutationAudit
	organisations          *testOrganisationImpact
	failures               *testFailureCheckpoints
}

func newWorkspaceHarness(t *testing.T) workspaceHarness {
	return newWorkspaceHarnessWithRepository(t, nil)
}

func newWorkspaceHarnessWithRepository(t *testing.T, repository Repository) workspaceHarness {
	t.Helper()
	now := time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC)
	source := clock.Func(func() time.Time { return now })
	policy, err := NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	generator, err := ids.NewGenerator(source, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "tammy-workspace.db")
	attemptPath := filepath.Join(t.TempDir(), "attempts.journal")
	attemptKey := bytes.Repeat([]byte{3}, 32)
	attemptAnchorID := "workspace/test-service"
	attemptAnchors := NewMemoryAnchorStore()
	journal, err := NewAttemptJournal(attemptPath, attemptKey, source, attemptAnchorID, attemptAnchors)
	if err != nil {
		t.Fatal(err)
	}
	if repository == nil {
		repository = NewMemoryRepository()
	}
	adminRecovered := false
	adminRecoveryMutations := 0
	invalidations := 0
	bootstrapCalls := 0
	activeSessionReads := 0
	activeSessionMutations := 0
	identity := &testWorkspaceIdentity{now: &now, adminRecovered: &adminRecovered, adminRecoveryMutations: &adminRecoveryMutations,
		adminRecoveryResults: make(map[string]*tammyv1.User), adminRecoverySemantics: make(map[string]string), invalidations: &invalidations,
		bootstrapCalls: &bootstrapCalls, activeSessionReads: &activeSessionReads, activeSessionMutations: &activeSessionMutations,
		assertions: make(map[string]*testAssertion)}
	audit := &testMutationAudit{counts: make(map[string]int)}
	organisations := &testOrganisationImpact{}
	failures := &testFailureCheckpoints{failures: make(map[string]error), hooks: make(map[string]func())}
	config := Config{Repository: repository, Capabilities: testCapabilities{
		"destination-capability": directory, "workspace-file-capability": databasePath,
	}, Storage: NewSQLCipherStorageFactory(2), Identity: identity, Audit: audit, OrganisationImpact: organisations,
		FailureCheckpoints: failures, Passwords: policy,
		RememberedKeys: mustRememberedManager(t, source), Attempts: journal, Clock: source, IDs: generator,
		HeaderAuthenticationKey: bytes.Repeat([]byte{4}, 32), InstallationKey: bytes.Repeat([]byte{5}, 32)}
	service, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceHarness{service: service, repository: repository, config: config, now: &now, databasePath: databasePath,
		attemptPath: attemptPath, attemptKey: attemptKey, attemptAnchorID: attemptAnchorID, attemptAnchors: attemptAnchors,
		adminRecovered: &adminRecovered, adminRecoveryMutations: &adminRecoveryMutations, invalidations: &invalidations, identity: identity, audit: audit,
		organisations: organisations, failures: failures, bootstrapCalls: &bootstrapCalls, activeSessionReads: &activeSessionReads,
		activeSessionMutations: &activeSessionMutations}
}

func TestNewWorkspaceServiceNormalizesStaleOpenCatalogueState(t *testing.T) {
	for _, staleState := range []tammyv1.WorkspaceState{
		tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
		tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
	} {
		t.Run(staleState.String(), func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			record := workspaceRecord{ID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073970", State: staleState, SetupPhase: "confirmed",
				DatabasePath: harness.databasePath, HeaderPath: harness.databasePath + ".header"}
			if err := harness.repository.Save(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if _, err := NewService(harness.config); err != nil {
				t.Fatal(err)
			}
			normalized, err := harness.repository.ByID(context.Background(), record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if normalized.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
				t.Fatalf("stale open state remained %v", normalized.State)
			}
		})
	}
}

func TestNewWorkspaceServiceNormalizationFailureIsRetryableAndPreservesWorkspaceFiles(t *testing.T) {
	cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
	repository, err := NewFileRepository(cataloguePath, bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	harness := newWorkspaceHarnessWithRepository(t, repository)
	record := workspaceRecord{ID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073969", State: tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
		SetupPhase: "confirmed", DatabasePath: harness.databasePath, HeaderPath: harness.databasePath + ".header"}
	if err := repository.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	databaseMarker := []byte("confirmed-sqlcipher-database")
	headerMarker := []byte("confirmed-authenticated-header")
	if err := os.WriteFile(record.DatabasePath, databaseMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record.HeaderPath, headerMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("catalogue normalization interrupted")
	failing := &failNextNormalizeRepository{Repository: repository, failNext: injected}
	config := harness.config
	config.Repository = failing
	if service, err := NewService(config); !errors.Is(err, injected) || service != nil {
		t.Fatalf("failed normalization service_present=%t err=%v", service != nil, err)
	}
	unmodified, err := repository.ByID(context.Background(), record.ID)
	if err != nil || unmodified.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
		t.Fatalf("failed normalization state=%v err=%v", unmodified.State, err)
	}
	for path, want := range map[string][]byte{record.DatabasePath: databaseMarker, record.HeaderPath: headerMarker} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("failed normalization changed %s: got=%q err=%v", path, got, err)
		}
	}
	if _, err := NewService(config); err != nil {
		t.Fatalf("normalization retry: %v", err)
	}
	normalized, err := repository.ByID(context.Background(), record.ID)
	if err != nil || normalized.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("normalization retry state=%v err=%v", normalized.State, err)
	}
	for path, want := range map[string][]byte{record.DatabasePath: databaseMarker, record.HeaderPath: headerMarker} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("normalization retry changed %s: got=%q err=%v", path, got, err)
		}
	}
}

func mustRememberedManager(t *testing.T, source clock.Clock) *RememberedKeyManager {
	t.Helper()
	manager, err := NewRememberedKeyManager(NewMemorySecretStore(), source)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestWorkspaceServiceCreateConfirmLockUnlockRestart(t *testing.T) {
	cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
	catalogueKey := bytes.Repeat([]byte{5}, 32)
	initialRepository, err := NewFileRepository(cataloguePath, catalogueKey)
	if err != nil {
		t.Fatal(err)
	}
	harness := newWorkspaceHarnessWithRepository(t, initialRepository)
	ctx := context.Background()
	created, err := harness.service.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId:               "01890f3c-7b2e-7cc4-98c4-dc0c0c073911",
		Destination:           &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
		WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if created.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY ||
		!created.Msg.ExpiresAt.AsTime().Equal(harness.now.Add(15*time.Minute)) {
		t.Fatalf("unexpected pending workspace: %v", created.Msg)
	}
	groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 12, Value: groups[12]},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
		t.Fatal("confirmation did not open workspace")
	}
	locked, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{}))
	if err != nil || locked.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("lock: %v", err)
	}
	if *harness.invalidations != 1 {
		t.Fatalf("lock invalidated %d session sets", *harness.invalidations)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("wrong-passphrase-long-enough", false))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("wrong passphrase returned %v", err)
	}
	unlocked, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false)))
	if err != nil || unlocked.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
		t.Fatalf("unlock: %v", err)
	}
	if *harness.invalidations != 2 {
		t.Fatalf("proved unlock invalidated %d session sets, want lock plus reopen", *harness.invalidations)
	}
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	restartedRepository, err := NewFileRepository(cataloguePath, catalogueKey)
	if err != nil {
		t.Fatal(err)
	}
	restartConfig := harness.config
	restartConfig.Repository = restartedRepository
	restarted, err := NewService(restartConfig)
	if err != nil {
		t.Fatal(err)
	}
	unlocked, err = restarted.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false)))
	if err != nil || unlocked.Msg.Workspace.Id != created.Msg.Workspace.Id {
		t.Fatalf("restart unlock: %v", err)
	}
}

func TestWorkspaceCreateResumesAfterCrashCheckpoints(t *testing.T) {
	testCases := []struct {
		checkpoint string
		setupID    string
	}{
		{checkpoint: "create.after_reservation", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073971"},
		{checkpoint: "create.after_database", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073972"},
		{checkpoint: "create.after_database_commit", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073973"},
		{checkpoint: "create.after_header", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073974"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.checkpoint, func(t *testing.T) {
			cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
			catalogueKey := bytes.Repeat([]byte{5}, 32)
			repository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			harness := newWorkspaceHarnessWithRepository(t, repository)
			harness.failures.failures[testCase.checkpoint] = errors.New("simulated process crash")
			request := &tammyv1.CreateWorkspaceRequest{
				SetupId: testCase.setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
				WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
				AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
				AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
			}
			if _, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request)); err == nil {
				t.Fatal("create unexpectedly crossed crash checkpoint")
			}
			reserved, err := repository.BySetup(context.Background(), request.SetupId)
			if err != nil {
				t.Fatalf("durable reservation missing: %v", err)
			}
			restartedRepository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			restartConfig := harness.config
			restartConfig.Repository = restartedRepository
			restarted, err := NewService(restartConfig)
			if err != nil {
				t.Fatal(err)
			}
			created, err := restarted.CreateWorkspace(context.Background(), connect.NewRequest(request))
			if err != nil {
				t.Fatalf("resumed create: %v", err)
			}
			if created.Msg.Workspace.Id != reserved.ID || len(created.Msg.RecoverySecret.Utf8) == 0 {
				t.Fatalf("resume changed reservation: workspace=%q reserved=%q", created.Msg.Workspace.Id, reserved.ID)
			}
			if *harness.bootstrapCalls != 1 || harness.audit.counts["CREATE"] != 1 {
				t.Fatalf("create side effects: bootstrap=%d audit=%d", *harness.bootstrapCalls, harness.audit.counts["CREATE"])
			}
		})
	}
}

func TestWorkspaceServicePendingSetupExpiryAndAttemptLimit(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		expire bool
	}{
		{name: "fifteen minute expiry", expire: true},
		{name: "five wrong confirmations"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			ctx := context.Background()
			created, err := harness.service.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
				SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073934", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
				WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
				AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
				AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
			}))
			if err != nil {
				t.Fatal(err)
			}
			if testCase.expire {
				*harness.now = harness.now.Add(15 * time.Minute)
				groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
				if err != nil {
					t.Fatal(err)
				}
				_, err = harness.service.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
					SetupId:       "01890f3c-7b2e-7cc4-98c4-dc0c0c073934",
					Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
				}))
				if !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
					t.Fatalf("expired confirmation returned %v", err)
				}
			} else {
				for attempt := 0; attempt < 5; attempt++ {
					_, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
						SetupId:       "01890f3c-7b2e-7cc4-98c4-dc0c0c073934",
						Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: []byte("WRNG")}, {GroupIndex: 1, Value: []byte("WRNG")}},
					}))
					if !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
						t.Fatalf("attempt %d returned %v", attempt+1, err)
					}
				}
			}
			tombstone, err := harness.repository.BySetup(ctx, "01890f3c-7b2e-7cc4-98c4-dc0c0c073934")
			if err != nil {
				t.Fatalf("expiry tombstone missing: %v", err)
			}
			if tombstone.SetupPhase != "expired" || len(tombstone.RecoveryDisplayEncrypted) != 0 ||
				len(tombstone.RecoveryGroupHashes) != 0 || len(tombstone.SetupMaterialEncrypted) != 0 ||
				tombstone.DatabasePath != "" || tombstone.HeaderPath != "" {
				t.Fatalf("invalid expiry tombstone: phase=%q display=%d hashes=%d setup=%d database_path=%t header_path=%t",
					tombstone.SetupPhase, len(tombstone.RecoveryDisplayEncrypted), len(tombstone.RecoveryGroupHashes),
					len(tombstone.SetupMaterialEncrypted), tombstone.DatabasePath != "", tombstone.HeaderPath != "")
			}
			if _, err := os.Stat(harness.databasePath); !os.IsNotExist(err) {
				t.Fatalf("pending database survived: %v", err)
			}
		})
	}
}

func TestWorkspaceExpiredSetupRetainsThirtyDaySecretFreeTombstone(t *testing.T) {
	t.Run("retention lifecycle", func(t *testing.T) {
		cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
		catalogueKey := bytes.Repeat([]byte{5}, 32)
		repository, err := NewFileRepository(cataloguePath, catalogueKey)
		if err != nil {
			t.Fatal(err)
		}
		harness := newWorkspaceHarnessWithRepository(t, repository)
		request := &tammyv1.CreateWorkspaceRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073982", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
			WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
			AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
			AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
		}
		created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request))
		if err != nil {
			t.Fatal(err)
		}
		pending, err := repository.BySetup(context.Background(), request.SetupId)
		if err != nil {
			t.Fatal(err)
		}
		*harness.now = harness.now.Add(15*time.Minute + time.Second)
		response, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request))
		if !errors.Is(err, faults.New(faults.CodeValidation, nil)) || response != nil {
			t.Fatalf("expired exact replay returned response_present=%t err=%v", response != nil, err)
		}
		tombstone, err := repository.BySetup(context.Background(), request.SetupId)
		if err != nil {
			t.Fatal(err)
		}
		if tombstone.SetupPhase != "expired" || tombstone.SetupSemanticHash == "" || tombstone.SetupExpires == 0 ||
			len(tombstone.RecoveryDisplayEncrypted) != 0 || len(tombstone.RecoveryGroupHashes) != 0 || len(tombstone.SetupMaterialEncrypted) != 0 ||
			tombstone.DatabasePath != "" || tombstone.HeaderPath != "" {
			t.Fatalf("invalid expiry tombstone: phase=%q semantic=%t expiry=%d display=%d hashes=%d setup=%d database_path=%t header_path=%t",
				tombstone.SetupPhase, tombstone.SetupSemanticHash != "", tombstone.SetupExpires,
				len(tombstone.RecoveryDisplayEncrypted), len(tombstone.RecoveryGroupHashes), len(tombstone.SetupMaterialEncrypted),
				tombstone.DatabasePath != "", tombstone.HeaderPath != "")
		}
		if _, active := harness.service.active[created.Msg.Workspace.Id]; active {
			t.Fatal("expired setup retained active DEK runtime")
		}
		if _, err := os.Lstat(pending.DatabasePath); !os.IsNotExist(err) {
			t.Fatalf("expired database survived: %v", err)
		}
		if _, err := os.Lstat(pending.HeaderPath); !os.IsNotExist(err) {
			t.Fatalf("expired header survived: %v", err)
		}
		changed := proto.Clone(request).(*tammyv1.CreateWorkspaceRequest)
		changed.AdministratorDisplayName = "Changed Admin"
		if _, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(changed)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
			t.Fatalf("changed retained tombstone replay returned %v", err)
		}
		*harness.now = harness.now.Add(30*24*time.Hour + time.Second)
		restarted, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request))
		if err != nil {
			t.Fatalf("setup did not restart after tombstone retention: %v", err)
		}
		if restarted.Msg.Workspace.Id == created.Msg.Workspace.Id || len(restarted.Msg.RecoverySecret.Utf8) == 0 {
			t.Fatalf("post-retention setup did not create fresh material: old=%q new=%q", created.Msg.Workspace.Id, restarted.Msg.Workspace.Id)
		}
	})

	t.Run("deletion failure is retryable", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		request := &tammyv1.CreateWorkspaceRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073983", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
			WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
			AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
			AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
		}
		created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request))
		if err != nil {
			t.Fatal(err)
		}
		pending, err := harness.repository.BySetup(context.Background(), request.SetupId)
		if err != nil {
			t.Fatal(err)
		}
		harness.service.closeRuntime(created.Msg.Workspace.Id)
		blocker := filepath.Join(pending.HeaderPath, "blocker")
		var injectionErr error
		harness.failures.hooks["expire_pending.after_tombstone"] = func() {
			if err := os.Remove(pending.HeaderPath); err != nil {
				injectionErr = err
				return
			}
			if err := os.Mkdir(pending.HeaderPath, 0o700); err != nil {
				injectionErr = err
				return
			}
			injectionErr = os.WriteFile(blocker, []byte("block deletion"), 0o600)
		}
		*harness.now = harness.now.Add(15*time.Minute + time.Second)
		if response, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request)); err == nil || response != nil {
			t.Fatalf("deletion failure returned response_present=%t err=%v", response != nil, err)
		}
		if injectionErr != nil {
			t.Fatal(injectionErr)
		}
		tombstone, err := harness.repository.BySetup(context.Background(), request.SetupId)
		if err != nil {
			t.Fatal(err)
		}
		if tombstone.SetupPhase != "expiry_cleanup" || len(tombstone.RecoveryDisplayEncrypted) != 0 || len(tombstone.SetupMaterialEncrypted) != 0 ||
			tombstone.SetupCleanupDatabasePath != pending.DatabasePath || tombstone.SetupCleanupHeaderPath != pending.HeaderPath {
			t.Fatalf("retryable cleanup tombstone invalid: phase=%q display=%d setup=%d database_path_match=%t header_path_match=%t",
				tombstone.SetupPhase, len(tombstone.RecoveryDisplayEncrypted), len(tombstone.SetupMaterialEncrypted),
				tombstone.SetupCleanupDatabasePath == pending.DatabasePath, tombstone.SetupCleanupHeaderPath == pending.HeaderPath)
		}
		sidecars := []string{pending.DatabasePath + "-journal", pending.DatabasePath + "-shm", pending.DatabasePath + "-wal"}
		for _, path := range sidecars {
			if err := os.WriteFile(path, []byte("expired setup sidecar"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Remove(blocker); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(pending.HeaderPath); err != nil {
			t.Fatal(err)
		}
		if response, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeValidation, nil)) || response != nil {
			t.Fatalf("cleanup retry returned response_present=%t err=%v", response != nil, err)
		}
		tombstone, err = harness.repository.BySetup(context.Background(), request.SetupId)
		if err != nil {
			t.Fatal(err)
		}
		if tombstone.SetupPhase != "expired" {
			t.Fatalf("cleanup retry phase = %q", tombstone.SetupPhase)
		}
		if _, err := os.Lstat(pending.DatabasePath); !os.IsNotExist(err) {
			t.Fatalf("database survived cleanup retry: %v", err)
		}
		for _, path := range sidecars {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("database sidecar survived cleanup retry: %v", err)
			}
		}
	})
}

func unlockRequest(passphrase string, remember bool) *tammyv1.UnlockWorkspaceRequest {
	return &tammyv1.UnlockWorkspaceRequest{WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		Proof:             &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{Passphrase: &tammyv1.SecretInput{Utf8: []byte(passphrase)}}},
		RememberWorkspace: &remember}
}

func TestWorkspaceServiceRememberForgetAndExpiry(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := context.Background()
	workspace, _ := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	unlocked, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", true)))
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.Msg.Workspace.RememberedUntil == nil || !unlocked.Msg.Workspace.RememberedUntil.AsTime().Equal(harness.now.Add(23*time.Hour+59*time.Minute)) {
		t.Fatalf("remembered expiry = %v", unlocked.Msg.Workspace.RememberedUntil)
	}
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	rememberedRequest := &tammyv1.UnlockWorkspaceRequest{WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_UseRememberedWorkspace{UseRememberedWorkspace: true}}}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(rememberedRequest)); err != nil {
		t.Fatal(err)
	}
	markWorkspaceAuthenticated(t, harness, workspace.Id)
	if _, err := harness.service.ForgetRememberedWorkspace(ctx, connect.NewRequest(&tammyv1.ForgetRememberedWorkspaceRequest{
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073912"}, WorkspaceId: workspace.Id,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(rememberedRequest)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("forgotten remembered key returned %v", err)
	}
}

func TestUnlockRememberedKeyIsCleanedWhenAuthoritativeCommitFails(t *testing.T) {
	harness := newWorkspaceHarness(t)
	store := newTrackingSecretStore()
	manager, err := NewRememberedKeyManager(store, harness.config.Clock)
	if err != nil {
		t.Fatal(err)
	}
	harness.service.remembered = manager
	workspace, _ := createConfirmedWorkspace(t, harness)
	dek := append([]byte(nil), harness.service.active[workspace.Id].dek...)
	defer Zero(dek)
	if _, err := harness.service.LockWorkspace(context.Background(), connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("unlock workspace audit unavailable")
	harness.audit.fail = injected
	if _, err := harness.service.UnlockWorkspace(context.Background(), connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", true))); !errors.Is(err, injected) {
		t.Fatalf("unlock commit failure returned %v", err)
	}
	if store.deleteCalls != 1 || store.Count() != 0 || len(harness.service.active) != 0 {
		t.Fatalf("failed unlock orphaned key deletes=%d items=%d runtimes=%d", store.deleteCalls, store.Count(), len(harness.service.active))
	}
	catalogue, err := harness.repository.ByID(context.Background(), workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := harness.config.Storage.Open(context.Background(), catalogue.DatabasePath, dek)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	authoritative, err := storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED || authoritative.RememberedUntil != 0 ||
		catalogue.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED || catalogue.RememberedUntil != 0 {
		t.Fatalf("failed unlock mutated authoritative=%+v catalogue=%+v", authoritative, catalogue)
	}
}

func TestUnlockRememberedCleanupFailureIsSurfacedAndExplicitForgetRemovesOrphan(t *testing.T) {
	harness := newWorkspaceHarness(t)
	store := newTrackingSecretStore()
	manager, err := NewRememberedKeyManager(store, harness.config.Clock)
	if err != nil {
		t.Fatal(err)
	}
	harness.service.remembered = manager
	workspace, _ := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(context.Background(), connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	commitFailure := errors.New("unlock database commit unavailable")
	cleanupFailure := errors.New("vault cleanup unavailable")
	harness.audit.fail = commitFailure
	store.deleteErr = cleanupFailure
	if _, err := harness.service.UnlockWorkspace(context.Background(), connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", true))); !errors.Is(err, commitFailure) || !errors.Is(err, cleanupFailure) || !errors.Is(err, ErrRememberedKeyUnavailable) {
		t.Fatalf("unlock cleanup failure returned %v", err)
	}
	if store.deleteCalls != 1 || store.Count() != 1 || len(harness.service.active) != 0 {
		t.Fatalf("cleanup failure state deletes=%d items=%d runtimes=%d", store.deleteCalls, store.Count(), len(harness.service.active))
	}
	harness.audit.fail = nil
	if _, err := harness.service.UnlockWorkspace(context.Background(), connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false))); err != nil {
		t.Fatal(err)
	}
	markWorkspaceAuthenticated(t, harness, workspace.Id)
	readsBefore, mutationsBefore := *harness.activeSessionReads, *harness.activeSessionMutations
	if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(forgetRememberedRequest(workspace.Id))); err != nil {
		t.Fatal(err)
	}
	if store.deleteCalls != 2 || store.Count() != 0 || harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 0 ||
		*harness.activeSessionReads != readsBefore+1 || *harness.activeSessionMutations != mutationsBefore {
		t.Fatalf("explicit zero-metadata forget deletes=%d items=%d audit=%d reads=%d→%d mutations=%d→%d", store.deleteCalls,
			store.Count(), harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"], readsBefore, *harness.activeSessionReads,
			mutationsBefore, *harness.activeSessionMutations)
	}
}

func TestUnlockPreservesRememberedKeyAfterAuthoritativeCommitAndConvergesCatalogue(t *testing.T) {
	repository := &failNextWorkspaceSaveRepository{Repository: NewMemoryRepository()}
	harness := newWorkspaceHarnessWithRepository(t, repository)
	store := newTrackingSecretStore()
	manager, err := NewRememberedKeyManager(store, harness.config.Clock)
	if err != nil {
		t.Fatal(err)
	}
	harness.service.remembered = manager
	workspace, _ := createConfirmedWorkspace(t, harness)
	dek := append([]byte(nil), harness.service.active[workspace.Id].dek...)
	defer Zero(dek)
	if _, err := harness.service.LockWorkspace(context.Background(), connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("catalogue unavailable after unlock commit")
	repository.failNext = injected
	if _, err := harness.service.UnlockWorkspace(context.Background(), connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", true))); !errors.Is(err, injected) {
		t.Fatalf("catalogue failure returned %v", err)
	}
	if store.deleteCalls != 0 || store.Count() != 1 || len(harness.service.active) != 0 {
		t.Fatalf("committed unlock cleanup deletes=%d items=%d runtimes=%d", store.deleteCalls, store.Count(), len(harness.service.active))
	}
	catalogue, err := repository.ByID(context.Background(), workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := harness.config.Storage.Open(context.Background(), catalogue.DatabasePath, dek)
	if err != nil {
		t.Fatal(err)
	}
	authoritative, err := storage.LoadWorkspaceRecord(context.Background())
	if closeErr := storage.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if authoritative.RememberedUntil == 0 || authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED ||
		catalogue.RememberedUntil != 0 || catalogue.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("commit boundary authoritative=%+v catalogue=%+v", authoritative, catalogue)
	}
	rememberedRequest := &tammyv1.UnlockWorkspaceRequest{WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_UseRememberedWorkspace{UseRememberedWorkspace: true}}}
	converged, err := harness.service.UnlockWorkspace(context.Background(), connect.NewRequest(rememberedRequest))
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err = repository.ByID(context.Background(), workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if converged.Msg.Workspace.RememberedUntil == nil || catalogue.RememberedUntil != authoritative.RememberedUntil ||
		store.deleteCalls != 0 || store.Count() != 1 {
		t.Fatalf("catalogue retry response=%v catalogue_until=%d authoritative_until=%d deletes=%d items=%d",
			converged.Msg.Workspace, catalogue.RememberedUntil, authoritative.RememberedUntil, store.deleteCalls, store.Count())
	}
}

func markWorkspaceAuthenticated(t *testing.T, harness workspaceHarness, workspaceID string) {
	t.Helper()
	runtime := harness.service.active[workspaceID]
	if runtime == nil || runtime.storage == nil {
		t.Fatal("workspace runtime missing")
	}
	record, err := runtime.storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED
	operationID, err := harness.config.IDs.New()
	if err != nil {
		t.Fatal(err)
	}
	mutation := WorkspaceMutation{OperationID: operationID, Kind: "TEST_MARK_AUTHENTICATED", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: "test-mark-authenticated"}
	if err := runtime.storage.CommitWorkspaceMutation(context.Background(), mutation, record, nil); err != nil {
		t.Fatal(err)
	}
	if err := harness.repository.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
}

func seedRememberedWorkspace(t *testing.T, harness workspaceHarness, store SecretStore) (*tammyv1.Workspace, *workspaceRuntime) {
	t.Helper()
	manager, err := NewRememberedKeyManager(store, harness.config.Clock)
	if err != nil {
		t.Fatal(err)
	}
	harness.service.remembered = manager
	harness.config.RememberedKeys = manager
	workspace, _ := createConfirmedWorkspace(t, harness)
	runtime := harness.service.active[workspace.Id]
	if runtime == nil {
		t.Fatal("confirmed workspace runtime missing")
	}
	consent := true
	until, err := manager.Remember(workspace.Id, runtime.dek, &consent)
	if err != nil {
		t.Fatal(err)
	}
	record, err := runtime.storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	record.RememberedUntil = until.Unix()
	mutation := WorkspaceMutation{OperationID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073bf1", Kind: "TEST_SEED_REMEMBERED",
		WorkspaceID: record.ID, Version: record.Version, SemanticHash: "test-seed-remembered"}
	if err := runtime.storage.CommitWorkspaceMutation(context.Background(), mutation, record, nil); err != nil {
		t.Fatal(err)
	}
	if err := harness.repository.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	markWorkspaceAuthenticated(t, harness, workspace.Id)
	return workspace, runtime
}

func forgetRememberedRequest(workspaceID string) *tammyv1.ForgetRememberedWorkspaceRequest {
	return &tammyv1.ForgetRememberedWorkspaceRequest{WorkspaceId: workspaceID, Authentication: &tammyv1.AuthenticationContext{
		ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073912",
	}}
}

func TestForgetRememberedWorkspaceRejectsBeforeVaultAccess(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		configure func(*testing.T, workspaceHarness, *trackingSecretStore, *tammyv1.Workspace, *workspaceRuntime, *tammyv1.ForgetRememberedWorkspaceRequest)
	}{
		{name: "different catalogue workspace", configure: func(t *testing.T, harness workspaceHarness, store *trackingSecretStore,
			_ *tammyv1.Workspace, runtime *workspaceRuntime, request *tammyv1.ForgetRememberedWorkspaceRequest) {
			foreign, err := runtime.storage.LoadWorkspaceRecord(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			foreign.ID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073bf2"
			foreign.SetupID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073bf3"
			foreign.DatabasePath += ".foreign"
			foreign.HeaderPath += ".foreign"
			if err := harness.repository.Save(context.Background(), foreign); err != nil {
				t.Fatal(err)
			}
			consent := true
			if _, err := harness.service.remembered.Remember(foreign.ID, runtime.dek, &consent); err != nil {
				t.Fatal(err)
			}
			request.WorkspaceId = foreign.ID
			if store.Count() != 2 {
				t.Fatalf("foreign remembered item setup count=%d", store.Count())
			}
		}},
		{name: "missing session", configure: func(_ *testing.T, _ workspaceHarness, _ *trackingSecretStore,
			_ *tammyv1.Workspace, _ *workspaceRuntime, request *tammyv1.ForgetRememberedWorkspaceRequest) {
			request.Authentication = nil
		}},
		{name: "wrong session", configure: func(_ *testing.T, harness workspaceHarness, _ *trackingSecretStore,
			_ *tammyv1.Workspace, _ *workspaceRuntime, _ *tammyv1.ForgetRememberedWorkspaceRequest) {
			harness.identity.activeSessionErr = faults.New(faults.CodeAuthenticationRequired, nil)
		}},
		{name: "locked workspace", configure: func(t *testing.T, harness workspaceHarness, _ *trackingSecretStore,
			_ *tammyv1.Workspace, _ *workspaceRuntime, _ *tammyv1.ForgetRememberedWorkspaceRequest) {
			if _, err := harness.service.LockWorkspace(context.Background(), connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			store := newTrackingSecretStore()
			workspace, runtime := seedRememberedWorkspace(t, harness, store)
			request := forgetRememberedRequest(workspace.Id)
			testCase.configure(t, harness, store, workspace, runtime, request)
			itemsBefore := store.Count()
			if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(request)); err == nil {
				t.Fatal("invalid active-workspace request succeeded")
			}
			if store.deleteCalls != 0 || store.Count() != itemsBefore {
				t.Fatalf("denial touched vault deletes=%d items=%d", store.deleteCalls, store.Count())
			}
		})
	}
}

func TestForgetRememberedWorkspaceFailureOrderingAndRetry(t *testing.T) {
	for _, testCase := range []struct {
		name                  string
		configure             func(workspaceHarness, *trackingSecretStore, error)
		wantFirstDeleteCalls  int
		wantFirstWorkspaceAud int
	}{
		{name: "vault delete", configure: func(_ workspaceHarness, store *trackingSecretStore, injected error) {
			store.deleteErr = injected
		}, wantFirstDeleteCalls: 1},
		{name: "sql mutation", configure: func(harness workspaceHarness, _ *trackingSecretStore, injected error) {
			harness.failures.failures["forget_remembered_workspace.before_db_commit"] = injected
		}, wantFirstDeleteCalls: 1},
		{name: "workspace audit", configure: func(harness workspaceHarness, _ *trackingSecretStore, injected error) {
			harness.audit.fail = injected
		}, wantFirstDeleteCalls: 1},
		{name: "catalogue convergence", configure: func(harness workspaceHarness, _ *trackingSecretStore, injected error) {
			harness.repository.(*failNextWorkspaceSaveRepository).failNext = injected
		}, wantFirstDeleteCalls: 1, wantFirstWorkspaceAud: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var repository Repository = NewMemoryRepository()
			if testCase.name == "catalogue convergence" {
				repository = &failNextWorkspaceSaveRepository{Repository: repository}
			}
			harness := newWorkspaceHarnessWithRepository(t, repository)
			store := newTrackingSecretStore()
			workspace, runtime := seedRememberedWorkspace(t, harness, store)
			dekBefore := append([]byte(nil), runtime.dek...)
			injected := errors.New("injected forget boundary failure")
			testCase.configure(harness, store, injected)

			if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(forgetRememberedRequest(workspace.Id))); !errors.Is(err, injected) {
				t.Fatalf("first attempt returned %v", err)
			}
			authoritative, err := runtime.storage.LoadWorkspaceRecord(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			catalogue, err := harness.repository.ByID(context.Background(), workspace.Id)
			if err != nil {
				t.Fatal(err)
			}
			committed := testCase.name == "catalogue convergence"
			if (authoritative.RememberedUntil == 0) != committed || catalogue.RememberedUntil == 0 ||
				harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != testCase.wantFirstWorkspaceAud ||
				store.deleteCalls != testCase.wantFirstDeleteCalls || harness.service.active[workspace.Id] != runtime ||
				!bytes.Equal(runtime.dek, dekBefore) {
				t.Fatalf("failure boundary committed=%t sql_until=%d catalogue_until=%d audit=%d deletes=%d runtime_same=%t dek_same=%t",
					committed, authoritative.RememberedUntil, catalogue.RememberedUntil,
					harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"], store.deleteCalls,
					harness.service.active[workspace.Id] == runtime, bytes.Equal(runtime.dek, dekBefore))
			}
			harness.audit.fail = nil
			response, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(forgetRememberedRequest(workspace.Id)))
			if err != nil {
				t.Fatal(err)
			}
			authoritative, err = runtime.storage.LoadWorkspaceRecord(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			catalogue, err = harness.repository.ByID(context.Background(), workspace.Id)
			if err != nil {
				t.Fatal(err)
			}
			wantDeleteCalls := 2
			if response.Msg.Workspace.RememberedUntil != nil || authoritative.RememberedUntil != 0 || catalogue.RememberedUntil != 0 ||
				store.Count() != 0 || store.deleteCalls != wantDeleteCalls ||
				harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 || harness.service.active[workspace.Id] != runtime ||
				!bytes.Equal(runtime.dek, dekBefore) {
				t.Fatalf("retry did not converge response=%v sql_until=%d catalogue_until=%d items=%d deletes=%d audit=%d",
					response.Msg.Workspace, authoritative.RememberedUntil, catalogue.RememberedUntil, store.Count(), store.deleteCalls,
					harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"])
			}

			readsBefore, mutationsBefore := *harness.activeSessionReads, *harness.activeSessionMutations
			if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(forgetRememberedRequest(workspace.Id))); err != nil {
				t.Fatal(err)
			}
			if store.deleteCalls != wantDeleteCalls+1 || harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 ||
				*harness.activeSessionReads != readsBefore+1 || *harness.activeSessionMutations != mutationsBefore {
				t.Fatalf("terminal replay was impure deletes=%d audit=%d reads=%d→%d mutations=%d→%d", store.deleteCalls,
					harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"], readsBefore, *harness.activeSessionReads,
					mutationsBefore, *harness.activeSessionMutations)
			}
		})
	}
}

func TestForgetRememberedWorkspaceTerminalReplayDeletesReinsertedVaultItemPurely(t *testing.T) {
	harness := newWorkspaceHarness(t)
	store := newTrackingSecretStore()
	workspace, runtime := seedRememberedWorkspace(t, harness, store)
	request := forgetRememberedRequest(workspace.Id)
	if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(request)); err != nil {
		t.Fatal(err)
	}
	authoritativeBefore, err := runtime.storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	readsBefore, mutationsBefore := *harness.activeSessionReads, *harness.activeSessionMutations
	consent := true
	if _, err := harness.service.remembered.Remember(workspace.Id, runtime.dek, &consent); err != nil {
		t.Fatal(err)
	}
	deleteFailure := errors.New("terminal replay vault unavailable")
	store.deleteErr = deleteFailure
	if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(request)); !errors.Is(err, deleteFailure) {
		t.Fatalf("terminal replay vault failure returned %v", err)
	}
	authoritativeAfterFailure, err := runtime.storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if store.deleteCalls != 2 || store.Count() != 1 || harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 ||
		*harness.activeSessionReads != readsBefore+1 || *harness.activeSessionMutations != mutationsBefore ||
		!reflect.DeepEqual(authoritativeBefore, authoritativeAfterFailure) {
		t.Fatalf("terminal vault failure deletes=%d items=%d audits=%d reads=%d→%d mutations=%d→%d sql_same=%t",
			store.deleteCalls, store.Count(), harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"], readsBefore,
			*harness.activeSessionReads, mutationsBefore, *harness.activeSessionMutations,
			reflect.DeepEqual(authoritativeBefore, authoritativeAfterFailure))
	}
	if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(request)); err != nil {
		t.Fatal(err)
	}
	authoritativeAfter, err := runtime.storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if store.deleteCalls != 3 || store.Count() != 0 || harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 ||
		*harness.activeSessionReads != readsBefore+2 || *harness.activeSessionMutations != mutationsBefore ||
		!reflect.DeepEqual(authoritativeBefore, authoritativeAfter) || harness.service.active[workspace.Id] != runtime {
		t.Fatalf("terminal replay deletes=%d items=%d audits=%d reads=%d→%d mutations=%d→%d sql_same=%t runtime_same=%t",
			store.deleteCalls, store.Count(), harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"], readsBefore,
			*harness.activeSessionReads, mutationsBefore, *harness.activeSessionMutations,
			reflect.DeepEqual(authoritativeBefore, authoritativeAfter), harness.service.active[workspace.Id] == runtime)
	}
}

func TestForgetRememberedWorkspaceCatalogueFailureSurvivesLockAndRestart(t *testing.T) {
	repository := &failNextWorkspaceSaveRepository{Repository: NewMemoryRepository()}
	harness := newWorkspaceHarnessWithRepository(t, repository)
	store := newTrackingSecretStore()
	workspace, _ := seedRememberedWorkspace(t, harness, store)
	harness.config.RememberedKeys = harness.service.remembered
	injected := errors.New("catalogue unavailable after remembered consent withdrawal")
	repository.failNext = injected
	request := forgetRememberedRequest(workspace.Id)
	if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(request)); !errors.Is(err, injected) {
		t.Fatalf("catalogue failure returned %v", err)
	}
	if _, err := harness.service.LockWorkspace(context.Background(), connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	locked, err := repository.ByID(context.Background(), workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if locked.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED || locked.RememberedUntil != 0 || store.Count() != 0 ||
		harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 {
		t.Fatalf("lock resurrected consent state=%v until=%d vault=%d audit=%d", locked.State, locked.RememberedUntil, store.Count(),
			harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"])
	}
	restarted, err := NewService(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	state, err := restarted.GetWorkspaceState(context.Background(), connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &workspace.Id}))
	if err != nil || state.Msg.Workspace.RememberedUntil != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("restart state=%v err=%v", state.Msg.Workspace, err)
	}
	rememberedRequest := &tammyv1.UnlockWorkspaceRequest{WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_UseRememberedWorkspace{UseRememberedWorkspace: true}}}
	if _, err := restarted.UnlockWorkspace(context.Background(), connect.NewRequest(rememberedRequest)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("restart remembered unlock returned %v", err)
	}
	if harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 || store.Count() != 0 {
		t.Fatalf("restart duplicated audit or key audit=%d vault=%d", harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"], store.Count())
	}
}

func TestForgetRememberedWorkspaceCatalogueFailureConvergesAfterProcessRestart(t *testing.T) {
	repository := &failNextWorkspaceSaveRepository{Repository: NewMemoryRepository()}
	harness := newWorkspaceHarnessWithRepository(t, repository)
	store := newTrackingSecretStore()
	workspace, _ := seedRememberedWorkspace(t, harness, store)
	harness.config.RememberedKeys = harness.service.remembered
	injected := errors.New("catalogue unavailable before process death")
	repository.failNext = injected
	if _, err := harness.service.ForgetRememberedWorkspace(context.Background(), connect.NewRequest(forgetRememberedRequest(workspace.Id))); !errors.Is(err, injected) {
		t.Fatalf("catalogue failure returned %v", err)
	}
	harness.service.closeRuntime(workspace.Id)
	restarted, err := NewService(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	unlocked, err := restarted.UnlockWorkspace(context.Background(), connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false)))
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := repository.ByID(context.Background(), workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	runtime := restarted.active[workspace.Id]
	if runtime == nil || runtime.storage == nil {
		t.Fatal("restart did not admit proved runtime")
	}
	authoritative, err := runtime.storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.Msg.Workspace.RememberedUntil != nil || catalogue.RememberedUntil != 0 || authoritative.RememberedUntil != 0 ||
		store.Count() != 0 || harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 {
		t.Fatalf("restart convergence response=%v catalogue_until=%d sql_until=%d vault=%d audit=%d", unlocked.Msg.Workspace,
			catalogue.RememberedUntil, authoritative.RememberedUntil, store.Count(), harness.audit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"])
	}
}

func TestUnlockRejectsRepeatWithoutDisplacingActiveRuntime(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := context.Background()
	workspace, _ := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	request := unlockRequest("workspace-passphrase-long-enough", false)
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.UnlockWorkspaceRequest))); err != nil {
		t.Fatal(err)
	}
	original := harness.service.active[workspace.Id]
	if original == nil || original.storage == nil || original.header == nil || len(original.dek) != DEKSize {
		t.Fatal("first unlock did not retain one complete runtime")
	}
	if response, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.UnlockWorkspaceRequest))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) || response != nil {
		t.Fatalf("repeat unlock returned response_present=%t err=%v", response != nil, err)
	}
	if len(harness.service.active) != 1 || harness.service.active[workspace.Id] != original || len(original.dek) != DEKSize {
		t.Fatal("repeat unlock displaced or destroyed the active runtime")
	}
}

func TestConcurrentUnlockHasExactlyOneWinner(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := context.Background()
	workspace, _ := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false)))
			results <- err
		}()
	}
	close(start)
	successes := 0
	authenticationFailures := 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)):
			authenticationFailures++
		default:
			t.Fatalf("concurrent unlock returned %v", err)
		}
	}
	if successes != 1 || authenticationFailures != 1 || len(harness.service.active) != 1 || harness.service.active[workspace.Id] == nil {
		t.Fatalf("unlock winners=%d auth_failures=%d active=%d", successes, authenticationFailures, len(harness.service.active))
	}
}

func TestChangePassphraseFailsClosedWhenRememberedDEKCannotBeDeleted(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := context.Background()
	workspace, _ := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", true))); err != nil {
		t.Fatal(err)
	}
	originalStore := harness.service.remembered.store
	harness.service.remembered.store = deleteFailingSecretStore{SecretStore: originalStore}
	request := &tammyv1.ChangePassphraseRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073937", Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073912",
		}, FreshFactor: harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073938", "change_passphrase", *harness.now),
	}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
		CurrentPassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		NewPassphrase:     &tammyv1.SecretInput{Utf8: []byte("replacement-passphrase-long-enough")}}
	if _, err := harness.service.ChangePassphrase(ctx, connect.NewRequest(request)); !errors.Is(err, ErrRememberedKeyUnavailable) {
		t.Fatalf("vault deletion failure returned %v", err)
	}
	record, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != workspace.Version || record.RememberedUntil == 0 {
		t.Fatalf("workspace mutated despite stale remembered key: %+v", record)
	}
}

func TestWorkspaceServiceUnauthenticatedOpenExpiresAfterFiveMinutes(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := context.Background()
	workspace, _ := createConfirmedWorkspace(t, harness)
	*harness.now = harness.now.Add(5 * time.Minute)
	if err := harness.service.ExpireUnauthenticated(ctx); err != nil {
		t.Fatal(err)
	}
	record, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("state = %v, want locked", record.State)
	}
	if _, active := harness.service.active[workspace.Id]; active {
		t.Fatal("unauthenticated runtime retained its DEK")
	}
}

func createConfirmedWorkspace(t *testing.T, harness workspaceHarness) (*tammyv1.Workspace, []byte) {
	return createConfirmedWorkspaceAt(t, harness, "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", "destination-capability")
}

func createConfirmedWorkspaceAt(t *testing.T, harness workspaceHarness, setupID, destinationCapability string) (*tammyv1.Workspace, []byte) {
	t.Helper()
	created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId: setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: destinationCapability},
		WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")}, AdministratorUsername: "admin@example.test",
		AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: setupID, Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return confirmed.Msg.Workspace, append([]byte(nil), created.Msg.RecoverySecret.Utf8...)
}

func prepareTwoLockedWorkspaces(t *testing.T, harness workspaceHarness) (*tammyv1.Workspace, []byte, *tammyv1.Workspace, []byte) {
	t.Helper()
	ctx := context.Background()
	first, firstRecovery := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	secondDirectory := t.TempDir()
	capabilities := harness.service.capabilities.(testCapabilities)
	capabilities["second-destination-capability"] = secondDirectory
	capabilities["second-workspace-file-capability"] = filepath.Join(secondDirectory, "tammy-workspace.db")
	second, secondRecovery := createConfirmedWorkspaceAt(t, harness,
		"01890f3c-7b2e-7cc4-98c4-dc0c0c073921", "second-destination-capability")
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	return first, firstRecovery, second, secondRecovery
}

func unlockRequestFor(capability, passphrase string) *tammyv1.UnlockWorkspaceRequest {
	remember := false
	return &tammyv1.UnlockWorkspaceRequest{WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: capability},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte(passphrase)}}}, RememberWorkspace: &remember}
}

func confirmationRequestFor(t *testing.T, setupID string, recovery []byte) *tammyv1.ConfirmRecoveryRequest {
	t.Helper()
	groups, err := ParseRecoveryGroups(recovery)
	if err != nil {
		t.Fatal(err)
	}
	return &tammyv1.ConfirmRecoveryRequest{SetupId: setupID, Confirmations: []*tammyv1.RecoveryGroupConfirmation{
		{GroupIndex: 0, Value: append([]byte(nil), groups[0]...)},
		{GroupIndex: 1, Value: append([]byte(nil), groups[1]...)},
	}}
}

func terminalPassphraseProof(passphrase string) *tammyv1.WorkspaceUnlockProof {
	return &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
		Passphrase: &tammyv1.SecretInput{Utf8: []byte(passphrase)},
	}}
}

func terminalRememberedProof() *tammyv1.WorkspaceUnlockProof {
	return &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_UseRememberedWorkspace{
		UseRememberedWorkspace: true,
	}}
}

type closeTrackingStorage struct{ closed bool }

func (*closeTrackingStorage) CommitWorkspaceMutation(context.Context, WorkspaceMutation, workspaceRecord, func(MutationExecutor, *workspaceRecord) error) error {
	return nil
}
func (*closeTrackingStorage) LoadWorkspaceRecord(context.Context) (workspaceRecord, error) {
	return workspaceRecord{}, ErrWorkspaceNotFound
}
func (*closeTrackingStorage) HeaderOperationCommitted(context.Context, string, uint64) bool {
	return false
}
func (*closeTrackingStorage) Database() *sqlcipher.Database { return nil }
func (storage *closeTrackingStorage) Close() error {
	storage.closed = true
	return nil
}

type replayTrackingStorageFactory struct {
	inner          StorageFactory
	mu             sync.Mutex
	handles        []*replayTrackingStorageHandle
	observedActive func() int
	maxActive      int
}

type replayTrackingStorageHandle struct {
	StorageHandle
	factory *replayTrackingStorageFactory
	key     []byte
	once    sync.Once
}

func (factory *replayTrackingStorageFactory) Create(ctx context.Context, path string, key []byte) (StorageHandle, error) {
	handle, err := factory.inner.Create(ctx, path, key)
	if err != nil {
		return nil, err
	}
	return factory.track(handle, key), nil
}

func (factory *replayTrackingStorageFactory) Open(ctx context.Context, path string, key []byte) (StorageHandle, error) {
	handle, err := factory.inner.Open(ctx, path, key)
	if err != nil {
		return nil, err
	}
	return factory.track(handle, key), nil
}

func (factory *replayTrackingStorageFactory) track(handle StorageHandle, key []byte) StorageHandle {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.observedActive != nil {
		if active := factory.observedActive(); active > factory.maxActive {
			factory.maxActive = active
		}
	}
	tracked := &replayTrackingStorageHandle{StorageHandle: handle, factory: factory, key: key}
	factory.handles = append(factory.handles, tracked)
	return tracked
}

func (handle *replayTrackingStorageHandle) Close() error {
	var err error
	handle.once.Do(func() {
		err = handle.StorageHandle.Close()
	})
	return err
}

func (factory *replayTrackingStorageFactory) reset() {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	factory.handles = nil
	factory.maxActive = 0
}

func (factory *replayTrackingStorageFactory) assertClosedAndZeroed(t *testing.T, want int) {
	t.Helper()
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.handles) != want || factory.maxActive > 1 {
		t.Fatalf("temporary handles=%d want=%d max_active=%d", len(factory.handles), want, factory.maxActive)
	}
	for index, handle := range factory.handles {
		closed := false
		handle.once.Do(func() { closed = true })
		if closed {
			t.Fatalf("temporary handle %d was not closed", index)
		}
		if !bytes.Equal(handle.key, make([]byte, len(handle.key))) {
			t.Fatalf("temporary DEK %d was not zeroed", index)
		}
	}
}

func attachReplayTrackingStorage(harness workspaceHarness) *replayTrackingStorageFactory {
	tracker := &replayTrackingStorageFactory{inner: harness.service.storage}
	tracker.observedActive = func() int { return len(harness.service.active) }
	harness.service.storage = tracker
	return tracker
}

func TestTerminalConfirmationReplayDoesNotDisturbAnotherActiveWorkspace(t *testing.T) {
	catalogue := NewMemoryRepository()
	repository := &failNextWorkspaceSaveRepository{Repository: catalogue}
	harness := newWorkspaceHarnessWithRepository(t, repository)
	tracker := attachReplayTrackingStorage(harness)
	ctx := context.Background()
	first, _ := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	secondDirectory := t.TempDir()
	capabilities := harness.service.capabilities.(testCapabilities)
	capabilities["second-destination-capability"] = secondDirectory
	capabilities["second-workspace-file-capability"] = filepath.Join(secondDirectory, "tammy-workspace.db")
	created, err := harness.service.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "second-destination-capability"},
		WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("second-workspace-passphrase-long-enough")}, AdministratorUsername: "admin@example.test",
		AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	exact := &tammyv1.ConfirmRecoveryRequest{SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", Confirmations: []*tammyv1.RecoveryGroupConfirmation{
		{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]},
	}}
	pendingAuditBefore := harness.audit.counts["RECOVERY_CONFIRMATION"]
	repository.failNext = errors.New("catalogue unavailable after confirmation commit")
	initial := proto.Clone(exact).(*tammyv1.ConfirmRecoveryRequest)
	initial.TerminalReplayProof = terminalRememberedProof()
	if _, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(initial)); err == nil {
		t.Fatal("confirmation did not stop at catalogue interruption")
	}
	confirmationAudit := harness.audit.counts["RECOVERY_CONFIRMATION"]
	if confirmationAudit != pendingAuditBefore+1 {
		t.Fatalf("pending confirmation consumed terminal proof: audit_before=%d audit_after=%d", pendingAuditBefore, confirmationAudit)
	}
	harness.service.closeRuntime(created.Msg.Workspace.Id)
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequestFor(
		"workspace-file-capability", "workspace-passphrase-long-enough"))); err != nil {
		t.Fatal(err)
	}
	active := harness.service.active[first.Id]
	before, err := catalogue.ByID(ctx, created.Msg.Workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	attemptsBefore, err := harness.service.attempts.Status("workspace_recovery_confirmation", created.Msg.Workspace.Id, policy)
	if err != nil {
		t.Fatal(err)
	}
	tracker.reset()
	wrong := proto.Clone(exact).(*tammyv1.ConfirmRecoveryRequest)
	wrong.Confirmations[0].Value = []byte("NOPE")
	if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(wrong)); response != nil ||
		!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("wrong terminal confirmation response_present=%t err=%v", response != nil, err)
	}
	tracker.assertClosedAndZeroed(t, 1)
	afterWrong, err := catalogue.ByID(ctx, created.Msg.Workspace.Id)
	if err != nil || !reflect.DeepEqual(before, afterWrong) {
		t.Fatalf("wrong terminal proof changed catalogue err=%v", err)
	}
	attemptsAfter, err := harness.service.attempts.Status("workspace_recovery_confirmation", created.Msg.Workspace.Id, policy)
	if err != nil || attemptsAfter != attemptsBefore {
		t.Fatalf("wrong terminal proof changed attempt journal before=%+v after=%+v err=%v", attemptsBefore, attemptsAfter, err)
	}
	tracker.reset()
	response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(exact).(*tammyv1.ConfirmRecoveryRequest)))
	if err != nil || response.Msg.Workspace.Id != created.Msg.Workspace.Id {
		t.Fatalf("exact terminal confirmation response=%v err=%v", response, err)
	}
	tracker.assertClosedAndZeroed(t, 1)
	if len(harness.service.active) != 1 || harness.service.active[first.Id] != active || harness.audit.counts["RECOVERY_CONFIRMATION"] != confirmationAudit {
		t.Fatalf("confirmation replay disturbed active workspace active=%d audit=%d", len(harness.service.active), harness.audit.counts["RECOVERY_CONFIRMATION"])
	}
	terminalBefore, err := catalogue.ByID(ctx, created.Msg.Workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(terminalBefore.RecoveryDisplayEncrypted) != 0 || len(terminalBefore.RecoveryGroupHashes) != 0 ||
		len(terminalBefore.SetupMaterialEncrypted) != 0 {
		t.Fatal("terminal catalogue retained scrubbed recovery or setup material")
	}

	tracker.reset()
	if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(exact).(*tammyv1.ConfirmRecoveryRequest))); response != nil ||
		!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("missing inactive terminal proof response_present=%t err=%v", response != nil, err)
	}
	tracker.assertClosedAndZeroed(t, 0)

	wrongPassphrase := proto.Clone(exact).(*tammyv1.ConfirmRecoveryRequest)
	wrongPassphrase.TerminalReplayProof = terminalPassphraseProof("wrong-terminal-passphrase-long-enough")
	if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(wrongPassphrase)); response != nil ||
		!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("wrong inactive terminal proof response_present=%t err=%v", response != nil, err)
	}
	tracker.assertClosedAndZeroed(t, 0)

	valid := proto.Clone(exact).(*tammyv1.ConfirmRecoveryRequest)
	valid.TerminalReplayProof = terminalPassphraseProof("second-workspace-passphrase-long-enough")
	response, err = harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(valid).(*tammyv1.ConfirmRecoveryRequest)))
	if err != nil || response.Msg.Workspace.Id != created.Msg.Workspace.Id {
		t.Fatalf("proved inactive terminal confirmation response=%v err=%v", response, err)
	}
	tracker.assertClosedAndZeroed(t, 1)
	terminalAfter, err := catalogue.ByID(ctx, created.Msg.Workspace.Id)
	if err != nil || !reflect.DeepEqual(terminalBefore, terminalAfter) {
		t.Fatalf("proved terminal replay changed catalogue err=%v", err)
	}
	for _, path := range []string{before.DatabasePath, before.HeaderPath} {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(contents, []byte("second-workspace-passphrase-long-enough")) {
			t.Fatalf("terminal replay proof persisted in %s", path)
		}
	}

	tracker.reset()
	errorsFound := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(valid).(*tammyv1.ConfirmRecoveryRequest)))
			errorsFound <- err
		}()
	}
	if left, right := <-errorsFound, <-errorsFound; left != nil || right != nil {
		t.Fatalf("concurrent terminal confirmations=%v/%v", left, right)
	}
	tracker.assertClosedAndZeroed(t, 2)
	if len(harness.service.active) != 1 || harness.service.active[first.Id] != active || harness.audit.counts["RECOVERY_CONFIRMATION"] != confirmationAudit {
		t.Fatalf("concurrent confirmation replay disturbed active workspace active=%d audit=%d", len(harness.service.active), harness.audit.counts["RECOVERY_CONFIRMATION"])
	}
	if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{SetupId: exact.SetupId})); response != nil ||
		!errors.Is(err, faults.New(faults.CodeValidation, nil)) {
		t.Fatalf("nil terminal confirmation proof response_present=%t err=%v", response != nil, err)
	}
}

func TestTerminalConfirmationReplayUsesActiveRuntimeWithoutProof(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, recovery := createConfirmedWorkspace(t, harness)
	tracker := attachReplayTrackingStorage(harness)
	request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", recovery)
	auditBefore := harness.audit.counts["RECOVERY_CONFIRMATION"]
	response, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(request))
	if err != nil || response.Msg.Workspace.Id != workspace.Id {
		t.Fatalf("active terminal confirmation response=%v err=%v", response, err)
	}
	tracker.assertClosedAndZeroed(t, 0)
	if len(harness.service.active) != 1 || harness.service.active[workspace.Id] == nil || harness.audit.counts["RECOVERY_CONFIRMATION"] != auditBefore {
		t.Fatal("active terminal confirmation disturbed runtime or audit")
	}
}

func TestTerminalConfirmationReplayRejectsUnboundAuthoritativeRecord(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*testing.T, StorageHandle, workspaceRecord)
	}{
		{name: "missing", mutate: func(t *testing.T, storage StorageHandle, _ workspaceRecord) {
			t.Helper()
			if _, err := storage.Database().ExecContext(context.Background(),
				`DELETE FROM workspace_metadata WHERE key = ?`, authoritativeWorkspaceRecordKey); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "corrupt", mutate: func(t *testing.T, storage StorageHandle, _ workspaceRecord) {
			t.Helper()
			if _, err := storage.Database().ExecContext(context.Background(),
				`UPDATE workspace_metadata SET value = ?, revision = revision + 1 WHERE key = ?`, []byte("{"), authoritativeWorkspaceRecordKey); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mismatched workspace", mutate: func(t *testing.T, storage StorageHandle, record workspaceRecord) {
			t.Helper()
			record.ID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073999"
			payload, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := storage.Database().ExecContext(context.Background(),
				`UPDATE workspace_metadata SET value = ?, revision = revision + 1 WHERE key = ?`, payload, authoritativeWorkspaceRecordKey); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			ctx := context.Background()
			first, _ := createConfirmedWorkspace(t, harness)
			if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
				t.Fatal(err)
			}
			secondDirectory := t.TempDir()
			capabilities := harness.service.capabilities.(testCapabilities)
			capabilities["second-destination-capability"] = secondDirectory
			capabilities["second-workspace-file-capability"] = filepath.Join(secondDirectory, "tammy-workspace.db")
			second, recovery := createConfirmedWorkspaceAt(t, harness,
				"01890f3c-7b2e-7cc4-98c4-dc0c0c073921", "second-destination-capability")
			secondDEK := append([]byte(nil), harness.service.active[second.Id].dek...)
			if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
				t.Fatal(err)
			}
			catalogueBefore, err := harness.repository.ByID(ctx, second.Id)
			if err != nil {
				t.Fatal(err)
			}
			header, err := NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
			if err != nil {
				t.Fatal(err)
			}
			slotsBefore, err := header.Slots()
			header.Close()
			if err != nil {
				t.Fatal(err)
			}
			storage, err := harness.service.storage.Open(ctx, catalogueBefore.DatabasePath, secondDEK)
			if err != nil {
				t.Fatal(err)
			}
			authoritative, err := storage.LoadWorkspaceRecord(ctx)
			if err != nil {
				t.Fatal(err)
			}
			testCase.mutate(t, storage, authoritative)
			if err := storage.Close(); err != nil {
				t.Fatal(err)
			}
			Zero(secondDEK)

			tracker := attachReplayTrackingStorage(harness)
			if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequestFor(
				"workspace-file-capability", "workspace-passphrase-long-enough"))); err != nil {
				t.Fatal(err)
			}
			active := harness.service.active[first.Id]
			tracker.reset()
			auditBefore := harness.audit.counts["RECOVERY_CONFIRMATION"]
			policy := workspaceUnlockAttemptPolicy()
			attemptsBefore, err := harness.service.attempts.Status(workspaceUnlockAttemptScope, second.Id, policy)
			if err != nil {
				t.Fatal(err)
			}
			request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", recovery)
			request.TerminalReplayProof = terminalPassphraseProof("workspace-passphrase-long-enough")
			if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(request)); response != nil ||
				!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
				t.Fatalf("unbound terminal confirmation response_present=%t err=%v", response != nil, err)
			}
			tracker.assertClosedAndZeroed(t, 1)
			attemptsAfter, err := harness.service.attempts.Status(workspaceUnlockAttemptScope, second.Id, policy)
			if err != nil || attemptsAfter.AttemptCount != attemptsBefore.AttemptCount+1 {
				t.Fatalf("unbound terminal proof did not retain a shared failure before=%+v after=%+v err=%v", attemptsBefore, attemptsAfter, err)
			}
			catalogueAfter, err := harness.repository.ByID(ctx, second.Id)
			if err != nil || !reflect.DeepEqual(catalogueBefore, catalogueAfter) {
				t.Fatalf("unbound terminal confirmation changed catalogue err=%v", err)
			}
			header, err = NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
			if err != nil {
				t.Fatal(err)
			}
			slotsAfter, err := header.Slots()
			header.Close()
			if err != nil || !reflect.DeepEqual(slotsBefore, slotsAfter) {
				t.Fatalf("unbound terminal confirmation changed header err=%v", err)
			}
			if len(harness.service.active) != 1 || harness.service.active[first.Id] != active || harness.audit.counts["RECOVERY_CONFIRMATION"] != auditBefore {
				t.Fatal("unbound terminal confirmation disturbed runtime or audit")
			}
		})
	}
}

func TestTerminalConfirmationReplayRememberedProofLifecycle(t *testing.T) {
	prepare := func(t *testing.T, rememberSecond bool) (workspaceHarness, *tammyv1.Workspace, *tammyv1.Workspace, []byte, *replayTrackingStorageFactory, *workspaceRuntime) {
		t.Helper()
		harness := newWorkspaceHarness(t)
		ctx := context.Background()
		first, _, second, recovery := prepareTwoLockedWorkspaces(t, harness)
		if rememberSecond {
			request := unlockRequestFor("second-workspace-file-capability", "workspace-passphrase-long-enough")
			consent := true
			request.RememberWorkspace = &consent
			if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(request)); err != nil {
				t.Fatal(err)
			}
			if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
				t.Fatal(err)
			}
		}
		tracker := attachReplayTrackingStorage(harness)
		if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequestFor(
			"workspace-file-capability", "workspace-passphrase-long-enough"))); err != nil {
			t.Fatal(err)
		}
		active := harness.service.active[first.Id]
		tracker.reset()
		return harness, first, second, recovery, tracker, active
	}

	t.Run("existing unexpired consent succeeds without extension", func(t *testing.T) {
		harness, first, second, recovery, tracker, active := prepare(t, true)
		ctx := context.Background()
		keyBefore, expiryBefore, err := harness.service.remembered.Use(second.Id)
		if err != nil {
			t.Fatal(err)
		}
		Zero(keyBefore)
		catalogueBefore, err := harness.repository.ByID(ctx, second.Id)
		if err != nil {
			t.Fatal(err)
		}
		policy := workspaceUnlockAttemptPolicy()
		attemptsBefore, err := harness.service.attempts.Status(workspaceUnlockAttemptScope, second.Id, policy)
		if err != nil {
			t.Fatal(err)
		}
		request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", recovery)
		request.TerminalReplayProof = terminalRememberedProof()
		response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(request))
		if err != nil || response.Msg.Workspace.Id != second.Id {
			t.Fatalf("remembered terminal confirmation response=%v err=%v", response, err)
		}
		tracker.assertClosedAndZeroed(t, 1)
		keyAfter, expiryAfter, err := harness.service.remembered.Use(second.Id)
		if err != nil {
			t.Fatal(err)
		}
		Zero(keyAfter)
		catalogueAfter, err := harness.repository.ByID(ctx, second.Id)
		attemptsAfter, attemptsErr := harness.service.attempts.Status(workspaceUnlockAttemptScope, second.Id, policy)
		if err != nil || attemptsErr != nil || attemptsAfter != attemptsBefore || !reflect.DeepEqual(catalogueBefore, catalogueAfter) || !expiryAfter.Equal(expiryBefore) {
			t.Fatalf("remembered terminal confirmation extended or persisted consent expiry_before=%v expiry_after=%v err=%v", expiryBefore, expiryAfter, err)
		}
		if len(harness.service.active) != 1 || harness.service.active[first.Id] != active {
			t.Fatal("remembered terminal confirmation disturbed active runtime")
		}
	})

	t.Run("absent consent fails closed", func(t *testing.T) {
		harness, _, _, recovery, tracker, _ := prepare(t, false)
		request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", recovery)
		request.TerminalReplayProof = terminalRememberedProof()
		if response, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(request)); response != nil ||
			!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("absent remembered proof response_present=%t err=%v", response != nil, err)
		}
		tracker.assertClosedAndZeroed(t, 0)
	})

	t.Run("remembered capability must be explicitly elected", func(t *testing.T) {
		harness, _, _, recovery, tracker, _ := prepare(t, true)
		request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", recovery)
		request.TerminalReplayProof = &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_UseRememberedWorkspace{}}
		if response, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(request)); response != nil ||
			!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("false remembered election response_present=%t err=%v", response != nil, err)
		}
		tracker.assertClosedAndZeroed(t, 0)
	})

	for _, testCase := range []struct {
		name          string
		deleteFailure bool
	}{
		{name: "expired consent fails closed"},
		{name: "expired consent deletion failure fails closed", deleteFailure: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness, _, _, recovery, tracker, _ := prepare(t, true)
			*harness.now = harness.now.Add(rememberedLifetime)
			if testCase.deleteFailure {
				harness.service.remembered.store = cleanupFailingSecretStore{
					SecretStore: harness.service.remembered.store,
					err:         errors.New("credential deletion unavailable"),
				}
			}
			request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", recovery)
			request.TerminalReplayProof = terminalRememberedProof()
			if response, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(request)); response != nil ||
				!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
				t.Fatalf("expired remembered proof response_present=%t err=%v", response != nil, err)
			}
			tracker.assertClosedAndZeroed(t, 0)
		})
	}
}

func TestTerminalConfirmationPassphraseEnforcesWorkspaceUnlockCooldown(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := context.Background()
	workspace, recovery := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	tracker := attachReplayTrackingStorage(harness)
	request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", recovery)
	request.TerminalReplayProof = terminalPassphraseProof("wrong-terminal-passphrase-long-enough")
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	catalogueBefore, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	header, err := NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
	if err != nil {
		t.Fatal(err)
	}
	slotsBefore, err := header.Slots()
	header.Close()
	if err != nil {
		t.Fatal(err)
	}
	auditBefore := harness.audit.counts["RECOVERY_CONFIRMATION"]
	for attempt := 1; attempt <= policy.Limit; attempt++ {
		if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.ConfirmRecoveryRequest))); response != nil ||
			!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("wrong terminal passphrase attempt %d response_present=%t err=%v", attempt, response != nil, err)
		}
		tracker.assertClosedAndZeroed(t, 0)
		decision, err := harness.service.attempts.Status("workspace_unlock", workspace.Id, policy)
		if err != nil || decision.AttemptCount != attempt {
			t.Fatalf("shared terminal attempt count after %d = %+v, err=%v", attempt, decision, err)
		}
	}
	decision, err := harness.service.attempts.Status("workspace_unlock", workspace.Id, policy)
	if err != nil || !decision.CoolingDown(*harness.now) {
		t.Fatalf("terminal passphrase cooldown = %+v, err=%v", decision, err)
	}
	correct := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", recovery)
	correct.TerminalReplayProof = terminalPassphraseProof("workspace-passphrase-long-enough")
	if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(correct).(*tammyv1.ConfirmRecoveryRequest))); response != nil ||
		!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("correct terminal passphrase during cooldown response_present=%t err=%v", response != nil, err)
	}
	tracker.assertClosedAndZeroed(t, 0)
	decision, err = harness.service.attempts.Status("workspace_unlock", workspace.Id, policy)
	if err != nil || decision.AttemptCount != policy.Limit {
		t.Fatalf("cooldown attempt performed password work or appended a failure: %+v err=%v", decision, err)
	}

	*harness.now = harness.now.Add(policy.Cooldown)
	response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(correct))
	if err != nil || response.Msg.Workspace.Id != workspace.Id || response.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("terminal passphrase after cooldown response=%v err=%v", response, err)
	}
	tracker.assertClosedAndZeroed(t, 1)
	decision, err = harness.service.attempts.Status("workspace_unlock", workspace.Id, policy)
	if err != nil || decision.AttemptCount != 0 || !decision.CooldownUntil.IsZero() {
		t.Fatalf("successful terminal passphrase did not reset shared attempts: %+v err=%v", decision, err)
	}
	catalogueAfter, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil || !reflect.DeepEqual(catalogueBefore, catalogueAfter) {
		t.Fatalf("terminal passphrase attempts changed catalogue err=%v", err)
	}
	header, err = NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
	if err != nil {
		t.Fatal(err)
	}
	slotsAfter, err := header.Slots()
	header.Close()
	if err != nil || !reflect.DeepEqual(slotsBefore, slotsAfter) {
		t.Fatalf("terminal passphrase attempts changed header err=%v", err)
	}
	if len(harness.service.active) != 0 || harness.audit.counts["RECOVERY_CONFIRMATION"] != auditBefore {
		t.Fatalf("terminal passphrase attempts admitted runtime or changed audit active=%d audit=%d", len(harness.service.active), harness.audit.counts["RECOVERY_CONFIRMATION"])
	}
}

func TestTerminalConfirmationAndUnlockSharePersistedPassphraseCooldown(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := context.Background()
	workspace, recovery := createConfirmedWorkspace(t, harness)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	terminal := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", recovery)
	terminal.TerminalReplayProof = terminalPassphraseProof("wrong-terminal-passphrase-long-enough")
	unlock := unlockRequest("wrong-unlock-passphrase-long-enough", false)
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	for attempt := 1; attempt <= policy.Limit; attempt++ {
		var responsePresent bool
		var err error
		if attempt%2 == 0 {
			var response *connect.Response[tammyv1.ConfirmRecoveryResponse]
			response, err = harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(terminal).(*tammyv1.ConfirmRecoveryRequest)))
			responsePresent = response != nil
		} else {
			var response *connect.Response[tammyv1.UnlockWorkspaceResponse]
			response, err = harness.service.UnlockWorkspace(ctx, connect.NewRequest(proto.Clone(unlock).(*tammyv1.UnlockWorkspaceRequest)))
			responsePresent = response != nil
		}
		if responsePresent || !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("alternating passphrase attempt %d response_present=%t err=%v", attempt, responsePresent, err)
		}
		decision, statusErr := harness.service.attempts.Status("workspace_unlock", workspace.Id, policy)
		if statusErr != nil || decision.AttemptCount != attempt {
			t.Fatalf("alternating shared count after %d = %+v err=%v", attempt, decision, statusErr)
		}
	}
	harness.service.attempts.Close()
	restartedAttempts, err := NewAttemptJournal(harness.attemptPath, harness.attemptKey, harness.config.Clock,
		harness.attemptAnchorID, harness.attemptAnchors)
	if err != nil {
		t.Fatal(err)
	}
	restartConfig := harness.config
	restartConfig.Attempts = restartedAttempts
	restarted, err := NewService(restartConfig)
	if err != nil {
		t.Fatal(err)
	}
	harness.service = restarted
	tracker := attachReplayTrackingStorage(harness)
	correctTerminal := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", recovery)
	correctTerminal.TerminalReplayProof = terminalPassphraseProof("workspace-passphrase-long-enough")
	if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(proto.Clone(correctTerminal).(*tammyv1.ConfirmRecoveryRequest))); response != nil ||
		!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("restarted terminal cooldown response_present=%t err=%v", response != nil, err)
	}
	if response, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false))); response != nil ||
		!errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("restarted unlock cooldown response_present=%t err=%v", response != nil, err)
	}
	tracker.assertClosedAndZeroed(t, 0)
	decision, err := harness.service.attempts.Status("workspace_unlock", workspace.Id, policy)
	if err != nil || decision.AttemptCount != policy.Limit || !decision.CoolingDown(*harness.now) {
		t.Fatalf("restarted shared cooldown = %+v err=%v", decision, err)
	}
	*harness.now = harness.now.Add(policy.Cooldown)
	response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(correctTerminal))
	if err != nil || response.Msg.Workspace.Id != workspace.Id {
		t.Fatalf("restarted terminal after cooldown response=%v err=%v", response, err)
	}
	tracker.assertClosedAndZeroed(t, 1)
	decision, err = harness.service.attempts.Status("workspace_unlock", workspace.Id, policy)
	if err != nil || decision.AttemptCount != 0 {
		t.Fatalf("restarted terminal success did not clear shared cooldown: %+v err=%v", decision, err)
	}
}

func TestTerminalConfirmationPassphraseFailsClosedOnAttemptJournalErrors(t *testing.T) {
	testCases := []struct {
		name         string
		proof        string
		prepare      func(*testing.T, workspaceHarness, string)
		wantError    error
		wantSQLCOpen int
	}{
		{name: "unavailable journal", proof: "workspace-passphrase-long-enough", prepare: func(_ *testing.T, harness workspaceHarness, _ string) {
			harness.service.attempts.Close()
		}, wantError: ErrAttemptPolicy},
		{name: "corrupt persisted journal", proof: "workspace-passphrase-long-enough", prepare: func(t *testing.T, harness workspaceHarness, workspaceID string) {
			t.Helper()
			policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
			if _, err := harness.service.attempts.Failure("workspace_unlock", workspaceID, policy); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(harness.attemptPath, []byte("{corrupt\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantError: ErrAttemptJournalAuthentication},
		{name: "failure append unavailable", proof: "wrong-terminal-passphrase-long-enough", prepare: func(_ *testing.T, harness workspaceHarness, _ string) {
			harness.service.attempts.anchors = saveFailingAnchorStore{AnchorStore: harness.service.attempts.anchors, err: errors.New("anchor save unavailable")}
		}, wantError: ErrAttemptJournalAuthentication},
		{name: "success reset unavailable", proof: "workspace-passphrase-long-enough", prepare: func(_ *testing.T, harness workspaceHarness, _ string) {
			harness.service.attempts.anchors = saveFailingAnchorStore{AnchorStore: harness.service.attempts.anchors, err: errors.New("anchor save unavailable")}
		}, wantError: ErrAttemptJournalAuthentication, wantSQLCOpen: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			ctx := context.Background()
			workspace, recovery := createConfirmedWorkspace(t, harness)
			if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
				t.Fatal(err)
			}
			catalogueBefore, err := harness.repository.ByID(ctx, workspace.Id)
			if err != nil {
				t.Fatal(err)
			}
			header, err := NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
			if err != nil {
				t.Fatal(err)
			}
			slotsBefore, err := header.Slots()
			header.Close()
			if err != nil {
				t.Fatal(err)
			}
			auditBefore := harness.audit.counts["RECOVERY_CONFIRMATION"]
			testCase.prepare(t, harness, workspace.Id)
			tracker := attachReplayTrackingStorage(harness)
			request := confirmationRequestFor(t, "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", recovery)
			request.TerminalReplayProof = terminalPassphraseProof(testCase.proof)
			if response, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(request)); response != nil || !errors.Is(err, testCase.wantError) {
				t.Fatalf("journal failure response_present=%t err=%v want=%v", response != nil, err, testCase.wantError)
			}
			tracker.assertClosedAndZeroed(t, testCase.wantSQLCOpen)
			catalogueAfter, err := harness.repository.ByID(ctx, workspace.Id)
			if err != nil || !reflect.DeepEqual(catalogueBefore, catalogueAfter) {
				t.Fatalf("journal failure changed catalogue err=%v", err)
			}
			header, err = NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
			if err != nil {
				t.Fatal(err)
			}
			slotsAfter, err := header.Slots()
			header.Close()
			if err != nil || !reflect.DeepEqual(slotsBefore, slotsAfter) {
				t.Fatalf("journal failure changed header err=%v", err)
			}
			if len(harness.service.active) != 0 || harness.audit.counts["RECOVERY_CONFIRMATION"] != auditBefore {
				t.Fatalf("journal failure admitted runtime or changed audit active=%d audit=%d", len(harness.service.active), harness.audit.counts["RECOVERY_CONFIRMATION"])
			}
		})
	}
}

func TestTerminalRecoveryReplaysDoNotDisturbAnotherActiveWorkspace(t *testing.T) {
	for _, operation := range []string{"recovery", "administrator recovery"} {
		t.Run(operation, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			tracker := attachReplayTrackingStorage(harness)
			ctx := context.Background()
			first, _, second, secondRecovery := prepareTwoLockedWorkspaces(t, harness)
			operationID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073925"
			newPassphrase := "terminal-recovery-passphrase-long-enough"
			var recover func() error
			if operation == "recovery" {
				recover = func() error {
					_, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(&tammyv1.RecoverWorkspaceRequest{
						RecoveryOperationId: operationID, WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
						RecoverySecret: &tammyv1.SecretInput{Utf8: append([]byte(nil), secondRecovery...)},
						NewPassphrase:  &tammyv1.SecretInput{Utf8: []byte(newPassphrase)},
					}))
					return err
				}
			} else {
				recover = func() error {
					_, err := harness.service.RecoverAdministrator(ctx, &tammyv1.RecoverAdministratorRequest{
						RecoveryOperationId: operationID, WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
						RecoverySecret: &tammyv1.SecretInput{Utf8: append([]byte(nil), secondRecovery...)}, AdministratorUsername: "admin@example.test",
						NewWorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte(newPassphrase)},
						NewUserPassword:        &tammyv1.SecretInput{Utf8: []byte("terminal-admin-password-long-enough")},
					})
					return err
				}
			}
			if err := recover(); err != nil {
				t.Fatal(err)
			}
			if operation == "recovery" {
				if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequestFor(
				"workspace-file-capability", "workspace-passphrase-long-enough"))); err != nil {
				t.Fatal(err)
			}
			active := harness.service.active[first.Id]
			before, err := harness.repository.ByID(ctx, second.Id)
			if err != nil {
				t.Fatal(err)
			}
			auditKind := "RECOVERY"
			if operation != "recovery" {
				auditKind = "ADMIN_RECOVERY"
			}
			auditBefore := harness.audit.counts[auditKind]
			adminMutationsBefore := *harness.adminRecoveryMutations
			tracker.reset()
			if err := recover(); err != nil {
				t.Fatalf("exact terminal replay: %v", err)
			}
			tracker.assertClosedAndZeroed(t, 1)
			after, err := harness.repository.ByID(ctx, second.Id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("exact terminal replay changed catalogue err=%v", err)
			}
			if len(harness.service.active) != 1 || harness.service.active[first.Id] != active || harness.audit.counts[auditKind] != auditBefore {
				t.Fatalf("exact terminal replay active=%d audit=%d want=%d", len(harness.service.active), harness.audit.counts[auditKind], auditBefore)
			}
			if *harness.adminRecoveryMutations != adminMutationsBefore {
				t.Fatalf("exact terminal replay repeated administrator mutation got=%d want=%d", *harness.adminRecoveryMutations, adminMutationsBefore)
			}

			tracker.reset()
			errorsFound := make(chan error, 2)
			for range 2 {
				go func() { errorsFound <- recover() }()
			}
			if left, right := <-errorsFound, <-errorsFound; left != nil || right != nil {
				t.Fatalf("concurrent terminal replays=%v/%v", left, right)
			}
			tracker.assertClosedAndZeroed(t, 2)
			if len(harness.service.active) != 1 || harness.service.active[first.Id] != active || harness.audit.counts[auditKind] != auditBefore {
				t.Fatalf("concurrent terminal replay active=%d audit=%d want=%d", len(harness.service.active), harness.audit.counts[auditKind], auditBefore)
			}
			if *harness.adminRecoveryMutations != adminMutationsBefore {
				t.Fatalf("concurrent terminal replay repeated administrator mutation got=%d want=%d", *harness.adminRecoveryMutations, adminMutationsBefore)
			}
			if operation != "recovery" {
				tracker.reset()
				_, err := harness.service.RecoverAdministrator(ctx, &tammyv1.RecoverAdministratorRequest{
					RecoveryOperationId: operationID, WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
					RecoverySecret: &tammyv1.SecretInput{Utf8: append([]byte(nil), secondRecovery...)}, AdministratorUsername: "admin@example.test",
					NewWorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte(newPassphrase)},
					NewUserPassword:        &tammyv1.SecretInput{Utf8: []byte("changed-terminal-admin-password-long-enough")},
				})
				if !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
					t.Fatalf("changed administrator replay returned %v", err)
				}
				tracker.assertClosedAndZeroed(t, 1)
				if *harness.adminRecoveryMutations != adminMutationsBefore {
					t.Fatalf("changed administrator replay mutated domain got=%d want=%d", *harness.adminRecoveryMutations, adminMutationsBefore)
				}
			}

			changedSecret := append([]byte(nil), secondRecovery...)
			changedSecret[0] ^= 1
			var changedErr error
			if operation == "recovery" {
				_, changedErr = harness.service.RecoverWorkspace(ctx, connect.NewRequest(&tammyv1.RecoverWorkspaceRequest{
					RecoveryOperationId: operationID, WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
					RecoverySecret: &tammyv1.SecretInput{Utf8: changedSecret}, NewPassphrase: &tammyv1.SecretInput{Utf8: []byte(newPassphrase)},
				}))
			} else {
				_, changedErr = harness.service.RecoverAdministrator(ctx, &tammyv1.RecoverAdministratorRequest{
					RecoveryOperationId: operationID, WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
					RecoverySecret: &tammyv1.SecretInput{Utf8: changedSecret}, AdministratorUsername: "admin@example.test",
					NewWorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte(newPassphrase)},
					NewUserPassword:        &tammyv1.SecretInput{Utf8: []byte("terminal-admin-password-long-enough")},
				})
			}
			if !errors.Is(changedErr, faults.New(faults.CodeIdempotencyConflict, nil)) {
				t.Fatalf("changed terminal proof returned %v", changedErr)
			}
		})
	}
}

func TestRuntimeGateClosesAndZerosLosingCandidate(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, _ := createConfirmedWorkspace(t, harness)
	storage := &closeTrackingStorage{}
	loser := &workspaceRuntime{dek: bytes.Repeat([]byte{0x5a}, DEKSize), storage: storage}
	if err := harness.service.admitRuntime("01890f3c-7b2e-7cc4-98c4-dc0c0c073999", loser); err == nil {
		t.Fatal("runtime gate admitted a second workspace")
	}
	if !storage.closed || !bytes.Equal(loser.dek, make([]byte, DEKSize)) || len(harness.service.active) != 1 ||
		harness.service.active[workspace.Id] == nil {
		t.Fatalf("losing candidate cleanup closed=%t zeroed=%t active=%d", storage.closed,
			bytes.Equal(loser.dek, make([]byte, DEKSize)), len(harness.service.active))
	}
}

func TestRuntimeGateRejectsSecondWorkspaceAcrossOpeningPaths(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		first, _ := createConfirmedWorkspace(t, harness)
		secondDirectory := t.TempDir()
		harness.service.capabilities.(testCapabilities)["second-destination-capability"] = secondDirectory
		_, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "second-destination-capability"},
			WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("second-workspace-passphrase-long-enough")}, AdministratorUsername: "admin@example.test",
			AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
		}))
		if err == nil || len(harness.service.active) != 1 || harness.service.active[first.Id] == nil {
			t.Fatalf("second create err=%v active=%d", err, len(harness.service.active))
		}
		if _, statErr := os.Lstat(filepath.Join(secondDirectory, "tammy-workspace.db")); !os.IsNotExist(statErr) {
			t.Fatalf("rejected create produced a database: %v", statErr)
		}
	})

	t.Run("recovery confirmation", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		first, _ := createConfirmedWorkspace(t, harness)
		ctx := context.Background()
		if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
			t.Fatal(err)
		}
		secondDirectory := t.TempDir()
		harness.service.capabilities.(testCapabilities)["second-destination-capability"] = secondDirectory
		created, err := harness.service.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "second-destination-capability"},
			WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("second-workspace-passphrase-long-enough")}, AdministratorUsername: "admin@example.test",
			AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
		}))
		if err != nil {
			t.Fatal(err)
		}
		groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
		if err != nil {
			t.Fatal(err)
		}
		harness.service.closeRuntime(created.Msg.Workspace.Id)
		if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequestFor(
			"workspace-file-capability", "workspace-passphrase-long-enough"))); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.service.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073921", Confirmations: []*tammyv1.RecoveryGroupConfirmation{
				{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
		})); err == nil {
			t.Fatal("recovery confirmation opened a second workspace")
		}
		if len(harness.service.active) != 1 || harness.service.active[first.Id] == nil {
			t.Fatalf("recovery confirmation changed active runtime: %d", len(harness.service.active))
		}
	})

	for _, operation := range []string{"recovery", "administrator recovery"} {
		t.Run(operation, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			first, _, _, secondRecovery := prepareTwoLockedWorkspaces(t, harness)
			ctx := context.Background()
			if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequestFor(
				"workspace-file-capability", "workspace-passphrase-long-enough"))); err != nil {
				t.Fatal(err)
			}
			if operation == "recovery" {
				_, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(&tammyv1.RecoverWorkspaceRequest{
					RecoveryOperationId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073922",
					WorkspaceFile:       &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
					RecoverySecret:      &tammyv1.SecretInput{Utf8: secondRecovery},
					NewPassphrase:       &tammyv1.SecretInput{Utf8: []byte("rejected-second-recovery-passphrase-long-enough")},
				}))
				if err == nil {
					t.Fatal("recovery opened a second workspace")
				}
			} else {
				_, err := harness.service.RecoverAdministrator(ctx, &tammyv1.RecoverAdministratorRequest{
					RecoveryOperationId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073923",
					WorkspaceFile:       &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
					RecoverySecret:      &tammyv1.SecretInput{Utf8: secondRecovery}, AdministratorUsername: "admin@example.test",
					NewWorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("rejected-admin-recovery-passphrase-long-enough")},
					NewUserPassword:        &tammyv1.SecretInput{Utf8: []byte("rejected-admin-password-long-enough")},
				})
				if err == nil || *harness.adminRecovered {
					t.Fatalf("administrator recovery err=%v reset=%t", err, *harness.adminRecovered)
				}
			}
			if len(harness.service.active) != 1 || harness.service.active[first.Id] == nil {
				t.Fatalf("%s changed active runtime: %d", operation, len(harness.service.active))
			}
		})
	}
}

func TestRuntimeGateAllowsOnlySameWorkspacePendingTransition(t *testing.T) {
	harness := newWorkspaceHarness(t)
	request := &tammyv1.CreateWorkspaceRequest{
		SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073911", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
		WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")}, AdministratorUsername: "admin@example.test",
		AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}
	created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(proto.Clone(request).(*tammyv1.CreateWorkspaceRequest)))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(proto.Clone(request).(*tammyv1.CreateWorkspaceRequest)))
	if err != nil || replayed.Msg.Workspace.Id != created.Msg.Workspace.Id || len(harness.service.active) != 1 {
		t.Fatalf("same pending replay err=%v active=%d", err, len(harness.service.active))
	}
	groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: request.SetupId, Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
	})); err != nil || len(harness.service.active) != 1 {
		t.Fatalf("same pending confirmation err=%v active=%d", err, len(harness.service.active))
	}
}

func TestConcurrentUnlockAndRecoveryAdmitExactlyOneRuntime(t *testing.T) {
	harness := newWorkspaceHarness(t)
	first, _, second, secondRecovery := prepareTwoLockedWorkspaces(t, harness)
	ctx := context.Background()
	errorsFound := make(chan error, 2)
	go func() {
		_, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequestFor(
			"workspace-file-capability", "workspace-passphrase-long-enough")))
		errorsFound <- err
	}()
	go func() {
		_, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(&tammyv1.RecoverWorkspaceRequest{
			RecoveryOperationId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073924",
			WorkspaceFile:       &tammyv1.ApprovedFileRef{CapabilityId: "second-workspace-file-capability"},
			RecoverySecret:      &tammyv1.SecretInput{Utf8: secondRecovery},
			NewPassphrase:       &tammyv1.SecretInput{Utf8: []byte("concurrent-recovery-passphrase-long-enough")},
		}))
		errorsFound <- err
	}()
	firstErr, secondErr := <-errorsFound, <-errorsFound
	if (firstErr == nil) == (secondErr == nil) || len(harness.service.active) != 1 {
		t.Fatalf("concurrent openings errors=%v/%v active=%d", firstErr, secondErr, len(harness.service.active))
	}
	if harness.service.active[first.Id] == nil && harness.service.active[second.Id] == nil {
		t.Fatalf("unexpected active runtime: %+v", harness.service.active)
	}
}

func requireAttemptJournalWriteError(t *testing.T, err error) {
	t.Helper()
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("attempt journal write returned %v", err)
	}
}

func TestConfirmRecoveryFailsClosedWhenAttemptJournalCannotWrite(t *testing.T) {
	t.Run("failed confirmation", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		_, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073952", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
			WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")}, AdministratorUsername: "admin@example.test",
			AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
		}))
		if err != nil {
			t.Fatal(err)
		}
		harness.service.attempts.path = t.TempDir()
		_, err = harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073952", Confirmations: []*tammyv1.RecoveryGroupConfirmation{
				{GroupIndex: 0, Value: []byte("WRONG")}, {GroupIndex: 1, Value: []byte("WRONG")},
			},
		}))
		requireAttemptJournalWriteError(t, err)
	})

	t.Run("successful confirmation", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073953", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
			WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")}, AdministratorUsername: "admin@example.test",
			AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
		}))
		if err != nil {
			t.Fatal(err)
		}
		groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
		if err != nil {
			t.Fatal(err)
		}
		harness.service.attempts.path = t.TempDir()
		_, err = harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
			SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073953", Confirmations: []*tammyv1.RecoveryGroupConfirmation{
				{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]},
			},
		}))
		requireAttemptJournalWriteError(t, err)
		record, loadErr := harness.repository.ByID(context.Background(), created.Msg.Workspace.Id)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY {
			t.Fatalf("journal failure persisted confirmation state %v", record.State)
		}
	})
}

func TestConfirmRecoveryScrubsAuthoritativeSQLCipherRecord(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, _ := createConfirmedWorkspace(t, harness)
	record, err := harness.service.active[workspace.Id].storage.LoadWorkspaceRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
		t.Fatalf("authoritative state = %v", record.State)
	}
	if len(record.RecoveryDisplayEncrypted) != 0 || len(record.RecoveryGroupHashes) != 0 || len(record.SetupMaterialEncrypted) != 0 {
		t.Fatalf("authoritative recovery material retained: display=%d hashes=%d setup=%d",
			len(record.RecoveryDisplayEncrypted), len(record.RecoveryGroupHashes), len(record.SetupMaterialEncrypted))
	}
}

func TestConfirmRecoveryReopensPendingRuntimeAfterRestart(t *testing.T) {
	cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
	repository, err := NewFileRepository(cataloguePath, bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	harness := newWorkspaceHarnessWithRepository(t, repository)
	request := &tammyv1.CreateWorkspaceRequest{
		SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073975", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
		WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}
	created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	harness.service.closeRuntime(created.Msg.Workspace.Id)
	restartedRepository, err := NewFileRepository(cataloguePath, bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	restartConfig := harness.config
	restartConfig.Repository = restartedRepository
	restarted, err := NewService(restartConfig)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := restarted.ConfirmRecovery(context.Background(), connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: request.SetupId, Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 12, Value: groups[12]},
		},
	}))
	if err != nil || confirmed.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
		t.Fatalf("restart confirmation: response=%v err=%v", confirmed, err)
	}
}

func TestConfirmRecoveryConvergesCatalogueWithoutRedisplayingSecretAfterCommitInterruption(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		checkpoint      bool
		advance         bool
		clearCiphertext bool
		setupID         string
	}{
		{name: "catalogue save failure", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073977"},
		{name: "process death checkpoint after setup deadline", checkpoint: true, advance: true, clearCiphertext: true, setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073978"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
			catalogueKey := bytes.Repeat([]byte{5}, 32)
			baseRepository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			repository := &failNextWorkspaceSaveRepository{Repository: baseRepository}
			harness := newWorkspaceHarnessWithRepository(t, repository)
			request := &tammyv1.CreateWorkspaceRequest{
				SetupId: testCase.setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
				WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
				AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
				AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
			}
			created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request))
			if err != nil {
				t.Fatal(err)
			}
			groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
			if err != nil {
				t.Fatal(err)
			}
			stale, err := baseRepository.BySetup(context.Background(), testCase.setupID)
			if err != nil {
				t.Fatal(err)
			}
			material, err := harness.service.openSetupMaterial(stale.SetupID, stale.SetupMaterialEncrypted)
			if err != nil {
				t.Fatal(err)
			}
			dek := append([]byte(nil), material.DEK...)
			material.destroy()
			defer Zero(dek)
			interrupted := errors.New("confirmation interrupted after SQLCipher commit")
			if testCase.checkpoint {
				harness.failures.failures["confirm_recovery.after_database_commit"] = interrupted
			} else {
				repository.failNext = interrupted
			}
			if _, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
				SetupId: testCase.setupID, Confirmations: []*tammyv1.RecoveryGroupConfirmation{
					{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 12, Value: groups[12]},
				},
			})); !errors.Is(err, interrupted) {
				t.Fatalf("interrupted confirmation returned %v", err)
			}
			harness.service.closeRuntime(created.Msg.Workspace.Id)
			if testCase.advance {
				*harness.now = harness.now.Add(15*time.Minute + time.Second)
			}
			stale, err = baseRepository.BySetup(context.Background(), testCase.setupID)
			if err != nil {
				t.Fatal(err)
			}
			if testCase.clearCiphertext {
				stale.RecoveryDisplayEncrypted = nil
				stale.SetupMaterialEncrypted = nil
			} else {
				stale.RecoveryDisplayEncrypted = []byte("corrupt recovery display ciphertext")
				stale.SetupMaterialEncrypted = []byte("corrupt setup material ciphertext")
			}
			if err := baseRepository.Save(context.Background(), stale); err != nil {
				t.Fatal(err)
			}
			restartedRepository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			restartConfig := harness.config
			restartConfig.Repository = restartedRepository
			restarted, err := NewService(restartConfig)
			if err != nil {
				t.Fatal(err)
			}
			changed := proto.Clone(request).(*tammyv1.CreateWorkspaceRequest)
			changed.AdministratorDisplayName = "Changed Admin"
			if response, err := restarted.CreateWorkspace(context.Background(), connect.NewRequest(changed)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) || response != nil {
				t.Fatalf("changed stale setup replay returned response_present=%t err=%v", response != nil, err)
			}
			response, err := restarted.CreateWorkspace(context.Background(), connect.NewRequest(request))
			if !errors.Is(err, faults.New(faults.CodeValidation, nil)) || response != nil {
				t.Fatalf("confirmed setup replay returned response_present=%t err=%v", response != nil, err)
			}
			converged, err := restartedRepository.BySetup(context.Background(), testCase.setupID)
			if err != nil {
				t.Fatal(err)
			}
			if converged.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED || converged.SetupPhase != "confirmed" ||
				len(converged.RecoveryDisplayEncrypted) != 0 || len(converged.RecoveryGroupHashes) != 0 || len(converged.SetupMaterialEncrypted) != 0 {
				t.Fatalf("catalogue did not converge without secrets: state=%v phase=%q display=%d hashes=%d setup=%d",
					converged.State, converged.SetupPhase, len(converged.RecoveryDisplayEncrypted), len(converged.RecoveryGroupHashes), len(converged.SetupMaterialEncrypted))
			}
			storage, err := restartConfig.Storage.Open(context.Background(), harness.databasePath, dek)
			if err != nil {
				t.Fatal(err)
			}
			defer storage.Close()
			authoritative, err := storage.LoadWorkspaceRecord(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED || authoritative.SetupPhase != "confirmed" ||
				len(authoritative.RecoveryDisplayEncrypted) != 0 || len(authoritative.RecoveryGroupHashes) != 0 || len(authoritative.SetupMaterialEncrypted) != 0 {
				t.Fatalf("authoritative confirmation was downgraded: state=%v phase=%q display=%d hashes=%d setup=%d",
					authoritative.State, authoritative.SetupPhase, len(authoritative.RecoveryDisplayEncrypted),
					len(authoritative.RecoveryGroupHashes), len(authoritative.SetupMaterialEncrypted))
			}
			if _, err := restarted.CreateWorkspace(context.Background(), connect.NewRequest(changed)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
				t.Fatalf("changed confirmed setup replay returned %v", err)
			}
		})
	}
}

func TestConfirmRecoveryRetryReconcilesCommittedDatabaseExactlyOnce(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		advance time.Duration
		setupID string
	}{
		{name: "immediate restart", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073979"},
		{name: "restart after deadline", advance: 15*time.Minute + time.Second, setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073980"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
			catalogueKey := bytes.Repeat([]byte{5}, 32)
			baseRepository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			repository := &failNextWorkspaceSaveRepository{Repository: baseRepository}
			harness := newWorkspaceHarnessWithRepository(t, repository)
			createRequest := &tammyv1.CreateWorkspaceRequest{
				SetupId: testCase.setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
				WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
				AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
				AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
			}
			created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(createRequest))
			if err != nil {
				t.Fatal(err)
			}
			groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
			if err != nil {
				t.Fatal(err)
			}
			confirmRequest := &tammyv1.ConfirmRecoveryRequest{SetupId: testCase.setupID, Confirmations: []*tammyv1.RecoveryGroupConfirmation{
				{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 12, Value: groups[12]},
			}}
			interrupted := errors.New("catalogue unavailable after confirmation commit")
			repository.failNext = interrupted
			if _, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(confirmRequest)); !errors.Is(err, interrupted) {
				t.Fatalf("interrupted confirmation returned %v", err)
			}
			if got := harness.audit.counts["RECOVERY_CONFIRMATION"]; got != 1 {
				t.Fatalf("committed confirmation audit count = %d, want 1", got)
			}
			harness.service.closeRuntime(created.Msg.Workspace.Id)
			*harness.now = harness.now.Add(testCase.advance)

			restartedRepository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			restartConfig := harness.config
			restartConfig.Repository = restartedRepository
			restarted, err := NewService(restartConfig)
			if err != nil {
				t.Fatal(err)
			}
			response, err := restarted.ConfirmRecovery(context.Background(), connect.NewRequest(proto.Clone(confirmRequest).(*tammyv1.ConfirmRecoveryRequest)))
			if err != nil || response.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
				t.Fatalf("confirmation retry: response=%v err=%v", response, err)
			}
			if got := harness.audit.counts["RECOVERY_CONFIRMATION"]; got != 1 {
				t.Fatalf("confirmation replay audit count = %d, want exactly 1", got)
			}
			converged, err := restartedRepository.BySetup(context.Background(), testCase.setupID)
			if err != nil {
				t.Fatal(err)
			}
			if converged.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED || converged.SetupPhase != "confirmed" ||
				converged.DatabasePath == "" || converged.HeaderPath == "" || converged.SetupCleanupDatabasePath != "" ||
				converged.SetupCleanupHeaderPath != "" || len(converged.RecoveryDisplayEncrypted) != 0 ||
				len(converged.RecoveryGroupHashes) != 0 || len(converged.SetupMaterialEncrypted) != 0 {
				t.Fatalf("catalogue did not converge safely: %+v", converged)
			}
			for _, path := range []string{harness.databasePath, converged.HeaderPath} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("committed workspace artifact %q was not retained: %v", path, err)
				}
			}
		})
	}
}

func TestConfirmRecoveryRetryFailsClosedOnUnboundSetupMaterialWithoutCleanup(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		setupID string
		tamper  func(*testing.T, *Service, workspaceRecord) []byte
	}{
		{name: "corrupt ciphertext", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073981", tamper: func(_ *testing.T, _ *Service, _ workspaceRecord) []byte {
			return []byte("corrupt installation ciphertext")
		}},
		{name: "mismatched authenticated material", setupID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073982", tamper: func(t *testing.T, service *Service, record workspaceRecord) []byte {
			material, err := service.openSetupMaterial(record.SetupID, record.SetupMaterialEncrypted)
			if err != nil {
				t.Fatal(err)
			}
			defer material.destroy()
			material.InitialHeader.WorkspaceID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073999"
			encrypted, err := service.sealSetupMaterial(record.SetupID, *material)
			if err != nil {
				t.Fatal(err)
			}
			return encrypted
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
			catalogueKey := bytes.Repeat([]byte{5}, 32)
			baseRepository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			repository := &failNextWorkspaceSaveRepository{Repository: baseRepository}
			harness := newWorkspaceHarnessWithRepository(t, repository)
			request := &tammyv1.CreateWorkspaceRequest{
				SetupId: testCase.setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: "destination-capability"},
				WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
				AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
				AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
			}
			created, err := harness.service.CreateWorkspace(context.Background(), connect.NewRequest(request))
			if err != nil {
				t.Fatal(err)
			}
			groups, err := ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
			if err != nil {
				t.Fatal(err)
			}
			confirm := &tammyv1.ConfirmRecoveryRequest{SetupId: testCase.setupID, Confirmations: []*tammyv1.RecoveryGroupConfirmation{
				{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]},
			}}
			repository.failNext = errors.New("catalogue unavailable after SQLCipher commit")
			if _, err := harness.service.ConfirmRecovery(context.Background(), connect.NewRequest(confirm)); err == nil {
				t.Fatal("confirmation unexpectedly crossed catalogue failure")
			}
			harness.service.closeRuntime(created.Msg.Workspace.Id)
			stale, err := baseRepository.BySetup(context.Background(), testCase.setupID)
			if err != nil {
				t.Fatal(err)
			}
			stale.SetupMaterialEncrypted = testCase.tamper(t, harness.service, stale)
			if err := baseRepository.Save(context.Background(), stale); err != nil {
				t.Fatal(err)
			}
			*harness.now = harness.now.Add(15*time.Minute + time.Second)
			restartedRepository, err := NewFileRepository(cataloguePath, catalogueKey)
			if err != nil {
				t.Fatal(err)
			}
			config := harness.config
			config.Repository = restartedRepository
			restarted, err := NewService(config)
			if err != nil {
				t.Fatal(err)
			}
			if response, err := restarted.ConfirmRecovery(context.Background(), connect.NewRequest(proto.Clone(confirm).(*tammyv1.ConfirmRecoveryRequest))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) || response != nil {
				t.Fatalf("unbound retry returned response_present=%t err=%v", response != nil, err)
			}
			retained, err := restartedRepository.BySetup(context.Background(), testCase.setupID)
			if err != nil {
				t.Fatal(err)
			}
			if retained.State != tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY || retained.SetupPhase != "ready" ||
				retained.DatabasePath == "" || retained.HeaderPath == "" || retained.SetupCleanupDatabasePath != "" || retained.SetupCleanupHeaderPath != "" {
				t.Fatalf("unbound retry mutated or tombstoned catalogue: %+v", retained)
			}
			for _, path := range []string{retained.DatabasePath, retained.HeaderPath} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("committed artifact %q was removed: %v", path, err)
				}
			}
			if got := harness.audit.counts["RECOVERY_CONFIRMATION"]; got != 1 {
				t.Fatalf("confirmation audit count = %d, want 1", got)
			}
		})
	}
}

func TestUnlockFailsClosedWhenAttemptJournalUnavailable(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, _ := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	harness.service.attempts.Close()
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false))); !errors.Is(err, ErrAttemptPolicy) {
		t.Fatalf("unavailable unlock journal returned %v", err)
	}
	record, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("journal failure opened workspace: %v", record.State)
	}
}

func TestRecoveryFailsClosedWhenAttemptJournalUnavailable(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, recovery := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	harness.service.attempts.Close()
	if _, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(&tammyv1.RecoverWorkspaceRequest{
		RecoveryOperationId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073954", WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		RecoverySecret: &tammyv1.SecretInput{Utf8: recovery}, NewPassphrase: &tammyv1.SecretInput{Utf8: []byte("journal-safe-recovery-passphrase-long-enough")},
	})); !errors.Is(err, ErrAttemptPolicy) {
		t.Fatalf("unavailable recovery journal returned %v", err)
	}
	record, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != workspace.Version {
		t.Fatalf("journal failure persisted recovery version %d", record.Version)
	}
}

func TestChangePassphraseRequiresNewPurposeBoundFactor(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, _ := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	base := &tammyv1.ChangePassphraseRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073913", Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073912"},
	}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
		CurrentPassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		NewPassphrase:     &tammyv1.SecretInput{Utf8: []byte("replacement-passphrase-long-enough")}}
	t.Run("missing factor", func(t *testing.T) {
		if _, err := harness.service.ChangePassphrase(ctx, connect.NewRequest(proto.Clone(base).(*tammyv1.ChangePassphraseRequest))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("missing factor returned %v", err)
		}
	})
	t.Run("stale factor", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.ChangePassphraseRequest)
		request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073914", "change_passphrase", harness.now.Add(-5*time.Minute))
		if _, err := harness.service.ChangePassphrase(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("stale factor returned %v", err)
		}
	})
	t.Run("marker reserved for another action", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.ChangePassphraseRequest)
		request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073915", "ownership_transfer", *harness.now)
		if _, err := harness.service.ChangePassphrase(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("wrong-purpose factor returned %v", err)
		}
	})
	t.Run("newly asserted factor", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.ChangePassphraseRequest)
		request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073916", "change_passphrase", *harness.now)
		response, err := harness.service.ChangePassphrase(ctx, connect.NewRequest(request))
		if err != nil {
			t.Fatal(err)
		}
		if response.Msg.Workspace.Version != workspace.Version+1 {
			t.Fatalf("version = %d", response.Msg.Workspace.Version)
		}
	})
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("old passphrase returned %v", err)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("replacement-passphrase-long-enough", false))); err != nil {
		t.Fatal(err)
	}
}

func TestChangePassphraseCrashRecoveryBoundaries(t *testing.T) {
	testCases := []struct {
		name       string
		checkpoint string
		operation  string
		marker     string
		committed  bool
	}{
		{name: "before database audit commit", checkpoint: "change_passphrase.before_db_commit", operation: "01890f3c-7b2e-7cc4-98c4-dc0c0c073940", marker: "01890f3c-7b2e-7cc4-98c4-dc0c0c073943"},
		{name: "before slot activation", checkpoint: "change_passphrase.before_slot_activation", operation: "01890f3c-7b2e-7cc4-98c4-dc0c0c073941", marker: "01890f3c-7b2e-7cc4-98c4-dc0c0c073944", committed: true},
		{name: "after slot activation", checkpoint: "change_passphrase.after_slot_activation", operation: "01890f3c-7b2e-7cc4-98c4-dc0c0c073942", marker: "01890f3c-7b2e-7cc4-98c4-dc0c0c073945", committed: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			ctx := context.Background()
			workspace, _ := createConfirmedWorkspace(t, harness)
			injected := errors.New("injected crash boundary")
			harness.failures.failures[testCase.checkpoint] = injected
			request := &tammyv1.ChangePassphraseRequest{CommandContext: &tammyv1.CommandContext{
				IdempotencyKey: testCase.operation,
				Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073912"},
				FreshFactor:    harness.identity.issueFactor(testCase.marker, "change_passphrase", *harness.now),
			}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
				CurrentPassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
				NewPassphrase:     &tammyv1.SecretInput{Utf8: []byte("crash-recovered-passphrase-long-enough")}}
			if _, err := harness.service.ChangePassphrase(ctx, connect.NewRequest(request)); !errors.Is(err, injected) {
				t.Fatalf("checkpoint returned %v", err)
			}
			if got, want := harness.audit.counts["PASSPHRASE_CHANGE"], 0; testCase.committed {
				want = 1
				if got != want {
					t.Fatalf("audit count = %d, want %d", got, want)
				}
			} else if got != want {
				t.Fatalf("audit count = %d, want %d", got, want)
			}
			if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
				t.Fatal(err)
			}
			if testCase.committed {
				if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("crash-recovered-passphrase-long-enough", false))); err != nil {
					t.Fatalf("committed slot was not elected: %v", err)
				}
			} else {
				if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false))); err != nil {
					t.Fatalf("uncommitted slot replaced prior passphrase: %v", err)
				}
				request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073946", "change_passphrase", *harness.now)
			}
			response, err := harness.service.ChangePassphrase(ctx, connect.NewRequest(request))
			if err != nil || response.Msg.Workspace.Version != workspace.Version+1 {
				t.Fatalf("crash replay: %v", err)
			}
			if harness.audit.counts["PASSPHRASE_CHANGE"] != 1 {
				t.Fatalf("replay audit count = %d", harness.audit.counts["PASSPHRASE_CHANGE"])
			}
		})
	}
}

func TestWorkspaceServiceBreakGlassRecoverySetsNewPassphrase(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, recovery := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	recovered, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(&tammyv1.RecoverWorkspaceRequest{
		RecoveryOperationId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073917", WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		RecoverySecret: &tammyv1.SecretInput{Utf8: recovery}, NewPassphrase: &tammyv1.SecretInput{Utf8: []byte("recovered-passphrase-long-enough")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED || recovered.Msg.Workspace.Version != workspace.Version+1 {
		t.Fatalf("unexpected recovery: %v", recovered.Msg.Workspace)
	}
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("old passphrase after recovery returned %v", err)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("recovered-passphrase-long-enough", false))); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRejectsMissingCorruptOrMismatchedAuthoritativeRecord(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*testing.T, StorageHandle, workspaceRecord) []byte
	}{
		{name: "missing", mutate: func(t *testing.T, storage StorageHandle, _ workspaceRecord) []byte {
			t.Helper()
			if _, err := storage.Database().ExecContext(context.Background(),
				`DELETE FROM workspace_metadata WHERE key = ?`, authoritativeWorkspaceRecordKey); err != nil {
				t.Fatal(err)
			}
			return nil
		}},
		{name: "corrupt", mutate: func(t *testing.T, storage StorageHandle, _ workspaceRecord) []byte {
			t.Helper()
			payload := []byte("{")
			if _, err := storage.Database().ExecContext(context.Background(),
				`UPDATE workspace_metadata SET value = ?, revision = revision + 1 WHERE key = ?`, payload, authoritativeWorkspaceRecordKey); err != nil {
				t.Fatal(err)
			}
			return payload
		}},
		{name: "mismatched workspace id", mutate: func(t *testing.T, storage StorageHandle, record workspaceRecord) []byte {
			t.Helper()
			record.ID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073999"
			payload, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := storage.Database().ExecContext(context.Background(),
				`UPDATE workspace_metadata SET value = ?, revision = revision + 1 WHERE key = ?`, payload, authoritativeWorkspaceRecordKey); err != nil {
				t.Fatal(err)
			}
			return payload
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHarness(t)
			workspace, recovery := createConfirmedWorkspace(t, harness)
			ctx := context.Background()
			runtime := harness.service.active[workspace.Id]
			dek := append([]byte(nil), runtime.dek...)
			if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
				t.Fatal(err)
			}
			defer Zero(dek)
			catalogueBefore, err := harness.repository.ByID(ctx, workspace.Id)
			if err != nil {
				t.Fatal(err)
			}
			cataloguePayloadBefore, err := json.Marshal(catalogueBefore)
			if err != nil {
				t.Fatal(err)
			}
			storage, err := harness.service.storage.Open(ctx, catalogueBefore.DatabasePath, dek)
			if err != nil {
				t.Fatal(err)
			}
			defer storage.Close()
			authoritative, err := storage.LoadWorkspaceRecord(ctx)
			if err != nil {
				t.Fatal(err)
			}
			originalPayload, err := json.Marshal(authoritative)
			if err != nil {
				t.Fatal(err)
			}
			headerBefore, err := NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
			if err != nil {
				t.Fatal(err)
			}
			slotsBefore, err := headerBefore.Slots()
			headerBefore.Close()
			if err != nil {
				t.Fatal(err)
			}
			mutatedPayload := testCase.mutate(t, storage, authoritative)
			request := &tammyv1.RecoverWorkspaceRequest{
				RecoveryOperationId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073917",
				WorkspaceFile:       &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
				RecoverySecret:      &tammyv1.SecretInput{Utf8: recovery},
				NewPassphrase:       &tammyv1.SecretInput{Utf8: []byte("strictly-bound-recovery-passphrase-long-enough")},
			}
			if _, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(request)); err == nil {
				t.Fatal("recovery accepted an unbound authoritative record")
			}
			catalogueAfter, err := harness.repository.ByID(ctx, workspace.Id)
			if err != nil {
				t.Fatal(err)
			}
			cataloguePayloadAfter, err := json.Marshal(catalogueAfter)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(cataloguePayloadBefore, cataloguePayloadAfter) || harness.audit.counts["RECOVERY"] != 0 {
				t.Fatalf("rejected recovery changed catalogue/audit: catalogue=%t audit=%d",
					bytes.Equal(cataloguePayloadBefore, cataloguePayloadAfter), harness.audit.counts["RECOVERY"])
			}
			if _, active := harness.service.active[workspace.Id]; active {
				t.Fatal("rejected recovery retained an active runtime")
			}
			var count int
			if err := storage.Database().QueryRowContext(ctx,
				`SELECT count(*) FROM workspace_metadata WHERE key = ?`, authoritativeWorkspaceRecordKey).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if mutatedPayload == nil {
				if count != 0 {
					t.Fatal("rejected recovery recreated the missing authoritative record")
				}
			} else {
				var payload []byte
				if count != 1 || storage.Database().QueryRowContext(ctx,
					`SELECT value FROM workspace_metadata WHERE key = ?`, authoritativeWorkspaceRecordKey).Scan(&payload) != nil ||
					!bytes.Equal(payload, mutatedPayload) {
					t.Fatal("rejected recovery overwrote the invalid authoritative record")
				}
			}
			headerAfter, err := NewHeaderStore(catalogueBefore.HeaderPath, harness.service.headerAuthKey)
			if err != nil {
				t.Fatal(err)
			}
			slotsAfter, err := headerAfter.Slots()
			headerAfter.Close()
			if err != nil || !reflect.DeepEqual(slotsBefore, slotsAfter) {
				t.Fatalf("rejected recovery changed header slots: %v", err)
			}
			if _, err := storage.Database().ExecContext(ctx, `INSERT INTO workspace_metadata(key, value, revision, updated_at)
				VALUES (?, ?, 1, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value,
				revision = workspace_metadata.revision + 1, updated_at = excluded.updated_at`,
				authoritativeWorkspaceRecordKey, originalPayload, harness.now.UTC().Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			if _, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(request)); err != nil {
				t.Fatalf("recovery after authoritative repair: %v", err)
			}
		})
	}
}

func TestRecoveryRechecksAuthoritativeOperationStateInsideTransaction(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, recovery := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	runtime := harness.service.active[workspace.Id]
	dek := append([]byte(nil), runtime.dek...)
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	defer Zero(dek)
	catalogueBefore, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := harness.service.storage.Open(ctx, catalogueBefore.DatabasePath, dek)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	operationID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073918"
	harness.failures.hooks["recovery.before_db_commit"] = func() {
		authoritative, loadErr := storage.LoadWorkspaceRecord(ctx)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if authoritative.OperationHashes == nil {
			authoritative.OperationHashes = make(map[string]string)
		}
		authoritative.OperationHashes[operationID] = "concurrent-authoritative-operation"
		overwriteAuthoritativeWorkspaceRecord(t, &workspaceRuntime{storage: storage}, authoritative)
	}
	request := &tammyv1.RecoverWorkspaceRequest{
		RecoveryOperationId: operationID,
		WorkspaceFile:       &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		RecoverySecret:      &tammyv1.SecretInput{Utf8: recovery},
		NewPassphrase:       &tammyv1.SecretInput{Utf8: []byte("transaction-rechecked-recovery-passphrase-long-enough")},
	}
	if _, err := harness.service.RecoverWorkspace(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeStaleVersion, nil)) {
		t.Fatalf("authoritative operation race returned %v", err)
	}
	catalogueAfter, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(catalogueBefore, catalogueAfter) || harness.audit.counts["RECOVERY"] != 0 {
		t.Fatalf("operation race changed catalogue/audit: catalogue=%t audit=%d",
			reflect.DeepEqual(catalogueBefore, catalogueAfter), harness.audit.counts["RECOVERY"])
	}
	if _, active := harness.service.active[workspace.Id]; active {
		t.Fatal("operation race retained an active runtime")
	}
}

func TestWorkspaceServiceBreakGlassAdministratorRecoveryClosesWorkspace(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, recovery := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	user, err := harness.service.RecoverAdministrator(ctx, &tammyv1.RecoverAdministratorRequest{
		RecoveryOperationId:    "01890f3c-7b2e-7cc4-98c4-dc0c0c073933",
		WorkspaceFile:          &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		RecoverySecret:         &tammyv1.SecretInput{Utf8: recovery},
		AdministratorUsername:  "admin@example.test",
		NewWorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("recovered-workspace-passphrase-long-enough")},
		NewUserPassword:        &tammyv1.SecretInput{Utf8: []byte("recovered-admin-password-long-enough")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.State != tammyv1.UserState_USER_STATE_ACTIVE || !*harness.adminRecovered {
		t.Fatalf("administrator was not reset: %v", user)
	}
	record, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("state = %v, want locked", record.State)
	}
	if _, active := harness.service.active[workspace.Id]; active {
		t.Fatal("administrator recovery retained the workspace DEK")
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("recovered-workspace-passphrase-long-enough", false))); err != nil {
		t.Fatal(err)
	}
	var operationKind string
	if err := harness.service.active[workspace.Id].storage.Database().QueryRowContext(ctx,
		`SELECT operation_kind FROM header_operation_ids WHERE operation_id = ?`,
		"01890f3c-7b2e-7cc4-98c4-dc0c0c073933").Scan(&operationKind); err != nil {
		t.Fatal(err)
	}
	if operationKind != "ADMIN_RECOVERY" {
		t.Fatalf("operation kind = %q", operationKind)
	}
}

func TestWorkspaceServiceAdministratorResetFailureRollsBackRecovery(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, recovery := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	request := &tammyv1.RecoverAdministratorRequest{
		RecoveryOperationId:    "01890f3c-7b2e-7cc4-98c4-dc0c0c073948",
		WorkspaceFile:          &tammyv1.ApprovedFileRef{CapabilityId: "workspace-file-capability"},
		RecoverySecret:         &tammyv1.SecretInput{Utf8: recovery},
		AdministratorUsername:  "admin@example.test",
		NewWorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte("transactional-recovery-passphrase-long-enough")},
		NewUserPassword:        &tammyv1.SecretInput{Utf8: []byte("transactional-admin-password-long-enough")},
	}
	resetErr := errors.New("administrator reset failed")
	harness.identity.adminRecoveryErr = resetErr
	if _, err := harness.service.RecoverAdministrator(ctx, request); !errors.Is(err, resetErr) {
		t.Fatalf("administrator reset failure returned %v", err)
	}
	if *harness.adminRecovered || harness.audit.counts["ADMIN_RECOVERY"] != 0 {
		t.Fatalf("failed reset side effects: reset=%t audit=%d", *harness.adminRecovered, harness.audit.counts["ADMIN_RECOVERY"])
	}
	record, err := harness.repository.ByID(ctx, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != workspace.Version || record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("failed reset persisted workspace mutation: %+v", record)
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("workspace-passphrase-long-enough", false))); err != nil {
		t.Fatalf("failed reset replaced the prior passphrase: %v", err)
	}
	if _, err := harness.service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		t.Fatal(err)
	}
	harness.identity.adminRecoveryErr = nil
	user, err := harness.service.RecoverAdministrator(ctx, request)
	if err != nil {
		t.Fatalf("retry after rolled-back reset: %v", err)
	}
	if user.State != tammyv1.UserState_USER_STATE_ACTIVE || !*harness.adminRecovered || harness.audit.counts["ADMIN_RECOVERY"] != 1 {
		t.Fatalf("retry side effects: user=%v reset=%t audit=%d", user, *harness.adminRecovered, harness.audit.counts["ADMIN_RECOVERY"])
	}
	if _, err := harness.service.UnlockWorkspace(ctx, connect.NewRequest(unlockRequest("transactional-recovery-passphrase-long-enough", false))); err != nil {
		t.Fatalf("retry did not install recovered passphrase: %v", err)
	}
}

func factorMarker(id, purpose string, asserted time.Time) *tammyv1.FreshFactorContext {
	return &tammyv1.FreshFactorContext{AssertionId: id, Purpose: purpose, AssertedAt: timestamppb.New(asserted)}
}

func TestTransferOwnershipRequiresNewPurposeBoundFactor(t *testing.T) {
	harness := newWorkspaceHarness(t)
	workspace, _ := createConfirmedWorkspace(t, harness)
	ctx := context.Background()
	acknowledged := true
	base := &tammyv1.TransferOwnershipRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073918", Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073912"},
	}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
		TargetUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073919", AcknowledgeVerificationEffect: &acknowledged}
	t.Run("missing factor", func(t *testing.T) {
		if _, err := harness.service.TransferOwnership(ctx, connect.NewRequest(proto.Clone(base).(*tammyv1.TransferOwnershipRequest))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("missing factor returned %v", err)
		}
	})
	t.Run("stale factor", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.TransferOwnershipRequest)
		request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073920", "ownership_transfer", harness.now.Add(-5*time.Minute))
		if _, err := harness.service.TransferOwnership(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("stale factor returned %v", err)
		}
	})
	t.Run("marker reserved for another action", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.TransferOwnershipRequest)
		request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073921", "change_passphrase", *harness.now)
		if _, err := harness.service.TransferOwnership(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("wrong-purpose factor returned %v", err)
		}
	})
	t.Run("unresolved organisation transmission", func(t *testing.T) {
		dependencyErr := errors.New("unresolved transmission")
		harness.organisations.fail = dependencyErr
		request := proto.Clone(base).(*tammyv1.TransferOwnershipRequest)
		request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073939", "ownership_transfer", *harness.now)
		if _, err := harness.service.TransferOwnership(ctx, connect.NewRequest(request)); !errors.Is(err, dependencyErr) {
			t.Fatalf("organisation guard returned %v", err)
		}
		record, err := harness.repository.ByID(ctx, workspace.Id)
		if err != nil {
			t.Fatal(err)
		}
		if record.OwnerUserID != "01890f3c-7b2e-7cc4-98c4-dc0c0c073910" || record.Version != workspace.Version {
			t.Fatalf("failed transfer persisted ownership: %+v", record)
		}
		if *harness.invalidations != 0 || harness.identity.assertions[request.CommandContext.FreshFactor.AssertionId].consumed {
			t.Fatal("failed transfer consumed authentication state")
		}
		harness.organisations.fail = nil
	})
	t.Run("newly asserted factor", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.TransferOwnershipRequest)
		request.CommandContext.FreshFactor = harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073922", "ownership_transfer", *harness.now)
		response, err := harness.service.TransferOwnership(ctx, connect.NewRequest(request))
		if err != nil {
			t.Fatal(err)
		}
		if response.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED || response.Msg.Workspace.Version != workspace.Version+1 {
			t.Fatalf("unexpected transfer: %v", response.Msg.Workspace)
		}
		if harness.organisations.calls != 1 || harness.audit.counts["OWNERSHIP_TRANSFER"] != 1 {
			t.Fatalf("transfer dependencies: organisations=%d audit=%d", harness.organisations.calls, harness.audit.counts["OWNERSHIP_TRANSFER"])
		}
	})
}

func TestSensitiveWorkspaceReplayRequiresAuthentication(t *testing.T) {
	t.Run("passphrase change", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		workspace, _ := createConfirmedWorkspace(t, harness)
		request := &tammyv1.ChangePassphraseRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073958",
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073959"},
			FreshFactor:    harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073960", "change_passphrase", *harness.now),
		}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
			CurrentPassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
			NewPassphrase:     &tammyv1.SecretInput{Utf8: []byte("authenticated-replay-passphrase-long-enough")}}
		if _, err := harness.service.ChangePassphrase(context.Background(), connect.NewRequest(request)); err != nil {
			t.Fatal(err)
		}
		replay := proto.Clone(request).(*tammyv1.ChangePassphraseRequest)
		replay.CommandContext.Authentication = nil
		replay.CommandContext.FreshFactor = nil
		if _, err := harness.service.ChangePassphrase(context.Background(), connect.NewRequest(replay)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("unauthenticated passphrase replay returned %v", err)
		}
		replay.CommandContext.Authentication = &tammyv1.AuthenticationContext{
			ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073919", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073957"}
		if _, err := harness.service.ChangePassphrase(context.Background(), connect.NewRequest(replay)); !errors.Is(err, faults.New(faults.CodePermissionDenied, nil)) {
			t.Fatalf("cross-actor passphrase replay returned %v", err)
		}
	})

	t.Run("ownership transfer", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		workspace, _ := createConfirmedWorkspace(t, harness)
		acknowledged := true
		request := &tammyv1.TransferOwnershipRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073961",
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073962"},
			FreshFactor:    harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073963", "ownership_transfer", *harness.now),
		}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
			TargetUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073919", AcknowledgeVerificationEffect: &acknowledged}
		if _, err := harness.service.TransferOwnership(context.Background(), connect.NewRequest(request)); err != nil {
			t.Fatal(err)
		}
		replay := proto.Clone(request).(*tammyv1.TransferOwnershipRequest)
		replay.CommandContext.Authentication = nil
		replay.CommandContext.FreshFactor = nil
		if _, err := harness.service.TransferOwnership(context.Background(), connect.NewRequest(replay)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("unauthenticated ownership replay returned %v", err)
		}
		replay.CommandContext.Authentication = &tammyv1.AuthenticationContext{
			ActorUserId: request.TargetUserId, SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073964"}
		if _, err := harness.service.TransferOwnership(context.Background(), connect.NewRequest(replay)); !errors.Is(err, faults.New(faults.CodePermissionDenied, nil)) {
			t.Fatalf("cross-actor ownership replay returned %v", err)
		}
	})
}

func TestWorkspaceHighRiskMutationRechecksAuthoritativeStateInTransaction(t *testing.T) {
	t.Run("passphrase change", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		workspace, _ := createConfirmedWorkspace(t, harness)
		runtime := harness.service.active[workspace.Id]
		harness.failures.hooks["change_passphrase.before_db_commit"] = func() {
			record, err := runtime.storage.LoadWorkspaceRecord(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			record.Version++
			overwriteAuthoritativeWorkspaceRecord(t, runtime, record)
		}
		request := &tammyv1.ChangePassphraseRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073965",
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073966"},
			FreshFactor:    harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073967", "change_passphrase", *harness.now),
		}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
			CurrentPassphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
			NewPassphrase:     &tammyv1.SecretInput{Utf8: []byte("transaction-race-passphrase-long-enough")}}
		if _, err := harness.service.ChangePassphrase(context.Background(), connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeStaleVersion, nil)) {
			t.Fatalf("passphrase race returned %v", err)
		}
	})

	t.Run("ownership transfer", func(t *testing.T) {
		harness := newWorkspaceHarness(t)
		workspace, _ := createConfirmedWorkspace(t, harness)
		runtime := harness.service.active[workspace.Id]
		harness.failures.hooks["ownership_transfer.before_db_commit"] = func() {
			record, err := runtime.storage.LoadWorkspaceRecord(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			record.Version++
			record.OwnerUserID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073918"
			overwriteAuthoritativeWorkspaceRecord(t, runtime, record)
		}
		acknowledged := true
		request := &tammyv1.TransferOwnershipRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073968",
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073910", SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073969"},
			FreshFactor:    harness.identity.issueFactor("01890f3c-7b2e-7cc4-98c4-dc0c0c073970", "ownership_transfer", *harness.now),
		}, WorkspaceId: workspace.Id, ExpectedVersion: workspace.Version,
			TargetUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073919", AcknowledgeVerificationEffect: &acknowledged}
		if _, err := harness.service.TransferOwnership(context.Background(), connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeStaleVersion, nil)) {
			t.Fatalf("ownership race returned %v", err)
		}
	})
}

func overwriteAuthoritativeWorkspaceRecord(t *testing.T, runtime *workspaceRuntime, record workspaceRecord) {
	t.Helper()
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.storage.Database().ExecContext(context.Background(),
		`UPDATE workspace_metadata SET value = ?, revision = revision + 1 WHERE key = ?`, payload, authoritativeWorkspaceRecordKey); err != nil {
		t.Fatal(err)
	}
}
