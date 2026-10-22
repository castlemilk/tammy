//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestJournalRepositoryBASLegacyCashPreservesLegacyNullSourceHash(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const (
		sourceID  = "018f0000-0000-7000-8000-000000027891"
		journalID = "018f0000-0000-7000-8000-000000027892"
		lineID    = "018f0000-0000-7000-8000-000000027893"
		factID    = "018f0000-0000-7000-8000-000000027894"
	)
	// This is the historical nullable-hash insert shape. Current writers require
	// a 32-byte source hash, so use the existing raw legacy-row test helper here.
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
		journalID, "MANUAL", 0, sourceID, lineID, factID, sourceID, "ADJUSTMENT", nil)
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	summary, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil || summary == nil || summary.AssociationCount != 2 || summary.JournalCount != 1 || summary.LineCount != 2 {
		t.Fatalf("historical source summary = %#v, %v", summary, err)
	}
	read, err := f.repository.ReadBASLegacyCash(f.ctx, query, *summary, 16384, 8*1024*1024)
	if err != nil || read == nil || len(read.Associations) != 2 {
		t.Fatalf("historical source read = %#v, %v", read, err)
	}
	var sawTaxFact bool
	for _, association := range read.Associations {
		if association.Kind == "TAX_FACT" {
			sawTaxFact = true
			if association.TaxFactID == nil || *association.TaxFactID != factID || association.JournalLineID == nil ||
				*association.JournalLineID != lineID || association.FactSourceContentHashPresent ||
				association.FactSourceContentHash != nil {
				t.Fatalf("NULL source hash was not retained as absent-with-nil: %#v", association)
			}
		}
	}
	if !sawTaxFact {
		t.Fatal("historical tax-fact association was omitted")
	}
}

func TestJournalRepositoryBASLegacyCashRejectsInvalidOwnerStorageAndLinksBeforeRead(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *taxFactReportingFixture, string)
	}{
		{
			name: "non-integer tax source revision",
			mutate: func(t *testing.T, f *taxFactReportingFixture, factID string) {
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatal(err)
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts",
					`UPDATE tax_facts SET source_revision=zeroblob(8) WHERE id=?`, factID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "empty present source hash",
			mutate: func(t *testing.T, f *taxFactReportingFixture, factID string) {
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatal(err)
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts",
					`UPDATE tax_facts SET source_content_hash=zeroblob(0) WHERE id=?`, factID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "foreign fact owner on target-owned journal line",
			mutate: func(t *testing.T, f *taxFactReportingFixture, factID string) {
				otherOwner := addBASLegacyCashOtherOwner(t, f)
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
					t.Fatal(err)
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts",
					`UPDATE tax_facts SET organisation_id=? WHERE id=?`, otherOwner, factID); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f := newTaxFactReportingFixture(t)
			const sourceID = "018f0000-0000-7000-8000-000000027901"
			hash := []byte(strings.Repeat("h", 32))
			posted := f.post(t, 27902, sourceID, hash, hash)
			query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
			before, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
			if err != nil || before == nil {
				t.Fatalf("baseline summary = %#v, %v", before, err)
			}
			testCase.mutate(t, f, posted.factID)
			probe := &basLegacyCashGuardProbe{Transaction: f.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := repository.SummarizeBASLegacyCash(f.ctx, query); got != nil || !errors.Is(err, accounting.ErrJournalRepository) {
				t.Fatalf("invalid projection summary = %#v, %v; want repository failure", got, err)
			}
			if got, err := repository.ReadBASLegacyCash(f.ctx, query, *before, 16384, 8*1024*1024); got != nil || !errors.Is(err, accounting.ErrJournalRepository) {
				t.Fatalf("invalid projection read = %#v, %v; want no partial result and repository failure", got, err)
			}
			if probe.bodyQueries != 0 {
				t.Fatalf("selector entered body reader %d times", probe.bodyQueries)
			}
		})
	}
}

func TestJournalRepositoryBASLegacyCashRejectsForeignCurrentAccount(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const sourceID = "018f0000-0000-7000-8000-000000027911"
	hash := []byte(strings.Repeat("a", 32))
	posted := f.post(t, 27912, sourceID, hash, hash)
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	before, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	otherOwner := addBASLegacyCashOtherOwner(t, f)
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "accounts",
		`UPDATE accounts SET organisation_id=? WHERE id=?`, otherOwner, f.accounts[0].Id); err != nil {
		t.Fatal(err)
	}
	probe := &basLegacyCashGuardProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repository.ReadBASLegacyCash(f.ctx, query, *before, 16384, 8*1024*1024); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.bodyQueries != 0 {
		t.Fatalf("foreign account read = %#v, %v body=%d; want rejection before any body load", got, err, probe.bodyQueries)
	}
	_ = posted
}

func TestJournalRepositoryBASLegacyCashRejectsForeignDirectSourceOwner(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const (
		sourceID  = "018f0000-0000-7000-8000-000000027915"
		journalID = "018f0000-0000-7000-8000-000000027916"
		lineID    = "018f0000-0000-7000-8000-000000027917"
		factID    = "018f0000-0000-7000-8000-000000027918"
	)
	// The tax fact deliberately has another source ID, isolating the direct
	// journal association. A requested root must not disappear when its journal
	// owner is foreign.
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
		journalID, "MANUAL", 0, sourceID, lineID, factID,
		"018f0000-0000-7000-8000-000000027919", "ADJUSTMENT", nil)
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	otherOwner := addBASLegacyCashOtherOwner(t, f)
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "journals",
		`UPDATE journals SET organisation_id=? WHERE id=?`, otherOwner, journalID); err != nil {
		t.Fatal(err)
	}
	probe := &basLegacyCashGuardProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repository.SummarizeBASLegacyCash(f.ctx, query); got != nil || !errors.Is(err, accounting.ErrJournalRepository) {
		t.Fatalf("foreign direct source summary = %#v, %v; want owner rejection", got, err)
	}
	if got, err := repository.ReadBASLegacyCash(f.ctx, query,
		accounting.BASLegacyCashSummary{OrganisationID: query.OrganisationID}, 16384, 8*1024*1024); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.bodyQueries != 0 {
		t.Fatalf("foreign direct source read = %#v, %v body=%d; want no partial result or body read",
			got, err, probe.bodyQueries)
	}
}

func TestJournalRepositoryBASLegacyCashRejectsForeignTaxFactAndParentOwner(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const (
		sourceID              = "018f0000-0000-7000-8000-000000027925"
		journalID             = "018f0000-0000-7000-8000-000000027926"
		lineID                = "018f0000-0000-7000-8000-000000027927"
		factID                = "018f0000-0000-7000-8000-000000027928"
		unrelatedDirectSource = "018f0000-0000-7000-8000-000000027929"
	)
	// Neither the direct source link nor a target-owned fact can reveal this
	// requested root. The exact TAX_FACT association nevertheless exists with
	// both its fact and linked journal foreign-owned, so the scalar selector
	// must surface and reject that malformed cross-owner arm.
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
		journalID, "MANUAL", 0, unrelatedDirectSource, lineID, factID, sourceID, "ADJUSTMENT", nil)
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	otherOwner := addBASLegacyCashOtherOwner(t, f)
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "journals",
		`UPDATE journals SET organisation_id=? WHERE id=?`, otherOwner, journalID); err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "tax_facts",
		`UPDATE tax_facts SET organisation_id=? WHERE id=?`, otherOwner, factID); err != nil {
		t.Fatal(err)
	}
	probe := &basLegacyCashGuardProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repository.SummarizeBASLegacyCash(f.ctx, query); got != nil || !errors.Is(err, accounting.ErrJournalRepository) {
		t.Fatalf("foreign tax-fact association summary = %#v, %v; want owner rejection", got, err)
	}
	if got, err := repository.ReadBASLegacyCash(f.ctx, query,
		accounting.BASLegacyCashSummary{OrganisationID: query.OrganisationID}, 16384, 8*1024*1024); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.bodyQueries != 0 {
		t.Fatalf("foreign tax-fact association read = %#v, %v body=%d; want no partial result or body read",
			got, err, probe.bodyQueries)
	}
}

func TestJournalRepositoryBASLegacyCashRetainsOpaqueLegacyJournalLineAndFactIDs(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const (
		sourceID         = "018f0000-0000-7000-8000-000000027935"
		parentID         = "legacy-manual-journal"
		parentCashLineID = "legacy-cash-line"
		factID           = "legacy-tax-fact"
		childID          = "legacy-reversal-journal"
	)
	// Existing legacy journal, line and fact identities are retained opaque
	// TEXT. Only the query's organisation and source root are canonical IDs.
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
		parentID, "MANUAL", 0, sourceID, parentCashLineID, factID, sourceID, "ADJUSTMENT", nil)
	postedAt := f.now.Format(time.RFC3339Nano)
	if _, err := f.tx.ExecContext(f.ctx, `UPDATE journals SET state='POSTED',posted_at=? WHERE id=?`, postedAt, parentID); err != nil {
		t.Fatalf("post legacy parent: %v", err)
	}
	if _, err := f.tx.ExecContext(f.ctx, `
		INSERT INTO journals(id,organisation_id,source_type,source_kind,source_id,source_revision,state,journal_date,
		 description,reversal_of_journal_id,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at)
		VALUES(?,?,'REVERSAL',0,?,1,'DRAFT','2026-08-11','opaque legacy reversal',?,1,1,'AUD',2,?)`,
		childID, accountTestOrganisationID, parentID, parentID, postedAt); err != nil {
		t.Fatalf("insert opaque reversal child: %v", err)
	}
	for _, line := range []struct {
		id, account   string
		sequence      int64
		debit, credit int64
	}{
		{"legacy-reversal-cash", f.accounts[0].Id, 1, 0, 1},
		{"legacy-reversal-expense", f.accounts[1].Id, 2, 1, 0},
	} {
		if _, err := f.tx.ExecContext(f.ctx,
			`INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,?,?,?,?, 'AUD')`,
			line.id, childID, line.sequence, line.account, line.debit, line.credit); err != nil {
			t.Fatalf("insert opaque reversal line %q: %v", line.id, err)
		}
	}
	if _, err := f.tx.ExecContext(f.ctx, `UPDATE journals SET state='POSTED',posted_at=? WHERE id=?`, postedAt, childID); err != nil {
		t.Fatalf("post opaque reversal child: %v", err)
	}
	if _, err := f.tx.ExecContext(f.ctx, `
		UPDATE journals SET state='REVERSED',version=2,reversed_by_journal_id=? WHERE id=?`, childID, parentID); err != nil {
		t.Fatalf("complete opaque reversal: %v", err)
	}
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	summary, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil || summary == nil || summary.AssociationCount != 2 || summary.JournalCount != 2 ||
		summary.LineCount != 4 || summary.RowCount != 8 {
		t.Fatalf("opaque legacy summary = %#v, %v", summary, err)
	}
	read, err := f.repository.ReadBASLegacyCash(f.ctx, query, *summary, 16384, 8*1024*1024)
	if err != nil || read == nil {
		t.Fatalf("opaque legacy read = %#v, %v", read, err)
	}
	var sawDirect, sawFact, sawOpaqueChild bool
	for _, association := range read.Associations {
		if association.Kind == "DIRECT" && association.JournalID == parentID {
			sawDirect = true
		}
		if association.Kind == "TAX_FACT" && association.TaxFactID != nil && *association.TaxFactID == factID &&
			association.JournalLineID != nil && *association.JournalLineID == parentCashLineID {
			sawFact = true
		}
	}
	for _, journal := range read.Journals {
		if journal.ID == childID && journal.SourceID == parentID && journal.ReversalOfID != nil && *journal.ReversalOfID == parentID {
			sawOpaqueChild = true
		}
	}
	lineIDs := make(map[string]bool)
	for _, line := range read.Lines {
		lineIDs[line.ID] = true
	}
	if !sawDirect || !sawFact || !sawOpaqueChild || !lineIDs[parentCashLineID] ||
		!lineIDs["legacy-reversal-cash"] || !lineIDs["legacy-reversal-expense"] {
		t.Fatalf("opaque legacy identities not retained: associations=%#v journals=%#v lines=%#v",
			read.Associations, read.Journals, read.Lines)
	}
}

func TestJournalRepositoryBASLegacyCashRejectsZeroJournalSourceRevision(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const (
		sourceID  = "018f0000-0000-7000-8000-000000027945"
		journalID = "018f0000-0000-7000-8000-000000027946"
		lineID    = "018f0000-0000-7000-8000-000000027947"
		factID    = "018f0000-0000-7000-8000-000000027948"
	)
	seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
		journalID, "MANUAL", 0, sourceID, lineID, factID, "018f0000-0000-7000-8000-000000027949", "ADJUSTMENT", nil)
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "journals",
		`UPDATE journals SET source_revision=0 WHERE id=?`, journalID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	probe := &basLegacyCashGuardProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repository.SummarizeBASLegacyCash(f.ctx, query); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.bodyQueries != 0 {
		t.Fatalf("zero source revision summary = %#v, %v body=%d; want scalar rejection before body read",
			got, err, probe.bodyQueries)
	}
}

func TestJournalRepositoryBASLegacyCashRejectsMalformedAndUnknownSourceKindsOnBothRootArms(t *testing.T) {
	for _, rootArm := range []struct {
		name                string
		journalSourceIsRoot bool
	}{
		{name: "DIRECT", journalSourceIsRoot: true},
		{name: "TAX_FACT", journalSourceIsRoot: false},
	} {
		for _, malformedKind := range []struct {
			name, assignment string
		}{
			{name: "blob", assignment: "zeroblob(8)"},
			{name: "text", assignment: "'not-an-integer'"},
			{name: "unknown-integer", assignment: "9"},
		} {
			t.Run(rootArm.name+"/"+malformedKind.name, func(t *testing.T) {
				f := newTaxFactReportingFixture(t)
				const sourceID = "018f0000-0000-7000-8000-000000027965"
				const journalID = "018f0000-0000-7000-8000-000000027966"
				const lineID = "018f0000-0000-7000-8000-000000027967"
				const factID = "018f0000-0000-7000-8000-000000027968"
				journalSourceID, factSourceID := "018f0000-0000-7000-8000-000000027969", sourceID
				if rootArm.journalSourceIsRoot {
					journalSourceID, factSourceID = sourceID, "018f0000-0000-7000-8000-000000027969"
				}
				seedTaxFactReportingDraft(t, f.tx, f.accounts, f.now,
					journalID, "MANUAL", 0, journalSourceID, lineID, factID, factSourceID, "ADJUSTMENT", nil)
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatal(err)
				}
				update := "UPDATE journals SET source_kind=" + malformedKind.assignment + " WHERE id=?"
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "journals", update, journalID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
					t.Fatal(err)
				}
				query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
				probe := &basLegacyCashGuardProbe{Transaction: f.tx}
				repository, err := accounting.NewJournalRepository(probe)
				if err != nil {
					t.Fatal(err)
				}
				if got, err := repository.SummarizeBASLegacyCash(f.ctx, query); got != nil ||
					!errors.Is(err, accounting.ErrJournalRepository) || probe.bodyQueries != 0 {
					t.Fatalf("%s malformed source kind summary = %#v, %v body=%d; want scalar rejection",
						rootArm.name, got, err, probe.bodyQueries)
				}
				if got, err := repository.ReadBASLegacyCash(f.ctx, query,
					accounting.BASLegacyCashSummary{OrganisationID: query.OrganisationID}, 16384, 8*1024*1024); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.bodyQueries != 0 {
					t.Fatalf("%s malformed source kind read = %#v, %v body=%d; want no partial result",
						rootArm.name, got, err, probe.bodyQueries)
				}
			})
		}
	}
}

func TestJournalRepositoryBASLegacyCashExcludesOpeningAndOwnedPurchaseJournalKinds(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const sourceID = "018f0000-0000-7000-8000-000000027955"
	for index, source := range []struct {
		id, sourceType string
		kind           int64
	}{
		{"018f0000-0000-7000-8000-000000027956", "OPENING", 0},
		{"018f0000-0000-7000-8000-000000027957", "PAYABLE", 6},
		{"018f0000-0000-7000-8000-000000027958", "BANKING", 7},
		{"018f0000-0000-7000-8000-000000027959", "TAX", 8},
	} {
		if _, err := f.tx.ExecContext(f.ctx, `
			INSERT INTO journals(id,organisation_id,source_type,source_kind,source_id,source_revision,state,journal_date,
			 description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at)
			VALUES(?,?,?,?,?,1,'DRAFT','2026-08-10','excluded source kind',1,1,'AUD',?,?)`,
			source.id, accountTestOrganisationID, source.sourceType, source.kind, sourceID, index+1,
			f.now.Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("insert excluded %s/%d source journal: %v", source.sourceType, source.kind, err)
		}
	}
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	summary, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil || summary == nil || summary.AssociationCount != 0 || summary.JournalCount != 0 ||
		summary.LineCount != 0 || summary.RowCount != 0 || summary.Bytes != 0 {
		t.Fatalf("excluded roots became legacy cash rows: %#v, %v", summary, err)
	}
	read, err := f.repository.ReadBASLegacyCash(f.ctx, query, *summary, 16384, 8*1024*1024)
	if err != nil || read == nil || len(read.Associations) != 0 || len(read.Journals) != 0 || len(read.Lines) != 0 {
		t.Fatalf("excluded root read = %#v, %v", read, err)
	}
}

func TestJournalRepositoryBASLegacyCashRejectsBrokenReciprocalReversalBeforeRead(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const sourceID = "018f0000-0000-7000-8000-000000027921"
	hash := []byte(strings.Repeat("r", 32))
	posted := f.post(t, 27922, sourceID, hash, hash)
	const reversalID = "018f0000-0000-7000-8000-000000027929"
	lineIDs := []string{taxFactReportingID(27930), taxFactReportingID(27931)}
	reversed, _, err := f.repository.Reverse(f.ctx, posted.journal.Id, posted.journal.Version,
		&tammyv1.CivilDate{Year: 2026, Month: 8, Day: 11}, "BAS legacy selector guard", reversalID, lineIDs, f.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	before, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "journals",
		`UPDATE journals SET reversed_by_journal_id=NULL WHERE id=?`, reversed.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	probe := &basLegacyCashGuardProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repository.ReadBASLegacyCash(f.ctx, query, *before, 16384, 8*1024*1024); got != nil || !errors.Is(err, accounting.ErrJournalRepository) || probe.bodyQueries != 0 {
		t.Fatalf("broken reversal read = %#v, %v body=%d; want closure rejection before any body load", got, err, probe.bodyQueries)
	}
}

func TestJournalRepositoryBASLegacyCashMasksLateOverBudgetCell(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const sourceID = "018f0000-0000-7000-8000-000000027941"
	hash := []byte(strings.Repeat("b", 32))
	f.post(t, 27942, sourceID, hash, hash)
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	summary, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	probe := &basLegacyCashGuardProbe{
		Transaction: f.tx, growAccountClassificationOnRead: true, accountID: f.accounts[0].Id,
	}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repository.ReadBASLegacyCash(f.ctx, query, *summary, 16384, 8*1024*1024)
	if got != nil || !errors.Is(err, accounting.ErrJournalRepository) || !probe.grewAccount || probe.bodyQueries != 0 {
		t.Fatalf("late over-budget read = %#v, %v, grew=%t body=%d; want nil and bounded failure before body loads",
			got, err, probe.grewAccount, probe.bodyQueries)
	}
}

func TestJournalRepositoryBASLegacyCashHonorsLateCancellation(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const sourceID = "018f0000-0000-7000-8000-000000027951"
	hash := []byte(strings.Repeat("c", 32))
	f.post(t, 27952, sourceID, hash, hash)
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	summary, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	probe := &basLegacyCashGuardProbe{Transaction: f.tx, cancelOnTypedRead: cancel}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repository.ReadBASLegacyCash(ctx, query, *summary, 16384, 8*1024*1024)
	if got != nil || !errors.Is(err, context.Canceled) || !probe.canceled || probe.bodyQueries != 0 {
		t.Fatalf("canceled read = %#v, %v, canceled=%t body=%d; want no partial result and context cancellation",
			got, err, probe.canceled, probe.bodyQueries)
	}
}

func TestJournalRepositoryBASLegacyCashCapsEachSourceReversalClosureAt1000(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const sourceID = "018f0000-0000-7000-8000-000000027961"
	hash := []byte(strings.Repeat("l", 32))
	posted := f.post(t, 27962, sourceID, hash, hash)
	currentID, currentVersion := posted.journal.Id, posted.journal.Version
	for index := 0; index < 1000; index++ {
		seed := 28000 + index*3
		reversalID := taxFactReportingID(seed)
		lineIDs := []string{taxFactReportingID(seed + 1), taxFactReportingID(seed + 2)}
		reversed, reversal, err := f.repository.Reverse(f.ctx, currentID, currentVersion,
			&tammyv1.CivilDate{Year: 2026, Month: 8, Day: 11}, "Bound closure chain", reversalID, lineIDs,
			f.now.Add(time.Duration(index+1)*time.Second))
		if err != nil || reversed == nil || reversal == nil {
			t.Fatalf("writer reversal %d = original:%#v child:%#v err:%v", index, reversed, reversal, err)
		}
		currentID, currentVersion = reversal.Id, reversal.Version
		if index == 998 {
			query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
			summary, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
			if err != nil || summary == nil || summary.JournalCount != 1000 {
				t.Fatalf("exact 1000-journal closure = %#v, %v", summary, err)
			}
			read, err := f.repository.ReadBASLegacyCash(f.ctx, query, *summary, summary.RowCount, summary.Bytes)
			if err != nil || read == nil || len(read.Journals) != 1000 {
				t.Fatalf("exact closure read = %v, %v", read != nil, err)
			}
		}
	}
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	probe := &basLegacyCashGuardProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repository.SummarizeBASLegacyCash(f.ctx, query); got != nil ||
		!errors.Is(err, accounting.ErrTaxFactReportingBound) || probe.bodyQueries != 0 {
		t.Fatalf("1001-journal closure = %#v, %v body=%d; want closure-bound failure before body reads", got, err, probe.bodyQueries)
	}
}

type basLegacyCashGuardProbe struct {
	*sqlcipher.Transaction
	bodyQueries                     int
	growAccountClassificationOnRead bool
	accountID                       string
	grewAccount                     bool
	cancelOnTypedRead               context.CancelFunc
	canceled                        bool
}

func (probe *basLegacyCashGuardProbe) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	if !strings.Contains(query, "BAS legacy cash") &&
		(strings.Contains(query, "FROM journals") || strings.Contains(query, "FROM journal_lines") ||
			strings.Contains(query, "FROM tax_facts") || strings.Contains(query, "FROM accounts")) {
		probe.bodyQueries++
	}
	if strings.Contains(query, "row_number() OVER") && probe.growAccountClassificationOnRead && !probe.grewAccount {
		if _, err := probe.Transaction.ExecContext(context.Background(), "PRAGMA ignore_check_constraints=ON"); err != nil {
			return nil, err
		}
		large := strings.Repeat("x", 8*1024*1024+1)
		if err := taxFactSharedBudgetMutateWithTriggersDisabled(probe.Transaction, "accounts",
			`UPDATE accounts SET report_classification=? WHERE id=?`, large, probe.accountID); err != nil {
			return nil, err
		}
		if _, err := probe.Transaction.ExecContext(context.Background(), "PRAGMA ignore_check_constraints=OFF"); err != nil {
			return nil, err
		}
		probe.grewAccount = true
	}
	rows, err := probe.Transaction.QueryContext(ctx, query, arguments...)
	if err == nil && strings.Contains(query, "row_number() OVER") && probe.cancelOnTypedRead != nil {
		probe.cancelOnTypedRead()
		probe.cancelOnTypedRead = nil
		probe.canceled = true
	}
	return rows, err
}

func addBASLegacyCashOtherOwner(t *testing.T, f *taxFactReportingFixture) string {
	t.Helper()
	otherOwner := taxFactReportingID(27999)
	var singletonGuard string
	if err := f.tx.QueryRowContext(f.ctx,
		`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='organisations_singleton_insert'`).Scan(&singletonGuard); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.ExecContext(f.ctx, `DROP TRIGGER organisations_singleton_insert`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.ExecContext(f.ctx,
		`INSERT INTO organisations(id,legal_name,status,created_at) VALUES(?,'Foreign legacy cash owner','INACTIVE',?)`,
		otherOwner, f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.ExecContext(f.ctx, singletonGuard); err != nil {
		t.Fatal(err)
	}
	return otherOwner
}

func TestJournalRepositoryBASLegacyCashRejectsBLOBParentOfUnfactoredLine(t *testing.T) {
	f := newTaxFactReportingFixture(t)
	const sourceID = "018f0000-0000-7000-8000-000000029991"
	hash := []byte(strings.Repeat("l", 32))
	posted := f.post(t, 29992, sourceID, hash, hash)
	query := accounting.BASLegacyCashQuery{OrganisationID: accountTestOrganisationID, SourceIDs: []string{sourceID}}
	before, err := f.repository.SummarizeBASLegacyCash(f.ctx, query)
	if err != nil || before == nil || before.LineCount != 2 {
		t.Fatalf("writer line baseline=%#v, %v", before, err)
	}
	if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(f.tx, "journal_lines",
		`UPDATE journal_lines SET journal_id=CAST(? AS BLOB) WHERE id=?`, posted.journal.Id, posted.journal.Lines[0].Id); err != nil {
		t.Fatal(err)
	}
	probe := &basLegacyCashGuardProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := repository.SummarizeBASLegacyCash(f.ctx, query)
	if summary != nil || !errors.Is(err, accounting.ErrJournalRepository) {
		t.Errorf("BLOB parent hid selected journal line: summary=%#v err=%v", summary, err)
	}
	expected := *before
	if summary != nil {
		expected = *summary
	}
	if read, err := repository.ReadBASLegacyCash(f.ctx, query, expected, 16384, 8*1024*1024); read != nil || !errors.Is(err, accounting.ErrJournalRepository) {
		t.Errorf("BLOB parent returned incomplete journal line proof: read=%v err=%v", read != nil, err)
	}
	if probe.bodyQueries != 0 || !f.tx.Authenticated() {
		t.Fatal("invalid scalar read loaded bodies or ended caller transaction")
	}
}
