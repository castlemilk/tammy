package organisations_test

import (
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const domainOrganisationID = "018f0000-0000-7000-8000-000000000020"

func TestOrganisationABNChecksumAndSingleWorkspacePolicy(t *testing.T) {
	for _, valid := range []string{"51824753556", "53004085616", "83914571673"} {
		if err := organisations.ValidateABN(valid); err != nil {
			t.Fatalf("ValidateABN(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "51824753557", "518 247 535 56", "abcdefghijk", "123"} {
		if err := organisations.ValidateABN(invalid); !errors.Is(err, organisations.ErrInvalidOrganisation) {
			t.Fatalf("ValidateABN(%q) = %v, want invalid", invalid, err)
		}
	}
	if organisations.WorkspaceTimezone != "Australia/Melbourne" || organisations.WorkspaceCurrency != "AUD" {
		t.Fatalf("workspace policy = %q/%q", organisations.WorkspaceTimezone, organisations.WorkspaceCurrency)
	}
	if err := organisations.ValidateMoney(&tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 1}); err != nil {
		t.Fatal(err)
	}
	if err := organisations.ValidateMoney(&tammyv1.Money{CurrencyCode: "USD", MinorUnits: 1}); !errors.Is(err, organisations.ErrInvalidOrganisation) {
		t.Fatalf("USD error = %v", err)
	}
}

func TestCreateProfileValidatesIdentityGSTAndRuleProvenance(t *testing.T) {
	request := validCreateOrganisationRequest()
	profile, err := organisations.CreateProfile(request, domainOrganisationID, "018f0000-0000-7000-8000-000000000021")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Version != 1 || profile.VerificationState != tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED ||
		profile.DisplayName != request.DisplayName || profile.ActiveTaxRuleBundle == request.ActiveTaxRuleBundle {
		t.Fatalf("profile = %#v", profile)
	}

	for _, mutate := range []func(*tammyv1.CreateOrganisationRequest){
		func(value *tammyv1.CreateOrganisationRequest) { value.Abn = "51824753557" },
		func(value *tammyv1.CreateOrganisationRequest) { value.LegalName = " Tammy Pty Ltd" },
		func(value *tammyv1.CreateOrganisationRequest) { value.DisplayName = "" },
		func(value *tammyv1.CreateOrganisationRequest) { value.EntityType = "" },
		func(value *tammyv1.CreateOrganisationRequest) {
			value.GstBasis = tammyv1.GstBasis_GST_BASIS_CASH
			value.GstReportingFrequency = 0
		},
		func(value *tammyv1.CreateOrganisationRequest) { value.FinancialYearEndMonth = 13 },
		func(value *tammyv1.CreateOrganisationRequest) { value.ActiveTaxRuleBundle.ContentHash = nil },
	} {
		candidate := validCreateOrganisationRequest()
		mutate(candidate)
		if _, err := organisations.CreateProfile(candidate, domainOrganisationID, "018f0000-0000-7000-8000-000000000021"); !errors.Is(err, organisations.ErrInvalidOrganisation) {
			t.Fatalf("invalid profile error = %v", err)
		}
	}
}

func TestUpdateSecurityRequiresFreshPurposeBoundFactorAndFutureEffectiveDate(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	highRisk := &tammyv1.UpdateOrganisationRequest{
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"gst_basis"}},
		Patch:         &tammyv1.OrganisationPatch{GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH},
		EffectiveDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1},
		Reason:        proto.String("GST attribution change"),
		CommandContext: &tammyv1.CommandContext{FreshFactor: &tammyv1.FreshFactorContext{
			AssertionId: "018f0000-0000-7000-8000-000000000090",
			Purpose:     organisations.UpdateHighRiskPurpose,
			AssertedAt:  timestamppb.New(now.Add(-time.Minute)),
		}},
	}
	if err := organisations.ValidateUpdateSecurity(highRisk, now); err != nil {
		t.Fatal(err)
	}

	for _, mutate := range []func(*tammyv1.UpdateOrganisationRequest){
		func(value *tammyv1.UpdateOrganisationRequest) { value.CommandContext.FreshFactor = nil },
		func(value *tammyv1.UpdateOrganisationRequest) { value.CommandContext.FreshFactor.Purpose = "wrong" },
		func(value *tammyv1.UpdateOrganisationRequest) {
			value.CommandContext.FreshFactor.AssertedAt = timestamppb.New(now.Add(-5 * time.Minute))
		},
		func(value *tammyv1.UpdateOrganisationRequest) { value.EffectiveDate = nil },
		func(value *tammyv1.UpdateOrganisationRequest) {
			value.EffectiveDate = &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 1}
		},
	} {
		candidate := proto.Clone(highRisk).(*tammyv1.UpdateOrganisationRequest)
		mutate(candidate)
		if err := organisations.ValidateUpdateSecurity(candidate, now); !errors.Is(err, organisations.ErrFreshFactorRequired) &&
			!errors.Is(err, organisations.ErrInvalidOrganisation) {
			t.Fatalf("invalid security error = %v", err)
		}
	}

	lowerRisk := &tammyv1.UpdateOrganisationRequest{
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
		Patch:      &tammyv1.OrganisationPatch{DisplayName: "New display"},
	}
	if err := organisations.ValidateUpdateSecurity(lowerRisk, now); err != nil {
		t.Fatalf("lower risk update requires factor: %v", err)
	}
}

func validCreateOrganisationRequest() *tammyv1.CreateOrganisationRequest {
	return &tammyv1.CreateOrganisationRequest{
		Abn: "51824753556", LegalName: "Tammy Pty Ltd", DisplayName: "Tammy", EntityType: "company",
		GstBasis:              tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6,
		ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: "018f0000-0000-7000-8000-000000000022",
			Revision: 1, ContentHash: make([]byte, 32)},
	}
}
