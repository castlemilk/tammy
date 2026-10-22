package purchases

import (
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestBASLegacyCapturedCashProofsPreservesKnownParentExclusionBoundary(t *testing.T) {
	const (
		organisationID = "018f0000-0000-7000-8000-000000000001"
		sourceID       = "018f0000-0000-7000-8000-000000000101"
		journalID      = "opaque-retained-journal"
		lineID         = "opaque-retained-line"
		factID         = "opaque-retained-fact"
	)
	parents := []struct {
		name       string
		sourceKind int64
		sourceType string
	}{
		{name: "payable", sourceKind: 6, sourceType: "PAYABLE"},
		{name: "banking", sourceKind: 7, sourceType: "BANKING"},
		{name: "tax", sourceKind: 8, sourceType: "TAX"},
		{name: "opening", sourceKind: 0, sourceType: "OPENING"},
	}
	for _, parent := range parents {
		t.Run(parent.name, func(t *testing.T) {
			factRow := PurchaseReportingRetainedRow{
				Table:   "tax_facts",
				Columns: []string{"id", "source_id", "journal_line_id", "source_type", "source_revision", "source_content_hash"},
				Values: []PurchaseReportingRetainedValue{
					{Kind: "TEXT", Text: factID},
					{Kind: "TEXT", Text: sourceID},
					{Kind: "TEXT", Text: lineID},
					{Kind: "TEXT", Text: "historical_manual_fact"},
					{Kind: "INTEGER", Integer: 1},
					{Kind: "NULL"},
				},
			}
			proof := &accounting.JournalReportingRecord{
				Journal: &tammyv1.Journal{
					Id: journalID, OrganisationId: organisationID, Version: 1,
					State:       tammyv1.JournalState_JOURNAL_STATE_POSTED,
					PostingDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 15},
					Lines:       []*tammyv1.JournalLine{{Id: lineID, JournalId: journalID}},
				},
				SourceType: parent.sourceType, SourceKind: parent.sourceKind,
				SourceID: sourceID, SourceRevision: 1, State: "POSTED", ContentHash: make([]byte, 32),
			}
			discovery := &BASLegacyCandidateDiscovery{Nominations: []BASLegacyNomination{{
				Key: BASLegacyCandidateKey{Kind: BASLegacyCandidateDocument, OrganisationID: organisationID, SourceID: sourceID},
			}}}
			capture := &PurchaseCoverageReportingCapture{OrganisationID: organisationID, Journals: []*accounting.JournalReportingRecord{proof}}
			cash := accounting.BASLegacyCashRead{Summary: accounting.BASLegacyCashSummary{OrganisationID: organisationID}}
			got, err := basLegacyCapturedCashProofs(organisationID, discovery, cash, capture, nil,
				map[string]PurchaseReportingRetainedRow{factID: factRow})
			if err != nil || got == nil || len(got) != 1 || got[journalID] != proof {
				t.Fatalf("known parent fact should be excluded from cash associations after link proof: proofs=%v err=%v", got, err)
			}
		})
	}

	newCase := func(withParent, withLine bool) (*BASLegacyCandidateDiscovery, *PurchaseCoverageReportingCapture,
		accounting.BASLegacyCashRead, map[string]PurchaseReportingRetainedRow) {
		lineID := "opaque-retained-line"
		if !withLine {
			lineID = "missing-retained-line"
		}
		factRow := PurchaseReportingRetainedRow{
			Table:   "tax_facts",
			Columns: []string{"id", "source_id", "journal_line_id", "source_type", "source_revision", "source_content_hash"},
			Values: []PurchaseReportingRetainedValue{
				{Kind: "TEXT", Text: factID}, {Kind: "TEXT", Text: sourceID},
				{Kind: "TEXT", Text: lineID}, {Kind: "TEXT", Text: "historical_manual_fact"},
				{Kind: "INTEGER", Integer: 1}, {Kind: "NULL"},
			},
		}
		discovery := &BASLegacyCandidateDiscovery{Nominations: []BASLegacyNomination{{
			Key: BASLegacyCandidateKey{Kind: BASLegacyCandidateDocument, OrganisationID: organisationID, SourceID: sourceID},
		}}}
		capture := &PurchaseCoverageReportingCapture{OrganisationID: organisationID}
		if withParent {
			line := &tammyv1.JournalLine{Id: "opaque-retained-line", JournalId: journalID}
			capture.Journals = []*accounting.JournalReportingRecord{{
				Journal: &tammyv1.Journal{Id: journalID, OrganisationId: organisationID, Version: 1,
					State:       tammyv1.JournalState_JOURNAL_STATE_POSTED,
					PostingDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 15}, Lines: []*tammyv1.JournalLine{line}},
				SourceType: "PAYABLE", SourceKind: 6, SourceID: sourceID, SourceRevision: 1,
				State: "POSTED", ContentHash: make([]byte, 32),
			}}
		}
		cash := accounting.BASLegacyCashRead{Summary: accounting.BASLegacyCashSummary{OrganisationID: organisationID}}
		return discovery, capture, cash, map[string]PurchaseReportingRetainedRow{factID: factRow}
	}
	for _, test := range []struct {
		name       string
		withParent bool
		withLine   bool
	}{
		{name: "missing parent", withLine: true},
		{name: "unmapped selected line", withParent: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			discovery, capture, cash, rows := newCase(test.withParent, test.withLine)
			if got, err := basLegacyCapturedCashProofs(organisationID, discovery, cash, capture, nil, rows); err == nil || got != nil {
				t.Fatalf("invalid selected fact link returned a partial proof: proofs=%v err=%v", got, err)
			}
		})
	}
}
