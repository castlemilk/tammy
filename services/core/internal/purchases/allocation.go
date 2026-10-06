package purchases

import "math/big"

// AllocationAmounts is the exact AUD extent of one immutable ordered allocation.
type AllocationAmounts struct{ Gross, SupplierGST, EligibleGST int64 }

// CumulativeAllocations selects gross cents in increasing k/gross priority order.
// Ties retain the source order. The floor seed leaves fewer than len(lines)
// cents to select, so even a maximum-int64 purchase needs bounded work.
func CumulativeAllocations(lines []AllocationAmounts, paid, total int64) ([]AllocationAmounts, error) {
	if paid < 0 || total < 0 || paid > total || len(lines) < 1 || len(lines) > 50 {
		return nil, ErrInvalidAllocation
	}
	var checkedTotal int64
	for _, line := range lines {
		if line.Gross < 0 || line.SupplierGST < 0 || line.EligibleGST < 0 ||
			line.EligibleGST > line.SupplierGST || line.SupplierGST > line.Gross || !addPurchaseAmount(&checkedTotal, line.Gross) {
			return nil, ErrInvalidAllocation
		}
	}
	if checkedTotal != total {
		return nil, ErrInvalidAllocation
	}
	result := make([]AllocationAmounts, len(lines))
	if total == 0 {
		return result, nil
	}
	var allocated int64
	for i, line := range lines {
		result[i].Gross = purchaseProportion(paid, line.Gross, total)
		allocated += result[i].Gross // Seed sum is bounded by paid, already validated.
	}
	for remaining := paid - allocated; remaining > 0; remaining-- {
		selected := -1
		for i, line := range lines {
			if line.Gross == 0 || result[i].Gross == line.Gross {
				continue
			}
			if selected < 0 || purchaseCompareProducts(result[i].Gross+1, lines[selected].Gross, result[selected].Gross+1, line.Gross) < 0 {
				selected = i
			}
		}
		if selected < 0 {
			return nil, ErrInvalidAllocation
		}
		result[selected].Gross++
	}
	for i, line := range lines {
		if line.Gross > 0 {
			// Each GST extent is independently proportional to gross. Rounding a
			// supplier extent first would artificially delay eligible GST.
			result[i].SupplierGST = purchaseProportion(result[i].Gross, line.SupplierGST, line.Gross)
			result[i].EligibleGST = purchaseProportion(result[i].Gross, line.EligibleGST, line.Gross)
		}
	}
	return result, nil
}

// PaymentAllocationDeltas subtracts cumulative extents, preserving conservation
// across any payment partition. An eligible delta can exceed its supplier delta
// when their independent floors cross different cent boundaries.
func PaymentAllocationDeltas(lines []AllocationAmounts, paidBefore, amount, total int64) ([]AllocationAmounts, error) {
	if paidBefore < 0 || total < 0 || paidBefore > total || amount <= 0 || amount > total-paidBefore {
		return nil, ErrInvalidAllocation
	}
	before, err := CumulativeAllocations(lines, paidBefore, total)
	if err != nil {
		return nil, err
	}
	after, err := CumulativeAllocations(lines, paidBefore+amount, total)
	if err != nil {
		return nil, err
	}
	for i := range after {
		after[i].Gross -= before[i].Gross
		after[i].SupplierGST -= before[i].SupplierGST
		after[i].EligibleGST -= before[i].EligibleGST
	}
	return after, nil
}

func purchaseProportion(part, amount, total int64) int64 {
	var product big.Int
	product.Mul(big.NewInt(part), big.NewInt(amount))
	product.Quo(&product, big.NewInt(total))
	return product.Int64()
}

func purchaseCompareProducts(a, b, c, d int64) int {
	var left, right big.Int
	left.Mul(big.NewInt(a), big.NewInt(b))
	right.Mul(big.NewInt(c), big.NewInt(d))
	return left.Cmp(&right)
}
