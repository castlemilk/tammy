package migrations

import (
	"strings"
	"testing"
)

func TestAuditIdempotencyMigrationIsForwardOnlyAndPreservesPredecessors(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 4 {
		t.Fatalf("migration count = %d, want 4", len(steps))
	}
	if steps[0].SHA256 != "a3643ab3c6f9d162972beccc41ba75569f66722bb5f8f33e2bff136f9345f4fc" {
		t.Fatalf("0001 checksum changed: %s", steps[0].SHA256)
	}
	if steps[1].SHA256 != "3db2b98e9d515beb814ae4e3aab465d64744adcdf38c2aeefb61e36064f7ccc9" {
		t.Fatalf("0002 checksum changed: %s", steps[1].SHA256)
	}
	if steps[2].Name != "0003_audit_idempotency.sql" {
		t.Fatalf("migration 3 name = %q", steps[2].Name)
	}
	if steps[2].SHA256 != "8cc61fa5d69bac136c7920fae9beabae7230c9c9f3fe02e0ea95f36e23d2cced" {
		t.Fatalf("0003 checksum changed: %s", steps[2].SHA256)
	}
	schema := strings.ToLower(string(steps[2].SQL))
	for _, table := range []string{
		"audit_chain_headers_v1", "audit_events_v1", "audit_signing_keys_v1", "audit_signing_key_state_v1",
		"audit_descriptor_sets_v1", "audit_export_jobs_v1", "command_idempotency_v1",
	} {
		if !strings.Contains(schema, "create table "+table) {
			t.Errorf("migration 3 does not create table %q", table)
		}
	}
	for _, fragment := range []string{
		"semantic_hash_version", "request_type", "normalized_hash", "result_type",
		"result_proto", "actor_user_id", "payload_schema_fingerprint", "payload_proto",
		"payload_json", "canonical_event", "operation_hash", "input_hash",
		"checkpoint_hash", "result_ref", "destination_hash",
		"filter_proto", "snapshot_generation", "snapshot_head", "destination_provider", "evidence_provider",
		"predecessor_key_id", "predecessor_signature", "successor_possession_signature",
		"rotation_prior_sequence", "rotation_prior_head", "active_key_id", "active_epoch",
		"audit_chain_headers_v1_no_conflicting_insert", "audit_events_v1_no_conflicting_insert",
		"audit_chain_headers_v1_no_delete", "audit_chain_headers_v1_linked_advance_only",
		"new.current_sequence is not old.current_sequence + 1", "new.current_head is old.current_head",
		"previous_hash = old.current_head",
		"event_hash = new.current_head", "audit_signing_keys_v1_one_active_per_workspace",
		"where retired_at is null", "audit_signing_keys_v1_no_conflicting_insert",
		"audit_signing_keys_v1_no_delete", "audit_signing_keys_v1_retire_only",
		"audit_signing_key_state_v1_no_conflicting_insert", "audit_signing_key_state_v1_no_delete",
		"audit_signing_key_state_v1_linked_advance_only", "rotation_event.event_type = 15",
		"rotation_event.occurred_at = successor.created_at", "rotation_header.current_head = rotation_event.event_hash",
		"substr(new.retired_at, 12, 2) not between '00' and '23'", "strftime(",
		"length(created_at) = 30", "length(encrypted_private_key) = 80",
		"retired_at is null or (", "retired_at > created_at",
		"foreign key (workspace_id, predecessor_key_id)", "foreign key (workspace_id, signing_key_id)",
		"audit_descriptor_sets_v1_no_conflicting_insert", "audit_descriptor_sets_v1_no_update",
		"audit_descriptor_sets_v1_no_delete", "fingerprint blob not null primary key", "length(fingerprint) = 32",
		"length(descriptor_set) between 1 and 67108864",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("migration 3 missing required column %q", fragment)
		}
	}
}
