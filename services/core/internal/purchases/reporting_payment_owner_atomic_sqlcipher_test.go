//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

type purchasePaymentOwnerAtomicProbe struct {
	app.CommandSQLExecutor
	t                     *testing.T
	fixture               *postingFixture
	tx                    *sqlcipher.Transaction
	mode                  string
	stage                 string
	ownerID               string
	bodyColumn            string
	bodyID                string
	hookCount             int
	afterHook             bool
	laterBodies           int
	documentReplayReads   int
	stateAfterMutation    map[string]any
	triggersBefore        map[string]string
	triggersAfterMutation map[string]string
	beforeRead            runtime.MemStats
}

func (probe *purchasePaymentOwnerAtomicProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if probe.afterHook && purchaseReportingRootOwnerIsBodyStatement(query) {
		probe.laterBodies++
	}
	isDocumentReplay := purchaseReportingRootOwnerIsDocumentReplayQuery(query)
	if probe.hookCount == 0 && probe.matchesHook(query) {
		if probe.stage == "payment-replay" && probe.documentReplayReads == 0 {
			probe.t.Fatalf("payment replay hook ran before a successful document replay")
		}
		probe.hookCount++
		probe.mutateAndSample()
	}
	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if isDocumentReplay && err == nil && rows != nil {
		probe.documentReplayReads++
	}
	return rows, err
}

func (probe *purchasePaymentOwnerAtomicProbe) matchesHook(query string) bool {
	for _, marker := range probe.markers() {
		if strings.Contains(query, marker) {
			if probe.stage != "payment-journal-ids" ||
				strings.Contains(strings.ToLower(query), "from purchase_payments_v1 as j") {
				return true
			}
		}
	}
	compact := purchaseReportingRootOwnerCompactSQL(query)
	switch probe.stage {
	case "raw-payment", "modern-empty":
		return strings.HasPrefix(compact,
			"selectp.id,p.posting_id,p.organisation_id,p.accounting_version,p.payment_date,p.amount_minor,p.currency_code,p.bank_account_id,p.journal_id,p.reference,p.payment_proto,") &&
			strings.Contains(compact, "frompurchase_payments_v1asp")
	case "payment-replay":
		return strings.HasPrefix(compact,
			"selectid,accounting_version,payment_date,amount_minor,currency_code,bank_account_id,journal_id,reference,payment_proto,source_hash,actor_user_id,created_atfrompurchase_payments_v1whereposting_id=?andorganisation_id=?orderbyaccounting_version,id")
	case "payment-journal-ids":
		return strings.HasPrefix(compact,
			"selectj.journal_idfrompurchase_payments_v1asjwherej.organisation_id=?orderbyj.id")
	default:
		return false
	}
}

func (probe *purchasePaymentOwnerAtomicProbe) markers() []string {
	switch probe.stage {
	case "raw-payment", "modern-empty":
		return []string{"/* BAS purchase owner body purchase_payments_v1 */"}
	case "payment-replay":
		return []string{"/* BAS purchase owner body payment replay */"}
	case "payment-journal-ids":
		return []string{"/* BAS purchase owner body coverage_journal_ids */"}
	default:
		return nil
	}
}

func (probe *purchasePaymentOwnerAtomicProbe) mutateAndSample() {
	probe.t.Helper()
	probe.triggersBefore = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
	if probe.bodyColumn == "" {
		mutatePurchaseBASScalar(probe.t, probe.fixture, "purchase_payments_v1",
			`UPDATE purchase_payments_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, probe.ownerID)
	} else if probe.ownerID == probe.bodyID {
		mutatePurchaseBASScalar(probe.t, probe.fixture, "purchase_payments_v1",
			"UPDATE purchase_payments_v1 SET organisation_id=CAST(? AS BLOB),"+probe.bodyColumn+"=zeroblob(?) WHERE id=?",
			postingOrg, purchaseRootOwnerLateCellBytes, probe.ownerID)
	} else {
		mutatePurchaseBASScalar(probe.t, probe.fixture, "purchase_payments_v1",
			`UPDATE purchase_payments_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, probe.ownerID)
		mutatePurchaseBASScalar(probe.t, probe.fixture, "purchase_payments_v1",
			"UPDATE purchase_payments_v1 SET "+probe.bodyColumn+"=zeroblob(?) WHERE id=?",
			purchaseRootOwnerLateCellBytes, probe.bodyID)
	}
	purchaseRootOwnerAssertStoredBLOB(probe.t, probe.tx, "purchase_payments_v1", probe.ownerID, []byte(postingOrg))
	if probe.bodyColumn != "" {
		purchaseRootOwnerAssertLateCellMetadata(probe.t, probe.tx, "purchase_payments_v1", probe.bodyColumn, probe.bodyID)
		if probe.bodyID != probe.ownerID {
			purchasePaymentOwnerAssertTextSelectedOwner(probe.t, probe.tx, probe.bodyID)
		}
	}
	if got := postScalar(probe.t, probe.tx, `PRAGMA ignore_check_constraints`); got != 0 {
		probe.t.Fatalf("fixture mutation left ignore_check_constraints=%d, want 0", got)
	}
	probe.triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) {
		probe.t.Fatal("payment owner fixture mutation did not restore exact trigger definitions")
	}
	probe.stateAfterMutation = purchaseReportingRootOwnerState(probe.t, probe.tx, probe.mode)
	probe.afterHook = true
	runtime.GC()
	runtime.ReadMemStats(&probe.beforeRead)
}

func purchasePaymentOwnerCapture(t *testing.T, repo *purchases.Repository, mode string) (bool, error) {
	t.Helper()
	return purchaseReportingRootOwnerCapture(t, repo, mode)
}

func purchasePaymentOwnerAssertAtomicRejection(
	t *testing.T,
	probe *purchasePaymentOwnerAtomicProbe,
	nilCapture bool,
	err error,
	afterRead runtime.MemStats,
) {
	t.Helper()
	if probe.hookCount != 1 {
		t.Fatalf("raw %s hook count=%d, want exactly one; capture_nil=%t error_bytes=%d",
			probe.stage, probe.hookCount, nilCapture, purchasePaymentOwnerErrorBytes(err))
	}
	allocation := afterRead.TotalAlloc - probe.beforeRead.TotalAlloc
	errorBytes := purchasePaymentOwnerErrorBytes(err)
	errorPreview := purchasePaymentOwnerErrorPreview(err)
	if !nilCapture || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("late %s owner drift returned capture_nil=%t error_bytes=%d error=%q; want nil capture and ErrRepository",
			probe.stage, nilCapture, errorBytes, errorPreview)
	}
	if errorBytes >= 512 || allocation >= 2*1024*1024 {
		t.Fatalf("late %s rejection copied a payment cell: error_bytes=%d allocation=%d error=%q",
			probe.stage, errorBytes, allocation, errorPreview)
	}
	if probe.laterBodies != 0 {
		t.Fatalf("late %s rejection attempted %d subsequent body queries", probe.stage, probe.laterBodies)
	}
	purchaseRootOwnerAssertStoredBLOB(t, probe.tx, "purchase_payments_v1", probe.ownerID, []byte(postingOrg))
	if probe.bodyColumn != "" {
		purchaseRootOwnerAssertLateCellMetadata(t, probe.tx, "purchase_payments_v1", probe.bodyColumn, probe.bodyID)
		if probe.bodyID != probe.ownerID {
			purchasePaymentOwnerAssertTextSelectedOwner(t, probe.tx, probe.bodyID)
		}
	}
	if probe.stateAfterMutation == nil ||
		!reflect.DeepEqual(probe.stateAfterMutation, purchaseReportingRootOwnerState(t, probe.tx, probe.mode)) {
		t.Fatal("payment owner rejection changed retained state or total_changes after the fixture mutation")
	}
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) ||
		!reflect.DeepEqual(probe.triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, probe.tx)) {
		t.Fatal("payment owner rejection left trigger definitions different from the post-mutation baseline")
	}
	if !probe.tx.Authenticated() {
		t.Fatal("payment owner rejection ended the caller-owned encrypted transaction")
	}
	if got := postScalar(t, probe.tx, `PRAGMA ignore_check_constraints`); got != 0 {
		t.Fatalf("payment owner rejection left ignore_check_constraints=%d, want 0", got)
	}
}

func purchasePaymentOwnerErrorBytes(err error) int {
	if err == nil {
		return 0
	}
	return len(err.Error())
}

func purchasePaymentOwnerErrorPreview(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 240 {
		text = text[:240]
	}
	return text
}

func purchasePaymentOwnerAssertTextSelectedOwner(t *testing.T, tx *sqlcipher.Transaction, paymentID string) {
	t.Helper()
	var storageType string
	var byteLength int64
	var owner string
	err := tx.QueryRowContext(context.Background(),
		`SELECT typeof(organisation_id),length(organisation_id),CAST(organisation_id AS TEXT)
		 FROM purchase_payments_v1 WHERE id=?`, paymentID).Scan(&storageType, &byteLength, &owner)
	if err != nil {
		t.Fatalf("inspect still-selected payment owner: %v", err)
	}
	if storageType != "text" || byteLength != int64(len(postingOrg)) || owner != postingOrg {
		t.Fatalf("other payment owner type/length/value=%q/%d/%q; want TEXT/%d/exact requested owner",
			storageType, byteLength, owner, len(postingOrg))
	}
}

func purchasePaymentOwnerAtomicRecordAdditionalCanonical(t *testing.T, f *postingFixture, version uint64, amount int64, day int32) *tammyv1.PurchasePayment {
	t.Helper()
	response, err := f.repo(f.executor).RecordPayment(context.Background(),
		paymentRequest(t, f, version, amount, paymentDate(8, day)))
	if err != nil || response == nil || response.Payment == nil || response.Journal == nil {
		t.Fatalf("RecordPayment fixture returned payment=%t journal=%t error=%q",
			response != nil && response.Payment != nil, response != nil && response.Journal != nil, purchasePaymentOwnerErrorPreview(err))
	}
	return response.Payment
}

func TestPurchaseBASPaymentOwnerScalarLateEmptySentinel(t *testing.T) {
	ctx := context.Background()
	tx, f, posted, paymentResponse, initialDocumentID := purchasePaymentOwnerFixture(t)
	defer tx.Rollback()
	if posted == nil || paymentResponse == nil || paymentResponse.Payment == nil {
		t.Fatal("PostReviewed fixture returned no accounting state")
	}
	payment := paymentResponse.Payment
	purchasePaymentOwnerMoveGraphOutOfScope(t, f, posted, paymentResponse, initialDocumentID)
	mutatePurchaseBASScalar(t, f, "purchase_payments_v1",
		`UPDATE purchase_payments_v1 SET organisation_id='foreign-owner' WHERE id=?`, payment.Id)

	triggerDefinitions := purchaseRootOwnerTriggerDefinitions(t, tx)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	wantEmpty := purchases.BASScalarSummary{OrganisationID: postingOrg}
	if err != nil || summary == nil || *summary != wantEmpty {
		t.Fatalf("foreign-scope payment scalar baseline=%#v err=%q, want a zero reservation", summary, purchasePaymentOwnerErrorPreview(err))
	}
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("foreign-scope scalar read returned %d keys, error=%q; want an empty read", len(keys), purchasePaymentOwnerErrorPreview(err))
	}
	if !reflect.DeepEqual(triggerDefinitions, purchaseRootOwnerTriggerDefinitions(t, tx)) || !tx.Authenticated() {
		t.Fatal("empty scalar baseline changed triggers or ended the transaction")
	}

	summaryReadsBeforeLate, keyReadsBeforeLate := probe.summaryReads, probe.keyReads
	var stateAfterMutation map[string]any
	var triggersAfterMutation map[string]string
	var beforeRead runtime.MemStats
	probe.beforeKeyRead = func() error {
		if probe.summaryReads != summaryReadsBeforeLate+1 {
			return errors.New("late payment mutation did not follow exactly one fresh scalar summary")
		}
		mutatePurchaseBASScalar(t, f, "purchase_payments_v1",
			`UPDATE purchase_payments_v1 SET organisation_id=CAST(? AS BLOB),accounting_version=zeroblob(?) WHERE id=?`,
			postingOrg, purchaseRootOwnerLateCellBytes, payment.Id)
		purchaseRootOwnerAssertStoredBLOB(t, tx, "purchase_payments_v1", payment.Id, []byte(postingOrg))
		purchaseRootOwnerAssertLateCellMetadata(t, tx, "purchase_payments_v1", "accounting_version", payment.Id)
		if got := postScalar(t, tx, `PRAGMA ignore_check_constraints`); got != 0 {
			t.Fatalf("fixture mutation left ignore_check_constraints=%d, want 0", got)
		}
		stateAfterMutation = purchaseBASScalarState(t, tx)
		triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(t, tx)
		if !reflect.DeepEqual(triggerDefinitions, triggersAfterMutation) {
			t.Fatal("scalar payment owner fixture mutation did not restore exact trigger definitions")
		}
		runtime.GC()
		runtime.ReadMemStats(&beforeRead)
		return nil
	}

	keys, err = repo.ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
	var afterRead runtime.MemStats
	runtime.ReadMemStats(&afterRead)
	allocation := afterRead.TotalAlloc - beforeRead.TotalAlloc
	errorBytes := purchasePaymentOwnerErrorBytes(err)
	var bound *purchases.SourceBoundError
	if keys != nil || !errors.Is(err, purchases.ErrSourceBound) ||
		!errors.As(err, &bound) || bound.Resource != "BAS scalar keys" || bound.Limit != 0 {
		t.Fatalf("late scalar payment owner returned keys_nil=%t bound=%#v error_bytes=%d error=%q; want nil keys and BAS scalar keys/0",
			keys == nil, bound, errorBytes, purchasePaymentOwnerErrorPreview(err))
	}
	if errorBytes >= 512 || allocation >= 2*1024*1024 {
		t.Fatalf("late scalar payment guard copied a 9 MiB version cell: error_bytes=%d allocation=%d error=%q",
			errorBytes, allocation, purchasePaymentOwnerErrorPreview(err))
	}
	purchaseRootOwnerAssertStoredBLOB(t, tx, "purchase_payments_v1", payment.Id, []byte(postingOrg))
	purchaseRootOwnerAssertLateCellMetadata(t, tx, "purchase_payments_v1", "accounting_version", payment.Id)
	if probe.summaryReads != summaryReadsBeforeLate+1 || probe.keyReads != keyReadsBeforeLate+1 || probe.bodyReads != 0 {
		t.Fatalf("late scalar payment read issued summary/key/body queries %d/%d/%d; want one fresh summary, one key query, no body reads",
			probe.summaryReads-summaryReadsBeforeLate, probe.keyReads-keyReadsBeforeLate, probe.bodyReads)
	}
	if stateAfterMutation == nil || !reflect.DeepEqual(stateAfterMutation, purchaseBASScalarState(t, tx)) ||
		!reflect.DeepEqual(triggerDefinitions, triggersAfterMutation) ||
		!reflect.DeepEqual(triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, tx)) || !tx.Authenticated() {
		t.Fatal("scalar payment owner rejection changed retained state, failed to restore triggers, or ended the transaction")
	}
}

func TestPurchaseReportingPaymentOwnerLateRawRowsAreAtomic(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		for _, sameRow := range []bool{true, false} {
			caseName := "different_text_row_body"
			if sameRow {
				caseName = "same_payment_row"
			}
			t.Run(mode+"/"+caseName, func(t *testing.T) {
				tx, f, posted, paymentResponse, _ := purchasePaymentOwnerFixture(t)
				defer tx.Rollback()
				if posted == nil || paymentResponse == nil || paymentResponse.Payment == nil {
					t.Fatal("PostReviewed fixture returned no accounting state")
				}
				ownerPayment := paymentResponse.Payment
				bodyPayment := ownerPayment
				if !sameRow {
					bodyPayment = purchasePaymentOwnerAtomicRecordAdditionalCanonical(t, f, paymentResponse.Accounting.AccountingVersion, 1000, 21)
				}
				probe := &purchasePaymentOwnerAtomicProbe{
					CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode,
					stage: "raw-payment", ownerID: ownerPayment.Id, bodyColumn: "payment_proto", bodyID: bodyPayment.Id,
				}
				nilCapture, err := purchasePaymentOwnerCapture(t, f.repo(probe), mode)
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				purchasePaymentOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
			})
		}
	}
}

func TestPurchaseReportingPaymentOwnerLateJournalIDsMaskOtherPaymentMetadata(t *testing.T) {
	tx, f, posted, paymentResponse, _ := purchasePaymentOwnerFixture(t)
	defer tx.Rollback()
	if posted == nil || paymentResponse == nil || paymentResponse.Payment == nil {
		t.Fatal("PostReviewed fixture returned no accounting state")
	}
	ownerPayment := paymentResponse.Payment
	bodyPayment := purchasePaymentOwnerAtomicRecordAdditionalCanonical(t, f, paymentResponse.Accounting.AccountingVersion, 1000, 21)
	probe := &purchasePaymentOwnerAtomicProbe{
		CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: "full-coverage",
		stage: "payment-journal-ids", ownerID: ownerPayment.Id, bodyColumn: "journal_id", bodyID: bodyPayment.Id,
	}
	nilCapture, err := purchasePaymentOwnerCapture(t, f.repo(probe), "full-coverage")
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchasePaymentOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
}

func TestPurchaseReportingPaymentOwnerLateEmptyJournalIDBranchFindsBLOB(t *testing.T) {
	for _, operation := range []string{"preflight", "capture"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			tx, f, posted, paymentResponse, initialDocumentID := purchasePaymentOwnerFixture(t)
			defer tx.Rollback()
			if posted == nil || paymentResponse == nil || paymentResponse.Payment == nil {
				t.Fatal("PostReviewed fixture returned no accounting state")
			}
			payment := paymentResponse.Payment
			purchasePaymentOwnerMoveGraphOutOfScope(t, f, posted, paymentResponse, initialDocumentID)
			mutatePurchaseBASScalar(t, f, "purchase_payments_v1",
				`UPDATE purchase_payments_v1 SET organisation_id='foreign-owner' WHERE id=?`, payment.Id)

			probe := &purchasePaymentOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: "full-coverage",
				stage: "payment-journal-ids", ownerID: payment.Id,
				bodyColumn: "journal_id", bodyID: payment.Id,
			}
			var nilCapture bool
			var err error
			if operation == "preflight" {
				preflight, preflightErr := f.repo(probe).PreflightCoverageForBASWithPayloadBudget(
					ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
				nilCapture, err = preflight == nil, preflightErr
			} else {
				nilCapture, err = purchasePaymentOwnerCapture(t, f.repo(probe), "full-coverage")
			}
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchasePaymentOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
		})
	}
}

func TestPurchaseReportingPaymentOwnerLateReplayMasksRemainingPaymentBody(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		t.Run(mode, func(t *testing.T) {
			tx, f, posted, paymentResponse, _ := purchasePaymentOwnerFixture(t)
			defer tx.Rollback()
			if posted == nil || paymentResponse == nil || paymentResponse.Payment == nil {
				t.Fatal("PostReviewed fixture returned no accounting state")
			}
			ownerPayment := paymentResponse.Payment
			bodyPayment := purchasePaymentOwnerAtomicRecordAdditionalCanonical(t, f, paymentResponse.Accounting.AccountingVersion, 1000, 21)
			probe := &purchasePaymentOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode,
				stage: "payment-replay", ownerID: ownerPayment.Id, bodyColumn: "payment_proto", bodyID: bodyPayment.Id,
			}
			nilCapture, err := purchasePaymentOwnerCapture(t, f.repo(probe), mode)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchasePaymentOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
			if probe.documentReplayReads == 0 {
				t.Fatal("payment replay was not preceded by a successful document replay")
			}
		})
	}
}

func TestPurchaseReportingPaymentOwnerZeroToBLOBInFreshEmptyModernBranch(t *testing.T) {
	ctx := context.Background()
	tx, f, posted, paymentResponse, initialDocumentID := purchasePaymentOwnerFixture(t)
	defer tx.Rollback()
	if posted == nil || paymentResponse == nil || paymentResponse.Payment == nil {
		t.Fatal("PostReviewed fixture returned no accounting state")
	}
	payment := paymentResponse.Payment
	purchasePaymentOwnerMoveGraphOutOfScope(t, f, posted, paymentResponse, initialDocumentID)
	mutatePurchaseBASScalar(t, f, "purchase_payments_v1",
		`UPDATE purchase_payments_v1 SET organisation_id='foreign-owner' WHERE id=?`, payment.Id)

	baseline, baselineErr := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
	if baselineErr != nil || baseline == nil || len(baseline.Postings) != 0 || len(baseline.Documents) != 0 {
		t.Fatalf("foreign-scope modern payment baseline postings=%d documents=%d error=%q; want empty capture",
			paymentOwnerPostingCount(baseline), paymentOwnerDocumentCount(baseline), purchasePaymentOwnerErrorPreview(baselineErr))
	}
	probe := &purchasePaymentOwnerAtomicProbe{
		CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: "modern-capture",
		stage: "modern-empty", ownerID: payment.Id,
	}
	nilCapture, err := purchasePaymentOwnerCapture(t, f.repo(probe), "modern-capture")
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	purchasePaymentOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
}

func paymentOwnerPostingCount(capture *purchases.PostingReportingCapture) int {
	if capture == nil {
		return -1
	}
	return len(capture.Postings)
}

func paymentOwnerDocumentCount(capture *purchases.PostingReportingCapture) int {
	if capture == nil {
		return -1
	}
	return len(capture.Documents)
}
