// Package money provides deterministic integer-only accounting arithmetic.
package money

import (
	"errors"
	"math"
	"math/big"
	"sort"
)

var (
	ErrInvalidAllocation = errors.New("invalid allocation")
	ErrInvalidAmount     = errors.New("invalid amount")
	ErrInvalidCurrency   = errors.New("invalid currency")
	ErrInvalidRate       = errors.New("invalid rate")
	ErrInvalidSide       = errors.New("invalid side")
	ErrOverflow          = errors.New("integer overflow")
)

// Money is a currency-qualified signed amount in minor units.
type Money struct {
	currency   string
	minorUnits int64
}

// New creates a signed money value with a canonical ISO-style currency code.
func New(currency string, minorUnits int64) (Money, error) {
	if len(currency) != 3 {
		return Money{}, ErrInvalidCurrency
	}
	for index := range len(currency) {
		if currency[index] < 'A' || currency[index] > 'Z' {
			return Money{}, ErrInvalidCurrency
		}
	}
	return Money{currency: currency, minorUnits: minorUnits}, nil
}

// Currency returns the canonical currency code.
func (money Money) Currency() string {
	return money.currency
}

// MinorUnits returns the signed amount in minor units.
func (money Money) MinorUnits() int64 {
	return money.minorUnits
}

// CheckedAdd adds two signed integers and rejects overflow.
func CheckedAdd(left, right int64) (int64, error) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, ErrOverflow
	}
	return left + right, nil
}

// CheckedSub subtracts two signed integers and rejects overflow.
func CheckedSub(left, right int64) (int64, error) {
	if (right < 0 && left > math.MaxInt64+right) || (right > 0 && left < math.MinInt64+right) {
		return 0, ErrOverflow
	}
	return left - right, nil
}

// CheckedMul multiplies two signed integers and rejects overflow.
func CheckedMul(left, right int64) (int64, error) {
	if left == 0 || right == 0 {
		return 0, nil
	}
	if (left == math.MinInt64 && right == -1) || (right == math.MinInt64 && left == -1) {
		return 0, ErrOverflow
	}
	product := left * right
	if product/right != left {
		return 0, ErrOverflow
	}
	return product, nil
}

// Rate is a signed decimal coefficient with an explicit base-10 scale.
type Rate struct {
	Coefficient int64
	Scale       uint32
}

// Apply multiplies an amount by a decimal rate and rounds exact halves away from zero.
func (rate Rate) Apply(amount int64) (int64, error) {
	base, err := decimalBase(rate.Scale)
	if err != nil {
		return 0, err
	}
	numerator := new(big.Int).Mul(big.NewInt(amount), big.NewInt(rate.Coefficient))
	return roundBigRatioHalfAwayFromZero(numerator, base)
}

// GSTInclusive returns the tax component included in gross at the supplied rate.
func GSTInclusive(gross int64, rate Rate) (int64, error) {
	if rate.Coefficient < 0 {
		return 0, ErrInvalidRate
	}
	base, err := decimalBase(rate.Scale)
	if err != nil {
		return 0, err
	}
	denominator := new(big.Int).Add(new(big.Int).Set(base), big.NewInt(rate.Coefficient))
	if denominator.Sign() <= 0 {
		return 0, ErrInvalidRate
	}
	numerator := new(big.Int).Mul(big.NewInt(gross), big.NewInt(rate.Coefficient))
	return roundBigRatioHalfAwayFromZero(numerator, denominator)
}

// RoundRatioHalfAwayFromZero rounds an exact signed ratio without floating point.
func RoundRatioHalfAwayFromZero(numerator, denominator int64) (int64, error) {
	if denominator == 0 {
		return 0, ErrInvalidRate
	}
	return roundBigRatioHalfAwayFromZero(big.NewInt(numerator), big.NewInt(denominator))
}

func decimalBase(scale uint32) (*big.Int, error) {
	if scale > 18 {
		return nil, ErrInvalidRate
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil), nil
}

func roundBigRatioHalfAwayFromZero(numerator, denominator *big.Int) (int64, error) {
	if denominator.Sign() == 0 {
		return 0, ErrInvalidRate
	}
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	twiceRemainder := new(big.Int).Lsh(new(big.Int).Abs(remainder), 1)
	absDenominator := new(big.Int).Abs(denominator)
	if twiceRemainder.Cmp(absDenominator) >= 0 {
		direction := int64(numerator.Sign() * denominator.Sign())
		quotient.Add(quotient, big.NewInt(direction))
	}
	if !quotient.IsInt64() {
		return 0, ErrOverflow
	}
	return quotient.Int64(), nil
}

// LineWeight gives one positive proportional allocation weight a stable line identity.
type LineWeight struct {
	LineID string
	Weight int64
}

// Allocation is the finalized signed minor-unit allocation for a line.
type Allocation struct {
	LineID     string
	MinorUnits int64
}

type allocationRemainder struct {
	index     int
	remainder *big.Int
}

// DistributeLargestRemainder allocates total exactly, with ties resolved by line ID.
func DistributeLargestRemainder(total int64, lines []LineWeight) ([]Allocation, error) {
	if len(lines) == 0 {
		if total == 0 {
			return []Allocation{}, nil
		}
		return nil, ErrInvalidAllocation
	}
	sortedLines := append([]LineWeight(nil), lines...)
	sort.Slice(sortedLines, func(left, right int) bool {
		return sortedLines[left].LineID < sortedLines[right].LineID
	})
	sumWeight := new(big.Int)
	for index, line := range sortedLines {
		if line.LineID == "" || line.Weight <= 0 || (index > 0 && line.LineID == sortedLines[index-1].LineID) {
			return nil, ErrInvalidAllocation
		}
		sumWeight.Add(sumWeight, big.NewInt(line.Weight))
	}

	absTotal := new(big.Int).Abs(big.NewInt(total))
	shares := make([]*big.Int, len(sortedLines))
	remainders := make([]allocationRemainder, len(sortedLines))
	allocated := new(big.Int)
	for index, line := range sortedLines {
		numerator := new(big.Int).Mul(absTotal, big.NewInt(line.Weight))
		share := new(big.Int)
		remainder := new(big.Int)
		share.QuoRem(numerator, sumWeight, remainder)
		shares[index] = share
		remainders[index] = allocationRemainder{index: index, remainder: remainder}
		allocated.Add(allocated, share)
	}
	leftover := new(big.Int).Sub(absTotal, allocated)
	if !leftover.IsInt64() || leftover.Int64() > int64(len(sortedLines)) {
		return nil, ErrOverflow
	}
	sort.SliceStable(remainders, func(left, right int) bool {
		comparison := remainders[left].remainder.Cmp(remainders[right].remainder)
		if comparison != 0 {
			return comparison > 0
		}
		return sortedLines[remainders[left].index].LineID < sortedLines[remainders[right].index].LineID
	})
	for index := int64(0); index < leftover.Int64(); index++ {
		shares[remainders[index].index].Add(shares[remainders[index].index], big.NewInt(1))
	}

	allocations := make([]Allocation, len(sortedLines))
	for index, line := range sortedLines {
		if total < 0 {
			shares[index].Neg(shares[index])
		}
		if !shares[index].IsInt64() {
			return nil, ErrOverflow
		}
		allocations[index] = Allocation{LineID: line.LineID, MinorUnits: shares[index].Int64()}
	}
	return allocations, nil
}

// Side identifies the signed ledger convention for one posting amount.
type Side uint8

const (
	Debit Side = iota + 1
	Credit
)

// Signed applies the debit-positive, credit-negative ledger convention.
func Signed(side Side, magnitude int64) (int64, error) {
	if magnitude < 0 {
		return 0, ErrInvalidAmount
	}
	switch side {
	case Debit:
		return magnitude, nil
	case Credit:
		return -magnitude, nil
	default:
		return 0, ErrInvalidSide
	}
}
