//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"sort"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func purchasePaymentOwnerFixture(t *testing.T) (*sqlcipher.Transaction, *postingFixture, *tammyv1.PostReviewedPurchaseResponse, *tammyv1.RecordPurchasePaymentResponse, string) {
	t.Helper()
	tx, f := purchaseRootOwnerFixture(t)
	initialDocumentID := f.document.Id
	posted := postReportingPurchase(t, f, "PAYMENT-OWNER-CANONICAL", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if posted == nil || posted.Posting == nil || posted.Accounting == nil || posted.Posting.JournalId == nil {
		t.Fatal("real PostReviewed writer returned no journal-backed purchase posting/accounting")
	}
	payment, err := f.repo(tx).RecordPayment(context.Background(),
		paymentRequest(t, f, posted.Accounting.AccountingVersion, 5500, paymentDate(8, 20)))
	if err != nil || payment == nil || payment.Payment == nil || payment.Journal == nil || payment.Accounting == nil {
		t.Fatalf("real RecordPayment writer returned incomplete result: present=%t err=%q",
			payment != nil, purchaseRootOwnerErrorPreview(err))
	}
	return tx, f, posted, payment, initialDocumentID
}

func purchasePaymentOwnerAssertRawPayment(t *testing.T, capture *purchases.PostingReportingCapture, payment *tammyv1.PurchasePayment) {
	t.Helper()
	row := retainedRowForText(t, capture, "purchase_payments_v1", "id", payment.Id)
	owner, ownerOK := retainedValue(row, "organisation_id")
	posting, postingOK := retainedValue(row, "posting_id")
	version, versionOK := retainedValue(row, "accounting_version")
	recordHash, hashOK := retainedValue(row, "source_hash")
	protoBytes, protoOK := retainedValue(row, "payment_proto")
	wantProto := mustCoverageMarshal(t, payment)
	copy := proto.Clone(payment).(*tammyv1.PurchasePayment)
	copy.SourceHash = nil
	wantHash := coverageMessageHash(t, copy)
	if !ownerOK || owner.Kind != "TEXT" || owner.Text != payment.OrganisationId ||
		!postingOK || posting.Kind != "TEXT" || posting.Text != payment.PostingId ||
		!versionOK || version.Kind != "INTEGER" || version.Integer != int64(payment.AccountingVersion) ||
		!hashOK || recordHash.Kind != "BLOB" || !bytes.Equal(recordHash.Bytes, payment.SourceHash) ||
		!protoOK || protoBytes.Kind != "BLOB" || !bytes.Equal(protoBytes.Bytes, wantProto) ||
		!bytes.Equal(recordHash.Bytes, wantHash) {
		t.Fatalf("captured payment row lost owner/identity/version/hash provenance: owner=%t/%s posting=%t/%s version=%t/%s hash=%t/%s proto=%t/%s",
			ownerOK, owner.Kind, postingOK, posting.Kind, versionOK, version.Kind,
			hashOK, recordHash.Kind, protoOK, protoBytes.Kind)
	}
}

func purchasePaymentOwnerAssertScalarRejected(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture, expected purchases.BASScalarSummary) {
	t.Helper()
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	summary, summaryErr := repo.SummarizeBASKeys(context.Background(), postingOrg)
	keys, keysErr := repo.ReadBASKeys(context.Background(), postingOrg, expected, expected.ScalarKeyCount, expected.ScalarBytes)
	if summary != nil || !errors.Is(summaryErr, purchases.ErrRepository) {
		t.Errorf("same-byte payment BLOB summary present=%t err=%q; want nil and repository error",
			summary != nil, purchaseRootOwnerErrorPreview(summaryErr))
	}
	if keys != nil || !errors.Is(keysErr, purchases.ErrRepository) {
		t.Errorf("same-byte payment BLOB keys present=%t err=%q; want nil and repository error",
			keys != nil, purchaseRootOwnerErrorPreview(keysErr))
	}
	if probe.summaryReads != 2 || probe.keyReads != 0 || probe.bodyReads != 0 {
		t.Errorf("same-byte payment BLOB scalar queries summary/key/body=%d/%d/%d; want two fresh summaries and no key/body query",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
}

func purchasePaymentOwnerAssertAllocationOwner(t *testing.T, tx *sqlcipher.Transaction, wantType string, want []byte) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		`SELECT typeof(organisation_id),length(organisation_id),organisation_id
		 FROM purchase_payment_allocations_v1 WHERE typeof(organisation_id)=? AND organisation_id=? ORDER BY sequence`,
		wantType, want)
	if err != nil {
		t.Fatalf("inspect payment allocation owner: %v", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatal("missing payment allocation owner row")
	}
	var storageType string
	var byteLength int64
	var owner []byte
	if err := rows.Scan(&storageType, &byteLength, &owner); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatal("unexpected multiple payment allocation owner rows")
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if storageType != wantType || byteLength != int64(len(want)) || !bytes.Equal(owner, want) {
		t.Fatalf("payment allocation owner type/length/exact=%q/%d/%t; want %q/%d/exact",
			storageType, byteLength, bytes.Equal(owner, want), wantType, len(want))
	}
}

func purchasePaymentOwnerPreflightRoots(preflight *purchases.PurchaseCoverageReportingPreflight) int {
	if preflight == nil {
		return 0
	}
	return preflight.RootCount
}

func purchasePaymentOwnerPreflightReceipts(preflight *purchases.PurchaseCoverageReportingPreflight) int {
	if preflight == nil {
		return 0
	}
	return preflight.ReceiptCount
}

func purchasePaymentOwnerAssertPublicRejected(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture) {
	t.Helper()
	ctx := context.Background()

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if preflight != nil || !errors.Is(err, purchases.ErrRepository) || preflightProbe.bodyReads != 0 {
		t.Errorf("same-byte payment BLOB preflight present=%t err=%q bodyReads=%d; want nil, repository error, and no body reads",
			preflight != nil, purchaseRootOwnerErrorPreview(err), preflightProbe.bodyReads)
	}

	coverageProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	coverage, coverageErr := f.repo(coverageProbe).CaptureCoverageForBAS(ctx, postingOrg)
	if coverage != nil || !errors.Is(coverageErr, purchases.ErrRepository) || coverageProbe.bodyReads != 0 {
		t.Errorf("same-byte payment BLOB full capture present=%t err=%q bodyReads=%d; want nil, repository error, and no body reads",
			coverage != nil, purchaseRootOwnerErrorPreview(coverageErr), coverageProbe.bodyReads)
	}

	modernProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	modern, modernErr := f.repo(modernProbe).CapturePostingsForReporting(ctx, postingOrg)
	if modern != nil || !errors.Is(modernErr, purchases.ErrRepository) || modernProbe.bodyReads != 0 {
		t.Errorf("same-byte payment BLOB modern capture present=%t err=%q bodyReads=%d; want nil, repository error, and no body reads",
			modern != nil, purchaseRootOwnerErrorPreview(modernErr), modernProbe.bodyReads)
	}
}

func purchasePaymentOwnerMoveGraphOutOfScope(t *testing.T, f *postingFixture, posted *tammyv1.PostReviewedPurchaseResponse, payment *tammyv1.RecordPurchasePaymentResponse, initialDocumentID string) {
	t.Helper()
	const (
		foreignOwner = purchaseScalarRootOwnerForeignUUID
		unrelatedID  = "018f0000-0000-7000-8000-000000009998"
	)
	movePurchaseScalarRootPostingGraphOutOfTextScope(t, f,
		posted.Posting.Id, posted.Posting.DocumentId, initialDocumentID)

	mutatePurchaseBASScalar(t, f, "accounts",
		`UPDATE accounts SET organisation_id=? WHERE id=?`, foreignOwner, payment.Payment.BankAccountId)
	mutatePurchaseBASScalar(t, f, "journals",
		`UPDATE journals SET organisation_id=?,source_id=? WHERE id IN (?,?)`,
		foreignOwner, unrelatedID, *posted.Posting.JournalId, payment.Payment.JournalId)
	mutatePurchaseBASScalar(t, f, "purchase_payment_allocations_v1",
		`UPDATE purchase_payment_allocations_v1 SET organisation_id=CAST(? AS BLOB),payment_id=?,posting_id=? WHERE payment_id=?`,
		foreignOwner, unrelatedID, unrelatedID, payment.Payment.Id)
	mutatePurchaseBASScalar(t, f, "purchase_payments_v1",
		`UPDATE purchase_payments_v1 SET organisation_id=CAST(? AS BLOB),posting_id=?,bank_account_id=?,journal_id=? WHERE id=?`,
		foreignOwner, unrelatedID, unrelatedID, unrelatedID, payment.Payment.Id)
}

func TestPurchaseReportingPaymentOwnerCanonicalWriterControls(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	posted := postReportingPurchase(t, f, "PAYMENT-OWNER-CANONICAL-TWO", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if posted == nil || posted.Posting == nil || posted.Accounting == nil || posted.Posting.JournalId == nil {
		t.Fatal("real PostReviewed writer returned no nonzero purchase posting")
	}
	repo := f.repo(tx)
	first, err := repo.RecordPayment(ctx, paymentRequest(t, f, posted.Accounting.AccountingVersion, 5500, paymentDate(8, 20)))
	if err != nil || first == nil || first.Payment == nil || first.Journal == nil || first.Accounting == nil {
		t.Fatalf("first real RecordPayment writer returned incomplete result: present=%t err=%q",
			first != nil, purchaseRootOwnerErrorPreview(err))
	}
	second, err := repo.RecordPayment(ctx, paymentRequest(t, f, first.Accounting.AccountingVersion, 5500, paymentDate(8, 21)))
	if err != nil || second == nil || second.Payment == nil || second.Journal == nil || second.Accounting == nil {
		t.Fatalf("second real RecordPayment writer returned incomplete result: present=%t err=%q",
			second != nil, purchaseRootOwnerErrorPreview(err))
	}
	payments := []*tammyv1.PurchasePayment{first.Payment, second.Payment}
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	before := purchaseBASScalarState(t, tx)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	summary, err := f.repo(probe).SummarizeBASKeys(ctx, postingOrg)
	wantFamilies := purchaseBASScalarSQLFamilyCounts(t, tx)
	wantRoots := purchaseBASScalarSQLRootCount(t, tx)
	if err != nil || summary == nil || summary.RootCount != wantRoots || summary.RootCount != 2 ||
		summary.ReceiptCount != 2 || summary.FamilyCounts != wantFamilies ||
		summary.FamilyCounts[purchases.BASScalarPayment-1] != 2 ||
		summary.FamilyCounts[purchases.BASScalarPaymentAllocation-1] != 2 ||
		summary.ScalarKeyCount <= 0 || summary.ScalarBytes <= 0 {
		t.Fatalf("canonical payment scalar summary roots/receipts/families/err=%d/%d/%v/%q; SQL roots/families=%d/%v",
			valueOrZero(summary, func(v *purchases.BASScalarSummary) int64 { return v.RootCount }),
			valueOrZero(summary, func(v *purchases.BASScalarSummary) int64 { return v.ReceiptCount }),
			wantFamilies, purchaseRootOwnerErrorPreview(err), wantRoots, wantFamilies)
	}
	keys, err := f.repo(probe).ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount || purchaseBASScalarIndependentBytes(keys) != summary.ScalarBytes {
		t.Fatalf("canonical payment keys=%d independentBytes=%d err=%q summaryKeys/bytes=%d/%d",
			len(keys), purchaseBASScalarIndependentBytes(keys), purchaseRootOwnerErrorPreview(err),
			summary.ScalarKeyCount, summary.ScalarBytes)
	}
	for index := 1; index < len(keys); index++ {
		previous, current := keys[index-1], keys[index]
		if previous.Kind > current.Kind || (previous.Kind == current.Kind && previous.ID > current.ID) {
			t.Fatalf("canonical payment scalar keys are not sorted at index %d", index)
		}
	}
	for _, payment := range payments {
		key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPayment, payment.Id)
		if key.OrganisationID != postingOrg || key.IsRoot || !key.IsReceipt ||
			!bytes.Equal(key.RecordHash, payment.SourceHash) {
			t.Fatalf("payment scalar identity/root/receipt/hash=%t/%t/%t/%t; want writer-owned nonroot receipt and exact source hash",
				key.OrganisationID == postingOrg, key.IsRoot, key.IsReceipt, bytes.Equal(key.RecordHash, payment.SourceHash))
		}
		purchaseBASScalarRequireText(t, key.PostingID, posted.Posting.Id, "payment posting ID")
		purchaseBASScalarRequireText(t, key.AccountID, payment.BankAccountId, "payment bank account ID")
		purchaseBASScalarRequireText(t, key.JournalID, payment.JournalId, "payment journal ID")
		purchaseBASScalarRequireText(t, key.SourceType, "BANKING", "payment journal source type")
		purchaseBASScalarRequireUint(t, key.JournalSourceKind, 7, "payment journal source kind")
		purchaseBASScalarRequireUint(t, key.AccountingVersion, payment.AccountingVersion, "payment accounting version")
		allocation := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPaymentAllocation, payment.Id)
		if allocation.IsRoot || allocation.IsReceipt || allocation.PaymentID == nil || *allocation.PaymentID != payment.Id ||
			allocation.PostingID == nil || *allocation.PostingID != posted.Posting.Id {
			t.Fatalf("payment allocation identity/root/receipt flags=%t/%t/%t; want payment child, not receipt/root",
				allocation.PaymentID != nil && *allocation.PaymentID == payment.Id,
				allocation.IsRoot, allocation.IsReceipt)
		}
	}
	if probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 {
		t.Fatalf("canonical payment scalar queries summary/key/body=%d/%d/%d; want two summaries, one typed-key read, no bodies",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || preflight == nil || preflight.RootCount != int(summary.RootCount) || preflight.ReceiptCount != int(summary.ReceiptCount) || preflightProbe.bodyReads != 0 {
		t.Fatalf("canonical public preflight roots/receipts/bodyReads=%d/%d/%d err=%q",
			purchasePaymentOwnerPreflightRoots(preflight), purchasePaymentOwnerPreflightReceipts(preflight),
			preflightProbe.bodyReads, purchaseRootOwnerErrorPreview(err))
	}
	coverage := captureCoverage(t, f.repo(tx), ctx)
	modern, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modern == nil {
		t.Fatalf("canonical modern capture present=%t err=%q", modern != nil, purchaseRootOwnerErrorPreview(err))
	}
	coveragePaymentCount, modernPaymentCount := 0, 0
	if len(coverage.Postings) != 0 {
		coveragePaymentCount = len(coverage.Postings[0].Payments)
	}
	if len(modern.Postings) != 0 {
		modernPaymentCount = len(modern.Postings[0].Payments)
	}
	if coverage.RootCount != preflight.RootCount || coverage.ReceiptCount != preflight.ReceiptCount ||
		len(coverage.Postings) != 1 || coverage.Postings[0].Posting == nil ||
		coveragePaymentCount != 2 || len(modern.Postings) != 1 ||
		modernPaymentCount != 2 || len(coverage.Journals) != 3 || len(modern.Journals) != 3 {
		t.Fatalf("canonical payment capture roots/receipts/full-payments/modern-payments/full-journals/modern-journals=%d/%d/%d/%d/%d/%d",
			coverage.RootCount, coverage.ReceiptCount, coveragePaymentCount,
			modernPaymentCount, len(coverage.Journals), len(modern.Journals))
	}
	if !proto.Equal(coverage.Postings[0].Posting, posted.Posting) ||
		!proto.Equal(coverage.Postings[0].Payments[0], first.Payment) || !proto.Equal(coverage.Postings[0].Payments[1], second.Payment) ||
		!proto.Equal(modern.Postings[0].Posting, posted.Posting) ||
		!proto.Equal(modern.Postings[0].Payments[0], first.Payment) || !proto.Equal(modern.Postings[0].Payments[1], second.Payment) {
		t.Fatal("full or modern capture changed canonical payment order, identity, version, or source hash")
	}
	coverageAgain := captureCoverage(t, f.repo(tx), ctx)
	modernAgain, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
	if err != nil || modernAgain == nil {
		t.Fatalf("repeated canonical modern capture present=%t err=%q", modernAgain != nil, purchaseRootOwnerErrorPreview(err))
	}
	if len(coverage.Fingerprint) != sha256.Size || !reflect.DeepEqual(coverage.Rows, coverageAgain.Rows) ||
		coverage.PayloadBytes != coverageAgain.PayloadBytes || !bytes.Equal(coverage.Fingerprint, coverageAgain.Fingerprint) {
		t.Fatal("repeated full payment capture changed rows, payload bytes, or 32-byte fingerprint")
	}
	if len(modern.Fingerprint) != sha256.Size || !reflect.DeepEqual(modern.Rows, modernAgain.Rows) ||
		modern.PayloadBytes != modernAgain.PayloadBytes || !bytes.Equal(modern.Fingerprint, modernAgain.Fingerprint) {
		t.Fatal("repeated modern payment capture changed rows, payload bytes, or 32-byte fingerprint")
	}
	if int64(countCoverageRows(coverageRawRows(coverage), "purchase_payments_v1", "id", first.Payment.Id)) != 1 ||
		int64(countCoverageRows(coverageRawRows(coverage), "purchase_payments_v1", "id", second.Payment.Id)) != 1 ||
		int64(countCoverageRows(coverageRawRows(coverage), "purchase_payment_allocations_v1", "payment_id", first.Payment.Id)) != 1 ||
		int64(countCoverageRows(coverageRawRows(coverage), "purchase_payment_allocations_v1", "payment_id", second.Payment.Id)) != 1 ||
		int64(countCoverageRows(modern, "purchase_payments_v1", "id", first.Payment.Id)) != 1 ||
		int64(countCoverageRows(modern, "purchase_payments_v1", "id", second.Payment.Id)) != 1 {
		t.Fatal("full or modern retained rows omitted or duplicated a writer-created payment/allocation")
	}
	for _, payment := range payments {
		purchasePaymentOwnerAssertRawPayment(t, coverageRawRows(coverage), payment)
		purchasePaymentOwnerAssertRawPayment(t, modern, payment)
		journal := coverageJournal(t, coverage, payment.JournalId)
		if journal.SourceType != "BANKING" || journal.SourceKind != 7 || journal.SourceID != payment.Id ||
			len(journal.ContentHash) != sha256.Size {
			t.Fatalf("payment journal proof source identity/type/kind is inconsistent with writer payment %s", payment.Id)
		}
	}

	coverageRows := purchaseRootOwnerRowsPayloadBytes(t, coverage.Rows)
	coverageJournalBytes := purchaseRootOwnerJournalPayloadBytes(t, preflight.JournalPayloadBytes)
	if preflight.PayloadBytes != coverageRows+coverageJournalBytes ||
		coverage.BasePayloadBytes != coverageRows || coverage.PayloadBytes != coverageRows+coverageJournalBytes ||
		coverage.PayloadBytes != preflight.PayloadBytes ||
		!reflect.DeepEqual(coverage.JournalIDs, preflight.JournalIDs) ||
		!purchaseRootOwnerJournalPayloadMapsEqual(coverage.JournalPayloadBytes, preflight.JournalPayloadBytes) {
		t.Fatalf("independent full payload rows/parents/expected/preflight/full=%d/%d/%d/%d/%d",
			coverageRows, coverageJournalBytes, coverageRows+coverageJournalBytes,
			preflight.PayloadBytes, coverage.PayloadBytes)
	}
	modernJournalPayloads := make(map[string]int64, len(modern.Journals))
	for _, journal := range modern.Journals {
		if journal == nil || journal.Journal == nil {
			t.Fatal("canonical modern capture omitted a complete journal proof")
		}
		modernJournalPayloads[journal.Journal.Id] = journal.PayloadBytes
	}
	modernRows := purchaseRootOwnerRowsPayloadBytes(t, modern.Rows)
	modernJournalBytes := purchaseRootOwnerJournalPayloadBytes(t, modernJournalPayloads)
	if modern.PayloadBytes != modernRows+modernJournalBytes {
		t.Fatalf("independent modern payload rows/parents/expected/actual=%d/%d/%d/%d",
			modernRows, modernJournalBytes, modernRows+modernJournalBytes, modern.PayloadBytes)
	}
	if len(preflight.JournalIDs) != 3 || !sort.StringsAreSorted(preflight.JournalIDs) {
		t.Fatalf("canonical full preflight journal IDs=%d sorted=%t; want three sorted source proofs",
			len(preflight.JournalIDs), sort.StringsAreSorted(preflight.JournalIDs))
	}
	assertPurchaseRootOwnerNoMutation(t, tx, before, triggers)
}

func TestPurchaseReportingPaymentOwnerRejectsSameByteBlobWithTargetReferences(t *testing.T) {
	tx, f, _, payment, _ := purchasePaymentOwnerFixture(t)
	defer tx.Rollback()
	valid, err := f.repo(tx).SummarizeBASKeys(context.Background(), postingOrg)
	if err != nil || valid == nil {
		t.Fatalf("canonical payment scalar baseline present=%t err=%q", valid != nil, purchaseRootOwnerErrorPreview(err))
	}
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	mutatePurchaseBASScalar(t, f, "purchase_payments_v1",
		`UPDATE purchase_payments_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, payment.Payment.Id)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_payments_v1", payment.Payment.Id, "blob", []byte(postingOrg))
	if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) ||
		postScalar(t, tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("payment owner fixture failed to restore immutable triggers or check-constraint mode")
	}
	afterMutation := purchaseBASScalarState(t, tx)

	purchasePaymentOwnerAssertScalarRejected(t, tx, f, *valid)
	purchasePaymentOwnerAssertPublicRejected(t, tx, f)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_payments_v1", payment.Payment.Id, "blob", []byte(postingOrg))
	assertPurchaseRootOwnerNoMutation(t, tx, afterMutation, triggers)
}

func TestPurchaseReportingPaymentOwnerRejectsDirectOnlyBlobAndIgnoresForeignBlob(t *testing.T) {
	tx, f, posted, payment, initialDocumentID := purchasePaymentOwnerFixture(t)
	defer tx.Rollback()
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
	purchasePaymentOwnerMoveGraphOutOfScope(t, f, posted, payment, initialDocumentID)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_payments_v1", payment.Payment.Id,
		"blob", []byte(purchaseScalarRootOwnerForeignUUID))
	purchasePaymentOwnerAssertAllocationOwner(t, tx,
		"blob", []byte(purchaseScalarRootOwnerForeignUUID))
	if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) ||
		postScalar(t, tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("foreign payment graph fixture failed to restore immutable triggers or check-constraint mode")
	}
	beforeForeignReads := purchaseBASScalarState(t, tx)

	foreignSummary, err := f.repo(tx).SummarizeBASKeys(context.Background(), postingOrg)
	if err != nil || foreignSummary == nil {
		t.Fatalf("isolated foreign BLOB scalar summary present=%t err=%q",
			foreignSummary != nil, purchaseRootOwnerErrorPreview(err))
	}
	if foreignSummary.RootCount != 0 || foreignSummary.ReceiptCount != 0 ||
		foreignSummary.ScalarKeyCount != 0 || foreignSummary.ScalarBytes != 0 ||
		foreignSummary.FamilyCounts != ([purchases.BASScalarFamilyCount]int64{}) ||
		purchaseBASScalarSQLRootCount(t, tx) != 0 || purchaseBASScalarSQLFamilyCounts(t, tx) != ([purchases.BASScalarFamilyCount]int64{}) {
		t.Fatalf("isolated foreign BLOB scalar graph roots/receipts/keys/bytes/families=%d/%d/%d/%d/%v err=%q",
			foreignSummary.RootCount, foreignSummary.ReceiptCount, foreignSummary.ScalarKeyCount,
			foreignSummary.ScalarBytes, foreignSummary.FamilyCounts, purchaseRootOwnerErrorPreview(err))
	}
	foreignKeys, err := f.repo(tx).ReadBASKeys(context.Background(), postingOrg, *foreignSummary, 0, 0)
	if err != nil || len(foreignKeys) != 0 {
		t.Fatalf("isolated foreign BLOB typed keys=%d err=%q; want empty read",
			len(foreignKeys), purchaseRootOwnerErrorPreview(err))
	}
	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	foreignPreflight, err := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		context.Background(), postingOrg, purchaseCoverageDraftPayloadLimit)
	if err != nil || foreignPreflight == nil || foreignPreflight.RootCount != 0 ||
		foreignPreflight.ReceiptCount != 0 || preflightProbe.bodyReads != 0 {
		t.Fatalf("isolated foreign BLOB preflight roots/receipts/bodyReads=%d/%d/%d err=%q",
			purchasePaymentOwnerPreflightRoots(foreignPreflight), purchasePaymentOwnerPreflightReceipts(foreignPreflight),
			preflightProbe.bodyReads, purchaseRootOwnerErrorPreview(err))
	}
	assertPurchaseRootOwnerEmptyCoverage(t, tx, f)
	assertPurchaseRootOwnerNoMutation(t, tx, beforeForeignReads, triggers)

	// Change only the payment's direct owner bytes. Its posting, bank, journal,
	// allocation, document and event references all remain outside target TEXT
	// scope, so this must be rejected solely because the direct owner matches.
	mutatePurchaseBASScalar(t, f, "purchase_payments_v1",
		`UPDATE purchase_payments_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, payment.Payment.Id)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_payments_v1", payment.Payment.Id, "blob", []byte(postingOrg))
	if !reflect.DeepEqual(triggers, purchaseRootOwnerTriggerDefinitions(t, tx)) ||
		postScalar(t, tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("direct-only payment owner fixture failed to restore immutable triggers or check-constraint mode")
	}
	afterMutation := purchaseBASScalarState(t, tx)
	zeroReservation := purchases.BASScalarSummary{OrganisationID: postingOrg}
	purchasePaymentOwnerAssertScalarRejected(t, tx, f, zeroReservation)
	purchasePaymentOwnerAssertPublicRejected(t, tx, f)
	assertPurchaseScalarRootOwnerStorage(t, tx, "purchase_payments_v1", payment.Payment.Id, "blob", []byte(postingOrg))
	assertPurchaseRootOwnerNoMutation(t, tx, afterMutation, triggers)
}

func valueOrZero(summary *purchases.BASScalarSummary, pick func(*purchases.BASScalarSummary) int64) int64 {
	if summary == nil {
		return 0
	}
	return pick(summary)
}
