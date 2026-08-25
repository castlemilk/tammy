package contracts_test

import (
	"testing"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestReportingCapabilityHasExactFourModeContract(t *testing.T) {
	file := tammyv1.File_tammy_v1_reporting_capability_proto
	assertReportingEnum(t, file.Enums().ByName("ReportingCapabilityMode"), []string{
		"REPORTING_CAPABILITY_MODE_UNSPECIFIED",
		"REPORTING_CAPABILITY_MODE_PREPARATION",
		"REPORTING_CAPABILITY_MODE_SIMULATOR",
		"REPORTING_CAPABILITY_MODE_EVTE",
		"REPORTING_CAPABILITY_MODE_PRODUCTION",
	})
	assertReportingEnum(t, file.Enums().ByName("ReportingModeAvailability"), []string{
		"REPORTING_MODE_AVAILABILITY_UNSPECIFIED",
		"REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED",
		"REPORTING_MODE_AVAILABILITY_AVAILABLE",
		"REPORTING_MODE_AVAILABILITY_EXTERNAL_GATED",
	})
	assertReportingEnumContains(t, file.Enums().ByName("ReportingEntityType"), "REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY")
	assertReportingEnumContains(t, file.Enums().ByName("ReportKind"), "REPORT_KIND_COMPANY_TAX_RETURN")

	mode := file.Messages().ByName("ReportingModeCapability")
	if mode == nil {
		t.Fatal("tammy.v1.ReportingModeCapability missing")
	}
	wantFields := []struct {
		name     protoreflect.Name
		kind     protoreflect.Kind
		optional bool
	}{
		{"mode", protoreflect.EnumKind, false},
		{"availability", protoreflect.EnumKind, false},
		{"required_bundle_id", protoreflect.StringKind, true},
		{"activated_bundle_fingerprint", protoreflect.BytesKind, true},
		{"required_service_name", protoreflect.StringKind, true},
		{"summary", protoreflect.StringKind, false},
		{"blockers", protoreflect.StringKind, false},
	}
	if mode.Fields().Len() != len(wantFields) {
		t.Fatalf("ReportingModeCapability field count = %d, want %d", mode.Fields().Len(), len(wantFields))
	}
	for index, want := range wantFields {
		field := mode.Fields().Get(index)
		if field.Name() != want.name || field.Number() != protoreflect.FieldNumber(index+1) || field.Kind() != want.kind || field.HasPresence() != want.optional {
			t.Errorf("ReportingModeCapability field %d = %s #%d %s presence=%v", index, field.Name(), field.Number(), field.Kind(), field.HasPresence())
		}
	}
	modes := file.Messages().ByName("ReportingCapability").Fields().ByName("modes")
	if modes == nil || modes.Number() != 8 || !modes.IsList() || modes.Message().FullName() != "tammy.v1.ReportingModeCapability" {
		t.Fatalf("ReportingCapability.modes = %#v", modes)
	}
}

func TestReportingCapabilityProtovalidateEnforcesFourUniqueModes(t *testing.T) {
	valid := validReportingCapabilityContractFixture()
	if err := protovalidate.Validate(valid); err != nil {
		t.Fatalf("valid capability rejected: %v", err)
	}

	tests := map[string]func(*tammyv1.ReportingCapability){
		"missing mode": func(value *tammyv1.ReportingCapability) {
			value.Modes = value.Modes[:3]
		},
		"duplicate mode": func(value *tammyv1.ReportingCapability) {
			value.Modes[3].Mode = value.Modes[0].Mode
		},
		"zero mode": func(value *tammyv1.ReportingCapability) {
			value.Modes[0].Mode = tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_UNSPECIFIED
		},
		"zero availability": func(value *tammyv1.ReportingCapability) {
			value.Modes[0].Availability = tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_UNSPECIFIED
		},
		"short fingerprint": func(value *tammyv1.ReportingCapability) {
			value.Modes[0].ActivatedBundleFingerprint = []byte{1}
		},
		"missing summary": func(value *tammyv1.ReportingCapability) {
			value.Modes[0].Summary = ""
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := proto.Clone(valid).(*tammyv1.ReportingCapability)
			mutate(fixture)
			if err := protovalidate.Validate(fixture); err == nil {
				t.Fatalf("invalid capability passed validation: %#v", fixture)
			}
		})
	}
}

func validReportingCapabilityContractFixture() *tammyv1.ReportingCapability {
	return &tammyv1.ReportingCapability{
		Report:     tammyv1.ReportKind_REPORT_KIND_COMPANY_TAX_RETURN,
		TaxYear:    2026,
		EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY,
		Status:     tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
		AppVersion: "test",
		Summary:    "Company return contracts do not prepare, validate, simulate, or lodge a return.",
		Modes: []*tammyv1.ReportingModeCapability{
			{Mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION, Availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED, Summary: "Contracts alone do not prepare or validate a return."},
			{Mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR, Availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED, Summary: "Contracts alone do not simulate a return."},
			{Mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE, Availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED, Summary: "Contracts alone do not lodge a return through EVTE."},
			{Mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION, Availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED, Summary: "Contracts alone do not lodge a production return."},
		},
	}
}

func assertReportingEnum(t *testing.T, descriptor protoreflect.EnumDescriptor, want []string) {
	t.Helper()
	if descriptor == nil {
		t.Fatalf("enum missing; want %v", want)
	}
	if descriptor.Values().Len() != len(want) {
		t.Fatalf("%s value count = %d, want %d", descriptor.FullName(), descriptor.Values().Len(), len(want))
	}
	for index, name := range want {
		if got := string(descriptor.Values().Get(index).Name()); got != name {
			t.Errorf("%s value %d = %q, want %q", descriptor.FullName(), index, got, name)
		}
	}
}

func assertReportingEnumContains(t *testing.T, descriptor protoreflect.EnumDescriptor, want string) {
	t.Helper()
	if descriptor == nil || descriptor.Values().ByName(protoreflect.Name(want)) == nil {
		t.Errorf("enum does not contain %s", want)
	}
}
