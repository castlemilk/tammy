//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

type purchaseBASLegacyDecisionProbe struct {
	*sqlcipher.Transaction
	queries, decisionReads, bodyReads int
	beforeDecisionRead                func() error
	afterDecisionQuery                func()
}

func (p *purchaseBASLegacyDecisionProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	p.queries++
	decision := strings.Contains(query, "/* BAS legacy decision records */")
	if decision {
		p.decisionReads++
		if p.beforeDecisionRead != nil {
			before := p.beforeDecisionRead
			p.beforeDecisionRead = nil
			if err := before(); err != nil {
				return nil, err
			}
		}
	}
	rows, err := p.Transaction.QueryContext(ctx, query, args...)
	if decision && err == nil && p.afterDecisionQuery != nil {
		after := p.afterDecisionQuery
		p.afterDecisionQuery = nil
		after()
	}
	if err != nil || rows == nil || decision {
		return rows, err
	}
	if columns, err := rows.Columns(); err == nil {
		for _, column := range columns {
			if purchaseCoverageBodyResultColumn(column) {
				p.bodyReads++
				break
			}
		}
	}
	return rows, err
}

func purchaseBASLegacyDecisionFixture(t *testing.T) (*sqlcipher.Transaction, *postingFixture, *tammyv1.ResolveLegacyPurchaseReviewRequest, *tammyv1.LegacyPurchaseReview) {
	t.Helper()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	postRequest := f.request
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	response, err := legacyRepo(t, f, tx).ResolveLegacy(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	f.request = postRequest
	return tx, f, request, response.Review
}

// This independent oracle copies only the six charged cells, then checks SQL
// COUNT/SUM against their actual byte lengths and the returned canonical records.
func assertPurchaseBASLegacyDecisionCharge(t *testing.T, tx *sqlcipher.Transaction, got *purchases.BASLegacyDecisionRead) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), `SELECT id,organisation_id,source_kind,
		COALESCE(document_id,opening_payable_id),review_proto,record_hash
		FROM purchase_legacy_reviews_v1 WHERE organisation_id=?
		ORDER BY source_kind,COALESCE(document_id,opening_payable_id),accounting_version,id`, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	var count, total int64
	for rows.Next() {
		var id, org, kind, source string
		var content, hash []byte
		if err := rows.Scan(&id, &org, &kind, &source, &content, &hash); err != nil {
			t.Fatal(err)
		}
		if count >= int64(len(got.Reviews)) {
			t.Fatal("decision projection omitted a physical row")
		}
		review := got.Reviews[count]
		canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(review)
		if err != nil || review.Id != id || review.OrganisationId != org || !bytes.Equal(canonical, content) || !bytes.Equal(review.SourceHash, hash) {
			t.Fatal("decision projection differs from exact stored canonical record/hash")
		}
		unsigned := proto.Clone(review).(*tammyv1.LegacyPurchaseReview)
		unsigned.SourceHash = nil
		hashInput, err := proto.MarshalOptions{Deterministic: true}.Marshal(unsigned)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(hashInput)
		if !bytes.Equal(hash, digest[:]) {
			t.Fatal("returned record has an invalid canonical hash")
		}
		total += 512 + int64(len(id)+len(org)+len(kind)+len(source)+len(content)+len(hash))
		count++
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	wantCount := postScalar(t, tx, `SELECT count(*) FROM purchase_legacy_reviews_v1 WHERE organisation_id=?`, postingOrg)
	wantBytes := postScalar(t, tx, `SELECT COALESCE(SUM(512+length(CAST(id AS BLOB))+length(CAST(organisation_id AS BLOB))+
		length(CAST(source_kind AS BLOB))+length(CAST(COALESCE(document_id,opening_payable_id) AS BLOB))+
		length(review_proto)+length(record_hash)),0) FROM purchase_legacy_reviews_v1 WHERE organisation_id=?`, postingOrg)
	if count != wantCount || total != wantBytes || got.Summary.RecordCount != count || got.Summary.DecisionBytes != total || got.Summary.OrganisationID != postingOrg {
		t.Fatalf("decision charge count/bytes=%d/%d SQL=%d/%d summary=%#v", count, total, wantCount, wantBytes, got.Summary)
	}
}

func TestPurchaseBASLegacyDecisionsEmptyAndInvalidReservations(t *testing.T) {
	ctx := context.Background()
	tx, f := emptyPurchaseBASScalarFixture(t)
	defer tx.Rollback()
	p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
	r := f.repo(p)
	before := purchaseBASScalarState(t, tx)
	summary, err := r.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || summary == nil || *summary != (purchases.BASLegacyDecisionSummary{OrganisationID: postingOrg}) {
		t.Fatalf("empty summary=%#v %v", summary, err)
	}
	got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, 0, 0)
	if err != nil || got == nil || len(got.Reviews) != 0 || got.Summary != *summary {
		t.Fatalf("empty read=%#v %v", got, err)
	}
	for _, budgets := range [][2]int64{{-1, 0}, {0, -1}} {
		queries := p.queries
		if got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, budgets[0], budgets[1]); got != nil || !errors.Is(err, purchases.ErrRepository) {
			t.Fatalf("negative reservation=%#v %v", got, err)
		}
		if p.queries != queries {
			t.Fatal("negative allowance reached SQL")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := r.SummarizeBASLegacyDecisions(cancelled, postingOrg); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled summary=%#v %v", got, err)
	}
	if got, err := r.ReadBASLegacyDecisions(cancelled, postingOrg, *summary, 0, 0); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read=%#v %v", got, err)
	}
	if p.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("empty decision API hydrated bodies, wrote, or ended caller transaction")
	}
}

func TestPurchaseBASLegacyDecisionsMatchCompleteFullReaders(t *testing.T) {
	ctx := context.Background()
	tx, f, first, firstRecord := purchaseBASLegacyDecisionFixture(t)
	postRequest := f.request
	second := electNextLegacy(t, f, first)
	f.addDocument("Additional retained evidence", "DECISION-SUPPORT", 0, 0)
	second.Review.SupportingEvidence = append(second.Review.SupportingEvidence, &tammyv1.PurchaseRetainedSourceRef{SourceId: f.document.Id, SourceVersion: f.document.Version, SourceHash: f.document.Sha256})
	second.Review.Reason = "Réexaminé: retained complete history"
	second.Review.AffectedPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}}
	second.Review.ReportingBalanceDate = second.Review.AffectedPeriod.PeriodEnd
	secondRecord, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	// A real modern posting must not hide its earlier legacy decision history.
	if _, err := f.repo(tx).PostReviewed(ctx, postRequest); err != nil {
		t.Fatalf("promote reviewed document: %v", err)
	}
	openingID := legacyOpeningFixture(t, f)
	openingRepo, _ := purchases.NewOpeningRepository(tx)
	opening, err := openingRepo.GetOpeningPayable(ctx, postingOrg, openingID)
	if err != nil {
		t.Fatal(err)
	}
	openingRequest := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	openingRequest.Review.Source = opening.LegacySource()
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, openingRequest); err != nil {
		t.Fatal(err)
	}
	want := []*tammyv1.LegacyPurchaseReview{}
	for _, source := range []*tammyv1.LegacyPurchaseSourceRef{first.Review.Source, openingRequest.Review.Source} {
		full, err := legacyRepo(t, f, tx).ReadLegacy(ctx, postingOrg, source)
		if err != nil {
			t.Fatalf("authoritative full history: %v", err)
		}
		want = append(want, full...)
	}
	if len(want) != 3 || !proto.Equal(want[0], firstRecord) || !proto.Equal(want[1], secondRecord.Review) {
		t.Fatal("fixture omitted real historical decisions")
	}
	before := purchaseBASScalarState(t, tx)
	p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
	r := f.repo(p)
	summary, err := r.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("summary=%#v %v", summary, err)
	}
	if p.decisionReads != 0 || p.bodyReads != 0 {
		t.Fatal("summary copied decision or financial bodies")
	}
	got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, summary.RecordCount, summary.DecisionBytes)
	if err != nil || got == nil || got.Summary != *summary || len(got.Reviews) != len(want) {
		t.Fatalf("exact read=%#v %v", got, err)
	}
	for index := range want {
		if !proto.Equal(got.Reviews[index], want[index]) {
			t.Fatalf("complete decision %d differs from full reader", index)
		}
	}
	assertPurchaseBASLegacyDecisionCharge(t, tx, got)
	reads, queries := p.decisionReads, p.queries
	for _, budgets := range [][2]int64{{summary.RecordCount - 1, summary.DecisionBytes}, {summary.RecordCount, summary.DecisionBytes - 1}} {
		if got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, budgets[0], budgets[1]); got != nil {
			t.Fatal("short reservation returned partial decisions")
		} else {
			assertPurchaseBASScalarBound(t, err)
		}
	}
	if p.queries != queries || p.decisionReads != reads || reads == 0 || p.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("decision projection exceeded reservation, hydrated bodies, wrote, or ended transaction")
	}
}

// Clone canonical decision records with exact outer-column mirrors. These rows
// intentionally have no corresponding accounting events, so this fixture proves
// decision history limits and scalar integrity, not complete financial validity.
func appendPurchaseBASLegacyDecisionOnly(t *testing.T, f *postingFixture, first *tammyv1.LegacyPurchaseReview, version uint64) *tammyv1.LegacyPurchaseReview {
	t.Helper()
	record := proto.Clone(first).(*tammyv1.LegacyPurchaseReview)
	record.Id, _ = f.id()
	record.SourceRevision = version
	record.SourceHash = nil
	unsigned, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(unsigned)
	record.SourceHash = digest[:]
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.executor.ExecContext(context.Background(), `INSERT INTO purchase_legacy_reviews_v1(
		id,organisation_id,source_kind,document_id,opening_payable_id,source_version,source_hash,reviewed_snapshot_hash,
		accounting_version,affected_period_start,affected_period_end,reporting_balance_date,disposition,
		previously_claimed_period_start,previously_claimed_period_end,claim_evidence_source_id,claim_evidence_source_version,
		claim_evidence_source_hash,linked_posting_id,reason,review_proto,record_hash,actor_user_id,created_at)
		SELECT ?,organisation_id,source_kind,document_id,opening_payable_id,source_version,source_hash,reviewed_snapshot_hash,
		?,affected_period_start,affected_period_end,reporting_balance_date,disposition,
		previously_claimed_period_start,previously_claimed_period_end,claim_evidence_source_id,claim_evidence_source_version,
		claim_evidence_source_hash,linked_posting_id,reason,?,?,actor_user_id,created_at
		FROM purchase_legacy_reviews_v1 WHERE id=?`, record.Id, version, content, record.SourceHash, first.Id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestPurchaseBASLegacyDecisionsSeparatePerSourceAndCombinedHistoryLimits(t *testing.T) {
	ctx := context.Background()
	tx, f, _, first := purchaseBASLegacyDecisionFixture(t)
	for version := uint64(2); version <= 1000; version++ {
		appendPurchaseBASLegacyDecisionOnly(t, f, first, version)
	}
	p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
	r := f.repo(p)
	summary, err := r.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || summary == nil || summary.RecordCount != 1000 {
		t.Fatalf("1000 canonical decisions summary=%#v %v", summary, err)
	}
	got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, summary.RecordCount, summary.DecisionBytes)
	if err != nil || got == nil || len(got.Reviews) != 1000 {
		t.Fatalf("1000 canonical decisions read=%#v %v", got, err)
	}
	// A distinct real source makes 1001 combined records without exceeding the
	// per-source cap. This must not inherit the 1000-candidate semantic limit.
	other := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, other); err != nil {
		t.Fatal(err)
	}
	summary, err = r.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || summary == nil || summary.RecordCount != 1001 {
		t.Fatalf("1001 combined summary=%#v %v", summary, err)
	}
	got, err = r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, summary.RecordCount, summary.DecisionBytes)
	if err != nil || got == nil || len(got.Reviews) != 1001 {
		t.Fatalf("1001 combined read=%#v %v", got, err)
	}
	assertPurchaseBASLegacyDecisionCharge(t, tx, got)
	appendPurchaseBASLegacyDecisionOnly(t, f, first, 1001)
	reads := p.decisionReads
	if got, err := r.SummarizeBASLegacyDecisions(ctx, postingOrg); got != nil {
		t.Fatal("1001 per-source decisions summarized")
	} else {
		assertPurchaseBASScalarBound(t, err)
	}
	if got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, 16384, 8*1024*1024); got != nil {
		t.Fatal("1001 per-source decisions returned")
	} else {
		assertPurchaseBASScalarBound(t, err)
	}
	if p.decisionReads != reads || p.bodyReads != 0 || !tx.Authenticated() {
		t.Fatal("per-source rejection copied decisions/bodies or ended transaction")
	}
}

func TestPurchaseBASLegacyDecisionsRejectCanonicalScalarAndOwnerCorruption(t *testing.T) {
	for _, test := range []struct {
		name, table, query string
		args               func(*tammyv1.LegacyPurchaseReview) []any
	}{
		{"outer_reason", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET reason='Different outer reason'`, nil},
		{"record_hash", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET record_hash=zeroblob(32)`, nil},
		{"source_owner", "documents", `UPDATE documents SET organisation_id='foreign-owner' WHERE id=?`, func(r *tammyv1.LegacyPurchaseReview) []any { return []any{r.Review.Source.GetDocument().DocumentId} }},
		{"cross_owner_record", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET organisation_id='foreign-owner'`, nil},
		{"noncanonical_proto", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET review_proto=CAST(review_proto || ? AS BLOB)`, func(r *tammyv1.LegacyPurchaseReview) []any {
			return []any{append([]byte{0x0a, byte(len(r.Id))}, []byte(r.Id)...)}
		}},
		{"malformed_proto", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET review_proto=x'ff'`, nil},
		{"gap", "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET accounting_version=2`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, f, _, record := purchaseBASLegacyDecisionFixture(t)
			reservation, err := f.repo(tx).SummarizeBASLegacyDecisions(context.Background(), postingOrg)
			if err != nil || reservation == nil {
				t.Fatal(err)
			}
			var args []any
			if test.args != nil {
				args = test.args(record)
			}
			mutatePurchaseBASScalar(t, f, test.table, test.query, args...)
			decoderCase := test.name == "noncanonical_proto" || test.name == "malformed_proto"
			if decoderCase {
				// Reserve the new byte length so the decoder, rather than stale
				// reservation comparison, must reject the invalid encoding.
				reservation, err = f.repo(tx).SummarizeBASLegacyDecisions(context.Background(), postingOrg)
				if err != nil || reservation == nil {
					t.Fatalf("noncanonical proto metadata summary=%#v %v", reservation, err)
				}
			}
			before := purchaseBASScalarState(t, tx)
			p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
			got, err := f.repo(p).ReadBASLegacyDecisions(context.Background(), postingOrg, *reservation, 16384, 8*1024*1024)
			if got != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Fatalf("corrupt decisions=%#v %v", got, err)
			}
			if decoderCase && p.decisionReads != 1 {
				t.Fatal("invalid proto fixture did not reach guarded decision decoder")
			}
			if p.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
				t.Fatal("corrupt decision read hydrated bodies, wrote, or ended transaction")
			}
		})
	}
}

func TestPurchaseBASLegacyDecisionsWithholdOversizedDriftCells(t *testing.T) {
	for _, test := range []struct{ name, query string }{
		{"proto", `UPDATE purchase_legacy_reviews_v1 SET review_proto=zeroblob(9437184)`},
		{"numeric", `UPDATE purchase_legacy_reviews_v1 SET accounting_version=zeroblob(9437184)`},
		{"metadata", `UPDATE purchase_legacy_reviews_v1 SET created_at=zeroblob(9437184)`},
		{"identity", `UPDATE purchase_legacy_reviews_v1 SET id=zeroblob(9437184)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, f, _, _ := purchaseBASLegacyDecisionFixture(t)
			p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
			r := f.repo(p)
			summary, err := r.SummarizeBASLegacyDecisions(context.Background(), postingOrg)
			if err != nil || summary == nil {
				t.Fatal(err)
			}
			var before runtime.MemStats
			p.beforeDecisionRead = func() error {
				mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1", test.query)
				runtime.ReadMemStats(&before)
				return nil
			}
			got, err := r.ReadBASLegacyDecisions(context.Background(), postingOrg, *summary, summary.RecordCount, summary.DecisionBytes)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			if got != nil || err == nil || lenErrorPurchaseBASScalar(err) > 1024 || after.TotalAlloc-before.TotalAlloc >= 2*1024*1024 {
				t.Fatalf("drift copied rejected 9 MiB %s cell: read=%t allocation=%d error=%v", test.name, got != nil, after.TotalAlloc-before.TotalAlloc, err)
			}
			if p.decisionReads != 1 || p.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("drift escaped decision guard or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASLegacyDecisionsRejectClaimAndPostingOwnerEdges(t *testing.T) {
	for _, edge := range []string{"claim", "posting"} {
		for _, foreignRecord := range []bool{false, true} {
			name := edge + "_foreign_reference"
			if foreignRecord {
				name = edge + "_only_requested_owner_touch"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
				tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
				var request *tammyv1.ResolveLegacyPurchaseReviewRequest
				var referencedTable, referencedID string
				if edge == "claim" {
					request = legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED)
					request.Review.PreviouslyClaimedPeriod = proto.Clone(request.Review.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
					request.Review.ClaimEvidence = proto.Clone(request.Review.SupportingEvidence[0]).(*tammyv1.PurchaseRetainedSourceRef)
					referencedTable, referencedID = "documents", request.Review.ClaimEvidence.SourceId
				} else {
					posting, err := f.repo(tx).PostReviewed(ctx, f.request)
					if err != nil {
						t.Fatal(err)
					}
					f.addDocument("Additional evidence", "DECISION-LINK", 10000, 1000)
					request = legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
					request.Review.LinkedPostingId = proto.String(posting.Posting.Id)
					referencedTable, referencedID = "purchase_postings_v1", posting.Posting.Id
				}
				if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request); err != nil {
					t.Fatal(err)
				}
				p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
				r := f.repo(p)
				baseline, err := r.SummarizeBASLegacyDecisions(ctx, postingOrg)
				if err != nil || baseline == nil {
					t.Fatal(err)
				}
				if got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *baseline, baseline.RecordCount, baseline.DecisionBytes); err != nil || got == nil || len(got.Reviews) != 1 {
					t.Fatalf("valid owner edge read=%#v %v", got, err)
				}
				if foreignRecord {
					// Leave only the claim/link owner touching the requested org.
					// Filtering solely by review/source owner would hide corruption.
					mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET organisation_id='foreign-owner' WHERE id=?`, request.Review.Source.GetDocument().DocumentId)
					mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET organisation_id='foreign-owner'`)
				} else {
					mutatePurchaseBASScalar(t, f, referencedTable, "UPDATE "+referencedTable+" SET organisation_id='foreign-owner' WHERE id=?", referencedID)
				}
				before := purchaseBASScalarState(t, tx)
				reads := p.decisionReads
				if got, err := r.SummarizeBASLegacyDecisions(ctx, postingOrg); got != nil || !errors.Is(err, purchases.ErrRepository) {
					t.Fatalf("foreign owner summary=%#v %v", got, err)
				}
				if got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *baseline, baseline.RecordCount, baseline.DecisionBytes); got != nil || !errors.Is(err, purchases.ErrRepository) {
					t.Fatalf("foreign owner decisions=%#v %v", got, err)
				}
				if p.decisionReads != reads || p.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
					t.Fatal("owner rejection copied bodies, wrote, or ended caller transaction")
				}
			})
		}
	}
}

func TestPurchaseBASLegacyDecisionsRejectMalformedSummaryWithoutPayloadReads(t *testing.T) {
	for _, test := range []struct{ name, query string }{
		{"source_version", `UPDATE purchase_legacy_reviews_v1 SET source_version=zeroblob(9437184)`},
		{"accounting_version", `UPDATE purchase_legacy_reviews_v1 SET accounting_version=1.5`},
		{"source_hash", `UPDATE purchase_legacy_reviews_v1 SET source_hash=zeroblob(0)`},
		{"snapshot_hash", `UPDATE purchase_legacy_reviews_v1 SET reviewed_snapshot_hash=zeroblob(0)`},
		{"record_hash", `UPDATE purchase_legacy_reviews_v1 SET record_hash=zeroblob(0)`},
		{"claim_version", `UPDATE purchase_legacy_reviews_v1 SET claim_evidence_source_version=zeroblob(9437184)`},
		{"reason", `UPDATE purchase_legacy_reviews_v1 SET reason=zeroblob(9437184)`},
		{"period", `UPDATE purchase_legacy_reviews_v1 SET affected_period_start=zeroblob(9437184)`},
		{"empty_proto", `UPDATE purchase_legacy_reviews_v1 SET review_proto=zeroblob(0)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, f, _, _ := purchaseBASLegacyDecisionFixture(t)
			mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1", test.query)
			p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
			got, err := f.repo(p).SummarizeBASLegacyDecisions(context.Background(), postingOrg)
			if got != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Fatalf("malformed summary=%#v %v", got, err)
			}
			if p.decisionReads != 0 || p.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("malformed summary copied payloads or ended transaction")
			}
		})
	}
}

func TestPurchaseBASLegacyDecisionsDoNotRequireCurrentFinancialBodiesOrResultEvents(t *testing.T) {
	tx, f, request, _ := purchaseBASLegacyDecisionFixture(t)
	ctx := context.Background()
	baseline, err := f.repo(tx).SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || baseline == nil {
		t.Fatal(err)
	}
	// Decision ownership needs source identities, while current amounts and
	// complete result events remain the responsibility of the full reader.
	mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET total_minor=zeroblob(9437184) WHERE id=?`, request.Review.Source.GetDocument().DocumentId)
	mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1", `UPDATE purchase_accounting_events_v1 SET result_proto=zeroblob(9437184)`)
	p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
	got, err := f.repo(p).ReadBASLegacyDecisions(ctx, postingOrg, *baseline, baseline.RecordCount, baseline.DecisionBytes)
	if err != nil || got == nil || len(got.Reviews) != 1 || got.Summary != *baseline || p.bodyReads != 0 {
		t.Fatalf("decision projection depended on financial bodies/events: %#v %v", got, err)
	}
}

func TestPurchaseBASLegacyDecisionsRejectDriftAndCancellationWithoutPartialRead(t *testing.T) {
	for _, stage := range []string{"count_drift", "oversized_count_sentinel", "byte_drift", "before_query_cancel", "after_query_cancel"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tx, f, _, first := purchaseBASLegacyDecisionFixture(t)
			p := &purchaseBASLegacyDecisionProbe{Transaction: tx}
			r := f.repo(p)
			summary, err := r.SummarizeBASLegacyDecisions(ctx, postingOrg)
			if err != nil || summary == nil {
				t.Fatal(err)
			}
			switch stage {
			case "count_drift":
				p.beforeDecisionRead = func() error { appendPurchaseBASLegacyDecisionOnly(t, f, first, 2); return nil }
			case "oversized_count_sentinel":
				p.beforeDecisionRead = func() error {
					// Revision two sorts after the accepted first row. Its invalid
					// proto must stay masked behind the count sentinel.
					extra := appendPurchaseBASLegacyDecisionOnly(t, f, first, 2)
					mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET review_proto=zeroblob(9437184) WHERE id=?`, extra.Id)
					return nil
				}
			case "byte_drift":
				p.beforeDecisionRead = func() error {
					record := proto.Clone(first).(*tammyv1.LegacyPurchaseReview)
					record.Review.Reason = strings.Repeat("é", 200)
					record.SourceHash = nil
					unsigned, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
					if err != nil {
						return err
					}
					digest := sha256.Sum256(unsigned)
					record.SourceHash = digest[:]
					content, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
					if err != nil {
						return err
					}
					mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1", `UPDATE purchase_legacy_reviews_v1 SET reason=?,review_proto=?,record_hash=? WHERE id=?`, record.Review.Reason, content, record.SourceHash, record.Id)
					return nil
				}
			case "before_query_cancel":
				p.beforeDecisionRead = func() error { cancel(); return nil }
			case "after_query_cancel":
				p.afterDecisionQuery = cancel
			}
			got, err := r.ReadBASLegacyDecisions(ctx, postingOrg, *summary, summary.RecordCount, summary.DecisionBytes)
			if got != nil {
				t.Fatal("drift/cancellation returned partial decision history")
			}
			if stage == "count_drift" || stage == "oversized_count_sentinel" {
				assertPurchaseCoverageBound(t, err, "legacy decision records", summary.RecordCount)
			} else if stage == "byte_drift" {
				assertPurchaseCoverageBound(t, err, "legacy decision bytes", summary.DecisionBytes)
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error=%v", err)
			}
			if p.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("failed read hydrated bodies or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASLegacyDecisionsSurviveEncryptedReopenAndCallerRollback(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request); err != nil {
		t.Fatal(err)
	}
	baseline, err := f.repo(tx).SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || baseline == nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	read, err := db.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	f.executor = read
	p := &purchaseBASLegacyDecisionProbe{Transaction: read}
	got, err := f.repo(p).ReadBASLegacyDecisions(ctx, postingOrg, *baseline, baseline.RecordCount, baseline.DecisionBytes)
	if err != nil || got == nil {
		t.Fatalf("reopened decisions=%#v %v", got, err)
	}
	full, err := legacyRepo(t, f, read).ReadLegacy(ctx, postingOrg, request.Review.Source)
	if err != nil || len(full) != 1 || !proto.Equal(full[0], got.Reviews[0]) {
		t.Fatalf("reopened full oracle=%v %v", full, err)
	}
	next := electNextLegacy(t, f, request)
	next.Review.Reason = "Caller-owned uncommitted decision"
	if _, err := legacyRepo(t, f, read).ResolveLegacy(ctx, next); err != nil {
		t.Fatal(err)
	}
	summary, err := f.repo(p).SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || summary == nil || summary.RecordCount != 2 {
		t.Fatalf("caller decision summary=%#v %v", summary, err)
	}
	if err := read.Rollback(); err != nil {
		t.Fatal(err)
	}
	check, err := db.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Rollback()
	summary, err = f.repo(check).SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || summary == nil || *summary != *baseline || p.bodyReads != 0 {
		t.Fatalf("caller rollback changed committed decisions: %#v %v", summary, err)
	}
}

func TestPurchaseBASLegacyDecisionsRejectSameByteBlobOwnerWithoutTargetTextReference(t *testing.T) {
	ctx := context.Background()
	tx, f, _, writerReview := purchaseBASLegacyDecisionFixture(t)
	probe := &purchaseBASLegacyDecisionProbe{Transaction: tx}
	repo := f.repo(probe)

	// Passing control: a real writer-created, canonical owner remains readable.
	control, err := repo.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || control == nil || control.RecordCount != 1 {
		t.Fatalf("canonical decision summary=%#v %v", control, err)
	}
	read, err := repo.ReadBASLegacyDecisions(ctx, postingOrg, *control, control.RecordCount, control.DecisionBytes)
	if err != nil || read == nil || len(read.Reviews) != 1 || !proto.Equal(read.Reviews[0], writerReview) {
		t.Fatalf("canonical writer decision read=%#v %v", read, err)
	}
	readsBeforeCorruption := probe.decisionReads
	if readsBeforeCorruption != 1 {
		t.Fatalf("canonical control decision reads=%d, want 1", readsBeforeCorruption)
	}

	// Corruption-only fixture: remove every requested-org TEXT owner arm, then
	// preserve the requested UUID's exact bytes in the review owner as a BLOB.
	// This does not claim financial validity after either owner is corrupted.
	documentID := writerReview.Review.Source.GetDocument().GetDocumentId()
	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id='foreign-owner' WHERE id=?`, documentID)
	mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
		`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(organisation_id AS BLOB) WHERE id=?`, writerReview.Id)
	before := purchaseBASScalarState(t, tx)

	if summary, err := repo.SummarizeBASLegacyDecisions(ctx, postingOrg); summary != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("same-byte BLOB review owner without a target TEXT reference was omitted: summary=%#v err=%v", summary, err)
	}
	if got, err := repo.ReadBASLegacyDecisions(ctx, postingOrg, *control, control.RecordCount, control.DecisionBytes); got != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("same-byte BLOB review owner read=%#v err=%v", got, err)
	}
	if probe.decisionReads != readsBeforeCorruption || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("owner-scope summary hydrated decision/financial bodies, wrote, or ended caller transaction")
	}
}

func TestPurchaseBASLegacyDecisionsRejectSameByteBlobOwnerDriftAfterEmptyReservation(t *testing.T) {
	ctx := context.Background()
	tx, f, _, writerReview := purchaseBASLegacyDecisionFixture(t)
	documentID := writerReview.Review.Source.GetDocument().GetDocumentId()

	// Make the real writer-created row genuinely unrelated to the requested
	// organisation before taking the empty reservation. This is corruption-only
	// test setup, not a claim that the altered retained history is valid.
	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id='foreign-owner' WHERE id=?`, documentID)
	mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
		`UPDATE purchase_legacy_reviews_v1 SET organisation_id='foreign-owner' WHERE id=?`, writerReview.Id)

	probe := &purchaseBASLegacyDecisionProbe{Transaction: tx}
	repo := f.repo(probe)
	expected, err := repo.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || expected == nil || *expected != (purchases.BASLegacyDecisionSummary{OrganisationID: postingOrg}) {
		t.Fatalf("foreign baseline decision summary=%#v %v", expected, err)
	}

	var afterCorruption map[string]any
	probe.beforeDecisionRead = func() error {
		// This runs after Read's fresh empty summary but before the guarded SQL.
		// Only the decision owner changes; the referenced document stays foreign.
		mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
			`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, writerReview.Id)
		afterCorruption = purchaseBASScalarState(t, tx)
		return nil
	}
	got, err := repo.ReadBASLegacyDecisions(ctx, postingOrg, *expected, 0, 0)
	if got != nil || err == nil {
		t.Fatalf("same-byte BLOB owner drift returned a decision census: read=%#v err=%v", got, err)
	}
	var bound *purchases.SourceBoundError
	if !errors.Is(err, purchases.ErrSourceBound) || !errors.As(err, &bound) || bound.Resource != "legacy decision records" || bound.Limit != 0 {
		t.Fatalf("drift did not reach the zero-record sentinel: %v", err)
	}
	if probe.decisionReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() || afterCorruption == nil ||
		!reflect.DeepEqual(afterCorruption, purchaseBASScalarState(t, tx)) {
		t.Fatal("drift guard hydrated bodies, wrote, or ended caller transaction")
	}
}

func TestPurchaseBASLegacyDecisionsLeaveUnrelatedBlobOwnerOutOfScope(t *testing.T) {
	ctx := context.Background()
	tx, f, _, writerReview := purchaseBASLegacyDecisionFixture(t)
	documentID := writerReview.Review.Source.GetDocument().GetDocumentId()
	const foreignID = "018f0000-0000-7000-8000-000000009999"

	// This corruption-only sentinel uses a different UUID value as a BLOB for
	// both owner cells. It is intentionally unrelated to the requested org; it
	// does not assert valid ownership or financial history after mutation.
	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?`, foreignID, documentID)
	mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
		`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, foreignID, writerReview.Id)
	before := purchaseBASScalarState(t, tx)
	probe := &purchaseBASLegacyDecisionProbe{Transaction: tx}
	repo := f.repo(probe)

	summary, err := repo.SummarizeBASLegacyDecisions(ctx, postingOrg)
	if err != nil || summary == nil || *summary != (purchases.BASLegacyDecisionSummary{OrganisationID: postingOrg}) {
		t.Fatalf("unrelated BLOB owner entered target decision census: summary=%#v %v", summary, err)
	}
	got, err := repo.ReadBASLegacyDecisions(ctx, postingOrg, *summary, 0, 0)
	if err != nil || got == nil || got.Summary != *summary || len(got.Reviews) != 0 {
		t.Fatalf("unrelated BLOB owner changed target decision read: read=%#v %v", got, err)
	}
	if probe.decisionReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("out-of-scope owner read hydrated bodies, wrote, or ended caller transaction")
	}
}
