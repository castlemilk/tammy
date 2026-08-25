package reportingcapability

import (
	"reflect"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestNewRegistryRejectsInvalidAppVersions(t *testing.T) {
	tests := []struct {
		name       string
		appVersion string
	}{
		{name: "blank", appVersion: ""},
		{name: "whitespace", appVersion: " \t"},
		{name: "leading whitespace", appVersion: " dev"},
		{name: "trailing whitespace", appVersion: "dev "},
		{name: "longer than core version bound", appVersion: strings.Repeat("v", 129)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if registry, err := NewRegistry(tt.appVersion); err == nil || registry != nil {
				t.Fatalf("NewRegistry(%q) = %#v, %v; want rejection", tt.appVersion, registry, err)
			}
		})
	}
}

func TestRegistryLookupReturnsFailClosedCompanyReturn2026Capability(t *testing.T) {
	registry, err := NewRegistry("company-return-test")
	if err != nil {
		t.Fatal(err)
	}

	got := registry.Lookup(
		tammyv1.ReportKind_REPORT_KIND_COMPANY_TAX_RETURN,
		tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY,
		2026,
	)
	if got.GetStatus() != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED || len(got.GetModes()) != 4 {
		t.Fatalf("Lookup() = %#v", got)
	}
	if got.GetSummary() != "Contracts alone do not prepare, validate, simulate, or lodge a company return." {
		t.Fatalf("summary = %q", got.GetSummary())
	}
	wantModes := []tammyv1.ReportingCapabilityMode{
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION,
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR,
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE,
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION,
	}
	wantBlockers := [][]string{
		{"COMPANY_RETURN_PREPARATION_NOT_IMPLEMENTED"},
		{"COMPANY_RETURN_SIMULATOR_NOT_IMPLEMENTED"},
		{"COMPANY_RETURN_DELIVERY_NOT_IMPLEMENTED", "DSP_REGISTRATION_REQUIRED", "OFFICIAL_SERVICE_ARTEFACTS_REQUIRED", "EVTE_ACCESS_REQUIRED", "CONFORMANCE_REQUIRED"},
		{"COMPANY_RETURN_DELIVERY_NOT_IMPLEMENTED", "DSP_REGISTRATION_REQUIRED", "OFFICIAL_SERVICE_ARTEFACTS_REQUIRED", "EVTE_ACCESS_REQUIRED", "CONFORMANCE_REQUIRED", "PRODUCT_ID_REQUIRED", "PRODUCTION_ACCESS_REQUIRED", "RAM_MACHINE_CREDENTIAL_REQUIRED", "RELEASE_APPROVAL_REQUIRED"},
	}
	for index, mode := range got.GetModes() {
		if mode.GetMode() != wantModes[index] || mode.GetAvailability() != tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED ||
			!reflect.DeepEqual(mode.GetBlockers(), wantBlockers[index]) || len(mode.GetActivatedBundleFingerprint()) != 0 {
			t.Errorf("mode %d = %#v", index, mode)
		}
		if mode.GetSummary() != "Contracts alone do not prepare, validate, simulate, or lodge a company return." {
			t.Errorf("mode %d summary = %q", index, mode.GetSummary())
		}
		if mode.GetAvailability() == tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_EXTERNAL_GATED {
			t.Errorf("mode %d is externally gated while internal behavior is not implemented", index)
		}
	}
	if got.GetModes()[0].GetRequiredBundleId() != "au-company-return-2026-preparation-v1" ||
		got.GetModes()[1].GetRequiredBundleId() != "au-company-return-2026-preparation-v1" ||
		got.GetModes()[2].GetRequiredServiceName() != "Company return 2026" ||
		got.GetModes()[3].GetRequiredServiceName() != "Company return 2026" {
		t.Fatalf("unexpected company return requirements: %#v", got.GetModes())
	}
}

func TestRegistryLookupReturnsExactCurrentCapabilities(t *testing.T) {
	const appVersion = "0.1.0-test"
	registry, err := NewRegistry(appVersion)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		report  tammyv1.ReportKind
		entity  tammyv1.ReportingEntityType
		year    int32
		status  tammyv1.ReportingCapabilityStatus
		summary string
	}{
		{
			name:    "local GST workpaper",
			report:  tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
			entity:  tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
			year:    2024,
			status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE,
			summary: "Tammy supports a local reviewed-document GST workpaper only.",
		},
		{
			name:    "complete BAS",
			report:  tammyv1.ReportKind_REPORT_KIND_BAS,
			entity:  tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
			year:    2024,
			status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
			summary: "Complete BAS preparation, declaration, and lodgement are unavailable.",
		},
		{
			name:    "individual income tax return",
			report:  tammyv1.ReportKind_REPORT_KIND_INDIVIDUAL_INCOME_TAX_RETURN,
			entity:  tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_INDIVIDUAL,
			year:    2024,
			status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
			summary: "Individual return preparation and myTax handoff are unavailable.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registry.Lookup(tt.report, tt.entity, tt.year)
			if got == nil || got.GetReport() != tt.report || got.GetEntityType() != tt.entity ||
				got.GetTaxYear() != tt.year || got.GetStatus() != tt.status ||
				got.GetAppVersion() != appVersion || got.GetSummary() != tt.summary {
				t.Fatalf("Lookup() = %#v", got)
			}
			assertFourOrderedModes(t, got)
			for index, mode := range got.GetModes() {
				want := tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED
				if tt.report == tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER && index == 0 {
					want = tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_AVAILABLE
				}
				if mode.GetAvailability() != want {
					t.Errorf("mode %d availability = %s, want %s", index, mode.GetAvailability(), want)
				}
			}
		})
	}
}

func TestRegistryLookupFailsClosedForUnlistedCombination(t *testing.T) {
	registry, err := NewRegistry("build-42")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		report tammyv1.ReportKind
		entity tammyv1.ReportingEntityType
		year   int32
	}{
		{tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_INDIVIDUAL, 2025},
		{tammyv1.ReportKind_REPORT_KIND_COMPANY_TAX_RETURN, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY, 2025},
		{tammyv1.ReportKind_REPORT_KIND_COMPANY_TAX_RETURN, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, 2026},
		{tammyv1.ReportKind_REPORT_KIND_BAS, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY, 2026},
	}
	for _, tt := range tests {
		got := registry.Lookup(tt.report, tt.entity, tt.year)
		if got == nil || got.GetReport() != tt.report || got.GetEntityType() != tt.entity ||
			got.GetTaxYear() != tt.year ||
			got.GetStatus() != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED ||
			got.GetAppVersion() != "build-42" ||
			got.GetSummary() != "This report is not supported for the selected entity and year." {
			t.Errorf("Lookup(%s/%s/%d) = %#v", tt.report, tt.entity, tt.year, got)
			continue
		}
		assertFourOrderedModes(t, got)
		for index, mode := range got.GetModes() {
			if mode.GetAvailability() != tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED {
				t.Errorf("mode %d availability = %s", index, mode.GetAvailability())
			}
		}
	}
}

func TestRegistryLookupReturnsIndependentValues(t *testing.T) {
	registry, err := NewRegistry(strings.Repeat("v", 128))
	if err != nil {
		t.Fatal(err)
	}

	first := registry.Lookup(
		tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		2024,
	)
	first.Summary = "changed"
	first.Limitations = append(first.Limitations, "changed")
	first.Modes[0].Summary = "changed"
	first.Modes[1].Blockers[0] = "CHANGED"

	second := registry.Lookup(
		tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		2024,
	)
	if first == second || second.GetSummary() != "Tammy supports a local reviewed-document GST workpaper only." ||
		len(second.GetLimitations()) != 0 || second.GetModes()[0].GetSummary() != "Local reviewed-document GST workpaper preparation is available." ||
		second.GetModes()[1].GetBlockers()[0] != "GST_WORKPAPER_SIMULATOR_NOT_IMPLEMENTED" {
		t.Fatalf("second Lookup() reused mutable state: first=%#v second=%#v", first, second)
	}
}

func assertFourOrderedModes(t *testing.T, capability *tammyv1.ReportingCapability) {
	t.Helper()
	want := []tammyv1.ReportingCapabilityMode{
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION,
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR,
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE,
		tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION,
	}
	if len(capability.GetModes()) != len(want) {
		t.Fatalf("mode count = %d, want %d", len(capability.GetModes()), len(want))
	}
	for index, mode := range capability.GetModes() {
		if mode.GetMode() != want[index] {
			t.Errorf("mode %d = %s, want %s", index, mode.GetMode(), want[index])
		}
	}
}
