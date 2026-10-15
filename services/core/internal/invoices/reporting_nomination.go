package invoices

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

type BASCandidateKeyKind int64

const (
	BASCandidateInvoiceRoot BASCandidateKeyKind = iota + 1
	BASCandidateIncomeLine
	BASCandidateReceipt
	BASCandidateReceiptAllocation
)

// BASCandidateKey identifies one structural coverage unit, not a financial
// amount or eligibility decision. Both invoice and receipt families remain
// present on either GST basis. Fallback kinds have Sequence zero; invoice-side
// kinds have ReceiptID empty. Opaque IDs are never encoded into composite text.
type BASCandidateKey struct {
	Kind                                 BASCandidateKeyKind
	OrganisationID, InvoiceID, ReceiptID string
	Sequence                             uint64
}

// NominateBASCandidates maps a complete physical scalar reservation to exact
// structural coverage units without SQL or body hydration. The caller retains
// the full physical keys for later body reconciliation. These nominations do
// not establish eligibility, complete proofs or a shared cross-module fence.
func NominateBASCandidates(ctx context.Context, expected BASScalarSummary, keys []BASScalarKey, maximumCandidates int64) ([]BASCandidateKey, error) {
	if ctx == nil || maximumCandidates < 0 || !ids.IsCanonicalV7(expected.OrganisationID) {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if err := validateInvoiceBASNominationKeys(ctx, expected, keys); err != nil {
		return nil, err
	}
	if maximumCandidates > maxBASReportingInvoices {
		maximumCandidates = maxBASReportingInvoices
	}
	out := nominateInvoiceBASKeys(keys)
	if int64(len(out)) > maximumCandidates {
		return nil, &SourceBoundError{Resource: "BAS candidates", Limit: maximumCandidates}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return out, nil
}

func validateInvoiceBASNominationKeys(ctx context.Context, expected BASScalarSummary, keys []BASScalarKey) error {
	for _, count := range []int64{expected.RootCount, expected.PostingCount, expected.IncomeLineCount, expected.ReceiptCount, expected.AllocationCount, expected.ScalarKeyCount, expected.ScalarBytes} {
		if count < 0 {
			return ErrRepository
		}
	}
	for _, bound := range []struct {
		count, limit int64
		resource     string
	}{
		{expected.RootCount, maxBASReportingInvoices, "invoices"},
		{expected.ReceiptCount, maxBASReportingReceipts, "receipts"},
		{expected.ScalarKeyCount, maxBASScalarKeys, "BAS scalar keys"},
		{int64(len(keys)), maxBASScalarKeys, "BAS scalar keys"},
		{expected.ScalarBytes, maxBASReportingBytes, "BAS scalar bytes"},
	} {
		if bound.count > bound.limit {
			return &SourceBoundError{Resource: bound.resource, Limit: bound.limit}
		}
	}
	if int64(len(keys)) != expected.ScalarKeyCount {
		return fmt.Errorf("%w: invoice nomination key count differs from reservation", ErrRepository)
	}
	type identity struct {
		kind             BASScalarKeyKind
		invoice, receipt string
		sequence         uint64
	}
	seen := make(map[identity]bool)
	roots, postings, incomeParents := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	receipts, allocationParents := make(map[string]string), make(map[string]string)
	actual := BASScalarSummary{OrganisationID: expected.OrganisationID}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		if key.OrganisationID != expected.OrganisationID || key.InvoiceID == "" || !validInvoiceBASNominationShape(key) {
			return fmt.Errorf("%w: invalid invoice nomination key shape or owner", ErrRepository)
		}
		id := identity{kind: key.Kind, invoice: key.InvoiceID}
		receiptBytes, journalBytes := 0, 0
		if key.ReceiptID != nil {
			id.receipt = *key.ReceiptID
			receiptBytes = len(*key.ReceiptID)
		}
		if key.Sequence != nil {
			id.sequence = *key.Sequence
		}
		if key.JournalID != nil {
			journalBytes = len(*key.JournalID)
		}
		if seen[id] {
			return fmt.Errorf("%w: duplicate invoice nomination physical key", ErrRepository)
		}
		seen[id] = true
		if !addBASReportingRowPayload(&actual.ScalarBytes, len(key.OrganisationID), len(key.InvoiceID), receiptBytes, journalBytes, len(key.SourceHash)) {
			return &SourceBoundError{Resource: "BAS scalar bytes", Limit: maxBASReportingBytes}
		}
		actual.ScalarKeyCount++
		switch key.Kind {
		case BASScalarInvoice:
			actual.RootCount++
			roots[key.InvoiceID] = true
		case BASScalarPosting:
			actual.PostingCount++
			postings[key.InvoiceID] = true
		case BASScalarIncomeLine:
			actual.IncomeLineCount++
			incomeParents[key.InvoiceID] = true
		case BASScalarReceipt:
			actual.ReceiptCount++
			if _, exists := receipts[id.receipt]; exists {
				return fmt.Errorf("%w: reused invoice receipt identity", ErrRepository)
			}
			receipts[id.receipt] = key.InvoiceID
		case BASScalarAllocation:
			actual.AllocationCount++
			if owner, exists := allocationParents[id.receipt]; exists && owner != key.InvoiceID {
				return fmt.Errorf("%w: cross-invoice allocation identity", ErrRepository)
			}
			allocationParents[id.receipt] = key.InvoiceID
		}
	}
	if actual != expected {
		return fmt.Errorf("%w: invoice nomination family counts or charges differ from reservation", ErrRepository)
	}
	for invoice := range postings {
		if !roots[invoice] {
			return fmt.Errorf("%w: invoice nomination posting has no root", ErrRepository)
		}
	}
	for invoice := range incomeParents {
		if !postings[invoice] {
			return fmt.Errorf("%w: invoice nomination line has no posting", ErrRepository)
		}
	}
	for _, invoice := range receipts {
		if !postings[invoice] {
			return fmt.Errorf("%w: invoice nomination receipt has no posting", ErrRepository)
		}
	}
	for receipt, invoice := range allocationParents {
		if receipts[receipt] != invoice {
			return fmt.Errorf("%w: invoice nomination allocation has no matching receipt", ErrRepository)
		}
	}
	return nil
}

func validInvoiceBASNominationShape(key BASScalarKey) bool {
	for _, value := range []*uint64{key.RootVersion, key.PostingInvoiceVersion, key.Sequence} {
		if value != nil && (*value == 0 || *value > math.MaxInt64) {
			return false
		}
	}
	switch key.Kind {
	case BASScalarInvoice:
		return key.RootVersion != nil && key.PostingInvoiceVersion == nil && key.Sequence == nil && key.ReceiptID == nil && key.JournalID == nil && key.SourceHash == nil
	case BASScalarPosting:
		return key.RootVersion == nil && key.PostingInvoiceVersion != nil && key.Sequence == nil && key.ReceiptID == nil && len(key.SourceHash) == 32 && (key.JournalID == nil || *key.JournalID != "")
	case BASScalarIncomeLine:
		return key.RootVersion == nil && key.PostingInvoiceVersion == nil && key.Sequence != nil && key.ReceiptID == nil && key.JournalID == nil && key.SourceHash == nil
	case BASScalarReceipt:
		return key.RootVersion == nil && key.PostingInvoiceVersion == nil && key.Sequence == nil && key.ReceiptID != nil && *key.ReceiptID != "" && key.JournalID != nil && *key.JournalID != "" && len(key.SourceHash) == 32
	case BASScalarAllocation:
		return key.RootVersion == nil && key.PostingInvoiceVersion == nil && key.Sequence != nil && key.ReceiptID != nil && *key.ReceiptID != "" && key.JournalID == nil && key.SourceHash == nil
	default:
		return false
	}
}

func nominateInvoiceBASKeys(keys []BASScalarKey) []BASCandidateKey {
	lineCounts := make(map[string]int)
	allocationCounts := make(map[string]int)
	for _, key := range keys {
		if key.Kind == BASScalarIncomeLine {
			lineCounts[key.InvoiceID]++
		}
		if key.Kind == BASScalarAllocation && key.ReceiptID != nil {
			allocationCounts[*key.ReceiptID]++
		}
	}
	var out []BASCandidateKey
	for _, key := range keys {
		candidate := BASCandidateKey{OrganisationID: key.OrganisationID, InvoiceID: key.InvoiceID}
		switch key.Kind {
		case BASScalarInvoice:
			if lineCounts[key.InvoiceID] != 0 {
				continue
			}
			candidate.Kind = BASCandidateInvoiceRoot
		case BASScalarIncomeLine:
			candidate.Kind = BASCandidateIncomeLine
		case BASScalarReceipt:
			if key.ReceiptID != nil && allocationCounts[*key.ReceiptID] != 0 {
				continue
			}
			candidate.Kind = BASCandidateReceipt
		case BASScalarAllocation:
			candidate.Kind = BASCandidateReceiptAllocation
		default:
			continue
		}
		if key.ReceiptID != nil {
			candidate.ReceiptID = *key.ReceiptID
		}
		if key.Sequence != nil {
			candidate.Sequence = *key.Sequence
		}
		out = append(out, candidate)
	}
	slices.SortFunc(out, func(left, right BASCandidateKey) int {
		if order := cmp.Compare(left.Kind, right.Kind); order != 0 {
			return order
		}
		if order := cmp.Compare(left.InvoiceID, right.InvoiceID); order != 0 {
			return order
		}
		if order := cmp.Compare(left.ReceiptID, right.ReceiptID); order != 0 {
			return order
		}
		return cmp.Compare(left.Sequence, right.Sequence)
	})
	return out
}
