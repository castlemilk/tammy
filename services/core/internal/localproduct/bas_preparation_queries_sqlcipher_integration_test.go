//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestBASProfileQueriesThroughRealTLSRetainExactHistoryAndRequireAuthentication(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	request := &tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-000000008e01"},
		OrganisationId: server.organisationID, Profile: localBASProfileInput("Original retained answer"),
	}
	created, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(request, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	old := created.Msg.Profile
	request.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008e02"
	request.ExpectedVersion = 1
	request.Profile.Reason = "Corrected answer in the same interval"
	corrected, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(request, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	before := basPreparationCounts(t, server)
	exact := &tammyv1.GetBasObligationsProfileRequest{Authentication: server.authentication,
		AsOfDate:       &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 1},
		OrganisationId: server.organisationID, ProfileId: &old.ProfileId, ProfileHash: bytes.Clone(old.ContentHash)}
	got, err := server.basPreparation.GetBasObligationsProfile(ctx, brandingRequest(exact, server.capability))
	if err != nil || !proto.Equal(got.Msg.Profile, old) {
		t.Fatalf("exact old profile = %v, %v", got, err)
	}
	asOf := &tammyv1.GetBasObligationsProfileRequest{Authentication: server.authentication,
		OrganisationId: server.organisationID, AsOfDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 1}}
	got, err = server.basPreparation.GetBasObligationsProfile(ctx, brandingRequest(asOf, server.capability))
	if err != nil || !proto.Equal(got.Msg.Profile, corrected.Msg.Profile) {
		t.Fatalf("effective corrected profile = %v, %v", got, err)
	}
	listed, err := server.basPreparation.ListBasObligationsProfiles(ctx, brandingRequest(&tammyv1.ListBasObligationsProfilesRequest{
		Authentication: server.authentication, OrganisationId: server.organisationID, Page: &tammyv1.PageRequest{PageSize: 10}}, server.capability))
	if err != nil || len(listed.Msg.Profiles) != 2 {
		t.Fatalf("retained profile history = %v, %v", listed, err)
	}
	wrong := proto.Clone(exact).(*tammyv1.GetBasObligationsProfileRequest)
	wrong.ProfileHash[0] ^= 0x80
	if _, err := server.basPreparation.GetBasObligationsProfile(ctx, brandingRequest(wrong, server.capability)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("wrong exact profile hash = %v", err)
	}
	unknown := proto.Clone(asOf).(*tammyv1.GetBasObligationsProfileRequest)
	unknown.AsOfDate.Year = 2025
	if _, err := server.basPreparation.GetBasObligationsProfile(ctx, brandingRequest(unknown, server.capability)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown historical interval = %v", err)
	}
	unauth := proto.Clone(exact).(*tammyv1.GetBasObligationsProfileRequest)
	unauth.Authentication = proto.Clone(server.authentication).(*tammyv1.AuthenticationContext)
	unauth.Authentication.SessionId = "018f0000-0000-7000-8000-000000008eff"
	if _, err := server.basPreparation.GetBasObligationsProfile(ctx, brandingRequest(unauth, server.capability)); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unknown query session = %v", err)
	}
	assertBASPreparationCounts(t, before, basPreparationCounts(t, server))
}

func localBASSupportedGSTProfileInput() *tammyv1.BasObligationsProfileInput {
	input := localBASProfileInput("Reviewed Simpler GST scope for the actual statement")
	input.Gst = &tammyv1.BasGstProfileInput{
		Applicability:      tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
		RegistrationStatus: tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_REGISTERED,
		ReportingMethod:    tammyv1.BasGstMethod_BAS_GST_METHOD_SIMPLER,
		TurnoverCategory:   tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_SMALL,
		GstBasis:           tammyv1.BasGstBasis_BAS_GST_BASIS_CASH, Cadence: tammyv1.BasCadence_BAS_CADENCE_QUARTERLY,
		RegistrationInterval: &tammyv1.BasEffectiveInterval{StartDate: &tammyv1.CivilDate{Year: 2020, Month: 1, Day: 1}},
		Reason:               "Registered for the supported quarterly Simpler BAS method",
	}
	na := func() *tammyv1.BasObligationAnswer {
		return &tammyv1.BasObligationAnswer{
			Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE, Reason: "Reviewed as not applicable"}
	}
	input.Paygw.Applicability = na()
	input.Paygi.Applicability, input.Paygi.Method = na(), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE
	input.OtherObligations = &tammyv1.BasOtherObligationInventory{
		FringeBenefitsTax: na(), WineEqualisationTax: na(), LuxuryCarTax: na(), FuelTaxCredits: na(),
		DeferredCompanyInstalments: na(), DeferredOrImportGst: na(), SpecialGstSchemesOrAdjustments: na(), OtherActivityStatementObligations: na(),
	}
	return input
}

func TestBASCalendarThroughRealTLSDoesNotInventProfileHistory(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	input := localBASSupportedGSTProfileInput()
	input.EffectiveInterval.EndDate = &tammyv1.CivilDate{Year: 2026, Month: 12, Day: 31}
	set, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(&tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-000000008e10"}, OrganisationId: server.organisationID, Profile: input}, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.basPreparation.GetBasCalendar(ctx, brandingRequest(&tammyv1.GetBasCalendarRequest{
		Authentication: server.authentication, OrganisationId: server.organisationID,
		AsOfDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 1},
		Horizon: &tammyv1.BasCivilDateRange{StartDate: &tammyv1.CivilDate{Year: 2025, Month: 1, Day: 1},
			EndDate: &tammyv1.CivilDate{Year: 2027, Month: 12, Day: 31}}}, server.capability))
	if err != nil || len(response.Msg.Suggestions) != 4 {
		t.Fatalf("known-interval calendar = %v, %v", response, err)
	}
	for _, suggestion := range response.Msg.Suggestions {
		if suggestion.Period.StartDate.Year != 2026 || suggestion.Profile.Id != set.Msg.Profile.ProfileId ||
			suggestion.DueDateRuleVersion != baspreparation.DueDateRuleVersion || !bytes.Equal(suggestion.DueDateRuleHash, baspreparation.DueDateRuleFingerprint()) {
			t.Fatalf("calendar invented profile history or lost pinned date rule: %v", suggestion)
		}
		if suggestion.Window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC &&
			!proto.Equal(suggestion.NominalDueDate, &tammyv1.CivilDate{Year: 2027, Month: 2, Day: 28}) {
			t.Fatalf("December quarter due guidance = %v", suggestion)
		}
	}
}
