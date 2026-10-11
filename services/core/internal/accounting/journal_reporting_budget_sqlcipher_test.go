//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestJournalRepositoryGetForReportingWithPayloadBudgetUsesActualRetainedBytes(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journal := postJournalReportingFixture(t, fixture, 12601)

	full, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, int64(journalReportingCaptureByteLimit))
	if err != nil || full == nil || full.Journal == nil || len(full.ContentHash) != sha256.Size ||
		full.PayloadBytes <= 0 || full.PayloadBytes > int64(journalReportingCaptureByteLimit) {
		t.Fatalf("full budgeted journal proof = %#v, %v; want bounded proof with positive retained bytes", full, err)
	}

	exact, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, full.PayloadBytes)
	if err != nil || exact == nil || exact.PayloadBytes != full.PayloadBytes || !bytes.Equal(exact.ContentHash, full.ContentHash) {
		t.Fatalf("exact retained-byte budget = %#v, %v; want stable actual payload bytes/hash from full read", exact, err)
	}

	probe := &journalReportingBudgetExecutor{Transaction: fixture.tx}
	probedRepository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	probed, err := probedRepository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, full.PayloadBytes)
	if err != nil || probed == nil || probe.hydrationQueries == 0 {
		t.Fatalf("successful budgeted probe = %#v, %v; want an observed hydration query", probed, err)
	}
	probe.preflightQueries, probe.hydrationQueries = 0, 0
	below := full.PayloadBytes - 1
	limited, err := probedRepository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, below)
	assertJournalReportingCallerBudgetBound(t, limited, err)
	if probe.preflightQueries == 0 || probe.hydrationQueries != 0 {
		t.Fatalf("under-budget lookup ran %d preflights and %d hydration queries; want preflight rejection before hydration",
			probe.preflightQueries, probe.hydrationQueries)
	}
}

func TestJournalRepositoryGetForReportingWithPayloadBudgetValidatesBudgetAndContext(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journal := postJournalReportingFixture(t, fixture, 12701)

	zero, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, 0)
	assertJournalReportingCallerBudgetBound(t, zero, err)

	negative, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, -1)
	if negative != nil || !errors.Is(err, accounting.ErrJournalRepository) ||
		errors.Is(err, accounting.ErrTaxFactReportingBound) {
		t.Fatalf("negative payload budget = %#v, %v; want nil and ErrJournalRepository", negative, err)
	}

	canceled, cancel := context.WithCancel(fixture.ctx)
	cancel()
	capture, err := fixture.repository.GetForReportingWithPayloadBudget(
		canceled, accountTestOrganisationID, journal.Id, int64(journalReportingCaptureByteLimit))
	if capture != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled budgeted read = %#v, %v; want nil and context.Canceled", capture, err)
	}

	foreign, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, taxFactReportingID(12799), journal.Id, int64(journalReportingCaptureByteLimit))
	if foreign != nil || err == nil {
		t.Fatalf("wrong-organisation budgeted read = %#v, %v; want nil proof and an error", foreign, err)
	}
}

func TestJournalRepositoryGetForReportingWithPayloadBudgetCountsUTF8BeforeHydration(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000012801"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "utf8-budget-source", taxFactReportingID(12802),
		taxFactReportingID(12803), "utf8-budget-fact", "ADJUSTMENT", nil)
	// The source is three MiB in UTF-8 bytes but only one MiB in Unicode code
	// points. It remains below the normal eight MiB limit while exceeding a two
	// MiB caller budget if SQL counts retained bytes correctly.
	largeUTF8 := strings.Repeat("界", 1<<20)
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`UPDATE journals SET description=? WHERE id=? AND state='DRAFT'`, largeUTF8, journalID); err != nil {
		t.Fatalf("set UTF-8 raw parent description: %v", err)
	}
	full, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journalID, int64(journalReportingCaptureByteLimit))
	if err != nil || full == nil || full.State != "DRAFT" || full.PayloadBytes <= 0 ||
		full.PayloadBytes >= int64(journalReportingCaptureByteLimit) {
		t.Fatalf("UTF-8 DRAFT proof = %#v, %v; want complete proof within the normal payload limit", full, err)
	}

	probe := &journalReportingBudgetExecutor{Transaction: fixture.tx}
	probedRepository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	limited, err := probedRepository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journalID, 2*1024*1024)
	assertJournalReportingCallerBudgetBound(t, limited, err)
	if probe.preflightQueries == 0 || probe.hydrationQueries != 0 {
		t.Fatalf("UTF-8 under-budget lookup ran %d preflights and %d hydration queries; want byte-bound rejection before hydration",
			probe.preflightQueries, probe.hydrationQueries)
	}
}

func TestJournalRepositoryGetForReportingWithPayloadBudgetClampsOversizedCallerBudget(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000012901"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "oversized-caller-budget-source", taxFactReportingID(12902),
		taxFactReportingID(12903), "oversized-caller-budget-fact", "ADJUSTMENT", nil)
	largeText := strings.Repeat("界", journalReportingCaptureByteLimit/3+1)
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`UPDATE journals SET description=? WHERE id=? AND state='DRAFT'`, largeText, journalID); err != nil {
		t.Fatalf("set oversized UTF-8 raw parent description: %v", err)
	}

	budget := int64(journalReportingCaptureByteLimit) + 1
	proof, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journalID, budget)
	assertJournalReportingBound(t, proof, err, "capture bytes", journalReportingCaptureByteLimit)
}

type journalReportingBudgetExecutor struct {
	*sqlcipher.Transaction
	preflightQueries int
	hydrationQueries int
}

func (executor *journalReportingBudgetExecutor) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	if strings.Contains(query, "count(*)") && strings.Contains(query, "total(") {
		executor.preflightQueries++
	}
	if strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind") ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id") {
		executor.hydrationQueries++
	}
	return executor.Transaction.QueryContext(ctx, query, arguments...)
}

func assertJournalReportingCallerBudgetBound(
	t *testing.T,
	record *accounting.JournalReportingRecord,
	err error,
) {
	t.Helper()
	var bound *accounting.TaxFactReportingBoundError
	if record != nil || !errors.Is(err, accounting.ErrTaxFactReportingBound) ||
		!errors.As(err, &bound) || bound.Resource != "capture bytes" {
		t.Fatalf("caller-budget reporting proof = %#v, %v; want nil and a typed capture-bytes bound", record, err)
	}
}
