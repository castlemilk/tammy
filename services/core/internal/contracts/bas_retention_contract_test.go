package contracts_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func basRetentionFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/bas_retention.proto")
	if err != nil {
		t.Fatalf("BAS retention descriptor missing: %v", err)
	}
	return file
}

func basRetentionMessage(t *testing.T, file protoreflect.FileDescriptor, name protoreflect.Name) protoreflect.MessageDescriptor {
	t.Helper()
	message := file.Messages().ByName(name)
	if message == nil {
		t.Fatalf("BAS retention message %s missing", name)
	}
	return message
}

func basRetentionField(t *testing.T, message protoreflect.MessageDescriptor, name protoreflect.Name) protoreflect.FieldDescriptor {
	t.Helper()
	field := message.Fields().ByName(name)
	if field == nil {
		t.Fatalf("%s has no field %q", message.FullName(), name)
	}
	return field
}

func basRetentionFieldCEL(t *testing.T, field protoreflect.FieldDescriptor) []string {
	t.Helper()
	rules := fieldRules(t, field)
	expressions := make([]string, 0, len(rules.GetCel()))
	for _, rule := range rules.GetCel() {
		expressions = append(expressions, rule.GetExpression())
	}
	return expressions
}

func basRetentionRequiredSourceRef(t *testing.T, message protoreflect.MessageDescriptor, fieldName protoreflect.Name, sourceType string) {
	t.Helper()
	field := basRetentionField(t, message, fieldName)
	assertRequiredMessage(t, field, "tammy.v1.SourceRef")
	expressions := strings.Join(basRetentionFieldCEL(t, field), " ")
	if !strings.Contains(expressions, "this.type == '"+sourceType+"'") {
		t.Errorf("%s must constrain its source type to %q, CEL = %q", field.FullName(), sourceType, expressions)
	}
}

func basRetentionOptionalSourceRef(t *testing.T, message protoreflect.MessageDescriptor, fieldName protoreflect.Name, sourceType string) {
	t.Helper()
	field := basRetentionField(t, message, fieldName)
	if field.Kind() != protoreflect.MessageKind || field.Message().FullName() != "tammy.v1.SourceRef" || !field.HasPresence() {
		t.Fatalf("%s must be an explicitly optional SourceRef", field.FullName())
	}
	if fieldRules(t, field).GetRequired() {
		t.Errorf("%s must remain optional", field.FullName())
	}
	expressions := strings.Join(basRetentionFieldCEL(t, field), " ")
	if !strings.Contains(expressions, "this.type == '"+sourceType+"'") {
		t.Errorf("%s must constrain its source type to %q, CEL = %q", field.FullName(), sourceType, expressions)
	}
}

func basRetentionRevisionOneSourceRef(t *testing.T, message protoreflect.MessageDescriptor, fieldName protoreflect.Name) {
	t.Helper()
	field := basRetentionField(t, message, fieldName)
	expressions := strings.Join(basRetentionFieldCEL(t, field), " ")
	if !strings.Contains(expressions, "this.revision == 1u") {
		t.Errorf("%s must identify the immutable revision-one resource, CEL = %q", field.FullName(), expressions)
	}
}

func basRetentionMessageCEL(t *testing.T, message protoreflect.MessageDescriptor) []string {
	t.Helper()
	options, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || !proto.HasExtension(options, validate.E_Message) {
		t.Fatalf("%s has no message validation rules", message.FullName())
	}
	rules, ok := proto.GetExtension(options, validate.E_Message).(*validate.MessageRules)
	if !ok {
		t.Fatalf("%s message validation rules have unexpected type", message.FullName())
	}
	expressions := make([]string, 0, len(rules.GetCel()))
	for _, rule := range rules.GetCel() {
		expressions = append(expressions, rule.GetExpression())
	}
	return expressions
}

func TestBasRetentionSnapshotsAreTypedBoundedAndHashable(t *testing.T) {
	file := basRetentionFile(t)
	wantMessages := []protoreflect.Name{
		"BasFamilyIdentity", "BasFamilyState", "BasBranchIdentity", "BasBranchState", "BasSavedPaygInputs",
	}
	for _, name := range wantMessages {
		basRetentionMessage(t, file, name)
	}

	for i := 0; i < file.Messages().Len(); i++ {
		message := file.Messages().Get(i)
		for j := 0; j < message.Fields().Len(); j++ {
			field := message.Fields().Get(j)
			if field.IsMap() || (field.IsList() && fieldRules(t, field).GetRepeated().GetMaxItems() == 0) {
				t.Errorf("%s must be typed and bounded", field.FullName())
			}
			if field.Kind() == protoreflect.MessageKind {
				fullName := field.Message().FullName()
				if fullName == "google.protobuf.Any" || fullName == "google.protobuf.Struct" || fullName == "google.protobuf.Value" {
					t.Errorf("%s uses an untyped dynamic payload", field.FullName())
				}
			}
			if field.Kind() == protoreflect.FloatKind || field.Kind() == protoreflect.DoubleKind {
				t.Errorf("%s must use exact integer or typed data", field.FullName())
			}
			if field.Kind() == protoreflect.StringKind && !field.IsList() && fieldRules(t, field).GetString().GetMaxLen() == 0 && fieldRules(t, field).GetString().GetPattern() == "" && fieldRules(t, field).GetString().GetLen() == 0 {
				t.Errorf("%s must bound text or use a canonical pattern", field.FullName())
			}
			if field.Kind() == protoreflect.EnumKind && !field.IsList() && !fieldRules(t, field).GetEnum().GetDefinedOnly() {
				t.Errorf("%s must reject undefined enum values", field.FullName())
			}
			if field.Kind() == protoreflect.BytesKind && fieldRules(t, field).GetBytes().GetLen() != 32 {
				t.Errorf("%s must carry a 32-byte SHA-256 digest", field.FullName())
			}
		}
	}
}

func TestBasRetentionFamilyAndBranchSnapshotsPinExactTypedLineage(t *testing.T) {
	file := basRetentionFile(t)
	familyIdentity := basRetentionMessage(t, file, "BasFamilyIdentity")
	assertUUIDv7Field(t, basRetentionField(t, familyIdentity, "family_id"))
	assertUUIDv7Field(t, basRetentionField(t, familyIdentity, "organisation_id"))
	assertRequiredMessage(t, basRetentionField(t, familyIdentity, "period_scope"), "tammy.v1.BasCivilDateRange")
	assertRequiredMessage(t, basRetentionField(t, familyIdentity, "created_at"), "google.protobuf.Timestamp")
	if field := basRetentionField(t, familyIdentity, "window"); field.Kind() != protoreflect.EnumKind || field.Enum().FullName() != "tammy.v1.BasPeriodWindow" || !fieldRules(t, field).GetEnum().GetDefinedOnly() {
		t.Fatalf("%s must use a defined BAS period window", field.FullName())
	}
	if fieldRules(t, basRetentionField(t, familyIdentity, "content_hash")).GetBytes().GetLen() != 32 {
		t.Fatal("BasFamilyIdentity.content_hash must be SHA-256")
	}

	familyState := basRetentionMessage(t, file, "BasFamilyState")
	basRetentionRequiredSourceRef(t, familyState, "family", "bas_family")
	basRetentionRevisionOneSourceRef(t, familyState, "family")
	basRetentionRequiredSourceRef(t, familyState, "root_preparation", "bas_preparation")
	basRetentionOptionalSourceRef(t, familyState, "effective_original", "bas_preparation")
	assertRequiredMessage(t, basRetentionField(t, familyState, "latest_head"), "tammy.v1.BasExpectedIdentity")
	assertRequiredMessage(t, basRetentionField(t, familyState, "active_branch"), "tammy.v1.BasExpectedIdentity")
	if field := basRetentionField(t, familyState, "version"); field.Kind() != protoreflect.Uint64Kind || fieldRules(t, field).GetUint64().GetGte() != 1 {
		t.Fatalf("%s must be a positive independent family CAS version", field.FullName())
	}
	if field := basRetentionField(t, familyState, "last_family_event_ordinal"); field.Kind() != protoreflect.Uint64Kind {
		t.Fatalf("%s must carry the shared family event ordinal", field.FullName())
	}
	familyExpressions := strings.Join(basRetentionMessageCEL(t, familyState), " ")
	for _, selector := range []string{"latest_head.retained.type == 'bas_preparation'", "active_branch.retained.type == 'bas_branch'"} {
		if !strings.Contains(familyExpressions, selector) {
			t.Errorf("BasFamilyState must validate nested identity selector %q", selector)
		}
	}

	branchIdentity := basRetentionMessage(t, file, "BasBranchIdentity")
	assertUUIDv7Field(t, basRetentionField(t, branchIdentity, "branch_id"))
	assertUUIDv7Field(t, basRetentionField(t, branchIdentity, "organisation_id"))
	basRetentionRequiredSourceRef(t, branchIdentity, "family", "bas_family")
	basRetentionRevisionOneSourceRef(t, branchIdentity, "family")
	basRetentionRequiredSourceRef(t, branchIdentity, "family_root_preparation", "bas_preparation")
	assertRequiredMessage(t, basRetentionField(t, branchIdentity, "root_head"), "tammy.v1.BasExpectedIdentity")
	basRetentionOptionalSourceRef(t, branchIdentity, "effective_original", "bas_preparation")
	assertRequiredMessage(t, basRetentionField(t, branchIdentity, "created_at"), "google.protobuf.Timestamp")
	if field := basRetentionField(t, branchIdentity, "original_purpose"); field.Kind() != protoreflect.EnumKind || field.Enum().FullName() != "tammy.v1.BasBranchPurpose" || !fieldRules(t, field).GetEnum().GetDefinedOnly() {
		t.Fatalf("%s must preserve a finite immutable branch purpose", field.FullName())
	}
	branchIdentityExpressions := strings.Join(basRetentionMessageCEL(t, branchIdentity), " ")
	if !strings.Contains(branchIdentityExpressions, "root_head.retained.type == 'bas_preparation'") {
		t.Error("BasBranchIdentity must constrain a present root-head identity to a BAS preparation")
	}

	branchState := basRetentionMessage(t, file, "BasBranchState")
	basRetentionRequiredSourceRef(t, branchState, "branch_identity", "bas_branch")
	basRetentionRevisionOneSourceRef(t, branchState, "branch_identity")
	assertRequiredMessage(t, basRetentionField(t, branchState, "active_attempt"), "tammy.v1.BasExpectedIdentity")
	if field := basRetentionField(t, branchState, "version"); field.Kind() != protoreflect.Uint64Kind || fieldRules(t, field).GetUint64().GetGte() != 1 {
		t.Fatalf("%s must be a positive independent branch CAS version", field.FullName())
	}
	if field := basRetentionField(t, branchState, "workflow_state"); field.Kind() != protoreflect.EnumKind || field.Enum().FullName() != "tammy.v1.BasBranchWorkflowState" || !fieldRules(t, field).GetEnum().GetDefinedOnly() {
		t.Fatalf("%s must use a finite retained workflow state", field.FullName())
	}
	branchStateExpressions := strings.Join(basRetentionMessageCEL(t, branchState), " ")
	if !strings.Contains(branchStateExpressions, "active_attempt.retained.type == 'bas_preparation'") {
		t.Error("BasBranchState must constrain a present active attempt to a BAS preparation")
	}
	basRetentionOptionalSourceRef(t, branchState, "resolution", "bas_external_dispute_resolution")
	basRetentionOptionalSourceRef(t, branchState, "frozen_barrier", "bas_frozen")
}

func TestBasRetentionSavedPaygInputsBindEnvelopeAndActualPreparation(t *testing.T) {
	file := basRetentionFile(t)
	inputs := basRetentionMessage(t, file, "BasSavedPaygInputs")
	assertUUIDv7Field(t, basRetentionField(t, inputs, "input_snapshot_id"))
	assertUUIDv7Field(t, basRetentionField(t, inputs, "organisation_id"))
	assertUUIDv7Field(t, basRetentionField(t, inputs, "preparation_id"))
	basRetentionRequiredSourceRef(t, inputs, "preparation", "bas_preparation")
	if !strings.Contains(strings.Join(basRetentionMessageCEL(t, inputs), " "), "this.preparation.id == this.preparation_id") {
		t.Error("saved PAYG inputs must bind the exact preparation reference to its scoped preparation ID")
	}
	assertRequiredMessage(t, basRetentionField(t, inputs, "paygw_inputs"), "tammy.v1.BasPaygwInputs")
	assertRequiredMessage(t, basRetentionField(t, inputs, "paygi_inputs"), "tammy.v1.BasPaygiInputs")
	assertRequiredMessage(t, basRetentionField(t, inputs, "recorded_at"), "google.protobuf.Timestamp")
	assertUUIDv7Field(t, basRetentionField(t, inputs, "actor_user_id"))
	for _, name := range []protoreflect.Name{"input_hash", "content_hash"} {
		if got := fieldRules(t, basRetentionField(t, inputs, name)).GetBytes().GetLen(); got != 32 {
			t.Errorf("BasSavedPaygInputs.%s length = %d, want SHA-256 length 32", name, got)
		}
	}
	if field := basRetentionField(t, inputs, "input_revision"); field.Kind() != protoreflect.Uint64Kind || fieldRules(t, field).GetUint64().GetGte() != 1 {
		t.Errorf("%s must identify a positive PAYG input revision", field.FullName())
	}
}

const basRetentionRuntimeID = "018f0c4a-7b9d-7abc-8def-0123456789ab"

func basRetentionRuntimeMessage(t *testing.T, name protoreflect.Name) *dynamicpb.Message {
	t.Helper()
	return dynamicpb.NewMessage(requireMessage(t, protoreflect.FullName("tammy.v1."+string(name))))
}

func basRetentionRuntimeField(t *testing.T, message protoreflect.Message, name protoreflect.Name) protoreflect.FieldDescriptor {
	t.Helper()
	field := message.Descriptor().Fields().ByName(name)
	if field == nil {
		t.Fatalf("%s has no field %q", message.Descriptor().FullName(), name)
	}
	return field
}

func basRetentionRuntimeSetString(t *testing.T, message protoreflect.Message, name protoreflect.Name, value string) {
	t.Helper()
	message.Set(basRetentionRuntimeField(t, message, name), protoreflect.ValueOfString(value))
}

func basRetentionRuntimeSetBytes(t *testing.T, message protoreflect.Message, name protoreflect.Name, value []byte) {
	t.Helper()
	message.Set(basRetentionRuntimeField(t, message, name), protoreflect.ValueOfBytes(value))
}

func basRetentionRuntimeSetUint64(t *testing.T, message protoreflect.Message, name protoreflect.Name, value uint64) {
	t.Helper()
	message.Set(basRetentionRuntimeField(t, message, name), protoreflect.ValueOfUint64(value))
}

func basRetentionRuntimeSetInt32(t *testing.T, message protoreflect.Message, name protoreflect.Name, value int32) {
	t.Helper()
	message.Set(basRetentionRuntimeField(t, message, name), protoreflect.ValueOfInt32(value))
}

func basRetentionRuntimeSetBool(t *testing.T, message protoreflect.Message, name protoreflect.Name, value bool) {
	t.Helper()
	message.Set(basRetentionRuntimeField(t, message, name), protoreflect.ValueOfBool(value))
}

func basRetentionRuntimeSetEnum(t *testing.T, message protoreflect.Message, name protoreflect.Name, valueName protoreflect.Name) {
	t.Helper()
	field := basRetentionRuntimeField(t, message, name)
	value := field.Enum().Values().ByName(valueName)
	if value == nil {
		t.Fatalf("%s has no enum value %q", field.FullName(), valueName)
	}
	message.Set(field, protoreflect.ValueOfEnum(value.Number()))
}

func basRetentionRuntimeChild(t *testing.T, message protoreflect.Message, name protoreflect.Name) protoreflect.Message {
	t.Helper()
	field := basRetentionRuntimeField(t, message, name)
	if field.Kind() != protoreflect.MessageKind {
		t.Fatalf("%s is not a message field", field.FullName())
	}
	return message.Mutable(field).Message()
}

func basRetentionRuntimeSetTimestamp(t *testing.T, message protoreflect.Message, name protoreflect.Name) {
	t.Helper()
	field := basRetentionRuntimeField(t, message, name)
	stamp := timestamppb.New(time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC))
	message.Set(field, protoreflect.ValueOfMessage(stamp.ProtoReflect()))
}

func basRetentionRuntimeSetSourceRef(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name, sourceType string) *dynamicpb.Message {
	t.Helper()
	ref := basRetentionRuntimeMessage(t, "SourceRef")
	basRetentionRuntimeSetString(t, ref, "type", sourceType)
	basRetentionRuntimeSetString(t, ref, "id", basRetentionRuntimeID)
	basRetentionRuntimeSetUint64(t, ref, "revision", 1)
	basRetentionRuntimeSetBytes(t, ref, "content_hash", bytes.Repeat([]byte{0x5a}, 32))
	message.Set(basRetentionRuntimeField(t, message, fieldName), protoreflect.ValueOfMessage(ref))
	return ref
}

func basRetentionRuntimeExpected(t *testing.T, sourceType string) *dynamicpb.Message {
	t.Helper()
	expected := basRetentionRuntimeMessage(t, "BasExpectedIdentity")
	basRetentionRuntimeSetSourceRef(t, expected, "retained", sourceType)
	basRetentionRuntimeSetUint64(t, expected, "expected_version", 1)
	return expected
}

func basRetentionRuntimeAbsent(t *testing.T, message protoreflect.Message, fieldName protoreflect.Name) {
	t.Helper()
	expected := basRetentionRuntimeChild(t, message, fieldName)
	basRetentionRuntimeSetBool(t, expected, "absent", true)
}

func basRetentionRuntimeFamilyIdentity(t *testing.T) *dynamicpb.Message {
	t.Helper()
	identity := basRetentionRuntimeMessage(t, "BasFamilyIdentity")
	basRetentionRuntimeSetString(t, identity, "family_id", basRetentionRuntimeID)
	basRetentionRuntimeSetString(t, identity, "organisation_id", basRetentionRuntimeID)
	scope := basRetentionRuntimeChild(t, identity, "period_scope")
	start := basRetentionRuntimeMessage(t, "CivilDate")
	basRetentionRuntimeSetInt32(t, start, "year", 2026)
	basRetentionRuntimeSetInt32(t, start, "month", 7)
	basRetentionRuntimeSetInt32(t, start, "day", 1)
	end := basRetentionRuntimeMessage(t, "CivilDate")
	basRetentionRuntimeSetInt32(t, end, "year", 2026)
	basRetentionRuntimeSetInt32(t, end, "month", 9)
	basRetentionRuntimeSetInt32(t, end, "day", 30)
	scope.Set(basRetentionRuntimeField(t, scope, "start_date"), protoreflect.ValueOfMessage(start))
	scope.Set(basRetentionRuntimeField(t, scope, "end_date"), protoreflect.ValueOfMessage(end))
	basRetentionRuntimeSetEnum(t, identity, "window", "BAS_PERIOD_WINDOW_JUL_SEP")
	basRetentionRuntimeSetTimestamp(t, identity, "created_at")
	basRetentionRuntimeSetBytes(t, identity, "content_hash", bytes.Repeat([]byte{0x5a}, 32))
	return identity
}

func basRetentionRuntimeFamilyState(t *testing.T) *dynamicpb.Message {
	t.Helper()
	state := basRetentionRuntimeMessage(t, "BasFamilyState")
	basRetentionRuntimeSetSourceRef(t, state, "family", "bas_family")
	basRetentionRuntimeSetUint64(t, state, "version", 1)
	basRetentionRuntimeSetSourceRef(t, state, "root_preparation", "bas_preparation")
	latestHead := basRetentionRuntimeExpected(t, "bas_preparation")
	state.Set(basRetentionRuntimeField(t, state, "latest_head"), protoreflect.ValueOfMessage(latestHead))
	basRetentionRuntimeAbsent(t, state, "active_branch")
	basRetentionRuntimeSetEnum(t, state, "dispute_overlay", "BAS_DISPUTE_OVERLAY_STATE_NONE")
	basRetentionRuntimeSetTimestamp(t, state, "updated_at")
	basRetentionRuntimeSetBytes(t, state, "content_hash", bytes.Repeat([]byte{0x5a}, 32))
	return state
}

func basRetentionRuntimeBranchIdentity(t *testing.T) *dynamicpb.Message {
	t.Helper()
	identity := basRetentionRuntimeMessage(t, "BasBranchIdentity")
	basRetentionRuntimeSetString(t, identity, "branch_id", basRetentionRuntimeID)
	basRetentionRuntimeSetString(t, identity, "organisation_id", basRetentionRuntimeID)
	basRetentionRuntimeSetSourceRef(t, identity, "family", "bas_family")
	basRetentionRuntimeSetEnum(t, identity, "original_purpose", "BAS_BRANCH_PURPOSE_REPLACEMENT")
	basRetentionRuntimeSetSourceRef(t, identity, "family_root_preparation", "bas_preparation")
	basRetentionRuntimeAbsent(t, identity, "root_head")
	basRetentionRuntimeSetTimestamp(t, identity, "created_at")
	basRetentionRuntimeSetBytes(t, identity, "content_hash", bytes.Repeat([]byte{0x5a}, 32))
	return identity
}

func basRetentionRuntimeBranchState(t *testing.T) *dynamicpb.Message {
	t.Helper()
	state := basRetentionRuntimeMessage(t, "BasBranchState")
	basRetentionRuntimeSetSourceRef(t, state, "branch_identity", "bas_branch")
	basRetentionRuntimeSetUint64(t, state, "version", 1)
	basRetentionRuntimeSetEnum(t, state, "workflow_state", "BAS_BRANCH_WORKFLOW_STATE_ACTIVE")
	basRetentionRuntimeAbsent(t, state, "active_attempt")
	basRetentionRuntimeSetTimestamp(t, state, "updated_at")
	basRetentionRuntimeSetBytes(t, state, "content_hash", bytes.Repeat([]byte{0x5a}, 32))
	return state
}

func basRetentionRuntimeSavedPaygInputs(t *testing.T) *dynamicpb.Message {
	t.Helper()
	inputs := basRetentionRuntimeMessage(t, "BasSavedPaygInputs")
	basRetentionRuntimeSetString(t, inputs, "input_snapshot_id", basRetentionRuntimeID)
	basRetentionRuntimeSetString(t, inputs, "organisation_id", basRetentionRuntimeID)
	basRetentionRuntimeSetString(t, inputs, "preparation_id", basRetentionRuntimeID)
	basRetentionRuntimeSetSourceRef(t, inputs, "preparation", "bas_preparation")
	basRetentionRuntimeSetUint64(t, inputs, "input_revision", 1)
	basRetentionRuntimeSetBytes(t, inputs, "input_hash", bytes.Repeat([]byte{0x5a}, 32))
	paygw := basRetentionRuntimeChild(t, inputs, "paygw_inputs")
	basRetentionRuntimeSetEnum(t, paygw, "category", "BAS_PAYGW_CATEGORY_UNKNOWN")
	for _, name := range []protoreflect.Name{"w1_total_payments", "w2_amount_withheld", "w3_other_withholding", "w4_no_abn_withholding"} {
		amount := basRetentionRuntimeChild(t, paygw, name)
		basRetentionRuntimeSetEnum(t, amount, "state", "BAS_AMOUNT_STATE_UNSET")
	}
	basRetentionRuntimeSetEnum(t, paygw, "prefill_review", "BAS_PREFILL_REQUIREMENT_UNKNOWN")
	paygi := basRetentionRuntimeChild(t, inputs, "paygi_inputs")
	basRetentionRuntimeSetEnum(t, paygi, "method", "BAS_PAYGI_METHOD_NOT_APPLICABLE")
	basRetentionRuntimeSetEnum(t, paygi, "notice_operative_status", "BAS_NOTICE_OPERATIVE_STATUS_UNKNOWN")
	basRetentionRuntimeSetEnum(t, paygi, "prior_variation", "BAS_PRIOR_VARIATION_DISPOSITION_UNKNOWN")
	basRetentionRuntimeSetEnum(t, paygi, "unsupported_state", "BAS_PAYGI_UNSUPPORTED_STATE_NONE")
	basRetentionRuntimeSetEnum(t, paygi, "t1_review_decision", "BAS_T1_REVIEW_DECISION_UNKNOWN")
	basRetentionRuntimeSetString(t, inputs, "actor_user_id", basRetentionRuntimeID)
	basRetentionRuntimeSetTimestamp(t, inputs, "recorded_at")
	basRetentionRuntimeSetBytes(t, inputs, "content_hash", bytes.Repeat([]byte{0x5a}, 32))
	return inputs
}

func basRetentionRuntimeAccept(t *testing.T, name string, message proto.Message) {
	t.Helper()
	if err := protovalidate.Validate(message); err != nil {
		t.Fatalf("valid %s rejected: %v", name, err)
	}
}

func basRetentionRuntimeReject(t *testing.T, name string, message proto.Message) {
	t.Helper()
	if err := protovalidate.Validate(message); err == nil {
		t.Fatalf("invalid %s passed runtime validation", name)
	}
}

func TestBasRetentionRuntimeProtoValidationAcceptsTypedSnapshotsAndExplicitAbsence(t *testing.T) {
	for name, message := range map[string]proto.Message{
		"family identity":   basRetentionRuntimeFamilyIdentity(t),
		"family state":      basRetentionRuntimeFamilyState(t),
		"branch identity":   basRetentionRuntimeBranchIdentity(t),
		"branch state":      basRetentionRuntimeBranchState(t),
		"saved PAYG inputs": basRetentionRuntimeSavedPaygInputs(t),
	} {
		t.Run(name, func(t *testing.T) { basRetentionRuntimeAccept(t, name, message) })
	}
}

func TestBasRetentionRuntimeProtoValidationRejectsMalformedLineageAndPayloads(t *testing.T) {
	t.Run("family civil scope", func(t *testing.T) {
		identity := basRetentionRuntimeFamilyIdentity(t)
		scope := basRetentionRuntimeChild(t, identity, "period_scope")
		start := basRetentionRuntimeChild(t, scope, "start_date")
		basRetentionRuntimeSetInt32(t, start, "day", 32)
		basRetentionRuntimeReject(t, "family identity with invalid calendar date", identity)
	})

	t.Run("family selector", func(t *testing.T) {
		state := basRetentionRuntimeFamilyState(t)
		family := basRetentionRuntimeChild(t, state, "family")
		basRetentionRuntimeSetString(t, family, "type", "bas_period")
		basRetentionRuntimeReject(t, "family state with wrong family selector", state)
	})

	t.Run("branch root absence", func(t *testing.T) {
		identity := basRetentionRuntimeBranchIdentity(t)
		rootHead := basRetentionRuntimeChild(t, identity, "root_head")
		basRetentionRuntimeSetBool(t, rootHead, "absent", false)
		basRetentionRuntimeReject(t, "branch identity with false explicit absence", identity)
	})

	t.Run("branch active attempt selector", func(t *testing.T) {
		state := basRetentionRuntimeBranchState(t)
		activeAttempt := basRetentionRuntimeExpected(t, "bas_period")
		state.Set(basRetentionRuntimeField(t, state, "active_attempt"), protoreflect.ValueOfMessage(activeAttempt))
		basRetentionRuntimeReject(t, "branch state with wrong active attempt selector", state)
	})

	t.Run("branch enum", func(t *testing.T) {
		state := basRetentionRuntimeBranchState(t)
		field := basRetentionRuntimeField(t, state, "workflow_state")
		state.Set(field, protoreflect.ValueOfEnum(0))
		basRetentionRuntimeReject(t, "branch state with unspecified workflow", state)
	})

	t.Run("branch identity revision", func(t *testing.T) {
		state := basRetentionRuntimeBranchState(t)
		branch := basRetentionRuntimeChild(t, state, "branch_identity")
		basRetentionRuntimeSetUint64(t, branch, "revision", 2)
		basRetentionRuntimeReject(t, "branch state with non-identity branch revision", state)
	})

	t.Run("saved PAYG selector", func(t *testing.T) {
		inputs := basRetentionRuntimeSavedPaygInputs(t)
		preparation := basRetentionRuntimeChild(t, inputs, "preparation")
		basRetentionRuntimeSetString(t, preparation, "type", "bas_family")
		basRetentionRuntimeReject(t, "saved PAYG inputs with wrong preparation selector", inputs)
	})

	t.Run("saved PAYG input hash", func(t *testing.T) {
		inputs := basRetentionRuntimeSavedPaygInputs(t)
		basRetentionRuntimeSetBytes(t, inputs, "input_hash", bytes.Repeat([]byte{0x5a}, 31))
		basRetentionRuntimeReject(t, "saved PAYG inputs with truncated input hash", inputs)
	})

	t.Run("saved PAYG preparation ID binding", func(t *testing.T) {
		inputs := basRetentionRuntimeSavedPaygInputs(t)
		basRetentionRuntimeSetString(t, inputs, "preparation_id", "018f0c4a-7b9d-7abc-8def-0123456789ac")
		basRetentionRuntimeReject(t, "saved PAYG inputs with a mismatched preparation ID", inputs)
	})
}
