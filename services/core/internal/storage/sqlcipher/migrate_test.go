//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
)

func TestApplyMigrationsSupportsEveryEncryptedSchemaPrefix(t *testing.T) {
	t.Parallel()
	for _, target := range []uint32{1, 2} {
		t.Run(fmt.Sprintf("prefix_%d", target), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database, err := Open(ctx, filepath.Join(t.TempDir(), "workspace.db"), testKey(byte(0x80+target)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			if err := applyMigrations(ctx, database, allMigrations(t), target); err != nil {
				t.Fatal(err)
			}
			assertAppliedVersions(t, database, target)
			assertTablesPresent(t, database, expectedTables(target))
			if err := migrationRelationalIntegrityCheck(ctx, database); err != nil {
				t.Fatalf("schema prefix relational integrity: %v", err)
			}
			if target == 1 {
				assertTablesAbsent(t, database, []string{"organisations", "accounts", "journals"})
			}
		})
	}
}

func TestApplyMigrationsRejectsChecksumDriftBeforeApplyingNextPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "checksum.db"), testKey(0x91))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	steps := allMigrations(t)
	if err := applyMigrations(ctx, database, steps, 1); err != nil {
		t.Fatal(err)
	}
	steps[0].SHA256 = strings.Repeat("0", 64)
	if err := applyMigrations(ctx, database, steps, 2); !errors.Is(err, ErrMigrationChecksum) {
		t.Fatalf("checksum error = %v, want ErrMigrationChecksum", err)
	}
	assertAppliedVersions(t, database, 1)
	assertTablesAbsent(t, database, []string{"organisations"})
}

func TestApplyMigrationsRollsBackFailedStepAndVersionRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "rollback.db"), testKey(0x92))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	steps := allMigrations(t)
	if err := applyMigrations(ctx, database, steps, 2); err != nil {
		t.Fatal(err)
	}
	badSQL := []byte("CREATE TABLE migration_rollback_probe(id INTEGER PRIMARY KEY); SELECT * FROM missing_table;")
	digest := sha256.Sum256(badSQL)
	steps = append(steps, migrations.Migration{
		Version: 3,
		Name:    "0003_injected_failure.sql",
		SHA256:  hex.EncodeToString(digest[:]),
		SQL:     badSQL,
	})
	if err := applyMigrations(ctx, database, steps, 3); err == nil {
		t.Fatal("failed migration succeeded")
	}
	assertAppliedVersions(t, database, 2)
	assertTablesAbsent(t, database, []string{"migration_rollback_probe"})
}

func TestSplitMigrationSQLUsesCompleteSingleStatements(t *testing.T) {
	t.Parallel()
	source := []byte(`
-- a semicolon in a comment; is inert
CREATE TABLE quoted(value TEXT NOT NULL);
INSERT INTO quoted(value) VALUES ('semi;colon');
CREATE TRIGGER quoted_guard BEFORE UPDATE ON quoted
BEGIN
  SELECT CASE WHEN NEW.value = 'blocked;value' THEN RAISE(ABORT, 'blocked;value') END;
END;
`)
	statements, err := splitMigrationSQL(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("statement count = %d, want 3: %#v", len(statements), statements)
	}
	for index, statement := range statements {
		if strings.TrimSpace(statement) == "" || !strings.HasSuffix(strings.TrimSpace(statement), ";") {
			t.Fatalf("statement %d is incomplete: %q", index, statement)
		}
	}
	for _, invalid := range [][]byte{
		{},
		[]byte("-- comment only;\n"),
		[]byte("CREATE TABLE unfinished("),
		[]byte("CREATE TABLE nul(value TEXT);\x00DROP TABLE nul;"),
		[]byte("INSERT INTO quoted(value) VALUES ('unterminated);"),
	} {
		if _, err := splitMigrationSQL(invalid); err == nil {
			t.Fatalf("invalid migration SQL accepted: %q", invalid)
		}
	}
}

func TestLedgerSchemaEnforcesPostingAndOwnershipInvariants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "ledger-schema.db"), testKey(0x93))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := applyMigrations(ctx, database, allMigrations(t), 2); err != nil {
		t.Fatal(err)
	}
	mustExec(t, database, `INSERT INTO organisations(id, legal_name, status, created_at) VALUES ('org-1','Tammy Pty Ltd','ACTIVE','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, created_at) VALUES ('account-1','org-1','1000','Cash','ASSET','DEBIT','ACTIVE','ORDINARY','2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, created_at) VALUES ('account-2','org-1','1000','Duplicate','ASSET','DEBIT','ACTIVE','ORDINARY','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, created_at) VALUES ('account-2','org-1','2000','Equity','EQUITY','CREDIT','ACTIVE','ORDINARY','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('journal-1','org-1','MANUAL','source-1',1,'DRAFT','2026-08-04','Opening','2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, posted_at, created_at) VALUES ('journal-direct-post','org-1','MANUAL','source-direct',1,'POSTED','2026-08-04','Bypass','2026-08-04T00:00:00Z','2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('journal-duplicate','org-1','MANUAL','source-1',1,'DRAFT','2026-08-04','Duplicate','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('line-1','journal-1',1,'account-1',100,0,'AUD')`)
	assertExecFails(t, database, `UPDATE journals SET state='POSTED', posted_at='2026-08-04T00:00:00Z' WHERE id='journal-1'`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('line-2','journal-1',2,'account-2',0,100,'AUD')`)
	mustExec(t, database, `UPDATE journals SET state='POSTED', posted_at='2026-08-04T00:00:00Z' WHERE id='journal-1'`)
	assertExecFails(t, database, `UPDATE journal_lines SET debit_minor=99 WHERE id='line-1'`)
	assertExecFails(t, database, `DELETE FROM journal_lines WHERE id='line-1'`)
	assertExecFails(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('line-3','journal-1',3,'account-1',1,0,'AUD')`)
	assertExecFails(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('invalid-reversal','org-1','REVERSAL','missing-link',1,'DRAFT','2026-08-04','Invalid reversal','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, reversal_of_journal_id, created_at) VALUES ('journal-reversal','org-1','REVERSAL','reversal-1',1,'DRAFT','2026-08-04','Reverse opening','journal-1','2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, reversal_of_journal_id, created_at) VALUES ('journal-second-reversal','org-1','REVERSAL','reversal-2',1,'DRAFT','2026-08-04','Second reversal','journal-1','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('reversal-line-1','journal-reversal',1,'account-2',100,0,'AUD')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('reversal-line-2','journal-reversal',2,'account-1',0,100,'AUD')`)
	mustExec(t, database, `UPDATE journals SET state='POSTED', posted_at='2026-08-04T00:01:00Z' WHERE id='journal-reversal'`)
	mustExec(t, database, `UPDATE journals SET state='REVERSED', reversed_by_journal_id='journal-reversal' WHERE id='journal-1'`)
	assertExecFails(t, database, `UPDATE journals SET description='mutated' WHERE id='journal-1'`)
	assertExecFails(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, created_at) VALUES ('bad','org-1','9999','Bad','INVALID','DEBIT','ACTIVE','ORDINARY','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO organisations(id, legal_name, status, created_at) VALUES ('org-2','Other Pty Ltd','ACTIVE','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, created_at) VALUES ('other-account','org-2','1000','Other Cash','ASSET','DEBIT','ACTIVE','ORDINARY','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('journal-2','org-1','MANUAL','source-2',1,'DRAFT','2026-08-04','Ownership','2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('wrong-owner','journal-2',1,'other-account',100,0,'AUD')`)
	assertExecFails(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('wrong-currency','journal-2',1,'account-1',100,0,'USD')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('ownership-line-1','journal-2',1,'account-1',100,0,'AUD')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('ownership-line-2','journal-2',2,'account-2',0,100,'AUD')`)
	assertExecFails(t, database, `INSERT INTO tax_facts(id, organisation_id, journal_line_id, tax_code, treatment, taxable_minor, tax_minor, source_revision, created_at) VALUES ('wrong-tax-owner','org-2','ownership-line-1','GST','TAXABLE',100,10,1,'2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO cash_flow_facts(id, organisation_id, journal_line_id, category, amount_minor, source_revision, created_at) VALUES ('wrong-cash-owner','org-2','ownership-line-1','OPERATING',100,1,'2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO tax_facts(id, organisation_id, journal_line_id, tax_code, treatment, taxable_minor, tax_minor, source_revision, created_at) VALUES ('tax-fact-1','org-1','ownership-line-1','GST','TAXABLE',100,10,1,'2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO cash_flow_facts(id, organisation_id, journal_line_id, category, amount_minor, source_revision, created_at) VALUES ('cash-fact-1','org-1','ownership-line-1','OPERATING',100,1,'2026-08-04T00:00:00Z')`)
	mustExec(t, database, `UPDATE journals SET state='POSTED', posted_at='2026-08-04T00:02:00Z' WHERE id='journal-2'`)
	assertExecFails(t, database, `UPDATE tax_facts SET tax_minor=11 WHERE id='tax-fact-1'`)
	assertExecFails(t, database, `DELETE FROM cash_flow_facts WHERE id='cash-fact-1'`)
	mustExec(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('journal-other','org-2','OPENING','source-other',1,'DRAFT','2026-08-04','Other opening','2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, journal_id, created_at) VALUES ('wrong-opening-journal','org-1','2026-08-04','DRAFT','0000000000000000000000000000000000000000000000000000000000000000','journal-other','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('journal-opening','org-1','OPENING','source-opening',1,'DRAFT','2026-08-04','Opening conversion','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('opening-line-1','journal-opening',1,'account-1',100,0,'AUD')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('opening-line-2','journal-opening',2,'account-2',0,100,'AUD')`)
	mustExec(t, database, `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, journal_id, created_at) VALUES ('opening-1','org-1','2026-08-04','DRAFT','0000000000000000000000000000000000000000000000000000000000000000','journal-opening','2026-08-04T00:00:00Z')`)
	assertExecFails(t, database, `INSERT INTO opening_items(id, conversion_id, account_id, item_kind, debit_minor, credit_minor, currency_code) VALUES ('wrong-opening-account','opening-1','other-account','ORDINARY',100,0,'AUD')`)
	mustExec(t, database, `INSERT INTO opening_items(id, conversion_id, account_id, item_kind, debit_minor, credit_minor, currency_code) VALUES ('opening-item-1','opening-1','account-1','ORDINARY',100,0,'AUD')`)
	mustExec(t, database, `UPDATE journals SET state='POSTED', posted_at='2026-08-04T00:03:00Z' WHERE id='journal-opening'`)
	mustExec(t, database, `UPDATE opening_conversions SET state='POSTED', financial_revision=1 WHERE id='opening-1'`)
	assertExecFails(t, database, `UPDATE opening_items SET debit_minor=99 WHERE id='opening-item-1'`)
	assertExecFails(t, database, `UPDATE opening_conversions SET conversion_date='2026-08-05' WHERE id='opening-1'`)
	assertExecFails(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, owner_module, created_at) VALUES ('wrong-module','org-1','9998','Wrong Owner','ASSET','DEBIT','ACTIVE','ORDINARY','banking','2026-08-04T00:00:00Z')`)
}

func TestLedgerOwnershipKeysCannotBeReparented(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "ledger-reparent.db"), testKey(0x94))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := applyMigrations(ctx, database, allMigrations(t), 2); err != nil {
		t.Fatal(err)
	}
	mustExec(t, database, `INSERT INTO organisations(id, legal_name, status, created_at) VALUES ('org-1','Tammy Pty Ltd','ACTIVE','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO organisations(id, legal_name, status, created_at) VALUES ('org-2','Other Pty Ltd','ACTIVE','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, created_at) VALUES ('account-1','org-1','1000','Cash','ASSET','DEBIT','ACTIVE','ORDINARY','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('journal-1','org-1','MANUAL','source-1',1,'DRAFT','2026-08-04','Draft','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journal_lines(id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code) VALUES ('line-1','journal-1',1,'account-1',100,0,'AUD')`)
	mustExec(t, database, `INSERT INTO tax_facts(id, organisation_id, journal_line_id, tax_code, treatment, taxable_minor, tax_minor, source_revision, created_at) VALUES ('tax-1','org-1','line-1','GST','TAXABLE',100,10,1,'2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO accounts(id, organisation_id, code, name, account_type, normal_balance, status, designation, created_at) VALUES ('account-2','org-2','1000','Other Cash','ASSET','DEBIT','ACTIVE','ORDINARY','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO journals(id, organisation_id, source_type, source_id, source_revision, state, journal_date, description, created_at) VALUES ('journal-2','org-2','MANUAL','source-2',1,'DRAFT','2026-08-04','Other Draft','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, created_at) VALUES ('opening-1','org-1','2026-08-04','DRAFT','0000000000000000000000000000000000000000000000000000000000000000','2026-08-04T00:00:00Z')`)
	mustExec(t, database, `INSERT INTO opening_items(id, conversion_id, account_id, item_kind, debit_minor, credit_minor, currency_code) VALUES ('opening-item-1','opening-1','account-1','ORDINARY',100,0,'AUD')`)
	mustExec(t, database, `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, created_at) VALUES ('opening-2','org-2','2026-08-04','DRAFT','1111111111111111111111111111111111111111111111111111111111111111','2026-08-04T00:00:00Z')`)

	for name, statement := range map[string]string{
		"journal":                        `UPDATE journals SET organisation_id='org-2' WHERE id='journal-1'`,
		"account":                        `UPDATE accounts SET organisation_id='org-2' WHERE id='account-1'`,
		"opening conversion":             `UPDATE opening_conversions SET organisation_id='org-2' WHERE id='opening-1'`,
		"journal line carrying facts":    `UPDATE journal_lines SET journal_id='journal-2', account_id='account-2' WHERE id='line-1'`,
		"self replacement conversion":    `UPDATE opening_conversions SET replaced_by_id='opening-1' WHERE id='opening-1'`,
		"cross-organisation replacement": `UPDATE opening_conversions SET replaced_by_id='opening-2' WHERE id='opening-1'`,
		"direct posted opening insert":   `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, financial_revision, created_at) VALUES ('opening-direct-posted','org-1','2026-08-04','POSTED','2222222222222222222222222222222222222222222222222222222222222222',1,'2026-08-04T00:00:00Z')`,
		"direct replaced opening insert": `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, created_at) VALUES ('opening-direct-replaced','org-1','2026-08-04','REPLACED','3333333333333333333333333333333333333333333333333333333333333333','2026-08-04T00:00:00Z')`,
		"self replacement insert":        `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, replaced_by_id, created_at) VALUES ('opening-self','org-1','2026-08-04','DRAFT','4444444444444444444444444444444444444444444444444444444444444444','opening-self','2026-08-04T00:00:00Z')`,
		"cross-owner replacement insert": `INSERT INTO opening_conversions(id, organisation_id, conversion_date, state, source_sha256, replaced_by_id, created_at) VALUES ('opening-cross','org-1','2026-08-04','DRAFT','5555555555555555555555555555555555555555555555555555555555555555','opening-2','2026-08-04T00:00:00Z')`,
	} {
		t.Run(name, func(t *testing.T) {
			transaction, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transaction.ExecContext(ctx, statement); err == nil {
				t.Errorf("ownership reparenting succeeded: %s", statement)
			}
			if err := transaction.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMigrateWorkspaceCopyActivatesAndRetainsEncryptedPredecessor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "workspace.db")
	key := testKey(0xa1)
	created, err := MigrateWorkspace(ctx, path, key, 1)
	if err != nil {
		t.Fatal(err)
	}
	if created.PredecessorPath != "" {
		t.Fatalf("fresh predecessor = %q", created.PredecessorPath)
	}
	database, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, database, `INSERT INTO workspace_metadata(key,value,updated_at) VALUES ('marker',x'010203','2026-08-04T00:00:00Z')`)
	if err := database.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := MigrateWorkspace(ctx, path, key, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.PredecessorPath == "" || result.ActivePath != path || result.Version != 2 {
		t.Fatalf("migration result = %#v", result)
	}
	if result.StagedPath != "" {
		t.Fatalf("successful migration reported nonexistent staged residue %q", result.StagedPath)
	}
	predecessor, err := os.ReadFile(result.PredecessorPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(predecessor) != string(original) {
		t.Fatal("predecessor is not the byte-identical original encrypted file")
	}
	assertEncryptedWorkspaceMarker(t, result.PredecessorPath, key, 1)
	assertEncryptedWorkspaceMarker(t, path, key, 2)
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) == string(original) {
		t.Fatal("active file did not change after the second migration")
	}
}

func TestMigrateWorkspaceHoldsExclusiveWriterBarrierThroughActivation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := testKey(0xa2)
	if _, err := MigrateWorkspace(ctx, path, key, 1); err != nil {
		t.Fatal(err)
	}
	openWriter, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateWorkspace(ctx, path, key, 2); !errors.Is(err, ErrWorkspaceLocked) {
		t.Fatalf("migration with open writer error = %v, want ErrWorkspaceLocked", err)
	}
	mustExec(t, openWriter, `INSERT INTO workspace_metadata(key,value,updated_at) VALUES ('marker',x'010203','2026-08-04T00:00:00Z')`)
	if err := openWriter.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := openWriter.Close(); err != nil {
		t.Fatal(err)
	}

	errInterrupted := errors.New("stop after lock proof")
	result, err := migrateWorkspace(ctx, path, key, workspaceMigrationOptions{
		steps: allMigrations(t), target: 2,
		hooks: migrationBoundaryHooks{afterStagedIntegrity: func() error {
			concurrentWriter, openErr := Open(ctx, path, key)
			if concurrentWriter != nil {
				_ = concurrentWriter.Close()
				t.Fatal("concurrent writer opened during migration")
			}
			if !errors.Is(openErr, ErrWorkspaceLocked) {
				t.Fatalf("concurrent writer error = %v, want ErrWorkspaceLocked", openErr)
			}
			return errInterrupted
		}},
	})
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("interrupted migration error = %v", err)
	}
	if result.StagedPath == "" {
		t.Fatal("interrupted migration did not retain encrypted stage")
	}
	assertEncryptedWorkspaceMarker(t, path, key, 1)
	if _, err := MigrateWorkspace(ctx, path, key, 2); err != nil {
		t.Fatal(err)
	}
	assertEncryptedWorkspaceMarker(t, path, key, 2)
}

func TestMigrateWorkspaceFailuresNeverMutateTheOnlyEncryptedFile(t *testing.T) {
	t.Parallel()
	errInterrupted := errors.New("migration interrupted")
	for _, test := range []struct {
		name      string
		mutate    func([]migrations.Migration) []migrations.Migration
		hooks     migrationBoundaryHooks
		target    uint32
		wantPre   bool
		wantStage bool
	}{
		{
			name: "checksum_drift",
			mutate: func(steps []migrations.Migration) []migrations.Migration {
				steps[0].SHA256 = strings.Repeat("0", 64)
				return steps
			},
			target: 2,
		},
		{
			name: "transaction_rollback",
			mutate: func(steps []migrations.Migration) []migrations.Migration {
				badSQL := []byte("CREATE TABLE copy_rollback_probe(id INTEGER PRIMARY KEY); SELECT * FROM absent_copy_table;")
				digest := sha256.Sum256(badSQL)
				return append(steps, migrations.Migration{Version: 4, Name: "0004_bad.sql", SHA256: hex.EncodeToString(digest[:]), SQL: badSQL})
			},
			target:    4,
			wantStage: true,
		},
		{
			name:   "after_staged_integrity",
			mutate: func(steps []migrations.Migration) []migrations.Migration { return steps },
			hooks: migrationBoundaryHooks{afterStagedIntegrity: func() error {
				return errInterrupted
			}},
			target:    2,
			wantStage: true,
		},
		{
			name:   "after_predecessor_ready",
			mutate: func(steps []migrations.Migration) []migrations.Migration { return steps },
			hooks: migrationBoundaryHooks{afterPredecessorReady: func() error {
				return errInterrupted
			}},
			target:    2,
			wantPre:   true,
			wantStage: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "workspace.db")
			key := testKey(0xb1)
			if _, err := MigrateWorkspace(ctx, path, key, 1); err != nil {
				t.Fatal(err)
			}
			database, err := Open(ctx, path, key)
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, database, `INSERT INTO workspace_metadata(key,value,updated_at) VALUES ('marker',x'0a0b0c','2026-08-04T00:00:00Z')`)
			if err := database.Checkpoint(ctx); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := migrateWorkspace(ctx, path, key, workspaceMigrationOptions{
				steps:  test.mutate(allMigrations(t)),
				target: test.target,
				hooks:  test.hooks,
			})
			if err == nil {
				t.Fatal("interrupted migration succeeded")
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(after) != string(before) {
				t.Fatal("failed migration changed the only active encrypted file")
			}
			assertEncryptedWorkspaceMarker(t, path, key, 1)
			if test.wantStage {
				if result.StagedPath == "" {
					t.Fatal("failed migration did not report recoverable staged residue")
				}
				if _, statErr := os.Lstat(result.StagedPath); statErr != nil {
					t.Fatalf("staged residue is unavailable: %v", statErr)
				}
			}
			if test.wantPre {
				if result.PredecessorPath == "" {
					t.Fatal("predecessor was not retained before interruption")
				}
				assertEncryptedWorkspaceMarker(t, result.PredecessorPath, key, 1)
			}
		})
	}
}

func assertEncryptedWorkspaceMarker(t *testing.T, path string, key []byte, version uint32) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(contents), "SQLite format 3\x00") {
		t.Fatalf("workspace %q exposes a plaintext SQLite header", path)
	}
	database, err := Open(context.Background(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	assertAppliedVersions(t, database, version)
	var marker []byte
	if err := database.QueryRow(`SELECT value FROM workspace_metadata WHERE key='marker'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if len(marker) != 3 {
		t.Fatalf("marker = %x", marker)
	}
}

func assertAppliedVersions(t *testing.T, database *Database, target uint32) {
	t.Helper()
	rows, err := database.Query(`SELECT version, sha256 FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var count uint32
	for rows.Next() {
		var version uint32
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			t.Fatal(err)
		}
		count++
		if version != count || len(checksum) != 64 {
			t.Fatalf("migration row = version:%d checksum:%q", version, checksum)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != target {
		t.Fatalf("applied version count = %d, want %d", count, target)
	}
}

func allMigrations(t *testing.T) []migrations.Migration {
	t.Helper()
	steps, err := migrations.All()
	if err != nil {
		t.Fatal(err)
	}
	return steps
}

func expectedTables(target uint32) []string {
	platform := []string{"schema_migrations", "users", "jobs", "organisation_evidence_objects"}
	if target == 1 {
		return platform
	}
	return append(platform, "organisations", "accounts", "journals", "journal_lines", "financial_revisions")
}

func assertTablesPresent(t *testing.T, database *Database, tables []string) {
	t.Helper()
	for _, table := range tables {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %q count = %d, want 1", table, count)
		}
	}
}

func assertTablesAbsent(t *testing.T, database *Database, tables []string) {
	t.Helper()
	for _, table := range tables {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("table %q count = %d, want 0", table, count)
		}
	}
}

func mustExec(t *testing.T, database *Database, query string) {
	t.Helper()
	if _, err := database.Exec(query); err != nil {
		t.Fatalf("Exec(%q): %v", query, err)
	}
}

func assertExecFails(t *testing.T, database *Database, query string) {
	t.Helper()
	if _, err := database.Exec(query); err == nil {
		t.Fatalf("Exec(%q) succeeded", query)
	}
}
