//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceReportingCaptureReturnsNilWhenFirstEncryptedQueryCancelsContext(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	fixture.saveDraft(t, invoiceReportingID(4401), "CANCEL-DURING-QUERY", "2026-10-12", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)

	captureContext, cancel := context.WithCancel(fixture.ctx)
	t.Cleanup(cancel)
	executor := &cancelFirstInvoiceReportingQueryExecutor{
		CommandSQLExecutor: fixture.tx,
		cancel:             cancel,
	}
	port, err := NewReportingPort(executor)
	if err != nil {
		t.Fatalf("NewReportingPort() = %v", err)
	}

	capture, err := port.CaptureBAS(captureContext, orgID, invoiceReportingPeriod())
	if capture != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, ErrRepository) {
		t.Fatalf("capture after in-query cancellation = %#v, %v; want nil capture with context.Canceled and ErrRepository", capture, err)
	}
	if executor.queryCalls != 1 {
		t.Fatalf("encrypted query calls before cancellation = %d; want the first real executor query to be reached", executor.queryCalls)
	}
}

type cancelFirstInvoiceReportingQueryExecutor struct {
	app.CommandSQLExecutor
	cancel     context.CancelFunc
	queryCalls int
}

func (executor *cancelFirstInvoiceReportingQueryExecutor) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	executor.queryCalls++
	if executor.queryCalls == 1 {
		executor.cancel()
	}
	return executor.CommandSQLExecutor.QueryContext(ctx, query, arguments...)
}

var _ app.CommandSQLExecutor = (*cancelFirstInvoiceReportingQueryExecutor)(nil)
