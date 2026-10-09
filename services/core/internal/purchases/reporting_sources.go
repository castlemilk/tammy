package purchases

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"hash"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const maximumBASPurchaseSources = 1000

// CaptureBAS reads every owned purchase and every legacy candidate. No browser
// page or mutable paid flag participates in claimed GST. Get validates complete
// accounting/journal/event replay before any retained fact becomes reportable.
func (r *Repository) CaptureBAS(ctx context.Context, org string, period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate, asOf time.Time) (*BASSnapshot, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) || !validStoredClaimPeriod(period) || !validPurchaseDate(balance) || asOf.IsZero() {
		return nil, ErrRepository
	}
	reader := *r
	reader.now = func() time.Time { return asOf.UTC() }
	r = &reader
	profileRepo, _ := organisations.NewRepository(r.executor)
	profile, err := profileRepo.Get(ctx, org)
	if err != nil {
		return nil, ErrRepository
	}
	watermark, err := invoices.CheckGSTBasisHistory(ctx, r.executor, org, 0)
	if err != nil {
		return nil, err
	}
	out := &BASSnapshot{LegacyCandidateGST: purchaseJournalMoney(0)}
	digest := sha256.New()
	if err := basHashMessage(digest, profile); err != nil {
		return nil, err
	}
	basHashBytes(digest, lowValueRuleBytes)
	if err := r.basRetainedRuleMetadata(ctx, digest); err != nil {
		return nil, err
	}
	var version [8]byte
	binary.BigEndian.PutUint64(version[:], watermark)
	basHashBytes(digest, version[:])
	rows, err := r.executor.QueryContext(ctx, `SELECT document_id FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY invoice_date,id LIMIT 1001`, org)
	if err != nil {
		return nil, ErrRepository
	}
	var documents []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		documents = append(documents, id)
		if len(documents) > maximumBASPurchaseSources {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return nil, ErrRepository
	}
	for _, id := range documents {
		state, err := r.Get(ctx, org, id)
		if err != nil || !state.Posted {
			if err != nil {
				return nil, err
			}
			return nil, ErrRepository
		}
		posting := state.Posting
		if err := basHashMessage(digest, state); err != nil {
			return nil, err
		}
		payments, err := r.loadPayments(ctx, posting)
		if err != nil {
			return nil, err
		}
		evidences, err := r.retainedEvidenceHistory(ctx, posting)
		if err != nil {
			return nil, err
		}
		history, err := r.VerifiedClaims(ctx, org, id)
		if err != nil {
			return nil, err
		}
		for _, e := range evidences {
			if err := basHashMessage(digest, e); err != nil {
				return nil, err
			}
		}
		for _, p := range payments {
			if err := basHashMessage(digest, p); err != nil {
				return nil, err
			}
		}
		for _, a := range history.Attributions {
			if err := basHashMessage(digest, a); err != nil {
				return nil, err
			}
		}
		for _, d := range history.Deferrals {
			if err := basHashMessage(digest, d); err != nil {
				return nil, err
			}
		}
		if posting.GstBasis == tammyv1.GstBasis_GST_BASIS_NON_CASH && basDateInPeriod(posting.InvoiceDate, period) {
			automatic, err := r.basAutomaticCredit(ctx, org, posting.GetJournalId())
			if err != nil {
				return nil, err
			}
			for _, a := range posting.Allocations {
				if automatic || a.EligibleGst.MinorUnits == 0 {
					line := basPostingSource(posting, a, period)
					if automatic {
						line.ClaimedGst = proto.Clone(a.EligibleGst).(*tammyv1.Money)
					}
					line.Evidence = basEvidenceRef(evidences[0])
					out.Sources = append(out.Sources, line)
				}
			}
		}
		if posting.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH {
			for _, payment := range payments {
				if !basDateInPeriod(payment.ActualPaymentDate, period) {
					continue
				}
				automatic, err := r.basAutomaticCredit(ctx, org, payment.JournalId)
				if err != nil {
					return nil, err
				}
				evidence := basEvidenceAt(evidences, payment.AccountingVersion)
				if evidence == nil {
					return nil, ErrRepository
				}
				for _, a := range payment.Allocations {
					if automatic || a.EligibleGst.MinorUnits == 0 {
						line := basPaymentSource(posting, payment, a, period)
						if automatic {
							line.ClaimedGst = proto.Clone(a.EligibleGst).(*tammyv1.Money)
						}
						line.Evidence = basEvidenceRef(evidence)
						out.Sources = append(out.Sources, line)
					}
				}
			}
		}
		for _, a := range history.Attributions {
			if !basSamePeriod(a.ClaimPeriod, period) {
				continue
			}
			line, err := basAttributionSource(posting, payments, evidences, a)
			if err != nil {
				return nil, err
			}
			out.Sources = append(out.Sources, line)
		}
		if err := r.basPostingReviews(out, state, payments, evidences, history, period, asOf); err != nil {
			return nil, err
		}
		if err := basSnapshotBounds(out); err != nil {
			return nil, err
		}
	}
	candidates, err := r.LegacyCandidates(ctx, org, period, balance)
	if err != nil {
		return nil, err
	}
	for _, c := range candidates {
		if err := basHashMessage(digest, c.Source); err != nil {
			return nil, err
		}
		if c.Document != nil {
			if err := basHashMessage(digest, c.Document); err != nil {
				return nil, err
			}
		}
		if c.Opening != nil {
			if err := basHashMessage(digest, c.Opening.Input); err != nil {
				return nil, err
			}
			for _, value := range []string{c.Opening.OrganisationID, c.Opening.ID, c.Opening.ConversionID, c.Opening.JournalID, c.Opening.State} {
				basHashBytes(digest, []byte(value))
			}
			for _, value := range []uint64{c.Opening.SourceVersion, c.Opening.FinancialRevision} {
				binary.BigEndian.PutUint64(version[:], value)
				basHashBytes(digest, version[:])
			}
			basHashBytes(digest, c.Opening.SourceHash)
			basHashBytes(digest, c.Opening.ConversionHash)
			journals, _ := accounting.NewJournalRepository(r.executor)
			journal, err := journals.Get(ctx, c.Opening.JournalID)
			if err != nil || journal.OrganisationId != org {
				return nil, ErrRepository
			}
			if err := basHashMessage(digest, journal); err != nil {
				return nil, err
			}
		}
		for _, prior := range c.PriorSources {
			if err := basHashMessage(digest, prior); err != nil {
				return nil, err
			}
		}
		for _, review := range c.Reviews {
			if err := basHashMessage(digest, review); err != nil {
				return nil, err
			}
		}
		flags := []byte{byte(c.Status), 0, 0}
		if c.Excluded {
			flags[1] = 1
		}
		if c.EvidenceOnly {
			flags[2] = 1
		}
		basHashBytes(digest, flags)
		if !c.Excluded && !c.EvidenceOnly {
			if c.CandidateGST < 0 || c.CandidateGST > math.MaxInt64-out.LegacyCandidateGST.MinorUnits {
				return nil, ErrRepository
			}
			out.LegacyCandidateGST.MinorUnits += c.CandidateGST
			if profile.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH {
				out.ExcludedCashPurchaseCount++
			}
		}
		base := basLegacyCandidateReview(c, period)
		if len(c.Reviews) == 0 {
			out.Reviews = append(out.Reviews, base)
		}
		for _, record := range c.Reviews {
			line := proto.Clone(base).(*tammyv1.BasPurchaseReview)
			line.LegacyReview = basLegacyDecision(record)
			line.SourceRevision = record.SourceRevision
			line.SourceHash = append([]byte(nil), record.SourceHash...)
			line.Reason = record.Review.Reason
			if c.Excluded || c.EvidenceOnly {
				line.Kind = tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_RESOLVED_LEGACY
			}
			out.Reviews = append(out.Reviews, line)
			for _, group := range []struct {
				kind tammyv1.BasPurchaseLegacySupportKind
				refs []*tammyv1.PurchaseRetainedSourceRef
			}{{tammyv1.BasPurchaseLegacySupportKind_BAS_PURCHASE_LEGACY_SUPPORT_KIND_PAYMENT, record.Review.SupportingPayments}, {tammyv1.BasPurchaseLegacySupportKind_BAS_PURCHASE_LEGACY_SUPPORT_KIND_EVIDENCE, record.Review.SupportingEvidence}} {
				for index, ref := range group.refs {
					out.LegacySupport = append(out.LegacySupport, &tammyv1.BasPurchaseLegacySupport{ReviewId: record.Id, Kind: group.kind, Sequence: uint32(index + 1), Source: proto.Clone(ref).(*tammyv1.PurchaseRetainedSourceRef)})
				}
			}
		}
		if err := basSnapshotBounds(out); err != nil {
			return nil, err
		}
	}
	// Save and reload share this stable order. UUIDv7 entropy within one
	// millisecond does not imply the order in which decisions were appended.
	sort.Slice(out.LegacySupport, func(i, j int) bool {
		a, b := out.LegacySupport[i], out.LegacySupport[j]
		if a.ReviewId != b.ReviewId {
			return a.ReviewId < b.ReviewId
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Sequence < b.Sequence
	})
	view := &tammyv1.BasWorkpaper{OrganisationId: org, PeriodStart: period.PeriodStart, PeriodEnd: period.PeriodEnd, GstBasis: profile.GstBasis, PurchaseSources: out.Sources, PurchaseReviews: out.Reviews, LegacyPurchaseSupport: out.LegacySupport, LegacyCandidateGst: out.LegacyCandidateGST}
	if err := basHashMessage(digest, view); err != nil {
		return nil, err
	}
	out.Fingerprint = digest.Sum(nil)
	return out, nil
}

func (r *Repository) basRetainedRuleMetadata(ctx context.Context, digest hash.Hash) error {
	rule, err := r.LowValueExceptionRule(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		basHashBytes(digest, []byte("GST_EXCEPTION_RULE_NOT_RETAINED"))
		return nil
	}
	if err != nil {
		return err
	}
	basHashBytes(digest, []byte("GST_EXCEPTION_RULE_RETAINED"))
	if err := basHashMessage(digest, rule.Source); err != nil {
		return err
	}
	// LowValueExceptionRule validates every retained rule scalar and byte.
	// The installation instant is provenance too, even though it changes no GST.
	var stamp string
	rows, err := r.executor.QueryContext(ctx, `SELECT retained_at FROM rule_bundles WHERE id=?`, rule.Source.SourceId)
	if err != nil || (purchaseRevisionRow{rows: rows}).Scan(&stamp) != nil {
		return ErrRepository
	}
	basHashBytes(digest, []byte(stamp))
	return nil
}

func basSnapshotBounds(s *BASSnapshot) error {
	if len(s.Sources) > maximumBASPurchaseSources || len(s.Reviews) > maximumBASPurchaseSources || len(s.LegacySupport) > maximumBASPurchaseSources {
		return ErrSourceBound
	}
	return nil
}
func basHashBytes(h hash.Hash, value []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(value)))
	_, _ = h.Write(n[:])
	_, _ = h.Write(value)
}
func basHashMessage(h hash.Hash, value proto.Message) error {
	content, err := purchaseBytes(value)
	if err != nil {
		return err
	}
	basHashBytes(h, content)
	return nil
}
func basDateInPeriod(d *tammyv1.CivilDate, p *tammyv1.PurchaseClaimPeriod) bool {
	return validPurchaseDate(d) && purchaseDateKey(d) >= purchaseDateKey(p.PeriodStart) && purchaseDateKey(d) <= purchaseDateKey(p.PeriodEnd)
}
func basSamePeriod(a, b *tammyv1.PurchaseClaimPeriod) bool { return proto.Equal(a, b) }
func basEvidenceAt(values []*tammyv1.PurchaseTaxEvidence, version uint64) *tammyv1.PurchaseTaxEvidence {
	var selected *tammyv1.PurchaseTaxEvidence
	for _, v := range values {
		if v.AccountingVersion < version {
			selected = v
		}
	}
	return selected
}
func basEvidenceRef(e *tammyv1.PurchaseTaxEvidence) *tammyv1.PurchaseRetainedSourceRef {
	return &tammyv1.PurchaseRetainedSourceRef{SourceId: e.Id, SourceVersion: e.AccountingVersion, SourceHash: append([]byte(nil), e.SourceHash...)}
}
func (r *Repository) basAutomaticCredit(ctx context.Context, org, journal string) (bool, error) {
	if journal == "" {
		return false, nil
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT COALESCE(SUM(l.debit_minor),0) FROM journal_lines l JOIN accounts a ON a.id=l.account_id AND a.organisation_id=? WHERE l.journal_id=? AND a.code='1150'`, org, journal)
	var amount int64
	if err != nil || (purchaseRevisionRow{rows: rows}).Scan(&amount) != nil || amount < 0 {
		return false, ErrRepository
	}
	return amount > 0, nil
}
func basPostingSource(p *tammyv1.PurchasePosting, a *tammyv1.PurchaseAllocationInput, period *tammyv1.PurchaseClaimPeriod) *tammyv1.BasPurchaseSourceLine {
	return &tammyv1.BasPurchaseSourceLine{PostingId: p.Id, PostingHash: append([]byte(nil), p.SourceHash...), DocumentId: p.DocumentId, OriginalContentHash: append([]byte(nil), p.OriginalContentHash...), ReviewedSnapshotHash: append([]byte(nil), p.ReviewedSnapshotHash...), AllocationSequence: a.Sequence, Use: a.Use, BasClassification: a.BasClassification, CashFlowClassification: a.CashFlowClassification, InvoiceDate: cloneClaimDate(p.InvoiceDate), NormalTaxEventDate: cloneClaimDate(p.InvoiceDate), ClaimPeriod: proto.Clone(period).(*tammyv1.PurchaseClaimPeriod), AllocatedGross: proto.Clone(a.Gross).(*tammyv1.Money), SupplierGst: proto.Clone(a.SupplierGst).(*tammyv1.Money), EligibleGst: proto.Clone(a.EligibleGst).(*tammyv1.Money), ClaimedGst: purchaseJournalMoney(0), SourceRevision: 1, SourceHash: append([]byte(nil), p.SourceHash...), AccountId: proto.String(a.AccountId), AccountVersion: proto.Uint64(a.AccountVersion), SupplierName: proto.String(p.SupplierName), InvoiceNumber: proto.String(p.InvoiceNumber)}
}
func basPaymentSource(p *tammyv1.PurchasePosting, payment *tammyv1.PurchasePayment, a *tammyv1.PurchasePaymentAllocation, period *tammyv1.PurchaseClaimPeriod) *tammyv1.BasPurchaseSourceLine {
	line := basPostingSource(p, p.Allocations[a.Sequence-1], period)
	id := payment.Id
	line.PaymentId = &id
	line.PaymentHash = append([]byte(nil), payment.SourceHash...)
	line.ActualPaymentDate = cloneClaimDate(payment.ActualPaymentDate)
	line.NormalTaxEventDate = cloneClaimDate(payment.ActualPaymentDate)
	line.AllocatedGross = proto.Clone(a.Gross).(*tammyv1.Money)
	line.SupplierGst = proto.Clone(a.SupplierGst).(*tammyv1.Money)
	line.EligibleGst = proto.Clone(a.EligibleGst).(*tammyv1.Money)
	line.SourceRevision = payment.AccountingVersion
	line.SourceHash = append([]byte(nil), payment.SourceHash...)
	return line
}
func basAttributionSource(p *tammyv1.PurchasePosting, payments []*tammyv1.PurchasePayment, evidences []*tammyv1.PurchaseTaxEvidence, a *tammyv1.PurchaseGstAttribution) (*tammyv1.BasPurchaseSourceLine, error) {
	var line *tammyv1.BasPurchaseSourceLine
	if ref := a.Portion.GetPosting(); ref != nil {
		line = basPostingSource(p, p.Allocations[ref.AllocationSequence-1], a.ClaimPeriod)
	} else if ref := a.Portion.GetPayment(); ref != nil {
		for _, payment := range payments {
			if payment.Id == ref.PaymentId {
				line = basPaymentSource(p, payment, payment.Allocations[ref.AllocationSequence-1], a.ClaimPeriod)
				break
			}
		}
	}
	if line == nil {
		return nil, ErrRepository
	}
	for _, e := range evidences {
		if e.Id == a.Portion.EvidenceId {
			line.Evidence = basEvidenceRef(e)
		}
	}
	if line.Evidence == nil {
		return nil, ErrRepository
	}
	id := a.Id
	line.AttributionId = &id
	line.AttributionHash = append([]byte(nil), a.SourceHash...)
	line.ReleasePostingDate = cloneClaimDate(a.ReleasePostingDate)
	line.ClaimedGst = proto.Clone(a.ClaimedGst).(*tammyv1.Money)
	line.SourceRevision = a.AccountingVersion
	line.SourceHash = append([]byte(nil), a.SourceHash...)
	return line, nil
}

func basLegacyDecision(record *tammyv1.LegacyPurchaseReview) *tammyv1.BasPurchaseLegacyReview {
	v := record.Review
	return &tammyv1.BasPurchaseLegacyReview{Id: record.Id, OrganisationId: record.OrganisationId, Source: proto.Clone(v.Source).(*tammyv1.LegacyPurchaseSourceRef), AffectedPeriod: proto.Clone(v.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod), ReportingBalanceDate: cloneClaimDate(v.ReportingBalanceDate), Disposition: v.Disposition, PreviouslyClaimedPeriod: v.PreviouslyClaimedPeriod, ClaimEvidence: v.ClaimEvidence, LinkedPostingId: v.LinkedPostingId, Reason: v.Reason, SourceRevision: record.SourceRevision, SourceHash: append([]byte(nil), record.SourceHash...), ActorUserId: record.ActorUserId, RecordedAt: record.RecordedAt}
}
func basLegacyCandidateReview(c *LegacyCandidate, period *tammyv1.PurchaseClaimPeriod) *tammyv1.BasPurchaseReview {
	_, _, version, sourceHash, _, _ := legacySourceScope(c.Source)
	line := &tammyv1.BasPurchaseReview{LegacySource: proto.Clone(c.Source).(*tammyv1.LegacyPurchaseSourceRef), Status: c.Status, AffectedPeriod: proto.Clone(period).(*tammyv1.PurchaseClaimPeriod), EligibleGst: purchaseJournalMoney(c.CandidateGST), UnclaimedGst: purchaseJournalMoney(c.CandidateGST), SourceRevision: version, SourceHash: append([]byte(nil), sourceHash...), Reason: "Legacy source needs review of eligibility, settlement and prior GST claims.", Kind: tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_OLD_LEGACY_DOCUMENT}
	if c.Document != nil {
		line.InvoiceDate = cloneClaimDate(c.Document.Candidate.DocumentDate)
		if line.InvoiceDate == nil {
			line.Kind = tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_UNDATED_LEGACY_DOCUMENT
		}
	}
	if c.Opening != nil {
		line.Kind = tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_OPENING_PAYABLE
	}
	if c.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED {
		line.Kind = tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_UNSUPPORTED_SETTLEMENT
		line.Reason = "Existing settlement or contradictory legacy facts need supported ownership and claim-period review."
	}
	if c.Excluded || c.EvidenceOnly {
		line.UnclaimedGst = purchaseJournalMoney(0)
		line.Reason = "Retained legacy decision excludes an additional purchase credit in this scope."
	}
	return line
}

func (r *Repository) basPostingReviews(out *BASSnapshot, state *tammyv1.PurchaseAccounting, payments []*tammyv1.PurchasePayment, evidences []*tammyv1.PurchaseTaxEvidence, history VerifiedClaimHistory, period *tammyv1.PurchaseClaimPeriod, asOf time.Time) error {
	p := state.Posting
	base := func(kind tammyv1.BasPurchaseReviewKind, status tammyv1.PurchaseReviewStatus, eligible, unclaimed int64, reason string) *tammyv1.BasPurchaseReview {
		id := p.Id
		return &tammyv1.BasPurchaseReview{PostingId: &id, DocumentId: proto.String(p.DocumentId), Status: status, AffectedPeriod: proto.Clone(period).(*tammyv1.PurchaseClaimPeriod), InvoiceDate: cloneClaimDate(p.InvoiceDate), EligibleGst: purchaseJournalMoney(eligible), UnclaimedGst: purchaseJournalMoney(unclaimed), Evidence: basEvidenceRef(evidences[len(evidences)-1]), SourceRevision: state.AccountingVersion, SourceHash: append([]byte(nil), p.SourceHash...), Reason: reason, Kind: kind}
	}
	if state.ReviewOutcomes.Evidence != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
		for _, source := range out.Sources {
			if source.PostingId == p.Id && source.ClaimedGst.MinorUnits > 0 {
				latest := evidences[len(evidences)-1]
				line := base(tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_MISSING_EVIDENCE, state.ReviewOutcomes.Evidence, state.EligibleGst.MinorUnits, 0, "The latest GST evidence review needs resolution; this workpaper retains the previously accepted credit and its original evidence provenance.")
				line.SourceHash = append([]byte(nil), latest.SourceHash...)
				out.Reviews = append(out.Reviews, line)
				break
			}
		}
	}
	if state.DeferredGst.MinorUnits > 0 && purchaseDateKey(p.InvoiceDate) <= purchaseDateKey(period.PeriodEnd) {
		out.Reviews = append(out.Reviews, base(tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_EXCLUDED_UNPAID, tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR, state.EligibleGst.MinorUnits, state.DeferredGst.MinorUnits, "Owned purchase GST remains deferred to the extent no actual payment has been recorded."))
	}
	remaining := map[string]VerifiedDeferralPortion{}
	for _, v := range history.DeferralPortions {
		remaining[v.DeferralID+":"+strconv.FormatUint(uint64(v.Sequence), 10)] = v
	}
	selectedDeferral := map[string]string{}
	for _, d := range history.Deferrals {
		for i, portion := range d.Portions {
			retained := remaining[d.Id+":"+strconv.Itoa(i+1)]
			if retained.UnclaimedGST != nil && retained.UnclaimedGST.MinorUnits > 0 {
				key := basPortionKey(portion)
				if selectedDeferral[key] == "" || basSamePeriod(d.AffectedPeriod, period) {
					selectedDeferral[key] = d.Id
				}
			}
		}
	}
	covered := map[string]bool{}
	var coveredGST int64
	for _, d := range history.Deferrals {
		for index, portion := range d.Portions {
			retained, found := remaining[d.Id+":"+strconv.Itoa(index+1)]
			if !found || retained.DeferredGST == nil || retained.UnclaimedGST == nil || retained.DeferredGST.MinorUnits <= 0 || retained.UnclaimedGST.MinorUnits < 0 {
				return ErrRepository
			}
			amount := retained.UnclaimedGST.MinorUnits
			key := basPortionKey(portion)
			if covered[key] || selectedDeferral[key] != d.Id {
				// Retain each immutable decision while counting a physical source
				// portion only once across period-specific deferral decisions.
				amount = 0
			} else if amount > 0 {
				if amount > math.MaxInt64-coveredGST {
					return ErrRepository
				}
				coveredGST += amount
				covered[key] = true
			}
			line := base(tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_DEFERRED_FUTURE_CLAIM, tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR, retained.DeferredGST.MinorUnits, amount, d.Reason)
			if amount > 0 && !basSamePeriod(d.AffectedPeriod, period) {
				line.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW
				line.Reason = "The retained deferral resolves its affected period only; this period needs an explicit supported claim or deferral decision."
			}
			if amount > 0 && state.ReviewOutcomes.Evidence != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
				line.Status = state.ReviewOutcomes.Evidence
				line.Reason = "The future credit remains outstanding, but the current GST evidence review needs resolution."
			}
			if amount > 0 && purchaseDateKey(purchaseAsOf(asOf)) > purchaseDateKey(portion.OrdinaryLastClaimDate) {
				line.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
				line.Reason = "The retained future claim deadline has expired; entitlement needs review."
			}
			id := d.Id
			line.DeferralId = &id
			line.SourceRevision = d.AccountingVersion
			line.SourceHash = append([]byte(nil), d.SourceHash...)
			line.FutureClaimObligation = &tammyv1.BasPurchaseFutureClaim{DeferralId: d.Id, OrganisationId: d.OrganisationId, PostingId: d.PostingId, DocumentId: d.DocumentId, AccountingVersion: d.AccountingVersion, DeferralHash: append([]byte(nil), d.SourceHash...), Portion: proto.Clone(portion).(*tammyv1.PurchaseGstSourcePortion), AffectedPeriod: proto.Clone(d.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod), TimingReview: proto.Clone(d.TimingReview).(*tammyv1.PurchaseClaimTimingReview), AllocatedDeferredGst: purchaseJournalMoney(retained.DeferredGST.MinorUnits), Reason: d.Reason, ActorUserId: d.ActorUserId, RecordedAt: d.RecordedAt}
			if ref := portion.GetPayment(); ref != nil {
				id := ref.PaymentId
				line.PaymentId = &id
				line.ActualPaymentDate = cloneClaimDate(portion.NormalEventDate)
			}
			out.Reviews = append(out.Reviews, line)
		}
	}
	if coveredGST > state.SuspenseGst.MinorUnits {
		return ErrRepository
	}
	if unresolved := state.SuspenseGst.MinorUnits - coveredGST; unresolved > 0 && purchaseDateKey(p.InvoiceDate) <= purchaseDateKey(period.PeriodEnd) {
		kind, status := tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_MISSING_EVIDENCE, state.ReviewOutcomes.Evidence
		if status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
			kind = tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_CLAIM_TIMING
			status = state.ReviewOutcomes.Timing
			if status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
				status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW
			}
		}
		out.Reviews = append(out.Reviews, base(kind, status, state.EligibleGst.MinorUnits, unresolved, "Eligible GST remains in evidence suspense until an explicit supported claim-period attribution."))
	}
	return nil
}
func basPortionKey(p *tammyv1.PurchaseGstSourcePortion) string {
	if v := p.GetPosting(); v != nil {
		return "POSTING:" + v.PostingId + ":" + strconv.FormatUint(uint64(v.AllocationSequence), 10)
	}
	if v := p.GetPayment(); v != nil {
		return "PAYMENT:" + v.PaymentId + ":" + strconv.FormatUint(uint64(v.AllocationSequence), 10)
	}
	return ""
}
