package purchases

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrClaimBalance = errors.New("purchases: selected source has no eligible unclaimed suspense")

// ClaimTimingError preserves the blocker for later reporting/review decisions.
type ClaimTimingError struct{ Status ClaimTimingStatus }

func (e *ClaimTimingError) Error() string { return "purchases: claim timing " + string(e.Status) }
func (e *ClaimTimingError) Unwrap() error { return ErrInvalidClaimTiming }

const purchaseClaimByteLimit = 4 << 20

type ownedClaimPortion struct {
	kind, paymentID string
	sequence        uint32
	amount          int64
	eventRef        *tammyv1.PurchaseRetainedSourceRef
	eventDate       *tammyv1.CivilDate
	payment         *tammyv1.PurchasePayment
	evidence        *tammyv1.PurchaseTaxEvidence
}

func (r *Repository) Attribute(ctx context.Context, request *tammyv1.AttributePurchaseGstRequest) (*tammyv1.AttributePurchaseGstResponse, error) {
	if request == nil || !validClaimCommand(r, ctx, request.CommandContext, request.ExpectedAccountingVersion) || !validStoredClaimPeriod(request.ClaimPeriod) || purchaseDateString(request.ReleasePostingDate) == "" || (request.DeferralId != nil && !ids.IsCanonicalV7(*request.DeferralId)) {
		return nil, ErrInvalidClaimTiming
	}
	now := r.now().UTC()
	state, err := r.claimState(ctx, request.OrganisationId, request.DocumentId, request.ExpectedAccountingVersion, now)
	if err != nil {
		return nil, err
	}
	selected, amount, err := r.selectClaimPortions(ctx, state.Posting, request.Portions, request.ClaimPeriod, request.ReleasePostingDate, request.TimingReview, ClaimTimingDelayed, now, state.AccountingVersion+1)
	if err != nil {
		return nil, err
	}
	if amount > state.SuspenseGst.MinorUnits {
		return nil, ErrClaimBalance
	}
	deferrals, err := r.loadDeferrals(ctx, state.Posting)
	if err != nil {
		return nil, err
	}
	if err := verifyAttributionDeferralLink(request.Portions, request.DeferralId, deferrals, state.AccountingVersion+1); err != nil {
		return nil, err
	}
	writer, _ := NewJournalWriter(r.executor, func() time.Time { return now }, r.newID)
	if err := r.claimPeriodOpen(ctx, state.OrganisationId, request.ClaimPeriod); err != nil {
		return nil, err
	}
	if err := writer.IsPostingDateOpen(ctx, state.OrganisationId, request.ReleasePostingDate); err != nil {
		return nil, err
	}
	// Every accepted portion shares one aggregate journal and ledger+tax revision.
	identities := make([]string, len(selected))
	for i := range identities {
		identities[i], err = r.newID()
		if err != nil || !ids.IsCanonicalV7(identities[i]) {
			return nil, ErrRepository
		}
	}
	if err := preflightAttributionResponse(request, state, selected, identities, now); err != nil {
		return nil, err
	}
	revision, err := ReservePurchaseRevision(ctx, r.executor, request.CommandContext.IdempotencyKey, purchaseService+"AttributePurchaseGst", true, now)
	if err != nil {
		return nil, err
	}
	journal, err := writer.ReleaseSuspense(ctx, state.OrganisationId, request.CommandContext.IdempotencyKey, request.ReleasePostingDate, amount, revision.Financial)
	if err != nil {
		return nil, err
	}
	response := &tammyv1.AttributePurchaseGstResponse{Journal: journal, Accounting: state}
	for i, s := range selected {
		a := &tammyv1.PurchaseGstAttribution{Id: identities[i], OrganisationId: state.OrganisationId, PostingId: state.Posting.Id, DocumentId: state.DocumentId, AccountingVersion: state.AccountingVersion + 1, Portion: proto.Clone(request.Portions[i]).(*tammyv1.PurchaseGstSourcePortion), ClaimPeriod: proto.Clone(request.ClaimPeriod).(*tammyv1.PurchaseClaimPeriod), ReleasePostingDate: cloneClaimDate(request.ReleasePostingDate), TimingReview: proto.Clone(request.TimingReview).(*tammyv1.PurchaseClaimTimingReview), ClaimedGst: purchaseJournalMoney(s.amount), ClosesDeferralId: request.DeferralId, JournalId: journal.Id, FinancialRevision: revision.Financial, ActorUserId: request.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
		a.SourceHash, err = purchaseHash(a)
		if err != nil {
			return nil, err
		}
		if err := r.retainAttribution(ctx, request.CommandContext.IdempotencyKey, a); err != nil {
			return nil, err
		}
		response.Attributions = append(response.Attributions, a)
	}
	if err := applyAttributionState(state, response.Attributions); err != nil {
		return nil, err
	}
	if err := r.refreshClaimOutcomes(ctx, state, now); err != nil {
		return nil, err
	}
	if !purchaseResultFits(response) {
		return nil, ErrSourceBound
	}
	if err := RecordAccountingEvent(ctx, r.executor, AccountingEvent{OperationKey: request.CommandContext.IdempotencyKey, OrganisationID: state.OrganisationId, ScopeKind: "POSTING", ScopeID: state.Posting.Id, CommandType: purchaseService + "AttributePurchaseGst", AccountingVersion: state.AccountingVersion, Revision: revision, Result: response, JournalID: &journal.Id, Now: now}); err != nil {
		return nil, err
	}
	return response, nil
}

func validClaimCommand(r *Repository, ctx context.Context, command *tammyv1.CommandContext, version uint64) bool {
	return r != nil && r.executor != nil && r.now != nil && r.newID != nil && ctx != nil && command != nil && command.Authentication != nil && ids.IsCanonicalV7(command.IdempotencyKey) && ids.IsCanonicalV7(command.Authentication.ActorUserId) && version > 0 && version < math.MaxInt64
}

func (r *Repository) claimState(ctx context.Context, org, doc string, expected uint64, now time.Time) (*tammyv1.PurchaseAccounting, error) {
	if now.IsZero() {
		return nil, ErrRepository
	}
	state, err := r.getAt(ctx, org, doc, false, now)
	if err != nil {
		return nil, err
	}
	if !state.Posted || state.Posting == nil || state.AccountingVersion != expected {
		return nil, ErrConflict
	}
	profiles, _ := organisations.NewRepository(r.executor)
	profile, err := profiles.Get(ctx, org)
	if err != nil {
		return nil, err
	}
	if profile.GstBasis != state.Posting.GstBasis {
		return nil, invoices.ErrGSTBasisHistory
	}
	if _, err := invoices.CheckGSTBasisHistory(ctx, r.executor, org, state.Posting.GstHistoryWatermark); err != nil {
		return nil, err
	}
	return state, nil
}

func (r *Repository) claimPeriodOpen(ctx context.Context, org string, period *tammyv1.PurchaseClaimPeriod) error {
	writer, _ := NewJournalWriter(r.executor, r.now, r.newID)
	// Closing is cumulative through end_date: check the complete selected period.
	if err := writer.IsPostingDateOpen(ctx, org, period.PeriodStart); err != nil {
		return err
	}
	return writer.IsPostingDateOpen(ctx, org, period.PeriodEnd)
}

func validClaimReview(review *tammyv1.PurchaseClaimTimingReview, now time.Time) bool {
	return review != nil && purchaseDateString(review.ReviewedAsOf) != "" && review.AllowablePeriodConfirmed && review.OrdinaryTimeLimitConfirmed && review.EvidenceHeldConfirmed && purchaseReviewText(review.Reason, 512, true) && !now.IsZero() && purchaseDateKey(review.ReviewedAsOf) <= purchaseDateKey(purchaseAsOf(now))
}

func validStoredClaimPeriod(period *tammyv1.PurchaseClaimPeriod) bool {
	return validClaimTimingPeriod(period) && purchaseDateString(period.PeriodStart) != "" && purchaseDateString(period.PeriodEnd) != ""
}

func (r *Repository) selectClaimPortions(ctx context.Context, posting *tammyv1.PurchasePosting, portions []*tammyv1.PurchaseGstSourcePortion, period *tammyv1.PurchaseClaimPeriod, release *tammyv1.CivilDate, review *tammyv1.PurchaseClaimTimingReview, use ClaimTimingUse, now time.Time, eventVersion uint64) ([]ownedClaimPortion, int64, error) {
	if posting == nil || len(portions) == 0 || len(portions) > 200 || !validStoredClaimPeriod(period) || !validClaimReview(review, now) {
		return nil, 0, ErrInvalidClaimTiming
	}
	payments, err := r.loadPayments(ctx, posting)
	if err != nil {
		return nil, 0, err
	}
	evidences, err := r.retainedEvidenceHistory(ctx, posting)
	if err != nil {
		return nil, 0, err
	}
	var currentEvidence *tammyv1.PurchaseTaxEvidence
	for _, e := range evidences {
		if e.AccountingVersion < eventVersion && !e.RecordedAt.AsTime().After(now) {
			currentEvidence = e
		}
	}
	attributions, err := r.loadAttributions(ctx, posting)
	if err != nil {
		return nil, 0, err
	}
	seen := make(map[string]bool, len(portions))
	selected := make([]ownedClaimPortion, 0, len(portions))
	var total int64
	for _, p := range portions {
		if p == nil || proto.Size(p) > 16384 {
			return nil, 0, ErrSourceBound
		}
		s, err := r.ownedClaimSource(ctx, posting, p, payments, eventVersion)
		if err != nil {
			return nil, 0, err
		}
		key := claimPortionKey(s.kind, s.paymentID, s.sequence)
		if seen[key] {
			return nil, 0, ErrClaimBalance
		}
		seen[key] = true
		for _, a := range attributions {
			if a.value.AccountingVersion >= eventVersion {
				continue
			}
			if sameClaimPortionSource(p, a.value.Portion) {
				if a.value.ClaimedGst.MinorUnits > s.amount {
					return nil, 0, ErrRepository
				}
				s.amount -= a.value.ClaimedGst.MinorUnits
			}
		}
		if s.amount <= 0 {
			return nil, 0, ErrClaimBalance
		}
		// Selecting an older retained review cannot bypass a later withdrawal.
		// Historical replay uses the accepted event's version and instant, so a
		// later review does not change the evidence that authorized that event.
		s.evidence = currentEvidence
		if s.evidence == nil || s.evidence.Id != p.EvidenceId || !proto.Equal(s.evidence.Review.HeldDate, p.EvidenceHeldDate) {
			return nil, 0, ErrInvalidClaimTiming
		}
		rule, err := r.ResolveEvidenceRule(ctx, posting.OrganisationId, posting.DocumentId, s.evidence.Review)
		if err != nil {
			return nil, 0, err
		}
		required, err := r.reviewedRequiredReturn(ctx, posting, s, p, review, now)
		if err != nil {
			return nil, 0, err
		}
		input := ClaimTimingInput{Use: use, Basis: posting.GstBasis, InvoiceSource: &tammyv1.PurchaseRetainedSourceRef{SourceId: posting.Id, SourceVersion: 1, SourceHash: posting.SourceHash}, InvoiceDate: posting.InvoiceDate, Evidence: s.evidence.Review, ExceptionRule: rule, ClaimPeriod: period, ReleaseDate: release, ReviewedAsOf: review.ReviewedAsOf, RequiredReturn: required}
		if s.payment != nil {
			input.PaymentSource = s.eventRef
			input.PaymentDate = s.eventDate
		}
		timing, err := EvaluateClaimTiming(input)
		if err != nil {
			return nil, 0, err
		}
		if timing.Status != ClaimTimingNormalSupported && timing.Status != ClaimTimingReviewRequired {
			return nil, 0, &ClaimTimingError{Status: timing.Status}
		}
		if timing.OrdinaryLastClaimDate == nil || !proto.Equal(timing.OrdinaryLastClaimDate, p.OrdinaryLastClaimDate) {
			return nil, 0, ErrInvalidClaimTiming
		}
		// A newly accepted event cannot use a backdated reviewed-as-of date to
		// extend entitlement after the trusted command clock passes its limit.
		// Replay supplies that historical event's retained instant here.
		if purchaseDateKey(purchaseAsOf(now)) > purchaseDateKey(timing.OrdinaryLastClaimDate) {
			return nil, 0, &ClaimTimingError{Status: ClaimTimingExpired}
		}
		if !addPurchaseAmount(&total, s.amount) {
			return nil, 0, ErrClaimBalance
		}
		selected = append(selected, s)
	}
	return selected, total, nil
}

func (r *Repository) ownedClaimSource(ctx context.Context, posting *tammyv1.PurchasePosting, p *tammyv1.PurchaseGstSourcePortion, payments []*tammyv1.PurchasePayment, eventVersion uint64) (ownedClaimPortion, error) {
	s := ownedClaimPortion{}
	if p == nil || !validPurchaseDate(p.NormalEventDate) {
		return s, ErrInvalidClaimTiming
	}
	var journalID string
	if ref := p.GetPosting(); ref != nil {
		if posting.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH || ref.PostingId != posting.Id || !bytes.Equal(ref.PostingHash, posting.SourceHash) || ref.AllocationSequence == 0 || int(ref.AllocationSequence) > len(posting.Allocations) || posting.JournalId == nil {
			return s, ErrClaimBalance
		}
		s.kind = "POSTING"
		s.sequence = ref.AllocationSequence
		s.amount = posting.Allocations[s.sequence-1].EligibleGst.MinorUnits
		s.eventDate = posting.InvoiceDate
		s.eventRef = &tammyv1.PurchaseRetainedSourceRef{SourceId: posting.Id, SourceVersion: 1, SourceHash: posting.SourceHash}
		journalID = *posting.JournalId
	} else if ref := p.GetPayment(); ref != nil {
		if posting.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH || ref.PostingId != posting.Id || !bytes.Equal(ref.PostingHash, posting.SourceHash) || ref.AllocationSequence == 0 {
			return s, ErrClaimBalance
		}
		for _, payment := range payments {
			if payment.Id == ref.PaymentId && payment.AccountingVersion < eventVersion {
				s.payment = payment
				break
			}
		}
		if s.payment == nil || !bytes.Equal(ref.PaymentHash, s.payment.SourceHash) || int(ref.AllocationSequence) > len(s.payment.Allocations) {
			return s, ErrClaimBalance
		}
		s.kind = "PAYMENT"
		s.paymentID = s.payment.Id
		s.sequence = ref.AllocationSequence
		s.amount = s.payment.Allocations[s.sequence-1].EligibleGst.MinorUnits
		s.eventDate = s.payment.ActualPaymentDate
		s.eventRef = &tammyv1.PurchaseRetainedSourceRef{SourceId: s.payment.Id, SourceVersion: s.payment.AccountingVersion, SourceHash: s.payment.SourceHash}
		journalID = s.payment.JournalId
	} else {
		return s, ErrClaimBalance
	}
	if !proto.Equal(p.NormalEventDate, s.eventDate) {
		return s, ErrInvalidClaimTiming
	}
	// Automatic normal claims share this same source ledger. Read the original
	// owned journal, never current evidence or an aggregate balance.
	rows, err := r.executor.QueryContext(ctx, `SELECT COALESCE(SUM(l.debit_minor),0) FROM journal_lines l JOIN accounts a ON a.id=l.account_id AND a.organisation_id=? WHERE l.journal_id=? AND a.code='1150'`, posting.OrganisationId, journalID)
	var automatic int64
	if err != nil || (purchaseRevisionRow{rows: rows}).Scan(&automatic) != nil {
		return s, ErrRepository
	}
	if automatic > 0 {
		s.amount = 0
	}
	return s, nil
}

// This is a MANUAL semantic review. Core verifies the retained supporting
// document and mechanical binding; it does not authenticate an external ATO
// date or infer a nominal due date from reporting frequency.
func (r *Repository) reviewedRequiredReturn(ctx context.Context, posting *tammyv1.PurchasePosting, s ownedClaimPortion, p *tammyv1.PurchaseGstSourcePortion, review *tammyv1.PurchaseClaimTimingReview, now time.Time) (*ReviewedRequiredReturn, error) {
	ref := p.DueDateEvidence
	if !validPurchaseSourceRef(ref) || ref.SourceVersion > math.MaxInt64 || ref.SourceId == posting.DocumentId || ref.SourceId == s.evidence.Review.GetSource().GetSourceId() {
		return nil, &ClaimTimingError{Status: ClaimTimingUndeterminableLimit}
	}
	doc, err := r.document(ctx, posting.OrganisationId, ref.SourceId)
	if err != nil {
		return nil, err
	}
	if doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || doc.Version != ref.SourceVersion || !bytes.Equal(doc.Sha256, ref.SourceHash) || doc.ReviewedAt == nil || doc.ReviewedAt.CheckValid() != nil || doc.ReviewedAt.AsTime().After(now) || purchaseDateKey(purchaseAsOf(doc.ReviewedAt.AsTime().UTC())) > purchaseDateKey(review.ReviewedAsOf) {
		return nil, &ClaimTimingError{Status: ClaimTimingUndeterminableLimit}
	}
	if !validStoredClaimPeriod(p.EarliestOtherwiseEligiblePeriod) || !claimTimingPeriodContains(p.EarliestOtherwiseEligiblePeriod, s.eventDate) || purchaseDateString(p.RequiredReturnDueDate) == "" || purchaseDateKey(p.RequiredReturnDueDate) < purchaseDateKey(p.EarliestOtherwiseEligiblePeriod.PeriodEnd) {
		return nil, ErrInvalidClaimTiming
	}
	return &ReviewedRequiredReturn{EventSource: cloneClaimRef(s.eventRef), NormalEventDate: cloneClaimDate(s.eventDate), EarliestOtherwiseEligiblePeriod: proto.Clone(p.EarliestOtherwiseEligiblePeriod).(*tammyv1.PurchaseClaimPeriod), DueDate: cloneClaimDate(p.RequiredReturnDueDate), Evidence: cloneClaimRef(ref)}, nil
}

func preflightAttributionResponse(request *tammyv1.AttributePurchaseGstRequest, state *tammyv1.PurchaseAccounting, selected []ownedClaimPortion, identities []string, now time.Time) error {
	preview := &tammyv1.AttributePurchaseGstResponse{Accounting: proto.Clone(state).(*tammyv1.PurchaseAccounting)}
	for i, s := range selected {
		a := &tammyv1.PurchaseGstAttribution{Id: identities[i], OrganisationId: state.OrganisationId, PostingId: state.Posting.Id, DocumentId: state.DocumentId, AccountingVersion: state.AccountingVersion + 1, Portion: request.Portions[i], ClaimPeriod: request.ClaimPeriod, ReleasePostingDate: request.ReleasePostingDate, TimingReview: request.TimingReview, ClaimedGst: purchaseJournalMoney(s.amount), ClosesDeferralId: request.DeferralId, JournalId: request.CommandContext.IdempotencyKey, FinancialRevision: math.MaxInt64, SourceHash: make([]byte, 32), ActorUserId: request.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
		if proto.Size(a) > 32768 {
			return ErrSourceBound
		}
		preview.Attributions = append(preview.Attributions, a)
	}
	if err := applyAttributionState(preview.Accounting, preview.Attributions); err != nil {
		return err
	}
	// The owned release journal has two fixed-shape lines. These maximum-width
	// values bound its actual encoding without writing or reserving a revision.
	journal := &tammyv1.Journal{Id: request.CommandContext.IdempotencyKey, OrganisationId: state.OrganisationId, Version: 1, Source: tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION, State: tammyv1.JournalState_JOURNAL_STATE_POSTED, PostingDate: request.ReleasePostingDate, Memo: "Purchase accounting", FinancialRevision: math.MaxInt64, PostedAt: timestamppb.New(now), TotalDebits: purchaseJournalMoney(math.MaxInt64), TotalCredits: purchaseJournalMoney(math.MaxInt64)}
	for i := 0; i < 2; i++ {
		journal.Lines = append(journal.Lines, &tammyv1.JournalLine{Id: journal.Id, JournalId: journal.Id, AccountId: journal.Id, Sequence: uint32(i + 1), Debit: purchaseJournalMoney(math.MaxInt64), Credit: purchaseJournalMoney(math.MaxInt64)})
	}
	preview.Journal = journal
	if !purchaseResultFits(preview) || proto.Size(preview) > 1<<20 {
		return ErrSourceBound
	}
	return nil
}

func claimPortionKey(kind, payment string, sequence uint32) string {
	return fmt.Sprintf("%s/%s/%d", kind, payment, sequence)
}
func sameClaimPortionSource(a, b *tammyv1.PurchaseGstSourcePortion) bool {
	if a == nil || b == nil {
		return false
	}
	if a.GetPosting() != nil && b.GetPosting() != nil {
		return proto.Equal(a.GetPosting(), b.GetPosting())
	}
	return a.GetPayment() != nil && b.GetPayment() != nil && proto.Equal(a.GetPayment(), b.GetPayment())
}
func applyAttributionState(state *tammyv1.PurchaseAccounting, values []*tammyv1.PurchaseGstAttribution) error {
	if len(values) == 0 {
		return ErrRepository
	}
	version := values[0].AccountingVersion
	var amount int64
	for _, a := range values {
		if a.AccountingVersion != version || !validPurchaseMoney(a.ClaimedGst) || a.ClaimedGst.MinorUnits <= 0 || !addPurchaseAmount(&amount, a.ClaimedGst.MinorUnits) {
			return ErrRepository
		}
	}
	if amount > state.SuspenseGst.MinorUnits || !addPurchaseAmount(&state.AttributedGst.MinorUnits, amount) {
		return ErrRepository
	}
	state.SuspenseGst.MinorUnits -= amount
	state.AccountingVersion = version
	return nil
}

func (r *Repository) retainAttribution(ctx context.Context, operation string, a *tammyv1.PurchaseGstAttribution) error {
	content, err := purchaseBytes(a)
	if err != nil || len(content) > 32768 {
		return ErrSourceBound
	}
	scalars, err := attributionScalars(operation, a)
	if err != nil {
		return err
	}
	scalars["attribution_proto"] = content
	return r.insertClaimScalars(ctx, "purchase_gst_attributions_v1", scalars)
}
func nullableClaimPayment(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func verifyAttributionDeferralLink(portions []*tammyv1.PurchaseGstSourcePortion, selectedID *string, deferrals []*tammyv1.PurchaseGstDeferral, eventVersion uint64) error {
	var selected *tammyv1.PurchaseGstDeferral
	if selectedID != nil {
		for _, d := range deferrals {
			if d.Id == *selectedID && d.AccountingVersion < eventVersion {
				selected = d
				break
			}
		}
		if selected == nil {
			return ErrConflict
		}
	}
	for _, p := range portions {
		previous := false
		for _, d := range deferrals {
			if d.AccountingVersion >= eventVersion {
				continue
			}
			for _, dp := range d.Portions {
				if sameClaimPortionSource(p, dp) {
					previous = true
					break
				}
			}
		}
		if previous && selected == nil {
			return ErrConflict
		}
		if selected != nil {
			matched := false
			for _, dp := range selected.Portions {
				if sameClaimPortionSource(p, dp) {
					matched = true
					break
				}
			}
			if !matched {
				return ErrClaimBalance
			}
		}
	}
	return nil
}

type retainedAttribution struct {
	operation string
	value     *tammyv1.PurchaseGstAttribution
}

func (r *Repository) loadAttributions(ctx context.Context, p *tammyv1.PurchasePosting) ([]retainedAttribution, error) {
	rows, err := r.executor.QueryContext(ctx, `SELECT operation_key,attribution_proto,source_hash FROM purchase_gst_attributions_v1 WHERE posting_id=? AND organisation_id=? ORDER BY accounting_version,rowid`, p.Id, p.OrganisationId)
	if err != nil {
		return nil, ErrRepository
	}
	var result []retainedAttribution
	var budget int
	for rows.Next() {
		var operation string
		var content, hash []byte
		if rows.Scan(&operation, &content, &hash) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		budget += len(content)
		if budget > purchaseClaimByteLimit {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
		a := &tammyv1.PurchaseGstAttribution{}
		if proto.Unmarshal(content, a) != nil || len(content) > 32768 || !canonicalPurchaseBytes(a, content) || !verifyPurchaseHash(a, hash) || !bytes.Equal(a.SourceHash, hash) || !validRetainedAttribution(a, p) || !ids.IsCanonicalV7(operation) {
			_ = rows.Close()
			return nil, ErrRepository
		}
		result = append(result, retainedAttribution{operation, a})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	for _, a := range result {
		scalars, err := attributionScalars(a.operation, a.value)
		if err != nil {
			return nil, err
		}
		if err := r.verifyClaimScalars(ctx, "purchase_gst_attributions_v1", scalars); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func validRetainedAttribution(a *tammyv1.PurchaseGstAttribution, p *tammyv1.PurchasePosting) bool {
	return a != nil && ids.IsCanonicalV7(a.Id) && a.OrganisationId == p.OrganisationId && a.PostingId == p.Id && a.DocumentId == p.DocumentId && a.AccountingVersion > 1 && a.AccountingVersion <= math.MaxInt64 && ids.IsCanonicalV7(a.ActorUserId) && ids.IsCanonicalV7(a.JournalId) && a.FinancialRevision > 0 && a.FinancialRevision <= math.MaxInt64 && a.RecordedAt != nil && a.RecordedAt.CheckValid() == nil && a.Portion != nil && validClaimTimingPeriod(a.ClaimPeriod) && validPurchaseDate(a.ReleasePostingDate) && a.TimingReview != nil && validPurchaseMoney(a.ClaimedGst) && a.ClaimedGst.MinorUnits > 0 && (a.ClosesDeferralId == nil || ids.IsCanonicalV7(*a.ClosesDeferralId))
}
func attributionScalars(operation string, a *tammyv1.PurchaseGstAttribution) (map[string]any, error) {
	m, err := claimPortionScalars(a.Portion)
	if err != nil {
		return nil, err
	}
	m["id"] = a.Id
	m["operation_key"] = operation
	m["posting_id"] = a.PostingId
	m["organisation_id"] = a.OrganisationId
	m["document_id"] = a.DocumentId
	m["accounting_version"] = a.AccountingVersion
	m["claim_period_start"] = purchaseDateString(a.ClaimPeriod.PeriodStart)
	m["claim_period_end"] = purchaseDateString(a.ClaimPeriod.PeriodEnd)
	m["release_posting_date"] = purchaseDateString(a.ReleasePostingDate)
	m["reviewed_as_of"] = purchaseDateString(a.TimingReview.ReviewedAsOf)
	m["timing_reason"] = a.TimingReview.Reason
	m["claimed_gst_minor"] = a.ClaimedGst.MinorUnits
	m["currency_code"] = "AUD"
	m["closes_deferral_id"] = purchaseNullable(a.ClosesDeferralId)
	m["journal_id"] = a.JournalId
	m["financial_revision"] = a.FinancialRevision
	m["source_hash"] = a.SourceHash
	m["actor_user_id"] = a.ActorUserId
	m["created_at"] = a.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano)
	return m, nil
}
func scalarClaimPortion(p *tammyv1.PurchaseGstSourcePortion) (ownedClaimPortion, error) {
	s := ownedClaimPortion{}
	if p == nil || !validPurchaseDate(p.NormalEventDate) || !validClaimTimingPeriod(p.EarliestOtherwiseEligiblePeriod) || !validPurchaseDate(p.RequiredReturnDueDate) || !validPurchaseDate(p.OrdinaryLastClaimDate) || !validPurchaseSourceRef(p.DueDateEvidence) || !ids.IsCanonicalV7(p.EvidenceId) || !validPurchaseDate(p.EvidenceHeldDate) {
		return s, ErrRepository
	}
	if ref := p.GetPosting(); ref != nil {
		s.kind = "POSTING"
		s.sequence = ref.AllocationSequence
		if !ids.IsCanonicalV7(ref.PostingId) || len(ref.PostingHash) != 32 {
			return s, ErrRepository
		}
	} else if ref := p.GetPayment(); ref != nil {
		s.kind = "PAYMENT"
		s.sequence = ref.AllocationSequence
		s.paymentID = ref.PaymentId
		if !ids.IsCanonicalV7(ref.PostingId) || len(ref.PostingHash) != 32 || !ids.IsCanonicalV7(ref.PaymentId) || len(ref.PaymentHash) != 32 {
			return s, ErrRepository
		}
	} else {
		return s, ErrRepository
	}
	if s.sequence == 0 {
		return s, ErrRepository
	}
	return s, nil
}
func claimPortionScalars(p *tammyv1.PurchaseGstSourcePortion) (map[string]any, error) {
	s, err := scalarClaimPortion(p)
	if err != nil {
		return nil, err
	}
	return map[string]any{"source_kind": s.kind, "payment_id": nullableClaimPayment(s.paymentID), "allocation_sequence": s.sequence, "normal_event_date": purchaseDateString(p.NormalEventDate), "earliest_period_start": purchaseDateString(p.EarliestOtherwiseEligiblePeriod.PeriodStart), "earliest_period_end": purchaseDateString(p.EarliestOtherwiseEligiblePeriod.PeriodEnd), "required_return_due_date": purchaseDateString(p.RequiredReturnDueDate), "due_date_source_id": p.DueDateEvidence.SourceId, "due_date_source_version": p.DueDateEvidence.SourceVersion, "due_date_source_hash": p.DueDateEvidence.SourceHash, "ordinary_last_claim_date": purchaseDateString(p.OrdinaryLastClaimDate), "evidence_id": p.EvidenceId, "evidence_held_date": purchaseDateString(p.EvidenceHeldDate)}, nil
}

// These helpers accept only maps built above/below from fixed internal columns,
// never caller-provided identifiers. Nulls are compared exactly with SQL IS.
func claimScalarArgs(m map[string]any) ([]string, []any) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = m[k]
	}
	return keys, args
}
func (r *Repository) insertClaimScalars(ctx context.Context, table string, m map[string]any) error {
	keys, args := claimScalarArgs(m)
	holders := make([]string, len(keys))
	for i := range holders {
		holders[i] = "?"
	}
	_, err := r.executor.ExecContext(ctx, "INSERT INTO "+table+"("+strings.Join(keys, ",")+") VALUES("+strings.Join(holders, ",")+")", args...)
	return err
}
func (r *Repository) verifyClaimScalars(ctx context.Context, table string, m map[string]any) error {
	keys, args := claimScalarArgs(m)
	for i, k := range keys {
		keys[i] = k + " IS ?"
	}
	rows, err := r.executor.QueryContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+strings.Join(keys, " AND "), args...)
	var count int
	if err != nil || (purchaseRevisionRow{rows: rows}).Scan(&count) != nil || count != 1 {
		return ErrRepository
	}
	return nil
}
