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

func TestPurchaseBASScalarGuardsCumulativeAndSentinelCells(t *testing.T) {
	for _, sentinel := range []bool{false, true} {
		name := "cumulative_bytes"
		if sentinel {
			name = "count_sentinel"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			original := f.document.Id
			firstID := strings.Repeat("a", 3*1024*1024)
			secondID := strings.Repeat("b", 3*1024*1024)
			// Raw opaque ID fixtures exercise copies, not domain/body validity.
			mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET id=? WHERE id=?`, firstID, original)
			var secondOriginal string
			if !sentinel {
				f.addDocumentReview("Scalar drift", "SECOND", 0, 0, false)
				secondOriginal = f.document.Id
			}
			probe := &purchaseBASScalarProbe{Transaction: tx}
			repo := f.repo(probe)
			summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
			if err != nil || summary == nil {
				t.Fatalf("reserve scalar copies: %v", err)
			}
			var before runtime.MemStats
			probe.beforeKeyRead = func() error {
				if sentinel {
					f.addDocumentReview("Scalar drift", "SENTINEL", 0, 0, false)
					secondOriginal = f.document.Id
				}
				mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET id=? WHERE id=?`, secondID, secondOriginal)
				runtime.ReadMemStats(&before)
				return nil
			}
			keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			resource, limit := "BAS scalar bytes", summary.ScalarBytes
			if sentinel {
				resource, limit = "BAS scalar keys", summary.ScalarKeyCount
			}
			var bound *purchases.SourceBoundError
			if keys != nil || !errors.As(err, &bound) || bound.Resource != resource || bound.Limit != limit {
				t.Fatalf("rejected scalar cells returned keys=%t bound=%#v error=%v, want %s/%d", keys != nil, bound, err, resource, limit)
			}
			if len(err.Error()) > 1024 || after.TotalAlloc-before.TotalAlloc >= 5*1024*1024 {
				t.Fatalf("rejected second 3 MiB cell reached Go: error=%d bytes allocated=%d", len(err.Error()), after.TotalAlloc-before.TotalAlloc)
			}
			if probe.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("drift read bodies or ended transaction")
			}
		})
	}
}

func TestPurchaseBASScalarWithholdsNumericAndIdentityDriftCells(t *testing.T) {
	cases := []struct {
		name, table, query string
		modern             bool
	}{
		{"huge_id", "documents", `UPDATE documents SET id=?`, false},
		{"document_version", "documents", `UPDATE documents SET version=zeroblob(8388609)`, false},
		{"posting_source_version", "purchase_postings_v1", `UPDATE purchase_postings_v1 SET reviewed_version=zeroblob(8388609)`, true},
		{"allocation_sequence", "purchase_posting_allocations_v1", `UPDATE purchase_posting_allocations_v1 SET sequence=zeroblob(8388609)`, true},
		{"account_version", "purchase_posting_allocations_v1", `UPDATE purchase_posting_allocations_v1 SET account_version=zeroblob(8388609)`, true},
		{"payment_version", "purchase_payments_v1", `UPDATE purchase_payments_v1 SET accounting_version=zeroblob(8388609)`, true},
		{"payment_sequence", "purchase_payment_allocations_v1", `UPDATE purchase_payment_allocations_v1 SET sequence=zeroblob(8388609)`, true},
		{"posting_allocation_sequence", "purchase_payment_allocations_v1", `UPDATE purchase_payment_allocations_v1 SET posting_allocation_sequence=zeroblob(8388609)`, true},
		{"evidence_version", "purchase_tax_evidence_v1", `UPDATE purchase_tax_evidence_v1 SET accounting_version=zeroblob(8388609)`, true},
		{"evidence_source_version", "purchase_tax_evidence_v1", `UPDATE purchase_tax_evidence_v1 SET source_version=zeroblob(8388609) WHERE source_version IS NOT NULL`, true},
		{"event_version", "purchase_accounting_events_v1", `UPDATE purchase_accounting_events_v1 SET accounting_version=zeroblob(8388609) WHERE accounting_version=1`, true},
		{"deferral_version", "purchase_gst_deferrals_v1", `UPDATE purchase_gst_deferrals_v1 SET accounting_version=zeroblob(8388609)`, true},
		{"portion_sequence", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET sequence=zeroblob(8388609)`, true},
		{"portion_allocation_sequence", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET allocation_sequence=zeroblob(8388609)`, true},
		{"portion_due_version", "purchase_gst_deferral_portions_v1", `UPDATE purchase_gst_deferral_portions_v1 SET due_date_source_version=zeroblob(8388609)`, true},
		{"attribution_version", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET accounting_version=zeroblob(8388609)`, true},
		{"attribution_allocation_sequence", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET allocation_sequence=zeroblob(8388609)`, true},
		{"attribution_due_version", "purchase_gst_attributions_v1", `UPDATE purchase_gst_attributions_v1 SET due_date_source_version=zeroblob(8388609)`, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			var f *postingFixture
			if test.modern {
				_, f = purchaseBASScalarModernFixture(t, true)
			} else {
				workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
				tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = tx.Rollback() })
				f = newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			}
			// The fixture's real transaction remains owned by the caller.
			tx := f.executor.(*sqlcipher.Transaction)
			probe := &purchaseBASScalarProbe{Transaction: tx}
			repo := f.repo(probe)
			summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
			if err != nil || summary == nil {
				t.Fatalf("valid baseline: %v", err)
			}
			var before runtime.MemStats
			probe.beforeKeyRead = func() error {
				if test.name == "huge_id" {
					mutatePurchaseBASScalar(t, f, test.table, test.query, strings.Repeat("x", 8*1024*1024+1))
				} else {
					mutatePurchaseBASScalar(t, f, test.table, test.query)
				}
				runtime.ReadMemStats(&before)
				return nil
			}
			keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			if keys != nil || err == nil || len(err.Error()) > 1024 || after.TotalAlloc-before.TotalAlloc >= 2*1024*1024 {
				t.Fatalf("rejected cell copied/expanded: keys=%t error bytes=%d allocated=%d error=%v", keys != nil, lenErrorPurchaseBASScalar(err), after.TotalAlloc-before.TotalAlloc, err)
			}
			if probe.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("drift read bodies or ended transaction")
			}
		})
	}
}

func lenErrorPurchaseBASScalar(err error) int {
	if err == nil {
		return 0
	}
	return len(err.Error())
}
