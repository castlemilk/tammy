//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"errors"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

const invoiceNumericPreflightRegressionLimit = 8 * 1024 * 1024

func TestReportingPreflightRejectsOversizedIncomeAndAllocationCellsBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name, table, mutateSQL, typeSQL, wantType string
		needsReceipt                              bool
		arguments                                 func(invoiceID, paymentID string) []any
	}{
		{
			name:      "income-line net blob",
			table:     "invoice_income_lines_v1",
			mutateSQL: `UPDATE invoice_income_lines_v1 SET gross_minor=0,net_minor=zeroblob(?),gst_minor=0 WHERE invoice_id=? AND sequence=1`,
			typeSQL:   `SELECT typeof(net_minor),length(CAST(net_minor AS BLOB)) FROM invoice_income_lines_v1 WHERE invoice_id=?`,
			wantType:  "blob",
			arguments: func(invoiceID, _ string) []any {
				return []any{invoiceNumericPreflightRegressionLimit + 1, invoiceID}
			},
		},
		{
			name:      "income-line net text",
			table:     "invoice_income_lines_v1",
			mutateSQL: `UPDATE invoice_income_lines_v1 SET gross_minor=0,net_minor=?,gst_minor=0 WHERE invoice_id=? AND sequence=1`,
			typeSQL:   `SELECT typeof(net_minor),length(CAST(net_minor AS BLOB)) FROM invoice_income_lines_v1 WHERE invoice_id=?`,
			wantType:  "text",
			arguments: func(invoiceID, _ string) []any {
				return []any{strings.Repeat("x", invoiceNumericPreflightRegressionLimit+1), invoiceID}
			},
		},
		{
			name:      "income-line sequence blob",
			table:     "invoice_income_lines_v1",
			mutateSQL: `UPDATE invoice_income_lines_v1 SET sequence=zeroblob(?) WHERE invoice_id=? AND sequence=1`,
			typeSQL:   `SELECT typeof(sequence),length(CAST(sequence AS BLOB)) FROM invoice_income_lines_v1 WHERE invoice_id=?`,
			wantType:  "blob",
			arguments: func(invoiceID, _ string) []any {
				return []any{invoiceNumericPreflightRegressionLimit + 1, invoiceID}
			},
		},
		{
			name:         "receipt-allocation gross blob",
			table:        "invoice_payment_lines_v1",
			needsReceipt: true,
			mutateSQL:    `UPDATE invoice_payment_lines_v1 SET gross_minor=zeroblob(?),gst_minor=0 WHERE payment_id=? AND sequence=1`,
			typeSQL:      `SELECT typeof(gross_minor),length(CAST(gross_minor AS BLOB)) FROM invoice_payment_lines_v1 WHERE payment_id=?`,
			wantType:     "blob",
			arguments: func(_, paymentID string) []any {
				return []any{invoiceNumericPreflightRegressionLimit + 1, paymentID}
			},
		},
		{
			name:         "receipt-allocation gross text",
			table:        "invoice_payment_lines_v1",
			needsReceipt: true,
			mutateSQL:    `UPDATE invoice_payment_lines_v1 SET gross_minor=?,gst_minor=0 WHERE payment_id=? AND sequence=1`,
			typeSQL:      `SELECT typeof(gross_minor),length(CAST(gross_minor AS BLOB)) FROM invoice_payment_lines_v1 WHERE payment_id=?`,
			wantType:     "text",
			arguments: func(_, paymentID string) []any {
				return []any{strings.Repeat("x", invoiceNumericPreflightRegressionLimit+1), paymentID}
			},
		},
		{
			name:         "receipt-allocation sequence blob",
			table:        "invoice_payment_lines_v1",
			needsReceipt: true,
			mutateSQL:    `UPDATE invoice_payment_lines_v1 SET sequence=zeroblob(?) WHERE payment_id=? AND sequence=1`,
			typeSQL:      `SELECT typeof(sequence),length(CAST(sequence AS BLOB)) FROM invoice_payment_lines_v1 WHERE payment_id=?`,
			wantType:     "blob",
			arguments: func(_, paymentID string) []any {
				return []any{invoiceNumericPreflightRegressionLimit + 1, paymentID}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			basis := tammyv1.GstBasis_GST_BASIS_NON_CASH
			if testCase.needsReceipt {
				basis = tammyv1.GstBasis_GST_BASIS_CASH
			}
			fixture := newInvoiceReportingFixture(t, basis)
			invoice := fixture.saveDraft(t, invoiceReportingID(5701), "NUMERIC-PREFLIGHT", "2026-10-12", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
			fixture.issue(t, invoice)
			fixture.post(t, invoice)
			var paymentID string
			if testCase.needsReceipt {
				payment := fixture.recordPayment(t, invoice, 1, 1, 0)
				paymentID = payment.Id
			}

			if err := invoiceReportingMutateWithTriggersRestored(
				fixture, testCase.table, testCase.mutateSQL, testCase.arguments(invoice.Id, paymentID)...,
			); err != nil {
				t.Fatalf("install oversized %s cell: %v", testCase.name, err)
			}
			identity := invoice.Id
			if testCase.needsReceipt {
				identity = paymentID
			}
			var storedType string
			var storedBytes int
			if err := fixture.tx.QueryRowContext(fixture.ctx, testCase.typeSQL, identity).Scan(&storedType, &storedBytes); err != nil {
				t.Fatalf("inspect oversized %s cell: %v", testCase.name, err)
			}
			if storedType != testCase.wantType || storedBytes <= invoiceNumericPreflightRegressionLimit {
				t.Fatalf("stored %s cell = %s/%d bytes; want %s and more than %d bytes",
					testCase.name, storedType, storedBytes, testCase.wantType, invoiceNumericPreflightRegressionLimit)
			}

			stateBefore := invoiceReportingBudgetState(t, fixture)
			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatalf("NewReportingPort(probe) = %v", err)
			}
			preflight, preflightErr := port.PreflightBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(invoiceNumericPreflightRegressionLimit))
			if preflight != nil || !errors.Is(preflightErr, ErrRepository) || probe.hydrationQueries != 0 {
				t.Errorf("numeric-cell preflight: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					preflight != nil, errors.Is(preflightErr, ErrRepository), probe.hydrationQueries)
			}

			probe.hydrationQueries = 0
			capture, captureErr := port.CaptureBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(invoiceNumericPreflightRegressionLimit))
			if capture != nil || !errors.Is(captureErr, ErrRepository) || probe.hydrationQueries != 0 {
				t.Errorf("numeric-cell capture: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					capture != nil, errors.Is(captureErr, ErrRepository), probe.hydrationQueries)
			}
			if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
				t.Errorf("numeric-cell reporting changed revisions or retained rows: before=%v after=%v", stateBefore, stateAfter)
			}
		})
	}
}

// Disable immutable-row triggers only around the fixture mutation, then restore
// their exact sqlite_master definitions even when the update fails.
func invoiceReportingMutateWithTriggersRestored(
	fixture *invoiceReportingFixture,
	table, update string,
	arguments ...any,
) error {
	rows, err := fixture.tx.QueryContext(fixture.ctx,
		`SELECT name,sql FROM sqlite_master WHERE type='trigger' AND tbl_name=? ORDER BY name`, table)
	if err != nil {
		return err
	}
	type triggerDefinition struct{ name, sql string }
	var triggers []triggerDefinition
	for rows.Next() {
		var trigger triggerDefinition
		if err := rows.Scan(&trigger.name, &trigger.sql); err != nil {
			_ = rows.Close()
			return err
		}
		triggers = append(triggers, trigger)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return errors.Join(rowsErr, closeErr)
	}
	var dropped []triggerDefinition
	restore := func() error {
		var restoreErr error
		for _, trigger := range dropped {
			if _, err := fixture.tx.ExecContext(fixture.ctx, trigger.sql); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}
		return restoreErr
	}
	for _, trigger := range triggers {
		quotedName := `"` + strings.ReplaceAll(trigger.name, `"`, `""`) + `"`
		if _, err := fixture.tx.ExecContext(fixture.ctx, "DROP TRIGGER "+quotedName); err != nil {
			return errors.Join(err, restore())
		}
		dropped = append(dropped, trigger)
	}
	_, updateErr := fixture.tx.ExecContext(fixture.ctx, update, arguments...)
	return errors.Join(updateErr, restore())
}

func TestReportingPreflightRejectsEverySmallMistypedIntegerBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name, table, column, keyColumn string
		hasReceipt, ignoreChecks       bool
	}{
		{name: "invoice version", table: "invoices_v1", column: "version", keyColumn: "id"},
		{name: "posting invoice version", table: "invoice_income_postings_v1", column: "invoice_version", keyColumn: "invoice_id"},
		{name: "posting GST basis", table: "invoice_income_postings_v1", column: "gst_basis", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "posting invoice net", table: "invoice_income_postings_v1", column: "invoice_net_minor", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "posting invoice GST", table: "invoice_income_postings_v1", column: "invoice_gst_minor", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "posting invoice total", table: "invoice_income_postings_v1", column: "invoice_total_minor", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "posting BAS sales gross", table: "invoice_income_postings_v1", column: "bas_sales_gross_minor", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "posting profile version", table: "invoice_income_postings_v1", column: "profile_version", keyColumn: "invoice_id"},
		{name: "posting basis audit sequence", table: "invoice_income_postings_v1", column: "basis_audit_sequence", keyColumn: "invoice_id"},
		{name: "posting financial revision", table: "invoice_income_postings_v1", column: "financial_revision", keyColumn: "invoice_id"},
		{name: "income-line sequence", table: "invoice_income_lines_v1", column: "sequence", keyColumn: "invoice_id"},
		{name: "income-line gross", table: "invoice_income_lines_v1", column: "gross_minor", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "income-line net", table: "invoice_income_lines_v1", column: "net_minor", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "income-line GST", table: "invoice_income_lines_v1", column: "gst_minor", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "income-line treatment", table: "invoice_income_lines_v1", column: "treatment", keyColumn: "invoice_id", ignoreChecks: true},
		{name: "receipt amount", table: "invoice_payment_v1", column: "amount_minor", keyColumn: "id", hasReceipt: true},
		{name: "receipt allocated GST", table: "invoice_payment_v1", column: "allocated_gst_minor", keyColumn: "id", hasReceipt: true, ignoreChecks: true},
		{name: "receipt BAS sales gross", table: "invoice_payment_v1", column: "bas_sales_gross_minor", keyColumn: "id", hasReceipt: true, ignoreChecks: true},
		{name: "allocation sequence", table: "invoice_payment_lines_v1", column: "sequence", keyColumn: "payment_id", hasReceipt: true},
		{name: "allocation gross", table: "invoice_payment_lines_v1", column: "gross_minor", keyColumn: "payment_id", hasReceipt: true},
		{name: "allocation GST", table: "invoice_payment_lines_v1", column: "gst_minor", keyColumn: "payment_id", hasReceipt: true, ignoreChecks: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			basis := tammyv1.GstBasis_GST_BASIS_NON_CASH
			if testCase.hasReceipt {
				basis = tammyv1.GstBasis_GST_BASIS_CASH
			}
			fixture := newInvoiceReportingFixture(t, basis)
			invoice := fixture.saveDraft(t, invoiceReportingID(5801), "SMALL-NUMERIC-PREFLIGHT", "2026-10-12", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
			fixture.issue(t, invoice)
			fixture.post(t, invoice)
			paymentID := ""
			if testCase.hasReceipt {
				paymentID = fixture.recordPayment(t, invoice, 1, 1, 0).Id
			}
			rowID := invoice.Id
			if testCase.keyColumn == "id" && testCase.table == "invoice_payment_v1" {
				rowID = paymentID
			}
			if testCase.keyColumn == "payment_id" {
				rowID = paymentID
			}

			update := "UPDATE " + testCase.table + " SET " + testCase.column + "=zeroblob(16) WHERE " + testCase.keyColumn + "=?"
			if testCase.ignoreChecks {
				if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatalf("enable fixture-only CHECK bypass: %v", err)
				}
			}
			mutationErr := invoiceReportingMutateWithTriggersRestored(fixture, testCase.table, update, rowID)
			var resetErr error
			if testCase.ignoreChecks {
				_, resetErr = fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=OFF")
			}
			if err := errors.Join(mutationErr, resetErr); err != nil {
				t.Fatalf("install small BLOB in %s: %v", testCase.name, err)
			}

			var storedType string
			var storedBytes int
			inspect := "SELECT typeof(" + testCase.column + "),length(CAST(" + testCase.column + " AS BLOB)) FROM " + testCase.table + " WHERE " + testCase.keyColumn + "=?"
			if err := fixture.tx.QueryRowContext(fixture.ctx, inspect, rowID).Scan(&storedType, &storedBytes); err != nil {
				t.Fatalf("inspect small BLOB in %s: %v", testCase.name, err)
			}
			if storedType != "blob" || storedBytes != 16 {
				t.Fatalf("stored %s type/length = %s/%d; want blob/16", testCase.name, storedType, storedBytes)
			}

			stateBefore := invoiceReportingBudgetState(t, fixture)
			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatalf("NewReportingPort(probe) = %v", err)
			}
			preflight, preflightErr := port.PreflightBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(invoiceNumericPreflightRegressionLimit))
			if preflight != nil || !errors.Is(preflightErr, ErrRepository) || probe.hydrationQueries != 0 {
				t.Errorf("small numeric BLOB preflight: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					preflight != nil, errors.Is(preflightErr, ErrRepository), probe.hydrationQueries)
			}

			probe.hydrationQueries = 0
			capture, captureErr := port.CaptureBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(invoiceNumericPreflightRegressionLimit))
			if capture != nil || !errors.Is(captureErr, ErrRepository) || probe.hydrationQueries != 0 {
				t.Errorf("small numeric BLOB capture: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					capture != nil, errors.Is(captureErr, ErrRepository), probe.hydrationQueries)
			}
			if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
				t.Errorf("small numeric BLOB reporting changed revisions or retained rows: before=%v after=%v", stateBefore, stateAfter)
			}
		})
	}
}

func TestReportingPreflightRejectsRepresentativeRealAndTextBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name, table, column, keyColumn, expression, wantType string
		hasReceipt, ignoreChecks                             bool
	}{
		{name: "invoice REAL version", table: "invoices_v1", column: "version", keyColumn: "id", expression: "1.5", wantType: "real"},
		{name: "posting TEXT financial revision", table: "invoice_income_postings_v1", column: "financial_revision", keyColumn: "invoice_id", expression: "'x'", wantType: "text"},
		{name: "income-line REAL treatment", table: "invoice_income_lines_v1", column: "treatment", keyColumn: "invoice_id", expression: "1.5", wantType: "real"},
		{name: "receipt REAL amount", table: "invoice_payment_v1", column: "amount_minor", keyColumn: "id", expression: "1.5", wantType: "real", hasReceipt: true},
		{name: "allocation REAL sequence", table: "invoice_payment_lines_v1", column: "sequence", keyColumn: "payment_id", expression: "1.5", wantType: "real", hasReceipt: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			basis := tammyv1.GstBasis_GST_BASIS_NON_CASH
			if testCase.hasReceipt {
				basis = tammyv1.GstBasis_GST_BASIS_CASH
			}
			fixture := newInvoiceReportingFixture(t, basis)
			invoice := fixture.saveDraft(t, invoiceReportingID(5802), "SMALL-NUMERIC-PREFLIGHT", "2026-10-12", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
			fixture.issue(t, invoice)
			fixture.post(t, invoice)
			paymentID := ""
			if testCase.hasReceipt {
				paymentID = fixture.recordPayment(t, invoice, 1, 1, 0).Id
			}
			rowID := invoice.Id
			if testCase.table == "invoice_payment_v1" {
				rowID = paymentID
			}
			if testCase.keyColumn == "payment_id" {
				rowID = paymentID
			}
			update := "UPDATE " + testCase.table + " SET " + testCase.column + "=" + testCase.expression + " WHERE " + testCase.keyColumn + "=?"
			if err := invoiceReportingMutateWithTriggersRestored(fixture, testCase.table, update, rowID); err != nil {
				t.Fatalf("install representative %s cell: %v", testCase.name, err)
			}
			var storedType string
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT typeof("+testCase.column+") FROM "+testCase.table+" WHERE "+testCase.keyColumn+"=?", rowID).Scan(&storedType); err != nil {
				t.Fatalf("inspect representative %s cell: %v", testCase.name, err)
			}
			if storedType != testCase.wantType {
				t.Fatalf("stored %s type = %s; want %s", testCase.name, storedType, testCase.wantType)
			}
			stateBefore := invoiceReportingBudgetState(t, fixture)
			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatalf("NewReportingPort(probe) = %v", err)
			}
			preflight, preflightErr := port.PreflightBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(invoiceNumericPreflightRegressionLimit))
			if preflight != nil || !errors.Is(preflightErr, ErrRepository) || probe.hydrationQueries != 0 {
				t.Errorf("representative numeric type preflight: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					preflight != nil, errors.Is(preflightErr, ErrRepository), probe.hydrationQueries)
			}
			probe.hydrationQueries = 0
			capture, captureErr := port.CaptureBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(invoiceNumericPreflightRegressionLimit))
			if capture != nil || !errors.Is(captureErr, ErrRepository) || probe.hydrationQueries != 0 {
				t.Errorf("representative numeric type capture: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					capture != nil, errors.Is(captureErr, ErrRepository), probe.hydrationQueries)
			}
			if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
				t.Errorf("representative numeric reporting changed revisions or retained rows: before=%v after=%v",
					stateBefore, stateAfter)
			}
		})
	}
}

func TestInvoiceNumericPreflightPreservesCountAndByteBoundPriority(t *testing.T) {
	t.Run("count bound wins over malformed numeric storage", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		seed := fixture.saveDraft(t, invoiceReportingID(172001), "PRIORITY-SEED", "", 0,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)

		// Clone 1,000 draft roots in one SQL statement. Their proto bytes are
		// copied solely to satisfy the retained-row CHECK; preflight must stop at
		// the root count, before any copied document body is hydrated.
		if _, err := fixture.tx.ExecContext(fixture.ctx, `
			WITH RECURSIVE sequence(n) AS (
				SELECT 1 UNION ALL SELECT n + 1 FROM sequence WHERE n < 1000
			)
			INSERT INTO invoices_v1(
				id,organisation_id,version,state,number,invoice_proto,created_at,updated_at
			)
			SELECT '018f0000-0000-7000-8000-' || printf('%012d', 600000 + sequence.n),
				source.organisation_id,source.version,'DRAFT','',source.invoice_proto,
				source.created_at,source.updated_at
			FROM sequence CROSS JOIN invoices_v1 AS source WHERE source.id=?`, seed.Id); err != nil {
			t.Fatalf("clone 1,000 lightweight invoice roots: %v", err)
		}
		if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoices_v1",
			`UPDATE invoices_v1 SET version=zeroblob(16) WHERE id=?`, seed.Id); err != nil {
			t.Fatalf("install malformed numeric storage: %v", err)
		}

		stateBefore := invoiceReportingBudgetState(t, fixture)
		probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatalf("NewReportingPort(probe) = %v", err)
		}
		preflight, preflightErr := port.PreflightBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
		if preflight != nil || probe.hydrationQueries != 0 {
			t.Fatalf("count-bound preflight = %#v, %v after %d body reads",
				preflight, preflightErr, probe.hydrationQueries)
		}
		assertInvoiceNumericPriorityBound(t, preflightErr, "invoices", maxBASReportingInvoices)

		probe.hydrationQueries = 0
		capture, captureErr := port.CaptureBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
		if capture != nil || probe.hydrationQueries != 0 {
			t.Fatalf("count-bound capture = %#v, %v after %d body reads",
				capture, captureErr, probe.hydrationQueries)
		}
		assertInvoiceNumericPriorityBound(t, captureErr, "invoices", maxBASReportingInvoices)
		if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
			t.Fatalf("count-bound reporting changed retained state or transaction authentication: before=%v after=%v",
				stateBefore, stateAfter)
		}
	})

	t.Run("zero-byte bound wins over malformed numeric storage", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		seed := fixture.saveDraft(t, invoiceReportingID(173001), "PRIORITY-SEED", "", 0,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoices_v1",
			`UPDATE invoices_v1 SET version=zeroblob(16) WHERE id=?`, seed.Id); err != nil {
			t.Fatalf("install malformed numeric storage: %v", err)
		}

		stateBefore := invoiceReportingBudgetState(t, fixture)
		probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatalf("NewReportingPort(probe) = %v", err)
		}
		preflight, preflightErr := port.PreflightBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), 0)
		if preflight != nil || probe.hydrationQueries != 0 {
			t.Fatalf("zero-budget preflight = %#v, %v after %d body reads",
				preflight, preflightErr, probe.hydrationQueries)
		}
		assertInvoiceNumericPriorityBound(t, preflightErr, "capture bytes", 0)

		probe.hydrationQueries = 0
		capture, captureErr := port.CaptureBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), 0)
		if capture != nil || probe.hydrationQueries != 0 {
			t.Fatalf("zero-budget capture = %#v, %v after %d body reads",
				capture, captureErr, probe.hydrationQueries)
		}
		assertInvoiceNumericPriorityBound(t, captureErr, "capture bytes", 0)
		if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
			t.Fatalf("zero-budget reporting changed retained state or transaction authentication: before=%v after=%v",
				stateBefore, stateAfter)
		}
	})
}

func assertInvoiceNumericPriorityBound(t *testing.T, err error, resource string, limit int64) {
	t.Helper()
	var bound *SourceBoundError
	if !errors.Is(err, ErrSourceBound) || !errors.As(err, &bound) ||
		bound.Resource != resource || bound.Limit != limit {
		t.Fatalf("error = %v; want typed %q bound at %d", err, resource, limit)
	}
}
