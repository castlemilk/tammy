package baspreparation

import (
	"bytes"
	"testing"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestConvertBasCalculationInputsCompleteRateHasAllLabelsAndExactTrace(t *testing.T) {
	profile, period, paygw, paygi, gst := calculationAdapterFixture()
	result, findings := ConvertBasCalculationInputs(profile, period, paygw, paygi, gst[0], gst[1], gst[2])
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
	if result.GetOutcome() != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE {
		t.Fatalf("outcome = %v", result.GetOutcome())
	}
	if result.GetLabel_9Direction() != tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_PAYABLE {
		t.Fatalf("direction = %v", result.GetLabel_9Direction())
	}
	if len(result.GetLabels()) != 18 {
		t.Fatalf("label count = %d, want all 18", len(result.GetLabels()))
	}
	for index, trace := range result.GetLabels() {
		if trace.GetLabel() != orderedBASLabels()[index] {
			t.Fatalf("labels[%d] = %v, want %v", index, trace.GetLabel(), orderedBASLabels()[index])
		}
		if err := protovalidate.Validate(trace); err != nil {
			t.Errorf("label %s fails contract: %v", trace.GetLabel(), err)
		}
	}
	t2 := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_T2)
	if t2.GetExactRateNumerator() != "17" || t2.GetExactRateDenominator() != "10" || t2.ExactCentsNumerator != nil || t2.ReportedWholeDollars != nil {
		t.Fatalf("T2 rate trace = %#v; want exact 17/10 percent and no currency fields", t2)
	}
	t11 := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_T11)
	if t11.GetExactCentsNumerator() != "3417" || t11.GetExactCentsDenominator() != "1" || t11.GetReportedWholeDollars() != 34 {
		t.Fatalf("T11 trace = %#v; want exact 3417 cents and $34 reported", t11)
	}
	if len(t11.GetOperands()) != 2 || t11.GetOperands()[1].GetLabel() != tammyv1.BasFormLabel_BAS_FORM_LABEL_T2 ||
		t11.GetOperands()[1].GetExactRateNumerator() != "17" || t11.GetOperands()[1].GetExactRateDenominator() != "10" {
		t.Fatalf("T11 operands = %#v; want reportable T1 and exact T2 rate", t11.GetOperands())
	}
	w5 := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_W5)
	if w5.GetReportedWholeDollars() != 600 || w5.GetExactCentsNumerator() != "60006" || len(w5.GetOperands()) != 3 {
		t.Fatalf("W5 trace = %#v; want whole-dollar component sum and exact-cent evidence", w5)
	}
	label9 := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_9)
	if label9.GetReportedWholeDollars() != 1834 || label9.GetExactCentsNumerator() != "183472" || label9.GetRoundingDifferenceCentsNumerator() != "72" {
		t.Fatalf("label 9 trace = %#v", label9)
	}
}

func TestConvertBasCalculationInputsUnknownPAYGPreservesGSTAndDoesNotInventDerivedZeros(t *testing.T) {
	profile, period, paygw, paygi, gst := calculationAdapterFixture()
	paygw.W2AmountWithheld = &tammyv1.BasAmountInput{State: tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, Reason: "Not reviewed"}
	result, findings := ConvertBasCalculationInputs(profile, period, paygw, paygi, gst[0], gst[1], gst[2])
	if result.GetOutcome() != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE {
		t.Fatalf("outcome = %v", result.GetOutcome())
	}
	if len(findings) == 0 {
		t.Fatal("expected a blocker finding")
	}
	g1 := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_G1)
	if g1.GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN || g1.GetExactCentsNumerator() != "305001" {
		t.Fatalf("known GST G1 was lost: %#v", g1)
	}
	for _, label := range []tammyv1.BasFormLabel{
		tammyv1.BasFormLabel_BAS_FORM_LABEL_W5,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_4,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_8A,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_9,
	} {
		trace := traceForLabel(t, result, label)
		if trace.GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN || trace.ExactCentsNumerator != nil || trace.ReportedWholeDollars != nil {
			t.Errorf("%s fabricated an amount despite unresolved PAYGW: %#v", label, trace)
		}
	}
	if result.GetLabel_9Direction() != tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_UNSPECIFIED {
		t.Fatalf("incomplete direction = %v", result.GetLabel_9Direction())
	}
}

func TestConvertBasCalculationInputsNotApplicableGSTUsesExplicitZeroForFormSummaries(t *testing.T) {
	profile, period, paygw, paygi, _ := calculationAdapterFixture()
	profile.Profile.Gst.Applicability = tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE
	profile.Profile.Gst.RegistrationStatus = tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_NOT_REGISTERED
	profile.Profile.Gst.RegistrationInterval = nil
	result, findings := ConvertBasCalculationInputs(profile, period, paygw, paygi, nil, nil, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
	if result.GetOutcome() != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE {
		t.Fatalf("outcome = %v", result.GetOutcome())
	}
	if traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_G1).GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
		t.Fatal("not-applicable GST input should remain explicitly not applicable")
	}
	if trace := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_8B); trace.GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO || trace.GetExactCentsNumerator() != "0" {
		t.Fatalf("8B for non-GST profile = %#v, want explicit zero", trace)
	}
	if trace := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_8A); trace.GetReportedWholeDollars() != 634 {
		t.Fatalf("8A for non-GST profile = %#v, want PAYGW+PAYGI reportable total", trace)
	}
}

func TestConvertBasCalculationInputsNotApplicablePAYGWAcceptsEmptyCapturedInputs(t *testing.T) {
	profile, period, _, paygi, gst := calculationAdapterFixture()
	profile.Profile.Paygw.Applicability.Applicability = tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE
	profile.Profile.Paygw.Applicability.Reason = "No withholding obligations"
	profile.Profile.Paygw.Applicability.Evidence = nil
	result, findings := ConvertBasCalculationInputs(profile, period, &tammyv1.BasPaygwInputs{}, paygi, gst[0], gst[1], gst[2])
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
	if result.GetOutcome() != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE {
		t.Fatalf("outcome = %v", result.GetOutcome())
	}
	if trace := traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_W5); trace.GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
		t.Fatalf("W5 for not-applicable PAYGW = %#v, want not applicable", trace)
	}
}

func TestConvertBasCalculationInputsUnsupportedOtherObligationKeepsKnownGSTPartial(t *testing.T) {
	profile, period, paygw, paygi, gst := calculationAdapterFixture()
	profile.Profile.OtherObligations.OtherActivityStatementObligations = &tammyv1.BasObligationAnswer{
		Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
		Evidence:      []*tammyv1.SourceRef{adapterSourceRef(20)}, Reason: "Active obligation needs separate attribution",
	}
	result, findings := ConvertBasCalculationInputs(profile, period, paygw, paygi, gst[0], gst[1], gst[2])
	if result.GetOutcome() != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSUPPORTED {
		t.Fatalf("outcome = %v", result.GetOutcome())
	}
	if !hasFindingCode(findings, "BAS_OTHER_OBLIGATION_UNSUPPORTED") {
		t.Fatalf("findings = %#v", findings)
	}
	if traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_G1).GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN {
		t.Fatal("known GST input should remain visible when another obligation blocks completion")
	}
}

func TestConvertBasCalculationInputsUnknownProfileDoesNotTurnMissingPAYGIntoZero(t *testing.T) {
	_, period, _, _, gst := calculationAdapterFixture()
	result, findings := ConvertBasCalculationInputs(nil, period, nil, nil, gst[0], gst[1], gst[2])
	if result.GetOutcome() != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE {
		t.Fatalf("outcome = %v", result.GetOutcome())
	}
	if !hasFindingCode(findings, "BAS_PROFILE_UNKNOWN") {
		t.Fatalf("findings = %#v", findings)
	}
	if traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_G1).GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN {
		t.Fatal("known GST fact should be retained")
	}
	for _, label := range []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_W5, tammyv1.BasFormLabel_BAS_FORM_LABEL_8A, tammyv1.BasFormLabel_BAS_FORM_LABEL_9} {
		trace := traceForLabel(t, result, label)
		if trace.GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET && trace.GetState() != tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN || trace.ExactCentsNumerator != nil {
			t.Errorf("%s should stay unresolved, got %#v", label, trace)
		}
	}
}

func TestConvertBasCalculationInputsFindingTemplatesAreStable(t *testing.T) {
	profile, period, paygw, paygi, gst := calculationAdapterFixture()
	profile.Profile.OtherObligations.FringeBenefitsTax.Applicability = tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN
	first, firstFindings := ConvertBasCalculationInputs(profile, period, paygw, paygi, gst[0], gst[1], gst[2])
	second, secondFindings := ConvertBasCalculationInputs(profile, period, paygw, paygi, gst[0], gst[1], gst[2])
	if !proto.Equal(first, second) {
		t.Fatal("repeated conversion changed the result")
	}
	if len(firstFindings) != len(secondFindings) {
		t.Fatalf("finding counts differ: %d and %d", len(firstFindings), len(secondFindings))
	}
	for i := range firstFindings {
		if firstFindings[i].StableCode != secondFindings[i].StableCode || firstFindings[i].SafeMessage != secondFindings[i].SafeMessage || firstFindings[i].Severity != secondFindings[i].Severity {
			t.Fatalf("finding template %d changed across identical input", i)
		}
	}
}

func TestT2RateAndRateOperandContractRejectsCurrencyPayload(t *testing.T) {
	profile, period, paygw, paygi, gst := calculationAdapterFixture()
	result, _ := ConvertBasCalculationInputs(profile, period, paygw, paygi, gst[0], gst[1], gst[2])
	t2 := proto.Clone(traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_T2)).(*tammyv1.BasLabelTrace)
	t2.ExactCentsNumerator = proto.String("17")
	t2.ExactCentsDenominator = proto.String("10")
	if protovalidate.Validate(t2) == nil {
		t.Fatal("T2 accepted currency fields alongside its percentage rate")
	}
	op := proto.Clone(traceForLabel(t, result, tammyv1.BasFormLabel_BAS_FORM_LABEL_T11).GetOperands()[1]).(*tammyv1.BasTraceOperand)
	op.ExactCentsNumerator = "17"
	op.ExactCentsDenominator = "10"
	if protovalidate.Validate(op) == nil {
		t.Fatal("T2 operand accepted currency fields alongside its percentage rate")
	}
}

func TestAssessBASProfilePeriodReturnsSharedSupportStateAndFindings(t *testing.T) {
	profile, period, _, _, _ := calculationAdapterFixture()
	assessment := AssessBASProfilePeriod(profile, period.GetConfirmation().GetPeriod(), period.GetConfirmation().GetWindow())
	if assessment.SupportState != tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED || len(assessment.Findings) != 0 {
		t.Fatalf("supported profile/period assessment = %#v", assessment)
	}

	partialRegistration, period, _, _, _ := calculationAdapterFixture()
	partialRegistration.Profile.Gst.RegistrationInterval = &tammyv1.BasEffectiveInterval{
		StartDate: adapterDate(2026, 7, 15), EndDate: adapterDate(2027, 6, 30),
	}
	assessment = AssessBASProfilePeriod(partialRegistration, period.GetConfirmation().GetPeriod(), period.GetConfirmation().GetWindow())
	if assessment.SupportState != tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_PART_PERIOD_REGISTRATION ||
		!hasSupportFindingCode(assessment.Findings, "GST_PART_PERIOD_REGISTRATION") {
		t.Fatalf("partial-registration assessment = %#v", assessment)
	}

	mixedCadence, period, _, _, _ := calculationAdapterFixture()
	mixedCadence.Profile.Gst.Cadence = tammyv1.BasCadence_BAS_CADENCE_MONTHLY
	assessment = AssessBASProfilePeriod(mixedCadence, period.GetConfirmation().GetPeriod(), period.GetConfirmation().GetWindow())
	if assessment.SupportState != tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_MIXED_WINDOW ||
		!hasSupportFindingCode(assessment.Findings, "GST_CADENCE_UNSUPPORTED") {
		t.Fatalf("mixed-cadence assessment = %#v", assessment)
	}
}

func hasSupportFindingCode(findings []*tammyv1.BasPeriodSupportFinding, code string) bool {
	for _, finding := range findings {
		if finding.GetStableCode() == code {
			return true
		}
	}
	return false
}

func traceForLabel(t *testing.T, result *tammyv1.BasCalculationResult, label tammyv1.BasFormLabel) *tammyv1.BasLabelTrace {
	t.Helper()
	for _, trace := range result.GetLabels() {
		if trace.GetLabel() == label {
			return trace
		}
	}
	t.Fatalf("missing label %s", label)
	return nil
}

func hasFindingCode(findings []CalculationFindingTemplate, code string) bool {
	for _, finding := range findings {
		if finding.StableCode == code {
			return true
		}
	}
	return false
}

func calculationAdapterFixture() (*tammyv1.BasObligationsProfile, *tammyv1.BasConfirmedPeriod,
	*tammyv1.BasPaygwInputs, *tammyv1.BasPaygiInputs, [3]*tammyv1.BasAmountInput) {
	incomeYear := &tammyv1.BasIncomeYearIdentity{IncomeYearId: adapterUUID(1),
		Dates:        &tammyv1.BasCivilDateRange{StartDate: adapterDate(2026, 7, 1), EndDate: adapterDate(2027, 6, 30)},
		Confirmation: adapterSourceRef(1)}
	profile := &tammyv1.BasObligationsProfile{
		ProfileId: adapterUUID(2), OrganisationId: adapterUUID(3), Revision: 1, ContentHash: bytes.Repeat([]byte{0x33}, 32),
		Profile: &tammyv1.BasObligationsProfileInput{
			EffectiveInterval: &tammyv1.BasEffectiveInterval{StartDate: adapterDate(2020, 1, 1)},
			Gst: &tammyv1.BasGstProfileInput{Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
				RegistrationStatus: tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_REGISTERED,
				ReportingMethod:    tammyv1.BasGstMethod_BAS_GST_METHOD_SIMPLER,
				TurnoverCategory:   tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_SMALL,
				GstBasis:           tammyv1.BasGstBasis_BAS_GST_BASIS_CASH, Cadence: tammyv1.BasCadence_BAS_CADENCE_QUARTERLY,
				RegistrationInterval: &tammyv1.BasEffectiveInterval{StartDate: adapterDate(2020, 1, 1)}, Evidence: []*tammyv1.SourceRef{adapterSourceRef(2)}},
			Paygw: &tammyv1.BasPaygwProfileInput{Applicability: &tammyv1.BasObligationAnswer{Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
				Evidence: []*tammyv1.SourceRef{adapterSourceRef(3)}, Reason: "Withholding applies"}, Category: tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_SMALL,
				Cadence: tammyv1.BasCadence_BAS_CADENCE_QUARTERLY, PrefillRequirement: tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_NOT_REQUIRED},
			Paygi: &tammyv1.BasPaygiProfileInput{Applicability: &tammyv1.BasObligationAnswer{Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
				Evidence: []*tammyv1.SourceRef{adapterSourceRef(4)}, Reason: "Instalments apply"}, Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE,
				Cadence: tammyv1.BasCadence_BAS_CADENCE_QUARTERLY, ConfirmedIncomeYear: incomeYear},
			OtherObligations: adapterAllOtherObligationsNotApplicable(),
		},
	}
	period := &tammyv1.BasConfirmedPeriod{PeriodId: adapterUUID(4), OrganisationId: adapterUUID(3), Version: 1,
		Confirmation: &tammyv1.BasPeriodConfirmationInput{Period: &tammyv1.BasCivilDateRange{StartDate: adapterDate(2026, 7, 1), EndDate: adapterDate(2026, 9, 30)},
			Window: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, SupportState: tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED}}
	paygw := &tammyv1.BasPaygwInputs{Category: tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_SMALL, PrefillReview: tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_NOT_REQUIRED,
		PayrollStatementEvidence: []*tammyv1.SourceRef{adapterSourceRef(5)},
		W1TotalPayments:          adapterMoneyInput(300000, 6), W2AmountWithheld: adapterMoneyInput(10001, 7),
		W3OtherWithholding: adapterMoneyInput(20002, 8), W4NoAbnWithholding: adapterMoneyInput(30003, 9)}
	paygi := &tammyv1.BasPaygiInputs{Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE, Notice: adapterSourceRef(10),
		NoticeOperativeStatus: tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_OPERATIVE,
		PriorVariation:        tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NONE,
		UnsupportedState:      tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_NONE,
		T1ReviewDecision:      tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_REVIEWED,
		RateT1:                adapterMoneyInput(201099, 11), RateT2: &tammyv1.Decimal{Coefficient: 17, Scale: 1},
		ConfirmedIncomeYear: proto.Clone(incomeYear).(*tammyv1.BasIncomeYearIdentity), Evidence: []*tammyv1.SourceRef{adapterSourceRef(12)}}
	gst := [3]*tammyv1.BasAmountInput{adapterMoneyInput(305001, 13), adapterMoneyInput(220099, 14), adapterMoneyInput(100050, 15)}
	return profile, period, paygw, paygi, gst
}

func adapterAllOtherObligationsNotApplicable() *tammyv1.BasOtherObligationInventory {
	answer := func(n int) *tammyv1.BasObligationAnswer {
		return &tammyv1.BasObligationAnswer{Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE,
			Reason: "Not applicable", Evidence: []*tammyv1.SourceRef{adapterSourceRef(n)}}
	}
	return &tammyv1.BasOtherObligationInventory{FringeBenefitsTax: answer(20), WineEqualisationTax: answer(21), LuxuryCarTax: answer(22), FuelTaxCredits: answer(23),
		DeferredCompanyInstalments: answer(24), DeferredOrImportGst: answer(25), SpecialGstSchemesOrAdjustments: answer(26), OtherActivityStatementObligations: answer(27)}
}

func adapterMoneyInput(cents int64, refID int) *tammyv1.BasAmountInput {
	state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	if cents == 0 {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	}
	return &tammyv1.BasAmountInput{State: state, Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: cents}, Evidence: []*tammyv1.SourceRef{adapterSourceRef(refID)}}
}

func adapterSourceRef(id int) *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: "evidence", Id: adapterUUID(uint64(id)), Revision: 1, ContentHash: bytes.Repeat([]byte{byte(id)}, 32)}
}

func adapterUUID(index uint64) string {
	// Deterministic UUIDv7-shaped synthetic identity for contract fixtures.
	return "00000000-0000-7000-8000-" + fixedHex(index)
}

func fixedHex(index uint64) string {
	const digits = "0123456789abcdef"
	var out [12]byte
	for i := 11; i >= 0; i-- {
		out[i] = digits[index&15]
		index >>= 4
	}
	return string(out[:])
}

func adapterDate(year int32, month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: year, Month: month, Day: day}
}
