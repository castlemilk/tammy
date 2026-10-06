package purchases

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var (
	ErrInvalidReviewedPurchase = errors.New("purchases: invalid reviewed purchase")
	ErrInvalidAllocation       = errors.New("purchases: invalid allocation")
	ErrPurchaseEligibility     = errors.New("purchases: missing or invalid eligibility review")
	ErrPurchaseAccount         = errors.New("purchases: inappropriate allocation account")
	ErrInvalidEvidence         = errors.New("purchases: invalid evidence review")
	ErrUnsupportedException    = errors.New("purchases: unsupported evidence exception")
)

// ReviewedPurchaseInput contains server-loaded source/account facts and explicit
// retained review decisions. The command owner must reload these within its
// transaction and verify GST profile history and exception provenance there.
// Public uint64 versions are checked for presence here; the storage owner must
// reject values outside its signed SQL integer range before persisting them.
type ReviewedPurchaseInput struct {
	OrganisationID, DocumentID             string
	ReviewedVersion                        uint64
	Document                               *tammyv1.Document
	Allocations                            []*tammyv1.PurchaseAllocationInput
	Accounts                               map[string]*tammyv1.Account
	GSTBasis                               tammyv1.GstBasis
	GSTProfileVersion, GSTHistoryWatermark uint64
	Evidence                               *tammyv1.PurchaseEvidenceInput
	ExceptionRule                          *ReviewedExceptionRule
}
type ReviewedPurchaseAmounts struct {
	Gross, SupplierGST, EligibleGST int64
	Allocations                     []AllocationAmounts
}

// ValidateReviewedPurchase validates exact source arithmetic and the narrow
// supported purchase account policy without posting or changing any source.
func ValidateReviewedPurchase(input ReviewedPurchaseInput) (ReviewedPurchaseAmounts, error) {
	invalid := ReviewedPurchaseAmounts{}
	document := input.Document
	if document == nil || !ids.IsCanonicalV7(input.OrganisationID) || !ids.IsCanonicalV7(input.DocumentID) ||
		document.OrganisationId != input.OrganisationID || document.Id != input.DocumentID || input.ReviewedVersion == 0 ||
		document.Version != input.ReviewedVersion || document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		len(document.Sha256) != 32 || document.ReviewedAt == nil || document.ReviewedAt.CheckValid() != nil || document.Candidate == nil {
		return invalid, ErrInvalidReviewedPurchase
	}
	candidate := document.Candidate
	if !validPurchaseDate(candidate.DocumentDate) || !validPurchaseMoney(candidate.Subtotal) ||
		!validPurchaseMoney(candidate.Gst) || !validPurchaseMoney(candidate.Total) ||
		!purchaseReviewText(candidate.SupplierName, 256, false) || !purchaseReviewText(candidate.InvoiceNumber, 128, false) {
		return invalid, ErrInvalidReviewedPurchase
	}
	gross := candidate.Subtotal.MinorUnits
	if !addPurchaseAmount(&gross, candidate.Gst.MinorUnits) || gross != candidate.Total.MinorUnits {
		return invalid, ErrInvalidReviewedPurchase
	}
	if input.GSTProfileVersion == 0 || (input.GSTBasis != tammyv1.GstBasis_GST_BASIS_UNSPECIFIED &&
		input.GSTBasis != tammyv1.GstBasis_GST_BASIS_CASH && input.GSTBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) ||
		(candidate.Gst.MinorUnits > 0 && input.GSTBasis == tammyv1.GstBasis_GST_BASIS_UNSPECIFIED) {
		return invalid, ErrPurchaseEligibility
	}
	if len(input.Allocations) < 1 || len(input.Allocations) > 50 {
		return invalid, ErrInvalidAllocation
	}
	result := ReviewedPurchaseAmounts{Allocations: make([]AllocationAmounts, len(input.Allocations))}
	for i, allocation := range input.Allocations {
		if allocation == nil || allocation.Sequence != uint32(i+1) || !validPurchaseMoney(allocation.Gross) ||
			!validPurchaseMoney(allocation.SupplierGst) || !validPurchaseMoney(allocation.EligibleGst) ||
			allocation.EligibleGst.MinorUnits > allocation.SupplierGst.MinorUnits || allocation.SupplierGst.MinorUnits > allocation.Gross.MinorUnits ||
			(allocation.Use != tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS && allocation.Use != tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE) ||
			(allocation.BasClassification != tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_CAPITAL && allocation.BasClassification != tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL && allocation.BasClassification != tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED) ||
			(allocation.CashFlowClassification != tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING && allocation.CashFlowClassification != tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_INVESTING && allocation.CashFlowClassification != tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING) {
			return invalid, ErrInvalidAllocation
		}
		amounts := AllocationAmounts{allocation.Gross.MinorUnits, allocation.SupplierGst.MinorUnits, allocation.EligibleGst.MinorUnits}
		if !addPurchaseAmount(&result.Gross, amounts.Gross) || !addPurchaseAmount(&result.SupplierGST, amounts.SupplierGST) || !addPurchaseAmount(&result.EligibleGST, amounts.EligibleGST) {
			return invalid, ErrInvalidAllocation
		}
		if !allocation.EligibilityReviewConfirmed || !allocation.EvidenceReviewConfirmed ||
			!purchaseReviewText(allocation.NoncreditableReason, 512, amounts.EligibleGST < amounts.SupplierGST) ||
			(allocation.Use == tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE &&
				(amounts.EligibleGST != 0 || allocation.BasClassification != tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED)) {
			return invalid, ErrPurchaseEligibility
		}
		if !validPurchaseAccount(input.Accounts[allocation.AccountId], input.OrganisationID, allocation) {
			return invalid, ErrPurchaseAccount
		}
		result.Allocations[i] = amounts
	}
	if result.Gross != gross || result.SupplierGST != candidate.Gst.MinorUnits {
		return invalid, ErrInvalidAllocation
	}
	if input.Evidence != nil && (!input.Evidence.RegistrationReviewConfirmed || !input.Evidence.BusinessUseReviewConfirmed) {
		return invalid, ErrPurchaseEligibility
	}
	if err := ValidatePurchaseEvidence(input.Evidence, input.ExceptionRule); err != nil {
		return invalid, err
	}
	return result, nil
}

func validPurchaseAccount(account *tammyv1.Account, organisationID string, allocation *tammyv1.PurchaseAllocationInput) bool {
	if accounting.ValidateManualPosting(account) != nil || account.Id != allocation.AccountId || account.OrganisationId != organisationID ||
		account.Version != allocation.AccountVersion || accounting.IsCashAccount(account) {
		return false
	}
	business := allocation.Use == tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS
	private := allocation.Use == tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE
	capital := allocation.BasClassification == tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_CAPITAL
	noncapital := allocation.BasClassification == tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL
	excluded := allocation.BasClassification == tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED
	operating := allocation.CashFlowClassification == tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING
	investing := allocation.CashFlowClassification == tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_INVESTING
	financing := allocation.CashFlowClassification == tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING
	switch account.Type {
	case tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, tammyv1.AccountType_ACCOUNT_TYPE_OTHER_EXPENSE:
		return business && noncapital && operating
	case tammyv1.AccountType_ACCOUNT_TYPE_ASSET:
		// These two subtype strings are a local purchase convention, not a
		// global chart registry. Generic assets cannot become private receivables.
		if account.GetSubtype() == "owner_receivable" || account.GetSubtype() == "director_receivable" {
			return private && excluded && financing && (account.ReportClassification == "custom.account" || account.ReportClassification == "balance_sheet.receivables")
		}
		// Explicit capital review supplies purpose for Chart-created assets.
		// Specialised financial/control metadata cannot be reclassified here.
		return business && capital && investing && account.GetSubtype() == "" && account.ReportClassification == "custom.account"
	case tammyv1.AccountType_ACCOUNT_TYPE_EQUITY:
		return private && excluded && financing
	default:
		return false
	}
}

func validPurchaseMoney(value *tammyv1.Money) bool {
	return value != nil && value.CurrencyCode == "AUD" && value.MinorUnits >= 0
}

func addPurchaseAmount(total *int64, amount int64) bool {
	if amount < 0 || *total < 0 || amount > math.MaxInt64-*total {
		return false
	}
	*total += amount
	return true
}

func validPurchaseDate(value *tammyv1.CivilDate) bool {
	if value == nil || value.Year < 1 || value.Year > 9999 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return false
	}
	date := time.Date(int(value.Year), time.Month(value.Month), int(value.Day), 0, 0, 0, 0, time.UTC)
	return date.Year() == int(value.Year) && date.Month() == time.Month(value.Month) && date.Day() == int(value.Day)
}

func purchaseReviewText(value string, maximum int, required bool) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum && strings.TrimSpace(value) == value && (!required || value != "")
}
