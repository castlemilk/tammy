package audit

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	evidenceArchiveFormat       = "tammy-audit-evidence-v1"
	filterOpeningVersion        = "tammy.audit.filter-opening.v1"
	maxEvidenceArchiveBytes     = 512 << 20
	maxEvidenceArchiveMembers   = 10_000
	maxEvidenceArchiveMember    = 64 << 20
	maxEvidenceDescriptorBytes  = maxEvidenceArchiveBytes
	maxEvidenceArchivePathBytes = 512
	signingKeyChainArchivePath  = "signing-key-chain.pb"
)

var ErrEvidenceArchive = errors.New("audit: invalid evidence archive")

// EvidenceObject is one caller-selected, already-safe evidence member.
type EvidenceObject struct {
	Path  string
	Bytes []byte
}

// preflightEvidenceObjects validates provider-controlled members and their
// share of the final archive budget without copying caller-owned bytes.
func preflightEvidenceObjects(objects []EvidenceObject, reservedMembers int, reservedBytes uint64) error {
	if reservedMembers < 0 || reservedMembers > maxEvidenceArchiveMembers ||
		len(objects) > maxEvidenceArchiveMembers-reservedMembers || reservedBytes > maxEvidenceArchiveBytes {
		return ErrEvidenceArchive
	}
	names := make(map[string]struct{}, len(objects))
	total := reservedBytes
	for _, object := range objects {
		if !safeArchivePath(object.Path) || reservedEvidencePath(object.Path) || len(object.Bytes) > maxEvidenceArchiveMember {
			return ErrEvidenceArchive
		}
		if _, duplicate := names[object.Path]; duplicate {
			return ErrEvidenceArchive
		}
		names[object.Path] = struct{}{}
		size := uint64(len(object.Bytes))
		if size > uint64(maxEvidenceArchiveBytes)-total {
			return ErrEvidenceArchive
		}
		total += size
	}
	return nil
}

// EvidenceArchiveInput contains only exact retained/public bytes plus the
// transient DEK needed to sign. The DEK and encrypted private key are never
// members of the resulting archive.
type EvidenceArchiveInput struct {
	Header             ChainHeader
	Events             []StoredEvent
	SelectedEvents     []StoredEvent
	SelectionApplied   bool
	FilterProto        []byte
	DescriptorSets     map[[sha256.Size]byte][]byte
	descriptorRegistry descriptorRegistry
	Evidence           []EvidenceObject
	SigningKey         SigningKeyRecord
	SigningKeyHistory  []SigningKeyRecord
	DEK                []byte
	CreatedAt          time.Time
}

// EvidenceVerificationResult is the DB-free verification result.
type EvidenceVerificationResult struct {
	Manifest   *tammyv1.AuditExportManifest
	EventCount uint64
}

// BuildSignedEvidenceArchive verifies a full retained chain and emits one
// byte-deterministic ZIP whose canonical manifest signs every public object.
func BuildSignedEvidenceArchive(input EvidenceArchiveInput) ([]byte, error) {
	if len(input.DEK) != 32 {
		return nil, ErrEvidenceArchive
	}
	return buildSignedEvidenceArchiveWithSigner(input, func(record SigningKeyRecord, manifestHash [sha256.Size]byte) ([]byte, error) {
		return SignManifestHash(record, input.DEK, manifestHash)
	})
}

type evidenceArchiveSigner func(SigningKeyRecord, [sha256.Size]byte) ([]byte, error)

func buildSignedEvidenceArchiveWithSigner(input EvidenceArchiveInput, signer evidenceArchiveSigner) ([]byte, error) {
	if signer == nil {
		return nil, ErrEvidenceArchive
	}
	input.DEK = nil
	if err := validateEvidenceInput(input); err != nil {
		return nil, err
	}
	var descriptorSets descriptorRegistry
	var err error
	if input.descriptorRegistry != nil {
		descriptorSets, err = validatePreparedDescriptorRegistry(input.Events, input.descriptorRegistry)
	} else {
		descriptorSets, err = validatedDescriptorRegistry(input.Events, input.DescriptorSets)
	}
	if err != nil || !verifyStoredChainWithDescriptorRegistry(input.Header, input.Events, descriptorSets) {
		return nil, ErrEvidenceArchive
	}
	keyHistory := input.SigningKeyHistory
	if len(keyHistory) == 0 {
		keyHistory = []SigningKeyRecord{input.SigningKey}
	}
	signingKeyChain, err := signingKeyChainFromRecords(keyHistory, input.SigningKey)
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	if err := attachSigningKeyRotationEventProofs(signingKeyChain, input.Header, input.Events); err != nil {
		return nil, ErrEvidenceArchive
	}
	signingKeyChainProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(signingKeyChain)
	if err != nil || len(signingKeyChainProto) == 0 || len(signingKeyChainProto) > maxEvidenceArchiveMember {
		return nil, ErrEvidenceArchive
	}
	objects := map[string][]byte{
		"public-key.ed25519":       append([]byte(nil), input.SigningKey.PublicKey...),
		signingKeyChainArchivePath: signingKeyChainProto,
	}
	selected := input.Events
	if input.SelectionApplied {
		selected = input.SelectedEvents
		objects["filter.pb"] = append([]byte(nil), input.FilterProto...)
		chainHeads := make([]byte, 0, len(input.Events)*sha256.Size)
		for index := range input.Events {
			stored := input.Events[index]
			chainHeads = append(chainHeads, stored.Event.EventHash...)
		}
		objects["chain/heads.bin"] = chainHeads
		eventCommitments, err := buildEventCommitmentProof(input.Events)
		if err != nil {
			return nil, ErrEvidenceArchive
		}
		objects["chain/event-commitments.jsonl"] = eventCommitments
		filter := new(tammyv1.AuditEventFilter)
		if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(input.FilterProto, filter) != nil {
			return nil, ErrEvidenceArchive
		}
		filterOpenings, err := buildFilterOpeningProof(input.Events, filter)
		if err != nil {
			return nil, ErrEvidenceArchive
		}
		objects["chain/filter-openings.jsonl"] = filterOpenings
	}
	selectedFingerprints, err := referencedDescriptorFingerprints(selected)
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	for _, proof := range signingKeyChain.EventProofs {
		if proof == nil || len(proof.SchemaFingerprint) != sha256.Size {
			return nil, ErrEvidenceArchive
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], proof.SchemaFingerprint)
		selectedFingerprints[fingerprint] = struct{}{}
	}
	for fingerprint := range selectedFingerprints {
		descriptorSet := descriptorSets[fingerprint]
		if descriptorSet == nil {
			return nil, ErrEvidenceArchive
		}
		objects[descriptorArchivePath(fingerprint)] = append([]byte(nil), descriptorSet.encoded...)
	}
	var eventLines bytes.Buffer
	for index := range selected {
		stored := selected[index]
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], stored.Event.PayloadSchemaFingerprint)
		publicJSON, err := canonicalStoredEventJSONWithDescriptor(stored, descriptorSets[fingerprint])
		if err != nil {
			return nil, ErrEvidenceArchive
		}
		_, _ = eventLines.Write(publicJSON)
		_ = eventLines.WriteByte('\n')
		prefix := fmt.Sprintf("events/%020d/", stored.Event.Sequence)
		objects[prefix+"event.pb"] = append([]byte(nil), stored.EventProto...)
		objects[prefix+"payload.pb"] = append([]byte(nil), stored.PayloadProto...)
		objects[prefix+"payload.json"] = append([]byte(nil), stored.PayloadJSON...)
		objects[prefix+"payload.type"] = []byte(stored.PayloadType)
	}
	objects["events.jsonl"] = eventLines.Bytes()
	for _, evidence := range input.Evidence {
		if !safeArchivePath(evidence.Path) || reservedEvidencePath(evidence.Path) || len(evidence.Bytes) > maxEvidenceArchiveMember {
			return nil, ErrEvidenceArchive
		}
		if _, duplicate := objects[evidence.Path]; duplicate {
			return nil, ErrEvidenceArchive
		}
		objects[evidence.Path] = append([]byte(nil), evidence.Bytes...)
	}
	if len(objects)+2 > maxEvidenceArchiveMembers {
		return nil, ErrEvidenceArchive
	}
	objectNames := sortedMemberNames(objects)
	manifestObjects := make([]*tammyv1.AuditExportObject, 0, len(objectNames))
	for _, name := range objectNames {
		digest := sha256.Sum256(objects[name])
		manifestObjects = append(manifestObjects, &tammyv1.AuditExportObject{
			Path: name, Sha256: digest[:], ByteLength: uint64(len(objects[name])),
		})
	}
	manifest := &tammyv1.AuditExportManifest{
		Format: evidenceArchiveFormat, WorkspaceId: input.Header.WorkspaceID, Generation: input.Header.Generation,
		StartSequence: 1, EndSequence: input.Header.CurrentSequence,
		ChainSalt: append([]byte(nil), input.Header.ChainSalt...), GenesisHash: append([]byte(nil), input.Header.GenesisHash[:]...),
		VerifiedHead: append([]byte(nil), input.Header.CurrentHead[:]...), SigningKeyId: input.SigningKey.KeyID,
		RootSigningKeyId: signingKeyChain.Keys[0].KeyId, SigningKeyEpoch: input.SigningKey.Epoch,
		CreatedAt: timestamppb.New(input.CreatedAt.UTC()), Objects: manifestObjects,
	}
	manifestJSON, err := canonical.NormalizedJSON(manifest)
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	manifestHash := sha256.Sum256(manifestJSON)
	signature, err := signer(input.SigningKey, manifestHash)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(input.SigningKey.PublicKey, manifestHash[:], signature) {
		return nil, ErrEvidenceArchive
	}
	objects["manifest.json"] = manifestJSON
	objects["signature.ed25519"] = signature
	return writeDeterministicZIP(objects)
}

// VerifyEvidenceArchive validates bounds, canonical bytes, every object hash,
// the Ed25519 signature, exact payload bytes, and the complete event chain.
// It has no database or workspace-key dependency.
func VerifyEvidenceArchive(archive []byte) (*EvidenceVerificationResult, error) {
	members, err := indexStoredEvidenceArchive(archive)
	if err != nil {
		return nil, err
	}
	manifestJSON, manifestExists := members["manifest.json"]
	signature, signatureExists := members["signature.ed25519"]
	if !manifestExists || !signatureExists || len(signature) != ed25519.SignatureSize {
		return nil, ErrEvidenceArchive
	}
	manifest := &tammyv1.AuditExportManifest{}
	if err := canonical.UnmarshalStrict(manifestJSON, manifest); err != nil {
		return nil, ErrEvidenceArchive
	}
	canonicalManifest, err := canonical.NormalizedJSON(manifest)
	if err != nil || !bytes.Equal(canonicalManifest, manifestJSON) || !validEvidenceManifest(manifest) {
		return nil, ErrEvidenceArchive
	}
	if len(manifest.Objects) != len(members)-2 {
		return nil, ErrEvidenceArchive
	}
	previousPath := ""
	for _, object := range manifest.Objects {
		if object == nil || !safeArchivePath(object.Path) || object.Path == "manifest.json" || object.Path == "signature.ed25519" ||
			len(object.Sha256) != sha256.Size || object.Path <= previousPath {
			return nil, ErrEvidenceArchive
		}
		content, exists := members[object.Path]
		if !exists || uint64(len(content)) != object.ByteLength {
			return nil, ErrEvidenceArchive
		}
		digest := sha256.Sum256(content)
		if !bytes.Equal(digest[:], object.Sha256) {
			return nil, ErrEvidenceArchive
		}
		previousPath = object.Path
	}
	publicKey := members["public-key.ed25519"]
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, ErrEvidenceArchive
	}
	for name := range members {
		if strings.HasPrefix(name, "signing-key") && name != signingKeyChainArchivePath {
			return nil, ErrEvidenceArchive
		}
	}
	signingKeyChain, validSigningKeyChain := verifyArchivedSigningKeyChain(members[signingKeyChainArchivePath], manifest, publicKey)
	if !validSigningKeyChain {
		return nil, ErrEvidenceArchive
	}
	keyID, err := SigningKeyID(manifest.WorkspaceId, ed25519.PublicKey(publicKey))
	if err != nil || keyID != manifest.SigningKeyId {
		return nil, ErrEvidenceArchive
	}
	manifestHash := sha256.Sum256(manifestJSON)
	if !ed25519.Verify(ed25519.PublicKey(publicKey), manifestHash[:], signature) {
		return nil, ErrEvidenceArchive
	}
	selected, err := evidenceArchiveSelectionMode(members)
	if err != nil {
		return nil, err
	}
	descriptorSets, err := descriptorRegistryFromArchiveMembers(members)
	if err != nil {
		return nil, err
	}
	var eventContext archiveEventVerification
	if selected {
		chainHeads := members["chain/heads.bin"]
		eventContext.events, err = verifySelectedArchiveEvents(manifest, members, descriptorSets, chainHeads, &eventContext)
	} else {
		eventContext.events, err = verifyArchiveEvents(manifest, members, descriptorSets)
		if err == nil {
			eventContext.openings = newSelectedOpeningRegistry(len(eventContext.events) * 5)
			for _, stored := range eventContext.events {
				if !eventContext.openings.addEvent(stored.Event) {
					err = ErrEvidenceArchive
					break
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if requireExactArchiveDescriptorRegistryWithRotationProofs(eventContext.events, signingKeyChain.EventProofs, descriptorSets) != nil ||
		!verifySigningKeyRotationEventProofs(signingKeyChain, manifest, members, eventContext, descriptorSets) {
		return nil, ErrEvidenceArchive
	}
	return &EvidenceVerificationResult{Manifest: manifest, EventCount: uint64(len(eventContext.events))}, nil
}

type storedZIPMember struct {
	name       string
	dataOffset uint64
	size       uint64
	crc32      uint32
}

// indexStoredEvidenceArchive validates the complete central directory before
// exposing any member bytes. Store members are returned as zero-copy views into
// archive, so callers must keep archive immutable while using the index.
func indexStoredEvidenceArchive(archive []byte) (map[string][]byte, error) {
	if len(archive) == 0 || len(archive) > maxEvidenceArchiveBytes {
		return nil, ErrEvidenceArchive
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > maxEvidenceArchiveMembers {
		return nil, ErrEvidenceArchive
	}
	preflight := make([]storedZIPMember, 0, len(reader.File))
	names := make(map[string]struct{}, len(reader.File))
	var total uint64
	archiveSize := uint64(len(archive))
	for _, file := range reader.File {
		if file == nil || !safeArchivePath(file.Name) || !file.FileInfo().Mode().IsRegular() ||
			file.Flags&0x1 != 0 || file.Method != zip.Store || file.UncompressedSize64 > maxEvidenceArchiveMember ||
			file.CompressedSize64 != file.UncompressedSize64 {
			return nil, ErrEvidenceArchive
		}
		if _, duplicate := names[file.Name]; duplicate {
			return nil, ErrEvidenceArchive
		}
		names[file.Name] = struct{}{}
		if file.UncompressedSize64 > uint64(maxEvidenceArchiveBytes)-total {
			return nil, ErrEvidenceArchive
		}
		total += file.UncompressedSize64
		offset, offsetErr := file.DataOffset()
		if offsetErr != nil || offset < 0 {
			return nil, ErrEvidenceArchive
		}
		dataOffset := uint64(offset)
		if dataOffset > archiveSize || file.UncompressedSize64 > archiveSize-dataOffset {
			return nil, ErrEvidenceArchive
		}
		preflight = append(preflight, storedZIPMember{name: file.Name, dataOffset: dataOffset,
			size: file.UncompressedSize64, crc32: file.CRC32})
	}

	members := make(map[string][]byte, len(preflight))
	for _, member := range preflight {
		content := archive[member.dataOffset : member.dataOffset+member.size]
		if crc32.ChecksumIEEE(content) != member.crc32 {
			return nil, ErrEvidenceArchive
		}
		members[member.name] = content
	}
	return members, nil
}

func attachSigningKeyRotationEventProofs(chain *tammyv1.AuditSigningKeyChain, header ChainHeader,
	events []StoredEvent) error {
	if chain == nil || uint64(len(events)) != header.CurrentSequence {
		return ErrEvidenceArchive
	}
	chain.EventProofs = nil
	for _, link := range chain.Links {
		if link.Generation != header.Generation {
			continue
		}
		if link.PriorSequence == header.CurrentSequence {
			if !bytes.Equal(link.PriorHead, header.CurrentHead[:]) {
				return ErrEvidenceArchive
			}
			continue
		}
		if link.PriorSequence > header.CurrentSequence {
			continue
		}
		var wantPriorHead []byte
		if link.PriorSequence == 0 {
			wantPriorHead = header.GenesisHash[:]
		} else {
			wantPriorHead = events[link.PriorSequence-1].Event.EventHash
		}
		if !bytes.Equal(link.PriorHead, wantPriorHead) {
			return ErrEvidenceArchive
		}
		stored := events[link.PriorSequence]
		if event, ok := rotationEventMatchesLink(link, stored.EventProto, stored.PayloadProto); !ok ||
			!bytes.Equal(event.EventHash, stored.Event.EventHash) {
			return ErrEvidenceArchive
		}
		openings := stored.Event.CommitmentOpenings
		if !validCommitmentOpenings(openings) || len(stored.Event.PayloadSchemaFingerprint) != sha256.Size {
			return ErrEvidenceArchive
		}
		chain.EventProofs = append(chain.EventProofs, &tammyv1.AuditSigningKeyRotationEventProof{
			SuccessorEpoch:          link.SuccessorEpoch,
			SchemaFingerprint:       append([]byte(nil), stored.Event.PayloadSchemaFingerprint...),
			PayloadIdentityBlinding: append([]byte(nil), openings.PayloadIdentityBlinding...),
			EventTypeBlinding:       append([]byte(nil), openings.EventTypeBlinding...),
			OccurredAtBlinding:      append([]byte(nil), openings.OccurredAtBlinding...),
		})
	}
	return nil
}

func buildEventCommitmentProof(events []StoredEvent) ([]byte, error) {
	var proof bytes.Buffer
	for index, stored := range events {
		if stored.Event == nil || stored.Event.Sequence != uint64(index+1) || len(stored.Event.EventHash) != sha256.Size || len(stored.CanonicalEvent) == 0 {
			return nil, ErrEvidenceArchive
		}
		_, _ = proof.Write(stored.CanonicalEvent)
		_ = proof.WriteByte('\n')
	}
	if proof.Len() == 0 || proof.Len() > maxEvidenceArchiveMember {
		return nil, ErrEvidenceArchive
	}
	return proof.Bytes(), nil
}

func buildFilterOpeningProof(events []StoredEvent, filter *tammyv1.AuditEventFilter) ([]byte, error) {
	if filter == nil {
		return nil, ErrEvidenceArchive
	}
	var proof bytes.Buffer
	for index, stored := range events {
		if stored.Event == nil || stored.Event.Sequence != uint64(index+1) || !validCommitmentOpenings(stored.Event.CommitmentOpenings) {
			return nil, ErrEvidenceArchive
		}
		line, err := buildFilterOpeningLine(stored, filter)
		if err != nil {
			return nil, ErrEvidenceArchive
		}
		_, _ = proof.Write(line)
		_ = proof.WriteByte('\n')
	}
	if proof.Len() == 0 || proof.Len() > maxEvidenceArchiveMember {
		return nil, ErrEvidenceArchive
	}
	return proof.Bytes(), nil
}

func buildFilterOpeningLine(stored StoredEvent, filter *tammyv1.AuditEventFilter) ([]byte, error) {
	if stored.Event == nil || filter == nil || !validCommitmentOpenings(stored.Event.CommitmentOpenings) {
		return nil, ErrEvidenceArchive
	}
	fields := map[string]*structpb.Value{
		"sequence": structpb.NewStringValue(strconv.FormatUint(stored.Event.Sequence, 10)),
		"version":  structpb.NewStringValue(filterOpeningVersion),
	}
	if len(filter.EventTypes) != 0 {
		fields["event_type"] = commitmentOpeningValue(strconv.FormatInt(int64(stored.Event.Type), 10),
			stored.Event.CommitmentOpenings.EventTypeBlinding)
	}
	if filter.FromTime != nil || filter.ToTime != nil {
		fields["occurred_at"] = commitmentOpeningValue(stored.Event.OccurredAt.AsTime().UTC().Format(time.RFC3339Nano),
			stored.Event.CommitmentOpenings.OccurredAtBlinding)
	}
	if filter.ActorUserId != nil {
		actorUserID := ""
		if stored.Event.Actor != nil {
			actorUserID = stored.Event.Actor.ActorUserId
		}
		fields["actor_user_id"] = commitmentOpeningValue(actorUserID, stored.Event.CommitmentOpenings.ActorUserIdBlinding)
	}
	line, err := canonical.NormalizedJSON(&structpb.Struct{Fields: fields})
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	return line, nil
}

func commitmentOpeningValue(value string, blinding []byte) *structpb.Value {
	return structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
		"blinding": structpb.NewStringValue(hex.EncodeToString(blinding)),
		"value":    structpb.NewStringValue(value),
	}})
}

func evidenceArchiveSelectionMode(members map[string][]byte) (bool, error) {
	_, hasFilter := members["filter.pb"]
	_, hasHeads := members["chain/heads.bin"]
	_, hasCommitments := members["chain/event-commitments.jsonl"]
	_, hasFilterOpenings := members["chain/filter-openings.jsonl"]
	for name := range members {
		if strings.HasPrefix(name, "chain/") && name != "chain/heads.bin" && name != "chain/event-commitments.jsonl" &&
			name != "chain/filter-openings.jsonl" {
			return false, ErrEvidenceArchive
		}
	}
	if !hasFilter && !hasHeads && !hasCommitments && !hasFilterOpenings {
		return false, nil
	}
	if !hasFilter || !hasHeads || !hasCommitments || !hasFilterOpenings {
		return false, ErrEvidenceArchive
	}
	return true, nil
}

type eventCommitmentProof struct {
	canonical                 []byte
	event                     StoredEvent
	payloadIdentityCommitment string
	eventTypeCommitment       string
	occurredAtCommitment      string
	actorUserIDCommitment     string
}

type archiveEventVerification struct {
	events      []StoredEvent
	commitments []eventCommitmentProof
	openings    *selectedOpeningRegistry
}

type commitmentOpeningOwner struct {
	sequence uint64
	category string
}

type selectedOpeningRegistry struct {
	byBlinding map[[sha256.Size]byte]commitmentOpeningOwner
	byOwner    map[commitmentOpeningOwner][sha256.Size]byte
}

func newSelectedOpeningRegistry(capacity int) *selectedOpeningRegistry {
	return &selectedOpeningRegistry{
		byBlinding: make(map[[sha256.Size]byte]commitmentOpeningOwner, capacity),
		byOwner:    make(map[commitmentOpeningOwner][sha256.Size]byte, capacity),
	}
}

func (registry *selectedOpeningRegistry) add(owner commitmentOpeningOwner, opening []byte) bool {
	if registry == nil || owner.sequence == 0 || owner.category == "" || len(opening) != sha256.Size ||
		bytes.Equal(opening, make([]byte, sha256.Size)) {
		return false
	}
	var key [sha256.Size]byte
	copy(key[:], opening)
	if registered, exists := registry.byOwner[owner]; exists {
		registeredOwner, claimed := registry.byBlinding[key]
		return registered == key && claimed && registeredOwner == owner
	}
	if _, claimed := registry.byBlinding[key]; claimed {
		return false
	}
	registry.byOwner[owner] = key
	registry.byBlinding[key] = owner
	return true
}

func (registry *selectedOpeningRegistry) addEvent(event *tammyv1.AuditEvent) bool {
	if event == nil || !validCommitmentOpenings(event.CommitmentOpenings) {
		return false
	}
	for _, owned := range []struct {
		category string
		opening  []byte
	}{
		{category: "hidden_metadata", opening: event.CommitmentOpenings.HiddenMetadataBlinding},
		{category: "payload_identity", opening: event.CommitmentOpenings.PayloadIdentityBlinding},
		{category: "event_type", opening: event.CommitmentOpenings.EventTypeBlinding},
		{category: "occurred_at", opening: event.CommitmentOpenings.OccurredAtBlinding},
		{category: "actor_user_id", opening: event.CommitmentOpenings.ActorUserIdBlinding},
	} {
		if !registry.add(commitmentOpeningOwner{sequence: event.Sequence, category: owned.category}, owned.opening) {
			return false
		}
	}
	return true
}

func walkExactJSONLLines(encoded []byte, expected uint64, visit func(uint64, []byte) error) error {
	if len(encoded) > maxEvidenceArchiveMember || expected > uint64(maxEvidenceArchiveMember) {
		return ErrEvidenceArchive
	}
	if expected == 0 {
		if len(encoded) != 0 {
			return ErrEvidenceArchive
		}
		return nil
	}
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' || visit == nil {
		return ErrEvidenceArchive
	}

	var count uint64
	lineStart := 0
	for index, value := range encoded {
		if value != '\n' {
			continue
		}
		count++
		if count > expected || index == lineStart {
			return ErrEvidenceArchive
		}
		lineStart = index + 1
	}
	if count != expected || lineStart != len(encoded) {
		return ErrEvidenceArchive
	}

	lineStart = 0
	var index uint64
	for offset, value := range encoded {
		if value != '\n' {
			continue
		}
		if err := visit(index, encoded[lineStart:offset]); err != nil {
			return err
		}
		index++
		lineStart = offset + 1
	}
	return nil
}

func verifyEventCommitmentProof(manifest *tammyv1.AuditExportManifest, members map[string][]byte,
	chainHeads []byte) ([]eventCommitmentProof, error) {
	encoded, exists := members["chain/event-commitments.jsonl"]
	if !exists || len(chainHeads)%sha256.Size != 0 || uint64(len(chainHeads)/sha256.Size) != manifest.EndSequence {
		return nil, ErrEvidenceArchive
	}
	proofs := make([]eventCommitmentProof, 0, min(int(manifest.EndSequence), 1024))
	var previous [sha256.Size]byte
	copy(previous[:], manifest.GenesisHash)
	err := walkExactJSONLLines(encoded, manifest.EndSequence, func(index uint64, line []byte) error {
		sequence := index + 1
		commitments, ok := parseCanonicalEventCommitments(line, manifest.WorkspaceId, manifest.Generation, sequence)
		if !ok {
			return ErrEvidenceArchive
		}
		headOffset := int(index) * sha256.Size
		eventHash, err := EventHash(previous, line)
		if err != nil || !bytes.Equal(eventHash[:], chainHeads[headOffset:headOffset+sha256.Size]) {
			return ErrEvidenceArchive
		}
		event := &tammyv1.AuditEvent{WorkspaceId: manifest.WorkspaceId, Generation: manifest.Generation, Sequence: sequence,
			EventHash: append([]byte(nil), eventHash[:]...)}
		stored := StoredEvent{Event: event}
		proofs = append(proofs, eventCommitmentProof{canonical: line, event: stored,
			payloadIdentityCommitment: commitments.payloadIdentityCommitment,
			eventTypeCommitment:       commitments.eventTypeCommitment, occurredAtCommitment: commitments.occurredAtCommitment,
			actorUserIDCommitment: commitments.actorUserIDCommitment})
		previous = eventHash
		return nil
	})
	if err != nil {
		return nil, err
	}
	return proofs, nil
}

type canonicalEventCommitments struct {
	payloadIdentityCommitment string
	eventTypeCommitment       string
	occurredAtCommitment      string
	actorUserIDCommitment     string
}

func parseCanonicalEventCommitments(encoded []byte, workspaceID string, generation, sequence uint64) (canonicalEventCommitments, bool) {
	if len(encoded) == 0 || len(encoded) > maxEvidenceArchiveMember || workspaceID == "" || generation == 0 || sequence == 0 {
		return canonicalEventCommitments{}, false
	}
	envelope := new(structpb.Struct)
	if err := canonical.UnmarshalStrict(encoded, envelope); err != nil || len(envelope.Fields) != 7 {
		return canonicalEventCommitments{}, false
	}
	canonicalLine, err := canonical.NormalizedJSON(envelope)
	if err != nil || !bytes.Equal(canonicalLine, encoded) {
		return canonicalEventCommitments{}, false
	}
	version, versionOK := projectionString(envelope, "version")
	hiddenCommitment, hiddenOK := projectionString(envelope, "hidden_metadata_commitment")
	payloadCommitment, payloadOK := projectionString(envelope, "payload_identity_commitment")
	eventTypeCommitment, eventTypeOK := projectionString(envelope, "event_type_commitment")
	occurredAtCommitment, occurredAtOK := projectionString(envelope, "occurred_at_commitment")
	actorUserIDCommitment, actorUserIDOK := projectionString(envelope, "actor_user_id_commitment")
	projectionValue, projectionOK := envelope.Fields["identity_projection"]
	projection := projectionValue.GetStructValue()
	if !versionOK || version != canonicalEventVersion || !hiddenOK || !canonicalDigestHex(hiddenCommitment) ||
		!payloadOK || !canonicalDigestHex(payloadCommitment) || !eventTypeOK || !canonicalDigestHex(eventTypeCommitment) ||
		!occurredAtOK || !canonicalDigestHex(occurredAtCommitment) || !actorUserIDOK || !canonicalDigestHex(actorUserIDCommitment) ||
		!projectionOK || projection == nil || len(projection.Fields) != 3 {
		return canonicalEventCommitments{}, false
	}
	generationText, generationOK := projectionString(projection, "generation")
	sequenceText, sequenceOK := projectionString(projection, "sequence")
	projectedWorkspaceID, workspaceOK := projectionString(projection, "workspace_id")
	if !generationOK || !sequenceOK || !workspaceOK || sequenceText != strconv.FormatUint(sequence, 10) ||
		generationText != strconv.FormatUint(generation, 10) || projectedWorkspaceID != workspaceID {
		return canonicalEventCommitments{}, false
	}
	return canonicalEventCommitments{payloadIdentityCommitment: payloadCommitment,
		eventTypeCommitment: eventTypeCommitment, occurredAtCommitment: occurredAtCommitment,
		actorUserIDCommitment: actorUserIDCommitment}, true
}

func verifyFilterOpeningProof(manifest *tammyv1.AuditExportManifest, members map[string][]byte,
	proofs []eventCommitmentProof, filter *tammyv1.AuditEventFilter,
	openingRegistry *selectedOpeningRegistry, selectedSequences []uint64) error {
	encoded, exists := members["chain/filter-openings.jsonl"]
	if !exists || uint64(len(proofs)) != manifest.EndSequence {
		return ErrEvidenceArchive
	}
	revealEventType := len(filter.EventTypes) != 0
	revealOccurredAt := filter.FromTime != nil || filter.ToTime != nil
	revealActorUserID := filter.ActorUserId != nil
	wantFields := 2
	if revealEventType {
		wantFields++
	}
	if revealOccurredAt {
		wantFields++
	}
	if revealActorUserID {
		wantFields++
	}
	selectedIndex := 0
	err := walkExactJSONLLines(encoded, manifest.EndSequence, func(index uint64, line []byte) error {
		openingLine := new(structpb.Struct)
		if err := canonical.UnmarshalStrict(line, openingLine); err != nil || len(openingLine.Fields) != wantFields {
			return ErrEvidenceArchive
		}
		canonicalLine, err := canonical.NormalizedJSON(openingLine)
		if err != nil || !bytes.Equal(canonicalLine, line) {
			return ErrEvidenceArchive
		}
		sequence := index + 1
		sequenceText, sequenceOK := projectionString(openingLine, "sequence")
		version, versionOK := projectionString(openingLine, "version")
		if !sequenceOK || sequenceText != strconv.FormatUint(sequence, 10) || !versionOK || version != filterOpeningVersion {
			return ErrEvidenceArchive
		}
		proof := proofs[int(index)]
		projectedEvent := proto.Clone(proof.event.Event).(*tammyv1.AuditEvent)
		if revealEventType {
			value, blinding, ok := parseFilterOpening(openingLine, "event_type", openingRegistry,
				commitmentOpeningOwner{sequence: sequence, category: "event_type"})
			if !ok || !filterOpeningMatchesCommitment(proof.eventTypeCommitment, eventTypeCommitmentDomain, blinding, value) {
				return ErrEvidenceArchive
			}
			eventTypeValue, err := strconv.ParseInt(value, 10, 32)
			if err != nil || value != strconv.FormatInt(eventTypeValue, 10) {
				return ErrEvidenceArchive
			}
			projectedEvent.Type = tammyv1.AuditEventType(eventTypeValue)
			if projectedEvent.Type == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED {
				return ErrEvidenceArchive
			}
			if _, defined := tammyv1.AuditEventType_name[int32(projectedEvent.Type)]; !defined {
				return ErrEvidenceArchive
			}
		}
		if revealOccurredAt {
			value, blinding, ok := parseFilterOpening(openingLine, "occurred_at", openingRegistry,
				commitmentOpeningOwner{sequence: sequence, category: "occurred_at"})
			if !ok || !filterOpeningMatchesCommitment(proof.occurredAtCommitment, occurredAtCommitmentDomain, blinding, value) {
				return ErrEvidenceArchive
			}
			occurredAt, err := time.Parse(time.RFC3339Nano, value)
			if err != nil || occurredAt.UTC().Format(time.RFC3339Nano) != value {
				return ErrEvidenceArchive
			}
			projectedEvent.OccurredAt = timestamppb.New(occurredAt)
		}
		if revealActorUserID {
			value, blinding, ok := parseFilterOpening(openingLine, "actor_user_id", openingRegistry,
				commitmentOpeningOwner{sequence: sequence, category: "actor_user_id"})
			if !ok || !filterOpeningMatchesCommitment(proof.actorUserIDCommitment, actorUserIDCommitmentDomain, blinding, value) ||
				value != "" && !exportReferencePattern.MatchString(value) {
				return ErrEvidenceArchive
			}
			if value != "" {
				projectedEvent.Actor = &tammyv1.AuthenticationContext{ActorUserId: value}
			}
		}
		if len(filterStoredEvents([]StoredEvent{{Event: projectedEvent}}, filter, 0)) == 1 {
			if selectedIndex >= len(selectedSequences) || selectedSequences[selectedIndex] != sequence {
				return ErrEvidenceArchive
			}
			selectedIndex++
		}
		return nil
	})
	if err != nil || selectedIndex != len(selectedSequences) {
		return ErrEvidenceArchive
	}
	return nil
}

func filterOpeningMatchesCommitment(expected, domain string, blinding []byte, value string) bool {
	commitment := blindedFramedSHA256(domain, blinding, []byte(value))
	return hex.EncodeToString(commitment[:]) == expected
}

func parseFilterOpening(line *structpb.Struct, name string, registry *selectedOpeningRegistry,
	owner commitmentOpeningOwner) (string, []byte, bool) {
	openingValue, exists := line.Fields[name]
	opening := openingValue.GetStructValue()
	if !exists || opening == nil || len(opening.Fields) != 2 {
		return "", nil, false
	}
	value, valueOK := projectionString(opening, "value")
	blindingHex, blindingOK := projectionString(opening, "blinding")
	blinding, err := hex.DecodeString(blindingHex)
	if !valueOK || !blindingOK || err != nil || len(blinding) != sha256.Size || blindingHex != strings.ToLower(blindingHex) ||
		bytes.Equal(blinding, make([]byte, sha256.Size)) {
		return "", nil, false
	}
	return value, blinding, registry.add(owner, blinding)
}

func canonicalDigestHex(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func projectionString(projection *structpb.Struct, name string) (string, bool) {
	value, exists := projection.Fields[name]
	if !exists || value == nil {
		return "", false
	}
	_, isString := value.Kind.(*structpb.Value_StringValue)
	return value.GetStringValue(), isString
}

func verifySelectedArchiveEvents(manifest *tammyv1.AuditExportManifest, members map[string][]byte,
	descriptorSets descriptorRegistry, chainHeads []byte, verification *archiveEventVerification) ([]StoredEvent, error) {
	if verification == nil {
		return nil, ErrEvidenceArchive
	}
	if manifest.StartSequence != 1 || len(chainHeads) < sha256.Size || len(chainHeads)%sha256.Size != 0 ||
		uint64(len(chainHeads)/sha256.Size) != manifest.EndSequence ||
		!bytes.Equal(chainHeads[len(chainHeads)-sha256.Size:], manifest.VerifiedHead) {
		return nil, ErrEvidenceArchive
	}
	filterProto, ok := members["filter.pb"]
	if !ok {
		return nil, ErrEvidenceArchive
	}
	filter := &tammyv1.AuditEventFilter{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(filterProto, filter); err != nil || len(filter.ProtoReflect().GetUnknown()) != 0 {
		return nil, ErrEvidenceArchive
	}
	canonicalFilter, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil || !bytes.Equal(canonicalFilter, filterProto) {
		return nil, ErrEvidenceArchive
	}
	sequences, err := selectedArchiveSequences(members)
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	proofs, err := verifyEventCommitmentProof(manifest, members, chainHeads)
	if err != nil {
		return nil, err
	}
	openingRegistry := newSelectedOpeningRegistry(min(len(proofs)*5, 1024))
	if err := verifyFilterOpeningProof(manifest, members, proofs, filter, openingRegistry, sequences); err != nil {
		return nil, err
	}
	linesBytes, ok := members["events.jsonl"]
	if !ok {
		return nil, ErrEvidenceArchive
	}
	events := make([]StoredEvent, 0, len(sequences))
	lastSequence := uint64(0)
	err = walkExactJSONLLines(linesBytes, uint64(len(sequences)), func(index uint64, line []byte) error {
		sequence := sequences[int(index)]
		prefix := fmt.Sprintf("events/%020d/", sequence)
		eventProto, eventOK := members[prefix+"event.pb"]
		payloadProto, payloadOK := members[prefix+"payload.pb"]
		payloadJSON, payloadJSONOK := members[prefix+"payload.json"]
		payloadType, payloadTypeOK := members[prefix+"payload.type"]
		if !eventOK || !payloadOK || !payloadJSONOK || !payloadTypeOK {
			return ErrEvidenceArchive
		}
		event := &tammyv1.AuditEvent{}
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(eventProto, event); err != nil ||
			event.Sequence != sequence || event.Sequence <= lastSequence || event.Sequence == 0 || event.Sequence > manifest.EndSequence ||
			event.WorkspaceId != manifest.WorkspaceId || event.Generation != manifest.Generation {
			return ErrEvidenceArchive
		}
		lastSequence = event.Sequence
		if !bytes.Equal(event.EventHash, chainHeads[(event.Sequence-1)*sha256.Size:event.Sequence*sha256.Size]) {
			return ErrEvidenceArchive
		}
		if !openingRegistry.addEvent(event) {
			return ErrEvidenceArchive
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], event.PayloadSchemaFingerprint)
		stored := StoredEvent{Event: event, EventProto: eventProto, PayloadProto: payloadProto, PayloadJSON: payloadJSON,
			PayloadType: string(payloadType)}
		if validateStoredEventJSONWithDescriptor(line, stored, descriptorSets[fingerprint]) != nil {
			return ErrEvidenceArchive
		}
		var previous [sha256.Size]byte
		if event.Sequence == 1 {
			copy(previous[:], manifest.GenesisHash)
		} else {
			copy(previous[:], chainHeads[(event.Sequence-2)*sha256.Size:(event.Sequence-1)*sha256.Size])
		}
		prepared, prepareErr := reconstructStoredEventWithValidatedDescriptor(stored, descriptorSets[fingerprint])
		if prepareErr != nil || !bytes.Equal(prepared.Event.PreviousHash, previous[:]) ||
			!bytes.Equal(prepared.CanonicalEvent, proofs[event.Sequence-1].canonical) {
			return ErrEvidenceArchive
		}
		events = append(events, prepared)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if len(filterStoredEvents([]StoredEvent{event}, filter, 0)) != 1 {
			return nil, ErrEvidenceArchive
		}
	}
	verification.commitments = proofs
	verification.openings = openingRegistry
	return events, nil
}

func selectedArchiveSequences(members map[string][]byte) ([]uint64, error) {
	artifacts := make(map[uint64]uint8)
	for name := range members {
		if !strings.HasPrefix(name, "events/") {
			continue
		}
		if len(name) <= len("events/")+20 || name[len("events/")+20] != '/' {
			return nil, ErrEvidenceArchive
		}
		sequence, err := strconv.ParseUint(name[len("events/"):len("events/")+20], 10, 64)
		prefix := fmt.Sprintf("events/%020d/", sequence)
		if err != nil || sequence == 0 || !strings.HasPrefix(name, prefix) {
			return nil, ErrEvidenceArchive
		}
		var bit uint8
		switch strings.TrimPrefix(name, prefix) {
		case "event.pb":
			bit = 1
		case "payload.pb":
			bit = 2
		case "payload.json":
			bit = 4
		case "payload.type":
			bit = 8
		default:
			return nil, ErrEvidenceArchive
		}
		artifacts[sequence] |= bit
	}
	sequences := make([]uint64, 0, len(artifacts))
	for sequence, mask := range artifacts {
		if mask != 15 {
			return nil, ErrEvidenceArchive
		}
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(left, right int) bool { return sequences[left] < sequences[right] })
	return sequences, nil
}

func sameSelectedEvents(left, right []StoredEvent) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index].EventProto, right[index].EventProto) || !bytes.Equal(left[index].PayloadProto, right[index].PayloadProto) {
			return false
		}
	}
	return true
}

func verifyArchiveEvents(manifest *tammyv1.AuditExportManifest, members map[string][]byte,
	descriptorSets descriptorRegistry) ([]StoredEvent, error) {
	linesBytes, exists := members["events.jsonl"]
	if !exists {
		return nil, ErrEvidenceArchive
	}
	expectedCount := manifest.EndSequence - manifest.StartSequence + 1
	if manifest.StartSequence != 1 {
		return nil, ErrEvidenceArchive
	}
	artifactSequences, err := selectedArchiveSequences(members)
	if err != nil || uint64(len(artifactSequences)) != expectedCount {
		return nil, ErrEvidenceArchive
	}
	for index, sequence := range artifactSequences {
		if sequence != uint64(index+1) {
			return nil, ErrEvidenceArchive
		}
	}
	events := make([]StoredEvent, 0, len(artifactSequences))
	previous := [sha256.Size]byte{}
	copy(previous[:], manifest.GenesisHash)
	err = walkExactJSONLLines(linesBytes, expectedCount, func(index uint64, line []byte) error {
		sequence := index + 1
		prefix := fmt.Sprintf("events/%020d/", sequence)
		eventProto, eventOK := members[prefix+"event.pb"]
		payloadProto, payloadOK := members[prefix+"payload.pb"]
		payloadJSON, payloadJSONOK := members[prefix+"payload.json"]
		payloadType, payloadTypeOK := members[prefix+"payload.type"]
		if !eventOK || !payloadOK || !payloadJSONOK || !payloadTypeOK {
			return ErrEvidenceArchive
		}
		event := &tammyv1.AuditEvent{}
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(eventProto, event); err != nil || event.Sequence != sequence ||
			event.WorkspaceId != manifest.WorkspaceId || event.Generation != manifest.Generation {
			return ErrEvidenceArchive
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], event.PayloadSchemaFingerprint)
		stored := StoredEvent{Event: event, EventProto: eventProto, PayloadProto: payloadProto, PayloadJSON: payloadJSON,
			PayloadType: string(payloadType)}
		if validateStoredEventJSONWithDescriptor(line, stored, descriptorSets[fingerprint]) != nil {
			return ErrEvidenceArchive
		}
		prepared, err := reconstructStoredEventWithValidatedDescriptor(stored, descriptorSets[fingerprint])
		if err != nil || !bytes.Equal(event.PreviousHash, previous[:]) {
			return ErrEvidenceArchive
		}
		events = append(events, prepared)
		copy(previous[:], prepared.Event.EventHash)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var genesis, head [sha256.Size]byte
	copy(genesis[:], manifest.GenesisHash)
	copy(head[:], manifest.VerifiedHead)
	header := ChainHeader{WorkspaceID: manifest.WorkspaceId, Generation: manifest.Generation,
		ChainSalt: manifest.ChainSalt, GenesisHash: genesis, CurrentSequence: manifest.EndSequence, CurrentHead: head}
	if !verifyStoredChainWithDescriptorRegistry(header, events, descriptorSets) {
		return nil, ErrEvidenceArchive
	}
	return events, nil
}

func validateEvidenceInput(input EvidenceArchiveInput) error {
	if input.Header.WorkspaceID == "" || input.Header.Generation == 0 || input.Header.CurrentSequence == 0 ||
		len(input.Events) != int(input.Header.CurrentSequence) || len(input.DescriptorSets) == 0 && len(input.descriptorRegistry) == 0 || input.CreatedAt.IsZero() ||
		!validSigningKeyRecord(input.SigningKey) || input.SigningKey.WorkspaceID != input.Header.WorkspaceID {
		return ErrEvidenceArchive
	}
	if input.SelectionApplied {
		filter := &tammyv1.AuditEventFilter{}
		if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(input.FilterProto, filter) != nil ||
			len(filter.ProtoReflect().GetUnknown()) != 0 || !sameSelectedEvents(input.SelectedEvents, filterStoredEvents(input.Events, filter, 0)) {
			return ErrEvidenceArchive
		}
		canonicalFilter, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
		if err != nil || !bytes.Equal(canonicalFilter, input.FilterProto) {
			return ErrEvidenceArchive
		}
	}
	return nil
}

func validEvidenceManifest(manifest *tammyv1.AuditExportManifest) bool {
	if manifest == nil || manifest.Format != evidenceArchiveFormat || manifest.WorkspaceId == "" || manifest.Generation == 0 ||
		manifest.StartSequence == 0 || manifest.EndSequence < manifest.StartSequence || len(manifest.ChainSalt) != sha256.Size ||
		len(manifest.GenesisHash) != sha256.Size || len(manifest.VerifiedHead) != sha256.Size || manifest.SigningKeyId == "" ||
		manifest.RootSigningKeyId == "" || manifest.SigningKeyEpoch == 0 ||
		manifest.CreatedAt == nil || !manifest.CreatedAt.IsValid() || len(manifest.Objects) == 0 || len(manifest.Objects)+2 > maxEvidenceArchiveMembers {
		return false
	}
	genesis, err := Genesis(manifest.WorkspaceId, manifest.ChainSalt)
	return err == nil && bytes.Equal(genesis[:], manifest.GenesisHash)
}

func verifyArchivedSigningKeyChain(encoded []byte, manifest *tammyv1.AuditExportManifest,
	activePublicKey []byte) (*tammyv1.AuditSigningKeyChain, bool) {
	if len(encoded) == 0 || len(encoded) > maxEvidenceArchiveMember || manifest == nil ||
		len(activePublicKey) != ed25519.PublicKeySize {
		return nil, false
	}
	chain := new(tammyv1.AuditSigningKeyChain)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(encoded, chain); err != nil ||
		messageHasUnknown(chain.ProtoReflect()) || !verifyPublicSigningKeyChain(chain) {
		return nil, false
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(chain)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return nil, false
	}
	root, terminal := chain.Keys[0], chain.Keys[len(chain.Keys)-1]
	valid := root.WorkspaceId == manifest.WorkspaceId && root.KeyId == manifest.RootSigningKeyId &&
		terminal.WorkspaceId == manifest.WorkspaceId && terminal.KeyId == manifest.SigningKeyId &&
		terminal.Epoch == manifest.SigningKeyEpoch && bytes.Equal(terminal.PublicKey, activePublicKey)
	return chain, valid
}

func requireExactArchiveDescriptorRegistryWithRotationProofs(events []StoredEvent,
	proofs []*tammyv1.AuditSigningKeyRotationEventProof, descriptorSets descriptorRegistry) error {
	referenced, err := referencedDescriptorFingerprints(events)
	if err != nil {
		return ErrEvidenceArchive
	}
	for _, proof := range proofs {
		if proof == nil || len(proof.SchemaFingerprint) != sha256.Size {
			return ErrEvidenceArchive
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], proof.SchemaFingerprint)
		referenced[fingerprint] = struct{}{}
	}
	if len(referenced) != len(descriptorSets) {
		return ErrEvidenceArchive
	}
	for fingerprint := range referenced {
		if descriptorSets[fingerprint] == nil {
			return ErrEvidenceArchive
		}
	}
	return nil
}

func verifySigningKeyRotationEventProofs(chain *tammyv1.AuditSigningKeyChain, manifest *tammyv1.AuditExportManifest,
	members map[string][]byte, context archiveEventVerification, descriptorSets descriptorRegistry) bool {
	if chain == nil || manifest == nil || context.openings == nil {
		return false
	}
	var heads []byte
	if manifest.EndSequence > uint64(maxEvidenceArchiveMember/sha256.Size) {
		return false
	}
	wantHeadsLength := int(manifest.EndSequence) * sha256.Size
	if encoded, selected := members["chain/heads.bin"]; selected {
		if len(encoded) != wantHeadsLength || len(context.commitments) != int(manifest.EndSequence) {
			return false
		}
		heads = encoded
	} else {
		if uint64(len(context.events)) != manifest.EndSequence || len(context.commitments) != 0 {
			return false
		}
		heads = make([]byte, 0, len(context.events)*sha256.Size)
		for _, stored := range context.events {
			if stored.Event == nil {
				return false
			}
			heads = append(heads, stored.Event.EventHash...)
		}
	}
	expectedLinks := make([]*tammyv1.AuditSigningKeyRotationLink, 0, len(chain.Links))
	for _, link := range chain.Links {
		if link.Generation != manifest.Generation || link.PriorSequence > manifest.EndSequence {
			continue
		}
		if link.PriorSequence == manifest.EndSequence {
			if !bytes.Equal(link.PriorHead, manifest.VerifiedHead) {
				return false
			}
			continue
		}
		var priorHead []byte
		if link.PriorSequence == 0 {
			priorHead = manifest.GenesisHash
		} else {
			offset := int(link.PriorSequence-1) * sha256.Size
			priorHead = heads[offset : offset+sha256.Size]
		}
		if !bytes.Equal(link.PriorHead, priorHead) {
			return false
		}
		expectedLinks = append(expectedLinks, link)
	}
	if len(chain.EventProofs) != len(expectedLinks) {
		return false
	}
	var aggregate uint64
	for index, proof := range chain.EventProofs {
		link := expectedLinks[index]
		if proof == nil || proof.SuccessorEpoch != link.SuccessorEpoch || len(proof.SchemaFingerprint) != sha256.Size ||
			len(proof.PayloadIdentityBlinding) != sha256.Size || len(proof.EventTypeBlinding) != sha256.Size ||
			len(proof.OccurredAtBlinding) != sha256.Size {
			return false
		}
		var within bool
		aggregate, within = checkedAggregateBytes(aggregate, uint64(len(proof.SchemaFingerprint)+len(proof.PayloadIdentityBlinding)+
			len(proof.EventTypeBlinding)+len(proof.OccurredAtBlinding)), maxEvidenceArchiveMember)
		if !within {
			return false
		}
		sequence := link.PriorSequence + 1
		if sequence == 0 || sequence > manifest.EndSequence {
			return false
		}
		var canonicalEvent []byte
		var commitments canonicalEventCommitments
		if len(context.commitments) != 0 {
			commitmentProof := context.commitments[sequence-1]
			canonicalEvent = commitmentProof.canonical
			commitments = canonicalEventCommitments{
				payloadIdentityCommitment: commitmentProof.payloadIdentityCommitment,
				eventTypeCommitment:       commitmentProof.eventTypeCommitment,
				occurredAtCommitment:      commitmentProof.occurredAtCommitment,
				actorUserIDCommitment:     commitmentProof.actorUserIDCommitment,
			}
		} else {
			stored := context.events[sequence-1]
			canonicalEvent = stored.CanonicalEvent
			var ok bool
			commitments, ok = parseCanonicalEventCommitments(canonicalEvent, link.WorkspaceId, link.Generation, sequence)
			if !ok {
				return false
			}
		}
		eventHash, err := EventHash(bytesToDigest(link.PriorHead), canonicalEvent)
		if err != nil {
			return false
		}
		offset := int(sequence-1) * sha256.Size
		if !bytes.Equal(eventHash[:], heads[offset:offset+sha256.Size]) {
			return false
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], proof.SchemaFingerprint)
		payloadProto, payloadJSON, ok := deterministicSigningKeyRotationPayload(link, descriptorSets[fingerprint])
		if !ok {
			return false
		}
		payloadCommitment := blindedFramedSHA256(payloadIdentityCommitmentDomain, proof.PayloadIdentityBlinding,
			[]byte(protobufTypeURLPrefix+"tammy.v1.SigningKeyRotatedEvent"), proof.SchemaFingerprint, payloadProto, payloadJSON)
		eventTypeCommitment := blindedFramedSHA256(eventTypeCommitmentDomain, proof.EventTypeBlinding,
			[]byte(strconv.FormatInt(int64(tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED), 10)))
		occurredAtCommitment := blindedFramedSHA256(occurredAtCommitmentDomain, proof.OccurredAtBlinding,
			[]byte(link.RotatedAt.AsTime().UTC().Format(time.RFC3339Nano)))
		if hex.EncodeToString(payloadCommitment[:]) != commitments.payloadIdentityCommitment ||
			hex.EncodeToString(eventTypeCommitment[:]) != commitments.eventTypeCommitment ||
			hex.EncodeToString(occurredAtCommitment[:]) != commitments.occurredAtCommitment {
			return false
		}
		for _, opening := range []struct {
			category string
			value    []byte
		}{
			{category: "payload_identity", value: proof.PayloadIdentityBlinding},
			{category: "event_type", value: proof.EventTypeBlinding},
			{category: "occurred_at", value: proof.OccurredAtBlinding},
		} {
			if !context.openings.add(commitmentOpeningOwner{sequence: sequence, category: opening.category}, opening.value) {
				return false
			}
		}
	}
	return true
}

func bytesToDigest(encoded []byte) [sha256.Size]byte {
	var digest [sha256.Size]byte
	copy(digest[:], encoded)
	return digest
}

func deterministicSigningKeyRotationPayload(link *tammyv1.AuditSigningKeyRotationLink,
	descriptorSet *validatedDescriptorSet) ([]byte, []byte, bool) {
	if !verifySigningKeyRotationLink(link) || descriptorSet == nil || descriptorSet.files == nil {
		return nil, nil, false
	}
	linkDigest, err := signedSigningKeyRotationLinkDigest(link)
	if err != nil {
		return nil, nil, false
	}
	payload := &tammyv1.SigningKeyRotatedEvent{
		WorkspaceId: link.WorkspaceId, Generation: link.Generation, SuccessorEpoch: link.SuccessorEpoch,
		PredecessorKeyId: link.PredecessorKeyId, SuccessorKeyId: link.SuccessorKeyId,
		RotationLinkSha256: linkDigest[:],
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return nil, nil, false
	}
	payloadJSON, err := canonical.NormalizedJSON(payload)
	if err != nil {
		return nil, nil, false
	}
	descriptor, err := descriptorSet.files.FindDescriptorByName(protoreflect.FullName("tammy.v1.SigningKeyRotatedEvent"))
	messageDescriptor, ok := descriptor.(protoreflect.MessageDescriptor)
	if err != nil || !ok || messageDescriptor.IsMapEntry() || payloadDescriptorContainsForbiddenSecret(messageDescriptor) {
		return nil, nil, false
	}
	dynamicPayload := dynamicpb.NewMessage(messageDescriptor)
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payloadProto, dynamicPayload) != nil ||
		messageHasUnknown(dynamicPayload.ProtoReflect()) {
		return nil, nil, false
	}
	dynamicProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(dynamicPayload)
	if err != nil || !bytes.Equal(dynamicProto, payloadProto) {
		return nil, nil, false
	}
	dynamicJSON, err := canonical.NormalizedJSON(dynamicPayload)
	if err != nil || !bytes.Equal(dynamicJSON, payloadJSON) {
		return nil, nil, false
	}
	return payloadProto, payloadJSON, true
}

func safeArchivePath(name string) bool {
	return name != "" && len(name) <= maxEvidenceArchivePathBytes && archivePathAlphabet(name) && !strings.Contains(name, "\\") &&
		!strings.HasPrefix(name, "/") && name != ".." && !strings.HasPrefix(name, "../") &&
		path.Clean(name) == name && name != "." && !strings.HasSuffix(name, "/")
}

func archivePathAlphabet(name string) bool {
	for index := range len(name) {
		character := name[index]
		alphanumeric := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9'
		if !alphanumeric && (index == 0 || character != '.' && character != '_' && character != '/' && character != '-') {
			return false
		}
	}
	return true
}

func reservedEvidencePath(name string) bool {
	return name == "manifest.json" || name == "signature.ed25519" || name == "descriptors.pb" ||
		name == "events.jsonl" || name == "filter.pb" || name == "chain/heads.bin" || name == "public-key.ed25519" ||
		name == signingKeyChainArchivePath || strings.HasPrefix(name, "signing-key") ||
		strings.HasPrefix(name, "events/") || strings.HasPrefix(name, "chain/") || strings.HasPrefix(name, descriptorArchivePrefix)
}

func sortedMemberNames(members map[string][]byte) []string {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func writeDeterministicZIP(members map[string][]byte) ([]byte, error) {
	names := sortedMemberNames(members)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	fixedTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Store, CreatorVersion: 20, ReaderVersion: 20}
		header.SetModTime(fixedTime)
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, ErrEvidenceArchive
		}
		if _, err := entry.Write(members[name]); err != nil {
			_ = writer.Close()
			return nil, ErrEvidenceArchive
		}
	}
	if err := writer.Close(); err != nil || output.Len() > maxEvidenceArchiveBytes {
		return nil, ErrEvidenceArchive
	}
	return output.Bytes(), nil
}
