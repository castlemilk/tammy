package accounting

import (
	"errors"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

var ErrInvalidCashFlow = errors.New("accounting: invalid cash-flow allocation")

type CashFlowCategory uint8

const (
	CashFlowUnspecified CashFlowCategory = iota
	CashFlowOperating
	CashFlowInvesting
	CashFlowFinancing
	CashFlowTransfer
	CashFlowNoncash
)

// CashFlowComponent is signed debit-positive. Its ordered components are
// immutable facts attached to one journal line.
type CashFlowComponent struct {
	Category    CashFlowCategory
	AmountMinor int64
}

func ValidateCashFlowAllocation(line *tammyv1.JournalLine, cashAccount bool, components []CashFlowComponent) error {
	if line == nil || line.Debit == nil || line.Credit == nil || line.Debit.CurrencyCode != "AUD" ||
		line.Credit.CurrencyCode != "AUD" || (line.Debit.MinorUnits > 0) == (line.Credit.MinorUnits > 0) ||
		len(components) == 0 || len(components) > 64 {
		return ErrInvalidCashFlow
	}
	expected := line.Debit.MinorUnits - line.Credit.MinorUnits
	var total int64
	for _, component := range components {
		if component.AmountMinor == 0 || component.Category < CashFlowOperating || component.Category > CashFlowNoncash {
			return ErrInvalidCashFlow
		}
		if cashAccount && component.Category == CashFlowNoncash || !cashAccount && component.Category != CashFlowNoncash {
			return ErrInvalidCashFlow
		}
		if component.AmountMinor > 0 && total > math.MaxInt64-component.AmountMinor ||
			component.AmountMinor < 0 && total < math.MinInt64-component.AmountMinor {
			return ErrInvalidCashFlow
		}
		total += component.AmountMinor
	}
	if total != expected {
		return ErrInvalidCashFlow
	}
	return nil
}

func IsCashAccount(account *tammyv1.Account) bool {
	return account != nil && (account.ReportClassification == "balance_sheet.cash" ||
		account.ReportClassification == "balance_sheet.cash_equivalent")
}
