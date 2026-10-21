package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const purchaseCoverageFingerprintTag = "tammy/purchases/coverage-reporting/v1"

// PurchaseCoverageReportingCapture is a bounded, read-only source view for BAS
// preparation. It includes modern purchase postings, all document roots that
// are not represented by a modern posting, posted/replaced opening payables,
// their immutable history, and the complete linked journal reversal closure.
// It does not classify, deduplicate, or decide BAS eligibility.
type PurchaseCoverageReportingCapture struct {
	OrganisationID      string
	Postings            []PostingReportingRecord
	LegacySources       []*LegacyPurchaseReportingRecord
	Documents           []PostingDocumentReportingRecord
	Journals            []*accounting.JournalReportingRecord
	Rows                []PurchaseReportingRetainedRow
	Fingerprint         []byte
	PayloadBytes        int64
	RootCount           int
	ReceiptCount        int
	JournalCount        int
	JournalIDs          []string
	JournalPayloadBytes map[string]int64
	BasePayloadBytes    int64
	TaxFactPayloadBytes int64
}

// LegacyPurchaseReportingRecord retains the source identity and its explicit
// document/opening/journal edges. A promoted document can have historical
// review or journal provenance while CurrentRoot is false.
type LegacyPurchaseReportingRecord struct {
	Source                    *tammyv1.LegacyPurchaseSourceRef
	Document                  *tammyv1.Document
	Opening                   *OpeningPayable
	CurrentRoot               bool
	Reviews                   []*tammyv1.LegacyPurchaseReview
	PriorJournalIDs           []string
	PaymentJournalIDs         []string
	MatchingOpeningPayableIDs []string
}

type purchaseCoverageEdge struct {
	sourceID  string
	journalID string
}

type purchaseCoverageJournalHeader struct {
	id, organisationID, sourceType, sourceID, state string
	version, sourceRevision                         int64
	reversalOf, reversedBy                          sql.NullString
}

type purchaseCoverageConversionHeader struct {
	id, organisationID, state, conversionDate, sourceHash, createdAt string
	version                                                          int64
	journalID, replacedBy                                            sql.NullString
	financialRevision                                                sql.NullInt64
}

func (r *Repository) CaptureCoverageForBAS(ctx context.Context, organisationID string) (*PurchaseCoverageReportingCapture, error) {
	return r.CaptureCoverageForBASWithPayloadBudget(ctx, organisationID, maximumPostingReportingPayloadSize)
}

// PurchaseCoverageReportingPreflight describes the exact bounded reservation
// used by CaptureCoverageForBASWithPayloadBudget. RootCount combines modern
// postings, documents without a modern posting, and POSTED/REPLACED opening
// payables. ReceiptCount combines modern payment rows and unique explicitly
// linked POSTED manual cash-receipt journals; opening/reversal lineage does
// not add receipts. JournalCount includes the unique complete parent closure.
// TaxFactPayloadBytes is a component of BasePayloadBytes.
type PurchaseCoverageReportingPreflight struct {
	RootCount           int
	ReceiptCount        int
	JournalCount        int
	JournalIDs          []string
	JournalPayloadBytes map[string]int64
	BasePayloadBytes    int64
	TaxFactPayloadBytes int64
	PayloadBytes        int64
}

type purchaseCoverageReportingReservation struct {
	maximumPayloadBytes      int64
	rootCount                int64
	modernPostingCount       int64
	modernPaymentCount       int64
	receiptCount             int
	receiptIDs               map[string]struct{}
	documentIDs              []string
	openingRootIDs           []string
	openingRootConversions   []string
	conversionIDs            []string
	conversionDiscoveryBytes int64
	priorEdges               []purchaseCoverageEdge
	edgePayload              int64
	operationKeys            []string
	needsExceptionRule       bool
	baseQueries              []postingReportingQuery
	preflightSizes           []int64
	preflightCounts          []int64
	reserved                 int64
	scalarJournalIDs         []string
	journalHeaders           map[string]purchaseCoverageJournalHeader
	journalIDs               []string
	accountIDs               map[string]struct{}
	allFactAccountQueries    []postingReportingQuery
	factAccountSizes         []int64
	factAccountCounts        []int64
	factAccountReserved      int64
	taxFactPayloadBytes      int64
	journalPayloadByID       map[string]int64
	journalPayloadReserved   int64
	basePayloadBytes         int64
	payloadBytes             int64
	remaining                int64
}

func purchaseCoverageReportingPreflightSnapshot(reservation *purchaseCoverageReportingReservation) *PurchaseCoverageReportingPreflight {
	if reservation == nil {
		return nil
	}
	journalIDs := append([]string(nil), reservation.journalIDs...)
	journalPayloadBytes := make(map[string]int64, len(reservation.journalPayloadByID))
	for id, charge := range reservation.journalPayloadByID {
		journalPayloadBytes[id] = charge
	}
	return &PurchaseCoverageReportingPreflight{
		RootCount:           int(reservation.rootCount),
		ReceiptCount:        reservation.receiptCount,
		JournalCount:        len(journalIDs),
		JournalIDs:          journalIDs,
		JournalPayloadBytes: journalPayloadBytes,
		BasePayloadBytes:    reservation.basePayloadBytes,
		TaxFactPayloadBytes: reservation.taxFactPayloadBytes,
		PayloadBytes:        reservation.payloadBytes,
	}
}

func (r *Repository) PreflightCoverageForBASWithPayloadBudget(
	ctx context.Context,
	organisationID string,
	maximumPayloadBytes int64,
) (*PurchaseCoverageReportingPreflight, error) {
	reservation, err := r.reserveCoverageForBAS(ctx, organisationID, maximumPayloadBytes)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return purchaseCoverageReportingPreflightSnapshot(reservation), nil
}

func (r *Repository) reserveCoverageForBAS(
	ctx context.Context,
	organisationID string,
	maximumPayloadBytes int64,
) (*purchaseCoverageReportingReservation, error) {
	if r == nil || r.executor == nil || r.now == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || maximumPayloadBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if maximumPayloadBytes > maximumPostingReportingPayloadSize {
		maximumPayloadBytes = maximumPostingReportingPayloadSize
	}

	// This is the public source-root census. A document with a modern posting
	// contributes one posting root rather than both a posting and a document
	// root. Opening payables are independent roots, including replaced ones.
	rootCount, err := purchaseCoverageRootCount(ctx, r.executor, organisationID)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if rootCount > maximumPostingReportingRoots {
		return nil, &SourceBoundError{Resource: "purchase roots", Limit: maximumPostingReportingRoots}
	}
	modernPostingCount, err := purchaseReportingOwnerCount(ctx, r.executor, purchaseReportingRootOwnerGuard(organisationID, false),
		`SELECT count(*) FROM (SELECT 1 FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY id LIMIT ?)`,
		organisationID, maximumPostingReportingRoots+1)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if modernPostingCount > maximumPostingReportingRoots {
		return nil, &SourceBoundError{Resource: "purchase roots", Limit: maximumPostingReportingRoots}
	}
	modernPaymentCount, err := purchaseReportingOwnerCount(ctx, r.executor, purchaseReportingPaymentOwnerGuard(organisationID),
		`SELECT count(*) FROM (SELECT 1 FROM purchase_payments_v1 WHERE organisation_id=? ORDER BY posting_id,accounting_version,id LIMIT ?)`,
		organisationID, maximumPostingReportingPayments+1)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if modernPaymentCount > maximumPostingReportingPayments {
		return nil, &SourceBoundError{Resource: "receipt sources", Limit: maximumPostingReportingPayments}
	}

	// Count all unique legacy cash receipt parents before exposing any journal
	// identifiers. The count uses org-scoped source subqueries and remains a
	// scalar-only check, so overflow wins before any parent hydration.
	discoveryRemaining := maximumPayloadBytes
	legacyReceiptIDs, err := purchaseCoverageLegacyReceiptIDs(ctx, r.executor, organisationID,
		int(maximumPostingReportingPayments-modernPaymentCount), &discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	receiptIDs := make(map[string]struct{}, len(legacyReceiptIDs))
	for _, id := range legacyReceiptIDs {
		receiptIDs[id] = struct{}{}
	}
	if int64(len(receiptIDs))+modernPaymentCount > maximumPostingReportingPayments {
		return nil, &SourceBoundError{Resource: "receipt sources", Limit: maximumPostingReportingPayments}
	}

	// All source IDs and reversal/replacement edges are scalar-only discovery.
	// This runs before any retained proto, original document bytes, journal line,
	// or other large column is selected into Go.
	documentIDs, _, err := purchaseCoverageDocumentIDs(ctx, r.executor, organisationID, &discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	openingRootIDs, openingRootConversions, err := purchaseCoverageOpeningRoots(ctx, r.executor, organisationID, &discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	conversionIDs, conversionDiscoveryBytes, err := purchaseCoverageConversionClosure(ctx, r.executor, organisationID,
		openingRootConversions, &discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	sourceIDs := append([]string(nil), documentIDs...)
	sourceIDs = append(sourceIDs, openingRootIDs...)
	sourceIDs = purchaseCoverageUniqueSorted(sourceIDs)
	priorEdges, edgePayload, err := purchaseCoverageManualJournalEdges(ctx, r.executor, organisationID, sourceIDs,
		&discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	operationKeys, err := purchaseCoverageOperationKeys(ctx, r.executor, organisationID, &discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	needsExceptionRule, err := purchaseCoverageNeedsExceptionRule(ctx, r.executor, organisationID)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	baseQueries := purchaseCoverageBaseQueries(organisationID, documentIDs, conversionIDs, operationKeys, needsExceptionRule)
	remaining := maximumPayloadBytes
	if edgePayload > remaining || conversionDiscoveryBytes > maximumPayloadBytes {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	remaining -= edgePayload
	preflightSizes, preflightCounts, reserved, err := purchaseCoveragePreflightQueries(ctx, r.executor, baseQueries, &remaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}

	// Discover all parent identifiers from bounded scalar columns before any
	// source protobuf, original document, line, or other body cell is loaded.
	scalarJournalIDs, err := purchaseCoverageScalarJournalIDs(ctx, r.executor, organisationID, conversionIDs,
		&discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	allJournalSet := make(map[string]struct{}, len(scalarJournalIDs)+len(priorEdges))
	for _, id := range scalarJournalIDs {
		allJournalSet[id] = struct{}{}
	}
	for _, edge := range priorEdges {
		allJournalSet[edge.journalID] = struct{}{}
	}
	journalRootIDs := purchaseCoverageMapKeys(allJournalSet)
	journalHeaders, err := purchaseCoverageJournalClosure(ctx, r.executor, organisationID, journalRootIDs,
		&discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	journalIDs := purchaseCoverageMapKeysFromHeaders(journalHeaders)

	// Reserve every fact, account, and full parent proof against the same final
	// payload budget before loading any retained SQL body.
	factQueries := postingReportingJournalFactQueries(organisationID, journalIDs)
	accountIDs, err := purchaseCoverageJournalAccountIDs(ctx, r.executor, organisationID, journalIDs,
		&discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	openingAccountIDs, err := purchaseCoverageOpeningAccountIDs(ctx, r.executor, organisationID, conversionIDs,
		&discoveryRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	for id := range openingAccountIDs {
		accountIDs[id] = struct{}{}
	}
	accountQueries := purchaseCoverageAccountQueries(organisationID, purchaseCoverageMapKeys(accountIDs), len(journalIDs) > 0)
	allFactAccountQueries := append(append([]postingReportingQuery(nil), factQueries...), accountQueries...)
	factAccountSizes, factAccountCounts, factAccountReserved, err := purchaseCoveragePreflightQueries(ctx, r.executor,
		allFactAccountQueries, &remaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	var taxFactPayloadBytes int64
	if len(allFactAccountQueries) != len(factAccountSizes) {
		return nil, ErrRepository
	}
	for index, query := range allFactAccountQueries {
		if query.table != "tax_facts" {
			continue
		}
		var ok bool
		taxFactPayloadBytes, ok = addPostingReportingBytes(taxFactPayloadBytes, factAccountSizes[index])
		if !ok || taxFactPayloadBytes > maximumPayloadBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
	}
	journalRepository, err := accounting.NewJournalRepository(r.executor)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	parentRemaining := remaining
	var journalPayloadReserved int64
	journalPayloadByID := make(map[string]int64, len(journalIDs))
	for _, journalID := range journalIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		parentBytes, err := journalRepository.PreflightForReportingWithPayloadBudget(ctx, organisationID, journalID, parentRemaining)
		if err != nil {
			var bound *accounting.TaxFactReportingBoundError
			if errors.As(err, &bound) {
				if bound.Resource == "capture bytes" {
					return nil, errors.Join(&SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}, err, ctx.Err())
				}
				return nil, errors.Join(&SourceBoundError{Resource: bound.Resource, Limit: int64(bound.Limit)}, err, ctx.Err())
			}
			return nil, errors.Join(ErrRepository, err, ctx.Err())
		}
		var ok bool
		if parentBytes < 0 {
			return nil, ErrRepository
		}
		if parentBytes > parentRemaining {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
		parentRemaining -= parentBytes
		if _, exists := journalPayloadByID[journalID]; exists {
			return nil, ErrRepository
		}
		journalPayloadByID[journalID] = parentBytes
		journalPayloadReserved, ok = addPostingReportingBytes(journalPayloadReserved, parentBytes)
		if !ok || journalPayloadReserved > maximumPayloadBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}

	basePayloadBytes, ok := addPostingReportingBytes(edgePayload, reserved)
	if !ok {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	basePayloadBytes, ok = addPostingReportingBytes(basePayloadBytes, factAccountReserved)
	if !ok || basePayloadBytes > maximumPayloadBytes || taxFactPayloadBytes > basePayloadBytes {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	payloadBytes, ok := addPostingReportingBytes(basePayloadBytes, journalPayloadReserved)
	if !ok || payloadBytes > maximumPayloadBytes {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	if len(journalPayloadByID) != len(journalIDs) {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return &purchaseCoverageReportingReservation{
		maximumPayloadBytes:      maximumPayloadBytes,
		rootCount:                rootCount,
		modernPostingCount:       modernPostingCount,
		modernPaymentCount:       modernPaymentCount,
		receiptCount:             len(receiptIDs) + int(modernPaymentCount),
		receiptIDs:               receiptIDs,
		documentIDs:              documentIDs,
		openingRootIDs:           openingRootIDs,
		openingRootConversions:   openingRootConversions,
		conversionIDs:            conversionIDs,
		conversionDiscoveryBytes: conversionDiscoveryBytes,
		priorEdges:               priorEdges,
		edgePayload:              edgePayload,
		operationKeys:            operationKeys,
		needsExceptionRule:       needsExceptionRule,
		baseQueries:              baseQueries,
		preflightSizes:           preflightSizes,
		preflightCounts:          preflightCounts,
		reserved:                 reserved,
		scalarJournalIDs:         scalarJournalIDs,
		journalHeaders:           journalHeaders,
		journalIDs:               journalIDs,
		accountIDs:               accountIDs,
		allFactAccountQueries:    allFactAccountQueries,
		factAccountSizes:         factAccountSizes,
		factAccountCounts:        factAccountCounts,
		factAccountReserved:      factAccountReserved,
		taxFactPayloadBytes:      taxFactPayloadBytes,
		journalPayloadByID:       journalPayloadByID,
		journalPayloadReserved:   journalPayloadReserved,
		basePayloadBytes:         basePayloadBytes,
		payloadBytes:             payloadBytes,
		remaining:                remaining,
	}, nil
}

func (r *Repository) CaptureCoverageForBASWithPayloadBudget(
	ctx context.Context,
	organisationID string,
	maximumPayloadBytes int64,
) (*PurchaseCoverageReportingCapture, error) {
	reservation, err := r.reserveCoverageForBAS(ctx, organisationID, maximumPayloadBytes)
	if err != nil {
		return nil, err
	}
	maximumPayloadBytes = reservation.maximumPayloadBytes
	rootCount := reservation.rootCount
	modernPostingCount := reservation.modernPostingCount
	modernPaymentCount := reservation.modernPaymentCount
	receiptIDs := reservation.receiptIDs
	documentIDs := reservation.documentIDs
	openingRootIDs := reservation.openingRootIDs
	openingRootConversions := reservation.openingRootConversions
	conversionIDs := reservation.conversionIDs
	priorEdges := reservation.priorEdges
	edgePayload := reservation.edgePayload
	operationKeys := reservation.operationKeys
	needsExceptionRule := reservation.needsExceptionRule
	baseQueries := reservation.baseQueries
	preflightSizes := reservation.preflightSizes
	preflightCounts := reservation.preflightCounts
	reserved := reservation.reserved
	scalarJournalIDs := reservation.scalarJournalIDs
	journalHeaders := reservation.journalHeaders
	journalIDs := reservation.journalIDs
	accountIDs := reservation.accountIDs
	allFactAccountQueries := reservation.allFactAccountQueries
	factAccountSizes := reservation.factAccountSizes
	factAccountCounts := reservation.factAccountCounts
	factAccountReserved := reservation.factAccountReserved
	journalPayloadReserved := reservation.journalPayloadReserved
	journalPayloadByID := reservation.journalPayloadByID
	remaining := reservation.remaining
	baseRows, loadedPayload, err := purchaseCoverageLoadQueries(ctx, r.executor, baseQueries, preflightSizes, preflightCounts, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if loadedPayload != reserved {
		return nil, ErrRepository
	}
	factAccountRows, factAccountPayload, err := purchaseCoverageLoadQueries(ctx, r.executor, allFactAccountQueries,
		factAccountSizes, factAccountCounts, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if factAccountPayload != factAccountReserved {
		return nil, ErrRepository
	}
	baseRows = append(baseRows, factAccountRows...)
	if purchaseCoverageRowsForTable(baseRows, "purchase_payments_v1") != modernPaymentCount ||
		len(journalIDs) != len(journalPayloadByID) || len(journalIDs) != len(journalHeaders) {
		return nil, ErrRepository
	}

	// Ensure every org document ID was represented by one complete SQL row.
	if purchaseCoverageRowsForTable(baseRows, "documents") != int64(len(documentIDs)) {
		return nil, ErrRepository
	}
	modernDocumentSet := make(map[string]struct{}, modernPostingCount)
	for _, row := range baseRows {
		if row.Table == "purchase_postings_v1" {
			documentID := postingReportingString(row, "document_id")
			if documentID == "" {
				return nil, ErrRepository
			}
			modernDocumentSet[documentID] = struct{}{}
		}
	}
	var documentRootCount int64
	for _, id := range documentIDs {
		if _, modern := modernDocumentSet[id]; !modern {
			documentRootCount++
		}
	}
	if int64(len(modernDocumentSet)) != modernPostingCount ||
		rootCount != modernPostingCount+documentRootCount+int64(len(openingRootIDs)) {
		return nil, ErrRepository
	}
	if err := purchaseCoverageValidateSourceRows(ctx, organisationID, documentIDs, openingRootIDs, conversionIDs,
		openingRootConversions, operationKeys, baseRows, modernPostingCount); err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	modernDocumentIDs, decodedJournalIDs, decodedNeedsExceptionRule, err := discoverPostingReportingReferences(ctx, baseRows)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if decodedNeedsExceptionRule != needsExceptionRule {
		return nil, ErrRepository
	}
	scalarJournalSet := make(map[string]struct{}, len(scalarJournalIDs))
	for _, id := range scalarJournalIDs {
		scalarJournalSet[id] = struct{}{}
	}
	for _, id := range decodedJournalIDs {
		if _, discovered := scalarJournalSet[id]; !discovered {
			return nil, ErrRepository
		}
	}
	ownedDocuments := make(map[string]struct{}, len(documentIDs))
	for _, id := range documentIDs {
		ownedDocuments[id] = struct{}{}
	}
	for _, id := range modernDocumentIDs {
		if _, ok := ownedDocuments[id]; !ok {
			return nil, ErrRepository
		}
	}
	for _, row := range baseRows {
		if row.Table == "opening_items" {
			sourceType, _ := purchaseCoverageNullableText(row, "source_type")
			sourceID, _ := purchaseCoverageNullableText(row, "source_id")
			if sourceType == "document" {
				if _, ok := ownedDocuments[sourceID]; !ok {
					return nil, ErrRepository
				}
			}
		}
		if row.Table == "tax_facts" {
			sourceType, _ := purchaseCoverageNullableText(row, "source_type")
			sourceID, _ := purchaseCoverageNullableText(row, "source_id")
			if sourceType == "document" {
				if _, ok := ownedDocuments[sourceID]; !ok {
					return nil, ErrRepository
				}
			}
		}
	}

	if err := purchaseCoverageValidateAccountRows(baseRows, accountIDs); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}

	journalRemaining := remaining
	journalRecords, journalPayload, err := loadPostingReportingJournals(ctx, r.executor, organisationID, journalIDs,
		journalRemaining, maximumPayloadBytes)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if journalPayload != journalPayloadReserved || len(journalRecords) != len(journalIDs) || len(journalPayloadByID) != len(journalIDs) {
		return nil, ErrRepository
	}
	seenJournalIDs := make(map[string]struct{}, len(journalRecords))
	for _, proof := range journalRecords {
		if proof == nil || proof.Journal == nil {
			return nil, ErrRepository
		}
		id := proof.Journal.Id
		reservedBytes, ok := journalPayloadByID[id]
		if !ok || proof.PayloadBytes != reservedBytes {
			return nil, ErrRepository
		}
		if _, duplicate := seenJournalIDs[id]; duplicate {
			return nil, ErrRepository
		}
		seenJournalIDs[id] = struct{}{}
	}
	if len(seenJournalIDs) != len(journalPayloadByID) {
		return nil, ErrRepository
	}
	remaining -= journalPayload
	if remaining < 0 {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	if err := purchaseCoverageValidateJournalClosure(journalHeaders, journalRecords); err != nil {
		return nil, err
	}
	if err := purchaseCoverageValidateCashCandidates(receiptIDs, journalRecords, baseRows); err != nil {
		return nil, err
	}

	// Accounting's mature proof is now bounded and complete. Repository reads
	// below recheck the captured state through the caller's transaction; their
	// additional bounded copies are intentionally not counted as a heap estimate.
	reader := *r
	readAsOf := r.now().UTC()
	if readAsOf.IsZero() {
		return nil, ErrRepository
	}
	reader.now = func() time.Time { return readAsOf }
	reader.reportingRootOwners = true
	postings := make([]PostingReportingRecord, 0, int(modernPostingCount))
	for _, documentID := range postingReportingRootDocumentIDs(baseRows) {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		state, err := reader.Get(ctx, organisationID, documentID)
		if err != nil {
			return nil, purchaseReportingReadError(ctx, err)
		}
		if state == nil || !state.Posted || state.Posting == nil || state.Posting.DocumentId != documentID || state.OrganisationId != organisationID {
			return nil, ErrRepository
		}
		payments, err := reader.loadPayments(ctx, state.Posting)
		if err != nil {
			return nil, purchaseReportingReadError(ctx, err)
		}
		evidences, err := reader.retainedEvidenceHistory(ctx, state.Posting)
		if err != nil || len(evidences) == 0 {
			return nil, purchaseReportingReadError(ctx, errors.Join(ErrRepository, err))
		}
		claims, err := reader.VerifiedClaims(ctx, organisationID, documentID)
		if err != nil {
			return nil, purchaseReportingReadError(ctx, err)
		}
		postings = append(postings, PostingReportingRecord{Posting: state.Posting, Accounting: state, Payments: payments, Evidences: evidences, Claims: claims})
	}
	documentRecords, documentByID, err := purchaseCoverageHydrateDocuments(ctx, &reader, organisationID, documentIDs)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	openingByID, err := purchaseCoverageHydrateOpenings(ctx, &reader, organisationID, openingRootIDs)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	conversionRepo, err := accounting.NewOpeningConversionRepository(r.executor)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	if err := purchaseCoverageVerifyConversions(ctx, conversionRepo, baseRows, conversionIDs); err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}

	legacySources, err := purchaseCoverageBuildLegacySources(ctx, &reader, organisationID, documentIDs, openingRootIDs,
		openingByID, documentByID, priorEdges, receiptIDs, baseRows, journalRecords, documentIDs)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}

	actualBasePayloadBytes, ok := addPostingReportingBytes(edgePayload, loadedPayload)
	if !ok {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	actualBasePayloadBytes, ok = addPostingReportingBytes(actualBasePayloadBytes, factAccountPayload)
	if !ok || actualBasePayloadBytes != reservation.basePayloadBytes || actualBasePayloadBytes > maximumPayloadBytes {
		return nil, ErrRepository
	}
	payloadBytes, ok := addPostingReportingBytes(actualBasePayloadBytes, journalPayload)
	if !ok || payloadBytes != reservation.payloadBytes || payloadBytes > maximumPayloadBytes {
		return nil, ErrRepository
	}
	rows := baseRows
	fingerprint, err := purchaseCoverageFingerprint(ctx, organisationID, rows, journalRecords)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	metadata := purchaseCoverageReportingPreflightSnapshot(reservation)
	return &PurchaseCoverageReportingCapture{
		OrganisationID: organisationID, Postings: postings, LegacySources: legacySources,
		Documents: documentRecords, Journals: journalRecords, Rows: rows,
		Fingerprint: fingerprint, PayloadBytes: payloadBytes,
		RootCount: metadata.RootCount, ReceiptCount: metadata.ReceiptCount, JournalCount: metadata.JournalCount,
		JournalIDs: metadata.JournalIDs, JournalPayloadBytes: metadata.JournalPayloadBytes,
		BasePayloadBytes: actualBasePayloadBytes, TaxFactPayloadBytes: reservation.taxFactPayloadBytes,
	}, nil
}

func purchaseCoverageRootCount(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string) (int64, error) {
	return purchaseReportingOwnerCount(ctx, executor, purchaseReportingRootOwnerGuard(organisationID, true), `SELECT count(*) FROM (
		SELECT id FROM (SELECT id FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY id LIMIT 1001)
		UNION ALL
		SELECT id FROM (SELECT d.id AS id FROM documents d WHERE d.organisation_id=? AND NOT EXISTS(
			SELECT 1 FROM purchase_postings_v1 p WHERE p.organisation_id=d.organisation_id AND p.document_id=d.id
		) ORDER BY d.id LIMIT 1001)
		UNION ALL
		SELECT id FROM (SELECT p.id AS id FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED') ORDER BY p.id LIMIT 1001)
	)`, organisationID, organisationID, organisationID)
}

func purchaseCoverageBoundedRows(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query postingReportingQuery, remaining *int64, limit int64) ([]PurchaseReportingRetainedRow, int64, error) {
	size, count, err := preflightPostingReportingQuery(ctx, executor, query, remaining, limit)
	if err != nil {
		return nil, 0, err
	}
	loaded, err := loadPostingReportingQuery(ctx, executor, query)
	if err != nil {
		return nil, 0, err
	}
	if loaded.payloadSize != size || int64(len(loaded.rows)) != count {
		return nil, 0, errors.Join(ErrRepository, errors.New("purchase coverage discovery changed during capture"), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, errors.Join(ErrRepository, err)
	}
	return loaded.rows, size, nil
}

func purchaseCoverageDocumentIDs(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, discoveryRemaining *int64, limit int64) ([]string, int64, error) {
	query := postingReportingQuery{table: "coverage_document_ids", alias: "d", columns: []string{"id"},
		from: "documents AS d", where: "d.organisation_id=?", args: []any{organisationID}, orderBy: "d.id",
		ownerGuard: purchaseReportingRootOwnerGuard(organisationID, true)}
	rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
	if err != nil {
		return nil, 0, err
	}
	documentIDs := make([]string, 0)
	for _, row := range rows {
		id := postingReportingString(row, "id")
		if !ids.IsCanonicalV7(id) {
			return nil, 0, ErrRepository
		}
		documentIDs = append(documentIDs, id)
	}
	return documentIDs, int64(len(documentIDs)), nil
}

func purchaseCoverageOpeningRoots(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, discoveryRemaining *int64, limit int64) ([]string, []string, error) {
	query := postingReportingQuery{table: "coverage_opening_roots", alias: "r", columns: []string{"id", "conversion_id"},
		from: `(SELECT p.id AS id,c.id AS conversion_id FROM purchase_opening_payables p
		JOIN opening_conversions c ON c.id=p.conversion_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED')) AS r`,
		where: "1=1", args: []any{organisationID}, orderBy: "r.id"}
	rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
	if err != nil {
		return nil, nil, err
	}
	payableIDs := make([]string, 0, len(rows))
	conversionIDs := make([]string, 0, len(rows))
	seenConversions := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		payableID := postingReportingString(row, "id")
		conversionID := postingReportingString(row, "conversion_id")
		if !ids.IsCanonicalV7(payableID) || !ids.IsCanonicalV7(conversionID) {
			return nil, nil, ErrRepository
		}
		payableIDs = append(payableIDs, payableID)
		if _, seen := seenConversions[conversionID]; !seen {
			seenConversions[conversionID] = struct{}{}
			conversionIDs = append(conversionIDs, conversionID)
		}
	}
	sort.Strings(payableIDs)
	sort.Strings(conversionIDs)
	return payableIDs, conversionIDs, nil
}

func purchaseCoverageConversionClosure(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, roots []string, discoveryRemaining *int64, limit int64) ([]string, int64, error) {
	roots = purchaseCoverageUniqueSorted(roots)
	if len(roots) == 0 {
		return nil, 0, nil
	}
	rootSet := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if !ids.IsCanonicalV7(root) {
			return nil, 0, ErrRepository
		}
		rootSet[root] = struct{}{}
	}
	placeholders := purchaseCoveragePlaceholders(len(roots))
	if *discoveryRemaining < 0 {
		return nil, 0, ErrRepository
	}
	// The recursive CTE can otherwise materialize a long replacement chain
	// before the SQL byte preflight sees its rows. Its own row limit is chosen
	// one above the remaining-budget-derived minimum, so an overlong component
	// is proven over budget while SQLite and Go retain only a bounded prefix.
	maximumRows := *discoveryRemaining/postingReportingRowOverhead + 1
	closureCTE := `WITH RECURSIVE closure(id) AS (
		SELECT id FROM opening_conversions WHERE organisation_id=? AND id IN (` + placeholders + `)
		UNION
		SELECT c.replaced_by_id FROM opening_conversions c JOIN closure prior ON prior.id=c.id
		WHERE c.organisation_id=? AND c.replaced_by_id IS NOT NULL
		UNION
		SELECT c.id FROM opening_conversions c JOIN closure next ON next.id=c.replaced_by_id
		WHERE c.organisation_id=?
		LIMIT ?
	)`
	query := postingReportingQuery{
		table: "coverage_conversion_closure", alias: "c",
		columns: []string{"id", "organisation_id", "replaced_by_id"},
		from: `(` + closureCTE + ` SELECT c.id AS id,c.organisation_id AS organisation_id,c.replaced_by_id AS replaced_by_id
		FROM opening_conversions c JOIN closure discovered ON discovered.id=c.id WHERE c.organisation_id=?) AS c`,
		where: "1=1", orderBy: "c.id",
	}
	query.args = []any{organisationID}
	query.args = append(query.args, batchArgs(roots)...)
	query.args = append(query.args, organisationID, organisationID, maximumRows, organisationID)
	rows, charged, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
	if err != nil {
		return nil, 0, err
	}
	idsFound := make([]string, 0, len(rows))
	replacedBy := make(map[string]string, len(rows))
	parent := make(map[string]string, len(rows))
	for _, row := range rows {
		id := postingReportingString(row, "id")
		owner := postingReportingString(row, "organisation_id")
		target, targetPresent := purchaseCoverageNullableText(row, "replaced_by_id")
		if !ids.IsCanonicalV7(id) || owner != organisationID {
			return nil, 0, ErrRepository
		}
		idsFound = append(idsFound, id)
		if targetPresent {
			if !ids.IsCanonicalV7(target) || target == id {
				return nil, 0, ErrRepository
			}
			replacedBy[id] = target
			if previous, exists := parent[target]; exists && previous != id {
				return nil, 0, ErrRepository
			}
			parent[target] = id
		}
	}
	found := make(map[string]struct{}, len(idsFound))
	for _, id := range idsFound {
		found[id] = struct{}{}
	}
	// Incoming edges from another organisation are not part of this owner's
	// closure. Detect them with scalar probes so foreign raw identifiers never
	// enter the retained rows or bypass the byte preflight.
	for _, batch := range purchaseCoverageIDBatches(idsFound, 400) {
		foreignCount, err := postingReportingCount(ctx, executor,
			"SELECT count(*) FROM opening_conversions WHERE organisation_id<>? AND replaced_by_id IN ("+purchaseCoveragePlaceholders(len(batch))+")",
			append([]any{organisationID}, batchArgs(batch)...)...)
		if err != nil {
			return nil, 0, err
		}
		if foreignCount != 0 {
			return nil, 0, ErrRepository
		}
	}
	for root := range rootSet {
		if _, ok := found[root]; !ok {
			return nil, 0, ErrRepository
		}
	}
	for _, target := range replacedBy {
		if _, ok := found[target]; !ok {
			// A foreign or absent target is an invalid retained ownership edge.
			return nil, 0, ErrRepository
		}
	}
	visited := make(map[string]uint8, len(idsFound))
	for _, id := range idsFound {
		if visited[id] == 2 {
			continue
		}
		path := make([]string, 0)
		for next := id; next != "" && visited[next] != 2; next = replacedBy[next] {
			if err := ctx.Err(); err != nil {
				return nil, 0, errors.Join(ErrRepository, err)
			}
			if visited[next] == 1 {
				return nil, 0, ErrRepository
			}
			visited[next] = 1
			path = append(path, next)
		}
		for _, member := range path {
			visited[member] = 2
		}
	}
	return idsFound, charged, nil
}

func purchaseCoverageManualJournalEdges(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, sourceIDs []string, discoveryRemaining *int64, limit int64) ([]purchaseCoverageEdge, int64, error) {
	edges := make([]purchaseCoverageEdge, 0)
	seen := make(map[purchaseCoverageEdge]struct{})
	var charged int64
	for _, batch := range purchaseCoverageIDBatches(sourceIDs, 400) {
		placeholders := purchaseCoveragePlaceholders(len(batch))
		args := []any{organisationID}
		args = append(args, batchArgs(batch)...)
		args = append(args, organisationID, organisationID)
		args = append(args, batchArgs(batch)...)
		query := postingReportingQuery{
			table: "coverage_prior_edges", alias: "e", columns: []string{"source_id", "journal_id"},
			from: `(SELECT source_id,journal_id FROM (
			SELECT j.source_id AS source_id,j.id AS journal_id FROM journals j
			WHERE j.organisation_id=? AND j.source_kind=0 AND j.source_type<>'OPENING' AND j.source_id IN (` + placeholders + `)
			UNION
			SELECT f.source_id AS source_id,j.id AS journal_id FROM tax_facts f
			JOIN journal_lines l ON l.id=f.journal_line_id JOIN journals j ON j.id=l.journal_id
			WHERE f.organisation_id=? AND j.organisation_id=? AND j.source_kind=0 AND j.source_type<>'OPENING' AND f.source_id IN (` + placeholders + `)
			) ORDER BY source_id,journal_id) AS e`,
			where: "1=1", args: args, orderBy: "e.source_id,e.journal_id",
		}
		rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
		if err != nil {
			return nil, 0, err
		}
		for _, row := range rows {
			edge := purchaseCoverageEdge{sourceID: postingReportingString(row, "source_id"), journalID: postingReportingString(row, "journal_id")}
			if edge.sourceID == "" || edge.journalID == "" {
				return nil, 0, ErrRepository
			}
			if _, duplicate := seen[edge]; duplicate {
				continue
			}
			charge, ok := addPostingReportingBytes(postingReportingRowOverhead, int64(len(edge.sourceID)+len(edge.journalID)))
			if !ok {
				return nil, 0, &SourceBoundError{Resource: "capture bytes", Limit: limit}
			}
			charged, ok = addPostingReportingBytes(charged, charge)
			if !ok || charged > limit {
				return nil, 0, &SourceBoundError{Resource: "capture bytes", Limit: limit}
			}
			seen[edge] = struct{}{}
			edges = append(edges, edge)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, errors.Join(ErrRepository, err)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].sourceID != edges[j].sourceID {
			return edges[i].sourceID < edges[j].sourceID
		}
		return edges[i].journalID < edges[j].journalID
	})
	return edges, charged, nil
}

func purchaseCoverageLegacyReceiptIDs(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, remainingCapacity int, discoveryRemaining *int64, limit int64) ([]string, error) {
	if remainingCapacity < 0 {
		return nil, &SourceBoundError{Resource: "receipt sources", Limit: maximumPostingReportingPayments}
	}
	const sourceCTE = `WITH source_ids(id) AS (
		SELECT id FROM documents WHERE organisation_id=?
		UNION SELECT p.id FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED')
	)`
	const eligibleFrom = ` FROM journals j
		WHERE j.organisation_id=? AND j.state='POSTED' AND j.source_kind=0
		AND j.source_type<>'OPENING' AND j.source_type<>'REVERSAL'
		AND EXISTS (
			SELECT 1 FROM journal_lines l JOIN accounts a ON a.id=l.account_id AND a.organisation_id=j.organisation_id
			WHERE l.journal_id=j.id AND a.report_classification IN ('balance_sheet.cash','balance_sheet.cash_equivalent')
			AND l.credit_minor>l.debit_minor
		)
		AND (j.source_id IN (SELECT id FROM source_ids) OR EXISTS (
			SELECT 1 FROM tax_facts f JOIN journal_lines source_line ON source_line.id=f.journal_line_id
			WHERE f.organisation_id=j.organisation_id AND source_line.journal_id=j.id
			AND f.source_id IN (SELECT id FROM source_ids)
		))`
	countArgs := []any{organisationID, organisationID, organisationID}
	candidateCount, err := postingReportingCount(ctx, executor,
		sourceCTE+` SELECT count(DISTINCT j.id)`+eligibleFrom, countArgs...)
	if err != nil {
		return nil, purchaseReportingReadError(ctx, err)
	}
	if candidateCount > int64(remainingCapacity) {
		return nil, &SourceBoundError{Resource: "receipt sources", Limit: maximumPostingReportingPayments}
	}
	if candidateCount == 0 {
		return nil, nil
	}
	query := postingReportingQuery{
		table: "coverage_receipt_ids", alias: "r", columns: []string{"id"},
		from:  `(` + sourceCTE + ` SELECT DISTINCT j.id AS id` + eligibleFrom + `) AS r`,
		where: "1=1", args: countArgs, orderBy: "r.id",
	}
	query.limit = " LIMIT ?"
	query.args = append(query.args, remainingCapacity+1)
	preflightQuery := query
	preflightQuery.args = query.args[:len(query.args)-1]
	size, count, err := preflightPostingReportingQuery(ctx, executor, preflightQuery, discoveryRemaining, limit)
	if err != nil {
		return nil, err
	}
	if count > int64(remainingCapacity) {
		return nil, &SourceBoundError{Resource: "receipt sources", Limit: maximumPostingReportingPayments}
	}
	loaded, err := loadPostingReportingQuery(ctx, executor, query)
	if err != nil {
		return nil, err
	}
	if loaded.payloadSize != size || int64(len(loaded.rows)) != candidateCount || count != candidateCount {
		return nil, errors.Join(ErrRepository, errors.New("purchase receipt discovery changed during capture"), ctx.Err())
	}
	result := make([]string, 0, len(loaded.rows))
	for _, row := range loaded.rows {
		id := postingReportingString(row, "id")
		if id == "" {
			return nil, ErrRepository
		}
		result = append(result, id)
	}
	return result, nil
}

func purchaseCoverageOperationKeys(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, discoveryRemaining *int64, limit int64) ([]string, error) {
	query := postingReportingQuery{table: "coverage_operation_keys", alias: "e", columns: []string{"operation_key"},
		from: "purchase_accounting_events_v1 AS e", where: "e.organisation_id=?", args: []any{organisationID}, orderBy: "e.operation_key",
		ownerGuard: purchaseReportingOwnerGuardForSet(organisationID, purchaseOwnerCoverageEvents)}
	rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		key := postingReportingString(row, "operation_key")
		if key == "" {
			return nil, ErrRepository
		}
		result = append(result, key)
	}
	return result, nil
}

func purchaseCoverageNeedsExceptionRule(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string) (bool, error) {
	count, err := purchaseReportingOwnerCount(ctx, executor, purchaseReportingOwnerGuardForSet(organisationID, purchaseOwnersEvidence),
		"SELECT count(*) FROM purchase_tax_evidence_v1 WHERE organisation_id=? AND classification=?",
		organisationID, int64(tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION))
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func purchaseCoverageScalarJournalIDs(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, conversionIDs []string, discoveryRemaining *int64, limit int64) ([]string, error) {
	queries := []postingReportingQuery{
		{table: "coverage_journal_ids", alias: "j", columns: []string{"journal_id"}, from: "purchase_postings_v1 AS j", where: "j.organisation_id=?", args: []any{organisationID}, orderBy: "j.id"},
		{table: "coverage_journal_ids", alias: "j", columns: []string{"journal_id"}, from: "purchase_payments_v1 AS j", where: "j.organisation_id=?", args: []any{organisationID}, orderBy: "j.id", ownerGuard: purchaseReportingPaymentOwnerGuard(organisationID)},
		{table: "coverage_journal_ids", alias: "j", columns: []string{"journal_id"}, from: "purchase_accounting_events_v1 AS j", where: "j.organisation_id=?", args: []any{organisationID}, orderBy: "j.operation_key", ownerGuard: purchaseReportingOwnerGuardForSet(organisationID, purchaseOwnerCoverageEvents)},
		{table: "coverage_journal_ids", alias: "j", columns: []string{"journal_id"}, from: "purchase_gst_attributions_v1 AS j", where: "j.organisation_id=?", args: []any{organisationID}, orderBy: "j.operation_key,j.id", ownerGuard: purchaseReportingOwnerGuardForSet(organisationID, purchaseOwnersAttribution)},
	}
	for _, batch := range purchaseCoverageIDBatches(conversionIDs, 400) {
		queries = append(queries, postingReportingQuery{
			table: "coverage_journal_ids", alias: "c", columns: []string{"journal_id"},
			from: "opening_conversions AS c", where: "c.organisation_id=? AND c.id IN (" + purchaseCoveragePlaceholders(len(batch)) + ")",
			args: append([]any{organisationID}, batchArgs(batch)...), orderBy: "c.id",
		})
	}
	result := make(map[string]struct{})
	for _, query := range queries {
		rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			id, present := purchaseCoverageNullableText(row, "journal_id")
			if !present {
				continue
			}
			if strings.TrimSpace(id) == "" {
				return nil, ErrRepository
			}
			result[id] = struct{}{}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return purchaseCoverageMapKeys(result), nil
}

func purchaseCoverageOpeningAccountIDs(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, conversionIDs []string, discoveryRemaining *int64, limit int64) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for _, batch := range purchaseCoverageIDBatches(conversionIDs, 400) {
		query := postingReportingQuery{
			table: "coverage_opening_account_ids", alias: "a", columns: []string{"account_id"},
			from: `(SELECT DISTINCT i.account_id AS account_id FROM opening_items i
			JOIN opening_conversions c ON c.id=i.conversion_id
			WHERE c.organisation_id=? AND c.id IN (` + purchaseCoveragePlaceholders(len(batch)) + `)) AS a`,
			where: "1=1", args: append([]any{organisationID}, batchArgs(batch)...), orderBy: "a.account_id",
		}
		rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			id := postingReportingString(row, "account_id")
			if strings.TrimSpace(id) == "" {
				return nil, ErrRepository
			}
			result[id] = struct{}{}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return result, nil
}

func purchaseCoverageBaseQueries(organisationID string, documentIDs, conversionIDs, operationKeys []string, includeExceptionRule bool) []postingReportingQuery {
	queries := make([]postingReportingQuery, 0, 32)
	for _, query := range postingReportingSourceQueries(organisationID) {
		switch query.table {
		case "purchase_accounting_events_v1", "financial_revision_claims", "command_idempotency_v1":
			continue
		}
		if query.table == "purchase_postings_v1" {
			query.ownerGuard = purchaseReportingRootOwnerGuard(organisationID, true)
		}
		queries = append(queries, query)
	}
	queries = append(queries, purchaseCoverageEventQueries(organisationID)...)
	queries = append(queries, purchaseCoverageLegacyReviewQuery(organisationID))
	queries = append(queries, purchaseCoverageDocumentQueries(organisationID, documentIDs)...)
	queries = append(queries, purchaseCoverageOpeningQueries(organisationID, conversionIDs)...)
	queries = append(queries, purchaseCoverageOperationQueries(organisationID, operationKeys)...)
	if includeExceptionRule {
		queries = append(queries, postingReportingLowValueRuleQuery())
	}
	return queries
}

func purchaseCoverageEventQueries(organisationID string) []postingReportingQuery {
	return []postingReportingQuery{{
		table: "purchase_accounting_events_v1", alias: "e",
		columns: strings.Fields("operation_key organisation_id scope_kind scope_id posting_id document_id opening_payable_id accounting_version command_type financial_revision ledger_revision tax_source_revision settlement_revision result_sha256 result_proto journal_id created_at"),
		from:    "purchase_accounting_events_v1 AS e", where: "e.organisation_id=?", args: []any{organisationID}, orderBy: "e.scope_kind,e.scope_id,e.accounting_version,e.operation_key",
		ownerGuard: purchaseReportingOwnerGuardForSet(organisationID, purchaseOwnerCoverageEvents),
	}}
}

func purchaseCoverageLegacyReviewQuery(organisationID string) postingReportingQuery {
	return postingReportingQuery{
		table: "purchase_legacy_reviews_v1", alias: "r",
		columns: strings.Fields("id organisation_id source_kind document_id opening_payable_id source_version source_hash reviewed_snapshot_hash accounting_version affected_period_start affected_period_end reporting_balance_date disposition previously_claimed_period_start previously_claimed_period_end claim_evidence_source_id claim_evidence_source_version claim_evidence_source_hash linked_posting_id reason review_proto record_hash actor_user_id created_at"),
		from:    "purchase_legacy_reviews_v1 AS r", where: "r.organisation_id=?", args: []any{organisationID}, orderBy: "r.source_kind,COALESCE(r.document_id,r.opening_payable_id),r.accounting_version,r.id",
	}
}

func purchaseCoverageDocumentQueries(organisationID string, documentIDs []string) []postingReportingQuery {
	return postingReportingDocumentQueries(organisationID, documentIDs)
}

func purchaseCoverageOpeningQueries(organisationID string, conversionIDs []string) []postingReportingQuery {
	queries := make([]postingReportingQuery, 0, 3*((len(conversionIDs)+399)/400))
	for _, batch := range purchaseCoverageIDBatches(conversionIDs, 400) {
		placeholders := purchaseCoveragePlaceholders(len(batch))
		args := []any{organisationID}
		args = append(args, batchArgs(batch)...)
		queries = append(queries, postingReportingQuery{
			table: "opening_conversions", alias: "c",
			columns: strings.Fields("id organisation_id conversion_date state source_sha256 journal_id version replaced_by_id financial_revision created_at"),
			from:    "opening_conversions AS c", where: "c.organisation_id=? AND c.id IN (" + placeholders + ")", args: args, orderBy: "c.id",
		})
		payableArgs := append([]any(nil), batchArgs(batch)...)
		queries = append(queries, postingReportingQuery{
			table: "purchase_opening_payables", alias: "p",
			columns: strings.Fields("id conversion_id retained_input_proto created_at"),
			from:    "purchase_opening_payables AS p", where: "p.conversion_id IN (" + placeholders + ")", args: payableArgs, orderBy: "p.id",
		})
		itemArgs := append([]any(nil), batchArgs(batch)...)
		queries = append(queries, postingReportingQuery{
			table: "opening_items", alias: "i",
			columns: strings.Fields("id conversion_id account_id item_kind debit_minor credit_minor currency_code source_type source_id source_revision source_content_hash original_issue_date original_due_date outstanding_gst_minor prior_gst_attributed_minor latest_statement_date latest_statement_balance_minor"),
			from:    "opening_items AS i", where: "i.conversion_id IN (" + placeholders + ")", args: itemArgs, orderBy: "i.conversion_id,i.id",
		})
	}
	return queries
}

func purchaseCoverageOperationQueries(organisationID string, operationKeys []string) []postingReportingQuery {
	queries := make([]postingReportingQuery, 0, 3*((len(operationKeys)+399)/400))
	for _, batch := range purchaseCoverageIDBatches(operationKeys, 400) {
		guard := purchaseReportingCoverageOperationOwnerGuard(organisationID, batch)
		placeholders := purchaseCoveragePlaceholders(len(batch))
		args := append([]any(nil), batchArgs(batch)...)
		queries = append(queries, postingReportingQuery{
			table: "financial_revision_claims", alias: "c",
			columns: strings.Fields("operation_key domain_mask financial_revision ledger_revision settlement_revision banking_revision tax_source_revision organisation_profile_revision rule_bundle_revision updated_at"),
			from:    "financial_revision_claims AS c", where: "c.operation_key IN (" + placeholders + ")", args: args, orderBy: "c.operation_key",
			ownerGuard: guard,
		})
		idemArgs := append([]any(nil), batchArgs(batch)...)
		queries = append(queries, postingReportingQuery{
			table: "command_idempotency_v1", alias: "c",
			columns: strings.Fields("workspace_id actor_user_id fully_qualified_rpc_name operation_key semantic_hash_version request_type normalized_hash result_type result_proto outcome failure_code result_resource_id attempt retention_policy created_at completed_at"),
			from:    "command_idempotency_v1 AS c", where: "c.operation_key IN (" + placeholders + ")", args: idemArgs, orderBy: "c.operation_key",
			ownerGuard: guard,
		})
		auditArgs := []any{}
		auditArgs = append(auditArgs, batchArgs(batch)...)
		queries = append(queries, postingReportingQuery{
			table: "purchase_command_audit_v1", alias: "a",
			columns: strings.Fields("operation_key organisation_id workspace_id actor_user_id session_id command_type result_sha256 payload_proto occurred_at"),
			from:    "purchase_command_audit_v1 AS a", where: "a.operation_key IN (" + placeholders + ")", args: auditArgs, orderBy: "a.operation_key",
			ownerGuard: guard,
		})
	}
	return queries
}

func purchaseCoveragePreflightQueries(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, queries []postingReportingQuery, remaining *int64, limit int64) ([]int64, []int64, int64, error) {
	sizes := make([]int64, 0, len(queries))
	counts := make([]int64, 0, len(queries))
	var total int64
	for _, query := range queries {
		if err := ctx.Err(); err != nil {
			return nil, nil, 0, errors.Join(ErrRepository, err)
		}
		size, count, err := preflightPostingReportingQuery(ctx, executor, query, remaining, limit)
		if err != nil {
			return nil, nil, 0, err
		}
		var ok bool
		total, ok = addPostingReportingBytes(total, size)
		if !ok {
			return nil, nil, 0, &SourceBoundError{Resource: "capture bytes", Limit: limit}
		}
		sizes = append(sizes, size)
		counts = append(counts, count)
	}
	return sizes, counts, total, nil
}

func purchaseCoverageLoadQueries(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, queries []postingReportingQuery, sizes, counts []int64, limit int64) ([]PurchaseReportingRetainedRow, int64, error) {
	if len(queries) != len(sizes) || len(queries) != len(counts) {
		return nil, 0, ErrRepository
	}
	rows := make([]PurchaseReportingRetainedRow, 0, len(queries)*4)
	var total int64
	for index, query := range queries {
		if err := ctx.Err(); err != nil {
			return nil, 0, errors.Join(ErrRepository, err)
		}
		loaded, err := loadPostingReportingQuery(ctx, executor, query)
		if err != nil {
			return nil, 0, err
		}
		if loaded.payloadSize != sizes[index] || int64(len(loaded.rows)) != counts[index] {
			return nil, 0, errors.Join(ErrRepository, errors.New("purchase coverage source changed during capture"), ctx.Err())
		}
		var ok bool
		total, ok = addPostingReportingBytes(total, loaded.payloadSize)
		if !ok || total > limit {
			return nil, 0, &SourceBoundError{Resource: "capture bytes", Limit: limit}
		}
		rows = append(rows, loaded.rows...)
	}
	return rows, total, nil
}

func purchaseCoverageValidateSourceRows(ctx context.Context, organisationID string, documentIDs, openingRootIDs, conversionIDs []string,
	openingRootConversions []string, operationKeys []string, rows []PurchaseReportingRetainedRow, modernCount int64) error {
	docSet := make(map[string]struct{}, len(documentIDs))
	for _, id := range documentIDs {
		docSet[id] = struct{}{}
	}
	conversionSet := make(map[string]struct{}, len(conversionIDs))
	for _, id := range conversionIDs {
		conversionSet[id] = struct{}{}
	}
	openingSet := make(map[string]struct{}, len(openingRootIDs))
	for _, id := range openingRootIDs {
		openingSet[id] = struct{}{}
	}
	opKeySet := make(map[string]struct{}, len(operationKeys))
	for _, key := range operationKeys {
		opKeySet[key] = struct{}{}
	}
	postingSet := make(map[string]struct{}, modernCount)
	var postingRows int64
	reviewRows := make(map[string][]PurchaseReportingRetainedRow)
	openingRows := make(map[string][]PurchaseReportingRetainedRow)
	eventRows := make([]PurchaseReportingRetainedRow, 0)
	opKeysSeen := make(map[string]struct{})
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		switch row.Table {
		case "purchase_postings_v1":
			id := postingReportingString(row, "id")
			docID := postingReportingString(row, "document_id")
			if id == "" || !ids.IsCanonicalV7(id) || docID == "" {
				return ErrRepository
			}
			postingSet[id] = struct{}{}
			postingRows++
		case "purchase_legacy_reviews_v1":
			kind := postingReportingString(row, "source_kind")
			id := postingReportingNullableString(row, "document_id")
			if kind == "OPENING_PAYABLE" {
				id = postingReportingNullableString(row, "opening_payable_id")
			}
			if kind != "DOCUMENT" && kind != "OPENING_PAYABLE" || id == "" || postingReportingString(row, "organisation_id") != organisationID {
				return ErrRepository
			}
			if kind == "DOCUMENT" {
				if _, ok := docSet[id]; !ok {
					return ErrRepository
				}
			} else if _, ok := openingSet[id]; !ok {
				return ErrRepository
			}
			reviewRows[kind+":"+id] = append(reviewRows[kind+":"+id], row)
		case "purchase_accounting_events_v1":
			key := postingReportingString(row, "operation_key")
			if _, ok := opKeySet[key]; !ok || key == "" {
				return ErrRepository
			}
			if _, duplicate := opKeysSeen[key]; duplicate {
				return ErrRepository
			}
			opKeysSeen[key] = struct{}{}
			eventRows = append(eventRows, row)
		case "opening_conversions":
			id := postingReportingString(row, "id")
			if _, ok := conversionSet[id]; !ok || postingReportingString(row, "organisation_id") != organisationID {
				return ErrRepository
			}
		case "purchase_opening_payables":
			id := postingReportingString(row, "id")
			conversionID := postingReportingString(row, "conversion_id")
			if !ids.IsCanonicalV7(id) {
				return ErrRepository
			}
			if _, ok := conversionSet[conversionID]; !ok {
				return ErrRepository
			}
			openingRows[id] = append(openingRows[id], row)
		}
	}
	if postingRows != modernCount || int64(len(opKeysSeen)) != int64(len(operationKeys)) {
		return ErrRepository
	}
	for _, event := range eventRows {
		if postingReportingString(event, "organisation_id") != organisationID {
			return ErrRepository
		}
		scopeKind, scopeID := postingReportingString(event, "scope_kind"), postingReportingString(event, "scope_id")
		switch scopeKind {
		case "POSTING":
			if _, ok := postingSet[scopeID]; !ok {
				return ErrRepository
			}
		case "DOCUMENT":
			if _, ok := docSet[scopeID]; !ok {
				return ErrRepository
			}
		case "OPENING_PAYABLE":
			if len(openingRows[scopeID]) != 1 {
				return ErrRepository
			}
		default:
			return ErrRepository
		}
	}
	for _, id := range openingRootIDs {
		if len(openingRows[id]) != 1 {
			return ErrRepository
		}
	}
	for _, event := range eventRows {
		key := postingReportingString(event, "operation_key")
		for _, table := range []string{"financial_revision_claims", "command_idempotency_v1"} {
			if purchaseCoverageCountText(rows, table, "operation_key", key) != 1 {
				return ErrRepository
			}
		}
		if purchaseCoverageCountText(rows, "purchase_command_audit_v1", "operation_key", key) > 1 {
			return ErrRepository
		}
	}
	for _, row := range rows {
		if row.Table == "purchase_command_audit_v1" {
			key := postingReportingString(row, "operation_key")
			eventFound := false
			for _, event := range eventRows {
				if postingReportingString(event, "operation_key") == key {
					eventFound = true
					commandType := postingReportingString(event, "command_type")
					if !ids.IsCanonicalV7(key) || postingReportingString(row, "organisation_id") != organisationID ||
						postingReportingString(row, "command_type") != commandType ||
						!ids.IsCanonicalV7(postingReportingString(row, "workspace_id")) ||
						!ids.IsCanonicalV7(postingReportingString(row, "actor_user_id")) ||
						!ids.IsCanonicalV7(postingReportingString(row, "session_id")) {
						return ErrRepository
					}
					eventHash, _ := postingReportingBytes(event, "result_sha256")
					auditHash, _ := postingReportingBytes(row, "result_sha256")
					eventResult, resultPresent := postingReportingBytes(event, "result_proto")
					auditPayload, payloadPresent := postingReportingBytes(row, "payload_proto")
					expectedResultType, expectedPayload, payloadErr := purchaseCoverageExpectedCommandAuditPayload(commandType, eventResult)
					eventDigest := sha256.Sum256(eventResult)
					if !resultPresent || !payloadPresent || len(auditPayload) == 0 || len(auditPayload) > 1<<20 ||
						payloadErr != nil || !bytes.Equal(expectedPayload, auditPayload) ||
						!bytes.Equal(eventHash, auditHash) || !bytes.Equal(eventHash, eventDigest[:]) {
						return ErrRepository
					}
					idempotency, idempotencyFound := purchaseCoverageRowForKey(rows, "command_idempotency_v1", key)
					if !idempotencyFound || postingReportingString(idempotency, "workspace_id") != postingReportingString(row, "workspace_id") ||
						postingReportingString(idempotency, "actor_user_id") != postingReportingString(row, "actor_user_id") ||
						postingReportingString(idempotency, "fully_qualified_rpc_name") != commandType ||
						postingReportingString(idempotency, "outcome") != "COMMITTED" ||
						postingReportingString(idempotency, "result_type") != expectedResultType {
						return ErrRepository
					}
					idempotentResult, ok := postingReportingBytes(idempotency, "result_proto")
					if !ok || !bytes.Equal(idempotentResult, eventResult) {
						return ErrRepository
					}
					occurredAt, err := time.Parse(time.RFC3339Nano, postingReportingString(row, "occurred_at"))
					if err != nil || occurredAt.IsZero() {
						return ErrRepository
					}
					break
				}
			}
			if !eventFound {
				return ErrRepository
			}
		}
	}
	return nil
}

func purchaseCoverageExpectedCommandAuditPayload(commandType string, eventResult []byte) (string, []byte, error) {
	var result proto.Message
	var payload proto.Message
	resultType := ""
	switch commandType {
	case purchaseService + "PostReviewedPurchase":
		response := &tammyv1.PostReviewedPurchaseResponse{}
		if proto.Unmarshal(eventResult, response) != nil || !canonicalPurchaseBytes(response, eventResult) || response.Posting == nil || response.Accounting == nil {
			return "", nil, ErrRepository
		}
		result, payload, resultType = response, response.Posting, "tammy.v1.PostReviewedPurchaseResponse"
	case purchaseService + "RecordPurchasePayment":
		response := &tammyv1.RecordPurchasePaymentResponse{}
		if proto.Unmarshal(eventResult, response) != nil || !canonicalPurchaseBytes(response, eventResult) || response.Payment == nil || response.Accounting == nil {
			return "", nil, ErrRepository
		}
		result, payload, resultType = response, response.Payment, "tammy.v1.RecordPurchasePaymentResponse"
	case purchaseService + "RecordPurchaseTaxEvidence":
		response := &tammyv1.RecordPurchaseTaxEvidenceResponse{}
		if proto.Unmarshal(eventResult, response) != nil || !canonicalPurchaseBytes(response, eventResult) || response.Evidence == nil || response.Accounting == nil {
			return "", nil, ErrRepository
		}
		result, payload, resultType = response, response.Evidence, "tammy.v1.RecordPurchaseTaxEvidenceResponse"
	case purchaseService + "AttributePurchaseGst":
		response := &tammyv1.AttributePurchaseGstResponse{}
		if proto.Unmarshal(eventResult, response) != nil || !canonicalPurchaseBytes(response, eventResult) || response.Accounting == nil || response.Journal == nil || len(response.Attributions) == 0 || response.Attributions[0] == nil {
			return "", nil, ErrRepository
		}
		result, payload, resultType = response, response, "tammy.v1.AttributePurchaseGstResponse"
	case purchaseService + "DeferPurchaseGstClaim":
		response := &tammyv1.DeferPurchaseGstClaimResponse{}
		if proto.Unmarshal(eventResult, response) != nil || !canonicalPurchaseBytes(response, eventResult) || response.Deferral == nil || response.Accounting == nil {
			return "", nil, ErrRepository
		}
		result, payload, resultType = response, response.Deferral, "tammy.v1.DeferPurchaseGstClaimResponse"
	case purchaseService + "ResolveLegacyPurchaseReview":
		response := &tammyv1.ResolveLegacyPurchaseReviewResponse{}
		if proto.Unmarshal(eventResult, response) != nil || !canonicalPurchaseBytes(response, eventResult) || response.Review == nil {
			return "", nil, ErrRepository
		}
		result, payload, resultType = response, response.Review, "tammy.v1.ResolveLegacyPurchaseReviewResponse"
	default:
		return "", nil, ErrRepository
	}
	if result == nil || payload == nil {
		return "", nil, ErrRepository
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil || len(encoded) == 0 || len(encoded) > 1<<20 {
		return "", nil, ErrRepository
	}
	return resultType, encoded, nil
}

func purchaseCoverageRowForKey(rows []PurchaseReportingRetainedRow, table, key string) (PurchaseReportingRetainedRow, bool) {
	var found PurchaseReportingRetainedRow
	count := 0
	for _, row := range rows {
		if row.Table == table && postingReportingString(row, "operation_key") == key {
			found = row
			count++
		}
	}
	return found, count == 1
}

func purchaseCoverageJournalClosure(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, roots []string, discoveryRemaining *int64, limit int64) (map[string]purchaseCoverageJournalHeader, error) {
	result := make(map[string]purchaseCoverageJournalHeader, len(roots))
	queue := purchaseCoverageUniqueSorted(roots)
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		pending := make([]string, 0, len(queue))
		for _, id := range queue {
			if _, known := result[id]; !known {
				pending = append(pending, id)
			}
		}
		if len(pending) == 0 {
			break
		}
		next := make(map[string]struct{})
		for _, batch := range purchaseCoverageIDBatches(pending, 400) {
			query := postingReportingQuery{
				table: "coverage_journal_headers", alias: "j",
				columns: strings.Fields("id organisation_id version source_type source_id source_revision state reversal_of_journal_id reversed_by_journal_id"),
				from:    "journals AS j", where: "j.organisation_id=? AND j.id IN (" + purchaseCoveragePlaceholders(len(batch)) + ")",
				args: append([]any{organisationID}, batchArgs(batch)...), orderBy: "j.id",
			}
			rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
			if err != nil {
				return nil, err
			}
			if len(rows) != len(batch) {
				return nil, ErrRepository
			}
			for _, row := range rows {
				value := purchaseCoverageJournalHeader{
					id: postingReportingString(row, "id"), organisationID: postingReportingString(row, "organisation_id"),
					sourceType: postingReportingString(row, "source_type"), sourceID: postingReportingString(row, "source_id"),
					state: postingReportingString(row, "state"),
				}
				value.version, _ = purchaseCoverageInteger(row, "version")
				value.sourceRevision, _ = purchaseCoverageInteger(row, "source_revision")
				if value.id == "" || value.organisationID != organisationID || value.version <= 0 || value.sourceRevision < 0 {
					return nil, ErrRepository
				}
				if reversalOf, ok := purchaseCoverageNullableText(row, "reversal_of_journal_id"); ok {
					value.reversalOf = sql.NullString{String: reversalOf, Valid: true}
				}
				if reversedBy, ok := purchaseCoverageNullableText(row, "reversed_by_journal_id"); ok {
					value.reversedBy = sql.NullString{String: reversedBy, Valid: true}
				}
				result[value.id] = value
				for _, linked := range []sql.NullString{value.reversalOf, value.reversedBy} {
					if linked.Valid {
						if linked.String == "" {
							return nil, ErrRepository
						}
						if _, known := result[linked.String]; !known {
							next[linked.String] = struct{}{}
						}
					}
				}
			}
		}
		queue = purchaseCoverageMapKeys(next)
	}
	for id, value := range result {
		if (value.sourceType == "REVERSAL") != value.reversalOf.Valid || (value.state == "REVERSED") != value.reversedBy.Valid {
			return nil, ErrRepository
		}
		if value.reversalOf.Valid {
			parent, ok := result[value.reversalOf.String]
			if !ok || !parent.reversedBy.Valid || parent.reversedBy.String != id || parent.state != "REVERSED" ||
				value.sourceType != "REVERSAL" || value.sourceID != parent.id || parent.version < 2 || value.sourceRevision != parent.version-1 {
				return nil, ErrRepository
			}
		}
		if value.reversedBy.Valid {
			child, ok := result[value.reversedBy.String]
			if !ok || !child.reversalOf.Valid || child.reversalOf.String != id || child.sourceType != "REVERSAL" ||
				child.sourceID != value.id || value.version < 2 || child.sourceRevision != value.version-1 ||
				(child.state != "POSTED" && child.state != "REVERSED") {
				return nil, ErrRepository
			}
		}
	}
	// Reciprocal links alone can describe a closed cycle. A retained reversal
	// must ultimately reach an original journal, including when a reversal has
	// itself been reversed. Walk each predecessor once without recursion.
	visited := make(map[string]uint8, len(result))
	for id := range result {
		if visited[id] == 2 {
			continue
		}
		path := make([]string, 0)
		for next := id; next != "" && visited[next] != 2; {
			if err := ctx.Err(); err != nil {
				return nil, errors.Join(ErrRepository, err)
			}
			if visited[next] == 1 {
				return nil, ErrRepository
			}
			visited[next] = 1
			path = append(path, next)
			header := result[next]
			next = ""
			if header.reversalOf.Valid {
				next = header.reversalOf.String
			}
		}
		for _, member := range path {
			visited[member] = 2
		}
	}
	return result, nil
}

func purchaseCoverageJournalAccountIDs(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID string, journalIDs []string, discoveryRemaining *int64, limit int64) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for _, batch := range purchaseCoverageIDBatches(journalIDs, 400) {
		query := postingReportingQuery{
			table: "coverage_journal_account_ids", alias: "a", columns: []string{"account_id"},
			from: `(SELECT DISTINCT l.account_id AS account_id FROM journal_lines l JOIN journals j ON j.id=l.journal_id
			WHERE j.organisation_id=? AND j.id IN (` + purchaseCoveragePlaceholders(len(batch)) + `)) AS a`,
			where: "1=1", args: append([]any{organisationID}, batchArgs(batch)...), orderBy: "a.account_id",
		}
		rows, _, err := purchaseCoverageBoundedRows(ctx, executor, query, discoveryRemaining, limit)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			id := postingReportingString(row, "account_id")
			if id == "" {
				return nil, ErrRepository
			}
			result[id] = struct{}{}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return result, nil
}

func purchaseCoverageAccountQueries(organisationID string, accountIDs []string, includeControls bool) []postingReportingQuery {
	queries := make([]postingReportingQuery, 0, (len(accountIDs)+399)/400+1)
	columns := strings.Fields("id organisation_id code name account_type subtype normal_balance status designation default_tax_code_id report_classification cash_flow_classification owner_module version created_at updated_at")
	for index, batch := range purchaseCoverageIDBatches(accountIDs, 400) {
		where := "a.organisation_id=? AND a.id IN (" + purchaseCoveragePlaceholders(len(batch)) + ")"
		args := []any{organisationID}
		args = append(args, batchArgs(batch)...)
		if includeControls && index == 0 {
			where = "a.organisation_id=? AND (a.id IN (" + purchaseCoveragePlaceholders(len(batch)) + ") OR a.code IN ('2000','1150','1160','1170'))"
		} else if includeControls {
			where += " AND a.code NOT IN ('2000','1150','1160','1170')"
		}
		queries = append(queries, postingReportingQuery{table: "accounts", alias: "a", columns: columns,
			from: "accounts AS a", where: where, args: args, orderBy: "a.id"})
	}
	if includeControls && len(accountIDs) == 0 {
		queries = append(queries, postingReportingQuery{table: "accounts", alias: "a", columns: columns,
			from: "accounts AS a", where: "a.organisation_id=? AND a.code IN ('2000','1150','1160','1170')", args: []any{organisationID}, orderBy: "a.id"})
	}
	return queries
}

func purchaseCoverageValidateAccountRows(rows []PurchaseReportingRetainedRow, required map[string]struct{}) error {
	found := make(map[string]struct{})
	for _, row := range rows {
		if row.Table == "accounts" {
			id := postingReportingString(row, "id")
			if id == "" {
				return ErrRepository
			}
			found[id] = struct{}{}
		}
	}
	for id := range required {
		if _, ok := found[id]; !ok {
			return ErrRepository
		}
	}
	return nil
}

func purchaseCoverageValidateJournalClosure(headers map[string]purchaseCoverageJournalHeader, records []*accounting.JournalReportingRecord) error {
	if len(headers) != len(records) {
		return ErrRepository
	}
	for _, proof := range records {
		if proof == nil || proof.Journal == nil {
			return ErrRepository
		}
		id := proof.Journal.Id
		header, ok := headers[id]
		if !ok || proof.Journal.OrganisationId != header.organisationID || proof.Journal.Version != uint64(header.version) ||
			proof.SourceType != header.sourceType || proof.SourceID != header.sourceID || proof.SourceRevision != uint64(header.sourceRevision) ||
			proof.State != header.state || proof.SourceKind < 0 {
			return ErrRepository
		}
		if header.reversalOf.Valid {
			if proof.Journal.ReversalOfJournalId == nil || *proof.Journal.ReversalOfJournalId != header.reversalOf.String {
				return ErrRepository
			}
		} else if proof.Journal.ReversalOfJournalId != nil {
			return ErrRepository
		}
		if header.reversedBy.Valid {
			if proof.Journal.ReversedByJournalId == nil || *proof.Journal.ReversedByJournalId != header.reversedBy.String {
				return ErrRepository
			}
		} else if proof.Journal.ReversedByJournalId != nil {
			return ErrRepository
		}
	}
	return nil
}

func purchaseCoverageValidateCashCandidates(candidateIDs map[string]struct{}, records []*accounting.JournalReportingRecord, rows []PurchaseReportingRetainedRow) error {
	accounts := make(map[string]string)
	for _, row := range rows {
		if row.Table == "accounts" {
			accounts[postingReportingString(row, "id")] = postingReportingString(row, "report_classification")
		}
	}
	proofs := make(map[string]*accounting.JournalReportingRecord, len(records))
	for _, record := range records {
		if record != nil && record.Journal != nil {
			proofs[record.Journal.Id] = record
		}
	}
	for id := range candidateIDs {
		proof := proofs[id]
		if proof == nil || proof.SourceKind != 0 || proof.SourceType == "OPENING" || proof.SourceType == "REVERSAL" ||
			proof.State != "POSTED" || proof.Journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED {
			return ErrRepository
		}
		cash := false
		for _, line := range proof.Journal.Lines {
			classification := accounts[line.AccountId]
			if (classification == "balance_sheet.cash" || classification == "balance_sheet.cash_equivalent") &&
				line.GetCredit().GetMinorUnits() > line.GetDebit().GetMinorUnits() {
				cash = true
				break
			}
		}
		if !cash {
			return ErrRepository
		}
	}
	return nil
}

func purchaseCoverageHydrateDocuments(ctx context.Context, reader *Repository, organisationID string, documentIDs []string) ([]PostingDocumentReportingRecord, map[string]*tammyv1.Document, error) {
	records := make([]PostingDocumentReportingRecord, 0, len(documentIDs))
	byID := make(map[string]*tammyv1.Document, len(documentIDs))
	for _, id := range documentIDs {
		if err := ctx.Err(); err != nil {
			return nil, nil, errors.Join(ErrRepository, err)
		}
		document, err := reader.document(ctx, organisationID, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, nil, ErrRepository
			}
			return nil, nil, purchaseReportingReadError(ctx, err)
		}
		byID[id] = document
		records = append(records, PostingDocumentReportingRecord{Document: document})
	}
	return records, byID, nil
}

func purchaseCoverageHydrateOpenings(ctx context.Context, reader *Repository, organisationID string, rootIDs []string) (map[string]*OpeningPayable, error) {
	repository, err := NewOpeningRepository(reader.executor)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	result := make(map[string]*OpeningPayable, len(rootIDs))
	for _, id := range rootIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		opening, err := repository.GetOpeningPayable(ctx, organisationID, id)
		if err != nil || opening == nil || opening.ID != id || opening.OrganisationID != organisationID {
			return nil, errors.Join(ErrRepository, err, ctx.Err())
		}
		result[id] = opening
	}
	return result, nil
}

func purchaseCoverageVerifyConversions(ctx context.Context, repository *accounting.OpeningConversionRepository, rows []PurchaseReportingRetainedRow, conversionIDs []string) error {
	headers := make(map[string]purchaseCoverageConversionHeader)
	for _, row := range rows {
		if row.Table != "opening_conversions" {
			continue
		}
		id := postingReportingString(row, "id")
		version, ok := purchaseCoverageInteger(row, "version")
		if !ok || version <= 0 {
			return ErrRepository
		}
		financial, financialPresent := purchaseCoverageNullableInteger(row, "financial_revision")
		journal, journalPresent := purchaseCoverageNullableText(row, "journal_id")
		replacedBy, replacedPresent := purchaseCoverageNullableText(row, "replaced_by_id")
		value := purchaseCoverageConversionHeader{
			id: id, organisationID: postingReportingString(row, "organisation_id"), conversionDate: postingReportingString(row, "conversion_date"),
			state: postingReportingString(row, "state"), sourceHash: postingReportingString(row, "source_sha256"), createdAt: postingReportingString(row, "created_at"), version: version,
		}
		if journalPresent {
			value.journalID = sql.NullString{String: journal, Valid: true}
		}
		if replacedPresent {
			value.replacedBy = sql.NullString{String: replacedBy, Valid: true}
		}
		if financialPresent {
			value.financialRevision = sql.NullInt64{Int64: financial, Valid: true}
		}
		if _, duplicate := headers[id]; duplicate || id == "" || !ids.IsCanonicalV7(id) {
			return ErrRepository
		}
		headers[id] = value
	}
	if len(headers) != len(conversionIDs) {
		return ErrRepository
	}
	for _, id := range conversionIDs {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		header, ok := headers[id]
		if !ok || header.organisationID == "" || len(header.sourceHash) != 64 || strings.ToLower(header.sourceHash) != header.sourceHash {
			return ErrRepository
		}
		decoded, err := hex.DecodeString(header.sourceHash)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != header.sourceHash {
			return ErrRepository
		}
		date, err := time.Parse("2006-01-02", header.conversionDate)
		if err != nil || date.Format("2006-01-02") != header.conversionDate || date.Year() < 1900 || date.Year() > 9999 {
			return ErrRepository
		}
		if header.state == "DRAFT" {
			// A replacement target may be a retained draft conversion with no
			// module payable yet. Keep its exact raw header/items in the capture,
			// but do not ask the mature posted/replaced reader to interpret it.
			if header.version != 1 || header.journalID.Valid || header.financialRevision.Valid || header.replacedBy.Valid {
				return ErrRepository
			}
			continue
		}
		projection, err := repository.Get(ctx, id)
		if err != nil || projection == nil || projection.Id != id || projection.OrganisationId != header.organisationID || projection.Version != uint64(header.version) ||
			projection.ConversionDate == nil || projection.ConversionDate.Year != int32(date.Year()) || projection.ConversionDate.Month != int32(date.Month()) || projection.ConversionDate.Day != int32(date.Day()) {
			return errors.Join(ErrRepository, err, ctx.Err())
		}
		if header.state == "POSTED" {
			if projection.State != tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_POSTED || !header.journalID.Valid || !header.financialRevision.Valid || header.replacedBy.Valid {
				return ErrRepository
			}
		} else if header.state == "REPLACED" {
			if projection.State != tammyv1.OpeningConversionState_OPENING_CONVERSION_STATE_REPLACED || !header.journalID.Valid || !header.financialRevision.Valid || !header.replacedBy.Valid {
				return ErrRepository
			}
		} else {
			return ErrRepository
		}
		if header.journalID.Valid != (projection.JournalId != "") || (header.journalID.Valid && header.journalID.String != projection.JournalId) ||
			header.financialRevision.Valid != (projection.FinancialRevision != 0) || (header.financialRevision.Valid && uint64(header.financialRevision.Int64) != projection.FinancialRevision) ||
			header.replacedBy.Valid != (projection.ReplacedById != nil) || (header.replacedBy.Valid && *projection.ReplacedById != header.replacedBy.String) {
			return ErrRepository
		}
		if header.replacedBy.Valid {
			target, ok := headers[header.replacedBy.String]
			if !ok || target.organisationID != header.organisationID || header.state != "REPLACED" {
				return ErrRepository
			}
		}
	}
	return nil
}

func purchaseCoverageBuildLegacySources(ctx context.Context, reader *Repository, organisationID string, documentIDs, openingRootIDs []string,
	openingByID map[string]*OpeningPayable, documentByID map[string]*tammyv1.Document, edges []purchaseCoverageEdge,
	receipts map[string]struct{}, rows []PurchaseReportingRetainedRow, journals []*accounting.JournalReportingRecord, allDocumentIDs []string,
) ([]*LegacyPurchaseReportingRecord, error) {
	postingDocumentIDs := make(map[string]struct{})
	for _, row := range rows {
		if row.Table == "purchase_postings_v1" {
			postingDocumentIDs[postingReportingString(row, "document_id")] = struct{}{}
		}
	}
	reviews := make(map[string][]*tammyv1.LegacyPurchaseReview)
	reviewRows := make(map[string][]PurchaseReportingRetainedRow)
	for _, row := range rows {
		if row.Table != "purchase_legacy_reviews_v1" {
			continue
		}
		kind := postingReportingString(row, "source_kind")
		id := postingReportingString(row, "document_id")
		if kind == "OPENING_PAYABLE" {
			id = postingReportingString(row, "opening_payable_id")
		}
		reviewRows[kind+":"+id] = append(reviewRows[kind+":"+id], row)
	}
	for _, docID := range documentIDs {
		key := "DOCUMENT:" + docID
		if len(reviewRows[key]) == 0 {
			continue
		}
		first := &tammyv1.LegacyPurchaseReview{}
		if content, ok := postingReportingBytes(reviewRows[key][0], "review_proto"); !ok || proto.Unmarshal(content, first) != nil || first.Review == nil || first.Review.Source == nil {
			return nil, ErrRepository
		}
		kind, sourceID, _, _, _, scopeErr := legacySourceScope(first.Review.Source)
		if scopeErr != nil || kind != "DOCUMENT" || sourceID != docID {
			return nil, ErrRepository
		}
		read, err := reader.ReadLegacy(ctx, organisationID, first.Review.Source)
		if err != nil {
			return nil, err
		}
		if len(read) != len(reviewRows[key]) {
			return nil, ErrRepository
		}
		for index, record := range read {
			encoded, ok := postingReportingBytes(reviewRows[key][index], "review_proto")
			computed, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(record)
			if !ok || marshalErr != nil || !bytes.Equal(encoded, computed) {
				return nil, ErrRepository
			}
		}
		reviews[key] = read
	}
	for _, openingID := range openingRootIDs {
		key := "OPENING_PAYABLE:" + openingID
		if len(reviewRows[key]) == 0 {
			continue
		}
		opening := openingByID[openingID]
		if opening == nil {
			return nil, ErrRepository
		}
		read, err := reader.ReadLegacy(ctx, organisationID, opening.LegacySource())
		if err != nil {
			return nil, err
		}
		if len(read) != len(reviewRows[key]) {
			return nil, ErrRepository
		}
		for index, record := range read {
			encoded, ok := postingReportingBytes(reviewRows[key][index], "review_proto")
			computed, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(record)
			if !ok || marshalErr != nil || !bytes.Equal(encoded, computed) {
				return nil, ErrRepository
			}
		}
		reviews[key] = read
	}

	priorBySource := make(map[string]map[string]struct{})
	for _, edge := range edges {
		if priorBySource[edge.sourceID] == nil {
			priorBySource[edge.sourceID] = make(map[string]struct{})
		}
		priorBySource[edge.sourceID][edge.journalID] = struct{}{}
	}
	paymentByDocument := make(map[string]map[string]struct{})
	for _, row := range rows {
		if row.Table != "purchase_payments_v1" {
			continue
		}
		postingID := postingReportingString(row, "posting_id")
		documentID := ""
		for _, postingRow := range rows {
			if postingRow.Table == "purchase_postings_v1" && postingReportingString(postingRow, "id") == postingID {
				documentID = postingReportingString(postingRow, "document_id")
				break
			}
		}
		journalID := postingReportingString(row, "journal_id")
		if documentID == "" || journalID == "" {
			return nil, ErrRepository
		}
		if paymentByDocument[documentID] == nil {
			paymentByDocument[documentID] = make(map[string]struct{})
		}
		paymentByDocument[documentID][journalID] = struct{}{}
	}
	journalByID := make(map[string]*accounting.JournalReportingRecord, len(journals))
	for _, journal := range journals {
		if journal != nil && journal.Journal != nil {
			journalByID[journal.Journal.Id] = journal
		}
	}
	result := make([]*LegacyPurchaseReportingRecord, 0, len(documentIDs)+len(openingRootIDs))
	for _, docID := range allDocumentIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		key := "DOCUMENT:" + docID
		prior := purchaseCoverageMapKeys(priorBySource[docID])
		paymentSet := make(map[string]struct{})
		for id := range priorBySource[docID] {
			if _, receipt := receipts[id]; receipt {
				paymentSet[id] = struct{}{}
			}
		}
		for id := range paymentByDocument[docID] {
			paymentSet[id] = struct{}{}
		}
		paymentIDs := purchaseCoverageMapKeys(paymentSet)
		_, posted := postingDocumentIDs[docID]
		currentRoot := !posted
		matchingOpeningIDs := purchaseCoverageMatchingOpeningIDs(rows, docID)
		if currentRoot || len(reviews[key]) > 0 || len(prior) > 0 || len(paymentIDs) > 0 || len(matchingOpeningIDs) > 0 {
			var source *tammyv1.LegacyPurchaseSourceRef
			if document := documentByID[docID]; document != nil && document.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED {
				currentSource, err := purchaseCoverageCurrentDocumentSource(document)
				if err != nil {
					return nil, err
				}
				source = currentSource
			}
			result = append(result, &LegacyPurchaseReportingRecord{
				Source: source, Document: documentByID[docID], CurrentRoot: currentRoot,
				Reviews: reviews[key], PriorJournalIDs: prior, PaymentJournalIDs: paymentIDs,
				MatchingOpeningPayableIDs: matchingOpeningIDs,
			})
		}
	}
	for _, openingID := range openingRootIDs {
		opening := openingByID[openingID]
		if opening == nil {
			return nil, ErrRepository
		}
		key := "OPENING_PAYABLE:" + openingID
		prior := purchaseCoverageMapKeys(priorBySource[openingID])
		paymentSet := make(map[string]struct{})
		for id := range priorBySource[openingID] {
			if _, receipt := receipts[id]; receipt {
				paymentSet[id] = struct{}{}
			}
		}
		for _, review := range reviews[key] {
			if err := verifyCoverageSupportingPayments(review, priorBySource[openingID], journalByID, rows); err != nil {
				return nil, err
			}
		}
		result = append(result, &LegacyPurchaseReportingRecord{
			Source: opening.LegacySource(), Opening: opening, CurrentRoot: true, Reviews: reviews[key],
			PriorJournalIDs: prior, PaymentJournalIDs: purchaseCoverageMapKeys(paymentSet),
			MatchingOpeningPayableIDs: nil,
		})
	}
	for _, docID := range allDocumentIDs {
		for _, review := range reviews["DOCUMENT:"+docID] {
			if err := verifyCoverageSupportingPayments(review, priorBySource[docID], journalByID, rows); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := purchaseCoverageSourceSortKey(result[i]), purchaseCoverageSourceSortKey(result[j])
		return left < right
	})
	_ = documentByID
	return result, nil
}

func purchaseCoverageCurrentDocumentSource(document *tammyv1.Document) (*tammyv1.LegacyPurchaseSourceRef, error) {
	if document == nil || document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		!ids.IsCanonicalV7(document.Id) || document.Version == 0 || len(document.Sha256) != sha256.Size || document.Candidate == nil {
		return nil, ErrRepository
	}
	content, err := purchaseBytes(document.Candidate)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	return &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_Document{
		Document: &tammyv1.LegacyPurchaseDocumentRef{
			DocumentId: document.Id, ReviewedVersion: document.Version,
			OriginalContentHash: bytes.Clone(document.Sha256), ReviewedSnapshotHash: bytes.Clone(digest[:]),
		},
	}}, nil
}

func verifyCoverageSupportingPayments(record *tammyv1.LegacyPurchaseReview, priorIDs map[string]struct{},
	journalByID map[string]*accounting.JournalReportingRecord, rows []PurchaseReportingRetainedRow,
) error {
	if record == nil || record.Review == nil {
		return ErrRepository
	}
	for _, ref := range record.Review.SupportingPayments {
		if !validLegacySupport(ref) {
			return ErrRepository
		}
		var modern *tammyv1.PurchasePayment
		for _, row := range rows {
			if row.Table != "purchase_payments_v1" || postingReportingString(row, "id") != ref.SourceId {
				continue
			}
			content, ok := postingReportingBytes(row, "payment_proto")
			if !ok {
				return ErrRepository
			}
			value := &tammyv1.PurchasePayment{}
			if proto.Unmarshal(content, value) != nil || value.Id != ref.SourceId {
				return ErrRepository
			}
			modern = value
			break
		}
		if modern != nil {
			if record.Review.LinkedPostingId == nil || modern.PostingId != *record.Review.LinkedPostingId ||
				modern.AccountingVersion != ref.SourceVersion || !bytes.Equal(modern.SourceHash, ref.SourceHash) {
				return ErrRepository
			}
			continue
		}
		// The complete closure has already been verified. A retained payment
		// may be a reversal descendant of this source's direct edge, but an
		// unrelated journal captured for another source cannot support it.
		linked := false
		for id, steps := ref.SourceId, 0; steps < len(journalByID); steps++ {
			ancestor := journalByID[id]
			if ancestor == nil || ancestor.Journal == nil || ancestor.Journal.Id != id ||
				ancestor.Journal.OrganisationId != record.OrganisationId || ancestor.SourceKind != 0 {
				return ErrRepository
			}
			if _, direct := priorIDs[id]; direct {
				linked = true
				break
			}
			parentID := ancestor.Journal.GetReversalOfJournalId()
			parent := journalByID[parentID]
			if ancestor.SourceType != "REVERSAL" || parentID == "" || ancestor.SourceID != parentID ||
				parent == nil || parent.Journal == nil || parent.Journal.GetReversedByJournalId() != id ||
				parent.State != "REVERSED" || parent.Journal.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED ||
				parent.Journal.Version < 2 || ancestor.SourceRevision != parent.Journal.Version-1 {
				return ErrRepository
			}
			id = parentID
		}
		if !linked {
			return ErrRepository
		}
		proof := journalByID[ref.SourceId]
		if proof == nil || proof.Journal == nil || proof.Journal.OrganisationId != record.OrganisationId || proof.SourceKind != 0 || proof.SourceType == "OPENING" {
			return ErrRepository
		}
		candidate := proof.Journal
		if proof.State == "REVERSED" && candidate.State == tammyv1.JournalState_JOURNAL_STATE_REVERSED &&
			candidate.Version == ref.SourceVersion+1 && candidate.ReversedByJournalId != nil {
			candidate = proto.Clone(candidate).(*tammyv1.Journal)
			candidate.State = tammyv1.JournalState_JOURNAL_STATE_POSTED
			candidate.Version = ref.SourceVersion
			candidate.ReversedByJournalId = nil
		} else if proof.State != "POSTED" || candidate.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || candidate.Version != ref.SourceVersion {
			return ErrRepository
		}
		if candidate.Id != ref.SourceId || candidate.OrganisationId != record.OrganisationId {
			return ErrRepository
		}
		encoded, err := purchaseBytes(candidate)
		if err != nil {
			return ErrRepository
		}
		digest := sha256.Sum256(encoded)
		if !bytes.Equal(digest[:], ref.SourceHash) {
			return ErrRepository
		}
	}
	return nil
}

func purchaseCoverageMatchingOpeningIDs(rows []PurchaseReportingRetainedRow, documentID string) []string {
	eligibleConversions := make(map[string]struct{})
	for _, row := range rows {
		if row.Table != "opening_conversions" {
			continue
		}
		state := postingReportingString(row, "state")
		if state == "POSTED" || state == "REPLACED" {
			eligibleConversions[postingReportingString(row, "id")] = struct{}{}
		}
	}
	type openingItemIdentity struct {
		conversionID string
		itemID       string
	}
	linkedItems := make(map[openingItemIdentity]struct{})
	for _, row := range rows {
		if row.Table != "opening_items" {
			continue
		}
		sourceType, typePresent := purchaseCoverageNullableText(row, "source_type")
		sourceID, idPresent := purchaseCoverageNullableText(row, "source_id")
		if typePresent && sourceType == "document" && idPresent && sourceID == documentID {
			if conversionID := postingReportingString(row, "conversion_id"); conversionID != "" {
				if _, eligible := eligibleConversions[conversionID]; eligible {
					itemID := postingReportingString(row, "id")
					if itemID != "" {
						linkedItems[openingItemIdentity{conversionID: conversionID, itemID: itemID}] = struct{}{}
					}
				}
			}
		}
	}
	result := make(map[string]struct{})
	for _, row := range rows {
		if row.Table != "purchase_opening_payables" {
			continue
		}
		conversionID := postingReportingString(row, "conversion_id")
		payableID := postingReportingString(row, "id")
		if conversionID != "" && payableID != "" {
			if _, linked := linkedItems[openingItemIdentity{conversionID: conversionID, itemID: payableID}]; linked {
				result[payableID] = struct{}{}
			}
		}
	}
	return purchaseCoverageMapKeys(result)
}

func purchaseCoverageSourceSortKey(record *LegacyPurchaseReportingRecord) string {
	if record == nil {
		return ""
	}
	if record.Document != nil {
		return "DOCUMENT:" + record.Document.Id
	}
	if record.Opening != nil {
		return "OPENING_PAYABLE:" + record.Opening.ID
	}
	return ""
}

func purchaseCoverageFingerprint(ctx context.Context, organisationID string, rows []PurchaseReportingRetainedRow, journals []*accounting.JournalReportingRecord) ([]byte, error) {
	for _, journal := range journals {
		if journal == nil || journal.Journal == nil || len(journal.ContentHash) != sha256.Size {
			return nil, ErrRepository
		}
	}
	digest := sha256.New()
	if err := postingReportingHashFrame(ctx, digest, []byte(purchaseCoverageFingerprintTag)); err != nil {
		return nil, err
	}
	if err := postingReportingHashFrame(ctx, digest, []byte(organisationID)); err != nil {
		return nil, err
	}
	if err := postingReportingHashInt64(ctx, digest, int64(len(rows))); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := postingReportingHashFrame(ctx, digest, []byte(row.Table)); err != nil {
			return nil, err
		}
		if len(row.Columns) != len(row.Values) {
			return nil, ErrRepository
		}
		if err := postingReportingHashInt64(ctx, digest, int64(len(row.Columns))); err != nil {
			return nil, err
		}
		for index, column := range row.Columns {
			if err := postingReportingHashFrame(ctx, digest, []byte(column)); err != nil {
				return nil, err
			}
			value := row.Values[index]
			if err := postingReportingHashFrame(ctx, digest, []byte(value.Kind)); err != nil {
				return nil, err
			}
			switch value.Kind {
			case "NULL":
			case "INTEGER":
				if err := postingReportingHashInt64(ctx, digest, value.Integer); err != nil {
					return nil, err
				}
			case "TEXT":
				if err := postingReportingHashFrame(ctx, digest, []byte(value.Text)); err != nil {
					return nil, err
				}
			case "BLOB":
				if err := postingReportingHashFrame(ctx, digest, value.Bytes); err != nil {
					return nil, err
				}
			default:
				return nil, ErrRepository
			}
		}
	}
	sort.Slice(journals, func(i, j int) bool { return journals[i].Journal.Id < journals[j].Journal.Id })
	if err := postingReportingHashInt64(ctx, digest, int64(len(journals))); err != nil {
		return nil, err
	}
	for _, journal := range journals {
		if err := postingReportingHashFrame(ctx, digest, []byte(journal.Journal.Id)); err != nil {
			return nil, err
		}
		if err := postingReportingHashFrame(ctx, digest, journal.ContentHash); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
}

func purchaseCoverageNullableText(row PurchaseReportingRetainedRow, column string) (string, bool) {
	for index, name := range row.Columns {
		if name != column || index >= len(row.Values) {
			continue
		}
		value := row.Values[index]
		if value.Kind == "NULL" {
			return "", false
		}
		if value.Kind == "TEXT" {
			return value.Text, true
		}
	}
	return "", false
}

func purchaseCoverageNullableInteger(row PurchaseReportingRetainedRow, column string) (int64, bool) {
	for index, name := range row.Columns {
		if name != column || index >= len(row.Values) {
			continue
		}
		value := row.Values[index]
		if value.Kind == "NULL" {
			return 0, false
		}
		if value.Kind == "INTEGER" {
			return value.Integer, true
		}
	}
	return 0, false
}

func purchaseCoverageInteger(row PurchaseReportingRetainedRow, column string) (int64, bool) {
	return purchaseCoverageNullableInteger(row, column)
}

func purchaseCoverageRowsForTable(rows []PurchaseReportingRetainedRow, table string) int64 {
	var count int64
	for _, row := range rows {
		if row.Table == table {
			count++
		}
	}
	return count
}

func purchaseCoverageCountText(rows []PurchaseReportingRetainedRow, table, column, value string) int {
	count := 0
	for _, row := range rows {
		if row.Table == table && postingReportingString(row, column) == value {
			count++
		}
	}
	return count
}

func purchaseCoverageMapKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func purchaseCoverageUniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return purchaseCoverageMapKeys(set)
}

func purchaseCoverageMapKeysFromHeaders(values map[string]purchaseCoverageJournalHeader) []string {
	set := make(map[string]struct{}, len(values))
	for id := range values {
		set[id] = struct{}{}
	}
	return purchaseCoverageMapKeys(set)
}

func purchaseCoverageIDBatches(values []string, maximum int) [][]string {
	if maximum <= 0 || len(values) == 0 {
		return nil
	}
	values = purchaseCoverageUniqueSorted(values)
	result := make([][]string, 0, (len(values)+maximum-1)/maximum)
	for start := 0; start < len(values); start += maximum {
		end := start + maximum
		if end > len(values) {
			end = len(values)
		}
		result = append(result, append([]string(nil), values[start:end]...))
	}
	return result
}

func purchaseCoveragePlaceholders(count int) string {
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func batchArgs(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func purchaseReportingReadError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx == nil || ctx.Err() == nil {
		return err
	}
	return errors.Join(err, ctx.Err())
}
