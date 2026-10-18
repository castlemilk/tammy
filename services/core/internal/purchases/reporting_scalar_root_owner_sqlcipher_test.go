//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const purchaseScalarRootOwnerForeignUUID = "018f0000-0000-7000-8000-000000009999"

func assertPurchaseScalarRootOwnerStorage(t *testing.T, tx *sqlcipher.Transaction, table, id, wantType string, wantBytes []byte) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		"SELECT typeof(organisation_id),length(organisation_id),organisation_id FROM "+table+" WHERE id=?", id)
	if err != nil {
		t.Fatalf("inspect %s owner storage: %v", table, err)
	}
	defer rows.Close()
	var storageType string
	var byteLength int64
	var owner []byte
	if !rows.Next() || rows.Scan(&storageType, &byteLength, &owner) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique %s owner storage for %s", table, id)
	}
	if storageType != wantType || byteLength != int64(len(wantBytes)) || !bytes.Equal(owner, wantBytes) {
		t.Fatalf("%s owner storage=(%q,%d,%x), want (%q,%d,%x)", table, storageType, byteLength, owner,
			wantType, len(wantBytes), wantBytes)
	}
}

func assertPurchaseScalarRootOwnerEmpty(t *testing.T, got *purchases.BASScalarSummary, err error) {
	t.Helper()
	want := purchases.BASScalarSummary{OrganisationID: postingOrg}
	if err != nil || got == nil || *got != want {
		t.Fatalf("empty physical scalar reservation=%#v err=%v, want %#v", got, err, want)
	}
}

func assertPurchaseScalarRootOwnerSummaryError(t *testing.T, got *purchases.BASScalarSummary, err error) {
	t.Helper()
	if got != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Errorf("BLOB owner summary=%#v err=%v; want nil summary and repository error", got, err)
	}
}

func assertPurchaseScalarRootOwnerKeysError(t *testing.T, got []purchases.BASScalarKey, err error) {
	t.Helper()
	if got != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Errorf("BLOB owner keys=%#v err=%v; want nil keys and repository error", got, err)
	}
}

func assertPurchaseScalarRootOwnerZeroKeyBound(t *testing.T, got []purchases.BASScalarKey, err error) {
	t.Helper()
	var bound *purchases.SourceBoundError
	if got != nil || !errors.Is(err, purchases.ErrSourceBound) || !errors.As(err, &bound) ||
		bound.Resource != "BAS scalar keys" || bound.Limit != 0 {
		t.Fatalf("zero-key drift result=%#v err=%v bound=%#v, want BAS scalar keys/0", got, err, bound)
	}
}

// This corruption-only helper removes every target TEXT edge from a real
// writer-created zero-total posting graph. The canonical foreign owner UUID is
// used for clearer fixtures; no historical-validity claim is made.
func movePurchaseScalarRootPostingGraphOutOfTextScope(t *testing.T, f *postingFixture, postingID, documentID, initialDocumentID string) {
	t.Helper()
	const foreignOwner = purchaseScalarRootOwnerForeignUUID
	const unrelatedID = "018f0000-0000-7000-8000-000000009998"

	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id=? WHERE id=?`, foreignOwner, documentID)
	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id=? WHERE id=?`, foreignOwner, initialDocumentID)
	mutatePurchaseBASScalar(t, f, "purchase_posting_allocations_v1",
		`UPDATE purchase_posting_allocations_v1 SET organisation_id=?,posting_id=?,account_id=? WHERE posting_id=?`,
		foreignOwner, unrelatedID, unrelatedID, postingID)
	mutatePurchaseBASScalar(t, f, "purchase_tax_evidence_v1",
		`UPDATE purchase_tax_evidence_v1 SET organisation_id=?,posting_id=?,document_id=? WHERE posting_id=?`,
		foreignOwner, unrelatedID, unrelatedID, postingID)
	mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1",
		`UPDATE purchase_accounting_events_v1 SET organisation_id=?,posting_id=?,document_id=?,scope_id=? WHERE posting_id=? OR document_id=?`,
		foreignOwner, unrelatedID, unrelatedID, unrelatedID, postingID, documentID)
	mutatePurchaseBASScalar(t, f, "accounts",
		`UPDATE accounts SET organisation_id=? WHERE id=?`, foreignOwner, postingExpense)
	mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
		`UPDATE purchase_postings_v1 SET organisation_id=? WHERE id=?`, foreignOwner, postingID)
}

func TestPurchaseScalarRootOwnerCanonicalWriterControl(t *testing.T) {
	for _, tc := range []struct {
		name        string
		net         int64
		gst         int64
		wantJournal bool
	}{
		{name: "zero_total_no_journal", net: 0, gst: 0, wantJournal: false},
		{name: "nonzero_with_journal", net: 10000, gst: 1000, wantJournal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			posted := postReportingPurchase(t, f, "ROOT-OWNER-CONTROL", purchaseReportingDate(2026, 8, 10), tc.net, tc.gst)
			if posted == nil || posted.Posting == nil || (posted.Posting.JournalId != nil) != tc.wantJournal {
				t.Fatalf("writer journal presence=%v, want %v", posted != nil && posted.Posting != nil && posted.Posting.JournalId != nil, tc.wantJournal)
			}

			probe := &purchaseBASScalarProbe{Transaction: tx}
			repo := f.repo(probe)
			before := purchaseBASScalarState(t, tx)
			wantRoots := purchaseBASScalarSQLRootCount(t, tx)
			wantFamilies := purchaseBASScalarSQLFamilyCounts(t, tx)
			summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
			if err != nil || summary == nil || summary.RootCount != wantRoots || summary.RootCount != 2 ||
				summary.ReceiptCount != 0 || summary.FamilyCounts != wantFamilies ||
				summary.ScalarKeyCount <= 0 || summary.ScalarBytes <= 0 {
				t.Fatalf("canonical scalar summary=%#v err=%v; SQL roots/families=%d/%v", summary, err, wantRoots, wantFamilies)
			}
			keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
			if err != nil || int64(len(keys)) != summary.ScalarKeyCount ||
				purchaseBASScalarIndependentBytes(keys) != summary.ScalarBytes {
				t.Fatalf("canonical scalar keys=%d independent charge=%d err=%v summary=%#v",
					len(keys), purchaseBASScalarIndependentBytes(keys), err, summary)
			}
			for index := 1; index < len(keys); index++ {
				previous, current := keys[index-1], keys[index]
				if previous.Kind > current.Kind || (previous.Kind == current.Kind && previous.ID > current.ID) {
					t.Fatalf("canonical scalar keys are not sorted by physical kind/id: %#v", keys)
				}
			}
			postingKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPosting, posted.Posting.Id)
			documentKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, f.document.Id)
			if postingKey.OrganisationID != postingOrg || !postingKey.IsRoot || postingKey.IsReceipt ||
				postingKey.DocumentID == nil || *postingKey.DocumentID != f.document.Id ||
				!bytes.Equal(postingKey.SourceHash, posted.Posting.OriginalContentHash) ||
				!bytes.Equal(postingKey.ReviewedSnapshotHash, posted.Posting.ReviewedSnapshotHash) ||
				!bytes.Equal(postingKey.RecordHash, posted.Posting.SourceHash) {
				t.Fatalf("canonical posting key lost root or exact source provenance: %#v writer=%#v", postingKey, posted.Posting)
			}
			purchaseBASScalarRequireUint(t, postingKey.SourceVersion, posted.Posting.ReviewedVersion, "posting reviewed source version")
			if documentKey.OrganisationID != postingOrg || documentKey.IsRoot || documentKey.IsReceipt ||
				!bytes.Equal(documentKey.SourceHash, f.document.Sha256) {
				t.Fatalf("canonical posted document key lost owner/hash/root provenance: %#v", documentKey)
			}
			purchaseBASScalarRequireUint(t, documentKey.ObjectVersion, f.document.Version, "document object version")
			if tc.wantJournal {
				purchaseBASScalarRequireText(t, postingKey.JournalID, *posted.Posting.JournalId, "posting journal id")
				purchaseBASScalarRequireUint(t, postingKey.JournalSourceKind, 6, "posting journal source kind")
				purchaseBASScalarRequireText(t, postingKey.SourceType, "PAYABLE", "posting journal source type")
				assertPurchaseScalarRootOwnerStorage(t, tx, "journals", *posted.Posting.JournalId, "text", []byte(postingOrg))
			} else if postingKey.JournalID != nil || postingKey.JournalSourceKind != nil || postingKey.SourceType != nil {
				t.Fatalf("zero-total posting key retained a journal reference: %#v", postingKey)
			}
			assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "text", []byte(postingOrg))
			assertPurchaseScalarRootOwnerStorage(t, tx, "documents", f.document.Id, "text", []byte(postingOrg))
			if probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() ||
				!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
				t.Fatalf("canonical writer queries=%d/%d/%d, changed state, or ended transaction",
					probe.summaryReads, probe.keyReads, probe.bodyReads)
			}
		})
	}
}

// Mutate the requested direct owner before Summary and Read. The saved empty
// reservation makes both public calls exercise their scalar census while no
// key query or body read can run for the malformed physical owner.
func TestPurchaseScalarRootOwnerStaticPostingBLOBRejectedBeforeSummary(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	initialDocumentID := f.document.Id
	posted := postReportingPurchase(t, f, "ROOT-OWNER-STATIC-POSTING", purchaseReportingDate(2026, 8, 10), 0, 0)
	movePurchaseScalarRootPostingGraphOutOfTextScope(t, f, posted.Posting.Id, f.document.Id, initialDocumentID)
	empty, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, empty, err)
	if empty.FamilyCounts != ([purchases.BASScalarFamilyCount]int64{}) {
		t.Fatalf("pre-corruption posting graph retained a target family: %v", empty.FamilyCounts)
	}
	keys, err := f.repo(tx).ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("pre-corruption empty posting read=%#v err=%v", keys, err)
	}
	mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
		`UPDATE purchase_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, posted.Posting.Id)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "blob", []byte(postingOrg))
	afterCorruption := purchaseBASScalarState(t, tx)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	got, err := repo.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerSummaryError(t, got, err)
	keys, err = repo.ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	assertPurchaseScalarRootOwnerKeysError(t, keys, err)
	if probe.summaryReads != 2 || probe.keyReads != 0 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(afterCorruption, purchaseBASScalarState(t, tx)) {
		t.Fatalf("static posting BLOB queries=%d/%d/%d, state changed, or transaction ended",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
}

func TestPurchaseScalarRootOwnerStaticDocumentBLOBRejectedBeforeSummary(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	documentID := f.document.Id
	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, documentID)
	empty, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, empty, err)
	if empty.FamilyCounts != ([purchases.BASScalarFamilyCount]int64{}) {
		t.Fatalf("pre-corruption document graph retained a target family: %v", empty.FamilyCounts)
	}
	keys, err := f.repo(tx).ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("pre-corruption empty document read=%#v err=%v", keys, err)
	}
	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, documentID)
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents", documentID, "blob", []byte(postingOrg))
	afterCorruption := purchaseBASScalarState(t, tx)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	got, err := repo.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerSummaryError(t, got, err)
	keys, err = repo.ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	assertPurchaseScalarRootOwnerKeysError(t, keys, err)
	if probe.summaryReads != 2 || probe.keyReads != 0 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(afterCorruption, purchaseBASScalarState(t, tx)) {
		t.Fatalf("static document BLOB queries=%d/%d/%d, state changed, or transaction ended",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
}

// Keep a target-owned document and journal beside this malformed posting. The
// unchanged reference arms must still discover and reject the selected row.
func TestPurchaseScalarRootOwnerCanonicalDocumentPostingBLOBRejected(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	posted := postReportingPurchase(t, f, "ROOT-OWNER-REFERENCE-ARM", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	valid, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
	if err != nil || valid == nil {
		t.Fatalf("canonical referenced posting summary=%#v err=%v", valid, err)
	}
	if posted.Posting.JournalId == nil {
		t.Fatal("nonzero canonical posting has no journal")
	}
	mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
		`UPDATE purchase_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, posted.Posting.Id)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "blob", []byte(postingOrg))
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents", f.document.Id, "text", []byte(postingOrg))
	assertPurchaseScalarRootOwnerStorage(t, tx, "journals", *posted.Posting.JournalId, "text", []byte(postingOrg))
	afterCorruption := purchaseBASScalarState(t, tx)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	got, err := repo.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerSummaryError(t, got, err)
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *valid, valid.ScalarKeyCount, valid.ScalarBytes)
	assertPurchaseScalarRootOwnerKeysError(t, keys, err)
	if probe.summaryReads != 2 || probe.keyReads != 0 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(afterCorruption, purchaseBASScalarState(t, tx)) {
		t.Fatalf("reference-arm BLOB queries=%d/%d/%d, state changed, or transaction ended",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
}

// Late-read tests assert 2 summaries, 1 guarded key query, and no body query
// before any optional follow-up Summary call: one saved census, one fresh
// census inside Read, then the injected direct-owner mutation and key query.
func TestPurchaseScalarRootOwnerPostingBlobDriftAfterFreshEmptySummary(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	initialDocumentID := f.document.Id
	posted := postReportingPurchase(t, f, "ROOT-OWNER-POSTING-LATE", purchaseReportingDate(2026, 8, 10), 0, 0)
	movePurchaseScalarRootPostingGraphOutOfTextScope(t, f, posted.Posting.Id, f.document.Id, initialDocumentID)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	empty, err := repo.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, empty, err)
	if empty.FamilyCounts != ([purchases.BASScalarFamilyCount]int64{}) {
		t.Fatalf("pre-drift posting graph retained a target family: %v", empty.FamilyCounts)
	}
	var afterCorruption map[string]any
	probe.beforeKeyRead = func() error {
		mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
			`UPDATE purchase_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, posted.Posting.Id)
		assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "blob", []byte(postingOrg))
		afterCorruption = purchaseBASScalarState(t, tx)
		return nil
	}
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	assertPurchaseScalarRootOwnerZeroKeyBound(t, keys, err)
	if afterCorruption == nil || probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(afterCorruption, purchaseBASScalarState(t, tx)) {
		t.Fatalf("posting drift queries=%d/%d/%d, state changed, or transaction ended",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
}

func TestPurchaseScalarRootOwnerDocumentBlobDriftAfterFreshEmptySummary(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	documentID := f.document.Id
	mutatePurchaseBASScalar(t, f, "documents",
		`UPDATE documents SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, documentID)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	empty, err := repo.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, empty, err)
	if empty.FamilyCounts != ([purchases.BASScalarFamilyCount]int64{}) {
		t.Fatalf("pre-drift document graph retained a target family: %v", empty.FamilyCounts)
	}
	var afterCorruption map[string]any
	probe.beforeKeyRead = func() error {
		mutatePurchaseBASScalar(t, f, "documents",
			`UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, documentID)
		assertPurchaseScalarRootOwnerStorage(t, tx, "documents", documentID, "blob", []byte(postingOrg))
		afterCorruption = purchaseBASScalarState(t, tx)
		return nil
	}
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	assertPurchaseScalarRootOwnerZeroKeyBound(t, keys, err)
	if afterCorruption == nil || probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(afterCorruption, purchaseBASScalarState(t, tx)) {
		t.Fatalf("document drift queries=%d/%d/%d, state changed, or transaction ended",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
}

func TestPurchaseScalarRootOwnerIgnoresUnrelatedBlobUUIDGraph(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	posted := postReportingPurchase(t, f, "ROOT-OWNER-FOREIGN-BLOB", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if posted.Posting.JournalId == nil {
		t.Fatal("nonzero canonical posting has no journal")
	}
	foreignBytes := []byte(purchaseScalarRootOwnerForeignUUID)
	if bytes.Equal(foreignBytes, []byte(postingOrg)) || len(foreignBytes) != 36 {
		t.Fatal("foreign UUID control must be a distinct 36-byte canonical UUID")
	}
	// This is deliberate corruption for scope discrimination, not a claim that
	// the rewritten BLOB-owned graph could exist in valid historical storage.
	for _, mutation := range []struct {
		table, query string
		args         []any
	}{
		{"documents", `UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE organisation_id=?`, []any{purchaseScalarRootOwnerForeignUUID, postingOrg}},
		{"purchase_postings_v1", `UPDATE purchase_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, []any{purchaseScalarRootOwnerForeignUUID, posted.Posting.Id}},
		{"purchase_posting_allocations_v1", `UPDATE purchase_posting_allocations_v1 SET organisation_id=CAST(? AS BLOB) WHERE posting_id=?`, []any{purchaseScalarRootOwnerForeignUUID, posted.Posting.Id}},
		{"purchase_tax_evidence_v1", `UPDATE purchase_tax_evidence_v1 SET organisation_id=CAST(? AS BLOB) WHERE posting_id=?`, []any{purchaseScalarRootOwnerForeignUUID, posted.Posting.Id}},
		{"purchase_accounting_events_v1", `UPDATE purchase_accounting_events_v1 SET organisation_id=CAST(? AS BLOB) WHERE posting_id=?`, []any{purchaseScalarRootOwnerForeignUUID, posted.Posting.Id}},
		{"accounts", `UPDATE accounts SET organisation_id=CAST(? AS BLOB) WHERE id=?`, []any{purchaseScalarRootOwnerForeignUUID, postingExpense}},
		{"journals", `UPDATE journals SET organisation_id=CAST(? AS BLOB) WHERE id=?`, []any{purchaseScalarRootOwnerForeignUUID, *posted.Posting.JournalId}},
	} {
		mutatePurchaseBASScalar(t, f, mutation.table, mutation.query, mutation.args...)
	}
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_postings_v1", posted.Posting.Id, "blob", foreignBytes)
	assertPurchaseScalarRootOwnerStorage(t, tx, "documents", f.document.Id, "blob", foreignBytes)
	assertPurchaseScalarRootOwnerStorage(t, tx, "journals", *posted.Posting.JournalId, "blob", foreignBytes)
	afterCorruption := purchaseBASScalarState(t, tx)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, summary, err)
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
	if err != nil || len(keys) != 0 || probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 {
		t.Fatalf("foreign BLOB graph changed target scalar read: keys=%#v err=%v queries=%d/%d/%d",
			keys, err, probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
	if !tx.Authenticated() || !reflect.DeepEqual(afterCorruption, purchaseBASScalarState(t, tx)) {
		t.Fatal("foreign BLOB control read bodies, changed retained state, or ended caller transaction")
	}
}
