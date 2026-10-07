package accounting

import (
	"context"
	"errors"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrPostingPolicy = errors.New("accounting: posting policy rejected intent")

func supportedJournalSource(source tammyv1.JournalSource) bool {
	switch source {
	case tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		tammyv1.JournalSource_JOURNAL_SOURCE_REVERSAL, tammyv1.JournalSource_JOURNAL_SOURCE_RECEIVABLE,
		tammyv1.JournalSource_JOURNAL_SOURCE_INVOICE_PAYMENT, tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE,
		tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT, tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION:
		return true
	}
	return false
}

func journalSourceKind(sourceType string, source tammyv1.JournalSource) (int64, bool) {
	switch source {
	case tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE:
		return 6, sourceType == "PAYABLE"
	case tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT:
		return 7, sourceType == "BANKING"
	case tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION:
		return 8, sourceType == "TAX"
	}
	return 0, source != tammyv1.JournalSource_JOURNAL_SOURCE_UNSPECIFIED && parseJournalSource(sourceType, 0) == source
}

func workflowOwnedJournal(source tammyv1.JournalSource) bool {
	switch source {
	case tammyv1.JournalSource_JOURNAL_SOURCE_RECEIVABLE, tammyv1.JournalSource_JOURNAL_SOURCE_INVOICE_PAYMENT,
		tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE, tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT,
		tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION:
		return true
	}
	return false
}

type TaxCodeReadPort interface {
	GetEffectiveTaxCode(context.Context, string, string, string) (*tammyv1.TaxCode, error)
}

type JournalStore interface {
	ReserveFinancialRevision(context.Context, *uint64, time.Time) (uint64, error)
	Post(context.Context, *tammyv1.Journal, string, string, uint64, map[string]TaxFact, map[string][]CashFlowComponent, time.Time) error
	Get(context.Context, string) (*tammyv1.Journal, error)
	Reverse(context.Context, string, uint64, *tammyv1.CivilDate, string, string, []string, time.Time) (*tammyv1.Journal, *tammyv1.Journal, error)
}

type PostingIntent struct {
	OrganisationID string
	PostingDate    *tammyv1.CivilDate
	Memo           string
	Lines          []*tammyv1.ManualJournalLineInput
}

func ValidateManualPostingIntent(intent PostingIntent) error {
	if !ids.IsCanonicalV7(intent.OrganisationID) || !validCivilDate(intent.PostingDate) ||
		len(intent.Memo) > 512 || strings.TrimSpace(intent.Memo) != intent.Memo ||
		len(intent.Lines) < 2 || len(intent.Lines) > 1000 {
		return ErrPostingPolicy
	}
	seen := make(map[string]struct{}, len(intent.Lines))
	for _, line := range intent.Lines {
		if line == nil || !ids.IsCanonicalV7(line.ClientLineId) || !ids.IsCanonicalV7(line.AccountId) ||
			line.Debit == nil || line.Credit == nil || line.Debit.CurrencyCode != "AUD" ||
			line.Credit.CurrencyCode != "AUD" || line.Debit.MinorUnits < 0 || line.Credit.MinorUnits < 0 ||
			(line.Debit.MinorUnits > 0) == (line.Credit.MinorUnits > 0) ||
			len(line.Description) > 512 || strings.TrimSpace(line.Description) != line.Description {
			return ErrPostingPolicy
		}
		if _, duplicate := seen[line.ClientLineId]; duplicate {
			return ErrPostingPolicy
		}
		seen[line.ClientLineId] = struct{}{}
		if line.TaxCodeId == nil {
			if line.TaxAmount != nil {
				return ErrPostingPolicy
			}
		} else if !ids.IsCanonicalV7(*line.TaxCodeId) || line.TaxAmount == nil || line.TaxAmount.CurrencyCode != "AUD" {
			return ErrPostingPolicy
		}
	}
	return nil
}
