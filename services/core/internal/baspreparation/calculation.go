// Package baspreparation contains deterministic BAS calculation primitives.
package baspreparation

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/platform/money"
)

const (
	// RuleVersion pins the ATO label instructions reviewed on 2026-10-10.
	RuleVersion = "ato-simpler-bas-2026-10-10"
	// CalculatorVersion identifies this integer/rational calculation behavior.
	CalculatorVersion = "tammy-bas-calculator-v1"
	// FormVersion identifies the supported activity statement label equations.
	FormVersion = "ato-activity-statement-simpler-bas-v1"

	// MaxDecimalScale is an implementation bound for exact decimal input, not an ATO rule.
	MaxDecimalScale uint8 = 18
)

var (
	ErrUnsetValue                 = errors.New("BAS amount is unset")
	ErrUnknownValue               = errors.New("BAS amount is unknown")
	ErrInvalidValue               = errors.New("BAS amount state or value is invalid")
	ErrMixedApplicability         = errors.New("BAS section mixes applicable and not-applicable labels")
	ErrOverflow                   = money.ErrOverflow
	ErrInvalidDecimal             = errors.New("invalid exact decimal")
	ErrIncompleteObligationReview = errors.New("other activity statement obligations were not reviewed")
	ErrInvalidObligation          = errors.New("other activity statement obligation is invalid")
	ErrUnknownObligation          = errors.New("other activity statement obligation applicability is unknown")
	ErrUnsupportedObligation      = errors.New("active activity statement obligation is unsupported")
	ErrPAYGIUnknown               = errors.New("PAYG instalment method or operative notice is unknown")
	ErrMissingNotice              = errors.New("PAYG instalment notice provenance is missing")
	ErrInvalidNotice              = errors.New("PAYG instalment notice provenance is invalid")
	ErrUncertainPriorVariation    = errors.New("prior PAYG instalment variation is uncertain")
	ErrUnsupportedVariation       = errors.New("PAYG instalment variation or credit is unsupported")
	ErrT1NotReviewed              = errors.New("PAYG instalment T1 has not been reviewed")
	ErrInvalidPAYGI               = errors.New("PAYG instalment inputs do not match the selected method")
)

// ValueState distinguishes a real zero from an unanswered or inactive label.
type ValueState uint8

const (
	ValueUnset ValueState = iota
	ValueUnknown
	ValueNotApplicable
	ValueKnownZero
	ValueKnown
)

// EvidenceReference identifies retained source evidence without embedding its contents.
type EvidenceReference struct {
	Reference string
	SHA256    string
}

// AmountInput is an exact, nonnegative AUD amount in cents and its review state.
type AmountInput struct {
	State    ValueState
	Cents    int64
	Evidence []EvidenceReference
}

// ExactRational represents an exact number of cents as a reduced numerator/denominator.
// The denominator is positive and both values use canonical base-10 notation.
type ExactRational struct {
	Numerator   string
	Denominator string
}

// AmountTrace retains the exact cent amount and what was omitted when reporting whole dollars.
type AmountTrace struct {
	State                ValueState
	ExactCents           ExactRational
	ReportedWholeDollars int64
	OmittedCents         ExactRational
	Evidence             []EvidenceReference
}

// GSTInput holds the exact aggregate input for each supported Simpler BAS GST label.
type GSTInput struct {
	G1   AmountInput
	OneA AmountInput
	OneB AmountInput
}

// PAYGWInput holds the exact aggregate input for each supported PAYG withholding label.
type PAYGWInput struct {
	W1 AmountInput
	W2 AmountInput
	W3 AmountInput
	W4 AmountInput
}

// Decimal is a nonnegative decimal coefficient over 10^Scale. For PAYGI it represents T2 percent.
type Decimal struct {
	Coefficient int64
	Scale       uint8
}

// ParseDecimal parses a base-10 decimal without floating point. A trailing percent sign is optional.
// Scale is bounded for implementation safety; the bound is not an ATO rate limit.
func ParseDecimal(value string) (Decimal, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "%") {
		value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	}
	if value == "" || strings.ContainsAny(value, "+-eE") || strings.Count(value, ".") > 1 {
		return Decimal{}, ErrInvalidDecimal
	}
	whole, fraction, hasDecimalPoint := strings.Cut(value, ".")
	if !allASCIIDigits(whole) {
		return Decimal{}, ErrInvalidDecimal
	}
	if hasDecimalPoint && !allASCIIDigits(fraction) {
		return Decimal{}, ErrInvalidDecimal
	}
	if len(fraction) > int(MaxDecimalScale) {
		return Decimal{}, ErrInvalidDecimal
	}
	coefficient, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil {
		return Decimal{}, ErrInvalidDecimal
	}
	return Decimal{Coefficient: coefficient, Scale: uint8(len(fraction))}, nil
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

// PAYGIMethod selects the explicit notified amount, notified rate, or inactive-section path.
type PAYGIMethod uint8

const (
	PAYGIMethodUnset PAYGIMethod = iota
	PAYGIMethodNotApplicable
	PAYGIMethodAmount
	PAYGIMethodRate
)

// PAYGIOperativeState records whether the pinned notice's current method was confirmed operative.
type PAYGIOperativeState uint8

const (
	PAYGIOperativeUnknown PAYGIOperativeState = iota
	PAYGIOperativeConfirmed
)

// PAYGIPriorVariationState distinguishes no variation, a reflected prior variation, and unsupported cases.
type PAYGIPriorVariationState uint8

const (
	PAYGIPriorVariationUnknown PAYGIPriorVariationState = iota
	PAYGIPriorVariationNone
	PAYGIPriorVariationReflectedInNotice
	PAYGIPriorVariationUncertain
	PAYGIPriorVariationNew
)

// NoticeReference pins the user-reviewed PAYGI notice used by the calculation.
type NoticeReference struct {
	Reference string
	SHA256    string
}

// PAYGIInput contains reviewed facts and provenance for one supported PAYGI method.
type PAYGIInput struct {
	Method                  PAYGIMethod
	Operative               PAYGIOperativeState
	PriorVariation          PAYGIPriorVariationState
	Notice                  NoticeReference
	T7                      AmountInput
	T1                      AmountInput
	T1Reviewed              bool
	Rate                    *Decimal
	RequiresVariationFields bool
	Claims5BCredit          bool
}

// ObligationApplicability is the explicit review state for an additional activity-statement section.
type ObligationApplicability uint8

const (
	ApplicabilityUnset ObligationApplicability = iota
	ApplicabilityUnknown
	ApplicabilityActive
	ApplicabilityNotApplicable
)

// OtherObligation is any activity-statement section outside the implemented GST/PAYG equations.
type OtherObligation struct {
	Code          string
	Applicability ObligationApplicability
}

// CalculationInput is one complete supported-profile input to the pure label calculator.
type CalculationInput struct {
	GST                      GSTInput
	PAYGW                    PAYGWInput
	PAYGI                    PAYGIInput
	OtherObligations         []OtherObligation
	OtherObligationsReviewed bool
}

// GSTCalculation retains exact and reportable G1, 1A, and 1B values.
type GSTCalculation struct {
	G1   AmountTrace
	OneA AmountTrace
	OneB AmountTrace
}

// PAYGWCalculation retains W1-W4 and the form-derived W5/4 amounts.
type PAYGWCalculation struct {
	W1   AmountTrace
	W2   AmountTrace
	W3   AmountTrace
	W4   AmountTrace
	W5   AmountTrace
	Four AmountTrace
}

// PAYGICalculation retains the selected method, notice provenance, and T1/T7/T11/5A traces.
type PAYGICalculation struct {
	Method         PAYGIMethod
	Operative      PAYGIOperativeState
	PriorVariation PAYGIPriorVariationState
	Notice         NoticeReference
	T7             AmountTrace
	T1             AmountTrace
	Rate           *Decimal
	T11ExactCents  *ExactRational
	FiveA          AmountTrace
}

// PaymentDirection identifies the label 9 direction without a negative reportable amount.
type PaymentDirection uint8

const (
	DirectionUnset PaymentDirection = iota
	DirectionPayable
	DirectionRefundable
	DirectionNil
)

// FormCalculation retains the reported summary labels and label 9 direction.
type FormCalculation struct {
	EightA    AmountTrace
	EightB    AmountTrace
	Nine      AmountTrace
	Direction PaymentDirection
}

// CalculationResult is an independent calculation result, including pinned rule versions.
type CalculationResult struct {
	RuleVersion       string
	CalculatorVersion string
	FormVersion       string
	GST               GSTCalculation
	PAYGW             PAYGWCalculation
	PAYGI             PAYGICalculation
	Form              FormCalculation
	OtherObligations  []OtherObligation
}

type namedAmount struct {
	name  string
	input AmountInput
}

// Calculate builds the supported Simpler BAS GST and small/medium PAYG label calculations.
// Unknown inputs and every active unsupported obligation fail closed.
func Calculate(input CalculationInput) (CalculationResult, error) {
	result := CalculationResult{
		RuleVersion:       RuleVersion,
		CalculatorVersion: CalculatorVersion,
		FormVersion:       FormVersion,
		OtherObligations:  append([]OtherObligation(nil), input.OtherObligations...),
	}
	if !input.OtherObligationsReviewed {
		return CalculationResult{}, ErrIncompleteObligationReview
	}
	if err := validateOtherObligations(input.OtherObligations); err != nil {
		return CalculationResult{}, err
	}

	gstInputs := []namedAmount{
		{name: "G1", input: input.GST.G1},
		{name: "1A", input: input.GST.OneA},
		{name: "1B", input: input.GST.OneB},
	}
	gstTraces, gstNotApplicable, err := resolveSection(gstInputs)
	if err != nil {
		return CalculationResult{}, err
	}
	result.GST = GSTCalculation{G1: gstTraces[0], OneA: gstTraces[1], OneB: gstTraces[2]}

	paygwInputs := []namedAmount{
		{name: "W1", input: input.PAYGW.W1},
		{name: "W2", input: input.PAYGW.W2},
		{name: "W3", input: input.PAYGW.W3},
		{name: "W4", input: input.PAYGW.W4},
	}
	paygwTraces, paygwNotApplicable, err := resolveSection(paygwInputs)
	if err != nil {
		return CalculationResult{}, err
	}
	result.PAYGW = PAYGWCalculation{
		W1: paygwTraces[0],
		W2: paygwTraces[1],
		W3: paygwTraces[2],
		W4: paygwTraces[3],
	}

	result.PAYGI, err = calculatePAYGI(input.PAYGI)
	if err != nil {
		return CalculationResult{}, err
	}

	w5Reported, err := checkedSum(
		result.PAYGW.W2.ReportedWholeDollars,
		result.PAYGW.W3.ReportedWholeDollars,
		result.PAYGW.W4.ReportedWholeDollars,
	)
	if err != nil {
		return CalculationResult{}, err
	}
	w5Exact, err := sumTraceExact(result.PAYGW.W2, result.PAYGW.W3, result.PAYGW.W4)
	if err != nil {
		return CalculationResult{}, err
	}
	w5State := stateForExact(w5Exact)
	if paygwNotApplicable {
		w5State = ValueNotApplicable
	}
	result.PAYGW.W5, err = amountTrace(w5State, w5Exact, w5Reported, joinEvidence(result.PAYGW.W2, result.PAYGW.W3, result.PAYGW.W4))
	if err != nil {
		return CalculationResult{}, err
	}
	result.PAYGW.Four = cloneAmountTrace(result.PAYGW.W5)

	eightAReported, err := checkedSum(
		result.GST.OneA.ReportedWholeDollars,
		result.PAYGW.Four.ReportedWholeDollars,
		result.PAYGI.FiveA.ReportedWholeDollars,
	)
	if err != nil {
		return CalculationResult{}, err
	}
	eightAExact, err := sumTraceExact(result.GST.OneA, result.PAYGW.Four, result.PAYGI.FiveA)
	if err != nil {
		return CalculationResult{}, err
	}
	eightAState := stateForExact(eightAExact)
	if gstNotApplicable && paygwNotApplicable && result.PAYGI.FiveA.State == ValueNotApplicable {
		eightAState = ValueKnownZero
	}
	result.Form.EightA, err = amountTrace(eightAState, eightAExact, eightAReported, joinEvidence(result.GST.OneA, result.PAYGW.Four, result.PAYGI.FiveA))
	if err != nil {
		return CalculationResult{}, err
	}

	eightBExact, err := traceExact(result.GST.OneB)
	if err != nil {
		return CalculationResult{}, err
	}
	eightBState := stateForExact(eightBExact)
	if gstNotApplicable {
		eightBState = ValueKnownZero
	}
	result.Form.EightB, err = amountTrace(eightBState, eightBExact, result.GST.OneB.ReportedWholeDollars, cloneEvidence(result.GST.OneB.Evidence))
	if err != nil {
		return CalculationResult{}, err
	}

	nineReported, direction, err := difference(result.Form.EightA.ReportedWholeDollars, result.Form.EightB.ReportedWholeDollars)
	if err != nil {
		return CalculationResult{}, err
	}
	nineExact, err := subtractTraceExact(result.Form.EightA, result.Form.EightB)
	if err != nil {
		return CalculationResult{}, err
	}
	if nineExact.Sign() < 0 {
		nineExact.Neg(nineExact)
	}
	result.Form.Nine, err = amountTrace(stateForExact(nineExact), nineExact, nineReported, joinEvidence(result.Form.EightA, result.Form.EightB))
	if err != nil {
		return CalculationResult{}, err
	}
	result.Form.Direction = direction
	return result, nil
}

func validateOtherObligations(obligations []OtherObligation) error {
	seen := make(map[string]struct{}, len(obligations))
	for _, obligation := range obligations {
		code := strings.TrimSpace(obligation.Code)
		if code == "" {
			return ErrInvalidObligation
		}
		if _, exists := seen[code]; exists {
			return ErrInvalidObligation
		}
		seen[code] = struct{}{}
		switch obligation.Applicability {
		case ApplicabilityNotApplicable:
		case ApplicabilityUnknown, ApplicabilityUnset:
			return fmt.Errorf("%s: %w", code, ErrUnknownObligation)
		case ApplicabilityActive:
			return fmt.Errorf("%s: %w", code, ErrUnsupportedObligation)
		default:
			return ErrInvalidObligation
		}
	}
	return nil
}

func resolveSection(inputs []namedAmount) ([]AmountTrace, bool, error) {
	traces := make([]AmountTrace, len(inputs))
	notApplicableCount := 0
	for index, item := range inputs {
		trace, err := amountInputTrace(item.name, item.input)
		if err != nil {
			return nil, false, err
		}
		traces[index] = trace
		if trace.State == ValueNotApplicable {
			notApplicableCount++
		}
	}
	if notApplicableCount != 0 && notApplicableCount != len(inputs) {
		return nil, false, ErrMixedApplicability
	}
	return traces, notApplicableCount == len(inputs), nil
}

func amountInputTrace(name string, input AmountInput) (AmountTrace, error) {
	state := input.State
	switch state {
	case ValueUnset:
		return AmountTrace{}, fmt.Errorf("%s: %w", name, ErrUnsetValue)
	case ValueUnknown:
		return AmountTrace{}, fmt.Errorf("%s: %w", name, ErrUnknownValue)
	case ValueNotApplicable:
		if input.Cents != 0 {
			return AmountTrace{}, fmt.Errorf("%s: %w", name, ErrInvalidValue)
		}
		return amountTrace(ValueNotApplicable, new(big.Rat), 0, cloneEvidence(input.Evidence))
	case ValueKnownZero:
		if input.Cents != 0 {
			return AmountTrace{}, fmt.Errorf("%s: %w", name, ErrInvalidValue)
		}
	case ValueKnown:
		if input.Cents <= 0 {
			return AmountTrace{}, fmt.Errorf("%s: %w", name, ErrInvalidValue)
		}
	default:
		return AmountTrace{}, fmt.Errorf("%s: %w", name, ErrInvalidValue)
	}
	exact := new(big.Rat).SetInt64(input.Cents)
	return amountTrace(state, exact, input.Cents/100, cloneEvidence(input.Evidence))
}

func calculatePAYGI(input PAYGIInput) (PAYGICalculation, error) {
	result := PAYGICalculation{
		Method:         input.Method,
		Operative:      input.Operative,
		PriorVariation: input.PriorVariation,
		Notice:         input.Notice,
	}
	switch input.Method {
	case PAYGIMethodNotApplicable:
		if input.Operative != PAYGIOperativeUnknown || input.PriorVariation != PAYGIPriorVariationUnknown || input.Notice != (NoticeReference{}) || !emptyAmountInput(input.T7) || !emptyAmountInput(input.T1) || input.T1Reviewed || input.Rate != nil || input.RequiresVariationFields || input.Claims5BCredit {
			return PAYGICalculation{}, ErrInvalidPAYGI
		}
		result.T7 = emptyAmountTrace(ValueUnset)
		result.T1 = emptyAmountTrace(ValueUnset)
		result.FiveA = emptyAmountTrace(ValueNotApplicable)
		return result, nil
	case PAYGIMethodAmount, PAYGIMethodRate:
	default:
		return PAYGICalculation{}, ErrPAYGIUnknown
	}

	if input.Claims5BCredit || input.RequiresVariationFields || input.PriorVariation == PAYGIPriorVariationNew {
		return PAYGICalculation{}, ErrUnsupportedVariation
	}
	if input.PriorVariation == PAYGIPriorVariationUncertain {
		return PAYGICalculation{}, ErrUncertainPriorVariation
	}
	if input.Operative != PAYGIOperativeConfirmed || (input.PriorVariation != PAYGIPriorVariationNone && input.PriorVariation != PAYGIPriorVariationReflectedInNotice) {
		return PAYGICalculation{}, ErrPAYGIUnknown
	}
	if err := validateNotice(input.Notice); err != nil {
		return PAYGICalculation{}, err
	}

	noticeEvidence := []EvidenceReference{{Reference: input.Notice.Reference, SHA256: input.Notice.SHA256}}
	switch input.Method {
	case PAYGIMethodAmount:
		if !emptyAmountInput(input.T1) || input.T1Reviewed || input.Rate != nil {
			return PAYGICalculation{}, ErrInvalidPAYGI
		}
		t7, err := amountInputTrace("T7", input.T7)
		if err != nil {
			return PAYGICalculation{}, err
		}
		if t7.State == ValueNotApplicable {
			return PAYGICalculation{}, ErrInvalidPAYGI
		}
		result.T7 = t7
		result.T1 = emptyAmountTrace(ValueUnset)
		result.FiveA = cloneAmountTrace(t7)
		result.FiveA.Evidence = append(result.FiveA.Evidence, noticeEvidence...)
		return result, nil
	case PAYGIMethodRate:
		if !emptyAmountInput(input.T7) {
			return PAYGICalculation{}, ErrInvalidPAYGI
		}
		if input.Rate == nil {
			return PAYGICalculation{}, ErrPAYGIUnknown
		}
		if input.T1Reviewed == false {
			if input.T1.State == ValueUnknown || input.T1.State == ValueUnset {
				return PAYGICalculation{}, fmt.Errorf("T1: %w", stateError(input.T1.State))
			}
			return PAYGICalculation{}, ErrT1NotReviewed
		}
		t1, err := amountInputTrace("T1", input.T1)
		if err != nil {
			return PAYGICalculation{}, err
		}
		if t1.State == ValueNotApplicable {
			return PAYGICalculation{}, ErrInvalidPAYGI
		}
		if input.Rate.Coefficient < 0 || input.Rate.Scale > MaxDecimalScale {
			return PAYGICalculation{}, ErrInvalidDecimal
		}
		rateCopy := *input.Rate
		result.Rate = &rateCopy
		result.T1 = t1
		result.T7 = emptyAmountTrace(ValueUnset)

		reportableT1Dollars := big.NewInt(t1.ReportedWholeDollars)
		numerator := new(big.Int).Mul(reportableT1Dollars, big.NewInt(input.Rate.Coefficient))
		denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(input.Rate.Scale)), nil)
		t11ExactCents := new(big.Rat).SetFrac(numerator, denominator)
		if err := checkExactCentsBound(t11ExactCents); err != nil {
			return PAYGICalculation{}, err
		}
		t11WholeDollarDenominator := new(big.Int).Mul(new(big.Int).Set(denominator), big.NewInt(100))
		t11WholeDollars := new(big.Int).Quo(new(big.Int).Set(numerator), t11WholeDollarDenominator)
		if !t11WholeDollars.IsInt64() {
			return PAYGICalculation{}, ErrOverflow
		}
		t11ExactTrace := rationalValue(t11ExactCents)
		result.T11ExactCents = &t11ExactTrace
		result.FiveA, err = amountTrace(
			stateForExact(t11ExactCents),
			t11ExactCents,
			t11WholeDollars.Int64(),
			append(cloneEvidence(t1.Evidence), noticeEvidence...),
		)
		if err != nil {
			return PAYGICalculation{}, err
		}
		return result, nil
	default:
		return PAYGICalculation{}, ErrPAYGIUnknown
	}
}

func emptyAmountInput(input AmountInput) bool {
	return input.State == ValueUnset && input.Cents == 0 && len(input.Evidence) == 0
}

func stateError(state ValueState) error {
	switch state {
	case ValueUnknown:
		return ErrUnknownValue
	case ValueUnset:
		return ErrUnsetValue
	default:
		return ErrInvalidValue
	}
}

func validateNotice(notice NoticeReference) error {
	if strings.TrimSpace(notice.Reference) == "" || notice.SHA256 == "" {
		return ErrMissingNotice
	}
	if len(notice.SHA256) != 64 {
		return ErrInvalidNotice
	}
	decoded, err := hex.DecodeString(notice.SHA256)
	if err != nil || len(decoded) != 32 {
		return ErrInvalidNotice
	}
	return nil
}

func emptyAmountTrace(state ValueState) AmountTrace {
	return AmountTrace{
		State:        state,
		ExactCents:   ExactRational{Numerator: "0", Denominator: "1"},
		OmittedCents: ExactRational{Numerator: "0", Denominator: "1"},
	}
}

func amountTrace(state ValueState, exact *big.Rat, reportedWholeDollars int64, evidence []EvidenceReference) (AmountTrace, error) {
	if exact == nil || exact.Sign() < 0 || reportedWholeDollars < 0 {
		return AmountTrace{}, ErrInvalidValue
	}
	if err := checkExactCentsBound(exact); err != nil {
		return AmountTrace{}, err
	}
	reportedCents := new(big.Rat).SetInt(new(big.Int).Mul(big.NewInt(reportedWholeDollars), big.NewInt(100)))
	omitted := new(big.Rat).Sub(new(big.Rat).Set(exact), reportedCents)
	if state == ValueUnset {
		state = stateForExact(exact)
	}
	return AmountTrace{
		State:                state,
		ExactCents:           rationalValue(exact),
		ReportedWholeDollars: reportedWholeDollars,
		OmittedCents:         rationalValue(omitted),
		Evidence:             cloneEvidence(evidence),
	}, nil
}

func checkExactCentsBound(exact *big.Rat) error {
	if exact == nil || exact.Sign() < 0 {
		return ErrInvalidValue
	}
	maximum := new(big.Int).Mul(big.NewInt(math.MaxInt64), exact.Denom())
	if exact.Num().Cmp(maximum) > 0 {
		return ErrOverflow
	}
	return nil
}

func rationalValue(value *big.Rat) ExactRational {
	if value == nil {
		return ExactRational{Numerator: "0", Denominator: "1"}
	}
	return ExactRational{Numerator: value.Num().String(), Denominator: value.Denom().String()}
}

func traceExact(value AmountTrace) (*big.Rat, error) {
	numerator, ok := new(big.Int).SetString(value.ExactCents.Numerator, 10)
	if !ok {
		return nil, ErrInvalidValue
	}
	denominator, ok := new(big.Int).SetString(value.ExactCents.Denominator, 10)
	if !ok || denominator.Sign() <= 0 {
		return nil, ErrInvalidValue
	}
	return new(big.Rat).SetFrac(numerator, denominator), nil
}

func sumTraceExact(values ...AmountTrace) (*big.Rat, error) {
	total := new(big.Rat)
	for _, value := range values {
		exact, err := traceExact(value)
		if err != nil {
			return nil, err
		}
		total.Add(total, exact)
	}
	if err := checkExactCentsBound(total); err != nil {
		return nil, err
	}
	return total, nil
}

func subtractTraceExact(left, right AmountTrace) (*big.Rat, error) {
	leftExact, err := traceExact(left)
	if err != nil {
		return nil, err
	}
	rightExact, err := traceExact(right)
	if err != nil {
		return nil, err
	}
	result := new(big.Rat).Sub(leftExact, rightExact)
	if result.Sign() < 0 {
		result.Neg(result)
	}
	if err := checkExactCentsBound(result); err != nil {
		return nil, err
	}
	return result, nil
}

func stateForExact(exact *big.Rat) ValueState {
	if exact == nil || exact.Sign() == 0 {
		return ValueKnownZero
	}
	return ValueKnown
}

func checkedSum(values ...int64) (int64, error) {
	total := int64(0)
	for _, value := range values {
		updated, err := money.CheckedAdd(total, value)
		if err != nil {
			return 0, err
		}
		total = updated
	}
	return total, nil
}

func difference(eightA, eightB int64) (int64, PaymentDirection, error) {
	switch {
	case eightA > eightB:
		magnitude, err := money.CheckedSub(eightA, eightB)
		return magnitude, DirectionPayable, err
	case eightB > eightA:
		magnitude, err := money.CheckedSub(eightB, eightA)
		return magnitude, DirectionRefundable, err
	default:
		return 0, DirectionNil, nil
	}
}

func joinEvidence(values ...AmountTrace) []EvidenceReference {
	var evidence []EvidenceReference
	for _, value := range values {
		evidence = append(evidence, value.Evidence...)
	}
	return evidence
}

func cloneEvidence(values []EvidenceReference) []EvidenceReference {
	return append([]EvidenceReference(nil), values...)
}

func cloneAmountTrace(value AmountTrace) AmountTrace {
	value.Evidence = cloneEvidence(value.Evidence)
	return value
}
