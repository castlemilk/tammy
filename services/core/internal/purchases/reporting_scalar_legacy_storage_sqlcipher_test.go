//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

type purchaseBASLegacyStorageFixture struct {
	tx                                *sqlcipher.Transaction
	f                                 *postingFixture
	payableID, conversionID, reviewID string
	manual, factJournal               *tammyv1.Journal
}

func newPurchaseBASLegacyStorageFixture(t *testing.T, withTaxFact bool) *purchaseBASLegacyStorageFixture {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	payableID, conversionID := coverageOpeningWithDocument(t, f, f.document, false)
	review := coverageReviewOpening(t, f, payableID)
	manual := seedCoverageLegacyCashPayment(t, f, f.document.Id, "1012")
	fixture := &purchaseBASLegacyStorageFixture{
		tx: tx, f: f, payableID: payableID, conversionID: conversionID,
		reviewID: review.Id, manual: manual,
	}
	if withTaxFact {
		// The receipt must be reachable through its tax fact alone; its journal
		// source ID is deliberately unrelated to the retained document ID.
		unrelatedSourceID, _ := f.id()
		fixture.factJournal = seedCoverageCashReceiptWithTaxFactSource(t, f, f.document.Id, unrelatedSourceID)
	}
	return fixture
}

func TestPurchaseBASScalarRejectsMalformedLegacyStorageBeforeKeyReads(t *testing.T) {
	cases := []struct {
		name, table, query string
		target             string
		withTaxFact        bool
	}{
		{"review_source_version_blob", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET source_version=zeroblob(8388609) WHERE id=?`, "review", false},
		{"review_source_version_real", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET source_version=0.5 WHERE id=?`, "review", false},
		{"review_accounting_version_blob", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET accounting_version=zeroblob(8388609) WHERE id=?`, "review", false},
		{"review_source_hash", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET source_hash=zeroblob(0) WHERE id=?`, "review", false},
		{"review_record_hash", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET record_hash=zeroblob(0) WHERE id=?`, "review", false},
		{"reviewed_snapshot_hash", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET reviewed_snapshot_hash=zeroblob(0) WHERE id=?`, "review", false},
		{"conversion_version_blob", "opening_conversions", `UPDATE opening_conversions SET version=zeroblob(8388609) WHERE id=?`, "conversion", false},
		{"conversion_version_real", "opening_conversions", `UPDATE opening_conversions SET version=1.5 WHERE id=?`, "conversion", false},
		{"conversion_source_hash_text", "opening_conversions", `UPDATE opening_conversions SET source_sha256=upper(source_sha256) WHERE id=?`, "conversion", false},
		{"item_source_revision_blob", "opening_items", `UPDATE opening_items SET source_revision=zeroblob(8388609) WHERE id=?`, "payable", false},
		{"item_source_revision_zero", "opening_items", `UPDATE opening_items SET source_revision=0 WHERE id=?`, "payable", false},
		{"item_source_revision_real", "opening_items", `UPDATE opening_items SET source_revision=1.5 WHERE id=?`, "payable", false},
		{"item_source_hash", "opening_items", `UPDATE opening_items SET source_content_hash=zeroblob(0) WHERE id=?`, "payable", false},
		{"item_partial_source_tuple", "opening_items", `UPDATE opening_items SET source_type=NULL WHERE id=?`, "payable", false},
		{"payable_missing_conversion", "purchase_opening_payables", `UPDATE purchase_opening_payables SET conversion_id='missing-conversion' WHERE id=?`, "payable", false},
		{"orphan_opening_item", "opening_items", `UPDATE opening_items SET conversion_id='missing-conversion' WHERE id=?`, "payable", false},
		{"manual_version_blob", "journals", `UPDATE journals SET version=zeroblob(8388609) WHERE id=?`, "manual", false},
		{"manual_source_revision_zero", "journals", `UPDATE journals SET source_revision=0 WHERE id=?`, "manual", false},
		{"manual_source_kind_unsupported_integer", "journals", `UPDATE journals SET source_kind=1 WHERE id=?`, "manual", false},
		{"manual_source_kind_negative_integer", "journals", `UPDATE journals SET source_kind=-1 WHERE id=?`, "manual", false},
		{"manual_source_kind_blob", "journals", `UPDATE journals SET source_kind=zeroblob(8388609) WHERE id=?`, "manual", false},
		{"manual_line_amount_blob", "journal_lines", `UPDATE journal_lines SET credit_minor=zeroblob(8388609) WHERE id=?`, "cash_line", false},
		{"manual_line_missing_account", "journal_lines", `UPDATE journal_lines SET account_id='missing-account' WHERE id=?`, "cash_line", false},
		{"tax_fact_missing_source_line", "tax_facts", `UPDATE tax_facts SET journal_line_id='missing-line' WHERE journal_line_id IN (SELECT id FROM journal_lines WHERE journal_id=?)`, "fact_journal", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPurchaseBASLegacyStorageFixture(t, test.withTaxFact)
			args := []any{}
			switch test.target {
			case "review":
				args = []any{fixture.reviewID}
			case "conversion":
				args = []any{fixture.conversionID}
			case "payable":
				args = []any{fixture.payableID}
			case "manual":
				args = []any{fixture.manual.Id}
			case "cash_line":
				args = []any{fixture.manual.Lines[1].Id}
			case "fact_journal":
				args = []any{fixture.factJournal.Id}
			default:
				t.Fatalf("unknown legacy mutation target %q", test.target)
			}
			reservation, err := fixture.f.repo(fixture.tx).SummarizeBASKeys(ctx, postingOrg)
			if err != nil || reservation == nil {
				t.Fatalf("reserve valid retained legacy sources: %#v %v", reservation, err)
			}
			mutatePurchaseBASScalar(t, fixture.f, test.table, test.query, args...)
			before := purchaseBASScalarState(t, fixture.tx)
			probe := &purchaseBASScalarProbe{Transaction: fixture.tx}
			repository := fixture.f.repo(probe)
			if got, err := repository.SummarizeBASKeys(ctx, postingOrg); got != nil ||
				!(errors.Is(err, purchases.ErrRepository) || errors.Is(err, purchases.ErrSourceBound)) {
				t.Fatalf("malformed legacy summary=%#v err=%v", got, err)
			}
			if got, err := repository.ReadBASKeys(ctx, postingOrg, *reservation, reservation.ScalarKeyCount, reservation.ScalarBytes); got != nil || err == nil {
				t.Fatalf("malformed legacy key read returned keys=%t err=%v", got != nil, err)
			}
			if probe.keyReads != 0 || probe.bodyReads != 0 || !fixture.tx.Authenticated() ||
				!reflect.DeepEqual(before, purchaseBASScalarState(t, fixture.tx)) {
				t.Fatal("malformed legacy read reached keys/bodies, wrote state, or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASScalarLegacyNumericDriftIsGuardedBeforeDriverCopy(t *testing.T) {
	cases := []struct {
		name, table, query, target string
	}{
		{"review_source_version", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET source_version=zeroblob(8388609) WHERE id=?`, "review"},
		{"opening_conversion_version", "opening_conversions", `UPDATE opening_conversions SET version=zeroblob(8388609) WHERE id=?`, "conversion"},
		{"manual_source_revision", "journals", `UPDATE journals SET source_revision=zeroblob(8388609) WHERE id=?`, "manual"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPurchaseBASLegacyStorageFixture(t, false)
			probe := &purchaseBASScalarProbe{Transaction: fixture.tx}
			repository := fixture.f.repo(probe)
			reservation, err := repository.SummarizeBASKeys(ctx, postingOrg)
			if err != nil || reservation == nil {
				t.Fatalf("reserve valid legacy source before drift: %#v %v", reservation, err)
			}
			var targetID string
			switch test.target {
			case "review":
				targetID = fixture.reviewID
			case "conversion":
				targetID = fixture.conversionID
			case "manual":
				targetID = fixture.manual.Id
			default:
				t.Fatalf("unknown drift target %q", test.target)
			}
			var before runtime.MemStats
			var sourceState map[string]any
			probe.beforeKeyRead = func() error {
				mutatePurchaseBASScalar(t, fixture.f, test.table, test.query, targetID)
				sourceState = purchaseBASScalarState(t, fixture.tx)
				runtime.ReadMemStats(&before)
				return nil
			}
			keys, err := repository.ReadBASKeys(ctx, postingOrg, *reservation, reservation.ScalarKeyCount, reservation.ScalarBytes)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			if keys != nil || err == nil || len(err.Error()) > 1024 || after.TotalAlloc-before.TotalAlloc >= 2*1024*1024 {
				t.Fatalf("legacy drift returned keys=%t error-bytes=%d allocated=%d err=%v", keys != nil, lenErrorPurchaseBASScalar(err), after.TotalAlloc-before.TotalAlloc, err)
			}
			if sourceState == nil || probe.keyReads != 1 || probe.bodyReads != 0 ||
				!fixture.tx.Authenticated() || !reflect.DeepEqual(sourceState, purchaseBASScalarState(t, fixture.tx)) {
				t.Fatal("legacy drift copied key/body cells, changed source state after mutation, or ended caller transaction")
			}
		})
	}
}
