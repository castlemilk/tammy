package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

const (
	basSharedTestOrganisation = "018f0000-0000-7000-8000-000000000001"
	basSharedTestJournal      = "journal-a"
	basSharedTestOutside      = "journal-z"
)

func TestVerifyBASSharedTaxFactsReconcilesSelectedSubsetAndIndependentCharges(t *testing.T) {
	purchase, taxCapture := basSharedTestFactCaptures()
	beforePurchase := basSharedClonePurchaseCapture(purchase)
	beforeTaxCapture := basSharedCloneAccountingCapture(taxCapture)

	proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture)
	if err != nil {
		t.Fatalf("verifyBASSharedTaxFacts() error = %v", err)
	}
	if proof == nil || proof.Count != 1 || proof.PurchaseBytes != 685 ||
		proof.AccountingSubsetBytes != 719 || proof.PurchaseBytes == proof.AccountingSubsetBytes {
		t.Fatalf("proof = %#v; want one exact shared fact with distinct raw/canonical charges 685/719", proof)
	}
	if taxCapture.FactCount != 2 || taxCapture.BasePayloadBytes != 1438 ||
		len(taxCapture.Facts) != 2 || taxCapture.Facts[1].Journal.Id != basSharedTestOutside {
		t.Fatalf("out-of-scope accounting fact was removed or omitted from its base charge: %#v", taxCapture)
	}
	if !reflect.DeepEqual(beforePurchase, purchase) || !reflect.DeepEqual(beforeTaxCapture, taxCapture) {
		t.Fatal("reconciliation mutated one of its retained captures")
	}
}

func TestVerifyBASSharedTaxFactsRejectsEveryRawCellMismatch(t *testing.T) {
	for index, column := range basSharedTaxFactColumns {
		t.Run(column, func(t *testing.T) {
			purchase, taxCapture := basSharedTestFactCaptures()
			value := &purchase.Rows[0].Values[index]
			switch index {
			case basSharedFactID, basSharedFactOrganisationID, basSharedFactJournalLineID,
				basSharedFactTaxCode, basSharedFactTreatment, basSharedFactRuleType,
				basSharedFactRuleID, basSharedFactSourceType, basSharedFactSourceID,
				basSharedFactCreatedAt:
				value.Text += "-tampered"
			case basSharedFactOriginalGross, basSharedFactOriginalNet, basSharedFactOriginalGST,
				basSharedFactAttributedGross, basSharedFactAttributedNet, basSharedFactAttributedGST,
				basSharedFactRemainingGross, basSharedFactRemainingNet, basSharedFactRemainingGST,
				basSharedFactRuleRevision, basSharedFactSourceRevision:
				value.Integer++
			case basSharedFactRuleHash:
				value.Bytes[0] ^= 0xff
			case basSharedFactSourceHash:
				value.Kind = "BLOB"
				value.Bytes = bytes.Repeat([]byte{0x4c}, 32)
			default:
				t.Fatalf("test omitted raw column index %d", index)
			}
			purchase.TaxFactPayloadBytes = basSharedTestPurchaseRowCharge(purchase.Rows[0])
			if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
				t.Fatalf("tampered %s was accepted with proof %#v", column, proof)
			}
		})
	}
}

func TestVerifyBASSharedTaxFactsRequiresCompleteUniqueSelectedRows(t *testing.T) {
	t.Run("missing-selected-row", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.Rows = nil
		purchase.TaxFactPayloadBytes = 0
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("missing selected raw fact was accepted: %#v", proof)
		}
	})
	t.Run("duplicate-raw-id", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		duplicate := basSharedCloneRow(purchase.Rows[0])
		purchase.Rows = append(purchase.Rows, duplicate)
		purchase.TaxFactPayloadBytes += basSharedTestPurchaseRowCharge(duplicate)
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("duplicate raw fact was accepted: %#v", proof)
		}
	})
	t.Run("duplicate-accounting-id", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		taxCapture.Facts = append(taxCapture.Facts, basSharedCloneFact(taxCapture.Facts[0]))
		taxCapture.FactCount++
		taxCapture.BasePayloadBytes += basSharedTestAccountingCharge(taxCapture.Facts[0])
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("duplicate canonical fact was accepted: %#v", proof)
		}
	})
	t.Run("wrong-column-order", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.Rows[0].Columns[0], purchase.Rows[0].Columns[1] = purchase.Rows[0].Columns[1], purchase.Rows[0].Columns[0]
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("wrong raw projection column order was accepted: %#v", proof)
		}
	})
}

func TestVerifyBASSharedTaxFactsRequiresExactJournalProofIdentity(t *testing.T) {
	t.Run("duplicate-journal-id", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.JournalIDs = []string{basSharedTestJournal, basSharedTestJournal}
		purchase.JournalCount = 2
		purchase.JournalPayloadBytes[basSharedTestOutside] = 1024
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("duplicate selected journal id was accepted: %#v", proof)
		}
	})
	t.Run("missing-purchase-journal-proof", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.Journals = nil
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("missing selected journal proof was accepted: %#v", proof)
		}
	})
	t.Run("journal-proof-outside-selected-list", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.Journals[0].Journal.Id = basSharedTestOutside
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("out-of-set journal proof was accepted: %#v", proof)
		}
	})
	t.Run("journal-payload-map-key-mismatch", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.JournalPayloadBytes = map[string]int64{basSharedTestOutside: 1024}
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("journal payload map with a foreign key was accepted: %#v", proof)
		}
	})
}

func TestVerifyBASSharedTaxFactsPreservesNullableHashAndStorageKinds(t *testing.T) {
	t.Run("null-source-hash", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		if purchase.Rows[0].Values[basSharedFactSourceHash].Kind != "NULL" ||
			taxCapture.Facts[0].SourceContentHashPresent || taxCapture.Facts[0].SourceContentHash != nil {
			t.Fatal("fixture did not represent a retained SQL NULL")
		}
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err != nil || proof == nil {
			t.Fatalf("matching NULL source hash proof=%#v err=%v", proof, err)
		}
	})
	t.Run("null-versus-empty-blob", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.Rows[0].Values[basSharedFactSourceHash] = basSharedTestBlob([]byte{})
		purchase.TaxFactPayloadBytes = basSharedTestPurchaseRowCharge(purchase.Rows[0])
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("empty BLOB was treated as SQL NULL: %#v", proof)
		}
	})
	t.Run("present-32-byte-source-hash", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		hash := bytes.Repeat([]byte{0x5a}, 32)
		taxCapture.Facts[0].SourceContentHashPresent = true
		taxCapture.Facts[0].SourceContentHash = bytes.Clone(hash)
		purchase.Rows[0].Values[basSharedFactSourceHash] = basSharedTestBlob(hash)
		purchase.TaxFactPayloadBytes = basSharedTestPurchaseRowCharge(purchase.Rows[0])
		taxCapture.BasePayloadBytes = basSharedTestAccountingCharge(taxCapture.Facts[0]) +
			basSharedTestAccountingCharge(taxCapture.Facts[1])
		proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture)
		if err != nil || proof == nil || proof.Count != 1 || proof.AccountingSubsetBytes != 751 {
			t.Fatalf("matching present source hash proof=%#v err=%v", proof, err)
		}
	})
	t.Run("text-storage-for-integer", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.Rows[0].Values[basSharedFactOriginalGross] = basSharedTestText("-11")
		purchase.TaxFactPayloadBytes = basSharedTestPurchaseRowCharge(purchase.Rows[0])
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("TEXT amount was normalized into INTEGER: %#v", proof)
		}
	})
	t.Run("blob-storage-for-text", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		purchase.Rows[0].Values[basSharedFactSourceID] = basSharedTestBlob([]byte("source-a"))
		purchase.TaxFactPayloadBytes = basSharedTestPurchaseRowCharge(purchase.Rows[0])
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("BLOB source id was normalized into TEXT: %#v", proof)
		}
	})
}

func TestVerifyBASSharedTaxFactsRejectsInvalidInputsAndCancellationWithoutMutation(t *testing.T) {
	t.Run("nil-context", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		if proof, err := verifyBASSharedTaxFacts(nil, basSharedTestOrganisation, purchase, taxCapture); err == nil || proof != nil {
			t.Fatalf("nil context result = %#v, %v", proof, err)
		}
	})
	t.Run("nil-captures", func(t *testing.T) {
		if proof, err := verifyBASSharedTaxFacts(context.Background(), basSharedTestOrganisation, nil, nil); err == nil || proof != nil {
			t.Fatalf("nil captures result = %#v, %v", proof, err)
		}
	})
	t.Run("already-cancelled", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		beforePurchase := basSharedClonePurchaseCapture(purchase)
		beforeTaxCapture := basSharedCloneAccountingCapture(taxCapture)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if proof, err := verifyBASSharedTaxFacts(ctx, basSharedTestOrganisation, purchase, taxCapture); !errors.Is(err, context.Canceled) || proof != nil {
			t.Fatalf("cancelled result = %#v, %v", proof, err)
		}
		if !reflect.DeepEqual(beforePurchase, purchase) || !reflect.DeepEqual(beforeTaxCapture, taxCapture) {
			t.Fatal("cancelled reconciliation mutated a retained capture")
		}
	})
	t.Run("cancel-during-iteration", func(t *testing.T) {
		purchase, taxCapture := basSharedTestFactCaptures()
		beforePurchase := basSharedClonePurchaseCapture(purchase)
		beforeTaxCapture := basSharedCloneAccountingCapture(taxCapture)
		base, cancel := context.WithCancel(context.Background())
		ctx := &basSharedCancelAfterContext{Context: base, cancel: cancel, cancelAt: 5}
		if proof, err := verifyBASSharedTaxFacts(ctx, basSharedTestOrganisation, purchase, taxCapture); !errors.Is(err, context.Canceled) || proof != nil {
			t.Fatalf("mid-iteration cancellation result = %#v, %v", proof, err)
		}
		if !reflect.DeepEqual(beforePurchase, purchase) || !reflect.DeepEqual(beforeTaxCapture, taxCapture) {
			t.Fatal("cancelled reconciliation mutated a retained capture")
		}
	})
}

type basSharedCancelAfterContext struct {
	context.Context
	checks   int
	cancelAt int
	cancel   context.CancelFunc
}

func (ctx *basSharedCancelAfterContext) Err() error {
	ctx.checks++
	if ctx.checks >= ctx.cancelAt {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func basSharedTestFactCaptures() (*purchases.PurchaseCoverageReportingCapture, *accounting.TaxFactReportingCapture) {
	selectedJournal := &tammyv1.Journal{
		Id: basSharedTestJournal, OrganisationId: basSharedTestOrganisation,
		PostingDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
	}
	outsideJournal := &tammyv1.Journal{
		Id: basSharedTestOutside, OrganisationId: basSharedTestOrganisation,
		PostingDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
	}
	selected := basSharedTestAccountingFact("fact-a", "line-a", selectedJournal)
	outside := basSharedTestAccountingFact("fact-z", "line-z", outsideJournal)

	purchase := &purchases.PurchaseCoverageReportingCapture{
		OrganisationID: basSharedTestOrganisation,
		JournalCount:   1, JournalIDs: []string{basSharedTestJournal},
		JournalPayloadBytes: map[string]int64{basSharedTestJournal: 1024},
		Journals: []*accounting.JournalReportingRecord{{
			Journal: selectedJournal, ContentHash: bytes.Repeat([]byte{0x11}, 32), PayloadBytes: 1024,
		}},
		Rows:                []purchases.PurchaseReportingRetainedRow{basSharedTestRawFactRow(selected)},
		TaxFactPayloadBytes: 685,
	}
	taxCapture := &accounting.TaxFactReportingCapture{
		Facts:     []accounting.TaxFactReportingRecord{selected, outside},
		FactCount: 2, JournalCount: 2, JournalIDs: []string{basSharedTestJournal, basSharedTestOutside},
		JournalPayloadBytes: map[string]int64{basSharedTestJournal: 1024, basSharedTestOutside: 2048},
		BasePayloadBytes:    1438,
	}
	return purchase, taxCapture
}

func basSharedTestAccountingFact(id, lineID string, journal *tammyv1.Journal) accounting.TaxFactReportingRecord {
	return accounting.TaxFactReportingRecord{
		ID: id, OrganisationID: basSharedTestOrganisation, JournalLineID: lineID,
		TaxCode: "GST", Treatment: "TAXABLE",
		OriginalGrossMinor: -11, OriginalNetMinor: -10, OriginalGSTMinor: -1,
		AttributedGrossMinor: -9, AttributedNetMinor: -8, AttributedGSTMinor: -1,
		RemainingGrossMinor: -2, RemainingNetMinor: -2, RemainingGSTMinor: 0,
		Rule:       &tammyv1.SourceRef{Type: "retained_rule", Id: "rule-a", Revision: 7, ContentHash: bytes.Repeat([]byte{0x31}, 32)},
		SourceType: "manual_evidence", SourceID: "source-a", SourceRevision: 3,
		CreatedAt: "2026-08-10T00:00:00Z", Journal: journal,
	}
}

func basSharedTestRawFactRow(fact accounting.TaxFactReportingRecord) purchases.PurchaseReportingRetainedRow {
	row := purchases.PurchaseReportingRetainedRow{
		Table: "tax_facts", Columns: append([]string(nil), basSharedTaxFactColumns[:]...),
		Values: []purchases.PurchaseReportingRetainedValue{
			basSharedTestText(fact.ID), basSharedTestText(fact.OrganisationID), basSharedTestText(fact.JournalLineID),
			basSharedTestText(fact.TaxCode), basSharedTestText(fact.Treatment),
			basSharedTestInteger(fact.OriginalGrossMinor), basSharedTestInteger(fact.OriginalNetMinor), basSharedTestInteger(fact.OriginalGSTMinor),
			basSharedTestInteger(fact.AttributedGrossMinor), basSharedTestInteger(fact.AttributedNetMinor), basSharedTestInteger(fact.AttributedGSTMinor),
			basSharedTestInteger(fact.RemainingGrossMinor), basSharedTestInteger(fact.RemainingNetMinor), basSharedTestInteger(fact.RemainingGSTMinor),
			basSharedTestText(fact.Rule.Type), basSharedTestText(fact.Rule.Id), basSharedTestInteger(int64(fact.Rule.Revision)),
			basSharedTestBlob(fact.Rule.ContentHash), basSharedTestText(fact.SourceType), basSharedTestText(fact.SourceID),
			basSharedTestInteger(int64(fact.SourceRevision)), {Kind: "NULL"}, basSharedTestText(fact.CreatedAt),
		},
	}
	if fact.SourceContentHashPresent {
		row.Values[basSharedFactSourceHash] = basSharedTestBlob(fact.SourceContentHash)
	}
	return row
}

func basSharedTestText(value string) purchases.PurchaseReportingRetainedValue {
	return purchases.PurchaseReportingRetainedValue{Kind: "TEXT", Text: value}
}

func basSharedTestInteger(value int64) purchases.PurchaseReportingRetainedValue {
	return purchases.PurchaseReportingRetainedValue{Kind: "INTEGER", Integer: value}
}

func basSharedTestBlob(value []byte) purchases.PurchaseReportingRetainedValue {
	return purchases.PurchaseReportingRetainedValue{Kind: "BLOB", Bytes: bytes.Clone(value)}
}

func basSharedTestPurchaseRowCharge(row purchases.PurchaseReportingRetainedRow) int64 {
	total := int64(512)
	for _, value := range row.Values {
		switch value.Kind {
		case "NULL":
		case "INTEGER":
			total += int64(len(strconv.FormatInt(value.Integer, 10)))
		case "TEXT":
			total += int64(len(value.Text))
		case "BLOB":
			total += int64(len(value.Bytes))
		}
	}
	return total
}

func basSharedTestAccountingCharge(fact accounting.TaxFactReportingRecord) int64 {
	journalDate := fmt.Sprintf("%04d-%02d-%02d", fact.Journal.PostingDate.Year, fact.Journal.PostingDate.Month, fact.Journal.PostingDate.Day)
	total := int64(512)
	for _, value := range [][]byte{
		[]byte(fact.ID), []byte(fact.OrganisationID), []byte(fact.JournalLineID),
		[]byte(fact.TaxCode), []byte(fact.Treatment),
		[]byte(fact.Rule.Type), []byte(fact.Rule.Id), fact.Rule.ContentHash,
		[]byte(fact.SourceType), []byte(fact.SourceID), fact.SourceContentHash,
		[]byte(fact.CreatedAt), []byte(fact.Journal.Id), []byte(fact.Journal.OrganisationId), []byte(journalDate),
	} {
		total += int64(len(value))
	}
	return total
}

func basSharedCloneRow(row purchases.PurchaseReportingRetainedRow) purchases.PurchaseReportingRetainedRow {
	copy := row
	copy.Columns = append([]string(nil), row.Columns...)
	copy.Values = append([]purchases.PurchaseReportingRetainedValue(nil), row.Values...)
	for index := range copy.Values {
		copy.Values[index].Bytes = bytes.Clone(copy.Values[index].Bytes)
	}
	return copy
}

func basSharedClonePurchaseCapture(capture *purchases.PurchaseCoverageReportingCapture) *purchases.PurchaseCoverageReportingCapture {
	copy := *capture
	copy.JournalIDs = append([]string(nil), capture.JournalIDs...)
	copy.JournalPayloadBytes = make(map[string]int64, len(capture.JournalPayloadBytes))
	for id, payload := range capture.JournalPayloadBytes {
		copy.JournalPayloadBytes[id] = payload
	}
	copy.Journals = make([]*accounting.JournalReportingRecord, len(capture.Journals))
	for index, proof := range capture.Journals {
		if proof == nil {
			continue
		}
		proofCopy := *proof
		proofCopy.ContentHash = bytes.Clone(proof.ContentHash)
		if proof.Journal != nil {
			proofCopy.Journal = proto.Clone(proof.Journal).(*tammyv1.Journal)
		}
		copy.Journals[index] = &proofCopy
	}
	copy.Rows = make([]purchases.PurchaseReportingRetainedRow, len(capture.Rows))
	for index, row := range capture.Rows {
		copy.Rows[index] = basSharedCloneRow(row)
	}
	return &copy
}

func basSharedCloneAccountingCapture(capture *accounting.TaxFactReportingCapture) *accounting.TaxFactReportingCapture {
	copy := *capture
	copy.Fingerprint = bytes.Clone(capture.Fingerprint)
	copy.JournalIDs = append([]string(nil), capture.JournalIDs...)
	copy.JournalPayloadBytes = make(map[string]int64, len(capture.JournalPayloadBytes))
	for id, payload := range capture.JournalPayloadBytes {
		copy.JournalPayloadBytes[id] = payload
	}
	copy.Facts = make([]accounting.TaxFactReportingRecord, len(capture.Facts))
	for index, fact := range capture.Facts {
		copy.Facts[index] = basSharedCloneFact(fact)
	}
	return &copy
}

func basSharedCloneFact(fact accounting.TaxFactReportingRecord) accounting.TaxFactReportingRecord {
	copy := fact
	copy.SourceContentHash = bytes.Clone(fact.SourceContentHash)
	if fact.Rule != nil {
		copy.Rule = proto.Clone(fact.Rule).(*tammyv1.SourceRef)
	}
	if fact.Journal != nil {
		copy.Journal = proto.Clone(fact.Journal).(*tammyv1.Journal)
	}
	return copy
}

// Add to source_fact_proofs_test.go after adding the helper and accounting
// TaxFactReportingCapture.Journals field. These are pure unit tests; no SQL is
// involved.

func TestVerifyBASSharedJournalCaptureProofsComparesCommonRawProofs(t *testing.T) {
	t.Run("same opaque legacy journal across all source captures", func(t *testing.T) {
		invoice, purchase, accountingCapture := basSharedJournalCaptures()
		if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation,
			invoice, purchase, accountingCapture); err != nil {
			t.Fatalf("matching journal proofs rejected: %v", err)
		}
	})

	t.Run("same ID with changed raw hash", func(t *testing.T) {
		invoice, purchase, accountingCapture := basSharedJournalCaptures()
		accountingCapture.Journals[0].ContentHash[0] ^= 0xff
		if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation,
			invoice, purchase, accountingCapture); err == nil {
			t.Fatal("same journal ID with different raw content hashes was accepted")
		}
	})

	t.Run("same ID with changed accounting charge", func(t *testing.T) {
		invoice, purchase, accountingCapture := basSharedJournalCaptures()
		accountingCapture.Journals[0].PayloadBytes++
		accountingCapture.JournalPayloadBytes[basSharedJournalOpaqueID]++
		if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation,
			invoice, purchase, accountingCapture); err == nil {
			t.Fatal("same journal ID with different retained charges was accepted")
		}
	})

	t.Run("invoice proof missing from a selected parent", func(t *testing.T) {
		invoice, purchase, accountingCapture := basSharedJournalCaptures()
		invoice.Candidates[0].Posting.Journal = nil
		if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation,
			invoice, purchase, accountingCapture); err == nil {
			t.Fatal("invoice reservation without its complete nested journal proof was accepted")
		}
	})

	t.Run("capture payload map does not describe its proof", func(t *testing.T) {
		invoice, purchase, accountingCapture := basSharedJournalCaptures()
		purchase.JournalPayloadBytes[basSharedJournalOpaqueID]++
		if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation,
			invoice, purchase, accountingCapture); err == nil {
			t.Fatal("purchase proof that disagrees with its reserved charge was accepted")
		}
	})
}

func TestVerifyBASSharedJournalCaptureProofsRejectsDuplicateAndMissingParents(t *testing.T) {
	for _, module := range []string{"purchase", "accounting"} {
		t.Run(module, func(t *testing.T) {
			invoice, purchase, tax := basSharedJournalCaptures()
			if module == "purchase" {
				purchase.Journals = append(purchase.Journals, purchase.Journals[0])
			} else {
				tax.Journals = append(tax.Journals, tax.Journals[0])
			}
			if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation, invoice, purchase, tax); err == nil {
				t.Fatal("duplicate complete parent proof accepted")
			}
		})
	}
	invoice, purchase, tax := basSharedJournalCaptures()
	tax.Journals = nil
	if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation, invoice, purchase, tax); err == nil {
		t.Fatal("missing complete parent proof accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyBASSharedJournalCaptureProofs(ctx, basSharedTestOrganisation, invoice, purchase, tax); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

const basSharedJournalOpaqueID = "legacy-journal/opaque-id"

func basSharedJournalCaptures() (
	*invoices.BASReportingCapture,
	*purchases.PurchaseCoverageReportingCapture,
	*accounting.TaxFactReportingCapture,
) {
	const payloadBytes int64 = 901
	proof := func() *accounting.JournalReportingRecord {
		return &accounting.JournalReportingRecord{
			Journal: &tammyv1.Journal{
				Id: basSharedJournalOpaqueID, OrganisationId: basSharedTestOrganisation,
			},
			ContentHash: bytes.Repeat([]byte{0x59}, 32), PayloadBytes: payloadBytes,
		}
	}

	invoiceJournalID := basSharedJournalOpaqueID
	invoiceCapture := &invoices.BASReportingCapture{
		JournalCount: 1, JournalIDs: []string{basSharedJournalOpaqueID},
		JournalPayloadBytes: map[string]int64{basSharedJournalOpaqueID: payloadBytes},
		Candidates: []invoices.BASInvoiceCandidate{{
			OrganisationID: basSharedTestOrganisation,
			Posting:        &invoices.BASInvoicePosting{JournalID: &invoiceJournalID, Journal: proof()},
		}},
	}
	purchaseCapture := &purchases.PurchaseCoverageReportingCapture{
		OrganisationID: basSharedTestOrganisation,
		JournalCount:   1, JournalIDs: []string{basSharedJournalOpaqueID},
		JournalPayloadBytes: map[string]int64{basSharedJournalOpaqueID: payloadBytes},
		Journals:            []*accounting.JournalReportingRecord{proof()},
	}
	accountingCapture := &accounting.TaxFactReportingCapture{
		JournalCount: 1, JournalIDs: []string{basSharedJournalOpaqueID},
		JournalPayloadBytes: map[string]int64{basSharedJournalOpaqueID: payloadBytes},
		Journals:            []*accounting.JournalReportingRecord{proof()},
	}
	return invoiceCapture, purchaseCapture, accountingCapture
}

func TestVerifyBASSharedJournalCaptureProofsRejectsNegativeJournalCount(t *testing.T) {
	invoice, purchase, tax := basSharedJournalCaptures()
	invoice.JournalCount = -1
	if err := verifyBASSharedJournalCaptureProofs(context.Background(), basSharedTestOrganisation, invoice, purchase, tax); !errors.Is(err, ErrSourceReservation) {
		t.Fatalf("negative parent count: %v", err)
	}
}
