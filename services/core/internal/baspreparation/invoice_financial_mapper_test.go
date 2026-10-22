// Draft test file for package baspreparation. Run alongside the mapper draft
// after ROOT copies both files into the package.
package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const invoiceMapTestOrg = "018f0000-0000-7000-8000-000000009001"

type invoiceMapTestLine struct {
	gross, net, gst int64
	treatment       tammyv1.TaxTreatment
}

func TestMapBASInvoiceFinancialEffectsNonCashUsesPostedIncomeLines(t *testing.T) {
	candidate := invoiceMapTestCandidate(t, 9101, "ISSUED", "2026-10-05", tammyv1.GstBasis_GST_BASIS_NON_CASH,
		[]invoiceMapTestLine{
			{gross: 110, net: 100, gst: 10, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE},
			{gross: 40, net: 40, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE},
			{gross: 30, net: 30, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED},
			{gross: 20, net: 20, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE},
			{treatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE},
		}, true)
	capture := invoiceMapTestCapture(candidate)
	frozen := invoiceMapTestKeys(capture)
	before := proto.Clone(candidate.Invoice).(*tammyv1.Invoice)

	got, err := MapBASInvoiceFinancialEffects(context.Background(), capture, frozen,
		tammyv1.GstBasis_GST_BASIS_NON_CASH, invoiceMapTestPeriod())
	if err != nil {
		t.Fatal(err)
	}
	if got.G1GrossMinor != 180 || got.GstOnSales1AMinor != 10 || len(got.Units) != 5 {
		t.Fatalf("non-cash totals/units = G1 %d, 1A %d, units %d; want 180/10/5",
			got.G1GrossMinor, got.GstOnSales1AMinor, len(got.Units))
	}
	if !proto.Equal(candidate.Invoice, before) {
		t.Fatal("mapper mutated the retained invoice body")
	}
	if got.Units[0].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE ||
		got.Units[0].GrossMinor != 110 || got.Units[0].GSTMinor != 10 ||
		got.Units[0].TaxDate == nil || got.Units[0].TaxDate.Day != 5 {
		t.Fatalf("taxable income unit = %#v", got.Units[0])
	}
	if got.Units[3].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON ||
		got.Units[3].ReasonCode != basInvoiceReasonOutOfScope || got.Units[3].GrossMinor != 0 ||
		got.Units[3].SourceGrossMinor != 20 {
		t.Fatalf("out-of-scope unit was not retained as excluded = %#v", got.Units[3])
	}
	if got.Units[4].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO {
		t.Fatalf("in-period zero income line = %#v; want reviewed zero", got.Units[4])
	}
	lineProof := got.Units[0].References[len(got.Units[0].References)-1]
	if lineProof.Table != "invoice_income_lines_v1" || !lineProof.HasSequence || lineProof.ComponentSequence != 1 ||
		lineProof.ID != candidate.ID || len(lineProof.ComputedProofHash) != sha256.Size ||
		got.Units[0].Key.Sequence != 1 {
		t.Fatalf("income-line proof lost structured identity/full proof: %#v", got.Units[0])
	}
}

func TestMapBASInvoiceFinancialEffectsCashUsesActualReceiptDates(t *testing.T) {
	candidate := invoiceMapTestCandidate(t, 9201, "ISSUED", "2026-09-01", tammyv1.GstBasis_GST_BASIS_CASH,
		[]invoiceMapTestLine{
			{gross: 110, net: 100, gst: 10, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE},
			{gross: 30, net: 30, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE},
		}, true)
	invoiceMapTestAddReceipt(t, &candidate, 9211, "2026-09-30", []invoiceMapTestLine{
		{gross: 30, net: 27, gst: 3, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE},
		{gross: 10, net: 10, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE},
	})
	invoiceMapTestAddReceipt(t, &candidate, 9212, "2026-10-10", []invoiceMapTestLine{
		{gross: 40, net: 36, gst: 4, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE},
		{gross: 10, net: 10, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE},
	})
	invoiceMapTestAddReceipt(t, &candidate, 9213, "2026-11-02", []invoiceMapTestLine{
		{gross: 40, net: 37, gst: 3, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE},
		{gross: 10, net: 10, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE},
	})
	capture := invoiceMapTestCapture(candidate)
	frozen := invoiceMapTestKeys(capture)
	got, err := MapBASInvoiceFinancialEffects(context.Background(), capture, frozen,
		tammyv1.GstBasis_GST_BASIS_CASH, invoiceMapTestPeriod())
	if err != nil {
		t.Fatal(err)
	}
	if got.G1GrossMinor != 40 || got.GstOnSales1AMinor != 4 || len(got.Units) != 8 {
		t.Fatalf("cash totals/units = G1 %d, 1A %d, units %d; want 40/4/8",
			got.G1GrossMinor, got.GstOnSales1AMinor, len(got.Units))
	}
	for _, unit := range got.Units[:2] {
		if unit.Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE || unit.TaxDate != nil {
			t.Fatalf("cash invoice-line unit should remain structurally not applicable: %#v", unit)
		}
	}
	var inPeriod, beforePeriod, afterPeriod int
	for _, unit := range got.Units {
		if unit.Key.Kind != invoices.BASCandidateReceiptAllocation || unit.Key.ReceiptID == "" {
			continue
		}
		switch unit.Key.ReceiptID {
		case invoiceMapTestID(9211):
			if unit.Disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON && unit.TaxDate != nil && unit.TaxDate.Month == 9 {
				beforePeriod++
			}
		case invoiceMapTestID(9212):
			if unit.Disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE && unit.TaxDate != nil && unit.TaxDate.Day == 10 {
				inPeriod++
			}
		case invoiceMapTestID(9213):
			if unit.Disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON && unit.TaxDate != nil && unit.TaxDate.Month == 11 {
				afterPeriod++
			}
		}
	}
	if beforePeriod != 2 || inPeriod != 1 || afterPeriod != 2 {
		t.Fatalf("receipt date dispositions = before %d, in-period %d, after %d; want 2/1/2 (out-of-scope unit has its own exclusion)",
			beforePeriod, inPeriod, afterPeriod)
	}
	var foundAllocationProof bool
	for _, unit := range got.Units {
		if unit.Key.ReceiptID != invoiceMapTestID(9212) || unit.Key.Sequence != 1 {
			continue
		}
		for _, ref := range unit.References {
			if ref.Table == "invoice_payment_lines_v1" && ref.ID == invoiceMapTestID(9212) &&
				ref.ParentID == candidate.ID && ref.ComponentSequence == 1 && len(ref.ComputedProofHash) == sha256.Size {
				foundAllocationProof = true
			}
		}
	}
	if !foundAllocationProof {
		t.Fatal("cash allocation unit omitted its exact parent/sequence proof")
	}
}

func TestMapBASInvoiceFinancialEffectsKeepsDraftAndUnpostedRootDecisions(t *testing.T) {
	draft := invoiceMapTestCandidate(t, 9301, "DRAFT", "", tammyv1.GstBasis_GST_BASIS_NON_CASH,
		[]invoiceMapTestLine{{gross: 120, net: 110, gst: 10, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE}}, false)
	oldIssued := invoiceMapTestCandidate(t, 9302, "ISSUED", "2026-08-31", tammyv1.GstBasis_GST_BASIS_NON_CASH,
		[]invoiceMapTestLine{{gross: 220, net: 200, gst: 20, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE}}, false)
	futureIssued := invoiceMapTestCandidate(t, 9303, "ISSUED", "2027-01-01", tammyv1.GstBasis_GST_BASIS_NON_CASH,
		[]invoiceMapTestLine{{gross: 330, net: 300, gst: 30, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE}}, false)
	capture := invoiceMapTestCapture(draft, oldIssued, futureIssued)
	got, err := MapBASInvoiceFinancialEffects(context.Background(), capture, invoiceMapTestKeys(capture),
		tammyv1.GstBasis_GST_BASIS_NON_CASH, invoiceMapTestPeriod())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Units) != 3 || got.G1GrossMinor != 0 || got.GstOnSales1AMinor != 0 || got.UnresolvedBlockerCount != 1 {
		t.Fatalf("root mapping = %#v; want three retained roots, one blocker, zero amounts", got)
	}
	byID := make(map[string]BASInvoiceFinancialUnit, len(got.Units))
	for _, unit := range got.Units {
		byID[unit.Key.InvoiceID] = unit
	}
	if byID[draft.ID].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON ||
		byID[draft.ID].ReasonCode != basInvoiceReasonDraft {
		t.Fatalf("draft fallback = %#v", byID[draft.ID])
	}
	if byID[oldIssued.ID].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED ||
		byID[oldIssued.ID].Severity != tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER ||
		byID[oldIssued.ID].ReasonCode != basInvoiceReasonIssuedUnposted {
		t.Fatalf("old issued-unposted fallback must block through period end: %#v", byID[oldIssued.ID])
	}
	if byID[futureIssued.ID].Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON ||
		byID[futureIssued.ID].ReasonCode != basInvoiceReasonFutureUnposted {
		t.Fatalf("future issued-unposted fallback = %#v", byID[futureIssued.ID])
	}
}

func TestMapBASInvoiceFinancialEffectsRejectsNonFrozenKeysAndCancelledContext(t *testing.T) {
	candidate := invoiceMapTestCandidate(t, 9401, "ISSUED", "2026-10-05", tammyv1.GstBasis_GST_BASIS_NON_CASH,
		[]invoiceMapTestLine{{gross: 110, net: 100, gst: 10, treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE}}, true)
	capture := invoiceMapTestCapture(candidate)
	frozen := invoiceMapTestKeys(capture)
	if result, err := MapBASInvoiceFinancialEffects(context.Background(), capture, frozen[:0],
		tammyv1.GstBasis_GST_BASIS_NON_CASH, invoiceMapTestPeriod()); result != nil || err == nil {
		t.Fatalf("missing frozen candidate returned result=%#v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := MapBASInvoiceFinancialEffects(ctx, capture, frozen,
		tammyv1.GstBasis_GST_BASIS_NON_CASH, invoiceMapTestPeriod()); result != nil || err == nil {
		t.Fatalf("cancelled mapping returned result=%#v err=%v", result, err)
	}
	if result, err := MapBASInvoiceFinancialEffects(context.Background(), capture, frozen,
		tammyv1.GstBasis_GST_BASIS_CASH, invoiceMapTestPeriod()); result != nil || err == nil {
		t.Fatalf("posting basis drift returned result=%#v err=%v", result, err)
	}
}

func invoiceMapTestCandidate(t *testing.T, idNumber int, state, issue string, basis tammyv1.GstBasis, lines []invoiceMapTestLine, posted bool) invoices.BASInvoiceCandidate {
	t.Helper()
	id := invoiceMapTestID(idNumber)
	invoiceState := tammyv1.InvoiceState_INVOICE_STATE_DRAFT
	if state == "ISSUED" {
		invoiceState = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	}
	invoice := &tammyv1.Invoice{
		Id: id, OrganisationId: invoiceMapTestOrg, Version: 2, State: invoiceState,
		Draft: &tammyv1.InvoiceDraft{Number: fmt.Sprintf("MAP-%d", idNumber), IssueDate: issue},
	}
	candidate := invoices.BASInvoiceCandidate{
		ID: id, OrganisationID: invoiceMapTestOrg, Version: 2, State: state,
		Number: invoice.Draft.Number, Invoice: invoice,
	}
	var netTotal, gstTotal, grossTotal, basGross int64
	var proofLines []invoices.BASInvoiceIncomeLine
	for index, source := range lines {
		invoice.Draft.Lines = append(invoice.Draft.Lines, &tammyv1.InvoiceLineInput{
			Description: fmt.Sprintf("line %d", index+1), QuantityMilli: 1000,
			UnitPriceMinor: source.net, TaxTreatment: source.treatment,
		})
		invoice.LineTotals = append(invoice.LineTotals, &tammyv1.InvoiceLineTotal{
			SubtotalMinor: source.net, GstMinor: source.gst, TotalMinor: source.gross,
		})
		netTotal += source.net
		gstTotal += source.gst
		grossTotal += source.gross
		if source.treatment != tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			basGross += source.gross
		}
		proofLines = append(proofLines, invoices.BASInvoiceIncomeLine{
			InvoiceID: id, Sequence: uint64(index + 1), GrossMinor: source.gross, NetMinor: source.net,
			GSTMinor: source.gst, Treatment: source.treatment,
		})
	}
	invoice.SubtotalMinor, invoice.GstMinor, invoice.TotalMinor = netTotal, gstTotal, grossTotal
	encoded, err := proto.Marshal(invoice)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	candidate.InvoiceProto = encoded
	candidate.InvoiceHash = bytes.Clone(digest[:])
	if issue == "" {
		candidate.PeriodDisposition = "UNDATED"
	} else if issue < "2026-10-01" || issue > "2026-10-31" {
		candidate.PeriodDisposition = "OUT_OF_PERIOD"
	} else {
		candidate.PeriodDisposition = "IN_PERIOD"
	}
	if posted {
		posting := &invoices.BASInvoicePosting{
			InvoiceID: id, OrganisationID: invoiceMapTestOrg, InvoiceVersion: 2,
			InvoiceDate: issue, GSTBasis: basis, RevenueAccountID: invoiceMapTestID(9801),
			InvoiceNetMinor: netTotal, InvoiceGSTMinor: gstTotal, InvoiceTotalMinor: grossTotal,
			BASSalesGrossMinor: basGross, SourceHash: bytes.Clone(digest[:]), IncomeLines: proofLines,
		}
		if grossTotal > 0 {
			journalID := invoiceMapTestID(idNumber + 10000)
			posting.JournalID = &journalID
			posting.Journal = invoiceMapTestJournalProof(journalID, 1)
		}
		candidate.Posting = posting
	}
	return candidate
}

func invoiceMapTestAddReceipt(t *testing.T, candidate *invoices.BASInvoiceCandidate, receiptNumber int, date string, allocations []invoiceMapTestLine) {
	t.Helper()
	if candidate == nil || candidate.Posting == nil {
		t.Fatal("receipt fixture requires a posted invoice")
	}
	paymentID := invoiceMapTestID(receiptNumber)
	var amount, gst, basGross int64
	retained := make([]invoices.BASInvoicePaymentLine, 0, len(allocations))
	for index, allocation := range allocations {
		amount += allocation.gross
		gst += allocation.gst
		if allocation.treatment != tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			basGross += allocation.gross
		}
		retained = append(retained, invoices.BASInvoicePaymentLine{
			PaymentID: paymentID, Sequence: uint64(index + 1), GrossMinor: allocation.gross, GSTMinor: allocation.gst,
		})
	}
	journalID := invoiceMapTestID(receiptNumber + 20000)
	created := timestamppb.New(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	paymentDate := invoiceMapTestDate(date)
	payment := &tammyv1.InvoicePayment{
		Id: paymentID, OrganisationId: candidate.OrganisationID, InvoiceId: candidate.ID,
		PaymentDate:   paymentDate,
		Amount:        &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: amount},
		AllocatedGst:  &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: gst},
		BasSalesGross: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: basGross},
		BankAccountId: invoiceMapTestID(9802), Reference: "mapper fixture", JournalId: journalID, CreatedAt: created,
	}
	encoded, err := proto.Marshal(payment)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	candidate.Receipts = append(candidate.Receipts, invoices.BASInvoiceReceipt{
		ID: paymentID, OrganisationID: candidate.OrganisationID, InvoiceID: candidate.ID,
		PaymentDate: date, AmountMinor: amount, AllocatedGSTMinor: gst, BASSalesGrossMinor: basGross,
		BankAccountID: payment.BankAccountId, JournalID: journalID, Reference: payment.Reference,
		PaymentProto: encoded, PaymentHash: bytes.Clone(digest[:]), SourceHash: bytes.Clone(digest[:]),
		Payment: payment, Allocations: retained,
		Journal: invoiceMapTestJournalProof(journalID, 2),
	})
}

func invoiceMapTestJournalProof(id string, payload int64) *accounting.JournalReportingRecord {
	digest := sha256.Sum256([]byte("complete journal proof:" + id))
	return &accounting.JournalReportingRecord{
		Journal:        &tammyv1.Journal{Id: id, OrganisationId: invoiceMapTestOrg},
		SourceRevision: 1, ContentHash: bytes.Clone(digest[:]), PayloadBytes: payload,
	}
}

func invoiceMapTestCapture(candidates ...invoices.BASInvoiceCandidate) *invoices.BASReportingCapture {
	capture := &invoices.BASReportingCapture{
		Candidates: candidates, CandidateCount: len(candidates), Fingerprint: bytes.Repeat([]byte{0x7a}, sha256.Size),
	}
	for _, candidate := range candidates {
		capture.ReceiptCount += len(candidate.Receipts)
	}
	return capture
}

func invoiceMapTestKeys(capture *invoices.BASReportingCapture) []invoices.BASCandidateKey {
	var result []invoices.BASCandidateKey
	for _, candidate := range capture.Candidates {
		lineCount := 0
		if candidate.Posting != nil {
			lineCount = len(candidate.Posting.IncomeLines)
		}
		if lineCount == 0 {
			result = append(result, invoices.BASCandidateKey{
				Kind: invoices.BASCandidateInvoiceRoot, OrganisationID: candidate.OrganisationID, InvoiceID: candidate.ID,
			})
		} else {
			for sequence := 1; sequence <= lineCount; sequence++ {
				result = append(result, invoices.BASCandidateKey{
					Kind: invoices.BASCandidateIncomeLine, OrganisationID: candidate.OrganisationID,
					InvoiceID: candidate.ID, Sequence: uint64(sequence),
				})
			}
		}
		for _, receipt := range candidate.Receipts {
			if len(receipt.Allocations) == 0 {
				result = append(result, invoices.BASCandidateKey{
					Kind: invoices.BASCandidateReceipt, OrganisationID: candidate.OrganisationID,
					InvoiceID: candidate.ID, ReceiptID: receipt.ID,
				})
				continue
			}
			for _, allocation := range receipt.Allocations {
				result = append(result, invoices.BASCandidateKey{
					Kind: invoices.BASCandidateReceiptAllocation, OrganisationID: candidate.OrganisationID,
					InvoiceID: candidate.ID, ReceiptID: receipt.ID, Sequence: allocation.Sequence,
				})
			}
		}
	}
	slices.SortFunc(result, compareBASInvoiceCandidateKey)
	return result
}

func invoiceMapTestPeriod() *tammyv1.BasCivilDateRange {
	return &tammyv1.BasCivilDateRange{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1},
		EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 31},
	}
}

func invoiceMapTestDate(value string) *tammyv1.CivilDate {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return &tammyv1.CivilDate{Year: int32(parsed.Year()), Month: int32(parsed.Month()), Day: int32(parsed.Day())}
}

func invoiceMapTestID(number int) string {
	return fmt.Sprintf("018f0000-0000-7000-8000-%012x", number)
}
