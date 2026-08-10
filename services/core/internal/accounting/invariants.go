package accounting

import (
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

var ErrAccountingInvariant = errors.New("accounting: invariant violation")

type InvariantSnapshot struct {
	Accounts  map[string]*tammyv1.Account
	Journals  []*tammyv1.Journal
	TaxFacts  map[string]TaxFact
	CashFlows map[string][]CashFlowComponent
}

func VerifyAccountingInvariants(snapshot InvariantSnapshot) error {
	var ledgerDebits, ledgerCredits int64
	seenJournals := make(map[string]struct{}, len(snapshot.Journals))
	for _, journal := range snapshot.Journals {
		if journal == nil {
			return ErrAccountingInvariant
		}
		if _, duplicate := seenJournals[journal.Id]; duplicate {
			return ErrAccountingInvariant
		}
		seenJournals[journal.Id] = struct{}{}
		if err := ValidateJournal(journal, snapshot.Accounts, journal.Source == tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL); err != nil {
			return errors.Join(ErrAccountingInvariant, err)
		}
		var ok bool
		if ledgerDebits, ok = checkedPositiveAdd(ledgerDebits, journal.TotalDebits.MinorUnits); !ok {
			return ErrAccountingInvariant
		}
		if ledgerCredits, ok = checkedPositiveAdd(ledgerCredits, journal.TotalCredits.MinorUnits); !ok {
			return ErrAccountingInvariant
		}
		for _, line := range journal.Lines {
			fact, hasFact := snapshot.TaxFacts[line.Id]
			if (line.TaxCodeId != nil) != hasFact || hasFact && ValidateTaxFact(fact) != nil {
				return ErrAccountingInvariant
			}
			account := snapshot.Accounts[line.AccountId]
			if ValidateCashFlowAllocation(line, IsCashAccount(account), snapshot.CashFlows[line.Id]) != nil {
				return ErrAccountingInvariant
			}
		}
	}
	if ledgerDebits != ledgerCredits {
		return ErrAccountingInvariant
	}
	return nil
}
