//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func TestPurchaseBASScalarRejectsManualReceiptWithForeignJournalOwner(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	journal := seedLegacyCashPayment(t, f, f.document.Id)

	// Keep the journal's source edge aimed at an owned document while corrupting
	// its retained owner. The row must remain visible long enough to be rejected.
	mutatePurchaseBASScalar(t, f, "journals",
		`UPDATE journals SET organisation_id='foreign-owner' WHERE id=?`, journal.Id)
	before := purchaseBASScalarState(t, tx)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	got, err := f.repo(probe).SummarizeBASKeys(ctx, postingOrg)
	if got != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("foreign-owner manual receipt summary=%#v err=%v; want a storage error", got, err)
	}
	if probe.keyReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("foreign-owner manual journal escaped rejection or changed caller state")
	}
}

func TestPurchaseBASScalarRejectsOwnedDocumentOpeningWithForeignConversionOwner(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	_, conversionID := coverageOpeningWithDocument(t, f, f.document, false)

	// The opening item still points at this owned document, but its conversion
	// no longer belongs to the requested organisation.
	mutatePurchaseBASScalar(t, f, "opening_conversions",
		`UPDATE opening_conversions SET organisation_id='foreign-owner' WHERE id=?`, conversionID)
	before := purchaseBASScalarState(t, tx)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	got, err := f.repo(probe).SummarizeBASKeys(ctx, postingOrg)
	if got != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("foreign-owner opening source summary=%#v err=%v; want a storage error", got, err)
	}
	if probe.keyReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("foreign-owner conversion edge escaped rejection or changed caller state")
	}
}

func TestPurchaseBASScalarRejectsTaxFactOnlyManualReceiptWithForeignFactOwner(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	// The helper accepts (tax-fact document source, journal source). Keep the
	// journal source unrelated so its only purchase lineage is the fact link.
	unrelatedSourceID, _ := f.id()
	journal := seedCoverageCashReceiptWithTaxFactSource(t, f, f.document.Id, unrelatedSourceID)

	mutatePurchaseBASScalar(t, f, "tax_facts",
		`UPDATE tax_facts SET organisation_id='foreign-owner' WHERE journal_line_id IN (SELECT id FROM journal_lines WHERE journal_id=?)`, journal.Id)
	before := purchaseBASScalarState(t, tx)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	got, err := f.repo(probe).SummarizeBASKeys(ctx, postingOrg)
	if got != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("foreign-owner tax-fact receipt summary=%#v err=%v; want a storage error", got, err)
	}
	if probe.keyReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("foreign-owner tax-fact lineage escaped rejection or changed caller state")
	}
}

func TestPurchaseBASScalarIncludesDRAFTOpeningPayableInReplacementContext(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	oldPayableID, oldConversionID := coverageOpeningWithDocument(t, f, f.document, true)
	replacementConversionID := purchaseBASScalarText(t, tx,
		`SELECT replaced_by_id FROM opening_conversions WHERE id=?`, oldConversionID)
	repository := f.repo(tx)
	before, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || before == nil {
		t.Fatalf("summarize actual REPLACED opening baseline: %#v %v", before, err)
	}

	// Attach a second physical payable/item identity to the real replacement
	// DRAFT conversion. The copied proto still names the old payable, so these
	// schema-valid fixture rows are count-only and do not claim financial proof.
	draftPayableID, _ := f.id()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO purchase_opening_payables(id,conversion_id,retained_input_proto,created_at)
		SELECT ?,?,retained_input_proto,created_at
		FROM purchase_opening_payables WHERE id=?`,
		draftPayableID, replacementConversionID, oldPayableID); err != nil {
		t.Fatalf("insert count-only DRAFT opening payable identity: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO opening_items(
			id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,
			source_type,source_id,source_revision,source_content_hash,original_issue_date,
			original_due_date,outstanding_gst_minor,prior_gst_attributed_minor,
			latest_statement_date,latest_statement_balance_minor
		)
		SELECT ?,?,account_id,item_kind,debit_minor,credit_minor,currency_code,
			source_type,source_id,source_revision,source_content_hash,original_issue_date,
			original_due_date,outstanding_gst_minor,prior_gst_attributed_minor,
			latest_statement_date,latest_statement_balance_minor
		FROM opening_items WHERE id=? AND conversion_id=?`,
		draftPayableID, replacementConversionID, oldPayableID, oldConversionID); err != nil {
		t.Fatalf("insert count-only paired DRAFT opening item identity: %v", err)
	}

	wantFamilies := before.FamilyCounts
	wantFamilies[purchases.BASScalarOpeningPayable-1]++
	wantFamilies[purchases.BASScalarOpeningItem-1]++
	after, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || after == nil || after.RootCount != before.RootCount ||
		after.ScalarKeyCount != before.ScalarKeyCount+2 || after.FamilyCounts != wantFamilies {
		t.Fatalf("DRAFT replacement context summary=%#v err=%v; baseline=%#v", after, err, before)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *after, after.ScalarKeyCount, after.ScalarBytes)
	if err != nil || int64(len(keys)) != after.ScalarKeyCount {
		t.Fatalf("DRAFT replacement context keys=%d err=%v", len(keys), err)
	}
	payable := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningPayable, draftPayableID)
	item := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningItem, draftPayableID)
	conversion := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningConversion, replacementConversionID)
	purchaseBASScalarRequireText(t, payable.ConversionID, replacementConversionID, "DRAFT payable conversion id")
	purchaseBASScalarRequireText(t, item.ConversionID, replacementConversionID, "DRAFT item conversion id")
	if payable.IsRoot || item.IsRoot || conversion.IsRoot ||
		payable.IsReceipt || item.IsReceipt || conversion.IsReceipt {
		t.Fatal("replacement DRAFT payable, item, or conversion was marked as a root or receipt")
	}
}
