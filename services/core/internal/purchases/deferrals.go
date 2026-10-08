package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Defer retains a supported future obligation for one named period. It has no
// release journal and never changes the source's suspense/attributed balance.
func (r *Repository) Defer(ctx context.Context, request *tammyv1.DeferPurchaseGstClaimRequest) (*tammyv1.DeferPurchaseGstClaimResponse, error) {
	if request == nil || !validClaimCommand(r, ctx, request.CommandContext, request.ExpectedAccountingVersion) || !purchaseReviewText(request.Reason, 512, true) || !validStoredClaimPeriod(request.AffectedPeriod) {
		return nil, ErrInvalidClaimTiming
	}
	now := r.now().UTC()
	state, err := r.claimState(ctx, request.OrganisationId, request.DocumentId, request.ExpectedAccountingVersion, now)
	if err != nil {
		return nil, err
	}
	selected, amount, err := r.selectClaimPortions(ctx, state.Posting, request.Portions, request.AffectedPeriod, nil, request.TimingReview, ClaimTimingDeferral, now, state.AccountingVersion+1)
	if err != nil {
		return nil, err
	}
	if amount > state.SuspenseGst.MinorUnits {
		return nil, ErrClaimBalance
	}
	if err := r.claimPeriodOpen(ctx, state.OrganisationId, request.AffectedPeriod); err != nil {
		return nil, err
	}
	previous, err := r.loadDeferrals(ctx, state.Posting)
	if err != nil {
		return nil, err
	}
	if err := verifyNewDeferralPeriod(request.Portions, request.AffectedPeriod, previous, state.AccountingVersion+1); err != nil {
		return nil, err
	}
	id, err := r.newID()
	if err != nil || !ids.IsCanonicalV7(id) {
		return nil, ErrRepository
	}
	deadline := cloneClaimDate(request.Portions[0].OrdinaryLastClaimDate)
	for _, p := range request.Portions {
		if purchaseDateKey(p.OrdinaryLastClaimDate) < purchaseDateKey(deadline) {
			deadline = cloneClaimDate(p.OrdinaryLastClaimDate)
		}
	}
	d := &tammyv1.PurchaseGstDeferral{Id: id, OrganisationId: state.OrganisationId, PostingId: state.Posting.Id, DocumentId: state.DocumentId, AccountingVersion: state.AccountingVersion + 1, AffectedPeriod: proto.Clone(request.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod), TimingReview: proto.Clone(request.TimingReview).(*tammyv1.PurchaseClaimTimingReview), DeferredGst: purchaseJournalMoney(amount), OrdinaryLastClaimDate: deadline, Reason: request.Reason, ActorUserId: request.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
	for _, p := range request.Portions {
		d.Portions = append(d.Portions, proto.Clone(p).(*tammyv1.PurchaseGstSourcePortion))
	}
	d.SourceHash, err = purchaseHash(d)
	if err != nil {
		return nil, err
	}
	content, err := purchaseBytes(d)
	if err != nil || len(content) > 262144 {
		return nil, ErrSourceBound
	}
	previewState := proto.Clone(state).(*tammyv1.PurchaseAccounting)
	previewState.AccountingVersion = d.AccountingVersion
	preview := &tammyv1.DeferPurchaseGstClaimResponse{Deferral: d, Accounting: previewState}
	if !purchaseResultFits(preview) || proto.Size(preview) > 1<<20 {
		return nil, ErrSourceBound
	}
	revision, err := ReservePurchaseRevision(ctx, r.executor, request.CommandContext.IdempotencyKey, purchaseService+"DeferPurchaseGstClaim", false, now)
	if err != nil {
		return nil, err
	}
	m := deferralScalars(d)
	m["deferral_proto"] = content
	if err := r.insertClaimScalars(ctx, "purchase_gst_deferrals_v1", m); err != nil {
		return nil, err
	}
	for i, p := range d.Portions {
		m, err := deferralPortionScalars(d, i, p, selected[i].amount)
		if err != nil {
			return nil, err
		}
		if err := r.insertClaimScalars(ctx, "purchase_gst_deferral_portions_v1", m); err != nil {
			return nil, err
		}
	}
	state.AccountingVersion = d.AccountingVersion
	if err := r.refreshClaimOutcomes(ctx, state, now); err != nil {
		return nil, err
	}
	response := &tammyv1.DeferPurchaseGstClaimResponse{Deferral: d, Accounting: state}
	if !purchaseResultFits(response) {
		return nil, ErrSourceBound
	}
	if err := RecordAccountingEvent(ctx, r.executor, AccountingEvent{OperationKey: request.CommandContext.IdempotencyKey, OrganisationID: state.OrganisationId, ScopeKind: "POSTING", ScopeID: state.Posting.Id, CommandType: purchaseService + "DeferPurchaseGstClaim", AccountingVersion: d.AccountingVersion, Revision: revision, Result: response, Now: now}); err != nil {
		return nil, err
	}
	return response, nil
}

func deferralScalars(d *tammyv1.PurchaseGstDeferral) map[string]any {
	return map[string]any{"id": d.Id, "organisation_id": d.OrganisationId, "posting_id": d.PostingId, "document_id": d.DocumentId, "accounting_version": d.AccountingVersion, "affected_period_start": purchaseDateString(d.AffectedPeriod.PeriodStart), "affected_period_end": purchaseDateString(d.AffectedPeriod.PeriodEnd), "reviewed_as_of": purchaseDateString(d.TimingReview.ReviewedAsOf), "timing_reason": d.TimingReview.Reason, "deferred_gst_minor": d.DeferredGst.MinorUnits, "currency_code": "AUD", "ordinary_last_claim_date": purchaseDateString(d.OrdinaryLastClaimDate), "reason": d.Reason, "source_hash": d.SourceHash, "actor_user_id": d.ActorUserId, "created_at": d.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano)}
}
func deferralPortionScalars(d *tammyv1.PurchaseGstDeferral, index int, p *tammyv1.PurchaseGstSourcePortion, amount int64) (map[string]any, error) {
	m, err := claimPortionScalars(p)
	if err != nil {
		return nil, err
	}
	content, err := purchaseBytes(p)
	if err != nil || len(content) > 16384 || amount <= 0 {
		return nil, ErrRepository
	}
	hash := sha256.Sum256(content)
	m["deferral_id"] = d.Id
	m["sequence"] = index + 1
	m["organisation_id"] = d.OrganisationId
	m["posting_id"] = d.PostingId
	m["deferred_gst_minor"] = amount
	m["portion_proto"] = content
	m["source_hash"] = hash[:]
	return m, nil
}
func (r *Repository) loadDeferrals(ctx context.Context, p *tammyv1.PurchasePosting) ([]*tammyv1.PurchaseGstDeferral, error) {
	rows, err := r.executor.QueryContext(ctx, `SELECT deferral_proto,source_hash FROM purchase_gst_deferrals_v1 WHERE posting_id=? AND organisation_id=? ORDER BY accounting_version,rowid`, p.Id, p.OrganisationId)
	if err != nil {
		return nil, ErrRepository
	}
	var result []*tammyv1.PurchaseGstDeferral
	var budget int
	for rows.Next() {
		var content, hash []byte
		if rows.Scan(&content, &hash) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		budget += len(content)
		if budget > purchaseClaimByteLimit {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
		d := &tammyv1.PurchaseGstDeferral{}
		if proto.Unmarshal(content, d) != nil || len(content) > 262144 || !canonicalPurchaseBytes(d, content) || !verifyPurchaseHash(d, hash) || !bytes.Equal(d.SourceHash, hash) || !validRetainedDeferral(d, p) {
			_ = rows.Close()
			return nil, ErrRepository
		}
		result = append(result, d)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	for _, d := range result {
		if err := r.verifyClaimScalars(ctx, "purchase_gst_deferrals_v1", deferralScalars(d)); err != nil {
			return nil, err
		}
		if err := r.verifyDeferralPortions(ctx, d, nil); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func validRetainedDeferral(d *tammyv1.PurchaseGstDeferral, p *tammyv1.PurchasePosting) bool {
	return d != nil && ids.IsCanonicalV7(d.Id) && d.OrganisationId == p.OrganisationId && d.PostingId == p.Id && d.DocumentId == p.DocumentId && d.AccountingVersion > 1 && d.AccountingVersion <= math.MaxInt64 && ids.IsCanonicalV7(d.ActorUserId) && d.RecordedAt != nil && d.RecordedAt.CheckValid() == nil && len(d.Portions) > 0 && len(d.Portions) <= 200 && validClaimTimingPeriod(d.AffectedPeriod) && d.TimingReview != nil && validPurchaseMoney(d.DeferredGst) && d.DeferredGst.MinorUnits > 0 && validPurchaseDate(d.OrdinaryLastClaimDate) && purchaseReviewText(d.Reason, 512, true)
}
func (r *Repository) verifyDeferralPortions(ctx context.Context, d *tammyv1.PurchaseGstDeferral, expected []ownedClaimPortion) error {
	rows, err := r.executor.QueryContext(ctx, `SELECT sequence,deferred_gst_minor FROM purchase_gst_deferral_portions_v1 WHERE deferral_id=? AND posting_id=? AND organisation_id=? ORDER BY sequence`, d.Id, d.PostingId, d.OrganisationId)
	if err != nil {
		return ErrRepository
	}
	var amounts []int64
	var total int64
	for rows.Next() {
		var sequence int
		var amount int64
		if rows.Scan(&sequence, &amount) != nil || sequence != len(amounts)+1 || len(amounts) >= len(d.Portions) || amount <= 0 || !addPurchaseAmount(&total, amount) {
			_ = rows.Close()
			return ErrRepository
		}
		if expected != nil && (len(amounts) >= len(expected) || amount != expected[len(amounts)].amount) {
			_ = rows.Close()
			return ErrRepository
		}
		amounts = append(amounts, amount)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil || len(amounts) != len(d.Portions) || total != d.DeferredGst.MinorUnits {
		return ErrRepository
	}
	var deadline *tammyv1.CivilDate
	for i, p := range d.Portions {
		m, err := deferralPortionScalars(d, i, p, amounts[i])
		if err != nil {
			return err
		}
		if err := r.verifyClaimScalars(ctx, "purchase_gst_deferral_portions_v1", m); err != nil {
			return err
		}
		if deadline == nil || purchaseDateKey(p.OrdinaryLastClaimDate) < purchaseDateKey(deadline) {
			deadline = p.OrdinaryLastClaimDate
		}
	}
	if !proto.Equal(deadline, d.OrdinaryLastClaimDate) {
		return ErrRepository
	}
	return nil
}

func verifyNewDeferralPeriod(portions []*tammyv1.PurchaseGstSourcePortion, period *tammyv1.PurchaseClaimPeriod, previous []*tammyv1.PurchaseGstDeferral, eventVersion uint64) error {
	for _, d := range previous {
		if d.AccountingVersion >= eventVersion || !proto.Equal(d.AffectedPeriod, period) {
			continue
		}
		for _, a := range d.Portions {
			for _, b := range portions {
				if sameClaimPortionSource(a, b) {
					return ErrConflict
				}
			}
		}
	}
	return nil
}
