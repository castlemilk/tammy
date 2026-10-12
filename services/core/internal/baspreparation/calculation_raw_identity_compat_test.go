//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestCalculationRepositoryPreservesSubstantialSchema13CanonicalCaptureAcrossUpgrade14(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	size := buildSubstantialCanonicalOnlyUpgradeCapture(t, value)
	if size < 3*1024*1024 || size >= calculationSnapshotLimit-32*1024 {
		t.Fatalf("schema-13 capture must approach, but leave room below, the unchanged 4 MiB calculation cap: size=%d cap=%d",
			size, calculationSnapshotLimit)
	}

	repository := newCalculationRepositoryForTest(t, tx)
	ref, err := repository.Append(context.Background(), value, inputsRef)
	if err != nil || ref == nil {
		t.Fatalf("append substantial public canonical-only schema-13 capture: ref=%v err=%v", ref, err)
	}
	expected := proto.Clone(value).(*tammyv1.BasCalculation)
	expected.ContentHash = bytes.Clone(ref.ContentHash)
	if got, err := repository.GetExact(context.Background(), organisationID, ref); err != nil || !proto.Equal(got, expected) {
		t.Fatalf("read complete canonical-only capture before upgrade: value=%v err=%v", got, err)
	}
	assertLegacyCalculationEmptyCoverageMarkers(t, tx, ref.Id)
	var storedHash []byte
	if err := tx.QueryRowContext(context.Background(), `SELECT content_hash FROM bas_calculations_v1 WHERE calculation_id=? AND version=?`,
		ref.Id, int64(ref.Revision)).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedHash, ref.ContentHash) {
		t.Fatal("schema-13 retained calculation hash differs from its exact reference")
	}
	commitBASRepositoryTx(t, tx)
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := sqlcipher.MigrateWorkspace(context.Background(), workspace.Path, workspace.Key, 14); err != nil {
		t.Fatalf("upgrade the encrypted schema-13 capture to schema 14: %v", err)
	}
	reopened, err := sqlcipher.Open(context.Background(), workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repository = newCalculationRepositoryForTest(t, reopened)
	got, err := repository.GetExact(context.Background(), organisationID, ref)
	if err != nil || !proto.Equal(got, expected) || !bytes.Equal(got.ContentHash, storedHash) {
		t.Fatalf("schema-14 reopen changed the historical canonical calculation body or hash: value=%v err=%v", got, err)
	}
	assertLegacyCalculationEmptyCoverageMarkers(t, reopened, ref.Id)
	var rawRows int
	if err := reopened.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_calculation_raw_coverage_v1 WHERE calculation_id=?`, ref.Id).Scan(&rawRows); err != nil {
		t.Fatal(err)
	}
	if rawRows != 0 {
		t.Fatalf("schema-14 migration synthesized raw identities for canonical-only history: rows=%d", rawRows)
	}
}

func TestCalculationRepositoryKeepsLegacyCanonicalReadBoundAtSixteenMiB(t *testing.T) {
	schemas := []struct {
		name         string
		newWorkspace func(*testing.T) (*testkit.EncryptedWorkspace, string)
	}{
		{
			name: "schema13",
			newWorkspace: func(t *testing.T) (*testkit.EncryptedWorkspace, string) {
				workspace, organisationID, _ := newBASRepositoryWorkspace(t)
				return workspace, organisationID
			},
		},
		{name: "schema14_merged_reader", newWorkspace: newBASRawIdentityRepositoryWorkspace},
	}
	frames := []struct {
		name             string
		lineageBytes     int
		evidenceBytes    int
		coverageBytes    int
		wantBoundFailure bool
	}{
		{name: "above_new_8_mib_but_within_legacy_16_mib", lineageBytes: 1 << 20, evidenceBytes: 1 << 20, coverageBytes: 1 << 20},
		{name: "above_legacy_16_mib", lineageBytes: 2 << 20, evidenceBytes: 2 << 20, coverageBytes: 1 << 20, wantBoundFailure: true},
	}
	for _, schema := range schemas {
		for _, frame := range frames {
			schema, frame := schema, frame
			t.Run(schema.name+"/"+frame.name, func(t *testing.T) {
				workspace, organisationID := schema.newWorkspace(t)
				tx := beginBASRepositoryTx(t, workspace)
				value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
				value.Coverage = legacyBoundProbeCoverage()
				repository := newCalculationRepositoryForTest(t, tx)
				ref, err := repository.Append(context.Background(), value, inputsRef)
				if err != nil || ref == nil {
					t.Fatalf("append canonical coverage before bound probe: ref=%v err=%v", ref, err)
				}
				if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_coverage_v1_no_update`); err != nil {
					t.Fatalf("temporarily remove update guard for retained-frame bound fixture: %v", err)
				}
				query := `UPDATE bas_calculation_coverage_v1
					SET lineage_proto=zeroblob(?),evidence_proto=zeroblob(?),coverage_proto=zeroblob(?)
					WHERE calculation_id=? AND calculation_version=?`
				if _, err := tx.ExecContext(context.Background(), query, frame.lineageBytes, frame.evidenceBytes,
					frame.coverageBytes, ref.Id, int64(ref.Revision)); err != nil {
					t.Fatalf("inject explicitly corrupted retained coverage frames: %v", err)
				}
				got, err := repository.GetExact(context.Background(), organisationID, ref)
				if got != nil {
					t.Fatalf("corrupt canonical-only frame read returned a partial calculation: %v", got)
				}
				if frame.wantBoundFailure {
					if !errors.Is(err, ErrSnapshotBound) {
						t.Fatalf("aggregate above the historical 16 MiB bound should fail as a bound, got %v", err)
					}
					return
				}
				if !errors.Is(err, ErrSnapshotCorrupt) || errors.Is(err, ErrSnapshotBound) {
					t.Fatalf("12 MiB explicitly corrupted canonical frames should pass the 16 MiB preflight and reach row verification, got %v", err)
				}
			})
		}
	}
}

func buildSubstantialCanonicalOnlyUpgradeCapture(t *testing.T, value *tammyv1.BasCalculation) int {
	t.Helper()
	const candidateCount = 1000
	const referencesPerList = 20
	const targetBytes = calculationSnapshotLimit - 64*1024
	coverage := make([]*tammyv1.BasSourceCoverage, candidateCount)
	for index := 0; index < candidateCount; index++ {
		candidate := &tammyv1.BasSourceCoverage{
			Source:             valueSourceRef("manual_tax_fact", uint64(90000+index)),
			CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
			Disposition:        tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
			ClassificationCode: "LEGACY_UNSUPPORTED",
			Reason:             strings.Repeat("r", 512),
		}
		if index%100 != 0 {
			candidate.Lineage = make([]*tammyv1.SourceRef, referencesPerList)
			candidate.Evidence = make([]*tammyv1.SourceRef, referencesPerList)
			for referenceIndex := 0; referenceIndex < referencesPerList; referenceIndex++ {
				base := uint64(1000000 + index*referencesPerList*2 + referenceIndex*2)
				candidate.Lineage[referenceIndex] = valueSourceRef("journal", base)
				candidate.Evidence[referenceIndex] = valueSourceRef("document", base+1)
			}
		}
		coverage[index] = candidate
	}
	value.Coverage = coverage
	size := proto.Size(value)
	for size > targetBytes {
		removedReference := false
		for _, candidate := range coverage {
			if len(candidate.Lineage) == 0 {
				continue
			}
			candidate.Lineage = candidate.Lineage[:len(candidate.Lineage)-1]
			candidate.Evidence = candidate.Evidence[:len(candidate.Evidence)-1]
			removedReference = true
		}
		if !removedReference {
			t.Fatalf("canonical-only schema-13 fixture cannot be fit under parent cap; body size=%d", size)
		}
		size = proto.Size(value)
	}
	return size
}

func assertLegacyCalculationEmptyCoverageMarkers(t *testing.T, executor app.CommandSQLExecutor, calculationID string) {
	t.Helper()
	var lineage, evidence []byte
	rows, err := executor.QueryContext(context.Background(), `SELECT lineage_proto,evidence_proto FROM bas_calculation_coverage_v1
		WHERE calculation_id=? AND calculation_version=1 AND sequence=1`, calculationID)
	if err != nil {
		t.Fatalf("read schema-13 empty lineage/evidence markers: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("schema-13 empty marker row missing: %v", rows.Err())
	}
	if err := rows.Scan(&lineage, &evidence); err != nil {
		t.Fatalf("scan schema-13 empty lineage/evidence markers: %v", err)
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatalf("schema-13 empty marker query returned extra rows or an error: %v", rows.Err())
	}
	wantLineage := []byte{0x32, 0x00}
	wantEvidence := []byte{0x3a, 0x00}
	if !bytes.Equal(lineage, wantLineage) || !bytes.Equal(evidence, wantEvidence) {
		t.Fatal("canonical-only migration changed the exact legacy empty lineage/evidence markers")
	}
}

func legacyBoundProbeCoverage() []*tammyv1.BasSourceCoverage {
	coverage := make([]*tammyv1.BasSourceCoverage, 4)
	for index := range coverage {
		candidate := &tammyv1.BasSourceCoverage{
			Source:             valueSourceRef("manual_tax_fact", uint64(98000+index)),
			CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
			Disposition:        tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
			ClassificationCode: "LEGACY_UNSUPPORTED", Reason: "Retained canonical bound probe",
		}
		if index != 0 {
			candidate.Lineage = []*tammyv1.SourceRef{valueSourceRef("journal", uint64(98100+index))}
			candidate.Evidence = []*tammyv1.SourceRef{valueSourceRef("document", uint64(98200+index))}
		}
		coverage[index] = candidate
	}
	return coverage
}
