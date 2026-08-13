//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package identity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type switchingWorkspaceIdentity struct{ current *Service }

func (bridge *switchingWorkspaceIdentity) BootstrapAdministrator(ctx context.Context, username, displayName string, password []byte) (*tammyv1.User, error) {
	return bridge.current.BootstrapAdministrator(ctx, username, displayName, password)
}
func (bridge *switchingWorkspaceIdentity) BootstrapAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, operationID, username, displayName string, password []byte) (*tammyv1.User, error) {
	return bridge.current.BootstrapAdministratorWithin(ctx, executor, operationID, username, displayName, password)
}
func (bridge *switchingWorkspaceIdentity) BreakGlassResetAdministrator(ctx context.Context, operationID, username string, password []byte) (*tammyv1.User, error) {
	return bridge.current.BreakGlassResetAdministrator(ctx, operationID, username, password)
}
func (bridge *switchingWorkspaceIdentity) BreakGlassResetAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, operationID, username string, password []byte) (*tammyv1.User, error) {
	return bridge.current.BreakGlassResetAdministratorWithin(ctx, executor, operationID, username, password)
}
func (bridge *switchingWorkspaceIdentity) RequireAdministrator(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	return bridge.current.RequireAdministrator(ctx, authentication)
}
func (bridge *switchingWorkspaceIdentity) RequireAdministratorReadOnly(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	return bridge.current.RequireAdministratorReadOnly(ctx, authentication)
}
func (bridge *switchingWorkspaceIdentity) ValidateAdministratorReplayBinding(ctx context.Context, authentication *tammyv1.AuthenticationContext,
	boundActorUserID, boundSessionID string) error {
	return bridge.current.ValidateAdministratorReplayBinding(ctx, authentication, boundActorUserID, boundSessionID)
}
func (bridge *switchingWorkspaceIdentity) RequireAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, authentication *tammyv1.AuthenticationContext) error {
	return bridge.current.RequireAdministratorWithin(ctx, executor, authentication)
}
func (bridge *switchingWorkspaceIdentity) RequireActiveSessionReadOnly(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	return bridge.current.RequireActiveSessionReadOnly(ctx, authentication)
}
func (bridge *switchingWorkspaceIdentity) RequireActiveSessionWithin(ctx context.Context, executor workspace.MutationExecutor,
	authentication *tammyv1.AuthenticationContext) error {
	return bridge.current.RequireActiveSessionWithin(ctx, executor, authentication)
}
func (bridge *switchingWorkspaceIdentity) ConsumeFreshFactor(ctx context.Context, authentication *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, purpose string) error {
	return bridge.current.ConsumeFreshFactor(ctx, authentication, factor, purpose)
}
func (bridge *switchingWorkspaceIdentity) ConsumeFreshFactorWithin(ctx context.Context, executor workspace.MutationExecutor, authentication *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, purpose string) error {
	return bridge.current.ConsumeFreshFactorWithin(ctx, executor, authentication, factor, purpose)
}
func (bridge *switchingWorkspaceIdentity) IsActiveAdministrator(ctx context.Context, userID string) bool {
	return bridge.current.IsActiveAdministrator(ctx, userID)
}
func (bridge *switchingWorkspaceIdentity) IsActiveAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, userID string) (bool, error) {
	return bridge.current.IsActiveAdministratorWithin(ctx, executor, userID)
}
func (bridge *switchingWorkspaceIdentity) InvalidateAllSessions(ctx context.Context) error {
	return bridge.current.InvalidateAllSessions(ctx)
}
func (bridge *switchingWorkspaceIdentity) InvalidateAllSessionsWithin(ctx context.Context, executor workspace.MutationExecutor) error {
	return bridge.current.InvalidateAllSessionsWithin(ctx, executor)
}

type integrationCapabilities struct{ directory, database string }

func (capabilities integrationCapabilities) Resolve(_ context.Context, reference *tammyv1.ApprovedFileRef, kind workspace.CapabilityKind) (string, error) {
	if reference == nil {
		return "", errors.New("missing capability")
	}
	switch kind {
	case workspace.CapabilityDirectory:
		return capabilities.directory, nil
	case workspace.CapabilityWorkspaceFile:
		return capabilities.database, nil
	default:
		return "", errors.New("unsupported capability")
	}
}

type integrationWorkspaceAudit struct {
	counts map[string]int
	fail   error
}

func (audit *integrationWorkspaceAudit) AppendWorkspaceMutation(_ context.Context, _ workspace.MutationExecutor, mutation workspace.WorkspaceMutation) error {
	if audit.fail != nil {
		return audit.fail
	}
	audit.counts[mutation.Kind]++
	return nil
}

type integrationOrganisationImpact struct {
	calls int
	fail  error
}

func (impact *integrationOrganisationImpact) ApplyOwnershipTransfer(context.Context, workspace.MutationExecutor, workspace.OwnershipImpact) error {
	if impact.fail != nil {
		return impact.fail
	}
	impact.calls++
	return nil
}

type integrationFailureCheckpoints struct{ failures map[string]error }

func (checkpoints *integrationFailureCheckpoints) Check(name string) error {
	return checkpoints.failures[name]
}

type workspaceSessionLifecycle struct{ service *workspace.Service }

func (lifecycle workspaceSessionLifecycle) SessionStartedWithin(ctx context.Context, executor workspace.MutationExecutor, sessionID string) error {
	return lifecycle.service.SessionStartedWithin(ctx, executor, sessionID)
}
func (lifecycle workspaceSessionLifecycle) SessionStartedAuditedWithin(ctx context.Context, executor workspace.MutationExecutor) error {
	return lifecycle.service.SessionStartedAuditedWithin(ctx, executor)
}
func (lifecycle workspaceSessionLifecycle) SessionStartedCommitted(ctx context.Context) error {
	return lifecycle.service.SessionStartedCommitted(ctx)
}

func TestRealWorkspaceAndIdentitySessionLifecycle(t *testing.T) {
	for _, testCase := range []struct {
		name string
		end  func(context.Context, identityHarness, *tammyv1.SignInResponse) error
	}{
		{name: "sign out", end: func(ctx context.Context, harness identityHarness, signedIn *tammyv1.SignInResponse) error {
			_, err := harness.service.SignOut(ctx, connect.NewRequest(&tammyv1.SignOutRequest{Authentication: &tammyv1.AuthenticationContext{
				ActorUserId: signedIn.User.Id, SessionId: signedIn.Session.Id,
			}}))
			return err
		}},
		{name: "thirty minute inactivity", end: func(ctx context.Context, harness identityHarness, _ *tammyv1.SignInResponse) error {
			*harness.now = harness.now.Add(24 * time.Minute)
			return harness.service.ExpireIdle(ctx)
		}},
		{name: "operating system lock", end: func(ctx context.Context, harness identityHarness, _ *tammyv1.SignInResponse) error {
			return harness.service.HandleOSLock(ctx)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			bootstrap := newIdentityHarness(t)
			bridge := &switchingWorkspaceIdentity{current: bootstrap.service}
			directory := t.TempDir()
			databasePath := filepath.Join(directory, "tammy-workspace.db")
			workspaceAttempts, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "workspace-attempts.journal"), bytes.Repeat([]byte{3}, 32),
				bootstrap.config.Clock, "workspace/session-integration", workspace.NewMemoryAnchorStore())
			if err != nil {
				t.Fatal(err)
			}
			remembered, err := workspace.NewRememberedKeyManager(workspace.NewMemorySecretStore(), bootstrap.config.Clock)
			if err != nil {
				t.Fatal(err)
			}
			headerKey := bytes.Repeat([]byte{4}, 32)
			workspaceService, err := workspace.NewService(workspace.Config{
				Repository: workspace.NewMemoryRepository(), Capabilities: integrationCapabilities{directory: directory, database: databasePath},
				Storage: workspace.NewSQLCipherStorageFactory(2), Identity: bridge, Audit: &integrationWorkspaceAudit{counts: make(map[string]int)},
				OrganisationImpact: &integrationOrganisationImpact{}, Passwords: bootstrap.config.Passwords, RememberedKeys: remembered,
				Attempts: workspaceAttempts, Clock: bootstrap.config.Clock, IDs: bootstrap.config.IDs,
				HeaderAuthenticationKey: headerKey, InstallationKey: bytes.Repeat([]byte{5}, 32),
			})
			if err != nil {
				t.Fatal(err)
			}
			created, err := workspaceService.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
				SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073991", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "directory"},
				WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
				AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
				AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
			}))
			if err != nil {
				t.Fatal(err)
			}
			groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := workspaceService.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073991",
				Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
			})); err != nil {
				t.Fatal(err)
			}

			header, err := workspace.NewHeaderStore(databasePath+".header", headerKey)
			if err != nil {
				t.Fatal(err)
			}
			slots, err := header.Slots()
			header.Close()
			if err != nil {
				t.Fatal(err)
			}
			dek, err := workspace.UnwrapWithPassphrase(bootstrap.config.Passwords, []byte("workspace-passphrase-long-enough"), slots[0].PassphraseWrap,
				created.Msg.Workspace.Id, slots[0].Version)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Zero(dek)
			storage, err := workspace.NewSQLCipherStorageFactory(2).Open(ctx, databasePath, dek)
			if err != nil {
				t.Fatal(err)
			}
			defer storage.Close()
			databaseRepository, err := NewDatabaseRepository(storage.Database())
			if err != nil {
				t.Fatal(err)
			}
			identityAudit := &recordingIdentityAudit{counts: make(map[string]int)}
			identityAttempts, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "identity-attempts.journal"), bytes.Repeat([]byte{7}, 32),
				bootstrap.config.Clock, "identity/session-integration", workspace.NewMemoryAnchorStore())
			if err != nil {
				t.Fatal(err)
			}
			var workspaceLockErr error
			identityConfig := bootstrap.config
			identityConfig.Repository = databaseRepository
			identityConfig.Attempts = identityAttempts
			identityConfig.Audit = identityAudit
			identityConfig.SessionLifecycle = workspaceSessionLifecycle{service: workspaceService}
			identityConfig.OnWorkspaceLock = func() {
				_, workspaceLockErr = workspaceService.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{}))
			}
			realIdentity, err := NewService(identityConfig)
			if err != nil {
				t.Fatal(err)
			}
			bridge.current = realIdentity
			realHarness := identityHarness{service: realIdentity, repository: databaseRepository, config: identityConfig, audit: identityAudit, now: bootstrap.now}
			signedIn, err := realIdentity.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: "admin@example.test",
				Password: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")}}))
			if err != nil {
				t.Fatal(err)
			}
			*bootstrap.now = bootstrap.now.Add(6 * time.Minute)
			if err := workspaceService.ExpireUnauthenticated(ctx); err != nil {
				t.Fatal(err)
			}
			state, err := workspaceService.GetWorkspaceState(ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &created.Msg.Workspace.Id}))
			if err != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
				t.Fatalf("workspace did not remain authenticated after pre-auth deadline: state=%v err=%v", state.Msg.Workspace, err)
			}
			if err := testCase.end(ctx, realHarness, signedIn.Msg); err != nil {
				t.Fatal(err)
			}
			if workspaceLockErr != nil {
				t.Fatal(workspaceLockErr)
			}
			state, err = workspaceService.GetWorkspaceState(ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &created.Msg.Workspace.Id}))
			if err != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
				t.Fatalf("terminal session event did not lock workspace: state=%v err=%v", state.Msg.Workspace, err)
			}
			authoritative, err := storage.LoadWorkspaceRecord(ctx)
			if err != nil || authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
				t.Fatalf("terminal session event left authoritative state=%v err=%v", authoritative.State, err)
			}
			if _, err := workspaceService.UnlockWorkspace(ctx, connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
				WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace"},
				Proof:         &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{Passphrase: &tammyv1.SecretInput{Utf8: []byte("wrong-passphrase-long-enough")}}},
			})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
				t.Fatalf("closed runtime did not require a fresh unlock proof: %v", err)
			}
			if _, err := workspaceService.UnlockWorkspace(ctx, connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
				WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace"},
				Proof:         &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")}}},
			})); err != nil {
				t.Fatal(err)
			}
			authoritative, err = storage.LoadWorkspaceRecord(ctx)
			if err != nil || authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
				t.Fatalf("fresh unlock left authoritative state=%v err=%v", authoritative.State, err)
			}
		})
	}
}

func TestRealWorkspaceProcessDeathRequiresProofAndFreshSignIn(t *testing.T) {
	ctx := context.Background()
	bootstrap := newIdentityHarness(t)
	bridge := &switchingWorkspaceIdentity{current: bootstrap.service}
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "tammy-workspace.db")
	cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.enc")
	catalogueKey := bytes.Repeat([]byte{5}, 32)
	repository, err := workspace.NewFileRepository(cataloguePath, catalogueKey)
	if err != nil {
		t.Fatal(err)
	}
	workspaceAttempts, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "workspace-attempts.journal"), bytes.Repeat([]byte{3}, 32),
		bootstrap.config.Clock, "workspace/process-death", workspace.NewMemoryAnchorStore())
	if err != nil {
		t.Fatal(err)
	}
	remembered, err := workspace.NewRememberedKeyManager(workspace.NewMemorySecretStore(), bootstrap.config.Clock)
	if err != nil {
		t.Fatal(err)
	}
	headerKey := bytes.Repeat([]byte{4}, 32)
	workspaceAudit := &integrationWorkspaceAudit{counts: make(map[string]int)}
	workspaceConfig := workspace.Config{
		Repository: repository, Capabilities: integrationCapabilities{directory: directory, database: databasePath},
		Storage: workspace.NewSQLCipherStorageFactory(2), Identity: bridge, Audit: workspaceAudit,
		OrganisationImpact: &integrationOrganisationImpact{}, Passwords: bootstrap.config.Passwords, RememberedKeys: remembered,
		Attempts: workspaceAttempts, Clock: bootstrap.config.Clock, IDs: bootstrap.config.IDs,
		HeaderAuthenticationKey: headerKey, InstallationKey: catalogueKey,
	}
	workspaceService, err := workspace.NewService(workspaceConfig)
	if err != nil {
		t.Fatal(err)
	}
	const setupID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073992"
	created, err := workspaceService.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId: setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: "directory"},
		WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername: "admin@example.test", AdministratorDisplayName: "Admin",
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceService.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId:       setupID,
		Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
	})); err != nil {
		t.Fatal(err)
	}

	header, err := workspace.NewHeaderStore(databasePath+".header", headerKey)
	if err != nil {
		t.Fatal(err)
	}
	slots, err := header.Slots()
	header.Close()
	if err != nil {
		t.Fatal(err)
	}
	dek, err := workspace.UnwrapWithPassphrase(bootstrap.config.Passwords, []byte("workspace-passphrase-long-enough"), slots[0].PassphraseWrap,
		created.Msg.Workspace.Id, slots[0].Version)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := workspace.NewSQLCipherStorageFactory(2).Open(ctx, databasePath, dek)
	workspace.Zero(dek)
	if err != nil {
		t.Fatal(err)
	}
	databaseRepository, err := NewDatabaseRepository(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	identityAudit := &recordingIdentityAudit{counts: make(map[string]int)}
	identityAttempts, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "identity-attempts.journal"), bytes.Repeat([]byte{7}, 32),
		bootstrap.config.Clock, "identity/process-death", workspace.NewMemoryAnchorStore())
	if err != nil {
		t.Fatal(err)
	}
	identityConfig := bootstrap.config
	identityConfig.Repository = databaseRepository
	identityConfig.Attempts = identityAttempts
	identityConfig.Audit = identityAudit
	identityConfig.SessionLifecycle = workspaceSessionLifecycle{service: workspaceService}
	activeWorkspaceService := workspaceService
	var terminalLockErr error
	identityConfig.OnWorkspaceLock = func() {
		_, terminalLockErr = activeWorkspaceService.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{}))
	}
	realIdentity, err := NewService(identityConfig)
	if err != nil {
		t.Fatal(err)
	}
	bridge.current = realIdentity
	signedIn, err := realIdentity.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: "admin@example.test", Password: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	failedLockAuthentication := &tammyv1.AuthenticationContext{ActorUserId: signedIn.Msg.User.Id, SessionId: signedIn.Msg.Session.Id}
	lockFailure := errors.New("identity audit unavailable during lock")
	identityAudit.fail = lockFailure
	if response, err := workspaceService.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); !errors.Is(err, lockFailure) || response != nil {
		t.Fatalf("failed lock response_present=%t err=%v", response != nil, err)
	}
	identityAudit.fail = nil
	rolledBack, err := databaseRepository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Sessions[failedLockAuthentication.SessionId].State != tammyv1.SessionState_SESSION_STATE_ACTIVE ||
		identityAudit.counts["sessions_invalidated"] != 0 {
		t.Fatalf("failed lock leaked session state=%v invalidation_audits=%d",
			rolledBack.Sessions[failedLockAuthentication.SessionId].State, identityAudit.counts["sessions_invalidated"])
	}
	if _, err := workspaceService.UnlockWorkspace(ctx, connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		}},
	})); err != nil {
		t.Fatalf("proved reopen after failed lock: %v", err)
	}
	if _, err := realIdentity.GetSession(ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: failedLockAuthentication})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("failed-lock session survived proved reopen: %v", err)
	}
	if identityAudit.counts["sessions_invalidated"] != 1 {
		t.Fatalf("failed-lock repair invalidation audits=%d", identityAudit.counts["sessions_invalidated"])
	}
	if workspaceAudit.counts["LOCK"] != 0 || workspaceAudit.counts["UNLOCK"] != 1 {
		t.Fatalf("failed-lock repair workspace audits lock=%d unlock=%d", workspaceAudit.counts["LOCK"], workspaceAudit.counts["UNLOCK"])
	}
	signedIn, err = realIdentity.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: "admin@example.test", Password: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}))
	if err != nil {
		t.Fatalf("fresh sign-in after failed-lock repair: %v", err)
	}
	oldAuthentication := &tammyv1.AuthenticationContext{ActorUserId: signedIn.Msg.User.Id, SessionId: signedIn.Msg.Session.Id}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a process disappearing without calling workspace Lock or either
	// service's Close method. Only installation catalogue/header/database files
	// survive; the replacement workspace service starts with an empty runtime.
	restartedRepository, err := workspace.NewFileRepository(cataloguePath, catalogueKey)
	if err != nil {
		t.Fatal(err)
	}
	restartBootstrap := newIdentityHarness(t)
	bridge.current = restartBootstrap.service
	workspaceConfig.Repository = restartedRepository
	restartedWorkspace, err := workspace.NewService(workspaceConfig)
	if err != nil {
		t.Fatal(err)
	}
	activeWorkspaceService = restartedWorkspace
	closed, err := restartedRepository.ByID(ctx, created.Msg.Workspace.Id)
	if err != nil || closed.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("restart catalogue state=%v err=%v", closed.State, err)
	}
	if _, err := restartedWorkspace.UnlockWorkspace(ctx, connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte("wrong-passphrase-long-enough")},
		}},
	})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("restart wrong proof returned %v", err)
	}
	if _, err := restartedWorkspace.UnlockWorkspace(ctx, connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		}},
	})); err != nil {
		t.Fatalf("restart correct proof: %v", err)
	}

	header, err = workspace.NewHeaderStore(databasePath+".header", headerKey)
	if err != nil {
		t.Fatal(err)
	}
	slots, err = header.Slots()
	header.Close()
	if err != nil {
		t.Fatal(err)
	}
	dek, err = workspace.UnwrapWithPassphrase(bootstrap.config.Passwords, []byte("workspace-passphrase-long-enough"), slots[0].PassphraseWrap,
		created.Msg.Workspace.Id, slots[0].Version)
	if err != nil {
		t.Fatal(err)
	}
	restartedStorage, err := workspace.NewSQLCipherStorageFactory(2).Open(ctx, databasePath, dek)
	workspace.Zero(dek)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedStorage.Close()
	restartedIdentityRepository, err := NewDatabaseRepository(restartedStorage.Database())
	if err != nil {
		t.Fatal(err)
	}
	identityConfig.Repository = restartedIdentityRepository
	identityConfig.SessionLifecycle = workspaceSessionLifecycle{service: restartedWorkspace}
	restartedIdentity, err := NewService(identityConfig)
	if err != nil {
		t.Fatal(err)
	}
	bridge.current = restartedIdentity
	if _, err := restartedIdentity.GetSession(ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: oldAuthentication})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("pre-crash session survived proved reopen: %v", err)
	}
	state, err := restartedWorkspace.GetWorkspaceState(ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &created.Msg.Workspace.Id}))
	if err != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
		t.Fatalf("proved reopen state=%v err=%v", state.Msg.Workspace, err)
	}
	freshSignedIn, err := restartedIdentity.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: "admin@example.test", Password: &tammyv1.SecretInput{Utf8: []byte("admin-password-long-enough")},
	}))
	if err != nil {
		t.Fatalf("fresh sign-in after proved reopen: %v", err)
	}
	createdUser, err := restartedIdentity.CreateUser(ctx, connect.NewRequest(&tammyv1.CreateUserRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073993", Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: freshSignedIn.Msg.User.Id, SessionId: freshSignedIn.Msg.Session.Id,
		}},
		Username: "activated@example.test", DisplayName: "Activated", Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restartedIdentity.SignOut(ctx, connect.NewRequest(&tammyv1.SignOutRequest{Authentication: &tammyv1.AuthenticationContext{
		ActorUserId: freshSignedIn.Msg.User.Id, SessionId: freshSignedIn.Msg.Session.Id,
	}})); err != nil || terminalLockErr != nil {
		t.Fatalf("pre-activation lock sign_out=%v lock=%v", err, terminalLockErr)
	}
	if _, err := restartedWorkspace.UnlockWorkspace(ctx, connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		}},
	})); err != nil {
		t.Fatal(err)
	}
	activationRequest := &tammyv1.ActivateUserRequest{
		Username: createdUser.Msg.User.Username, ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), createdUser.Msg.ActivationCode.Utf8...)},
		NewPassword: &tammyv1.SecretInput{Utf8: []byte("activated-password-long-enough")},
	}
	activationAuditFailure := errors.New("identity activation audit unavailable")
	identityAudit.fail = activationAuditFailure
	if response, err := restartedIdentity.ActivateUser(ctx, connect.NewRequest(activationRequest)); !errors.Is(err, activationAuditFailure) || response != nil {
		t.Fatalf("failed activation response_present=%t err=%v", response != nil, err)
	}
	identityAudit.fail = nil
	rolledBack, err = restartedIdentityRepository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rolledBackUser := rolledBack.Users[createdUser.Msg.User.Id]
	state, err = restartedWorkspace.GetWorkspaceState(ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &created.Msg.Workspace.Id}))
	if err != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED ||
		rolledBackUser == nil || rolledBackUser.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION ||
		rolledBackUser.ActivationSessionID != "" || identityAudit.counts["user_activated"] != 0 {
		t.Fatalf("activation rollback workspace=%v user=%+v audits=%d err=%v", state.Msg.Workspace, rolledBackUser,
			identityAudit.counts["user_activated"], err)
	}
	activated, err := restartedIdentity.ActivateUser(ctx, connect.NewRequest(activationRequest))
	if err != nil {
		t.Fatal(err)
	}
	replayedActivation, err := restartedIdentity.ActivateUser(ctx, connect.NewRequest(activationRequest))
	if err != nil || replayedActivation.Msg.Session.Id != activated.Msg.Session.Id || identityAudit.counts["user_activated"] != 1 {
		t.Fatalf("terminal activation replay session=%v audit=%d err=%v", replayedActivation, identityAudit.counts["user_activated"], err)
	}
	*bootstrap.now = bootstrap.now.Add(6 * time.Minute)
	if err := restartedWorkspace.ExpireUnauthenticated(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = restartedWorkspace.GetWorkspaceState(ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &created.Msg.Workspace.Id}))
	if err != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
		t.Fatalf("activation session did not survive pre-auth deadline: state=%v err=%v", state.Msg.Workspace, err)
	}
	terminalLockErr = nil
	if _, err := restartedIdentity.SignOut(ctx, connect.NewRequest(&tammyv1.SignOutRequest{Authentication: &tammyv1.AuthenticationContext{
		ActorUserId: activated.Msg.User.Id, SessionId: activated.Msg.Session.Id,
	}})); err != nil || terminalLockErr != nil {
		t.Fatalf("activation sign-out sign_out=%v lock=%v", err, terminalLockErr)
	}
	state, err = restartedWorkspace.GetWorkspaceState(ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &created.Msg.Workspace.Id}))
	if err != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("activation terminal event state=%v err=%v", state.Msg.Workspace, err)
	}
}

type sensitiveWorkspaceIntegration struct {
	ctx                context.Context
	workspace          *workspace.Service
	identity           *Service
	identityRepository *DatabaseRepository
	identityAudit      *sqlIdentityAudit
	workspaceAudit     *integrationWorkspaceAudit
	organisations      *integrationOrganisationImpact
	storage            workspace.StorageHandle
	catalogue          workspace.Repository
	remembered         *workspace.RememberedKeyManager
	secretStore        *workspace.MemorySecretStore
	dek                []byte
	now                *time.Time
	databasePath       string
	cataloguePath      string
	headerKey          []byte
	failures           *integrationFailureCheckpoints
	workspaceID        string
	workspaceVersion   uint64
	ownerID            string
	sessionID          string
	passphrase         string
	password           string
}

func newSensitiveWorkspaceIntegration(t *testing.T) *sensitiveWorkspaceIntegration {
	t.Helper()
	ctx := context.Background()
	bootstrap := newIdentityHarness(t)
	bridge := &switchingWorkspaceIdentity{current: bootstrap.service}
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "tammy-workspace.db")
	workspaceAttempts, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "workspace-attempts.journal"), bytes.Repeat([]byte{3}, 32),
		bootstrap.config.Clock, "workspace/sensitive-integration", workspace.NewMemoryAnchorStore())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := workspace.NewMemorySecretStore()
	remembered, err := workspace.NewRememberedKeyManager(secretStore, bootstrap.config.Clock)
	if err != nil {
		t.Fatal(err)
	}
	headerKey := bytes.Repeat([]byte{4}, 32)
	cataloguePath := filepath.Join(t.TempDir(), "workspace-catalogue.bin")
	installationKey := bytes.Repeat([]byte{5}, 32)
	catalogue, err := workspace.NewFileRepository(cataloguePath, installationKey)
	if err != nil {
		t.Fatal(err)
	}
	workspaceAudit := &integrationWorkspaceAudit{counts: make(map[string]int)}
	organisations := &integrationOrganisationImpact{}
	failures := &integrationFailureCheckpoints{failures: make(map[string]error)}
	workspaceService, err := workspace.NewService(workspace.Config{
		Repository: catalogue, Capabilities: integrationCapabilities{directory: directory, database: databasePath},
		Storage: workspace.NewSQLCipherStorageFactory(2), Identity: bridge, Audit: workspaceAudit, OrganisationImpact: organisations,
		FailureCheckpoints: failures,
		Passwords:          bootstrap.config.Passwords, RememberedKeys: remembered, Attempts: workspaceAttempts, Clock: bootstrap.config.Clock,
		IDs: bootstrap.config.IDs, HeaderAuthenticationKey: headerKey, InstallationKey: installationKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	const passphrase = "workspace-passphrase-long-enough"
	const password = "admin-password-long-enough"
	created, err := workspaceService.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073a01", Destination: &tammyv1.ApprovedFileRef{CapabilityId: "directory"},
		WorkspacePassphrase: &tammyv1.SecretInput{Utf8: []byte(passphrase)}, AdministratorUsername: "admin@example.test",
		AdministratorDisplayName: "Admin", AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte(password)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceService.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073a01", Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]},
		},
	})); err != nil {
		t.Fatal(err)
	}
	header, err := workspace.NewHeaderStore(databasePath+".header", headerKey)
	if err != nil {
		t.Fatal(err)
	}
	slots, err := header.Slots()
	header.Close()
	if err != nil {
		t.Fatal(err)
	}
	dek, err := workspace.UnwrapWithPassphrase(bootstrap.config.Passwords, []byte(passphrase), slots[0].PassphraseWrap,
		created.Msg.Workspace.Id, slots[0].Version)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := workspace.NewSQLCipherStorageFactory(2).Open(ctx, databasePath, dek)
	if err != nil {
		workspace.Zero(dek)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = storage.Close()
		workspace.Zero(dek)
	})
	databaseRepository, err := NewDatabaseRepository(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Database().ExecContext(ctx, `CREATE TABLE identity_test_audit(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		mutation TEXT NOT NULL,
		subject TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	identityAudit := &sqlIdentityAudit{database: storage.Database()}
	identityAttempts, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "identity-attempts.journal"), bytes.Repeat([]byte{7}, 32),
		bootstrap.config.Clock, "identity/sensitive-integration", workspace.NewMemoryAnchorStore())
	if err != nil {
		t.Fatal(err)
	}
	identityConfig := bootstrap.config
	identityConfig.Repository = databaseRepository
	identityConfig.Attempts = identityAttempts
	identityConfig.Audit = identityAudit
	identityConfig.SessionLifecycle = workspaceSessionLifecycle{service: workspaceService}
	realIdentity, err := NewService(identityConfig)
	if err != nil {
		t.Fatal(err)
	}
	bridge.current = realIdentity
	signedIn, err := realIdentity.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: "admin@example.test", Password: &tammyv1.SecretInput{Utf8: []byte(password)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return &sensitiveWorkspaceIntegration{
		ctx: ctx, workspace: workspaceService, identity: realIdentity, identityRepository: databaseRepository,
		identityAudit: identityAudit, workspaceAudit: workspaceAudit, organisations: organisations, storage: storage, catalogue: catalogue,
		remembered: remembered, secretStore: secretStore, dek: dek, now: bootstrap.now,
		databasePath: databasePath, cataloguePath: cataloguePath, headerKey: headerKey, failures: failures, workspaceID: created.Msg.Workspace.Id,
		workspaceVersion: created.Msg.Workspace.Version, ownerID: signedIn.Msg.User.Id, sessionID: signedIn.Msg.Session.Id,
		passphrase: passphrase, password: password,
	}
}

func (harness *sensitiveWorkspaceIntegration) authentication() *tammyv1.AuthenticationContext {
	return &tammyv1.AuthenticationContext{ActorUserId: harness.ownerID, SessionId: harness.sessionID}
}

func (harness *sensitiveWorkspaceIntegration) seedRememberedWorkspace(t *testing.T) {
	t.Helper()
	consent := true
	until, err := harness.remembered.Remember(harness.workspaceID, harness.dek, &consent)
	if err != nil {
		t.Fatal(err)
	}
	record, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
		t.Fatalf("remembered test workspace state=%v", record.State)
	}
	record.RememberedUntil = until.Unix()
	mutation := workspace.WorkspaceMutation{OperationID: "01890f3c-7b2e-7cc4-98c4-dc0c0c073bf4", Kind: "TEST_SEED_REMEMBERED",
		WorkspaceID: record.ID, Version: record.Version, SemanticHash: "test-seed-remembered"}
	if err := harness.storage.CommitWorkspaceMutation(harness.ctx, mutation, record, nil); err != nil {
		t.Fatal(err)
	}
	if err := harness.catalogue.Save(harness.ctx, record); err != nil {
		t.Fatal(err)
	}
}

func (harness *sensitiveWorkspaceIntegration) session(t *testing.T) sessionRecord {
	t.Helper()
	state, err := harness.identityRepository.Load(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	session := state.Sessions[harness.sessionID]
	if session == nil {
		t.Fatalf("session %q missing", harness.sessionID)
	}
	return *session
}

func (harness *sensitiveWorkspaceIntegration) factor(t *testing.T, purpose, enrolmentOperation string) *tammyv1.FreshFactorContext {
	t.Helper()
	identityHarness := identityHarness{service: harness.identity, repository: harness.identityRepository, now: harness.now}
	secret := enrolConfirmedFactor(t, identityHarness, harness.authentication(), harness.password, enrolmentOperation)
	*harness.now = harness.now.Add(30 * time.Second)
	asserted, err := harness.identity.AssertTOTP(harness.ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: harness.authentication(), Code: &tammyv1.TotpCodeInput{Value: TOTPCode(secret, *harness.now)}, Purpose: purpose,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return asserted.Msg.FreshFactor
}

func sameSessionTimes(left, right sessionRecord) bool {
	return left.LastActive.Equal(right.LastActive) && left.ExpiresAt.Equal(right.ExpiresAt) && left.EndedAt.Equal(right.EndedAt) && left.State == right.State
}

func TestRealForgetRememberedWorkspaceAllowsEveryActiveRoleAndReplaysPurely(t *testing.T) {
	for _, role := range []tammyv1.Role{
		tammyv1.Role_ROLE_WORKSPACE_ADMIN,
		tammyv1.Role_ROLE_BUSINESS_PREPARER,
		tammyv1.Role_ROLE_BUSINESS_LODGER,
		tammyv1.Role_ROLE_AUDITOR,
	} {
		t.Run(role.String(), func(t *testing.T) {
			harness := newSensitiveWorkspaceIntegration(t)
			state, err := harness.identityRepository.Load(harness.ctx)
			if err != nil {
				t.Fatal(err)
			}
			state.Users[harness.ownerID].Roles = []tammyv1.Role{role}
			if err := harness.identityRepository.Save(harness.ctx, state); err != nil {
				t.Fatal(err)
			}
			harness.seedRememberedWorkspace(t)
			beforeSession := harness.session(t)
			beforeTouches := identityAuditCount(t, harness.storage.Database(), "session_touched")
			*harness.now = harness.now.Add(time.Minute)
			request := &tammyv1.ForgetRememberedWorkspaceRequest{WorkspaceId: harness.workspaceID, Authentication: harness.authentication()}
			forgotten, err := harness.workspace.ForgetRememberedWorkspace(harness.ctx, connect.NewRequest(request))
			if err != nil {
				t.Fatal(err)
			}
			afterSession := harness.session(t)
			authoritative, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
			if err != nil {
				t.Fatal(err)
			}
			catalogue, err := harness.catalogue.ByID(harness.ctx, harness.workspaceID)
			if err != nil {
				t.Fatal(err)
			}
			if forgotten.Msg.Workspace.RememberedUntil != nil || authoritative.RememberedUntil != 0 || catalogue.RememberedUntil != 0 ||
				harness.secretStore.Count() != 0 || harness.workspaceAudit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 ||
				identityAuditCount(t, harness.storage.Database(), "session_touched") != beforeTouches+1 ||
				!afterSession.LastActive.Equal(*harness.now) || sameSessionTimes(beforeSession, afterSession) {
				t.Fatalf("role revoke did not converge response=%v sql_until=%d catalogue_until=%d vault=%d workspace_audit=%d touches=%d",
					forgotten.Msg.Workspace, authoritative.RememberedUntil, catalogue.RememberedUntil, harness.secretStore.Count(),
					harness.workspaceAudit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"],
					identityAuditCount(t, harness.storage.Database(), "session_touched"))
			}
			if _, _, err := harness.remembered.Use(harness.workspaceID); !errors.Is(err, workspace.ErrRememberedKeyUnavailable) {
				t.Fatalf("forgotten OS key remained usable: %v", err)
			}

			*harness.now = harness.now.Add(time.Minute)
			replayed, err := harness.workspace.ForgetRememberedWorkspace(harness.ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.ForgetRememberedWorkspaceRequest)))
			if err != nil || !proto.Equal(forgotten.Msg.Workspace, replayed.Msg.Workspace) {
				t.Fatalf("replay response=%v err=%v", replayed, err)
			}
			if !sameSessionTimes(afterSession, harness.session(t)) ||
				identityAuditCount(t, harness.storage.Database(), "session_touched") != beforeTouches+1 ||
				harness.workspaceAudit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 {
				t.Fatal("terminal replay touched session or duplicated audit")
			}
		})
	}
}

func TestRealForgetRememberedWorkspaceDeniesWrongSessionBeforeVault(t *testing.T) {
	harness := newSensitiveWorkspaceIntegration(t)
	harness.seedRememberedWorkspace(t)
	for name, authentication := range map[string]*tammyv1.AuthenticationContext{
		"missing": nil,
		"wrong actor": {
			ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073bf5", SessionId: harness.sessionID,
		},
		"wrong session": {
			ActorUserId: harness.ownerID, SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073bf6",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := harness.workspace.ForgetRememberedWorkspace(harness.ctx, connect.NewRequest(&tammyv1.ForgetRememberedWorkspaceRequest{
				WorkspaceId: harness.workspaceID, Authentication: authentication,
			})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
				t.Fatalf("denial returned %v", err)
			}
			if harness.secretStore.Count() != 1 {
				t.Fatalf("denial touched OS vault items=%d", harness.secretStore.Count())
			}
		})
	}
}

func TestRealForgetRememberedWorkspaceAuditFailureRollsBackThenRetries(t *testing.T) {
	harness := newSensitiveWorkspaceIntegration(t)
	harness.seedRememberedWorkspace(t)
	beforeSession := harness.session(t)
	beforeTouches := identityAuditCount(t, harness.storage.Database(), "session_touched")
	injected := errors.New("remembered workspace audit unavailable")
	harness.workspaceAudit.fail = injected
	request := &tammyv1.ForgetRememberedWorkspaceRequest{WorkspaceId: harness.workspaceID, Authentication: harness.authentication()}
	*harness.now = harness.now.Add(time.Minute)
	if _, err := harness.workspace.ForgetRememberedWorkspace(harness.ctx, connect.NewRequest(request)); !errors.Is(err, injected) {
		t.Fatalf("audit failure returned %v", err)
	}
	authoritative, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := harness.catalogue.ByID(harness.ctx, harness.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if authoritative.RememberedUntil == 0 || catalogue.RememberedUntil == 0 || harness.secretStore.Count() != 0 ||
		!sameSessionTimes(beforeSession, harness.session(t)) || identityAuditCount(t, harness.storage.Database(), "session_touched") != beforeTouches ||
		harness.workspaceAudit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 0 {
		t.Fatalf("audit rollback leaked sql_until=%d catalogue_until=%d vault=%d touches=%d workspace_audit=%d",
			authoritative.RememberedUntil, catalogue.RememberedUntil, harness.secretStore.Count(),
			identityAuditCount(t, harness.storage.Database(), "session_touched"), harness.workspaceAudit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"])
	}
	harness.workspaceAudit.fail = nil
	if _, err := harness.workspace.ForgetRememberedWorkspace(harness.ctx, connect.NewRequest(request)); err != nil {
		t.Fatal(err)
	}
	authoritative, err = harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if authoritative.RememberedUntil != 0 || identityAuditCount(t, harness.storage.Database(), "session_touched") != beforeTouches+1 ||
		harness.workspaceAudit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"] != 1 {
		t.Fatalf("retry sql_until=%d touches=%d workspace_audit=%d", authoritative.RememberedUntil,
			identityAuditCount(t, harness.storage.Database(), "session_touched"), harness.workspaceAudit.counts["REMEMBERED_WORKSPACE_FORGOTTEN"])
	}
}

func TestRealChangePassphraseReplayAndFailureDoNotTouchSession(t *testing.T) {
	harness := newSensitiveWorkspaceIntegration(t)
	secret := enrolConfirmedFactor(t, identityHarness{service: harness.identity, repository: harness.identityRepository, now: harness.now},
		harness.authentication(), harness.password, "01890f3c-7b2e-7cc4-98c4-dc0c0c073a02")
	operationID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073a03"
	request := &tammyv1.ChangePassphraseRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: operationID, Authentication: harness.authentication(), FreshFactor: &tammyv1.FreshFactorContext{
			AssertionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073a04", Purpose: "change_passphrase", AssertedAt: timestamppb.New(*harness.now),
		},
	}, WorkspaceId: harness.workspaceID, ExpectedVersion: harness.workspaceVersion,
		CurrentPassphrase: &tammyv1.SecretInput{Utf8: []byte(harness.passphrase)},
		NewPassphrase:     &tammyv1.SecretInput{Utf8: []byte("replacement-passphrase-long-enough")},
	}
	beforeFailure := harness.session(t)
	beforeFailureAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	*harness.now = harness.now.Add(time.Minute)
	if _, err := harness.workspace.ChangePassphrase(harness.ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("unissued factor returned %v", err)
	}
	afterFailure := harness.session(t)
	afterFailureAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	if !sameSessionTimes(beforeFailure, afterFailure) || afterFailureAudits != beforeFailureAudits {
		t.Fatalf("failed passphrase change touched session before=%+v after=%+v audits=%d→%d", beforeFailure, afterFailure,
			beforeFailureAudits, afterFailureAudits)
	}
	record, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != harness.workspaceVersion || record.OperationHashes[operationID] != "" || harness.workspaceAudit.counts["PASSPHRASE_CHANGE"] != 0 {
		t.Fatalf("failed passphrase mutation leaked version=%d operations=%d audits=%d", record.Version, len(record.OperationHashes),
			harness.workspaceAudit.counts["PASSPHRASE_CHANGE"])
	}
	*harness.now = harness.now.Add(30 * time.Second)
	asserted, err := harness.identity.AssertTOTP(harness.ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: harness.authentication(), Code: &tammyv1.TotpCodeInput{Value: TOTPCode(secret, *harness.now)}, Purpose: "change_passphrase",
	}))
	if err != nil {
		t.Fatal(err)
	}
	request.CommandContext.FreshFactor = asserted.Msg.FreshFactor
	beforeBoundary := harness.session(t)
	beforeBoundaryAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	beforeBoundaryRecord, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	headerFailure := errors.New("header/database commit boundary unavailable")
	harness.failures.failures["change_passphrase.before_db_commit"] = headerFailure
	if _, err := harness.workspace.ChangePassphrase(harness.ctx, connect.NewRequest(request)); !errors.Is(err, headerFailure) {
		t.Fatalf("header boundary failure returned %v", err)
	}
	delete(harness.failures.failures, "change_passphrase.before_db_commit")
	afterHeaderFailure := harness.session(t)
	afterHeaderFailureAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	afterHeaderFailureRecord, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSessionTimes(beforeBoundary, afterHeaderFailure) || afterHeaderFailureAudits != beforeBoundaryAudits ||
		afterHeaderFailureRecord.Version != beforeBoundaryRecord.Version || afterHeaderFailureRecord.OperationHashes[operationID] != "" {
		t.Fatalf("header boundary failure leaked session=%+v→%+v audits=%d→%d record=%+v→%+v", beforeBoundary, afterHeaderFailure,
			beforeBoundaryAudits, afterHeaderFailureAudits, beforeBoundaryRecord, afterHeaderFailureRecord)
	}
	auditFailure := errors.New("workspace audit unavailable")
	harness.workspaceAudit.fail = auditFailure
	if _, err := harness.workspace.ChangePassphrase(harness.ctx, connect.NewRequest(request)); !errors.Is(err, auditFailure) {
		t.Fatalf("workspace audit failure returned %v", err)
	}
	harness.workspaceAudit.fail = nil
	afterAuditFailure := harness.session(t)
	afterAuditFailureAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	afterAuditFailureRecord, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSessionTimes(beforeBoundary, afterAuditFailure) || afterAuditFailureAudits != beforeBoundaryAudits ||
		afterAuditFailureRecord.Version != beforeBoundaryRecord.Version || afterAuditFailureRecord.OperationHashes[operationID] != "" ||
		harness.workspaceAudit.counts["PASSPHRASE_CHANGE"] != 0 {
		t.Fatalf("audit failure leaked session=%+v→%+v identity_audits=%d→%d workspace_audits=%d record=%+v→%+v", beforeBoundary,
			afterAuditFailure, beforeBoundaryAudits, afterAuditFailureAudits, harness.workspaceAudit.counts["PASSPHRASE_CHANGE"],
			beforeBoundaryRecord, afterAuditFailureRecord)
	}
	changed, err := harness.workspace.ChangePassphrase(harness.ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	committedHeader, err := os.ReadFile(harness.databasePath + ".header")
	if err != nil {
		t.Fatal(err)
	}
	committedCatalogue, err := os.ReadFile(harness.cataloguePath)
	if err != nil {
		t.Fatal(err)
	}
	committedSession := harness.session(t)
	committedAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	committedWorkspaceAudits := harness.workspaceAudit.counts["PASSPHRASE_CHANGE"]
	committedRecord, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(time.Minute)
	replayed, err := harness.workspace.ChangePassphrase(harness.ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.ChangePassphraseRequest)))
	if err != nil || !proto.Equal(changed.Msg.Workspace, replayed.Msg.Workspace) {
		t.Fatalf("exact passphrase replay response=%v err=%v", replayed, err)
	}
	conflict := proto.Clone(request).(*tammyv1.ChangePassphraseRequest)
	conflict.NewPassphrase = &tammyv1.SecretInput{Utf8: []byte("different-replacement-passphrase-long-enough")}
	if _, err := harness.workspace.ChangePassphrase(harness.ctx, connect.NewRequest(conflict)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("passphrase semantic conflict returned %v", err)
	}
	afterReplay := harness.session(t)
	afterRecord, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterHeader, err := os.ReadFile(harness.databasePath + ".header")
	if err != nil {
		t.Fatal(err)
	}
	afterCatalogue, err := os.ReadFile(harness.cataloguePath)
	if err != nil {
		t.Fatal(err)
	}
	afterReplayAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	if !sameSessionTimes(committedSession, afterReplay) || afterReplayAudits != committedAudits ||
		harness.workspaceAudit.counts["PASSPHRASE_CHANGE"] != committedWorkspaceAudits || afterRecord.Version != committedRecord.Version ||
		afterRecord.OperationHashes[operationID] != committedRecord.OperationHashes[operationID] || !bytes.Equal(committedHeader, afterHeader) ||
		!bytes.Equal(committedCatalogue, afterCatalogue) {
		t.Fatalf("passphrase replay/conflict mutated session=%+v→%+v identity_audits=%d→%d workspace_audits=%d→%d record=%+v→%+v",
			committedSession, afterReplay, committedAudits, afterReplayAudits, committedWorkspaceAudits,
			harness.workspaceAudit.counts["PASSPHRASE_CHANGE"], committedRecord, afterRecord)
	}
}

func TestRealTransferOwnershipTerminalReplayIsBoundAndPure(t *testing.T) {
	harness := newSensitiveWorkspaceIntegration(t)
	target, err := harness.identity.BootstrapAdministrator(harness.ctx, "next-owner@example.test", "Next Owner", []byte("next-owner-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	factor := harness.factor(t, "ownership_transfer", "01890f3c-7b2e-7cc4-98c4-dc0c0c073a05")
	acknowledged := true
	operationID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073a06"
	request := &tammyv1.TransferOwnershipRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: operationID, Authentication: harness.authentication(), FreshFactor: factor,
	}, WorkspaceId: harness.workspaceID, ExpectedVersion: harness.workspaceVersion, TargetUserId: target.Id,
		AcknowledgeVerificationEffect: &acknowledged,
	}
	organisationFailure := errors.New("organisation impact unavailable")
	harness.organisations.fail = organisationFailure
	beforeFailure := harness.session(t)
	beforeFailureAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	*harness.now = harness.now.Add(time.Minute)
	if _, err := harness.workspace.TransferOwnership(harness.ctx, connect.NewRequest(request)); !errors.Is(err, organisationFailure) {
		t.Fatalf("organisation failure returned %v", err)
	}
	afterFailure := harness.session(t)
	afterFailureAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	if !sameSessionTimes(beforeFailure, afterFailure) || afterFailureAudits != beforeFailureAudits {
		t.Fatalf("failed ownership transfer touched session before=%+v after=%+v audits=%d→%d", beforeFailure, afterFailure,
			beforeFailureAudits, afterFailureAudits)
	}
	rolledBack, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.OwnerUserID != harness.ownerID || rolledBack.Version != harness.workspaceVersion ||
		rolledBack.OperationHashes[operationID] != "" || harness.workspaceAudit.counts["OWNERSHIP_TRANSFER"] != 0 {
		t.Fatalf("failed ownership mutation leaked record=%+v audits=%d", rolledBack, harness.workspaceAudit.counts["OWNERSHIP_TRANSFER"])
	}
	harness.organisations.fail = nil
	transferred, err := harness.workspace.TransferOwnership(harness.ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	committedSession := harness.session(t)
	if committedSession.State != tammyv1.SessionState_SESSION_STATE_INVALIDATED {
		t.Fatalf("ownership session state=%v", committedSession.State)
	}
	committedIdentityAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	committedInvalidationAudits := identityAuditCount(t, harness.storage.Database(), "sessions_invalidated")
	committedWorkspaceAudits := harness.workspaceAudit.counts["OWNERSHIP_TRANSFER"]
	committedRecord, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	committedHeader, err := os.ReadFile(harness.databasePath + ".header")
	if err != nil {
		t.Fatal(err)
	}
	committedCatalogue, err := os.ReadFile(harness.cataloguePath)
	if err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(time.Minute)
	replayed, err := harness.workspace.TransferOwnership(harness.ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.TransferOwnershipRequest)))
	if err != nil || !proto.Equal(transferred.Msg.Workspace, replayed.Msg.Workspace) {
		t.Fatalf("terminal ownership replay response=%v err=%v", replayed, err)
	}
	conflict := proto.Clone(request).(*tammyv1.TransferOwnershipRequest)
	conflict.TargetUserId = "01890f3c-7b2e-7cc4-98c4-dc0c0c073a07"
	if _, err := harness.workspace.TransferOwnership(harness.ctx, connect.NewRequest(conflict)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("terminal ownership conflict returned %v", err)
	}
	for name, authentication := range map[string]*tammyv1.AuthenticationContext{
		"unauthenticated": nil,
		"wrong actor":     {ActorUserId: target.Id, SessionId: harness.sessionID},
		"wrong session":   {ActorUserId: harness.ownerID, SessionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073a08"},
	} {
		replay := proto.Clone(request).(*tammyv1.TransferOwnershipRequest)
		replay.CommandContext.Authentication = authentication
		if _, err := harness.workspace.TransferOwnership(harness.ctx, connect.NewRequest(replay)); err == nil {
			t.Fatalf("%s terminal replay succeeded", name)
		}
	}
	differentOperation := proto.Clone(request).(*tammyv1.TransferOwnershipRequest)
	differentOperation.CommandContext.IdempotencyKey = "01890f3c-7b2e-7cc4-98c4-dc0c0c073a09"
	if _, err := harness.workspace.TransferOwnership(harness.ctx, connect.NewRequest(differentOperation)); err == nil {
		t.Fatal("invalidated session authorized a different operation")
	}
	afterReplay := harness.session(t)
	afterRecord, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterHeader, err := os.ReadFile(harness.databasePath + ".header")
	if err != nil {
		t.Fatal(err)
	}
	afterCatalogue, err := os.ReadFile(harness.cataloguePath)
	if err != nil {
		t.Fatal(err)
	}
	afterReplayIdentityAudits := identityAuditCount(t, harness.storage.Database(), "session_touched")
	afterReplayInvalidationAudits := identityAuditCount(t, harness.storage.Database(), "sessions_invalidated")
	if !sameSessionTimes(committedSession, afterReplay) || afterReplayIdentityAudits != committedIdentityAudits ||
		afterReplayInvalidationAudits != committedInvalidationAudits ||
		harness.workspaceAudit.counts["OWNERSHIP_TRANSFER"] != committedWorkspaceAudits || harness.organisations.calls != 1 ||
		afterRecord.Version != committedRecord.Version || afterRecord.OperationHashes[operationID] != committedRecord.OperationHashes[operationID] ||
		!bytes.Equal(committedHeader, afterHeader) || !bytes.Equal(committedCatalogue, afterCatalogue) {
		t.Fatalf("ownership replay/conflict mutated session=%+v→%+v identity_touch=%d→%d invalidations=%d→%d workspace_audits=%d→%d organisations=%d record=%+v→%+v",
			committedSession, afterReplay, committedIdentityAudits, afterReplayIdentityAudits, committedInvalidationAudits,
			afterReplayInvalidationAudits, committedWorkspaceAudits, harness.workspaceAudit.counts["OWNERSHIP_TRANSFER"],
			harness.organisations.calls, committedRecord, afterRecord)
	}
}

func forceAuthoritativeWorkspaceUnauthenticated(t *testing.T, harness *sensitiveWorkspaceIntegration) {
	t.Helper()
	record, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED
	operationID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073a10"
	if err := harness.storage.CommitWorkspaceMutation(harness.ctx, workspace.WorkspaceMutation{
		OperationID: operationID, Kind: "TEST_UNAUTHENTICATED", WorkspaceID: record.ID, Version: record.Version,
		SemanticHash: "test-only-authoritative-unauthenticated",
	}, record, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRealExpireUnauthenticatedCommitsAuthoritativeLockAndInvalidatesSession(t *testing.T) {
	harness := newSensitiveWorkspaceIntegration(t)
	forceAuthoritativeWorkspaceUnauthenticated(t, harness)
	*harness.now = harness.now.Add(6 * time.Minute)
	if err := harness.workspace.ExpireUnauthenticated(harness.ctx); err != nil {
		t.Fatal(err)
	}
	authoritative, err := harness.storage.LoadWorkspaceRecord(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, err := harness.workspace.GetWorkspaceState(harness.ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &harness.workspaceID}))
	if err != nil {
		t.Fatal(err)
	}
	if authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED ||
		harness.workspaceAudit.counts["LOCK"] != 1 {
		t.Fatalf("expiry state authoritative=%v catalogue=%v lock_audits=%d", authoritative.State, state.Msg.Workspace.State,
			harness.workspaceAudit.counts["LOCK"])
	}
	if _, err := harness.identity.GetSession(harness.ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: harness.authentication()})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("expired workspace retained session: %v", err)
	}
	if _, err := harness.workspace.LockWorkspace(harness.ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); !errors.Is(err, faults.New(faults.CodeValidation, nil)) {
		t.Fatalf("expiry retained active runtime: %v", err)
	}
}

func TestRealExpireUnauthenticatedFailureClosesRuntimeAndProvedReopenRepairsSession(t *testing.T) {
	harness := newSensitiveWorkspaceIntegration(t)
	forceAuthoritativeWorkspaceUnauthenticated(t, harness)
	*harness.now = harness.now.Add(6 * time.Minute)
	injected := errors.New("identity invalidation audit unavailable")
	harness.identityAudit.fail = injected
	if err := harness.workspace.ExpireUnauthenticated(harness.ctx); !errors.Is(err, injected) {
		t.Fatalf("expiry failure returned %v", err)
	}
	harness.identityAudit.fail = nil
	state, err := harness.workspace.GetWorkspaceState(harness.ctx, connect.NewRequest(&tammyv1.GetWorkspaceStateRequest{WorkspaceId: &harness.workspaceID}))
	if err != nil || state.Msg.Workspace.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		t.Fatalf("failed expiry catalogue state=%v err=%v", state.Msg.Workspace, err)
	}
	if _, err := harness.workspace.LockWorkspace(harness.ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); !errors.Is(err, faults.New(faults.CodeValidation, nil)) {
		t.Fatalf("failed expiry retained active runtime: %v", err)
	}
	rolledBack := harness.session(t)
	if rolledBack.State != tammyv1.SessionState_SESSION_STATE_ACTIVE {
		t.Fatalf("injected SQL rollback did not preserve repair case: %v", rolledBack.State)
	}
	if _, err := harness.workspace.UnlockWorkspace(harness.ctx, connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: "workspace"},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte(harness.passphrase)},
		}},
	})); err != nil {
		t.Fatalf("proved repair reopen: %v", err)
	}
	if _, err := harness.identity.GetSession(harness.ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: harness.authentication()})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("proved reopen retained rolled-back session: %v", err)
	}
}
