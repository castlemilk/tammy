package baspreparation

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

// CalculationFindingTemplate is a deterministic finding before the command layer
// assigns its persisted UUIDv7 finding_id. It deliberately contains no clock or
// generated identifier, so rerunning the same inputs yields the same template.
type CalculationFindingTemplate struct {
	Severity    tammyv1.BasFindingSeverity
	StableCode  string
	SafeMessage string
	Sources     []*tammyv1.SourceRef
}

type basCalculationAdapter struct {
	findings    []CalculationFindingTemplate
	codes       map[string]struct{}
	blocked     bool
	unsupported bool
}

type calculationTraceValue struct {
	state      tammyv1.BasAmountState
	exactCents *big.Rat
	reported   int64
	rounding   *big.Rat
	exactRate  *big.Rat
	evidence   []*tammyv1.SourceRef
}

// ConvertBasCalculationInputs maps reviewed domain inputs to the persisted
// 18-label trace. The existing integer/rational Calculate function is the sole
// complete-path calculator. The partial path only exposes supplied facts and
// derives a label when every required operand is known.
func ConvertBasCalculationInputs(
	profile *tammyv1.BasObligationsProfile,
	period *tammyv1.BasConfirmedPeriod,
	paygw *tammyv1.BasPaygwInputs,
	paygi *tammyv1.BasPaygiInputs,
	g1 *tammyv1.BasAmountInput,
	oneA *tammyv1.BasAmountInput,
	oneB *tammyv1.BasAmountInput,
) (*tammyv1.BasCalculationResult, []CalculationFindingTemplate) {
	a := &basCalculationAdapter{codes: make(map[string]struct{})}
	profileReady := a.checkProfileAndPeriod(profile, period, paygw, paygi)

	profileGst := profileSectionGst(profile)
	profilePaygw := profileSectionPaygw(profile)
	profilePaygi := profileSectionPaygi(profile)

	values := make(map[tammyv1.BasFormLabel]calculationTraceValue, 18)
	gstProfileEvidence := gstSectionEvidence(profileGst)
	paygwProfileEvidence := paygwSectionEvidence(profilePaygw)
	paygiProfileEvidence := paygiSectionEvidence(profilePaygi)

	if profileGst != nil && profileGst.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		inputs := []struct {
			label tammyv1.BasFormLabel
			code  string
			input *tammyv1.BasAmountInput
		}{
			{tammyv1.BasFormLabel_BAS_FORM_LABEL_G1, "GST_G1_INPUT", g1},
			{tammyv1.BasFormLabel_BAS_FORM_LABEL_1A, "GST_1A_INPUT", oneA},
			{tammyv1.BasFormLabel_BAS_FORM_LABEL_1B, "GST_1B_INPUT", oneB},
		}
		for _, item := range inputs {
			label, input := item.label, item.input
			if input == nil {
				values[label] = notApplicableTraceValue(gstProfileEvidence)
				continue
			}
			value := a.amountValue(item.code, input, gstProfileEvidence)
			if value.state != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
				a.blocker("GST_PROFILE_INPUT_CONFLICT", "GST values conflict with the confirmed not-applicable GST profile.", gstProfileEvidence)
				value = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, value.evidence)
			}
			values[label] = value
		}
	} else {
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_G1] = a.amountValue("GST_G1_INPUT", g1, gstProfileEvidence)
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1A] = a.amountValue("GST_1A_INPUT", oneA, gstProfileEvidence)
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1B] = a.amountValue("GST_1B_INPUT", oneB, gstProfileEvidence)
	}
	if profileGst != nil && profileGst.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		for _, label := range []tammyv1.BasFormLabel{
			tammyv1.BasFormLabel_BAS_FORM_LABEL_G1,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_1A,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_1B,
		} {
			if values[label].state != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
				a.blocker("GST_PROFILE_INPUT_CONFLICT", "GST values conflict with the confirmed not-applicable GST profile.", gstProfileEvidence)
				values[label] = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, values[label].evidence)
			}
		}
	} else if profileGst != nil && profileGst.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
		for _, label := range []tammyv1.BasFormLabel{
			tammyv1.BasFormLabel_BAS_FORM_LABEL_G1,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_1A,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_1B,
		} {
			if values[label].state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
				a.blocker("GST_ACTIVE_LABEL_NOT_APPLICABLE", "An active GST section must use a known amount or reviewed zero for each GST label.", gstProfileEvidence)
			}
		}
	}

	paygwInputValues := map[tammyv1.BasFormLabel]*tammyv1.BasAmountInput{}
	if paygw != nil {
		paygwInputValues = map[tammyv1.BasFormLabel]*tammyv1.BasAmountInput{
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W1: paygw.GetW1TotalPayments(),
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W2: paygw.GetW2AmountWithheld(),
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W3: paygw.GetW3OtherWithholding(),
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W4: paygw.GetW4NoAbnWithholding(),
		}
	} else if profilePaygw != nil && profilePaygw.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
		a.blocker("PAYGW_INPUTS_MISSING", "PAYG withholding inputs are not available for this calculation.", paygwProfileEvidence)
	}
	for _, label := range []tammyv1.BasFormLabel{
		tammyv1.BasFormLabel_BAS_FORM_LABEL_W1,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_W2,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_W3,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_W4,
	} {
		input := paygwInputValues[label]
		if profilePaygw != nil && profilePaygw.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
			if input == nil {
				values[label] = notApplicableTraceValue(paygwProfileEvidence)
				continue
			}
			value := a.amountValue("PAYGW_"+label.String(), input, mergeSourceRefs(paygwProfileEvidence, payrollEvidence(paygw)))
			if value.state != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
				a.blocker("PAYGW_PROFILE_INPUT_CONFLICT", "PAYG withholding values conflict with the confirmed not-applicable PAYG withholding profile.", paygwProfileEvidence)
				value = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, value.evidence)
			}
			values[label] = value
		} else {
			values[label] = a.amountValue("PAYGW_"+label.String(), input, mergeSourceRefs(paygwProfileEvidence, payrollEvidence(paygw)))
		}
	}
	if profilePaygw != nil && profilePaygw.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		for _, label := range []tammyv1.BasFormLabel{
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W1,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W2,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W3,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W4,
		} {
			if values[label].state != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
				a.blocker("PAYGW_PROFILE_INPUT_CONFLICT", "PAYG withholding values conflict with the confirmed not-applicable PAYG withholding profile.", paygwProfileEvidence)
				values[label] = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, values[label].evidence)
			}
		}
	} else if profilePaygw != nil && profilePaygw.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
		for _, label := range []tammyv1.BasFormLabel{
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W1,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W2,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W3,
			tammyv1.BasFormLabel_BAS_FORM_LABEL_W4,
		} {
			if values[label].state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
				a.blocker("PAYGW_ACTIVE_LABEL_NOT_APPLICABLE", "An active PAYG withholding section must use a known amount or reviewed zero for each label.", paygwProfileEvidence)
			}
		}
	}

	paygiMethod := tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNSPECIFIED
	if paygi != nil {
		paygiMethod = paygi.GetMethod()
	}
	var t1, t7 calculationTraceValue
	var t2 calculationTraceValue
	if paygi == nil && profilePaygi != nil && profilePaygi.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		paygiMethod = tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE
		t1 = notApplicableTraceValue(paygiProfileEvidence)
		t2 = notApplicableTraceValue(paygiProfileEvidence)
		t7 = notApplicableTraceValue(paygiProfileEvidence)
	} else if paygi == nil {
		a.blocker("PAYGI_INPUTS_MISSING", "PAYG instalment inputs are not available for this calculation.", paygiProfileEvidence)
		t1 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET, paygiProfileEvidence)
		t7 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET, paygiProfileEvidence)
		t2 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET, paygiProfileEvidence)
	} else {
		switch paygiMethod {
		case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT:
			t1 = notApplicableTraceValue(paygiProfileEvidence)
			t2 = notApplicableTraceValue(paygiProfileEvidence)
			t7 = a.amountValue("PAYGI_T7_INPUT", paygi.GetT7Amount(), mergeSourceRefs(paygiProfileEvidence, paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
			if t7.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE && profilePaygi != nil && profilePaygi.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
				a.blocker("PAYGI_ACTIVE_T7_NOT_APPLICABLE", "An active notified-amount PAYG instalment requires a known T7 amount or reviewed zero.", paygiProfileEvidence)
			}
		case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE:
			t1 = a.amountValue("PAYGI_T1_INPUT", paygi.GetRateT1(), mergeSourceRefs(paygiProfileEvidence, paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
			t2 = a.rateValue("PAYGI_T2_RATE", paygi.GetRateT2(), mergeSourceRefs(paygiProfileEvidence, paygiEvidence(paygi), incomeYearEvidence(paygi)))
			t7 = notApplicableTraceValue(paygiProfileEvidence)
			if (t1.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE || t2.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE) && profilePaygi != nil && profilePaygi.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
				a.blocker("PAYGI_ACTIVE_RATE_LABEL_NOT_APPLICABLE", "An active notified-rate PAYG instalment requires reviewed T1 and a notified T2 rate.", paygiProfileEvidence)
			}
			if paygi.GetT1ReviewDecision() != tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_REVIEWED {
				a.blocker("PAYGI_T1_NOT_REVIEWED", "PAYG instalment T1 must be explicitly reviewed before it can be used.", mergeSourceRefs(paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
			}
		case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE:
			t1 = notApplicableTraceValue(paygiProfileEvidence)
			t2 = notApplicableTraceValue(paygiProfileEvidence)
			t7 = notApplicableTraceValue(paygiProfileEvidence)
		case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNKNOWN,
			tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNSPECIFIED:
			a.blocker("PAYGI_METHOD_UNKNOWN", "The PAYG instalment method has not been confirmed.", mergeSourceRefs(paygiProfileEvidence, paygiEvidence(paygi)))
			t1 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiProfileEvidence)
			t2 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiProfileEvidence)
			t7 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiProfileEvidence)
		default:
			a.unsupportedFinding("PAYGI_METHOD_UNSUPPORTED", "The selected PAYG instalment method is not supported for calculation.", mergeSourceRefs(paygiProfileEvidence, paygiEvidence(paygi)))
			t1 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiProfileEvidence)
			t2 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiProfileEvidence)
			t7 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiProfileEvidence)
		}
		if paygi.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_UNSPECIFIED {
			a.blocker("PAYGI_UNSUPPORTED_STATE_UNKNOWN", "The PAYG instalment unsupported-case review has not been completed.", paygiEvidence(paygi))
		}
		if (paygiMethod == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT && (paygi.GetRateT1() != nil || paygi.GetRateT2() != nil)) ||
			(paygiMethod == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE && paygi.GetT7Amount() != nil) ||
			(paygiMethod == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE && (paygi.GetT7Amount() != nil || paygi.GetRateT1() != nil || paygi.GetRateT2() != nil)) {
			a.blocker("PAYGI_METHOD_PAYLOAD_CONFLICT", "PAYG instalment amount and rate inputs do not match the selected method.", paygiEvidence(paygi))
		}
		if paygi.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_VARIATION_FIELDS_REQUIRED ||
			paygi.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_CLAIM_5B ||
			paygi.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_OTHER ||
			paygi.GetProposedVariationAmount() != nil || paygi.GetProposedVariationRate() != nil || paygi.GetClaim_5B() != nil {
			a.unsupportedFinding("PAYGI_VARIATION_OR_CREDIT_UNSUPPORTED", "The PAYG instalment variation or 5B credit requires unsupported handling.", mergeSourceRefs(paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
		}
		if paygi.GetNoticeOperativeStatus() != tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_OPERATIVE &&
			paygiMethod != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE {
			if paygi.GetNoticeOperativeStatus() == tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_INOPERATIVE {
				a.blocker("PAYGI_NOTICE_INOPERATIVE", "The retained PAYG instalment notice is not confirmed operative for this period.", mergeSourceRefs(paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
			} else {
				a.blocker("PAYGI_NOTICE_STATUS_UNKNOWN", "The operative status of the PAYG instalment notice is not confirmed.", mergeSourceRefs(paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
			}
		}
		if paygi.GetPriorVariation() == tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_UNCERTAIN ||
			paygi.GetPriorVariation() == tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NEW {
			a.unsupportedFinding("PAYGI_PRIOR_VARIATION_UNSUPPORTED", "An uncertain or newly varied PAYG instalment cannot be calculated by the supported path.", mergeSourceRefs(paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
		} else if paygi.GetPriorVariation() != tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NONE &&
			paygi.GetPriorVariation() != tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_REFLECTED_IN_CURRENT_NOTICE &&
			paygiMethod != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE {
			a.blocker("PAYGI_PRIOR_VARIATION_UNKNOWN", "The prior PAYG instalment variation status is not confirmed.", mergeSourceRefs(paygiEvidence(paygi), adapterSourceRefs(paygi.GetNotice())))
		}
	}
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_T1] = t1
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_T2] = t2
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_T7] = t7

	var fiveB calculationTraceValue
	if paygi != nil && paygi.GetClaim_5B() != nil {
		fiveB = a.amountValue("PAYGI_5B_UNSUPPORTED_INPUT", paygi.GetClaim_5B(), paygiEvidence(paygi))
	} else if paygi != nil && paygi.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_CLAIM_5B {
		fiveB = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiEvidence(paygi))
	} else {
		fiveB = notApplicableTraceValue(paygiProfileEvidence)
	}
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_5B] = fiveB

	t11 := deriveT11(t1, t2)
	if paygiMethod == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE || paygiMethod == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT {
		t11 = notApplicableTraceValue(paygiProfileEvidence)
	}
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_T11] = t11

	fiveA := calculationTraceValue{}
	switch paygiMethod {
	case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT:
		fiveA = copyTraceValue(t7)
		if paygi == nil || !paygiNoticeCanBeUsed(paygi) {
			fiveA = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, mergeSourceRefs(t7.evidence, paygiProfileEvidence))
		}
	case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE:
		fiveA = copyTraceValue(t11)
		if paygi == nil || !paygiNoticeCanBeUsed(paygi) || paygi.GetT1ReviewDecision() != tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_REVIEWED {
			fiveA = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, mergeSourceRefs(t11.evidence, paygiProfileEvidence))
		}
	case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE:
		fiveA = notApplicableTraceValue(paygiProfileEvidence)
	default:
		fiveA = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, paygiProfileEvidence)
	}
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_5A] = fiveA

	mixedPaygw := hasMixedApplicability(values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W1], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W2], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W3], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W4])
	if mixedPaygw {
		a.blocker("PAYGW_LABEL_APPLICABILITY_MIXED", "PAYG withholding labels mix applicable and not-applicable states.", paygwProfileEvidence)
	}
	mixedGST := hasMixedApplicability(values[tammyv1.BasFormLabel_BAS_FORM_LABEL_G1], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1A], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1B])
	if mixedGST {
		a.blocker("GST_LABEL_APPLICABILITY_MIXED", "GST labels mix applicable and not-applicable states.", gstProfileEvidence)
	}
	w5 := deriveSectionSum(
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W2],
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W3],
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W4],
	)
	if mixedPaygw {
		w5 = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, mergeTraceEvidence(values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W2], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W3], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W4]))
	}
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_W5] = w5
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_4] = copyTraceValue(w5)
	eightA := deriveSectionSum(
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1A],
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_4],
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_5A],
	)
	if profileGst != nil && profileGst.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE && values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1A].state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
		eightA = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, mergeTraceEvidence(values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1A], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_4], values[tammyv1.BasFormLabel_BAS_FORM_LABEL_5A]))
	}
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8A] = eightA
	oneBValue := values[tammyv1.BasFormLabel_BAS_FORM_LABEL_1B]
	if mixedGST || (profileGst != nil && profileGst.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE && oneBValue.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE) {
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8B] = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, oneBValue.evidence)
	} else if oneBValue.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8B] = knownZeroTraceValue(oneBValue.evidence)
	} else {
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8B] = copyTraceValue(oneBValue)
	}
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_9] = deriveAbsoluteDifference(
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8A],
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8B],
	)

	completeResult := false
	var direction tammyv1.BasAmountDirection
	if profileReady && !a.blocked && !a.unsupported {
		input, ready := pureCalculationInput(profileGst, profilePaygw, profilePaygi, g1, oneA, oneB, paygw, paygi)
		if ready {
			calculated, err := Calculate(input)
			if err == nil {
				values = valuesFromPureCalculation(calculated, profile, paygw, paygi, g1, oneA, oneB)
				completeResult = true
				direction = protoDirection(calculated.Form.Direction)
			} else {
				a.addCalculationError(err, mergeSourceRefs(gstProfileEvidence, paygwProfileEvidence, paygiProfileEvidence))
			}
		}
	}

	result := &tammyv1.BasCalculationResult{Labels: make([]*tammyv1.BasLabelTrace, 0, 18)}
	labels := orderedBASLabels()
	for _, label := range labels {
		value, ok := values[label]
		if !ok {
			value = unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET, nil)
		}
		operands := operandsForLabel(label, values, paygiMethod)
		trace, err := a.makeLabelTrace(label, operationForLabel(label), ruleForLabel(label), value, operands, values)
		if err != nil {
			a.blocker("CALCULATION_TRACE_BOUND", "A calculation trace value exceeds a supported exact-value bound.", value.evidence)
			trace = unknownLabelTrace(label, operationForLabel(label), ruleForLabel(label), value.state, value.evidence)
		}
		result.Labels = append(result.Labels, trace)
	}
	if completeResult && !a.blocked && !a.unsupported {
		result.Outcome = tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE
		result.Label_9Direction = direction
	} else if a.unsupported {
		result.Outcome = tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSUPPORTED
	} else {
		result.Outcome = tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE
	}
	return result, a.findings
}

func (a *basCalculationAdapter) checkProfileAndPeriod(profile *tammyv1.BasObligationsProfile, period *tammyv1.BasConfirmedPeriod,
	paygw *tammyv1.BasPaygwInputs, paygi *tammyv1.BasPaygiInputs) bool {
	ready := true
	if profile == nil || profile.GetProfile() == nil {
		a.blocker("BAS_PROFILE_UNKNOWN", "A complete BAS obligations profile is required to calculate this statement.", sourceRefProfile(profile))
		ready = false
	}
	if period == nil || period.GetConfirmation() == nil || period.GetConfirmation().GetPeriod() == nil {
		a.blocker("BAS_PERIOD_UNKNOWN", "A confirmed reporting period is required to calculate this statement.", nil)
		ready = false
	} else {
		switch period.GetConfirmation().GetSupportState() {
		case tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED:
		case tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_UNSPECIFIED:
			a.blocker("BAS_PERIOD_SUPPORT_UNKNOWN", "The confirmed period support has not been determined.", sourceRefProfile(profile))
			ready = false
		case tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_OTHER_UNSUPPORTED:
			a.unsupportedFinding("BAS_PERIOD_UNSUPPORTED", "The confirmed reporting period is outside the supported calculation window.", sourceRefProfile(profile))
			ready = false
		default:
			a.unsupportedFinding("BAS_PERIOD_UNSUPPORTED", "The confirmed reporting period is outside the supported calculation window.", sourceRefProfile(profile))
			ready = false
		}
	}
	if profile == nil || profile.GetProfile() == nil {
		return false
	}
	input := profile.GetProfile()
	if period != nil && period.GetConfirmation() != nil && period.GetConfirmation().GetPeriod() != nil {
		if input.GetEffectiveInterval() == nil {
			a.blocker("BAS_PROFILE_EFFECTIVE_INTERVAL_UNKNOWN", "The selected obligations profile does not have a confirmed effective interval.", mergeSourceRefs(input.GetEvidence(), sourceRefProfile(profile)))
			ready = false
		} else if !effectiveIntervalCovers(input.GetEffectiveInterval(), period.GetConfirmation().GetPeriod()) {
			a.blocker("BAS_PROFILE_EFFECTIVE_INTERVAL_MISMATCH", "The selected obligations profile does not cover the complete reporting period.", mergeSourceRefs(input.GetEvidence(), sourceRefProfile(profile)))
			ready = false
		}
	}
	periodWindow := tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_UNSPECIFIED
	if period != nil && period.GetConfirmation() != nil {
		periodWindow = period.GetConfirmation().GetWindow()
	}
	if input.GetGst() == nil {
		a.blocker("BAS_GST_PROFILE_UNKNOWN", "The GST profile section has not been confirmed.", input.GetEvidence())
		ready = false
	} else {
		gst := input.GetGst()
		switch gst.GetApplicability() {
		case tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE:
			if gst.GetRegistrationStatus() == tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_UNKNOWN || gst.GetRegistrationStatus() == tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_UNSPECIFIED {
				a.blocker("GST_REGISTRATION_UNKNOWN", "GST registration status has not been confirmed.", gst.GetEvidence())
				ready = false
			} else if gst.GetRegistrationStatus() != tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_REGISTERED {
				a.blocker("GST_PROFILE_INCONSISTENT", "The active GST profile conflicts with its registration status.", gst.GetEvidence())
				ready = false
			}
			periodDates := (*tammyv1.BasCivilDateRange)(nil)
			if period != nil && period.GetConfirmation() != nil {
				periodDates = period.GetConfirmation().GetPeriod()
			}
			if gst.GetRegistrationInterval() == nil {
				a.blocker("GST_REGISTRATION_INTERVAL_UNKNOWN", "The active GST registration interval has not been confirmed.", gst.GetEvidence())
				ready = false
			} else if periodDates != nil && !effectiveIntervalCovers(gst.GetRegistrationInterval(), periodDates) {
				a.unsupportedFinding("GST_PART_PERIOD_REGISTRATION", "GST registration does not cover the complete confirmed reporting period.", gst.GetEvidence())
				ready = false
			}
			if gst.GetReportingMethod() == tammyv1.BasGstMethod_BAS_GST_METHOD_UNKNOWN || gst.GetReportingMethod() == tammyv1.BasGstMethod_BAS_GST_METHOD_UNSPECIFIED {
				a.blocker("GST_METHOD_UNKNOWN", "The GST reporting method has not been confirmed.", gst.GetEvidence())
				ready = false
			} else if gst.GetReportingMethod() != tammyv1.BasGstMethod_BAS_GST_METHOD_SIMPLER {
				a.unsupportedFinding("GST_METHOD_UNSUPPORTED", "The active GST registration does not use the supported Simpler BAS method.", gst.GetEvidence())
				ready = false
			}
			if gst.GetGstBasis() == tammyv1.BasGstBasis_BAS_GST_BASIS_UNKNOWN || gst.GetGstBasis() == tammyv1.BasGstBasis_BAS_GST_BASIS_UNSPECIFIED {
				a.blocker("GST_BASIS_UNKNOWN", "The GST attribution basis has not been confirmed.", gst.GetEvidence())
				ready = false
			} else if gst.GetGstBasis() != tammyv1.BasGstBasis_BAS_GST_BASIS_CASH && gst.GetGstBasis() != tammyv1.BasGstBasis_BAS_GST_BASIS_ACCRUAL {
				a.unsupportedFinding("GST_BASIS_UNSUPPORTED", "The GST attribution basis is not supported for this calculation.", gst.GetEvidence())
				ready = false
			}
			if gst.GetCadence() == tammyv1.BasCadence_BAS_CADENCE_UNKNOWN || gst.GetCadence() == tammyv1.BasCadence_BAS_CADENCE_UNSPECIFIED {
				a.blocker("GST_CADENCE_UNKNOWN", "The GST reporting cadence has not been confirmed.", gst.GetEvidence())
				ready = false
			} else if !supportedCadence(gst.GetCadence(), periodWindow) {
				a.unsupportedFinding("GST_CADENCE_UNSUPPORTED", "The GST reporting cadence does not match the confirmed supported period.", gst.GetEvidence())
				ready = false
			}
			if gst.GetTurnoverCategory() == tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_UNKNOWN ||
				gst.GetTurnoverCategory() == tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_UNSPECIFIED {
				a.blocker("GST_TURNOVER_UNKNOWN", "The GST turnover category has not been confirmed.", gst.GetEvidence())
				ready = false
			} else if gst.GetTurnoverCategory() != tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_SMALL && gst.GetTurnoverCategory() != tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_MEDIUM {
				a.unsupportedFinding("GST_TURNOVER_UNSUPPORTED", "The GST turnover category is outside the supported calculation scope.", gst.GetEvidence())
				ready = false
			}
		case tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE:
			if gst.GetRegistrationStatus() != tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_NOT_REGISTERED {
				a.blocker("GST_REGISTRATION_UNKNOWN", "GST non-registration has not been confirmed for this profile.", gst.GetEvidence())
				ready = false
			}
		case tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN,
			tammyv1.BasApplicability_BAS_APPLICABILITY_UNSPECIFIED:
			a.blocker("GST_APPLICABILITY_UNKNOWN", "GST applicability has not been confirmed.", gst.GetEvidence())
			ready = false
		default:
			a.unsupportedFinding("GST_APPLICABILITY_UNSUPPORTED", "The GST applicability value is outside the supported calculation scope.", gst.GetEvidence())
			ready = false
		}
	}
	if input.GetPaygw() == nil || input.GetPaygw().GetApplicability() == nil {
		a.blocker("PAYGW_PROFILE_UNKNOWN", "The PAYG withholding profile section has not been confirmed.", input.GetEvidence())
		ready = false
	} else {
		paygw := input.GetPaygw()
		switch paygw.GetApplicability().GetApplicability() {
		case tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE:
			if paygw.GetCategory() == tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_UNKNOWN || paygw.GetCategory() == tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_UNSPECIFIED {
				a.blocker("PAYGW_CATEGORY_UNKNOWN", "The PAYG withholding category has not been confirmed.", mergeSourceRefs(paygw.GetApplicability().GetEvidence(), paygw.GetStatementRequirementEvidence()))
				ready = false
			} else if paygw.GetCategory() != tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_SMALL && paygw.GetCategory() != tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_MEDIUM {
				a.unsupportedFinding("PAYGW_CATEGORY_UNSUPPORTED", "Only small and medium PAYG withholding categories are supported.", mergeSourceRefs(paygw.GetApplicability().GetEvidence(), paygw.GetStatementRequirementEvidence()))
				ready = false
			}
			if paygw.GetCadence() == tammyv1.BasCadence_BAS_CADENCE_UNKNOWN || paygw.GetCadence() == tammyv1.BasCadence_BAS_CADENCE_UNSPECIFIED {
				a.blocker("PAYGW_CADENCE_UNKNOWN", "The PAYG withholding cadence has not been confirmed.", mergeSourceRefs(paygw.GetApplicability().GetEvidence(), paygw.GetStatementRequirementEvidence()))
				ready = false
			} else if !supportedCadence(paygw.GetCadence(), periodWindow) {
				a.unsupportedFinding("PAYGW_CADENCE_UNSUPPORTED", "The PAYG withholding cadence does not match the confirmed supported period.", mergeSourceRefs(paygw.GetApplicability().GetEvidence(), paygw.GetStatementRequirementEvidence()))
				ready = false
			}
			if paygw.GetPrefillRequirement() == tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNKNOWN || paygw.GetPrefillRequirement() == tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNSPECIFIED {
				a.blocker("PAYGW_PREFILL_REQUIREMENT_UNKNOWN", "The PAYG withholding prefill requirement has not been confirmed.", paygw.GetStatementRequirementEvidence())
				ready = false
			} else if paygw.GetPrefillRequirement() == tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNSUPPORTED {
				a.unsupportedFinding("PAYGW_PREFILL_UNSUPPORTED", "The statement-specific PAYG withholding prefill requirement is unsupported.", paygw.GetStatementRequirementEvidence())
				ready = false
			} else if paygw.GetPrefillRequirement() == tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNAVAILABLE {
				a.blocker("PAYGW_PREFILL_UNAVAILABLE", "The required statement-specific PAYG withholding prefill is unavailable.", paygw.GetStatementRequirementEvidence())
				ready = false
			}
		case tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE:
		case tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN,
			tammyv1.BasApplicability_BAS_APPLICABILITY_UNSPECIFIED:
			a.blocker("PAYGW_APPLICABILITY_UNKNOWN", "PAYG withholding applicability has not been confirmed.", paygw.GetApplicability().GetEvidence())
			ready = false
		default:
			a.unsupportedFinding("PAYGW_APPLICABILITY_UNSUPPORTED", "The PAYG withholding applicability value is outside the supported calculation scope.", paygw.GetApplicability().GetEvidence())
			ready = false
		}
	}
	if input.GetPaygi() == nil || input.GetPaygi().GetApplicability() == nil {
		a.blocker("PAYGI_PROFILE_UNKNOWN", "The PAYG instalment profile section has not been confirmed.", input.GetEvidence())
		ready = false
	} else {
		paygi := input.GetPaygi()
		switch paygi.GetApplicability().GetApplicability() {
		case tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE:
			if paygi.GetMethod() != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT && paygi.GetMethod() != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE {
				if paygi.GetMethod() == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNKNOWN || paygi.GetMethod() == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNSPECIFIED {
					a.blocker("PAYGI_PROFILE_METHOD_UNKNOWN", "The PAYG instalment method has not been confirmed in the obligations profile.", paygi.GetEvidence())
				} else {
					a.unsupportedFinding("PAYGI_PROFILE_METHOD_UNSUPPORTED", "The PAYG instalment method is outside the supported calculation scope.", paygi.GetEvidence())
				}
				ready = false
			}
			if paygi.GetCadence() == tammyv1.BasCadence_BAS_CADENCE_UNKNOWN || paygi.GetCadence() == tammyv1.BasCadence_BAS_CADENCE_UNSPECIFIED {
				a.blocker("PAYGI_CADENCE_UNKNOWN", "The PAYG instalment cadence has not been confirmed.", paygi.GetEvidence())
				ready = false
			} else if !supportedCadence(paygi.GetCadence(), periodWindow) {
				a.unsupportedFinding("PAYGI_CADENCE_UNSUPPORTED", "The PAYG instalment cadence does not match the confirmed supported period.", paygi.GetEvidence())
				ready = false
			}
			identity := paygi.GetConfirmedIncomeYear()
			periodDates := (*tammyv1.BasCivilDateRange)(nil)
			if period != nil && period.GetConfirmation() != nil {
				periodDates = period.GetConfirmation().GetPeriod()
			}
			if identity == nil || identity.GetDates() == nil || periodDates == nil {
				a.blocker("PAYGI_INCOME_YEAR_UNKNOWN", "The PAYG instalment income year is not confirmed for the reporting period.", paygi.GetEvidence())
				ready = false
			} else if !rangeCovers(identity.GetDates(), periodDates) {
				a.blocker("PAYGI_INCOME_YEAR_MISMATCH", "The confirmed PAYG instalment income year does not cover this reporting period.", mergeSourceRefs(paygi.GetEvidence(), adapterSourceRefs(identity.GetConfirmation())))
				ready = false
			}
		case tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE:
			if paygi.GetMethod() != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE {
				a.blocker("PAYGI_PROFILE_METHOD_CONFLICT", "The PAYG instalment method conflicts with the not-applicable profile.", paygi.GetEvidence())
				ready = false
			}
		case tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN,
			tammyv1.BasApplicability_BAS_APPLICABILITY_UNSPECIFIED:
			a.blocker("PAYGI_APPLICABILITY_UNKNOWN", "PAYG instalment applicability has not been confirmed.", paygi.GetApplicability().GetEvidence())
			ready = false
		default:
			a.unsupportedFinding("PAYGI_APPLICABILITY_UNSUPPORTED", "The PAYG instalment applicability value is outside the supported calculation scope.", paygi.GetApplicability().GetEvidence())
			ready = false
		}
	}
	if input.GetOtherObligations() == nil {
		a.blocker("BAS_OTHER_OBLIGATIONS_UNKNOWN", "The other activity-statement obligations have not been reviewed.", input.GetEvidence())
		ready = false
	} else {
		answers := []struct {
			code string
			v    *tammyv1.BasObligationAnswer
		}{
			{"FBT", input.GetOtherObligations().GetFringeBenefitsTax()},
			{"WET", input.GetOtherObligations().GetWineEqualisationTax()},
			{"LCT", input.GetOtherObligations().GetLuxuryCarTax()},
			{"FUEL_TAX_CREDITS", input.GetOtherObligations().GetFuelTaxCredits()},
			{"DEFERRED_COMPANY_INSTALMENTS", input.GetOtherObligations().GetDeferredCompanyInstalments()},
			{"DEFERRED_OR_IMPORT_GST", input.GetOtherObligations().GetDeferredOrImportGst()},
			{"SPECIAL_GST_SCHEMES_OR_ADJUSTMENTS", input.GetOtherObligations().GetSpecialGstSchemesOrAdjustments()},
			{"OTHER_ACTIVITY_STATEMENT_OBLIGATIONS", input.GetOtherObligations().GetOtherActivityStatementObligations()},
		}
		for _, answer := range answers {
			if answer.v == nil {
				a.blocker("BAS_OTHER_OBLIGATION_UNKNOWN", "An activity-statement obligation has not been reviewed.", input.GetEvidence())
				ready = false
				continue
			}
			switch answer.v.GetApplicability() {
			case tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE:
			case tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN,
				tammyv1.BasApplicability_BAS_APPLICABILITY_UNSPECIFIED:
				a.blocker("BAS_OTHER_OBLIGATION_UNKNOWN", "An activity-statement obligation has not been reviewed.", answer.v.GetEvidence())
				ready = false
			case tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE:
				a.unsupportedFinding("BAS_OTHER_OBLIGATION_UNSUPPORTED", "An active activity-statement obligation is outside the supported calculation scope.", answer.v.GetEvidence())
				ready = false
			default:
				a.unsupportedFinding("BAS_OTHER_OBLIGATION_UNSUPPORTED", "An activity-statement obligation has an unsupported applicability value.", answer.v.GetEvidence())
				ready = false
			}
		}
	}
	if paygi != nil && input.GetPaygi() != nil && paygi.GetMethod() != input.GetPaygi().GetMethod() {
		a.blocker("PAYGI_METHOD_PROFILE_MISMATCH", "The PAYG instalment input method does not match the confirmed obligations profile.", mergeSourceRefs(paygiEvidence(paygi), input.GetPaygi().GetEvidence()))
		ready = false
	}
	if paygi != nil && input.GetPaygi() != nil && input.GetPaygi().GetConfirmedIncomeYear() != nil && !proto.Equal(paygi.GetConfirmedIncomeYear(), input.GetPaygi().GetConfirmedIncomeYear()) {
		a.blocker("PAYGI_INCOME_YEAR_PROFILE_MISMATCH", "The PAYG instalment inputs do not use the income year pinned by the obligations profile.", mergeSourceRefs(paygiEvidence(paygi), input.GetPaygi().GetEvidence()))
		ready = false
	}
	if paygw != nil && input.GetPaygw() != nil && input.GetPaygw().GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
		if paygw.GetCategory() != input.GetPaygw().GetCategory() {
			a.blocker("PAYGW_CATEGORY_PROFILE_MISMATCH", "The PAYG withholding input category does not match the confirmed obligations profile.", mergeSourceRefs(payrollEvidence(paygw), input.GetPaygw().GetStatementRequirementEvidence()))
			ready = false
		}
		if (input.GetPaygw().GetPrefillRequirement() == tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_NOT_REQUIRED && paygw.GetPrefillReview() != tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_NOT_REQUIRED) ||
			(input.GetPaygw().GetPrefillRequirement() == tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_REVIEWED && paygw.GetPrefillReview() != tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_REVIEWED) {
			a.blocker("PAYGW_PREFILL_REVIEW_MISMATCH", "The PAYG withholding prefill review does not match the confirmed statement requirement.", mergeSourceRefs(payrollEvidence(paygw), input.GetPaygw().GetStatementRequirementEvidence()))
			ready = false
		}
	}
	return ready
}

func supportedCadence(cadence tammyv1.BasCadence, window tammyv1.BasPeriodWindow) bool {
	if window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_MONTHLY {
		return cadence == tammyv1.BasCadence_BAS_CADENCE_MONTHLY
	}
	quarter := window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP ||
		window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC ||
		window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR ||
		window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN
	return quarter && cadence == tammyv1.BasCadence_BAS_CADENCE_QUARTERLY
}

// BASProfilePeriodAssessment is shared by ConfirmBasPeriod admission and the
// calculator. An unspecified state means the profile facts are incomplete;
// only SUPPORTED is ready for a new preparation.
type BASProfilePeriodAssessment struct {
	SupportState tammyv1.BasPeriodSupportState
	Findings     []*tammyv1.BasPeriodSupportFinding
}

// AssessBASProfilePeriod applies the same supported-scope rules before a period
// is persisted and before a calculation is attempted. Calendar and authority
// provenance checks remain the period command's responsibility.
func AssessBASProfilePeriod(profile *tammyv1.BasObligationsProfile, period *tammyv1.BasCivilDateRange,
	window tammyv1.BasPeriodWindow) BASProfilePeriodAssessment {
	a := &basCalculationAdapter{codes: make(map[string]struct{})}
	assessmentPeriod := &tammyv1.BasConfirmedPeriod{Confirmation: &tammyv1.BasPeriodConfirmationInput{
		Period: period, Window: window,
		// The candidate is being assessed, so the state field is a placeholder;
		// checkProfileAndPeriod evaluates only the selected profile's facts.
		SupportState: tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED,
	}}
	a.checkProfileAndPeriod(profile, assessmentPeriod, nil, nil)

	result := BASProfilePeriodAssessment{SupportState: derivedSupportState(profile, window, a.findings)}
	result.Findings = make([]*tammyv1.BasPeriodSupportFinding, 0, len(a.findings))
	for _, finding := range a.findings {
		result.Findings = append(result.Findings, &tammyv1.BasPeriodSupportFinding{
			StableCode:  finding.StableCode,
			SafeMessage: finding.SafeMessage,
			Sources:     cloneSourceRefs(finding.Sources),
		})
	}
	return result
}

func derivedSupportState(profile *tammyv1.BasObligationsProfile, window tammyv1.BasPeriodWindow,
	findings []CalculationFindingTemplate) tammyv1.BasPeriodSupportState {
	hasUnsupported, hasUnknown := false, false
	codes := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		codes[finding.StableCode] = struct{}{}
		if finding.Severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED {
			hasUnsupported = true
		} else if finding.Severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER {
			hasUnknown = true
		}
	}
	if !hasUnsupported && !hasUnknown {
		return tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED
	}
	if _, ok := codes["GST_PART_PERIOD_REGISTRATION"]; ok {
		return tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_PART_PERIOD_REGISTRATION
	}
	if hasAnnualCadence(profile) {
		return tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_ANNUAL_METHOD
	}
	if mixedCadences(profile) {
		return tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_MIXED_WINDOW
	}
	if cadenceMismatchesWindow(profile, window) {
		return tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_CADENCE_CHANGE
	}
	if hasUnsupported {
		return tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_OTHER_UNSUPPORTED
	}
	return tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_OTHER_UNSUPPORTED
}

func activeCadences(profile *tammyv1.BasObligationsProfile) []tammyv1.BasCadence {
	if profile == nil || profile.GetProfile() == nil {
		return nil
	}
	input := profile.GetProfile()
	var result []tammyv1.BasCadence
	if gst := input.GetGst(); gst != nil && gst.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
		result = append(result, gst.GetCadence())
	}
	if paygw := input.GetPaygw(); paygw != nil && paygw.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
		result = append(result, paygw.GetCadence())
	}
	if paygi := input.GetPaygi(); paygi != nil && paygi.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE {
		result = append(result, paygi.GetCadence())
	}
	return result
}

func hasAnnualCadence(profile *tammyv1.BasObligationsProfile) bool {
	for _, cadence := range activeCadences(profile) {
		if cadence == tammyv1.BasCadence_BAS_CADENCE_ANNUAL {
			return true
		}
	}
	return false
}

func mixedCadences(profile *tammyv1.BasObligationsProfile) bool {
	monthly, quarterly := false, false
	for _, cadence := range activeCadences(profile) {
		monthly = monthly || cadence == tammyv1.BasCadence_BAS_CADENCE_MONTHLY
		quarterly = quarterly || cadence == tammyv1.BasCadence_BAS_CADENCE_QUARTERLY
	}
	return monthly && quarterly
}

func cadenceMismatchesWindow(profile *tammyv1.BasObligationsProfile, window tammyv1.BasPeriodWindow) bool {
	for _, cadence := range activeCadences(profile) {
		if cadence == tammyv1.BasCadence_BAS_CADENCE_MONTHLY || cadence == tammyv1.BasCadence_BAS_CADENCE_QUARTERLY {
			if !supportedCadence(cadence, window) {
				return true
			}
		}
	}
	return false
}

func rangeCovers(outer, inner *tammyv1.BasCivilDateRange) bool {
	if outer == nil || inner == nil || outer.GetStartDate() == nil || outer.GetEndDate() == nil || inner.GetStartDate() == nil || inner.GetEndDate() == nil {
		return false
	}
	dateKey := func(value *tammyv1.CivilDate) int64 {
		return int64(value.GetYear())*10000 + int64(value.GetMonth())*100 + int64(value.GetDay())
	}
	return dateKey(outer.GetStartDate()) <= dateKey(inner.GetStartDate()) && dateKey(outer.GetEndDate()) >= dateKey(inner.GetEndDate())
}

func effectiveIntervalCovers(outer *tammyv1.BasEffectiveInterval, inner *tammyv1.BasCivilDateRange) bool {
	if outer == nil || inner == nil || outer.GetStartDate() == nil || inner.GetStartDate() == nil || inner.GetEndDate() == nil {
		return false
	}
	dateKey := func(value *tammyv1.CivilDate) int64 {
		return int64(value.GetYear())*10000 + int64(value.GetMonth())*100 + int64(value.GetDay())
	}
	if dateKey(outer.GetStartDate()) > dateKey(inner.GetStartDate()) {
		return false
	}
	return outer.GetEndDate() == nil || dateKey(outer.GetEndDate()) >= dateKey(inner.GetEndDate())
}

func profileSectionGst(profile *tammyv1.BasObligationsProfile) *tammyv1.BasGstProfileInput {
	if profile == nil || profile.GetProfile() == nil {
		return nil
	}
	return profile.GetProfile().GetGst()
}

func profileSectionPaygw(profile *tammyv1.BasObligationsProfile) *tammyv1.BasPaygwProfileInput {
	if profile == nil || profile.GetProfile() == nil {
		return nil
	}
	return profile.GetProfile().GetPaygw()
}

func profileSectionPaygi(profile *tammyv1.BasObligationsProfile) *tammyv1.BasPaygiProfileInput {
	if profile == nil || profile.GetProfile() == nil {
		return nil
	}
	return profile.GetProfile().GetPaygi()
}

func gstSectionEvidence(profile *tammyv1.BasGstProfileInput) []*tammyv1.SourceRef {
	if profile == nil {
		return nil
	}
	return profile.GetEvidence()
}

func paygwSectionEvidence(profile *tammyv1.BasPaygwProfileInput) []*tammyv1.SourceRef {
	if profile == nil {
		return nil
	}
	return mergeSourceRefs(profile.GetApplicability().GetEvidence(), profile.GetStatementRequirementEvidence())
}

func paygiSectionEvidence(profile *tammyv1.BasPaygiProfileInput) []*tammyv1.SourceRef {
	if profile == nil {
		return nil
	}
	refs := profile.GetEvidence()
	if profile.GetConfirmedIncomeYear() != nil {
		refs = mergeSourceRefs(refs, adapterSourceRefs(profile.GetConfirmedIncomeYear().GetConfirmation()))
	}
	return refs
}

func adapterSourceRefs(value *tammyv1.SourceRef) []*tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return []*tammyv1.SourceRef{value}
}

func sourceRefProfile(value *tammyv1.BasObligationsProfile) []*tammyv1.SourceRef {
	if value == nil || value.GetProfileId() == "" || value.GetRevision() == 0 || len(value.GetContentHash()) != 32 {
		return nil
	}
	return []*tammyv1.SourceRef{{Type: "bas_obligations_profile", Id: value.GetProfileId(), Revision: value.GetRevision(), ContentHash: append([]byte(nil), value.GetContentHash()...)}}
}

func payrollEvidence(value *tammyv1.BasPaygwInputs) []*tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return value.GetPayrollStatementEvidence()
}

func paygiEvidence(value *tammyv1.BasPaygiInputs) []*tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return value.GetEvidence()
}

func incomeYearEvidence(value *tammyv1.BasPaygiInputs) []*tammyv1.SourceRef {
	if value == nil || value.GetConfirmedIncomeYear() == nil {
		return nil
	}
	return adapterSourceRefs(value.GetConfirmedIncomeYear().GetConfirmation())
}

func (a *basCalculationAdapter) amountValue(code string, input *tammyv1.BasAmountInput, commonEvidence []*tammyv1.SourceRef) calculationTraceValue {
	if input == nil {
		a.blocker(code+"_UNSET", "A required BAS amount has not been entered or reviewed.", commonEvidence)
		return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET, commonEvidence)
	}
	evidence := a.mergeEvidence(input.GetEvidence(), commonEvidence)
	state := input.GetState()
	switch state {
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET:
		if input.GetAmount() != nil {
			return a.invalidAmount(code, evidence)
		}
		a.blocker(code+"_UNSET", "A required BAS amount has not been entered or reviewed.", evidence)
		return unknownTraceValue(state, evidence)
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN:
		if input.GetAmount() != nil {
			return a.invalidAmount(code, evidence)
		}
		a.blocker(code+"_UNKNOWN", "A required BAS amount remains unknown.", evidence)
		return unknownTraceValue(state, evidence)
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE:
		if input.GetAmount() != nil {
			return a.invalidAmount(code, evidence)
		}
		return notApplicableTraceValue(evidence)
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO,
		tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN:
		money := input.GetAmount()
		if money == nil || money.GetCurrencyCode() != "AUD" || money.GetMinorUnits() < 0 {
			return a.invalidAmount(code, evidence)
		}
		if state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO && money.GetMinorUnits() != 0 ||
			state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN && money.GetMinorUnits() <= 0 {
			return a.invalidAmount(code, evidence)
		}
		return knownAmountTraceValue(money.GetMinorUnits(), evidence)
	default:
		return a.invalidAmount(code, evidence)
	}
}

func (a *basCalculationAdapter) invalidAmount(code string, evidence []*tammyv1.SourceRef) calculationTraceValue {
	a.blocker("CALCULATION_AMOUNT_INVALID", "A BAS amount has an invalid state or currency payload.", evidence)
	return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, evidence)
}

func (a *basCalculationAdapter) rateValue(code string, value *tammyv1.Decimal, evidence []*tammyv1.SourceRef) calculationTraceValue {
	if value == nil {
		a.blocker(code+"_UNSET", "The notified PAYG instalment rate has not been supplied.", evidence)
		return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET, evidence)
	}
	if value.GetCoefficient() < 0 || value.GetScale() > uint32(MaxDecimalScale) {
		a.blocker("PAYGI_RATE_INVALID", "The PAYG instalment rate is outside the supported exact decimal bound.", evidence)
		return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, evidence)
	}
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(value.GetScale())), nil)
	rate := new(big.Rat).SetFrac(big.NewInt(value.GetCoefficient()), den)
	state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	if rate.Sign() == 0 {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	}
	return calculationTraceValue{state: state, exactRate: rate, evidence: evidence}
}

func knownAmountTraceValue(cents int64, evidence []*tammyv1.SourceRef) calculationTraceValue {
	exact := new(big.Rat).SetInt64(cents)
	reported := cents / 100
	round := new(big.Rat).Sub(new(big.Rat).Set(exact), new(big.Rat).SetInt64(reported*100))
	state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	if cents == 0 {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	}
	return calculationTraceValue{state: state, exactCents: exact, reported: reported, rounding: round, evidence: evidence}
}

func unknownTraceValue(state tammyv1.BasAmountState, evidence []*tammyv1.SourceRef) calculationTraceValue {
	return calculationTraceValue{state: state, evidence: evidence}
}

func notApplicableTraceValue(evidence []*tammyv1.SourceRef) calculationTraceValue {
	return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE, evidence)
}

func knownZeroTraceValue(evidence []*tammyv1.SourceRef) calculationTraceValue {
	return knownAmountTraceValue(0, evidence)
}

func deriveT11(t1, t2 calculationTraceValue) calculationTraceValue {
	if !isKnownState(t1.state) || t1.exactCents == nil || !isKnownState(t2.state) || t2.exactRate == nil {
		return unknownTraceValue(propagatedUnknownState(t1.state, t2.state), mergeSourceRefs(t1.evidence, t2.evidence))
	}
	// T11 exact cents = reportable T1 whole dollars multiplied by notified T2
	// percentage points. The result is exact because T2 is a base-10 rational.
	reportedDollars := big.NewInt(t1.reported)
	exact := new(big.Rat).Mul(new(big.Rat).SetInt(reportedDollars), t2.exactRate)
	wholeDollars := new(big.Int).Quo(new(big.Int).Set(exact.Num()), new(big.Int).Mul(new(big.Int).Set(exact.Denom()), big.NewInt(100)))
	if !wholeDollars.IsInt64() {
		return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, mergeSourceRefs(t1.evidence, t2.evidence))
	}
	rounding := new(big.Rat).Sub(new(big.Rat).Set(exact), new(big.Rat).SetInt(new(big.Int).Mul(new(big.Int).Set(wholeDollars), big.NewInt(100))))
	state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	if exact.Sign() == 0 {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	}
	return calculationTraceValue{state: state, exactCents: exact, reported: wholeDollars.Int64(), rounding: rounding, evidence: mergeSourceRefs(t1.evidence, t2.evidence)}
}

func deriveSectionSum(values ...calculationTraceValue) calculationTraceValue {
	allNA := len(values) != 0
	for _, value := range values {
		if value.state != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
			allNA = false
		}
	}
	if allNA {
		return notApplicableTraceValue(mergeTraceEvidence(values...))
	}
	for _, value := range values {
		if !isKnownState(value.state) && value.state != tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
			return unknownTraceValue(propagatedUnknownState(states(values)...), mergeTraceEvidence(values...))
		}
	}
	exact := new(big.Rat)
	reported := int64(0)
	for _, value := range values {
		if value.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
			continue
		}
		if value.exactCents == nil {
			return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, mergeTraceEvidence(values...))
		}
		exact.Add(exact, value.exactCents)
		if value.reported > 0 && reported > int64(^uint64(0)>>1)-value.reported {
			return unknownTraceValue(tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN, mergeTraceEvidence(values...))
		}
		reported += value.reported
	}
	reportedCents := new(big.Int).Mul(big.NewInt(reported), big.NewInt(100))
	round := new(big.Rat).Sub(new(big.Rat).Set(exact), new(big.Rat).SetInt(reportedCents))
	state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	if exact.Sign() == 0 {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	}
	return calculationTraceValue{state: state, exactCents: exact, reported: reported, rounding: round, evidence: mergeTraceEvidence(values...)}
}

func hasMixedApplicability(values ...calculationTraceValue) bool {
	hasApplicable, hasNotApplicable := false, false
	for _, value := range values {
		if value.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
			hasNotApplicable = true
		}
		if isKnownState(value.state) || value.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN || value.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET {
			hasApplicable = true
		}
	}
	return hasApplicable && hasNotApplicable
}

func deriveAbsoluteDifference(left, right calculationTraceValue) calculationTraceValue {
	if !isKnownState(left.state) || !isKnownState(right.state) || left.exactCents == nil || right.exactCents == nil {
		return unknownTraceValue(propagatedUnknownState(left.state, right.state), mergeSourceRefs(left.evidence, right.evidence))
	}
	exact := new(big.Rat).Sub(new(big.Rat).Set(left.exactCents), right.exactCents)
	if exact.Sign() < 0 {
		exact.Neg(exact)
	}
	reported := left.reported - right.reported
	if reported < 0 {
		reported = -reported
	}
	reportedCents := new(big.Int).Mul(big.NewInt(reported), big.NewInt(100))
	round := new(big.Rat).Sub(new(big.Rat).Set(exact), new(big.Rat).SetInt(reportedCents))
	state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	if exact.Sign() == 0 {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	}
	return calculationTraceValue{state: state, exactCents: exact, reported: reported, rounding: round, evidence: mergeSourceRefs(left.evidence, right.evidence)}
}

func copyTraceValue(source calculationTraceValue) calculationTraceValue {
	copied := source
	copied.evidence = append([]*tammyv1.SourceRef(nil), source.evidence...)
	if source.exactCents != nil {
		copied.exactCents = new(big.Rat).Set(source.exactCents)
	}
	if source.rounding != nil {
		copied.rounding = new(big.Rat).Set(source.rounding)
	}
	if source.exactRate != nil {
		copied.exactRate = new(big.Rat).Set(source.exactRate)
	}
	return copied
}

func states(values []calculationTraceValue) []tammyv1.BasAmountState {
	out := make([]tammyv1.BasAmountState, 0, len(values))
	for _, value := range values {
		out = append(out, value.state)
	}
	return out
}

func propagatedUnknownState(states ...tammyv1.BasAmountState) tammyv1.BasAmountState {
	for _, state := range states {
		if state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN {
			return tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN
		}
	}
	return tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET
}

func isKnownState(state tammyv1.BasAmountState) bool {
	return state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN || state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
}

func mergeTraceEvidence(values ...calculationTraceValue) []*tammyv1.SourceRef {
	refs := make([][]*tammyv1.SourceRef, 0, len(values))
	for _, value := range values {
		refs = append(refs, value.evidence)
	}
	return mergeSourceRefs(refs...)
}

func pureCalculationInput(
	gst *tammyv1.BasGstProfileInput,
	paygwProfile *tammyv1.BasPaygwProfileInput,
	paygiProfile *tammyv1.BasPaygiProfileInput,
	g1, oneA, oneB *tammyv1.BasAmountInput,
	paygw *tammyv1.BasPaygwInputs,
	paygi *tammyv1.BasPaygiInputs,
) (CalculationInput, bool) {
	var input CalculationInput
	if gst == nil || paygwProfile == nil || paygwProfile.GetApplicability() == nil || paygiProfile == nil || paygiProfile.GetApplicability() == nil {
		return input, false
	}
	var ok bool
	if gst.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		input.GST = GSTInput{G1: AmountInput{State: ValueNotApplicable}, OneA: AmountInput{State: ValueNotApplicable}, OneB: AmountInput{State: ValueNotApplicable}}
	} else {
		if input.GST.G1, ok = pureAmountInput(g1); !ok {
			return input, false
		}
		if input.GST.OneA, ok = pureAmountInput(oneA); !ok {
			return input, false
		}
		if input.GST.OneB, ok = pureAmountInput(oneB); !ok {
			return input, false
		}
	}
	if paygwProfile.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		input.PAYGW = PAYGWInput{W1: AmountInput{State: ValueNotApplicable}, W2: AmountInput{State: ValueNotApplicable}, W3: AmountInput{State: ValueNotApplicable}, W4: AmountInput{State: ValueNotApplicable}}
	} else {
		if paygw == nil {
			return input, false
		}
		if input.PAYGW.W1, ok = pureAmountInput(paygw.GetW1TotalPayments()); !ok {
			return input, false
		}
		if input.PAYGW.W2, ok = pureAmountInput(paygw.GetW2AmountWithheld()); !ok {
			return input, false
		}
		if input.PAYGW.W3, ok = pureAmountInput(paygw.GetW3OtherWithholding()); !ok {
			return input, false
		}
		if input.PAYGW.W4, ok = pureAmountInput(paygw.GetW4NoAbnWithholding()); !ok {
			return input, false
		}
	}
	if paygiProfile.GetApplicability().GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		input.PAYGI = PAYGIInput{Method: PAYGIMethodNotApplicable}
	} else {
		if paygi == nil {
			return input, false
		}
		input.PAYGI.Method = purePAYGIMethod(paygi.GetMethod())
		input.PAYGI.Operative = pureOperative(paygi.GetNoticeOperativeStatus())
		input.PAYGI.PriorVariation = purePriorVariation(paygi.GetPriorVariation())
		input.PAYGI.Notice = pureNoticeReference(paygi.GetNotice())
		input.PAYGI.RequiresVariationFields = paygi.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_VARIATION_FIELDS_REQUIRED || paygi.GetProposedVariationAmount() != nil || paygi.GetProposedVariationRate() != nil
		input.PAYGI.Claims5BCredit = paygi.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_CLAIM_5B || paygi.GetClaim_5B() != nil
		switch input.PAYGI.Method {
		case PAYGIMethodAmount:
			if input.PAYGI.T7, ok = pureAmountInput(paygi.GetT7Amount()); !ok {
				return input, false
			}
		case PAYGIMethodRate:
			if input.PAYGI.T1, ok = pureAmountInput(paygi.GetRateT1()); !ok {
				return input, false
			}
			input.PAYGI.T1Reviewed = paygi.GetT1ReviewDecision() == tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_REVIEWED
			if rate := paygi.GetRateT2(); rate != nil {
				input.PAYGI.Rate = &Decimal{Coefficient: rate.GetCoefficient(), Scale: uint8(rate.GetScale())}
			}
		default:
			return input, false
		}
	}
	input.OtherObligationsReviewed = true
	input.OtherObligations = []OtherObligation{
		{Code: "FBT", Applicability: ApplicabilityNotApplicable},
		{Code: "WET", Applicability: ApplicabilityNotApplicable},
		{Code: "LCT", Applicability: ApplicabilityNotApplicable},
		{Code: "FUEL_TAX_CREDITS", Applicability: ApplicabilityNotApplicable},
		{Code: "DEFERRED_COMPANY_INSTALMENTS", Applicability: ApplicabilityNotApplicable},
		{Code: "DEFERRED_OR_IMPORT_GST", Applicability: ApplicabilityNotApplicable},
		{Code: "SPECIAL_GST_SCHEMES_OR_ADJUSTMENTS", Applicability: ApplicabilityNotApplicable},
		{Code: "OTHER_ACTIVITY_STATEMENT_OBLIGATIONS", Applicability: ApplicabilityNotApplicable},
	}
	return input, true
}

func pureAmountInput(input *tammyv1.BasAmountInput) (AmountInput, bool) {
	if input == nil {
		return AmountInput{}, false
	}
	result := AmountInput{State: pureValueState(input.GetState())}
	if input.GetAmount() != nil {
		result.Cents = input.GetAmount().GetMinorUnits()
	}
	for _, ref := range input.GetEvidence() {
		result.Evidence = append(result.Evidence, evidenceReference(ref))
	}
	if result.State == ValueUnset || result.State == ValueUnknown {
		return result, false
	}
	return result, true
}

func pureValueState(value tammyv1.BasAmountState) ValueState {
	switch value {
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE:
		return ValueNotApplicable
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO:
		return ValueKnownZero
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN:
		return ValueKnown
	case tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN:
		return ValueUnknown
	default:
		return ValueUnset
	}
}

func purePAYGIMethod(value tammyv1.BasPaygiMethod) PAYGIMethod {
	switch value {
	case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE:
		return PAYGIMethodNotApplicable
	case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT:
		return PAYGIMethodAmount
	case tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE:
		return PAYGIMethodRate
	default:
		return PAYGIMethodUnset
	}
}

func pureOperative(value tammyv1.BasNoticeOperativeStatus) PAYGIOperativeState {
	if value == tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_OPERATIVE {
		return PAYGIOperativeConfirmed
	}
	return PAYGIOperativeUnknown
}

func purePriorVariation(value tammyv1.BasPriorVariationDisposition) PAYGIPriorVariationState {
	switch value {
	case tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NONE:
		return PAYGIPriorVariationNone
	case tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_REFLECTED_IN_CURRENT_NOTICE:
		return PAYGIPriorVariationReflectedInNotice
	case tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_UNCERTAIN:
		return PAYGIPriorVariationUncertain
	case tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NEW:
		return PAYGIPriorVariationNew
	default:
		return PAYGIPriorVariationUnknown
	}
}

func pureNoticeReference(ref *tammyv1.SourceRef) NoticeReference {
	if ref == nil {
		return NoticeReference{}
	}
	return NoticeReference{Reference: stableSourceKey(ref), SHA256: hex.EncodeToString(ref.GetContentHash())}
}

func evidenceReference(ref *tammyv1.SourceRef) EvidenceReference {
	if ref == nil {
		return EvidenceReference{}
	}
	return EvidenceReference{Reference: stableSourceKey(ref), SHA256: hex.EncodeToString(ref.GetContentHash())}
}

func stableSourceKey(ref *tammyv1.SourceRef) string {
	if ref == nil {
		return ""
	}
	return ref.GetType() + "/" + ref.GetId() + "@" + strconv.FormatUint(ref.GetRevision(), 10)
}

func valuesFromPureCalculation(value CalculationResult, profile *tammyv1.BasObligationsProfile,
	paygw *tammyv1.BasPaygwInputs, paygi *tammyv1.BasPaygiInputs,
	g1, oneA, oneB *tammyv1.BasAmountInput) map[tammyv1.BasFormLabel]calculationTraceValue {
	gstProfileRefs := gstSectionEvidence(profileSectionGst(profile))
	paygwProfileRefs := paygwSectionEvidence(profileSectionPaygw(profile))
	paygiProfileRefs := paygiSectionEvidence(profileSectionPaygi(profile))
	gstRefs := func(input *tammyv1.BasAmountInput) []*tammyv1.SourceRef {
		if input == nil {
			return gstProfileRefs
		}
		return mergeSourceRefs(gstProfileRefs, input.GetEvidence())
	}
	paygwRefs := func(input *tammyv1.BasAmountInput) []*tammyv1.SourceRef {
		if input == nil {
			return mergeSourceRefs(paygwProfileRefs, payrollEvidence(paygw))
		}
		return mergeSourceRefs(paygwProfileRefs, payrollEvidence(paygw), input.GetEvidence())
	}
	paygiRefs := func(input *tammyv1.BasAmountInput) []*tammyv1.SourceRef {
		refs := mergeSourceRefs(paygiProfileRefs, paygiEvidence(paygi))
		if paygi != nil {
			refs = mergeSourceRefs(refs, adapterSourceRefs(paygi.GetNotice()))
		}
		if input != nil {
			refs = mergeSourceRefs(refs, input.GetEvidence())
		}
		return refs
	}
	values := make(map[tammyv1.BasFormLabel]calculationTraceValue, 18)
	set := func(label tammyv1.BasFormLabel, trace AmountTrace, evidence []*tammyv1.SourceRef) {
		values[label] = valueFromPureAmount(trace, evidence)
	}
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_G1, value.GST.G1, gstRefs(g1))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_1A, value.GST.OneA, gstRefs(oneA))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_1B, value.GST.OneB, gstRefs(oneB))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_W1, value.PAYGW.W1, paygwRefs(amountInputAt(paygw, 1)))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_W2, value.PAYGW.W2, paygwRefs(amountInputAt(paygw, 2)))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_W3, value.PAYGW.W3, paygwRefs(amountInputAt(paygw, 3)))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_W4, value.PAYGW.W4, paygwRefs(amountInputAt(paygw, 4)))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_W5, value.PAYGW.W5, mergeSourceRefs(paygwRefs(amountInputAt(paygw, 2)), paygwRefs(amountInputAt(paygw, 3)), paygwRefs(amountInputAt(paygw, 4))))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_4, value.PAYGW.Four, mergeSourceRefs(paygwRefs(amountInputAt(paygw, 2)), paygwRefs(amountInputAt(paygw, 3)), paygwRefs(amountInputAt(paygw, 4))))
	t1Input, t7Input := (*tammyv1.BasAmountInput)(nil), (*tammyv1.BasAmountInput)(nil)
	if paygi != nil {
		t1Input, t7Input = paygi.GetRateT1(), paygi.GetT7Amount()
	}
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_T1, value.PAYGI.T1, paygiRefs(t1Input))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_T7, value.PAYGI.T7, paygiRefs(t7Input))
	t2Refs := mergeSourceRefs(paygiProfileRefs, paygiEvidence(paygi), incomeYearEvidence(paygi))
	if value.PAYGI.Rate != nil {
		set(tammyv1.BasFormLabel_BAS_FORM_LABEL_T11, value.PAYGI.FiveA, mergeSourceRefs(paygiRefs(t1Input), t2Refs))
	} else {
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_T11] = notApplicableTraceValue(paygiProfileRefs)
	}
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_5A, value.PAYGI.FiveA, mergeSourceRefs(paygiRefs(t1Input), paygiRefs(t7Input), t2Refs))
	values[tammyv1.BasFormLabel_BAS_FORM_LABEL_5B] = notApplicableTraceValue(paygiProfileRefs)
	if paygi != nil && value.PAYGI.Rate != nil {
		rate := decimalRational(*value.PAYGI.Rate)
		state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
		if rate.Sign() == 0 {
			state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
		}
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_T2] = calculationTraceValue{state: state, exactRate: rate, evidence: t2Refs}
	} else {
		values[tammyv1.BasFormLabel_BAS_FORM_LABEL_T2] = notApplicableTraceValue(paygiProfileRefs)
	}
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_8A, value.Form.EightA, mergeSourceRefs(gstRefs(oneA), paygwRefs(amountInputAt(paygw, 2)), paygwRefs(amountInputAt(paygw, 3)), paygwRefs(amountInputAt(paygw, 4)), paygiRefs(t1Input), paygiRefs(t7Input)))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_8B, value.Form.EightB, gstRefs(oneB))
	set(tammyv1.BasFormLabel_BAS_FORM_LABEL_9, value.Form.Nine, mergeSourceRefs(mergeTraceEvidence(values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8A]), mergeTraceEvidence(values[tammyv1.BasFormLabel_BAS_FORM_LABEL_8B])))
	return values
}

func amountInputAt(value *tammyv1.BasPaygwInputs, label int) *tammyv1.BasAmountInput {
	if value == nil {
		return nil
	}
	switch label {
	case 1:
		return value.GetW1TotalPayments()
	case 2:
		return value.GetW2AmountWithheld()
	case 3:
		return value.GetW3OtherWithholding()
	case 4:
		return value.GetW4NoAbnWithholding()
	}
	return nil
}

func valueFromPureAmount(value AmountTrace, evidence []*tammyv1.SourceRef) calculationTraceValue {
	state := protoValueState(value.State)
	if state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE || state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET || state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN {
		return unknownTraceValue(state, evidence)
	}
	exact, _ := rationalFromText(value.ExactCents.Numerator, value.ExactCents.Denominator)
	rounding, _ := rationalFromText(value.OmittedCents.Numerator, value.OmittedCents.Denominator)
	return calculationTraceValue{state: state, exactCents: exact, reported: value.ReportedWholeDollars, rounding: rounding, evidence: evidence}
}

func decimalRational(value Decimal) *big.Rat {
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(value.Scale)), nil)
	return new(big.Rat).SetFrac(big.NewInt(value.Coefficient), den)
}

func protoValueState(value ValueState) tammyv1.BasAmountState {
	switch value {
	case ValueUnset:
		return tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET
	case ValueUnknown:
		return tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN
	case ValueNotApplicable:
		return tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE
	case ValueKnownZero:
		return tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	case ValueKnown:
		return tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	default:
		return tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN
	}
}

func rationalFromText(numerator, denominator string) (*big.Rat, bool) {
	num, ok := new(big.Int).SetString(numerator, 10)
	if !ok {
		return nil, false
	}
	den, ok := new(big.Int).SetString(denominator, 10)
	if !ok || den.Sign() <= 0 {
		return nil, false
	}
	return new(big.Rat).SetFrac(num, den), true
}

func (a *basCalculationAdapter) makeLabelTrace(label tammyv1.BasFormLabel, operation tammyv1.BasTraceOperation, rule string,
	value calculationTraceValue, operandLabels []tammyv1.BasFormLabel,
	values map[tammyv1.BasFormLabel]calculationTraceValue) (*tammyv1.BasLabelTrace, error) {
	trace := &tammyv1.BasLabelTrace{Label: label, Operation: operation, RuleId: rule, State: value.state}
	trace.Evidence = a.mergeEvidence(value.evidence)
	if value.exactRate != nil {
		numerator, denominator, err := rationalStrings(value.exactRate)
		if err != nil {
			return nil, err
		}
		trace.ExactRateNumerator = proto.String(numerator)
		trace.ExactRateDenominator = proto.String(denominator)
	} else if value.exactCents != nil {
		if value.exactCents.Sign() < 0 || value.rounding == nil || value.reported < 0 {
			return nil, ErrInvalidValue
		}
		numerator, denominator, err := rationalStrings(value.exactCents)
		if err != nil {
			return nil, err
		}
		roundNumerator, roundDenominator, err := rationalStrings(value.rounding)
		if err != nil {
			return nil, err
		}
		trace.ExactCentsNumerator = proto.String(numerator)
		trace.ExactCentsDenominator = proto.String(denominator)
		trace.ReportedWholeDollars = proto.Int64(value.reported)
		trace.RoundingDifferenceCentsNumerator = proto.String(roundNumerator)
		trace.RoundingDifferenceCentsDenominator = proto.String(roundDenominator)
	}
	for _, operandLabel := range operandLabels {
		operandValue, ok := values[operandLabel]
		if !ok || operandValue.state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE {
			continue
		}
		operand, ok := a.makeOperand(operandLabel, operandValue)
		if !ok {
			continue
		}
		trace.Operands = append(trace.Operands, operand)
	}
	return trace, nil
}

func (a *basCalculationAdapter) makeOperand(label tammyv1.BasFormLabel, value calculationTraceValue) (*tammyv1.BasTraceOperand, bool) {
	if !isKnownState(value.state) {
		return nil, false
	}
	operand := &tammyv1.BasTraceOperand{Label: label, State: value.state}
	if value.exactRate != nil {
		numerator, denominator, err := rationalStrings(value.exactRate)
		if err != nil {
			return nil, false
		}
		operand.ExactRateNumerator = proto.String(numerator)
		operand.ExactRateDenominator = proto.String(denominator)
		return operand, true
	}
	if value.exactCents == nil || value.exactCents.Sign() < 0 {
		return nil, false
	}
	numerator, denominator, err := rationalStrings(value.exactCents)
	if err != nil {
		return nil, false
	}
	operand.ExactCentsNumerator = numerator
	operand.ExactCentsDenominator = denominator
	operand.ReportedWholeDollars = value.reported
	return operand, true
}

func unknownLabelTrace(label tammyv1.BasFormLabel, operation tammyv1.BasTraceOperation, rule string, state tammyv1.BasAmountState, evidence []*tammyv1.SourceRef) *tammyv1.BasLabelTrace {
	if state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN || state == tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN
	}
	return &tammyv1.BasLabelTrace{Label: label, Operation: operation, RuleId: rule, State: state, Evidence: cloneSourceRefs(evidence)}
}

func (a *basCalculationAdapter) mergeEvidence(sets ...[]*tammyv1.SourceRef) []*tammyv1.SourceRef {
	merged := mergeSourceRefs(sets...)
	if len(merged) > 20 {
		a.blocker("CALCULATION_EVIDENCE_LIMIT", "The calculation evidence exceeds the supported trace bound.", merged[:20])
		return cloneSourceRefs(merged[:20])
	}
	return cloneSourceRefs(merged)
}

func mergeSourceRefs(sets ...[]*tammyv1.SourceRef) []*tammyv1.SourceRef {
	result := make([]*tammyv1.SourceRef, 0)
	seen := make(map[string]struct{})
	for _, refs := range sets {
		for _, ref := range refs {
			if ref == nil {
				continue
			}
			key := stableSourceKey(ref) + "#" + hex.EncodeToString(ref.GetContentHash())
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, ref)
		}
	}
	return result
}

func cloneSourceRefs(values []*tammyv1.SourceRef) []*tammyv1.SourceRef {
	out := make([]*tammyv1.SourceRef, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, proto.Clone(value).(*tammyv1.SourceRef))
		}
	}
	return out
}

func (a *basCalculationAdapter) blocker(code, message string, sources []*tammyv1.SourceRef) {
	a.blocked = true
	a.add(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER, code, message, sources)
}

func (a *basCalculationAdapter) unsupportedFinding(code, message string, sources []*tammyv1.SourceRef) {
	a.unsupported = true
	a.add(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED, code, message, sources)
}

func (a *basCalculationAdapter) add(severity tammyv1.BasFindingSeverity, code, message string, sources []*tammyv1.SourceRef) {
	if _, exists := a.codes[code]; exists {
		return
	}
	a.codes[code] = struct{}{}
	a.findings = append(a.findings, CalculationFindingTemplate{Severity: severity, StableCode: code, SafeMessage: message, Sources: cloneSourceRefs(sources)})
}

func (a *basCalculationAdapter) addCalculationError(err error, sources []*tammyv1.SourceRef) {
	switch {
	case errors.Is(err, ErrUnsupportedObligation), errors.Is(err, ErrUnsupportedVariation), errors.Is(err, ErrUncertainPriorVariation):
		a.unsupportedFinding("BAS_CALCULATION_UNSUPPORTED", "The selected BAS obligations or PAYG method requires unsupported handling.", sources)
	case errors.Is(err, ErrUnknownObligation), errors.Is(err, ErrPAYGIUnknown), errors.Is(err, ErrUnknownValue), errors.Is(err, ErrUnsetValue), errors.Is(err, ErrT1NotReviewed):
		a.blocker("BAS_CALCULATION_INPUT_INCOMPLETE", "One or more required BAS calculation inputs are unknown or unset.", sources)
	default:
		a.blocker("BAS_CALCULATION_INPUT_INVALID", "The BAS inputs could not be calculated within the supported exact-value rules.", sources)
	}
}

func paygiNoticeCanBeUsed(input *tammyv1.BasPaygiInputs) bool {
	return input != nil && input.GetNoticeOperativeStatus() == tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_OPERATIVE &&
		(input.GetPriorVariation() == tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NONE ||
			input.GetPriorVariation() == tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_REFLECTED_IN_CURRENT_NOTICE) &&
		input.GetUnsupportedState() == tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_NONE &&
		input.GetProposedVariationAmount() == nil && input.GetProposedVariationRate() == nil && input.GetClaim_5B() == nil && input.GetNotice() != nil
}

func orderedBASLabels() []tammyv1.BasFormLabel {
	return []tammyv1.BasFormLabel{
		tammyv1.BasFormLabel_BAS_FORM_LABEL_G1, tammyv1.BasFormLabel_BAS_FORM_LABEL_1A, tammyv1.BasFormLabel_BAS_FORM_LABEL_1B,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_W1, tammyv1.BasFormLabel_BAS_FORM_LABEL_W2, tammyv1.BasFormLabel_BAS_FORM_LABEL_W3,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_W4, tammyv1.BasFormLabel_BAS_FORM_LABEL_W5, tammyv1.BasFormLabel_BAS_FORM_LABEL_4,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_T1, tammyv1.BasFormLabel_BAS_FORM_LABEL_T2, tammyv1.BasFormLabel_BAS_FORM_LABEL_T7,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_T11, tammyv1.BasFormLabel_BAS_FORM_LABEL_5A, tammyv1.BasFormLabel_BAS_FORM_LABEL_5B,
		tammyv1.BasFormLabel_BAS_FORM_LABEL_8A, tammyv1.BasFormLabel_BAS_FORM_LABEL_8B, tammyv1.BasFormLabel_BAS_FORM_LABEL_9,
	}
}

func operationForLabel(label tammyv1.BasFormLabel) tammyv1.BasTraceOperation {
	switch label {
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_W5, tammyv1.BasFormLabel_BAS_FORM_LABEL_8A:
		return tammyv1.BasTraceOperation_BAS_TRACE_OPERATION_SUM
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_4, tammyv1.BasFormLabel_BAS_FORM_LABEL_5A, tammyv1.BasFormLabel_BAS_FORM_LABEL_8B:
		return tammyv1.BasTraceOperation_BAS_TRACE_OPERATION_COPY
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_T11:
		return tammyv1.BasTraceOperation_BAS_TRACE_OPERATION_MULTIPLY
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_9:
		return tammyv1.BasTraceOperation_BAS_TRACE_OPERATION_ABSOLUTE_DIFFERENCE
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_T2:
		return tammyv1.BasTraceOperation_BAS_TRACE_OPERATION_INPUT
	default:
		return tammyv1.BasTraceOperation_BAS_TRACE_OPERATION_TRUNCATE_TO_WHOLE_DOLLARS
	}
}

func ruleForLabel(label tammyv1.BasFormLabel) string {
	switch label {
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_G1:
		return "bas.gst.g1.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_1A:
		return "bas.gst.1a.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_1B:
		return "bas.gst.1b.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_W1:
		return "bas.paygw.w1.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_W2:
		return "bas.paygw.w2.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_W3:
		return "bas.paygw.w3.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_W4:
		return "bas.paygw.w4.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_W5:
		return "bas.paygw.w5.sum-reported-w2-w4.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_4:
		return "bas.paygw.label4.copy-w5.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_T1:
		return "bas.paygi.t1.truncate.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_T2:
		return "bas.paygi.t2.notified-rate-percent.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_T7:
		return "bas.paygi.t7.notified-amount.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_T11:
		return "bas.paygi.t11.reportable-t1-times-rate-percent.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_5A:
		return "bas.paygi.5a.copy-method-result.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_5B:
		return "bas.paygi.5b.unsupported-credit.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_8A:
		return "bas.form.8a.sum-reported-1a-4-5a.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_8B:
		return "bas.form.8b.copy-reported-1b.v1"
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_9:
		return "bas.form.9.absolute-reported-difference.v1"
	default:
		return "bas.label.unspecified.v1"
	}
}

func operandsForLabel(label tammyv1.BasFormLabel, values map[tammyv1.BasFormLabel]calculationTraceValue, method tammyv1.BasPaygiMethod) []tammyv1.BasFormLabel {
	switch label {
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_W5:
		return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_W2, tammyv1.BasFormLabel_BAS_FORM_LABEL_W3, tammyv1.BasFormLabel_BAS_FORM_LABEL_W4}
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_4:
		return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_W5}
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_T11:
		return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_T1, tammyv1.BasFormLabel_BAS_FORM_LABEL_T2}
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_5A:
		if method == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT {
			return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_T7}
		}
		if method == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE {
			return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_T11}
		}
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_8A:
		return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_1A, tammyv1.BasFormLabel_BAS_FORM_LABEL_4, tammyv1.BasFormLabel_BAS_FORM_LABEL_5A}
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_8B:
		return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_1B}
	case tammyv1.BasFormLabel_BAS_FORM_LABEL_9:
		return []tammyv1.BasFormLabel{tammyv1.BasFormLabel_BAS_FORM_LABEL_8A, tammyv1.BasFormLabel_BAS_FORM_LABEL_8B}
	}
	return nil
}

func protoDirection(value PaymentDirection) tammyv1.BasAmountDirection {
	switch value {
	case DirectionPayable:
		return tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_PAYABLE
	case DirectionRefundable:
		return tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_REFUNDABLE
	case DirectionNil:
		return tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_NIL
	default:
		return tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_UNSPECIFIED
	}
}

func rationalStrings(value *big.Rat) (string, string, error) {
	if value == nil {
		return "", "", ErrInvalidValue
	}
	num := value.Num().String()
	den := value.Denom().String()
	if len(num) > 80 || len(den) > 80 {
		return "", "", fmt.Errorf("rational exceeds proto bound")
	}
	return num, den, nil
}
