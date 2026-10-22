package accounting

import (
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestValidateCashFlowAllocationUsesDebitPositiveComponents(t *testing.T) {
	line := &tammyv1.JournalLine{Debit: aud(10000), Credit: aud(0)}
	valid := []CashFlowComponent{{Category: CashFlowOperating, AmountMinor: 7500}, {Category: CashFlowInvesting, AmountMinor: 2500}}
	if err := ValidateCashFlowAllocation(line, true, valid); err != nil {
		t.Fatalf("valid allocation rejected: %v", err)
	}
	credit := &tammyv1.JournalLine{Debit: aud(0), Credit: aud(10000)}
	if err := ValidateCashFlowAllocation(credit, true, []CashFlowComponent{{Category: CashFlowOperating, AmountMinor: -10000}}); err != nil {
		t.Fatalf("credit allocation rejected: %v", err)
	}

	for name, components := range map[string][]CashFlowComponent{
		"missing":       nil,
		"mismatch":      {{Category: CashFlowOperating, AmountMinor: 9999}},
		"unclassified":  {{Category: CashFlowUnspecified, AmountMinor: 10000}},
		"noncash mixed": {{Category: CashFlowNoncash, AmountMinor: 5000}, {Category: CashFlowOperating, AmountMinor: 5000}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCashFlowAllocation(line, true, components); !errors.Is(err, ErrInvalidCashFlow) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNonCashLineRequiresOneExplicitNoncashComponent(t *testing.T) {
	line := &tammyv1.JournalLine{Debit: aud(29000), Credit: aud(0)}
	if err := ValidateCashFlowAllocation(line, false, []CashFlowComponent{{Category: CashFlowNoncash, AmountMinor: 29000}}); err != nil {
		t.Fatalf("valid noncash component rejected: %v", err)
	}
	if err := ValidateCashFlowAllocation(line, false, []CashFlowComponent{{Category: CashFlowOperating, AmountMinor: 29000}}); !errors.Is(err, ErrInvalidCashFlow) {
		t.Fatalf("error = %v", err)
	}
}
