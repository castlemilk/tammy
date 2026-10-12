//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

type purchaseReportingCountProbe struct {
	app.CommandSQLExecutor
	hydrated int
}

func (probe *purchaseReportingCountProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if !strings.Contains(strings.ToLower(query), "count(*)") {
		probe.hydrated++
	}
	return probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
}

func TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	for index := 0; index < 1000; index++ {
		postReportingPurchase(t, f, fmt.Sprintf("ZERO-ROOT-%d", index), postDate(10), 0, 0)
	}
	capture := capturePostings(t, f.repo(tx), ctx, postingOrg)
	if len(capture.Postings) != 1000 || len(capture.Documents) != 1000 || len(capture.Journals) != 0 {
		t.Fatalf("exact root capacity returned %d postings/%d documents/%d journals", len(capture.Postings), len(capture.Documents), len(capture.Journals))
	}
	if capture.PayloadBytes <= 0 || capture.PayloadBytes > 8*1024*1024 {
		t.Fatalf("exact root fixture escaped the retained byte budget: %d", capture.PayloadBytes)
	}
	postReportingPurchase(t, f, "ZERO-ROOT-OVERFLOW", postDate(10), 0, 0)
	probe := &purchaseReportingCountProbe{CommandSQLExecutor: tx}
	partial, err := f.repo(probe).CapturePostingsForReporting(ctx, postingOrg)
	assertPurchaseSourceBound(t, err, 1000)
	var bound *purchases.SourceBoundError
	if partial != nil || !errors.As(err, &bound) || bound.Resource != "posting roots" || probe.hydrated != 0 {
		t.Fatalf("1001 roots returned partial=%v bound=%+v hydration=%d", partial != nil, bound, probe.hydrated)
	}
}

func TestPurchaseReportingPaymentCountBoundaryPrecedesBodyHydration(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	if _, err := f.repo(tx).PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	first, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, 1, 1, postDate(10)))
	if err != nil {
		t.Fatal(err)
	}
	// SQL-only cap fixtures keep all ownership/immutable/balance triggers. Their
	// copied protobufs deliberately need not replay: a zero caller budget must
	// reject before any body read, independently of the payment-count cap.
	appendCountFixture := func(index int) {
		t.Helper()
		paymentID, _ := f.id()
		journalID, _ := f.id()
		if _, err := tx.ExecContext(ctx, `INSERT INTO journals(
			id,organisation_id,version,source_type,source_id,source_revision,state,journal_date,
			description,reversal_of_journal_id,reversed_by_journal_id,total_debits_minor,
			total_credits_minor,currency_code,financial_revision,posted_at,created_at,source_kind)
			SELECT ?,organisation_id,version,source_type,?,source_revision,'DRAFT',journal_date,
			description,NULL,NULL,total_debits_minor,total_credits_minor,currency_code,
			financial_revision,NULL,created_at,source_kind FROM journals WHERE id=?`, journalID, paymentID, first.Payment.JournalId); err != nil {
			t.Fatal(err)
		}
		for _, line := range first.Journal.Lines {
			lineID, _ := f.id()
			if _, err := tx.ExecContext(ctx, `INSERT INTO journal_lines(
				id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code,memo,
				tax_code_id,tax_amount_minor,tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash)
				SELECT ?,?,line_number,account_id,debit_minor,credit_minor,currency_code,memo,
				tax_code_id,tax_amount_minor,tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash
				FROM journal_lines WHERE id=?`, lineID, journalID, line.Id); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE journals SET state='POSTED',posted_at=? WHERE id=?`, first.Payment.RecordedAt.AsTime().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), journalID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO purchase_payments_v1(
			id,posting_id,organisation_id,accounting_version,payment_date,amount_minor,
			currency_code,bank_account_id,journal_id,reference,payment_proto,source_hash,actor_user_id,created_at)
			SELECT ?,posting_id,organisation_id,?,payment_date,amount_minor,currency_code,
			bank_account_id,?,reference,payment_proto,source_hash,actor_user_id,created_at
			FROM purchase_payments_v1 WHERE id=?`, paymentID, index+2, journalID, first.Payment.Id); err != nil {
			t.Fatal(err)
		}
	}
	for index := 1; index < 1000; index++ {
		appendCountFixture(index)
	}
	probe := &purchaseReportingCountProbe{CommandSQLExecutor: tx}
	partial, err := f.repo(probe).CapturePostingsForReportingWithPayloadBudget(ctx, postingOrg, 0)
	assertPurchaseSourceBound(t, err, 0)
	var bound *purchases.SourceBoundError
	if partial != nil || !errors.As(err, &bound) || bound.Resource != "capture bytes" || probe.hydrated != 0 {
		t.Fatalf("1000 payments did not reach the independent byte bound before hydration: partial=%v bound=%+v hydration=%d", partial != nil, bound, probe.hydrated)
	}
	appendCountFixture(1000)
	partial, err = f.repo(probe).CapturePostingsForReportingWithPayloadBudget(ctx, postingOrg, 0)
	assertPurchaseSourceBound(t, err, 1000)
	if partial != nil || !errors.As(err, &bound) || bound.Resource != "payments" || probe.hydrated != 0 {
		t.Fatalf("1001 payments did not fail at its count bound before hydration: partial=%v bound=%+v hydration=%d", partial != nil, bound, probe.hydrated)
	}
}
