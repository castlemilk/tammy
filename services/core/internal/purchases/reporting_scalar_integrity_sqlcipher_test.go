//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func purchaseBASScalarModernFixture(t *testing.T, closeDeferral bool) (*sqlcipher.Transaction, *postingFixture) {
	t.Helper()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferral, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	if closeDeferral {
		claim := claimRequest(t, f, deferral.Accounting, portion)
		claim.DeferralId = &deferral.Deferral.Id
		if _, err := attributeCommand(t, repo, claim); err != nil {
			t.Fatal(err)
		}
	}
	return tx, f
}

// Fixture-only constraint/trigger bypass is restored before a reader runs.
// The caller transaction is rolled back; production schema remains unchanged.
func mutatePurchaseBASScalar(t *testing.T, f *postingFixture, table, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	rows, err := f.executor.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='trigger' AND tbl_name=? ORDER BY name`, table)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		t.Fatal(errors.Join(rowsErr, closeErr))
	}
	if _, err := f.executor.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.ExecContext(ctx, `PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := f.executor.ExecContext(ctx, `PRAGMA ignore_check_constraints=OFF`); err != nil {
			t.Error(err)
		}
	}()
	temporarilyDropPurchaseCoverageTriggers(t, f, names, func() {
		result, err := f.executor.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("mutate %s: %v", table, err)
		}
		count, err := result.RowsAffected()
		if err != nil || count == 0 {
			t.Fatalf("mutation affected %d rows: %v", count, err)
		}
	})
}

func TestPurchaseBASScalarRejectsMalformedModernFamiliesBeforeKeyReads(t *testing.T) {
	cases := []struct{ name, table, query string }{
		{"posting_source_version", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET reviewed_version=zeroblob(8388609)`},
		{"posting_linkage_amount", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET gross_minor=zeroblob(8388609)`},
		{"posting_required_journal", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET journal_id=NULL`},
		{"posting_hash", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET original_content_hash=zeroblob(0)`},
		{"posting_snapshot", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET reviewed_snapshot_hash=zeroblob(0)`},
		{"posting_record_hash", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET source_hash=zeroblob(0)`},
		{"posting_parent", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET document_id='missing-document'`},
		{"posting_owner", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET organisation_id='foreign-owner'`},
		{"allocation_sequence", "purchase_posting_allocations_v1", `UPDATE purchase_posting_allocations_v1 SET sequence=zeroblob(8388609)`},
		{"allocation_account_version", "purchase_posting_allocations_v1", `UPDATE purchase_posting_allocations_v1 SET account_version=zeroblob(8388609)`},
		{"allocation_parent", "purchase_posting_allocations_v1", `UPDATE purchase_posting_allocations_v1 SET posting_id='missing-posting'`},
		{"allocation_account", "purchase_posting_allocations_v1", `UPDATE purchase_posting_allocations_v1 SET account_id='missing-account'`},
		{"payment_version", "purchase_payments_v1", `UPDATE purchase_payments_v1 SET accounting_version=zeroblob(8388609)`},
		{"payment_parent", "purchase_payments_v1", `UPDATE purchase_payments_v1 SET posting_id='missing-posting'`},
		{"payment_hash", "purchase_payments_v1", `UPDATE purchase_payments_v1 SET source_hash=zeroblob(0)`},
		{"payment_journal", "purchase_payments_v1", `UPDATE purchase_payments_v1 SET journal_id='missing-journal'`},
		{"payment_allocation_sequence", "purchase_payment_allocations_v1", `UPDATE purchase_payment_allocations_v1 SET sequence=zeroblob(8388609)`},
		{"payment_posting_sequence", "purchase_payment_allocations_v1", `UPDATE purchase_payment_allocations_v1 SET posting_allocation_sequence=zeroblob(8388609)`},
		{"payment_allocation_parent", "purchase_payment_allocations_v1", `UPDATE purchase_payment_allocations_v1 SET payment_id='missing-payment'`},
		{"evidence_version", "purchase_tax_evidence_v1", `UPDATE purchase_tax_evidence_v1 SET accounting_version=zeroblob(8388609)`},
		{"evidence_source_version", "purchase_tax_evidence_v1", `UPDATE purchase_tax_evidence_v1 SET source_version=zeroblob(8388609) WHERE source_version IS NOT NULL`},
		{"evidence_source_presence", "purchase_tax_evidence_v1", `UPDATE purchase_tax_evidence_v1 SET source_hash=NULL WHERE source_document_id IS NOT NULL`},
		{"evidence_parent", "purchase_tax_evidence_v1", `UPDATE purchase_tax_evidence_v1 SET posting_id='missing-posting'`},
		{"event_version", "purchase_accounting_events_v1", `UPDATE purchase_accounting_events_v1 SET accounting_version=zeroblob(8388609) WHERE accounting_version=1`},
		{"event_scope", "purchase_accounting_events_v1", `UPDATE purchase_accounting_events_v1 SET scope_kind='UNKNOWN'`},
		{"event_parent", "purchase_accounting_events_v1", `UPDATE purchase_accounting_events_v1 SET posting_id='missing-posting',scope_id='missing-posting'`},
		{"deferral_version", "purchase_gst_deferrals_v1", `UPDATE purchase_gst_deferrals_v1 SET accounting_version=zeroblob(8388609)`},
		{"deferral_parent", "purchase_gst_deferrals_v1", `UPDATE purchase_gst_deferrals_v1 SET posting_id='missing-posting'`},
		{"portion_sequence", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET sequence=zeroblob(8388609)`},
		{"portion_allocation_sequence", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET allocation_sequence=zeroblob(8388609)`},
		{"portion_due_version", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET due_date_source_version=zeroblob(8388609)`},
		{"portion_parent", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET deferral_id='missing-deferral'`},
		{"portion_evidence", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET evidence_id='missing-evidence'`},
		{"attribution_version", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET accounting_version=zeroblob(8388609)`},
		{"attribution_allocation_sequence", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET allocation_sequence=zeroblob(8388609)`},
		{"attribution_due_version", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET due_date_source_version=zeroblob(8388609)`},
		{"attribution_parent", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET posting_id='missing-posting'`},
		{"attribution_deferral", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET closes_deferral_id='missing-deferral'`},
		{"document_version", "documents", `UPDATE documents SET version=zeroblob(8388609)`},
		{"document_real_version", "documents", `UPDATE documents SET version=1.5`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tx, f := purchaseBASScalarModernFixture(t, true)
			valid, err := f.repo(tx).SummarizeBASKeys(context.Background(), postingOrg)
			if err != nil || valid == nil {
				t.Fatalf("valid baseline: %v", err)
			}
			mutatePurchaseBASScalar(t, f, test.table, test.query)
			before := purchaseBASScalarState(t, tx)
			probe := &purchaseBASScalarProbe{Transaction: tx}
			repo := f.repo(probe)
			if got, err := repo.SummarizeBASKeys(context.Background(), postingOrg); got != nil || !(errors.Is(err, purchases.ErrRepository) || errors.Is(err, purchases.ErrSourceBound)) {
				t.Fatalf("malformed summary=%#v error=%v", got, err)
			}
			if got, err := repo.ReadBASKeys(context.Background(), postingOrg, *valid, valid.ScalarKeyCount, valid.ScalarBytes); got != nil || err == nil {
				t.Fatalf("malformed keys present=%t error=%v", got != nil, err)
			}
			if probe.keyReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
				t.Fatal("corrupt source read keys/bodies, wrote state, or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASScalarRetainsInvalidPortionThroughActualDeferralParent(t *testing.T) {
	tx, f := purchaseBASScalarModernFixture(t, false)
	// The exact deferral ID still reaches an owned parent. Other damaged
	// composite cells must not turn the LEFT JOIN into an effective inner join.
	mutatePurchaseBASScalar(t, f, "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET organisation_id='foreign-owner',posting_id='missing-posting',payment_id='missing-payment',evidence_id='missing-evidence',due_date_source_id='missing-document'`)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	if got, err := f.repo(probe).SummarizeBASKeys(context.Background(), postingOrg); got != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("owned deferral's malformed child disappeared: summary=%#v error=%v", got, err)
	}
	if probe.keyReads != 0 || probe.bodyReads != 0 {
		t.Fatal("invalid portion reached keys or bodies")
	}
}
