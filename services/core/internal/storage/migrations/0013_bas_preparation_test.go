package migrations

import "testing"

func TestBASPreparationMigrationPreservesAcceptedPurchaseSchema(t *testing.T) {
	steps, err := Prefix(13)
	if err != nil {
		t.Fatalf("BAS preparation migration unavailable: %v", err)
	}
	if len(steps) != 13 || steps[12].Name != "0013_bas_preparation.sql" {
		t.Fatalf("BAS preparation migration catalogue: %#v", steps)
	}
	// Migration 12 was accepted with the purchase phase. Older prefix hashes
	// remain covered by TestPurchaseMigrationIsAvailableWithoutChangingHistoricalPrefix.
	if steps[11].SHA256 != "40d634b44d8ef8e038cf5dc26686f2ab83207e2bf6f869dc238bd51b3afc77d1" {
		t.Fatalf("accepted purchase migration changed: %s", steps[11].SHA256)
	}
	prior, err := Prefix(12)
	if err != nil {
		t.Fatal(err)
	}
	for index, step := range prior {
		if steps[index].Name != step.Name || steps[index].Version != step.Version || steps[index].SHA256 != step.SHA256 {
			t.Fatalf("historical prefix changed at version %d", index+1)
		}
	}
}
