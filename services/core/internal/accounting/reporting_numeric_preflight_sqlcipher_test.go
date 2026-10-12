//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
)

const numericPreflightRegressionLimit = 8 * 1024 * 1024

func TestJournalRepositoryTaxFactNumericPreflightRejectsOversizedStorageBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name, mutateSQL, typeSQL, wantType string
		arguments                          func(string) []any
	}{
		{
			name:      "amount blob",
			mutateSQL: `UPDATE tax_facts SET original_gross_minor=zeroblob(?) WHERE id=?`,
			typeSQL:   `SELECT typeof(original_gross_minor), length(CAST(original_gross_minor AS BLOB)) FROM tax_facts WHERE id=?`,
			wantType:  "blob",
			arguments: func(id string) []any { return []any{numericPreflightRegressionLimit + 1, id} },
		},
		{
			name:      "revision text",
			mutateSQL: `UPDATE tax_facts SET source_revision=? WHERE id=?`,
			typeSQL:   `SELECT typeof(source_revision), length(CAST(source_revision AS BLOB)) FROM tax_facts WHERE id=?`,
			wantType:  "text",
			arguments: func(id string) []any {
				return []any{strings.Repeat("x", numericPreflightRegressionLimit+1), id}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTaxFactReportingFixture(t)
			journalID, factLineID, factID := taxFactReportingID(16101), taxFactReportingID(16102), taxFactReportingID(16103)
			seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
				journalID, "SYSTEM", 0, "numeric-preflight-source", factLineID,
				factID, "numeric-preflight-fact", "ADJUSTMENT", nil)
			if err := taxFactSharedBudgetMutateWithTriggersDisabled(
				fixture.tx, "tax_facts", testCase.mutateSQL, testCase.arguments(factID)...,
			); err != nil {
				t.Fatalf("install oversized %s cell: %v", testCase.name, err)
			}

			var storedType string
			var storedBytes int
			if err := fixture.tx.QueryRowContext(fixture.ctx, testCase.typeSQL, factID).Scan(&storedType, &storedBytes); err != nil {
				t.Fatalf("inspect oversized %s cell: %v", testCase.name, err)
			}
			if storedType != testCase.wantType || storedBytes <= numericPreflightRegressionLimit {
				t.Fatalf("stored %s cell = %s/%d bytes; want %s and more than %d bytes",
					testCase.name, storedType, storedBytes, testCase.wantType, numericPreflightRegressionLimit)
			}

			stateBefore := journalReportingPreflightState(t, fixture)
			probe := &taxFactSharedBudgetProbe{Transaction: fixture.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatalf("NewJournalRepository(probe) = %v", err)
			}

			preflight, preflightErr := repository.PreflightTaxFactsForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, int64(numericPreflightRegressionLimit))
			if preflight != nil || !errors.Is(preflightErr, accounting.ErrJournalRepository) || probe.bodyHydrationQueries != 0 {
				t.Errorf("numeric-cell preflight: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					preflight != nil, errors.Is(preflightErr, accounting.ErrJournalRepository), probe.bodyHydrationQueries)
			}

			probe.bodyHydrationQueries, probe.preflightQueries = 0, 0
			capture, captureErr := repository.CaptureTaxFactsForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, int64(numericPreflightRegressionLimit))
			if capture != nil || !errors.Is(captureErr, accounting.ErrJournalRepository) || probe.bodyHydrationQueries != 0 {
				t.Errorf("numeric-cell capture: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					capture != nil, errors.Is(captureErr, accounting.ErrJournalRepository), probe.bodyHydrationQueries)
			}
			if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) || !fixture.tx.Authenticated() {
				t.Errorf("numeric-cell reporting changed revisions or retained rows: before=%v after=%v", stateBefore, got)
			}
		})
	}
}

func TestJournalRepositoryParentNumericPreflightRejectsOversizedStorageBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name, table, mutateSQL, typeSQL, wantType string
		arguments                                 func(string) []any
	}{
		{
			name:      "header version blob",
			table:     "journals",
			mutateSQL: `UPDATE journals SET version=zeroblob(?) WHERE id=?`,
			typeSQL:   `SELECT typeof(version), length(CAST(version AS BLOB)) FROM journals WHERE id=?`,
			wantType:  "blob",
			arguments: func(id string) []any { return []any{numericPreflightRegressionLimit + 1, id} },
		},
		{
			name:      "line debit blob",
			table:     "journal_lines",
			mutateSQL: `UPDATE journal_lines SET debit_minor=zeroblob(?) WHERE id=?`,
			typeSQL:   `SELECT typeof(debit_minor), length(CAST(debit_minor AS BLOB)) FROM journal_lines WHERE id=?`,
			wantType:  "blob",
			arguments: func(id string) []any {
				return []any{numericPreflightRegressionLimit + 1, id}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTaxFactReportingFixture(t)
			journalID, factLineID := taxFactReportingID(16201), taxFactReportingID(16202)
			seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
				journalID, "SYSTEM", 0, "parent-numeric-preflight-source", factLineID,
				taxFactReportingID(16203), "parent-numeric-preflight-fact", "ADJUSTMENT", nil)
			rowID := journalID
			if testCase.table == "journal_lines" {
				rowID = factLineID
			}
			if err := taxFactSharedBudgetMutateWithTriggersDisabled(
				fixture.tx, testCase.table, testCase.mutateSQL, testCase.arguments(rowID)...,
			); err != nil {
				t.Fatalf("install oversized %s cell: %v", testCase.name, err)
			}

			var storedType string
			var storedBytes int
			if err := fixture.tx.QueryRowContext(fixture.ctx, testCase.typeSQL, rowID).Scan(&storedType, &storedBytes); err != nil {
				t.Fatalf("inspect oversized %s cell: %v", testCase.name, err)
			}
			if storedType != testCase.wantType || storedBytes <= numericPreflightRegressionLimit {
				t.Fatalf("stored %s cell = %s/%d bytes; want %s and more than %d bytes",
					testCase.name, storedType, storedBytes, testCase.wantType, numericPreflightRegressionLimit)
			}

			stateBefore := journalReportingPreflightState(t, fixture)
			probe := &journalReportingBudgetExecutor{Transaction: fixture.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatalf("NewJournalRepository(probe) = %v", err)
			}
			preflightBytes, preflightErr := repository.PreflightForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, journalID, int64(numericPreflightRegressionLimit))
			if preflightBytes != 0 || !errors.Is(preflightErr, accounting.ErrJournalRepository) || probe.hydrationQueries != 0 {
				t.Errorf("parent numeric preflight: bytes=%d repositoryError=%t bodyReads=%d; want zero, repository error and no hydration",
					preflightBytes, errors.Is(preflightErr, accounting.ErrJournalRepository), probe.hydrationQueries)
			}

			probe.preflightQueries, probe.hydrationQueries = 0, 0
			record, captureErr := repository.GetForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, journalID, int64(numericPreflightRegressionLimit))
			if record != nil || !errors.Is(captureErr, accounting.ErrJournalRepository) || probe.hydrationQueries != 0 {
				t.Errorf("parent numeric capture: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					record != nil, errors.Is(captureErr, accounting.ErrJournalRepository), probe.hydrationQueries)
			}
			if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) || !fixture.tx.Authenticated() {
				t.Errorf("parent numeric reporting changed revisions or retained rows: before=%v after=%v", stateBefore, got)
			}
		})
	}
}

func TestJournalRepositoryRejectsRemainingMistypedAccountingIntegersBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name, table, column, keyColumn, expression, extraSet, wantType string
		keyFromLine, ignoreChecks                                      bool
	}{
		{name: "tax fact original net", table: "tax_facts", column: "original_net_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact original GST", table: "tax_facts", column: "original_gst_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact attributed gross", table: "tax_facts", column: "attributed_gross_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact attributed net", table: "tax_facts", column: "attributed_net_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact attributed GST", table: "tax_facts", column: "attributed_gst_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact remaining gross", table: "tax_facts", column: "remaining_gross_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact remaining net", table: "tax_facts", column: "remaining_net_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact remaining GST", table: "tax_facts", column: "remaining_gst_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "tax fact tax-rule revision", table: "tax_facts", column: "tax_rule_revision", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "journal source kind", table: "journals", column: "source_kind", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob", ignoreChecks: true},
		{name: "journal source revision", table: "journals", column: "source_revision", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "journal debit total", table: "journals", column: "total_debits_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "journal credit total", table: "journals", column: "total_credits_minor", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "journal financial revision", table: "journals", column: "financial_revision", keyColumn: "id", expression: "zeroblob(16)", wantType: "blob"},
		{name: "journal line credit", table: "journal_lines", column: "credit_minor", keyColumn: "id", keyFromLine: true, expression: "zeroblob(16)", wantType: "blob"},
		{name: "nullable journal-line tax amount", table: "journal_lines", column: "tax_amount_minor", keyColumn: "id", keyFromLine: true, expression: "zeroblob(16)", extraSet: ",tax_code_id='GST',tax_rule_id='numeric-preflight-rule'", wantType: "blob"},
		{name: "nullable journal-line tax-rule revision", table: "journal_lines", column: "tax_rule_revision", keyColumn: "id", keyFromLine: true, expression: "zeroblob(16)", wantType: "blob"},
		// BLOB/TEXT line_number values are already rejected by the existing
		// MaxUint32 comparison. REAL 1.5 isolates the remaining typeof gap.
		{name: "journal line fractional sequence", table: "journal_lines", column: "line_number", keyColumn: "id", keyFromLine: true, expression: "1.5", wantType: "real"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTaxFactReportingFixture(t)
			journalID, factLineID, factID := taxFactReportingID(16301), taxFactReportingID(16302), taxFactReportingID(16303)
			seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
				journalID, "SYSTEM", 0, "small-numeric-preflight-source", factLineID,
				factID, "small-numeric-preflight-fact", "ADJUSTMENT", nil)

			rowID := factID
			if testCase.table == "journals" {
				rowID = journalID
			}
			if testCase.keyFromLine {
				rowID = factLineID
				if testCase.column == "credit_minor" {
					rowID = journalID + "-balancing-line"
				}
			}
			update := "UPDATE " + testCase.table + " SET " + testCase.column + "=" + testCase.expression + testCase.extraSet + " WHERE " + testCase.keyColumn + "=?"
			if testCase.ignoreChecks {
				if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatalf("enable fixture-only CHECK bypass: %v", err)
				}
			}
			mutationErr := taxFactSharedBudgetMutateWithTriggersDisabled(
				fixture.tx, testCase.table, update, rowID)
			var resetErr error
			if testCase.ignoreChecks {
				_, resetErr = fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=OFF")
			}
			if err := errors.Join(mutationErr, resetErr); err != nil {
				t.Fatalf("install small mis-typed %s cell: %v", testCase.name, err)
			}

			var storedType string
			var storedBytes int
			inspect := "SELECT typeof(" + testCase.column + "),length(CAST(" + testCase.column + " AS BLOB)) FROM " + testCase.table + " WHERE " + testCase.keyColumn + "=?"
			if err := fixture.tx.QueryRowContext(fixture.ctx, inspect, rowID).Scan(&storedType, &storedBytes); err != nil {
				t.Fatalf("inspect small mis-typed %s cell: %v", testCase.name, err)
			}
			if storedType != testCase.wantType || testCase.wantType == "blob" && storedBytes != 16 {
				t.Fatalf("stored %s type/length = %s/%d; want %s and, for BLOB, 16 bytes",
					testCase.name, storedType, storedBytes, testCase.wantType)
			}

			stateBefore := journalReportingPreflightState(t, fixture)
			probe := &taxFactSharedBudgetProbe{Transaction: fixture.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatalf("NewJournalRepository(probe) = %v", err)
			}
			preflight, preflightErr := repository.PreflightTaxFactsForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, int64(numericPreflightRegressionLimit))
			if preflight != nil || !errors.Is(preflightErr, accounting.ErrJournalRepository) || probe.bodyHydrationQueries != 0 {
				t.Errorf("small numeric storage preflight: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					preflight != nil, errors.Is(preflightErr, accounting.ErrJournalRepository), probe.bodyHydrationQueries)
			}

			probe.bodyHydrationQueries, probe.preflightQueries = 0, 0
			capture, captureErr := repository.CaptureTaxFactsForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, int64(numericPreflightRegressionLimit))
			if capture != nil || !errors.Is(captureErr, accounting.ErrJournalRepository) || probe.bodyHydrationQueries != 0 {
				t.Errorf("small numeric storage capture: resultPresent=%t repositoryError=%t bodyReads=%d; want nil, repository error and no hydration",
					capture != nil, errors.Is(captureErr, accounting.ErrJournalRepository), probe.bodyHydrationQueries)
			}
			if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) || !fixture.tx.Authenticated() {
				t.Errorf("small numeric storage reporting changed revisions or retained rows: before=%v after=%v",
					stateBefore, got)
			}
		})
	}
}

func TestTaxFactNumericPreflightPreservesCountAndByteBoundPriority(t *testing.T) {
	t.Run("count bound wins over malformed numeric storage", func(t *testing.T) {
		fixture := newTaxFactReportingFixture(t)
		journalID, lineID, factID := taxFactReportingID(170001), taxFactReportingID(170002), taxFactReportingID(170003)
		seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
			journalID, "SYSTEM", 0, "numeric-priority-source", lineID,
			factID, "numeric-priority-fact", "ADJUSTMENT", nil)

		// One journal and two set-based inserts produce 1,001 fact roots without
		// invoking posting/fixture writers for each row. The distinct journal
		// lines satisfy tax_facts.journal_line_id's unique foreign key.
		const additional = 1000
		idPrefix := "018f0000-0000-7000-8000-"
		if _, err := fixture.tx.ExecContext(fixture.ctx, `
			WITH RECURSIVE sequence(n) AS (
				SELECT 1 UNION ALL SELECT n + 1 FROM sequence WHERE n < ?
			)
			INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code)
			SELECT ? || printf('%012d', 300000 + sequence.n), ?, sequence.n + 2, ?, 1, 0, 'AUD'
			FROM sequence`, additional, idPrefix, journalID, fixture.accounts[0].Id); err != nil {
			t.Fatalf("insert 1,000 fixture journal lines: %v", err)
		}
		if _, err := fixture.tx.ExecContext(fixture.ctx, `
			WITH RECURSIVE sequence(n) AS (
				SELECT 1 UNION ALL SELECT n + 1 FROM sequence WHERE n < ?
			)
			INSERT INTO tax_facts(
				id,organisation_id,journal_line_id,tax_code,treatment,
				original_gross_minor,original_net_minor,original_gst_minor,
				attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
				remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
				tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
				source_type,source_id,source_revision,created_at,source_content_hash
			)
			SELECT ? || printf('%012d', 400000 + sequence.n), base.organisation_id,
				? || printf('%012d', 300000 + sequence.n), base.tax_code, base.treatment,
				base.original_gross_minor, base.original_net_minor, base.original_gst_minor,
				base.attributed_gross_minor, base.attributed_net_minor, base.attributed_gst_minor,
				base.remaining_gross_minor, base.remaining_net_minor, base.remaining_gst_minor,
				base.tax_rule_type, base.tax_rule_id, base.tax_rule_revision, base.tax_rule_content_hash,
				base.source_type, base.source_id, base.source_revision, base.created_at, base.source_content_hash
			FROM sequence CROSS JOIN tax_facts AS base WHERE base.id = ?`,
			additional, idPrefix, idPrefix, factID); err != nil {
			t.Fatalf("insert 1,000 fixture tax facts: %v", err)
		}
		if err := taxFactSharedBudgetMutateWithTriggersDisabled(fixture.tx, "tax_facts",
			`UPDATE tax_facts SET original_gross_minor=zeroblob(16) WHERE id=?`, factID); err != nil {
			t.Fatalf("install malformed numeric storage: %v", err)
		}

		stateBefore := journalReportingPreflightState(t, fixture)
		probe := &taxFactSharedBudgetProbe{Transaction: fixture.tx}
		repository, err := accounting.NewJournalRepository(probe)
		if err != nil {
			t.Fatalf("NewJournalRepository(probe) = %v", err)
		}
		preflight, preflightErr := repository.PreflightTaxFactsForReportingWithPayloadBudget(
			fixture.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
		if preflight != nil || probe.bodyHydrationQueries != 0 || probe.preflightQueries == 0 {
			t.Fatalf("count-bound preflight = %#v, %v after %d body reads and %d preflight queries",
				preflight, preflightErr, probe.bodyHydrationQueries, probe.preflightQueries)
		}
		assertTaxFactNumericPriorityBound(t, preflightErr, "tax facts", 1000)

		probe.bodyHydrationQueries, probe.preflightQueries = 0, 0
		capture, captureErr := repository.CaptureTaxFactsForReportingWithPayloadBudget(
			fixture.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
		if capture != nil || probe.bodyHydrationQueries != 0 || probe.preflightQueries == 0 {
			t.Fatalf("count-bound capture = %#v, %v after %d body reads and %d preflight queries",
				capture, captureErr, probe.bodyHydrationQueries, probe.preflightQueries)
		}
		assertTaxFactNumericPriorityBound(t, captureErr, "tax facts", 1000)
		if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) || !fixture.tx.Authenticated() {
			t.Fatalf("count-bound reporting changed retained state or transaction authentication: before=%v after=%v",
				stateBefore, got)
		}
	})

	t.Run("zero-byte bound wins over malformed numeric storage", func(t *testing.T) {
		fixture := newTaxFactReportingFixture(t)
		journalID, lineID, factID := taxFactReportingID(171001), taxFactReportingID(171002), taxFactReportingID(171003)
		seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
			journalID, "SYSTEM", 0, "numeric-priority-source", lineID,
			factID, "numeric-priority-fact", "ADJUSTMENT", nil)
		if err := taxFactSharedBudgetMutateWithTriggersDisabled(fixture.tx, "tax_facts",
			`UPDATE tax_facts SET original_gross_minor=zeroblob(16) WHERE id=?`, factID); err != nil {
			t.Fatalf("install malformed numeric storage: %v", err)
		}

		stateBefore := journalReportingPreflightState(t, fixture)
		probe := &taxFactSharedBudgetProbe{Transaction: fixture.tx}
		repository, err := accounting.NewJournalRepository(probe)
		if err != nil {
			t.Fatalf("NewJournalRepository(probe) = %v", err)
		}
		preflight, preflightErr := repository.PreflightTaxFactsForReportingWithPayloadBudget(
			fixture.ctx, accountTestOrganisationID, 0)
		if preflight != nil || probe.bodyHydrationQueries != 0 || probe.preflightQueries == 0 {
			t.Fatalf("zero-budget preflight = %#v, %v after %d body reads and %d preflight queries",
				preflight, preflightErr, probe.bodyHydrationQueries, probe.preflightQueries)
		}
		assertTaxFactNumericPriorityBound(t, preflightErr, "capture bytes", 0)

		probe.bodyHydrationQueries, probe.preflightQueries = 0, 0
		capture, captureErr := repository.CaptureTaxFactsForReportingWithPayloadBudget(
			fixture.ctx, accountTestOrganisationID, 0)
		if capture != nil || probe.bodyHydrationQueries != 0 || probe.preflightQueries == 0 {
			t.Fatalf("zero-budget capture = %#v, %v after %d body reads and %d preflight queries",
				capture, captureErr, probe.bodyHydrationQueries, probe.preflightQueries)
		}
		assertTaxFactNumericPriorityBound(t, captureErr, "capture bytes", 0)
		if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) || !fixture.tx.Authenticated() {
			t.Fatalf("zero-budget reporting changed retained state or transaction authentication: before=%v after=%v",
				stateBefore, got)
		}
	})
}

func assertTaxFactNumericPriorityBound(t *testing.T, err error, resource string, limit int) {
	t.Helper()
	var bound *accounting.TaxFactReportingBoundError
	if !errors.Is(err, accounting.ErrTaxFactReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != resource || bound.Limit != limit {
		t.Fatalf("error = %v; want typed %q bound at %d", err, resource, limit)
	}
}
