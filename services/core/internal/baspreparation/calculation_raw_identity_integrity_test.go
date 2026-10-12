//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestCalculationRepositoryFailsClosedOnMergedRawCoverageCorruption(t *testing.T) {
	for _, kind := range []string{
		"sequence_gap", "duplicate_cross_table_sequence", "missing_raw_row", "extra_raw_row",
		"wrong_owner", "wrong_parent_hash", "malformed_coverage_wire", "changed_coverage_hash",
		"altered_proof_scope", "raw_canonical_projection_mismatch",
	} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()

			value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
			canonical := proto.Clone(value.Coverage[0]).(*tammyv1.BasSourceCoverage)
			raw := rawIdentityRepositoryCoverage(81001, true)
			if kind == "raw_canonical_projection_mismatch" {
				raw.LineageIdentities = append(raw.LineageIdentities, &tammyv1.BasSourceIdentity{RawSource: &tammyv1.BasRawSourceRef{
					Type: "journals", Id: "opaque-linked-journal",
					ComputedProofHash: bytes.Repeat([]byte{0x61}, 32),
					ProofScope:        tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
				}})
			}
			value.Coverage = []*tammyv1.BasSourceCoverage{canonical, raw}
			repository := newCalculationRepositoryForTest(t, tx)
			exact, err := repository.Append(context.Background(), value, inputsRef)
			if err != nil || exact == nil {
				t.Fatalf("append valid canonical/raw capture before corruption: ref=%v err=%v", exact, err)
			}

			if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_raw_coverage_v1_no_update`); err != nil {
				t.Fatalf("temporarily remove raw-row update guard: %v", err)
			}
			switch kind {
			case "sequence_gap":
				_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET sequence=3
					WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, exact.Id)
			case "duplicate_cross_table_sequence":
				_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET sequence=1
					WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, exact.Id)
			case "missing_raw_row":
				if _, dropErr := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_raw_coverage_v1_no_delete`); dropErr != nil {
					t.Fatalf("temporarily remove raw-row delete guard: %v", dropErr)
				}
				_, err = tx.ExecContext(context.Background(), `DELETE FROM bas_calculation_raw_coverage_v1
					WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, exact.Id)
			case "extra_raw_row":
				err = insertSupplementalRawCoverageClone(t, tx, exact.Id, 3, "unused-extra-row-id")
			case "wrong_owner":
				if _, err = tx.ExecContext(context.Background(), `PRAGMA defer_foreign_keys=ON`); err == nil {
					_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET organisation_id=?
						WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, testUUID(10102), exact.Id)
				}
			case "wrong_parent_hash":
				if _, err = tx.ExecContext(context.Background(), `PRAGMA defer_foreign_keys=ON`); err == nil {
					_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET calculation_hash=?
						WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, bytes.Repeat([]byte{0x69}, 32), exact.Id)
				}
			case "malformed_coverage_wire":
				_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET coverage_proto=X'01'
					WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, exact.Id)
			case "changed_coverage_hash":
				_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET content_hash=?
					WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, bytes.Repeat([]byte{0x6a}, 32), exact.Id)
			case "altered_proof_scope":
				_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET proof_scope='COMPLETE_JOURNAL'
					WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, exact.Id)
			case "raw_canonical_projection_mismatch":
				projection, marshalErr := (proto.MarshalOptions{Deterministic: true}).Marshal(&tammyv1.BasSourceCoverage{
					Lineage: []*tammyv1.SourceRef{valueSourceRef("journal", 81002)},
				})
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET lineage_proto=?
					WHERE calculation_id=? AND calculation_version=1 AND sequence=2`, projection, exact.Id)
			}
			if err != nil {
				t.Fatalf("inject %s raw coverage corruption: %v", kind, err)
			}

			retained, err := repository.GetExact(context.Background(), organisationID, exact)
			if retained != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("%s corruption must return no partial calculation and a typed corruption error: value=%v err=%v", kind, retained, err)
			}
		})
	}
}

func TestCalculationRepositoryPreservesRawOriginalMetadataPresenceAndProofScopes(t *testing.T) {
	zeroRevision := int64(0)
	cases := []struct {
		name              string
		scope             tammyv1.BasSourceProofScope
		wantScope         string
		originalRevision  *int64
		originalHash      []byte
		computedProofHash []byte
	}{
		{
			name:              "source_row_projection_without_original_metadata",
			scope:             tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION,
			wantScope:         "SOURCE_ROW_PROJECTION",
			computedProofHash: bytes.Repeat([]byte{0x51}, 32),
		},
		{
			name:              "complete_journal_present_zero_revision_and_original_hash",
			scope:             tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_COMPLETE_JOURNAL,
			wantScope:         "COMPLETE_JOURNAL",
			originalRevision:  &zeroRevision,
			originalHash:      bytes.Repeat([]byte{0x52}, 32),
			computedProofHash: bytes.Repeat([]byte{0x53}, 32),
		},
		{
			name:              "reference_projection_zero_revision_without_original_hash",
			scope:             tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
			wantScope:         "REFERENCE_ONLY_PROJECTION",
			originalRevision:  &zeroRevision,
			computedProofHash: bytes.Repeat([]byte{0x57}, 32),
		},
		{
			name:              "source_row_projection_hash_without_original_revision",
			scope:             tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION,
			wantScope:         "SOURCE_ROW_PROJECTION",
			originalHash:      bytes.Repeat([]byte{0x58}, 32),
			computedProofHash: bytes.Repeat([]byte{0x59}, 32),
		},
		{
			name:              "reference_projection_allows_equal_separate_hash_fields",
			scope:             tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION,
			wantScope:         "REFERENCE_ONLY_PROJECTION",
			originalRevision:  &zeroRevision,
			originalHash:      bytes.Repeat([]byte{0x54}, 32),
			computedProofHash: bytes.Repeat([]byte{0x54}, 32),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
			candidate := rawIdentityRepositoryCoverage(80501, true)
			candidate.RawSource.OriginalRevision = testCase.originalRevision
			candidate.RawSource.OriginalContentHash = bytes.Clone(testCase.originalHash)
			candidate.RawSource.ComputedProofHash = bytes.Clone(testCase.computedProofHash)
			candidate.RawSource.ProofScope = testCase.scope
			value.Coverage = []*tammyv1.BasSourceCoverage{candidate}

			repository := newCalculationRepositoryForTest(t, tx)
			ref, err := repository.Append(context.Background(), value, inputsRef)
			if err != nil || ref == nil {
				t.Fatalf("append raw source metadata case: ref=%v err=%v", ref, err)
			}
			retained, err := repository.GetExact(context.Background(), organisationID, ref)
			if err != nil || retained == nil || !proto.Equal(retained.Coverage[0], candidate) {
				t.Fatalf("exact read did not preserve raw metadata and proof scope: retained=%v err=%v", retained, err)
			}

			var storedRevision sql.NullInt64
			var storedOriginalHash, storedComputedHash []byte
			var storedScope string
			if err := tx.QueryRowContext(context.Background(), `SELECT original_revision,original_content_hash,
				computed_proof_hash,proof_scope FROM bas_calculation_raw_coverage_v1
				WHERE calculation_id=? AND calculation_version=1 AND sequence=1`, ref.Id).
				Scan(&storedRevision, &storedOriginalHash, &storedComputedHash, &storedScope); err != nil {
				t.Fatal(err)
			}
			if testCase.originalRevision == nil {
				if storedRevision.Valid {
					t.Fatalf("absent source revision must remain SQL NULL: revision=%#v", storedRevision)
				}
			} else if !storedRevision.Valid || storedRevision.Int64 != 0 {
				t.Fatalf("present zero source revision must remain distinct from SQL NULL: revision=%#v", storedRevision)
			}
			if testCase.originalHash == nil {
				if storedOriginalHash != nil {
					t.Fatalf("absent original hash must remain SQL NULL: originalHash=%x", storedOriginalHash)
				}
			} else if !bytes.Equal(storedOriginalHash, testCase.originalHash) {
				t.Fatalf("present original hash changed: originalHash=%x", storedOriginalHash)
			}
			if !bytes.Equal(storedComputedHash, testCase.computedProofHash) || storedScope != testCase.wantScope {
				t.Fatalf("computed proof or scope changed: computed=%x scope=%q", storedComputedHash, storedScope)
			}
		})
	}
}

const supplementalRawCoverageColumns = `calculation_id,organisation_id,family_id,calculation_version,calculation_hash,sequence,
	candidate_kind,disposition,raw_source_type,raw_source_id,original_revision,original_content_hash,computed_proof_hash,proof_scope,
	eligible_amount_minor,currency_code,tax_date,classification_code,reason,lineage_proto,evidence_proto,coverage_proto,content_hash`

func insertSupplementalRawCoverageClone(t *testing.T, tx app.CommandSQLExecutor, calculationID string, sequence int64, rawID string) error {
	t.Helper()
	_, err := tx.ExecContext(context.Background(), `INSERT INTO bas_calculation_raw_coverage_v1(`+supplementalRawCoverageColumns+`)
		SELECT calculation_id,organisation_id,family_id,calculation_version,calculation_hash,?,candidate_kind,disposition,
			raw_source_type,?,original_revision,original_content_hash,computed_proof_hash,proof_scope,eligible_amount_minor,
			currency_code,tax_date,classification_code,reason,lineage_proto,evidence_proto,coverage_proto,content_hash
		FROM bas_calculation_raw_coverage_v1 WHERE calculation_id=? AND calculation_version=1 AND sequence=2`,
		sequence, rawID, calculationID)
	return err
}

func TestCalculationRepositoryPreflightsMergedCoverageBeforeHydration(t *testing.T) {
	for _, kind := range []string{"combined_count", "combined_payload_bytes"} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
			value.Coverage = []*tammyv1.BasSourceCoverage{
				proto.Clone(value.Coverage[0]).(*tammyv1.BasSourceCoverage),
				rawIdentityRepositoryCoverage(82001, true),
			}
			exact, err := newCalculationRepositoryForTest(t, tx).Append(context.Background(), value, inputsRef)
			if err != nil || exact == nil {
				t.Fatalf("append small mixed capture before adding retained-only corruption: ref=%v err=%v", exact, err)
			}

			if kind == "combined_count" {
				_, err = tx.ExecContext(context.Background(), `WITH RECURSIVE extra(sequence) AS (
					SELECT 3 UNION ALL SELECT sequence+1 FROM extra WHERE sequence<1002
				) INSERT INTO bas_calculation_raw_coverage_v1(`+supplementalRawCoverageColumns+`)
					SELECT source.calculation_id,source.organisation_id,source.family_id,source.calculation_version,source.calculation_hash,
						extra.sequence,source.candidate_kind,source.disposition,source.raw_source_type,source.raw_source_id,
						source.original_revision,source.original_content_hash,source.computed_proof_hash,source.proof_scope,
						source.eligible_amount_minor,source.currency_code,source.tax_date,source.classification_code,source.reason,
						source.lineage_proto,source.evidence_proto,source.coverage_proto,source.content_hash
					FROM extra CROSS JOIN bas_calculation_raw_coverage_v1 source
					WHERE source.calculation_id=? AND source.calculation_version=1 AND source.sequence=2`, exact.Id)
			} else {
				if _, err = tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_coverage_v1_no_update`); err == nil {
					_, err = tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculation_raw_coverage_v1_no_update`)
				}
				if err == nil {
					_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_coverage_v1 SET
						lineage_proto=zeroblob(2097152),evidence_proto=zeroblob(2097152),coverage_proto=zeroblob(1048576)
						WHERE calculation_id=? AND calculation_version=1`, exact.Id)
				}
				if err == nil {
					_, err = tx.ExecContext(context.Background(), `UPDATE bas_calculation_raw_coverage_v1 SET
						lineage_proto=zeroblob(2097152),evidence_proto=zeroblob(2097152),coverage_proto=zeroblob(1048576)
						WHERE calculation_id=? AND calculation_version=1`, exact.Id)
				}
			}
			if err != nil {
				t.Fatalf("add valid, bounded raw SQL rows that exceed the %s preflight: %v", kind, err)
			}

			probe := &supplementalCoverageHydrationProbe{CommandSQLExecutor: tx}
			retained, err := newCalculationRepositoryForTest(t, probe).GetExact(context.Background(), organisationID, exact)
			if retained != nil || !errors.Is(err, ErrSnapshotBound) {
				t.Fatalf("over-bound %s capture must fail with no partial result: value=%v err=%v", kind, retained, err)
			}
			if probe.aggregateQueries == 0 || probe.detailQueries != 0 {
				t.Fatalf("%s limit must be rejected by aggregate preflight before child-row hydration: aggregates=%d detail=%d", kind, probe.aggregateQueries, probe.detailQueries)
			}
		})
	}
}

type supplementalCoverageHydrationProbe struct {
	app.CommandSQLExecutor
	aggregateQueries int
	detailQueries    int
}

func (probe *supplementalCoverageHydrationProbe) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	lower := strings.ToLower(query)
	childQuery := strings.Contains(lower, "from bas_calculation_coverage_v1") ||
		strings.Contains(lower, "from bas_calculation_raw_coverage_v1")
	if childQuery {
		if strings.Contains(lower, "count(") || strings.Contains(lower, "sum(") {
			probe.aggregateQueries++
		} else {
			probe.detailQueries++
			return nil, errors.New("coverage child hydration occurred before bounds passed")
		}
	}
	return probe.CommandSQLExecutor.QueryContext(ctx, query, arguments...)
}

func TestCalculationRepositoryAcceptsExactSharedRawCoverageByteBudget(t *testing.T) {
	workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	value.Coverage = supplementalBoundaryCoverage(128, 30_000, 0)
	_, payload := supplementalFindBoundaryExtra(t, value)
	gap := int64(calculationCoverageIdentityPayloadLimit) - payload
	if gap < 0 || gap > 8 {
		t.Fatalf("boundary fixture should be within eight single-varint bytes of the limit, payload=%d gap=%d", payload, gap)
	}
	for index := int64(0); index < gap; index++ {
		value.Coverage[index].EligibleAmount.MinorUnits = 128
	}
	if got := supplementalRawCoveragePayloadBytes(t, value); got != calculationCoverageIdentityPayloadLimit {
		t.Fatalf("independent retained-cell charge = %d, want exact 8 MiB", got)
	}
	supplementalAssertCalculationFitsParentAndRowFrames(t, value)

	ref, err := newCalculationRepositoryForTest(t, tx).Append(context.Background(), value, inputsRef)
	if err != nil || ref == nil {
		t.Fatalf("the exact shared 8 MiB raw-identity budget should append: ref=%v err=%v", ref, err)
	}
	retained, err := newCalculationRepositoryForTest(t, tx).GetExact(context.Background(), organisationID, ref)
	if err != nil || retained == nil || len(retained.Coverage) != len(value.Coverage) {
		t.Fatalf("the exact 8 MiB capture should be fully readable: rows=%d err=%v", len(retained.GetCoverage()), err)
	}
	assertBASRepositoryCount(t, tx, "bas_calculation_coverage_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_calculation_raw_coverage_v1", 127)
}

func TestCalculationRepositoryRejectsOneByteOverSharedRawCoverageBudgetWithoutLosingCallerWork(t *testing.T) {
	workspace, organisationID := newBASRawIdentityRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	value.Coverage = supplementalBoundaryCoverage(128, 30_000, 0)
	_, payload := supplementalFindBoundaryExtra(t, value)
	gap := int64(calculationCoverageIdentityPayloadLimit) - payload
	if gap < 0 || gap > 8 {
		t.Fatalf("boundary fixture should be within eight single-varint bytes of the limit, payload=%d gap=%d", payload, gap)
	}
	for index := int64(0); index < gap+1; index++ {
		value.Coverage[index].EligibleAmount.MinorUnits = 128
	}
	if got := supplementalRawCoveragePayloadBytes(t, value); got != calculationCoverageIdentityPayloadLimit+1 {
		t.Fatalf("independent retained-cell charge = %d, want exactly one byte over 8 MiB", got)
	}
	supplementalAssertCalculationFitsParentAndRowFrames(t, value)
	marker := writeCallerWorkBeforeRepositoryAppend(t, tx, organisationID, "one-byte-over raw coverage")
	ref, err := newCalculationRepositoryForTest(t, tx).Append(context.Background(), value, inputsRef)
	if ref != nil || !errors.Is(err, ErrSnapshotBound) {
		t.Fatalf("one byte over the shared 8 MiB budget must fail with ErrSnapshotBound before writes: ref=%v err=%v", ref, err)
	}
	for _, table := range []string{"bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1"} {
		assertBASRepositoryCount(t, tx, table, 0)
	}
	assertCallerWorkCanCommit(t, workspace.Database, tx, organisationID, marker, "one-byte-over raw coverage")
}

func supplementalBoundaryCoverage(count, baseIDBytes, extraIDBytes int) []*tammyv1.BasSourceCoverage {
	coverage := make([]*tammyv1.BasSourceCoverage, count)
	for index := range coverage {
		isRaw := index != count-1
		candidate := rawIdentityRepositoryCoverage(uint64(83000+index), isRaw)
		if isRaw {
			idBytes := baseIDBytes
			if index == 0 {
				idBytes += extraIDBytes
			}
			candidate.RawSource.Id = fmt.Sprintf("raw-boundary-界-%03d-%s", index, strings.Repeat("x", idBytes))
		}
		candidate.EligibleAmount = &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 127}
		coverage[index] = candidate
	}
	return coverage
}

func supplementalFindBoundaryExtra(t *testing.T, value *tammyv1.BasCalculation) (int, int64) {
	t.Helper()
	low, high := 0, 750_000
	bestExtra := -1
	var bestPayload int64
	for low <= high {
		middle := low + (high-low)/2
		value.Coverage = supplementalBoundaryCoverage(128, 30_000, middle)
		payload := supplementalRawCoveragePayloadBytes(t, value)
		if payload <= calculationCoverageIdentityPayloadLimit {
			bestExtra, bestPayload = middle, payload
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	if bestExtra < 0 {
		t.Fatal("could not construct a parent-valid raw-coverage fixture under the shared limit")
	}
	value.Coverage = supplementalBoundaryCoverage(128, 30_000, bestExtra)
	return bestExtra, bestPayload
}

// supplementalRawCoveragePayloadBytes independently totals the retained SQL
// cells from the public calculation, using UTF-8 byte lengths for TEXT, eight
// bytes for present INTEGER cells, actual deterministic protobuf frame bytes,
// and the fixed per-row allowance. It intentionally does not call the
// repository's charge helper.
func supplementalRawCoveragePayloadBytes(t *testing.T, value *tammyv1.BasCalculation) int64 {
	t.Helper()
	var total int64
	for _, candidate := range value.Coverage {
		raw := candidate.RawSource
		lineage, err := supplementalCoverageReferenceFrame(candidate.Lineage, candidate.LineageIdentities, true)
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := supplementalCoverageReferenceFrame(candidate.Evidence, candidate.EvidenceIdentities, false)
		if err != nil {
			t.Fatal(err)
		}
		coverageWire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		candidateKind := strings.TrimPrefix(candidate.CandidateKind.String(), "BAS_COVERAGE_CANDIDATE_KIND_")
		disposition := strings.TrimPrefix(candidate.Disposition.String(), "BAS_COVERAGE_DISPOSITION_")
		rowBytes := int64(512 + len(value.CalculationId) + len(value.OrganisationId) + len(value.Family.Id) + 8 + 32 + 8 +
			len(candidateKind) + len(disposition) + len(candidate.ClassificationCode) + len(candidate.Reason) +
			len(lineage) + len(evidence) + len(coverageWire) + 32)
		if raw != nil {
			proofScope := strings.TrimPrefix(raw.ProofScope.String(), "BAS_SOURCE_PROOF_SCOPE_")
			rowBytes += int64(len(raw.Type) + len(raw.Id) + len(raw.ComputedProofHash) + len(proofScope))
			if raw.OriginalRevision != nil {
				rowBytes += 8
			}
			rowBytes += int64(len(raw.OriginalContentHash))
		} else {
			if candidate.Source == nil {
				t.Fatal("canonical boundary fixture is missing its source reference")
			}
			rowBytes += int64(len(candidate.Source.Type) + len(candidate.Source.Id) + 8 + len(candidate.Source.ContentHash))
		}
		if candidate.EligibleAmount != nil {
			rowBytes += 8 + int64(len(candidate.EligibleAmount.CurrencyCode))
		}
		if candidate.TaxDate != nil {
			rowBytes += int64(len(fmt.Sprintf("%04d-%02d-%02d", candidate.TaxDate.Year,
				candidate.TaxDate.Month, candidate.TaxDate.Day)))
		}
		total += rowBytes
	}
	return total
}

func supplementalCoverageReferenceFrame(references []*tammyv1.SourceRef, identities []*tammyv1.BasSourceIdentity,
	lineage bool) ([]byte, error) {
	frame := &tammyv1.BasSourceCoverage{}
	if len(identities) > 0 {
		cloned := make([]*tammyv1.BasSourceIdentity, len(identities))
		for index, identity := range identities {
			cloned[index] = proto.Clone(identity).(*tammyv1.BasSourceIdentity)
		}
		if lineage {
			frame.LineageIdentities = cloned
		} else {
			frame.EvidenceIdentities = cloned
		}
	} else {
		cloned := make([]*tammyv1.SourceRef, 0, len(references))
		for _, reference := range references {
			cloned = append(cloned, proto.Clone(reference).(*tammyv1.SourceRef))
		}
		if len(cloned) == 0 {
			cloned = []*tammyv1.SourceRef{{}}
		}
		if lineage {
			frame.Lineage = cloned
		} else {
			frame.Evidence = cloned
		}
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(frame)
}

func supplementalAssertCalculationFitsParentAndRowFrames(t *testing.T, value *tammyv1.BasCalculation) {
	t.Helper()
	unsigned := proto.Clone(value).(*tammyv1.BasCalculation)
	unsigned.ContentHash = nil
	unsignedWire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(unsignedWire)
	sealed := proto.Clone(unsigned).(*tammyv1.BasCalculation)
	sealed.ContentHash = bytes.Clone(digest[:])
	sealedWire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealedWire) >= calculationSnapshotLimit {
		t.Fatalf("boundary fixture parent body must fit the 4 MiB snapshot cap, got %d bytes", len(sealedWire))
	}
	for index, candidate := range value.Coverage {
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(candidate)
		if err != nil || len(wire) >= 1<<20 {
			t.Fatalf("boundary fixture coverage row %d must fit its 1 MiB frame: bytes=%d err=%v", index, len(wire), err)
		}
	}
}

var _ app.CommandSQLExecutor = (*supplementalCoverageHydrationProbe)(nil)
