// Package reportingcapability owns the immutable build-pinned reporting support catalogue.
package reportingcapability

import (
	"errors"
	"strings"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

const maxAppVersionBytes = 128

var ErrInvalidAppVersion = errors.New("reporting capability: invalid app version")

type capabilityKey struct {
	report tammyv1.ReportKind
	entity tammyv1.ReportingEntityType
	year   int32
}

type capabilityEntry struct {
	status      tammyv1.ReportingCapabilityStatus
	summary     string
	limitations []string
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

	return &Registry{
		appVersion: appVersion,
		entries: map[capabilityKey]capabilityEntry{
			{
				report: tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
				entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
				year:   2024,
			}: {
				status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE,
				summary: "Tammy supports a local reviewed-document GST workpaper only.",
			},
			{
				report: tammyv1.ReportKind_REPORT_KIND_BAS,
				entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
				year:   2024,
			}: {
				status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
				summary: "Complete BAS preparation, declaration, and lodgement are unavailable.",
			},
			{
				report: tammyv1.ReportKind_REPORT_KIND_INDIVIDUAL_INCOME_TAX_RETURN,
				entity: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_INDIVIDUAL,
				year:   2024,
			}: {
				status:  tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_UNSUPPORTED,
				summary: "Individual return preparation and myTax handoff are unavailable.",
			},
		},
	}, nil
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
	}
}
