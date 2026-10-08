//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func TestPurchaseEvidenceRepositoryExists(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	if _, ok := any(f.repo(tx)).(interface {
		RecordEvidence(context.Context, *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error)
	}); !ok {
		t.Fatal("purchase evidence command cannot retain a new review without claiming historical GST")
	}
}

type evidenceCommandPort interface {
	RecordEvidence(context.Context, *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error)
}

func evidenceCommand(t *testing.T, r *purchases.Repository, ctx context.Context, request *tammyv1.RecordPurchaseTaxEvidenceRequest) (*tammyv1.RecordPurchaseTaxEvidenceResponse, error) {
	t.Helper()
	port, ok := any(r).(evidenceCommandPort)
	if !ok {
		t.Fatal("missing retained evidence command")
	}
	return port.RecordEvidence(ctx, request)
}
func evidenceRequest(t *testing.T, f *postingFixture, version uint64, review *tammyv1.PurchaseEvidenceInput) *tammyv1.RecordPurchaseTaxEvidenceRequest {
	t.Helper()
	key, _ := f.id()
	request := &tammyv1.RecordPurchaseTaxEvidenceRequest{CommandContext: testkit.CommandContext(key), OrganisationId: postingOrg, DocumentId: f.document.Id, ExpectedAccountingVersion: version, Evidence: proto.Clone(review).(*tammyv1.PurchaseEvidenceInput)}
	if _, err := f.executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?,?,'tammy.v1.PurchaseService.RecordPurchaseTaxEvidence',?,'v1','tammy.v1.RecordPurchaseTaxEvidenceRequest',?,'ELECTED',?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, key, make([]byte, 32), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return request
}
func evidenceStateCounts(t *testing.T, executor app.CommandSQLExecutor) map[string]int64 {
	t.Helper()
	result := map[string]int64{}
	for _, query := range []string{`SELECT count(*) FROM purchase_tax_evidence_v1`, `SELECT count(*) FROM purchase_accounting_events_v1`, `SELECT count(*) FROM financial_revision_claims`, `SELECT count(*) FROM journals`, `SELECT financial_revision FROM financial_revisions WHERE id=1`, `SELECT ledger_revision FROM financial_revisions WHERE id=1`, `SELECT tax_source_revision FROM financial_revisions WHERE id=1`, `SELECT settlement_revision FROM financial_revisions WHERE id=1`} {
		result[query] = postScalar(t, executor, query)
	}
	return result
}
func TestPurchaseEvidenceAppendChangesOnlyTaxVersionAndSurvivesRestart(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, basis)
			review := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
			f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
			f.request.Evidence.Source = nil
			f.request.Evidence.HeldDate = nil
			repo := f.repo(tx)
			posted, err := repo.PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			first, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 5500, paymentDate(9, 30)))
			if err != nil {
				t.Fatal(err)
			}
			oldResult := append([]byte(nil), paymentEventBytes(t, tx, 2)...)
			before := evidenceStateCounts(t, tx)
			controls := map[string]int64{}
			for _, code := range []string{"5001", "1000", "2000", "1150", "1160", "1170"} {
				controls[code] = paymentBalance(t, tx, code)
			}
			review.HeldDate = paymentDate(9, 1)
			result, err := evidenceCommand(t, repo, ctx, evidenceRequest(t, f, 2, review))
			if err != nil {
				t.Fatal(err)
			}
			if result.Evidence.AccountingVersion != 3 || result.Accounting.AccountingVersion != 3 || !proto.Equal(result.Evidence.Review, review) || result.Evidence.ActorUserId != testkit.ActorUserID || !result.Evidence.RecordedAt.AsTime().Equal(f.now) {
				t.Fatalf("retained evidence lost identity/time: %v", result)
			}
			want := proto.Clone(first.Accounting).(*tammyv1.PurchaseAccounting)
			want.AccountingVersion = 3
			want.ReviewOutcomes.Evidence = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
			want.ReviewOutcomes.Timing = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW
			if !proto.Equal(result.Accounting, want) {
				t.Fatalf("evidence changed purchase extent: %v want %v", result.Accounting, want)
			}
			for code, amount := range controls {
				if got := paymentBalance(t, tx, code); got != amount {
					t.Fatalf("evidence moved %s from %d to %d", code, amount, got)
				}
			}
			after := evidenceStateCounts(t, tx)
			for query, amount := range before {
				delta := int64(0)
				if query == `SELECT count(*) FROM purchase_tax_evidence_v1` || query == `SELECT count(*) FROM purchase_accounting_events_v1` || query == `SELECT count(*) FROM financial_revision_claims` || query == `SELECT financial_revision FROM financial_revisions WHERE id=1` || query == `SELECT tax_source_revision FROM financial_revisions WHERE id=1` {
					delta = 1
				}
				if after[query] != amount+delta {
					t.Fatalf("%s=%d want %d", query, after[query], amount+delta)
				}
			}
			if !bytes.Equal(oldResult, paymentEventBytes(t, tx, 2)) || !proto.Equal(posted.Posting.Evidence, f.request.Evidence) {
				t.Fatal("evidence overwrote a historical source")
			}
			current, err := repo.Get(ctx, postingOrg, f.document.Id)
			if err != nil || !proto.Equal(current, result.Accounting) {
				t.Fatalf("current evidence projection=%v %v", current, err)
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
			readTx, err := reopened.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer readTx.Rollback()
			retained, err := f.repo(readTx).Get(ctx, postingOrg, f.document.Id)
			if err != nil || !proto.Equal(retained, result.Accounting) {
				t.Fatalf("restart evidence=%v %v", retained, err)
			}
		})
	}
}
func TestPurchaseEvidenceInvalidFactsRejectBeforeAnyMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*tammyv1.RecordPurchaseTaxEvidenceRequest)
	}{
		{"stale", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.ExpectedAccountingVersion = 2 }},
		{"overflow", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.ExpectedAccountingVersion = math.MaxUint64 }},
		{"foreign organisation", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) {
			r.OrganisationId = "018f0000-0000-7000-8000-000000009991"
		}},
		{"missing source", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.Evidence.Source = nil }},
		{"stale source", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.Evidence.Source.SourceVersion++ }},
		{"source hash", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.Evidence.Source.SourceHash[0] ^= 1 }},
		{"foreign source", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) {
			r.Evidence.Source.SourceId = "018f0000-0000-7000-8000-000000009991"
		}},
		{"unknown enum", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.Evidence.Classification = 99 }},
		{"missing held", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.Evidence.HeldDate = nil }},
		{"impossible date", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) {
			r.Evidence.HeldDate = &tammyv1.CivilDate{Year: 2026, Month: 2, Day: 30}
		}},
		{"registration unreviewed", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.Evidence.RegistrationReviewConfirmed = false }},
		{"missing actor", func(r *tammyv1.RecordPurchaseTaxEvidenceRequest) { r.CommandContext.Authentication = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			repo := f.repo(tx)
			if _, err := repo.PostReviewed(ctx, f.request); err != nil {
				t.Fatal(err)
			}
			request := evidenceRequest(t, f, 1, f.request.Evidence)
			test.change(request)
			before := evidenceStateCounts(t, tx)
			if result, err := evidenceCommand(t, repo, ctx, request); err == nil || result != nil {
				t.Fatalf("invalid evidence accepted=%v %v", result, err)
			}
			for query, amount := range before {
				if got := postScalar(t, tx, query); got != amount {
					t.Fatalf("invalid evidence mutated %s=%d want %d", query, got, amount)
				}
			}
		})
	}
}
