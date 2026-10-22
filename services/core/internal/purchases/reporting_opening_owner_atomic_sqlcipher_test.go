//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

const purchaseOpeningAtomicLateCellBytes = 9 * 1024 * 1024

// Writer-backed SQLCipher regressions inject late drift at actual reporting
// statements. Old SQL shapes remain recognized so removing a guard restores
// the same regression path.

type purchaseOpeningAtomicProbe struct {
	app.CommandSQLExecutor
	t                        *testing.T
	fixture                  *postingFixture
	tx                       *sqlcipher.Transaction
	stage                    string
	reject                   string
	mutation                 func()
	postingMutation          func()
	targetID                 string
	documentID               string
	hookCount                int
	postingHookCount         int
	postingReplayReads       int
	openingPriorReplayReads  int
	rawOpeningBodyReads      int
	rejects                  int
	laterBodies              int
	rawPreflightReads        int
	documentReplayReads      int
	legacyScalarCountQueries int
	legacyScalarCountGuarded bool
	rejectionMatched         bool
	stateAfterMutation       map[string]any
	triggersBefore           map[string]string
	triggersAfterMutation    map[string]string
	beforeRead               runtime.MemStats
	lastRejectQuery          string
}

func (probe *purchaseOpeningAtomicProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if purchaseOpeningAtomicIsRawOpeningBody(query) {
		probe.rawOpeningBodyReads++
	}
	if probe.stage == "legacy-prior-review" && probe.postingHookCount > 0 &&
		purchaseOpeningAtomicIsOpeningPriorReplay(query) && purchaseOpeningAtomicHasArg(args, postingOrg) {
		probe.openingPriorReplayReads++
	}
	if probe.rejectionMatched && purchaseOpeningAtomicIsBody(query) {
		probe.laterBodies++
	}
	if probe.stage == "raw-opening" && strings.Contains(strings.ToLower(query), "total(payload_bytes)") &&
		strings.Contains(strings.ToLower(query), "opening_conversions") {
		probe.rawPreflightReads++
	}
	if purchaseOpeningAtomicIsLegacyScalarCount(query) {
		probe.legacyScalarCountQueries++
		probe.legacyScalarCountGuarded = strings.Contains(query, "purchase_owner_guard") &&
			strings.Contains(query, "invalid_owner") && strings.Contains(strings.ToLower(query), "legacy scalar replay")
	}

	if (probe.stage == "opening-prior" || probe.stage == "legacy-prior-review") && purchaseOpeningAtomicIsPostingReplay(query) &&
		purchaseOpeningAtomicHasArg(args, probe.documentID) {
		probe.postingReplayReads++
		if probe.postingHookCount == 0 {
			if probe.documentReplayReads == 0 {
				probe.t.Fatalf("%s posting reference mutation ran before its target document replay", probe.stage)
			}
			if probe.postingMutation == nil {
				probe.t.Fatalf("%s posting replay has no reference mutation", probe.stage)
			}
			probe.postingHookCount++
			before := purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
			probe.postingMutation()
			after := purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
			if !reflect.DeepEqual(before, after) {
				probe.t.Fatal("posting reference fixture mutation did not restore exact trigger definitions")
			}
			if got := postScalar(probe.t, probe.tx, "PRAGMA ignore_check_constraints"); got != 0 {
				probe.t.Fatalf("posting reference fixture mutation left ignore_check_constraints=%d, want 0", got)
			}
		}
	}

	if probe.hookCount == 0 && probe.matchesRejectTarget(query, args) {
		if probe.stage == "raw-opening" && probe.rawPreflightReads == 0 {
			probe.t.Fatal("raw opening body hook ran before its own successful payload preflight")
		}
		if (probe.stage == "opening-payable" || probe.stage == "legacy-review" || probe.stage == "legacy-event" || probe.stage == "legacy-prior-review") &&
			probe.documentReplayReads == 0 {
			probe.t.Fatalf("%s replay hook ran before a completed target document replay", probe.stage)
		}
		if probe.stage == "conversion-header" && probe.rawOpeningBodyReads == 0 {
			probe.t.Fatal("conversion header hook ran before the raw opening body was read")
		}
		if probe.stage == "legacy-prior-review" && probe.openingPriorReplayReads == 0 {
			probe.t.Fatal("legacy prior review hook ran before MatchingPriorSources completed")
		}
		probe.hookCount++
		probe.triggersBefore = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
		probe.mutation()
		probe.triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
		if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) {
			probe.t.Fatal("fixture mutation did not restore exact trigger definitions")
		}
		if got := postScalar(probe.t, probe.tx, "PRAGMA ignore_check_constraints"); got != 0 {
			probe.t.Fatalf("fixture mutation left ignore_check_constraints=%d, want 0", got)
		}
		probe.stateAfterMutation = purchaseReportingRootOwnerState(probe.t, probe.tx, "full-coverage")
		runtime.GC()
		runtime.ReadMemStats(&probe.beforeRead)
	}

	if probe.hookCount > 0 && probe.matchesRejectTarget(query, args) {
		if (probe.stage == "opening-prior" || probe.stage == "legacy-prior-review") && (probe.documentReplayReads == 0 ||
			probe.postingHookCount != 1 || probe.postingReplayReads != 1) {
			probe.t.Fatalf("%s guard ran without document/posting replay; document=%d posting-hook=%d posting-reads=%d",
				probe.stage, probe.documentReplayReads, probe.postingHookCount, probe.postingReplayReads)
		}
		if probe.stage == "legacy-prior-review" && probe.openingPriorReplayReads == 0 {
			probe.t.Fatal("legacy prior review guard ran before MatchingPriorSources completed")
		}
		probe.rejects++
		probe.rejectionMatched = true
		probe.lastRejectQuery = query
	}

	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err == nil && rows != nil {
		if purchaseOpeningAtomicIsDocumentReplay(query) && purchaseOpeningAtomicHasArg(args, probe.documentID) {
			probe.documentReplayReads++
		}
	}
	return rows, err
}

func (probe *purchaseOpeningAtomicProbe) matchesReject(query string, args []any) bool {
	lower := strings.ToLower(query)
	compact := purchaseOpeningAtomicCompactSQL(query)
	switch probe.reject {
	case "raw-opening":
		return strings.Contains(lower, "/* bas purchase owner body opening_conversions */") ||
			strings.HasPrefix(compact, "selectc.id,c.organisation_id,c.conversion_date,c.state,c.source_sha256,c.journal_id,c.version,c.replaced_by_id,c.financial_revision,c.created_atfromopening_conversionsascwhere")
	case "closure":
		return strings.Contains(lower, "/* bas purchase owner body coverage_conversion_closure */") ||
			(strings.HasPrefix(compact, "selectc.id,c.organisation_id,c.replaced_by_idfrom(") && strings.Contains(compact, "withrecursiveclosure(id)as("))
	case "raw-payable":
		return strings.Contains(lower, "/* bas purchase owner body purchase_opening_payables */") ||
			strings.HasPrefix(compact, "selectp.id,p.conversion_id,p.retained_input_proto,p.created_atfrompurchase_opening_payablesaspwhere")
	case "raw-item":
		return strings.Contains(lower, "/* bas purchase owner body opening_items */") ||
			strings.HasPrefix(compact, "selecti.id,i.conversion_id,i.account_id,i.item_kind,i.debit_minor,i.credit_minor,i.currency_code,i.source_type,")
	case "opening-payable":
		return strings.Contains(lower, "opening payable replay") ||
			(strings.Contains(compact, "c.source_sha256") && strings.Contains(compact, "and(?=''orp.id=?)") &&
				strings.Contains(compact, "frompurchase_opening_payablespjoinopening_conversionsc"))
	case "opening-prior":
		return strings.Contains(lower, "opening prior replay") ||
			(strings.HasPrefix(compact, "selectp.id,p.conversion_id,p.retained_input_proto,c.version,c.conversion_date,") &&
				!strings.Contains(compact, "c.source_sha256") && strings.Contains(compact, "frompurchase_opening_payablespjoinopening_conversionsc"))
	case "legacy-review":
		return strings.Contains(lower, "legacy review replay") ||
			strings.HasPrefix(compact, "selectid,review_proto,record_hashfrompurchase_legacy_reviews_v1whereorganisation_id=?andsource_kind=?andcoalesce(document_id,opening_payable_id)=?orderbyaccounting_version")
	case "legacy-prior-review":
		return strings.Contains(lower, "legacy prior review replay") ||
			strings.HasPrefix(compact, "selectid,review_proto,record_hashfrompurchase_legacy_reviews_v1whereorganisation_id=?and(document_id=?orsource_hash=?)orderbyid")
	case "legacy-event":
		return strings.Contains(lower, "legacy event replay") ||
			strings.HasPrefix(compact, "selecte.operation_key,e.accounting_version,e.command_type,e.result_proto,e.result_sha256,e.created_at,e.posting_id,e.document_id,e.opening_payable_id,e.journal_id,e.financial_revision,e.ledger_revision,e.tax_source_revision,e.settlement_revision,c.domain_mask,c.financial_revision,c.ledger_revision,c.tax_source_revision,c.settlement_revisionfrompurchase_accounting_events_v1eleftjoinfinancial_revision_claimsc")
	case "conversion-header":
		return strings.Contains(lower, "bas purchase conversion header verification") ||
			strings.HasPrefix(compact, "selectid,organisation_id,version,state,conversion_date,journal_id,replaced_by_id,financial_revisionfromopening_conversionswhereid=?")
	case "legacy-scalar":
		return purchaseOpeningAtomicIsLegacyScalarCount(query)
	default:
		return false
	}
}

func (probe *purchaseOpeningAtomicProbe) matchesRejectTarget(query string, args []any) bool {
	if !probe.matchesReject(query, args) {
		return false
	}
	switch probe.reject {
	case "opening-prior":
		return purchaseOpeningAtomicHasArg(args, postingOrg)
	case "legacy-prior-review":
		return purchaseOpeningAtomicHasArg(args, probe.documentID)
	case "conversion-header":
		return purchaseOpeningAtomicHasArg(args, probe.targetID)
	case "opening-payable":
		return purchaseOpeningAtomicHasArg(args, probe.targetID)
	case "legacy-review":
		return purchaseOpeningAtomicTailArgsEqual(args, postingOrg, "OPENING_PAYABLE", probe.targetID)
	case "legacy-event":
		return purchaseOpeningAtomicTailArgsEqual(args, postingOrg, "OPENING_PAYABLE", probe.targetID)
	default:
		return purchaseOpeningAtomicHasArg(args, probe.targetID)
	}
}

func (probe *purchaseOpeningAtomicProbe) marker() string {
	switch probe.reject {
	case "raw-opening":
		return "/* BAS purchase owner body opening_conversions */"
	case "closure":
		return "/* BAS purchase owner body coverage_conversion_closure */"
	case "raw-payable":
		return "/* BAS purchase owner body purchase_opening_payables */"
	case "raw-item":
		return "/* BAS purchase owner body opening_items */"
	case "opening-payable":
		return "opening payable replay"
	case "opening-prior":
		return "opening prior replay"
	case "legacy-review":
		return "legacy review replay"
	case "legacy-prior-review":
		return "legacy prior review replay"
	case "legacy-event":
		return "legacy event replay"
	case "conversion-header":
		return "BAS purchase conversion header verification"
	default:
		return ""
	}
}

func purchaseOpeningAtomicCompactSQL(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), ""))
}

func purchaseOpeningAtomicIsRawOpeningBody(query string) bool {
	return strings.Contains(query, "/* BAS purchase owner body opening_conversions */") ||
		strings.HasPrefix(purchaseOpeningAtomicCompactSQL(query), "selectc.id,c.organisation_id,c.conversion_date,c.state,c.source_sha256,")
}

func purchaseOpeningAtomicIsOpeningPriorReplay(query string) bool {
	compact := purchaseOpeningAtomicCompactSQL(query)
	return strings.Contains(query, "/* BAS purchase owner body opening prior replay */") ||
		(strings.HasPrefix(compact, "selectp.id,p.conversion_id,p.retained_input_proto,c.version,c.conversion_date,") &&
			!strings.Contains(compact, "c.source_sha256") && strings.Contains(compact, "frompurchase_opening_payablespjoinopening_conversionsc"))
}

func purchaseOpeningAtomicHasArg(args []any, want string) bool {
	for _, arg := range args {
		if value, ok := arg.(string); ok && value == want {
			return true
		}
	}
	return false
}

func purchaseOpeningAtomicTailArgsEqual(args []any, want ...any) bool {
	if len(args) < len(want) {
		return false
	}
	return reflect.DeepEqual(args[len(args)-len(want):], want)
}

func purchaseOpeningAtomicIsDocumentReplay(query string) bool {
	lower := strings.ToLower(query)
	return strings.Contains(lower, "/* bas purchase owner body document replay */") ||
		strings.HasPrefix(purchaseOpeningAtomicCompactSQL(query), "selectoriginal_bytesfromdocumentswhereid=?andorganisation_id=?")
}

// Body classification is deliberately limited to known row projections and
// stable body markers. It excludes total(payload_bytes), count scalars, and the
// legacy scalar-count marker so metadata cannot masquerade as a body read.
func purchaseOpeningAtomicIsBody(query string) bool {
	lower := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(lower, "total(payload_bytes)") || strings.Contains(lower, "selectcount(") ||
		strings.Contains(lower, "bas purchase owner count legacy scalar replay") {
		return false
	}
	for _, bodyMarker := range []string{
		"opening prior replay", "opening payable replay", "legacy review replay", "legacy prior review replay", "legacy event replay",
		"/* bas purchase owner body ",
	} {
		if strings.Contains(lower, bodyMarker) {
			return true
		}
	}
	compact := purchaseOpeningAtomicCompactSQL(query)
	for _, prefix := range []string{
		"selectc.id,c.organisation_id,c.conversion_date,c.state,c.source_sha256,",
		"selectp.id,p.conversion_id,p.retained_input_proto,",
		"selectid,review_proto,record_hashfrompurchase_legacy_reviews_v1",
		"selecte.operation_key,e.accounting_version,e.command_type,e.result_proto,",
		"selectoriginal_bytesfromdocuments",
	} {
		if strings.HasPrefix(compact, prefix) {
			return true
		}
	}
	return false
}

func purchaseOpeningAtomicIsLegacyScalarCount(query string) bool {
	lower := strings.ToLower(query)
	compact := purchaseOpeningAtomicCompactSQL(query)
	return strings.Contains(lower, "bas purchase owner count legacy scalar replay") ||
		strings.HasPrefix(compact, "selectcount(*)frompurchase_legacy_reviews_v1whereid=?andorganisation_id=?")
}

func purchaseOpeningAtomicAssertGuardedBody(t *testing.T, probe *purchaseOpeningAtomicProbe, nilCapture bool, err error, after runtime.MemStats) {
	t.Helper()
	allocation := after.TotalAlloc - probe.beforeRead.TotalAlloc
	errorBytes := 0
	if err != nil {
		errorBytes = len(err.Error())
	}
	if probe.hookCount != 1 || probe.rejects != 1 {
		t.Fatalf("late %s hooks/rejection=%d/%d, want exactly one; capture_nil=%t err=%v", probe.stage, probe.hookCount, probe.rejects, nilCapture, err)
	}
	if !nilCapture || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("late %s capture_nil=%t error_bytes=%d allocation=%d err=%q; want nil capture and ErrRepository", probe.stage, nilCapture, errorBytes, allocation, err)
	}
	if errorBytes >= 512 || allocation >= 2*1024*1024 {
		t.Fatalf("late %s guard copied a rejected 9 MiB body: error_bytes=%d allocation=%d err=%q", probe.stage, errorBytes, allocation, err)
	}
	if probe.laterBodies != 0 {
		t.Fatalf("late %s rejection attempted %d subsequent body queries", probe.stage, probe.laterBodies)
	}
	if probe.stateAfterMutation == nil || !reflect.DeepEqual(probe.stateAfterMutation,
		purchaseReportingRootOwnerState(t, probe.tx, "full-coverage")) {
		t.Fatal("late opening/legacy rejection changed caller transaction state after the fixture mutation")
	}
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) ||
		!reflect.DeepEqual(probe.triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, probe.tx)) {
		t.Fatal("late opening/legacy rejection changed trigger definitions")
	}
	if !probe.tx.Authenticated() {
		t.Fatal("late opening/legacy rejection ended the caller-owned encrypted transaction")
	}
	if got := postScalar(t, probe.tx, "PRAGMA ignore_check_constraints"); got != 0 {
		t.Fatalf("late opening/legacy rejection left ignore_check_constraints=%d, want 0", got)
	}
	query := probe.lastRejectQuery
	if !strings.Contains(strings.ToLower(query), "purchase_owner_guard") ||
		!strings.Contains(strings.ToLower(query), "invalid_owner") || !strings.Contains(strings.ToLower(query), "case when") ||
		!strings.Contains(strings.ToLower(query), "purchase_owner_rows") || !strings.Contains(strings.ToLower(query), strings.ToLower(probe.marker())) {
		t.Fatalf("%s body lacks its stable same-statement owner sentinel and full projection mask", probe.reject)
	}
	if probe.reject == "legacy-event" && (!probe.legacyScalarCountGuarded || probe.legacyScalarCountQueries != 1) {
		t.Fatalf("legacy event replay scalar-count guard queries=%d guarded=%t; want one guarded count marker/query", probe.legacyScalarCountQueries, probe.legacyScalarCountGuarded)
	}
}

func purchaseOpeningAtomicAssertLateCellMetadata(t *testing.T, tx *sqlcipher.Transaction, table, column, id string) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), "SELECT typeof("+column+"),length("+column+") FROM "+table+" WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("missing %s row %s", table, id)
	}
	var storageType string
	var byteLength int64
	if err := rows.Scan(&storageType, &byteLength); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatalf("non-unique %s row %s", table, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if storageType != "blob" || byteLength != purchaseOpeningAtomicLateCellBytes {
		t.Fatalf("late %s.%s metadata=%s/%d, want BLOB/%d", table, column, storageType, byteLength, purchaseOpeningAtomicLateCellBytes)
	}
}

func purchaseOpeningAtomicAssertLateHeader(t *testing.T, probe *purchaseOpeningAtomicProbe, nilCapture bool, err error, after runtime.MemStats) {
	t.Helper()
	allocation := after.TotalAlloc - probe.beforeRead.TotalAlloc
	errorBytes := 0
	if err != nil {
		errorBytes = len(err.Error())
	}
	if probe.hookCount != 1 || probe.rejects != 1 || !nilCapture || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("late conversion header hooks/rejection=%d/%d nil=%t err=%v; want one late hook, nil capture, ErrRepository", probe.hookCount, probe.rejects, nilCapture, err)
	}
	if allocation >= 2*1024*1024 || errorBytes >= 512 {
		t.Fatalf("late conversion header copied the 9 MiB owner cell: allocation=%d error_bytes=%d err=%q", allocation, errorBytes, err)
	}
	if probe.laterBodies != 0 {
		t.Fatalf("late conversion header rejection attempted %d subsequent body queries", probe.laterBodies)
	}
	if probe.stateAfterMutation == nil || !reflect.DeepEqual(probe.stateAfterMutation,
		purchaseReportingRootOwnerState(t, probe.tx, "full-coverage")) {
		t.Fatal("late conversion header rejection changed caller transaction state after the fixture mutation")
	}
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) ||
		!reflect.DeepEqual(probe.triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, probe.tx)) {
		t.Fatal("late conversion header rejection changed trigger definitions")
	}
	if !probe.tx.Authenticated() {
		t.Fatal("late conversion header rejection ended the caller-owned encrypted transaction")
	}
	if got := postScalar(t, probe.tx, "PRAGMA ignore_check_constraints"); got != 0 {
		t.Fatalf("late conversion header rejection left ignore_check_constraints=%d, want 0", got)
	}
	query := strings.ToLower(probe.lastRejectQuery)
	compact := purchaseOpeningAtomicCompactSQL(probe.lastRejectQuery)
	if !strings.HasPrefix(compact, "selectid,organisation_id,version,state,conversion_date,journal_id,replaced_by_id,financial_revisionfromopening_conversionswhereid=?") ||
		strings.Contains(compact, "source_sha256") {
		t.Fatalf("conversion header query changed its eight-cell projection: %s", probe.lastRejectQuery)
	}
	if strings.Contains(query, "bas purchase conversion header verification") && !strings.Contains(compact, "andorganisation_id=?") {
		t.Fatalf("strict reporting header query lacks its exact TEXT owner predicate: %s", probe.lastRejectQuery)
	}
}

func TestPurchaseOpeningRawLateEmptySelectionIsAtomic(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	f.addDocument("Opening raw target", "OPENING-RAW-TARGET", 10000, 1000)
	firstID, firstConversion := coverageOpeningWithDocument(t, f, f.document, true)
	f.addDocument("Opening raw sibling", "OPENING-RAW-SIBLING", 12000, 1200)
	secondID, secondConversion := coverageOpeningWithDocument(t, f, f.document, false)
	baseline, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || baseline == nil || countCoverageRows(coverageRawRows(baseline), "opening_conversions", "id", firstConversion) != 1 ||
		countCoverageRows(coverageRawRows(baseline), "opening_conversions", "id", secondConversion) != 1 {
		t.Fatalf("writer-backed two-opening capture baseline=%#v err=%v", baseline, err)
	}

	probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
		stage: "raw-opening", reject: "raw-opening", targetID: firstConversion}
	probe.mutation = func() {
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, firstConversion)
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET source_sha256=zeroblob(?) WHERE id=?`, purchaseOpeningAtomicLateCellBytes, secondConversion)
		purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", firstConversion, []byte(postingOrg))
		purchaseOpeningAtomicAssertTextOwner(t, tx, "opening_conversions", secondConversion)
		purchaseRootOwnerAssertLateCellMetadata(t, tx, "opening_conversions", "source_sha256", secondConversion)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
	if firstID == secondID {
		t.Fatal("writer-created openings unexpectedly share payable IDs")
	}
}

func TestPurchaseOpeningPayableReplayLateEmptySelectionIsAtomic(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	f.addDocument("Opening payable replay", "OPENING-PAYABLE-REPLAY", 10000, 1000)
	payableID, conversionID := coverageOpeningWithDocument(t, f, f.document, false)
	baseline, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || baseline == nil || countCoverageRows(coverageRawRows(baseline), "purchase_opening_payables", "id", payableID) != 1 {
		t.Fatalf("writer-backed payable capture baseline=%#v err=%v", baseline, err)
	}
	probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
		stage: "opening-payable", reject: "opening-payable", targetID: payableID, documentID: f.document.Id}
	probe.mutation = func() {
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, conversionID)
		mutatePurchaseBASScalar(t, f, "purchase_opening_payables",
			`UPDATE purchase_opening_payables SET retained_input_proto=zeroblob(?) WHERE id=?`, purchaseOpeningAtomicLateCellBytes, payableID)
		purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", conversionID, []byte(postingOrg))
		purchaseRootOwnerAssertLateCellMetadata(t, tx, "purchase_opening_payables", "retained_input_proto", payableID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
}

func TestPurchaseOpeningPriorReplayLateSiblingBodyIsAtomic(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	posted := postReportingPurchase(t, f, "OPENING-PRIOR-POSTED-REPLAY", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if posted == nil || posted.Posting == nil || posted.Posting.DocumentId == "" || f.document == nil ||
		f.document.Id != posted.Posting.DocumentId {
		t.Fatalf("writer-backed posting fixture=%#v current document=%#v", posted, f.document)
	}
	targetDocumentID := posted.Posting.DocumentId
	_, targetConversion := coverageOpeningWithDocument(t, f, f.document, true)
	siblingPayableID, siblingConversion := coverageOpeningWithDocument(t, f, f.document, false)
	baseline, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || baseline == nil ||
		countCoverageRows(coverageRawRows(baseline), "opening_conversions", "id", targetConversion) != 1 ||
		countCoverageRows(coverageRawRows(baseline), "opening_conversions", "id", siblingConversion) != 1 ||
		len(baseline.Postings) != 1 || baseline.Postings[0].Posting == nil ||
		baseline.Postings[0].Posting.DocumentId != targetDocumentID {
		t.Fatalf("writer-backed posted-document/two-opening prior-source baseline=%#v err=%v", baseline, err)
	}
	probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
		stage: "opening-prior", reject: "opening-prior", targetID: targetConversion, documentID: targetDocumentID}
	probe.postingMutation = func() {
		mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
			`UPDATE purchase_postings_v1 SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, posted.Posting.Id)
		if got := postScalar(t, tx, `SELECT count(*) FROM purchase_postings_v1 WHERE id=? AND organisation_id=? AND document_id=?`,
			posted.Posting.Id, purchaseScalarRootOwnerForeignUUID, targetDocumentID); got != 1 {
			t.Fatalf("late foreign-TEXT posting owner selected %d rows with its unchanged document ID; want one", got)
		}
	}
	probe.mutation = func() {
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, targetConversion)
		mutatePurchaseBASScalar(t, f, "purchase_opening_payables",
			`UPDATE purchase_opening_payables SET retained_input_proto=zeroblob(?) WHERE id=?`, purchaseOpeningAtomicLateCellBytes, siblingPayableID)
		purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", targetConversion, []byte(postingOrg))
		purchaseOpeningAtomicAssertTextOwner(t, tx, "opening_conversions", siblingConversion)
		purchaseRootOwnerAssertLateCellMetadata(t, tx, "purchase_opening_payables", "retained_input_proto", siblingPayableID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
}

func TestPurchaseLegacyPriorReviewReplayLateSiblingBodyIsAtomic(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	f.addDocument("Legacy prior target", "LEGACY-PRIOR-TARGET", 10000, 1000)
	targetDocument, postRequest := f.document, f.request
	targetRequest := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	targetResult, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, targetRequest)
	if err != nil || targetResult == nil || targetResult.Review == nil {
		t.Fatalf("write legacy decision before promotion: response=%v err=%v", targetResult, err)
	}
	targetReview := targetResult.Review
	f.document, f.request = targetDocument, postRequest
	posted, err := f.repo(tx).PostReviewed(ctx, postRequest)
	if err != nil || posted == nil || posted.Posting == nil {
		t.Fatalf("promote reviewed NONCREDITABLE source: response=%v err=%v", posted, err)
	}
	targetDocumentID := targetDocument.Id
	f.addDocument("Legacy prior sibling", "LEGACY-PRIOR-SIBLING", 12000, 1200)
	siblingRequest := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	siblingResult, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, siblingRequest)
	if err != nil || siblingResult == nil || siblingResult.Review == nil {
		t.Fatalf("write independent sibling decision: response=%v err=%v", siblingResult, err)
	}
	siblingReview := siblingResult.Review
	baseline := captureCoverage(t, f.repo(tx), ctx)
	if len(baseline.Postings) != 1 || len(coverageLegacyDocument(t, baseline, targetDocumentID).Reviews) != 1 ||
		postScalar(t, tx, `SELECT count(*) FROM purchase_legacy_reviews_v1 WHERE id=? AND organisation_id=? AND (document_id=? OR source_hash=?)`,
			targetReview.Id, postingOrg, targetDocumentID, targetDocument.Sha256) != 1 {
		t.Fatal("writer-created promoted review is not selected by the exact prior-source predicate")
	}
	probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
		stage: "legacy-prior-review", reject: "legacy-prior-review", targetID: targetReview.Id, documentID: targetDocumentID}
	probe.postingMutation = func() {
		mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
			`UPDATE purchase_postings_v1 SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, posted.Posting.Id)
		if got := postScalar(t, tx, `SELECT count(*) FROM purchase_postings_v1 WHERE id=? AND organisation_id=? AND document_id=?`,
			posted.Posting.Id, purchaseScalarRootOwnerForeignUUID, targetDocumentID); got != 1 {
			t.Fatalf("late foreign-TEXT posting owner selected %d rows with its unchanged document ID; want one", got)
		}
	}
	probe.mutation = func() {
		mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
			`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, siblingReview.Id)
		mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
			`UPDATE purchase_legacy_reviews_v1 SET review_proto=zeroblob(?) WHERE id=?`, purchaseOpeningAtomicLateCellBytes, targetReview.Id)
		purchaseRootOwnerAssertStoredBLOB(t, tx, "purchase_legacy_reviews_v1", siblingReview.Id, []byte(postingOrg))
		purchaseOpeningAtomicAssertTextOwner(t, tx, "purchase_legacy_reviews_v1", targetReview.Id)
		purchaseRootOwnerAssertLateCellMetadata(t, tx, "purchase_legacy_reviews_v1", "review_proto", targetReview.Id)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
}

func TestPurchaseConversionHeaderVerificationLateBLOBIsAtomic(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	f.addDocument("Opening header verification", "OPENING-HEADER-VERIFY", 10000, 1000)
	_, conversionID := coverageOpeningWithDocument(t, f, f.document, false)
	baseline, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || baseline == nil || countCoverageRows(coverageRawRows(baseline), "opening_conversions", "id", conversionID) != 1 {
		t.Fatalf("writer-backed opening header baseline=%#v err=%v", baseline, err)
	}
	probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
		stage: "conversion-header", reject: "conversion-header", targetID: conversionID}
	probe.mutation = func() {
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET organisation_id=zeroblob(?) WHERE id=?`, purchaseOpeningAtomicLateCellBytes, conversionID)
		purchaseOpeningAtomicAssertLateCellMetadata(t, tx, "opening_conversions", "organisation_id", conversionID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchaseOpeningAtomicAssertLateHeader(t, probe, capture == nil, err, after)
}

func TestPurchaseLegacyReviewAndEventReplaysAreAtomic(t *testing.T) {
	t.Run("review-body-with-sibling-BLOB-owner", func(t *testing.T) {
		ctx := context.Background()
		tx, f := purchaseRootOwnerFixture(t)
		defer tx.Rollback()
		f.addDocument("Opening review target", "OPENING-REVIEW-TARGET", 10000, 1000)
		targetDocumentID := f.document.Id
		targetID, _ := coverageOpeningWithDocument(t, f, f.document, true)
		targetReview := coverageReviewOpening(t, f, targetID)
		f.addDocument("Opening review sibling", "OPENING-REVIEW-SIBLING", 12000, 1200)
		siblingID, _ := coverageOpeningWithDocument(t, f, f.document, false)
		siblingReview := coverageReviewOpening(t, f, siblingID)
		baseline, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
		if err != nil || baseline == nil || len(coverageLegacyOpening(t, baseline, targetID).Reviews) != 1 ||
			len(coverageLegacyOpening(t, baseline, siblingID).Reviews) != 1 {
			t.Fatalf("writer-backed two-source legacy baseline=%#v err=%v", baseline, err)
		}
		probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
			stage: "legacy-review", reject: "legacy-review", targetID: targetID, documentID: targetDocumentID}
		probe.mutation = func() {
			mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
				`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, siblingReview.Id)
			mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
				`UPDATE purchase_legacy_reviews_v1 SET review_proto=zeroblob(?) WHERE id=?`, purchaseOpeningAtomicLateCellBytes, targetReview.Id)
			purchaseRootOwnerAssertStoredBLOB(t, tx, "purchase_legacy_reviews_v1", siblingReview.Id, []byte(postingOrg))
			purchaseOpeningAtomicAssertTextOwner(t, tx, "purchase_legacy_reviews_v1", targetReview.Id)
			purchaseRootOwnerAssertLateCellMetadata(t, tx, "purchase_legacy_reviews_v1", "review_proto", targetReview.Id)
		}
		capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
	})

	t.Run("event-body-after-review-history", func(t *testing.T) {
		ctx := context.Background()
		tx, f := purchaseRootOwnerFixture(t)
		defer tx.Rollback()
		f.addDocument("Opening event target", "OPENING-EVENT-TARGET", 10000, 1000)
		targetDocumentID := f.document.Id
		targetID, _ := coverageOpeningWithDocument(t, f, f.document, false)
		targetReview := coverageReviewOpening(t, f, targetID)
		baseline, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
		if err != nil || baseline == nil || len(coverageLegacyOpening(t, baseline, targetID).Reviews) != 1 {
			t.Fatalf("writer-backed opening/event baseline=%#v err=%v", baseline, err)
		}
		probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
			stage: "legacy-event", reject: "legacy-event", targetID: targetID, documentID: targetDocumentID}
		probe.mutation = func() {
			mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
				`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, targetReview.Id)
			mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1",
				`UPDATE purchase_accounting_events_v1 SET result_proto=zeroblob(?) WHERE scope_kind='OPENING_PAYABLE' AND scope_id=?`,
				purchaseOpeningAtomicLateCellBytes, targetID)
			purchaseRootOwnerAssertStoredBLOB(t, tx, "purchase_legacy_reviews_v1", targetReview.Id, []byte(postingOrg))
			purchaseOpeningAtomicAssertTextOwnerByScope(t, tx, "purchase_accounting_events_v1", "scope_id", targetID)
			purchaseOpeningAtomicAssertLateCellMetadataByScope(t, tx, "purchase_accounting_events_v1", "result_proto", "scope_id", targetID)
		}
		capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
	})
}

func purchaseOpeningAtomicAssertTextOwner(t *testing.T, tx *sqlcipher.Transaction, table, id string) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), "SELECT typeof(organisation_id),organisation_id FROM "+table+" WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("missing %s owner row %s", table, id)
	}
	var storageType, owner string
	if err := rows.Scan(&storageType, &owner); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatalf("non-unique %s owner row %s", table, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if storageType != "text" || owner != postingOrg {
		t.Fatalf("%s owner=%q/%q, want target TEXT owner %s", table, storageType, owner, postingOrg)
	}
}

func purchaseOpeningAtomicAssertTextOwnerByScope(t *testing.T, tx *sqlcipher.Transaction, table, key, value string) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), "SELECT typeof(organisation_id),organisation_id FROM "+table+" WHERE "+key+"=?", value)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("missing %s owner row for %s=%s", table, key, value)
	}
	var storageType, owner string
	if err := rows.Scan(&storageType, &owner); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatalf("non-unique %s owner row for %s=%s", table, key, value)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if storageType != "text" || owner != postingOrg {
		t.Fatalf("%s owner for %s=%s is %q/%q, want target TEXT owner %s", table, key, value, storageType, owner, postingOrg)
	}
}

func purchaseOpeningAtomicAssertLateCellMetadataByScope(t *testing.T, tx *sqlcipher.Transaction, table, column, key, value string) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), "SELECT typeof("+column+"),length("+column+") FROM "+table+" WHERE "+key+"=?", value)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("missing %s row for %s=%s", table, key, value)
	}
	var storageType string
	var byteLength int64
	if err := rows.Scan(&storageType, &byteLength); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatalf("non-unique %s row for %s=%s", table, key, value)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if storageType != "blob" || byteLength != purchaseOpeningAtomicLateCellBytes {
		t.Fatalf("late %s.%s metadata=%s/%d, want BLOB/%d", table, column, storageType, byteLength, purchaseOpeningAtomicLateCellBytes)
	}
}

func TestPurchaseLegacyScalarReplayRejectsSiblingOwnerDriftBeforeEventBody(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	f.addDocument("Opening scalar target", "OPENING-SCALAR-TARGET", 10000, 1000)
	targetID, _ := coverageOpeningWithDocument(t, f, f.document, true)
	targetReview := coverageReviewOpening(t, f, targetID)
	f.addDocument("Opening scalar sibling", "OPENING-SCALAR-SIBLING", 12000, 1200)
	siblingID, _ := coverageOpeningWithDocument(t, f, f.document, false)
	siblingReview := coverageReviewOpening(t, f, siblingID)
	baseline := captureCoverage(t, f.repo(tx), ctx)
	if len(coverageLegacyOpening(t, baseline, targetID).Reviews) != 1 || len(coverageLegacyOpening(t, baseline, siblingID).Reviews) != 1 {
		t.Fatal("writer-backed scalar replay baseline lacks both review histories")
	}
	probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
		stage: "legacy-scalar", reject: "legacy-scalar", targetID: targetReview.Id, documentID: f.document.Id}
	probe.mutation = func() {
		mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
			`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, siblingReview.Id)
		mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1",
			`UPDATE purchase_accounting_events_v1 SET result_proto=zeroblob(?) WHERE scope_kind='OPENING_PAYABLE' AND scope_id=?`,
			purchaseOpeningAtomicLateCellBytes, targetID)
		purchaseRootOwnerAssertStoredBLOB(t, tx, "purchase_legacy_reviews_v1", siblingReview.Id, []byte(postingOrg))
		if owner := purchaseBASScalarText(t, tx, `SELECT typeof(organisation_id)||':'||organisation_id FROM purchase_legacy_reviews_v1 WHERE id=?`, targetReview.Id); owner != "text:"+postingOrg {
			t.Fatalf("selected scalar review owner=%q; want unchanged target TEXT", owner)
		}
		purchaseOpeningAtomicAssertLateCellMetadataByScope(t, tx, "purchase_accounting_events_v1", "result_proto", "scope_id", targetID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocation := after.TotalAlloc - probe.beforeRead.TotalAlloc
	if probe.hookCount != 1 || probe.rejects != 1 || capture != nil || !errors.Is(err, purchases.ErrRepository) ||
		allocation >= 2*1024*1024 || probe.laterBodies != 0 {
		t.Fatalf("legacy scalar owner drift hooks/rejects=%d/%d nil=%t err=%v allocation=%d later_bodies=%d",
			probe.hookCount, probe.rejects, capture == nil, err, allocation, probe.laterBodies)
	}
	if !probe.legacyScalarCountGuarded || !reflect.DeepEqual(probe.stateAfterMutation, purchaseReportingRootOwnerState(t, tx, "full-coverage")) ||
		!reflect.DeepEqual(probe.triggersBefore, purchaseRootOwnerTriggerDefinitions(t, tx)) || !tx.Authenticated() {
		t.Fatal("legacy scalar rejection lacks its owner sentinel or changed caller state")
	}
}

func purchaseOpeningAtomicIsPostingReplay(query string) bool {
	lower := strings.ToLower(query)
	return strings.Contains(lower, "/* bas purchase owner body posting replay */") ||
		strings.HasPrefix(purchaseOpeningAtomicCompactSQL(query),
			"selectposting_proto,source_hash,reviewed_source_proto,reviewed_snapshot_hashfrompurchase_postings_v1whereorganisation_id=?anddocument_id=?")
}

func TestPurchaseOpeningRawDependentBodiesRecheckConversionOwner(t *testing.T) {
	for _, tc := range []struct{ stage, table, column string }{
		{"raw-payable", "purchase_opening_payables", "retained_input_proto"},
		{"raw-item", "opening_items", "source_type"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			f.addDocument("Dependent opening target", "DEPENDENT-OPENING-TARGET", 10000, 1000)
			_, firstConversion := coverageOpeningWithDocument(t, f, f.document, true)
			f.addDocument("Dependent opening sibling", "DEPENDENT-OPENING-SIBLING", 12000, 1200)
			secondID, secondConversion := coverageOpeningWithDocument(t, f, f.document, false)
			baseline := captureCoverage(t, f.repo(tx), ctx)
			if countCoverageRows(coverageRawRows(baseline), tc.table, "id", secondID) != 1 {
				t.Fatal("writer-backed dependent opening body missing from canonical capture")
			}
			probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
				stage: "raw-opening", reject: tc.stage, targetID: firstConversion}
			probe.mutation = func() {
				if probe.rawOpeningBodyReads != 1 {
					t.Fatalf("dependent body ran after %d conversion body reads; want one", probe.rawOpeningBodyReads)
				}
				mutatePurchaseBASScalar(t, f, "opening_conversions",
					`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, firstConversion)
				mutatePurchaseBASScalar(t, f, tc.table,
					"UPDATE "+tc.table+" SET "+tc.column+"=zeroblob(?) WHERE id=?", purchaseOpeningAtomicLateCellBytes, secondID)
				purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", firstConversion, []byte(postingOrg))
				purchaseOpeningAtomicAssertTextOwner(t, tx, "opening_conversions", secondConversion)
				purchaseRootOwnerAssertLateCellMetadata(t, tx, tc.table, tc.column, secondID)
			}
			capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
		})
	}
}

func TestPurchaseOpeningClosureLateOwnerDriftMasksSiblingReference(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	f.addDocument("Closure target", "CLOSURE-TARGET", 10000, 1000)
	_, firstConversion := coverageOpeningWithDocument(t, f, f.document, true)
	f.addDocument("Closure sibling", "CLOSURE-SIBLING", 12000, 1200)
	_, secondConversion := coverageOpeningWithDocument(t, f, f.document, false)
	baseline := captureCoverage(t, f.repo(tx), ctx)
	if countCoverageRows(coverageRawRows(baseline), "opening_conversions", "id", firstConversion) != 1 ||
		countCoverageRows(coverageRawRows(baseline), "opening_conversions", "id", secondConversion) != 1 {
		t.Fatal("writer-backed replacement closure baseline omitted a payable root")
	}
	closurePreflights := 0
	probe := &purchaseOpeningAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
		stage: "closure", reject: "closure", targetID: firstConversion}
	// Count only this closure's aggregate query, before the body hook.
	counter := &purchaseOpeningClosurePreflightProbe{CommandSQLExecutor: tx, count: &closurePreflights}
	probe.CommandSQLExecutor = counter
	probe.mutation = func() {
		if closurePreflights != 1 {
			t.Fatalf("closure body follows %d own preflights; want one", closurePreflights)
		}
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, firstConversion)
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET replaced_by_id=zeroblob(?) WHERE id=?`, purchaseOpeningAtomicLateCellBytes, secondConversion)
		purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", firstConversion, []byte(postingOrg))
		purchaseOpeningAtomicAssertTextOwner(t, tx, "opening_conversions", secondConversion)
		purchaseRootOwnerAssertLateCellMetadata(t, tx, "opening_conversions", "replaced_by_id", secondConversion)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchaseOpeningAtomicAssertGuardedBody(t, probe, capture == nil, err, after)
}

type purchaseOpeningClosurePreflightProbe struct {
	app.CommandSQLExecutor
	count *int
}

func (p *purchaseOpeningClosurePreflightProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "total(payload_bytes)") && strings.Contains(purchaseOpeningAtomicCompactSQL(query), "withrecursiveclosure(id)as(") {
		*p.count++
	}
	return p.CommandSQLExecutor.QueryContext(ctx, query, args...)
}
