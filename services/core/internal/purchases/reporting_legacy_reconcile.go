package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

// NominateBASLegacyCandidates exposes the deterministic mapper to the future
// producer. It has no repository dependency and owns no input memory.
func NominateBASLegacyCandidates(
	ctx context.Context,
	organisationID string,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
	documentStates []BASDocumentState,
	purchaseKeys []BASScalarKey,
	decisions []*tammyv1.LegacyPurchaseReview,
	cash accounting.BASLegacyCashRead,
	maximumCandidates int64,
) (*BASLegacyCandidateDiscovery, error) {
	if ctx == nil {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	result, err := nominateBASLegacyCandidates(organisationID, period, balance,
		documentStates, purchaseKeys, decisions, cash, maximumCandidates)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return result, nil
}

// ReconcileBASLegacyCandidates validates a frozen pure nomination against the
// selected rows and the existing bounded full-source capture. It performs no
// database I/O, does not classify eligibility, and never changes discovery.
func ReconcileBASLegacyCandidates(
	ctx context.Context,
	organisationID string,
	period *tammyv1.PurchaseClaimPeriod,
	balance *tammyv1.CivilDate,
	documentStates []BASDocumentState,
	purchaseKeys []BASScalarKey,
	decisions []*tammyv1.LegacyPurchaseReview,
	cash accounting.BASLegacyCashRead,
	discovery *BASLegacyCandidateDiscovery,
	capture *PurchaseCoverageReportingCapture,
) error {
	fail := func(reason string) error {
		return fmt.Errorf("%w: BAS legacy full-proof reconciliation: %s", ErrRepository, reason)
	}
	if ctx == nil {
		return fail("missing context")
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRepository, err)
	}
	if discovery == nil || capture == nil || capture.OrganisationID != organisationID {
		return fail("missing capture/discovery or owner mismatch")
	}
	// A successful original nomination necessarily fit the module maximum. Use
	// that same semantic ceiling here, then compare every sorted key and flag.
	wanted, err := nominateBASLegacyCandidates(organisationID, period, balance,
		documentStates, purchaseKeys, decisions, cash, maximumBASLegacyCandidates)
	if err != nil {
		return err
	}
	if !basLegacySameDiscovery(discovery, wanted) {
		return fail("selector inputs no longer produce the frozen discovery")
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRepository, err)
	}

	states := make(map[string]BASDocumentState, len(documentStates))
	for _, state := range documentStates {
		states[state.ID] = state
	}
	keysByKindAndID := make(map[BASScalarKeyKind]map[string]BASScalarKey)
	for _, key := range purchaseKeys {
		if keysByKindAndID[key.Kind] == nil {
			keysByKindAndID[key.Kind] = make(map[string]BASScalarKey)
		}
		keysByKindAndID[key.Kind][key.ID] = key
	}
	rowsByTable, err := basLegacyRowsByTableAndID(capture.Rows)
	if err != nil {
		return fail("captured retained rows have duplicate or empty identities")
	}
	documents, err := basLegacyCapturedDocuments(capture, states, keysByKindAndID[BASScalarDocument], rowsByTable["documents"])
	if err != nil {
		return fail("captured documents differ from owner state or retained rows")
	}
	postingProofs, err := basLegacyCapturedPostings(capture, states, documents, keysByKindAndID[BASScalarPosting], rowsByTable["purchase_postings_v1"])
	if err != nil {
		return fail("captured postings differ from selected owner tuples")
	}
	paymentProofs, err := basLegacyCapturedPayments(capture, keysByKindAndID[BASScalarPayment], rowsByTable["purchase_payments_v1"])
	if err != nil {
		return fail("captured payments differ from selected owner tuples")
	}
	legacyRecords, err := basLegacyCapturedLegacySources(
		organisationID, wanted, states, purchaseKeys, decisions, capture, rowsByTable,
	)
	if err != nil {
		return fail("captured legacy roots or complete decision histories disagree")
	}
	journals, err := basLegacyCapturedCashProofs(organisationID, wanted, cash, capture, rowsByTable["accounts"], rowsByTable["tax_facts"])
	if err != nil {
		return fail("selected cash associations or closure differ from captured journal/account proofs")
	}

	// CaptureCoverageForBAS already applies this canonical manual-journal SHA
	// reconstruction (including a reversed original at its prior POSTED
	// version). Repeat the existing validator against the exact matched source
	// records so this API ties those proofs to the selector-provided history.
	for sourceKey, record := range legacyRecords {
		prior := make(map[string]struct{}, len(record.PriorJournalIDs))
		for _, id := range record.PriorJournalIDs {
			prior[id] = struct{}{}
		}
		for _, review := range record.Reviews {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err)
			}
			if err := verifyCoverageSupportingPayments(review, prior, journals, capture.Rows); err != nil {
				return fail("retained payment reference no longer matches its selected full proof for " + sourceKey)
			}
		}
	}
	if len(paymentProofs) != len(keysByKindAndID[BASScalarPayment]) || len(postingProofs) != len(keysByKindAndID[BASScalarPosting]) {
		return fail("selected purchase payment/posting key set was not fully captured")
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRepository, err)
	}
	return nil
}

func basLegacySameDiscovery(left, right *BASLegacyCandidateDiscovery) bool {
	if left == nil || right == nil || len(left.Nominations) != len(right.Nominations) || len(left.Candidates) != len(right.Candidates) {
		return false
	}
	for index := range left.Nominations {
		a, b := left.Nominations[index], right.Nominations[index]
		if a.Key != b.Key || a.SourceVersion != b.SourceVersion ||
			!bytes.Equal(a.DocumentRawSHA256, b.DocumentRawSHA256) ||
			a.ProvisionalEvidenceOnly != b.ProvisionalEvidenceOnly ||
			!sameStrings(a.ProvisionalDecisionIDs, b.ProvisionalDecisionIDs) {
			return false
		}
	}
	for index := range left.Candidates {
		if left.Candidates[index] != right.Candidates[index] {
			return false
		}
	}
	return true
}

func sameStrings(left, right []string) bool {
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

func basLegacyRowsByTableAndID(rows []PurchaseReportingRetainedRow) (map[string]map[string]PurchaseReportingRetainedRow, error) {
	result := make(map[string]map[string]PurchaseReportingRetainedRow)
	for _, row := range rows {
		if row.Table == "" {
			return nil, ErrRepository
		}
		if row.Table == "opening_items" {
			// Opening items are checked against both item ID and conversion ID
			// below; keep them out of this shared ID-only lookup.
			continue
		}
		if result[row.Table] == nil {
			result[row.Table] = make(map[string]PurchaseReportingRetainedRow)
		}
		id := postingReportingString(row, "id")
		// Event tables use operation_key, but their exact source/event proof is
		// already validated inside CaptureCoverageForBAS and ReadLegacy.
		if id == "" && row.Table != "purchase_accounting_events_v1" {
			continue
		}
		if id == "" {
			continue
		}
		if _, exists := result[row.Table][id]; exists {
			return nil, ErrRepository
		}
		result[row.Table][id] = row
	}
	return result, nil
}

func basLegacyCapturedDocuments(
	capture *PurchaseCoverageReportingCapture,
	states map[string]BASDocumentState,
	keys map[string]BASScalarKey,
	rows map[string]PurchaseReportingRetainedRow,
) (map[string]*tammyv1.Document, error) {
	if len(capture.Documents) != len(states) || len(keys) != len(states) || len(rows) != len(states) {
		return nil, ErrRepository
	}
	result := make(map[string]*tammyv1.Document, len(capture.Documents))
	for _, retained := range capture.Documents {
		doc := retained.Document
		if doc == nil || doc.OrganisationId != capture.OrganisationID || doc.Id == "" {
			return nil, ErrRepository
		}
		if _, exists := result[doc.Id]; exists {
			return nil, ErrRepository
		}
		state, stateExists := states[doc.Id]
		key, keyExists := keys[doc.Id]
		row, rowExists := rows[doc.Id]
		if !stateExists || !keyExists || !rowExists || state.OrganisationID != doc.OrganisationId ||
			state.Version != doc.Version || state.Status != doc.Status || !bytes.Equal(state.ContentHash, doc.Sha256) ||
			key.OrganisationID != doc.OrganisationId || key.ObjectVersion == nil || *key.ObjectVersion != doc.Version ||
			!bytes.Equal(key.SourceHash, doc.Sha256) || key.IsRoot != (state.PostingID == nil) ||
			!basLegacyDocumentRowMatches(row, doc) {
			return nil, ErrRepository
		}
		result[doc.Id] = doc
	}
	return result, nil
}

func basLegacyDocumentRowMatches(row PurchaseReportingRetainedRow, doc *tammyv1.Document) bool {
	if doc == nil || row.Table != "documents" || postingReportingString(row, "id") != doc.Id ||
		postingReportingString(row, "organisation_id") != doc.OrganisationId ||
		postingReportingString(row, "status") != basLegacyDocumentStatusText(doc.Status) ||
		!basLegacyRowIntEquals(row, "version", int64(doc.Version)) ||
		!basLegacyRowBlobEquals(row, "sha256", doc.Sha256, true) ||
		postingReportingString(row, "source_display_name") != doc.SourceDisplayName ||
		postingReportingString(row, "mime_type") != doc.MimeType ||
		!basLegacyRowIntEquals(row, "byte_length", int64(doc.ByteLength)) ||
		postingReportingString(row, "extracted_text") != doc.ExtractedText || doc.Candidate == nil {
		return false
	}
	original, ok := postingReportingBytes(row, "original_bytes")
	if !ok || uint64(len(original)) != doc.ByteLength {
		return false
	}
	digest := sha256.Sum256(original)
	if !bytes.Equal(digest[:], doc.Sha256) {
		return false
	}
	candidate := doc.Candidate
	if postingReportingString(row, "supplier_name") != candidate.SupplierName ||
		postingReportingString(row, "invoice_number") != candidate.InvoiceNumber ||
		!basLegacyRowDateEquals(row, "document_date", candidate.DocumentDate) ||
		!basLegacyRowMoneyEquals(row, "subtotal_minor", candidate.Subtotal) ||
		!basLegacyRowMoneyEquals(row, "gst_minor", candidate.Gst) ||
		!basLegacyRowMoneyEquals(row, "total_minor", candidate.Total) {
		return false
	}
	return true
}

func basLegacyDocumentStatusText(status tammyv1.DocumentStatus) string {
	switch status {
	case tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW:
		return "NEEDS_REVIEW"
	case tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED:
		return "REVIEWED"
	default:
		return ""
	}
}

func basLegacyRowDateEquals(row PurchaseReportingRetainedRow, column string, value *tammyv1.CivilDate) bool {
	want := purchaseDateString(value)
	got, present := purchaseCoverageNullableText(row, column)
	return present == (want != "") && (!present || got == want)
}

func basLegacyRowMoneyEquals(row PurchaseReportingRetainedRow, column string, value *tammyv1.Money) bool {
	got, present := purchaseCoverageNullableInteger(row, column)
	if value == nil {
		return !present
	}
	return present && value.CurrencyCode == "AUD" && got == value.MinorUnits
}

func basLegacyCapturedPostings(
	capture *PurchaseCoverageReportingCapture,
	states map[string]BASDocumentState,
	documents map[string]*tammyv1.Document,
	keys map[string]BASScalarKey,
	rows map[string]PurchaseReportingRetainedRow,
) (map[string]*tammyv1.PurchasePosting, error) {
	if len(capture.Postings) != len(keys) || len(rows) != len(keys) {
		return nil, ErrRepository
	}
	result := make(map[string]*tammyv1.PurchasePosting, len(capture.Postings))
	for _, retained := range capture.Postings {
		p := retained.Posting
		if p == nil || p.Id == "" || p.OrganisationId != capture.OrganisationID {
			return nil, ErrRepository
		}
		key, keyOK := keys[p.Id]
		row, rowOK := rows[p.Id]
		state, stateOK := states[p.DocumentId]
		doc := documents[p.DocumentId]
		encoded, bytesOK := postingReportingBytes(row, "posting_proto")
		canonical, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(p)
		candidateHashMatches := false
		if doc != nil && doc.Candidate != nil {
			candidateBytes, candidateErr := purchaseBytes(doc.Candidate)
			if candidateErr == nil {
				candidateHash := sha256.Sum256(candidateBytes)
				candidateHashMatches = bytes.Equal(candidateHash[:], p.ReviewedSnapshotHash)
			}
		}
		if !keyOK || !rowOK || !stateOK || doc == nil || !candidateHashMatches || !bytesOK || marshalErr != nil || !bytes.Equal(encoded, canonical) ||
			state.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || state.PostingID == nil || *state.PostingID != p.Id ||
			key.OrganisationID != p.OrganisationId || !key.IsRoot || key.DocumentID == nil || *key.DocumentID != p.DocumentId ||
			key.SourceID == nil || *key.SourceID != p.DocumentId || key.SourceVersion == nil || *key.SourceVersion != p.ReviewedVersion ||
			!bytes.Equal(key.SourceHash, p.OriginalContentHash) || !bytes.Equal(state.ContentHash, p.OriginalContentHash) ||
			!bytes.Equal(key.RecordHash, p.SourceHash) || !bytes.Equal(key.ReviewedSnapshotHash, p.ReviewedSnapshotHash) ||
			!basLegacyOptionalStringMatches(key.JournalID, p.JournalId) ||
			postingReportingString(row, "organisation_id") != p.OrganisationId || postingReportingString(row, "document_id") != p.DocumentId ||
			!basLegacyRowIntEquals(row, "reviewed_version", int64(p.ReviewedVersion)) ||
			!basLegacyRowBlobEquals(row, "original_content_hash", p.OriginalContentHash, true) ||
			!basLegacyRowBlobEquals(row, "reviewed_snapshot_hash", p.ReviewedSnapshotHash, true) ||
			!basLegacyRowBlobEquals(row, "source_hash", p.SourceHash, true) {
			return nil, ErrRepository
		}
		if _, exists := result[p.Id]; exists {
			return nil, ErrRepository
		}
		result[p.Id] = p
	}
	return result, nil
}

func basLegacyCapturedPayments(
	capture *PurchaseCoverageReportingCapture,
	keys map[string]BASScalarKey,
	rows map[string]PurchaseReportingRetainedRow,
) (map[string]*tammyv1.PurchasePayment, error) {
	result := make(map[string]*tammyv1.PurchasePayment, len(keys))
	for _, retained := range capture.Postings {
		if retained.Posting == nil {
			return nil, ErrRepository
		}
		for _, payment := range retained.Payments {
			if payment == nil || payment.Id == "" {
				return nil, ErrRepository
			}
			key, keyOK := keys[payment.Id]
			row, rowOK := rows[payment.Id]
			encoded, bytesOK := postingReportingBytes(row, "payment_proto")
			canonical, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(payment)
			if !keyOK || !rowOK || !bytesOK || marshalErr != nil || !bytes.Equal(encoded, canonical) ||
				key.OrganisationID != capture.OrganisationID || key.PostingID == nil || *key.PostingID != payment.PostingId ||
				key.AccountingVersion == nil || *key.AccountingVersion != payment.AccountingVersion ||
				!bytes.Equal(key.RecordHash, payment.SourceHash) || key.ActualPaymentDate == nil ||
				*key.ActualPaymentDate != purchaseDateString(payment.ActualPaymentDate) ||
				postingReportingString(row, "organisation_id") != payment.OrganisationId ||
				postingReportingString(row, "posting_id") != payment.PostingId ||
				postingReportingString(row, "payment_date") != purchaseDateString(payment.ActualPaymentDate) ||
				!basLegacyRowIntEquals(row, "accounting_version", int64(payment.AccountingVersion)) ||
				!basLegacyRowBlobEquals(row, "source_hash", payment.SourceHash, true) {
				return nil, ErrRepository
			}
			if _, exists := result[payment.Id]; exists {
				return nil, ErrRepository
			}
			result[payment.Id] = payment
		}
	}
	if len(result) != len(keys) || len(rows) != len(keys) {
		return nil, ErrRepository
	}
	return result, nil
}

func basLegacyCapturedLegacySources(
	organisationID string,
	discovery *BASLegacyCandidateDiscovery,
	states map[string]BASDocumentState,
	keys []BASScalarKey,
	decisions []*tammyv1.LegacyPurchaseReview,
	capture *PurchaseCoverageReportingCapture,
	rowsByTable map[string]map[string]PurchaseReportingRetainedRow,
) (map[string]*LegacyPurchaseReportingRecord, error) {
	if capture == nil {
		return nil, ErrRepository
	}
	byDocument := make(map[string]*LegacyPurchaseReportingRecord)
	byOpening := make(map[string]*LegacyPurchaseReportingRecord)
	rootSet := make(map[BASLegacyCandidateKey]struct{}, len(discovery.Nominations))
	for _, nomination := range discovery.Nominations {
		rootSet[nomination.Key] = struct{}{}
	}
	capturedRoots := make(map[BASLegacyCandidateKey]struct{})
	records := make(map[string]*LegacyPurchaseReportingRecord)
	for _, record := range capture.LegacySources {
		if record == nil || (record.Document == nil) == (record.Opening == nil) {
			return nil, ErrRepository
		}
		var key BASLegacyCandidateKey
		if record.Document != nil {
			key = BASLegacyCandidateKey{Kind: BASLegacyCandidateDocument, OrganisationID: organisationID, SourceID: record.Document.Id}
			state, exists := states[record.Document.Id]
			if !exists || record.Document.OrganisationId != organisationID || state.Version != record.Document.Version ||
				state.Status != record.Document.Status || !bytes.Equal(state.ContentHash, record.Document.Sha256) ||
				record.CurrentRoot != (state.PostingID == nil) {
				return nil, ErrRepository
			}
			if record.Document.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED {
				currentSource, err := purchaseCoverageCurrentDocumentSource(record.Document)
				if err != nil || record.Source == nil || !proto.Equal(currentSource, record.Source) {
					return nil, ErrRepository
				}
			} else if record.Source != nil {
				return nil, ErrRepository
			}
			if _, duplicate := byDocument[record.Document.Id]; duplicate {
				return nil, ErrRepository
			}
			byDocument[record.Document.Id] = record
		} else {
			opening := record.Opening
			key = BASLegacyCandidateKey{Kind: BASLegacyCandidateOpeningPayable, OrganisationID: organisationID, SourceID: opening.ID, ConversionID: opening.ConversionID}
			payableKey, payableOK := basLegacyFindOpeningKey(keys, BASScalarOpeningPayable, opening.ID)
			conversionKey, conversionOK := basLegacyFindOpeningKey(keys, BASScalarOpeningConversion, opening.ConversionID)
			itemKey, itemOK := basLegacyFindOpeningKey(keys, BASScalarOpeningItem, opening.ID)
			payableRow, payableRowOK := rowsByTable["purchase_opening_payables"][opening.ID]
			conversionRow, conversionRowOK := rowsByTable["opening_conversions"][opening.ConversionID]
			itemRow, itemRowOK := basLegacyFindOpeningItemRow(capture.Rows, opening.ID, opening.ConversionID)
			inputBytes, marshalErr := purchaseBytes(opening.Input)
			inputHash := sha256.Sum256(inputBytes)
			retainedInput, retainedInputOK := postingReportingBytes(payableRow, "retained_input_proto")
			conversionSourceHash, conversionHashOK := purchaseCoverageNullableText(conversionRow, "source_sha256")
			decodedConversionHash, decodeErr := hex.DecodeString(conversionSourceHash)
			if !payableOK || !conversionOK || !itemOK || !payableRowOK || !conversionRowOK || !itemRowOK || marshalErr != nil ||
				!retainedInputOK || !bytes.Equal(inputBytes, retainedInput) || !bytes.Equal(inputHash[:], opening.SourceHash) ||
				!conversionHashOK || decodeErr != nil || !bytes.Equal(decodedConversionHash, opening.ConversionHash) ||
				!payableKey.IsRoot || payableKey.ConversionID == nil || *payableKey.ConversionID != opening.ConversionID ||
				itemKey.ConversionID == nil || *itemKey.ConversionID != opening.ConversionID ||
				itemKey.AccountID == nil || *itemKey.AccountID != opening.Input.GetAccountId() ||
				conversionKey.ObjectVersion == nil || *conversionKey.ObjectVersion != opening.SourceVersion ||
				!bytes.Equal(conversionKey.SourceHash, []byte(conversionSourceHash)) ||
				postingReportingString(payableRow, "conversion_id") != opening.ConversionID ||
				postingReportingString(conversionRow, "organisation_id") != organisationID ||
				!basLegacyRowIntEquals(conversionRow, "version", int64(opening.SourceVersion)) ||
				postingReportingString(conversionRow, "state") != opening.State ||
				postingReportingString(conversionRow, "journal_id") != opening.JournalID ||
				postingReportingString(itemRow, "conversion_id") != opening.ConversionID ||
				postingReportingString(itemRow, "account_id") != opening.Input.GetAccountId() ||
				record.Source == nil || !proto.Equal(opening.LegacySource(), record.Source) || !record.CurrentRoot {
				return nil, ErrRepository
			}
			if _, duplicate := byOpening[opening.ID]; duplicate {
				return nil, ErrRepository
			}
			byOpening[opening.ID] = record
		}
		if record.CurrentRoot {
			if _, exists := rootSet[key]; !exists {
				return nil, ErrRepository
			}
			if _, duplicate := capturedRoots[key]; duplicate {
				return nil, ErrRepository
			}
			capturedRoots[key] = struct{}{}
		}
		if record.Document != nil {
			recordKey := "DOCUMENT:" + record.Document.Id
			if _, exists := records[recordKey]; exists {
				return nil, ErrRepository
			}
			records[recordKey] = record
		} else {
			recordKey := "OPENING_PAYABLE:" + record.Opening.ID
			if _, exists := records[recordKey]; exists {
				return nil, ErrRepository
			}
			records[recordKey] = record
		}
	}
	if len(capturedRoots) != len(rootSet) {
		return nil, ErrRepository
	}
	for key := range rootSet {
		if _, exists := capturedRoots[key]; !exists {
			return nil, ErrRepository
		}
	}
	for _, nomination := range discovery.Nominations {
		if nomination.Key.Kind == BASLegacyCandidateDocument {
			if byDocument[nomination.Key.SourceID] == nil || !byDocument[nomination.Key.SourceID].CurrentRoot {
				return nil, ErrRepository
			}
		} else if byOpening[nomination.Key.SourceID] == nil ||
			byOpening[nomination.Key.SourceID].Opening.ConversionID != nomination.Key.ConversionID ||
			byOpening[nomination.Key.SourceID].Opening.SourceVersion != nomination.SourceVersion {
			return nil, ErrRepository
		}
	}

	decisionByID := make(map[string]*tammyv1.LegacyPurchaseReview, len(decisions))
	decisionHistory := make(map[string][]*tammyv1.LegacyPurchaseReview)
	for _, decision := range decisions {
		if decision == nil || decision.Review == nil {
			return nil, ErrRepository
		}
		decisionByID[decision.Id] = decision
		kind, sourceID, _, _, _, err := legacySourceScope(decision.Review.Source)
		if err != nil {
			return nil, ErrRepository
		}
		decisionHistory[kind+":"+sourceID] = append(decisionHistory[kind+":"+sourceID], decision)
	}
	capturedReviews := make(map[string]*tammyv1.LegacyPurchaseReview, len(decisions))
	for _, record := range capture.LegacySources {
		for _, review := range record.Reviews {
			if review == nil || review.Review == nil {
				return nil, ErrRepository
			}
			if _, duplicate := capturedReviews[review.Id]; duplicate {
				return nil, ErrRepository
			}
			capturedReviews[review.Id] = review
			kind, sourceID, version, rawHash, reviewedHash, err := legacySourceScope(review.Review.Source)
			if err != nil {
				return nil, ErrRepository
			}
			if kind == "DOCUMENT" {
				current := byDocument[sourceID]
				if current != nil && current.Document != nil && version == current.Document.Version && bytes.Equal(rawHash, current.Document.Sha256) {
					currentSource, err := purchaseCoverageCurrentDocumentSource(current.Document)
					if err != nil || !bytes.Equal(reviewedHash, currentSource.GetDocument().ReviewedSnapshotHash) {
						return nil, ErrRepository
					}
				}
			} else {
				current := byOpening[sourceID]
				if current != nil && current.Opening != nil && version == current.Opening.SourceVersion {
					if !bytes.Equal(rawHash, current.Opening.SourceHash) || !proto.Equal(review.Review.Source, current.Opening.LegacySource()) {
						return nil, ErrRepository
					}
				}
			}
		}
		recordKind, recordID := "", ""
		if record.Document != nil {
			recordKind, recordID = "DOCUMENT", record.Document.Id
		} else {
			recordKind, recordID = "OPENING_PAYABLE", record.Opening.ID
		}
		expectedHistory := decisionHistory[recordKind+":"+recordID]
		if len(record.Reviews) != len(expectedHistory) {
			return nil, ErrRepository
		}
		for index, expected := range expectedHistory {
			if !proto.Equal(record.Reviews[index], expected) {
				return nil, ErrRepository
			}
		}
	}
	if len(capturedReviews) != len(decisionByID) {
		return nil, ErrRepository
	}
	for id, expected := range decisionByID {
		actual := capturedReviews[id]
		if actual == nil || !proto.Equal(actual, expected) {
			return nil, ErrRepository
		}
	}
	legacyRows := rowsByTable["purchase_legacy_reviews_v1"]
	if len(legacyRows) != len(decisionByID) {
		return nil, ErrRepository
	}
	for id, decision := range decisionByID {
		row, ok := legacyRows[id]
		if !ok || !basLegacyReviewRowMatches(row, decision) {
			return nil, ErrRepository
		}
	}
	return records, nil
}

func basLegacyFindOpeningKey(keys []BASScalarKey, kind BASScalarKeyKind, id string) (BASScalarKey, bool) {
	for _, key := range keys {
		if key.Kind == kind && key.ID == id {
			return key, true
		}
	}
	return BASScalarKey{}, false
}

func basLegacyFindOpeningItemRow(rows []PurchaseReportingRetainedRow, id, conversionID string) (PurchaseReportingRetainedRow, bool) {
	var found PurchaseReportingRetainedRow
	count := 0
	for _, row := range rows {
		if row.Table == "opening_items" && postingReportingString(row, "id") == id &&
			postingReportingString(row, "conversion_id") == conversionID {
			found = row
			count++
		}
	}
	return found, count == 1
}

func basLegacyReviewRowMatches(row PurchaseReportingRetainedRow, record *tammyv1.LegacyPurchaseReview) bool {
	if record == nil || record.Review == nil || row.Table != "purchase_legacy_reviews_v1" ||
		postingReportingString(row, "id") != record.Id || postingReportingString(row, "organisation_id") != record.OrganisationId {
		return false
	}
	kind, sourceID, sourceVersion, rawHash, reviewedHash, err := legacySourceScope(record.Review.Source)
	if err != nil {
		return false
	}
	wantKind := kind
	if kind == "OPENING_PAYABLE" {
		wantKind = "OPENING_PAYABLE"
	}
	input := record.Review
	var claimedStart, claimedEnd *string
	if input.PreviouslyClaimedPeriod != nil {
		start, end := purchaseDateString(input.PreviouslyClaimedPeriod.PeriodStart), purchaseDateString(input.PreviouslyClaimedPeriod.PeriodEnd)
		claimedStart, claimedEnd = &start, &end
	}
	var claimID *string
	var claimVersion *uint64
	var claimHash []byte
	if input.ClaimEvidence != nil {
		claimID, claimVersion, claimHash = &input.ClaimEvidence.SourceId, &input.ClaimEvidence.SourceVersion, input.ClaimEvidence.SourceHash
	}
	content, contentOK := postingReportingBytes(row, "review_proto")
	encoded, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(record)
	if !contentOK || marshalErr != nil || !bytes.Equal(content, encoded) ||
		!basLegacyRowBlobEquals(row, "record_hash", record.SourceHash, true) ||
		!basLegacyRowBlobEquals(row, "source_hash", rawHash, true) ||
		!basLegacyRowBlobEquals(row, "reviewed_snapshot_hash", reviewedHash, len(reviewedHash) != 0) ||
		postingReportingString(row, "source_kind") != wantKind ||
		!basLegacyRowIntEquals(row, "source_version", int64(sourceVersion)) ||
		!basLegacyRowIntEquals(row, "accounting_version", int64(record.SourceRevision)) ||
		!basLegacyRowTextEquals(row, "affected_period_start", purchaseDateString(input.AffectedPeriod.PeriodStart), true) ||
		!basLegacyRowTextEquals(row, "affected_period_end", purchaseDateString(input.AffectedPeriod.PeriodEnd), true) ||
		!basLegacyRowTextEquals(row, "reporting_balance_date", purchaseDateString(input.ReportingBalanceDate), true) ||
		!basLegacyRowIntEquals(row, "disposition", int64(input.Disposition)) ||
		!basLegacyRowNullableTextEquals(row, "previously_claimed_period_start", claimedStart) ||
		!basLegacyRowNullableTextEquals(row, "previously_claimed_period_end", claimedEnd) ||
		!basLegacyRowNullableTextEquals(row, "claim_evidence_source_id", claimID) ||
		!basLegacyRowNullableUintEquals(row, "claim_evidence_source_version", claimVersion) ||
		!basLegacyRowNullableBlobEquals(row, "claim_evidence_source_hash", claimHash, claimID != nil) ||
		!basLegacyRowNullableTextEquals(row, "linked_posting_id", input.LinkedPostingId) ||
		postingReportingString(row, "reason") != input.Reason || postingReportingString(row, "actor_user_id") != record.ActorUserId ||
		postingReportingString(row, "created_at") != record.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) ||
		postingReportingString(row, "document_id") != basLegacyReviewNullableSourceID(kind, sourceID, true) ||
		postingReportingString(row, "opening_payable_id") != basLegacyReviewNullableSourceID(kind, sourceID, false) {
		return false
	}
	return true
}

func basLegacyReviewNullableSourceID(kind, sourceID string, document bool) string {
	if (kind == "DOCUMENT") == document {
		return sourceID
	}
	return ""
}

func basLegacyCapturedCashProofs(
	organisationID string,
	discovery *BASLegacyCandidateDiscovery,
	cash accounting.BASLegacyCashRead,
	capture *PurchaseCoverageReportingCapture,
	accountRows, factRows map[string]PurchaseReportingRetainedRow,
) (map[string]*accounting.JournalReportingRecord, error) {
	roots := make(map[string]struct{}, len(discovery.Nominations))
	for _, nomination := range discovery.Nominations {
		roots[nomination.Key.SourceID] = struct{}{}
	}
	proofs := make(map[string]*accounting.JournalReportingRecord, len(capture.Journals))
	lineToJournal := make(map[string]string)
	for _, proof := range capture.Journals {
		if proof == nil || proof.Journal == nil || proof.Journal.Id == "" || proof.Journal.OrganisationId != organisationID ||
			proof.SourceKind < 0 || proof.SourceType == "" || proof.SourceID == "" || proof.SourceRevision == 0 ||
			proof.State == "" || len(proof.ContentHash) != sha256.Size {
			return nil, ErrRepository
		}
		if _, duplicate := proofs[proof.Journal.Id]; duplicate {
			return nil, ErrRepository
		}
		if basLegacyJournalStateText(proof.Journal.State) != proof.State || basLegacyJournalDateText(proof.Journal.PostingDate) == "" {
			return nil, ErrRepository
		}
		proofs[proof.Journal.Id] = proof
		for _, line := range proof.Journal.Lines {
			if line == nil || line.Id == "" || line.JournalId != proof.Journal.Id {
				return nil, ErrRepository
			}
			if _, duplicate := lineToJournal[line.Id]; duplicate {
				return nil, ErrRepository
			}
			lineToJournal[line.Id] = proof.Journal.Id
		}
	}
	expectedAssociations := make(map[basLegacyCashAssociationKey]accounting.BASLegacyCashAssociation)
	putAssociation := func(value accounting.BASLegacyCashAssociation) error {
		key := basLegacyCashAssociationKey{sourceID: value.SourceID, journalID: value.JournalID, kind: value.Kind}
		if value.TaxFactID != nil {
			key.taxFactID = *value.TaxFactID
		}
		if _, duplicate := expectedAssociations[key]; duplicate {
			return ErrRepository
		}
		expectedAssociations[key] = value
		return nil
	}
	for _, proof := range proofs {
		if _, requested := roots[proof.SourceID]; requested && proof.SourceKind == 0 && proof.SourceType != "OPENING" {
			if err := putAssociation(accounting.BASLegacyCashAssociation{SourceID: proof.SourceID, JournalID: proof.Journal.Id, Kind: "DIRECT"}); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range factRows {
		sourceID := postingReportingString(row, "source_id")
		if _, requested := roots[sourceID]; !requested {
			continue
		}
		factID := postingReportingString(row, "id")
		lineID := postingReportingString(row, "journal_line_id")
		journalID, lineExists := lineToJournal[lineID]
		proof := proofs[journalID]
		sourceType := postingReportingString(row, "source_type")
		sourceRevision, revisionOK := purchaseCoverageNullableInteger(row, "source_revision")
		factHashValue, factHashOK := basLegacyRowValue(row, "source_content_hash")
		if !factHashOK || (factHashValue.Kind != "NULL" && factHashValue.Kind != "BLOB") {
			return nil, ErrRepository
		}
		factHashPresent := factHashValue.Kind == "BLOB"
		factHash := bytes.Clone(factHashValue.Bytes)
		if !lineExists || proof == nil {
			return nil, ErrRepository
		}
		if proof.SourceKind == 6 || proof.SourceKind == 7 || proof.SourceKind == 8 || proof.SourceType == "OPENING" {
			continue
		}
		if factID == "" || lineID == "" || proof.SourceKind != 0 ||
			sourceType == "" || !revisionOK || sourceRevision <= 0 ||
			(factHashPresent && len(factHash) != sha256.Size) {
			return nil, ErrRepository
		}
		revision := uint64(sourceRevision)
		association := accounting.BASLegacyCashAssociation{SourceID: sourceID, JournalID: journalID, Kind: "TAX_FACT",
			TaxFactID: basLegacyStringPointer(factID), JournalLineID: basLegacyStringPointer(lineID), FactSourceType: basLegacyStringPointer(sourceType),
			FactSourceRevision: &revision, FactSourceContentHashPresent: factHashPresent}
		if factHashPresent {
			association.FactSourceContentHash = bytes.Clone(factHash)
		}
		if err := putAssociation(association); err != nil {
			return nil, err
		}
	}
	actualAssociations := make(map[basLegacyCashAssociationKey]accounting.BASLegacyCashAssociation, len(cash.Associations))
	for _, association := range cash.Associations {
		key := basLegacyCashAssociationKey{sourceID: association.SourceID, journalID: association.JournalID, kind: association.Kind}
		if association.TaxFactID != nil {
			key.taxFactID = *association.TaxFactID
		}
		if _, duplicate := actualAssociations[key]; duplicate {
			return nil, ErrRepository
		}
		actualAssociations[key] = association
	}
	if len(actualAssociations) != len(expectedAssociations) {
		return nil, ErrRepository
	}
	for key, expected := range expectedAssociations {
		actual, exists := actualAssociations[key]
		if !exists || !basLegacySameCashAssociation(actual, expected) {
			return nil, ErrRepository
		}
	}

	closure := make(map[string]struct{})
	queue := make([]string, 0, len(expectedAssociations))
	for _, association := range expectedAssociations {
		queue = append(queue, association.JournalID)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, seen := closure[id]; seen {
			continue
		}
		proof := proofs[id]
		if proof == nil || proof.SourceKind != 0 {
			return nil, ErrRepository
		}
		closure[id] = struct{}{}
		if proof.Journal.ReversalOfJournalId != nil {
			queue = append(queue, *proof.Journal.ReversalOfJournalId)
		}
		if proof.Journal.ReversedByJournalId != nil {
			queue = append(queue, *proof.Journal.ReversedByJournalId)
		}
	}
	if len(closure) != len(cash.Journals) {
		return nil, ErrRepository
	}
	cashJournals := make(map[string]accounting.BASLegacyCashJournal, len(cash.Journals))
	for _, value := range cash.Journals {
		if _, duplicate := cashJournals[value.ID]; duplicate {
			return nil, ErrRepository
		}
		cashJournals[value.ID] = value
	}
	for id := range closure {
		proof := proofs[id]
		actual, exists := cashJournals[id]
		if !exists || !basLegacyCashJournalMatches(actual, proof) {
			return nil, ErrRepository
		}
	}

	accounts := make(map[string]PurchaseReportingRetainedRow, len(accountRows))
	for id, row := range accountRows {
		if row.Table != "accounts" || postingReportingString(row, "id") != id || postingReportingString(row, "organisation_id") != organisationID {
			return nil, ErrRepository
		}
		accounts[id] = row
	}
	expectedLines := make(map[basLegacyCashLineKey]accounting.BASLegacyCashLine)
	for id := range closure {
		proof := proofs[id]
		for _, line := range proof.Journal.Lines {
			accountRow, ok := accounts[line.AccountId]
			if !ok || line.GetDebit() == nil || line.GetCredit() == nil || line.Debit.CurrencyCode != "AUD" || line.Credit.CurrencyCode != "AUD" {
				return nil, ErrRepository
			}
			accountVersion, versionOK := purchaseCoverageNullableInteger(accountRow, "version")
			classification, classOK := purchaseCoverageNullableText(accountRow, "report_classification")
			if !versionOK || !classOK || accountVersion <= 0 || classification == "" {
				return nil, ErrRepository
			}
			value := accounting.BASLegacyCashLine{JournalID: id, ID: line.Id, AccountID: line.AccountId,
				AccountOrganisationID: organisationID, Sequence: uint64(line.Sequence), AccountVersion: uint64(accountVersion),
				DebitMinor: line.Debit.MinorUnits, CreditMinor: line.Credit.MinorUnits, ReportClassification: classification}
			key := basLegacyCashLineKey{journalID: value.JournalID, id: value.ID, sequence: value.Sequence}
			if _, duplicate := expectedLines[key]; duplicate {
				return nil, ErrRepository
			}
			expectedLines[key] = value
		}
	}
	actualLines := make(map[basLegacyCashLineKey]accounting.BASLegacyCashLine, len(cash.Lines))
	for _, value := range cash.Lines {
		key := basLegacyCashLineKey{journalID: value.JournalID, id: value.ID, sequence: value.Sequence}
		if _, duplicate := actualLines[key]; duplicate {
			return nil, ErrRepository
		}
		actualLines[key] = value
	}
	if len(actualLines) != len(expectedLines) {
		return nil, ErrRepository
	}
	for key, expected := range expectedLines {
		if actual, exists := actualLines[key]; !exists || actual != expected {
			return nil, ErrRepository
		}
	}
	return proofs, nil
}

type basLegacyCashLineKey struct {
	journalID, id string
	sequence      uint64
}

func basLegacySameCashAssociation(left, right accounting.BASLegacyCashAssociation) bool {
	return left.SourceID == right.SourceID && left.JournalID == right.JournalID && left.Kind == right.Kind &&
		basLegacyOptionalStringMatches(left.TaxFactID, right.TaxFactID) &&
		basLegacyOptionalStringMatches(left.JournalLineID, right.JournalLineID) &&
		basLegacyOptionalStringMatches(left.FactSourceType, right.FactSourceType) &&
		basLegacyOptionalUintMatches(left.FactSourceRevision, right.FactSourceRevision) &&
		left.FactSourceContentHashPresent == right.FactSourceContentHashPresent &&
		bytes.Equal(left.FactSourceContentHash, right.FactSourceContentHash)
}

func basLegacyCashJournalMatches(value accounting.BASLegacyCashJournal, proof *accounting.JournalReportingRecord) bool {
	if proof == nil || proof.Journal == nil {
		return false
	}
	journal := proof.Journal
	return value.OrganisationID == journal.OrganisationId && value.ID == journal.Id && value.Version == journal.Version &&
		value.SourceRevision == proof.SourceRevision && value.SourceKind == proof.SourceKind &&
		value.SourceType == proof.SourceType && value.SourceID == proof.SourceID && value.State == proof.State &&
		value.JournalDate == basLegacyJournalDateText(journal.PostingDate) &&
		basLegacyOptionalStringMatches(value.ReversalOfID, journal.ReversalOfJournalId) &&
		basLegacyOptionalStringMatches(value.ReversedByID, journal.ReversedByJournalId)
}

func basLegacyJournalStateText(value tammyv1.JournalState) string {
	switch value {
	case tammyv1.JournalState_JOURNAL_STATE_UNSPECIFIED:
		return "DRAFT"
	case tammyv1.JournalState_JOURNAL_STATE_POSTED:
		return "POSTED"
	case tammyv1.JournalState_JOURNAL_STATE_REVERSED:
		return "REVERSED"
	default:
		return ""
	}
}

func basLegacyStringPointer(value string) *string { return &value }

func basLegacyOptionalStringMatches(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func basLegacyOptionalUintMatches(left, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func basLegacyRowValue(row PurchaseReportingRetainedRow, column string) (PurchaseReportingRetainedValue, bool) {
	for index, name := range row.Columns {
		if name == column && index < len(row.Values) {
			return row.Values[index], true
		}
	}
	return PurchaseReportingRetainedValue{}, false
}

func basLegacyRowIntEquals(row PurchaseReportingRetainedRow, column string, expected int64) bool {
	actual, ok := purchaseCoverageNullableInteger(row, column)
	return ok && actual == expected
}

func basLegacyRowTextEquals(row PurchaseReportingRetainedRow, column, expected string, expectedPresent bool) bool {
	return basLegacyRowNullableTextEquals(row, column, basLegacyStringPointerIf(expected, expectedPresent))
}

func basLegacyRowNullableTextEquals(row PurchaseReportingRetainedRow, column string, expected *string) bool {
	actual, exists := basLegacyRowValue(row, column)
	if !exists {
		return false
	}
	if expected == nil {
		return actual.Kind == "NULL"
	}
	return actual.Kind == "TEXT" && actual.Text == *expected
}

func basLegacyRowNullableUintEquals(row PurchaseReportingRetainedRow, column string, expected *uint64) bool {
	actual, exists := basLegacyRowValue(row, column)
	if !exists {
		return false
	}
	if expected == nil {
		return actual.Kind == "NULL"
	}
	return actual.Kind == "INTEGER" && actual.Integer >= 0 && uint64(actual.Integer) == *expected
}

func basLegacyRowNullableBlobEquals(row PurchaseReportingRetainedRow, column string, expected []byte, expectedPresent bool) bool {
	actual, exists := basLegacyRowValue(row, column)
	if !exists {
		return false
	}
	if !expectedPresent {
		return actual.Kind == "NULL" && len(expected) == 0
	}
	return actual.Kind == "BLOB" && bytes.Equal(actual.Bytes, expected)
}

func basLegacyStringPointerIf(value string, present bool) *string {
	if !present {
		return nil
	}
	return &value
}

func basLegacyRowBlobEquals(row PurchaseReportingRetainedRow, column string, expected []byte, expectedPresent bool) bool {
	actual, exists := basLegacyRowValue(row, column)
	if !exists {
		return false
	}
	if actual.Kind == "NULL" {
		return !expectedPresent && len(expected) == 0
	}
	return expectedPresent && actual.Kind == "BLOB" && bytes.Equal(actual.Bytes, expected)
}

func basLegacyJournalDateText(value *tammyv1.CivilDate) string {
	if _, valid := basLegacyCashCivilDateKey(value); !valid {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
}
