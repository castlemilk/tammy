package audit

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEvidenceArchiveSupportsHistoricalDescriptorSetsAndValidatesPayloadPerEvent(t *testing.T) {
	currentDescriptors := testAuditDescriptorSet(t)
	historicalDescriptors := testEvolvedAuditDescriptorSet(t, currentDescriptors)
	currentFingerprint := sha256.Sum256(currentDescriptors)
	historicalFingerprint := sha256.Sum256(historicalDescriptors)
	if currentFingerprint == historicalFingerprint {
		t.Fatal("historical schema fixture did not produce a distinct fingerprint")
	}

	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x6c}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	first := verifierEvent(1, genesis)
	first.Event.PayloadSchemaFingerprint = currentFingerprint[:]
	first.Event.CommitmentOpenings = nil
	first, err = PrepareEvent(genesis, first.Event, first.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	var firstHead [sha256.Size]byte
	copy(firstHead[:], first.Event.EventHash)
	second := historicalStoredEventWithUnknown(t, 2, firstHead, historicalDescriptors, historicalFingerprint)
	if err := validateStoredPayloadWithDescriptor(second, historicalDescriptors); err != nil {
		t.Fatalf("historical field validation failed: %v", err)
	}
	var head [sha256.Size]byte
	copy(head[:], second.Event.EventHash)
	header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentSequence: 2, CurrentHead: head, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	dek := bytes.Repeat([]byte{0x5a}, 32)
	key, _, err := GenerateSigningKey(workspaceID, dek, time.Date(2026, 8, 4, 1, 1, 0, 0, time.UTC),
		bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	descriptorSets := map[[sha256.Size]byte][]byte{
		currentFingerprint:    currentDescriptors,
		historicalFingerprint: historicalDescriptors,
	}
	input := EvidenceArchiveInput{Header: header, Events: []StoredEvent{first, second}, DescriptorSets: descriptorSets,
		SigningKey: key, DEK: dek, CreatedAt: time.Date(2026, 8, 4, 3, 4, 5, 0, time.UTC)}
	validatedSets, err := validatedDescriptorSets(input.Events, input.DescriptorSets)
	if err != nil {
		t.Fatalf("historical descriptor map validation: %v", err)
	}
	if !verifyStoredChainWithDescriptorSets(header, input.Events, validatedSets) {
		t.Fatal("descriptor-aware historical chain verification failed")
	}
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyEvidenceArchive(archive)
	if err != nil || verification.EventCount != 2 {
		t.Fatalf("historical archive verification=%#v err=%v", verification, err)
	}
	members := readArchiveMembers(t, archive)
	for fingerprint, descriptorSet := range descriptorSets {
		name := "descriptors/" + hex.EncodeToString(fingerprint[:]) + ".pb"
		if !bytes.Equal(members[name], descriptorSet) {
			t.Fatalf("descriptor member %q missing or changed", name)
		}
	}
	if _, legacy := members["descriptors.pb"]; legacy {
		t.Fatal("archive retained ambiguous single-schema descriptors.pb")
	}
	for sequence, stored := range []StoredEvent{first, second} {
		name := fmt.Sprintf("events/%020d/payload.type", sequence+1)
		if string(members[name]) != stored.PayloadType {
			t.Fatalf("payload type member %q=%q, want %q", name, members[name], stored.PayloadType)
		}
	}

	missing := input
	missing.DescriptorSets = map[[sha256.Size]byte][]byte{currentFingerprint: currentDescriptors}
	if _, err := BuildSignedEvidenceArchive(missing); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("missing historical descriptor error=%v, want ErrEvidenceArchive", err)
	}
	extraBytes := testEvolvedAuditDescriptorSet(t, historicalDescriptors)
	extraFingerprint := sha256.Sum256(extraBytes)
	extra := input
	extra.DescriptorSets = map[[sha256.Size]byte][]byte{
		currentFingerprint: currentDescriptors, historicalFingerprint: historicalDescriptors, extraFingerprint: extraBytes,
	}
	if _, err := BuildSignedEvidenceArchive(extra); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("unreferenced descriptor error=%v, want ErrEvidenceArchive", err)
	}
	reserved := input
	reserved.Evidence = []EvidenceObject{{Path: "descriptors/custom.pb", Bytes: []byte("shadow")}}
	if _, err := BuildSignedEvidenceArchive(reserved); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("descriptor namespace evidence error=%v, want ErrEvidenceArchive", err)
	}
	sequence := uint64(2)
	filter := &tammyv1.AuditEventFilter{StartSequence: &sequence, EndSequence: &sequence}
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	selectedInput := input
	selectedInput.SelectionApplied = true
	selectedInput.SelectedEvents = []StoredEvent{second}
	selectedInput.FilterProto = filterProto
	selectedArchive, err := BuildSignedEvidenceArchive(selectedInput)
	if err != nil {
		t.Fatal(err)
	}
	if verification, err := VerifyEvidenceArchive(selectedArchive); err != nil || verification.EventCount != 1 {
		t.Fatalf("selected historical archive verification=%#v err=%v", verification, err)
	}
}

func TestHistoricalDescriptorValidationRejectsPayloadTypeBytesAndJSONMismatch(t *testing.T) {
	descriptors := testAuditDescriptorSet(t)
	fingerprint := sha256.Sum256(descriptors)
	salt := bytes.Repeat([]byte{0x6d}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	stored := verifierEvent(1, genesis)
	stored.Event.PayloadSchemaFingerprint = fingerprint[:]
	stored.Event.CommitmentOpenings = nil
	stored, _ = PrepareEvent(genesis, stored.Event, stored.PayloadProto)
	if err := validateStoredPayloadWithDescriptor(stored, descriptors); err != nil {
		t.Fatalf("valid dynamically decoded payload rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*StoredEvent)
	}{
		{name: "wrong payload type", mutate: func(candidate *StoredEvent) {
			candidate.PayloadType = "tammy.v1.WorkspaceTrustEstablishedEvent"
		}},
		{name: "unknown payload field", mutate: func(candidate *StoredEvent) {
			candidate.PayloadProto = protowire.AppendTag(candidate.PayloadProto, 127, protowire.VarintType)
			candidate.PayloadProto = protowire.AppendVarint(candidate.PayloadProto, 1)
		}},
		{name: "non deterministic duplicate field", mutate: func(candidate *StoredEvent) {
			candidate.PayloadProto = protowire.AppendTag(candidate.PayloadProto, 1, protowire.BytesType)
			candidate.PayloadProto = protowire.AppendString(candidate.PayloadProto, candidate.Event.WorkspaceId)
		}},
		{name: "canonical JSON mismatch", mutate: func(candidate *StoredEvent) {
			candidate.PayloadJSON = []byte(`{"workspaceId":"different"}`)
		}},
		{name: "event payload binding mismatch", mutate: func(candidate *StoredEvent) {
			changed := proto.Clone(candidate.Event.GetPayload().GetWorkspaceStateChanged()).(*tammyv1.WorkspaceStateChangedEvent)
			changed.ReasonCode = "DIFFERENT"
			candidate.PayloadProto, _ = proto.MarshalOptions{Deterministic: true}.Marshal(changed)
			candidate.PayloadJSON, _ = canonical.NormalizedJSON(changed)
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := stored
			candidate.PayloadProto = append([]byte(nil), stored.PayloadProto...)
			candidate.PayloadJSON = append([]byte(nil), stored.PayloadJSON...)
			testCase.mutate(&candidate)
			if err := validateStoredPayloadWithDescriptor(candidate, descriptors); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("validation error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestHistoricalDescriptorSecretFieldIsRejectedByBuildAndVerifier(t *testing.T) {
	baseDescriptors := testAuditDescriptorSet(t)
	safeDescriptors := testEvolvedAuditDescriptorSet(t, baseDescriptors)
	secretDescriptors := renameHistoricalDescriptorField(t, safeDescriptors, "password")
	safeFingerprint := sha256.Sum256(safeDescriptors)
	secretFingerprint := sha256.Sum256(secretDescriptors)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x6e}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	safeStored := historicalStoredEventWithUnknown(t, 1, genesis, safeDescriptors, safeFingerprint)
	secretStored := historicalStoredEventWithUnknown(t, 1, genesis, secretDescriptors, secretFingerprint)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	key, _, err := GenerateSigningKey(workspaceID, dek, time.Date(2026, 8, 4, 1, 1, 0, 0, time.UTC),
		bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	inputFor := func(stored StoredEvent, fingerprint [sha256.Size]byte, descriptors []byte) EvidenceArchiveInput {
		header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
			CurrentSequence: 1, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
		copy(header.CurrentHead[:], stored.Event.EventHash)
		return EvidenceArchiveInput{Header: header, Events: []StoredEvent{stored},
			DescriptorSets: map[[sha256.Size]byte][]byte{fingerprint: descriptors}, SigningKey: key,
			DEK: append([]byte(nil), dek...), CreatedAt: time.Date(2026, 8, 4, 3, 4, 5, 0, time.UTC)}
	}
	t.Run("build", func(t *testing.T) {
		if _, err := BuildSignedEvidenceArchive(inputFor(secretStored, secretFingerprint, secretDescriptors)); !errors.Is(err, ErrEvidenceArchive) {
			t.Fatalf("secret historical descriptor build error=%v, want ErrEvidenceArchive", err)
		}
	})
	t.Run("verify", func(t *testing.T) {
		safeArchive, err := BuildSignedEvidenceArchive(inputFor(safeStored, safeFingerprint, safeDescriptors))
		if err != nil {
			t.Fatal(err)
		}
		members := readArchiveMembers(t, safeArchive)
		delete(members, descriptorArchivePath(safeFingerprint))
		members[descriptorArchivePath(secretFingerprint)] = secretDescriptors
		const prefix = "events/00000000000000000001/"
		members[prefix+"event.pb"] = secretStored.EventProto
		members[prefix+"payload.pb"] = secretStored.PayloadProto
		members[prefix+"payload.json"] = secretStored.PayloadJSON
		validated, err := newValidatedDescriptorSet(secretDescriptors)
		if err != nil {
			t.Fatal(err)
		}
		publicJSON, err := canonicalStoredEventJSONWithDescriptor(secretStored, validated)
		if err != nil {
			t.Fatal(err)
		}
		members["events.jsonl"] = append(publicJSON, '\n')
		unsafeArchive := resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
			manifest.VerifiedHead = append([]byte(nil), secretStored.Event.EventHash...)
			for _, object := range manifest.Objects {
				if object.Path == descriptorArchivePath(safeFingerprint) {
					object.Path = descriptorArchivePath(secretFingerprint)
				}
			}
			sort.Slice(manifest.Objects, func(left, right int) bool { return manifest.Objects[left].Path < manifest.Objects[right].Path })
		})
		if _, err := VerifyEvidenceArchive(unsafeArchive); !errors.Is(err, ErrEvidenceArchive) {
			t.Fatalf("secret historical descriptor verification error=%v, want ErrEvidenceArchive", err)
		}
	})
}

func TestArchiveDescriptorMembersRequireCanonicalLowercaseFingerprintNames(t *testing.T) {
	descriptors := testAuditDescriptorSet(t)
	fingerprint := sha256.Sum256(descriptors)
	validName := "descriptors/" + hex.EncodeToString(fingerprint[:]) + ".pb"
	if _, err := descriptorSetsFromArchiveMembers(map[string][]byte{validName: descriptors}); err != nil {
		t.Fatalf("valid descriptor member rejected: %v", err)
	}
	invalidDescriptors := testEvolvedAuditDescriptorSet(t, descriptors)
	invalidFingerprint := sha256.Sum256(invalidDescriptors)
	for _, testCase := range []struct {
		name    string
		members map[string][]byte
	}{
		{name: "uppercase fingerprint", members: map[string][]byte{
			"descriptors/" + strings.ToUpper(hex.EncodeToString(fingerprint[:])) + ".pb": descriptors,
		}},
		{name: "short fingerprint", members: map[string][]byte{"descriptors/abcd.pb": descriptors}},
		{name: "hash content mismatch", members: map[string][]byte{validName: invalidDescriptors}},
		{name: "legacy ambiguous member", members: map[string][]byte{validName: descriptors, "descriptors.pb": descriptors}},
		{name: "noncanonical descriptor set", members: map[string][]byte{
			"descriptors/" + hex.EncodeToString(invalidFingerprint[:]) + ".pb": reverseDescriptorFiles(t, invalidDescriptors),
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := descriptorSetsFromArchiveMembers(testCase.members); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("descriptor member error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestSelectedArchiveSequencesRejectsMissingAndStrayEventArtifacts(t *testing.T) {
	prefix := "events/00000000000000000007/"
	members := map[string][]byte{
		prefix + "event.pb":     {1},
		prefix + "payload.pb":   {2},
		prefix + "payload.json": {3},
		prefix + "payload.type": {4},
	}
	if sequences, err := selectedArchiveSequences(members); err != nil || len(sequences) != 1 || sequences[0] != 7 {
		t.Fatalf("valid event artifact set sequences=%v err=%v", sequences, err)
	}
	delete(members, prefix+"payload.type")
	if _, err := selectedArchiveSequences(members); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("missing payload.type error=%v, want ErrEvidenceArchive", err)
	}
	members[prefix+"payload.type"] = []byte{4}
	members[prefix+"shadow.bin"] = []byte{5}
	if _, err := selectedArchiveSequences(members); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("stray event artifact error=%v, want ErrEvidenceArchive", err)
	}
}

func historicalStoredEventWithUnknown(t *testing.T, sequence uint64, previous [sha256.Size]byte,
	descriptors []byte, fingerprint [sha256.Size]byte) StoredEvent {
	t.Helper()
	stored := verifierEvent(sequence, previous)
	event := proto.Clone(stored.Event).(*tammyv1.AuditEvent)
	event.PayloadSchemaFingerprint = fingerprint[:]
	payload := event.GetPayload().GetWorkspaceStateChanged()
	unknown := protowire.AppendTag(nil, 99, protowire.BytesType)
	unknown = protowire.AppendString(unknown, "retained-by-historical-schema")
	payload.ProtoReflect().SetUnknown(unknown)
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	files, err := validateDescriptorSet(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := files.FindDescriptorByName("tammy.v1.WorkspaceStateChangedEvent")
	if err != nil {
		t.Fatal(err)
	}
	dynamicPayload := dynamicpb.NewMessage(descriptor.(protoreflect.MessageDescriptor))
	if err := proto.Unmarshal(payloadProto, dynamicPayload); err != nil {
		t.Fatal(err)
	}
	payloadJSON, err := canonical.NormalizedJSON(dynamicPayload)
	if err != nil {
		t.Fatal(err)
	}
	event.PreviousHash = append([]byte(nil), previous[:]...)
	event.EventHash = nil
	chainView := proto.Clone(event).(*tammyv1.AuditEvent)
	chainView.PreviousHash = nil
	chainView.EventHash = nil
	chainView.Payload = nil
	chainView.PayloadSchemaFingerprint = nil
	canonicalEvent, err := canonicalEventEnvelope(chainView, payloadProto, payloadJSON,
		"tammy.v1.WorkspaceStateChangedEvent", fingerprint[:])
	if err != nil {
		t.Fatal(err)
	}
	eventHash, err := EventHash(previous, canonicalEvent)
	if err != nil {
		t.Fatal(err)
	}
	event.EventHash = eventHash[:]
	eventProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return StoredEvent{Event: event, PayloadType: "tammy.v1.WorkspaceStateChangedEvent", PayloadProto: payloadProto,
		PayloadJSON: payloadJSON, CanonicalEvent: canonicalEvent, EventProto: eventProto}
}

func TestBuildSignedEvidenceArchiveIsDeterministicCanonicalAndStandaloneVerifiable(t *testing.T) {
	descriptors := testAuditDescriptorSet(t)
	descriptorHash := sha256.Sum256(descriptors)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x31}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	payload := &tammyv1.WorkspaceTrustEstablishedEvent{
		WorkspaceId: workspaceID, PriorHead: genesis[:],
		DestinationInstallationHash: bytes.Repeat([]byte{0x42}, sha256.Size), PriorMirrorUnavailable: true,
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	event, err := PrepareEvent(genesis, &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b002", WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Type:                     tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED,
		OccurredAt:               timestamppb.New(time.Date(2026, 8, 4, 2, 3, 4, 5, time.UTC)),
		Source:                   &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: 1, ContentHash: genesis[:]},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceTrustEstablished{WorkspaceTrustEstablished: payload}},
		PayloadSchemaFingerprint: descriptorHash[:], CommandType: "tammy.v1.WorkspaceService.EstablishMovedWorkspaceTrust",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.EstablishMovedWorkspaceTrustResponse", DeterministicSha256: descriptorHash[:], OutcomeCode: "OK"},
	}, payloadProto)
	if err != nil {
		t.Fatal(err)
	}
	header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentSequence: 1, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	copy(header.CurrentHead[:], event.Event.EventHash)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	key, _, err := GenerateSigningKey(workspaceID, dek, time.Date(2026, 8, 4, 1, 1, 0, 0, time.UTC), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	input := EvidenceArchiveInput{
		Header: header, Events: []StoredEvent{event}, DescriptorSets: map[[sha256.Size]byte][]byte{descriptorHash: descriptors}, SigningKey: key, DEK: dek,
		CreatedAt: time.Date(2026, 8, 4, 3, 4, 5, 6, time.UTC),
		Evidence:  []EvidenceObject{{Path: "evidence/receipt-01.bin", Bytes: []byte("selected encrypted evidence")}},
	}
	first, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical inputs produced different ZIP bytes")
	}
	input.DEK = nil
	signerCalls := 0
	injected, err := buildSignedEvidenceArchiveWithSigner(input, func(record SigningKeyRecord, manifestHash [sha256.Size]byte) ([]byte, error) {
		signerCalls++
		return SignManifestHash(record, dek, manifestHash)
	})
	if err != nil {
		t.Fatal(err)
	}
	if signerCalls != 1 || !bytes.Equal(first, injected) {
		t.Fatalf("injected signer calls=%d archive_equal=%t", signerCalls, bytes.Equal(first, injected))
	}
	for _, testCase := range []struct {
		name      string
		signature []byte
	}{
		{name: "wrong length", signature: bytes.Repeat([]byte{0x11}, ed25519.SignatureSize-1)},
		{name: "invalid signature", signature: bytes.Repeat([]byte{0x11}, ed25519.SignatureSize)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := buildSignedEvidenceArchiveWithSigner(input, func(SigningKeyRecord, [sha256.Size]byte) ([]byte, error) {
				return testCase.signature, nil
			}); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("invalid injected signature error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
	cleanupCompleted := false
	ordered, err := buildSignedEvidenceArchiveWithSigner(input, func(record SigningKeyRecord, manifestHash [sha256.Size]byte) ([]byte, error) {
		signature, signErr := SignManifestHash(record, dek, manifestHash)
		if signErr != nil {
			return nil, signErr
		}
		defer func() {
			cleanupCompleted = true
		}()
		return signature, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEvidenceArchive(ordered); err != nil || !cleanupCompleted {
		t.Fatalf("ordered archive verification error=%v signer cleanup completed=%t", err, cleanupCompleted)
	}
	result, err := VerifyEvidenceArchive(first)
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.WorkspaceId != workspaceID || result.Manifest.EndSequence != 1 || result.EventCount != 1 {
		t.Fatalf("unexpected verification result: %#v", result)
	}
	members := readArchiveMembers(t, first)
	wantMembers := []string{
		descriptorArchivePath(descriptorHash), "events.jsonl", "events/00000000000000000001/event.pb",
		"events/00000000000000000001/payload.json", "events/00000000000000000001/payload.pb",
		"events/00000000000000000001/payload.type",
		"evidence/receipt-01.bin", "manifest.json", "public-key.ed25519", "signature.ed25519",
	}
	for _, name := range wantMembers {
		if _, exists := members[name]; !exists {
			t.Errorf("archive member %q missing", name)
		}
	}
	if !bytes.Equal(members["events/00000000000000000001/payload.pb"], payloadProto) ||
		!bytes.Equal(members["events/00000000000000000001/payload.json"], event.PayloadJSON) {
		t.Fatal("export did not preserve exact retained payload bytes")
	}
	for _, forbidden := range [][]byte{[]byte("actual-workspace-passphrase-value"), []byte("/Users/example/secret-destination.zip"), dek, key.EncryptedPrivateKey} {
		if bytes.Contains(first, forbidden) {
			t.Fatalf("archive contains forbidden secret material %x", forbidden)
		}
	}
}

func TestVerifyEvidenceArchiveRejectsDelimiterStormsInEveryJSONLMember(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	fullArchive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	sequence := uint64(1)
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(&tammyv1.AuditEventFilter{
		StartSequence: &sequence,
		EndSequence:   &sequence,
	})
	if err != nil {
		t.Fatal(err)
	}
	selectedInput := input
	selectedInput.SelectionApplied = true
	selectedInput.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	selectedInput.FilterProto = filterProto
	selectedArchive, err := BuildSignedEvidenceArchive(selectedInput)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		archive []byte
		path    string
	}{
		{name: "full events", archive: fullArchive, path: "events.jsonl"},
		{name: "selected events", archive: selectedArchive, path: "events.jsonl"},
		{name: "event commitments", archive: selectedArchive, path: "chain/event-commitments.jsonl"},
		{name: "filter openings", archive: selectedArchive, path: "chain/filter-openings.jsonl"},
	}
	storm := bytes.Repeat([]byte{'\n'}, 1<<20)
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			members := readArchiveMembers(t, testCase.archive)
			members[testCase.path] = storm
			tampered := resignArchiveMembers(t, members, key, dek, nil)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("delimiter storm error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestSignedEvidenceArchiveIncludesAndVerifiesCompleteSigningKeyRotationChain(t *testing.T) {
	archive, root, successor, _ := buildRotatedEvidenceArchiveFixture(t, 2, nil)
	members := readArchiveMembers(t, archive)
	chainProto, exists := members["signing-key-chain.pb"]
	if !exists {
		t.Fatal("signed evidence archive omitted signing-key-chain.pb")
	}
	chain := new(tammyv1.AuditSigningKeyChain)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(chainProto, chain); err != nil ||
		len(chain.ProtoReflect().GetUnknown()) != 0 || len(chain.Keys) != 2 || len(chain.Links) != 1 {
		t.Fatalf("decode complete signing key chain: chain=%#v err=%v", chain, err)
	}
	canonicalChain, err := proto.MarshalOptions{Deterministic: true}.Marshal(chain)
	if err != nil || !bytes.Equal(canonicalChain, chainProto) {
		t.Fatal("signing key chain is not canonical deterministic protobuf")
	}
	verification, err := VerifyEvidenceArchive(archive)
	if err != nil || verification.Manifest.RootSigningKeyId != root.KeyID ||
		verification.Manifest.SigningKeyId != successor.KeyID || verification.Manifest.SigningKeyEpoch != 2 {
		t.Fatalf("rotation-chain verification=%#v error=%v", verification, err)
	}
}

func TestVerifyEvidenceArchiveRejectsSameGenerationRotationLinkWithoutAuditEvent(t *testing.T) {
	archive, root, successor, dek := buildRotatedEvidenceArchiveFixture(t, 1, nil)
	members := readArchiveMembers(t, archive)
	chain := new(tammyv1.AuditSigningKeyChain)
	if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil || len(chain.Links) != 1 {
		t.Fatalf("decode rotation chain: %#v err=%v", chain, err)
	}
	link := chain.Links[0]
	manifest := new(tammyv1.AuditExportManifest)
	if err := canonical.UnmarshalStrict(members["manifest.json"], manifest); err != nil {
		t.Fatal(err)
	}
	link.PriorSequence = 0
	link.PriorHead = append([]byte(nil), manifest.GenesisHash...)
	link.PredecessorSignature = nil
	link.SuccessorPossessionSignature = nil
	predecessorDigest, err := signingKeyLinkDigest(predecessorRotationSignatureDomain, link)
	if err != nil {
		t.Fatal(err)
	}
	predecessorPrivate, err := DecryptSigningKey(root, dek)
	if err != nil {
		t.Fatal(err)
	}
	link.PredecessorSignature = ed25519.Sign(predecessorPrivate, predecessorDigest[:])
	Zero(predecessorPrivate)
	successorDigest, err := signingKeyLinkDigest(successorPossessionSignatureDomain, link)
	if err != nil {
		t.Fatal(err)
	}
	successorPrivate, err := DecryptSigningKey(successor, dek)
	if err != nil {
		t.Fatal(err)
	}
	link.SuccessorPossessionSignature = ed25519.Sign(successorPrivate, successorDigest[:])
	Zero(successorPrivate)
	members[signingKeyChainArchivePath], err = proto.MarshalOptions{Deterministic: true}.Marshal(chain)
	if err != nil {
		t.Fatal(err)
	}
	tampered := resignArchiveMembers(t, members, successor, dek, nil)
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("same-generation rotation without binding audit event error=%v, want ErrEvidenceArchive", err)
	}
}

func TestVerifyEvidenceArchiveAcceptsSameGenerationRotationAfterSnapshot(t *testing.T) {
	archive, _, _, _ := buildRotatedEvidenceArchiveFixture(t, 1, nil)
	if _, err := VerifyEvidenceArchive(archive); err != nil {
		t.Fatalf("same-generation post-snapshot rotation rejected: %v", err)
	}
}

func TestVerifyEvidenceArchiveAcceptsSameGenerationRotationStrictlyAfterSnapshot(t *testing.T) {
	strictlyAfterSnapshot := uint64(2)
	archive, _, _, _ := buildRotatedEvidenceArchiveFixture(t, 1, &strictlyAfterSnapshot)
	if _, err := VerifyEvidenceArchive(archive); err != nil {
		t.Fatalf("same-generation rotation strictly after snapshot rejected: %v", err)
	}
}

func TestVerifyEvidenceArchiveRejectsSameGenerationBoundaryRotationWithWrongPriorHead(t *testing.T) {
	archive, root, successor, dek := buildRotatedEvidenceArchiveFixture(t, 1, nil)
	members := readArchiveMembers(t, archive)
	manifest := new(tammyv1.AuditExportManifest)
	if err := canonical.UnmarshalStrict(members["manifest.json"], manifest); err != nil {
		t.Fatal(err)
	}
	chain := new(tammyv1.AuditSigningKeyChain)
	if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil || len(chain.Links) != 1 {
		t.Fatalf("decode boundary rotation chain: chain=%#v err=%v", chain, err)
	}
	link := chain.Links[0]
	if link.PriorSequence != manifest.EndSequence || !bytes.Equal(link.PriorHead, manifest.VerifiedHead) {
		t.Fatalf("fixture link prior=(%d,%x), manifest boundary=(%d,%x)",
			link.PriorSequence, link.PriorHead, manifest.EndSequence, manifest.VerifiedHead)
	}
	link.PriorHead = bytes.Repeat([]byte{0xee}, sha256.Size)
	link.PredecessorSignature = nil
	link.SuccessorPossessionSignature = nil
	predecessorDigest, err := signingKeyLinkDigest(predecessorRotationSignatureDomain, link)
	if err != nil {
		t.Fatal(err)
	}
	predecessorPrivate, err := DecryptSigningKey(root, dek)
	if err != nil {
		t.Fatal(err)
	}
	link.PredecessorSignature = ed25519.Sign(predecessorPrivate, predecessorDigest[:])
	Zero(predecessorPrivate)
	successorDigest, err := signingKeyLinkDigest(successorPossessionSignatureDomain, link)
	if err != nil {
		t.Fatal(err)
	}
	successorPrivate, err := DecryptSigningKey(successor, dek)
	if err != nil {
		t.Fatal(err)
	}
	link.SuccessorPossessionSignature = ed25519.Sign(successorPrivate, successorDigest[:])
	Zero(successorPrivate)
	members[signingKeyChainArchivePath], err = proto.MarshalOptions{Deterministic: true}.Marshal(chain)
	if err != nil {
		t.Fatal(err)
	}
	tampered := resignArchiveMembers(t, members, successor, dek, nil)
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("wrong boundary prior head error=%v, want ErrEvidenceArchive", err)
	}
}

func TestSelectedEvidenceArchiveBindsExcludedRotationWithoutDisclosingPrivateEnvelope(t *testing.T) {
	fixture := buildInSnapshotRotationArchiveFixture(t, true)
	if _, err := VerifyEvidenceArchive(fixture.archive); err != nil {
		t.Fatalf("selected archive with excluded in-snapshot rotation failed verification: %v", err)
	}
	members := readArchiveMembers(t, fixture.archive)
	chain := new(tammyv1.AuditSigningKeyChain)
	if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil || len(chain.EventProofs) != 1 {
		t.Fatalf("decode excluded rotation control proof: proofs=%d err=%v", len(chain.EventProofs), err)
	}
	for label, forbidden := range map[string][]byte{
		"actor user id":               []byte(fixture.actorUserID),
		"session id":                  []byte(fixture.sessionID),
		"source id":                   []byte(fixture.sourceID),
		"command type":                []byte(fixture.commandType),
		"result type":                 []byte(fixture.resultType),
		"result outcome":              []byte(fixture.resultOutcome),
		"result digest":               fixture.resultDigest,
		"hidden metadata blinding":    fixture.rotation.Event.CommitmentOpenings.HiddenMetadataBlinding,
		"hidden metadata opening hex": []byte(hex.EncodeToString(fixture.rotation.Event.CommitmentOpenings.HiddenMetadataBlinding)),
		"actor user id blinding":      fixture.rotation.Event.CommitmentOpenings.ActorUserIdBlinding,
		"actor user id opening hex":   []byte(hex.EncodeToString(fixture.rotation.Event.CommitmentOpenings.ActorUserIdBlinding)),
		"full event proto":            fixture.rotation.EventProto,
	} {
		for path, member := range members {
			if bytes.Contains(member, forbidden) {
				t.Errorf("archive member %q disclosed excluded rotation %s", path, label)
			}
		}
	}
}

type inSnapshotRotationArchiveFixture struct {
	archive       []byte
	input         EvidenceArchiveInput
	successor     SigningKeyRecord
	dek           []byte
	rotation      StoredEvent
	actorUserID   string
	sessionID     string
	sourceID      string
	commandType   string
	resultType    string
	resultOutcome string
	resultDigest  []byte
}

func buildInSnapshotRotationArchiveFixture(t *testing.T, selected bool) inSnapshotRotationArchiveFixture {
	return buildInSnapshotRotationArchiveFixtureWithDescriptor(t, selected, nil)
}

func buildInSnapshotRotationArchiveFixtureWithDescriptor(t *testing.T, selected bool,
	rotationDescriptorSet []byte) inSnapshotRotationArchiveFixture {
	t.Helper()
	input, _, dek := buildEvidenceArchiveFixtureInput(t)
	root := input.SigningKey
	rotatedAt := time.Date(2026, 8, 4, 2, 4, 5, 0, time.UTC)
	retiredRoot, successor, link, err := createSigningKeySuccessor(root, dek, input.Header.Generation,
		input.Header.CurrentSequence, input.Header.CurrentHead, rotatedAt,
		bytes.NewReader(bytes.Repeat([]byte{0x6c}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	linkDigest, err := signedSigningKeyRotationLinkDigest(link)
	if err != nil {
		t.Fatal(err)
	}
	payload := &tammyv1.SigningKeyRotatedEvent{
		WorkspaceId: input.Header.WorkspaceID, Generation: input.Header.Generation,
		SuccessorEpoch: successor.Epoch, PredecessorKeyId: root.KeyID, SuccessorKeyId: successor.KeyID,
		RotationLinkSha256: linkDigest[:],
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	actorUserID := "01890f60-4d6d-7c12-8f02-6c9129d5b0c1"
	sessionID := "01890f60-4d6d-7c12-8f02-6c9129d5b0c2"
	sourceID := "01890f60-4d6d-7c12-8f02-6c9129d5b0c3"
	commandType := "tammy.v1.AuditService.RotateSigningKey.PrivateControl"
	resultType := "tammy.v1.PrivateRotationResult"
	resultOutcome := "PRIVATE_OK"
	resultDigest := bytes.Repeat([]byte{0xd1}, sha256.Size)
	var fingerprint [sha256.Size]byte
	for candidate := range input.DescriptorSets {
		fingerprint = candidate
	}
	if len(rotationDescriptorSet) != 0 {
		fingerprint = sha256.Sum256(rotationDescriptorSet)
		input.DescriptorSets[fingerprint] = append([]byte(nil), rotationDescriptorSet...)
	}
	event := &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b0c0", WorkspaceId: input.Header.WorkspaceID,
		Generation: input.Header.Generation, Sequence: input.Header.CurrentSequence + 1,
		Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED, OccurredAt: timestamppb.New(rotatedAt),
		Actor: &tammyv1.AuthenticationContext{ActorUserId: actorUserID, SessionId: sessionID},
		Source: &tammyv1.SourceRef{Type: "audit_signing_key", Id: sourceID, Revision: 2,
			ContentHash: bytes.Repeat([]byte{0xc1}, sha256.Size)},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_SigningKeyRotated{SigningKeyRotated: payload}},
		PayloadSchemaFingerprint: fingerprint[:], CommandType: commandType,
		Result: &tammyv1.AuditResultMetadata{TypeName: resultType, DeterministicSha256: resultDigest, OutcomeCode: resultOutcome},
	}
	blindingBytes := make([]byte, 0, 5*sha256.Size)
	for value := byte(0xa1); value <= 0xa5; value++ {
		blindingBytes = append(blindingBytes, bytes.Repeat([]byte{value}, sha256.Size)...)
	}
	rotation, err := prepareEventWithBlindingSource(input.Header.CurrentHead, event, payloadProto,
		bytes.NewReader(blindingBytes))
	if err != nil {
		t.Fatal(err)
	}
	input.Header.CurrentSequence = rotation.Event.Sequence
	copy(input.Header.CurrentHead[:], rotation.Event.EventHash)
	input.Events = append(input.Events, rotation)
	input.SigningKey = successor
	input.SigningKeyHistory = []SigningKeyRecord{retiredRoot, successor}
	if selected {
		input.SelectionApplied = true
		input.SelectedEvents = []StoredEvent{input.Events[0]}
		filter := &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{input.Events[0].Event.Type}}
		input.FilterProto, err = proto.MarshalOptions{Deterministic: true}.Marshal(filter)
		if err != nil {
			t.Fatal(err)
		}
	}
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	return inSnapshotRotationArchiveFixture{
		archive: archive, input: input, successor: successor, dek: dek, rotation: rotation,
		actorUserID: actorUserID, sessionID: sessionID, sourceID: sourceID, commandType: commandType,
		resultType: resultType, resultOutcome: resultOutcome, resultDigest: resultDigest,
	}
}

func TestVerifyEvidenceArchiveAcceptsPrivacyPreservingRotationProofInSelectedAndFullArchives(t *testing.T) {
	for _, selected := range []bool{false, true} {
		name := "full"
		if selected {
			name = "selected"
		}
		t.Run(name, func(t *testing.T) {
			first := buildInSnapshotRotationArchiveFixture(t, selected)
			second, err := BuildSignedEvidenceArchive(first.input)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first.archive, second) {
				t.Fatal("rotation proof archive bytes are not deterministic")
			}
			verification, err := VerifyEvidenceArchive(first.archive)
			if err != nil || verification.EventCount != map[bool]uint64{false: 2, true: 1}[selected] {
				t.Fatalf("verification=%#v err=%v", verification, err)
			}
		})
	}
}

func TestVerifyEvidenceArchiveRejectsResignedPrivacyRotationProofTampering(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*tammyv1.AuditSigningKeyChain)
	}{
		{name: "missing proof", mutate: func(chain *tammyv1.AuditSigningKeyChain) { chain.EventProofs = nil }},
		{name: "wrong epoch", mutate: func(chain *tammyv1.AuditSigningKeyChain) { chain.EventProofs[0].SuccessorEpoch++ }},
		{name: "duplicate proof", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs = append(chain.EventProofs, proto.Clone(chain.EventProofs[0]).(*tammyv1.AuditSigningKeyRotationEventProof))
		}},
		{name: "schema fingerprint", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs[0].SchemaFingerprint[0] ^= 0xff
		}},
		{name: "payload identity opening", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs[0].PayloadIdentityBlinding[0] ^= 0xff
		}},
		{name: "zero payload identity opening", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs[0].PayloadIdentityBlinding = make([]byte, sha256.Size)
		}},
		{name: "truncated event type opening", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs[0].EventTypeBlinding = chain.EventProofs[0].EventTypeBlinding[:sha256.Size-1]
		}},
		{name: "event type opening", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs[0].EventTypeBlinding[0] ^= 0xff
		}},
		{name: "occurred at opening", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs[0].OccurredAtBlinding[0] ^= 0xff
		}},
		{name: "opening reused across owners", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.EventProofs[0].PayloadIdentityBlinding = append([]byte(nil), chain.EventProofs[0].OccurredAtBlinding...)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := buildInSnapshotRotationArchiveFixture(t, true)
			members := readArchiveMembers(t, fixture.archive)
			chain := new(tammyv1.AuditSigningKeyChain)
			if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil {
				t.Fatal(err)
			}
			testCase.mutate(chain)
			var err error
			members[signingKeyChainArchivePath], err = proto.MarshalOptions{Deterministic: true}.Marshal(chain)
			if err != nil {
				t.Fatal(err)
			}
			tampered := resignArchiveMembers(t, members, fixture.successor, fixture.dek, nil)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("tampered %s error=%v, want ErrEvidenceArchive", testCase.name, err)
			}
		})
	}
}

func TestVerifySelectedEvidenceArchiveRejectsReorderedRotationProofs(t *testing.T) {
	input, _, dek := buildEvidenceArchiveFixtureInput(t)
	appendInSnapshotRotation(t, &input, dek, time.Date(2026, 8, 4, 2, 4, 5, 0, time.UTC),
		0x6c, "01890f60-4d6d-7c12-8f02-6c9129d5b0d0", "01890f60-4d6d-7c12-8f02-6c9129d5b0d1", 0xb1)
	appendInSnapshotRotation(t, &input, dek, time.Date(2026, 8, 4, 2, 5, 5, 0, time.UTC),
		0x6d, "01890f60-4d6d-7c12-8f02-6c9129d5b0d2", "01890f60-4d6d-7c12-8f02-6c9129d5b0d3", 0xc1)
	input.SelectionApplied = true
	input.SelectedEvents = []StoredEvent{input.Events[0]}
	filter := &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{input.Events[0].Event.Type}}
	var err error
	input.FilterProto, err = proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEvidenceArchive(archive); err != nil {
		t.Fatalf("two-rotation selected archive rejected: %v", err)
	}
	members := readArchiveMembers(t, archive)
	chain := new(tammyv1.AuditSigningKeyChain)
	if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil || len(chain.EventProofs) != 2 {
		t.Fatalf("decode two rotation proofs: proofs=%d err=%v", len(chain.EventProofs), err)
	}
	chain.EventProofs[0], chain.EventProofs[1] = chain.EventProofs[1], chain.EventProofs[0]
	members[signingKeyChainArchivePath], err = proto.MarshalOptions{Deterministic: true}.Marshal(chain)
	if err != nil {
		t.Fatal(err)
	}
	tampered := resignArchiveMembers(t, members, input.SigningKey, dek, nil)
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("reordered proof error=%v, want ErrEvidenceArchive", err)
	}
}

func appendInSnapshotRotation(t *testing.T, input *EvidenceArchiveInput, dek []byte, rotatedAt time.Time,
	keyRandom byte, eventID, sourceID string, openingStart byte) {
	t.Helper()
	current := input.SigningKey
	retired, successor, link, err := createSigningKeySuccessor(current, dek, input.Header.Generation,
		input.Header.CurrentSequence, input.Header.CurrentHead, rotatedAt,
		bytes.NewReader(bytes.Repeat([]byte{keyRandom}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	linkDigest, err := signedSigningKeyRotationLinkDigest(link)
	if err != nil {
		t.Fatal(err)
	}
	payload := &tammyv1.SigningKeyRotatedEvent{
		WorkspaceId: input.Header.WorkspaceID, Generation: input.Header.Generation, SuccessorEpoch: successor.Epoch,
		PredecessorKeyId: current.KeyID, SuccessorKeyId: successor.KeyID, RotationLinkSha256: linkDigest[:],
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var fingerprint [sha256.Size]byte
	for candidate := range input.DescriptorSets {
		fingerprint = candidate
	}
	event := &tammyv1.AuditEvent{
		Id: eventID, WorkspaceId: input.Header.WorkspaceID, Generation: input.Header.Generation,
		Sequence: input.Header.CurrentSequence + 1, Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED,
		OccurredAt: timestamppb.New(rotatedAt),
		Source: &tammyv1.SourceRef{Type: "audit_signing_key", Id: sourceID, Revision: input.Header.CurrentSequence + 1,
			ContentHash: bytes.Repeat([]byte{0xe1}, sha256.Size)},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_SigningKeyRotated{SigningKeyRotated: payload}},
		PayloadSchemaFingerprint: fingerprint[:], CommandType: "tammy.v1.AuditService.RotateSigningKey",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.RotateSigningKeyResponse",
			DeterministicSha256: bytes.Repeat([]byte{0xe2}, sha256.Size), OutcomeCode: "OK"},
	}
	blindings := make([]byte, 0, 5*sha256.Size)
	for offset := byte(0); offset < 5; offset++ {
		blindings = append(blindings, bytes.Repeat([]byte{openingStart + offset}, sha256.Size)...)
	}
	stored, err := prepareEventWithBlindingSource(input.Header.CurrentHead, event, payloadProto, bytes.NewReader(blindings))
	if err != nil {
		t.Fatal(err)
	}
	input.Header.CurrentSequence = stored.Event.Sequence
	copy(input.Header.CurrentHead[:], stored.Event.EventHash)
	input.Events = append(input.Events, stored)
	if len(input.SigningKeyHistory) == 0 {
		input.SigningKeyHistory = []SigningKeyRecord{retired, successor}
	} else {
		input.SigningKeyHistory[len(input.SigningKeyHistory)-1] = retired
		input.SigningKeyHistory = append(input.SigningKeyHistory, successor)
	}
	input.SigningKey = successor
}

func TestVerifySelectedEvidenceArchiveRejectsRotationCanonicalAndDescriptorTampering(t *testing.T) {
	fixture := buildInSnapshotRotationArchiveFixture(t, true)
	t.Run("canonical event", func(t *testing.T) {
		members := readArchiveMembers(t, fixture.archive)
		commitments := members["chain/event-commitments.jsonl"]
		marker := []byte(`"payload_identity_commitment":"`)
		offset := bytes.Index(commitments, marker)
		if offset < 0 {
			t.Fatal("payload commitment missing")
		}
		offset += len(marker)
		commitments[offset] = map[bool]byte{true: '1', false: '0'}[commitments[offset] == '0']
		members["chain/event-commitments.jsonl"] = commitments
		tampered := resignArchiveMembers(t, members, fixture.successor, fixture.dek, nil)
		if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
			t.Fatalf("canonical tamper error=%v, want ErrEvidenceArchive", err)
		}
	})
	t.Run("missing descriptor", func(t *testing.T) {
		members := readArchiveMembers(t, fixture.archive)
		chain := new(tammyv1.AuditSigningKeyChain)
		if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil {
			t.Fatal(err)
		}
		var fingerprint [sha256.Size]byte
		copy(fingerprint[:], chain.EventProofs[0].SchemaFingerprint)
		descriptorPath := descriptorArchivePath(fingerprint)
		delete(members, descriptorPath)
		tampered := resignArchiveMembers(t, members, fixture.successor, fixture.dek, func(manifest *tammyv1.AuditExportManifest) {
			objects := manifest.Objects[:0]
			for _, object := range manifest.Objects {
				if object.Path != descriptorPath {
					objects = append(objects, object)
				}
			}
			manifest.Objects = objects
		})
		if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
			t.Fatalf("missing descriptor error=%v, want ErrEvidenceArchive", err)
		}
	})
}

func TestSelectedEvidenceArchiveRetainsExactProofOnlyRotationDescriptor(t *testing.T) {
	baseDescriptors := testAuditDescriptorSet(t)
	rotationDescriptors := testEvolvedAuditDescriptorSet(t, baseDescriptors)
	baseFingerprint := sha256.Sum256(baseDescriptors)
	rotationFingerprint := sha256.Sum256(rotationDescriptors)
	if baseFingerprint == rotationFingerprint {
		t.Fatal("proof-only descriptor fixture did not evolve")
	}
	fixture := buildInSnapshotRotationArchiveFixtureWithDescriptor(t, true, rotationDescriptors)
	members := readArchiveMembers(t, fixture.archive)
	for _, fingerprint := range [][sha256.Size]byte{baseFingerprint, rotationFingerprint} {
		if _, exists := members[descriptorArchivePath(fingerprint)]; !exists {
			t.Fatalf("selected archive omitted descriptor %x", fingerprint)
		}
	}
	if _, err := VerifyEvidenceArchive(fixture.archive); err != nil {
		t.Fatalf("selected archive with proof-only descriptor rejected: %v", err)
	}

	t.Run("missing proof-only descriptor", func(t *testing.T) {
		candidate := readArchiveMembers(t, fixture.archive)
		proofDescriptorPath := descriptorArchivePath(rotationFingerprint)
		delete(candidate, proofDescriptorPath)
		tampered := resignArchiveMembers(t, candidate, fixture.successor, fixture.dek, func(manifest *tammyv1.AuditExportManifest) {
			objects := manifest.Objects[:0]
			for _, object := range manifest.Objects {
				if object.Path != proofDescriptorPath {
					objects = append(objects, object)
				}
			}
			manifest.Objects = objects
		})
		if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
			t.Fatalf("missing proof-only descriptor error=%v, want ErrEvidenceArchive", err)
		}
	})

	t.Run("extraneous descriptor", func(t *testing.T) {
		candidate := readArchiveMembers(t, fixture.archive)
		extraDescriptors := testEvolvedAuditDescriptorSet(t, rotationDescriptors)
		extraFingerprint := sha256.Sum256(extraDescriptors)
		extraPath := descriptorArchivePath(extraFingerprint)
		candidate[extraPath] = extraDescriptors
		tampered := resignArchiveMembers(t, candidate, fixture.successor, fixture.dek, func(manifest *tammyv1.AuditExportManifest) {
			manifest.Objects = append(manifest.Objects, &tammyv1.AuditExportObject{Path: extraPath})
			sort.Slice(manifest.Objects, func(left, right int) bool {
				return manifest.Objects[left].Path < manifest.Objects[right].Path
			})
		})
		if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
			t.Fatalf("extraneous descriptor error=%v, want ErrEvidenceArchive", err)
		}
	})
}

func TestVerifyEvidenceArchiveRejectsProofAttachedToNonSnapshotRotation(t *testing.T) {
	proofFixture := buildInSnapshotRotationArchiveFixture(t, true)
	proofMembers := readArchiveMembers(t, proofFixture.archive)
	proofChain := new(tammyv1.AuditSigningKeyChain)
	if err := proto.Unmarshal(proofMembers[signingKeyChainArchivePath], proofChain); err != nil {
		t.Fatal(err)
	}
	for _, generation := range []uint64{1, 2} {
		t.Run(fmt.Sprintf("generation_%d", generation), func(t *testing.T) {
			archive, _, successor, dek := buildRotatedEvidenceArchiveFixture(t, generation, nil)
			members := readArchiveMembers(t, archive)
			chain := new(tammyv1.AuditSigningKeyChain)
			if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil {
				t.Fatal(err)
			}
			chain.EventProofs = []*tammyv1.AuditSigningKeyRotationEventProof{
				proto.Clone(proofChain.EventProofs[0]).(*tammyv1.AuditSigningKeyRotationEventProof),
			}
			var err error
			members[signingKeyChainArchivePath], err = proto.MarshalOptions{Deterministic: true}.Marshal(chain)
			if err != nil {
				t.Fatal(err)
			}
			tampered := resignArchiveMembers(t, members, successor, dek, nil)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("unexpected proof error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestVerifyEvidenceArchiveRejectsResignedSigningKeyChainTamperMatrix(t *testing.T) {
	otherWorkspace := "01890f60-4d6d-7c12-8f02-6c9129d5b009"
	for _, testCase := range []struct {
		name   string
		mutate func(*tammyv1.AuditSigningKeyChain)
	}{
		{name: "missing key", mutate: func(chain *tammyv1.AuditSigningKeyChain) { chain.Keys = chain.Keys[:1] }},
		{name: "reordered keys", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Keys[0], chain.Keys[1] = chain.Keys[1], chain.Keys[0]
		}},
		{name: "duplicate key", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Keys[1] = proto.Clone(chain.Keys[0]).(*tammyv1.AuditSigningPublicKey)
		}},
		{name: "forked predecessor", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Links = append(chain.Links, proto.Clone(chain.Links[0]).(*tammyv1.AuditSigningKeyRotationLink))
		}},
		{name: "missing link", mutate: func(chain *tammyv1.AuditSigningKeyChain) { chain.Links = nil }},
		{name: "predecessor signature", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Links[0].PredecessorSignature[0] ^= 0xff
		}},
		{name: "successor possession", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Links[0].SuccessorPossessionSignature[0] ^= 0xff
		}},
		{name: "cross workspace replay", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Links[0].WorkspaceId = otherWorkspace
		}},
		{name: "cross generation replay", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Links[0].Generation++
		}},
		{name: "missing retirement", mutate: func(chain *tammyv1.AuditSigningKeyChain) { chain.Keys[0].RetiredAt = nil }},
		{name: "retired terminal", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			chain.Keys[1].RetiredAt = timestamppb.New(chain.Keys[1].CreatedAt.AsTime().Add(time.Second))
		}},
		{name: "nested unknown field", mutate: func(chain *tammyv1.AuditSigningKeyChain) {
			unknown := protowire.AppendTag(nil, 99, protowire.VarintType)
			unknown = protowire.AppendVarint(unknown, 1)
			chain.Keys[0].ProtoReflect().SetUnknown(unknown)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			archive, _, successor, dek := buildRotatedEvidenceArchiveFixture(t, 2, nil)
			members := readArchiveMembers(t, archive)
			chain := new(tammyv1.AuditSigningKeyChain)
			if err := proto.Unmarshal(members[signingKeyChainArchivePath], chain); err != nil {
				t.Fatal(err)
			}
			testCase.mutate(chain)
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(chain)
			if err != nil {
				t.Fatal(err)
			}
			members[signingKeyChainArchivePath] = encoded
			tampered := resignArchiveMembers(t, members, successor, dek, nil)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("re-signed %s error=%v, want ErrEvidenceArchive", testCase.name, err)
			}
		})
	}
}

func TestVerifyEvidenceArchiveRejectsSigningKeyChainControlTampering(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, map[string][]byte, SigningKeyRecord, []byte) []byte
	}{
		{name: "missing chain", mutate: func(t *testing.T, members map[string][]byte, successor SigningKeyRecord, dek []byte) []byte {
			delete(members, signingKeyChainArchivePath)
			return resignArchiveMembers(t, members, successor, dek, func(manifest *tammyv1.AuditExportManifest) {
				objects := manifest.Objects[:0]
				for _, object := range manifest.Objects {
					if object.Path != signingKeyChainArchivePath {
						objects = append(objects, object)
					}
				}
				manifest.Objects = objects
			})
		}},
		{name: "noncanonical chain", mutate: func(t *testing.T, members map[string][]byte, successor SigningKeyRecord, dek []byte) []byte {
			encoded := append([]byte(nil), members[signingKeyChainArchivePath]...)
			encoded = protowire.AppendTag(encoded, 1, protowire.BytesType)
			encoded = protowire.AppendString(encoded, signingKeyChainVersion)
			members[signingKeyChainArchivePath] = encoded
			return resignArchiveMembers(t, members, successor, dek, nil)
		}},
		{name: "wrong root", mutate: func(t *testing.T, members map[string][]byte, successor SigningKeyRecord, dek []byte) []byte {
			return resignArchiveMembers(t, members, successor, dek, func(manifest *tammyv1.AuditExportManifest) {
				manifest.RootSigningKeyId = successor.KeyID
			})
		}},
		{name: "wrong active", mutate: func(t *testing.T, members map[string][]byte, successor SigningKeyRecord, dek []byte) []byte {
			chain := new(tammyv1.AuditSigningKeyChain)
			if proto.Unmarshal(members[signingKeyChainArchivePath], chain) != nil {
				t.Fatal("decode chain")
			}
			return resignArchiveMembers(t, members, successor, dek, func(manifest *tammyv1.AuditExportManifest) {
				manifest.SigningKeyId = chain.Keys[0].KeyId
			})
		}},
		{name: "wrong epoch", mutate: func(t *testing.T, members map[string][]byte, successor SigningKeyRecord, dek []byte) []byte {
			return resignArchiveMembers(t, members, successor, dek, func(manifest *tammyv1.AuditExportManifest) {
				manifest.SigningKeyEpoch = 1
			})
		}},
		{name: "active signature mismatch", mutate: func(_ *testing.T, members map[string][]byte, _ SigningKeyRecord, _ []byte) []byte {
			members["signature.ed25519"][0] ^= 0xff
			return writeRawArchive(t, members)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			archive, _, successor, dek := buildRotatedEvidenceArchiveFixture(t, 2, nil)
			tampered := testCase.mutate(t, readArchiveMembers(t, archive), successor, dek)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("%s error=%v, want ErrEvidenceArchive", testCase.name, err)
			}
		})
	}
}

func buildRotatedEvidenceArchiveFixture(t *testing.T, rotationGeneration uint64,
	rotationPriorSequence *uint64) ([]byte, SigningKeyRecord, SigningKeyRecord, []byte) {
	t.Helper()
	input, _, dek := buildEvidenceArchiveFixtureInput(t)
	root := input.SigningKey
	if root.Generation != input.Header.Generation || root.Epoch != 1 {
		t.Fatalf("fixture root scope generation=%d epoch=%d", root.Generation, root.Epoch)
	}
	priorSequence := input.Header.CurrentSequence
	priorHead := input.Header.CurrentHead
	if rotationPriorSequence != nil {
		priorSequence = *rotationPriorSequence
		if priorSequence == 0 {
			priorHead = input.Header.GenesisHash
		}
	}
	retiredRoot, successor, _, err := createSigningKeySuccessor(root, dek, rotationGeneration,
		priorSequence, priorHead,
		time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC), bytes.NewReader(bytes.Repeat([]byte{0x6c}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	input.SigningKey = successor
	input.SigningKeyHistory = []SigningKeyRecord{retiredRoot, successor}
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	return archive, root, successor, dek
}

func TestVerifyEvidenceArchiveRejectsTamperedObjectAndTraversal(t *testing.T) {
	archive := buildEvidenceArchiveFixture(t)
	members := readArchiveMembers(t, archive)
	members["events/00000000000000000001/payload.pb"][0] ^= 0xff
	tampered := writeRawArchive(t, members)
	if _, err := VerifyEvidenceArchive(tampered); err == nil {
		t.Fatal("tampered retained payload was accepted")
	}
	members = readArchiveMembers(t, archive)
	members["../escaped"] = []byte("x")
	traversal := writeRawArchive(t, members)
	if _, err := VerifyEvidenceArchive(traversal); err == nil {
		t.Fatal("archive traversal member was accepted")
	}
}

func TestVerifySelectedEvidenceArchiveRejectsManifestEventIdentityMismatch(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	input.SelectionApplied = true
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	input.FilterProto = nil // Canonical encoding of the default AuditEventFilter.
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*tammyv1.AuditEvent)
	}{
		{name: "workspace", mutate: func(event *tammyv1.AuditEvent) {
			event.WorkspaceId = "01890f60-4d6d-7c12-8f02-6c9129d5b009"
		}},
		{name: "generation", mutate: func(event *tammyv1.AuditEvent) {
			event.Generation++
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tampered := rewriteSelectedEventAndResign(t, archive, key, dek, testCase.mutate)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("identity-mismatched selected archive error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestVerifySelectedEvidenceArchiveRejectsOversizedEndWithoutPanic(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	input.SelectionApplied = true
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	members := readArchiveMembers(t, archive)
	members["chain/heads.bin"] = nil
	tampered := resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
		manifest.EndSequence = uint64(1) << 59
	})
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("selected archive verification panicked: %v", recovered)
		}
	}()
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("oversized selected proof error=%v, want ErrEvidenceArchive", err)
	}
}

func TestVerifySelectedEvidenceArchiveRejectsOmittedFilterMatch(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	first := input.Events[0]
	var previous [sha256.Size]byte
	copy(previous[:], first.Event.EventHash)
	secondEvent := proto.Clone(first.Event).(*tammyv1.AuditEvent)
	secondEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b003"
	secondEvent.Sequence = 2
	secondEvent.OccurredAt = timestamppb.New(secondEvent.OccurredAt.AsTime().Add(time.Second))
	secondEvent.PreviousHash = nil
	secondEvent.EventHash = nil
	secondEvent.CommitmentOpenings = nil
	second, err := PrepareEvent(previous, secondEvent, first.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	input.Events = []StoredEvent{first, second}
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	input.SelectionApplied = true
	input.Header.CurrentSequence = 2
	copy(input.Header.CurrentHead[:], second.Event.EventHash)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	members := readArchiveMembers(t, archive)
	const omittedPrefix = "events/00000000000000000002/"
	for name := range members {
		if strings.HasPrefix(name, omittedPrefix) {
			delete(members, name)
		}
	}
	lines := bytes.Split(members["events.jsonl"], []byte{'\n'})
	members["events.jsonl"] = append([]byte(nil), lines[0]...)
	members["events.jsonl"] = append(members["events.jsonl"], '\n')
	tampered := resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
		original := append([]*tammyv1.AuditExportObject(nil), manifest.Objects...)
		objects := manifest.Objects[:0]
		for _, object := range original {
			if _, exists := members[object.Path]; exists {
				objects = append(objects, object)
			}
		}
		manifest.Objects = objects
	})
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("selected archive with omitted default-filter match error=%v, want ErrEvidenceArchive", err)
	}
}

func TestSelectedEvidenceArchiveUsesCanonicalEventCommitmentProof(t *testing.T) {
	input, _, _ := buildEvidenceArchiveFixtureInput(t)
	input.SelectionApplied = true
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	members := readArchiveMembers(t, archive)
	proof, exists := members["chain/event-commitments.jsonl"]
	if !exists {
		t.Fatal("selected archive omitted chain/event-commitments.jsonl")
	}
	if _, legacy := members["chain/filter-events.jsonl"]; legacy {
		t.Fatal("selected archive retained legacy chain/filter-events.jsonl")
	}
	if !bytes.Equal(proof, append(append([]byte(nil), input.Events[0].CanonicalEvent...), '\n')) {
		t.Fatalf("event commitment proof did not preserve exact canonical v3 envelope: %s", proof)
	}
}

func TestSequenceOnlySelectedArchiveCarriesEmptyPerEventFilterOpenings(t *testing.T) {
	input, _, _ := buildEvidenceArchiveFixtureInput(t)
	first := input.Events[0]
	var previous [sha256.Size]byte
	copy(previous[:], first.Event.EventHash)
	secondEvent := proto.Clone(first.Event).(*tammyv1.AuditEvent)
	secondEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b003"
	secondEvent.Sequence = 2
	secondEvent.OccurredAt = timestamppb.New(secondEvent.OccurredAt.AsTime().Add(time.Second))
	secondEvent.PreviousHash = nil
	secondEvent.EventHash = nil
	secondEvent.CommitmentOpenings = nil
	second, err := PrepareEvent(previous, secondEvent, first.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	end := uint64(1)
	filter := &tammyv1.AuditEventFilter{EndSequence: &end}
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	input.Events = []StoredEvent{first, second}
	input.SelectionApplied = true
	input.SelectedEvents = []StoredEvent{first}
	input.FilterProto = filterProto
	input.Header.CurrentSequence = 2
	copy(input.Header.CurrentHead[:], second.Event.EventHash)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	members := readArchiveMembers(t, archive)
	want := []byte("{\"sequence\":\"1\",\"version\":\"tammy.audit.filter-opening.v1\"}\n" +
		"{\"sequence\":\"2\",\"version\":\"tammy.audit.filter-opening.v1\"}\n")
	if !bytes.Equal(members["chain/filter-openings.jsonl"], want) {
		t.Fatalf("sequence-only filter openings = %s, want %s", members["chain/filter-openings.jsonl"], want)
	}
	for _, forbidden := range []string{"event_type", "occurred_at", "actor_user_id", "blinding"} {
		if bytes.Contains(members["chain/filter-openings.jsonl"], []byte(forbidden)) {
			t.Fatalf("sequence-only filter openings disclosed %q: %s", forbidden, members["chain/filter-openings.jsonl"])
		}
	}
	excludedOpenings := second.Event.GetCommitmentOpenings()
	for category, opening := range map[string][]byte{
		"hidden metadata":  excludedOpenings.GetHiddenMetadataBlinding(),
		"payload identity": excludedOpenings.GetPayloadIdentityBlinding(),
		"event type":       excludedOpenings.GetEventTypeBlinding(),
		"occurred at":      excludedOpenings.GetOccurredAtBlinding(),
		"actor user id":    excludedOpenings.GetActorUserIdBlinding(),
	} {
		for representation, forbidden := range map[string][]byte{
			"raw":           opening,
			"lowercase hex": []byte(hex.EncodeToString(opening)),
		} {
			for location, content := range map[string][]byte{
				"entire archive":         archive,
				"event commitment proof": members["chain/event-commitments.jsonl"],
				"filter opening proof":   members["chain/filter-openings.jsonl"],
			} {
				if bytes.Contains(content, forbidden) {
					t.Fatalf("%s disclosed excluded %s blinding as %s", location, category, representation)
				}
			}
		}
	}
	verification, err := VerifyEvidenceArchive(archive)
	if err != nil || verification.EventCount != 1 {
		t.Fatalf("sequence-only selected archive verification=%#v error=%v", verification, err)
	}
}

func TestSelectedFilterOpeningsRevealOnlyReferencedCommitmentCategories(t *testing.T) {
	actorUserID := "01890f60-4d6d-7c12-8f02-6c9129d5b099"
	for _, testCase := range []struct {
		name        string
		filter      func(StoredEvent) *tammyv1.AuditEventFilter
		revealed    string
		wantValue   func(StoredEvent) string
		wantOpening func(StoredEvent) []byte
	}{
		{name: "event type", filter: func(stored StoredEvent) *tammyv1.AuditEventFilter {
			return &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{stored.Event.Type}}
		}, revealed: "event_type", wantValue: func(stored StoredEvent) string {
			return strconv.FormatInt(int64(stored.Event.Type), 10)
		}, wantOpening: func(stored StoredEvent) []byte { return stored.Event.CommitmentOpenings.EventTypeBlinding }},
		{name: "occurred at", filter: func(stored StoredEvent) *tammyv1.AuditEventFilter {
			return &tammyv1.AuditEventFilter{FromTime: timestamppb.New(stored.Event.OccurredAt.AsTime())}
		}, revealed: "occurred_at", wantValue: func(stored StoredEvent) string {
			return stored.Event.OccurredAt.AsTime().UTC().Format(time.RFC3339Nano)
		}, wantOpening: func(stored StoredEvent) []byte { return stored.Event.CommitmentOpenings.OccurredAtBlinding }},
		{name: "actor user id", filter: func(StoredEvent) *tammyv1.AuditEventFilter {
			return &tammyv1.AuditEventFilter{ActorUserId: &actorUserID}
		}, revealed: "actor_user_id", wantValue: func(StoredEvent) string { return "" },
			wantOpening: func(stored StoredEvent) []byte { return stored.Event.CommitmentOpenings.ActorUserIdBlinding }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input, _, _ := buildEvidenceArchiveFixtureInput(t)
			stored := input.Events[0]
			filter := testCase.filter(stored)
			filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
			if err != nil {
				t.Fatal(err)
			}
			input.SelectionApplied = true
			input.FilterProto = filterProto
			input.SelectedEvents = filterStoredEvents(input.Events, filter, 0)
			archive, err := BuildSignedEvidenceArchive(input)
			if err != nil {
				t.Fatal(err)
			}
			members := readArchiveMembers(t, archive)
			line := bytes.TrimSuffix(members["chain/filter-openings.jsonl"], []byte{'\n'})
			openingLine := new(structpb.Struct)
			if err := canonical.UnmarshalStrict(line, openingLine); err != nil || len(openingLine.Fields) != 3 {
				t.Fatalf("strict selective opening line = %s, error=%v", line, err)
			}
			opening := openingLine.Fields[testCase.revealed].GetStructValue()
			if opening == nil || opening.Fields["value"].GetStringValue() != testCase.wantValue(stored) ||
				opening.Fields["blinding"].GetStringValue() != hex.EncodeToString(testCase.wantOpening(stored)) {
				t.Fatalf("%s opening mismatch: %s", testCase.revealed, line)
			}
			for _, hidden := range []string{"event_type", "occurred_at", "actor_user_id"} {
				if hidden != testCase.revealed && openingLine.Fields[hidden] != nil {
					t.Fatalf("%s filter disclosed unreferenced %s opening: %s", testCase.revealed, hidden, line)
				}
			}
			verification, err := VerifyEvidenceArchive(archive)
			if err != nil || verification.EventCount != uint64(len(input.SelectedEvents)) {
				t.Fatalf("%s selected verification=%#v error=%v", testCase.revealed, verification, err)
			}
		})
	}
}

func TestCombinedSelectedFilterRevealsExactlyTypeTimeAndActorOpenings(t *testing.T) {
	input, _, _ := buildEvidenceArchiveFixtureInput(t)
	stored := input.Events[0]
	actorUserID := "01890f60-4d6d-7c12-8f02-6c9129d5b099"
	eventWithActor := proto.Clone(stored.Event).(*tammyv1.AuditEvent)
	eventWithActor.Actor = &tammyv1.AuthenticationContext{
		ActorUserId: actorUserID,
		SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b098",
	}
	stored, err := reconstructEventWithStoredOpenings(input.Header.GenesisHash, eventWithActor, stored.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	input.Events = []StoredEvent{stored}
	copy(input.Header.CurrentHead[:], stored.Event.EventHash)
	filter := &tammyv1.AuditEventFilter{
		EventTypes:  []tammyv1.AuditEventType{stored.Event.Type},
		ActorUserId: &actorUserID,
		FromTime:    timestamppb.New(stored.Event.OccurredAt.AsTime()),
	}
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	input.SelectionApplied = true
	input.FilterProto = filterProto
	input.SelectedEvents = filterStoredEvents(input.Events, filter, 0)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	members := readArchiveMembers(t, archive)
	line := bytes.TrimSuffix(members["chain/filter-openings.jsonl"], []byte{'\n'})
	openingLine := new(structpb.Struct)
	if err := canonical.UnmarshalStrict(line, openingLine); err != nil || len(openingLine.Fields) != 5 {
		t.Fatalf("combined opening line=%s error=%v", line, err)
	}
	for _, field := range []string{"sequence", "version", "event_type", "occurred_at", "actor_user_id"} {
		if openingLine.Fields[field] == nil {
			t.Fatalf("combined opening omitted %s: %s", field, line)
		}
	}
	for category, want := range map[string][]byte{
		"event_type":    stored.Event.CommitmentOpenings.EventTypeBlinding,
		"occurred_at":   stored.Event.CommitmentOpenings.OccurredAtBlinding,
		"actor_user_id": stored.Event.CommitmentOpenings.ActorUserIdBlinding,
	} {
		opening := openingLine.Fields[category].GetStructValue()
		if opening == nil || opening.Fields["blinding"].GetStringValue() != hex.EncodeToString(want) {
			t.Fatalf("combined %s proof does not duplicate its selected artifact owner: %s", category, line)
		}
	}
	verification, err := VerifyEvidenceArchive(archive)
	if err != nil || verification.EventCount != 1 {
		t.Fatalf("combined filter verification=%#v error=%v", verification, err)
	}
}

func TestVerifyEvidenceArchiveRejectsUnknownAndPartialChainControls(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name    string
		member  string
		content []byte
	}{
		{name: "unknown chain member", member: "chain/unknown.bin", content: []byte("unknown")},
		{name: "legacy filter projection", member: "chain/filter-events.jsonl", content: []byte("{}\n")},
		{name: "selected filter in full mode", member: "filter.pb", content: nil},
		{name: "selected heads in full mode", member: "chain/heads.bin", content: bytes.Repeat([]byte{0x11}, sha256.Size)},
		{name: "selected commitments in full mode", member: "chain/event-commitments.jsonl", content: append(append([]byte(nil), input.Events[0].CanonicalEvent...), '\n')},
		{name: "selected filter openings in full mode", member: "chain/filter-openings.jsonl", content: []byte("{\"sequence\":\"1\",\"version\":\"tammy.audit.filter-opening.v1\"}\n")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			members := readArchiveMembers(t, archive)
			members[testCase.member] = testCase.content
			tampered := resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
				manifest.Objects = append(manifest.Objects, &tammyv1.AuditExportObject{Path: testCase.member})
				sort.Slice(manifest.Objects, func(left, right int) bool {
					return manifest.Objects[left].Path < manifest.Objects[right].Path
				})
			})
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("archive control error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestVerifySelectedEvidenceArchiveRejectsResignedFilterOpeningTampering(t *testing.T) {
	actorUserID := "01890f60-4d6d-7c12-8f02-6c9129d5b099"
	for _, testCase := range []struct {
		name          string
		field         string
		filter        func(StoredEvent) *tammyv1.AuditEventFilter
		tamperedValue string
	}{
		{name: "event type", field: "event_type", filter: func(stored StoredEvent) *tammyv1.AuditEventFilter {
			return &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{stored.Event.Type}}
		}, tamperedValue: "3"},
		{name: "actor user id", field: "actor_user_id", filter: func(StoredEvent) *tammyv1.AuditEventFilter {
			return &tammyv1.AuditEventFilter{ActorUserId: &actorUserID}
		}, tamperedValue: actorUserID},
		{name: "occurred at", field: "occurred_at", filter: func(stored StoredEvent) *tammyv1.AuditEventFilter {
			return &tammyv1.AuditEventFilter{FromTime: timestamppb.New(stored.Event.OccurredAt.AsTime())}
		}, tamperedValue: "2026-08-04T02:03:05Z"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input, key, dek := buildEvidenceArchiveFixtureInput(t)
			filter := testCase.filter(input.Events[0])
			filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
			if err != nil {
				t.Fatal(err)
			}
			input.SelectionApplied = true
			input.FilterProto = filterProto
			input.SelectedEvents = filterStoredEvents(input.Events, filter, 0)
			archive, err := BuildSignedEvidenceArchive(input)
			if err != nil {
				t.Fatal(err)
			}
			members := readArchiveMembers(t, archive)
			proof := members["chain/filter-openings.jsonl"]
			if len(proof) == 0 || proof[len(proof)-1] != '\n' {
				t.Fatalf("invalid source proof: %q", proof)
			}
			openingLine := new(structpb.Struct)
			if err := canonical.UnmarshalStrict(proof[:len(proof)-1], openingLine); err != nil {
				t.Fatal(err)
			}
			opening := openingLine.Fields[testCase.field].GetStructValue()
			if opening == nil {
				t.Fatalf("missing %s filter opening", testCase.field)
			}
			opening.Fields["value"] = structpb.NewStringValue(testCase.tamperedValue)
			canonicalProof, err := canonical.NormalizedJSON(openingLine)
			if err != nil {
				t.Fatal(err)
			}
			members["chain/filter-openings.jsonl"] = append(canonicalProof, '\n')
			tampered := resignArchiveMembers(t, members, key, dek, nil)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("re-signed filter-opening tamper error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestVerifySelectedEvidenceArchiveRejectsMalformedFilterOpeningProofMatrix(t *testing.T) {
	actorUserID := "01890f60-4d6d-7c12-8f02-6c9129d5b099"
	buildArchive := func(t *testing.T, combined bool) ([]byte, SigningKeyRecord, []byte) {
		t.Helper()
		input, key, dek := buildEvidenceArchiveFixtureInput(t)
		filter := &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{input.Events[0].Event.Type}}
		if combined {
			filter.ActorUserId = &actorUserID
			filter.FromTime = timestamppb.New(input.Events[0].Event.OccurredAt.AsTime())
		}
		filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
		if err != nil {
			t.Fatal(err)
		}
		input.SelectionApplied = true
		input.FilterProto = filterProto
		input.SelectedEvents = filterStoredEvents(input.Events, filter, 0)
		archive, err := BuildSignedEvidenceArchive(input)
		if err != nil {
			t.Fatal(err)
		}
		return archive, key, dek
	}
	for _, testCase := range []struct {
		name     string
		combined bool
		mutate   func(*testing.T, []byte) []byte
	}{
		{name: "tampered blinding", combined: true, mutate: func(t *testing.T, proof []byte) []byte {
			line := decodeSingleFilterOpeningLine(t, proof)
			opening := line.Fields["event_type"].GetStructValue()
			blinding := opening.Fields["blinding"].GetStringValue()
			if blinding[0] == '0' {
				blinding = "1" + blinding[1:]
			} else {
				blinding = "0" + blinding[1:]
			}
			opening.Fields["blinding"] = structpb.NewStringValue(blinding)
			return canonicalFilterOpeningLine(t, line)
		}},
		{name: "swapped blindings", combined: true, mutate: func(t *testing.T, proof []byte) []byte {
			line := decodeSingleFilterOpeningLine(t, proof)
			eventType := line.Fields["event_type"].GetStructValue()
			occurredAt := line.Fields["occurred_at"].GetStructValue()
			eventType.Fields["blinding"], occurredAt.Fields["blinding"] = occurredAt.Fields["blinding"], eventType.Fields["blinding"]
			return canonicalFilterOpeningLine(t, line)
		}},
		{name: "missing required category", combined: true, mutate: func(t *testing.T, proof []byte) []byte {
			line := decodeSingleFilterOpeningLine(t, proof)
			delete(line.Fields, "actor_user_id")
			return canonicalFilterOpeningLine(t, line)
		}},
		{name: "extra unreferenced category", mutate: func(t *testing.T, proof []byte) []byte {
			line := decodeSingleFilterOpeningLine(t, proof)
			line.Fields["actor_user_id"] = commitmentOpeningValue("", bytes.Repeat([]byte{0x66}, sha256.Size))
			return canonicalFilterOpeningLine(t, line)
		}},
		{name: "noncanonical line", combined: true, mutate: func(t *testing.T, proof []byte) []byte {
			return append([]byte(" "), proof...)
		}},
		{name: "malformed line", combined: true, mutate: func(t *testing.T, _ []byte) []byte {
			return []byte("{}\n")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			archive, key, dek := buildArchive(t, testCase.combined)
			members := readArchiveMembers(t, archive)
			members["chain/filter-openings.jsonl"] = testCase.mutate(t, members["chain/filter-openings.jsonl"])
			tampered := resignArchiveMembers(t, members, key, dek, nil)
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("filter-opening matrix error=%v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func decodeSingleFilterOpeningLine(t *testing.T, proof []byte) *structpb.Struct {
	t.Helper()
	if len(proof) == 0 || proof[len(proof)-1] != '\n' || bytes.Count(proof, []byte{'\n'}) != 1 {
		t.Fatalf("expected one filter-opening line: %q", proof)
	}
	line := new(structpb.Struct)
	if err := canonical.UnmarshalStrict(proof[:len(proof)-1], line); err != nil {
		t.Fatal(err)
	}
	return line
}

func canonicalFilterOpeningLine(t *testing.T, line *structpb.Struct) []byte {
	t.Helper()
	encoded, err := canonical.NormalizedJSON(line)
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func TestVerifySelectedEvidenceArchiveRejectsReusedRevealedBlindingAcrossLines(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	first := input.Events[0]
	var previous [sha256.Size]byte
	copy(previous[:], first.Event.EventHash)
	secondEvent := proto.Clone(first.Event).(*tammyv1.AuditEvent)
	secondEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b003"
	secondEvent.Sequence = 2
	secondEvent.PreviousHash = nil
	secondEvent.EventHash = nil
	secondEvent.CommitmentOpenings = nil
	second, err := PrepareEvent(previous, secondEvent, first.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	filter := &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{first.Event.Type}}
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	input.Events = []StoredEvent{first, second}
	input.SelectionApplied = true
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	input.FilterProto = filterProto
	input.Header.CurrentSequence = 2
	copy(input.Header.CurrentHead[:], second.Event.EventHash)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	members := readArchiveMembers(t, archive)
	lines := bytes.Split(members["chain/filter-openings.jsonl"], []byte{'\n'})
	if len(lines) != 3 || len(lines[2]) != 0 {
		t.Fatalf("unexpected filter-opening lines: %q", lines)
	}
	firstLine, secondLine := new(structpb.Struct), new(structpb.Struct)
	if canonical.UnmarshalStrict(lines[0], firstLine) != nil || canonical.UnmarshalStrict(lines[1], secondLine) != nil {
		t.Fatal("decode filter-opening lines")
	}
	secondLine.Fields["event_type"].GetStructValue().Fields["blinding"] =
		firstLine.Fields["event_type"].GetStructValue().Fields["blinding"]
	lines[1], err = canonical.NormalizedJSON(secondLine)
	if err != nil {
		t.Fatal(err)
	}
	members["chain/filter-openings.jsonl"] = bytes.Join(lines, []byte{'\n'})
	tampered := resignArchiveMembers(t, members, key, dek, nil)
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("reused revealed blinding error=%v, want ErrEvidenceArchive", err)
	}
}

func TestVerifySelectedEvidenceArchiveRejectsReusedOpeningAcrossSelectedArtifacts(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	first := input.Events[0]
	var previous [sha256.Size]byte
	copy(previous[:], first.Event.EventHash)
	secondEvent := proto.Clone(first.Event).(*tammyv1.AuditEvent)
	secondEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b003"
	secondEvent.Sequence = 2
	secondEvent.OccurredAt = timestamppb.New(secondEvent.OccurredAt.AsTime().Add(time.Second))
	secondEvent.PreviousHash = nil
	secondEvent.EventHash = nil
	secondEvent.CommitmentOpenings = nil
	second, err := PrepareEvent(previous, secondEvent, first.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	input.Events = []StoredEvent{first, second}
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	input.SelectionApplied = true
	input.FilterProto = nil // Canonical sequence-only/default filter.
	input.Header.CurrentSequence = 2
	copy(input.Header.CurrentHead[:], second.Event.EventHash)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}

	reused := proto.Clone(second.Event).(*tammyv1.AuditEvent)
	reused.CommitmentOpenings.HiddenMetadataBlinding =
		append([]byte(nil), first.Event.CommitmentOpenings.HiddenMetadataBlinding...)
	reused.CommitmentOpenings.PayloadIdentityBlinding =
		append([]byte(nil), first.Event.CommitmentOpenings.PayloadIdentityBlinding...)
	reused.CommitmentOpenings.EventTypeBlinding =
		append([]byte(nil), first.Event.CommitmentOpenings.EventTypeBlinding...)
	tamperedSecond, err := reconstructEventWithStoredOpenings(previous, reused, second.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}

	members := readArchiveMembers(t, archive)
	const secondPrefix = "events/00000000000000000002/"
	members[secondPrefix+"event.pb"] = tamperedSecond.EventProto
	members["chain/heads.bin"] = append(append([]byte(nil), first.Event.EventHash...), tamperedSecond.Event.EventHash...)
	members["chain/event-commitments.jsonl"] = append(append(append(append([]byte(nil), first.CanonicalEvent...), '\n'),
		tamperedSecond.CanonicalEvent...), '\n')
	registry, err := descriptorRegistryFromArchiveMembers(members)
	if err != nil {
		t.Fatal(err)
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], tamperedSecond.Event.PayloadSchemaFingerprint)
	publicJSON, err := canonicalStoredEventJSONWithDescriptor(tamperedSecond, registry[fingerprint])
	if err != nil {
		t.Fatal(err)
	}
	eventLines := bytes.Split(members["events.jsonl"], []byte{'\n'})
	if len(eventLines) != 3 || len(eventLines[2]) != 0 {
		t.Fatalf("unexpected selected event lines: %q", eventLines)
	}
	members["events.jsonl"] = append(append(append([]byte(nil), eventLines[0]...), '\n'), publicJSON...)
	members["events.jsonl"] = append(members["events.jsonl"], '\n')
	tampered := resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
		manifest.VerifiedHead = append([]byte(nil), tamperedSecond.Event.EventHash...)
	})
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("selected archive with cross-owner opening reuse error=%v, want ErrEvidenceArchive", err)
	}
}

func TestVerifySelectedEvidenceArchiveRequiresExactControlSet(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	input.SelectionApplied = true
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"filter.pb", "chain/heads.bin", "chain/event-commitments.jsonl", "chain/filter-openings.jsonl"} {
		t.Run(missing, func(t *testing.T) {
			members := readArchiveMembers(t, archive)
			delete(members, missing)
			tampered := resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
				objects := manifest.Objects[:0]
				for _, object := range manifest.Objects {
					if object.Path != missing {
						objects = append(objects, object)
					}
				}
				manifest.Objects = objects
			})
			if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("missing selected control %s error=%v, want ErrEvidenceArchive", missing, err)
			}
		})
	}
}

func TestVerifySelectedEvidenceArchiveRejectsResignedProjectionExclusionAndArtifactOmission(t *testing.T) {
	input, key, dek := buildEvidenceArchiveFixtureInput(t)
	first := input.Events[0]
	var previous [sha256.Size]byte
	copy(previous[:], first.Event.EventHash)
	secondEvent := proto.Clone(first.Event).(*tammyv1.AuditEvent)
	secondEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b003"
	secondEvent.Sequence = 2
	secondEvent.OccurredAt = timestamppb.New(secondEvent.OccurredAt.AsTime().Add(time.Second))
	secondEvent.PreviousHash = nil
	secondEvent.EventHash = nil
	secondEvent.CommitmentOpenings = nil
	second, err := PrepareEvent(previous, secondEvent, first.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	filter := &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{first.Event.Type}}
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	input.Events = []StoredEvent{first, second}
	input.SelectionApplied = true
	input.SelectedEvents = append([]StoredEvent(nil), input.Events...)
	input.FilterProto = filterProto
	input.Header.CurrentSequence = 2
	copy(input.Header.CurrentHead[:], second.Event.EventHash)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	members := readArchiveMembers(t, archive)
	proofLines := bytes.Split(members["chain/filter-openings.jsonl"], []byte{'\n'})
	if len(proofLines) != 3 || len(proofLines[2]) != 0 {
		t.Fatalf("unexpected proof lines: %q", proofLines)
	}
	openingLine := new(structpb.Struct)
	if err := canonical.UnmarshalStrict(proofLines[1], openingLine); err != nil {
		t.Fatal(err)
	}
	openingLine.Fields["event_type"].GetStructValue().Fields["value"] =
		structpb.NewStringValue("3") // User-state events do not match the signed trust-event filter.
	proofLines[1], err = canonical.NormalizedJSON(openingLine)
	if err != nil {
		t.Fatal(err)
	}
	members["chain/filter-openings.jsonl"] = bytes.Join(proofLines, []byte{'\n'})
	const omittedPrefix = "events/00000000000000000002/"
	for name := range members {
		if strings.HasPrefix(name, omittedPrefix) {
			delete(members, name)
		}
	}
	eventLines := bytes.Split(members["events.jsonl"], []byte{'\n'})
	members["events.jsonl"] = append(append([]byte(nil), eventLines[0]...), '\n')
	tampered := resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
		objects := manifest.Objects[:0]
		for _, object := range manifest.Objects {
			if _, exists := members[object.Path]; exists {
				objects = append(objects, object)
			}
		}
		manifest.Objects = objects
	})
	if _, err := VerifyEvidenceArchive(tampered); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("re-signed projection exclusion plus artifact omission error=%v, want ErrEvidenceArchive", err)
	}
}

func TestSelectedEvidenceArchiveDoesNotDiscloseExcludedPayloadBytes(t *testing.T) {
	input, _, _ := buildEvidenceArchiveFixtureInput(t)
	first := input.Events[0]
	var previous [sha256.Size]byte
	copy(previous[:], first.Event.EventHash)
	secondEvent := proto.Clone(first.Event).(*tammyv1.AuditEvent)
	secondEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b003"
	secondEvent.Sequence = 2
	secondEvent.OccurredAt = timestamppb.New(secondEvent.OccurredAt.AsTime().Add(time.Second))
	const excludedSecret = "excluded-payload-unique-secret-7d55e55f"
	secondEvent.GetPayload().GetWorkspaceTrustEstablished().WorkspaceId = excludedSecret
	secondEvent.PreviousHash = nil
	secondEvent.EventHash = nil
	secondEvent.CommitmentOpenings = nil
	secondPayloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(secondEvent.GetPayload().GetWorkspaceTrustEstablished())
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareEvent(previous, secondEvent, secondPayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	end := uint64(1)
	filter := &tammyv1.AuditEventFilter{EndSequence: &end}
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	input.Events = []StoredEvent{first, second}
	input.SelectionApplied = true
	input.SelectedEvents = []StoredEvent{first}
	input.FilterProto = filterProto
	input.Header.CurrentSequence = 2
	copy(input.Header.CurrentHead[:], second.Event.EventHash)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(archive, []byte(excludedSecret)) || bytes.Contains(archive, second.PayloadProto) || bytes.Contains(archive, second.PayloadJSON) {
		t.Fatal("selected archive disclosed excluded payload bytes")
	}
	members := readArchiveMembers(t, archive)
	if bytes.Contains(members["chain/event-commitments.jsonl"], []byte(excludedSecret)) {
		t.Fatal("event commitment proof disclosed excluded payload content")
	}
	verification, err := VerifyEvidenceArchive(archive)
	if err != nil || verification.EventCount != 1 {
		t.Fatalf("selected archive verification=%#v error=%v", verification, err)
	}
}

func TestEvidenceArchivePathBoundaryRejectsParentTraversalDirectly(t *testing.T) {
	for _, invalid := range []string{"..", "../escape", "nested/../../escape", "nested\\escape", "/absolute", "C:/escape", "nul\x00name", "line\nbreak"} {
		if safeArchivePath(invalid) {
			t.Fatalf("unsafe archive path %q was accepted", invalid)
		}
	}
	for _, valid := range []string{"evidence/report.json", "events/00000000000000000001/event.pb"} {
		if !safeArchivePath(valid) {
			t.Fatalf("safe archive path %q was rejected", valid)
		}
	}
}

func TestVerifyEvidenceArchiveRejectsNonRegularMembers(t *testing.T) {
	members := readArchiveMembers(t, buildEvidenceArchiveFixture(t))
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, content := range members {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		if name == "events.jsonl" {
			header.SetMode(os.ModeSymlink | 0o777)
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEvidenceArchive(output.Bytes()); err == nil {
		t.Fatal("archive with a symlink member was accepted")
	}
}

func buildEvidenceArchiveFixture(t *testing.T) []byte {
	t.Helper()
	archive, _ := buildEvidenceArchiveFixtureWithKey(t)
	return archive
}

func buildEvidenceArchiveFixtureWithKey(t *testing.T) ([]byte, SigningKeyRecord) {
	t.Helper()
	input, key, _ := buildEvidenceArchiveFixtureInput(t)
	archive, err := BuildSignedEvidenceArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	return archive, key
}

func buildEvidenceArchiveFixtureInput(t *testing.T) (EvidenceArchiveInput, SigningKeyRecord, []byte) {
	t.Helper()
	// The determinism test constructs the complete fixture. Re-run it through a
	// compact equivalent here so tampering remains independent of ZIP internals.
	descriptors := testAuditDescriptorSet(t)
	fingerprint := sha256.Sum256(descriptors)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x31}, 32)
	genesis, _ := Genesis(workspaceID, salt)
	payload := &tammyv1.WorkspaceTrustEstablishedEvent{WorkspaceId: workspaceID, PriorHead: genesis[:], DestinationInstallationHash: fingerprint[:], PriorMirrorUnavailable: true}
	payloadProto, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	stored, err := PrepareEvent(genesis, &tammyv1.AuditEvent{Id: "01890f60-4d6d-7c12-8f02-6c9129d5b002", WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED, OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 2, 3, 4, 0, time.UTC)),
		Source:                   &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: 1, ContentHash: genesis[:]},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceTrustEstablished{WorkspaceTrustEstablished: payload}},
		PayloadSchemaFingerprint: fingerprint[:], CommandType: "tammy.v1.WorkspaceService.EstablishMovedWorkspaceTrust",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.EstablishMovedWorkspaceTrustResponse", DeterministicSha256: fingerprint[:], OutcomeCode: "OK"}}, payloadProto)
	if err != nil {
		t.Fatal(err)
	}
	header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis, CurrentSequence: 1, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	copy(header.CurrentHead[:], stored.Event.EventHash)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	key, _, err := GenerateSigningKey(workspaceID, dek, time.Date(2026, 8, 4, 1, 1, 0, 0, time.UTC), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	input := EvidenceArchiveInput{Header: header, Events: []StoredEvent{stored},
		DescriptorSets: map[[sha256.Size]byte][]byte{fingerprint: descriptors}, SigningKey: key,
		DEK: append([]byte(nil), dek...), CreatedAt: time.Date(2026, 8, 4, 3, 4, 5, 0, time.UTC)}
	return input, key, dek
}

func rewriteSelectedEventAndResign(t *testing.T, archive []byte, key SigningKeyRecord, dek []byte,
	mutate func(*tammyv1.AuditEvent)) []byte {
	t.Helper()
	members := readArchiveMembers(t, archive)
	const prefix = "events/00000000000000000001/"
	event := new(tammyv1.AuditEvent)
	if err := proto.Unmarshal(members[prefix+"event.pb"], event); err != nil {
		t.Fatal(err)
	}
	mutate(event)
	event.EventHash = nil
	chainView := proto.Clone(event).(*tammyv1.AuditEvent)
	chainView.PreviousHash = nil
	chainView.EventHash = nil
	chainView.Payload = nil
	chainView.PayloadSchemaFingerprint = nil
	canonicalEvent, err := canonicalEventEnvelope(chainView, members[prefix+"payload.pb"], members[prefix+"payload.json"],
		string(members[prefix+"payload.type"]), event.PayloadSchemaFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	var previous [sha256.Size]byte
	copy(previous[:], event.PreviousHash)
	eventHash, err := EventHash(previous, canonicalEvent)
	if err != nil {
		t.Fatal(err)
	}
	event.EventHash = eventHash[:]
	eventProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	members[prefix+"event.pb"] = eventProto
	members["chain/heads.bin"] = append([]byte(nil), event.EventHash...)
	registry, err := descriptorRegistryFromArchiveMembers(members)
	if err != nil {
		t.Fatal(err)
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], event.PayloadSchemaFingerprint)
	publicJSON, err := canonicalStoredEventJSONWithDescriptor(StoredEvent{Event: event, EventProto: eventProto}, registry[fingerprint])
	if err != nil {
		t.Fatal(err)
	}
	members["events.jsonl"] = append(publicJSON, '\n')

	return resignArchiveMembers(t, members, key, dek, func(manifest *tammyv1.AuditExportManifest) {
		manifest.VerifiedHead = append([]byte(nil), event.EventHash...)
	})
}

func resignArchiveMembers(t *testing.T, members map[string][]byte, key SigningKeyRecord, dek []byte,
	mutateManifest func(*tammyv1.AuditExportManifest)) []byte {
	t.Helper()
	manifest := new(tammyv1.AuditExportManifest)
	if err := canonical.UnmarshalStrict(members["manifest.json"], manifest); err != nil {
		t.Fatal(err)
	}
	if mutateManifest != nil {
		mutateManifest(manifest)
	}
	for _, object := range manifest.Objects {
		content, exists := members[object.Path]
		if !exists {
			t.Fatalf("manifest object %q missing", object.Path)
		}
		digest := sha256.Sum256(content)
		object.Sha256 = digest[:]
		object.ByteLength = uint64(len(content))
	}
	manifestJSON, err := canonical.NormalizedJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(manifestJSON)
	signature, err := SignManifestHash(key, dek, manifestHash)
	if err != nil {
		t.Fatal(err)
	}
	members["manifest.json"] = manifestJSON
	members["signature.ed25519"] = signature
	return writeRawArchive(t, members)
}

func readArchiveMembers(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	members := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		members[file.Name] = content
	}
	return members
}

func writeRawArchive(t *testing.T, members map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, content := range members {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testAuditDescriptorSet(t *testing.T) []byte {
	t.Helper()
	seen := make(map[string]*descriptorpb.FileDescriptorProto)
	var visit func(protoreflect.FileDescriptor)
	visit = func(file protoreflect.FileDescriptor) {
		name := file.Path()
		if _, exists := seen[name]; exists {
			return
		}
		imports := file.Imports()
		for index := range imports.Len() {
			visit(imports.Get(index).FileDescriptor)
		}
		seen[name] = protodesc.ToFileDescriptorProto(file)
	}
	visit(tammyv1.File_tammy_v1_audit_proto)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	set := &descriptorpb.FileDescriptorSet{File: make([]*descriptorpb.FileDescriptorProto, 0, len(names))}
	for _, name := range names {
		set.File = append(set.File, seen[name])
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func testEvolvedAuditDescriptorSet(t *testing.T, source []byte) []byte {
	t.Helper()
	set := new(descriptorpb.FileDescriptorSet)
	if err := proto.Unmarshal(source, set); err != nil {
		t.Fatal(err)
	}
	set = proto.Clone(set).(*descriptorpb.FileDescriptorSet)
	for _, file := range set.File {
		if file.GetName() != "tammy/v1/events.proto" {
			continue
		}
		for _, message := range file.MessageType {
			if message.GetName() != "WorkspaceStateChangedEvent" {
				continue
			}
			fieldNumber := int32(99)
			fieldName := "historical_note"
			jsonName := "historicalNote"
			message.Field = append(message.Field, &descriptorpb.FieldDescriptorProto{
				Name: &fieldName, JsonName: &jsonName, Number: &fieldNumber,
				Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:  descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			})
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
			if err != nil {
				t.Fatal(err)
			}
			return encoded
		}
	}
	t.Fatal("WorkspaceStateChangedEvent descriptor not found")
	return nil
}

func reverseDescriptorFiles(t *testing.T, source []byte) []byte {
	t.Helper()
	set := new(descriptorpb.FileDescriptorSet)
	if err := proto.Unmarshal(source, set); err != nil {
		t.Fatal(err)
	}
	for left, right := 0, len(set.File)-1; left < right; left, right = left+1, right-1 {
		set.File[left], set.File[right] = set.File[right], set.File[left]
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func renameHistoricalDescriptorField(t *testing.T, source []byte, name string) []byte {
	t.Helper()
	set := new(descriptorpb.FileDescriptorSet)
	if err := proto.Unmarshal(source, set); err != nil {
		t.Fatal(err)
	}
	set = proto.Clone(set).(*descriptorpb.FileDescriptorSet)
	for _, file := range set.File {
		for _, message := range file.MessageType {
			if message.GetName() != "WorkspaceStateChangedEvent" {
				continue
			}
			for _, field := range message.Field {
				if field.GetNumber() == 99 {
					field.Name = proto.String(name)
					field.JsonName = proto.String(name)
					encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
					if err != nil {
						t.Fatal(err)
					}
					return encoded
				}
			}
		}
	}
	t.Fatal("historical field 99 not found")
	return nil
}
