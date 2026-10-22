//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func purchaseReferenceScalarJournalDiscoveryQuery(query, table, alias string, preflight bool) bool {
	compact := purchaseReferenceCompactSQL(query)
	if strings.Contains(compact, "total(payload_bytes)") != preflight ||
		!strings.Contains(compact, "from"+table+"as"+alias) ||
		!strings.Contains(compact, alias+".journal_id") {
		return false
	}
	if table == "opening_conversions" {
		if preflight {
			return strings.Contains(compact, "length(cast(c.journal_idasblob))") &&
				!strings.Contains(compact, "length(cast(c.idasblob))")
		}
		return strings.Contains(query, "/* BAS purchase owner body coverage_journal_ids */") ||
			strings.HasPrefix(compact, "selectc.journal_idfromopening_conversionsasc")
	}
	return true
}

func purchaseReferenceJournalAccountDiscoveryQuery(query string, preflight bool) bool {
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "total(payload_bytes)") == preflight &&
		strings.Contains(compact, "selectdistinctl.account_idasaccount_idfromjournal_linesljoinjournalsjon")
}

func purchaseReferenceOpeningAccountDiscoveryQuery(query string, preflight bool) bool {
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "total(payload_bytes)") == preflight &&
		strings.Contains(compact, "selectdistincti.account_idasaccount_idfromopening_itemsijoinopening_conversionsc")
}

func purchaseReferenceAssertCoverageBaseline(t *testing.T, f *postingFixture) {
	t.Helper()
	ctx := context.Background()
	preflight, err := f.repo(f.executor).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("valid public coverage preflight=%v err=%v", preflight != nil, err)
	}
	capture, err := f.repo(f.executor).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("valid public coverage capture=%v err=%v", capture != nil, err)
	}
}

// This account is deliberately outside all selected journal/opening item
// references and outside the fixed control-code set. Reference discovery must
// stay scoped to its selected parent batch instead of census-scanning accounts.
func purchaseReferenceMakeUnrelatedAccountOwnerBLOB(t *testing.T, f *postingFixture) {
	t.Helper()
	rows, err := f.executor.QueryContext(context.Background(), `SELECT a.id FROM accounts AS a
		WHERE a.organisation_id=? AND a.code NOT IN ('2000','1150','1160','1170')
		AND NOT EXISTS(SELECT 1 FROM journal_lines AS l WHERE l.account_id=a.id)
		AND NOT EXISTS(SELECT 1 FROM opening_items AS i WHERE i.account_id=a.id)
		ORDER BY a.id LIMIT 1`, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if !rows.Next() || rows.Scan(&id) != nil || id == "" || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		t.Fatal("writer fixture lacks an unrelated ordinary account for the scope control")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	mutatePurchaseBASScalar(t, f, "accounts",
		`UPDATE accounts SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, id)
}

func TestPurchaseCoverageScalarJournalIDDiscoveryRechecksLateRequiredReferences(t *testing.T) {
	for _, tc := range []struct {
		name      string
		table     string
		alias     string
		rowID     func(*testing.T, *postingFixture) string
		journalID func(*testing.T, *postingFixture) string
	}{
		{name: "modern-posting", table: "purchase_postings_v1", alias: "j",
			rowID: func(t *testing.T, f *postingFixture) string {
				return purchaseBASScalarText(t, f.executor, `SELECT id FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY id LIMIT 1`, postingOrg)
			},
			journalID: func(t *testing.T, f *postingFixture) string {
				return purchaseBASScalarText(t, f.executor, `SELECT journal_id FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY id LIMIT 1`, postingOrg)
			}},
		{name: "selected-opening-conversion", table: "opening_conversions", alias: "c",
			rowID: func(t *testing.T, f *postingFixture) string {
				payableID := legacyOpeningFixture(t, f)
				return purchaseBASScalarText(t, f.executor, `SELECT conversion_id FROM purchase_opening_payables WHERE id=?`, payableID)
			},
			journalID: func(t *testing.T, f *postingFixture) string {
				return purchaseBASScalarText(t, f.executor, `SELECT journal_id FROM opening_conversions WHERE organisation_id=? AND state='POSTED' ORDER BY id LIMIT 1`, postingOrg)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f *postingFixture
			if tc.table == "purchase_postings_v1" {
				f = newPurchaseModernOwnerFixture(t).fixture
			} else {
				f = newPostingFixture(t, newPurchaseReferenceDiscoveryTx(t), tammyv1.GstBasis_GST_BASIS_CASH)
			}
			rowID := tc.rowID(t, f)
			journalID := tc.journalID(t, f)
			purchaseReferenceAssertCoverageBaseline(t, f)

			matchPreflight := func(query string, _ []any) bool {
				return purchaseReferenceScalarJournalDiscoveryQuery(query, tc.table, tc.alias, true)
			}
			matchTarget := func(query string, _ []any) bool {
				return purchaseReferenceScalarJournalDiscoveryQuery(query, tc.table, tc.alias, false)
			}
			probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: f.executor, t: t,
				requirePreflight: true, matchPreflight: matchPreflight, matchTarget: matchTarget}
			probe.mutate = func() {
				mutatePurchaseBASScalar(t, f, tc.table,
					`UPDATE `+tc.table+` SET journal_id=CAST(? AS BLOB) WHERE id=?`, journalID, rowID)
			}
			capture, err := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
			if !probe.mutated || probe.targetCount != 1 || probe.preflightCount == 0 || capture != nil ||
				!errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 {
				t.Fatalf("late %s journal reference escaped its successful discovery preflight: mutated=%t target=%d preflights=%d capture=%v err=%v later bodies=%d",
					tc.name, probe.mutated, probe.targetCount, probe.preflightCount, capture != nil, err, probe.laterBodyQueries)
			}
		})
	}
}

// Each subtest needs its own workspace. Returning the transaction through the
// fixture's executor keeps the test helper independent from database types.
func newPurchaseReferenceDiscoveryTx(t *testing.T) *sqlcipher.Transaction {
	t.Helper()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func TestPurchaseCoverageJournalAccountDiscoveryRejectsLateLargeBLOBReference(t *testing.T) {
	tx, f, journal := purchaseReferenceManualFixture(t, false)
	purchaseReferenceMakeUnrelatedAccountOwnerBLOB(t, f)
	purchaseReferenceAssertCoverageBaseline(t, f)
	lineID := journal.Lines[0].Id
	matchPreflight := func(query string, _ []any) bool {
		return purchaseReferenceJournalAccountDiscoveryQuery(query, true)
	}
	probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t, requirePreflight: true,
		matchPreflight: matchPreflight, measureAllocation: true,
		matchTarget: func(query string, _ []any) bool {
			return purchaseReferenceJournalAccountDiscoveryQuery(query, false)
		}}
	probe.mutate = func() {
		mutatePurchaseBASScalar(t, f, "journal_lines",
			`UPDATE journal_lines SET account_id=zeroblob(?) WHERE id=?`, purchaseReferenceLargeCell, lineID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
	if !probe.mutated || probe.targetCount != 1 || probe.preflightCount == 0 || capture != nil ||
		!errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 || !tx.Authenticated() {
		t.Fatalf("late selected journal account BLOB escaped discovery guard: mutated=%t target=%d preflights=%d capture=%v err=%v later bodies=%d",
			probe.mutated, probe.targetCount, probe.preflightCount, capture != nil, err, probe.laterBodyQueries)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc; allocated >= 2*1024*1024 {
		t.Fatalf("journal account discovery cloned its 9 MiB BLOB before rejecting it: %d allocated bytes", allocated)
	}
}

func TestPurchaseCoverageOpeningAccountDiscoveryRejectsLateLargeBLOBReference(t *testing.T) {
	tx := newPurchaseReferenceDiscoveryTx(t)
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	payableID := legacyOpeningFixture(t, f)
	purchaseReferenceMakeUnrelatedAccountOwnerBLOB(t, f)
	purchaseReferenceAssertCoverageBaseline(t, f)
	matchPreflight := func(query string, _ []any) bool {
		return purchaseReferenceOpeningAccountDiscoveryQuery(query, true)
	}
	probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t, requirePreflight: true,
		matchPreflight: matchPreflight, measureAllocation: true,
		matchTarget: func(query string, _ []any) bool {
			return purchaseReferenceOpeningAccountDiscoveryQuery(query, false)
		}}
	probe.mutate = func() {
		mutatePurchaseBASScalar(t, f, "opening_items",
			`UPDATE opening_items SET account_id=zeroblob(?) WHERE id=?`, purchaseReferenceLargeCell, payableID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
	if !probe.mutated || probe.targetCount != 1 || probe.preflightCount == 0 || capture != nil ||
		!errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 || !tx.Authenticated() {
		t.Fatalf("late selected opening account BLOB escaped discovery guard: mutated=%t target=%d preflights=%d capture=%v err=%v later bodies=%d",
			probe.mutated, probe.targetCount, probe.preflightCount, capture != nil, err, probe.laterBodyQueries)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc; allocated >= 2*1024*1024 {
		t.Fatalf("opening account discovery cloned its 9 MiB BLOB before rejecting it: %d allocated bytes", allocated)
	}
}

func TestPurchaseManualReferenceGuardRejectsBLOBJournalIDsInBothParentArms(t *testing.T) {
	for _, factOnly := range []bool{false, true} {
		name := "direct-parent"
		if factOnly {
			name = "fact-parent"
		}
		t.Run(name, func(t *testing.T) {
			tx, f, journal := purchaseReferenceManualFixture(t, factOnly)
			purchaseReferenceBaselineHasManualEdge(t, f, tx, journal)
			probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t,
				requirePreflight: true,
				matchTarget:      func(query string, _ []any) bool { return purchaseReferencePriorEdgesPreflight(query) },
				matchPreflight:   func(query string, _ []any) bool { return purchaseReferencePriorEdgesPreflight(query) }}
			probe.mutate = func() {
				mutatePurchaseBASScalar(t, f, "journals",
					`UPDATE journals SET id=CAST(? AS BLOB) WHERE id=?`, journal.Id, journal.Id)
			}
			preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(
				context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
			if !probe.mutated || probe.targetCount != 1 || probe.preflightCount == 0 || preflight != nil ||
				!errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 {
				t.Fatalf("BLOB journal ID escaped %s reference guard: mutated=%t target=%d preflights=%d preflight=%v err=%v later bodies=%d",
					name, probe.mutated, probe.targetCount, probe.preflightCount, preflight != nil, err, probe.laterBodyQueries)
			}

		})
	}
}
