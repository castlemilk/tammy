// Draft for services/core/internal/baspreparation/invoice_financial_mapper.go.
// Pure mapping from a complete invoice-owned capture and frozen structural keys.
package baspreparation

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"google.golang.org/protobuf/proto"
)

const (
	basInvoiceProofScopeProto      = "INVOICE_PROTO"
	basInvoiceProofScopePayment    = "PAYMENT_PROTO"
	basInvoiceProofScopeJournal    = "COMPLETE_JOURNAL"
	basInvoiceProofScopeIncomeLine = "INVOICE_INCOME_LINE_V1"
	basInvoiceProofScopeAllocation = "INVOICE_PAYMENT_ALLOCATION_V1"

	basInvoiceReasonDraft           = "DRAFT_NOT_ISSUED"
	basInvoiceReasonIssuedUnposted  = "ISSUED_UNPOSTED"
	basInvoiceReasonFutureUnposted  = "FUTURE_ISSUED_HELD"
	basInvoiceReasonOutsidePeriod   = "OUTSIDE_CONFIRMED_PERIOD"
	basInvoiceReasonOutOfScope      = "OUT_OF_SCOPE"
	basInvoiceReasonCashLine        = "CASH_BASIS_AWAITING_RECEIPT"
	basInvoiceReasonNonCashReceipt  = "NON_CASH_BASIS_RECEIPT_NOT_RECOGNISED"
	basInvoiceReasonZero            = "REVIEWED_ZERO"
	basInvoiceReasonEligibleLine    = "POSTED_INVOICE_LINE_IN_PERIOD"
	basInvoiceReasonEligibleReceipt = "DATED_RECEIPT_IN_PERIOD"
	basInvoiceReasonEligibleRoot    = "POSTED_INVOICE_IN_PERIOD"
	basInvoiceReasonZeroReceipt     = "RECEIPT_ALLOCATION_ZERO"
	basInvoiceReasonZeroRoot        = "POSTED_INVOICE_ZERO"
	basInvoiceReasonZeroReceiptRoot = "RECEIPT_ZERO"
)

type BASInvoiceProofReference struct {
	// IDs remain separate fields. ComponentSequence preserves exact line or
	// allocation identity without encoding a synthetic composite string.
	Table             string
	ID                string
	ParentID          string
	ComponentSequence uint64
	HasSequence       bool

	// SourceRevision/SourceHash preserve only source metadata present in the
	// retained row. ComputedProofHash is separately scoped and never fills an
	// absent original hash.
	SourceRevision    uint64
	HasSourceRevision bool
	SourceHash        []byte
	SourceHashPresent bool
	ComputedProofHash []byte
	ProofScope        string
}

type BASInvoiceFinancialUnit struct {
	Key invoices.BASCandidateKey

	Disposition tammyv1.BasCoverageDisposition
	Severity    tammyv1.BasFindingSeverity
	ReasonCode  string
	Reason      string

	// GrossMinor/GSTMinor are the effect contributed to G1/1A. Source amounts
	// preserve values from the validated retained row when excluded or N/A.
	GrossMinor       int64
	GSTMinor         int64
	SourceGrossMinor int64
	SourceGSTMinor   int64

	// SourceDate is the retained issue/payment date. TaxDate is set only when
	// this basis attributes the candidate to a BAS period.
	SourceDate *tammyv1.CivilDate
	TaxDate    *tammyv1.CivilDate

	References []BASInvoiceProofReference
}

type BASInvoiceFinancialMapping struct {
	GstBasis               tammyv1.GstBasis
	Period                 *tammyv1.BasCivilDateRange
	CaptureFingerprint     []byte
	Units                  []BASInvoiceFinancialUnit
	G1GrossMinor           int64
	GstOnSales1AMinor      int64
	UnresolvedBlockerCount int
}

// MapBASInvoiceFinancialEffects consumes the complete result of the
// invoice-owned CaptureBASWithPayloadBudget reader and the exact frozen keys
// nominated from that capture's scalar reservation. That reader has already
// run validateBASInvoicePosting, validateBASInvoiceReceipt,
// validateBASInvoiceJournal and validateBASReceiptJournal. This mapper applies
// basis/date disposition without SQL or legacy report-capture input.
//
// It retains every structural candidate. NON_CASH attributes validated income
// lines on invoice issue date; CASH attributes validated receipt allocations
// on actual payment date. Draft roots are excluded. Dated issued roots without
// postings are unresolved blockers through period end, including invoices
// predating period start. Future unposted issued roots remain excluded.
func MapBASInvoiceFinancialEffects(
	ctx context.Context,
	capture *invoices.BASReportingCapture,
	frozen []invoices.BASCandidateKey,
	currentBasis tammyv1.GstBasis,
	period *tammyv1.BasCivilDateRange,
) (*BASInvoiceFinancialMapping, error) {
	fail := func(err error) (*BASInvoiceFinancialMapping, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, errors.Join(ErrSourceReservation, err)
	}
	if ctx == nil || capture == nil || len(capture.Fingerprint) != sha256.Size ||
		(currentBasis != tammyv1.GstBasis_GST_BASIS_CASH && currentBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) ||
		period == nil || !validCalendarDate(period.StartDate) || !validCalendarDate(period.EndDate) ||
		calendarKey(period.StartDate) > calendarKey(period.EndDate) ||
		capture.CandidateCount != len(capture.Candidates) || len(frozen) > maximumBASRecordCandidates ||
		len(capture.Candidates) > maximumBASRecordCandidates {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	startDate, endDate := calendarKey(period.StartDate), calendarKey(period.EndDate)
	byInvoice := make(map[string]*invoices.BASInvoiceCandidate, len(capture.Candidates))
	expected := make([]invoices.BASCandidateKey, 0, len(frozen))
	receiptCount := 0
	organisationID := ""
	for index := range capture.Candidates {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		candidate := &capture.Candidates[index]
		if err := validateBASInvoiceMapCandidate(candidate, organisationID, currentBasis, startDate, endDate); err != nil {
			return fail(err)
		}
		if organisationID == "" {
			organisationID = candidate.OrganisationID
		}
		if _, duplicate := byInvoice[candidate.ID]; duplicate {
			return fail(nil)
		}
		byInvoice[candidate.ID] = candidate
		lineCount := 0
		if candidate.Posting != nil {
			lineCount = len(candidate.Posting.IncomeLines)
		}
		if lineCount == 0 {
			expected = append(expected, invoices.BASCandidateKey{
				Kind: invoices.BASCandidateInvoiceRoot, OrganisationID: candidate.OrganisationID, InvoiceID: candidate.ID,
			})
		} else {
			for sequence := 1; sequence <= lineCount; sequence++ {
				expected = append(expected, invoices.BASCandidateKey{
					Kind: invoices.BASCandidateIncomeLine, OrganisationID: candidate.OrganisationID,
					InvoiceID: candidate.ID, Sequence: uint64(sequence),
				})
			}
		}
		receiptCount += len(candidate.Receipts)
		for _, receipt := range candidate.Receipts {
			if err := ctx.Err(); err != nil {
				return fail(err)
			}
			if receipt.ID == "" || receipt.OrganisationID != candidate.OrganisationID || receipt.InvoiceID != candidate.ID {
				return fail(nil)
			}
			if len(receipt.Allocations) == 0 {
				expected = append(expected, invoices.BASCandidateKey{
					Kind: invoices.BASCandidateReceipt, OrganisationID: candidate.OrganisationID,
					InvoiceID: candidate.ID, ReceiptID: receipt.ID,
				})
				continue
			}
			for _, allocation := range receipt.Allocations {
				if allocation.Sequence == 0 || allocation.PaymentID != receipt.ID {
					return fail(nil)
				}
				expected = append(expected, invoices.BASCandidateKey{
					Kind: invoices.BASCandidateReceiptAllocation, OrganisationID: candidate.OrganisationID,
					InvoiceID: candidate.ID, ReceiptID: receipt.ID, Sequence: allocation.Sequence,
				})
			}
		}
	}
	if receiptCount != capture.ReceiptCount {
		return fail(nil)
	}
	slices.SortFunc(expected, compareBASInvoiceCandidateKey)
	if len(expected) != len(frozen) {
		return fail(nil)
	}
	for index := range expected {
		if expected[index] != frozen[index] {
			return fail(nil)
		}
	}

	result := &BASInvoiceFinancialMapping{
		GstBasis: currentBasis, Period: cloneBASInvoicePeriod(period),
		CaptureFingerprint: bytes.Clone(capture.Fingerprint),
		Units:              make([]BASInvoiceFinancialUnit, 0, len(frozen)),
	}
	invoiceTotals := make(map[string][2]int64)
	receiptTotals := make(map[string][2]int64)
	for _, key := range frozen {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		candidate := byInvoice[key.InvoiceID]
		if candidate == nil || candidate.OrganisationID != key.OrganisationID {
			return fail(nil)
		}
		unit, err := mapBASInvoiceCandidate(ctx, candidate, key, currentBasis, startDate, endDate)
		if err != nil {
			return fail(err)
		}
		if unit.Disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED {
			result.UnresolvedBlockerCount++
		}
		var ok bool
		if result.G1GrossMinor, ok = addBASInvoiceMapAmount(result.G1GrossMinor, unit.GrossMinor); !ok {
			return fail(nil)
		}
		if result.GstOnSales1AMinor, ok = addBASInvoiceMapAmount(result.GstOnSales1AMinor, unit.GSTMinor); !ok {
			return fail(nil)
		}
		if key.Kind == invoices.BASCandidateIncomeLine || key.Kind == invoices.BASCandidateInvoiceRoot {
			current := invoiceTotals[key.InvoiceID]
			if current[0], ok = addBASInvoiceMapAmount(current[0], unit.GrossMinor); !ok {
				return fail(nil)
			}
			if current[1], ok = addBASInvoiceMapAmount(current[1], unit.GSTMinor); !ok {
				return fail(nil)
			}
			invoiceTotals[key.InvoiceID] = current
		}
		if key.Kind == invoices.BASCandidateReceiptAllocation || key.Kind == invoices.BASCandidateReceipt {
			current := receiptTotals[key.ReceiptID]
			if current[0], ok = addBASInvoiceMapAmount(current[0], unit.GrossMinor); !ok {
				return fail(nil)
			}
			if current[1], ok = addBASInvoiceMapAmount(current[1], unit.GSTMinor); !ok {
				return fail(nil)
			}
			receiptTotals[key.ReceiptID] = current
		}
		result.Units = append(result.Units, unit)
	}
	if err := validateBASInvoiceMatureTotals(ctx, capture, invoiceTotals, receiptTotals, currentBasis, startDate, endDate); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return result, nil
}

func validateBASInvoiceMapCandidate(
	candidate *invoices.BASInvoiceCandidate,
	knownOrganisation string,
	basis tammyv1.GstBasis,
	startDate, endDate string,
) error {
	if candidate == nil || candidate.ID == "" || candidate.OrganisationID == "" ||
		(knownOrganisation != "" && candidate.OrganisationID != knownOrganisation) ||
		candidate.Version == 0 || candidate.Invoice == nil || candidate.Invoice.Draft == nil ||
		candidate.Invoice.Id != candidate.ID || candidate.Invoice.OrganisationId != candidate.OrganisationID ||
		candidate.Invoice.Version != candidate.Version || len(candidate.InvoiceProto) == 0 ||
		len(candidate.InvoiceHash) != sha256.Size {
		return ErrSourceReservation
	}
	digest := sha256.Sum256(candidate.InvoiceProto)
	if !bytes.Equal(digest[:], candidate.InvoiceHash) {
		return ErrSourceReservation
	}
	decoded := &tammyv1.Invoice{}
	if proto.Unmarshal(candidate.InvoiceProto, decoded) != nil || !proto.Equal(decoded, candidate.Invoice) {
		return ErrSourceReservation
	}
	var issueDate *tammyv1.CivilDate
	issue := candidate.Invoice.Draft.IssueDate
	if issue != "" {
		var ok bool
		issueDate, ok = basInvoiceMapDate(issue)
		if !ok {
			return ErrSourceReservation
		}
	}
	switch candidate.Invoice.State {
	case tammyv1.InvoiceState_INVOICE_STATE_DRAFT:
		if candidate.State != "DRAFT" {
			return ErrSourceReservation
		}
	case tammyv1.InvoiceState_INVOICE_STATE_ISSUED:
		if candidate.State != "ISSUED" || issueDate == nil {
			return ErrSourceReservation
		}
	default:
		return ErrSourceReservation
	}
	wantDisposition := "UNDATED"
	if issueDate != nil {
		if calendarKey(issueDate) < startDate || calendarKey(issueDate) > endDate {
			wantDisposition = "OUT_OF_PERIOD"
		} else {
			wantDisposition = "IN_PERIOD"
		}
	}
	if candidate.PeriodDisposition != wantDisposition {
		return ErrSourceReservation
	}
	if candidate.Posting != nil {
		posting := candidate.Posting
		if candidate.Invoice.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED ||
			posting.InvoiceID != candidate.ID || posting.OrganisationID != candidate.OrganisationID ||
			posting.InvoiceVersion != candidate.Version || posting.InvoiceDate != issue || posting.GSTBasis != basis ||
			posting.InvoiceNetMinor != candidate.Invoice.SubtotalMinor ||
			posting.InvoiceGSTMinor != candidate.Invoice.GstMinor || posting.InvoiceTotalMinor != candidate.Invoice.TotalMinor ||
			!bytes.Equal(posting.SourceHash, candidate.InvoiceHash) ||
			len(posting.IncomeLines) != len(candidate.Invoice.Draft.Lines) ||
			len(posting.IncomeLines) != len(candidate.Invoice.LineTotals) {
			return ErrSourceReservation
		}
		if _, err := invoicePostingProofRefs(candidate); err != nil {
			return err
		}
		for index, line := range posting.IncomeLines {
			if line.InvoiceID != candidate.ID || line.Sequence != uint64(index+1) ||
				candidate.Invoice.Draft.Lines[index] == nil || candidate.Invoice.LineTotals[index] == nil ||
				line.GrossMinor != candidate.Invoice.LineTotals[index].TotalMinor ||
				line.NetMinor != candidate.Invoice.LineTotals[index].SubtotalMinor ||
				line.GSTMinor != candidate.Invoice.LineTotals[index].GstMinor ||
				line.Treatment != candidate.Invoice.Draft.Lines[index].TaxTreatment {
				return ErrSourceReservation
			}
		}
	} else if len(candidate.Receipts) != 0 {
		return ErrSourceReservation
	}
	return nil
}

func mapBASInvoiceCandidate(
	ctx context.Context,
	candidate *invoices.BASInvoiceCandidate,
	key invoices.BASCandidateKey,
	basis tammyv1.GstBasis,
	startDate, endDate string,
) (BASInvoiceFinancialUnit, error) {
	if err := ctx.Err(); err != nil {
		return BASInvoiceFinancialUnit{}, err
	}
	unit := BASInvoiceFinancialUnit{Key: key, Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION}
	refs, err := invoicePostingProofRefs(candidate)
	if err != nil {
		return BASInvoiceFinancialUnit{}, err
	}
	unit.References = refs
	issueDate, issueOK := basInvoiceMapDate(candidate.Invoice.Draft.IssueDate)
	if candidate.Invoice.Draft.IssueDate == "" {
		issueOK = true
	}
	if !issueOK {
		return BASInvoiceFinancialUnit{}, ErrSourceReservation
	}
	unit.SourceDate = cloneBASInvoiceDate(issueDate)

	switch key.Kind {
	case invoices.BASCandidateInvoiceRoot:
		if candidate.Posting != nil {
			unit.SourceGrossMinor, unit.SourceGSTMinor = candidate.Posting.BASSalesGrossMinor, candidate.Posting.InvoiceGSTMinor
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE,
					basInvoiceReasonCashLine, "Cash-basis sales are attributed to dated receipts, not the invoice posting.")
				return unit, nil
			}
			unit.TaxDate = cloneBASInvoiceDate(issueDate)
			if calendarKey(issueDate) < startDate || calendarKey(issueDate) > endDate {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
					basInvoiceReasonOutsidePeriod, "The posted invoice issue date is outside the confirmed BAS period.")
				return unit, nil
			}
			unit.GrossMinor, unit.GSTMinor = unit.SourceGrossMinor, unit.SourceGSTMinor
			if unit.GrossMinor == 0 && unit.GSTMinor == 0 {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO,
					basInvoiceReasonZeroRoot, "The posted invoice contributes a reviewed zero amount for this period.")
			} else {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE,
					basInvoiceReasonEligibleRoot, "The posted invoice is attributed by issue date in this period.")
			}
			return unit, nil
		}
		unit.SourceGrossMinor, unit.SourceGSTMinor = candidate.Invoice.TotalMinor, candidate.Invoice.GstMinor
		if candidate.Invoice.State == tammyv1.InvoiceState_INVOICE_STATE_DRAFT {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
				basInvoiceReasonDraft, "Draft invoices are retained for review and do not contribute to BAS sales.")
			return unit, nil
		}
		if issueDate == nil {
			return BASInvoiceFinancialUnit{}, ErrSourceReservation
		}
		unit.TaxDate = cloneBASInvoiceDate(issueDate)
		if calendarKey(issueDate) > endDate {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
				basInvoiceReasonFutureUnposted, "The issued invoice is dated after period end and remains held until posting.")
			return unit, nil
		}
		unit.Severity = tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER
		setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED,
			basInvoiceReasonIssuedUnposted, "The issued invoice has no accounting posting and requires review before BAS preparation.")
		return unit, nil

	case invoices.BASCandidateIncomeLine:
		if candidate.Posting == nil || key.Sequence == 0 || key.Sequence > uint64(len(candidate.Posting.IncomeLines)) {
			return BASInvoiceFinancialUnit{}, ErrSourceReservation
		}
		line := candidate.Posting.IncomeLines[key.Sequence-1]
		unit.SourceGrossMinor, unit.SourceGSTMinor = line.GrossMinor, line.GSTMinor
		if basis == tammyv1.GstBasis_GST_BASIS_NON_CASH {
			unit.TaxDate = cloneBASInvoiceDate(issueDate)
			if issueDate == nil {
				return BASInvoiceFinancialUnit{}, ErrSourceReservation
			}
		}
		lineRef, err := invoiceIncomeLineProofRef(candidate, line)
		if err != nil {
			return BASInvoiceFinancialUnit{}, err
		}
		unit.References = append(unit.References, lineRef)
		if basis == tammyv1.GstBasis_GST_BASIS_CASH {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE,
				basInvoiceReasonCashLine, "Cash-basis sales are attributed to receipts, not the invoice income line.")
			return unit, nil
		}
		if line.Treatment == tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
				basInvoiceReasonOutOfScope, "Out-of-scope invoice income is excluded from G1 and 1A.")
			return unit, nil
		}
		if calendarKey(issueDate) < startDate || calendarKey(issueDate) > endDate {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
				basInvoiceReasonOutsidePeriod, "The invoice issue date is outside the confirmed BAS period.")
			return unit, nil
		}
		unit.GrossMinor, unit.GSTMinor = line.GrossMinor, line.GSTMinor
		if unit.GrossMinor == 0 && unit.GSTMinor == 0 {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO,
				basInvoiceReasonZero, "The retained income line is a reviewed zero amount.")
		} else {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE,
				basInvoiceReasonEligibleLine, "The posted income line is within the confirmed period.")
		}
		return unit, nil

	case invoices.BASCandidateReceipt, invoices.BASCandidateReceiptAllocation:
		receipt, ok := basInvoiceReceiptByID(candidate.Receipts, key.ReceiptID)
		if !ok || receipt.Payment == nil {
			return BASInvoiceFinancialUnit{}, ErrSourceReservation
		}
		receiptDate, dateOK := basInvoiceMapDate(receipt.PaymentDate)
		if !dateOK || receipt.PaymentDate != basInvoiceMapDateString(receipt.Payment.PaymentDate) {
			return BASInvoiceFinancialUnit{}, ErrSourceReservation
		}
		unit.SourceDate = cloneBASInvoiceDate(receiptDate)
		receiptRefs, err := invoiceReceiptProofRefs(receipt)
		if err != nil {
			return BASInvoiceFinancialUnit{}, err
		}
		unit.References = append(unit.References, receiptRefs...)
		if key.Kind == invoices.BASCandidateReceipt {
			if len(receipt.Allocations) != 0 {
				return BASInvoiceFinancialUnit{}, ErrSourceReservation
			}
			unit.SourceGrossMinor, unit.SourceGSTMinor = receipt.BASSalesGrossMinor, receipt.AllocatedGSTMinor
			if basis == tammyv1.GstBasis_GST_BASIS_NON_CASH {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE,
					basInvoiceReasonNonCashReceipt, "Non-cash sales are attributed to posted invoice income, not receipts.")
				return unit, nil
			}
			unit.TaxDate = cloneBASInvoiceDate(receiptDate)
			if calendarKey(receiptDate) < startDate || calendarKey(receiptDate) > endDate {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
					basInvoiceReasonOutsidePeriod, "The receipt payment date is outside the confirmed BAS period.")
				return unit, nil
			}
			unit.GrossMinor, unit.GSTMinor = unit.SourceGrossMinor, unit.SourceGSTMinor
			if unit.GrossMinor == 0 && unit.GSTMinor == 0 {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO,
					basInvoiceReasonZeroReceiptRoot, "The retained receipt fallback contributes a reviewed zero amount.")
			} else {
				setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE,
					basInvoiceReasonEligibleReceipt, "Cash-basis sales are attributed on the actual receipt date.")
			}
			return unit, nil
		}
		if candidate.Posting == nil || key.Sequence == 0 ||
			key.Sequence > uint64(len(receipt.Allocations)) || key.Sequence > uint64(len(candidate.Posting.IncomeLines)) {
			return BASInvoiceFinancialUnit{}, ErrSourceReservation
		}
		allocation := receipt.Allocations[key.Sequence-1]
		line := candidate.Posting.IncomeLines[key.Sequence-1]
		if allocation.Sequence != key.Sequence || allocation.PaymentID != receipt.ID {
			return BASInvoiceFinancialUnit{}, ErrSourceReservation
		}
		unit.SourceGrossMinor, unit.SourceGSTMinor = allocation.GrossMinor, allocation.GSTMinor
		lineRef, err := invoiceIncomeLineProofRef(candidate, line)
		if err != nil {
			return BASInvoiceFinancialUnit{}, err
		}
		allocationRef, err := invoicePaymentAllocationProofRef(receipt, allocation)
		if err != nil {
			return BASInvoiceFinancialUnit{}, err
		}
		unit.References = append(unit.References, lineRef, allocationRef)
		if basis == tammyv1.GstBasis_GST_BASIS_NON_CASH {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE,
				basInvoiceReasonNonCashReceipt, "Non-cash sales are attributed to posted invoice income, not receipt allocations.")
			return unit, nil
		}
		unit.TaxDate = cloneBASInvoiceDate(receiptDate)
		if line.Treatment == tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
				basInvoiceReasonOutOfScope, "Out-of-scope receipt allocations are excluded from G1 and 1A.")
			return unit, nil
		}
		if calendarKey(receiptDate) < startDate || calendarKey(receiptDate) > endDate {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
				basInvoiceReasonOutsidePeriod, "The receipt payment date is outside the confirmed BAS period.")
			return unit, nil
		}
		unit.GrossMinor, unit.GSTMinor = allocation.GrossMinor, allocation.GSTMinor
		if unit.GrossMinor == 0 && unit.GSTMinor == 0 {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO,
				basInvoiceReasonZeroReceipt, "The retained receipt allocation is a reviewed zero amount.")
		} else {
			setBASInvoiceDisposition(&unit, tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE,
				basInvoiceReasonEligibleReceipt, "The receipt allocation is within the confirmed period.")
		}
		return unit, nil
	default:
		return BASInvoiceFinancialUnit{}, ErrSourceReservation
	}
}

func invoicePostingProofRefs(candidate *invoices.BASInvoiceCandidate) ([]BASInvoiceProofReference, error) {
	if candidate == nil || candidate.Invoice == nil || len(candidate.InvoiceHash) != sha256.Size {
		return nil, ErrSourceReservation
	}
	refs := []BASInvoiceProofReference{{
		Table: "invoices_v1", ID: candidate.ID, SourceRevision: candidate.Version, HasSourceRevision: true,
		ComputedProofHash: bytes.Clone(candidate.InvoiceHash), ProofScope: basInvoiceProofScopeProto,
	}}
	if candidate.Posting == nil {
		return refs, nil
	}
	posting := candidate.Posting
	if posting.InvoiceID != candidate.ID || posting.OrganisationID != candidate.OrganisationID ||
		posting.InvoiceVersion != candidate.Version || !bytes.Equal(posting.SourceHash, candidate.InvoiceHash) {
		return nil, ErrSourceReservation
	}
	refs = append(refs, BASInvoiceProofReference{
		Table: "invoice_income_postings_v1", ID: posting.InvoiceID,
		SourceRevision: posting.InvoiceVersion, HasSourceRevision: true,
		SourceHash: bytes.Clone(posting.SourceHash), SourceHashPresent: true,
	})
	if posting.JournalID == nil {
		if posting.Journal != nil || posting.InvoiceTotalMinor != 0 {
			return nil, ErrSourceReservation
		}
		return refs, nil
	}
	if *posting.JournalID == "" || posting.Journal == nil {
		return nil, ErrSourceReservation
	}
	journalRef, err := invoiceJournalProofRef(posting.Journal, *posting.JournalID, candidate.OrganisationID)
	if err != nil {
		return nil, err
	}
	return append(refs, journalRef), nil
}

func invoiceReceiptProofRefs(receipt invoices.BASInvoiceReceipt) ([]BASInvoiceProofReference, error) {
	if receipt.ID == "" || receipt.Payment == nil || receipt.Payment.Id != receipt.ID ||
		receipt.Payment.InvoiceId != receipt.InvoiceID || receipt.Payment.OrganisationId != receipt.OrganisationID ||
		len(receipt.SourceHash) != sha256.Size || len(receipt.PaymentHash) != sha256.Size ||
		!bytes.Equal(receipt.SourceHash, receipt.PaymentHash) {
		return nil, ErrSourceReservation
	}
	hash := sha256.Sum256(receipt.PaymentProto)
	if len(receipt.PaymentProto) == 0 || !bytes.Equal(hash[:], receipt.SourceHash) {
		return nil, ErrSourceReservation
	}
	decoded := &tammyv1.InvoicePayment{}
	if proto.Unmarshal(receipt.PaymentProto, decoded) != nil || !proto.Equal(decoded, receipt.Payment) {
		return nil, ErrSourceReservation
	}
	refs := []BASInvoiceProofReference{{
		Table: "invoice_payment_v1", ID: receipt.ID, ParentID: receipt.InvoiceID,
		SourceHash: bytes.Clone(receipt.SourceHash), SourceHashPresent: true,
		ComputedProofHash: bytes.Clone(receipt.PaymentHash), ProofScope: basInvoiceProofScopePayment,
	}}
	journalRef, err := invoiceJournalProofRef(receipt.Journal, receipt.JournalID, receipt.OrganisationID)
	if err != nil {
		return nil, err
	}
	return append(refs, journalRef), nil
}

func invoiceJournalProofRef(
	proof *accounting.JournalReportingRecord,
	journalID, organisationID string,
) (BASInvoiceProofReference, error) {
	if proof == nil || proof.Journal == nil || journalID == "" ||
		proof.Journal.Id != journalID || proof.Journal.OrganisationId != organisationID ||
		len(proof.ContentHash) != sha256.Size || proof.PayloadBytes <= 0 {
		return BASInvoiceProofReference{}, ErrSourceReservation
	}
	return BASInvoiceProofReference{
		Table: "journals", ID: journalID, SourceRevision: proof.SourceRevision, HasSourceRevision: true,
		ComputedProofHash: bytes.Clone(proof.ContentHash), ProofScope: basInvoiceProofScopeJournal,
	}, nil
}

func invoiceIncomeLineProofRef(candidate *invoices.BASInvoiceCandidate, line invoices.BASInvoiceIncomeLine) (BASInvoiceProofReference, error) {
	if candidate == nil || candidate.Posting == nil || line.InvoiceID != candidate.ID || line.Sequence == 0 {
		return BASInvoiceProofReference{}, ErrSourceReservation
	}
	digest := sha256.New()
	basInvoiceMapFrame(digest, []byte("tammy/bas/invoice-income-line/v1"))
	basInvoiceMapFrame(digest, []byte(candidate.OrganisationID))
	basInvoiceMapFrame(digest, []byte(line.InvoiceID))
	basInvoiceMapUint64(digest, line.Sequence)
	basInvoiceMapInt64(digest, line.GrossMinor)
	basInvoiceMapInt64(digest, line.NetMinor)
	basInvoiceMapInt64(digest, line.GSTMinor)
	basInvoiceMapInt64(digest, int64(line.Treatment))
	return BASInvoiceProofReference{
		Table: "invoice_income_lines_v1", ID: candidate.ID,
		ComponentSequence: line.Sequence, HasSequence: true,
		ComputedProofHash: digest.Sum(nil), ProofScope: basInvoiceProofScopeIncomeLine,
	}, nil
}

func invoicePaymentAllocationProofRef(
	receipt invoices.BASInvoiceReceipt,
	allocation invoices.BASInvoicePaymentLine,
) (BASInvoiceProofReference, error) {
	if receipt.ID == "" || allocation.PaymentID != receipt.ID || allocation.Sequence == 0 {
		return BASInvoiceProofReference{}, ErrSourceReservation
	}
	digest := sha256.New()
	basInvoiceMapFrame(digest, []byte("tammy/bas/invoice-payment-allocation/v1"))
	basInvoiceMapFrame(digest, []byte(receipt.OrganisationID))
	basInvoiceMapFrame(digest, []byte(receipt.InvoiceID))
	basInvoiceMapFrame(digest, []byte(receipt.ID))
	basInvoiceMapUint64(digest, allocation.Sequence)
	basInvoiceMapInt64(digest, allocation.GrossMinor)
	basInvoiceMapInt64(digest, allocation.GSTMinor)
	return BASInvoiceProofReference{
		Table: "invoice_payment_lines_v1", ID: receipt.ID, ParentID: receipt.InvoiceID,
		ComponentSequence: allocation.Sequence, HasSequence: true,
		ComputedProofHash: digest.Sum(nil), ProofScope: basInvoiceProofScopeAllocation,
	}, nil
}

func validateBASInvoiceMatureTotals(
	ctx context.Context,
	capture *invoices.BASReportingCapture,
	invoiceTotals, receiptTotals map[string][2]int64,
	basis tammyv1.GstBasis,
	startDate, endDate string,
) error {
	for _, candidate := range capture.Candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if candidate.Posting == nil || candidate.Invoice.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED {
			continue
		}
		if basis == tammyv1.GstBasis_GST_BASIS_NON_CASH {
			issueDate, ok := basInvoiceMapDate(candidate.Posting.InvoiceDate)
			if !ok {
				return ErrSourceReservation
			}
			if calendarKey(issueDate) >= startDate && calendarKey(issueDate) <= endDate {
				got := invoiceTotals[candidate.ID]
				if got[0] != candidate.Posting.BASSalesGrossMinor || got[1] != candidate.Posting.InvoiceGSTMinor {
					return fmt.Errorf("%w: invoice effect differs from retained posting totals", ErrSourceReservation)
				}
			}
		}
		if basis == tammyv1.GstBasis_GST_BASIS_CASH {
			for _, receipt := range candidate.Receipts {
				receiptDate, ok := basInvoiceMapDate(receipt.PaymentDate)
				if !ok {
					return ErrSourceReservation
				}
				if calendarKey(receiptDate) >= startDate && calendarKey(receiptDate) <= endDate {
					got := receiptTotals[receipt.ID]
					if got[0] != receipt.BASSalesGrossMinor || got[1] != receipt.AllocatedGSTMinor {
						return fmt.Errorf("%w: receipt effect differs from retained payment totals", ErrSourceReservation)
					}
				}
			}
		}
	}
	return nil
}

func basInvoiceReceiptByID(receipts []invoices.BASInvoiceReceipt, id string) (invoices.BASInvoiceReceipt, bool) {
	for _, receipt := range receipts {
		if receipt.ID == id {
			return receipt, true
		}
	}
	return invoices.BASInvoiceReceipt{}, false
}

func setBASInvoiceDisposition(
	unit *BASInvoiceFinancialUnit,
	disposition tammyv1.BasCoverageDisposition,
	reasonCode, reason string,
) {
	unit.Disposition, unit.ReasonCode, unit.Reason = disposition, reasonCode, reason
}

func addBASInvoiceMapAmount(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

func basInvoiceMapDate(value string) (*tammyv1.CivilDate, bool) {
	if value == "" {
		return nil, false
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Year() < 1900 || parsed.Year() > 9999 || parsed.Format("2006-01-02") != value {
		return nil, false
	}
	return &tammyv1.CivilDate{Year: int32(parsed.Year()), Month: int32(parsed.Month()), Day: int32(parsed.Day())}, true
}

func basInvoiceMapDateString(value *tammyv1.CivilDate) string {
	if !validCalendarDate(value) {
		return ""
	}
	return calendarKey(value)
}

func cloneBASInvoiceDate(value *tammyv1.CivilDate) *tammyv1.CivilDate {
	if value == nil {
		return nil
	}
	return &tammyv1.CivilDate{Year: value.Year, Month: value.Month, Day: value.Day}
}

func cloneBASInvoicePeriod(value *tammyv1.BasCivilDateRange) *tammyv1.BasCivilDateRange {
	if value == nil {
		return nil
	}
	return &tammyv1.BasCivilDateRange{
		StartDate: cloneBASInvoiceDate(value.StartDate), EndDate: cloneBASInvoiceDate(value.EndDate),
	}
}

func compareBASInvoiceCandidateKey(left, right invoices.BASCandidateKey) int {
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
}

func basInvoiceMapFrame(digest interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

func basInvoiceMapInt64(digest interface{ Write([]byte) (int, error) }, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = digest.Write(encoded[:])
}

func basInvoiceMapUint64(digest interface{ Write([]byte) (int, error) }, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = digest.Write(encoded[:])
}
