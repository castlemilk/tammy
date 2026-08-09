package restore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrJournal         = errors.New("restore: invalid journal")
	ErrJournalConflict = errors.New("restore: operation manifest conflict")
)

const (
	maximumJournalBytes            = 8 * 1024
	maximumJournalDirectoryEntries = 4096
	maximumJournalPageSize         = 200
)

var journalMagic = [24]byte{'t', 'a', 'm', 'm', 'y', '-', 'r', 'e', 's', 't', 'o', 'r', 'e', '-', 'j', 'o', 'u', 'r', 'n', 'a', 'l', '-', 'v', '2'}

var journalAuthenticationDomain = []byte("tammy.restore.journal.auth.v2\x00")
var journalTemporaryNameDomain = []byte("tammy.restore.journal.temp.v1\x00")

type JournalConfig struct {
	Directory         string
	AuthenticationKey []byte
	Now               func() time.Time
	hooks             *journalHooks
}

type journalHooks struct {
	openRoot      func(string) (*os.Root, error)
	write         func(*os.File, []byte) (int, error)
	syncFile      func(*os.File) error
	rename        func(*os.Root, string, string) error
	syncDirectory func(*os.Root) error
}

type JournalStore struct {
	mu                      sync.Mutex
	root                    *os.Root
	now                     func() time.Time
	auth                    []byte
	hooks                   journalHooks
	preparedArchiveBindings map[*PreparedArchiveBinding]preparedArchivePublication
	closed                  bool
}

// PreparedArchiveBinding is an opaque, single-use authority issued only after
// BindPreparedRecovery has durably persisted the exact publication identity.
// Its fields are intentionally private so callers cannot manufacture one.
type PreparedArchiveBinding struct{ store *JournalStore }

type preparedArchivePublication struct {
	operationID      string
	archiveID        string
	preparedBasename string
	finalBasename    string
	hash             [sha256.Size]byte
}

type RestoreJournalPage struct {
	Records              []*tammyv1.RestoreStatus
	NextAfterOperationID string
}

func NewJournalStore(config JournalConfig) (*JournalStore, error) {
	if config.Directory == "" || !filepath.IsAbs(config.Directory) || filepath.Clean(config.Directory) != config.Directory ||
		len(config.AuthenticationKey) != sha256.Size || config.Now == nil {
		return nil, ErrJournal
	}
	info, err := os.Lstat(config.Directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrJournal
	}
	hooks := normalizedJournalHooks(config.hooks)
	root, err := hooks.openRoot(config.Directory)
	if err != nil {
		return nil, ErrJournal
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, rootInfo) {
		_ = root.Close()
		return nil, ErrJournal
	}
	store := &JournalStore{root: root, now: config.Now, auth: append([]byte(nil), config.AuthenticationKey...), hooks: hooks,
		preparedArchiveBindings: make(map[*PreparedArchiveBinding]preparedArchivePublication)}
	if err := store.removePartialTemps(); err != nil {
		_ = root.Close()
		zeroBytes(store.auth)
		return nil, err
	}
	return store, nil
}

func (store *JournalStore) BindPreparedRecovery(ctx context.Context, operationID string, manifestHash []byte,
	recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, *PreparedArchiveBinding, error) {
	if store == nil || ctx == nil || !ids.IsCanonicalV7(operationID) || len(manifestHash) != sha256.Size ||
		!validPreparedRecovery(operationID, recovery) {
		return nil, nil, ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed || store.root == nil || ctx.Err() != nil {
		return nil, nil, ErrJournal
	}
	status, err := store.load(operationID)
	if err != nil || status.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED ||
		subtle.ConstantTimeCompare(status.BackupManifestHash, manifestHash) != 1 {
		return nil, nil, ErrJournalConflict
	}
	if status.Recovery != nil {
		if !proto.Equal(status.Recovery, recovery) {
			return nil, nil, ErrJournalConflict
		}
		return cloneStatus(status), store.issuePreparedArchiveBinding(status), nil
	}
	status.Recovery = proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	status.UpdatedAt = timestamppb.New(store.now().UTC())
	if err := store.persist(status); err != nil {
		return nil, nil, err
	}
	return cloneStatus(status), store.issuePreparedArchiveBinding(status), nil
}

func (store *JournalStore) issuePreparedArchiveBinding(status *tammyv1.RestoreStatus) *PreparedArchiveBinding {
	recovery := status.Recovery
	binding := &PreparedArchiveBinding{store: store}
	var hash [sha256.Size]byte
	copy(hash[:], recovery.PreRestoreArchiveHash)
	store.preparedArchiveBindings[binding] = preparedArchivePublication{operationID: status.OperationId,
		archiveID: recovery.PreRestoreArchiveId, preparedBasename: recovery.PreRestoreArchivePreparedBasename,
		finalBasename: recovery.PreRestoreArchiveFinalBasename, hash: hash}
	return binding
}

func (binding *PreparedArchiveBinding) claim(ctx context.Context, archive *PreRestoreArchive) error {
	if binding == nil || binding.store == nil {
		return ErrJournal
	}
	return binding.store.claimPreparedArchiveBinding(ctx, binding, archive)
}

func (store *JournalStore) claimPreparedArchiveBinding(
	ctx context.Context,
	binding *PreparedArchiveBinding,
	archive *PreRestoreArchive,
) error {
	if store == nil || ctx == nil || archive == nil {
		return ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	publication, exists := store.preparedArchiveBindings[binding]
	if store.closed || store.root == nil || ctx.Err() != nil || !exists || binding.store != store ||
		archive.OperationID != publication.operationID || archive.ArchiveID != publication.archiveID ||
		(archive.storageName != publication.preparedBasename && archive.storageName != publication.finalBasename) ||
		subtle.ConstantTimeCompare(archive.SHA256, publication.hash[:]) != 1 {
		return ErrJournal
	}
	status, err := store.load(publication.operationID)
	if err != nil || status.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED || status.Recovery == nil ||
		status.Recovery.PreRestoreArchiveId != publication.archiveID ||
		status.Recovery.PreRestoreArchivePreparedBasename != publication.preparedBasename ||
		status.Recovery.PreRestoreArchiveFinalBasename != publication.finalBasename ||
		subtle.ConstantTimeCompare(status.Recovery.PreRestoreArchiveHash, publication.hash[:]) != 1 {
		return ErrJournal
	}
	delete(store.preparedArchiveBindings, binding)
	binding.store = nil
	return nil
}

func (store *JournalStore) BindStagedRecovery(ctx context.Context, operationID string,
	recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, error) {
	if store == nil || ctx == nil || !ids.IsCanonicalV7(operationID) || !validStagedRecovery(operationID, recovery) {
		return nil, ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed || store.root == nil || ctx.Err() != nil {
		return nil, ErrJournal
	}
	status, err := store.load(operationID)
	if err != nil {
		return nil, ErrJournal
	}
	if status.State == tammyv1.RestoreState_RESTORE_STATE_STAGED {
		if !proto.Equal(status.Recovery, recovery) {
			return nil, ErrJournalConflict
		}
		return cloneStatus(status), nil
	}
	if status.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED || status.Recovery == nil ||
		!samePreparedRecovery(status.Recovery, recovery) {
		return nil, ErrJournalConflict
	}
	status.State = tammyv1.RestoreState_RESTORE_STATE_STAGED
	status.NewAuditHead = append([]byte(nil), recovery.FinalizedAuditHead...)
	status.Recovery = proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	status.UpdatedAt = timestamppb.New(store.now().UTC())
	if err := store.persist(status); err != nil {
		return nil, err
	}
	return cloneStatus(status), nil
}

func (store *JournalStore) CheckpointRecovery(ctx context.Context, operationID string, state tammyv1.RestoreState,
	recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, error) {
	if store == nil || ctx == nil || !ids.IsCanonicalV7(operationID) ||
		state != tammyv1.RestoreState_RESTORE_STATE_SWAPPED || !validStagedRecovery(operationID, recovery) {
		return nil, ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed || store.root == nil || ctx.Err() != nil {
		return nil, ErrJournal
	}
	status, err := store.load(operationID)
	if err != nil {
		return nil, ErrJournal
	}
	if status.State != state || status.Recovery == nil || !sameStagedRecovery(status.Recovery, recovery) {
		return nil, ErrJournalConflict
	}
	if proto.Equal(status.Recovery, recovery) {
		return cloneStatus(status), nil
	}
	if !validNextRecoveryCheckpoint(status.Recovery, recovery) {
		return nil, ErrJournalConflict
	}
	status.Recovery = proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	status.UpdatedAt = timestamppb.New(store.now().UTC())
	if err := store.persist(status); err != nil {
		return nil, err
	}
	return cloneStatus(status), nil
}

func (store *JournalStore) Prepare(ctx context.Context, operationID string, manifestHash []byte) (*tammyv1.RestoreStatus, error) {
	if store == nil || ctx == nil || !ids.IsCanonicalV7(operationID) || len(manifestHash) != sha256.Size {
		return nil, ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed || store.root == nil || ctx.Err() != nil {
		return nil, ErrJournal
	}
	existing, err := store.load(operationID)
	if err == nil {
		if subtle.ConstantTimeCompare(existing.BackupManifestHash, manifestHash) != 1 {
			return nil, ErrJournalConflict
		}
		return cloneStatus(existing), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	status := &tammyv1.RestoreStatus{OperationId: operationID, State: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
		BackupManifestHash: append([]byte(nil), manifestHash...), UpdatedAt: timestamppb.New(store.now().UTC())}
	if err := store.persist(status); err != nil {
		return nil, err
	}
	return cloneStatus(status), nil
}

func (store *JournalStore) Advance(ctx context.Context, operationID string, from, to tammyv1.RestoreState, newAuditHead []byte) (*tammyv1.RestoreStatus, error) {
	if store == nil || ctx == nil || !ids.IsCanonicalV7(operationID) || !validJournalTransition(from, to) ||
		!validTransitionAuditHead(from, to, newAuditHead) {
		return nil, ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed || store.root == nil || ctx.Err() != nil {
		return nil, ErrJournal
	}
	status, err := store.load(operationID)
	if err != nil {
		return nil, ErrJournal
	}
	if status.State == to {
		if subtle.ConstantTimeCompare(status.NewAuditHead, newAuditHead) != 1 {
			return nil, ErrJournalConflict
		}
		return cloneStatus(status), nil
	}
	if status.State != from {
		return nil, ErrJournal
	}
	if from != tammyv1.RestoreState_RESTORE_STATE_PREPARED && subtle.ConstantTimeCompare(status.NewAuditHead, newAuditHead) != 1 {
		return nil, ErrJournalConflict
	}
	status.State = to
	status.NewAuditHead = append([]byte(nil), newAuditHead...)
	status.UpdatedAt = timestamppb.New(store.now().UTC())
	if err := store.persist(status); err != nil {
		return nil, err
	}
	return cloneStatus(status), nil
}

func (store *JournalStore) Get(ctx context.Context, operationID string) (*tammyv1.RestoreStatus, error) {
	if store == nil || ctx == nil || !ids.IsCanonicalV7(operationID) {
		return nil, ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed || store.root == nil || ctx.Err() != nil {
		return nil, ErrJournal
	}
	status, err := store.load(operationID)
	if err != nil {
		return nil, err
	}
	return cloneStatus(status), nil
}

// ListRecoveryRecords authenticates and canonicalizes the complete bounded
// journal set before exposing a deterministic page to startup recovery. This
// prevents a later tampered record from being discovered only after earlier
// filesystem recovery actions have already run.
func (store *JournalStore) ListRecoveryRecords(
	ctx context.Context,
	afterOperationID string,
	limit uint32,
) (*RestoreJournalPage, error) {
	if store == nil || ctx == nil || (afterOperationID != "" && !ids.IsCanonicalV7(afterOperationID)) ||
		limit == 0 || limit > maximumJournalPageSize {
		return nil, ErrJournal
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed || store.root == nil || ctx.Err() != nil {
		return nil, errors.Join(ErrJournal, ctx.Err())
	}
	directory, err := store.root.Open(".")
	if err != nil {
		return nil, ErrJournal
	}
	defer directory.Close()
	names := make([]string, 0, 64)
	totalEntries := 0
	for {
		entries, readErr := directory.ReadDir(128)
		totalEntries += len(entries)
		if totalEntries > maximumJournalDirectoryEntries {
			return nil, ErrJournal
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".restore.pb") {
				continue
			}
			operationID := strings.TrimSuffix(name, ".restore.pb")
			if journalName(operationID) != name || !ids.IsCanonicalV7(operationID) {
				return nil, ErrJournal
			}
			names = append(names, operationID)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, ErrJournal
		}
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrJournal, err)
		}
	}
	sort.Strings(names)
	records := make([]*tammyv1.RestoreStatus, 0, len(names))
	actionableNames := make([]string, 0, len(names))
	for _, operationID := range names {
		status, err := store.load(operationID)
		if err != nil {
			return nil, ErrJournal
		}
		if status.State == tammyv1.RestoreState_RESTORE_STATE_ROLLED_BACK {
			continue
		}
		actionableNames = append(actionableNames, operationID)
		records = append(records, status)
	}
	names = actionableNames
	start := sort.SearchStrings(names, afterOperationID)
	if afterOperationID != "" && start < len(names) && names[start] == afterOperationID {
		start++
	} else if afterOperationID != "" {
		start = sort.Search(len(names), func(index int) bool { return names[index] > afterOperationID })
	}
	end := min(start+int(limit), len(records))
	page := &RestoreJournalPage{Records: make([]*tammyv1.RestoreStatus, end-start)}
	for index := start; index < end; index++ {
		page.Records[index-start] = cloneStatus(records[index])
	}
	if end < len(records) && end > start {
		page.NextAfterOperationID = records[end-1].OperationId
	}
	return page, nil
}

func (store *JournalStore) Close() error {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return nil
	}
	store.closed = true
	zeroBytes(store.auth)
	store.auth = nil
	if store.root == nil {
		return nil
	}
	err := store.root.Close()
	store.root = nil
	if err != nil {
		return ErrJournal
	}
	return nil
}

func (store *JournalStore) persist(status *tammyv1.RestoreStatus) error {
	if !validStatus(status) {
		return ErrJournal
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(status)
	if err != nil || len(payload) == 0 || len(payload) > maximumJournalBytes-24-4-sha256.Size {
		return ErrJournal
	}
	frame := make([]byte, 0, 24+4+len(payload)+sha256.Size)
	frame = append(frame, journalMagic[:]...)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
	frame = append(frame, payload...)
	authenticator := hmac.New(sha256.New, store.auth)
	_, _ = authenticator.Write(journalAuthenticationDomain)
	_, _ = authenticator.Write(frame)
	frame = append(frame, authenticator.Sum(nil)...)
	name := journalName(status.OperationId)
	temporary := store.journalTemporaryName(status.OperationId)
	_ = store.root.Remove(temporary)
	file, err := store.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrJournal
	}
	owned := true
	defer func() {
		_ = file.Close()
		if owned {
			_ = store.root.Remove(temporary)
		}
	}()
	for offset := 0; offset < len(frame); {
		written, writeErr := store.hooks.write(file, frame[offset:])
		if written <= 0 || written > len(frame)-offset {
			return ErrJournal
		}
		offset += written
		if writeErr != nil {
			return ErrJournal
		}
	}
	if err := store.hooks.syncFile(file); err != nil {
		return ErrJournal
	}
	if err := file.Close(); err != nil {
		return ErrJournal
	}
	if err := store.hooks.rename(store.root, temporary, name); err != nil {
		return ErrJournal
	}
	owned = false
	if err := store.hooks.syncDirectory(store.root); err != nil {
		return ErrJournal
	}
	return nil
}

func (store *JournalStore) load(operationID string) (*tammyv1.RestoreStatus, error) {
	name := journalName(operationID)
	initial, err := store.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !initial.Mode().IsRegular() || initial.Mode().Perm() != 0o600 || initial.Size() <= 0 || initial.Size() > maximumJournalBytes {
		return nil, ErrJournal
	}
	file, err := store.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != initial.Size() || !os.SameFile(initial, info) {
		return nil, ErrJournal
	}
	frame := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(file, frame); err != nil {
		return nil, ErrJournal
	}
	var extra [1]byte
	if read, readErr := file.Read(extra[:]); read != 0 || readErr != io.EOF {
		return nil, ErrJournal
	}
	final, err := store.root.Lstat(name)
	if err != nil || !os.SameFile(initial, final) {
		return nil, ErrJournal
	}
	if len(frame) < 24+4+sha256.Size || !bytes.Equal(frame[:24], journalMagic[:]) {
		return nil, ErrJournal
	}
	payloadLength := int(binary.BigEndian.Uint32(frame[24:28]))
	if payloadLength <= 0 || payloadLength != len(frame)-24-4-sha256.Size {
		return nil, ErrJournal
	}
	authenticator := hmac.New(sha256.New, store.auth)
	_, _ = authenticator.Write(journalAuthenticationDomain)
	_, _ = authenticator.Write(frame[:len(frame)-sha256.Size])
	if !hmac.Equal(authenticator.Sum(nil), frame[len(frame)-sha256.Size:]) {
		return nil, ErrJournal
	}
	payload := frame[28 : 28+payloadLength]
	status := new(tammyv1.RestoreStatus)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, status); err != nil || !validStatus(status) || status.OperationId != operationID {
		return nil, ErrJournal
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(status)
	if err != nil || !bytes.Equal(canonical, payload) {
		return nil, ErrJournal
	}
	return status, nil
}

func (store *JournalStore) removePartialTemps() error {
	directory, err := store.root.Open(".")
	if err != nil {
		return ErrJournal
	}
	owned := make([]string, 0, 256)
	totalEntries := 0
	for {
		entries, readErr := directory.ReadDir(256)
		totalEntries += len(entries)
		if totalEntries > maximumJournalDirectoryEntries {
			_ = directory.Close()
			return ErrJournal
		}
		for _, entry := range entries {
			if store.validJournalTemporaryName(entry.Name()) {
				owned = append(owned, entry.Name())
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = directory.Close()
			return ErrJournal
		}
	}
	if err := directory.Close(); err != nil {
		return ErrJournal
	}
	sort.Strings(owned)
	if len(owned) > 256 {
		owned = owned[:256]
	}
	removed := false
	for _, name := range owned {
		if err := store.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrJournal
		}
		removed = true
	}
	if removed && store.hooks.syncDirectory(store.root) != nil {
		return ErrJournal
	}
	return nil
}

func (store *JournalStore) journalTemporaryName(operationID string) string {
	authenticator := hmac.New(sha256.New, store.auth)
	_, _ = authenticator.Write(journalTemporaryNameDomain)
	_, _ = authenticator.Write([]byte(operationID))
	return ".tammy-restore-journal-" + operationID + "-" + hex.EncodeToString(authenticator.Sum(nil)) + ".tmp"
}

func (store *JournalStore) validJournalTemporaryName(name string) bool {
	const prefix = ".tammy-restore-journal-"
	const suffix = ".tmp"
	const tagBytes = sha256.Size * 2
	if len(name) != len(prefix)+36+1+tagBytes+len(suffix) || !strings.HasPrefix(name, prefix) ||
		!strings.HasSuffix(name, suffix) {
		return false
	}
	operationID := name[len(prefix) : len(prefix)+36]
	if !ids.IsCanonicalV7(operationID) || name[len(prefix)+36] != '-' {
		return false
	}
	return hmac.Equal([]byte(name), []byte(store.journalTemporaryName(operationID)))
}

func validStatus(status *tammyv1.RestoreStatus) bool {
	if status == nil || len(status.ProtoReflect().GetUnknown()) != 0 || protovalidate.Validate(status) != nil {
		return false
	}
	switch status.State {
	case tammyv1.RestoreState_RESTORE_STATE_PREPARED:
		return len(status.NewAuditHead) == 0 && (status.Recovery == nil || validPreparedRecovery(status.OperationId, status.Recovery))
	case tammyv1.RestoreState_RESTORE_STATE_STAGED, tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
		tammyv1.RestoreState_RESTORE_STATE_COMPLETE:
		return len(status.NewAuditHead) == sha256.Size && (status.Recovery == nil ||
			(validStagedRecovery(status.OperationId, status.Recovery) &&
				subtle.ConstantTimeCompare(status.NewAuditHead, status.Recovery.FinalizedAuditHead) == 1))
	case tammyv1.RestoreState_RESTORE_STATE_ROLLED_BACK:
		return len(status.NewAuditHead) == 0 && (status.Recovery == nil || validPreparedRecovery(status.OperationId, status.Recovery)) ||
			len(status.NewAuditHead) == sha256.Size && status.Recovery != nil && validStagedRecovery(status.OperationId, status.Recovery) &&
				subtle.ConstantTimeCompare(status.NewAuditHead, status.Recovery.FinalizedAuditHead) == 1
	default:
		return false
	}
}

func validPreparedRecovery(operationID string, recovery *tammyv1.RestoreRecoveryRecord) bool {
	return recovery != nil && len(recovery.ProtoReflect().GetUnknown()) == 0 && protovalidate.Validate(recovery) == nil &&
		ids.IsCanonicalV7(recovery.WorkspaceId) && ids.IsCanonicalV7(recovery.PreRestoreArchiveId) &&
		len(recovery.PreRestoreArchiveHash) == sha256.Size && len(recovery.ArtifactOwnershipDigest) == sha256.Size &&
		len(recovery.StageOwnerMarkerSha256) == sha256.Size && len(recovery.RollbackOwnerMarkerSha256) == sha256.Size &&
		len(recovery.RollbackPredecessorHash) == 0 && len(recovery.ActivatedDatabaseSha256) == 0 &&
		validRestoreArtifactBasenames(operationID, recovery.WorkspaceId, recovery.StageBasename, recovery.RollbackBasename) &&
		validPreRestoreArchivePreparedName(operationID, recovery.PreRestoreArchivePreparedBasename) &&
		recovery.PreRestoreArchiveFinalBasename == preRestoreArchiveName(recovery.PreRestoreArchiveId) &&
		recovery.FinalizedGeneration == nil && len(recovery.FinalizedAuditHead) == 0 && recovery.SchemaVersion == nil &&
		len(recovery.MigrationManifestHash) == 0 && !recovery.PostSwapVerified &&
		!recovery.MachineCredentialsRevoked && !recovery.MirrorPublished
}

func validStagedRecovery(operationID string, recovery *tammyv1.RestoreRecoveryRecord) bool {
	if recovery == nil || len(recovery.ProtoReflect().GetUnknown()) != 0 || protovalidate.Validate(recovery) != nil ||
		!ids.IsCanonicalV7(recovery.WorkspaceId) || !ids.IsCanonicalV7(recovery.PreRestoreArchiveId) ||
		len(recovery.PreRestoreArchiveHash) != sha256.Size || len(recovery.ArtifactOwnershipDigest) != sha256.Size ||
		len(recovery.StageOwnerMarkerSha256) != sha256.Size || len(recovery.RollbackOwnerMarkerSha256) != sha256.Size ||
		len(recovery.RollbackPredecessorHash) != sha256.Size || len(recovery.ActivatedDatabaseSha256) != sha256.Size ||
		!validRestoreArtifactBasenames(operationID, recovery.WorkspaceId, recovery.StageBasename, recovery.RollbackBasename) ||
		!validPreRestoreArchivePreparedName(operationID, recovery.PreRestoreArchivePreparedBasename) ||
		recovery.PreRestoreArchiveFinalBasename != preRestoreArchiveName(recovery.PreRestoreArchiveId) ||
		recovery.FinalizedGeneration == nil || *recovery.FinalizedGeneration < 2 ||
		len(recovery.FinalizedAuditHead) != sha256.Size || recovery.SchemaVersion == nil || *recovery.SchemaVersion == 0 ||
		len(recovery.MigrationManifestHash) != sha256.Size {
		return false
	}
	return (!recovery.MachineCredentialsRevoked || recovery.PostSwapVerified) &&
		(!recovery.MirrorPublished || recovery.MachineCredentialsRevoked)
}

func samePreparedRecovery(left, right *tammyv1.RestoreRecoveryRecord) bool {
	return left != nil && right != nil && left.WorkspaceId == right.WorkspaceId &&
		left.PreRestoreArchiveId == right.PreRestoreArchiveId &&
		subtle.ConstantTimeCompare(left.PreRestoreArchiveHash, right.PreRestoreArchiveHash) == 1 &&
		subtle.ConstantTimeCompare(left.ArtifactOwnershipDigest, right.ArtifactOwnershipDigest) == 1 &&
		subtle.ConstantTimeCompare(left.StageOwnerMarkerSha256, right.StageOwnerMarkerSha256) == 1 &&
		subtle.ConstantTimeCompare(left.RollbackOwnerMarkerSha256, right.RollbackOwnerMarkerSha256) == 1 &&
		left.StageBasename == right.StageBasename && left.RollbackBasename == right.RollbackBasename &&
		left.PreRestoreArchivePreparedBasename == right.PreRestoreArchivePreparedBasename &&
		left.PreRestoreArchiveFinalBasename == right.PreRestoreArchiveFinalBasename &&
		left.FinalizedGeneration == nil && len(left.FinalizedAuditHead) == 0 && left.SchemaVersion == nil &&
		len(left.MigrationManifestHash) == 0 && len(left.RollbackPredecessorHash) == 0 && len(left.ActivatedDatabaseSha256) == 0 &&
		(len(right.RollbackPredecessorHash) == 0 || len(right.RollbackPredecessorHash) == sha256.Size) &&
		(len(right.ActivatedDatabaseSha256) == 0 || len(right.ActivatedDatabaseSha256) == sha256.Size)
}

func sameStagedRecovery(left, right *tammyv1.RestoreRecoveryRecord) bool {
	return validStagedRecoveryFromAnyOperation(left) && validStagedRecoveryFromAnyOperation(right) &&
		left.WorkspaceId == right.WorkspaceId && left.PreRestoreArchiveId == right.PreRestoreArchiveId &&
		subtle.ConstantTimeCompare(left.PreRestoreArchiveHash, right.PreRestoreArchiveHash) == 1 &&
		subtle.ConstantTimeCompare(left.ArtifactOwnershipDigest, right.ArtifactOwnershipDigest) == 1 &&
		subtle.ConstantTimeCompare(left.StageOwnerMarkerSha256, right.StageOwnerMarkerSha256) == 1 &&
		subtle.ConstantTimeCompare(left.RollbackOwnerMarkerSha256, right.RollbackOwnerMarkerSha256) == 1 &&
		subtle.ConstantTimeCompare(left.RollbackPredecessorHash, right.RollbackPredecessorHash) == 1 &&
		subtle.ConstantTimeCompare(left.ActivatedDatabaseSha256, right.ActivatedDatabaseSha256) == 1 &&
		left.StageBasename == right.StageBasename && left.RollbackBasename == right.RollbackBasename &&
		left.PreRestoreArchivePreparedBasename == right.PreRestoreArchivePreparedBasename &&
		left.PreRestoreArchiveFinalBasename == right.PreRestoreArchiveFinalBasename &&
		left.GetFinalizedGeneration() == right.GetFinalizedGeneration() &&
		subtle.ConstantTimeCompare(left.FinalizedAuditHead, right.FinalizedAuditHead) == 1 &&
		left.GetSchemaVersion() == right.GetSchemaVersion() &&
		subtle.ConstantTimeCompare(left.MigrationManifestHash, right.MigrationManifestHash) == 1
}

func validStagedRecoveryFromAnyOperation(recovery *tammyv1.RestoreRecoveryRecord) bool {
	if recovery == nil {
		return false
	}
	operationID, ok := restoreOperationFromStageBasename(recovery.StageBasename)
	if !ok {
		return false
	}
	return validStagedRecovery(operationID, recovery)
}

func validNextRecoveryCheckpoint(current, next *tammyv1.RestoreRecoveryRecord) bool {
	return !current.PostSwapVerified && !current.MachineCredentialsRevoked && !current.MirrorPublished &&
		next.PostSwapVerified && !next.MachineCredentialsRevoked && !next.MirrorPublished ||
		current.PostSwapVerified && !current.MachineCredentialsRevoked && !current.MirrorPublished &&
			next.PostSwapVerified && next.MachineCredentialsRevoked && !next.MirrorPublished ||
		current.PostSwapVerified && current.MachineCredentialsRevoked && !current.MirrorPublished &&
			next.PostSwapVerified && next.MachineCredentialsRevoked && next.MirrorPublished
}

func validJournalTransition(from, to tammyv1.RestoreState) bool {
	return from == tammyv1.RestoreState_RESTORE_STATE_PREPARED && to == tammyv1.RestoreState_RESTORE_STATE_STAGED ||
		from == tammyv1.RestoreState_RESTORE_STATE_STAGED && to == tammyv1.RestoreState_RESTORE_STATE_SWAPPED ||
		from == tammyv1.RestoreState_RESTORE_STATE_SWAPPED && to == tammyv1.RestoreState_RESTORE_STATE_COMPLETE ||
		(from == tammyv1.RestoreState_RESTORE_STATE_PREPARED || from == tammyv1.RestoreState_RESTORE_STATE_STAGED) &&
			to == tammyv1.RestoreState_RESTORE_STATE_ROLLED_BACK
}

func validTransitionAuditHead(from, to tammyv1.RestoreState, head []byte) bool {
	if to != tammyv1.RestoreState_RESTORE_STATE_ROLLED_BACK {
		return len(head) == sha256.Size
	}
	return from == tammyv1.RestoreState_RESTORE_STATE_PREPARED && len(head) == 0 ||
		from == tammyv1.RestoreState_RESTORE_STATE_STAGED && len(head) == sha256.Size
}

func journalName(operationID string) string { return operationID + ".restore.pb" }

func cloneStatus(status *tammyv1.RestoreStatus) *tammyv1.RestoreStatus {
	return proto.Clone(status).(*tammyv1.RestoreStatus)
}

func normalizedJournalHooks(injected *journalHooks) journalHooks {
	hooks := journalHooks{
		openRoot: func(path string) (*os.Root, error) { return os.OpenRoot(path) },
		write:    func(file *os.File, value []byte) (int, error) { return file.Write(value) },
		syncFile: func(file *os.File) error { return file.Sync() },
		rename:   func(root *os.Root, oldName, newName string) error { return root.Rename(oldName, newName) },
		syncDirectory: func(root *os.Root) error {
			directory, err := root.Open(".")
			if err != nil {
				return err
			}
			defer directory.Close()
			return directory.Sync()
		},
	}
	if injected == nil {
		return hooks
	}
	if injected.openRoot != nil {
		hooks.openRoot = injected.openRoot
	}
	if injected.write != nil {
		hooks.write = injected.write
	}
	if injected.syncFile != nil {
		hooks.syncFile = injected.syncFile
	}
	if injected.rename != nil {
		hooks.rename = injected.rename
	}
	if injected.syncDirectory != nil {
		hooks.syncDirectory = injected.syncDirectory
	}
	return hooks
}
