package backup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"testing"
)

type snapshotSourceFunc func(context.Context, string, *ProviderRegistry) (CapturedSnapshot, error)

func (function snapshotSourceFunc) ConsistentSnapshot(ctx context.Context, workspaceID string, registry *ProviderRegistry) (CapturedSnapshot, error) {
	return function(ctx, workspaceID, registry)
}

type snapshotPolicyFunc func(context.Context, WorkspaceSnapshot) error

func (function snapshotPolicyFunc) VerifyExcluded(ctx context.Context, snapshot WorkspaceSnapshot) error {
	return function(ctx, snapshot)
}

type signerFunc func(context.Context, ManifestSigningRequest) ([]byte, error)

func (function signerFunc) SignManifest(ctx context.Context, request ManifestSigningRequest) ([]byte, error) {
	return function(ctx, request)
}

type destinationResolver map[string]*memoryDestination

func (resolver destinationResolver) Resolve(reference string) (Destination, error) {
	return resolver[reference], nil
}

type memoryDestination struct {
	reference string
	archive   []byte
}

func (destination *memoryDestination) Reference() string { return destination.reference }
func (destination *memoryDestination) AtomicCommit(_ context.Context, archive []byte) error {
	destination.archive = append([]byte(nil), archive...)
	return nil
}
func (destination *memoryDestination) ReadCommitted(_ context.Context) ([]byte, error) {
	return append([]byte(nil), destination.archive...), nil
}

func TestBackupServicePublishesVerifiedConsistentSnapshot(t *testing.T) {
	workspaceID := "018f0000-0000-7000-8000-000000000021"
	keyID := "018f0000-0000-7000-8000-000000000022"
	destinationID := "018f0000-0000-7000-8000-000000000023"
	seed := sha256.Sum256([]byte("backup service audit signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	header := []byte("bounded workspace header")
	migrationHash := sha256.Sum256([]byte("ordered migrations"))
	lineage := AuditLineage{Generation: 3, Sequence: 9, Head: bytes.Repeat([]byte{0x31}, sha256.Size),
		Root: bytes.Repeat([]byte{0x32}, sha256.Size), SigningKeyID: keyID, SigningKeyEpoch: 2,
		PublicKey: privateKey.Public().(ed25519.PublicKey)}
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 1,
		Provider: providerFunc(func(_ context.Context, _ SnapshotReader, _ SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	destination := &memoryDestination{reference: destinationID}
	service, err := NewService(ServiceConfig{
		AppVersion: "0.1.0",
		Snapshots: snapshotSourceFunc(func(ctx context.Context, gotWorkspaceID string, providers *ProviderRegistry) (CapturedSnapshot, error) {
			if gotWorkspaceID != workspaceID {
				t.Fatalf("snapshot workspace = %q", gotWorkspaceID)
			}
			objects, err := providers.Collect(ctx, []ProjectionSourceRegistration{{Name: "rules", Version: 1,
				Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
					return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules")}}}, nil
				})}}, SnapshotRequest{WorkspaceID: workspaceID, AuditGeneration: lineage.Generation,
				AuditSequence: lineage.Sequence, AuditHead: lineage.Head})
			return CapturedSnapshot{Workspace: WorkspaceSnapshot{Database: []byte("consistent encrypted database snapshot"), Header: header,
				SchemaVersion: 3, MigrationManifestHash: migrationHash[:]}, Lineage: lineage, ProviderObjects: objects}, err
		}),
		SnapshotPolicy: snapshotPolicyFunc(func(_ context.Context, snapshot WorkspaceSnapshot) error {
			if bytes.Contains(snapshot.Database, []byte("live-session-secret")) {
				t.Fatal("unsanitized session material reached archive sealing")
			}
			return nil
		}),
		Signer: signerFunc(func(_ context.Context, request ManifestSigningRequest) ([]byte, error) {
			return ed25519.Sign(privateKey, request.Statement), nil
		}),
		Providers:    registry,
		Destinations: destinationResolver{destinationID: destination},
		Random:       bytes.NewReader(bytes.Repeat([]byte{0x55}, 128)),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.Create(context.Background(), CreateRequest{WorkspaceID: workspaceID,
		DestinationCapability: destinationID, Passphrase: []byte("correct horse battery staple")})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	committedHash := sha256.Sum256(destination.archive)
	if !bytes.Equal(result.DestinationHash, committedHash[:]) || len(result.ManifestHash) != sha256.Size {
		t.Fatalf("result = %#v", result)
	}
	opened, err := Open(destination.archive, []byte("correct horse battery staple"), TrustAnchor{
		WorkspaceID: workspaceID, AuditGeneration: lineage.Generation, AuditRoot: lineage.Root,
		SigningKeyID: keyID, SigningKeyEpoch: lineage.SigningKeyEpoch,
		PublicKey: privateKey.Public().(ed25519.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Objects) != 3 || opened.Objects[0].Path != "database/workspace.db" ||
		opened.Objects[1].Path != "rules/current.pb" || opened.Objects[2].Path != "workspace/header.pb" {
		t.Fatalf("archive objects = %#v", opened.Objects)
	}
}

func TestBackupServiceCannotMixMutatingLiveReads(t *testing.T) {
	captures := 0
	providers, err := NewProviderRegistry([]ProviderRegistration{{Name: "workspace", Version: 1,
		Provider: providerFunc(func(_ context.Context, _ SnapshotReader, request SnapshotRequest) (Projection, error) {
			if request.AuditGeneration != 7 {
				t.Fatalf("provider observed generation %d, want captured generation 7", request.AuditGeneration)
			}
			return Projection{Objects: []Object{{Path: "providers/workspace/state.pb", Bytes: []byte("revision-7")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("fixed snapshot signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	lineage := AuditLineage{Generation: 7, Sequence: 12, Head: bytes.Repeat([]byte{7}, 32), Root: bytes.Repeat([]byte{8}, 32),
		SigningKeyID: "018f0000-0000-7000-8000-000000000032", SigningKeyEpoch: 3,
		PublicKey: privateKey.Public().(ed25519.PublicKey)}
	destination := &memoryDestination{reference: "018f0000-0000-7000-8000-000000000033"}
	service, err := NewService(ServiceConfig{AppVersion: "0.1.0", Providers: providers,
		Snapshots: snapshotSourceFunc(func(ctx context.Context, workspaceID string, registry *ProviderRegistry) (CapturedSnapshot, error) {
			captures++
			objects, err := registry.Collect(ctx, []ProjectionSourceRegistration{{Name: "workspace", Version: 1,
				Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
					return Projection{Objects: []Object{{Path: "providers/workspace/state.pb", Bytes: []byte("revision-7")}}}, nil
				})}}, SnapshotRequest{WorkspaceID: workspaceID, AuditGeneration: lineage.Generation,
				AuditSequence: lineage.Sequence, AuditHead: lineage.Head})
			migration := sha256.Sum256([]byte("migration-7"))
			return CapturedSnapshot{Workspace: WorkspaceSnapshot{Database: []byte("database-revision-7"), Header: []byte("header-7"),
				SchemaVersion: 7, MigrationManifestHash: migration[:]}, Lineage: lineage, ProviderObjects: objects}, err
		}),
		SnapshotPolicy: snapshotPolicyFunc(func(context.Context, WorkspaceSnapshot) error { return nil }),
		Signer: signerFunc(func(_ context.Context, request ManifestSigningRequest) ([]byte, error) {
			return ed25519.Sign(privateKey, request.Statement), nil
		}),
		Destinations: destinationResolver{destination.reference: destination}, Random: bytes.NewReader(bytes.Repeat([]byte{0x58}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), CreateRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000031",
		DestinationCapability: destination.reference, Passphrase: []byte("correct horse battery staple")})
	if err != nil || captures != 1 {
		t.Fatalf("Create() error = %v, captures = %d, want exactly one", err, captures)
	}
}

func TestBackupServiceRejectsSignerRotatedAfterFixedCopy(t *testing.T) {
	const (
		workspaceID   = "018f0000-0000-7000-8000-000000000061"
		oldKeyID      = "018f0000-0000-7000-8000-000000000062"
		destinationID = "018f0000-0000-7000-8000-000000000063"
	)
	oldSeed := sha256.Sum256([]byte("captured old signing key"))
	newSeed := sha256.Sum256([]byte("rotated live signing key"))
	oldPrivate := ed25519.NewKeyFromSeed(oldSeed[:])
	newPrivate := ed25519.NewKeyFromSeed(newSeed[:])
	root := bytes.Repeat([]byte{0x71}, sha256.Size)
	head := bytes.Repeat([]byte{0x72}, sha256.Size)
	migration := sha256.Sum256([]byte("migration manifest"))
	lineage := AuditLineage{Generation: 4, Sequence: 10, Head: head, Root: root,
		SigningKeyID: oldKeyID, SigningKeyEpoch: 2, PublicKey: oldPrivate.Public().(ed25519.PublicKey)}
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 1,
		Provider: providerFunc(func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("captured")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	destination := &memoryDestination{reference: destinationID}
	service, err := NewService(ServiceConfig{AppVersion: "0.1.0", Providers: registry,
		Snapshots: snapshotSourceFunc(func(context.Context, string, *ProviderRegistry) (CapturedSnapshot, error) {
			return CapturedSnapshot{Workspace: WorkspaceSnapshot{Database: []byte("captured database"),
				Header: []byte("captured header"), SchemaVersion: 3, MigrationManifestHash: migration[:]},
				Lineage: lineage, ProviderObjects: []Object{{Path: "rules/current.pb", Provider: "rules",
					ProviderVersion: 1, Bytes: []byte("captured")}}}, nil
		}),
		SnapshotPolicy: snapshotPolicyFunc(func(context.Context, WorkspaceSnapshot) error { return nil }),
		Signer: signerFunc(func(_ context.Context, request ManifestSigningRequest) ([]byte, error) {
			if request.WorkspaceID != workspaceID || request.SigningKeyID != oldKeyID || request.SigningKeyEpoch != 2 ||
				request.AuditGeneration != 4 || !bytes.Equal(request.AuditRoot, root) || len(request.Statement) == 0 {
				t.Fatalf("manifest signing request = %#v", request)
			}
			return ed25519.Sign(newPrivate, request.Statement), nil
		}),
		Destinations: destinationResolver{destinationID: destination}, Random: bytes.NewReader(bytes.Repeat([]byte{0x73}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), CreateRequest{WorkspaceID: workspaceID,
		DestinationCapability: destinationID, Passphrase: []byte("correct horse battery staple")})
	if !errors.Is(err, ErrService) || len(destination.archive) != 0 {
		t.Fatalf("rotated signer Create() error=%v destination bytes=%d", err, len(destination.archive))
	}
}
