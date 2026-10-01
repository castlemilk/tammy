package invoices

import (
	"math/rand"
	"testing"
)

func TestInvoiceReceiptAllocationConservesCentsAndNeverReversesEarlierAllocation(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	for example := 0; example < 100; example++ {
		lines := make([]incomeLine, 2+random.Intn(8))
		var total int64
		for index := range lines {
			lines[index] = incomeLine{Gross: int64(1 + random.Intn(80))}
			lines[index].GST = lines[index].Gross / 11
			total += lines[index].Gross
		}
		previousGross, previousGST := make([]int64, len(lines)), make([]int64, len(lines))
		for paid := int64(0); paid <= total; paid++ {
			gross, gst, err := cumulativeAllocations(lines, paid, total)
			if err != nil {
				t.Fatal(err)
			}
			var allocated int64
			for index := range lines {
				allocated += gross[index]
				if gross[index] < previousGross[index] || gst[index] < previousGST[index] || gst[index]-previousGST[index] > gross[index]-previousGross[index] {
					t.Fatalf("allocation moved backwards: paid=%d before=%v/%v after=%v/%v", paid, previousGross, previousGST, gross, gst)
				}
				if paid == total && (gross[index] != lines[index].Gross || gst[index] != lines[index].GST) {
					t.Fatal("settlement did not retain exact original cents")
				}
			}
			if allocated != paid {
				t.Fatalf("received=%d allocated=%d", paid, allocated)
			}
			previousGross, previousGST = gross, gst
		}
	}
}

func TestInvoiceReceiptAllocationDoesNotOverflowLargeProducts(t *testing.T) {
	lines := []incomeLine{{Gross: 499999999999, GST: 45454545454}, {Gross: 500000000001, GST: 45454545454}}
	gross, gst, err := cumulativeAllocations(lines, 999999999999, 1000000000000)
	if err != nil || gross[0]+gross[1] != 999999999999 || gst[0]+gst[1] > 90909090908 {
		t.Fatalf("gross=%v gst=%v error=%v", gross, gst, err)
	}
}
