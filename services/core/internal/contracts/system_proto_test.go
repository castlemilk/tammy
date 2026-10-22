package contracts_test

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
)

func TestSystemContract(t *testing.T) {
	if tammyv1connect.SystemServiceName != "tammy.v1.SystemService" {
		t.Fatalf("unexpected service name %q", tammyv1connect.SystemServiceName)
	}
	response := &tammyv1.GetDiagnosticsResponse{
		ApiVersion:      "tammy.v1",
		CoreVersion:     "test",
		RuntimeMode:     tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE,
		NetworkRequired: false,
	}
	if response.GetRuntimeMode() != tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE {
		t.Fatal("offline runtime enum is not usable")
	}
}

func TestReportingCapabilityContract(t *testing.T) {
	if tammyv1connect.ReportingCapabilityServiceName != "tammy.v1.ReportingCapabilityService" {
		t.Fatalf("unexpected service name %q", tammyv1connect.ReportingCapabilityServiceName)
	}
	if tammyv1connect.ReportingCapabilityServiceGetReportingCapabilityProcedure !=
		"/tammy.v1.ReportingCapabilityService/GetReportingCapability" {
		t.Fatalf("unexpected procedure %q", tammyv1connect.ReportingCapabilityServiceGetReportingCapabilityProcedure)
	}

	capability := &tammyv1.ReportingCapability{
		Report:     tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		TaxYear:    2024,
		EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		Status:     tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE,
		AppVersion: "test",
		Summary:    "Local reviewed-document GST workpaper only.",
	}
	if capability.GetStatus() != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE {
		t.Fatal("available reporting capability enum is not usable")
	}
}
