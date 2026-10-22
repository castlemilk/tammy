//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestInvoiceReportingCapturePreservesDualReceiptTimestamps(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
	invoice := fixture.saveDraft(t, invoiceReportingID(9701), "DUAL-CLOCK-RECEIPT", "2026-10-12", 100,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, invoice)
	fixture.post(t, invoice)
	payment := fixture.recordPayment(t, invoice, 1, 50, 1)

	var originalProto, originalSourceHash []byte
	var originalCreatedAt string
	if err := fixture.tx.QueryRowContext(fixture.ctx, `SELECT payment_proto,source_hash,created_at
		FROM invoice_payment_v1 WHERE id=?`, payment.Id).Scan(&originalProto, &originalSourceHash, &originalCreatedAt); err != nil {
		t.Fatalf("read newly written receipt cells: %v", err)
	}
	wantProto, err := (proto.MarshalOptions{Deterministic: true}).Marshal(payment)
	if err != nil {
		t.Fatalf("marshal payment returned by public writer: %v", err)
	}
	wantSourceHash := sha256.Sum256(originalProto)
	if !bytes.Equal(originalProto, wantProto) || !bytes.Equal(originalSourceHash, wantSourceHash[:]) {
		t.Fatal("public writer did not retain the exact returned protobuf and its payload hash")
	}
	protoCreatedAt := payment.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano)
	if originalCreatedAt != protoCreatedAt {
		t.Fatalf("new writer timestamps differ: protobuf=%q SQL=%q", protoCreatedAt, originalCreatedAt)
	}

	// Model a historical row written by the old two-clock call pattern. Change
	// only the SQL row metadata; preserve the exact serialized payment and its
	// payload hash as they were committed by the payment writer.
	legacyCreatedAt := payment.CreatedAt.AsTime().UTC().Add(17 * time.Second).Format(time.RFC3339Nano)
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_payment_v1",
		`UPDATE invoice_payment_v1 SET created_at=? WHERE id=?`, legacyCreatedAt, payment.Id); err != nil {
		t.Fatalf("install historical dual-clock SQL timestamp: %v", err)
	}

	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort() = %v", err)
	}
	captureReceipt := func(wantCreatedAt string) (*BASReportingCapture, BASInvoiceReceipt) {
		t.Helper()
		capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if err != nil || capture == nil || len(capture.Candidates) != 1 || len(capture.Candidates[0].Receipts) != 1 {
			t.Fatalf("CaptureBAS() = %#v, %v; want one invoice with one receipt", capture, err)
		}
		receipt := capture.Candidates[0].Receipts[0]
		if receipt.CreatedAt != wantCreatedAt {
			t.Fatalf("captured SQL created_at = %q; want exact raw value %q", receipt.CreatedAt, wantCreatedAt)
		}
		if receipt.Payment == nil || receipt.Payment.CreatedAt == nil ||
			receipt.Payment.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano) != protoCreatedAt {
			t.Fatalf("captured protobuf timestamp changed: got %#v, want %q", receipt.Payment, protoCreatedAt)
		}
		if receipt.CreatedAt == protoCreatedAt {
			t.Fatalf("test fixture did not retain distinct SQL/protobuf timestamps: %q", protoCreatedAt)
		}
		if !bytes.Equal(receipt.PaymentProto, originalProto) || !bytes.Equal(receipt.SourceHash, originalSourceHash) {
			t.Fatal("capture rewrote the exact protobuf payload or stored source hash")
		}
		computed := sha256.Sum256(receipt.PaymentProto)
		if !bytes.Equal(receipt.PaymentHash, computed[:]) || !bytes.Equal(receipt.SourceHash, computed[:]) {
			t.Fatalf("captured source lineage changed: payload=%x payment=%x stored=%x",
				receipt.PaymentProto, receipt.PaymentHash, receipt.SourceHash)
		}
		return capture, receipt
	}

	first, firstReceipt := captureReceipt(legacyCreatedAt)
	repeated, repeatedReceipt := captureReceipt(legacyCreatedAt)
	if !bytes.Equal(first.Fingerprint, repeated.Fingerprint) ||
		!bytes.Equal(firstReceipt.PaymentProto, repeatedReceipt.PaymentProto) ||
		!bytes.Equal(firstReceipt.SourceHash, repeatedReceipt.SourceHash) {
		t.Fatal("repeat capture changed the legacy dual-clock source or fingerprint")
	}

	// SQL created_at remains a separate fingerprinted raw cell. A later change
	// to that cell must change the source fingerprint even though the payment
	// protobuf and its source_hash remain byte-for-byte unchanged.
	changedCreatedAt := payment.CreatedAt.AsTime().UTC().Add(23 * time.Second).Format(time.RFC3339Nano)
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_payment_v1",
		`UPDATE invoice_payment_v1 SET created_at=? WHERE id=?`, changedCreatedAt, payment.Id); err != nil {
		t.Fatalf("change SQL created_at for fingerprint check: %v", err)
	}
	changed, changedReceipt := captureReceipt(changedCreatedAt)
	if bytes.Equal(first.Fingerprint, changed.Fingerprint) {
		t.Fatalf("changing only raw SQL created_at left capture fingerprint unchanged: %x", first.Fingerprint)
	}
	if !bytes.Equal(changedReceipt.PaymentProto, originalProto) || !bytes.Equal(changedReceipt.SourceHash, originalSourceHash) {
		t.Fatal("SQL timestamp change rewrote immutable payment payload or stored source hash")
	}
	changedRepeat, _ := captureReceipt(changedCreatedAt)
	if !bytes.Equal(changed.Fingerprint, changedRepeat.Fingerprint) {
		t.Fatal("repeat capture after SQL timestamp change was not stable")
	}
}

func TestInvoiceReportingCaptureRejectsInvalidReceiptTimestamps(t *testing.T) {
	newPaidFixture := func(t *testing.T) (*invoiceReportingFixture, *tammyv1.InvoicePayment, *ReportingPort) {
		t.Helper()
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
		invoice := fixture.saveDraft(t, invoiceReportingID(9702), "INVALID-RECEIPT-TIME", "2026-10-12", 100,
			tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
		fixture.issue(t, invoice)
		fixture.post(t, invoice)
		payment := fixture.recordPayment(t, invoice, 1, 50, 2)
		port, err := NewReportingPort(fixture.tx)
		if err != nil {
			t.Fatalf("NewReportingPort() = %v", err)
		}
		return fixture, payment, port
	}

	t.Run("noncanonical SQL timestamp", func(t *testing.T) {
		fixture, payment, port := newPaidFixture(t)
		// RFC3339 accepts this offset spelling, but the retained timestamp
		// contract requires canonical UTC formatting, which is Z here.
		if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_payment_v1",
			`UPDATE invoice_payment_v1 SET created_at=? WHERE id=?`,
			"2026-10-12T13:00:00+00:00", payment.Id); err != nil {
			t.Fatalf("install noncanonical SQL timestamp: %v", err)
		}
		capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if capture != nil || !errors.Is(err, ErrRepository) {
			t.Fatalf("CaptureBAS() = %#v, %v; want nil capture and ErrRepository", capture, err)
		}
	})

	t.Run("invalid protobuf timestamp with matching payload hash", func(t *testing.T) {
		fixture, payment, port := newPaidFixture(t)
		invalid := proto.Clone(payment).(*tammyv1.InvoicePayment)
		invalid.CreatedAt = &timestamppb.Timestamp{Seconds: 253402300800}
		invalidProto, err := (proto.MarshalOptions{Deterministic: true}).Marshal(invalid)
		if err != nil {
			t.Fatalf("marshal invalid timestamp fixture: %v", err)
		}
		invalidHash := sha256.Sum256(invalidProto)
		if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_payment_v1",
			`UPDATE invoice_payment_v1 SET payment_proto=?,source_hash=? WHERE id=?`,
			invalidProto, invalidHash[:], payment.Id); err != nil {
			t.Fatalf("install invalid payment timestamp with matching source hash: %v", err)
		}
		capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
		if capture != nil || !errors.Is(err, ErrRepository) {
			t.Fatalf("CaptureBAS() = %#v, %v; want nil capture and ErrRepository", capture, err)
		}
	})
}
