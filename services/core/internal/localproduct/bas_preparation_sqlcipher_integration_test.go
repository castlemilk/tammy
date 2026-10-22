//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func localBASProfileInput(reason string) *tammyv1.BasObligationsProfileInput {
	unknown := func() *tammyv1.BasObligationAnswer {
		return &tammyv1.BasObligationAnswer{Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN}
	}
	return &tammyv1.BasObligationsProfileInput{
		EffectiveInterval: &tammyv1.BasEffectiveInterval{StartDate: &tammyv1.CivilDate{Year: 2026, Month: 1, Day: 1}},
		Gst: &tammyv1.BasGstProfileInput{
			Applicability:      tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN,
			RegistrationStatus: tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_UNKNOWN,
			ReportingMethod:    tammyv1.BasGstMethod_BAS_GST_METHOD_UNKNOWN,
			TurnoverCategory:   tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_UNKNOWN,
			GstBasis:           tammyv1.BasGstBasis_BAS_GST_BASIS_UNKNOWN,
			Cadence:            tammyv1.BasCadence_BAS_CADENCE_UNKNOWN,
		},
		Paygw: &tammyv1.BasPaygwProfileInput{
			Applicability: unknown(), Category: tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_UNKNOWN,
			Cadence:            tammyv1.BasCadence_BAS_CADENCE_UNKNOWN,
			PrefillRequirement: tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNKNOWN,
		},
		Paygi: &tammyv1.BasPaygiProfileInput{
			Applicability: unknown(), Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNKNOWN,
			Cadence: tammyv1.BasCadence_BAS_CADENCE_UNKNOWN,
		},
		OtherObligations: &tammyv1.BasOtherObligationInventory{
			FringeBenefitsTax: unknown(), WineEqualisationTax: unknown(), LuxuryCarTax: unknown(),
			FuelTaxCredits: unknown(), DeferredCompanyInstalments: unknown(), DeferredOrImportGst: unknown(),
			SpecialGstSchemesOrAdjustments: unknown(), OtherActivityStatementObligations: unknown(),
		},
		Reason: reason,
	}
}

func basPreparationCounts(t *testing.T, server documentPageServer) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, table := range []string{"bas_profiles_v1", "bas_profile_versions_v1", "bas_profile_heads_v1", "audit_events_v1", "command_idempotency_v1"} {
		var count int64
		if err := server.module.db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func assertBASPreparationCounts(t *testing.T, want, got map[string]int64) {
	t.Helper()
	for table, value := range want {
		if got[table] != value {
			t.Fatalf("BAS command changed %s: got %d want %d", table, got[table], value)
		}
	}
}

// Fixture integration requirements: documentPageServer has a `basPreparation`
// generated client initialized with the same TLS client/baseURL/capability as
// its other clients; LedgerModule mounts the authenticated BAS route.
func TestSetBASProfileThroughRealTLSCommitsReplaysAndRollsBackAuditFailure(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	request := &tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-000000008f01"},
		OrganisationId: server.organisationID, ExpectedVersion: 0,
		Profile: localBASProfileInput("Complete unknown-answer acceptance profile"),
	}
	first, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(request, server.capability))
	if err != nil || first.Msg.Profile == nil || first.Msg.Profile.Revision != 1 || len(first.Msg.Profile.ContentHash) != 32 {
		t.Fatalf("first real BAS profile set = %v, %v", first, err)
	}
	beforeReplay := basPreparationCounts(t, server)
	replay, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(request, server.capability))
	if err != nil || !proto.Equal(first.Msg, replay.Msg) {
		t.Fatalf("exact profile replay = %v, %v", replay, err)
	}
	assertBASPreparationCounts(t, beforeReplay, basPreparationCounts(t, server))

	changed := proto.Clone(request).(*tammyv1.SetBasObligationsProfileRequest)
	changed.Profile.Reason = "Different body, same key"
	if _, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("changed profile body with same key = %v", err)
	}
	stale := proto.Clone(request).(*tammyv1.SetBasObligationsProfileRequest)
	stale.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008f02"
	if _, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(stale, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("stale first-version expectation = %v", err)
	}
	assertBASPreparationCounts(t, beforeReplay, basPreparationCounts(t, server))

	failed := proto.Clone(request).(*tammyv1.SetBasObligationsProfileRequest)
	failed.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008f03"
	failed.ExpectedVersion = 1
	failed.Profile.Reason = "Second version rolls back when final audit append fails"
	if _, err := server.module.db.ExecContext(ctx, `CREATE TRIGGER fail_bas_preparation_audit BEFORE INSERT ON audit_events_v1 BEGIN SELECT RAISE(ABORT,'injected BAS audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(failed, server.capability)); connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("real BAS audit failure = %v", err)
	}
	if _, err := server.module.db.ExecContext(ctx, `DROP TRIGGER fail_bas_preparation_audit`); err != nil {
		t.Fatal(err)
	}
	assertBASPreparationCounts(t, beforeReplay, basPreparationCounts(t, server))
}

func TestConfirmBASPeriodThroughRealTLSUsesEffectiveProfileForWholeWindow(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()

	setV1 := &tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-000000008f10"},
		OrganisationId: server.organisationID, ExpectedVersion: 0,
		Profile: localBASProfileInput("Initial open-ended profile assertion"),
	}
	setV1.Profile.EffectiveInterval = &tammyv1.BasEffectiveInterval{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 1, Day: 1},
	}
	first, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(setV1, server.capability))
	if err != nil || first.Msg.Profile == nil || first.Msg.Profile.Revision != 1 {
		t.Fatalf("set initial profile over TLS = %v, %v", first, err)
	}
	firstRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: first.Msg.Profile.ProfileId,
		Revision: first.Msg.Profile.Revision, ContentHash: bytes.Clone(first.Msg.Profile.ContentHash)}

	setV2 := proto.Clone(setV1).(*tammyv1.SetBasObligationsProfileRequest)
	setV2.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008f11"
	setV2.ExpectedVersion = 1
	setV2.Profile.EffectiveInterval = &tammyv1.BasEffectiveInterval{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
	}
	setV2.Profile.Reason = "Prospective profile change from 1 July 2026"
	second, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(setV2, server.capability))
	if err != nil || second.Msg.Profile == nil || second.Msg.Profile.Revision != 2 {
		t.Fatalf("set prospective profile over TLS = %v, %v", second, err)
	}
	secondRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: second.Msg.Profile.ProfileId,
		Revision: second.Msg.Profile.Revision, ContentHash: bytes.Clone(second.Msg.Profile.ContentHash)}

	confirmation := func(key string, profile *tammyv1.SourceRef, start, end *tammyv1.CivilDate,
		window tammyv1.BasPeriodWindow, due *tammyv1.CivilDate) *tammyv1.ConfirmBasPeriodRequest {
		return &tammyv1.ConfirmBasPeriodRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: key},
			OrganisationId: server.organisationID, ExpectedVersion: 0,
			Period: &tammyv1.BasPeriodConfirmationInput{
				Period: &tammyv1.BasCivilDateRange{StartDate: start, EndDate: end}, Window: window,
				Profile:        proto.Clone(profile).(*tammyv1.SourceRef),
				NominalDueDate: due, DueDateRuleVersion: baspreparation.DueDateRuleVersion, DueDateRuleHash: baspreparation.DueDateRuleFingerprint(),
				ConfirmedDueDate: proto.Clone(due).(*tammyv1.CivilDate),
				DueDateKind:      tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION,
				SupportState:     tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED,
				Reason:           "User-confirmed actual activity-statement period",
			},
		}
	}

	// The predecessor was open-ended when retained, but its logical range ends
	// the day before the newer span. A known earlier quarter still resolves to
	// the predecessor after the future profile revision has been recorded.
	q2 := confirmation("018f0000-0000-7000-8000-000000008f12", firstRef,
		&tammyv1.CivilDate{Year: 2026, Month: 4, Day: 1}, &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 30},
		tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN,
		&tammyv1.CivilDate{Year: 2026, Month: 7, Day: 28})
	if response, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(q2, server.capability)); err != nil || response.Msg.Period == nil || !proto.Equal(response.Msg.Period.Confirmation.Profile, firstRef) {
		t.Fatalf("known earlier period should retain its effective predecessor: %v, %v", response, err)
	} else if response.Msg.Period.Confirmation.SupportState != tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_OTHER_UNSUPPORTED || len(response.Msg.Period.Confirmation.SupportFindings) == 0 {
		t.Fatalf("unknown obligations trusted the client's supported flag: %v", response.Msg.Period.Confirmation)
	}

	cross := confirmation("018f0000-0000-7000-8000-000000008f13", firstRef,
		&tammyv1.CivilDate{Year: 2026, Month: 6, Day: 30}, &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_CUSTOM,
		&tammyv1.CivilDate{Year: 2026, Month: 8, Day: 28})
	if _, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(cross, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("period crossing a profile effective-date boundary = %v", err)
	}

	// Supplying the retained predecessor for a period after the cutover cannot
	// bypass selection of the new exact profile version.
	q3WithOld := confirmation("018f0000-0000-7000-8000-000000008f14", firstRef,
		&tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}, &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
		tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP,
		&tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28})
	if _, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(q3WithOld, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("later period using a superseded profile reference = %v", err)
	}
	q3 := confirmation("018f0000-0000-7000-8000-000000008f15", secondRef,
		&tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}, &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
		tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP,
		&tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28})
	if response, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(q3, server.capability)); err != nil || response.Msg.Period == nil || !proto.Equal(response.Msg.Period.Confirmation.Profile, secondRef) {
		t.Fatalf("new period should use the exact effective successor: %v, %v", response, err)
	}
	for index, change := range []func(*tammyv1.BasPeriodConfirmationInput){
		func(input *tammyv1.BasPeriodConfirmationInput) { input.DueDateRuleHash[0] ^= 0x80 },
		func(input *tammyv1.BasPeriodConfirmationInput) { input.DueDateRuleVersion = "unreviewed-calendar-rule" },
		func(input *tammyv1.BasPeriodConfirmationInput) {
			input.NominalDueDate.Day = 29
			input.ConfirmedDueDate.Day = 29
		},
		func(input *tammyv1.BasPeriodConfirmationInput) { input.ConfirmedDueDate.Day = 29 },
	} {
		bad := proto.Clone(q3).(*tammyv1.ConfirmBasPeriodRequest)
		bad.ExpectedVersion = 1
		bad.CommandContext.IdempotencyKey = []string{"018f0000-0000-7000-8000-000000008f16", "018f0000-0000-7000-8000-000000008f17", "018f0000-0000-7000-8000-000000008f18", "018f0000-0000-7000-8000-000000008f19"}[index]
		change(bad.Period)
		if _, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(bad, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatalf("untrusted calendar/normal extension case %d = %v", index, err)
		}
	}

	var profileRows, periodRows int64
	if err := server.module.db.QueryRowContext(ctx, `SELECT count(*) FROM bas_profile_versions_v1`).Scan(&profileRows); err != nil || profileRows != 2 {
		t.Fatalf("profile history after exact TLS commands = %d, %v", profileRows, err)
	}
	if err := server.module.db.QueryRowContext(ctx, `SELECT count(*) FROM bas_period_versions_v1`).Scan(&periodRows); err != nil || periodRows != 2 {
		t.Fatalf("period history after rejected boundary/mismatch requests = %d, %v", periodRows, err)
	}
}

func TestConcurrentBASProfileCorrectionsCompareAndSwapLatestAnswer(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	initial := &tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-000000008f11"},
		OrganisationId: server.organisationID,
		Profile:        localBASProfileInput("Known interval before concurrent corrections"),
	}
	created, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(initial, server.capability))
	if err != nil || created.Msg.Profile == nil || created.Msg.Profile.Revision != 1 {
		t.Fatalf("create initial profile = %v, %v", created, err)
	}
	before := basPreparationCounts(t, server)

	makeCorrection := func(key, reason string) *tammyv1.SetBasObligationsProfileRequest {
		return &tammyv1.SetBasObligationsProfileRequest{
			CommandContext:  &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: key},
			OrganisationId:  server.organisationID,
			ExpectedVersion: 1,
			Profile:         localBASProfileInput(reason), // same exact [2026-01-01, ∞) interval
		}
	}
	type result struct {
		request  *tammyv1.SetBasObligationsProfileRequest
		response *connect.Response[tammyv1.SetBasObligationsProfileResponse]
		err      error
	}
	requests := []*tammyv1.SetBasObligationsProfileRequest{
		makeCorrection("018f0000-0000-7000-8000-000000008f12", "Concurrent answer A"),
		makeCorrection("018f0000-0000-7000-8000-000000008f13", "Concurrent answer B"),
	}
	results := make(chan result, len(requests))
	var wait sync.WaitGroup
	for _, request := range requests {
		request := request
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(request, server.capability))
			results <- result{request: request, response: response, err: err}
		}()
	}
	wait.Wait()
	close(results)

	var winner *tammyv1.SetBasObligationsProfileResponse
	var loser *tammyv1.SetBasObligationsProfileRequest
	for value := range results {
		if value.err == nil && value.response != nil && value.response.Msg.Profile != nil && value.response.Msg.Profile.Revision == 2 {
			if winner != nil {
				t.Fatal("both same-version profile corrections committed")
			}
			winner = value.response.Msg
			continue
		}
		if connect.CodeOf(value.err) != connect.CodeFailedPrecondition {
			t.Fatalf("competing correction %s returned response=%v err=%v", value.request.CommandContext.IdempotencyKey, value.response, value.err)
		}
		loser = value.request
	}
	if winner == nil || loser == nil {
		t.Fatalf("expected one v2 correction and one stale-version conflict: winner=%v loser=%v", winner, loser)
	}
	after := basPreparationCounts(t, server)
	if after["bas_profile_versions_v1"] != before["bas_profile_versions_v1"]+1 ||
		after["audit_events_v1"] != before["audit_events_v1"]+1 ||
		after["command_idempotency_v1"] != before["command_idempotency_v1"]+1 {
		t.Fatalf("concurrent corrections retained multiple effects: before=%v after=%v", before, after)
	}
	var head uint64
	if err := server.module.db.QueryRowContext(ctx,
		`SELECT current_version FROM bas_profile_heads_v1 WHERE organisation_id=?`, server.organisationID).Scan(&head); err != nil || head != 2 {
		t.Fatalf("concurrent correction head = %d, %v", head, err)
	}

	// A fresh expected version makes the losing answer a valid later correction;
	// same-interval edits are not globally forbidden.
	loser.ExpectedVersion = 2
	loser.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008f14"
	refreshed, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(loser, server.capability))
	if err != nil || refreshed.Msg.Profile == nil || refreshed.Msg.Profile.Revision != 3 ||
		refreshed.Msg.Profile.PreviousProfile == nil || refreshed.Msg.Profile.PreviousProfile.Revision != 2 ||
		!proto.Equal(refreshed.Msg.Profile.Profile.EffectiveInterval, winner.Profile.Profile.EffectiveInterval) {
		t.Fatalf("refreshed same-interval correction = %v, %v", refreshed, err)
	}
}
