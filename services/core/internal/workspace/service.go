//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package workspace

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CapabilityKind uint8

const (
	CapabilityDirectory CapabilityKind = iota + 1
	CapabilityWorkspaceFile
)

const expiredSetupRetention = 30 * 24 * time.Hour

type CapabilityResolver interface {
	Resolve(context.Context, *tammyv1.ApprovedFileRef, CapabilityKind) (string, error)
}

type IdentityPort interface {
	BootstrapAdministrator(context.Context, string, string, []byte) (*tammyv1.User, error)
	BootstrapAdministratorWithin(context.Context, MutationExecutor, string, string, string, []byte) (*tammyv1.User, error)
	BreakGlassResetAdministrator(context.Context, string, string, []byte) (*tammyv1.User, error)
	BreakGlassResetAdministratorWithin(context.Context, MutationExecutor, string, string, []byte) (*tammyv1.User, error)
	RequireAdministrator(context.Context, *tammyv1.AuthenticationContext) error
	RequireAdministratorReadOnly(context.Context, *tammyv1.AuthenticationContext) error
	ValidateAdministratorReplayBinding(context.Context, *tammyv1.AuthenticationContext, string, string) error
	RequireAdministratorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext) error
	RequireActiveSessionReadOnly(context.Context, *tammyv1.AuthenticationContext) error
	RequireActiveSessionWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext) error
	ConsumeFreshFactor(context.Context, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error
	ConsumeFreshFactorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error
	IsActiveAdministrator(context.Context, string) bool
	IsActiveAdministratorWithin(context.Context, MutationExecutor, string) (bool, error)
	InvalidateAllSessions(context.Context) error
	InvalidateAllSessionsWithin(context.Context, MutationExecutor) error
}

type MutationAuditPort interface {
	AppendWorkspaceMutation(context.Context, MutationExecutor, WorkspaceMutation) error
}

// AuditBootstrapPort is an optional extension implemented by the production
// audit adapter. It joins chain/key creation to CREATE and can reconstruct the
// public header metadata after a crash between the database commit and header write.
type AuditBootstrapPort interface {
	BeginInitialMirrorLifecycle(context.Context, string, string) error
	BootstrapWorkspaceAudit(context.Context, MutationExecutor, string, []byte, time.Time) (*AuditHeaderMetadata, error)
	LoadWorkspaceAuditHeader(context.Context, MutationExecutor, string) (*AuditHeaderMetadata, error)
	EstablishInitialMirror(context.Context, MutationExecutor, string, string) error
}

type FailureCheckpointPort interface {
	Check(string) error
}

const workspaceUnlockAttemptScope = "workspace_unlock"

func workspaceUnlockAttemptPolicy() AttemptPolicy {
	return AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
}

type Config struct {
	Repository              Repository
	Capabilities            CapabilityResolver
	Storage                 StorageFactory
	Identity                IdentityPort
	Audit                   MutationAuditPort
	OrganisationImpact      OrganisationImpactPort
	FailureCheckpoints      FailureCheckpointPort
	Passwords               *PasswordPolicy
	RememberedKeys          *RememberedKeyManager
	Attempts                *AttemptJournal
	Clock                   clock.Clock
	IDs                     *ids.Generator
	HeaderAuthenticationKey []byte
	InstallationKey         []byte
}

type workspaceRuntime struct {
	dek      []byte
	storage  StorageHandle
	header   *HeaderStore
	openedAt time.Time
}

type Service struct {
	mu            sync.Mutex
	repository    Repository
	capabilities  CapabilityResolver
	storage       StorageFactory
	identity      IdentityPort
	audit         MutationAuditPort
	organisations OrganisationImpactPort
	failures      FailureCheckpointPort
	passwords     *PasswordPolicy
	remembered    *RememberedKeyManager
	attempts      *AttemptJournal
	clock         clock.Clock
	ids           *ids.Generator
	headerAuthKey []byte
	installKey    []byte
	active        map[string]*workspaceRuntime
}

func NewService(config Config) (*Service, error) {
	if config.Repository == nil || config.Capabilities == nil || config.Storage == nil || config.Identity == nil ||
		config.Audit == nil || config.OrganisationImpact == nil || config.Passwords == nil || config.RememberedKeys == nil || config.Attempts == nil || config.Clock == nil ||
		config.IDs == nil || len(config.HeaderAuthenticationKey) != 32 || len(config.InstallationKey) != 32 {
		return nil, ErrWorkspaceNotFound
	}
	if err := config.Repository.NormalizeOpen(context.Background()); err != nil {
		return nil, err
	}
	return &Service{repository: config.Repository, capabilities: config.Capabilities, storage: config.Storage,
		identity: config.Identity, audit: config.Audit, organisations: config.OrganisationImpact, failures: config.FailureCheckpoints,
		passwords: config.Passwords, remembered: config.RememberedKeys, attempts: config.Attempts,
		clock: config.Clock, ids: config.IDs, headerAuthKey: append([]byte(nil), config.HeaderAuthenticationKey...),
		installKey: append([]byte(nil), config.InstallationKey...), active: make(map[string]*workspaceRuntime)}, nil
}

func (service *Service) CreateWorkspace(ctx context.Context, request *connect.Request[tammyv1.CreateWorkspaceRequest]) (*connect.Response[tammyv1.CreateWorkspaceResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || !ids.IsCanonicalV7(request.Msg.SetupId) || request.Msg.Destination == nil ||
		request.Msg.WorkspacePassphrase == nil || request.Msg.AdministratorPassword == nil || request.Msg.AdministratorUsername == "" || request.Msg.AdministratorDisplayName == "" {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	semantic := service.setupSemantic(request.Msg)
	retained, retainedErr := service.repository.BySetup(ctx, request.Msg.SetupId)
	if retainedErr == nil {
		if err := service.requireRuntimeAdmission(retained.ID, true); err != nil {
			return nil, err
		}
		retainedExists := true
		if service.expiredSetupRetentionElapsed(retained) {
			if err := service.repository.Delete(ctx, retained.ID); err != nil {
				return nil, err
			}
			retainedExists = false
		}
		if retainedExists {
			if retained.SetupSemanticHash != semantic {
				return nil, faults.New(faults.CodeIdempotencyConflict, nil)
			}
			terminal, err := service.convergeRetainedTerminalSetup(ctx, request.Msg.WorkspacePassphrase.Utf8, retained)
			if err != nil {
				return nil, err
			}
			if terminal {
				retained, err = service.repository.BySetup(ctx, retained.SetupID)
				if err != nil {
					return nil, err
				}
			}
			retained, retainedExists, err = service.applyRetainedSetupLifecycle(ctx, retained)
			if err != nil {
				return nil, err
			}
			if retainedExists {
				if retained.SetupPhase == "expired" {
					return nil, faults.New(faults.CodeValidation, nil)
				}
				if retained.State != tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY {
					return nil, faults.New(faults.CodeValidation, nil)
				}
				return service.resumeCreate(ctx, request.Msg, retained)
			}
		}
	} else if !errors.Is(retainedErr, ErrWorkspaceNotFound) {
		return nil, retainedErr
	}
	if err := service.requireRuntimeAdmission("", false); err != nil {
		return nil, err
	}
	directory, err := service.capabilities.Resolve(ctx, request.Msg.Destination, CapabilityDirectory)
	if err != nil || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	databasePath := filepath.Join(directory, "tammy-workspace.db")
	if _, err := os.Lstat(databasePath); !os.IsNotExist(err) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	workspaceID, err := service.ids.New()
	if err != nil {
		return nil, err
	}
	material, recoveryDisplay, err := GenerateKeyMaterial(service.passwords, request.Msg.WorkspacePassphrase.Utf8, workspaceID, 1)
	if err != nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	defer Zero(recoveryDisplay)
	headerPath := databasePath + ".header"
	groups, err := ParseRecoveryGroups(recoveryDisplay)
	if err != nil {
		material.Destroy()
		return nil, err
	}
	hashes := make([][]byte, len(groups))
	for index, group := range groups {
		hashes[index] = service.recoveryGroupHash(workspaceID, index, group)
	}
	encryptedDisplay, err := service.sealInstallation(recoveryDisplay, "setup/"+request.Msg.SetupId)
	if err != nil {
		material.Destroy()
		return nil, err
	}
	initialHeader := HeaderSlot{Version: 1, RecoveryVersion: 1, OperationID: request.Msg.SetupId,
		WorkspaceID: workspaceID, PassphraseWrap: material.PassphraseWrap, RecoveryWrap: material.RecoveryWrap}
	encryptedMaterial, err := service.sealSetupMaterial(request.Msg.SetupId, setupMaterial{DEK: material.DEK, InitialHeader: initialHeader})
	material.Destroy()
	if err != nil {
		return nil, err
	}
	now := service.clock.Now().UTC()
	record := workspaceRecord{ID: workspaceID, Version: 1, State: tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY,
		TrustState: tammyv1.WorkspaceTrustState_WORKSPACE_TRUST_STATE_TRUSTED, DisplayName: "Tammy Workspace",
		DatabasePath: databasePath, HeaderPath: headerPath, SetupID: request.Msg.SetupId, SetupSemanticHash: semantic,
		SetupExpires: now.Add(15 * time.Minute).Unix(), SetupPhase: "reserved", SetupMaterialEncrypted: encryptedMaterial,
		RecoveryDisplayEncrypted: encryptedDisplay, RecoveryGroupHashes: hashes}
	if request.Msg.Destination.DisplayName != nil && *request.Msg.Destination.DisplayName != "" {
		record.DisplayName = *request.Msg.Destination.DisplayName
	}
	if err := service.repository.Save(ctx, record); err != nil {
		return nil, err
	}
	if err := service.checkpoint("create.after_reservation"); err != nil {
		return nil, err
	}
	return service.resumeCreate(ctx, request.Msg, record)
}

func (service *Service) applyRetainedSetupLifecycle(ctx context.Context, record workspaceRecord) (workspaceRecord, bool, error) {
	now := service.clock.Now().UTC()
	var err error
	if record.SetupPhase == "expiry_cleanup" {
		if err := service.finishExpiredSetupCleanup(ctx, record); err != nil {
			return workspaceRecord{}, true, err
		}
		record, err = service.repository.BySetup(ctx, record.SetupID)
		if err != nil {
			return workspaceRecord{}, true, err
		}
	}
	if record.State == tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY &&
		record.SetupPhase != "expired" && !now.Before(time.Unix(record.SetupExpires, 0)) {
		if err := service.expirePendingSetup(ctx, record); err != nil {
			return workspaceRecord{}, true, err
		}
		record, err = service.repository.BySetup(ctx, record.SetupID)
		if err != nil {
			return workspaceRecord{}, true, err
		}
	}
	if record.SetupPhase != "expired" {
		return record, true, nil
	}
	if !service.expiredSetupRetentionElapsed(record) {
		return record, true, nil
	}
	if err := service.repository.Delete(ctx, record.ID); err != nil {
		return workspaceRecord{}, true, err
	}
	return workspaceRecord{}, false, nil
}

func (service *Service) expiredSetupRetentionElapsed(record workspaceRecord) bool {
	if record.SetupPhase != "expired" {
		return false
	}
	expiredAt := record.SetupExpiredAt
	if expiredAt == 0 {
		expiredAt = record.SetupExpires
	}
	return !service.clock.Now().UTC().Before(time.Unix(expiredAt, 0).Add(expiredSetupRetention))
}

func (service *Service) convergeRetainedTerminalSetup(ctx context.Context, passphrase []byte, record workspaceRecord) (bool, error) {
	if record.SetupPhase != "ready" {
		return false, nil
	}
	if runtime := service.active[record.ID]; runtime != nil && runtime.storage != nil {
		authoritative, err := runtime.storage.LoadWorkspaceRecord(ctx)
		if err != nil {
			return false, err
		}
		if !terminalSetupRecord(authoritative) {
			return false, nil
		}
		if err := service.convergeTerminalSetup(ctx, record, authoritative); err != nil {
			return false, err
		}
		return true, nil
	}
	if info, err := os.Lstat(record.DatabasePath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrWorkspaceNotFound
	}
	header, err := NewHeaderStore(record.HeaderPath, service.headerAuthKey)
	if err != nil {
		return false, err
	}
	defer header.Close()
	slots, err := header.Slots()
	if err != nil {
		return false, err
	}
	sort.Slice(slots[:], func(i, j int) bool { return slots[i].Version > slots[j].Version })
	for _, candidate := range slots {
		if candidate.Version == 0 || candidate.WorkspaceID != record.ID {
			continue
		}
		dek, unwrapErr := UnwrapWithPassphrase(service.passwords, passphrase, candidate.PassphraseWrap, record.ID, candidate.Version)
		if unwrapErr != nil {
			continue
		}
		storage, openErr := service.storage.Open(ctx, record.DatabasePath, dek)
		if openErr != nil {
			Zero(dek)
			continue
		}
		elected, electionErr := header.Elect(func(operationID string, version uint64) bool {
			return storage.HeaderOperationCommitted(ctx, operationID, version)
		})
		if electionErr != nil || elected.Version != candidate.Version || elected.WorkspaceID != record.ID {
			_ = storage.Close()
			Zero(dek)
			continue
		}
		authoritative, loadErr := storage.LoadWorkspaceRecord(ctx)
		_ = storage.Close()
		Zero(dek)
		if loadErr != nil {
			return false, loadErr
		}
		if authoritative.ID != record.ID || authoritative.SetupID != record.SetupID ||
			authoritative.SetupSemanticHash != record.SetupSemanticHash {
			return false, ErrWorkspaceNotFound
		}
		if !terminalSetupRecord(authoritative) {
			return false, nil
		}
		if err := service.convergeTerminalSetup(ctx, record, authoritative); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, ErrKeyMaterial
}

type setupMaterial struct {
	DEK           []byte
	InitialHeader HeaderSlot
}

func (material *setupMaterial) destroy() {
	if material == nil {
		return
	}
	Zero(material.DEK)
	material.DEK = nil
}

func (service *Service) resumeCreate(ctx context.Context, request *tammyv1.CreateWorkspaceRequest,
	record workspaceRecord) (*connect.Response[tammyv1.CreateWorkspaceResponse], error) {
	if err := service.requireRuntimeAdmission(record.ID, true); err != nil {
		return nil, err
	}
	if runtime := service.active[record.ID]; runtime != nil && runtime.storage != nil && runtime.header != nil && record.SetupPhase == "ready" {
		authoritative, err := runtime.storage.LoadWorkspaceRecord(ctx)
		if err != nil {
			return nil, err
		}
		if terminalSetupRecord(authoritative) {
			if err := service.convergeTerminalSetup(ctx, record, authoritative); err != nil {
				return nil, err
			}
			return nil, faults.New(faults.CodeValidation, nil)
		}
		return service.pendingCreateResponse(record)
	}
	if bootstrap, ok := service.audit.(AuditBootstrapPort); ok {
		if err := bootstrap.BeginInitialMirrorLifecycle(ctx, record.ID, record.SetupID); err != nil {
			return nil, err
		}
	}
	material, err := service.openSetupMaterial(record.SetupID, record.SetupMaterialEncrypted)
	if err != nil {
		return nil, err
	}
	defer material.destroy()
	var storage StorageHandle
	if info, statErr := os.Lstat(record.DatabasePath); os.IsNotExist(statErr) {
		storage, err = service.storage.Create(ctx, record.DatabasePath, material.DEK)
	} else if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, faults.New(faults.CodeValidation, nil)
	} else {
		storage, err = service.storage.Open(ctx, record.DatabasePath, material.DEK)
	}
	if err != nil {
		return nil, err
	}
	keepOpen := false
	defer func() {
		if !keepOpen {
			_ = storage.Close()
		}
	}()
	if err := service.checkpoint("create.after_database"); err != nil {
		return nil, err
	}
	mutation := WorkspaceMutation{OperationID: record.SetupID, Kind: "CREATE", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: record.SetupSemanticHash, HeaderOperation: true}
	if !storage.HeaderOperationCommitted(ctx, record.SetupID, record.Version) {
		var administrator *tammyv1.User
		if err := storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, transactionRecord *workspaceRecord) error {
			if bootstrap, ok := service.audit.(AuditBootstrapPort); ok && material.InitialHeader.Audit == nil {
				metadata, bootstrapErr := bootstrap.BootstrapWorkspaceAudit(ctx, executor, record.ID, material.DEK, service.clock.Now().UTC())
				if bootstrapErr != nil {
					return bootstrapErr
				}
				material.InitialHeader.Audit = metadata.Clone()
			}
			var bootstrapErr error
			administrator, bootstrapErr = service.identity.BootstrapAdministratorWithin(ctx, executor, record.SetupID,
				request.AdministratorUsername, request.AdministratorDisplayName, request.AdministratorPassword.Utf8)
			if bootstrapErr != nil {
				return bootstrapErr
			}
			transactionRecord.OwnerUserID = administrator.Id
			transactionRecord.SetupPhase = "database"
			return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
		}); err != nil {
			return nil, err
		}
		record.OwnerUserID = administrator.Id
		record.SetupPhase = "database"
	} else {
		authoritative, loadErr := storage.LoadWorkspaceRecord(ctx)
		if loadErr != nil || authoritative.ID != record.ID || authoritative.SetupSemanticHash != record.SetupSemanticHash {
			return nil, ErrWorkspaceNotFound
		}
		if terminalSetupRecord(authoritative) {
			if err := service.convergeTerminalSetup(ctx, record, authoritative); err != nil {
				return nil, err
			}
			return nil, faults.New(faults.CodeValidation, nil)
		}
		record.OwnerUserID = authoritative.OwnerUserID
		record.SetupPhase = authoritative.SetupPhase
		if bootstrap, ok := service.audit.(AuditBootstrapPort); ok && material.InitialHeader.Audit == nil {
			metadata, bootstrapErr := bootstrap.LoadWorkspaceAuditHeader(ctx, storage.Database(), record.ID)
			if bootstrapErr != nil {
				return nil, bootstrapErr
			}
			material.InitialHeader.Audit = metadata.Clone()
		}
	}
	if err := service.checkpoint("create.after_database_commit"); err != nil {
		return nil, err
	}
	header, err := NewHeaderStore(record.HeaderPath, service.headerAuthKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !keepOpen {
			header.Close()
		}
	}()
	if info, statErr := os.Lstat(record.HeaderPath); os.IsNotExist(statErr) {
		if err := header.Initialize(material.InitialHeader); err != nil {
			return nil, err
		}
	} else if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrHeaderAuthentication
	}
	elected, err := header.Elect(func(operationID string, version uint64) bool {
		return storage.HeaderOperationCommitted(ctx, operationID, version)
	})
	if err != nil || elected.OperationID != record.SetupID || elected.WorkspaceID != record.ID || elected.Version != record.Version {
		return nil, ErrHeaderOperation
	}
	if err := service.checkpoint("create.after_header"); err != nil {
		return nil, err
	}
	record.SetupPhase = "ready"
	readyMutation := WorkspaceMutation{OperationID: record.SetupID, Kind: "CREATE_READY", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: record.SetupSemanticHash}
	if err := storage.CommitWorkspaceMutation(ctx, readyMutation, record, nil); err != nil {
		return nil, err
	}
	if err := service.repository.Save(ctx, record); err != nil {
		return nil, err
	}
	if err := service.admitRuntime(record.ID, &workspaceRuntime{dek: append([]byte(nil), material.DEK...), storage: storage, header: header,
		openedAt: service.clock.Now().UTC()}); err != nil {
		return nil, err
	}
	keepOpen = true
	return service.pendingCreateResponse(record)
}

func terminalSetupRecord(record workspaceRecord) bool {
	return record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY ||
		record.SetupPhase == "confirmed" || record.SetupPhase == "expiry_cleanup" || record.SetupPhase == "expired"
}

func (service *Service) convergeTerminalSetup(ctx context.Context, catalogue, authoritative workspaceRecord) error {
	if authoritative.ID != catalogue.ID || authoritative.SetupID != catalogue.SetupID ||
		authoritative.SetupSemanticHash != catalogue.SetupSemanticHash || !terminalSetupRecord(authoritative) {
		return ErrWorkspaceNotFound
	}
	Zero(authoritative.RecoveryDisplayEncrypted)
	authoritative.RecoveryDisplayEncrypted = nil
	authoritative.RecoveryGroupHashes = nil
	Zero(authoritative.SetupMaterialEncrypted)
	authoritative.SetupMaterialEncrypted = nil
	return service.repository.Save(ctx, authoritative)
}

func (service *Service) pendingCreateResponse(record workspaceRecord) (*connect.Response[tammyv1.CreateWorkspaceResponse], error) {
	display, err := service.openInstallation(record.RecoveryDisplayEncrypted, "setup/"+record.SetupID)
	if err != nil {
		return nil, ErrKeyMaterial
	}
	defer Zero(display)
	return service.createResponse(record, display), nil
}

func (service *Service) createResponse(record workspaceRecord, display []byte) *connect.Response[tammyv1.CreateWorkspaceResponse] {
	return connect.NewResponse(&tammyv1.CreateWorkspaceResponse{Workspace: service.projection(record),
		RecoverySecret: &tammyv1.OneTimeSecretOutput{Utf8: append([]byte(nil), display...)},
		ExpiresAt:      timestamppb.New(time.Unix(record.SetupExpires, 0).UTC())})
}

func (service *Service) sealSetupMaterial(setupID string, material setupMaterial) ([]byte, error) {
	payload, err := json.Marshal(material)
	if err != nil {
		return nil, ErrKeyMaterial
	}
	defer Zero(payload)
	return service.sealInstallation(payload, "setup-material/"+setupID)
}

func (service *Service) openSetupMaterial(setupID string, encrypted []byte) (*setupMaterial, error) {
	payload, err := service.openInstallation(encrypted, "setup-material/"+setupID)
	if err != nil {
		return nil, ErrKeyMaterial
	}
	defer Zero(payload)
	var material setupMaterial
	if err := json.Unmarshal(payload, &material); err != nil || len(material.DEK) != DEKSize || material.InitialHeader.OperationID != setupID {
		material.destroy()
		return nil, ErrKeyMaterial
	}
	return &material, nil
}

func (service *Service) ConfirmRecovery(ctx context.Context, request *connect.Request[tammyv1.ConfirmRecoveryRequest]) (*connect.Response[tammyv1.ConfirmRecoveryResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || !ids.IsCanonicalV7(request.Msg.SetupId) || len(request.Msg.Confirmations) < 2 || len(request.Msg.Confirmations) > 8 {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	record, err := service.repository.BySetup(ctx, request.Msg.SetupId)
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	semantic, structurallyValid := service.recoveryConfirmationSemantic(request.Msg)
	valid := structurallyValid
	seen := make(map[uint32]bool, len(request.Msg.Confirmations))
	for _, confirmation := range request.Msg.Confirmations {
		if confirmation == nil || int(confirmation.GroupIndex) >= len(record.RecoveryGroupHashes) || seen[confirmation.GroupIndex] {
			valid = false
			continue
		}
		seen[confirmation.GroupIndex] = true
		digest := service.recoveryGroupHash(record.ID, int(confirmation.GroupIndex), confirmation.Value)
		if subtle.ConstantTimeCompare(digest, record.RecoveryGroupHashes[confirmation.GroupIndex]) != 1 {
			valid = false
		}
	}
	if record.State == tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY {
		authoritative, reconciled, committed, reconcileErr := service.reconcileCommittedRecoveryConfirmation(ctx, record, semantic)
		if reconcileErr != nil {
			return nil, faults.New(faults.CodeAuthenticationRequired, nil)
		}
		if reconciled {
			return connect.NewResponse(&tammyv1.ConfirmRecoveryResponse{Workspace: service.projection(authoritative)}), nil
		}
		if committed {
			return nil, faults.New(faults.CodeAuthenticationRequired, nil)
		}
		if !valid {
			decision, journalErr := service.attempts.Failure("workspace_recovery_confirmation", record.ID,
				AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute})
			if journalErr != nil {
				return nil, journalErr
			}
			if decision.AttemptCount >= 5 {
				if err := service.expirePendingSetup(ctx, record); err != nil {
					return nil, err
				}
			}
			return nil, faults.New(faults.CodeAuthenticationRequired, nil)
		}
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY {
		authoritative, bound, boundErr := service.terminalRecoveryConfirmationBound(ctx, record, semantic, request.Msg.TerminalReplayProof)
		if boundErr != nil {
			return nil, boundErr
		}
		if !structurallyValid || !bound {
			return nil, faults.New(faults.CodeAuthenticationRequired, nil)
		}
		return connect.NewResponse(&tammyv1.ConfirmRecoveryResponse{Workspace: service.projection(authoritative)}), nil
	}
	if !service.clock.Now().UTC().Before(time.Unix(record.SetupExpires, 0)) {
		if err := service.expirePendingSetup(ctx, record); err != nil {
			return nil, err
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if err := service.requireRuntimeAdmission(record.ID, true); err != nil {
		return nil, err
	}
	runtime := service.active[record.ID]
	if runtime == nil || runtime.storage == nil {
		if err := service.openPendingRuntime(ctx, record); err != nil {
			return nil, faults.New(faults.CodeAuthenticationRequired, nil)
		}
		runtime = service.active[record.ID]
	}
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED
	record.SetupPhase = "confirmed"
	Zero(record.RecoveryDisplayEncrypted)
	record.RecoveryDisplayEncrypted = nil
	record.RecoveryGroupHashes = nil
	Zero(record.SetupMaterialEncrypted)
	record.SetupMaterialEncrypted = nil
	record.SetupConfirmationHash = semantic
	if err := service.attempts.Success("workspace_recovery_confirmation", record.ID); err != nil {
		return nil, err
	}
	mutation := WorkspaceMutation{OperationID: record.SetupID, Kind: "RECOVERY_CONFIRMATION", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: semantic}
	if err := runtime.storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, _ *workspaceRecord) error {
		return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
	}); err != nil {
		return nil, err
	}
	if err := service.checkpoint("confirm_recovery.after_database_commit"); err != nil {
		return nil, err
	}
	if err := service.repository.Save(ctx, record); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ConfirmRecoveryResponse{Workspace: service.projection(record)}), nil
}

// reconcileCommittedRecoveryConfirmation treats the authenticated SQLCipher
// aggregate as authoritative when confirmation committed but the installation
// catalogue did not. It deliberately runs before deadline cleanup so a stale
// catalogue can never delete a workspace that already crossed the commit point.
func (service *Service) reconcileCommittedRecoveryConfirmation(ctx context.Context, catalogue workspaceRecord, semantic string) (workspaceRecord, bool, bool, error) {
	if catalogue.SetupPhase != "ready" {
		return catalogue, false, false, nil
	}
	if runtime := service.active[catalogue.ID]; runtime != nil && runtime.storage != nil && runtime.header != nil {
		elected, err := runtime.header.Elect(func(operationID string, version uint64) bool {
			return runtime.storage.HeaderOperationCommitted(ctx, operationID, version)
		})
		if err != nil || elected.OperationID != catalogue.SetupID || elected.WorkspaceID != catalogue.ID || elected.Version != catalogue.Version {
			return catalogue, false, false, ErrHeaderOperation
		}
		authoritative, err := runtime.storage.LoadWorkspaceRecord(ctx)
		if err != nil {
			return catalogue, false, false, err
		}
		retained, committed := committedRecoveryConfirmation(catalogue, authoritative)
		if !committed || retained != semantic {
			return authoritative, false, committed, nil
		}
		if err := service.convergeTerminalSetup(ctx, catalogue, authoritative); err != nil {
			return catalogue, false, true, err
		}
		return authoritative, true, true, nil
	}
	if len(catalogue.SetupMaterialEncrypted) == 0 {
		return catalogue, false, false, ErrKeyMaterial
	}
	material, err := service.openSetupMaterial(catalogue.SetupID, catalogue.SetupMaterialEncrypted)
	if err != nil {
		return catalogue, false, false, err
	}
	defer material.destroy()
	if material.InitialHeader.OperationID != catalogue.SetupID || material.InitialHeader.WorkspaceID != catalogue.ID ||
		material.InitialHeader.Version != catalogue.Version {
		return catalogue, false, false, ErrKeyMaterial
	}
	for _, path := range []string{catalogue.DatabasePath, catalogue.HeaderPath} {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return catalogue, false, false, ErrKeyMaterial
		}
	}
	storage, err := service.storage.Open(ctx, catalogue.DatabasePath, material.DEK)
	if err != nil {
		return catalogue, false, false, ErrKeyMaterial
	}
	header, err := NewHeaderStore(catalogue.HeaderPath, service.headerAuthKey)
	if err != nil {
		_ = storage.Close()
		return catalogue, false, false, ErrKeyMaterial
	}
	keepOpen := false
	defer func() {
		if !keepOpen {
			header.Close()
			_ = storage.Close()
		}
	}()
	elected, err := header.Elect(func(operationID string, version uint64) bool {
		return storage.HeaderOperationCommitted(ctx, operationID, version)
	})
	if err != nil || elected.OperationID != material.InitialHeader.OperationID || elected.WorkspaceID != material.InitialHeader.WorkspaceID ||
		elected.Version != material.InitialHeader.Version {
		return catalogue, false, false, ErrHeaderOperation
	}
	authoritative, err := storage.LoadWorkspaceRecord(ctx)
	if err != nil {
		return catalogue, false, false, err
	}
	if authoritative.ID != catalogue.ID || authoritative.SetupID != catalogue.SetupID || authoritative.Version != catalogue.Version ||
		authoritative.SetupSemanticHash != catalogue.SetupSemanticHash {
		return catalogue, false, false, ErrWorkspaceNotFound
	}
	retained, committed := committedRecoveryConfirmation(catalogue, authoritative)
	if !committed || retained != semantic {
		return authoritative, false, committed, nil
	}
	if err := service.convergeTerminalSetup(ctx, catalogue, authoritative); err != nil {
		return catalogue, false, true, err
	}
	if len(service.active) == 0 {
		if err := service.admitRuntime(catalogue.ID, &workspaceRuntime{dek: append([]byte(nil), material.DEK...), storage: storage, header: header,
			openedAt: service.clock.Now().UTC()}); err != nil {
			return catalogue, false, true, err
		}
		keepOpen = true
	}
	return authoritative, true, true, nil
}

func committedRecoveryConfirmation(catalogue, authoritative workspaceRecord) (string, bool) {
	retained := authoritative.SetupConfirmationHash
	committed := authoritative.ID == catalogue.ID && authoritative.SetupID == catalogue.SetupID &&
		authoritative.Version == catalogue.Version && authoritative.SetupSemanticHash == catalogue.SetupSemanticHash &&
		authoritative.SetupPhase == "confirmed" && retained != "" &&
		(authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED ||
			authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED)
	return retained, committed
}

func (service *Service) terminalRecoveryConfirmationBound(ctx context.Context, record workspaceRecord, semantic string,
	proof *tammyv1.WorkspaceUnlockProof) (workspaceRecord, bool, error) {
	if record.SetupConfirmationHash != semantic || record.SetupPhase != "confirmed" {
		return record, false, nil
	}
	if runtime := service.active[record.ID]; runtime != nil && runtime.storage != nil && runtime.header != nil {
		current, err := runtime.header.Elect(func(operationID string, version uint64) bool {
			return runtime.storage.HeaderOperationCommitted(ctx, operationID, version)
		})
		authoritative, loadErr := runtime.storage.LoadWorkspaceRecord(ctx)
		return authoritative, err == nil && loadErr == nil && terminalRecoveryAuthoritativeBound(record, authoritative, current, semantic), nil
	}
	if proof == nil {
		return record, false, nil
	}
	_, passphraseAttempt := proof.Proof.(*tammyv1.WorkspaceUnlockProof_Passphrase)
	policy := workspaceUnlockAttemptPolicy()
	if passphraseAttempt {
		decision, err := service.attempts.Status(workspaceUnlockAttemptScope, record.ID, policy)
		if err != nil {
			return record, false, err
		}
		if decision.CoolingDown(service.clock.Now()) {
			return record, false, nil
		}
	}
	for _, path := range []string{record.DatabasePath, record.HeaderPath} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return record, false, nil
		}
	}
	header, err := NewHeaderStore(record.HeaderPath, service.headerAuthKey)
	if err != nil {
		return record, false, nil
	}
	defer header.Close()
	var dek []byte
	var storage StorageHandle
	var current HeaderSlot
	var rememberedUntil time.Time
	rememberedAttempt := false
	switch selected := proof.Proof.(type) {
	case *tammyv1.WorkspaceUnlockProof_Passphrase:
		if selected.Passphrase == nil {
			break
		}
		slots, slotErr := header.Slots()
		if slotErr != nil {
			break
		}
		sort.Slice(slots[:], func(i, j int) bool { return slots[i].Version > slots[j].Version })
		for _, candidate := range slots {
			if candidate.Version == 0 || candidate.WorkspaceID != record.ID {
				continue
			}
			candidateDEK, unwrapErr := UnwrapWithPassphrase(service.passwords, selected.Passphrase.Utf8,
				candidate.PassphraseWrap, record.ID, candidate.Version)
			if unwrapErr != nil {
				continue
			}
			candidateStorage, openErr := service.storage.Open(ctx, record.DatabasePath, candidateDEK)
			if openErr != nil {
				Zero(candidateDEK)
				continue
			}
			elected, electionErr := header.Elect(func(operationID string, version uint64) bool {
				return candidateStorage.HeaderOperationCommitted(ctx, operationID, version)
			})
			if electionErr == nil && elected.WorkspaceID == record.ID && elected.Version == candidate.Version &&
				elected.OperationID == candidate.OperationID {
				dek, storage, current = candidateDEK, candidateStorage, elected
				break
			}
			_ = candidateStorage.Close()
			Zero(candidateDEK)
		}
	case *tammyv1.WorkspaceUnlockProof_UseRememberedWorkspace:
		if !selected.UseRememberedWorkspace {
			break
		}
		rememberedAttempt = true
		candidateDEK, expires, useErr := service.remembered.Use(record.ID)
		if useErr != nil {
			break
		}
		rememberedUntil = expires
		candidateStorage, openErr := service.storage.Open(ctx, record.DatabasePath, candidateDEK)
		if openErr != nil {
			Zero(candidateDEK)
			break
		}
		elected, electionErr := header.Elect(func(operationID string, version uint64) bool {
			return candidateStorage.HeaderOperationCommitted(ctx, operationID, version)
		})
		if electionErr != nil {
			_ = candidateStorage.Close()
			Zero(candidateDEK)
			break
		}
		dek, storage, current = candidateDEK, candidateStorage, elected
	}
	if storage == nil || len(dek) != DEKSize {
		Zero(dek)
		if passphraseAttempt {
			if _, err := service.attempts.Failure(workspaceUnlockAttemptScope, record.ID, policy); err != nil {
				return record, false, err
			}
		}
		return record, false, nil
	}
	defer func() {
		_ = storage.Close()
		Zero(dek)
	}()
	authoritative, err := storage.LoadWorkspaceRecord(ctx)
	if err != nil || !terminalRecoveryAuthoritativeBound(record, authoritative, current, semantic) {
		if passphraseAttempt {
			if _, journalErr := service.attempts.Failure(workspaceUnlockAttemptScope, record.ID, policy); journalErr != nil {
				return record, false, journalErr
			}
		}
		return record, false, nil
	}
	if rememberedAttempt && (authoritative.RememberedUntil == 0 || authoritative.RememberedUntil != rememberedUntil.Unix() ||
		!service.clock.Now().UTC().Before(time.Unix(authoritative.RememberedUntil, 0).UTC())) {
		if err := service.remembered.Forget(record.ID); err != nil {
			return record, false, err
		}
		return record, false, nil
	}
	if passphraseAttempt {
		if err := service.attempts.Success(workspaceUnlockAttemptScope, record.ID); err != nil {
			return record, false, err
		}
	}
	return authoritative, true, nil
}

func terminalRecoveryAuthoritativeBound(catalogue, authoritative workspaceRecord, current HeaderSlot, semantic string) bool {
	terminalState := authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED ||
		authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED ||
		authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED
	return terminalState && catalogue.State == authoritative.State && catalogue.Version == authoritative.Version &&
		catalogue.SetupConfirmationHash == semantic && authoritative.SetupConfirmationHash == semantic &&
		recoveryAuthoritativeBound(catalogue, authoritative, current)
}

func (service *Service) openPendingRuntime(ctx context.Context, record workspaceRecord) error {
	if err := service.requireRuntimeAdmission(record.ID, true); err != nil {
		return err
	}
	if record.SetupPhase != "ready" || len(record.SetupMaterialEncrypted) == 0 {
		return ErrKeyMaterial
	}
	material, err := service.openSetupMaterial(record.SetupID, record.SetupMaterialEncrypted)
	if err != nil {
		return err
	}
	defer material.destroy()
	storage, err := service.storage.Open(ctx, record.DatabasePath, material.DEK)
	if err != nil {
		return err
	}
	header, err := NewHeaderStore(record.HeaderPath, service.headerAuthKey)
	if err != nil {
		_ = storage.Close()
		return err
	}
	elected, err := header.Elect(func(operationID string, version uint64) bool {
		return storage.HeaderOperationCommitted(ctx, operationID, version)
	})
	if err != nil || elected.WorkspaceID != record.ID || elected.OperationID != record.SetupID || elected.Version != record.Version {
		header.Close()
		_ = storage.Close()
		return ErrHeaderOperation
	}
	return service.admitRuntime(record.ID, &workspaceRuntime{dek: append([]byte(nil), material.DEK...), storage: storage, header: header,
		openedAt: service.clock.Now().UTC()})
}

func (service *Service) LockWorkspace(ctx context.Context, _ *connect.Request[tammyv1.LockWorkspaceRequest]) (*connect.Response[tammyv1.LockWorkspaceResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	id, runtime, found := service.soleRuntime()
	if !found || runtime == nil || runtime.storage == nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	record, err := service.repository.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	authoritative, err := runtime.storage.LoadWorkspaceRecord(ctx)
	if err != nil || authoritative.ID != record.ID {
		service.closeRuntime(record.ID)
		return nil, ErrWorkspaceNotFound
	}
	record = authoritative
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
	operationID, err := service.ids.New()
	if err != nil {
		service.closeRuntime(record.ID)
		return nil, err
	}
	mutation := WorkspaceMutation{OperationID: operationID, Kind: "LOCK", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: service.operationSemantic("lock", []byte(operationID))}
	if err := runtime.storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, _ *workspaceRecord) error {
		if err := service.identity.InvalidateAllSessionsWithin(ctx, executor); err != nil {
			return err
		}
		return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
	}); err != nil {
		// Fail closed even when a dependency prevents the logical unit from
		// committing; the next proved unlock will reconcile SQLCipher.
		if saveErr := service.repository.Save(ctx, record); saveErr != nil {
			service.closeRuntime(record.ID)
			return nil, saveErr
		}
		service.closeRuntime(record.ID)
		return nil, err
	}
	if err := service.repository.Save(ctx, record); err != nil {
		service.closeRuntime(record.ID)
		return nil, err
	}
	service.closeRuntime(record.ID)
	return connect.NewResponse(&tammyv1.LockWorkspaceResponse{Workspace: service.projection(record)}), nil
}

func (service *Service) UnlockWorkspace(ctx context.Context, request *connect.Request[tammyv1.UnlockWorkspaceRequest]) (
	response *connect.Response[tammyv1.UnlockWorkspaceResponse], resultErr error,
) {
	service.mu.Lock()
	defer service.mu.Unlock()
	rememberedWritePending := false
	rememberedWorkspaceID := ""
	defer func() {
		if !rememberedWritePending {
			return
		}
		if cleanupErr := service.remembered.Forget(rememberedWorkspaceID); cleanupErr != nil {
			response = nil
			resultErr = errors.Join(resultErr, cleanupErr)
		}
	}()
	if request == nil || request.Msg == nil || request.Msg.WorkspaceFile == nil || request.Msg.Proof == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	path, err := service.capabilities.Resolve(ctx, request.Msg.WorkspaceFile, CapabilityWorkspaceFile)
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	record, err := service.repository.ByPath(ctx, path)
	if err != nil || record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if err := service.requireRuntimeAdmission(record.ID, false); err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	policy := workspaceUnlockAttemptPolicy()
	decision, journalErr := service.attempts.Status(workspaceUnlockAttemptScope, record.ID, policy)
	if journalErr != nil {
		return nil, journalErr
	}
	if decision.CoolingDown(service.clock.Now()) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	header, err := NewHeaderStore(record.HeaderPath, service.headerAuthKey)
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	var dek []byte
	var storage StorageHandle
	rememberedProof := false
	var rememberedUntil time.Time
	switch proof := request.Msg.Proof.Proof.(type) {
	case *tammyv1.WorkspaceUnlockProof_Passphrase:
		if proof.Passphrase == nil {
			break
		}
		slots, slotErr := header.Slots()
		if slotErr != nil {
			break
		}
		sort.Slice(slots[:], func(i, j int) bool { return slots[i].Version > slots[j].Version })
		for _, candidate := range slots {
			if candidate.Version == 0 {
				continue
			}
			candidateDEK, unwrapErr := UnwrapWithPassphrase(service.passwords, proof.Passphrase.Utf8, candidate.PassphraseWrap, record.ID, candidate.Version)
			if unwrapErr != nil {
				continue
			}
			candidateStorage, openErr := service.storage.Open(ctx, record.DatabasePath, candidateDEK)
			if openErr != nil {
				Zero(candidateDEK)
				continue
			}
			elected, electionErr := header.Elect(func(operationID string, version uint64) bool {
				return candidateStorage.HeaderOperationCommitted(ctx, operationID, version)
			})
			if electionErr == nil && elected.Version == candidate.Version {
				dek, storage = candidateDEK, candidateStorage
				break
			}
			_ = candidateStorage.Close()
			Zero(candidateDEK)
		}
	case *tammyv1.WorkspaceUnlockProof_UseRememberedWorkspace:
		if proof.UseRememberedWorkspace {
			rememberedProof = true
			dek, rememberedUntil, err = service.remembered.Use(record.ID)
			if err == nil {
				storage, err = service.storage.Open(ctx, record.DatabasePath, dek)
			}
			if err == nil {
				_, err = header.Elect(func(operationID string, version uint64) bool {
					return storage.HeaderOperationCommitted(ctx, operationID, version)
				})
			}
			if err != nil {
				if storage != nil {
					_ = storage.Close()
				}
				Zero(dek)
				dek, storage = nil, nil
			}
		}
	}
	if storage == nil || len(dek) != DEKSize {
		header.Close()
		if _, journalErr := service.attempts.Failure(workspaceUnlockAttemptScope, record.ID, policy); journalErr != nil {
			return nil, journalErr
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	authoritative, err := storage.LoadWorkspaceRecord(ctx)
	if err != nil || authoritative.ID != record.ID || authoritative.SetupID != record.SetupID ||
		authoritative.SetupSemanticHash != record.SetupSemanticHash {
		_ = storage.Close()
		header.Close()
		Zero(dek)
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if rememberedProof && (authoritative.RememberedUntil == 0 || authoritative.RememberedUntil != rememberedUntil.Unix() ||
		!service.clock.Now().UTC().Before(time.Unix(authoritative.RememberedUntil, 0).UTC())) {
		_ = storage.Close()
		header.Close()
		Zero(dek)
		if err := service.remembered.Forget(record.ID); err != nil {
			return nil, err
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if err := service.attempts.Success(workspaceUnlockAttemptScope, record.ID); err != nil {
		_ = storage.Close()
		header.Close()
		Zero(dek)
		return nil, err
	}
	record = authoritative
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED
	if request.Msg.RememberWorkspace != nil && *request.Msg.RememberWorkspace && !rememberedProof {
		until, rememberErr := service.remembered.Remember(record.ID, dek, request.Msg.RememberWorkspace)
		if rememberErr != nil {
			_ = storage.Close()
			header.Close()
			Zero(dek)
			return nil, rememberErr
		}
		rememberedWritePending = true
		rememberedWorkspaceID = record.ID
		record.RememberedUntil = until.Unix()
	}
	operationID, err := service.ids.New()
	if err != nil {
		_ = storage.Close()
		header.Close()
		Zero(dek)
		return nil, err
	}
	mutation := WorkspaceMutation{OperationID: operationID, Kind: "UNLOCK", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: service.operationSemantic("unlock", []byte(operationID))}
	if err := storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, _ *workspaceRecord) error {
		if err := service.identity.InvalidateAllSessionsWithin(ctx, executor); err != nil {
			return err
		}
		return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
	}); err != nil {
		_ = storage.Close()
		header.Close()
		Zero(dek)
		return nil, err
	}
	rememberedWritePending = false
	if err := service.repository.Save(ctx, record); err != nil {
		_ = storage.Close()
		header.Close()
		Zero(dek)
		return nil, err
	}
	if err := service.admitRuntime(record.ID, &workspaceRuntime{dek: dek, storage: storage, header: header, openedAt: service.clock.Now().UTC()}); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.UnlockWorkspaceResponse{Workspace: service.projection(record)}), nil
}

func (service *Service) ForgetRememberedWorkspace(ctx context.Context, request *connect.Request[tammyv1.ForgetRememberedWorkspaceRequest]) (*connect.Response[tammyv1.ForgetRememberedWorkspaceResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || !ids.IsCanonicalV7(request.Msg.WorkspaceId) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	activeID, runtime, found := service.soleRuntime()
	if !found || activeID != request.Msg.WorkspaceId || runtime == nil || runtime.storage == nil || runtime.header == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	catalogue, err := service.repository.ByID(ctx, activeID)
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	current, err := runtime.header.Elect(func(operationID string, version uint64) bool {
		return runtime.storage.HeaderOperationCommitted(ctx, operationID, version)
	})
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	authoritative, err := runtime.storage.LoadWorkspaceRecord(ctx)
	if err != nil || !activeRememberedWorkspaceBound(activeID, catalogue, authoritative, current) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if err := service.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, err
	}
	if err := service.remembered.Forget(activeID); err != nil {
		return nil, err
	}
	converged := mergeActiveAuthoritativeRecord(catalogue, authoritative)
	if authoritative.RememberedUntil == 0 {
		if !reflect.DeepEqual(catalogue, converged) {
			if err := service.repository.Save(ctx, converged); err != nil {
				return nil, err
			}
		}
		return connect.NewResponse(&tammyv1.ForgetRememberedWorkspaceResponse{Workspace: service.projection(converged)}), nil
	}
	if err := service.checkpoint("forget_remembered_workspace.before_db_commit"); err != nil {
		return nil, err
	}
	operationID, err := service.ids.New()
	if err != nil {
		return nil, err
	}
	authoritative.RememberedUntil = 0
	mutation := WorkspaceMutation{OperationID: operationID, Kind: "REMEMBERED_WORKSPACE_FORGOTTEN", WorkspaceID: authoritative.ID,
		Version: authoritative.Version, SemanticHash: service.operationSemantic("forget_remembered_workspace", []byte(operationID), []byte(activeID))}
	if err := runtime.storage.CommitWorkspaceMutation(ctx, mutation, authoritative, func(executor MutationExecutor, _ *workspaceRecord) error {
		transactionRecord, err := loadWorkspaceRecordFrom(ctx, executor)
		if err != nil || !sameRememberedWorkspaceMutationSource(converged, transactionRecord) {
			return faults.New(faults.CodeAuthenticationRequired, nil)
		}
		if err := service.identity.RequireActiveSessionWithin(ctx, executor, request.Msg.Authentication); err != nil {
			return err
		}
		return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
	}); err != nil {
		return nil, err
	}
	if err := service.checkpoint("forget_remembered_workspace.after_database_commit"); err != nil {
		return nil, err
	}
	converged = mergeActiveAuthoritativeRecord(catalogue, authoritative)
	if err := service.repository.Save(ctx, converged); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ForgetRememberedWorkspaceResponse{Workspace: service.projection(converged)}), nil
}

func activeRememberedWorkspaceBound(activeID string, catalogue, authoritative workspaceRecord, current HeaderSlot) bool {
	return activeID != "" && catalogue.ID == activeID && authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED &&
		recoveryAuthoritativeBound(catalogue, authoritative, current)
}

func sameRememberedWorkspaceMutationSource(expected, actual workspaceRecord) bool {
	return expected.ID != "" && actual.ID == expected.ID && actual.Version == expected.Version &&
		actual.State == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED && actual.SetupID == expected.SetupID &&
		actual.SetupSemanticHash == expected.SetupSemanticHash && actual.SetupPhase == expected.SetupPhase &&
		actual.DatabasePath == expected.DatabasePath && actual.HeaderPath == expected.HeaderPath &&
		actual.RememberedUntil == expected.RememberedUntil
}

func mergeActiveAuthoritativeRecord(catalogue, authoritative workspaceRecord) workspaceRecord {
	converged := mergeAuthoritativeRecord(catalogue, authoritative)
	converged.State = authoritative.State
	converged.TrustState = authoritative.TrustState
	converged.SetupPhase = authoritative.SetupPhase
	return converged
}

func (service *Service) GetWorkspaceState(ctx context.Context, request *connect.Request[tammyv1.GetWorkspaceStateRequest]) (*connect.Response[tammyv1.GetWorkspaceStateResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.WorkspaceId == nil {
		return connect.NewResponse(&tammyv1.GetWorkspaceStateResponse{}), nil
	}
	record, err := service.repository.ByID(ctx, *request.Msg.WorkspaceId)
	if errors.Is(err, ErrWorkspaceNotFound) {
		return connect.NewResponse(&tammyv1.GetWorkspaceStateResponse{}), nil
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.GetWorkspaceStateResponse{Workspace: service.projection(record)}), nil
}

// ActiveDatabase returns the database owned by the single active workspace.
// Application composition uses this narrow activation hook to bind generated
// workspace-scoped handlers after CreateWorkspace or UnlockWorkspace succeeds.
// Callers must not retain the database beyond the active workspace lifetime.
func (service *Service) ActiveDatabase(workspaceID string) (*sqlcipher.Database, error) {
	if service == nil {
		return nil, ErrWorkspaceNotFound
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if !ids.IsCanonicalV7(workspaceID) {
		return nil, ErrWorkspaceNotFound
	}
	runtime := service.active[workspaceID]
	if runtime == nil || runtime.storage == nil || runtime.storage.Database() == nil {
		return nil, ErrWorkspaceNotFound
	}
	return runtime.storage.Database(), nil
}

// SessionStartedWithin is the workspace half of a sign-in unit of work. It
// intentionally does not acquire service.mu: identity invokes it from inside
// the SQLCipher transaction while holding its own mutex, and taking the
// workspace mutex here would invert LockWorkspace's lock order.
func (service *Service) SessionStartedWithin(ctx context.Context, executor MutationExecutor, sessionID string) error {
	if executor == nil || !ids.IsCanonicalV7(sessionID) {
		return ErrWorkspaceNotFound
	}
	record, err := loadWorkspaceRecordFrom(ctx, executor)
	if err != nil {
		return err
	}
	if record.State == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
		return nil
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED || record.SetupPhase != "confirmed" {
		return ErrWorkspaceNotFound
	}
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED
	payload, err := json.Marshal(record)
	if err != nil {
		return ErrWorkspaceNotFound
	}
	result, err := executor.ExecContext(ctx, `UPDATE workspace_metadata SET value = ?, revision = revision + 1, updated_at = ? WHERE key = ?`,
		payload, service.clock.Now().UTC().Format(time.RFC3339Nano), authoritativeWorkspaceRecordKey)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ErrWorkspaceNotFound
	}
	mutation := WorkspaceMutation{OperationID: sessionID, Kind: "SESSION_STARTED", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: service.operationSemantic("session_started", []byte(sessionID))}
	if err := service.audit.AppendWorkspaceMutation(ctx, executor, mutation); err != nil {
		return err
	}
	return nil
}

// SessionStartedAuditedWithin finalizes the initial mirror only after every
// sign-in audit append in the shared identity transaction has advanced the
// chain, so the baseline is the exact committed head.
func (service *Service) SessionStartedAuditedWithin(ctx context.Context, executor MutationExecutor) error {
	if executor == nil {
		return ErrWorkspaceNotFound
	}
	record, err := loadWorkspaceRecordFrom(ctx, executor)
	if err != nil || record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED || record.SetupPhase != "confirmed" {
		return ErrWorkspaceNotFound
	}
	if bootstrap, ok := service.audit.(AuditBootstrapPort); ok {
		return bootstrap.EstablishInitialMirror(ctx, executor, record.ID, record.SetupID)
	}
	return nil
}

// SessionStartedCommitted converges the installation catalogue after the
// authoritative session/workspace transaction commits. A crash before this
// step is repaired by ExpireUnauthenticated from the same authoritative row.
func (service *Service) SessionStartedCommitted(ctx context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.active) != 1 {
		return ErrWorkspaceNotFound
	}
	for id, runtime := range service.active {
		if runtime == nil || runtime.storage == nil {
			return ErrWorkspaceNotFound
		}
		authoritative, err := runtime.storage.LoadWorkspaceRecord(ctx)
		if err != nil || authoritative.ID != id || authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
			return ErrWorkspaceNotFound
		}
		return service.repository.Save(ctx, authoritative)
	}
	return ErrWorkspaceNotFound
}

// ExpireUnauthenticated closes any workspace that remained open without an
// application-user session for the full five-minute pre-authentication window.
func (service *Service) ExpireUnauthenticated(ctx context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.clock.Now().UTC()
	for id, runtime := range service.active {
		if runtime == nil || now.Before(runtime.openedAt.Add(5*time.Minute)) {
			continue
		}
		record, err := service.repository.ByID(ctx, id)
		if err != nil {
			service.closeRuntime(id)
			return err
		}
		if runtime.storage == nil {
			return service.failClosedRuntime(ctx, record, ErrWorkspaceNotFound)
		}
		authoritative, err := runtime.storage.LoadWorkspaceRecord(ctx)
		if err != nil || authoritative.ID != record.ID {
			if err == nil {
				err = ErrWorkspaceNotFound
			}
			return service.failClosedRuntime(ctx, record, err)
		}
		if authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
			if err := service.repository.Save(ctx, authoritative); err != nil {
				return err
			}
			continue
		}
		if authoritative.State == tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
			return service.failClosedRuntime(ctx, authoritative, nil)
		}
		if authoritative.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
			return service.failClosedRuntime(ctx, record, ErrWorkspaceNotFound)
		}
		authoritative.State = tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
		operationID, err := service.ids.New()
		if err != nil {
			return service.failClosedRuntime(ctx, authoritative, err)
		}
		mutation := WorkspaceMutation{OperationID: operationID, Kind: "LOCK", WorkspaceID: authoritative.ID,
			Version: authoritative.Version, SemanticHash: service.operationSemantic("lock", []byte(operationID))}
		if err := runtime.storage.CommitWorkspaceMutation(ctx, mutation, authoritative, func(executor MutationExecutor, _ *workspaceRecord) error {
			if err := service.identity.InvalidateAllSessionsWithin(ctx, executor); err != nil {
				return err
			}
			return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
		}); err != nil {
			return service.failClosedRuntime(ctx, authoritative, err)
		}
		return service.failClosedRuntime(ctx, authoritative, nil)
	}
	return nil
}

func (service *Service) failClosedRuntime(ctx context.Context, record workspaceRecord, cause error) error {
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
	saveErr := service.repository.Save(ctx, record)
	service.closeRuntime(record.ID)
	return errors.Join(cause, saveErr)
}

func (service *Service) ChangePassphrase(ctx context.Context, request *connect.Request[tammyv1.ChangePassphraseRequest]) (*connect.Response[tammyv1.ChangePassphraseResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		!ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.Msg.WorkspaceId) ||
		request.Msg.ExpectedVersion == 0 || request.Msg.CurrentPassphrase == nil || request.Msg.NewPassphrase == nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	record, err := service.repository.ByID(ctx, request.Msg.WorkspaceId)
	if err != nil {
		return nil, faults.New(faults.CodeNotFound, nil)
	}
	catalogueRecord := cloneWorkspaceRecord(record)
	runtime := service.active[record.ID]
	if runtime == nil || runtime.storage == nil || runtime.header == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if authoritative, loadErr := runtime.storage.LoadWorkspaceRecord(ctx); loadErr == nil && authoritative.ID == record.ID {
		record = mergeAuthoritativeRecord(record, authoritative)
	}
	semantic := service.operationSemantic("change_passphrase", []byte(request.Msg.WorkspaceId), []byte(fmtVersion(request.Msg.ExpectedVersion)),
		request.Msg.CurrentPassphrase.Utf8, request.Msg.NewPassphrase.Utf8)
	if err := service.identity.RequireAdministratorReadOnly(ctx, request.Msg.CommandContext.Authentication); err != nil {
		return nil, err
	}
	if retained, ok := record.OperationHashes[request.Msg.CommandContext.IdempotencyKey]; ok {
		if retained != semantic {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		if record.OperationActors[request.Msg.CommandContext.IdempotencyKey] != request.Msg.CommandContext.Authentication.ActorUserId {
			return nil, faults.New(faults.CodePermissionDenied, nil)
		}
		if _, err := runtime.header.Elect(func(operationID string, version uint64) bool {
			return runtime.storage.HeaderOperationCommitted(ctx, operationID, version)
		}); err != nil {
			return nil, err
		}
		if catalogueRecord.Version != record.Version ||
			catalogueRecord.OperationHashes[request.Msg.CommandContext.IdempotencyKey] != retained ||
			catalogueRecord.OperationActors[request.Msg.CommandContext.IdempotencyKey] != record.OperationActors[request.Msg.CommandContext.IdempotencyKey] {
			if err := service.repository.Save(ctx, record); err != nil {
				return nil, err
			}
		}
		return connect.NewResponse(&tammyv1.ChangePassphraseResponse{Workspace: service.projection(record)}), nil
	}
	if record.Version != request.Msg.ExpectedVersion {
		return nil, faults.New(faults.CodeStaleVersion, nil)
	}
	if err := authorisation.ValidateFreshFactor(request.Msg.CommandContext.FreshFactor, "change_passphrase", service.clock.Now()); err != nil {
		return nil, err
	}
	current, err := runtime.header.Elect(func(operationID string, version uint64) bool {
		return runtime.storage.HeaderOperationCommitted(ctx, operationID, version)
	})
	if err != nil {
		return nil, err
	}
	provedDEK, err := UnwrapWithPassphrase(service.passwords, request.Msg.CurrentPassphrase.Utf8, current.PassphraseWrap, record.ID, current.Version)
	if err != nil || subtle.ConstantTimeCompare(provedDEK, runtime.dek) != 1 {
		Zero(provedDEK)
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	Zero(provedDEK)
	history := append([]PasswordVerifier{current.PassphraseWrap.Verifier}, current.PassphraseHistory...)
	if service.passwords.Reused(request.Msg.NewPassphrase.Utf8, history) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	nextVersion := current.Version + 1
	passphraseWrap, err := WrapWithPassphrase(service.passwords, request.Msg.NewPassphrase.Utf8, runtime.dek, record.ID, nextVersion)
	if err != nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	recoveryVersion := current.RecoveryVersion
	if recoveryVersion == 0 {
		recoveryVersion = current.Version
	}
	next := HeaderSlot{Version: nextVersion, OperationID: request.Msg.CommandContext.IdempotencyKey, WorkspaceID: record.ID,
		PassphraseWrap: passphraseWrap, RecoveryWrap: current.RecoveryWrap, RecoveryVersion: recoveryVersion,
		PassphraseHistory: RetainPasswordHistory(current.PassphraseWrap.Verifier, current.PassphraseHistory, 3), Audit: current.Audit.Clone()}
	if err := service.remembered.Forget(record.ID); err != nil {
		return nil, err
	}
	if err := runtime.header.Prepare(next); err != nil {
		return nil, err
	}
	record.RememberedUntil = 0
	record.Version = nextVersion
	if record.OperationHashes == nil {
		record.OperationHashes = make(map[string]string)
	}
	record.OperationHashes[request.Msg.CommandContext.IdempotencyKey] = semantic
	if record.OperationActors == nil {
		record.OperationActors = make(map[string]string)
	}
	record.OperationActors[request.Msg.CommandContext.IdempotencyKey] = request.Msg.CommandContext.Authentication.ActorUserId
	mutation := WorkspaceMutation{OperationID: next.OperationID, Kind: "PASSPHRASE_CHANGE", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: semantic, HeaderOperation: true}
	if err := service.checkpoint("change_passphrase.before_db_commit"); err != nil {
		return nil, err
	}
	if err := runtime.storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, _ *workspaceRecord) error {
		authoritative, err := loadWorkspaceRecordFrom(ctx, executor)
		if err != nil {
			return err
		}
		if authoritative.ID != record.ID || authoritative.Version != request.Msg.ExpectedVersion {
			return faults.New(faults.CodeStaleVersion, nil)
		}
		if request.Msg.CommandContext.Authentication.ActorUserId != authoritative.OwnerUserID {
			return faults.New(faults.CodePermissionDenied, nil)
		}
		if err := service.identity.RequireAdministratorWithin(ctx, executor, request.Msg.CommandContext.Authentication); err != nil {
			return err
		}
		if err := service.identity.ConsumeFreshFactorWithin(ctx, executor, request.Msg.CommandContext.Authentication,
			request.Msg.CommandContext.FreshFactor, "change_passphrase"); err != nil {
			return err
		}
		return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
	}); err != nil {
		return nil, err
	}
	if err := service.checkpoint("change_passphrase.before_slot_activation"); err != nil {
		return nil, err
	}
	if err := runtime.header.Activate(next.OperationID); err != nil {
		return nil, err
	}
	if err := service.checkpoint("change_passphrase.after_slot_activation"); err != nil {
		return nil, err
	}
	if err := service.repository.Save(ctx, record); err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ChangePassphraseResponse{Workspace: service.projection(record)}), nil
}

func (service *Service) RecoverWorkspace(ctx context.Context, request *connect.Request[tammyv1.RecoverWorkspaceRequest]) (*connect.Response[tammyv1.RecoverWorkspaceResponse], error) {
	return service.recoverWorkspace(ctx, request, "RECOVERY", nil, nil, tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED)
}

func (service *Service) recoverWorkspace(ctx context.Context, request *connect.Request[tammyv1.RecoverWorkspaceRequest], operationKind string,
	dependent, replay func(MutationExecutor) error, finalState tammyv1.WorkspaceState) (*connect.Response[tammyv1.RecoverWorkspaceResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if operationKind != "RECOVERY" && operationKind != "ADMIN_RECOVERY" {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	if request == nil || request.Msg == nil || !ids.IsCanonicalV7(request.Msg.RecoveryOperationId) ||
		request.Msg.WorkspaceFile == nil || request.Msg.RecoverySecret == nil || request.Msg.NewPassphrase == nil {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	path, err := service.capabilities.Resolve(ctx, request.Msg.WorkspaceFile, CapabilityWorkspaceFile)
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	record, err := service.repository.ByPath(ctx, path)
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	semantic := service.operationSemantic(strings.ToLower(operationKind), []byte(record.ID), request.Msg.RecoverySecret.Utf8, request.Msg.NewPassphrase.Utf8)
	retainedInCatalogue, catalogueRetained := record.OperationHashes[request.Msg.RecoveryOperationId]
	if catalogueRetained && retainedInCatalogue != semantic {
		return nil, faults.New(faults.CodeIdempotencyConflict, nil)
	}
	if runtime := service.active[record.ID]; runtime != nil {
		if !catalogueRetained || runtime.storage == nil || runtime.header == nil {
			return nil, faults.New(faults.CodeValidation, nil)
		}
		current, electErr := runtime.header.Elect(func(operationID string, version uint64) bool {
			return runtime.storage.HeaderOperationCommitted(ctx, operationID, version)
		})
		authoritative, loadErr := runtime.storage.LoadWorkspaceRecord(ctx)
		if electErr != nil || loadErr != nil || !recoveryAuthoritativeBound(record, authoritative, current) {
			return nil, faults.New(faults.CodeAuthenticationRequired, nil)
		}
		retained, ok := authoritative.OperationHashes[request.Msg.RecoveryOperationId]
		if !ok || retained != semantic {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		if replay != nil {
			database := runtime.storage.Database()
			if database == nil {
				return nil, faults.New(faults.CodeAuthenticationRequired, nil)
			}
			if err := replay(database); err != nil {
				return nil, err
			}
		}
		converged := mergeAuthoritativeRecord(record, authoritative)
		if !reflect.DeepEqual(record, converged) {
			if err := service.repository.Save(ctx, converged); err != nil {
				return nil, err
			}
		}
		return connect.NewResponse(&tammyv1.RecoverWorkspaceResponse{Workspace: service.projection(converged)}), nil
	}
	if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	otherRuntimeActive := len(service.active) != 0
	policy := AttemptPolicy{Limit: 5, Window: 15 * time.Minute, Cooldown: 15 * time.Minute}
	decision, journalErr := service.attempts.Status("workspace_recovery", record.ID, policy)
	if journalErr != nil {
		return nil, journalErr
	}
	if decision.CoolingDown(service.clock.Now()) {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	header, err := NewHeaderStore(record.HeaderPath, service.headerAuthKey)
	if err != nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	slots, err := header.Slots()
	if err != nil {
		header.Close()
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	sort.Slice(slots[:], func(i, j int) bool { return slots[i].Version > slots[j].Version })
	var dek []byte
	var storage StorageHandle
	var current HeaderSlot
	for _, candidate := range slots {
		if candidate.Version == 0 {
			continue
		}
		recoveryVersion := candidate.RecoveryVersion
		if recoveryVersion == 0 {
			recoveryVersion = candidate.Version
		}
		candidateDEK, unwrapErr := UnwrapWithRecovery(request.Msg.RecoverySecret.Utf8, candidate.RecoveryWrap, record.ID, recoveryVersion)
		if unwrapErr != nil {
			continue
		}
		candidateStorage, openErr := service.storage.Open(ctx, record.DatabasePath, candidateDEK)
		if openErr != nil {
			Zero(candidateDEK)
			continue
		}
		elected, electionErr := header.Elect(func(operationID string, version uint64) bool {
			return candidateStorage.HeaderOperationCommitted(ctx, operationID, version)
		})
		if electionErr == nil && elected.Version == candidate.Version && elected.OperationID == candidate.OperationID &&
			elected.WorkspaceID == record.ID && candidate.WorkspaceID == record.ID {
			dek, storage, current = candidateDEK, candidateStorage, elected
			break
		}
		_ = candidateStorage.Close()
		Zero(candidateDEK)
	}
	if storage == nil {
		header.Close()
		if _, journalErr := service.attempts.Failure("workspace_recovery", record.ID, policy); journalErr != nil {
			return nil, journalErr
		}
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	fail := func(err error) (*connect.Response[tammyv1.RecoverWorkspaceResponse], error) {
		_ = storage.Close()
		header.Close()
		Zero(dek)
		return nil, err
	}
	authoritative, loadErr := storage.LoadWorkspaceRecord(ctx)
	if loadErr != nil || !recoveryAuthoritativeBound(record, authoritative, current) {
		return fail(faults.New(faults.CodeAuthenticationRequired, nil))
	}
	authoritativeBefore := cloneWorkspaceRecord(authoritative)
	catalogueBeforeMerge := cloneWorkspaceRecord(record)
	record = mergeAuthoritativeRecord(record, authoritative)
	if retained, ok := record.OperationHashes[request.Msg.RecoveryOperationId]; ok {
		if retained != semantic {
			return fail(faults.New(faults.CodeIdempotencyConflict, nil))
		}
		if replay != nil {
			database := storage.Database()
			if database == nil {
				return fail(faults.New(faults.CodeAuthenticationRequired, nil))
			}
			if err := replay(database); err != nil {
				return fail(err)
			}
		}
		if otherRuntimeActive {
			if !reflect.DeepEqual(catalogueBeforeMerge, record) {
				if err := service.repository.Save(ctx, record); err != nil {
					return fail(err)
				}
			}
			_ = storage.Close()
			header.Close()
			Zero(dek)
			result := cloneWorkspaceRecord(record)
			result.State = finalState
			return connect.NewResponse(&tammyv1.RecoverWorkspaceResponse{Workspace: service.projection(result)}), nil
		}
		record.State = finalState
		if err := service.repository.Save(ctx, record); err != nil {
			return fail(err)
		}
		if finalState != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
			if err := service.admitRuntime(record.ID, &workspaceRuntime{dek: dek, storage: storage, header: header, openedAt: service.clock.Now().UTC()}); err != nil {
				return fail(err)
			}
		} else {
			_ = storage.Close()
			header.Close()
			Zero(dek)
		}
		return connect.NewResponse(&tammyv1.RecoverWorkspaceResponse{Workspace: service.projection(record)}), nil
	}
	if otherRuntimeActive {
		return fail(faults.New(faults.CodeValidation, nil))
	}
	history := append([]PasswordVerifier{current.PassphraseWrap.Verifier}, current.PassphraseHistory...)
	if service.passwords.Reused(request.Msg.NewPassphrase.Utf8, history) {
		return fail(faults.New(faults.CodeValidation, nil))
	}
	nextVersion := current.Version + 1
	passphraseWrap, err := WrapWithPassphrase(service.passwords, request.Msg.NewPassphrase.Utf8, dek, record.ID, nextVersion)
	if err != nil {
		return fail(faults.New(faults.CodeValidation, nil))
	}
	recoveryWrap, err := WrapWithRecovery(request.Msg.RecoverySecret.Utf8, dek, record.ID, nextVersion)
	if err != nil {
		return fail(faults.New(faults.CodeAuthenticationRequired, nil))
	}
	next := HeaderSlot{Version: nextVersion, RecoveryVersion: nextVersion, OperationID: request.Msg.RecoveryOperationId,
		WorkspaceID: record.ID, PassphraseWrap: passphraseWrap, RecoveryWrap: recoveryWrap,
		PassphraseHistory: RetainPasswordHistory(current.PassphraseWrap.Verifier, current.PassphraseHistory, 3), Audit: current.Audit.Clone()}
	if err := service.attempts.Success("workspace_recovery", record.ID); err != nil {
		return fail(err)
	}
	if err := service.remembered.Forget(record.ID); err != nil {
		return fail(err)
	}
	if err := header.Prepare(next); err != nil {
		return fail(err)
	}
	record.RememberedUntil = 0
	record.Version = nextVersion
	record.State = finalState
	if record.OperationHashes == nil {
		record.OperationHashes = make(map[string]string)
	}
	record.OperationHashes[request.Msg.RecoveryOperationId] = semantic
	mutation := WorkspaceMutation{OperationID: next.OperationID, Kind: operationKind, WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: semantic, HeaderOperation: true}
	checkpointPrefix := strings.ToLower(operationKind)
	if err := service.checkpoint(checkpointPrefix + ".before_db_commit"); err != nil {
		return fail(err)
	}
	if err := storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, _ *workspaceRecord) error {
		transactionAuthoritative, err := loadWorkspaceRecordFrom(ctx, executor)
		if err != nil || !reflect.DeepEqual(transactionAuthoritative, authoritativeBefore) {
			return faults.New(faults.CodeStaleVersion, nil)
		}
		if dependent != nil {
			if err := dependent(executor); err != nil {
				return err
			}
		}
		return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
	}); err != nil {
		return fail(err)
	}
	if err := service.checkpoint(checkpointPrefix + ".before_slot_activation"); err != nil {
		return fail(err)
	}
	if err := header.Activate(next.OperationID); err != nil {
		return fail(err)
	}
	if err := service.checkpoint(checkpointPrefix + ".after_slot_activation"); err != nil {
		return fail(err)
	}
	if err := service.repository.Save(ctx, record); err != nil {
		return fail(err)
	}
	if finalState == tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED {
		_ = storage.Close()
		header.Close()
		Zero(dek)
	} else {
		if err := service.admitRuntime(record.ID, &workspaceRuntime{dek: dek, storage: storage, header: header, openedAt: service.clock.Now().UTC()}); err != nil {
			return fail(err)
		}
	}
	return connect.NewResponse(&tammyv1.RecoverWorkspaceResponse{Workspace: service.projection(record)}), nil
}

func recoveryAuthoritativeBound(catalogue, authoritative workspaceRecord, current HeaderSlot) bool {
	return catalogue.ID != "" && authoritative.ID == catalogue.ID &&
		authoritative.SetupID == catalogue.SetupID && authoritative.SetupSemanticHash == catalogue.SetupSemanticHash &&
		authoritative.SetupPhase == "confirmed" && authoritative.DatabasePath == catalogue.DatabasePath &&
		authoritative.HeaderPath == catalogue.HeaderPath && current.WorkspaceID == catalogue.ID &&
		current.Version == authoritative.Version && current.Version > 0 && current.OperationID != ""
}

// RecoverAdministrator verifies the workspace recovery secret, resets the
// selected administrator through the identity owner, invalidates application
// authentication, and closes the workspace before returning.
func (service *Service) RecoverAdministrator(ctx context.Context, request *tammyv1.RecoverAdministratorRequest) (*tammyv1.User, error) {
	if request == nil || request.RecoverySecret == nil || request.NewWorkspacePassphrase == nil || request.NewUserPassword == nil ||
		!ids.IsCanonicalV7(request.RecoveryOperationId) || !validRecoveryUsername(request.AdministratorUsername) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	var user *tammyv1.User
	resetAdministrator := func(executor MutationExecutor) error {
		var resetErr error
		user, resetErr = service.identity.BreakGlassResetAdministratorWithin(ctx, executor, request.RecoveryOperationId,
			request.AdministratorUsername, request.NewUserPassword.Utf8)
		return resetErr
	}
	_, err := service.recoverWorkspace(ctx, connect.NewRequest(&tammyv1.RecoverWorkspaceRequest{
		RecoveryOperationId: request.RecoveryOperationId,
		WorkspaceFile:       request.WorkspaceFile,
		RecoverySecret:      request.RecoverySecret,
		NewPassphrase:       request.NewWorkspacePassphrase,
	}), "ADMIN_RECOVERY", resetAdministrator, resetAdministrator, tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return user, nil
}

func validRecoveryUsername(username string) bool {
	return username != "" && len([]rune(username)) <= 128 && username == strings.TrimSpace(username)
}

func (service *Service) TransferOwnership(ctx context.Context, request *connect.Request[tammyv1.TransferOwnershipRequest]) (*connect.Response[tammyv1.TransferOwnershipResponse], error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil ||
		!ids.IsCanonicalV7(request.Msg.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.Msg.WorkspaceId) ||
		!ids.IsCanonicalV7(request.Msg.TargetUserId) || request.Msg.ExpectedVersion == 0 ||
		request.Msg.AcknowledgeVerificationEffect == nil || !*request.Msg.AcknowledgeVerificationEffect {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	record, err := service.repository.ByID(ctx, request.Msg.WorkspaceId)
	if err != nil {
		return nil, faults.New(faults.CodeNotFound, nil)
	}
	runtime := service.active[record.ID]
	if runtime != nil && runtime.storage != nil {
		if authoritative, loadErr := runtime.storage.LoadWorkspaceRecord(ctx); loadErr == nil && authoritative.ID == record.ID {
			record = mergeAuthoritativeRecord(record, authoritative)
		}
	}
	semantic := service.operationSemantic("transfer_ownership", []byte(request.Msg.WorkspaceId),
		[]byte(fmtVersion(request.Msg.ExpectedVersion)), []byte(request.Msg.TargetUserId), []byte{1})
	operationKey := request.Msg.CommandContext.IdempotencyKey
	authentication := request.Msg.CommandContext.Authentication
	if err := service.identity.RequireAdministratorReadOnly(ctx, authentication); err != nil {
		boundActor, actorBound := record.OperationActors[operationKey]
		boundSession, sessionBound := record.OperationSessions[operationKey]
		if !actorBound || !sessionBound {
			return nil, err
		}
		if replayErr := service.identity.ValidateAdministratorReplayBinding(ctx, authentication, boundActor, boundSession); replayErr != nil {
			return nil, replayErr
		}
	}
	if retained, ok := record.OperationHashes[operationKey]; ok {
		if authentication == nil || record.OperationActors[operationKey] != authentication.ActorUserId {
			return nil, faults.New(faults.CodePermissionDenied, nil)
		}
		if record.OperationSessions[operationKey] != authentication.SessionId {
			return nil, faults.New(faults.CodeAuthenticationRequired, nil)
		}
		if retained != semantic {
			return nil, faults.New(faults.CodeIdempotencyConflict, nil)
		}
		return connect.NewResponse(&tammyv1.TransferOwnershipResponse{Workspace: service.projection(record)}), nil
	}
	if runtime == nil || runtime.storage == nil {
		return nil, faults.New(faults.CodeAuthenticationRequired, nil)
	}
	if record.Version != request.Msg.ExpectedVersion {
		return nil, faults.New(faults.CodeStaleVersion, nil)
	}
	if request.Msg.CommandContext.Authentication == nil || request.Msg.CommandContext.Authentication.ActorUserId != record.OwnerUserID {
		return nil, faults.New(faults.CodePermissionDenied, nil)
	}
	if !service.identity.IsActiveAdministrator(ctx, request.Msg.TargetUserId) {
		return nil, faults.New(faults.CodeValidation, nil)
	}
	if err := authorisation.ValidateFreshFactor(request.Msg.CommandContext.FreshFactor, "ownership_transfer", service.clock.Now()); err != nil {
		return nil, err
	}
	priorOwner := record.OwnerUserID
	record.OwnerUserID = request.Msg.TargetUserId
	record.Version++
	record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
	if record.OperationHashes == nil {
		record.OperationHashes = make(map[string]string)
	}
	record.OperationHashes[operationKey] = semantic
	if record.OperationActors == nil {
		record.OperationActors = make(map[string]string)
	}
	record.OperationActors[operationKey] = request.Msg.CommandContext.Authentication.ActorUserId
	if record.OperationSessions == nil {
		record.OperationSessions = make(map[string]string)
	}
	record.OperationSessions[operationKey] = request.Msg.CommandContext.Authentication.SessionId
	mutation := WorkspaceMutation{OperationID: operationKey, Kind: "OWNERSHIP_TRANSFER", WorkspaceID: record.ID,
		Version: record.Version, SemanticHash: semantic}
	impact := OwnershipImpact{WorkspaceID: record.ID, PriorOwnerUserID: priorOwner, NextOwnerUserID: record.OwnerUserID,
		AcknowledgeVerificationEffect: true}
	if err := service.checkpoint("ownership_transfer.before_db_commit"); err != nil {
		return nil, err
	}
	if err := runtime.storage.CommitWorkspaceMutation(ctx, mutation, record, func(executor MutationExecutor, _ *workspaceRecord) error {
		authoritative, err := loadWorkspaceRecordFrom(ctx, executor)
		if err != nil {
			return err
		}
		if authoritative.ID != record.ID || authoritative.Version != request.Msg.ExpectedVersion {
			return faults.New(faults.CodeStaleVersion, nil)
		}
		if request.Msg.CommandContext.Authentication.ActorUserId != authoritative.OwnerUserID {
			return faults.New(faults.CodePermissionDenied, nil)
		}
		if err := service.identity.RequireAdministratorWithin(ctx, executor, request.Msg.CommandContext.Authentication); err != nil {
			return err
		}
		active, err := service.identity.IsActiveAdministratorWithin(ctx, executor, request.Msg.TargetUserId)
		if err != nil {
			return err
		}
		if !active {
			return faults.New(faults.CodeValidation, nil)
		}
		if err := service.organisations.ApplyOwnershipTransfer(ctx, executor, impact); err != nil {
			return err
		}
		if err := service.identity.ConsumeFreshFactorWithin(ctx, executor, request.Msg.CommandContext.Authentication,
			request.Msg.CommandContext.FreshFactor, "ownership_transfer"); err != nil {
			return err
		}
		if err := service.identity.InvalidateAllSessionsWithin(ctx, executor); err != nil {
			return err
		}
		return service.audit.AppendWorkspaceMutation(ctx, executor, mutation)
	}); err != nil {
		return nil, err
	}
	if err := service.repository.Save(ctx, record); err != nil {
		service.closeRuntime(record.ID)
		return nil, err
	}
	service.closeRuntime(record.ID)
	return connect.NewResponse(&tammyv1.TransferOwnershipResponse{Workspace: service.projection(record)}), nil
}

func (service *Service) operationSemantic(kind string, parts ...[]byte) string {
	digest := hmac.New(sha256.New, service.installKey)
	_, _ = digest.Write([]byte("tammy.workspace.operation.v1\x00" + kind + "\x00"))
	for _, part := range parts {
		_, _ = digest.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		_, _ = digest.Write(part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func (service *Service) recoveryConfirmationSemantic(request *tammyv1.ConfirmRecoveryRequest) (string, bool) {
	if request == nil || request.SetupId == "" || len(request.Confirmations) < 2 || len(request.Confirmations) > 8 {
		return "", false
	}
	parts := make([][]byte, 0, 1+len(request.Confirmations)*2)
	parts = append(parts, []byte(request.SetupId))
	seen := make(map[uint32]bool, len(request.Confirmations))
	for _, confirmation := range request.Confirmations {
		if confirmation == nil || confirmation.GroupIndex >= recoveryEncodedSize/recoveryGroupSize ||
			seen[confirmation.GroupIndex] || len(confirmation.Value) != recoveryGroupSize {
			return "", false
		}
		seen[confirmation.GroupIndex] = true
		index := confirmation.GroupIndex
		parts = append(parts, []byte{byte(index >> 24), byte(index >> 16), byte(index >> 8), byte(index)}, confirmation.Value)
	}
	return service.operationSemantic("confirm_recovery", parts...), true
}

func (service *Service) checkpoint(name string) error {
	if service.failures == nil {
		return nil
	}
	return service.failures.Check(name)
}

func mergeAuthoritativeRecord(catalogue, authoritative workspaceRecord) workspaceRecord {
	// Paths and transient open state remain installation-catalogue concerns;
	// committed aggregate fields come from the SQLCipher transaction record.
	catalogue.Version = authoritative.Version
	catalogue.SetupConfirmationHash = authoritative.SetupConfirmationHash
	catalogue.OwnerUserID = authoritative.OwnerUserID
	catalogue.RememberedUntil = authoritative.RememberedUntil
	catalogue.OperationHashes = authoritative.OperationHashes
	catalogue.OperationActors = authoritative.OperationActors
	catalogue.OperationSessions = authoritative.OperationSessions
	return catalogue
}

func fmtVersion(value uint64) string {
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

func (service *Service) closeRuntime(id string) {
	runtime := service.active[id]
	if runtime == nil {
		return
	}
	if runtime.storage != nil {
		_ = runtime.storage.Close()
	}
	if runtime.header != nil {
		runtime.header.Close()
	}
	Zero(runtime.dek)
	delete(service.active, id)
}

func (service *Service) admitRuntime(id string, candidate *workspaceRuntime) error {
	if existing := service.active[id]; existing != nil && existing == candidate {
		return nil
	}
	if id == "" || candidate == nil || candidate.storage == nil || candidate.header == nil || len(candidate.dek) != DEKSize || len(service.active) != 0 {
		discardRuntime(candidate)
		return faults.New(faults.CodeValidation, nil)
	}
	service.active[id] = candidate
	return nil
}

func (service *Service) requireRuntimeAdmission(id string, sameWorkspacePending bool) error {
	if len(service.active) == 0 {
		return nil
	}
	if sameWorkspacePending && len(service.active) == 1 && id != "" && service.active[id] != nil {
		return nil
	}
	return faults.New(faults.CodeValidation, nil)
}

func (service *Service) soleRuntime() (string, *workspaceRuntime, bool) {
	if len(service.active) != 1 {
		return "", nil, false
	}
	for id, runtime := range service.active {
		return id, runtime, true
	}
	return "", nil, false
}

func discardRuntime(runtime *workspaceRuntime) {
	if runtime == nil {
		return
	}
	if runtime.storage != nil {
		_ = runtime.storage.Close()
	}
	if runtime.header != nil {
		runtime.header.Close()
	}
	Zero(runtime.dek)
}

func (service *Service) expirePendingSetup(ctx context.Context, record workspaceRecord) error {
	if record.SetupPhase == "expired" {
		return nil
	}
	service.closeRuntime(record.ID)
	if record.SetupPhase != "expiry_cleanup" {
		record.SetupPhase = "expiry_cleanup"
		record.SetupExpiredAt = service.clock.Now().UTC().Unix()
		record.SetupCleanupDatabasePath = record.DatabasePath
		record.SetupCleanupHeaderPath = record.HeaderPath
		record.DatabasePath = ""
		record.HeaderPath = ""
		Zero(record.RecoveryDisplayEncrypted)
		record.RecoveryDisplayEncrypted = nil
		record.RecoveryGroupHashes = nil
		Zero(record.SetupMaterialEncrypted)
		record.SetupMaterialEncrypted = nil
		record.OwnerUserID = ""
		record.RememberedUntil = 0
		record.OperationHashes = nil
		record.OperationActors = nil
		record.OperationSessions = nil
		if err := service.repository.Save(ctx, record); err != nil {
			return err
		}
		if err := service.checkpoint("expire_pending.after_tombstone"); err != nil {
			return err
		}
	}
	return service.finishExpiredSetupCleanup(ctx, record)
}

func (service *Service) finishExpiredSetupCleanup(ctx context.Context, record workspaceRecord) error {
	service.closeRuntime(record.ID)
	if err := os.Remove(record.SetupCleanupHeaderPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, suffix := range []string{"-journal", "-shm", "-wal", ""} {
		if err := os.Remove(record.SetupCleanupDatabasePath + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	record.SetupPhase = "expired"
	record.SetupCleanupDatabasePath = ""
	record.SetupCleanupHeaderPath = ""
	return service.repository.Save(ctx, record)
}

func (service *Service) projection(record workspaceRecord) *tammyv1.Workspace {
	projection := &tammyv1.Workspace{Id: record.ID, Version: record.Version, State: record.State,
		TrustState: record.TrustState, DisplayName: record.DisplayName}
	if record.RememberedUntil > service.clock.Now().UTC().Unix() {
		projection.RememberedUntil = timestamppb.New(time.Unix(record.RememberedUntil, 0).UTC())
	}
	return projection
}

func (service *Service) setupSemantic(request *tammyv1.CreateWorkspaceRequest) string {
	digest := hmac.New(sha256.New, service.installKey)
	for _, part := range [][]byte{[]byte(request.Destination.CapabilityId), request.WorkspacePassphrase.Utf8,
		[]byte(request.AdministratorUsername), []byte(request.AdministratorDisplayName), request.AdministratorPassword.Utf8} {
		_, _ = digest.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		_, _ = digest.Write(part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func (service *Service) recoveryGroupHash(workspaceID string, index int, group []byte) []byte {
	digest := hmac.New(sha256.New, service.installKey)
	_, _ = digest.Write([]byte("tammy.recovery-group.v1\x00" + workspaceID + "\x00"))
	_, _ = digest.Write([]byte{byte(index)})
	_, _ = digest.Write(group)
	return digest.Sum(nil)
}

func (service *Service) sealInstallation(secret []byte, aad string) ([]byte, error) {
	block, err := aes.NewCipher(service.installKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, secret, []byte(aad))...), nil
}

func (service *Service) openInstallation(encrypted []byte, aad string) ([]byte, error) {
	block, err := aes.NewCipher(service.installKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrKeyMaterial
	}
	return aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(aad))
}
