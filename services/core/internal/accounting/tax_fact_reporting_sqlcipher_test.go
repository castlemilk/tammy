//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestJournalRepositoryCapturesCompleteTaxFactLineageAndFingerprintsIt(t *testing.T) {
	first := newTaxFactReportingFixture(t)
	firstRuleHash := sha256.Sum256([]byte("reporting tax rule"))
	firstSourceHash := sha256.Sum256([]byte("first retained evidence"))
	firstPosted := first.post(t, 501, "018f0000-0000-7000-8000-000000000501", firstSourceHash[:], firstRuleHash[:])

	firstCapture, err := first.repository.CaptureTaxFactsForReporting(first.ctx, accountTestOrganisationID)
	if err != nil {
		t.Fatalf("CaptureTaxFactsForReporting() = %v", err)
	}
	if firstCapture == nil || len(firstCapture.Facts) != 1 || len(firstCapture.Fingerprint) != sha256.Size {
		t.Fatalf("first reporting capture = %#v; want one fact and a SHA-256 fingerprint", firstCapture)
	}
	firstRecord := firstCapture.Facts[0]
	assertTaxFactReportingPosting(t, first.repository, firstRecord, firstPosted, firstSourceHash[:], firstRuleHash[:], first.now)

	repeatedCapture, err := first.repository.CaptureTaxFactsForReporting(first.ctx, accountTestOrganisationID)
	if err != nil {
		t.Fatalf("repeated CaptureTaxFactsForReporting() = %v", err)
	}
	if repeatedCapture == nil || !bytes.Equal(firstCapture.Fingerprint, repeatedCapture.Fingerprint) {
		t.Fatalf("repeated capture fingerprint changed: first=%x repeated=%#v", firstCapture.Fingerprint, repeatedCapture)
	}

	second := newTaxFactReportingFixture(t)
	secondRuleHash := sha256.Sum256([]byte("reporting tax rule"))
	secondSourceHash := sha256.Sum256([]byte("different retained evidence"))
	secondPosted := second.post(t, 501, "018f0000-0000-7000-8000-000000000501", secondSourceHash[:], secondRuleHash[:])
	secondCapture, err := second.repository.CaptureTaxFactsForReporting(second.ctx, accountTestOrganisationID)
	if err != nil || secondCapture == nil || len(secondCapture.Facts) != 1 {
		t.Fatalf("second reporting capture = %#v, %v", secondCapture, err)
	}
	secondRecord := secondCapture.Facts[0]
	assertTaxFactReportingPosting(t, second.repository, secondRecord, secondPosted, secondSourceHash[:], secondRuleHash[:], second.now)
	if !taxFactReportingAmountsEqual(firstRecord, secondRecord) {
		t.Fatalf("comparison postings do not have identical tax amounts: first=%#v second=%#v", firstRecord, secondRecord)
	}
	if bytes.Equal(firstCapture.Fingerprint, secondCapture.Fingerprint) {
		t.Fatalf("changing only the evidence content hash left the same-total fingerprint unchanged: %x", firstCapture.Fingerprint)
	}
	third := newTaxFactReportingFixture(t)
	third.post(t, 501, "018f0000-0000-7000-8000-000000000502", firstSourceHash[:], firstRuleHash[:])
	thirdCapture, err := third.repository.CaptureTaxFactsForReporting(third.ctx, accountTestOrganisationID)
	if err != nil || thirdCapture == nil || len(thirdCapture.Facts) != 1 {
		t.Fatalf("third reporting capture = %#v, %v", thirdCapture, err)
	}
	if bytes.Equal(firstCapture.Fingerprint, thirdCapture.Fingerprint) {
		t.Fatalf("changing only the evidence identity left the same-total fingerprint unchanged: %x", firstCapture.Fingerprint)
	}
}

func TestJournalRepositoryRetainsRawUnsupportedAdjustmentAndJournalMetadata(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	unknownID := "018f0000-0000-7000-8000-000000000601"
	unknownLineID := "018f0000-0000-7000-8000-000000000602"

	taxID := "018f0000-0000-7000-8000-000000000611"
	taxLineID := "018f0000-0000-7000-8000-000000000612"
	taxSourceHash := sha256.Sum256([]byte("retained attribution evidence"))
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		"018f0000-0000-7000-8000-000000000613", "TAX", 8, "tax-source", taxLineID,
		taxID, "018f0000-0000-7000-8000-000000000641", "ADJUSTMENT", taxSourceHash[:])

	payableID := "018f0000-0000-7000-8000-000000000621"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		"018f0000-0000-7000-8000-000000000623", "PAYABLE", 6, "purchase-posting-source",
		"018f0000-0000-7000-8000-000000000622", payableID, "018f0000-0000-7000-8000-000000000651", "ADJUSTMENT", nil)

	bankingID := "018f0000-0000-7000-8000-000000000631"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		"018f0000-0000-7000-8000-000000000633", "BANKING", 7, "banking-operation-source",
		"018f0000-0000-7000-8000-000000000632", bankingID, "018f0000-0000-7000-8000-000000000661", "ADJUSTMENT", nil)
	// Append the first journal in sorted order last, so append order cannot
	// accidentally satisfy the reporting port's deterministic ordering.
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		"018f0000-0000-7000-8000-000000000603", "SYSTEM", 0, "system-source", unknownLineID,
		unknownID, "018f0000-0000-7000-8000-000000000671", "ADJUSTMENT", nil)

	capture, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil {
		t.Fatalf("CaptureTaxFactsForReporting() = %v", err)
	}
	if capture == nil || len(capture.Facts) != 4 {
		t.Fatalf("reporting capture = %#v; want all four raw draft adjustment facts", capture)
	}
	byID := make(map[string]accounting.TaxFactReportingRecord, len(capture.Facts))
	for _, record := range capture.Facts {
		byID[record.ID] = record
	}
	if capture.Facts[0].ID != unknownID || capture.Facts[1].ID != taxID ||
		capture.Facts[2].ID != payableID || capture.Facts[3].ID != bankingID {
		t.Fatalf("raw fact projection order = %q, %q, %q, %q; want date/journal/line/fact order",
			capture.Facts[0].ID, capture.Facts[1].ID, capture.Facts[2].ID, capture.Facts[3].ID)
	}
	unknown := byID[unknownID]
	if unknown.ID == "" || unknown.Treatment != "ADJUSTMENT" || unknown.JournalSourceType != "SYSTEM" ||
		unknown.JournalSourceKind != 0 || unknown.JournalState != "DRAFT" || unknown.Journal == nil ||
		unknown.Journal.State != tammyv1.JournalState_JOURNAL_STATE_UNSPECIFIED ||
		unknown.Journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_UNSPECIFIED || len(unknown.Journal.Lines) != 2 {
		t.Fatalf("SQL-only SYSTEM adjustment metadata was not retained faithfully: %#v", unknown)
	}
	if unknown.SourceContentHashPresent || unknown.SourceContentHash != nil {
		t.Fatalf("NULL source hash became present: present=%t hash=%x", unknown.SourceContentHashPresent, unknown.SourceContentHash)
	}
	if unknown.Rule == nil || unknown.Rule.Type != "retained_rule" || unknown.Rule.Id != "018f0000-0000-7000-8000-000000000640" ||
		len(unknown.Rule.ContentHash) != sha256.Size {
		t.Fatalf("unsupported adjustment rule lineage = %#v", unknown.Rule)
	}
	if unknown.OriginalGrossMinor != 0 || unknown.OriginalNetMinor != 0 || unknown.OriginalGSTMinor != 0 ||
		unknown.AttributedGrossMinor != 0 || unknown.AttributedNetMinor != 0 || unknown.AttributedGSTMinor != 0 ||
		unknown.RemainingGrossMinor != 0 || unknown.RemainingNetMinor != 0 || unknown.RemainingGSTMinor != 0 {
		t.Fatalf("zero-valued adjustment changed during projection: %#v", unknown)
	}
	if unknown.JournalSourceID != "system-source" || unknown.JournalSourceRevision != 1 || unknown.JournalCreatedAt == "" ||
		unknown.CreatedAt != fixture.now.Format(time.RFC3339Nano) || unknown.SourceType != "unclassified_manual_evidence" ||
		unknown.SourceID != "018f0000-0000-7000-8000-000000000671" || unknown.SourceRevision != 1 {
		t.Fatalf("unknown adjustment lineage fields = %#v", unknown)
	}

	tax := byID[taxID]
	if tax.ID == "" || tax.JournalSourceType != "TAX" || tax.JournalSourceKind != 8 || tax.JournalState != "DRAFT" ||
		tax.Treatment != "ADJUSTMENT" || !tax.SourceContentHashPresent || !bytes.Equal(tax.SourceContentHash, taxSourceHash[:]) {
		t.Fatalf("TAX source-kind discriminator or source hash was lost: %#v", tax)
	}
	if payable := byID[payableID]; payable.ID == "" || payable.JournalSourceType != "PAYABLE" || payable.JournalSourceKind != 6 {
		t.Fatalf("PAYABLE source-kind discriminator was lost: %#v", payable)
	}
	if banking := byID[bankingID]; banking.ID == "" || banking.JournalSourceType != "BANKING" || banking.JournalSourceKind != 7 {
		t.Fatalf("BANKING source-kind discriminator was lost: %#v", banking)
	}
}

func TestJournalRepositoryKeepsLegacyNullSourceHashAfterMigration13(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	organisationRepository, _ := organisations.NewRepository(tx)
	if err := organisationRepository.Create(ctx, accountTestOrganisation(), time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	accounts := []*tammyv1.Account{
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000701", "1001", "Operating bank", "balance_sheet.cash"),
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000702", "5001", "Office expenses", "profit_loss.operating_expense"),
	}
	accountRepository, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		if err := accountRepository.Create(ctx, account, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	const journalID = "018f0000-0000-7000-8000-000000000710"
	const lineID = "018f0000-0000-7000-8000-000000000711"
	const factID = "018f0000-0000-7000-8000-000000000712"
	createdAt := "2026-08-10T12:00:00Z"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO journals(id,organisation_id,source_type,source_kind,source_id,source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at) VALUES(?,?,?,0,?,1,'DRAFT','2026-08-10','legacy tax fact',1,1,'AUD',1,?)`, []any{journalID, accountTestOrganisationID, "MANUAL", journalID, createdAt}},
		{`INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,1,?,1,0,'AUD')`, []any{lineID, journalID, accounts[0].Id}},
		{`INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,2,?,0,1,'AUD')`, []any{"018f0000-0000-7000-8000-000000000713", journalID, accounts[1].Id}},
		// Migration 12 has no source_content_hash column; this is an actual historical insert shape.
		{`INSERT INTO tax_facts(id,organisation_id,journal_line_id,tax_code,treatment,original_gross_minor,original_net_minor,original_gst_minor,attributed_gross_minor,attributed_net_minor,attributed_gst_minor,remaining_gross_minor,remaining_net_minor,remaining_gst_minor,tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,source_type,source_id,source_revision,created_at) VALUES(?,?,?,'GST','TAXABLE',1100,1000,100,1100,1000,100,0,0,0,'retained_rule','legacy-rule',1,zeroblob(32),'manual_evidence',?,1,?)`, []any{factID, accountTestOrganisationID, lineID, "018f0000-0000-7000-8000-000000000714", createdAt}},
	} {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed migration-12 legacy row: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlcipher.MigrateWorkspace(ctx, workspace.Path, workspace.Key, 13); err != nil {
		t.Fatalf("migrate legacy workspace to schema 13: %v", err)
	}
	database, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatalf("open migrated encrypted workspace: %v", err)
	}
	defer database.Close()
	readTx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	repository, err := accounting.NewJournalRepository(readTx)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := repository.CaptureTaxFactsForReporting(ctx, accountTestOrganisationID)
	if err != nil || capture == nil || len(capture.Facts) != 1 {
		t.Fatalf("migrated legacy capture = %#v, %v", capture, err)
	}
	legacy := capture.Facts[0]
	if legacy.ID != factID || legacy.JournalState != "DRAFT" || legacy.SourceContentHashPresent || legacy.SourceContentHash != nil {
		t.Fatalf("migration invented or lost the historical NULL source hash: %#v", legacy)
	}
}

func TestJournalRepositoryTaxFactReportingBoundReturnsNoPartialCapture(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000000801"
	if _, err := fixture.tx.ExecContext(fixture.ctx, `
		INSERT INTO journals(id,organisation_id,source_type,source_kind,source_id,source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at)
		VALUES(?,?,'SYSTEM',0,?,1,'DRAFT','2026-08-10','bounded reporting fixture',1,1,'AUD',1,?)`,
		journalID, accountTestOrganisationID, journalID, fixture.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	insertFact := func(index int) {
		t.Helper()
		lineID := taxFactReportingID(810 + index)
		factID := taxFactReportingID(3000 + index)
		if _, err := fixture.tx.ExecContext(fixture.ctx,
			`INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,?, ?,1,0,'AUD')`,
			lineID, journalID, index+1, fixture.accounts[0].Id); err != nil {
			t.Fatalf("insert bounded fixture line %d: %v", index, err)
		}
		if _, err := fixture.tx.ExecContext(fixture.ctx, `
			INSERT INTO tax_facts(id,organisation_id,journal_line_id,tax_code,treatment,
			 original_gross_minor,original_net_minor,original_gst_minor,
			 attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
			 remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
			 tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
			 source_type,source_id,source_revision,created_at,source_content_hash)
			VALUES(?,?,?,'GST','ADJUSTMENT',0,0,0,0,0,0,0,0,0,'retained_rule','legacy-rule',1,zeroblob(32),
			 'unclassified_manual_evidence',?,1,?,NULL)`,
			factID, accountTestOrganisationID, lineID, taxFactReportingID(5000+index), fixture.now.Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("insert bounded fixture fact %d: %v", index, err)
		}
	}
	for index := 0; index < 1000; index++ {
		insertFact(index)
	}
	withinBound, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || withinBound == nil || len(withinBound.Facts) != 1000 {
		t.Fatalf("capture at exact 1000-fact bound = facts:%d capture:%t err:%v", reportingFactCount(withinBound), withinBound != nil, err)
	}
	if withinBound.Facts[0].JournalLineID != taxFactReportingID(810) ||
		withinBound.Facts[999].JournalLineID != taxFactReportingID(1809) {
		t.Fatalf("bounded capture did not follow journal line order: first=%#v last=%#v", withinBound.Facts[0], withinBound.Facts[999])
	}
	insertFact(1000)
	overBound, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if !errors.Is(err, accounting.ErrTaxFactReportingBound) || overBound != nil {
		t.Fatalf("capture over bound = %#v, %v; want typed bound error and nil partial output", overBound, err)
	}
}

func TestJournalRepositoryTaxFactReportingRejectsInvalidOrganisationAndCanceledContext(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	invalid, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, "not-an-organisation-id")
	if err == nil || invalid != nil {
		t.Fatalf("capture with invalid organisation = %#v, %v; want error and nil output", invalid, err)
	}
	canceled, cancel := context.WithCancel(fixture.ctx)
	cancel()
	capture, err := fixture.repository.CaptureTaxFactsForReporting(canceled, accountTestOrganisationID)
	if !errors.Is(err, context.Canceled) || capture != nil {
		t.Fatalf("capture with canceled context = %#v, %v; want error and nil output", capture, err)
	}
}

func TestJournalRepositoryTaxFactReportingParentLineBoundReturnsNoPartialCapture(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000007001"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "bounded-parent", taxFactReportingID(7002),
		taxFactReportingID(7003), taxFactReportingID(7004), "ADJUSTMENT", nil)
	insertLine := func(sequence int) {
		t.Helper()
		if _, err := fixture.tx.ExecContext(fixture.ctx, `INSERT INTO journal_lines(
			id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code)
			VALUES(?,?,?,?,1,0,'AUD')`, taxFactReportingID(8000+sequence), journalID,
			sequence, fixture.accounts[0].Id); err != nil {
			t.Fatal(err)
		}
	}
	for sequence := 3; sequence <= 1000; sequence++ {
		insertLine(sequence)
	}
	within, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || within == nil || len(within.Facts) != 1 || len(within.Facts[0].Journal.Lines) != 1000 {
		t.Fatalf("capture at parent line bound = %#v, %v", within, err)
	}
	insertLine(1001)
	over, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	var bound *accounting.TaxFactReportingBoundError
	if !errors.Is(err, accounting.ErrTaxFactReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != "journal lines" || bound.Limit != 1000 || over != nil {
		t.Fatalf("one-fact capture above parent line bound = %#v, %v", over, err)
	}
}

func TestJournalRepositoryTaxFactReportingKeepsOrganisationCapturesIsolated(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000009001"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "wrong-owner-parent", taxFactReportingID(9002),
		taxFactReportingID(9003), taxFactReportingID(9004), "ADJUSTMENT", nil)
	otherOrgID := taxFactReportingID(9010)
	// A temporary second owner exercises legacy cross-owner foreign keys.
	// Restore the exact workspace guard before attempting the reporting read.
	var singletonGuard string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='organisations_singleton_insert'`).Scan(&singletonGuard); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx, `DROP TRIGGER organisations_singleton_insert`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`INSERT INTO organisations(id,legal_name,status,created_at) VALUES(?,'Other owner','INACTIVE',?)`,
		otherOrgID, fixture.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx, singletonGuard); err != nil {
		t.Fatal(err)
	}
	capture, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, otherOrgID)
	if err != nil || capture == nil || len(capture.Facts) != 0 {
		t.Fatalf("capture exposed another organisation's facts: %#v, %v", capture, err)
	}
	ownerCapture, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || ownerCapture == nil || len(ownerCapture.Facts) != 1 ||
		bytes.Equal(capture.Fingerprint, ownerCapture.Fingerprint) {
		t.Fatalf("owner capture lost its fact or organisation-bound fingerprint: %#v, %v", ownerCapture, err)
	}
}

func TestJournalRepositoryTaxFactReportingPayloadBoundReturnsNoPartialCapture(t *testing.T) {
	const captureByteLimit = 8 * 1024 * 1024
	for _, field := range []string{"journal description", "line memo", "fact source"} {
		t.Run(field, func(t *testing.T) {
			fixture := newTaxFactReportingFixture(t)
			journalID := taxFactReportingID(10001)
			lineID := taxFactReportingID(10002)
			factSourceID := "raw-evidence"
			// Fewer than 8 MiB characters, but more than 8 MiB UTF-8 bytes.
			largeText := strings.Repeat("界", captureByteLimit/3+1)
			if field == "fact source" {
				factSourceID = largeText
			}
			seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
				journalID, "SYSTEM", 0, "payload-bound", lineID, taxFactReportingID(10003),
				factSourceID, "ADJUSTMENT", nil)
			switch field {
			case "journal description":
				if _, err := fixture.tx.ExecContext(fixture.ctx,
					`UPDATE journals SET description=? WHERE id=? AND state='DRAFT'`, largeText, journalID); err != nil {
					t.Fatal(err)
				}
			case "line memo":
				if _, err := fixture.tx.ExecContext(fixture.ctx,
					`INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code,memo)
					 VALUES(?,?,3,?,1,0,'AUD',?)`, taxFactReportingID(10005), journalID,
					fixture.accounts[0].Id, largeText); err != nil {
					t.Fatal(err)
				}
			}
			capture, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
			assertTaxFactReportingPayloadBound(t, capture, err, captureByteLimit)
		})
	}
}

func TestJournalRepositoryTaxFactReportingPayloadBoundCountsUniqueParentsTogether(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const captureByteLimit = 8 * 1024 * 1024
	// Each parent is below the capture budget; their combined retained bytes
	// exceed it despite having only two facts and four journal lines.
	for index := 0; index < 2; index++ {
		journalID := taxFactReportingID(11001 + index*10)
		seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
			journalID, "SYSTEM", 0, fmt.Sprintf("aggregate-payload-%d", index),
			taxFactReportingID(11002+index*10), taxFactReportingID(11003+index*10),
			taxFactReportingID(11004+index*10), "ADJUSTMENT", nil)
		if _, err := fixture.tx.ExecContext(fixture.ctx,
			`UPDATE journals SET description=? WHERE id=? AND state='DRAFT'`,
			strings.Repeat("x", 5*1024*1024), journalID); err != nil {
			t.Fatal(err)
		}
		capture, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
		if index == 0 {
			if err != nil || capture == nil || len(capture.Facts) != 1 {
				t.Fatalf("one parent below the payload budget: capture=%t err=%v", capture != nil, err)
			}
		} else {
			assertTaxFactReportingPayloadBound(t, capture, err, captureByteLimit)
		}
	}
}

func assertTaxFactReportingPayloadBound(t *testing.T, capture *accounting.TaxFactReportingCapture, err error, limit int) {
	t.Helper()
	var bound *accounting.TaxFactReportingBoundError
	if !errors.Is(err, accounting.ErrTaxFactReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != "capture bytes" || bound.Limit != limit || capture != nil {
		t.Fatalf("oversized capture = capture:%t err:%v; want typed %d-byte bound and nil output", capture != nil, err, limit)
	}
}

func TestJournalRepositoryTaxFactReportingRetainsReversedNegativeFacts(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	sourceHash := sha256.Sum256([]byte("reversed reporting evidence"))
	ruleHash := sha256.Sum256([]byte("reversed reporting rule"))
	posted := fixture.post(t, 9501, taxFactReportingID(9510), sourceHash[:], ruleHash[:])
	before, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil {
		t.Fatal(err)
	}
	_, reversal, err := fixture.repository.Reverse(fixture.ctx, posted.journal.Id, 1,
		posted.journal.PostingDate, "Retain reversed tax source", taxFactReportingID(9520),
		[]string{taxFactReportingID(9521), taxFactReportingID(9522)}, fixture.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	after, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || after == nil || len(after.Facts) != 2 {
		t.Fatalf("reversed fact capture = %#v, %v", after, err)
	}
	negative := after.Facts[1]
	if negative.ID != reversal.Lines[1].Id+":tax" || negative.OriginalGrossMinor != -31900 ||
		negative.OriginalNetMinor != -29000 || negative.OriginalGSTMinor != -2900 ||
		negative.AttributedGrossMinor != -31900 || negative.AttributedNetMinor != -29000 || negative.AttributedGSTMinor != -2900 ||
		negative.RemainingGrossMinor != 0 || negative.RemainingNetMinor != 0 || negative.RemainingGSTMinor != 0 ||
		negative.JournalSourceType != "REVERSAL" || !negative.SourceContentHashPresent ||
		!bytes.Equal(negative.SourceContentHash, sourceHash[:]) || negative.Journal.ReversalOfJournalId == nil ||
		*negative.Journal.ReversalOfJournalId != posted.journal.Id {
		t.Fatalf("negative reversal fact lost signed amounts or lineage: %#v", negative)
	}
	if bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Fatal("adding an inverse fact left the coverage fingerprint unchanged")
	}
}

func TestJournalRepositoryTaxFactReportingHonorsOuterRollback(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	baseline, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || baseline == nil || len(baseline.Facts) != 0 {
		t.Fatalf("baseline capture = %#v, %v", baseline, err)
	}
	if err := fixture.tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rollbackTx, err := fixture.workspace.Database.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rollbackRepository, err := accounting.NewJournalRepository(rollbackTx)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := rollbackRepository.ReserveFinancialRevision(fixture.ctx, nil, fixture.now)
	if err != nil || revision != 1 {
		t.Fatalf("ReserveFinancialRevision() = %d, %v", revision, err)
	}
	sourceHash := sha256.Sum256([]byte("outer rollback evidence"))
	ruleHash := sha256.Sum256([]byte("outer rollback rule"))
	posted := suppliedHashTaxPosting(t, fixture.accounts, fixture.now, revision,
		"018f0000-0000-7000-8000-000000000901",
		"018f0000-0000-7000-8000-000000000902",
		"018f0000-0000-7000-8000-000000000903",
		"018f0000-0000-7000-8000-000000000904",
		"018f0000-0000-7000-8000-000000000905",
		"018f0000-0000-7000-8000-000000000906", sourceHash[:], ruleHash[:])
	if err := rollbackRepository.Post(fixture.ctx, posted.journal, "MANUAL", posted.journal.Id, 1, posted.facts, posted.flows, fixture.now); err != nil {
		t.Fatalf("Post() inside outer transaction: %v", err)
	}
	inside, err := rollbackRepository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || inside == nil || len(inside.Facts) != 1 {
		t.Fatalf("capture before outer rollback = %#v, %v", inside, err)
	}
	if err := rollbackTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	afterTx, err := fixture.workspace.Database.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer afterTx.Rollback()
	afterRepository, err := accounting.NewJournalRepository(afterTx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := afterRepository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || after == nil || len(after.Facts) != 0 || !bytes.Equal(after.Fingerprint, baseline.Fingerprint) {
		t.Fatalf("capture after outer rollback = %#v, %v; baseline fingerprint %x", after, err, baseline.Fingerprint)
	}
}

type taxFactReportingFixture struct {
	ctx        context.Context
	workspace  *testkit.EncryptedWorkspace
	tx         *sqlcipher.Transaction
	repository *accounting.JournalRepository
	accounts   []*tammyv1.Account
	now        time.Time
}

func newTaxFactReportingFixture(t *testing.T) *taxFactReportingFixture {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	organisationRepository, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := organisationRepository.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	accounts := []*tammyv1.Account{
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000401", "1001", "Operating bank", "balance_sheet.cash"),
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000402", "5001", "Office expenses", "profit_loss.operating_expense"),
	}
	accountRepository, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		if err := accountRepository.Create(ctx, account, now); err != nil {
			t.Fatal(err)
		}
	}
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	return &taxFactReportingFixture{ctx: ctx, workspace: workspace, tx: tx, repository: repository, accounts: accounts, now: now}
}

func (fixture *taxFactReportingFixture) post(t *testing.T, seed int, sourceID string, sourceHash, ruleHash []byte) suppliedHashPosting {
	t.Helper()
	revision, err := fixture.repository.ReserveFinancialRevision(fixture.ctx, nil, fixture.now)
	if err != nil {
		t.Fatalf("ReserveFinancialRevision() = %v", err)
	}
	posted := suppliedHashTaxPosting(t, fixture.accounts, fixture.now, revision,
		taxFactReportingID(seed), taxFactReportingID(seed+1), taxFactReportingID(seed+2),
		taxFactReportingID(seed+3), taxFactReportingID(seed+4), sourceID, sourceHash, ruleHash)
	if err := fixture.repository.Post(fixture.ctx, posted.journal, "MANUAL", posted.journal.Id, 1, posted.facts, posted.flows, fixture.now); err != nil {
		t.Fatalf("Post() = %v", err)
	}
	return posted
}

func assertTaxFactReportingPosting(
	t *testing.T,
	repository *accounting.JournalRepository,
	record accounting.TaxFactReportingRecord,
	posted suppliedHashPosting,
	sourceHash, ruleHash []byte,
	now time.Time,
) {
	t.Helper()
	if record.ID != posted.factID || record.OrganisationID != accountTestOrganisationID ||
		record.JournalLineID != posted.journal.Lines[1].Id || record.TaxCode != "GST" || record.Treatment != "TAXABLE" {
		t.Fatalf("reporting fact identity = %#v", record)
	}
	if record.OriginalGrossMinor != 31900 || record.OriginalNetMinor != 29000 || record.OriginalGSTMinor != 2900 ||
		record.AttributedGrossMinor != 31900 || record.AttributedNetMinor != 29000 || record.AttributedGSTMinor != 2900 ||
		record.RemainingGrossMinor != 0 || record.RemainingNetMinor != 0 || record.RemainingGSTMinor != 0 {
		t.Fatalf("reporting fact lost signed amount fields: %#v", record)
	}
	if record.Rule == nil || record.Rule.Type != "tax_rule_bundle" || record.Rule.Id != "018f0000-0000-7000-8000-000000000430" ||
		record.Rule.Revision != 1 || !bytes.Equal(record.Rule.ContentHash, ruleHash) {
		t.Fatalf("reporting fact rule lineage = %#v", record.Rule)
	}
	expectedSource := posted.facts[posted.journal.Lines[1].Id].Source
	if expectedSource == nil || record.SourceType != expectedSource.Type || record.SourceID != expectedSource.Id ||
		record.SourceRevision != expectedSource.Revision ||
		!record.SourceContentHashPresent || !bytes.Equal(record.SourceContentHash, sourceHash) {
		t.Fatalf("reporting fact source lineage = type:%q id:%q rev:%d hashPresent:%t hash:%x",
			record.SourceType, record.SourceID, record.SourceRevision, record.SourceContentHashPresent, record.SourceContentHash)
	}
	if record.CreatedAt != now.Format(time.RFC3339Nano) || record.JournalSourceType != "MANUAL" ||
		record.JournalSourceKind != 0 || record.JournalSourceID != posted.journal.Id ||
		record.JournalSourceRevision != 1 || record.JournalCreatedAt == "" || record.JournalState != "POSTED" {
		t.Fatalf("reporting fact journal metadata = %#v", record)
	}
	expectedJournal, err := repository.Get(context.Background(), posted.journal.Id)
	if err != nil {
		t.Fatalf("JournalRepository.Get(%q): %v", posted.journal.Id, err)
	}
	if record.Journal == nil || !proto.Equal(record.Journal, expectedJournal) || len(record.Journal.Lines) != 2 {
		t.Fatalf("reporting record omitted or changed full journal and lines: got=%#v want=%#v", record.Journal, expectedJournal)
	}
}

func taxFactReportingAmountsEqual(left, right accounting.TaxFactReportingRecord) bool {
	return left.OriginalGrossMinor == right.OriginalGrossMinor && left.OriginalNetMinor == right.OriginalNetMinor &&
		left.OriginalGSTMinor == right.OriginalGSTMinor && left.AttributedGrossMinor == right.AttributedGrossMinor &&
		left.AttributedNetMinor == right.AttributedNetMinor && left.AttributedGSTMinor == right.AttributedGSTMinor &&
		left.RemainingGrossMinor == right.RemainingGrossMinor && left.RemainingNetMinor == right.RemainingNetMinor &&
		left.RemainingGSTMinor == right.RemainingGSTMinor
}

func seedTaxFactReportingDraft(
	t *testing.T,
	tx *sqlcipher.Transaction,
	accounts []*tammyv1.Account,
	now time.Time,
	journalID, sourceType string,
	sourceKind int64,
	journalSourceID, factLineID, factID, factSourceID, treatment string,
	sourceContentHash []byte,
) {
	t.Helper()
	var retainedSourceHash any
	if sourceContentHash != nil {
		retainedSourceHash = sourceContentHash
	}
	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO journals(id,organisation_id,source_type,source_kind,source_id,source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,currency_code,financial_revision,created_at)
		VALUES(?,?,?,?,?,1,'DRAFT','2026-08-10','raw reporting projection fixture',1,1,'AUD',1,?)`,
		journalID, accountTestOrganisationID, sourceType, sourceKind, journalSourceID, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert raw %s journal: %v", sourceType, err)
	}
	otherLineID := journalID + "-balancing-line"
	for index, values := range []struct {
		lineID  string
		account string
		debit   int64
		credit  int64
	}{
		{factLineID, accounts[0].Id, 1, 0},
		{otherLineID, accounts[1].Id, 0, 1},
	} {
		if _, err := tx.ExecContext(context.Background(),
			`INSERT INTO journal_lines(id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code) VALUES(?,?,?,?,?,?,'AUD')`,
			values.lineID, journalID, index+1, values.account, values.debit, values.credit); err != nil {
			t.Fatalf("insert raw journal line: %v", err)
		}
	}
	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO tax_facts(id,organisation_id,journal_line_id,tax_code,treatment,
		 original_gross_minor,original_net_minor,original_gst_minor,
		 attributed_gross_minor,attributed_net_minor,attributed_gst_minor,
		 remaining_gross_minor,remaining_net_minor,remaining_gst_minor,
		 tax_rule_type,tax_rule_id,tax_rule_revision,tax_rule_content_hash,
		 source_type,source_id,source_revision,created_at,source_content_hash)
		VALUES(?,?,?,'GST',?,0,0,0,0,0,0,0,0,0,'retained_rule','018f0000-0000-7000-8000-000000000640',1,zeroblob(32),
		 'unclassified_manual_evidence',?,1,?,?)`,
		factID, accountTestOrganisationID, factLineID, treatment, factSourceID, now.Format(time.RFC3339Nano), retainedSourceHash); err != nil {
		t.Fatalf("insert raw tax fact %q: %v", factID, err)
	}
}

func taxFactReportingID(value int) string {
	return fmt.Sprintf("018f0000-0000-7000-8000-%012d", value)
}

func reportingFactCount(capture *accounting.TaxFactReportingCapture) int {
	if capture == nil {
		return 0
	}
	return len(capture.Facts)
}
