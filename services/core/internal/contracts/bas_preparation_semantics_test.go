package contracts_test

import (
	"bytes"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

const basSemanticID = "018f0c4a-7b9d-7abc-8def-0123456789ab"
const basSemanticCorrectionRecordID = "018f0c4a-7b9d-7abc-8def-0123456789ad"
const basSemanticOtherObservationID = "018f0c4a-7b9d-7abc-8def-0123456789ac"

func basSemanticMessage(t *testing.T, name string) *dynamicpb.Message {
	t.Helper()
	fullName := name
	if !strings.Contains(fullName, ".") {
		fullName = "tammy.v1." + fullName
	}
	return dynamicpb.NewMessage(requireMessage(t, protoreflect.FullName(fullName)))
}

func basSemanticField(t *testing.T, message protoreflect.Message, name string) protoreflect.FieldDescriptor {
	t.Helper()
	field := message.Descriptor().Fields().ByName(protoreflect.Name(name))
	if field == nil {
		t.Fatalf("%s has no field %q", message.Descriptor().FullName(), name)
	}
	return field
}

func basSemanticSetString(t *testing.T, message protoreflect.Message, name, value string) {
	t.Helper()
	message.Set(basSemanticField(t, message, name), protoreflect.ValueOfString(value))
}

func basSemanticSetBytes(t *testing.T, message protoreflect.Message, name string, value []byte) {
	t.Helper()
	message.Set(basSemanticField(t, message, name), protoreflect.ValueOfBytes(value))
}

func basSemanticSetInt64(t *testing.T, message protoreflect.Message, name string, value int64) {
	t.Helper()
	message.Set(basSemanticField(t, message, name), protoreflect.ValueOfInt64(value))
}

func basSemanticSetInt32(t *testing.T, message protoreflect.Message, name string, value int32) {
	t.Helper()
	message.Set(basSemanticField(t, message, name), protoreflect.ValueOfInt32(value))
}

func basSemanticSetUint64(t *testing.T, message protoreflect.Message, name string, value uint64) {
	t.Helper()
	message.Set(basSemanticField(t, message, name), protoreflect.ValueOfUint64(value))
}

func basSemanticSetEnumName(t *testing.T, message protoreflect.Message, fieldName, valueName string) {
	t.Helper()
	field := basSemanticField(t, message, fieldName)
	value := field.Enum().Values().ByName(protoreflect.Name(valueName))
	if value == nil {
		t.Fatalf("%s enum has no value %q", field.FullName(), valueName)
	}
	message.Set(field, protoreflect.ValueOfEnum(value.Number()))
}

func basSemanticSetMessage(t *testing.T, message protoreflect.Message, fieldName string, nested protoreflect.Message) {
	t.Helper()
	message.Set(basSemanticField(t, message, fieldName), protoreflect.ValueOfMessage(nested))
}

func basSemanticChild(t *testing.T, message protoreflect.Message, fieldName string) protoreflect.Message {
	t.Helper()
	field := basSemanticField(t, message, fieldName)
	if field.Kind() != protoreflect.MessageKind {
		t.Fatalf("%s is not a message", field.FullName())
	}
	return message.Mutable(field).Message()
}

func basSemanticAppendMessage(t *testing.T, message protoreflect.Message, fieldName string, nested protoreflect.Message) {
	t.Helper()
	field := basSemanticField(t, message, fieldName)
	if !field.IsList() {
		t.Fatalf("%s is not repeated", field.FullName())
	}
	message.Mutable(field).List().Append(protoreflect.ValueOfMessage(nested))
}

func basSemanticSetPathString(t *testing.T, message protoreflect.Message, path, value string) {
	t.Helper()
	parts := strings.Split(path, ".")
	for _, name := range parts[:len(parts)-1] {
		message = basSemanticChild(t, message, name)
	}
	basSemanticSetString(t, message, parts[len(parts)-1], value)
}

func basSemanticSetPathBytes(t *testing.T, message protoreflect.Message, path string, value []byte) {
	t.Helper()
	parts := strings.Split(path, ".")
	for _, name := range parts[:len(parts)-1] {
		message = basSemanticChild(t, message, name)
	}
	basSemanticSetBytes(t, message, parts[len(parts)-1], value)
}

func basSemanticClearPath(t *testing.T, message protoreflect.Message, path string) {
	t.Helper()
	parts := strings.Split(path, ".")
	for _, name := range parts[:len(parts)-1] {
		message = basSemanticChild(t, message, name)
	}
	message.Clear(basSemanticField(t, message, parts[len(parts)-1]))
}

func basSemanticClone(t *testing.T, message *dynamicpb.Message) *dynamicpb.Message {
	t.Helper()
	clone, ok := proto.Clone(message).(*dynamicpb.Message)
	if !ok {
		t.Fatal("clone lost its dynamic descriptor")
	}
	return clone
}

func basSemanticAccept(t *testing.T, name string, message proto.Message) {
	t.Helper()
	if err := protovalidate.Validate(message); err != nil {
		t.Fatalf("valid %s rejected: %v", name, err)
	}
}

func basSemanticReject(t *testing.T, name string, message proto.Message) {
	t.Helper()
	if err := protovalidate.Validate(message); err == nil {
		t.Fatalf("invalid %s passed runtime validation", name)
	}
}

func basSemanticHash() []byte { return bytes.Repeat([]byte{0x5a}, 32) }

func basSemanticSourceRef(t *testing.T, sourceType string) *dynamicpb.Message {
	t.Helper()
	ref := basSemanticMessage(t, "SourceRef")
	basSemanticSetString(t, ref, "type", sourceType)
	basSemanticSetString(t, ref, "id", basSemanticID)
	basSemanticSetUint64(t, ref, "revision", 1)
	basSemanticSetBytes(t, ref, "content_hash", basSemanticHash())
	return ref
}

func basSemanticAmountInput(t *testing.T, state string, amount *int64, currency string) *dynamicpb.Message {
	t.Helper()
	input := basSemanticMessage(t, "BasAmountInput")
	basSemanticSetEnumName(t, input, "state", state)
	if amount != nil {
		money := basSemanticMessage(t, "Money")
		basSemanticSetString(t, money, "currency_code", currency)
		basSemanticSetInt64(t, money, "minor_units", *amount)
		basSemanticSetMessage(t, input, "amount", money)
	}
	return input
}

func basSemanticTraceOperand(t *testing.T, numerator string, reported int64, state string) *dynamicpb.Message {
	t.Helper()
	operand := basSemanticMessage(t, "BasTraceOperand")
	basSemanticSetEnumName(t, operand, "label", "BAS_FORM_LABEL_G1")
	basSemanticSetString(t, operand, "exact_cents_numerator", numerator)
	basSemanticSetString(t, operand, "exact_cents_denominator", "100")
	basSemanticSetInt64(t, operand, "reported_whole_dollars", reported)
	basSemanticSetEnumName(t, operand, "state", state)
	return operand
}

func basSemanticKnownTrace(t *testing.T) *dynamicpb.Message {
	t.Helper()
	trace := basSemanticMessage(t, "BasLabelTrace")
	basSemanticSetEnumName(t, trace, "label", "BAS_FORM_LABEL_G1")
	basSemanticSetEnumName(t, trace, "operation", "BAS_TRACE_OPERATION_INPUT")
	basSemanticSetString(t, trace, "exact_cents_numerator", "99")
	basSemanticSetString(t, trace, "exact_cents_denominator", "1")
	basSemanticSetInt64(t, trace, "reported_whole_dollars", 0)
	basSemanticSetString(t, trace, "rounding_difference_cents_numerator", "-99")
	basSemanticSetString(t, trace, "rounding_difference_cents_denominator", "1")
	basSemanticSetString(t, trace, "rule_id", "bas.gst.g1.input")
	basSemanticSetEnumName(t, trace, "state", "BAS_AMOUNT_STATE_KNOWN")
	return trace
}

func basSemanticEmptyTrace(t *testing.T, state string) *dynamicpb.Message {
	t.Helper()
	trace := basSemanticMessage(t, "BasLabelTrace")
	basSemanticSetEnumName(t, trace, "label", "BAS_FORM_LABEL_G1")
	basSemanticSetEnumName(t, trace, "operation", "BAS_TRACE_OPERATION_INPUT")
	basSemanticSetString(t, trace, "rule_id", "bas.gst.g1.input")
	basSemanticSetEnumName(t, trace, "state", state)
	return trace
}

func basSemanticKnownZeroTrace(t *testing.T) *dynamicpb.Message {
	t.Helper()
	trace := basSemanticEmptyTrace(t, "BAS_AMOUNT_STATE_KNOWN_ZERO")
	basSemanticSetString(t, trace, "exact_cents_numerator", "0")
	basSemanticSetString(t, trace, "exact_cents_denominator", "1")
	basSemanticSetInt64(t, trace, "reported_whole_dollars", 0)
	basSemanticSetString(t, trace, "rounding_difference_cents_numerator", "0")
	basSemanticSetString(t, trace, "rounding_difference_cents_denominator", "1")
	return trace
}

func TestBasAmountInputStatesRequireOnlyTheirMatchingAUDPayload(t *testing.T) {
	for _, state := range []string{
		"BAS_AMOUNT_STATE_UNSET", "BAS_AMOUNT_STATE_UNKNOWN", "BAS_AMOUNT_STATE_NOT_APPLICABLE",
	} {
		t.Run(state, func(t *testing.T) {
			basSemanticAccept(t, state+" without an invented amount", basSemanticAmountInput(t, state, nil, ""))
		})
	}

	zero := int64(0)
	basSemanticAccept(t, "known zero with exact AUD zero", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN_ZERO", &zero, "AUD"))
	positive := int64(12345)
	basSemanticAccept(t, "known positive amount with exact AUD cents", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &positive, "AUD"))
	basSemanticReject(t, "known-zero state with positive AUD amount", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN_ZERO", &positive, "AUD"))
	basSemanticReject(t, "known-positive state with zero AUD amount", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &zero, "AUD"))

	for _, test := range []struct {
		name     string
		state    string
		amount   int64
		currency string
	}{
		{name: "negative cents", state: "BAS_AMOUNT_STATE_KNOWN", amount: -1, currency: "AUD"},
		{name: "foreign currency", state: "BAS_AMOUNT_STATE_KNOWN", amount: 1, currency: "USD"},
		{name: "amount hidden behind unknown", state: "BAS_AMOUNT_STATE_UNKNOWN", amount: 0, currency: "AUD"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := basSemanticAmountInput(t, test.state, &test.amount, test.currency)
			basSemanticReject(t, test.name, bad)
		})
	}

	undefined := basSemanticAmountInput(t, "BAS_AMOUNT_STATE_UNKNOWN", nil, "")
	state := basSemanticField(t, undefined, "state")
	undefined.Set(state, protoreflect.ValueOfEnum(99))
	basSemanticReject(t, "undefined amount state", undefined)
}

func TestBasAmountEvidenceIsBoundedAndRetainedBySHA256(t *testing.T) {
	positive := int64(1)
	input := basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &positive, "AUD")
	for i := 0; i < 20; i++ {
		basSemanticAppendMessage(t, input, "evidence", basSemanticSourceRef(t, "payg_source"))
	}
	basSemanticAccept(t, "20 retained amount evidence references", input)
	basSemanticAppendMessage(t, input, "evidence", basSemanticSourceRef(t, "payg_source"))
	basSemanticReject(t, "21 retained amount evidence references", input)

	badHash := basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &positive, "AUD")
	ref := basSemanticSourceRef(t, "payg_source")
	basSemanticSetBytes(t, ref, "content_hash", make([]byte, 31))
	basSemanticAppendMessage(t, badHash, "evidence", ref)
	basSemanticReject(t, "evidence reference with non-SHA256 hash", badHash)
}

func TestBasLabelTraceKeepsExactValuesNonnegativeAndRoundingSigned(t *testing.T) {
	baseline := basSemanticKnownTrace(t)
	basSemanticAccept(t, "nonnegative exact cents with -99-cent rounding difference", baseline)
	basSemanticAccept(t, "known-zero label with exact zero payload", basSemanticKnownZeroTrace(t))

	for _, state := range []string{
		"BAS_AMOUNT_STATE_UNSET", "BAS_AMOUNT_STATE_UNKNOWN", "BAS_AMOUNT_STATE_NOT_APPLICABLE",
	} {
		t.Run(state+" without numeric payload", func(t *testing.T) {
			trace := basSemanticEmptyTrace(t, state)
			basSemanticAccept(t, "unresolved trace state without invented values", trace)
			basSemanticSetString(t, trace, "exact_cents_numerator", "0")
			basSemanticReject(t, "unresolved trace state with invented numeric payload", trace)
		})
	}

	knownPositiveWithZero := basSemanticKnownZeroTrace(t)
	basSemanticSetEnumName(t, knownPositiveWithZero, "state", "BAS_AMOUNT_STATE_KNOWN")
	basSemanticReject(t, "known-positive state with known-zero trace payload", knownPositiveWithZero)
	knownZeroWithPositive := basSemanticKnownZeroTrace(t)
	basSemanticSetString(t, knownZeroWithPositive, "exact_cents_numerator", "1")
	basSemanticReject(t, "known-zero state with positive exact payload", knownZeroWithPositive)

	for _, test := range []struct {
		name  string
		field string
		value string
	}{
		{name: "negative exact cents", field: "exact_cents_numerator", value: "-1"},
		{name: "leading zero numerator", field: "exact_cents_numerator", value: "01"},
		{name: "negative zero numerator", field: "exact_cents_numerator", value: "-0"},
		{name: "zero denominator", field: "exact_cents_denominator", value: "0"},
		{name: "oversized numerator", field: "exact_cents_numerator", value: "1" + strings.Repeat("0", 80)},
		{name: "negative reported dollars", field: "reported_whole_dollars", value: "-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := basSemanticClone(t, baseline)
			if test.field == "reported_whole_dollars" {
				basSemanticSetInt64(t, bad, test.field, -1)
			} else {
				basSemanticSetString(t, bad, test.field, test.value)
			}
			basSemanticReject(t, test.name, bad)
		})
	}

	operand := basSemanticTraceOperand(t, "12300", 1, "BAS_AMOUNT_STATE_KNOWN")
	basSemanticAccept(t, "known trace operand", operand)
	zeroOperand := basSemanticTraceOperand(t, "0", 0, "BAS_AMOUNT_STATE_KNOWN_ZERO")
	basSemanticAccept(t, "known-zero trace operand", zeroOperand)
	negativeReport := basSemanticTraceOperand(t, "123", -1, "BAS_AMOUNT_STATE_KNOWN")
	basSemanticReject(t, "trace operand with negative reported dollars", negativeReport)
	for _, test := range []struct {
		name, field, value string
	}{
		{"negative exact operand", "exact_cents_numerator", "-1"},
		{"leading-zero exact operand", "exact_cents_numerator", "01"},
		{"negative-zero exact operand", "exact_cents_numerator", "-0"},
		{"zero operand denominator", "exact_cents_denominator", "0"},
		{"oversized exact operand", "exact_cents_numerator", "1" + strings.Repeat("0", 80)},
	} {
		bad := basSemanticClone(t, operand)
		basSemanticSetString(t, bad, test.field, test.value)
		basSemanticReject(t, test.name, bad)
	}

	bounded := basSemanticKnownTrace(t)
	for i := 0; i < 8; i++ {
		basSemanticAppendMessage(t, bounded, "operands", basSemanticTraceOperand(t, "12300", 1, "BAS_AMOUNT_STATE_KNOWN"))
	}
	basSemanticAccept(t, "eight bounded trace operands", bounded)
	basSemanticAppendMessage(t, bounded, "operands", basSemanticTraceOperand(t, "12300", 1, "BAS_AMOUNT_STATE_KNOWN"))
	basSemanticReject(t, "ninth trace operand", bounded)
}

func basSemanticObligationAnswer(t *testing.T, applicability, reason string, evidence bool) *dynamicpb.Message {
	t.Helper()
	answer := basSemanticMessage(t, "BasObligationAnswer")
	basSemanticSetEnumName(t, answer, "applicability", applicability)
	if reason != "" {
		basSemanticSetString(t, answer, "reason", reason)
	}
	if evidence {
		basSemanticAppendMessage(t, answer, "evidence", basSemanticSourceRef(t, "obligation_review"))
	}
	return answer
}

func basSemanticObligationInventory(t *testing.T, applicability, reason string, evidence bool) *dynamicpb.Message {
	t.Helper()
	inventory := basSemanticMessage(t, "BasOtherObligationInventory")
	for _, field := range []string{
		"fringe_benefits_tax", "wine_equalisation_tax", "luxury_car_tax", "fuel_tax_credits",
		"deferred_company_instalments", "deferred_or_import_gst", "special_gst_schemes_or_adjustments",
		"other_activity_statement_obligations",
	} {
		basSemanticSetMessage(t, inventory, field, basSemanticObligationAnswer(t, applicability, reason, evidence))
	}
	return inventory
}

func TestBasOtherObligationInventoryDistinguishesUnknownFromReviewedNotApplicable(t *testing.T) {
	unknown := basSemanticObligationInventory(t, "BAS_APPLICABILITY_UNKNOWN", "", false)
	basSemanticAccept(t, "all eight unanswered obligations while collecting", unknown)

	notApplicable := basSemanticObligationInventory(t, "BAS_APPLICABILITY_NOT_APPLICABLE", "No such obligation applies", true)
	basSemanticAccept(t, "all eight explicitly not-applicable obligations with reason and evidence", notApplicable)
	basSemanticClearPath(t, notApplicable, "fuel_tax_credits")
	basSemanticReject(t, "inventory omitting a reviewed obligation answer", notApplicable)

	missingReason := basSemanticObligationAnswer(t, "BAS_APPLICABILITY_NOT_APPLICABLE", "", true)
	basSemanticReject(t, "not-applicable answer without a reason", missingReason)
	activeWithoutEvidence := basSemanticObligationAnswer(t, "BAS_APPLICABILITY_ACTIVE", "Withheld tax is reported", false)
	basSemanticReject(t, "active obligation without evidence", activeWithoutEvidence)
}

func basSemanticExpectedIdentity(t *testing.T, absent bool, sourceType string, version uint64) *dynamicpb.Message {
	t.Helper()
	identity := basSemanticMessage(t, "BasExpectedIdentity")
	if absent {
		message := basSemanticField(t, identity, "absent")
		identity.Set(message, protoreflect.ValueOfBool(true))
	} else {
		basSemanticSetMessage(t, identity, "retained", basSemanticSourceRef(t, sourceType))
		basSemanticSetUint64(t, identity, "expected_version", version)
	}
	return identity
}

func TestBasExpectedIdentityRequiresExplicitAbsenceOrCompleteRetainedIdentity(t *testing.T) {
	basSemanticAccept(t, "explicit absence", basSemanticExpectedIdentity(t, true, "", 0))
	basSemanticAccept(t, "retained source and positive version", basSemanticExpectedIdentity(t, false, "bas_preparation", 2))
	zeroVersion := basSemanticExpectedIdentity(t, false, "bas_preparation", 0)
	basSemanticReject(t, "retained source with zero expected version", zeroVersion)

	missing := basSemanticMessage(t, "BasExpectedIdentity")
	basSemanticReject(t, "identity with no oneof branch", missing)

	falseAbsence := basSemanticMessage(t, "BasExpectedIdentity")
	absent := basSemanticField(t, falseAbsence, "absent")
	falseAbsence.Set(absent, protoreflect.ValueOfBool(false))
	basSemanticReject(t, "false explicit-absence branch", falseAbsence)

	for _, test := range []struct {
		name string
		edit func(*testing.T, *dynamicpb.Message)
	}{
		{name: "retained reference with malformed hash", edit: func(t *testing.T, identity *dynamicpb.Message) {
			basSemanticSetPathBytes(t, identity, "retained.content_hash", make([]byte, 31))
		}},
		{name: "retained reference with zero revision", edit: func(t *testing.T, identity *dynamicpb.Message) {
			retained := basSemanticChild(t, identity, "retained")
			basSemanticSetUint64(t, retained, "revision", 0)
		}},
		{name: "retained reference with malformed id", edit: func(t *testing.T, identity *dynamicpb.Message) {
			basSemanticSetPathString(t, identity, "retained.id", "not-a-uuid")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := basSemanticExpectedIdentity(t, false, "bas_preparation", 2)
			test.edit(t, bad)
			basSemanticReject(t, test.name, bad)
		})
	}
}

func basSemanticCommandContext(t *testing.T, purpose string) *dynamicpb.Message {
	t.Helper()
	context := basSemanticMessage(t, "CommandContext")
	basSemanticSetString(t, context, "idempotency_key", basSemanticID)
	auth := basSemanticMessage(t, "AuthenticationContext")
	basSemanticSetString(t, auth, "actor_user_id", basSemanticID)
	basSemanticSetString(t, auth, "session_id", basSemanticID)
	basSemanticSetMessage(t, context, "authentication", auth)
	if purpose != "" {
		factor := basSemanticMessage(t, "FreshFactorContext")
		basSemanticSetString(t, factor, "assertion_id", basSemanticID)
		basSemanticSetString(t, factor, "purpose", purpose)
		timestamp := basSemanticMessage(t, "google.protobuf.Timestamp")
		basSemanticSetInt64(t, timestamp, "seconds", 1)
		basSemanticSetMessage(t, factor, "asserted_at", timestamp)
		basSemanticSetMessage(t, context, "fresh_factor", factor)
	}
	return context
}

func basSemanticChain(t *testing.T) *dynamicpb.Message {
	t.Helper()
	chain := basSemanticMessage(t, "BasChainExpectation")
	basSemanticSetMessage(t, chain, "family", basSemanticSourceRef(t, "bas_family"))
	basSemanticSetUint64(t, chain, "expected_family_version", 1)
	basSemanticSetMessage(t, chain, "root_preparation", basSemanticSourceRef(t, "bas_preparation"))
	basSemanticSetMessage(t, chain, "latest_head", basSemanticExpectedIdentity(t, false, "bas_preparation", 1))
	basSemanticSetMessage(t, chain, "branch", basSemanticExpectedIdentity(t, true, "", 0))
	basSemanticSetMessage(t, chain, "active_attempt", basSemanticExpectedIdentity(t, true, "", 0))
	return chain
}

func basSemanticCommandRequest(t *testing.T, name, purpose string) *dynamicpb.Message {
	t.Helper()
	request := basSemanticMessage(t, name)
	basSemanticSetMessage(t, request, "command_context", basSemanticCommandContext(t, purpose))
	basSemanticSetString(t, request, "organisation_id", basSemanticID)
	basSemanticSetUint64(t, request, "expected_version", 1)
	return request
}

func basSemanticFreshFactorRequest(t *testing.T, name, purpose string) *dynamicpb.Message {
	t.Helper()
	request := basSemanticCommandRequest(t, name, purpose)
	basSemanticSetMessage(t, request, "chain", basSemanticChain(t))
	switch name {
	case "FreezeBasForManualHandoffRequest":
		for _, field := range []string{"preparation_id", "calculation_id", "validation_id"} {
			basSemanticSetString(t, request, field, basSemanticID)
		}
		for _, field := range []string{"preparation_hash", "calculation_hash", "validation_hash"} {
			basSemanticSetBytes(t, request, field, basSemanticHash())
		}
		basSemanticSetString(t, request, "approval_reason", "Reviewed BAS for manual handoff")
	case "CreateBasReplacementRequest":
		basSemanticSetString(t, request, "predecessor_frozen_id", basSemanticID)
		basSemanticSetBytes(t, request, "predecessor_frozen_hash", basSemanticHash())
		basSemanticSetString(t, request, "reason", "Supersede unused handoff")
	case "RecordExternalBasObservationRequest":
		basSemanticSetString(t, request, "frozen_id", basSemanticID)
		basSemanticSetBytes(t, request, "frozen_hash", basSemanticHash())
		observation := basSemanticMessage(t, "BasExternalObservationInput")
		basSemanticSetEnumName(t, observation, "event_kind", "BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE")
		basSemanticSetEnumName(t, observation, "channel", "BAS_EXTERNAL_CHANNEL_ONLINE_PORTAL")
		claimedAt := basSemanticMessage(t, "google.protobuf.Timestamp")
		basSemanticSetInt64(t, claimedAt, "seconds", 1)
		basSemanticSetMessage(t, observation, "claimed_event_at", claimedAt)
		basSemanticSetString(t, observation, "reason", "User recorded an external acceptance")
		basSemanticSetMessage(t, request, "observation", observation)
	case "ResolveExternalBasDisputeRequest":
		resolution := basSemanticMessage(t, "BasExternalDisputeInput")
		basSemanticSetEnumName(t, resolution, "outcome", "BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN")
		basSemanticAppendMessage(t, resolution, "disputed_observations", basSemanticSourceRef(t, "bas_external_observation"))
		basSemanticAppendMessage(t, resolution, "affected_confirmed_chain", basSemanticSourceRef(t, "bas_preparation"))
		basSemanticSetBytes(t, resolution, "dependent_chain_manifest_hash", basSemanticHash())
		basSemanticSetString(t, resolution, "reason", "Reviewed recorded evidence")
		basSemanticSetMessage(t, request, "resolution", resolution)
	case "ReplaceRejectedBasAttemptRequest":
		basSemanticSetString(t, request, "rejected_preparation_id", basSemanticID)
		basSemanticSetBytes(t, request, "rejected_preparation_hash", basSemanticHash())
		basSemanticSetMessage(t, request, "resolved_rejection", basSemanticSourceRef(t, "bas_external_observation"))
		basSemanticSetString(t, request, "reason", "Create a corrected replacement attempt")
	default:
		t.Fatalf("no BAS fresh-factor fixture for %s", name)
	}
	return request
}

func TestBasHighRiskCommandsRequireTheirExactFreshFactorPurpose(t *testing.T) {
	tests := []struct {
		name, purpose, idPath, hashPath string
	}{
		{"freeze", "bas_freeze_manual_handoff", "preparation_id", "preparation_hash"},
		{"unused replacement", "bas_replace_unused_attempt", "predecessor_frozen_id", "predecessor_frozen_hash"},
		{"external confirmation", "bas_record_external_confirmation", "frozen_id", "frozen_hash"},
		{"dispute resolution", "bas_resolve_external_dispute", "chain.family.id", "chain.family.content_hash"},
		{"rejected replacement", "bas_replace_rejected_attempt", "rejected_preparation_id", "rejected_preparation_hash"},
	}
	requestTypes := []string{
		"FreezeBasForManualHandoffRequest", "CreateBasReplacementRequest", "RecordExternalBasObservationRequest",
		"ResolveExternalBasDisputeRequest", "ReplaceRejectedBasAttemptRequest",
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseline := basSemanticFreshFactorRequest(t, requestTypes[index], test.purpose)
			basSemanticAccept(t, test.name+" with its exact fresh-factor purpose", baseline)

			missingFactor := basSemanticClone(t, baseline)
			basSemanticClearPath(t, missingFactor, "command_context.fresh_factor")
			basSemanticReject(t, test.name+" without fresh factor", missingFactor)

			wrongPurpose := basSemanticClone(t, baseline)
			basSemanticSetPathString(t, wrongPurpose, "command_context.fresh_factor.purpose", test.purpose+"_other")
			basSemanticReject(t, test.name+" with another operation's factor", wrongPurpose)

			badAssertion := basSemanticClone(t, baseline)
			basSemanticSetPathString(t, badAssertion, "command_context.fresh_factor.assertion_id", "not-a-uuid")
			basSemanticReject(t, test.name+" with malformed assertion id", badAssertion)

			badVersion := basSemanticClone(t, baseline)
			basSemanticSetUint64(t, badVersion, "expected_version", 0)
			basSemanticReject(t, test.name+" with zero expected version", badVersion)

			badHash := basSemanticClone(t, baseline)
			basSemanticSetPathBytes(t, badHash, test.hashPath, make([]byte, 31))
			basSemanticReject(t, test.name+" with malformed retained hash", badHash)

			badID := basSemanticClone(t, baseline)
			basSemanticSetPathString(t, badID, test.idPath, "not-a-uuid")
			basSemanticReject(t, test.name+" with malformed retained id", badID)

			badChainVersion := basSemanticClone(t, baseline)
			chain := basSemanticChild(t, badChainVersion, "chain")
			basSemanticSetUint64(t, chain, "expected_family_version", 0)
			basSemanticReject(t, test.name+" with zero chain version", badChainVersion)

			if test.name == "dispute resolution" {
				wrongDisputedObservationType := basSemanticClone(t, baseline)
				resolution := basSemanticChild(t, wrongDisputedObservationType, "resolution")
				disputed := resolution.Mutable(basSemanticField(t, resolution, "disputed_observations")).List().Get(0).Message()
				basSemanticSetString(t, disputed, "type", "bas_frozen")
				basSemanticReject(t, "dispute resolution with a frozen BAS ref in disputed observations", wrongDisputedObservationType)

				wrongConfirmedChainType := basSemanticClone(t, baseline)
				confirmedResolution := basSemanticChild(t, wrongConfirmedChainType, "resolution")
				confirmed := confirmedResolution.Mutable(basSemanticField(t, confirmedResolution, "affected_confirmed_chain")).List().Get(0).Message()
				basSemanticSetString(t, confirmed, "type", "bas_period")
				basSemanticReject(t, "dispute resolution with a period ref in affected confirmed chain", wrongConfirmedChainType)
			}

			if test.name == "rejected replacement" {
				wrongRejectionType := basSemanticClone(t, baseline)
				basSemanticSetPathString(t, wrongRejectionType, "resolved_rejection.type", "bas_preparation")
				basSemanticReject(t, "rejected replacement with a preparation ref instead of a recorded rejection", wrongRejectionType)
			}
		})
	}
}

func basSemanticRegenerateRequest(t *testing.T, expectedVersion uint64, genesis bool) *dynamicpb.Message {
	t.Helper()
	request := basSemanticMessage(t, "RegenerateBasPreparationRequest")
	basSemanticSetMessage(t, request, "command_context", basSemanticCommandContext(t, ""))
	basSemanticSetString(t, request, "organisation_id", basSemanticID)
	basSemanticSetUint64(t, request, "expected_version", expectedVersion)
	basSemanticSetMessage(t, request, "period", basSemanticSourceRef(t, "bas_period"))
	basSemanticSetMessage(t, request, "profile", basSemanticSourceRef(t, "bas_obligations_profile"))
	if genesis {
		basSemanticSetMessage(t, request, "family_expectation", basSemanticExpectedIdentity(t, true, "", 0))
		basSemanticSetMessage(t, request, "preparation_expectation", basSemanticExpectedIdentity(t, true, "", 0))
	} else {
		basSemanticSetMessage(t, request, "family_expectation", basSemanticExpectedIdentity(t, false, "bas_family", 1))
		basSemanticSetMessage(t, request, "preparation_expectation", basSemanticExpectedIdentity(t, false, "bas_preparation", expectedVersion))
	}
	return request
}

func TestBasRegeneratePinsEitherGenesisAbsenceOrTheCurrentRetainedPair(t *testing.T) {
	genesis := basSemanticRegenerateRequest(t, 0, true)
	basSemanticAccept(t, "genesis with explicit family and preparation absence", genesis)

	current := basSemanticRegenerateRequest(t, 3, false)
	basSemanticAccept(t, "existing family and exact current preparation identity", current)
	for _, baseline := range []struct {
		name    string
		request *dynamicpb.Message
	}{
		{name: "genesis", request: genesis},
		{name: "current", request: current},
	} {
		wrongPeriodType := basSemanticClone(t, baseline.request)
		basSemanticSetPathString(t, wrongPeriodType, "period.type", "bas_family")
		basSemanticReject(t, baseline.name+" with family typed as period", wrongPeriodType)

		wrongProfileType := basSemanticClone(t, baseline.request)
		basSemanticSetPathString(t, wrongProfileType, "profile.type", "bas_preparation")
		basSemanticReject(t, baseline.name+" with preparation typed as profile", wrongProfileType)
	}

	wrongFamilyType := basSemanticClone(t, current)
	basSemanticSetPathString(t, wrongFamilyType, "family_expectation.retained.type", "bas_period")
	basSemanticReject(t, "retained non-family source in family expectation", wrongFamilyType)

	wrongPreparationType := basSemanticClone(t, current)
	basSemanticSetPathString(t, wrongPreparationType, "preparation_expectation.retained.type", "bas_period")
	basSemanticReject(t, "retained non-preparation source in preparation expectation", wrongPreparationType)

	missingPreparation := basSemanticClone(t, current)
	basSemanticClearPath(t, missingPreparation, "preparation_expectation.retained")
	basSemanticReject(t, "existing family without current preparation identity", missingPreparation)

	missingFamily := basSemanticClone(t, current)
	basSemanticClearPath(t, missingFamily, "family_expectation.retained")
	basSemanticReject(t, "current preparation without retained family", missingFamily)

	wrongExpectedVersion := basSemanticClone(t, current)
	basSemanticSetUint64(t, wrongExpectedVersion, "expected_version", 4)
	basSemanticReject(t, "retained preparation version different from request CAS version", wrongExpectedVersion)

	wrongPreparationVersion := basSemanticClone(t, current)
	preparation := basSemanticChild(t, wrongPreparationVersion, "preparation_expectation")
	basSemanticSetUint64(t, preparation, "expected_version", 2)
	basSemanticReject(t, "retained preparation with stale expected version", wrongPreparationVersion)

	partialGenesis := basSemanticClone(t, genesis)
	basSemanticSetMessage(t, partialGenesis, "preparation_expectation", basSemanticExpectedIdentity(t, false, "bas_preparation", 1))
	basSemanticReject(t, "genesis with retained preparation instead of explicit absence", partialGenesis)
}

func basSemanticIncomeYear(t *testing.T) *dynamicpb.Message {
	t.Helper()
	year := basSemanticMessage(t, "BasIncomeYearIdentity")
	basSemanticSetString(t, year, "income_year_id", basSemanticID)
	dates := basSemanticMessage(t, "BasCivilDateRange")
	start := basSemanticMessage(t, "CivilDate")
	basSemanticSetInt32(t, start, "year", 2025)
	basSemanticSetInt32(t, start, "month", 7)
	basSemanticSetInt32(t, start, "day", 1)
	end := basSemanticMessage(t, "CivilDate")
	basSemanticSetInt32(t, end, "year", 2026)
	basSemanticSetInt32(t, end, "month", 6)
	basSemanticSetInt32(t, end, "day", 30)
	basSemanticSetMessage(t, dates, "start_date", start)
	basSemanticSetMessage(t, dates, "end_date", end)
	basSemanticSetMessage(t, year, "dates", dates)
	basSemanticSetMessage(t, year, "confirmation", basSemanticSourceRef(t, "income_year_confirmation"))
	return year
}

func basSemanticPaygiBase(t *testing.T, method string) *dynamicpb.Message {
	t.Helper()
	inputs := basSemanticMessage(t, "BasPaygiInputs")
	basSemanticSetEnumName(t, inputs, "method", method)
	basSemanticSetEnumName(t, inputs, "notice_operative_status", "BAS_NOTICE_OPERATIVE_STATUS_UNKNOWN")
	basSemanticSetEnumName(t, inputs, "prior_variation", "BAS_PRIOR_VARIATION_DISPOSITION_UNKNOWN")
	basSemanticSetEnumName(t, inputs, "unsupported_state", "BAS_PAYGI_UNSUPPORTED_STATE_NONE")
	basSemanticSetEnumName(t, inputs, "t1_review_decision", "BAS_T1_REVIEW_DECISION_UNKNOWN")
	return inputs
}

func TestBasPaygiInputsEnforceSelectedMethodAndExplicitT1Review(t *testing.T) {
	positive := int64(25000)
	t7 := basSemanticPaygiBase(t, "BAS_PAYGI_METHOD_AMOUNT")
	basSemanticSetMessage(t, t7, "t7_amount", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &positive, "AUD"))
	basSemanticSetMessage(t, t7, "confirmed_income_year", basSemanticIncomeYear(t))
	basSemanticAccept(t, "T7 amount method with only its amount payload", t7)

	wrongT7Shape := basSemanticClone(t, t7)
	unknownT1 := basSemanticAmountInput(t, "BAS_AMOUNT_STATE_UNKNOWN", nil, "")
	basSemanticSetMessage(t, wrongT7Shape, "rate_t1", unknownT1)
	basSemanticReject(t, "T7 method carrying rate-method T1 payload", wrongT7Shape)

	rate := basSemanticPaygiBase(t, "BAS_PAYGI_METHOD_RATE")
	basSemanticSetMessage(t, rate, "confirmed_income_year", basSemanticIncomeYear(t))
	basSemanticSetMessage(t, rate, "rate_t1", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_UNKNOWN", nil, ""))
	rateT2 := basSemanticMessage(t, "Decimal")
	basSemanticSetInt64(t, rateT2, "coefficient", 12345)
	rateScale := basSemanticField(t, rateT2, "scale")
	rateT2.Set(rateScale, protoreflect.ValueOfUint32(18))
	basSemanticSetMessage(t, rate, "rate_t2", rateT2)
	basSemanticAccept(t, "rate method with unknown T1 and exact 18-place T2", rate)

	rateWithT7 := basSemanticClone(t, rate)
	basSemanticSetMessage(t, rateWithT7, "t7_amount", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &positive, "AUD"))
	basSemanticReject(t, "rate method carrying an exclusive T7 amount payload", rateWithT7)

	rateWithoutT1 := basSemanticClone(t, rate)
	basSemanticClearPath(t, rateWithoutT1, "rate_t1")
	basSemanticReject(t, "rate method missing T1 input", rateWithoutT1)

	rateWithoutT2 := basSemanticClone(t, rate)
	basSemanticClearPath(t, rateWithoutT2, "rate_t2")
	basSemanticReject(t, "rate method missing T2 input", rateWithoutT2)

	knownT1 := basSemanticClone(t, rate)
	basSemanticSetMessage(t, knownT1, "rate_t1", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &positive, "AUD"))
	basSemanticReject(t, "known T1 amount without explicit T1 review", knownT1)
	basSemanticSetEnumName(t, knownT1, "t1_review_decision", "BAS_T1_REVIEW_DECISION_REVIEWED")
	basSemanticAccept(t, "known T1 amount after explicit review", knownT1)

	tooPrecise := basSemanticClone(t, rate)
	tooPreciseRate := basSemanticChild(t, tooPrecise, "rate_t2")
	tooPreciseRate.Set(basSemanticField(t, tooPreciseRate, "scale"), protoreflect.ValueOfUint32(19))
	basSemanticReject(t, "T2 rate with scale above 18", tooPrecise)
}

func TestBasPaygiUnknownAndInoperativeNoticeStatesRemainCollectable(t *testing.T) {
	unknown := basSemanticPaygiBase(t, "BAS_PAYGI_METHOD_UNKNOWN")
	positive := int64(1200)
	basSemanticSetMessage(t, unknown, "business_income_comparison", basSemanticAmountInput(t, "BAS_AMOUNT_STATE_KNOWN", &positive, "AUD"))
	basSemanticAccept(t, "unknown method with retained comparison input but no invented rate T1 or notice", unknown)
	if unknown.Has(basSemanticField(t, unknown, "rate_t1")) || unknown.Has(basSemanticField(t, unknown, "notice")) {
		t.Fatal("opaque comparison inputs were promoted into a PAYGI rate or an absent notice")
	}

	inoperative := basSemanticPaygiBase(t, "BAS_PAYGI_METHOD_UNKNOWN")
	basSemanticSetEnumName(t, inoperative, "notice_operative_status", "BAS_NOTICE_OPERATIVE_STATUS_INOPERATIVE")
	basSemanticSetEnumName(t, inoperative, "unsupported_state", "BAS_PAYGI_UNSUPPORTED_STATE_VARIATION_FIELDS_REQUIRED")
	basSemanticAccept(t, "inoperative notice and unsupported variation state without fabricated payload", inoperative)

	claimedOperative := basSemanticClone(t, inoperative)
	basSemanticSetEnumName(t, claimedOperative, "notice_operative_status", "BAS_NOTICE_OPERATIVE_STATUS_OPERATIVE")
	basSemanticReject(t, "operative notice without retained provenance", claimedOperative)
}

func TestBasDerivedNineRetainsExactZeroAndReportedDollarDifference(t *testing.T) {
	amount := func(state baspreparation.ValueState, cents int64) baspreparation.AmountInput {
		return baspreparation.AmountInput{State: state, Cents: cents}
	}
	calculate := func(oneBCents, w2Cents int64) (baspreparation.CalculationResult, error) {
		w2 := amount(baspreparation.ValueKnown, w2Cents)
		if w2Cents == 0 {
			w2 = amount(baspreparation.ValueKnownZero, 0)
		}
		return baspreparation.Calculate(baspreparation.CalculationInput{
			GST: baspreparation.GSTInput{
				G1:   amount(baspreparation.ValueKnown, 10_000),
				OneA: amount(baspreparation.ValueKnown, 99),
				OneB: amount(baspreparation.ValueKnown, oneBCents),
			},
			PAYGW: baspreparation.PAYGWInput{
				W1: amount(baspreparation.ValueKnown, 10_000),
				W2: w2,
				W3: amount(baspreparation.ValueKnownZero, 0),
				W4: amount(baspreparation.ValueKnownZero, 0),
			},
			PAYGI:                    baspreparation.PAYGIInput{Method: baspreparation.PAYGIMethodNotApplicable},
			OtherObligationsReviewed: true,
		})
	}
	calculation, err := calculate(198, 99)
	if err != nil {
		t.Fatalf("calculate derived label 9: %v", err)
	}
	nine := calculation.Form.Nine
	if nine.ExactCents.Numerator != "0" || nine.ExactCents.Denominator != "1" || nine.ReportedWholeDollars != 1 ||
		nine.OmittedCents.Numerator != "-100" || nine.OmittedCents.Denominator != "1" ||
		nine.State != baspreparation.ValueKnownZero || calculation.Form.Direction != baspreparation.DirectionRefundable {
		t.Fatalf("derived label 9 lost its exact/reporting distinction: trace=%#v direction=%v", nine, calculation.Form.Direction)
	}

	traceForNine := func(nine baspreparation.AmountTrace) *dynamicpb.Message {
		trace := basSemanticMessage(t, "BasLabelTrace")
		basSemanticSetEnumName(t, trace, "label", "BAS_FORM_LABEL_9")
		basSemanticSetEnumName(t, trace, "operation", "BAS_TRACE_OPERATION_ABSOLUTE_DIFFERENCE")
		basSemanticSetString(t, trace, "exact_cents_numerator", nine.ExactCents.Numerator)
		basSemanticSetString(t, trace, "exact_cents_denominator", nine.ExactCents.Denominator)
		basSemanticSetInt64(t, trace, "reported_whole_dollars", nine.ReportedWholeDollars)
		basSemanticSetString(t, trace, "rounding_difference_cents_numerator", nine.OmittedCents.Numerator)
		basSemanticSetString(t, trace, "rounding_difference_cents_denominator", nine.OmittedCents.Denominator)
		basSemanticSetString(t, trace, "rule_id", "bas.form.nine.absolute_difference")
		basSemanticSetEnumName(t, trace, "state", "BAS_AMOUNT_STATE_KNOWN_ZERO")
		return trace
	}
	trace := traceForNine(nine)
	basSemanticAccept(t, "calculated label 9 with exact zero and one reported dollar", trace)

	wrongLabel := basSemanticClone(t, trace)
	basSemanticSetEnumName(t, wrongLabel, "label", "BAS_FORM_LABEL_G1")
	basSemanticReject(t, "known-zero exception applied to a non-nine label", wrongLabel)

	wrongOperation := basSemanticClone(t, trace)
	basSemanticSetEnumName(t, wrongOperation, "operation", "BAS_TRACE_OPERATION_INPUT")
	basSemanticReject(t, "known-zero exception applied to a non-difference operation", wrongOperation)

	positiveOmission := basSemanticClone(t, trace)
	basSemanticSetString(t, positiveOmission, "rounding_difference_cents_numerator", "100")
	basSemanticReject(t, "derived label 9 with a positive omission difference", positiveOmission)

	zeroOmission := basSemanticClone(t, trace)
	basSemanticSetString(t, zeroOmission, "rounding_difference_cents_numerator", "0")
	basSemanticReject(t, "derived label 9 with zero omission despite a reported dollar", zeroOmission)

	zeroCalculation, err := calculate(99, 0)
	if err != nil {
		t.Fatalf("calculate zero reported label 9: %v", err)
	}
	zeroNine := zeroCalculation.Form.Nine
	if zeroNine.ExactCents.Numerator != "0" || zeroNine.ExactCents.Denominator != "1" || zeroNine.ReportedWholeDollars != 0 ||
		zeroNine.OmittedCents.Numerator != "0" || zeroNine.OmittedCents.Denominator != "1" ||
		zeroNine.State != baspreparation.ValueKnownZero || zeroCalculation.Form.Direction != baspreparation.DirectionNil {
		t.Fatalf("zero label 9 fixture changed: trace=%#v direction=%v", zeroNine, zeroCalculation.Form.Direction)
	}
	basSemanticAccept(t, "zero-exact label 9 with zero reported dollars and no omission", traceForNine(zeroNine))
}

func basSemanticCivilDate(t *testing.T, year, month, day int32) *dynamicpb.Message {
	t.Helper()
	date := basSemanticMessage(t, "CivilDate")
	basSemanticSetInt32(t, date, "year", year)
	basSemanticSetInt32(t, date, "month", month)
	basSemanticSetInt32(t, date, "day", day)
	return date
}

func basSemanticCalendarRequest(t *testing.T, endYear int32) *dynamicpb.Message {
	t.Helper()
	request := basSemanticMessage(t, "GetBasCalendarRequest")
	authentication := basSemanticMessage(t, "AuthenticationContext")
	basSemanticSetString(t, authentication, "actor_user_id", basSemanticID)
	basSemanticSetString(t, authentication, "session_id", basSemanticID)
	basSemanticSetMessage(t, request, "authentication", authentication)
	basSemanticSetString(t, request, "organisation_id", basSemanticID)
	basSemanticSetMessage(t, request, "as_of_date", basSemanticCivilDate(t, 2026, 10, 10))
	horizon := basSemanticMessage(t, "BasCivilDateRange")
	basSemanticSetMessage(t, horizon, "start_date", basSemanticCivilDate(t, 2020, 1, 1))
	basSemanticSetMessage(t, horizon, "end_date", basSemanticCivilDate(t, endYear, 12, 31))
	basSemanticSetMessage(t, request, "horizon", horizon)
	return request
}

func TestBasCalendarRequestLimitsHorizonToTenCalendarYears(t *testing.T) {
	tenYears := basSemanticCalendarRequest(t, 2029)
	basSemanticAccept(t, "authenticated ten-calendar-year horizon from 2020 through 2029", tenYears)
	basSemanticReject(t, "authenticated eleven-calendar-year horizon from 2020 through 2030", basSemanticCalendarRequest(t, 2030))
}

func TestBasCalendarOptionalProfileMustBeAnObligationsProfile(t *testing.T) {
	basSemanticAccept(t, "calendar request without an optional profile", basSemanticCalendarRequest(t, 2029))

	withProfile := basSemanticCalendarRequest(t, 2029)
	basSemanticSetMessage(t, withProfile, "profile", basSemanticSourceRef(t, "bas_obligations_profile"))
	basSemanticAccept(t, "calendar request pinned to an obligations profile", withProfile)

	wrongProfileType := basSemanticClone(t, withProfile)
	basSemanticSetPathString(t, wrongProfileType, "profile.type", "bas_preparation")
	basSemanticReject(t, "calendar request with a preparation ref in the profile filter", wrongProfileType)
}

func basSemanticPeriodConfirmationInput(t *testing.T) *dynamicpb.Message {
	t.Helper()
	confirmation := basSemanticMessage(t, "BasPeriodConfirmationInput")
	period := basSemanticMessage(t, "BasCivilDateRange")
	basSemanticSetMessage(t, period, "start_date", basSemanticCivilDate(t, 2025, 7, 1))
	basSemanticSetMessage(t, period, "end_date", basSemanticCivilDate(t, 2025, 9, 30))
	basSemanticSetMessage(t, confirmation, "period", period)
	basSemanticSetEnumName(t, confirmation, "window", "BAS_PERIOD_WINDOW_JUL_SEP")
	basSemanticSetMessage(t, confirmation, "profile", basSemanticSourceRef(t, "bas_obligations_profile"))
	basSemanticSetMessage(t, confirmation, "nominal_due_date", basSemanticCivilDate(t, 2025, 10, 28))
	basSemanticSetString(t, confirmation, "due_date_rule_version", "bas-due-date-v1")
	basSemanticSetBytes(t, confirmation, "due_date_rule_hash", basSemanticHash())
	basSemanticSetMessage(t, confirmation, "confirmed_due_date", basSemanticCivilDate(t, 2025, 10, 28))
	basSemanticSetEnumName(t, confirmation, "due_date_kind", "BAS_DUE_DATE_KIND_NORMAL_SUGGESTION")
	basSemanticSetEnumName(t, confirmation, "support_state", "BAS_PERIOD_SUPPORT_STATE_SUPPORTED")
	basSemanticSetString(t, confirmation, "reason", "Confirmed the normal quarterly due date")
	return confirmation
}

func TestBasPeriodConfirmationRequiresAnObligationsProfileReference(t *testing.T) {
	baseline := basSemanticPeriodConfirmationInput(t)
	basSemanticAccept(t, "complete normal-due-date period confirmation with obligations profile", baseline)

	wrongProfileType := basSemanticClone(t, baseline)
	basSemanticSetPathString(t, wrongProfileType, "profile.type", "bas_preparation")
	basSemanticReject(t, "period confirmation with a preparation ref in the profile field", wrongProfileType)
}

func basSemanticTimestamp(t *testing.T, seconds int64) *dynamicpb.Message {
	t.Helper()
	timestamp := basSemanticMessage(t, "google.protobuf.Timestamp")
	basSemanticSetInt64(t, timestamp, "seconds", seconds)
	return timestamp
}

func basSemanticExternalObservationInput(t *testing.T) *dynamicpb.Message {
	t.Helper()
	input := basSemanticMessage(t, "BasExternalObservationInput")
	basSemanticSetEnumName(t, input, "event_kind", "BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE")
	basSemanticSetEnumName(t, input, "channel", "BAS_EXTERNAL_CHANNEL_ONLINE_PORTAL")
	basSemanticSetMessage(t, input, "claimed_event_at", basSemanticTimestamp(t, 1))
	basSemanticSetString(t, input, "reason", "User recorded an external acceptance")
	return input
}

func basSemanticExternalObservationBase(t *testing.T) *dynamicpb.Message {
	t.Helper()
	observation := basSemanticMessage(t, "BasExternalObservation")
	basSemanticSetString(t, observation, "observation_id", basSemanticID)
	basSemanticSetString(t, observation, "organisation_id", basSemanticID)
	basSemanticSetMessage(t, observation, "frozen_bas", basSemanticSourceRef(t, "bas_frozen"))
	basSemanticSetEnumName(t, observation, "trust", "BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED")
	basSemanticSetString(t, observation, "actor_user_id", basSemanticID)
	basSemanticSetMessage(t, observation, "recorded_at", basSemanticTimestamp(t, 1))
	basSemanticSetBytes(t, observation, "content_hash", basSemanticHash())
	return observation
}

func basSemanticExternalCorrectionInput(t *testing.T, kind string, correctedEvent *string, correctedAt bool) *dynamicpb.Message {
	t.Helper()
	correction := basSemanticMessage(t, "BasExternalObservationCorrectionInput")
	basSemanticSetMessage(t, correction, "disputed_observation", basSemanticSourceRef(t, "bas_external_observation"))
	basSemanticSetEnumName(t, correction, "correction_kind", kind)
	if correctedEvent != nil {
		basSemanticSetEnumName(t, correction, "corrected_event_kind", *correctedEvent)
	}
	if correctedAt {
		basSemanticSetMessage(t, correction, "corrected_claimed_event_at", basSemanticTimestamp(t, 2))
	}
	basSemanticSetString(t, correction, "reason", "Correct the retained external observation")
	return correction
}

func basSemanticExternalObservationCorrectionRecord(t *testing.T, correction *dynamicpb.Message) *dynamicpb.Message {
	t.Helper()
	observation := basSemanticExternalObservationBase(t)
	basSemanticSetString(t, observation, "observation_id", basSemanticCorrectionRecordID)
	basSemanticSetMessage(t, observation, "corrects_observation", basSemanticSourceRef(t, "bas_external_observation"))
	basSemanticSetMessage(t, observation, "correction", correction)
	return observation
}

func basSemanticExternalObservationSummary(t *testing.T, correction bool) *dynamicpb.Message {
	t.Helper()
	summary := basSemanticMessage(t, "BasExternalObservationSummary")
	observationRef := basSemanticSourceRef(t, "bas_external_observation")
	if correction {
		basSemanticSetString(t, observationRef, "id", basSemanticCorrectionRecordID)
	}
	basSemanticSetMessage(t, summary, "observation", observationRef)
	basSemanticSetMessage(t, summary, "frozen_bas", basSemanticSourceRef(t, "bas_frozen"))
	basSemanticSetEnumName(t, summary, "trust", "BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED")
	basSemanticSetMessage(t, summary, "recorded_at", basSemanticTimestamp(t, 1))
	if correction {
		basSemanticSetEnumName(t, summary, "correction_kind", "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT")
		basSemanticSetMessage(t, summary, "corrects_observation", basSemanticSourceRef(t, "bas_external_observation"))
	} else {
		basSemanticSetEnumName(t, summary, "event_kind", "BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE")
	}
	return summary
}

func basSemanticRoundTrip(t *testing.T, message *dynamicpb.Message) *dynamicpb.Message {
	t.Helper()
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		t.Fatalf("marshal %s: %v", message.Descriptor().FullName(), err)
	}
	decoded := dynamicpb.NewMessage(message.Descriptor())
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", message.Descriptor().FullName(), err)
	}
	if !proto.Equal(message, decoded) {
		t.Fatalf("%s changed in deterministic wire round trip", message.Descriptor().FullName())
	}
	return decoded
}

func TestBasExternalObservationRetainsTypedCorrectionHistory(t *testing.T) {
	observationDescriptor := requireMessage(t, "tammy.v1.BasExternalObservation")
	observationField := observationDescriptor.Fields().ByName("observation")
	correctionField := observationDescriptor.Fields().ByName("correction")
	if observationField == nil || observationField.Number() != 4 {
		t.Fatalf("regular observation payload must remain field 4, got %v", observationField)
	}
	if correctionField == nil || correctionField.Number() != 10 {
		t.Fatalf("typed correction payload must be retained as field 10, got %v", correctionField)
	}
	if observationField.ContainingOneof() == nil || observationField.ContainingOneof() != correctionField.ContainingOneof() {
		t.Fatal("regular and correction payloads must be mutually exclusive alternatives")
	}

	regular := basSemanticExternalObservationBase(t)
	basSemanticSetMessage(t, regular, "observation", basSemanticExternalObservationInput(t))
	basSemanticAccept(t, "ordinary external observation", regular)
	if regular.Has(basSemanticField(t, regular, "corrects_observation")) {
		t.Fatal("ordinary observation unexpectedly records a correction subject")
	}
	regularRoundTrip := basSemanticRoundTrip(t, regular)
	regularInput := basSemanticChild(t, regularRoundTrip, "observation")
	if regularInput.Get(basSemanticField(t, regularInput, "event_kind")).Enum() !=
		basSemanticField(t, regularInput, "event_kind").Enum().Values().ByName("BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE").Number() {
		t.Fatal("wire round trip lost the ordinary observation event kind")
	}

	unknownEvent := "BAS_EXTERNAL_EVENT_KIND_UNKNOWN"
	replacement := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_REPLACE_ASSERTION", &unknownEvent, false))
	basSemanticAccept(t, "replacement correction retaining an explicitly unknown corrected event", replacement)
	replacementRoundTrip := basSemanticRoundTrip(t, replacement)
	replacementInput := basSemanticChild(t, replacementRoundTrip, "correction")
	if replacementInput.Get(basSemanticField(t, replacementInput, "correction_kind")).Enum() !=
		basSemanticField(t, replacementInput, "correction_kind").Enum().Values().ByName("BAS_CORRECTION_KIND_REPLACE_ASSERTION").Number() ||
		!replacementInput.Has(basSemanticField(t, replacementInput, "corrected_event_kind")) {
		t.Fatal("wire round trip lost replacement kind or its explicit unknown event")
	}

	retraction := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT", nil, false))
	basSemanticAccept(t, "retraction without fabricated replacement event or time", retraction)
	retractionRoundTrip := basSemanticRoundTrip(t, retraction)
	retractionInput := basSemanticChild(t, retractionRoundTrip, "correction")
	if retractionInput.Get(basSemanticField(t, retractionInput, "correction_kind")).Enum() !=
		basSemanticField(t, retractionInput, "correction_kind").Enum().Values().ByName("BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT").Number() {
		t.Fatal("wire round trip changed a retraction into another correction kind")
	}
	if retractionInput.Has(basSemanticField(t, retractionInput, "corrected_event_kind")) ||
		retractionInput.Has(basSemanticField(t, retractionInput, "corrected_claimed_event_at")) {
		t.Fatal("wire round trip invented replacement details for a retraction")
	}

	timeOnlyReplacement := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_REPLACE_ASSERTION", nil, true))
	basSemanticAccept(t, "replacement correction retaining only corrected event time", timeOnlyReplacement)
	timeOnlyRoundTrip := basSemanticRoundTrip(t, timeOnlyReplacement)
	timeOnlyInput := basSemanticChild(t, timeOnlyRoundTrip, "correction")
	if timeOnlyInput.Get(basSemanticField(t, timeOnlyInput, "correction_kind")).Enum() !=
		basSemanticField(t, timeOnlyInput, "correction_kind").Enum().Values().ByName("BAS_CORRECTION_KIND_REPLACE_ASSERTION").Number() ||
		!timeOnlyInput.Has(basSemanticField(t, timeOnlyInput, "corrected_claimed_event_at")) ||
		timeOnlyInput.Has(basSemanticField(t, timeOnlyInput, "corrected_event_kind")) {
		t.Fatal("wire round trip lost the time-only replacement correction distinction")
	}

	noCorrectionDetail := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_REPLACE_ASSERTION", nil, false))
	basSemanticReject(t, "replacement correction without corrected event or time", noCorrectionDetail)

	retractionWithEvent := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT", &unknownEvent, false))
	basSemanticReject(t, "retraction carrying a corrected event", retractionWithEvent)

	retractionWithTime := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT", nil, true))
	basSemanticReject(t, "retraction carrying corrected event time", retractionWithTime)

	missingPayload := basSemanticExternalObservationBase(t)
	basSemanticReject(t, "retained external observation with no oneof payload", missingPayload)

	mismatchedSubject := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT", nil, false))
	wrongSubject := basSemanticSourceRef(t, "bas_external_observation")
	basSemanticSetString(t, wrongSubject, "id", basSemanticOtherObservationID)
	basSemanticSetMessage(t, mismatchedSubject, "corrects_observation", wrongSubject)
	basSemanticReject(t, "correction whose retained subject differs from its typed disputed subject", mismatchedSubject)

	subjectHashMismatch := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT", nil, false))
	differentHash := basSemanticSourceRef(t, "bas_external_observation")
	basSemanticSetBytes(t, differentHash, "content_hash", bytes.Repeat([]byte{0x33}, 32))
	basSemanticSetMessage(t, subjectHashMismatch, "corrects_observation", differentHash)
	basSemanticReject(t, "correction whose subject hash differs from its typed disputed source", subjectHashMismatch)

	subjectRevisionMismatch := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT", nil, false))
	differentRevision := basSemanticSourceRef(t, "bas_external_observation")
	basSemanticSetUint64(t, differentRevision, "revision", 2)
	basSemanticSetMessage(t, subjectRevisionMismatch, "corrects_observation", differentRevision)
	basSemanticReject(t, "correction whose subject revision differs from its typed disputed source", subjectRevisionMismatch)

	selfTarget := basSemanticExternalObservationCorrectionRecord(t,
		basSemanticExternalCorrectionInput(t, "BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT", nil, false))
	basSemanticSetString(t, selfTarget, "observation_id", basSemanticID)
	basSemanticReject(t, "correction that targets its own retained record id", selfTarget)

	regularWithCorrectionRef := basSemanticExternalObservationBase(t)
	basSemanticSetMessage(t, regularWithCorrectionRef, "observation", basSemanticExternalObservationInput(t))
	basSemanticSetMessage(t, regularWithCorrectionRef, "corrects_observation", basSemanticSourceRef(t, "bas_external_observation"))
	basSemanticReject(t, "ordinary event payload also claiming correction lineage", regularWithCorrectionRef)

	basSemanticAccept(t, "ordinary event summary", basSemanticExternalObservationSummary(t, false))
	correctionSummary := basSemanticExternalObservationSummary(t, true)
	basSemanticAccept(t, "retraction summary with correction kind and subject", correctionSummary)
	missingCorrectionSubject := basSemanticClone(t, correctionSummary)
	basSemanticClearPath(t, missingCorrectionSubject, "corrects_observation")
	basSemanticReject(t, "correction summary without its corrected observation", missingCorrectionSubject)

	mislabelledRetraction := basSemanticClone(t, correctionSummary)
	basSemanticSetEnumName(t, mislabelledRetraction, "event_kind", "BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE")
	basSemanticReject(t, "retraction summary also claiming ordinary acceptance", mislabelledRetraction)
}

func basSemanticCreateRevisionRequest(t *testing.T) *dynamicpb.Message {
	t.Helper()
	request := basSemanticMessage(t, "CreateBasRevisionRequest")
	basSemanticSetMessage(t, request, "command_context", basSemanticCommandContext(t, ""))
	basSemanticSetString(t, request, "organisation_id", basSemanticID)
	basSemanticSetUint64(t, request, "expected_version", 2)
	basSemanticSetMessage(t, request, "chain", basSemanticChain(t))
	basSemanticSetString(t, request, "effective_original_preparation_id", basSemanticID)
	basSemanticSetBytes(t, request, "effective_original_preparation_hash", basSemanticHash())
	basSemanticSetString(t, request, "latest_confirmed_head_id", basSemanticID)
	basSemanticSetBytes(t, request, "latest_confirmed_head_hash", basSemanticHash())
	basSemanticSetMessage(t, request, "period", basSemanticSourceRef(t, "bas_period"))
	basSemanticSetMessage(t, request, "profile", basSemanticSourceRef(t, "bas_obligations_profile"))
	basSemanticSetString(t, request, "reason", "Revise the retained BAS preparation")
	return request
}

func TestBasCreateRevisionRequiresTypedPeriodAndProfileSources(t *testing.T) {
	baseline := basSemanticCreateRevisionRequest(t)
	basSemanticAccept(t, "revision with retained period and obligations profile", baseline)

	wrongPeriodType := basSemanticClone(t, baseline)
	basSemanticSetPathString(t, wrongPeriodType, "period.type", "bas_preparation")
	basSemanticReject(t, "revision with preparation typed as period", wrongPeriodType)

	wrongProfileType := basSemanticClone(t, baseline)
	basSemanticSetPathString(t, wrongProfileType, "profile.type", "bas_period")
	basSemanticReject(t, "revision with period typed as profile", wrongProfileType)
}

func basSemanticAuthenticationContext(t *testing.T) *dynamicpb.Message {
	t.Helper()
	authentication := basSemanticMessage(t, "AuthenticationContext")
	basSemanticSetString(t, authentication, "actor_user_id", basSemanticID)
	basSemanticSetString(t, authentication, "session_id", basSemanticID)
	return authentication
}

func basSemanticExternalLookupRequest(t *testing.T, name, idField, hashField string) *dynamicpb.Message {
	t.Helper()
	request := basSemanticMessage(t, name)
	basSemanticSetMessage(t, request, "authentication", basSemanticAuthenticationContext(t))
	basSemanticSetString(t, request, "organisation_id", basSemanticID)
	basSemanticSetString(t, request, idField, basSemanticID)
	basSemanticSetBytes(t, request, hashField, basSemanticHash())
	return request
}

func TestBasExternalObservationAndResolutionLookupsRequireExactIdentity(t *testing.T) {
	for _, test := range []struct {
		name, messageName, idField, hashField string
	}{
		{name: "observation", messageName: "GetBasExternalObservationRequest", idField: "observation_id", hashField: "observation_hash"},
		{name: "dispute resolution", messageName: "GetBasExternalDisputeResolutionRequest", idField: "resolution_id", hashField: "resolution_hash"},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseline := basSemanticExternalLookupRequest(t, test.messageName, test.idField, test.hashField)
			basSemanticAccept(t, test.name+" exact authenticated lookup identity", baseline)

			missingAuthentication := basSemanticClone(t, baseline)
			basSemanticClearPath(t, missingAuthentication, "authentication")
			basSemanticReject(t, test.name+" lookup without authentication", missingAuthentication)

			shortHash := basSemanticClone(t, baseline)
			basSemanticSetBytes(t, shortHash, test.hashField, make([]byte, 31))
			basSemanticReject(t, test.name+" lookup with a short identity hash", shortHash)

			malformedID := basSemanticClone(t, baseline)
			basSemanticSetString(t, malformedID, test.idField, "not-a-uuid")
			basSemanticReject(t, test.name+" lookup with malformed exact id", malformedID)
		})
	}
}

func basSemanticExternalDisputeResolutionSummary(t *testing.T) *dynamicpb.Message {
	t.Helper()
	summary := basSemanticMessage(t, "BasExternalDisputeResolutionSummary")
	basSemanticSetMessage(t, summary, "resolution", basSemanticSourceRef(t, "bas_external_dispute_resolution"))
	basSemanticSetMessage(t, summary, "family", basSemanticSourceRef(t, "bas_family"))
	basSemanticSetEnumName(t, summary, "outcome", "BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN")
	basSemanticSetMessage(t, summary, "resolved_at", basSemanticTimestamp(t, 1))
	return summary
}

func basSemanticListExternalDisputeResolutionsRequest(t *testing.T, withFamily bool, cursor string) *dynamicpb.Message {
	t.Helper()
	request := basSemanticMessage(t, "ListBasExternalDisputeResolutionsRequest")
	basSemanticSetMessage(t, request, "authentication", basSemanticAuthenticationContext(t))
	basSemanticSetString(t, request, "organisation_id", basSemanticID)
	if withFamily {
		basSemanticSetString(t, request, "family_id", basSemanticID)
		basSemanticSetBytes(t, request, "family_hash", basSemanticHash())
	}
	page := basSemanticMessage(t, "PageRequest")
	page.Set(basSemanticField(t, page, "page_size"), protoreflect.ValueOfUint32(200))
	if cursor != "" {
		basSemanticSetString(t, page, "cursor", cursor)
	}
	basSemanticSetMessage(t, request, "page", page)
	return request
}

func basSemanticListExternalDisputeResolutionsResponse(t *testing.T, count int, cursor string) *dynamicpb.Message {
	t.Helper()
	response := basSemanticMessage(t, "ListBasExternalDisputeResolutionsResponse")
	for i := 0; i < count; i++ {
		basSemanticAppendMessage(t, response, "resolutions", basSemanticExternalDisputeResolutionSummary(t))
	}
	page := basSemanticMessage(t, "PageInfo")
	page.Set(basSemanticField(t, page, "returned_count"), protoreflect.ValueOfUint32(uint32(count)))
	if cursor != "" {
		basSemanticSetString(t, page, "next_cursor", cursor)
	}
	basSemanticSetMessage(t, response, "page", page)
	return response
}

func TestBasExternalDisputeResolutionListIsPairedAndBounded(t *testing.T) {
	withFamily := basSemanticListExternalDisputeResolutionsRequest(t, true, "")
	basSemanticAccept(t, "list scoped to an exact family identity", withFamily)
	basSemanticAccept(t, "list without optional family filter", basSemanticListExternalDisputeResolutionsRequest(t, false, ""))

	missingFamilyHash := basSemanticClone(t, withFamily)
	basSemanticClearPath(t, missingFamilyHash, "family_hash")
	basSemanticReject(t, "family id without paired hash", missingFamilyHash)

	missingFamilyID := basSemanticClone(t, withFamily)
	basSemanticClearPath(t, missingFamilyID, "family_id")
	basSemanticReject(t, "family hash without paired id", missingFamilyID)

	missingPage := basSemanticClone(t, withFamily)
	basSemanticClearPath(t, missingPage, "page")
	basSemanticReject(t, "external dispute resolution list without page request", missingPage)

	missingAuthentication := basSemanticClone(t, withFamily)
	basSemanticClearPath(t, missingAuthentication, "authentication")
	basSemanticReject(t, "external dispute resolution list without authentication", missingAuthentication)

	malformedOrganisation := basSemanticClone(t, withFamily)
	basSemanticSetString(t, malformedOrganisation, "organisation_id", "not-a-uuid")
	basSemanticReject(t, "external dispute resolution list with malformed organisation id", malformedOrganisation)

	oversizedPage := basSemanticClone(t, withFamily)
	oversizedPageRequest := basSemanticChild(t, oversizedPage, "page")
	oversizedPageRequest.Set(basSemanticField(t, oversizedPageRequest, "page_size"), protoreflect.ValueOfUint32(201))
	basSemanticReject(t, "external dispute resolution list page size above 200", oversizedPage)

	basSemanticAccept(t, "request cursor at the 2048-character bound", basSemanticListExternalDisputeResolutionsRequest(t, false, strings.Repeat("c", 2048)))
	basSemanticReject(t, "request cursor beyond the 2048-character bound", basSemanticListExternalDisputeResolutionsRequest(t, false, strings.Repeat("c", 2049)))

	basSemanticAccept(t, "response with 200 shallow resolution summaries", basSemanticListExternalDisputeResolutionsResponse(t, 200, strings.Repeat("n", 2048)))
	basSemanticReject(t, "response with 201 summaries", basSemanticListExternalDisputeResolutionsResponse(t, 201, ""))
	basSemanticReject(t, "response cursor beyond the 2048-character bound", basSemanticListExternalDisputeResolutionsResponse(t, 0, strings.Repeat("n", 2049)))
}

func basSemanticPreparationSnapshot(t *testing.T) *dynamicpb.Message {
	t.Helper()
	preparation := basSemanticMessage(t, "BasPreparation")
	basSemanticSetString(t, preparation, "preparation_id", basSemanticID)
	basSemanticSetString(t, preparation, "organisation_id", basSemanticID)
	basSemanticSetUint64(t, preparation, "version", 1)
	basSemanticSetMessage(t, preparation, "family", basSemanticSourceRef(t, "bas_family"))
	basSemanticSetMessage(t, preparation, "period", basSemanticSourceRef(t, "bas_period"))
	basSemanticSetMessage(t, preparation, "profile", basSemanticSourceRef(t, "bas_obligations_profile"))
	basSemanticSetEnumName(t, preparation, "state", "BAS_PREPARATION_STATE_COLLECTING")
	basSemanticSetEnumName(t, preparation, "dispute_overlay", "BAS_DISPUTE_OVERLAY_STATE_NONE")
	basSemanticSetBytes(t, preparation, "source_snapshot_hash", basSemanticHash())
	basSemanticSetMessage(t, preparation, "created_at", basSemanticTimestamp(t, 1))
	basSemanticSetMessage(t, preparation, "updated_at", basSemanticTimestamp(t, 1))
	basSemanticSetBytes(t, preparation, "content_hash", basSemanticHash())
	basSemanticSetUint64(t, preparation, "input_revision", 1)
	basSemanticSetBytes(t, preparation, "input_hash", basSemanticHash())
	return preparation
}

func basSemanticPaygwInputs(t *testing.T) *dynamicpb.Message {
	t.Helper()
	inputs := basSemanticMessage(t, "BasPaygwInputs")
	basSemanticSetEnumName(t, inputs, "category", "BAS_PAYGW_CATEGORY_UNKNOWN")
	for _, name := range []string{"w1_total_payments", "w2_amount_withheld", "w3_other_withholding", "w4_no_abn_withholding"} {
		basSemanticSetMessage(t, inputs, name, basSemanticAmountInput(t, "BAS_AMOUNT_STATE_UNKNOWN", nil, ""))
	}
	basSemanticSetEnumName(t, inputs, "prefill_review", "BAS_PREFILL_REQUIREMENT_UNKNOWN")
	return inputs
}

func basSemanticPreparationResponse(t *testing.T, withPaygw, withPaygi bool) *dynamicpb.Message {
	t.Helper()
	response := basSemanticMessage(t, "GetBasPreparationResponse")
	basSemanticSetMessage(t, response, "preparation", basSemanticPreparationSnapshot(t))
	basSemanticSetMessage(t, response, "current_chain", basSemanticChain(t))
	if withPaygw {
		basSemanticSetMessage(t, response, "paygw_inputs", basSemanticPaygwInputs(t))
	}
	if withPaygi {
		basSemanticSetMessage(t, response, "paygi_inputs", basSemanticPaygiBase(t, "BAS_PAYGI_METHOD_UNKNOWN"))
	}
	return response
}

func TestBasPreparationResponseRetainsSavedInputsAndWarningAcknowledgements(t *testing.T) {
	descriptor := requireMessage(t, "tammy.v1.GetBasPreparationResponse")
	acknowledgements := descriptor.Fields().ByName("acknowledgements")
	if acknowledgements == nil || !acknowledgements.IsList() || acknowledgements.Kind() != protoreflect.MessageKind ||
		acknowledgements.Message().FullName() != "tammy.v1.BasWarningAcknowledgement" || acknowledgements.Number() != 5 {
		t.Fatalf("response must retain bounded BasWarningAcknowledgement history in field 5, got %v", acknowledgements)
	}
	if fieldRules(t, acknowledgements).GetRepeated().GetMaxItems() != 200 {
		t.Fatalf("response acknowledgement history must be capped at 200, got %d", fieldRules(t, acknowledgements).GetRepeated().GetMaxItems())
	}
	for _, test := range []struct {
		name, messageName string
		number            protoreflect.FieldNumber
	}{
		{name: "PAYGW", messageName: "tammy.v1.BasPaygwInputs", number: 6},
		{name: "PAYGI", messageName: "tammy.v1.BasPaygiInputs", number: 7},
	} {
		field := descriptor.Fields().ByName(protoreflect.Name(strings.ToLower(test.name) + "_inputs"))
		if field == nil || field.IsList() || field.Kind() != protoreflect.MessageKind || !field.HasPresence() ||
			field.Message().FullName() != protoreflect.FullName(test.messageName) || field.Number() != test.number {
			t.Fatalf("response must retain optional %s inputs in field %d, got %v", test.name, test.number, field)
		}
	}

	beforeInputsSaved := basSemanticPreparationResponse(t, false, false)
	basSemanticAccept(t, "preparation response before PAYGW/PAYGI inputs are saved", beforeInputsSaved)

	retainedInputs := basSemanticPreparationResponse(t, true, true)
	basSemanticAccept(t, "preparation response with both saved PAYGW and PAYGI inputs", retainedInputs)

	missingPaygw := basSemanticClone(t, retainedInputs)
	basSemanticClearPath(t, missingPaygw, "paygw_inputs")
	basSemanticReject(t, "preparation response with PAYGI but no paired PAYGW snapshot", missingPaygw)

	missingPaygi := basSemanticClone(t, retainedInputs)
	basSemanticClearPath(t, missingPaygi, "paygi_inputs")
	basSemanticReject(t, "preparation response with PAYGW but no paired PAYGI snapshot", missingPaygi)
}
