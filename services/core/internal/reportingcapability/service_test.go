package reportingcapability

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

type recordingRegistry struct {
	calls  int
	report tammyv1.ReportKind
	entity tammyv1.ReportingEntityType
	year   int32
	value  *tammyv1.ReportingCapability
}

func (registry *recordingRegistry) Lookup(
	report tammyv1.ReportKind,
	entity tammyv1.ReportingEntityType,
	year int32,
) *tammyv1.ReportingCapability {
	registry.calls++
	registry.report = report
	registry.entity = entity
	registry.year = year
	return registry.value
}

func validCapabilityRequest() *tammyv1.GetReportingCapabilityRequest {
	return &tammyv1.GetReportingCapabilityRequest{
		Report:     tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		TaxYear:    2024,
		EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
	}
}

func validCapability() *tammyv1.ReportingCapability {
	return &tammyv1.ReportingCapability{
		Report:     tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		TaxYear:    2024,
		EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		Status:     tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE,
		AppVersion: "0.1.0-test",
		Summary:    "Tammy supports a local reviewed-document GST workpaper only.",
	}
}

func TestServiceLooksUpCapabilityOnceAndReturnsOwnedResponse(t *testing.T) {
	capability := validCapability()
	registry := &recordingRegistry{value: capability}
	service, err := NewService(registry)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.GetReportingCapability(
		context.Background(),
		connect.NewRequest(validCapabilityRequest()),
	)
	if err != nil {
		t.Fatalf("GetReportingCapability() error = %v", err)
	}
	if registry.calls != 1 || registry.report != capability.Report ||
		registry.entity != capability.EntityType || registry.year != capability.TaxYear {
		t.Fatalf("Lookup() calls = %d, key = %v/%v/%d", registry.calls, registry.report, registry.entity, registry.year)
	}
	if response.Msg.GetCapability() == nil || response.Msg.GetCapability() == capability ||
		response.Msg.GetCapability().GetSummary() != capability.GetSummary() {
		t.Fatalf("GetReportingCapability() = %#v", response.Msg)
	}

	response.Msg.Capability.Summary = "changed"
	if capability.GetSummary() != "Tammy supports a local reviewed-document GST workpaper only." {
		t.Fatal("service response aliases registry-owned data")
	}
}

func TestServiceRejectsInvalidRequestsWithoutLookup(t *testing.T) {
	tests := []struct {
		name    string
		request *connect.Request[tammyv1.GetReportingCapabilityRequest]
	}{
		{name: "nil request"},
		{name: "nil message", request: connect.NewRequest((*tammyv1.GetReportingCapabilityRequest)(nil))},
		{name: "unspecified report", request: connect.NewRequest(&tammyv1.GetReportingCapabilityRequest{
			TaxYear: 2024, EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		})},
		{name: "undefined report", request: connect.NewRequest(&tammyv1.GetReportingCapabilityRequest{
			Report: 99, TaxYear: 2024, EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		})},
		{name: "invalid tax year", request: connect.NewRequest(&tammyv1.GetReportingCapabilityRequest{
			Report: tammyv1.ReportKind_REPORT_KIND_BAS, TaxYear: 1999,
			EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		})},
		{name: "unspecified entity", request: connect.NewRequest(&tammyv1.GetReportingCapabilityRequest{
			Report: tammyv1.ReportKind_REPORT_KIND_BAS, TaxYear: 2024,
		})},
	}
	unknown := validCapabilityRequest()
	unknown.ProtoReflect().SetUnknown([]byte{0x20, 0x01})
	tests = append(tests, struct {
		name    string
		request *connect.Request[tammyv1.GetReportingCapabilityRequest]
	}{name: "unknown field", request: connect.NewRequest(unknown)})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &recordingRegistry{value: validCapability()}
			service, err := NewService(registry)
			if err != nil {
				t.Fatal(err)
			}
			response, callErr := service.GetReportingCapability(context.Background(), tt.request)
			if response != nil || connect.CodeOf(callErr) != connect.CodeInvalidArgument {
				t.Fatalf("GetReportingCapability() = %#v, %v; want InvalidArgument", response, callErr)
			}
			if registry.calls != 0 {
				t.Fatalf("Lookup() calls = %d; want 0", registry.calls)
			}
		})
	}
}

func TestServiceRejectsInvalidRegistryResponse(t *testing.T) {
	registry := &recordingRegistry{value: &tammyv1.ReportingCapability{}}
	service, err := NewService(registry)
	if err != nil {
		t.Fatal(err)
	}

	response, callErr := service.GetReportingCapability(
		context.Background(),
		connect.NewRequest(validCapabilityRequest()),
	)
	if response != nil || connect.CodeOf(callErr) != connect.CodeInternal {
		t.Fatalf("GetReportingCapability() = %#v, %v; want Internal", response, callErr)
	}
	if registry.calls != 1 {
		t.Fatalf("Lookup() calls = %d; want 1", registry.calls)
	}
}

func TestServiceRejectsMismatchedRegistryResponse(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tammyv1.ReportingCapability)
	}{
		{
			name: "report",
			mutate: func(capability *tammyv1.ReportingCapability) {
				capability.Report = tammyv1.ReportKind_REPORT_KIND_BAS
			},
		},
		{
			name: "entity type",
			mutate: func(capability *tammyv1.ReportingCapability) {
				capability.EntityType = tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_INDIVIDUAL
			},
		},
		{
			name: "tax year",
			mutate: func(capability *tammyv1.ReportingCapability) {
				capability.TaxYear = 2025
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capability := validCapability()
			tt.mutate(capability)
			registry := &recordingRegistry{value: capability}
			service, err := NewService(registry)
			if err != nil {
				t.Fatal(err)
			}

			response, callErr := service.GetReportingCapability(
				context.Background(),
				connect.NewRequest(validCapabilityRequest()),
			)
			if response != nil || connect.CodeOf(callErr) != connect.CodeInternal {
				t.Fatalf("GetReportingCapability() = %#v, %v; want Internal", response, callErr)
			}
			if registry.calls != 1 {
				t.Fatalf("Lookup() calls = %d; want 1", registry.calls)
			}
		})
	}
}

func TestNewServiceRejectsNilRegistry(t *testing.T) {
	if service, err := NewService(nil); err == nil || service != nil {
		t.Fatalf("NewService(nil) = %#v, %v; want rejection", service, err)
	}
}

func TestNewServiceRejectsTypedNilRegistry(t *testing.T) {
	var registry *recordingRegistry
	if service, err := NewService(registry); err == nil || service != nil {
		t.Fatalf("NewService(typed nil) = %#v, %v; want rejection", service, err)
	}
}
