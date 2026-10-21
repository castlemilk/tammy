//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const purchaseModernOwnerUnrelatedUUID = "018f0000-0000-7000-8000-000000009998"

type purchaseModernOwnerFixture struct {
	tx            *sqlcipher.Transaction
	fixture       *postingFixture
	deferralID    string
	attributionID string
}

func newPurchaseModernOwnerFixture(t *testing.T) purchaseModernOwnerFixture {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	f, repository, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferred, err := deferCommand(t, repository, deferRequest(t, f, state, portion))
	if err != nil || deferred == nil || deferred.Deferral == nil || deferred.Accounting == nil {
		t.Fatalf("create writer-backed GST deferral: response=%v err=%v", deferred, err)
	}
	claim := claimRequest(t, f, deferred.Accounting, portion)
	claim.DeferralId = &deferred.Deferral.Id
	attributed, err := attributeCommand(t, repository, claim)
	if err != nil || attributed == nil || len(attributed.Attributions) != 1 {
		t.Fatalf("create writer-backed GST attribution: response=%v err=%v", attributed, err)
	}
	return purchaseModernOwnerFixture{tx: tx, fixture: f, deferralID: deferred.Deferral.Id,
		attributionID: attributed.Attributions[0].Id}
}

// Move the entire writer-created purchase graph and all of its owner parents
// to the existing canonical foreign UUID used by the accepted owner probes.
// This leaves every target scalar family empty before the direct-child test.
func movePurchaseModernOwnerGraphOutOfTarget(t *testing.T, f *postingFixture) {
	movePurchaseModernOwnerOrganisationGraphOutOfTarget(t, f, postingOrg)
}

func movePurchaseModernOwnerOrganisationGraphOutOfTarget(t *testing.T, f *postingFixture, organisationID string) {
	t.Helper()
	const foreignOwner = purchaseScalarRootOwnerForeignUUID
	for _, table := range []string{
		"documents", "accounts", "journals", "purchase_postings_v1", "purchase_posting_allocations_v1",
		"purchase_payments_v1", "purchase_payment_allocations_v1", "purchase_tax_evidence_v1",
		"purchase_accounting_events_v1", "purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1",
		"purchase_gst_attributions_v1",
	} {
		if postScalar(t, f.executor, "SELECT count(*) FROM "+table+" WHERE organisation_id=?", organisationID) == 0 {
			continue
		}
		mutatePurchaseBASScalar(t, f, table, "UPDATE "+table+" SET organisation_id=? WHERE organisation_id=?", foreignOwner, organisationID)
	}
}

func purchaseModernOwnerAssertGraphOwnerPartition(t *testing.T, tx *sqlcipher.Transaction, blobTable string, blobOwner []byte) {
	t.Helper()
	for _, table := range []string{
		"documents", "accounts", "journals", "purchase_postings_v1", "purchase_posting_allocations_v1",
		"purchase_payments_v1", "purchase_payment_allocations_v1", "purchase_tax_evidence_v1",
		"purchase_accounting_events_v1", "purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1",
		"purchase_gst_attributions_v1",
	} {
		rows, err := tx.QueryContext(context.Background(), `SELECT count(*),
			COALESCE(sum(typeof(organisation_id)='text' AND organisation_id=?),0),
			COALESCE(sum(typeof(organisation_id)='text' AND organisation_id=?),0),
			COALESCE(sum(typeof(organisation_id)='blob'),0),
			COALESCE(sum(typeof(organisation_id)='blob' AND organisation_id=CAST(? AS BLOB)),0)
			FROM `+table, postingOrg, purchaseScalarRootOwnerForeignUUID, blobOwner)
		if err != nil {
			t.Fatal(err)
		}
		var total, targetText, foreignText, blobs, exactBlob int64
		if !rows.Next() || rows.Scan(&total, &targetText, &foreignText, &blobs, &exactBlob) != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			t.Fatalf("read %s graph owner partition", table)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		wantBlobs := int64(0)
		if blobTable == table {
			wantBlobs = 1
		}
		if total == 0 || targetText != 0 || blobs != wantBlobs || exactBlob != wantBlobs ||
			foreignText+blobs != total || (blobTable != table && foreignText == 0) {
			t.Fatalf("%s graph owners total/target-text/foreign-text/blob/exact-blob=%d/%d/%d/%d/%d; want all foreign TEXT except one selected child BLOB in %s",
				table, total, targetText, foreignText, blobs, exactBlob, blobTable)
		}
	}
}

func purchaseModernOwnerAssertEmptyScalarReservation(t *testing.T, ctx context.Context, f *postingFixture, tx *sqlcipher.Transaction) *purchases.BASScalarSummary {
	t.Helper()
	summary, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, summary, err)
	keys, err := f.repo(tx).ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("foreign-owner graph scalar keys=%#v err=%v; want a successful empty physical key read", keys, err)
	}
	return summary
}

// Only the selected child's direct owner storage is changed after the real
// graph has moved out of target TEXT scope. All reference cells remain writer
// values and all their parents remain foreign-owner TEXT rows.
func mutatePurchaseModernOwnerFamily(t *testing.T, f *postingFixture, table string, ownerBytes []byte) {
	t.Helper()
	switch table {
	case "purchase_payment_allocations_v1", "purchase_tax_evidence_v1", "purchase_accounting_events_v1",
		"purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1", "purchase_gst_attributions_v1":
		query := "UPDATE " + table + " SET organisation_id=CAST(? AS BLOB) WHERE rowid=(SELECT rowid FROM " + table + " ORDER BY rowid LIMIT 1)"
		mutatePurchaseBASScalar(t, f, table, query, ownerBytes)
	default:
		t.Fatalf("unknown modern owner family %q", table)
	}
}

func purchaseModernOwnerAssertStoredBlob(t *testing.T, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table string, want []byte) {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), `SELECT typeof(organisation_id),length(organisation_id),organisation_id
		FROM `+table+` WHERE typeof(organisation_id)='blob' AND organisation_id=CAST(? AS BLOB)`, want)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var storageType string
	var length int64
	var got []byte
	if !rows.Next() || rows.Scan(&storageType, &length, &got) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique BLOB owner in %s", table)
	}
	if storageType != "blob" || length != int64(len(want)) || !bytes.Equal(got, want) {
		t.Fatalf("stored %s owner=(%q,%d,%x), want exact BLOB (%d bytes)", table, storageType, length, got, len(want))
	}
}

func purchaseModernOwnerAssertForeignTextEdges(t *testing.T, tx *sqlcipher.Transaction, table string) {
	t.Helper()
	var columns []string
	switch table {
	case "purchase_payment_allocations_v1":
		columns = []string{"payment_id", "posting_id"}
	case "purchase_tax_evidence_v1":
		columns = []string{"posting_id", "document_id", "source_document_id"}
	case "purchase_accounting_events_v1":
		columns = []string{"scope_id", "posting_id", "document_id"}
	case "purchase_gst_deferrals_v1":
		columns = []string{"posting_id", "document_id"}
	case "purchase_gst_deferral_portions_v1":
		columns = []string{"deferral_id", "posting_id", "payment_id", "due_date_source_id", "evidence_id"}
	case "purchase_gst_attributions_v1":
		columns = []string{"operation_key", "posting_id", "document_id", "payment_id", "due_date_source_id", "evidence_id", "closes_deferral_id", "journal_id"}
	default:
		t.Fatalf("unknown modern owner family %q", table)
	}
	for _, column := range columns {
		rows, err := tx.QueryContext(context.Background(), `SELECT typeof(`+column+`),`+column+` FROM `+table+`
			WHERE typeof(organisation_id)='blob' AND length(organisation_id)=36 ORDER BY rowid LIMIT 1`)
		if err != nil {
			t.Fatal(err)
		}
		var storageType string
		var value sql.NullString
		if !rows.Next() || rows.Scan(&storageType, &value) != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			t.Fatalf("read %s.%s after owner mutation", table, column)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if (storageType != "text" || !value.Valid) && (storageType != "null" || value.Valid) {
			t.Fatalf("reference edge %s.%s=(%q,%q), want its original TEXT or NULL storage", table, column, storageType, value.String)
		}
	}
	if table == "purchase_accounting_events_v1" {
		var scopeKind, scopeType, scope, postingOwnerType, postingOwner string
		rows, err := tx.QueryContext(context.Background(), `SELECT ev.scope_kind,typeof(ev.scope_id),ev.scope_id,
			typeof(p.organisation_id),p.organisation_id
			FROM purchase_accounting_events_v1 AS ev LEFT JOIN purchase_postings_v1 AS p ON p.id=ev.posting_id
			WHERE typeof(ev.organisation_id)='blob' AND length(ev.organisation_id)=36`)
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() || rows.Scan(&scopeKind, &scopeType, &scope, &postingOwnerType, &postingOwner) != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			t.Fatal("read posting event's foreign TEXT parent")
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if scopeKind != "POSTING" || scopeType != "text" || scope == "" || postingOwnerType != "text" || postingOwner != purchaseScalarRootOwnerForeignUUID {
			t.Fatalf("event scope/parent=(%q,%q,%q)/%q/%q, want POSTING and foreign TEXT parent", scopeKind, scopeType, scope, postingOwnerType, postingOwner)
		}
	}
	if table == "purchase_gst_attributions_v1" {
		var ownerType, owner, scopeKind, scopeType, scope, postingOwnerType, postingOwner string
		rows, err := tx.QueryContext(context.Background(), `SELECT typeof(ev.organisation_id),ev.organisation_id,ev.scope_kind,
			typeof(ev.scope_id),ev.scope_id,typeof(p.organisation_id),p.organisation_id
			FROM purchase_gst_attributions_v1 AS a JOIN purchase_accounting_events_v1 AS ev ON ev.operation_key=a.operation_key
			LEFT JOIN purchase_postings_v1 AS p ON p.id=ev.posting_id
			WHERE typeof(a.organisation_id)='blob' AND length(a.organisation_id)=36`)
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() || rows.Scan(&ownerType, &owner, &scopeKind, &scopeType, &scope, &postingOwnerType, &postingOwner) != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			t.Fatal("read attribution's foreign TEXT event parent")
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if ownerType != "text" || owner != purchaseScalarRootOwnerForeignUUID || scopeKind != "POSTING" ||
			scopeType != "text" || scope == "" || postingOwnerType != "text" || postingOwner != purchaseScalarRootOwnerForeignUUID {
			t.Fatalf("attribution event parent owner/scope/posting=(%q,%q)/%q/%q/%q/%q, want foreign TEXT writer history",
				ownerType, owner, scopeKind, scopeType, scope, postingOwner)
		}
	}
}

func purchaseModernOwnerState(t *testing.T, tx *sqlcipher.Transaction) map[string]any {
	t.Helper()
	state := coverageStateCounts(t, tx)
	state["SELECT total_changes()"] = postScalar(t, tx, `SELECT total_changes()`)
	return state
}

func TestPurchaseReportingModernOwnerRejectsExactBLOBBeforeScalarKeysOrHydration(t *testing.T) {
	cases := []struct{ name, table string }{
		{"payment_allocations", "purchase_payment_allocations_v1"},
		{"evidence", "purchase_tax_evidence_v1"},
		{"events", "purchase_accounting_events_v1"},
		{"deferrals", "purchase_gst_deferrals_v1"},
		{"deferral_portions", "purchase_gst_deferral_portions_v1"},
		{"attributions", "purchase_gst_attributions_v1"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPurchaseModernOwnerFixture(t)
			valid, err := fixture.fixture.repo(fixture.tx).SummarizeBASKeys(ctx, postingOrg)
			if err != nil || valid == nil {
				t.Fatalf("real writer scalar baseline: summary=%#v err=%v", valid, err)
			}
			for _, family := range []purchases.BASScalarKeyKind{
				purchases.BASScalarPaymentAllocation, purchases.BASScalarEvidence, purchases.BASScalarEvent,
				purchases.BASScalarDeferral, purchases.BASScalarDeferralPortion, purchases.BASScalarAttribution,
			} {
				if valid.FamilyCounts[family-1] == 0 {
					t.Fatalf("real writer scalar baseline has no rows for family %d: %#v", family, valid.FamilyCounts)
				}
			}
			validKeys, err := fixture.fixture.repo(fixture.tx).ReadBASKeys(ctx, postingOrg, *valid, valid.ScalarKeyCount, valid.ScalarBytes)
			if err != nil || int64(len(validKeys)) != valid.ScalarKeyCount {
				t.Fatalf("valid TEXT owner scalar keys=%d summary=%d err=%v", len(validKeys), valid.ScalarKeyCount, err)
			}

			movePurchaseModernOwnerGraphOutOfTarget(t, fixture.fixture)
			purchaseModernOwnerAssertGraphOwnerPartition(t, fixture.tx, "", nil)
			empty := purchaseModernOwnerAssertEmptyScalarReservation(t, ctx, fixture.fixture, fixture.tx)

			mutatePurchaseModernOwnerFamily(t, fixture.fixture, test.table, []byte(postingOrg))
			purchaseModernOwnerAssertStoredBlob(t, fixture.tx, test.table, []byte(postingOrg))
			purchaseModernOwnerAssertGraphOwnerPartition(t, fixture.tx, test.table, []byte(postingOrg))
			purchaseModernOwnerAssertForeignTextEdges(t, fixture.tx, test.table)
			before := purchaseModernOwnerState(t, fixture.tx)

			scalarProbe := &purchaseBASScalarProbe{Transaction: fixture.tx}
			repository := fixture.fixture.repo(scalarProbe)
			if got, err := repository.SummarizeBASKeys(ctx, postingOrg); got != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Errorf("same-byte %s owner summary=%#v err=%v; want nil and repository error", test.table, got, err)
			}
			if got, err := repository.ReadBASKeys(ctx, postingOrg, *empty, 0, 0); got != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Errorf("same-byte %s owner keys=%#v err=%v; want nil and repository error", test.table, got, err)
			}
			if scalarProbe.summaryReads != 2 || scalarProbe.keyReads != 0 || scalarProbe.bodyReads != 0 {
				t.Errorf("same-byte %s scalar reads summary/key/body=%d/%d/%d; want two fresh summaries and no key/body read",
					test.table, scalarProbe.summaryReads, scalarProbe.keyReads, scalarProbe.bodyReads)
			}

			preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: fixture.tx}
			preflight, preflightErr := fixture.fixture.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
				ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if preflight != nil || !errors.Is(preflightErr, purchases.ErrRepository) || preflightProbe.bodyReads != 0 {
				t.Errorf("same-byte %s preflight present=%t err=%v body reads=%d; want nil, repository error, no bodies",
					test.table, preflight != nil, preflightErr, preflightProbe.bodyReads)
			}

			coverageProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: fixture.tx}
			coverage, coverageErr := fixture.fixture.repo(coverageProbe).CaptureCoverageForBAS(ctx, postingOrg)
			if coverage != nil || !errors.Is(coverageErr, purchases.ErrRepository) || coverageProbe.bodyReads != 0 {
				t.Errorf("same-byte %s full coverage present=%t err=%v body reads=%d; want nil, repository error, no bodies",
					test.table, coverage != nil, coverageErr, coverageProbe.bodyReads)
			}

			modernProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: fixture.tx}
			modern, modernErr := fixture.fixture.repo(modernProbe).CapturePostingsForReporting(ctx, postingOrg)
			if modern != nil || !errors.Is(modernErr, purchases.ErrRepository) || modernProbe.bodyReads != 0 {
				t.Errorf("same-byte %s modern capture present=%t err=%v body reads=%d; want nil, repository error, no bodies",
					test.table, modern != nil, modernErr, modernProbe.bodyReads)
			}

			if after := purchaseModernOwnerState(t, fixture.tx); !reflect.DeepEqual(before, after) || !fixture.tx.Authenticated() {
				t.Errorf("rejected %s owner reads changed postmutation rows/revisions or ended caller transaction: before=%v after=%v authenticated=%t",
					test.table, before, after, fixture.tx.Authenticated())
			}
		})
	}
}

type purchaseModernOwnerPhysicalKey struct {
	kind     purchases.BASScalarKeyKind
	id       string
	sequence uint64
}

func purchaseModernOwnerExpectedPhysicalKeys(t *testing.T, tx *sqlcipher.Transaction) []purchaseModernOwnerPhysicalKey {
	t.Helper()
	queries := []struct {
		kind  purchases.BASScalarKeyKind
		query string
		pair  bool
	}{
		{purchases.BASScalarPaymentAllocation, `SELECT payment_id,sequence FROM purchase_payment_allocations_v1 WHERE organisation_id=? ORDER BY posting_id,payment_id,sequence`, true},
		{purchases.BASScalarEvidence, `SELECT id,NULL FROM purchase_tax_evidence_v1 WHERE organisation_id=? ORDER BY posting_id,accounting_version,id`, false},
		{purchases.BASScalarEvent, `SELECT operation_key,NULL FROM purchase_accounting_events_v1 WHERE organisation_id=? ORDER BY scope_id,accounting_version,operation_key`, false},
		{purchases.BASScalarDeferral, `SELECT id,NULL FROM purchase_gst_deferrals_v1 WHERE organisation_id=? ORDER BY posting_id,accounting_version,id`, false},
		{purchases.BASScalarDeferralPortion, `SELECT deferral_id,sequence FROM purchase_gst_deferral_portions_v1 WHERE organisation_id=? ORDER BY posting_id,deferral_id,sequence`, true},
		{purchases.BASScalarAttribution, `SELECT id,NULL FROM purchase_gst_attributions_v1 WHERE organisation_id=? ORDER BY posting_id,accounting_version,operation_key,id`, false},
	}
	var expected []purchaseModernOwnerPhysicalKey
	for _, family := range queries {
		rows, err := tx.QueryContext(context.Background(), family.query, postingOrg)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			var sequence sql.NullInt64
			if err := rows.Scan(&id, &sequence); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			var seq uint64
			if family.pair {
				if !sequence.Valid || sequence.Int64 < 1 {
					_ = rows.Close()
					t.Fatalf("invalid physical sequence in family kind %d: %v", family.kind, sequence)
				}
				seq = uint64(sequence.Int64)
			} else if sequence.Valid {
				_ = rows.Close()
				t.Fatalf("unexpected sequence in family kind %d", family.kind)
			}
			expected = append(expected, purchaseModernOwnerPhysicalKey{kind: family.kind, id: id, sequence: seq})
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
	}
	return expected
}

func purchaseModernOwnerActualPhysicalKeys(keys []purchases.BASScalarKey) []purchaseModernOwnerPhysicalKey {
	var actual []purchaseModernOwnerPhysicalKey
	for _, key := range keys {
		switch key.Kind {
		case purchases.BASScalarPaymentAllocation, purchases.BASScalarEvidence, purchases.BASScalarEvent,
			purchases.BASScalarDeferral, purchases.BASScalarDeferralPortion, purchases.BASScalarAttribution:
			var sequence uint64
			if key.Sequence != nil {
				sequence = *key.Sequence
			}
			actual = append(actual, purchaseModernOwnerPhysicalKey{kind: key.Kind, id: key.ID, sequence: sequence})
		}
	}
	sort.Slice(actual, func(i, j int) bool {
		if actual[i].kind != actual[j].kind {
			return actual[i].kind < actual[j].kind
		}
		if actual[i].id != actual[j].id {
			return actual[i].id < actual[j].id
		}
		return actual[i].sequence < actual[j].sequence
	})
	return actual
}

func purchaseModernOwnerRawLocator(key purchaseModernOwnerPhysicalKey) (table, column string) {
	switch key.kind {
	case purchases.BASScalarPaymentAllocation:
		return "purchase_payment_allocations_v1", "payment_id"
	case purchases.BASScalarEvidence:
		return "purchase_tax_evidence_v1", "id"
	case purchases.BASScalarEvent:
		return "purchase_accounting_events_v1", "operation_key"
	case purchases.BASScalarDeferral:
		return "purchase_gst_deferrals_v1", "id"
	case purchases.BASScalarDeferralPortion:
		return "purchase_gst_deferral_portions_v1", "deferral_id"
	case purchases.BASScalarAttribution:
		return "purchase_gst_attributions_v1", "id"
	default:
		return "", ""
	}
}

func assertPurchaseModernOwnerCanonicalRawRows(t *testing.T, capture *purchases.PostingReportingCapture, expected []purchaseModernOwnerPhysicalKey) {
	t.Helper()
	wantCounts := map[string]int{
		"purchase_payment_allocations_v1":   0,
		"purchase_tax_evidence_v1":          0,
		"purchase_accounting_events_v1":     0,
		"purchase_gst_deferrals_v1":         0,
		"purchase_gst_deferral_portions_v1": 0,
		"purchase_gst_attributions_v1":      0,
	}
	for _, key := range expected {
		table, _ := purchaseModernOwnerRawLocator(key)
		wantCounts[table]++
	}
	gotCounts := make(map[string]int, len(wantCounts))
	for _, row := range capture.Rows {
		if _, ok := wantCounts[row.Table]; ok {
			gotCounts[row.Table]++
		}
	}
	if !reflect.DeepEqual(gotCounts, wantCounts) {
		t.Fatalf("captured modern raw family row counts=%v, want direct SQL physical counts %v", gotCounts, wantCounts)
	}
	expectedByTable := make(map[string][]purchaseModernOwnerPhysicalKey, len(wantCounts))
	for _, key := range expected {
		table, _ := purchaseModernOwnerRawLocator(key)
		expectedByTable[table] = append(expectedByTable[table], key)
	}
	actualByTable := make(map[string][]purchaseModernOwnerPhysicalKey, len(wantCounts))
	for _, row := range capture.Rows {
		expectedKeys, ok := expectedByTable[row.Table]
		if !ok {
			continue
		}
		_, column := purchaseModernOwnerRawLocator(expectedKeys[0])
		id, ok := retainedValue(row, column)
		if !ok || id.Kind != "TEXT" {
			t.Fatalf("captured %s raw physical identity=%#v, want TEXT", row.Table, id)
		}
		key := purchaseModernOwnerPhysicalKey{kind: expectedKeys[0].kind, id: id.Text}
		if key.kind == purchases.BASScalarPaymentAllocation || key.kind == purchases.BASScalarDeferralPortion {
			sequence, ok := retainedValue(row, "sequence")
			if !ok || sequence.Kind != "INTEGER" || sequence.Integer < 1 {
				t.Fatalf("captured %s raw sequence=%#v, want a positive INTEGER", row.Table, sequence)
			}
			key.sequence = uint64(sequence.Integer)
		}
		actualByTable[row.Table] = append(actualByTable[row.Table], key)
	}
	for table, want := range expectedByTable {
		if !reflect.DeepEqual(actualByTable[table], want) {
			t.Fatalf("captured %s raw physical row order=%v, want direct SQL order %v", table, actualByTable[table], want)
		}
	}
	for _, key := range expected {
		table, column := purchaseModernOwnerRawLocator(key)
		var matched *purchases.PurchaseReportingRetainedRow
		for index := range capture.Rows {
			row := &capture.Rows[index]
			if row.Table != table {
				continue
			}
			id, ok := retainedValue(*row, column)
			if !ok || id.Kind != "TEXT" || id.Text != key.id {
				continue
			}
			if key.kind == purchases.BASScalarPaymentAllocation || key.kind == purchases.BASScalarDeferralPortion {
				sequence, ok := retainedValue(*row, "sequence")
				if !ok || sequence.Kind != "INTEGER" || uint64(sequence.Integer) != key.sequence {
					continue
				}
			}
			if matched != nil {
				t.Fatalf("capture retained duplicate %s physical identity %q/%d", table, key.id, key.sequence)
			}
			matched = row
		}
		if matched == nil {
			t.Fatalf("capture omitted %s physical identity %q/%d", table, key.id, key.sequence)
		}
		row := *matched
		owner, ok := retainedValue(row, "organisation_id")
		if !ok || owner.Kind != "TEXT" || owner.Text != postingOrg {
			t.Fatalf("captured %s owner for %q = %#v, want exact TEXT organisation", table, key.id, owner)
		}
	}
}

func TestPurchaseReportingModernOwnerCanonicalWriterPreservesKeysRowsChargesAndOrder(t *testing.T) {
	ctx := context.Background()
	fixture := newPurchaseModernOwnerFixture(t)
	repository := fixture.fixture.repo(fixture.tx)
	before := purchaseModernOwnerState(t, fixture.tx)

	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("canonical six-family scalar summary=%#v err=%v", summary, err)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil {
		t.Fatalf("canonical six-family scalar keys: %v", err)
	}
	expected := purchaseModernOwnerExpectedPhysicalKeys(t, fixture.tx)
	expectedScalarKeys := append([]purchaseModernOwnerPhysicalKey(nil), expected...)
	sort.Slice(expectedScalarKeys, func(i, j int) bool {
		if expectedScalarKeys[i].kind != expectedScalarKeys[j].kind {
			return expectedScalarKeys[i].kind < expectedScalarKeys[j].kind
		}
		if expectedScalarKeys[i].id != expectedScalarKeys[j].id {
			return expectedScalarKeys[i].id < expectedScalarKeys[j].id
		}
		return expectedScalarKeys[i].sequence < expectedScalarKeys[j].sequence
	})
	if len(expected) == 0 || !reflect.DeepEqual(purchaseModernOwnerActualPhysicalKeys(keys), expectedScalarKeys) {
		t.Fatalf("six modern family physical keys=%v, want direct SQL identities %v", purchaseModernOwnerActualPhysicalKeys(keys), expectedScalarKeys)
	}
	for _, family := range []purchases.BASScalarKeyKind{
		purchases.BASScalarPaymentAllocation, purchases.BASScalarEvidence, purchases.BASScalarEvent,
		purchases.BASScalarDeferral, purchases.BASScalarDeferralPortion, purchases.BASScalarAttribution,
	} {
		count := int64(0)
		for _, physical := range expected {
			if physical.kind == family {
				count++
			}
		}
		if count == 0 || summary.FamilyCounts[family-1] != count {
			t.Fatalf("family %d scalar count=%d, direct SQL physical count=%d", family, summary.FamilyCounts[family-1], count)
		}
	}
	if int64(len(keys)) != summary.ScalarKeyCount || purchaseBASScalarIndependentBytes(keys) != summary.ScalarBytes {
		t.Fatalf("canonical scalar key charge count/bytes=%d/%d, summary=%d/%d", len(keys), purchaseBASScalarIndependentBytes(keys), summary.ScalarKeyCount, summary.ScalarBytes)
	}
	for index := 1; index < len(keys); index++ {
		previous, current := keys[index-1], keys[index]
		if previous.Kind > current.Kind || (previous.Kind == current.Kind && previous.ID > current.ID) ||
			(previous.Kind == current.Kind && previous.ID == current.ID && previous.Sequence != nil && current.Sequence != nil && *previous.Sequence > *current.Sequence) {
			t.Fatalf("physical BAS keys are not in stable kind/id/sequence order at index %d", index)
		}
	}

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: fixture.tx}
	preflight, err := fixture.fixture.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil || preflightProbe.bodyReads != 0 {
		t.Fatalf("canonical full preflight=%#v err=%v body reads=%d", preflight, err, preflightProbe.bodyReads)
	}
	coverage, err := repository.CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || coverage == nil {
		t.Fatalf("canonical full coverage=%#v err=%v", coverage, err)
	}
	coverageAgain, err := repository.CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || coverageAgain == nil {
		t.Fatalf("repeated canonical full coverage=%#v err=%v", coverageAgain, err)
	}
	modern, err := repository.CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modern == nil {
		t.Fatalf("canonical modern capture=%#v err=%v", modern, err)
	}
	modernAgain, err := repository.CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modernAgain == nil {
		t.Fatalf("repeated canonical modern capture=%#v err=%v", modernAgain, err)
	}

	if !reflect.DeepEqual(coverage.Rows, coverageAgain.Rows) || coverage.PayloadBytes != coverageAgain.PayloadBytes ||
		!bytes.Equal(coverage.Fingerprint, coverageAgain.Fingerprint) {
		t.Fatal("repeated full capture changed retained raw row order/content, payload charge, or fingerprint")
	}
	if !reflect.DeepEqual(modern.Rows, modernAgain.Rows) || modern.PayloadBytes != modernAgain.PayloadBytes ||
		!bytes.Equal(modern.Fingerprint, modernAgain.Fingerprint) {
		t.Fatal("repeated modern capture changed retained raw row order/content, payload charge, or fingerprint")
	}
	if len(coverage.Fingerprint) != 32 || len(modern.Fingerprint) != 32 {
		t.Fatalf("canonical fingerprints have lengths %d/%d, want SHA-256", len(coverage.Fingerprint), len(modern.Fingerprint))
	}
	t.Logf("canonical modern owner oracle: coverage=%x bytes=%d; modern=%x bytes=%d", coverage.Fingerprint, coverage.PayloadBytes, modern.Fingerprint, modern.PayloadBytes)
	if hex.EncodeToString(coverage.Fingerprint) != "bb50e03e16fda492f6dde519f2527f01f511f76af391ea25e416922668f8111b" || coverage.PayloadBytes != 49660 ||
		hex.EncodeToString(modern.Fingerprint) != "01ff014e04b3f2d20a79861c61f0206323b7d0d48866cd912b80b53ba2c11e68" || modern.PayloadBytes != 48306 {
		t.Fatal("canonical full/modern fingerprint or selected-cell charge differs from the pre-implementation oracle")
	}
	coverageRows := coverageRawRows(coverage)
	assertPurchaseModernOwnerCanonicalRawRows(t, coverageRows, expected)
	assertPurchaseModernOwnerCanonicalRawRows(t, modern, expected)
	if countCoverageRows(coverageRows, "purchase_gst_deferrals_v1", "id", fixture.deferralID) != 1 ||
		countCoverageRows(coverageRows, "purchase_gst_attributions_v1", "id", fixture.attributionID) != 1 {
		t.Fatal("writer-created deferral/attribution identity did not appear exactly once in retained raw rows")
	}

	coverageRowBytes := purchaseRootOwnerRowsPayloadBytes(t, coverage.Rows)
	coverageJournalBytes := purchaseRootOwnerJournalPayloadBytes(t, preflight.JournalPayloadBytes)
	if coverage.BasePayloadBytes != coverageRowBytes || coverage.PayloadBytes != coverageRowBytes+coverageJournalBytes ||
		coverage.PayloadBytes != preflight.PayloadBytes {
		t.Fatalf("full raw/parent payload charge=%d/%d/%d, preflight=%d", coverage.BasePayloadBytes,
			coverage.PayloadBytes, coverageRowBytes+coverageJournalBytes, preflight.PayloadBytes)
	}
	modernJournalCharges := make(map[string]int64, len(modern.Journals))
	for _, journal := range modern.Journals {
		if journal == nil || journal.Journal == nil {
			t.Fatal("modern capture omitted a complete accounting journal body")
		}
		modernJournalCharges[journal.Journal.Id] = journal.PayloadBytes
	}
	modernRowsBytes := purchaseRootOwnerRowsPayloadBytes(t, modern.Rows)
	if modern.PayloadBytes != modernRowsBytes+purchaseRootOwnerJournalPayloadBytes(t, modernJournalCharges) {
		t.Fatalf("modern payload charge=%d, independently retained rows/parents=%d/%d", modern.PayloadBytes,
			modernRowsBytes, purchaseRootOwnerJournalPayloadBytes(t, modernJournalCharges))
	}
	if after := purchaseModernOwnerState(t, fixture.tx); !reflect.DeepEqual(before, after) || !fixture.tx.Authenticated() {
		t.Fatalf("canonical readers changed rows/revisions or ended caller transaction: before=%v after=%v authenticated=%t",
			before, after, fixture.tx.Authenticated())
	}
}

func TestPurchaseReportingModernOwnerIgnoresUnrelatedUUIDBlobOwner(t *testing.T) {
	cases := []struct{ name, table string }{
		{"payment_allocations", "purchase_payment_allocations_v1"},
		{"evidence", "purchase_tax_evidence_v1"},
		{"events", "purchase_accounting_events_v1"},
		{"deferrals", "purchase_gst_deferrals_v1"},
		{"deferral_portions", "purchase_gst_deferral_portions_v1"},
		{"attributions", "purchase_gst_attributions_v1"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPurchaseModernOwnerFixture(t)
			valid, err := fixture.fixture.repo(fixture.tx).SummarizeBASKeys(ctx, postingOrg)
			if err != nil || valid == nil || valid.FamilyCounts[purchases.BASScalarPaymentAllocation-1] == 0 ||
				valid.FamilyCounts[purchases.BASScalarEvidence-1] == 0 || valid.FamilyCounts[purchases.BASScalarEvent-1] == 0 ||
				valid.FamilyCounts[purchases.BASScalarDeferral-1] == 0 || valid.FamilyCounts[purchases.BASScalarDeferralPortion-1] == 0 ||
				valid.FamilyCounts[purchases.BASScalarAttribution-1] == 0 {
				t.Fatalf("unrelated BLOB control lacks the valid TEXT scalar families: summary=%#v err=%v", valid, err)
			}
			validKeys, err := fixture.fixture.repo(fixture.tx).ReadBASKeys(ctx, postingOrg, *valid, valid.ScalarKeyCount, valid.ScalarBytes)
			if err != nil || int64(len(validKeys)) != valid.ScalarKeyCount {
				t.Fatalf("unrelated BLOB control valid TEXT keys=%d summary=%d err=%v", len(validKeys), valid.ScalarKeyCount, err)
			}
			movePurchaseModernOwnerGraphOutOfTarget(t, fixture.fixture)
			purchaseModernOwnerAssertGraphOwnerPartition(t, fixture.tx, "", nil)
			purchaseModernOwnerAssertEmptyScalarReservation(t, ctx, fixture.fixture, fixture.tx)
			mutatePurchaseModernOwnerFamily(t, fixture.fixture, test.table, []byte(purchaseModernOwnerUnrelatedUUID))
			purchaseModernOwnerAssertStoredBlob(t, fixture.tx, test.table, []byte(purchaseModernOwnerUnrelatedUUID))
			purchaseModernOwnerAssertGraphOwnerPartition(t, fixture.tx, test.table, []byte(purchaseModernOwnerUnrelatedUUID))
			purchaseModernOwnerAssertForeignTextEdges(t, fixture.tx, test.table)
			before := purchaseModernOwnerState(t, fixture.tx)

			scalarProbe := &purchaseBASScalarProbe{Transaction: fixture.tx}
			repository := fixture.fixture.repo(scalarProbe)
			summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
			if err != nil || summary == nil || summary.RootCount != 0 || summary.ReceiptCount != 0 || summary.ScalarKeyCount != 0 || summary.ScalarBytes != 0 {
				t.Fatalf("unrelated UUID BLOB %s rejected scalar summary=%#v err=%v", test.table, summary, err)
			}
			keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
			if err != nil || int64(len(keys)) != summary.ScalarKeyCount || scalarProbe.bodyReads != 0 {
				t.Fatalf("unrelated UUID BLOB %s keys=%d summary=%d err=%v body reads=%d", test.table, len(keys), summary.ScalarKeyCount, err, scalarProbe.bodyReads)
			}

			preflight, err := fixture.fixture.repo(fixture.tx).PreflightCoverageForBASWithPayloadBudget(
				ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if err != nil || preflight == nil {
				t.Fatalf("unrelated UUID BLOB %s rejected full preflight=%#v err=%v", test.table, preflight, err)
			}
			coverage, err := fixture.fixture.repo(fixture.tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || coverage == nil {
				t.Fatalf("unrelated UUID BLOB %s rejected full capture=%#v err=%v", test.table, coverage, err)
			}
			modern, err := fixture.fixture.repo(fixture.tx).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modern == nil || len(modern.Postings) != 0 || len(modern.Documents) != 0 || len(modern.Rows) != 0 ||
				modern.PayloadBytes != 0 || len(modern.Fingerprint) != 32 {
				t.Fatalf("unrelated UUID BLOB %s rejected modern capture=%#v err=%v", test.table, modern, err)
			}
			if len(coverage.Rows) != 0 || coverage.PayloadBytes != 0 || len(coverage.Fingerprint) != 32 {
				t.Fatalf("unrelated UUID BLOB %s changed empty full capture: rows=%d payload=%d fingerprint=%d",
					test.table, len(coverage.Rows), coverage.PayloadBytes, len(coverage.Fingerprint))
			}
			if after := purchaseModernOwnerState(t, fixture.tx); !reflect.DeepEqual(before, after) || !fixture.tx.Authenticated() {
				t.Fatalf("unrelated UUID BLOB %s reads changed rows/revisions or ended caller transaction", test.table)
			}
		})
	}
}

func TestPurchaseCoverageRejectsExactBLOBCommandAuditOwnerFromRealTLSWriter(t *testing.T) {
	ctx := context.Background()
	server := newCoverageAuditServer(t)
	document := server.reviewedDocument(t)
	request := server.postRequest(t, document)
	posted, err := server.purchases.PostReviewedPurchase(ctx, coverageAuditRequest(request, server.capability))
	if err != nil || posted == nil || posted.Msg == nil || posted.Msg.Posting == nil {
		t.Fatalf("real authenticated purchase command: response=%v err=%v", posted, err)
	}
	tx, err := server.activation.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	repository, err := purchases.NewRepository(tx, server.activation.Now, server.activation.NewID)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := repository.CaptureCoverageForBAS(ctx, server.organisationID)
	if err != nil || baseline == nil || countCoverageRows(coverageRawRows(baseline), "purchase_command_audit_v1", "operation_key", request.CommandContext.IdempotencyKey) != 1 {
		t.Fatalf("real TLS command did not create one capturable immutable command audit row: capture=%v err=%v", baseline != nil, err)
	}

	fixture := &postingFixture{t: t, executor: tx}
	modernBaseline, err := repository.CapturePostingsForReporting(ctx, server.organisationID)
	if err != nil || modernBaseline == nil {
		t.Fatalf("real TLS writer modern baseline: %v", err)
	}
	mutatePurchaseBASScalar(t, fixture, "purchase_command_audit_v1",
		`UPDATE purchase_command_audit_v1 SET organisation_id=CAST(? AS BLOB) WHERE operation_key=?`,
		[]byte(server.organisationID), request.CommandContext.IdempotencyKey)
	purchaseModernOwnerAssertStoredBlob(t, tx, "purchase_command_audit_v1", []byte(server.organisationID))
	before := purchaseAuditRetentionState(t, tx)
	before["SELECT total_changes()"] = postScalar(t, tx, `SELECT total_changes()`)

	preflightProbe := &purchaseCoverageAuditProbe{CommandSQLExecutor: tx}
	preflightRepository := repositoryWithCoverageProbe(t, preflightProbe, server.activation)
	preflight, preflightErr := preflightRepository.PreflightCoverageForBASWithPayloadBudget(
		ctx, server.organisationID, purchaseCoverageDraftPayloadLimit)
	if preflight != nil || !errors.Is(preflightErr, purchases.ErrRepository) || preflightProbe.bodyReads != 0 {
		t.Errorf("same-byte audit BLOB preflight present=%t err=%v body reads=%d; want nil, repository error, no bodies",
			preflight != nil, preflightErr, preflightProbe.bodyReads)
	}

	coverageProbe := &purchaseCoverageAuditProbe{CommandSQLExecutor: tx}
	coverageRepository := repositoryWithCoverageProbe(t, coverageProbe, server.activation)
	coverage, coverageErr := coverageRepository.CaptureCoverageForBAS(ctx, server.organisationID)
	if coverage != nil || !errors.Is(coverageErr, purchases.ErrRepository) || coverageProbe.bodyReads != 0 {
		t.Errorf("same-byte audit BLOB full capture present=%t err=%v body reads=%d; want nil, repository error, no bodies",
			coverage != nil, coverageErr, coverageProbe.bodyReads)
	}
	after := purchaseAuditRetentionState(t, tx)
	after["SELECT total_changes()"] = postScalar(t, tx, `SELECT total_changes()`)
	if !reflect.DeepEqual(before, after) || !tx.Authenticated() {
		t.Errorf("audit owner rejection changed postmutation rows/revisions or ended caller transaction: before=%v after=%v authenticated=%t",
			before, after, tx.Authenticated())
	}
	modernAfter, err := repository.CapturePostingsForReporting(ctx, server.organisationID)
	if err != nil || modernAfter == nil || !reflect.DeepEqual(modernBaseline.Rows, modernAfter.Rows) ||
		modernBaseline.PayloadBytes != modernAfter.PayloadBytes || !bytes.Equal(modernBaseline.Fingerprint, modernAfter.Fingerprint) {
		t.Fatalf("audit owner outside modern scope changed modern capture: %v", err)
	}

	// Audit scope follows retained event operation keys. Once this real writer
	// graph is foreign, its audit row is outside the target coverage batch.
	mutatePurchaseBASScalar(t, fixture, "purchase_command_audit_v1",
		`UPDATE purchase_command_audit_v1 SET organisation_id=? WHERE operation_key=?`,
		server.organisationID, request.CommandContext.IdempotencyKey)
	movePurchaseModernOwnerOrganisationGraphOutOfTarget(t, fixture, server.organisationID)
	outsideBaseline, err := repository.CaptureCoverageForBAS(ctx, server.organisationID)
	if err != nil || outsideBaseline == nil || len(outsideBaseline.Rows) != 0 || outsideBaseline.PayloadBytes != 0 {
		t.Fatalf("empty target baseline with real out-of-batch audit: %v", err)
	}
	mutatePurchaseBASScalar(t, fixture, "purchase_command_audit_v1",
		`UPDATE purchase_command_audit_v1 SET organisation_id=CAST(? AS BLOB) WHERE operation_key=?`,
		[]byte(server.organisationID), request.CommandContext.IdempotencyKey)
	outsideAfter, err := repository.CaptureCoverageForBAS(ctx, server.organisationID)
	if err != nil || outsideAfter == nil || !reflect.DeepEqual(outsideBaseline.Rows, outsideAfter.Rows) ||
		outsideBaseline.PayloadBytes != outsideAfter.PayloadBytes || !bytes.Equal(outsideBaseline.Fingerprint, outsideAfter.Fingerprint) {
		t.Fatalf("real out-of-batch audit BLOB changed target capture: %v", err)
	}
}

func TestPurchaseModernOwnerIgnoresExactBLOBOutOfScopeEvents(t *testing.T) {
	for _, tc := range []struct{ scopeKind, name string }{
		{"DOCUMENT", "document"},
		{"OPENING_PAYABLE", "opening-payable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPurchaseModernOwnerFixture(t)
			f := fixture.fixture
			var scopeID string
			switch tc.scopeKind {
			case "DOCUMENT":
				// A fresh reviewed but unposted document gives this event a scope
				// outside modern posting coverage.
				f.addDocument("Out-of-scope source", "OWNER-SCOPE-DOCUMENT", 0, 0)
				request := legacyRequest(t, f,
					tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
				if _, err := legacyRepo(t, f, f.executor).ResolveLegacy(ctx, request); err != nil {
					t.Fatalf("create DOCUMENT-scoped event: %v", err)
				}
				scopeID = request.Review.Source.GetDocument().DocumentId
			case "OPENING_PAYABLE":
				itemID, _ := coverageOpeningWithDocument(t, f, f.document, false)
				coverageReviewOpening(t, f, itemID)
				scopeID = itemID
			}
			opKey := purchaseBASScalarText(t, f.executor, `SELECT operation_key
				FROM purchase_accounting_events_v1
				WHERE organisation_id=? AND scope_kind=? AND scope_id=?
				ORDER BY operation_key LIMIT 1`, postingOrg, tc.scopeKind, scopeID)

			modernBefore, err := f.repo(fixture.tx).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modernBefore == nil {
				t.Fatalf("modern baseline: capture=%v err=%v", modernBefore != nil, err)
			}
			if countCoverageRows(&purchases.PostingReportingCapture{Rows: modernBefore.Rows},
				"purchase_accounting_events_v1", "operation_key", opKey) != 0 {
				t.Fatalf("modern baseline unexpectedly retained out-of-scope %s event %s", tc.scopeKind, opKey)
			}
			coverageBefore, err := f.repo(fixture.tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || coverageBefore == nil || countCoverageRows(coverageRawRows(coverageBefore),
				"purchase_accounting_events_v1", "operation_key", opKey) != 1 {
				t.Fatalf("full-coverage baseline omitted %s event: capture=%v err=%v", tc.scopeKind, coverageBefore != nil, err)
			}

			mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1",
				`UPDATE purchase_accounting_events_v1 SET organisation_id=CAST(? AS BLOB) WHERE operation_key=?`,
				[]byte(postingOrg), opKey)

			modernAfter, err := f.repo(fixture.tx).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modernAfter == nil ||
				!reflect.DeepEqual(modernBefore.Rows, modernAfter.Rows) ||
				modernBefore.PayloadBytes != modernAfter.PayloadBytes ||
				!bytes.Equal(modernBefore.Fingerprint, modernAfter.Fingerprint) {
				t.Fatalf("exact target BLOB out-of-scope %s event changed modern capture: before=%v after=%v err=%v",
					tc.scopeKind, modernBefore, modernAfter, err)
			}

			preflight, preflightErr := f.repo(fixture.tx).PreflightCoverageForBASWithPayloadBudget(
				ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if preflight != nil || !errors.Is(preflightErr, purchases.ErrRepository) {
				t.Fatalf("full preflight accepted exact target BLOB %s event: result=%v err=%v", tc.scopeKind, preflight, preflightErr)
			}
			coverageAfter, coverageErr := f.repo(fixture.tx).CaptureCoverageForBAS(ctx, postingOrg)
			if coverageAfter != nil || !errors.Is(coverageErr, purchases.ErrRepository) {
				t.Fatalf("full capture accepted exact target BLOB %s event: result=%v err=%v", tc.scopeKind, coverageAfter, coverageErr)
			}
		})
	}
}

func TestPurchaseModernOwnerRejectsExactBLOBPostingEventWithMissingParent(t *testing.T) {
	ctx := context.Background()
	fixture := newPurchaseModernOwnerFixture(t)
	f := fixture.fixture
	opKey := purchaseBASScalarText(t, f.executor, `SELECT operation_key
		FROM purchase_accounting_events_v1
		WHERE organisation_id=? AND scope_kind='POSTING' AND accounting_version=1
		ORDER BY operation_key LIMIT 1`, postingOrg)

	// Keep the row in the POSTING scope, but remove its posting parent. The
	// selected direct-child owner probe must still see/reject the exact BLOB.
	mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1",
		`UPDATE purchase_accounting_events_v1
		SET organisation_id=CAST(? AS BLOB),posting_id=?,scope_id=?
		WHERE operation_key=?`, []byte(postingOrg), purchaseModernOwnerUnrelatedUUID,
		purchaseModernOwnerUnrelatedUUID, opKey)
	if got := postScalar(t, fixture.tx, `SELECT count(*) FROM purchase_postings_v1 WHERE id=?`, purchaseModernOwnerUnrelatedUUID); got != 0 {
		t.Fatalf("missing-parent control unexpectedly has %d posting parents", got)
	}

	modern, modernErr := f.repo(fixture.tx).CapturePostingsForReporting(ctx, postingOrg)
	if modern != nil || !errors.Is(modernErr, purchases.ErrRepository) {
		t.Fatalf("modern capture accepted exact BLOB POSTING event with missing parent: result=%v err=%v", modern, modernErr)
	}
	preflight, preflightErr := f.repo(fixture.tx).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if preflight != nil || !errors.Is(preflightErr, purchases.ErrRepository) {
		t.Fatalf("full preflight accepted exact BLOB POSTING event with missing parent: result=%v err=%v", preflight, preflightErr)
	}
	coverage, coverageErr := f.repo(fixture.tx).CaptureCoverageForBAS(ctx, postingOrg)
	if coverage != nil || !errors.Is(coverageErr, purchases.ErrRepository) {
		t.Fatalf("full capture accepted exact BLOB POSTING event with missing parent: result=%v err=%v", coverage, coverageErr)
	}
}
