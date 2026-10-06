package migrations

import "testing"

func TestPurchaseMigrationIsAvailableWithoutChangingHistoricalPrefix(t *testing.T) {
	steps, err := Prefix(12)
	if err != nil {
		t.Fatalf("purchase migration unavailable: %v", err)
	}
	if len(steps) != 12 || steps[11].Name != "0012_purchase_payments_gst.sql" {
		t.Fatalf("purchase migration catalogue: %#v", steps)
	}
	// Frozen from the reviewed contract commit 62363327, before migration 12.
	want := map[string]string{
		"0001_platform.sql":             "034ebfd1359799f3c9da73dd36b8882175f78b7fc4337993e4ab474c42d3834d",
		"0002_ledger.sql":               "61ea34dbe8c0ba954610de21e76bc932fb1cf0b80d024c227fa240327930aeff",
		"0003_audit_idempotency.sql":    "8143516aa6643d4a70915e910227a6f36e3d7d7cfc5673ba59641e06aa9c2953",
		"0004_pre_restore_archives.sql": "7efaeadd639a8bfbcf5bc793040365d0d645fc93f6cff92904f3534a23710816",
		"0005_documents.sql":            "6dee7df100f3ce4b3ab242f2caf60a000c8ea0fe50857d5a0213e2fbf2974688",
		"0006_banking_reporting.sql":    "e1e57ab77f7e74e337e8edc9f0631e34afbdb0c31c0dcdddf610e7d4f914fcae",
		"0007_sbr_readiness.sql":        "d07b5eff3cb5e2c5dcc58e86408ef0fb3e7f81a7a818b397688250a99ce29735",
		"0008_invoices.sql":             "7ca46902adafcd0b43b54c9b744ba6220180c3328546cf46a5049e1ff38f0bcc",
		"0009_invoice_branding.sql":     "e92d6c0213ca97cf399a070877edd6b1d49a3a8463724782ccaddca9ee5ac375",
		"0010_invoice_income.sql":       "d993dc2b988587fab9f22585820be9e7a6a015faae2ab178145a163a39f307f2",
		"0011_bas_save_order.sql":       "6d1b56d144b4ede746214b5f706fbe2f764120d5ea570c43bab8827d82a9dd3e",
	}
	for _, step := range steps[:11] {
		if step.SHA256 != want[step.Name] {
			t.Fatalf("historical migration %s changed: %s", step.Name, step.SHA256)
		}
	}
}
