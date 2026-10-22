package baspreparation

import (
	"errors"
	"math"
	"sort"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

const maximumBASSourcePayloadBytes = int64(8 * 1024 * 1024)

type BASPayloadModuleSizes struct {
	BasePayloadBytes int64
	PayloadBytes     int64
	JournalIDs       []string
	JournalBytes     map[string]int64
}

type BASPayloadExtraSizes struct {
	RuleBytes, DecisionBytes, DocumentStateBytes, CashProjectionBytes int64
	PurchaseTaxFactBytes                                              int64 // already included in purchase BasePayloadBytes
}

type BASUniqueJournalCharge struct {
	OrganisationID string
	JournalID      string // exact opaque ID, never canonicalized or trimmed
	PayloadBytes   int64
}

type BASPayloadReservation struct {
	OrganisationID       string
	ScalarBytes          int64
	InvoiceBaseBytes     int64
	PurchaseBaseBytes    int64
	AccountingBaseBytes  int64
	RuleBytes            int64
	DecisionBytes        int64
	DocumentStateBytes   int64
	CashProjectionBytes  int64
	PurchaseTaxFactBytes int64
	Journals             []BASUniqueJournalCharge
	JournalBytes         int64
	ReservedPayloadBytes int64 // exact retained charge after verified physical duplicate reuse
}

// reserveBASPayloadBytes merges exact complete-parent charges and separately
// retains every base and selector category. A purchase fact discount requires
// the module's aggregate physical-row proof after the complete accounting
// preflight on the same transaction; loaded canonical parity is still mandatory.
// Source nomination and financial effects are independent of proof reuse.
func reserveBASPayloadBytes(
	organisationID string,
	scalar BASSourceScalarReservation,
	invoice, purchase, accounting BASPayloadModuleSizes,
	extra BASPayloadExtraSizes,
	physicalReuse purchases.BASPurchaseTaxFactReuseProof,
	expectedTaxFactCount int64,
	maximumBytes int64,
) (*BASPayloadReservation, error) {
	if !ids.IsCanonicalV7(organisationID) || maximumBytes < 0 ||
		scalar.Invoices.OrganisationID != organisationID ||
		scalar.Purchases.OrganisationID != organisationID ||
		scalar.Accounting.OrganisationID != organisationID || scalar.ScalarBytes < 0 {
		return nil, ErrSourceReservation
	}
	if maximumBytes > maximumBASSourcePayloadBytes {
		maximumBytes = maximumBASSourcePayloadBytes
	}
	if scalar.ScalarBytes > maximumBASSourcePayloadBytes ||
		!nonnegativeBASPayloadExtras(extra) || extra.PurchaseTaxFactBytes > purchase.BasePayloadBytes {
		return nil, ErrSourceReservation
	}

	scalarTotal, valid := sumBASSourceScalars(scalar.Invoices.ScalarBytes, scalar.Purchases.ScalarBytes, scalar.Accounting.ScalarBytes)
	if !valid || scalarTotal != scalar.ScalarBytes || expectedTaxFactCount < 0 ||
		expectedTaxFactCount > maximumBASSourceScalarKeys ||
		physicalReuse.FactCount != expectedTaxFactCount || physicalReuse.MatchedFactCount != expectedTaxFactCount ||
		physicalReuse.MismatchCount != 0 || physicalReuse.PayloadBytes != extra.PurchaseTaxFactBytes ||
		physicalReuse.PayloadBytes < expectedTaxFactCount*512 ||
		(expectedTaxFactCount == 0 && physicalReuse.PayloadBytes != 0) {
		return nil, ErrSourceReservation
	}

	journalUnion := make(map[string]int64)
	for _, module := range []BASPayloadModuleSizes{invoice, purchase, accounting} {
		charges, err := validateBASPayloadModule(module)
		if err != nil {
			return nil, err
		}
		for id, charge := range charges {
			if previous, ok := journalUnion[id]; ok && previous != charge {
				return nil, ErrSourceReservation
			}
			journalUnion[id] = charge
		}
	}

	reservation := &BASPayloadReservation{
		OrganisationID: organisationID, ScalarBytes: scalar.ScalarBytes,
		InvoiceBaseBytes: invoice.BasePayloadBytes, PurchaseBaseBytes: purchase.BasePayloadBytes,
		AccountingBaseBytes: accounting.BasePayloadBytes, RuleBytes: extra.RuleBytes,
		DecisionBytes: extra.DecisionBytes, DocumentStateBytes: extra.DocumentStateBytes,
		CashProjectionBytes: extra.CashProjectionBytes, PurchaseTaxFactBytes: extra.PurchaseTaxFactBytes,
	}
	idsByRawID := make([]string, 0, len(journalUnion))
	for id := range journalUnion {
		idsByRawID = append(idsByRawID, id)
	}
	sort.Strings(idsByRawID)
	for _, id := range idsByRawID {
		charge := journalUnion[id]
		reservation.Journals = append(reservation.Journals, BASUniqueJournalCharge{
			OrganisationID: organisationID, JournalID: id, PayloadBytes: charge,
		})
		if err := addBASPayloadBytes(&reservation.JournalBytes, charge); err != nil {
			return nil, err
		}
	}
	for _, charge := range []int64{
		reservation.ScalarBytes, reservation.InvoiceBaseBytes, reservation.PurchaseBaseBytes - reservation.PurchaseTaxFactBytes,
		reservation.AccountingBaseBytes, reservation.RuleBytes, reservation.DecisionBytes,
		reservation.DocumentStateBytes, reservation.CashProjectionBytes, reservation.JournalBytes,
	} {
		if err := addBASPayloadBytes(&reservation.ReservedPayloadBytes, charge); err != nil {
			return nil, err
		}
	}
	if reservation.ReservedPayloadBytes > maximumBytes {
		return nil, errors.Join(ErrSourceReservation, &SourceBoundError{Resource: "reserved payload bytes", Limit: maximumBytes})
	}
	return reservation, nil
}

func validateBASPayloadModule(module BASPayloadModuleSizes) (map[string]int64, error) {
	if module.BasePayloadBytes < 0 || module.PayloadBytes < 0 || len(module.JournalIDs) != len(module.JournalBytes) {
		return nil, ErrSourceReservation
	}
	charges := make(map[string]int64, len(module.JournalIDs))
	var journalTotal int64
	for index, id := range module.JournalIDs {
		charge, ok := module.JournalBytes[id]
		if id == "" || !ok || charge <= 0 || (index > 0 && id <= module.JournalIDs[index-1]) {
			return nil, ErrSourceReservation
		}
		if _, duplicate := charges[id]; duplicate {
			return nil, ErrSourceReservation
		}
		charges[id] = charge
		if err := addBASPayloadBytes(&journalTotal, charge); err != nil {
			return nil, err
		}
	}
	var expected int64
	if err := addBASPayloadBytes(&expected, module.BasePayloadBytes); err != nil {
		return nil, err
	}
	if err := addBASPayloadBytes(&expected, journalTotal); err != nil {
		return nil, err
	}
	if expected != module.PayloadBytes {
		return nil, ErrSourceReservation
	}
	return charges, nil
}

func nonnegativeBASPayloadExtras(extra BASPayloadExtraSizes) bool {
	return extra.RuleBytes >= 0 && extra.DecisionBytes >= 0 && extra.DocumentStateBytes >= 0 &&
		extra.CashProjectionBytes >= 0 && extra.PurchaseTaxFactBytes >= 0
}

func addBASPayloadBytes(total *int64, value int64) error {
	if total == nil || *total < 0 || value < 0 || value > math.MaxInt64-*total {
		return ErrSourceReservation
	}
	*total += value
	return nil
}
