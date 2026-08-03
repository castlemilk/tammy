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
		"factor_assertions", "command_idempotency", "recovery_state", "attempt_journal_anchors",
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
