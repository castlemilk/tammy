//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestInvoiceBASScalarKeysReopenAndRespectCallerRollback(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
	invoice := fixture.saveDraft(t, invoiceReportingID(9401), "SCALAR-REOPEN", "2026-10-12", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, invoice)
	fixture.post(t, invoice)
	fixture.recordPayment(t, invoice, 1, 50, 0)

	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort(initial transaction) = %v", err)
	}
	stateBeforeRead := invoiceReportingBudgetState(t, fixture)
	initialSummary, err := port.SummarizeBASKeys(fixture.ctx, orgID)
	if err != nil || initialSummary == nil {
		t.Fatalf("initial SummarizeBASKeys() = %#v, %v", initialSummary, err)
	}
	initialKeys, err := port.ReadBASKeys(fixture.ctx, orgID, *initialSummary,
		initialSummary.ScalarKeyCount, initialSummary.ScalarBytes)
	if err != nil || int64(len(initialKeys)) != initialSummary.ScalarKeyCount {
		t.Fatalf("initial ReadBASKeys() returned %d keys, %v; summary=%#v", len(initialKeys), err, initialSummary)
	}
	if stateAfterRead := invoiceReportingBudgetState(t, fixture); stateAfterRead != stateBeforeRead || !fixture.tx.Authenticated() {
		t.Fatalf("initial scalar reads changed retained state or caller transaction: before=%v after=%v", stateBeforeRead, stateAfterRead)
	}

	if err := fixture.tx.Commit(); err != nil {
		t.Fatalf("caller commit initial invoice sources: %v", err)
	}
	fixture.tx = nil
	if err := fixture.workspace.Database.Close(); err != nil {
		t.Fatalf("close initial SQLCipher database: %v", err)
	}
	reopened, err := sqlcipher.Open(fixture.ctx, fixture.workspace.Path, fixture.workspace.Key)
	if err != nil {
		t.Fatalf("reopen committed SQLCipher workspace: %v", err)
	}
	fixture.workspace.Database = reopened
	t.Cleanup(func() {
		if fixture.tx != nil {
			_ = fixture.tx.Rollback()
			fixture.tx = nil
		}
		_ = reopened.Close()
	})

	fixture.tx, err = reopened.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin reopened caller transaction: %v", err)
	}
	fixture.invoiceRepository, err = NewRepository(fixture.tx)
	if err != nil {
		t.Fatalf("NewRepository(reopened transaction) = %v", err)
	}
	reopenedPort, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort(reopened transaction) = %v", err)
	}
	stateBeforeReopenedRead := invoiceReportingBudgetState(t, fixture)
	reopenedSummary, err := reopenedPort.SummarizeBASKeys(fixture.ctx, orgID)
	if err != nil || reopenedSummary == nil {
		t.Fatalf("reopened SummarizeBASKeys() = %#v, %v", reopenedSummary, err)
	}
	reopenedKeys, err := reopenedPort.ReadBASKeys(fixture.ctx, orgID, *reopenedSummary,
		reopenedSummary.ScalarKeyCount, reopenedSummary.ScalarBytes)
	if err != nil || int64(len(reopenedKeys)) != reopenedSummary.ScalarKeyCount {
		t.Fatalf("reopened ReadBASKeys() returned %d keys, %v; summary=%#v", len(reopenedKeys), err, reopenedSummary)
	}
	if !reflect.DeepEqual(*initialSummary, *reopenedSummary) || !reflect.DeepEqual(initialKeys, reopenedKeys) {
		t.Fatalf("committed scalar snapshot changed after SQLCipher reopen: summary before=%#v after=%#v keys before=%#v after=%#v",
			initialSummary, reopenedSummary, initialKeys, reopenedKeys)
	}
	if stateAfterReopenedRead := invoiceReportingBudgetState(t, fixture); stateAfterReopenedRead != stateBeforeReopenedRead || !fixture.tx.Authenticated() {
		t.Fatalf("reopened scalar reads changed retained state or caller transaction: before=%v after=%v",
			stateBeforeReopenedRead, stateAfterReopenedRead)
	}

	pending := fixture.saveDraft(t, invoiceReportingID(9402), "SCALAR-ROLLBACK-PENDING", "", 0,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	pendingSummary, err := reopenedPort.SummarizeBASKeys(fixture.ctx, orgID)
	if err != nil || pendingSummary == nil || pendingSummary.RootCount != initialSummary.RootCount+1 {
		t.Fatalf("pending caller write summary = %#v, %v; want root count to rise from %d to %d",
			pendingSummary, err, initialSummary.RootCount, initialSummary.RootCount+1)
	}
	pendingKeys, err := reopenedPort.ReadBASKeys(fixture.ctx, orgID, *pendingSummary,
		pendingSummary.ScalarKeyCount, pendingSummary.ScalarBytes)
	if err != nil || int64(len(pendingKeys)) != pendingSummary.ScalarKeyCount ||
		pendingSummary.ScalarKeyCount != initialSummary.ScalarKeyCount+1 {
		t.Fatalf("pending caller write keys = %d, %v; want one additional root key", len(pendingKeys), err)
	}
	foundPending := false
	for _, key := range pendingKeys {
		if key.Kind == BASScalarInvoice && key.InvoiceID == pending.Id && key.RootVersion != nil && *key.RootVersion == pending.Version {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatalf("pending caller transaction omitted new draft root %s: %#v", pending.Id, pendingKeys)
	}
	if !fixture.tx.Authenticated() {
		t.Fatal("scalar readers ended the caller-owned reopened transaction")
	}

	if err := fixture.tx.Rollback(); err != nil {
		t.Fatalf("caller rollback pending draft: %v", err)
	}
	fixture.tx = nil
	fixture.tx, err = reopened.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin post-rollback verification transaction: %v", err)
	}
	fixture.invoiceRepository, err = NewRepository(fixture.tx)
	if err != nil {
		t.Fatalf("NewRepository(post-rollback transaction) = %v", err)
	}
	afterRollbackPort, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort(post-rollback transaction) = %v", err)
	}
	stateBeforeFinalRead := invoiceReportingBudgetState(t, fixture)
	afterRollbackSummary, err := afterRollbackPort.SummarizeBASKeys(fixture.ctx, orgID)
	if err != nil || afterRollbackSummary == nil {
		t.Fatalf("post-rollback SummarizeBASKeys() = %#v, %v", afterRollbackSummary, err)
	}
	afterRollbackKeys, err := afterRollbackPort.ReadBASKeys(fixture.ctx, orgID, *afterRollbackSummary,
		afterRollbackSummary.ScalarKeyCount, afterRollbackSummary.ScalarBytes)
	if err != nil || int64(len(afterRollbackKeys)) != afterRollbackSummary.ScalarKeyCount {
		t.Fatalf("post-rollback ReadBASKeys() returned %d keys, %v; summary=%#v",
			len(afterRollbackKeys), err, afterRollbackSummary)
	}
	if !reflect.DeepEqual(*initialSummary, *afterRollbackSummary) || !reflect.DeepEqual(initialKeys, afterRollbackKeys) {
		t.Fatalf("caller rollback did not restore committed scalar snapshot: summary before=%#v after=%#v keys before=%#v after=%#v",
			initialSummary, afterRollbackSummary, initialKeys, afterRollbackKeys)
	}
	if stateAfterFinalRead := invoiceReportingBudgetState(t, fixture); stateAfterFinalRead != stateBeforeFinalRead || !fixture.tx.Authenticated() {
		t.Fatalf("post-rollback scalar reads changed retained state or caller transaction: before=%v after=%v",
			stateBeforeFinalRead, stateAfterFinalRead)
	}
	if err := fixture.tx.Rollback(); err != nil {
		t.Fatalf("caller rollback final read-only transaction: %v", err)
	}
	fixture.tx = nil
}
