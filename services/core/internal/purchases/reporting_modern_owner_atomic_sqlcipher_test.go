//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
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
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

const purchaseModernOwnerAtomicLargeCellBytes = 9 * 1024 * 1024

type purchaseModernOwnerAtomicFamily struct {
	name        string
	table       string
	protoColumn string
	keySQL      string
	keyWhere    string
	keyCount    int
}

var purchaseModernOwnerAtomicFamilies = []purchaseModernOwnerAtomicFamily{
	{
		name: "payment-allocation", table: "purchase_payment_allocations_v1", protoColumn: "allocation_proto",
		keySQL:   `SELECT payment_id,sequence FROM purchase_payment_allocations_v1 WHERE posting_id=? ORDER BY payment_id,sequence LIMIT 1`,
		keyWhere: "payment_id=? AND sequence=?", keyCount: 2,
	},
	{
		name: "evidence", table: "purchase_tax_evidence_v1", protoColumn: "evidence_proto",
		keySQL:   `SELECT id FROM purchase_tax_evidence_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`,
		keyWhere: "id=?", keyCount: 1,
	},
	{
		name: "event", table: "purchase_accounting_events_v1", protoColumn: "result_proto",
		keySQL:   `SELECT operation_key FROM purchase_accounting_events_v1 WHERE posting_id=? AND scope_kind='POSTING' AND accounting_version=1 ORDER BY operation_key LIMIT 1`,
		keyWhere: "operation_key=?", keyCount: 1,
	},
	{
		name: "attribution", table: "purchase_gst_attributions_v1", protoColumn: "attribution_proto",
		keySQL:   `SELECT id FROM purchase_gst_attributions_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`,
		keyWhere: "id=?", keyCount: 1,
	},
	{
		name: "deferral", table: "purchase_gst_deferrals_v1", protoColumn: "deferral_proto",
		keySQL:   `SELECT id FROM purchase_gst_deferrals_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`,
		keyWhere: "id=?", keyCount: 1,
	},
	{
		name: "deferral-portion", table: "purchase_gst_deferral_portions_v1", protoColumn: "portion_proto",
		keySQL:   `SELECT deferral_id,sequence FROM purchase_gst_deferral_portions_v1 WHERE posting_id=? ORDER BY deferral_id,sequence LIMIT 1`,
		keyWhere: "deferral_id=? AND sequence=?", keyCount: 2,
	},
}

func TestPurchaseModernOwnerLateRawFamilyDriftIsAtomic(t *testing.T) {
	tx, f, firstPostingID, secondPostingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
	defer tx.Rollback()
	for _, family := range purchaseModernOwnerAtomicFamilies {
		for _, mode := range []string{"modern-capture", "full-coverage"} {
			t.Run(family.name+"/"+mode, func(t *testing.T) {
				ownerKey := purchaseModernOwnerAtomicKey(t, tx, family, secondPostingID)
				probe := &purchaseModernOwnerAtomicProbe{
					CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode, stage: "raw",
					family: family, ownerKey: ownerKey, firstPostingID: firstPostingID,
				}
				defer purchaseModernOwnerAtomicSetOwner(t, f, family, ownerKey, postingOrg)
				nilResult, err := purchaseModernOwnerAtomicRun(t, f.repo(probe), mode)
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				purchaseModernOwnerAtomicAssertRejection(t, probe, nilResult, err, after)
			})
		}
	}
}

func TestPurchaseModernOwnerLateRawLargeSelectedCellsAreAtomic(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		family    purchaseModernOwnerAtomicFamily
		ownerPost string
		ownerVer  int64
		bodyPost  string
		bodyVer   int64
		sameRow   bool
	}{
		{name: "event-cross-row", family: purchaseModernOwnerAtomicFamilies[2], ownerPost: "second", ownerVer: 1, bodyPost: "first", bodyVer: 2},
		{name: "claim-deferral-cross-row", family: purchaseModernOwnerAtomicFamilies[4], ownerPost: "second", bodyPost: "first"},
		{name: "claim-attribution-same-row", family: purchaseModernOwnerAtomicFamilies[3], ownerPost: "second", sameRow: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx, f, firstPostingID, secondPostingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
			defer tx.Rollback()
			ownerPostingID := secondPostingID
			if scenario.ownerPost == "first" {
				ownerPostingID = firstPostingID
			}
			ownerVersion := scenario.ownerVer
			ownerKey := purchaseModernOwnerAtomicKeyVersion(t, tx, scenario.family, ownerPostingID, ownerVersion)
			bodyPostingID := firstPostingID
			if scenario.bodyPost == "second" {
				bodyPostingID = secondPostingID
			}
			bodyKey := ownerKey
			if !scenario.sameRow {
				bodyKey = purchaseModernOwnerAtomicKeyVersion(t, tx, scenario.family, bodyPostingID, scenario.bodyVer)
			}
			if scenario.sameRow && !reflect.DeepEqual(ownerKey, bodyKey) {
				t.Fatal("same-row fixture selected different direct-owner and proto keys")
			}
			if !scenario.sameRow && reflect.DeepEqual(ownerKey, bodyKey) {
				t.Fatal("cross-row fixture selected the same direct-owner and proto key")
			}
			probe := &purchaseModernOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: "modern-capture", stage: "raw",
				family: scenario.family, ownerKey: ownerKey, bodyKey: bodyKey,
				largeColumn: scenario.family.protoColumn, firstPostingID: firstPostingID,
			}
			nilResult, err := purchaseModernOwnerAtomicRun(t, f.repo(probe), "modern-capture")
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchaseModernOwnerAtomicAssertRejection(t, probe, nilResult, err, after)
		})
	}
}

func TestPurchaseModernOwnerEmptySelectionExactBLOBIsAtomic(t *testing.T) {
	for _, family := range purchaseModernOwnerAtomicFamilies {
		t.Run(family.name, func(t *testing.T) {
			tx, f, _, postingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
			defer tx.Rollback()
			key := purchaseModernOwnerAtomicKey(t, tx, family, postingID)
			purchaseModernOwnerAtomicMoveAllRootsOutOfTextScope(t, f)
			purchaseModernOwnerAtomicSetOwner(t, f, family, key, "foreign-owner")
			purchaseModernOwnerAtomicSetOwnerBlob(t, f, family, key, purchaseScalarRootOwnerForeignUUID)

			modernProbe := &purchaseModernOwnerAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
				mode: "modern-capture", family: family, ownerKey: key}
			modernEmpty, modernErr := purchaseModernOwnerAtomicRun(t, f.repo(modernProbe), "modern-capture")
			if modernEmpty || modernErr != nil {
				t.Fatalf("foreign BLOB modern empty baseline nil=%t err=%q; want empty capture",
					modernEmpty, purchaseModernOwnerAtomicErrorPreview(modernErr))
			}
			coverageProbe := &purchaseModernOwnerAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
				mode: "full-coverage", family: family, ownerKey: key}
			preflight, preflightErr := f.repo(coverageProbe).PreflightCoverageForBASWithPayloadBudget(
				context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
			coverage, coverageErr := f.repo(coverageProbe).CaptureCoverageForBAS(context.Background(), postingOrg)
			if preflightErr != nil || preflight == nil || preflight.RootCount != 0 || coverageErr != nil || coverage == nil || coverage.RootCount != 0 {
				t.Fatalf("foreign BLOB coverage empty baseline preflight=%#v err=%q capture=%#v err=%q",
					preflight, purchaseModernOwnerAtomicErrorPreview(preflightErr), coverage,
					purchaseModernOwnerAtomicErrorPreview(coverageErr))
			}

			purchaseModernOwnerAtomicSetOwnerBlob(t, f, family, key, postingOrg)
			modernProbe = &purchaseModernOwnerAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
				mode: "modern-capture", family: family, ownerKey: key}
			modernState := purchaseReportingRootOwnerState(t, tx, "modern-capture")
			modernTriggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			runtime.GC()
			var modernBefore runtime.MemStats
			runtime.ReadMemStats(&modernBefore)
			modernNil, modernErr := purchaseModernOwnerAtomicRun(t, f.repo(modernProbe), "modern-capture")
			var modernAfter runtime.MemStats
			runtime.ReadMemStats(&modernAfter)
			purchaseModernOwnerAtomicAssertStaticRejection(t, modernProbe, modernNil, modernErr,
				modernBefore, modernAfter, modernState, modernTriggers)

			coverageProbe = &purchaseModernOwnerAtomicProbe{CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: "full-coverage"}
			coverageProbe.family, coverageProbe.ownerKey = family, key
			preflightState := purchaseReportingRootOwnerState(t, tx, "full-coverage")
			preflightTriggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			runtime.GC()
			var preflightBefore runtime.MemStats
			runtime.ReadMemStats(&preflightBefore)
			preflight, preflightErr = f.repo(coverageProbe).PreflightCoverageForBASWithPayloadBudget(
				context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
			var preflightAfter runtime.MemStats
			runtime.ReadMemStats(&preflightAfter)
			purchaseModernOwnerAtomicAssertStaticRejection(t, coverageProbe, preflight == nil, preflightErr,
				preflightBefore, preflightAfter, preflightState, preflightTriggers)

			captureState := purchaseReportingRootOwnerState(t, tx, "full-coverage")
			captureTriggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			runtime.GC()
			var captureBefore runtime.MemStats
			runtime.ReadMemStats(&captureBefore)
			coverage, coverageErr = f.repo(coverageProbe).CaptureCoverageForBAS(context.Background(), postingOrg)
			var captureAfter runtime.MemStats
			runtime.ReadMemStats(&captureAfter)
			purchaseModernOwnerAtomicAssertStaticRejection(t, coverageProbe, coverage == nil, coverageErr,
				captureBefore, captureAfter, captureState, captureTriggers)
		})
	}
}

func TestPurchaseModernOwnerLateEmptySelectionIsAtomicAfterAllPreflights(t *testing.T) {
	for _, family := range purchaseModernOwnerAtomicFamilies {
		t.Run(family.name, func(t *testing.T) {
			tx, f, _, postingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
			defer tx.Rollback()
			key := purchaseModernOwnerAtomicKey(t, tx, family, postingID)
			purchaseModernOwnerAtomicMoveAllRootsOutOfTextScope(t, f)
			baseline, err := f.repo(tx).CapturePostingsForReporting(context.Background(), postingOrg)
			if err != nil || baseline == nil || len(baseline.Rows) != 0 || baseline.PayloadBytes != 0 {
				t.Fatalf("empty modern baseline: capture=%#v err=%v", baseline, err)
			}
			probe := &purchaseModernOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: "modern-capture", stage: "raw",
				family: family, ownerKey: key, bodyKey: key, largeColumn: family.protoColumn,
				expectEmptyPreflights: true,
			}
			nilResult, err := purchaseModernOwnerAtomicRun(t, f.repo(probe), "modern-capture")
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchaseModernOwnerAtomicAssertRejection(t, probe, nilResult, err, after)
			if probe.sourcePreflights != 11 {
				t.Fatalf("empty modern preflight statements=%d; want all 11", probe.sourcePreflights)
			}
		})
	}
}

func TestPurchaseModernOwnerReplayFamilyDriftIsAtomicAfterDocumentAndPostingRead(t *testing.T) {
	for _, tc := range []struct {
		name       string
		family     purchaseModernOwnerAtomicFamily
		ownerVer   int64
		stage      string
		occurrence int
	}{
		{name: "event-version-one", family: purchaseModernOwnerAtomicFamilies[2], ownerVer: 1, stage: "event-v1", occurrence: 1},
		{name: "later-payment-event", family: purchaseModernOwnerAtomicFamilies[2], ownerVer: 2, stage: "payment-event", occurrence: 1},
		{name: "payment-allocation", family: purchaseModernOwnerAtomicFamilies[0], stage: "payment-allocation", occurrence: 1},
		{name: "evidence", family: purchaseModernOwnerAtomicFamilies[1], stage: "evidence", occurrence: 1},
		{name: "attribution", family: purchaseModernOwnerAtomicFamilies[3], stage: "attribution", occurrence: 1},
		{name: "deferral", family: purchaseModernOwnerAtomicFamilies[4], stage: "deferral", occurrence: 1},
		{name: "deferral-portions-verified", family: purchaseModernOwnerAtomicFamilies[5], stage: "deferral-portion", occurrence: 1},
		{name: "deferral-portions-verified-claims-amounts", family: purchaseModernOwnerAtomicFamilies[5], stage: "deferral-portion", occurrence: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"modern-capture", "full-coverage"} {
				t.Run(mode, func(t *testing.T) {
					tx, f, ownerPostingID, replayPostingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
					defer tx.Rollback()
					ownerKey := purchaseModernOwnerAtomicKeyVersion(t, tx, tc.family, ownerPostingID, tc.ownerVer)
					replayDocumentID := purchaseModernOwnerAtomicDocumentID(t, tx, replayPostingID)
					probe := &purchaseModernOwnerAtomicProbe{
						CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode, stage: tc.stage,
						family: tc.family, ownerKey: ownerKey, replayPostingID: replayPostingID,
						replayDocumentID: replayDocumentID,
						targetOccurrence: tc.occurrence,
					}
					defer purchaseModernOwnerAtomicSetOwner(t, f, tc.family, ownerKey, postingOrg)
					nilResult, err := purchaseModernOwnerAtomicRun(t, f.repo(probe), mode)
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					purchaseModernOwnerAtomicAssertRejection(t, probe, nilResult, err, after)
					if !probe.targetDocumentReplayRead || !probe.targetPostingReplayRead || probe.replayHookBeforePosting != replayPostingID {
						t.Fatalf("%s hook ran before target document/posting replay or selected posting %q; reads=%d/%d",
							probe.stage, probe.replayHookBeforePosting, probe.documentReplayReads, probe.postingReplayReads)
					}
				})
			}
		})
	}
}

func TestPurchaseModernOwnerReplayLargeSelectedProtosAreAtomicAfterDocumentAndPostingRead(t *testing.T) {
	for _, tc := range []struct {
		name     string
		family   purchaseModernOwnerAtomicFamily
		ownerVer int64
		stage    string
	}{
		{name: "evidence-proto", family: purchaseModernOwnerAtomicFamilies[1], stage: "evidence"},
		{name: "event-result-proto", family: purchaseModernOwnerAtomicFamilies[2], ownerVer: 1, stage: "event-v1"},
		{name: "attribution-proto", family: purchaseModernOwnerAtomicFamilies[3], stage: "attribution"},
		{name: "deferral-proto", family: purchaseModernOwnerAtomicFamilies[4], stage: "deferral"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"modern-capture", "full-coverage"} {
				t.Run(mode, func(t *testing.T) {
					tx, f, ownerPostingID, replayPostingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
					defer tx.Rollback()
					ownerKey := purchaseModernOwnerAtomicKeyVersion(t, tx, tc.family, ownerPostingID, tc.ownerVer)
					bodyKey := purchaseModernOwnerAtomicKeyVersion(t, tx, tc.family, replayPostingID, tc.ownerVer)
					if reflect.DeepEqual(ownerKey, bodyKey) {
						t.Fatal("large replay fixture did not separate sibling owner and selected proto rows")
					}
					probe := &purchaseModernOwnerAtomicProbe{
						CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode, stage: tc.stage,
						family: tc.family, ownerKey: ownerKey, bodyKey: bodyKey,
						largeColumn: tc.family.protoColumn, replayPostingID: replayPostingID,
						replayDocumentID: purchaseModernOwnerAtomicDocumentID(t, tx, replayPostingID),
					}
					nilResult, err := purchaseModernOwnerAtomicRun(t, f.repo(probe), mode)
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					purchaseModernOwnerAtomicAssertRejection(t, probe, nilResult, err, after)
					if !probe.targetDocumentReplayRead || !probe.targetPostingReplayRead || probe.replayHookBeforePosting != replayPostingID {
						t.Fatalf("large %s hook ran before target document/posting replay or selected posting %q; reads=%d/%d",
							probe.stage, probe.replayHookBeforePosting, probe.documentReplayReads, probe.postingReplayReads)
					}
				})
			}
		})
	}
}

func TestPurchaseCoverageOperationKeyLateEventOwnerDriftIsAtomic(t *testing.T) {
	tx, f, _, secondPostingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
	defer tx.Rollback()
	family := purchaseModernOwnerAtomicFamilies[2]
	ownerKey := purchaseModernOwnerAtomicKeyVersion(t, tx, family, secondPostingID, 1)
	probe := &purchaseModernOwnerAtomicProbe{
		CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: "full-coverage", stage: "coverage-operation-keys",
		family: family, ownerKey: ownerKey,
	}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(
		context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchaseModernOwnerAtomicAssertRejection(t, probe, preflight == nil, err, after)
	if probe.bodyQueries != 0 {
		t.Fatalf("coverage operation-key owner rejection attempted %d body queries", probe.bodyQueries)
	}
	purchaseModernOwnerAtomicSetOwner(t, f, family, ownerKey, postingOrg)
}

func purchaseModernOwnerAtomicTwoClaimFixture(t *testing.T) (*sqlcipher.Transaction, *postingFixture, string, string) {
	t.Helper()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	firstPostingID := state.Posting.Id
	deferral, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatalf("create first real purchase deferral: %v", err)
	}
	claim := claimRequest(t, f, deferral.Accounting, portion)
	claim.DeferralId = &deferral.Deferral.Id
	if _, err := attributeCommand(t, repo, claim); err != nil {
		t.Fatalf("create first real purchase attribution: %v", err)
	}
	secondPostingID := purchaseModernOwnerAtomicAppendSecondClaim(t, f)
	if !(firstPostingID < secondPostingID) {
		t.Fatalf("writer fixture posting IDs are not ordered: %q >= %q", firstPostingID, secondPostingID)
	}
	return tx, f, firstPostingID, secondPostingID
}

func purchaseModernOwnerAtomicAppendSecondClaim(t *testing.T, f *postingFixture) string {
	t.Helper()
	ctx := context.Background()
	f.date = purchaseReportingDate(2026, 8, 11)
	f.addDocument("Reporting supplier", "MODERN-OWNER-SECOND", 10000, 1000)
	ready := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
	f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	f.request.Evidence.Source = nil
	f.request.Evidence.HeldDate = nil
	repo := f.repo(f.executor)
	posted, err := repo.PostReviewed(ctx, f.request)
	if err != nil || posted == nil || posted.Posting == nil || posted.Accounting == nil {
		t.Fatalf("create second real purchase posting: response=%#v err=%v", posted, err)
	}
	portion := &tammyv1.PurchaseGstSourcePortion{
		NormalEventDate:                 posted.Posting.InvoiceDate,
		EarliestOtherwiseEligiblePeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(7, 1), PeriodEnd: paymentDate(9, 30)},
		RequiredReturnDueDate:           paymentDate(11, 3),
		OrdinaryLastClaimDate:           &tammyv1.CivilDate{Year: 2030, Month: 11, Day: 3},
	}
	paid, err := repo.RecordPayment(ctx, paymentRequest(t, f, posted.Accounting.AccountingVersion, 5500, paymentDate(9, 30)))
	if err != nil || paid == nil || paid.Payment == nil || paid.Accounting == nil {
		t.Fatalf("create second real purchase payment: response=%#v err=%v", paid, err)
	}
	portion.Source = &tammyv1.PurchaseGstSourcePortion_Payment{Payment: &tammyv1.PurchasePaymentPortionRef{
		PostingId: posted.Posting.Id, PostingHash: append([]byte(nil), posted.Posting.SourceHash...),
		PaymentId: paid.Payment.Id, PaymentHash: append([]byte(nil), paid.Payment.SourceHash...), AllocationSequence: 1,
	}}
	portion.NormalEventDate = paid.Payment.ActualPaymentDate
	ready.HeldDate = paymentDate(10, 1)
	evidenced, err := evidenceCommand(t, repo, ctx, evidenceRequest(t, f, paid.Accounting.AccountingVersion, ready))
	if err != nil || evidenced == nil || evidenced.Evidence == nil || evidenced.Accounting == nil {
		t.Fatalf("create second real purchase evidence history: response=%#v err=%v", evidenced, err)
	}
	portion.EvidenceId = evidenced.Evidence.Id
	portion.EvidenceHeldDate = ready.HeldDate
	portion.DueDateEvidence = claimNotice(t, f)
	deferred, err := deferCommand(t, repo, deferRequest(t, f, evidenced.Accounting, portion))
	if err != nil || deferred == nil || deferred.Deferral == nil || deferred.Accounting == nil {
		t.Fatalf("create second real purchase deferral: response=%#v err=%v", deferred, err)
	}
	claim := claimRequest(t, f, deferred.Accounting, portion)
	claim.DeferralId = &deferred.Deferral.Id
	attributed, err := attributeCommand(t, repo, claim)
	if err != nil || attributed == nil || len(attributed.Attributions) != 1 {
		t.Fatalf("create second real purchase attribution: response=%#v err=%v", attributed, err)
	}
	return posted.Posting.Id
}

func purchaseModernOwnerAtomicKey(t *testing.T, tx *sqlcipher.Transaction, family purchaseModernOwnerAtomicFamily, postingID string) []any {
	t.Helper()
	return purchaseModernOwnerAtomicReadKey(t, tx, family, family.keySQL, postingID)
}

func purchaseModernOwnerAtomicKeyVersion(t *testing.T, tx *sqlcipher.Transaction, family purchaseModernOwnerAtomicFamily, postingID string, version int64) []any {
	t.Helper()
	query := family.keySQL
	if family.name == "event" {
		query = `SELECT operation_key FROM purchase_accounting_events_v1 WHERE posting_id=? AND scope_kind='POSTING' AND accounting_version=? ORDER BY operation_key LIMIT 1`
		rows, err := tx.QueryContext(context.Background(), query, postingID, version)
		if err != nil {
			t.Fatalf("read %s event key for %s version %d: %v", family.name, postingID, version, err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("missing %s event writer row for %s version %d", family.name, postingID, version)
		}
		var operationKey string
		if err := rows.Scan(&operationKey); err != nil || operationKey == "" || rows.Next() || rows.Err() != nil {
			t.Fatalf("read unique %s event key for %s version %d: %v", family.name, postingID, version, err)
		}
		return []any{operationKey}
	}
	return purchaseModernOwnerAtomicReadKey(t, tx, family, query, postingID)
}

func purchaseModernOwnerAtomicReadKey(t *testing.T, tx *sqlcipher.Transaction, family purchaseModernOwnerAtomicFamily, query, postingID string) []any {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), query, postingID)
	if err != nil {
		t.Fatalf("read %s real writer key for %s: %v", family.name, postingID, err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("missing %s real writer row for posting %s", family.name, postingID)
	}
	values := make([]any, family.keyCount)
	destinations := make([]any, family.keyCount)
	for index := range destinations {
		destinations[index] = new(any)
	}
	if err := rows.Scan(destinations...); err != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique %s key for posting %s: %v", family.name, postingID, err)
	}
	for index, destination := range destinations {
		values[index] = *(destination.(*any))
	}
	return values
}

func purchaseModernOwnerAtomicRun(t *testing.T, repo *purchases.Repository, mode string) (bool, error) {
	t.Helper()
	ctx := context.Background()
	switch mode {
	case "modern-capture":
		capture, err := repo.CapturePostingsForReporting(ctx, postingOrg)
		return capture == nil, err
	case "full-coverage":
		capture, err := repo.CaptureCoverageForBAS(ctx, postingOrg)
		return capture == nil, err
	case "coverage-preflight":
		preflight, err := repo.PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
		return preflight == nil, err
	default:
		t.Fatalf("unknown owner-atomic capture mode %q", mode)
		return false, purchases.ErrRepository
	}
}

type purchaseModernOwnerAtomicProbe struct {
	app.CommandSQLExecutor
	t                        *testing.T
	fixture                  *postingFixture
	tx                       *sqlcipher.Transaction
	mode                     string
	stage                    string
	family                   purchaseModernOwnerAtomicFamily
	ownerFamily              *purchaseModernOwnerAtomicFamily
	ownerKey                 []any
	bodyKey                  []any
	largeColumn              string
	firstPostingID           string
	replayPostingID          string
	replayDocumentID         string
	targetOccurrence         int
	matchingOccurrences      int
	hookCount                int
	afterHook                bool
	bodyQueries              int
	laterBodies              int
	documentReplayReads      int
	postingReplayReads       int
	rawPreflightReads        int
	expectEmptyPreflights    bool
	sourcePreflights         int
	targetDocumentReplayRead bool
	targetPostingReplayRead  bool
	replayHookBeforePosting  string
	stateAfterMutation       map[string]any
	triggersBefore           map[string]string
	triggersAfterMutation    map[string]string
	beforeRead               runtime.MemStats
}

func (probe *purchaseModernOwnerAtomicProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	wasAfterHook := probe.afterHook
	if probe.expectEmptyPreflights && strings.Contains(query, "total(payload_bytes)") {
		probe.sourcePreflights++
	}
	isRawPreflight := probe.stage == "raw" && purchaseModernOwnerAtomicFamilyPreflightQuery(query, probe.family)
	isDocumentReplay := probe.stage != "" && purchaseReportingRootOwnerIsDocumentReplayQuery(query)
	isPostingReplay := probe.stage != "" && purchaseModernOwnerAtomicIsPostingReplayQuery(query)
	isReplayStage := purchaseModernOwnerAtomicIsReplayStage(probe.stage)
	matches := probe.stage != "" && probe.matchesHook(query)
	targetReplay := !isReplayStage || purchaseModernOwnerAtomicTailArgsEqual(args, probe.expectedReplayTail()...)
	if matches && targetReplay {
		probe.matchingOccurrences++
	}
	if probe.hookCount == 0 && matches && targetReplay && (probe.targetOccurrence == 0 || probe.matchingOccurrences == probe.targetOccurrence) {
		if isReplayStage {
			if !probe.targetDocumentReplayRead || !probe.targetPostingReplayRead {
				probe.t.Fatalf("%s replay hook ran before successful target document/posting reads: %d/%d",
					probe.family.name, probe.documentReplayReads, probe.postingReplayReads)
			}
			purchaseModernOwnerAtomicAssertTailArgs(probe.t, args, probe.expectedReplayTail()...)
			probe.replayHookBeforePosting = probe.replayPostingID
		}
		if probe.stage == "raw" {
			if probe.rawPreflightReads == 0 {
				probe.t.Fatalf("%s raw body hook ran before its successful own-family preflight", probe.family.name)
			}
			if probe.mode == "full-coverage" && probe.family.name == "event" {
				purchaseModernOwnerAtomicAssertTailArgs(probe.t, args, postingOrg)
			} else {
				purchaseModernOwnerAtomicAssertTailArgs(probe.t, args, postingOrg, postingOrg)
			}
		}
		if probe.stage == "coverage-operation-keys" {
			purchaseModernOwnerAtomicAssertTailArgs(probe.t, args, postingOrg)
		}
		probe.hookCount++
		probe.mutateAndSample()
	}
	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err == nil && rows != nil {
		columns, columnErr := rows.Columns()
		if columnErr != nil {
			probe.t.Fatal(columnErr)
		}
		bodyStatement := false
		for _, column := range columns {
			if purchaseCoverageBodyResultColumn(column) {
				bodyStatement = true
				break
			}
		}
		if bodyStatement {
			if probe.expectEmptyPreflights && probe.bodyQueries == 0 && probe.sourcePreflights != 11 {
				probe.t.Fatalf("empty capture began body reads after %d preflights; want all 11", probe.sourcePreflights)
			}
			probe.bodyQueries++
			if wasAfterHook {
				probe.laterBodies++
			}
		}
		if isRawPreflight {
			probe.rawPreflightReads++
		}
		if isDocumentReplay {
			probe.documentReplayReads++
			if purchaseModernOwnerAtomicTailArgsEqual(args, probe.replayDocumentID, postingOrg) {
				probe.targetDocumentReplayRead = true
			}
		}
		if isPostingReplay {
			probe.postingReplayReads++
			if purchaseModernOwnerAtomicTailArgsEqual(args, postingOrg, probe.replayDocumentID) {
				probe.targetPostingReplayRead = true
			}
		}
	}
	return rows, err
}

func (probe *purchaseModernOwnerAtomicProbe) matchesHook(query string) bool {
	compact := purchaseReportingRootOwnerCompactSQL(query)
	switch probe.stage {
	case "raw":
		return purchaseModernOwnerAtomicFamilyRawQuery(query, probe.family)
	case "coverage-operation-keys":
		if strings.Contains(query, "/* BAS purchase owner body coverage_operation_keys */") {
			return true
		}
		return strings.Contains(compact, "frompurchase_accounting_events_v1ase") &&
			strings.Contains(compact, "selecte.operation_key") && strings.Contains(compact, "wheree.organisation_id=?") &&
			!strings.Contains(compact, "total(payload_bytes)") && !strings.Contains(compact, "result_proto")
	default:
		return purchaseModernOwnerAtomicReplayQuery(query, probe.family, probe.stage, compact)
	}
}

func (probe *purchaseModernOwnerAtomicProbe) expectedReplayTail() []any {
	postingID := probe.replayPostingID
	switch probe.stage {
	case "event-v1", "payment-event":
		return []any{postingOrg, postingID}
	case "payment-allocation":
		paymentID := purchaseModernOwnerAtomicPaymentID(probe.t, probe.tx, postingID)
		return []any{paymentID, postingID, postingOrg}
	case "evidence":
		documentID := purchaseModernOwnerAtomicDocumentID(probe.t, probe.tx, postingID)
		return []any{postingOrg, postingID, documentID}
	case "attribution", "deferral":
		return []any{postingID, postingOrg}
	case "deferral-portion":
		deferralID := purchaseModernOwnerAtomicDeferralID(probe.t, probe.tx, postingID)
		return []any{deferralID, postingID, postingOrg}
	default:
		return nil
	}
}

func purchaseModernOwnerAtomicIsReplayStage(stage string) bool {
	switch stage {
	case "event-v1", "payment-event", "payment-allocation", "evidence", "attribution", "deferral", "deferral-portion":
		return true
	default:
		return false
	}
}

func purchaseModernOwnerAtomicTailArgsEqual(args []any, want ...any) bool {
	return len(args) >= len(want) && reflect.DeepEqual(args[len(args)-len(want):], want)
}

func (probe *purchaseModernOwnerAtomicProbe) ownerMutationFamily() purchaseModernOwnerAtomicFamily {
	if probe.ownerFamily != nil {
		return *probe.ownerFamily
	}
	return probe.family
}

func (probe *purchaseModernOwnerAtomicProbe) mutateAndSample() {
	probe.t.Helper()
	probe.triggersBefore = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
	ownerFamily := probe.ownerMutationFamily()
	purchaseModernOwnerAtomicSetOwnerBlob(probe.t, probe.fixture, ownerFamily, probe.ownerKey, postingOrg)
	if probe.largeColumn != "" {
		purchaseModernOwnerAtomicSetLargeCell(probe.t, probe.fixture, probe.family, probe.bodyKey, probe.largeColumn)
		if ownerFamily.table != probe.family.table || !reflect.DeepEqual(probe.ownerKey, probe.bodyKey) {
			purchaseModernOwnerAtomicAssertOwner(probe.t, probe.tx, probe.family, probe.bodyKey, "text", []byte(postingOrg))
		}
		purchaseModernOwnerAtomicAssertCell(probe.t, probe.tx, probe.family, probe.bodyKey, probe.largeColumn, purchaseModernOwnerAtomicLargeCellBytes)
	}
	purchaseModernOwnerAtomicAssertOwner(probe.t, probe.tx, ownerFamily, probe.ownerKey, "blob", []byte(postingOrg))
	if postScalar(probe.t, probe.tx, `PRAGMA ignore_check_constraints`) != 0 {
		probe.t.Fatal("owner mutation left ignore_check_constraints enabled")
	}
	probe.triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) {
		probe.t.Fatal("owner mutation did not restore exact trigger definitions")
	}
	stateMode := probe.mode
	if stateMode == "coverage-preflight" {
		stateMode = "full-coverage"
	}
	probe.stateAfterMutation = purchaseReportingRootOwnerState(probe.t, probe.tx, stateMode)
	probe.afterHook = true
	runtime.GC()
	runtime.ReadMemStats(&probe.beforeRead)
}

func purchaseModernOwnerAtomicFamilyRawQuery(query string, family purchaseModernOwnerAtomicFamily) bool {
	if strings.Contains(query, "/* BAS purchase owner body "+family.table+" */") {
		return true
	}
	compact := purchaseReportingRootOwnerCompactSQL(query)
	if strings.HasPrefix(compact, "selectcount(") || strings.Contains(compact, "total(payload_bytes)") ||
		strings.Contains(compact, "length(cast(") {
		return false
	}
	return strings.Contains(compact, "from"+family.table+"as") && strings.Contains(compact, family.protoColumn)
}

func purchaseModernOwnerAtomicFamilyPreflightQuery(query string, family purchaseModernOwnerAtomicFamily) bool {
	compact := purchaseReportingRootOwnerCompactSQL(query)
	return strings.Contains(compact, "total(payload_bytes)") && strings.Contains(compact, "from"+family.table)
}

func purchaseModernOwnerAtomicReplayQuery(query string, family purchaseModernOwnerAtomicFamily, stage, compact string) bool {
	if marker := purchaseModernOwnerAtomicReplayMarker(stage); marker != "" && strings.Contains(query, marker) {
		return purchaseModernOwnerAtomicReplayFamily(stage) == family.name
	}
	if !strings.Contains(compact, "from"+family.table) {
		return false
	}
	switch stage {
	case "event-v1":
		return family.name == "event" && strings.Contains(compact, "operation_key,posting_id,document_id,opening_payable_id") &&
			strings.Contains(compact, "result_proto") &&
			strings.Contains(compact, "accounting_version=1") && strings.Contains(compact, "scope_kind='posting'")
	case "payment-event":
		return family.name == "event" && strings.Contains(compact, "operation_key,posting_id,document_id,opening_payable_id") &&
			strings.Contains(compact, "result_proto") &&
			strings.Contains(compact, "accounting_version>1") && strings.Contains(compact, "scope_kind='posting'")
	case "payment-allocation":
		return family.name == "payment-allocation" && strings.Contains(compact, "sequence,posting_allocation_sequence,gross_minor,supplier_gst_minor") &&
			strings.Contains(compact, "allocation_proto") && strings.Contains(compact, "payment_id=?andposting_id=?andorganisation_id=?")
	case "evidence":
		return family.name == "evidence" && strings.Contains(compact, "id,accounting_version,classification,source_document_id") &&
			strings.Contains(compact, "evidence_proto") && strings.Contains(compact, "organisation_id=?andposting_id=?anddocument_id=?")
	case "attribution":
		return family.name == "attribution" && strings.Contains(compact, "operation_key,attribution_proto,source_hash") &&
			strings.Contains(compact, "posting_id=?andorganisation_id=?")
	case "deferral":
		return family.name == "deferral" && strings.Contains(compact, "deferral_proto,source_hash") &&
			strings.Contains(compact, "posting_id=?andorganisation_id=?")
	case "deferral-portion":
		return family.name == "deferral-portion" && strings.Contains(compact, "sequence,deferred_gst_minor") &&
			strings.Contains(compact, "deferral_id=?andposting_id=?andorganisation_id=?")
	default:
		return false
	}
}

func purchaseModernOwnerAtomicReplayMarker(stage string) string {
	label := ""
	switch stage {
	case "event-v1":
		label = "posting event replay"
	case "payment-event":
		label = "payment event replay"
	case "payment-allocation":
		label = "payment allocation replay"
	case "evidence":
		label = "evidence replay"
	case "attribution":
		label = "attribution replay"
	case "deferral":
		label = "deferral replay"
	case "deferral-portion":
		label = "deferral portion replay"
	}
	if label == "" {
		return ""
	}
	return "/* BAS purchase owner body " + label + " */"
}

func purchaseModernOwnerAtomicReplayFamily(stage string) string {
	switch stage {
	case "event-v1", "payment-event":
		return "event"
	case "payment-allocation":
		return "payment-allocation"
	case "evidence":
		return "evidence"
	case "attribution":
		return "attribution"
	case "deferral":
		return "deferral"
	case "deferral-portion":
		return "deferral-portion"
	default:
		return ""
	}
}

func purchaseModernOwnerAtomicIsPostingReplayQuery(query string) bool {
	if strings.Contains(query, "/* BAS purchase owner body posting replay */") {
		return true
	}
	return strings.HasPrefix(purchaseReportingRootOwnerCompactSQL(query),
		"selectposting_proto,source_hash,reviewed_source_proto,reviewed_snapshot_hashfrompurchase_postings_v1whereorganisation_id=?anddocument_id=?")
}

func purchaseModernOwnerAtomicAssertTailArgs(t *testing.T, args []any, want ...any) {
	t.Helper()
	if !purchaseModernOwnerAtomicTailArgsEqual(args, want...) {
		tail := args
		if len(tail) > len(want) {
			tail = tail[len(tail)-len(want):]
		}
		t.Fatalf("query arguments tail=%#v; want %#v (all args=%#v)", tail, want, args)
	}
}

func purchaseModernOwnerAtomicAssertRejection(t *testing.T, probe *purchaseModernOwnerAtomicProbe, nilResult bool, err error, after runtime.MemStats) {
	t.Helper()
	allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc
	errorBytes := 0
	if err != nil {
		errorBytes = len(err.Error())
	}
	if probe.hookCount != 1 {
		t.Fatalf("%s %s mutation hook count=%d; want exactly one; nil result=%t err=%q",
			probe.stage, probe.family.name, probe.hookCount, nilResult, purchaseModernOwnerAtomicErrorPreview(err))
	}
	if !nilResult || !errors.Is(err, purchases.ErrRepository) {
		t.Errorf("late %s %s owner drift nil result=%t error_bytes=%d err=%q; want nil result and ErrRepository",
			probe.stage, probe.family.name, nilResult, errorBytes, purchaseModernOwnerAtomicErrorPreview(err))
	}
	if errorBytes >= 512 || allocated >= 2*1024*1024 {
		t.Errorf("late %s %s rejection exceeded allocation/error bounds: error_bytes=%d allocation=%d err=%q",
			probe.stage, probe.family.name, errorBytes, allocated, purchaseModernOwnerAtomicErrorPreview(err))
	}
	if probe.laterBodies != 0 {
		t.Errorf("late %s %s rejection attempted %d later body queries", probe.stage, probe.family.name, probe.laterBodies)
	}
	if probe.stage != "coverage-operation-keys" && probe.bodyQueries == 0 {
		t.Fatalf("late %s %s rejection did not attempt its matched body query", probe.stage, probe.family.name)
	}
	ownerFamily := probe.ownerMutationFamily()
	purchaseModernOwnerAtomicAssertOwner(t, probe.tx, ownerFamily, probe.ownerKey, "blob", []byte(postingOrg))
	if probe.largeColumn != "" {
		if ownerFamily.table != probe.family.table || !reflect.DeepEqual(probe.ownerKey, probe.bodyKey) {
			purchaseModernOwnerAtomicAssertOwner(t, probe.tx, probe.family, probe.bodyKey, "text", []byte(postingOrg))
		}
		purchaseModernOwnerAtomicAssertCell(t, probe.tx, probe.family, probe.bodyKey, probe.largeColumn, purchaseModernOwnerAtomicLargeCellBytes)
	}
	stateMode := probe.mode
	if stateMode == "coverage-preflight" {
		stateMode = "full-coverage"
	}
	if probe.stateAfterMutation == nil || !reflect.DeepEqual(probe.stateAfterMutation, purchaseReportingRootOwnerState(t, probe.tx, stateMode)) {
		t.Fatal("owner rejection changed retained row state or total_changes after the mutation baseline")
	}
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) ||
		!reflect.DeepEqual(probe.triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, probe.tx)) {
		t.Fatal("owner rejection failed to preserve the exact trigger definitions")
	}
	if !probe.tx.Authenticated() || postScalar(t, probe.tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("owner rejection ended the encrypted transaction or left checks disabled")
	}
}

func purchaseModernOwnerAtomicAssertStaticRejection(
	t *testing.T,
	probe *purchaseModernOwnerAtomicProbe,
	nilResult bool,
	err error,
	before runtime.MemStats,
	after runtime.MemStats,
	stateBefore map[string]any,
	triggersBefore map[string]string,
) {
	t.Helper()
	allocated := after.TotalAlloc - before.TotalAlloc
	errorBytes := 0
	if err != nil {
		errorBytes = len(err.Error())
	}
	if !nilResult || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("static %s %s owner drift nil result=%t error_bytes=%d err=%q; want nil result and ErrRepository",
			probe.mode, probe.family.name, nilResult, errorBytes, purchaseModernOwnerAtomicErrorPreview(err))
	}
	if errorBytes >= 512 || allocated >= 2*1024*1024 {
		t.Fatalf("static %s %s rejection exceeded bounded result: error_bytes=%d allocation=%d err=%q",
			probe.mode, probe.family.name, errorBytes, allocated, purchaseModernOwnerAtomicErrorPreview(err))
	}
	if probe.bodyQueries != 0 || probe.laterBodies != 0 {
		t.Fatalf("static %s %s owner rejection attempted body/later queries %d/%d",
			probe.mode, probe.family.name, probe.bodyQueries, probe.laterBodies)
	}
	purchaseModernOwnerAtomicAssertOwner(t, probe.tx, probe.family, probe.ownerKey, "blob", []byte(postingOrg))
	stateMode := probe.mode
	if stateMode == "coverage-preflight" {
		stateMode = "full-coverage"
	}
	if !reflect.DeepEqual(stateBefore, purchaseReportingRootOwnerState(t, probe.tx, stateMode)) {
		t.Fatal("static owner rejection changed retained row state or total_changes")
	}
	if !reflect.DeepEqual(triggersBefore, purchaseRootOwnerTriggerDefinitions(t, probe.tx)) {
		t.Fatal("static owner rejection changed exact trigger definitions")
	}
	if !probe.tx.Authenticated() || postScalar(t, probe.tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("static owner rejection ended the encrypted transaction or left checks disabled")
	}
}

func purchaseModernOwnerAtomicErrorPreview(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 240 {
		value = value[:240]
	}
	return value
}

func purchaseModernOwnerAtomicSetOwner(t *testing.T, f *postingFixture, family purchaseModernOwnerAtomicFamily, key []any, owner string) {
	t.Helper()
	query := "UPDATE " + family.table + " SET organisation_id=? WHERE " + family.keyWhere
	args := append([]any{owner}, key...)
	mutatePurchaseBASScalar(t, f, family.table, query, args...)
}

func purchaseModernOwnerAtomicSetOwnerBlob(t *testing.T, f *postingFixture, family purchaseModernOwnerAtomicFamily, key []any, owner string) {
	t.Helper()
	query := "UPDATE " + family.table + " SET organisation_id=CAST(? AS BLOB) WHERE " + family.keyWhere
	args := append([]any{owner}, key...)
	mutatePurchaseBASScalar(t, f, family.table, query, args...)
}

func purchaseModernOwnerAtomicSetLargeCell(t *testing.T, f *postingFixture, family purchaseModernOwnerAtomicFamily, key []any, column string) {
	t.Helper()
	if column != family.protoColumn {
		t.Fatalf("unexpected %s large cell column %q", family.name, column)
	}
	query := "UPDATE " + family.table + " SET " + column + "=zeroblob(?) WHERE " + family.keyWhere
	args := append([]any{purchaseModernOwnerAtomicLargeCellBytes}, key...)
	mutatePurchaseBASScalar(t, f, family.table, query, args...)
}

func purchaseModernOwnerAtomicAssertOwner(t *testing.T, tx *sqlcipher.Transaction, family purchaseModernOwnerAtomicFamily, key []any, wantType string, want []byte) {
	t.Helper()
	query := "SELECT typeof(organisation_id),length(organisation_id),CAST(organisation_id AS BLOB) FROM " + family.table + " WHERE " + family.keyWhere
	rows, err := tx.QueryContext(context.Background(), query, key...)
	if err != nil {
		t.Fatalf("read %s owner: %v", family.name, err)
	}
	defer rows.Close()
	var storageType string
	var byteLength int64
	var value []byte
	if !rows.Next() || rows.Scan(&storageType, &byteLength, &value) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique %s owner for key %#v", family.name, key)
	}
	if storageType != wantType || byteLength != int64(len(want)) || !bytes.Equal(value, want) {
		t.Fatalf("%s owner=(%q,%d,%x), want (%q,%d,%x)", family.name, storageType, byteLength, value, wantType, len(want), want)
	}
}

func purchaseModernOwnerAtomicAssertCell(t *testing.T, tx *sqlcipher.Transaction, family purchaseModernOwnerAtomicFamily, key []any, column string, length int) {
	t.Helper()
	if column != family.protoColumn {
		t.Fatalf("unexpected %s cell column %q", family.name, column)
	}
	query := "SELECT typeof(" + column + "),length(" + column + ") FROM " + family.table + " WHERE " + family.keyWhere
	rows, err := tx.QueryContext(context.Background(), query, key...)
	if err != nil {
		t.Fatalf("read %s cell metadata: %v", family.name, err)
	}
	defer rows.Close()
	var storageType string
	var gotLength int64
	if !rows.Next() || rows.Scan(&storageType, &gotLength) != nil || rows.Next() || rows.Err() != nil || storageType != "blob" || gotLength != int64(length) {
		t.Fatalf("%s %s metadata=(%q,%d); want (blob,%d)", family.name, column, storageType, gotLength, length)
	}
}

func purchaseModernOwnerAtomicMoveAllRootsOutOfTextScope(t *testing.T, f *postingFixture) {
	t.Helper()
	for _, table := range []string{
		"documents", "journals", "accounts", "purchase_postings_v1", "purchase_posting_allocations_v1", "purchase_payments_v1",
		"purchase_payment_allocations_v1", "purchase_tax_evidence_v1", "purchase_accounting_events_v1",
		"purchase_gst_attributions_v1", "purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1",
	} {
		mutatePurchaseBASScalar(t, f, table,
			"UPDATE "+table+" SET organisation_id=? WHERE organisation_id=?", "foreign-owner", postingOrg)
	}
}

func purchaseModernOwnerAtomicPaymentID(t *testing.T, tx *sqlcipher.Transaction, postingID string) string {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), `SELECT id FROM purchase_payments_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`, postingID)
	if err != nil {
		t.Fatalf("read posting payment id: %v", err)
	}
	defer rows.Close()
	var id string
	if !rows.Next() || rows.Scan(&id) != nil || id == "" || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique payment for posting %s", postingID)
	}
	return id
}

func purchaseModernOwnerAtomicDocumentID(t *testing.T, tx *sqlcipher.Transaction, postingID string) string {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), `SELECT document_id FROM purchase_postings_v1 WHERE id=?`, postingID)
	if err != nil {
		t.Fatalf("read posting document id: %v", err)
	}
	defer rows.Close()
	var id string
	if !rows.Next() || rows.Scan(&id) != nil || id == "" || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique document for posting %s", postingID)
	}
	return id
}

func purchaseModernOwnerAtomicDeferralID(t *testing.T, tx *sqlcipher.Transaction, postingID string) string {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), `SELECT id FROM purchase_gst_deferrals_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`, postingID)
	if err != nil {
		t.Fatalf("read deferral id: %v", err)
	}
	defer rows.Close()
	var id string
	if !rows.Next() || rows.Scan(&id) != nil || id == "" || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique deferral for posting %s", postingID)
	}
	return id
}

func TestPurchaseModernOwnerLateParentFamilyDriftMasksSelectedLargeChildBody(t *testing.T) {
	paymentParent := purchaseModernOwnerAtomicFamily{
		name: "payment-parent", table: "purchase_payments_v1",
		keySQL:   `SELECT id FROM purchase_payments_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`,
		keyWhere: "id=?", keyCount: 1,
	}
	postingAllocationParent := purchaseModernOwnerAtomicFamily{
		name: "posting-allocation-parent", table: "purchase_posting_allocations_v1",
		keySQL:   `SELECT posting_id,sequence FROM purchase_posting_allocations_v1 WHERE posting_id=? ORDER BY sequence LIMIT 1`,
		keyWhere: "posting_id=? AND sequence=?", keyCount: 2,
	}
	evidenceParent := purchaseModernOwnerAtomicFamily{
		name: "evidence-parent", table: "purchase_tax_evidence_v1",
		keySQL:   `SELECT id FROM purchase_tax_evidence_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`,
		keyWhere: "id=?", keyCount: 1,
	}
	eventParent := purchaseModernOwnerAtomicFamily{
		name: "posting-event-parent", table: "purchase_accounting_events_v1",
		keySQL:   `SELECT operation_key FROM purchase_accounting_events_v1 WHERE posting_id=? AND scope_kind='POSTING' AND accounting_version=1 ORDER BY operation_key LIMIT 1`,
		keyWhere: "operation_key=?", keyCount: 1,
	}
	deferralParent := purchaseModernOwnerAtomicFamily{
		name: "deferral-parent", table: "purchase_gst_deferrals_v1",
		keySQL:   `SELECT id FROM purchase_gst_deferrals_v1 WHERE posting_id=? ORDER BY accounting_version,id LIMIT 1`,
		keyWhere: "id=?", keyCount: 1,
	}

	cases := []struct {
		name        string
		bodyFamily  purchaseModernOwnerAtomicFamily
		ownerFamily purchaseModernOwnerAtomicFamily
		stage       string
	}{
		{"raw-payment-allocation-payment-parent", purchaseModernOwnerAtomicFamilies[0], paymentParent, "raw"},
		{"replay-payment-allocation-posting-allocation-parent", purchaseModernOwnerAtomicFamilies[0], postingAllocationParent, "payment-allocation"},
		{"raw-attribution-evidence-parent", purchaseModernOwnerAtomicFamilies[3], evidenceParent, "raw"},
		{"replay-attribution-posting-event-parent", purchaseModernOwnerAtomicFamilies[3], eventParent, "attribution"},
		{"raw-deferral-portion-deferral-parent", purchaseModernOwnerAtomicFamilies[5], deferralParent, "raw"},
		{"replay-deferral-evidence-parent", purchaseModernOwnerAtomicFamilies[4], evidenceParent, "deferral"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, f, firstPostingID, secondPostingID := purchaseModernOwnerAtomicTwoClaimFixture(t)
			defer tx.Rollback()

			ownerKey := purchaseModernOwnerAtomicReadKey(t, tx, tc.ownerFamily, tc.ownerFamily.keySQL, firstPostingID)
			bodyKey := purchaseModernOwnerAtomicKey(t, tx, tc.bodyFamily, secondPostingID)
			ownerFamily := tc.ownerFamily
			probe := &purchaseModernOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx,
				mode: "modern-capture", stage: tc.stage,
				family: tc.bodyFamily, ownerFamily: &ownerFamily,
				ownerKey: ownerKey, bodyKey: bodyKey,
				largeColumn:      tc.bodyFamily.protoColumn,
				replayPostingID:  secondPostingID,
				targetOccurrence: 1,
			}
			if purchaseModernOwnerAtomicIsReplayStage(tc.stage) {
				probe.replayDocumentID = purchaseModernOwnerAtomicDocumentID(t, tx, secondPostingID)
			}

			nilCapture, err := purchaseModernOwnerAtomicRun(t, f.repo(probe), "modern-capture")
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchaseModernOwnerAtomicAssertRejection(t, probe, nilCapture, err, after)
			if purchaseModernOwnerAtomicIsReplayStage(tc.stage) &&
				(!probe.targetDocumentReplayRead || !probe.targetPostingReplayRead || probe.replayHookBeforePosting != secondPostingID) {
				t.Fatalf("%s hook did not follow the selected second document/posting reads", tc.stage)
			}
		})
	}
}
