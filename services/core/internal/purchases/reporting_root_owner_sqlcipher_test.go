//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

const purchaseRootOwnerRetainedRowOverhead = int64(512)

func purchaseRootOwnerErrorPreview(err error) string {
	if err == nil {
		return "<nil>"
	}
	message := err.Error()
	if len(message) > 256 {
		message = message[:256]
	}
	return message
}

func purchaseRootOwnerRowsPayloadBytes(t *testing.T, rows []purchases.PurchaseReportingRetainedRow) int64 {
	t.Helper()
	var total int64
	for _, row := range rows {
		if len(row.Columns) != len(row.Values) {
			t.Fatalf("retained row %s has %d columns and %d values", row.Table, len(row.Columns), len(row.Values))
		}
		total += purchaseRootOwnerRetainedRowOverhead
		for index, value := range row.Values {
			var cellBytes int
			switch value.Kind {
			case "NULL":
			case "INTEGER":
				cellBytes = len(strconv.FormatInt(value.Integer, 10))
			case "TEXT":
				cellBytes = len(value.Text)
			case "BLOB":
				cellBytes = len(value.Bytes)
			default:
				t.Fatalf("retained row %s column %s has unsupported kind %q", row.Table, row.Columns[index], value.Kind)
			}
			total += int64(cellBytes)
		}
	}
	return total
}

func purchaseRootOwnerJournalPayloadBytes(t *testing.T, charges map[string]int64) int64 {
	t.Helper()
	var total int64
	for id, charge := range charges {
		if id == "" || charge <= 0 {
			t.Fatalf("journal %q has invalid reserved payload charge %d", id, charge)
		}
		total += charge
	}
	return total
}

func purchaseRootOwnerJournalPayloadMapsEqual(left, right map[string]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for id, leftCharge := range left {
		if right[id] != leftCharge {
			return false
		}
	}
	return true
}

func purchaseRootOwnerFindDocument(t *testing.T, records []purchases.PostingDocumentReportingRecord, id string) *tammyv1.Document {
	t.Helper()
	for _, record := range records {
		if record.Document != nil && record.Document.Id == id {
			return record.Document
		}
	}
	t.Fatalf("capture omitted document %s", id)
	return nil
}

type purchaseRootOwnerModernDocumentProbe struct {
	app.CommandSQLExecutor
	postingBodyQueries  int
	documentBodyQueries int
}

func (probe *purchaseRootOwnerModernDocumentProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		return rows, err
	}
	if columns, columnsErr := rows.Columns(); columnsErr == nil {
		for _, column := range columns {
			switch strings.ToLower(strings.TrimSpace(column)) {
			case "posting_proto", "reviewed_source_proto", "payment_proto", "allocation_proto", "evidence_proto",
				"claim_proto", "deferral_proto", "attribution_proto", "event_proto", "result_proto":
				probe.postingBodyQueries++
			case "original_bytes", "extracted_text":
				probe.documentBodyQueries++
			}
		}
	}
	return rows, nil
}

func purchaseRootOwnerFixture(t *testing.T) (*sqlcipher.Transaction, *postingFixture) {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tx, newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
}

func assertPurchaseRootOwnerRejected(t *testing.T, resultPresent bool, err error, bodyReads int, api string) {
	t.Helper()
	if resultPresent || !errors.Is(err, purchases.ErrRepository) || bodyReads != 0 {
		t.Errorf("%s returned result=%t err=%q bodyReads=%d; want nil result, repository error, and no body reads",
			api, resultPresent, purchaseRootOwnerErrorPreview(err), bodyReads)
	}
}

func assertPurchaseRootOwnerModernDocumentRejected(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture) {
	t.Helper()
	probe := &purchaseRootOwnerModernDocumentProbe{CommandSQLExecutor: tx}
	capture, err := f.repo(probe).CapturePostingsForReporting(context.Background(), postingOrg)
	if capture != nil || !errors.Is(err, purchases.ErrRepository) || probe.postingBodyQueries == 0 || probe.documentBodyQueries != 0 {
		t.Errorf("modern capture for BLOB-owned posted document returned result=%t err=%q postingBodyQueries=%d documentBodyQueries=%d; want nil result, repository error, and no document-body columns",
			capture != nil, purchaseRootOwnerErrorPreview(err), probe.postingBodyQueries, probe.documentBodyQueries)
	}
}

func assertPurchaseRootOwnerNoMutation(t *testing.T, tx *sqlcipher.Transaction, before map[string]any, triggers map[string]string) {
	t.Helper()
	if !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) ||
		!reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) ||
		postScalar(t, tx, "PRAGMA ignore_check_constraints") != 0 {
		t.Fatal("reporting read changed retained state/total_changes, triggers, constraint mode, or caller transaction")
	}
}

func assertPurchaseRootOwnerCoverageRejected(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture) {
	t.Helper()
	ctx := context.Background()

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	assertPurchaseRootOwnerRejected(t, preflight != nil, err, preflightProbe.bodyReads, "coverage preflight")

	captureProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	capture, err := f.repo(captureProbe).CaptureCoverageForBAS(ctx, postingOrg)
	assertPurchaseRootOwnerRejected(t, capture != nil, err, captureProbe.bodyReads, "coverage capture")
}

func assertPurchaseRootOwnerModernRejected(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture) {
	t.Helper()
	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	capture, err := f.repo(probe).CapturePostingsForReporting(context.Background(), postingOrg)
	assertPurchaseRootOwnerRejected(t, capture != nil, err, probe.bodyReads, "modern posting capture")
}

func assertPurchaseRootOwnerEmptyCoverage(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture) {
	t.Helper()
	ctx := context.Background()
	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("unrelated owner preflight returned nil/error state err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if preflight.RootCount != 0 || preflight.PayloadBytes != 0 ||
		preflight.BasePayloadBytes != 0 || preflight.JournalCount != 0 || len(preflight.JournalIDs) != 0 ||
		len(preflight.JournalPayloadBytes) != 0 || probe.bodyReads != 0 {
		t.Fatalf("unrelated owner preflight root/payload/base/journals=%d/%d/%d/%d bodyReads=%d; want empty scalar reservation",
			preflight.RootCount, preflight.PayloadBytes, preflight.BasePayloadBytes, preflight.JournalCount,
			probe.bodyReads)
	}
	captureProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	capture, err := f.repo(captureProbe).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("unrelated owner coverage returned nil/error state err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if capture.RootCount != 0 || len(capture.Postings) != 0 ||
		len(capture.LegacySources) != 0 || len(capture.Documents) != 0 || len(capture.Journals) != 0 ||
		len(capture.Rows) != 0 || capture.PayloadBytes != 0 || len(capture.Fingerprint) != 32 {
		t.Fatalf("unrelated owner coverage roots/postings/legacy/documents/journals/rows/payload/fingerprint=%d/%d/%d/%d/%d/%d/%d/%d err=%q; want empty capture",
			capture.RootCount, len(capture.Postings), len(capture.LegacySources), len(capture.Documents),
			len(capture.Journals), len(capture.Rows), capture.PayloadBytes, len(capture.Fingerprint),
			purchaseRootOwnerErrorPreview(err))
	}
	modernProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	modern, err := f.repo(modernProbe).CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modern == nil {
		t.Fatalf("unrelated owner modern capture returned nil/error state err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if len(modern.Postings) != 0 || len(modern.Documents) != 0 ||
		len(modern.Journals) != 0 || len(modern.Rows) != 0 || modern.PayloadBytes != 0 || len(modern.Fingerprint) != 32 {
		t.Fatalf("unrelated owner modern roots/documents/journals/rows/payload/fingerprint=%d/%d/%d/%d/%d/%d err=%q; want empty capture",
			len(modern.Postings), len(modern.Documents), len(modern.Journals), len(modern.Rows),
			modern.PayloadBytes, len(modern.Fingerprint), purchaseRootOwnerErrorPreview(err))
	}
}

func TestPurchaseReportingRootOwnerCanonicalWriterControls(t *testing.T) {
	for _, tc := range []struct {
		name string
		net  int64
		gst  int64
	}{
		{name: "zero_total_no_journal", net: 0, gst: 0},
		{name: "nonzero_with_journal", net: 10000, gst: 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			fallbackDocumentID := f.document.Id
			posted := postReportingPurchase(t, f, "ROOT-OWNER-CANONICAL-"+tc.name,
				purchaseReportingDate(2026, 8, 10), tc.net, tc.gst)
			if posted == nil || posted.Posting == nil {
				t.Fatal("real PostReviewed writer returned no purchase posting")
			}
			wantJournal := tc.net+tc.gst != 0
			if (posted.Posting.JournalId != nil) != wantJournal {
				t.Fatalf("writer journal presence=%v, want %v", posted.Posting.JournalId != nil, wantJournal)
			}
			assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "text", []byte(postingOrg))
			assertPurchaseScalarRootOwnerStorage(t, tx, "documents", posted.Posting.DocumentId, "text", []byte(postingOrg))

			triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			before := purchaseBASScalarState(t, tx)
			preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
			preflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
				ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if err != nil || preflight == nil {
				t.Fatalf("canonical preflight returned nil/error state err=%q", purchaseRootOwnerErrorPreview(err))
			}
			if preflight.RootCount != 2 || preflightProbe.bodyReads != 0 {
				t.Fatalf("canonical preflight roots/journals/payload/bodyQueries=%d/%d/%d/%d; want posting plus fallback document and scalar preflight only",
					preflight.RootCount, preflight.JournalCount, preflight.PayloadBytes, preflightProbe.bodyReads)
			}

			coverage, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || coverage == nil {
				t.Fatalf("canonical coverage returned nil/error state err=%q", purchaseRootOwnerErrorPreview(err))
			}
			if coverage.RootCount != preflight.RootCount || len(coverage.Postings) != 1 || coverage.Postings[0].Posting == nil ||
				coverage.PayloadBytes != preflight.PayloadBytes ||
				coverage.BasePayloadBytes != preflight.BasePayloadBytes ||
				coverage.JournalCount != preflight.JournalCount ||
				!reflect.DeepEqual(coverage.JournalIDs, preflight.JournalIDs) ||
				!purchaseRootOwnerJournalPayloadMapsEqual(coverage.JournalPayloadBytes, preflight.JournalPayloadBytes) ||
				coverage.TaxFactPayloadBytes != preflight.TaxFactPayloadBytes {
				t.Fatalf("canonical coverage/preflight root/posting/journal/payload/base/taxfact=%d/%d/%d/%d/%d/%d vs %d/%d/%d/%d/%d/%d",
					coverage.RootCount, len(coverage.Postings), coverage.JournalCount, coverage.PayloadBytes,
					coverage.BasePayloadBytes, coverage.TaxFactPayloadBytes,
					preflight.RootCount, 1, preflight.JournalCount, preflight.PayloadBytes,
					preflight.BasePayloadBytes, preflight.TaxFactPayloadBytes)
			}
			for index := 1; index < len(preflight.JournalIDs); index++ {
				if preflight.JournalIDs[index-1] >= preflight.JournalIDs[index] {
					t.Fatalf("preflight journal IDs are not strictly sorted at index %d", index)
				}
			}
			if len(preflight.JournalPayloadBytes) != preflight.JournalCount {
				t.Fatalf("preflight journal IDs/map/count=%d/%d/%d", len(preflight.JournalIDs), len(preflight.JournalPayloadBytes), preflight.JournalCount)
			}
			journalCharge := purchaseRootOwnerJournalPayloadBytes(t, preflight.JournalPayloadBytes)
			coverageRowCharge := purchaseRootOwnerRowsPayloadBytes(t, coverage.Rows)
			if coverage.BasePayloadBytes != coverageRowCharge ||
				coverage.PayloadBytes != coverageRowCharge+journalCharge ||
				preflight.PayloadBytes != coverageRowCharge+journalCharge {
				t.Fatalf("independent coverage charge rows/journals/expected/base/coverage/preflight=%d/%d/%d/%d/%d/%d",
					coverageRowCharge, journalCharge, coverageRowCharge+journalCharge, coverage.BasePayloadBytes,
					coverage.PayloadBytes, preflight.PayloadBytes)
			}

			fallback := coverageLegacyDocument(t, coverage, fallbackDocumentID)
			if fallback == nil || !fallback.CurrentRoot || fallback.Document == nil || fallback.Document.Id != fallbackDocumentID {
				t.Fatalf("canonical capture lost unposted document fallback: present=%t currentRoot=%t documentPresent=%t",
					fallback != nil, fallback != nil && fallback.CurrentRoot, fallback != nil && fallback.Document != nil)
			}
			postingRow := retainedRowForText(t, coverageRawRows(coverage),
				"purchase_postings_v1", "id", posted.Posting.Id)
			owner, ok := retainedValue(postingRow, "organisation_id")
			if !ok || owner.Kind != "TEXT" || owner.Text != postingOrg {
				t.Fatalf("canonical posting raw owner present/kind/target=%t/%s/%t; want target TEXT",
					ok, owner.Kind, ok && owner.Text == postingOrg)
			}
			for column, want := range map[string][]byte{
				"original_content_hash":  posted.Posting.OriginalContentHash,
				"reviewed_snapshot_hash": posted.Posting.ReviewedSnapshotHash,
				"source_hash":            posted.Posting.SourceHash,
			} {
				value, ok := retainedValue(postingRow, column)
				if !ok || value.Kind != "BLOB" || !bytes.Equal(value.Bytes, want) {
					t.Fatalf("canonical posting %s present/kind/exact=%t/%s/%t; want exact writer provenance",
						column, ok, value.Kind, ok && bytes.Equal(value.Bytes, want))
				}
			}
			documentRow := retainedRowForText(t, coverageRawRows(coverage),
				"documents", "id", posted.Posting.DocumentId)
			documentOwner, documentOwnerOK := retainedValue(documentRow, "organisation_id")
			documentHash, documentHashOK := retainedValue(documentRow, "sha256")
			if !documentOwnerOK || documentOwner.Kind != "TEXT" || documentOwner.Text != postingOrg ||
				!documentHashOK || documentHash.Kind != "BLOB" || !bytes.Equal(documentHash.Bytes, f.document.Sha256) {
				t.Fatalf("canonical document raw owner/hash present=%t/%t kinds=%s/%s exactOwner/hash=%t/%t",
					documentOwnerOK, documentHashOK, documentOwner.Kind, documentHash.Kind,
					documentOwnerOK && documentOwner.Text == postingOrg,
					documentHashOK && bytes.Equal(documentHash.Bytes, f.document.Sha256))
			}
			if wantJournal {
				if len(coverage.Journals) != 1 || posted.Posting.JournalId == nil || len(preflight.JournalIDs) != 1 {
					t.Fatalf("nonzero posting journal count=%d", len(coverage.Journals))
				}
				parent := coverage.Journals[0]
				parentPresent := parent != nil && parent.Journal != nil
				idMatches, sourceType, sourceKind, sourceIDMatches, sourceRevision, chargeMatches := false, "", int64(0), false, uint64(0), false
				if parentPresent {
					idMatches = parent.Journal.Id == *posted.Posting.JournalId
					sourceType, sourceKind = parent.SourceType, parent.SourceKind
					sourceIDMatches, sourceRevision = parent.SourceID == posted.Posting.Id, parent.SourceRevision
					chargeMatches = preflight.JournalPayloadBytes[parent.Journal.Id] == parent.PayloadBytes
				}
				if !parentPresent || !idMatches || sourceType != "PAYABLE" || sourceKind != 6 || !sourceIDMatches ||
					sourceRevision != 1 || !chargeMatches || preflight.JournalIDs[0] != parent.Journal.Id {
					t.Fatalf("nonzero posting parent PAYABLE/6 provenance/charge failed: present=%t idMatches=%t source=%s/%d/%t revision=%d chargeMatches=%t",
						parentPresent, idMatches, sourceType, sourceKind, sourceIDMatches, sourceRevision, chargeMatches)
				}
			} else {
				if len(coverage.Journals) != 0 {
					t.Fatalf("zero-total posting synthesized %d journals", len(coverage.Journals))
				}
				journal, ok := retainedValue(postingRow, "journal_id")
				if !ok || journal.Kind != "NULL" {
					t.Fatalf("zero-total raw journal_id present/kind=%t/%s; want SQL NULL", ok, journal.Kind)
				}
			}

			modern, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modern == nil {
				t.Fatalf("canonical modern capture returned nil/error state err=%q", purchaseRootOwnerErrorPreview(err))
			}
			if len(modern.Postings) != 1 || modern.Postings[0].Posting == nil || len(modern.Documents) != 1 ||
				len(modern.Journals) != boolCount(wantJournal) || modern.PayloadBytes <= 0 ||
				len(modern.Fingerprint) != 32 {
				t.Fatalf("canonical modern capture roots/documents/journals=%d/%d/%d rows=%d payload=%d fingerprint=%d",
					len(modern.Postings), len(modern.Documents), len(modern.Journals), len(modern.Rows),
					modern.PayloadBytes, len(modern.Fingerprint))
			}
			modernJournalPayloads := make(map[string]int64, len(modern.Journals))
			for _, parent := range modern.Journals {
				if parent == nil || parent.Journal == nil {
					t.Fatal("canonical modern capture omitted a journal proof")
				}
				if _, duplicate := modernJournalPayloads[parent.Journal.Id]; duplicate {
					t.Fatalf("canonical modern capture duplicated journal proof %s", parent.Journal.Id)
				}
				modernJournalPayloads[parent.Journal.Id] = parent.PayloadBytes
			}
			modernJournalCharge := purchaseRootOwnerJournalPayloadBytes(t, modernJournalPayloads)
			modernRowCharge := purchaseRootOwnerRowsPayloadBytes(t, modern.Rows)
			if !purchaseRootOwnerJournalPayloadMapsEqual(modernJournalPayloads, preflight.JournalPayloadBytes) ||
				modern.PayloadBytes != modernRowCharge+modernJournalCharge {
				t.Fatalf("independent modern charge rows/journals/expected/actual=%d/%d/%d/%d journalMapsMatch=%t",
					modernRowCharge, modernJournalCharge, modernRowCharge+modernJournalCharge,
					modern.PayloadBytes, purchaseRootOwnerJournalPayloadMapsEqual(modernJournalPayloads, preflight.JournalPayloadBytes))
			}
			if !proto.Equal(coverage.Postings[0].Posting, posted.Posting) ||
				coverage.Postings[0].Posting.Id != posted.Posting.Id ||
				coverage.Postings[0].Posting.DocumentId != posted.Posting.DocumentId ||
				!bytes.Equal(coverage.Postings[0].Posting.OriginalContentHash, posted.Posting.OriginalContentHash) ||
				!bytes.Equal(coverage.Postings[0].Posting.ReviewedSnapshotHash, posted.Posting.ReviewedSnapshotHash) ||
				!bytes.Equal(coverage.Postings[0].Posting.SourceHash, posted.Posting.SourceHash) ||
				!proto.Equal(modern.Postings[0].Posting, posted.Posting) {
				t.Fatal("coverage or modern capture changed the real PostReviewed posting ID, version, or hashes")
			}
			coverageDocument := purchaseRootOwnerFindDocument(t, coverage.Documents, posted.Posting.DocumentId)
			modernDocument := purchaseRootOwnerFindDocument(t, modern.Documents, posted.Posting.DocumentId)
			if !proto.Equal(coverageDocument, f.document) || !proto.Equal(modernDocument, f.document) ||
				coverageDocument.Id != f.document.Id || modernDocument.Id != f.document.Id ||
				coverageDocument.Version != f.document.Version || modernDocument.Version != f.document.Version ||
				!bytes.Equal(coverageDocument.Sha256, f.document.Sha256) || !bytes.Equal(modernDocument.Sha256, f.document.Sha256) {
				t.Fatal("coverage or modern capture changed the real reviewed document ID, version, or hash")
			}

			coverageAgain, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || coverageAgain == nil ||
				!reflect.DeepEqual(coverage.Rows, coverageAgain.Rows) || !bytes.Equal(coverage.Fingerprint, coverageAgain.Fingerprint) {
				t.Fatalf("repeated coverage changed ordered rows/fingerprint err=%q rowsEqual=%t fingerprintEqual=%t",
					purchaseRootOwnerErrorPreview(err), coverageAgain != nil && reflect.DeepEqual(coverage.Rows, coverageAgain.Rows),
					coverageAgain != nil && bytes.Equal(coverage.Fingerprint, coverageAgain.Fingerprint))
			}
			modernAgain, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modernAgain == nil ||
				!reflect.DeepEqual(modern.Rows, modernAgain.Rows) || !bytes.Equal(modern.Fingerprint, modernAgain.Fingerprint) {
				t.Fatalf("repeated modern capture changed ordered rows/fingerprint err=%q rowsEqual=%t fingerprintEqual=%t",
					purchaseRootOwnerErrorPreview(err), modernAgain != nil && reflect.DeepEqual(modern.Rows, modernAgain.Rows),
					modernAgain != nil && bytes.Equal(modern.Fingerprint, modernAgain.Fingerprint))
			}
			assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "text", []byte(postingOrg))
			assertPurchaseScalarRootOwnerStorage(t, tx, "documents", posted.Posting.DocumentId, "text", []byte(postingOrg))
			assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestPurchaseReportingRootOwnerRejectsSameByteBlobPostingOwner(t *testing.T) {
	for _, tc := range []struct {
		name     string
		net, gst int64
		isolate  bool
	}{
		{name: "target_document_and_references_remain", net: 10000, gst: 1000},
		{name: "direct_only_owner_after_foreign_graph", net: 0, gst: 0, isolate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			initialDocumentID := f.document.Id
			posted := postReportingPurchase(t, f, "ROOT-OWNER-BLOB-POSTING",
				purchaseReportingDate(2026, 8, 10), tc.net, tc.gst)
			if posted == nil || posted.Posting == nil {
				t.Fatal("real writer returned no posting")
			}
			triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			if tc.isolate {
				movePurchaseScalarRootPostingGraphOutOfTextScope(t, f,
					posted.Posting.Id, posted.Posting.DocumentId, initialDocumentID)
			}
			mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
				"UPDATE purchase_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?",
				postingOrg, posted.Posting.Id)
			assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1",
				posted.Posting.Id, "blob", []byte(postingOrg))
			if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) {
				t.Fatal("fixture did not restore exact trigger definitions")
			}
			before := purchaseBASScalarState(t, tx)

			assertPurchaseRootOwnerCoverageRejected(t, tx, f)
			assertPurchaseRootOwnerModernRejected(t, tx, f)
			assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1",
				posted.Posting.Id, "blob", []byte(postingOrg))
			assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
		})
	}
}

func TestPurchaseReportingRootOwnerRejectsSameByteBlobUnpostedDocument(t *testing.T) {
	for _, tc := range []struct {
		name, owner string
		wantReject  bool
	}{
		{name: "requested_owner", owner: postingOrg, wantReject: true},
		{name: "unrelated_foreign_blob", owner: purchaseScalarRootOwnerForeignUUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			documentID := f.document.Id
			triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			mutatePurchaseBASScalar(t, f, "documents",
				"UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?",
				tc.owner, documentID)
			assertPurchaseScalarRootOwnerStorage(t, tx, "documents", documentID, "blob", []byte(tc.owner))
			if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) {
				t.Fatal("fixture did not restore exact trigger definitions")
			}
			before := purchaseBASScalarState(t, tx)

			if tc.wantReject {
				assertPurchaseRootOwnerCoverageRejected(t, tx, f)
			} else {
				assertPurchaseRootOwnerEmptyCoverage(t, tx, f)
			}
			// This modern API's root set is purchase postings. An unposted
			// document, including a target-owner BLOB, is not a modern root.
			modernProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
			modern, err := f.repo(modernProbe).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modern == nil || len(modern.Postings) != 0 ||
				len(modern.Documents) != 0 || len(modern.Rows) != 0 || modern.PayloadBytes != 0 || len(modern.Fingerprint) != 32 {
				t.Fatalf("unposted document changed modern capture roots/documents/rows/payload/fingerprint=%d/%d/%d/%d/%d err=%q",
					len(modern.Postings), len(modern.Documents), len(modern.Rows), modern.PayloadBytes,
					len(modern.Fingerprint), purchaseRootOwnerErrorPreview(err))
			}
			assertPurchaseScalarRootOwnerStorage(t, tx, "documents", documentID, "blob", []byte(tc.owner))
			assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
		})
	}
}

func TestPurchaseReportingRootOwnerRejectsSameByteBlobPostedDocument(t *testing.T) {
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	posted := postReportingPurchase(t, f, "ROOT-OWNER-BLOB-DOCUMENT",
		purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if posted == nil || posted.Posting == nil {
		t.Fatal("real writer returned no posting")
	}
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	mutatePurchaseBASScalar(t, f, "documents",
		"UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?",
		postingOrg, posted.Posting.DocumentId)
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents",
		posted.Posting.DocumentId, "blob", []byte(postingOrg))
	if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) {
		t.Fatal("fixture did not restore exact trigger definitions")
	}
	before := purchaseBASScalarState(t, tx)

	assertPurchaseRootOwnerCoverageRejected(t, tx, f)
	assertPurchaseRootOwnerModernDocumentRejected(t, tx, f)
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents",
		posted.Posting.DocumentId, "blob", []byte(postingOrg))
	assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
}

func TestPurchaseReportingRootOwnerIgnoresTargetBlobUnpostedDocumentInModernCapture(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	unpostedDocumentID := f.document.Id
	posted := postReportingPurchase(t, f, "ROOT-OWNER-TARGET-UNPOSTED-BLOB",
		purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if posted == nil || posted.Posting == nil {
		t.Fatal("real writer returned no posting")
	}
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	mutatePurchaseBASScalar(t, f, "documents",
		"UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?",
		postingOrg, unpostedDocumentID)
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents", unpostedDocumentID, "blob", []byte(postingOrg))
	if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) {
		t.Fatal("fixture did not restore exact trigger definitions")
	}
	before := purchaseBASScalarState(t, tx)

	// Whole-organisation coverage owns the document root and must reject it.
	// Modern reporting owns only IDs discovered from posting rows, so the
	// unposted BLOB document remains outside that batch and the valid posting
	// continues to capture.
	assertPurchaseRootOwnerCoverageRejected(t, tx, f)
	modernProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	modern, err := f.repo(modernProbe).CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modern == nil {
		t.Fatalf("modern capture for out-of-batch target BLOB document returned nil/error state err=%q",
			purchaseRootOwnerErrorPreview(err))
	}
	if len(modern.Postings) != 1 || len(modern.Documents) != 1 || len(modern.Rows) == 0 ||
		modern.Postings[0].Posting == nil || modern.Postings[0].Posting.Id != posted.Posting.Id ||
		modern.Documents[0].Document == nil || modern.Documents[0].Document.Id != posted.Posting.DocumentId ||
		!proto.Equal(modern.Postings[0].Posting, posted.Posting) || !proto.Equal(modern.Documents[0].Document, f.document) {
		t.Fatalf("modern capture with unposted target BLOB rows/postings/documents=%d/%d/%d targetPosting=%t targetDocument=%t",
			len(modern.Rows), len(modern.Postings), len(modern.Documents),
			len(modern.Postings) == 1 && modern.Postings[0].Posting.Id == posted.Posting.Id,
			len(modern.Documents) == 1 && modern.Documents[0].Document.Id == posted.Posting.DocumentId)
	}
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents", unpostedDocumentID, "blob", []byte(postingOrg))
	assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
}

func TestPurchaseReportingRootOwnerLeavesUnrelatedForeignBlobPostingGraphOutOfScope(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	initialDocumentID := f.document.Id
	target := postReportingPurchase(t, f, "ROOT-OWNER-TARGET-FOREIGN-BLOB-GRAPH",
		purchaseReportingDate(2026, 8, 10), 10000, 1000)
	foreign := postReportingPurchase(t, f, "ROOT-OWNER-FOREIGN-BLOB-GRAPH",
		purchaseReportingDate(2026, 8, 11), 0, 0)
	if target == nil || target.Posting == nil || foreign == nil || foreign.Posting == nil {
		t.Fatal("real writers returned no target or foreign posting")
	}
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	movePurchaseScalarRootPostingGraphOutOfTextScope(t, f,
		foreign.Posting.Id, foreign.Posting.DocumentId, initialDocumentID)
	mutatePurchaseBASScalar(t, f, "accounts",
		"UPDATE accounts SET organisation_id=? WHERE id=?", postingOrg, postingExpense)
	mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
		"UPDATE purchase_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?",
		purchaseScalarRootOwnerForeignUUID, foreign.Posting.Id)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1",
		foreign.Posting.Id, "blob", []byte(purchaseScalarRootOwnerForeignUUID))
	if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) {
		t.Fatal("fixture did not restore exact trigger definitions")
	}
	before := purchaseBASScalarState(t, tx)

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("foreign BLOB graph changed target preflight to nil/error err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if preflight.RootCount != 1 || preflightProbe.bodyReads != 0 {
		t.Fatalf("foreign BLOB graph changed target preflight roots/bodyQueries=%d/%d", preflight.RootCount, preflightProbe.bodyReads)
	}
	coverage, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || coverage == nil {
		t.Fatalf("foreign BLOB graph changed target coverage to nil/error err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if coverage.RootCount != 1 || len(coverage.Postings) != 1 || coverage.Postings[0].Posting == nil || coverage.Postings[0].Posting.Id != target.Posting.Id {
		t.Fatalf("foreign BLOB graph changed target coverage roots/postings/target=%d/%d/%t",
			coverage.RootCount, len(coverage.Postings), len(coverage.Postings) == 1 && coverage.Postings[0].Posting.Id == target.Posting.Id)
	}
	modern, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modern == nil {
		t.Fatalf("foreign BLOB graph changed modern capture to nil/error err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if len(modern.Postings) != 1 || modern.Postings[0].Posting == nil || modern.Postings[0].Posting.Id != target.Posting.Id {
		t.Fatalf("foreign BLOB graph changed modern postings/target=%d/%t", len(modern.Postings),
			len(modern.Postings) == 1 && modern.Postings[0].Posting.Id == target.Posting.Id)
	}
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1",
		foreign.Posting.Id, "blob", []byte(purchaseScalarRootOwnerForeignUUID))
	assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
}

func TestPurchaseReportingRootOwnerLeavesUnrelatedDocumentBlobOutOfScope(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	unpostedDocumentID := f.document.Id
	posted := postReportingPurchase(t, f, "ROOT-OWNER-FOREIGN-DOCUMENT-BLOB",
		purchaseReportingDate(2026, 8, 10), 10000, 1000)
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	mutatePurchaseBASScalar(t, f, "documents",
		"UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?",
		purchaseScalarRootOwnerForeignUUID, unpostedDocumentID)
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents",
		unpostedDocumentID, "blob", []byte(purchaseScalarRootOwnerForeignUUID))
	before := purchaseBASScalarState(t, tx)

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil {
		t.Fatalf("foreign document BLOB changed target preflight to nil/error err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if preflight.RootCount != 1 || preflightProbe.bodyReads != 0 {
		t.Fatalf("foreign document BLOB changed target preflight roots/bodyQueries=%d/%d",
			preflight.RootCount, preflightProbe.bodyReads)
	}
	coverageProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	coverage, err := f.repo(coverageProbe).CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || coverage == nil {
		t.Fatalf("foreign document BLOB changed target coverage to nil/error err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if coverage.RootCount != 1 || len(coverage.Postings) != 1 || coverage.Postings[0].Posting == nil || coverage.Postings[0].Posting.Id != posted.Posting.Id ||
		coverageProbe.bodyReads == 0 {
		t.Fatalf("foreign document BLOB changed target coverage roots/postings/bodyQueries=%d/%d/%d",
			coverage.RootCount, len(coverage.Postings), coverageProbe.bodyReads)
	}
	modernProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	modern, err := f.repo(modernProbe).CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modern == nil {
		t.Fatalf("foreign document BLOB changed target modern capture to nil/error err=%q", purchaseRootOwnerErrorPreview(err))
	}
	if len(modern.Postings) != 1 || modern.Postings[0].Posting == nil || modern.Postings[0].Posting.Id != posted.Posting.Id || modernProbe.bodyReads == 0 {
		t.Fatalf("foreign document BLOB changed target modern postings/bodyQueries=%d/%d", len(modern.Postings), modernProbe.bodyReads)
	}
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents",
		unpostedDocumentID, "blob", []byte(purchaseScalarRootOwnerForeignUUID))
	assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
}
