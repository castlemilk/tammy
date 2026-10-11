//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const postingOrg = "018f0000-0000-7000-8000-000000001001"
const postingExpense = "018f0000-0000-7000-8000-000000001002"

type postingFixture struct {
	t        *testing.T
	executor app.CommandSQLExecutor
	now      time.Time
	next     int
	request  *tammyv1.PostReviewedPurchaseRequest
	document *tammyv1.Document
	date     *tammyv1.CivilDate
}

func newPostingFixture(t *testing.T, executor app.CommandSQLExecutor, basis tammyv1.GstBasis) *postingFixture {
	t.Helper()
	f := &postingFixture{t: t, executor: executor, now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), next: 1100}
	ctx := context.Background()
	stamp := f.now.Format(time.RFC3339Nano)
	if _, err := executor.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,'post@example.test','Owner','ACTIVE',?,?)`, testkit.ActorUserID, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	org := &tammyv1.Organisation{Id: postingOrg, Version: 1, Abn: "51824753556", LegalName: "Purchase Pty Ltd", DisplayName: "Purchase", EntityType: "Australian Private Company", GstBasis: basis, GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY, FinancialYearEndMonth: 6, OwnerUserId: testkit.ActorUserID, VerificationState: tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED, ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: "018f0000-0000-7000-8000-000000001003", Revision: 1, ContentHash: make([]byte, 32)}}
	orgRepo, _ := organisations.NewRepository(executor)
	if err := orgRepo.Create(ctx, org, f.now); err != nil {
		t.Fatal(err)
	}
	accounts, _ := accounting.NewAccountRepository(executor)
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.InstallTemplate(ctx, postingOrg, template, f.now); err != nil {
		t.Fatal(err)
	}
	expense := &tammyv1.Account{Id: postingExpense, OrganisationId: postingOrg, Version: 1, Code: "5001", Name: "Office costs", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "operating"}
	if err := accounts.Create(ctx, expense, f.now); err != nil {
		t.Fatal(err)
	}
	f.addDocument("Supplier", "COST-1", 10000, 1000)
	return f
}

func postMoney(amount int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: amount}
}
func postDate(day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: 2026, Month: 8, Day: day}
}
func (f *postingFixture) id() (string, error) {
	f.next++
	return fmt.Sprintf("018f0000-0000-7000-8000-%012d", f.next), nil
}
func (f *postingFixture) addDocument(supplier, invoice string, net, gst int64) {
	f.addDocumentReview(supplier, invoice, net, gst, true)
}
func (f *postingFixture) addDocumentReview(supplier, invoice string, net, gst int64, review bool) {
	f.t.Helper()
	id, _ := f.id()
	key, _ := f.id()
	original := []byte("original purchase " + id)
	hash := sha256.Sum256(original)
	date := postDate(10)
	if f.date != nil {
		date = proto.Clone(f.date).(*tammyv1.CivilDate)
	}
	candidate := &tammyv1.DocumentCandidate{SupplierName: supplier, InvoiceNumber: invoice, DocumentDate: date, Subtotal: postMoney(net), Gst: postMoney(gst), Total: postMoney(net + gst)}
	doc := &tammyv1.Document{Id: id, OrganisationId: postingOrg, Version: 1, Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, SourceDisplayName: "cost.pdf", MimeType: "application/pdf", ByteLength: uint64(len(original)), Sha256: hash[:], Candidate: candidate, CreatedAt: timestamppb.New(f.now)}
	repo, _ := documents.NewRepository(f.executor)
	if _, err := repo.Create(context.Background(), key, testkit.ActorUserID, doc, original); err != nil {
		f.t.Fatal(err)
	}
	reviewed := doc
	var err error
	if review {
		reviewed, err = repo.SaveReview(context.Background(), id, 1, candidate, f.now)
		if err != nil {
			f.t.Fatal(err)
		}
	}
	f.document = reviewed
	operation, _ := f.id()
	f.request = &tammyv1.PostReviewedPurchaseRequest{CommandContext: testkit.CommandContext(operation), OrganisationId: postingOrg, DocumentId: id, ExpectedReviewedVersion: 2,
		Allocations:           []*tammyv1.PurchaseAllocationInput{{AccountId: postingExpense, AccountVersion: 1, Sequence: 1, Gross: postMoney(net + gst), SupplierGst: postMoney(gst), EligibleGst: postMoney(gst), Use: tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS, BasClassification: tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL, CashFlowClassification: tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING, EligibilityReviewConfirmed: true, EvidenceReviewConfirmed: true}},
		Evidence:              &tammyv1.PurchaseEvidenceInput{Source: &tammyv1.PurchaseRetainedSourceRef{SourceId: id, SourceVersion: 2, SourceHash: hash[:]}, Classification: tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_TAX_INVOICE, HeldDate: proto.Clone(date).(*tammyv1.CivilDate), RegistrationReviewConfirmed: true, BusinessUseReviewConfirmed: true, EvidenceReviewConfirmed: true, Reason: "Reviewed tax invoice"},
		DuplicateReview:       &tammyv1.PurchaseDuplicateReview{Decision: tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_NO_MATCH, Reason: "No matching purchase"},
		PriorAccountingReview: &tammyv1.PurchasePriorAccountingReview{Decision: tammyv1.PurchasePriorAccountingDecision_PURCHASE_PRIOR_ACCOUNTING_DECISION_NOT_PREVIOUSLY_ACCOUNTED_OR_CLAIMED, ReviewedExistingAccountingConfirmed: true, Reason: "Not previously accounted or claimed"}}
	f.elect(f.request)
}
func (f *postingFixture) elect(request *tammyv1.PostReviewedPurchaseRequest) {
	f.t.Helper()
	_, err := f.executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES(?, ?, 'tammy.v1.PurchaseService.PostReviewedPurchase', ?, 'v1', 'tammy.v1.PostReviewedPurchaseRequest', ?, 'ELECTED', ?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, request.CommandContext.IdempotencyKey, make([]byte, 32), f.now.Format(time.RFC3339Nano))
	if err != nil {
		f.t.Fatal(err)
	}
}
func (f *postingFixture) corruptReviewed(query string) {
	f.t.Helper()
	if _, err := f.executor.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS documents_review_transition`); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.executor.ExecContext(context.Background(), query, f.document.Id); err != nil {
		f.t.Fatal(err)
	}
}
func (f *postingFixture) repo(executor app.CommandSQLExecutor) *purchases.Repository {
	f.t.Helper()
	repo, err := purchases.NewRepository(executor, func() time.Time { return f.now }, f.id)
	if err != nil {
		f.t.Fatal(err)
	}
	return repo
}
func postScalar(t *testing.T, executor app.CommandSQLExecutor, query string, args ...any) int64 {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var value int64
	if !rows.Next() || rows.Scan(&value) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("invalid scalar: %s", query)
	}
	return value
}

func TestPostReviewedPurchasePersistsOwnedSnapshotJournalAndSingleRevision(t *testing.T) {
	for _, test := range []struct {
		name              string
		basis             tammyv1.GstBasis
		missing           bool
		zero              bool
		control           string
		claimed, suspense int64
	}{
		{"cash", tammyv1.GstBasis_GST_BASIS_CASH, false, false, "1160", 0, 0},
		{"noncash ready", tammyv1.GstBasis_GST_BASIS_NON_CASH, false, false, "1150", 1000, 0},
		{"noncash missing", tammyv1.GstBasis_GST_BASIS_NON_CASH, true, false, "1170", 0, 1000},
		{"zero", tammyv1.GstBasis_GST_BASIS_CASH, false, true, "", 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, test.basis)
			if test.zero {
				f.addDocument("Supplier", "ZERO", 0, 0)
			}
			if test.missing {
				f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
				f.request.Evidence.Source = nil
				f.request.Evidence.HeldDate = nil
			}
			repo := f.repo(tx)
			response, err := repo.PostReviewed(context.Background(), f.request)
			if err != nil {
				t.Fatal(err)
			}
			p := response.Posting
			if p == nil || p.DocumentId != f.document.Id || p.ReviewedVersion != 2 || p.FinancialRevision != 1 || len(p.SourceHash) != 32 || len(p.ReviewedSnapshotHash) != 32 || !proto.Equal(p.Evidence, f.request.Evidence) {
				t.Fatalf("posting = %#v", p)
			}
			candidateBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(f.document.Candidate)
			snapshot := sha256.Sum256(candidateBytes)
			if !proto.Equal(&tammyv1.SourceRef{ContentHash: p.ReviewedSnapshotHash}, &tammyv1.SourceRef{ContentHash: snapshot[:]}) {
				t.Fatal("reviewed snapshot hash is not candidate hash")
			}
			a, err := repo.Get(context.Background(), postingOrg, f.document.Id)
			if err != nil {
				t.Fatal(err)
			}
			if !a.Posted || a.AccountingVersion != 1 || a.Outstanding.MinorUnits != p.Gross.MinorUnits || a.Paid.MinorUnits != 0 || a.AttributedGst.MinorUnits != test.claimed || a.SuspenseGst.MinorUnits != test.suspense {
				t.Fatalf("accounting = %#v", a)
			}
			if test.zero {
				if response.Journal != nil || p.JournalId != nil {
					t.Fatal("zero purchase created a journal")
				}
			} else {
				if response.Journal == nil || response.Journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE {
					t.Fatal("missing owned journal")
				}
				query := `SELECT COALESCE(SUM(l.debit_minor-l.credit_minor),0) FROM journal_lines l JOIN accounts a ON a.id=l.account_id WHERE l.journal_id=? AND a.code=?`
				if got := postScalar(t, tx, query, response.Journal.Id, "5001"); got != 10000 {
					t.Fatalf("expense = %d", got)
				}
				if got := postScalar(t, tx, query, response.Journal.Id, test.control); got != 1000 {
					t.Fatalf("GST control = %d", got)
				}
				if got := postScalar(t, tx, query, response.Journal.Id, "2000"); got != -11000 {
					t.Fatalf("payable = %d", got)
				}
			}
			for _, table := range []string{"purchase_posting_allocations_v1", "purchase_tax_evidence_v1", "purchase_accounting_events_v1", "financial_revision_claims"} {
				if got := postScalar(t, tx, "SELECT count(*) FROM "+table); got != 1 {
					t.Fatalf("%s rows = %d", table, got)
				}
			}
			wantLedger := int64(1)
			if test.zero {
				wantLedger = 0
			}
			if got := postScalar(t, tx, `SELECT ledger_revision FROM financial_revisions WHERE id=1`); got != wantLedger {
				t.Fatalf("ledger = %d", got)
			}
			if got := postScalar(t, tx, `SELECT tax_source_revision FROM financial_revisions WHERE id=1`); got != 1 {
				t.Fatalf("tax source = %d", got)
			}
			if got := postScalar(t, tx, `SELECT count(*) FROM purchase_gst_attributions_v1`); got != 0 {
				t.Fatal("ordinary posting created attribution")
			}
		})
	}
}

func TestPostReviewedPurchaseRejectsInvalidInputsBeforeReservation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *postingFixture)
	}{
		{"stale", func(t *testing.T, f *postingFixture) { f.request.ExpectedReviewedVersion = 1 }},
		{"foreign", func(t *testing.T, f *postingFixture) {
			f.request.OrganisationId = "018f0000-0000-7000-8000-000000009999"
		}},
		{"unreviewed", func(t *testing.T, f *postingFixture) {
			f.addDocumentReview("Unreviewed", "NEW", 10000, 1000, false)
		}},
		{"allocation arithmetic", func(t *testing.T, f *postingFixture) { f.request.Allocations[0].Gross.MinorUnits-- }},
		{"account version", func(t *testing.T, f *postingFixture) { f.request.Allocations[0].AccountVersion = 2 }},
		{"account purpose", func(t *testing.T, f *postingFixture) {
			f.request.Allocations[0].BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_CAPITAL
		}},
		{"account version overflow", func(t *testing.T, f *postingFixture) { f.request.Allocations[0].AccountVersion = math.MaxUint64 }},
		{"reviewed version overflow", func(t *testing.T, f *postingFixture) { f.request.ExpectedReviewedVersion = math.MaxUint64 }},
		{"evidence version", func(t *testing.T, f *postingFixture) { f.request.Evidence.Source.SourceVersion = 1 }},
		{"evidence version overflow", func(t *testing.T, f *postingFixture) { f.request.Evidence.Source.SourceVersion = math.MaxUint64 }},
		{"missing actor", func(t *testing.T, f *postingFixture) { f.request.CommandContext.Authentication.ActorUserId = "" }},
		{"missing prior confirmation", func(t *testing.T, f *postingFixture) {
			f.request.PriorAccountingReview.ReviewedExistingAccountingConfirmed = false
		}},
		{"protected account", func(t *testing.T, f *postingFixture) {
			rows, err := f.executor.QueryContext(context.Background(), `SELECT id FROM accounts WHERE organisation_id=? AND code='1150'`, postingOrg)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() || rows.Scan(&f.request.Allocations[0].AccountId) != nil {
				t.Fatal("missing control")
			}
		}},
		{"cash account", func(t *testing.T, f *postingFixture) {
			rows, err := f.executor.QueryContext(context.Background(), `SELECT id FROM accounts WHERE organisation_id=? AND code='1000'`, postingOrg)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() || rows.Scan(&f.request.Allocations[0].AccountId) != nil {
				t.Fatal("missing bank")
			}
		}},
		{"archived account", func(t *testing.T, f *postingFixture) {
			if _, err := f.executor.ExecContext(context.Background(), `UPDATE accounts SET status='ARCHIVED',version=version+1 WHERE id=?`, postingExpense); err != nil {
				t.Fatal(err)
			}
			f.request.Allocations[0].AccountVersion = 2
		}},
		{"source arithmetic", func(t *testing.T, f *postingFixture) {
			f.corruptReviewed(`UPDATE documents SET subtotal_minor=9999 WHERE id=?`)
		}},
		{"nil source date", func(t *testing.T, f *postingFixture) {
			f.corruptReviewed(`UPDATE documents SET document_date=NULL WHERE id=?`)
		}},
		{"invalid source date", func(t *testing.T, f *postingFixture) {
			f.corruptReviewed(`UPDATE documents SET document_date='2026-02-30' WHERE id=?`)
		}},
		{"SQL year unsupported", func(t *testing.T, f *postingFixture) {
			f.corruptReviewed(`UPDATE documents SET document_date='1899-08-10' WHERE id=?`)
		}},
		{"original bytes corrupt", func(t *testing.T, f *postingFixture) {
			if _, err := f.executor.ExecContext(context.Background(), `DROP TRIGGER documents_immutable_source`); err != nil {
				t.Fatal(err)
			}
			f.corruptReviewed(`UPDATE documents SET original_bytes=zeroblob(byte_length) WHERE id=?`)
		}},
		{"unsupported exception", func(t *testing.T, f *postingFixture) {
			f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION
			f.request.Evidence.ExceptionReview = &tammyv1.PurchaseTaxInvoiceExceptionReview{Kind: tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_LOW_VALUE_ACQUISITION, Rule: proto.Clone(f.request.Evidence.Source).(*tammyv1.PurchaseRetainedSourceRef), SupportingRecords: []*tammyv1.PurchaseRetainedSourceRef{proto.Clone(f.request.Evidence.Source).(*tammyv1.PurchaseRetainedSourceRef)}, TaxInvoiceNotRequiredReviewConfirmed: true, SupportingRecordRequirementsReviewConfirmed: true, Reason: "Explicit rule still needs retained verification"}
		}},
		{"evidence source hash", func(t *testing.T, f *postingFixture) { f.request.Evidence.Source.SourceHash = make([]byte, 32) }},
		{"prior review", func(t *testing.T, f *postingFixture) { f.request.PriorAccountingReview = nil }},
		{"basis history", func(t *testing.T, f *postingFixture) {
			_, err := f.executor.ExecContext(context.Background(), `UPDATE organisations SET version=2 WHERE id=?`, postingOrg)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"basis changed", func(t *testing.T, f *postingFixture) {
			profiles, _ := organisations.NewRepository(f.executor)
			old, err := profiles.Get(context.Background(), postingOrg)
			if err != nil {
				t.Fatal(err)
			}
			current := proto.Clone(old).(*tammyv1.Organisation)
			current.Version = 2
			current.GstBasis = tammyv1.GstBasis_GST_BASIS_CASH
			if err := profiles.Update(context.Background(), 1, current, f.now); err != nil {
				t.Fatal(err)
			}
			for index, profile := range []*tammyv1.Organisation{old, current} {
				key, _ := f.id()
				var result proto.Message = &tammyv1.CreateOrganisationResponse{Organisation: profile}
				method, typeName, requestType := "CreateOrganisation", "tammy.v1.CreateOrganisationResponse", "tammy.v1.CreateOrganisationRequest"
				if index == 1 {
					result = &tammyv1.UpdateOrganisationResponse{Organisation: profile}
					method = "UpdateOrganisation"
					typeName = "tammy.v1.UpdateOrganisationResponse"
					requestType = "tammy.v1.UpdateOrganisationRequest"
				}
				content, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.executor.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,result_type,result_proto,created_at,completed_at) VALUES(?, ?, ?, ?, 'v1', ?, ?, 'COMMITTED', ?, ?, ?, ?)`, "018f0000-0000-7000-8000-000000001099", testkit.ActorUserID, "tammy.v1.OrganisationService."+method, key, requestType, make([]byte, 32), typeName, content, f.now.Format(time.RFC3339Nano), f.now.Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := invoices.CheckGSTBasisHistory(context.Background(), f.executor, postingOrg, 0); !errors.Is(err, invoices.ErrGSTBasisHistory) {
				t.Fatalf("retained full history did not identify basis transition: %v", err)
			}
		}},
		{"closed", func(t *testing.T, f *postingFixture) {
			_, err := f.executor.ExecContext(context.Background(), `INSERT INTO accounting_periods(id,organisation_id,start_date,end_date,state,closed_at) VALUES(?,?,'2026-08-01','2026-08-31','CLOSED',?)`, "018f0000-0000-7000-8000-000000009998", postingOrg, f.now.Format(time.RFC3339Nano))
			if err != nil {
				t.Fatal(err)
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
			test.mutate(t, f)
			if _, err := f.repo(tx).PostReviewed(context.Background(), f.request); err == nil {
				t.Fatal("invalid posting accepted")
			}
			if got := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`); got != 0 {
				t.Fatalf("rejected input reserved %d revisions", got)
			}
		})
	}
}

func TestPostReviewedPurchaseSemanticDuplicatesRequireExactDistinctReview(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	repo := f.repo(tx)
	first, err := repo.PostReviewed(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PostReviewed(context.Background(), f.request); err == nil {
		t.Fatal("second posting accepted")
	}
	f.date = postDate(9)
	f.addDocument("Ｓｕｐｐｌｉｅｒ", "cost-1", 20000, 2000)
	if _, err := repo.PostReviewed(context.Background(), f.request); err == nil {
		t.Fatal("different amount bypassed duplicate discovery")
	}
	f.request.DuplicateReview.Decision = tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_DISTINCT_TRANSACTION
	f.request.DuplicateReview.MatchedPostingId = proto.String(first.Posting.Id)
	f.request.DuplicateReview.Reason = "Supplier reused invoice number for a distinct purchase"
	second, err := repo.PostReviewed(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Posting.DuplicateCandidates) != 1 || second.Posting.DuplicateCandidates[0].Id != first.Posting.Id || !proto.Equal(&tammyv1.SourceRef{ContentHash: second.Posting.DuplicateCandidates[0].ContentHash}, &tammyv1.SourceRef{ContentHash: first.Posting.SourceHash}) {
		t.Fatal("distinct review lost server provenance")
	}
	f.addDocument("Supplier", "COST-1", 30000, 3000)
	f.request.DuplicateReview.Decision = tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_DISTINCT_TRANSACTION
	f.request.DuplicateReview.MatchedPostingId = proto.String(first.Posting.Id)
	f.request.DuplicateReview.Reason = "Distinct"
	if _, err := repo.PostReviewed(context.Background(), f.request); err == nil {
		t.Fatal("multiple duplicate matches accepted")
	}
}

type postingInsertFailure struct{ app.CommandSQLExecutor }

func (e postingInsertFailure) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "INSERT INTO purchase_tax_evidence_v1") {
		return nil, errors.New("injected evidence insert failure")
	}
	return e.CommandSQLExecutor.ExecContext(ctx, query, args...)
}

func TestPostReviewedPurchasePartialFailureRollsBackAllOwnedFacts(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	if _, err := f.repo(postingInsertFailure{tx}).PostReviewed(ctx, f.request); err == nil {
		t.Fatal("injected failure ignored")
	}
	if postScalar(t, tx, `SELECT count(*) FROM purchase_postings_v1`) != 1 {
		t.Fatal("failure did not follow retained posting")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	verify, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Rollback()
	for _, table := range []string{"purchase_postings_v1", "purchase_posting_allocations_v1", "purchase_tax_evidence_v1", "purchase_accounting_events_v1", "financial_revision_claims", "journals", "cash_flow_facts"} {
		if got := postScalar(t, verify, "SELECT count(*) FROM "+table); got != 0 {
			t.Fatalf("rollback retained %s=%d", table, got)
		}
	}
}

func TestPostReviewedPurchaseMixedCostsAndRestartPersistence(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	accounts, _ := accounting.NewAccountRepository(tx)
	assetID, _ := f.id()
	privateID, _ := f.id()
	for _, account := range []*tammyv1.Account{
		{Id: assetID, OrganisationId: postingOrg, Version: 1, Code: "1501", Name: "Equipment", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "investing"},
		{Id: privateID, OrganisationId: postingOrg, Version: 1, Code: "3101", Name: "Owner drawings", Type: tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "financing"},
	} {
		if err := accounts.Create(ctx, account, f.now); err != nil {
			t.Fatal(err)
		}
	}
	expense := proto.Clone(f.request.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
	expense.Gross = postMoney(5500)
	expense.SupplierGst = postMoney(500)
	expense.EligibleGst = postMoney(400)
	expense.NoncreditableReason = "Partly noncreditable business cost"
	asset := proto.Clone(expense).(*tammyv1.PurchaseAllocationInput)
	asset.AccountId = assetID
	asset.Sequence = 2
	asset.Gross = postMoney(3300)
	asset.SupplierGst = postMoney(300)
	asset.EligibleGst = postMoney(300)
	asset.NoncreditableReason = ""
	asset.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_CAPITAL
	asset.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_INVESTING
	private := proto.Clone(expense).(*tammyv1.PurchaseAllocationInput)
	private.AccountId = privateID
	private.Sequence = 3
	private.Gross = postMoney(2200)
	private.SupplierGst = postMoney(200)
	private.EligibleGst = postMoney(0)
	private.Use = tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE
	private.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED
	private.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING
	private.NoncreditableReason = "Private use"
	f.request.Allocations = []*tammyv1.PurchaseAllocationInput{expense, asset, private}
	response, err := f.repo(tx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		code  string
		minor int64
	}{{"5001", 5100}, {"1501", 3000}, {"3101", 2200}, {"1150", 700}, {"2000", -11000}} {
		got := postScalar(t, tx, `SELECT SUM(l.debit_minor-l.credit_minor) FROM journal_lines l JOIN accounts a ON a.id=l.account_id WHERE l.journal_id=? AND a.code=?`, response.Journal.Id, want.code)
		if got != want.minor {
			t.Fatalf("%s movement=%d", want.code, got)
		}
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
	f.executor = readTx
	stored, err := f.repo(readTx).Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(stored, response.Accounting) || !proto.Equal(stored.Posting.Allocations[2], private) {
		t.Fatal("restart changed immutable accounting facts")
	}
}

func TestPostReviewedPurchaseGetRejectsRetainedCorruption(t *testing.T) {
	for _, test := range []struct {
		name, trigger, query string
		alterResult          bool
	}{
		{"posting hash", "purchase_postings_v1_no_update", `UPDATE purchase_postings_v1 SET source_hash=zeroblob(32)`, false},
		{"posting scalar", "purchase_postings_v1_no_update", `UPDATE purchase_postings_v1 SET gst_history_watermark=1`, false},
		{"reviewed snapshot", "purchase_postings_v1_no_update", `UPDATE purchase_postings_v1 SET reviewed_source_proto=x'01'`, false},
		{"allocation scalar", "purchase_posting_allocations_v1_no_update", `UPDATE purchase_posting_allocations_v1 SET account_version=2`, false},
		{"allocation bytes", "purchase_posting_allocations_v1_no_update", `UPDATE purchase_posting_allocations_v1 SET allocation_proto=x'01'`, false},
		{"evidence hash", "purchase_tax_evidence_v1_no_update", `UPDATE purchase_tax_evidence_v1 SET record_hash=zeroblob(32)`, false},
		{"evidence scalar", "purchase_tax_evidence_v1_no_update", `UPDATE purchase_tax_evidence_v1 SET reason='Changed reason'`, false},
		{"event hash", "purchase_accounting_events_v1_no_update", `UPDATE purchase_accounting_events_v1 SET result_sha256=zeroblob(32)`, false},
		{"event vector", "purchase_accounting_events_v1_no_update", `UPDATE purchase_accounting_events_v1 SET ledger_revision=ledger_revision+1`, false},
		{"event first command", "purchase_accounting_events_v1_no_update", `UPDATE purchase_accounting_events_v1 SET command_type='tammy.v1.PurchaseService.RecordPurchaseTaxEvidence'`, false},
		{"event missing", "purchase_accounting_events_v1_no_delete", `DELETE FROM purchase_accounting_events_v1`, false},
		{"event foreign result", "purchase_accounting_events_v1_no_update", "", true},
		{"event accounting totals", "purchase_accounting_events_v1_no_update", "", true},
		{"event outcomes missing", "purchase_accounting_events_v1_no_update", "", true},
		{"event outcomes unknown", "purchase_accounting_events_v1_no_update", "", true},
		{"event outcomes wrong readiness", "purchase_accounting_events_v1_no_update", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			if test.name == "event outcomes wrong readiness" {
				f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
				f.request.Evidence.Source = nil
				f.request.Evidence.HeldDate = nil
			}
			repo := f.repo(tx)
			response, err := repo.PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+test.trigger); err != nil {
				t.Fatal(err)
			}
			if test.alterResult {
				switch test.name {
				case "event accounting totals":
					response.Accounting.Paid = postMoney(11000)
					response.Accounting.Outstanding = postMoney(0)
				case "event outcomes missing":
					response.Accounting.ReviewOutcomes = nil
				case "event outcomes unknown":
					response.Accounting.ReviewOutcomes.Evidence = tammyv1.PurchaseReviewStatus(99)
				case "event outcomes wrong readiness":
					response.Accounting.ReviewOutcomes.Evidence = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
				default:
					response.Posting.DocumentId = "018f0000-0000-7000-8000-000000009999"
				}
				content, err := proto.MarshalOptions{Deterministic: true}.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(content)
				if _, err := tx.ExecContext(ctx, `UPDATE purchase_accounting_events_v1 SET result_proto=?,result_sha256=?`, content, hash[:]); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := tx.ExecContext(ctx, test.query); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.Get(ctx, postingOrg, f.document.Id); err == nil {
				t.Fatal("corrupt retained facts accepted")
			}
		})
	}
}

func TestPostReviewedPurchaseHistoricalReviewOutcomesSurviveClockAdvance(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	// A future-dated invoice and held date are retained separately from the
	// posting clock. Current readiness can change without rewriting its event.
	id, _ := f.id()
	key, _ := f.id()
	original := []byte("future invoice " + id)
	digest := sha256.Sum256(original)
	candidate := proto.Clone(f.document.Candidate).(*tammyv1.DocumentCandidate)
	candidate.InvoiceNumber = "FUTURE-1"
	candidate.DocumentDate = postDate(20)
	document := proto.Clone(f.document).(*tammyv1.Document)
	document.Id, document.Version = id, 1
	document.Status = tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW
	document.ReviewedAt = nil
	document.Candidate = candidate
	document.Sha256, document.ByteLength = digest[:], uint64(len(original))
	documentsRepo, _ := documents.NewRepository(tx)
	if _, err := documentsRepo.Create(ctx, key, testkit.ActorUserID, document, original); err != nil {
		t.Fatal(err)
	}
	reviewed, err := documentsRepo.SaveReview(ctx, id, 1, candidate, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.document = reviewed
	f.request.DocumentId = id
	f.request.Evidence.Source = &tammyv1.PurchaseRetainedSourceRef{SourceId: id, SourceVersion: 2, SourceHash: digest[:]}
	f.request.Evidence.HeldDate = postDate(20)
	before := time.Date(2026, 8, 19, 23, 59, 59, 999999999, time.UTC)
	after := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	clockCalls := 0
	repo, err := purchases.NewRepository(tx, func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return before
		}
		return after
	}, f.id)
	if err != nil {
		t.Fatal(err)
	}
	response, err := repo.PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Accounting.ReviewOutcomes.Timing != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW || response.Accounting.SuspenseGst.MinorUnits != 1000 {
		t.Fatalf("future-held initial outcome=%v", response.Accounting)
	}
	if !response.Posting.PostedAt.AsTime().Equal(before) {
		t.Fatal("posting did not retain the command's initial trusted time")
	}
	current, err := repo.Get(ctx, postingOrg, id)
	if err != nil {
		t.Fatal(err)
	}
	if current.ReviewOutcomes.Timing != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR || current.SuspenseGst.MinorUnits != 1000 || current.AttributedGst.MinorUnits != 0 {
		t.Fatalf("clock advance changed accounting or missed current readiness: %v", current)
	}
	if response.Accounting.ReviewOutcomes.Timing != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW {
		t.Fatal("historical event outcome was rewritten")
	}
}

func (f *postingFixture) seedPriorJournal(sourceID string, source tammyv1.JournalSource, debitAccount, creditAccount string, taxSource ...string) *tammyv1.Journal {
	f.t.Helper()
	ctx := context.Background()
	repo, _ := accounting.NewJournalRepository(f.executor)
	revision, err := repo.ReserveFinancialRevision(ctx, nil, f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	journalID, _ := f.id()
	debitID, _ := f.id()
	creditID, _ := f.id()
	j := &tammyv1.Journal{Id: journalID, OrganisationId: postingOrg, Version: 1, PostingDate: postDate(10), Memo: "Retained prior source", Source: source, State: tammyv1.JournalState_JOURNAL_STATE_POSTED, FinancialRevision: revision, PostedAt: timestamppb.New(f.now), TotalDebits: postMoney(11000), TotalCredits: postMoney(11000), Lines: []*tammyv1.JournalLine{{Id: debitID, JournalId: journalID, AccountId: debitAccount, Sequence: 1, Debit: postMoney(11000), Credit: postMoney(0)}, {Id: creditID, JournalId: journalID, AccountId: creditAccount, Sequence: 2, Debit: postMoney(0), Credit: postMoney(11000)}}}
	sourceType := "MANUAL"
	if source == tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION {
		sourceType = "OPENING"
	}
	flows := map[string][]accounting.CashFlowComponent{debitID: {{Category: accounting.CashFlowNoncash, AmountMinor: 11000}}, creditID: {{Category: accounting.CashFlowNoncash, AmountMinor: -11000}}}
	facts := map[string]accounting.TaxFact{}
	if len(taxSource) > 0 {
		codeID, _ := f.id()
		factID, _ := f.id()
		rule := &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: "018f0000-0000-7000-8000-000000001003", Revision: 1, ContentHash: make([]byte, 32)}
		code := &tammyv1.TaxCode{Id: codeID, Code: "GST", Treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, Rule: rule}
		j.Lines[0].TaxCodeId = proto.String(codeID)
		j.Lines[0].TaxAmount = postMoney(1000)
		j.Lines[0].TaxRule = rule
		fact, err := accounting.BuildNonCashTaxFact(factID, postingOrg, debitID, code, 10000, 1000, &tammyv1.SourceRef{Type: "document", Id: taxSource[0], Revision: 2, ContentHash: f.document.Sha256}, debitID)
		if err != nil {
			f.t.Fatal(err)
		}
		facts[debitID] = fact
	}
	if err := repo.Post(ctx, j, sourceType, sourceID, 1, facts, flows, f.now); err != nil {
		f.t.Fatal(err)
	}
	return j
}

func TestPostReviewedPurchasePriorJournalAndOpeningProvenanceBlocksRecognition(t *testing.T) {
	for _, kind := range []string{"journal header", "manual tax fact", "opening document", "replaced opening hash", "opening scalar corruption"} {
		t.Run(kind, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			var payableID string
			rows, err := tx.QueryContext(ctx, `SELECT id FROM accounts WHERE organisation_id=? AND code='2000'`, postingOrg)
			if err != nil {
				t.Fatal(err)
			}
			if !rows.Next() || rows.Scan(&payableID) != nil {
				t.Fatal("no payable")
			}
			rows.Close()
			if strings.HasPrefix(kind, "journal") {
				f.seedPriorJournal(f.document.Id, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
			} else if kind == "manual tax fact" {
				operation, _ := f.id()
				f.seedPriorJournal(operation, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense, f.document.Id)
			} else {
				conversionID, _ := f.id()
				itemID, _ := f.id()
				journal := f.seedPriorJournal(conversionID, tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION, postingExpense, payableID)
				state := "POSTED"
				sourceID := f.document.Id
				sourceHash := f.document.Sha256
				if kind == "replaced opening hash" {
					state = "REPLACED"
					sourceID = "018f0000-0000-7000-8000-000000009997"
					candidateBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(f.document.Candidate)
					digest := sha256.Sum256(candidateBytes)
					sourceHash = digest[:]
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO opening_conversions(id,organisation_id,conversion_date,state,source_sha256,version,created_at) VALUES(?,?,'2026-08-10','DRAFT',?,1,?)`, conversionID, postingOrg, strings.Repeat("a", 64), f.now.Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
				input := &tammyv1.OpeningBalanceInput{ClientLineId: itemID, AccountId: payableID, Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM, LedgerBalance: postMoney(11000), Source: &tammyv1.SourceRef{Type: "document", Id: sourceID, Revision: 2, ContentHash: sourceHash}, OriginalIssueDate: postDate(10), OriginalDueDate: postDate(20), OutstandingGst: postMoney(1000), PriorGstAttributed: postMoney(0)}
				gst := int64(1000)
				if kind == "opening scalar corruption" {
					gst = 999
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,source_type,source_id,source_revision,source_content_hash,original_issue_date,original_due_date,outstanding_gst_minor,prior_gst_attributed_minor) VALUES(?,?,?,'SUPPLIER_OPEN_ITEM',0,11000,'AUD','document',?,2,?,'2026-08-10','2026-08-20',?,0)`, itemID, conversionID, payableID, sourceID, sourceHash, gst); err != nil {
					t.Fatal(err)
				}
				opening, _ := purchases.NewOpeningRepository(tx)
				if err := opening.RecordOpeningPayable(ctx, conversionID, input, f.now); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE opening_conversions SET state='POSTED',journal_id=?,financial_revision=? WHERE id=?`, journal.Id, journal.FinancialRevision, conversionID); err != nil {
					t.Fatal(err)
				}
				if state == "REPLACED" {
					replacementID, _ := f.id()
					if _, err := tx.ExecContext(ctx, `INSERT INTO opening_conversions(id,organisation_id,conversion_date,state,source_sha256,version,created_at) VALUES(?,?,'2026-08-10','DRAFT',?,1,?)`, replacementID, postingOrg, strings.Repeat("b", 64), f.now.Format(time.RFC3339Nano)); err != nil {
						t.Fatal(err)
					}
					if _, err := tx.ExecContext(ctx, `UPDATE opening_conversions SET state='REPLACED',version=version+1,replaced_by_id=? WHERE id=?`, replacementID, conversionID); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`)
			repo := f.repo(tx)
			if _, err := repo.PostReviewed(ctx, f.request); err == nil {
				t.Fatal("previously accounted source accepted")
			}
			if got := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`); got != before {
				t.Fatal("blocked prior source reserved purchase revision")
			}
			if kind != "opening scalar corruption" {
				state, err := repo.Get(ctx, postingOrg, f.document.Id)
				if err != nil {
					t.Fatal(err)
				}
				if state.Posted || state.ReviewOutcomes.PriorAccounting != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY {
					t.Fatal("unposted prior source missing corrective review outcome")
				}
			}
		})
	}
}

func (f *postingFixture) seedLegacyDecision(sqlDisposition, payloadDisposition tammyv1.LegacyPurchaseDisposition, linked *string) {
	f.t.Helper()
	id, _ := f.id()
	input := &tammyv1.LegacyPurchaseReviewInput{Source: &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_Document{Document: &tammyv1.LegacyPurchaseDocumentRef{DocumentId: f.document.Id, ReviewedVersion: 2, OriginalContentHash: f.document.Sha256}}}, AffectedPeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}, ReportingBalanceDate: postDate(31), Disposition: payloadDisposition, LinkedPostingId: linked, Reason: "Retained reviewed legacy decision"}
	candidateBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(f.document.Candidate)
	if err != nil {
		f.t.Fatal(err)
	}
	snapshot := sha256.Sum256(candidateBytes)
	input.Source.GetDocument().ReviewedSnapshotHash = snapshot[:]
	review := &tammyv1.LegacyPurchaseReview{Id: id, OrganisationId: postingOrg, Review: input, SourceRevision: 1, ActorUserId: testkit.ActorUserID, RecordedAt: timestamppb.New(f.now)}
	preimage, err := proto.MarshalOptions{Deterministic: true}.Marshal(review)
	if err != nil {
		f.t.Fatal(err)
	}
	recordHash := sha256.Sum256(preimage)
	review.SourceHash = recordHash[:]
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(review)
	if err != nil {
		f.t.Fatal(err)
	}
	var linkedID any
	if linked != nil {
		linkedID = *linked
	}
	if _, err := f.executor.ExecContext(context.Background(), `INSERT INTO purchase_legacy_reviews_v1(id,organisation_id,source_kind,document_id,source_version,source_hash,reviewed_snapshot_hash,accounting_version,affected_period_start,affected_period_end,reporting_balance_date,disposition,linked_posting_id,reason,review_proto,record_hash,actor_user_id,created_at) VALUES(?,?,'DOCUMENT',?,2,?,?,1,'2026-08-01','2026-08-31','2026-08-31',?,?,?,?,?,?,?)`, id, postingOrg, f.document.Id, f.document.Sha256, snapshot[:], sqlDisposition, linkedID, input.Reason, content, review.SourceHash, testkit.ActorUserID, f.now.Format(time.RFC3339Nano)); err != nil {
		f.t.Fatal(err)
	}
}

func TestPostReviewedPurchaseLegacyDecisionsVerifyScalarsAndEvidenceOnlyLinks(t *testing.T) {
	for _, kind := range []string{"unpaid permits recognition", "duplicate blocks", "evidence only blocks", "disposition corruption"} {
		t.Run(kind, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			ctx := context.Background()
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			repo := f.repo(tx)
			disposition := tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE
			payload := disposition
			var linked *string
			if kind == "unpaid permits recognition" {
				disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE
				payload = disposition
			}
			if kind == "evidence only blocks" {
				first, err := repo.PostReviewed(ctx, f.request)
				if err != nil {
					t.Fatal(err)
				}
				linked = proto.String(first.Posting.Id)
				f.addDocument("Additional evidence", "EVIDENCE", 10000, 1000)
				disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE
				payload = disposition
			}
			if kind == "disposition corruption" {
				payload = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE
			}
			f.seedLegacyDecision(disposition, payload, linked)
			response, err := repo.PostReviewed(ctx, f.request)
			if kind == "unpaid permits recognition" {
				if err != nil || response == nil {
					t.Fatalf("reviewed unpaid document should permit explicit recognition: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("legacy duplicate/link/corruption permitted new recognition")
				}
			}
			if kind == "evidence only blocks" {
				state, err := repo.Get(ctx, postingOrg, f.document.Id)
				if err != nil {
					t.Fatal(err)
				}
				if state.ReviewOutcomes.Duplicate != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR || state.ReviewOutcomes.PriorAccounting != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY {
					t.Fatal("evidence-only link became transaction duplicate candidate")
				}
			}
		})
	}
}

func TestPostReviewedPurchasePriorDiscoveryFailsVisiblyAboveBound(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	for index := 0; index < 51; index++ {
		operation, _ := f.id()
		f.seedPriorJournal(operation, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense, f.document.Id)
	}
	before := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`)
	repo := f.repo(tx)
	if _, err := repo.PostReviewed(ctx, f.request); !errors.Is(err, purchases.ErrSourceBound) {
		t.Fatalf("51 source discovery did not fail visibly: %v", err)
	}
	if _, err := repo.Get(ctx, postingOrg, f.document.Id); !errors.Is(err, purchases.ErrSourceBound) {
		t.Fatalf("51 source projection did not fail visibly: %v", err)
	}
	if postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`) != before {
		t.Fatal("source bound reserved revision")
	}
}
