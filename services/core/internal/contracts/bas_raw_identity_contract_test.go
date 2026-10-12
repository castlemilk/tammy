package contracts_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	rawIdentityCanonicalID = "018f0c4a-7b9d-7abc-8def-0123456789ab"
	rawIdentityHashByte    = byte(0x5a)
)

func rawIdentityFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/bas_preparation.proto")
	if err != nil {
		t.Fatalf("BAS raw identity descriptor missing: %v", err)
	}
	return file
}

func rawIdentityMessageDescriptor(t *testing.T, name string) protoreflect.MessageDescriptor {
	t.Helper()
	fullName := protoreflect.FullName(name)
	if !strings.Contains(name, ".") {
		fullName = "tammy.v1." + protoreflect.FullName(name)
	}
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(fullName)
	if err != nil {
		t.Fatalf("required BAS raw identity descriptor %s is missing: %v", fullName, err)
	}
	message, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("BAS raw identity descriptor %s is not a message", fullName)
	}
	return message
}

func rawIdentityField(t *testing.T, message protoreflect.MessageDescriptor, name protoreflect.Name) protoreflect.FieldDescriptor {
	t.Helper()
	field := message.Fields().ByName(name)
	if field == nil {
		t.Fatalf("required BAS raw identity field %s.%s is missing", message.FullName(), name)
	}
	return field
}

func rawIdentityDynamic(t *testing.T, name string) *dynamicpb.Message {
	t.Helper()
	return dynamicpb.NewMessage(rawIdentityMessageDescriptor(t, name))
}

func rawIdentityRequired(t *testing.T, field protoreflect.FieldDescriptor) bool {
	t.Helper()
	return proto.HasExtension(field.Options(), validate.E_Field) && fieldRules(t, field).GetRequired()
}

func rawIdentitySetString(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, value string) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if field.Kind() != protoreflect.StringKind {
		t.Fatalf("%s is %s, want string", field.FullName(), field.Kind())
	}
	message.Set(field, protoreflect.ValueOfString(value))
}

func rawIdentitySetBytes(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, value []byte) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if field.Kind() != protoreflect.BytesKind {
		t.Fatalf("%s is %s, want bytes", field.FullName(), field.Kind())
	}
	message.Set(field, protoreflect.ValueOfBytes(bytes.Clone(value)))
}

func rawIdentitySetInt64(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, value int64) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if field.Kind() != protoreflect.Int64Kind {
		t.Fatalf("%s is %s, want int64", field.FullName(), field.Kind())
	}
	message.Set(field, protoreflect.ValueOfInt64(value))
}

func rawIdentitySetUint64(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, value uint64) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if field.Kind() != protoreflect.Uint64Kind {
		t.Fatalf("%s is %s, want uint64", field.FullName(), field.Kind())
	}
	message.Set(field, protoreflect.ValueOfUint64(value))
}

func rawIdentitySetEnumName(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, valueName protoreflect.Name) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if field.Kind() != protoreflect.EnumKind {
		t.Fatalf("%s is %s, want enum", field.FullName(), field.Kind())
	}
	value := field.Enum().Values().ByName(valueName)
	if value == nil {
		t.Fatalf("%s has no enum value %s", field.Enum().FullName(), valueName)
	}
	message.Set(field, protoreflect.ValueOfEnum(value.Number()))
}

func rawIdentitySetEnumNumber(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, value protoreflect.EnumNumber) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if field.Kind() != protoreflect.EnumKind {
		t.Fatalf("%s is %s, want enum", field.FullName(), field.Kind())
	}
	message.Set(field, protoreflect.ValueOfEnum(value))
}

func rawIdentityAppendMessage(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, child *dynamicpb.Message) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if !field.IsList() || field.Kind() != protoreflect.MessageKind || child == nil || child.Descriptor().FullName() != field.Message().FullName() {
		t.Fatalf("%s cannot contain the supplied child message", field.FullName())
	}
	message.Mutable(field).List().Append(protoreflect.ValueOfMessage(child.ProtoReflect()))
}

func rawIdentitySetMessage(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, child *dynamicpb.Message) {
	t.Helper()
	field := rawIdentityField(t, message.Descriptor(), fieldName)
	if field.IsList() || field.Kind() != protoreflect.MessageKind || child == nil || child.Descriptor().FullName() != field.Message().FullName() {
		t.Fatalf("%s cannot contain the supplied child message", field.FullName())
	}
	message.Set(field, protoreflect.ValueOfMessage(child.ProtoReflect()))
}

func rawIdentitySourceRef(t *testing.T, sourceType, id string, revision uint64, digest []byte) *dynamicpb.Message {
	t.Helper()
	message := rawIdentityDynamic(t, "SourceRef")
	rawIdentitySetString(t, message, "type", sourceType)
	rawIdentitySetString(t, message, "id", id)
	rawIdentitySetUint64(t, message, "revision", revision)
	rawIdentitySetBytes(t, message, "content_hash", digest)
	return message
}

func rawIdentityRawRef(t *testing.T, id string, revision *int64, originalHash []byte, scope protoreflect.EnumNumber) *dynamicpb.Message {
	t.Helper()
	message := rawIdentityDynamic(t, "BasRawSourceRef")
	rawIdentitySetString(t, message, "type", "tax_facts")
	rawIdentitySetString(t, message, "id", id)
	if revision != nil {
		rawIdentitySetInt64(t, message, "original_revision", *revision)
	}
	if originalHash != nil {
		rawIdentitySetBytes(t, message, "original_content_hash", originalHash)
	}
	rawIdentitySetBytes(t, message, "computed_proof_hash", bytes.Repeat([]byte{rawIdentityHashByte}, 32))
	rawIdentitySetEnumNumber(t, message, "proof_scope", scope)
	return message
}

func rawIdentitySource(t *testing.T, raw bool, id string) *dynamicpb.Message {
	t.Helper()
	identity := rawIdentityDynamic(t, "BasSourceIdentity")
	if raw {
		rawIdentitySetMessage(t, identity, "raw_source", rawIdentityRawRef(t, id, nil, nil, 1))
	} else {
		rawIdentitySetMessage(t, identity, "canonical_source", rawIdentitySourceRef(t, "tax_fact", id, 1, bytes.Repeat([]byte{0x26}, 32)))
	}
	return identity
}

func rawIdentityCanonicalWrapper(t *testing.T, source *dynamicpb.Message) *dynamicpb.Message {
	t.Helper()
	identity := rawIdentityDynamic(t, "BasSourceIdentity")
	rawIdentitySetMessage(t, identity, "canonical_source", source)
	return identity
}

func rawIdentityCoverage(t *testing.T, raw bool) *dynamicpb.Message {
	t.Helper()
	coverage := rawIdentityDynamic(t, "BasSourceCoverage")
	rawIdentitySetEnumName(t, coverage, "candidate_kind", "BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT")
	rawIdentitySetEnumName(t, coverage, "disposition", "BAS_COVERAGE_DISPOSITION_UNSUPPORTED")
	rawIdentitySetString(t, coverage, "classification_code", "UNCLASSIFIED")
	rawIdentitySetString(t, coverage, "reason", "Manual tax fact needs reviewed classification")
	if raw {
		rawIdentitySetMessage(t, coverage, "raw_source", rawIdentityRawRef(t, "legacy-tax-fact:91", nil, nil, 1))
	} else {
		rawIdentitySetMessage(t, coverage, "source", rawIdentitySourceRef(t, "tax_fact", rawIdentityCanonicalID, 1, bytes.Repeat([]byte{0x31}, 32)))
	}
	return coverage
}

func rawIdentityFinding(t *testing.T) *dynamicpb.Message {
	t.Helper()
	finding := rawIdentityDynamic(t, "BasValidationFinding")
	rawIdentitySetString(t, finding, "finding_id", rawIdentityCanonicalID)
	rawIdentitySetEnumName(t, finding, "severity", "BAS_FINDING_SEVERITY_BLOCKER")
	rawIdentitySetString(t, finding, "stable_code", "tax_fact.unclassified")
	rawIdentitySetString(t, finding, "safe_message", "A retained tax fact needs reviewed classification.")
	return finding
}

func rawIdentityAssertValid(t *testing.T, label string, message proto.Message) {
	t.Helper()
	if err := protovalidate.Validate(message); err != nil {
		t.Fatalf("valid %s rejected: %v", label, err)
	}
}

func rawIdentityAssertInvalid(t *testing.T, label string, message proto.Message) {
	t.Helper()
	if err := protovalidate.Validate(message); err == nil {
		t.Fatalf("invalid %s passed runtime validation", label)
	}
}

func rawIdentityClone(t *testing.T, message *dynamicpb.Message) *dynamicpb.Message {
	t.Helper()
	cloned, ok := proto.Clone(message).(*dynamicpb.Message)
	if !ok {
		t.Fatal("clone lost raw identity dynamic descriptor")
	}
	return cloned
}

func rawIdentityCanonicalUUID(index int) string {
	return fmt.Sprintf("018f0c4a-7b9d-7abc-8def-%012x", index+1)
}

func rawIdentityAppendListPair(t *testing.T, parent *dynamicpb.Message, identitiesField, canonicalField protoreflect.Name, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		isRaw := index%2 == 1
		id := rawIdentityCanonicalUUID(index)
		if !isRaw {
			ref := rawIdentitySourceRef(t, "tax_fact", id, 1, bytes.Repeat([]byte{byte(index + 1)}, 32))
			rawIdentityAppendMessage(t, parent, identitiesField, rawIdentityCanonicalWrapper(t, ref))
			rawIdentityAppendMessage(t, parent, canonicalField, ref)
		} else {
			identity := rawIdentitySource(t, true, fmt.Sprintf("opaque-source-%03d", index))
			rawIdentityAppendMessage(t, parent, identitiesField, identity)
		}
	}
}

func TestBASRawIdentityContractDescriptorAndWireNumbers(t *testing.T) {
	file := rawIdentityFile(t)
	coverage := file.Messages().ByName("BasSourceCoverage")
	if coverage == nil {
		t.Fatal("BasSourceCoverage descriptor missing")
	}
	for _, want := range []struct {
		name   protoreflect.Name
		number protoreflect.FieldNumber
		kind   protoreflect.Kind
	}{
		{name: "source", number: 1, kind: protoreflect.MessageKind},
		{name: "lineage", number: 6, kind: protoreflect.MessageKind},
		{name: "evidence", number: 7, kind: protoreflect.MessageKind},
		{name: "classification_code", number: 8, kind: protoreflect.StringKind},
		{name: "reason", number: 9, kind: protoreflect.StringKind},
		{name: "raw_source", number: 10, kind: protoreflect.MessageKind},
		{name: "lineage_identities", number: 11, kind: protoreflect.MessageKind},
		{name: "evidence_identities", number: 12, kind: protoreflect.MessageKind},
	} {
		field := rawIdentityField(t, coverage, want.name)
		if field.Number() != want.number || field.Kind() != want.kind {
			t.Errorf("BasSourceCoverage.%s = #%d %s, want #%d %s", want.name, field.Number(), field.Kind(), want.number, want.kind)
		}
	}
	coverageSource := rawIdentityField(t, coverage, "source")
	coverageRaw := rawIdentityField(t, coverage, "raw_source")
	if coverageSource.Message().FullName() != "tammy.v1.SourceRef" || coverageRaw.Message().FullName() != "tammy.v1.BasRawSourceRef" ||
		coverageSource.ContainingOneof() != nil || coverageRaw.ContainingOneof() != nil || rawIdentityRequired(t, coverageSource) || rawIdentityRequired(t, coverageRaw) {
		t.Fatal("coverage source variants must remain ordinary optional message fields")
	}
	for _, name := range []protoreflect.Name{"lineage", "evidence"} {
		field := rawIdentityField(t, coverage, name)
		if !field.IsList() || field.Message().FullName() != "tammy.v1.SourceRef" {
			t.Errorf("BasSourceCoverage.%s no longer preserves its canonical SourceRef list", name)
		}
	}
	for _, name := range []protoreflect.Name{"lineage_identities", "evidence_identities"} {
		field := rawIdentityField(t, coverage, name)
		if !field.IsList() || field.Message().FullName() != "tammy.v1.BasSourceIdentity" || fieldRules(t, field).GetRepeated().GetMaxItems() != 20 {
			t.Errorf("BasSourceCoverage.%s must be a bounded ordered identity list", name)
		}
	}

	identity := rawIdentityMessageDescriptor(t, "BasSourceIdentity")
	for _, want := range []struct {
		name   protoreflect.Name
		number protoreflect.FieldNumber
		full   protoreflect.FullName
	}{
		{name: "canonical_source", number: 1, full: "tammy.v1.SourceRef"},
		{name: "raw_source", number: 10, full: "tammy.v1.BasRawSourceRef"},
	} {
		field := rawIdentityField(t, identity, want.name)
		if field.Number() != want.number || field.Kind() != protoreflect.MessageKind || field.Message().FullName() != want.full ||
			field.ContainingOneof() != nil || rawIdentityRequired(t, field) {
			t.Errorf("BasSourceIdentity.%s must be ordinary optional message field #%d of %s", want.name, want.number, want.full)
		}
	}

	finding := file.Messages().ByName("BasValidationFinding")
	if finding == nil {
		t.Fatal("BasValidationFinding descriptor missing")
	}
	if field := rawIdentityField(t, finding, "sources"); field.Number() != 5 || !field.IsList() || field.Message().FullName() != "tammy.v1.SourceRef" {
		t.Errorf("BasValidationFinding.sources must preserve canonical SourceRef field 5")
	}
	if field := rawIdentityField(t, finding, "source_identities"); field.Number() != 6 || !field.IsList() ||
		field.Message().FullName() != "tammy.v1.BasSourceIdentity" || fieldRules(t, field).GetRepeated().GetMaxItems() != 20 {
		t.Errorf("BasValidationFinding.source_identities must be ordered identity field 6 bounded at 20")
	}

	raw := rawIdentityMessageDescriptor(t, "BasRawSourceRef")
	assertExactFields(t, raw, map[protoreflect.Name]protoreflect.Kind{
		"type": protoreflect.StringKind, "id": protoreflect.StringKind, "original_revision": protoreflect.Int64Kind,
		"original_content_hash": protoreflect.BytesKind, "computed_proof_hash": protoreflect.BytesKind, "proof_scope": protoreflect.EnumKind,
	})
	typeField := rawIdentityField(t, raw, "type")
	typeRules := fieldRules(t, typeField).GetString()
	if typeRules.GetMinLen() != 1 || typeRules.GetMaxLen() != 128 || typeRules.GetPattern() == "" {
		t.Errorf("raw source type must be bounded to 128 lower_snake_case characters, rules=%v", typeRules)
	}
	idField := rawIdentityField(t, raw, "id")
	idRules := fieldRules(t, idField).GetString()
	if idRules.GetMinLen() == 0 || idRules.GetMaxLen() != 0 {
		t.Errorf("raw source id must be nonblank without an invented fixed maximum, rules=%v", idRules)
	}
	revision := rawIdentityField(t, raw, "original_revision")
	originalHash := rawIdentityField(t, raw, "original_content_hash")
	if !revision.HasPresence() || !originalHash.HasPresence() || revision.ContainingOneof() == nil || originalHash.ContainingOneof() == nil {
		t.Error("original revision and hash must be presence-aware optional fields")
	}
	if rules := fieldRules(t, revision).GetInt64(); rules.GetGte() != 0 {
		t.Errorf("original revision must preserve zero while rejecting negative SQL corruption, rules=%v", rules)
	}
	if rules := fieldRules(t, originalHash).GetBytes(); rules.GetLen() != 32 {
		t.Errorf("present original hash must be exactly 32 bytes, rules=%v", rules)
	}
	computed := rawIdentityField(t, raw, "computed_proof_hash")
	if computed.Kind() != protoreflect.BytesKind || computed.HasPresence() || fieldRules(t, computed).GetBytes().GetLen() != 32 {
		t.Error("computed proof hash must be an implicit-presence bytes field with exactly 32 bytes")
	}
	scope := rawIdentityField(t, raw, "proof_scope")
	if scope.Enum().FullName() != "tammy.v1.BasSourceProofScope" || !fieldRules(t, scope).GetEnum().GetDefinedOnly() {
		t.Fatal("raw source proof_scope must use the defined-only BasSourceProofScope enum")
	}
	for _, want := range []struct {
		name   protoreflect.Name
		number protoreflect.EnumNumber
	}{
		{name: "BAS_SOURCE_PROOF_SCOPE_UNSPECIFIED", number: 0},
		{name: "BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION", number: 1},
		{name: "BAS_SOURCE_PROOF_SCOPE_COMPLETE_JOURNAL", number: 2},
		{name: "BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION", number: 3},
	} {
		value := scope.Enum().Values().ByName(want.name)
		if value == nil || value.Number() != want.number {
			t.Errorf("proof scope %s number = %v, want %d", want.name, value, want.number)
		}
	}
}

func TestBASRawSourceIdentityRetainsNullableOriginalsAndAcceptsOpaqueLongIDs(t *testing.T) {
	if rawIdentityFile(t) == nil {
		t.Fatal("BAS preparation file descriptor unavailable")
	}
	longID := strings.Repeat("legacy/journal:", 24)
	proofHash := bytes.Repeat([]byte{rawIdentityHashByte}, 32)
	value := rawIdentityRawRef(t, longID, nil, nil, 1)
	if err := protovalidate.Validate(value); err != nil {
		t.Fatalf("valid opaque non-UUID raw source ID of %d bytes rejected: %v", len(longID), err)
	}
	if len(longID) <= 256 {
		t.Fatal("the non-UUID compatibility fixture must exceed the old 256-byte source ID limit")
	}
	originalRevision := rawIdentityField(t, value.Descriptor(), "original_revision")
	originalHash := rawIdentityField(t, value.Descriptor(), "original_content_hash")
	computed := rawIdentityField(t, value.Descriptor(), "computed_proof_hash")
	if value.Has(originalRevision) || value.Has(originalHash) {
		t.Fatal("SQL NULL original revision/hash were fabricated as present values")
	}
	if !bytes.Equal(value.Get(computed).Bytes(), proofHash) || bytes.Equal(value.Get(computed).Bytes(), nil) {
		t.Fatal("module proof must remain a separate present SHA-256 value")
	}

	zero := int64(0)
	original := bytes.Repeat([]byte{0x33}, 32)
	withPresentZero := rawIdentityRawRef(t, longID, &zero, original, 1)
	if err := protovalidate.Validate(withPresentZero); err != nil {
		t.Fatalf("present SQL zero revision and exact original hash rejected: %v", err)
	}
	if !withPresentZero.Has(originalRevision) || withPresentZero.Get(originalRevision).Int() != 0 || !withPresentZero.Has(originalHash) ||
		!bytes.Equal(withPresentZero.Get(originalHash).Bytes(), original) {
		t.Fatal("present original zero/hash did not preserve exact SQL presence")
	}
	if bytes.Equal(withPresentZero.Get(computed).Bytes(), withPresentZero.Get(originalHash).Bytes()) {
		t.Fatal("fixture must demonstrate that computed proof and original source hash are separate values")
	}
	wire, err := proto.Marshal(withPresentZero)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip := rawIdentityDynamic(t, "BasRawSourceRef")
	if err := proto.Unmarshal(wire, roundTrip); err != nil {
		t.Fatal(err)
	}
	if !roundTrip.Has(originalRevision) || roundTrip.Get(originalRevision).Int() != 0 || !roundTrip.Has(originalHash) {
		t.Fatal("protobuf round trip erased explicit zero or original-hash presence")
	}
}

func TestBASRawSourceIdentityRejectsMalformedTypePresenceAndScope(t *testing.T) {
	base := rawIdentityRawRef(t, "legacy-journal-91", nil, nil, 1)
	rawIdentityAssertValid(t, "source-row projection", base)
	for _, scope := range []protoreflect.EnumNumber{2, 3} {
		rawIdentityAssertValid(t, fmt.Sprintf("proof scope %d", scope), rawIdentityRawRef(t, "legacy-journal-91", nil, nil, scope))
	}
	for _, sourceType := range []string{"tax__facts", "tax_facts_"} {
		value := rawIdentityClone(t, base)
		rawIdentitySetString(t, value, "type", sourceType)
		rawIdentityAssertValid(t, "established type alphabet "+sourceType, value)
	}

	tests := []struct {
		name   string
		mutate func(*dynamicpb.Message)
	}{
		{name: "blank id", mutate: func(value *dynamicpb.Message) { rawIdentitySetString(t, value, "id", "") }},
		{name: "whitespace id", mutate: func(value *dynamicpb.Message) { rawIdentitySetString(t, value, "id", "  \t") }},
		{name: "non snake type", mutate: func(value *dynamicpb.Message) { rawIdentitySetString(t, value, "type", "Tax_Facts") }},
		{name: "type over 128", mutate: func(value *dynamicpb.Message) { rawIdentitySetString(t, value, "type", strings.Repeat("a", 129)) }},
		{name: "negative original revision", mutate: func(value *dynamicpb.Message) { rawIdentitySetInt64(t, value, "original_revision", -1) }},
		{name: "present empty original hash", mutate: func(value *dynamicpb.Message) { rawIdentitySetBytes(t, value, "original_content_hash", []byte{}) }},
		{name: "short original hash", mutate: func(value *dynamicpb.Message) {
			rawIdentitySetBytes(t, value, "original_content_hash", bytes.Repeat([]byte{1}, 31))
		}},
		{name: "missing proof hash", mutate: func(value *dynamicpb.Message) {
			value.Clear(rawIdentityField(t, value.Descriptor(), "computed_proof_hash"))
		}},
		{name: "short proof hash", mutate: func(value *dynamicpb.Message) {
			rawIdentitySetBytes(t, value, "computed_proof_hash", bytes.Repeat([]byte{2}, 31))
		}},
		{name: "unspecified proof scope", mutate: func(value *dynamicpb.Message) { rawIdentitySetEnumNumber(t, value, "proof_scope", 0) }},
		{name: "undefined proof scope", mutate: func(value *dynamicpb.Message) { rawIdentitySetEnumNumber(t, value, "proof_scope", 99) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := rawIdentityClone(t, base)
			test.mutate(invalid)
			rawIdentityAssertInvalid(t, test.name, invalid)
		})
	}
}

func TestBASSourceVariantsUseXORAndKeepCanonicalSourceRefStrict(t *testing.T) {
	canonical := rawIdentityCoverage(t, false)
	raw := rawIdentityCoverage(t, true)
	rawIdentityAssertValid(t, "canonical-only coverage", canonical)
	rawIdentityAssertValid(t, "raw-only coverage", raw)

	neither := rawIdentityCoverage(t, false)
	neither.Clear(rawIdentityField(t, neither.Descriptor(), "source"))
	rawIdentityAssertInvalid(t, "coverage without either source variant", neither)

	both := rawIdentityCoverage(t, false)
	rawIdentitySetMessage(t, both, "raw_source", rawIdentityRawRef(t, "legacy-tax-fact:92", nil, nil, 1))
	rawIdentityAssertInvalid(t, "coverage with both source variants", both)

	identity := rawIdentitySource(t, false, rawIdentityCanonicalID)
	rawIdentityAssertValid(t, "canonical source identity", identity)
	identityNeither := rawIdentityDynamic(t, "BasSourceIdentity")
	rawIdentityAssertInvalid(t, "source identity without either variant", identityNeither)
	identityBoth := rawIdentityClone(t, identity)
	rawIdentitySetMessage(t, identityBoth, "raw_source", rawIdentityRawRef(t, "legacy-tax-fact:93", nil, nil, 1))
	rawIdentityAssertInvalid(t, "source identity with both variants", identityBoth)
	rawIdentityAssertValid(t, "raw source identity", rawIdentitySource(t, true, "legacy-tax-fact:94"))

	strict := rawIdentitySourceRef(t, "tax_fact", "legacy/journal/id", 1, bytes.Repeat([]byte{0x77}, 32))
	rawIdentityAssertInvalid(t, "canonical SourceRef with non-UUID ID", strict)
	strict = rawIdentitySourceRef(t, "tax_fact", rawIdentityCanonicalID, 0, bytes.Repeat([]byte{0x77}, 32))
	rawIdentityAssertInvalid(t, "canonical SourceRef revision zero", strict)
	strict = rawIdentitySourceRef(t, "tax_fact", rawIdentityCanonicalID, 1, bytes.Repeat([]byte{0x77}, 31))
	rawIdentityAssertInvalid(t, "canonical SourceRef non-SHA256 content hash", strict)
}

func TestBASCoverageIdentityListsPreserveCanonicalProjectionOrderAndBounds(t *testing.T) {
	legacy := rawIdentityCoverage(t, false)
	legacyA := rawIdentitySourceRef(t, "journal", rawIdentityCanonicalUUID(0), 1, bytes.Repeat([]byte{0x10}, 32))
	rawIdentityAppendMessage(t, legacy, "lineage", legacyA)
	rawIdentityAppendMessage(t, legacy, "evidence", legacyA)
	rawIdentityAssertValid(t, "legacy canonical lists with empty identity lists", legacy)

	valid := rawIdentityCoverage(t, false)
	first := rawIdentitySourceRef(t, "journal", rawIdentityCanonicalUUID(0), 1, bytes.Repeat([]byte{0x10}, 32))
	last := rawIdentitySourceRef(t, "document", rawIdentityCanonicalUUID(1), 2, bytes.Repeat([]byte{0x11}, 32))
	for _, pair := range []struct {
		identities protoreflect.Name
		canonical  protoreflect.Name
	}{
		{identities: "lineage_identities", canonical: "lineage"},
		{identities: "evidence_identities", canonical: "evidence"},
	} {
		rawIdentityAppendMessage(t, valid, pair.identities, rawIdentityCanonicalWrapper(t, first))
		rawIdentityAppendMessage(t, valid, pair.identities, rawIdentitySource(t, true, "opaque-between-canonicals"))
		rawIdentityAppendMessage(t, valid, pair.identities, rawIdentityCanonicalWrapper(t, last))
		rawIdentityAppendMessage(t, valid, pair.canonical, first)
		rawIdentityAppendMessage(t, valid, pair.canonical, last)
	}
	rawIdentityAssertValid(t, "mixed identities with exact ordered canonical projection", valid)

	wrongOrder := rawIdentityClone(t, valid)
	for _, fieldName := range []protoreflect.Name{"lineage", "evidence"} {
		list := wrongOrder.Mutable(rawIdentityField(t, wrongOrder.Descriptor(), fieldName)).List()
		firstValue, lastValue := list.Get(0), list.Get(1)
		list.Set(0, lastValue)
		list.Set(1, firstValue)
	}
	rawIdentityAssertInvalid(t, "canonical projection in a different order", wrongOrder)

	wrongHash := rawIdentityClone(t, valid)
	lineage := wrongHash.Mutable(rawIdentityField(t, wrongHash.Descriptor(), "lineage")).List()
	changed := rawIdentitySourceRef(t, "journal", rawIdentityCanonicalUUID(0), 1, bytes.Repeat([]byte{0x99}, 32))
	lineage.Set(0, protoreflect.ValueOfMessage(changed.ProtoReflect()))
	rawIdentityAssertInvalid(t, "canonical projection with a different hash", wrongHash)

	for _, count := range []int{20, 21} {
		coverage := rawIdentityCoverage(t, false)
		rawIdentityAppendListPair(t, coverage, "lineage_identities", "lineage", count)
		rawIdentityAppendListPair(t, coverage, "evidence_identities", "evidence", count)
		if count == 20 {
			rawIdentityAssertValid(t, "20 mixed lineage and evidence identities", coverage)
		} else {
			rawIdentityAssertInvalid(t, "21 mixed lineage and evidence identities", coverage)
		}
	}
}

func TestBASValidationFindingIdentityListsPreserveProjectionAndBounds(t *testing.T) {
	legacy := rawIdentityFinding(t)
	rawIdentityAppendMessage(t, legacy, "sources", rawIdentitySourceRef(t, "tax_fact", rawIdentityCanonicalID, 1, bytes.Repeat([]byte{0x4a}, 32)))
	rawIdentityAssertValid(t, "legacy finding source list with empty identity list", legacy)

	for _, count := range []int{20, 21} {
		finding := rawIdentityFinding(t)
		canonicalIndex := 0
		for index := 0; index < count; index++ {
			isRaw := index%2 == 1
			id := rawIdentityCanonicalUUID(canonicalIndex)
			if !isRaw {
				canonicalIndex++
			}
			if !isRaw {
				ref := rawIdentitySourceRef(t, "tax_fact", id, 1, bytes.Repeat([]byte{byte(canonicalIndex)}, 32))
				rawIdentityAppendMessage(t, finding, "source_identities", rawIdentityCanonicalWrapper(t, ref))
				rawIdentityAppendMessage(t, finding, "sources", ref)
			} else {
				rawIdentityAppendMessage(t, finding, "source_identities", rawIdentitySource(t, true, fmt.Sprintf("finding-opaque-%03d", index)))
			}
		}
		if count == 20 {
			rawIdentityAssertValid(t, "20 mixed finding source identities", finding)
		} else {
			rawIdentityAssertInvalid(t, "21 mixed finding source identities", finding)
		}
	}

	valid := rawIdentityFinding(t)
	canonical := rawIdentitySourceRef(t, "tax_fact", rawIdentityCanonicalID, 1, bytes.Repeat([]byte{0x4a}, 32))
	rawIdentityAppendMessage(t, valid, "source_identities", rawIdentityCanonicalWrapper(t, canonical))
	rawIdentityAppendMessage(t, valid, "source_identities", rawIdentitySource(t, true, "opaque-finding-lineage"))
	rawIdentityAppendMessage(t, valid, "sources", canonical)
	rawIdentityAssertValid(t, "finding identity with exact canonical projection", valid)

	wrongProjection := rawIdentityClone(t, valid)
	wrong := rawIdentitySourceRef(t, "tax_fact", rawIdentityCanonicalID, 2, bytes.Repeat([]byte{0x4a}, 32))
	wrongProjection.Mutable(rawIdentityField(t, wrongProjection.Descriptor(), "sources")).List().Set(0, protoreflect.ValueOfMessage(wrong.ProtoReflect()))
	rawIdentityAssertInvalid(t, "finding canonical projection with mismatched revision", wrongProjection)
}
