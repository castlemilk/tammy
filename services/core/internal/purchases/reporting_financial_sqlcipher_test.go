//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func captureAndMapBASPurchaseFixture(
	t *testing.T,
	tx *sqlcipher.Transaction,
	f *postingFixture,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
	basis tammyv1.GstBasis,
) (*purchases.BASPurchaseFinancialMap, *purchases.BASSnapshot,
	*purchases.BASModernCandidateDiscovery, *purchases.BASLegacyCandidateDiscovery) {
	t.Helper()
	ctx := context.Background()
	repo := f.repo(tx)

	// Freeze the scalar nomination inputs before hydrating the complete capture.
	legacyInputs := readBASLegacyReconcileInputs(t, tx, f)
	summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("reserve modern physical keys: summary=%v err=%v", summary, err)
	}
	modern, err := repo.ReadModernBASCandidates(ctx, postingOrg, *summary,
		summary.ScalarKeyCount, summary.ScalarBytes, 1000)
	if err != nil || modern == nil {
		t.Fatalf("read frozen modern candidates: discovery=%v err=%v", modern, err)
	}
	legacy, err := purchases.NominateBASLegacyCandidates(ctx, postingOrg, period, balance,
		legacyInputs.states, legacyInputs.keys, legacyInputs.decisions, legacyInputs.cash, 1000)
	if err != nil || legacy == nil {
		t.Fatalf("nominate frozen legacy candidates: discovery=%v err=%v", legacy, err)
	}

	// This full read follows nomination. Both reconciliations run before mapping.
	capture, err := repo.CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture complete purchase proof: capture=%v err=%v", capture, err)
	}
	if err := purchases.ReconcileBASPhysicalKeys(ctx, postingOrg, modern.PhysicalKeys, capture); err != nil {
		t.Fatalf("reconcile exact modern physical keys: %v", err)
	}
	if err := purchases.ReconcileBASPhysicalKeys(ctx, postingOrg, legacyInputs.keys, capture); err != nil {
		t.Fatalf("reconcile exact legacy selector physical keys: %v", err)
	}
	if err := purchases.ReconcileBASLegacyCandidates(ctx, postingOrg, period, balance,
		legacyInputs.states, legacyInputs.keys, legacyInputs.decisions, legacyInputs.cash, legacy, capture); err != nil {
		t.Fatalf("reconcile exact legacy nominations/full proof: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if partial, err := purchases.MapBASPurchaseReporting(cancelled, capture, modern, legacy, basis, period, balance, f.now); partial != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pure mapping returned partial=%v err=%v", partial, err)
	}
	got, err := purchases.MapBASPurchaseReporting(ctx, capture, modern, legacy, basis, period, balance, f.now)
	if err != nil || got == nil {
		t.Fatalf("map reconciled purchase proof: map=%v err=%v", got, err)
	}
	mature, err := repo.CaptureBAS(ctx, postingOrg, period, balance, f.now)
	if err != nil || mature == nil {
		t.Fatalf("read mature purchase oracle: snapshot=%v err=%v", mature, err)
	}
	return got, mature, modern, legacy
}

func TestBASPurchaseFinancialMapMatchesWriterSourcesForBothGstBases(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{
		tammyv1.GstBasis_GST_BASIS_CASH,
		tammyv1.GstBasis_GST_BASIS_NON_CASH,
	} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, basis)
			repo := f.repo(tx)
			posted, err := repo.PostReviewed(ctx, f.request)
			if err != nil || posted == nil || posted.Posting == nil {
				t.Fatalf("post reviewed source through writer: response=%v err=%v", posted, err)
			}
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				f.now = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
				if _, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, 11000, postDate(20))); err != nil {
					t.Fatalf("record full cash-basis payment through writer: %v", err)
				}
			}
			f.now = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
			period := &tammyv1.PurchaseClaimPeriod{
				PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 1},
				PeriodEnd:   &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 31},
			}
			mapped, mature, modern, _ := captureAndMapBASPurchaseFixture(t, tx, f, period, period.PeriodEnd, basis)
			gotProjection := &tammyv1.BasWorkpaper{
				PurchaseSources: mapped.Sources, PurchaseReviews: mapped.Reviews,
				LegacyPurchaseSupport: mapped.LegacySupport,
				LegacyCandidateGst:    mapped.LegacyCandidateGST,
			}
			wantProjection := &tammyv1.BasWorkpaper{
				PurchaseSources: mature.Sources, PurchaseReviews: mature.Reviews,
				LegacyPurchaseSupport: mature.LegacySupport,
				LegacyCandidateGst:    mature.LegacyCandidateGST,
			}
			if !proto.Equal(gotProjection, wantProjection) {
				t.Fatalf("captured pure purchase map differs from mature writer oracle\ngot: %v\nwant: %v", gotProjection, wantProjection)
			}

			// Candidate membership must be exactly the frozen key set, including
			// zero-line structural fallbacks. Every source-bearing unit carries
			// precisely one full typed key.
			modernUnits := make(map[purchases.BASModernCandidateKey]int)
			for _, unit := range mapped.Units {
				if (unit.Candidate.Modern == nil) == (unit.Candidate.Legacy == nil) {
					t.Fatalf("candidate unit must preserve exactly one typed key: %#v", unit)
				}
				if unit.Candidate.Modern == nil {
					continue
				}
				modernUnits[*unit.Candidate.Modern]++
				if unit.Source != nil {
					key := unit.Candidate.Modern
					if unit.Source.PostingId != key.PostingID || unit.Source.DocumentId != key.DocumentID ||
						unit.Source.AllocationSequence != uint32(key.Sequence) {
						t.Fatalf("source line lost its exact modern candidate identity: unit=%#v", unit)
					}
					if unit.Source.PaymentId != nil &&
						(key.Kind != purchases.BASModernCandidatePaymentAllocation ||
							*unit.Source.PaymentId != key.PaymentID || key.Sequence != uint64(unit.Source.AllocationSequence)) {
						t.Fatalf("cash source line lost exact payment component key: unit=%#v", unit)
					}
				}
			}
			if len(modernUnits) != len(modern.Candidates) {
				t.Fatalf("mapped modern membership count=%d, want frozen candidate count %d", len(modernUnits), len(modern.Candidates))
			}
			for _, key := range modern.Candidates {
				if modernUnits[key] != 1 {
					t.Fatalf("modern key mapped %d times: %#v", modernUnits[key], key)
				}
			}
			for _, attribution := range mapped.Attributions {
				if attribution.Source == nil || attribution.Source.AttributionId == nil ||
					attribution.Source.PostingId != attribution.Candidate.PostingID ||
					attribution.Source.AllocationSequence == 0 {
					t.Fatalf("attribution lost its exact source portion owner: %#v", attribution)
				}
				if attribution.Source.PaymentId == nil {
					if attribution.Candidate.Kind != purchases.BASModernCandidatePostingAllocation ||
						attribution.Candidate.Sequence != uint64(attribution.Source.AllocationSequence) {
						t.Fatalf("posting attribution mapped to the wrong component: %#v", attribution)
					}
				} else if attribution.Candidate.Kind != purchases.BASModernCandidatePaymentAllocation ||
					attribution.Candidate.PaymentID != *attribution.Source.PaymentId ||
					attribution.Candidate.Sequence != uint64(attribution.Source.AllocationSequence) ||
					attribution.Candidate.PostingAllocationSequence != uint64(attribution.Source.AllocationSequence) {
					t.Fatalf("payment attribution mapped to the wrong component: %#v", attribution)
				}
				matched := false
				for _, key := range modern.Candidates {
					if key == attribution.Candidate {
						matched = true
						break
					}
				}
				if !matched {
					t.Fatalf("attribution references a non-member candidate: %#v", attribution)
				}
			}
		})
	}
}

func TestBASPurchaseFinancialMapAttachesDelayedAttributionToExactWriterPortion(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{
		tammyv1.GstBasis_GST_BASIS_CASH,
		tammyv1.GstBasis_GST_BASIS_NON_CASH,
	} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, basis)
			claimed, err := attributeCommand(t, repo, claimRequest(t, f, state, portion))
			if err != nil || claimed == nil || len(claimed.Attributions) != 1 {
				t.Fatalf("retain delayed attribution through writer: result=%v err=%v", claimed, err)
			}
			period := claimPeriod()
			mapped, mature, modern, _ := captureAndMapBASPurchaseFixture(t, tx, f, period, period.PeriodEnd, basis)
			gotProjection := &tammyv1.BasWorkpaper{
				PurchaseSources: mapped.Sources, PurchaseReviews: mapped.Reviews,
				LegacyPurchaseSupport: mapped.LegacySupport,
				LegacyCandidateGst:    mapped.LegacyCandidateGST,
			}
			wantProjection := &tammyv1.BasWorkpaper{
				PurchaseSources: mature.Sources, PurchaseReviews: mature.Reviews,
				LegacyPurchaseSupport: mature.LegacySupport,
				LegacyCandidateGst:    mature.LegacyCandidateGST,
			}
			if !proto.Equal(gotProjection, wantProjection) {
				t.Fatalf("delayed attribution changed mature BAS output\ngot: %v\nwant: %v", gotProjection, wantProjection)
			}
			if len(mapped.Attributions) != 1 || mapped.Attributions[0].Source == nil ||
				mapped.Attributions[0].Source.AttributionId == nil ||
				*mapped.Attributions[0].Source.AttributionId != claimed.Attributions[0].Id {
				t.Fatalf("attribution identity/provenance missing from financial map: %#v", mapped.Attributions)
			}
			owner := mapped.Attributions[0].Candidate
			found := false
			for _, key := range modern.Candidates {
				if key == owner {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("attribution maps to a nonmember source component: %#v", owner)
			}
		})
	}
}

// A compact writer-backed legacy comparison. Broader legacy transitions are
// already exercised in legacy_sqlcipher_test.go and
// legacy_currentness_sqlcipher_test.go; add the same map-vs-CaptureBAS
// projection assertion to those fixtures so each branch is compared without
// building synthetic retained review/journal bodies here.
func TestBASPurchaseFinancialMapMatchesLegacyWriterDecisionProjection(t *testing.T) {
	tx, f, request, _ := purchaseBASLegacyDecisionFixture(t)
	period := proto.Clone(request.Review.AffectedPeriod).(*tammyv1.PurchaseClaimPeriod)
	balance := proto.Clone(request.Review.ReportingBalanceDate).(*tammyv1.CivilDate)
	mapped, mature, _, _ := captureAndMapBASPurchaseFixture(t, tx, f, period, balance,
		tammyv1.GstBasis_GST_BASIS_NON_CASH)
	gotProjection := &tammyv1.BasWorkpaper{
		PurchaseSources: mapped.Sources, PurchaseReviews: mapped.Reviews,
		LegacyPurchaseSupport: mapped.LegacySupport, LegacyCandidateGst: mapped.LegacyCandidateGST,
	}
	wantProjection := &tammyv1.BasWorkpaper{
		PurchaseSources: mature.Sources, PurchaseReviews: mature.Reviews,
		LegacyPurchaseSupport: mature.LegacySupport, LegacyCandidateGst: mature.LegacyCandidateGST,
	}
	if !proto.Equal(gotProjection, wantProjection) {
		t.Fatalf("pure adapter changed retained legacy decision projection\nmap=%v\nwant=%v", mapped, mature)
	}
}

func assertBASPurchaseMapMatchesMature(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture,
	period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate, basis tammyv1.GstBasis) *purchases.BASPurchaseFinancialMap {
	t.Helper()
	mapped, mature, _, _ := captureAndMapBASPurchaseFixture(t, tx, f, period, balance, basis)
	got := &tammyv1.BasWorkpaper{PurchaseSources: mapped.Sources, PurchaseReviews: mapped.Reviews,
		LegacyPurchaseSupport: mapped.LegacySupport, LegacyCandidateGst: mapped.LegacyCandidateGST,
		ExcludedCashPurchaseCount: mapped.ExcludedCashPurchaseCount}
	want := &tammyv1.BasWorkpaper{PurchaseSources: mature.Sources, PurchaseReviews: mature.Reviews,
		LegacyPurchaseSupport: mature.LegacySupport, LegacyCandidateGst: mature.LegacyCandidateGST,
		ExcludedCashPurchaseCount: mature.ExcludedCashPurchaseCount}
	if !proto.Equal(got, want) {
		t.Fatalf("pure map differs from exact mature legacy/claim projection\ngot: %v\nwant: %v", got, want)
	}
	return mapped
}
