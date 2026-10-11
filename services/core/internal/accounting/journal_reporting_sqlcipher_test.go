//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

const journalReportingCaptureByteLimit = 8 * 1024 * 1024

func TestJournalRepositoryGetForReportingCapturesCompleteNoFactJournal(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journal := postJournalReportingFixture(t, fixture, 12001)
	var taxFactCount int
	if err := fixture.tx.QueryRowContext(fixture.ctx, `SELECT count(*) FROM tax_facts AS fact
		JOIN journal_lines AS line ON line.id=fact.journal_line_id WHERE line.journal_id=?`, journal.Id).Scan(&taxFactCount); err != nil {
		t.Fatalf("count posted journal tax facts: %v", err)
	}
	if taxFactCount != 0 {
		t.Fatalf("posted journal unexpectedly has %d tax facts", taxFactCount)
	}

	record, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journal.Id)
	if err != nil {
		t.Fatalf("GetForReporting() = %v", err)
	}
	if record == nil || record.Journal == nil || len(record.ContentHash) != sha256.Size {
		t.Fatalf("reporting record = %#v; want full journal and SHA-256 content hash", record)
	}
	if !proto.Equal(record.Journal, journal) || len(record.Journal.Lines) != 2 {
		t.Fatalf("reporting journal = %#v; want the complete posted journal %#v", record.Journal, journal)
	}
	if record.SourceType != "MANUAL" || record.SourceKind != 0 || record.SourceID != journal.Id ||
		record.SourceRevision != 1 || record.State != "POSTED" || record.CreatedAt != fixture.now.Format(time.RFC3339Nano) {
		t.Fatalf("reporting source tuple/state = %#v", record)
	}
	if record.PayloadBytes < int64(proto.Size(record.Journal)) || record.PayloadBytes > journalReportingCaptureByteLimit {
		t.Fatalf("reporting payload bytes %d do not cover the projected journal within its budget", record.PayloadBytes)
	}

	repeated, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journal.Id)
	if err != nil || repeated == nil || !bytes.Equal(record.ContentHash, repeated.ContentHash) {
		t.Fatalf("repeated reporting read = %#v, %v; want stable content hash %x", repeated, err, record.ContentHash)
	}
}

func TestJournalRepositoryGetForReportingPreservesRawDraftSystemMetadata(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000012101"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "unclassified-system-source", taxFactReportingID(12102),
		taxFactReportingID(12103), taxFactReportingID(12104), "ADJUSTMENT", nil)

	record, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journalID)
	if err != nil || record == nil || record.Journal == nil {
		t.Fatalf("GetForReporting() = %#v, %v; want retained DRAFT/SYSTEM journal", record, err)
	}
	if record.SourceType != "SYSTEM" || record.SourceKind != 0 || record.SourceID != "unclassified-system-source" ||
		record.SourceRevision != 1 || record.State != "DRAFT" || record.CreatedAt != fixture.now.Format(time.RFC3339Nano) {
		t.Fatalf("raw DRAFT/SYSTEM source metadata = %#v", record)
	}
	if record.Journal.State != tammyv1.JournalState_JOURNAL_STATE_UNSPECIFIED ||
		record.Journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_UNSPECIFIED || len(record.Journal.Lines) != 2 {
		t.Fatalf("DRAFT/SYSTEM protobuf projection = %#v", record.Journal)
	}
}

func TestJournalRepositoryGetForReportingBindsOrganisation(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journal := postJournalReportingFixture(t, fixture, 12201)

	record, err := fixture.repository.GetForReporting(fixture.ctx, taxFactReportingID(12210), journal.Id)
	if err == nil || record != nil {
		t.Fatalf("wrong-organisation reporting read = %#v, %v; want an error and nil record", record, err)
	}
}

func TestJournalRepositoryGetForReportingRetainsLegacyJournalIdentity(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "legacy-journal-identity"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "legacy-source", "legacy-journal-line",
		taxFactReportingID(12221), "legacy-fact-source", "ADJUSTMENT", nil)
	record, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journalID)
	if err != nil || record == nil || record.Journal.Id != journalID ||
		record.Journal.Lines[0].Id != "legacy-journal-line" || record.SourceID != "legacy-source" {
		t.Fatalf("retained legacy journal identity = %#v, %v", record, err)
	}
}

func TestJournalRepositoryGetForReportingHashesRawParentChanges(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000012201"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "draft-source", taxFactReportingID(12202),
		taxFactReportingID(12203), taxFactReportingID(12204), "ADJUSTMENT", nil)

	before, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journalID)
	if err != nil || before == nil {
		t.Fatalf("initial GetForReporting() = %#v, %v", before, err)
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`UPDATE journals SET description='updated raw parent memo' WHERE id=? AND state='DRAFT'`, journalID); err != nil {
		t.Fatalf("update mutable draft description: %v", err)
	}
	after, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journalID)
	if err != nil || after == nil {
		t.Fatalf("updated GetForReporting() = %#v, %v", after, err)
	}
	if bytes.Equal(before.ContentHash, after.ContentHash) {
		t.Fatalf("changing only raw parent description left content hash unchanged: %x", before.ContentHash)
	}
	if after.Journal.Memo != "updated raw parent memo" || len(after.Journal.Lines) != len(before.Journal.Lines) {
		t.Fatalf("changed parent projection = %#v", after.Journal)
	}
}

func TestJournalRepositoryGetForReportingLineBoundReturnsNoPartialRecord(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000012301"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "bounded-system-source", taxFactReportingID(12302),
		taxFactReportingID(12303), taxFactReportingID(12304), "ADJUSTMENT", nil)
	for sequence := 3; sequence <= 1001; sequence++ {
		if _, err := fixture.tx.ExecContext(fixture.ctx, `INSERT INTO journal_lines(
			id,journal_id,line_number,account_id,debit_minor,credit_minor,currency_code)
			VALUES(?,?,?,?,1,0,'AUD')`, taxFactReportingID(13000+sequence), journalID,
			sequence, fixture.accounts[0].Id); err != nil {
			t.Fatalf("insert parent line %d: %v", sequence, err)
		}
	}

	record, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journalID)
	assertJournalReportingBound(t, record, err, "journal lines", 1000)
}

func TestJournalRepositoryGetForReportingPayloadBoundReturnsNoPartialRecord(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const journalID = "018f0000-0000-7000-8000-000000012401"
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "oversized-system-source", taxFactReportingID(12402),
		taxFactReportingID(12403), taxFactReportingID(12404), "ADJUSTMENT", nil)
	largeText := strings.Repeat("界", journalReportingCaptureByteLimit/3+1)
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		`UPDATE journals SET description=? WHERE id=? AND state='DRAFT'`, largeText, journalID); err != nil {
		t.Fatalf("set oversized parent metadata: %v", err)
	}

	record, err := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journalID)
	assertJournalReportingBound(t, record, err, "capture bytes", journalReportingCaptureByteLimit)
}

func TestJournalRepositoryGetForReportingHonorsCanceledContext(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journal := postJournalReportingFixture(t, fixture, 12501)
	canceled, cancel := context.WithCancel(fixture.ctx)
	cancel()

	record, err := fixture.repository.GetForReporting(canceled, accountTestOrganisationID, journal.Id)
	if !errors.Is(err, context.Canceled) || record != nil {
		t.Fatalf("canceled GetForReporting() = %#v, %v; want context.Canceled and nil record", record, err)
	}
}

func TestJournalRepositoryGetForReportingRespectsCallerRollback(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	if err := fixture.tx.Commit(); err != nil {
		t.Fatalf("commit fixture setup: %v", err)
	}

	writeTx, err := fixture.workspace.Database.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin caller write transaction: %v", err)
	}
	defer writeTx.Rollback()
	writeRepository, err := accounting.NewJournalRepository(writeTx)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := writeRepository.ReserveFinancialRevision(fixture.ctx, nil, fixture.now)
	if err != nil {
		t.Fatalf("ReserveFinancialRevision() = %v", err)
	}
	journal := repositoryJournal(fixture.accounts, revision, fixture.now)
	if err := writeRepository.Post(fixture.ctx, journal, "MANUAL", journal.Id, 1, nil, journalReportingCashFlows(journal), fixture.now); err != nil {
		t.Fatalf("Post() inside caller transaction: %v", err)
	}
	inside, err := writeRepository.GetForReporting(fixture.ctx, accountTestOrganisationID, journal.Id)
	if err != nil || inside == nil || inside.SourceType != "MANUAL" {
		_ = writeTx.Rollback()
		t.Fatalf("reporting read inside caller transaction = %#v, %v", inside, err)
	}
	if err := writeTx.Rollback(); err != nil {
		t.Fatalf("rollback caller transaction: %v", err)
	}

	readTx, err := fixture.workspace.Database.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin verification transaction: %v", err)
	}
	defer readTx.Rollback()
	readRepository, err := accounting.NewJournalRepository(readTx)
	if err != nil {
		t.Fatal(err)
	}
	afterRollback, err := readRepository.GetForReporting(fixture.ctx, accountTestOrganisationID, journal.Id)
	if err == nil || afterRollback != nil {
		t.Fatalf("reporting read after caller rollback = %#v, %v; want absent row", afterRollback, err)
	}
}

func postJournalReportingFixture(
	t *testing.T,
	fixture *taxFactReportingFixture,
	financialRevisionSeed int,
) *tammyv1.Journal {
	t.Helper()
	revision, err := fixture.repository.ReserveFinancialRevision(fixture.ctx, nil, fixture.now)
	if err != nil {
		t.Fatalf("ReserveFinancialRevision() = %v", err)
	}
	journal := repositoryJournal(fixture.accounts, revision, fixture.now)
	journal.Id = taxFactReportingID(financialRevisionSeed)
	for index, line := range journal.Lines {
		line.Id = taxFactReportingID(financialRevisionSeed + index + 1)
		line.JournalId = journal.Id
	}
	if err := fixture.repository.Post(fixture.ctx, journal, "MANUAL", journal.Id, 1, nil, journalReportingCashFlows(journal), fixture.now); err != nil {
		t.Fatalf("Post() without tax facts: %v", err)
	}
	return journal
}

func journalReportingCashFlows(journal *tammyv1.Journal) map[string][]accounting.CashFlowComponent {
	return map[string][]accounting.CashFlowComponent{
		journal.Lines[0].Id: {{Category: accounting.CashFlowOperating, AmountMinor: 31900}},
		journal.Lines[1].Id: {{Category: accounting.CashFlowNoncash, AmountMinor: -31900}},
	}
}

func assertJournalReportingBound(
	t *testing.T,
	record *accounting.JournalReportingRecord,
	err error,
	resource string,
	limit int,
) {
	t.Helper()
	var bound *accounting.TaxFactReportingBoundError
	if !errors.Is(err, accounting.ErrTaxFactReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != resource || bound.Limit != limit || record != nil {
		t.Fatalf("bounded reporting record = %#v, %v; want %q limit %d and nil result", record, err, resource, limit)
	}
}
