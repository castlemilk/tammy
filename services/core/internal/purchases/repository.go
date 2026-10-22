package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var (
	ErrInvalidCommand  = errors.New("purchases: invalid command context or scope")
	ErrRepository      = errors.New("purchases: repository failure or corrupt retained facts")
	ErrNotFound        = errors.New("purchases: document not found in organisation")
	ErrConflict        = errors.New("purchases: stale or already posted source")
	ErrPriorAccounting = errors.New("purchases: source has prior accounting or evidence-only provenance; corrective review required")
	ErrSourceBound     = errors.New("purchases: discovered source count exceeds supported review bound")
)

// Repository uses the caller's encrypted command transaction. The caller owns
// election, commit/rollback, audit and durable command-result replay.
type Repository struct {
	executor            app.CommandSQLExecutor
	now                 func() time.Time
	newID               func() (string, error)
	reportingRootOwners bool
}

func NewRepository(executor app.CommandSQLExecutor, now func() time.Time, newID func() (string, error)) (*Repository, error) {
	if executor == nil || now == nil || newID == nil {
		return nil, ErrRepository
	}
	return &Repository{executor: executor, now: now, newID: newID}, nil
}

func (r *Repository) document(ctx context.Context, org, id string) (*tammyv1.Document, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(id) {
		return nil, ErrRepository
	}
	if r.reportingRootOwners {
		return r.reportingDocument(ctx, org, id)
	}
	repo, _ := documents.NewRepository(r.executor)
	doc, err := repo.Get(ctx, id)
	if errors.Is(err, documents.ErrDocumentNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if doc.OrganisationId != org {
		return nil, ErrNotFound
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT original_bytes FROM documents WHERE id=? AND organisation_id=?`, id, org)
	if err != nil {
		return nil, ErrRepository
	}
	var content []byte
	if err := (purchaseRevisionRow{rows: rows}).Scan(&content); err != nil {
		return nil, ErrRepository
	}
	hash := sha256.Sum256(content)
	if !bytes.Equal(hash[:], doc.Sha256) || uint64(len(content)) != doc.ByteLength {
		return nil, ErrRepository
	}
	return doc, nil
}

func (r *Repository) Get(ctx context.Context, org, id string) (*tammyv1.PurchaseAccounting, error) {
	return r.get(ctx, org, id, false)
}
func (r *Repository) get(ctx context.Context, org, id string, pendingInitialEvent bool) (*tammyv1.PurchaseAccounting, error) {
	if r == nil || r.now == nil {
		return nil, ErrRepository
	}
	return r.getAt(ctx, org, id, pendingInitialEvent, r.now().UTC())
}
func (r *Repository) getAt(ctx context.Context, org, id string, pendingInitialEvent bool, readAsOf time.Time) (*tammyv1.PurchaseAccounting, error) {
	doc, err := r.document(ctx, org, id)
	if err != nil {
		return nil, err
	}
	value := &tammyv1.PurchaseAccounting{OrganisationId: org, DocumentId: id, Paid: purchaseJournalMoney(0), Outstanding: proto.Clone(doc.Candidate.Total).(*tammyv1.Money), EligibleGst: purchaseJournalMoney(0), DeferredGst: purchaseJournalMoney(0), SuspenseGst: purchaseJournalMoney(0), AttributedGst: purchaseJournalMoney(0), ReviewOutcomes: clearPurchaseOutcomes()}
	posting, err := r.loadPosting(ctx, org, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Reporting replays only posting roots already retained by capture.
		// A missing row is drift, so no unposted fallback can be accepted.
		if r.reportingRootOwners {
			return nil, ErrRepository
		}
		matches, err := r.duplicateCandidates(ctx, doc)
		if err != nil {
			return nil, err
		}
		if len(matches) > 0 {
			value.ReviewOutcomes.Duplicate = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
		}
		prior, err := r.priorSources(ctx, doc)
		if err != nil {
			return nil, err
		}
		if len(prior) > 0 {
			value.ReviewOutcomes.PriorAccounting = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
			value.ReviewOutcomes.Legacy = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
		}
		if doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED {
			value.ReviewOutcomes.Evidence = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE
		}
		return value, nil
	}
	if err != nil {
		return nil, err
	}
	if posting.ReviewedVersion != doc.Version || !bytes.Equal(posting.OriginalContentHash, doc.Sha256) {
		return nil, ErrRepository
	}
	currentReviewed, err := purchaseBytes(doc.Candidate)
	if err != nil {
		return nil, err
	}
	currentDigest := sha256.Sum256(currentReviewed)
	if !bytes.Equal(currentDigest[:], posting.ReviewedSnapshotHash) {
		return nil, ErrRepository
	}
	value.Posted = true
	value.Posting = posting
	value.AccountingVersion = 1
	value.Outstanding = proto.Clone(posting.Gross).(*tammyv1.Money)
	value.EligibleGst = proto.Clone(posting.EligibleGst).(*tammyv1.Money)
	evidence, err := r.retainedEvidence(ctx, posting)
	if err != nil {
		return nil, err
	}
	if evidence == nil {
		return nil, ErrRepository
	}
	asOf := posting.PostedAt.AsTime().UTC()
	if !pendingInitialEvent {
		asOf = readAsOf
	}
	value.ReviewOutcomes, err = r.evidenceOutcomes(ctx, posting, evidence.Review, asOf)
	if err != nil {
		return nil, err
	}
	// Claims follow the owned ledger movement. Later evidence cannot turn an
	// original suspense balance into a claim without an explicit release journal.
	if posting.JournalId != nil {
		var journal *tammyv1.Journal
		if r.reportingRootOwners {
			proof, err := r.reportingJournalProof(ctx, org, *posting.JournalId)
			if err != nil {
				return nil, err
			}
			if proof.SourceID != posting.Id || proof.SourceRevision != 1 {
				return nil, ErrRepository
			}
			journal = proof.Journal
		} else {
			journal, err = r.reportingJournal(ctx, org, *posting.JournalId)
			if err != nil {
				return nil, err
			}
		}
		if journal.OrganisationId != org || journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE || journal.FinancialRevision != posting.FinancialRevision {
			return nil, ErrRepository
		}
		if !r.reportingRootOwners {
			rows, err := r.executor.QueryContext(ctx, `SELECT source_id,source_revision FROM journals WHERE id=? AND organisation_id=?`, journal.Id, org)
			if err != nil {
				return nil, ErrRepository
			}
			var sourceID string
			var revision uint64
			if (purchaseRevisionRow{rows: rows}).Scan(&sourceID, &revision) != nil || sourceID != posting.Id || revision != 1 {
				return nil, ErrRepository
			}
		}
		rows, err := r.executor.QueryContext(ctx, `SELECT a.code,SUM(l.debit_minor-l.credit_minor) FROM journal_lines l JOIN accounts a ON a.id=l.account_id AND a.organisation_id=? WHERE l.journal_id=? AND a.code IN ('1150','1160','1170') GROUP BY a.code`, org, journal.Id)
		if err != nil {
			return nil, ErrRepository
		}
		for rows.Next() {
			var code string
			var amount int64
			if rows.Scan(&code, &amount) != nil || amount < 0 {
				_ = rows.Close()
				return nil, ErrRepository
			}
			switch code {
			case "1150":
				value.AttributedGst.MinorUnits = amount
			case "1160":
				value.DeferredGst.MinorUnits = amount
			case "1170":
				value.SuspenseGst.MinorUnits = amount
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, ErrRepository
		}
		if value.AttributedGst.MinorUnits+value.DeferredGst.MinorUnits+value.SuspenseGst.MinorUnits != posting.EligibleGst.MinorUnits {
			return nil, ErrRepository
		}
	}
	if err := r.verifyPaymentHistory(ctx, posting, pendingInitialEvent, value); err != nil {
		return nil, err
	}
	// Public readiness follows the supplied trusted clock; historical events
	// were compared in full at each event's own recorded timestamp above.
	if err := r.refreshClaimOutcomes(ctx, value, asOf); err != nil {
		return nil, err
	}
	return value, nil
}

func clearPurchaseOutcomes() *tammyv1.PurchaseAccountingReviewOutcomes {
	clear := tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	return &tammyv1.PurchaseAccountingReviewOutcomes{Evidence: clear, Timing: clear, Duplicate: clear, PriorAccounting: clear, Legacy: clear}
}

func purchaseEvidenceOutcomes(evidence *tammyv1.PurchaseEvidenceInput, eventDate *tammyv1.CivilDate, asOf time.Time, rule *ReviewedExceptionRule) (*tammyv1.PurchaseAccountingReviewOutcomes, error) {
	if asOf.IsZero() {
		return nil, ErrRepository
	}
	readiness, err := EvidenceReadinessFor(evidence, eventDate, purchaseAsOf(asOf.UTC()), rule)
	if err != nil {
		return nil, err
	}
	outcomes := clearPurchaseOutcomes()
	switch readiness {
	case EvidenceMissing:
		outcomes.Evidence = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE
	case EvidenceFuture, EvidenceTimingReviewRequired:
		outcomes.Timing = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW
	case EvidenceUnsupportedException:
		outcomes.Evidence = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
	}
	return outcomes, nil
}

func purchaseAsOf(now time.Time) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: int32(now.Year()), Month: int32(now.Month()), Day: int32(now.Day())}
}
func purchaseDateString(date *tammyv1.CivilDate) string {
	if !validPurchaseDate(date) || date.Year < 1900 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", date.Year, date.Month, date.Day)
}
func purchaseNullable(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
func purchaseBytes(message proto.Message) ([]byte, error) {
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil || len(content) == 0 || len(content) > 1<<20 {
		return nil, ErrRepository
	}
	return content, nil
}
func purchaseHash(message proto.Message) ([]byte, error) {
	copy := proto.Clone(message)
	field := copy.ProtoReflect().Descriptor().Fields().ByName("source_hash")
	if field == nil {
		return nil, ErrRepository
	}
	copy.ProtoReflect().Clear(field)
	content, err := purchaseBytes(copy)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(content)
	return hash[:], nil
}
func verifyPurchaseHash(message proto.Message, hash []byte) bool {
	digest, err := purchaseHash(message)
	return err == nil && len(hash) == 32 && bytes.Equal(digest, hash)
}

func (r *Repository) loadPosting(ctx context.Context, org, docID string) (*tammyv1.PurchasePosting, error) {
	sqlText := `SELECT posting_proto,source_hash,reviewed_source_proto,reviewed_snapshot_hash FROM purchase_postings_v1 WHERE organisation_id=? AND document_id=?`
	args := []any{org, docID}
	if r.reportingRootOwners {
		sqlText, args = postingReportingReadSQL(purchaseReportingPostingReplayQuery(org, docID))
	}
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, ErrRepository
	}
	var content, hash, reviewed, reviewedHash []byte
	destinations := []any{&content, &hash, &reviewed, &reviewedHash}
	var invalidOwner int64
	if r.reportingRootOwners {
		destinations = append([]any{&invalidOwner}, destinations...)
	}
	if err := (purchaseRevisionRow{rows: rows}).Scan(destinations...); err != nil {
		return nil, err
	}
	if invalidOwner != 0 {
		return nil, ErrRepository
	}
	p := &tammyv1.PurchasePosting{}
	if proto.Unmarshal(content, p) != nil || !verifyPurchaseHash(p, hash) || !bytes.Equal(p.SourceHash, hash) || p.OrganisationId != org || p.DocumentId != docID || !ids.IsCanonicalV7(p.Id) || p.ReviewedVersion == 0 || p.ReviewedVersion > math.MaxInt64 || p.GstProfileVersion == 0 || p.GstProfileVersion > math.MaxInt64 || p.GstHistoryWatermark > math.MaxInt64 || p.FinancialRevision == 0 || p.FinancialRevision > math.MaxInt64 || !validPurchaseMoney(p.Gross) || !validPurchaseMoney(p.SupplierGst) || !validPurchaseMoney(p.EligibleGst) || p.PostedAt == nil || p.PostedAt.CheckValid() != nil || !ids.IsCanonicalV7(p.ActorUserId) {
		return nil, ErrRepository
	}
	if !canonicalPurchaseBytes(p, content) {
		return nil, ErrRepository
	}
	candidate := &tammyv1.DocumentCandidate{}
	if proto.Unmarshal(reviewed, candidate) != nil {
		return nil, ErrRepository
	}
	canonical, err := purchaseBytes(candidate)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	if !bytes.Equal(canonical, reviewed) || !bytes.Equal(digest[:], reviewedHash) || !bytes.Equal(reviewedHash, p.ReviewedSnapshotHash) || candidate.SupplierName != p.SupplierName || candidate.InvoiceNumber != p.InvoiceNumber || !proto.Equal(candidate.DocumentDate, p.InvoiceDate) || !proto.Equal(candidate.Total, p.Gross) || !proto.Equal(candidate.Gst, p.SupplierGst) {
		return nil, ErrRepository
	}
	supplier, invoice, err := NormalizeDuplicateIdentity(p.SupplierName, p.InvoiceNumber)
	if err != nil {
		return nil, ErrRepository
	}
	rows, err = r.executor.QueryContext(ctx, `SELECT count(*) FROM purchase_postings_v1 WHERE id=? AND organisation_id=? AND document_id=? AND reviewed_version=? AND original_content_hash=? AND reviewed_snapshot_hash=? AND supplier_name=? AND supplier_identity IS ? AND invoice_number=? AND invoice_date=? AND gst_basis=? AND gst_profile_version=? AND gst_history_watermark=? AND gross_minor=? AND supplier_gst_minor=? AND eligible_gst_minor=? AND currency_code='AUD' AND journal_id IS ? AND financial_revision=? AND duplicate_decision=? AND duplicate_reason=? AND prior_accounting_decision=? AND prior_accounting_reason=? AND normalized_supplier=? AND normalized_invoice=? AND source_hash=? AND actor_user_id=? AND created_at=?`, p.Id, org, docID, p.ReviewedVersion, p.OriginalContentHash, p.ReviewedSnapshotHash, p.SupplierName, purchaseNullable(p.SupplierIdentity), p.InvoiceNumber, purchaseDateString(p.InvoiceDate), p.GstBasis, p.GstProfileVersion, p.GstHistoryWatermark, p.Gross.MinorUnits, p.SupplierGst.MinorUnits, p.EligibleGst.MinorUnits, purchaseNullable(p.JournalId), p.FinancialRevision, p.GetDuplicateReview().GetDecision(), p.GetDuplicateReview().GetReason(), p.GetPriorAccountingReview().GetDecision(), p.GetPriorAccountingReview().GetReason(), supplier, invoice, p.SourceHash, p.ActorUserId, p.PostedAt.AsTime().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, ErrRepository
	}
	var count int
	if (purchaseRevisionRow{rows: rows}).Scan(&count) != nil || count != 1 {
		return nil, ErrRepository
	}
	if err := r.verifyAllocations(ctx, p); err != nil {
		return nil, err
	}
	doc, err := r.document(ctx, org, docID)
	if err != nil {
		return nil, err
	}
	if doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || doc.Version != p.ReviewedVersion || !bytes.Equal(doc.Sha256, p.OriginalContentHash) || !proto.Equal(doc.Candidate, candidate) {
		return nil, ErrRepository
	}
	return p, nil
}

func (r *Repository) verifyAllocations(ctx context.Context, p *tammyv1.PurchasePosting) error {
	sqlText := `SELECT sequence,account_id,account_version,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,noncreditable_reason,allocation_proto FROM purchase_posting_allocations_v1 WHERE posting_id=? AND organisation_id=? ORDER BY sequence`
	args := []any{p.Id, p.OrganisationId}
	if r.reportingRootOwners {
		sqlText, args = postingReportingReadSQL(purchaseReportingPostingAllocationReplayQuery(p.OrganisationId, p.Id))
	}
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return ErrRepository
	}
	defer rows.Close()
	index := 0
	var gross, supplier, eligible int64
	for rows.Next() {
		a := &tammyv1.PurchaseAllocationInput{Gross: purchaseJournalMoney(0), SupplierGst: purchaseJournalMoney(0), EligibleGst: purchaseJournalMoney(0)}
		var content []byte
		destinations := []any{&a.Sequence, &a.AccountId, &a.AccountVersion, &a.Gross.MinorUnits, &a.SupplierGst.MinorUnits, &a.EligibleGst.MinorUnits, &a.Use, &a.BasClassification, &a.CashFlowClassification, &a.NoncreditableReason, &content}
		var invalidOwner int64
		if r.reportingRootOwners {
			destinations = append([]any{&invalidOwner}, destinations...)
		}
		if rows.Scan(destinations...) != nil || invalidOwner != 0 || index >= len(p.Allocations) {
			return ErrRepository
		}
		stored := &tammyv1.PurchaseAllocationInput{}
		if proto.Unmarshal(content, stored) != nil || !canonicalPurchaseBytes(stored, content) || !proto.Equal(stored, p.Allocations[index]) {
			return ErrRepository
		}
		a.EligibilityReviewConfirmed = stored.EligibilityReviewConfirmed
		a.EvidenceReviewConfirmed = stored.EvidenceReviewConfirmed
		if !proto.Equal(a, stored) || a.Sequence != uint32(index+1) || a.AccountVersion == 0 || a.AccountVersion > math.MaxInt64 || !addPurchaseAmount(&gross, a.Gross.MinorUnits) || !addPurchaseAmount(&supplier, a.SupplierGst.MinorUnits) || !addPurchaseAmount(&eligible, a.EligibleGst.MinorUnits) {
			return ErrRepository
		}
		index++
	}
	if rows.Err() != nil || index != len(p.Allocations) || index == 0 || gross != p.Gross.MinorUnits || supplier != p.SupplierGst.MinorUnits || eligible != p.EligibleGst.MinorUnits {
		return ErrRepository
	}
	return rows.Close()
}

func (r *Repository) retainedEvidence(ctx context.Context, p *tammyv1.PurchasePosting) (*tammyv1.PurchaseTaxEvidence, error) {
	history, err := r.retainedEvidenceHistory(ctx, p)
	if err != nil || len(history) == 0 {
		return nil, err
	}
	return history[len(history)-1], nil
}

func (r *Repository) retainedEvidenceHistory(ctx context.Context, p *tammyv1.PurchasePosting) ([]*tammyv1.PurchaseTaxEvidence, error) {
	sqlText := `SELECT id,accounting_version,classification,source_document_id,source_version,source_hash,held_date,registration_review_confirmed,business_use_review_confirmed,evidence_review_confirmed,reason,evidence_proto,record_hash,actor_user_id,created_at FROM purchase_tax_evidence_v1 WHERE organisation_id=? AND posting_id=? AND document_id=? ORDER BY accounting_version,id`
	args := []any{p.OrganisationId, p.Id, p.DocumentId}
	if r.reportingRootOwners {
		sqlText, args = postingReportingReadSQL(purchaseReportingEvidenceReplayQuery(p.OrganisationId, p.Id, p.DocumentId))
	}
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	var history []*tammyv1.PurchaseTaxEvidence
	scanner := r.reportingOwnerScanner(rows)
	for rows.Next() {
		var id, reason, actor, stamp string
		var version uint64
		var classification int32
		var sourceID, held sql.NullString
		var sourceVersion sql.NullInt64
		var sourceHash, content, hash []byte
		var registration, business, confirmed bool
		if scanner.Scan(&id, &version, &classification, &sourceID, &sourceVersion, &sourceHash, &held, &registration, &business, &confirmed, &reason, &content, &hash, &actor, &stamp) != nil {
			return nil, ErrRepository
		}
		e := &tammyv1.PurchaseTaxEvidence{}
		if proto.Unmarshal(content, e) != nil || !canonicalPurchaseBytes(e, content) || !verifyPurchaseHash(e, hash) || !bytes.Equal(hash, e.SourceHash) || e.Id != id || !ids.IsCanonicalV7(id) || e.OrganisationId != p.OrganisationId || e.DocumentId != p.DocumentId || e.AccountingVersion != version || version == 0 || version > math.MaxInt64 || e.ActorUserId != actor || !ids.IsCanonicalV7(actor) || e.RecordedAt == nil || e.RecordedAt.CheckValid() != nil || e.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) != stamp || e.Review == nil {
			return nil, ErrRepository
		}
		review := e.Review
		if int32(review.Classification) != classification || review.Reason != reason || review.RegistrationReviewConfirmed != registration || review.BusinessUseReviewConfirmed != business || review.EvidenceReviewConfirmed != confirmed || (review.Source != nil) != sourceID.Valid || (review.HeldDate != nil) != held.Valid || (held.Valid && purchaseDateString(review.HeldDate) != held.String) {
			return nil, ErrRepository
		}
		if review.Source != nil && (review.Source.SourceId != sourceID.String || !sourceVersion.Valid || sourceVersion.Int64 <= 0 || review.Source.SourceVersion != uint64(sourceVersion.Int64) || !bytes.Equal(review.Source.SourceHash, sourceHash)) {
			return nil, ErrRepository
		}
		if (review.Source == nil && (sourceVersion.Valid || len(sourceHash) != 0)) || (len(history) == 0 && (version != 1 || !proto.Equal(review, p.Evidence) || e.ActorUserId != p.ActorUserId || !proto.Equal(e.RecordedAt, p.PostedAt))) || (len(history) > 0 && version <= history[len(history)-1].AccountingVersion) {
			return nil, ErrRepository
		}
		if _, err := r.ResolveEvidenceRule(ctx, p.OrganisationId, p.DocumentId, e.Review); err != nil {
			return nil, err
		}
		if err := r.verifyEvidenceSource(ctx, p.OrganisationId, e.Review); err != nil {
			return nil, err
		}
		history = append(history, e)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return history, nil
}

func canonicalPurchaseBytes(message proto.Message, content []byte) bool {
	canonical, err := purchaseBytes(message)
	return err == nil && bytes.Equal(canonical, content)
}

func (r *Repository) verifyEvents(ctx context.Context, p *tammyv1.PurchasePosting, pending bool, derived *tammyv1.PurchaseAccounting) (uint64, error) {
	sqlText := `SELECT operation_key,posting_id,document_id,opening_payable_id,accounting_version,command_type,financial_revision,ledger_revision,tax_source_revision,settlement_revision,result_proto,result_sha256,journal_id,created_at FROM purchase_accounting_events_v1 WHERE organisation_id=? AND scope_kind='POSTING' AND scope_id=? AND accounting_version=1`
	args := []any{p.OrganisationId, p.Id}
	if r.reportingRootOwners {
		sqlText, args = postingReportingReadSQL(purchaseReportingEventReplayQuery(p.OrganisationId, p.Id, true))
	}
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return 0, ErrRepository
	}
	defer rows.Close()
	if !rows.Next() {
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return 0, err
		}
		if pending {
			return 0, nil
		}
		return 0, ErrRepository
	}
	var operation, command, stamp string
	var postingID, documentID, openingID, journalID sql.NullString
	var version, financial, ledger, taxSource, settlement uint64
	var content, hash []byte
	if r.reportingOwnerScanner(rows).Scan(&operation, &postingID, &documentID, &openingID, &version, &command, &financial, &ledger, &taxSource, &settlement, &content, &hash, &journalID, &stamp) != nil || rows.Next() || rows.Err() != nil {
		return 0, ErrRepository
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	digest := sha256.Sum256(content)
	response := &tammyv1.PostReviewedPurchaseResponse{}
	if version != 1 || command != purchaseService+"PostReviewedPurchase" || !postingID.Valid || postingID.String != p.Id || documentID.Valid || openingID.Valid || !ids.IsCanonicalV7(operation) || financial != p.FinancialRevision || !bytes.Equal(digest[:], hash) || proto.Unmarshal(content, response) != nil || !canonicalPurchaseBytes(response, content) || !proto.Equal(response.Posting, p) || response.Accounting == nil || !response.Accounting.Posted || response.Accounting.AccountingVersion != 1 || response.Accounting.OrganisationId != p.OrganisationId || response.Accounting.DocumentId != p.DocumentId || !proto.Equal(response.Accounting.Posting, p) || stamp != p.PostedAt.AsTime().UTC().Format(time.RFC3339Nano) || (p.JournalId != nil) != journalID.Valid {
		return 0, ErrRepository
	}
	expected := proto.Clone(derived).(*tammyv1.PurchaseAccounting)
	expected.ReviewOutcomes, err = r.evidenceOutcomes(ctx, p, p.Evidence, p.PostedAt.AsTime().UTC())
	if err != nil || !proto.Equal(response.Accounting, expected) {
		return 0, ErrRepository
	}
	if err := r.verifyElectedEventActor(ctx, operation, command, p.ActorUserId); err != nil {
		return 0, err
	}
	if p.JournalId == nil {
		if response.Journal != nil {
			return 0, ErrRepository
		}
	} else {
		if journalID.String != *p.JournalId || response.Journal == nil || response.Journal.Id != *p.JournalId {
			return 0, ErrRepository
		}
		journal, err := r.reportingJournal(ctx, p.OrganisationId, *p.JournalId)
		if err != nil || !proto.Equal(journal, response.Journal) {
			return 0, ErrRepository
		}
	}
	rows, err = r.executor.QueryContext(ctx, `SELECT domain_mask,financial_revision,ledger_revision,tax_source_revision,settlement_revision FROM financial_revision_claims WHERE operation_key=?`, operation)
	if err != nil {
		return 0, ErrRepository
	}
	var mask uint8
	var claimedFinancial, claimedLedger, claimedTax, claimedSettlement uint64
	if (purchaseRevisionRow{rows: rows}).Scan(&mask, &claimedFinancial, &claimedLedger, &claimedTax, &claimedSettlement) != nil {
		return 0, ErrRepository
	}
	wantMask := uint8(8)
	if p.JournalId != nil {
		wantMask |= 1
	}
	if mask != wantMask || financial != claimedFinancial || ledger != claimedLedger || taxSource != claimedTax || settlement != claimedSettlement {
		return 0, ErrRepository
	}
	return 1, nil
}
