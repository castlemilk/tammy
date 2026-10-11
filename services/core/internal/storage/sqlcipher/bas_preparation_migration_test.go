//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const basTaxFactsV12Columns = `id,organisation_id,journal_line_id,tax_code,treatment,
	original_gross_minor,original_net_minor,original_gst_minor,
	attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
	remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
	tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
	source_type,source_id,source_revision,created_at`

func TestBasPreparationMigration13PreservesVersion12BusinessHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bas-preparation-upgrade.db")
	key := testKey(0x91)
	if _, err := MigrateWorkspace(ctx, path, key, 12); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	purchaseSeedHistorical(t, db)
	seedBasMigration12PurchaseHistory(t, db)
	before := snapshotBasPre13Tables(t, db)
	beforeLedger := purchaseQueryHash(t, db, `SELECT version,name,sha256 FROM schema_migrations WHERE version<=12 ORDER BY version`)
	var oldTaxFacts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tax_facts`).Scan(&oldTaxFacts); err != nil {
		t.Fatal(err)
	}
	if oldTaxFacts == 0 {
		t.Fatal("fixture must contain pre-existing tax facts")
	}
	for table, want := range map[string]int{
		"purchase_postings_v1":               1,
		"purchase_payments_v1":               1,
		"purchase_tax_evidence_v1":           1,
		"purchase_gst_attributions_v1":       3,
		"purchase_legacy_reviews_v1":         2,
		"bas_workpaper_purchase_sources_v1":  3,
		"bas_workpaper_purchase_reviews_v1":  1,
		"bas_workpaper_purchase_support_v1":  1,
		"bas_workpaper_purchase_metadata_v1": 0,
	} {
		var got int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("fixture %s rows = %d, want %d", table, got, want)
		}
	}
	if err := db.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateWorkspace(ctx, path, key, 13); err != nil {
		t.Fatalf("BAS preparation migration 13 failed: %v", err)
	}
	db, err = Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	after := snapshotBasPre13Tables(t, db)
	for table, want := range before {
		if got, ok := after[table]; !ok || got != want {
			t.Fatalf("migration changed pre-existing %s rows (present=%t)", table, ok)
		}
	}
	if got := purchaseQueryHash(t, db, `SELECT version,name,sha256 FROM schema_migrations WHERE version<=12 ORDER BY version`); got != beforeLedger {
		t.Fatal("migration changed the accepted migration 1-12 ledger")
	}
	var gotTaxFacts, populatedOldHashes int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tax_facts`).Scan(&gotTaxFacts); err != nil {
		t.Fatal(err)
	}
	if gotTaxFacts != oldTaxFacts {
		t.Fatalf("tax fact count = %d, want %d", gotTaxFacts, oldTaxFacts)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tax_facts WHERE source_content_hash IS NOT NULL`).Scan(&populatedOldHashes); err != nil {
		t.Fatal(err)
	}
	if populatedOldHashes != 0 {
		t.Fatalf("migration invented source hashes for %d historical tax facts", populatedOldHashes)
	}
	assertAppliedVersions(t, db, 13)
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrationRelationalIntegrityCheck(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func seedBasMigration12PurchaseHistory(t *testing.T, db *Database) {
	t.Helper()
	posting := map[string]any{
		"id": "posting", "organisation_id": "org", "document_id": "dated", "reviewed_version": 2,
		"original_content_hash": bytes.Repeat([]byte{1}, 32), "reviewed_snapshot_hash": bytes.Repeat([]byte{2}, 32),
		"reviewed_source_proto": []byte{1}, "supplier_name": "Supplier", "supplier_identity": "supplier",
		"invoice_number": "INV", "invoice_date": "2026-10-01", "gst_basis": 2, "gst_profile_version": 1,
		"gst_history_watermark": 0, "gross_minor": 1100, "supplier_gst_minor": 100, "eligible_gst_minor": 100,
		"currency_code": "AUD", "journal_id": "purchase-journal", "financial_revision": 1, "duplicate_decision": 1,
		"duplicate_reason": "", "prior_accounting_decision": 1, "prior_accounting_reason": "Not accounted",
		"normalized_supplier": "supplier", "normalized_invoice": "inv", "source_hash": bytes.Repeat([]byte{3}, 32),
		"posting_proto": []byte{1}, "actor_user_id": "user", "created_at": "2026-10-01T00:00:00Z",
	}
	purchaseJournal(t, db, "purchase-journal", "PAYABLE", 6, "posting")
	purchaseInsert(t, db, "purchase_postings_v1", posting, true)
	allocation := map[string]any{
		"posting_id": "posting", "organisation_id": "org", "sequence": 1, "account_id": "expense", "account_version": 1,
		"gross_minor": 1100, "supplier_gst_minor": 100, "eligible_gst_minor": 100, "use_kind": 1,
		"bas_classification": 2, "cash_flow_classification": 1, "noncreditable_reason": "", "allocation_proto": []byte{1},
	}
	purchaseInsert(t, db, "purchase_posting_allocations_v1", allocation, true)
	purchaseInsert(t, db, "purchase_posting_allocations_v1", purchaseChanged(allocation, map[string]any{"sequence": 2}), true)
	purchaseJournal(t, db, "payment-journal", "BANKING", 7, "payment")
	payment := map[string]any{
		"id": "payment", "posting_id": "posting", "organisation_id": "org", "accounting_version": 2,
		"payment_date": "2026-10-02", "amount_minor": 550, "currency_code": "AUD", "bank_account_id": "bank",
		"journal_id": "payment-journal", "reference": "partial payment", "payment_proto": []byte{1},
		"source_hash": bytes.Repeat([]byte{4}, 32), "actor_user_id": "user", "created_at": "2026-10-02T00:00:00Z",
	}
	purchaseInsert(t, db, "purchase_payments_v1", payment, true)
	paymentAllocation := map[string]any{
		"payment_id": "payment", "posting_id": "posting", "organisation_id": "org", "sequence": 1,
		"posting_allocation_sequence": 1, "gross_minor": 550, "supplier_gst_minor": 50, "eligible_gst_minor": 50,
		"use_kind": 1, "bas_classification": 2, "cash_flow_classification": 1, "allocation_proto": []byte{1},
	}
	purchaseInsert(t, db, "purchase_payment_allocations_v1", paymentAllocation, true)
	purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{
		"sequence": 2, "posting_allocation_sequence": 2, "gross_minor": 2, "supplier_gst_minor": 0, "eligible_gst_minor": 1,
	}), true)
	purchaseInsert(t, db, "purchase_payment_allocations_v1", purchaseChanged(paymentAllocation, map[string]any{
		"sequence": 3, "posting_allocation_sequence": 2, "gross_minor": 2, "supplier_gst_minor": 0, "eligible_gst_minor": 1,
	}), true)
	evidence := map[string]any{
		"id": "evidence", "organisation_id": "org", "posting_id": "posting", "document_id": "dated",
		"accounting_version": 3, "classification": 2, "source_document_id": "undated", "source_version": 2,
		"source_hash": bytes.Repeat([]byte{2}, 32), "held_date": "2026-10-02", "registration_review_confirmed": 1,
		"business_use_review_confirmed": 1, "evidence_review_confirmed": 1, "reason": "held",
		"evidence_proto": []byte{1}, "record_hash": bytes.Repeat([]byte{5}, 32), "actor_user_id": "user",
		"created_at": "2026-10-02T00:00:00Z",
	}
	purchaseInsert(t, db, "purchase_tax_evidence_v1", evidence, true)
	legacy := map[string]any{
		"id": "legacy", "organisation_id": "org", "source_kind": "OPENING_PAYABLE", "opening_payable_id": "opening-payable",
		"source_version": 1, "source_hash": bytes.Repeat([]byte{7}, 32), "accounting_version": 1,
		"affected_period_start": "2026-10-01", "affected_period_end": "2026-12-31", "reporting_balance_date": "2026-12-31",
		"disposition": 1, "reason": "confirmed unpaid", "review_proto": []byte{1}, "record_hash": bytes.Repeat([]byte{8}, 32),
		"actor_user_id": "user", "created_at": "2026-10-03T00:00:00Z",
	}
	purchaseInsert(t, db, "purchase_legacy_reviews_v1", legacy, true)
	purchaseInsert(t, db, "purchase_legacy_reviews_v1", purchaseChanged(legacy, map[string]any{
		"id": "legacy-new", "accounting_version": 2, "reason": "new supporting facts",
	}), true)
	// Seed the accepted posting and payment accounting events before the
	// evidence and attribution helper appends versions 3 and 4.
	event := map[string]any{
		"operation_key": "op1", "organisation_id": "org", "scope_kind": "POSTING", "scope_id": "posting",
		"posting_id": "posting", "accounting_version": 1,
		"command_type":       "tammy.v1.PurchaseService.PostReviewedPurchase",
		"financial_revision": 1, "ledger_revision": 1, "tax_source_revision": 1, "settlement_revision": 0,
		"result_sha256": bytes.Repeat([]byte{6}, 32), "result_proto": []byte{1},
		"journal_id": "purchase-journal", "created_at": "2026-10-01T00:00:00Z",
	}
	purchaseElection(t, db, "op1", "tammy.v1.PurchaseService.PostReviewedPurchase")
	purchaseElection(t, db, "op2", "tammy.v1.PurchaseService.RecordPurchasePayment")
	purchaseInsert(t, db, "purchase_accounting_events_v1", event, true)
	purchaseInsert(t, db, "purchase_accounting_events_v1", purchaseChanged(event, map[string]any{
		"operation_key": "op2", "accounting_version": 2,
		"command_type": "tammy.v1.PurchaseService.RecordPurchasePayment", "journal_id": "payment-journal",
	}), true)
	audit := map[string]any{
		"operation_key": "op1", "organisation_id": "org", "workspace_id": "workspace",
		"actor_user_id": "user", "session_id": "session",
		"command_type":  "tammy.v1.PurchaseService.PostReviewedPurchase",
		"result_sha256": bytes.Repeat([]byte{6}, 32), "payload_proto": []byte{1},
		"occurred_at": "2026-10-01T00:00:00Z",
	}
	purchaseInsert(t, db, "purchase_command_audit_v1", audit, true)
	purchaseInsert(t, db, "purchase_command_audit_v1", purchaseChanged(audit, map[string]any{
		"operation_key": "op2", "command_type": "tammy.v1.PurchaseService.RecordPurchasePayment",
		"occurred_at": "2026-10-02T00:00:00Z",
	}), true)
	purchaseTestTaxHistory(t, db)
}

func snapshotBasPre13Tables(t *testing.T, db *Database) map[string][32]byte {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name<>'schema_migrations' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	out := make(map[string][32]byte, len(tables))
	for _, table := range tables {
		query := `SELECT * FROM ` + quoteBasSQLIdentifier(table) + ` ORDER BY rowid`
		if table == "tax_facts" {
			query = `SELECT ` + basTaxFactsV12Columns + ` FROM tax_facts ORDER BY rowid`
		}
		out[table] = purchaseQueryHash(t, db, query)
	}
	return out
}

func quoteBasSQLIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func TestBasPreparationSchemaRetainsScopedSnapshotAndSharedEventHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "bas-preparation-constraints.db"), testKey(0x92))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := applyMigrations(ctx, db, allMigrations(t), 13); err != nil {
		t.Fatalf("BAS preparation migration missing: %v", err)
	}
	assertBasLifecycleTablesEmpty(t, db)
	purchaseSeedHistorical(t, db)
	assertBasTaxFactSourceHashShape(t, db)
	addBasTestSecondOrganisation(t, db)
	seedBasLifecycleRows(t, db)
	assertBasTypedRetentionRows(t, db)
	assertBasLifecycleHistoricalRowsImmutable(t, db)
	if err := migrationRelationalIntegrityCheck(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func assertBasLifecycleTablesEmpty(t *testing.T, db *Database) {
	t.Helper()
	for _, table := range []string{
		"bas_profiles_v1", "bas_profile_versions_v1", "bas_profile_heads_v1",
		"bas_periods_v1", "bas_period_versions_v1", "bas_period_heads_v1",
		"bas_family_identities_v1", "bas_family_state_v1",
		"bas_preparation_identities_v1", "bas_preparation_versions_v1", "bas_preparation_heads_v1",
		"bas_branch_identities_v1", "bas_branch_versions_v1", "bas_branch_heads_v1", "bas_family_events_v1",
		"bas_payg_input_versions_v1", "bas_calculations_v1", "bas_calculation_coverage_v1",
		"bas_validations_v1", "bas_validation_findings_v1", "bas_warning_acknowledgements_v1",
		"bas_frozen_bas_v1", "bas_frozen_acknowledgements_v1", "bas_export_events_v1",
		"bas_external_observations_v1", "bas_external_observation_corrections_v1",
		"bas_dispute_resolutions_v1", "bas_dispute_resolution_observations_v1", "bas_dispute_resolution_preparations_v1",
	} {
		var count int
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("migration backfilled %s with %d rows", table, count)
		}
	}
}

func assertBasTaxFactSourceHashShape(t *testing.T, db *Database) {
	t.Helper()
	basExec(t, db, `INSERT INTO journals(id,organisation_id,source_type,source_kind,source_id,source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at) VALUES('hash-shape-journal','org','MANUAL',0,'hash-shape',1,'DRAFT','2026-10-01','source hash shape',1100,1100,'AUD',1,'2026-10-01T00:00:00Z')`)
	basExec(t, db, `INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES('hash-shape-line-null','hash-shape-journal',1,'expense',1100,0,'AUD')`)
	basExec(t, db, `INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES('hash-shape-line-set','hash-shape-journal',2,'bank',0,1100,'AUD')`)
	base := map[string]any{
		"organisation_id": "org", "tax_code": "GST", "treatment": "TAXABLE",
		"original_gross_minor": 1100, "original_net_minor": 1000, "original_gst_minor": 100,
		"attributed_gross_minor": 1100, "attributed_net_minor": 1000, "attributed_gst_minor": 100,
		"remaining_gross_minor": 1100, "remaining_net_minor": 1000, "remaining_gst_minor": 100,
		"tax_rule_type": "GST", "tax_rule_id": "rule", "tax_rule_revision": 1, "tax_rule_content_hash": bytes.Repeat([]byte{0x91}, 32),
		"source_type": "MANUAL", "source_id": "hash-shape", "source_revision": 1, "created_at": "2026-10-01T00:00:00Z",
	}
	nilHash := purchaseChanged(base, map[string]any{"id": "hash-shape-tax-null", "journal_line_id": "hash-shape-line-null", "source_content_hash": nil})
	purchaseInsert(t, db, "tax_facts", nilHash, true)
	invalidHash := purchaseChanged(base, map[string]any{"id": "hash-shape-tax-invalid", "journal_line_id": "hash-shape-line-set", "source_content_hash": bytes.Repeat([]byte{0x92}, 31)})
	purchaseInsert(t, db, "tax_facts", invalidHash, false)
	validHash := purchaseChanged(base, map[string]any{"id": "hash-shape-tax-valid", "journal_line_id": "hash-shape-line-set", "source_content_hash": bytes.Repeat([]byte{0x92}, 32)})
	purchaseInsert(t, db, "tax_facts", validHash, true)
}

func addBasTestSecondOrganisation(t *testing.T, db *Database) {
	t.Helper()
	mustExec(t, db, `DROP TRIGGER organisations_singleton_insert`)
	mustExec(t, db, `INSERT INTO organisations(id,legal_name,status,created_at) VALUES('other','Other','INACTIVE','2026-10-01T00:00:00Z')`)
	mustExec(t, db, `CREATE TRIGGER organisations_singleton_insert BEFORE INSERT ON organisations WHEN EXISTS(SELECT 1 FROM organisations) BEGIN SELECT RAISE(ABORT,'workspace already has an organisation'); END`)
}

type basTestSnapshotSeed struct {
	organisationID, familyID, preparationID string
	profileID, periodID                     string
	calculationID, validationID, findingID  string
	acknowledgementID, frozenID             string
	factorAssertionID, sessionID            string
	profileVersion, periodVersion           int
	profileHash, periodHash                 []byte
	inputHash, calculationHash              []byte
	validationHash, findingHash             []byte
	acknowledgementHash, frozenHash         []byte
}

func seedBasCalculationStack(t *testing.T, db *Database, seed basTestSnapshotSeed) map[string]any {
	t.Helper()
	sourceSnapshotHash := bytes.Repeat([]byte{0xa1}, 32)
	inputID := "0199c105-0000-7000-8000-000000000001"
	if seed.preparationID != "0199c101-0000-7000-8000-000000000007" {
		inputID = "0199c105-0000-7000-8000-000000000002"
	}
	inputContentHash := bytes.Repeat([]byte{seed.inputHash[0] + 1}, 32)
	prep := map[string]any{
		"preparation_id": seed.preparationID, "organisation_id": seed.organisationID, "family_id": seed.familyID,
		"version": 1, "profile_id": seed.profileID, "profile_version": seed.profileVersion, "profile_hash": seed.profileHash,
		"period_id": seed.periodID, "period_version": seed.periodVersion, "period_hash": seed.periodHash,
		"input_revision": 1, "input_hash": seed.inputHash, "dispute_overlay": "NONE", "source_snapshot_hash": sourceSnapshotHash,
		"state": "COLLECTING", "content_hash": basSeedPreparationHash(seed, 1), "preparation_proto": []byte{1},
		"created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-10-01T00:00:00Z",
	}
	var predecessorID string
	if err := db.QueryRowContext(context.Background(), `SELECT coalesce(predecessor_id,'') FROM bas_preparation_identities_v1 WHERE preparation_id=?`, seed.preparationID).Scan(&predecessorID); err != nil {
		t.Fatal(err)
	}
	if predecessorID != "" {
		prep["predecessor_id"] = predecessorID
		prep["predecessor_version"] = 6
		prep["predecessor_hash"] = bytes.Repeat([]byte{0x31}, 32)
	}
	appendBasFixturePreparation(t, db, prep)
	purchaseInsert(t, db, "bas_payg_input_versions_v1", map[string]any{
		"preparation_id": seed.preparationID, "organisation_id": seed.organisationID, "family_id": seed.familyID,
		"input_snapshot_id": inputID, "preparation_version": 1, "preparation_hash": basSeedPreparationHash(seed, 1),
		"input_revision": 1, "input_hash": seed.inputHash, "content_hash": inputContentHash, "input_proto": []byte{0xa2}, "actor_user_id": "user",
		"created_at": "2026-10-01T00:00:00Z",
	}, true)
	prep = purchaseChanged(prep, map[string]any{"version": 2, "content_hash": basSeedPreparationHash(seed, 2), "preparation_proto": []byte{2}, "input_snapshot_id": inputID, "input_content_hash": inputContentHash})
	appendBasFixturePreparation(t, db, prep)
	purchaseInsert(t, db, "bas_calculations_v1", map[string]any{
		"calculation_id": seed.calculationID, "organisation_id": seed.organisationID, "family_id": seed.familyID,
		"preparation_id": seed.preparationID, "preparation_version": 2, "preparation_hash": basSeedPreparationHash(seed, 2), "version": 1, "content_hash": seed.calculationHash,
		"input_snapshot_id": inputID, "input_content_hash": inputContentHash,
		"input_revision": 1, "input_hash": seed.inputHash,
		"profile_id": seed.profileID, "profile_version": seed.profileVersion, "profile_hash": seed.profileHash,
		"period_id": seed.periodID, "period_version": seed.periodVersion, "period_hash": seed.periodHash,
		"outcome": "COMPLETE", "source_snapshot_hash": sourceSnapshotHash,
		"coverage_manifest_hash": bytes.Repeat([]byte{0xa4}, 32), "revision_vector_hash": bytes.Repeat([]byte{0xa5}, 32),
		"rule_version": "gst-rules-v1", "rule_hash": bytes.Repeat([]byte{0xa6}, 32),
		"calculator_version": "calculator-v1", "calculator_hash": bytes.Repeat([]byte{0xa7}, 32),
		"form_version": "bas-form-v1", "form_hash": bytes.Repeat([]byte{0xa8}, 32),
		"schema_version": "schema-v1", "schema_hash": bytes.Repeat([]byte{0xa9}, 32),
		"descriptor_version": "descriptor-v1", "descriptor_hash": bytes.Repeat([]byte{0xaa}, 32),
		"organisation_snapshot_hash": bytes.Repeat([]byte{0xab}, 32),
		"result_proto":               []byte{0xac}, "workpaper_snapshot_proto": []byte{0xad},
		"revision_vector_proto": []byte{0xae}, "organisation_snapshot_proto": []byte{0xaf},
		"calculation_proto": []byte{0xb0}, "created_at": "2026-10-01T00:00:00Z",
	}, true)
	purchaseInsert(t, db, "bas_calculation_coverage_v1", map[string]any{
		"calculation_id": seed.calculationID, "organisation_id": seed.organisationID, "family_id": seed.familyID, "calculation_version": 1,
		"calculation_hash": seed.calculationHash, "sequence": 1, "candidate_kind": "INVOICE_POSTING",
		"disposition": "ELIGIBLE", "source_type": "invoice", "source_id": "invoice", "source_revision": 1,
		"source_hash": bytes.Repeat([]byte{0xb1}, 32), "eligible_amount_minor": 1100, "currency_code": "AUD",
		"tax_date": "2026-10-01", "classification_code": "GST_SALE", "reason": "reviewed invoice posting",
		"lineage_proto": []byte{0xb2}, "evidence_proto": []byte{0xb3}, "coverage_proto": []byte{0xb4},
		"content_hash": bytes.Repeat([]byte{0xb5}, 32),
	}, true)
	prep = purchaseChanged(prep, map[string]any{"version": 3, "content_hash": basSeedPreparationHash(seed, 3), "preparation_proto": []byte{3}, "calculation_id": seed.calculationID, "calculation_version": 1, "calculation_hash": seed.calculationHash})
	appendBasFixturePreparation(t, db, prep)
	purchaseInsert(t, db, "bas_validations_v1", map[string]any{
		"validation_id": seed.validationID, "organisation_id": seed.organisationID, "family_id": seed.familyID,
		"preparation_id": seed.preparationID, "preparation_version": 3, "revision": 1, "content_hash": seed.validationHash,
		"preparation_hash": basSeedPreparationHash(seed, 3), "input_snapshot_id": inputID, "input_content_hash": inputContentHash,
		"calculation_id": seed.calculationID, "calculation_version": 1, "calculation_hash": seed.calculationHash,
		"source_snapshot_hash": sourceSnapshotHash, "input_revision": 1, "input_hash": seed.inputHash,
		"validation_proto": []byte{0xb7}, "created_at": "2026-10-01T00:00:00Z",
	}, true)
	prep = purchaseChanged(prep, map[string]any{"version": 4, "content_hash": basSeedPreparationHash(seed, 4), "preparation_proto": []byte{4}, "state": "REVIEW_READY", "validation_id": seed.validationID, "validation_revision": 1, "validation_hash": seed.validationHash})
	appendBasFixturePreparation(t, db, prep)
	purchaseInsert(t, db, "bas_validation_findings_v1", map[string]any{
		"validation_id": seed.validationID, "organisation_id": seed.organisationID, "family_id": seed.familyID, "revision": 1,
		"validation_hash": seed.validationHash, "finding_id": seed.findingID, "finding_hash": seed.findingHash,
		"severity": "WARNING", "stable_code": "REVIEW_DUE_DATE", "safe_message": "Review the confirmed due date",
		"finding_proto": []byte{0xb8},
	}, true)
	purchaseInsert(t, db, "bas_warning_acknowledgements_v1", map[string]any{
		"acknowledgement_id": seed.acknowledgementID, "organisation_id": seed.organisationID,
		"family_id": seed.familyID, "preparation_id": seed.preparationID,
		"validation_id": seed.validationID, "validation_revision": 1, "validation_hash": seed.validationHash,
		"finding_id": seed.findingID, "finding_hash": seed.findingHash,
		"source_snapshot_hash": sourceSnapshotHash, "actor_user_id": "user",
		"acknowledged_at": "2026-10-01T00:00:00Z", "content_hash": seed.acknowledgementHash,
		"acknowledgement_proto": []byte{0xb9},
	}, true)
	basExec(t, db, `INSERT INTO application_sessions(id,user_id,state,created_at,last_active_at,expires_at,ended_at) VALUES(?, 'user',2,'2026-10-01T00:00:00Z','2026-10-01T00:00:00Z','2026-10-02T00:00:00Z','2026-10-01T00:01:00Z')`, seed.sessionID)
	basExec(t, db, `INSERT INTO factor_assertions(id,user_id,session_id,purpose,asserted_at,consumed) VALUES(?, 'user',?,'bas_freeze_manual_handoff','2026-10-01T00:00:00Z',0)`, seed.factorAssertionID, seed.sessionID)
	return prep
}

func seedBasFrozenPreparationVersion(t *testing.T, db *Database, frozen, preparationVersion map[string]any, factorAssertionID string) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, item := range []struct {
		table string
		row   map[string]any
	}{{"bas_frozen_bas_v1", frozen}, {"bas_preparation_versions_v1", preparationVersion}} {
		query, args := purchaseInsertSQL(item.table, item.row, "INSERT")
		if _, err := tx.ExecContext(context.Background(), query, args...); err != nil {
			t.Fatalf("%s insert failed inside freeze transaction: %v", item.table, err)
		}
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_preparation_heads_v1 SET current_version=5,current_hash=?,cas_version=5 WHERE preparation_id=? AND organisation_id=? AND current_version=4 AND cas_version=4`, preparationVersion["content_hash"], preparationVersion["preparation_id"], preparationVersion["organisation_id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE factor_assertions SET consumed=1 WHERE id=? AND user_id='user' AND consumed=0`, factorAssertionID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func basSeedPreparationHash(seed basTestSnapshotSeed, version byte) []byte {
	return bytes.Repeat([]byte{seed.frozenHash[0] + version}, 32)
}

func appendBasFixturePreparation(t *testing.T, db *Database, row map[string]any) {
	t.Helper()
	purchaseInsert(t, db, "bas_preparation_versions_v1", row, true)
	version := row["version"].(int)
	if version == 1 {
		purchaseInsert(t, db, "bas_preparation_heads_v1", map[string]any{"preparation_id": row["preparation_id"], "organisation_id": row["organisation_id"], "current_version": 1, "current_hash": row["content_hash"], "cas_version": 1, "updated_at": row["updated_at"]}, true)
	} else {
		assertBasRowsAffected(t, db, 1, `UPDATE bas_preparation_heads_v1 SET current_version=?,current_hash=?,cas_version=cas_version+1 WHERE preparation_id=? AND organisation_id=? AND current_version=? AND cas_version=?`, version, row["content_hash"], row["preparation_id"], row["organisation_id"], version-1, version-1)
	}
}

func seedBasLifecycleRows(t *testing.T, db *Database, beforeCorrectiveAdoption ...func(*Database, map[string]any)) {
	t.Helper()
	ctx := context.Background()
	const (
		profileID                = "0199c101-0000-7000-8000-000000000001"
		otherProfileID           = "0199c101-0000-7000-8000-000000000002"
		periodID                 = "0199c101-0000-7000-8000-000000000003"
		otherFamilyID            = "0199c101-0000-7000-8000-000000000004"
		familyID                 = "0199c101-0000-7000-8000-000000000005"
		newPeriodFamily          = "0199c101-0000-7000-8000-000000000006"
		preparationID            = "0199c101-0000-7000-8000-000000000007"
		branchID                 = "0199c101-0000-7000-8000-000000000008"
		rootAttemptID            = "0199c101-0000-7000-8000-000000000009"
		activeAttemptID          = "0199c101-0000-7000-8000-000000000010"
		rootFreezeID             = "0199c101-0000-7000-8000-000000000011"
		duplicateFamily          = "0199c101-0000-7000-8000-000000000016"
		customFamily             = "0199c101-0000-7000-8000-000000000017"
		attemptPrepID            = "0199c101-0000-7000-8000-000000000018"
		attemptFreezeID          = "0199c101-0000-7000-8000-000000000019"
		invalidPrepID            = "0199c101-0000-7000-8000-000000000020"
		rootCalculationID        = "0199c104-0000-7000-8000-000000000001"
		rootValidationID         = "0199c104-0000-7000-8000-000000000002"
		rootFindingID            = "0199c104-0000-7000-8000-000000000003"
		rootAcknowledgementID    = "0199c104-0000-7000-8000-000000000004"
		attemptCalculationID     = "0199c104-0000-7000-8000-000000000005"
		attemptValidationID      = "0199c104-0000-7000-8000-000000000006"
		attemptFindingID         = "0199c104-0000-7000-8000-000000000007"
		attemptAcknowledgementID = "0199c104-0000-7000-8000-000000000008"
		rootFactorAssertionID    = "0199c106-0000-7000-8000-000000000001"
		attemptFactorAssertionID = "0199c106-0000-7000-8000-000000000002"
		rootSessionID            = "0199c106-0000-7000-8000-000000000011"
		attemptSessionID         = "0199c106-0000-7000-8000-000000000012"
		rootExportStartedID      = "0199c107-0000-7000-8000-000000000001"
		frozenExportFinishedID   = "0199c107-0000-7000-8000-000000000002"
		observationID            = "0199c102-0000-7000-8000-000000000003"
		correctionID             = "0199c102-0000-7000-8000-000000000001"
		resolutionID             = "0199c102-0000-7000-8000-000000000002"
	)
	profileV1Hash := bytes.Repeat([]byte{0x11}, 32)
	profileV2Hash := bytes.Repeat([]byte{0x12}, 32)
	otherProfileHash := bytes.Repeat([]byte{0x13}, 32)
	periodV1Hash := bytes.Repeat([]byte{0x21}, 32)
	periodV2Hash := bytes.Repeat([]byte{0x22}, 32)
	prepV1Hash := bytes.Repeat([]byte{0x31}, 32)
	prepV2Hash := bytes.Repeat([]byte{0x32}, 32)
	activeAttemptHash := bytes.Repeat([]byte{0x33}, 32)
	branchV1Hash := bytes.Repeat([]byte{0x41}, 32)
	branchV2Hash := bytes.Repeat([]byte{0x42}, 32)
	branchIdentityHash := bytes.Repeat([]byte{0x43}, 32)
	familyHash := bytes.Repeat([]byte{0x51}, 32)
	familyStateInitialHash := bytes.Repeat([]byte{0x54}, 32)
	familyStateRootHash := bytes.Repeat([]byte{0x55}, 32)
	familyStateBranchHash := bytes.Repeat([]byte{0x56}, 32)
	familyStateDisputeHash := bytes.Repeat([]byte{0x57}, 32)
	identityHash := bytes.Repeat([]byte{0x61}, 32)
	otherFamilyHash := bytes.Repeat([]byte{0x62}, 32)
	newPeriodHash := bytes.Repeat([]byte{0x63}, 32)
	rootInputHash := bytes.Repeat([]byte{0x81}, 32)
	rootCalculationHash := bytes.Repeat([]byte{0x82}, 32)
	rootValidationHash := bytes.Repeat([]byte{0x84}, 32)
	rootFindingHash := bytes.Repeat([]byte{0x85}, 32)
	rootAcknowledgementHash := bytes.Repeat([]byte{0x86}, 32)
	rootFrozenHash := bytes.Repeat([]byte{0x87}, 32)
	attemptInputHash := bytes.Repeat([]byte{0x91}, 32)
	attemptCalculationHash := bytes.Repeat([]byte{0x92}, 32)
	attemptValidationHash := bytes.Repeat([]byte{0x94}, 32)
	attemptFindingHash := bytes.Repeat([]byte{0x95}, 32)
	attemptAcknowledgementHash := bytes.Repeat([]byte{0x96}, 32)
	attemptFrozenHash := bytes.Repeat([]byte{0x97}, 32)

	profile := map[string]any{"profile_id": profileID, "organisation_id": "org", "created_at": "2026-10-01T00:00:00Z"}
	otherProfile := map[string]any{"profile_id": otherProfileID, "organisation_id": "other", "created_at": "2026-10-01T00:00:00Z"}
	purchaseInsert(t, db, "bas_profiles_v1", profile, true)
	purchaseInsert(t, db, "bas_profiles_v1", otherProfile, true)
	profileV1 := map[string]any{"profile_id": profileID, "organisation_id": "org", "version": 1, "effective_start": "2026-01-01", "effective_end": "2026-12-31", "content_hash": profileV1Hash, "profile_proto": []byte{1}, "actor_user_id": "user", "created_at": "2026-10-01T00:00:00Z"}
	profileV2 := map[string]any{"profile_id": profileID, "organisation_id": "org", "version": 2, "effective_start": "2026-10-01", "effective_end": "2027-12-31", "content_hash": profileV2Hash, "profile_proto": []byte{2}, "actor_user_id": "user", "previous_version": 1, "previous_hash": profileV1Hash, "created_at": "2026-10-02T00:00:00Z"}
	otherProfileV1 := map[string]any{"profile_id": otherProfileID, "organisation_id": "other", "version": 1, "effective_start": "2026-01-01", "effective_end": "2026-12-31", "content_hash": otherProfileHash, "profile_proto": []byte{3}, "actor_user_id": "user", "created_at": "2026-10-01T00:00:00Z"}
	purchaseInsert(t, db, "bas_profile_versions_v1", profileV1, true)
	// Profile intervals can overlap as retained corrections; effective-date choice belongs to policy.
	purchaseInsert(t, db, "bas_profile_versions_v1", profileV2, true)
	purchaseInsert(t, db, "bas_profile_versions_v1", otherProfileV1, true)
	purchaseInsert(t, db, "bas_profile_heads_v1", map[string]any{"profile_id": profileID, "organisation_id": "org", "current_version": 1, "current_hash": profileV1Hash, "cas_version": 1, "updated_at": "2026-10-01T00:00:00Z"}, true)
	assertBasRowsAffected(t, db, 1, `UPDATE bas_profile_heads_v1 SET current_version=2,current_hash=?,cas_version=2,updated_at=? WHERE profile_id=? AND organisation_id=? AND current_version=1 AND current_hash=? AND cas_version=1`, profileV2Hash, "2026-10-02T00:00:00Z", profileID, "org", profileV1Hash)
	purchaseInsert(t, db, "bas_profile_heads_v1", map[string]any{"profile_id": otherProfileID, "organisation_id": "other", "current_version": 1, "current_hash": otherProfileHash, "cas_version": 1, "updated_at": "2026-10-01T00:00:00Z"}, true)

	period := map[string]any{"period_id": periodID, "organisation_id": "org", "period_start": "2026-10-01", "period_end": "2026-12-31", "reporting_window": "OCT_DEC", "identity_hash": identityHash, "created_at": "2026-10-01T00:00:00Z"}
	purchaseInsert(t, db, "bas_periods_v1", period, true)
	periodV1 := map[string]any{"period_id": periodID, "organisation_id": "org", "version": 1, "profile_id": profileID, "profile_version": 1, "profile_hash": profileV1Hash, "content_hash": periodV1Hash, "period_proto": []byte{1}, "actor_user_id": "user", "created_at": "2026-10-01T00:00:00Z"}
	periodV2 := map[string]any{"period_id": periodID, "organisation_id": "org", "version": 2, "profile_id": profileID, "profile_version": 2, "profile_hash": profileV2Hash, "content_hash": periodV2Hash, "period_proto": []byte{2}, "actor_user_id": "user", "created_at": "2026-10-02T00:00:00Z"}
	purchaseInsert(t, db, "bas_period_versions_v1", periodV1, true)
	purchaseInsert(t, db, "bas_period_versions_v1", periodV2, true)
	purchaseInsert(t, db, "bas_period_versions_v1", purchaseChanged(periodV2, map[string]any{"version": 3, "profile_id": otherProfileID, "profile_hash": otherProfileHash, "content_hash": bytes.Repeat([]byte{0x23}, 32)}), false)
	purchaseInsert(t, db, "bas_period_heads_v1", map[string]any{"period_id": periodID, "organisation_id": "org", "current_version": 1, "current_hash": periodV1Hash, "cas_version": 1, "updated_at": "2026-10-01T00:00:00Z"}, true)
	assertBasRowsAffected(t, db, 1, `UPDATE bas_period_heads_v1 SET current_version=2,current_hash=?,cas_version=2,updated_at=? WHERE period_id=? AND organisation_id=? AND current_version=1 AND current_hash=? AND cas_version=1`, periodV2Hash, "2026-10-02T00:00:00Z", periodID, "org", periodV1Hash)

	family := map[string]any{"family_id": familyID, "organisation_id": "org", "period_start": "2026-10-01", "period_end": "2026-12-31", "reporting_window": "OCT_DEC", "identity_hash": familyHash, "identity_proto": []byte("family-identity-v1"), "created_at": "2026-10-01T00:00:00Z"}
	purchaseInsert(t, db, "bas_family_identities_v1", family, true)
	purchaseInsert(t, db, "bas_family_identities_v1", purchaseChanged(family, map[string]any{"family_id": duplicateFamily, "identity_hash": bytes.Repeat([]byte{0x52}, 32)}), false)
	purchaseInsert(t, db, "bas_family_identities_v1", purchaseChanged(family, map[string]any{"family_id": customFamily, "reporting_window": "CUSTOM", "identity_hash": bytes.Repeat([]byte{0x53}, 32)}), true)
	otherFamily := purchaseChanged(family, map[string]any{"family_id": otherFamilyID, "organisation_id": "other", "identity_hash": otherFamilyHash})
	purchaseInsert(t, db, "bas_family_identities_v1", otherFamily, true)
	newPeriodFamilyRow := purchaseChanged(family, map[string]any{"family_id": newPeriodFamily, "period_start": "2027-01-01", "period_end": "2027-03-31", "reporting_window": "JAN_MAR", "identity_hash": newPeriodHash})
	purchaseInsert(t, db, "bas_family_identities_v1", newPeriodFamilyRow, true)

	preparation := map[string]any{"preparation_id": preparationID, "organisation_id": "org", "family_id": familyID, "branch_id": nil, "attempt_id": rootAttemptID, "predecessor_id": nil, "created_at": "2026-10-01T00:00:00Z"}
	purchaseInsert(t, db, "bas_preparation_identities_v1", preparation, true)
	rootSnapshot := basTestSnapshotSeed{
		organisationID: "org", familyID: familyID, preparationID: preparationID,
		profileID: profileID, periodID: periodID,
		calculationID: rootCalculationID, validationID: rootValidationID, findingID: rootFindingID,
		acknowledgementID: rootAcknowledgementID, frozenID: rootFreezeID,
		factorAssertionID: rootFactorAssertionID, sessionID: rootSessionID,
		profileVersion: 1, periodVersion: 1, profileHash: profileV1Hash, periodHash: periodV1Hash,
		inputHash: rootInputHash, calculationHash: rootCalculationHash, validationHash: rootValidationHash,
		findingHash: rootFindingHash, acknowledgementHash: rootAcknowledgementHash, frozenHash: rootFrozenHash,
	}
	rootReady := seedBasCalculationStack(t, db, rootSnapshot)
	purchaseInsert(t, db, "bas_family_state_v1", map[string]any{"family_id": familyID, "organisation_id": "org", "root_preparation_id": preparationID, "root_version": 1, "root_hash": basSeedPreparationHash(rootSnapshot, 1), "cas_version": 1, "overlay_state": "NONE", "family_state_proto": []byte("family-state-initial"), "state_hash": familyStateInitialHash, "updated_at": "2026-10-01T00:00:00Z"}, true)
	prepV1 := purchaseChanged(rootReady, map[string]any{"version": 6, "state": "EXTERNAL_CONFIRMATION_RECORDED", "freeze_id": rootFreezeID, "freeze_hash": rootFrozenHash, "content_hash": prepV1Hash, "preparation_proto": []byte("confirmed-v6")})
	rootFrozen := map[string]any{
		"frozen_id": rootFreezeID, "organisation_id": "org", "family_id": familyID,
		"preparation_id": preparationID, "preparation_version": 4, "preparation_hash": basSeedPreparationHash(rootSnapshot, 4),
		"profile_id": profileID, "profile_version": 1, "profile_hash": profileV1Hash, "period_id": periodID, "period_version": 1, "period_hash": periodV1Hash,
		"calculation_id": rootCalculationID, "calculation_version": 1, "calculation_hash": rootCalculationHash,
		"validation_id": rootValidationID, "validation_revision": 1, "validation_hash": rootValidationHash,
		"source_snapshot_hash": bytes.Repeat([]byte{0xa1}, 32), "approved_snapshot_hash": bytes.Repeat([]byte{0x88}, 32),
		"content_hash": rootFrozenHash, "calculation_snapshot_proto": []byte{0xc1}, "frozen_proto": []byte("frozen-root-v1"),
		"approval_actor_user_id": "user", "fresh_factor_assertion_id": rootFactorAssertionID,
		"approved_at": "2026-10-01T00:01:00Z", "approval_reason": "Reviewed statement snapshot",
	}
	rootPostFreeze := purchaseChanged(rootReady, map[string]any{"version": 5, "state": "FROZEN_FOR_MANUAL_HANDOFF", "freeze_id": rootFreezeID, "freeze_hash": rootFrozenHash, "content_hash": basSeedPreparationHash(rootSnapshot, 5), "preparation_proto": []byte("frozen-v5")})
	seedBasFrozenPreparationVersion(t, db, rootFrozen, rootPostFreeze, rootFactorAssertionID)
	appendBasFixturePreparation(t, db, prepV1)
	prepV2 := purchaseChanged(prepV1, map[string]any{
		"version": 7, "content_hash": prepV2Hash,
		"preparation_proto": []byte("confirmed-v1-with-export-metadata"), "created_at": "2026-10-03T00:00:00Z",
	})
	purchaseInsert(t, db, "bas_preparation_versions_v1", purchaseChanged(prepV1, map[string]any{"version": 3, "content_hash": []byte{1}}), false)
	appendBasFixturePreparation(t, db, prepV2)
	purchaseInsert(t, db, "bas_frozen_acknowledgements_v1", map[string]any{
		"frozen_id": rootFreezeID, "frozen_hash": rootFrozenHash, "organisation_id": "org", "family_id": familyID,
		"acknowledgement_id": rootAcknowledgementID, "acknowledgement_hash": rootAcknowledgementHash,
	}, true)

	// Family/root-head references pin the confirmed retained version. Appending a new current snapshot does not rewrite that exact identity.
	assertBasRowsAffected(t, db, 1, `UPDATE bas_family_state_v1 SET effective_original_preparation_id=?,effective_original_version=6,effective_original_hash=?,head_preparation_id=?,head_version=6,head_hash=?,family_state_proto=?,state_hash=?,cas_version=2,updated_at=? WHERE family_id=? AND organisation_id=? AND cas_version=1`, preparationID, prepV1Hash, preparationID, prepV1Hash, []byte("family-state-root-v1"), familyStateRootHash, "2026-10-01T00:00:00Z", familyID, "org")
	assertBasRowsAffected(t, db, 0, `UPDATE bas_preparation_heads_v1 SET current_version=3,current_hash=?,cas_version=3 WHERE preparation_id=? AND organisation_id=? AND current_version=1 AND cas_version=1`, bytes.Repeat([]byte{0x34}, 32), preparationID, "org")

	branch := map[string]any{"branch_id": branchID, "family_id": familyID, "organisation_id": "org", "original_purpose": "REVISION", "identity_hash": branchIdentityHash, "branch_proto": []byte("branch-identity-v1"), "root_preparation_id": preparationID, "root_version": 6, "root_hash": prepV1Hash, "family_root_preparation_id": preparationID, "family_root_version": 1, "family_root_hash": basSeedPreparationHash(rootSnapshot, 1), "effective_original_preparation_id": preparationID, "effective_original_version": 6, "effective_original_hash": prepV1Hash, "created_at": "2026-10-01T00:00:00Z"}
	purchaseInsert(t, db, "bas_branch_identities_v1", branch, true)
	attemptPreparation := map[string]any{"preparation_id": attemptPrepID, "organisation_id": "org", "family_id": familyID, "branch_id": branchID, "attempt_id": activeAttemptID, "predecessor_id": preparationID, "created_at": "2026-10-02T00:00:00Z"}
	purchaseInsert(t, db, "bas_preparation_identities_v1", attemptPreparation, true)
	attemptSnapshot := basTestSnapshotSeed{
		organisationID: "org", familyID: familyID, preparationID: attemptPrepID,
		profileID: profileID, periodID: periodID,
		calculationID: attemptCalculationID, validationID: attemptValidationID, findingID: attemptFindingID,
		acknowledgementID: attemptAcknowledgementID, frozenID: attemptFreezeID,
		factorAssertionID: attemptFactorAssertionID, sessionID: attemptSessionID,
		profileVersion: 1, periodVersion: 1, profileHash: profileV1Hash, periodHash: periodV1Hash,
		inputHash: attemptInputHash, calculationHash: attemptCalculationHash, validationHash: attemptValidationHash,
		findingHash: attemptFindingHash, acknowledgementHash: attemptAcknowledgementHash, frozenHash: attemptFrozenHash,
	}
	attemptReady := seedBasCalculationStack(t, db, attemptSnapshot)
	attemptVersion := purchaseChanged(attemptReady, map[string]any{"version": 5, "state": "FROZEN_FOR_MANUAL_HANDOFF", "freeze_id": attemptFreezeID, "freeze_hash": attemptFrozenHash, "content_hash": activeAttemptHash, "preparation_proto": []byte("revision-attempt-frozen-v5")})
	attemptFrozen := map[string]any{
		"frozen_id": attemptFreezeID, "organisation_id": "org", "family_id": familyID,
		"preparation_id": attemptPrepID, "preparation_version": 4, "preparation_hash": basSeedPreparationHash(attemptSnapshot, 4),
		"profile_id": profileID, "profile_version": 1, "profile_hash": profileV1Hash, "period_id": periodID, "period_version": 1, "period_hash": periodV1Hash,
		"calculation_id": attemptCalculationID, "calculation_version": 1, "calculation_hash": attemptCalculationHash,
		"validation_id": attemptValidationID, "validation_revision": 1, "validation_hash": attemptValidationHash,
		"source_snapshot_hash": bytes.Repeat([]byte{0xa1}, 32), "approved_snapshot_hash": bytes.Repeat([]byte{0x98}, 32),
		"content_hash": attemptFrozenHash, "calculation_snapshot_proto": []byte{0xc2}, "frozen_proto": []byte("frozen-attempt-v1"),
		"approval_actor_user_id": "user", "fresh_factor_assertion_id": attemptFactorAssertionID,
		"approved_at": "2026-10-02T00:01:00Z", "approval_reason": "Reviewed revision attempt snapshot",
	}
	seedBasFrozenPreparationVersion(t, db, attemptFrozen, attemptVersion, attemptFactorAssertionID)
	purchaseInsert(t, db, "bas_frozen_acknowledgements_v1", map[string]any{
		"frozen_id": attemptFreezeID, "frozen_hash": attemptFrozenHash, "organisation_id": "org", "family_id": familyID,
		"acknowledgement_id": attemptAcknowledgementID, "acknowledgement_hash": attemptAcknowledgementHash,
	}, true)
	branchV1 := map[string]any{"branch_id": branchID, "family_id": familyID, "organisation_id": "org", "version": 1, "workflow_state": "ACTIVE", "active_attempt_id": activeAttemptID, "active_preparation_id": attemptPrepID, "active_attempt_version": 5, "active_attempt_hash": activeAttemptHash, "corrective_resolution_id": nil, "corrective_resolution_hash": nil, "barrier_preparation_id": nil, "barrier_preparation_version": nil, "barrier_preparation_hash": nil, "barrier_attempt_id": nil, "barrier_frozen_id": nil, "barrier_frozen_hash": nil, "content_hash": branchV1Hash, "branch_state_proto": []byte("branch-state-active-v1"), "created_at": "2026-10-02T00:00:00Z"}
	purchaseInsert(t, db, "bas_branch_versions_v1", branchV1, true)
	purchaseInsert(t, db, "bas_branch_heads_v1", map[string]any{"branch_id": branchID, "family_id": familyID, "organisation_id": "org", "current_version": 1, "current_hash": branchV1Hash, "cas_version": 1, "updated_at": "2026-10-02T00:00:00Z"}, true)
	assertBasRowsAffected(t, db, 1, `UPDATE bas_family_state_v1 SET active_branch_id=?,active_branch_identity_hash=?,active_branch_version=1,active_branch_hash=?,family_state_proto=?,state_hash=?,cas_version=3,updated_at=? WHERE family_id=? AND organisation_id=? AND cas_version=2`, branchID, branchIdentityHash, branchV1Hash, []byte("family-state-active-branch-v1"), familyStateBranchHash, "2026-10-02T00:00:00Z", familyID, "org")
	var activePrepID, activeState, activeFreeze string
	if err := db.QueryRowContext(ctx, `SELECT p.preparation_id,v.state,v.freeze_id FROM bas_branch_heads_v1 h JOIN bas_branch_versions_v1 b ON b.branch_id=h.branch_id AND b.family_id=h.family_id AND b.organisation_id=h.organisation_id AND b.version=h.current_version AND b.content_hash=h.current_hash JOIN bas_preparation_identities_v1 p ON p.branch_id=b.branch_id AND p.family_id=b.family_id AND p.organisation_id=b.organisation_id AND p.attempt_id=b.active_attempt_id JOIN bas_preparation_heads_v1 ph ON ph.preparation_id=p.preparation_id AND ph.organisation_id=p.organisation_id JOIN bas_preparation_versions_v1 v ON v.preparation_id=ph.preparation_id AND v.organisation_id=ph.organisation_id AND v.version=ph.current_version AND v.content_hash=ph.current_hash WHERE h.branch_id=?`, branchID).Scan(&activePrepID, &activeState, &activeFreeze); err != nil {
		t.Fatal(err)
	}
	if activePrepID != attemptPrepID || activeState != "FROZEN_FOR_MANUAL_HANDOFF" || activeFreeze != attemptFreezeID {
		t.Fatalf("revision branch does not point to its separate frozen attempt: prep=%s state=%s freeze=%s", activePrepID, activeState, activeFreeze)
	}
	purchaseInsert(t, db, "bas_export_events_v1", map[string]any{
		"export_event_id": rootExportStartedID, "operation_key": "0199c107-0000-7000-8000-000000000011",
		"organisation_id": "org", "family_id": familyID, "target_kind": "CALCULATION",
		"event_version":  1,
		"calculation_id": rootCalculationID, "calculation_version": 1, "calculation_hash": rootCalculationHash,
		"format": "PDF", "phase": "STARTED", "outcome": nil, "artifact_hash": nil,
		"actor_user_id": "user", "reason": "Review export", "content_hash": bytes.Repeat([]byte{0xc3}, 32),
		"export_proto": []byte{0xc4}, "recorded_at": "2026-10-03T00:00:00Z",
	}, true)
	purchaseInsert(t, db, "bas_export_events_v1", map[string]any{
		"export_event_id": frozenExportFinishedID, "operation_key": "0199c107-0000-7000-8000-000000000012",
		"organisation_id": "org", "family_id": familyID, "target_kind": "FROZEN",
		"event_version": 1,
		"frozen_id":     rootFreezeID, "frozen_hash": rootFrozenHash,
		"format": "CSV", "phase": "FINISHED", "outcome": "SUCCEEDED", "artifact_hash": bytes.Repeat([]byte{0xc5}, 32),
		"actor_user_id": "user", "reason": "Reviewed fixed export", "content_hash": bytes.Repeat([]byte{0xc6}, 32),
		"export_proto": []byte{0xc7}, "recorded_at": "2026-10-03T00:01:00Z",
	}, true)

	observationOperation := "0199c103-0000-7000-8000-000000000001"
	correctionOperation := "0199c103-0000-7000-8000-000000000002"
	resolutionOperation := "0199c103-0000-7000-8000-000000000003"
	observationOrdinal := insertBasFamilyEvent(t, db, observationID, familyID, "OBSERVATION", observationOperation, "2026-10-05T00:00:00Z", "2026-10-05T00:00:00Z", 0x71)
	correctionOrdinal := insertBasFamilyEvent(t, db, correctionID, familyID, "CORRECTION", correctionOperation, "2026-10-01T00:00:00Z", "2026-10-06T00:00:00Z", 0x72)
	resolutionOrdinal := insertBasFamilyEvent(t, db, resolutionID, familyID, "RESOLUTION", resolutionOperation, "2026-10-03T00:00:00Z", "2026-10-07T00:00:00Z", 0x73)
	if !(observationOrdinal < correctionOrdinal && correctionOrdinal < resolutionOrdinal) {
		t.Fatalf("family event ordinals are not append ordered: %d, %d, %d", observationOrdinal, correctionOrdinal, resolutionOrdinal)
	}
	observationHash := bytes.Repeat([]byte{0x71}, 32)
	correctionHash := bytes.Repeat([]byte{0x72}, 32)
	resolutionHash := bytes.Repeat([]byte{0x73}, 32)
	purchaseInsert(t, db, "bas_external_observations_v1", map[string]any{
		"observation_id": observationID, "event_id": observationID, "organisation_id": "org", "family_id": familyID,
		"frozen_id": rootFreezeID, "frozen_hash": rootFrozenHash, "event_kind": "PENDING", "channel": "ONLINE_PORTAL",
		"claimed_event_at": "2026-10-05T00:00:00Z", "external_reference": "ATO-REF-1", "reason": "Portal reports pending",
		"trust": "USER_RECORDED_UNVERIFIED", "actor_user_id": "user", "recorded_at": "2026-10-05T00:00:00Z",
		"content_hash": observationHash, "observation_proto": []byte{0xc8},
	}, true)
	purchaseInsert(t, db, "bas_external_observation_corrections_v1", map[string]any{
		"correction_id": correctionID, "event_id": correctionID, "organisation_id": "org", "family_id": familyID,
		"frozen_id": rootFreezeID, "frozen_hash": rootFrozenHash,
		"disputed_observation_id": observationID, "disputed_observation_hash": observationHash,
		"correction_kind": "REPLACE_ASSERTION", "corrected_event_kind": "ACCEPTANCE",
		"corrected_claimed_event_at": "2026-10-04T00:00:00Z", "reason": "Corrected the selected event",
		"trust": "USER_RECORDED_UNVERIFIED", "actor_user_id": "user", "recorded_at": "2026-10-06T00:00:00Z",
		"content_hash": correctionHash, "correction_proto": []byte{0xc9},
	}, true)
	purchaseInsert(t, db, "bas_dispute_resolutions_v1", map[string]any{
		"resolution_id": resolutionID, "event_id": resolutionID, "organisation_id": "org", "family_id": familyID,
		"outcome": "REQUIRE_CORRECTIVE_REVISION", "dependent_chain_manifest_hash": bytes.Repeat([]byte{0xd1}, 32),
		"reason": "A corrective revision is required", "actor_user_id": "user", "resolved_at": "2026-10-07T00:00:00Z",
		"content_hash": resolutionHash, "resolution_proto": []byte{0xd2},
	}, true)
	purchaseInsert(t, db, "bas_dispute_resolution_observations_v1", map[string]any{
		"resolution_id": resolutionID, "organisation_id": "org", "family_id": familyID,
		"subject_kind": "OBSERVATION", "subject_id": observationID, "subject_hash": observationHash,
		"observation_id": observationID, "observation_hash": observationHash,
	}, true)
	purchaseInsert(t, db, "bas_dispute_resolution_observations_v1", map[string]any{
		"resolution_id": resolutionID, "organisation_id": "org", "family_id": familyID,
		"subject_kind": "CORRECTION", "subject_id": correctionID, "subject_hash": correctionHash,
		"correction_id": correctionID, "correction_hash": correctionHash,
	}, true)
	purchaseInsert(t, db, "bas_dispute_resolution_preparations_v1", map[string]any{
		"resolution_id": resolutionID, "organisation_id": "org", "family_id": familyID,
		"preparation_id": preparationID, "preparation_version": 6, "preparation_hash": prepV1Hash,
	}, true)
	var duplicateOperation int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_family_events_v1 WHERE family_id=? AND operation_key=?`, familyID, observationOperation).Scan(&duplicateOperation); err != nil || duplicateOperation != 1 {
		t.Fatalf("idempotency key count = %d, %v", duplicateOperation, err)
	}
	assertExecFails(t, db, `INSERT INTO bas_family_events_v1(event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at) VALUES('0199c102-0000-7000-8000-000000000020','org','0199c101-0000-7000-8000-000000000005','CORRECTION','0199c103-0000-7000-8000-000000000001','2026-10-01T00:00:00Z',X'01',zeroblob(32),'2026-10-07T00:00:00Z')`)
	assertBasFamilyEventOrder(t, db, []string{observationID, correctionID, resolutionID})

	branchV2 := map[string]any{"branch_id": branchID, "family_id": familyID, "organisation_id": "org", "version": 2, "workflow_state": "CORRECTIVE", "active_attempt_id": activeAttemptID, "active_preparation_id": attemptPrepID, "active_attempt_version": 5, "active_attempt_hash": activeAttemptHash, "corrective_resolution_id": resolutionID, "corrective_resolution_hash": resolutionHash, "barrier_preparation_id": attemptPrepID, "barrier_preparation_version": 5, "barrier_preparation_hash": activeAttemptHash, "barrier_attempt_id": activeAttemptID, "barrier_frozen_id": attemptFreezeID, "barrier_frozen_hash": attemptFrozenHash, "content_hash": branchV2Hash, "branch_state_proto": []byte("branch-state-corrective-v2"), "created_at": "2026-10-07T00:00:00Z"}
	for _, check := range beforeCorrectiveAdoption {
		check(db, branchV2)
	}
	purchaseInsert(t, db, "bas_branch_versions_v1", branchV2, true)
	assertBasRowsAffected(t, db, 1, `UPDATE bas_branch_heads_v1 SET current_version=2,current_hash=?,cas_version=2,updated_at=? WHERE branch_id=? AND organisation_id=? AND current_version=1 AND current_hash=? AND cas_version=1`, branchV2Hash, "2026-10-07T00:00:00Z", branchID, "org", branchV1Hash)
	assertBasRowsAffected(t, db, 0, `UPDATE bas_branch_heads_v1 SET current_version=3,current_hash=?,cas_version=3 WHERE branch_id=? AND organisation_id=? AND current_version=1 AND cas_version=1`, bytes.Repeat([]byte{0x43}, 32), branchID, "org")
	assertBasRowsAffected(t, db, 1, `UPDATE bas_family_state_v1 SET cas_version=4,overlay_state='DISPUTED',family_state_proto=?,state_hash=?,updated_at=? WHERE family_id=? AND organisation_id=? AND cas_version=3`, []byte("family-state-disputed-v1"), familyStateDisputeHash, "2026-10-07T00:00:00Z", familyID, "org")
	assertBasRowsAffected(t, db, 0, `UPDATE bas_family_state_v1 SET cas_version=5,overlay_state='RESOLVED_UPHELD' WHERE family_id=? AND organisation_id=? AND cas_version=3`, familyID, "org")

	var exactVersion int
	var exactHash, body []byte
	if err := db.QueryRowContext(context.Background(), `SELECT p.version,p.content_hash,p.preparation_proto FROM bas_family_state_v1 f JOIN bas_preparation_versions_v1 p ON p.preparation_id=f.head_preparation_id AND p.organisation_id=f.organisation_id AND p.version=f.head_version AND p.content_hash=f.head_hash WHERE f.family_id=? AND f.organisation_id=?`, familyID, "org").Scan(&exactVersion, &exactHash, &body); err != nil {
		t.Fatal(err)
	}
	if exactVersion != 6 || !bytes.Equal(exactHash, prepV1Hash) || !bytes.Equal(body, []byte("confirmed-v6")) {
		t.Fatalf("family exact retained head changed: version=%d hash=%x body=%q", exactVersion, exactHash, body)
	}
	var currentPreparationVersion int
	var currentPreparationHash []byte
	if err := db.QueryRowContext(ctx, `SELECT current_version,current_hash FROM bas_preparation_heads_v1 WHERE preparation_id=? AND organisation_id=?`, preparationID, "org").Scan(&currentPreparationVersion, &currentPreparationHash); err != nil {
		t.Fatal(err)
	}
	if currentPreparationVersion != 7 || !bytes.Equal(currentPreparationHash, prepV2Hash) {
		t.Fatalf("preparation current pointer = %d/%x, want metadata snapshot v2/%x", currentPreparationVersion, currentPreparationHash, prepV2Hash)
	}
	var v2ProfileVersion, v2PeriodVersion int
	var v2ProfileHash, v2PeriodHash []byte
	var v2State string
	var v2FreezeID string
	if err := db.QueryRowContext(ctx, `SELECT profile_version,profile_hash,period_version,period_hash,state,freeze_id FROM bas_preparation_versions_v1 WHERE preparation_id=? AND version=7`, preparationID).Scan(&v2ProfileVersion, &v2ProfileHash, &v2PeriodVersion, &v2PeriodHash, &v2State, &v2FreezeID); err != nil {
		t.Fatal(err)
	}
	if v2ProfileVersion != 1 || !bytes.Equal(v2ProfileHash, profileV1Hash) || v2PeriodVersion != 1 || !bytes.Equal(v2PeriodHash, periodV1Hash) || v2State != "EXTERNAL_CONFIRMATION_RECORDED" || v2FreezeID != rootFreezeID {
		t.Fatal("metadata-only preparation version changed its frozen profile, period, or confirmation state")
	}
	if err := db.QueryRowContext(context.Background(), `SELECT p.version,p.content_hash,p.freeze_id FROM bas_branch_identities_v1 b JOIN bas_preparation_versions_v1 p ON p.preparation_id=b.root_preparation_id AND p.organisation_id=b.organisation_id AND p.version=b.root_version AND p.content_hash=b.root_hash WHERE b.branch_id=? AND b.organisation_id=?`, branchID, "org").Scan(&exactVersion, &exactHash, new(string)); err != nil {
		t.Fatal(err)
	}
	if exactVersion != 6 || !bytes.Equal(exactHash, prepV1Hash) {
		t.Fatalf("branch root head changed after preparation metadata append: version=%d hash=%x", exactVersion, exactHash)
	}
	var purpose string
	var branchVersion, branchCAS int
	var gotBranchIdentityHash []byte
	if err := db.QueryRowContext(context.Background(), `SELECT i.original_purpose,i.identity_hash,v.version,h.cas_version FROM bas_branch_identities_v1 i JOIN bas_branch_heads_v1 h ON h.branch_id=i.branch_id AND h.family_id=i.family_id AND h.organisation_id=i.organisation_id JOIN bas_branch_versions_v1 v ON v.branch_id=h.branch_id AND v.family_id=h.family_id AND v.organisation_id=h.organisation_id AND v.version=h.current_version AND v.content_hash=h.current_hash WHERE i.branch_id=?`, branchID).Scan(&purpose, &gotBranchIdentityHash, &branchVersion, &branchCAS); err != nil {
		t.Fatal(err)
	}
	if purpose != "REVISION" || !bytes.Equal(gotBranchIdentityHash, branchIdentityHash) || branchVersion != 2 || branchCAS != 2 {
		t.Fatalf("corrective adoption did not append branch workflow version: purpose=%s version=%d cas=%d", purpose, branchVersion, branchCAS)
	}
	var barrierVersion int
	var barrierAttemptID string
	var barrierHash, frozenHash []byte
	var barrierPrepID, barrierFreezeID string
	if err := db.QueryRowContext(ctx, `SELECT b.barrier_preparation_version,b.barrier_preparation_hash,b.barrier_attempt_id,b.barrier_frozen_id,f.content_hash,p.preparation_id,v.freeze_id FROM bas_branch_versions_v1 b JOIN bas_preparation_identities_v1 p ON p.preparation_id=b.barrier_preparation_id AND p.organisation_id=b.organisation_id AND p.family_id=b.family_id AND p.branch_id=b.branch_id AND p.attempt_id=b.barrier_attempt_id JOIN bas_preparation_versions_v1 v ON v.preparation_id=b.barrier_preparation_id AND v.organisation_id=b.organisation_id AND v.version=b.barrier_preparation_version AND v.content_hash=b.barrier_preparation_hash AND v.freeze_id=b.barrier_frozen_id JOIN bas_frozen_bas_v1 f ON f.frozen_id=b.barrier_frozen_id AND f.organisation_id=b.organisation_id AND f.family_id=b.family_id AND f.preparation_id=b.barrier_preparation_id AND f.preparation_version<b.barrier_preparation_version AND f.content_hash=b.barrier_frozen_hash WHERE b.branch_id=? AND b.version=2`, branchID).Scan(&barrierVersion, &barrierHash, &barrierAttemptID, &barrierFreezeID, &frozenHash, &barrierPrepID, new(string)); err != nil {
		t.Fatal(err)
	}
	if barrierVersion != 5 || barrierAttemptID != activeAttemptID || barrierPrepID != attemptPrepID || barrierFreezeID != attemptFreezeID || !bytes.Equal(barrierHash, activeAttemptHash) || !bytes.Equal(frozenHash, attemptFrozenHash) {
		t.Fatalf("corrective branch lost exact frozen pre-resolution barrier: version=%d prep=%s attempt=%s freeze=%s hash=%x frozen=%x", barrierVersion, barrierPrepID, barrierAttemptID, barrierFreezeID, barrierHash, frozenHash)
	}
	assertExecFails(t, db, `UPDATE bas_branch_identities_v1 SET original_purpose='CORRECTIVE' WHERE branch_id='0199c101-0000-7000-8000-000000000008'`)
	assertExecFails(t, db, `DELETE FROM bas_branch_identities_v1 WHERE branch_id='0199c101-0000-7000-8000-000000000008'`)
	assertExecFails(t, db, `UPDATE bas_branch_versions_v1 SET workflow_state='MUTATED' WHERE branch_id='0199c101-0000-7000-8000-000000000008' AND version=1`)
	assertExecFails(t, db, `DELETE FROM bas_branch_versions_v1 WHERE branch_id='0199c101-0000-7000-8000-000000000008' AND version=1`)

	// A scoped family FK must reject a preparation that claims another organisation's family.
	purchaseInsert(t, db, "bas_preparation_identities_v1", purchaseChanged(preparation, map[string]any{"preparation_id": invalidPrepID, "family_id": otherFamilyID}), false)
	assertExecFails(t, db, `INSERT INTO bas_family_events_v1(event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at) VALUES('0199c102-0000-7000-8000-000000000021','org','0199c101-0000-7000-8000-000000000004','OBSERVATION','0199c103-0000-7000-8000-000000000004','2026-10-01T00:00:00Z',X'01',zeroblob(32),'2026-10-07T00:00:00Z')`)

	// A failed compound write rolls back the new immutable version and its mutable CAS pointer together.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rollbackVersion := purchaseChanged(prepV2, map[string]any{"version": 8, "content_hash": bytes.Repeat([]byte{0x34}, 32), "preparation_proto": []byte("rolled-back-metadata")})
	rollbackQuery, rollbackArgs := purchaseInsertSQL("bas_preparation_versions_v1", rollbackVersion, "INSERT")
	if _, err := tx.ExecContext(ctx, rollbackQuery, rollbackArgs...); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_preparation_heads_v1 SET current_version=8,current_hash=?,cas_version=8 WHERE preparation_id=? AND organisation_id=? AND cas_version=7 AND current_version=7`, bytes.Repeat([]byte{0x34}, 32), preparationID, "org"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bas_family_events_v1(event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, "0199c102-0000-7000-8000-000000000022", "org", "0199c101-0000-7000-8000-000000000099", "CORRECTION", "0199c103-0000-7000-8000-000000000005", "2026-10-08T00:00:00Z", []byte{1}, bytes.Repeat([]byte{0x74}, 32), "2026-10-08T00:00:00Z"); err == nil {
		_ = tx.Rollback()
		t.Fatal("cross-family event admitted in compound transaction")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var rolledBackRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM bas_preparation_versions_v1 WHERE preparation_id=? AND version=8`, preparationID).Scan(&rolledBackRows); err != nil || rolledBackRows != 0 {
		t.Fatalf("rolled-back preparation version count = %d, %v", rolledBackRows, err)
	}
	var currentVersion, currentCAS int
	if err := db.QueryRowContext(ctx, `SELECT current_version,cas_version FROM bas_preparation_heads_v1 WHERE preparation_id=?`, preparationID).Scan(&currentVersion, &currentCAS); err != nil || currentVersion != 7 || currentCAS != 7 {
		t.Fatalf("rollback changed preparation CAS pointer: version=%d cas=%d err=%v", currentVersion, currentCAS, err)
	}
	var eventCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM bas_family_events_v1 WHERE family_id=?`, familyID).Scan(&eventCount); err != nil || eventCount != 3 {
		t.Fatalf("rollback retained family event: count=%d err=%v", eventCount, err)
	}
}

func assertBasLifecycleHistoricalRowsImmutable(t *testing.T, db *Database) {
	t.Helper()
	for _, query := range []string{
		`UPDATE bas_profiles_v1 SET created_at=created_at`,
		`DELETE FROM bas_profiles_v1`,
		`UPDATE bas_profile_versions_v1 SET profile_proto=profile_proto`,
		`DELETE FROM bas_profile_versions_v1`,
		`UPDATE bas_periods_v1 SET period_start=period_start`,
		`DELETE FROM bas_periods_v1`,
		`UPDATE bas_period_versions_v1 SET period_proto=period_proto`,
		`DELETE FROM bas_period_versions_v1`,
		`UPDATE bas_family_identities_v1 SET identity_hash=identity_hash`,
		`DELETE FROM bas_family_identities_v1`,
		`UPDATE bas_preparation_identities_v1 SET attempt_id=attempt_id`,
		`DELETE FROM bas_preparation_identities_v1`,
		`UPDATE bas_preparation_versions_v1 SET preparation_proto=preparation_proto`,
		`DELETE FROM bas_preparation_versions_v1`,
		`UPDATE bas_payg_input_versions_v1 SET input_proto=input_proto`,
		`DELETE FROM bas_payg_input_versions_v1`,
		`UPDATE bas_calculations_v1 SET calculation_proto=calculation_proto`,
		`DELETE FROM bas_calculations_v1`,
		`UPDATE bas_calculation_coverage_v1 SET coverage_proto=coverage_proto`,
		`DELETE FROM bas_calculation_coverage_v1`,
		`UPDATE bas_validations_v1 SET validation_proto=validation_proto`,
		`DELETE FROM bas_validations_v1`,
		`UPDATE bas_validation_findings_v1 SET finding_proto=finding_proto`,
		`DELETE FROM bas_validation_findings_v1`,
		`UPDATE bas_warning_acknowledgements_v1 SET acknowledgement_proto=acknowledgement_proto`,
		`DELETE FROM bas_warning_acknowledgements_v1`,
		`UPDATE bas_frozen_bas_v1 SET frozen_proto=frozen_proto`,
		`DELETE FROM bas_frozen_bas_v1`,
		`UPDATE bas_frozen_acknowledgements_v1 SET acknowledgement_hash=acknowledgement_hash`,
		`DELETE FROM bas_frozen_acknowledgements_v1`,
		`UPDATE bas_export_events_v1 SET export_proto=export_proto`,
		`DELETE FROM bas_export_events_v1`,
		`UPDATE bas_external_observations_v1 SET observation_proto=observation_proto`,
		`DELETE FROM bas_external_observations_v1`,
		`UPDATE bas_external_observation_corrections_v1 SET correction_proto=correction_proto`,
		`DELETE FROM bas_external_observation_corrections_v1`,
		`UPDATE bas_dispute_resolutions_v1 SET resolution_proto=resolution_proto`,
		`DELETE FROM bas_dispute_resolutions_v1`,
		`UPDATE bas_dispute_resolution_observations_v1 SET observation_hash=observation_hash`,
		`DELETE FROM bas_dispute_resolution_observations_v1`,
		`UPDATE bas_dispute_resolution_preparations_v1 SET preparation_hash=preparation_hash`,
		`DELETE FROM bas_dispute_resolution_preparations_v1`,
		`UPDATE bas_family_events_v1 SET body_hash=body_hash`,
		`DELETE FROM bas_family_events_v1`,
	} {
		assertExecFails(t, db, query)
	}
}

func assertBasTypedRetentionRows(t *testing.T, db *Database) {
	t.Helper()
	wantCounts := map[string]int{
		"bas_payg_input_versions_v1": 2, "bas_calculations_v1": 2,
		"bas_calculation_coverage_v1": 2, "bas_validations_v1": 2,
		"bas_validation_findings_v1": 2, "bas_warning_acknowledgements_v1": 2,
		"bas_frozen_bas_v1": 2, "bas_frozen_acknowledgements_v1": 2,
		"bas_export_events_v1": 2, "bas_external_observations_v1": 1,
		"bas_external_observation_corrections_v1": 1, "bas_dispute_resolutions_v1": 1,
		"bas_dispute_resolution_observations_v1": 2, "bas_dispute_resolution_preparations_v1": 1,
	}
	for table, want := range wantCounts {
		var got int
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("typed history %s rows = %d, want %d", table, got, want)
		}
	}
	var consumed int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM factor_assertions WHERE id IN ('0199c106-0000-7000-8000-000000000001','0199c106-0000-7000-8000-000000000002') AND consumed=1`).Scan(&consumed); err != nil || consumed != 2 {
		t.Fatalf("freeze factor assertions consumed = %d, %v", consumed, err)
	}
	var exactFrozen, exactBranch int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_frozen_bas_v1 f JOIN bas_preparation_versions_v1 p ON p.preparation_id=f.preparation_id AND p.organisation_id=f.organisation_id AND p.version=f.preparation_version AND p.content_hash=f.preparation_hash AND p.state='REVIEW_READY' AND p.freeze_id IS NULL JOIN bas_calculations_v1 c ON c.calculation_id=f.calculation_id AND c.organisation_id=f.organisation_id AND c.version=f.calculation_version AND c.content_hash=f.calculation_hash JOIN bas_validations_v1 v ON v.validation_id=f.validation_id AND v.organisation_id=f.organisation_id AND v.revision=f.validation_revision AND v.content_hash=f.validation_hash WHERE f.frozen_id IN ('0199c101-0000-7000-8000-000000000011','0199c101-0000-7000-8000-000000000019')`).Scan(&exactFrozen); err != nil || exactFrozen != 2 {
		t.Fatalf("frozen approvals lost exact preparation/calculation/validation parents: rows=%d err=%v", exactFrozen, err)
	}
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_branch_versions_v1 b JOIN bas_dispute_resolutions_v1 r ON r.resolution_id=b.corrective_resolution_id AND r.organisation_id=b.organisation_id AND r.family_id=b.family_id AND r.content_hash=b.corrective_resolution_hash JOIN bas_frozen_bas_v1 f ON f.frozen_id=b.barrier_frozen_id AND f.organisation_id=b.organisation_id AND f.family_id=b.family_id AND f.preparation_id=b.barrier_preparation_id AND f.preparation_version<b.barrier_preparation_version AND f.content_hash=b.barrier_frozen_hash WHERE b.version=2 AND b.workflow_state='CORRECTIVE'`).Scan(&exactBranch); err != nil || exactBranch != 1 {
		t.Fatalf("corrective branch lost its exact resolution/barrier: rows=%d err=%v", exactBranch, err)
	}
	for table := range wantCounts {
		assertExecFails(t, db, `INSERT OR REPLACE INTO `+table+` SELECT * FROM `+table+` LIMIT 1`)
	}
	assertExecFails(t, db, `INSERT OR REPLACE INTO bas_family_identities_v1(family_id,organisation_id,period_start,period_end,reporting_window,identity_hash,identity_proto,created_at) VALUES('0199c101-0000-7000-8000-000000000013','org','2026-10-01','2026-12-31','OCT_DEC',zeroblob(32),X'01','2026-10-08T00:00:00Z')`)
}

func insertBasFamilyEvent(t *testing.T, db *Database, eventID, familyID, kind, operationKey, claimedAt, createdAt string, hashByte byte) int64 {
	t.Helper()
	result := basExec(t, db, `INSERT INTO bas_family_events_v1(event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, eventID, "org", familyID, kind, operationKey, claimedAt, []byte{hashByte}, bytes.Repeat([]byte{hashByte}, 32), createdAt)
	ordinal, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return ordinal
}

func assertBasFamilyEventOrder(t *testing.T, db *Database, expected []string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT event_id,event_ordinal FROM bas_family_events_v1 WHERE family_id='0199c101-0000-7000-8000-000000000005' ORDER BY event_ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for i, want := range expected {
		if !rows.Next() {
			t.Fatalf("missing event ordinal %d", i+1)
		}
		var got string
		var ordinal int64
		if err := rows.Scan(&got, &ordinal); err != nil {
			t.Fatal(err)
		}
		if got != want || ordinal != int64(i+1) {
			t.Fatalf("event at ordinal %d = %s/%d, want %s/%d", i+1, got, ordinal, want, i+1)
		}
	}
	if rows.Next() {
		t.Fatal("unexpected extra family event")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func basExec(t *testing.T, db *Database, query string, args ...any) interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
} {
	t.Helper()
	result, err := db.ExecContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("Exec(%q): %v", query, err)
	}
	return result
}

func assertBasRowsAffected(t *testing.T, db *Database, want int64, query string, args ...any) {
	t.Helper()
	result := basExec(t, db, query, args...)
	rows, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if rows != want {
		t.Fatalf("compare-and-swap changed %d rows, want %d: %s", rows, want, query)
	}
}
