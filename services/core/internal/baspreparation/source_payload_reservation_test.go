package baspreparation

import (
	"errors"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"math"
	"reflect"
	"sort"
	"testing"
)

func payloadScalar(bytes int64) BASSourceScalarReservation {
	s := BASSourceScalarReservation{ScalarBytes: bytes}
	s.Invoices.OrganisationID = sourceScalarTestOrganisation
	s.Invoices.ScalarBytes = bytes
	s.Purchases.OrganisationID = sourceScalarTestOrganisation
	s.Accounting.OrganisationID = sourceScalarTestOrganisation
	return s
}
func payloadModule(base int64, journals map[string]int64) BASPayloadModuleSizes {
	var ids []string
	total := base
	for id, charge := range journals {
		ids = append(ids, id)
		total += charge
	}
	sort.Strings(ids)
	return BASPayloadModuleSizes{BasePayloadBytes: base, PayloadBytes: total, JournalIDs: ids, JournalBytes: journals}
}
func TestReserveBASPayloadExactCategoriesAndSharedJournalUnion(t *testing.T) {
	shared := "legacy journal / 04"
	invoice := payloadModule(101, map[string]int64{shared: 700, "invoice-only": 800})
	purchase := payloadModule(1202, map[string]int64{shared: 700})
	accounting := payloadModule(1303, map[string]int64{shared: 700})
	extra := BASPayloadExtraSizes{RuleBytes: 11, DecisionBytes: 12, DocumentStateBytes: 13, CashProjectionBytes: 14, PurchaseTaxFactBytes: 842}
	proof := purchases.BASPurchaseTaxFactReuseProof{FactCount: 1, PayloadBytes: 842, MatchedFactCount: 1}
	got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(10), invoice, purchase, accounting, extra, proof, 1, maximumBASSourcePayloadBytes)
	if err != nil || got == nil {
		t.Fatalf("reservation=%#v err=%v", got, err)
	}
	const want = int64(10 + 101 + 1202 + 1303 + 11 + 12 + 13 + 14 + 700 + 800 - 842)
	if got.ReservedPayloadBytes != want || got.JournalBytes != 1500 || len(got.Journals) != 2 || got.Journals[0].JournalID != "invoice-only" || got.Journals[1].JournalID != shared || got.Journals[1].OrganisationID != sourceScalarTestOrganisation {
		t.Fatalf("reservation=%#v want exact %d", got, want)
	}
	if !reflect.DeepEqual([]int64{got.ScalarBytes, got.InvoiceBaseBytes, got.PurchaseBaseBytes, got.AccountingBaseBytes, got.RuleBytes, got.DecisionBytes, got.DocumentStateBytes, got.CashProjectionBytes, got.PurchaseTaxFactBytes}, []int64{10, 101, 1202, 1303, 11, 12, 13, 14, 842}) {
		t.Fatalf("lost retained category: %#v", got)
	}
}
func TestReserveBASPayloadProvedDiscountPrecedesInclusiveSharedFence(t *testing.T) {
	extra := BASPayloadExtraSizes{PurchaseTaxFactBytes: 842}
	proof := purchases.BASPurchaseTaxFactReuseProof{FactCount: 1, PayloadBytes: 842, MatchedFactCount: 1}
	invoice := payloadModule(maximumBASSourcePayloadBytes-700, nil)
	purchase := payloadModule(842, nil)
	accounting := payloadModule(700, nil)
	for _, maximum := range []int64{maximumBASSourcePayloadBytes, math.MaxInt64} {
		got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), invoice, purchase, accounting, extra, proof, 1, maximum)
		if err != nil || got == nil || got.ReservedPayloadBytes != maximumBASSourcePayloadBytes {
			t.Fatalf("inclusive deduplicated fence: got=%#v err=%v", got, err)
		}
	}
	got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), invoice, purchase, accounting, extra, proof, 1, maximumBASSourcePayloadBytes-1)
	if got != nil || !errors.Is(err, ErrSourceBound) {
		t.Fatalf("one byte short: got=%#v err=%v", got, err)
	}
	invoice.BasePayloadBytes++
	invoice.PayloadBytes++
	got, err = reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), invoice, purchase, accounting, extra, proof, 1, math.MaxInt64)
	if got != nil || !errors.Is(err, ErrSourceBound) {
		t.Fatalf("clamped cap+1: got=%#v err=%v", got, err)
	}
}
func TestReserveBASPayloadRejectsUnprovedOrIncompleteFactDiscount(t *testing.T) {
	valid := purchases.BASPurchaseTaxFactReuseProof{FactCount: 1, PayloadBytes: 842, MatchedFactCount: 1}
	for _, proof := range []purchases.BASPurchaseTaxFactReuseProof{{}, {FactCount: 1, PayloadBytes: 842}, {FactCount: 1, PayloadBytes: 842, MatchedFactCount: 1, MismatchCount: 1}, {FactCount: 2, PayloadBytes: 842, MatchedFactCount: 2}, {FactCount: 1, PayloadBytes: 841, MatchedFactCount: 1}} {
		got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), payloadModule(0, nil), payloadModule(842, nil), payloadModule(700, nil), BASPayloadExtraSizes{PurchaseTaxFactBytes: 842}, proof, 1, maximumBASSourcePayloadBytes)
		if got != nil || !errors.Is(err, ErrSourceReservation) {
			t.Fatalf("unproved discount %#v: got=%#v err=%v", proof, got, err)
		}
	}
	got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), payloadModule(0, nil), payloadModule(841, nil), payloadModule(700, nil), BASPayloadExtraSizes{PurchaseTaxFactBytes: 842}, valid, 1, maximumBASSourcePayloadBytes)
	if got != nil || !errors.Is(err, ErrSourceReservation) {
		t.Fatalf("fact charge outside purchase base: got=%#v err=%v", got, err)
	}
}
func TestReserveBASPayloadRejectsInvalidModuleAndSharedProofCharges(t *testing.T) {
	for name, module := range map[string]BASPayloadModuleSizes{
		"wrong module total": {BasePayloadBytes: 100, PayloadBytes: 801, JournalIDs: []string{"j"}, JournalBytes: map[string]int64{"j": 700}},
		"duplicate ID":       {BasePayloadBytes: 100, PayloadBytes: 1500, JournalIDs: []string{"j", "j"}, JournalBytes: map[string]int64{"j": 700}},
		"missing map entry":  {BasePayloadBytes: 100, PayloadBytes: 800, JournalIDs: []string{"x"}, JournalBytes: map[string]int64{"j": 700}},
		"orphan map entry":   {BasePayloadBytes: 100, PayloadBytes: 800, JournalIDs: []string{"j"}, JournalBytes: map[string]int64{"j": 700, "extra": 0}},
		"unsorted IDs":       {PayloadBytes: 1400, JournalIDs: []string{"z", "a"}, JournalBytes: map[string]int64{"z": 700, "a": 700}},
		"empty ID":           {PayloadBytes: 700, JournalIDs: []string{""}, JournalBytes: map[string]int64{"": 700}},
		"zero charge":        {JournalIDs: []string{"j"}, JournalBytes: map[string]int64{"j": 0}},
		"negative base":      {BasePayloadBytes: -1, PayloadBytes: 0},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), module, payloadModule(0, nil), payloadModule(0, nil), BASPayloadExtraSizes{}, purchases.BASPurchaseTaxFactReuseProof{}, 0, maximumBASSourcePayloadBytes)
			if got != nil || !errors.Is(err, ErrSourceReservation) {
				t.Fatalf("invalid module: got=%#v err=%v", got, err)
			}
		})
	}
	got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), payloadModule(0, map[string]int64{"j": 700}), payloadModule(0, map[string]int64{"j": 701}), payloadModule(0, nil), BASPayloadExtraSizes{}, purchases.BASPurchaseTaxFactReuseProof{}, 0, maximumBASSourcePayloadBytes)
	if got != nil || !errors.Is(err, ErrSourceReservation) {
		t.Fatalf("shared charge conflict: got=%#v err=%v", got, err)
	}
}
func TestReserveBASPayloadRejectsInvalidScalarExtrasAndOverflow(t *testing.T) {
	for _, extra := range []BASPayloadExtraSizes{{RuleBytes: -1}, {DecisionBytes: -1}, {DocumentStateBytes: -1}, {CashProjectionBytes: -1}, {PurchaseTaxFactBytes: -1}} {
		got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), payloadModule(0, nil), payloadModule(0, nil), payloadModule(0, nil), extra, purchases.BASPurchaseTaxFactReuseProof{}, 0, maximumBASSourcePayloadBytes)
		if got != nil || !errors.Is(err, ErrSourceReservation) {
			t.Fatalf("negative extra: got=%#v err=%v", got, err)
		}
	}
	for _, s := range []BASSourceScalarReservation{payloadScalar(-1), payloadScalar(maximumBASSourcePayloadBytes + 1), func() BASSourceScalarReservation { s := payloadScalar(1); s.ScalarBytes = 0; return s }(), func() BASSourceScalarReservation {
		s := payloadScalar(0)
		s.Purchases.OrganisationID = "foreign"
		return s
	}()} {
		got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, s, payloadModule(0, nil), payloadModule(0, nil), payloadModule(0, nil), BASPayloadExtraSizes{}, purchases.BASPurchaseTaxFactReuseProof{}, 0, maximumBASSourcePayloadBytes)
		if got != nil || !errors.Is(err, ErrSourceReservation) {
			t.Fatalf("invalid scalar: got=%#v err=%v", got, err)
		}
	}
	got, err := reserveBASPayloadBytes(sourceScalarTestOrganisation, payloadScalar(0), payloadModule(math.MaxInt64, nil), payloadModule(math.MaxInt64, nil), payloadModule(0, nil), BASPayloadExtraSizes{}, purchases.BASPurchaseTaxFactReuseProof{}, 0, math.MaxInt64)
	if got != nil || !errors.Is(err, ErrSourceReservation) {
		t.Fatalf("overflow: got=%#v err=%v", got, err)
	}
}
