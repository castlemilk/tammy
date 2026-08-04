package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

const descriptorArchivePrefix = "descriptors/"

var ErrDescriptorSetRegistry = errors.New("audit: descriptor set registry failure")

type validatedDescriptorSet struct {
	fingerprint [sha256.Size]byte
	encoded     []byte
	files       *protoregistry.Files
}

type descriptorRegistry map[[sha256.Size]byte]*validatedDescriptorSet

func checkedAggregateBytes(current, next, limit uint64) (uint64, bool) {
	if current > limit || next > limit-current {
		return 0, false
	}
	return current + next, true
}

func newValidatedDescriptorSet(encoded []byte) (*validatedDescriptorSet, error) {
	files, err := validateDescriptorSet(encoded)
	if err != nil {
		return nil, err
	}
	return &validatedDescriptorSet{fingerprint: sha256.Sum256(encoded), encoded: append([]byte(nil), encoded...), files: files}, nil
}

// PersistDescriptorSet validates and appends one immutable canonical descriptor
// set through the caller-owned transaction.
func PersistDescriptorSet(ctx context.Context, executor Executor, descriptorSet []byte,
	createdAt time.Time) ([sha256.Size]byte, error) {
	if executor == nil || !validAuditTimestamp(createdAt) {
		return [sha256.Size]byte{}, ErrDescriptorSetRegistry
	}
	if _, err := validateDescriptorSet(descriptorSet); err != nil {
		return [sha256.Size]byte{}, ErrDescriptorSetRegistry
	}
	fingerprint := sha256.Sum256(descriptorSet)
	if _, err := executor.ExecContext(ctx, `INSERT INTO audit_descriptor_sets_v1(
		fingerprint, descriptor_set, created_at
	) VALUES (?, ?, ?)`, fingerprint[:], descriptorSet, formatTimestamp(createdAt)); err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("%w: persist descriptor set", ErrDescriptorSetRegistry)
	}
	return fingerprint, nil
}

// LoadDescriptorSet returns a validated copy of the exact canonical bytes for
// one schema fingerprint.
func LoadDescriptorSet(ctx context.Context, executor Executor,
	fingerprint [sha256.Size]byte) ([]byte, error) {
	descriptorSet, err := loadRawDescriptorSet(ctx, executor, fingerprint)
	if err != nil {
		return nil, err
	}
	if _, err := validateDescriptorSet(descriptorSet); err != nil {
		return nil, ErrDescriptorSetRegistry
	}
	return descriptorSet, nil
}

func loadRawDescriptorSet(ctx context.Context, executor Executor,
	fingerprint [sha256.Size]byte) ([]byte, error) {
	if executor == nil {
		return nil, ErrDescriptorSetRegistry
	}
	rows, err := executor.QueryContext(ctx, `SELECT length(descriptor_set), created_at
		FROM audit_descriptor_sets_v1
		WHERE fingerprint = ? AND length(CAST(created_at AS BLOB)) = ?`, fingerprint[:], len(timestampLayout))
	if err != nil {
		return nil, fmt.Errorf("%w: preflight descriptor set", ErrDescriptorSetRegistry)
	}
	if !rows.Next() {
		_ = rows.Close()
		return nil, ErrDescriptorSetRegistry
	}
	var descriptorLength int64
	var createdAt string
	if err := rows.Scan(&descriptorLength, &createdAt); err != nil {
		_ = rows.Close()
		return nil, ErrDescriptorSetRegistry
	}
	if rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return nil, ErrDescriptorSetRegistry
	}
	if err := rows.Close(); err != nil {
		return nil, ErrDescriptorSetRegistry
	}
	parsedCreatedAt, err := time.Parse(timestampLayout, createdAt)
	if err != nil || descriptorLength <= 0 || descriptorLength > maxEvidenceArchiveMember ||
		!validAuditTimestamp(parsedCreatedAt) || formatTimestamp(parsedCreatedAt) != createdAt {
		return nil, ErrDescriptorSetRegistry
	}

	rows, err = executor.QueryContext(ctx, `SELECT descriptor_set, created_at
		FROM audit_descriptor_sets_v1
		WHERE fingerprint = ? AND length(descriptor_set) = ? AND created_at = ?`, fingerprint[:], descriptorLength, createdAt)
	if err != nil {
		return nil, fmt.Errorf("%w: load descriptor set", ErrDescriptorSetRegistry)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrDescriptorSetRegistry
	}
	var descriptorSet []byte
	var selectedCreatedAt string
	if err := rows.Scan(&descriptorSet, &selectedCreatedAt); err != nil || rows.Next() || rows.Err() != nil ||
		int64(len(descriptorSet)) != descriptorLength || selectedCreatedAt != createdAt || sha256.Sum256(descriptorSet) != fingerprint {
		return nil, ErrDescriptorSetRegistry
	}
	return descriptorSet, nil
}

func loadValidatedDescriptorRegistry(ctx context.Context, executor Executor,
	fingerprints map[[sha256.Size]byte]struct{}) (descriptorRegistry, error) {
	if len(fingerprints) == 0 || len(fingerprints) > maxEvidenceArchiveMembers {
		return nil, ErrDescriptorSetRegistry
	}
	sets := make(descriptorRegistry, len(fingerprints))
	var aggregate uint64
	for fingerprint := range fingerprints {
		encoded, err := loadRawDescriptorSet(ctx, executor, fingerprint)
		if err != nil {
			return nil, err
		}
		var withinBudget bool
		aggregate, withinBudget = checkedAggregateBytes(aggregate, uint64(len(encoded)), maxEvidenceDescriptorBytes)
		if !withinBudget {
			return nil, ErrDescriptorSetRegistry
		}
		descriptorSet, err := newValidatedDescriptorSet(encoded)
		if err != nil || descriptorSet.fingerprint != fingerprint {
			return nil, ErrDescriptorSetRegistry
		}
		sets[fingerprint] = descriptorSet
	}
	return sets, nil
}

func validateDescriptorSet(encoded []byte) (*protoregistry.Files, error) {
	if len(encoded) == 0 || len(encoded) > maxEvidenceArchiveMember {
		return nil, ErrEvidenceArchive
	}
	set := new(descriptorpb.FileDescriptorSet)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(encoded, set); err != nil ||
		len(set.File) == 0 || messageHasUnknown(set.ProtoReflect()) {
		return nil, ErrEvidenceArchive
	}
	canonicalBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
	if err != nil || !bytes.Equal(canonicalBytes, encoded) {
		return nil, ErrEvidenceArchive
	}
	previous := ""
	for _, file := range set.File {
		if file == nil || file.GetName() == "" || file.GetName() <= previous {
			return nil, ErrEvidenceArchive
		}
		previous = file.GetName()
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	return files, nil
}

func messageHasUnknown(message protoreflect.Message) bool {
	if len(message.GetUnknown()) != 0 {
		return true
	}
	hasUnknown := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			if field.MapValue().Kind() != protoreflect.MessageKind && field.MapValue().Kind() != protoreflect.GroupKind {
				return true
			}
			value.Map().Range(func(_ protoreflect.MapKey, entry protoreflect.Value) bool {
				hasUnknown = messageHasUnknown(entry.Message())
				return !hasUnknown
			})
			return !hasUnknown
		}
		if field.IsList() {
			if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
				return true
			}
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				if messageHasUnknown(list.Get(index).Message()) {
					hasUnknown = true
					return false
				}
			}
			return true
		}
		if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
			hasUnknown = messageHasUnknown(value.Message())
		}
		return !hasUnknown
	})
	return hasUnknown
}

func descriptorArchivePath(fingerprint [sha256.Size]byte) string {
	return descriptorArchivePrefix + hex.EncodeToString(fingerprint[:]) + ".pb"
}

func descriptorSetsFromArchiveMembers(members map[string][]byte) (map[[sha256.Size]byte][]byte, error) {
	registry, err := descriptorRegistryFromArchiveMembers(members)
	if err != nil {
		return nil, err
	}
	sets := make(map[[sha256.Size]byte][]byte, len(registry))
	for fingerprint, descriptorSet := range registry {
		sets[fingerprint] = append([]byte(nil), descriptorSet.encoded...)
	}
	return sets, nil
}

func descriptorRegistryFromArchiveMembers(members map[string][]byte) (descriptorRegistry, error) {
	if _, legacy := members["descriptors.pb"]; legacy {
		return nil, ErrEvidenceArchive
	}
	registry := make(descriptorRegistry)
	var aggregate uint64
	for name, encoded := range members {
		if !strings.HasPrefix(name, descriptorArchivePrefix) {
			continue
		}
		fingerprintHex := strings.TrimSuffix(strings.TrimPrefix(name, descriptorArchivePrefix), ".pb")
		if len(name) != len(descriptorArchivePrefix)+sha256.Size*2+len(".pb") ||
			!strings.HasSuffix(name, ".pb") || fingerprintHex != strings.ToLower(fingerprintHex) {
			return nil, ErrEvidenceArchive
		}
		decoded, err := hex.DecodeString(fingerprintHex)
		if err != nil || len(decoded) != sha256.Size {
			return nil, ErrEvidenceArchive
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], decoded)
		if digest := sha256.Sum256(encoded); digest != fingerprint {
			return nil, ErrEvidenceArchive
		}
		var withinBudget bool
		aggregate, withinBudget = checkedAggregateBytes(aggregate, uint64(len(encoded)), maxEvidenceDescriptorBytes)
		if !withinBudget {
			return nil, ErrEvidenceArchive
		}
		descriptorSet, err := newValidatedDescriptorSet(encoded)
		if err != nil {
			return nil, err
		}
		if descriptorSet.fingerprint != fingerprint {
			return nil, ErrEvidenceArchive
		}
		if _, duplicate := registry[fingerprint]; duplicate {
			return nil, ErrEvidenceArchive
		}
		registry[fingerprint] = descriptorSet
	}
	return registry, nil
}

func canonicalStoredEventJSON(stored StoredEvent, descriptorSet []byte) ([]byte, error) {
	validated, err := newValidatedDescriptorSet(descriptorSet)
	if err != nil {
		return nil, err
	}
	return canonicalStoredEventJSONWithDescriptor(stored, validated)
}

func canonicalStoredEventJSONWithDescriptor(stored StoredEvent, descriptorSet *validatedDescriptorSet) ([]byte, error) {
	if stored.Event == nil || len(stored.EventProto) == 0 {
		return nil, ErrEvidenceArchive
	}
	if descriptorSet == nil || descriptorSet.files == nil {
		return nil, ErrEvidenceArchive
	}
	descriptor, err := descriptorSet.files.FindDescriptorByName("tammy.v1.AuditEvent")
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	messageDescriptor, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, ErrEvidenceArchive
	}
	dynamicEvent := dynamicpb.NewMessage(messageDescriptor)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(stored.EventProto, dynamicEvent); err != nil ||
		messageHasUnknown(dynamicEvent.ProtoReflect()) {
		return nil, ErrEvidenceArchive
	}
	canonicalWire, err := proto.MarshalOptions{Deterministic: true}.Marshal(dynamicEvent)
	if err != nil || !bytes.Equal(canonicalWire, stored.EventProto) {
		return nil, ErrEvidenceArchive
	}
	publicJSON, err := canonical.NormalizedJSON(dynamicEvent)
	if err != nil {
		return nil, ErrEvidenceArchive
	}
	return publicJSON, nil
}

func validateStoredEventJSON(publicJSON []byte, stored StoredEvent, descriptorSet []byte) error {
	validated, err := newValidatedDescriptorSet(descriptorSet)
	if err != nil {
		return err
	}
	return validateStoredEventJSONWithDescriptor(publicJSON, stored, validated)
}

func validateStoredEventJSONWithDescriptor(publicJSON []byte, stored StoredEvent, descriptorSet *validatedDescriptorSet) error {
	want, err := canonicalStoredEventJSONWithDescriptor(stored, descriptorSet)
	if err != nil || !bytes.Equal(want, publicJSON) {
		return ErrEvidenceArchive
	}
	descriptor, err := descriptorSet.files.FindDescriptorByName("tammy.v1.AuditEvent")
	if err != nil {
		return ErrEvidenceArchive
	}
	decoded := dynamicpb.NewMessage(descriptor.(protoreflect.MessageDescriptor))
	if err := canonical.UnmarshalStrict(publicJSON, decoded); err != nil {
		return ErrEvidenceArchive
	}
	canonicalJSON, err := canonical.NormalizedJSON(decoded)
	if err != nil || !bytes.Equal(canonicalJSON, publicJSON) {
		return ErrEvidenceArchive
	}
	return nil
}

func validateStoredPayloadWithDescriptor(stored StoredEvent, descriptorSet []byte) error {
	_, err := reconstructStoredEventWithDescriptor(stored, descriptorSet)
	return err
}

func reconstructStoredEventWithDescriptor(stored StoredEvent, descriptorSet []byte) (StoredEvent, error) {
	validated, err := newValidatedDescriptorSet(descriptorSet)
	if err != nil {
		return StoredEvent{}, err
	}
	return reconstructStoredEventWithValidatedDescriptor(stored, validated)
}

func reconstructStoredEventWithValidatedDescriptor(stored StoredEvent,
	descriptorSet *validatedDescriptorSet) (StoredEvent, error) {
	if stored.Event == nil || stored.PayloadType == "" || len(stored.Event.PayloadSchemaFingerprint) != sha256.Size {
		return StoredEvent{}, ErrEvidenceArchive
	}
	if descriptorSet == nil || descriptorSet.files == nil || !bytes.Equal(descriptorSet.fingerprint[:], stored.Event.PayloadSchemaFingerprint) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	descriptor, err := descriptorSet.files.FindDescriptorByName(protoreflect.FullName(stored.PayloadType))
	if err != nil {
		return StoredEvent{}, ErrEvidenceArchive
	}
	messageDescriptor, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok || messageDescriptor.IsMapEntry() || payloadDescriptorContainsForbiddenSecret(messageDescriptor) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	dynamicPayload := dynamicpb.NewMessage(messageDescriptor)
	if len(stored.PayloadProto) == 0 || (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(stored.PayloadProto, dynamicPayload) != nil ||
		messageHasUnknown(dynamicPayload.ProtoReflect()) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	canonicalWire, err := proto.MarshalOptions{Deterministic: true}.Marshal(dynamicPayload)
	if err != nil || !bytes.Equal(canonicalWire, stored.PayloadProto) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	payloadJSON, err := canonical.NormalizedJSON(dynamicPayload)
	if err != nil || !bytes.Equal(payloadJSON, stored.PayloadJSON) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	boundPayload, payloadType, err := selectedPayload(stored.Event.Type, stored.Event.Payload)
	if err != nil || payloadType != stored.PayloadType {
		return StoredEvent{}, ErrEvidenceArchive
	}
	boundWire, err := proto.MarshalOptions{Deterministic: true}.Marshal(boundPayload)
	if err != nil || !bytes.Equal(boundWire, stored.PayloadProto) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	eventProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(stored.Event)
	if err != nil || len(stored.EventProto) != 0 && !bytes.Equal(eventProto, stored.EventProto) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	chainView := proto.Clone(stored.Event).(*tammyv1.AuditEvent)
	chainView.PreviousHash = nil
	chainView.EventHash = nil
	chainView.Payload = nil
	chainView.PayloadSchemaFingerprint = nil
	canonicalEvent, err := canonicalEventEnvelope(chainView, stored.PayloadProto, payloadJSON, stored.PayloadType, descriptorSet.fingerprint[:])
	if err != nil || len(stored.CanonicalEvent) != 0 && !bytes.Equal(canonicalEvent, stored.CanonicalEvent) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	var previous [sha256.Size]byte
	if len(stored.Event.PreviousHash) != sha256.Size {
		return StoredEvent{}, ErrEvidenceArchive
	}
	copy(previous[:], stored.Event.PreviousHash)
	eventHash, err := EventHash(previous, canonicalEvent)
	if err != nil || !bytes.Equal(eventHash[:], stored.Event.EventHash) {
		return StoredEvent{}, ErrEvidenceArchive
	}
	stored.PayloadProto = append([]byte(nil), canonicalWire...)
	stored.PayloadJSON = append([]byte(nil), payloadJSON...)
	stored.CanonicalEvent = canonicalEvent
	stored.EventProto = eventProto
	return stored, nil
}

func referencedDescriptorFingerprints(events []StoredEvent) (map[[sha256.Size]byte]struct{}, error) {
	referenced := make(map[[sha256.Size]byte]struct{})
	for _, stored := range events {
		if stored.Event == nil || len(stored.Event.PayloadSchemaFingerprint) != sha256.Size {
			return nil, ErrEvidenceArchive
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], stored.Event.PayloadSchemaFingerprint)
		referenced[fingerprint] = struct{}{}
	}
	return referenced, nil
}

func validatedDescriptorSets(events []StoredEvent, supplied map[[sha256.Size]byte][]byte) (map[[sha256.Size]byte][]byte, error) {
	registry, err := validatedDescriptorRegistry(events, supplied)
	if err != nil {
		return nil, err
	}
	validated := make(map[[sha256.Size]byte][]byte, len(registry))
	for fingerprint, descriptorSet := range registry {
		validated[fingerprint] = append([]byte(nil), descriptorSet.encoded...)
	}
	return validated, nil
}

func validatedDescriptorRegistry(events []StoredEvent, supplied map[[sha256.Size]byte][]byte) (descriptorRegistry, error) {
	referenced, err := referencedDescriptorFingerprints(events)
	if err != nil || len(supplied) != len(referenced) {
		return nil, ErrEvidenceArchive
	}
	validated := make(descriptorRegistry, len(supplied))
	var aggregate uint64
	for fingerprint := range referenced {
		encoded, exists := supplied[fingerprint]
		if !exists || sha256.Sum256(encoded) != fingerprint {
			return nil, ErrEvidenceArchive
		}
		var withinBudget bool
		aggregate, withinBudget = checkedAggregateBytes(aggregate, uint64(len(encoded)), maxEvidenceDescriptorBytes)
		if !withinBudget {
			return nil, ErrEvidenceArchive
		}
		descriptorSet, err := newValidatedDescriptorSet(encoded)
		if err != nil || descriptorSet.fingerprint != fingerprint {
			return nil, ErrEvidenceArchive
		}
		validated[fingerprint] = descriptorSet
	}
	return validated, nil
}

func validatePreparedDescriptorRegistry(events []StoredEvent, supplied descriptorRegistry) (descriptorRegistry, error) {
	referenced, err := referencedDescriptorFingerprints(events)
	if err != nil || len(supplied) != len(referenced) {
		return nil, ErrEvidenceArchive
	}
	var aggregate uint64
	for fingerprint := range referenced {
		descriptorSet := supplied[fingerprint]
		if descriptorSet == nil || descriptorSet.files == nil || descriptorSet.fingerprint != fingerprint ||
			sha256.Sum256(descriptorSet.encoded) != fingerprint {
			return nil, ErrEvidenceArchive
		}
		var withinBudget bool
		aggregate, withinBudget = checkedAggregateBytes(aggregate, uint64(len(descriptorSet.encoded)), maxEvidenceDescriptorBytes)
		if !withinBudget {
			return nil, ErrEvidenceArchive
		}
	}
	return supplied, nil
}

func requireExactArchiveDescriptorSets(events []StoredEvent, descriptorSets map[[sha256.Size]byte][]byte) error {
	registry := make(descriptorRegistry, len(descriptorSets))
	for fingerprint, encoded := range descriptorSets {
		registry[fingerprint] = &validatedDescriptorSet{fingerprint: fingerprint, encoded: encoded}
	}
	return requireExactArchiveDescriptorRegistry(events, registry)
}

func requireExactArchiveDescriptorRegistry(events []StoredEvent, descriptorSets descriptorRegistry) error {
	referenced, err := referencedDescriptorFingerprints(events)
	if err != nil || len(referenced) != len(descriptorSets) {
		return ErrEvidenceArchive
	}
	for fingerprint := range referenced {
		if _, exists := descriptorSets[fingerprint]; !exists {
			return ErrEvidenceArchive
		}
	}
	return nil
}

func verifyStoredChainWithDescriptorSets(header ChainHeader, events []StoredEvent,
	descriptorSets map[[sha256.Size]byte][]byte) bool {
	registry, err := validatedDescriptorRegistry(events, descriptorSets)
	return err == nil && verifyStoredChainWithDescriptorRegistry(header, events, registry)
}

func verifyStoredChainWithDescriptorRegistry(header ChainHeader, events []StoredEvent,
	descriptorSets descriptorRegistry) bool {
	genesis, err := Genesis(header.WorkspaceID, header.ChainSalt)
	if err != nil || genesis != header.GenesisHash || len(events) != int(header.CurrentSequence) {
		return false
	}
	previous := genesis
	seenOpenings := make(map[[sha256.Size]byte]struct{}, len(events)*5)
	for index := range events {
		stored := events[index]
		if stored.Event == nil || stored.Event.WorkspaceId != header.WorkspaceID || stored.Event.Generation != header.Generation ||
			stored.Event.Sequence != uint64(index+1) || !bytes.Equal(stored.Event.PreviousHash, previous[:]) ||
			len(stored.Event.PayloadSchemaFingerprint) != sha256.Size ||
			!addUniqueCommitmentOpenings(seenOpenings, stored.Event.CommitmentOpenings) {
			return false
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], stored.Event.PayloadSchemaFingerprint)
		prepared, err := reconstructStoredEventWithValidatedDescriptor(stored, descriptorSets[fingerprint])
		if err != nil || prepared.PayloadType != stored.PayloadType ||
			!bytes.Equal(prepared.PayloadProto, stored.PayloadProto) || !bytes.Equal(prepared.PayloadJSON, stored.PayloadJSON) ||
			!bytes.Equal(prepared.CanonicalEvent, stored.CanonicalEvent) || !bytes.Equal(prepared.EventProto, stored.EventProto) {
			return false
		}
		copy(previous[:], stored.Event.EventHash)
	}
	return previous == header.CurrentHead
}
