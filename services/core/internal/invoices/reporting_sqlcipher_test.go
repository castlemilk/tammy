//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestInvoiceReportingCaptureCensusesDocumentsAndCompleteReceiptLineage(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
	period := invoiceReportingPeriod()

	// Keep one posted cash-basis invoice unpaid so the capture must retain the
	// invoice/posting even though it contributes no settlement events yet.
	unpaid := fixture.saveDraft(t, invoiceReportingID(1001), "UNPAID-001", "2026-10-01", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, unpaid)
	fixture.post(t, unpaid)

	// The ordinary invoice accounting read intentionally exposes at most 200
	// payment protos. The reporting census must keep every retained receipt.
	paid := fixture.saveDraft(t, invoiceReportingID(1002), "PAID-001", "2026-10-02", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, paid)
	fixture.post(t, paid)
	for index := 0; index < 201; index++ {
		fixture.recordPayment(t, paid, uint64(index+1), 1, index)
	}
	displayRepository, err := fixture.accountingRepository(t)
	if err != nil {
		t.Fatal(err)
	}
	display, err := displayRepository.Get(fixture.ctx, orgID, paid.Id)
	if err != nil {
		t.Fatalf("ordinary accounting display: %v", err)
	}
	if display == nil {
		t.Fatal("ordinary accounting display returned nil without an error")
	}
	if display.PaymentCount != 201 || len(display.Payments) != 200 {
		t.Fatalf("ordinary accounting display = count %d, rows %d; want bounded 200-row display", display.PaymentCount, len(display.Payments))
	}

	// An issued document without an income posting remains an explicit source.
	unposted := fixture.saveDraft(t, invoiceReportingID(1003), "UNPOSTED-001", "2026-10-03", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, unposted)

	// Drafts with dates before the activity period and drafts without dates are
	// still visible, with their period status stated rather than filtered away.
	olderDraft := fixture.saveDraft(t, invoiceReportingID(1004), "OLDER-DRAFT", "2026-09-30", 200,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	undatedDraft := fixture.saveDraft(t, invoiceReportingID(1005), "UNDATED-DRAFT", "", 200,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)

	// Include future activity in the same complete census as well.
	future := fixture.saveDraft(t, invoiceReportingID(1006), "FUTURE-ISSUED", "2027-01-01", 200,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, future)

	// A zero-total posting is retained with an absent journal, and its raw
	// unspecified GST basis remains visible. This changes only the fixture's
	// profile row; invoice/accounting sources remain written by real writers.
	fixture.setGSTBasis(t, tammyv1.GstBasis_GST_BASIS_UNSPECIFIED)
	zero := fixture.saveDraft(t, invoiceReportingID(1007), "ZERO-UNKNOWN-BASIS", "2026-10-31", 0,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, zero)
	fixture.post(t, zero)

	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort() = %v", err)
	}
	capture, err := port.CaptureBAS(fixture.ctx, orgID, period)
	if err != nil {
		t.Fatalf("CaptureBAS() = %v", err)
	}
	if capture == nil || len(capture.Candidates) != 7 || len(capture.Fingerprint) != sha256.Size {
		t.Fatalf("capture = %#v; want seven complete candidates and a SHA-256 fingerprint", capture)
	}
	assertInvoiceReportingCandidateSource(t, capture.Candidates)

	byID := invoiceReportingCandidatesByID(t, capture)
	for _, candidate := range []BASInvoiceCandidate{byID[unpaid.Id], byID[paid.Id], byID[unposted.Id], byID[olderDraft.Id], byID[undatedDraft.Id], byID[future.Id], byID[zero.Id]} {
		if candidate.ID == "" {
			t.Fatal("capture omitted one of the saved organisation invoices")
		}
	}
	if byID[olderDraft.Id].PeriodDisposition != "OUT_OF_PERIOD" ||
		byID[future.Id].PeriodDisposition != "OUT_OF_PERIOD" ||
		byID[undatedDraft.Id].PeriodDisposition != "UNDATED" ||
		byID[unpaid.Id].PeriodDisposition != "IN_PERIOD" ||
		byID[paid.Id].PeriodDisposition != "IN_PERIOD" ||
		byID[unposted.Id].PeriodDisposition != "IN_PERIOD" ||
		byID[zero.Id].PeriodDisposition != "IN_PERIOD" {
		t.Fatalf("invoice period dispositions lost older, future, undated, or in-period sources: %#v", byID)
	}
	if byID[olderDraft.Id].State != "DRAFT" || byID[undatedDraft.Id].State != "DRAFT" ||
		byID[undatedDraft.Id].Invoice.Draft.IssueDate != "" {
		t.Fatalf("draft invoice sources were hidden or assigned a fabricated date: older=%#v undated=%#v", byID[olderDraft.Id], byID[undatedDraft.Id])
	}
	if byID[future.Id].State != "ISSUED" || byID[future.Id].Posting != nil {
		t.Fatalf("issued but unposted invoice was lost or fabricated a posting: %#v", byID[future.Id])
	}

	unpaidSource := byID[unpaid.Id]
	if unpaidSource.Posting == nil || int32(unpaidSource.Posting.GSTBasis) != int32(tammyv1.GstBasis_GST_BASIS_CASH) ||
		unpaidSource.Posting.InvoiceTotalMinor <= 0 || len(unpaidSource.Receipts) != 0 {
		t.Fatalf("unpaid cash-basis invoice was not retained as a posted source with empty receipt history: %#v", unpaidSource)
	}
	assertInvoiceReportingPostingJournal(t, unpaidSource, "RECEIVABLE", unpaid.Id, unpaid.Version)

	zeroSource := byID[zero.Id]
	if zeroSource.Posting == nil || zeroSource.Posting.InvoiceTotalMinor != 0 ||
		int32(zeroSource.Posting.GSTBasis) != int32(tammyv1.GstBasis_GST_BASIS_UNSPECIFIED) ||
		zeroSource.Posting.JournalID != nil || zeroSource.Posting.Journal != nil || len(zeroSource.Receipts) != 0 {
		t.Fatalf("zero-total posting must remain distinct from an unposted invoice and retain its unknown basis: %#v", zeroSource)
	}
	if len(zeroSource.Posting.IncomeLines) != 1 || zeroSource.Posting.IncomeLines[0].Sequence != 1 ||
		zeroSource.Posting.IncomeLines[0].GrossMinor != 0 || zeroSource.Posting.IncomeLines[0].NetMinor != 0 ||
		zeroSource.Posting.IncomeLines[0].GSTMinor != 0 {
		t.Fatalf("zero-total posting line provenance = %#v", zeroSource.Posting.IncomeLines)
	}

	paidSource := byID[paid.Id]
	if paidSource.Posting == nil || len(paidSource.Receipts) != 201 {
		t.Fatalf("capture lost historical receipts beyond the accounting display window: posting=%#v receipt count=%d", paidSource.Posting, len(paidSource.Receipts))
	}
	assertInvoiceReportingPostingJournal(t, paidSource, "RECEIVABLE", paid.Id, paid.Version)
	seenReceiptIDs := make(map[string]bool, len(paidSource.Receipts))
	var capturedReceiptMinor int64
	for _, receipt := range paidSource.Receipts {
		if receipt.ID == "" || seenReceiptIDs[receipt.ID] {
			t.Fatalf("receipt identity missing or repeated: %#v", receipt)
		}
		seenReceiptIDs[receipt.ID] = true
		if receipt.OrganisationID != orgID || receipt.InvoiceID != paid.Id || receipt.PaymentDate != "2026-10-12" ||
			receipt.AmountMinor != 1 || receipt.BankAccountID != templateBankID() || receipt.Reference == "" ||
			receipt.JournalID == "" || receipt.CreatedAt == "" || receipt.Payment == nil ||
			receipt.Payment.Id != receipt.ID || receipt.Payment.OrganisationId != orgID || receipt.Payment.InvoiceId != paid.Id ||
			receipt.Payment.Amount == nil || receipt.Payment.Amount.MinorUnits != receipt.AmountMinor ||
			receipt.Payment.AllocatedGst == nil || receipt.Payment.AllocatedGst.MinorUnits != receipt.AllocatedGSTMinor ||
			receipt.Payment.BasSalesGross == nil || receipt.Payment.BasSalesGross.MinorUnits != receipt.BASSalesGrossMinor ||
			receipt.Payment.BankAccountId != receipt.BankAccountID || receipt.Payment.JournalId != receipt.JournalID ||
			receipt.Payment.Reference != receipt.Reference {
			t.Fatalf("receipt raw row and payment proto disagree: %#v", receipt)
		}
		if len(receipt.PaymentProto) == 0 || len(receipt.PaymentHash) != sha256.Size || len(receipt.SourceHash) != sha256.Size {
			t.Fatalf("receipt omitted exact payment bytes, computed payload hash, or retained SQL source hash: %#v", receipt)
		}
		encodedPayment, err := (proto.MarshalOptions{Deterministic: true}).Marshal(receipt.Payment)
		if err != nil || !bytes.Equal(receipt.PaymentProto, encodedPayment) {
			t.Fatalf("receipt protobuf payload was not preserved exactly: error=%v", err)
		}
		paymentHash := sha256.Sum256(receipt.PaymentProto)
		if !bytes.Equal(receipt.PaymentHash, paymentHash[:]) || !bytes.Equal(receipt.SourceHash, paymentHash[:]) {
			t.Fatalf("receipt hashes lost payload or retained source lineage: payment=%x stored=%x", receipt.PaymentHash, receipt.SourceHash)
		}
		if len(receipt.Allocations) != 1 || receipt.Allocations[0].Sequence != 1 ||
			receipt.Allocations[0].GrossMinor != receipt.AmountMinor ||
			receipt.Allocations[0].GSTMinor < 0 || receipt.Allocations[0].GSTMinor > receipt.Allocations[0].GrossMinor {
			t.Fatalf("receipt allocation history is incomplete or unordered: %#v", receipt.Allocations)
		}
		capturedReceiptMinor += receipt.AmountMinor
		assertInvoiceReportingJournalProof(t, receipt.Journal, "BANKING", receipt.ID, 1,
			tammyv1.JournalSource_JOURNAL_SOURCE_INVOICE_PAYMENT)
	}
	if capturedReceiptMinor != 201 || len(seenReceiptIDs) != 201 {
		t.Fatalf("complete receipt history totals/count = %d/%d; want 201/201", capturedReceiptMinor, len(seenReceiptIDs))
	}

	repeated, err := port.CaptureBAS(fixture.ctx, orgID, period)
	if err != nil || repeated == nil || !bytes.Equal(capture.Fingerprint, repeated.Fingerprint) {
		t.Fatalf("repeat capture changed its fingerprint: first=%x repeat=%#v error=%v", capture.Fingerprint, repeated, err)
	}
	if !sameInvoiceCandidateOrder(capture.Candidates, repeated.Candidates) {
		t.Fatal("repeat capture changed deterministic invoice candidate order")
	}

	if err := fixture.tx.Commit(); err != nil {
		t.Fatalf("commit actual encrypted invoice sources: %v", err)
	}
	fixture.tx = nil
	if err := fixture.workspace.Database.Close(); err != nil {
		t.Fatalf("close SQLCipher workspace before reopen: %v", err)
	}
	reopened, err := sqlcipher.Open(fixture.ctx, fixture.workspace.Path, fixture.workspace.Key)
	if err != nil {
		t.Fatalf("reopen SQLCipher workspace: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reopenedPort, err := NewReportingPort(reopened)
	if err != nil {
		t.Fatalf("NewReportingPort(reopened) = %v", err)
	}
	afterReopen, err := reopenedPort.CaptureBAS(fixture.ctx, orgID, period)
	if err != nil {
		t.Fatalf("capture after reopen: %v", err)
	}
	if afterReopen == nil {
		t.Fatal("capture after reopen returned nil without an error")
	}
	if !bytes.Equal(capture.Fingerprint, afterReopen.Fingerprint) {
		t.Fatalf("committed encrypted capture changed after reopen: before=%x after=%x", capture.Fingerprint, afterReopen.Fingerprint)
	}
}

func TestInvoiceReportingCaptureRetainsReceiptDatesAroundPeriodForOlderInvoice(t *testing.T) {
	fixture := newInvoiceReportingFixtureAt(t, tammyv1.GstBasis_GST_BASIS_CASH,
		time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC))
	invoice := fixture.saveDraft(t, invoiceReportingID(1050), "OLDER-CASH-001", "2026-09-01", 201,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, invoice)
	fixture.post(t, invoice)

	wantDateCounts := map[string]int{
		"2026-09-30": 67,
		"2026-10-12": 67,
		"2026-11-01": 67,
	}
	for index := 0; index < 201; index++ {
		paymentDate := &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}
		if index >= 67 && index < 134 {
			paymentDate = &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 12}
		} else if index >= 134 {
			paymentDate = &tammyv1.CivilDate{Year: 2026, Month: 11, Day: 1}
		}
		fixture.recordPaymentOn(t, invoice, uint64(index+1), 1, index, paymentDate)
	}

	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort() = %v", err)
	}
	capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil {
		t.Fatalf("CaptureBAS() = %v", err)
	}
	if capture == nil || len(capture.Candidates) != 1 {
		t.Fatalf("capture = %#v; want the older invoice retained as one candidate", capture)
	}
	candidate := capture.Candidates[0]
	if candidate.ID != invoice.Id || candidate.PeriodDisposition != "OUT_OF_PERIOD" ||
		candidate.Posting == nil || candidate.Posting.InvoiceDate != "2026-09-01" || len(candidate.Receipts) != 201 {
		t.Fatalf("older invoice or its out-of-period disposition/receipt history was lost: %#v", candidate)
	}
	gotDateCounts := map[string]int{}
	for _, receipt := range candidate.Receipts {
		if receipt.Payment == nil || receipt.Payment.PaymentDate == nil {
			t.Fatalf("receipt omitted its payment proto/date: %#v", receipt)
		}
		paymentDate := receipt.Payment.PaymentDate
		protoDate := fmt.Sprintf("%04d-%02d-%02d", paymentDate.Year, paymentDate.Month, paymentDate.Day)
		if receipt.PaymentDate != protoDate {
			t.Fatalf("receipt raw date %q disagrees with payment proto date %q", receipt.PaymentDate, protoDate)
		}
		gotDateCounts[receipt.PaymentDate]++
	}
	for date, wantCount := range wantDateCounts {
		if gotDateCounts[date] != wantCount {
			t.Fatalf("receipt dates retained = %#v; want %s count %d", gotDateCounts, date, wantCount)
		}
	}
}

func TestInvoiceReportingCaptureFingerprintTracksSameTotalIdentityAndCancellationReturnsNil(t *testing.T) {
	first := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	firstInvoice := first.saveDraft(t, invoiceReportingID(1101), "IDENTICAL-001", "2026-10-01", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	firstPort, err := NewReportingPort(first.tx)
	if err != nil {
		t.Fatal(err)
	}
	firstCapture, err := firstPort.CaptureBAS(first.ctx, orgID, invoiceReportingPeriod())
	if err != nil || firstCapture == nil || len(firstCapture.Candidates) != 1 {
		t.Fatalf("first same-total capture = %#v, %v", firstCapture, err)
	}

	second := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	secondInvoice := second.saveDraft(t, invoiceReportingID(1102), "IDENTICAL-001", "2026-10-01", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	if secondInvoice.TotalMinor != firstInvoice.TotalMinor || secondInvoice.SubtotalMinor != firstInvoice.SubtotalMinor ||
		secondInvoice.GstMinor != firstInvoice.GstMinor {
		t.Fatalf("comparison invoices have different amounts: first=%d/%d/%d second=%d/%d/%d",
			firstInvoice.SubtotalMinor, firstInvoice.GstMinor, firstInvoice.TotalMinor,
			secondInvoice.SubtotalMinor, secondInvoice.GstMinor, secondInvoice.TotalMinor)
	}
	secondPort, err := NewReportingPort(second.tx)
	if err != nil {
		t.Fatal(err)
	}
	secondCapture, err := secondPort.CaptureBAS(second.ctx, orgID, invoiceReportingPeriod())
	if err != nil || secondCapture == nil || len(secondCapture.Candidates) != 1 {
		t.Fatalf("second same-total capture = %#v, %v", secondCapture, err)
	}
	if firstInvoice.Id == secondInvoice.Id || bytes.Equal(firstCapture.Fingerprint, secondCapture.Fingerprint) {
		t.Fatalf("changing only retained invoice identity at equal totals left the fingerprint unchanged: %x", firstCapture.Fingerprint)
	}

	cancelled, cancel := context.WithCancel(first.ctx)
	cancel()
	capture, err := firstPort.CaptureBAS(cancelled, orgID, invoiceReportingPeriod())
	if capture != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture = %#v, %v; want nil capture and context.Canceled", capture, err)
	}
	if port, err := NewReportingPort(nil); err == nil || port != nil {
		t.Fatalf("NewReportingPort(nil) = %#v, %v; want a nil port and error", port, err)
	}
}

func TestInvoiceReportingCaptureRejectsCandidateReceiptAndRetainedPayloadBounds(t *testing.T) {
	t.Run("candidate count", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		for index := 0; index < 1000; index++ {
			fixture.saveDraft(t, invoiceReportingID(uint64(1200+index)), fmt.Sprintf("BOUND-%04d", index), "", 0,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		}
		port, err := NewReportingPort(fixture.tx)
		if err != nil {
			t.Fatal(err)
		}
		capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if err != nil || capture == nil || len(capture.Candidates) != 1000 {
			t.Fatalf("1000-candidate capture = %#v, %v; want exactly 1000 candidates", capture, err)
		}
		fixture.saveDraft(t, invoiceReportingID(2200), "BOUND-OVER-1000", "", 0,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		capture, err = port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if capture != nil || !errors.Is(err, ErrSourceBound) {
			t.Fatalf("1001-candidate capture = %#v, %v; want nil capture and ErrSourceBound", capture, err)
		}
	})

	t.Run("receipt count", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
		invoice := fixture.saveDraft(t, invoiceReportingID(2301), "RECEIPT-BOUND", "2026-10-01", 1001,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		fixture.issue(t, invoice)
		fixture.post(t, invoice)
		for index := 0; index < 1000; index++ {
			fixture.recordPayment(t, invoice, uint64(index+1), 1, index)
		}
		port, err := NewReportingPort(fixture.tx)
		if err != nil {
			t.Fatal(err)
		}
		capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if err != nil || capture == nil || len(capture.Candidates) != 1 || len(capture.Candidates[0].Receipts) != 1000 {
			t.Fatalf("1000-receipt capture = %#v, %v; want exactly 1000 receipts", capture, err)
		}
		fixture.recordPayment(t, invoice, 1001, 1, 1000)
		capture, err = port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if capture != nil || !errors.Is(err, ErrSourceBound) {
			t.Fatalf("1001-receipt capture = %#v, %v; want nil capture and ErrSourceBound", capture, err)
		}
	})

	t.Run("aggregate payload bytes", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		for index := 0; index < 400; index++ {
			draft := &tammyv1.InvoiceDraft{
				Number: fmt.Sprintf("PAYLOAD-%03d", index),
				Lines:  make([]*tammyv1.InvoiceLineInput, 50),
			}
			for line := range draft.Lines {
				draft.Lines[line] = &tammyv1.InvoiceLineInput{
					Description:   strings.Repeat("x", 500),
					QuantityMilli: 1000,
					TaxTreatment:  tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE,
				}
			}
			fixture.saveDraftInput(t, invoiceReportingID(uint64(3000+index)), draft)
		}
		port, err := NewReportingPort(fixture.tx)
		if err != nil {
			t.Fatal(err)
		}
		capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if capture != nil || !errors.Is(err, ErrSourceBound) {
			t.Fatalf("large-payload capture = %#v, %v; want nil capture and ErrSourceBound for the 8 MiB limit", capture, err)
		}
	})
}

func newInvoiceReportingFixture(t *testing.T, basis tammyv1.GstBasis) *invoiceReportingFixture {
	t.Helper()
	return newInvoiceReportingFixtureAt(t, basis, time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
}

func newInvoiceReportingFixtureAt(t *testing.T, basis tammyv1.GstBasis, now time.Time) *invoiceReportingFixture {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin schema-13 encrypted transaction: %v", err)
	}
	fixture := &invoiceReportingFixture{
		ctx:       ctx,
		workspace: workspace,
		tx:        tx,
		now:       now.UTC(),
		idCounter: 200000,
	}
	t.Cleanup(func() {
		if fixture.tx != nil {
			_ = fixture.tx.Rollback()
		}
	})
	createdAt := fixture.now.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?, 'owner@example.test','Owner','ACTIVE',?,?)`,
		userID, createdAt, createdAt); err != nil {
		t.Fatalf("insert invoice reporting owner: %v", err)
	}
	fixture.profileRepository, err = organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	profile := &tammyv1.Organisation{
		Id:                    orgID,
		Version:               1,
		Abn:                   "51824753556",
		LegalName:             "Tammy Business Pty Ltd",
		DisplayName:           "Tammy",
		EntityType:            "AU_PRIVATE_COMPANY",
		GstBasis:              basis,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6,
		OwnerUserId:           userID,
		VerificationState:     tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		ActiveTaxRuleBundle:   &tammyv1.SourceRef{Type: "rule_bundle", Id: invoiceID, Revision: 1, ContentHash: make([]byte, sha256.Size)},
	}
	if err := fixture.profileRepository.Create(ctx, profile, fixture.now); err != nil {
		t.Fatalf("create invoice reporting organisation: %v", err)
	}
	fixture.profile, err = fixture.profileRepository.Get(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.InstallTemplate(ctx, orgID, template, fixture.now); err != nil {
		t.Fatalf("install actual accounting template: %v", err)
	}
	fixture.invoiceRepository, err = NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

type invoiceReportingFixture struct {
	ctx               context.Context
	workspace         *testkit.EncryptedWorkspace
	tx                *sqlcipher.Transaction
	invoiceRepository *Repository
	profileRepository *organisations.Repository
	profile           *tammyv1.Organisation
	now               time.Time
	idCounter         uint64
}

func (fixture *invoiceReportingFixture) nextID() (string, error) {
	fixture.idCounter++
	return invoiceReportingID(fixture.idCounter), nil
}

func (fixture *invoiceReportingFixture) setGSTBasis(t *testing.T, basis tammyv1.GstBasis) {
	t.Helper()
	frequency := tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY
	if basis == tammyv1.GstBasis_GST_BASIS_UNSPECIFIED {
		frequency = tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_UNSPECIFIED
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx, `UPDATE organisations SET gst_basis=?,gst_reporting_frequency=? WHERE id=?`,
		int32(basis), int32(frequency), orgID); err != nil {
		t.Fatalf("set fixture GST basis: %v", err)
	}
	var err error
	fixture.profile, err = fixture.profileRepository.Get(fixture.ctx, orgID)
	if err != nil {
		t.Fatalf("reload fixture GST basis: %v", err)
	}
}

func (fixture *invoiceReportingFixture) saveDraft(
	t *testing.T,
	id, number, issueDate string,
	unitPrice int64,
	treatment tammyv1.TaxTreatment,
) *tammyv1.Invoice {
	t.Helper()
	dueDate := ""
	if issueDate != "" {
		dueDate = issueDate
	}
	draft := &tammyv1.InvoiceDraft{
		Number:          number,
		IssueDate:       issueDate,
		DueDate:         dueDate,
		CustomerName:    "Customer",
		CustomerAddress: "1 High Street",
		SellerAddress:   "27 Cambridge Road",
		GstRegistered:   treatment == tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE,
		Lines: []*tammyv1.InvoiceLineInput{{
			Description:    "Consulting",
			QuantityMilli:  1000,
			UnitPriceMinor: unitPrice,
			TaxTreatment:   treatment,
		}},
	}
	return fixture.saveDraftInput(t, id, draft)
}

func (fixture *invoiceReportingFixture) saveDraftInput(t *testing.T, id string, draft *tammyv1.InvoiceDraft) *tammyv1.Invoice {
	t.Helper()
	totals, err := Calculate(draft)
	if err != nil {
		t.Fatalf("calculate fixture invoice %s: %v", id, err)
	}
	invoice := &tammyv1.Invoice{
		Id:             id,
		OrganisationId: orgID,
		Version:        1,
		State:          tammyv1.InvoiceState_INVOICE_STATE_DRAFT,
		SellerName:     fixture.profile.LegalName,
		SellerAbn:      fixture.profile.Abn,
		Draft:          proto.Clone(draft).(*tammyv1.InvoiceDraft),
		SubtotalMinor:  totals.SubtotalMinor,
		GstMinor:       totals.GstMinor,
		TotalMinor:     totals.TotalMinor,
		CostMinor:      totals.CostMinor,
		LineTotals:     totals.LineTotals,
		CreatedAt:      timestamppb.New(fixture.now),
		UpdatedAt:      timestamppb.New(fixture.now),
	}
	if err := fixture.invoiceRepository.Save(fixture.ctx, invoice, 0); err != nil {
		t.Fatalf("save actual invoice draft %s: %v", id, err)
	}
	return invoice
}

func (fixture *invoiceReportingFixture) issue(t *testing.T, invoice *tammyv1.Invoice) {
	t.Helper()
	expectedVersion := invoice.Version
	invoice.Version++
	invoice.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	invoice.UpdatedAt = timestamppb.New(fixture.now.Add(time.Minute))
	if err := fixture.invoiceRepository.Save(fixture.ctx, invoice, expectedVersion); err != nil {
		t.Fatalf("issue actual invoice %s through repository writer: %v", invoice.Id, err)
	}
}

func (fixture *invoiceReportingFixture) post(t *testing.T, invoice *tammyv1.Invoice) *tammyv1.InvoiceAccounting {
	t.Helper()
	repository, err := fixture.accountingRepository(t)
	if err != nil {
		t.Fatal(err)
	}
	posted, err := repository.Post(fixture.ctx, invoice, fixture.profile,
		fixture.electOperation(t, "PostInvoiceIncome"))
	if err != nil {
		t.Fatalf("post actual invoice income %s: %v", invoice.Id, err)
	}
	return posted
}

func (fixture *invoiceReportingFixture) recordPayment(
	t *testing.T,
	invoice *tammyv1.Invoice,
	expectedVersion uint64,
	amount int64,
	index int,
) *tammyv1.InvoicePayment {
	t.Helper()
	return fixture.recordPaymentOn(t, invoice, expectedVersion, amount, index,
		&tammyv1.CivilDate{Year: 2026, Month: 10, Day: 12})
}

func (fixture *invoiceReportingFixture) recordPaymentOn(
	t *testing.T,
	invoice *tammyv1.Invoice,
	expectedVersion uint64,
	amount int64,
	index int,
	paymentDate *tammyv1.CivilDate,
) *tammyv1.InvoicePayment {
	t.Helper()
	repository, err := fixture.accountingRepository(t)
	if err != nil {
		t.Fatal(err)
	}
	_, payment, err := repository.RecordPayment(fixture.ctx, invoice, fixture.profile, &tammyv1.RecordInvoicePaymentRequest{
		CommandContext:            &tammyv1.CommandContext{IdempotencyKey: fixture.electOperation(t, "RecordInvoicePayment")},
		OrganisationId:            orgID,
		InvoiceId:                 invoice.Id,
		ExpectedAccountingVersion: expectedVersion,
		PaymentDate:               paymentDate,
		Amount:                    incomeMoney(amount),
		BankAccountId:             templateBankID(),
		Reference:                 fmt.Sprintf("Reporting receipt %04d", index),
	})
	if err != nil {
		t.Fatalf("record actual invoice payment %s/%d: %v", invoice.Id, index, err)
	}
	return payment
}

func (fixture *invoiceReportingFixture) accountingRepository(t *testing.T) (*AccountingRepository, error) {
	t.Helper()
	return NewAccountingRepository(fixture.tx, func() time.Time { return fixture.now.Add(time.Hour) }, fixture.nextID)
}

func (fixture *invoiceReportingFixture) mustID(t *testing.T) string {
	t.Helper()
	id, err := fixture.nextID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (fixture *invoiceReportingFixture) electOperation(t *testing.T, method string) string {
	t.Helper()
	key := fixture.mustID(t)
	// Accounting writers retain a revision claim against an elected command in
	// the same transaction, just as the ordinary command coordinator does.
	if _, err := fixture.tx.ExecContext(fixture.ctx, `INSERT INTO command_idempotency_v1(
		workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,
		semantic_hash_version,request_type,normalized_hash,outcome,created_at)
		VALUES(?,?,?,?,'v1','invoice-reporting-test',zeroblob(32),'ELECTED',?)`,
		orgID, userID, "tammy.v1.InvoiceService."+method, key,
		fixture.now.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("elect accounting fixture command: %v", err)
	}
	return key
}

func invoiceReportingPeriod() *tammyv1.BasCivilDateRange {
	return &tammyv1.BasCivilDateRange{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1},
		EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 31},
	}
}

func invoiceReportingID(sequence uint64) string {
	return fmt.Sprintf("018f0000-0000-7000-8000-%012x", sequence)
}

func assertInvoiceReportingCandidateSource(t *testing.T, candidates []BASInvoiceCandidate) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.ID == "" || candidate.OrganisationID != orgID || candidate.Invoice == nil ||
			candidate.Invoice.Id != candidate.ID || candidate.Invoice.OrganisationId != candidate.OrganisationID ||
			uint64(candidate.Version) != candidate.Invoice.Version || candidate.State != "DRAFT" && candidate.State != "ISSUED" ||
			candidate.Number != candidate.Invoice.Draft.Number || candidate.CreatedAt == "" || candidate.UpdatedAt == "" ||
			len(candidate.InvoiceProto) == 0 || len(candidate.InvoiceHash) != sha256.Size {
			t.Fatalf("invoice reporting candidate omitted or mismatched its source row: %#v", candidate)
		}
		encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(candidate.Invoice)
		if err != nil || !bytes.Equal(candidate.InvoiceProto, encoded) {
			t.Fatalf("invoice %s payload bytes did not preserve the exact deterministic source proto: %v", candidate.ID, err)
		}
		hash := sha256.Sum256(candidate.InvoiceProto)
		if !bytes.Equal(candidate.InvoiceHash, hash[:]) {
			t.Fatalf("invoice %s payload hash does not bind its exact proto: got %x want %x", candidate.ID, candidate.InvoiceHash, hash)
		}
	}
}

func invoiceReportingCandidatesByID(t *testing.T, capture *BASReportingCapture) map[string]BASInvoiceCandidate {
	t.Helper()
	result := make(map[string]BASInvoiceCandidate, len(capture.Candidates))
	for _, candidate := range capture.Candidates {
		if _, found := result[candidate.ID]; found {
			t.Fatalf("invoice reporting capture repeated candidate %s", candidate.ID)
		}
		result[candidate.ID] = candidate
	}
	return result
}

func assertInvoiceReportingPostingJournal(
	t *testing.T,
	candidate BASInvoiceCandidate,
	wantSourceType, wantSourceID string,
	wantSourceRevision uint64,
) {
	t.Helper()
	posting := candidate.Posting
	if posting == nil || posting.InvoiceID != candidate.ID || posting.OrganisationID != orgID ||
		uint64(posting.InvoiceVersion) != uint64(candidate.Version) || posting.InvoiceDate != candidate.Invoice.Draft.IssueDate ||
		posting.RevenueAccountID == "" || posting.JournalID == nil || *posting.JournalID == "" ||
		posting.InvoiceTotalMinor != posting.InvoiceNetMinor+posting.InvoiceGSTMinor ||
		posting.BASSalesGrossMinor < 0 || posting.ProfileVersion == 0 || posting.FinancialRevision == 0 ||
		posting.CreatedAt == "" || len(posting.SourceHash) != sha256.Size || posting.Journal == nil {
		t.Fatalf("invoice %s posting row lost raw source metadata: %#v", candidate.ID, posting)
	}
	if !bytes.Equal(posting.SourceHash, candidate.InvoiceHash) {
		t.Fatalf("invoice %s posting hash lost exact invoice payload lineage: posting=%x invoice=%x", candidate.ID, posting.SourceHash, candidate.InvoiceHash)
	}
	if len(posting.IncomeLines) != len(candidate.Invoice.Draft.Lines) {
		t.Fatalf("invoice %s income line count=%d want %d", candidate.ID, len(posting.IncomeLines), len(candidate.Invoice.Draft.Lines))
	}
	for index, line := range posting.IncomeLines {
		want := candidate.Invoice.LineTotals[index]
		if uint64(line.Sequence) != uint64(index+1) || line.GrossMinor != want.TotalMinor ||
			line.NetMinor != want.SubtotalMinor || line.GSTMinor != want.GstMinor ||
			int32(line.Treatment) != int32(candidate.Invoice.Draft.Lines[index].TaxTreatment) {
			t.Fatalf("invoice %s ordered source income line %d changed: got %#v want=%#v", candidate.ID, index+1, line, want)
		}
	}
	assertInvoiceReportingJournalProof(t, posting.Journal, wantSourceType, wantSourceID,
		wantSourceRevision, tammyv1.JournalSource_JOURNAL_SOURCE_RECEIVABLE)
}

func assertInvoiceReportingJournalProof(
	t *testing.T,
	proof *accounting.JournalReportingRecord,
	wantSourceType, wantSourceID string,
	wantSourceRevision uint64,
	wantSource tammyv1.JournalSource,
) {
	t.Helper()
	if proof == nil || proof.Journal == nil || proof.Journal.Id == "" || proof.Journal.OrganisationId != orgID ||
		proof.SourceType != wantSourceType || proof.SourceID != wantSourceID ||
		proof.SourceRevision != wantSourceRevision || proof.State != "POSTED" || proof.CreatedAt == "" ||
		len(proof.ContentHash) != sha256.Size || proof.PayloadBytes <= 0 ||
		proof.Journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || proof.Journal.Source != wantSource ||
		len(proof.Journal.Lines) < 2 {
		t.Fatalf("full invoice journal proof is incomplete: %#v", proof)
	}
}

func sameInvoiceCandidateOrder(left, right []BASInvoiceCandidate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID {
			return false
		}
	}
	return true
}
