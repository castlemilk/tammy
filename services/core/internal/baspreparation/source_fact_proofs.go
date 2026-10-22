package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

const basSharedTaxFactRowOverhead int64 = 512

var basSharedTaxFactColumns = [...]string{
	"id", "organisation_id", "journal_line_id", "tax_code", "treatment",
	"original_gross_minor", "original_net_minor", "original_gst_minor",
	"attributed_gross_minor", "attributed_net_minor", "attributed_gst_minor",
	"remaining_gross_minor", "remaining_net_minor", "remaining_gst_minor",
	"tax_rule_type", "tax_rule_id", "tax_rule_revision", "tax_rule_content_hash",
	"source_type", "source_id", "source_revision", "source_content_hash", "created_at",
}

const (
	basSharedFactID = iota
	basSharedFactOrganisationID
	basSharedFactJournalLineID
	basSharedFactTaxCode
	basSharedFactTreatment
	basSharedFactOriginalGross
	basSharedFactOriginalNet
	basSharedFactOriginalGST
	basSharedFactAttributedGross
	basSharedFactAttributedNet
	basSharedFactAttributedGST
	basSharedFactRemainingGross
	basSharedFactRemainingNet
	basSharedFactRemainingGST
	basSharedFactRuleType
	basSharedFactRuleID
	basSharedFactRuleRevision
	basSharedFactRuleHash
	basSharedFactSourceType
	basSharedFactSourceID
	basSharedFactSourceRevision
	basSharedFactSourceHash
	basSharedFactCreatedAt
)

type BASSharedFactProof struct {
	Count                 int
	PurchaseBytes         int64
	AccountingSubsetBytes int64
}

// verifyBASSharedTaxFacts independently reconciles the raw purchase projection
// with the accounting-owned canonical fact records for purchase-selected
// journal parents. It does no SQL, filtering by finance meaning, or effect
// calculation. Accounting facts outside purchase.JournalIDs remain part of
// the independently verified accounting base charge but are not compared with
// the purchase subset.
func verifyBASSharedTaxFacts(
	ctx context.Context,
	organisationID string,
	purchase *purchases.PurchaseCoverageReportingCapture,
	accountingCapture *accounting.TaxFactReportingCapture,
) (*BASSharedFactProof, error) {
	if ctx == nil || !ids.IsCanonicalV7(organisationID) || purchase == nil || accountingCapture == nil ||
		purchase.OrganisationID != organisationID || accountingCapture.FactCount != len(accountingCapture.Facts) {
		return nil, ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrSourceReservation, err)
	}

	purchaseJournalIDs, ok := basSharedTaxFactJournalIDSet(purchase.JournalIDs, purchase.JournalCount)
	if !ok || !basSharedTaxFactJournalPayloadMapMatches(purchase.JournalIDs, purchase.JournalPayloadBytes) {
		return nil, ErrSourceReservation
	}
	if len(purchase.Journals) != len(purchaseJournalIDs) {
		return nil, ErrSourceReservation
	}
	seenPurchaseProofs := make(map[string]struct{}, len(purchase.Journals))
	for _, proof := range purchase.Journals {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrSourceReservation, err)
		}
		if proof == nil || proof.Journal == nil || proof.Journal.Id == "" ||
			proof.Journal.OrganisationId != organisationID || len(proof.ContentHash) != 32 ||
			proof.PayloadBytes <= 0 {
			return nil, ErrSourceReservation
		}
		if _, selected := purchaseJournalIDs[proof.Journal.Id]; !selected {
			return nil, ErrSourceReservation
		}
		if proof.PayloadBytes != purchase.JournalPayloadBytes[proof.Journal.Id] {
			return nil, ErrSourceReservation
		}
		if _, duplicate := seenPurchaseProofs[proof.Journal.Id]; duplicate {
			return nil, ErrSourceReservation
		}
		seenPurchaseProofs[proof.Journal.Id] = struct{}{}
	}
	if len(seenPurchaseProofs) != len(purchaseJournalIDs) {
		return nil, ErrSourceReservation
	}

	accountingJournalIDs, ok := basSharedTaxFactJournalIDSet(accountingCapture.JournalIDs, accountingCapture.JournalCount)
	if !ok || !basSharedTaxFactJournalPayloadMapMatches(accountingCapture.JournalIDs, accountingCapture.JournalPayloadBytes) {
		return nil, ErrSourceReservation
	}
	selectedFacts := make(map[string]accounting.TaxFactReportingRecord)
	seenAccountingFacts := make(map[string]struct{}, len(accountingCapture.Facts))
	seenAccountingJournals := make(map[string]struct{}, len(accountingCapture.JournalIDs))
	var accountingBaseBytes, accountingSubsetBytes int64
	for _, fact := range accountingCapture.Facts {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrSourceReservation, err)
		}
		if fact.ID == "" || fact.OrganisationID != organisationID || fact.Journal == nil ||
			fact.Journal.Id == "" || fact.Journal.OrganisationId != organisationID {
			return nil, ErrSourceReservation
		}
		if _, duplicate := seenAccountingFacts[fact.ID]; duplicate {
			return nil, ErrSourceReservation
		}
		seenAccountingFacts[fact.ID] = struct{}{}
		seenAccountingJournals[fact.Journal.Id] = struct{}{}

		charge, valid := basSharedTaxFactAccountingCharge(fact)
		if !valid {
			return nil, ErrSourceReservation
		}
		accountingBaseBytes, valid = basSharedTaxFactAdd(accountingBaseBytes, charge)
		if !valid {
			return nil, ErrSourceReservation
		}
		if _, selected := purchaseJournalIDs[fact.Journal.Id]; selected {
			selectedFacts[fact.ID] = fact
			accountingSubsetBytes, valid = basSharedTaxFactAdd(accountingSubsetBytes, charge)
			if !valid {
				return nil, ErrSourceReservation
			}
		}
	}
	if len(seenAccountingFacts) != accountingCapture.FactCount ||
		len(seenAccountingJournals) != len(accountingJournalIDs) {
		return nil, ErrSourceReservation
	}
	for journalID := range seenAccountingJournals {
		if _, listed := accountingJournalIDs[journalID]; !listed {
			return nil, ErrSourceReservation
		}
	}
	if accountingBaseBytes != accountingCapture.BasePayloadBytes {
		return nil, ErrSourceReservation
	}

	matchedFacts := make(map[string]struct{}, len(selectedFacts))
	var purchaseBytes int64
	rawFactCount := 0
	for _, row := range purchase.Rows {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrSourceReservation, err)
		}
		if row.Table != "tax_facts" {
			continue
		}
		rawFactCount++
		if !basSharedTaxFactProjectionShape(row) {
			return nil, ErrSourceReservation
		}
		factID := row.Values[basSharedFactID].Text
		fact, expected := selectedFacts[factID]
		if !expected {
			return nil, ErrSourceReservation
		}
		if _, duplicate := matchedFacts[factID]; duplicate {
			return nil, ErrSourceReservation
		}
		if !basSharedTaxFactProjectionMatches(row, fact) {
			return nil, ErrSourceReservation
		}
		matchedFacts[factID] = struct{}{}
		charge, valid := basSharedTaxFactPurchaseCharge(row)
		if !valid {
			return nil, ErrSourceReservation
		}
		purchaseBytes, valid = basSharedTaxFactAdd(purchaseBytes, charge)
		if !valid {
			return nil, ErrSourceReservation
		}
	}
	if rawFactCount != len(selectedFacts) || len(matchedFacts) != len(selectedFacts) ||
		purchaseBytes != purchase.TaxFactPayloadBytes {
		return nil, ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrSourceReservation, err)
	}
	return &BASSharedFactProof{
		Count: len(selectedFacts), PurchaseBytes: purchaseBytes, AccountingSubsetBytes: accountingSubsetBytes,
	}, nil
}

func basSharedTaxFactJournalIDSet(journalIDs []string, count int) (map[string]struct{}, bool) {
	if count < 0 || len(journalIDs) != count {
		return nil, false
	}
	result := make(map[string]struct{}, len(journalIDs))
	previous := ""
	for index, id := range journalIDs {
		if id == "" || index > 0 && id <= previous {
			return nil, false
		}
		result[id] = struct{}{}
		previous = id
	}
	return result, true
}

func basSharedTaxFactJournalPayloadMapMatches(journalIDs []string, payloads map[string]int64) bool {
	if len(journalIDs) != len(payloads) {
		return false
	}
	for _, journalID := range journalIDs {
		payload, exists := payloads[journalID]
		if !exists || payload <= 0 {
			return false
		}
	}
	return true
}

func basSharedTaxFactAccountingCharge(fact accounting.TaxFactReportingRecord) (int64, bool) {
	if fact.Rule == nil || fact.Rule.Revision == 0 || fact.Rule.Revision > math.MaxInt64 || len(fact.Rule.ContentHash) != 32 ||
		fact.SourceRevision == 0 || fact.SourceRevision > math.MaxInt64 || fact.SourceContentHashPresent != (fact.SourceContentHash != nil) ||
		(fact.SourceContentHashPresent && len(fact.SourceContentHash) != 32) ||
		fact.Journal == nil || fact.Journal.PostingDate == nil {
		return 0, false
	}
	date := fact.Journal.PostingDate
	journalDate := fmt.Sprintf("%04d-%02d-%02d", date.Year, date.Month, date.Day)
	values := [][]byte{
		[]byte(fact.ID), []byte(fact.OrganisationID), []byte(fact.JournalLineID),
		[]byte(fact.TaxCode), []byte(fact.Treatment),
		[]byte(fact.Rule.Type), []byte(fact.Rule.Id), fact.Rule.ContentHash,
		[]byte(fact.SourceType), []byte(fact.SourceID), fact.SourceContentHash,
		[]byte(fact.CreatedAt), []byte(fact.Journal.Id), []byte(fact.Journal.OrganisationId),
		[]byte(journalDate),
	}
	total := basSharedTaxFactRowOverhead
	for _, value := range values {
		var ok bool
		total, ok = basSharedTaxFactAdd(total, int64(len(value)))
		if !ok {
			return 0, false
		}
	}
	return total, true
}

func basSharedTaxFactProjectionShape(row purchases.PurchaseReportingRetainedRow) bool {
	if len(row.Columns) != len(basSharedTaxFactColumns) || len(row.Values) != len(basSharedTaxFactColumns) {
		return false
	}
	for index, column := range basSharedTaxFactColumns {
		if row.Columns[index] != column {
			return false
		}
	}
	return true
}

func basSharedTaxFactProjectionMatches(
	row purchases.PurchaseReportingRetainedRow,
	fact accounting.TaxFactReportingRecord,
) bool {
	if fact.Rule == nil || fact.Rule.Revision > math.MaxInt64 || fact.SourceRevision > math.MaxInt64 {
		return false
	}
	values := row.Values
	text := [...]struct {
		index int
		value string
	}{
		{basSharedFactID, fact.ID},
		{basSharedFactOrganisationID, fact.OrganisationID},
		{basSharedFactJournalLineID, fact.JournalLineID},
		{basSharedFactTaxCode, fact.TaxCode},
		{basSharedFactTreatment, fact.Treatment},
		{basSharedFactRuleType, fact.Rule.Type},
		{basSharedFactRuleID, fact.Rule.Id},
		{basSharedFactSourceType, fact.SourceType},
		{basSharedFactSourceID, fact.SourceID},
		{basSharedFactCreatedAt, fact.CreatedAt},
	}
	for _, item := range text {
		if !basSharedTaxFactTextCell(values[item.index], item.value) {
			return false
		}
	}
	numeric := [...]struct {
		index int
		value int64
	}{
		{basSharedFactOriginalGross, fact.OriginalGrossMinor},
		{basSharedFactOriginalNet, fact.OriginalNetMinor},
		{basSharedFactOriginalGST, fact.OriginalGSTMinor},
		{basSharedFactAttributedGross, fact.AttributedGrossMinor},
		{basSharedFactAttributedNet, fact.AttributedNetMinor},
		{basSharedFactAttributedGST, fact.AttributedGSTMinor},
		{basSharedFactRemainingGross, fact.RemainingGrossMinor},
		{basSharedFactRemainingNet, fact.RemainingNetMinor},
		{basSharedFactRemainingGST, fact.RemainingGSTMinor},
		{basSharedFactRuleRevision, int64(fact.Rule.Revision)},
		{basSharedFactSourceRevision, int64(fact.SourceRevision)},
	}
	for _, item := range numeric {
		if !basSharedTaxFactIntegerCell(values[item.index], item.value) {
			return false
		}
	}
	if !basSharedTaxFactBlobCell(values[basSharedFactRuleHash], fact.Rule.ContentHash) {
		return false
	}
	sourceHash := values[basSharedFactSourceHash]
	if fact.SourceContentHashPresent {
		if !basSharedTaxFactBlobCell(sourceHash, fact.SourceContentHash) {
			return false
		}
	} else if fact.SourceContentHash != nil || !basSharedTaxFactNullCell(sourceHash) {
		return false
	}
	return true
}

func basSharedTaxFactTextCell(value purchases.PurchaseReportingRetainedValue, expected string) bool {
	return value.Kind == "TEXT" && value.Text == expected && value.Integer == 0 && value.Bytes == nil
}

func basSharedTaxFactIntegerCell(value purchases.PurchaseReportingRetainedValue, expected int64) bool {
	return value.Kind == "INTEGER" && value.Integer == expected && value.Text == "" && value.Bytes == nil
}

func basSharedTaxFactBlobCell(value purchases.PurchaseReportingRetainedValue, expected []byte) bool {
	return value.Kind == "BLOB" && value.Integer == 0 && value.Text == "" && value.Bytes != nil && bytes.Equal(value.Bytes, expected)
}

func basSharedTaxFactNullCell(value purchases.PurchaseReportingRetainedValue) bool {
	return value.Kind == "NULL" && value.Integer == 0 && value.Text == "" && value.Bytes == nil
}

func basSharedTaxFactPurchaseCharge(row purchases.PurchaseReportingRetainedRow) (int64, bool) {
	if !basSharedTaxFactProjectionShape(row) {
		return 0, false
	}
	total := basSharedTaxFactRowOverhead
	for _, value := range row.Values {
		var size int64
		switch value.Kind {
		case "NULL":
			if value.Integer != 0 || value.Text != "" || value.Bytes != nil {
				return 0, false
			}
		case "INTEGER":
			if value.Text != "" || value.Bytes != nil {
				return 0, false
			}
			size = int64(len(strconv.FormatInt(value.Integer, 10)))
		case "TEXT":
			if value.Integer != 0 || value.Bytes != nil {
				return 0, false
			}
			size = int64(len(value.Text))
		case "BLOB":
			if value.Integer != 0 || value.Text != "" {
				return 0, false
			}
			size = int64(len(value.Bytes))
		default:
			return 0, false
		}
		var ok bool
		total, ok = basSharedTaxFactAdd(total, size)
		if !ok {
			return 0, false
		}
	}
	return total, true
}

func basSharedTaxFactAdd(total, value int64) (int64, bool) {
	if total < 0 || value < 0 || total > math.MaxInt64-value {
		return 0, false
	}
	return total + value, true
}

// verifyBASSharedJournalCaptureProofs checks that each complete source capture
// retained exactly the journal proofs named by its own reservation, then checks
// every journal shared by two or more captures for identical accounting charge
// and raw journal content hash. Different source captures may have different
// journal sets; this deliberately compares only their intersection.
func verifyBASSharedJournalCaptureProofs(
	ctx context.Context,
	organisationID string,
	invoiceCapture *invoices.BASReportingCapture,
	purchaseCapture *purchases.PurchaseCoverageReportingCapture,
	accountingCapture *accounting.TaxFactReportingCapture,
) error {
	if ctx == nil || !ids.IsCanonicalV7(organisationID) || invoiceCapture == nil ||
		purchaseCapture == nil || accountingCapture == nil || purchaseCapture.OrganisationID != organisationID {
		return ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrSourceReservation, err)
	}

	invoiceProofs, ok := basSharedInvoiceJournalProofs(invoiceCapture, organisationID)
	if !ok {
		return ErrSourceReservation
	}
	sets := []basSharedJournalProofSet{
		{
			count: invoiceCapture.JournalCount, ids: invoiceCapture.JournalIDs,
			payloadBytes: invoiceCapture.JournalPayloadBytes, proofs: invoiceProofs,
		},
		{
			count: purchaseCapture.JournalCount, ids: purchaseCapture.JournalIDs,
			payloadBytes: purchaseCapture.JournalPayloadBytes, proofs: purchaseCapture.Journals,
		},
		{
			count: accountingCapture.JournalCount, ids: accountingCapture.JournalIDs,
			payloadBytes: accountingCapture.JournalPayloadBytes, proofs: accountingCapture.Journals,
		},
	}
	if len(purchaseCapture.Journals) != purchaseCapture.JournalCount || len(accountingCapture.Journals) != accountingCapture.JournalCount {
		return ErrSourceReservation
	}
	indexes := make([]map[string]*accounting.JournalReportingRecord, len(sets))
	for index, set := range sets {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrSourceReservation, err)
		}
		proofs, valid := basSharedJournalProofIndex(ctx, organisationID, set)
		if !valid {
			return errors.Join(ErrSourceReservation, ctx.Err())
		}
		indexes[index] = proofs
	}
	for leftIndex := range indexes {
		for journalID, left := range indexes[leftIndex] {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrSourceReservation, err)
			}
			for rightIndex := leftIndex + 1; rightIndex < len(indexes); rightIndex++ {
				right := indexes[rightIndex][journalID]
				if right != nil && (left.PayloadBytes != right.PayloadBytes ||
					!bytes.Equal(left.ContentHash, right.ContentHash)) {
					return ErrSourceReservation
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrSourceReservation, err)
	}
	return nil
}

type basSharedJournalProofSet struct {
	count        int
	ids          []string
	payloadBytes map[string]int64
	proofs       []*accounting.JournalReportingRecord
}

func basSharedInvoiceJournalProofs(
	capture *invoices.BASReportingCapture,
	organisationID string,
) ([]*accounting.JournalReportingRecord, bool) {
	if capture == nil || capture.JournalCount < 0 {
		return nil, false
	}
	proofs := make([]*accounting.JournalReportingRecord, 0, capture.JournalCount)
	for _, candidate := range capture.Candidates {
		if candidate.OrganisationID != organisationID {
			return nil, false
		}
		if candidate.Posting != nil {
			posting := candidate.Posting
			if posting.JournalID == nil {
				if posting.Journal != nil {
					return nil, false
				}
			} else {
				if *posting.JournalID == "" || posting.Journal == nil || posting.Journal.Journal == nil ||
					posting.Journal.Journal.Id != *posting.JournalID {
					return nil, false
				}
				proofs = append(proofs, posting.Journal)
			}
		}
		for _, receipt := range candidate.Receipts {
			if receipt.JournalID == "" || receipt.Journal == nil || receipt.Journal.Journal == nil ||
				receipt.Journal.Journal.Id != receipt.JournalID {
				return nil, false
			}
			proofs = append(proofs, receipt.Journal)
		}
	}
	return proofs, true
}

func basSharedJournalProofIndex(
	ctx context.Context,
	organisationID string,
	set basSharedJournalProofSet,
) (map[string]*accounting.JournalReportingRecord, bool) {
	selected, ok := basSharedTaxFactJournalIDSet(set.ids, set.count)
	if !ok || !basSharedTaxFactJournalPayloadMapMatches(set.ids, set.payloadBytes) {
		return nil, false
	}
	proofs := make(map[string]*accounting.JournalReportingRecord, len(selected))
	for _, proof := range set.proofs {
		if err := ctx.Err(); err != nil {
			return nil, false
		}
		if proof == nil || proof.Journal == nil || proof.Journal.Id == "" ||
			proof.Journal.OrganisationId != organisationID || len(proof.ContentHash) != 32 || proof.PayloadBytes <= 0 {
			return nil, false
		}
		journalID := proof.Journal.Id
		if _, exists := selected[journalID]; !exists || set.payloadBytes[journalID] != proof.PayloadBytes {
			return nil, false
		}
		if previous := proofs[journalID]; previous != nil {
			if previous.PayloadBytes != proof.PayloadBytes || !bytes.Equal(previous.ContentHash, proof.ContentHash) {
				return nil, false
			}
			continue
		}
		proofs[journalID] = proof
	}
	return proofs, len(proofs) == len(selected)
}
