package purchases

import (
	"errors"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func TestPurchaseAllocationKnownPriorityAndIndependentGSTFloors(t *testing.T) {
	for _, test := range []struct {
		name        string
		lines       []AllocationAmounts
		paid, total int64
		want        []AllocationAmounts
	}{
		{"stable tie", []AllocationAmounts{{Gross: 1}, {Gross: 1}}, 1, 2, []AllocationAmounts{{Gross: 1}, {}}},
		{"unequal priorities", []AllocationAmounts{{Gross: 1}, {Gross: 3}}, 2, 4, []AllocationAmounts{{}, {Gross: 2}}},
		{"unequal tie", []AllocationAmounts{{Gross: 1}, {Gross: 3}}, 3, 4, []AllocationAmounts{{Gross: 1}, {Gross: 2}}},
		{"direct eligible extent", []AllocationAmounts{{55, 5, 3}}, 37, 55, []AllocationAmounts{{37, 3, 2}}},
		{"zero source", []AllocationAmounts{{}, {}}, 0, 0, []AllocationAmounts{{}, {}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := CumulativeAllocations(test.lines, test.paid, test.total)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("paid=%d allocation=%v want=%v error=%v", test.paid, got, test.want, err)
			}
		})
	}
	delta, err := PaymentAllocationDeltas([]AllocationAmounts{{5, 3, 2}}, 2, 1, 5)
	if err != nil || !reflect.DeepEqual(delta, []AllocationAmounts{{1, 0, 1}}) {
		t.Fatalf("independent floors must cross: delta=%v error=%v", delta, err)
	}
	for _, before := range []int64{0, 5500} {
		delta, err := PaymentAllocationDeltas([]AllocationAmounts{{11000, 1000, 1000}}, before, 5500, 11000)
		if err != nil || !reflect.DeepEqual(delta, []AllocationAmounts{{5500, 500, 500}}) {
			t.Fatalf("$55 payment: before=%d delta=%v error=%v", before, delta, err)
		}
	}
}

// This small-only oracle enumerates all rational cent priorities. It does not
// share the production seed/remainder algorithm or its product comparator.
func purchasePriorityOracle(lines []AllocationAmounts, paid int64) []AllocationAmounts {
	type cent struct {
		line     int
		priority *big.Rat
	}
	var cents []cent
	for i, line := range lines {
		for k := int64(1); k <= line.Gross; k++ {
			cents = append(cents, cent{i, big.NewRat(k, line.Gross)})
		}
	}
	sort.SliceStable(cents, func(i, j int) bool { return cents[i].priority.Cmp(cents[j].priority) < 0 })
	result := make([]AllocationAmounts, len(lines))
	for _, value := range cents[:paid] {
		result[value.line].Gross++
	}
	for i, line := range lines {
		if line.Gross > 0 {
			result[i].SupplierGST = result[i].Gross * line.SupplierGST / line.Gross
			result[i].EligibleGST = result[i].Gross * line.EligibleGST / line.Gross
		}
	}
	return result
}

func TestPurchaseAllocationIndependentOracleConservationMonotonicityAndPartitions(t *testing.T) {
	random := rand.New(rand.NewSource(817))
	for example := 0; example < 120; example++ {
		lines := make([]AllocationAmounts, 1+random.Intn(7))
		var total int64
		for i := range lines {
			gross := int64(random.Intn(10))
			supplier := int64(random.Intn(int(gross) + 1))
			lines[i] = AllocationAmounts{gross, supplier, int64(random.Intn(int(supplier) + 1))}
			total += gross
		}
		previous := make([]AllocationAmounts, len(lines))
		oneCentSum := make([]AllocationAmounts, len(lines))
		for paid := int64(0); paid <= total; paid++ {
			got, err := CumulativeAllocations(lines, paid, total)
			want := purchasePriorityOracle(lines, paid)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("example=%d lines=%v paid=%d got=%v oracle=%v error=%v", example, lines, paid, got, want, err)
			}
			var sum int64
			for i, value := range got {
				sum += value.Gross
				if value.EligibleGST > value.SupplierGST || value.SupplierGST > value.Gross ||
					value.Gross > lines[i].Gross || value.SupplierGST > lines[i].SupplierGST || value.EligibleGST > lines[i].EligibleGST ||
					value.Gross < previous[i].Gross || value.SupplierGST < previous[i].SupplierGST || value.EligibleGST < previous[i].EligibleGST {
					t.Fatalf("cumulative bounds: lines=%v paid=%d previous=%v got=%v", lines, paid, previous, got)
				}
			}
			if sum != paid {
				t.Fatalf("gross conservation: paid=%d sum=%d", paid, sum)
			}
			if paid > 0 {
				delta, err := PaymentAllocationDeltas(lines, paid-1, 1, total)
				if err != nil {
					t.Fatal(err)
				}
				assertPurchaseDelta(t, lines, delta, 1)
				addPurchaseAmounts(oneCentSum, delta)
				if !reflect.DeepEqual(oneCentSum, got) {
					t.Fatalf("tiny partition mismatch: sum=%v cumulative=%v", oneCentSum, got)
				}
			}
			previous = got
		}
		if !reflect.DeepEqual(previous, lines) {
			t.Fatalf("final cents: got=%v want=%v", previous, lines)
		}
		// Independently vary partition sizes and endpoints, including partial settlement.
		for partition := 0; partition < 5; partition++ {
			endpoint := int64(random.Intn(int(total) + 1))
			accumulated := make([]AllocationAmounts, len(lines))
			for paid := int64(0); paid < endpoint; {
				amount := int64(1 + random.Intn(int(endpoint-paid)))
				delta, err := PaymentAllocationDeltas(lines, paid, amount, total)
				if err != nil {
					t.Fatal(err)
				}
				assertPurchaseDelta(t, lines, delta, amount)
				addPurchaseAmounts(accumulated, delta)
				paid += amount
			}
			if want := purchasePriorityOracle(lines, endpoint); !reflect.DeepEqual(accumulated, want) {
				t.Fatalf("partition endpoint=%d sum=%v oracle=%v", endpoint, accumulated, want)
			}
		}
	}
}

func addPurchaseAmounts(sum, delta []AllocationAmounts) {
	for i, value := range delta {
		sum[i].Gross += value.Gross
		sum[i].SupplierGST += value.SupplierGST
		sum[i].EligibleGST += value.EligibleGST
	}
}

func assertPurchaseDelta(t *testing.T, lines, delta []AllocationAmounts, amount int64) {
	t.Helper()
	var gross int64
	for i, value := range delta {
		gross += value.Gross
		if value.Gross < 0 || value.SupplierGST < 0 || value.EligibleGST < 0 ||
			value.Gross > lines[i].Gross || value.SupplierGST > lines[i].SupplierGST || value.EligibleGST > lines[i].EligibleGST ||
			value.SupplierGST > value.Gross || value.EligibleGST > value.Gross {
			t.Fatalf("delta bounds: original=%v delta=%v", lines, delta)
		}
	}
	if gross != amount {
		t.Fatalf("payment conservation: amount=%d gross=%d", amount, gross)
	}
}

func TestPurchaseAllocationMaximumMoneyAndInvalidInputs(t *testing.T) {
	lines := []AllocationAmounts{{math.MaxInt64/2 + 1, 20, 13}, {math.MaxInt64 / 2, 30, 29}}
	for _, paid := range []int64{0, 1, math.MaxInt64 / 2, math.MaxInt64 - 1, math.MaxInt64} {
		got, err := CumulativeAllocations(lines, paid, math.MaxInt64)
		if err != nil || got[0].Gross+got[1].Gross != paid {
			t.Fatalf("maximum source: paid=%d result=%v error=%v", paid, got, err)
		}
		if paid == math.MaxInt64 && !reflect.DeepEqual(got, lines) {
			t.Fatalf("maximum settlement lost cents: %v", got)
		}
	}
	delta, err := PaymentAllocationDeltas(lines, math.MaxInt64-1, 1, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	assertPurchaseDelta(t, lines, delta, 1)
	for _, test := range []struct {
		lines       []AllocationAmounts
		paid, total int64
	}{
		{nil, 0, 0}, {[]AllocationAmounts{{Gross: -1}}, 0, 0},
		{[]AllocationAmounts{{Gross: 1, SupplierGST: -1}}, 0, 1},
		{[]AllocationAmounts{{Gross: 1, SupplierGST: 2}}, 0, 1},
		{[]AllocationAmounts{{Gross: 2, SupplierGST: 1, EligibleGST: 2}}, 0, 2},
		{[]AllocationAmounts{{Gross: 1, EligibleGST: -1}}, 0, 1},
		{[]AllocationAmounts{{Gross: math.MaxInt64}, {Gross: 1}}, 0, math.MaxInt64},
		{[]AllocationAmounts{{Gross: 1}}, -1, 1}, {[]AllocationAmounts{{Gross: 1}}, 2, 1},
		{[]AllocationAmounts{{Gross: 1}}, 0, -1}, {[]AllocationAmounts{{Gross: 1}}, 0, 2},
		{make([]AllocationAmounts, 51), 0, 0},
	} {
		if _, err := CumulativeAllocations(test.lines, test.paid, test.total); !errors.Is(err, ErrInvalidAllocation) {
			t.Fatalf("invalid allocation accepted: %+v error=%v", test, err)
		}
	}
	if _, err := CumulativeAllocations(make([]AllocationAmounts, 50), 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ before, amount int64 }{{-1, 1}, {0, 0}, {0, -1}, {math.MaxInt64, 1}, {math.MaxInt64 - 1, 2}} {
		if _, err := PaymentAllocationDeltas(lines, test.before, test.amount, math.MaxInt64); !errors.Is(err, ErrInvalidAllocation) {
			t.Fatalf("invalid payment accepted: %+v error=%v", test, err)
		}
	}
}
