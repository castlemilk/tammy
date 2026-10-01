package invoices

import (
	"math/big"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

type incomeLine struct {
	Gross, Net, GST int64
	Treatment       tammyv1.TaxTreatment
}

// cumulativeAllocations selects received cents in increasing k/gross order.
// This divisor allocation conserves every receipt cent and is monotone for
// each line, unlike independent rounding or largest-remainder apportionment.
// Equal priorities use the immutable invoice line order.
func cumulativeAllocations(lines []incomeLine, paid, total int64) ([]int64, []int64, error) {
	if paid < 0 || total < 0 || paid > total || len(lines) == 0 {
		return nil, nil, ErrInvalidDraft
	}
	gross, gst := make([]int64, len(lines)), make([]int64, len(lines))
	var check int64
	for _, line := range lines {
		if line.Gross < 0 || line.GST < 0 || line.GST > line.Gross || line.Gross > maxTotalMinor-check {
			return nil, nil, ErrInvalidDraft
		}
		check += line.Gross
	}
	if check != total {
		return nil, nil, ErrInvalidDraft
	}
	if total == 0 {
		return gross, gst, nil
	}
	var allocated int64
	for index, line := range lines {
		gross[index] = proportion(paid, line.Gross, total)
		allocated += gross[index]
	}
	for allocated < paid {
		selected := -1
		for index, line := range lines {
			if line.Gross == 0 || gross[index] >= line.Gross {
				continue
			}
			if selected < 0 || compareProducts(gross[index]+1, lines[selected].Gross, gross[selected]+1, line.Gross) < 0 {
				selected = index
			}
		}
		if selected < 0 {
			return nil, nil, ErrRepository
		}
		gross[selected]++
		allocated++
	}
	for index, line := range lines {
		if line.Gross > 0 {
			gst[index] = proportion(gross[index], line.GST, line.Gross)
		}
	}
	return gross, gst, nil
}
func proportion(part, amount, total int64) int64 {
	var value big.Int
	value.Mul(big.NewInt(part), big.NewInt(amount))
	value.Quo(&value, big.NewInt(total))
	return value.Int64()
}
func compareProducts(a, b, c, d int64) int {
	var left, right big.Int
	left.Mul(big.NewInt(a), big.NewInt(b))
	right.Mul(big.NewInt(c), big.NewInt(d))
	return left.Cmp(&right)
}
