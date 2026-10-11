//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func TestInvoiceReportingCaptureRejectsValidDraftRowProtoVersionMismatch(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := fixture.saveDraft(t, invoiceReportingID(4101), "ROW-PROTO-MISMATCH", "2026-10-12", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)

	// The ordinary DRAFT update guard permits a version advance. Keep a valid,
	// deterministic version-one invoice proto while advancing the row to version
	// two, so the capture must detect source-row/proto disagreement without
	// disabling any schema guard or storing malformed protobuf bytes.
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(invoice)
	if err != nil {
		t.Fatalf("marshal valid invoice proto: %v", err)
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`UPDATE invoices_v1 SET version=version+1,invoice_proto=? WHERE id=? AND organisation_id=?`,
		encoded, invoice.Id, orgID); err != nil {
		t.Fatalf("advance mutable DRAFT row while retaining its valid prior-version proto: %v", err)
	}

	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort() = %v", err)
	}
	capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if capture != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("capture of mismatched DRAFT row/proto = %#v, %v; want nil capture and ErrRepository", capture, err)
	}
}

func TestInvoiceReportingCaptureKeepsEmptyCensusesOrganisationBound(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	fixture.saveDraft(t, invoiceReportingID(4201), "OWNER-ONLY", "2026-10-12", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort() = %v", err)
	}

	ownerCapture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil || ownerCapture == nil || len(ownerCapture.Candidates) != 1 {
		t.Fatalf("owner census = %#v, %v; want the one owned invoice", ownerCapture, err)
	}
	firstOtherOrg := invoiceReportingID(4291)
	secondOtherOrg := invoiceReportingID(4292)
	firstEmpty, err := port.CaptureBAS(fixture.ctx, firstOtherOrg, invoiceReportingPeriod())
	if err != nil || firstEmpty == nil || len(firstEmpty.Candidates) != 0 || len(firstEmpty.Fingerprint) != sha256.Size {
		t.Fatalf("first unrelated-org census = %#v, %v; want an empty fingerprinted census", firstEmpty, err)
	}
	secondEmpty, err := port.CaptureBAS(fixture.ctx, secondOtherOrg, invoiceReportingPeriod())
	if err != nil || secondEmpty == nil || len(secondEmpty.Candidates) != 0 || len(secondEmpty.Fingerprint) != sha256.Size {
		t.Fatalf("second unrelated-org census = %#v, %v; want an empty fingerprinted census", secondEmpty, err)
	}
	repeated, err := port.CaptureBAS(fixture.ctx, firstOtherOrg, invoiceReportingPeriod())
	if err != nil || repeated == nil || !bytes.Equal(firstEmpty.Fingerprint, repeated.Fingerprint) {
		t.Fatalf("repeat empty census changed its fingerprint: first=%#v repeat=%#v error=%v", firstEmpty, repeated, err)
	}
	if bytes.Equal(firstEmpty.Fingerprint, secondEmpty.Fingerprint) ||
		bytes.Equal(firstEmpty.Fingerprint, ownerCapture.Fingerprint) {
		t.Fatalf("empty census fingerprint was not bound to the requested organisation: first=%x second=%x owner=%x",
			firstEmpty.Fingerprint, secondEmpty.Fingerprint, ownerCapture.Fingerprint)
	}
}

func TestInvoiceReportingCaptureSeesOuterTransactionChangesAndIsReadOnly(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	fixture.saveDraft(t, invoiceReportingID(4301), "COMMITTED-BASELINE", "2026-10-12", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	baselinePort, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort(baseline) = %v", err)
	}
	baseline, err := baselinePort.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil || baseline == nil || len(baseline.Candidates) != 1 {
		t.Fatalf("baseline capture = %#v, %v; want one baseline candidate", baseline, err)
	}
	if err := fixture.tx.Commit(); err != nil {
		t.Fatalf("commit baseline invoice fixture: %v", err)
	}
	fixture.tx = nil

	writeTx, err := fixture.workspace.Database.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin caller-owned outer transaction: %v", err)
	}
	fixture.tx = writeTx
	fixture.invoiceRepository, err = NewRepository(writeTx)
	if err != nil {
		t.Fatalf("NewRepository(write transaction) = %v", err)
	}
	added := fixture.saveDraft(t, invoiceReportingID(4302), "UNCOMMITTED-CANDIDATE", "2026-10-13", 200,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)

	readRevisionVector := func(tx *sqlcipher.Transaction) [7]int64 {
		t.Helper()
		var vector [7]int64
		if err := tx.QueryRowContext(fixture.ctx, `SELECT financial_revision,ledger_revision,settlement_revision,
			banking_revision,tax_source_revision,organisation_profile_revision,rule_bundle_revision
			FROM financial_revisions WHERE id=1`).Scan(
			&vector[0], &vector[1], &vector[2], &vector[3], &vector[4], &vector[5], &vector[6]); err != nil {
			t.Fatalf("read financial revision vector: %v", err)
		}
		return vector
	}
	beforeCaptureRevisions := readRevisionVector(writeTx)
	writePort, err := NewReportingPort(writeTx)
	if err != nil {
		t.Fatalf("NewReportingPort(write transaction) = %v", err)
	}
	withinOuterTx, err := writePort.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil || withinOuterTx == nil || len(withinOuterTx.Candidates) != 2 {
		t.Fatalf("capture inside caller transaction = %#v, %v; want baseline and uncommitted candidate", withinOuterTx, err)
	}
	withinOuterTxByID := invoiceReportingCandidatesByID(t, withinOuterTx)
	if withinOuterTxByID[added.Id].ID != added.Id || bytes.Equal(baseline.Fingerprint, withinOuterTx.Fingerprint) {
		t.Fatalf("capture did not bind the uncommitted candidate: candidates=%#v baseline=%x within=%x",
			withinOuterTx.Candidates, baseline.Fingerprint, withinOuterTx.Fingerprint)
	}
	if afterCaptureRevisions := readRevisionVector(writeTx); afterCaptureRevisions != beforeCaptureRevisions {
		t.Fatalf("read-only capture changed financial revisions: before=%v after=%v", beforeCaptureRevisions, afterCaptureRevisions)
	}
	if err := writeTx.Rollback(); err != nil {
		t.Fatalf("caller rollback: %v", err)
	}
	fixture.tx = nil

	verifyTx, err := fixture.workspace.Database.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin post-rollback verification transaction: %v", err)
	}
	fixture.tx = verifyTx
	verifyPort, err := NewReportingPort(verifyTx)
	if err != nil {
		t.Fatalf("NewReportingPort(post-rollback) = %v", err)
	}
	afterRollback, err := verifyPort.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil || afterRollback == nil || len(afterRollback.Candidates) != 1 {
		t.Fatalf("capture after caller rollback = %#v, %v; want only the baseline candidate", afterRollback, err)
	}
	if afterRollback.Candidates[0].ID != invoiceReportingID(4301) || !bytes.Equal(baseline.Fingerprint, afterRollback.Fingerprint) {
		t.Fatalf("outer rollback did not restore the baseline capture: after=%#v baseline=%x after=%x",
			afterRollback.Candidates, baseline.Fingerprint, afterRollback.Fingerprint)
	}
	if afterRollbackRevisions := readRevisionVector(verifyTx); afterRollbackRevisions != beforeCaptureRevisions {
		t.Fatalf("capture/rollback changed financial revisions: before=%v after=%v", beforeCaptureRevisions, afterRollbackRevisions)
	}
	// fixture cleanup owns verifyTx and rolls it back after assertions.
}
