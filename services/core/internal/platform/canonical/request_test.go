package canonical_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type semanticHashFixtureDocument struct {
	SemanticHashCases []semanticHashFixtureCase `json:"semanticHashCases"`
}

type semanticHashFixtureCase struct {
	Name                        string          `json:"name"`
	Input                       json.RawMessage `json:"input"`
	ExpectedCanonicalJSON       string          `json:"expectedCanonicalJson"`
	ExpectedMessageType         string          `json:"expectedMessageType"`
	ExpectedSemanticHashHex     string          `json:"expectedSemanticHashHex"`
	ExpectedSemanticHashVersion string          `json:"expectedSemanticHashVersion"`
}

func boolPointer(value bool) *bool       { return &value }
func int32Pointer(value int32) *int32    { return &value }
func stringPointer(value string) *string { return &value }

func TestNormalizedJSONPinsProtobufPresenceAndRFC8785Encoding(t *testing.T) {
	explicitFalse := false
	explicitZero := int64(0)
	optionalNote := "<>&\u2028é"
	request := &tammyv1.CanonicalRequest{
		ExplicitDefaultFlag: &explicitFalse,
		ExplicitZero:        &explicitZero,
		OptionalNote:        &optionalNote,
		SignedUnits:         -9223372036854775808,
		AccountStatus:       tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED,
	}

	got, err := canonical.NormalizedJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"account_status":"ACCOUNT_STATUS_ARCHIVED","explicit_default_flag":false,"explicit_zero":"0","optional_note":"<>& é","signed_units":"-9223372036854775808"}`)
	if !bytes.Equal(got, want) {
		t.Fatalf("normalized JSON\nwant: %s\n got: %s", want, got)
	}
	if bytes.Contains(got, []byte("implicit_default_flag")) {
		t.Fatalf("implicit default unexpectedly emitted: %s", got)
	}
}

func TestNormalizedJSONIncludesSelectedOneofDefaultAndUint64String(t *testing.T) {
	message := dynamicMessage(t, "PresenceRequest", []*descriptorpb.FieldDescriptorProto{
		{
			Name:       stringPointer("selected_flag"),
			JsonName:   stringPointer("selectedFlag"),
			Number:     int32Pointer(1),
			Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:       descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
			OneofIndex: int32Pointer(0),
		},
		{
			Name:     stringPointer("unsigned_units"),
			JsonName: stringPointer("unsignedUnits"),
			Number:   int32Pointer(2),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
		},
	}, []string{"selection"})
	message.Set(message.Descriptor().Fields().ByName("selected_flag"), protoreflect.ValueOfBool(false))
	message.Set(message.Descriptor().Fields().ByName("unsigned_units"), protoreflect.ValueOfUint64(^uint64(0)))

	got, err := canonical.NormalizedJSON(message)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"selected_flag":false,"unsigned_units":"18446744073709551615"}`
	if string(got) != want {
		t.Fatalf("normalized JSON = %s, want %s", got, want)
	}
}

func TestNormalizedJSONNormalizesFieldMaskTimestampAndPreservesRepeatedOrder(t *testing.T) {
	request := &tammyv1.CanonicalRequest{
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"legal_name", "display_name", "legal_name"}},
		OrderedValues: []string{"second", "first", "second"},
		ObservedAt:    &timestamppb.Timestamp{Seconds: 1785715872, Nanos: 120000000},
	}
	got, err := canonical.NormalizedJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"observed_at":"2026-08-03T00:11:12.120Z","ordered_values":["second","first","second"],"update_mask":"displayName,legalName"}`
	if string(got) != want {
		t.Fatalf("normalized JSON = %s, want %s", got, want)
	}
	if request.UpdateMask.Paths[0] != "legal_name" {
		t.Fatal("normalization mutated the caller's message")
	}
}

func TestNormalizedJSONRejectsInvalidFieldMaskAndTimestamp(t *testing.T) {
	for name, request := range map[string]*tammyv1.CanonicalRequest{
		"field_mask": {UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"bad-name"}}},
		"timestamp":  {ObservedAt: &timestamppb.Timestamp{Seconds: 0, Nanos: 1_000_000_000}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := canonical.NormalizedJSON(request); !errors.Is(err, canonical.ErrInvalidMessage) {
				t.Fatalf("error = %v, want %v", err, canonical.ErrInvalidMessage)
			}
		})
	}
}

func TestNormalizedJSONPinsTimestampFractionalPrecision(t *testing.T) {
	for _, test := range []struct {
		name  string
		nanos int32
		want  string
	}{
		{name: "zero", nanos: 0, want: `{"observed_at":"1970-01-01T00:00:00Z"}`},
		{name: "milliseconds", nanos: 120_000_000, want: `{"observed_at":"1970-01-01T00:00:00.120Z"}`},
		{name: "microseconds", nanos: 123_456_000, want: `{"observed_at":"1970-01-01T00:00:00.123456Z"}`},
		{name: "nanoseconds", nanos: 123_456_789, want: `{"observed_at":"1970-01-01T00:00:00.123456789Z"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := canonical.NormalizedJSON(&tammyv1.CanonicalRequest{
				ObservedAt: &timestamppb.Timestamp{Nanos: test.nanos},
			})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("normalized timestamp = %s, want %s", got, test.want)
			}
		})
	}
}

func TestSemanticHashV1RejectsUnknownWireFieldsRecursively(t *testing.T) {
	unknown := []byte{0xa0, 0x06, 0x01}
	for name, request := range map[string]*tammyv1.CanonicalRequest{
		"top_level": {},
		"nested":    {CommandContext: &tammyv1.CommandContext{}},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "top_level" {
				request.ProtoReflect().SetUnknown(unknown)
			} else {
				request.CommandContext.ProtoReflect().SetUnknown(unknown)
			}
			if _, err := canonical.SemanticHashV1(request); !errors.Is(err, canonical.ErrUnknownFields) {
				t.Fatalf("error = %v, want %v", err, canonical.ErrUnknownFields)
			}
		})
	}
}

func TestUnmarshalStrictRejectsUnknownJSONFields(t *testing.T) {
	request := &tammyv1.CanonicalRequest{}
	err := canonical.UnmarshalStrict([]byte(`{"future_rule":"must-not-be-dropped"}`), request)
	if !errors.Is(err, canonical.ErrUnknownFields) {
		t.Fatalf("error = %v, want %v", err, canonical.ErrUnknownFields)
	}
}

func TestSemanticHashV1RejectsMapAnyAndFloatingPointDescriptors(t *testing.T) {
	for name, message := range map[string]proto.Message{
		"map":   &structpb.Struct{},
		"any":   &anypb.Any{},
		"float": &wrapperspb.DoubleValue{Value: 1.25},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := canonical.SemanticHashV1(message); !errors.Is(err, canonical.ErrUnsupportedShape) {
				t.Fatalf("error = %v, want %v", err, canonical.ErrUnsupportedShape)
			}
		})
	}
}

func TestSemanticHashV1RemovesOnlyAuthenticationAndIdempotencyMetadata(t *testing.T) {
	base := &tammyv1.CanonicalRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c07398f",
			Authentication: &tammyv1.AuthenticationContext{
				ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073980",
				SessionId:   "01890f3c-7b2e-7cc4-98c4-dc0c0c073981",
			},
			FreshFactor: &tammyv1.FreshFactorContext{
				AssertionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073982",
				Purpose:     "post_journal",
				AssertedAt:  &timestamppb.Timestamp{Seconds: 1785715872},
			},
		},
		OrderedValues: []string{"second", "first"},
	}
	differentMetadata := proto.Clone(base).(*tammyv1.CanonicalRequest)
	differentMetadata.CommandContext.IdempotencyKey = "01890f3c-7b2e-7cc4-98c4-dc0c0c073983"
	differentMetadata.CommandContext.Authentication.ActorUserId = "01890f3c-7b2e-7cc4-98c4-dc0c0c073984"
	differentSemantics := proto.Clone(base).(*tammyv1.CanonicalRequest)
	differentSemantics.CommandContext.FreshFactor.Purpose = "reverse_journal"

	baseHash, err := canonical.SemanticHashV1(base)
	if err != nil {
		t.Fatal(err)
	}
	metadataHash, err := canonical.SemanticHashV1(differentMetadata)
	if err != nil {
		t.Fatal(err)
	}
	semanticHash, err := canonical.SemanticHashV1(differentSemantics)
	if err != nil {
		t.Fatal(err)
	}
	if baseHash != metadataHash {
		t.Fatalf("authentication/idempotency changed hash: %s != %s", baseHash.Hex(), metadataHash.Hex())
	}
	if baseHash == semanticHash {
		t.Fatal("fresh-factor semantics did not change hash")
	}
	if baseHash.Version != canonical.SemanticHashVersionV1 {
		t.Fatalf("version = %q", baseHash.Version)
	}
	if _, err := hex.DecodeString(baseHash.Hex()); err != nil || len(baseHash.Hex()) != 64 {
		t.Fatalf("hash is not lowercase SHA-256 hex: %q, %v", baseHash.Hex(), err)
	}
}

func TestSemanticHashV1KeepsSameNamedFieldsOutsideCommandContext(t *testing.T) {
	conflictA := &tammyv1.IdempotencyConflictErrorDetail{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c07398f"}
	conflictB := proto.Clone(conflictA).(*tammyv1.IdempotencyConflictErrorDetail)
	conflictB.IdempotencyKey = "01890f3c-7b2e-7cc4-98c4-dc0c0c073980"
	queryA := &tammyv1.GetAccountRequest{
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073981"},
		AccountId:      "01890f3c-7b2e-7cc4-98c4-dc0c0c073982",
	}
	queryB := proto.Clone(queryA).(*tammyv1.GetAccountRequest)
	queryB.Authentication.ActorUserId = "01890f3c-7b2e-7cc4-98c4-dc0c0c073983"

	for name, messages := range map[string][2]proto.Message{
		"idempotency_key": {conflictA, conflictB},
		"authentication":  {queryA, queryB},
	} {
		t.Run(name, func(t *testing.T) {
			left, err := canonical.SemanticHashV1(messages[0])
			if err != nil {
				t.Fatal(err)
			}
			right, err := canonical.SemanticHashV1(messages[1])
			if err != nil {
				t.Fatal(err)
			}
			if left == right {
				t.Fatalf("non-CommandContext %s was removed from semantic hash", name)
			}
		})
	}
}

func TestSemanticHashV1IgnoresUnrelatedAbsentPresenceAwareDescriptorAddition(t *testing.T) {
	oldMessage := dynamicMessage(t, "StableRequest", []*descriptorpb.FieldDescriptorProto{
		{
			Name:     stringPointer("value"),
			JsonName: stringPointer("value"),
			Number:   int32Pointer(1),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}, nil)
	newMessage := dynamicMessage(t, "StableRequest", []*descriptorpb.FieldDescriptorProto{
		{
			Name:     stringPointer("value"),
			JsonName: stringPointer("value"),
			Number:   int32Pointer(1),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
		{
			Name:           stringPointer("future_note"),
			JsonName:       stringPointer("futureNote"),
			Number:         int32Pointer(2),
			Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:           descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			OneofIndex:     int32Pointer(0),
			Proto3Optional: boolPointer(true),
		},
	}, []string{"_future_note"})
	oldMessage.Set(oldMessage.Descriptor().Fields().ByName("value"), protoreflect.ValueOfString("stable"))
	newMessage.Set(newMessage.Descriptor().Fields().ByName("value"), protoreflect.ValueOfString("stable"))

	oldHash, err := canonical.SemanticHashV1(oldMessage)
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := canonical.SemanticHashV1(newMessage)
	if err != nil {
		t.Fatal(err)
	}
	if oldHash != newHash {
		t.Fatalf("absent compatible descriptor addition changed hash: %s != %s", oldHash.Hex(), newHash.Hex())
	}

	newMessage.Set(newMessage.Descriptor().Fields().ByName("future_note"), protoreflect.ValueOfString("present"))
	presentHash, err := canonical.SemanticHashV1(newMessage)
	if err != nil {
		t.Fatal(err)
	}
	if presentHash == oldHash {
		t.Fatal("present compatible descriptor addition did not change hash")
	}
}

func TestCrossLanguageSemanticHashFixture(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve canonical test path")
	}
	fixturePath := filepath.Join(filepath.Dir(sourceFile), "../../../../../test/fixtures/proto/canonical-requests.json")
	fixtureBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture semanticHashFixtureDocument
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.SemanticHashCases) != 1 {
		t.Fatalf("semantic hash fixture cases = %d, want 1", len(fixture.SemanticHashCases))
	}
	fixtureCase := fixture.SemanticHashCases[0]
	request := &tammyv1.CanonicalRequest{}
	if err := canonical.UnmarshalStrict(fixtureCase.Input, request); err != nil {
		t.Fatal(err)
	}
	normalized, err := canonical.NormalizedJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(normalized) != fixtureCase.ExpectedCanonicalJSON {
		t.Fatalf("canonical JSON\nwant: %s\n got: %s", fixtureCase.ExpectedCanonicalJSON, normalized)
	}
	hash, err := canonical.SemanticHashV1(request)
	if err != nil {
		t.Fatal(err)
	}
	if fixtureCase.Name != "semantic-v1" ||
		fixtureCase.ExpectedMessageType != string(request.ProtoReflect().Descriptor().FullName()) ||
		fixtureCase.ExpectedSemanticHashVersion != hash.Version ||
		fixtureCase.ExpectedSemanticHashHex != hash.Hex() {
		t.Fatalf("semantic fixture mismatch: %#v, hash=%s/%s", fixtureCase, hash.Version, hash.Hex())
	}
}

func dynamicMessage(
	t *testing.T,
	messageName string,
	fields []*descriptorpb.FieldDescriptorProto,
	oneofNames []string,
) *dynamicpb.Message {
	t.Helper()
	oneofs := make([]*descriptorpb.OneofDescriptorProto, len(oneofNames))
	for index, name := range oneofNames {
		oneofs[index] = &descriptorpb.OneofDescriptorProto{Name: stringPointer(name)}
	}
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    stringPointer("canonical_test.proto"),
		Package: stringPointer("canonical.test"),
		Syntax:  stringPointer("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:      stringPointer(messageName),
			Field:     fields,
			OneofDecl: oneofs,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("build dynamic descriptor: %v", err)
	}
	return dynamicpb.NewMessage(file.Messages().ByName(protoreflect.Name(messageName)))
}
