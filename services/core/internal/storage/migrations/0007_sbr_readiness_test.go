package migrations

import (
	"strings"
	"testing"
)

func TestSBRReadinessMigrationIsEmbeddedLastAndContainsOnlyRedactedState(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 7 {
		t.Fatalf("migration count = %d, want 7", len(steps))
	}
	if steps[6].Version != 7 || steps[6].Name != "0007_sbr_readiness.sql" {
		t.Fatalf("last migration = %#v, want version 7 SBR readiness", steps[6])
	}

	schema := strings.ToLower(string(steps[6].SQL))
	for _, table := range []string{
		"sbr_credential_bindings_v1",
		"sbr_authenticated_profiles_v1",
		"sbr_readiness_transitions_v1",
		"sbr_mutations_v1",
		"sbr_idempotency_v1",
		"sbr_simulator_transports_v1",
		"sbr_helper_dispatches_v1",
		"sbr_pending_mutation_effects_v1",
	} {
		if !strings.Contains(schema, "create table "+table) {
			t.Errorf("migration 7 does not create %s", table)
		}
	}
	for _, fragment := range []string{
		"workspace_id text not null",
		"foreign key (organisation_id, canonical_abn) references organisations(id, abn) on delete restrict",
		"canonical_abn text not null",
		"schema_version integer not null check (schema_version = 1)",
		"credential_fingerprint blob check (credential_fingerprint is null or (length(credential_fingerprint) = 32 and credential_fingerprint != zeroblob(32)))",
		"check (state in ('prepared','dispatching','not_started','maybe_sent','response_received','accepted','failed','unknown'))",
		"check (binding_state in ('active','reimport_required','removed'))",
		"check (mutation_kind in ('import_credential','replace_credential','remove_credential','import_product_id','remove_product_id'))",
		"check (mutation_state in ('prepared','staged','core_committed','helper_committed','abort_required','aborting','aborted','reconcile_required'))",
		"check (credential_fingerprint is not null or (mutation_kind = 'import_credential' and mutation_state in ('prepared','aborted')))",
		"evidence_sequence integer not null check (evidence_sequence > 0)",
		"conformance text not null check (conformance in ('simulator','pre_conformance','post_conformance'))",
		"retry_of_operation_id text unique references sbr_simulator_transports_v1(operation_id) on delete restrict",
		"pending_terminal_state text check (pending_terminal_state is null or pending_terminal_state in ('accepted','failed'))",
		"pending_result_hash blob check (pending_result_hash is null or (length(pending_result_hash) = 32 and pending_result_hash != zeroblob(32)))",
		"state text not null check (state in ('dispatching','completed','failed','unknown'))",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("migration 7 missing exact invariant %q", fragment)
		}
	}
	if strings.Contains(schema, "original.retry_of_operation_id is null") {
		t.Error("migration 7 prevents a NOT_STARTED retry attempt from owning its own retry")
	}
	for _, forbidden := range []string{
		"credential_bytes", "credential_password", "product_id_value", "selected_local_path",
		"security_scoped_bookmark", "endpoint_url", "private_key", "keychain_path",
	} {
		if strings.Contains(schema, forbidden) {
			t.Errorf("migration 7 contains forbidden secret-bearing field %q", forbidden)
		}
	}
}
