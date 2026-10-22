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

func TestRegenerateBASPreparationThroughRealTLSRetainsExactIncompleteDraftAndReplays(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	profile, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(&tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-000000008d01"}, OrganisationId: server.organisationID,
		Profile: localBASProfileInput("Incomplete profile remains reviewable")}, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	profileRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: profile.Msg.Profile.ProfileId,
		Revision: profile.Msg.Profile.Revision, ContentHash: bytes.Clone(profile.Msg.Profile.ContentHash)}
	period, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(&tammyv1.ConfirmBasPeriodRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-000000008d02"}, OrganisationId: server.organisationID,
		Period: &tammyv1.BasPeriodConfirmationInput{
			Period: &tammyv1.BasCivilDateRange{StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}, EndDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}},
			Window: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, Profile: profileRef,
			NominalDueDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28}, DueDateRuleVersion: baspreparation.DueDateRuleVersion,
			DueDateRuleHash: baspreparation.DueDateRuleFingerprint(), ConfirmedDueDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28},
			DueDateKind:  tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION,
			SupportState: tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED, Reason: "Actual statement window reviewed; remaining evidence is unresolved",
		}}, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	absent := func() *tammyv1.BasExpectedIdentity {
		return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	}
	request := &tammyv1.RegenerateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008d03"},
		OrganisationId: server.organisationID, FamilyExpectation: absent(), PreparationExpectation: absent(),
		Profile: profileRef, Period: &tammyv1.SourceRef{Type: "bas_period", Id: period.Msg.Period.PeriodId,
			Revision: period.Msg.Period.Version, ContentHash: bytes.Clone(period.Msg.Period.ContentHash)},
	}
	first, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(request, server.capability))
	if err != nil || first.Msg.Preparation == nil || first.Msg.Calculation == nil {
		t.Fatalf("initial real regeneration = %v, %v", first, err)
	}
	if first.Msg.Calculation.Result.Outcome == tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE ||
		len(first.Msg.Calculation.Findings) == 0 || len(first.Msg.Calculation.Result.Labels) != 18 {
		t.Fatalf("unknown scope became complete or lost review detail: %v", first.Msg.Calculation)
	}
	for _, label := range first.Msg.Calculation.Result.Labels {
		if label.Label == tammyv1.BasFormLabel_BAS_FORM_LABEL_G1 &&
			(label.State != tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO || label.GetExactCentsNumerator() != "0") {
			t.Fatalf("empty complete source capture lost its exact known-zero G1: %v", label)
		}
	}
	before := basPreparationCounts(t, server)
	replay, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(request, server.capability))
	if err != nil || !proto.Equal(first.Msg, replay.Msg) {
		t.Fatalf("lost-response regeneration replay = %v, %v", replay, err)
	}
	assertBASPreparationCounts(t, before, basPreparationCounts(t, server))
	changed := proto.Clone(request).(*tammyv1.RegenerateBasPreparationRequest)
	changed.Period.ContentHash[0] ^= 0x80
	if _, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("changed regeneration under retained key = %v", err)
	}
	read, err := server.basPreparation.GetBasPreparation(ctx, brandingRequest(&tammyv1.GetBasPreparationRequest{
		Authentication: server.authentication, OrganisationId: server.organisationID,
		PreparationId: first.Msg.Preparation.PreparationId, PreparationHash: bytes.Clone(first.Msg.Preparation.ContentHash)}, server.capability))
	if err != nil || !proto.Equal(read.Msg.Preparation, first.Msg.Preparation) || !proto.Equal(read.Msg.CurrentCalculation, first.Msg.Calculation) {
		t.Fatalf("exact preparation/calculation read = %v, %v", read, err)
	}
	validate := &tammyv1.ValidateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008d04"},
		OrganisationId: server.organisationID, ExpectedVersion: first.Msg.Preparation.Version,
		PreparationId: first.Msg.Preparation.PreparationId, PreparationHash: bytes.Clone(first.Msg.Preparation.ContentHash),
		CalculationId: first.Msg.Calculation.CalculationId, CalculationHash: bytes.Clone(first.Msg.Calculation.ContentHash),
	}
	validated, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(validate, server.capability))
	if err != nil || validated.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED || validated.Msg.Validation == nil ||
		len(validated.Msg.Validation.Findings) == 0 { t.Fatalf("validate unresolved scope = %v, %v", validated, err) }
	beforeValidationReplay := basPreparationCounts(t, server)
	validationReplay, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(validate, server.capability))
	if err != nil || !proto.Equal(validated.Msg, validationReplay.Msg) { t.Fatalf("exact validation replay = %v, %v", validationReplay, err) }
	assertBASPreparationCounts(t, beforeValidationReplay, basPreparationCounts(t, server))
}
