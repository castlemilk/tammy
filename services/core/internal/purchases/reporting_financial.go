package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

// BASPurchaseUnitRef identifies one exact modern source component or one exact
// legacy root. Exactly one field is set.
type BASPurchaseUnitRef struct {
	Modern *BASModernCandidateKey
	Legacy *BASLegacyCandidateKey
}

// BASPurchaseFinancialUnit preserves the frozen candidate membership. Source
// is the ordinary zero-or-one BAS line for a modern component; attribution
// lines are represented separately. A suppressed legacy nomination remains
// visible with IsCandidate false and CandidateGST zero.
type BASPurchaseFinancialUnit struct {
	Candidate    BASPurchaseUnitRef
	IsCandidate  bool
	Source       *tammyv1.BasPurchaseSourceLine
	CandidateGST int64
	Status       tammyv1.PurchaseReviewStatus
	Excluded     bool
	EvidenceOnly bool
}

type BASPurchaseAttributionUnit struct {
	Candidate BASModernCandidateKey
	Source    *tammyv1.BasPurchaseSourceLine
}

type BASPurchasePostingReview struct {
	PostingID string
	Review    *tammyv1.BasPurchaseReview
}

type BASPurchaseLegacyReview struct {
	Candidate   BASLegacyCandidateKey
	IsCandidate bool
	Review      *tammyv1.BasPurchaseReview
}

// BASPurchaseFinancialMap is the pure purchase-owned projection consumed by
// the BAS workpaper writer. Its aggregate fields match the mature BASSnapshot
// projection; candidate and review ownership remain explicit for auditability.
type BASPurchaseFinancialMap struct {
	Units                     []BASPurchaseFinancialUnit
	Attributions              []BASPurchaseAttributionUnit
	Sources                   []*tammyv1.BasPurchaseSourceLine
	PostingReviews            []BASPurchasePostingReview
	LegacyReviews             []BASPurchaseLegacyReview
	Reviews                   []*tammyv1.BasPurchaseReview
	LegacySupport             []*tammyv1.BasPurchaseLegacySupport
	LegacyCandidateGST        *tammyv1.Money
	ExcludedCashPurchaseCount uint64
}

type basPurchasePaymentOwner struct {
	posting PostingReportingRecord
	payment *tammyv1.PurchasePayment
}

type basPurchaseMapIndex struct {
	postings    map[string]PostingReportingRecord
	documents   map[string]*tammyv1.Document
	payments    map[string]basPurchasePaymentOwner
	journals    map[string]*accounting.JournalReportingRecord
	accounts    map[string]PurchaseReportingRetainedRow
	lineJournal map[string]string
}

// MapBASPurchaseReporting applies mature BAS source/disposition semantics to
// the complete pre-dedup capture view and frozen discovery values. Callers must
// first complete ReconcileBASLegacyCandidates and invoke this mapper before
// discarding the raw tax_facts rows used by ReconcileBASPhysicalKeys. It never
// reads a repository, widens a selector, or calls the mature CaptureBAS reader.
func MapBASPurchaseReporting(
	ctx context.Context,
	capture *PurchaseCoverageReportingCapture,
	modern *BASModernCandidateDiscovery,
	legacy *BASLegacyCandidateDiscovery,
	currentGstBasis tammyv1.GstBasis,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
	asOf time.Time,
) (*BASPurchaseFinancialMap, error) {
	fail := func(reason string) (*BASPurchaseFinancialMap, error) {
		return nil, fmt.Errorf("%w: BAS purchase financial map: %s", ErrRepository, reason)
	}
	if ctx == nil || capture == nil || modern == nil || legacy == nil || !ids.IsCanonicalV7(capture.OrganisationID) ||
		(currentGstBasis != tammyv1.GstBasis_GST_BASIS_CASH && currentGstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) ||
		!validClaimTimingPeriod(period) || !validPurchaseDate(balance) || asOf.IsZero() || !validPurchaseDate(purchaseAsOf(asOf)) {
		return fail("invalid capture, owner, period, balance, basis, or as-of")
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if modern.PhysicalKeys == nil || len(modern.Candidates) > maximumBASModernCandidates || len(legacy.Candidates) > maximumBASLegacyCandidates {
		return fail("modern discovery has no frozen physical key set")
	}
	if err := ReconcileBASPhysicalKeys(ctx, capture.OrganisationID, modern.PhysicalKeys, capture); err != nil {
		return nil, err
	}
	wantedModern, err := nominateModernBASCandidates(modern.PhysicalKeys)
	if err != nil || !basPurchaseSameModernKeys(wantedModern, modern.Candidates) {
		return fail("modern candidates differ from their frozen physical keys")
	}

	index, err := basPurchaseBuildMapIndex(capture)
	if err != nil {
		return nil, err
	}
	if err := basPurchaseValidateModernBodies(ctx, capture, modern, index); err != nil {
		return nil, err
	}
	legacyRecords, legacyCandidates, err := basPurchaseValidateLegacyNominations(ctx, capture, legacy)
	if err != nil {
		return nil, err
	}

	result := &BASPurchaseFinancialMap{LegacyCandidateGST: purchaseJournalMoney(0)}
	modernUnitIndex := make(map[BASModernCandidateKey]int, len(modern.Candidates))
	for _, key := range modern.Candidates {
		if _, duplicate := modernUnitIndex[key]; duplicate {
			return fail("duplicate modern candidate key")
		}
		keyCopy := key
		modernUnitIndex[key] = len(result.Units)
		result.Units = append(result.Units, BASPurchaseFinancialUnit{
			Candidate: BASPurchaseUnitRef{Modern: &keyCopy}, IsCandidate: true,
		})
	}
	if err := basPurchaseMapModern(ctx, capture, index, modernUnitIndex, period, asOf, result); err != nil {
		return nil, err
	}
	if err := basPurchaseMapLegacy(ctx, legacy, legacyRecords, legacyCandidates, index, currentGstBasis, period, balance, result); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	sort.Slice(result.LegacySupport, func(i, j int) bool {
		left, right := result.LegacySupport[i], result.LegacySupport[j]
		if left.ReviewId != right.ReviewId {
			return left.ReviewId < right.ReviewId
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Sequence < right.Sequence
	})
	if err := basSnapshotBounds(&BASSnapshot{Sources: result.Sources, Reviews: result.Reviews, LegacySupport: result.LegacySupport}); err != nil {
		return nil, err
	}
	return result, nil
}

func basPurchaseBuildMapIndex(capture *PurchaseCoverageReportingCapture) (*basPurchaseMapIndex, error) {
	if capture == nil {
		return nil, ErrRepository
	}
	index := &basPurchaseMapIndex{
		postings:    make(map[string]PostingReportingRecord, len(capture.Postings)),
		documents:   make(map[string]*tammyv1.Document, len(capture.Documents)),
		payments:    make(map[string]basPurchasePaymentOwner),
		journals:    make(map[string]*accounting.JournalReportingRecord, len(capture.Journals)),
		accounts:    make(map[string]PurchaseReportingRetainedRow),
		lineJournal: make(map[string]string),
	}
	for _, retained := range capture.Documents {
		doc := retained.Document
		if doc == nil || !ids.IsCanonicalV7(doc.Id) || doc.OrganisationId != capture.OrganisationID {
			return nil, ErrRepository
		}
		if _, exists := index.documents[doc.Id]; exists {
			return nil, ErrRepository
		}
		index.documents[doc.Id] = doc
	}
	for _, row := range capture.Rows {
		switch row.Table {
		case "accounts":
			id := postingReportingString(row, "id")
			if id == "" || postingReportingString(row, "organisation_id") != capture.OrganisationID {
				return nil, ErrRepository
			}
			if _, exists := index.accounts[id]; exists {
				return nil, ErrRepository
			}
			index.accounts[id] = row
		}
	}
	for _, proof := range capture.Journals {
		if proof == nil || proof.Journal == nil || proof.Journal.Id == "" || proof.Journal.OrganisationId != capture.OrganisationID ||
			proof.Journal.Version == 0 || len(proof.ContentHash) != sha256.Size || proof.PayloadBytes <= 0 {
			return nil, ErrRepository
		}
		id := proof.Journal.Id
		if _, exists := index.journals[id]; exists {
			return nil, ErrRepository
		}
		if !basPurchaseJournalStateMatches(proof) {
			return nil, ErrRepository
		}
		index.journals[id] = proof
		for _, line := range proof.Journal.Lines {
			if line == nil || line.Id == "" || line.JournalId != id || line.AccountId == "" || line.Sequence == 0 ||
				line.Debit == nil || line.Credit == nil || line.Debit.CurrencyCode != "AUD" || line.Credit.CurrencyCode != "AUD" ||
				line.Debit.MinorUnits < 0 || line.Credit.MinorUnits < 0 {
				return nil, ErrRepository
			}
			if _, exists := index.lineJournal[line.Id]; exists {
				return nil, ErrRepository
			}
			account, err := index.account(line.AccountId, capture.OrganisationID)
			if err != nil {
				return nil, err
			}
			_ = account
			index.lineJournal[line.Id] = id
		}
	}
	for _, record := range capture.Postings {
		posting := record.Posting
		state := record.Accounting
		if posting == nil || state == nil || !ids.IsCanonicalV7(posting.Id) || posting.OrganisationId != capture.OrganisationID ||
			posting.DocumentId == "" || !state.Posted || state.OrganisationId != capture.OrganisationID || state.DocumentId != posting.DocumentId ||
			state.Posting == nil || !proto.Equal(state.Posting, posting) || state.ReviewOutcomes == nil ||
			!validPurchaseMoney(state.EligibleGst) || !validPurchaseMoney(state.DeferredGst) || !validPurchaseMoney(state.SuspenseGst) {
			return nil, ErrRepository
		}
		if _, exists := index.postings[posting.Id]; exists {
			return nil, ErrRepository
		}
		doc, exists := index.documents[posting.DocumentId]
		if !exists || doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || doc.Candidate == nil {
			return nil, ErrRepository
		}
		index.postings[posting.Id] = record
		for _, payment := range record.Payments {
			if payment == nil || !ids.IsCanonicalV7(payment.Id) || payment.OrganisationId != capture.OrganisationID ||
				payment.PostingId != posting.Id || payment.DocumentId != posting.DocumentId || payment.AccountingVersion == 0 ||
				payment.JournalId == "" || !validPurchaseDate(payment.ActualPaymentDate) {
				return nil, ErrRepository
			}
			if _, exists := index.payments[payment.Id]; exists {
				return nil, ErrRepository
			}
			index.payments[payment.Id] = basPurchasePaymentOwner{posting: record, payment: payment}
		}
		if len(record.Evidences) == 0 {
			return nil, ErrRepository
		}
		for evidenceIndex, evidence := range record.Evidences {
			if evidence == nil || evidence.Id == "" || evidence.OrganisationId != capture.OrganisationID ||
				evidence.DocumentId != posting.DocumentId || evidence.AccountingVersion == 0 ||
				(evidenceIndex > 0 && record.Evidences[evidenceIndex-1].AccountingVersion >= evidence.AccountingVersion) {
				return nil, ErrRepository
			}
		}
	}
	return index, nil
}

func basPurchaseJournalStateMatches(proof *accounting.JournalReportingRecord) bool {
	if proof == nil || proof.Journal == nil {
		return false
	}
	switch proof.Journal.State {
	case tammyv1.JournalState_JOURNAL_STATE_UNSPECIFIED:
		return proof.State == "DRAFT"
	case tammyv1.JournalState_JOURNAL_STATE_POSTED:
		return proof.State == "POSTED"
	case tammyv1.JournalState_JOURNAL_STATE_REVERSED:
		return proof.State == "REVERSED"
	default:
		return false
	}
}

func (index *basPurchaseMapIndex) account(id, organisationID string) (*tammyv1.Account, error) {
	if index == nil || id == "" {
		return nil, ErrRepository
	}
	row, exists := index.accounts[id]
	version, versionOK := purchaseCoverageNullableInteger(row, "version")
	code, codeOK := purchaseCoverageNullableText(row, "code")
	classification, classOK := purchaseCoverageNullableText(row, "report_classification")
	if !exists || postingReportingString(row, "id") != id || postingReportingString(row, "organisation_id") != organisationID ||
		!versionOK || version <= 0 || !codeOK || !classOK {
		return nil, ErrRepository
	}
	return &tammyv1.Account{Id: id, OrganisationId: organisationID, Version: uint64(version), Code: code, ReportClassification: classification}, nil
}

func basPurchaseSameModernKeys(left, right []BASModernCandidateKey) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func basPurchaseValidateModernBodies(ctx context.Context, capture *PurchaseCoverageReportingCapture, discovery *BASModernCandidateDiscovery, index *basPurchaseMapIndex) error {
	fail := func() error { return fmt.Errorf("%w: modern candidate bodies differ from frozen keys", ErrRepository) }
	candidateSet := make(map[BASModernCandidateKey]struct{}, len(discovery.Candidates))
	for _, key := range discovery.Candidates {
		if key.OrganisationID != capture.OrganisationID || key.PostingID == "" || key.DocumentID == "" {
			return fail()
		}
		if _, duplicate := candidateSet[key]; duplicate {
			return fail()
		}
		candidateSet[key] = struct{}{}
	}
	expected := make(map[BASModernCandidateKey]struct{}, len(candidateSet))
	add := func(key BASModernCandidateKey) error {
		if _, exists := candidateSet[key]; !exists {
			return fail()
		}
		if _, duplicate := expected[key]; duplicate {
			return fail()
		}
		expected[key] = struct{}{}
		return nil
	}
	for _, record := range capture.Postings {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		posting := record.Posting
		if posting.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH && posting.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH {
			return fail()
		}
		base := BASModernCandidateKey{OrganisationID: posting.OrganisationId, PostingID: posting.Id, DocumentID: posting.DocumentId}
		if len(posting.Allocations) == 0 {
			base.Kind = BASModernCandidatePosting
			if err := add(base); err != nil {
				return err
			}
		} else {
			for allocationIndex, allocation := range posting.Allocations {
				if allocation == nil || allocation.Sequence != uint32(allocationIndex+1) || !validPurchaseMoney(allocation.Gross) ||
					!validPurchaseMoney(allocation.SupplierGst) || !validPurchaseMoney(allocation.EligibleGst) {
					return fail()
				}
				key := base
				key.Kind, key.Sequence = BASModernCandidatePostingAllocation, uint64(allocation.Sequence)
				if err := add(key); err != nil {
					return err
				}
			}
		}
		for _, payment := range record.Payments {
			paymentBase := base
			paymentBase.PaymentID = payment.Id
			if len(payment.Allocations) == 0 {
				paymentBase.Kind = BASModernCandidatePayment
				if err := add(paymentBase); err != nil {
					return err
				}
				continue
			}
			for allocationIndex, allocation := range payment.Allocations {
				if allocation == nil || allocation.Sequence != uint32(allocationIndex+1) || allocation.Sequence == 0 || int(allocation.Sequence) > len(posting.Allocations) ||
					!validPurchaseMoney(allocation.Gross) || !validPurchaseMoney(allocation.SupplierGst) || !validPurchaseMoney(allocation.EligibleGst) {
					return fail()
				}
				key := paymentBase
				key.Kind, key.Sequence = BASModernCandidatePaymentAllocation, uint64(allocation.Sequence)
				key.PostingAllocationSequence = uint64(allocation.Sequence)
				if err := add(key); err != nil {
					return err
				}
			}
		}
	}
	if len(expected) != len(candidateSet) {
		return fail()
	}
	for key := range candidateSet {
		if _, exists := expected[key]; !exists {
			return fail()
		}
	}
	for _, record := range capture.Postings {
		posting := record.Posting
		if posting.JournalId == nil {
			return fail()
		}
		if _, err := basPurchaseAutomaticCredit(index, capture.OrganisationID, *posting.JournalId,
			tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE, "PAYABLE", 6, posting.Id, 1); err != nil {
			return err
		}
		for _, payment := range record.Payments {
			if _, err := basPurchaseAutomaticCredit(index, capture.OrganisationID, payment.JournalId,
				tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT, "BANKING", 7, payment.Id, 1); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func basPurchaseAutomaticCredit(index *basPurchaseMapIndex, organisationID, journalID string,
	journalSource tammyv1.JournalSource, sourceType string, sourceKind int64, sourceID string, sourceRevision uint64,
) (bool, error) {
	proof := index.journals[journalID]
	if proof == nil || proof.Journal == nil || proof.Journal.Id != journalID || proof.Journal.OrganisationId != organisationID ||
		proof.Journal.Source != journalSource || proof.SourceType != sourceType || proof.SourceKind != sourceKind ||
		proof.SourceID != sourceID || proof.SourceRevision != sourceRevision || proof.State != "POSTED" ||
		proof.Journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED {
		return false, ErrRepository
	}
	var debit int64
	for _, line := range proof.Journal.Lines {
		account, err := index.account(line.AccountId, organisationID)
		if err != nil {
			return false, err
		}
		if account.Code != "1150" {
			continue
		}
		if line.Debit.MinorUnits > math.MaxInt64-debit {
			return false, ErrRepository
		}
		debit += line.Debit.MinorUnits
	}
	return debit > 0, nil
}

func basPurchaseMapModern(
	ctx context.Context,
	capture *PurchaseCoverageReportingCapture,
	index *basPurchaseMapIndex,
	unitIndexes map[BASModernCandidateKey]int,
	period *tammyv1.PurchaseClaimPeriod,
	asOf time.Time,
	result *BASPurchaseFinancialMap,
) error {
	for _, record := range capture.Postings {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		posting := record.Posting
		state := record.Accounting
		localSources := make([]*tammyv1.BasPurchaseSourceLine, 0)
		addSource := func(key BASModernCandidateKey, line *tammyv1.BasPurchaseSourceLine) error {
			unitIndex, exists := unitIndexes[key]
			if !exists || line == nil || result.Units[unitIndex].Source != nil {
				return ErrRepository
			}
			result.Units[unitIndex].Source = line
			result.Sources = append(result.Sources, line)
			localSources = append(localSources, line)
			return nil
		}
		if posting.GstBasis == tammyv1.GstBasis_GST_BASIS_NON_CASH && basDateInPeriod(posting.InvoiceDate, period) {
			automatic, err := basPurchaseAutomaticCredit(index, capture.OrganisationID, posting.GetJournalId(),
				tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE, "PAYABLE", 6, posting.Id, 1)
			if err != nil {
				return err
			}
			for _, allocation := range posting.Allocations {
				key := BASModernCandidateKey{Kind: BASModernCandidatePostingAllocation, OrganisationID: posting.OrganisationId,
					PostingID: posting.Id, DocumentID: posting.DocumentId, Sequence: uint64(allocation.Sequence)}
				if automatic || allocation.EligibleGst.MinorUnits == 0 {
					line := basPostingSource(posting, allocation, period)
					if automatic {
						line.ClaimedGst = proto.Clone(allocation.EligibleGst).(*tammyv1.Money)
					}
					line.Evidence = basEvidenceRef(record.Evidences[0])
					if err := addSource(key, line); err != nil {
						return err
					}
				}
			}
		}
		if posting.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH {
			for _, payment := range record.Payments {
				if !basDateInPeriod(payment.ActualPaymentDate, period) {
					continue
				}
				automatic, err := basPurchaseAutomaticCredit(index, capture.OrganisationID, payment.JournalId,
					tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT, "BANKING", 7, payment.Id, 1)
				if err != nil {
					return err
				}
				evidence := basEvidenceAt(record.Evidences, payment.AccountingVersion)
				if evidence == nil {
					return ErrRepository
				}
				for _, allocation := range payment.Allocations {
					key := BASModernCandidateKey{Kind: BASModernCandidatePaymentAllocation, OrganisationID: posting.OrganisationId,
						PostingID: posting.Id, DocumentID: posting.DocumentId, PaymentID: payment.Id,
						Sequence: uint64(allocation.Sequence), PostingAllocationSequence: uint64(allocation.Sequence)}
					if automatic || allocation.EligibleGst.MinorUnits == 0 {
						line := basPaymentSource(posting, payment, allocation, period)
						if automatic {
							line.ClaimedGst = proto.Clone(allocation.EligibleGst).(*tammyv1.Money)
						}
						line.Evidence = basEvidenceRef(evidence)
						if err := addSource(key, line); err != nil {
							return err
						}
					}
				}
			}
		}
		for _, attribution := range record.Claims.Attributions {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err)
			}
			if attribution == nil {
				return ErrRepository
			}
			if !basSamePeriod(attribution.ClaimPeriod, period) {
				continue
			}
			key, err := basPurchaseAttributionCandidate(posting, record.Payments, attribution)
			if err != nil {
				return err
			}
			if _, exists := unitIndexes[key]; !exists {
				return ErrRepository
			}
			line, err := basAttributionSource(posting, record.Payments, record.Evidences, attribution)
			if err != nil {
				return err
			}
			result.Attributions = append(result.Attributions, BASPurchaseAttributionUnit{Candidate: key, Source: line})
			result.Sources = append(result.Sources, line)
			localSources = append(localSources, line)
		}
		local := &BASSnapshot{Sources: localSources}
		var reader Repository
		if err := reader.basPostingReviews(local, state, record.Payments, record.Evidences, record.Claims, period, asOf); err != nil {
			return err
		}
		for _, review := range local.Reviews {
			result.PostingReviews = append(result.PostingReviews, BASPurchasePostingReview{PostingID: posting.Id, Review: review})
			result.Reviews = append(result.Reviews, review)
		}
	}
	return ctx.Err()
}

func basPurchaseAttributionCandidate(posting *tammyv1.PurchasePosting, payments []*tammyv1.PurchasePayment, attribution *tammyv1.PurchaseGstAttribution) (BASModernCandidateKey, error) {
	if posting == nil || attribution == nil || attribution.Portion == nil || attribution.PostingId != posting.Id || attribution.DocumentId != posting.DocumentId {
		return BASModernCandidateKey{}, ErrRepository
	}
	base := BASModernCandidateKey{OrganisationID: posting.OrganisationId, PostingID: posting.Id, DocumentID: posting.DocumentId}
	if ref := attribution.Portion.GetPosting(); ref != nil {
		if ref.AllocationSequence == 0 || int(ref.AllocationSequence) > len(posting.Allocations) {
			return BASModernCandidateKey{}, ErrRepository
		}
		allocation := posting.Allocations[ref.AllocationSequence-1]
		if allocation == nil || allocation.Sequence != ref.AllocationSequence {
			return BASModernCandidateKey{}, ErrRepository
		}
		base.Kind, base.Sequence = BASModernCandidatePostingAllocation, uint64(ref.AllocationSequence)
		return base, nil
	}
	if ref := attribution.Portion.GetPayment(); ref != nil {
		if ref.PaymentId == "" || ref.AllocationSequence == 0 {
			return BASModernCandidateKey{}, ErrRepository
		}
		for _, payment := range payments {
			if payment.Id != ref.PaymentId {
				continue
			}
			if int(ref.AllocationSequence) > len(payment.Allocations) {
				return BASModernCandidateKey{}, ErrRepository
			}
			allocation := payment.Allocations[ref.AllocationSequence-1]
			if allocation == nil || allocation.Sequence != ref.AllocationSequence || allocation.Sequence == 0 {
				return BASModernCandidateKey{}, ErrRepository
			}
			base.Kind, base.PaymentID = BASModernCandidatePaymentAllocation, payment.Id
			base.Sequence = uint64(allocation.Sequence)
			base.PostingAllocationSequence = uint64(allocation.Sequence)
			return base, nil
		}
	}
	return BASModernCandidateKey{}, ErrRepository
}

func basPurchaseValidateLegacyNominations(
	ctx context.Context,
	capture *PurchaseCoverageReportingCapture,
	discovery *BASLegacyCandidateDiscovery,
) (map[BASLegacyCandidateKey]*LegacyPurchaseReportingRecord, map[BASLegacyCandidateKey]bool, error) {
	fail := func(reason string) (map[BASLegacyCandidateKey]*LegacyPurchaseReportingRecord, map[BASLegacyCandidateKey]bool, error) {
		return nil, nil, fmt.Errorf("%w: legacy nomination/capture mismatch: %s", ErrRepository, reason)
	}
	if discovery == nil {
		return fail("missing discovery")
	}
	organisationID := capture.OrganisationID
	if len(discovery.Candidates) > maximumBASLegacyCandidates {
		return fail("candidate limit exceeded")
	}
	candidates := make(map[BASLegacyCandidateKey]bool, len(discovery.Candidates))
	for _, key := range discovery.Candidates {
		if key.OrganisationID != organisationID || !ids.IsCanonicalV7(key.SourceID) ||
			(key.Kind == BASLegacyCandidateDocument && key.ConversionID != "") ||
			(key.Kind == BASLegacyCandidateOpeningPayable && !ids.IsCanonicalV7(key.ConversionID)) {
			return fail("candidate owner mismatch")
		}
		if _, duplicate := candidates[key]; duplicate {
			return fail("duplicate candidate")
		}
		candidates[key] = true
	}
	wantCandidates := make(map[BASLegacyCandidateKey]bool, len(discovery.Nominations))
	nominations := make(map[BASLegacyCandidateKey]BASLegacyNomination, len(discovery.Nominations))
	for _, nomination := range discovery.Nominations {
		if err := ctx.Err(); err != nil {
			return nil, nil, errors.Join(ErrRepository, err)
		}
		key := nomination.Key
		if key.OrganisationID != organisationID || !ids.IsCanonicalV7(key.SourceID) ||
			(key.Kind == BASLegacyCandidateDocument && key.ConversionID != "") ||
			(key.Kind == BASLegacyCandidateOpeningPayable && !ids.IsCanonicalV7(key.ConversionID)) ||
			(key.Kind != BASLegacyCandidateDocument && key.Kind != BASLegacyCandidateOpeningPayable) {
			return fail("malformed nomination key")
		}
		if _, duplicate := nominations[key]; duplicate {
			return fail("duplicate nomination")
		}
		nominations[key] = nomination
		if !nomination.ProvisionalEvidenceOnly {
			wantCandidates[key] = true
		}
	}
	for index := 1; index < len(discovery.Nominations); index++ {
		if compareBASLegacyCandidateKey(discovery.Nominations[index-1].Key, discovery.Nominations[index].Key) >= 0 {
			return fail("nominations are not in unique canonical order")
		}
	}
	for index := 1; index < len(discovery.Candidates); index++ {
		if compareBASLegacyCandidateKey(discovery.Candidates[index-1], discovery.Candidates[index]) >= 0 {
			return fail("candidates are not in unique canonical order")
		}
	}
	if len(wantCandidates) != len(candidates) {
		return fail("candidate and unsuppressed nomination counts differ")
	}
	for key := range wantCandidates {
		if !candidates[key] {
			return fail("candidate set differs from unsuppressed nominations")
		}
	}

	records := make(map[BASLegacyCandidateKey]*LegacyPurchaseReportingRecord, len(nominations))
	currentRoots := 0
	for _, record := range capture.LegacySources {
		if err := ctx.Err(); err != nil {
			return nil, nil, errors.Join(ErrRepository, err)
		}
		if record == nil || !record.CurrentRoot {
			continue
		}
		currentRoots++
		var key BASLegacyCandidateKey
		switch {
		case record.Document != nil && record.Opening == nil:
			key = BASLegacyCandidateKey{Kind: BASLegacyCandidateDocument, OrganisationID: organisationID, SourceID: record.Document.Id}
		case record.Opening != nil && record.Document == nil:
			key = BASLegacyCandidateKey{Kind: BASLegacyCandidateOpeningPayable, OrganisationID: organisationID,
				SourceID: record.Opening.ID, ConversionID: record.Opening.ConversionID}
		default:
			return fail("current root has ambiguous source shape")
		}
		if _, exists := records[key]; exists {
			return fail("duplicate current root")
		}
		nomination, exists := nominations[key]
		if !exists {
			return fail("captured current root is not nominated")
		}
		if err := basPurchaseCheckLegacyNomination(nomination, record); err != nil {
			return nil, nil, err
		}
		records[key] = record
	}
	if currentRoots != len(nominations) || len(records) != len(nominations) {
		return fail("nomination and current-root sets differ")
	}
	return records, candidates, nil
}

func basPurchaseCheckLegacyNomination(nomination BASLegacyNomination, record *LegacyPurchaseReportingRecord) error {
	if record == nil || !record.CurrentRoot {
		return ErrRepository
	}
	switch nomination.Key.Kind {
	case BASLegacyCandidateDocument:
		doc := record.Document
		if doc == nil || doc.Id != nomination.Key.SourceID || doc.Version != nomination.SourceVersion || len(doc.Sha256) != sha256.Size ||
			!bytes.Equal(doc.Sha256, nomination.DocumentRawSHA256) || (doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED &&
			doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW) {
			return ErrRepository
		}
		if doc.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW {
			if record.Source != nil || nomination.ProvisionalEvidenceOnly || len(nomination.ProvisionalDecisionIDs) != 0 {
				return ErrRepository
			}
			return nil
		}
		if doc.Candidate == nil || record.Source == nil {
			return ErrRepository
		}
		kind, id, version, sourceHash, reviewedHash, err := legacySourceScope(record.Source)
		if err != nil || kind != "DOCUMENT" || id != doc.Id || version != doc.Version || !bytes.Equal(sourceHash, doc.Sha256) {
			return ErrRepository
		}
		candidateBytes, err := purchaseBytes(doc.Candidate)
		if err != nil {
			return err
		}
		candidateHash := sha256.Sum256(candidateBytes)
		if !bytes.Equal(reviewedHash, candidateHash[:]) {
			return ErrRepository
		}
		currentIDs := make([]string, 0)
		for _, review := range record.Reviews {
			if review == nil || review.Review == nil {
				return ErrRepository
			}
			oldKind, oldID, oldVersion, oldHash, oldReviewedHash, oldErr := legacySourceScope(review.Review.Source)
			if oldErr != nil || review.OrganisationId != doc.OrganisationId || oldKind != kind || oldID != id {
				return ErrRepository
			}
			if oldVersion == version && bytes.Equal(oldHash, sourceHash) && bytes.Equal(oldReviewedHash, reviewedHash) {
				currentIDs = append(currentIDs, review.Id)
			}
		}
		sort.Strings(currentIDs)
		if !sameStrings(currentIDs, nomination.ProvisionalDecisionIDs) ||
			(nomination.ProvisionalEvidenceOnly && len(currentIDs) == 0) {
			return ErrRepository
		}
	case BASLegacyCandidateOpeningPayable:
		opening := record.Opening
		if opening == nil || opening.ID != nomination.Key.SourceID || opening.ConversionID != nomination.Key.ConversionID ||
			opening.OrganisationID != nomination.Key.OrganisationID || opening.SourceVersion != nomination.SourceVersion ||
			(opening.State != "POSTED" && opening.State != "REPLACED") || nomination.ProvisionalEvidenceOnly ||
			len(nomination.DocumentRawSHA256) != 0 || len(nomination.ProvisionalDecisionIDs) != 0 || record.Source == nil {
			return ErrRepository
		}
		kind, id, version, sourceHash, _, err := legacySourceScope(record.Source)
		if err != nil || kind != "OPENING_PAYABLE" || id != opening.ID || version != opening.SourceVersion || !bytes.Equal(sourceHash, opening.SourceHash) {
			return ErrRepository
		}
	default:
		return ErrRepository
	}
	return nil
}

func basPurchaseMapLegacy(
	ctx context.Context,
	discovery *BASLegacyCandidateDiscovery,
	records map[BASLegacyCandidateKey]*LegacyPurchaseReportingRecord,
	candidates map[BASLegacyCandidateKey]bool,
	index *basPurchaseMapIndex,
	basis tammyv1.GstBasis,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
	result *BASPurchaseFinancialMap,
) error {
	for _, nomination := range discovery.Nominations {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		key := nomination.Key
		record := records[key]
		if record == nil {
			return ErrRepository
		}
		isCandidate := candidates[key]
		keyCopy := key
		unit := BASPurchaseFinancialUnit{Candidate: BASPurchaseUnitRef{Legacy: &keyCopy}, IsCandidate: isCandidate}
		if key.Kind == BASLegacyCandidateDocument && record.Document.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW {
			result.Units = append(result.Units, unit)
			continue
		}
		value := &LegacyCandidate{
			Source: record.Source, Document: record.Document, Opening: record.Opening, Reviews: record.Reviews,
			Status: tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY,
		}
		candidateGST, err := basPurchaseLegacyCandidateGST(record)
		if err != nil {
			return err
		}
		value.CandidateGST = candidateGST
		if err := basPurchaseApplyLegacy(ctx, index, record, value, basis, period, balance); err != nil {
			return err
		}
		if nomination.ProvisionalEvidenceOnly && (!value.EvidenceOnly || !value.Excluded || value.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR) {
			return ErrRepository
		}
		unit.Status, unit.Excluded, unit.EvidenceOnly = value.Status, value.Excluded, value.EvidenceOnly
		// NEEDS_REVIEW roots are retained in the frozen nomination set, but
		// mature LegacyCandidates never treated them as financial candidates.
		financialCandidate := record.Opening != nil || (record.Document != nil &&
			record.Document.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED)
		if isCandidate && financialCandidate {
			unit.CandidateGST = candidateGST
			if !value.Excluded && !value.EvidenceOnly {
				if candidateGST < 0 || candidateGST > math.MaxInt64-result.LegacyCandidateGST.MinorUnits {
					return ErrRepository
				}
				result.LegacyCandidateGST.MinorUnits += candidateGST
				if basis == tammyv1.GstBasis_GST_BASIS_CASH {
					result.ExcludedCashPurchaseCount++
				}
			}
		}
		result.Units = append(result.Units, unit)
		base := basLegacyCandidateReview(value, period)
		if len(record.Reviews) == 0 {
			result.Reviews = append(result.Reviews, base)
			result.LegacyReviews = append(result.LegacyReviews, BASPurchaseLegacyReview{Candidate: key, IsCandidate: isCandidate, Review: base})
		}
		for _, review := range record.Reviews {
			if review == nil || review.Review == nil {
				return ErrRepository
			}
			line := proto.Clone(base).(*tammyv1.BasPurchaseReview)
			line.LegacyReview = basLegacyDecision(review)
			line.SourceRevision = review.SourceRevision
			line.SourceHash = append([]byte(nil), review.SourceHash...)
			line.Reason = review.Review.Reason
			if value.Excluded || value.EvidenceOnly {
				line.Kind = tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_RESOLVED_LEGACY
			}
			result.Reviews = append(result.Reviews, line)
			result.LegacyReviews = append(result.LegacyReviews, BASPurchaseLegacyReview{Candidate: key, IsCandidate: isCandidate, Review: line})
			for _, group := range []struct {
				kind tammyv1.BasPurchaseLegacySupportKind
				refs []*tammyv1.PurchaseRetainedSourceRef
			}{
				{tammyv1.BasPurchaseLegacySupportKind_BAS_PURCHASE_LEGACY_SUPPORT_KIND_PAYMENT, review.Review.SupportingPayments},
				{tammyv1.BasPurchaseLegacySupportKind_BAS_PURCHASE_LEGACY_SUPPORT_KIND_EVIDENCE, review.Review.SupportingEvidence},
			} {
				for index, ref := range group.refs {
					if ref == nil {
						return ErrRepository
					}
					result.LegacySupport = append(result.LegacySupport, &tammyv1.BasPurchaseLegacySupport{
						ReviewId: review.Id, Kind: group.kind, Sequence: uint32(index + 1), Source: proto.Clone(ref).(*tammyv1.PurchaseRetainedSourceRef),
					})
				}
			}
		}
	}
	return ctx.Err()
}

func basPurchaseLegacyCandidateGST(record *LegacyPurchaseReportingRecord) (int64, error) {
	if record == nil {
		return 0, ErrRepository
	}
	if record.Document != nil {
		if record.Document.Candidate == nil || record.Document.Candidate.Gst == nil {
			return 0, nil
		}
		if !validPurchaseMoney(record.Document.Candidate.Gst) {
			return 0, ErrRepository
		}
		return record.Document.Candidate.Gst.MinorUnits, nil
	}
	if record.Opening == nil || record.Opening.Input == nil {
		return 0, ErrRepository
	}
	input := record.Opening.Input
	if input.OutstandingGst == nil {
		return 0, nil
	}
	if input.OutstandingGst.CurrencyCode != "AUD" || input.PriorGstAttributed == nil || input.PriorGstAttributed.CurrencyCode != "AUD" {
		return 0, ErrRepository
	}
	abs := func(value int64) (int64, bool) {
		if value == math.MinInt64 {
			return 0, false
		}
		if value < 0 {
			return -value, true
		}
		return value, true
	}
	gst, ok := abs(input.OutstandingGst.MinorUnits)
	if !ok {
		return 0, ErrRepository
	}
	prior, ok := abs(input.PriorGstAttributed.MinorUnits)
	if !ok || prior > gst {
		return 0, ErrRepository
	}
	return gst - prior, nil
}

type basPurchaseManualPayment struct {
	journal *tammyv1.Journal
	hash    []byte
}

func basPurchaseApplyLegacy(
	ctx context.Context,
	index *basPurchaseMapIndex,
	record *LegacyPurchaseReportingRecord,
	value *LegacyCandidate,
	basis tammyv1.GstBasis,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
) error {
	allCash, err := basPurchaseLegacyManualCash(ctx, index, record)
	if err != nil {
		return err
	}
	var currentCash []*basPurchaseManualPayment
	for _, payment := range allCash {
		if claimTimingPeriodContains(period, payment.journal.PostingDate) && purchaseDateKey(payment.journal.PostingDate) <= purchaseDateKey(balance) {
			currentCash = append(currentCash, payment)
		}
	}
	knownPayment := len(currentCash) != 0
	value.Excluded, value.EvidenceOnly = false, false
	value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
	if knownPayment {
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
	}
	hasEvidenceLink := false
	for _, review := range value.Reviews {
		if review != nil && review.Review != nil && proto.Equal(review.Review.Source, value.Source) &&
			review.Review.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE {
			hasEvidenceLink = true
		}
	}
	var applied *tammyv1.LegacyPurchaseReviewInput
	var appliedReviews []*tammyv1.LegacyPurchaseReviewInput
	contradiction := false
	for _, review := range value.Reviews {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		if review == nil || review.Review == nil {
			return ErrRepository
		}
		if !LegacyReviewApplies(review, value.Source, period, balance) && !(hasEvidenceLink && proto.Equal(review.Review.Source, value.Source)) {
			continue
		}
		if err := basPurchaseVerifyLegacyDecisionFacts(ctx, index, record, review.Review, allCash); err != nil {
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
		if knownPayment && basis == tammyv1.GstBasis_GST_BASIS_CASH {
			if applied.PreviouslyClaimedPeriod == nil || !validClaimTimingPeriod(applied.PreviouslyClaimedPeriod) {
				return ErrRepository
			}
			for _, payment := range currentCash {
				if purchaseDateKey(payment.journal.PostingDate) > purchaseDateKey(applied.PreviouslyClaimedPeriod.PeriodEnd) {
					return nil
				}
				supported := false
				for _, review := range appliedReviews {
					for _, ref := range review.SupportingPayments {
						if ref.SourceId == payment.journal.Id && ref.SourceVersion == payment.journal.Version && bytes.Equal(ref.SourceHash, payment.hash) {
							supported = true
						}
					}
				}
				if !supported {
					return nil
				}
			}
		}
		value.Excluded = true
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE,
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE:
		value.Excluded = true
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT:
		value.Status = tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
	default:
		return ErrRepository
	}
	return nil
}

func basPurchaseLegacyManualCash(
	ctx context.Context,
	index *basPurchaseMapIndex,
	record *LegacyPurchaseReportingRecord,
) ([]*basPurchaseManualPayment, error) {
	if index == nil || record == nil || record.Source == nil {
		return nil, ErrRepository
	}
	_, _, _, _, _, err := legacySourceScope(record.Source)
	if err != nil {
		return nil, ErrRepository
	}
	roots := make(map[string]struct{})
	prior := make(map[string]struct{}, len(record.PriorJournalIDs))
	for _, id := range record.PriorJournalIDs {
		if id == "" {
			return nil, ErrRepository
		}
		if _, duplicate := prior[id]; duplicate {
			return nil, ErrRepository
		}
		prior[id] = struct{}{}
	}
	// CaptureCoverageForBAS has already reconciled the selected direct and
	// tax-fact associations into PriorJournalIDs. It may have dropped the
	// duplicate raw tax_facts projection after the canonical accounting proof
	// was established, so this mapper consumes the frozen source-owned IDs.
	for _, id := range record.PriorJournalIDs {
		proof := index.journals[id]
		if proof == nil || proof.SourceKind != 0 || proof.SourceType == "" || proof.SourceType == "OPENING" {
			return nil, ErrRepository
		}
		roots[id] = struct{}{}
	}
	for id := range roots {
		if index.journals[id] == nil {
			return nil, ErrRepository
		}
		if _, selected := prior[id]; !selected {
			return nil, ErrRepository
		}
	}
	paymentJournalIDs := make(map[string]struct{}, len(record.PaymentJournalIDs))
	for _, id := range record.PaymentJournalIDs {
		if id == "" || index.journals[id] == nil {
			return nil, ErrRepository
		}
		if _, duplicate := paymentJournalIDs[id]; duplicate {
			return nil, ErrRepository
		}
		paymentJournalIDs[id] = struct{}{}
		if _, isLegacyRoot := roots[id]; !isLegacyRoot {
			matchedModernPayment := false
			for _, owner := range index.payments {
				if owner.posting.Posting.DocumentId == legacyDocumentID(record.Source) && owner.payment.JournalId == id {
					matchedModernPayment = true
					break
				}
			}
			if !matchedModernPayment {
				return nil, ErrRepository
			}
		}
	}
	associated := make(map[string]struct{})
	journalIDs := make([]string, 0, len(index.journals))
	for id := range index.journals {
		journalIDs = append(journalIDs, id)
	}
	sort.Strings(journalIDs)
	children := make(map[string][]string)
	visit := make(map[string]uint8, len(journalIDs))
	for _, id := range journalIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		proof := index.journals[id]
		parentID := proof.Journal.GetReversalOfJournalId()
		if parentID != "" {
			parent := index.journals[parentID]
			if parent == nil || parent.Journal.GetReversedByJournalId() != id {
				return nil, ErrRepository
			}
			children[parentID] = append(children[parentID], id)
		}
		var path []string
		for current := id; current != ""; {
			if visit[current] == 1 {
				return nil, ErrRepository
			}
			if visit[current] == 2 {
				break
			}
			ancestor := index.journals[current]
			if ancestor == nil {
				return nil, ErrRepository
			}
			visit[current] = 1
			path = append(path, current)
			current = ancestor.Journal.GetReversalOfJournalId()
		}
		for _, value := range path {
			visit[value] = 2
		}
	}
	queue := make([]string, 0, len(roots))
	for id := range roots {
		associated[id] = struct{}{}
		queue = append(queue, id)
	}
	sort.Strings(queue)
	for head := 0; head < len(queue); head++ {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		for _, child := range children[queue[head]] {
			if _, already := associated[child]; already {
				continue
			}
			associated[child] = struct{}{}
			queue = append(queue, child)
		}
	}
	var result []*basPurchaseManualPayment
	for _, id := range journalIDs {
		if _, linked := associated[id]; !linked {
			continue
		}
		proof := index.journals[id]
		if proof.SourceKind != 0 || proof.SourceType == "OPENING" {
			continue
		}
		if proof.SourceType == "" {
			return nil, ErrRepository
		}
		switch proof.Journal.Source {
		case tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE,
			tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT,
			tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION:
			return nil, ErrRepository
		}
		if proof.State != "POSTED" {
			continue
		}
		if proof.Journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || !validPurchaseDate(proof.Journal.PostingDate) {
			return nil, ErrRepository
		}
		cashCredit := false
		for _, line := range proof.Journal.Lines {
			account, err := index.account(line.AccountId, proof.Journal.OrganisationId)
			if err != nil {
				return nil, err
			}
			if accounting.IsCashAccount(account) && line.Credit.MinorUnits > line.Debit.MinorUnits {
				cashCredit = true
				break
			}
		}
		if !cashCredit {
			continue
		}
		content, err := purchaseBytes(proof.Journal)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		result = append(result, &basPurchaseManualPayment{journal: proof.Journal, hash: bytes.Clone(digest[:])})
	}
	return result, ctx.Err()
}

func basPurchaseVerifyLegacyDecisionFacts(
	ctx context.Context,
	index *basPurchaseMapIndex,
	record *LegacyPurchaseReportingRecord,
	input *tammyv1.LegacyPurchaseReviewInput,
	manualCash []*basPurchaseManualPayment,
) error {
	if ctx == nil || index == nil || record == nil || record.Source == nil || input == nil {
		return ErrInvalidLegacyReview
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRepository, err)
	}
	if ValidateLegacyReview(input) != nil {
		return ErrInvalidLegacyReview
	}
	verifyEvidence := func(ref *tammyv1.PurchaseRetainedSourceRef) error {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		if ref == nil || !validLegacySupport(ref) {
			return ErrInvalidEvidence
		}
		document := index.documents[ref.SourceId]
		if document == nil {
			return ErrRepository
		}
		if document.OrganisationId != record.DocumentOwner() || document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
			document.Version != ref.SourceVersion || !bytes.Equal(document.Sha256, ref.SourceHash) {
			return ErrInvalidEvidence
		}
		return nil
	}
	for _, ref := range input.SupportingEvidence {
		if err := verifyEvidence(ref); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRepository, err)
	}
	if input.ClaimEvidence != nil {
		if err := verifyEvidence(input.ClaimEvidence); err != nil {
			return err
		}
	}
	var linked *tammyv1.PurchasePosting
	if input.LinkedPostingId != nil {
		linkedRecord, exists := index.postings[*input.LinkedPostingId]
		if !exists || linkedRecord.Posting == nil || linkedRecord.Accounting == nil || !linkedRecord.Accounting.Posted ||
			linkedRecord.Posting.Id != *input.LinkedPostingId {
			return ErrInvalidLegacyReview
		}
		if sourceDocument := legacyDocumentID(input.Source); sourceDocument != "" && linkedRecord.Posting.DocumentId == sourceDocument {
			return ErrInvalidLegacyReview
		}
		linked = linkedRecord.Posting
	}
	if input.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE {
		if record.Document == nil || record.Document.Candidate == nil {
			return ErrInvalidLegacyReview
		}
		matched := false
		supplier, invoice, _ := NormalizeDuplicateIdentity(record.Document.Candidate.SupplierName, record.Document.Candidate.InvoiceNumber)
		matches := 0
		for _, candidate := range index.postings {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err)
			}
			posting := candidate.Posting
			candidateSupplier, candidateInvoice, err := NormalizeDuplicateIdentity(posting.SupplierName, posting.InvoiceNumber)
			if err != nil {
				return ErrRepository
			}
			if posting.DocumentId == record.Document.Id || bytes.Equal(posting.OriginalContentHash, record.Document.Sha256) ||
				(supplier != "" && invoice != "" && candidateSupplier == supplier && candidateInvoice == invoice) {
				matches++
				if matches > 50 {
					return ErrSourceBound
				}
				if linked != nil && posting.DocumentId == linked.DocumentId {
					matched = true
				}
				for _, ref := range input.SupportingEvidence {
					if ref.SourceId == posting.DocumentId {
						matched = true
					}
				}
			}
		}
		if !matched {
			return ErrInvalidLegacyReview
		}
	}
	for _, ref := range input.SupportingPayments {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		date, err := basPurchaseVerifyLegacyPayment(ref, input, index, manualCash)
		if err != nil {
			return err
		}
		if (input.Disposition != tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE &&
			purchaseDateKey(date) > purchaseDateKey(input.ReportingBalanceDate)) ||
			(input.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE &&
				purchaseDateKey(date) <= purchaseDateKey(input.ReportingBalanceDate)) {
			return ErrInvalidLegacyReview
		}
	}
	if input.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE {
		for _, payment := range manualCash {
			if purchaseDateKey(payment.journal.PostingDate) <= purchaseDateKey(input.ReportingBalanceDate) {
				return ErrInvalidLegacyReview
			}
		}
	}
	return ctx.Err()
}

func (record *LegacyPurchaseReportingRecord) DocumentOwner() string {
	if record == nil {
		return ""
	}
	if record.Document != nil {
		return record.Document.OrganisationId
	}
	if record.Opening != nil {
		return record.Opening.OrganisationID
	}
	return ""
}

func legacyDocumentID(source *tammyv1.LegacyPurchaseSourceRef) string {
	if source == nil || source.GetDocument() == nil {
		return ""
	}
	return source.GetDocument().DocumentId
}

func basPurchaseVerifyLegacyPayment(
	ref *tammyv1.PurchaseRetainedSourceRef,
	input *tammyv1.LegacyPurchaseReviewInput,
	index *basPurchaseMapIndex,
	manualCash []*basPurchaseManualPayment,
) (*tammyv1.CivilDate, error) {
	if !validLegacySupport(ref) {
		return nil, ErrInvalidLegacyReview
	}
	if owner, exists := index.payments[ref.SourceId]; exists {
		if input.LinkedPostingId == nil || owner.posting.Posting.Id != *input.LinkedPostingId ||
			owner.payment.AccountingVersion != ref.SourceVersion || !bytes.Equal(owner.payment.SourceHash, ref.SourceHash) {
			return nil, ErrInvalidLegacyReview
		}
		return owner.payment.ActualPaymentDate, nil
	}
	for _, payment := range manualCash {
		if payment.journal.Id == ref.SourceId && payment.journal.Version == ref.SourceVersion && bytes.Equal(payment.hash, ref.SourceHash) {
			return payment.journal.PostingDate, nil
		}
	}
	return nil, ErrInvalidLegacyReview
}
