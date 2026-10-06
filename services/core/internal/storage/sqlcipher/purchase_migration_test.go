//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var purchaseHistoryTables = []string{"purchase_postings_v1", "purchase_posting_allocations_v1", "purchase_payments_v1", "purchase_payment_allocations_v1", "purchase_tax_evidence_v1", "purchase_gst_attributions_v1", "purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1", "purchase_legacy_reviews_v1", "purchase_accounting_events_v1", "purchase_command_audit_v1", "bas_workpaper_purchase_sources_v1", "bas_workpaper_purchase_reviews_v1", "bas_workpaper_purchase_support_v1", "bas_workpaper_purchase_metadata_v1"}

func TestPurchaseUpgradeRetainsPrefix11BusinessRowsWithoutBackfill(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "purchase-upgrade.db")
	key := testKey(0x82)
	if _, err := MigrateWorkspace(ctx, path, key, 11); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	purchaseSeedHistorical(t, db)
	queries := []string{"SELECT * FROM documents ORDER BY id", "SELECT * FROM purchase_opening_payables ORDER BY id", "SELECT * FROM opening_conversions ORDER BY id", "SELECT * FROM invoice_income_postings_v1 ORDER BY invoice_id", "SELECT * FROM invoice_payment_v1 ORDER BY id", "SELECT * FROM bas_workpapers ORDER BY id", "SELECT * FROM bas_workpaper_sources ORDER BY sequence", "SELECT * FROM bas_workpaper_sales_sources_v1 ORDER BY sequence", "SELECT * FROM bas_workpaper_save_order_v1 ORDER BY sequence", "SELECT id,organisation_id,source_type,source_id,source_revision,state,total_debits_minor,total_credits_minor FROM journals ORDER BY id", "SELECT * FROM journal_lines ORDER BY id", "SELECT * FROM tax_facts ORDER BY id", "SELECT * FROM cash_flow_facts ORDER BY id", "SELECT * FROM financial_revisions"}
	before := make([][32]byte, len(queries))
	for i, q := range queries {
		before[i] = purchaseQueryHash(t, db, q)
	}
	if err := db.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateWorkspace(ctx, path, key, 12); err != nil {
		t.Fatalf("purchase upgrade missing: %v", err)
	}
	db, err = Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, q := range queries {
		if got := purchaseQueryHash(t, db, q); got != before[i] {
			t.Fatalf("historical rows changed: %s", q)
		}
	}
	for _, table := range purchaseHistoryTables {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("new business rows %s=%d, %v", table, n, err)
		}
	}
	var kinds int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM journals WHERE source_kind<>0`).Scan(&kinds); err != nil || kinds != 0 {
		t.Fatalf("legacy discriminator rewritten: %d %v", kinds, err)
	}
	assertAppliedVersions(t, db, 12)
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrationRelationalIntegrityCheck(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseSchemaEnforcesOwnedImmutableHistoryAndCommandVersions(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "purchase-constraints.db"), testKey(0x83))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := applyMigrations(ctx, db, allMigrations(t), 12); err != nil {
		t.Fatalf("purchase migration missing: %v", err)
	}
	purchaseSeedHistorical(t, db)
	// Two organisations are used only to exercise composite ownership constraints.
	mustExec(t, db, `DROP TRIGGER organisations_singleton_insert`)
	mustExec(t, db, `INSERT INTO organisations(id,legal_name,status,created_at) VALUES('other','Other','INACTIVE','2026-10-01T00:00:00Z')`)
	mustExec(t, db, `CREATE TRIGGER organisations_singleton_insert BEFORE INSERT ON organisations WHEN EXISTS(SELECT 1 FROM organisations) BEGIN SELECT RAISE(ABORT,'workspace already has an organisation'); END`)
	purchaseInsert(t, db, "accounts", map[string]any{"id": "other-account", "organisation_id": "other", "code": "5000", "name": "Other", "account_type": "EXPENSE", "normal_balance": "DEBIT", "status": "ACTIVE", "designation": "ORDINARY", "created_at": "2026-10-01T00:00:00Z"}, true)
	posting := map[string]any{"id": "posting", "organisation_id": "org", "document_id": "dated", "reviewed_version": 2, "original_content_hash": bytes.Repeat([]byte{1}, 32), "reviewed_snapshot_hash": bytes.Repeat([]byte{2}, 32), "reviewed_source_proto": []byte{1}, "supplier_name": "Supplier", "supplier_identity": "supplier", "invoice_number": "INV", "invoice_date": "2026-10-01", "gst_basis": 2, "gst_profile_version": 1, "gst_history_watermark": 0, "gross_minor": 1100, "supplier_gst_minor": 100, "eligible_gst_minor": 100, "currency_code": "AUD", "journal_id": "purchase-journal", "financial_revision": 1, "duplicate_decision": 1, "duplicate_reason": "", "prior_accounting_decision": 1, "prior_accounting_reason": "Not accounted", "normalized_supplier": "supplier", "normalized_invoice": "inv", "source_hash": bytes.Repeat([]byte{3}, 32), "posting_proto": []byte{1}, "actor_user_id": "user", "created_at": "2026-10-01T00:00:00Z"}
	purchaseJournal(t, db, "purchase-journal", "PAYABLE", 6, "posting")
	for name, changes := range map[string]map[string]any{"missing id": {"id": nil}, "blank id": {"id": ""}, "wrong document owner": {"organisation_id": "other"}, "bad leap date": {"invoice_date": "2023-02-29"}, "bad April date": {"invoice_date": "2026-04-31"}, "date text": {"invoice_date": "banana"}, "bad currency": {"currency_code": "USD"}, "bad amount": {"eligible_gst_minor": 101}, "bad hash": {"source_hash": []byte{1}}, "bad version": {"reviewed_version": 0}, "fractional version": {"financial_revision": 1.5}, "text hash": {"source_hash": strings.Repeat("a", 32)}, "distinct match requires reason": {"duplicate_decision": 2}, "positive GST without basis": {"gst_basis": 0}, "fractional history": {"gst_history_watermark": 1.5}, "wrong journal kind": {"journal_id": "invoice-journal"}} {
		t.Run(name, func(t *testing.T) {
			purchaseInsert(t, db, "purchase_postings_v1", purchaseChanged(posting, changes), false)
		})
	}
	purchaseInsert(t, db, "purchase_postings_v1", posting, true)
	purchaseInsert(t, db, "purchase_postings_v1", purchaseChanged(posting, map[string]any{"id": "other-posting", "gst_basis": 0, "document_id": "duplicate", "original_content_hash": bytes.Repeat([]byte{3}, 32), "gross_minor": 0, "supplier_gst_minor": 0, "eligible_gst_minor": 0, "journal_id": nil}), true)

	allocation := map[string]any{"posting_id": "posting", "organisation_id": "org", "sequence": 1, "account_id": "expense", "account_version": 1, "gross_minor": 1100, "supplier_gst_minor": 100, "eligible_gst_minor": 100, "use_kind": 1, "bas_classification": 2, "cash_flow_classification": 1, "noncreditable_reason": "", "allocation_proto": []byte{1}}
	purchaseInsert(t, db, "purchase_posting_allocations_v1", purchaseChanged(allocation, map[string]any{"account_id": "other-account"}), false)
	purchaseInsert(t, db, "purchase_posting_allocations_v1", purchaseChanged(allocation, map[string]any{"use_kind": 2}), false)
	for _, column := range []string{"bas_classification", "cash_flow_classification"} {
		t.Run("fractional posting "+column, func(t *testing.T) {
			purchaseInsert(t, db, "purchase_posting_allocations_v1", purchaseChanged(allocation, map[string]any{column: 1.5}), false)
		})
	}
	purchaseInsert(t, db, "purchase_posting_allocations_v1", allocation, true)
	purchaseInsert(t, db, "purchase_posting_allocations_v1", purchaseChanged(allocation, map[string]any{"posting_id": "other-posting", "gross_minor": 0, "supplier_gst_minor": 0, "eligible_gst_minor": 0}), true)

	purchaseInsert(t, db, "purchase_posting_allocations_v1", purchaseChanged(allocation, map[string]any{"sequence": 2}), true)
	purchaseJournal(t, db, "payment-journal", "BANKING", 7, "payment")
	payment := map[string]any{"id": "payment", "posting_id": "posting", "organisation_id": "org", "accounting_version": 2, "payment_date": "2026-10-02", "amount_minor": 550, "currency_code": "AUD", "bank_account_id": "bank", "journal_id": "payment-journal", "reference": strings.Repeat("x", 512), "payment_proto": []byte{1}, "source_hash": bytes.Repeat([]byte{4}, 32), "actor_user_id": "user", "created_at": "2026-10-02T00:00:00Z"}
	purchaseInsert(t, db, "purchase_payments_v1", purchaseChanged(payment, map[string]any{"bank_account_id": "other-account"}), false)
	purchaseInsert(t, db, "purchase_payments_v1", purchaseChanged(payment, map[string]any{"amount_minor": 0.5}), false)
	purchaseInsert(t, db, "purchase_payments_v1", purchaseChanged(payment, map[string]any{"reference": strings.Repeat("x", 513)}), false)
	purchaseInsert(t, db, "purchase_payments_v1", payment, true)
	paymentAllocation := map[string]any{"payment_id": "payment", "posting_id": "posting", "organisation_id": "org", "sequence": 1, "posting_allocation_sequence": 1, "gross_minor": 550, "supplier_gst_minor": 50, "eligible_gst_minor": 50, "use_kind": 1, "bas_classification": 2, "cash_flow_classification": 1, "allocation_proto": []byte{1}}
	purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{"posting_id": "missing"}), false)
	purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{"posting_id": "other-posting"}), false)

	purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{"posting_allocation_sequence": 99}), false)
	for _, column := range []string{"bas_classification", "cash_flow_classification"} {
		t.Run("fractional payment "+column, func(t *testing.T) {
			purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{column: 1.5}), false)
		})
	}
	purchaseInsert(t, db, "purchase_payment_allocations_v1", paymentAllocation, true)
	// Independent cumulative rounding may yield supplier GST delta 0 and eligible delta 1.
	purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{"sequence": 2, "posting_allocation_sequence": 2, "gross_minor": 2, "supplier_gst_minor": 0, "eligible_gst_minor": 1}), true)
	// Payment sequence is its own identity; its posting sequence is explicit.
	purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{"sequence": 3, "posting_allocation_sequence": 2, "gross_minor": 2, "supplier_gst_minor": 0, "eligible_gst_minor": 1}), true)

	evidence := map[string]any{"id": "evidence", "organisation_id": "org", "posting_id": "posting", "document_id": "dated", "accounting_version": 3, "classification": 2, "source_document_id": "undated", "source_version": 2, "source_hash": bytes.Repeat([]byte{2}, 32), "held_date": "2026-10-02", "registration_review_confirmed": 1, "business_use_review_confirmed": 1, "evidence_review_confirmed": 1, "reason": "held", "evidence_proto": []byte{1}, "record_hash": bytes.Repeat([]byte{5}, 32), "actor_user_id": "user", "created_at": "2026-10-02T00:00:00Z"}
	purchaseInsert(t, db, "purchase_tax_evidence_v1", purchaseChanged(evidence, map[string]any{"organisation_id": "other"}), false)
	t.Run("fractional evidence classification", func(t *testing.T) {
		purchaseInsert(t, db, "purchase_tax_evidence_v1", purchaseChanged(evidence, map[string]any{"classification": 1.5}), false)
	})
	t.Run("fractional evidence source", func(t *testing.T) {
		purchaseInsert(t, db, "purchase_tax_evidence_v1", purchaseChanged(evidence, map[string]any{"source_version": 0.5}), false)
	})
	purchaseInsert(t, db, "purchase_tax_evidence_v1", evidence, true)
	event := map[string]any{"operation_key": "op1", "organisation_id": "org", "scope_kind": "POSTING", "scope_id": "posting", "posting_id": "posting", "accounting_version": 1, "command_type": "tammy.v1.PurchaseService.PostReviewedPurchase", "financial_revision": 1, "ledger_revision": 1, "tax_source_revision": 1, "settlement_revision": 0, "result_sha256": bytes.Repeat([]byte{6}, 32), "result_proto": []byte{1}, "journal_id": "purchase-journal", "created_at": "2026-10-01T00:00:00Z"}
	purchaseElection(t, db, "op1", "tammy.v1.PurchaseService.PostReviewedPurchase")
	purchaseElection(t, db, "op2", "tammy.v1.PurchaseService.RecordPurchasePayment")
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{"accounting_version": 2}), false)
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{"ledger_revision": -1}), false)
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{"ledger_revision": 1.5}), false)
	purchaseInsert(t, db, "purchase_accounting_events_v1", event, true)
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{"operation_key": "op2", "accounting_version": 3, "journal_id": "payment-journal", "command_type": "tammy.v1.PurchaseService.RecordPurchasePayment"}), false)
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{"operation_key": "op2", "accounting_version": 2, "journal_id": "payment-journal", "command_type": "tammy.v1.PurchaseService.RecordPurchasePayment"}), true)
	audit := map[string]any{"operation_key": "op1", "organisation_id": "org", "workspace_id": "workspace", "actor_user_id": "user", "session_id": "session", "command_type": "tammy.v1.PurchaseService.PostReviewedPurchase", "result_sha256": bytes.Repeat([]byte{6}, 32), "payload_proto": []byte{1}, "occurred_at": "2026-10-01T00:00:00Z"}
	purchaseInsert(t, db, "purchase_command_audit_v1", purchaseChanged(audit, map[string]any{"actor_user_id": "missing"}), false)
	purchaseInsert(t, db, "purchase_command_audit_v1", audit, true)
	legacy := map[string]any{"id": "legacy", "organisation_id": "org", "source_kind": "OPENING_PAYABLE", "opening_payable_id": "opening-payable", "source_version": 1, "source_hash": bytes.Repeat([]byte{7}, 32), "accounting_version": 1, "affected_period_start": "2026-10-01", "affected_period_end": "2026-12-31", "reporting_balance_date": "2026-12-31", "disposition": 1, "reason": "confirmed unpaid", "review_proto": []byte{1}, "record_hash": bytes.Repeat([]byte{8}, 32), "actor_user_id": "user", "created_at": "2026-10-03T00:00:00Z"}
	purchaseInsert(t, db, "purchase_legacy_reviews_v1", purchaseChanged(legacy, map[string]any{"organisation_id": "other"}), false)
	purchaseInsert(t, db, "purchase_legacy_reviews_v1", purchaseChanged(legacy, map[string]any{"id": nil}), false)
	t.Run("fractional legacy disposition", func(t *testing.T) {
		purchaseInsert(t, db, "purchase_legacy_reviews_v1", purchaseChanged(legacy, map[string]any{"disposition": 1.5}), false)
	})
	t.Run("fractional legacy claim evidence", func(t *testing.T) {
		purchaseInsert(t, db, "purchase_legacy_reviews_v1", purchaseChanged(legacy, map[string]any{"disposition": 2, "previously_claimed_period_start": "2026-04-01", "previously_claimed_period_end": "2026-06-30", "claim_evidence_source_id": "retained-claim", "claim_evidence_source_version": 0.5, "claim_evidence_source_hash": bytes.Repeat([]byte{1}, 32)}), false)
	})
	purchaseInsert(t, db, "purchase_legacy_reviews_v1", legacy, true)
	purchaseInsert(t, db, "purchase_legacy_reviews_v1", purchaseChanged(legacy, map[string]any{"id": "legacy-new", "accounting_version": 2, "reason": "new supporting facts"}), true)
	purchaseInsert(t, db, "purchase_legacy_reviews_v1", purchaseChanged(legacy, map[string]any{"id": "legacy-same-version", "reason": "contradictory"}), false)

	purchaseTestTaxHistory(t, db)
	for table, row := range map[string]map[string]any{"purchase_postings_v1": posting, "purchase_posting_allocations_v1": allocation, "purchase_payments_v1": payment, "purchase_payment_allocations_v1": paymentAllocation, "purchase_tax_evidence_v1": evidence, "purchase_accounting_events_v1": event, "purchase_command_audit_v1": audit, "purchase_legacy_reviews_v1": legacy} {
		purchaseAssertRetained(t, db, table, row)
	}
	// Owned journals cannot be relabelled or reversed through raw SQL.
	assertExecFails(t, db, `UPDATE journals SET source_kind=0 WHERE id='purchase-journal'`)
	assertExecFails(t, db, `INSERT INTO journals(id,organisation_id,source_type,source_id,source_revision,state,journal_date,description,reversal_of_journal_id,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at) VALUES('reverse-purchase','org','REVERSAL','reverse',1,'DRAFT','2026-10-03','reverse','purchase-journal',1100,1100,'AUD',2,'2026-10-03T00:00:00Z')`)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO purchase_posting_allocations_v1 SELECT posting_id,organisation_id,99,account_id,account_version,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,noncreditable_reason,allocation_proto FROM purchase_posting_allocations_v1 WHERE sequence=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE purchase_postings_v1 SET gross_minor=0`); err == nil {
		t.Fatal("immutable update admitted")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM purchase_posting_allocations_v1 WHERE sequence=99`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rollback retained allocation")
	}
	mustExec(t, db, `UPDATE accounts SET name='Updated expense name',version=2 WHERE id='expense'`)
	if err := migrationRelationalIntegrityCheck(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func purchaseChanged(row, changes map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range row {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}
func purchaseInsert(t *testing.T, db *Database, table string, row map[string]any, want bool) {
	t.Helper()
	q, args := purchaseInsertSQL(table, row, "INSERT")
	if !want {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, insertErr := tx.ExecContext(context.Background(), q, args...)
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if insertErr == nil {
			t.Fatalf("%s admitted rejected fixture", table)
		}
		return
	}
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("%s insert failed: %v", table, err)
	}
}
func purchaseInsertSQL(table string, row map[string]any, verb string) (string, []any) {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]any, len(keys))
	marks := make([]string, len(keys))
	for i, k := range keys {
		args[i] = row[k]
		marks[i] = "?"
	}
	return verb + " INTO " + table + "(" + strings.Join(keys, ",") + ") VALUES(" + strings.Join(marks, ",") + ")", args
}
func purchaseAssertRetained(t *testing.T, db *Database, table string, row map[string]any) {
	t.Helper()
	for _, q := range []string{"UPDATE " + table + " SET organisation_id=organisation_id", "DELETE FROM " + table} {
		if _, err := db.ExecContext(context.Background(), q); err == nil {
			t.Fatalf("history mutable: %s", q)
		}
	}
	q, args := purchaseInsertSQL(table, row, "INSERT OR REPLACE")
	if _, err := db.ExecContext(context.Background(), q, args...); err == nil {
		t.Fatalf("replace admitted: %s", table)
	}
}
func purchaseElection(t *testing.T, db *Database, key, rpc string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `INSERT INTO command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,semantic_hash_version,request_type,normalized_hash,outcome,created_at) VALUES('workspace','user',?,?,'v1','request',?,'ELECTED','2026-10-01T00:00:00Z')`, rpc, key, bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
}
func purchaseJournal(t *testing.T, db *Database, id, kind string, sourceKind int, source string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `INSERT INTO journals(id,organisation_id,source_type,source_kind,source_id,source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at) VALUES(?,'org',?,?,?,1,'DRAFT','2026-10-01','source',1100,1100,'AUD',1,'2026-10-01T00:00:00Z')`, id, kind, sourceKind, source)
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range []string{"expense", "bank"} {
		debit, credit := 1100, 0
		if i == 1 {
			debit, credit = 0, 1100
		}
		_, err = db.ExecContext(context.Background(), `INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,?,?,?,?,'AUD')`, id+fmt.Sprint(i), id, i+1, a, debit, credit)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.ExecContext(context.Background(), `UPDATE journals SET state='POSTED',posted_at='2026-10-01T00:00:00Z' WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
}
func purchaseQueryHash(t *testing.T, db *Database, q string) [32]byte {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for rows.Next() {
		values := make([]any, len(cols))
		targets := make([]any, len(cols))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			fmt.Fprintf(h, "%T:%v;", v, v)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func purchaseSeedHistorical(t *testing.T, db *Database) {
	t.Helper()
	mustExec(t, db, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES('user','user@example.test','User','ACTIVE','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO organisations(id,legal_name,status,created_at,gst_basis) VALUES('org','Organisation','ACTIVE','2026-10-01T00:00:00Z',2)`)
	for _, a := range []string{"expense", "bank"} {
		mustExec(t, db, fmt.Sprintf(`INSERT INTO accounts(id,organisation_id,code,name,account_type,normal_balance,status,designation,created_at) VALUES('%s','org','%s','Account','EXPENSE','DEBIT','ACTIVE','ORDINARY','2026-10-01T00:00:00Z')`, a, a))
	}
	for i, id := range []string{"dated", "undated", "duplicate"} {
		date := any("2026-10-01")
		if id == "undated" {
			date = nil
		}
		_, err := db.ExecContext(context.Background(), `INSERT INTO documents(id,operation_key,organisation_id,version,status,source_display_name,mime_type,byte_length,sha256,original_bytes,extracted_text,supplier_name,invoice_number,document_date,subtotal_minor,gst_minor,total_minor,created_by_user_id,created_at,reviewed_at) VALUES(?,?,'org',2,'REVIEWED','source.pdf','application/pdf',1,?,X'01','','Supplier','INV',?,1000,100,1100,'user','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, id, id, bytes.Repeat([]byte{byte(i + 1)}, 32), date)
		if err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, db, `INSERT INTO opening_conversions(id,organisation_id,conversion_date,state,source_sha256,created_at) VALUES('opening','org','2026-10-01','DRAFT','0000000000000000000000000000000000000000000000000000000000000000','2026-10-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO purchase_opening_payables(id,conversion_id,retained_input_proto,created_at) VALUES('opening-payable','opening',X'0102','2026-10-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO invoices_v1(id,organisation_id,version,state,number,invoice_proto,created_at,updated_at) VALUES('invoice','org',1,'ISSUED','INV-1',X'0102','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`)
	for _, j := range []string{"invoice-journal", "invoice-payment-journal"} {
		mustExec(t, db, fmt.Sprintf(`INSERT INTO journals(id,organisation_id,source_type,source_id,source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at) VALUES('%s','org','RECEIVABLE','%s',1,'DRAFT','2026-10-01','invoice',1100,1100,'AUD',1,'2026-10-01T00:00:00Z')`, j, j))
		for i, a := range []string{"expense", "bank"} {
			debit, credit := 1100, 0
			if i == 1 {
				debit, credit = 0, 1100
			}
			mustExec(t, db, fmt.Sprintf(`INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES('%s%d','%s',%d,'%s',%d,%d,'AUD')`, j, i, j, i+1, a, debit, credit))
		}
		mustExec(t, db, fmt.Sprintf(`INSERT INTO tax_facts(id,organisation_id,journal_line_id,tax_code,treatment,original_gross_minor,original_net_minor,original_gst_minor,attributed_gross_minor,attributed_net_minor,attributed_gst_minor,remaining_gross_minor,remaining_net_minor,remaining_gst_minor,tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,source_type,source_id,source_revision,created_at) VALUES('%s-tax','org','%s0','GST','TAXABLE',1100,1000,100,1100,1000,100,1100,1000,100,'GST','rule',1,zeroblob(32),'RECEIVABLE','invoice',1,'2026-10-01T00:00:00Z')`, j, j))
		mustExec(t, db, fmt.Sprintf(`INSERT INTO cash_flow_facts(id,organisation_id,journal_line_id,sequence,category,amount_minor,source_revision,created_at) VALUES('%s-cash','org','%s1',1,'OPERATING',-1100,1,'2026-10-01T00:00:00Z')`, j, j))
		mustExec(t, db, fmt.Sprintf(`UPDATE journals SET state='POSTED',posted_at='2026-10-01T00:00:00Z' WHERE id='%s'`, j))
	}
	mustExec(t, db, `INSERT INTO invoice_income_postings_v1(invoice_id,organisation_id,invoice_version,invoice_date,gst_basis,revenue_account_id,journal_id,invoice_net_minor,invoice_gst_minor,invoice_total_minor,bas_sales_gross_minor,profile_version,basis_audit_sequence,financial_revision,source_hash,created_at) VALUES('invoice','org',1,'2026-10-01',2,'expense','invoice-journal',1000,100,1100,1100,1,0,1,zeroblob(32),'2026-10-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO invoice_payment_v1(id,organisation_id,invoice_id,payment_date,amount_minor,allocated_gst_minor,bas_sales_gross_minor,bank_account_id,journal_id,reference,payment_proto,source_hash,created_at) VALUES('invoice-payment','org','invoice','2026-10-02',550,50,550,'bank','invoice-payment-journal','paid',X'0102',zeroblob(32),'2026-10-02T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO bas_workpapers(id,operation_key,organisation_id,version,period_start,period_end,status,sales_g1_minor,gst_on_sales_1a_minor,gst_credits_1b_minor,net_gst_payable_minor,created_at,gst_basis,source_fingerprint) VALUES('paper','paper-op','org',1,'2026-10-01','2026-12-31','DRAFT_NOT_LODGED',1100,100,100,0,'2026-10-01T00:00:00Z',2,zeroblob(32))`)
	mustExec(t, db, `INSERT INTO bas_workpaper_sources(workpaper_id,sequence,document_id,document_version,supplier_name,invoice_number,document_date,gross_minor,gst_credit_minor) VALUES('paper',1,'dated',2,'Supplier','INV','2026-10-01',1100,100)`)
	mustExec(t, db, `INSERT INTO bas_workpaper_sales_sources_v1(workpaper_id,sequence,invoice_id,invoice_number,customer_name,tax_date,gross_minor,gst_minor,source_revision,source_hash) VALUES('paper',1,'invoice','INV-1','Customer','2026-10-01',1100,100,1,zeroblob(32))`)
}

func purchaseTestTaxHistory(t *testing.T, db *Database) {
	t.Helper()
	purchaseElection(t, db, "op3", "tammy.v1.PurchaseService.RecordPurchaseTaxEvidence")
	purchaseElection(t, db, "op4", "tammy.v1.PurchaseService.AttributePurchaseGst")
	event := map[string]any{"operation_key": "op3", "organisation_id": "org", "scope_kind": "POSTING", "scope_id": "posting", "posting_id": "posting", "accounting_version": 3, "command_type": "tammy.v1.PurchaseService.RecordPurchaseTaxEvidence", "financial_revision": 3, "ledger_revision": 3, "tax_source_revision": 3, "settlement_revision": 1, "result_sha256": bytes.Repeat([]byte{6}, 32), "result_proto": []byte{1}, "created_at": "2026-10-03T00:00:00Z"}
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{"journal_id": "payment-journal"}), false)
	purchaseInsert(t, db, "purchase_accounting_events_v1", event, true)
	purchaseJournal(t, db, "attribution-journal", "TAX", 8, "op4")
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{"operation_key": "op4", "accounting_version": 4, "command_type": "tammy.v1.PurchaseService.AttributePurchaseGst", "journal_id": "attribution-journal"}), true)
	attribution := map[string]any{"id": "claim1", "operation_key": "op4", "organisation_id": "org", "posting_id": "posting", "document_id": "dated", "accounting_version": 4, "source_kind": "POSTING", "allocation_sequence": 1, "claimed_gst_minor": 25, "currency_code": "AUD", "claim_period_start": "2026-10-01", "claim_period_end": "2026-12-31", "release_posting_date": "2026-10-03", "normal_event_date": "2026-10-01", "earliest_period_start": "2026-10-01", "earliest_period_end": "2026-12-31", "required_return_due_date": "2027-02-28", "due_date_source_id": "dated", "due_date_source_version": 2, "due_date_source_hash": bytes.Repeat([]byte{1}, 32), "ordinary_last_claim_date": "2031-02-28", "evidence_id": "evidence", "evidence_held_date": "2026-10-02", "reviewed_as_of": "2026-10-03", "timing_reason": "confirmed", "journal_id": "attribution-journal", "financial_revision": 4, "source_hash": bytes.Repeat([]byte{9}, 32), "attribution_proto": []byte{1}, "actor_user_id": "user", "created_at": "2026-10-03T00:00:00Z"}
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(attribution, map[string]any{"organisation_id": "other"}), false)
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(attribution, map[string]any{"evidence_id": "missing"}), false)
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(attribution, map[string]any{"journal_id": "payment-journal"}), false)
	paymentAttribution := purchaseChanged(attribution, map[string]any{"id": "payment-claim", "source_kind": "PAYMENT", "payment_id": "payment", "allocation_sequence": 3})
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(paymentAttribution, map[string]any{"source_kind": "POSTING", "payment_id": nil}), false)
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(paymentAttribution, map[string]any{"payment_id": "missing"}), false)
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(paymentAttribution, map[string]any{"organisation_id": "other"}), false)
	t.Run("payment attribution uses payment allocation sequence", func(t *testing.T) {
		purchaseInsert(t, db, "purchase_gst_attributions_v1", paymentAttribution, true)
	})
	purchaseInsert(t, db, "purchase_gst_attributions_v1", attribution, true)
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(attribution, map[string]any{"id": "claim2", "allocation_sequence": 2}), true)
	purchaseInsert(t, db, "purchase_gst_attributions_v1", purchaseChanged(attribution, map[string]any{"id": "duplicate-claim"}), false)
	purchaseAssertRetained(t, db, "purchase_gst_attributions_v1", attribution)
	deferral := map[string]any{"id": "defer", "organisation_id": "org", "posting_id": "posting", "document_id": "dated", "accounting_version": 5, "affected_period_start": "2026-10-01", "affected_period_end": "2026-12-31", "reviewed_as_of": "2026-10-03", "timing_reason": "future period", "deferred_gst_minor": 50, "currency_code": "AUD", "ordinary_last_claim_date": "2031-02-28", "reason": "deferred", "deferral_proto": []byte{1}, "source_hash": bytes.Repeat([]byte{10}, 32), "actor_user_id": "user", "created_at": "2026-10-03T00:00:00Z"}
	purchaseInsert(t, db, "purchase_gst_deferrals_v1", deferral, true)
	portion := map[string]any{"deferral_id": "defer", "sequence": 1, "organisation_id": "org", "posting_id": "posting", "source_kind": "PAYMENT", "payment_id": "payment", "allocation_sequence": 1, "deferred_gst_minor": 50, "normal_event_date": "2026-10-02", "earliest_period_start": "2026-10-01", "earliest_period_end": "2026-12-31", "required_return_due_date": "2027-02-28", "due_date_source_id": "dated", "due_date_source_version": 2, "due_date_source_hash": bytes.Repeat([]byte{1}, 32), "ordinary_last_claim_date": "2031-02-28", "evidence_id": "evidence", "evidence_held_date": "2026-10-02", "portion_proto": []byte{1}, "source_hash": bytes.Repeat([]byte{11}, 32)}
	purchaseInsert(t, db, "purchase_gst_deferral_portions_v1", purchaseChanged(portion, map[string]any{"payment_id": "invoice-payment"}), false)
	paymentPortion := purchaseChanged(portion, map[string]any{"sequence": 2, "allocation_sequence": 3})
	purchaseInsert(t, db, "purchase_gst_deferral_portions_v1", purchaseChanged(paymentPortion, map[string]any{"source_kind": "POSTING", "payment_id": nil}), false)
	purchaseInsert(t, db, "purchase_gst_deferral_portions_v1", purchaseChanged(paymentPortion, map[string]any{"payment_id": "missing"}), false)
	purchaseInsert(t, db, "purchase_gst_deferral_portions_v1", purchaseChanged(paymentPortion, map[string]any{"organisation_id": "other"}), false)
	t.Run("payment deferral uses payment allocation sequence", func(t *testing.T) {
		purchaseInsert(t, db, "purchase_gst_deferral_portions_v1", paymentPortion, true)
	})
	purchaseInsert(t, db, "purchase_gst_deferral_portions_v1", portion, true)
	purchaseAssertRetained(t, db, "purchase_gst_deferrals_v1", deferral)
	purchaseAssertRetained(t, db, "purchase_gst_deferral_portions_v1", portion)
	source := map[string]any{"workpaper_id": "paper", "sequence": 1, "organisation_id": "org", "posting_id": "posting", "document_id": "dated", "allocation_sequence": 1, "claim_period_start": "2026-10-01", "claim_period_end": "2026-12-31", "gross_minor": 1100, "supplier_gst_minor": 100, "eligible_gst_minor": 100, "claimed_gst_minor": 25, "currency_code": "AUD", "source_revision": 4, "source_hash": bytes.Repeat([]byte{12}, 32), "snapshot_proto": []byte{1}}
	purchaseInsert(t, db, "bas_workpaper_purchase_sources_v1", purchaseChanged(source, map[string]any{"organisation_id": "other"}), false)
	purchaseInsert(t, db, "bas_workpaper_purchase_sources_v1", source, true)
	purchaseInsert(t, db, "bas_workpaper_purchase_sources_v1", purchaseChanged(source, map[string]any{"sequence": 3, "payment_id": "payment", "allocation_sequence": 2, "gross_minor": 2, "supplier_gst_minor": 0, "eligible_gst_minor": 1, "claimed_gst_minor": 1}), true)
	purchaseInsert(t, db, "bas_workpaper_purchase_sources_v1", purchaseChanged(source, map[string]any{"sequence": 4, "attribution_id": "claim1", "allocation_sequence": 2}), false)
	purchaseInsert(t, db, "bas_workpaper_purchase_sources_v1", purchaseChanged(source, map[string]any{"sequence": 5, "posting_id": "other-posting", "document_id": "duplicate", "attribution_id": "claim1"}), false)

	purchaseInsert(t, db, "bas_workpaper_purchase_sources_v1", purchaseChanged(source, map[string]any{"sequence": 2, "allocation_sequence": 2}), true)
	review := map[string]any{"workpaper_id": "paper", "sequence": 1, "organisation_id": "org", "kind": 11, "status": 4, "source_kind": "OPENING_PAYABLE", "opening_payable_id": "opening-payable", "affected_period_start": "2026-10-01", "affected_period_end": "2026-12-31", "eligible_gst_minor": 0, "unclaimed_gst_minor": 0, "currency_code": "AUD", "legacy_review_id": "legacy", "source_revision": 1, "source_hash": bytes.Repeat([]byte{13}, 32), "snapshot_proto": []byte{1}}
	t.Run("fractional saved review kind", func(t *testing.T) {
		purchaseInsert(t, db, "bas_workpaper_purchase_reviews_v1", purchaseChanged(review, map[string]any{"kind": 1.5}), false)
	})
	t.Run("fractional saved review status", func(t *testing.T) {
		purchaseInsert(t, db, "bas_workpaper_purchase_reviews_v1", purchaseChanged(review, map[string]any{"status": 1.5}), false)
	})
	purchaseInsert(t, db, "bas_workpaper_purchase_reviews_v1", review, true)
	support := map[string]any{"workpaper_id": "paper", "organisation_id": "org", "review_id": "legacy", "kind": 1, "sequence": 1, "source_id": "dated", "source_version": 2, "source_hash": bytes.Repeat([]byte{1}, 32), "snapshot_proto": []byte{1}}
	purchaseInsert(t, db, "bas_workpaper_purchase_support_v1", purchaseChanged(support, map[string]any{"review_id": "missing"}), false)
	purchaseInsert(t, db, "bas_workpaper_purchase_support_v1", support, true)
	purchaseAssertRetained(t, db, "bas_workpaper_purchase_sources_v1", source)
	purchaseAssertRetained(t, db, "bas_workpaper_purchase_reviews_v1", review)
	purchaseAssertRetained(t, db, "bas_workpaper_purchase_support_v1", support)
}

func TestPurchaseFailedMigrationRollsBackSchemaAndRetainedRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "purchase-migration-rollback.db"), testKey(0x84))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	steps := allMigrations(t)
	if err := applyMigrations(ctx, db, steps, 11); err != nil {
		t.Fatal(err)
	}
	purchaseSeedHistorical(t, db)
	before := purchaseQueryHash(t, db, `SELECT * FROM documents ORDER BY id`)
	steps[11].SQL = append(steps[11].SQL, []byte("\nSELECT * FROM deliberately_missing_purchase_migration_probe;\n")...)
	digest := sha256.Sum256(steps[11].SQL)
	steps[11].SHA256 = fmt.Sprintf("%x", digest)
	if err := applyMigrations(ctx, db, steps, 12); err == nil {
		t.Fatal("failed purchase migration committed")
	}
	assertAppliedVersions(t, db, 11)
	assertTablesAbsent(t, db, purchaseHistoryTables)
	if got := purchaseQueryHash(t, db, `SELECT * FROM documents ORDER BY id`); got != before {
		t.Fatal("failed migration changed reviewed documents")
	}
	rows, err := db.QueryContext(ctx, `SELECT source_kind FROM journals LIMIT 0`)
	if err == nil {
		rows.Close()
		t.Fatal("failed migration retained the journal discriminator")
	}
	if err := migrationRelationalIntegrityCheck(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseSavedEmptySnapshotRetainsMetadata(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "purchase-empty-snapshot.db")
	db, err := Open(ctx, path, testKey(0x85))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := applyMigrations(ctx, db, allMigrations(t), 12); err != nil {
		t.Fatal(err)
	}
	purchaseSeedHistorical(t, db)
	mustExec(t, db, `INSERT INTO bas_workpapers(id,operation_key,organisation_id,version,period_start,period_end,status,sales_g1_minor,gst_on_sales_1a_minor,gst_credits_1b_minor,net_gst_payable_minor,created_at) VALUES('empty-paper','empty-operation','org',1,'2027-01-01','2027-03-31','DRAFT_NOT_LODGED',0,0,0,0,'2027-04-01T00:00:00Z')`)
	mustExec(t, db, `DROP TRIGGER organisations_singleton_insert`)
	mustExec(t, db, `INSERT INTO organisations(id,legal_name,status,created_at) VALUES('other','Other','INACTIVE','2026-10-01T00:00:00Z')`)
	mustExec(t, db, `CREATE TRIGGER organisations_singleton_insert BEFORE INSERT ON organisations WHEN EXISTS(SELECT 1 FROM organisations) BEGIN SELECT RAISE(ABORT,'workspace already has an organisation'); END`)
	metadata := map[string]any{"workpaper_id": "empty-paper", "organisation_id": "org", "purchase_source_format": "RECONCILED_V1", "legacy_candidate_gst_minor": 0, "currency_code": "AUD"}
	// Test each constraint before inserting the valid row so duplicate protection
	// cannot mask an invalid owner, amount, format or currency.
	for name, changes := range map[string]map[string]any{"wrong owner": {"organisation_id": "other"}, "blank format": {"purchase_source_format": ""}, "unknown format": {"purchase_source_format": "LEGACY"}, "negative candidate": {"legacy_candidate_gst_minor": -1}, "fractional candidate": {"legacy_candidate_gst_minor": 1.5}, "wrong currency": {"currency_code": "USD"}} {
		t.Run(name, func(t *testing.T) {
			purchaseInsert(t, db, "bas_workpaper_purchase_metadata_v1", purchaseChanged(metadata, changes), false)
		})
	}
	purchaseInsert(t, db, "bas_workpaper_purchase_metadata_v1", metadata, true)
	var format, currency string
	var candidate int64
	if err := db.QueryRowContext(ctx, `SELECT purchase_source_format,legacy_candidate_gst_minor,currency_code FROM bas_workpaper_purchase_metadata_v1 WHERE workpaper_id='empty-paper' AND organisation_id='org'`).Scan(&format, &candidate, &currency); err != nil || format != "RECONCILED_V1" || candidate != 0 || currency != "AUD" {
		t.Fatalf("empty snapshot metadata: %s %d %s %v", format, candidate, currency, err)
	}
	for _, table := range []string{"bas_workpaper_purchase_sources_v1", "bas_workpaper_purchase_reviews_v1", "bas_workpaper_purchase_support_v1"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE workpaper_id='empty-paper'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("empty snapshot %s: %d %v", table, count, err)
		}
	}
	var legacy int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM bas_workpaper_purchase_metadata_v1 WHERE workpaper_id='paper'`).Scan(&legacy); err != nil || legacy != 0 {
		t.Fatalf("legacy metadata backfill: %d %v", legacy, err)
	}
	purchaseAssertRetained(t, db, "bas_workpaper_purchase_metadata_v1", metadata)
	if err := db.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path, testKey(0x85))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx, `SELECT purchase_source_format,legacy_candidate_gst_minor,currency_code FROM bas_workpaper_purchase_metadata_v1 WHERE workpaper_id='empty-paper'`).Scan(&format, &candidate, &currency); err != nil || format != "RECONCILED_V1" || candidate != 0 || currency != "AUD" {
		t.Fatalf("reopened empty metadata: %s %d %s %v", format, candidate, currency, err)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrationRelationalIntegrityCheck(ctx, db); err != nil {
		t.Fatal(err)
	}
}
