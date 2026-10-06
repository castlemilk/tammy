package migrations

import (
	"strings"
	"testing"
)

func TestInvoiceBrandingMigrationRetainsImmutableAssetsAndCommandEvidence(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 12 {
		t.Fatalf("migration count=%d, want 12", len(steps))
	}
	schema := strings.ToLower(string(steps[8].SQL))
	for _, invariant := range []string{"create table invoice_templates_v1", "create table invoice_logo_assets_v1", "create table invoice_branding_command_audit_v1", "invoice_logo_assets_v1_no_update", "invoice_logo_assets_v1_no_delete", "invoice_templates_v1_version_guard", "1048576", "4000000"} {
		if !strings.Contains(schema, invariant) {
			t.Errorf("missing invariant %s", invariant)
		}
	}
}
