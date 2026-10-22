package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

const basSourceCoverageHashTag = "tammy/bas/source-coverage/v1"
const basSourceSnapshotHashTag = "tammy/bas/source-snapshot/v1"
const basRevisionVectorHashTag = "tammy/bas/revision-vector/v1"

var basCoverageCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,95}$`)

// BASSourceFindingTemplate deliberately has no finding ID. The command's
// existing UUIDv7 allocator supplies IDs only when it assembles final
// BasValidationFinding values.
type BASSourceFindingTemplate struct {
	Severity         tammyv1.BasFindingSeverity
	StableCode       string
	SafeMessage      string
	SourceIdentities []*tammyv1.BasSourceIdentity
}

type BASSourceCoverageResult struct {
	Coverage             []*tammyv1.BasSourceCoverage
	FindingTemplates     []BASSourceFindingTemplate
	ManifestHash         []byte
	CoveragePayloadBytes int64
}

// BuildBASSourceCoverage maps the exact frozen financial unit sets and every
// accounting fact. It consumes no repository, does no SQL, does not mutate
// either capture, and never coalesces candidates through shared-byte proof.
// Purchase raw fact bodies may already have been removed after their complete
// purchase/accounting cell proof; the accounting-owned fact capture remains
// authoritative and independent.
func BuildBASSourceCoverage(ctx context.Context, capture *BASBoundedSourceRecordCapture) (*BASSourceCoverageResult, error) {
	fail := func(err error) (*BASSourceCoverageResult, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, errors.Join(ErrSourceReservation, err)
	}
	if ctx == nil || capture == nil || !ids.IsCanonicalV7(capture.OrganisationID) ||
		capture.ConfirmedPeriod == nil || capture.ConfirmedPeriod.OrganisationId != capture.OrganisationID ||
		capture.ConfirmedPeriod.Confirmation == nil || capture.ConfirmedPeriod.Confirmation.Period == nil ||
		capture.ModernPurchaseCandidates == nil || capture.LegacyPurchaseCandidates == nil ||
		capture.Invoices == nil || capture.Purchases == nil || capture.Purchases.Capture == nil ||
		!capture.Purchases.TaxFactBodiesReplaced || capture.Accounting == nil ||
		capture.Financial == nil || capture.Financial.Invoices == nil || capture.Financial.Purchases == nil ||
		capture.PayloadReservation == nil || capture.PayloadReservation.OrganisationID != capture.OrganisationID ||
		capture.PayloadReservation.ReservedPayloadBytes < 0 ||
		capture.PayloadReservation.ReservedPayloadBytes > maximumBASSourcePayloadBytes ||
		capture.SharedFactProof == nil || capture.PurchasePreflight == nil ||
		int64(capture.SharedFactProof.Count) != capture.PurchasePreflight.TaxFactCount ||
		capture.SharedFactProof.PurchaseBytes != capture.Purchases.OriginalTaxFactPayloadBytes ||
		capture.Invoices.CandidateCount != len(capture.Invoices.Candidates) ||
		capture.Accounting.FactCount != len(capture.Accounting.Facts) ||
		(capture.Financial.Invoices.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH &&
			capture.Financial.Invoices.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) ||
		!proto.Equal(capture.Financial.Invoices.Period, capture.ConfirmedPeriod.Confirmation.Period) ||
		capture.Purchases.Capture.OrganisationID != capture.OrganisationID ||
		len(capture.Invoices.Fingerprint) != sha256.Size ||
		len(capture.Purchases.OriginalFingerprint) != sha256.Size ||
		len(capture.Accounting.Fingerprint) != sha256.Size ||
		!bytes.Equal(capture.Invoices.Fingerprint, capture.Financial.Invoices.CaptureFingerprint) ||
		!bytes.Equal(capture.Purchases.OriginalFingerprint, capture.Purchases.Capture.Fingerprint) ||
		capture.Purchases.OriginalPayloadBytes != capture.Purchases.Capture.PayloadBytes ||
		capture.Purchases.OriginalTaxFactPayloadBytes != capture.Purchases.Capture.TaxFactPayloadBytes ||
		capture.Purchases.OriginalPayloadBytes < 0 || capture.Purchases.OriginalTaxFactPayloadBytes < 0 ||
		capture.Accounting.FactCount != len(capture.AccountingFactKeys) {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := basCoverageValidateMappedUnits(ctx, capture); err != nil {
		return fail(err)
	}
	candidateCount, ok := sumBASSourceScalars(
		int64(len(capture.InvoiceCandidates)),
		int64(len(capture.ModernPurchaseCandidates.Candidates)),
		int64(len(capture.LegacyPurchaseCandidates.Candidates)),
		int64(len(capture.AccountingFactKeys)),
	)
	if !ok || candidateCount > maximumBASRecordCandidates {
		return fail(&SourceBoundError{Resource: "BAS source coverage candidates", Limit: maximumBASRecordCandidates})
	}

	result := &BASSourceCoverageResult{
		Coverage:         make([]*tammyv1.BasSourceCoverage, 0, candidateCount),
		FindingTemplates: make([]BASSourceFindingTemplate, 0),
	}
	for _, unit := range capture.Financial.Invoices.Units {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		coverage, primary, _, _, err := basCoverageInvoiceUnit(unit)
		if err != nil {
			return fail(err)
		}
		result.Coverage = append(result.Coverage, coverage)
		if basCoverageNeedsFinding(unit.Disposition, unit.Severity) {
			// The coverage row retains every lineage/evidence identity. Findings
			// link to the exact source root, then aggregate those roots in stable
			// groups of at most twenty identities.
			result.FindingTemplates = append(result.FindingTemplates, basCoverageFinding(unit.Severity,
				"INVOICE_"+unit.ReasonCode, unit.Reason, []*tammyv1.BasSourceIdentity{primary}))
		}
	}

	rows := capture.Purchases.Capture.Rows
	journalProofs, err := basCoverageIndexJournals(capture.Purchases.Capture.Journals, capture.OrganisationID)
	if err != nil {
		return fail(err)
	}
	modernUnits := len(capture.ModernPurchaseCandidates.Candidates)
	for index := 0; index < modernUnits; index++ {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		unit := capture.Financial.Purchases.Units[index]
		key := capture.ModernPurchaseCandidates.Candidates[index]
		coverage, _, _, _, err := basCoverageModernPurchaseUnit(ctx, rows, journalProofs,
			capture.Financial.Purchases, capture.OrganisationID, unit, key)
		if err != nil {
			return fail(err)
		}
		result.Coverage = append(result.Coverage, coverage)
	}
	if err := basCoveragePostingReviewFindings(ctx, rows, journalProofs, capture.Financial.Purchases.PostingReviews,
		capture.OrganisationID, &result.FindingTemplates); err != nil {
		return fail(err)
	}

	for index, nomination := range capture.LegacyPurchaseCandidates.Nominations {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		unitIndex := modernUnits + index
		unit := capture.Financial.Purchases.Units[unitIndex]
		if !unit.IsCandidate {
			// Suppressed legacy roots stay in Nominations/Units for the later
			// exact reconciliation. They are not financial coverage candidates.
			continue
		}
		coverage, primary, _, _, err := basCoverageLegacyPurchaseUnit(ctx, rows, journalProofs,
			capture.OrganisationID, capture.Purchases.Capture.LegacySources, capture.Financial.Purchases.LegacyReviews,
			unit, nomination)
		if err != nil {
			return fail(err)
		}
		result.Coverage = append(result.Coverage, coverage)
		if basCoverageNeedsFinding(coverage.Disposition, basCoverageSeverityForDisposition(coverage.Disposition)) {
			result.FindingTemplates = append(result.FindingTemplates, basCoverageFinding(
				basCoverageSeverityForDisposition(coverage.Disposition), coverage.ClassificationCode,
				coverage.Reason, []*tammyv1.BasSourceIdentity{primary}))
		}
	}
	if err := basCoverageLegacyReviewFindings(ctx, rows, capture.Financial.Purchases.LegacyReviews,
		capture.OrganisationID, &result.FindingTemplates); err != nil {
		return fail(err)
	}

	accountingJournals, err := basCoverageIndexJournals(capture.Accounting.Journals, capture.OrganisationID)
	if err != nil {
		return fail(err)
	}
	seenFacts := make(map[string]struct{}, len(capture.Accounting.Facts))
	for index, fact := range capture.Accounting.Facts {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		key := capture.AccountingFactKeys[index]
		if fact.ID == "" || fact.OrganisationID != capture.OrganisationID || key.ID != fact.ID ||
			key.OrganisationID != capture.OrganisationID || key.JournalLineID != fact.JournalLineID ||
			key.SourceType != fact.SourceType || key.SourceID != fact.SourceID || key.SourceRevision != fact.SourceRevision ||
			key.SourceContentHashPresent != fact.SourceContentHashPresent ||
			!bytes.Equal(key.SourceContentHash, fact.SourceContentHash) || fact.Journal == nil ||
			fact.Journal.OrganisationId != capture.OrganisationID || fact.SourceRevision > math.MaxInt64 {
			return fail(nil)
		}
		if _, duplicate := seenFacts[fact.ID]; duplicate {
			return fail(nil)
		}
		seenFacts[fact.ID] = struct{}{}
		proof := accountingJournals[fact.Journal.Id]
		if proof == nil || !proto.Equal(proof.Journal, fact.Journal) ||
			fact.JournalSourceType != proof.SourceType || fact.JournalSourceKind != proof.SourceKind ||
			fact.JournalSourceID != proof.SourceID || fact.JournalSourceRevision != proof.SourceRevision ||
			fact.JournalCreatedAt != proof.CreatedAt || fact.JournalState != proof.State {
			return fail(nil)
		}
		factHash, err := basCoverageTaxFactProofHash(fact)
		if err != nil {
			return fail(err)
		}
		primary, err := basCoverageRawIdentity("tax_facts", fact.ID,
			basCoverageOptionalRevision(fact.SourceRevision), basCoverageOptionalHash(fact.SourceContentHashPresent, fact.SourceContentHash),
			factHash, tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION, nil, nil, nil)
		if err != nil {
			return fail(err)
		}
		journalIdentity, err := basCoverageJournalIdentity(proof)
		if err != nil {
			return fail(err)
		}
		coverage := &tammyv1.BasSourceCoverage{
			RawSource:          primary.RawSource,
			CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
			Disposition:        tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
			EvidenceIdentities: []*tammyv1.BasSourceIdentity{journalIdentity},
			ClassificationCode: "MANUAL_TAX_FACT_UNSUPPORTED",
			Reason:             "This retained accounting tax fact has no approved immutable BAS classification; reconcile the complete fact and journal before inclusion.",
		}
		result.Coverage = append(result.Coverage, coverage)
		result.FindingTemplates = append(result.FindingTemplates, basCoverageFinding(
			tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED,
			"MANUAL_TAX_FACT_UNSUPPORTED", coverage.Reason,
			[]*tammyv1.BasSourceIdentity{primary, journalIdentity}))
	}
	if len(seenFacts) != capture.Accounting.FactCount || len(result.Coverage) != int(candidateCount) {
		return fail(nil)
	}

	manifestHash, payloadBytes, err := basCoverageManifest(ctx, result.Coverage)
	if err != nil {
		return fail(err)
	}
	if payloadBytes > maximumBASSourcePayloadBytes-capture.PayloadReservation.ReservedPayloadBytes {
		return fail(&SourceBoundError{Resource: "BAS source coverage bytes", Limit: maximumBASSourcePayloadBytes - capture.PayloadReservation.ReservedPayloadBytes})
	}
	result.FindingTemplates, err = basCoverageAggregateFindings(result.FindingTemplates)
	if err != nil || len(result.FindingTemplates) > 200 {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	result.ManifestHash = manifestHash
	result.CoveragePayloadBytes = payloadBytes
	return result, nil
}

func basCoverageManifest(ctx context.Context, rows []*tammyv1.BasSourceCoverage) ([]byte, int64, error) {
	if ctx == nil || len(rows) > maximumBASRecordCandidates {
		return nil, 0, ErrSourceReservation
	}
	h := sha256.New()
	basCoverageHashFrame(h, []byte(basSourceCoverageHashTag))
	basCoverageHashUint64(h, uint64(len(rows)))
	marshal := proto.MarshalOptions{Deterministic: true}
	var payloadBytes int64
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if err := basCoverageValidateProto(row); err != nil {
			return nil, 0, err
		}
		wire, err := marshal.Marshal(row)
		if err != nil {
			return nil, 0, err
		}
		charge, valid := basCoverageAdd(512, int64(len(wire)))
		if !valid || payloadBytes > maximumBASSourcePayloadBytes-charge {
			return nil, 0, &SourceBoundError{Resource: "BAS coverage manifest bytes", Limit: maximumBASSourcePayloadBytes}
		}
		payloadBytes += charge
		basCoverageHashFrame(h, wire)
	}
	return h.Sum(nil), payloadBytes, nil
}

func basCoverageValidateMappedUnits(ctx context.Context, capture *BASBoundedSourceRecordCapture) error {
	if len(capture.Financial.Invoices.Units) != len(capture.InvoiceCandidates) ||
		len(capture.Financial.Purchases.Units) != len(capture.ModernPurchaseCandidates.Candidates)+len(capture.LegacyPurchaseCandidates.Nominations) {
		return ErrSourceReservation
	}
	for index, key := range capture.InvoiceCandidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if capture.Financial.Invoices.Units[index].Key != key || key.OrganisationID != capture.OrganisationID ||
			!ids.IsCanonicalV7(key.InvoiceID) ||
			(key.Kind == invoices.BASCandidateInvoiceRoot && (key.ReceiptID != "" || key.Sequence != 0)) ||
			(key.Kind == invoices.BASCandidateIncomeLine && (key.ReceiptID != "" || key.Sequence == 0)) ||
			(key.Kind == invoices.BASCandidateReceipt && (!ids.IsCanonicalV7(key.ReceiptID) || key.Sequence != 0)) ||
			(key.Kind == invoices.BASCandidateReceiptAllocation && (!ids.IsCanonicalV7(key.ReceiptID) || key.Sequence == 0)) ||
			(key.Kind != invoices.BASCandidateInvoiceRoot && key.Kind != invoices.BASCandidateIncomeLine &&
				key.Kind != invoices.BASCandidateReceipt && key.Kind != invoices.BASCandidateReceiptAllocation) {
			return ErrSourceReservation
		}
	}
	for index, key := range capture.ModernPurchaseCandidates.Candidates {
		unit := capture.Financial.Purchases.Units[index]
		shapeValid := false
		switch key.Kind {
		case purchases.BASModernCandidatePosting:
			shapeValid = key.PaymentID == "" && key.Sequence == 0 && key.PostingAllocationSequence == 0
		case purchases.BASModernCandidatePostingAllocation:
			shapeValid = key.PaymentID == "" && key.Sequence > 0 && key.PostingAllocationSequence == 0
		case purchases.BASModernCandidatePayment:
			shapeValid = ids.IsCanonicalV7(key.PaymentID) && key.Sequence == 0 && key.PostingAllocationSequence == 0
		case purchases.BASModernCandidatePaymentAllocation:
			shapeValid = ids.IsCanonicalV7(key.PaymentID) && key.Sequence > 0 && key.PostingAllocationSequence > 0
		}
		if !unit.IsCandidate || unit.Candidate.Modern == nil || unit.Candidate.Legacy != nil ||
			*unit.Candidate.Modern != key || key.OrganisationID != capture.OrganisationID ||
			!ids.IsCanonicalV7(key.PostingID) || !ids.IsCanonicalV7(key.DocumentID) || !shapeValid {
			return ErrSourceReservation
		}
	}
	actualCandidates := make(map[purchases.BASLegacyCandidateKey]struct{}, len(capture.LegacyPurchaseCandidates.Candidates))
	for _, key := range capture.LegacyPurchaseCandidates.Candidates {
		if key.OrganisationID != capture.OrganisationID || !ids.IsCanonicalV7(key.SourceID) ||
			(key.Kind == purchases.BASLegacyCandidateDocument && key.ConversionID != "") ||
			(key.Kind == purchases.BASLegacyCandidateOpeningPayable && !ids.IsCanonicalV7(key.ConversionID)) ||
			(key.Kind != purchases.BASLegacyCandidateDocument && key.Kind != purchases.BASLegacyCandidateOpeningPayable) {
			return ErrSourceReservation
		}
		if _, duplicate := actualCandidates[key]; duplicate {
			return ErrSourceReservation
		}
		actualCandidates[key] = struct{}{}
	}
	seenNominations := make(map[purchases.BASLegacyCandidateKey]struct{}, len(capture.LegacyPurchaseCandidates.Nominations))
	for index, nomination := range capture.LegacyPurchaseCandidates.Nominations {
		if err := ctx.Err(); err != nil {
			return err
		}
		unit := capture.Financial.Purchases.Units[len(capture.ModernPurchaseCandidates.Candidates)+index]
		if unit.Candidate.Legacy == nil || unit.Candidate.Modern != nil || *unit.Candidate.Legacy != nomination.Key ||
			nomination.Key.OrganisationID != capture.OrganisationID || !ids.IsCanonicalV7(nomination.Key.SourceID) ||
			(nomination.Key.Kind == purchases.BASLegacyCandidateDocument && nomination.Key.ConversionID != "") ||
			(nomination.Key.Kind == purchases.BASLegacyCandidateOpeningPayable && !ids.IsCanonicalV7(nomination.Key.ConversionID)) ||
			(nomination.Key.Kind != purchases.BASLegacyCandidateDocument && nomination.Key.Kind != purchases.BASLegacyCandidateOpeningPayable) {
			return ErrSourceReservation
		}
		if _, duplicate := seenNominations[nomination.Key]; duplicate {
			return ErrSourceReservation
		}
		seenNominations[nomination.Key] = struct{}{}
		_, wanted := actualCandidates[nomination.Key]
		if wanted != unit.IsCandidate {
			return ErrSourceReservation
		}
		if unit.CandidateGST < 0 || unit.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR &&
			!unit.Excluded && !unit.EvidenceOnly && unit.CandidateGST > 0 {
			return ErrSourceReservation
		}
	}
	if len(seenNominations) != len(capture.LegacyPurchaseCandidates.Nominations) || len(actualCandidates) > len(seenNominations) {
		return ErrSourceReservation
	}
	return nil
}

func basCoverageInvoiceUnit(unit BASInvoiceFinancialUnit) (*tammyv1.BasSourceCoverage, *tammyv1.BasSourceIdentity, []*tammyv1.BasSourceIdentity, []*tammyv1.BasSourceIdentity, error) {
	if unit.ReasonCode == "" || !basCoverageCodePattern.MatchString(unit.ReasonCode) || unit.Reason == "" ||
		len(unit.References) == 0 || !basCoverageDispositionValid(unit.Disposition) {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	primaryIndex := -1
	for index, reference := range unit.References {
		matches := false
		switch unit.Key.Kind {
		case invoices.BASCandidateInvoiceRoot:
			matches = reference.Table == "invoices_v1" && reference.ID == unit.Key.InvoiceID
		case invoices.BASCandidateIncomeLine:
			matches = reference.Table == "invoice_income_lines_v1" && reference.ID == unit.Key.InvoiceID && reference.HasSequence && reference.ComponentSequence == unit.Key.Sequence
		case invoices.BASCandidateReceipt:
			matches = reference.Table == "invoice_payment_v1" && reference.ID == unit.Key.ReceiptID && reference.ParentID == unit.Key.InvoiceID
		case invoices.BASCandidateReceiptAllocation:
			matches = reference.Table == "invoice_payment_lines_v1" && reference.ID == unit.Key.ReceiptID && reference.ParentID == unit.Key.InvoiceID && reference.HasSequence && reference.ComponentSequence == unit.Key.Sequence
		default:
			return nil, nil, nil, nil, ErrSourceReservation
		}
		if matches {
			if primaryIndex >= 0 {
				return nil, nil, nil, nil, ErrSourceReservation
			}
			primaryIndex = index
		}
	}
	if primaryIndex < 0 {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	primary, err := basCoverageInvoiceIdentity(unit.References[primaryIndex])
	if err != nil {
		return nil, nil, nil, nil, err
	}
	lineage, evidence := make([]*tammyv1.BasSourceIdentity, 0, len(unit.References)-1), make([]*tammyv1.BasSourceIdentity, 0)
	for index, reference := range unit.References {
		if index == primaryIndex {
			continue
		}
		identity, err := basCoverageInvoiceIdentity(reference)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if reference.Table == "journals" {
			evidence = append(evidence, identity)
		} else {
			lineage = append(lineage, identity)
		}
	}
	if len(lineage) > basSourceIdentityMaximum || len(evidence) > basSourceIdentityMaximum {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	coverage := &tammyv1.BasSourceCoverage{
		RawSource:          primary.RawSource,
		CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_INVOICE_POSTING,
		Disposition:        unit.Disposition,
		TaxDate:            basCoverageCloneCalendarDate(unit.TaxDate),
		LineageIdentities:  lineage,
		EvidenceIdentities: evidence,
		ClassificationCode: unit.ReasonCode,
		Reason:             unit.Reason,
	}
	// Invoice coverage records the GST effect. Gross sales are retained
	// separately in the exact cent workpaper and its complete source lines.
	if unit.Disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE &&
		(unit.GrossMinor < 0 || unit.GSTMinor < 0) {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	if unit.Disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE {
		coverage.EligibleAmount = &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: unit.GSTMinor}
	}
	return coverage, primary, lineage, evidence, nil
}

func basCoverageInvoiceIdentity(reference BASInvoiceProofReference) (*tammyv1.BasSourceIdentity, error) {
	if reference.Table == "" || reference.ID == "" || !reference.SourceHashPresent && len(reference.SourceHash) != 0 ||
		(reference.SourceHashPresent && len(reference.SourceHash) != sha256.Size) ||
		(reference.HasSourceRevision && reference.SourceRevision > math.MaxInt64) {
		return nil, ErrSourceReservation
	}
	proofHash := bytes.Clone(reference.ComputedProofHash)
	scope, ok := basCoverageInvoiceScope(reference.ProofScope)
	if !ok {
		return nil, ErrSourceReservation
	}
	if len(proofHash) != sha256.Size {
		if scope != tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION {
			return nil, ErrSourceReservation
		}
		proofHash = basCoverageReferenceOnlyHash(reference)
	}
	var sequence *uint64
	if reference.HasSequence {
		value := reference.ComponentSequence
		sequence = &value
	}
	if reference.ParentID != "" {
		parent := reference.ParentID
		var revision *int64
		if reference.HasSourceRevision {
			if reference.SourceRevision > math.MaxInt64 {
				return nil, ErrSourceReservation
			}
			value := int64(reference.SourceRevision)
			revision = &value
		}
		return basCoverageRawIdentity(reference.Table, reference.ID,
			revision, basCoverageOptionalHash(reference.SourceHashPresent, reference.SourceHash), proofHash, scope, sequence, &parent, nil)
	}
	var revision *int64
	if reference.HasSourceRevision {
		if reference.SourceRevision > math.MaxInt64 {
			return nil, ErrSourceReservation
		}
		value := int64(reference.SourceRevision)
		revision = &value
	}
	return basCoverageRawIdentity(reference.Table, reference.ID,
		revision, basCoverageOptionalHash(reference.SourceHashPresent, reference.SourceHash), proofHash, scope, sequence, nil, nil)
}

func basCoverageInvoiceScope(value string) (tammyv1.BasSourceProofScope, bool) {
	switch value {
	case basInvoiceProofScopeProto, basInvoiceProofScopePayment, basInvoiceProofScopeIncomeLine, basInvoiceProofScopeAllocation:
		return tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION, true
	case basInvoiceProofScopeJournal:
		return tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_COMPLETE_JOURNAL, true
	case "":
		return tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION, true
	default:
		return 0, false
	}
}

func basCoverageReferenceOnlyHash(reference BASInvoiceProofReference) []byte {
	h := sha256.New()
	basCoverageHashFrame(h, []byte("tammy/bas/invoice-reference-only/v1"))
	basCoverageHashFrame(h, []byte(reference.Table))
	basCoverageHashFrame(h, []byte(reference.ID))
	basCoverageHashFrame(h, []byte(reference.ParentID))
	basCoverageHashBool(h, reference.HasSourceRevision)
	if reference.HasSourceRevision {
		basCoverageHashUint64(h, reference.SourceRevision)
	}
	basCoverageHashBool(h, reference.SourceHashPresent)
	if reference.SourceHashPresent {
		basCoverageHashFrame(h, reference.SourceHash)
	}
	basCoverageHashBool(h, reference.HasSequence)
	if reference.HasSequence {
		basCoverageHashUint64(h, reference.ComponentSequence)
	}
	return h.Sum(nil)
}

func basCoverageModernPurchaseUnit(ctx context.Context, rows []purchases.PurchaseReportingRetainedRow,
	journals map[string]*accounting.JournalReportingRecord, financial *purchases.BASPurchaseFinancialMap, organisationID string,
	unit purchases.BASPurchaseFinancialUnit, key purchases.BASModernCandidateKey,
) (*tammyv1.BasSourceCoverage, *tammyv1.BasSourceIdentity, []*tammyv1.BasSourceIdentity, []*tammyv1.BasSourceIdentity, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	if !unit.IsCandidate || unit.Candidate.Modern == nil || *unit.Candidate.Modern != key || key.OrganisationID != organisationID ||
		key.PostingID == "" || key.DocumentID == "" ||
		(key.Kind == purchases.BASModernCandidatePayment || key.Kind == purchases.BASModernCandidatePaymentAllocation) && key.PaymentID == "" ||
		(key.Kind == purchases.BASModernCandidatePostingAllocation || key.Kind == purchases.BASModernCandidatePaymentAllocation) && key.Sequence == 0 ||
		key.Kind == purchases.BASModernCandidatePaymentAllocation && key.PostingAllocationSequence == 0 {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	table, idColumn, id, sequenceColumn := "", "", "", ""
	var sequence uint64
	var relatedID *string
	var relatedSequence *uint64
	switch key.Kind {
	case purchases.BASModernCandidatePosting:
		table, idColumn, id = "purchase_postings_v1", "id", key.PostingID
	case purchases.BASModernCandidatePostingAllocation:
		table, idColumn, id, sequenceColumn, sequence = "purchase_posting_allocations_v1", "posting_id", key.PostingID, "sequence", key.Sequence
	case purchases.BASModernCandidatePayment:
		table, idColumn, id = "purchase_payments_v1", "id", key.PaymentID
		relatedID = &key.PostingID
	case purchases.BASModernCandidatePaymentAllocation:
		table, idColumn, id, sequenceColumn, sequence = "purchase_payment_allocations_v1", "payment_id", key.PaymentID, "sequence", key.Sequence
		relatedID = &key.PostingID
		relatedSequence = &key.PostingAllocationSequence
	default:
		return nil, nil, nil, nil, ErrSourceReservation
	}
	row, err := basCoverageFindRow(rows, table, idColumn, id, sequenceColumn, sequence, organisationID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if row == nil || basCoverageValidateModernPrimaryRow(*row, key, organisationID) != nil {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	postingRoot, err := basCoverageFindRow(rows, "purchase_postings_v1", "id", key.PostingID, "", 0, organisationID)
	if err != nil || postingRoot == nil || basCoverageText(*postingRoot, "document_id") != key.DocumentID {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	if _, err := basCoverageFindRow(rows, "documents", "id", key.DocumentID, "", 0, organisationID); err != nil {
		return nil, nil, nil, nil, err
	}
	var component *uint64
	if sequenceColumn != "" {
		component = &sequence
	}
	primary, err := basCoverageRowIdentity(*row, id, component, relatedID, relatedSequence)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	lineage, evidence := make([]*tammyv1.BasSourceIdentity, 0, 4), make([]*tammyv1.BasSourceIdentity, 0, 4)
	addRow := func(table, idColumn, id string, sequenceColumn string, sequence uint64, category string) error {
		if table == primary.RawSource.Type && id == primary.RawSource.Id && sequenceColumn == "" {
			return nil
		}
		candidate, findErr := basCoverageFindRow(rows, table, idColumn, id, sequenceColumn, sequence, organisationID)
		if findErr != nil {
			return findErr
		}
		var component *uint64
		if sequenceColumn != "" {
			value := sequence
			component = &value
		}
		identity, identityErr := basCoverageRowIdentity(*candidate, id, component, nil, nil)
		if identityErr != nil {
			return identityErr
		}
		if category == "evidence" {
			evidence = append(evidence, identity)
		} else {
			lineage = append(lineage, identity)
		}
		return nil
	}
	if key.Kind != purchases.BASModernCandidatePosting {
		if err := addRow("purchase_postings_v1", "id", key.PostingID, "", 0, "lineage"); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	if key.Kind == purchases.BASModernCandidatePayment || key.Kind == purchases.BASModernCandidatePaymentAllocation {
		if key.Kind != purchases.BASModernCandidatePayment {
			if err := addRow("purchase_payments_v1", "id", key.PaymentID, "", 0, "lineage"); err != nil {
				return nil, nil, nil, nil, err
			}
		}
	}
	if key.Kind == purchases.BASModernCandidatePosting || key.Kind == purchases.BASModernCandidatePostingAllocation ||
		key.Kind == purchases.BASModernCandidatePayment || key.Kind == purchases.BASModernCandidatePaymentAllocation {
		if err := addRow("documents", "id", key.DocumentID, "", 0, "lineage"); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	lines := make([]*tammyv1.BasPurchaseSourceLine, 0, 1)
	if unit.Source != nil {
		lines = append(lines, unit.Source)
	}
	attributions, err := findBASPurchaseAttributions(ctx, financial, key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, attribution := range attributions {
		lines = append(lines, attribution)
	}
	amount := int64(0)
	var taxDate *tammyv1.CivilDate
	taxDateComplete := len(lines) > 0
	for _, line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, err
		}
		if line == nil || line.ClaimedGst == nil || line.ClaimedGst.CurrencyCode != "AUD" || line.ClaimedGst.MinorUnits < 0 ||
			line.PostingId != key.PostingID || line.DocumentId != key.DocumentID {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		if err := basCoverageValidatePurchaseLineRowLinks(rows, *line, key, organisationID); err != nil {
			return nil, nil, nil, nil, err
		}
		if amount > math.MaxInt64-line.ClaimedGst.MinorUnits {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		amount += line.ClaimedGst.MinorUnits
		if line.NormalTaxEventDate != nil && validCalendarDate(line.NormalTaxEventDate) {
			if taxDate == nil {
				taxDate = basCoverageCloneCalendarDate(line.NormalTaxEventDate)
			} else if !proto.Equal(taxDate, line.NormalTaxEventDate) {
				taxDateComplete = false
			}
		} else {
			taxDateComplete = false
		}
		if line.Evidence != nil {
			if err := addRow("purchase_tax_evidence_v1", "id", line.Evidence.SourceId, "", 0, "evidence"); err != nil {
				return nil, nil, nil, nil, err
			}
		}
		if line.AttributionId != nil {
			if err := addRow("purchase_gst_attributions_v1", "id", *line.AttributionId, "", 0, "evidence"); err != nil {
				return nil, nil, nil, nil, err
			}
		}
	}
	if len(lines) == 0 || !taxDateComplete {
		taxDate = nil
	}
	disposition, classification, reason := basCoverageModernDisposition(len(lines), amount)
	candidateKind := tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_PURCHASE_CREDIT
	if key.Kind == purchases.BASModernCandidatePayment || key.Kind == purchases.BASModernCandidatePaymentAllocation {
		candidateKind = tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_PURCHASE_SETTLEMENT
	}
	coverage := &tammyv1.BasSourceCoverage{
		RawSource:          primary.RawSource,
		CandidateKind:      candidateKind,
		Disposition:        disposition,
		TaxDate:            taxDate,
		LineageIdentities:  basCoverageDedupeIdentities(lineage),
		EvidenceIdentities: basCoverageDedupeIdentities(evidence),
		ClassificationCode: classification,
		Reason:             reason,
	}
	if disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE {
		coverage.EligibleAmount = &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: amount}
	}
	if len(coverage.LineageIdentities) > basSourceIdentityMaximum || len(coverage.EvidenceIdentities) > basSourceIdentityMaximum {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	for _, journalID := range basCoverageJournalIDsForModernKey(rows, key) {
		proof := journals[journalID]
		if proof == nil {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		identity, identityErr := basCoverageJournalIdentity(proof)
		if identityErr != nil {
			return nil, nil, nil, nil, identityErr
		}
		coverage.EvidenceIdentities = basCoverageAppendUnique(coverage.EvidenceIdentities, identity)
	}
	if len(coverage.EvidenceIdentities) > basSourceIdentityMaximum {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	return coverage, primary, coverage.LineageIdentities, coverage.EvidenceIdentities, nil
}

func findBASPurchaseAttributions(ctx context.Context, financial *purchases.BASPurchaseFinancialMap, key purchases.BASModernCandidateKey) ([]*tammyv1.BasPurchaseSourceLine, error) {
	if ctx == nil || financial == nil {
		return nil, ErrSourceReservation
	}
	result := make([]*tammyv1.BasPurchaseSourceLine, 0)
	for _, attribution := range financial.Attributions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attribution.Candidate == key {
			if attribution.Source == nil {
				return nil, ErrSourceReservation
			}
			result = append(result, attribution.Source)
		}
	}
	return result, nil
}

func basCoverageModernDisposition(lineCount int, amount int64) (tammyv1.BasCoverageDisposition, string, string) {
	if lineCount == 0 {
		return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE, "NO_CURRENT_EFFECT_LINE", "The frozen purchase component has no current-period source line in the purchase mapper."
	}
	if amount > 0 {
		return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE, "CURRENT_PURCHASE_GST", "The purchase mapper retained this exact component as a current-period GST amount."
	}
	return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO, "CURRENT_PURCHASE_ZERO", "The purchase mapper retained this exact current-period component with zero claimed GST."
}

func basCoverageLegacyPurchaseUnit(ctx context.Context, rows []purchases.PurchaseReportingRetainedRow,
	journals map[string]*accounting.JournalReportingRecord, organisationID string,
	legacySources []*purchases.LegacyPurchaseReportingRecord, legacyReviews []purchases.BASPurchaseLegacyReview,
	unit purchases.BASPurchaseFinancialUnit, nomination purchases.BASLegacyNomination,
) (*tammyv1.BasSourceCoverage, *tammyv1.BasSourceIdentity, []*tammyv1.BasSourceIdentity, []*tammyv1.BasSourceIdentity, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	key := nomination.Key
	if !unit.IsCandidate || unit.Candidate.Legacy == nil || *unit.Candidate.Legacy != key || key.OrganisationID != organisationID ||
		!ids.IsCanonicalV7(key.SourceID) ||
		(key.Kind == purchases.BASLegacyCandidateOpeningPayable && !ids.IsCanonicalV7(key.ConversionID)) ||
		(key.Kind == purchases.BASLegacyCandidateDocument && key.ConversionID != "") ||
		(key.Kind != purchases.BASLegacyCandidateDocument && key.Kind != purchases.BASLegacyCandidateOpeningPayable) {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	legacyRecord, err := basCoverageFindLegacyRecord(ctx, legacySources, key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var primary *tammyv1.BasSourceIdentity
	var lineage, evidence []*tammyv1.BasSourceIdentity
	switch key.Kind {
	case purchases.BASLegacyCandidateDocument:
		row, err := basCoverageFindRow(rows, "documents", "id", key.SourceID, "", 0, organisationID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if legacyRecord.Document == nil || legacyRecord.Document.Id != key.SourceID || legacyRecord.Document.OrganisationId != organisationID ||
			legacyRecord.Document.Version != nomination.SourceVersion || !bytes.Equal(legacyRecord.Document.Sha256, nomination.DocumentRawSHA256) ||
			basCoverageInteger(*row, "version") != int64(nomination.SourceVersion) ||
			!basCoverageBlobEqual(*row, "sha256", nomination.DocumentRawSHA256) {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		primary, err = basCoverageRowIdentity(*row, key.SourceID, nil, nil, nil)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	case purchases.BASLegacyCandidateOpeningPayable:
		row, err := basCoverageFindOpeningPayable(rows, key.SourceID, key.ConversionID)
		if err != nil || row == nil || legacyRecord.Opening == nil || legacyRecord.Opening.ID != key.SourceID ||
			legacyRecord.Opening.OrganisationID != organisationID || legacyRecord.Opening.ConversionID != key.ConversionID ||
			legacyRecord.Opening.SourceVersion != nomination.SourceVersion || nomination.SourceVersion == 0 ||
			len(legacyRecord.Opening.SourceHash) != sha256.Size {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		retainedInput, present := basCoverageRowCell(*row, "retained_input_proto")
		if !present || retainedInput.Kind != "BLOB" || retainedInput.Bytes == nil {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		inputHash := sha256.Sum256(retainedInput.Bytes)
		if !bytes.Equal(inputHash[:], legacyRecord.Opening.SourceHash) {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		conversion, err := basCoverageFindRow(rows, "opening_conversions", "id", key.ConversionID, "", 0, organisationID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if basCoverageInteger(*conversion, "version") != int64(nomination.SourceVersion) ||
			basCoverageText(*conversion, "organisation_id") != organisationID ||
			basCoverageText(*conversion, "state") != legacyRecord.Opening.State {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		primaryHash, err := basCoverageRowProofHash(*row)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		revision := int64(legacyRecord.Opening.SourceVersion)
		primary, err = basCoverageRawIdentity("purchase_opening_payables", key.SourceID, &revision,
			legacyRecord.Opening.SourceHash, primaryHash,
			tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION, nil,
			&key.ConversionID, nil)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		identity, err := basCoverageRowIdentity(*conversion, key.ConversionID, nil, nil, nil)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if legacyRecord.Opening == nil || len(legacyRecord.Opening.ConversionHash) != sha256.Size ||
			!bytes.Equal(identity.RawSource.OriginalContentHash, legacyRecord.Opening.ConversionHash) {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		lineage = append(lineage, identity)
		item, err := basCoverageFindOpeningItem(rows, key.SourceID, key.ConversionID)
		if err != nil || item == nil || legacyRecord.Opening.Input == nil ||
			basCoverageText(*item, "account_id") != legacyRecord.Opening.Input.AccountId {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		itemIdentity, err := basCoverageRowIdentity(*item, key.SourceID, nil, &key.ConversionID, nil)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		lineage = append(lineage, itemIdentity)
		account, err := basCoverageFindRow(rows, "accounts", "id", legacyRecord.Opening.Input.AccountId, "", 0, organisationID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		accountIdentity, err := basCoverageRowIdentity(*account, legacyRecord.Opening.Input.AccountId, nil, &key.SourceID, nil)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		lineage = append(lineage, accountIdentity)
	default:
		return nil, nil, nil, nil, ErrSourceReservation
	}
	disposition, classification, reason := basCoverageLegacyDisposition(unit)
	coverage := &tammyv1.BasSourceCoverage{
		RawSource:          primary.RawSource,
		CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_OPENING_OR_LEGACY_FACT,
		Disposition:        disposition,
		LineageIdentities:  lineage,
		ClassificationCode: classification,
		Reason:             reason,
	}
	if disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE {
		if unit.CandidateGST < 0 {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		coverage.EligibleAmount = &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: unit.CandidateGST}
	}
	for _, journalID := range append(append([]string(nil), legacyRecord.PriorJournalIDs...), legacyRecord.PaymentJournalIDs...) {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, err
		}
		proof := journals[journalID]
		if journalID == "" || proof == nil || proof.Journal == nil || proof.Journal.OrganisationId != organisationID {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		identity, identityErr := basCoverageJournalIdentity(proof)
		if identityErr != nil {
			return nil, nil, nil, nil, identityErr
		}
		evidence = basCoverageAppendUnique(evidence, identity)
	}
	if key.Kind == purchases.BASLegacyCandidateOpeningPayable {
		conversion, findErr := basCoverageFindRow(rows, "opening_conversions", "id", key.ConversionID, "", 0, organisationID)
		if findErr != nil || conversion == nil {
			return nil, nil, nil, nil, ErrSourceReservation
		}
		if journalID := basCoverageText(*conversion, "journal_id"); journalID != "" {
			proof := journals[journalID]
			if proof == nil {
				return nil, nil, nil, nil, ErrSourceReservation
			}
			identity, identityErr := basCoverageJournalIdentity(proof)
			if identityErr != nil {
				return nil, nil, nil, nil, identityErr
			}
			evidence = basCoverageAppendUnique(evidence, identity)
		}
	}
	for _, review := range legacyReviews {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, err
		}
		if review.Candidate != key || review.Review == nil || review.Review.LegacyReview == nil {
			continue
		}
		if identity, identityErr := basCoverageLegacyDecisionIdentity(rows, review.Review.LegacyReview, key, organisationID); identityErr != nil {
			return nil, nil, nil, nil, identityErr
		} else {
			evidence = basCoverageAppendUnique(evidence, identity)
		}
	}
	evidence = basCoverageDedupeIdentities(evidence)
	if len(lineage) > basSourceIdentityMaximum || len(evidence) > basSourceIdentityMaximum {
		return nil, nil, nil, nil, ErrSourceReservation
	}
	coverage.EvidenceIdentities = evidence
	return coverage, primary, lineage, evidence, nil
}

func basCoverageFindLegacyRecord(ctx context.Context, records []*purchases.LegacyPurchaseReportingRecord,
	key purchases.BASLegacyCandidateKey,
) (*purchases.LegacyPurchaseReportingRecord, error) {
	var found *purchases.LegacyPurchaseReportingRecord
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if record == nil || !record.CurrentRoot {
			continue
		}
		matches := false
		switch key.Kind {
		case purchases.BASLegacyCandidateDocument:
			matches = record.Document != nil && record.Opening == nil && record.Document.Id == key.SourceID &&
				record.Document.OrganisationId == key.OrganisationID && record.Document.Version > 0 &&
				len(record.Document.Sha256) == sha256.Size && key.ConversionID == ""
		case purchases.BASLegacyCandidateOpeningPayable:
			matches = record.Opening != nil && record.Document == nil && record.Opening.ID == key.SourceID &&
				record.Opening.OrganisationID == key.OrganisationID && record.Opening.ConversionID == key.ConversionID &&
				record.Opening.SourceVersion > 0 && len(record.Opening.SourceHash) == sha256.Size
		}
		if matches {
			if found != nil {
				return nil, ErrSourceReservation
			}
			found = record
		}
	}
	if found == nil {
		return nil, ErrSourceReservation
	}
	return found, nil
}

func basCoverageLegacyDecisionIdentity(rows []purchases.PurchaseReportingRetainedRow,
	decision *tammyv1.BasPurchaseLegacyReview, key purchases.BASLegacyCandidateKey, organisationID string,
) (*tammyv1.BasSourceIdentity, error) {
	if decision == nil || decision.Id == "" || decision.OrganisationId != organisationID ||
		decision.SourceRevision == 0 || decision.SourceRevision > math.MaxInt64 || len(decision.SourceHash) != sha256.Size {
		return nil, ErrSourceReservation
	}
	kind, sourceID, sourceVersion, sourceHash, reviewedHash, err := basCoverageLegacySourceScope(decision.Source)
	if err != nil || sourceID != key.SourceID ||
		(kind == "DOCUMENT" && (key.Kind != purchases.BASLegacyCandidateDocument || key.ConversionID != "")) ||
		(kind == "OPENING_PAYABLE" && key.Kind != purchases.BASLegacyCandidateOpeningPayable) ||
		(kind != "DOCUMENT" && kind != "OPENING_PAYABLE") {
		return nil, ErrSourceReservation
	}
	table := "documents"
	if kind == "OPENING_PAYABLE" {
		if decision.Source.GetOpeningPayable().OpeningPayableId != key.SourceID || key.ConversionID == "" {
			return nil, ErrSourceReservation
		}
		table = "purchase_opening_payables"
	} else if decision.Source.GetDocument().DocumentId != key.SourceID {
		return nil, ErrSourceReservation
	}
	row, findErr := basCoverageFindRow(rows, "purchase_legacy_reviews_v1", "id", decision.Id, "", 0, organisationID)
	if findErr != nil || row == nil {
		return nil, ErrSourceReservation
	}
	var root *purchases.PurchaseReportingRetainedRow
	if kind == "DOCUMENT" {
		root, findErr = basCoverageFindRow(rows, table, "id", key.SourceID, "", 0, organisationID)
	} else {
		root, findErr = basCoverageFindOpeningPayable(rows, key.SourceID, key.ConversionID)
	}
	if findErr != nil || root == nil {
		return nil, ErrSourceReservation
	}
	wantReviewedHashPresent := kind == "DOCUMENT"
	if basCoverageText(*row, "source_kind") != kind ||
		basCoverageText(*row, "document_id") != map[bool]string{true: sourceID, false: ""}[kind == "DOCUMENT"] ||
		basCoverageText(*row, "opening_payable_id") != map[bool]string{true: sourceID, false: ""}[kind == "OPENING_PAYABLE"] ||
		basCoverageInteger(*row, "source_version") != int64(sourceVersion) ||
		basCoverageInteger(*row, "accounting_version") != int64(decision.SourceRevision) ||
		!basCoverageBlobEqual(*row, "source_hash", sourceHash) ||
		!basCoverageNullableBlobEqual(*row, "reviewed_snapshot_hash", reviewedHash, wantReviewedHashPresent) ||
		!basCoverageBlobEqual(*row, "record_hash", decision.SourceHash) {
		return nil, ErrSourceReservation
	}
	if kind == "OPENING_PAYABLE" && basCoverageText(*root, "conversion_id") != key.ConversionID {
		return nil, ErrSourceReservation
	}
	return basCoverageRowIdentity(*row, decision.Id, nil, nil, nil)
}

func basCoverageLegacyPrimaryIdentity(rows []purchases.PurchaseReportingRetainedRow,
	key purchases.BASLegacyCandidateKey, source *tammyv1.LegacyPurchaseSourceRef, organisationID string,
) (*tammyv1.BasSourceIdentity, error) {
	kind, sourceID, sourceVersion, sourceHash, _, err := basCoverageLegacySourceScope(source)
	if err != nil || sourceID != key.SourceID ||
		(kind == "DOCUMENT" && (key.Kind != purchases.BASLegacyCandidateDocument || key.ConversionID != "")) ||
		(kind == "OPENING_PAYABLE" && (key.Kind != purchases.BASLegacyCandidateOpeningPayable || !ids.IsCanonicalV7(key.ConversionID))) {
		return nil, ErrSourceReservation
	}
	if kind == "DOCUMENT" {
		row, findErr := basCoverageFindRow(rows, "documents", "id", sourceID, "", 0, organisationID)
		if findErr != nil || row == nil || basCoverageInteger(*row, "version") != int64(sourceVersion) ||
			!basCoverageBlobEqual(*row, "sha256", sourceHash) {
			return nil, ErrSourceReservation
		}
		return basCoverageRowIdentity(*row, sourceID, nil, nil, nil)
	}
	row, findErr := basCoverageFindOpeningPayable(rows, sourceID, key.ConversionID)
	if findErr != nil || row == nil {
		return nil, ErrSourceReservation
	}
	conversion, findErr := basCoverageFindRow(rows, "opening_conversions", "id", key.ConversionID, "", 0, organisationID)
	if findErr != nil || conversion == nil || basCoverageInteger(*conversion, "version") != int64(sourceVersion) {
		return nil, ErrSourceReservation
	}
	retainedInput, present := basCoverageRowCell(*row, "retained_input_proto")
	if !present || retainedInput.Kind != "BLOB" || retainedInput.Bytes == nil {
		return nil, ErrSourceReservation
	}
	inputHash := sha256.Sum256(retainedInput.Bytes)
	if !bytes.Equal(inputHash[:], sourceHash) {
		return nil, ErrSourceReservation
	}
	proofHash, err := basCoverageRowProofHash(*row)
	if err != nil {
		return nil, err
	}
	revision := int64(sourceVersion)
	return basCoverageRawIdentity("purchase_opening_payables", sourceID, &revision, sourceHash, proofHash,
		tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION, nil, &key.ConversionID, nil)
}

func basCoverageLegacyDisposition(unit purchases.BASPurchaseFinancialUnit) (tammyv1.BasCoverageDisposition, string, string) {
	switch {
	case unit.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED:
		return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED, "LEGACY_SETTLEMENT_UNSUPPORTED", "The retained legacy source has settlement or ownership facts outside supported automatic treatment."
	case unit.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY || unit.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSPECIFIED:
		return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED, "LEGACY_REVIEW_REQUIRED", "The legacy source requires a retained, applicable decision before its GST treatment can be resolved."
	case unit.Excluded || unit.EvidenceOnly:
		return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON, "LEGACY_EXCLUDED_BY_RETAINED_DECISION", "The exact retained legacy decision excludes an additional purchase credit in this scope."
	case unit.CandidateGST > 0:
		return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED, "LEGACY_REVIEW_REQUIRED", "This legacy candidate amount is diagnostic and has no eligible current-period claim."
	default:
		return tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO, "LEGACY_REVIEWED_ZERO", "The mature purchase mapper retained this exact legacy candidate with zero GST."
	}
}

func basCoveragePostingReviewFindings(ctx context.Context, rows []purchases.PurchaseReportingRetainedRow,
	journals map[string]*accounting.JournalReportingRecord, reviews []purchases.BASPurchasePostingReview,
	organisationID string, out *[]BASSourceFindingTemplate,
) error {
	for _, postingReview := range reviews {
		if err := ctx.Err(); err != nil {
			return err
		}
		if postingReview.Review == nil || postingReview.PostingID == "" || postingReview.Review.PostingId == nil ||
			*postingReview.Review.PostingId != postingReview.PostingID {
			return ErrSourceReservation
		}
		if postingReview.Review.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
			continue
		}
		row, err := basCoverageFindRow(rows, "purchase_postings_v1", "id", postingReview.PostingID, "", 0, organisationID)
		if err != nil {
			return err
		}
		root, err := basCoverageRowIdentity(*row, postingReview.PostingID, nil, nil, nil)
		if err != nil {
			return err
		}
		if postingReview.Review.DocumentId != nil && *postingReview.Review.DocumentId != basCoverageText(*row, "document_id") {
			return ErrSourceReservation
		}
		refs := []*tammyv1.BasSourceIdentity{root}
		if postingReview.Review.Evidence != nil {
			evidence, findErr := basCoverageFindRow(rows, "purchase_tax_evidence_v1", "id", postingReview.Review.Evidence.SourceId, "", 0, organisationID)
			if findErr != nil {
				return findErr
			}
			if evidence == nil || basCoverageText(*evidence, "posting_id") != postingReview.PostingID ||
				postingReview.Review.Evidence.SourceVersion == 0 ||
				basCoverageInteger(*evidence, "accounting_version") != int64(postingReview.Review.Evidence.SourceVersion) ||
				!basCoverageBlobEqual(*evidence, "record_hash", postingReview.Review.Evidence.SourceHash) {
				return ErrSourceReservation
			}
			identity, identityErr := basCoverageRowIdentity(*evidence, postingReview.Review.Evidence.SourceId, nil, nil, nil)
			if identityErr != nil {
				return identityErr
			}
			refs = append(refs, identity)
		}
		if obligation := postingReview.Review.FutureClaimObligation; obligation != nil {
			if obligation.DeferralId == "" || obligation.PostingId != postingReview.PostingID ||
				obligation.OrganisationId != organisationID || obligation.AccountingVersion == 0 ||
				len(obligation.DeferralHash) != sha256.Size {
				return ErrSourceReservation
			}
			deferral, findErr := basCoverageFindRow(rows, "purchase_gst_deferrals_v1", "id", obligation.DeferralId, "", 0, organisationID)
			if findErr != nil || deferral == nil || basCoverageText(*deferral, "posting_id") != postingReview.PostingID ||
				basCoverageText(*deferral, "document_id") != obligation.DocumentId ||
				basCoverageInteger(*deferral, "accounting_version") != int64(obligation.AccountingVersion) ||
				!basCoverageBlobEqual(*deferral, "source_hash", obligation.DeferralHash) {
				return ErrSourceReservation
			}
			identity, identityErr := basCoverageRowIdentity(*deferral, obligation.DeferralId, nil, nil, nil)
			if identityErr != nil {
				return identityErr
			}
			refs = append(refs, identity)
		}
		for _, journalID := range basCoverageJournalIDsForPostingReview(*row, postingReview.Review) {
			proof := journals[journalID]
			if proof == nil {
				return ErrSourceReservation
			}
			identity, identityErr := basCoverageJournalIdentity(proof)
			if identityErr != nil {
				return identityErr
			}
			refs = append(refs, identity)
		}
		severity := basCoverageSeverityForPurchaseStatus(postingReview.Review.Status)
		code := basCoveragePurchaseReviewCode(postingReview.Review)
		if !basCoverageCodePattern.MatchString(code) {
			return ErrSourceReservation
		}
		*out = append(*out, basCoverageFinding(severity, code,
			"A retained purchase posting review requires resolution before this source can be used for BAS.", refs))
	}
	return nil
}

func basCoverageLegacyReviewFindings(ctx context.Context, rows []purchases.PurchaseReportingRetainedRow,
	reviews []purchases.BASPurchaseLegacyReview,
	organisationID string, out *[]BASSourceFindingTemplate,
) error {
	for _, legacyReview := range reviews {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !legacyReview.IsCandidate {
			continue
		}
		if legacyReview.Review == nil || legacyReview.Review.LegacySource == nil ||
			legacyReview.Candidate.OrganisationID != organisationID {
			return ErrSourceReservation
		}
		if legacyReview.Review.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
			continue
		}
		id := legacyReview.Candidate.SourceID
		switch legacyReview.Candidate.Kind {
		case purchases.BASLegacyCandidateOpeningPayable:
			if legacyReview.Candidate.ConversionID == "" || legacyReview.Review.LegacySource.GetOpeningPayable() == nil ||
				legacyReview.Review.LegacySource.GetOpeningPayable().OpeningPayableId != id {
				return ErrSourceReservation
			}
		case purchases.BASLegacyCandidateDocument:
			if legacyReview.Candidate.ConversionID != "" || legacyReview.Review.LegacySource.GetDocument() == nil ||
				legacyReview.Review.LegacySource.GetDocument().DocumentId != id {
				return ErrSourceReservation
			}
		default:
			return ErrSourceReservation
		}
		root, err := basCoverageLegacyPrimaryIdentity(rows, legacyReview.Candidate,
			legacyReview.Review.LegacySource, organisationID)
		if err != nil {
			return err
		}
		refs := []*tammyv1.BasSourceIdentity{root}
		if legacyReview.Candidate.Kind == purchases.BASLegacyCandidateOpeningPayable {
			if legacyReview.Candidate.ConversionID == "" {
				return ErrSourceReservation
			}
			conversion, findErr := basCoverageFindRow(rows, "opening_conversions", "id", legacyReview.Candidate.ConversionID, "", 0, organisationID)
			if findErr != nil {
				return findErr
			}
			identity, identityErr := basCoverageRowIdentity(*conversion, legacyReview.Candidate.ConversionID, nil, nil, nil)
			if identityErr != nil {
				return identityErr
			}
			refs = append(refs, identity)
		}
		if legacyReview.Review.LegacyReview != nil {
			identity, identityErr := basCoverageLegacyDecisionIdentity(rows,
				legacyReview.Review.LegacyReview, legacyReview.Candidate, organisationID)
			if identityErr != nil {
				return identityErr
			}
			refs = append(refs, identity)
		}
		code := "LEGACY_" + basCoveragePurchaseReviewCode(legacyReview.Review)
		if !basCoverageCodePattern.MatchString(code) {
			return ErrSourceReservation
		}
		*out = append(*out, basCoverageFinding(basCoverageSeverityForPurchaseStatus(legacyReview.Review.Status), code,
			"A retained legacy purchase review requires resolution before this source can be used for BAS.", refs))
	}
	return nil
}

func basCoverageRawIdentity(sourceType, sourceID string, revision *int64, originalHash, proofHash []byte,
	scope tammyv1.BasSourceProofScope, component *uint64, relatedID *string, relatedSequence *uint64,
) (*tammyv1.BasSourceIdentity, error) {
	if len(proofHash) != sha256.Size {
		return nil, ErrSourceReservation
	}
	ref := &tammyv1.BasRawSourceRef{Type: sourceType, Id: sourceID, ComputedProofHash: bytes.Clone(proofHash), ProofScope: scope}
	if revision != nil {
		value := *revision
		if value < 0 {
			return nil, ErrSourceReservation
		}
		ref.OriginalRevision = &value
	}
	if originalHash != nil {
		if len(originalHash) != sha256.Size {
			return nil, ErrSourceReservation
		}
		ref.OriginalContentHash = bytes.Clone(originalHash)
	}
	if component != nil {
		value := *component
		if value == 0 {
			return nil, ErrSourceReservation
		}
		ref.ComponentSequence = &value
	}
	if relatedID != nil {
		value := *relatedID
		ref.RelatedId = &value
	}
	if relatedSequence != nil {
		value := *relatedSequence
		ref.RelatedSequence = &value
	}
	if !validBASRawSourceRef(ref) {
		return nil, ErrSourceReservation
	}
	return &tammyv1.BasSourceIdentity{RawSource: ref}, nil
}

func basCoverageRowIdentity(row purchases.PurchaseReportingRetainedRow, id string,
	component *uint64, relatedID *string, relatedSequence *uint64,
) (*tammyv1.BasSourceIdentity, error) {
	proofHash, err := basCoverageRowProofHash(row)
	if err != nil {
		return nil, err
	}
	revision, err := basCoverageRowRevision(row)
	if err != nil {
		return nil, err
	}
	originalHashColumn := ""
	var originalHash []byte
	switch row.Table {
	case "documents":
		originalHashColumn = "sha256"
	case "purchase_postings_v1":
		originalHashColumn = "original_content_hash"
	case "purchase_payments_v1", "purchase_tax_evidence_v1", "purchase_gst_attributions_v1", "purchase_legacy_reviews_v1":
		originalHashColumn = "source_hash"
	case "opening_conversions":
		value, present := basCoverageRowCell(row, "source_sha256")
		if !present {
			return nil, ErrSourceReservation
		}
		switch value.Kind {
		case "TEXT":
			if len(value.Text) != 64 || strings.ToLower(value.Text) != value.Text {
				return nil, ErrSourceReservation
			}
			decoded, decodeErr := hex.DecodeString(value.Text)
			if decodeErr != nil || len(decoded) != sha256.Size {
				return nil, ErrSourceReservation
			}
			originalHash = decoded
		default:
			return nil, ErrSourceReservation
		}
	case "opening_items":
		originalHashColumn = "source_content_hash"
	}
	if originalHashColumn != "" {
		originalHash, err = basCoverageRowOptionalBlob(row, originalHashColumn)
		if err != nil {
			return nil, err
		}
	}
	return basCoverageRawIdentity(row.Table, id, revision, originalHash, proofHash,
		tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION,
		component, relatedID, relatedSequence)
}

func basCoverageRowProofHash(row purchases.PurchaseReportingRetainedRow) ([]byte, error) {
	if row.Table == "" || len(row.Columns) == 0 || len(row.Columns) != len(row.Values) {
		return nil, ErrSourceReservation
	}
	seen := make(map[string]struct{}, len(row.Columns))
	h := sha256.New()
	basCoverageHashFrame(h, []byte("tammy/bas/purchase-retained-row/v1"))
	basCoverageHashFrame(h, []byte(row.Table))
	basCoverageHashUint64(h, uint64(len(row.Columns)))
	for index, column := range row.Columns {
		if column == "" {
			return nil, ErrSourceReservation
		}
		if _, duplicate := seen[column]; duplicate {
			return nil, ErrSourceReservation
		}
		seen[column] = struct{}{}
		value := row.Values[index]
		basCoverageHashFrame(h, []byte(column))
		basCoverageHashFrame(h, []byte(value.Kind))
		switch value.Kind {
		case "NULL":
			if value.Integer != 0 || value.Text != "" || value.Bytes != nil {
				return nil, ErrSourceReservation
			}
		case "INTEGER":
			if value.Text != "" || value.Bytes != nil {
				return nil, ErrSourceReservation
			}
			basCoverageHashInt64(h, value.Integer)
		case "TEXT":
			if value.Integer != 0 || value.Bytes != nil {
				return nil, ErrSourceReservation
			}
			basCoverageHashFrame(h, []byte(value.Text))
		case "BLOB":
			if value.Integer != 0 || value.Text != "" {
				return nil, ErrSourceReservation
			}
			basCoverageHashFrame(h, value.Bytes)
		default:
			return nil, ErrSourceReservation
		}
	}
	return h.Sum(nil), nil
}

func basCoverageFindRow(rows []purchases.PurchaseReportingRetainedRow, table, idColumn, id,
	sequenceColumn string, sequence uint64, organisationID string,
) (*purchases.PurchaseReportingRetainedRow, error) {
	if table == "" || idColumn == "" || id == "" || sequence > math.MaxInt64 {
		return nil, ErrSourceReservation
	}
	var found *purchases.PurchaseReportingRetainedRow
	for index := range rows {
		row := &rows[index]
		if row.Table != table || basCoverageText(*row, idColumn) != id {
			continue
		}
		if sequenceColumn != "" && basCoverageInteger(*row, sequenceColumn) != int64(sequence) {
			continue
		}
		if organisationID != "" {
			owner, present := basCoverageRowCell(*row, "organisation_id")
			if !present || owner.Kind != "TEXT" || owner.Text != organisationID {
				continue
			}
		}
		if found != nil {
			return nil, ErrSourceReservation
		}
		found = row
	}
	if found == nil {
		return nil, ErrSourceReservation
	}
	return found, nil
}

func basCoverageFindOpeningPayable(rows []purchases.PurchaseReportingRetainedRow, id, conversionID string) (*purchases.PurchaseReportingRetainedRow, error) {
	return basCoverageFindExactPairRow(rows, "purchase_opening_payables", id, "conversion_id", conversionID)
}

func basCoverageFindOpeningItem(rows []purchases.PurchaseReportingRetainedRow, id, conversionID string) (*purchases.PurchaseReportingRetainedRow, error) {
	return basCoverageFindExactPairRow(rows, "opening_items", id, "conversion_id", conversionID)
}

func basCoverageFindExactPairRow(rows []purchases.PurchaseReportingRetainedRow, table, id, relatedColumn, related string) (*purchases.PurchaseReportingRetainedRow, error) {
	if table == "" || id == "" || relatedColumn == "" || related == "" {
		return nil, ErrSourceReservation
	}
	var found *purchases.PurchaseReportingRetainedRow
	for index := range rows {
		row := &rows[index]
		if row.Table != table || basCoverageText(*row, "id") != id || basCoverageText(*row, relatedColumn) != related {
			continue
		}
		if found != nil {
			return nil, ErrSourceReservation
		}
		found = row
	}
	if found == nil {
		return nil, ErrSourceReservation
	}
	return found, nil
}

func basCoverageValidateModernPrimaryRow(row purchases.PurchaseReportingRetainedRow, key purchases.BASModernCandidateKey, organisationID string) error {
	if row.Table == "" || basCoverageText(row, "organisation_id") != organisationID {
		return ErrSourceReservation
	}
	switch key.Kind {
	case purchases.BASModernCandidatePosting:
		if row.Table != "purchase_postings_v1" || basCoverageText(row, "id") != key.PostingID ||
			basCoverageText(row, "document_id") != key.DocumentID {
			return ErrSourceReservation
		}
	case purchases.BASModernCandidatePostingAllocation:
		if row.Table != "purchase_posting_allocations_v1" || basCoverageText(row, "posting_id") != key.PostingID ||
			key.Sequence > math.MaxInt64 ||
			basCoverageInteger(row, "sequence") != int64(key.Sequence) {
			return ErrSourceReservation
		}
	case purchases.BASModernCandidatePayment:
		if row.Table != "purchase_payments_v1" || basCoverageText(row, "id") != key.PaymentID ||
			basCoverageText(row, "posting_id") != key.PostingID {
			return ErrSourceReservation
		}
	case purchases.BASModernCandidatePaymentAllocation:
		if row.Table != "purchase_payment_allocations_v1" || basCoverageText(row, "payment_id") != key.PaymentID ||
			basCoverageText(row, "posting_id") != key.PostingID ||
			key.Sequence > math.MaxInt64 || key.PostingAllocationSequence > math.MaxInt64 ||
			basCoverageInteger(row, "sequence") != int64(key.Sequence) ||
			basCoverageInteger(row, "posting_allocation_sequence") != int64(key.PostingAllocationSequence) {
			return ErrSourceReservation
		}
	default:
		return ErrSourceReservation
	}
	return nil
}

func basCoverageValidatePurchaseLineRowLinks(rows []purchases.PurchaseReportingRetainedRow,
	line tammyv1.BasPurchaseSourceLine, key purchases.BASModernCandidateKey, organisationID string,
) error {
	posting, err := basCoverageFindRow(rows, "purchase_postings_v1", "id", key.PostingID, "", 0, organisationID)
	if err != nil || posting == nil || basCoverageText(*posting, "document_id") != key.DocumentID ||
		!basCoverageBlobEqual(*posting, "source_hash", line.PostingHash) ||
		!basCoverageBlobEqual(*posting, "original_content_hash", line.OriginalContentHash) ||
		!basCoverageBlobEqual(*posting, "reviewed_snapshot_hash", line.ReviewedSnapshotHash) {
		return ErrSourceReservation
	}
	if key.Kind != purchases.BASModernCandidatePostingAllocation && key.Kind != purchases.BASModernCandidatePaymentAllocation {
		return ErrSourceReservation
	}
	wantPostingSequence := key.Sequence
	if key.Kind == purchases.BASModernCandidatePaymentAllocation {
		wantPostingSequence = key.PostingAllocationSequence
	}
	if line.AllocationSequence == 0 || uint64(line.AllocationSequence) != wantPostingSequence {
		return ErrSourceReservation
	}
	if key.Kind == purchases.BASModernCandidatePaymentAllocation {
		if line.PaymentId == nil || *line.PaymentId != key.PaymentID {
			return ErrSourceReservation
		}
	} else if line.PaymentId != nil {
		return ErrSourceReservation
	}
	if line.Evidence == nil || line.Evidence.SourceId == "" || line.Evidence.SourceVersion == 0 || len(line.Evidence.SourceHash) != sha256.Size {
		return ErrSourceReservation
	}
	evidence, err := basCoverageFindRow(rows, "purchase_tax_evidence_v1", "id", line.Evidence.SourceId, "", 0, organisationID)
	if err != nil || evidence == nil || basCoverageText(*evidence, "posting_id") != key.PostingID ||
		basCoverageText(*evidence, "document_id") != key.DocumentID ||
		basCoverageInteger(*evidence, "accounting_version") != int64(line.Evidence.SourceVersion) ||
		!basCoverageBlobEqual(*evidence, "record_hash", line.Evidence.SourceHash) {
		return ErrSourceReservation
	}
	if line.AttributionId != nil {
		if *line.AttributionId == "" || len(line.AttributionHash) != sha256.Size || !bytes.Equal(line.AttributionHash, line.SourceHash) {
			return ErrSourceReservation
		}
		attribution, findErr := basCoverageFindRow(rows, "purchase_gst_attributions_v1", "id", *line.AttributionId, "", 0, organisationID)
		if findErr != nil || attribution == nil || basCoverageText(*attribution, "posting_id") != key.PostingID ||
			basCoverageText(*attribution, "document_id") != key.DocumentID ||
			basCoverageInteger(*attribution, "accounting_version") != int64(line.SourceRevision) ||
			basCoverageInteger(*attribution, "allocation_sequence") != int64(line.AllocationSequence) ||
			!basCoverageBlobEqual(*attribution, "source_hash", line.AttributionHash) {
			return ErrSourceReservation
		}
		storedPayment, present := basCoverageRowCell(*attribution, "payment_id")
		if key.Kind == purchases.BASModernCandidatePostingAllocation {
			if line.PaymentId != nil || !present || storedPayment.Kind != "NULL" {
				return ErrSourceReservation
			}
		} else if line.PaymentId == nil || *line.PaymentId != key.PaymentID {
			return ErrSourceReservation
		}
		if line.PaymentId == nil {
			if !present || storedPayment.Kind != "NULL" {
				return ErrSourceReservation
			}
		} else if !present || storedPayment.Kind != "TEXT" || storedPayment.Text != *line.PaymentId {
			return ErrSourceReservation
		}
	} else {
		if line.AttributionHash != nil || line.SourceRevision == 0 ||
			!bytes.Equal(line.SourceHash, line.PostingHash) && line.PaymentId == nil {
			return ErrSourceReservation
		}
	}
	if line.PaymentId != nil {
		payment, findErr := basCoverageFindRow(rows, "purchase_payments_v1", "id", *line.PaymentId, "", 0, organisationID)
		if findErr != nil || payment == nil || basCoverageText(*payment, "posting_id") != key.PostingID ||
			len(line.PaymentHash) != sha256.Size || !basCoverageBlobEqual(*payment, "source_hash", line.PaymentHash) {
			return ErrSourceReservation
		}
		if line.AttributionId == nil && (basCoverageInteger(*payment, "accounting_version") != int64(line.SourceRevision) ||
			!bytes.Equal(line.SourceHash, line.PaymentHash)) {
			return ErrSourceReservation
		}
	} else if line.PaymentHash != nil || key.Kind == purchases.BASModernCandidatePaymentAllocation || key.Kind == purchases.BASModernCandidatePayment {
		return ErrSourceReservation
	}
	return nil
}

func basCoverageBlobEqual(row purchases.PurchaseReportingRetainedRow, column string, expected []byte) bool {
	value, present := basCoverageRowCell(row, column)
	return present && value.Kind == "BLOB" && bytes.Equal(value.Bytes, expected)
}

func basCoverageNullableBlobEqual(row purchases.PurchaseReportingRetainedRow, column string, expected []byte, present bool) bool {
	value, found := basCoverageRowCell(row, column)
	if !found {
		return false
	}
	if !present {
		return value.Kind == "NULL" && value.Integer == 0 && value.Text == "" && value.Bytes == nil
	}
	return value.Kind == "BLOB" && len(expected) == sha256.Size && bytes.Equal(value.Bytes, expected) &&
		value.Integer == 0 && value.Text == ""
}

func basCoverageJoinIdentities(primary *tammyv1.BasSourceIdentity, lineage, evidence []*tammyv1.BasSourceIdentity) []*tammyv1.BasSourceIdentity {
	result := make([]*tammyv1.BasSourceIdentity, 0, 1+len(lineage)+len(evidence))
	if primary != nil {
		result = append(result, primary)
	}
	result = append(result, lineage...)
	result = append(result, evidence...)
	return result
}

func basCoverageText(row purchases.PurchaseReportingRetainedRow, column string) string {
	for index, name := range row.Columns {
		if name == column && index < len(row.Values) && row.Values[index].Kind == "TEXT" {
			return row.Values[index].Text
		}
	}
	return ""
}

func basCoverageInteger(row purchases.PurchaseReportingRetainedRow, column string) int64 {
	for index, name := range row.Columns {
		if name == column && index < len(row.Values) && row.Values[index].Kind == "INTEGER" {
			return row.Values[index].Integer
		}
	}
	return math.MinInt64
}

func basCoverageHasColumn(row purchases.PurchaseReportingRetainedRow, column string) bool {
	for _, name := range row.Columns {
		if name == column {
			return true
		}
	}
	return false
}

func basCoverageRowCell(row purchases.PurchaseReportingRetainedRow, column string) (purchases.PurchaseReportingRetainedValue, bool) {
	for index, name := range row.Columns {
		if name == column && index < len(row.Values) {
			return row.Values[index], true
		}
	}
	return purchases.PurchaseReportingRetainedValue{}, false
}

func basCoverageRowOptionalInteger(row purchases.PurchaseReportingRetainedRow, column string) (*int64, error) {
	value, present := basCoverageRowCell(row, column)
	if !present {
		return nil, ErrSourceReservation
	}
	if value.Kind == "NULL" {
		return nil, nil
	}
	if value.Kind != "INTEGER" || value.Integer < 0 || value.Text != "" || value.Bytes != nil {
		return nil, ErrSourceReservation
	}
	result := value.Integer
	return &result, nil
}

func basCoverageRowRevision(row purchases.PurchaseReportingRetainedRow) (*int64, error) {
	column := ""
	switch row.Table {
	case "documents":
		column = "version"
	case "purchase_postings_v1":
		column = "reviewed_version"
	case "purchase_payments_v1", "purchase_tax_evidence_v1", "purchase_gst_attributions_v1", "purchase_legacy_reviews_v1":
		column = "accounting_version"
	case "opening_conversions":
		column = "version"
	case "opening_items":
		column = "source_revision"
	}
	if column == "" {
		return nil, nil
	}
	return basCoverageRowOptionalInteger(row, column)
}

func basCoverageRowOptionalBlob(row purchases.PurchaseReportingRetainedRow, column string) ([]byte, error) {
	value, present := basCoverageRowCell(row, column)
	if !present {
		return nil, ErrSourceReservation
	}
	if value.Kind == "NULL" {
		return nil, nil
	}
	if value.Kind != "BLOB" || len(value.Bytes) != sha256.Size || value.Integer != 0 || value.Text != "" {
		return nil, ErrSourceReservation
	}
	return bytes.Clone(value.Bytes), nil
}

func basCoverageTaxFactProofHash(fact accounting.TaxFactReportingRecord) ([]byte, error) {
	h := sha256.New()
	basCoverageHashFrame(h, []byte("tammy/bas/accounting-tax-fact-row/v1"))
	for _, value := range []string{fact.ID, fact.OrganisationID, fact.JournalLineID, fact.TaxCode, fact.Treatment} {
		basCoverageHashFrame(h, []byte("TEXT"))
		basCoverageHashFrame(h, []byte(value))
	}
	for _, value := range []int64{
		fact.OriginalGrossMinor, fact.OriginalNetMinor, fact.OriginalGSTMinor,
		fact.AttributedGrossMinor, fact.AttributedNetMinor, fact.AttributedGSTMinor,
		fact.RemainingGrossMinor, fact.RemainingNetMinor, fact.RemainingGSTMinor,
	} {
		basCoverageHashFrame(h, []byte("INTEGER"))
		basCoverageHashInt64(h, value)
	}
	if fact.Rule == nil {
		basCoverageHashFrame(h, []byte("NULL_RULE"))
	} else {
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(fact.Rule)
		if err != nil {
			return nil, err
		}
		basCoverageHashFrame(h, []byte("SOURCE_REF"))
		basCoverageHashFrame(h, wire)
	}
	for _, value := range []string{fact.SourceType, fact.SourceID} {
		basCoverageHashFrame(h, []byte("TEXT"))
		basCoverageHashFrame(h, []byte(value))
	}
	basCoverageHashFrame(h, []byte("INTEGER"))
	basCoverageHashUint64(h, fact.SourceRevision)
	if fact.SourceContentHashPresent {
		if len(fact.SourceContentHash) != sha256.Size {
			return nil, ErrSourceReservation
		}
		basCoverageHashFrame(h, []byte("BLOB"))
		basCoverageHashFrame(h, fact.SourceContentHash)
	} else {
		if fact.SourceContentHash != nil {
			return nil, ErrSourceReservation
		}
		basCoverageHashFrame(h, []byte("NULL"))
	}
	basCoverageHashFrame(h, []byte("TEXT"))
	basCoverageHashFrame(h, []byte(fact.CreatedAt))
	return h.Sum(nil), nil
}

func basCoverageIndexJournals(records []*accounting.JournalReportingRecord, organisationID string) (map[string]*accounting.JournalReportingRecord, error) {
	indexed := make(map[string]*accounting.JournalReportingRecord, len(records))
	for _, record := range records {
		if record == nil || record.Journal == nil || record.Journal.Id == "" || record.Journal.OrganisationId != organisationID ||
			len(record.ContentHash) != sha256.Size || record.PayloadBytes <= 0 {
			return nil, ErrSourceReservation
		}
		if _, duplicate := indexed[record.Journal.Id]; duplicate {
			return nil, ErrSourceReservation
		}
		indexed[record.Journal.Id] = record
	}
	return indexed, nil
}

func basCoverageJournalIdentity(record *accounting.JournalReportingRecord) (*tammyv1.BasSourceIdentity, error) {
	if record == nil || record.Journal == nil || record.Journal.Id == "" || record.SourceRevision > math.MaxInt64 {
		return nil, ErrSourceReservation
	}
	return basCoverageRawIdentity("journals", record.Journal.Id, basCoverageOptionalRevision(record.SourceRevision), nil,
		record.ContentHash, tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_COMPLETE_JOURNAL, nil, nil, nil)
}

func basCoverageJournalIDsForModernKey(rows []purchases.PurchaseReportingRetainedRow, key purchases.BASModernCandidateKey) []string {
	set := make(map[string]struct{}, 2)
	for _, row := range rows {
		if row.Table == "purchase_postings_v1" && basCoverageText(row, "id") == key.PostingID {
			if id := basCoverageText(row, "journal_id"); id != "" {
				set[id] = struct{}{}
			}
		}
		if row.Table == "purchase_payments_v1" && basCoverageText(row, "id") == key.PaymentID {
			if id := basCoverageText(row, "journal_id"); id != "" {
				set[id] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func basCoverageJournalIDsForPostingReview(row purchases.PurchaseReportingRetainedRow, review *tammyv1.BasPurchaseReview) []string {
	set := make(map[string]struct{}, 2)
	if id := basCoverageText(row, "journal_id"); id != "" {
		set[id] = struct{}{}
	}
	if review != nil && review.FutureClaimObligation != nil {
		// The complete source/reversal parent set is globally fingerprinted by
		// the purchase module; the current candidate-level link remains its root.
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func basCoverageDedupeIdentities(values []*tammyv1.BasSourceIdentity) []*tammyv1.BasSourceIdentity {
	seen := make(map[string]struct{}, len(values))
	result := make([]*tammyv1.BasSourceIdentity, 0, len(values))
	marshal := proto.MarshalOptions{Deterministic: true}
	for _, value := range values {
		wire, err := marshal.Marshal(value)
		if err != nil {
			continue
		}
		key := string(wire)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func basCoverageAppendUnique(values []*tammyv1.BasSourceIdentity, identity *tammyv1.BasSourceIdentity) []*tammyv1.BasSourceIdentity {
	for _, existing := range values {
		if proto.Equal(existing, identity) {
			return values
		}
	}
	return append(values, identity)
}

func basCoverageFinding(severity tammyv1.BasFindingSeverity, code, message string, identities []*tammyv1.BasSourceIdentity) BASSourceFindingTemplate {
	return BASSourceFindingTemplate{Severity: severity, StableCode: code, SafeMessage: message,
		SourceIdentities: basCoverageDedupeIdentities(identities)}
}

func basCoverageAggregateFindings(values []BASSourceFindingTemplate) ([]BASSourceFindingTemplate, error) {
	type key struct {
		severity tammyv1.BasFindingSeverity
		code     string
		message  string
	}
	order := make([]key, 0)
	groups := make(map[key][][]*tammyv1.BasSourceIdentity)
	for _, value := range values {
		if value.Severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSPECIFIED ||
			value.StableCode == "" || len(value.StableCode) > 128 || strings.TrimSpace(value.SafeMessage) == "" ||
			len(value.SafeMessage) > 512 || len(value.SourceIdentities) == 0 || len(value.SourceIdentities) > basSourceIdentityMaximum {
			return nil, ErrSourceReservation
		}
		for _, identity := range value.SourceIdentities {
			if !validBASSourceIdentity(identity) {
				return nil, ErrSourceReservation
			}
		}
		group := key{severity: value.Severity, code: value.StableCode, message: value.SafeMessage}
		if _, exists := groups[group]; !exists {
			order = append(order, group)
			groups[group] = make([][]*tammyv1.BasSourceIdentity, 0, 1)
		}
		groups[group] = append(groups[group], value.SourceIdentities)
	}
	result := make([]BASSourceFindingTemplate, 0, len(values))
	for _, group := range order {
		chunks := groups[group]
		current := make([]*tammyv1.BasSourceIdentity, 0, basSourceIdentityMaximum)
		for _, sourceSet := range chunks {
			unique := basCoverageDedupeIdentities(append(append([]*tammyv1.BasSourceIdentity(nil), current...), sourceSet...))
			if len(unique) > basSourceIdentityMaximum {
				if len(current) == 0 {
					return nil, ErrSourceReservation
				}
				result = append(result, BASSourceFindingTemplate{Severity: group.severity, StableCode: group.code,
					SafeMessage: group.message, SourceIdentities: current})
				current = basCoverageDedupeIdentities(sourceSet)
				if len(current) > basSourceIdentityMaximum {
					return nil, ErrSourceReservation
				}
			} else {
				current = unique
			}
		}
		if len(current) != 0 {
			result = append(result, BASSourceFindingTemplate{Severity: group.severity, StableCode: group.code,
				SafeMessage: group.message, SourceIdentities: current})
		}
	}
	return result, nil
}

func basCoverageNeedsFinding(disposition tammyv1.BasCoverageDisposition, severity tammyv1.BasFindingSeverity) bool {
	return severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER ||
		severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING ||
		severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED ||
		disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED ||
		disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED
}

func basCoverageSeverityForDisposition(disposition tammyv1.BasCoverageDisposition) tammyv1.BasFindingSeverity {
	switch disposition {
	case tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED:
		return tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED
	case tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED:
		return tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER
	default:
		return tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION
	}
}

func basCoverageSeverityForPurchaseStatus(status tammyv1.PurchaseReviewStatus) tammyv1.BasFindingSeverity {
	switch status {
	case tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED:
		return tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED
	case tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR:
		return tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION
	default:
		return tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER
	}
}

func basCoveragePurchaseReviewCode(review *tammyv1.BasPurchaseReview) string {
	if review == nil {
		return "PURCHASE_REVIEW_REQUIRED"
	}
	base := strings.TrimPrefix(review.Kind.String(), "BAS_PURCHASE_REVIEW_KIND_")
	base = strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(base))
	if base == "" || strings.Contains(base, "UNSPECIFIED") {
		base = "REVIEW_REQUIRED"
	}
	return "PURCHASE_" + base
}

func basCoverageDispositionValid(value tammyv1.BasCoverageDisposition) bool {
	return value >= tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE &&
		value <= tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE
}

func basCoverageValidateProto(value *tammyv1.BasSourceCoverage) error {
	if value == nil || value.RawSource == nil || value.Source != nil || !validBASRawSourceRef(value.RawSource) ||
		value.CandidateKind < tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_INVOICE_POSTING ||
		value.CandidateKind > tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_OTHER_OBLIGATION ||
		!basCoverageDispositionValid(value.Disposition) || !basCoverageCodePattern.MatchString(value.ClassificationCode) ||
		strings.TrimSpace(value.Reason) == "" || len(value.Reason) > 512 ||
		(value.TaxDate != nil && !validCalendarDate(value.TaxDate)) ||
		len(value.LineageIdentities) > basSourceIdentityMaximum || len(value.EvidenceIdentities) > basSourceIdentityMaximum ||
		len(value.Lineage) != 0 || len(value.Evidence) != 0 {
		return ErrSourceReservation
	}
	for _, identity := range append(append([]*tammyv1.BasSourceIdentity(nil), value.LineageIdentities...), value.EvidenceIdentities...) {
		if !validBASSourceIdentity(identity) || identity.RawSource == nil {
			return ErrSourceReservation
		}
	}
	if value.EligibleAmount != nil && (value.EligibleAmount.CurrencyCode != "AUD" || value.EligibleAmount.MinorUnits < 0) {
		return ErrSourceReservation
	}
	if value.Disposition == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE && (value.EligibleAmount == nil || value.TaxDate == nil) {
		return ErrSourceReservation
	}
	if value.Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE && value.EligibleAmount != nil {
		return ErrSourceReservation
	}
	return nil
}

func basCoverageTaxFactColumnsMatch(row purchases.PurchaseReportingRetainedRow) bool {
	return basSharedTaxFactProjectionShape(row)
}

func basCoverageOptionalRevision(value uint64) *int64 {
	if value > math.MaxInt64 {
		return nil
	}
	revision := int64(value)
	return &revision
}

func basCoverageOptionalHash(present bool, value []byte) []byte {
	if !present || len(value) != sha256.Size {
		return nil
	}
	return bytes.Clone(value)
}

func basCoverageHashFrame(h hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write(value)
}

func basCoverageHashUint64(h hash.Hash, value uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	basCoverageHashFrame(h, raw[:])
}

func basCoverageHashInt64(h hash.Hash, value int64) { basCoverageHashUint64(h, uint64(value)) }

func basCoverageHashBool(h hash.Hash, value bool) {
	if value {
		basCoverageHashFrame(h, []byte{1})
	} else {
		basCoverageHashFrame(h, []byte{0})
	}
}

func basCoverageAdd(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

// BASSourceSnapshotBinding is assembled by the owning transaction command.
// These are exact retained protos, not IDs fetched by this pure hash layer.
type BASSourceSnapshotBinding struct {
	Organisation   *tammyv1.Organisation
	Profile        *tammyv1.BasObligationsProfile
	Period         *tammyv1.BasConfirmedPeriod
	PaygwInputs    *tammyv1.BasPaygwInputs
	PaygiInputs    *tammyv1.BasPaygiInputs
	RevisionVector *tammyv1.BasSourceRevisionVector
}

type BASSourceSnapshotBindingResult struct {
	SourceSnapshotHash   []byte
	RevisionVectorHash   []byte
	BindingPayloadBytes  int64
	CombinedPayloadBytes int64 // coverage plus retained scope/revision protos
}

// BindBASSourceSnapshot hashes the exact scope and all source-owned semantic
// revisions. financial_revision remains in RevisionVectorHash only because it
// is an aggregate counter that can change for unrelated reporting writes.
// remainingPayloadBytes is the caller's budget after the source-record
// reservation; this function charges coverage and binding protos against it.
func BindBASSourceSnapshot(ctx context.Context, capture *BASBoundedSourceRecordCapture,
	binding BASSourceSnapshotBinding, coverage *BASSourceCoverageResult, remainingPayloadBytes int64,
) (*BASSourceSnapshotBindingResult, error) {
	fail := func(err error) (*BASSourceSnapshotBindingResult, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, errors.Join(ErrSourceReservation, err)
	}
	if ctx == nil || capture == nil || coverage == nil || remainingPayloadBytes < 0 ||
		capture.Financial == nil || capture.Financial.Invoices == nil || capture.Invoices == nil ||
		capture.Purchases == nil || capture.Accounting == nil || capture.ConfirmedPeriod == nil ||
		binding.Organisation == nil || binding.Profile == nil || binding.Period == nil ||
		binding.PaygwInputs == nil || binding.PaygiInputs == nil || binding.RevisionVector == nil ||
		!ids.IsCanonicalV7(capture.OrganisationID) ||
		binding.Organisation.Id != capture.OrganisationID || binding.Profile.OrganisationId != capture.OrganisationID ||
		binding.Period.OrganisationId != capture.OrganisationID || !proto.Equal(binding.Period, capture.ConfirmedPeriod) ||
		binding.Organisation.Version == 0 || binding.Profile.Revision == 0 || binding.Period.Version == 0 ||
		binding.Profile.Profile == nil || binding.Profile.Profile.Gst == nil ||
		!validProfileReference(&tammyv1.SourceRef{Type: "bas_obligations_profile", Id: binding.Profile.ProfileId,
			Revision: binding.Profile.Revision, ContentHash: binding.Profile.ContentHash}, binding.Profile, capture.OrganisationID) ||
		!validConfirmedPeriodReference(&tammyv1.SourceRef{Type: "bas_period", Id: binding.Period.PeriodId,
			Revision: binding.Period.Version, ContentHash: binding.Period.ContentHash}, binding.Period, capture.OrganisationID) ||
		binding.Period.Confirmation == nil || !sameBASSourceRef(binding.Period.Confirmation.Profile,
		&tammyv1.SourceRef{Type: "bas_obligations_profile", Id: binding.Profile.ProfileId,
			Revision: binding.Profile.Revision, ContentHash: binding.Profile.ContentHash}) ||
		capture.ActiveRuleBundle == nil || binding.Organisation.ActiveTaxRuleBundle == nil ||
		!validRef(binding.Organisation.ActiveTaxRuleBundle, "tax_rule_bundle") ||
		!sameBASSourceRef(binding.Organisation.ActiveTaxRuleBundle, capture.ActiveRuleBundle) ||
		capture.RuleBundle == nil ||
		!sameBASSourceRef(capture.RuleBundle.Bundle.Source, capture.ActiveRuleBundle) ||
		capture.RuleBundle.OrganisationID != capture.OrganisationID || len(capture.RuleBundle.ContentHash) != sha256.Size ||
		!validRef(capture.ActiveRuleBundle, "tax_rule_bundle") ||
		binding.Organisation.GstBasis != capture.Financial.Invoices.GstBasis {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	manifestHash, manifestBytes, err := basCoverageManifest(ctx, coverage.Coverage)
	if err != nil {
		return fail(err)
	}
	if len(coverage.ManifestHash) != sha256.Size || !bytes.Equal(coverage.ManifestHash, manifestHash) ||
		coverage.CoveragePayloadBytes != manifestBytes {
		return fail(nil)
	}
	if manifestBytes > remainingPayloadBytes || manifestBytes > maximumBASSourcePayloadBytes {
		return fail(&SourceBoundError{Resource: "BAS source coverage bytes", Limit: remainingPayloadBytes})
	}
	messages := []proto.Message{binding.Organisation, binding.Profile, binding.Period, binding.PaygwInputs, binding.PaygiInputs, binding.RevisionVector}
	wires := make([][]byte, len(messages))
	var payloadBytes int64
	for index, message := range messages {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		size := int64(proto.Size(message))
		charge, ok := basCoverageAdd(512, size)
		if !ok || payloadBytes > remainingPayloadBytes-manifestBytes-charge ||
			payloadBytes > maximumBASSourcePayloadBytes-manifestBytes-charge {
			return fail(&SourceBoundError{Resource: "BAS source binding bytes", Limit: remainingPayloadBytes})
		}
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
		if err != nil || int64(len(wire)) != size {
			return fail(err)
		}
		wires[index] = wire
		payloadBytes += charge
	}
	findingHash, findingBytes, err := basCoverageFindingManifest(ctx, coverage.FindingTemplates)
	if err != nil {
		return fail(err)
	}
	if payloadBytes > remainingPayloadBytes-manifestBytes-findingBytes {
		return fail(&SourceBoundError{Resource: "BAS source finding bytes", Limit: remainingPayloadBytes})
	}
	payloadBytes += findingBytes
	if len(capture.Invoices.Fingerprint) != sha256.Size || len(capture.Purchases.OriginalFingerprint) != sha256.Size ||
		len(capture.Accounting.Fingerprint) != sha256.Size {
		return fail(nil)
	}
	vectorHash := sha256.New()
	basCoverageHashFrame(vectorHash, []byte(basRevisionVectorHashTag))
	basCoverageHashFrame(vectorHash, wires[5])
	var sourceHash = sha256.New()
	basCoverageHashFrame(sourceHash, []byte(basSourceSnapshotHashTag))
	for _, wire := range [][]byte{wires[0], wires[1], wires[2], wires[3], wires[4]} {
		basCoverageHashFrame(sourceHash, wire)
	}
	ruleRef, err := (proto.MarshalOptions{Deterministic: true}).Marshal(capture.ActiveRuleBundle)
	if err != nil {
		return fail(err)
	}
	basCoverageHashFrame(sourceHash, ruleRef)
	basCoverageHashFrame(sourceHash, capture.RuleBundle.ContentHash)
	basCoverageHashFrame(sourceHash, capture.Invoices.Fingerprint)
	basCoverageHashFrame(sourceHash, capture.Purchases.OriginalFingerprint)
	basCoverageHashFrame(sourceHash, capture.Accounting.Fingerprint)
	basCoverageHashFrame(sourceHash, coverage.ManifestHash)
	basCoverageHashFrame(sourceHash, findingHash)
	// Meaningful per-source domains only. Full vector bytes above also retain
	// financial_revision for historical integrity, but it is omitted here.
	for _, value := range []uint64{
		binding.RevisionVector.LedgerRevision,
		binding.RevisionVector.SettlementRevision,
		binding.RevisionVector.BankingRevision,
		binding.RevisionVector.TaxSourceRevision,
		binding.RevisionVector.OrganisationProfileRevision,
		binding.RevisionVector.RuleBundleRevision,
		binding.RevisionVector.PaygRevision,
		binding.RevisionVector.BasProfileRevision,
		binding.RevisionVector.PeriodRevision,
	} {
		basCoverageHashUint64(sourceHash, value)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	combinedPayloadBytes, ok := basCoverageAdd(manifestBytes, payloadBytes)
	if !ok || combinedPayloadBytes > remainingPayloadBytes || combinedPayloadBytes > maximumBASSourcePayloadBytes {
		return fail(&SourceBoundError{Resource: "BAS source coverage and binding bytes", Limit: remainingPayloadBytes})
	}
	return &BASSourceSnapshotBindingResult{SourceSnapshotHash: sourceHash.Sum(nil),
		RevisionVectorHash: vectorHash.Sum(nil), BindingPayloadBytes: payloadBytes,
		CombinedPayloadBytes: combinedPayloadBytes}, nil
}

// basCoverageLegacySourceScope decodes the already retained, purchase-owned
// source identity without consulting live source tables.
func basCoverageLegacySourceScope(source *tammyv1.LegacyPurchaseSourceRef) (kind, id string, version uint64, originalHash, reviewedHash []byte, err error) {
	if source == nil {
		err = ErrSourceReservation
		return
	}
	switch value := source.Source.(type) {
	case *tammyv1.LegacyPurchaseSourceRef_Document:
		if value == nil || value.Document == nil {
			err = ErrSourceReservation
			return
		}
		kind, id, version, originalHash, reviewedHash = "DOCUMENT", value.Document.DocumentId, value.Document.ReviewedVersion, value.Document.OriginalContentHash, value.Document.ReviewedSnapshotHash
	case *tammyv1.LegacyPurchaseSourceRef_OpeningPayable:
		if value == nil || value.OpeningPayable == nil {
			err = ErrSourceReservation
			return
		}
		kind, id, version, originalHash = "OPENING_PAYABLE", value.OpeningPayable.OpeningPayableId, value.OpeningPayable.SourceVersion, value.OpeningPayable.SourceHash
	default:
		err = ErrSourceReservation
		return
	}
	if !ids.IsCanonicalV7(id) || version == 0 || version > math.MaxInt64 || len(originalHash) != sha256.Size || kind == "DOCUMENT" && len(reviewedHash) != sha256.Size {
		err = ErrSourceReservation
	}
	return
}

func basCoverageCloneCalendarDate(value *tammyv1.CivilDate) *tammyv1.CivilDate {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*tammyv1.CivilDate)
}

// Findings include timing outcomes that can change without a source-row write.
// Hash their deterministic semantic payload before commands allocate finding IDs.
func basCoverageFindingManifest(ctx context.Context, findings []BASSourceFindingTemplate) ([]byte, int64, error) {
	if ctx == nil || len(findings) > validationFindingMaximum {
		return nil, 0, ErrSourceReservation
	}
	h := sha256.New()
	basCoverageHashFrame(h, []byte("tammy/bas/source-finding-semantics/v1"))
	basCoverageHashUint64(h, uint64(len(findings)))
	var payloadBytes int64
	for _, finding := range findings {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if finding.Severity < tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER ||
			finding.Severity > tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED ||
			!basCoverageCodePattern.MatchString(finding.StableCode) || strings.TrimSpace(finding.SafeMessage) == "" ||
			len(finding.SafeMessage) > 512 || len(finding.SourceIdentities) == 0 || len(finding.SourceIdentities) > basSourceIdentityMaximum {
			return nil, 0, ErrSourceReservation
		}
		for _, identity := range finding.SourceIdentities {
			if !validBASSourceIdentity(identity) {
				return nil, 0, ErrSourceReservation
			}
		}
		frame := &tammyv1.BasValidationFinding{Severity: finding.Severity, StableCode: finding.StableCode,
			SafeMessage: finding.SafeMessage, SourceIdentities: finding.SourceIdentities}
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(frame)
		if err != nil {
			return nil, 0, err
		}
		charge, ok := basCoverageAdd(512, int64(len(wire)))
		if !ok || payloadBytes > maximumBASSourcePayloadBytes-charge {
			return nil, 0, ErrSourceBound
		}
		payloadBytes += charge
		basCoverageHashFrame(h, wire)
	}
	return h.Sum(nil), payloadBytes, nil
}
