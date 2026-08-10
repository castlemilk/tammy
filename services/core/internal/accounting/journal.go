package accounting

import (
	"errors"
	"math"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrInvalidJournal    = errors.New("accounting: invalid journal")
	ErrUnbalancedJournal = errors.New("accounting: journal debits and credits differ")
)

// CheckedJournalTotals calculates exact debit and credit columns without
// permitting signed integer wraparound.
func CheckedJournalTotals(lines []*tammyv1.JournalLine) (int64, int64, error) {
	var debits, credits int64
	for _, line := range lines {
		if line == nil || line.Debit == nil || line.Credit == nil || line.Debit.MinorUnits < 0 || line.Credit.MinorUnits < 0 {
			return 0, 0, ErrInvalidJournal
		}
		var ok bool
		if debits, ok = checkedPositiveAdd(debits, line.Debit.MinorUnits); !ok {
			return 0, 0, ErrInvalidJournal
		}
		if credits, ok = checkedPositiveAdd(credits, line.Credit.MinorUnits); !ok {
			return 0, 0, ErrInvalidJournal
		}
	}
	return debits, credits, nil
}

// ValidateJournal enforces the immutable balanced posting contract. When
// manual is true, every referenced account must be ordinary and postable.
func ValidateJournal(journal *tammyv1.Journal, accounts map[string]*tammyv1.Account, manual bool) error {
	if journal == nil || !ids.IsCanonicalV7(journal.Id) || !ids.IsCanonicalV7(journal.OrganisationId) ||
		journal.Version == 0 || journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED ||
		journal.Source < tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION ||
		journal.Source > tammyv1.JournalSource_JOURNAL_SOURCE_REVERSAL || !validCivilDate(journal.PostingDate) ||
		len(journal.Memo) > 512 || strings.TrimSpace(journal.Memo) != journal.Memo ||
		len(journal.Lines) < 2 || len(journal.Lines) > 1000 || journal.TotalDebits == nil ||
		journal.TotalCredits == nil || journal.TotalDebits.CurrencyCode != "AUD" ||
		journal.TotalCredits.CurrencyCode != "AUD" || journal.PostedAt == nil ||
		journal.PostedAt.CheckValid() != nil || journal.FinancialRevision == 0 {
		return ErrInvalidJournal
	}
	seen := make(map[string]struct{}, len(journal.Lines))
	for index, line := range journal.Lines {
		if line == nil || !ids.IsCanonicalV7(line.Id) || line.JournalId != journal.Id ||
			!ids.IsCanonicalV7(line.AccountId) || line.Sequence != uint32(index+1) ||
			len(line.Description) > 512 || strings.TrimSpace(line.Description) != line.Description ||
			line.Debit == nil || line.Credit == nil || line.Debit.CurrencyCode != "AUD" ||
			line.Credit.CurrencyCode != "AUD" || line.Debit.MinorUnits < 0 || line.Credit.MinorUnits < 0 ||
			(line.Debit.MinorUnits > 0) == (line.Credit.MinorUnits > 0) {
			return ErrInvalidJournal
		}
		if _, duplicate := seen[line.Id]; duplicate {
			return ErrInvalidJournal
		}
		seen[line.Id] = struct{}{}
		account := accounts[line.AccountId]
		if account == nil || account.OrganisationId != journal.OrganisationId ||
			(manual && ValidateManualPosting(account) != nil) ||
			(!manual && (ValidateAccount(account) != nil || account.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE)) {
			return ErrAccountNotPostable
		}
		if !validJournalTax(line) {
			return ErrInvalidJournal
		}
	}
	debits, credits, err := CheckedJournalTotals(journal.Lines)
	if err != nil {
		return err
	}
	if debits == 0 || debits != credits {
		return ErrUnbalancedJournal
	}
	if journal.TotalDebits.MinorUnits != debits || journal.TotalCredits.MinorUnits != credits {
		return ErrInvalidJournal
	}
	return nil
}

func validJournalTax(line *tammyv1.JournalLine) bool {
	if line.TaxCodeId == nil {
		return line.TaxAmount == nil && line.TaxRule == nil
	}
	return ids.IsCanonicalV7(*line.TaxCodeId) && line.TaxAmount != nil && line.TaxAmount.CurrencyCode == "AUD" &&
		line.TaxRule != nil && line.TaxRule.Type == "tax_rule_bundle" && ids.IsCanonicalV7(line.TaxRule.Id) &&
		line.TaxRule.Revision > 0 && len(line.TaxRule.ContentHash) == 32
}

func validCivilDate(value *tammyv1.CivilDate) bool {
	if value == nil || value.Year < 1 || value.Year > 9999 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return false
	}
	date := time.Date(int(value.Year), time.Month(value.Month), int(value.Day), 0, 0, 0, 0, time.UTC)
	return date.Year() == int(value.Year) && int32(date.Month()) == value.Month && date.Day() == int(value.Day)
}

func checkedPositiveAdd(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

// BuildJournalReversal returns a terminal projection for the original and a
// new immutable inverse journal. It never mutates the caller-owned original.
func BuildJournalReversal(original *tammyv1.Journal, reversalID string, lineIDs []string,
	date *tammyv1.CivilDate, reason string, financialRevision uint64, now time.Time,
) (*tammyv1.Journal, *tammyv1.Journal, error) {
	if original == nil || original.State != tammyv1.JournalState_JOURNAL_STATE_POSTED ||
		original.ReversedByJournalId != nil || !ids.IsCanonicalV7(reversalID) || len(lineIDs) != len(original.Lines) ||
		!validCivilDate(date) || reason == "" || len(reason) > 512 || strings.TrimSpace(reason) != reason ||
		financialRevision == 0 || now.IsZero() {
		return nil, nil, ErrInvalidJournal
	}
	seen := make(map[string]struct{}, len(lineIDs))
	reversal := &tammyv1.Journal{Id: reversalID, OrganisationId: original.OrganisationId, Version: 1,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, Source: tammyv1.JournalSource_JOURNAL_SOURCE_REVERSAL,
		PostingDate: proto.Clone(date).(*tammyv1.CivilDate), Memo: reason,
		Lines:               make([]*tammyv1.JournalLine, 0, len(original.Lines)),
		TotalDebits:         proto.Clone(original.TotalCredits).(*tammyv1.Money),
		TotalCredits:        proto.Clone(original.TotalDebits).(*tammyv1.Money),
		ReversalOfJournalId: stringPointer(original.Id), PostedAt: timestamppb.New(now), FinancialRevision: financialRevision}
	for index, originalLine := range original.Lines {
		if !ids.IsCanonicalV7(lineIDs[index]) {
			return nil, nil, ErrInvalidJournal
		}
		if _, duplicate := seen[lineIDs[index]]; duplicate {
			return nil, nil, ErrInvalidJournal
		}
		seen[lineIDs[index]] = struct{}{}
		line := &tammyv1.JournalLine{Id: lineIDs[index], JournalId: reversalID, AccountId: originalLine.AccountId,
			Sequence: uint32(index + 1), Debit: proto.Clone(originalLine.Credit).(*tammyv1.Money),
			Credit: proto.Clone(originalLine.Debit).(*tammyv1.Money), Description: originalLine.Description}
		if originalLine.TaxCodeId != nil {
			line.TaxCodeId = stringPointer(*originalLine.TaxCodeId)
			line.TaxAmount = proto.Clone(originalLine.TaxAmount).(*tammyv1.Money)
			line.TaxAmount.MinorUnits = -line.TaxAmount.MinorUnits
			line.TaxRule = proto.Clone(originalLine.TaxRule).(*tammyv1.SourceRef)
		}
		reversal.Lines = append(reversal.Lines, line)
	}
	reversed := proto.Clone(original).(*tammyv1.Journal)
	reversed.State = tammyv1.JournalState_JOURNAL_STATE_REVERSED
	reversed.Version++
	reversed.ReversedByJournalId = stringPointer(reversalID)
	return reversed, reversal, nil
}

func stringPointer(value string) *string { return &value }
