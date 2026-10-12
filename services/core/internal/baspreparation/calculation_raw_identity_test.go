//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestCalculationRepositoryRoundTripsOrderedMixedRawCoverage(t *testing.T) {
	workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()

	value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	canonicalFirst := proto.Clone(value.Coverage[0]).(*tammyv1.BasSourceCoverage)
	canonicalLast := proto.Clone(value.Coverage[2]).(*tammyv1.BasSourceCoverage)
	canonicalLineage := valueSourceRef("invoice", 10031)
	canonicalEvidence := valueSourceRef("document", 10032)
	zeroRevision := int64(0)
	originalHash := bytes.Repeat([]byte{0x43}, 32)
	computedFactProof := bytes.Repeat([]byte{0x71}, 32)
	computedJournalProof := bytes.Repeat([]byte{0x72}, 32)
	longJournalID := "legacy-journal/" + string(bytes.Repeat([]byte("j"), 320))
	if len(longJournalID) <= 256 {
		t.Fatal("legacy journal ID fixture must exceed the former 256-byte limit")
	}

	value.Coverage = []*tammyv1.BasSourceCoverage{
		canonicalFirst,
		{
			RawSource: &tammyv1.BasRawSourceRef{
				Type: "tax_facts", Id: "legacy-tax-fact/" + string(bytes.Repeat([]byte("f"), 300)),
				ComputedProofHash: computedFactProof,
				ProofScope:        tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION,
			},
			CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
			Disposition:        tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
			ClassificationCode: "LEGACY_UNSUPPORTED",
			Reason:             "Retained manual fact has no canonical source hash",
			Lineage:            []*tammyv1.SourceRef{canonicalLineage},
			Evidence:           []*tammyv1.SourceRef{canonicalEvidence},
			LineageIdentities: []*tammyv1.BasSourceIdentity{
				{CanonicalSource: canonicalLineage},
				{RawSource: &tammyv1.BasRawSourceRef{
					Type: "journals", Id: longJournalID, OriginalRevision: &zeroRevision,
					OriginalContentHash: bytes.Clone(originalHash), ComputedProofHash: computedJournalProof,
					ProofScope: tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
				}},
			},
			EvidenceIdentities: []*tammyv1.BasSourceIdentity{
				{CanonicalSource: canonicalEvidence},
				{RawSource: &tammyv1.BasRawSourceRef{
					Type: "documents", Id: "legacy-document-opaque-id",
					ComputedProofHash: bytes.Repeat([]byte{0x73}, 32),
					ProofScope:        tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
				}},
			},
		},
		canonicalLast,
	}
	want := proto.Clone(value).(*tammyv1.BasCalculation)
	repository := newCalculationRepositoryForTest(t, tx)
	ref, err := repository.Append(context.Background(), value, inputsRef)
	if err != nil || ref == nil {
		t.Fatalf("append mixed canonical/raw calculation coverage: ref=%v err=%v", ref, err)
	}

	retained, err := repository.GetExact(context.Background(), organisationID, ref)
	if err != nil || retained == nil {
		t.Fatalf("exact mixed-coverage read: value=%v err=%v", retained, err)
	}
	want.ContentHash = bytes.Clone(ref.ContentHash)
	if !proto.Equal(retained, want) {
		t.Fatal("exact read did not preserve the complete ordered canonical/raw calculation body")
	}
	rawCoverage := retained.Coverage[1]
	if rawCoverage.Source != nil || rawCoverage.RawSource == nil || rawCoverage.RawSource.Type != "tax_facts" ||
		rawCoverage.RawSource.Id != value.Coverage[1].RawSource.Id || rawCoverage.RawSource.OriginalRevision != nil ||
		rawCoverage.RawSource.OriginalContentHash != nil || !bytes.Equal(rawCoverage.RawSource.ComputedProofHash, computedFactProof) {
		t.Fatal("raw tax-fact row identity or SQL-NULL source metadata changed during round trip")
	}
	var rawType, rawID, proofScope string
	var sourceRevision sql.NullInt64
	var sourceHash, storedProof []byte
	if err := tx.QueryRowContext(context.Background(), `SELECT raw_source_type,raw_source_id,original_revision,
		original_content_hash,computed_proof_hash,proof_scope FROM bas_calculation_raw_coverage_v1
		WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, ref.Id).
		Scan(&rawType, &rawID, &sourceRevision, &sourceHash, &storedProof, &proofScope); err != nil {
		t.Fatal(err)
	}
	if rawType != "tax_facts" || rawID != rawCoverage.RawSource.Id || sourceRevision.Valid || sourceHash != nil ||
		!bytes.Equal(storedProof, computedFactProof) || proofScope != "SOURCE_ROW_PROJECTION" {
		t.Fatalf("raw table changed nullable source/proof cells: type=%q idBytes=%d revision=%#v hash=%x scope=%q",
			rawType, len(rawID), sourceRevision, sourceHash, proofScope)
	}
	rawJournal := rawCoverage.LineageIdentities[1].RawSource
	if rawJournal == nil || rawJournal.Id != longJournalID || rawJournal.OriginalRevision == nil || *rawJournal.OriginalRevision != 0 ||
		!bytes.Equal(rawJournal.OriginalContentHash, originalHash) || bytes.Equal(rawJournal.ComputedProofHash, originalHash) {
		t.Fatal("underlying journal identity did not preserve its opaque ID, present zero/hash, and separate computed proof")
	}
	if len(rawCoverage.Lineage) != 1 || !sameBASSourceRef(rawCoverage.Lineage[0], canonicalLineage) ||
		len(rawCoverage.Evidence) != 1 || !sameBASSourceRef(rawCoverage.Evidence[0], canonicalEvidence) ||
		len(rawCoverage.LineageIdentities) != 2 || len(rawCoverage.EvidenceIdentities) != 2 ||
		rawCoverage.LineageIdentities[0].CanonicalSource == nil || rawCoverage.LineageIdentities[1].RawSource == nil ||
		rawCoverage.EvidenceIdentities[0].CanonicalSource == nil || rawCoverage.EvidenceIdentities[1].RawSource == nil {
		t.Fatal("mixed identity lists or their ordered canonical compatibility projections changed")
	}

	assertBASRawCoverageSequences(t, tx, organisationID, ref.Id, "bas_calculation_coverage_v1", []int64{1, 3})
	assertBASRawCoverageSequences(t, tx, organisationID, ref.Id, "bas_calculation_raw_coverage_v1", []int64{2})
}

func newBASRawIdentityRepositoryWorkspace(t *testing.T) (*testkit.EncryptedWorkspace, string) {
	t.Helper()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 14)
	organisationID := testUUID(10101)
	seedBASRepositoryOrganisation(t, workspace, organisationID)
	for _, actor := range []string{testUUID(36), testUUID(72)} {
		if _, err := workspace.Database.ExecContext(context.Background(), `INSERT INTO users(id,email,display_name,status,created_at,updated_at)
			VALUES(?,?,?,'ACTIVE',?,?)`, actor, actor+"@example.invalid", "Raw identity test actor", "2026-10-12T00:00:00Z", "2026-10-12T00:00:00Z"); err != nil {
			t.Fatalf("seed raw identity test actor: %v", err)
		}
	}
	return workspace, organisationID
}

func assertBASRawCoverageSequences(t *testing.T, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, organisationID, calculationID, table string, want []int64) {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), "SELECT sequence FROM "+table+" WHERE organisation_id=? AND calculation_id=? AND calculation_version=1 ORDER BY sequence",
		organisationID, calculationID)
	if err != nil {
		t.Fatalf("read %s shared sequence placement: %v", table, err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var sequence int64
		if err := rows.Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		got = append(got, sequence)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s sequence count = %v, want %v", table, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s sequences = %v, want %v", table, got, want)
		}
	}
}

func TestCalculationRepositoryRejectsMalformedRawCoverageBeforeAnyWrite(t *testing.T) {
	for _, kind := range []string{
		"neither_source_variant", "both_source_variants", "unknown_proof_scope", "canonical_projection_mismatch",
		"nested_identity_neither_source_variant", "nested_identity_both_source_variants",
		"blank_raw_type", "non_snake_raw_type", "blank_raw_id", "negative_original_revision",
		"short_original_hash", "short_computed_proof_hash",
	} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
			candidate := rawIdentityRepositoryCoverage(22001, true)
			switch kind {
			case "neither_source_variant":
				candidate.RawSource = nil
			case "both_source_variants":
				candidate.Source = valueSourceRef("tax_fact", 22002)
			case "unknown_proof_scope":
				candidate.RawSource.ProofScope = tammyv1.BasSourceProofScope(99)
			case "canonical_projection_mismatch":
				candidate.Lineage[0].ContentHash[0] ^= 0xff
			case "nested_identity_neither_source_variant":
				candidate.LineageIdentities[0] = &tammyv1.BasSourceIdentity{}
			case "nested_identity_both_source_variants":
				candidate.LineageIdentities[0].RawSource = &tammyv1.BasRawSourceRef{
					Type: "journals", Id: "opaque-lineage-id",
					ComputedProofHash: bytes.Repeat([]byte{0x44}, 32),
					ProofScope:        tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
				}
			case "blank_raw_type":
				candidate.RawSource.Type = "  "
			case "non_snake_raw_type":
				candidate.RawSource.Type = "TaxFacts"
			case "blank_raw_id":
				candidate.RawSource.Id = "\t "
			case "negative_original_revision":
				negativeRevision := int64(-1)
				candidate.RawSource.OriginalRevision = &negativeRevision
			case "short_original_hash":
				candidate.RawSource.OriginalContentHash = []byte{1, 2}
			case "short_computed_proof_hash":
				candidate.RawSource.ComputedProofHash = []byte{1, 2}
			}
			value.Coverage = []*tammyv1.BasSourceCoverage{candidate}
			ref, err := newCalculationRepositoryForTest(t, tx).Append(context.Background(), value, inputsRef)
			if ref != nil || err == nil {
				t.Fatalf("invalid %s raw coverage was retained: ref=%v err=%v", kind, ref, err)
			}
			for _, table := range []string{"bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1"} {
				assertBASRepositoryCount(t, tx, table, 0)
			}
		})
	}
}

func TestCalculationRepositoryAcceptsTwentyAndRejectsTwentyOneMixedLineageAndEvidenceIdentities(t *testing.T) {
	for _, dimension := range []string{"lineage", "evidence"} {
		for _, count := range []int{20, 21} {
			t.Run(fmt.Sprintf("%s_%d", dimension, count), func(t *testing.T) {
				workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
				tx := beginBASRepositoryTx(t, workspace)
				defer tx.Rollback()
				value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
				candidate := rawIdentityRepositoryCoverage(25001, true)
				canonicalCount := count / 2
				canonicalType, rawType := "journal", "journals"
				if dimension == "evidence" {
					canonicalType, rawType = "document", "documents"
				}
				canonicalRefs, identities := mixedRawIdentityReferences(canonicalType, rawType, canonicalCount, count-canonicalCount)
				if dimension == "lineage" {
					candidate.Lineage = canonicalRefs
					candidate.LineageIdentities = identities
				} else {
					candidate.Evidence = canonicalRefs
					candidate.EvidenceIdentities = identities
				}
				value.Coverage = []*tammyv1.BasSourceCoverage{candidate}
				repository := newCalculationRepositoryForTest(t, tx)
				ref, err := repository.Append(context.Background(), value, inputsRef)
				if count == 20 {
					if err != nil || ref == nil {
						t.Fatalf("exactly 20 mixed %s identities should be accepted: ref=%v err=%v", dimension, ref, err)
					}
					retained, err := repository.GetExact(context.Background(), organisationID, ref)
					if err != nil || retained == nil || !proto.Equal(retained.Coverage[0], candidate) {
						t.Fatalf("exact read did not preserve all 20 ordered %s identities: value=%v err=%v", dimension, retained, err)
					}
					return
				}
				if ref != nil || !errors.Is(err, ErrSnapshotBound) {
					t.Fatalf("21 mixed %s identities must fail with the typed bound before writes: ref=%v err=%v", dimension, ref, err)
				}
				for _, table := range []string{"bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1"} {
					assertBASRepositoryCount(t, tx, table, 0)
				}
			})
		}
	}
}

func mixedRawIdentityReferences(canonicalType, rawType string, canonicalCount, rawCount int) ([]*tammyv1.SourceRef, []*tammyv1.BasSourceIdentity) {
	canonicalRefs := make([]*tammyv1.SourceRef, 0, canonicalCount)
	identities := make([]*tammyv1.BasSourceIdentity, 0, canonicalCount+rawCount)
	for index := 0; index < canonicalCount; index++ {
		ref := valueSourceRef(canonicalType, uint64(70000+index))
		canonicalRefs = append(canonicalRefs, ref)
		identities = append(identities, &tammyv1.BasSourceIdentity{CanonicalSource: proto.Clone(ref).(*tammyv1.SourceRef)})
	}
	for index := 0; index < rawCount; index++ {
		identities = append(identities, &tammyv1.BasSourceIdentity{RawSource: &tammyv1.BasRawSourceRef{
			Type: rawType, Id: fmt.Sprintf("opaque-%s-%03d", rawType, index),
			ComputedProofHash: bytes.Repeat([]byte{byte(index + 1)}, 32),
			ProofScope:        tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
		}})
	}
	return canonicalRefs, identities
}

func TestCalculationRepositoryRejectsRawCoverageSQLPresenceAndScalarCorruption(t *testing.T) {
	for _, corruption := range []struct {
		name  string
		query string
	}{
		{
			name:  "normalized_reason",
			query: `UPDATE bas_calculation_raw_coverage_v1 SET reason=reason||' changed' WHERE calculation_id=? AND calculation_version=1 AND sequence=1`,
		},
		{
			name:  "nullable_revision_and_hash_presence",
			query: `UPDATE bas_calculation_raw_coverage_v1 SET original_revision=0,original_content_hash=? WHERE calculation_id=? AND calculation_version=1 AND sequence=1`,
		},
	} {
		t.Run(corruption.name, func(t *testing.T) {
			workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
			value.Coverage = []*tammyv1.BasSourceCoverage{rawIdentityRepositoryCoverage(26001, true)}
			repository := newCalculationRepositoryForTest(t, tx)
			ref, err := repository.Append(context.Background(), value, inputsRef)
			if err != nil || ref == nil {
				t.Fatalf("append raw identity before integrity mutation: ref=%v err=%v", ref, err)
			}
			if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_raw_coverage_v1_no_update`); err != nil {
				t.Fatalf("temporarily remove raw-row update guard for corruption fixture: %v", err)
			}
			var updateErr error
			if corruption.name == "nullable_revision_and_hash_presence" {
				_, updateErr = tx.ExecContext(context.Background(), corruption.query, bytes.Repeat([]byte{0x55}, 32), ref.Id)
			} else {
				_, updateErr = tx.ExecContext(context.Background(), corruption.query, ref.Id)
			}
			if updateErr != nil {
				t.Fatalf("inject retained raw row corruption: %v", updateErr)
			}
			retained, err := repository.GetExact(context.Background(), organisationID, ref)
			if retained != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt raw SQL row must fail closed without a partial calculation: value=%v err=%v", retained, err)
			}
		})
	}
}

func TestCalculationRepositoryUsesOneCombinedCoverageCountAndSequence(t *testing.T) {
	for _, count := range []int{1000, 1001} {
		t.Run(fmt.Sprintf("candidates_%d", count), func(t *testing.T) {
			workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
			value.Coverage = make([]*tammyv1.BasSourceCoverage, count)
			canonicalSequences := make([]int64, 0, (count+1)/2)
			rawSequences := make([]int64, 0, count/2)
			for index := 0; index < count; index++ {
				isRaw := index%2 == 1
				value.Coverage[index] = rawIdentityRepositoryCoverage(uint64(23000+index), isRaw)
				if isRaw {
					rawSequences = append(rawSequences, int64(index+1))
				} else {
					canonicalSequences = append(canonicalSequences, int64(index+1))
				}
			}
			ref, err := newCalculationRepositoryForTest(t, tx).Append(context.Background(), value, inputsRef)
			if count == 1000 {
				if err != nil || ref == nil {
					t.Fatalf("the shared 1,000-row canonical/raw coverage limit should be accepted: ref=%v err=%v", ref, err)
				}
				retained, err := newCalculationRepositoryForTest(t, tx).GetExact(context.Background(), organisationID, ref)
				if err != nil || retained == nil || len(retained.Coverage) != count {
					t.Fatalf("complete 1,000-row mixed coverage read: rows=%d err=%v", len(retained.GetCoverage()), err)
				}
				assertBASRawCoverageSequences(t, tx, organisationID, ref.Id, "bas_calculation_coverage_v1", canonicalSequences)
				assertBASRawCoverageSequences(t, tx, organisationID, ref.Id, "bas_calculation_raw_coverage_v1", rawSequences)
				return
			}
			if ref != nil || !errors.Is(err, ErrSnapshotBound) {
				t.Fatalf("combined 1,001-row coverage must fail with the typed snapshot bound before writes: ref=%v err=%v", ref, err)
			}
			for _, table := range []string{"bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1"} {
				assertBASRepositoryCount(t, tx, table, 0)
			}
		})
	}
}

func TestCalculationRepositoryRejectsSharedRawCoveragePayloadBeforeAnyWrite(t *testing.T) {
	workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	const rowCount = 1000
	const rawIDBytes = 3600
	value.Coverage = make([]*tammyv1.BasSourceCoverage, rowCount)
	for index := 0; index < rowCount; index++ {
		candidate := rawIdentityRepositoryCoverage(uint64(60000+index), true)
		prefix := fmt.Sprintf("legacy-fact-%04d-", index)
		candidate.RawSource.Id = prefix + strings.Repeat("x", rawIDBytes-len(prefix))
		value.Coverage[index] = candidate
	}
	if size := proto.Size(value); size >= calculationSnapshotLimit {
		t.Fatalf("shared-payload fixture must fit the unchanged %d-byte calculation body cap, got %d", calculationSnapshotLimit, size)
	}
	for index, candidate := range value.Coverage {
		if size := proto.Size(candidate); size >= 1<<20 {
			t.Fatalf("candidate %d must fit the existing 1 MiB per-row frame cap, got %d", index, size)
		}
	}

	ref, err := newCalculationRepositoryForTest(t, tx).Append(context.Background(), value, inputsRef)
	if ref != nil || !errors.Is(err, ErrSnapshotBound) {
		t.Fatalf("shared 8 MiB mixed/raw capture budget should reject this parent-valid capture before writes: ref=%v err=%v", ref, err)
	}
	for _, table := range []string{"bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1"} {
		assertBASRepositoryCount(t, tx, table, 0)
	}
}

func TestCalculationRepositoryBoundsOpaqueRawIDByExistingCoverageFrame(t *testing.T) {
	workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	candidate := rawIdentityRepositoryCoverage(61001, true)
	// The schema's raw ID cell permits up to 1 MiB. This ID fits that cell but
	// makes its enclosing coverage protobuf exceed the existing 1 MiB row-frame
	// limit, so the repository must report a bound instead of inventing a short
	// canonical-ID restriction.
	candidate.RawSource.Id = strings.Repeat("x", 1<<20)
	value.Coverage = []*tammyv1.BasSourceCoverage{candidate}
	if size := proto.Size(value); size >= calculationSnapshotLimit {
		t.Fatalf("single raw-ID boundary fixture must fit the unchanged calculation body cap, got %d", size)
	}
	ref, err := newCalculationRepositoryForTest(t, tx).Append(context.Background(), value, inputsRef)
	if ref != nil || !errors.Is(err, ErrSnapshotBound) {
		t.Fatalf("raw ID that exceeds its existing bounded coverage frame must fail with ErrSnapshotBound: ref=%v err=%v", ref, err)
	}
	for _, table := range []string{"bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1"} {
		assertBASRepositoryCount(t, tx, table, 0)
	}
}

func TestCalculationRepositoryMixedRawCancellationRollsBackOnlyItsSavepoint(t *testing.T) {
	workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	value.Coverage = []*tammyv1.BasSourceCoverage{
		proto.Clone(value.Coverage[0]).(*tammyv1.BasSourceCoverage),
		rawIdentityRepositoryCoverage(24001, true),
	}
	marker := writeCallerWorkBeforeRepositoryAppend(t, tx, organisationID, "raw identity calculation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := &cancelAfterRepositoryParentInsertExecutor{
		CommandSQLExecutor: tx,
		parentTable:        "bas_calculations_v1",
		cancel:             cancel,
	}
	repository := newCalculationRepositoryForTest(t, executor)
	ref, err := repository.Append(ctx, value, inputsRef)
	if ref != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after the parent insert must fail without a partial mixed capture: ref=%v err=%v", ref, err)
	}
	if !executor.parentInserted {
		t.Fatal("test did not cancel after the calculation parent row was inserted")
	}
	assertBoundedUncancelledSavepointCleanup(t, executor)
	for _, table := range []string{"bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1"} {
		assertBASRepositoryCount(t, tx, table, 0)
	}
	assertCallerWorkCanCommit(t, workspace.Database, tx, organisationID, marker, "raw identity calculation")
}

func rawIdentityRepositoryCoverage(index uint64, raw bool) *tammyv1.BasSourceCoverage {
	lineage := valueSourceRef("journal", 30000+index)
	evidence := valueSourceRef("document", 40000+index)
	coverage := &tammyv1.BasSourceCoverage{
		CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
		Disposition:        tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
		ClassificationCode: "LEGACY_UNSUPPORTED",
		Reason:             "Retained candidate needs source review",
		Lineage:            []*tammyv1.SourceRef{lineage},
		Evidence:           []*tammyv1.SourceRef{evidence},
	}
	if raw {
		coverage.RawSource = &tammyv1.BasRawSourceRef{
			Type: "tax_facts", Id: fmt.Sprintf("legacy-fact-%d", index),
			ComputedProofHash: bytes.Repeat([]byte{byte(index)}, 32),
			ProofScope:        tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION,
		}
		coverage.LineageIdentities = []*tammyv1.BasSourceIdentity{
			{CanonicalSource: proto.Clone(lineage).(*tammyv1.SourceRef)},
		}
		coverage.EvidenceIdentities = []*tammyv1.BasSourceIdentity{
			{CanonicalSource: proto.Clone(evidence).(*tammyv1.SourceRef)},
		}
	} else {
		coverage.Source = valueSourceRef("manual_tax_fact", 50000+index)
	}
	return coverage
}
