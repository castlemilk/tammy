package backup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"sync"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrService = errors.New("backup: service failed")

type WorkspaceSnapshot struct {
	Database              []byte
	Header                []byte
	SchemaVersion         uint64
	MigrationManifestHash []byte
}

type SnapshotSource interface {
	// ConsistentSnapshot owns one fixed read boundary and must invoke the
	// supplied immutable provider registry before that boundary closes.
	ConsistentSnapshot(context.Context, string, *ProviderRegistry) (CapturedSnapshot, error)
}

type CapturedSnapshot struct {
	Workspace       WorkspaceSnapshot
	Lineage         AuditLineage
	ProviderObjects []Object
}

type AuditLineage struct {
	Generation      uint64
	Sequence        uint64
	Head            []byte
	Root            []byte
	SigningKeyID    string
	SigningKeyEpoch uint64
	PublicKey       ed25519.PublicKey
}

type SnapshotPolicy interface {
	// VerifyExcluded performs schema-aware validation of the transformed
	// database image before any archive bytes are sealed.
	VerifyExcluded(context.Context, WorkspaceSnapshot) error
}

// ManifestSigningRequest binds one canonical manifest statement to the exact
// authenticated signing lineage captured with its staged workspace bytes.
type ManifestSigningRequest struct {
	WorkspaceID     string
	SigningKeyID    string
	SigningKeyEpoch uint64
	AuditGeneration uint64
	AuditRoot       []byte
	Statement       []byte
}

// ManifestSigner is an audit-owned, manifest-specific signing capability;
// backup never receives or persists the encrypted private signing key.
type ManifestSigner interface {
	SignManifest(context.Context, ManifestSigningRequest) ([]byte, error)
}

type Destination = audit.ExportDestination

type DestinationResolver interface {
	Resolve(string) (Destination, error)
}

type ServiceConfig struct {
	AppVersion     string
	Snapshots      SnapshotSource
	SnapshotPolicy SnapshotPolicy
	Signer         ManifestSigner
	Providers      *ProviderRegistry
	Destinations   DestinationResolver
	Random         io.Reader
}

type Service struct {
	mu             sync.Mutex
	appVersion     string
	snapshots      SnapshotSource
	snapshotPolicy SnapshotPolicy
	signer         ManifestSigner
	providers      *ProviderRegistry
	destinations   DestinationResolver
	random         io.Reader
}

type CreateRequest struct {
	WorkspaceID           string
	DestinationCapability string
	Passphrase            []byte
}

type CreateResult struct {
	ManifestHash    []byte
	DestinationHash []byte
}

type preparedBackup struct {
	workspaceID           string
	destinationCapability string
	archive               []byte
	manifestHash          []byte
	archiveHash           [sha256.Size]byte
}

func (prepared *preparedBackup) close() {
	if prepared == nil {
		return
	}
	zero(prepared.archive)
	zero(prepared.manifestHash)
	*prepared = preparedBackup{}
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.AppVersion == "" || len(config.AppVersion) > 64 || nilInterface(config.Snapshots) ||
		nilInterface(config.SnapshotPolicy) || nilInterface(config.Signer) || config.Providers == nil ||
		nilInterface(config.Destinations) {
		return nil, ErrService
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &Service{appVersion: config.AppVersion, snapshots: config.Snapshots, snapshotPolicy: config.SnapshotPolicy,
		signer: config.Signer, providers: config.Providers, destinations: config.Destinations, random: config.Random}, nil
}

func (service *Service) Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	prepared, err := service.prepare(ctx, request)
	if err != nil {
		return CreateResult{}, err
	}
	defer prepared.close()
	return service.publish(ctx, prepared)
}

func (service *Service) prepare(ctx context.Context, request CreateRequest) (*preparedBackup, error) {
	if service == nil || ctx == nil || !ids.IsCanonicalV7(request.WorkspaceID) ||
		!ids.IsCanonicalV7(request.DestinationCapability) || len(request.Passphrase) == 0 {
		return nil, ErrService
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrService, err)
	}
	// Serializing the source/signer/random boundary makes each call one exact
	// online snapshot and prevents a non-thread-safe injected entropy stream.
	service.mu.Lock()
	defer service.mu.Unlock()

	captured, err := service.snapshots.ConsistentSnapshot(ctx, request.WorkspaceID, service.providers)
	if err != nil || !validWorkspaceSnapshot(captured.Workspace) || !validAuditLineage(captured.Lineage) || ctx.Err() != nil {
		return nil, ErrService
	}
	defer func() {
		zero(captured.Workspace.Database)
		zero(captured.Workspace.Header)
		zero(captured.Workspace.MigrationManifestHash)
		for index := range captured.ProviderObjects {
			zero(captured.ProviderObjects[index].Bytes)
		}
	}()
	if err := service.snapshotPolicy.VerifyExcluded(ctx, captured.Workspace); err != nil || ctx.Err() != nil {
		return nil, ErrService
	}
	if err := service.providers.ValidateCollected(captured.ProviderObjects); err != nil {
		return nil, ErrService
	}
	snapshot := captured.Workspace
	lineage := captured.Lineage
	objects := cloneObjects(captured.ProviderObjects)
	objects = append(objects,
		Object{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: append([]byte(nil), snapshot.Database...)},
		Object{Path: "workspace/header.pb", Provider: "workspace", ProviderVersion: 1, Bytes: append([]byte(nil), snapshot.Header...)},
	)
	defer func() {
		for index := range objects {
			zero(objects[index].Bytes)
		}
	}()
	headerHash := sha256.Sum256(snapshot.Header)
	input := ArchiveInput{
		WorkspaceID: request.WorkspaceID, SchemaVersion: snapshot.SchemaVersion, AppVersion: service.appVersion,
		AuditGeneration: lineage.Generation, AuditSequence: lineage.Sequence, AuditHead: lineage.Head, AuditRoot: lineage.Root,
		SigningKeyID: lineage.SigningKeyID, SigningKeyEpoch: lineage.SigningKeyEpoch,
		WorkspaceHeaderHash: headerHash[:], MigrationManifestHash: snapshot.MigrationManifestHash, Objects: objects,
	}
	archive, err := sealWithSigner(input, request.Passphrase, func(statement []byte) ([]byte, error) {
		ownedStatement := append([]byte(nil), statement...)
		defer zero(ownedStatement)
		signature, err := service.signer.SignManifest(ctx, ManifestSigningRequest{WorkspaceID: request.WorkspaceID,
			SigningKeyID: lineage.SigningKeyID, SigningKeyEpoch: lineage.SigningKeyEpoch,
			AuditGeneration: lineage.Generation, AuditRoot: append([]byte(nil), lineage.Root...), Statement: ownedStatement})
		if err != nil || len(signature) != ed25519.SignatureSize ||
			!ed25519.Verify(lineage.PublicKey, statement, signature) {
			zero(signature)
			return nil, ErrService
		}
		return signature, nil
	}, service.random)
	if err != nil || ctx.Err() != nil {
		zero(archive)
		return nil, ErrService
	}
	verified, err := Open(archive, request.Passphrase, TrustAnchor{WorkspaceID: request.WorkspaceID,
		AuditGeneration: lineage.Generation, AuditRoot: lineage.Root, SigningKeyID: lineage.SigningKeyID,
		SigningKeyEpoch: lineage.SigningKeyEpoch, PublicKey: lineage.PublicKey})
	if err != nil {
		zero(archive)
		return nil, ErrService
	}
	for index := range verified.Objects {
		defer zero(verified.Objects[index].Bytes)
	}
	archiveHash := sha256.Sum256(archive)
	return &preparedBackup{workspaceID: request.WorkspaceID, destinationCapability: request.DestinationCapability,
		archive: archive, manifestHash: append([]byte(nil), verified.ManifestHash...), archiveHash: archiveHash}, nil
}

func (service *Service) publish(ctx context.Context, prepared *preparedBackup) (CreateResult, error) {
	if service == nil || ctx == nil || prepared == nil || !ids.IsCanonicalV7(prepared.workspaceID) ||
		!ids.IsCanonicalV7(prepared.destinationCapability) || len(prepared.archive) == 0 ||
		len(prepared.manifestHash) != sha256.Size || sha256.Sum256(prepared.archive) != prepared.archiveHash {
		return CreateResult{}, ErrService
	}
	if err := ctx.Err(); err != nil {
		return CreateResult{}, errors.Join(ErrService, err)
	}
	destination, err := service.destinations.Resolve(prepared.destinationCapability)
	if err != nil || nilInterface(destination) || destination.Reference() != prepared.destinationCapability {
		return CreateResult{}, ErrService
	}
	if err := destination.AtomicCommit(ctx, prepared.archive); err != nil {
		return CreateResult{}, ErrService
	}
	committed, err := destination.ReadCommitted(ctx)
	if err != nil || !bytes.Equal(committed, prepared.archive) {
		zero(committed)
		return CreateResult{}, ErrService
	}
	destinationHash := sha256.Sum256(committed)
	zero(committed)
	return CreateResult{ManifestHash: append([]byte(nil), prepared.manifestHash...),
		DestinationHash: append([]byte(nil), destinationHash[:]...)}, nil
}

func validWorkspaceSnapshot(snapshot WorkspaceSnapshot) bool {
	return len(snapshot.Database) > 0 && len(snapshot.Database) <= maximumArchivePlaintext && len(snapshot.Header) > 0 &&
		len(snapshot.Header) <= 1024*1024 && snapshot.SchemaVersion > 0 && len(snapshot.MigrationManifestHash) == sha256.Size
}

func validAuditLineage(lineage AuditLineage) bool {
	return lineage.Generation > 0 && len(lineage.Head) == sha256.Size && len(lineage.Root) == sha256.Size &&
		ids.IsCanonicalV7(lineage.SigningKeyID) && lineage.SigningKeyEpoch > 0 && len(lineage.PublicKey) == ed25519.PublicKeySize
}

func cloneObjects(objects []Object) []Object {
	owned := make([]Object, len(objects))
	for index, object := range objects {
		owned[index] = object
		owned[index].Bytes = append([]byte(nil), object.Bytes...)
	}
	return owned
}
