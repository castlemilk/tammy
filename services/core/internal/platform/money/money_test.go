package money_test

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/platform/money"
)

func TestCheckedInt64Arithmetic(t *testing.T) {
	tests := []struct {
		name    string
		apply   func(int64, int64) (int64, error)
		left    int64
		right   int64
		want    int64
		wantErr error
	}{
		{name: "add", apply: money.CheckedAdd, left: 7, right: -2, want: 5},
		{name: "add_positive_overflow", apply: money.CheckedAdd, left: math.MaxInt64, right: 1, wantErr: money.ErrOverflow},
		{name: "add_negative_overflow", apply: money.CheckedAdd, left: math.MinInt64, right: -1, wantErr: money.ErrOverflow},
		{name: "subtract", apply: money.CheckedSub, left: 7, right: -2, want: 9},
		{name: "subtract_positive_overflow", apply: money.CheckedSub, left: math.MaxInt64, right: -1, wantErr: money.ErrOverflow},
		{name: "subtract_negative_overflow", apply: money.CheckedSub, left: math.MinInt64, right: 1, wantErr: money.ErrOverflow},
		{name: "multiply", apply: money.CheckedMul, left: -7, right: 6, want: -42},
		{name: "multiply_positive_overflow", apply: money.CheckedMul, left: math.MaxInt64, right: 2, wantErr: money.ErrOverflow},
		{name: "multiply_minimum_by_negative_one", apply: money.CheckedMul, left: math.MinInt64, right: -1, wantErr: money.ErrOverflow},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.apply(test.left, test.right)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("result = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRoundRatioHalfAwayFromZero(t *testing.T) {
	tests := []struct {
		name        string
		numerator   int64
		denominator int64
		want        int64
	}{
		{name: "positive_below_half", numerator: 1, denominator: 3, want: 0},
		{name: "positive_tie", numerator: 1, denominator: 2, want: 1},
		{name: "negative_tie", numerator: -1, denominator: 2, want: -1},
		{name: "negative_denominator", numerator: 1, denominator: -2, want: -1},
		{name: "minimum_int64_exact", numerator: math.MinInt64, denominator: 1, want: math.MinInt64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := money.RoundRatioHalfAwayFromZero(test.numerator, test.denominator)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("result = %d, want %d", got, test.want)
			}
		})
	}

	if _, err := money.RoundRatioHalfAwayFromZero(1, 0); !errors.Is(err, money.ErrInvalidRate) {
		t.Fatalf("zero denominator error = %v, want %v", err, money.ErrInvalidRate)
	}
}

func TestGSTInclusiveUsesExactHalfAwayFromZero(t *testing.T) {
	rate := money.Rate{Coefficient: 4, Scale: 2}
	for _, test := range []struct {
		name  string
		gross int64
		want  int64
	}{
		{name: "positive_tie", gross: 13, want: 1},
		{name: "negative_tie", gross: -13, want: -1},
		{name: "ordinary_ten_percent", gross: 11000, want: 1000},
	} {
		t.Run(test.name, func(t *testing.T) {
			selectedRate := rate
			if test.name == "ordinary_ten_percent" {
				selectedRate = money.Rate{Coefficient: 10, Scale: 2}
			}
			got, err := money.GSTInclusive(test.gross, selectedRate)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("GST = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRateApplyRejectsInvalidScaleAndOverflow(t *testing.T) {
	if _, err := (money.Rate{Coefficient: 1, Scale: 19}).Apply(1); !errors.Is(err, money.ErrInvalidRate) {
		t.Fatalf("scale error = %v, want %v", err, money.ErrInvalidRate)
	}
	if _, err := (money.Rate{Coefficient: math.MaxInt64, Scale: 0}).Apply(math.MaxInt64); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("overflow error = %v, want %v", err, money.ErrOverflow)
	}
}

func TestLargestRemainderDistributionUsesStableLineIDTieBreak(t *testing.T) {
	lines := []money.LineWeight{
		{LineID: "line-b", Weight: 1},
		{LineID: "line-a", Weight: 1},
		{LineID: "line-c", Weight: 1},
	}
	wantPositive := []money.Allocation{
		{LineID: "line-a", MinorUnits: 1},
		{LineID: "line-b", MinorUnits: 1},
		{LineID: "line-c", MinorUnits: 0},
	}
	wantNegative := []money.Allocation{
		{LineID: "line-a", MinorUnits: -1},
		{LineID: "line-b", MinorUnits: -1},
		{LineID: "line-c", MinorUnits: 0},
	}

	for _, test := range []struct {
		name  string
		total int64
		want  []money.Allocation
	}{
		{name: "positive", total: 2, want: wantPositive},
		{name: "negative", total: -2, want: wantNegative},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := money.DistributeLargestRemainder(test.total, lines)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("allocation = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestLargestRemainderDistributionPreservesTotalAndRejectsInvalidLines(t *testing.T) {
	got, err := money.DistributeLargestRemainder(10, []money.LineWeight{
		{LineID: "a", Weight: 1},
		{LineID: "b", Weight: 2},
		{LineID: "c", Weight: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []money.Allocation{{LineID: "a", MinorUnits: 2}, {LineID: "b", MinorUnits: 3}, {LineID: "c", MinorUnits: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allocation = %#v, want %#v", got, want)
	}

	invalid := [][]money.LineWeight{
		{{LineID: "", Weight: 1}},
		{{LineID: "a", Weight: 0}},
		{{LineID: "a", Weight: 1}, {LineID: "a", Weight: 2}},
	}
	for _, lines := range invalid {
		if _, err := money.DistributeLargestRemainder(1, lines); !errors.Is(err, money.ErrInvalidAllocation) {
			t.Fatalf("invalid allocation error = %v, want %v", err, money.ErrInvalidAllocation)
		}
	}
}

func TestDebitCreditSigns(t *testing.T) {
	debit, err := money.Signed(money.Debit, 125)
	if err != nil || debit != 125 {
		t.Fatalf("debit = %d, %v", debit, err)
	}
	credit, err := money.Signed(money.Credit, 125)
	if err != nil || credit != -125 {
		t.Fatalf("credit = %d, %v", credit, err)
	}
	if _, err := money.Signed(money.Debit, -1); !errors.Is(err, money.ErrInvalidAmount) {
		t.Fatalf("negative magnitude error = %v, want %v", err, money.ErrInvalidAmount)
	}
	if _, err := money.Signed(money.Side(99), 1); !errors.Is(err, money.ErrInvalidSide) {
		t.Fatalf("invalid side error = %v, want %v", err, money.ErrInvalidSide)
	}
}

func TestMoneyRequiresCanonicalCurrency(t *testing.T) {
	got, err := money.New("AUD", -125)
	if err != nil {
		t.Fatal(err)
	}
	if got.Currency() != "AUD" || got.MinorUnits() != -125 {
		t.Fatalf("money = %#v", got)
	}
	for _, invalid := range []string{"", "aud", "AU", "AÜD", "AUDD"} {
		if _, err := money.New(invalid, 0); !errors.Is(err, money.ErrInvalidCurrency) {
			t.Fatalf("currency %q error = %v, want %v", invalid, err, money.ErrInvalidCurrency)
		}
	}
}

func TestMoneyRepresentationCannotBypassConstructorValidation(t *testing.T) {
	typeOfMoney := reflect.TypeOf(money.Money{})
	for index := range typeOfMoney.NumField() {
		field := typeOfMoney.Field(index)
		if field.IsExported() {
			t.Fatalf("Money exposes mutable representation field %q", field.Name)
		}
	}
}
