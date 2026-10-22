//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const purchaseTaxFactReuseProofTestMarker = "/* BAS purchase tax fact reuse proof */"

type purchaseTaxFactReuseProofProbe struct {
	app.CommandSQLExecutor

	proofQueries  int
	proofColumns  []string
	bodyReads     int
	execCalls     int
	beforeProof   func()
	cancelAtProof context.CancelFunc
}

func (probe *purchaseTaxFactReuseProofProbe) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	probe.execCalls++
	return probe.CommandSQLExecutor.ExecContext(ctx, query, args...)
}

func (probe *purchaseTaxFactReuseProofProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, purchaseTaxFactReuseProofTestMarker) {
		probe.proofQueries++
		if probe.beforeProof != nil {
			before := probe.beforeProof
			probe.beforeProof = nil
			before()
		}
		if probe.cancelAtProof != nil {
			cancel := probe.cancelAtProof
			probe.cancelAtProof = nil
			cancel()
			return nil, ctx.Err()
		}
		rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
		if rows != nil {
			columns, columnsErr := rows.Columns()
			if columnsErr == nil {
				probe.proofColumns = append([]string(nil), columns...)
			}
		}
		return rows, err
	}
	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if rows == nil || err != nil {
		return rows, err
	}
	columns, columnsErr := rows.Columns()
	if columnsErr == nil {
		for _, column := range columns {
			if purchaseCoverageBodyResultColumn(column) {
				probe.bodyReads++
				break
			}
		}
	}
	return rows, err
}

func assertPurchaseTaxFactReuseProof(t *testing.T, preflight *purchases.PurchaseCoverageReportingPreflight, count, payloadBytes int64) {
	t.Helper()
	want := purchases.BASPurchaseTaxFactReuseProof{
		FactCount: count, PayloadBytes: payloadBytes, MatchedFactCount: count,
	}
	if preflight == nil {
		t.Fatal("purchase tax-fact preflight is nil")
	}
	if preflight.TaxFactCount != count || preflight.TaxFactReuseProof != want {
		t.Fatalf("purchase tax-fact reuse summary=%+v proof=%+v, want count=%d bytes=%d",
			preflight, preflight.TaxFactReuseProof, count, payloadBytes)
	}
}

func assertPurchaseTaxFactReuseAggregateColumns(t *testing.T, probe *purchaseTaxFactReuseProofProbe) {
	t.Helper()
	want := []string{"fact_count", "payload_bytes", "matched_fact_count", "mismatch_count", "invalid_owner"}
	if !reflect.DeepEqual(probe.proofColumns, want) {
		t.Fatalf("proof query returned columns %v; want aggregate-only columns %v", probe.proofColumns, want)
	}
}

func TestPurchaseCoverageTaxFactReuseProofIsAggregateAndMatchesWriterRows(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	journalSourceID, _ := f.id()
	journal := seedCoverageCashReceiptWithTaxFactSource(t, f, f.document.Id, journalSourceID)
	if journal == nil || journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED {
		t.Fatalf("manual receipt fixture is not writer-posted: %+v", journal)
	}
	stateBefore := coverageStateCounts(t, tx)
	changesBefore := postScalar(t, tx, "SELECT total_changes()")
	probe := &purchaseTaxFactReuseProofProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("preflight writer-backed manual tax fact: result=%+v err=%v", preflight, err)
	}
	if preflight.TaxFactCount != 1 || preflight.TaxFactPayloadBytes <= 0 ||
		preflight.TaxFactReuseProof.PayloadBytes != preflight.TaxFactPayloadBytes {
		t.Fatalf("preflight fact reservation/proof=%+v", preflight)
	}
	assertPurchaseTaxFactReuseProof(t, preflight, 1, preflight.TaxFactPayloadBytes)
	if probe.proofQueries != 1 || probe.bodyReads != 0 || probe.execCalls != 0 {
		t.Fatalf("proof stage queries=%d body reads=%d writes=%d; want one SQL aggregate before any body hydration and no writes",
			probe.proofQueries, probe.bodyReads, probe.execCalls)
	}
	assertPurchaseTaxFactReuseAggregateColumns(t, probe)
	if !tx.Authenticated() || !reflect.DeepEqual(stateBefore, coverageStateCounts(t, tx)) ||
		postScalar(t, tx, "SELECT total_changes()") != changesBefore {
		t.Fatal("aggregate proof changed retained state or ended the caller-owned transaction")
	}

	capture := captureCoverage(t, f.repo(tx), ctx)
	var retainedFacts int
	for _, row := range capture.Rows {
		if row.Table == "tax_facts" {
			retainedFacts++
		}
	}
	if retainedFacts != 1 || draftPurchaseCoverageTaxFactBytes(capture.Rows) != preflight.TaxFactPayloadBytes {
		t.Fatalf("independent retained-row count/charge=%d/%d, preflight=%d/%d",
			retainedFacts, draftPurchaseCoverageTaxFactBytes(capture.Rows), preflight.TaxFactCount, preflight.TaxFactPayloadBytes)
	}
	accountingRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	accountingPreflight, err := accountingRepository.PreflightTaxFactsForReportingWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || accountingPreflight == nil || accountingPreflight.FactCount != 1 {
		t.Fatalf("accounting all-fact census=%+v err=%v; want selected writer fact included", accountingPreflight, err)
	}
}

func TestPurchaseCoverageTaxFactReuseProofHandlesEmptySelectedSets(t *testing.T) {
	ctx := context.Background()

	t.Run("no selected journals avoids another aggregate", func(t *testing.T) {
		workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
		tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
		probe := &purchaseTaxFactReuseProofProbe{CommandSQLExecutor: tx}
		preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
		if err != nil || preflight == nil {
			t.Fatalf("empty-fact preflight=%+v err=%v", preflight, err)
		}
		assertPurchaseTaxFactReuseProof(t, preflight, 0, 0)
		if len(preflight.JournalIDs) != 0 || probe.proofQueries != 0 || probe.bodyReads != 0 || probe.execCalls != 0 {
			t.Fatalf("empty selection journals=%v proof=%d body=%d writes=%d", preflight.JournalIDs, probe.proofQueries, probe.bodyReads, probe.execCalls)
		}
	})

	t.Run("selected journal with no facts still runs a zero aggregate", func(t *testing.T) {
		workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
		tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
		journal := seedCoverageLegacyCashPayment(t, f, f.document.Id, "1011")
		probe := &purchaseTaxFactReuseProofProbe{CommandSQLExecutor: tx}
		preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
		if err != nil || preflight == nil {
			t.Fatalf("selected empty-fact journal preflight=%+v err=%v", preflight, err)
		}
		assertPurchaseTaxFactReuseProof(t, preflight, 0, 0)
		if !coverageHasID(preflight.JournalIDs, journal.Id) || probe.proofQueries != 1 ||
			probe.bodyReads != 0 || probe.execCalls != 0 {
			t.Fatalf("selected journal proof journals=%v queries=%d body=%d writes=%d",
				preflight.JournalIDs, probe.proofQueries, probe.bodyReads, probe.execCalls)
		}
		assertPurchaseTaxFactReuseAggregateColumns(t, probe)
	})
}

func TestPurchaseCoverageTaxFactReuseProofDoesNotExpandToUnrelatedAccountingFacts(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	unrelatedSourceID, _ := f.id()
	unrelatedJournalSourceID, _ := f.id()
	journal := seedCoverageCashReceiptWithTaxFactSource(t, f, unrelatedSourceID, unrelatedJournalSourceID)
	probe := &purchaseTaxFactReuseProofProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("purchase preflight with unrelated accounting fact=%+v err=%v", preflight, err)
	}
	assertPurchaseTaxFactReuseProof(t, preflight, 0, 0)
	if coverageHasID(preflight.JournalIDs, journal.Id) || probe.proofQueries != 0 || probe.bodyReads != 0 {
		t.Fatalf("unrelated manual fact entered purchase closure journals=%v proof queries=%d body=%d",
			preflight.JournalIDs, probe.proofQueries, probe.bodyReads)
	}
	accountingRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	accountingPreflight, err := accountingRepository.PreflightTaxFactsForReportingWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || accountingPreflight == nil || accountingPreflight.FactCount != 1 {
		t.Fatalf("accounting all-fact census=%+v err=%v; want unrelated fact retained globally", accountingPreflight, err)
	}
}

func TestPurchaseCoverageTaxFactReuseProofRejectsLateOwnerAndOversizedCellDriftBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *postingFixture)
		bound  bool
	}{
		{
			name: "late byte-equal BLOB owner",
			mutate: func(t *testing.T, f *postingFixture) {
				mutatePurchaseBASScalar(t, f, "tax_facts",
					`UPDATE tax_facts SET organisation_id=CAST(organisation_id AS BLOB) WHERE source_id=?`, f.document.Id)
			},
		},
		{
			name: "late oversized raw cell",
			mutate: func(t *testing.T, f *postingFixture) {
				mutatePurchaseBASScalar(t, f, "tax_facts",
					`UPDATE tax_facts SET tax_code=hex(zeroblob(4718593)) WHERE source_id=?`, f.document.Id)
			},
			bound: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			journalSourceID, _ := f.id()
			seedCoverageCashReceiptWithTaxFactSource(t, f, f.document.Id, journalSourceID)
			probe := &purchaseTaxFactReuseProofProbe{CommandSQLExecutor: tx}
			probe.beforeProof = func() { testCase.mutate(t, f) }
			preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if testCase.bound {
				assertPurchaseCoverageBound(t, err, "capture bytes", purchaseCoverageDraftPayloadLimit)
			} else if err == nil {
				t.Fatal("late owner drift passed the guarded aggregate")
			}
			if preflight != nil || probe.proofQueries != 1 || probe.bodyReads != 0 ||
				probe.execCalls != 0 || !tx.Authenticated() {
				t.Fatalf("drift returned partial=%v proof=%d body=%d writes=%d authenticated=%t err=%v",
					preflight != nil, probe.proofQueries, probe.bodyReads, probe.execCalls, tx.Authenticated(), err)
			}
			assertPurchaseTaxFactReuseAggregateColumns(t, probe)
		})
	}
}

func TestPurchaseCoverageTaxFactReuseProofStopsOnCancellationAtProofBoundary(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	journalSourceID, _ := f.id()
	seedCoverageCashReceiptWithTaxFactSource(t, f, f.document.Id, journalSourceID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &purchaseTaxFactReuseProofProbe{CommandSQLExecutor: tx, cancelAtProof: cancel}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if preflight != nil || !errors.Is(err, context.Canceled) || probe.proofQueries != 1 ||
		probe.bodyReads != 0 || probe.execCalls != 0 || !tx.Authenticated() {
		t.Fatalf("cancelled proof returned partial=%v err=%v queries=%d body=%d writes=%d authenticated=%t",
			preflight != nil, err, probe.proofQueries, probe.bodyReads, probe.execCalls, tx.Authenticated())
	}
}

func TestPurchaseCoverageTaxFactReuseProofPreservesHistoricalNullSourceHash(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	journal := seedCoverageLegacyCashPayment(t, f, f.document.Id, "1011")
	factID, _ := f.id()

	// Schema 12 had no source_content_hash column. This is a legacy row shape,
	// not a claim that the current tax-fact writer emits a NULL source hash.
	// Insert its nullable legacy fact before applying migration 13.
	mutatePurchaseBASScalar(t, f, "tax_facts", `
		INSERT INTO tax_facts(
			id,organisation_id,journal_line_id,tax_code,treatment,
			original_gross_minor,original_net_minor,original_gst_minor,
			attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
			remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
			tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
			source_type,source_id,source_revision,created_at
		) VALUES(?,?,?,'GST','TAXABLE',11000,10000,1000,11000,10000,1000,0,0,0,
			'tax_rule_bundle','018f0000-0000-7000-8000-000000001003',1,zeroblob(32),
			'document',?,2,?)`,
		factID, postingOrg, journal.Lines[0].Id, f.document.Id, f.now.Format(time.RFC3339Nano))
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlcipher.MigrateWorkspace(ctx, workspace.Path, workspace.Key, 13); err != nil {
		t.Fatalf("migrate nullable historical fact to schema 13: %v", err)
	}
	database, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	readTx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()

	probe := &purchaseTaxFactReuseProofProbe{CommandSQLExecutor: readTx}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("migrated NULL-hash proof=%+v err=%v", preflight, err)
	}
	assertPurchaseTaxFactReuseProof(t, preflight, 1, preflight.TaxFactPayloadBytes)
	if probe.proofQueries != 1 || probe.bodyReads != 0 || probe.execCalls != 0 ||
		!readTx.Authenticated() {
		t.Fatalf("historical proof queries=%d body=%d writes=%d authenticated=%t",
			probe.proofQueries, probe.bodyReads, probe.execCalls, readTx.Authenticated())
	}
	assertPurchaseTaxFactReuseAggregateColumns(t, probe)

	accountingRepository, err := accounting.NewJournalRepository(readTx)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := accountingRepository.CaptureTaxFactsForReporting(ctx, postingOrg)
	if err != nil || capture == nil || len(capture.Facts) != 1 {
		t.Fatalf("migrated accounting fact capture=%+v err=%v", capture, err)
	}
	if capture.Facts[0].ID != factID || capture.Facts[0].SourceContentHashPresent ||
		capture.Facts[0].SourceContentHash != nil {
		t.Fatalf("historical NULL source hash changed after proof: %+v", capture.Facts[0])
	}
	purchaseCapture := captureCoverage(t, f.repo(readTx), ctx)
	if draftPurchaseCoverageTaxFactBytes(purchaseCapture.Rows) != preflight.TaxFactPayloadBytes {
		t.Fatalf("NULL-hash purchase byte charge=%d, preflight=%d",
			draftPurchaseCoverageTaxFactBytes(purchaseCapture.Rows), preflight.TaxFactPayloadBytes)
	}
}
