//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"errors"
	"runtime"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestInvoiceBASScalarKeysRejectSameByteBlobRootOwnerWithoutChildRows(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(99201), "SCALAR-OWNER", "2026-10-12", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	probe := &invoiceBASScalarProbe{Transaction: f.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}

	// Passing control: the production writer's canonical TEXT-owned root is
	// returned as one root key before any corruption-only mutation.
	control, err := port.SummarizeBASKeys(f.ctx, orgID)
	if err != nil || control == nil || control.RootCount != 1 || control.ScalarKeyCount != 1 {
		t.Fatalf("canonical root summary=%#v err=%v", control, err)
	}
	keys, err := port.ReadBASKeys(f.ctx, orgID, *control, control.ScalarKeyCount, control.ScalarBytes)
	if err != nil || len(keys) != 1 || keys[0].Kind != BASScalarInvoice || keys[0].InvoiceID != invoice.Id ||
		keys[0].RootVersion == nil || *keys[0].RootVersion != 1 {
		t.Fatalf("canonical writer root keys=%#v err=%v", keys, err)
	}
	keyReadsBeforeCorruption := probe.keyReads
	if keyReadsBeforeCorruption != 1 {
		t.Fatalf("canonical control key reads=%d, want 1", keyReadsBeforeCorruption)
	}

	// Corruption-only root-only fixture: no posting/receipt parent arm can
	// expose this BLOB-owned draft invoice.
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
		`UPDATE invoices_v1 SET organisation_id=CAST(organisation_id AS BLOB) WHERE id=?`, invoice.Id); err != nil {
		t.Fatal(err)
	}
	assertInvoiceReportingBlobOwner(t, f, invoice.Id, orgID)
	afterCorruption := invoiceReportingBudgetState(t, f)

	summary, err := port.SummarizeBASKeys(f.ctx, orgID)
	if summary != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("same-byte BLOB-owned root was silently omitted: summary=%#v err=%v", summary, err)
	}
	keys, err = port.ReadBASKeys(f.ctx, orgID, *control, control.ScalarKeyCount, control.ScalarBytes)
	if keys != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("same-byte BLOB-owned root read=%#v err=%v", keys, err)
	}
	if probe.keyReads != keyReadsBeforeCorruption || probe.bodyReads != 0 ||
		!f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != afterCorruption {
		t.Fatal("owner-scope rejection read key/body payloads, wrote, or ended the caller transaction")
	}
}

func TestInvoiceBASScalarKeysRejectSameByteBlobOwnerDriftAfterEmptyReservation(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(99202), "SCALAR-OWNER-DRIFT", "2026-10-12", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
		`UPDATE invoices_v1 SET organisation_id='foreign-owner' WHERE id=?`, invoice.Id); err != nil {
		t.Fatal(err)
	}

	probe := &invoiceBASScalarProbe{Transaction: f.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := port.SummarizeBASKeys(f.ctx, orgID)
	empty := BASScalarSummary{OrganisationID: orgID}
	if err != nil || expected == nil || *expected != empty {
		t.Fatalf("foreign-owner empty baseline=%#v err=%v", expected, err)
	}

	var afterCorruption [8]int64
	probe.beforeKeyRead = func() error {
		// ReadBASKeys has just taken its fresh empty summary and is about to run
		// the guarded key SQL. Keep the reference graph empty; change only the
		// root owner to the requested UUID's exact bytes.
		if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
			`UPDATE invoices_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, orgID, invoice.Id); err != nil {
			return err
		}
		assertInvoiceReportingBlobOwner(t, f, invoice.Id, orgID)
		afterCorruption = invoiceReportingBudgetState(t, f)
		return nil
	}
	keys, err := port.ReadBASKeys(f.ctx, orgID, *expected, 0, 0)
	var bound *SourceBoundError
	if keys != nil || !errors.Is(err, ErrSourceBound) || !errors.As(err, &bound) ||
		bound.Resource != "BAS scalar keys" || bound.Limit != 0 {
		t.Fatalf("same-byte BLOB owner drift did not hit zero-key sentinel: keys=%#v err=%v", keys, err)
	}
	if probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 ||
		!f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != afterCorruption {
		t.Fatal("drift guard did not use a fresh summary, read bodies, write, or preserve the caller transaction")
	}
}

func TestInvoiceBASScalarKeysLeaveUnrelatedBlobRootOwnerOutOfScope(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(99203), "SCALAR-FOREIGN-BLOB", "2026-10-12", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	const unrelatedUUID = "018f0000-0000-7000-8000-000000009999"
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
		`UPDATE invoices_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, unrelatedUUID, invoice.Id); err != nil {
		t.Fatal(err)
	}
	assertInvoiceReportingBlobOwner(t, f, invoice.Id, unrelatedUUID)
	afterCorruption := invoiceReportingBudgetState(t, f)

	probe := &invoiceBASScalarProbe{Transaction: f.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := port.SummarizeBASKeys(f.ctx, orgID)
	empty := BASScalarSummary{OrganisationID: orgID}
	if err != nil || expected == nil || *expected != empty {
		t.Fatalf("unrelated BLOB owner entered target census: summary=%#v err=%v", expected, err)
	}
	keys, err := port.ReadBASKeys(f.ctx, orgID, *expected, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("unrelated BLOB owner changed empty target read: keys=%#v err=%v", keys, err)
	}
	if probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 ||
		!f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != afterCorruption {
		t.Fatal("out-of-scope owner read bodies, wrote, or ended the caller transaction")
	}
}

func assertInvoiceReportingBlobOwner(t *testing.T, f *invoiceReportingFixture, invoiceID, wantOwner string) {
	t.Helper()
	var storage string
	var owner []byte
	if err := f.tx.QueryRowContext(f.ctx, `SELECT typeof(organisation_id),organisation_id FROM invoices_v1 WHERE id=?`, invoiceID).Scan(&storage, &owner); err != nil {
		t.Fatal(err)
	}
	if storage != "blob" || len(owner) != 36 || string(owner) != wantOwner {
		t.Fatalf("corruption fixture owner type/bytes=%s/%q, want exact BLOB UUID %q", storage, owner, wantOwner)
	}
}

func TestInvoiceBASFullOwnerScopeRejectsSameByteBlobBeforeBodies(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(99401), "FULL-OWNER", "2026-10-12", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	beforeControl := invoiceReportingBudgetState(t, f)

	// Canonical TEXT-owner control exercises the real writer, full metadata
	// census and complete capture before introducing fixture corruption.
	port, err := NewReportingPort(f.tx)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := port.PreflightBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 1 ||
		preflight.ReceiptCount != 0 || preflight.JournalCount != 0 {
		t.Fatalf("canonical full preflight=%#v err=%v", preflight, err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || capture.CandidateCount != 1 || len(capture.Candidates) != 1 ||
		capture.Candidates[0].ID != invoice.Id || capture.Candidates[0].OrganisationID != orgID ||
		!proto.Equal(capture.Candidates[0].Invoice, invoice) || capture.PayloadBytes != preflight.PayloadBytes {
		t.Fatalf("canonical full capture=%#v err=%v", capture, err)
	}
	if !f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != beforeControl {
		t.Fatal("canonical full control changed retained state or caller transaction")
	}

	// Change only the retained root owner. Keep this root-only so no child
	// owner/reference path can accidentally surface it for the requested org.
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
		`UPDATE invoices_v1 SET organisation_id=CAST(organisation_id AS BLOB) WHERE id=?`, invoice.Id); err != nil {
		t.Fatal(err)
	}
	var ownerType string
	var ownerBytes []byte
	if err := f.tx.QueryRowContext(f.ctx,
		`SELECT typeof(organisation_id),organisation_id FROM invoices_v1 WHERE id=?`, invoice.Id,
	).Scan(&ownerType, &ownerBytes); err != nil || ownerType != "blob" || !bytes.Equal(ownerBytes, []byte(orgID)) {
		t.Fatalf("corrupted owner type=%q bytes=%x err=%v", ownerType, ownerBytes, err)
	}
	afterCorruption := invoiceReportingBudgetState(t, f)

	probe := &invoiceReportingBudgetProbe{Transaction: f.tx}
	guardedPort, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := guardedPort.PreflightBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes)); got != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("same-byte BLOB full preflight=%#v err=%v", got, err)
	}
	if got, err := guardedPort.CaptureBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes)); got != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("same-byte BLOB full capture=%#v err=%v", got, err)
	}
	if probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration ||
		!f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != afterCorruption {
		t.Fatal("pre-existing BLOB root reached any body query, wrote, or ended the caller transaction")
	}
}

func TestInvoiceBASFullCaptureRejectsSameByteBlobDriftAfterEmptyPreflight(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(99402), "FULL-OWNER-DRIFT", "2026-10-12", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	const foreignOwner = "018f0000-0000-7000-8000-000000009998"
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
		`UPDATE invoices_v1 SET organisation_id=? WHERE id=?`, foreignOwner, invoice.Id); err != nil {
		t.Fatal(err)
	}

	// Check the retained foreign-TEXT baseline independently; Capture below
	// takes its own fresh preflight and the hook fires after that reservation.
	baselinePort, err := NewReportingPort(f.tx)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := baselinePort.PreflightBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || baseline == nil || baseline.CandidateCount != 0 ||
		baseline.ReceiptCount != 0 || baseline.JournalCount != 0 {
		t.Fatalf("foreign-TEXT full baseline=%#v err=%v", baseline, err)
	}

	probe := &invoiceReportingBudgetProbe{Transaction: f.tx}
	var afterCorruption [8]int64
	var before runtime.MemStats
	probe.beforeFirstInvoiceHydration = func() {
		// Capture's internal preflight just observed an empty target census.
		// Drift the root to exact target-owner BLOB and grow its body only now.
		if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
			t.Fatalf("enable fixture checks override: %v", err)
		}
		mutationErr := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
			`UPDATE invoices_v1 SET organisation_id=CAST(? AS BLOB),invoice_proto=zeroblob(9437184) WHERE id=?`,
			orgID, invoice.Id)
		_, restoreChecksErr := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF")
		if mutationErr != nil || restoreChecksErr != nil {
			t.Fatalf("install owner/body drift: %v / restore checks: %v", mutationErr, restoreChecksErr)
		}
		var ownerType string
		var ownerBytes []byte
		var protoBytes int
		if err := f.tx.QueryRowContext(f.ctx,
			`SELECT typeof(organisation_id),organisation_id,length(invoice_proto) FROM invoices_v1 WHERE id=?`, invoice.Id,
		).Scan(&ownerType, &ownerBytes, &protoBytes); err != nil || ownerType != "blob" ||
			!bytes.Equal(ownerBytes, []byte(orgID)) || protoBytes != 9437184 {
			t.Fatalf("drift fixture owner=%q/%x proto=%d err=%v", ownerType, ownerBytes, protoBytes, err)
		}
		afterCorruption = invoiceReportingBudgetState(t, f)
		runtime.ReadMemStats(&before)
	}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	if capture != nil || !errors.Is(err, ErrRepository) || !probe.mutatedBeforeFirstHydration ||
		probe.hydrationQueries != 1 {
		t.Fatalf("drifting BLOB root capture=%#v err=%v body queries=%d hook=%t; want nil/error after one masked root query",
			capture, err, probe.hydrationQueries, probe.mutatedBeforeFirstHydration)
	}
	if allocated >= 2*1024*1024 {
		t.Fatalf("masked same-byte BLOB root copied its rejected 9 MiB proto: allocated=%d", allocated)
	}
	if !f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != afterCorruption {
		t.Fatal("full-capture owner drift wrote or ended the caller transaction")
	}
}

func TestInvoiceBASFullOwnerScopeLeavesUnrelatedBlobUUIDOutOfScope(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(99403), "UNRELATED-BLOB", "2026-10-12", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	const unrelatedUUID = "018f0000-0000-7000-8000-000000009999"
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1",
		`UPDATE invoices_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, unrelatedUUID, invoice.Id); err != nil {
		t.Fatal(err)
	}
	var ownerType string
	var ownerBytes []byte
	if err := f.tx.QueryRowContext(f.ctx,
		`SELECT typeof(organisation_id),organisation_id FROM invoices_v1 WHERE id=?`, invoice.Id,
	).Scan(&ownerType, &ownerBytes); err != nil || ownerType != "blob" ||
		!bytes.Equal(ownerBytes, []byte(unrelatedUUID)) {
		t.Fatalf("unrelated owner type=%q bytes=%x err=%v", ownerType, ownerBytes, err)
	}
	afterCorruption := invoiceReportingBudgetState(t, f)

	port, err := NewReportingPort(f.tx)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := port.PreflightBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 0 ||
		preflight.ReceiptCount != 0 || preflight.JournalCount != 0 {
		t.Fatalf("unrelated BLOB full preflight=%#v err=%v", preflight, err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		f.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || capture.CandidateCount != 0 || len(capture.Candidates) != 0 ||
		capture.ReceiptCount != 0 || capture.JournalCount != 0 {
		t.Fatalf("unrelated BLOB full capture=%#v err=%v", capture, err)
	}
	if !f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != afterCorruption {
		t.Fatal("out-of-scope BLOB owner wrote or ended the caller transaction")
	}
}
