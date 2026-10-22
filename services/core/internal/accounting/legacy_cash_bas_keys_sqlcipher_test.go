//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

// Proposed API under test:
//
// type BASLegacyCashQuery struct { OrganisationID string; SourceIDs []string }
// type BASLegacyCashSummary struct {
//     OrganisationID string
//     AssociationCount, JournalCount, LineCount int64
//     RowCount, Bytes int64
// }
// type BASLegacyCashAssociation struct {
//     SourceID, JournalID, Kind string
//     TaxFactID, JournalLineID, FactSourceType *string
//     FactSourceRevision *uint64
//     FactSourceContentHashPresent bool
//     FactSourceContentHash []byte
// }
// type BASLegacyCashJournal struct {
//     OrganisationID, ID string
//     Version, SourceRevision uint64
//     SourceKind int64
//     SourceType, SourceID, State, JournalDate string
//     ReversalOfID, ReversedByID *string
// }
// type BASLegacyCashLine struct {
//     JournalID, ID, AccountID, AccountOrganisationID string
//     Sequence, AccountVersion uint64
//     DebitMinor, CreditMinor int64
//     ReportClassification string
// }
// type BASLegacyCashRead struct {
//     Summary BASLegacyCashSummary
//     Associations []BASLegacyCashAssociation
//     Journals []BASLegacyCashJournal
//     Lines []BASLegacyCashLine
// }
// func (*JournalRepository) SummarizeBASLegacyCash(context.Context, BASLegacyCashQuery) (*BASLegacyCashSummary,error)
// func (*JournalRepository) ReadBASLegacyCash(context.Context, BASLegacyCashQuery, BASLegacyCashSummary, int64, int64) (*BASLegacyCashRead,error)

const legacyCashTestOrg = accountTestOrganisationID

func TestJournalRepositoryBASLegacyCashRetainsWriterLinksReversalAndCurrentAccounts(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const (
		documentID = "018f0000-0000-7000-8000-000000017900"
		reversalID = "018f0000-0000-7000-8000-000000017906"
	)
	sourceHash := sha256.Sum256([]byte("retained manual evidence source"))
	ruleHash := sha256.Sum256([]byte("retained tax rule"))
	posted := fixture.post(t, 17901, documentID, sourceHash[:], ruleHash[:])
	originalID := posted.journal.Id
	reversalLineIDs := []string{taxFactReportingID(17907), taxFactReportingID(17908)}
	reversed, reversal, err := fixture.repository.Reverse(fixture.ctx, originalID, posted.journal.Version,
		&tammyv1.CivilDate{Year: 2026, Month: 8, Day: 11}, "Reverse linked journal", reversalID,
		reversalLineIDs, fixture.now.Add(time.Minute))
	if err != nil || reversed == nil || reversal == nil {
		t.Fatalf("writer reverse = original:%#v reversal:%#v err:%v", reversed, reversal, err)
	}

	// Reclassify the current account after both immutable journals are written.
	// The projection must carry account version/classification independently of
	// journal version and must not filter an account out when its class changes.
	accounts, err := accounting.NewAccountRepository(fixture.tx)
	if err != nil {
		t.Fatal(err)
	}
	updatedCash := proto.Clone(fixture.accounts[0]).(*tammyv1.Account)
	updatedCash.Version++
	updatedCash.ReportClassification = "balance_sheet.cash_equivalent"
	if err := accounts.Update(fixture.ctx, fixture.accounts[0].Version, updatedCash, fixture.now.Add(2*time.Minute)); err != nil {
		t.Fatalf("reclassify current account: %v", err)
	}

	query := accounting.BASLegacyCashQuery{OrganisationID: legacyCashTestOrg,
		SourceIDs: []string{documentID, originalID}}
	before := journalReportingPreflightState(t, fixture)
	probe := &basLegacyCashSelectorProbe{Transaction: fixture.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := repository.SummarizeBASLegacyCash(fixture.ctx, query)
	if err != nil || summary == nil {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	if summary.OrganisationID != legacyCashTestOrg || summary.AssociationCount != 4 ||
		summary.JournalCount != 2 || summary.LineCount != 4 || summary.RowCount != 10 ||
		probe.unscopedBodyQueries != 0 {
		t.Fatalf("aggregate selector summary = %#v; unscoped body queries=%d", summary, probe.unscopedBodyQueries)
	}

	read, err := repository.ReadBASLegacyCash(fixture.ctx, query, *summary, 16_384, 8*1024*1024)
	if err != nil || read == nil {
		t.Fatalf("read = %#v, %v", read, err)
	}
	originalFactID := posted.factID
	reversalFactID := reversal.Lines[1].Id + ":tax"
	originalFactLineID := posted.journal.Lines[1].Id
	reversalFactLineID := reversal.Lines[1].Id
	wantAssociations := []accounting.BASLegacyCashAssociation{
		{SourceID: originalID, JournalID: originalID, Kind: "DIRECT"},
		{SourceID: originalID, JournalID: reversalID, Kind: "DIRECT"},
		{SourceID: documentID, JournalID: originalID, Kind: "TAX_FACT",
			TaxFactID: legacyCashString(originalFactID), JournalLineID: legacyCashString(originalFactLineID),
			FactSourceType: legacyCashString("manual_evidence"), FactSourceRevision: legacyCashUint64(1),
			FactSourceContentHashPresent: true, FactSourceContentHash: bytes.Clone(sourceHash[:])},
		{SourceID: documentID, JournalID: reversalID, Kind: "TAX_FACT",
			TaxFactID: legacyCashString(reversalFactID), JournalLineID: legacyCashString(reversalFactLineID),
			FactSourceType: legacyCashString("manual_evidence"), FactSourceRevision: legacyCashUint64(1),
			FactSourceContentHashPresent: true, FactSourceContentHash: bytes.Clone(sourceHash[:])},
	}
	associationLess := func(rows []accounting.BASLegacyCashAssociation) func(int, int) bool {
		return func(i, j int) bool {
			left, right := rows[i], rows[j]
			if left.SourceID != right.SourceID {
				return left.SourceID < right.SourceID
			}
			if left.JournalID != right.JournalID {
				return left.JournalID < right.JournalID
			}
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			return legacyCashOptional(left.TaxFactID) < legacyCashOptional(right.TaxFactID)
		}
	}
	sort.Slice(wantAssociations, associationLess(wantAssociations))
	sort.Slice(read.Associations, associationLess(read.Associations))
	if !reflect.DeepEqual(read.Associations, wantAssociations) {
		t.Fatalf("retained source associations = %#v; want %#v", read.Associations, wantAssociations)
	}

	wantJournals := []accounting.BASLegacyCashJournal{
		{OrganisationID: legacyCashTestOrg, ID: originalID, Version: reversed.Version, SourceRevision: 1,
			SourceKind: 0, SourceType: "MANUAL", SourceID: originalID, State: "REVERSED", JournalDate: "2026-08-10",
			ReversedByID: legacyCashString(reversalID)},
		{OrganisationID: legacyCashTestOrg, ID: reversalID, Version: reversal.Version, SourceRevision: reversed.Version - 1,
			SourceKind: 0, SourceType: "REVERSAL", SourceID: originalID, State: "POSTED", JournalDate: "2026-08-11",
			ReversalOfID: legacyCashString(originalID)},
	}
	sort.Slice(wantJournals, func(i, j int) bool { return wantJournals[i].ID < wantJournals[j].ID })
	sort.Slice(read.Journals, func(i, j int) bool { return read.Journals[i].ID < read.Journals[j].ID })
	if !reflect.DeepEqual(read.Journals, wantJournals) {
		t.Fatalf("selected writer headers = %#v; want %#v", read.Journals, wantJournals)
	}

	wantLines := make([]accounting.BASLegacyCashLine, 0, 4)
	for _, journal := range []*tammyv1.Journal{reversed, reversal} {
		for _, line := range journal.Lines {
			accountVersion, classification := uint64(1), "profit_loss.operating_expense"
			if line.AccountId == fixture.accounts[0].Id {
				accountVersion, classification = updatedCash.Version, updatedCash.ReportClassification
			}
			wantLines = append(wantLines, accounting.BASLegacyCashLine{
				JournalID: journal.Id, ID: line.Id, AccountID: line.AccountId, AccountOrganisationID: legacyCashTestOrg,
				Sequence: uint64(line.Sequence), AccountVersion: accountVersion,
				DebitMinor: line.GetDebit().GetMinorUnits(), CreditMinor: line.GetCredit().GetMinorUnits(),
				ReportClassification: classification,
			})
		}
	}
	lineLess := func(rows []accounting.BASLegacyCashLine) func(int, int) bool {
		return func(i, j int) bool {
			if rows[i].JournalID != rows[j].JournalID {
				return rows[i].JournalID < rows[j].JournalID
			}
			if rows[i].Sequence != rows[j].Sequence {
				return rows[i].Sequence < rows[j].Sequence
			}
			return rows[i].ID < rows[j].ID
		}
	}
	sort.Slice(wantLines, lineLess(wantLines))
	sort.Slice(read.Lines, lineLess(read.Lines))
	if !reflect.DeepEqual(read.Lines, wantLines) {
		t.Fatalf("all lines/current accounts = %#v; want %#v", read.Lines, wantLines)
	}

	wantBytes := legacyCashCharge(wantAssociations, wantJournals, wantLines)
	wantSummary := accounting.BASLegacyCashSummary{
		OrganisationID: legacyCashTestOrg, AssociationCount: 4, JournalCount: 2, LineCount: 4,
		RowCount: 10, Bytes: wantBytes,
	}
	if !reflect.DeepEqual(read.Summary, wantSummary) {
		t.Fatalf("returned charge = %#v; want %#v", read.Summary, wantSummary)
	}
	if probe.unscopedBodyQueries != 0 || !fixture.tx.Authenticated() ||
		!equalJournalReportingPreflightState(before, journalReportingPreflightState(t, fixture)) {
		t.Fatalf("scalar selector loaded bodies or changed transaction; unscoped body queries=%d", probe.unscopedBodyQueries)
	}
	for _, limits := range []struct {
		name        string
		rows, bytes int64
	}{
		{name: "one-short row cap", rows: read.Summary.RowCount - 1, bytes: read.Summary.Bytes},
		{name: "one-short byte cap", rows: read.Summary.RowCount, bytes: read.Summary.Bytes - 1},
	} {
		t.Run(limits.name, func(t *testing.T) {
			got, err := repository.ReadBASLegacyCash(fixture.ctx, query, *summary, limits.rows, limits.bytes)
			if got != nil || err == nil {
				t.Fatalf("short limit returned partial result=%#v error=%v", got, err)
			}
		})
	}
}

type basLegacyCashSelectorProbe struct {
	*sqlcipher.Transaction
	unscopedBodyQueries int
}

func (probe *basLegacyCashSelectorProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	// The scalar API tags its own aggregate/projection queries. A call into the
	// ordinary Journal/TaxFact/Account body readers is untagged and touches one
	// of these source tables. The selector must stay in its scalar layer.
	if !strings.Contains(query, "BAS legacy cash") &&
		(strings.Contains(query, "FROM journals") || strings.Contains(query, "FROM journal_lines") ||
			strings.Contains(query, "FROM tax_facts") || strings.Contains(query, "FROM accounts")) {
		probe.unscopedBodyQueries++
	}
	return probe.Transaction.QueryContext(ctx, query, args...)
}

func legacyCashString(value string) *string { return &value }
func legacyCashUint64(value uint64) *uint64 { return &value }
func legacyCashOptional(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func legacyCashCharge(
	associations []accounting.BASLegacyCashAssociation,
	journals []accounting.BASLegacyCashJournal,
	lines []accounting.BASLegacyCashLine,
) int64 {
	charged := int64(len(associations)+len(journals)+len(lines)) * 512
	add := func(value string) { charged += int64(len(value)) }
	addOptional := func(value *string) {
		if value != nil {
			add(*value)
		}
	}
	for _, row := range associations {
		add(row.SourceID)
		add(row.JournalID)
		add(row.Kind)
		addOptional(row.TaxFactID)
		addOptional(row.JournalLineID)
		addOptional(row.FactSourceType)
		if row.FactSourceContentHashPresent {
			charged += int64(len(row.FactSourceContentHash))
		}
	}
	for _, row := range journals {
		add(row.OrganisationID)
		add(row.ID)
		add(row.SourceType)
		add(row.SourceID)
		add(row.State)
		add(row.JournalDate)
		addOptional(row.ReversalOfID)
		addOptional(row.ReversedByID)
	}
	for _, row := range lines {
		add(row.JournalID)
		add(row.ID)
		add(row.AccountID)
		add(row.AccountOrganisationID)
		add(row.ReportClassification)
	}
	return charged
}
