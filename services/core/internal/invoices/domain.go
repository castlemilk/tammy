package invoices

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrInvalidDraft = errors.New("invoices: invalid draft")

const maxTotalMinor int64 = 1_000_000_000_000

// Calculate rounds each ex-GST line and then each taxable line's 10% GST,
// half up. All arithmetic stays in integer AUD cents.
func Calculate(draft *tammyv1.InvoiceDraft) (*tammyv1.Invoice, error) {
	if draft == nil || !text(draft.Number, 40, false) || !text(draft.CustomerName, 200, false) || !text(draft.CustomerAddress, 1000, false) || !text(draft.SellerAddress, 1000, false) || !text(draft.Notes, 2000, false) || !text(draft.PaymentInstructions, 1000, false) || (draft.CustomerAbn != "" && organisations.ValidateABN(draft.CustomerAbn) != nil) || !date(draft.IssueDate) || !date(draft.DueDate) || (draft.IssueDate != "" && draft.DueDate != "" && draft.DueDate < draft.IssueDate) || len(draft.Lines) < 1 || len(draft.Lines) > 50 {
		return nil, ErrInvalidDraft
	}
	result := &tammyv1.Invoice{}
	for _, line := range draft.Lines {
		if line == nil || !text(line.Description, 500, true) || line.QuantityMilli < 1 || line.QuantityMilli > 1_000_000 || line.UnitPriceMinor < 0 || line.UnitPriceMinor > 100_000_000 || (line.UnitCostMinor != nil && (*line.UnitCostMinor < 0 || *line.UnitCostMinor > 100_000_000)) || (line.SourceDocumentId != nil && !ids.IsCanonicalV7(*line.SourceDocumentId)) {
			return nil, ErrInvalidDraft
		}
		totals := &tammyv1.InvoiceLineTotal{SubtotalMinor: (int64(line.QuantityMilli)*line.UnitPriceMinor + 500) / 1000}
		switch line.TaxTreatment {
		case tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE:
			if !draft.GstRegistered {
				return nil, ErrInvalidDraft
			}
			totals.GstMinor = (totals.SubtotalMinor + 5) / 10
		case tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE, tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED, tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE:
		default:
			return nil, ErrInvalidDraft
		}
		totals.TotalMinor = totals.SubtotalMinor + totals.GstMinor
		if line.UnitCostMinor != nil {
			v := (int64(line.QuantityMilli)*(*line.UnitCostMinor) + 500) / 1000
			totals.CostMinor = &v
			result.CostMinor += v
		}
		result.LineTotals = append(result.LineTotals, totals)
		result.SubtotalMinor += totals.SubtotalMinor
		result.GstMinor += totals.GstMinor
		result.TotalMinor += totals.TotalMinor
		if result.TotalMinor > maxTotalMinor || result.CostMinor > maxTotalMinor {
			return nil, ErrInvalidDraft
		}
	}
	return result, nil
}

func ValidateIssue(draft *tammyv1.InvoiceDraft) error {
	if _, err := Calculate(draft); err != nil {
		return err
	}
	if draft.Number == "" || draft.IssueDate == "" || draft.DueDate == "" || draft.CustomerName == "" || draft.CustomerAddress == "" || draft.SellerAddress == "" {
		return ErrInvalidDraft
	}
	return nil
}
func text(value string, max int, required bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max || strings.TrimSpace(value) != value || (required && value == "") {
		return false
	}
	for _, character := range value {
		if (character < 32 && character != '\t' && character != '\n' && character != '\r') || character == 127 {
			return false
		}
	}
	return true
}
func date(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Year() >= 1900 && parsed.Year() <= 9999 && parsed.Format("2006-01-02") == value
}
