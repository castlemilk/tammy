package purchases

import (
	"context"
	"errors"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

// VerifiedClaims exposes complete owned snapshots after full accounting replay.
// Consumers apply period/as-of decisions themselves; no UI page bounds truncate
// the authoritative collections.
type VerifiedClaimHistory struct {
	Attributions     []*tammyv1.PurchaseGstAttribution
	Deferrals        []*tammyv1.PurchaseGstDeferral
	DeferralPortions []VerifiedDeferralPortion
}

// Period-specific decisions can identify the same physical future credit.
// Each portion retains its original extent and current remaining source amount;
// reporting deduplicates physical source identity instead of summing decisions.
type VerifiedDeferralPortion struct {
	DeferralID   string
	Sequence     uint32
	Portion      *tammyv1.PurchaseGstSourcePortion
	DeferredGST  *tammyv1.Money
	UnclaimedGST *tammyv1.Money
}

func (r *Repository) VerifiedClaims(ctx context.Context, org, document string) (VerifiedClaimHistory, error) {
	state, err := r.Get(ctx, org, document)
	if err != nil {
		return VerifiedClaimHistory{}, err
	}
	if !state.Posted {
		return VerifiedClaimHistory{}, nil
	}
	as, err := r.loadAttributions(ctx, state.Posting)
	if err != nil {
		return VerifiedClaimHistory{}, err
	}
	ds, err := r.loadDeferrals(ctx, state.Posting)
	if err != nil {
		return VerifiedClaimHistory{}, err
	}
	result := VerifiedClaimHistory{Deferrals: ds}
	for _, a := range as {
		result.Attributions = append(result.Attributions, a.value)
	}
	for _, d := range ds {
		rows, err := r.executor.QueryContext(ctx, `SELECT sequence,deferred_gst_minor FROM purchase_gst_deferral_portions_v1 WHERE deferral_id=? AND posting_id=? AND organisation_id=? ORDER BY sequence`, d.Id, d.PostingId, d.OrganisationId)
		if err != nil {
			return VerifiedClaimHistory{}, ErrRepository
		}
		index := 0
		var total int64
		for rows.Next() {
			var sequence uint32
			var amount int64
			if rows.Scan(&sequence, &amount) != nil || index >= len(d.Portions) || sequence != uint32(index+1) || amount <= 0 || !addPurchaseAmount(&total, amount) {
				_ = rows.Close()
				return VerifiedClaimHistory{}, ErrRepository
			}
			portion := d.Portions[index]
			remaining := amount
			for _, a := range result.Attributions {
				if a.AccountingVersion > d.AccountingVersion && sameClaimPortionSource(portion, a.Portion) {
					if a.ClaimedGst.MinorUnits > remaining {
						_ = rows.Close()
						return VerifiedClaimHistory{}, ErrRepository
					}
					remaining -= a.ClaimedGst.MinorUnits
				}
			}
			result.DeferralPortions = append(result.DeferralPortions, VerifiedDeferralPortion{DeferralID: d.Id, Sequence: sequence, Portion: proto.Clone(portion).(*tammyv1.PurchaseGstSourcePortion), DeferredGST: purchaseJournalMoney(amount), UnclaimedGST: purchaseJournalMoney(remaining)})
			index++
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil || index != len(d.Portions) || total != d.DeferredGst.MinorUnits {
			return VerifiedClaimHistory{}, ErrRepository
		}
	}
	return result, nil
}

func (r *Repository) refreshClaimOutcomes(ctx context.Context, state *tammyv1.PurchaseAccounting, now time.Time) error {
	evidences, err := r.retainedEvidenceHistory(ctx, state.Posting)
	if err != nil {
		return err
	}
	var selected *tammyv1.PurchaseTaxEvidence
	for _, e := range evidences {
		if e.AccountingVersion <= state.AccountingVersion && !e.RecordedAt.AsTime().After(now) {
			selected = e
		}
	}
	if selected == nil {
		return ErrRepository
	}
	outcomes, err := r.evidenceOutcomes(ctx, state.Posting, selected.Review, now)
	if err != nil {
		return err
	}
	attributions, err := r.loadAttributions(ctx, state.Posting)
	if err != nil {
		return err
	}
	deferrals, err := r.loadDeferrals(ctx, state.Posting)
	if err != nil {
		return err
	}
	hasClaim := false
	for _, a := range attributions {
		if a.value.AccountingVersion <= state.AccountingVersion {
			hasClaim = true
		}
	}
	if hasClaim && state.SuspenseGst.MinorUnits == 0 && outcomes.Evidence == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR && purchaseDateKey(selected.Review.HeldDate) <= purchaseDateKey(purchaseAsOf(now)) {
		outcomes.Timing = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	}
	for _, d := range deferrals {
		if d.AccountingVersion > state.AccountingVersion {
			continue
		}
		for _, p := range d.Portions {
			claimed := false
			for _, a := range attributions {
				if a.value.AccountingVersion <= state.AccountingVersion && a.value.AccountingVersion > d.AccountingVersion && sameClaimPortionSource(p, a.value.Portion) {
					claimed = true
					break
				}
			}
			if !claimed && purchaseDateKey(purchaseAsOf(now)) > purchaseDateKey(p.OrdinaryLastClaimDate) {
				outcomes.Timing = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
			}
		}
	}
	state.ReviewOutcomes = outcomes
	return nil
}

func (r *Repository) replayAttributionEvent(ctx context.Context, posting *tammyv1.PurchasePosting, event retainedPaymentEvent, values []retainedAttribution, deferrals []*tammyv1.PurchaseGstDeferral, state *tammyv1.PurchaseAccounting, instant time.Time) (*tammyv1.PurchaseAccounting, int, error) {
	response := &tammyv1.AttributePurchaseGstResponse{}
	if !event.journalID.Valid || proto.Unmarshal(event.content, response) != nil || !canonicalPurchaseBytes(response, event.content) || len(response.Attributions) == 0 || len(response.Attributions) > 200 {
		return nil, 0, ErrRepository
	}
	count := len(response.Attributions)
	if len(values) < count {
		return nil, 0, ErrRepository
	}
	first := response.Attributions[0]
	portions := make([]*tammyv1.PurchaseGstSourcePortion, count)
	for i, a := range response.Attributions {
		retained := values[i]
		if retained.operation != event.operation || a.AccountingVersion != event.version || a.JournalId != event.journalID.String || a.FinancialRevision != event.financial || a.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) != event.stamp || !proto.Equal(a, retained.value) || !proto.Equal(a.ClaimPeriod, first.ClaimPeriod) || !proto.Equal(a.ReleasePostingDate, first.ReleasePostingDate) || !proto.Equal(a.TimingReview, first.TimingReview) || a.ActorUserId != first.ActorUserId || a.GetClosesDeferralId() != first.GetClosesDeferralId() {
			return nil, 0, ErrRepository
		}
		portions[i] = a.Portion
	}
	selected, amount, err := r.selectClaimPortions(ctx, posting, portions, first.ClaimPeriod, first.ReleasePostingDate, first.TimingReview, ClaimTimingDelayed, instant, event.version)
	if err != nil {
		return nil, 0, ErrRepository
	}
	for i, a := range response.Attributions {
		if a.ClaimedGst.MinorUnits != selected[i].amount {
			return nil, 0, ErrRepository
		}
	}
	if err := verifyAttributionDeferralLink(portions, first.ClosesDeferralId, deferrals, event.version); err != nil {
		return nil, 0, ErrRepository
	}
	journal, err := r.verifyAttributionJournal(ctx, posting, event, first, amount)
	if err != nil || !proto.Equal(journal, response.Journal) {
		return nil, 0, ErrRepository
	}
	if err := applyAttributionState(state, response.Attributions); err != nil {
		return nil, 0, err
	}
	return response.Accounting, count, nil
}

func (r *Repository) replayDeferralEvent(ctx context.Context, posting *tammyv1.PurchasePosting, event retainedPaymentEvent, d *tammyv1.PurchaseGstDeferral, state *tammyv1.PurchaseAccounting, instant time.Time) (*tammyv1.PurchaseAccounting, error) {
	response := &tammyv1.DeferPurchaseGstClaimResponse{}
	if event.journalID.Valid || d == nil || d.AccountingVersion != event.version || d.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) != event.stamp || proto.Unmarshal(event.content, response) != nil || !canonicalPurchaseBytes(response, event.content) || !proto.Equal(response.Deferral, d) {
		return nil, ErrRepository
	}
	selected, amount, err := r.selectClaimPortions(ctx, posting, d.Portions, d.AffectedPeriod, nil, d.TimingReview, ClaimTimingDeferral, instant, event.version)
	if err != nil || amount != d.DeferredGst.MinorUnits || amount > state.SuspenseGst.MinorUnits {
		return nil, ErrRepository
	}
	previous, err := r.loadDeferrals(ctx, posting)
	if err != nil {
		return nil, err
	}
	if err := verifyNewDeferralPeriod(d.Portions, d.AffectedPeriod, previous, event.version); err != nil {
		return nil, ErrRepository
	}
	if err := r.verifyDeferralPortions(ctx, d, selected); err != nil {
		return nil, err
	}
	state.AccountingVersion = d.AccountingVersion
	return response.Accounting, nil
}

func (r *Repository) verifyElectedEventActor(ctx context.Context, operation, command, actor string) error {
	rows, err := r.executor.QueryContext(ctx, `SELECT count(*) FROM command_idempotency_v1 WHERE operation_key=? AND fully_qualified_rpc_name=? AND actor_user_id=? AND outcome IN ('ELECTED','COMMITTED')`, operation, command, actor)
	var count int
	if err != nil || (purchaseRevisionRow{rows: rows}).Scan(&count) != nil || count != 1 {
		return ErrRepository
	}
	return nil
}

func (r *Repository) verifyAttributionJournal(ctx context.Context, p *tammyv1.PurchasePosting, event retainedPaymentEvent, a *tammyv1.PurchaseGstAttribution, amount int64) (*tammyv1.Journal, error) {
	journals, _ := accounting.NewJournalRepository(r.executor)
	journal, err := journals.Get(ctx, a.JournalId)
	if err != nil || len(journal.Lines) != 2 {
		return nil, ErrRepository
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT count(*) FROM journals WHERE id=? AND organisation_id=? AND source_type='TAX' AND source_kind=8 AND source_id=? AND source_revision=1`, a.JournalId, p.OrganisationId, event.operation)
	var owners int
	if err != nil || (purchaseRevisionRow{rows: rows}).Scan(&owners) != nil || owners != 1 {
		return nil, ErrRepository
	}
	ids := []string{journal.Id, journal.Lines[0].Id, journal.Lines[1].Id}
	index := 0
	writer, _ := NewJournalWriter(r.executor, func() time.Time { return a.RecordedAt.AsTime() }, func() (string, error) {
		if index >= len(ids) {
			return "", ErrRepository
		}
		id := ids[index]
		index++
		return id, nil
	})
	spec, err := writer.spec(ctx, p.OrganisationId, event.operation, a.ReleasePostingDate, a.FinancialRevision, nil)
	if err != nil {
		return nil, ErrRepository
	}
	expected, flows, err := BuildPurchaseGSTReleaseJournal(spec, amount)
	if err != nil || !proto.Equal(expected, journal) {
		return nil, ErrRepository
	}
	for _, line := range journal.Lines {
		components := flows[line.Id]
		rows, err := r.executor.QueryContext(ctx, `SELECT sequence,category,amount_minor,source_revision FROM cash_flow_facts WHERE journal_line_id=? AND organisation_id=? ORDER BY sequence`, line.Id, p.OrganisationId)
		if err != nil {
			return nil, ErrRepository
		}
		index := 0
		for rows.Next() {
			var seq, revision uint64
			var category string
			var movement int64
			if rows.Scan(&seq, &category, &movement, &revision) != nil || index >= len(components) || seq != uint64(index+1) || revision != 1 || category != "NONCASH" || movement != components[index].AmountMinor {
				_ = rows.Close()
				return nil, ErrRepository
			}
			index++
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil || index != len(components) {
			return nil, ErrRepository
		}
	}
	return journal, nil
}
