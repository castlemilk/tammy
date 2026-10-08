package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (r *Repository) PostReviewed(ctx context.Context, request *tammyv1.PostReviewedPurchaseRequest) (*tammyv1.PostReviewedPurchaseResponse, error) {
	if r == nil || r.executor == nil || ctx == nil || request == nil || request.CommandContext == nil || request.CommandContext.Authentication == nil || !ids.IsCanonicalV7(request.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.CommandContext.Authentication.ActorUserId) || request.ExpectedReviewedVersion == 0 || request.ExpectedReviewedVersion > math.MaxInt64 {
		return nil, ErrRepository
	}
	now := r.now().UTC()
	if now.IsZero() {
		return nil, ErrRepository
	}
	doc, err := r.document(ctx, request.OrganisationId, request.DocumentId)
	if err != nil {
		return nil, err
	}
	if doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || doc.Version != request.ExpectedReviewedVersion {
		return nil, ErrConflict
	}
	if purchaseDateString(doc.Candidate.DocumentDate) == "" {
		return nil, ErrInvalidReviewedPurchase
	}
	profileRepo, _ := organisations.NewRepository(r.executor)
	profile, err := profileRepo.Get(ctx, request.OrganisationId)
	if err != nil {
		return nil, err
	}
	if profile.Version > math.MaxInt64 {
		return nil, ErrRepository
	}
	watermark, err := invoices.CheckGSTBasisHistory(ctx, r.executor, profile.Id, 0)
	if err != nil {
		return nil, err
	}
	if watermark > math.MaxInt64 {
		return nil, ErrRepository
	}
	accountsRepo, _ := accounting.NewAccountRepository(r.executor)
	accounts := make(map[string]*tammyv1.Account, len(request.Allocations))
	for _, allocation := range request.Allocations {
		if allocation == nil || allocation.AccountVersion == 0 || allocation.AccountVersion > math.MaxInt64 {
			return nil, ErrInvalidAllocation
		}
		if _, loaded := accounts[allocation.AccountId]; !loaded {
			account, err := accountsRepo.Get(ctx, allocation.AccountId)
			if err != nil {
				return nil, err
			}
			accounts[allocation.AccountId] = account
		}
	}
	exceptionRule, err := r.ResolveEvidenceRule(ctx, profile.Id, doc.Id, request.Evidence)
	if err != nil {
		return nil, err
	}
	input := ReviewedPurchaseInput{OrganisationID: profile.Id, DocumentID: doc.Id, ReviewedVersion: doc.Version, Document: doc, Allocations: request.Allocations, Accounts: accounts, GSTBasis: profile.GstBasis, GSTProfileVersion: profile.Version, GSTHistoryWatermark: watermark, Evidence: request.Evidence, ExceptionRule: exceptionRule}
	amounts, err := ValidateReviewedPurchase(input)
	if err != nil {
		return nil, err
	}
	if err := r.verifyEvidenceSource(ctx, profile.Id, request.Evidence); err != nil {
		return nil, err
	}
	readiness, err := EvidenceReadinessFor(request.Evidence, doc.Candidate.DocumentDate, purchaseAsOf(now), exceptionRule)
	if err != nil {
		return nil, err
	}
	if request.PriorAccountingReview == nil || request.PriorAccountingReview.Decision != tammyv1.PurchasePriorAccountingDecision_PURCHASE_PRIOR_ACCOUNTING_DECISION_NOT_PREVIOUSLY_ACCOUNTED_OR_CLAIMED || !request.PriorAccountingReview.ReviewedExistingAccountingConfirmed || !purchaseReviewText(request.PriorAccountingReview.Reason, 512, true) {
		return nil, ErrPriorAccounting
	}
	candidates, err := r.duplicateCandidates(ctx, doc)
	if err != nil {
		return nil, err
	}
	duplicateRefs, err := ValidateDuplicateReview(request.DuplicateReview, profile.Id, doc.Id, doc.Sha256, candidates)
	if err != nil {
		return nil, err
	}
	prior, err := r.priorSources(ctx, doc)
	if err != nil {
		return nil, err
	}
	if len(prior) > 0 {
		return nil, ErrPriorAccounting
	}
	supplier, invoice, err := NormalizeDuplicateIdentity(doc.Candidate.SupplierName, doc.Candidate.InvoiceNumber)
	if err != nil {
		return nil, err
	}
	writer, _ := NewJournalWriter(r.executor, func() time.Time { return now }, r.newID)
	if err := writer.IsPostingDateOpen(ctx, profile.Id, doc.Candidate.DocumentDate); err != nil {
		return nil, err
	}
	postingID, err := r.newID()
	if err != nil || !ids.IsCanonicalV7(postingID) {
		return nil, ErrRepository
	}
	evidenceID, err := r.newID()
	if err != nil || !ids.IsCanonicalV7(evidenceID) {
		return nil, ErrRepository
	}
	reviewed, err := purchaseBytes(doc.Candidate)
	if err != nil {
		return nil, err
	}
	reviewedHash := sha256.Sum256(reviewed)
	revision, err := ReservePurchaseRevision(ctx, r.executor, request.CommandContext.IdempotencyKey, purchaseService+"PostReviewedPurchase", amounts.Gross > 0, now)
	if err != nil {
		return nil, err
	}
	journal, err := writer.PostPurchase(ctx, postingID, input, readiness, revision.Financial)
	if err != nil {
		return nil, err
	}
	posting := &tammyv1.PurchasePosting{Id: postingID, OrganisationId: profile.Id, DocumentId: doc.Id, ReviewedVersion: doc.Version, OriginalContentHash: append([]byte(nil), doc.Sha256...), ReviewedSnapshotHash: reviewedHash[:], SupplierName: doc.Candidate.SupplierName, InvoiceNumber: doc.Candidate.InvoiceNumber, InvoiceDate: proto.Clone(doc.Candidate.DocumentDate).(*tammyv1.CivilDate), GstBasis: profile.GstBasis, GstProfileVersion: profile.Version, GstHistoryWatermark: watermark, Gross: purchaseJournalMoney(amounts.Gross), SupplierGst: purchaseJournalMoney(amounts.SupplierGST), EligibleGst: purchaseJournalMoney(amounts.EligibleGST), Evidence: proto.Clone(request.Evidence).(*tammyv1.PurchaseEvidenceInput), DuplicateReview: proto.Clone(request.DuplicateReview).(*tammyv1.PurchaseDuplicateReview), PriorAccountingReview: proto.Clone(request.PriorAccountingReview).(*tammyv1.PurchasePriorAccountingReview), FinancialRevision: revision.Financial, ActorUserId: request.CommandContext.Authentication.ActorUserId, PostedAt: timestamppb.New(now), DuplicateCandidates: duplicateRefs, PriorAccountingSources: prior}
	for _, allocation := range request.Allocations {
		posting.Allocations = append(posting.Allocations, proto.Clone(allocation).(*tammyv1.PurchaseAllocationInput))
	}
	if journal != nil {
		posting.JournalId = proto.String(journal.Id)
	}
	posting.SourceHash, err = purchaseHash(posting)
	if err != nil {
		return nil, err
	}
	content, err := purchaseBytes(posting)
	if err != nil {
		return nil, err
	}
	_, err = r.executor.ExecContext(ctx, `INSERT INTO purchase_postings_v1(id,organisation_id,document_id,reviewed_version,original_content_hash,reviewed_snapshot_hash,reviewed_source_proto,supplier_name,supplier_identity,invoice_number,invoice_date,gst_basis,gst_profile_version,gst_history_watermark,gross_minor,supplier_gst_minor,eligible_gst_minor,currency_code,journal_id,financial_revision,duplicate_decision,duplicate_reason,prior_accounting_decision,prior_accounting_reason,normalized_supplier,normalized_invoice,source_hash,posting_proto,actor_user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'AUD',?,?,?,?,?,?,?,?,?,?,?,?)`, posting.Id, profile.Id, doc.Id, doc.Version, posting.OriginalContentHash, posting.ReviewedSnapshotHash, reviewed, posting.SupplierName, purchaseNullable(posting.SupplierIdentity), posting.InvoiceNumber, purchaseDateString(posting.InvoiceDate), posting.GstBasis, profile.Version, watermark, amounts.Gross, amounts.SupplierGST, amounts.EligibleGST, purchaseNullable(posting.JournalId), revision.Financial, posting.DuplicateReview.Decision, posting.DuplicateReview.Reason, posting.PriorAccountingReview.Decision, posting.PriorAccountingReview.Reason, supplier, invoice, posting.SourceHash, content, posting.ActorUserId, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	for _, allocation := range posting.Allocations {
		encoded, err := purchaseBytes(allocation)
		if err != nil {
			return nil, err
		}
		_, err = r.executor.ExecContext(ctx, `INSERT INTO purchase_posting_allocations_v1(posting_id,organisation_id,sequence,account_id,account_version,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,noncreditable_reason,allocation_proto) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, posting.Id, profile.Id, allocation.Sequence, allocation.AccountId, allocation.AccountVersion, allocation.Gross.MinorUnits, allocation.SupplierGst.MinorUnits, allocation.EligibleGst.MinorUnits, allocation.Use, allocation.BasClassification, allocation.CashFlowClassification, allocation.NoncreditableReason, encoded)
		if err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
	}
	evidence := &tammyv1.PurchaseTaxEvidence{Id: evidenceID, OrganisationId: profile.Id, DocumentId: doc.Id, AccountingVersion: 1, Review: proto.Clone(posting.Evidence).(*tammyv1.PurchaseEvidenceInput), ActorUserId: posting.ActorUserId, RecordedAt: timestamppb.New(now)}
	if err := r.retainEvidence(ctx, posting.Id, evidence); err != nil {
		return nil, err
	}
	accountingState, err := r.get(ctx, profile.Id, doc.Id, true)
	if err != nil {
		return nil, err
	}
	response := &tammyv1.PostReviewedPurchaseResponse{Posting: posting, Journal: journal, Accounting: accountingState}
	if err := RecordAccountingEvent(ctx, r.executor, AccountingEvent{OperationKey: request.CommandContext.IdempotencyKey, OrganisationID: profile.Id, ScopeKind: "POSTING", ScopeID: posting.Id, CommandType: purchaseService + "PostReviewedPurchase", AccountingVersion: 1, Revision: revision, Result: response, JournalID: posting.JournalId, Now: now}); err != nil {
		return nil, err
	}
	return response, nil
}

func (r *Repository) verifyEvidenceSource(ctx context.Context, org string, review *tammyv1.PurchaseEvidenceInput) error {
	if review == nil {
		return ErrInvalidEvidence
	}
	if review.Source == nil {
		return nil
	}
	if review.Source.SourceVersion == 0 || review.Source.SourceVersion > math.MaxInt64 {
		return ErrInvalidEvidence
	}
	doc, err := r.document(ctx, org, review.Source.SourceId)
	if err != nil {
		return err
	}
	if doc.Version != review.Source.SourceVersion || doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || !bytes.Equal(doc.Sha256, review.Source.SourceHash) {
		return ErrInvalidEvidence
	}
	if review.HeldDate != nil && purchaseDateString(review.HeldDate) == "" {
		return ErrInvalidEvidence
	}
	return nil
}

func (r *Repository) retainEvidence(ctx context.Context, postingID string, e *tammyv1.PurchaseTaxEvidence) error {
	var err error
	e.SourceHash, err = purchaseHash(e)
	if err != nil {
		return err
	}
	content, err := purchaseBytes(e)
	if err != nil {
		return err
	}
	var sourceID, sourceVersion, sourceHash, held any
	if e.Review.Source != nil {
		sourceID = e.Review.Source.SourceId
		sourceVersion = e.Review.Source.SourceVersion
		sourceHash = e.Review.Source.SourceHash
	}
	if e.Review.HeldDate != nil {
		held = purchaseDateString(e.Review.HeldDate)
	}
	_, err = r.executor.ExecContext(ctx, `INSERT INTO purchase_tax_evidence_v1(id,organisation_id,posting_id,document_id,accounting_version,classification,source_document_id,source_version,source_hash,held_date,registration_review_confirmed,business_use_review_confirmed,evidence_review_confirmed,reason,evidence_proto,record_hash,actor_user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.Id, e.OrganisationId, postingID, e.DocumentId, e.AccountingVersion, e.Review.Classification, sourceID, sourceVersion, sourceHash, held, e.Review.RegistrationReviewConfirmed, e.Review.BusinessUseReviewConfirmed, e.Review.EvidenceReviewConfirmed, e.Review.Reason, content, e.SourceHash, e.ActorUserId, e.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return errors.Join(ErrRepository, err)
	}
	return nil
}

func (r *Repository) duplicateCandidates(ctx context.Context, doc *tammyv1.Document) ([]*tammyv1.PurchasePosting, error) {
	supplier, invoice, _ := NormalizeDuplicateIdentity(doc.Candidate.SupplierName, doc.Candidate.InvoiceNumber)
	rows, err := r.executor.QueryContext(ctx, `SELECT document_id FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY id`, doc.OrganisationId)
	if err != nil {
		return nil, ErrRepository
	}
	var documentIDs []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		documentIDs = append(documentIDs, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	result := make([]*tammyv1.PurchasePosting, 0, len(documentIDs))
	for _, id := range documentIDs {
		state, err := r.Get(ctx, doc.OrganisationId, id)
		if err != nil {
			return nil, err
		}
		p := state.Posting
		if p == nil {
			return nil, ErrRepository
		}
		candidateSupplier, candidateInvoice, err := NormalizeDuplicateIdentity(p.SupplierName, p.InvoiceNumber)
		if err != nil {
			return nil, ErrRepository
		}
		if p.DocumentId == doc.Id || bytes.Equal(p.OriginalContentHash, doc.Sha256) || (supplier != "" && invoice != "" && candidateSupplier == supplier && candidateInvoice == invoice) {
			result = append(result, p)
			if len(result) > 50 {
				return nil, ErrSourceBound
			}
		}
	}
	return result, nil
}

func (r *Repository) priorSources(ctx context.Context, doc *tammyv1.Document) ([]*tammyv1.SourceRef, error) {
	// Manual journal headers use the operation key; document lineage is also
	// retained on tax facts. Invoice recharge evidence alone is not an expense.
	rows, err := r.executor.QueryContext(ctx, `SELECT DISTINCT j.id FROM journals j LEFT JOIN journal_lines l ON l.journal_id=j.id LEFT JOIN tax_facts f ON f.journal_line_id=l.id AND f.organisation_id=j.organisation_id WHERE j.organisation_id=? AND j.source_kind=0 AND (j.source_id=? OR f.source_id=?) ORDER BY j.id`, doc.OrganisationId, doc.Id, doc.Id)
	if err != nil {
		return nil, ErrRepository
	}
	var journalIDs []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		journalIDs = append(journalIDs, id)
		if len(journalIDs) > 50 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	var result []*tammyv1.SourceRef
	journals, _ := accounting.NewJournalRepository(r.executor)
	for _, id := range journalIDs {
		journal, err := journals.Get(ctx, id)
		if err != nil || journal.OrganisationId != doc.OrganisationId {
			return nil, ErrRepository
		}
		content, err := purchaseBytes(journal)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(content)
		result = append(result, &tammyv1.SourceRef{Type: "journal", Id: id, Revision: journal.Version, ContentHash: hash[:]})
	}
	content, err := purchaseBytes(doc.Candidate)
	if err != nil {
		return nil, err
	}
	reviewedHash := sha256.Sum256(content)
	opening, _ := NewOpeningRepository(r.executor)
	openingRefs, err := opening.MatchingPriorSources(ctx, doc.OrganisationId, doc.Id, doc.Sha256, reviewedHash[:])
	if err != nil {
		return nil, err
	}
	result = append(result, openingRefs...)
	// A linked evidence document is not another transaction. Retain its reviewed
	// decision as prior provenance so it cannot be posted as a new expense.
	rows, err = r.executor.QueryContext(ctx, `SELECT id,review_proto,record_hash FROM purchase_legacy_reviews_v1 WHERE organisation_id=? AND (document_id=? OR source_hash=?) ORDER BY id`, doc.OrganisationId, doc.Id, doc.Sha256)
	if err != nil {
		return nil, ErrRepository
	}
	var legacyReviews []*tammyv1.LegacyPurchaseReview
	for rows.Next() {
		var id string
		var content, hash []byte
		if rows.Scan(&id, &content, &hash) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		review := &tammyv1.LegacyPurchaseReview{}
		if proto.Unmarshal(content, review) != nil || !canonicalPurchaseBytes(review, content) || review.Id != id || review.OrganisationId != doc.OrganisationId || review.Review == nil || !verifyPurchaseHash(review, hash) || !bytes.Equal(review.SourceHash, hash) {
			_ = rows.Close()
			return nil, ErrRepository
		}
		legacyReviews = append(legacyReviews, review)
		if len(legacyReviews) > 50 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	for _, review := range legacyReviews {
		if err := r.verifyLegacyDocumentReview(ctx, doc, review); err != nil {
			return nil, err
		}
		switch review.Review.Disposition {
		case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE:
			// Payment/credit eligibility reviews alone do not establish that
			// the underlying expense was already recognised.
			continue
		case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT:
		default:
			_ = rows.Close()
			return nil, ErrRepository
		}
		result = append(result, &tammyv1.SourceRef{Type: "legacy_purchase_review", Id: review.Id, Revision: review.SourceRevision, ContentHash: append([]byte(nil), review.SourceHash...)})
		if len(result) > 50 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	rows, err = r.executor.QueryContext(ctx, `SELECT posting_id,evidence_proto,record_hash FROM purchase_tax_evidence_v1 WHERE organisation_id=? AND source_document_id=? AND document_id<>? ORDER BY id`, doc.OrganisationId, doc.Id, doc.Id)
	if err != nil {
		return nil, ErrRepository
	}
	type evidenceSource struct {
		postingID string
		evidence  *tammyv1.PurchaseTaxEvidence
	}
	var evidenceSources []evidenceSource
	for rows.Next() {
		var postingID string
		var content, hash []byte
		if rows.Scan(&postingID, &content, &hash) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		e := &tammyv1.PurchaseTaxEvidence{}
		if proto.Unmarshal(content, e) != nil || !canonicalPurchaseBytes(e, content) || e.OrganisationId != doc.OrganisationId || !verifyPurchaseHash(e, hash) || !bytes.Equal(e.SourceHash, hash) {
			_ = rows.Close()
			return nil, ErrRepository
		}
		evidenceSources = append(evidenceSources, evidenceSource{postingID, e})
		if len(result)+len(evidenceSources) > 50 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	for _, source := range evidenceSources {
		state, err := r.Get(ctx, doc.OrganisationId, source.evidence.DocumentId)
		if err != nil {
			return nil, err
		}
		if state.Posting == nil || state.Posting.Id != source.postingID {
			return nil, ErrRepository
		}
		retained, err := r.retainedEvidence(ctx, state.Posting)
		if err != nil || !proto.Equal(retained, source.evidence) {
			return nil, ErrRepository
		}
		result = append(result, &tammyv1.SourceRef{Type: "purchase_tax_evidence", Id: retained.Id, Revision: retained.AccountingVersion, ContentHash: append([]byte(nil), retained.SourceHash...)})
	}
	if len(result) > 50 {
		return nil, ErrSourceBound
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].Id < result[j].Id
	})
	return result, nil
}

func (r *Repository) verifyLegacyDocumentReview(ctx context.Context, doc *tammyv1.Document, record *tammyv1.LegacyPurchaseReview) error {
	input := record.Review
	source := input.GetSource().GetDocument()
	content, err := purchaseBytes(doc.Candidate)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	if source == nil || source.DocumentId != doc.Id || source.ReviewedVersion != doc.Version || !bytes.Equal(source.OriginalContentHash, doc.Sha256) || !bytes.Equal(source.ReviewedSnapshotHash, digest[:]) || record.SourceRevision == 0 || record.SourceRevision > math.MaxInt64 || record.RecordedAt == nil || record.RecordedAt.CheckValid() != nil || !ids.IsCanonicalV7(record.ActorUserId) || input.AffectedPeriod == nil || purchaseDateString(input.AffectedPeriod.PeriodStart) == "" || purchaseDateString(input.AffectedPeriod.PeriodEnd) == "" || purchaseDateString(input.ReportingBalanceDate) == "" {
		return ErrRepository
	}
	var claimedStart, claimedEnd, claimID, claimVersion, claimHash any
	if input.PreviouslyClaimedPeriod != nil {
		claimedStart = purchaseDateString(input.PreviouslyClaimedPeriod.PeriodStart)
		claimedEnd = purchaseDateString(input.PreviouslyClaimedPeriod.PeriodEnd)
	}
	if input.ClaimEvidence != nil {
		claimID = input.ClaimEvidence.SourceId
		claimVersion = input.ClaimEvidence.SourceVersion
		claimHash = input.ClaimEvidence.SourceHash
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT count(*) FROM purchase_legacy_reviews_v1 WHERE id=? AND organisation_id=? AND source_kind='DOCUMENT' AND document_id=? AND opening_payable_id IS NULL AND source_version=? AND source_hash=? AND reviewed_snapshot_hash=? AND accounting_version=? AND affected_period_start=? AND affected_period_end=? AND reporting_balance_date=? AND disposition=? AND previously_claimed_period_start IS ? AND previously_claimed_period_end IS ? AND claim_evidence_source_id IS ? AND claim_evidence_source_version IS ? AND claim_evidence_source_hash IS ? AND linked_posting_id IS ? AND reason=? AND record_hash=? AND actor_user_id=? AND created_at=?`, record.Id, doc.OrganisationId, source.DocumentId, source.ReviewedVersion, source.OriginalContentHash, source.ReviewedSnapshotHash, record.SourceRevision, purchaseDateString(input.AffectedPeriod.PeriodStart), purchaseDateString(input.AffectedPeriod.PeriodEnd), purchaseDateString(input.ReportingBalanceDate), input.Disposition, claimedStart, claimedEnd, claimID, claimVersion, claimHash, purchaseNullable(input.LinkedPostingId), input.Reason, record.SourceHash, record.ActorUserId, record.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return ErrRepository
	}
	var count int
	if (purchaseRevisionRow{rows: rows}).Scan(&count) != nil || count != 1 {
		return ErrRepository
	}
	return nil
}
