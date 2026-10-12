//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
)

func TestJournalRepositoryPreflightForReportingWithPayloadBudgetMatchesProofWithoutHydration(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journal := postJournalReportingFixture(t, fixture, 13101)
	full, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journal.Id)
	if err != nil || full == nil || full.Journal == nil || full.PayloadBytes <= 0 {
		t.Fatalf("GetForReporting() = %#v, %v; want full proof and its positive retained-byte count", full, err)
	}

	probe := &journalReportingBudgetExecutor{Transaction: fixture.tx}
	probedRepository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	stateBefore := journalReportingPreflightState(t, fixture)
	actualBytes, err := probedRepository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, full.PayloadBytes)
	if err != nil || actualBytes != full.PayloadBytes || probe.hydrationQueries != 0 {
		t.Fatalf("exact size-only preflight = %d, %v with %d body reads; want %d, nil, and no header/line hydration",
			actualBytes, err, probe.hydrationQueries, full.PayloadBytes)
	}

	probe.preflightQueries, probe.hydrationQueries = 0, 0
	actualBytes, err = probedRepository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journal.Id, full.PayloadBytes-1)
	assertJournalReportingPreflightBound(t, actualBytes, err, full.PayloadBytes-1)
	if probe.hydrationQueries != 0 {
		t.Fatalf("one-byte-short preflight hydrated %d journal headers or lines", probe.hydrationQueries)
	}
	if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) {
		t.Fatalf("size-only posted-journal preflight changed retained rows or revisions: before=%v after=%v", stateBefore, got)
	}
}

func TestJournalRepositoryPreflightForReportingCountsUTF8HeaderBeforeHydration(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000013201"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "utf8-preflight-source", taxFactReportingID(13202),
		taxFactReportingID(13203), "utf8-preflight-fact", "ADJUSTMENT", nil)
	largeUTF8 := strings.Repeat("界", 1<<20)
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`UPDATE journals SET description=? WHERE id=? AND state='DRAFT'`, largeUTF8, journalID); err != nil {
		t.Fatalf("set UTF-8 raw journal description: %v", err)
	}

	stateBefore := journalReportingPreflightState(t, fixture)
	maximum := int64(journalReportingCaptureByteLimit)
	actualBytes, err := fixture.repository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journalID, maximum)
	if err != nil || actualBytes <= 2*1024*1024 || actualBytes >= maximum {
		t.Fatalf("UTF-8 retained-byte preflight = %d, %v; want actual UTF-8 bytes above two MiB and within the normal limit", actualBytes, err)
	}

	probe := &journalReportingBudgetExecutor{Transaction: fixture.tx}
	probedRepository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	limitedBytes, err := probedRepository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journalID, 2*1024*1024)
	assertJournalReportingPreflightBound(t, limitedBytes, err, 2*1024*1024)
	if probe.hydrationQueries != 0 {
		t.Fatalf("oversized UTF-8 header preflight hydrated %d journal headers or lines", probe.hydrationQueries)
	}
	if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) {
		t.Fatalf("UTF-8 size-only preflight changed retained rows or revisions: before=%v after=%v", stateBefore, got)
	}
}

func TestJournalRepositoryPreflightForReportingBindsSourceAndHonorsCancellation(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journal := postJournalReportingFixture(t, fixture, 13301)
	probe := &journalReportingBudgetExecutor{Transaction: fixture.tx}
	probedRepository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	stateBefore := journalReportingPreflightState(t, fixture)
	maximum := int64(journalReportingCaptureByteLimit)

	foreignBytes, foreignErr := probedRepository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, taxFactReportingID(13399), journal.Id, maximum)
	if foreignBytes != 0 || !errors.Is(foreignErr, accounting.ErrJournalRepository) {
		t.Fatalf("foreign-organisation preflight = %d, %v; want zero bytes and ErrJournalRepository", foreignBytes, foreignErr)
	}
	missingBytes, missingErr := probedRepository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, taxFactReportingID(13398), maximum)
	if missingBytes != 0 || !errors.Is(missingErr, accounting.ErrJournalRepository) {
		t.Fatalf("missing-journal preflight = %d, %v; want zero bytes and ErrJournalRepository", missingBytes, missingErr)
	}
	canceled, cancel := context.WithCancel(fixture.ctx)
	cancel()
	canceledBytes, canceledErr := probedRepository.PreflightForReportingWithPayloadBudget(
		canceled, accountTestOrganisationID, journal.Id, maximum)
	if canceledBytes != 0 || !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("canceled preflight = %d, %v; want zero bytes and context.Canceled", canceledBytes, canceledErr)
	}
	if probe.hydrationQueries != 0 {
		t.Fatalf("foreign, missing, or canceled preflights hydrated %d journal headers or lines", probe.hydrationQueries)
	}
	if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) {
		t.Fatalf("source-validation preflight changed retained rows or revisions: before=%v after=%v", stateBefore, got)
	}
}

func assertJournalReportingPreflightBound(t *testing.T, payloadBytes int64, err error, limit int64) {
	t.Helper()
	var bound *accounting.TaxFactReportingBoundError
	if payloadBytes != 0 || !errors.Is(err, accounting.ErrTaxFactReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != "capture bytes" || int64(bound.Limit) != limit {
		t.Fatalf("preflight result = %d, %v; want zero bytes and typed capture-bytes bound at %d", payloadBytes, err, limit)
	}
}

func journalReportingPreflightState(t *testing.T, fixture *taxFactReportingFixture) []int64 {
	t.Helper()
	var state = make([]int64, 8)
	err := fixture.tx.QueryRowContext(fixture.ctx, `SELECT financial_revision,ledger_revision,settlement_revision,
		banking_revision,tax_source_revision,organisation_profile_revision,rule_bundle_revision FROM financial_revisions WHERE id=1`).Scan(
		&state[0], &state[1], &state[2], &state[3], &state[4], &state[5], &state[6])
	if err != nil {
		t.Fatalf("read preflight revision snapshot: %v", err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx, `SELECT total_changes()`).Scan(&state[7]); err != nil {
		t.Fatalf("read preflight total_changes snapshot: %v", err)
	}
	return state
}

func equalJournalReportingPreflightState(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
