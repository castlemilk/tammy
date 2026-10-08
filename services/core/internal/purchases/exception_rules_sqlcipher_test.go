//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func lowValueReview(t *testing.T, f *postingFixture) *tammyv1.PurchaseEvidenceInput {
	t.Helper()
	hash, err := hex.DecodeString("11cc08f62cb53e5e896e0af18ad2056f1bf0a572a4cc2b98c342de6ba7398157")
	if err != nil {
		t.Fatal(err)
	}
	review := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
	review.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION
	review.ExceptionReview = &tammyv1.PurchaseTaxInvoiceExceptionReview{
		Kind:                                 tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_LOW_VALUE_ACQUISITION,
		Rule:                                 &tammyv1.PurchaseRetainedSourceRef{SourceId: "019a4210-0000-7000-8000-000000000075", SourceVersion: 1, SourceHash: hash},
		SupportingRecords:                    []*tammyv1.PurchaseRetainedSourceRef{proto.Clone(review.Source).(*tammyv1.PurchaseRetainedSourceRef)},
		TaxInvoiceNotRequiredReviewConfirmed: true, SupportingRecordRequirementsReviewConfirmed: true,
		Reason: "Reviewed the complete transaction and supporting purchase record under the pinned low-value rule",
	}
	return review
}

func retainExceptionRulesIfSupported(t *testing.T, r *purchases.Repository) {
	t.Helper()
	if port, ok := any(r).(interface{ RetainEvidenceRules(context.Context) error }); ok {
		if err := port.RetainEvidenceRules(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPurchaseLowValueExceptionUsesOwnedWholeTransactionBoundary(t *testing.T) {
	for _, test := range []struct {
		name     string
		net, gst int64
		accepted bool
	}{
		{"82.50 total", 7500, 750, true},
		{"82.51 total", 7501, 750, false},
		{"five 20-dollar supplies in one transaction", 10000, 1000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			f.addDocument("Low-value supplier", "LOW-VALUE", test.net, test.gst)
			f.request.Evidence = lowValueReview(t, f)
			if test.net == 10000 {
				f.request.Allocations = nil
				for sequence := uint32(1); sequence <= 5; sequence++ {
					a := proto.Clone(newPostingFixtureAllocation()).(*tammyv1.PurchaseAllocationInput)
					a.Sequence, a.Gross, a.SupplierGst, a.EligibleGst = sequence, postMoney(2200), postMoney(200), postMoney(200)
					f.request.Allocations = append(f.request.Allocations, a)
				}
			}
			repo := f.repo(tx)
			retainExceptionRulesIfSupported(t, repo)
			before := evidenceStateCounts(t, tx)
			result, err := repo.PostReviewed(context.Background(), f.request)
			if test.accepted {
				if err != nil || result == nil {
					t.Fatalf("reviewed owned $75 transaction cannot use its verified rule: %v", err)
				}
				if result.Accounting.AttributedGst.MinorUnits != test.gst || result.Accounting.SuspenseGst.MinorUnits != 0 {
					t.Fatalf("supported exception did not claim exact eligible GST: %v", result.Accounting)
				}
				current, err := repo.Get(context.Background(), postingOrg, f.document.Id)
				if err != nil || !proto.Equal(current, result.Accounting) {
					t.Fatalf("retained exception cannot be read consistently: %v %v", current, err)
				}
			} else {
				if !errors.Is(err, purchases.ErrUnsupportedException) || result != nil {
					t.Fatalf("above-limit transaction accepted: %v %v", result, err)
				}
				for query, count := range before {
					if got := postScalar(t, tx, query); got != count {
						t.Fatalf("unsupported transaction mutated %s", query)
					}
				}
			}
		})
	}
}

func newPostingFixtureAllocation() *tammyv1.PurchaseAllocationInput {
	return &tammyv1.PurchaseAllocationInput{AccountId: postingExpense, AccountVersion: 1, Use: tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS, BasClassification: tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL, CashFlowClassification: tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING, EligibilityReviewConfirmed: true, EvidenceReviewConfirmed: true}
}

func TestPurchaseLowValueExceptionRejectsUnverifiedProvenanceBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*postingFixture)
	}{
		{"stale rule", func(f *postingFixture) { f.request.Evidence.ExceptionReview.Rule.SourceVersion++ }},
		{"wrong rule ID", func(f *postingFixture) { f.request.Evidence.ExceptionReview.Rule.SourceId = f.document.Id }},
		{"wrong rule hash", func(f *postingFixture) { f.request.Evidence.ExceptionReview.Rule.SourceHash[0] ^= 1 }},
		{"specific waiver unsupported", func(f *postingFixture) {
			f.request.Evidence.ExceptionReview.Kind = tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_SPECIFIC_RETAINED_WAIVER
		}},
		{"stale support", func(f *postingFixture) { f.request.Evidence.ExceptionReview.SupportingRecords[0].SourceVersion++ }},
		{"support hash", func(f *postingFixture) { f.request.Evidence.ExceptionReview.SupportingRecords[0].SourceHash[0] ^= 1 }},
		{"absent owned support", func(f *postingFixture) {
			f.request.Evidence.ExceptionReview.SupportingRecords[0].SourceId = "018f0000-0000-7000-8000-000000009991"
		}},
		{"different owned support cannot replace whole acquisition", func(f *postingFixture) {
			request, document := f.request, f.document
			f.addDocument("Supporting supplier", "DIFFERENT-TRANSACTION", 100, 10)
			request.Evidence.Source = proto.Clone(f.request.Evidence.Source).(*tammyv1.PurchaseRetainedSourceRef)
			request.Evidence.ExceptionReview.SupportingRecords = []*tammyv1.PurchaseRetainedSourceRef{proto.Clone(f.request.Evidence.Source).(*tammyv1.PurchaseRetainedSourceRef)}
			f.request, f.document = request, document
		}},
		{"unreviewed additional support", func(f *postingFixture) {
			request, document := f.request, f.document
			f.addDocumentReview("Supporting supplier", "UNREVIEWED-SUPPORT", 100, 10, false)
			request.Evidence.ExceptionReview.SupportingRecords = append(request.Evidence.ExceptionReview.SupportingRecords, &tammyv1.PurchaseRetainedSourceRef{SourceId: f.document.Id, SourceVersion: f.document.Version, SourceHash: append([]byte(nil), f.document.Sha256...)})
			f.request, f.document = request, document
		}},
		{"duplicate support", func(f *postingFixture) {
			f.request.Evidence.ExceptionReview.SupportingRecords = append(f.request.Evidence.ExceptionReview.SupportingRecords, proto.Clone(f.request.Evidence.ExceptionReview.SupportingRecords[0]).(*tammyv1.PurchaseRetainedSourceRef))
		}},
		{"record review absent", func(f *postingFixture) {
			f.request.Evidence.ExceptionReview.SupportingRecordRequirementsReviewConfirmed = false
		}},
		{"waiver review absent", func(f *postingFixture) {
			f.request.Evidence.ExceptionReview.TaxInvoiceNotRequiredReviewConfirmed = false
		}},
		{"original record corrupt", func(f *postingFixture) {
			if _, err := f.executor.ExecContext(context.Background(), `DROP TRIGGER documents_immutable_source`); err != nil {
				f.t.Fatal(err)
			}
			if _, err := f.executor.ExecContext(context.Background(), `DROP TRIGGER documents_review_transition`); err != nil {
				f.t.Fatal(err)
			}
			if _, err := f.executor.ExecContext(context.Background(), `UPDATE documents SET original_bytes=? WHERE id=?`, []byte("changedx purchase "+f.document.Id), f.document.Id); err != nil {
				f.t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			f.addDocument("Low value", "PROVENANCE", 7500, 750)
			f.request.Evidence = lowValueReview(t, f)
			repo := f.repo(tx)
			retainExceptionRulesIfSupported(t, repo)
			test.mutate(f)
			before := evidenceStateCounts(t, tx)
			if result, err := repo.PostReviewed(context.Background(), f.request); err == nil || result != nil {
				t.Fatalf("unverified exception accepted: %v %v", result, err)
			}
			for query, count := range before {
				if postScalar(t, tx, query) != count {
					t.Fatalf("unverified exception mutated %s", query)
				}
			}
		})
	}
}

func TestPurchaseLowValueAmountAloneDoesNotEstablishException(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	f.addDocument("Small purchase", "UNKNOWN-EVIDENCE", 7500, 750)
	f.request.Evidence.Classification, f.request.Evidence.Source, f.request.Evidence.HeldDate = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN, nil, nil
	repo := f.repo(tx)
	retainExceptionRulesIfSupported(t, repo)
	result, err := repo.PostReviewed(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Accounting.AttributedGst.MinorUnits != 0 || result.Accounting.SuspenseGst.MinorUnits != 750 || result.Accounting.ReviewOutcomes.Evidence != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE {
		t.Fatalf("small amount bypassed reviewed evidence exception: %v", result.Accounting)
	}
}

func TestPurchaseLowValueExceptionRuleRowIsExactAndGlobal(t *testing.T) {
	for _, test := range []struct {
		name, column string
		value        any
	}{
		{"organisation owner", "organisation_id", postingOrg},
		{"wrong kind", "bundle_type", "AU_GST"},
		{"wrong version", "version", "changed-v1"},
		{"wrong hash", "semantic_sha256", "0000000000000000000000000000000000000000000000000000000000000000"},
		{"wrong bytes", "rules_proto", []byte("caller supplied rule")},
		{"wrong start", "effective_from", "2000-07-01"},
		{"finite end", "effective_to", "2026-12-31"},
		{"invalid stamp", "retained_at", "not a date"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			f.addDocument("Low value", "RULE-ROW", 7500, 750)
			f.request.Evidence = lowValueReview(t, f)
			repo := f.repo(tx)
			retainExceptionRulesIfSupported(t, repo)
			if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER rule_bundles_immutable_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(context.Background(), `UPDATE rule_bundles SET `+test.column+`=? WHERE id=?`, test.value, f.request.Evidence.ExceptionReview.Rule.SourceId); err != nil {
				t.Fatal(err)
			}
			before := evidenceStateCounts(t, tx)
			if result, err := repo.PostReviewed(context.Background(), f.request); err == nil || result != nil {
				t.Fatalf("changed retained rule accepted: %v %v", result, err)
			}
			for query, count := range before {
				if postScalar(t, tx, query) != count {
					t.Fatalf("changed rule mutated %s", query)
				}
			}
		})
	}
}

func TestPurchaseLowValueEvidenceAppendAndCashPaymentRetainHistoryAcrossRestart(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	f.addDocument("Low value", "EVIDENCE-CASH", 7500, 750)
	review := lowValueReview(t, f)
	f.request.Evidence.Classification, f.request.Evidence.Source, f.request.Evidence.HeldDate = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN, nil, nil
	repo := f.repo(tx)
	retainExceptionRulesIfSupported(t, repo)
	posted, err := repo.PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 4125, postDate(10)))
	if err != nil {
		t.Fatal(err)
	}
	oldEvent := append([]byte(nil), paymentEventBytes(t, tx, 2)...)
	f.now = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	before := evidenceStateCounts(t, tx)
	appended, err := evidenceCommand(t, repo, ctx, evidenceRequest(t, f, 2, review))
	if err != nil {
		t.Fatal(err)
	}
	if appended.Accounting.SuspenseGst.MinorUnits != 375 || appended.Accounting.AttributedGst.MinorUnits != 0 || appended.Accounting.DeferredGst.MinorUnits != 375 || first.Accounting.SuspenseGst.MinorUnits != 375 {
		t.Fatalf("exception evidence automatically released earlier credit: %v", appended.Accounting)
	}
	if postScalar(t, tx, `SELECT count(*) FROM journals`) != before[`SELECT count(*) FROM journals`] || postScalar(t, tx, `SELECT ledger_revision FROM financial_revisions WHERE id=1`) != before[`SELECT ledger_revision FROM financial_revisions WHERE id=1`] || postScalar(t, tx, `SELECT settlement_revision FROM financial_revisions WHERE id=1`) != before[`SELECT settlement_revision FROM financial_revisions WHERE id=1`] {
		t.Fatal("exception append changed ledger or settlement")
	}
	second, err := repo.RecordPayment(ctx, paymentRequest(t, f, 3, 4125, postDate(20)))
	if err != nil {
		t.Fatal(err)
	}
	if second.Accounting.SuspenseGst.MinorUnits != 375 || second.Accounting.AttributedGst.MinorUnits != 375 || second.Accounting.DeferredGst.MinorUnits != 0 || !bytes.Equal(oldEvent, paymentEventBytes(t, tx, 2)) || !proto.Equal(posted.Posting.Evidence, f.request.Evidence) {
		t.Fatalf("new exception changed historical extents: %v", second.Accounting)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	read, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	current, err := f.repo(read).Get(ctx, postingOrg, f.document.Id)
	if err != nil || !proto.Equal(current, second.Accounting) {
		t.Fatalf("restart lost verified exception or historical claims: %v %v", current, err)
	}
}
