package migrations

import (
	"strings"
	"testing"
)

func TestEmbeddedSchemaDeclaresOwnedPlatformAndLedgerTables(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	platformTables := []string{
		"schema_migrations", "header_operation_ids", "workspace_metadata", "users", "roles",
		"user_roles", "user_password_history", "application_sessions", "totp_factors",
		"factor_assertions", "command_idempotency", "command_idempotency_v1", "recovery_state", "attempt_journal_anchors",
		"idempotency_records", "audit_envelopes",
		"audit_mirror_metadata", "jobs", "job_checkpoints", "backup_evidence",
		"restore_evidence", "organisation_evidence_objects", "organisation_verifications",
	}
	ledgerTables := []string{
		"organisations", "accounts", "accounting_periods", "opening_conversions",
		"opening_items", "journals", "journal_lines", "tax_facts", "cash_flow_facts",
		"rule_bundles", "tax_code_catalogue", "financial_revisions", "financial_revision_claims",
	}
	assertCreatesTables(t, string(steps[0].SQL), platformTables)
	assertCreatesTables(t, string(steps[1].SQL), ledgerTables)
	assertCreatesTables(t, string(steps[3].SQL), []string{"pre_restore_archives_v1", "pre_restore_archive_export_jobs_v1"})
	assertCreatesTables(t, string(steps[4].SQL), []string{"documents"})
}

func TestRetainedCommandIdempotencyIsTheOnlyAuthorityForCommandOwnedEvidence(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	allSchema := strings.ToLower(string(steps[0].SQL) + string(steps[1].SQL) + string(steps[2].SQL))
	if strings.Contains(allSchema, "references idempotency_records(operation_key)") {
		t.Fatal("command-owned evidence still references the superseded idempotency authority")
	}
	for _, fragment := range []string{
		"references command_idempotency_v1(operation_key)",
		"unique (operation_key)",
	} {
		if !strings.Contains(allSchema, fragment) {
			t.Errorf("retained command idempotency schema missing %q", fragment)
		}
	}
}

func TestDocumentSchemaRetainsEncryptedWorkspaceEvidenceAndReviewState(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(string(steps[4].SQL))
	for _, fragment := range []string{
		"operation_key text not null unique",
		"original_bytes blob not null",
		"unique (organisation_id, sha256)",
		"documents_immutable_source",
		"documents_review_transition",
		"documents_immutable_delete",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("migration 5 missing document schema fragment %q", fragment)
		}
	}
}

func TestTask7MigrationCapsSharedJobProtobufBlobs(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(string(steps[3].SQL))
	for _, fragment := range []string{
		"jobs_proto_size_insert", "jobs_proto_size_update", "job_checkpoints_proto_size_insert",
		"length(new.payload_proto) not between 1 and 1048576",
		"length(new.progress_proto) not between 1 and 1048576",
		"length(new.result_proto) not between 1 and 1048576",
		"length(new.checkpoint_proto) not between 1 and 1048576",
		"length(new.checkpoint_sha256) != 64",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("migration 4 missing shared-job bound %q", fragment)
		}
	}
}

func TestTask9LedgerSchemaPersistsGeneratedOrganisationAndAccountFields(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(string(steps[1].SQL))
	for _, fragment := range []string{
		"display_name text not null",
		"entity_type text not null",
		"gst_basis integer not null",
		"gst_reporting_frequency integer not null",
		"financial_year_end_month integer not null",
		"owner_user_id text not null",
		"active_tax_rule_type text not null",
		"active_tax_rule_content_hash blob not null",
		"subtype text",
		"default_tax_code_id text",
		"report_classification text not null",
		"cash_flow_classification text not null",
		"id text not null unique",
		"accounts_control_fields_immutable",
		"organisations_singleton_insert",
		"rule_bundles_immutable_update",
		"tax_code_catalogue_immutable_delete",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("migration 2 missing Task9 schema fragment %q", fragment)
		}
	}
}

func TestTask10LedgerSchemaRetainsImmutablePostingAndFactProvenance(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(string(steps[1].SQL))
	for _, fragment := range []string{
		"total_debits_minor integer not null",
		"financial_revision integer not null",
		"tax_rule_content_hash blob",
		"original_gross_minor integer not null",
		"attributed_gst_minor integer not null",
		"remaining_gst_minor integer not null",
		"unique (journal_line_id, sequence)",
		"journals_posted_immutable",
		"journal_lines_immutable_update",
		"tax_facts_immutable_delete",
		"cash_flow_facts_immutable_update",
		"journal_posting_must_balance",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("migration 2 missing Task10 schema fragment %q", fragment)
		}
	}
}

func TestTask11LedgerSchemaOwnsOpeningModulesAndPeriodTransitions(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(string(steps[1].SQL))
	for _, fragment := range []string{
		"closed_at text not null",
		"accounting_period_transition_guard",
		"accounting_period_one_closed",
		"opening_conversions_one_current",
		"opening_conversion_transition_guard",
		"opening_items_immutable_update",
		"create table sales_opening_receivables",
		"create table purchase_opening_payables",
		"create table banking_opening_accounts",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("migration 2 missing Task11 schema fragment %q", fragment)
		}
	}
}

func assertCreatesTables(t *testing.T, sql string, tables []string) {
	t.Helper()
	normalized := strings.ToLower(sql)
	for _, table := range tables {
		if !strings.Contains(normalized, "create table "+table) &&
			!strings.Contains(normalized, "create table if not exists "+table) {
			t.Errorf("migration does not create table %q", table)
		}
	}
}
