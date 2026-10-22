package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

func TestBASCoverageTaxFactProofHashCommitsEveryCanonicalCell(t *testing.T) {
	base := basSharedTestAccountingFact("fact-a", "line-a", &tammyv1.Journal{
		Id: basSharedTestJournal, OrganisationId: basSharedTestOrganisation,
	})
	want, err := basCoverageTaxFactProofHash(base)
	if err != nil || len(want) != sha256.Size {
		t.Fatalf("baseline hash=%x err=%v", want, err)
	}
	tests := []struct {
		name   string
		mutate func(*accounting.TaxFactReportingRecord)
	}{
		{"id", func(v *accounting.TaxFactReportingRecord) { v.ID += "-changed" }},
		{"organisation", func(v *accounting.TaxFactReportingRecord) { v.OrganisationID += "-changed" }},
		{"journal-line", func(v *accounting.TaxFactReportingRecord) { v.JournalLineID += "-changed" }},
		{"tax-code", func(v *accounting.TaxFactReportingRecord) { v.TaxCode += "-changed" }},
		{"treatment", func(v *accounting.TaxFactReportingRecord) { v.Treatment += "-changed" }},
		{"original-gross", func(v *accounting.TaxFactReportingRecord) { v.OriginalGrossMinor++ }},
		{"original-net", func(v *accounting.TaxFactReportingRecord) { v.OriginalNetMinor++ }},
		{"original-gst", func(v *accounting.TaxFactReportingRecord) { v.OriginalGSTMinor++ }},
		{"attributed-gross", func(v *accounting.TaxFactReportingRecord) { v.AttributedGrossMinor++ }},
		{"attributed-net", func(v *accounting.TaxFactReportingRecord) { v.AttributedNetMinor++ }},
		{"attributed-gst", func(v *accounting.TaxFactReportingRecord) { v.AttributedGSTMinor++ }},
		{"remaining-gross", func(v *accounting.TaxFactReportingRecord) { v.RemainingGrossMinor++ }},
		{"remaining-net", func(v *accounting.TaxFactReportingRecord) { v.RemainingNetMinor++ }},
		{"remaining-gst", func(v *accounting.TaxFactReportingRecord) { v.RemainingGSTMinor++ }},
		{"rule-type", func(v *accounting.TaxFactReportingRecord) { v.Rule.Type += "-changed" }},
		{"rule-id", func(v *accounting.TaxFactReportingRecord) { v.Rule.Id += "-changed" }},
		{"rule-revision", func(v *accounting.TaxFactReportingRecord) { v.Rule.Revision++ }},
		{"rule-hash", func(v *accounting.TaxFactReportingRecord) { v.Rule.ContentHash[0] ^= 0xff }},
		{"source-type", func(v *accounting.TaxFactReportingRecord) { v.SourceType += "-changed" }},
		{"source-id", func(v *accounting.TaxFactReportingRecord) { v.SourceID += "-changed" }},
		{"source-revision", func(v *accounting.TaxFactReportingRecord) { v.SourceRevision++ }},
		{"created-at", func(v *accounting.TaxFactReportingRecord) { v.CreatedAt += "-changed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := basSharedCloneFact(base)
			test.mutate(&changed)
			got, err := basCoverageTaxFactProofHash(changed)
			if err != nil || bytes.Equal(got, want) {
				t.Fatalf("changed canonical cell did not change proof: %x err=%v", got, err)
			}
		})
	}
	t.Run("nil rule is distinct", func(t *testing.T) {
		changed := basSharedCloneFact(base)
		changed.Rule = nil
		got, err := basCoverageTaxFactProofHash(changed)
		if err != nil || bytes.Equal(got, want) {
			t.Fatalf("nil rule hash=%x err=%v", got, err)
		}
	})
	t.Run("null and present source hash are distinct", func(t *testing.T) {
		changed := basSharedCloneFact(base)
		changed.SourceContentHashPresent = true
		changed.SourceContentHash = bytes.Repeat([]byte{0x5a}, 32)
		got, err := basCoverageTaxFactProofHash(changed)
		if err != nil || bytes.Equal(got, want) {
			t.Fatalf("present source hash was collapsed into NULL: %x err=%v", got, err)
		}
	})
	t.Run("present hash must be exactly 32 bytes", func(t *testing.T) {
		changed := basSharedCloneFact(base)
		changed.SourceContentHashPresent = true
		changed.SourceContentHash = []byte{}
		if got, err := basCoverageTaxFactProofHash(changed); err == nil || got != nil {
			t.Fatalf("zero-length BLOB accepted: %x err=%v", got, err)
		}
	})
}

func TestBASCoverageRetainedRowHashPreservesColumnOrderAndStorageKinds(t *testing.T) {
	fact := basSharedTestAccountingFact("fact-a", "line-a", &tammyv1.Journal{Id: basSharedTestJournal, OrganisationId: basSharedTestOrganisation})
	base := basSharedTestRawFactRow(fact)
	want, err := basCoverageRowProofHash(base)
	if err != nil || len(want) != sha256.Size {
		t.Fatalf("baseline hash=%x err=%v", want, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*purchases.PurchaseReportingRetainedRow)
	}{
		{"ordered-column-position", func(v *purchases.PurchaseReportingRetainedRow) {
			v.Columns[0], v.Columns[1] = v.Columns[1], v.Columns[0]
			v.Values[0], v.Values[1] = v.Values[1], v.Values[0]
		}},
		{"null-versus-empty-text", func(v *purchases.PurchaseReportingRetainedRow) {
			v.Values[basSharedFactSourceHash] = basSharedTestText("")
		}},
		{"integer-versus-text", func(v *purchases.PurchaseReportingRetainedRow) {
			v.Values[basSharedFactOriginalGross] = basSharedTestText(fmt.Sprint(v.Values[basSharedFactOriginalGross].Integer))
		}},
		{"blob-bytes", func(v *purchases.PurchaseReportingRetainedRow) { v.Values[basSharedFactRuleHash].Bytes[0] ^= 0xff }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := basSharedCloneRow(base)
			test.mutate(&changed)
			got, err := basCoverageRowProofHash(changed)
			if err != nil || bytes.Equal(got, want) {
				t.Fatalf("raw projection change did not change row proof: %x err=%v", got, err)
			}
		})
	}
	bad := basSharedCloneRow(base)
	bad.Columns[1] = bad.Columns[0]
	if got, err := basCoverageRowProofHash(bad); err == nil || got != nil {
		t.Fatalf("duplicate raw column accepted: %x err=%v", got, err)
	}
	bad = basSharedCloneRow(base)
	bad.Values[basSharedFactOriginalGross].Bytes = []byte("inconsistent-union")
	if got, err := basCoverageRowProofHash(bad); err == nil || got != nil {
		t.Fatalf("invalid raw storage-kind union accepted: %x err=%v", got, err)
	}
}

func TestBASCoverageFindingAggregationRetainsEverySourceWithoutOversizedFinding(t *testing.T) {
	input := make([]BASSourceFindingTemplate, 0, 45)
	for index := 0; index < 45; index++ {
		id := fmt.Sprintf("opaque-fact-%02d", index)
		digest := sha256.Sum256([]byte(id))
		identity, err := basCoverageRawIdentity("tax_facts", id, nil, nil, digest[:],
			tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		input = append(input, basCoverageFinding(
			tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED,
			"MANUAL_TAX_FACT_UNSUPPORTED", "Retained manual tax facts require review.",
			[]*tammyv1.BasSourceIdentity{identity}))
	}
	first, err := basCoverageAggregateFindings(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := basCoverageAggregateFindings(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 || len(first) > 200 {
		t.Fatalf("aggregate produced %d finding templates; want three <=20-source groups", len(first))
	}
	seen := make(map[string]bool, 45)
	for index, finding := range first {
		if len(finding.SourceIdentities) == 0 || len(finding.SourceIdentities) > basSourceIdentityMaximum ||
			finding.StableCode != "MANUAL_TAX_FACT_UNSUPPORTED" || finding.Severity != tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED {
			t.Fatalf("invalid group %d: %#v", index, finding)
		}
		for _, identity := range finding.SourceIdentities {
			if identity.RawSource == nil || seen[identity.RawSource.Id] {
				t.Fatalf("missing/duplicate raw source identity: %#v", identity)
			}
			seen[identity.RawSource.Id] = true
		}
		if len(finding.SourceIdentities) != len(second[index].SourceIdentities) {
			t.Fatal("finding partition was nondeterministic")
		}
		for sourceIndex := range finding.SourceIdentities {
			if !proto.Equal(finding.SourceIdentities[sourceIndex], second[index].SourceIdentities[sourceIndex]) {
				t.Fatal("finding source order was nondeterministic")
			}
		}
	}
	if len(seen) != 45 {
		t.Fatalf("aggregate retained %d/45 source identities", len(seen))
	}
}
