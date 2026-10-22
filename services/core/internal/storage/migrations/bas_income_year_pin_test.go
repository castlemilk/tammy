package migrations

import (
	"strings"
	"testing"
)

func TestBASIncomeYearPinMigrationIsAppendOnlyAfterCurrentHead(t *testing.T) {
	prior, err := Prefix(14)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := Prefix(15)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 15 || steps[14].Name != "0015_bas_income_year_pins.sql" {
		t.Fatalf("income-year pin migration catalog = %#v", steps)
	}
	for index, step := range prior {
		if steps[index].Version != step.Version || steps[index].Name != step.Name || steps[index].SHA256 != step.SHA256 {
			t.Fatalf("historical migration prefix changed at version %d", index+1)
		}
	}
	for _, fragment := range []string{
		"CREATE TABLE bas_income_year_pins_v1",
		"PRIMARY KEY(organisation_id,income_year_id)",
		"bas_income_year_pins_v1_no_update",
		"bas_income_year_pins_v1_no_delete",
		"bas_income_year_pins_v1_no_overlapping_identity",
		"bas_income_year_pins_v1_require_period_profile_pair",
	} {
		if !strings.Contains(string(steps[14].SQL), fragment) {
			t.Fatalf("income-year pin migration omits %q", fragment)
		}
	}
}
