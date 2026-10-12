//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func TestValidationRepositoryRoundTripsOrderedMixedRawFindingIdentities(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	fixture := seedValidationRepositoryFixture(t, tx, organisationID)
	fixture.validation.Findings[0] = rawIdentityRepositoryFinding()
	// An empty new identity list keeps the old canonical representation valid.
	fixture.validation.Findings[1].Sources = []*tammyv1.SourceRef{valueSourceRef("document", 83003)}
	repository := newValidationRepositoryForTest(t, tx)
	firstBody := proto.Clone(fixture.validation).(*tammyv1.BasValidationSnapshot)

	firstRef, err := repository.Append(ctx, organisationID, fixture.validation, fixture.ownerRef)
	if err != nil || firstRef == nil {
		t.Fatalf("append mixed raw finding identities: ref=%v err=%v", firstRef, err)
	}
	expectedFirst := proto.Clone(firstBody).(*tammyv1.BasValidationSnapshot)
	expectedFirst.ContentHash = bytes.Clone(firstRef.ContentHash)
	if got, err := repository.GetExact(ctx, organisationID, firstRef); err != nil || !proto.Equal(got, expectedFirst) {
		t.Fatalf("exact mixed-source validation read: value=%v err=%v", got, err)
	}
	assertRetainedRawFindingIdentity(t, tx, firstRef, expectedFirst.Findings[0])

	secondBody := nextValidationRevision(firstBody)
	secondBody.Findings[0].SafeMessage = "Review the retained raw source before relying on this finding"
	secondRef, err := repository.Append(ctx, organisationID, secondBody, fixture.ownerRef)
	if err != nil || secondRef == nil || secondRef.Revision != 2 {
		t.Fatalf("append next mixed-source validation history: ref=%v err=%v", secondRef, err)
	}
	expectedSecond := proto.Clone(secondBody).(*tammyv1.BasValidationSnapshot)
	expectedSecond.ContentHash = bytes.Clone(secondRef.ContentHash)
	if got, err := repository.GetExact(ctx, organisationID, firstRef); err != nil || !proto.Equal(got, expectedFirst) {
		t.Fatalf("later revision changed the prior mixed finding identity: value=%v err=%v", got, err)
	}
	retained, err := repository.GetRetained(ctx, organisationID, secondRef)
	if err != nil || retained == nil || !proto.Equal(retained.Validation, expectedSecond) ||
		!sameBASSourceRef(retained.Preparation, fixture.ownerRef) || !sameBASSourceRef(retained.Calculation, fixture.calculationRef) {
		t.Fatalf("mixed finding identities lost their exact historical owners: retained=%v err=%v", retained, err)
	}

	commitBASRepositoryTx(t, tx)
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(context.Background(), workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repository = newValidationRepositoryForTest(t, reopened)
	for _, expected := range []*tammyv1.BasValidationSnapshot{expectedFirst, expectedSecond} {
		exact := &tammyv1.SourceRef{Type: "bas_validation", Id: expected.ValidationId,
			Revision: expected.Revision, ContentHash: bytes.Clone(expected.ContentHash)}
		got, err := repository.GetExact(ctx, organisationID, exact)
		if err != nil || !proto.Equal(got, expected) {
			t.Fatalf("mixed finding identities did not survive encrypted reopen at revision %d: value=%v err=%v", expected.Revision, got, err)
		}
	}
}

func assertRetainedRawFindingIdentity(t *testing.T, executor *sqlcipher.Transaction,
	ref *tammyv1.SourceRef, finding *tammyv1.BasValidationFinding) {
	t.Helper()
	var wire, findingHash []byte
	if err := executor.QueryRowContext(context.Background(), `SELECT finding_proto,finding_hash FROM bas_validation_findings_v1
		WHERE validation_id=? AND revision=? AND finding_id=?`, ref.Id, int64(ref.Revision), finding.FindingId).Scan(&wire, &findingHash); err != nil {
		t.Fatalf("read retained mixed finding body: %v", err)
	}
	var stored tammyv1.BasValidationFinding
	if err := proto.Unmarshal(wire, &stored); err != nil || !proto.Equal(&stored, finding) {
		t.Fatalf("finding row did not retain its exact ordered identity body: stored=%v err=%v", &stored, err)
	}
	if !bytes.Equal(findingHash, canonicalFindingHash(finding)) {
		t.Fatal("finding hash does not commit the complete mixed identity body")
	}
	if len(finding.SourceIdentities) != 4 || len(finding.Sources) != 2 {
		t.Fatalf("fixture does not exercise two canonical and two raw source identities: identities=%d canonical projection=%d",
			len(finding.SourceIdentities), len(finding.Sources))
	}
	fact := finding.SourceIdentities[1].RawSource
	journal := finding.SourceIdentities[3].RawSource
	if fact == nil || fact.OriginalRevision != nil || fact.OriginalContentHash != nil || len(fact.ComputedProofHash) != 32 {
		t.Fatal("raw tax-fact provenance must retain SQL NULL revision/hash and a separate computed proof")
	}
	if journal == nil || journal.OriginalRevision == nil || *journal.OriginalRevision != 0 || len(journal.OriginalContentHash) != 32 ||
		bytes.Equal(journal.OriginalContentHash, journal.ComputedProofHash) {
		t.Fatal("raw journal identity must preserve present revision zero/hash separately from its computed proof")
	}
}

func TestValidFindingValidatesSelectedMixedSourceIdentities(t *testing.T) {
	valid := rawIdentityRepositoryFinding()
	if !validFinding(valid) {
		t.Fatal("valid mixed canonical/raw finding identities were rejected")
	}
	legacy := proto.Clone(valid).(*tammyv1.BasValidationFinding)
	legacy.SourceIdentities = nil
	if !validFinding(legacy) {
		t.Fatal("legacy finding with only its canonical Sources list must remain valid")
	}

	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.BasValidationFinding)
	}{
		{
			name: "identity_without_either_variant",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.SourceIdentities[1] = &tammyv1.BasSourceIdentity{}
			},
		},
		{
			name: "identity_with_both_variants",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.SourceIdentities[0].RawSource = validRawFindingSource("journals", "both-kinds", 0x71)
			},
		},
		{
			name: "unknown_proof_scope",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.SourceIdentities[1].RawSource.ProofScope = tammyv1.BasSourceProofScope(99)
			},
		},
		{
			name: "blank_raw_type",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.SourceIdentities[1].RawSource.Type = "  "
			},
		},
		{
			name: "blank_raw_id",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.SourceIdentities[1].RawSource.Id = "\t "
			},
		},
		{
			name: "short_computed_proof_hash",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.SourceIdentities[1].RawSource.ComputedProofHash = []byte{1, 2}
			},
		},
		{
			name: "negative_original_revision",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				negative := int64(-1)
				finding.SourceIdentities[3].RawSource.OriginalRevision = &negative
			},
		},
		{
			name: "short_original_content_hash",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.SourceIdentities[3].RawSource.OriginalContentHash = []byte{1}
			},
		},
		{
			name: "canonical_projection_mismatch",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				finding.Sources[0].ContentHash[0] ^= 0xff
			},
		},
		{
			name: "twenty_one_selected_sources",
			mutate: func(finding *tammyv1.BasValidationFinding) {
				for index := 0; index < 17; index++ {
					finding.SourceIdentities = append(finding.SourceIdentities, &tammyv1.BasSourceIdentity{
						RawSource: validRawFindingSource("documents", fmt.Sprintf("too-many-%02d", index), byte(index+1)),
					})
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := proto.Clone(valid).(*tammyv1.BasValidationFinding)
			test.mutate(candidate)
			if validFinding(candidate) {
				t.Fatalf("malformed selected source identities were accepted: %s", test.name)
			}
		})
	}
}

func TestValidationRepositoryRejectsInvalidMixedFindingBeforeWritesAndPreservesCallerWork(t *testing.T) {
	for _, kind := range []string{
		"identity_without_variant", "identity_with_both_variants", "invalid_proof", "unknown_scope",
		"negative_original_revision", "projection_mismatch", "twenty_one_selected_sources",
	} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			fixture := seedValidationRepositoryFixture(t, tx, organisationID)
			candidate := proto.Clone(fixture.validation).(*tammyv1.BasValidationSnapshot)
			finding := rawIdentityRepositoryFinding()
			switch kind {
			case "identity_without_variant":
				finding.SourceIdentities[1] = &tammyv1.BasSourceIdentity{}
			case "identity_with_both_variants":
				finding.SourceIdentities[0].RawSource = validRawFindingSource("journals", "repository-both-kinds", 0x71)
			case "invalid_proof":
				finding.SourceIdentities[1].RawSource.ComputedProofHash = []byte{1}
			case "unknown_scope":
				finding.SourceIdentities[1].RawSource.ProofScope = tammyv1.BasSourceProofScope(99)
			case "negative_original_revision":
				negative := int64(-1)
				finding.SourceIdentities[3].RawSource.OriginalRevision = &negative
			case "projection_mismatch":
				finding.Sources[0].ContentHash[0] ^= 0xff
			case "twenty_one_selected_sources":
				for index := 0; index < 17; index++ {
					finding.SourceIdentities = append(finding.SourceIdentities, &tammyv1.BasSourceIdentity{
						RawSource: validRawFindingSource("documents", fmt.Sprintf("repository-too-many-%02d", index), byte(index+1)),
					})
				}
			}
			candidate.Findings[0] = finding
			marker := writeCallerWorkBeforeRepositoryAppend(t, tx, organisationID, "raw validation finding")
			ref, err := newValidationRepositoryForTest(t, tx).Append(context.Background(), organisationID, candidate, fixture.ownerRef)
			if ref != nil || err == nil {
				t.Fatalf("invalid %s source identity was retained: ref=%v err=%v", kind, ref, err)
			}
			assertBASRepositoryCount(t, tx, "bas_validations_v1", 0)
			assertBASRepositoryCount(t, tx, "bas_validation_findings_v1", 0)
			assertCallerWorkCanCommit(t, workspace.Database, tx, organisationID, marker, "raw validation finding")
		})
	}
}

func rawIdentityRepositoryFinding() *tammyv1.BasValidationFinding {
	canonicalInvoice := valueSourceRef("invoice", 83001)
	canonicalDocument := valueSourceRef("document", 83002)
	fact := validRawFindingSource("tax_facts", "legacy-tax-fact/no-retained-source-hash", 0x61)
	fact.ProofScope = tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION
	journal := validRawFindingSource("journals", "legacy-journal/"+string(bytes.Repeat([]byte("j"), 320)), 0x63)
	zeroRevision := int64(0)
	journal.OriginalRevision = &zeroRevision
	journal.OriginalContentHash = bytes.Repeat([]byte{0x62}, 32)
	return &tammyv1.BasValidationFinding{
		FindingId: testUUID(9932), Severity: tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING,
		StableCode: "SOURCE_REVIEW_REQUIRED", SafeMessage: "Review the exact retained source identity before relying on this finding",
		Sources: []*tammyv1.SourceRef{canonicalInvoice, canonicalDocument},
		SourceIdentities: []*tammyv1.BasSourceIdentity{
			{CanonicalSource: proto.Clone(canonicalInvoice).(*tammyv1.SourceRef)},
			{RawSource: fact},
			{CanonicalSource: proto.Clone(canonicalDocument).(*tammyv1.SourceRef)},
			{RawSource: journal},
		},
	}
}

func validRawFindingSource(sourceType, sourceID string, proofByte byte) *tammyv1.BasRawSourceRef {
	return &tammyv1.BasRawSourceRef{
		Type: sourceType, Id: sourceID, ComputedProofHash: bytes.Repeat([]byte{proofByte}, 32),
		ProofScope: tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
	}
}
