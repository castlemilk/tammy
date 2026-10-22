package purchases

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Mapper/API shape tested below. Nominations retains every current typed root,
// including suppressed rows for full-proof reconciliation. Candidates contains
// only unsuppressed semantic units; its keys retain opening conversion identity,
// and maximumCandidates is checked against len(Candidates), never Nominations.
//
// type BASLegacyCandidateKind uint8
// const (
//     BASLegacyCandidateDocument BASLegacyCandidateKind = iota + 1
//     BASLegacyCandidateOpeningPayable
// )
// type BASLegacyCandidateKey struct {
//     Kind BASLegacyCandidateKind
//     OrganisationID, SourceID string
//     ConversionID string // opening payable only
// }
// type BASLegacyNomination struct {
//     Key BASLegacyCandidateKey
//     SourceVersion uint64
//     DocumentRawSHA256 []byte // nil for opening until retained-input hydration
//     ProvisionalEvidenceOnly bool
//     ProvisionalDecisionIDs []string // all current-tuple decisions, sorted
// }
// type BASLegacyCandidateDiscovery struct {
//     Nominations []BASLegacyNomination
//     Candidates []BASLegacyCandidateKey
// }
// func nominateBASLegacyCandidates(
//     organisationID string,
//     period *tammyv1.PurchaseClaimPeriod,
//     balance *tammyv1.CivilDate,
//     documentStates []BASDocumentState,
//     purchaseKeys []BASScalarKey,
//     decisions []*tammyv1.LegacyPurchaseReview, // complete bounded history
//     cash accounting.BASLegacyCashRead,
//     maximumCandidates int64,
// ) (*BASLegacyCandidateDiscovery, error)

const legacyNominationTestOrg = "018f0000-0000-7000-8000-000000000001"
const legacyNominationTestActor = "018f0000-0000-7000-8000-000000000002"

func legacyNominationHash(value byte) []byte {
	return bytes.Repeat([]byte{value}, 32)
}

func legacyNominationText(value string) *string   { return &value }
func legacyNominationNumber(value uint64) *uint64 { return &value }

func legacyNominationDate(month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: 2026, Month: month, Day: day}
}

func legacyNominationMonthEnd(month int32) int32 {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		return 28
	default:
		return 31
	}
}

func legacyNominationPeriod(month int32) *tammyv1.PurchaseClaimPeriod {
	return &tammyv1.PurchaseClaimPeriod{
		PeriodStart: legacyNominationDate(month, 1),
		PeriodEnd:   legacyNominationDate(month, legacyNominationMonthEnd(month)),
	}
}

func legacyNominationDocumentFacts(org, id string, version uint64, status tammyv1.DocumentStatus, rawHash []byte, postingID string) (BASDocumentState, BASScalarKey) {
	state := BASDocumentState{
		OrganisationID: org,
		ID:             id,
		Version:        version,
		Status:         status,
		ContentHash:    bytes.Clone(rawHash),
	}
	if postingID != "" {
		state.PostingID = legacyNominationText(postingID)
	}
	key := BASScalarKey{
		Kind:           BASScalarDocument,
		OrganisationID: org,
		ID:             id,
		ObjectVersion:  legacyNominationNumber(version),
		SourceHash:     bytes.Clone(rawHash),
		IsRoot:         postingID == "",
	}
	return state, key
}

func legacyNominationPostingKey(org, postingID string, document BASDocumentState) BASScalarKey {
	return BASScalarKey{
		Kind:                 BASScalarPosting,
		OrganisationID:       org,
		ID:                   postingID,
		DocumentID:           legacyNominationText(document.ID),
		SourceID:             legacyNominationText(document.ID),
		SourceVersion:        legacyNominationNumber(document.Version),
		SourceHash:           bytes.Clone(document.ContentHash),
		RecordHash:           legacyNominationHash(0x51),
		ReviewedSnapshotHash: legacyNominationHash(0x52),
		IsRoot:               true,
	}
}

func legacyNominationDecision(
	t *testing.T,
	recordID, org string,
	recordRevision uint64,
	source *tammyv1.LegacyPurchaseSourceRef,
	disposition tammyv1.LegacyPurchaseDisposition,
	linkedPostingID string,
	affectedMonth, previouslyClaimedMonth int32,
	supportID string,
	supportHash []byte,
) *tammyv1.LegacyPurchaseReview {
	t.Helper()
	input := &tammyv1.LegacyPurchaseReviewInput{
		Source:               proto.Clone(source).(*tammyv1.LegacyPurchaseSourceRef),
		AffectedPeriod:       legacyNominationPeriod(affectedMonth),
		ReportingBalanceDate: legacyNominationDate(affectedMonth, legacyNominationMonthEnd(affectedMonth)),
		Disposition:          disposition,
		SupportingEvidence: []*tammyv1.PurchaseRetainedSourceRef{{
			SourceId:      supportID,
			SourceVersion: 1,
			SourceHash:    bytes.Clone(supportHash),
		}},
		Reason: "Retained evidence for the current legacy source",
	}
	if linkedPostingID != "" {
		input.LinkedPostingId = legacyNominationText(linkedPostingID)
	}
	if previouslyClaimedMonth != 0 {
		input.PreviouslyClaimedPeriod = legacyNominationPeriod(previouslyClaimedMonth)
		input.ClaimEvidence = &tammyv1.PurchaseRetainedSourceRef{
			SourceId:      supportID,
			SourceVersion: 1,
			SourceHash:    bytes.Clone(supportHash),
		}
	}
	if err := ValidateLegacyReview(input); err != nil {
		t.Fatalf("invalid test review input: %v", err)
	}
	record := &tammyv1.LegacyPurchaseReview{
		Id:             recordID,
		OrganisationId: org,
		Review:         input,
		SourceRevision: recordRevision,
		ActorUserId:    legacyNominationTestActor,
		RecordedAt:     timestamppb.New(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)),
	}
	var err error
	record.SourceHash, err = purchaseHash(record)
	if err != nil {
		t.Fatalf("hash test review: %v", err)
	}
	return record
}

func legacyNominationDecisionKey(t *testing.T, record *tammyv1.LegacyPurchaseReview) BASScalarKey {
	t.Helper()
	kind, sourceID, sourceVersion, sourceHash, reviewedHash, err := legacySourceScope(record.Review.Source)
	if err != nil {
		t.Fatalf("read test decision source scope: %v", err)
	}
	key := BASScalarKey{
		Kind:                 BASScalarLegacyReview,
		OrganisationID:       record.OrganisationId,
		ID:                   record.Id,
		SourceKind:           legacyNominationText(kind),
		SourceID:             legacyNominationText(sourceID),
		SourceVersion:        legacyNominationNumber(sourceVersion),
		AccountingVersion:    legacyNominationNumber(record.SourceRevision),
		SourceHash:           bytes.Clone(sourceHash),
		RecordHash:           bytes.Clone(record.SourceHash),
		ReviewedSnapshotHash: bytes.Clone(reviewedHash),
	}
	if record.Review.Source.GetDocument() != nil {
		key.DocumentID = legacyNominationText(sourceID)
	} else {
		key.OpeningPayableID = legacyNominationText(sourceID)
	}
	if record.Review.LinkedPostingId != nil {
		key.PostingID = legacyNominationText(record.Review.GetLinkedPostingId())
	}
	if record.Review.ClaimEvidence != nil {
		key.ClaimEvidenceSourceID = legacyNominationText(record.Review.ClaimEvidence.SourceId)
		key.ClaimEvidenceSourceVersion = legacyNominationNumber(record.Review.ClaimEvidence.SourceVersion)
		key.ClaimEvidenceSourceHash = bytes.Clone(record.Review.ClaimEvidence.SourceHash)
	}
	return key
}

func legacyNominationDocumentSource(id string, version uint64, rawHash, reviewedHash []byte) *tammyv1.LegacyPurchaseSourceRef {
	return &tammyv1.LegacyPurchaseSourceRef{
		Source: &tammyv1.LegacyPurchaseSourceRef_Document{Document: &tammyv1.LegacyPurchaseDocumentRef{
			DocumentId:           id,
			ReviewedVersion:      version,
			OriginalContentHash:  bytes.Clone(rawHash),
			ReviewedSnapshotHash: bytes.Clone(reviewedHash),
		}},
	}
}

func legacyNominationOpeningSource(id string, version uint64, hash []byte) *tammyv1.LegacyPurchaseSourceRef {
	return &tammyv1.LegacyPurchaseSourceRef{
		Source: &tammyv1.LegacyPurchaseSourceRef_OpeningPayable{OpeningPayable: &tammyv1.LegacyPurchaseOpeningPayableRef{
			OpeningPayableId: id,
			SourceVersion:    version,
			SourceHash:       bytes.Clone(hash),
		}},
	}
}

func legacyNominationAddPromotedSupport(org, documentID, postingID string, rawHash []byte, states *[]BASDocumentState, keys *[]BASScalarKey) {
	state, key := legacyNominationDocumentFacts(org, documentID, 1, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, rawHash, postingID)
	*states = append(*states, state)
	*keys = append(*keys, key, legacyNominationPostingKey(org, postingID, state))
}

func legacyNominationCandidate(kind BASLegacyCandidateKind, org, sourceID, conversionID string) BASLegacyCandidateKey {
	return BASLegacyCandidateKey{Kind: kind, OrganisationID: org, SourceID: sourceID, ConversionID: conversionID}
}

func legacyNominationBySource(t *testing.T, discovery *BASLegacyCandidateDiscovery, sourceID string) BASLegacyNomination {
	t.Helper()
	for _, row := range discovery.Nominations {
		if row.Key.SourceID == sourceID {
			return row
		}
	}
	t.Fatalf("nomination for source %s missing from %#v", sourceID, discovery.Nominations)
	return BASLegacyNomination{}
}

func legacyNominationSnapshot(t *testing.T, keys []BASScalarKey, states []BASDocumentState, decisions []*tammyv1.LegacyPurchaseReview) []byte {
	t.Helper()
	content, err := json.Marshal(struct {
		Keys      []BASScalarKey
		States    []BASDocumentState
		Decisions []*tammyv1.LegacyPurchaseReview
	}{keys, states, decisions})
	if err != nil {
		t.Fatalf("marshal nomination inputs: %v", err)
	}
	return content
}

func TestBASLegacyNominationEmptyInputReturnsEmptyDiscovery(t *testing.T) {
	period := legacyNominationPeriod(8)
	balance := legacyNominationDate(8, 31)
	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, period, balance, nil, nil, nil, legacyNominationEmptyCashRead(legacyNominationTestOrg), 0)
	if err != nil {
		t.Fatalf("empty input: %v", err)
	}
	if got == nil || len(got.Nominations) != 0 || len(got.Candidates) != 0 {
		t.Fatalf("empty input discovery = %#v, want nonnil empty discovery", got)
	}
}

func TestBASLegacyNominationNeedsReviewReviewedAndPromotedStates(t *testing.T) {
	const (
		needsID    = "018f0000-0000-7000-8000-000000000101"
		reviewedID = "018f0000-0000-7000-8000-000000000102"
		postedID   = "018f0000-0000-7000-8000-000000000103"
		postingID  = "018f0000-0000-7000-8000-000000000201"
	)
	needsHash, reviewedHash, postedHash := legacyNominationHash(0x11), legacyNominationHash(0x12), legacyNominationHash(0x13)
	needs, needsKey := legacyNominationDocumentFacts(legacyNominationTestOrg, needsID, 1, tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, needsHash, "")
	reviewed, reviewedKey := legacyNominationDocumentFacts(legacyNominationTestOrg, reviewedID, 2, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, reviewedHash, "")
	posted, postedKey := legacyNominationDocumentFacts(legacyNominationTestOrg, postedID, 3, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, postedHash, postingID)
	keys := []BASScalarKey{needsKey, reviewedKey, postedKey, legacyNominationPostingKey(legacyNominationTestOrg, postingID, posted)}
	states := []BASDocumentState{needs, reviewed, posted}
	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, nil, legacyNominationEmptyCashRead(legacyNominationTestOrg), 2)
	if err != nil {
		t.Fatalf("map current document states: %v", err)
	}
	if got == nil || len(got.Nominations) != 2 {
		t.Fatalf("nominations = %#v, want the NEEDS_REVIEW and unpromoted REVIEWED roots", got)
	}
	if len(got.Candidates) != 2 || !reflect.DeepEqual(got.Candidates, []BASLegacyCandidateKey{
		legacyNominationCandidate(BASLegacyCandidateDocument, legacyNominationTestOrg, needsID, ""),
		legacyNominationCandidate(BASLegacyCandidateDocument, legacyNominationTestOrg, reviewedID, ""),
	}) {
		t.Fatalf("semantic candidates = %#v", got.Candidates)
	}
	for _, expected := range []struct {
		id      string
		version uint64
		hash    []byte
	}{{needsID, 1, needsHash}, {reviewedID, 2, reviewedHash}} {
		row := legacyNominationBySource(t, got, expected.id)
		if row.Key.Kind != BASLegacyCandidateDocument || row.Key.OrganisationID != legacyNominationTestOrg || row.Key.ConversionID != "" ||
			row.SourceVersion != expected.version || !bytes.Equal(row.DocumentRawSHA256, expected.hash) || row.ProvisionalEvidenceOnly || len(row.ProvisionalDecisionIDs) != 0 {
			t.Fatalf("document nomination for %s = %#v", expected.id, row)
		}
	}
	if _, ok := func() (BASLegacyNomination, bool) {
		for _, row := range got.Nominations {
			if row.Key.SourceID == postedID {
				return row, true
			}
		}
		return BASLegacyNomination{}, false
	}(); ok {
		t.Fatalf("promoted document %s was emitted as a legacy candidate", postedID)
	}
}

func TestBASLegacyNominationStaleHistoryDoesNotSuppressCurrentConsistentEvidenceLink(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000000301"
		supportID  = "018f0000-0000-7000-8000-000000000302"
		postingID  = "018f0000-0000-7000-8000-000000000303"
		staleID    = "018f0000-0000-7000-8000-000000000401"
		currentID1 = "018f0000-0000-7000-8000-000000000402"
		currentID2 = "018f0000-0000-7000-8000-000000000403"
	)
	currentHash, oldHash, reviewedHash, supportHash := legacyNominationHash(0x21), legacyNominationHash(0x20), legacyNominationHash(0x22), legacyNominationHash(0x23)
	current, currentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, currentHash, "")
	states := []BASDocumentState{current}
	keys := []BASScalarKey{currentKey}
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportID, postingID, supportHash, &states, &keys)

	stale := legacyNominationDecision(t, staleID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 1, oldHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 6, 0, supportID, supportHash)
	currentFirst := legacyNominationDecision(t, currentID1, legacyNominationTestOrg, 2,
		legacyNominationDocumentSource(documentID, 2, currentHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	currentSecond := legacyNominationDecision(t, currentID2, legacyNominationTestOrg, 3,
		legacyNominationDocumentSource(documentID, 2, currentHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 7, 0, supportID, supportHash)
	decisions := []*tammyv1.LegacyPurchaseReview{stale, currentFirst, currentSecond}
	for _, decision := range decisions {
		keys = append(keys, legacyNominationDecisionKey(t, decision))
	}

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 0)
	if err != nil {
		t.Fatalf("map current evidence-only history with stale prefix: %v", err)
	}
	if got == nil || len(got.Nominations) != 1 || len(got.Candidates) != 0 {
		t.Fatalf("current consistent evidence-only source = %#v, want one retained nomination and zero semantic candidates", got)
	}
	row := legacyNominationBySource(t, got, documentID)
	if !row.ProvisionalEvidenceOnly {
		t.Fatalf("current consistent evidence-only rows were not provisionally grouped: %#v", row)
	}
	if !reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{currentID1, currentID2}) {
		t.Fatalf("provisional decision IDs = %v, want only sorted current tuple IDs %v", row.ProvisionalDecisionIDs, []string{currentID1, currentID2})
	}
	if row.SourceVersion != 2 || !bytes.Equal(row.DocumentRawSHA256, currentHash) {
		t.Fatalf("current source tuple = %#v", row)
	}
}

func TestBASLegacyNominationDefersReviewedCandidateHashUntilHydration(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000000451"
		supportID  = "018f0000-0000-7000-8000-000000000452"
		postingID  = "018f0000-0000-7000-8000-000000000453"
		firstID    = "018f0000-0000-7000-8000-000000000454"
		secondID   = "018f0000-0000-7000-8000-000000000455"
	)
	rawHash, supportHash := legacyNominationHash(0x2a), legacyNominationHash(0x2b)
	current, currentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, rawHash, "")
	states := []BASDocumentState{current}
	keys := []BASScalarKey{currentKey}
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportID, postingID, supportHash, &states, &keys)
	first := legacyNominationDecision(t, firstID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, rawHash, legacyNominationHash(0x2c)),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	second := legacyNominationDecision(t, secondID, legacyNominationTestOrg, 2,
		legacyNominationDocumentSource(documentID, 2, rawHash, legacyNominationHash(0x2d)),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	decisions := []*tammyv1.LegacyPurchaseReview{first, second}
	keys = append(keys, legacyNominationDecisionKey(t, first), legacyNominationDecisionKey(t, second))

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31),
		states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("provisionally group same raw tuple with different reviewed candidate hashes: %v", err)
	}
	row := legacyNominationBySource(t, got, documentID)
	if !row.ProvisionalEvidenceOnly || len(got.Candidates) != 0 ||
		!reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{firstID, secondID}) {
		t.Fatalf("reviewed candidate hash was used before hydration or history was dropped: row=%#v candidates=%#v", row, got.Candidates)
	}
}

func TestBASLegacyNominationRequiresCurrentLinkedPostingTuple(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000000471"
		supportID  = "018f0000-0000-7000-8000-000000000472"
		postingID  = "018f0000-0000-7000-8000-000000000473"
		reviewID   = "018f0000-0000-7000-8000-000000000474"
	)
	documentHash, supportHash, reviewedHash := legacyNominationHash(0x2e), legacyNominationHash(0x2f), legacyNominationHash(0x30)
	document, documentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, documentHash, "")
	support, supportKey := legacyNominationDocumentFacts(legacyNominationTestOrg, supportID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, supportHash, postingID)
	postingKey := legacyNominationPostingKey(legacyNominationTestOrg, postingID, support)
	postingKey.SourceVersion = legacyNominationNumber(1)
	decision := legacyNominationDecision(t, reviewID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, documentHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	decision.Review.SupportingEvidence[0].SourceVersion = 2
	decision.SourceHash, _ = purchaseHash(decision)

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31),
		[]BASDocumentState{document, support}, []BASScalarKey{documentKey, supportKey, postingKey, legacyNominationDecisionKey(t, decision)},
		[]*tammyv1.LegacyPurchaseReview{decision}, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("map stale linked posting tuple: %v", err)
	}
	row := legacyNominationBySource(t, got, documentID)
	if row.ProvisionalEvidenceOnly || len(got.Candidates) != 1 {
		t.Fatalf("stale posting source version was treated as a current evidence link: row=%#v candidates=%#v", row, got.Candidates)
	}
}

func TestBASLegacyNominationConflictingCurrentHistoryLeavesCandidateUnresolved(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000000501"
		supportID  = "018f0000-0000-7000-8000-000000000502"
		postingID  = "018f0000-0000-7000-8000-000000000503"
		evidenceID = "018f0000-0000-7000-8000-000000000601"
		claimID    = "018f0000-0000-7000-8000-000000000602"
	)
	rawHash, reviewedHash, supportHash := legacyNominationHash(0x31), legacyNominationHash(0x32), legacyNominationHash(0x33)
	current, currentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 4, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, rawHash, "")
	states := []BASDocumentState{current}
	keys := []BASScalarKey{currentKey}
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportID, postingID, supportHash, &states, &keys)

	evidenceOnly := legacyNominationDecision(t, evidenceID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 4, rawHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	claimed := legacyNominationDecision(t, claimID, legacyNominationTestOrg, 2,
		legacyNominationDocumentSource(documentID, 4, rawHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED,
		"", 7, 6, supportID, supportHash)
	decisions := []*tammyv1.LegacyPurchaseReview{evidenceOnly, claimed}
	for _, decision := range decisions {
		keys = append(keys, legacyNominationDecisionKey(t, decision))
	}

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("map conflicting current history: %v", err)
	}
	if got == nil || len(got.Nominations) != 1 || len(got.Candidates) != 1 {
		t.Fatalf("conflicting current history = %#v, want retained unsuppressed source", got)
	}
	row := legacyNominationBySource(t, got, documentID)
	if row.ProvisionalEvidenceOnly {
		t.Fatalf("evidence-only row bypassed conflicting ALREADY_CLAIMED history: %#v", row)
	}
	if !reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{evidenceID, claimID}) {
		t.Fatalf("conflict history IDs = %v, want all source-tuple rows sorted", row.ProvisionalDecisionIDs)
	}
	if !reflect.DeepEqual(got.Candidates, []BASLegacyCandidateKey{legacyNominationCandidate(BASLegacyCandidateDocument, legacyNominationTestOrg, documentID, "")}) {
		t.Fatalf("conflicting source did not remain a semantic candidate: %#v", got.Candidates)
	}
}

func TestBASLegacyNominationConflictingEvidenceLinksDoNotPickOnePosting(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000000701"
		supportOne = "018f0000-0000-7000-8000-000000000702"
		supportTwo = "018f0000-0000-7000-8000-000000000703"
		postingOne = "018f0000-0000-7000-8000-000000000704"
		postingTwo = "018f0000-0000-7000-8000-000000000705"
		reviewOne  = "018f0000-0000-7000-8000-000000000706"
		reviewTwo  = "018f0000-0000-7000-8000-000000000707"
	)
	rawHash, reviewedHash := legacyNominationHash(0x41), legacyNominationHash(0x42)
	supportHashOne, supportHashTwo := legacyNominationHash(0x43), legacyNominationHash(0x44)
	current, currentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, rawHash, "")
	states := []BASDocumentState{current}
	keys := []BASScalarKey{currentKey}
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportOne, postingOne, supportHashOne, &states, &keys)
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportTwo, postingTwo, supportHashTwo, &states, &keys)

	first := legacyNominationDecision(t, reviewOne, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, rawHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingOne, 8, 0, supportOne, supportHashOne)
	second := legacyNominationDecision(t, reviewTwo, legacyNominationTestOrg, 2,
		legacyNominationDocumentSource(documentID, 2, rawHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingTwo, 7, 0, supportTwo, supportHashTwo)
	decisions := []*tammyv1.LegacyPurchaseReview{first, second}
	for _, decision := range decisions {
		keys = append(keys, legacyNominationDecisionKey(t, decision))
	}

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("map conflicting evidence links: %v", err)
	}
	if got == nil || len(got.Nominations) != 1 || len(got.Candidates) != 1 {
		t.Fatalf("conflicting evidence links = %#v, want one unsuppressed unit", got)
	}
	row := legacyNominationBySource(t, got, documentID)
	if row.ProvisionalEvidenceOnly || !reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{reviewOne, reviewTwo}) {
		t.Fatalf("conflicting links selected a posting or dropped history: %#v", row)
	}
}

func TestBASLegacyNominationOnlyPostedOrReplacedOpeningPayablesAreRoots(t *testing.T) {
	const (
		rootPayableID  = "018f0000-0000-7000-8000-000000000801"
		rootConvertID  = "018f0000-0000-7000-8000-000000000802"
		rootItemID     = "018f0000-0000-7000-8000-000000000801" // writer invariant: ClientLineId == opening_items.id == payable.id
		draftPayableID = "018f0000-0000-7000-8000-000000000811"
		draftConvertID = "018f0000-0000-7000-8000-000000000812"
		draftItemID    = "018f0000-0000-7000-8000-000000000811" // same identity invariant for DRAFT context
		accountID      = "018f0000-0000-7000-8000-000000000814"
		supportID      = "018f0000-0000-7000-8000-000000000815"
		postingID      = "018f0000-0000-7000-8000-000000000816"
		reviewID       = "018f0000-0000-7000-8000-000000000817"
	)
	const rootConversionVersion = uint64(3)
	const draftConversionVersion = uint64(4)
	journalSourceRevision := uint64(1)
	zeroSourceKind := uint64(0)
	conversionHash := bytes.Repeat([]byte{'a'}, 64) // retained conversion SHA text, not opening input SHA
	rootJournalID := "018f0000-0000-7000-8000-000000000804"
	rootPayableKey := BASScalarKey{Kind: BASScalarOpeningPayable, OrganisationID: legacyNominationTestOrg, ID: rootPayableID,
		ConversionID: legacyNominationText(rootConvertID), IsRoot: true}
	rootConversionKey := BASScalarKey{Kind: BASScalarOpeningConversion, OrganisationID: legacyNominationTestOrg, ID: rootConvertID,
		ObjectVersion: legacyNominationNumber(rootConversionVersion), SourceHash: bytes.Clone(conversionHash), JournalID: legacyNominationText(rootJournalID),
		SourceType: legacyNominationText("OPENING"), SourceID: legacyNominationText(rootConvertID),
		SourceVersion: &journalSourceRevision, JournalSourceKind: &zeroSourceKind}
	rootItemKey := BASScalarKey{Kind: BASScalarOpeningItem, OrganisationID: legacyNominationTestOrg, ID: rootItemID,
		ConversionID: legacyNominationText(rootConvertID), AccountID: legacyNominationText(accountID)}
	draftPayableKey := BASScalarKey{Kind: BASScalarOpeningPayable, OrganisationID: legacyNominationTestOrg, ID: draftPayableID,
		ConversionID: legacyNominationText(draftConvertID), IsRoot: false}
	draftConversionKey := BASScalarKey{Kind: BASScalarOpeningConversion, OrganisationID: legacyNominationTestOrg, ID: draftConvertID,
		ObjectVersion: legacyNominationNumber(draftConversionVersion), SourceHash: bytes.Clone(conversionHash)}
	draftItemKey := BASScalarKey{Kind: BASScalarOpeningItem, OrganisationID: legacyNominationTestOrg, ID: draftItemID,
		ConversionID: legacyNominationText(draftConvertID), AccountID: legacyNominationText(accountID)}
	keys := []BASScalarKey{rootPayableKey, rootConversionKey, rootItemKey, draftPayableKey, draftConversionKey, draftItemKey}

	// A current opening review is ordinary source history, not permission to
	// remove the opening root. The opening source hash belongs to retained input;
	// conversionHash above is the independent source_sha256 scalar.
	input := &tammyv1.OpeningBalanceInput{ClientLineId: rootItemID, AccountId: accountID,
		Kind:          tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM,
		LedgerBalance: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 11000}, OriginalIssueDate: legacyNominationDate(6, 1), OriginalDueDate: legacyNominationDate(6, 1),
		OutstandingGst: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 1000}, PriorGstAttributed: &tammyv1.Money{CurrencyCode: "AUD"}}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	// Use the same deterministic SHA256 source tuple produced by the opening
	// reader, not the 64-byte conversion source_sha256 value.
	openingInputHash := sha256Sum(encoded)
	supportHash := legacyNominationHash(0x61)
	supportState, supportKey := legacyNominationDocumentFacts(legacyNominationTestOrg, supportID, 1, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, supportHash, postingID)
	states := []BASDocumentState{supportState}
	keys = append(keys, supportKey, legacyNominationPostingKey(legacyNominationTestOrg, postingID, supportState))
	openingReview := legacyNominationDecision(t, reviewID, legacyNominationTestOrg, 1,
		legacyNominationOpeningSource(rootPayableID, rootConversionVersion, openingInputHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE,
		"", 8, 0, supportID, supportHash)
	decisions := []*tammyv1.LegacyPurchaseReview{openingReview}
	keys = append(keys, legacyNominationDecisionKey(t, openingReview))

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("map opening root and DRAFT context: %v", err)
	}
	want := legacyNominationCandidate(BASLegacyCandidateOpeningPayable, legacyNominationTestOrg, rootPayableID, rootConvertID)
	if got == nil || len(got.Nominations) != 1 || !reflect.DeepEqual(got.Candidates, []BASLegacyCandidateKey{want}) {
		t.Fatalf("opening candidates = %#v, want only POSTED/REPLACED payable root %s/%s", got, rootPayableID, rootConvertID)
	}
	row := legacyNominationBySource(t, got, rootPayableID)
	if row.Key != want || row.SourceVersion != rootConversionVersion || row.DocumentRawSHA256 != nil || row.ProvisionalEvidenceOnly || len(row.ProvisionalDecisionIDs) != 0 {
		t.Fatalf("opening nomination = %#v, want root identity, no pre-body document hash or suppression", row)
	}
}

func sha256Sum(value []byte) []byte {
	digest := sha256.Sum256(value)
	return bytes.Clone(digest[:])
}

func TestBASLegacyNominationCandidateCapCountsUnsuppressedUnitsOnly(t *testing.T) {
	const (
		suppressedID = "018f0000-0000-7000-8000-000000000901"
		openID       = "018f0000-0000-7000-8000-000000000902"
		supportID    = "018f0000-0000-7000-8000-000000000903"
		postingID    = "018f0000-0000-7000-8000-000000000904"
		reviewID     = "018f0000-0000-7000-8000-000000000905"
	)
	suppressedHash, openHash, supportHash, reviewedHash := legacyNominationHash(0x71), legacyNominationHash(0x72), legacyNominationHash(0x73), legacyNominationHash(0x74)
	suppressed, suppressedKey := legacyNominationDocumentFacts(legacyNominationTestOrg, suppressedID, 2, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, suppressedHash, "")
	open, openKey := legacyNominationDocumentFacts(legacyNominationTestOrg, openID, 1, tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, openHash, "")
	states := []BASDocumentState{suppressed, open}
	keys := []BASScalarKey{suppressedKey, openKey}
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportID, postingID, supportHash, &states, &keys)
	decision := legacyNominationDecision(t, reviewID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(suppressedID, 2, suppressedHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	decisions := []*tammyv1.LegacyPurchaseReview{decision}
	keys = append(keys, legacyNominationDecisionKey(t, decision))

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("one unsuppressed semantic unit under cap: %v", err)
	}
	if got == nil || len(got.Nominations) != 2 || len(got.Candidates) != 1 || !reflect.DeepEqual(got.Candidates,
		[]BASLegacyCandidateKey{legacyNominationCandidate(BASLegacyCandidateDocument, legacyNominationTestOrg, openID, "")}) {
		t.Fatalf("cap counted source rows instead of unsuppressed units: %#v", got)
	}
	if row := legacyNominationBySource(t, got, suppressedID); !row.ProvisionalEvidenceOnly {
		t.Fatalf("suppressed nomination was lost or not marked: %#v", row)
	}
	if got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 0); got != nil || err == nil {
		t.Fatalf("zero candidate cap returned discovery=%#v err=%v, want no partial result and an error", got, err)
	}
	if got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), -1); got != nil || err == nil {
		t.Fatalf("negative candidate cap returned discovery=%#v err=%v, want no result and an error", got, err)
	}
}

func TestBASLegacyNominationCandidateCapOverflowReturnsNoPartialDiscovery(t *testing.T) {
	const (
		firstID  = "018f0000-0000-7000-8000-000000001001"
		secondID = "018f0000-0000-7000-8000-000000001002"
	)
	first, firstKey := legacyNominationDocumentFacts(legacyNominationTestOrg, firstID, 1, tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, legacyNominationHash(0x81), "")
	second, secondKey := legacyNominationDocumentFacts(legacyNominationTestOrg, secondID, 1, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, legacyNominationHash(0x82), "")
	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31),
		[]BASDocumentState{first, second}, []BASScalarKey{firstKey, secondKey}, nil, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if got != nil || err == nil {
		t.Fatalf("two units over cap returned discovery=%#v err=%v, want nil discovery and an error", got, err)
	}
}

func TestBASLegacyNominationDoesNotMutateInputsOrAliasDocumentHash(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000001101"
		supportID  = "018f0000-0000-7000-8000-000000001102"
		postingID  = "018f0000-0000-7000-8000-000000001103"
		reviewID   = "018f0000-0000-7000-8000-000000001104"
	)
	rawHash, supportHash, reviewedHash := legacyNominationHash(0x91), legacyNominationHash(0x92), legacyNominationHash(0x93)
	document, documentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2, tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, rawHash, "")
	states := []BASDocumentState{document}
	keys := []BASScalarKey{documentKey}
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportID, postingID, supportHash, &states, &keys)
	decision := legacyNominationDecision(t, reviewID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, rawHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	decisions := []*tammyv1.LegacyPurchaseReview{decision}
	keys = append(keys, legacyNominationDecisionKey(t, decision))
	slices.Reverse(keys)
	before := legacyNominationSnapshot(t, keys, states, decisions)
	beforeRawHash := bytes.Clone(states[0].ContentHash)
	beforeReviewedSource := proto.Clone(decisions[0]).(*tammyv1.LegacyPurchaseReview)

	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31), states, keys, decisions, legacyNominationEmptyCashRead(legacyNominationTestOrg), 0)
	if err != nil || got == nil || len(got.Nominations) != 1 || len(got.Candidates) != 0 {
		t.Fatalf("map without mutation = %#v, %v", got, err)
	}
	after := legacyNominationSnapshot(t, keys, states, decisions)
	if !bytes.Equal(before, after) || !proto.Equal(beforeReviewedSource, decisions[0]) {
		t.Fatal("pure nomination mutated scalar keys, current document state, or decision protobuf")
	}
	row := legacyNominationBySource(t, got, documentID)
	if len(row.DocumentRawSHA256) == 0 {
		t.Fatal("document nomination omitted its raw SHA")
	}
	row.DocumentRawSHA256[0] ^= 0xff
	if !bytes.Equal(states[0].ContentHash, beforeRawHash) {
		t.Fatal("nomination output aliases the caller's document raw SHA bytes")
	}
}

// These cash cases use the exact scalar result shape owned by accounting. The
// direct rows mirror JournalRepository.Post writer values (source_kind=0,
// source_type=MANUAL, current same-owner account cells). AccountVersion=2 with
// Journal.Version=1 models AccountRepository.Update changing current account
// classification without rewriting the journal. The reversal pair mirrors
// JournalRepository.Reverse's reciprocal state, source ID, version, and revision.
const legacyNominationCashTestOrg = "018f0000-0000-7000-8000-000000002001"

func legacyNominationCashTestPeriod(startMonth, startDay, endMonth, endDay int32) *tammyv1.PurchaseClaimPeriod {
	return &tammyv1.PurchaseClaimPeriod{
		PeriodStart: legacyNominationDate(startMonth, startDay),
		PeriodEnd:   legacyNominationDate(endMonth, endDay),
	}
}

func legacyNominationCashTestRead(
	org string,
	associations []accounting.BASLegacyCashAssociation,
	journals []accounting.BASLegacyCashJournal,
	lines []accounting.BASLegacyCashLine,
) accounting.BASLegacyCashRead {
	read := accounting.BASLegacyCashRead{
		Associations: associations,
		Journals:     journals,
		Lines:        lines,
	}
	rowCount := int64(len(associations) + len(journals) + len(lines))
	charged := rowCount * 512
	addText := func(value string) { charged += int64(len(value)) }
	addOptionalText := func(value *string) {
		if value != nil {
			addText(*value)
		}
	}
	for _, value := range associations {
		if value.FactSourceContentHashPresent {
			charged += int64(len(value.FactSourceContentHash))
		}
		addText(value.SourceID)
		addText(value.JournalID)
		addText(value.Kind)
		addOptionalText(value.TaxFactID)
		addOptionalText(value.JournalLineID)
		addOptionalText(value.FactSourceType)
	}
	for _, value := range journals {
		addText(value.OrganisationID)
		addText(value.ID)
		addText(value.SourceType)
		addText(value.SourceID)
		addText(value.State)
		addText(value.JournalDate)
		addOptionalText(value.ReversalOfID)
		addOptionalText(value.ReversedByID)
	}
	for _, value := range lines {
		addText(value.JournalID)
		addText(value.ID)
		addText(value.AccountID)
		addText(value.AccountOrganisationID)
		addText(value.ReportClassification)
	}
	read.Summary = accounting.BASLegacyCashSummary{
		OrganisationID:   org,
		AssociationCount: int64(len(associations)),
		JournalCount:     int64(len(journals)),
		LineCount:        int64(len(lines)),
		RowCount:         rowCount,
		Bytes:            charged,
	}
	return read
}

// Empty selector results still carry the owner and exact zero summary expected
// from accounting; a zero-value read is not a valid reserved result.
func legacyNominationEmptyCashRead(org string) accounting.BASLegacyCashRead {
	return legacyNominationCashTestRead(org, nil, nil, nil)
}

func legacyNominationCashTestDirectRead(
	org, sourceID, journalID, lineID, accountID, counterLineID, counterAccountID,
	journalDate, state, classification string,
	accountVersion uint64,
	debitMinor, creditMinor int64,
) accounting.BASLegacyCashRead {
	association := accounting.BASLegacyCashAssociation{
		SourceID: sourceID, JournalID: journalID, Kind: "DIRECT",
	}
	journal := accounting.BASLegacyCashJournal{
		OrganisationID: org, ID: journalID, Version: 1, SourceRevision: 1,
		SourceKind: 0, SourceType: "MANUAL", SourceID: sourceID, State: state, JournalDate: journalDate,
	}
	// The writer requires a balanced multi-line journal. The counter-line is
	// the exact opposite movement on a current expense account.
	lines := []accounting.BASLegacyCashLine{
		{JournalID: journalID, ID: lineID, AccountID: accountID, AccountOrganisationID: org,
			Sequence: 1, AccountVersion: accountVersion, DebitMinor: debitMinor, CreditMinor: creditMinor,
			ReportClassification: classification},
		{JournalID: journalID, ID: counterLineID, AccountID: counterAccountID, AccountOrganisationID: org,
			Sequence: 2, AccountVersion: 1, DebitMinor: creditMinor, CreditMinor: debitMinor,
			ReportClassification: "profit_loss.operating_expense"},
	}
	return legacyNominationCashTestRead(org, []accounting.BASLegacyCashAssociation{association},
		[]accounting.BASLegacyCashJournal{journal}, lines)
}

func legacyNominationCashTestTaxFactRead(
	org, sourceID, journalID, journalSourceID, factID, lineID, accountID,
	counterLineID, counterAccountID, journalDate, classification string,
	accountVersion uint64,
) accounting.BASLegacyCashRead {
	factRevision := uint64(1)
	association := accounting.BASLegacyCashAssociation{
		SourceID: sourceID, JournalID: journalID, Kind: "TAX_FACT",
		TaxFactID: legacyNominationText(factID), JournalLineID: legacyNominationText(lineID),
		FactSourceType: legacyNominationText("manual_evidence"), FactSourceRevision: &factRevision,
	}
	journal := accounting.BASLegacyCashJournal{
		OrganisationID: org, ID: journalID, Version: 1, SourceRevision: 1,
		SourceKind: 0, SourceType: "MANUAL", SourceID: journalSourceID, State: "POSTED", JournalDate: journalDate,
	}
	lines := []accounting.BASLegacyCashLine{
		{JournalID: journalID, ID: lineID, AccountID: accountID, AccountOrganisationID: org,
			Sequence: 1, AccountVersion: accountVersion, DebitMinor: 0, CreditMinor: 11000,
			ReportClassification: classification},
		{JournalID: journalID, ID: counterLineID, AccountID: counterAccountID, AccountOrganisationID: org,
			Sequence: 2, AccountVersion: 1, DebitMinor: 11000, CreditMinor: 0,
			ReportClassification: "profit_loss.operating_expense"},
	}
	return legacyNominationCashTestRead(org, []accounting.BASLegacyCashAssociation{association},
		[]accounting.BASLegacyCashJournal{journal}, lines)
}

func legacyNominationCashTestReversalRead(
	org, sourceID, originalID, reversalID, originalCashLineID, originalExpenseLineID,
	reversalCashLineID, reversalExpenseLineID, cashAccountID, expenseAccountID string,
	originalCashDebit bool,
) accounting.BASLegacyCashRead {
	original := accounting.BASLegacyCashJournal{
		OrganisationID: org, ID: originalID, Version: 2, SourceRevision: 1,
		SourceKind: 0, SourceType: "MANUAL", SourceID: sourceID, State: "REVERSED", JournalDate: "2026-08-15",
		ReversedByID: legacyNominationText(reversalID),
	}
	reversal := accounting.BASLegacyCashJournal{
		OrganisationID: org, ID: reversalID, Version: 1, SourceRevision: 1,
		SourceKind: 0, SourceType: "REVERSAL", SourceID: originalID, State: "POSTED", JournalDate: "2026-08-20",
		ReversalOfID: legacyNominationText(originalID),
	}
	originalCashDebitMinor, originalCashCreditMinor := int64(0), int64(11000)
	originalExpenseDebitMinor, originalExpenseCreditMinor := int64(11000), int64(0)
	reversalCashDebitMinor, reversalCashCreditMinor := int64(11000), int64(0)
	reversalExpenseDebitMinor, reversalExpenseCreditMinor := int64(0), int64(11000)
	if originalCashDebit {
		originalCashDebitMinor, originalCashCreditMinor = 11000, 0
		originalExpenseDebitMinor, originalExpenseCreditMinor = 0, 11000
		reversalCashDebitMinor, reversalCashCreditMinor = 0, 11000
		reversalExpenseDebitMinor, reversalExpenseCreditMinor = 11000, 0
	}
	lines := []accounting.BASLegacyCashLine{
		{JournalID: originalID, ID: originalCashLineID, AccountID: cashAccountID, AccountOrganisationID: org,
			Sequence: 1, AccountVersion: 2, DebitMinor: originalCashDebitMinor, CreditMinor: originalCashCreditMinor,
			ReportClassification: "balance_sheet.cash"},
		{JournalID: originalID, ID: originalExpenseLineID, AccountID: expenseAccountID, AccountOrganisationID: org,
			Sequence: 2, AccountVersion: 1, DebitMinor: originalExpenseDebitMinor, CreditMinor: originalExpenseCreditMinor,
			ReportClassification: "profit_loss.operating_expense"},
		{JournalID: reversalID, ID: reversalCashLineID, AccountID: cashAccountID, AccountOrganisationID: org,
			Sequence: 1, AccountVersion: 2, DebitMinor: reversalCashDebitMinor, CreditMinor: reversalCashCreditMinor,
			ReportClassification: "balance_sheet.cash"},
		{JournalID: reversalID, ID: reversalExpenseLineID, AccountID: expenseAccountID, AccountOrganisationID: org,
			Sequence: 2, AccountVersion: 1, DebitMinor: reversalExpenseDebitMinor, CreditMinor: reversalExpenseCreditMinor,
			ReportClassification: "profit_loss.operating_expense"},
	}
	association := accounting.BASLegacyCashAssociation{SourceID: sourceID, JournalID: originalID, Kind: "DIRECT"}
	return legacyNominationCashTestRead(org, []accounting.BASLegacyCashAssociation{association},
		[]accounting.BASLegacyCashJournal{original, reversal}, lines)
}

func legacyNominationCashTestCandidate(t *testing.T, period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate, cash accounting.BASLegacyCashRead) (*BASLegacyCandidateDiscovery, string, string) {
	t.Helper()
	got, documentID, reviewID, err := legacyNominationCashTestCandidateResult(t, period, balance, cash)
	if err != nil {
		t.Fatalf("map source-linked cash facts: %v", err)
	}
	return got, documentID, reviewID
}

func legacyNominationCashTestCandidateResult(t *testing.T, period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate, cash accounting.BASLegacyCashRead) (*BASLegacyCandidateDiscovery, string, string, error) {
	t.Helper()
	const (
		documentID = "018f0000-0000-7000-8000-000000002101"
		supportID  = "018f0000-0000-7000-8000-000000002102"
		postingID  = "018f0000-0000-7000-8000-000000002103"
		reviewID   = "018f0000-0000-7000-8000-000000002104"
	)
	rawHash, supportHash, reviewedHash := legacyNominationHash(0xa1), legacyNominationHash(0xa2), legacyNominationHash(0xa3)
	current, currentKey := legacyNominationDocumentFacts(legacyNominationCashTestOrg, documentID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, rawHash, "")
	states := []BASDocumentState{current}
	keys := []BASScalarKey{currentKey}
	legacyNominationAddPromotedSupport(legacyNominationCashTestOrg, supportID, postingID, supportHash, &states, &keys)
	decision := legacyNominationDecision(t, reviewID, legacyNominationCashTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, rawHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	keys = append(keys, legacyNominationDecisionKey(t, decision))
	got, err := nominateBASLegacyCandidates(legacyNominationCashTestOrg, period, balance,
		states, keys, []*tammyv1.LegacyPurchaseReview{decision}, cash, 1)
	return got, documentID, reviewID, err
}

func TestBASLegacyNominationCurrentCashCreditUsesPeriodBalanceAndCurrentAccount(t *testing.T) {
	cashAccountID := "018f0000-0000-7000-8000-000000002201"
	journalID := "018f0000-0000-7000-8000-000000002202"
	lineID := "018f0000-0000-7000-8000-000000002203"
	counterLineID := "018f0000-0000-7000-8000-000000002204"
	expenseAccountID := "018f0000-0000-7000-8000-000000002205"
	standardPeriod := legacyNominationPeriod(8)
	standardBalance := legacyNominationDate(8, 31)
	periodThroughSeptember := legacyNominationCashTestPeriod(8, 1, 9, 30)
	balanceMidSeptember := legacyNominationDate(9, 15)
	cases := []struct {
		name, journalDate, state, classification string
		period                                   *tammyv1.PurchaseClaimPeriod
		balance                                  *tammyv1.CivilDate
		accountVersion                           uint64
		debitMinor, creditMinor                  int64
		wantSuppressed                           bool
	}{
		{
			name:        "cash credit on inclusive period start uses current account classification",
			journalDate: "2026-08-01", state: "POSTED", classification: "balance_sheet.cash",
			period: standardPeriod, balance: standardBalance, accountVersion: 2, creditMinor: 11000,
			wantSuppressed: false,
		},
		{
			name:        "cash equivalent credit on inclusive period and balance end",
			journalDate: "2026-08-31", state: "POSTED", classification: "balance_sheet.cash_equivalent",
			period: standardPeriod, balance: standardBalance, accountVersion: 2, creditMinor: 11000,
			wantSuppressed: false,
		},
		{
			name:        "cash credit before requested period does not block suppression",
			journalDate: "2026-07-31", state: "POSTED", classification: "balance_sheet.cash",
			period: standardPeriod, balance: standardBalance, accountVersion: 2, creditMinor: 11000,
			wantSuppressed: true,
		},
		{
			name:        "cash credit inside period but after reporting balance does not block suppression",
			journalDate: "2026-09-20", state: "POSTED", classification: "balance_sheet.cash",
			period: periodThroughSeptember, balance: balanceMidSeptember, accountVersion: 2, creditMinor: 11000,
			wantSuppressed: true,
		},
		{
			name:        "current account reclassified away from cash does not block suppression",
			journalDate: "2026-08-15", state: "POSTED", classification: "profit_loss.operating_expense",
			period: standardPeriod, balance: standardBalance, accountVersion: 2, creditMinor: 11000,
			wantSuppressed: true,
		},
		{
			name:        "cash debit without a credit does not block suppression",
			journalDate: "2026-08-15", state: "POSTED", classification: "balance_sheet.cash",
			period: standardPeriod, balance: standardBalance, accountVersion: 2, debitMinor: 11000,
			wantSuppressed: true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cash := legacyNominationCashTestDirectRead(legacyNominationCashTestOrg,
				"018f0000-0000-7000-8000-000000002101", journalID, lineID, cashAccountID,
				counterLineID, expenseAccountID, test.journalDate, test.state, test.classification,
				test.accountVersion, test.debitMinor, test.creditMinor)
			got, documentID, reviewID := legacyNominationCashTestCandidate(t, test.period, test.balance, cash)
			row := legacyNominationBySource(t, got, documentID)
			if row.ProvisionalEvidenceOnly != test.wantSuppressed {
				t.Fatalf("provisional evidence-only suppression=%t, want %t for cash facts %+v", row.ProvisionalEvidenceOnly, test.wantSuppressed, cash)
			}
			wantCandidates := 1
			if test.wantSuppressed {
				wantCandidates = 0
			}
			if len(got.Candidates) != wantCandidates {
				t.Fatalf("semantic candidate count=%d, want %d for cash facts %+v", len(got.Candidates), wantCandidates, cash)
			}
			if !reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{reviewID}) {
				t.Fatalf("cash qualification dropped retained decision group IDs: %v", row.ProvisionalDecisionIDs)
			}
			if len(cash.Journals) != 1 || cash.Journals[0].Version != 1 || cash.Lines[0].AccountVersion != test.accountVersion {
				t.Fatalf("fixture must retain journal version 1 and current account version %d: %+v", test.accountVersion, cash)
			}
		})
	}
}

func TestBASLegacyNominationTaxFactCashCreditPreventsEvidenceOnlySuppression(t *testing.T) {
	const (
		documentID       = "018f0000-0000-7000-8000-000000002101"
		journalID        = "018f0000-0000-7000-8000-000000002301"
		journalSourceID  = "018f0000-0000-7000-8000-000000002302"
		factID           = "018f0000-0000-7000-8000-000000002303"
		lineID           = "018f0000-0000-7000-8000-000000002304"
		counterLineID    = "018f0000-0000-7000-8000-000000002305"
		accountID        = "018f0000-0000-7000-8000-000000002306"
		expenseAccountID = "018f0000-0000-7000-8000-000000002307"
	)
	cash := legacyNominationCashTestTaxFactRead(legacyNominationCashTestOrg, documentID, journalID,
		journalSourceID, factID, lineID, accountID, counterLineID, expenseAccountID,
		"2026-08-15", "balance_sheet.cash", 2)
	got, sourceID, _ := legacyNominationCashTestCandidate(t, legacyNominationPeriod(8), legacyNominationDate(8, 31), cash)
	row := legacyNominationBySource(t, got, sourceID)
	if row.ProvisionalEvidenceOnly || len(got.Candidates) != 1 {
		t.Fatalf("current source-linked TAX_FACT cash credit did not preserve the unresolved semantic unit: row=%#v candidates=%#v", row, got.Candidates)
	}
	association := cash.Associations[0]
	if association.Kind != "TAX_FACT" || association.TaxFactID == nil || *association.TaxFactID != factID ||
		association.JournalLineID == nil || *association.JournalLineID != lineID || association.FactSourceType == nil ||
		*association.FactSourceType != "manual_evidence" || association.FactSourceRevision == nil || *association.FactSourceRevision != 1 {
		t.Fatalf("tax-fact association lost actual retained source identity: %+v", association)
	}
}

func TestBASLegacyNominationCurrentCashFollowsWriterValidReversalClosure(t *testing.T) {
	const (
		documentID          = "018f0000-0000-7000-8000-000000002101"
		originalID          = "018f0000-0000-7000-8000-000000002401"
		reversalID          = "018f0000-0000-7000-8000-000000002402"
		originalCashLine    = "018f0000-0000-7000-8000-000000002403"
		originalExpenseLine = "018f0000-0000-7000-8000-000000002404"
		reversalCashLine    = "018f0000-0000-7000-8000-000000002405"
		reversalExpenseLine = "018f0000-0000-7000-8000-000000002406"
		cashAccountID       = "018f0000-0000-7000-8000-000000002407"
		expenseAccountID    = "018f0000-0000-7000-8000-000000002408"
	)
	for _, test := range []struct {
		name              string
		originalCashDebit bool
		wantSuppressed    bool
	}{
		{name: "original cash credit reversed to debit is no longer current cash", originalCashDebit: false, wantSuppressed: true},
		{name: "posted reversal cash credit remains current through reciprocal closure", originalCashDebit: true, wantSuppressed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cash := legacyNominationCashTestReversalRead(legacyNominationCashTestOrg, documentID,
				originalID, reversalID, originalCashLine, originalExpenseLine, reversalCashLine, reversalExpenseLine,
				cashAccountID, expenseAccountID, test.originalCashDebit)
			got, sourceID, reviewID := legacyNominationCashTestCandidate(t,
				legacyNominationPeriod(8), legacyNominationDate(8, 31), cash)
			row := legacyNominationBySource(t, got, sourceID)
			if row.ProvisionalEvidenceOnly != test.wantSuppressed {
				t.Fatalf("reciprocal current-cash closure suppression=%t, want %t: row=%#v", row.ProvisionalEvidenceOnly, test.wantSuppressed, row)
			}
			wantCandidates := 1
			if test.wantSuppressed {
				wantCandidates = 0
			}
			if len(got.Candidates) != wantCandidates || !reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{reviewID}) {
				t.Fatalf("reversal closure candidates=%#v history=%v; want %d candidate and retained decision", got.Candidates, row.ProvisionalDecisionIDs, wantCandidates)
			}
			original, reversal := cash.Journals[0], cash.Journals[1]
			if original.State != "REVERSED" || original.Version != 2 || original.ReversedByID == nil || *original.ReversedByID != reversalID ||
				reversal.State != "POSTED" || reversal.Version != 1 || reversal.ReversalOfID == nil || *reversal.ReversalOfID != originalID ||
				reversal.SourceKind != 0 || reversal.SourceType != "REVERSAL" || reversal.SourceID != originalID || reversal.SourceRevision != 1 {
				t.Fatalf("fixture does not match accounting JournalRepository.Reverse reciprocity and source revision: %+v", cash.Journals)
			}
		})
	}
}

func TestBASLegacyNominationAcceptsOpaqueCashJournalLineAndFactIdentities(t *testing.T) {
	const documentID = "018f0000-0000-7000-8000-000000002101"
	for _, test := range []struct {
		name string
		cash accounting.BASLegacyCashRead
	}{
		{
			name: "direct",
			cash: legacyNominationCashTestDirectRead(legacyNominationCashTestOrg, documentID,
				"legacy-direct-journal", "legacy-cash-line", "legacy-cash-account",
				"legacy-counter-line", "legacy-expense-account", "2026-08-15", "POSTED",
				"balance_sheet.cash", 2, 0, 11000),
		},
		{
			name: "tax fact",
			cash: legacyNominationCashTestTaxFactRead(legacyNominationCashTestOrg, documentID,
				"legacy-fact-journal", "legacy-journal-source", "legacy-tax-fact",
				"legacy-fact-line", "legacy-cash-account", "legacy-counter-line", "legacy-expense-account",
				"2026-08-15", "balance_sheet.cash", 2),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, sourceID, _ := legacyNominationCashTestCandidate(t, legacyNominationPeriod(8),
				legacyNominationDate(8, 31), test.cash)
			row := legacyNominationBySource(t, got, sourceID)
			if row.ProvisionalEvidenceOnly || len(got.Candidates) != 1 {
				t.Fatalf("opaque retained cash identity was rejected or ignored: row=%#v candidates=%#v", row, got.Candidates)
			}
		})
	}
}

func TestBASLegacyNominationFollowsWriterValidThreeNodeReversalClosure(t *testing.T) {
	const (
		documentID       = "018f0000-0000-7000-8000-000000002101"
		originalID       = "018f0000-0000-7000-8000-000000002501"
		firstReversalID  = "018f0000-0000-7000-8000-000000002502"
		finalReversalID  = "018f0000-0000-7000-8000-000000002503"
		cashAccountID    = "018f0000-0000-7000-8000-000000002504"
		expenseAccountID = "018f0000-0000-7000-8000-000000002505"
	)
	original := accounting.BASLegacyCashJournal{
		OrganisationID: legacyNominationCashTestOrg, ID: originalID, Version: 2, SourceRevision: 1,
		SourceKind: 0, SourceType: "MANUAL", SourceID: documentID, State: "REVERSED", JournalDate: "2026-08-15",
		ReversedByID: legacyNominationText(firstReversalID),
	}
	first := accounting.BASLegacyCashJournal{
		OrganisationID: legacyNominationCashTestOrg, ID: firstReversalID, Version: 2, SourceRevision: 1,
		SourceKind: 0, SourceType: "REVERSAL", SourceID: originalID, State: "REVERSED", JournalDate: "2026-08-20",
		ReversalOfID: legacyNominationText(originalID), ReversedByID: legacyNominationText(finalReversalID),
	}
	final := accounting.BASLegacyCashJournal{
		OrganisationID: legacyNominationCashTestOrg, ID: finalReversalID, Version: 1, SourceRevision: 1,
		SourceKind: 0, SourceType: "REVERSAL", SourceID: firstReversalID, State: "POSTED", JournalDate: "2026-08-25",
		ReversalOfID: legacyNominationText(firstReversalID),
	}
	journals := []accounting.BASLegacyCashJournal{original, first, final}
	lines := []accounting.BASLegacyCashLine{
		{JournalID: originalID, ID: "018f0000-0000-7000-8000-000000002506", AccountID: cashAccountID, AccountOrganisationID: legacyNominationCashTestOrg,
			Sequence: 1, AccountVersion: 2, DebitMinor: 0, CreditMinor: 11000, ReportClassification: "balance_sheet.cash"},
		{JournalID: originalID, ID: "018f0000-0000-7000-8000-000000002507", AccountID: expenseAccountID, AccountOrganisationID: legacyNominationCashTestOrg,
			Sequence: 2, AccountVersion: 1, DebitMinor: 11000, CreditMinor: 0, ReportClassification: "profit_loss.operating_expense"},
		{JournalID: firstReversalID, ID: "018f0000-0000-7000-8000-000000002508", AccountID: cashAccountID, AccountOrganisationID: legacyNominationCashTestOrg,
			Sequence: 1, AccountVersion: 2, DebitMinor: 11000, CreditMinor: 0, ReportClassification: "balance_sheet.cash"},
		{JournalID: firstReversalID, ID: "018f0000-0000-7000-8000-000000002509", AccountID: expenseAccountID, AccountOrganisationID: legacyNominationCashTestOrg,
			Sequence: 2, AccountVersion: 1, DebitMinor: 0, CreditMinor: 11000, ReportClassification: "profit_loss.operating_expense"},
		{JournalID: finalReversalID, ID: "018f0000-0000-7000-8000-000000002510", AccountID: cashAccountID, AccountOrganisationID: legacyNominationCashTestOrg,
			Sequence: 1, AccountVersion: 2, DebitMinor: 0, CreditMinor: 11000, ReportClassification: "balance_sheet.cash"},
		{JournalID: finalReversalID, ID: "018f0000-0000-7000-8000-000000002511", AccountID: expenseAccountID, AccountOrganisationID: legacyNominationCashTestOrg,
			Sequence: 2, AccountVersion: 1, DebitMinor: 11000, CreditMinor: 0, ReportClassification: "profit_loss.operating_expense"},
	}
	association := accounting.BASLegacyCashAssociation{SourceID: documentID, JournalID: originalID, Kind: "DIRECT"}
	cash := legacyNominationCashTestRead(legacyNominationCashTestOrg,
		[]accounting.BASLegacyCashAssociation{association}, journals, lines)
	got, sourceID, reviewID := legacyNominationCashTestCandidate(t, legacyNominationPeriod(8), legacyNominationDate(8, 31), cash)
	row := legacyNominationBySource(t, got, sourceID)
	if row.ProvisionalEvidenceOnly || len(got.Candidates) != 1 || !reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{reviewID}) {
		t.Fatalf("three-node posted cash-credit closure did not keep the candidate unresolved: row=%#v candidates=%#v", row, got.Candidates)
	}
	if len(cash.Journals) != 3 || cash.Journals[0].State != "REVERSED" || cash.Journals[1].State != "REVERSED" || cash.Journals[2].State != "POSTED" {
		t.Fatalf("fixture is not a writer-valid three-node reversal chain: %+v", cash.Journals)
	}
}

func TestBASLegacyNominationRequiresExactCurrentSupportingDocumentVersion(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000002601"
		supportID  = "018f0000-0000-7000-8000-000000002602"
		postingID  = "018f0000-0000-7000-8000-000000002603"
		reviewID   = "018f0000-0000-7000-8000-000000002604"
	)
	documentHash, supportHash, reviewedHash := legacyNominationHash(0xb1), legacyNominationHash(0xb2), legacyNominationHash(0xb3)
	document, documentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, documentHash, "")
	support, supportKey := legacyNominationDocumentFacts(legacyNominationTestOrg, supportID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, supportHash, postingID)
	keys := []BASScalarKey{documentKey, supportKey, legacyNominationPostingKey(legacyNominationTestOrg, postingID, support)}
	decision := legacyNominationDecision(t, reviewID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, documentHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		postingID, 8, 0, supportID, supportHash)
	decision.Review.SupportingEvidence[0].SourceVersion = 1 // same raw hash, but the supported document has since advanced
	decision.SourceHash, _ = purchaseHash(decision)
	keys = append(keys, legacyNominationDecisionKey(t, decision))
	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31),
		[]BASDocumentState{document, support}, keys, []*tammyv1.LegacyPurchaseReview{decision},
		legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("nominate source with stale-version supporting evidence: %v", err)
	}
	row := legacyNominationBySource(t, got, documentID)
	if row.ProvisionalEvidenceOnly || len(got.Candidates) != 1 {
		t.Fatalf("same-hash support with a stale document version suppressed a candidate: row=%#v candidates=%#v", row, got.Candidates)
	}
}

func TestBASLegacyNominationAllowsDifferentCurrentSupportAcrossConsistentReviews(t *testing.T) {
	const (
		documentID          = "018f0000-0000-7000-8000-000000002701"
		supportOneID        = "018f0000-0000-7000-8000-000000002702"
		supportTwoID        = "018f0000-0000-7000-8000-000000002703"
		linkedPostingID     = "018f0000-0000-7000-8000-000000002704"
		supportTwoPostingID = "018f0000-0000-7000-8000-000000002705"
		reviewOneID         = "018f0000-0000-7000-8000-000000002706"
		reviewTwoID         = "018f0000-0000-7000-8000-000000002707"
	)
	documentHash, reviewedHash := legacyNominationHash(0xc1), legacyNominationHash(0xc2)
	supportOneHash, supportTwoHash := legacyNominationHash(0xc3), legacyNominationHash(0xc4)
	document, documentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, documentHash, "")
	states := []BASDocumentState{document}
	keys := []BASScalarKey{documentKey}
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportOneID, linkedPostingID, supportOneHash, &states, &keys)
	legacyNominationAddPromotedSupport(legacyNominationTestOrg, supportTwoID, supportTwoPostingID, supportTwoHash, &states, &keys)
	first := legacyNominationDecision(t, reviewOneID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, documentHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		linkedPostingID, 8, 0, supportOneID, supportOneHash)
	second := legacyNominationDecision(t, reviewTwoID, legacyNominationTestOrg, 2,
		legacyNominationDocumentSource(documentID, 2, documentHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		linkedPostingID, 8, 0, supportTwoID, supportTwoHash)
	keys = append(keys, legacyNominationDecisionKey(t, first), legacyNominationDecisionKey(t, second))
	got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(8), legacyNominationDate(8, 31),
		states, keys, []*tammyv1.LegacyPurchaseReview{first, second}, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
	if err != nil {
		t.Fatalf("map consistent evidence-only reviews with distinct current support: %v", err)
	}
	row := legacyNominationBySource(t, got, documentID)
	if !row.ProvisionalEvidenceOnly || len(got.Candidates) != 0 ||
		!reflect.DeepEqual(row.ProvisionalDecisionIDs, []string{reviewOneID, reviewTwoID}) {
		t.Fatalf("consistent same-posting evidence-only history with separate verified support was treated as conflicting: row=%#v candidates=%#v", row, got.Candidates)
	}
}

// A legacy manual payment may be retained as evidence on an evidence-only
// decision even when it falls outside the BAS query period. Before body reads,
// source-root closure + journal version/date/current cash credit are enough to
// nominate provisionally; the caller must later hydrate that journal and
// compare the deterministic Journal SHA to SupportingPayments.SourceHash before
// accepting the suppression.
func TestBASLegacyNominationMatchesManualPaymentSupportAgainstItsReviewBalance(t *testing.T) {
	const (
		documentID        = "018f0000-0000-7000-8000-000000002801"
		supportDocumentID = "018f0000-0000-7000-8000-000000002802"
		linkedPostingID   = "018f0000-0000-7000-8000-000000002803"
		reviewID          = "018f0000-0000-7000-8000-000000002804"
		journalID         = "018f0000-0000-7000-8000-000000002805"
		cashLineID        = "018f0000-0000-7000-8000-000000002806"
		expenseLineID     = "018f0000-0000-7000-8000-000000002807"
		cashAccountID     = "018f0000-0000-7000-8000-000000002808"
		expenseAccountID  = "018f0000-0000-7000-8000-000000002809"
	)

	tests := []struct {
		name                  string
		queryMonth, dateMonth int32
		journalDateDay        int32
		refVersion            uint64
		withoutAssociation    bool
		wantSuppressed        bool
	}{
		{name: "prior-period cash support uses review balance, not requested period", queryMonth: 9, dateMonth: 8, journalDateDay: 15, refVersion: 1, wantSuppressed: true},
		{name: "cash payment after review balance is not valid support", queryMonth: 10, dateMonth: 9, journalDateDay: 1, refVersion: 1},
		{name: "stale manual journal version is not valid support", queryMonth: 9, dateMonth: 8, journalDateDay: 15, refVersion: 2},
		{name: "manual journal outside candidate source closure is not valid support", queryMonth: 9, dateMonth: 8, journalDateDay: 15, refVersion: 1, withoutAssociation: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			documentHash, supportHash, reviewedHash := legacyNominationHash(0xd1), legacyNominationHash(0xd2), legacyNominationHash(0xd3)
			document, documentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2,
				tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, documentHash, "")
			support, supportKey := legacyNominationDocumentFacts(legacyNominationTestOrg, supportDocumentID, 2,
				tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, supportHash, linkedPostingID)
			states := []BASDocumentState{document, support}
			keys := []BASScalarKey{documentKey, supportKey, legacyNominationPostingKey(legacyNominationTestOrg, linkedPostingID, support)}
			decision := legacyNominationDecision(t, reviewID, legacyNominationTestOrg, 1,
				legacyNominationDocumentSource(documentID, 2, documentHash, reviewedHash),
				tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
				linkedPostingID, 8, 0, supportDocumentID, supportHash)
			decision.Review.SupportingEvidence[0].SourceVersion = 2
			decision.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{
				SourceId: journalID, SourceVersion: test.refVersion, SourceHash: legacyNominationHash(0xd4),
			}}
			// This is a valid review-specific balance window even though its
			// August affected period differs from the September/October query.
			if err := ValidateLegacyReview(decision.Review); err != nil {
				t.Fatalf("valid evidence-only review with manual payment support rejected by fixture: %v", err)
			}
			decision.SourceHash, _ = purchaseHash(decision)
			keys = append(keys, legacyNominationDecisionKey(t, decision))

			journalDate := legacyNominationDate(test.dateMonth, test.journalDateDay)
			journalDateText := fmt.Sprintf("%04d-%02d-%02d", journalDate.Year, journalDate.Month, journalDate.Day)
			cash := legacyNominationCashTestDirectRead(legacyNominationTestOrg, documentID, journalID,
				cashLineID, cashAccountID, expenseLineID, expenseAccountID, journalDateText, "POSTED",
				"balance_sheet.cash", 2, 0, 11000)
			if test.withoutAssociation {
				cash = legacyNominationEmptyCashRead(legacyNominationTestOrg)
			}
			period := legacyNominationPeriod(test.queryMonth)
			balance := legacyNominationDate(test.queryMonth, legacyNominationMonthEnd(test.queryMonth))
			got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, period, balance,
				states, keys, []*tammyv1.LegacyPurchaseReview{decision}, cash, 1)
			if err != nil {
				t.Fatalf("map manual payment support in selected source closure: %v", err)
			}
			row := legacyNominationBySource(t, got, documentID)
			wantCandidates := 1
			if test.wantSuppressed {
				wantCandidates = 0
			}
			if row.ProvisionalEvidenceOnly != test.wantSuppressed || len(got.Candidates) != wantCandidates {
				t.Fatalf("manual payment support suppression=%t candidates=%#v, want suppression=%t", row.ProvisionalEvidenceOnly, got.Candidates, test.wantSuppressed)
			}
		})
	}
}

func TestBASLegacyNominationRejectsConflictingDuplicateCashAssociationIdentity(t *testing.T) {
	const (
		documentID = "018f0000-0000-7000-8000-000000002101"
		journalID  = "legacy-fact-journal"
		factID     = "legacy-tax-fact"
	)
	base := legacyNominationCashTestTaxFactRead(legacyNominationCashTestOrg, documentID,
		journalID, "legacy-journal-source", factID, "legacy-fact-line", "legacy-cash-account",
		"legacy-counter-line", "legacy-expense-account", "2026-08-15", "balance_sheet.cash", 2)
	conflict := base.Associations[0]
	conflict.FactSourceContentHashPresent = true
	conflict.FactSourceContentHash = legacyNominationHash(0xf1)
	cash := legacyNominationCashTestRead(legacyNominationCashTestOrg,
		[]accounting.BASLegacyCashAssociation{base.Associations[0], conflict}, base.Journals, base.Lines)

	got, _, _, err := legacyNominationCashTestCandidateResult(t, legacyNominationPeriod(8), legacyNominationDate(8, 31), cash)
	if got != nil || err == nil {
		t.Fatalf("conflicting provenance for the same source/journal/kind/tax-fact identity returned discovery=%#v err=%v; want rejection", got, err)
	}
}

func TestBASLegacyNominationRequiresNullTaxFactHashAndUint32LineSequence(t *testing.T) {
	const documentID = "018f0000-0000-7000-8000-000000002101"
	base := legacyNominationCashTestTaxFactRead(legacyNominationCashTestOrg, documentID,
		"legacy-fact-journal", "legacy-journal-source", "legacy-tax-fact", "legacy-fact-line",
		"legacy-cash-account", "legacy-counter-line", "legacy-expense-account", "2026-08-15",
		"balance_sheet.cash", 2)

	t.Run("absent fact hash must remain nil", func(t *testing.T) {
		association := base.Associations[0]
		association.FactSourceContentHash = []byte{}
		cash := legacyNominationCashTestRead(legacyNominationCashTestOrg,
			[]accounting.BASLegacyCashAssociation{association}, base.Journals, base.Lines)
		got, _, _, err := legacyNominationCashTestCandidateResult(t, legacyNominationPeriod(8), legacyNominationDate(8, 31), cash)
		if got != nil || err == nil {
			t.Fatalf("absent hash encoded as non-nil empty BLOB returned discovery=%#v err=%v; want rejection", got, err)
		}
	})

	t.Run("direct absent fact hash must remain nil", func(t *testing.T) {
		direct := legacyNominationCashTestDirectRead(legacyNominationCashTestOrg, documentID,
			"legacy-direct-journal", "legacy-cash-line", "legacy-cash-account", "legacy-counter-line",
			"legacy-expense-account", "2026-08-15", "POSTED", "balance_sheet.cash", 2, 0, 11000)
		direct.Associations[0].FactSourceContentHash = []byte{}
		cash := legacyNominationCashTestRead(legacyNominationCashTestOrg,
			direct.Associations, direct.Journals, direct.Lines)
		got, _, _, err := legacyNominationCashTestCandidateResult(t, legacyNominationPeriod(8), legacyNominationDate(8, 31), cash)
		if got != nil || err == nil {
			t.Fatalf("direct association with non-nil absent hash returned discovery=%#v err=%v; want rejection", got, err)
		}
	})

	t.Run("sequence must fit the accounting uint32 field", func(t *testing.T) {
		lines := append([]accounting.BASLegacyCashLine(nil), base.Lines...)
		lines[0].Sequence = uint64(1) << 32
		cash := legacyNominationCashTestRead(legacyNominationCashTestOrg, base.Associations, base.Journals, lines)
		got, _, _, err := legacyNominationCashTestCandidateResult(t, legacyNominationPeriod(8), legacyNominationDate(8, 31), cash)
		if got != nil || err == nil {
			t.Fatalf("out-of-range line sequence returned discovery=%#v err=%v; want rejection", got, err)
		}
	})
}

func TestBASLegacyNominationChecksModernPaymentSupportAgainstItsReviewBalance(t *testing.T) {
	const (
		documentID        = "018f0000-0000-7000-8000-000000002901"
		supportDocumentID = "018f0000-0000-7000-8000-000000002902"
		linkedPostingID   = "018f0000-0000-7000-8000-000000002903"
		reviewID          = "018f0000-0000-7000-8000-000000002904"
		paymentID         = "018f0000-0000-7000-8000-000000002905"
	)
	documentHash, supportHash, reviewedHash, paymentHash := legacyNominationHash(0xe1), legacyNominationHash(0xe2),
		legacyNominationHash(0xe3), legacyNominationHash(0xe4)
	document, documentKey := legacyNominationDocumentFacts(legacyNominationTestOrg, documentID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, documentHash, "")
	support, supportKey := legacyNominationDocumentFacts(legacyNominationTestOrg, supportDocumentID, 2,
		tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED, supportHash, linkedPostingID)
	states := []BASDocumentState{document, support}
	keys := []BASScalarKey{documentKey, supportKey, legacyNominationPostingKey(legacyNominationTestOrg, linkedPostingID, support)}
	decision := legacyNominationDecision(t, reviewID, legacyNominationTestOrg, 1,
		legacyNominationDocumentSource(documentID, 2, documentHash, reviewedHash),
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE,
		linkedPostingID, 8, 0, supportDocumentID, supportHash)
	decision.Review.SupportingEvidence[0].SourceVersion = 2
	decision.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{
		SourceId: paymentID, SourceVersion: 1, SourceHash: bytes.Clone(paymentHash),
	}}
	decision.SourceHash, _ = purchaseHash(decision)
	keys = append(keys, legacyNominationDecisionKey(t, decision))

	for _, test := range []struct {
		name           string
		paymentDate    string
		wantSuppressed bool
	}{
		{name: "within review balance despite requested period", paymentDate: "2026-08-15", wantSuppressed: true},
		{name: "after review balance", paymentDate: "2026-09-01", wantSuppressed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			paymentKey := BASScalarKey{
				Kind: BASScalarPayment, OrganisationID: legacyNominationTestOrg, ID: paymentID,
				PostingID: legacyNominationText(linkedPostingID), AccountingVersion: legacyNominationNumber(1),
				RecordHash: bytes.Clone(paymentHash), ActualPaymentDate: legacyNominationText(test.paymentDate),
			}
			withPayment := append(append([]BASScalarKey(nil), keys...), paymentKey)
			got, err := nominateBASLegacyCandidates(legacyNominationTestOrg, legacyNominationPeriod(9), legacyNominationDate(9, 30),
				states, withPayment, []*tammyv1.LegacyPurchaseReview{decision}, legacyNominationEmptyCashRead(legacyNominationTestOrg), 1)
			if err != nil {
				t.Fatalf("map current modern payment support: %v", err)
			}
			row := legacyNominationBySource(t, got, documentID)
			wantCandidates := 1
			if test.wantSuppressed {
				wantCandidates = 0
			}
			if row.ProvisionalEvidenceOnly != test.wantSuppressed || len(got.Candidates) != wantCandidates {
				t.Fatalf("modern payment support suppression=%t candidates=%#v, want suppression=%t", row.ProvisionalEvidenceOnly, got.Candidates, test.wantSuppressed)
			}
		})
	}
}
