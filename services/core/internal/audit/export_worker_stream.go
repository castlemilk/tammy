package audit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	workerEvidenceReservedMembers = 9
	workerEvidenceReservedBytes   = 4 << 10
)

type evidenceMemberSource struct {
	name   string
	data   []byte
	file   string
	size   uint64
	digest [sha256.Size]byte
}

type evidenceMemberRegistry struct {
	byName map[string]evidenceMemberSource
	total  uint64
}

func newEvidenceMemberRegistry() *evidenceMemberRegistry {
	return &evidenceMemberRegistry{byName: make(map[string]evidenceMemberSource)}
}

func (registry *evidenceMemberRegistry) addBytes(name string, data []byte) error {
	return registry.add(evidenceMemberSource{name: name, data: data, size: uint64(len(data)), digest: sha256.Sum256(data)},
		maxEvidenceArchiveMembers-2)
}

func (registry *evidenceMemberRegistry) addFinalBytes(name string, data []byte) error {
	return registry.add(evidenceMemberSource{name: name, data: data, size: uint64(len(data)), digest: sha256.Sum256(data)},
		maxEvidenceArchiveMembers)
}

func (registry *evidenceMemberRegistry) addFile(name, file string, size uint64, digest [sha256.Size]byte) error {
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || uint64(info.Size()) != size {
		return ErrEvidenceArchive
	}
	return registry.add(evidenceMemberSource{name: name, file: file, size: size, digest: digest}, maxEvidenceArchiveMembers-2)
}

func (registry *evidenceMemberRegistry) add(source evidenceMemberSource, memberLimit int) error {
	if registry == nil || registry.byName == nil || !safeArchivePath(source.name) ||
		source.size > maxEvidenceArchiveMember || memberLimit < 0 || memberLimit > maxEvidenceArchiveMembers ||
		len(registry.byName) >= memberLimit {
		return ErrEvidenceArchive
	}
	if source.file == "" && uint64(len(source.data)) != source.size || source.file != "" && source.data != nil {
		return ErrEvidenceArchive
	}
	if _, duplicate := registry.byName[source.name]; duplicate {
		return ErrEvidenceArchive
	}
	total, ok := checkedAggregateBytes(registry.total, source.size, maxEvidenceArchiveBytes)
	if !ok {
		return ErrEvidenceArchive
	}
	registry.byName[source.name] = source
	registry.total = total
	return nil
}

func (registry *evidenceMemberRegistry) sortedNames() []string {
	names := make([]string, 0, len(registry.byName))
	for name := range registry.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type boundedSpoolFile struct {
	file   *os.File
	path   string
	size   uint64
	digest hash.Hash
}

func createBoundedSpoolFile(directory, name string) (*boundedSpoolFile, error) {
	filePath := filepath.Join(directory, name)
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	return &boundedSpoolFile{file: file, path: filePath, digest: sha256.New()}, nil
}

func (spool *boundedSpoolFile) write(parts ...[]byte) error {
	if spool == nil || spool.file == nil || spool.digest == nil {
		return ErrEvidenceArchive
	}
	for _, part := range parts {
		if uint64(len(part)) > uint64(maxEvidenceArchiveMember)-spool.size {
			return ErrEvidenceArchive
		}
		written, err := spool.file.Write(part)
		if err != nil || written != len(part) {
			return ErrEvidenceArchive
		}
		_, _ = spool.digest.Write(part)
		spool.size += uint64(written)
	}
	return nil
}

func (spool *boundedSpoolFile) closeInto(registry *evidenceMemberRegistry, archivePath string) error {
	if spool == nil || spool.file == nil || spool.size == 0 {
		return ErrEvidenceArchive
	}
	if err := spool.file.Sync(); err != nil {
		return ErrEvidenceArchive
	}
	if err := spool.file.Close(); err != nil {
		spool.file = nil
		return ErrEvidenceArchive
	}
	spool.file = nil
	var digest [sha256.Size]byte
	copy(digest[:], spool.digest.Sum(nil))
	return registry.addFile(archivePath, spool.path, spool.size, digest)
}

func (spool *boundedSpoolFile) close() {
	if spool != nil && spool.file != nil {
		_ = spool.file.Close()
		spool.file = nil
	}
}

type streamingEvidenceSpool struct {
	directory   string
	heads       *boundedSpoolFile
	commitments *boundedSpoolFile
	openings    *boundedSpoolFile
	cleaned     bool
}

func newStreamingEvidenceSpool(parent string) (*streamingEvidenceSpool, error) {
	directory, err := os.MkdirTemp(parent, ".tammy-evidence-")
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	spool := &streamingEvidenceSpool{directory: directory}
	if err := os.Chmod(directory, 0o700); err != nil {
		spool.cleanup()
		return nil, ErrEvidenceArchive
	}
	if spool.heads, err = createBoundedSpoolFile(directory, "heads.bin"); err != nil {
		spool.cleanup()
		return nil, err
	}
	if spool.commitments, err = createBoundedSpoolFile(directory, "event-commitments.jsonl"); err != nil {
		spool.cleanup()
		return nil, err
	}
	if spool.openings, err = createBoundedSpoolFile(directory, "filter-openings.jsonl"); err != nil {
		spool.cleanup()
		return nil, err
	}
	return spool, nil
}

func (spool *streamingEvidenceSpool) closeInto(registry *evidenceMemberRegistry) error {
	if spool == nil || registry == nil ||
		spool.heads.closeInto(registry, "chain/heads.bin") != nil ||
		spool.commitments.closeInto(registry, "chain/event-commitments.jsonl") != nil ||
		spool.openings.closeInto(registry, "chain/filter-openings.jsonl") != nil {
		return ErrEvidenceArchive
	}
	return nil
}

func (spool *streamingEvidenceSpool) cleanup() {
	if spool == nil || spool.cleaned {
		return
	}
	spool.cleaned = true
	spool.heads.close()
	spool.commitments.close()
	spool.openings.close()
	if spool.directory != "" {
		_ = os.RemoveAll(spool.directory)
	}
}

type preparedStreamingEvidenceArchive struct {
	registry        *evidenceMemberRegistry
	header          ChainHeader
	signingKey      SigningKeyRecord
	signingKeyChain *tammyv1.AuditSigningKeyChain
	createdAt       time.Time
}

func (prepared *preparedStreamingEvidenceArchive) build(signer evidenceArchiveSigner) ([]byte, error) {
	if prepared == nil || prepared.registry == nil || signer == nil || prepared.signingKeyChain == nil ||
		len(prepared.signingKeyChain.Keys) == 0 {
		return nil, ErrEvidenceArchive
	}
	names := prepared.registry.sortedNames()
	objects := make([]*tammyv1.AuditExportObject, 0, len(names))
	for _, name := range names {
		source := prepared.registry.byName[name]
		objects = append(objects, &tammyv1.AuditExportObject{Path: name, Sha256: append([]byte(nil), source.digest[:]...),
			ByteLength: source.size})
	}
	manifest := &tammyv1.AuditExportManifest{
		Format: evidenceArchiveFormat, WorkspaceId: prepared.header.WorkspaceID, Generation: prepared.header.Generation,
		StartSequence: 1, EndSequence: prepared.header.CurrentSequence,
		ChainSalt: append([]byte(nil), prepared.header.ChainSalt...), GenesisHash: append([]byte(nil), prepared.header.GenesisHash[:]...),
		VerifiedHead: append([]byte(nil), prepared.header.CurrentHead[:]...), SigningKeyId: prepared.signingKey.KeyID,
		RootSigningKeyId: prepared.signingKeyChain.Keys[0].KeyId, SigningKeyEpoch: prepared.signingKey.Epoch,
		CreatedAt: timestamppb.New(prepared.createdAt.UTC()), Objects: objects,
	}
	manifestJSON, err := canonical.NormalizedJSON(manifest)
	if err != nil || uint64(len(manifestJSON)) > maxEvidenceArchiveMember {
		return nil, ErrEvidenceArchive
	}
	manifestHash := sha256.Sum256(manifestJSON)
	signature, err := signer(prepared.signingKey, manifestHash)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(prepared.signingKey.PublicKey, manifestHash[:], signature) {
		return nil, ErrEvidenceArchive
	}
	if err := prepared.registry.addFinalBytes("manifest.json", manifestJSON); err != nil {
		return nil, err
	}
	if err := prepared.registry.addFinalBytes("signature.ed25519", signature); err != nil {
		return nil, err
	}
	return writeDeterministicZIPFromRegistry(prepared.registry)
}

type boundedArchiveBuffer struct {
	bytes.Buffer
}

func (buffer *boundedArchiveBuffer) Write(data []byte) (int, error) {
	if len(data) > maxEvidenceArchiveBytes-buffer.Len() {
		return 0, ErrEvidenceArchive
	}
	return buffer.Buffer.Write(data)
}

func writeDeterministicZIPFromRegistry(registry *evidenceMemberRegistry) ([]byte, error) {
	if registry == nil || len(registry.byName) == 0 || len(registry.byName) > maxEvidenceArchiveMembers ||
		registry.total > maxEvidenceArchiveBytes {
		return nil, ErrEvidenceArchive
	}
	var output boundedArchiveBuffer
	writer := zip.NewWriter(&output)
	fixedTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range registry.sortedNames() {
		source := registry.byName[name]
		if source.file == "" && (uint64(len(source.data)) != source.size || sha256.Sum256(source.data) != source.digest) {
			_ = writer.Close()
			return nil, ErrEvidenceArchive
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store, CreatorVersion: 20, ReaderVersion: 20}
		header.SetModTime(fixedTime)
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, ErrEvidenceArchive
		}
		if source.file == "" {
			if written, err := entry.Write(source.data); err != nil || uint64(written) != source.size {
				_ = writer.Close()
				return nil, ErrEvidenceArchive
			}
			continue
		}
		file, err := os.Open(source.file)
		if err != nil {
			_ = writer.Close()
			return nil, ErrEvidenceArchive
		}
		digest := sha256.New()
		written, copyErr := io.Copy(entry, io.TeeReader(io.LimitReader(file, int64(source.size)+1), digest))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != int64(source.size) || !bytes.Equal(digest.Sum(nil), source.digest[:]) {
			_ = writer.Close()
			return nil, ErrEvidenceArchive
		}
	}
	if err := writer.Close(); err != nil || output.Len() > maxEvidenceArchiveBytes {
		return nil, ErrEvidenceArchive
	}
	return output.Bytes(), nil
}

type streamingArchiveCollector struct {
	registry             *evidenceMemberRegistry
	spool                *streamingEvidenceSpool
	filter               *tammyv1.AuditEventFilter
	eventTypes           map[tammyv1.AuditEventType]struct{}
	descriptors          descriptorRegistry
	descriptorBytes      uint64
	selectedFingerprints map[[sha256.Size]byte]struct{}
	eventLines           bytes.Buffer
	keyChain             *tammyv1.AuditSigningKeyChain
	rotationTargets      map[uint64][]int
	rotationProofs       map[int]*tammyv1.AuditSigningKeyRotationEventProof
}

func newStreamingArchiveCollector(
	registry *evidenceMemberRegistry,
	spool *streamingEvidenceSpool,
	filter *tammyv1.AuditEventFilter,
	keyChain *tammyv1.AuditSigningKeyChain,
	snapshot ChainHeader,
) (*streamingArchiveCollector, error) {
	if registry == nil || spool == nil || filter == nil || keyChain == nil {
		return nil, ErrEvidenceArchive
	}
	collector := &streamingArchiveCollector{registry: registry, spool: spool, filter: filter,
		eventTypes: make(map[tammyv1.AuditEventType]struct{}, len(filter.EventTypes)), descriptors: make(descriptorRegistry),
		selectedFingerprints: make(map[[sha256.Size]byte]struct{}), keyChain: keyChain,
		rotationTargets: make(map[uint64][]int), rotationProofs: make(map[int]*tammyv1.AuditSigningKeyRotationEventProof)}
	for _, eventType := range filter.EventTypes {
		collector.eventTypes[eventType] = struct{}{}
	}
	keyChain.EventProofs = nil
	for index, link := range keyChain.Links {
		if link.Generation != snapshot.Generation || link.PriorSequence > snapshot.CurrentSequence {
			continue
		}
		if link.PriorSequence == snapshot.CurrentSequence {
			if !bytes.Equal(link.PriorHead, snapshot.CurrentHead[:]) {
				return nil, ErrEvidenceArchive
			}
			continue
		}
		collector.rotationTargets[link.PriorSequence+1] = append(collector.rotationTargets[link.PriorSequence+1], index)
	}
	return collector, nil
}

func (collector *streamingArchiveCollector) descriptorFor(
	ctx context.Context,
	executor Executor,
	stored StoredEvent,
) ([sha256.Size]byte, *validatedDescriptorSet, error) {
	if stored.Event == nil || len(stored.Event.PayloadSchemaFingerprint) != sha256.Size {
		return [sha256.Size]byte{}, nil, ErrEvidenceArchive
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], stored.Event.PayloadSchemaFingerprint)
	if descriptor := collector.descriptors[fingerprint]; descriptor != nil {
		return fingerprint, descriptor, nil
	}
	if len(collector.descriptors) >= maxEvidenceArchiveMembers {
		return [sha256.Size]byte{}, nil, ErrEvidenceArchive
	}
	encoded, err := loadRawDescriptorSet(ctx, executor, fingerprint)
	if err != nil {
		return [sha256.Size]byte{}, nil, err
	}
	aggregate, ok := checkedAggregateBytes(collector.descriptorBytes, uint64(len(encoded)), maxEvidenceDescriptorBytes)
	if !ok {
		return [sha256.Size]byte{}, nil, ErrEvidenceArchive
	}
	descriptor, err := newValidatedDescriptorSet(encoded)
	if err != nil || descriptor.fingerprint != fingerprint {
		return [sha256.Size]byte{}, nil, ErrEvidenceArchive
	}
	collector.descriptorBytes = aggregate
	collector.descriptors[fingerprint] = descriptor
	return fingerprint, descriptor, nil
}

func (collector *streamingArchiveCollector) acceptEvent(
	ctx context.Context,
	executor Executor,
	stored StoredEvent,
) error {
	fingerprint, descriptor, err := collector.descriptorFor(ctx, executor, stored)
	if err != nil {
		return err
	}
	prepared, err := reconstructStoredEventWithValidatedDescriptor(stored, descriptor)
	if err != nil || prepared.PayloadType != stored.PayloadType ||
		!bytes.Equal(prepared.PayloadProto, stored.PayloadProto) || !bytes.Equal(prepared.PayloadJSON, stored.PayloadJSON) ||
		!bytes.Equal(prepared.CanonicalEvent, stored.CanonicalEvent) || !bytes.Equal(prepared.EventProto, stored.EventProto) ||
		!bytes.Equal(prepared.Event.EventHash, stored.Event.EventHash) {
		return ErrEvidenceArchive
	}
	if err := collector.spool.heads.write(stored.Event.EventHash); err != nil {
		return err
	}
	if err := collector.spool.commitments.write(stored.CanonicalEvent, []byte{'\n'}); err != nil {
		return err
	}
	openingLine, err := buildFilterOpeningLine(stored, collector.filter)
	if err != nil || collector.spool.openings.write(openingLine, []byte{'\n'}) != nil {
		return ErrEvidenceArchive
	}
	for _, linkIndex := range collector.rotationTargets[stored.Event.Sequence] {
		link := collector.keyChain.Links[linkIndex]
		event, matches := rotationEventMatchesLink(link, stored.EventProto, stored.PayloadProto)
		if !matches || !bytes.Equal(event.EventHash, stored.Event.EventHash) {
			return ErrEvidenceArchive
		}
		openings := stored.Event.CommitmentOpenings
		collector.rotationProofs[linkIndex] = &tammyv1.AuditSigningKeyRotationEventProof{
			SuccessorEpoch: link.SuccessorEpoch, SchemaFingerprint: append([]byte(nil), stored.Event.PayloadSchemaFingerprint...),
			PayloadIdentityBlinding: append([]byte(nil), openings.PayloadIdentityBlinding...),
			EventTypeBlinding:       append([]byte(nil), openings.EventTypeBlinding...),
			OccurredAtBlinding:      append([]byte(nil), openings.OccurredAtBlinding...),
		}
		collector.selectedFingerprints[fingerprint] = struct{}{}
	}
	if !collector.matches(stored.Event) {
		return nil
	}
	publicJSON, err := canonicalStoredEventJSONWithDescriptor(stored, descriptor)
	if err != nil || len(publicJSON)+1 > maxEvidenceArchiveMember-collector.eventLines.Len() {
		return ErrEvidenceArchive
	}
	_, _ = collector.eventLines.Write(publicJSON)
	_ = collector.eventLines.WriteByte('\n')
	prefix := fmt.Sprintf("events/%020d/", stored.Event.Sequence)
	for _, member := range []struct {
		name string
		data []byte
	}{
		{name: prefix + "event.pb", data: stored.EventProto},
		{name: prefix + "payload.pb", data: stored.PayloadProto},
		{name: prefix + "payload.json", data: stored.PayloadJSON},
		{name: prefix + "payload.type", data: []byte(stored.PayloadType)},
	} {
		if err := collector.registry.addBytes(member.name, member.data); err != nil {
			return err
		}
	}
	collector.selectedFingerprints[fingerprint] = struct{}{}
	return nil
}

func (collector *streamingArchiveCollector) matches(event *tammyv1.AuditEvent) bool {
	if event == nil || collector == nil || collector.filter == nil ||
		collector.filter.StartSequence != nil && event.Sequence < *collector.filter.StartSequence ||
		collector.filter.EndSequence != nil && event.Sequence > *collector.filter.EndSequence {
		return false
	}
	if len(collector.eventTypes) != 0 {
		if _, selected := collector.eventTypes[event.Type]; !selected {
			return false
		}
	}
	if collector.filter.ActorUserId != nil && (event.Actor == nil || event.Actor.ActorUserId != *collector.filter.ActorUserId) {
		return false
	}
	if collector.filter.FromTime != nil && event.OccurredAt.AsTime().Before(collector.filter.FromTime.AsTime()) ||
		collector.filter.ToTime != nil && event.OccurredAt.AsTime().After(collector.filter.ToTime.AsTime()) {
		return false
	}
	return true
}

func (collector *streamingArchiveCollector) finish() error {
	for sequence, indexes := range collector.rotationTargets {
		_ = sequence
		for _, index := range indexes {
			proof := collector.rotationProofs[index]
			if proof == nil {
				return ErrEvidenceArchive
			}
		}
	}
	for index := range collector.keyChain.Links {
		if proof := collector.rotationProofs[index]; proof != nil {
			collector.keyChain.EventProofs = append(collector.keyChain.EventProofs, proof)
		}
	}
	if err := collector.spool.closeInto(collector.registry); err != nil {
		return err
	}
	if err := collector.registry.addBytes("events.jsonl", collector.eventLines.Bytes()); err != nil {
		return err
	}
	for fingerprint := range collector.selectedFingerprints {
		descriptor := collector.descriptors[fingerprint]
		if descriptor == nil {
			return ErrEvidenceArchive
		}
		if err := collector.registry.addBytes(descriptorArchivePath(fingerprint), descriptor.encoded); err != nil {
			return err
		}
	}
	encodedKeyChain, err := proto.MarshalOptions{Deterministic: true}.Marshal(collector.keyChain)
	if err != nil || len(encodedKeyChain) == 0 {
		return ErrEvidenceArchive
	}
	return collector.registry.addBytes(signingKeyChainArchivePath, encodedKeyChain)
}

func prepareStreamingEvidenceArchive(
	ctx context.Context,
	executor ServiceTransaction,
	job ExportJob,
	evidence []EvidenceObject,
	spool *streamingEvidenceSpool,
) (*preparedStreamingEvidenceArchive, error) {
	current, err := LoadChainHeader(ctx, executor, job.WorkspaceID, job.SnapshotGeneration)
	if err != nil || current.CurrentSequence < job.SnapshotSequence || job.SnapshotSequence == 0 || len(job.SnapshotHead) != sha256.Size {
		return nil, ErrExportJob
	}
	var snapshotHead [sha256.Size]byte
	copy(snapshotHead[:], job.SnapshotHead)
	header := current
	header.CurrentSequence = job.SnapshotSequence
	header.CurrentHead = snapshotHead
	filter := new(tammyv1.AuditEventFilter)
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(job.FilterProto, filter) != nil ||
		len(filter.ProtoReflect().GetUnknown()) != 0 || !canonicalAuditEventFilter(filter) {
		return nil, ErrEvidenceArchive
	}
	canonicalFilter, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil || !bytes.Equal(canonicalFilter, job.FilterProto) {
		return nil, ErrEvidenceArchive
	}
	history, err := LoadSigningKeyHistory(ctx, executor, job.WorkspaceID)
	if err != nil || len(history) == 0 {
		return nil, ErrSigningKey
	}
	key := history[len(history)-1]
	keyChain, err := signingKeyChainFromRecords(history, key)
	if err != nil {
		return nil, err
	}
	registry := newEvidenceMemberRegistry()
	for _, object := range evidence {
		if err := registry.addBytes(object.Path, object.Bytes); err != nil {
			return nil, err
		}
	}
	if err := registry.addBytes("public-key.ed25519", key.PublicKey); err != nil {
		return nil, err
	}
	if err := registry.addBytes("filter.pb", job.FilterProto); err != nil {
		return nil, err
	}
	collector, err := newStreamingArchiveCollector(registry, spool, filter, keyChain, header)
	if err != nil {
		return nil, err
	}
	verifier, err := NewStreamingStoredChainVerifier(ctx, header)
	if err != nil {
		return nil, err
	}
	defer verifier.Close()
	snapshot := StoredEventSnapshot{WorkspaceID: header.WorkspaceID, Generation: header.Generation,
		EndSequence: header.CurrentSequence, EndHead: header.CurrentHead}
	checkpoint := StoredEventCheckpoint{Head: header.GenesisHash}
	for checkpoint.AfterSequence < snapshot.EndSequence {
		page, err := LoadStoredEventPage(ctx, executor, snapshot, checkpoint, StoredEventPageSizeLimit, StoredEventPageByteBudget)
		if err != nil {
			return nil, err
		}
		if err := verifier.AcceptPage(page.Events); err != nil {
			if terminalErr := verifier.TerminalError(); terminalErr != nil {
				return nil, terminalErr
			}
			return nil, ErrEvidenceArchive
		}
		for index := range page.Events {
			if err := collector.acceptEvent(ctx, executor, page.Events[index]); err != nil {
				return nil, err
			}
		}
		checkpoint = page.Checkpoint
		if !page.HasMore && checkpoint.AfterSequence != snapshot.EndSequence {
			return nil, ErrEvidenceArchive
		}
	}
	verification := verifier.Finish()
	if verifier.TerminalError() != nil || verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		verification.VerifiedThroughSequence != header.CurrentSequence || !bytes.Equal(verification.VerifiedHead, header.CurrentHead[:]) {
		return nil, ErrEvidenceArchive
	}
	if err := collector.finish(); err != nil {
		return nil, err
	}
	return &preparedStreamingEvidenceArchive{registry: registry, header: header, signingKey: key,
		signingKeyChain: keyChain, createdAt: job.CreatedAt}, nil
}
