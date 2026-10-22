//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestTaxFactBASKeysReserveBeforeReadingAndRetainIndependentFacts(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	hash := sha256.Sum256([]byte("scalar source"))
	sharedSource := taxFactReportingID(17199)
	posted := f.post(t, 17101, sharedSource, hash[:], hash[:])
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
		taxFactReportingID(17120), "SYSTEM", 0, "draft-source", taxFactReportingID(17121),
		taxFactReportingID(17122), sharedSource, "ADJUSTMENT", nil)
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
		taxFactReportingID(17130), "PAYABLE", 6, "purchase-source", taxFactReportingID(17131),
		taxFactReportingID(17132), sharedSource, "ADJUSTMENT", make([]byte, sha256.Size))
	state := journalReportingPreflightState(t, f)
	probe := &taxFactBASKeyProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || summary == nil {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	if summary.OrganisationID != accountTestOrganisationID || summary.RootCount != 0 || summary.ReceiptCount != 0 || summary.ScalarKeyCount != 3 || probe.keyReads != 0 || probe.bodyReads != 0 {
		t.Fatalf("aggregate-only summary = %#v; key/body reads %d/%d", summary, probe.keyReads, probe.bodyReads)
	}
	keys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 3, summary.ScalarBytes)
	if err != nil || len(keys) != 3 {
		t.Fatalf("keys = %#v, %v", keys, err)
	}
	var charged int64
	for index, key := range keys {
		if index > 0 && keys[index-1].ID >= key.ID {
			t.Fatal("keys are not sorted by exact fact identity")
		}
		if key.OrganisationID != accountTestOrganisationID || key.JournalID == "" || key.LineNumber < 1 || key.SourceID != sharedSource || key.SourceRevision != 1 {
			t.Fatalf("lost identity or source provenance: %#v", key)
		}
		charged += 512 + int64(len(key.ID)+len(key.OrganisationID)+len(key.JournalLineID)+len(key.JournalID)+len(key.OrganisationID)+len(key.SourceType)+len(key.SourceID)+len(key.SourceContentHash))
	}
	if charged != summary.ScalarBytes {
		t.Fatalf("charged %d, reserved %d", charged, summary.ScalarBytes)
	}
	if keys[0].ID != posted.factID || keys[0].JournalLineID != posted.journal.Lines[1].Id || keys[0].LineNumber != 2 || !keys[0].SourceContentHashPresent || !bytes.Equal(keys[0].SourceContentHash, hash[:]) {
		t.Fatalf("actual writer key = %#v", keys[0])
	}
	if keys[1].SourceContentHashPresent || keys[1].SourceContentHash != nil || !keys[2].SourceContentHashPresent || !bytes.Equal(keys[2].SourceContentHash, make([]byte, sha256.Size)) {
		t.Fatalf("NULL and present zero hash collapsed: %#v %#v", keys[1], keys[2])
	}
	if probe.keyReads != 1 || probe.bodyReads != 0 || !f.tx.Authenticated() || !equalJournalReportingPreflightState(state, journalReportingPreflightState(t, f)) {
		t.Fatalf("scalar discovery hydrated bodies or changed caller transaction: key/body reads %d/%d", probe.keyReads, probe.bodyReads)
	}
	for _, limit := range []struct {
		name          string
		count, budget int64
	}{{"one-short count", 2, summary.ScalarBytes}, {"one-short bytes", 3, summary.ScalarBytes - 1}} {
		t.Run(limit.name, func(t *testing.T) {
			probe.keyReads = 0
			got, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, limit.count, limit.budget)
			if got != nil || !errors.Is(err, accounting.ErrTaxFactReportingBound) || probe.keyReads != 0 {
				t.Fatalf("bound result present=%t error=%v key reads=%d", got != nil, err, probe.keyReads)
			}
		})
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	probe.keyReads = 0
	if got, err := repository.ReadTaxFactBASKeys(ctx, accountTestOrganisationID, *summary, 3, summary.ScalarBytes); got != nil || !errors.Is(err, context.Canceled) || probe.keyReads != 0 {
		t.Fatalf("cancelled read = %#v, %v", got, err)
	}
}

func TestTaxFactBASKeysRejectInvalidStorageAndParentsBeforeKeyReads(t *testing.T) {
	for _, testCase := range []struct {
		name, table, update string
		args                []any
		bound               bool
		ignoreChecks        bool
	}{
		{"source revision blob", "tax_facts", `UPDATE tax_facts SET source_revision=zeroblob(8388609) WHERE id=?`, nil, false, false},
		{"source revision text", "tax_facts", `UPDATE tax_facts SET source_revision=? WHERE id=?`, []any{strings.Repeat("x", 8388609)}, false, false},
		{"source revision fraction", "tax_facts", `UPDATE tax_facts SET source_revision=1.5 WHERE id=?`, nil, false, false},
		{"source revision zero", "tax_facts", `UPDATE tax_facts SET source_revision=0 WHERE id=?`, nil, false, true},
		{"line number fraction", "journal_lines", `UPDATE journal_lines SET line_number=1.5 WHERE id=?`, nil, false, false},
		{"line number blob", "journal_lines", `UPDATE journal_lines SET line_number=zeroblob(8388609) WHERE id=?`, nil, false, false},
		{"foreign journal owner", "journals", `UPDATE journals SET organisation_id=? WHERE id=?`, []any{taxFactReportingID(17299)}, false, false},
		{"missing journal", "journal_lines", `UPDATE journal_lines SET journal_id=? WHERE id=?`, []any{"missing-parent"}, false, false},
		{"missing line", "tax_facts", `UPDATE tax_facts SET journal_line_id=? WHERE id=?`, []any{"missing-line"}, false, false},
		{"empty present hash", "tax_facts", `UPDATE tax_facts SET source_content_hash=zeroblob(0) WHERE id=?`, nil, false, true},
		{"oversized scalar ID", "tax_facts", `UPDATE tax_facts SET source_id=? WHERE id=?`, []any{strings.Repeat("x", 8388609)}, true, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f := newTaxFactReportingFixture(t)
			journalID, lineID, factID := taxFactReportingID(17201), taxFactReportingID(17202), taxFactReportingID(17203)
			seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now, journalID, "SYSTEM", 0, "source", lineID, factID, "evidence", "ADJUSTMENT", nil)
			probe := &taxFactBASKeyProbe{Transaction: f.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
			if err != nil {
				t.Fatal(err)
			}
			rowID := factID
			if testCase.table == "journals" {
				rowID = journalID
			}
			if testCase.table == "journal_lines" {
				rowID = lineID
			}
			if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
				t.Fatal(err)
			}
			if testCase.ignoreChecks {
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatal(err)
				}
			}
			args := append(append([]any(nil), testCase.args...), rowID)
			if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, testCase.table, testCase.update, args...); err != nil {
				t.Fatal(err)
			}
			if testCase.ignoreChecks {
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
					t.Fatal(err)
				}
			}
			before := journalReportingPreflightState(t, f)
			got, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
			want := accounting.ErrJournalRepository
			if testCase.bound {
				want = accounting.ErrTaxFactReportingBound
			}
			if got != nil || !errors.Is(err, want) || probe.keyReads != 0 || probe.bodyReads != 0 {
				t.Fatalf("corrupt summary present=%t error=%v key/body reads %d/%d", got != nil, err, probe.keyReads, probe.bodyReads)
			}
			keys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 16384, 8388608)
			if keys != nil || !errors.Is(err, want) || probe.keyReads != 0 || probe.bodyReads != 0 {
				t.Fatalf("corrupt read present=%t error=%v key/body reads %d/%d", keys != nil, err, probe.keyReads, probe.bodyReads)
			}
			if !f.tx.Authenticated() || !equalJournalReportingPreflightState(before, journalReportingPreflightState(t, f)) {
				t.Fatal("reader mutated state or caller transaction")
			}
		})
	}
}

func TestTaxFactBASKeysEmptyAndSummaryDrift(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	probe := &taxFactBASKeyProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || empty == nil || empty.ScalarKeyCount != 0 || empty.ScalarBytes != 0 {
		t.Fatalf("empty summary = %#v, %v", empty, err)
	}
	if got, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *empty, 0, 0); err != nil || len(got) != 0 {
		t.Fatalf("empty keys = %#v, %v", got, err)
	}
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now, taxFactReportingID(17301), "SYSTEM", 0, "source", taxFactReportingID(17302), taxFactReportingID(17303), "source", "ADJUSTMENT", nil)
	probe.keyReads = 0
	if got, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *empty, 16384, 8388608); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.keyReads != 0 {
		t.Fatalf("stale count summary = %#v, %v; reads %d", got, err, probe.keyReads)
	}
	summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts", `UPDATE tax_facts SET source_id='longer-source' WHERE id=?`, taxFactReportingID(17303)); err != nil {
		t.Fatal(err)
	}
	if got, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 16384, 8388608); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.keyReads != 0 {
		t.Fatalf("stale bytes summary = %#v, %v; reads %d", got, err, probe.keyReads)
	}
	for _, bad := range []struct {
		organisation  string
		count, budget int64
	}{{accountTestOrganisationID, -1, 8388608}, {accountTestOrganisationID, 16384, -1}, {taxFactReportingID(17399), 16384, 8388608}} {
		if got, err := repository.ReadTaxFactBASKeys(f.ctx, bad.organisation, *summary, bad.count, bad.budget); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.keyReads != 0 {
			t.Fatalf("invalid read = %#v, %v", got, err)
		}
	}
}

func TestTaxFactBASKeysPhysicalCountIsSeparateFromCoverageCap(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	journalID, factID := taxFactReportingID(17401), taxFactReportingID(17403)
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now, journalID, "SYSTEM", 0, "source", taxFactReportingID(17402), factID, "source", "ADJUSTMENT", nil)
	const prefix = "018f0000-0000-7000-8000-"
	if _, err := f.tx.ExecContext(f.ctx, `WITH RECURSIVE sequence(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM sequence WHERE n<1000)
	 INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code)
	 SELECT ? || printf('%012d',500000+n),?,n+2,?,1,0,'AUD' FROM sequence`, prefix, journalID, f.accounts[0].Id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.ExecContext(f.ctx, `WITH RECURSIVE sequence(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM sequence WHERE n<1000)
	 INSERT INTO tax_facts(id,organisation_id,journal_line_id,tax_code,treatment,original_gross_minor,original_net_minor,original_gst_minor,attributed_gross_minor,attributed_net_minor,attributed_gst_minor,remaining_gross_minor,remaining_net_minor,remaining_gst_minor,tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,source_type,source_id,source_revision,created_at,source_content_hash)
	 SELECT ? || printf('%012d',600000+n),base.organisation_id,? || printf('%012d',500000+n),base.tax_code,base.treatment,base.original_gross_minor,base.original_net_minor,base.original_gst_minor,base.attributed_gross_minor,base.attributed_net_minor,base.attributed_gst_minor,base.remaining_gross_minor,base.remaining_net_minor,base.remaining_gst_minor,base.tax_rule_type,base.tax_rule_id,base.tax_rule_revision,base.tax_rule_content_hash,base.source_type,base.source_id,base.source_revision,base.created_at,base.source_content_hash
	 FROM sequence CROSS JOIN tax_facts base WHERE base.id=?`, prefix, prefix, factID); err != nil {
		t.Fatal(err)
	}
	summary, err := f.repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || summary == nil || summary.ScalarKeyCount != 1001 {
		t.Fatalf("physical summary = %#v, %v", summary, err)
	}
	keys, err := f.repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 1001, 8388608)
	if err != nil || len(keys) != 1001 {
		t.Fatalf("1,001 physical keys = %d, %v", len(keys), err)
	}
	if got, err := f.repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 1000, 8388608); got != nil || !errors.Is(err, accounting.ErrTaxFactReportingBound) {
		t.Fatalf("explicit key cap = %#v, %v", got, err)
	}
}

func TestTaxFactBASKeysDetectDriftDuringBoundedRead(t *testing.T) {
	for _, change := range []string{"count", "bytes", "removed row"} {
		t.Run(change, func(t *testing.T) {
			f := newTaxFactReportingFixture(t)
			factID := taxFactReportingID(17503)
			seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now, taxFactReportingID(17501), "SYSTEM", 0, "source", taxFactReportingID(17502), factID, "source", "ADJUSTMENT", nil)
			probe := &taxFactBASKeyProbe{Transaction: f.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
			if err != nil {
				t.Fatal(err)
			}
			probe.beforeKeyRead = func() error {
				switch change {
				case "count":
					seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now, taxFactReportingID(17511), "SYSTEM", 0, "second-source", taxFactReportingID(17512), taxFactReportingID(17513), "source", "ADJUSTMENT", nil)
					return nil
				case "bytes":
					return taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts", `UPDATE tax_facts SET source_id='longer-source' WHERE id=?`, factID)
				default:
					return taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts", `DELETE FROM tax_facts WHERE id=?`, factID)
				}
			}
			keys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 1, summary.ScalarBytes)
			want := accounting.ErrTaxFactReportingBound
			if change == "removed row" {
				want = accounting.ErrJournalRepository
			}
			if keys != nil || !errors.Is(err, want) || probe.keyReads != 1 || probe.bodyReads != 0 || !f.tx.Authenticated() {
				t.Fatalf("drifting read present=%t error=%v key/body reads %d/%d", keys != nil, err, probe.keyReads, probe.bodyReads)
			}
		})
	}
}

func TestTaxFactBASKeysRespectRollbackAndEncryptedReopen(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	hash := sha256.Sum256([]byte("reopen fact"))
	f.post(t, 17601, taxFactReportingID(17699), hash[:], hash[:])
	summary, err := f.repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := f.repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 16384, 8388608)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := f.workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(f.ctx, f.workspace.Path, f.workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.BeginEncryptedTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || reopened == nil || *reopened != *summary {
		t.Fatalf("reopened summary = %#v, %v", reopened, err)
	}
	reopenedKeys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *reopened, 16384, 8388608)
	if err != nil || !reflect.DeepEqual(reopenedKeys, keys) {
		t.Fatalf("reopened keys changed: %#v, %v", reopenedKeys, err)
	}
	seedTaxFactReportingDraft(t, tx, f.accounts, f.now, taxFactReportingID(17611), "SYSTEM", 0, "source", taxFactReportingID(17612), taxFactReportingID(17613), "source", "ADJUSTMENT", nil)
	rolledBack, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || rolledBack.ScalarKeyCount != 2 {
		t.Fatalf("caller transaction did not see its pending fact: %#v, %v", rolledBack, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	readTx, err := database.BeginEncryptedTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	repository, err = accounting.NewJournalRepository(readTx)
	if err != nil {
		t.Fatal(err)
	}
	final, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || final == nil || *final != *summary {
		t.Fatalf("rolled-back fact escaped caller transaction: %#v, %v", final, err)
	}
}

type taxFactBASKeyProbe struct {
	*sqlcipher.Transaction
	keyReads, bodyReads int
	beforeKeyRead       func() error
}

func (probe *taxFactBASKeyProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "/* BAS tax fact scalar keys */") {
		probe.keyReads++
		if probe.beforeKeyRead != nil {
			change := probe.beforeKeyRead
			probe.beforeKeyRead = nil
			if err := change(); err != nil {
				return nil, err
			}
		}
	}
	if strings.Contains(query, "SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code") ||
		strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.Contains(query, journalBodyMaskLinesMarker) ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind") ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id") {
		probe.bodyReads++
	}
	return probe.Transaction.QueryContext(ctx, query, args...)
}

// The native fixture mutates SQL cells after the repeated aggregate, immediately
// before the key query. TotalAlloc is sampled after mutation so only the bounded
// read is measured; SQL zeroblob avoids a large input allocation in Go.
func TestTaxFactBASKeysBoundCellsThatDriftBeforeKeyQuery(t *testing.T) {
	for _, testCase := range []struct {
		name, table, update string
		numeric             bool
	}{
		{"oversized source ID", "tax_facts", `UPDATE tax_facts SET source_id=CAST(zeroblob(8388609) AS TEXT) WHERE id=?`, false},
		{"oversized source revision BLOB", "tax_facts", `UPDATE tax_facts SET source_revision=zeroblob(8388609) WHERE id=?`, true},
		{"oversized line number BLOB", "journal_lines", `UPDATE journal_lines SET line_number=zeroblob(8388609) WHERE id=?`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f := newTaxFactReportingFixture(t)
			factID, lineID := taxFactReportingID(17703), taxFactReportingID(17702)
			seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
				taxFactReportingID(17701), "SYSTEM", 0, "source", lineID, factID, "source", "ADJUSTMENT", nil)
			probe := &taxFactBASKeyProbe{Transaction: f.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
			if err != nil {
				t.Fatal(err)
			}
			var before runtime.MemStats
			probe.beforeKeyRead = func() error {
				rowID := factID
				if testCase.table == "journal_lines" {
					rowID = lineID
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx,
					testCase.table, testCase.update, rowID); err != nil {
					return err
				}
				runtime.ReadMemStats(&before)
				return nil
			}
			keys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID,
				*summary, 1, summary.ScalarBytes)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			if keys != nil || err == nil || probe.keyReads != 1 || probe.bodyReads != 0 || !f.tx.Authenticated() {
				t.Fatalf("drifting cell read present=%t error present=%t key/body reads %d/%d", keys != nil, err != nil, probe.keyReads, probe.bodyReads)
			}
			want := accounting.ErrTaxFactReportingBound
			if testCase.numeric {
				want = accounting.ErrJournalRepository
			}
			if !errors.Is(err, want) {
				t.Fatalf("error kind = %T; expected %v", err, want)
			}
			if size := len(err.Error()); size > 1024 {
				t.Errorf("corrupt cell was expanded into an error of %d bytes", size)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 2*1024*1024 {
				t.Errorf("bounded scalar read allocated %d bytes after the aggregate reservation", allocated)
			}
		})
	}
}

func TestTaxFactBASKeysBoundCumulativeAndSentinelProjectionBeforeDriverCopy(t *testing.T) {
	for _, sentinel := range []bool{false, true} {
		name := "cumulative bytes"
		if sentinel {
			name = "count sentinel"
		}
		t.Run(name, func(t *testing.T) {
			f := newTaxFactReportingFixture(t)
			firstFact, secondFact := taxFactReportingID(17803), taxFactReportingID(17813)
			seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now, taxFactReportingID(17801), "SYSTEM", 0, "first", taxFactReportingID(17802), firstFact, "first", "ADJUSTMENT", nil)
			seedSecond := func() {
				seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now, taxFactReportingID(17811), "SYSTEM", 0, "second", taxFactReportingID(17812), secondFact, "second", "ADJUSTMENT", nil)
			}
			if !sentinel {
				seedSecond()
			}
			if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts", `UPDATE tax_facts SET source_id=CAST(zeroblob(3145728) AS TEXT) WHERE id=?`, firstFact); err != nil {
				t.Fatal(err)
			}
			probe := &taxFactBASKeyProbe{Transaction: f.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
			if err != nil {
				t.Fatal(err)
			}
			var before runtime.MemStats
			probe.beforeKeyRead = func() error {
				if sentinel {
					seedSecond()
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts", `UPDATE tax_facts SET source_id=CAST(zeroblob(3145728) AS TEXT) WHERE id=?`, secondFact); err != nil {
					return err
				}
				runtime.ReadMemStats(&before)
				return nil
			}
			keys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, summary.ScalarKeyCount, 8388608)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			var bound *accounting.TaxFactReportingBoundError
			if keys != nil || !errors.As(err, &bound) || probe.keyReads != 1 || probe.bodyReads != 0 {
				t.Fatalf("drift present=%t error kind=%T key/body reads %d/%d", keys != nil, err, probe.keyReads, probe.bodyReads)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 5*1024*1024 {
				t.Errorf("read copied a rejected second 3 MiB key: allocated %d bytes", allocated)
			}
			wantLimit := int(summary.ScalarBytes)
			if sentinel {
				wantLimit = int(summary.ScalarKeyCount)
			}
			if bound.Limit != wantLimit {
				t.Errorf("bound limit=%d, want actual reservation %d", bound.Limit, wantLimit)
			}
		})
	}
}
