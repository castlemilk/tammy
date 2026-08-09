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

	"github.com/tammyapp/tammy/services/core/internal/backup"
)

type trustResolverFunc func(context.Context, string) (backup.TrustAnchor, error)

func (function trustResolverFunc) ResolveRestoreTrust(ctx context.Context, workspaceID string) (backup.TrustAnchor, error) {
	return function(ctx, workspaceID)
}

type activeMutatingStager struct {
	activePath string
	calls      int
}

func (stager *activeMutatingStager) ReserveRestoreArtifacts(_ context.Context, operationID, workspaceID string) (*RestoreArtifactReservation, error) {
	return testRestoreArtifactReservation(operationID, workspaceID), nil
}

func (stager *activeMutatingStager) ReleaseRestoreArtifacts(context.Context, *RestoreArtifactReservation) error {
	return nil
}

func (stager *activeMutatingStager) Stage(_ context.Context, _ StageRequest) (*StagedWorkspace, error) {
	stager.calls++
	if err := os.WriteFile(stager.activePath, []byte("invalid restore touched active bytes"), 0o600); err != nil {
		return nil, err
	}
	return &StagedWorkspace{Handle: "unexpected-active-stage"}, nil
}

func (stager *activeMutatingStager) DiscardStaged(context.Context, *StagedWorkspace) error {
	return nil
}

func TestRestoreServiceRejectsWrongPasswordAndTamperWithoutTouchingActiveBytes(t *testing.T) {
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	seed := sha256.Sum256([]byte("restore service signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	passphrase := []byte("correct horse battery staple")
	input := backup.ArchiveInput{
		WorkspaceID: workspaceID, SchemaVersion: 3, AppVersion: "0.1.0", AuditGeneration: 2, AuditSequence: 7,
		AuditHead: bytes.Repeat([]byte{0x42}, sha256.Size), AuditRoot: bytes.Repeat([]byte{0x43}, sha256.Size),
		SigningKeyID: "018f0000-0000-7000-8000-000000000002", SigningKeyEpoch: 1,
		WorkspaceHeaderHash:   bytes.Repeat([]byte{0x44}, sha256.Size),
		MigrationManifestHash: bytes.Repeat([]byte{0x45}, sha256.Size),
		Objects:               []backup.Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: []byte("verified staged database")}},
	}
	archive, err := backup.Seal(input, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x7a}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	trust := backup.TrustAnchor{WorkspaceID: workspaceID, AuditGeneration: input.AuditGeneration,
		AuditRoot: input.AuditRoot, SigningKeyID: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch,
		PublicKey: privateKey.Public().(ed25519.PublicKey)}
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "workspace", Version: 1,
		Validator: validatorFunc(func(context.Context, ValidationInput) error { return nil })}})
	if err != nil {
		t.Fatal(err)
	}

	var firstExternalError string
	for _, test := range []struct {
		name       string
		archive    []byte
		passphrase []byte
	}{
		{name: "wrong_password", archive: archive, passphrase: []byte("wrong password")},
		{name: "tampered_archive", archive: tamperLastByte(archive), passphrase: passphrase},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			activePath := filepath.Join(directory, "workspace.db")
			activeBytes := []byte("current active workspace must remain exact")
			if err := os.WriteFile(activePath, activeBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			before := sha256.Sum256(activeBytes)
			stager := &activeMutatingStager{activePath: activePath}
			harness := &restoreOrchestrationHarness{active: "original", trust: trust, stagerOverride: stager}
			config := harness.config(registry)
			config.Trust = trustResolverFunc(func(_ context.Context, gotWorkspaceID string) (backup.TrustAnchor, error) {
				if gotWorkspaceID != workspaceID {
					t.Fatalf("trust workspace = %q, want %q", gotWorkspaceID, workspaceID)
				}
				return trust, nil
			})
			service, err := NewService(config)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Restore(context.Background(), RestoreRequest{OperationID: operationID,
				WorkspaceID: workspaceID, Archive: test.archive, Passphrase: test.passphrase,
				Proof: &AdminTOTPProof{AdminUserID: "018f0000-0000-7000-8000-000000000010",
					Password: []byte("administrator-password"), TOTP: "123456", IssuedAt: time.Unix(1_720_000_000, 0).UTC(),
					ReplayKey: "018f0000-0000-7000-8000-000000000011"}})
			if !errors.Is(err, ErrRestore) || !errors.Is(err, backup.ErrArchiveSecret) {
				t.Fatalf("Restore() error = %v, want generic authenticated-decrypt failure", err)
			}
			if firstExternalError == "" {
				firstExternalError = err.Error()
			} else if err.Error() != firstExternalError {
				t.Fatalf("authenticated-decrypt oracle: first=%q current=%q", firstExternalError, err.Error())
			}
			if stager.calls != 0 {
				t.Fatalf("stager calls = %d, want 0 before archive authentication", stager.calls)
			}
			afterBytes, readErr := os.ReadFile(activePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			after := sha256.Sum256(afterBytes)
			if before != after || !bytes.Equal(afterBytes, activeBytes) {
				t.Fatalf("active bytes changed: before=%x after=%x", before, after)
			}
		})
	}
}

func tamperLastByte(input []byte) []byte {
	output := append([]byte(nil), input...)
	output[len(output)-1] ^= 0x01
	return output
}
