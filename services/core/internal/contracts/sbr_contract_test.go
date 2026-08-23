package contracts_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestSbrPublicContractIsClosedAndRedacted(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/sbr.proto")
	if err != nil {
		t.Fatalf("SBR descriptor missing: %v", err)
	}
	if got := tammyv1connect.SbrServiceName; got != "tammy.v1.SbrService" {
		t.Fatalf("SBR generated service name = %q", got)
	}
	if file.Services().Len() != 1 {
		t.Fatalf("SBR service count = %d, want exactly one", file.Services().Len())
	}
	assertExactSbrMessageSchemas(t, file)

	service := file.Services().ByName("SbrService")
	if service == nil {
		t.Fatal("tammy.v1.SbrService descriptor missing")
	}
	wantMethods := []struct {
		name   protoreflect.Name
		input  protoreflect.FullName
		output protoreflect.FullName
	}{
		{"GetSbrReadiness", "tammy.v1.GetSbrReadinessRequest", "tammy.v1.GetSbrReadinessResponse"},
		{"ImportMachineCredential", "tammy.v1.ImportMachineCredentialRequest", "tammy.v1.ImportMachineCredentialResponse"},
		{"GetMachineCredentialStatus", "tammy.v1.GetMachineCredentialStatusRequest", "tammy.v1.GetMachineCredentialStatusResponse"},
		{"UnlockMachineCredential", "tammy.v1.UnlockMachineCredentialRequest", "tammy.v1.UnlockMachineCredentialResponse"},
		{"ReplaceMachineCredential", "tammy.v1.ReplaceMachineCredentialRequest", "tammy.v1.ReplaceMachineCredentialResponse"},
		{"RemoveMachineCredential", "tammy.v1.RemoveMachineCredentialRequest", "tammy.v1.RemoveMachineCredentialResponse"},
		{"ImportSbrProductId", "tammy.v1.ImportSbrProductIdRequest", "tammy.v1.ImportSbrProductIdResponse"},
		{"RemoveSbrProductId", "tammy.v1.RemoveSbrProductIdRequest", "tammy.v1.RemoveSbrProductIdResponse"},
		{"RunSbrReadinessFixture", "tammy.v1.RunSbrReadinessFixtureRequest", "tammy.v1.RunSbrReadinessFixtureResponse"},
	}
	if service.Methods().Len() != len(wantMethods) {
		t.Fatalf("SbrService method count = %d, want %d", service.Methods().Len(), len(wantMethods))
	}
	for index, want := range wantMethods {
		method := service.Methods().Get(index)
		if method.Name() != want.name || method.Input().FullName() != want.input || method.Output().FullName() != want.output {
			t.Errorf("SbrService method %d = %s(%s) returns (%s), want %s(%s) returns (%s)", index, method.Name(), method.Input().FullName(), method.Output().FullName(), want.name, want.input, want.output)
		}
		if method.IsStreamingClient() || method.IsStreamingServer() {
			t.Errorf("SbrService.%s must have client_streaming=false and server_streaming=false", want.name)
		}
	}

	assertEnumValues(t, "tammy.v1.SbrEnvironment", names(
		"SBR_ENVIRONMENT_UNSPECIFIED", "SBR_ENVIRONMENT_SIMULATOR", "SBR_ENVIRONMENT_EVTE",
	))
	assertEnumValues(t, "tammy.v1.SbrReadinessState", names(
		"SBR_READINESS_STATE_UNAVAILABLE",
		"SBR_READINESS_STATE_READY_FOR_SIMULATOR",
		"SBR_READINESS_STATE_READY_FOR_EVTE_PRE_CONFORMANCE",
		"SBR_READINESS_STATE_READY_FOR_EVTE_POST_CONFORMANCE",
	))
	assertEnumValues(t, "tammy.v1.MachineCredentialState", names(
		"MACHINE_CREDENTIAL_STATE_MISSING",
		"MACHINE_CREDENTIAL_STATE_PRESENT",
		"MACHINE_CREDENTIAL_STATE_INACCESSIBLE",
		"MACHINE_CREDENTIAL_STATE_INCOMPATIBLE",
		"MACHINE_CREDENTIAL_STATE_REVOKED",
		"MACHINE_CREDENTIAL_STATE_EXPIRED",
		"MACHINE_CREDENTIAL_STATE_ABN_MISMATCH",
	))
	assertEnumValues(t, "tammy.v1.ProductIdState", names(
		"PRODUCT_ID_STATE_UNSPECIFIED",
		"PRODUCT_ID_STATE_PRESENT",
		"PRODUCT_ID_STATE_MISSING",
		"PRODUCT_ID_STATE_INACCESSIBLE",
	))
	assertEnumValues(t, "tammy.v1.SbrReadinessFixtureFailure", names(
		"SBR_READINESS_FIXTURE_FAILURE_UNSPECIFIED",
		"SBR_READINESS_FIXTURE_FAILURE_NOT_STARTED",
		"SBR_READINESS_FIXTURE_FAILURE_MAYBE_SENT",
		"SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE",
		"SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH",
		"SBR_READINESS_FIXTURE_FAILURE_TIMEOUT",
		"SBR_READINESS_FIXTURE_FAILURE_UNKNOWN",
	))
	assertEnumValues(t, "tammy.v1.SbrReadinessFixtureOutcome", names(
		"SBR_READINESS_FIXTURE_OUTCOME_UNSPECIFIED",
		"SBR_READINESS_FIXTURE_OUTCOME_ACCEPTED",
		"SBR_READINESS_FIXTURE_OUTCOME_EXACT_REPLAY",
		"SBR_READINESS_FIXTURE_OUTCOME_NOT_STARTED",
		"SBR_READINESS_FIXTURE_OUTCOME_MAYBE_SENT",
		"SBR_READINESS_FIXTURE_OUTCOME_MALFORMED_RESPONSE",
		"SBR_READINESS_FIXTURE_OUTCOME_HELPER_DEATH",
		"SBR_READINESS_FIXTURE_OUTCOME_TIMEOUT",
		"SBR_READINESS_FIXTURE_OUTCOME_UNKNOWN",
		"SBR_READINESS_FIXTURE_OUTCOME_IDEMPOTENCY_CONFLICT",
	))

	readRequests := map[protoreflect.Name]map[protoreflect.Name]protoreflect.Kind{
		"GetSbrReadinessRequest":            {"authentication": protoreflect.MessageKind},
		"GetMachineCredentialStatusRequest": {"authentication": protoreflect.MessageKind},
	}
	commandRequests := map[protoreflect.Name]map[protoreflect.Name]protoreflect.Kind{
		"ImportMachineCredentialRequest": {
			"command_context": protoreflect.MessageKind, "selected_local_path": protoreflect.StringKind,
			"security_scoped_bookmark": protoreflect.BytesKind, "password": protoreflect.BytesKind,
		},
		"UnlockMachineCredentialRequest": {"command_context": protoreflect.MessageKind, "password": protoreflect.BytesKind},
		"ReplaceMachineCredentialRequest": {
			"command_context": protoreflect.MessageKind, "selected_local_path": protoreflect.StringKind,
			"security_scoped_bookmark": protoreflect.BytesKind, "password": protoreflect.BytesKind,
		},
		"RemoveMachineCredentialRequest": {"command_context": protoreflect.MessageKind},
		"ImportSbrProductIdRequest": {
			"command_context": protoreflect.MessageKind, "product_id_value": protoreflect.StringKind,
			"evte_product_identifier": protoreflect.StringKind, "evte_service_identifier": protoreflect.StringKind,
		},
		"RemoveSbrProductIdRequest": {
			"command_context": protoreflect.MessageKind, "evte_product_identifier": protoreflect.StringKind,
			"evte_service_identifier": protoreflect.StringKind,
		},
		"RunSbrReadinessFixtureRequest": {
			"command_context": protoreflect.MessageKind, "fixture_id": protoreflect.StringKind,
			"failure_case": protoreflect.EnumKind,
		},
	}
	for name, fields := range readRequests {
		message := file.Messages().ByName(name)
		assertExactFields(t, message, fields)
		assertRequiredMessage(t, message.Fields().ByName("authentication"), "tammy.v1.AuthenticationContext")
	}
	for name, fields := range commandRequests {
		message := file.Messages().ByName(name)
		assertExactFields(t, message, fields)
		assertRequiredMessage(t, message.Fields().ByName("command_context"), "tammy.v1.CommandContext")
	}

	for _, requestName := range []protoreflect.Name{"ImportMachineCredentialRequest", "ReplaceMachineCredentialRequest"} {
		request := file.Messages().ByName(requestName)
		pathRules := fieldRules(t, request.Fields().ByName("selected_local_path")).GetString_()
		if pathRules.GetMinBytes() != 1 || pathRules.GetMaxBytes() != 4096 || pathRules.GetPattern() != "^/" {
			t.Errorf("%s.selected_local_path must be one bounded absolute path", requestName)
		}
		bookmarkRules := fieldRules(t, request.Fields().ByName("security_scoped_bookmark")).GetBytes()
		if bookmarkRules.GetMinLen() != 1 || bookmarkRules.GetMaxLen() != 65536 {
			t.Errorf("%s.security_scoped_bookmark bounds = %d..%d, want 1..65536", requestName, bookmarkRules.GetMinLen(), bookmarkRules.GetMaxLen())
		}
		if !request.Fields().ByName("security_scoped_bookmark").HasOptionalKeyword() {
			t.Errorf("%s.security_scoped_bookmark must retain explicit optional presence", requestName)
		}
		if got := fieldRules(t, request.Fields().ByName("password")).GetBytes().GetMaxLen(); got != 1024 {
			t.Errorf("%s.password max bytes = %d, want 1024", requestName, got)
		}
	}
	if got := fieldRules(t, file.Messages().ByName("UnlockMachineCredentialRequest").Fields().ByName("password")).GetBytes().GetMaxLen(); got != 1024 {
		t.Errorf("UnlockMachineCredentialRequest.password max bytes = %d, want 1024", got)
	}
	productImport := file.Messages().ByName("ImportSbrProductIdRequest")
	if got := fieldRules(t, productImport.Fields().ByName("product_id_value")).GetString_().GetMaxBytes(); got != 1024 {
		t.Errorf("product_id_value max bytes = %d, want 1024", got)
	}
	for _, requestName := range []protoreflect.Name{"ImportSbrProductIdRequest", "RemoveSbrProductIdRequest"} {
		request := file.Messages().ByName(requestName)
		for _, fieldName := range []protoreflect.Name{"evte_product_identifier", "evte_service_identifier"} {
			rules := fieldRules(t, request.Fields().ByName(fieldName)).GetString_()
			if rules.GetMinLen() != 1 || rules.GetMaxLen() != 128 {
				t.Errorf("%s.%s bounds = %d..%d, want 1..128", requestName, fieldName, rules.GetMinLen(), rules.GetMaxLen())
			}
		}
	}
	fixtureRequest := file.Messages().ByName("RunSbrReadinessFixtureRequest")
	fixtureFailureRules := fieldRules(t, fixtureRequest.Fields().ByName("failure_case")).GetEnum()
	if !fixtureFailureRules.GetDefinedOnly() || len(fixtureFailureRules.GetNotIn()) != 0 {
		t.Errorf("RunSbrReadinessFixtureRequest.failure_case must accept UNSPECIFIED success and only defined transport failures")
	}

	assertSbrResponseShapes(t, file)
	assertSbrSensitiveFieldMatrix(t, file)
	assertSbrGeneratedParityAndExport(t)
	assertSbrLintExceptionIsNarrow(t)
}

func TestSbrMessageSchemaComparatorRejectsTypeAndPresenceDrift(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/sbr.proto")
	if err != nil {
		t.Fatalf("SBR descriptor missing: %v", err)
	}
	t.Run("wrong referenced type", func(t *testing.T) {
		contracts := expectedSbrMessageSchemas()
		field := contracts["GetSbrReadinessResponse"]["readiness"]
		field.referencedType = "tammy.v1.MachineCredentialStatus"
		contracts["GetSbrReadinessResponse"]["readiness"] = field
		if len(compareSbrMessageSchemas(file, contracts)) == 0 {
			t.Fatal("wrong referenced message type was not detected")
		}
	})
	t.Run("wrong optional presence", func(t *testing.T) {
		contracts := expectedSbrMessageSchemas()
		field := contracts["ImportMachineCredentialRequest"]["security_scoped_bookmark"]
		field.optionalKeyword = false
		field.hasPresence = false
		contracts["ImportMachineCredentialRequest"]["security_scoped_bookmark"] = field
		if len(compareSbrMessageSchemas(file, contracts)) == 0 {
			t.Fatal("wrong optional-presence contract was not detected")
		}
	})
	t.Run("wrong required rule", func(t *testing.T) {
		contracts := expectedSbrMessageSchemas()
		field := contracts["RunSbrReadinessFixtureResponse"]["result"]
		field.validationRequired = false
		contracts["RunSbrReadinessFixtureResponse"]["result"] = field
		if len(compareSbrMessageSchemas(file, contracts)) == 0 {
			t.Fatal("wrong buf.validate required rule was not detected")
		}
	})
	t.Run("wrong enum validation", func(t *testing.T) {
		contracts := expectedSbrMessageSchemas()
		field := contracts["ImportSbrProductIdResponse"]["product_id_state"]
		field.enumNotIn = "[]"
		contracts["ImportSbrProductIdResponse"]["product_id_state"] = field
		if len(compareSbrMessageSchemas(file, contracts)) == 0 {
			t.Fatal("wrong enum not_in rule was not detected")
		}
	})
}

type sbrFieldContract struct {
	kind               protoreflect.Kind
	referencedType     protoreflect.FullName
	cardinality        protoreflect.Cardinality
	list               bool
	mapField           bool
	hasPresence        bool
	optionalKeyword    bool
	validationRequired bool
	enumDefinedOnly    bool
	enumNotIn          string
	stringConst        string
}

func sbrScalar(kind protoreflect.Kind) sbrFieldContract {
	return sbrFieldContract{kind: kind, cardinality: protoreflect.Optional}
}

func sbrConstString(value string) sbrFieldContract {
	return sbrFieldContract{
		kind: protoreflect.StringKind, cardinality: protoreflect.Optional, stringConst: value,
	}
}

func sbrOptionalScalar(kind protoreflect.Kind) sbrFieldContract {
	return sbrFieldContract{
		kind: kind, cardinality: protoreflect.Optional, hasPresence: true, optionalKeyword: true,
	}
}

func sbrRepeated(kind protoreflect.Kind) sbrFieldContract {
	return sbrFieldContract{kind: kind, cardinality: protoreflect.Repeated, list: true}
}

func sbrMessage(name protoreflect.FullName, required bool) sbrFieldContract {
	return sbrFieldContract{
		kind: protoreflect.MessageKind, referencedType: name, cardinality: protoreflect.Optional,
		hasPresence: true, validationRequired: required,
	}
}

func sbrEnum(name protoreflect.FullName) sbrFieldContract {
	return sbrFieldContract{
		kind: protoreflect.EnumKind, referencedType: name, cardinality: protoreflect.Optional,
		enumDefinedOnly: true, enumNotIn: "[]",
	}
}

func sbrEnumRejectZero(name protoreflect.FullName) sbrFieldContract {
	contract := sbrEnum(name)
	contract.enumNotIn = "[0]"
	return contract
}

func expectedSbrMessageSchemas() map[protoreflect.Name]map[protoreflect.Name]sbrFieldContract {
	return map[protoreflect.Name]map[protoreflect.Name]sbrFieldContract{
		"MachineCredentialStatus": {
			"state":             sbrEnum("tammy.v1.MachineCredentialState"),
			"fingerprint":       sbrScalar(protoreflect.StringKind),
			"issuer":            sbrScalar(protoreflect.StringKind),
			"serial":            sbrScalar(protoreflect.StringKind),
			"created_at":        sbrMessage("google.protobuf.Timestamp", false),
			"expires_at":        sbrMessage("google.protobuf.Timestamp", false),
			"component_version": sbrScalar(protoreflect.StringKind),
		},
		"SbrReadiness": {
			"environment":              sbrEnum("tammy.v1.SbrEnvironment"),
			"state":                    sbrEnum("tammy.v1.SbrReadinessState"),
			"machine_credential_state": sbrEnum("tammy.v1.MachineCredentialState"),
			"product_id_state":         sbrEnumRejectZero("tammy.v1.ProductIdState"),
			"readiness_codes":          sbrRepeated(protoreflect.StringKind),
			"credential_fingerprint":   sbrScalar(protoreflect.StringKind),
			"profile_fingerprint":      sbrScalar(protoreflect.StringKind),
			"component_fingerprint":    sbrScalar(protoreflect.StringKind),
			"evte_product_identifier":  sbrScalar(protoreflect.StringKind),
			"evte_service_identifier":  sbrScalar(protoreflect.StringKind),
		},
		"SbrReadinessFixtureResult": {
			"fixture_id":   sbrConstString("SIM-SBR-READINESS-V1"),
			"failure_case": sbrEnum("tammy.v1.SbrReadinessFixtureFailure"),
			"succeeded":    sbrScalar(protoreflect.BoolKind),
			"readiness":    sbrMessage("tammy.v1.SbrReadiness", true),
			"outcome":      sbrEnumRejectZero("tammy.v1.SbrReadinessFixtureOutcome"),
		},
		"GetSbrReadinessRequest": {
			"authentication": sbrMessage("tammy.v1.AuthenticationContext", true),
		},
		"GetSbrReadinessResponse": {
			"readiness": sbrMessage("tammy.v1.SbrReadiness", true),
		},
		"ImportMachineCredentialRequest": {
			"command_context":          sbrMessage("tammy.v1.CommandContext", true),
			"selected_local_path":      sbrScalar(protoreflect.StringKind),
			"security_scoped_bookmark": sbrOptionalScalar(protoreflect.BytesKind),
			"password":                 sbrScalar(protoreflect.BytesKind),
		},
		"ImportMachineCredentialResponse": {
			"credential_status": sbrMessage("tammy.v1.MachineCredentialStatus", true),
		},
		"GetMachineCredentialStatusRequest": {
			"authentication": sbrMessage("tammy.v1.AuthenticationContext", true),
		},
		"GetMachineCredentialStatusResponse": {
			"credential_status": sbrMessage("tammy.v1.MachineCredentialStatus", true),
		},
		"UnlockMachineCredentialRequest": {
			"command_context": sbrMessage("tammy.v1.CommandContext", true),
			"password":        sbrScalar(protoreflect.BytesKind),
		},
		"UnlockMachineCredentialResponse": {
			"credential_status": sbrMessage("tammy.v1.MachineCredentialStatus", true),
		},
		"ReplaceMachineCredentialRequest": {
			"command_context":          sbrMessage("tammy.v1.CommandContext", true),
			"selected_local_path":      sbrScalar(protoreflect.StringKind),
			"security_scoped_bookmark": sbrOptionalScalar(protoreflect.BytesKind),
			"password":                 sbrScalar(protoreflect.BytesKind),
		},
		"ReplaceMachineCredentialResponse": {
			"credential_status": sbrMessage("tammy.v1.MachineCredentialStatus", true),
		},
		"RemoveMachineCredentialRequest": {
			"command_context": sbrMessage("tammy.v1.CommandContext", true),
		},
		"RemoveMachineCredentialResponse": {
			"credential_status": sbrMessage("tammy.v1.MachineCredentialStatus", true),
		},
		"ImportSbrProductIdRequest": {
			"command_context":         sbrMessage("tammy.v1.CommandContext", true),
			"product_id_value":        sbrScalar(protoreflect.StringKind),
			"evte_product_identifier": sbrScalar(protoreflect.StringKind),
			"evte_service_identifier": sbrScalar(protoreflect.StringKind),
		},
		"ImportSbrProductIdResponse": {
			"product_id_state": sbrEnumRejectZero("tammy.v1.ProductIdState"),
		},
		"RemoveSbrProductIdRequest": {
			"command_context":         sbrMessage("tammy.v1.CommandContext", true),
			"evte_product_identifier": sbrScalar(protoreflect.StringKind),
			"evte_service_identifier": sbrScalar(protoreflect.StringKind),
		},
		"RemoveSbrProductIdResponse": {
			"product_id_state": sbrEnumRejectZero("tammy.v1.ProductIdState"),
		},
		"RunSbrReadinessFixtureRequest": {
			"command_context": sbrMessage("tammy.v1.CommandContext", true),
			"fixture_id":      sbrConstString("SIM-SBR-READINESS-V1"),
			"failure_case":    sbrEnum("tammy.v1.SbrReadinessFixtureFailure"),
		},
		"RunSbrReadinessFixtureResponse": {
			"result": sbrMessage("tammy.v1.SbrReadinessFixtureResult", true),
		},
	}
}

func assertExactSbrMessageSchemas(t *testing.T, file protoreflect.FileDescriptor) {
	t.Helper()
	for _, difference := range compareSbrMessageSchemas(file, expectedSbrMessageSchemas()) {
		t.Error(difference)
	}
}

func compareSbrMessageSchemas(file protoreflect.FileDescriptor, expected map[protoreflect.Name]map[protoreflect.Name]sbrFieldContract) []string {
	differences := make([]string, 0)
	if file.Messages().Len() != len(expected) {
		differences = append(differences, fmt.Sprintf("SBR message count = %d, want %d", file.Messages().Len(), len(expected)))
	}
	for messageIndex := 0; messageIndex < file.Messages().Len(); messageIndex++ {
		message := file.Messages().Get(messageIndex)
		if _, ok := expected[message.Name()]; !ok {
			differences = append(differences, fmt.Sprintf("unexpected SBR message %s", message.FullName()))
		}
	}
	for messageName, fields := range expected {
		message := file.Messages().ByName(messageName)
		if message == nil {
			differences = append(differences, fmt.Sprintf("SBR message tammy.v1.%s missing", messageName))
			continue
		}
		if message.Fields().Len() != len(fields) {
			differences = append(differences, fmt.Sprintf("%s field count = %d, want %d", message.FullName(), message.Fields().Len(), len(fields)))
		}
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			field := message.Fields().Get(fieldIndex)
			if _, ok := fields[field.Name()]; !ok {
				differences = append(differences, fmt.Sprintf("unexpected SBR field %s", field.FullName()))
			}
		}
		for fieldName, want := range fields {
			field := message.Fields().ByName(fieldName)
			if field == nil {
				differences = append(differences, fmt.Sprintf("SBR field %s.%s missing", message.FullName(), fieldName))
				continue
			}
			var referencedType protoreflect.FullName
			switch field.Kind() {
			case protoreflect.MessageKind, protoreflect.GroupKind:
				referencedType = field.Message().FullName()
			case protoreflect.EnumKind:
				referencedType = field.Enum().FullName()
			}
			rules := sbrValidationRules(field)
			got := sbrFieldContract{
				kind: field.Kind(), referencedType: referencedType, cardinality: field.Cardinality(),
				list: field.IsList(), mapField: field.IsMap(), hasPresence: field.HasPresence(),
				optionalKeyword: field.HasOptionalKeyword(), validationRequired: rules.GetRequired(),
			}
			if field.Kind() == protoreflect.EnumKind {
				got.enumDefinedOnly = rules.GetEnum().GetDefinedOnly()
				got.enumNotIn = fmt.Sprint(rules.GetEnum().GetNotIn())
			}
			if field.Kind() == protoreflect.StringKind && !field.IsList() {
				got.stringConst = rules.GetString_().GetConst()
			}
			if got != want {
				differences = append(differences, fmt.Sprintf("%s schema = %+v, want %+v", field.FullName(), got, want))
			}
		}
	}
	return differences
}

func sbrValidationRules(field protoreflect.FieldDescriptor) *validate.FieldRules {
	options, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || !proto.HasExtension(options, validate.E_Field) {
		return &validate.FieldRules{}
	}
	rules, ok := proto.GetExtension(options, validate.E_Field).(*validate.FieldRules)
	if !ok {
		return &validate.FieldRules{}
	}
	return rules
}

func assertSbrResponseShapes(t *testing.T, file protoreflect.FileDescriptor) {
	t.Helper()
	responses := map[protoreflect.Name]map[protoreflect.Name]protoreflect.Kind{
		"GetSbrReadinessResponse":            {"readiness": protoreflect.MessageKind},
		"ImportMachineCredentialResponse":    {"credential_status": protoreflect.MessageKind},
		"GetMachineCredentialStatusResponse": {"credential_status": protoreflect.MessageKind},
		"UnlockMachineCredentialResponse":    {"credential_status": protoreflect.MessageKind},
		"ReplaceMachineCredentialResponse":   {"credential_status": protoreflect.MessageKind},
		"RemoveMachineCredentialResponse":    {"credential_status": protoreflect.MessageKind},
		"ImportSbrProductIdResponse":         {"product_id_state": protoreflect.EnumKind},
		"RemoveSbrProductIdResponse":         {"product_id_state": protoreflect.EnumKind},
		"RunSbrReadinessFixtureResponse":     {"result": protoreflect.MessageKind},
	}
	for name, fields := range responses {
		assertExactFields(t, file.Messages().ByName(name), fields)
	}

	readiness := file.Messages().ByName("SbrReadiness")
	assertExactFields(t, readiness, map[protoreflect.Name]protoreflect.Kind{
		"environment": protoreflect.EnumKind, "state": protoreflect.EnumKind,
		"machine_credential_state": protoreflect.EnumKind, "product_id_state": protoreflect.EnumKind,
		"readiness_codes": protoreflect.StringKind, "credential_fingerprint": protoreflect.StringKind,
		"profile_fingerprint": protoreflect.StringKind, "component_fingerprint": protoreflect.StringKind,
		"evte_product_identifier": protoreflect.StringKind, "evte_service_identifier": protoreflect.StringKind,
	})
	if field := readiness.Fields().ByName("readiness_codes"); !field.IsList() || fieldRules(t, field).GetRepeated().GetMaxItems() != 32 {
		t.Error("SbrReadiness.readiness_codes must be bounded to 32")
	}
	assertEnumRejectsUnspecified(t, readiness.Fields().ByName("product_id_state"))
	for _, fieldName := range []protoreflect.Name{"evte_product_identifier", "evte_service_identifier"} {
		rules := fieldRules(t, readiness.Fields().ByName(fieldName)).GetString_()
		if rules.GetMaxLen() != 128 || rules.GetPattern() != "^[A-Za-z0-9._:-]*$" {
			t.Errorf("SbrReadiness.%s must be bounded authenticated scope metadata", fieldName)
		}
	}

	status := file.Messages().ByName("MachineCredentialStatus")
	assertExactFields(t, status, map[protoreflect.Name]protoreflect.Kind{
		"state": protoreflect.EnumKind, "fingerprint": protoreflect.StringKind,
		"issuer": protoreflect.StringKind, "serial": protoreflect.StringKind,
		"created_at": protoreflect.MessageKind, "expires_at": protoreflect.MessageKind,
		"component_version": protoreflect.StringKind,
	})

	fixtureResult := file.Messages().ByName("SbrReadinessFixtureResult")
	assertExactFields(t, fixtureResult, map[protoreflect.Name]protoreflect.Kind{
		"fixture_id": protoreflect.StringKind, "failure_case": protoreflect.EnumKind,
		"succeeded": protoreflect.BoolKind, "readiness": protoreflect.MessageKind,
		"outcome": protoreflect.EnumKind,
	})
	if got := fieldRules(t, fixtureResult.Fields().ByName("fixture_id")).GetString_().GetConst(); got != "SIM-SBR-READINESS-V1" {
		t.Errorf("SbrReadinessFixtureResult.fixture_id const = %q", got)
	}
	assertEnumRejectsUnspecified(t, fixtureResult.Fields().ByName("outcome"))
}

func assertSbrSensitiveFieldMatrix(t *testing.T, file protoreflect.FileDescriptor) {
	t.Helper()
	pathOwners := map[protoreflect.FullName]bool{
		"tammy.v1.ImportMachineCredentialRequest.selected_local_path":  true,
		"tammy.v1.ReplaceMachineCredentialRequest.selected_local_path": true,
	}
	bookmarkOwners := map[protoreflect.FullName]bool{
		"tammy.v1.ImportMachineCredentialRequest.security_scoped_bookmark":  true,
		"tammy.v1.ReplaceMachineCredentialRequest.security_scoped_bookmark": true,
	}
	passwordOwners := map[protoreflect.FullName]bool{
		"tammy.v1.ImportMachineCredentialRequest.password":  true,
		"tammy.v1.UnlockMachineCredentialRequest.password":  true,
		"tammy.v1.ReplaceMachineCredentialRequest.password": true,
	}
	bytesOwners := map[protoreflect.FullName]bool{
		"tammy.v1.ImportMachineCredentialRequest.security_scoped_bookmark":  true,
		"tammy.v1.ImportMachineCredentialRequest.password":                  true,
		"tammy.v1.UnlockMachineCredentialRequest.password":                  true,
		"tammy.v1.ReplaceMachineCredentialRequest.security_scoped_bookmark": true,
		"tammy.v1.ReplaceMachineCredentialRequest.password":                 true,
	}
	productScopeOwners := map[protoreflect.FullName]bool{
		"tammy.v1.ImportSbrProductIdRequest.evte_product_identifier": true,
		"tammy.v1.ImportSbrProductIdRequest.evte_service_identifier": true,
		"tammy.v1.RemoveSbrProductIdRequest.evte_product_identifier": true,
		"tammy.v1.RemoveSbrProductIdRequest.evte_service_identifier": true,
		"tammy.v1.SbrReadiness.evte_product_identifier":              true,
		"tammy.v1.SbrReadiness.evte_service_identifier":              true,
	}
	for index := 0; index < file.Messages().Len(); index++ {
		message := file.Messages().Get(index)
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			field := message.Fields().Get(fieldIndex)
			fullName := field.FullName()
			lower := strings.ToLower(string(field.Name()))
			if strings.Contains(lower, "path") != pathOwners[fullName] {
				t.Errorf("unexpected SBR path field %s", fullName)
			}
			if strings.Contains(lower, "bookmark") != bookmarkOwners[fullName] {
				t.Errorf("unexpected SBR bookmark field %s", fullName)
			}
			if strings.Contains(lower, "password") != passwordOwners[fullName] {
				t.Errorf("unexpected SBR password field %s", fullName)
			}
			if (field.Kind() == protoreflect.BytesKind) != bytesOwners[fullName] {
				t.Errorf("unexpected SBR bytes field %s", fullName)
			}
			if strings.HasPrefix(lower, "evte_") != productScopeOwners[fullName] {
				t.Errorf("unexpected SBR EVTE scope field %s", fullName)
			}
			for _, prohibited := range []string{"workspace_id", "organisation_id", "endpoint", "private_key", "credential_bytes", "manifest", "actor", "roles", "second_factor", "opaque_scope", "secret"} {
				if strings.Contains(lower, prohibited) {
					t.Errorf("SBR field %s exposes prohibited client input", fullName)
				}
			}
			if field.Kind() == protoreflect.StringKind {
				rules := fieldRules(t, field).GetString_()
				if field.IsList() {
					rules = fieldRules(t, field).GetRepeated().GetItems().GetString_()
				}
				if rules.GetMaxLen() == 0 && rules.GetMaxBytes() == 0 && rules.GetConst() == "" {
					t.Errorf("SBR string field %s is unbounded", fullName)
				}
			}
			if field.Kind() == protoreflect.BytesKind && fieldRules(t, field).GetBytes().GetMaxLen() == 0 {
				t.Errorf("SBR bytes field %s is unbounded", fullName)
			}
			if field.IsList() && fieldRules(t, field).GetRepeated().GetMaxItems() == 0 {
				t.Errorf("SBR repeated field %s is unbounded", fullName)
			}
		}
	}

	assertNoProhibitedSbrSymbols(t, file)
}

func assertNoProhibitedSbrSymbols(t *testing.T, file protoreflect.FileDescriptor) {
	t.Helper()
	var symbols []protoreflect.FullName
	for index := 0; index < file.Messages().Len(); index++ {
		message := file.Messages().Get(index)
		symbols = append(symbols, message.FullName())
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			symbols = append(symbols, message.Fields().Get(fieldIndex).FullName())
		}
	}
	for index := 0; index < file.Enums().Len(); index++ {
		enum := file.Enums().Get(index)
		symbols = append(symbols, enum.FullName())
		for valueIndex := 0; valueIndex < enum.Values().Len(); valueIndex++ {
			symbols = append(symbols, enum.Values().Get(valueIndex).FullName())
		}
	}
	for index := 0; index < file.Services().Len(); index++ {
		service := file.Services().Get(index)
		symbols = append(symbols, service.FullName())
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			symbols = append(symbols, service.Methods().Get(methodIndex).FullName())
		}
	}
	for _, symbol := range symbols {
		upper := strings.ToUpper(string(symbol))
		for _, prohibited := range []string{"PRODUCTION", "SUBMIT", "LODGE", "REPORT", "BAS"} {
			if strings.Contains(upper, prohibited) {
				t.Errorf("SBR symbol %s contains prohibited term %s", symbol, prohibited)
			}
		}
	}
}

func TestSbrFixtureIdentifierIsExactAtRuntime(t *testing.T) {
	const fixtureID = "SIM-SBR-READINESS-V1"
	const id = "01890f1e-7c40-7cc0-8ef9-5d7707d34123"
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/sbr.proto")
	if err != nil {
		t.Fatalf("SBR descriptor missing: %v", err)
	}
	for _, messageName := range []protoreflect.Name{"RunSbrReadinessFixtureRequest", "SbrReadinessFixtureResult"} {
		field := file.Messages().ByName(messageName).Fields().ByName("fixture_id")
		if got := fieldRules(t, field).GetString_().GetConst(); got != fixtureID {
			t.Errorf("%s.fixture_id const = %q", messageName, got)
		}
	}
	request := &tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: id,
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: id, SessionId: id},
		},
		FixtureId: fixtureID,
	}
	result := &tammyv1.SbrReadinessFixtureResult{
		FixtureId: fixtureID,
		Succeeded: true,
		Readiness: &tammyv1.SbrReadiness{ProductIdState: tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING},
		Outcome:   tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_ACCEPTED,
	}
	if err := protovalidate.Validate(request); err != nil {
		t.Fatalf("exact successful fixture request rejected: %v", err)
	}
	if err := protovalidate.Validate(result); err != nil {
		t.Fatalf("exact successful fixture result rejected: %v", err)
	}
	request.FixtureId = "another-fixture"
	result.FixtureId = "another-fixture"
	if err := protovalidate.Validate(request); err == nil {
		t.Fatal("arbitrary fixture request ID passed runtime validation")
	}
	if err := protovalidate.Validate(result); err == nil {
		t.Fatal("arbitrary fixture result ID passed runtime validation")
	}
}

func TestSbrEnumRuntimeValidationIsClosedAndZeroSemanticsAreExplicit(t *testing.T) {
	unknown := int32(999)
	unknownCases := []struct {
		name    string
		message proto.Message
	}{
		{"readiness environment", &tammyv1.GetSbrReadinessResponse{Readiness: &tammyv1.SbrReadiness{Environment: tammyv1.SbrEnvironment(unknown), ProductIdState: tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING}}},
		{"readiness state", &tammyv1.GetSbrReadinessResponse{Readiness: &tammyv1.SbrReadiness{State: tammyv1.SbrReadinessState(unknown), ProductIdState: tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING}}},
		{"readiness machine credential state", &tammyv1.GetSbrReadinessResponse{Readiness: &tammyv1.SbrReadiness{MachineCredentialState: tammyv1.MachineCredentialState(unknown), ProductIdState: tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING}}},
		{"nested credential status", &tammyv1.GetMachineCredentialStatusResponse{CredentialStatus: &tammyv1.MachineCredentialStatus{State: tammyv1.MachineCredentialState(unknown)}}},
		{"readiness product ID state", &tammyv1.GetSbrReadinessResponse{Readiness: &tammyv1.SbrReadiness{ProductIdState: tammyv1.ProductIdState(unknown)}}},
		{"import product ID response", &tammyv1.ImportSbrProductIdResponse{ProductIdState: tammyv1.ProductIdState(unknown)}},
		{"remove product ID response", &tammyv1.RemoveSbrProductIdResponse{ProductIdState: tammyv1.ProductIdState(unknown)}},
		{"fixture request failure", func() proto.Message {
			request := validSbrFixtureRequest()
			request.FailureCase = tammyv1.SbrReadinessFixtureFailure(unknown)
			return request
		}()},
		{"nested fixture result failure", &tammyv1.RunSbrReadinessFixtureResponse{Result: &tammyv1.SbrReadinessFixtureResult{
			FixtureId: "SIM-SBR-READINESS-V1", FailureCase: tammyv1.SbrReadinessFixtureFailure(unknown),
			Readiness: validSbrReadiness(), Outcome: tammyv1.SbrReadinessFixtureOutcome_SBR_READINESS_FIXTURE_OUTCOME_ACCEPTED,
		}}},
		{"nested fixture result outcome", &tammyv1.RunSbrReadinessFixtureResponse{Result: &tammyv1.SbrReadinessFixtureResult{
			FixtureId: "SIM-SBR-READINESS-V1", Outcome: tammyv1.SbrReadinessFixtureOutcome(unknown),
			Readiness: validSbrReadiness(),
		}}},
	}
	for _, testCase := range unknownCases {
		t.Run("rejects unknown "+testCase.name, func(t *testing.T) {
			if err := protovalidate.Validate(testCase.message); err == nil {
				t.Fatal("unknown numeric enum value passed runtime validation")
			}
		})
	}

	acceptedZeroCases := []struct {
		name    string
		message proto.Message
	}{
		{"UNSPECIFIED environment with meaningful zero readiness and credential states", &tammyv1.GetSbrReadinessResponse{Readiness: validSbrReadiness()}},
		{"MISSING credential status", &tammyv1.GetMachineCredentialStatusResponse{CredentialStatus: &tammyv1.MachineCredentialStatus{}}},
		{"UNSPECIFIED fixture request failure", validSbrFixtureRequest()},
	}
	for _, testCase := range acceptedZeroCases {
		t.Run("accepts "+testCase.name, func(t *testing.T) {
			if err := protovalidate.Validate(testCase.message); err != nil {
				t.Fatalf("intentional zero enum rejected: %v", err)
			}
		})
	}

	rejectedRequiredZeroCases := []struct {
		name    string
		message proto.Message
	}{
		{"readiness", &tammyv1.GetSbrReadinessResponse{Readiness: &tammyv1.SbrReadiness{}}},
		{"import response", &tammyv1.ImportSbrProductIdResponse{}},
		{"remove response", &tammyv1.RemoveSbrProductIdResponse{}},
		{"fixture result outcome", &tammyv1.RunSbrReadinessFixtureResponse{Result: &tammyv1.SbrReadinessFixtureResult{
			FixtureId: "SIM-SBR-READINESS-V1", Readiness: validSbrReadiness(),
		}}},
	}
	for _, testCase := range rejectedRequiredZeroCases {
		t.Run("rejects forbidden UNSPECIFIED in "+testCase.name, func(t *testing.T) {
			if err := protovalidate.Validate(testCase.message); err == nil {
				t.Fatal("UNSPECIFIED Product ID state passed runtime validation")
			}
		})
	}
}

func TestSbrSecurityScopedBookmarkRequiresNonemptyPresentBytes(t *testing.T) {
	builders := []struct {
		name  string
		build func([]byte) proto.Message
	}{
		{"import", func(bookmark []byte) proto.Message {
			return &tammyv1.ImportMachineCredentialRequest{
				CommandContext: validSbrCommandContext(), SelectedLocalPath: "/tmp/machine-credential.p12",
				SecurityScopedBookmark: bookmark,
			}
		}},
		{"replace", func(bookmark []byte) proto.Message {
			return &tammyv1.ReplaceMachineCredentialRequest{
				CommandContext: validSbrCommandContext(), SelectedLocalPath: "/tmp/machine-credential.p12",
				SecurityScopedBookmark: bookmark,
			}
		}},
	}
	for _, builder := range builders {
		t.Run(builder.name+" accepts absent bookmark", func(t *testing.T) {
			if err := protovalidate.Validate(builder.build(nil)); err != nil {
				t.Fatalf("absent optional bookmark rejected: %v", err)
			}
		})
		t.Run(builder.name+" rejects present empty bookmark", func(t *testing.T) {
			if err := protovalidate.Validate(builder.build(make([]byte, 0))); err == nil {
				t.Fatal("present empty bookmark passed runtime validation")
			}
		})
		t.Run(builder.name+" rejects oversized bookmark", func(t *testing.T) {
			if err := protovalidate.Validate(builder.build(make([]byte, 65537))); err == nil {
				t.Fatal("oversized bookmark passed runtime validation")
			}
		})
	}
}

func validSbrReadiness() *tammyv1.SbrReadiness {
	return &tammyv1.SbrReadiness{ProductIdState: tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING}
}

func validSbrFixtureRequest() *tammyv1.RunSbrReadinessFixtureRequest {
	return &tammyv1.RunSbrReadinessFixtureRequest{
		CommandContext: validSbrCommandContext(), FixtureId: "SIM-SBR-READINESS-V1",
	}
}

func validSbrCommandContext() *tammyv1.CommandContext {
	const id = "01890f1e-7c40-7cc0-8ef9-5d7707d34123"
	return &tammyv1.CommandContext{
		IdempotencyKey: id,
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: id, SessionId: id},
	}
}

func assertSbrGeneratedParityAndExport(t *testing.T) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve SBR contract test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../.."))
	for _, relative := range []string{
		"services/core/internal/gen/tammy/v1/sbr.pb.go",
		"services/core/internal/gen/tammy/v1/tammyv1connect/sbr.connect.go",
		"packages/connect-client/src/gen/tammy/v1/sbr_pb.ts",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("generated SBR output %s missing: %v", relative, err)
		}
	}
	packageBytes, err := os.ReadFile(filepath.Join(root, "packages/connect-client/package.json"))
	if err != nil {
		t.Fatalf("read connect-client package: %v", err)
	}
	var packageJSON struct {
		Exports map[string]string `json:"exports"`
	}
	if err := json.Unmarshal(packageBytes, &packageJSON); err != nil {
		t.Fatalf("decode connect-client package: %v", err)
	}
	if got := packageJSON.Exports["./tammy/v1/sbr_pb.js"]; got != "./src/gen/tammy/v1/sbr_pb.ts" {
		t.Errorf("SBR package export = %q", got)
	}
}

func assertSbrLintExceptionIsNarrow(t *testing.T) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve SBR contract test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../.."))
	source, err := os.ReadFile(filepath.Join(root, "buf.yaml"))
	if err != nil {
		t.Fatalf("read buf lint configuration: %v", err)
	}
	const want = "ignore_only:\n    ENUM_ZERO_VALUE_SUFFIX:\n      - proto/tammy/v1/sbr.proto\n"
	if strings.Count(string(source), "ignore_only:") != 1 || !strings.Contains(string(source), want) {
		t.Fatalf("Buf enum-zero lint exception must contain only the file-scoped SBR rule")
	}
}

func names(values ...string) []protoreflect.Name {
	result := make([]protoreflect.Name, len(values))
	for index, value := range values {
		result[index] = protoreflect.Name(value)
	}
	return result
}
