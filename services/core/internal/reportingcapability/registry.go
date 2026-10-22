// Package reportingcapability owns the immutable build-pinned reporting support catalogue.
package reportingcapability

import (
	"errors"
	"strings"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

const maxAppVersionBytes = 128

var ErrInvalidAppVersion = errors.New("reporting capability: invalid app version")

const (
	basPreparationSummary = "Tammy supports local preparation of a limited Australian activity statement for explicitly supported obligations."
	basPreparationScope   = "Supported scope is Simpler GST with the same monthly or quarterly reporting cadence, small or medium PAYGW, and PAYGI using an operative ATO-notified amount or rate without initiating a variation."
	basPreparationLimits  = "Unknown or unsupported obligations block readiness and freezing; values are not inferred. Preparation produces a reviewed manual handoff only. Tammy does not transmit to the ATO or change SBR delivery status."
)

type capabilityKey struct {
	report tammyv1.ReportKind
	entity tammyv1.ReportingEntityType
	year   int32
}

type capabilityEntry struct {
	status      tammyv1.ReportingCapabilityStatus
	summary     string
	limitations []string
	modes       []modeEntry
}

type modeEntry struct {
	mode                tammyv1.ReportingCapabilityMode
	availability        tammyv1.ReportingModeAvailability
	requiredBundleID    string
	requiredServiceName string
	summary             string
	blockers            []string
}

// Registry is one immutable, in-process reporting capability catalogue.
type Registry struct {
	appVersion string
	entries    map[capabilityKey]capabilityEntry
}

// NewRegistry constructs a registry pinned to one exact core build version.
func NewRegistry(appVersion string) (*Registry, error) {
	if appVersion == "" || strings.TrimSpace(appVersion) != appVersion || len(appVersion) > maxAppVersionBytes {
		return nil, ErrInvalidAppVersion
	}

	registry := &Registry{
		appVersion: appVersion,
		entries: map[capabilityKey]capabilityEntry{
			{
				report: tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
				entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
				year:   2024,
			}: {
				status:      tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE,
				summary:     "Tammy supports a local invoice and reconciled purchase GST workpaper.",
				limitations: []string{"Purchase credits use retained accounting and actual payments or reviewed claim periods. Legacy purchase candidates remain provisional outside reconciled 1B until reviewed.", "Evidence suspense and future claim decisions remain visible. This saved report is GST-only; local BAS preparation is available for its supported scope. Unknown or unsupported obligations block readiness. Tammy does not transmit a BAS to the ATO."},
				modes: reportingModes(
					availableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION, "Local invoice and reconciled purchase GST workpaper preparation is available."),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR, "GST workpaper simulation is not implemented.", "GST_WORKPAPER_SIMULATOR_NOT_IMPLEMENTED"),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE, "GST workpaper EVTE delivery is not implemented.", "GST_WORKPAPER_EVTE_NOT_IMPLEMENTED"),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION, "GST workpaper production delivery is not implemented.", "GST_WORKPAPER_PRODUCTION_NOT_IMPLEMENTED"),
				),
			},
			{
				report: tammyv1.ReportKind_REPORT_KIND_BAS,
				entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
				year:   2024,
			}: {
				status:      tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_PREPARATION_ONLY,
				summary:     basPreparationSummary,
				limitations: []string{basPreparationScope, basPreparationLimits},
				modes: reportingModes(
					availableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION, "Local BAS preparation is available for the explicitly supported scope; unknown or unsupported obligations block readiness."),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR, "BAS simulation is not implemented.", "BAS_SIMULATOR_NOT_IMPLEMENTED"),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE, "BAS EVTE delivery is not implemented.", "BAS_EVTE_NOT_IMPLEMENTED"),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION, "BAS production delivery is not implemented.", "BAS_PRODUCTION_NOT_IMPLEMENTED"),
				),
			},
			{
				report: tammyv1.ReportKind_REPORT_KIND_INDIVIDUAL_INCOME_TAX_RETURN,
				entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_INDIVIDUAL,
				year:   2024,
			}: {
				status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
				summary: "Individual return preparation and myTax handoff are unavailable.",
				modes: reportingModes(
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION, "Individual return preparation is not implemented.", "INDIVIDUAL_RETURN_PREPARATION_NOT_IMPLEMENTED"),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR, "Individual return simulation is not implemented.", "INDIVIDUAL_RETURN_SIMULATOR_NOT_IMPLEMENTED"),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE, "Individual return EVTE delivery is not implemented.", "INDIVIDUAL_RETURN_EVTE_NOT_IMPLEMENTED"),
					unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION, "Individual return production delivery is not implemented.", "INDIVIDUAL_RETURN_PRODUCTION_NOT_IMPLEMENTED"),
				),
			},
			{
				report: tammyv1.ReportKind_REPORT_KIND_COMPANY_TAX_RETURN,
				entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY,
				year:   2026,
			}: {
				status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
				summary: "Contracts alone do not prepare, validate, simulate, or lodge a company return.",
				modes: reportingModes(
					modeEntry{
						mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION, availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED,
						requiredBundleID: "au-company-return-2026-preparation-v1", summary: "Contracts alone do not prepare, validate, simulate, or lodge a company return.", blockers: []string{"COMPANY_RETURN_PREPARATION_NOT_IMPLEMENTED"},
					},
					modeEntry{
						mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR, availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED,
						requiredBundleID: "au-company-return-2026-preparation-v1", summary: "Contracts alone do not prepare, validate, simulate, or lodge a company return.", blockers: []string{"COMPANY_RETURN_SIMULATOR_NOT_IMPLEMENTED"},
					},
					modeEntry{
						mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE, availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED,
						requiredServiceName: "Company return 2026", summary: "Contracts alone do not prepare, validate, simulate, or lodge a company return.", blockers: []string{"COMPANY_RETURN_DELIVERY_NOT_IMPLEMENTED", "DSP_REGISTRATION_REQUIRED", "OFFICIAL_SERVICE_ARTEFACTS_REQUIRED", "EVTE_ACCESS_REQUIRED", "CONFORMANCE_REQUIRED"},
					},
					modeEntry{
						mode: tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION, availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED,
						requiredServiceName: "Company return 2026", summary: "Contracts alone do not prepare, validate, simulate, or lodge a company return.", blockers: []string{"COMPANY_RETURN_DELIVERY_NOT_IMPLEMENTED", "DSP_REGISTRATION_REQUIRED", "OFFICIAL_SERVICE_ARTEFACTS_REQUIRED", "EVTE_ACCESS_REQUIRED", "CONFORMANCE_REQUIRED", "PRODUCT_ID_REQUIRED", "PRODUCTION_ACCESS_REQUIRED", "RAM_MACHINE_CREDENTIAL_REQUIRED", "RELEASE_APPROVAL_REQUIRED"},
					},
				),
			},
		},
	}
	for year := int32(2025); year <= 2027; year++ {
		registry.entries[capabilityKey{report: tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER, entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, year: year}] = registry.entries[capabilityKey{report: tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER, entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, year: 2024}]
		registry.entries[capabilityKey{report: tammyv1.ReportKind_REPORT_KIND_BAS, entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, year: year}] = registry.entries[capabilityKey{report: tammyv1.ReportKind_REPORT_KIND_BAS, entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS, year: 2024}]
	}
	return registry, nil
}

// Lookup returns an owned capability for the exact key, failing closed when the key is unlisted.
func (registry *Registry) Lookup(
	report tammyv1.ReportKind,
	entity tammyv1.ReportingEntityType,
	year int32,
) *tammyv1.ReportingCapability {
	entry, ok := registry.entries[capabilityKey{report: report, entity: entity, year: year}]
	if !ok {
		entry = capabilityEntry{
			status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
			summary: "This report is not supported for the selected entity and year.",
			modes: reportingModes(
				unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PREPARATION, "Preparation is not implemented for this report, entity, and year.", "REPORTING_PREPARATION_NOT_IMPLEMENTED"),
				unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_SIMULATOR, "Simulation is not implemented for this report, entity, and year.", "REPORTING_SIMULATOR_NOT_IMPLEMENTED"),
				unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_EVTE, "EVTE delivery is not implemented for this report, entity, and year.", "REPORTING_EVTE_NOT_IMPLEMENTED"),
				unavailableMode(tammyv1.ReportingCapabilityMode_REPORTING_CAPABILITY_MODE_PRODUCTION, "Production delivery is not implemented for this report, entity, and year.", "REPORTING_PRODUCTION_NOT_IMPLEMENTED"),
			),
		}
	}

	return &tammyv1.ReportingCapability{
		Report:      report,
		TaxYear:     year,
		EntityType:  entity,
		Status:      entry.status,
		AppVersion:  registry.appVersion,
		Summary:     entry.summary,
		Limitations: append([]string(nil), entry.limitations...),
		Modes:       cloneModes(entry.modes),
	}
}

func reportingModes(preparation, simulator, evte, production modeEntry) []modeEntry {
	return []modeEntry{preparation, simulator, evte, production}
}

func availableMode(mode tammyv1.ReportingCapabilityMode, summary string) modeEntry {
	return modeEntry{mode: mode, availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_AVAILABLE, summary: summary}
}

func unavailableMode(mode tammyv1.ReportingCapabilityMode, summary, blocker string) modeEntry {
	return modeEntry{mode: mode, availability: tammyv1.ReportingModeAvailability_REPORTING_MODE_AVAILABILITY_NOT_IMPLEMENTED, summary: summary, blockers: []string{blocker}}
}

func cloneModes(entries []modeEntry) []*tammyv1.ReportingModeCapability {
	modes := make([]*tammyv1.ReportingModeCapability, 0, len(entries))
	for _, entry := range entries {
		mode := &tammyv1.ReportingModeCapability{
			Mode: entry.mode, Availability: entry.availability, Summary: entry.summary,
			Blockers: append([]string(nil), entry.blockers...),
		}
		if entry.requiredBundleID != "" {
			value := entry.requiredBundleID
			mode.RequiredBundleId = &value
		}
		if entry.requiredServiceName != "" {
			value := entry.requiredServiceName
			mode.RequiredServiceName = &value
		}
		modes = append(modes, mode)
	}
	return modes
}
