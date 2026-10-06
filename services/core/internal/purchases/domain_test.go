package purchases

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const purchaseOrgID = "018f0000-0000-7000-8000-000000000020"
const purchaseDocID = "018f0000-0000-7000-8000-000000000900"
const purchaseAccountID = "018f0000-0000-7000-8000-000000000901"

func purchaseMoney(amount int64) *tammyv1.Money {
	return &tammyv1.Money{MinorUnits: amount, CurrencyCode: "AUD"}
}
func purchaseDate(day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: 2026, Month: 9, Day: day}
}
func purchaseSource(id string) *tammyv1.PurchaseRetainedSourceRef {
	return &tammyv1.PurchaseRetainedSourceRef{SourceId: id, SourceVersion: 2, SourceHash: make([]byte, 32)}
}
func purchaseEvidence() *tammyv1.PurchaseEvidenceInput {
	return &tammyv1.PurchaseEvidenceInput{Source: purchaseSource(purchaseDocID), Classification: tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_TAX_INVOICE,
		HeldDate: purchaseDate(30), RegistrationReviewConfirmed: true, BusinessUseReviewConfirmed: true, EvidenceReviewConfirmed: true, Reason: "Reviewed supplier tax invoice"}
}
func reviewedPurchaseFixture() ReviewedPurchaseInput {
	account := &tammyv1.Account{Id: purchaseAccountID, OrganisationId: purchaseOrgID, Version: 1,
		Code: "6001", Name: "Office supplies", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "noncash"}
	return ReviewedPurchaseInput{OrganisationID: purchaseOrgID, DocumentID: purchaseDocID, ReviewedVersion: 2,
		Document: &tammyv1.Document{Id: purchaseDocID, OrganisationId: purchaseOrgID, Version: 2,
			Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, Sha256: make([]byte, 32),
			ReviewedAt: timestamppb.New(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)),
			Candidate:  &tammyv1.DocumentCandidate{SupplierName: "Supplier", InvoiceNumber: "INV-1", DocumentDate: purchaseDate(30), Subtotal: purchaseMoney(10000), Gst: purchaseMoney(1000), Total: purchaseMoney(11000)}},
		Allocations: []*tammyv1.PurchaseAllocationInput{{AccountId: purchaseAccountID, AccountVersion: 1, Sequence: 1,
			Gross: purchaseMoney(11000), SupplierGst: purchaseMoney(1000), EligibleGst: purchaseMoney(1000),
			Use: tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS, BasClassification: tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL,
			CashFlowClassification:     tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING,
			EligibilityReviewConfirmed: true, EvidenceReviewConfirmed: true}},
		Accounts: map[string]*tammyv1.Account{purchaseAccountID: account}, GSTBasis: tammyv1.GstBasis_GST_BASIS_CASH,
		GSTProfileVersion: 1, Evidence: purchaseEvidence()}
}

func TestReviewedPurchaseExactArithmeticZeroMaximumAndEligibility(t *testing.T) {
	for _, test := range []struct {
		name                      string
		gross, supplier, eligible int64
		basis                     tammyv1.GstBasis
	}{
		{"ordinary", 11000, 1000, 1000, tammyv1.GstBasis_GST_BASIS_CASH},
		{"zero", 0, 0, 0, tammyv1.GstBasis_GST_BASIS_UNSPECIFIED},
		{"no GST unregistered", 10000, 0, 0, tammyv1.GstBasis_GST_BASIS_UNSPECIFIED},
		{"maximum valid source", math.MaxInt64, 1, 1, tammyv1.GstBasis_GST_BASIS_NON_CASH},
		{"partially creditable", 11000, 1000, 500, tammyv1.GstBasis_GST_BASIS_CASH},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := reviewedPurchaseFixture()
			input.Document.Candidate.Subtotal = purchaseMoney(test.gross - test.supplier)
			input.Document.Candidate.Gst = purchaseMoney(test.supplier)
			input.Document.Candidate.Total = purchaseMoney(test.gross)
			input.Allocations[0].Gross = purchaseMoney(test.gross)
			input.Allocations[0].SupplierGst = purchaseMoney(test.supplier)
			input.Allocations[0].EligibleGst = purchaseMoney(test.eligible)
			input.Allocations[0].NoncreditableReason = "Reviewed noncreditable portion"
			input.GSTBasis = test.basis
			result, err := ValidateReviewedPurchase(input)
			if err != nil || result.Gross != test.gross || result.SupplierGST != test.supplier || result.EligibleGST != test.eligible || len(result.Allocations) != 1 || result.Allocations[0] != (AllocationAmounts{test.gross, test.supplier, test.eligible}) {
				t.Fatalf("exact retained amounts: result=%+v error=%v", result, err)
			}
		})
	}
	input := reviewedPurchaseFixture()
	input.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	input.Evidence.Source, input.Evidence.HeldDate = nil, nil
	result, err := ValidateReviewedPurchase(input)
	if err != nil || result.EligibleGST != 1000 {
		t.Fatalf("known entitlement must survive missing evidence: result=%+v error=%v", result, err)
	}
	for _, count := range []int{50, 51} {
		input := reviewedPurchaseFixture()
		input.Document.Candidate.Subtotal, input.Document.Candidate.Gst, input.Document.Candidate.Total = purchaseMoney(0), purchaseMoney(0), purchaseMoney(0)
		input.Allocations = make([]*tammyv1.PurchaseAllocationInput, count)
		for i := range input.Allocations {
			allocation := proto.Clone(reviewedPurchaseFixture().Allocations[0]).(*tammyv1.PurchaseAllocationInput)
			allocation.Sequence = uint32(i + 1)
			allocation.Gross, allocation.SupplierGst, allocation.EligibleGst = purchaseMoney(0), purchaseMoney(0), purchaseMoney(0)
			input.Allocations[i] = allocation
		}
		_, err := ValidateReviewedPurchase(input)
		if count == 50 && err != nil || count == 51 && !errors.Is(err, ErrInvalidAllocation) {
			t.Fatalf("count=%d error=%v", count, err)
		}
	}
}

func TestReviewedPurchaseRejectsInvalidSourceAllocationAndEligibility(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ReviewedPurchaseInput)
		want   error
	}{
		{"missing document", func(v *ReviewedPurchaseInput) { v.Document = nil }, ErrInvalidReviewedPurchase},
		{"unreviewed", func(v *ReviewedPurchaseInput) {
			v.Document.Status = tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW
		}, ErrInvalidReviewedPurchase},
		{"stale source", func(v *ReviewedPurchaseInput) { v.ReviewedVersion++ }, ErrInvalidReviewedPurchase},
		{"foreign source", func(v *ReviewedPurchaseInput) { v.Document.OrganisationId = purchaseDocID }, ErrInvalidReviewedPurchase},
		{"wrong source", func(v *ReviewedPurchaseInput) { v.DocumentID = purchaseAccountID }, ErrInvalidReviewedPurchase},
		{"bad organisation", func(v *ReviewedPurchaseInput) { v.OrganisationID = "bad" }, ErrInvalidReviewedPurchase},
		{"missing review time", func(v *ReviewedPurchaseInput) { v.Document.ReviewedAt = nil }, ErrInvalidReviewedPurchase},
		{"missing hash", func(v *ReviewedPurchaseInput) { v.Document.Sha256 = nil }, ErrInvalidReviewedPurchase},
		{"missing candidate", func(v *ReviewedPurchaseInput) { v.Document.Candidate = nil }, ErrInvalidReviewedPurchase},
		{"missing date", func(v *ReviewedPurchaseInput) { v.Document.Candidate.DocumentDate = nil }, ErrInvalidReviewedPurchase},
		{"invalid calendar", func(v *ReviewedPurchaseInput) {
			v.Document.Candidate.DocumentDate = &tammyv1.CivilDate{Year: 2026, Month: 2, Day: 29}
		}, ErrInvalidReviewedPurchase},
		{"missing money", func(v *ReviewedPurchaseInput) { v.Document.Candidate.Gst = nil }, ErrInvalidReviewedPurchase},
		{"foreign money", func(v *ReviewedPurchaseInput) { v.Document.Candidate.Subtotal.CurrencyCode = "USD" }, ErrInvalidReviewedPurchase},
		{"negative money", func(v *ReviewedPurchaseInput) { v.Document.Candidate.Gst.MinorUnits = -1 }, ErrInvalidReviewedPurchase},
		{"arithmetic mismatch", func(v *ReviewedPurchaseInput) { v.Document.Candidate.Subtotal.MinorUnits++ }, ErrInvalidReviewedPurchase},
		{"subtotal overflow", func(v *ReviewedPurchaseInput) {
			v.Document.Candidate.Subtotal.MinorUnits = math.MaxInt64
			v.Document.Candidate.Gst.MinorUnits = 1
		}, ErrInvalidReviewedPurchase},
		{"missing allocations", func(v *ReviewedPurchaseInput) { v.Allocations = nil }, ErrInvalidAllocation},
		{"nil allocation", func(v *ReviewedPurchaseInput) { v.Allocations[0] = nil }, ErrInvalidAllocation},
		{"unordered", func(v *ReviewedPurchaseInput) { v.Allocations[0].Sequence = 2 }, ErrInvalidAllocation},
		{"missing allocation money", func(v *ReviewedPurchaseInput) { v.Allocations[0].Gross = nil }, ErrInvalidAllocation},
		{"foreign allocation money", func(v *ReviewedPurchaseInput) { v.Allocations[0].EligibleGst.CurrencyCode = "USD" }, ErrInvalidAllocation},
		{"negative allocation", func(v *ReviewedPurchaseInput) { v.Allocations[0].EligibleGst.MinorUnits = -1 }, ErrInvalidAllocation},
		{"gross mismatch", func(v *ReviewedPurchaseInput) { v.Allocations[0].Gross.MinorUnits++ }, ErrInvalidAllocation},
		{"supplier mismatch", func(v *ReviewedPurchaseInput) { v.Allocations[0].SupplierGst.MinorUnits-- }, ErrInvalidAllocation},
		{"supplier above gross", func(v *ReviewedPurchaseInput) { v.Allocations[0].SupplierGst.MinorUnits = 11001 }, ErrInvalidAllocation},
		{"eligible above supplier", func(v *ReviewedPurchaseInput) { v.Allocations[0].EligibleGst.MinorUnits = 1001 }, ErrInvalidAllocation},
		{"sum overflow", func(v *ReviewedPurchaseInput) {
			v.Allocations[0].Gross.MinorUnits = math.MaxInt64
			second := proto.Clone(v.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
			second.Sequence = 2
			second.Gross = purchaseMoney(1)
			second.SupplierGst = purchaseMoney(0)
			second.EligibleGst = purchaseMoney(0)
			v.Allocations = append(v.Allocations, second)
		}, ErrInvalidAllocation},
		{"missing use", func(v *ReviewedPurchaseInput) { v.Allocations[0].Use = 0 }, ErrInvalidAllocation},
		{"unknown BAS", func(v *ReviewedPurchaseInput) { v.Allocations[0].BasClassification = 99 }, ErrInvalidAllocation},
		{"missing cashflow", func(v *ReviewedPurchaseInput) { v.Allocations[0].CashFlowClassification = 0 }, ErrInvalidAllocation},
		{"eligibility unreviewed", func(v *ReviewedPurchaseInput) { v.Allocations[0].EligibilityReviewConfirmed = false }, ErrPurchaseEligibility},
		{"allocation evidence unreviewed", func(v *ReviewedPurchaseInput) { v.Allocations[0].EvidenceReviewConfirmed = false }, ErrPurchaseEligibility},
		{"registration unreviewed", func(v *ReviewedPurchaseInput) { v.Evidence.RegistrationReviewConfirmed = false }, ErrPurchaseEligibility},
		{"business unreviewed", func(v *ReviewedPurchaseInput) { v.Evidence.BusinessUseReviewConfirmed = false }, ErrPurchaseEligibility},
		{"noncreditable reason missing", func(v *ReviewedPurchaseInput) { v.Allocations[0].EligibleGst.MinorUnits = 0 }, ErrPurchaseEligibility},
		{"noncreditable reason whitespace", func(v *ReviewedPurchaseInput) {
			v.Allocations[0].EligibleGst.MinorUnits = 0
			v.Allocations[0].NoncreditableReason = " "
		}, ErrPurchaseEligibility},
		{"private credit", func(v *ReviewedPurchaseInput) { v.Allocations[0].Use = tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE }, ErrPurchaseEligibility},
		{"positive supplier missing basis even no entitlement", func(v *ReviewedPurchaseInput) {
			v.GSTBasis = 0
			v.Allocations[0].EligibleGst.MinorUnits = 0
			v.Allocations[0].NoncreditableReason = "No credit"
		}, ErrPurchaseEligibility},
		{"unknown basis", func(v *ReviewedPurchaseInput) { v.GSTBasis = 99 }, ErrPurchaseEligibility},
		{"missing profile version", func(v *ReviewedPurchaseInput) { v.GSTProfileVersion = 0 }, ErrPurchaseEligibility},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := reviewedPurchaseFixture()
			test.mutate(&input)
			if _, err := ValidateReviewedPurchase(input); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestPurchaseEligibilityChartReachableAccountsAndPurposeExclusions(t *testing.T) {
	for _, test := range []struct {
		name            string
		kind            tammyv1.AccountType
		subtype, report string
		use             tammyv1.PurchaseUse
		bas             tammyv1.PurchaseBasClassification
		flow            tammyv1.PurchaseCashFlowClassification
		accepted        bool
	}{
		{"Chart expense", tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, "", "custom.account", 1, 2, 1, true},
		{"Chart other expense", tammyv1.AccountType_ACCOUNT_TYPE_OTHER_EXPENSE, "", "custom.account", 1, 2, 1, true},
		{"Chart capital asset", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "", "custom.account", 1, 1, 2, true},
		{"Chart private equity", tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, "", "custom.account", 2, 3, 3, true},
		{"owner receivable", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "owner_receivable", "balance_sheet.receivables", 2, 3, 3, true},
		{"director receivable", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "director_receivable", "custom.account", 2, 3, 3, true},
		{"expense capital", tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, "", "custom.account", 1, 1, 2, false},
		{"expense private", tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, "", "custom.account", 2, 3, 3, false},
		{"expense investing", tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, "", "custom.account", 1, 2, 2, false},
		{"generic asset noncapital", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "", "custom.account", 1, 2, 1, false},
		{"generic asset private", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "", "custom.account", 2, 3, 3, false},
		{"capital operating", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "", "custom.account", 1, 1, 1, false},
		{"trade receivable capital", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "trade_receivable", "balance_sheet.receivables", 1, 1, 2, false},
		{"financial asset capital", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "financial_asset", "custom.account", 1, 1, 2, false},
		{"untyped trade report capital", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "", "balance_sheet.receivables", 1, 1, 2, false},
		{"owner receivable capital", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "owner_receivable", "custom.account", 1, 1, 2, false},
		{"director receivable business", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "director_receivable", "balance_sheet.receivables", 1, 1, 2, false},
		{"equity business", tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, "", "custom.account", 1, 1, 2, false},
		{"private nonexcluded", tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, "", "custom.account", 2, 2, 3, false},
		{"private operating", tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, "", "custom.account", 2, 3, 1, false},
		{"revenue", tammyv1.AccountType_ACCOUNT_TYPE_REVENUE, "", "custom.account", 1, 2, 1, false},
		{"other revenue", tammyv1.AccountType_ACCOUNT_TYPE_OTHER_REVENUE, "", "custom.account", 1, 2, 1, false},
		{"liability", tammyv1.AccountType_ACCOUNT_TYPE_LIABILITY, "", "custom.account", 1, 1, 2, false},
		{"contra", tammyv1.AccountType_ACCOUNT_TYPE_CONTRA, "", "custom.account", 2, 3, 3, false},
		{"ordinary GST control purpose", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "", "balance_sheet.gst_current", 1, 1, 2, false},
		{"cash equivalent", tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "", "balance_sheet.cash_equivalent", 1, 1, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := reviewedPurchaseFixture()
			account := input.Accounts[purchaseAccountID]
			account.Type = test.kind
			account.ReportClassification = test.report
			if test.subtype != "" {
				account.Subtype = proto.String(test.subtype)
			}
			if test.kind == tammyv1.AccountType_ACCOUNT_TYPE_EQUITY || test.kind == tammyv1.AccountType_ACCOUNT_TYPE_REVENUE || test.kind == tammyv1.AccountType_ACCOUNT_TYPE_OTHER_REVENUE || test.kind == tammyv1.AccountType_ACCOUNT_TYPE_LIABILITY {
				account.NormalBalance = tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
			}
			allocation := input.Allocations[0]
			allocation.Use = test.use
			allocation.BasClassification = test.bas
			allocation.CashFlowClassification = test.flow
			if test.use == tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE {
				allocation.EligibleGst = purchaseMoney(0)
				allocation.NoncreditableReason = "Private purchase"
			}
			_, err := ValidateReviewedPurchase(input)
			if test.accepted && err != nil || !test.accepted && err == nil {
				t.Fatalf("accepted=%v error=%v", test.accepted, err)
			}
			if !test.accepted {
				want := ErrPurchaseAccount
				if test.use == tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE && test.bas != tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED {
					want = ErrPurchaseEligibility
				}
				if !errors.Is(err, want) {
					t.Fatalf("error=%v want=%v", err, want)
				}
			}
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(*ReviewedPurchaseInput)
	}{
		{"missing", func(v *ReviewedPurchaseInput) { delete(v.Accounts, purchaseAccountID) }},
		{"wrong map identity", func(v *ReviewedPurchaseInput) { v.Accounts[purchaseAccountID].Id = purchaseDocID }},
		{"foreign", func(v *ReviewedPurchaseInput) { v.Accounts[purchaseAccountID].OrganisationId = purchaseDocID }},
		{"archived", func(v *ReviewedPurchaseInput) {
			v.Accounts[purchaseAccountID].Status = tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED
		}},
		{"stale", func(v *ReviewedPurchaseInput) { v.Allocations[0].AccountVersion++ }},
		{"cash", func(v *ReviewedPurchaseInput) {
			v.Accounts[purchaseAccountID].ReportClassification = "balance_sheet.cash"
		}},
		{"wrong normal", func(v *ReviewedPurchaseInput) {
			v.Accounts[purchaseAccountID].NormalBalance = tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
		}},
		{"protected", func(v *ReviewedPurchaseInput) {
			v.Accounts[purchaseAccountID].Designation = tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := reviewedPurchaseFixture()
			test.mutate(&input)
			if _, err := ValidateReviewedPurchase(input); !errors.Is(err, ErrPurchaseAccount) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestReviewedPurchaseMixedOrderedAllocationsRetainDecisions(t *testing.T) {
	input := reviewedPurchaseFixture()
	input.Allocations = make([]*tammyv1.PurchaseAllocationInput, 3)
	for i, amounts := range []AllocationAmounts{{5500, 500, 500}, {3300, 300, 100}, {2200, 200, 0}} {
		allocation := proto.Clone(reviewedPurchaseFixture().Allocations[0]).(*tammyv1.PurchaseAllocationInput)
		allocation.Sequence = uint32(i + 1)
		allocation.Gross, allocation.SupplierGst, allocation.EligibleGst = purchaseMoney(amounts.Gross), purchaseMoney(amounts.SupplierGST), purchaseMoney(amounts.EligibleGST)
		if i > 0 {
			account := proto.Clone(input.Accounts[purchaseAccountID]).(*tammyv1.Account)
			account.Id = []string{"", "018f0000-0000-7000-8000-000000000910", "018f0000-0000-7000-8000-000000000911"}[i]
			allocation.AccountId = account.Id
			allocation.NoncreditableReason = "Explicit reviewed noncreditable portion"
			if i == 1 {
				account.Type = tammyv1.AccountType_ACCOUNT_TYPE_ASSET
				allocation.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_CAPITAL
				allocation.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_INVESTING
			}
			if i == 2 {
				account.Type = tammyv1.AccountType_ACCOUNT_TYPE_EQUITY
				account.NormalBalance = tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
				allocation.Use = tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE
				allocation.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED
				allocation.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING
			}
			input.Accounts[account.Id] = account
		}
		input.Allocations[i] = allocation
	}
	before := make([]*tammyv1.PurchaseAllocationInput, len(input.Allocations))
	for i, allocation := range input.Allocations {
		before[i] = proto.Clone(allocation).(*tammyv1.PurchaseAllocationInput)
	}
	got, err := ValidateReviewedPurchase(input)
	if err != nil || got.Gross != 11000 || got.SupplierGST != 1000 || got.EligibleGST != 600 {
		t.Fatalf("mixed purchase=%+v error=%v", got, err)
	}
	for i, allocation := range input.Allocations {
		if !proto.Equal(before[i], allocation) {
			t.Fatal("domain validation changed retained decisions")
		}
	}
	delta, err := PaymentAllocationDeltas(got.Allocations, 0, 5500, got.Gross)
	want := []AllocationAmounts{{2750, 250, 250}, {1650, 150, 50}, {1100, 100, 0}}
	if err != nil || len(delta) != len(want) {
		t.Fatalf("mixed payment=%v error=%v", delta, err)
	}
	for i := range want {
		if delta[i] != want[i] {
			t.Fatalf("mixed payment=%v want=%v", delta, want)
		}
	}
}

func TestPurchaseEligibilityZeroGSTPrivateAndPublicVersions(t *testing.T) {
	input := reviewedPurchaseFixture()
	account := input.Accounts[purchaseAccountID]
	account.Type = tammyv1.AccountType_ACCOUNT_TYPE_EQUITY
	account.NormalBalance = tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
	input.Document.Candidate.Subtotal, input.Document.Candidate.Gst = purchaseMoney(11000), purchaseMoney(0)
	allocation := input.Allocations[0]
	allocation.Use = tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE
	allocation.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED
	allocation.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING
	allocation.SupplierGst, allocation.EligibleGst = purchaseMoney(0), purchaseMoney(0)
	input.GSTBasis = tammyv1.GstBasis_GST_BASIS_UNSPECIFIED
	// No supplier GST is noncreditable, so the public contract needs no reason.
	if got, err := ValidateReviewedPurchase(input); err != nil || got.Gross != 11000 || got.EligibleGST != 0 {
		t.Fatalf("zero GST private source: result=%+v error=%v", got, err)
	}
	// Public uint64 metadata stays intact; signed SQL bounds are the later
	// persistence owner's responsibility, independently of exact source money.
	input.GSTProfileVersion, input.GSTHistoryWatermark = math.MaxUint64, math.MaxUint64
	input.ReviewedVersion, input.Document.Version = math.MaxUint64, math.MaxUint64
	account.Version, allocation.AccountVersion = math.MaxUint64, math.MaxUint64
	if _, err := ValidateReviewedPurchase(input); err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseEligibilityRejectsEveryInstalledProtectedAccount(t *testing.T) {
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range template.Accounts {
		t.Run(entry.Key, func(t *testing.T) {
			input := reviewedPurchaseFixture()
			input.Allocations[0].AccountId = entry.ID
			input.Accounts = map[string]*tammyv1.Account{entry.ID: {Id: entry.ID, OrganisationId: purchaseOrgID,
				Version: 1, Code: entry.Code, Name: entry.Name, Type: entry.Type, NormalBalance: entry.NormalBalance,
				Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: entry.Designation,
				ReportClassification: entry.ReportClassification, CashFlowClassification: entry.CashFlowClassification}}
			if _, err := ValidateReviewedPurchase(input); !errors.Is(err, ErrPurchaseAccount) {
				t.Fatalf("protected %s accepted: %v", entry.Code, err)
			}
		})
	}
}
