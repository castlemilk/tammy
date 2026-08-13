package restore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

type PreRestoreSnapshotSource interface {
	CapturePreRestoreSnapshot(context.Context, string, *RestoreAuthorization) ([]byte, error)
}

type PreRestoreArchiveServiceConfig struct {
	Directory string
	DEK       []byte
	Snapshots PreRestoreSnapshotSource
	NewID     func() (string, error)
	Now       func() time.Time
	Random    io.Reader
	hooks     *preRestoreArchiveHooks
}

type preRestoreArchiveHooks struct {
	afterPreparedFileSync func() error
	afterPublishRename    func() error
}

type preparedArchiveCapability struct {
	operationID      string
	archiveID        string
	preparedBasename string
	finalBasename    string
	hash             [sha256.Size]byte
	byteLength       uint64
	createdAt        time.Time
	deletionEligible time.Time
	sourceGeneration uint64
	identity         os.FileInfo
	published        bool
}

type PreRestoreArchiveService struct {
	mu            sync.Mutex
	root          *os.Root
	directory     string
	baseInfo      os.FileInfo
	dek           []byte
	snapshots     PreRestoreSnapshotSource
	newID         func() (string, error)
	now           func() time.Time
	random        io.Reader
	hooks         preRestoreArchiveHooks
	prepared      map[*PreRestoreArchive]*preparedArchiveCapability
	ownedPrepared map[string]os.FileInfo
	closed        bool
}

func NewPreRestoreArchiveService(config PreRestoreArchiveServiceConfig) (*PreRestoreArchiveService, error) {
	if config.Directory == "" || !filepath.IsAbs(config.Directory) || filepath.Clean(config.Directory) != config.Directory ||
		len(config.DEK) != preRestoreArchiveKeySize || nilInterface(config.Snapshots) || config.NewID == nil ||
		config.Now == nil || config.Random == nil {
		return nil, ErrPreRestoreArchive
	}
	baseInfo, err := os.Lstat(config.Directory)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 || baseInfo.Mode().Perm()&0o077 != 0 {
		return nil, ErrPreRestoreArchive
	}
	root, err := os.OpenRoot(config.Directory)
	if err != nil {
		return nil, ErrPreRestoreArchive
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(baseInfo, rootInfo) {
		_ = root.Close()
		return nil, ErrPreRestoreArchive
	}
	service := &PreRestoreArchiveService{root: root, directory: config.Directory, baseInfo: baseInfo,
		dek: append([]byte(nil), config.DEK...), snapshots: config.Snapshots, newID: config.NewID,
		now: config.Now, random: config.Random, hooks: normalizedPreRestoreArchiveHooks(config.hooks),
		prepared: make(map[*PreRestoreArchive]*preparedArchiveCapability), ownedPrepared: make(map[string]os.FileInfo)}
	return service, nil
}

// PrepareVerifiedPreRestoreArchive durably writes verified predecessor
// evidence to the exact operation-owned prepared name. It deliberately does
// not publish the random archive identity: callers must first authenticate the
// returned identity, hash, and both basenames in the external restore journal.
func (service *PreRestoreArchiveService) PrepareVerifiedPreRestoreArchive(
	ctx context.Context,
	request PreRestoreArchiveRequest,
) (*PreRestoreArchive, error) {
	if service == nil || ctx == nil || !ids.IsCanonicalV7(request.OperationID) || !ids.IsCanonicalV7(request.WorkspaceID) ||
		!validAuthorization(request.Authorization, request.WorkspaceID) || len(request.ManifestHash) != sha256.Size {
		return nil, ErrPreRestoreArchive
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed || service.root == nil || ctx.Err() != nil || !samePreRestoreDirectory(service.directory, service.baseInfo) {
		return nil, errors.Join(ErrPreRestoreArchive, ctx.Err())
	}
	archiveID, err := service.newID()
	if err != nil || !ids.IsCanonicalV7(archiveID) {
		return nil, ErrPreRestoreArchive
	}
	predecessor, err := service.snapshots.CapturePreRestoreSnapshot(ctx, request.WorkspaceID, request.Authorization)
	if err != nil || len(predecessor) == 0 || len(predecessor) > maximumPreRestoreArchiveBytes {
		zeroBytes(predecessor)
		return nil, errors.Join(ErrPreRestoreArchive, err)
	}
	defer zeroBytes(predecessor)
	createdAt := service.now().UTC()
	if createdAt.IsZero() {
		return nil, ErrPreRestoreArchive
	}
	archiveBytes, err := SealPreRestoreArchive(PreRestoreArchiveFormatInput{ArchiveID: archiveID,
		WorkspaceID: request.WorkspaceID, SourceGeneration: request.Authorization.CurrentGeneration,
		CreatedAt: createdAt, DeleteEligibleAt: createdAt.AddDate(1, 0, 0), Predecessor: predecessor},
		service.dek, service.random)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(archiveBytes)
	name := preRestoreArchiveName(archiveID)
	prepared := preRestoreArchivePreparedName(service.dek, request.OperationID)
	if _, err := service.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrPreRestoreArchive
	}
	if _, err := service.root.Lstat(prepared); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrPreRestoreArchive
	}
	file, err := service.root.OpenFile(prepared, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, ErrPreRestoreArchive
	}
	preparedOwned := true
	defer func() {
		_ = file.Close()
		if preparedOwned {
			if service.root.Remove(prepared) == nil {
				delete(service.ownedPrepared, prepared)
			}
		}
	}()
	for offset := 0; offset < len(archiveBytes); {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrPreRestoreArchive, err)
		}
		end := min(offset+32*1024, len(archiveBytes))
		written, writeErr := file.Write(archiveBytes[offset:end])
		if written <= 0 || written > end-offset || writeErr != nil {
			return nil, ErrPreRestoreArchive
		}
		offset += written
	}
	if err := file.Sync(); err != nil {
		return nil, ErrPreRestoreArchive
	}
	syncedIdentity, err := service.root.Lstat(prepared)
	if err != nil || !syncedIdentity.Mode().IsRegular() || syncedIdentity.Mode().Perm() != 0o600 {
		return nil, ErrPreRestoreArchive
	}
	service.ownedPrepared[prepared] = syncedIdentity
	if err := service.hooks.afterPreparedFileSync(); err != nil {
		preparedOwned = false
		return nil, errors.Join(ErrPreRestoreArchive, err)
	}
	if file.Close() != nil || syncPreRestoreRoot(service.root) != nil ||
		!samePreRestoreDirectory(service.directory, service.baseInfo) {
		return nil, ErrPreRestoreArchive
	}
	committed, err := service.readArchive(ctx, prepared)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(committed)
	if !bytes.Equal(committed, archiveBytes) {
		return nil, ErrPreRestoreArchive
	}
	opened, err := OpenPreRestoreArchive(committed, service.dek, request.WorkspaceID, archiveID)
	if err != nil || opened.Manifest.SourceGeneration != request.Authorization.CurrentGeneration ||
		!bytes.Equal(opened.Predecessor, predecessor) {
		if opened != nil {
			zeroBytes(opened.Predecessor)
		}
		return nil, ErrPreRestoreArchive
	}
	zeroBytes(opened.Predecessor)
	digest := sha256.Sum256(committed)
	identity, err := service.root.Lstat(prepared)
	if err != nil || !identity.Mode().IsRegular() || identity.Mode().Perm() != 0o600 || identity.Size() != int64(len(committed)) {
		return nil, ErrPreRestoreArchive
	}
	result := &PreRestoreArchive{OperationID: request.OperationID, ArchiveID: archiveID, Version: 1,
		SHA256:    append([]byte(nil), digest[:]...),
		CreatedAt: createdAt, DeletionEligibleAt: createdAt.AddDate(1, 0, 0),
		SourceGeneration: request.Authorization.CurrentGeneration, EncryptedByteLength: uint64(len(committed)),
		archiveAuthority: service, storageName: prepared}
	service.prepared[result] = &preparedArchiveCapability{operationID: request.OperationID, archiveID: archiveID,
		preparedBasename: prepared, finalBasename: name, hash: digest, byteLength: uint64(len(committed)),
		createdAt: createdAt, deletionEligible: createdAt.AddDate(1, 0, 0), sourceGeneration: request.Authorization.CurrentGeneration,
		identity: identity}
	delete(service.ownedPrepared, prepared)
	preparedOwned = false
	return result, nil
}

// PublishPreRestoreArchive atomically publishes a prepared archive only after
// its authenticated journal binding exists.
func (service *PreRestoreArchiveService) PublishPreRestoreArchive(
	ctx context.Context,
	archive *PreRestoreArchive,
	binding *PreparedArchiveBinding,
) error {
	if service == nil || ctx == nil || archive == nil || !ids.IsCanonicalV7(archive.OperationID) ||
		!ids.IsCanonicalV7(archive.ArchiveID) || len(archive.SHA256) != sha256.Size {
		return ErrPreRestoreArchive
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	capability, exists := service.prepared[archive]
	if service.closed || service.root == nil || !exists || !archiveMatchesPreparedCapability(archive, service, capability) ||
		ctx.Err() != nil || !samePreRestoreDirectory(service.directory, service.baseInfo) {
		return errors.Join(ErrPreRestoreArchive, ctx.Err())
	}
	if capability.published {
		if archive.storageName != capability.finalBasename {
			return ErrPreRestoreArchive
		}
		current, err := service.root.Lstat(capability.finalBasename)
		if err != nil || !os.SameFile(current, capability.identity) || !current.Mode().IsRegular() ||
			current.Mode().Perm() != 0o600 || uint64(current.Size()) != capability.byteLength {
			return ErrPreRestoreArchive
		}
		contents, err := service.readArchive(ctx, capability.finalBasename)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(contents)
		zeroBytes(contents)
		if subtle.ConstantTimeCompare(digest[:], capability.hash[:]) != 1 || binding.claim(ctx, archive) != nil {
			return ErrPreRestoreArchive
		}
		return nil
	}
	if archive.storageName != capability.preparedBasename {
		return ErrPreRestoreArchive
	}
	current, err := service.root.Lstat(capability.preparedBasename)
	if err != nil || !os.SameFile(current, capability.identity) || !current.Mode().IsRegular() ||
		current.Mode().Perm() != 0o600 || uint64(current.Size()) != capability.byteLength {
		return ErrPreRestoreArchive
	}
	contents, err := service.readArchive(ctx, capability.preparedBasename)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(contents)
	zeroBytes(contents)
	if subtle.ConstantTimeCompare(digest[:], capability.hash[:]) != 1 {
		return ErrPreRestoreArchive
	}
	if _, err := service.root.Lstat(capability.finalBasename); !errors.Is(err, os.ErrNotExist) {
		return ErrPreRestoreArchive
	}
	if err := binding.claim(ctx, archive); err != nil {
		return errors.Join(ErrPreRestoreArchive, err)
	}
	if err := service.root.Rename(capability.preparedBasename, capability.finalBasename); err != nil {
		return ErrPreRestoreArchive
	}
	capability.published = true
	archive.storageName = capability.finalBasename
	if err := service.hooks.afterPublishRename(); err != nil {
		return errors.Join(ErrPreRestoreArchive, err)
	}
	if syncPreRestoreRoot(service.root) != nil || !samePreRestoreDirectory(service.directory, service.baseInfo) {
		return ErrPreRestoreArchive
	}
	return nil
}

func (service *PreRestoreArchiveService) AbortPreRestoreArchive(ctx context.Context, archive *PreRestoreArchive) error {
	if service == nil || ctx == nil || archive == nil {
		return ErrPreRestoreArchive
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	capability, exists := service.prepared[archive]
	if service.closed || service.root == nil || !exists || !archiveMatchesPreparedCapability(archive, service, capability) ||
		(archive.storageName != capability.preparedBasename && archive.storageName != capability.finalBasename) || ctx.Err() != nil ||
		!samePreRestoreDirectory(service.directory, service.baseInfo) {
		return ErrPreRestoreArchive
	}
	current, err := service.root.Lstat(archive.storageName)
	if errors.Is(err, os.ErrNotExist) {
		delete(service.prepared, archive)
		archive.archiveAuthority = nil
		archive.storageName = ""
		return nil
	}
	if err != nil || !os.SameFile(current, capability.identity) || !current.Mode().IsRegular() ||
		current.Mode().Perm() != 0o600 || uint64(current.Size()) != capability.byteLength {
		return ErrPreRestoreArchive
	}
	contents, err := service.readArchive(ctx, archive.storageName)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(contents)
	zeroBytes(contents)
	if subtle.ConstantTimeCompare(digest[:], capability.hash[:]) != 1 || service.root.Remove(archive.storageName) != nil ||
		syncPreRestoreRoot(service.root) != nil {
		return ErrPreRestoreArchive
	}
	delete(service.prepared, archive)
	archive.archiveAuthority = nil
	archive.storageName = ""
	return nil
}

func (service *PreRestoreArchiveService) ReadEncryptedPreRestoreArchive(
	ctx context.Context,
	workspaceID string,
	archiveID string,
	expectedHash []byte,
) ([]byte, error) {
	if service == nil || ctx == nil || !ids.IsCanonicalV7(workspaceID) || !ids.IsCanonicalV7(archiveID) ||
		len(expectedHash) != sha256.Size {
		return nil, ErrPreRestoreArchive
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed || service.root == nil || ctx.Err() != nil || !samePreRestoreDirectory(service.directory, service.baseInfo) {
		return nil, errors.Join(ErrPreRestoreArchive, ctx.Err())
	}
	contents, err := service.readArchive(ctx, preRestoreArchiveName(archiveID))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(contents)
	if !bytes.Equal(digest[:], expectedHash) {
		zeroBytes(contents)
		return nil, ErrPreRestoreArchive
	}
	opened, err := OpenPreRestoreArchive(contents, service.dek, workspaceID, archiveID)
	if err != nil {
		zeroBytes(contents)
		return nil, ErrPreRestoreArchive
	}
	zeroBytes(opened.Predecessor)
	return contents, nil
}

func (service *PreRestoreArchiveService) DeleteEncryptedPreRestoreArchive(
	ctx context.Context,
	archiveID string,
	expectedHash []byte,
) error {
	if service == nil || ctx == nil || !ids.IsCanonicalV7(archiveID) || len(expectedHash) != sha256.Size {
		return ErrPreRestoreArchive
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed || service.root == nil || ctx.Err() != nil || !samePreRestoreDirectory(service.directory, service.baseInfo) {
		return errors.Join(ErrPreRestoreArchive, ctx.Err())
	}
	name := preRestoreArchiveName(archiveID)
	contents, err := service.readArchive(ctx, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		if _, statErr := service.root.Lstat(name); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return err
	}
	digest := sha256.Sum256(contents)
	zeroBytes(contents)
	if !bytes.Equal(digest[:], expectedHash) {
		return ErrPreRestoreArchive
	}
	if err := service.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrPreRestoreArchive
	}
	if syncPreRestoreRoot(service.root) != nil || !samePreRestoreDirectory(service.directory, service.baseInfo) {
		return ErrPreRestoreArchive
	}
	if _, err := service.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return ErrPreRestoreArchive
	}
	return nil
}

// CleanupInterruptedPreRestoreArchive removes only residue whose identity is
// derived from an authenticated PREPARED/STAGED journal record. Before the
// archive identity is bound, only the operation-derived prepared name is
// eligible; after binding, either the prepared or final file must match the
// journaled content hash, and both existing at once is rejected as ambiguous.
func (service *PreRestoreArchiveService) CleanupInterruptedPreRestoreArchive(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
) error {
	if service == nil || ctx == nil || status == nil || !validStatus(status) ||
		(status.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED &&
			status.State != tammyv1.RestoreState_RESTORE_STATE_STAGED) {
		return ErrPreRestoreArchive
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed || service.root == nil || ctx.Err() != nil || !samePreRestoreDirectory(service.directory, service.baseInfo) {
		return errors.Join(ErrPreRestoreArchive, ctx.Err())
	}
	names := []string{preRestoreArchivePreparedName(service.dek, status.OperationId)}
	var expectedHash []byte
	if status.Recovery != nil {
		names = []string{status.Recovery.PreRestoreArchivePreparedBasename, status.Recovery.PreRestoreArchiveFinalBasename}
		expectedHash = status.Recovery.PreRestoreArchiveHash
	}
	found := ""
	for _, name := range names {
		_, err := service.root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || found != "" {
			return ErrPreRestoreArchive
		}
		found = name
	}
	if found == "" {
		return nil
	}
	contents, err := service.readArchive(ctx, found)
	if err != nil {
		return err
	}
	if expectedHash != nil {
		digest := sha256.Sum256(contents)
		if subtle.ConstantTimeCompare(digest[:], expectedHash) != 1 {
			zeroBytes(contents)
			return ErrPreRestoreArchive
		}
	}
	zeroBytes(contents)
	if err := service.root.Remove(found); err != nil || syncPreRestoreRoot(service.root) != nil ||
		!samePreRestoreDirectory(service.directory, service.baseInfo) {
		return ErrPreRestoreArchive
	}
	return nil
}

func (service *PreRestoreArchiveService) Close() error {
	if service == nil {
		return nil
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed {
		return nil
	}
	removedPrepared := false
	for name, identity := range service.ownedPrepared {
		current, err := service.root.Lstat(name)
		if err == nil && os.SameFile(current, identity) && current.Mode().IsRegular() && current.Mode().Perm() == 0o600 {
			if removeErr := service.root.Remove(name); removeErr != nil {
				return ErrPreRestoreArchive
			}
			removedPrepared = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return ErrPreRestoreArchive
		}
		delete(service.ownedPrepared, name)
	}
	for archive, capability := range service.prepared {
		if !capability.published {
			current, err := service.root.Lstat(capability.preparedBasename)
			if err == nil && os.SameFile(current, capability.identity) && current.Mode().IsRegular() &&
				current.Mode().Perm() == 0o600 && uint64(current.Size()) == capability.byteLength {
				if removeErr := service.root.Remove(capability.preparedBasename); removeErr != nil {
					return ErrPreRestoreArchive
				}
				removedPrepared = true
			} else if !errors.Is(err, os.ErrNotExist) {
				return ErrPreRestoreArchive
			}
		}
		archive.archiveAuthority = nil
		archive.storageName = ""
		delete(service.prepared, archive)
	}
	if removedPrepared && syncPreRestoreRoot(service.root) != nil {
		return ErrPreRestoreArchive
	}
	service.closed = true
	zeroBytes(service.dek)
	service.dek = nil
	err := service.root.Close()
	service.root = nil
	if err != nil {
		return ErrPreRestoreArchive
	}
	return nil
}

func (service *PreRestoreArchiveService) readArchive(ctx context.Context, name string) ([]byte, error) {
	info, err := service.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() <= 0 ||
		info.Size() > maximumPreRestoreArchiveFileBytes {
		return nil, ErrPreRestoreArchive
	}
	file, err := service.root.Open(name)
	if err != nil {
		return nil, ErrPreRestoreArchive
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrPreRestoreArchive
	}
	contents := make([]byte, int(info.Size()))
	for offset := 0; offset < len(contents); {
		if err := ctx.Err(); err != nil {
			zeroBytes(contents)
			return nil, errors.Join(ErrPreRestoreArchive, err)
		}
		end := min(offset+32*1024, len(contents))
		read, readErr := io.ReadFull(file, contents[offset:end])
		offset += read
		if readErr != nil {
			zeroBytes(contents)
			return nil, ErrPreRestoreArchive
		}
	}
	final, err := service.root.Lstat(name)
	if err != nil || !os.SameFile(info, final) {
		zeroBytes(contents)
		return nil, ErrPreRestoreArchive
	}
	return contents, nil
}

func preRestoreArchiveName(archiveID string) string {
	return ".tammy-pre-restore-" + archiveID + ".archive"
}

func preRestoreArchivePreparedName(dek []byte, operationID string) string {
	authenticator := hmac.New(sha256.New, dek)
	_, _ = authenticator.Write([]byte("tammy.pre-restore.prepared-name.v1\x00"))
	_, _ = authenticator.Write([]byte(operationID))
	return ".tammy-pre-restore-operation-" + operationID + "-" + hex.EncodeToString(authenticator.Sum(nil)[:16]) + ".prepared"
}

func validPreRestoreArchivePreparedName(operationID, name string) bool {
	const prefix = ".tammy-pre-restore-operation-"
	const suffix = ".prepared"
	if !ids.IsCanonicalV7(operationID) || len(name) != len(prefix)+36+1+16*2+len(suffix) ||
		!bytes.HasPrefix([]byte(name), []byte(prefix+operationID+"-")) || filepath.Ext(name) != suffix {
		return false
	}
	tag := name[len(prefix)+36+1 : len(name)-len(suffix)]
	decoded, err := hex.DecodeString(tag)
	return err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == tag
}

func normalizedPreRestoreArchiveHooks(source *preRestoreArchiveHooks) preRestoreArchiveHooks {
	hooks := preRestoreArchiveHooks{afterPreparedFileSync: func() error { return nil }, afterPublishRename: func() error { return nil }}
	if source == nil {
		return hooks
	}
	if source.afterPreparedFileSync != nil {
		hooks.afterPreparedFileSync = source.afterPreparedFileSync
	}
	if source.afterPublishRename != nil {
		hooks.afterPublishRename = source.afterPublishRename
	}
	return hooks
}

func archiveMatchesPreparedCapability(archive *PreRestoreArchive, service *PreRestoreArchiveService,
	capability *preparedArchiveCapability,
) bool {
	return archive != nil && capability != nil && archive.archiveAuthority == service &&
		archive.OperationID == capability.operationID && archive.ArchiveID == capability.archiveID && archive.Version == 1 &&
		subtle.ConstantTimeCompare(archive.SHA256, capability.hash[:]) == 1 && archive.CreatedAt.Equal(capability.createdAt) &&
		archive.DeletionEligibleAt.Equal(capability.deletionEligible) && archive.SourceGeneration == capability.sourceGeneration &&
		archive.EncryptedByteLength == capability.byteLength
}

func samePreRestoreDirectory(directory string, expected os.FileInfo) bool {
	current, err := os.Lstat(directory)
	return err == nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, expected)
}

func syncPreRestoreRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
