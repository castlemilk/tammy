//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func purchasePostingAllocationOwnerPost(t *testing.T, f *postingFixture, invoice string, net, gst int64, split bool) *tammyv1.PostReviewedPurchaseResponse {
	t.Helper()
	f.date = purchaseReportingDate(2026, 8, 10)
	f.addDocument("Allocation owner supplier", invoice, net, gst)
	if split {
		if net != 10000 || gst != 1000 {
			t.Fatalf("split fixture amounts net/gst=%d/%d; want 10000/1000", net, gst)
		}
		first := proto.Clone(f.request.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
		first.Gross, first.SupplierGst, first.EligibleGst = postMoney(5500), postMoney(500), postMoney(500)
		second := proto.Clone(first).(*tammyv1.PurchaseAllocationInput)
		second.Sequence = 2
		f.request.Allocations = []*tammyv1.PurchaseAllocationInput{first, second}
	}
	posted, err := f.repo(f.executor).PostReviewed(context.Background(), f.request)
	if err != nil || posted == nil || posted.Posting == nil {
		t.Fatalf("real PostReviewed allocation fixture returned posting=%t error=%q", posted != nil && posted.Posting != nil, purchaseRootOwnerErrorPreview(err))
	}
	return posted
}

func purchasePostingAllocationOwnerPrimaryKey(t *testing.T, tx *sqlcipher.Transaction, sequence int64) (string, int64) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		`SELECT posting_id,sequence FROM purchase_posting_allocations_v1 WHERE sequence=? ORDER BY posting_id`, sequence)
	if err != nil {
		t.Fatalf("read allocation composite key: %v", err)
	}
	defer rows.Close()
	var postingID string
	var gotSequence int64
	if !rows.Next() || rows.Scan(&postingID, &gotSequence) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("expected one allocation composite key for sequence %d", sequence)
	}
	if gotSequence != sequence {
		t.Fatalf("allocation sequence=%d, want %d", gotSequence, sequence)
	}
	return postingID, gotSequence
}

func assertPurchasePostingAllocationOwnerStorage(t *testing.T, tx *sqlcipher.Transaction, postingID string, sequence int64, wantType string, wantBytes []byte) {
	t.Helper()
	var storageType string
	var byteLength int64
	var owner []byte
	err := tx.QueryRowContext(context.Background(),
		`SELECT typeof(organisation_id),length(organisation_id),organisation_id
		 FROM purchase_posting_allocations_v1 WHERE posting_id=? AND sequence=?`, postingID, sequence).
		Scan(&storageType, &byteLength, &owner)
	if err != nil || storageType != wantType || byteLength != int64(len(wantBytes)) || !bytes.Equal(owner, wantBytes) {
		t.Fatalf("allocation (%s,%d) owner storage=(%q,%d,%x), want (%q,%d,%x), err=%v",
			postingID, sequence, storageType, byteLength, owner, wantType, len(wantBytes), wantBytes, err)
	}
}

func purchasePostingAllocationOwnerRawRows(t *testing.T, rows []purchases.PurchaseReportingRetainedRow, postingID string) []purchases.PurchaseReportingRetainedRow {
	t.Helper()
	result := make([]purchases.PurchaseReportingRetainedRow, 0, 2)
	for _, row := range rows {
		if row.Table != "purchase_posting_allocations_v1" {
			continue
		}
		key, ok := retainedValue(row, "posting_id")
		if !ok || key.Kind != "TEXT" {
			t.Fatalf("allocation raw row has missing/non-TEXT composite posting key: %#v", row)
		}
		if key.Text == postingID {
			result = append(result, row)
		}
	}
	return result
}

func assertPurchasePostingAllocationOwnerRawRows(t *testing.T, raw []purchases.PurchaseReportingRetainedRow, posting *tammyv1.PurchasePosting) {
	t.Helper()
	allocations := purchasePostingAllocationOwnerRawRows(t, raw, posting.Id)
	if len(allocations) != len(posting.Allocations) {
		t.Fatalf("raw allocations=%d, writer proto allocations=%d", len(allocations), len(posting.Allocations))
	}
	for index, row := range allocations {
		want := posting.Allocations[index]
		owner, ownerOK := retainedValue(row, "organisation_id")
		postingKey, postingOK := retainedValue(row, "posting_id")
		sequence, sequenceOK := retainedValue(row, "sequence")
		accountID, accountOK := retainedValue(row, "account_id")
		accountVersion, versionOK := retainedValue(row, "account_version")
		allocationProto, protoOK := retainedValue(row, "allocation_proto")
		encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if !ownerOK || owner.Kind != "TEXT" || owner.Text != postingOrg ||
			!postingOK || postingKey.Kind != "TEXT" || postingKey.Text != posting.Id ||
			!sequenceOK || sequence.Kind != "INTEGER" || sequence.Integer != int64(index+1) || int64(want.Sequence) != int64(index+1) ||
			!accountOK || accountID.Kind != "TEXT" || accountID.Text != want.AccountId ||
			!versionOK || accountVersion.Kind != "INTEGER" || accountVersion.Integer != int64(want.AccountVersion) ||
			!protoOK || allocationProto.Kind != "BLOB" || !bytes.Equal(allocationProto.Bytes, encoded) {
			t.Fatalf("raw allocation %d lost exact composite owner/account/version/proto provenance: owner=%#v posting=%#v sequence=%#v account=%#v version=%#v proto=%#v",
				index, owner, postingKey, sequence, accountID, accountVersion, allocationProto)
		}
	}
}

func assertPurchasePostingAllocationOwnerScalarKeys(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture, posted *tammyv1.PurchasePosting) {
	t.Helper()
	ctx := context.Background()
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	before := purchaseBASScalarState(t, tx)
	wantRoots := purchaseBASScalarSQLRootCount(t, tx)
	wantFamilies := purchaseBASScalarSQLFamilyCounts(t, tx)
	summary, err := repo.SummarizeBASKeys(ctx, posted.OrganisationId)
	if err != nil || summary == nil || summary.RootCount != wantRoots || summary.RootCount != 2 ||
		summary.ReceiptCount != 0 || summary.FamilyCounts != wantFamilies ||
		summary.FamilyCounts[purchases.BASScalarPostingAllocation-1] != int64(len(posted.Allocations)) ||
		summary.ScalarKeyCount <= 0 || summary.ScalarBytes <= 0 {
		t.Fatalf("canonical scalar allocation summary=%#v err=%q SQL roots/families=%d/%v", summary,
			purchaseRootOwnerErrorPreview(err), wantRoots, wantFamilies)
	}
	keys, err := repo.ReadBASKeys(ctx, posted.OrganisationId, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount || purchaseBASScalarIndependentBytes(keys) != summary.ScalarBytes {
		t.Fatalf("canonical scalar allocation keys=%d independent bytes=%d err=%q summary=%#v",
			len(keys), purchaseBASScalarIndependentBytes(keys), purchaseRootOwnerErrorPreview(err), summary)
	}
	postingKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPosting, posted.Id)
	if !postingKey.IsRoot || postingKey.IsReceipt || postingKey.ID != posted.Id {
		t.Fatalf("canonical posting scalar root flags/key=%#v", postingKey)
	}
	allocations := make([]purchases.BASScalarKey, 0, len(posted.Allocations))
	for _, key := range keys {
		if key.Kind == purchases.BASScalarPostingAllocation && key.ID == posted.Id {
			allocations = append(allocations, key)
		}
	}
	if len(allocations) != len(posted.Allocations) {
		t.Fatalf("scalar composite-key rows=%d, want %d", len(allocations), len(posted.Allocations))
	}
	for index, key := range allocations {
		want := posted.Allocations[index]
		if key.OrganisationID != posted.OrganisationId || key.ID != posted.Id || key.PostingID == nil || *key.PostingID != posted.Id ||
			key.Sequence == nil || *key.Sequence != uint64(index+1) || key.AccountID == nil || *key.AccountID != want.AccountId ||
			key.AccountVersion == nil || *key.AccountVersion != want.AccountVersion || key.IsRoot || key.IsReceipt ||
			len(key.SourceHash) != 0 || len(key.RecordHash) != 0 || len(key.ReviewedSnapshotHash) != 0 {
			t.Fatalf("scalar allocation composite key %d lost exact sequence/account/version/flags: %#v", index, key)
		}
	}
	if probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatalf("canonical allocation scalar queries/state/transaction=%d/%d/%d/%t/%t",
			probe.summaryReads, probe.keyReads, probe.bodyReads, tx.Authenticated(), reflect.DeepEqual(before, purchaseBASScalarState(t, tx)))
	}
}

func TestPurchaseBASPostingAllocationOwnerCanonicalWriterControls(t *testing.T) {
	for _, tc := range []struct {
		name  string
		net   int64
		gst   int64
		split bool
	}{
		{name: "zero_total_no_journal", net: 0, gst: 0},
		{name: "nonzero_two_ordered_allocations", net: 10000, gst: 1000, split: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			initialDocumentID := f.document.Id
			posted := purchasePostingAllocationOwnerPost(t, f, "POSTING-ALLOCATION-OWNER-"+tc.name, tc.net, tc.gst, tc.split)
			wantJournal := tc.net+tc.gst != 0
			if (posted.Posting.JournalId != nil) != wantJournal || posted.Posting.OrganisationId != postingOrg ||
				len(posted.Posting.Allocations) != map[bool]int{true: 2, false: 1}[tc.split] {
				t.Fatalf("real writer journal/owner/allocation count=%t/%q/%d", posted.Posting.JournalId != nil,
					posted.Posting.OrganisationId, len(posted.Posting.Allocations))
			}
			for index := range posted.Posting.Allocations {
				assertPurchasePostingAllocationOwnerStorage(t, tx, posted.Posting.Id, int64(index+1), "text", []byte(postingOrg))
			}
			assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "text", []byte(postingOrg))
			assertPurchaseScalarRootOwnerStorage(t, tx, "documents", posted.Posting.DocumentId, "text", []byte(postingOrg))

			triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			before := purchaseBASScalarState(t, tx)
			assertPurchasePostingAllocationOwnerScalarKeys(t, tx, f, posted.Posting)

			preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
			preflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if err != nil || preflight == nil || preflight.RootCount != 2 || preflightProbe.bodyReads != 0 {
				t.Fatalf("canonical full preflight=%#v err=%q bodyReads=%d; want two roots and no body reads",
					preflight, purchaseRootOwnerErrorPreview(err), preflightProbe.bodyReads)
			}
			coverage, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || coverage == nil || coverage.RootCount != preflight.RootCount || len(coverage.Postings) != 1 ||
				coverage.Postings[0].Posting == nil || !proto.Equal(coverage.Postings[0].Posting, posted.Posting) ||
				coverage.JournalCount != preflight.JournalCount || !reflect.DeepEqual(coverage.JournalIDs, preflight.JournalIDs) ||
				!purchaseRootOwnerJournalPayloadMapsEqual(coverage.JournalPayloadBytes, preflight.JournalPayloadBytes) {
				t.Fatalf("canonical full capture roots/postings/journals err=%q", purchaseRootOwnerErrorPreview(err))
			}
			if coverage.PayloadBytes != preflight.PayloadBytes || coverage.BasePayloadBytes != preflight.BasePayloadBytes ||
				coverage.TaxFactPayloadBytes != preflight.TaxFactPayloadBytes || len(coverage.Fingerprint) != 32 {
				t.Fatalf("full capture/preflight payload/base/tax/fingerprint=%d/%d/%d/%d vs %d/%d/%d",
					coverage.PayloadBytes, coverage.BasePayloadBytes, coverage.TaxFactPayloadBytes, len(coverage.Fingerprint),
					preflight.PayloadBytes, preflight.BasePayloadBytes, preflight.TaxFactPayloadBytes)
			}
			if !wantJournal && (len(coverage.Journals) != 0 || coverage.JournalCount != 0) {
				t.Fatalf("zero-total allocation writer synthesized journals: %d", len(coverage.Journals))
			}
			fullRows := purchasePostingAllocationOwnerRawRows(t, coverage.Rows, posted.Posting.Id)
			assertPurchasePostingAllocationOwnerRawRows(t, coverage.Rows, posted.Posting)
			fullRowCharge := purchaseRootOwnerRowsPayloadBytes(t, coverage.Rows)
			journalCharge := purchaseRootOwnerJournalPayloadBytes(t, coverage.JournalPayloadBytes)
			if coverage.BasePayloadBytes != fullRowCharge || coverage.PayloadBytes != fullRowCharge+journalCharge ||
				preflight.PayloadBytes != fullRowCharge+journalCharge {
				t.Fatalf("independent full charges rows/journals/base/capture/preflight=%d/%d/%d/%d/%d",
					fullRowCharge, journalCharge, coverage.BasePayloadBytes, coverage.PayloadBytes, preflight.PayloadBytes)
			}
			if len(fullRows) != len(posted.Posting.Allocations) {
				t.Fatalf("full raw allocation row count=%d writer count=%d", len(fullRows), len(posted.Posting.Allocations))
			}

			modern, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modern == nil || len(modern.Postings) != 1 || modern.Postings[0].Posting == nil ||
				!proto.Equal(modern.Postings[0].Posting, posted.Posting) || len(modern.Fingerprint) != 32 {
				t.Fatalf("canonical modern capture postings/fingerprint=%d/%d err=%q",
					modernPostingCountForAllocationOwner(modern), modernFingerprintLengthForAllocationOwner(modern), purchaseRootOwnerErrorPreview(err))
			}
			assertPurchasePostingAllocationOwnerRawRows(t, modern.Rows, posted.Posting)
			modernJournals := make(map[string]int64, len(modern.Journals))
			for _, journal := range modern.Journals {
				if journal == nil || journal.Journal == nil {
					t.Fatal("canonical modern capture omitted a journal proof")
				}
				modernJournals[journal.Journal.Id] = journal.PayloadBytes
			}
			modernRowCharge := purchaseRootOwnerRowsPayloadBytes(t, modern.Rows)
			modernJournalCharge := purchaseRootOwnerJournalPayloadBytes(t, modernJournals)
			if !purchaseRootOwnerJournalPayloadMapsEqual(modernJournals, preflight.JournalPayloadBytes) ||
				modern.PayloadBytes != modernRowCharge+modernJournalCharge {
				t.Fatalf("independent modern charges rows/journals/expected/actual=%d/%d/%d/%d journalMapsMatch=%t",
					modernRowCharge, modernJournalCharge, modernRowCharge+modernJournalCharge, modern.PayloadBytes,
					purchaseRootOwnerJournalPayloadMapsEqual(modernJournals, preflight.JournalPayloadBytes))
			}

			coverageAgain, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil || coverageAgain == nil || !reflect.DeepEqual(coverage.Rows, coverageAgain.Rows) ||
				!bytes.Equal(coverage.Fingerprint, coverageAgain.Fingerprint) || coverage.PayloadBytes != coverageAgain.PayloadBytes {
				t.Fatalf("repeated full capture changed rows/charge/fingerprint err=%q rowsEqual=%t fingerprintEqual=%t",
					purchaseRootOwnerErrorPreview(err), coverageAgain != nil && reflect.DeepEqual(coverage.Rows, coverageAgain.Rows),
					coverageAgain != nil && bytes.Equal(coverage.Fingerprint, coverageAgain.Fingerprint))
			}
			modernAgain, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
			if err != nil || modernAgain == nil || !reflect.DeepEqual(modern.Rows, modernAgain.Rows) ||
				!bytes.Equal(modern.Fingerprint, modernAgain.Fingerprint) || modern.PayloadBytes != modernAgain.PayloadBytes {
				t.Fatalf("repeated modern capture changed rows/charge/fingerprint err=%q rowsEqual=%t fingerprintEqual=%t",
					purchaseRootOwnerErrorPreview(err), modernAgain != nil && reflect.DeepEqual(modern.Rows, modernAgain.Rows),
					modernAgain != nil && bytes.Equal(modern.Fingerprint, modernAgain.Fingerprint))
			}
			for index := range posted.Posting.Allocations {
				assertPurchasePostingAllocationOwnerStorage(t, tx, posted.Posting.Id, int64(index+1), "text", []byte(postingOrg))
			}
			assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
			if initialDocumentID == posted.Posting.DocumentId {
				t.Fatal("canonical writer fixture did not retain distinct pre-post and posted document roots")
			}
		})
	}
}

func modernPostingCountForAllocationOwner(capture *purchases.PostingReportingCapture) int {
	if capture == nil {
		return -1
	}
	return len(capture.Postings)
}

func modernFingerprintLengthForAllocationOwner(capture *purchases.PostingReportingCapture) int {
	if capture == nil {
		return -1
	}
	return len(capture.Fingerprint)
}

func TestPurchaseBASPostingAllocationOwnerRejectsSameByteBLOB(t *testing.T) {
	for _, tc := range []struct {
		name       string
		directOnly bool
	}{
		{name: "with_target_posting_and_account_references"},
		{name: "direct_only_after_foreign_graph", directOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			initialDocumentID := f.document.Id
			net, gst := int64(10000), int64(1000)
			if tc.directOnly {
				net, gst = 0, 0
			}
			posted := purchasePostingAllocationOwnerPost(t, f, "POSTING-ALLOCATION-OWNER-BLOB-"+tc.name, net, gst, false)
			if posted.Posting == nil || len(posted.Posting.Allocations) != 1 {
				t.Fatal("real PostReviewed writer returned no single target allocation")
			}
			sequence := int64(posted.Posting.Allocations[0].Sequence)
			postingID := posted.Posting.Id
			expected := purchases.BASScalarSummary{OrganisationID: postingOrg}
			if tc.directOnly {
				movePurchaseScalarRootPostingGraphOutOfTextScope(t, f, posted.Posting.Id, posted.Posting.DocumentId, initialDocumentID)
				postingID, sequence = purchasePostingAllocationOwnerPrimaryKey(t, tx, sequence)
				if postingID == posted.Posting.Id {
					t.Fatal("foreign-graph helper did not move the allocation's posting_id composite-key component")
				}
				var allocationOwner, allocationAccount string
				var parentOwner, documentOwner, expenseOwner sql.NullString
				err := tx.QueryRowContext(ctx, `SELECT a.organisation_id,a.account_id,p.organisation_id,d.organisation_id,account.organisation_id
					FROM purchase_posting_allocations_v1 a
					LEFT JOIN purchase_postings_v1 p ON p.id=a.posting_id
					LEFT JOIN documents d ON d.id=?
					LEFT JOIN accounts account ON account.id=?
					WHERE a.posting_id=? AND a.sequence=?`, posted.Posting.DocumentId, postingExpense, postingID, sequence).
					Scan(&allocationOwner, &allocationAccount, &parentOwner, &documentOwner, &expenseOwner)
				if err != nil || allocationOwner != purchaseScalarRootOwnerForeignUUID || allocationAccount == postingExpense ||
					(parentOwner.Valid && parentOwner.String == postingOrg) ||
					(documentOwner.Valid && documentOwner.String == postingOrg) ||
					(expenseOwner.Valid && expenseOwner.String == postingOrg) {
					t.Fatalf("direct-only allocation references still have target TEXT scope: owner/account/parent/document/expense=%q/%q/%v/%v/%v err=%v",
						allocationOwner, allocationAccount, parentOwner, documentOwner, expenseOwner, err)
				}
				empty, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
				assertPurchaseScalarRootOwnerEmpty(t, empty, err)
				expected = *empty
				if empty.FamilyCounts != ([purchases.BASScalarFamilyCount]int64{}) {
					t.Fatalf("foreign-graph baseline retained target scalar families: %v", empty.FamilyCounts)
				}
			} else {
				valid, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
				if err != nil || valid == nil || valid.FamilyCounts[purchases.BASScalarPostingAllocation-1] != 1 {
					t.Fatalf("canonical target allocation summary=%#v err=%q", valid, purchaseRootOwnerErrorPreview(err))
				}
				expected = *valid
				assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "text", []byte(postingOrg))
				assertPurchaseScalarRootOwnerStorage(t, tx, "accounts", postingExpense, "text", []byte(postingOrg))
			}

			triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			mutatePurchaseBASScalar(t, f, "purchase_posting_allocations_v1",
				`UPDATE purchase_posting_allocations_v1 SET organisation_id=CAST(? AS BLOB) WHERE posting_id=? AND sequence=?`,
				postingOrg, postingID, sequence)
			assertPurchasePostingAllocationOwnerStorage(t, tx, postingID, sequence, "blob", []byte(postingOrg))
			if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) || postScalar(t, tx, `PRAGMA ignore_check_constraints`) != 0 {
				t.Fatal("allocation BLOB fixture did not restore exact triggers/check constraints")
			}
			before := purchaseBASScalarState(t, tx)

			probe := &purchaseBASScalarProbe{Transaction: tx}
			got, err := f.repo(probe).SummarizeBASKeys(ctx, postingOrg)
			assertPurchaseScalarRootOwnerSummaryError(t, got, err)
			keys, keyErr := f.repo(probe).ReadBASKeys(ctx, postingOrg, expected, expected.ScalarKeyCount, expected.ScalarBytes)
			if keys != nil || !errors.Is(keyErr, purchases.ErrRepository) {
				t.Errorf("BLOB owner scalar key replay=%#v err=%q", keys, purchaseRootOwnerErrorPreview(keyErr))
			}
			if probe.summaryReads != 2 || probe.keyReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() ||
				!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
				t.Errorf("BLOB scalar summary/key/body queries=%d/%d/%d; state unchanged=%t authenticated=%t",
					probe.summaryReads, probe.keyReads, probe.bodyReads,
					reflect.DeepEqual(before, purchaseBASScalarState(t, tx)), tx.Authenticated())
			}

			assertPurchaseRootOwnerCoverageRejected(t, tx, f)
			assertPurchaseRootOwnerModernRejected(t, tx, f)
			assertPurchasePostingAllocationOwnerStorage(t, tx, postingID, sequence, "blob", []byte(postingOrg))
			assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
		})
	}
}

func TestPurchaseBASPostingAllocationOwnerUnrelatedBlobIsOutOfScope(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	initialDocumentID := f.document.Id
	posted := purchasePostingAllocationOwnerPost(t, f, "POSTING-ALLOCATION-OWNER-FOREIGN-BLOB", 0, 0, false)
	if posted.Posting == nil || len(posted.Posting.Allocations) != 1 {
		t.Fatal("real writer returned no allocation to move out of scope")
	}
	movePurchaseScalarRootPostingGraphOutOfTextScope(t, f, posted.Posting.Id, posted.Posting.DocumentId, initialDocumentID)
	postingID, sequence := purchasePostingAllocationOwnerPrimaryKey(t, tx, int64(posted.Posting.Allocations[0].Sequence))
	if postingID == posted.Posting.Id {
		t.Fatal("foreign-graph helper did not move the allocation composite key")
	}
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	mutatePurchaseBASScalar(t, f, "purchase_posting_allocations_v1",
		`UPDATE purchase_posting_allocations_v1 SET organisation_id=CAST(? AS BLOB) WHERE posting_id=? AND sequence=?`,
		purchaseScalarRootOwnerForeignUUID, postingID, sequence)
	assertPurchasePostingAllocationOwnerStorage(t, tx, postingID, sequence, "blob", []byte(purchaseScalarRootOwnerForeignUUID))
	if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) || postScalar(t, tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("unrelated allocation BLOB fixture did not restore exact triggers/check constraints")
	}
	before := purchaseBASScalarState(t, tx)

	summary, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, summary, err)
	if summary.FamilyCounts != ([purchases.BASScalarFamilyCount]int64{}) {
		t.Fatalf("unrelated allocation BLOB populated scalar families: %v", summary.FamilyCounts)
	}
	keys, err := f.repo(tx).ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("unrelated allocation BLOB scalar read=%#v err=%q", keys, purchaseRootOwnerErrorPreview(err))
	}
	assertPurchaseRootOwnerEmptyCoverage(t, tx, f)
	assertPurchasePostingAllocationOwnerStorage(t, tx, postingID, sequence, "blob", []byte(purchaseScalarRootOwnerForeignUUID))
	assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
}
