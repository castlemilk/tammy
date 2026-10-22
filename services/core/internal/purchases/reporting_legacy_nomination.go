package purchases

import (
	"bytes"
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const maximumBASLegacyCandidates = 1000

type BASLegacyCandidateKind uint8

const (
	BASLegacyCandidateDocument BASLegacyCandidateKind = iota + 1
	BASLegacyCandidateOpeningPayable
)

type BASLegacyCandidateKey struct {
	Kind                     BASLegacyCandidateKind
	OrganisationID, SourceID string
	ConversionID             string
}

type BASLegacyNomination struct {
	Key                     BASLegacyCandidateKey
	SourceVersion           uint64
	DocumentRawSHA256       []byte
	ProvisionalEvidenceOnly bool
	ProvisionalDecisionIDs  []string
}

type BASLegacyCandidateDiscovery struct {
	Nominations []BASLegacyNomination
	Candidates  []BASLegacyCandidateKey
}

type basLegacyDecisionTuple struct {
	organisationID string
	documentID     string
	version        uint64
	rawHash        string
}

type basLegacySupportTuple struct {
	sourceID string
	version  uint64
	hash     string
}

// Physical association identity follows accounting's selected key. Retained
// provenance validates the row but cannot distinguish duplicate physical links.
type basLegacyCashAssociationKey struct {
	sourceID, journalID, kind, taxFactID string
}

// The cash selector has already fixed each source's direct/tax-fact roots and
// reciprocal reversal closure. Keep only the bounded scalar facts needed to
// validate a retained manual-cash payment reference before body hydration.
// Its Journal proto hash is intentionally checked by the later full-proof
// reconciliation, not guessed from these scalar cells.
type basLegacyCashJournalSupport struct {
	version     uint64
	sourceKind  int64
	sourceType  string
	state       string
	journalDate string
	cashCredit  bool
}

type basLegacyCashSourceFacts struct {
	currentCashCredit bool
	journals          map[string]basLegacyCashJournalSupport
}

// nominateBASLegacyCandidates freezes the provisional semantic legacy set from
// already-reserved owner scalars. It performs no I/O, body hydration, amount
// inference, or GST calculation. A suppressed nomination remains in Nominations
// so the full-reader stage can reconcile its frozen choice without widening it.
func nominateBASLegacyCandidates(
	organisationID string,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
	documentStates []BASDocumentState,
	purchaseKeys []BASScalarKey,
	decisions []*tammyv1.LegacyPurchaseReview,
	cash accounting.BASLegacyCashRead,
	maximumCandidates int64,
) (*BASLegacyCandidateDiscovery, error) {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: invalid BAS legacy nomination: %s", ErrRepository, reason)
	}
	if !ids.IsCanonicalV7(organisationID) || !validClaimTimingPeriod(period) || !validPurchaseDate(balance) || maximumCandidates < 0 {
		return nil, invalid("invalid owner, period, balance, or candidate limit")
	}
	if maximumCandidates > maximumBASLegacyCandidates {
		maximumCandidates = maximumBASLegacyCandidates
	}

	// Build exact maps for the owner selectors' rows. These checks are limited
	// to identities the nomination and provisional-support rules consume; the
	// selectors remain responsible for validating every other physical family.
	docKeys := make(map[string]BASScalarKey)
	postingKeys := make(map[string]BASScalarKey)
	paymentKeys := make(map[string]BASScalarKey)
	openingPayables := make(map[string]BASScalarKey)
	openingConversions := make(map[string]BASScalarKey)
	openingItems := make(map[string]BASScalarKey)
	legacyReviewKeys := make(map[string]BASScalarKey)
	for _, key := range purchaseKeys {
		if key.OrganisationID != organisationID {
			return nil, invalid("purchase scalar key belongs to another owner")
		}
		switch key.Kind {
		case BASScalarDocument:
			if !ids.IsCanonicalV7(key.ID) || key.ObjectVersion == nil || !basLegacyPositiveVersion(*key.ObjectVersion) || len(key.SourceHash) != 32 {
				return nil, invalid("malformed document owner key")
			}
			if _, exists := docKeys[key.ID]; exists {
				return nil, invalid("duplicate document owner key")
			}
			docKeys[key.ID] = key
		case BASScalarPosting:
			if !ids.IsCanonicalV7(key.ID) || key.DocumentID == nil || !ids.IsCanonicalV7(*key.DocumentID) ||
				key.SourceID == nil || *key.SourceID != *key.DocumentID || key.SourceVersion == nil || !basLegacyPositiveVersion(*key.SourceVersion) ||
				len(key.SourceHash) != 32 || len(key.RecordHash) != 32 || len(key.ReviewedSnapshotHash) != 32 {
				return nil, invalid("malformed posting owner key")
			}
			if _, exists := postingKeys[key.ID]; exists {
				return nil, invalid("duplicate posting owner key")
			}
			postingKeys[key.ID] = key
		case BASScalarPayment:
			if !ids.IsCanonicalV7(key.ID) || key.PostingID == nil || !ids.IsCanonicalV7(*key.PostingID) ||
				key.AccountingVersion == nil || !basLegacyPositiveVersion(*key.AccountingVersion) || len(key.RecordHash) != 32 ||
				key.ActualPaymentDate == nil {
				return nil, invalid("malformed payment owner key")
			}
			if _, valid := basLegacyCashDateKey(*key.ActualPaymentDate); !valid {
				return nil, invalid("malformed actual payment date")
			}
			if _, exists := paymentKeys[key.ID]; exists {
				return nil, invalid("duplicate payment owner key")
			}
			paymentKeys[key.ID] = key
		case BASScalarOpeningPayable:
			if !ids.IsCanonicalV7(key.ID) || key.ConversionID == nil || !ids.IsCanonicalV7(*key.ConversionID) {
				return nil, invalid("malformed opening-payable owner key")
			}
			if _, exists := openingPayables[key.ID]; exists {
				return nil, invalid("duplicate opening-payable owner key")
			}
			openingPayables[key.ID] = key
		case BASScalarOpeningConversion:
			if !ids.IsCanonicalV7(key.ID) || key.ObjectVersion == nil || !basLegacyPositiveVersion(*key.ObjectVersion) || !basLegacyHexSourceHash(key.SourceHash) {
				return nil, invalid("malformed opening-conversion owner key")
			}
			if _, exists := openingConversions[key.ID]; exists {
				return nil, invalid("duplicate opening-conversion owner key")
			}
			openingConversions[key.ID] = key
		case BASScalarOpeningItem:
			if !ids.IsCanonicalV7(key.ID) || key.ConversionID == nil || !ids.IsCanonicalV7(*key.ConversionID) ||
				key.AccountID == nil || !ids.IsCanonicalV7(*key.AccountID) {
				return nil, invalid("malformed opening-item owner key")
			}
			if _, exists := openingItems[key.ID]; exists {
				return nil, invalid("duplicate opening-item owner key")
			}
			openingItems[key.ID] = key
		case BASScalarLegacyReview:
			if !ids.IsCanonicalV7(key.ID) || key.SourceKind == nil || key.SourceID == nil || !ids.IsCanonicalV7(*key.SourceID) ||
				key.SourceVersion == nil || !basLegacyPositiveVersion(*key.SourceVersion) || key.AccountingVersion == nil ||
				!basLegacyPositiveVersion(*key.AccountingVersion) || len(key.SourceHash) != 32 || len(key.RecordHash) != 32 {
				return nil, invalid("malformed legacy-decision owner key")
			}
			if _, exists := legacyReviewKeys[key.ID]; exists {
				return nil, invalid("duplicate legacy-decision owner key")
			}
			legacyReviewKeys[key.ID] = key
		}
	}

	states := make(map[string]BASDocumentState, len(documentStates))
	for _, state := range documentStates {
		if state.OrganisationID != organisationID || !ids.IsCanonicalV7(state.ID) || !basLegacyPositiveVersion(state.Version) ||
			(state.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW && state.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED) || len(state.ContentHash) != 32 {
			return nil, invalid("malformed document state")
		}
		if state.PostingID != nil && !ids.IsCanonicalV7(*state.PostingID) {
			return nil, invalid("malformed document posting link")
		}
		if _, exists := states[state.ID]; exists {
			return nil, invalid("duplicate document state")
		}
		key, exists := docKeys[state.ID]
		if !exists || *key.ObjectVersion != state.Version || !bytes.Equal(key.SourceHash, state.ContentHash) || key.IsRoot != (state.PostingID == nil) {
			return nil, invalid("document state differs from its physical owner key")
		}
		states[state.ID] = state
	}
	if len(states) != len(docKeys) {
		return nil, invalid("document state and physical-key sets differ")
	}
	for _, state := range states {
		if state.PostingID == nil {
			continue
		}
		posting, exists := postingKeys[*state.PostingID]
		if !exists || posting.OrganisationID != organisationID || *posting.DocumentID != state.ID {
			return nil, invalid("promoted document posting is absent from owner keys")
		}
	}

	// Every payable remains represented by its exact conversion/item pair.
	// Only IsRoot rows become semantic units; DRAFT rows stay physical context.
	for payableID, payable := range openingPayables {
		conversionID := *payable.ConversionID
		conversion, exists := openingConversions[conversionID]
		item, itemExists := openingItems[payableID]
		if !exists || !itemExists || *item.ConversionID != conversionID {
			return nil, invalid("opening payable lacks its exact conversion/item context")
		}
		if payable.IsRoot {
			if conversion.JournalID == nil || !ids.IsCanonicalV7(*conversion.JournalID) || conversion.JournalSourceKind == nil || *conversion.JournalSourceKind != 0 ||
				conversion.SourceType == nil || *conversion.SourceType != "OPENING" || conversion.SourceID == nil || *conversion.SourceID != conversionID ||
				conversion.SourceVersion == nil || *conversion.SourceVersion != 1 {
				return nil, invalid("opening root lacks its posted conversion journal identity")
			}
		}
	}
	for itemID, item := range openingItems {
		payable, exists := openingPayables[itemID]
		if !exists || payable.ConversionID == nil || *payable.ConversionID != *item.ConversionID {
			return nil, invalid("opening item has no exact payable owner")
		}
	}

	// Match every full decision body to its reserved scalar key. Stale decisions
	// are retained and validated, but only current document tuples feed the
	// provisional group below. Opening source hashes await retained-input read.
	decisionByID := make(map[string]*tammyv1.LegacyPurchaseReview, len(decisions))
	decisionTuples := make(map[basLegacyDecisionTuple][]*tammyv1.LegacyPurchaseReview)
	for _, decision := range decisions {
		if decision == nil || !ids.IsCanonicalV7(decision.Id) || decision.OrganisationId != organisationID ||
			!basLegacyPositiveVersion(decision.SourceRevision) || decision.Review == nil || ValidateLegacyReview(decision.Review) != nil ||
			!verifyPurchaseHash(decision, decision.SourceHash) {
			return nil, invalid("malformed or unverified legacy-decision body")
		}
		if _, exists := decisionByID[decision.Id]; exists {
			return nil, invalid("duplicate legacy-decision body")
		}
		kind, sourceID, version, rawHash, reviewedHash, err := legacySourceScope(decision.Review.Source)
		if err != nil {
			return nil, invalid("invalid legacy-decision source tuple")
		}
		key, exists := legacyReviewKeys[decision.Id]
		if !exists || !basLegacyDecisionKeyMatches(key, decision, kind, sourceID, version, rawHash, reviewedHash) {
			return nil, invalid("legacy-decision body differs from its reserved scalar key")
		}
		decisionByID[decision.Id] = decision
		if kind == "DOCUMENT" {
			tuple := basLegacyDecisionTuple{organisationID: organisationID, documentID: sourceID, version: version, rawHash: string(rawHash)}
			decisionTuples[tuple] = append(decisionTuples[tuple], decision)
		}
	}
	if len(decisionByID) != len(legacyReviewKeys) {
		return nil, invalid("legacy-decision body and physical-key sets differ")
	}

	cashBySource, err := basLegacyCashSourcesWithCurrentCredit(organisationID, period, balance, cash, states, openingPayables)
	if err != nil {
		return nil, err
	}

	discovery := &BASLegacyCandidateDiscovery{
		Nominations: make([]BASLegacyNomination, 0),
		Candidates:  make([]BASLegacyCandidateKey, 0),
	}
	for id, state := range states {
		if state.PostingID != nil || (state.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW &&
			state.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED) {
			continue
		}
		nomination := BASLegacyNomination{
			Key:           BASLegacyCandidateKey{Kind: BASLegacyCandidateDocument, OrganisationID: organisationID, SourceID: id},
			SourceVersion: state.Version, DocumentRawSHA256: bytes.Clone(state.ContentHash),
		}
		if state.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED {
			tuple := basLegacyDecisionTuple{organisationID: organisationID, documentID: id, version: state.Version, rawHash: string(state.ContentHash)}
			group := decisionTuples[tuple]
			nomination.ProvisionalDecisionIDs = basLegacySortedDecisionIDs(group)
			nomination.ProvisionalEvidenceOnly = basLegacyEvidenceOnlyGroupIsCurrent(
				organisationID, id, group, postingKeys, paymentKeys, states, docKeys, cashBySource,
			)
		}
		discovery.Nominations = append(discovery.Nominations, nomination)
	}
	for payableID, payable := range openingPayables {
		if !payable.IsRoot {
			continue
		}
		conversionID := *payable.ConversionID
		conversion := openingConversions[conversionID]
		discovery.Nominations = append(discovery.Nominations, BASLegacyNomination{
			Key:           BASLegacyCandidateKey{Kind: BASLegacyCandidateOpeningPayable, OrganisationID: organisationID, SourceID: payableID, ConversionID: conversionID},
			SourceVersion: *conversion.ObjectVersion,
		})
	}

	sortBASLegacyCandidateKeys(discovery.Nominations)
	for _, nomination := range discovery.Nominations {
		if !nomination.ProvisionalEvidenceOnly {
			discovery.Candidates = append(discovery.Candidates, nomination.Key)
		}
	}
	slices.SortFunc(discovery.Candidates, compareBASLegacyCandidateKey)
	if int64(len(discovery.Candidates)) > maximumCandidates {
		return nil, fmt.Errorf("%w: BAS legacy candidates exceed limit %d", ErrRepository, maximumCandidates)
	}
	return discovery, nil
}

func basLegacyPositiveVersion(value uint64) bool { return value > 0 && value <= math.MaxInt64 }

func basLegacyHexSourceHash(value []byte) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func basLegacyDecisionKeyMatches(
	key BASScalarKey,
	decision *tammyv1.LegacyPurchaseReview,
	kind, sourceID string,
	version uint64,
	rawHash, reviewedHash []byte,
) bool {
	keyText := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}
	if key.OrganisationID != decision.OrganisationId || key.SourceKind == nil || *key.SourceKind != kind ||
		key.SourceID == nil || *key.SourceID != sourceID || key.SourceVersion == nil || *key.SourceVersion != version ||
		key.AccountingVersion == nil || *key.AccountingVersion != decision.SourceRevision ||
		!bytes.Equal(key.SourceHash, rawHash) || !bytes.Equal(key.ReviewedSnapshotHash, reviewedHash) ||
		!bytes.Equal(key.RecordHash, decision.SourceHash) {
		return false
	}
	if kind == "DOCUMENT" {
		if keyText(key.DocumentID) != sourceID || key.OpeningPayableID != nil {
			return false
		}
	} else if keyText(key.OpeningPayableID) != sourceID || key.DocumentID != nil {
		return false
	}
	linkedPosting := ""
	if decision.Review.LinkedPostingId != nil {
		linkedPosting = *decision.Review.LinkedPostingId
	}
	if keyText(key.PostingID) != linkedPosting {
		return false
	}
	claimID := ""
	var claimVersion *uint64
	var claimHash []byte
	if decision.Review.ClaimEvidence != nil {
		claimID = decision.Review.ClaimEvidence.SourceId
		claimVersion = &decision.Review.ClaimEvidence.SourceVersion
		claimHash = decision.Review.ClaimEvidence.SourceHash
	}
	if keyText(key.ClaimEvidenceSourceID) != claimID || !basLegacyOptionalUint64Equal(key.ClaimEvidenceSourceVersion, claimVersion) ||
		!bytes.Equal(key.ClaimEvidenceSourceHash, claimHash) {
		return false
	}
	return true
}

func basLegacyOptionalUint64Equal(left, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func basLegacySortedDecisionIDs(group []*tammyv1.LegacyPurchaseReview) []string {
	if len(group) == 0 {
		return nil
	}
	ids := make([]string, 0, len(group))
	for _, decision := range group {
		ids = append(ids, decision.Id)
	}
	slices.Sort(ids)
	return ids
}

func basLegacyEvidenceOnlyGroupIsCurrent(
	organisationID, candidateDocumentID string,
	group []*tammyv1.LegacyPurchaseReview,
	postings, payments map[string]BASScalarKey,
	states map[string]BASDocumentState,
	documentKeys map[string]BASScalarKey,
	cashBySource map[string]basLegacyCashSourceFacts,
) bool {
	if len(group) == 0 || cashBySource[candidateDocumentID].currentCashCredit {
		return false
	}
	first := group[0]
	if first.Review.Disposition != tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE ||
		first.Review.LinkedPostingId == nil || !ids.IsCanonicalV7(*first.Review.LinkedPostingId) ||
		!basLegacyCurrentPosting(organisationID, *first.Review.LinkedPostingId, candidateDocumentID, postings, states) ||
		!basLegacySupportRefsCurrent(organisationID, candidateDocumentID, first.Review, *first.Review.LinkedPostingId,
			states, documentKeys, payments, cashBySource) {
		return false
	}
	for _, decision := range group {
		review := decision.Review
		if review.Disposition != first.Review.Disposition || review.LinkedPostingId == nil ||
			*review.LinkedPostingId != *first.Review.LinkedPostingId || !proto.Equal(review.PreviouslyClaimedPeriod, first.Review.PreviouslyClaimedPeriod) ||
			!basLegacySameOptionalSupport(review.ClaimEvidence, first.Review.ClaimEvidence) {
			return false
		}
		if _, evidenceOK := basLegacySupportTuples(review.SupportingEvidence); !evidenceOK {
			return false
		}
		if _, paymentsOK := basLegacySupportTuples(review.SupportingPayments); !paymentsOK ||
			!basLegacySupportRefsCurrent(organisationID, candidateDocumentID, review, *review.LinkedPostingId,
				states, documentKeys, payments, cashBySource) {
			return false
		}
	}
	return true
}

func basLegacyCurrentPosting(
	organisationID, postingID, sourceDocumentID string,
	postings map[string]BASScalarKey,
	states map[string]BASDocumentState,
) bool {
	posting, exists := postings[postingID]
	if !exists || posting.OrganisationID != organisationID || !posting.IsRoot || posting.DocumentID == nil || *posting.DocumentID == sourceDocumentID {
		return false
	}
	supportState, exists := states[*posting.DocumentID]
	return exists && supportState.OrganisationID == organisationID && supportState.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED &&
		supportState.PostingID != nil && *supportState.PostingID == postingID &&
		posting.SourceID != nil && *posting.SourceID == supportState.ID && posting.SourceVersion != nil &&
		*posting.SourceVersion == supportState.Version && bytes.Equal(posting.SourceHash, supportState.ContentHash)
}

func basLegacySupportRefsCurrent(
	organisationID string,
	candidateDocumentID string,
	review *tammyv1.LegacyPurchaseReviewInput,
	linkedPostingID string,
	states map[string]BASDocumentState,
	documentKeys map[string]BASScalarKey,
	payments map[string]BASScalarKey,
	cashBySource map[string]basLegacyCashSourceFacts,
) bool {
	for _, ref := range review.SupportingEvidence {
		if ref == nil || !ids.IsCanonicalV7(ref.SourceId) || !basLegacyPositiveVersion(ref.SourceVersion) || len(ref.SourceHash) != 32 {
			return false
		}
		state, stateExists := states[ref.SourceId]
		key, keyExists := documentKeys[ref.SourceId]
		if !stateExists || !keyExists || state.OrganisationID != organisationID || state.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
			state.Version != ref.SourceVersion || !bytes.Equal(state.ContentHash, ref.SourceHash) ||
			*key.ObjectVersion != state.Version || !bytes.Equal(key.SourceHash, state.ContentHash) {
			return false
		}
	}
	for _, ref := range review.SupportingPayments {
		if ref == nil || !ids.IsCanonicalV7(ref.SourceId) || !basLegacyPositiveVersion(ref.SourceVersion) || len(ref.SourceHash) != 32 {
			return false
		}
		if key, exists := payments[ref.SourceId]; exists {
			if key.OrganisationID != organisationID || key.AccountingVersion == nil || *key.AccountingVersion != ref.SourceVersion ||
				!bytes.Equal(key.RecordHash, ref.SourceHash) || key.PostingID == nil || *key.PostingID != linkedPostingID ||
				key.ActualPaymentDate == nil {
				return false
			}
			paymentDate, paymentDateValid := basLegacyCashDateKey(*key.ActualPaymentDate)
			balanceDate, balanceDateValid := basLegacyCashCivilDateKey(review.ReportingBalanceDate)
			if !paymentDateValid || !balanceDateValid || paymentDate > balanceDate {
				return false
			}
			continue
		}
		// Manual legacy payment support is selected only through this candidate's
		// already bounded source association and reciprocal closure, never by a
		// global journal/ref scan. verifyLegacyPayment checks its body hash later.
		source := cashBySource[candidateDocumentID]
		journal, exists := source.journals[ref.SourceId]
		if !exists || journal.version != ref.SourceVersion || journal.sourceKind != 0 || journal.sourceType == "" ||
			journal.sourceType == "OPENING" ||
			journal.state != "POSTED" || !journal.cashCredit {
			return false
		}
		journalDate, dateValid := basLegacyCashDateKey(journal.journalDate)
		balanceDate, balanceValid := basLegacyCashCivilDateKey(review.ReportingBalanceDate)
		if !dateValid || !balanceValid || journalDate > balanceDate {
			return false
		}
	}
	return review.ClaimEvidence == nil
}

func basLegacySupportTuples(values []*tammyv1.PurchaseRetainedSourceRef) ([]basLegacySupportTuple, bool) {
	out := make([]basLegacySupportTuple, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == nil || !ids.IsCanonicalV7(value.SourceId) || !basLegacyPositiveVersion(value.SourceVersion) || len(value.SourceHash) != 32 {
			return nil, false
		}
		if _, exists := seen[value.SourceId]; exists {
			return nil, false
		}
		seen[value.SourceId] = struct{}{}
		out = append(out, basLegacySupportTuple{sourceID: value.SourceId, version: value.SourceVersion, hash: string(value.SourceHash)})
	}
	slices.SortFunc(out, func(left, right basLegacySupportTuple) int {
		if order := cmp.Compare(left.sourceID, right.sourceID); order != 0 {
			return order
		}
		if order := cmp.Compare(left.version, right.version); order != 0 {
			return order
		}
		return cmp.Compare(left.hash, right.hash)
	})
	return out, true
}

func basLegacySameOptionalSupport(left, right *tammyv1.PurchaseRetainedSourceRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.SourceId == right.SourceId && left.SourceVersion == right.SourceVersion && bytes.Equal(left.SourceHash, right.SourceHash)
}

func sortBASLegacyCandidateKeys(values []BASLegacyNomination) {
	slices.SortFunc(values, func(left, right BASLegacyNomination) int {
		return compareBASLegacyCandidateKey(left.Key, right.Key)
	})
}

func compareBASLegacyCandidateKey(left, right BASLegacyCandidateKey) int {
	if order := cmp.Compare(left.Kind, right.Kind); order != 0 {
		return order
	}
	if order := cmp.Compare(left.OrganisationID, right.OrganisationID); order != 0 {
		return order
	}
	if order := cmp.Compare(left.SourceID, right.SourceID); order != 0 {
		return order
	}
	return cmp.Compare(left.ConversionID, right.ConversionID)
}

func basLegacyCashSourcesWithCurrentCredit(
	organisationID string,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
	read accounting.BASLegacyCashRead,
	states map[string]BASDocumentState,
	openingPayables map[string]BASScalarKey,
) (map[string]basLegacyCashSourceFacts, error) {
	invalid := func(reason string) (map[string]basLegacyCashSourceFacts, error) {
		return nil, fmt.Errorf("%w: invalid BAS legacy cash projection: %s", ErrRepository, reason)
	}
	summary := read.Summary
	if summary.OrganisationID != organisationID || summary.AssociationCount < 0 || summary.JournalCount < 0 || summary.LineCount < 0 ||
		summary.RowCount < 0 || summary.Bytes < 0 {
		return invalid("invalid owner or negative summary")
	}
	rowCount := int64(len(read.Associations)) + int64(len(read.Journals)) + int64(len(read.Lines))
	if rowCount > 16384 || summary.AssociationCount != int64(len(read.Associations)) || summary.JournalCount != int64(len(read.Journals)) ||
		summary.LineCount != int64(len(read.Lines)) || summary.RowCount != rowCount {
		return invalid("summary counts differ from selected rows or exceed the bound")
	}
	charged := rowCount * 512
	addText := func(value string) bool {
		if int64(len(value)) > math.MaxInt64-charged {
			return false
		}
		charged += int64(len(value))
		return true
	}
	addOptionalText := func(value *string) bool {
		return value == nil || addText(*value)
	}
	seenAssociations := make(map[basLegacyCashAssociationKey]struct{}, len(read.Associations))
	allowedSources := make(map[string]struct{}, len(states)+len(openingPayables))
	for id, state := range states {
		if state.PostingID == nil {
			allowedSources[id] = struct{}{}
		}
	}
	for id, payable := range openingPayables {
		if payable.IsRoot {
			allowedSources[id] = struct{}{}
		}
	}
	journalByID := make(map[string]accounting.BASLegacyCashJournal, len(read.Journals))
	for _, value := range read.Associations {
		if !ids.IsCanonicalV7(value.SourceID) || value.JournalID == "" || value.Kind != "DIRECT" && value.Kind != "TAX_FACT" {
			return invalid("malformed association identity")
		}
		if _, allowed := allowedSources[value.SourceID]; !allowed {
			return invalid("association references an unreserved source root")
		}
		key := basLegacyCashAssociationKey{sourceID: value.SourceID, journalID: value.JournalID, kind: value.Kind}
		if value.Kind == "DIRECT" {
			if value.TaxFactID != nil || value.JournalLineID != nil || value.FactSourceType != nil || value.FactSourceRevision != nil ||
				value.FactSourceContentHashPresent || value.FactSourceContentHash != nil {
				return invalid("direct association carries tax-fact cells")
			}
		} else {
			if value.TaxFactID == nil || *value.TaxFactID == "" || value.JournalLineID == nil || *value.JournalLineID == "" ||
				value.FactSourceType == nil || *value.FactSourceType == "" || value.FactSourceRevision == nil || !basLegacyPositiveVersion(*value.FactSourceRevision) ||
				(value.FactSourceContentHashPresent && len(value.FactSourceContentHash) != 32) ||
				(!value.FactSourceContentHashPresent && value.FactSourceContentHash != nil) {
				return invalid("malformed tax-fact association cells")
			}
			key.taxFactID = *value.TaxFactID
		}
		if _, exists := seenAssociations[key]; exists {
			return invalid("duplicate association row")
		}
		seenAssociations[key] = struct{}{}
		if !addText(value.SourceID) || !addText(value.JournalID) || !addText(value.Kind) || !addOptionalText(value.TaxFactID) ||
			!addOptionalText(value.JournalLineID) || !addOptionalText(value.FactSourceType) {
			return invalid("association charge overflow")
		}
		if value.FactSourceContentHashPresent && !addText(string(value.FactSourceContentHash)) {
			return invalid("tax-fact hash charge overflow")
		}
	}
	for _, value := range read.Journals {
		dateKey, dateValid := basLegacyCashDateKey(value.JournalDate)
		if value.OrganisationID != organisationID || !ids.IsCanonicalV7(value.OrganisationID) || value.ID == "" ||
			!basLegacyPositiveVersion(value.Version) || !basLegacyPositiveVersion(value.SourceRevision) || value.SourceKind != 0 ||
			value.SourceType == "" || value.SourceType == "OPENING" || value.SourceID == "" || !dateValid ||
			(value.State != "DRAFT" && value.State != "POSTED" && value.State != "REVERSED") {
			return invalid("malformed journal header")
		}
		_ = dateKey // Parsing here validates the retained date before period matching.
		if value.ReversalOfID != nil && (*value.ReversalOfID == "" || *value.ReversalOfID == value.ID) {
			return invalid("malformed reversal parent identity")
		}
		if value.ReversedByID != nil && (*value.ReversedByID == "" || *value.ReversedByID == value.ID) {
			return invalid("malformed reversal child identity")
		}
		if _, exists := journalByID[value.ID]; exists {
			return invalid("duplicate journal header")
		}
		journalByID[value.ID] = value
		if !addText(value.OrganisationID) || !addText(value.ID) || !addText(value.SourceType) || !addText(value.SourceID) ||
			!addText(value.State) || !addText(value.JournalDate) || !addOptionalText(value.ReversalOfID) || !addOptionalText(value.ReversedByID) {
			return invalid("journal charge overflow")
		}
	}
	linesByJournal := make(map[string][]accounting.BASLegacyCashLine, len(journalByID))
	lineIDs := make(map[string]struct{}, len(read.Lines))
	sequences := make(map[string]struct{}, len(read.Lines))
	for _, value := range read.Lines {
		if value.JournalID == "" || value.ID == "" || value.AccountID == "" ||
			value.AccountOrganisationID != organisationID || !ids.IsCanonicalV7(value.AccountOrganisationID) ||
			value.Sequence == 0 || value.Sequence > math.MaxUint32 || !basLegacyPositiveVersion(value.AccountVersion) || value.DebitMinor < 0 || value.CreditMinor < 0 ||
			value.ReportClassification == "" {
			return invalid("malformed journal line/account projection")
		}
		if _, exists := journalByID[value.JournalID]; !exists {
			return invalid("journal line has no selected header")
		}
		if _, exists := lineIDs[value.ID]; exists {
			return invalid("duplicate journal line identity")
		}
		lineIDs[value.ID] = struct{}{}
		sequenceKey := fmt.Sprintf("%s/%d", value.JournalID, value.Sequence)
		if _, exists := sequences[sequenceKey]; exists {
			return invalid("duplicate line sequence")
		}
		sequences[sequenceKey] = struct{}{}
		linesByJournal[value.JournalID] = append(linesByJournal[value.JournalID], value)
		if !addText(value.JournalID) || !addText(value.ID) || !addText(value.AccountID) || !addText(value.AccountOrganisationID) || !addText(value.ReportClassification) {
			return invalid("journal-line charge overflow")
		}
	}
	if charged != summary.Bytes || charged > 8<<20 {
		return invalid("summary byte charge differs from selected scalar cells")
	}
	for _, association := range read.Associations {
		journal, exists := journalByID[association.JournalID]
		if !exists || journal.SourceKind != 0 || journal.SourceType == "OPENING" {
			return invalid("association root header is absent or ineligible")
		}
		if association.Kind == "DIRECT" && journal.SourceID != association.SourceID {
			return invalid("direct association differs from journal source identity")
		}
		if association.Kind == "TAX_FACT" {
			foundLine := false
			for _, line := range linesByJournal[association.JournalID] {
				if line.ID == *association.JournalLineID {
					foundLine = true
					break
				}
			}
			if !foundLine {
				return invalid("tax-fact association line is absent from its selected journal")
			}
		}
	}
	// Check the entire selected closure, not only rows that happen to qualify
	// as current cash. It must consist exactly of association roots and their
	// writer-valid reciprocal reversal partners.
	for _, journal := range journalByID {
		if (journal.State == "REVERSED") != (journal.ReversedByID != nil) ||
			(journal.SourceType == "REVERSAL") != (journal.ReversalOfID != nil) ||
			(journal.State == "DRAFT" && (journal.ReversalOfID != nil || journal.ReversedByID != nil)) {
			return invalid("journal reversal state and links disagree")
		}
		if journal.ReversalOfID != nil {
			parent, exists := journalByID[*journal.ReversalOfID]
			if !exists || parent.OrganisationID != journal.OrganisationID || parent.ReversedByID == nil || *parent.ReversedByID != journal.ID ||
				parent.State != "REVERSED" || parent.Version < 2 ||
				(journal.State != "POSTED" && journal.State != "REVERSED") || journal.SourceKind != 0 ||
				journal.SourceID != parent.ID || journal.SourceRevision+1 != parent.Version {
				return invalid("reversal does not match writer source/version reciprocity")
			}
		}
		if journal.ReversedByID != nil {
			child, exists := journalByID[*journal.ReversedByID]
			if !exists || child.ReversalOfID == nil || *child.ReversalOfID != journal.ID {
				return invalid("reversed journal lacks its reciprocal child")
			}
		}
	}
	for _, association := range read.Associations {
		if _, exists := journalByID[association.JournalID]; !exists {
			return invalid("association journal is missing")
		}
	}
	reachable := make(map[string]struct{}, len(journalByID))
	queue := make([]string, 0, len(journalByID))
	for _, association := range read.Associations {
		if _, exists := reachable[association.JournalID]; !exists {
			reachable[association.JournalID] = struct{}{}
			queue = append(queue, association.JournalID)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		journal := journalByID[id]
		for _, linked := range []*string{journal.ReversalOfID, journal.ReversedByID} {
			if linked == nil {
				continue
			}
			if _, exists := reachable[*linked]; !exists {
				reachable[*linked] = struct{}{}
				queue = append(queue, *linked)
			}
		}
	}
	if len(reachable) != len(journalByID) {
		return invalid("selected journal header is outside every source reversal closure")
	}

	currentCashBySource := make(map[string]basLegacyCashSourceFacts)
	closuresBySource := make(map[string]map[string]struct{})
	for _, association := range read.Associations {
		root := journalByID[association.JournalID]
		closure := map[string]struct{}{root.ID: {}}
		pending := []string{root.ID}
		for len(pending) > 0 {
			id := pending[0]
			pending = pending[1:]
			journal := journalByID[id]
			for _, linked := range []*string{journal.ReversalOfID, journal.ReversedByID} {
				if linked == nil {
					continue
				}
				if _, exists := closure[*linked]; !exists {
					closure[*linked] = struct{}{}
					pending = append(pending, *linked)
				}
			}
		}
		sourceClosure := closuresBySource[association.SourceID]
		if sourceClosure == nil {
			sourceClosure = make(map[string]struct{})
			closuresBySource[association.SourceID] = sourceClosure
		}
		for journalID := range closure {
			sourceClosure[journalID] = struct{}{}
		}
		if len(sourceClosure) > 1000 {
			return invalid("source reversal closure exceeds the journal bound")
		}
	}
	for sourceID, sourceClosure := range closuresBySource {
		facts := basLegacyCashSourceFacts{journals: make(map[string]basLegacyCashJournalSupport, len(sourceClosure))}
		for journalID := range sourceClosure {
			journal := journalByID[journalID]
			cashCredit := false
			for _, line := range linesByJournal[journalID] {
				if line.AccountOrganisationID == organisationID &&
					(line.ReportClassification == "balance_sheet.cash" || line.ReportClassification == "balance_sheet.cash_equivalent") &&
					line.CreditMinor > line.DebitMinor {
					cashCredit = true
					break
				}
			}
			facts.journals[journalID] = basLegacyCashJournalSupport{
				version: journal.Version, sourceKind: journal.SourceKind, sourceType: journal.SourceType,
				state: journal.State, journalDate: journal.JournalDate, cashCredit: cashCredit,
			}
			dateKey, _ := basLegacyCashDateKey(journal.JournalDate)
			if journal.State == "POSTED" && cashCredit && dateKey >= purchaseDateKey(period.PeriodStart) &&
				dateKey <= purchaseDateKey(period.PeriodEnd) && dateKey <= purchaseDateKey(balance) {
				facts.currentCashCredit = true
			}
		}
		currentCashBySource[sourceID] = facts
	}
	return currentCashBySource, nil
}

func basLegacyCashCivilDateKey(value *tammyv1.CivilDate) (int32, bool) {
	if !validPurchaseDate(value) {
		return 0, false
	}
	return int32(value.Year*10000 + value.Month*100 + value.Day), true
}

func basLegacyCashDateKey(value string) (int32, bool) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return 0, false
	}
	return int32(parsed.Year()*10000 + int(parsed.Month())*100 + parsed.Day()), true
}
