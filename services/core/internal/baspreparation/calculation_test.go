package baspreparation

import (
	"errors"
	"math"
	"testing"
)

func TestCalculateDerivesReportedLabelsFromAtomicWholeDollarValues(t *testing.T) {
	input := completeCalculationInput()
	input.GST = GSTInput{
		G1:   knownAmount(110_075),
		OneA: knownAmount(10_099),
		OneB: knownAmount(9_999),
	}
	input.PAYGW = PAYGWInput{
		W1: knownAmount(250_099),
		W2: knownAmount(10_075),
		W3: knownAmount(2_050),
		W4: knownAmount(3_099),
	}
	input.PAYGI = PAYGIInput{
		Method:         PAYGIMethodAmount,
		Operative:      PAYGIOperativeConfirmed,
		PriorVariation: PAYGIPriorVariationNone,
		Notice:         noticeReference(),
		T7:             knownAmount(530_000),
	}

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.PAYGW.W5.ReportedWholeDollars != 150 || got.PAYGW.Four.ReportedWholeDollars != 150 {
		t.Fatalf("W5/4 = %d/%d, want 150/150", got.PAYGW.W5.ReportedWholeDollars, got.PAYGW.Four.ReportedWholeDollars)
	}
	if got.PAYGW.W5.ExactCents != (ExactRational{Numerator: "15224", Denominator: "1"}) {
		t.Fatalf("exact W5 cents = %#v, want 15224/1", got.PAYGW.W5.ExactCents)
	}
	if got.GST.G1.ReportedWholeDollars != 1_100 || got.GST.OneA.ReportedWholeDollars != 100 || got.GST.OneB.ReportedWholeDollars != 99 {
		t.Fatalf("GST reported labels = %d/%d/%d, want 1100/100/99", got.GST.G1.ReportedWholeDollars, got.GST.OneA.ReportedWholeDollars, got.GST.OneB.ReportedWholeDollars)
	}
	if got.PAYGW.W2.ReportedWholeDollars != 100 || got.PAYGW.W3.ReportedWholeDollars != 20 || got.PAYGW.W4.ReportedWholeDollars != 30 {
		t.Fatalf("PAYGW atomic reported labels = %d/%d/%d, want 100/20/30", got.PAYGW.W2.ReportedWholeDollars, got.PAYGW.W3.ReportedWholeDollars, got.PAYGW.W4.ReportedWholeDollars)
	}
	if got.PAYGW.W5.OmittedCents != (ExactRational{Numerator: "224", Denominator: "1"}) {
		t.Fatalf("omitted W5 cents = %#v, want 224/1", got.PAYGW.W5.OmittedCents)
	}
	if got.PAYGI.FiveA.ReportedWholeDollars != 5_300 {
		t.Fatalf("5A = %d, want 5300", got.PAYGI.FiveA.ReportedWholeDollars)
	}
	if got.Form.EightA.ReportedWholeDollars != 5_550 || got.Form.EightB.ReportedWholeDollars != 99 {
		t.Fatalf("8A/8B = %d/%d, want 5550/99", got.Form.EightA.ReportedWholeDollars, got.Form.EightB.ReportedWholeDollars)
	}
	if got.Form.Nine.ReportedWholeDollars != 5_451 || got.Form.Direction != DirectionPayable {
		t.Fatalf("label 9 = %d %v, want 5451 PAYABLE", got.Form.Nine.ReportedWholeDollars, got.Form.Direction)
	}
	if got.Form.EightA.ExactCents != (ExactRational{Numerator: "555323", Denominator: "1"}) {
		t.Fatalf("exact 8A cents = %#v, want 555323/1", got.Form.EightA.ExactCents)
	}
	if got.Form.EightB.ExactCents != (ExactRational{Numerator: "9999", Denominator: "1"}) {
		t.Fatalf("exact 8B cents = %#v, want 9999/1", got.Form.EightB.ExactCents)
	}
	if got.RuleVersion == "" || got.CalculatorVersion == "" || got.FormVersion == "" {
		t.Fatalf("calculation versions must be pinned: %#v", got)
	}
}

func TestCalculateDoesNotCarryAtomicCentsIntoW5(t *testing.T) {
	input := completeCalculationInput()
	input.PAYGW = PAYGWInput{
		W1: knownZero(),
		W2: knownAmount(99),
		W3: knownAmount(99),
		W4: knownAmount(99),
	}

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.PAYGW.W2.ReportedWholeDollars != 0 || got.PAYGW.W3.ReportedWholeDollars != 0 || got.PAYGW.W4.ReportedWholeDollars != 0 {
		t.Fatalf("atomic reported labels = %d/%d/%d, want 0/0/0", got.PAYGW.W2.ReportedWholeDollars, got.PAYGW.W3.ReportedWholeDollars, got.PAYGW.W4.ReportedWholeDollars)
	}
	if got.PAYGW.W5.ReportedWholeDollars != 0 || got.PAYGW.Four.ReportedWholeDollars != 0 {
		t.Fatalf("derived W5/4 = %d/%d, want 0/0", got.PAYGW.W5.ReportedWholeDollars, got.PAYGW.Four.ReportedWholeDollars)
	}
	if got.PAYGW.W5.ExactCents != (ExactRational{Numerator: "297", Denominator: "1"}) {
		t.Fatalf("exact W5 cents = %#v, want 297/1", got.PAYGW.W5.ExactCents)
	}
}

func TestCalculatePreservesKnownZeroAndExplicitNotApplicableSections(t *testing.T) {
	input := completeCalculationInput()
	input.GST = GSTInput{G1: notApplicable(), OneA: notApplicable(), OneB: notApplicable()}
	input.PAYGW = PAYGWInput{W1: knownZero(), W2: knownZero(), W3: knownZero(), W4: knownZero()}
	input.PAYGI = PAYGIInput{Method: PAYGIMethodNotApplicable}

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.GST.G1.State != ValueNotApplicable || got.PAYGW.W2.State != ValueKnownZero || got.PAYGI.FiveA.State != ValueNotApplicable {
		t.Fatalf("states lost: GST G1=%v PAYGW W2=%v PAYGI 5A=%v", got.GST.G1.State, got.PAYGW.W2.State, got.PAYGI.FiveA.State)
	}
	if got.Form.Direction != DirectionNil || got.Form.Nine.ReportedWholeDollars != 0 {
		t.Fatalf("nil form result = %d %v, want 0 NIL", got.Form.Nine.ReportedWholeDollars, got.Form.Direction)
	}
}

func TestCalculateBlocksUnknownAndUnsetAtomicLabels(t *testing.T) {
	for _, test := range []struct {
		name  string
		value AmountInput
		want  error
	}{
		{name: "unknown", value: AmountInput{State: ValueUnknown}, want: ErrUnknownValue},
		{name: "unset", value: AmountInput{}, want: ErrUnsetValue},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := completeCalculationInput()
			input.GST.G1 = test.value
			if _, err := Calculate(input); !errors.Is(err, test.want) {
				t.Fatalf("Calculate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCalculateRejectsNegativeAndContradictoryKnownAmounts(t *testing.T) {
	for _, test := range []struct {
		name  string
		value AmountInput
	}{
		{name: "negative", value: AmountInput{State: ValueKnown, Cents: -1}},
		{name: "known_zero_with_amount", value: AmountInput{State: ValueKnownZero, Cents: 1}},
		{name: "known_without_amount", value: AmountInput{State: ValueKnown, Cents: 0}},
		{name: "not_applicable_with_amount", value: AmountInput{State: ValueNotApplicable, Cents: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := completeCalculationInput()
			input.GST.G1 = test.value
			if _, err := Calculate(input); !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("Calculate() error = %v, want %v", err, ErrInvalidValue)
			}
		})
	}
}

func TestCalculateRejectsPartialNotApplicableSections(t *testing.T) {
	input := completeCalculationInput()
	input.PAYGW.W4 = notApplicable()
	if _, err := Calculate(input); !errors.Is(err, ErrMixedApplicability) {
		t.Fatalf("Calculate() error = %v, want %v", err, ErrMixedApplicability)
	}
}

func TestCalculateSelectsRefundableAndNilDirectionsFromReportedLabels(t *testing.T) {
	for _, test := range []struct {
		name      string
		oneA      int64
		oneB      int64
		wantNine  int64
		direction PaymentDirection
	}{
		{name: "refundable", oneA: 100, oneB: 999, wantNine: 8, direction: DirectionRefundable},
		{name: "nil", oneA: 999, oneB: 999, wantNine: 0, direction: DirectionNil},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := completeCalculationInput()
			input.GST.OneA = knownAmount(test.oneA)
			input.GST.OneB = knownAmount(test.oneB)
			got, err := Calculate(input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Form.Nine.ReportedWholeDollars != test.wantNine || got.Form.Direction != test.direction {
				t.Fatalf("label 9 = %d %v, want %d %v", got.Form.Nine.ReportedWholeDollars, got.Form.Direction, test.wantNine, test.direction)
			}
		})
	}
}

func TestCalculatePreservesNegativeLabel9OmissionWhenExactAndReportedDifferencesDiverge(t *testing.T) {
	input := completeCalculationInput()
	input.GST.OneA = knownAmount(10_000)
	input.GST.OneB = knownAmount(9_999)

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Form.Nine.ReportedWholeDollars != 1 || got.Form.Direction != DirectionPayable {
		t.Fatalf("label 9 = %d %v, want 1 PAYABLE", got.Form.Nine.ReportedWholeDollars, got.Form.Direction)
	}
	if got.Form.Nine.ExactCents != (ExactRational{Numerator: "1", Denominator: "1"}) || got.Form.Nine.OmittedCents != (ExactRational{Numerator: "-99", Denominator: "1"}) {
		t.Fatalf("label 9 exact/omitted trace = %#v/%#v, want 1 cent/-99 cents", got.Form.Nine.ExactCents, got.Form.Nine.OmittedCents)
	}
}

func TestCalculateRejectsAtomicAndSummaryOverflow(t *testing.T) {
	t.Run("W5 exact cents", func(t *testing.T) {
		input := completeCalculationInput()
		input.PAYGW = PAYGWInput{
			W1: knownZero(),
			W2: knownAmount(math.MaxInt64),
			W3: knownAmount(1),
			W4: knownZero(),
		}
		if _, err := Calculate(input); !errors.Is(err, ErrOverflow) {
			t.Fatalf("Calculate() error = %v, want %v", err, ErrOverflow)
		}
	})

	t.Run("8A exact cents", func(t *testing.T) {
		input := completeCalculationInput()
		input.GST.OneA = knownAmount(math.MaxInt64)
		input.PAYGW = PAYGWInput{W1: knownZero(), W2: knownZero(), W3: knownZero(), W4: knownZero()}
		input.PAYGI = PAYGIInput{
			Method:         PAYGIMethodAmount,
			Operative:      PAYGIOperativeConfirmed,
			PriorVariation: PAYGIPriorVariationNone,
			Notice:         noticeReference(),
			T7:             knownAmount(1),
		}
		if _, err := Calculate(input); !errors.Is(err, ErrOverflow) {
			t.Fatalf("Calculate() error = %v, want %v", err, ErrOverflow)
		}
	})
}

func TestCalculateRequiresACompleteOtherObligationsReview(t *testing.T) {
	input := completeCalculationInput()
	input.OtherObligationsReviewed = false
	if _, err := Calculate(input); !errors.Is(err, ErrIncompleteObligationReview) {
		t.Fatalf("Calculate() error = %v, want %v", err, ErrIncompleteObligationReview)
	}
}

func TestCalculateBlocksUnknownOrActiveOtherObligations(t *testing.T) {
	for _, test := range []struct {
		name string
		item OtherObligation
		want error
	}{
		{name: "unknown", item: OtherObligation{Code: "FBT", Applicability: ApplicabilityUnknown}, want: ErrUnknownObligation},
		{name: "unset", item: OtherObligation{Code: "FBT", Applicability: ApplicabilityUnset}, want: ErrUnknownObligation},
		{name: "active", item: OtherObligation{Code: "FBT", Applicability: ApplicabilityActive}, want: ErrUnsupportedObligation},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := completeCalculationInput()
			input.OtherObligations = []OtherObligation{test.item}
			if _, err := Calculate(input); !errors.Is(err, test.want) {
				t.Fatalf("Calculate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseDecimalIsExactAndHasBoundedScale(t *testing.T) {
	got, err := ParseDecimal("1.7%")
	if err != nil {
		t.Fatal(err)
	}
	if got != (Decimal{Coefficient: 17, Scale: 1}) {
		t.Fatalf("decimal = %#v, want coefficient=17 scale=1", got)
	}
	boundary, err := ParseDecimal("0.123456789012345678")
	if err != nil || boundary.Scale != MaxDecimalScale {
		t.Fatalf("maximum supported decimal scale = %#v, %v; want scale %d", boundary, err, MaxDecimalScale)
	}
	if _, err := ParseDecimal("0.0000000000000000001"); !errors.Is(err, ErrInvalidDecimal) {
		t.Fatalf("over-scale decimal error = %v, want %v", err, ErrInvalidDecimal)
	}
	for _, invalid := range []string{"", "-1.7", "+1.7", "1e-3", "1.2.3", "1.%"} {
		if _, err := ParseDecimal(invalid); !errors.Is(err, ErrInvalidDecimal) {
			t.Errorf("ParseDecimal(%q) error = %v, want %v", invalid, err, ErrInvalidDecimal)
		}
	}
}

func TestPAYGIRateUsesReportedWholeDollarT1BeforeApplyingT2(t *testing.T) {
	rate, err := ParseDecimal("1.7%")
	if err != nil {
		t.Fatal(err)
	}
	input := completeCalculationInput()
	input.PAYGI = PAYGIInput{
		Method:         PAYGIMethodRate,
		Operative:      PAYGIOperativeConfirmed,
		PriorVariation: PAYGIPriorVariationNone,
		Notice:         noticeReference(),
		T1:             knownAmount(2_010_099),
		T1Reviewed:     true,
		Rate:           &rate,
	}

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.PAYGI.T1.ReportedWholeDollars != 20_100 || got.PAYGI.T1.OmittedCents != (ExactRational{Numerator: "99", Denominator: "1"}) {
		t.Fatalf("T1 exact/reportable trace = %#v, want 20100 dollars and 99 omitted cents", got.PAYGI.T1)
	}
	if got.PAYGI.T11ExactCents == nil || *got.PAYGI.T11ExactCents != (ExactRational{Numerator: "34170", Denominator: "1"}) {
		t.Fatalf("T11 exact cents = %#v, want 34170 based on reportable T1", got.PAYGI.T11ExactCents)
	}
}

func TestPAYGIAmountMethodCopiesPinnedNotifiedT7ToFiveA(t *testing.T) {
	input := completeCalculationInput()
	input.PAYGI = PAYGIInput{
		Method:         PAYGIMethodAmount,
		Operative:      PAYGIOperativeConfirmed,
		PriorVariation: PAYGIPriorVariationNone,
		Notice:         noticeReference(),
		T7: AmountInput{
			State:    ValueKnown,
			Cents:    530_099,
			Evidence: []EvidenceReference{{Reference: "paygi-t7", SHA256: validHash()}},
		},
	}

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.PAYGI.Method != PAYGIMethodAmount || got.PAYGI.FiveA.ReportedWholeDollars != 5_300 || got.PAYGI.T11ExactCents != nil {
		t.Fatalf("PAYGI method/5A = %v/%d, want amount/5300", got.PAYGI.Method, got.PAYGI.FiveA.ReportedWholeDollars)
	}
	if got.PAYGI.Notice != input.PAYGI.Notice || len(got.PAYGI.FiveA.Evidence) != 2 || got.PAYGI.FiveA.Evidence[0].Reference != "paygi-t7" || got.PAYGI.FiveA.Evidence[1].Reference != input.PAYGI.Notice.Reference {
		t.Fatalf("PAYGI evidence provenance was not retained: %#v", got.PAYGI)
	}
}

func TestPAYGIRateMethodUsesExactT1TimesT2AndTruncatesT11(t *testing.T) {
	rate, err := ParseDecimal("1.7%")
	if err != nil {
		t.Fatal(err)
	}
	input := completeCalculationInput()
	input.PAYGI = PAYGIInput{
		Method:         PAYGIMethodRate,
		Operative:      PAYGIOperativeConfirmed,
		PriorVariation: PAYGIPriorVariationNone,
		Notice:         noticeReference(),
		T1:             knownAmount(2_010_000),
		T1Reviewed:     true,
		Rate:           &rate,
	}

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.PAYGI.T1.ReportedWholeDollars != 20_100 || got.PAYGI.T11ExactCents == nil || *got.PAYGI.T11ExactCents != (ExactRational{Numerator: "34170", Denominator: "1"}) {
		t.Fatalf("T1/T11 exact trace = %d/%#v, want 20100/34170 cents", got.PAYGI.T1.ReportedWholeDollars, got.PAYGI.T11ExactCents)
	}
	if got.PAYGI.FiveA.ReportedWholeDollars != 341 || got.PAYGI.FiveA.OmittedCents != (ExactRational{Numerator: "70", Denominator: "1"}) {
		t.Fatalf("5A trace = %#v, want 341 dollars with 70 omitted cents", got.PAYGI.FiveA)
	}
}

func TestPAYGIRateTracePreservesSubcentRationalWithoutFloatingPoint(t *testing.T) {
	rate, err := ParseDecimal("0.01")
	if err != nil {
		t.Fatal(err)
	}
	input := completeCalculationInput()
	input.PAYGI = PAYGIInput{
		Method:         PAYGIMethodRate,
		Operative:      PAYGIOperativeConfirmed,
		PriorVariation: PAYGIPriorVariationNone,
		Notice:         noticeReference(),
		T1:             knownAmount(100),
		T1Reviewed:     true,
		Rate:           &rate,
	}

	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.PAYGI.T11ExactCents == nil || *got.PAYGI.T11ExactCents != (ExactRational{Numerator: "1", Denominator: "100"}) {
		t.Fatalf("T11 exact cents = %#v, want 1/100", got.PAYGI.T11ExactCents)
	}
	if got.PAYGI.FiveA.ReportedWholeDollars != 0 || got.PAYGI.FiveA.ExactCents != (ExactRational{Numerator: "1", Denominator: "100"}) {
		t.Fatalf("5A exact/reportable trace = %#v, want 1/100 cents and $0", got.PAYGI.FiveA)
	}
}

func TestPAYGIRejectsMissingNoticeUncertainOrNewVariationAndUnsupportedCredits(t *testing.T) {
	base := func() PAYGIInput {
		return PAYGIInput{
			Method:         PAYGIMethodAmount,
			Operative:      PAYGIOperativeConfirmed,
			PriorVariation: PAYGIPriorVariationNone,
			Notice:         noticeReference(),
			T7:             knownAmount(53_000),
		}
	}
	for _, test := range []struct {
		name string
		edit func(*PAYGIInput)
		want error
	}{
		{name: "missing notice", edit: func(value *PAYGIInput) { value.Notice = NoticeReference{} }, want: ErrMissingNotice},
		{name: "invalid notice hash", edit: func(value *PAYGIInput) { value.Notice.SHA256 = "not-a-sha256" }, want: ErrInvalidNotice},
		{name: "uncertain prior variation", edit: func(value *PAYGIInput) { value.PriorVariation = PAYGIPriorVariationUncertain }, want: ErrUncertainPriorVariation},
		{name: "new variation", edit: func(value *PAYGIInput) { value.PriorVariation = PAYGIPriorVariationNew }, want: ErrUnsupportedVariation},
		{name: "5B credit", edit: func(value *PAYGIInput) { value.Claims5BCredit = true }, want: ErrUnsupportedVariation},
		{name: "variation fields", edit: func(value *PAYGIInput) { value.RequiresVariationFields = true }, want: ErrUnsupportedVariation},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := completeCalculationInput()
			input.PAYGI = base()
			test.edit(&input.PAYGI)
			if _, err := Calculate(input); !errors.Is(err, test.want) {
				t.Fatalf("Calculate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPAYGIAcceptsOnlyConfirmedPriorVariationAlreadyReflectedInNotice(t *testing.T) {
	input := completeCalculationInput()
	input.PAYGI = PAYGIInput{
		Method:         PAYGIMethodAmount,
		Operative:      PAYGIOperativeConfirmed,
		PriorVariation: PAYGIPriorVariationReflectedInNotice,
		Notice:         noticeReference(),
		T7:             knownAmount(34_100),
	}
	if _, err := Calculate(input); err != nil {
		t.Fatalf("confirmed prior variation reflected in current notice should be supported: %v", err)
	}
}

func TestPAYGIRateRequiresReviewedExplicitT1AndConfirmedOperativeNotice(t *testing.T) {
	rate, err := ParseDecimal("1.7")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*PAYGIInput)
		want error
	}{
		{name: "unknown T1", edit: func(value *PAYGIInput) { value.T1 = AmountInput{State: ValueUnknown} }, want: ErrUnknownValue},
		{name: "unreviewed T1", edit: func(value *PAYGIInput) { value.T1Reviewed = false }, want: ErrT1NotReviewed},
		{name: "unknown operative status", edit: func(value *PAYGIInput) { value.Operative = PAYGIOperativeUnknown }, want: ErrPAYGIUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := completeCalculationInput()
			input.PAYGI = PAYGIInput{
				Method:         PAYGIMethodRate,
				Operative:      PAYGIOperativeConfirmed,
				PriorVariation: PAYGIPriorVariationNone,
				Notice:         noticeReference(),
				T1:             knownAmount(2_010_000),
				T1Reviewed:     true,
				Rate:           &rate,
			}
			test.edit(&input.PAYGI)
			if _, err := Calculate(input); !errors.Is(err, test.want) {
				t.Fatalf("Calculate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPAYGIUnknownMethodBlocksCalculation(t *testing.T) {
	input := completeCalculationInput()
	input.PAYGI = PAYGIInput{}
	if _, err := Calculate(input); !errors.Is(err, ErrPAYGIUnknown) {
		t.Fatalf("Calculate() error = %v, want %v", err, ErrPAYGIUnknown)
	}
}

func TestPAYGINotApplicableNeedsNoNoticeAndDoesNotBecomeKnownZero(t *testing.T) {
	input := completeCalculationInput()
	input.PAYGI = PAYGIInput{Method: PAYGIMethodNotApplicable}
	got, err := Calculate(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.PAYGI.FiveA.State != ValueNotApplicable {
		t.Fatalf("5A state = %v, want NOT_APPLICABLE", got.PAYGI.FiveA.State)
	}
}

func TestPAYGIRejectsPayloadOnUnusedMethodAmounts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*PAYGIInput)
		want error
	}{
		{
			name: "amount method unused T1 cents",
			edit: func(value *PAYGIInput) { value.T1.Cents = 1 },
			want: ErrInvalidPAYGI,
		},
		{
			name: "amount method unused T1 evidence",
			edit: func(value *PAYGIInput) {
				value.T1.Evidence = []EvidenceReference{{Reference: "hidden", SHA256: validHash()}}
			},
			want: ErrInvalidPAYGI,
		},
		{
			name: "rate method unused T7 cents",
			edit: func(value *PAYGIInput) { value.T7.Cents = 1 },
			want: ErrInvalidPAYGI,
		},
		{
			name: "rate method unused T7 evidence",
			edit: func(value *PAYGIInput) {
				value.T7.Evidence = []EvidenceReference{{Reference: "hidden", SHA256: validHash()}}
			},
			want: ErrInvalidPAYGI,
		},
		{
			name: "not applicable unused T1 cents",
			edit: func(value *PAYGIInput) { value.T1.Cents = 1 },
			want: ErrInvalidPAYGI,
		},
		{
			name: "not applicable unused T7 evidence",
			edit: func(value *PAYGIInput) {
				value.T7.Evidence = []EvidenceReference{{Reference: "hidden", SHA256: validHash()}}
			},
			want: ErrInvalidPAYGI,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := completeCalculationInput()
			switch test.name {
			case "amount method unused T1 cents", "amount method unused T1 evidence":
				input.PAYGI = PAYGIInput{
					Method:         PAYGIMethodAmount,
					Operative:      PAYGIOperativeConfirmed,
					PriorVariation: PAYGIPriorVariationNone,
					Notice:         noticeReference(),
					T7:             knownAmount(53_000),
				}
			case "rate method unused T7 cents", "rate method unused T7 evidence":
				rate, err := ParseDecimal("1.7%")
				if err != nil {
					t.Fatal(err)
				}
				input.PAYGI = PAYGIInput{
					Method:         PAYGIMethodRate,
					Operative:      PAYGIOperativeConfirmed,
					PriorVariation: PAYGIPriorVariationNone,
					Notice:         noticeReference(),
					T1:             knownAmount(201_000),
					T1Reviewed:     true,
					Rate:           &rate,
				}
			default:
				input.PAYGI = PAYGIInput{Method: PAYGIMethodNotApplicable}
			}
			test.edit(&input.PAYGI)
			if _, err := Calculate(input); !errors.Is(err, test.want) {
				t.Fatalf("Calculate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func completeCalculationInput() CalculationInput {
	return CalculationInput{
		GST: GSTInput{
			G1:   knownZero(),
			OneA: knownZero(),
			OneB: knownZero(),
		},
		PAYGW: PAYGWInput{
			W1: knownZero(),
			W2: knownZero(),
			W3: knownZero(),
			W4: knownZero(),
		},
		PAYGI:                    PAYGIInput{Method: PAYGIMethodNotApplicable},
		OtherObligationsReviewed: true,
	}
}

func knownAmount(cents int64) AmountInput {
	if cents == 0 {
		return knownZero()
	}
	return AmountInput{State: ValueKnown, Cents: cents}
}

func knownZero() AmountInput {
	return AmountInput{State: ValueKnownZero}
}

func notApplicable() AmountInput {
	return AmountInput{State: ValueNotApplicable}
}

func noticeReference() NoticeReference {
	return NoticeReference{Reference: "ato-notice-2026-10", SHA256: validHash()}
}

func validHash() string {
	return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
