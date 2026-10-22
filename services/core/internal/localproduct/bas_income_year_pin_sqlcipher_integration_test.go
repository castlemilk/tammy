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

func TestPAYGIIncomeYearMethodPinThroughRealTLS(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	const year2026 = "01800000-0000-7000-8000-000000009571"
	const year2027 = "01800000-0000-7000-8000-000000009572"

	set := func(key string, expected uint64, effectiveStart *tammyv1.CivilDate, yearID string,
		yearStart, yearEnd *tammyv1.CivilDate, method tammyv1.BasPaygiMethod) (*tammyv1.SetBasObligationsProfileResponse, error) {
		request := &tammyv1.SetBasObligationsProfileRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: key},
			OrganisationId: server.organisationID, ExpectedVersion: expected,
			Profile: localBASProfileWithYear("Reviewed PAYGI method for a confirmed income year", method, yearID, yearStart, yearEnd),
		}
		request.Profile.EffectiveInterval.StartDate = proto.Clone(effectiveStart).(*tammyv1.CivilDate)
		response, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(request, server.capability))
		if err != nil {
			return nil, err
		}
		return response.Msg, nil
	}
	ref := func(profile *tammyv1.BasObligationsProfile) *tammyv1.SourceRef {
		return &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: profile.ProfileId,
			Revision: profile.Revision, ContentHash: bytes.Clone(profile.ContentHash)}
	}
	confirm := func(key string, profileRef *tammyv1.SourceRef, start, end, due *tammyv1.CivilDate,
		window tammyv1.BasPeriodWindow) *tammyv1.ConfirmBasPeriodRequest {
		return &tammyv1.ConfirmBasPeriodRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: key},
			OrganisationId: server.organisationID,
			Period: &tammyv1.BasPeriodConfirmationInput{
				Period: &tammyv1.BasCivilDateRange{StartDate: start, EndDate: end}, Window: window,
				Profile: proto.Clone(profileRef).(*tammyv1.SourceRef), NominalDueDate: due,
				DueDateRuleVersion: baspreparation.DueDateRuleVersion, DueDateRuleHash: baspreparation.DueDateRuleFingerprint(),
				ConfirmedDueDate: proto.Clone(due).(*tammyv1.CivilDate),
				DueDateKind:      tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION,
				SupportState:     tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED,
				Reason:           "User-confirmed period used to establish the PAYGI income-year method",
			},
		}
	}

	// Before any statement is confirmed, correcting the profile's method is
	// allowed. The second profile revision is therefore the first selected
	// method that can create the durable pin.
	first, err := set("018f0000-0000-7000-8000-000000009573", 0, testLocalDate(2026, 1, 1), year2026,
		testLocalDate(2026, 7, 1), testLocalDate(2027, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT)
	if err != nil || first.Profile == nil || first.Profile.Revision != 1 {
		t.Fatalf("initial PAYGI profile=%v, err=%v", first, err)
	}
	second, err := set("018f0000-0000-7000-8000-000000009574", 1, testLocalDate(2026, 1, 1), year2026,
		testLocalDate(2026, 7, 1), testLocalDate(2027, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE)
	if err != nil || second.Profile == nil || second.Profile.Revision != 2 {
		t.Fatalf("pre-statement method correction=%v, err=%v", second, err)
	}

	q3 := confirm("018f0000-0000-7000-8000-000000009575", ref(second.Profile), testLocalDate(2026, 7, 1), testLocalDate(2026, 9, 30),
		testLocalDate(2026, 10, 28), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	firstConfirmation, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(q3, server.capability))
	if err != nil || firstConfirmation.Msg.Period == nil {
		t.Fatalf("first confirmed statement=%v, err=%v", firstConfirmation, err)
	}

	// Once the first statement pins RATE, changing the same income year to
	// AMOUNT fails at the public profile command before a new version is retained.
	conflict, err := set("018f0000-0000-7000-8000-000000009576", 2, testLocalDate(2026, 1, 1), year2026,
		testLocalDate(2026, 7, 1), testLocalDate(2027, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT)
	if conflict != nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("post-pin method change response=%v err=%v", conflict, err)
	}
	third, err := set("018f0000-0000-7000-8000-000000009577", 2, testLocalDate(2026, 1, 1), year2026,
		testLocalDate(2026, 7, 1), testLocalDate(2027, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE)
	if err != nil || third.Profile == nil || third.Profile.Revision != 3 {
		t.Fatalf("same-method profile correction=%v, err=%v", third, err)
	}

	q4 := confirm("018f0000-0000-7000-8000-000000009578", ref(third.Profile), testLocalDate(2026, 10, 1), testLocalDate(2026, 12, 31),
		testLocalDate(2027, 2, 28), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC)
	if response, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(q4, server.capability)); err != nil || response.Msg.Period == nil {
		t.Fatalf("later same-year/same-method confirmation=%v, err=%v", response, err)
	}

	// A nonoverlapping next confirmed year can choose another method.
	fourth, err := set("018f0000-0000-7000-8000-000000009579", 3, testLocalDate(2027, 7, 1), year2027,
		testLocalDate(2027, 7, 1), testLocalDate(2028, 6, 30), tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT)
	if err != nil || fourth.Profile == nil || fourth.Profile.Revision != 4 {
		t.Fatalf("next-year profile=%v, err=%v", fourth, err)
	}
	q3NextYear := confirm("018f0000-0000-7000-8000-000000009580", ref(fourth.Profile), testLocalDate(2027, 7, 1), testLocalDate(2027, 9, 30),
		testLocalDate(2027, 10, 28), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
	if response, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(q3NextYear, server.capability)); err != nil || response.Msg.Period == nil {
		t.Fatalf("next nonoverlapping year confirmation=%v, err=%v", response, err)
	}

	// Replaying the first public command returns its original result and does
	// not append a duplicate pin.
	replay, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(q3, server.capability))
	if err != nil || !proto.Equal(firstConfirmation.Msg, replay.Msg) {
		t.Fatalf("first statement replay=%v, err=%v", replay, err)
	}
	var pins, periods, profiles int64
	if err := server.module.db.QueryRowContext(ctx, `SELECT count(*) FROM bas_income_year_pins_v1 WHERE organisation_id=?`, server.organisationID).Scan(&pins); err != nil || pins != 2 {
		t.Fatalf("income-year pins=%d err=%v; want exactly one per confirmed income year", pins, err)
	}
	if err := server.module.db.QueryRowContext(ctx, `SELECT count(*) FROM bas_period_versions_v1 WHERE organisation_id=?`, server.organisationID).Scan(&periods); err != nil || periods != 3 {
		t.Fatalf("period history=%d err=%v; rejected method change must not append", periods, err)
	}
	if err := server.module.db.QueryRowContext(ctx, `SELECT count(*) FROM bas_profile_versions_v1 WHERE organisation_id=?`, server.organisationID).Scan(&profiles); err != nil || profiles != 4 {
		t.Fatalf("profile history=%d err=%v; rejected method change must not append", profiles, err)
	}
}

func localBASProfileWithYear(reason string, method tammyv1.BasPaygiMethod, yearID string,
	start, end *tammyv1.CivilDate) *tammyv1.BasObligationsProfileInput {
	profile := localBASProfileInput(reason)
	profile.Paygi.Applicability = &tammyv1.BasObligationAnswer{
		Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
		Reason:        "PAYGI instalments apply", Evidence: []*tammyv1.SourceRef{{Type: "ato_notice", Id: "018f0000-0000-7000-8000-000000009581", Revision: 1, ContentHash: bytes.Repeat([]byte{0x81}, 32)}},
	}
	profile.Paygi.Method = method
	profile.Paygi.Cadence = tammyv1.BasCadence_BAS_CADENCE_QUARTERLY
	profile.Paygi.ConfirmedIncomeYear = &tammyv1.BasIncomeYearIdentity{
		IncomeYearId: yearID, Dates: &tammyv1.BasCivilDateRange{StartDate: start, EndDate: end},
		Confirmation: &tammyv1.SourceRef{Type: "ato_income_year", Id: "018f0000-0000-7000-8000-000000009582", Revision: 1, ContentHash: bytes.Repeat([]byte{0x82}, 32)},
	}
	return profile
}

func testLocalDate(year, month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: year, Month: month, Day: day}
}
