package audit

import (
	"math"
	"testing"
)

func TestCheckedDescriptorAggregateBytesRejectsBudgetAndIntegerOverflow(t *testing.T) {
	if total, ok := checkedAggregateBytes(3, 4, 7); !ok || total != 7 {
		t.Fatalf("exact budget total=%d ok=%t, want 7 true", total, ok)
	}
	if _, ok := checkedAggregateBytes(7, 1, 7); ok {
		t.Fatal("descriptor aggregate above budget was accepted")
	}
	if _, ok := checkedAggregateBytes(math.MaxUint64-1, 2, math.MaxUint64); ok {
		t.Fatal("descriptor aggregate integer overflow was accepted")
	}
}
