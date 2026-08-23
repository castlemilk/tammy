// Package organisations owns the single business profile and independent
// entity-verification lifecycle for one encrypted workspace.
package organisations

import (
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/abn"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const (
	WorkspaceTimezone     = "Australia/Melbourne"
	WorkspaceCurrency     = "AUD"
	UpdateHighRiskPurpose = "update_organisation_high_risk"
)

var (
	ErrInvalidOrganisation = errors.New("organisations: invalid organisation")
	ErrFreshFactorRequired = errors.New("organisations: fresh factor required")
)

// ValidateABN applies the Australian Business Number weighted mod-89 check.
func ValidateABN(value string) error {
	if !abn.Valid(value) {
		return ErrInvalidOrganisation
	}
	return nil
}

// ValidateMoney enforces the product-wide single-currency policy.
func ValidateMoney(value *tammyv1.Money) error {
	if value == nil || value.CurrencyCode != WorkspaceCurrency {
		return ErrInvalidOrganisation
	}
	return nil
}

// CreateProfile validates and constructs the initial unverified projection.
func CreateProfile(request *tammyv1.CreateOrganisationRequest, organisationID, ownerUserID string) (*tammyv1.Organisation, error) {
	if request == nil || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(ownerUserID) ||
		ValidateABN(request.Abn) != nil || !boundedCanonicalText(request.LegalName, 256) ||
		!boundedCanonicalText(request.DisplayName, 256) || !boundedCanonicalText(request.EntityType, 96) ||
		request.FinancialYearEndMonth < 1 || request.FinancialYearEndMonth > 12 ||
		!validGSTConfiguration(request.GstBasis, request.GstReportingFrequency) ||
		!validSourceRef(request.ActiveTaxRuleBundle) {
		return nil, ErrInvalidOrganisation
	}
	return &tammyv1.Organisation{
		Id: organisationID, Version: 1, Abn: request.Abn, LegalName: request.LegalName,
		DisplayName: request.DisplayName, EntityType: request.EntityType, GstBasis: request.GstBasis,
		GstReportingFrequency: request.GstReportingFrequency,
		FinancialYearEndMonth: request.FinancialYearEndMonth,
		VerificationState:     tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		OwnerUserId:           ownerUserID,
		ActiveTaxRuleBundle:   proto.Clone(request.ActiveTaxRuleBundle).(*tammyv1.SourceRef),
	}, nil
}

// ValidateUpdateSecurity classifies profile changes before repository work.
// Factor replay consumption remains transaction-owned by the service port.
func ValidateUpdateSecurity(request *tammyv1.UpdateOrganisationRequest, now time.Time) error {
	if request == nil || request.UpdateMask == nil || request.Patch == nil || len(request.UpdateMask.Paths) == 0 {
		return ErrInvalidOrganisation
	}
	highRisk := false
	requiresEffectiveDate := false
	seen := make(map[string]struct{}, len(request.UpdateMask.Paths))
	for _, path := range request.UpdateMask.Paths {
		if _, duplicate := seen[path]; duplicate {
			return ErrInvalidOrganisation
		}
		seen[path] = struct{}{}
		switch path {
		case "display_name", "financial_year_end_month":
		case "abn", "legal_name", "entity_type":
			highRisk = true
		case "gst_basis", "gst_reporting_frequency", "active_tax_rule_bundle":
			highRisk = true
			requiresEffectiveDate = true
		default:
			return ErrInvalidOrganisation
		}
	}
	if !highRisk {
		return nil
	}
	if request.CommandContext == nil || authorisation.ValidateFreshFactor(
		request.CommandContext.FreshFactor, UpdateHighRiskPurpose, now,
	) != nil {
		return ErrFreshFactorRequired
	}
	if !boundedCanonicalText(request.GetReason(), 512) {
		return ErrInvalidOrganisation
	}
	if requiresEffectiveDate && !futureCivilDate(request.EffectiveDate, now) {
		return ErrInvalidOrganisation
	}
	return nil
}

func validGSTConfiguration(basis tammyv1.GstBasis, frequency tammyv1.GstReportingFrequency) bool {
	basisConfigured := basis == tammyv1.GstBasis_GST_BASIS_CASH || basis == tammyv1.GstBasis_GST_BASIS_NON_CASH
	frequencyConfigured := frequency >= tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_MONTHLY &&
		frequency <= tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_ANNUALLY
	return basisConfigured == frequencyConfigured
}

func validSourceRef(source *tammyv1.SourceRef) bool {
	return source != nil && boundedCanonicalText(source.Type, 64) && ids.IsCanonicalV7(source.Id) &&
		source.Revision > 0 && len(source.ContentHash) == sha256.Size
}

func boundedCanonicalText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value
}

func futureCivilDate(value *tammyv1.CivilDate, now time.Time) bool {
	if value == nil || value.Year < 1 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return false
	}
	location, err := time.LoadLocation(WorkspaceTimezone)
	if err != nil {
		return false
	}
	date := time.Date(int(value.Year), time.Month(value.Month), int(value.Day), 0, 0, 0, 0, location)
	if date.Year() != int(value.Year) || date.Month() != time.Month(value.Month) || date.Day() != int(value.Day) {
		return false
	}
	today := now.In(location)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	return date.After(today)
}
