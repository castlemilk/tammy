//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package documents

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const documentPageOrgID = "018f0000-0000-7000-8000-000000000020"
const documentPageUserID = "018f0000-0000-7000-8000-000000000010"

type documentPageFixture struct {
	db         *sqlcipher.Database
	repository *Repository
	accounts   map[string]string
	next       int
	options    PageOptions
}

func newDocumentPageFixture(t *testing.T) *documentPageFixture {
	t.Helper()
	db := testkit.NewEncryptedWorkspaceAtVersion(t, 12).Database
	insertDocumentPageRow(t, db, "users", map[string]any{"id": documentPageUserID, "email": "owner@example.test", "display_name": "Owner", "status": "ACTIVE", "created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-10-01T00:00:00Z"})
	organisationRepo, _ := organisations.NewRepository(db)
	if err := organisationRepo.Create(context.Background(), &tammyv1.Organisation{Id: documentPageOrgID, Version: 1, Abn: "51824753556", LegalName: "Tammy Business Pty Ltd", DisplayName: "Tammy", EntityType: "AU_PRIVATE_COMPANY", GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH, GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY, FinancialYearEndMonth: 6, OwnerUserId: documentPageUserID, VerificationState: tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED, ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "rule_bundle", Id: "018f0000-0000-7000-8000-000000000030", Revision: 1, ContentHash: make([]byte, 32)}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	accountRepo, _ := accounting.NewAccountRepository(db)
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	if err := accountRepo.InstallTemplate(context.Background(), documentPageOrgID, template, time.Now()); err != nil {
		t.Fatal(err)
	}
	accounts, err := accountRepo.List(context.Background(), documentPageOrgID, "", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &documentPageFixture{db: db, accounts: make(map[string]string), next: 0x4000}
	for _, account := range accounts {
		fixture.accounts[account.Code] = account.Id
	}
	expense := &tammyv1.Account{Id: fixture.id(), OrganisationId: documentPageOrgID, Version: 1, Code: "5001", Name: "Expense", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "operating"}
	if err := accountRepo.Create(context.Background(), expense, time.Now()); err != nil {
		t.Fatal(err)
	}
	fixture.accounts[expense.Code] = expense.Id
	fixture.repository, _ = NewRepository(db)
	codec, err := paging.NewCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	fixture.options = PageOptions{Limit: 100, ReviewAsOf: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), EncodeCursor: func(position string) (string, error) {
		return codec.Encode(paging.Cursor{Snapshot: "documents-v1", Position: position, QueryHash: sha256.Sum256([]byte("fixture"))})
	}}
	return fixture
}

func (fixture *documentPageFixture) id() string {
	fixture.next++
	return fmt.Sprintf("018f0000-0000-7000-8000-%012x", fixture.next)
}

func (fixture *documentPageFixture) document(t *testing.T, reviewed bool, gross, gst int64, text string) *tammyv1.Document {
	return fixture.documentAt(t, reviewed, gross, gst, text, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
}

func (fixture *documentPageFixture) documentAt(t *testing.T, reviewed bool, gross, gst int64, text string, createdAt time.Time) *tammyv1.Document {
	t.Helper()
	id := fixture.id()
	original := []byte(id)
	digest := sha256.Sum256(original)
	document := &tammyv1.Document{Id: id, OrganisationId: documentPageOrgID, Version: 1, Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW,
		SourceDisplayName: id + ".pdf", MimeType: "application/pdf", ByteLength: uint64(len(original)), Sha256: digest[:], ExtractedText: text,
		Candidate: &tammyv1.DocumentCandidate{SupplierName: "Supplier", DocumentDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1}, Subtotal: audMoney(gross - gst), Gst: audMoney(gst), Total: audMoney(gross)}, CreatedAt: timestamppb.New(createdAt)}
	saved, err := fixture.repository.Create(context.Background(), fixture.id(), documentPageUserID, document, original)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed {
		saved, err = fixture.repository.SaveReview(context.Background(), id, 1, saved.Candidate, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
	}
	return saved
}

func TestDocumentPageChronologicalNanosecondsAndSameSecondIngestion(t *testing.T) {
	fixture := newDocumentPageFixture(t)
	instant := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	whole := fixture.documentAt(t, false, 110, 10, "", instant)
	tenth := fixture.documentAt(t, false, 110, 10, "", instant.Add(100*time.Millisecond))
	oneNanoLater := fixture.documentAt(t, false, 110, 10, "", instant.Add(100*time.Millisecond+time.Nanosecond))
	latest := fixture.documentAt(t, false, 110, 10, "", instant.Add(110*time.Millisecond))
	options := fixture.options
	options.Limit = 3
	first := fixture.page(t, options)
	want := []*tammyv1.Document{latest, oneNanoLater, tenth}
	if len(first.Documents) != len(want) || first.NextPosition == "" {
		t.Fatalf("first fractional page=%v", first)
	}
	for index, document := range first.Documents {
		if document.Id != want[index].Id || !proto.Equal(document.CreatedAt, want[index].CreatedAt) {
			t.Fatalf("fractional page row %d=%s at %v, want %s at %v", index, document.Id, document.CreatedAt, want[index].Id, want[index].CreatedAt)
		}
	}
	newer := fixture.documentAt(t, false, 110, 10, "", instant.Add(110*time.Millisecond+time.Nanosecond))
	options.Position = first.NextPosition
	last := fixture.page(t, options)
	if len(last.Documents) != 1 || last.Documents[0].Id != whole.Id || last.NextPosition != "" {
		t.Fatalf("same-second ingestion changed retained continuation: %v", last)
	}
	options.Position = ""
	refreshed := fixture.page(t, options)
	if len(refreshed.Documents) != 3 || refreshed.Documents[0].Id != newer.Id || refreshed.Documents[1].Id != latest.Id || refreshed.Documents[2].Id != oneNanoLater.Id {
		t.Fatalf("refresh missed chronological ingestion: %v", refreshed)
	}
}

func (fixture *documentPageFixture) page(t *testing.T, options PageOptions) DocumentPage {
	t.Helper()
	page, err := fixture.repository.ListPage(context.Background(), documentPageOrgID, options)
	if err != nil {
		t.Fatal(err)
	}
	response := &tammyv1.ListDocumentsResponse{Documents: page.Documents, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(page.Documents))}}
	if page.NextPosition != "" {
		cursor, err := options.EncodeCursor(page.NextPosition)
		if err != nil {
			t.Fatal(err)
		}
		response.Page.NextCursor = &cursor
	}
	frame, err := anypb.New(response)
	if err != nil || proto.Size(frame) > MaximumListResponseBytes {
		t.Fatalf("page frame=%d error=%v", proto.Size(frame), err)
	}
	return page
}

func TestDocumentPageRetainsTiedAnchorsAndRejectsMismatchedStoredKey(t *testing.T) {
	fixture := newDocumentPageFixture(t)
	var retained []*tammyv1.Document
	for index := 0; index < 205; index++ {
		retained = append(retained, fixture.document(t, false, 110, 10, ""))
	}
	options := fixture.options
	seen := make(map[string]bool)
	for pageNumber, want := range []int{100, 100, 5} {
		page := fixture.page(t, options)
		if len(page.Documents) != want {
			t.Fatalf("page %d=%d want=%d", pageNumber, len(page.Documents), want)
		}
		for _, document := range page.Documents {
			if seen[document.Id] {
				t.Fatalf("duplicate anchor %s", document.Id)
			}
			seen[document.Id] = true
		}
		if pageNumber < 2 && page.NextPosition == "" {
			t.Fatal("missing continuation")
		}
		if pageNumber == 2 && page.NextPosition != "" {
			t.Fatal("final page has continuation")
		}
		options.Position = page.NextPosition
	}
	if len(seen) != 205 {
		t.Fatalf("traversed %d retained sources", len(seen))
	}
	options.Position = encodeDocumentPosition(documentPageOrgID, retained[0])
	if page := fixture.page(t, options); len(page.Documents) != 0 || page.NextPosition != "" {
		t.Fatal("last retained anchor did not yield empty terminal page")
	}
	for _, position := range []string{
		base64.RawURLEncoding.EncodeToString([]byte("document-page-v1|" + documentPageOrgID + "|2026-10-01T00:00:01Z|" + retained[100].Id)),
		base64.RawURLEncoding.EncodeToString([]byte("document-page-v1|" + documentPageOrgID + "|2026-10-01T00:00:00Z|018f0000-0000-7000-8000-000000000999")),
	} {
		options.Position = position
		if _, err := fixture.repository.ListPage(context.Background(), documentPageOrgID, options); !errors.Is(err, ErrInvalidPage) {
			t.Fatalf("mismatched retained anchor accepted: %v", err)
		}
	}
	options.Position = ""
	for _, limit := range []int{-1, 0, 201} {
		options.Limit = limit
		if _, err := fixture.repository.ListPage(context.Background(), documentPageOrgID, options); !errors.Is(err, ErrInvalidPage) {
			t.Fatalf("invalid limit %d accepted: %v", limit, err)
		}
	}
}

func TestDocumentPageEmptyInitialAndChangedFilteredContinuation(t *testing.T) {
	fixture := newDocumentPageFixture(t)
	options := fixture.options
	assertEmpty := func(page DocumentPage) {
		t.Helper()
		if len(page.Documents) != 0 || page.NextPosition != "" {
			t.Fatalf("empty page retained documents or continuation: %v", page)
		}
	}
	assertEmpty(fixture.page(t, options))
	var retained []*tammyv1.Document
	for index := 0; index < 3; index++ {
		retained = append(retained, fixture.document(t, false, 110, 10, ""))
	}
	options.Status = tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED
	assertEmpty(fixture.page(t, options))
	options.Status = tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW
	options.Limit = 1
	first := fixture.page(t, options)
	if len(first.Documents) != 1 || first.Documents[0].Id != retained[2].Id || first.NextPosition == "" {
		t.Fatalf("filtered first page did not retain its anchor: %v", first)
	}
	// Reviewing the remaining documents changes filter membership, while the
	// retained first-page anchor remains valid for an empty continuation.
	for _, document := range retained[:2] {
		if _, err := fixture.repository.SaveReview(context.Background(), document.Id, document.Version, document.Candidate, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	options.Position = first.NextPosition
	assertEmpty(fixture.page(t, options))
	options.Position = ""
	refreshed := fixture.page(t, options)
	if len(refreshed.Documents) != 1 || refreshed.Documents[0].Id != retained[2].Id || refreshed.NextPosition != "" {
		t.Fatalf("refresh returned incorrect current filter membership: %v", refreshed)
	}
	options.Accounting = tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_NEEDS_GST_REVIEW
	options.ReviewAsOf = time.Time{}
	if _, err := fixture.repository.ListPage(context.Background(), documentPageOrgID, options); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("review filter accepted absent trusted as-of: %v", err)
	}
}

func TestDocumentPageByteBoundaryUsesLastReturnedAnchor(t *testing.T) {
	fixture := newDocumentPageFixture(t)
	for index := 0; index < 11; index++ {
		fixture.document(t, false, 110, 10, strings.Repeat("界", 1024*1024/3))
	}
	options := fixture.options
	options.Limit = 200
	seen := map[string]bool{}
	for {
		page := fixture.page(t, options)
		if len(page.Documents) == 0 || len(page.Documents) > 3 {
			t.Fatalf("byte page count=%d", len(page.Documents))
		}
		for _, document := range page.Documents {
			if seen[document.Id] {
				t.Fatal("duplicate at byte boundary")
			}
			seen[document.Id] = true
		}
		if page.NextPosition == "" {
			break
		}
		_, id, err := decodeDocumentPosition(page.NextPosition, documentPageOrgID)
		if err != nil || id != page.Documents[len(page.Documents)-1].Id {
			t.Fatal("next anchor skips an unreturned document")
		}
		options.Position = page.NextPosition
	}
	if len(seen) != 11 {
		t.Fatalf("byte pages returned %d sources", len(seen))
	}
}

func TestDocumentPageReviewAndAccountingFiltersUseOwnedRetainedSources(t *testing.T) {
	fixture := newDocumentPageFixture(t)
	unreviewed := fixture.document(t, false, 110, 10, "")
	unposted := fixture.document(t, true, 110, 10, "")
	zeroUnknown := fixture.document(t, false, 0, 0, "")
	candidate := proto.Clone(zeroUnknown.Candidate).(*tammyv1.DocumentCandidate)
	candidate.DocumentDate = nil
	var err error
	zeroUnknown, err = fixture.repository.SaveReview(context.Background(), zeroUnknown.Id, 1, candidate, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	zeroPosted := fixture.document(t, true, 0, 0, "")
	fixture.posting(t, zeroPosted, 0, "", 2)
	private := fixture.document(t, true, 110, 10, "")
	fixture.posting(t, private, 0, "", 2)
	normal := fixture.document(t, true, 110, 10, "")
	normalPosting := fixture.posting(t, normal, 10, "1150", 2)
	fixture.evidence(t, normalPosting, normal, 2, 1)
	cashUnpaid := fixture.document(t, true, 110, 10, "")
	cashPosting := fixture.posting(t, cashUnpaid, 10, "1160", 1)
	fixture.evidence(t, cashPosting, cashUnpaid, 2, 1)
	unknown := fixture.document(t, true, 110, 10, "")
	unknownPosting := fixture.posting(t, unknown, 10, "1160", 1)
	fixture.evidence(t, unknownPosting, unknown, 1, 1)
	late := fixture.document(t, true, 110, 10, "")
	latePosting := fixture.posting(t, late, 10, "1170", 2)
	fixture.evidence(t, latePosting, late, 2, 1)
	unsupported := fixture.document(t, true, 110, 10, "")
	unsupportedPosting := fixture.posting(t, unsupported, 10, "1160", 1)
	fixture.evidence(t, unsupportedPosting, unsupported, 3, 1)
	futureHeld := fixture.document(t, true, 110, 10, "")
	futurePosting := fixture.posting(t, futureHeld, 10, "1160", 1)
	fixture.evidenceHeld(t, futurePosting, futureHeld, 2, 1, "9999-12-31")
	// Period-specific noncreditable review does not globally resolve this source.
	insertDocumentPageRow(t, fixture.db, "purchase_legacy_reviews_v1", map[string]any{
		"id": fixture.id(), "organisation_id": documentPageOrgID, "source_kind": "DOCUMENT", "document_id": unposted.Id,
		"source_version": unposted.Version, "source_hash": unposted.Sha256, "reviewed_snapshot_hash": make([]byte, 32), "accounting_version": 1,
		"affected_period_start": "2026-10-01", "affected_period_end": "2026-12-31", "reporting_balance_date": "2026-12-31", "disposition": 4,
		"reason": "Period-specific reviewed decision", "review_proto": []byte{1}, "record_hash": make([]byte, 32), "actor_user_id": documentPageUserID, "created_at": "2026-10-07T00:00:00Z",
	})
	// A manually posted suspense journal is not owned by any purchase.
	fixture.journal(t, 0, fixture.id(), map[string]int64{"1170": 999, "2000": -999})
	assert := func(status tammyv1.DocumentStatus, filter tammyv1.DocumentAccountingFilter, want ...string) {
		t.Helper()
		options := fixture.options
		options.Limit = 2
		options.Status = status
		options.Accounting = filter
		seen := map[string]bool{}
		for {
			page := fixture.page(t, options)
			for _, document := range page.Documents {
				if seen[document.Id] {
					t.Fatal("filtered pagination duplicated a document")
				}
				seen[document.Id] = true
			}
			if page.NextPosition == "" {
				break
			}
			options.Position = page.NextPosition
		}
		if len(seen) != len(want) {
			t.Fatalf("status=%s accounting=%s membership=%v want=%v", status, filter, seen, want)
		}
		for _, id := range want {
			if !seen[id] {
				t.Fatalf("missing filtered document %s", id)
			}
		}
	}
	assert(tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, 0, unreviewed.Id)
	assert(0, tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_UNPOSTED, unreviewed.Id, unposted.Id, zeroUnknown.Id)
	assert(0, tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_POSTED, zeroPosted.Id, private.Id, normal.Id, cashUnpaid.Id, unknown.Id, late.Id, unsupported.Id, futureHeld.Id)
	assert(0, tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_NEEDS_GST_REVIEW, unposted.Id, zeroUnknown.Id, unknown.Id, late.Id, unsupported.Id, futureHeld.Id)
	// Latest reviewed evidence controls current evidence status, but cannot erase
	// historic suspense. The ordinary normal journal still needs no attribution row.
	fixture.evidence(t, normalPosting, normal, 1, 2)
	assert(0, tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_NEEDS_GST_REVIEW, unposted.Id, zeroUnknown.Id, normal.Id, unknown.Id, late.Id, unsupported.Id, futureHeld.Id)
}

func (fixture *documentPageFixture) posting(t *testing.T, document *tammyv1.Document, eligible int64, control string, basis int) string {
	t.Helper()
	id := fixture.id()
	gross, supplier := document.Candidate.Total.MinorUnits, document.Candidate.Gst.MinorUnits
	var journalID any
	if gross > 0 {
		lines := map[string]int64{"5001": gross - eligible, "2000": -gross}
		if eligible > 0 {
			lines[control] = eligible
		}
		journalID = fixture.journal(t, 6, id, lines)
	}
	insertDocumentPageRow(t, fixture.db, "purchase_postings_v1", map[string]any{
		"id": id, "organisation_id": documentPageOrgID, "document_id": document.Id, "reviewed_version": document.Version,
		"original_content_hash": document.Sha256, "reviewed_snapshot_hash": make([]byte, 32), "reviewed_source_proto": []byte{1},
		"supplier_name": "Supplier", "invoice_number": "", "invoice_date": "2026-10-01", "gst_basis": basis, "gst_profile_version": 1, "gst_history_watermark": 0,
		"gross_minor": gross, "supplier_gst_minor": supplier, "eligible_gst_minor": eligible, "currency_code": "AUD", "journal_id": journalID, "financial_revision": 1,
		"duplicate_decision": 1, "duplicate_reason": "", "prior_accounting_decision": 1, "prior_accounting_reason": "Reviewed", "normalized_supplier": "supplier", "normalized_invoice": "",
		"source_hash": make([]byte, 32), "posting_proto": []byte{1}, "actor_user_id": documentPageUserID, "created_at": "2026-10-02T00:00:00Z",
	})
	for sequence := 1; sequence <= 2; sequence++ {
		portion := func(amount int64) int64 {
			if sequence == 1 {
				return amount / 2
			}
			return amount - amount/2
		}
		insertDocumentPageRow(t, fixture.db, "purchase_posting_allocations_v1", map[string]any{"posting_id": id, "organisation_id": documentPageOrgID, "sequence": sequence, "account_id": fixture.accounts["5001"], "account_version": 1, "gross_minor": portion(gross), "supplier_gst_minor": portion(supplier), "eligible_gst_minor": portion(eligible), "use_kind": 1, "bas_classification": 1, "cash_flow_classification": 1, "noncreditable_reason": "Reviewed", "allocation_proto": []byte{1}})
	}
	return id
}

func (fixture *documentPageFixture) evidence(t *testing.T, postingID string, document *tammyv1.Document, classification, version int) string {
	return fixture.evidenceHeld(t, postingID, document, classification, version, "2026-10-01")
}

func (fixture *documentPageFixture) evidenceHeld(t *testing.T, postingID string, document *tammyv1.Document, classification, version int, heldDate string) string {
	t.Helper()
	id := fixture.id()
	row := map[string]any{"id": id, "organisation_id": documentPageOrgID, "posting_id": postingID, "document_id": document.Id, "accounting_version": version, "classification": classification,
		"registration_review_confirmed": 1, "business_use_review_confirmed": 1, "evidence_review_confirmed": 1, "reason": "Reviewed", "evidence_proto": []byte{1}, "record_hash": make([]byte, 32), "actor_user_id": documentPageUserID, "created_at": "2026-10-02T00:00:00Z"}
	if classification != 1 {
		row["source_document_id"] = document.Id
		row["source_version"] = document.Version
		row["source_hash"] = document.Sha256
		row["held_date"] = heldDate
	}
	insertDocumentPageRow(t, fixture.db, "purchase_tax_evidence_v1", row)
	return id
}

func (fixture *documentPageFixture) journal(t *testing.T, kind int, sourceID string, balances map[string]int64) string {
	t.Helper()
	id := fixture.id()
	sourceType := map[int]string{0: "MANUAL", 6: "PAYABLE", 7: "BANKING", 8: "TAX"}[kind]
	var total int64
	for _, amount := range balances {
		if amount > 0 {
			total += amount
		}
	}
	insertDocumentPageRow(t, fixture.db, "journals", map[string]any{"id": id, "organisation_id": documentPageOrgID, "source_type": sourceType, "source_kind": kind, "source_id": sourceID, "source_revision": 1, "state": "DRAFT", "journal_date": "2026-10-01", "description": "Owned navigation fixture", "total_debits_minor": total, "total_credits_minor": total, "currency_code": "AUD", "financial_revision": 1, "created_at": "2026-10-02T00:00:00Z"})
	sequence := 0
	for code, amount := range balances {
		if amount == 0 {
			continue
		}
		sequence++
		debit, credit := amount, int64(0)
		if amount < 0 {
			debit, credit = 0, -amount
		}
		insertDocumentPageRow(t, fixture.db, "journal_lines", map[string]any{"id": fixture.id(), "journal_id": id, "line_number": sequence, "account_id": fixture.accounts[code], "debit_minor": debit, "credit_minor": credit, "currency_code": "AUD"})
	}
	if _, err := fixture.db.ExecContext(context.Background(), `UPDATE journals SET state='POSTED',posted_at='2026-10-02T00:00:00Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertDocumentPageRow(t *testing.T, executor app.CommandSQLExecutor, table string, row map[string]any) {
	t.Helper()
	columns := make([]string, 0, len(row))
	for column := range row {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	arguments := make([]any, 0, len(columns))
	placeholders := make([]string, len(columns))
	for index, column := range columns {
		arguments = append(arguments, row[column])
		placeholders[index] = "?"
	}
	if _, err := executor.ExecContext(context.Background(), "INSERT INTO "+table+"("+strings.Join(columns, ",")+") VALUES("+strings.Join(placeholders, ",")+")", arguments...); err != nil {
		t.Fatalf("insert %s: %v", table, err)
	}
}
