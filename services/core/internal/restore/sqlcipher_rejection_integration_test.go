//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestSQLCipherRestoreRejectionsPreserveExactActiveDatabase(t *testing.T) {
	for _, test := range []struct {
		name               string
		rejectProof        bool
		wrongSigningTrust  bool
		wrongSchema        bool
		breakStagedTrigger bool
	}{
		{name: "proof_authentication", rejectProof: true},
		{name: "signature", wrongSigningTrust: true},
		{name: "schema", wrongSchema: true},
		{name: "invariant", breakStagedTrigger: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			const (
				workspaceID = "018f0000-0000-7000-8000-000000000001"
				operationID = "018f0000-0000-7000-8000-000000000099"
				actorID     = "018f0000-0000-7000-8000-000000000010"
				sessionID   = "018f0000-0000-7000-8000-000000000012"
				replayKey   = "018f0000-0000-7000-8000-000000000011"
			)
			createdAt := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
			directory := filepath.Join(t.TempDir(), "private")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			activePath := filepath.Join(directory, "workspace.db")
			archivedPath := filepath.Join(t.TempDir(), "archived.db")
			key := bytes.Repeat([]byte{0x47}, sqlcipher.KeySize)
			defer zeroBytes(key)

			active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active-proof-boundary")
			activeHead, _ := seedArchivedAuditAndSessions(t, ctx, active, workspaceID, key, createdAt)
			if err := active.Checkpoint(ctx); err != nil || active.Close() != nil {
				t.Fatalf("close active fixture: %v", err)
			}
			activeBefore, err := os.ReadFile(activePath)
			if err != nil {
				t.Fatal(err)
			}
			activeBeforeHash := sha256.Sum256(activeBefore)

			archived := createRestoreDatabaseFixture(t, ctx, archivedPath, key, "archived-proof-boundary")
			archivedHead, signingKey := seedArchivedAuditAndSessions(t, ctx, archived, workspaceID, key, createdAt)
			schemaVersion, migrationHash := restoreSchemaMetadata(t, ctx, archived)
			if test.breakStagedTrigger {
				if _, err := archived.ExecContext(ctx, `DROP TRIGGER command_idempotency_no_update`); err != nil {
					t.Fatal(err)
				}
			}
			if err := archived.Checkpoint(ctx); err != nil || archived.Close() != nil {
				t.Fatalf("close archived fixture: %v", err)
			}
			archivedBytes, err := os.ReadFile(archivedPath)
			if err != nil {
				t.Fatal(err)
			}
			privateKey, err := audit.DecryptSigningKey(signingKey, key)
			if err != nil {
				t.Fatal(err)
			}
			defer audit.Zero(privateKey)
			auditRoot, err := audit.SigningLineageRootFingerprint(workspaceID, signingKey.KeyID, signingKey.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			manifestSchema := schemaVersion
			if test.wrongSchema {
				manifestSchema++
			}
			passphrase := []byte("correct horse battery staple")
			archive, err := backup.Seal(backup.ArchiveInput{WorkspaceID: workspaceID, SchemaVersion: manifestSchema,
				AppVersion: "0.1.0", AuditGeneration: 2, AuditSequence: 0, AuditHead: archivedHead[:], AuditRoot: auditRoot[:],
				SigningKeyID: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch,
				WorkspaceHeaderHash: bytes.Repeat([]byte{0x44}, sha256.Size), MigrationManifestHash: migrationHash,
				Objects: []backup.Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1,
					Bytes: archivedBytes}}}, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x7d}, 256)))
			if err != nil {
				t.Fatal(err)
			}

			authDatabase, err := sqlcipher.Open(ctx, activePath, key)
			if err != nil {
				t.Fatal(err)
			}
			transactions, err := NewSQLCipherRestoreAuthenticationTransactions(authDatabase)
			if err != nil {
				t.Fatal(err)
			}
			proofs, err := NewTransactionalRestoreProofVerifier(TransactionalRestoreProofVerifierConfig{
				Transactions: transactions, Now: func() time.Time { return createdAt.Add(time.Minute) },
				Authenticator: restoreAuthenticatorPortFunc(func(_ context.Context, _ RestoreAuthenticationExecutor,
					request RestoreAuthenticationRequest,
				) (AuthenticatedRestoreProof, error) {
					if test.rejectProof {
						return AuthenticatedRestoreProof{}, errors.New("identity detail must stay hidden")
					}
					return validAuthenticatedRestoreProof(RestoreAuthenticationModeNormal, workspaceID, actorID,
						sessionID, request.ReplayKey, "018f0000-0000-7000-8000-000000000077",
						request.IssuedAt), nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}

			adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
				StagingDirectory: directory, Key: key,
				NewID:                  func() (string, error) { return "018f0000-0000-7000-8000-000000000055", nil },
				NewReceiptID:           func() (string, error) { return "018f0000-0000-7000-8000-000000000066", nil },
				NewEventID:             func() (string, error) { return "018f0000-0000-7000-8000-000000000067", nil },
				Now:                    func() time.Time { return createdAt.Add(2 * time.Minute) },
				Random:                 bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)),
				AuditSchemaFingerprint: bytes.Repeat([]byte{0x72}, sha256.Size)})
			if err != nil {
				t.Fatal(err)
			}
			journal, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(),
				Now: func() time.Time { return createdAt.Add(2 * time.Minute) }})
			if err != nil {
				t.Fatal(err)
			}
			preArchives, err := NewPreRestoreArchiveService(PreRestoreArchiveServiceConfig{Directory: directory, DEK: key,
				Snapshots: preRestoreSnapshotSourceFunc(func(context.Context, string, *RestoreAuthorization) ([]byte, error) {
					return append([]byte(nil), activeBefore...), nil
				}), NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000088", nil },
				Now:    func() time.Time { return createdAt.Add(90 * time.Second) },
				Random: bytes.NewReader(bytes.Repeat([]byte{0x74}, 256))})
			if err != nil {
				t.Fatal(err)
			}
			registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "workspace", Version: 1,
				Validator: concreteValidatorFunc(func(_ context.Context, input ValidationInput) error {
					if len(input.Objects) != 1 || input.Objects[0].Path != "database/workspace.db" {
						return ErrProviderRegistry
					}
					return nil
				})}})
			if err != nil {
				t.Fatal(err)
			}
			trustPublicKey := append(ed25519.PublicKey(nil), signingKey.PublicKey...)
			if test.wrongSigningTrust {
				wrongSeed := sha256.Sum256([]byte("wrong restore signing trust"))
				trustPublicKey = ed25519.NewKeyFromSeed(wrongSeed[:]).Public().(ed25519.PublicKey)
			}
			effects := &concretePostRestoreEffects{}
			service, err := NewService(ServiceConfig{Proofs: proofs,
				Trust: trustResolverFunc(func(context.Context, string) (backup.TrustAnchor, error) {
					return backup.TrustAnchor{WorkspaceID: workspaceID, AuditGeneration: 2, AuditRoot: auditRoot[:],
						SigningKeyID: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch, PublicKey: trustPublicKey}, nil
				}), Providers: registry, Journal: journal, PreRestoreArchives: preArchives, Stager: adapter,
				StagedFinalizer: adapter, StagedVerifier: adapter, Swapper: adapter, PostSwapVerifier: adapter,
				MachineCredentials: effects, Mirror: effects})
			if err != nil {
				t.Fatal(err)
			}
			result, restoreErr := service.Restore(ctx, RestoreRequest{OperationID: operationID, WorkspaceID: workspaceID,
				Archive: archive, Passphrase: passphrase, Proof: &AdminTOTPProof{AdminUserID: actorID,
					Password: []byte("administrator-password"), TOTP: "123456", IssuedAt: createdAt,
					ReplayKey: replayKey}})
			if result != nil || !errors.Is(restoreErr, ErrRestore) {
				t.Fatalf("rejected restore result=%#v error=%v", result, restoreErr)
			}
			if effects.revoked != 0 || effects.mirrored != 0 {
				t.Fatalf("post-swap effects reached on rejection: %#v", effects)
			}
			if err := adapter.Close(); err != nil || preArchives.Close() != nil || journal.Close() != nil || authDatabase.Close() != nil {
				t.Fatalf("close rejection fixture: %v", err)
			}

			activeAfter, err := os.ReadFile(activePath)
			if err != nil {
				t.Fatal(err)
			}
			activeAfterHash := sha256.Sum256(activeAfter)
			if activeAfterHash != activeBeforeHash || !bytes.Equal(activeAfter, activeBefore) {
				t.Fatalf("active bytes changed: before=%x after=%x", activeBeforeHash, activeAfterHash)
			}
			reopened, err := sqlcipher.Open(ctx, activePath, key)
			if err != nil {
				t.Fatal(err)
			}
			header, err := audit.LoadChainHeader(ctx, reopened, workspaceID, 0)
			if err != nil || header.Generation != 2 || header.CurrentHead != activeHead {
				t.Fatalf("active audit header=%#v error=%v", header, err)
			}
			if rows, err := reopened.QueryContext(ctx, `SELECT value FROM restore_fixture_marker`); err != nil {
				t.Fatal(err)
			} else {
				var marker string
				if !rows.Next() || rows.Scan(&marker) != nil || marker != "active-proof-boundary" || rows.Next() ||
					rows.Err() != nil || rows.Close() != nil {
					t.Fatalf("active marker=%q", marker)
				}
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
