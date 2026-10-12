//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const purchaseCoverageDraftPayloadLimit = int64(8 * 1024 * 1024)

func TestPurchaseCoveragePublicPreflightReportsCompleteReservation(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	legacyDocumentID := f.document.Id

	// Exercise a legacy document root, a modern posting root, and an opening
	// payable root. The manual receipt adds a real linked tax fact to the
	// purchase-owned base charge; the modern payment is a distinct receipt
	// source with its own complete journal parent.
	if legacy := seedCoverageCashReceiptWithTaxFactSource(t, f, legacyDocumentID, legacyDocumentID); legacy == nil {
		t.Fatal("legacy receipt fixture returned nil")
	}
	posted := postReportingPurchase(t, f, "PREFLIGHT-MODERN", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	payment, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, posted.Accounting.AccountingVersion, 11000, postDate(20)))
	if err != nil || payment == nil || payment.Journal == nil {
		t.Fatalf("seed modern receipt source: payment=%v err=%v", payment, err)
	}
	legacyOpeningFixture(t, f)

	repo := f.repo(tx)
	stateBefore := coverageStateCounts(t, tx)
	changesBefore := postScalar(t, tx, "SELECT total_changes()")
	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("public preflight failed: result=%+v err=%v", preflight, err)
	}
	if probe.bodyReads != 0 {
		t.Fatalf("preflight hydrated %d document, proto, body, or journal-line queries", probe.bodyReads)
	}
	if preflight.RootCount != 3 {
		t.Fatalf("root count=%d, want one document, one modern posting, and one opening payable root", preflight.RootCount)
	}
	if preflight.ReceiptCount != 2 {
		t.Fatalf("receipt count=%d, want one modern payment and one distinct legacy cash receipt", preflight.ReceiptCount)
	}
	if preflight.JournalCount != len(preflight.JournalIDs) || preflight.JournalCount != len(preflight.JournalPayloadBytes) {
		t.Fatalf("journal count/IDs/charge map disagree: count=%d IDs=%v charges=%v", preflight.JournalCount, preflight.JournalIDs, preflight.JournalPayloadBytes)
	}
	if !sort.StringsAreSorted(preflight.JournalIDs) {
		t.Fatalf("journal IDs are not sorted: %v", preflight.JournalIDs)
	}
	for i := 1; i < len(preflight.JournalIDs); i++ {
		if preflight.JournalIDs[i-1] == preflight.JournalIDs[i] {
			t.Fatalf("journal IDs are not unique: %v", preflight.JournalIDs)
		}
	}
	var parentBytes int64
	for _, id := range preflight.JournalIDs {
		charge, ok := preflight.JournalPayloadBytes[id]
		if !ok || charge <= 0 {
			t.Fatalf("journal %s has no positive unique-parent charge: %v", id, preflight.JournalPayloadBytes)
		}
		parentBytes += charge
	}
	if preflight.PayloadBytes != preflight.BasePayloadBytes+parentBytes {
		t.Fatalf("total/base/parent charges=%d/%d/%d, want total = base + unique parents", preflight.PayloadBytes, preflight.BasePayloadBytes, parentBytes)
	}
	if preflight.TaxFactPayloadBytes <= 0 || preflight.TaxFactPayloadBytes > preflight.BasePayloadBytes {
		t.Fatalf("tax-fact category %d is not included within base charge %d", preflight.TaxFactPayloadBytes, preflight.BasePayloadBytes)
	}
	clampedPreflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	clampedPreflight, err := f.repo(clampedPreflightProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit+1)
	if err != nil || clampedPreflight == nil || clampedPreflight.PayloadBytes != preflight.PayloadBytes ||
		!reflect.DeepEqual(clampedPreflight.JournalIDs, preflight.JournalIDs) ||
		!reflect.DeepEqual(clampedPreflight.JournalPayloadBytes, preflight.JournalPayloadBytes) || clampedPreflightProbe.bodyReads != 0 {
		t.Fatalf("over-cap preflight did not clamp to the default reservation: result=%+v err=%v bodyReads=%d", clampedPreflight, err, clampedPreflightProbe.bodyReads)
	}
	if !reflect.DeepEqual(stateBefore, coverageStateCounts(t, tx)) ||
		postScalar(t, tx, "SELECT total_changes()") != changesBefore {
		t.Fatal("public preflight changed retained state or revisions")
	}

	// The exact total succeeds without hydration; one byte less fails with no
	// partial result and before any retained body query.
	exactProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	exact, err := f.repo(exactProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, preflight.PayloadBytes)
	if err != nil || exact == nil || exact.PayloadBytes != preflight.PayloadBytes || exactProbe.bodyReads != 0 {
		t.Fatalf("exact public budget: result=%+v err=%v bodyReads=%d", exact, err, exactProbe.bodyReads)
	}
	overProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	over, err := f.repo(overProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, preflight.PayloadBytes+1)
	if err != nil || over == nil || over.PayloadBytes != preflight.PayloadBytes || overProbe.bodyReads != 0 {
		t.Fatalf("one-byte-over public budget: result=%+v err=%v bodyReads=%d", over, err, overProbe.bodyReads)
	}
	shortProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	short, err := f.repo(shortProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, preflight.PayloadBytes-1)
	assertPurchaseCoverageBound(t, err, "capture bytes", preflight.PayloadBytes-1)
	if short != nil || shortProbe.bodyReads != 0 {
		t.Fatalf("one-byte-short preflight returned partial=%v or hydrated %d bodies", short != nil, shortProbe.bodyReads)
	}
	captureProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	partial, err := f.repo(captureProbe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, preflight.PayloadBytes-1)
	assertPurchaseCoverageBound(t, err, "capture bytes", preflight.PayloadBytes-1)
	if partial != nil || captureProbe.bodyReads != 0 {
		t.Fatalf("one-byte-short capture returned partial=%v or hydrated %d bodies", partial != nil, captureProbe.bodyReads)
	}

	// The new summary and capture metadata describe the same reservation, and
	// the existing default remains equivalent to an over-cap clamped request.
	capture := captureCoverage(t, repo, ctx)
	if len(capture.Journals) != 4 || preflight.JournalCount != len(capture.Journals) {
		t.Fatalf("complete parents=%d preflight=%d; want legacy receipt, posting, payment and opening parents", len(capture.Journals), preflight.JournalCount)
	}
	for _, journal := range capture.Journals {
		if journal == nil || journal.Journal == nil || preflight.JournalPayloadBytes[journal.Journal.Id] != journal.PayloadBytes {
			t.Fatalf("individual loaded journal charge does not match preflight: journal=%+v charges=%v", journal, preflight.JournalPayloadBytes)
		}
	}
	if capture.RootCount != preflight.RootCount ||
		capture.ReceiptCount != preflight.ReceiptCount ||
		capture.JournalCount != preflight.JournalCount ||
		!reflect.DeepEqual(capture.JournalIDs, preflight.JournalIDs) ||
		!reflect.DeepEqual(capture.JournalPayloadBytes, preflight.JournalPayloadBytes) ||
		capture.BasePayloadBytes != preflight.BasePayloadBytes ||
		capture.PayloadBytes != preflight.PayloadBytes ||
		capture.TaxFactPayloadBytes != preflight.TaxFactPayloadBytes {
		t.Fatalf("capture reservation metadata differs from preflight: capture=%+v preflight=%+v", capture, preflight)
	}
	if got := draftPurchaseCoverageTaxFactBytes(capture.Rows); preflight.TaxFactPayloadBytes != got {
		t.Fatalf("tax-fact preflight charge=%d, want exact retained-row charge %d", preflight.TaxFactPayloadBytes, got)
	}
	defaultCapture, err := repo.CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || defaultCapture == nil {
		t.Fatalf("default capture failed: capture=%v err=%v", defaultCapture, err)
	}
	clampedCapture, err := repo.CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit+1)
	if err != nil || clampedCapture == nil ||
		!bytes.Equal(defaultCapture.Fingerprint, clampedCapture.Fingerprint) ||
		defaultCapture.PayloadBytes != clampedCapture.PayloadBytes ||
		!reflect.DeepEqual(defaultCapture.Rows, clampedCapture.Rows) {
		t.Fatalf("over-cap budget changed default fingerprint/payload/rows: default=%v clamped=%v err=%v", defaultCapture, clampedCapture, err)
	}
}

func TestPurchaseCoveragePublicPreflightRejectsInvalidInputsAndBrokenParentsBeforeHydration(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, f *postingFixture, postingID, journalID string)
	}{
		{
			name: "missing parent",
			mutate: func(t *testing.T, f *postingFixture, postingID, _ string) {
				temporarilyDropPurchaseCoverageTriggers(t, f, []string{"purchase_postings_v1_no_update"}, func() {
					if _, err := f.executor.ExecContext(context.Background(), "PRAGMA defer_foreign_keys = ON"); err != nil {
						t.Fatalf("defer fixture FK check: %v", err)
					}
					missingParent := "018f0000-0000-7000-8000-000000009997"
					if _, err := f.executor.ExecContext(context.Background(),
						"UPDATE purchase_postings_v1 SET journal_id=? WHERE id=?", missingParent, postingID); err != nil {
						t.Fatalf("remove required posted-purchase journal parent: %v", err)
					}
				})
			},
		},
		{
			name: "wrong owner",
			mutate: func(t *testing.T, f *postingFixture, _, journalID string) {
				foreignOwner := "018f0000-0000-7000-8000-000000009998"
				// The workspace permits one organisation. This hostile storage
				// fixture changes the parent's owner to an unrelated canonical ID
				// while deferring FK validation; the transaction is rolled back.
				temporarilyDropPurchaseCoverageTriggers(t, f, []string{"journals_posted_immutable", "posted_journal_update_guard", "journal_organisation_immutable"}, func() {
					if _, err := f.executor.ExecContext(context.Background(), "PRAGMA defer_foreign_keys = ON"); err != nil {
						t.Fatalf("defer fixture FK check: %v", err)
					}
					if _, err := f.executor.ExecContext(context.Background(),
						"UPDATE journals SET organisation_id=? WHERE id=?", foreignOwner, journalID); err != nil {
						t.Fatalf("move linked parent outside the source organisation: %v", err)
					}
				})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			posted := postReportingPurchase(t, f, "PREFLIGHT-BROKEN-PARENT", purchaseReportingDate(2026, 8, 10), 10000, 1000)
			if posted == nil || posted.Posting == nil || posted.Posting.JournalId == nil {
				t.Fatal("purchase fixture did not return its complete journal parent edge")
			}
			test.mutate(t, f, posted.Posting.Id, *posted.Posting.JournalId)
			probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
			result, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if result != nil || err == nil || probe.bodyReads != 0 {
				t.Fatalf("broken parent preflight returned result=%+v err=%v bodyReads=%d", result, err, probe.bodyReads)
			}
		})
	}

	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	negative, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, -1)
	if negative != nil || !errors.Is(err, purchases.ErrRepository) || probe.bodyReads != 0 {
		t.Fatalf("negative preflight budget returned result=%+v err=%v bodyReads=%d", negative, err, probe.bodyReads)
	}
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	canceled, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(canceledCtx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if canceled != nil || !errors.Is(err, context.Canceled) || probe.bodyReads != 0 {
		t.Fatalf("canceled preflight returned result=%+v err=%v bodyReads=%d", canceled, err, probe.bodyReads)
	}
}

func TestPurchaseCoveragePublicPreflightPreservesCancellationDuringFirstQuery(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	before := coverageStateCounts(t, tx)
	changes := postScalar(t, tx, "SELECT total_changes()")
	cancelCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	executor := &purchasePreflightCancelAfterQuery{CommandSQLExecutor: probe, cancel: cancel}
	metadata, err := f.repo(executor).PreflightCoverageForBASWithPayloadBudget(cancelCtx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if metadata != nil || !errors.Is(err, context.Canceled) || executor.queries != 1 || probe.bodyReads != 0 {
		t.Fatalf("canceled preflight: metadata=%+v err=%v queries=%d bodyReads=%d", metadata, err, executor.queries, probe.bodyReads)
	}
	if !reflect.DeepEqual(before, coverageStateCounts(t, tx)) || changes != postScalar(t, tx, "SELECT total_changes()") || !tx.Authenticated() {
		t.Fatal("canceled preflight changed state or ended the caller's transaction")
	}
}

type purchasePreflightCancelAfterQuery struct {
	app.CommandSQLExecutor
	cancel  context.CancelFunc
	queries int
}

func (executor *purchasePreflightCancelAfterQuery) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := executor.CommandSQLExecutor.QueryContext(ctx, query, args...)
	executor.queries++
	if executor.queries == 1 {
		executor.cancel()
	}
	return rows, err
}

func TestPurchaseCoveragePublicPreflightAccepts1000RootsAndRejects1001BeforeHydration(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	// This mirrors the existing 1,000 mixed-root fixture: the initial document,
	// 997 additional documents, one modern posting, and one opening payable.
	for index := 0; index < 997; index++ {
		f.addDocumentReview("Bound supplier", fmt.Sprintf("PREFLIGHT-ROOT-%03d", index), 0, 0, index%17 == 0)
	}
	postReportingPurchase(t, f, "PREFLIGHT-ROOT-MODERN", postDate(10), 0, 0)
	legacyOpeningFixture(t, f)

	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil || preflight.RootCount != 1000 || probe.bodyReads != 0 {
		t.Fatalf("exact root capacity preflight: result=%+v err=%v bodyReads=%d", preflight, err, probe.bodyReads)
	}

	f.addDocumentReview("Bound supplier", "PREFLIGHT-ROOT-OVERFLOW", 0, 0, false)
	overflowProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	overflow, err := f.repo(overflowProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	assertPurchaseCoverageBound(t, err, "purchase roots", 1000)
	if overflow != nil || overflowProbe.bodyReads != 0 {
		t.Fatalf("1,001 roots returned partial=%v or hydrated %d bodies", overflow != nil, overflowProbe.bodyReads)
	}
}

func TestPurchaseCoveragePublicPreflightAllows1000ReceiptSourcesToReachByteBoundAndRejects1001(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	posting := postReportingPurchase(t, f, "PREFLIGHT-RECEIPT-BOUND", postDate(10), 10000, 1000)
	first, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, posting.Accounting.AccountingVersion, 11000, postDate(20)))
	if err != nil || first == nil {
		t.Fatalf("seed first modern receipt source: response=%v err=%v", first, err)
	}
	// Reuse the existing bounded clone fixture to reach 999 modern payment
	// rows, then add one distinct legacy cash receipt for 1,000 total sources.
	for index := 1; index < 999; index++ {
		appendCoveragePaymentCountFixture(t, f, tx, first, index)
	}
	f.addDocumentReview("Legacy receipt supplier", "PREFLIGHT-LEGACY-RECEIPT", 0, 0, false)
	seedLegacyCashPayment(t, f, f.document.Id)

	// The deliberately cloned near-cap rows are suitable for exercising the
	// scalar count fence. At 1,000 sources the count passes and the zero-byte
	// budget is the first failure; this does not assert a complete capture of
	// those cloned source bodies.
	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	result, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, 0)
	assertPurchaseCoverageBound(t, err, "capture bytes", 0)
	if result != nil || probe.bodyReads != 0 {
		t.Fatalf("1,000 receipt sources returned partial=%v or hydrated %d bodies before the byte bound", result != nil, probe.bodyReads)
	}

	appendCoveragePaymentCountFixture(t, f, tx, first, 999)
	overflowProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	overflow, err := f.repo(overflowProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, 0)
	assertPurchaseCoverageBound(t, err, "receipt sources", 1000)
	if overflow != nil || overflowProbe.bodyReads != 0 {
		t.Fatalf("1,001 receipt sources returned partial=%v or hydrated %d bodies before the count bound", overflow != nil, overflowProbe.bodyReads)
	}
}

func draftPurchaseCoverageTaxFactBytes(rows []purchases.PurchaseReportingRetainedRow) int64 {
	// This mirrors postingReportingPayloadSizeSQL and postingReportingValue:
	// 512 bytes per row plus each cell's SQLite BLOB-cast size. INTEGER cells
	// use their decimal text length in the current purchase retention recipe.
	const rowOverhead = int64(512)
	var total int64
	for _, row := range rows {
		if row.Table != "tax_facts" {
			continue
		}
		total += rowOverhead
		for _, value := range row.Values {
			switch value.Kind {
			case "NULL":
			case "INTEGER":
				total += int64(len(strconv.FormatInt(value.Integer, 10)))
			case "TEXT":
				total += int64(len(value.Text))
			case "BLOB":
				total += int64(len(value.Bytes))
			}
		}
	}
	return total
}

func temporarilyDropPurchaseCoverageTriggers(t *testing.T, f *postingFixture, names []string, mutate func()) {
	t.Helper()
	definitions := make(map[string]string, len(names))
	for _, name := range names {
		rows, err := f.executor.QueryContext(context.Background(),
			"SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?", name)
		if err != nil {
			t.Fatalf("read exact trigger %s definition: %v", name, err)
		}
		if !rows.Next() {
			_ = rows.Close()
			t.Fatalf("required trigger %s is missing", name)
		}
		var definition string
		scanErr := rows.Scan(&definition)
		extraRow := rows.Next()
		rowsErr := rows.Err()
		if scanErr != nil || definition == "" || extraRow || rowsErr != nil {
			_ = rows.Close()
			t.Fatalf("read unique exact trigger %s definition: scan=%v extra=%v rows=%v", name, scanErr, extraRow, rowsErr)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close trigger definition query for %s: %v", name, err)
		}
		definitions[name] = definition
	}
	dropped := make([]string, 0, len(names))
	defer func() {
		for index := len(dropped) - 1; index >= 0; index-- {
			name := dropped[index]
			if _, err := f.executor.ExecContext(context.Background(), definitions[name]); err != nil {
				t.Errorf("restore exact trigger %s definition: %v", name, err)
			}
		}
	}()
	for _, name := range names {
		if _, err := f.executor.ExecContext(context.Background(), "DROP TRIGGER "+name); err != nil {
			t.Fatalf("drop trigger %s for hostile fixture mutation: %v", name, err)
		}
		dropped = append(dropped, name)
	}
	mutate()
}
