package migrations

import (
	"strings"
	"testing"
)

func TestInvoiceMigrationPreservesIssuedDocumentsAndCommandAudit(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 9 {
		t.Fatalf("migration count=%d, want 9", len(steps))
	}
	schema := strings.ToLower(string(steps[7].SQL))
	for _, fragment := range []string{"create table invoices_v1", "create table invoice_command_audit_v1", "where number != ''", "old.state = 'issued'", "invoice_command_audit_v1_no_update", "invoice_command_audit_v1_no_delete"} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("missing invariant %s", fragment)
		}
	}
}
