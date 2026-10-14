//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

type purchaseBASScalarProbe struct {
	*sqlcipher.Transaction
	summaryReads  int
	keyReads      int
	bodyReads     int
	beforeKeyRead func() error
	afterKeyQuery func()
}

func (probe *purchaseBASScalarProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	switch {
	case strings.Contains(query, "/* BAS purchase scalar summary */"):
		probe.summaryReads++
	case strings.Contains(query, "/* BAS purchase scalar keys */"):
		probe.keyReads++
		if probe.beforeKeyRead != nil {
			change := probe.beforeKeyRead
			probe.beforeKeyRead = nil
			if err := change(); err != nil {
				return nil, err
			}
		}
	}
	rows, err := probe.Transaction.QueryContext(ctx, query, args...)
	if err == nil && strings.Contains(query, "/* BAS purchase scalar keys */") && probe.afterKeyQuery != nil {
		after := probe.afterKeyQuery
		probe.afterKeyQuery = nil
		after()
	}
	if err != nil || rows == nil {
		return rows, err
	}
	columns, err := rows.Columns()
	if err == nil {
		for _, column := range columns {
			if purchaseCoverageBodyResultColumn(column) {
				probe.bodyReads++
				break
			}
		}
	}
	return rows, err
}

func emptyPurchaseBASScalarFixture(t *testing.T) (*sqlcipher.Transaction, *postingFixture) {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,'scalar@example.test','Owner','ACTIVE',?,?)`, testkit.ActorUserID, stamp, stamp); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	organisationRepository, err := organisations.NewRepository(tx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	organisation := &tammyv1.Organisation{
		Id: postingOrg, Version: 1, Abn: "51824753556", LegalName: "Scalar Pty Ltd", DisplayName: "Scalar",
		EntityType: "Australian Private Company", GstBasis: tammyv1.GstBasis_GST_BASIS_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6, OwnerUserId: testkit.ActorUserID,
		VerificationState:   tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: "018f0000-0000-7000-8000-000000001003", Revision: 1, ContentHash: make([]byte, 32)},
	}
	if err := organisationRepository.Create(ctx, organisation, now); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	fixture := &postingFixture{t: t, executor: tx, now: now, next: 1100}
	return tx, fixture
}

func purchaseBASScalarState(t *testing.T, tx *sqlcipher.Transaction) map[string]any {
	t.Helper()
	state := coverageStateCounts(t, tx)
	state[`SELECT total_changes()`] = postScalar(t, tx, `SELECT total_changes()`)
	return state
}

func assertPurchaseBASScalarBound(t *testing.T, err error) {
	t.Helper()
	var bound *purchases.SourceBoundError
	if !errors.Is(err, purchases.ErrSourceBound) || !errors.As(err, &bound) {
		t.Fatalf("error %v is not a SourceBoundError", err)
	}
}

func TestPurchaseBASScalarKeysEmptyReservation(t *testing.T) {
	ctx := context.Background()
	tx, fixture := emptyPurchaseBASScalarFixture(t)
	defer tx.Rollback()
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := fixture.repo(probe)
	before := purchaseBASScalarState(t, tx)

	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil || summary.OrganisationID != postingOrg || summary.RootCount != 0 ||
		summary.ReceiptCount != 0 || summary.ScalarKeyCount != 0 || summary.ScalarBytes != 0 {
		t.Fatalf("empty scalar summary = %#v, %v", summary, err)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("empty scalar keys = %#v, %v", keys, err)
	}
	keyReadsBeforeCancellation := probe.keyReads

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := repository.SummarizeBASKeys(cancelled, postingOrg); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scalar summary = %#v, %v", got, err)
	}
	if got, err := repository.ReadBASKeys(cancelled, postingOrg, *summary, 0, 0); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scalar keys = %#v, %v", got, err)
	}
	if probe.keyReads != keyReadsBeforeCancellation || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("empty scalar calls read keys/bodies, changed retained state, or ended the caller transaction")
	}
}

func TestPurchaseBASScalarKeysProjectFullModernSourceFixture(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	fixture, commandRepository, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferral, err := deferCommand(t, commandRepository, deferRequest(t, fixture, state, portion))
	if err != nil {
		t.Fatalf("retain source deferral: %v", err)
	}
	claim := claimRequest(t, fixture, deferral.Accounting, portion)
	claim.DeferralId = &deferral.Deferral.Id
	if _, err := attributeCommand(t, commandRepository, claim); err != nil {
		t.Fatalf("attribute deferred source portion: %v", err)
	}

	// This mirrors purchaseCoverageRootCount's three physical root families.
	// The fixture has one posted purchase, its unposted due-date evidence
	// document, and no opening payable.
	wantRoots := postScalar(t, tx, `SELECT count(*) FROM (
		SELECT id FROM purchase_postings_v1 WHERE organisation_id=?
		UNION ALL
		SELECT d.id FROM documents d WHERE d.organisation_id=? AND NOT EXISTS(
			SELECT 1 FROM purchase_postings_v1 p WHERE p.organisation_id=d.organisation_id AND p.document_id=d.id
		)
		UNION ALL
		SELECT p.id FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED')
	)`, postingOrg, postingOrg, postingOrg)
	wantReceipts := postScalar(t, tx, `SELECT count(*) FROM purchase_payments_v1 WHERE organisation_id=?`, postingOrg)
	if wantRoots == 0 || wantReceipts != 1 {
		t.Fatalf("full modern fixture roots/receipts = %d/%d; want nonempty roots and one real receipt", wantRoots, wantReceipts)
	}
	before := purchaseBASScalarState(t, tx)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := fixture.repo(probe)

	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil || int64(summary.RootCount) != wantRoots || int64(summary.ReceiptCount) != wantReceipts ||
		summary.ScalarKeyCount <= 0 || summary.ScalarBytes <= 0 {
		t.Fatalf("full modern scalar summary = %#v, %v; want roots/receipts = %d/%d", summary, err, wantRoots, wantReceipts)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("exact scalar reservation returned %d keys, %v; summary=%#v", len(keys), err, summary)
	}
	if probe.summaryReads == 0 || probe.keyReads == 0 || probe.bodyReads != 0 {
		t.Fatalf("scalar probe saw summary/key/body queries %d/%d/%d; want both scalar queries and no body reads", probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
	keyReadsBeforeRejectedBudgets := probe.keyReads

	if got, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount-1, summary.ScalarBytes); got != nil {
		t.Fatalf("one-short key budget returned %d keys", len(got))
	} else {
		assertPurchaseBASScalarBound(t, err)
	}
	if got, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes-1); got != nil {
		t.Fatalf("one-short byte budget returned %d keys", len(got))
	} else {
		assertPurchaseBASScalarBound(t, err)
	}
	if probe.keyReads != keyReadsBeforeRejectedBudgets || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("scalar reads exceeded a rejected budget, read bodies, changed retained state, or ended the caller transaction")
	}
}
