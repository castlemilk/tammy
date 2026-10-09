//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Package testsupport builds authentic encrypted workspace fixtures that were
// created before ordinary audit bootstrap was installed.
package testsupport

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/identity"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

const (
	LegacyWorkspacePassphrase = "workspace-passphrase-long-enough"
	LegacyAdministratorName   = "admin@tammy.local"
	LegacyAdministratorPass   = "administrator-password-long-enough"
)

type LegacyWorkspaceFixture struct {
	Root        string
	WorkspaceID string
	Anchors     *workspace.MemoryAnchorStore
}

// CreateUnauditedLegacyWorkspace runs the real workspace creation and recovery
// workflow with the older mutation-audit port. The resulting database has a
// valid SQLCipher key, workspace header, encrypted catalogue and identity
// records, while its ordinary audit tables have never received a chain.
func CreateUnauditedLegacyWorkspace(ctx context.Context, root string) (LegacyWorkspaceFixture, error) {
	if ctx == nil || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return LegacyWorkspaceFixture{}, workspace.ErrWorkspaceNotFound
	}
	coreRoot := filepath.Join(root, "core")
	workspaceDirectory := filepath.Join(coreRoot, "workspace")
	if err := os.MkdirAll(workspaceDirectory, 0o700); err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	master, err := readOrCreateInstallationKey(filepath.Join(coreRoot, "installation.key"))
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	defer workspace.Zero(master)

	source := clock.Func(time.Now)
	generator, err := ids.NewGenerator(source, rand.Reader)
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	passwords, err := workspace.NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	anchors := workspace.NewMemoryAnchorStore()
	workspaceAttempts, err := workspace.NewAttemptJournal(filepath.Join(coreRoot, "workspace-attempts.journal"),
		deriveLocalKey(master, "workspace-attempts"), source, localAttemptAnchorID(master, "workspace"), anchors)
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	defer workspaceAttempts.Close()
	identityAttempts, err := workspace.NewAttemptJournal(filepath.Join(coreRoot, "identity-attempts.journal"),
		deriveLocalKey(master, "identity-attempts"), source, localAttemptAnchorID(master, "identity"), anchors)
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	defer identityAttempts.Close()
	remembered, err := workspace.NewRememberedKeyManager(workspace.NewMemorySecretStore(), source)
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	catalogue, err := workspace.NewFileRepository(filepath.Join(coreRoot, "workspace-catalogue.enc"), deriveLocalKey(master, "workspace-catalogue"))
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	identityService, err := identity.NewService(identity.Config{
		Repository: identity.NewMemoryRepository(), Passwords: passwords, Clock: source, Random: rand.Reader,
		IDs: generator, Attempts: identityAttempts, FactorEncryptionKey: deriveLocalKey(master, "identity-factor"),
		Audit: legacyIdentityAudit{}, SessionLifecycle: legacySessionLifecycle{},
	})
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	defer identityService.Close()
	databasePath := filepath.Join(workspaceDirectory, "tammy-workspace.db")
	service, err := workspace.NewService(workspace.Config{
		Repository:   catalogue,
		Capabilities: legacyCapabilityResolver{directory: workspaceDirectory, database: databasePath},
		Storage:      workspace.NewSQLCipherStorageFactory(12), Identity: identityService, Audit: legacyWorkspaceAudit{},
		OrganisationImpact: legacyOrganisationImpact{}, Passwords: passwords, RememberedKeys: remembered,
		Attempts: workspaceAttempts, Clock: source, IDs: generator,
		HeaderAuthenticationKey: deriveLocalKey(master, "workspace-header"),
		InstallationKey:         deriveLocalKey(master, "workspace-installation"),
	})
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	created, err := service.CreateWorkspace(ctx, connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId:               "01900f3c-7b2e-7cc4-98c4-dc0c0c070001",
		Destination:           &tammyv1.ApprovedFileRef{CapabilityId: "legacy-workspace-directory"},
		WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte(LegacyWorkspacePassphrase)},
		AdministratorUsername: LegacyAdministratorName, AdministratorDisplayName: "Tammy Admin",
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte(LegacyAdministratorPass)},
	}))
	if err != nil || created == nil || created.Msg == nil || created.Msg.Workspace == nil || created.Msg.RecoverySecret == nil {
		return LegacyWorkspaceFixture{}, errorsJoinWorkspaceSetup(err)
	}
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	if _, err := service.ConfirmRecovery(ctx, connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: "01900f3c-7b2e-7cc4-98c4-dc0c0c070001",
		Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]},
		},
	})); err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	if _, err := service.LockWorkspace(ctx, connect.NewRequest(&tammyv1.LockWorkspaceRequest{})); err != nil {
		return LegacyWorkspaceFixture{}, err
	}
	return LegacyWorkspaceFixture{Root: root, WorkspaceID: created.Msg.Workspace.Id, Anchors: anchors}, nil
}

type legacyCapabilityResolver struct{ directory, database string }

func (resolver legacyCapabilityResolver) Resolve(_ context.Context, reference *tammyv1.ApprovedFileRef, kind workspace.CapabilityKind) (string, error) {
	if reference == nil {
		return "", workspace.ErrWorkspaceNotFound
	}
	if kind == workspace.CapabilityDirectory && reference.CapabilityId == "legacy-workspace-directory" {
		return resolver.directory, nil
	}
	if kind == workspace.CapabilityWorkspaceFile && reference.CapabilityId == "legacy-workspace-file" {
		return resolver.database, nil
	}
	return "", workspace.ErrWorkspaceNotFound
}

type legacyWorkspaceAudit struct{}

func (legacyWorkspaceAudit) AppendWorkspaceMutation(context.Context, workspace.MutationExecutor, workspace.WorkspaceMutation) error {
	return nil
}

type legacyIdentityAudit struct{}

func (legacyIdentityAudit) Record(context.Context, workspace.MutationExecutor, string, string) error {
	return nil
}

type legacySessionLifecycle struct{}

func (legacySessionLifecycle) SessionStartedWithin(context.Context, workspace.MutationExecutor, string) error {
	return nil
}
func (legacySessionLifecycle) SessionStartedAuditedWithin(context.Context, workspace.MutationExecutor) error {
	return nil
}
func (legacySessionLifecycle) SessionStartedCommitted(context.Context) error { return nil }

type legacyOrganisationImpact struct{}

func (legacyOrganisationImpact) ApplyOwnershipTransfer(context.Context, workspace.MutationExecutor, workspace.OwnershipImpact) error {
	return nil
}

func readOrCreateInstallationKey(path string) ([]byte, error) {
	if payload, err := os.ReadFile(path); err == nil {
		if len(payload) != 32 {
			return nil, workspace.ErrKeyMaterial
		}
		return payload, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		workspace.Zero(key)
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		workspace.Zero(key)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		workspace.Zero(key)
		return nil, err
	}
	if err := file.Close(); err != nil {
		workspace.Zero(key)
		return nil, err
	}
	return key, nil
}

func deriveLocalKey(master []byte, domain string) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.local-composition.v1\x00"))
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(master)
	return digest.Sum(nil)
}

func localAttemptAnchorID(master []byte, purpose string) string {
	digest := hmac.New(sha256.New, master)
	_, _ = digest.Write([]byte("tammy.local-attempt-anchor.v1\x00"))
	_, _ = digest.Write([]byte(purpose))
	return "local-" + purpose + "/" + hex.EncodeToString(digest.Sum(nil))
}

func errorsJoinWorkspaceSetup(err error) error {
	if err != nil {
		return err
	}
	return workspace.ErrWorkspaceNotFound
}
