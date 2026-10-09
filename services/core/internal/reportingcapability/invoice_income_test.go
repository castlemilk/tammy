package reportingcapability

import (
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestGSTWorkpaperDescribesReconciledPurchasesAndLegacyReview(t *testing.T) {
	registry, err := NewRegistry("purchase-bas-test")
	if err != nil {
		t.Fatal(err)
	}
	c := registry.Lookup(tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, 2026)
	text := strings.ToLower(c.Summary + " " + strings.Join(c.Limitations, " ") + " " + c.Modes[0].Summary)
	for _, fact := range []string{"purchase", "payment", "reconciled", "legacy"} {
		if !strings.Contains(text, fact) {
			t.Errorf("workpaper capability omits %s: %s", fact, text)
		}
	}
	bas := registry.Lookup(tammyv1.ReportKind_REPORT_KIND_BAS, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, 2024)
	if bas.Status != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED {
		t.Fatal("purchase capture advertised complete BAS preparation")
	}
}

func TestCurrentGSTWorkpaperYearsRemainLocalOnly(t *testing.T) {
	registry, err := NewRegistry("invoice-income-test")
	if err != nil {
		t.Fatal(err)
	}
	for year := int32(2024); year <= 2027; year++ {
		capability := registry.Lookup(tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, year)
		if capability.Status != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE || capability.Modes[0].Availability != tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_AVAILABLE {
			t.Errorf("GST workpaper %d is unavailable: %v", year, capability)
		}
		for _, mode := range capability.Modes[1:] {
			if mode.Availability != tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED {
				t.Errorf("GST workpaper %d offers delivery: %v", year, mode)
			}
		}
		bas := registry.Lookup(tammyv1.ReportKind_REPORT_KIND_BAS, tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, year)
		if bas.Status != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED {
			t.Errorf("complete BAS %d is available", year)
		}
	}
}
