//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Native SQLCipher regressions for the finite reference/manual reader group.
package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const (
	purchaseReferenceMissingID = "018f0000-0000-7000-8000-000000009997"
	purchaseReferenceLargeCell = 9 * 1024 * 1024
)

// Query matchers accept a future stable BAS reference marker and also match
// today's SQL shape, so a production implementation can add the guard without
// making the stage hook depend on CTE formatting.
type purchaseReferenceQueryProbe struct {
	app.CommandSQLExecutor
	t                         *testing.T
	matchTarget               func(string, []any) bool
	matchPreflight            func(string, []any) bool
	mutate                    func()
	requirePreflight          bool
	requireDocumentAndPosting bool
	replayDocumentID          string
	mutated                   bool
	preflightCount            int
	targetCount               int
	documentReplayRead        bool
	postingReplayRead         bool
	laterBodyQueries          int
	measureAllocation         bool
	beforeRead                runtime.MemStats
}

func (p *purchaseReferenceQueryProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if p.matchPreflight != nil && p.matchPreflight(query, args) {
		p.preflightCount++
	}
	targetHere := !p.mutated && p.matchTarget != nil && p.matchTarget(query, args)
	if targetHere {
		if p.requirePreflight && p.preflightCount == 0 {
			p.t.Fatalf("reference mutation reached %q before its preflight boundary", query)
		}
		if p.requireDocumentAndPosting && (!p.documentReplayRead || !p.postingReplayRead) {
			p.t.Fatalf("reference mutation reached replay before document/posting replay reads: %t/%t",
				p.documentReplayRead, p.postingReplayRead)
		}
		p.mutate()
		p.mutated = true
		p.targetCount++
		if p.measureAllocation {
			runtime.GC()
			runtime.ReadMemStats(&p.beforeRead)
		}
	}

	rows, err := p.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		return rows, err
	}
	if p.requireDocumentAndPosting {
		if purchaseReportingRootOwnerIsDocumentReplayQuery(query) &&
			purchaseModernOwnerAtomicTailArgsEqual(args, p.replayDocumentID, postingOrg) {
			p.documentReplayRead = true
		}
		if purchaseModernOwnerAtomicIsPostingReplayQuery(query) &&
			purchaseModernOwnerAtomicTailArgsEqual(args, postingOrg, p.replayDocumentID) {
			p.postingReplayRead = true
		}
	}
	columns, columnErr := rows.Columns()
	if columnErr != nil {
		p.t.Fatalf("read reference-stage result columns: %v", columnErr)
	}
	if p.mutated && !targetHere {
		for _, column := range columns {
			if purchaseCoverageBodyResultColumn(column) {
				p.laterBodyQueries++
				break
			}
		}
	}
	return rows, nil
}

func purchaseReferenceCompactSQL(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), ""))
}

func purchaseReferenceMarker(query, marker string) bool {
	return strings.Contains(query, "/* BAS purchase reference "+marker+" */")
}

func purchaseReferenceModernRawMarker(query string, family purchaseModernOwnerAtomicFamily) bool {
	return purchaseReferenceMarker(query, "body "+family.name) ||
		purchaseModernOwnerAtomicFamilyRawQuery(query, family)
}

func purchaseReferenceModernPreflightMarker(query string, family purchaseModernOwnerAtomicFamily) bool {
	return purchaseReferenceMarker(query, "preflight "+family.name) ||
		purchaseModernOwnerAtomicFamilyPreflightQuery(query, family)
}

func purchaseReferenceModernReplayMarker(query string, family purchaseModernOwnerAtomicFamily, stage string) bool {
	return purchaseReferenceMarker(query, "replay "+family.name) ||
		purchaseModernOwnerAtomicReplayQuery(query, family, stage, purchaseReferenceCompactSQL(query))
}

func purchaseReferencePriorEdgesQuery(query string) bool {
	if purchaseReferenceMarker(query, "prior-edges") {
		return true
	}
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "selectsource_id,journal_idfrom(") &&
		strings.Contains(compact, "fromjournalsj") && strings.Contains(compact, "fromtax_factsf")
}

func purchaseReferencePriorSourceQuery(query string) bool {
	if purchaseReferenceMarker(query, "prior-sources") {
		return true
	}
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "selectdistinctj.idfromjournalsjleftjoinjournal_linesl") &&
		strings.Contains(compact, "leftjointax_factsf") && strings.Contains(compact, "j.source_kind=0")
}

func purchaseReferenceReceiptCountQuery(query string) bool {
	if purchaseReferenceMarker(query, "receipt-count") {
		return true
	}
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "count(distinctj.id)") &&
		strings.Contains(compact, "fromjournalsj") && strings.Contains(compact, "report_classification")
}

func purchaseReferenceProofQuery(query, table string) bool {
	if purchaseReferenceMarker(query, "proof "+table) || strings.Contains(query, "/* BAS purchase owner body "+table+" */") {
		return true
	}
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "from"+table+"asf") && strings.Contains(compact, "joinjournal_linesasl") && strings.Contains(compact, "joinjournalsasj")
}

func purchaseReferenceProofPreflight(query, table string) bool {
	return purchaseReferenceProofQuery(query, table) && strings.Contains(purchaseReferenceCompactSQL(query), "total(payload_bytes)")
}

func purchaseReferenceHasStringArg(args []any, want string) bool {
	for _, arg := range args {
		if value, ok := arg.(string); ok && value == want {
			return true
		}
	}
	return false
}

func purchaseReferenceAccountQuery(query string, args []any, accountID string) bool {
	if purchaseReferenceMarker(query, "proof accounts") || strings.Contains(query, "/* BAS purchase owner body accounts */") {
		return purchaseReferenceHasStringArg(args, accountID)
	}
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "fromaccountsasa") && strings.Contains(compact, "a.idin(") &&
		purchaseReferenceHasStringArg(args, accountID)
}

func purchaseReferenceSetOwner(t *testing.T, f *postingFixture, table, keyColumn, key string, storage string) {
	t.Helper()
	ownerSQL := "organisation_id=?"
	ownerValue := any("foreign-owner")
	if storage == "blob" {
		ownerSQL = "organisation_id=CAST(? AS BLOB)"
		ownerValue = postingOrg
	} else if storage != "foreign" {
		t.Fatalf("unknown reference owner storage %q", storage)
	}
	mutatePurchaseBASScalar(t, f, table,
		"UPDATE "+table+" SET "+ownerSQL+" WHERE "+keyColumn+"=?", ownerValue, key)
}

func purchaseReferenceManualFixture(t *testing.T, factOnly bool) (*sqlcipher.Transaction, *postingFixture, *tammyv1.Journal) {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	if factOnly {
		unrelatedJournalSourceID, _ := f.id()
		return tx, f, seedCoverageCashReceiptWithTaxFactSource(t, f, f.document.Id, unrelatedJournalSourceID)
	}
	return tx, f, seedLegacyCashPayment(t, f, f.document.Id)
}

func purchaseReferenceBaselineHasManualEdge(t *testing.T, f *postingFixture, tx *sqlcipher.Transaction, journal *tammyv1.Journal) {
	t.Helper()
	capture, err := f.repo(tx).CaptureCoverageForBAS(context.Background(), postingOrg)
	if err != nil || capture == nil || capture.ReceiptCount != 1 {
		t.Fatalf("writer-backed manual baseline capture=%v receipts=%d err=%v", capture != nil, captureCount(capture), err)
	}
	legacy := coverageLegacyDocument(t, capture, f.document.Id)
	if !coverageHasID(legacy.PriorJournalIDs, journal.Id) || !coverageHasID(legacy.PaymentJournalIDs, journal.Id) {
		t.Fatalf("writer-backed manual source did not expose its journal edge: prior=%v payment=%v", legacy.PriorJournalIDs, legacy.PaymentJournalIDs)
	}
}

func captureCount(capture *purchases.PurchaseCoverageReportingCapture) int {
	if capture == nil {
		return 0
	}
	return capture.ReceiptCount
}

func TestPurchaseManualSourceSelectionRejectsForeignAndExactBLOBParents(t *testing.T) {
	for _, edge := range []struct {
		name     string
		factOnly bool
		table    string
		keyCol   string
	}{
		{name: "direct-journal", table: "journals", keyCol: "id"},
		{name: "fact-only-tax-fact", factOnly: true, table: "tax_facts", keyCol: "journal_line_id"},
	} {
		for _, storage := range []string{"foreign", "blob"} {
			for _, stage := range []string{"preflight", "capture"} {
				t.Run(edge.name+"/"+storage+"/"+stage, func(t *testing.T) {
					tx, f, journal := purchaseReferenceManualFixture(t, edge.factOnly)
					purchaseReferenceBaselineHasManualEdge(t, f, tx, journal)
					key := journal.Id
					if edge.factOnly {
						key = journal.Lines[0].Id
					}
					probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t,
						requirePreflight: true,
						matchTarget: func(query string, _ []any) bool {
							return purchaseReferencePriorEdgesPreflight(query)
						},
						matchPreflight: func(query string, _ []any) bool {
							return purchaseReferencePriorEdgesPreflight(query)
						},
						mutate: func() { purchaseReferenceSetOwner(t, f, edge.table, edge.keyCol, key, storage) }}
					hasResult := false
					var err error
					if stage == "preflight" {
						preflight, preflightErr := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(
							context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
						hasResult = preflight != nil
						err = preflightErr
					} else {
						capture, captureErr := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
						hasResult = capture != nil
						err = captureErr
					}
					if !probe.mutated || probe.preflightCount == 0 || hasResult || !errors.Is(err, purchases.ErrRepository) ||
						probe.laterBodyQueries != 0 || !tx.Authenticated() {
						t.Fatalf("corrupt %s owner escaped %s source selection: mutated=%t preflights=%d capture=%#v err=%v later bodies=%d",
							edge.name, stage, probe.mutated, probe.preflightCount, hasResult, err, probe.laterBodyQueries)
					}
				})
			}
		}
	}
}

func purchaseReferencePriorEdgesPreflight(query string) bool {
	if purchaseReferenceMarker(query, "preflight prior-edges") {
		return true
	}
	return purchaseReferencePriorEdgesQuery(query) && strings.Contains(purchaseReferenceCompactSQL(query), "total(payload_bytes)")
}

func TestPurchaseManualReceiptCountRejectsOwnersBeforeZeroReturn(t *testing.T) {
	for _, owner := range []struct {
		name  string
		table string
		key   func(*testing.T, *sqlcipher.Transaction, *tammyv1.Journal) string
	}{
		{name: "journal", table: "journals", key: func(_ *testing.T, _ *sqlcipher.Transaction, j *tammyv1.Journal) string { return j.Id }},
		{name: "tax-fact", table: "tax_facts", key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
			return purchaseBASScalarText(t, tx, `SELECT id FROM tax_facts WHERE journal_line_id=?`, j.Lines[0].Id)
		}},
		{name: "cash-account", table: "accounts", key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
			return purchaseBASScalarText(t, tx, `SELECT l.account_id FROM journal_lines l WHERE l.journal_id=? AND l.credit_minor>l.debit_minor`, j.Id)
		}},
	} {
		for _, storage := range []string{"foreign", "blob"} {
			t.Run(owner.name+"/"+storage, func(t *testing.T) {
				tx, f, journal := purchaseReferenceManualFixture(t, true)
				purchaseReferenceBaselineHasManualEdge(t, f, tx, journal)
				key := owner.key(t, tx, journal)
				probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t,
					matchTarget: func(query string, _ []any) bool { return purchaseReferenceReceiptCountQuery(query) },
					mutate:      func() { purchaseReferenceSetOwner(t, f, owner.table, "id", key, storage) }}
				preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(
					context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
				if !probe.mutated || probe.targetCount != 1 || preflight != nil || !errors.Is(err, purchases.ErrRepository) ||
					probe.laterBodyQueries != 0 || !tx.Authenticated() {
					t.Fatalf("corrupt %s owner escaped the candidate-count zero path: mutated=%t count=%d preflight=%#v err=%v later bodies=%d",
						owner.name, probe.mutated, probe.targetCount, preflight, err, probe.laterBodyQueries)
				}
			})
		}
	}
}

type purchaseReferenceModernCase struct {
	name         string
	familyIndex  int
	referenceCol string
	parentTable  string
	parentKeyCol string
	replayStage  string
}

var purchaseReferenceModernCases = []purchaseReferenceModernCase{
	{name: "payment-allocation-payment", familyIndex: 0, referenceCol: "payment_id", parentTable: "purchase_payments_v1", parentKeyCol: "id", replayStage: "payment-allocation"},
	{name: "evidence-document", familyIndex: 1, referenceCol: "document_id", parentTable: "documents", parentKeyCol: "id", replayStage: "evidence"},
	{name: "event-posting-scope", familyIndex: 2, referenceCol: "scope_id", parentTable: "purchase_postings_v1", parentKeyCol: "id", replayStage: "event-v1"},
	{name: "attribution-evidence", familyIndex: 3, referenceCol: "evidence_id", parentTable: "purchase_tax_evidence_v1", parentKeyCol: "id", replayStage: "attribution"},
	{name: "deferral-document", familyIndex: 4, referenceCol: "document_id", parentTable: "documents", parentKeyCol: "id", replayStage: "deferral"},
	{name: "portion-evidence", familyIndex: 5, referenceCol: "evidence_id", parentTable: "purchase_tax_evidence_v1", parentKeyCol: "id", replayStage: "deferral-portion"},
}

func purchaseReferenceModernKey(t *testing.T, tx *sqlcipher.Transaction, family purchaseModernOwnerAtomicFamily, postingID string) []any {
	t.Helper()
	if family.name == "evidence" {
		return purchaseModernOwnerAtomicReadKey(t, tx, family,
			`SELECT id FROM purchase_tax_evidence_v1 WHERE posting_id=? AND source_document_id IS NOT NULL ORDER BY accounting_version DESC,id LIMIT 1`, postingID)
	}
	return purchaseModernOwnerAtomicKey(t, tx, family, postingID)
}

func purchaseReferenceCurrentText(t *testing.T, tx *sqlcipher.Transaction, family purchaseModernOwnerAtomicFamily, key []any, column string) string {
	t.Helper()
	return purchaseBASScalarText(t, tx, "SELECT "+column+" FROM "+family.table+" WHERE "+family.keyWhere, key...)
}

func purchaseReferenceSetChild(t *testing.T, f *postingFixture, family purchaseModernOwnerAtomicFamily, key []any, column, current, mode string) {
	t.Helper()
	setRef := column + "=?"
	value := any(purchaseReferenceMissingID)
	if mode == "blob-reference" {
		setRef = column + "=CAST(? AS BLOB)"
		value = []byte(current)
	} else if mode != "missing-reference" {
		t.Fatalf("unknown child-reference mode %q", mode)
	}
	args := []any{value}
	args = append(args, key...)
	mutatePurchaseBASScalar(t, f, family.table,
		"UPDATE "+family.table+" SET "+setRef+" WHERE "+family.keyWhere, args...)
}

func purchaseReferenceSetParentForeign(t *testing.T, f *postingFixture, tc purchaseReferenceModernCase, parentID string) {
	t.Helper()
	mutatePurchaseBASScalar(t, f, tc.parentTable,
		"UPDATE "+tc.parentTable+" SET organisation_id='foreign-owner' WHERE "+tc.parentKeyCol+"=?", parentID)
}

func TestPurchaseModernRawReadersRecheckRequiredReferences(t *testing.T) {
	for _, tc := range purchaseReferenceModernCases {
		for _, mode := range []string{"missing-reference", "blob-reference", "foreign-parent-owner"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				fixture := newPurchaseModernOwnerFixture(t)
				family := purchaseModernOwnerAtomicFamilies[tc.familyIndex]
				key := purchaseReferenceModernKey(t, fixture.tx, family, postingReferenceID(t, fixture.tx))
				current := purchaseReferenceCurrentText(t, fixture.tx, family, key, tc.referenceCol)
				if current == "" {
					t.Fatalf("real writer left required %s reference empty", tc.name)
				}
				if mode == "missing-reference" && postScalar(t, fixture.tx,
					"SELECT count(*) FROM "+tc.parentTable+" WHERE "+tc.parentKeyCol+"=?", purchaseReferenceMissingID) != 0 {
					t.Fatal("missing reference control unexpectedly has a parent row")
				}
				baseline, err := fixture.fixture.repo(fixture.tx).CapturePostingsForReporting(context.Background(), postingOrg)
				if err != nil || baseline == nil {
					t.Fatalf("writer-backed modern baseline capture=%v err=%v", baseline != nil, err)
				}

				probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: fixture.tx, t: t,
					measureAllocation: tc.name == "attribution-evidence" && mode == "foreign-parent-owner",
					requirePreflight:  true,
					matchTarget:       func(query string, _ []any) bool { return purchaseReferenceModernRawMarker(query, family) },
					matchPreflight:    func(query string, _ []any) bool { return purchaseReferenceModernPreflightMarker(query, family) }}
				probe.mutate = func() {
					if mode == "foreign-parent-owner" {
						large := tc.name == "attribution-evidence"
						if large {
							purchaseModernOwnerAtomicSetLargeCell(t, fixture.fixture, family, key, family.protoColumn)
						}
						purchaseReferenceSetParentForeign(t, fixture.fixture, tc, current)
						return
					}
					purchaseReferenceSetChild(t, fixture.fixture, family, key, tc.referenceCol, current, mode)
				}
				capture, captureErr := fixture.fixture.repo(probe).CapturePostingsForReporting(context.Background(), postingOrg)
				if !probe.mutated || probe.preflightCount == 0 || capture != nil || !errors.Is(captureErr, purchases.ErrRepository) ||
					probe.laterBodyQueries != 0 || !fixture.tx.Authenticated() {
					t.Fatalf("late %s %s drift escaped its family statement: mutated=%t preflights=%d capture=%v err=%v later bodies=%d",
						tc.name, mode, probe.mutated, probe.preflightCount, capture != nil, captureErr, probe.laterBodyQueries)
				}
				if tc.name == "attribution-evidence" && mode == "foreign-parent-owner" {
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					if after.TotalAlloc-probe.beforeRead.TotalAlloc >= 2*1024*1024 {
						t.Fatalf("late parent/reference guard copied a large attribution body: allocated=%d", after.TotalAlloc-probe.beforeRead.TotalAlloc)
					}
				}
			})
		}
	}
}

func postingReferenceID(t *testing.T, tx *sqlcipher.Transaction) string {
	t.Helper()
	return purchaseBASScalarText(t, tx,
		`SELECT id FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY id LIMIT 1`, postingOrg)
}

func TestPurchaseModernReportingReplayRechecksRequiredReferences(t *testing.T) {
	for _, tc := range purchaseReferenceModernCases {
		for _, mode := range []string{"blob-reference", "foreign-parent-owner"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				fixture := newPurchaseModernOwnerFixture(t)
				family := purchaseModernOwnerAtomicFamilies[tc.familyIndex]
				postingID := postingReferenceID(t, fixture.tx)
				key := purchaseReferenceModernKey(t, fixture.tx, family, postingID)
				current := purchaseReferenceCurrentText(t, fixture.tx, family, key, tc.referenceCol)
				baseline, err := fixture.fixture.repo(fixture.tx).CapturePostingsForReporting(context.Background(), postingOrg)
				if err != nil || baseline == nil {
					t.Fatalf("writer-backed modern replay baseline=%v err=%v", baseline != nil, err)
				}
				probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: fixture.tx, t: t,
					requireDocumentAndPosting: true, replayDocumentID: fixture.fixture.document.Id,
					matchTarget: func(query string, _ []any) bool {
						return purchaseReferenceModernReplayMarker(query, family, tc.replayStage)
					}}
				probe.mutate = func() {
					if mode == "foreign-parent-owner" {
						purchaseReferenceSetParentForeign(t, fixture.fixture, tc, current)
						return
					}
					purchaseReferenceSetChild(t, fixture.fixture, family, key, tc.referenceCol, current, mode)
				}
				capture, captureErr := fixture.fixture.repo(probe).CapturePostingsForReporting(context.Background(), postingOrg)
				if !probe.mutated || probe.targetCount != 1 || !probe.documentReplayRead || !probe.postingReplayRead ||
					capture != nil || !errors.Is(captureErr, purchases.ErrRepository) || probe.laterBodyQueries != 0 || !fixture.tx.Authenticated() {
					t.Fatalf("late %s %s replay drift escaped its same-statement reader: mutated=%t target=%d doc/post=%t/%t capture=%v err=%v later bodies=%d",
						tc.name, mode, probe.mutated, probe.targetCount, probe.documentReplayRead, probe.postingReplayRead,
						capture != nil, captureErr, probe.laterBodyQueries)
				}
			})
		}
	}
}

func TestPurchaseCoverageManualProofOwnerDriftIsAtomicAtFactAndAccountReads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		table     string
		keyColumn string
		key       func(*testing.T, *sqlcipher.Transaction, *tammyv1.Journal) string
		largeSQL  string
	}{
		{name: "tax-fact", table: "tax_facts", keyColumn: "id",
			key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
				return purchaseBASScalarText(t, tx, `SELECT id FROM tax_facts WHERE journal_line_id=?`, j.Lines[0].Id)
			}, largeSQL: ",source_content_hash=zeroblob(?)"},
		{name: "cash-flow-fact", table: "cash_flow_facts", keyColumn: "id",
			key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
				return purchaseBASScalarText(t, tx, `SELECT id FROM cash_flow_facts WHERE journal_line_id=? ORDER BY sequence LIMIT 1`, j.Lines[1].Id)
			}},
		{name: "account", table: "accounts", keyColumn: "id",
			key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
				return purchaseBASScalarText(t, tx, `SELECT l.account_id FROM journal_lines l WHERE l.journal_id=? AND l.credit_minor>l.debit_minor`, j.Id)
			}, largeSQL: ",name=zeroblob(?)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, f, journal := purchaseReferenceManualFixture(t, true)
			purchaseReferenceBaselineHasManualEdge(t, f, tx, journal)
			key := tc.key(t, tx, journal)
			matchProof := func(query string, args []any) bool {
				if tc.table == "accounts" {
					return purchaseReferenceAccountQuery(query, args, key)
				}
				return purchaseReferenceProofQuery(query, tc.table)
			}
			preflight := func(query string, args []any) bool {
				if tc.table == "accounts" {
					return matchProof(query, args) && strings.Contains(purchaseReferenceCompactSQL(query), "total(payload_bytes)")
				}
				return purchaseReferenceProofPreflight(query, tc.table)
			}
			probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t, requirePreflight: true,
				matchTarget: func(query string, args []any) bool {
					return matchProof(query, args) && !strings.Contains(purchaseReferenceCompactSQL(query), "total(payload_bytes)")
				},
				matchPreflight: preflight, measureAllocation: tc.largeSQL != ""}
			probe.mutate = func() {
				setOwner := "organisation_id=CAST(? AS BLOB)"
				args := []any{postingOrg}
				if tc.largeSQL != "" {
					args = append(args, purchaseReferenceLargeCell)
				}
				args = append(args, key)
				mutatePurchaseBASScalar(t, f, tc.table,
					"UPDATE "+tc.table+" SET "+setOwner+tc.largeSQL+" WHERE "+tc.keyColumn+"=?", args...)
			}
			capture, err := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
			if !probe.mutated || probe.preflightCount == 0 || capture != nil || !errors.Is(err, purchases.ErrRepository) ||
				probe.laterBodyQueries != 0 || !tx.Authenticated() {
				t.Fatalf("late selected %s owner changed proof capture: mutated=%t preflights=%d capture=%v err=%v later bodies=%d",
					tc.name, probe.mutated, probe.preflightCount, capture != nil, err, probe.laterBodyQueries)
			}
			if probe.measureAllocation {
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				if after.TotalAlloc-probe.beforeRead.TotalAlloc >= 2*1024*1024 {
					t.Fatalf("late %s owner guard copied its 9 MiB selected cell: %d bytes",
						tc.name, after.TotalAlloc-probe.beforeRead.TotalAlloc)
				}
			}
		})
	}
}

func TestPurchaseCoverageManualProofRejectsLateSelectedOwnerBeforeBodies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		table    string
		key      func(*testing.T, *sqlcipher.Transaction, *tammyv1.Journal) string
		largeSQL string
	}{
		{name: "tax-fact", table: "tax_facts", key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
			return purchaseBASScalarText(t, tx, `SELECT id FROM tax_facts WHERE journal_line_id=?`, j.Lines[0].Id)
		}, largeSQL: ",source_content_hash=zeroblob(?)"},
		{name: "cash-flow-fact", table: "cash_flow_facts", key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
			return purchaseBASScalarText(t, tx, `SELECT id FROM cash_flow_facts WHERE journal_line_id=? ORDER BY sequence LIMIT 1`, j.Lines[1].Id)
		}},
		{name: "account", table: "accounts", key: func(t *testing.T, tx *sqlcipher.Transaction, j *tammyv1.Journal) string {
			return purchaseBASScalarText(t, tx, `SELECT l.account_id FROM journal_lines l WHERE l.journal_id=? AND l.credit_minor>l.debit_minor`, j.Id)
		}, largeSQL: ",name=zeroblob(?)"},
	} {
		for _, storage := range []string{"foreign", "blob"} {
			t.Run(tc.name+"/"+storage, func(t *testing.T) {
				tx, f, journal := purchaseReferenceManualFixture(t, true)
				purchaseReferenceBaselineHasManualEdge(t, f, tx, journal)
				key := tc.key(t, tx, journal)
				matchProof := func(query string, args []any) bool {
					if tc.table == "accounts" {
						return purchaseReferenceAccountQuery(query, args, key)
					}
					return purchaseReferenceProofQuery(query, tc.table)
				}
				matchPreflight := func(query string, args []any) bool {
					if tc.table == "accounts" {
						return matchProof(query, args) && strings.Contains(purchaseReferenceCompactSQL(query), "total(payload_bytes)")
					}
					return purchaseReferenceProofPreflight(query, tc.table)
				}
				probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t, requirePreflight: true,
					matchTarget:    func(query string, args []any) bool { return matchPreflight(query, args) },
					matchPreflight: matchPreflight, measureAllocation: tc.largeSQL != ""}
				probe.mutate = func() {
					ownerSQL := "organisation_id=?"
					ownerValue := any("foreign-owner")
					if storage == "blob" {
						ownerSQL = "organisation_id=CAST(? AS BLOB)"
						ownerValue = postingOrg
					}
					args := []any{ownerValue}
					if tc.largeSQL != "" {
						args = append(args, purchaseReferenceLargeCell)
					}
					args = append(args, key)
					mutatePurchaseBASScalar(t, f, tc.table,
						"UPDATE "+tc.table+" SET "+ownerSQL+tc.largeSQL+" WHERE id=?", args...)
				}
				capture, err := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
				if !probe.mutated || probe.preflightCount == 0 || capture != nil || !errors.Is(err, purchases.ErrRepository) ||
					probe.laterBodyQueries != 0 || !tx.Authenticated() {
					t.Fatalf("late selected %s %s owner was hidden by proof preflight: mutated=%t preflights=%d capture=%v err=%v later bodies=%d",
						tc.name, storage, probe.mutated, probe.preflightCount, capture != nil, err, probe.laterBodyQueries)
				}
				if probe.measureAllocation {
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					if after.TotalAlloc-probe.beforeRead.TotalAlloc >= 2*1024*1024 {
						t.Fatalf("late %s owner preflight copied its 9 MiB selected cell: %d bytes", tc.name,
							after.TotalAlloc-probe.beforeRead.TotalAlloc)
					}
				}
			})
		}
	}
}

func TestPurchaseReferenceCanonicalNullableAndModernScopeControls(t *testing.T) {
	for _, scenario := range []string{"posted-portion", "attribution-without-deferral"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			if scenario == "posted-portion" {
				if _, err := deferCommand(t, repo, deferRequest(t, f, state, portion)); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := attributeCommand(t, repo, claimRequest(t, f, state, portion)); err != nil {
					t.Fatal(err)
				}
			}
			if postScalar(t, tx, `SELECT count(*) FROM purchase_tax_evidence_v1
				WHERE organisation_id=? AND source_document_id IS NULL AND source_version IS NULL AND source_hash IS NULL`, postingOrg) == 0 {
				t.Fatal("real writer fixture omitted the valid all-NULL evidence source triple")
			}
			if scenario == "posted-portion" && postScalar(t, tx, `SELECT count(*) FROM purchase_gst_deferral_portions_v1
				WHERE organisation_id=? AND source_kind='POSTING' AND payment_id IS NULL`, postingOrg) == 0 {
				t.Fatal("real POSTING-source deferral portion lost its valid NULL payment reference")
			}
			if scenario == "attribution-without-deferral" && postScalar(t, tx, `SELECT count(*) FROM purchase_gst_attributions_v1
				WHERE organisation_id=? AND payment_id IS NULL AND closes_deferral_id IS NULL`, postingOrg) == 0 {
				t.Fatal("real POSTING-source attribution lost its valid NULL payment/deferral references")
			}
			modern, err := repo.CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modern == nil {
				t.Fatalf("canonical nullable modern capture=%v err=%v", modern != nil, err)
			}
			for _, row := range modern.Rows {
				if row.Table != "purchase_accounting_events_v1" {
					continue
				}
				scope, ok := retainedValue(row, "scope_kind")
				if !ok || scope.Kind != "TEXT" || scope.Text != "POSTING" {
					t.Fatalf("modern event reader widened its scope: %+v", row)
				}
			}
			coverage, err := repo.CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || coverage == nil {
				t.Fatalf("canonical nullable full coverage=%v err=%v", coverage != nil, err)
			}
		})
	}
}

func TestPurchaseManualSourceSelectionRejectsBrokenReferenceEdges(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		factOnly                 bool
		table, column, keyColumn string
		blob                     bool
	}{
		{"direct BLOB source", false, "journals", "source_id", "id", true},
		{"fact BLOB source", true, "tax_facts", "source_id", "journal_line_id", true},
		{"missing fact line", true, "tax_facts", "journal_line_id", "id", false},
		{"BLOB fact line", true, "tax_facts", "journal_line_id", "id", true},
		{"missing line journal", true, "journal_lines", "journal_id", "id", false},
		{"direct empty source type", false, "journals", "source_type", "id", false},
		{"fact empty source type", true, "journals", "source_type", "id", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, f, journal := purchaseReferenceManualFixture(t, tc.factOnly)
			purchaseReferenceBaselineHasManualEdge(t, f, tx, journal)
			key := journal.Id
			if tc.table == "tax_facts" {
				key = journal.Lines[0].Id
				if tc.keyColumn == "id" {
					key = purchaseBASScalarText(t, tx, `SELECT id FROM tax_facts WHERE journal_line_id=?`, key)
				}
			} else if tc.table == "journal_lines" {
				key = journal.Lines[0].Id
			}
			original := purchaseBASScalarText(t, tx, "SELECT "+tc.column+" FROM "+tc.table+" WHERE "+tc.keyColumn+"=?", key)
			probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t,
				matchTarget: func(query string, _ []any) bool { return purchaseReferencePriorEdgesPreflight(query) }}
			probe.mutate = func() {
				value, set := any(purchaseReferenceMissingID), tc.column+"=?"
				if tc.column == "source_type" {
					value = ""
				}
				if tc.blob {
					value = original
					set = tc.column + "=CAST(? AS BLOB)"
				}
				mutatePurchaseBASScalar(t, f, tc.table, "UPDATE "+tc.table+" SET "+set+" WHERE "+tc.keyColumn+"=?", value, key)
			}
			preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
			if !probe.mutated || preflight != nil || !errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 || !tx.Authenticated() {
				t.Fatalf("broken manual reference mutated=%t result=%v err=%v later_bodies=%d", probe.mutated, preflight != nil, err, probe.laterBodyQueries)
			}
		})
	}
}

func TestPurchaseCoverageCashFlowFactRejectsLateBLOBLineReferenceBeforeBodies(t *testing.T) {
	tx, f, journal := purchaseReferenceManualFixture(t, false)
	factID := purchaseBASScalarText(t, tx, `SELECT id FROM cash_flow_facts
		WHERE journal_line_id=? ORDER BY sequence LIMIT 1`, journal.Lines[1].Id)
	lineID := purchaseBASScalarText(t, tx,
		`SELECT journal_line_id FROM cash_flow_facts WHERE id=?`, factID)
	preflight := func(query string, _ []any) bool {
		return purchaseReferenceProofPreflight(query, "cash_flow_facts")
	}
	probe := &purchaseReferenceQueryProbe{
		CommandSQLExecutor: tx, t: t, requirePreflight: true,
		matchTarget: preflight, matchPreflight: preflight,
	}
	probe.mutate = func() {
		mutatePurchaseBASScalar(t, f, "cash_flow_facts",
			`UPDATE cash_flow_facts SET journal_line_id=CAST(? AS BLOB) WHERE id=?`, lineID, factID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
	if !probe.mutated || probe.preflightCount == 0 || capture != nil ||
		!errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 {
		t.Fatalf("selected cash-flow fact BLOB line ref escaped its preflight guard: mutated=%t preflights=%d capture=%v err=%v later bodies=%d",
			probe.mutated, probe.preflightCount, capture != nil, err, probe.laterBodyQueries)
	}
}

// Existing accounting parent preflight already rejects a missing referenced
// account before purchase fact/account preflights. Keep it as an early control.
func TestPurchaseCoverageMissingSelectedAccountRejectsBeforeBodies(t *testing.T) {
	tx, f, journal := purchaseReferenceManualFixture(t, false)
	mutatePurchaseBASScalar(t, f, "journal_lines",
		`UPDATE journal_lines SET account_id=? WHERE id=?`, purchaseReferenceMissingID, journal.Lines[1].Id)
	probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t, mutated: true}
	capture, err := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
	if capture != nil || !errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 || !tx.Authenticated() {
		t.Fatalf("missing selected account capture=%v err=%v body queries=%d", capture != nil, err, probe.laterBodyQueries)
	}
}

// Draft additions for reporting_reference_manual_sqlcipher_test.go.
// Existing imports already include context, database/sql, errors, runtime,
// strings, testing, app, tammyv1, purchases, and sqlcipher.

type purchaseCoverageClosureProbe struct {
	app.CommandSQLExecutor
	t                    *testing.T
	mutate               func()
	mutated              bool
	preflightCount       int
	targetCount          int
	laterClosureBodyRuns int
	targetOwnerSentinel  bool
	measureAllocation    bool
	beforeRead           runtime.MemStats
}

func purchaseReferenceJournalClosureQuery(query string, args []any) bool {
	compact := purchaseReferenceCompactSQL(query)
	return strings.Contains(compact, "fromjournalsasj") &&
		strings.Contains(compact, "j.reversal_of_journal_id") &&
		strings.Contains(compact, "j.reversed_by_journal_id") &&
		purchaseReferenceHasStringArg(args, postingOrg)
}

func (p *purchaseCoverageClosureProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	closure := purchaseReferenceJournalClosureQuery(query, args)
	preflight := closure && strings.Contains(purchaseReferenceCompactSQL(query), "selectcount(*),total(payload_bytes)")
	if preflight {
		p.preflightCount++
	}
	target := closure && !preflight && !p.mutated
	if target {
		if p.preflightCount == 0 {
			p.t.Fatalf("reversal closure mutation reached body before its own preflight: %q", query)
		}
		p.mutate()
		p.mutated = true
		p.targetCount++
		if p.measureAllocation {
			runtime.GC()
			runtime.ReadMemStats(&p.beforeRead)
		}
	} else if closure && !preflight && p.mutated {
		p.laterClosureBodyRuns++
	}

	rows, err := p.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if target && err == nil && rows != nil {
		columns, columnErr := rows.Columns()
		if columnErr != nil {
			p.t.Fatalf("read reversal closure result columns: %v", columnErr)
		}
		p.targetOwnerSentinel = len(columns) > 0 && strings.EqualFold(columns[0], "invalid_owner")
	}
	return rows, err
}

func TestPurchaseCoverageJournalClosureRejectsLateMalformedHeadersBeforeCopy(t *testing.T) {
	for _, tc := range []struct {
		name              string
		measureAllocation bool
		mutate            func(*testing.T, *postingFixture, *tammyv1.Journal, *tammyv1.Journal)
	}{
		{
			name:              "late BLOB reversal link",
			measureAllocation: true,
			mutate: func(t *testing.T, f *postingFixture, original, _ *tammyv1.Journal) {
				mutatePurchaseBASScalar(t, f, "journals",
					`UPDATE journals SET reversed_by_journal_id=zeroblob(?) WHERE id=?`, purchaseReferenceLargeCell, original.Id)
			},
		},
		{
			name:              "late oversized TEXT reversal link",
			measureAllocation: true,
			mutate: func(t *testing.T, f *postingFixture, original, _ *tammyv1.Journal) {
				mutatePurchaseBASScalar(t, f, "journals",
					`UPDATE journals SET reversed_by_journal_id=CAST(zeroblob(?) AS TEXT) WHERE id=?`, purchaseReferenceLargeCell, original.Id)
			},
		},
		{
			name: "late foreign linked header owner",
			mutate: func(t *testing.T, f *postingFixture, _, reversal *tammyv1.Journal) {
				purchaseReferenceSetOwner(t, f, "journals", "id", reversal.Id, "foreign")
			},
		},
		{
			name: "late BLOB linked header owner",
			mutate: func(t *testing.T, f *postingFixture, _, reversal *tammyv1.Journal) {
				purchaseReferenceSetOwner(t, f, "journals", "id", reversal.Id, "blob")
			},
		},
		{
			name: "late empty selected header sentinel",
			mutate: func(t *testing.T, f *postingFixture, original, _ *tammyv1.Journal) {
				purchaseReferenceSetOwner(t, f, "journals", "id", original.Id, "foreign")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, f, original := purchaseReferenceManualFixture(t, false)
			purchaseReferenceBaselineHasManualEdge(t, f, tx, original)
			reversed, reversal := reverseLegacyReversalLinkedJournal(t, f, original)
			baseline, err := f.repo(tx).CaptureCoverageForBAS(context.Background(), postingOrg)
			var baselineIDs []string
			if baseline != nil {
				baselineIDs = baseline.JournalIDs
			}
			if err != nil || baseline == nil || !coverageHasID(baselineIDs, reversed.Id) || !coverageHasID(baselineIDs, reversal.Id) {
				t.Fatalf("writer-backed reversal closure baseline missing reciprocal journals: capture=%v ids=%v err=%v",
					baseline != nil, baselineIDs, err)
			}

			probe := &purchaseCoverageClosureProbe{
				CommandSQLExecutor: tx, t: t, measureAllocation: tc.measureAllocation,
				mutate: func() { tc.mutate(t, f, reversed, reversal) },
			}
			capture, captureErr := f.repo(probe).CaptureCoverageForBAS(context.Background(), postingOrg)
			var allocated uint64
			if tc.measureAllocation {
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				allocated = after.TotalAlloc - probe.beforeRead.TotalAlloc
			}
			if !probe.mutated || probe.preflightCount == 0 || probe.targetCount != 1 || !probe.targetOwnerSentinel ||
				capture != nil || !errors.Is(captureErr, purchases.ErrRepository) || probe.laterClosureBodyRuns != 0 || !tx.Authenticated() {
				t.Fatalf("late reversal closure corruption escaped its own bounded guard: mutated=%t preflights=%d targets=%d sentinel=%t capture=%v err=%v later closure bodies=%d allocated=%d",
					probe.mutated, probe.preflightCount, probe.targetCount, probe.targetOwnerSentinel, capture != nil, captureErr, probe.laterClosureBodyRuns, allocated)
			}
			if tc.measureAllocation {
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				if allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc; allocated >= 2*1024*1024 {
					t.Fatalf("late 9 MiB reversal link was copied before rejection: allocated=%d", allocated)
				}
			}
		})
	}
}
