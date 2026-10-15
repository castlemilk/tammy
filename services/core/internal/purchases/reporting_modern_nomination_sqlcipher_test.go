//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

const purchaseBASModernTestCandidateLimit int64 = 1000

func purchaseBASModernCoverageRows(capture *purchases.PurchaseCoverageReportingCapture) *purchases.PostingReportingCapture {
	return &purchases.PostingReportingCapture{Rows: capture.Rows}
}

func purchaseBASModernPaymentAllocationLink(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, paymentID string, sequence uint32) uint64 {
	t.Helper()
	var found *uint64
	for _, row := range capture.Rows {
		if row.Table != "purchase_payment_allocations_v1" {
			continue
		}
		payment, hasPayment := retainedValue(row, "payment_id")
		ownSequence, hasSequence := retainedValue(row, "sequence")
		if !hasPayment || payment.Kind != "TEXT" || payment.Text != paymentID ||
			!hasSequence || ownSequence.Kind != "INTEGER" || ownSequence.Integer != int64(sequence) {
			continue
		}
		if found != nil {
			t.Fatalf("coverage duplicated payment allocation identity (%s, %d)", paymentID, sequence)
		}
		postingSequence, ok := retainedValue(row, "posting_allocation_sequence")
		if !ok || postingSequence.Kind != "INTEGER" || postingSequence.Integer < 1 {
			t.Fatalf("coverage lost payment allocation (%s, %d) posting link: %#v", paymentID, sequence, postingSequence)
		}
		value := uint64(postingSequence.Integer)
		found = &value
	}
	if found == nil {
		t.Fatalf("coverage omitted exact payment allocation identity (%s, %d)", paymentID, sequence)
	}
	return *found
}

// This derives the structural oracle from the complete reader's immutable
// posting/payment bodies and its retained composite payment-allocation rows.
// It intentionally makes no decision from amount, date, status, or GST basis.
func purchaseBASModernExpectedCandidates(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture) []purchases.BASModernCandidateKey {
	t.Helper()
	if capture == nil {
		t.Fatal("nil complete purchase coverage")
	}
	var candidates []purchases.BASModernCandidateKey
	for _, record := range capture.Postings {
		posting := record.Posting
		if posting == nil {
			t.Fatal("complete coverage returned a posting record without its body")
		}
		if len(posting.Allocations) == 0 {
			candidates = append(candidates, purchases.BASModernCandidateKey{
				Kind: purchases.BASModernCandidatePosting, OrganisationID: posting.OrganisationId,
				PostingID: posting.Id, DocumentID: posting.DocumentId,
			})
		} else {
			for _, allocation := range posting.Allocations {
				if allocation == nil {
					t.Fatalf("posting %s contains a nil allocation body", posting.Id)
				}
				candidates = append(candidates, purchases.BASModernCandidateKey{
					Kind: purchases.BASModernCandidatePostingAllocation, OrganisationID: posting.OrganisationId,
					PostingID: posting.Id, DocumentID: posting.DocumentId, Sequence: uint64(allocation.Sequence),
				})
			}
		}
		for _, payment := range record.Payments {
			if payment == nil {
				t.Fatalf("posting %s contains a nil payment body", posting.Id)
			}
			if len(payment.Allocations) == 0 {
				candidates = append(candidates, purchases.BASModernCandidateKey{
					Kind: purchases.BASModernCandidatePayment, OrganisationID: posting.OrganisationId,
					PostingID: posting.Id, DocumentID: posting.DocumentId, PaymentID: payment.Id,
				})
				continue
			}
			for _, allocation := range payment.Allocations {
				if allocation == nil {
					t.Fatalf("payment %s contains a nil allocation body", payment.Id)
				}
				candidates = append(candidates, purchases.BASModernCandidateKey{
					Kind: purchases.BASModernCandidatePaymentAllocation, OrganisationID: posting.OrganisationId,
					PostingID: posting.Id, DocumentID: posting.DocumentId, PaymentID: payment.Id,
					Sequence:                  uint64(allocation.Sequence),
					PostingAllocationSequence: purchaseBASModernPaymentAllocationLink(t, capture, payment.Id, allocation.Sequence),
				})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		if candidates[i].PostingID != candidates[j].PostingID {
			return candidates[i].PostingID < candidates[j].PostingID
		}
		if candidates[i].PaymentID != candidates[j].PaymentID {
			return candidates[i].PaymentID < candidates[j].PaymentID
		}
		return candidates[i].Sequence < candidates[j].Sequence
	})
	return candidates
}

func purchaseBASModernAssertPhysicalKeys(t *testing.T, discovery *purchases.BASModernCandidateDiscovery, summary *purchases.BASScalarSummary) {
	t.Helper()
	if discovery == nil || summary == nil || int64(len(discovery.PhysicalKeys)) != summary.ScalarKeyCount {
		t.Fatalf("modern discovery lost physical keys: discovery=%#v summary=%#v", discovery, summary)
	}
	var counts [purchases.BASScalarFamilyCount]int64
	for _, key := range discovery.PhysicalKeys {
		if key.OrganisationID != summary.OrganisationID || key.Kind < purchases.BASScalarPosting || key.Kind > purchases.BASScalarManualJournal {
			t.Fatalf("modern discovery returned an unowned or unknown physical key: %#v", key)
		}
		counts[int(key.Kind)-1]++
	}
	if counts != summary.FamilyCounts {
		t.Fatalf("modern discovery physical family counts=%v, want complete scalar reservation %v", counts, summary.FamilyCounts)
	}
}

func purchaseBASModernRead(t *testing.T, ctx context.Context, tx *sqlcipher.Transaction, f *postingFixture, maximumCandidates int64) (*purchases.BASModernCandidateDiscovery, *purchases.BASScalarSummary, *purchaseBASScalarProbe) {
	t.Helper()
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := f.repo(probe)
	before := purchaseBASScalarState(t, tx)
	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("summarize physical purchase keys: summary=%#v err=%v", summary, err)
	}
	expected := *summary
	discovery, err := repository.ReadModernBASCandidates(ctx, postingOrg, *summary,
		summary.ScalarKeyCount, summary.ScalarBytes, maximumCandidates)
	if err != nil || discovery == nil {
		t.Fatalf("read modern purchase candidates: discovery=%#v err=%v", discovery, err)
	}
	if !reflect.DeepEqual(*summary, expected) {
		t.Fatalf("modern nomination changed caller summary: before=%#v after=%#v", expected, *summary)
	}
	if probe.keyReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatalf("modern nomination read %d scalar key sets and %d bodies, mutated state, or ended caller transaction", probe.keyReads, probe.bodyReads)
	}
	purchaseBASModernAssertPhysicalKeys(t, discovery, summary)
	return discovery, summary, probe
}

func purchaseBASModernAssertCandidateMatchesCoverage(t *testing.T, discovery *purchases.BASModernCandidateDiscovery, capture *purchases.PurchaseCoverageReportingCapture) {
	t.Helper()
	want := purchaseBASModernExpectedCandidates(t, capture)
	if !reflect.DeepEqual(discovery.Candidates, want) {
		t.Fatalf("modern candidate identities differ from complete writer bodies: got=%#v want=%#v", discovery.Candidates, want)
	}
}

func purchaseBASModernFindCandidate(t *testing.T, candidates []purchases.BASModernCandidateKey, kind purchases.BASModernCandidateKind, paymentID string, sequence uint64) purchases.BASModernCandidateKey {
	t.Helper()
	var found *purchases.BASModernCandidateKey
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.Kind != kind || candidate.PaymentID != paymentID || candidate.Sequence != sequence {
			continue
		}
		if found != nil {
			t.Fatalf("duplicate modern candidate kind=%d payment=%s sequence=%d", kind, paymentID, sequence)
		}
		found = candidate
	}
	if found == nil {
		t.Fatalf("missing modern candidate kind=%d payment=%s sequence=%d", kind, paymentID, sequence)
	}
	return *found
}

func purchaseBASModernAssertRejectedRead(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture, expected purchases.BASScalarSummary, ctx context.Context, maximumKeys, maximumBytes, maximumCandidates int64, wantKeyReads int, wantBound bool) {
	t.Helper()
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := f.repo(probe)
	before := purchaseBASScalarState(t, tx)
	expectedCopy := expected
	discovery, err := repository.ReadModernBASCandidates(ctx, postingOrg, expected, maximumKeys, maximumBytes, maximumCandidates)
	if discovery != nil || err == nil {
		t.Fatalf("rejected modern read returned discovery=%#v err=%v", discovery, err)
	}
	if wantBound && !errors.Is(err, purchases.ErrSourceBound) {
		t.Fatalf("short modern read error=%v, want source bound", err)
	}
	if !wantBound && !errors.Is(err, context.Canceled) && !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("invalid/cancelled modern read error=%v", err)
	}
	if !reflect.DeepEqual(expected, expectedCopy) || probe.keyReads != wantKeyReads || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatalf("rejected modern read changed input/state, read keys=%d bodies=%d, or ended caller transaction", probe.keyReads, probe.bodyReads)
	}
}

func TestPurchaseBASModernNominationRealCashAndNonCashWritersMatchCoverage(t *testing.T) {
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
			firstAllocation := proto.Clone(f.request.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
			firstAllocation.Gross = postMoney(5500)
			firstAllocation.SupplierGst = postMoney(500)
			firstAllocation.EligibleGst = postMoney(500)
			secondAllocation := proto.Clone(firstAllocation).(*tammyv1.PurchaseAllocationInput)
			secondAllocation.Sequence = 2
			f.request.Allocations = []*tammyv1.PurchaseAllocationInput{firstAllocation, secondAllocation}
			posted, err := f.repo(tx).PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatalf("post two-allocation purchase: %v", err)
			}
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

			var writerPayments []*tammyv1.RecordPurchasePaymentResponse
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				first, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, 1, 5500, paymentDate(9, 30)))
				if err != nil {
					t.Fatalf("record first allocation payment: %v", err)
				}
				second, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, 2, 5500, paymentDate(10, 2)))
				if err != nil {
					t.Fatalf("record second allocation payment: %v", err)
				}
				writerPayments = []*tammyv1.RecordPurchasePaymentResponse{first, second}
			}

			discovery, _, _ := purchaseBASModernRead(t, ctx, tx, f, purchaseBASModernTestCandidateLimit)
			coverage := captureCoverage(t, f.repo(tx), ctx)
			record := coveragePosting(t, coverage, posted.Posting.Id)
			if !proto.Equal(record.Posting, posted.Posting) {
				t.Fatal("complete coverage posting body differs from exact writer response")
			}
			expectedAccounting := posted.Accounting
			if len(writerPayments) > 0 {
				expectedAccounting = writerPayments[len(writerPayments)-1].Accounting
			}
			if !proto.Equal(record.Accounting, expectedAccounting) || len(record.Payments) != len(writerPayments) {
				t.Fatalf("complete coverage accounting/payment bodies differ from writer responses: accounting=%v payments=%d want=%d", record.Accounting, len(record.Payments), len(writerPayments))
			}
			for _, response := range writerPayments {
				if response == nil || response.Payment == nil {
					t.Fatal("payment writer response omitted its body")
				}
				found := false
				for _, payment := range record.Payments {
					if payment != nil && payment.Id == response.Payment.Id {
						found = true
						if !proto.Equal(payment, response.Payment) {
							t.Fatalf("complete coverage payment %s differs from exact writer body", payment.Id)
						}
					}
				}
				if !found {
					t.Fatalf("complete coverage omitted writer payment %s", response.Payment.Id)
				}
			}
			foundDocument := false
			for _, document := range coverage.Documents {
				if document.Document != nil && document.Document.Id == f.document.Id {
					foundDocument = true
					if !proto.Equal(document.Document, f.document) {
						t.Fatal("complete coverage document body differs from retained reviewed writer source")
					}
				}
			}
			if !foundDocument {
				t.Fatal("complete coverage omitted the exact writer document")
			}

			purchaseBASModernAssertCandidateMatchesCoverage(t, discovery, coverage)
			wantPostingAllocations := len(posted.Posting.Allocations)
			if got := countCoverageRows(purchaseBASModernCoverageRows(coverage), "purchase_posting_allocations_v1", "posting_id", posted.Posting.Id); got != wantPostingAllocations {
				t.Fatalf("complete coverage retained %d posting allocation identities, want %d", got, wantPostingAllocations)
			}
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				if len(discovery.Candidates) != 6 {
					t.Fatalf("cash source candidate count=%d, want two posting and four payment allocation units", len(discovery.Candidates))
				}
				secondPayment := writerPayments[1].Payment
				candidate := purchaseBASModernFindCandidate(t, discovery.Candidates, purchases.BASModernCandidatePaymentAllocation, secondPayment.Id, 2)
				if candidate.PostingAllocationSequence != 2 {
					t.Fatalf("payment candidate lost separate posting-allocation sequence: %#v", candidate)
				}
				foundSecondAllocation := false
				for _, payment := range record.Payments {
					if payment.Id == secondPayment.Id {
						if len(payment.Allocations) != 2 {
							t.Fatalf("second writer payment retained %d allocations, want both posting allocations", len(payment.Allocations))
						}
						for _, allocation := range payment.Allocations {
							if allocation.Sequence == 2 {
								foundSecondAllocation = true
							}
						}
					}
				}
				if !foundSecondAllocation {
					t.Fatal("second writer payment body omitted its exact allocation sequence 2")
				}
			} else if len(discovery.Candidates) != 2 {
				t.Fatalf("non-cash source candidate count=%d, want two posting allocation units", len(discovery.Candidates))
			}
		})
	}
}

func TestPurchaseBASModernNominationCandidatesStayStableAcrossRepeatedClaims(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f, repo, state, first := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	secondPayment, err := repo.RecordPayment(ctx, paymentRequest(t, f, state.AccountingVersion, 2750, paymentDate(9, 29)))
	if err != nil {
		t.Fatal("record sibling partial payment:", err)
	}
	if secondPayment.Payment == nil || secondPayment.Accounting == nil || len(secondPayment.Payment.Allocations) != 1 || secondPayment.Payment.Allocations[0].EligibleGst.MinorUnits != 250 {
		t.Fatalf("sibling cash payment extent=%v, want one 250-cent source", secondPayment.Payment)
	}
	second := basClaimOraclePaymentPortion(t, first, secondPayment)
	preDeferral, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	basClaimOracleAssertLedger(t, tx, preDeferral, 250, 750, 0)

	beforeDiscovery, beforeSummary, _ := purchaseBASModernRead(t, ctx, tx, f, purchaseBASModernTestCandidateLimit)
	beforeCoverage := captureCoverage(t, repo, ctx)
	purchaseBASModernAssertCandidateMatchesCoverage(t, beforeDiscovery, beforeCoverage)
	postingID := first.GetPayment().PostingId
	writerDerivedCandidates := []purchases.BASModernCandidateKey{
		{Kind: purchases.BASModernCandidatePostingAllocation, OrganisationID: postingOrg, PostingID: postingID, DocumentID: f.document.Id, Sequence: 1},
		{Kind: purchases.BASModernCandidatePaymentAllocation, OrganisationID: postingOrg, PostingID: postingID, DocumentID: f.document.Id,
			PaymentID: first.GetPayment().PaymentId, Sequence: 1, PostingAllocationSequence: 1},
		{Kind: purchases.BASModernCandidatePaymentAllocation, OrganisationID: postingOrg, PostingID: postingID, DocumentID: f.document.Id,
			PaymentID: secondPayment.Payment.Id, Sequence: 1, PostingAllocationSequence: 1},
	}
	sort.Slice(writerDerivedCandidates, func(i, j int) bool {
		if writerDerivedCandidates[i].Kind != writerDerivedCandidates[j].Kind {
			return writerDerivedCandidates[i].Kind < writerDerivedCandidates[j].Kind
		}
		if writerDerivedCandidates[i].PostingID != writerDerivedCandidates[j].PostingID {
			return writerDerivedCandidates[i].PostingID < writerDerivedCandidates[j].PostingID
		}
		if writerDerivedCandidates[i].PaymentID != writerDerivedCandidates[j].PaymentID {
			return writerDerivedCandidates[i].PaymentID < writerDerivedCandidates[j].PaymentID
		}
		return writerDerivedCandidates[i].Sequence < writerDerivedCandidates[j].Sequence
	})
	if len(beforeDiscovery.Candidates) != 3 || !reflect.DeepEqual(beforeDiscovery.Candidates, writerDerivedCandidates) {
		t.Fatalf("cash fixture writer-derived candidate set=%#v, want exactly three modern source units %#v", beforeDiscovery.Candidates, writerDerivedCandidates)
	}
	beforeCandidates := append([]purchases.BASModernCandidateKey(nil), beforeDiscovery.Candidates...)

	firstRequest := deferRequest(t, f, preDeferral, first)
	firstRequest.Portions = append(firstRequest.Portions, proto.Clone(second).(*tammyv1.PurchaseGstSourcePortion))
	firstDeferral, err := deferCommand(t, repo, firstRequest)
	if err != nil {
		t.Fatalf("retain first multi-source deferral: %v", err)
	}
	f.now = time.Date(2027, 1, 7, 12, 0, 0, 0, time.UTC)
	nextPeriod := basClaimOracleNextPeriod()
	secondRequest := deferRequest(t, f, firstDeferral.Accounting, first)
	secondRequest.AffectedPeriod = nextPeriod
	secondRequest.TimingReview.ReviewedAsOf = basClaimOracleLaterReview()
	secondDeferral, err := deferCommand(t, repo, secondRequest)
	if err != nil {
		t.Fatalf("retain repeated source deferral: %v", err)
	}
	claim := claimRequest(t, f, secondDeferral.Accounting, first)
	claim.ClaimPeriod = proto.Clone(nextPeriod).(*tammyv1.PurchaseClaimPeriod)
	claim.ReleasePostingDate = basClaimOracleLaterReview()
	claim.TimingReview.ReviewedAsOf = basClaimOracleLaterReview()
	claim.DeferralId = &secondDeferral.Deferral.Id
	claimed, err := attributeCommand(t, repo, claim)
	if err != nil {
		t.Fatalf("attribute selected source and close its linked deferral: %v", err)
	}
	if len(claimed.Attributions) != 1 || claimed.Attributions[0].ClaimedGst.MinorUnits != 500 ||
		!proto.Equal(claimed.Attributions[0].Portion, first) || claimed.Attributions[0].GetClosesDeferralId() != secondDeferral.Deferral.Id {
		t.Fatalf("selected source attribution differs from exact claim: %#v", claimed.Attributions)
	}

	afterWriters := purchaseBASScalarState(t, tx)
	current, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "post-claim accounting replay")
	basClaimOracleAssertLedger(t, tx, current, 250, 250, 500)
	if current.Paid.MinorUnits != 8250 || current.Outstanding.MinorUnits != 2750 {
		t.Fatalf("candidate decisions changed partial settlement: paid=%v outstanding=%v", current.Paid, current.Outstanding)
	}
	history, err := repo.VerifiedClaims(ctx, postingOrg, f.document.Id)
	if err != nil || len(history.Deferrals) != 2 || len(history.Attributions) != 1 {
		t.Fatalf("verified claim history=%#v err=%v", history, err)
	}
	writerDeferrals := []*tammyv1.PurchaseGstDeferral{firstDeferral.Deferral, secondDeferral.Deferral}
	wantPortions := map[basClaimOracleDeferralIdentity]struct {
		portion   *tammyv1.PurchaseGstSourcePortion
		allocated int64
		remaining int64
	}{
		{firstDeferral.Deferral.Id, 1}:  {first, 500, 0},
		{firstDeferral.Deferral.Id, 2}:  {second, 250, 250},
		{secondDeferral.Deferral.Id, 1}: {first, 500, 0},
	}
	uniqueOutstanding := basClaimOracleVerifiedPortions(t, history, wantPortions)
	if len(uniqueOutstanding) != 2 || uniqueOutstanding[basClaimOracleSource(t, first)] != 0 || uniqueOutstanding[basClaimOracleSource(t, second)] != 250 {
		t.Fatalf("repeated decisions changed exact sibling remainder: %#v", uniqueOutstanding)
	}

	afterDiscovery, afterSummary, _ := purchaseBASModernRead(t, ctx, tx, f, purchaseBASModernTestCandidateLimit)
	afterCoverage := captureCoverage(t, repo, ctx)
	purchaseBASModernAssertCandidateMatchesCoverage(t, afterDiscovery, afterCoverage)
	if !reflect.DeepEqual(afterDiscovery.Candidates, beforeCandidates) {
		t.Fatalf("claim/deferral history changed structural candidates: before=%#v after=%#v", beforeCandidates, afterDiscovery.Candidates)
	}
	if afterSummary.ScalarKeyCount <= beforeSummary.ScalarKeyCount ||
		afterSummary.FamilyCounts[purchases.BASScalarDeferral-1] != 2 ||
		afterSummary.FamilyCounts[purchases.BASScalarDeferralPortion-1] != 3 ||
		afterSummary.FamilyCounts[purchases.BASScalarAttribution-1] != 1 {
		t.Fatalf("physical decisions did not grow independently of candidates: before=%#v after=%#v", beforeSummary, afterSummary)
	}
	basClaimOracleAssertCapturedClaims(t, afterCoverage, state.Posting.Id, writerDeferrals, claimed.Attributions, wantPortions)
	basClaimOracleAssertScalarHistory(t, afterDiscovery.PhysicalKeys, writerDeferrals, claimed.Attributions,
		map[basClaimOracleDeferralIdentity]*tammyv1.PurchaseGstSourcePortion{
			{firstDeferral.Deferral.Id, 1}:  first,
			{firstDeferral.Deferral.Id, 2}:  second,
			{secondDeferral.Deferral.Id, 1}: first,
		},
		map[string]string{
			firstDeferral.Deferral.Id:  firstRequest.CommandContext.IdempotencyKey,
			secondDeferral.Deferral.Id: secondRequest.CommandContext.IdempotencyKey,
		},
		map[string]string{claimed.Attributions[0].Id: claim.CommandContext.IdempotencyKey},
		afterCoverage)
}

func TestPurchaseBASModernNominationLegacyOnlyKeysAllowZeroCandidates(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request); err != nil {
		t.Fatalf("retain legacy review and source history: %v", err)
	}
	manual := f.seedPriorJournal(request.Review.Source.GetDocument().DocumentId,
		tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
	openingPayableID := legacyOpeningFixture(t, f)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := f.repo(probe)
	before := purchaseBASScalarState(t, tx)
	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil || summary.ScalarKeyCount < 5 ||
		summary.FamilyCounts[purchases.BASScalarDocument-1] < 1 ||
		summary.FamilyCounts[purchases.BASScalarLegacyReview-1] != 1 ||
		summary.FamilyCounts[purchases.BASScalarManualJournal-1] != 1 ||
		summary.FamilyCounts[purchases.BASScalarOpeningPayable-1] != 1 ||
		summary.FamilyCounts[purchases.BASScalarOpeningConversion-1] != 1 ||
		summary.FamilyCounts[purchases.BASScalarOpeningItem-1] != 1 {
		t.Fatalf("legacy-only physical summary=%#v err=%v", summary, err)
	}
	expected := *summary
	discovery, err := repository.ReadModernBASCandidates(ctx, postingOrg, *summary,
		summary.ScalarKeyCount, summary.ScalarBytes, 0)
	if err != nil || discovery == nil || len(discovery.Candidates) != 0 {
		t.Fatalf("legacy-only zero-budget discovery=%#v err=%v", discovery, err)
	}
	if !reflect.DeepEqual(*summary, expected) {
		t.Fatal("legacy-only nomination changed caller summary")
	}
	purchaseBASModernAssertPhysicalKeys(t, discovery, summary)
	manualRetained, openingRetained := false, false
	for _, key := range discovery.PhysicalKeys {
		if key.Kind == purchases.BASScalarManualJournal && key.ID == manual.Id {
			manualRetained = true
		}
		if key.Kind == purchases.BASScalarOpeningPayable && key.ID == openingPayableID {
			openingRetained = true
		}
	}
	if !manualRetained || !openingRetained {
		t.Fatalf("zero-candidate discovery lost non-candidate manual/opening identities: manual=%v opening=%v", manualRetained, openingRetained)
	}
	if probe.keyReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("zero-modern discovery lost legacy keys, read bodies, changed state, or ended the caller transaction")
	}
}

func TestPurchaseBASModernNominationRejectsShortBudgetsAndCancellation(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	if _, err := f.repo(tx).PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	_, summary, _ := purchaseBASModernRead(t, ctx, tx, f, purchaseBASModernTestCandidateLimit)
	expected := *summary
	purchaseBASModernAssertRejectedRead(t, tx, f, expected, ctx,
		expected.ScalarKeyCount-1, expected.ScalarBytes, purchaseBASModernTestCandidateLimit, 0, true)
	purchaseBASModernAssertRejectedRead(t, tx, f, expected, ctx,
		expected.ScalarKeyCount, expected.ScalarBytes-1, purchaseBASModernTestCandidateLimit, 0, true)
	purchaseBASModernAssertRejectedRead(t, tx, f, expected, ctx,
		expected.ScalarKeyCount, expected.ScalarBytes, 0, 1, true)
	purchaseBASModernAssertRejectedRead(t, tx, f, expected, ctx,
		expected.ScalarKeyCount, expected.ScalarBytes, -1, 0, false)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	purchaseBASModernAssertRejectedRead(t, tx, f, expected, cancelled,
		expected.ScalarKeyCount, expected.ScalarBytes, purchaseBASModernTestCandidateLimit, 0, false)

	mappingCancelled, cancelMapping := context.WithCancel(ctx)
	mappingProbe := &purchaseBASScalarProbe{Transaction: tx, afterKeyQuery: cancelMapping}
	mappingRepository := f.repo(mappingProbe)
	beforeMapping := purchaseBASScalarState(t, tx)
	mappingExpected := expected
	mappingDiscovery, mappingErr := mappingRepository.ReadModernBASCandidates(mappingCancelled, postingOrg, expected,
		expected.ScalarKeyCount, expected.ScalarBytes, purchaseBASModernTestCandidateLimit)
	if mappingDiscovery != nil || !errors.Is(mappingErr, context.Canceled) {
		t.Fatalf("cancelled post-key candidate mapping returned discovery=%#v err=%v", mappingDiscovery, mappingErr)
	}
	if !reflect.DeepEqual(expected, mappingExpected) || mappingProbe.keyReads != 1 || mappingProbe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(beforeMapping, purchaseBASScalarState(t, tx)) {
		t.Fatal("post-key cancellation changed input/state, read bodies, or ended the caller transaction")
	}
}

func TestPurchaseBASModernNominationStructuralAllocationBoundary(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	posted := postReportingPurchase(t, f, "MODERN-CANDIDATE-BOUNDARY", postDate(10), 10000, 1000)
	appendPurchaseBASScalarAllocationCountRows(t, tx, posted.Posting.Id, 999, 1)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := f.repo(probe)
	before := purchaseBASScalarState(t, tx)
	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil || summary.FamilyCounts[purchases.BASScalarPostingAllocation-1] != purchaseBASModernTestCandidateLimit {
		t.Fatalf("count-only exact candidate boundary summary=%#v err=%v", summary, err)
	}
	expected := *summary
	discovery, err := repository.ReadModernBASCandidates(ctx, postingOrg, *summary,
		summary.ScalarKeyCount, summary.ScalarBytes, purchaseBASModernTestCandidateLimit)
	candidateCount := -1
	if discovery != nil {
		candidateCount = len(discovery.Candidates)
	}
	if err != nil || discovery == nil || candidateCount != int(purchaseBASModernTestCandidateLimit) {
		t.Fatalf("count-only exact candidate boundary returned %d candidates, err=%v", candidateCount, err)
	}
	if !reflect.DeepEqual(*summary, expected) || probe.keyReads != 1 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("exact count-only candidate boundary changed summary/state, read bodies, or ended caller transaction")
	}
	for index, candidate := range discovery.Candidates {
		if candidate.Kind != purchases.BASModernCandidatePostingAllocation ||
			candidate.PostingID != posted.Posting.Id || candidate.DocumentID != posted.Posting.DocumentId ||
			candidate.Sequence != uint64(index+1) || candidate.PostingAllocationSequence != 0 {
			t.Fatalf("count-only candidate %d lost exact structural identity: %#v", index, candidate)
		}
	}

	appendPurchaseBASScalarAllocationCountRows(t, tx, posted.Posting.Id, 1, purchaseBASModernTestCandidateLimit)
	beforeOverflow := purchaseBASScalarState(t, tx)
	overflowSummary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || overflowSummary == nil || overflowSummary.FamilyCounts[purchases.BASScalarPostingAllocation-1] != purchaseBASModernTestCandidateLimit+1 {
		t.Fatalf("count-only candidate overflow summary=%#v err=%v", overflowSummary, err)
	}
	overflowExpected := *overflowSummary
	overflow, err := repository.ReadModernBASCandidates(ctx, postingOrg, *overflowSummary,
		overflowSummary.ScalarKeyCount, overflowSummary.ScalarBytes, purchaseBASModernTestCandidateLimit+1)
	if overflow != nil || !errors.Is(err, purchases.ErrSourceBound) {
		t.Fatalf("1,001st count-only candidate returned partial discovery=%#v err=%v", overflow, err)
	}
	if !reflect.DeepEqual(*overflowSummary, overflowExpected) || probe.keyReads != 2 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(beforeOverflow, purchaseBASScalarState(t, tx)) {
		t.Fatal("candidate overflow changed summary/state, did not use exactly one key read, read bodies, or ended caller transaction")
	}
}
