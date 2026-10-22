package baspreparation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func buildCapturedBASWorkpaper(
	ctx context.Context,
	capture *BASBoundedSourceRecordCapture,
	organisation *tammyv1.Organisation,
	period *tammyv1.BasConfirmedPeriod,
	workpaperID string,
	createdAt *timestamppb.Timestamp,
	sourceSnapshotHash []byte,
) (*tammyv1.BasWorkpaper, []uint64, error) {
	fail := func(err error) (*tammyv1.BasWorkpaper, []uint64, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, nil, errors.Join(ErrSourceReservation, err)
	}
	if ctx == nil || capture == nil || capture.Financial == nil || capture.Financial.Invoices == nil ||
		capture.Financial.Purchases == nil || capture.Invoices == nil || capture.Purchases == nil ||
		capture.Purchases.Capture == nil || organisation == nil || period == nil || period.Confirmation == nil ||
		period.Confirmation.Period == nil || !validUUIDv7(organisation.Id) || capture.OrganisationID != organisation.Id ||
		period.OrganisationId != organisation.Id || !validUUIDv7(workpaperID) || createdAt == nil || !createdAt.IsValid() ||
		len(sourceSnapshotHash) != sha256.Size ||
		(organisation.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH && organisation.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) ||
		capture.Financial.Invoices.GstBasis != organisation.GstBasis ||
		!proto.Equal(capture.Financial.Invoices.Period, period.Confirmation.Period) {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	invoiceByID := make(map[string]*invoices.BASInvoiceCandidate, len(capture.Invoices.Candidates))
	for i := range capture.Invoices.Candidates {
		candidate := &capture.Invoices.Candidates[i]
		if candidate.ID == "" || candidate.OrganisationID != organisation.Id || candidate.Invoice == nil ||
			candidate.Invoice.Id != candidate.ID || candidate.Invoice.Draft == nil || len(candidate.InvoiceHash) != sha256.Size {
			return fail(nil)
		}
		if _, exists := invoiceByID[candidate.ID]; exists {
			return fail(nil)
		}
		invoiceByID[candidate.ID] = candidate
	}

	workpaper := &tammyv1.BasWorkpaper{
		Id: workpaperID, OrganisationId: organisation.Id, Version: 1,
		PeriodStart: proto.Clone(period.Confirmation.Period.StartDate).(*tammyv1.CivilDate),
		PeriodEnd:   proto.Clone(period.Confirmation.Period.EndDate).(*tammyv1.CivilDate),
		Status:      tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED,
		CreatedAt:   timestamppb.New(createdAt.AsTime().UTC()), GstBasis: organisation.GstBasis,
		SalesG1: basWorkpaperAUD(0), GstOnSales_1A: basWorkpaperAUD(0),
		GstCredits_1B: basWorkpaperAUD(0), NetGstPayable: basWorkpaperAUD(0),
		SourceFingerprint: append([]byte(nil), sourceSnapshotHash...),
	}
	invoiceTotals := [2]int64{capture.Financial.Invoices.G1GrossMinor, capture.Financial.Invoices.GstOnSales1AMinor}
	var salesGross, salesGST int64
	for _, unit := range capture.Financial.Invoices.Units {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if unit.GrossMinor < 0 || unit.GSTMinor < 0 || unit.GSTMinor > unit.GrossMinor {
			return fail(nil)
		}
		if unit.GrossMinor == 0 && unit.GSTMinor == 0 {
			continue
		}
		if unit.Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE ||
			unit.TaxDate == nil || !validCalendarDate(unit.TaxDate) {
			return fail(nil)
		}
		candidate := invoiceByID[unit.Key.InvoiceID]
		if candidate == nil || candidate.OrganisationID != unit.Key.OrganisationID {
			return fail(nil)
		}
		source := &tammyv1.BasSalesSourceLine{
			InvoiceId: candidate.ID, InvoiceNumber: candidate.Number,
			CustomerName: candidate.Invoice.Draft.CustomerName,
			TaxDate:      proto.Clone(unit.TaxDate).(*tammyv1.CivilDate),
			Gross:        basWorkpaperAUD(unit.GrossMinor), Gst: basWorkpaperAUD(unit.GSTMinor),
		}
		switch unit.Key.Kind {
		case invoices.BASCandidateIncomeLine, invoices.BASCandidateInvoiceRoot:
			source.SourceRevision = candidate.Version
			source.SourceHash = append([]byte(nil), candidate.InvoiceHash...)
		case invoices.BASCandidateReceipt, invoices.BASCandidateReceiptAllocation:
			if unit.Key.ReceiptID == "" {
				return fail(nil)
			}
			var payment *invoices.BASInvoiceReceipt
			for index := range candidate.Receipts {
				if candidate.Receipts[index].ID == unit.Key.ReceiptID {
					if payment != nil {
						return fail(nil)
					}
					payment = &candidate.Receipts[index]
				}
			}
			if payment == nil || payment.Payment == nil || payment.Payment.Id != unit.Key.ReceiptID || len(payment.SourceHash) != sha256.Size {
				return fail(nil)
			}
			receiptID := unit.Key.ReceiptID
			source.PaymentId = &receiptID
			source.SourceRevision = 1
			source.SourceHash = append([]byte(nil), payment.SourceHash...)
		default:
			return fail(nil)
		}
		if len(source.SourceHash) != sha256.Size || source.SourceRevision == 0 {
			return fail(nil)
		}
		if !basAddWorkpaperAmount(&salesGross, unit.GrossMinor) || !basAddWorkpaperAmount(&salesGST, unit.GSTMinor) {
			return fail(nil)
		}
		workpaper.SalesSources = append(workpaper.SalesSources, source)
	}
	if salesGross != invoiceTotals[0] || salesGST != invoiceTotals[1] {
		return fail(fmt.Errorf("BAS invoice workpaper totals differ from financial mapping"))
	}
	workpaper.SalesG1.MinorUnits = salesGross
	workpaper.GstOnSales_1A.MinorUnits = salesGST

	purchase := capture.Financial.Purchases
	workpaper.PurchaseSourceFormat = basWorkpaperPurchaseFormatPtr()
	workpaper.PurchaseSources = make([]*tammyv1.BasPurchaseSourceLine, len(purchase.Sources))
	for index, source := range purchase.Sources {
		if source == nil {
			return fail(nil)
		}
		workpaper.PurchaseSources[index] = proto.Clone(source).(*tammyv1.BasPurchaseSourceLine)
	}
	workpaper.PurchaseReviews = make([]*tammyv1.BasPurchaseReview, len(purchase.Reviews))
	for index, review := range purchase.Reviews {
		if review == nil {
			return fail(nil)
		}
		workpaper.PurchaseReviews[index] = proto.Clone(review).(*tammyv1.BasPurchaseReview)
	}
	workpaper.LegacyPurchaseSupport = make([]*tammyv1.BasPurchaseLegacySupport, len(purchase.LegacySupport))
	for index, support := range purchase.LegacySupport {
		if support == nil {
			return fail(nil)
		}
		workpaper.LegacyPurchaseSupport[index] = proto.Clone(support).(*tammyv1.BasPurchaseLegacySupport)
	}
	if purchase.LegacyCandidateGST == nil || purchase.LegacyCandidateGST.CurrencyCode != "AUD" || purchase.LegacyCandidateGST.MinorUnits < 0 {
		return fail(nil)
	}
	workpaper.LegacyCandidateGst = proto.Clone(purchase.LegacyCandidateGST).(*tammyv1.Money)
	workpaper.ExcludedCashPurchaseCount = purchase.ExcludedCashPurchaseCount
	var purchaseCredits int64
	for _, source := range purchase.Sources {
		if source == nil || source.ClaimedGst == nil || source.ClaimedGst.CurrencyCode != "AUD" || source.ClaimedGst.MinorUnits < 0 ||
			!basAddWorkpaperAmount(&purchaseCredits, source.ClaimedGst.MinorUnits) {
			return fail(nil)
		}
	}
	workpaper.GstCredits_1B.MinorUnits = purchaseCredits
	workpaper.NetGstPayable.MinorUnits = salesGST - purchaseCredits
	if len(purchase.Reviews) != 0 {
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings,
			fmt.Sprintf("%d retained purchase review rows include legacy candidates, evidence suspense, exclusions or future claims. Review the complete typed purchase details before relying on this draft.", len(purchase.Reviews)))
	}
	unposted := 0
	for _, unit := range capture.Financial.Invoices.Units {
		if unit.ReasonCode == basInvoiceReasonIssuedUnposted {
			unposted++
		}
	}
	if unposted > 0 {
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings,
			fmt.Sprintf("%d issued invoices dated on or before the period end have not been added to accounts. Review them before relying on this workpaper.", unposted))
	}
	if len(workpaper.ReviewWarnings) > 10 {
		return fail(nil)
	}
	return workpaper, make([]uint64, 0), nil
}

func basWorkpaperPurchaseFormatPtr() *tammyv1.BasPurchaseSourceFormat {
	value := tammyv1.BasPurchaseSourceFormat_BAS_PURCHASE_SOURCE_FORMAT_RECONCILED_V1
	return &value
}

func basWorkpaperAUD(value int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: value}
}

func basAddWorkpaperAmount(total *int64, value int64) bool {
	if total == nil || value < 0 || *total > int64(^uint64(0)>>1)-value {
		return false
	}
	*total += value
	return true
}
