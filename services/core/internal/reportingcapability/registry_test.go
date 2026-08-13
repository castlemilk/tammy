package reportingcapability

import (
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
		})
	}
}

func TestRegistryLookupFailsClosedForUnlistedCombination(t *testing.T) {
	registry, err := NewRegistry("build-42")
	if err != nil {
		t.Fatal(err)
	}

	got := registry.Lookup(
		tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_INDIVIDUAL,
		2025,
	)
	if got == nil || got.GetReport() != tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER ||
		got.GetEntityType() != tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_INDIVIDUAL ||
		got.GetTaxYear() != 2025 ||
		got.GetStatus() != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED ||
		got.GetAppVersion() != "build-42" ||
		got.GetSummary() != "This report is not supported for the selected entity and year." {
		t.Fatalf("Lookup() = %#v", got)
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

	second := registry.Lookup(
		tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
		2024,
	)
	if first == second || second.GetSummary() != "Tammy supports a local reviewed-document GST workpaper only." ||
		len(second.GetLimitations()) != 0 {
		t.Fatalf("second Lookup() reused mutable state: first=%#v second=%#v", first, second)
	}
}
