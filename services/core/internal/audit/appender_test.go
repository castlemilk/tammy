package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type canonicalEventFixture struct {
	Format                   string                            `json:"format"`
	CanonicalEventVersion    string                            `json:"canonicalEventVersion"`
	StoredPayloadType        string                            `json:"storedPayloadType"`
	PredecessorHex           string                            `json:"predecessorHex"`
	HiddenMetadataCommitment canonicalHiddenMetadataCommitment `json:"hiddenMetadataCommitment"`
	PayloadCommitment        canonicalPayloadCommitment        `json:"payloadCommitment"`
	EventTypeCommitment      canonicalValueCommitment          `json:"eventTypeCommitment"`
	OccurredAtCommitment     canonicalValueCommitment          `json:"occurredAtCommitment"`
	ActorUserIDCommitment    canonicalValueCommitment          `json:"actorUserIdCommitment"`
	CanonicalEnvelopeUTF8    string                            `json:"canonicalEnvelopeUtf8"`
	ExpectedEventSHA256Hex   string                            `json:"expectedEventSha256Hex"`
	Framing                  canonicalEventFixtureFraming      `json:"framing"`
}

type canonicalHiddenMetadataCommitment struct {
	Algorithm                  string   `json:"algorithm"`
	DomainUTF8                 string   `json:"domainUtf8"`
	BlindingHex                string   `json:"blindingHex"`
	CanonicalMetadataLengthHex string   `json:"canonicalMetadataLengthHex"`
	CanonicalMetadataUTF8      string   `json:"canonicalMetadataUtf8"`
	InputOrder                 []string `json:"inputOrder"`
	ExpectedSHA256Hex          string   `json:"expectedSha256Hex"`
}

type canonicalPayloadCommitment struct {
	Algorithm                  string   `json:"algorithm"`
	DomainUTF8                 string   `json:"domainUtf8"`
	BlindingHex                string   `json:"blindingHex"`
	TypeURLLengthHex           string   `json:"typeUrlLengthHex"`
	TypeURLUTF8                string   `json:"typeUrlUtf8"`
	SchemaFingerprintLengthHex string   `json:"schemaFingerprintLengthHex"`
	SchemaFingerprintHex       string   `json:"schemaFingerprintHex"`
	PayloadProtoLengthHex      string   `json:"payloadProtoLengthHex"`
	PayloadProtoHex            string   `json:"payloadProtoHex"`
	PayloadJSONLengthHex       string   `json:"payloadJsonLengthHex"`
	PayloadJSONUTF8            string   `json:"payloadJsonUtf8"`
	InputOrder                 []string `json:"inputOrder"`
	ExpectedSHA256Hex          string   `json:"expectedSha256Hex"`
}

type canonicalValueCommitment struct {
	Algorithm         string   `json:"algorithm"`
	DomainUTF8        string   `json:"domainUtf8"`
	BlindingHex       string   `json:"blindingHex"`
	ValueLengthHex    string   `json:"valueLengthHex"`
	ValueUTF8         string   `json:"valueUtf8"`
	InputOrder        []string `json:"inputOrder"`
	ExpectedSHA256Hex string   `json:"expectedSha256Hex"`
}

type canonicalEventFixtureFraming struct {
	Algorithm               string   `json:"algorithm"`
	DomainUTF8              string   `json:"domainUtf8"`
	CanonicalLengthEncoding string   `json:"canonicalLengthEncoding"`
	CanonicalLengthHex      string   `json:"canonicalLengthHex"`
	InputOrder              []string `json:"inputOrder"`
}

func TestPrepareEventGeneratesIndependentNonzeroCommitmentOpenings(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	stored := verifierEvent(1, previous)
	openings := stored.Event.GetCommitmentOpenings()
	if openings == nil {
		t.Fatal("PrepareEvent omitted commitment openings")
	}
	seen := make(map[[sha256.Size]byte]string)
	for name, opening := range map[string][]byte{
		"hidden_metadata":  openings.HiddenMetadataBlinding,
		"payload_identity": openings.PayloadIdentityBlinding,
		"event_type":       openings.EventTypeBlinding,
		"occurred_at":      openings.OccurredAtBlinding,
		"actor_user_id":    openings.ActorUserIdBlinding,
	} {
		if len(opening) != sha256.Size || bytes.Equal(opening, make([]byte, sha256.Size)) {
			t.Fatalf("%s opening is missing or zero: %x", name, opening)
		}
		var key [sha256.Size]byte
		copy(key[:], opening)
		if prior, duplicate := seen[key]; duplicate {
			t.Fatalf("%s opening reuses %s opening", name, prior)
		}
		seen[key] = name
	}
}

func TestPrepareEventRejectsCallerSuppliedAndInvalidGeneratedOpenings(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	stored := verifierEvent(1, previous)
	rawEvent := proto.Clone(stored.Event).(*tammyv1.AuditEvent)
	rawEvent.PreviousHash = nil
	rawEvent.EventHash = nil

	if _, err := PrepareEvent(previous, rawEvent, stored.PayloadProto); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("caller-supplied openings error=%v, want ErrInvalidEvent", err)
	}
	rawEvent.CommitmentOpenings = nil
	for _, testCase := range []struct {
		name   string
		source []byte
	}{
		{name: "missing bytes", source: testCommitmentBlindingBytes(1)[:sha256.Size]},
		{name: "zero opening", source: make([]byte, 5*sha256.Size)},
		{name: "reused opening", source: bytes.Repeat([]byte{0x77}, 5*sha256.Size)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := prepareEventWithBlindingSource(previous, rawEvent, stored.PayloadProto,
				bytes.NewReader(testCase.source)); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("invalid generated openings error=%v, want ErrInvalidEvent", err)
			}
		})
	}
}

func TestBlindedCommitmentsHideCorrelationAndResistUnblindedDictionaryAttempts(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	seed := verifierEvent(1, previous)
	rawEvent := proto.Clone(seed.Event).(*tammyv1.AuditEvent)
	rawEvent.PreviousHash = nil
	rawEvent.EventHash = nil
	rawEvent.CommitmentOpenings = nil
	first, err := prepareEventWithBlindingSource(previous, rawEvent, seed.PayloadProto,
		bytes.NewReader(testCommitmentBlindingBytes(1)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepareEventWithBlindingSource(previous, rawEvent, seed.PayloadProto,
		bytes.NewReader(testCommitmentBlindingBytes(2)))
	if err != nil {
		t.Fatal(err)
	}
	var firstEnvelope, secondEnvelope map[string]json.RawMessage
	if json.Unmarshal(first.CanonicalEvent, &firstEnvelope) != nil || json.Unmarshal(second.CanonicalEvent, &secondEnvelope) != nil {
		t.Fatal("decode blinded commitment envelopes")
	}
	if !bytes.Equal(firstEnvelope["identity_projection"], secondEnvelope["identity_projection"]) {
		t.Fatal("identical event identity projections changed with blindings")
	}
	for _, category := range []string{"hidden_metadata_commitment", "payload_identity_commitment", "event_type_commitment",
		"occurred_at_commitment", "actor_user_id_commitment"} {
		if bytes.Equal(firstEnvelope[category], secondEnvelope[category]) {
			t.Fatalf("identical low-entropy events correlated through %s", category)
		}
	}
	var actualTypeCommitment string
	if err := json.Unmarshal(firstEnvelope["event_type_commitment"], &actualTypeCommitment); err != nil {
		t.Fatal(err)
	}
	zeroBlinding := make([]byte, sha256.Size)
	for eventType := range tammyv1.AuditEventType_name {
		candidate := strconv.FormatInt(int64(eventType), 10)
		attempt := independentBlindedFramedSHA256(eventTypeCommitmentDomain, zeroBlinding, []byte(candidate))
		if hex.EncodeToString(attempt[:]) == actualTypeCommitment {
			t.Fatalf("unblinded event-type dictionary candidate %s matched commitment", candidate)
		}
	}
}

func TestPayloadAndHiddenCommitmentsResistLowEntropyDictionaryEnumeration(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	stored := verifierEvent(1, previous)
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(stored.CanonicalEvent, &envelope); err != nil {
		t.Fatal(err)
	}
	var actualPayloadCommitment, actualHiddenCommitment string
	if json.Unmarshal(envelope["payload_identity_commitment"], &actualPayloadCommitment) != nil ||
		json.Unmarshal(envelope["hidden_metadata_commitment"], &actualHiddenCommitment) != nil {
		t.Fatal("decode commitment envelope")
	}
	zeroBlinding := make([]byte, sha256.Size)
	states := []tammyv1.WorkspaceState{
		tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
		tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
	}
	for _, fromState := range states {
		for _, toState := range states {
			for _, reason := range []string{"", "SIGNED_IN", "SIGNED_OUT", "SESSION_REFRESHED"} {
				candidate := &tammyv1.WorkspaceStateChangedEvent{WorkspaceId: stored.Event.WorkspaceId,
					FromState: fromState, ToState: toState, ReasonCode: reason}
				payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				payloadJSON, err := canonical.NormalizedJSON(candidate)
				if err != nil {
					t.Fatal(err)
				}
				attempt := independentBlindedFramedSHA256(payloadIdentityCommitmentDomain, zeroBlinding,
					[]byte(protobufTypeURLPrefix+stored.PayloadType), stored.Event.PayloadSchemaFingerprint, payloadProto, payloadJSON)
				if hex.EncodeToString(attempt[:]) == actualPayloadCommitment {
					t.Fatalf("zero-blind payload dictionary matched from=%s to=%s reason=%q", fromState, toState, reason)
				}
			}
		}
	}
	for _, commandType := range []string{"tammy.v1.IdentityService.SignIn", "tammy.v1.IdentityService.SignOut"} {
		candidate := proto.Clone(stored.Event).(*tammyv1.AuditEvent)
		candidate.WorkspaceId = ""
		candidate.Generation = 0
		candidate.Sequence = 0
		candidate.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED
		candidate.OccurredAt = nil
		candidate.PreviousHash = nil
		candidate.EventHash = nil
		candidate.Payload = nil
		candidate.PayloadSchemaFingerprint = nil
		candidate.CommitmentOpenings = nil
		candidate.CommandType = commandType
		candidate.Actor.ActorUserId = ""
		candidateJSON, err := canonical.NormalizedJSON(candidate)
		if err != nil {
			t.Fatal(err)
		}
		attempt := independentBlindedFramedSHA256(hiddenMetadataCommitmentDomain, zeroBlinding, candidateJSON)
		if hex.EncodeToString(attempt[:]) == actualHiddenCommitment {
			t.Fatalf("zero-blind hidden metadata dictionary matched command_type=%q", commandType)
		}
	}
}

func TestPrepareEventV3CommitsAllMetadataWithoutDisclosingIt(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	stored := verifierEvent(1, previous)
	hiddenMetadata := proto.Clone(stored.Event).(*tammyv1.AuditEvent)
	hiddenMetadata.WorkspaceId = ""
	hiddenMetadata.Generation = 0
	hiddenMetadata.Sequence = 0
	hiddenMetadata.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED
	hiddenMetadata.OccurredAt = nil
	hiddenMetadata.PreviousHash = nil
	hiddenMetadata.EventHash = nil
	hiddenMetadata.Payload = nil
	hiddenMetadata.PayloadSchemaFingerprint = nil
	hiddenMetadata.CommitmentOpenings = nil
	hiddenMetadata.Actor.ActorUserId = ""
	hiddenMetadataJSON, err := canonical.NormalizedJSON(hiddenMetadata)
	if err != nil {
		t.Fatal(err)
	}
	openings := stored.Event.GetCommitmentOpenings()
	hiddenMetadataCommitment := independentBlindedFramedSHA256("tammy-audit-hidden-metadata-commitment-v2",
		openings.HiddenMetadataBlinding, hiddenMetadataJSON)
	typeURL := protobufTypeURLPrefix + stored.PayloadType
	payloadIdentityCommitment := independentBlindedFramedSHA256("tammy-audit-payload-identity-commitment-v1",
		openings.PayloadIdentityBlinding, []byte(typeURL), stored.Event.PayloadSchemaFingerprint, stored.PayloadProto, stored.PayloadJSON)
	actorUserID := ""
	if stored.Event.Actor != nil {
		actorUserID = stored.Event.Actor.ActorUserId
	}
	eventTypeValue := strconv.FormatInt(int64(stored.Event.Type), 10)
	occurredAtValue := stored.Event.OccurredAt.AsTime().UTC().Format(time.RFC3339Nano)
	eventTypeCommitment := independentBlindedFramedSHA256("tammy-audit-event-type-commitment-v1",
		openings.EventTypeBlinding, []byte(eventTypeValue))
	occurredAtCommitment := independentBlindedFramedSHA256("tammy-audit-occurred-at-commitment-v1",
		openings.OccurredAtBlinding, []byte(occurredAtValue))
	actorUserIDCommitment := independentBlindedFramedSHA256("tammy-audit-actor-user-id-commitment-v1",
		openings.ActorUserIdBlinding, []byte(actorUserID))
	want := &structpb.Struct{Fields: map[string]*structpb.Value{
		"identity_projection": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
			"generation":   structpb.NewStringValue(strconv.FormatUint(stored.Event.Generation, 10)),
			"sequence":     structpb.NewStringValue(strconv.FormatUint(stored.Event.Sequence, 10)),
			"workspace_id": structpb.NewStringValue(stored.Event.WorkspaceId),
		}}),
		"actor_user_id_commitment":    structpb.NewStringValue(hex.EncodeToString(actorUserIDCommitment[:])),
		"event_type_commitment":       structpb.NewStringValue(hex.EncodeToString(eventTypeCommitment[:])),
		"hidden_metadata_commitment":  structpb.NewStringValue(hex.EncodeToString(hiddenMetadataCommitment[:])),
		"occurred_at_commitment":      structpb.NewStringValue(hex.EncodeToString(occurredAtCommitment[:])),
		"payload_identity_commitment": structpb.NewStringValue(hex.EncodeToString(payloadIdentityCommitment[:])),
		"version":                     structpb.NewStringValue("tammy.audit.canonical-event.v3"),
	}}
	wantCanonical, err := canonical.NormalizedJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored.CanonicalEvent, wantCanonical) {
		t.Fatalf("canonical v3 envelope mismatch\n got: %s\nwant: %s", stored.CanonicalEvent, wantCanonical)
	}
	for _, forbidden := range [][]byte{
		[]byte(`"command_type"`), []byte(stored.Event.CommandType), []byte(stored.Event.Actor.SessionId),
		[]byte(stored.Event.Result.TypeName), []byte("SIGNED_IN"), []byte(`"payload"`),
	} {
		if bytes.Contains(stored.CanonicalEvent, forbidden) {
			t.Fatalf("canonical v3 envelope disclosed %q: %s", forbidden, stored.CanonicalEvent)
		}
	}
	if bytes.Count(stored.CanonicalEvent, []byte(`"payload_identity_commitment"`)) != 1 {
		t.Fatalf("payload identity commitment not present exactly once: %s", stored.CanonicalEvent)
	}
	for _, hiddenIdentity := range []string{"schema_fingerprint", "type_url", typeURL,
		hex.EncodeToString(stored.Event.PayloadSchemaFingerprint)} {
		if bytes.Contains(stored.CanonicalEvent, []byte(hiddenIdentity)) {
			t.Fatalf("v3 envelope disclosed payload identity %q: %s", hiddenIdentity, stored.CanonicalEvent)
		}
	}
}

func independentFramedSHA256(domain string, fields ...[]byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	length := make([]byte, 8)
	for _, field := range fields {
		binary.BigEndian.PutUint64(length, uint64(len(field)))
		_, _ = digest.Write(length)
		_, _ = digest.Write(field)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func independentBlindedFramedSHA256(domain string, blinding []byte, fields ...[]byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write(blinding)
	length := make([]byte, 8)
	for _, field := range fields {
		binary.BigEndian.PutUint64(length, uint64(len(field)))
		_, _ = digest.Write(length)
		_, _ = digest.Write(field)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func TestPrepareEventV3HiddenMetadataCommitmentCoversNonProjectionFields(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	base := verifierEvent(1, previous)
	canonicalField := func(t *testing.T, canonicalEvent []byte, field string) json.RawMessage {
		t.Helper()
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(canonicalEvent, &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope[field]
	}
	baseHiddenCommitment := canonicalField(t, base.CanonicalEvent, "hidden_metadata_commitment")
	tests := []struct {
		name   string
		mutate func(*tammyv1.AuditEvent)
	}{
		{name: "command id", mutate: func(event *tammyv1.AuditEvent) {
			value := "01890f60-4d6d-7c12-8f02-6c9129d5b011"
			event.CommandId = &value
		}},
		{name: "result digest", mutate: func(event *tammyv1.AuditEvent) {
			event.Result.DeterministicSha256[0] ^= 0xff
		}},
		{name: "source id", mutate: func(event *tammyv1.AuditEvent) {
			event.Source.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b012"
		}},
		{name: "affected resource", mutate: func(event *tammyv1.AuditEvent) {
			event.AffectedResources = append(event.AffectedResources, &tammyv1.SourceRef{Type: "user",
				Id: "01890f60-4d6d-7c12-8f02-6c9129d5b013", Revision: 1, ContentHash: bytes.Repeat([]byte{0x61}, sha256.Size)})
		}},
		{name: "session id", mutate: func(event *tammyv1.AuditEvent) {
			event.Actor.SessionId = "01890f60-4d6d-7c12-8f02-6c9129d5b014"
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			event := proto.Clone(base.Event).(*tammyv1.AuditEvent)
			testCase.mutate(event)
			event.CommitmentOpenings = nil
			changed, err := prepareEventWithBlindingSource(previous, event, base.PayloadProto,
				bytes.NewReader(testCommitmentBlindingBytes(1)))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(baseHiddenCommitment, canonicalField(t, changed.CanonicalEvent, "hidden_metadata_commitment")) ||
				bytes.Equal(base.Event.EventHash, changed.Event.EventHash) {
				t.Fatalf("%s did not alter hidden metadata commitment and event hash", testCase.name)
			}
			for _, owner := range []string{"identity_projection", "payload_identity_commitment", "event_type_commitment",
				"occurred_at_commitment", "actor_user_id_commitment"} {
				if !bytes.Equal(canonicalField(t, base.CanonicalEvent, owner), canonicalField(t, changed.CanonicalEvent, owner)) {
					t.Fatalf("%s leaked into %s: %s", testCase.name, owner, changed.CanonicalEvent)
				}
			}
		})
	}
}

func TestPrepareEventV3ProjectionFieldsHaveExactlyOneCommitmentOwner(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	base := verifierEvent(1, previous)
	field := func(t *testing.T, canonicalEvent []byte, name string) json.RawMessage {
		t.Helper()
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(canonicalEvent, &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope[name]
	}
	tests := []struct {
		name          string
		changedOwners map[string]bool
		mutate        func(*tammyv1.AuditEvent) []byte
	}{
		{name: "workspace id", changedOwners: map[string]bool{"identity_projection": true}, mutate: func(event *tammyv1.AuditEvent) []byte {
			event.WorkspaceId = "01890f60-4d6d-7c12-8f02-6c9129d5b021"
			return base.PayloadProto
		}},
		{name: "generation", changedOwners: map[string]bool{"identity_projection": true}, mutate: func(event *tammyv1.AuditEvent) []byte {
			event.Generation++
			return base.PayloadProto
		}},
		{name: "sequence", changedOwners: map[string]bool{"identity_projection": true}, mutate: func(event *tammyv1.AuditEvent) []byte {
			event.Sequence++
			return base.PayloadProto
		}},
		{name: "occurred at", changedOwners: map[string]bool{"occurred_at_commitment": true}, mutate: func(event *tammyv1.AuditEvent) []byte {
			event.OccurredAt = timestamppb.New(event.OccurredAt.AsTime().Add(time.Second))
			return base.PayloadProto
		}},
		{name: "actor user id", changedOwners: map[string]bool{"actor_user_id_commitment": true}, mutate: func(event *tammyv1.AuditEvent) []byte {
			event.Actor.ActorUserId = "01890f60-4d6d-7c12-8f02-6c9129d5b022"
			return base.PayloadProto
		}},
		{name: "event type", changedOwners: map[string]bool{"event_type_commitment": true, "payload_identity_commitment": true}, mutate: func(event *tammyv1.AuditEvent) []byte {
			event.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED
			payload := &tammyv1.UserStateChangedEvent{}
			event.Payload = &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_UserStateChanged{UserStateChanged: payload}}
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			return encoded
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			event := proto.Clone(base.Event).(*tammyv1.AuditEvent)
			payloadProto := testCase.mutate(event)
			event.CommitmentOpenings = nil
			changed, err := prepareEventWithBlindingSource(previous, event, payloadProto,
				bytes.NewReader(testCommitmentBlindingBytes(1)))
			if err != nil {
				t.Fatal(err)
			}
			for _, owner := range []string{"identity_projection", "hidden_metadata_commitment", "payload_identity_commitment",
				"event_type_commitment", "occurred_at_commitment", "actor_user_id_commitment"} {
				changedOwner := !bytes.Equal(field(t, base.CanonicalEvent, owner), field(t, changed.CanonicalEvent, owner))
				if changedOwner != testCase.changedOwners[owner] {
					t.Fatalf("%s changed %s=%t, want %t", testCase.name, owner, changedOwner, testCase.changedOwners[owner])
				}
			}
		})
	}
}

func TestPrepareEventV3PayloadInputsHaveExactlyOneCommitmentOwner(t *testing.T) {
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, sha256.Size))
	base := verifierEvent(1, previous)
	field := func(t *testing.T, canonicalEvent []byte, name string) json.RawMessage {
		t.Helper()
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(canonicalEvent, &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope[name]
	}
	basePayloadCommitment := field(t, base.CanonicalEvent, "payload_identity_commitment")
	tests := []struct {
		name   string
		mutate func(*tammyv1.AuditEvent) []byte
	}{
		{name: "schema fingerprint", mutate: func(event *tammyv1.AuditEvent) []byte {
			event.PayloadSchemaFingerprint[0] ^= 0xff
			return base.PayloadProto
		}},
		{name: "payload proto and json", mutate: func(event *tammyv1.AuditEvent) []byte {
			payload := event.GetPayload().GetWorkspaceStateChanged()
			payload.ReasonCode = "SIGNED_IN_AGAIN"
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			return encoded
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			event := proto.Clone(base.Event).(*tammyv1.AuditEvent)
			payloadProto := testCase.mutate(event)
			event.CommitmentOpenings = nil
			changed, err := prepareEventWithBlindingSource(previous, event, payloadProto,
				bytes.NewReader(testCommitmentBlindingBytes(1)))
			if err != nil {
				t.Fatal(err)
			}
			for _, owner := range []string{"identity_projection", "hidden_metadata_commitment", "event_type_commitment",
				"occurred_at_commitment", "actor_user_id_commitment"} {
				if !bytes.Equal(field(t, base.CanonicalEvent, owner), field(t, changed.CanonicalEvent, owner)) {
					t.Fatalf("%s altered non-payload owner %s", testCase.name, owner)
				}
			}
			if bytes.Equal(basePayloadCommitment, field(t, changed.CanonicalEvent, "payload_identity_commitment")) {
				t.Fatalf("%s did not alter payload identity commitment", testCase.name)
			}
		})
	}
}

func TestGenesisUsesExactDomainWorkspaceAndSaltFormula(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := []byte("0123456789abcdef0123456789abcdef")
	want, err := hex.DecodeString("26fc22600c468722942d3b70b8153c5421f19f63626779da23af92601ddf59b3")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[:]) != string(want) {
		t.Fatalf("genesis = %x, want %x", got, want)
	}
}

func TestMovedTrustCapabilityRequiresExactTypePayloadWorkspacePriorHeadAndUnpositionedGeneration(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	prior := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 2, Sequence: 7,
		Head: bytes.Repeat([]byte{0x11}, sha256.Size)}
	payload := &tammyv1.WorkspaceTrustEstablishedEvent{WorkspaceId: workspaceID,
		PriorHead: append([]byte(nil), prior.Head...), DestinationInstallationHash: bytes.Repeat([]byte{0x22}, sha256.Size),
		PriorMirrorUnavailable: true}
	event := &tammyv1.AuditEvent{WorkspaceId: workspaceID,
		Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED,
		Source: &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: prior.Sequence,
			ContentHash: append([]byte(nil), prior.Head...)},
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceTrustEstablished{
			WorkspaceTrustEstablished: payload,
		}},
	}
	if !validMovedTrustEvent(prior, event) {
		t.Fatal("exact moved-trust event was rejected")
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*tammyv1.AuditEvent)
	}{
		{name: "type", mutate: func(candidate *tammyv1.AuditEvent) {
			candidate.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED
		}},
		{name: "payload", mutate: func(candidate *tammyv1.AuditEvent) {
			candidate.Payload = &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{
				WorkspaceStateChanged: &tammyv1.WorkspaceStateChangedEvent{WorkspaceId: workspaceID},
			}}
		}},
		{name: "workspace", mutate: func(candidate *tammyv1.AuditEvent) {
			candidate.WorkspaceId = "01890f60-4d6d-7c12-8f02-6c9129d5b002"
		}},
		{name: "prior_head", mutate: func(candidate *tammyv1.AuditEvent) {
			candidate.GetPayload().GetWorkspaceTrustEstablished().PriorHead = bytes.Repeat([]byte{0x33}, sha256.Size)
		}},
		{name: "generation", mutate: func(candidate *tammyv1.AuditEvent) { candidate.Generation = prior.Generation }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := proto.Clone(event).(*tammyv1.AuditEvent)
			testCase.mutate(candidate)
			if validMovedTrustEvent(prior, candidate) {
				t.Fatal("malformed moved-trust event was accepted")
			}
		})
	}
}

func TestPrepareEventRetainsTypedPayloadSchemaAndCommandMetadata(t *testing.T) {
	payload := &tammyv1.WorkspaceTrustEstablishedEvent{
		WorkspaceId:                 "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		PriorHead:                   bytes.Repeat([]byte{0x11}, sha256.Size),
		DestinationInstallationHash: bytes.Repeat([]byte{0x22}, sha256.Size),
		PriorMirrorUnavailable:      true,
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	organisationID := "01890f60-4d6d-7c12-8f02-6c9129d5b002"
	commandID := "01890f60-4d6d-7c12-8f02-6c9129d5b003"
	idempotencyKey := "01890f60-4d6d-7c12-8f02-6c9129d5b004"
	event := &tammyv1.AuditEvent{
		Id:          "01890f60-4d6d-7c12-8f02-6c9129d5b005",
		WorkspaceId: payload.WorkspaceId,
		Generation:  1,
		Sequence:    1,
		Type:        tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED,
		OccurredAt:  timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 4_000_000, time.UTC)),
		Actor: &tammyv1.AuthenticationContext{
			ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
			SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
		},
		Source: &tammyv1.SourceRef{
			Type: "workspace", Id: payload.WorkspaceId, Revision: 7,
			ContentHash: bytes.Repeat([]byte{0x33}, sha256.Size),
		},
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceTrustEstablished{
			WorkspaceTrustEstablished: payload,
		}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
		OrganisationId:           &organisationID,
		CommandId:                &commandID,
		CommandType:              "tammy.v1.WorkspaceService.EstablishMovedWorkspaceTrust",
		IdempotencyKey:           &idempotencyKey,
		AffectedResources: []*tammyv1.SourceRef{{
			Type: "workspace", Id: payload.WorkspaceId, Revision: 7,
			ContentHash: bytes.Repeat([]byte{0x55}, sha256.Size),
		}},
		BeforeSemanticHash: bytes.Repeat([]byte{0x66}, sha256.Size),
		AfterSemanticHash:  bytes.Repeat([]byte{0x77}, sha256.Size),
		Result: &tammyv1.AuditResultMetadata{
			TypeName:            "tammy.v1.EstablishMovedWorkspaceTrustResponse",
			DeterministicSha256: bytes.Repeat([]byte{0x88}, sha256.Size),
			OutcomeCode:         "OK",
		},
	}
	var previous [sha256.Size]byte
	copy(previous[:], bytes.Repeat([]byte{0x99}, sha256.Size))
	stored, err := prepareEventWithBlindingSource(previous, event, payloadProto,
		bytes.NewReader(testCommitmentBlindingBytes(1)))
	if err != nil {
		t.Fatal(err)
	}
	if stored.PayloadType != "tammy.v1.WorkspaceTrustEstablishedEvent" {
		t.Fatalf("payload type = %q", stored.PayloadType)
	}
	if !bytes.Equal(stored.PayloadProto, payloadProto) {
		t.Fatalf("payload bytes changed\nwant %x\n got %x", payloadProto, stored.PayloadProto)
	}
	wantPayloadJSON := `{"destination_installation_hash":"IiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiI=","prior_head":"ERERERERERERERERERERERERERERERERERERERERERE=","prior_mirror_unavailable":true,"workspace_id":"01890f60-4d6d-7c12-8f02-6c9129d5b001"}`
	if string(stored.PayloadJSON) != wantPayloadJSON {
		t.Fatalf("payload JSON = %s, want %s", stored.PayloadJSON, wantPayloadJSON)
	}
	if !bytes.Equal(stored.Event.PayloadSchemaFingerprint, bytes.Repeat([]byte{0x44}, sha256.Size)) ||
		stored.Event.Actor.SessionId != event.Actor.SessionId || stored.Event.Source.Revision != 7 ||
		stored.Event.Result.TypeName != event.Result.TypeName || stored.Event.GetIdempotencyKey() != idempotencyKey {
		t.Fatalf("event metadata was not retained: %#v", stored.Event)
	}
	if !bytes.Equal(stored.Event.PreviousHash, previous[:]) || len(stored.Event.EventHash) != sha256.Size {
		t.Fatalf("event hashes not assigned: previous=%x event=%x", stored.Event.PreviousHash, stored.Event.EventHash)
	}
	if bytes.Contains(stored.CanonicalEvent, []byte("password")) || bytes.Contains(stored.CanonicalEvent, payloadProto) {
		t.Fatal("canonical event leaked a secret field or embedded raw binary payload")
	}
}

func TestPrepareEventCanonicalEnvelopeUsesOpaqueCommitmentsExactlyOnce(t *testing.T) {
	payload := &tammyv1.WorkspaceStateChangedEvent{
		WorkspaceId: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		FromState:   tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
		ToState:     tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
		ReasonCode:  "SIGNED_IN",
	}
	event := &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b008", WorkspaceId: payload.WorkspaceId,
		Generation: 1, Sequence: 1, Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt:               timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)),
		Source:                   &tammyv1.SourceRef{Type: "workspace", Id: payload.WorkspaceId, Revision: 1, ContentHash: bytes.Repeat([]byte{0x31}, sha256.Size)},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: payload}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), CommandType: "tammy.v1.IdentityService.SignIn",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse", DeterministicSha256: bytes.Repeat([]byte{0x55}, sha256.Size), OutcomeCode: "OK"},
	}
	stored, err := PrepareEvent([sha256.Size]byte{}, event, nil)
	if err != nil {
		t.Fatal(err)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(stored.CanonicalEvent, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 7 || string(envelope["version"]) != `"tammy.audit.canonical-event.v3"` {
		t.Fatalf("canonical envelope fields/version = %s", stored.CanonicalEvent)
	}
	var projection map[string]json.RawMessage
	if err := json.Unmarshal(envelope["identity_projection"], &projection); err != nil || len(projection) != 3 {
		t.Fatalf("decode identity projection: %v (%s)", err, envelope["identity_projection"])
	}
	for _, field := range []string{"generation", "sequence", "workspace_id"} {
		if _, ok := projection[field]; !ok {
			t.Fatalf("identity projection omitted %q: %s", field, envelope["identity_projection"])
		}
	}
	for _, field := range []string{"hidden_metadata_commitment", "payload_identity_commitment", "event_type_commitment",
		"occurred_at_commitment", "actor_user_id_commitment"} {
		var digest string
		if err := json.Unmarshal(envelope[field], &digest); err != nil || !canonicalDigestHex(digest) {
			t.Fatalf("opaque commitment %q invalid: %s", field, envelope[field])
		}
	}
	for _, forbidden := range []string{`"event_body":`, `"payload_identity":`, "type_url", "schema_fingerprint",
		"filter_projection", "event_type\":", "occurred_at\":", "actor_user_id\":",
		"type.googleapis.com/tammy.v1.WorkspaceStateChangedEvent", "SIGNED_IN"} {
		if bytes.Contains(stored.CanonicalEvent, []byte(forbidden)) {
			t.Fatalf("canonical envelope disclosed %q: %s", forbidden, stored.CanonicalEvent)
		}
	}
}

func TestPrepareEventRejectsPayloadThatContradictsEventType(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	event := &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b008", WorkspaceId: workspaceID,
		Generation: 1, Sequence: 1, Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)),
		Source:     &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: 1, ContentHash: bytes.Repeat([]byte{0x31}, sha256.Size)},
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_UserStateChanged{
			UserStateChanged: &tammyv1.UserStateChangedEvent{},
		}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), CommandType: "tammy.v1.IdentityService.SignIn",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse", DeterministicSha256: bytes.Repeat([]byte{0x55}, sha256.Size), OutcomeCode: "OK"},
	}
	if _, err := PrepareEvent([sha256.Size]byte{}, event, nil); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("contradictory event type/payload error = %v, want ErrInvalidEvent", err)
	}
}

func TestSelectedPayloadDefinesClosedMappingForEveryEventType(t *testing.T) {
	testCases := []struct {
		eventType   tammyv1.AuditEventType
		payload     *tammyv1.AuditEventPayload
		payloadType string
	}{
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: &tammyv1.WorkspaceStateChangedEvent{}}}, "tammy.v1.WorkspaceStateChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceTrustEstablished{WorkspaceTrustEstablished: &tammyv1.WorkspaceTrustEstablishedEvent{}}}, "tammy.v1.WorkspaceTrustEstablishedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_UserStateChanged{UserStateChanged: &tammyv1.UserStateChangedEvent{}}}, "tammy.v1.UserStateChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_FACTOR_STATE_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_FactorStateChanged{FactorStateChanged: &tammyv1.FactorStateChangedEvent{}}}, "tammy.v1.FactorStateChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_ORGANISATION_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_OrganisationChanged{OrganisationChanged: &tammyv1.OrganisationChangedEvent{}}}, "tammy.v1.OrganisationChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_ENTITY_VERIFICATION_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_EntityVerificationChanged{EntityVerificationChanged: &tammyv1.EntityVerificationChangedEvent{}}}, "tammy.v1.EntityVerificationChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_ACCOUNT_STATUS_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_AccountStatusChanged{AccountStatusChanged: &tammyv1.AccountStatusChangedEvent{}}}, "tammy.v1.AccountStatusChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_OPENING_CONVERSION_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_OpeningConversionChanged{OpeningConversionChanged: &tammyv1.OpeningConversionChangedEvent{}}}, "tammy.v1.OpeningConversionChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_JOURNAL_POSTED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_JournalPosted{JournalPosted: &tammyv1.JournalPostedEvent{}}}, "tammy.v1.JournalPostedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PERIOD_STATE_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_PeriodStateChanged{PeriodStateChanged: &tammyv1.PeriodStateChangedEvent{}}}, "tammy.v1.PeriodStateChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_BACKUP_JOB_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_BackupJobChanged{BackupJobChanged: &tammyv1.BackupJobChangedEvent{}}}, "tammy.v1.BackupJobChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_RESTORE_STATE_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_RestoreStateChanged{RestoreStateChanged: &tammyv1.RestoreStateChangedEvent{}}}, "tammy.v1.RestoreStateChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PRE_RESTORE_ARCHIVE_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_PreRestoreArchiveChanged{PreRestoreArchiveChanged: &tammyv1.PreRestoreArchiveChangedEvent{}}}, "tammy.v1.PreRestoreArchiveChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_EVIDENCE_EXPORT_CHANGED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_EvidenceExportChanged{EvidenceExportChanged: &tammyv1.EvidenceExportChangedEvent{}}}, "tammy.v1.EvidenceExportChangedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_SigningKeyRotated{SigningKeyRotated: &tammyv1.SigningKeyRotatedEvent{}}}, "tammy.v1.SigningKeyRotatedEvent"},
		{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED, &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceRestored{WorkspaceRestored: &tammyv1.WorkspaceRestoredEvent{}}}, "tammy.v1.WorkspaceRestoredEvent"},
	}
	if len(tammyv1.AuditEventType_name) != len(testCases)+1 {
		t.Fatalf("defined audit event enums = %d, mapped payloads = %d", len(tammyv1.AuditEventType_name)-1, len(testCases))
	}
	for _, testCase := range testCases {
		t.Run(testCase.eventType.String(), func(t *testing.T) {
			_, payloadType, err := selectedPayload(testCase.eventType, testCase.payload)
			if err != nil || payloadType != testCase.payloadType {
				t.Fatalf("selected payload type = %q, %v; want %q", payloadType, err, testCase.payloadType)
			}
		})
	}
}

func TestPrepareEventPayloadTypeAndFingerprintChangeCanonicalIdentityAndHash(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	base := &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b008", WorkspaceId: workspaceID,
		Generation: 1, Sequence: 1, Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)),
		Source:     &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: 1, ContentHash: bytes.Repeat([]byte{0x31}, sha256.Size)},
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{
			WorkspaceStateChanged: &tammyv1.WorkspaceStateChangedEvent{},
		}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), CommandType: "tammy.v1.IdentityService.SignIn",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse", DeterministicSha256: bytes.Repeat([]byte{0x55}, sha256.Size), OutcomeCode: "OK"},
	}
	differentType := proto.Clone(base).(*tammyv1.AuditEvent)
	differentType.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED
	differentType.Payload = &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_UserStateChanged{
		UserStateChanged: &tammyv1.UserStateChangedEvent{},
	}}
	differentFingerprint := proto.Clone(base).(*tammyv1.AuditEvent)
	differentFingerprint.PayloadSchemaFingerprint[0] ^= 0xff

	prepared := make([]StoredEvent, 3)
	for index, candidate := range []*tammyv1.AuditEvent{base, differentType, differentFingerprint} {
		var err error
		prepared[index], err = prepareEventWithBlindingSource([sha256.Size]byte{}, candidate, nil,
			bytes.NewReader(testCommitmentBlindingBytes(1)))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(prepared[0].PayloadJSON, prepared[1].PayloadJSON) || string(prepared[0].PayloadJSON) != `{}` {
		t.Fatalf("test payload JSON differs: %s != %s", prepared[0].PayloadJSON, prepared[1].PayloadJSON)
	}
	payloadCommitment := func(canonicalEvent []byte) string {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(canonicalEvent, &envelope); err != nil {
			t.Fatal(err)
		}
		var digest string
		if err := json.Unmarshal(envelope["payload_identity_commitment"], &digest); err != nil {
			t.Fatal(err)
		}
		return digest
	}
	if bytes.Equal(prepared[0].CanonicalEvent, prepared[1].CanonicalEvent) || bytes.Equal(prepared[0].Event.EventHash, prepared[1].Event.EventHash) ||
		payloadCommitment(prepared[0].CanonicalEvent) == payloadCommitment(prepared[1].CanonicalEvent) {
		t.Fatal("fully-qualified payload type URL was not bound into the opaque payload commitment and event hash")
	}
	if bytes.Equal(prepared[0].CanonicalEvent, prepared[2].CanonicalEvent) || bytes.Equal(prepared[0].Event.EventHash, prepared[2].Event.EventHash) ||
		payloadCommitment(prepared[0].CanonicalEvent) == payloadCommitment(prepared[2].CanonicalEvent) {
		t.Fatal("payload schema fingerprint was not bound into the opaque payload commitment and event hash")
	}
	for _, stored := range prepared {
		if bytes.Contains(stored.CanonicalEvent, []byte("type.googleapis.com/")) ||
			bytes.Contains(stored.CanonicalEvent, []byte(hex.EncodeToString(stored.Event.PayloadSchemaFingerprint))) {
			t.Fatalf("opaque payload identity was disclosed: %s", stored.CanonicalEvent)
		}
	}
}

func TestPrepareEventRejectsMalformedMetadataBeforePersistence(t *testing.T) {
	payloadMessage := &tammyv1.WorkspaceStateChangedEvent{WorkspaceId: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		FromState: tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED, ToState: tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
		ReasonCode: "SIGNED_IN"}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(payloadMessage)
	if err != nil {
		t.Fatal(err)
	}
	event := &tammyv1.AuditEvent{Id: "01890f60-4d6d-7c12-8f02-6c9129d5b008", WorkspaceId: payloadMessage.WorkspaceId,
		Generation: 1, Sequence: 1, Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt:               timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)),
		Source:                   &tammyv1.SourceRef{Type: "workspace", Id: payloadMessage.WorkspaceId, Revision: 1, ContentHash: bytes.Repeat([]byte{0x31}, sha256.Size)},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: payloadMessage}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), CommandType: "tammy.v1.IdentityService.SignIn",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse", DeterministicSha256: bytes.Repeat([]byte{0x55}, sha256.Size), OutcomeCode: "OK"}}
	event.Source.Id = "../not-a-resource"
	if _, err := PrepareEvent([sha256.Size]byte{}, event, payload); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("malformed source error=%v, want ErrInvalidEvent", err)
	}
}

func TestEventHashUsesPredecessorAndBigEndianCanonicalLength(t *testing.T) {
	var previous [sha256.Size]byte
	copy(previous[:], []byte("PPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP"))
	canonical := []byte(`{"a":1}`)
	want, err := hex.DecodeString("7a375f022d5ff8ca801cff9cb664abbe82e6a13d7b520f5191c2835759167783")
	if err != nil {
		t.Fatal(err)
	}
	got, err := EventHash(previous, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[:]) != string(want) {
		t.Fatalf("event hash = %x, want %x", got, want)
	}
	changedLengthBytes, err := EventHash(previous, append(canonical, ' '))
	if err != nil {
		t.Fatal(err)
	}
	if changedLengthBytes == got {
		t.Fatal("canonical length/content change did not change event hash")
	}
	previous[0] ^= 0xff
	changedPredecessor, err := EventHash(previous, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if changedPredecessor == got {
		t.Fatal("predecessor change did not change event hash")
	}
}

func TestPayloadDescriptorSecretGuardRecursesThroughMessagesListsMapsAndOneofs(t *testing.T) {
	if !payloadDescriptorContainsForbiddenSecret((&tammyv1.ChangePassphraseRequest{}).ProtoReflect().Descriptor()) {
		t.Fatal("nested SecretInput was not detected")
	}
	if payloadDescriptorContainsForbiddenSecret((&tammyv1.AuditEventPayload{}).ProtoReflect().Descriptor()) {
		t.Fatal("closed audit payload union reaches a forbidden secret message")
	}
}

func TestPrepareEventMatchesIndependentCanonicalAndBinaryGoldens(t *testing.T) {
	payload := &tammyv1.WorkspaceStateChangedEvent{WorkspaceId: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		FromState: tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
		ToState:   tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED, ReasonCode: "SIGNED_IN"}
	payloadProto, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	event := &tammyv1.AuditEvent{Id: "01890f60-4d6d-7c12-8f02-6c9129d5b008", WorkspaceId: payload.WorkspaceId,
		Generation: 1, Sequence: 1, Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt:               timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)),
		Source:                   &tammyv1.SourceRef{Type: "workspace", Id: payload.WorkspaceId, Revision: 1, ContentHash: bytes.Repeat([]byte{0x31}, 32)},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: payload}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, 32), CommandType: "tammy.v1.IdentityService.SignIn",
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse",
			DeterministicSha256: bytes.Repeat([]byte{0x55}, 32), OutcomeCode: "OK"}}
	previous := [sha256.Size]byte{}
	copy(previous[:], bytes.Repeat([]byte{0x22}, 32))
	stored, err := prepareEventWithBlindingSource(previous, event, payloadProto,
		bytes.NewReader(testCommitmentBlindingBytes(1)))
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical := `{"actor_user_id_commitment":"f33f8f9c7d8fa8bdcbceedb078b26518318ace94bad3b4895ae580168c18cf5a","event_type_commitment":"d886f7c6425d6742657561a69a7fe9290866a5005338a432e27a062080f5ab31","hidden_metadata_commitment":"32f00eafa904abdafe54756ad7023e63130c63106a7b0f0088491d6fab6b614e","identity_projection":{"generation":"1","sequence":"1","workspace_id":"01890f60-4d6d-7c12-8f02-6c9129d5b001"},"occurred_at_commitment":"2feafec2314f004dba9a25ad9865d931361cb80de72da7b2c405027ba78e8b3c","payload_identity_commitment":"288ca1bf7114a415115141f8b90d0e7aa4560a008003ba735b72c8a91e8c31f7","version":"tammy.audit.canonical-event.v3"}`
	wantEventProtoHex := `0a2430313839306636302d346436642d376331322d386630322d366339313239643562303038122430313839306636302d346436642d376331322d386630322d3663393132396435623030311801200128013206088bf4c4d30642550a09776f726b7370616365122430313839306636302d346436642d376331322d386630322d3663393132396435623030311801222031313131313131313131313131313131313131313131313131313131313131314a370a350a2430313839306636302d346436642d376331322d386630322d3663393132396435623030311003180422095349474e45445f494e522044444444444444444444444444444444444444444444444444444444444444445a202222222222222222222222222222222222222222222222222222222222222222622082bd5be9c5a4a202662bdff0e7fe69cffef887fb1f4795105ffd6e95b11e732a7a1f74616d6d792e76312e4964656e74697479536572766963652e5369676e496ea2013f0a1774616d6d792e76312e5369676e496e526573706f6e7365122055555555555555555555555555555555555555555555555555555555555555551a024f4baa01aa010a20988933e0caab30d03dcce01100aec8fbab608f72a2d1cd5e128a476976c6b6191220938441c4cf1d742bcb0fe8d20252c214b9b4f562620a2f575dac40f9c79ed7701a20b0ac0c13c76e3ac218f5956014b2b7f1ef4b373a68079957fa1ac51a5bd6713622208d11609322b0f4370d6347f717efa189c61c1ee9d2f7311ae005f0bb090937c52a20f0c425623fc492b913e81a40679305bccd71bb4e8ddd06371a07d52507218387`
	wantEventProto, _ := hex.DecodeString(wantEventProtoHex)
	if string(stored.CanonicalEvent) != wantCanonical {
		t.Fatalf("canonical golden mismatch\n%s", stored.CanonicalEvent)
	}
	if !bytes.Equal(stored.EventProto, wantEventProto) {
		t.Fatalf("event proto golden mismatch\n%x", stored.EventProto)
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve audit fixture path")
	}
	fixtureBytes, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "../../../../test/fixtures/audit/canonical-event-v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture canonicalEventFixture
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	lengthHex := func(value []byte) string {
		encoded := make([]byte, 8)
		binary.BigEndian.PutUint64(encoded, uint64(len(value)))
		return hex.EncodeToString(encoded)
	}
	hiddenMetadata := proto.Clone(stored.Event).(*tammyv1.AuditEvent)
	hiddenMetadata.WorkspaceId = ""
	hiddenMetadata.Generation = 0
	hiddenMetadata.Sequence = 0
	hiddenMetadata.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED
	hiddenMetadata.OccurredAt = nil
	hiddenMetadata.PreviousHash = nil
	hiddenMetadata.EventHash = nil
	hiddenMetadata.Payload = nil
	hiddenMetadata.PayloadSchemaFingerprint = nil
	hiddenMetadata.CommitmentOpenings = nil
	if hiddenMetadata.Actor != nil {
		hiddenMetadata.Actor.ActorUserId = ""
	}
	hiddenMetadataJSON, err := canonical.NormalizedJSON(hiddenMetadata)
	if err != nil {
		t.Fatal(err)
	}
	hiddenCommitment := fixture.HiddenMetadataCommitment
	hiddenBlinding, hiddenBlindingErr := hex.DecodeString(hiddenCommitment.BlindingHex)
	hiddenDigest := independentBlindedFramedSHA256(hiddenCommitment.DomainUTF8, hiddenBlinding, hiddenMetadataJSON)
	if hiddenBlindingErr != nil || !bytes.Equal(hiddenBlinding, stored.Event.CommitmentOpenings.HiddenMetadataBlinding) ||
		hiddenCommitment.Algorithm != "SHA-256" || hiddenCommitment.DomainUTF8 != hiddenMetadataCommitmentDomain ||
		hiddenCommitment.CanonicalMetadataLengthHex != lengthHex(hiddenMetadataJSON) ||
		hiddenCommitment.CanonicalMetadataUTF8 != string(hiddenMetadataJSON) ||
		!equalStringSequence(hiddenCommitment.InputOrder, []string{"domain_utf8", "blinding_exact_32_bytes", "canonical_metadata_length_uint64_be", "canonical_metadata_utf8"}) ||
		hiddenCommitment.ExpectedSHA256Hex != hex.EncodeToString(hiddenDigest[:]) {
		t.Fatalf("language-neutral hidden metadata commitment mismatch: %#v", hiddenCommitment)
	}
	payloadCommitment := fixture.PayloadCommitment
	fixtureSchemaFingerprint, schemaErr := hex.DecodeString(payloadCommitment.SchemaFingerprintHex)
	fixturePayloadProto, payloadErr := hex.DecodeString(payloadCommitment.PayloadProtoHex)
	payloadBlinding, payloadBlindingErr := hex.DecodeString(payloadCommitment.BlindingHex)
	payloadDigest := independentBlindedFramedSHA256(payloadCommitment.DomainUTF8, payloadBlinding, []byte(payloadCommitment.TypeURLUTF8),
		fixtureSchemaFingerprint, fixturePayloadProto, []byte(payloadCommitment.PayloadJSONUTF8))
	if schemaErr != nil || payloadErr != nil || payloadBlindingErr != nil ||
		!bytes.Equal(payloadBlinding, stored.Event.CommitmentOpenings.PayloadIdentityBlinding) || payloadCommitment.Algorithm != "SHA-256" ||
		payloadCommitment.DomainUTF8 != payloadIdentityCommitmentDomain ||
		payloadCommitment.TypeURLUTF8 != protobufTypeURLPrefix+stored.PayloadType ||
		payloadCommitment.TypeURLLengthHex != lengthHex([]byte(payloadCommitment.TypeURLUTF8)) ||
		payloadCommitment.SchemaFingerprintLengthHex != lengthHex(fixtureSchemaFingerprint) ||
		!bytes.Equal(fixtureSchemaFingerprint, stored.Event.PayloadSchemaFingerprint) ||
		payloadCommitment.PayloadProtoLengthHex != lengthHex(fixturePayloadProto) || !bytes.Equal(fixturePayloadProto, stored.PayloadProto) ||
		payloadCommitment.PayloadJSONLengthHex != lengthHex([]byte(payloadCommitment.PayloadJSONUTF8)) || payloadCommitment.PayloadJSONUTF8 != string(stored.PayloadJSON) ||
		!equalStringSequence(payloadCommitment.InputOrder, []string{"domain_utf8", "blinding_exact_32_bytes", "type_url_length_uint64_be", "type_url_utf8",
			"schema_fingerprint_length_uint64_be", "schema_fingerprint_exact_32_bytes", "payload_proto_length_uint64_be",
			"payload_proto_exact_bytes", "payload_json_length_uint64_be", "payload_json_canonical_utf8"}) ||
		payloadCommitment.ExpectedSHA256Hex != hex.EncodeToString(payloadDigest[:]) {
		t.Fatalf("language-neutral payload commitment mismatch: %#v", payloadCommitment)
	}
	validateValueCommitment := func(commitment canonicalValueCommitment, domain string, storedBlinding []byte) {
		t.Helper()
		blinding, err := hex.DecodeString(commitment.BlindingHex)
		digest := independentBlindedFramedSHA256(commitment.DomainUTF8, blinding, []byte(commitment.ValueUTF8))
		if err != nil || commitment.Algorithm != "SHA-256" || commitment.DomainUTF8 != domain ||
			!bytes.Equal(blinding, storedBlinding) || commitment.ValueLengthHex != lengthHex([]byte(commitment.ValueUTF8)) ||
			!equalStringSequence(commitment.InputOrder, []string{"domain_utf8", "blinding_exact_32_bytes", "value_length_uint64_be", "value_utf8"}) ||
			commitment.ExpectedSHA256Hex != hex.EncodeToString(digest[:]) {
			t.Fatalf("language-neutral value commitment mismatch: %#v", commitment)
		}
	}
	validateValueCommitment(fixture.EventTypeCommitment, eventTypeCommitmentDomain, stored.Event.CommitmentOpenings.EventTypeBlinding)
	validateValueCommitment(fixture.OccurredAtCommitment, occurredAtCommitmentDomain, stored.Event.CommitmentOpenings.OccurredAtBlinding)
	validateValueCommitment(fixture.ActorUserIDCommitment, actorUserIDCommitmentDomain, stored.Event.CommitmentOpenings.ActorUserIdBlinding)
	var fixtureEnvelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixture.CanonicalEnvelopeUTF8), &fixtureEnvelope); err != nil {
		t.Fatal(err)
	}
	for field, expected := range map[string]string{
		"hidden_metadata_commitment":  hiddenCommitment.ExpectedSHA256Hex,
		"payload_identity_commitment": payloadCommitment.ExpectedSHA256Hex,
		"event_type_commitment":       fixture.EventTypeCommitment.ExpectedSHA256Hex,
		"occurred_at_commitment":      fixture.OccurredAtCommitment.ExpectedSHA256Hex,
		"actor_user_id_commitment":    fixture.ActorUserIDCommitment.ExpectedSHA256Hex,
	} {
		var actual string
		if json.Unmarshal(fixtureEnvelope[field], &actual) != nil || actual != expected {
			t.Fatalf("fixture envelope %s does not match independently framed commitment: %s", field, fixture.CanonicalEnvelopeUTF8)
		}
	}
	fixtureLengthBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(fixtureLengthBytes, uint64(len([]byte(fixture.CanonicalEnvelopeUTF8))))
	if fixture.Format != "tammy.audit.canonical-event-fixture.v3" || fixture.CanonicalEventVersion != canonicalEventVersion ||
		fixture.StoredPayloadType != stored.PayloadType || fixture.PredecessorHex != hex.EncodeToString(previous[:]) ||
		fixture.CanonicalEnvelopeUTF8 != string(stored.CanonicalEvent) || fixture.Framing.Algorithm != "SHA-256" ||
		fixture.Framing.DomainUTF8 != eventDomain || fixture.Framing.CanonicalLengthEncoding != "uint64-big-endian" ||
		fixture.Framing.CanonicalLengthHex != hex.EncodeToString(fixtureLengthBytes) || len(fixture.Framing.InputOrder) != 4 ||
		fixture.Framing.InputOrder[0] != "domain_utf8" || fixture.Framing.InputOrder[1] != "predecessor_32_bytes" ||
		fixture.Framing.InputOrder[2] != "canonical_length_uint64_be" || fixture.Framing.InputOrder[3] != "canonical_envelope_utf8" ||
		fixture.ExpectedEventSHA256Hex != hex.EncodeToString(stored.Event.EventHash) {
		t.Fatalf("language-neutral canonical event fixture mismatch: %#v", fixture)
	}
	fixturePredecessorBytes, err := hex.DecodeString(fixture.PredecessorHex)
	if err != nil || len(fixturePredecessorBytes) != sha256.Size {
		t.Fatalf("fixture predecessor: %x, %v", fixturePredecessorBytes, err)
	}
	var fixturePredecessor [sha256.Size]byte
	copy(fixturePredecessor[:], fixturePredecessorBytes)
	fixtureDigest := sha256.New()
	_, _ = fixtureDigest.Write([]byte(fixture.Framing.DomainUTF8))
	_, _ = fixtureDigest.Write(fixturePredecessor[:])
	_, _ = fixtureDigest.Write(fixtureLengthBytes)
	_, _ = fixtureDigest.Write([]byte(fixture.CanonicalEnvelopeUTF8))
	fixtureHash := fixtureDigest.Sum(nil)
	if hex.EncodeToString(fixtureHash) != fixture.ExpectedEventSHA256Hex || !bytes.Equal(fixtureHash, stored.Event.EventHash) {
		t.Fatalf("independently framed fixture event hash = %x", fixtureHash)
	}
}

func equalStringSequence(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
