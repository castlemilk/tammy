package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func verifierEvent(sequence uint64, previous [sha256.Size]byte) StoredEvent {
	payload := &tammyv1.WorkspaceStateChangedEvent{
		WorkspaceId: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		FromState:   tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
		ToState:     tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
		ReasonCode:  "SIGNED_IN",
	}
	payloadProto, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	event := &tammyv1.AuditEvent{
		Id:          "01890f60-4d6d-7c12-8f02-6c9129d5b008",
		WorkspaceId: payload.WorkspaceId, Generation: 1, Sequence: sequence,
		Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 1, 2, int(sequence), 0, time.UTC)),
		Actor: &tammyv1.AuthenticationContext{
			ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
			SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
		},
		Source: &tammyv1.SourceRef{Type: "workspace", Id: payload.WorkspaceId, Revision: sequence,
			ContentHash: bytes.Repeat([]byte{byte(sequence)}, sha256.Size)},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: payload}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
		CommandType:              "tammy.v1.IdentityService.SignIn",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse",
			DeterministicSha256: bytes.Repeat([]byte{0x55}, sha256.Size), OutcomeCode: "OK"},
	}
	stored, _ := prepareEventWithBlindingSource(previous, event, payloadProto, bytes.NewReader(testCommitmentBlindingBytes(sequence)))
	return stored
}

func testCommitmentBlindingBytes(sequence uint64) []byte {
	encoded := make([]byte, 0, 5*sha256.Size)
	for category := byte(1); category <= 5; category++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("tammy-test-commitment-opening/%d/%d", sequence, category)))
		encoded = append(encoded, digest[:]...)
	}
	return encoded
}

func TestVerifyStoredChainReusesStoredCommitmentOpenings(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	stored := verifierEvent(1, genesis)
	var head [sha256.Size]byte
	copy(head[:], stored.Event.EventHash)
	header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentSequence: 1, CurrentHead: head, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	result := VerifyStoredChain(header, []StoredEvent{stored})
	if result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID || result.VerifiedThroughSequence != 1 {
		t.Fatalf("stored commitment openings were not reused during verification: %#v", result)
	}
}

func TestVerifyStoredChainRejectsCommitmentOpeningReuseAcrossEventsAndCategories(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	first := verifierEvent(1, genesis)
	var previous [sha256.Size]byte
	copy(previous[:], first.Event.EventHash)
	second := verifierEvent(2, previous)
	second.Event.CommitmentOpenings.EventTypeBlinding = append([]byte(nil), first.Event.CommitmentOpenings.HiddenMetadataBlinding...)
	second, err = reconstructEventWithStoredOpenings(previous, second.Event, second.PayloadProto)
	if err != nil {
		t.Fatal(err)
	}
	var head [sha256.Size]byte
	copy(head[:], second.Event.EventHash)
	header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentSequence: 2, CurrentHead: head, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	result := VerifyStoredChain(header, []StoredEvent{first, second})
	if result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_INVALID ||
		result.MismatchSequence == nil || *result.MismatchSequence != 2 ||
		!bytes.Equal(result.VerifiedHead, first.Event.EventHash) {
		t.Fatalf("cross-event/category commitment opening reuse was accepted: %#v", result)
	}
}

type failingOpeningAccumulator struct {
	err    error
	closed bool
}

func (accumulator *failingOpeningAccumulator) Add(uint64, [sha256.Size]byte, *tammyv1.AuditCommitmentOpenings) error {
	return accumulator.err
}

func (*failingOpeningAccumulator) FirstDuplicate() (uint64, [sha256.Size]byte, error) {
	return 0, [sha256.Size]byte{}, nil
}

func (accumulator *failingOpeningAccumulator) Close() error {
	accumulator.closed = true
	return nil
}

func TestStoredChainVerifierExposesAccumulatorTerminalErrorAndCleansUp(t *testing.T) {
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	stored := verifierEvent(1, genesis)
	var head [sha256.Size]byte
	copy(head[:], stored.Event.EventHash)
	header := ChainHeader{WorkspaceID: stored.Event.WorkspaceId, Generation: 1, ChainSalt: salt,
		GenesisHash: genesis, CurrentSequence: 1, CurrentHead: head,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	injected := errors.New("opening accumulator write failed")
	accumulator := &failingOpeningAccumulator{err: injected}
	verifier, err := newStoredChainVerifier(header, accumulator)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.AcceptPage([]StoredEvent{stored}); !errors.Is(err, ErrRepository) {
		t.Fatalf("AcceptPage error=%v, want ErrRepository", err)
	}
	if !errors.Is(verifier.TerminalError(), injected) {
		t.Fatalf("TerminalError=%v, want injected failure", verifier.TerminalError())
	}
	_ = verifier.Finish()
	if !accumulator.closed {
		t.Fatal("Finish did not close accumulator after terminal error")
	}
}

func TestNewStreamingStoredChainVerifierChecksExternalRecordBound(t *testing.T) {
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	header := ChainHeader{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentSequence: ExternalOpeningRecordLimit/5 + 1,
		CurrentHead: sha256.Sum256([]byte("bounded head")), CreatedAt: time.Now().UTC()}
	if _, err := NewStreamingStoredChainVerifier(context.Background(), header); !errors.Is(err, ErrInvalidChainInput) {
		t.Fatalf("oversized streaming verifier error=%v, want ErrInvalidChainInput", err)
	}
	header.CurrentSequence = 0
	header.CurrentHead = genesis
	verifier, err := NewStreamingStoredChainVerifier(context.Background(), header)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := verifier.openings.(*externalOpeningAccumulator); !ok {
		t.Fatalf("streaming verifier accumulator=%T", verifier.openings)
	}
	if err := verifier.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyStoredChainRejectsMissingZeroSwappedAndTamperedCommitmentOpenings(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	original := verifierEvent(1, genesis)
	var head [sha256.Size]byte
	copy(head[:], original.Event.EventHash)
	header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentSequence: 1, CurrentHead: head, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	for _, testCase := range []struct {
		name   string
		mutate func(*tammyv1.AuditEvent)
	}{
		{name: "missing", mutate: func(event *tammyv1.AuditEvent) { event.CommitmentOpenings = nil }},
		{name: "zero", mutate: func(event *tammyv1.AuditEvent) {
			event.CommitmentOpenings.HiddenMetadataBlinding = make([]byte, sha256.Size)
		}},
		{name: "swapped", mutate: func(event *tammyv1.AuditEvent) {
			event.CommitmentOpenings.HiddenMetadataBlinding, event.CommitmentOpenings.PayloadIdentityBlinding =
				event.CommitmentOpenings.PayloadIdentityBlinding, event.CommitmentOpenings.HiddenMetadataBlinding
		}},
		{name: "tampered", mutate: func(event *tammyv1.AuditEvent) {
			event.CommitmentOpenings.ActorUserIdBlinding[0] ^= 0xff
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stored := cloneStoredEvent(original)
			testCase.mutate(stored.Event)
			result := VerifyStoredChain(header, []StoredEvent{stored})
			if result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_INVALID ||
				result.MismatchSequence == nil || *result.MismatchSequence != 1 {
				t.Fatalf("%s commitment openings were accepted: %#v", testCase.name, result)
			}
		})
	}
}

func TestVerifierLocalizesFirstTamperedEvent(t *testing.T) {
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	events := make([]StoredEvent, 3)
	previous := genesis
	for index := range events {
		events[index] = verifierEvent(uint64(index+1), previous)
		copy(previous[:], events[index].Event.EventHash)
	}
	header := ChainHeader{WorkspaceID: events[0].Event.WorkspaceId, Generation: 1, ChainSalt: salt,
		GenesisHash: genesis, CurrentSequence: 3, CurrentHead: previous,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	events[1].PayloadProto[0] ^= 0x01
	result := VerifyStoredChain(header, events)
	if result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_INVALID ||
		result.MismatchSequence == nil || *result.MismatchSequence != 2 || result.VerifiedThroughSequence != 1 {
		t.Fatalf("tamper result = %#v", result)
	}
}

func TestStoredChainVerifierAcceptsBoundedPagesAndRequiresExactSnapshotTerminal(t *testing.T) {
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	events := make([]StoredEvent, 4)
	previous := genesis
	for index := range events {
		events[index] = verifierEvent(uint64(index+1), previous)
		copy(previous[:], events[index].Event.EventHash)
	}
	header := ChainHeader{WorkspaceID: events[0].Event.WorkspaceId, Generation: 1, ChainSalt: salt,
		GenesisHash: genesis, CurrentSequence: 3, CurrentHead: *(*[sha256.Size]byte)(events[2].Event.EventHash),
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}

	verifier, err := NewStoredChainVerifier(header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = verifier.Close() })
	if err := verifier.AcceptPage(events[:2]); err != nil {
		t.Fatal(err)
	}
	checkpoint := verifier.Checkpoint()
	if checkpoint.AfterSequence != 2 || !bytes.Equal(checkpoint.Head[:], events[1].Event.EventHash) {
		t.Fatalf("checkpoint=%#v", checkpoint)
	}
	if err := verifier.AcceptPage(events[2:3]); err != nil {
		t.Fatal(err)
	}
	result := verifier.Finish()
	if result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID || result.VerifiedThroughSequence != 3 {
		t.Fatalf("bounded verification=%#v", result)
	}

	withAppend, err := NewStoredChainVerifier(header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = withAppend.Close() })
	if err := withAppend.AcceptPage(events); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("post-snapshot append error=%v, want ErrInvalidEvent", err)
	}
}

func TestStoredChainVerifierRejectsGapRepeatReorderMutationAndWrongTerminal(t *testing.T) {
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	events := make([]StoredEvent, 3)
	previous := genesis
	for index := range events {
		events[index] = verifierEvent(uint64(index+1), previous)
		copy(previous[:], events[index].Event.EventHash)
	}
	header := ChainHeader{WorkspaceID: events[0].Event.WorkspaceId, Generation: 1, ChainSalt: salt,
		GenesisHash: genesis, CurrentSequence: 3, CurrentHead: previous,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	for _, testCase := range []struct {
		name   string
		pages  [][]StoredEvent
		header ChainHeader
	}{
		{name: "gap", pages: [][]StoredEvent{{events[0]}, {events[2]}}, header: header},
		{name: "repeat", pages: [][]StoredEvent{{events[0]}, {events[0]}}, header: header},
		{name: "reorder", pages: [][]StoredEvent{{events[1], events[0], events[2]}}, header: header},
		{name: "mutated", pages: [][]StoredEvent{{events[0], func() StoredEvent { event := cloneStoredEvent(events[1]); event.PayloadProto[0] ^= 1; return event }()}}, header: header},
		{name: "wrong terminal", pages: [][]StoredEvent{{events[0], events[1], events[2]}}, header: func() ChainHeader { changed := header; changed.CurrentHead[0] ^= 1; return changed }()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			verifier, err := NewStoredChainVerifier(testCase.header)
			if err != nil {
				t.Fatal(err)
			}
			defer verifier.Close()
			for _, page := range testCase.pages {
				if err := verifier.AcceptPage(page); err != nil {
					break
				}
			}
			result := verifier.Finish()
			if result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_INVALID {
				t.Fatalf("invalid stream accepted: %#v", result)
			}
		})
	}
}

func TestVerifierRegeneratesOpaqueCommitmentsAndRejectsPayloadIdentityTampering(t *testing.T) {
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	original := verifierEvent(1, genesis)
	var head [sha256.Size]byte
	copy(head[:], original.Event.EventHash)
	header := ChainHeader{
		WorkspaceID: original.Event.WorkspaceId, Generation: 1, ChainSalt: salt,
		GenesisHash: genesis, CurrentSequence: 1, CurrentHead: head,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*StoredEvent)
	}{
		{name: "stored_payload_type", mutate: func(stored *StoredEvent) {
			stored.PayloadType = "tammy.v1.UserStateChangedEvent"
		}},
		{name: "event_schema_fingerprint", mutate: func(stored *StoredEvent) {
			stored.Event.PayloadSchemaFingerprint[0] ^= 0xff
		}},
		{name: "canonical_payload_identity_commitment", mutate: func(stored *StoredEvent) {
			corruptCanonicalDigest(t, stored, `"payload_identity_commitment":"`)
		}},
		{name: "canonical_hidden_metadata_commitment", mutate: func(stored *StoredEvent) {
			corruptCanonicalDigest(t, stored, `"hidden_metadata_commitment":"`)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stored := cloneStoredEvent(original)
			testCase.mutate(&stored)
			result := VerifyStoredChain(header, []StoredEvent{stored})
			if result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_INVALID ||
				result.MismatchSequence == nil || *result.MismatchSequence != 1 || result.VerifiedThroughSequence != 0 {
				t.Fatalf("payload identity tamper result = %#v", result)
			}
		})
	}
}

func corruptCanonicalDigest(t *testing.T, stored *StoredEvent, marker string) {
	t.Helper()
	offset := bytes.Index(stored.CanonicalEvent, []byte(marker))
	if offset < 0 {
		t.Fatalf("canonical digest marker %q absent: %s", marker, stored.CanonicalEvent)
	}
	offset += len(marker)
	if stored.CanonicalEvent[offset] == '0' {
		stored.CanonicalEvent[offset] = '1'
	} else {
		stored.CanonicalEvent[offset] = '0'
	}
}

func cloneStoredEvent(original StoredEvent) StoredEvent {
	cloned := original
	cloned.Event = proto.Clone(original.Event).(*tammyv1.AuditEvent)
	cloned.PayloadProto = append([]byte(nil), original.PayloadProto...)
	cloned.PayloadJSON = append([]byte(nil), original.PayloadJSON...)
	cloned.AffectedResourcesProto = append([]byte(nil), original.AffectedResourcesProto...)
	cloned.CanonicalEvent = append([]byte(nil), original.CanonicalEvent...)
	cloned.EventProto = append([]byte(nil), original.EventProto...)
	return cloned
}
