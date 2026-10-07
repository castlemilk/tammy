package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (r *Repository) ResolveLegacy(ctx context.Context, request *tammyv1.ResolveLegacyPurchaseReviewRequest) (*tammyv1.ResolveLegacyPurchaseReviewResponse, error) {
	if r == nil || r.executor == nil || ctx == nil || request == nil || request.CommandContext == nil || request.CommandContext.Authentication == nil ||
		!ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.CommandContext.Authentication.ActorUserId) {
		return nil, ErrInvalidLegacyReview
	}
	if err := ValidateLegacyReview(request.Review); err != nil {
		return nil, err
	}
	now := r.now().UTC()
	if now.IsZero() || purchaseDateKey(request.Review.ReportingBalanceDate) > purchaseDateKey(purchaseAsOf(now)) {
		return nil, ErrInvalidLegacyReview
	}
	input := proto.Clone(request.Review).(*tammyv1.LegacyPurchaseReviewInput)
	kind, id, sourceVersion, sourceHash, reviewedHash, _ := legacySourceScope(input.Source)
	var doc *tammyv1.Document
	var opening *OpeningPayable
	if kind == "DOCUMENT" {
		var err error
		doc, err = r.document(ctx, request.OrganisationId, id)
		if err != nil {
			return nil, err
		}
		content, err := purchaseBytes(doc.Candidate)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		if doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || doc.Version != sourceVersion || !bytes.Equal(doc.Sha256, sourceHash) || !bytes.Equal(digest[:], reviewedHash) {
			return nil, ErrConflict
		}
		if _, err := r.loadPosting(ctx, request.OrganisationId, id); err == nil {
			return nil, ErrPriorAccounting
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	} else {
		owner, _ := NewOpeningRepository(r.executor)
		var err error
		opening, err = owner.GetOpeningPayable(ctx, request.OrganisationId, id)
		if err != nil {
			return nil, err
		}
		if opening.SourceVersion != sourceVersion || !bytes.Equal(opening.SourceHash, sourceHash) {
			return nil, ErrConflict
		}
	}
	history, err := r.ReadLegacy(ctx, request.OrganisationId, input.Source)
	if err != nil {
		return nil, err
	}
	for _, review := range history {
		if proto.Equal(review.Review, input) {
			return nil, ErrConflict
		}
	}
	if err := r.verifyLegacyDecisionFacts(ctx, request.OrganisationId, input, doc, opening); err != nil {
		return nil, err
	}
	if len(history) >= 1000 {
		return nil, ErrSourceBound
	}
	idReview, err := r.newID()
	if err != nil || !ids.IsCanonicalV7(idReview) {
		return nil, ErrRepository
	}
	version := uint64(len(history)) + 1
	record := &tammyv1.LegacyPurchaseReview{Id: idReview, OrganisationId: request.OrganisationId, Review: input, SourceRevision: version,
		ActorUserId: request.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
	record.SourceHash, err = purchaseHash(record)
	if err != nil {
		return nil, err
	}
	content, err := purchaseBytes(record)
	if err != nil || len(content) > 131072 {
		return nil, ErrSourceBound
	}
	var documentID, openingID, retainedReviewedHash any
	if kind == "DOCUMENT" {
		documentID, retainedReviewedHash = id, reviewedHash
	} else {
		openingID = id
	}
	var claimedStart, claimedEnd, claimID, claimVersion, claimHash any
	if input.PreviouslyClaimedPeriod != nil {
		claimedStart, claimedEnd = purchaseDateString(input.PreviouslyClaimedPeriod.PeriodStart), purchaseDateString(input.PreviouslyClaimedPeriod.PeriodEnd)
	}
	if input.ClaimEvidence != nil {
		claimID, claimVersion, claimHash = input.ClaimEvidence.SourceId, input.ClaimEvidence.SourceVersion, input.ClaimEvidence.SourceHash
	}
	revision, err := ReservePurchaseRevision(ctx, r.executor, request.CommandContext.IdempotencyKey, purchaseService+"ResolveLegacyPurchaseReview", false, now)
	if err != nil {
		return nil, err
	}
	_, err = r.executor.ExecContext(ctx, `INSERT INTO purchase_legacy_reviews_v1(id,organisation_id,source_kind,document_id,opening_payable_id,source_version,source_hash,reviewed_snapshot_hash,accounting_version,affected_period_start,affected_period_end,reporting_balance_date,disposition,previously_claimed_period_start,previously_claimed_period_end,claim_evidence_source_id,claim_evidence_source_version,claim_evidence_source_hash,linked_posting_id,reason,review_proto,record_hash,actor_user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.Id, record.OrganisationId, kind, documentID, openingID, sourceVersion, sourceHash, retainedReviewedHash, version,
		purchaseDateString(input.AffectedPeriod.PeriodStart), purchaseDateString(input.AffectedPeriod.PeriodEnd), purchaseDateString(input.ReportingBalanceDate), input.Disposition,
		claimedStart, claimedEnd, claimID, claimVersion, claimHash, purchaseNullable(input.LinkedPostingId), input.Reason, content, record.SourceHash, record.ActorUserId, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	response := &tammyv1.ResolveLegacyPurchaseReviewResponse{Review: record}
	if err := RecordAccountingEvent(ctx, r.executor, AccountingEvent{OperationKey: request.CommandContext.IdempotencyKey, OrganisationID: request.OrganisationId, ScopeKind: kind, ScopeID: id,
		CommandType: purchaseService + "ResolveLegacyPurchaseReview", AccountingVersion: version, Revision: revision, Result: response, Now: now}); err != nil {
		return nil, err
	}
	return response, nil
}

func (r *Repository) verifyLegacyDecisionFacts(ctx context.Context, org string, input *tammyv1.LegacyPurchaseReviewInput, doc *tammyv1.Document, opening *OpeningPayable) error {
	for _, ref := range input.SupportingEvidence {
		if err := r.verifyEvidenceSource(ctx, org, &tammyv1.PurchaseEvidenceInput{Source: ref}); err != nil {
			return err
		}
	}
	if input.ClaimEvidence != nil {
		if err := r.verifyEvidenceSource(ctx, org, &tammyv1.PurchaseEvidenceInput{Source: input.ClaimEvidence}); err != nil {
			return err
		}
	}
	var linked *tammyv1.PurchasePosting
	if input.LinkedPostingId != nil {
		rows, err := r.executor.QueryContext(ctx, `SELECT document_id FROM purchase_postings_v1 WHERE id=? AND organisation_id=?`, *input.LinkedPostingId, org)
		if err != nil {
			return ErrRepository
		}
		var id string
		if (purchaseRevisionRow{rows: rows}).Scan(&id) != nil {
			return ErrInvalidLegacyReview
		}
		value, err := r.Get(ctx, org, id)
		if err != nil {
			return err
		}
		if value.Posting == nil || value.Posting.Id != *input.LinkedPostingId || (doc != nil && value.DocumentId == doc.Id) {
			return ErrInvalidLegacyReview
		}
		linked = value.Posting
	}
	if input.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE {
		if doc == nil {
			return ErrInvalidLegacyReview
		}
		candidates, err := r.duplicateCandidates(ctx, doc)
		if err != nil {
			return err
		}
		matched := false
		for _, candidate := range candidates {
			if linked != nil && candidate.DocumentId == linked.DocumentId {
				matched = true
			}
			for _, ref := range input.SupportingEvidence {
				if ref.SourceId == candidate.DocumentId {
					matched = true
				}
			}
		}
		if !matched {
			return ErrInvalidLegacyReview
		}
	}
	for _, ref := range input.SupportingPayments {
		date, err := r.verifyLegacyPayment(ctx, org, input, ref)
		if err != nil {
			return err
		}
		if (input.Disposition != tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE && purchaseDateKey(date) > purchaseDateKey(input.ReportingBalanceDate)) ||
			(input.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE && purchaseDateKey(date) <= purchaseDateKey(input.ReportingBalanceDate)) {
			return ErrInvalidLegacyReview
		}
	}
	if input.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE {
		// A known linked cash movement contradicts unpaid. Having no such row
		// is never used as the positive proof: explicit retained evidence above is.
		payments, err := r.legacyJournalPayments(ctx, org, input.Source)
		if err != nil {
			return err
		}
		for _, payment := range payments {
			if purchaseDateKey(payment.PostingDate) <= purchaseDateKey(input.ReportingBalanceDate) {
				return ErrInvalidLegacyReview
			}
		}
	}
	_ = opening // Its verified provenance is retained by source, without settlement allocation.
	return nil
}

func (r *Repository) verifyLegacyPayment(ctx context.Context, org string, input *tammyv1.LegacyPurchaseReviewInput, ref *tammyv1.PurchaseRetainedSourceRef) (*tammyv1.CivilDate, error) {
	if !validLegacySupport(ref) {
		return nil, ErrInvalidLegacyReview
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT p.document_id FROM purchase_payments_v1 payment JOIN purchase_postings_v1 p ON p.id=payment.posting_id AND p.organisation_id=payment.organisation_id WHERE payment.id=? AND payment.organisation_id=?`, ref.SourceId, org)
	if err != nil {
		return nil, ErrRepository
	}
	var documentID string
	err = (purchaseRevisionRow{rows: rows}).Scan(&documentID)
	if err == nil {
		state, err := r.Get(ctx, org, documentID)
		if err != nil {
			return nil, err
		}
		if state.Posting == nil || (input.LinkedPostingId == nil || state.Posting.Id != *input.LinkedPostingId) {
			return nil, ErrInvalidLegacyReview
		}
		payments, err := r.loadPayments(ctx, state.Posting)
		if err != nil {
			return nil, err
		}
		for _, payment := range payments {
			if payment.Id == ref.SourceId && payment.AccountingVersion == ref.SourceVersion && bytes.Equal(payment.SourceHash, ref.SourceHash) {
				return payment.ActualPaymentDate, nil
			}
		}
		return nil, ErrInvalidLegacyReview
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRepository
	}
	payments, err := r.legacyJournalPayments(ctx, org, input.Source)
	if err != nil {
		return nil, err
	}
	for _, journal := range payments {
		content, err := purchaseBytes(journal)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		if journal.Id == ref.SourceId && journal.Version == ref.SourceVersion && bytes.Equal(digest[:], ref.SourceHash) {
			return journal.PostingDate, nil
		}
	}
	return nil, ErrInvalidLegacyReview
}

// Only explicit retained lineage and an actual cash credit establish that a
// legacy payment is known. Expense recognition alone does not establish this.
func (r *Repository) legacyJournalPayments(ctx context.Context, org string, source *tammyv1.LegacyPurchaseSourceRef) ([]*tammyv1.Journal, error) {
	_, id, _, _, _, err := legacySourceScope(source)
	if err != nil {
		return nil, err
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT DISTINCT j.id FROM journals j LEFT JOIN journal_lines l ON l.journal_id=j.id LEFT JOIN tax_facts f ON f.journal_line_id=l.id AND f.organisation_id=j.organisation_id WHERE j.organisation_id=? AND j.source_kind=0 AND j.source_type<>'OPENING' AND (j.source_id=? OR f.source_id=?) ORDER BY j.id`, org, id, id)
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
		if len(journalIDs) > 1000 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return nil, ErrRepository
	}
	journals, _ := accounting.NewJournalRepository(r.executor)
	accounts, _ := accounting.NewAccountRepository(r.executor)
	var result []*tammyv1.Journal
	for _, id := range journalIDs {
		journal, err := journals.Get(ctx, id)
		if err != nil || journal.OrganisationId != org {
			return nil, ErrRepository
		}
		if journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED {
			continue
		}
		for _, line := range journal.Lines {
			account, err := accounts.Get(ctx, line.AccountId)
			if err != nil || account.OrganisationId != org {
				return nil, ErrRepository
			}
			if accounting.IsCashAccount(account) && line.GetCredit().GetMinorUnits() > line.GetDebit().GetMinorUnits() {
				result = append(result, journal)
				break
			}
		}
	}
	return result, nil
}

// ReadLegacy verifies the complete immutable history and its full result events.
// It retains earlier source versions; candidate discovery marks those stale.
func (r *Repository) ReadLegacy(ctx context.Context, org string, source *tammyv1.LegacyPurchaseSourceRef) ([]*tammyv1.LegacyPurchaseReview, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) {
		return nil, ErrRepository
	}
	kind, id, _, _, _, err := legacySourceScope(source)
	if err != nil {
		return nil, err
	}
	if kind == "DOCUMENT" {
		if _, err := r.document(ctx, org, id); err != nil {
			return nil, err
		}
	} else {
		opening, _ := NewOpeningRepository(r.executor)
		if _, err := opening.GetOpeningPayable(ctx, org, id); err != nil {
			return nil, err
		}
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT id,review_proto,record_hash FROM purchase_legacy_reviews_v1 WHERE organisation_id=? AND source_kind=? AND COALESCE(document_id,opening_payable_id)=? ORDER BY accounting_version`, org, kind, id)
	if err != nil {
		return nil, ErrRepository
	}
	var values []*tammyv1.LegacyPurchaseReview
	for rows.Next() {
		var recordID string
		var content, hash []byte
		if rows.Scan(&recordID, &content, &hash) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		record := &tammyv1.LegacyPurchaseReview{}
		if proto.Unmarshal(content, record) != nil || !canonicalPurchaseBytes(record, content) || !verifyPurchaseHash(record, hash) || !bytes.Equal(record.SourceHash, hash) ||
			record.Id != recordID || !ids.IsCanonicalV7(record.Id) || record.OrganisationId != org || record.SourceRevision != uint64(len(values)+1) ||
			!ids.IsCanonicalV7(record.ActorUserId) || record.RecordedAt == nil || record.RecordedAt.CheckValid() != nil || ValidateLegacyReview(record.Review) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		oldKind, oldID, _, _, _, err := legacySourceScope(record.Review.Source)
		if err != nil || oldKind != kind || oldID != id {
			_ = rows.Close()
			return nil, ErrRepository
		}
		values = append(values, record)
		if len(values) > 1000 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return nil, ErrRepository
	}
	for _, record := range values {
		if err := r.verifyLegacyScalars(ctx, record); err != nil {
			return nil, err
		}
		// A later document review may make the historical decision stale, but
		// retained supporting bytes must still be owned and hash correctly.
		refs := append([]*tammyv1.PurchaseRetainedSourceRef(nil), record.Review.SupportingEvidence...)
		if record.Review.ClaimEvidence != nil {
			refs = append(refs, record.Review.ClaimEvidence)
		}
		for _, ref := range refs {
			doc, err := r.document(ctx, org, ref.SourceId)
			if err != nil || doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || doc.Version < ref.SourceVersion || !bytes.Equal(doc.Sha256, ref.SourceHash) {
				return nil, ErrRepository
			}
		}
	}
	if err := r.verifyLegacyEvents(ctx, org, kind, id, values); err != nil {
		return nil, err
	}
	return values, nil
}

func (r *Repository) verifyLegacyScalars(ctx context.Context, record *tammyv1.LegacyPurchaseReview) error {
	input := record.Review
	kind, id, version, hash, reviewedHash, err := legacySourceScope(input.Source)
	if err != nil {
		return ErrRepository
	}
	var docID, openingID, snapshot any
	if kind == "DOCUMENT" {
		docID, snapshot = id, reviewedHash
	} else {
		openingID = id
	}
	var claimedStart, claimedEnd, claimID, claimVersion, claimHash any
	if input.PreviouslyClaimedPeriod != nil {
		claimedStart, claimedEnd = purchaseDateString(input.PreviouslyClaimedPeriod.PeriodStart), purchaseDateString(input.PreviouslyClaimedPeriod.PeriodEnd)
	}
	if input.ClaimEvidence != nil {
		claimID, claimVersion, claimHash = input.ClaimEvidence.SourceId, input.ClaimEvidence.SourceVersion, input.ClaimEvidence.SourceHash
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT count(*) FROM purchase_legacy_reviews_v1 WHERE id=? AND organisation_id=? AND source_kind=? AND document_id IS ? AND opening_payable_id IS ? AND source_version=? AND source_hash=? AND reviewed_snapshot_hash IS ? AND accounting_version=? AND affected_period_start=? AND affected_period_end=? AND reporting_balance_date=? AND disposition=? AND previously_claimed_period_start IS ? AND previously_claimed_period_end IS ? AND claim_evidence_source_id IS ? AND claim_evidence_source_version IS ? AND claim_evidence_source_hash IS ? AND linked_posting_id IS ? AND reason=? AND record_hash=? AND actor_user_id=? AND created_at=?`, record.Id, record.OrganisationId, kind, docID, openingID, version, hash, snapshot, record.SourceRevision,
		purchaseDateString(input.AffectedPeriod.PeriodStart), purchaseDateString(input.AffectedPeriod.PeriodEnd), purchaseDateString(input.ReportingBalanceDate), input.Disposition,
		claimedStart, claimedEnd, claimID, claimVersion, claimHash, purchaseNullable(input.LinkedPostingId), input.Reason, record.SourceHash, record.ActorUserId, record.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return ErrRepository
	}
	var count int
	if (purchaseRevisionRow{rows: rows}).Scan(&count) != nil || count != 1 {
		return ErrRepository
	}
	return nil
}

func (r *Repository) verifyLegacyEvents(ctx context.Context, org, kind, id string, reviews []*tammyv1.LegacyPurchaseReview) error {
	rows, err := r.executor.QueryContext(ctx, `SELECT e.operation_key,e.accounting_version,e.command_type,e.result_proto,e.result_sha256,e.created_at,e.posting_id,e.document_id,e.opening_payable_id,e.journal_id,e.financial_revision,e.ledger_revision,e.tax_source_revision,e.settlement_revision,c.domain_mask,c.financial_revision,c.ledger_revision,c.tax_source_revision,c.settlement_revision FROM purchase_accounting_events_v1 e LEFT JOIN financial_revision_claims c ON c.operation_key=e.operation_key WHERE e.organisation_id=? AND e.scope_kind=? AND e.scope_id=? ORDER BY e.accounting_version`, org, kind, id)
	if err != nil {
		return ErrRepository
	}
	defer rows.Close()
	index := 0
	var previousFinancial, previousTax uint64
	for rows.Next() {
		var operation, command, stamp string
		var version, financial, ledger, tax, settlement uint64
		var content, hash []byte
		var posting, document, opening, journal sql.NullString
		var mask, cFinancial, cLedger, cTax, cSettlement sql.NullInt64
		if rows.Scan(&operation, &version, &command, &content, &hash, &stamp, &posting, &document, &opening, &journal, &financial, &ledger, &tax, &settlement,
			&mask, &cFinancial, &cLedger, &cTax, &cSettlement) != nil || index >= len(reviews) {
			return ErrRepository
		}
		digest := sha256.Sum256(content)
		response := &tammyv1.ResolveLegacyPurchaseReviewResponse{}
		if !ids.IsCanonicalV7(operation) || version != uint64(index+1) || command != purchaseService+"ResolveLegacyPurchaseReview" ||
			posting.Valid || journal.Valid || (kind == "DOCUMENT" && (!document.Valid || document.String != id || opening.Valid)) ||
			(kind == "OPENING_PAYABLE" && (!opening.Valid || opening.String != id || document.Valid)) ||
			!bytes.Equal(digest[:], hash) || proto.Unmarshal(content, response) != nil || !canonicalPurchaseBytes(response, content) || !proto.Equal(response.Review, reviews[index]) ||
			stamp != reviews[index].RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) || financial == 0 || financial > math.MaxInt64 || tax == 0 || tax > math.MaxInt64 || ledger > math.MaxInt64 || settlement > math.MaxInt64 ||
			financial <= previousFinancial || tax <= previousTax || !mask.Valid || mask.Int64 != 8 || !cFinancial.Valid || cFinancial.Int64 != int64(financial) ||
			!cLedger.Valid || cLedger.Int64 != int64(ledger) || !cTax.Valid || cTax.Int64 != int64(tax) || !cSettlement.Valid || cSettlement.Int64 != int64(settlement) {
			return ErrRepository
		}
		previousFinancial, previousTax = financial, tax
		index++
	}
	if index != len(reviews) || errors.Join(rows.Err(), rows.Close()) != nil {
		return ErrRepository
	}
	return nil
}

// LegacyCandidates returns provenance for every source, including excluded
// sources needed by workpaper fingerprints. It never limits discovery to pages.
func (r *Repository) LegacyCandidates(ctx context.Context, org string, period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate) ([]*LegacyCandidate, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || !validClaimTimingPeriod(period) ||
		purchaseDateString(period.PeriodStart) == "" || purchaseDateString(period.PeriodEnd) == "" || purchaseDateString(balance) == "" {
		return nil, ErrInvalidLegacyReview
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT d.id FROM documents d LEFT JOIN purchase_postings_v1 p ON p.document_id=d.id AND p.organisation_id=d.organisation_id WHERE d.organisation_id=? AND d.status='REVIEWED' AND p.id IS NULL ORDER BY d.id`, org)
	if err != nil {
		return nil, ErrRepository
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		ids = append(ids, id)
		if len(ids) > 1000 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return nil, ErrRepository
	}
	result := make([]*LegacyCandidate, 0, len(ids))
	for _, id := range ids {
		doc, err := r.document(ctx, org, id)
		if err != nil {
			return nil, err
		}
		content, err := purchaseBytes(doc.Candidate)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		source := &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_Document{Document: &tammyv1.LegacyPurchaseDocumentRef{DocumentId: doc.Id, ReviewedVersion: doc.Version, OriginalContentHash: append([]byte(nil), doc.Sha256...), ReviewedSnapshotHash: append([]byte(nil), digest[:]...)}}}
		reviews, err := r.ReadLegacy(ctx, org, source)
		if err != nil {
			return nil, err
		}
		value := &LegacyCandidate{Source: source, Document: doc, Reviews: reviews, Status: tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY}
		if doc.Candidate.GetGst() != nil {
			if !validPurchaseMoney(doc.Candidate.Gst) {
				return nil, ErrRepository
			}
			value.CandidateGST = doc.Candidate.Gst.MinorUnits
		}
		value.PriorSources, err = r.legacyPriorSources(ctx, doc)
		if err != nil {
			return nil, err
		}
		if err := r.applyLegacyCandidate(ctx, org, value, period, balance); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	openingRepo, _ := NewOpeningRepository(r.executor)
	openings, err := openingRepo.ListOpeningPayables(ctx, org)
	if err != nil {
		return nil, err
	}
	if len(result)+len(openings) > 1000 {
		return nil, ErrSourceBound
	}
	for _, opening := range openings {
		value := &LegacyCandidate{Source: opening.LegacySource(), Opening: opening, Status: tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY}
		if opening.Input.OutstandingGst != nil {
			gst, prior := opening.Input.OutstandingGst.MinorUnits, opening.Input.PriorGstAttributed.MinorUnits
			if gst < 0 {
				gst = -gst
			}
			if prior < 0 {
				prior = -prior
			}
			value.CandidateGST = gst - prior
		}
		value.Reviews, err = r.ReadLegacy(ctx, org, value.Source)
		if err != nil {
			return nil, err
		}
		if err := r.applyLegacyCandidate(ctx, org, value, period, balance); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (r *Repository) applyLegacyCandidate(ctx context.Context, org string, value *LegacyCandidate, period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate) error {
	payments, err := r.legacyJournalPayments(ctx, org, value.Source)
	if err != nil {
		return err
	}
	knownPayment := false
	var currentPayments []*tammyv1.Journal
	for _, payment := range payments {
		content, err := purchaseBytes(payment)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		retained := false
		for _, ref := range value.PriorSources {
			if ref.Type == "journal" && ref.Id == payment.Id {
				if ref.Revision != payment.Version || !bytes.Equal(ref.ContentHash, digest[:]) {
					return ErrRepository
				}
				retained = true
				break
			}
		}
		if !retained {
			value.PriorSources = append(value.PriorSources, &tammyv1.SourceRef{Type: "journal", Id: payment.Id, Revision: payment.Version, ContentHash: append([]byte(nil), digest[:]...)})
		}
		if claimTimingPeriodContains(period, payment.PostingDate) && purchaseDateKey(payment.PostingDate) <= purchaseDateKey(balance) {
			knownPayment = true
			currentPayments = append(currentPayments, payment)
		}
	}
	value.Excluded, value.EvidenceOnly = false, false
	value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
	if knownPayment {
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
	}
	// A consistent evidence-only link establishes transaction identity across
	// periods. All decisions about that identity must be assessed together;
	// conflicting links or settlement facts cannot be bypassed by period scope.
	hasEvidenceLink := false
	for _, review := range value.Reviews {
		if proto.Equal(review.Review.Source, value.Source) && review.Review.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE {
			hasEvidenceLink = true
		}
	}
	var applied *tammyv1.LegacyPurchaseReviewInput
	var appliedReviews []*tammyv1.LegacyPurchaseReviewInput
	contradiction := false
	for _, review := range value.Reviews {
		if !LegacyReviewApplies(review, value.Source, period, balance) && !(hasEvidenceLink && proto.Equal(review.Review.Source, value.Source)) {
			continue
		}
		if err := r.verifyLegacyDecisionFacts(ctx, org, review.Review, value.Document, value.Opening); err != nil {
			// Contradictory new payment/version facts remain visible, without
			// rewriting an earlier decision or quietly accepting its exclusion.
			if errors.Is(err, ErrInvalidLegacyReview) || errors.Is(err, ErrInvalidEvidence) || errors.Is(err, ErrConflict) {
				contradiction = true
				continue
			}
			return err
		}
		if applied != nil && !consistentLegacyDecision(applied, review.Review) {
			contradiction = true
		}
		appliedReviews = append(appliedReviews, review.Review)
		if applied == nil {
			applied = review.Review
		}
	}
	if hasEvidenceLink && knownPayment {
		contradiction = true
	}
	if contradiction {
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
		return nil
	}
	if applied == nil {
		return nil
	}
	switch applied.Disposition {
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE:
		value.EvidenceOnly, value.Excluded = true, true
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE:
		if !knownPayment {
			value.Excluded = true
			value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
		}
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED:
		if knownPayment {
			profiles, _ := organisations.NewRepository(r.executor)
			profile, err := profiles.Get(ctx, org)
			if err != nil {
				return err
			}
			if profile.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH {
				// Cash claim evidence cannot waive a newly known payment. Its
				// actual date must support the retained claim period, and exact
				// payment provenance must be reviewed in consistent source history.
				for _, payment := range currentPayments {
					if purchaseDateKey(payment.PostingDate) > purchaseDateKey(applied.PreviouslyClaimedPeriod.PeriodEnd) {
						return nil
					}
					content, err := purchaseBytes(payment)
					if err != nil {
						return err
					}
					digest := sha256.Sum256(content)
					supported := false
					for _, review := range appliedReviews {
						for _, ref := range review.SupportingPayments {
							if ref.SourceId == payment.Id && ref.SourceVersion == payment.Version && bytes.Equal(ref.SourceHash, digest[:]) {
								supported = true
							}
						}
					}
					if !supported {
						return nil
					}
				}
			}
		}
		value.Excluded = true
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE:
		value.Excluded = true
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT:
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
	}
	return nil
}

func consistentLegacyDecision(a, b *tammyv1.LegacyPurchaseReviewInput) bool {
	return a.Disposition == b.Disposition && (a.LinkedPostingId == nil) == (b.LinkedPostingId == nil) &&
		a.GetLinkedPostingId() == b.GetLinkedPostingId() && proto.Equal(a.PreviouslyClaimedPeriod, b.PreviouslyClaimedPeriod)
}

// LegacyEvidenceForPosting returns reviewed evidence provenance for an owned
// purchase. Consistent links enrich its source history without attributing GST
// or changing the purchase's original posting/evidence records.
func (r *Repository) LegacyEvidenceForPosting(ctx context.Context, org, postingID string, period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate) ([]*LegacyCandidate, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(postingID) {
		return nil, ErrRepository
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT document_id FROM purchase_postings_v1 WHERE id=? AND organisation_id=?`, postingID, org)
	if err != nil {
		return nil, ErrRepository
	}
	var documentID string
	if err := (purchaseRevisionRow{rows: rows}).Scan(&documentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, ErrRepository
	}
	owned, err := r.Get(ctx, org, documentID)
	if err != nil {
		return nil, err
	}
	if owned.Posting == nil || owned.Posting.Id != postingID {
		return nil, ErrRepository
	}
	values, err := r.LegacyCandidates(ctx, org, period, balance)
	if err != nil {
		return nil, err
	}
	var result []*LegacyCandidate
	for _, value := range values {
		if !value.EvidenceOnly || !value.Excluded || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
			continue
		}
		for _, review := range value.Reviews {
			if proto.Equal(review.Review.Source, value.Source) && review.Review.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE && review.Review.GetLinkedPostingId() == postingID {
				result = append(result, value)
				break
			}
		}
	}
	return result, nil
}

func (r *Repository) legacyPriorSources(ctx context.Context, doc *tammyv1.Document) ([]*tammyv1.SourceRef, error) {
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
		if len(journalIDs) > 1000 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
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
		digest := sha256.Sum256(content)
		result = append(result, &tammyv1.SourceRef{Type: "journal", Id: id, Revision: journal.Version, ContentHash: append([]byte(nil), digest[:]...)})
	}
	content, err := purchaseBytes(doc.Candidate)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	opening, _ := NewOpeningRepository(r.executor)
	refs, err := opening.MatchingPriorSources(ctx, doc.OrganisationId, doc.Id, doc.Sha256, digest[:])
	if err != nil {
		return nil, err
	}
	return append(result, refs...), nil
}
