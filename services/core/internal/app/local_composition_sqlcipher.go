//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/identity"
	"github.com/tammyapp/tammy/services/core/internal/overview"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

const (
	LocalWorkspaceDirectoryCapability = "local-workspace-directory"
	LocalWorkspaceFileCapability      = "local-workspace-file"
	localMigrationTarget              = 4
)

type LocalCompositionConfig struct {
	Info           buildinfo.Info
	Root           string
	AttemptAnchors workspace.AnchorStore
}

type localCapabilityResolver struct {
	directory string
	database  string
}

func (resolver localCapabilityResolver) Resolve(_ context.Context, reference *tammyv1.ApprovedFileRef, kind workspace.CapabilityKind) (string, error) {
	if reference == nil {
		return "", ErrComposition
	}
	switch {
	case kind == workspace.CapabilityDirectory && reference.CapabilityId == LocalWorkspaceDirectoryCapability:
		return resolver.directory, nil
	case kind == workspace.CapabilityWorkspaceFile && reference.CapabilityId == LocalWorkspaceFileCapability:
		return resolver.database, nil
	default:
		return "", ErrComposition
	}
}

type localIdentityAudit struct{}

func (localIdentityAudit) Record(context.Context, workspace.MutationExecutor, string, string) error {
	return nil
}

type localWorkspaceAudit struct{}

func (localWorkspaceAudit) AppendWorkspaceMutation(context.Context, workspace.MutationExecutor, workspace.WorkspaceMutation) error {
	return nil
}

type localOrganisationImpact struct{}

func (localOrganisationImpact) ApplyOwnershipTransfer(context.Context, workspace.MutationExecutor, workspace.OwnershipImpact) error {
	return nil
}

type localSessionLifecycle struct{ service **workspace.Service }

func (lifecycle localSessionLifecycle) current() (*workspace.Service, error) {
	if lifecycle.service == nil || *lifecycle.service == nil {
		return nil, ErrComposition
	}
	return *lifecycle.service, nil
}

func (lifecycle localSessionLifecycle) SessionStartedWithin(ctx context.Context, executor workspace.MutationExecutor, sessionID string) error {
	service, err := lifecycle.current()
	if err != nil {
		return err
	}
	return service.SessionStartedWithin(ctx, executor, sessionID)
}

func (lifecycle localSessionLifecycle) SessionStartedAuditedWithin(ctx context.Context, executor workspace.MutationExecutor) error {
	service, err := lifecycle.current()
	if err != nil {
		return err
	}
	return service.SessionStartedAuditedWithin(ctx, executor)
}

func (lifecycle localSessionLifecycle) SessionStartedCommitted(ctx context.Context) error {
	service, err := lifecycle.current()
	if err != nil {
		return err
	}
	return service.SessionStartedCommitted(ctx)
}

type localIdentityBridge struct {
	mu      sync.RWMutex
	current *identity.Service
}

func (bridge *localIdentityBridge) load() *identity.Service {
	bridge.mu.RLock()
	defer bridge.mu.RUnlock()
	return bridge.current
}

func (bridge *localIdentityBridge) store(service *identity.Service) {
	bridge.mu.Lock()
	bridge.current = service
	bridge.mu.Unlock()
}

func (bridge *localIdentityBridge) BootstrapAdministrator(ctx context.Context, username, displayName string, password []byte) (*tammyv1.User, error) {
	return bridge.load().BootstrapAdministrator(ctx, username, displayName, password)
}
func (bridge *localIdentityBridge) BootstrapAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, operationID, username, displayName string, password []byte) (*tammyv1.User, error) {
	return bridge.load().BootstrapAdministratorWithin(ctx, executor, operationID, username, displayName, password)
}
func (bridge *localIdentityBridge) BreakGlassResetAdministrator(ctx context.Context, operationID, username string, password []byte) (*tammyv1.User, error) {
	return bridge.load().BreakGlassResetAdministrator(ctx, operationID, username, password)
}
func (bridge *localIdentityBridge) BreakGlassResetAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, operationID, username string, password []byte) (*tammyv1.User, error) {
	return bridge.load().BreakGlassResetAdministratorWithin(ctx, executor, operationID, username, password)
}
func (bridge *localIdentityBridge) RequireAdministrator(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	return bridge.load().RequireAdministrator(ctx, authentication)
}
func (bridge *localIdentityBridge) RequireAdministratorReadOnly(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	return bridge.load().RequireAdministratorReadOnly(ctx, authentication)
}
func (bridge *localIdentityBridge) ValidateAdministratorReplayBinding(ctx context.Context, authentication *tammyv1.AuthenticationContext, actorID, sessionID string) error {
	return bridge.load().ValidateAdministratorReplayBinding(ctx, authentication, actorID, sessionID)
}
func (bridge *localIdentityBridge) RequireAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, authentication *tammyv1.AuthenticationContext) error {
	return bridge.load().RequireAdministratorWithin(ctx, executor, authentication)
}
func (bridge *localIdentityBridge) RequireActiveSessionReadOnly(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	return bridge.load().RequireActiveSessionReadOnly(ctx, authentication)
}
func (bridge *localIdentityBridge) RequireActiveSessionWithin(ctx context.Context, executor workspace.MutationExecutor, authentication *tammyv1.AuthenticationContext) error {
	return bridge.load().RequireActiveSessionWithin(ctx, executor, authentication)
}
func (bridge *localIdentityBridge) ConsumeFreshFactor(ctx context.Context, authentication *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, purpose string) error {
	return bridge.load().ConsumeFreshFactor(ctx, authentication, factor, purpose)
}
func (bridge *localIdentityBridge) ConsumeFreshFactorWithin(ctx context.Context, executor workspace.MutationExecutor, authentication *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, purpose string) error {
	return bridge.load().ConsumeFreshFactorWithin(ctx, executor, authentication, factor, purpose)
}
func (bridge *localIdentityBridge) IsActiveAdministrator(ctx context.Context, userID string) bool {
	return bridge.load().IsActiveAdministrator(ctx, userID)
}
func (bridge *localIdentityBridge) IsActiveAdministratorWithin(ctx context.Context, executor workspace.MutationExecutor, userID string) (bool, error) {
	return bridge.load().IsActiveAdministratorWithin(ctx, executor, userID)
}
func (bridge *localIdentityBridge) InvalidateAllSessions(ctx context.Context) error {
	return bridge.load().InvalidateAllSessions(ctx)
}
func (bridge *localIdentityBridge) InvalidateAllSessionsWithin(ctx context.Context, executor workspace.MutationExecutor) error {
	return bridge.load().InvalidateAllSessionsWithin(ctx, executor)
}

type localIdentityRoute struct {
	mu      sync.RWMutex
	service tammyv1connect.IdentityServiceHandler
	options []connect.HandlerOption
	handler http.Handler
}

type localOverviewAccess struct{ bridge *localIdentityBridge }

func (access localOverviewAccess) RequireRead(ctx context.Context, authentication *tammyv1.AuthenticationContext) error {
	if access.bridge == nil {
		return ErrComposition
	}
	return access.bridge.RequireActiveSessionReadOnly(ctx, authentication)
}

type localOverviewRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *localOverviewRoute) set(service tammyv1connect.OverviewServiceHandler) error {
	if service == nil {
		return ErrComposition
	}
	_, handler := tammyv1connect.NewOverviewServiceHandler(service, append([]connect.HandlerOption(nil), route.options...)...)
	route.mu.Lock()
	route.handler = handler
	route.mu.Unlock()
	return nil
}

func (route *localOverviewRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.OverviewServiceName + "/", route
}

func (route *localOverviewRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local overview unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

func (route *localIdentityRoute) set(service tammyv1connect.IdentityServiceHandler) error {
	if service == nil {
		return ErrComposition
	}
	_, handler := tammyv1connect.NewIdentityServiceHandler(service, append([]connect.HandlerOption(nil), route.options...)...)
	route.mu.Lock()
	route.service = service
	route.handler = handler
	route.mu.Unlock()
	return nil
}

func (route *localIdentityRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	_, route.handler = tammyv1connect.NewIdentityServiceHandler(route.service, route.options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.IdentityServiceName + "/", route
}

func (route *localIdentityRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local identity unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

type localRuntime struct {
	mu                sync.Mutex
	workspace         *workspace.Service
	bridge            *localIdentityBridge
	identityRoute     *localIdentityRoute
	overviewRoute     *localOverviewRoute
	bootstrapIdentity *identity.Service
	activeIdentity    *identity.Service
	passwords         *workspace.PasswordPolicy
	clock             clock.Clock
	ids               *ids.Generator
	identityAttempts  *workspace.AttemptJournal
	workspaceAttempts *workspace.AttemptJournal
	identityFactorKey []byte
}

type localWorkspaceHandler struct {
	tammyv1connect.UnimplementedWorkspaceServiceHandler
	service *workspace.Service
	runtime *localRuntime
}

func (handler *localWorkspaceHandler) CreateWorkspace(ctx context.Context, request *connect.Request[tammyv1.CreateWorkspaceRequest]) (*connect.Response[tammyv1.CreateWorkspaceResponse], error) {
	response, err := handler.service.CreateWorkspace(ctx, request)
	if err != nil || response == nil || response.Msg == nil || response.Msg.Workspace == nil {
		return response, err
	}
	if err := handler.runtime.activate(response.Msg.Workspace.Id); err != nil {
		return nil, err
	}
	return response, nil
}

func (handler *localWorkspaceHandler) UnlockWorkspace(ctx context.Context, request *connect.Request[tammyv1.UnlockWorkspaceRequest]) (*connect.Response[tammyv1.UnlockWorkspaceResponse], error) {
	response, err := handler.service.UnlockWorkspace(ctx, request)
	if err != nil || response == nil || response.Msg == nil || response.Msg.Workspace == nil {
		return response, err
	}
	if err := handler.runtime.activate(response.Msg.Workspace.Id); err != nil {
		return nil, err
	}
	return response, nil
}

func (handler *localWorkspaceHandler) ConfirmRecovery(ctx context.Context, request *connect.Request[tammyv1.ConfirmRecoveryRequest]) (*connect.Response[tammyv1.ConfirmRecoveryResponse], error) {
	return handler.service.ConfirmRecovery(ctx, request)
}

func (handler *localWorkspaceHandler) LockWorkspace(ctx context.Context, request *connect.Request[tammyv1.LockWorkspaceRequest]) (*connect.Response[tammyv1.LockWorkspaceResponse], error) {
	return handler.service.LockWorkspace(ctx, request)
}

func (handler *localWorkspaceHandler) ForgetRememberedWorkspace(ctx context.Context, request *connect.Request[tammyv1.ForgetRememberedWorkspaceRequest]) (*connect.Response[tammyv1.ForgetRememberedWorkspaceResponse], error) {
	return handler.service.ForgetRememberedWorkspace(ctx, request)
}

func (handler *localWorkspaceHandler) GetWorkspaceState(ctx context.Context, request *connect.Request[tammyv1.GetWorkspaceStateRequest]) (*connect.Response[tammyv1.GetWorkspaceStateResponse], error) {
	return handler.service.GetWorkspaceState(ctx, request)
}

func (handler *localWorkspaceHandler) ChangePassphrase(ctx context.Context, request *connect.Request[tammyv1.ChangePassphraseRequest]) (*connect.Response[tammyv1.ChangePassphraseResponse], error) {
	return handler.service.ChangePassphrase(ctx, request)
}

func (handler *localWorkspaceHandler) RecoverWorkspace(ctx context.Context, request *connect.Request[tammyv1.RecoverWorkspaceRequest]) (*connect.Response[tammyv1.RecoverWorkspaceResponse], error) {
	return handler.service.RecoverWorkspace(ctx, request)
}

func (handler *localWorkspaceHandler) TransferOwnership(ctx context.Context, request *connect.Request[tammyv1.TransferOwnershipRequest]) (*connect.Response[tammyv1.TransferOwnershipResponse], error) {
	return handler.service.TransferOwnership(ctx, request)
}

func (runtime *localRuntime) activate(workspaceID string) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	database, err := runtime.workspace.ActiveDatabase(workspaceID)
	if err != nil {
		return err
	}
	repository, err := identity.NewDatabaseRepository(database)
	if err != nil {
		return err
	}
	service, err := identity.NewService(identity.Config{
		Repository: repository, Passwords: runtime.passwords, Clock: runtime.clock, Random: rand.Reader,
		IDs: runtime.ids, Attempts: runtime.identityAttempts, FactorEncryptionKey: append([]byte(nil), runtime.identityFactorKey...),
		Audit: localIdentityAudit{}, SessionLifecycle: localSessionLifecycle{service: &runtime.workspace},
	})
	if err != nil {
		return err
	}
	runtime.activeIdentity = service
	runtime.bridge.store(service)
	if err := runtime.identityRoute.set(service); err != nil {
		return err
	}
	snapshots, err := overview.NewSQLCipherSnapshotPort(database, workspaceID)
	if err != nil {
		return err
	}
	overviewService, err := overview.NewService(overview.ServiceConfig{
		Access: localOverviewAccess{bridge: runtime.bridge}, Snapshots: snapshots,
	})
	if err != nil {
		return err
	}
	return runtime.overviewRoute.set(overviewService)
}

func (runtime *localRuntime) Close() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.workspace != nil {
		_, _ = runtime.workspace.LockWorkspace(context.Background(), connect.NewRequest(&tammyv1.LockWorkspaceRequest{}))
	}
	if runtime.activeIdentity != nil {
		_ = runtime.activeIdentity.Close()
	}
	if runtime.bootstrapIdentity != nil {
		_ = runtime.bootstrapIdentity.Close()
	}
	if runtime.workspaceAttempts != nil {
		runtime.workspaceAttempts.Close()
	}
	if runtime.identityAttempts != nil {
		runtime.identityAttempts.Close()
	}
	workspace.Zero(runtime.identityFactorKey)
	return nil
}

func NewLocalComposition(config LocalCompositionConfig) (*Composition, error) {
	if !validBuildInfo(config.Info) || !filepath.IsAbs(config.Root) || filepath.Clean(config.Root) != config.Root {
		return nil, ErrComposition
	}
	root := filepath.Join(config.Root, "core")
	workspaceDirectory := filepath.Join(root, "workspace")
	for _, directory := range []string{root, workspaceDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil || os.Chmod(directory, 0o700) != nil {
			return nil, ErrComposition
		}
	}
	master, err := readOrCreateLocalKey(filepath.Join(root, "installation.key"))
	if err != nil {
		return nil, err
	}
	defer workspace.Zero(master)
	source := clock.Func(time.Now)
	generator, err := ids.NewGenerator(source, rand.Reader)
	if err != nil {
		return nil, err
	}
	passwords, err := workspace.NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		return nil, err
	}
	attemptAnchors := config.AttemptAnchors
	if attemptAnchors == nil {
		attemptAnchors, err = workspace.NewPlatformAnchorStore()
		if err != nil {
			return nil, err
		}
	}
	workspaceAttempts, err := workspace.NewAttemptJournal(filepath.Join(root, "workspace-attempts.journal"), deriveLocalKey(master, "workspace-attempts"), source,
		"local-workspace", attemptAnchors)
	if err != nil {
		return nil, err
	}
	identityAttempts, err := workspace.NewAttemptJournal(filepath.Join(root, "identity-attempts.journal"), deriveLocalKey(master, "identity-attempts"), source,
		"local-identity", attemptAnchors)
	if err != nil {
		workspaceAttempts.Close()
		return nil, err
	}
	remembered, err := workspace.NewRememberedKeyManager(workspace.NewMemorySecretStore(), source)
	if err != nil {
		workspaceAttempts.Close()
		identityAttempts.Close()
		return nil, err
	}
	catalogue, err := workspace.NewFileRepository(filepath.Join(root, "workspace-catalogue.enc"), deriveLocalKey(master, "workspace-catalogue"))
	if err != nil {
		workspaceAttempts.Close()
		identityAttempts.Close()
		return nil, err
	}
	runtime := &localRuntime{passwords: passwords, clock: source, ids: generator,
		workspaceAttempts: workspaceAttempts, identityAttempts: identityAttempts,
		identityFactorKey: deriveLocalKey(master, "identity-factor")}
	bootstrapIdentity, err := identity.NewService(identity.Config{
		Repository: identity.NewMemoryRepository(), Passwords: passwords, Clock: source, Random: rand.Reader,
		IDs: generator, Attempts: identityAttempts, FactorEncryptionKey: append([]byte(nil), runtime.identityFactorKey...),
		Audit: localIdentityAudit{}, SessionLifecycle: localSessionLifecycle{service: &runtime.workspace},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	runtime.bootstrapIdentity = bootstrapIdentity
	runtime.bridge = &localIdentityBridge{current: bootstrapIdentity}
	runtime.identityRoute = &localIdentityRoute{service: bootstrapIdentity}
	runtime.overviewRoute = &localOverviewRoute{}
	workspaceService, err := workspace.NewService(workspace.Config{
		Repository:   catalogue,
		Capabilities: localCapabilityResolver{directory: workspaceDirectory, database: filepath.Join(workspaceDirectory, "tammy-workspace.db")},
		Storage:      workspace.NewSQLCipherStorageFactory(localMigrationTarget), Identity: runtime.bridge,
		Audit: localWorkspaceAudit{}, OrganisationImpact: localOrganisationImpact{}, Passwords: passwords,
		RememberedKeys: remembered, Attempts: workspaceAttempts, Clock: source, IDs: generator,
		HeaderAuthenticationKey: deriveLocalKey(master, "workspace-header"), InstallationKey: deriveLocalKey(master, "workspace-installation"),
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	runtime.workspace = workspaceService
	workspaceHandler := &localWorkspaceHandler{service: workspaceService, runtime: runtime}
	var _ tammyv1connect.WorkspaceServiceHandler = workspaceHandler
	factories := []transport.GeneratedHandlerFactory{
		systemHandlerFactory(config.Info),
		func(options ...connect.HandlerOption) (string, http.Handler) {
			return tammyv1connect.NewWorkspaceServiceHandler(workspaceHandler, options...)
		},
		runtime.identityRoute.factory,
		runtime.overviewRoute.factory,
	}
	return newComposition(factories, []ResourceCloser{runtime})
}

func deriveLocalKey(master []byte, domain string) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.local-composition.v1\x00"))
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(master)
	return digest.Sum(nil)
}

func readOrCreateLocalKey(path string) ([]byte, error) {
	if payload, err := os.ReadFile(path); err == nil {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || len(payload) != 32 {
			return nil, ErrComposition
		}
		return append([]byte(nil), payload...), nil
	} else if !os.IsNotExist(err) {
		return nil, ErrComposition
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, ErrComposition
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		workspace.Zero(key)
		return nil, ErrComposition
	}
	writeErr := error(nil)
	if _, err := file.Write(key); err != nil {
		writeErr = err
	} else if err := file.Sync(); err != nil {
		writeErr = err
	}
	writeErr = errors.Join(writeErr, file.Close())
	if writeErr != nil {
		workspace.Zero(key)
		return nil, ErrComposition
	}
	return key, nil
}
