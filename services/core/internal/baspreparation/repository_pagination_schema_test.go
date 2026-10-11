//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestVersionTablesUseDurableImmutableAppendOrdinals(t *testing.T) {
	workspace, _, _ := newBASRepositoryWorkspace(t)
	defer workspace.Database.Close()

	tables := []struct {
		name              string
		noUpdateName      string
		noDeleteName      string
		noConflictingName string
	}{
		{"bas_profile_versions_v1", "bas_profile_versions_v1_no_update", "bas_profile_versions_v1_no_delete", "bas_profile_versions_v1_no_conflicting_insert"},
		{"bas_period_versions_v1", "bas_period_versions_v1_no_update", "bas_period_versions_v1_no_delete", "bas_period_versions_v1_no_conflicting_insert"},
		{"bas_preparation_versions_v1", "bas_preparation_versions_v1_no_update", "bas_preparation_versions_v1_no_delete", "bas_preparation_versions_v1_no_conflicting_insert"},
		{"bas_calculations_v1", "bas_calculations_v1_no_update", "bas_calculations_v1_no_delete", "bas_calculations_v1_no_conflicting_insert"},
		{"bas_branch_versions_v1", "bas_branch_versions_v1_no_update", "bas_branch_versions_v1_no_delete", "bas_branch_versions_v1_no_conflicting_insert"},
		{"bas_frozen_bas_v1", "bas_frozen_bas_v1_no_update", "bas_frozen_bas_v1_no_delete", "bas_frozen_bas_v1_no_conflicting_insert"},
		{"bas_export_events_v1", "bas_export_events_v1_no_update", "bas_export_events_v1_no_delete", "bas_export_events_v1_no_conflicting_insert"},
	}

	for _, table := range tables {
		t.Run(table.name, func(t *testing.T) {
			var createSQL string
			if err := workspace.Database.QueryRowContext(context.Background(),
				"SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table.name).Scan(&createSQL); err != nil {
				t.Fatalf("read %s schema: %v", table.name, err)
			}
			normalizedSQL := strings.Join(strings.Fields(strings.ToUpper(createSQL)), " ")
			if !strings.Contains(normalizedSQL, "APPEND_ORDINAL INTEGER PRIMARY KEY AUTOINCREMENT") {
				t.Fatalf("%s lacks an explicit AUTOINCREMENT primary-key append ordinal: %s", table.name, createSQL)
			}

			rows, err := workspace.Database.QueryContext(context.Background(), "PRAGMA table_info("+table.name+")")
			if err != nil {
				t.Fatalf("inspect %s columns: %v", table.name, err)
			}
			var ordinalColumnFound bool
			for rows.Next() {
				var cid, notNull, primaryKey int
				var name, dataType string
				var defaultValue sql.NullString
				if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
					rows.Close()
					t.Fatalf("scan %s column metadata: %v", table.name, err)
				}
				if name == "append_ordinal" {
					ordinalColumnFound = true
					if dataType != "INTEGER" || primaryKey != 1 {
						t.Errorf("%s.append_ordinal metadata = type %q, pk %d; want INTEGER PRIMARY KEY", table.name, dataType, primaryKey)
					}
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				t.Fatalf("read %s column metadata: %v", table.name, err)
			}
			if err := rows.Close(); err != nil {
				t.Fatalf("close %s column metadata: %v", table.name, err)
			}
			if !ordinalColumnFound {
				t.Errorf("%s has no append_ordinal column", table.name)
			}

			for _, trigger := range []struct {
				name   string
				action string
			}{
				{table.noUpdateName, "BEFORE UPDATE ON"},
				{table.noDeleteName, "BEFORE DELETE ON"},
				{table.noConflictingName, "BEFORE INSERT ON"},
			} {
				var triggerTable, triggerSQL string
				if err := workspace.Database.QueryRowContext(context.Background(),
					"SELECT tbl_name, sql FROM sqlite_master WHERE type='trigger' AND name=?", trigger.name).
					Scan(&triggerTable, &triggerSQL); err != nil {
					t.Fatalf("read immutability trigger %s: %v", trigger.name, err)
				}
				if triggerTable != table.name {
					t.Errorf("trigger %s belongs to %s, want %s", trigger.name, triggerTable, table.name)
				}
				normalizedTriggerSQL := strings.Join(strings.Fields(strings.ToUpper(triggerSQL)), " ")
				if !strings.Contains(normalizedTriggerSQL, trigger.action) {
					t.Errorf("trigger %s SQL %q does not declare %s", trigger.name, triggerSQL, trigger.action)
				}
				if trigger.name == table.noConflictingName &&
					!strings.Contains(normalizedTriggerSQL, "APPEND_ORDINAL=NEW.APPEND_ORDINAL") {
					t.Errorf("trigger %s SQL %q does not reject a conflicting append_ordinal", trigger.name, triggerSQL)
				}
			}
		})
	}
}
