//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestJournalRepositoryTaxFactSharedBudgetPreflightMatchesCaptureAndFingerprint(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	fixture.post(t, 15101, taxFactReportingID(15105), bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x42}, 32))

	baseline, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || baseline == nil || baseline.PayloadBytes <= 0 || baseline.FactCount != len(baseline.Facts) {
		t.Fatalf("default capture = %#v, %v; want its bounded payload metadata", baseline, err)
	}
	stateBefore := journalReportingPreflightState(t, fixture)

	probe := &taxFactSharedBudgetProbe{Transaction: fixture.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	preflight, err := repository.PreflightTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, baseline.PayloadBytes)
	if err != nil || preflight == nil {
		t.Fatalf("exact-budget preflight = %#v, %v", preflight, err)
	}
	if preflight.PayloadBytes != baseline.PayloadBytes || preflight.BasePayloadBytes <= 0 ||
		preflight.FactCount != len(baseline.Facts) || preflight.JournalCount != 1 || len(preflight.JournalIDs) != 1 ||
		!sort.StringsAreSorted(preflight.JournalIDs) || len(preflight.JournalPayloadBytes) != preflight.JournalCount {
		t.Fatalf("preflight reservation metadata = %#v; want exact bytes, one fact and its one unique parent", preflight)
	}
	if preflight.JournalIDs[0] != baseline.Facts[0].Journal.Id ||
		preflight.JournalPayloadBytes[preflight.JournalIDs[0]] <= 0 ||
		preflight.PayloadBytes != preflight.BasePayloadBytes+sumTaxFactSharedJournalPayload(preflight.JournalPayloadBytes) {
		t.Fatalf("preflight parent charge = %#v; want the fact base plus each unique parent charge", preflight)
	}
	if probe.bodyHydrationQueries != 0 {
		t.Fatalf("size-only preflight hydrated %d fact or journal bodies", probe.bodyHydrationQueries)
	}
	if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) {
		t.Fatalf("size-only preflight changed revisions or retained rows: before=%v after=%v", stateBefore, got)
	}

	probe.bodyHydrationQueries, probe.preflightQueries = 0, 0
	below := baseline.PayloadBytes - 1
	limited, err := repository.PreflightTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, below)
	assertTaxFactSharedBudgetBound(t, limited, err, below)
	if probe.bodyHydrationQueries != 0 {
		t.Fatalf("one-byte-short preflight hydrated %d fact or journal bodies", probe.bodyHydrationQueries)
	}

	probe.bodyHydrationQueries, probe.preflightQueries = 0, 0
	limitedCapture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, below)
	assertTaxFactSharedCaptureBound(t, limitedCapture, err, below)
	if probe.bodyHydrationQueries != 0 {
		t.Fatalf("one-byte-short capture hydrated %d fact or journal bodies", probe.bodyHydrationQueries)
	}

	budgeted, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, baseline.PayloadBytes)
	if err != nil || budgeted == nil || budgeted.PayloadBytes != baseline.PayloadBytes ||
		budgeted.BasePayloadBytes != preflight.BasePayloadBytes || budgeted.FactCount != preflight.FactCount ||
		budgeted.JournalCount != preflight.JournalCount || len(budgeted.Facts) != baseline.FactCount ||
		!bytes.Equal(budgeted.Fingerprint, baseline.Fingerprint) {
		t.Fatalf("exact-budget capture = %#v, %v; want reserved counts/bytes and the existing fingerprint", budgeted, err)
	}
	if len(budgeted.JournalIDs) != len(preflight.JournalIDs) || len(budgeted.JournalPayloadBytes) != len(preflight.JournalPayloadBytes) {
		t.Fatalf("capture parent reservation metadata = %#v; want the preflight's unique-parent charge", budgeted)
	}
	for index := range preflight.JournalIDs {
		journalID := preflight.JournalIDs[index]
		if budgeted.JournalIDs[index] != journalID || budgeted.JournalPayloadBytes[journalID] != preflight.JournalPayloadBytes[journalID] {
			t.Fatalf("capture parent reservation differs from preflight: preflight=%#v capture=%#v", preflight, budgeted)
		}
	}

	canceled, cancel := context.WithCancel(fixture.ctx)
	cancel()
	probe.bodyHydrationQueries = 0
	canceledPreflight, canceledErr := repository.PreflightTaxFactsForReportingWithPayloadBudget(
		canceled, accountTestOrganisationID, baseline.PayloadBytes)
	if canceledPreflight != nil || !errors.Is(canceledErr, context.Canceled) || probe.bodyHydrationQueries != 0 {
		t.Fatalf("canceled preflight = %#v, %v with %d body reads; want context.Canceled and no hydration",
			canceledPreflight, canceledErr, probe.bodyHydrationQueries)
	}
	if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) {
		t.Fatalf("budgeted capture or cancellation changed revisions or retained rows: before=%v after=%v", stateBefore, got)
	}
}

func TestJournalRepositoryTaxFactSharedBudgetCountsOneParentForMultipleFacts(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journalID, factID := taxFactReportingID(15201), taxFactReportingID(15203)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "shared-parent-source", taxFactReportingID(15202),
		factID, "shared-parent-evidence", "ADJUSTMENT", nil)
	if _, err := fixture.tx.ExecContext(fixture.ctx, `
		INSERT INTO tax_facts(id,organisation_id,journal_line_id,tax_code,treatment,
		 original_gross_minor,original_net_minor,original_gst_minor,
		 attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
		 remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
		 tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
		 source_type,source_id,source_revision,source_content_hash,created_at)
		SELECT ?,organisation_id,?,tax_code,treatment,
		 original_gross_minor,original_net_minor,original_gst_minor,
		 attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
		 remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
		 tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
		 source_type,source_id,source_revision,source_content_hash,created_at
		FROM tax_facts WHERE id=?`, taxFactReportingID(15206), journalID+"-balancing-line", factID); err != nil {
		t.Fatalf("add a second fact on the same journal: %v", err)
	}

	preflight, err := fixture.repository.PreflightTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
	if err != nil || preflight == nil || preflight.FactCount != 2 || preflight.JournalCount != 1 ||
		len(preflight.JournalIDs) != 1 || len(preflight.JournalPayloadBytes) != 1 {
		t.Fatalf("shared-parent preflight = %#v, %v; want two facts charged against one parent", preflight, err)
	}
	if preflight.PayloadBytes != preflight.BasePayloadBytes+sumTaxFactSharedJournalPayload(preflight.JournalPayloadBytes) ||
		preflight.JournalPayloadBytes[preflight.JournalIDs[0]] <= 0 {
		t.Fatalf("shared-parent payload accounting = %#v; want one parent charge plus the fact base", preflight)
	}
	capture, err := fixture.repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, preflight.PayloadBytes)
	if err != nil || capture == nil || capture.FactCount != 2 || capture.JournalCount != 1 ||
		len(capture.Facts) != 2 || capture.PayloadBytes != preflight.PayloadBytes ||
		capture.JournalPayloadBytes[preflight.JournalIDs[0]] != preflight.JournalPayloadBytes[preflight.JournalIDs[0]] {
		t.Fatalf("shared-parent capture = %#v, %v; want both facts and one reserved parent", capture, err)
	}
}

func TestJournalRepositoryTaxFactSharedBudgetRejectsInvalidParentsBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		breakParent func(*testing.T, *taxFactReportingFixture, suppliedHashPosting)
	}{
		{
			name: "missing line parent",
			breakParent: func(t *testing.T, fixture *taxFactReportingFixture, posted suppliedHashPosting) {
				t.Helper()
				if _, err := fixture.tx.ExecContext(fixture.ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
					t.Fatalf("defer fixture foreign keys: %v", err)
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(fixture.tx, "tax_facts",
					`UPDATE tax_facts SET journal_line_id=? WHERE id=?`, taxFactReportingID(15390), posted.factID); err != nil {
					t.Fatalf("remove tax fact parent line: %v", err)
				}
			},
		},
		{
			name: "wrong organisation owner",
			breakParent: func(t *testing.T, fixture *taxFactReportingFixture, posted suppliedHashPosting) {
				t.Helper()
				if _, err := fixture.tx.ExecContext(fixture.ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
					t.Fatalf("defer fixture foreign keys: %v", err)
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(fixture.tx, "journals",
					`UPDATE journals SET organisation_id=? WHERE id=?`, taxFactReportingID(15391), posted.journal.Id); err != nil {
					t.Fatalf("change parent journal owner: %v", err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTaxFactReportingFixture(t)
			journalID, factID := taxFactReportingID(15301), taxFactReportingID(15303)
			seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
				journalID, "SYSTEM", 0, "invalid-parent-source", taxFactReportingID(15302),
				factID, "invalid-parent-evidence", "ADJUSTMENT", nil)
			posted := suppliedHashPosting{journal: &tammyv1.Journal{Id: journalID}, factID: factID}
			testCase.breakParent(t, fixture, posted)
			stateBefore := journalReportingPreflightState(t, fixture)

			probe := &taxFactSharedBudgetProbe{Transaction: fixture.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatalf("NewJournalRepository(probe) = %v", err)
			}
			preflight, err := repository.PreflightTaxFactsForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
			if preflight != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.bodyHydrationQueries != 0 {
				t.Fatalf("invalid-parent preflight = %#v, %v with %d body reads; want a repository error before hydration",
					preflight, err, probe.bodyHydrationQueries)
			}

			probe.bodyHydrationQueries, probe.preflightQueries = 0, 0
			capture, captureErr := repository.CaptureTaxFactsForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
			if capture != nil || !errors.Is(captureErr, accounting.ErrJournalRepository) || probe.bodyHydrationQueries != 0 {
				t.Fatalf("invalid-parent capture = %#v, %v with %d body reads; want a repository error before hydration",
					capture, captureErr, probe.bodyHydrationQueries)
			}
			if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) {
				t.Fatalf("invalid-parent reservation changed revisions or retained rows: before=%v after=%v", stateBefore, got)
			}
		})
	}
}

func TestJournalRepositoryTaxFactSharedBudgetRetainsNullSourceHash(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		taxFactReportingID(15401), "SYSTEM", 0, "null-hash-source", taxFactReportingID(15402),
		taxFactReportingID(15403), "null-hash-fact", "ADJUSTMENT", nil)

	baseline, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || baseline == nil || baseline.FactCount != 1 || baseline.Facts[0].SourceContentHashPresent ||
		baseline.Facts[0].SourceContentHash != nil {
		t.Fatalf("default NULL-hash capture = %#v, %v; want NULL provenance preserved", baseline, err)
	}
	capture, err := fixture.repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, baseline.PayloadBytes)
	if err != nil || capture == nil || capture.FactCount != 1 || capture.Facts[0].SourceContentHashPresent ||
		capture.Facts[0].SourceContentHash != nil || !bytes.Equal(capture.Fingerprint, baseline.Fingerprint) {
		t.Fatalf("budgeted NULL-hash capture = %#v, %v; want NULL provenance and existing fingerprint", capture, err)
	}
}

func assertTaxFactSharedBudgetBound(t *testing.T, preflight *accounting.TaxFactReportingPreflight, err error, limit int64) {
	t.Helper()
	var bound *accounting.TaxFactReportingBoundError
	if preflight != nil || !errors.Is(err, accounting.ErrTaxFactReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != "capture bytes" || int64(bound.Limit) != limit {
		t.Fatalf("preflight result = %#v, %v; want a typed capture-bytes bound at %d", preflight, err, limit)
	}
}

func assertTaxFactSharedCaptureBound(t *testing.T, capture *accounting.TaxFactReportingCapture, err error, limit int64) {
	t.Helper()
	var bound *accounting.TaxFactReportingBoundError
	if capture != nil || !errors.Is(err, accounting.ErrTaxFactReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != "capture bytes" || int64(bound.Limit) != limit {
		t.Fatalf("capture result = %#v, %v; want a typed capture-bytes bound at %d", capture, err, limit)
	}
}

func sumTaxFactSharedJournalPayload(charges map[string]int64) int64 {
	var total int64
	for _, charge := range charges {
		total += charge
	}
	return total
}

type taxFactSharedBudgetProbe struct {
	*sqlcipher.Transaction
	bodyHydrationQueries int
	preflightQueries     int
}

func (probe *taxFactSharedBudgetProbe) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	if strings.Contains(query, "count(*)") && strings.Contains(query, "total(") {
		probe.preflightQueries++
	}
	if strings.Contains(query, "SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code") ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind") ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id") {
		probe.bodyHydrationQueries++
	}
	return probe.Transaction.QueryContext(ctx, query, arguments...)
}
