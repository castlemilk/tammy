package invoices

import (
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

const nominationOrgID = "018f0000-0000-7000-8000-000000000020"

func nominationUint(value uint64) *uint64   { return &value }
func nominationString(value string) *string { return &value }

func nominationFixture() []BASScalarKey {
	const invoice = "opaque/invoice:one"
	return []BASScalarKey{
		{Kind: BASScalarInvoice, OrganisationID: nominationOrgID, InvoiceID: invoice, RootVersion: nominationUint(2)},
		{Kind: BASScalarInvoice, OrganisationID: nominationOrgID, InvoiceID: "opaque/invoice:two", RootVersion: nominationUint(1)},
		{Kind: BASScalarPosting, OrganisationID: nominationOrgID, InvoiceID: invoice, PostingInvoiceVersion: nominationUint(2), SourceHash: make([]byte, 32)},
		{Kind: BASScalarIncomeLine, OrganisationID: nominationOrgID, InvoiceID: invoice, Sequence: nominationUint(1)},
		{Kind: BASScalarIncomeLine, OrganisationID: nominationOrgID, InvoiceID: invoice, Sequence: nominationUint(2)},
		{Kind: BASScalarReceipt, OrganisationID: nominationOrgID, InvoiceID: invoice, ReceiptID: nominationString("receipt/one"), JournalID: nominationString("journal/one"), SourceHash: make([]byte, 32)},
		{Kind: BASScalarReceipt, OrganisationID: nominationOrgID, InvoiceID: invoice, ReceiptID: nominationString("receipt/two"), JournalID: nominationString("journal/two"), SourceHash: make([]byte, 32)},
		{Kind: BASScalarAllocation, OrganisationID: nominationOrgID, InvoiceID: invoice, ReceiptID: nominationString("receipt/one"), Sequence: nominationUint(1)},
		{Kind: BASScalarAllocation, OrganisationID: nominationOrgID, InvoiceID: invoice, ReceiptID: nominationString("receipt/one"), Sequence: nominationUint(2)},
	}
}

// This fixture reservation is independent of the production mapper. Synthetic
// keys establish structural behavior, never validity of financial bodies.
func nominationReservation(keys []BASScalarKey) BASScalarSummary {
	out := BASScalarSummary{OrganisationID: nominationOrgID, ScalarKeyCount: int64(len(keys))}
	for _, key := range keys {
		switch key.Kind {
		case BASScalarInvoice:
			out.RootCount++
		case BASScalarPosting:
			out.PostingCount++
		case BASScalarIncomeLine:
			out.IncomeLineCount++
		case BASScalarReceipt:
			out.ReceiptCount++
		case BASScalarAllocation:
			out.AllocationCount++
		}
		out.ScalarBytes += int64(512 + len(key.OrganisationID) + len(key.InvoiceID) + len(key.SourceHash))
		if key.ReceiptID != nil {
			out.ScalarBytes += int64(len(*key.ReceiptID))
		}
		if key.JournalID != nil {
			out.ScalarBytes += int64(len(*key.JournalID))
		}
	}
	return out
}

func TestInvoiceBASNominationExactFallbackAndStableOrder(t *testing.T) {
	keys := nominationFixture()
	slices.Reverse(keys)
	before := slices.Clone(keys)
	want := []BASCandidateKey{
		{Kind: BASCandidateInvoiceRoot, OrganisationID: nominationOrgID, InvoiceID: "opaque/invoice:two"},
		{Kind: BASCandidateIncomeLine, OrganisationID: nominationOrgID, InvoiceID: "opaque/invoice:one", Sequence: 1},
		{Kind: BASCandidateIncomeLine, OrganisationID: nominationOrgID, InvoiceID: "opaque/invoice:one", Sequence: 2},
		{Kind: BASCandidateReceipt, OrganisationID: nominationOrgID, InvoiceID: "opaque/invoice:one", ReceiptID: "receipt/two"},
		{Kind: BASCandidateReceiptAllocation, OrganisationID: nominationOrgID, InvoiceID: "opaque/invoice:one", ReceiptID: "receipt/one", Sequence: 1},
		{Kind: BASCandidateReceiptAllocation, OrganisationID: nominationOrgID, InvoiceID: "opaque/invoice:one", ReceiptID: "receipt/one", Sequence: 2},
	}
	got, err := NominateBASCandidates(context.Background(), nominationReservation(keys), keys, 6)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("nominations = %#v, %v; want %#v", got, err, want)
	}
	if !reflect.DeepEqual(keys, before) {
		t.Fatal("nomination reordered or changed the physical keys")
	}
	slices.Reverse(keys)
	again, err := NominateBASCandidates(context.Background(), nominationReservation(keys), keys, 6)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("input order changed nominations: %#v, %v", again, err)
	}
}

func TestInvoiceBASNominationExactCandidateBudget(t *testing.T) {
	for _, count := range []int{1000, 1001} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			keys := nominationFixture()[:1]
			keys = append(keys, BASScalarKey{Kind: BASScalarPosting, OrganisationID: nominationOrgID, InvoiceID: keys[0].InvoiceID, PostingInvoiceVersion: nominationUint(2), SourceHash: make([]byte, 32)})
			for sequence := 1; sequence <= count; sequence++ {
				keys = append(keys, BASScalarKey{Kind: BASScalarIncomeLine, OrganisationID: nominationOrgID, InvoiceID: keys[0].InvoiceID, Sequence: nominationUint(uint64(sequence))})
			}
			got, err := NominateBASCandidates(context.Background(), nominationReservation(keys), keys, 5000)
			if count == 1000 {
				if err != nil || len(got) != count || got[0].Sequence != 1 || got[999].Sequence != 1000 {
					t.Fatalf("exact boundary = %d, %v", len(got), err)
				}
			} else if got != nil || !errors.Is(err, ErrSourceBound) {
				t.Fatalf("1001 candidates = %#v, %v; want no result and bound", got, err)
			}
		})
	}
	keys := nominationFixture()
	for _, allowance := range []int64{0, 5} {
		got, err := NominateBASCandidates(context.Background(), nominationReservation(keys), keys, allowance)
		if got != nil || !errors.Is(err, ErrSourceBound) {
			t.Fatalf("allowance %d = %#v, %v", allowance, got, err)
		}
	}
	if got, err := NominateBASCandidates(context.Background(), BASScalarSummary{OrganisationID: nominationOrgID}, nil, 0); err != nil || len(got) != 0 {
		t.Fatalf("empty zero budget = %#v, %v", got, err)
	}
}

func TestInvoiceBASNominationRejectsInvalidPhysicalInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]BASScalarKey) []BASScalarKey
	}{
		{"foreign organisation", func(keys []BASScalarKey) []BASScalarKey { keys[4].OrganisationID = "foreign"; return keys }},
		{"unknown family", func(keys []BASScalarKey) []BASScalarKey { keys[4].Kind = 99; return keys }},
		{"empty invoice", func(keys []BASScalarKey) []BASScalarKey { keys[4].InvoiceID = ""; return keys }},
		{"zero root version", func(keys []BASScalarKey) []BASScalarKey { keys[0].RootVersion = nominationUint(0); return keys }},
		{"unsigned overflow", func(keys []BASScalarKey) []BASScalarKey {
			keys[4].Sequence = nominationUint(math.MaxUint64)
			return keys
		}},
		{"absent sequence", func(keys []BASScalarKey) []BASScalarKey { keys[4].Sequence = nil; return keys }},
		{"zero sequence", func(keys []BASScalarKey) []BASScalarKey { keys[4].Sequence = nominationUint(0); return keys }},
		{"extra root sequence", func(keys []BASScalarKey) []BASScalarKey { keys[0].Sequence = nominationUint(1); return keys }},
		{"absent posting proof", func(keys []BASScalarKey) []BASScalarKey { keys[2].SourceHash = nil; return keys }},
		{"extra empty hash", func(keys []BASScalarKey) []BASScalarKey { keys[4].SourceHash = []byte{}; return keys }},
		{"missing receipt journal", func(keys []BASScalarKey) []BASScalarKey { keys[5].JournalID = nil; return keys }},
		{"duplicate root", func(keys []BASScalarKey) []BASScalarKey { return append(keys, keys[0]) }},
		{"duplicate component", func(keys []BASScalarKey) []BASScalarKey { return append(keys, keys[4]) }},
		{"missing root", func(keys []BASScalarKey) []BASScalarKey { return append(keys[:0:0], keys[1:]...) }},
		{"missing posting", func(keys []BASScalarKey) []BASScalarKey { return append(keys[:2:2], keys[3:]...) }},
		{"missing receipt", func(keys []BASScalarKey) []BASScalarKey { return append(keys[:5:5], keys[6:]...) }},
		{"cross invoice allocation", func(keys []BASScalarKey) []BASScalarKey { keys[7].InvoiceID = keys[1].InvoiceID; return keys }},
		{"reused receipt ID", func(keys []BASScalarKey) []BASScalarKey {
			posting := keys[2]
			posting.InvoiceID = keys[1].InvoiceID
			receipt := keys[5]
			receipt.InvoiceID = keys[1].InvoiceID
			return append(keys, posting, receipt)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys := test.change(nominationFixture())
			got, err := NominateBASCandidates(context.Background(), nominationReservation(keys), keys, 1000)
			if got != nil || !errors.Is(err, ErrRepository) {
				t.Fatalf("invalid keys = %#v, %v; want no result and repository error", got, err)
			}
		})
	}
}

func TestInvoiceBASNominationReservationAndContext(t *testing.T) {
	keys := nominationFixture()
	for _, change := range []func(*BASScalarSummary){
		func(s *BASScalarSummary) { s.RootCount++ },
		func(s *BASScalarSummary) { s.PostingCount++ },
		func(s *BASScalarSummary) { s.IncomeLineCount++ },
		func(s *BASScalarSummary) { s.ReceiptCount++ },
		func(s *BASScalarSummary) { s.AllocationCount++ },
		func(s *BASScalarSummary) { s.ScalarKeyCount++ },
		func(s *BASScalarSummary) { s.ScalarBytes-- },
		func(s *BASScalarSummary) { s.ScalarBytes++ },
		func(s *BASScalarSummary) { s.RootCount = -1 },
	} {
		summary := nominationReservation(keys)
		change(&summary)
		if got, err := NominateBASCandidates(context.Background(), summary, keys, 1000); got != nil || !errors.Is(err, ErrRepository) {
			t.Fatalf("changed reservation %#v = %#v, %v", summary, got, err)
		}
	}
	if got, err := NominateBASCandidates(nil, nominationReservation(keys), keys, 1000); got != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("nil context = %#v, %v", got, err)
	}
	if got, err := NominateBASCandidates(context.Background(), nominationReservation(keys), keys, -1); got != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("negative allowance = %#v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := NominateBASCandidates(ctx, nominationReservation(keys), keys, 1000); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %#v, %v", got, err)
	}
}
