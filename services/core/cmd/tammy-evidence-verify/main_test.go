package main

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRunVerifiesSignedArchiveWithoutDatabase(t *testing.T) {
	archive := standaloneArchiveFixture(t)
	archivePath := filepath.Join(t.TempDir(), "evidence.zip")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{archivePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("run code = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "VALID") || !strings.Contains(stdout.String(), "events=1") || stderr.Len() != 0 {
		t.Fatalf("unexpected output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	archive[len(archive)/2] ^= 0xff
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run([]string{archivePath}, &stdout, &stderr); code != 1 {
		t.Fatalf("tampered run code = %d, want 1", code)
	}
}

func TestRunRequiresExactlyOneArchivePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("run = %d, stderr=%q", code, stderr.String())
	}
}

func standaloneArchiveFixture(t *testing.T) []byte {
	t.Helper()
	descriptors := standaloneDescriptorSet(t)
	fingerprint := sha256.Sum256(descriptors)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x31}, 32)
	genesis, err := audit.Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	payload := &tammyv1.WorkspaceTrustEstablishedEvent{WorkspaceId: workspaceID, PriorHead: genesis[:], DestinationInstallationHash: fingerprint[:], PriorMirrorUnavailable: true}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := audit.PrepareEvent(genesis, &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b002", WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED, OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 2, 3, 4, 0, time.UTC)),
		Source:                   &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: 1, ContentHash: genesis[:]},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceTrustEstablished{WorkspaceTrustEstablished: payload}},
		PayloadSchemaFingerprint: fingerprint[:], CommandType: "tammy.v1.WorkspaceService.EstablishMovedWorkspaceTrust",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.EstablishMovedWorkspaceTrustResponse", DeterministicSha256: fingerprint[:], OutcomeCode: "OK"},
	}, payloadProto)
	if err != nil {
		t.Fatal(err)
	}
	header := audit.ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis, CurrentSequence: 1, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	copy(header.CurrentHead[:], stored.Event.EventHash)
	dek := bytes.Repeat([]byte{0x5a}, 32)
	key, _, err := audit.GenerateSigningKey(workspaceID, dek, time.Date(2026, 8, 4, 1, 1, 0, 0, time.UTC), bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := audit.BuildSignedEvidenceArchive(audit.EvidenceArchiveInput{Header: header, Events: []audit.StoredEvent{stored},
		DescriptorSets: map[[sha256.Size]byte][]byte{fingerprint: descriptors}, SigningKey: key, DEK: dek,
		CreatedAt: time.Date(2026, 8, 4, 3, 4, 5, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func standaloneDescriptorSet(t *testing.T) []byte {
	t.Helper()
	seen := make(map[string]*descriptorpb.FileDescriptorProto)
	var visit func(protoreflect.FileDescriptor)
	visit = func(file protoreflect.FileDescriptor) {
		if _, exists := seen[file.Path()]; exists {
			return
		}
		imports := file.Imports()
		for index := range imports.Len() {
			visit(imports.Get(index).FileDescriptor)
		}
		seen[file.Path()] = protodesc.ToFileDescriptorProto(file)
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
