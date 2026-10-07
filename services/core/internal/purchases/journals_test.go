package purchases

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func purchaseJournalFixture(t *testing.T) (JournalSpec, ReviewedPurchaseInput) {
	t.Helper()
	input := reviewedPurchaseFixture()
	accounts := map[string]*tammyv1.Account{purchaseAccountID: input.Accounts[purchaseAccountID]}
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range template.Accounts {
		accounts[account.ID] = &tammyv1.Account{Id: account.ID, OrganisationId: purchaseOrgID, Version: 1, Code: account.Code, Name: account.Name, Type: account.Type, NormalBalance: account.NormalBalance, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: account.Designation, ReportClassification: account.ReportClassification, CashFlowClassification: account.CashFlowClassification}
	}
	next := 700
	return JournalSpec{OrganisationID: purchaseOrgID, SourceID: purchaseDocID, SourceRevision: 1, FinancialRevision: 1, Date: proto.Clone(input.Document.Candidate.DocumentDate).(*tammyv1.CivilDate), Memo: "Purchase", Accounts: accounts, Now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), NewID: func() (string, error) { next++; return fmt.Sprintf("018f0000-0000-7000-8000-%012d", next), nil }}, input
}

func controlID(spec JournalSpec, code string) string {
	for id, account := range spec.Accounts {
		if account.Code == code {
			return id
		}
	}
	return ""
}
func journalMovement(journal *tammyv1.Journal, accountID string) int64 {
	var amount int64
	for _, line := range journal.GetLines() {
		if line.AccountId == accountID {
			amount += line.Debit.MinorUnits - line.Credit.MinorUnits
		}
	}
	return amount
}

func TestPurchaseJournalPostingKeepsCostAndEligibleGSTSeparate(t *testing.T) {
	for _, test := range []struct {
		basis     tammyv1.GstBasis
		readiness EvidenceReadiness
		control   string
	}{
		{tammyv1.GstBasis_GST_BASIS_CASH, EvidenceNormalSupported, "1160"},
		{tammyv1.GstBasis_GST_BASIS_CASH, EvidenceMissing, "1160"},
		{tammyv1.GstBasis_GST_BASIS_NON_CASH, EvidenceNormalSupported, "1150"},
		{tammyv1.GstBasis_GST_BASIS_NON_CASH, EvidenceMissing, "1170"},
		{tammyv1.GstBasis_GST_BASIS_NON_CASH, EvidenceTimingReviewRequired, "1170"},
		{tammyv1.GstBasis_GST_BASIS_NON_CASH, EvidenceFuture, "1170"},
	} {
		spec, input := purchaseJournalFixture(t)
		input.GSTBasis = test.basis
		input.Allocations[0].EligibleGst.MinorUnits = 700
		input.Allocations[0].NoncreditableReason = "Private portion excluded"
		journal, flows, err := BuildPurchaseJournal(spec, input, test.readiness)
		if err != nil {
			t.Fatal(err)
		}
		if journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE || journalMovement(journal, purchaseAccountID) != 10300 || journalMovement(journal, controlID(spec, test.control)) != 700 || journalMovement(journal, controlID(spec, "2000")) != -11000 {
			t.Fatalf("wrong purchase journal: %v", journal)
		}
		assertPurchaseJournal(t, journal, spec.Accounts, flows, "")
	}
	spec, input := purchaseJournalFixture(t)
	for _, amount := range []*tammyv1.Money{input.Document.Candidate.Subtotal, input.Document.Candidate.Gst, input.Document.Candidate.Total, input.Allocations[0].Gross, input.Allocations[0].SupplierGst, input.Allocations[0].EligibleGst} {
		amount.MinorUnits = 0
	}
	journal, flows, err := BuildPurchaseJournal(spec, input, EvidenceNormalSupported)
	if err != nil || journal != nil || len(flows) != 0 {
		t.Fatalf("zero purchase = %v,%v,%v", journal, flows, err)
	}
}

func TestPurchaseJournalPaymentsConserveGSTAndCashFlowAcrossPartitions(t *testing.T) {
	spec, input := purchaseJournalFixture(t)
	posting := &tammyv1.PurchasePosting{OrganisationId: purchaseOrgID, Id: purchaseDocID, InvoiceDate: input.Document.Candidate.DocumentDate, GstBasis: tammyv1.GstBasis_GST_BASIS_CASH, Gross: input.Document.Candidate.Total, SupplierGst: input.Document.Candidate.Gst, EligibleGst: input.Allocations[0].EligibleGst, Allocations: input.Allocations}
	bank := spec.Accounts[controlID(spec, "1000")]
	var paid, transferred int64
	for _, payment := range []int64{1, 5499, 5500} {
		journal, flows, deltas, err := BuildPurchasePaymentJournal(spec, posting, paid, payment, bank, EvidenceNormalSupported)
		if err != nil {
			t.Fatal(err)
		}
		transferred += journalMovement(journal, controlID(spec, "1150"))
		if journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT || journalMovement(journal, bank.Id) != -payment || journalMovement(journal, controlID(spec, "2000")) != payment || journalMovement(journal, controlID(spec, "1160")) != -deltas[0].EligibleGST {
			t.Fatalf("payment wrong: %v", journal)
		}
		assertPurchaseJournal(t, journal, spec.Accounts, flows, bank.Id)
		paid += payment
	}
	if transferred != 1000 || paid != 11000 {
		t.Fatalf("conservation: paid=%d GST=%d", paid, transferred)
	}
	posting.GstBasis = tammyv1.GstBasis_GST_BASIS_NON_CASH
	journal, _, _, err := BuildPurchasePaymentJournal(spec, posting, 0, 11000, bank, EvidenceNormalSupported)
	if err != nil || len(journal.GetLines()) != 2 {
		t.Fatalf("noncash payment repeated claim: %v, %v", journal, err)
	}
	posting.GstBasis = tammyv1.GstBasis_GST_BASIS_CASH
	journal, flows, _, err := BuildPurchasePaymentJournal(spec, posting, 0, 5500, bank, EvidenceMissing)
	if err != nil || journalMovement(journal, controlID(spec, "1170")) != 500 || journalMovement(journal, controlID(spec, "1150")) != 0 {
		t.Fatalf("unsupported evidence payment = %v,%v", journal, err)
	}
	assertPurchaseJournal(t, journal, spec.Accounts, flows, bank.Id)
}

func TestPurchaseJournalMixedCashFlowUsesRetainedGrossDeltas(t *testing.T) {
	spec, input := purchaseJournalFixture(t)
	allocations := make([]*tammyv1.PurchaseAllocationInput, 3)
	for i := range allocations {
		allocations[i] = proto.Clone(input.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
		allocations[i].Sequence = uint32(i + 1)
		allocations[i].Gross = &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: int64(i+1) * 100}
		allocations[i].SupplierGst = &tammyv1.Money{CurrencyCode: "AUD"}
		allocations[i].EligibleGst = &tammyv1.Money{CurrencyCode: "AUD"}
		allocations[i].CashFlowClassification = tammyv1.PurchaseCashFlowClassification(i + 1)
	}
	posting := &tammyv1.PurchasePosting{OrganisationId: purchaseOrgID, InvoiceDate: spec.Date, GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH, Gross: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 600}, SupplierGst: &tammyv1.Money{CurrencyCode: "AUD"}, EligibleGst: &tammyv1.Money{CurrencyCode: "AUD"}, Allocations: allocations}
	bank := spec.Accounts[controlID(spec, "1000")]
	journal, flows, deltas, err := BuildPurchasePaymentJournal(spec, posting, 0, 300, bank, EvidenceNormalSupported)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range journal.Lines {
		if line.AccountId == bank.Id {
			components := flows[line.Id]
			if len(components) != 3 {
				t.Fatalf("mixed cash components = %v", components)
			}
			for i, component := range components {
				if component.Category != accounting.CashFlowCategory(i+1) || component.AmountMinor != -deltas[i].Gross {
					t.Fatalf("cash category/delta = %v", components)
				}
			}
		}
	}
	assertPurchaseJournal(t, journal, spec.Accounts, flows, bank.Id)
}

func TestPurchaseJournalReleaseAndControlProtection(t *testing.T) {
	spec, input := purchaseJournalFixture(t)
	journal, flows, err := BuildPurchaseGSTReleaseJournal(spec, 500)
	if err != nil || journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION || journalMovement(journal, controlID(spec, "1150")) != 500 || journalMovement(journal, controlID(spec, "1170")) != -500 {
		t.Fatalf("release = %v,%v", journal, err)
	}
	assertPurchaseJournal(t, journal, spec.Accounts, flows, "")
	for _, mutate := range []func(*tammyv1.Account){func(a *tammyv1.Account) { a.Designation = tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY }, func(a *tammyv1.Account) { a.ReportClassification = "custom.account" }, func(a *tammyv1.Account) { a.Status = tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED }, func(a *tammyv1.Account) { a.Type = tammyv1.AccountType_ACCOUNT_TYPE_ASSET }, func(a *tammyv1.Account) { a.OrganisationId = purchaseDocID }} {
		spec, input = purchaseJournalFixture(t)
		mutate(spec.Accounts[controlID(spec, "2000")])
		if _, _, err := BuildPurchaseJournal(spec, input, EvidenceNormalSupported); !errors.Is(err, accounting.ErrAccountNotPostable) {
			t.Fatalf("spoofed control accepted: %v", err)
		}
	}
	spec, input = purchaseJournalFixture(t)
	spec.FinancialRevision = math.MaxUint64
	if _, _, err := BuildPurchaseJournal(spec, input, EvidenceNormalSupported); err == nil {
		t.Fatal("SQL-overflow revision accepted")
	}
	spec, input = purchaseJournalFixture(t)
	input.Document.Candidate.Total.MinorUnits = math.MaxInt64
	input.Document.Candidate.Subtotal.MinorUnits = math.MaxInt64 - 1000
	input.Allocations[0].Gross.MinorUnits = math.MaxInt64
	posting := &tammyv1.PurchasePosting{OrganisationId: purchaseOrgID, InvoiceDate: spec.Date, GstBasis: tammyv1.GstBasis_GST_BASIS_CASH, Gross: input.Document.Candidate.Total, SupplierGst: input.Document.Candidate.Gst, EligibleGst: input.Allocations[0].EligibleGst, Allocations: input.Allocations}
	if _, _, _, err := BuildPurchasePaymentJournal(spec, posting, 0, math.MaxInt64, spec.Accounts[controlID(spec, "1000")], EvidenceNormalSupported); err == nil {
		t.Fatal("gross plus GST journal column overflow accepted")
	}
}

func TestPurchaseJournalRejectsProtectedBankOutsideInstalledBankControl(t *testing.T) {
	spec, input := purchaseJournalFixture(t)
	bank := spec.Accounts[controlID(spec, "1000")]
	bank.Code = "1001"
	posting := &tammyv1.PurchasePosting{OrganisationId: purchaseOrgID, InvoiceDate: spec.Date, GstBasis: input.GSTBasis, Gross: input.Document.Candidate.Total, SupplierGst: input.Document.Candidate.Gst, EligibleGst: input.Allocations[0].EligibleGst, Allocations: input.Allocations}
	if _, _, _, err := BuildPurchasePaymentJournal(spec, posting, 0, 5500, bank, EvidenceNormalSupported); err == nil {
		t.Fatal("non-bank protected control selected as payment bank")
	}
}

func assertPurchaseJournal(t *testing.T, journal *tammyv1.Journal, accounts map[string]*tammyv1.Account, flows map[string][]accounting.CashFlowComponent, bank string) {
	t.Helper()
	if err := accounting.ValidateJournal(journal, accounts, false); err != nil {
		t.Fatal(err)
	}
	for _, line := range journal.Lines {
		if err := accounting.ValidateCashFlowAllocation(line, line.AccountId == bank, flows[line.Id]); err != nil {
			t.Fatalf("cash facts for %s: %v", line.AccountId, err)
		}
	}
}
