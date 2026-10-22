//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestJournalRepositoryTaxFactPreflightBoundsParentIDDrift(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		taxFactReportingID(16001), "SYSTEM", 0, "initial-parent-source",
		taxFactReportingID(16002), taxFactReportingID(16003), taxFactReportingID(16004), "ADJUSTMENT", nil)

	probe := &taxFactParentIDDriftProbe{
		Transaction: fixture.tx,
		accountIDs:  [2]string{fixture.accounts[0].Id, fixture.accounts[1].Id},
	}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}

	preflight, err := repository.PreflightTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, 8*1024*1024)
	var bound *accounting.TaxFactReportingBoundError
	if preflight != nil || !errors.Is(err, accounting.ErrTaxFactReportingBound) ||
		!errors.As(err, &bound) || bound.Resource != "journal parents" || bound.Limit != 1000 {
		t.Fatalf("preflight after parent-ID drift returned object=%t, error=%v; want nil and a journal-parents bound at 1000", preflight != nil, err)
	}
	if !probe.injected || probe.injectionErr != nil {
		t.Fatalf("parent-ID drift hook did not complete: injected=%t err=%v", probe.injected, probe.injectionErr)
	}
	if probe.bodyHydrationQueries != 0 || probe.parentHeaderPreflightQueries != 0 {
		t.Fatalf("parent-ID overflow ran %d body hydration queries and %d parent-header preflights; want zero",
			probe.bodyHydrationQueries, probe.parentHeaderPreflightQueries)
	}
}

type taxFactParentIDDriftProbe struct {
	*sqlcipher.Transaction
	injected                     bool
	injectionErr                 error
	bodyHydrationQueries         int
	parentHeaderPreflightQueries int
	accountIDs                   [2]string
}

func (probe *taxFactParentIDDriftProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	if strings.Contains(query, "SELECT DISTINCT journal.id") && !probe.injected {
		probe.injected = true
		if err := probe.insertAdditionalParents(ctx); err != nil {
			probe.injectionErr = err
			return nil, err
		}
	}
	if strings.Contains(query, "SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code") ||
		strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.Contains(query, journalBodyMaskLinesMarker) ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind") ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id") {
		probe.bodyHydrationQueries++
	}
	if strings.Contains(query, "SELECT count(*), total(") &&
		strings.Contains(query, "FROM journals WHERE id = ? AND organisation_id = ?") {
		probe.parentHeaderPreflightQueries++
	}
	return probe.Transaction.QueryContext(ctx, query, arguments...)
}

func (probe *taxFactParentIDDriftProbe) insertAdditionalParents(ctx context.Context) error {
	if _, err := probe.Transaction.ExecContext(ctx, `
		WITH RECURSIVE sequence(value) AS (
			SELECT 1 UNION ALL SELECT value + 1 FROM sequence WHERE value < 1000
		)
		INSERT INTO journals(
			id,organisation_id,source_type,source_kind,source_id,source_revision,state,
			journal_date,description,total_debits_minor,total_credits_minor,currency_code,
			financial_revision,created_at
		)
		SELECT printf('018f0000-0000-7000-8000-%012d',18000+value),?,
			'SYSTEM',0,printf('drifted-parent-source-%d',value),1,'DRAFT',
			'2026-08-10','drifted parent identity fixture',1,1,'AUD',1,?
		FROM sequence`, accountTestOrganisationID, probeCreatedAt); err != nil {
		return err
	}
	if _, err := probe.Transaction.ExecContext(ctx, `
		WITH RECURSIVE sequence(value) AS (
			SELECT 1 UNION ALL SELECT value + 1 FROM sequence WHERE value < 1000
		)
		INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code)
		SELECT printf('018f0000-0000-7000-8000-%012d',19000+value),
			printf('018f0000-0000-7000-8000-%012d',18000+value),1,?,1,0,'AUD'
		FROM sequence`, probe.accountIDs[0]); err != nil {
		return err
	}
	if _, err := probe.Transaction.ExecContext(ctx, `
		WITH RECURSIVE sequence(value) AS (
			SELECT 1 UNION ALL SELECT value + 1 FROM sequence WHERE value < 1000
		)
		INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code)
		SELECT printf('018f0000-0000-7000-8000-%012d',20000+value),
			printf('018f0000-0000-7000-8000-%012d',18000+value),2,?,0,1,'AUD'
		FROM sequence`, probe.accountIDs[1]); err != nil {
		return err
	}
	_, err := probe.Transaction.ExecContext(ctx, `
		WITH RECURSIVE sequence(value) AS (
			SELECT 1 UNION ALL SELECT value + 1 FROM sequence WHERE value < 1000
		)
		INSERT INTO tax_facts(
			id,organisation_id,journal_line_id,tax_code,treatment,
			original_gross_minor,original_net_minor,original_gst_minor,
			attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
			remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
			tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
			source_type,source_id,source_revision,created_at,source_content_hash
		)
		SELECT printf('018f0000-0000-7000-8000-%012d',21000+value),?,
			printf('018f0000-0000-7000-8000-%012d',19000+value),
			'GST','ADJUSTMENT',0,0,0,0,0,0,0,0,0,
			'retained_rule','018f0000-0000-7000-8000-000000000640',1,zeroblob(32),
			'unclassified_manual_evidence',printf('drifted-evidence-%d',value),1,?,NULL
		FROM sequence`, accountTestOrganisationID, probeCreatedAt)
	return err
}

const probeCreatedAt = "2026-08-10T12:00:00Z"
