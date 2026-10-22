//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestJournalRepositoryTaxFactSharedBudgetRejectsNegativeBudget(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	preflight, preflightErr := fixture.repository.PreflightTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, -1)
	if preflight != nil || !errors.Is(preflightErr, accounting.ErrJournalRepository) ||
		errors.Is(preflightErr, accounting.ErrTaxFactReportingBound) {
		t.Fatalf("negative preflight budget = %#v, %v; want invalid repository input", preflight, preflightErr)
	}
	capture, captureErr := fixture.repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, -1)
	if capture != nil || !errors.Is(captureErr, accounting.ErrJournalRepository) ||
		errors.Is(captureErr, accounting.ErrTaxFactReportingBound) {
		t.Fatalf("negative capture budget = %#v, %v; want invalid repository input", capture, captureErr)
	}
}

func TestJournalRepositoryTaxFactSharedBudgetClampsOversizedLimit(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000015601"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "oversized-shared-budget", taxFactReportingID(15602),
		taxFactReportingID(15603), "oversized-shared-budget-fact", "ADJUSTMENT", nil)
	largeText := strings.Repeat("界", maxTaxFactSharedBudgetBytes/3+1)
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`UPDATE journals SET description=? WHERE id=? AND state='DRAFT'`, largeText, journalID); err != nil {
		t.Fatalf("set oversized journal description: %v", err)
	}

	budget := int64(maxTaxFactSharedBudgetBytes) + 1
	preflight, preflightErr := fixture.repository.PreflightTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, budget)
	assertTaxFactSharedBudgetBound(t, preflight, preflightErr, int64(maxTaxFactSharedBudgetBytes))
	capture, captureErr := fixture.repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, budget)
	assertTaxFactSharedCaptureBound(t, capture, captureErr, int64(maxTaxFactSharedBudgetBytes))
}

func TestJournalRepositoryTaxFactSharedBudgetHonorsCancellationDuringPreflight(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		taxFactReportingID(15701), "SYSTEM", 0, "late-cancel-source", taxFactReportingID(15702),
		taxFactReportingID(15703), "late-cancel-fact", "ADJUSTMENT", nil)
	ctx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	probe := &taxFactSharedBudgetExtraProbe{Transaction: fixture.tx, cancelAfterParentList: cancel}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	preflight, err := repository.PreflightTaxFactsForReportingWithPayloadBudget(
		ctx, accountTestOrganisationID, int64(maxTaxFactSharedBudgetBytes))
	if preflight != nil || !errors.Is(err, context.Canceled) || probe.bodyHydrationQueries != 0 {
		t.Fatalf("late-canceled preflight = %#v, %v with %d body reads; want cancellation before hydration",
			preflight, err, probe.bodyHydrationQueries)
	}
}

func TestJournalRepositoryTaxFactSharedBudgetRejectsFactDriftAfterPreflight(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journalID, factID := taxFactReportingID(15801), taxFactReportingID(15803)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "fact-drift-source", taxFactReportingID(15802),
		factID, "fact-drift-evidence", "ADJUSTMENT", nil)
	probe := &taxFactSharedBudgetExtraProbe{
		Transaction: fixture.tx, mutateOnFactHydration: true, targetID: factID,
	}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	capture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, int64(maxTaxFactSharedBudgetBytes))
	if capture != nil || !errors.Is(err, accounting.ErrJournalRepository) || !probe.mutated || probe.bodyHydrationQueries == 0 {
		t.Fatalf("fact-drift capture = %#v, %v (mutated=%t); want drift rejection after one hydration",
			capture, err, probe.mutated)
	}
}

func TestJournalRepositoryTaxFactSharedBudgetRejectsJournalDriftAfterPreflight(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journalID := taxFactReportingID(15901)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "journal-drift-source", taxFactReportingID(15902),
		taxFactReportingID(15903), "journal-drift-evidence", "ADJUSTMENT", nil)
	probe := &taxFactSharedBudgetExtraProbe{
		Transaction: fixture.tx, mutateOnJournalHydration: true, targetID: journalID,
	}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	capture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, int64(maxTaxFactSharedBudgetBytes))
	if capture != nil || !errors.Is(err, accounting.ErrJournalRepository) || !probe.mutated ||
		probe.headerBodyQueries != 1 || probe.lineBodyQueries != 0 {
		t.Fatalf("journal-drift capture = %#v, %v (mutated=%t, header=%d, lines=%d); want guarded header rejection before line hydration",
			capture, err, probe.mutated, probe.headerBodyQueries, probe.lineBodyQueries)
	}
}

const maxTaxFactSharedBudgetBytes = 8 * 1024 * 1024

type taxFactSharedBudgetExtraProbe struct {
	*sqlcipher.Transaction
	cancelAfterParentList    context.CancelFunc
	mutateOnFactHydration    bool
	mutateOnJournalHydration bool
	targetID                 string
	mutated                  bool
	bodyHydrationQueries     int
	headerBodyQueries        int
	lineBodyQueries          int
}

func (probe *taxFactSharedBudgetExtraProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	factHydration := strings.Contains(query, "SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code")
	journalHydration := strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind")
	lineHydration := strings.Contains(query, journalBodyMaskLinesMarker) ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id")
	if journalHydration {
		probe.headerBodyQueries++
	}
	if lineHydration {
		probe.lineBodyQueries++
	}
	if factHydration || journalHydration || lineHydration {
		probe.bodyHydrationQueries++
	}
	if factHydration && probe.mutateOnFactHydration && !probe.mutated {
		if err := taxFactSharedBudgetMutateWithTriggersDisabled(probe.Transaction, "tax_facts",
			`UPDATE tax_facts SET source_id=source_id || 'x' WHERE id=?`, probe.targetID); err != nil {
			return nil, err
		}
		probe.mutated = true
	}
	if journalHydration && probe.mutateOnJournalHydration && !probe.mutated {
		if err := taxFactSharedBudgetMutateWithTriggersDisabled(probe.Transaction, "journals",
			`UPDATE journals SET description=description || 'x' WHERE id=?`, probe.targetID); err != nil {
			return nil, err
		}
		probe.mutated = true
	}
	rows, err := probe.Transaction.QueryContext(ctx, query, arguments...)
	if err == nil && strings.Contains(query, "SELECT DISTINCT journal.id") && probe.cancelAfterParentList != nil {
		probe.cancelAfterParentList()
		probe.cancelAfterParentList = nil
	}
	return rows, err
}

func taxFactSharedBudgetMutateWithTriggersDisabled(
	tx *sqlcipher.Transaction, table, update string, arguments ...any,
) error {
	rows, err := tx.QueryContext(context.Background(),
		`SELECT name, sql FROM sqlite_master WHERE type='trigger' AND tbl_name=? ORDER BY name`, table)
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
	for _, trigger := range triggers {
		quotedName := `"` + strings.ReplaceAll(trigger.name, `"`, `""`) + `"`
		if _, err := tx.ExecContext(context.Background(), "DROP TRIGGER "+quotedName); err != nil {
			return err
		}
	}
	_, updateErr := tx.ExecContext(context.Background(), update, arguments...)
	var restoreErr error
	for _, trigger := range triggers {
		if _, err := tx.ExecContext(context.Background(), trigger.sql); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore trigger %s: %w", trigger.name, err))
		}
	}
	return errors.Join(updateErr, restoreErr)
}
