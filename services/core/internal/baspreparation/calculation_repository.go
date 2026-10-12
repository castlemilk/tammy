package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
)

var (
	ErrCalculationRepository = errors.New("baspreparation: calculation repository operation failed")
	ErrCalculationConflict   = errors.New("baspreparation: calculation conflicts with its retained capture owner")
)

const (
	calculationCoverageCountMaximum = 1000
	calculationCoverageFrameLimit   = 4 * calculationSnapshotLimit
)

// CalculationRepository retains immutable calculation snapshots and every
// bounded coverage candidate through a caller-owned transaction. Historical
// reads follow only exact references stored with the calculation.
type CalculationRepository struct {
	executor     app.CommandSQLExecutor
	cursors      *paging.Codec
	preparations *PreparationReader
	savedPayg    *SavedPaygRepository
}

func NewCalculationRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*CalculationRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrCalculationRepository
	}
	preparations, err := NewPreparationReader(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	savedPayg, err := NewSavedPaygRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	return &CalculationRepository{executor: executor, cursors: cursors, preparations: preparations, savedPayg: savedPayg}, nil
}

// Append writes one complete calculation and all of its coverage rows only
// after verifying its exact current preparation, historical source snapshots,
// and saved PAYG envelope. Transaction completion remains command-owned.
func (repository *CalculationRepository) Append(ctx context.Context, value *tammyv1.BasCalculation,
	savedInputs *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || value == nil {
		return nil, ErrCalculationRepository
	}
	if len(value.Coverage) > calculationCoverageCountMaximum {
		return nil, errors.Join(ErrCalculationRepository, ErrSnapshotBound)
	}
	for _, coverage := range value.Coverage {
		if coverage != nil && (len(coverage.Lineage) > basSourceIdentityMaximum || len(coverage.Evidence) > basSourceIdentityMaximum ||
			len(coverage.LineageIdentities) > basSourceIdentityMaximum || len(coverage.EvidenceIdentities) > basSourceIdentityMaximum) {
			return nil, errors.Join(ErrCalculationRepository, ErrSnapshotBound)
		}
	}
	wire, digest, err := sealCalculation(value)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	if !validBASSourceRef(value.Preparation, preparationSnapshotSpec.resource, "") ||
		!validBASSourceRef(savedInputs, savedPaygSnapshotSpec.resource, "") {
		return nil, ErrCalculationConflict
	}
	createdAt, ok := basTimestampSQLText(value.CreatedAt)
	if !ok {
		return nil, ErrCalculationRepository
	}
	coverage, usesNewCoverageIdentity, err := prepareCalculationCoverage(value.Coverage)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	if usesNewCoverageIdentity {
		payloadBytes, err := calculationCoveragePayloadBytes(coverage, value.CalculationId, value.OrganisationId, value.Family.Id, digest[:])
		if err != nil {
			return nil, errors.Join(ErrCalculationRepository, err)
		}
		if payloadBytes > calculationCoverageIdentityPayloadLimit {
			return nil, errors.Join(ErrCalculationRepository, ErrSnapshotBound)
		}
		available, err := calculationRawCoverageAvailable(ctx, repository.executor)
		if err != nil || !available {
			return nil, errors.Join(ErrCalculationRepository, ErrSnapshotCorrupt, err)
		}
	}

	owner, err := repository.preparations.GetCurrent(ctx, value.OrganisationId, value.Preparation.Id)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	if !sameBASSourceRef(value.Preparation, preparationReference(owner.Preparation)) {
		return nil, ErrCalculationConflict
	}
	if err := repository.verifyCapture(ctx, owner, value, savedInputs, ErrCalculationConflict); err != nil {
		if errors.Is(err, ErrCalculationConflict) {
			return nil, ErrCalculationConflict
		}
		return nil, errors.Join(ErrCalculationRepository, err)
	}

	resultWire, err := marshalCalculationFrame(value.Result, 2*1024*1024)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	workpaperWire, err := marshalCalculationFrame(value.WorkpaperSnapshot, calculationSnapshotLimit)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	revisionVectorWire, err := marshalCalculationFrame(value.RevisionVector, 2*1024*1024)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	organisationWire, err := marshalCalculationFrame(value.OrganisationSnapshot, 1024*1024)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	outcome, ok := basCalculationOutcomeSQL(value.Result.Outcome)
	if !ok {
		return nil, ErrCalculationRepository
	}
	version, _ := basSQLiteRevision(value.Version)
	preparationVersion, _ := basSQLiteRevision(value.Preparation.Revision)
	inputRevision, _ := basSQLiteRevision(value.InputRevision)
	profileVersion, _ := basSQLiteRevision(value.Profile.Revision)
	periodVersion, _ := basSQLiteRevision(value.Period.Revision)
	if err := repository.requireNextVersion(ctx, value, version); err != nil {
		return nil, err
	}

	inputContentHash := bytes.Clone(savedInputs.ContentHash)
	savepoint := "bas_calculation_append_" + strings.ReplaceAll(value.CalculationId, "-", "_")
	if _, err := repository.executor.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	rollback := func() error {
		return rollbackRepositorySavepoint(ctx, repository.executor, savepoint)
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_calculations_v1(
		calculation_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash,version,content_hash,
		input_snapshot_id,input_revision,input_hash,input_content_hash,profile_id,profile_version,profile_hash,period_id,period_version,period_hash,
		outcome,source_snapshot_hash,coverage_manifest_hash,revision_vector_hash,rule_version,rule_hash,calculator_version,calculator_hash,
		form_version,form_hash,schema_version,schema_hash,descriptor_version,descriptor_hash,organisation_snapshot_hash,
		result_proto,workpaper_snapshot_proto,revision_vector_proto,organisation_snapshot_proto,calculation_proto,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.CalculationId, value.OrganisationId, value.Family.Id, value.Preparation.Id, preparationVersion, value.Preparation.ContentHash,
		version, digest, savedInputs.Id, inputRevision, value.InputHash, inputContentHash,
		value.Profile.Id, profileVersion, value.Profile.ContentHash, value.Period.Id, periodVersion, value.Period.ContentHash,
		outcome, value.SourceSnapshotHash, value.CoverageManifestHash, value.RevisionVectorHash,
		value.RuleVersion, value.RuleHash, value.CalculatorVersion, value.CalculatorHash,
		value.FormVersion, value.FormHash, value.SchemaVersion, value.SchemaHash,
		value.DescriptorVersion, value.DescriptorHash, value.OrganisationSnapshotHash,
		resultWire, workpaperWire, revisionVectorWire, organisationWire, wire, createdAt); err != nil {
		return nil, errors.Join(ErrCalculationRepository, err, rollback())
	}
	for _, row := range coverage {
		var insertErr error
		if row.raw {
			var originalContentHash any
			if row.originalContentHash != nil {
				originalContentHash = row.originalContentHash
			}
			_, insertErr = repository.executor.ExecContext(ctx, `INSERT INTO bas_calculation_raw_coverage_v1(
				calculation_id,organisation_id,family_id,calculation_version,calculation_hash,sequence,candidate_kind,disposition,
				raw_source_type,raw_source_id,original_revision,original_content_hash,computed_proof_hash,proof_scope,
				eligible_amount_minor,currency_code,tax_date,classification_code,reason,lineage_proto,evidence_proto,coverage_proto,content_hash)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				value.CalculationId, value.OrganisationId, value.Family.Id, version, digest, row.sequence, row.candidateKind, row.disposition,
				row.rawSourceType, row.rawSourceID, row.originalRevision, originalContentHash, row.computedProofHash, row.proofScope,
				row.amountMinor, row.currencyCode, row.taxDate, row.classification, row.reason,
				row.lineageWire, row.evidenceWire, row.wire, row.hash)
		} else {
			_, insertErr = repository.executor.ExecContext(ctx, `INSERT INTO bas_calculation_coverage_v1(
				calculation_id,organisation_id,family_id,calculation_version,calculation_hash,sequence,candidate_kind,disposition,
				source_type,source_id,source_revision,source_hash,eligible_amount_minor,currency_code,tax_date,classification_code,reason,
				lineage_proto,evidence_proto,coverage_proto,content_hash)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				value.CalculationId, value.OrganisationId, value.Family.Id, version, digest, row.sequence, row.candidateKind, row.disposition,
				row.sourceType, row.sourceID, row.sourceRevision, row.sourceHash, row.amountMinor, row.currencyCode, row.taxDate,
				row.classification, row.reason, row.lineageWire, row.evidenceWire, row.wire, row.hash)
		}
		if insertErr != nil {
			return nil, errors.Join(ErrCalculationRepository, insertErr, rollback())
		}
	}
	if _, err := repository.executor.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return nil, errors.Join(ErrCalculationRepository, err, rollback())
	}
	return &tammyv1.SourceRef{Type: calculationSnapshotSpec.resource, Id: value.CalculationId,
		Revision: value.Version, ContentHash: bytes.Clone(digest[:])}, nil
}

// GetExact reads one immutable revision without consulting any current pointer.
func (repository *CalculationRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasCalculation, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) ||
		!validBASSourceRef(exact, calculationSnapshotSpec.resource, "") {
		return nil, ErrCalculationRepository
	}
	record, found, err := loadCalculationRecord(ctx, repository.executor, exact, organisationID)
	if err != nil || !found {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	value, err := openCalculationReadRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	if err := repository.verifyCoverageRows(ctx, record, value); err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	owner, err := repository.preparations.GetExact(ctx, organisationID, value.Preparation)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	if err := repository.verifyCapture(ctx, owner, value, &tammyv1.SourceRef{
		Type: savedPaygSnapshotSpec.resource, Id: record.inputSnapshotID,
		Revision: uint64(record.inputRevision), ContentHash: bytes.Clone(record.inputContentHash),
	}, ErrSnapshotCorrupt); err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	return value, nil
}

func (repository *CalculationRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && repository.cursors != nil &&
		repository.preparations != nil && repository.savedPayg != nil && ctx != nil && ctx.Err() == nil
}

func (repository *CalculationRepository) verifyCapture(ctx context.Context, owner *PreparationRetainedSnapshot,
	value *tammyv1.BasCalculation, savedInputs *tammyv1.SourceRef, mismatch error) error {
	if owner == nil || owner.Preparation == nil || value == nil ||
		!sameBASSourceRef(value.Preparation, preparationReference(owner.Preparation)) ||
		!sameBASSourceRef(value.Family, owner.Preparation.Family) ||
		!sameBASSourceRef(value.Profile, owner.Preparation.Profile) ||
		!sameBASSourceRef(value.Period, owner.Preparation.Period) ||
		!bytes.Equal(value.SourceSnapshotHash, owner.Preparation.SourceSnapshotHash) ||
		value.InputRevision != owner.Preparation.InputRevision || !bytes.Equal(value.InputHash, owner.Preparation.InputHash) ||
		owner.SavedPaygInputs == nil || !sameBASSourceRef(owner.SavedPaygInputs, savedInputs) {
		return mismatch
	}
	if value.OrganisationSnapshot == nil || value.OrganisationSnapshot.Id != value.OrganisationId ||
		!validBASSourceRef(value.OrganisationIdentity, "organisation", value.OrganisationSnapshot.Id) ||
		value.WorkpaperSnapshot == nil || value.CentWorkpaper == nil ||
		value.CentWorkpaper.Type != "bas_workpaper" || value.CentWorkpaper.Id != value.WorkpaperSnapshot.Id ||
		value.CentWorkpaper.Revision != value.WorkpaperSnapshot.Version ||
		value.WorkpaperSnapshot.OrganisationId != value.OrganisationId {
		return mismatch
	}
	profiles, err := NewProfileRepository(repository.executor, repository.cursors)
	if err != nil {
		return err
	}
	profile, err := profiles.GetExact(ctx, value.OrganisationId, value.Profile)
	if err != nil {
		return err
	}
	if !proto.Equal(profile, value.ProfileSnapshot) {
		return mismatch
	}
	periods, err := NewPeriodRepository(repository.executor, repository.cursors)
	if err != nil {
		return err
	}
	period, err := periods.GetExact(ctx, value.OrganisationId, value.Period)
	if err != nil {
		return err
	}
	if !proto.Equal(period, value.PeriodSnapshot) || !sameBASSourceRef(period.Confirmation.Profile, value.Profile) {
		return mismatch
	}
	saved, err := repository.savedPayg.GetExact(ctx, value.OrganisationId, savedInputs)
	if err != nil {
		return err
	}
	if saved.OrganisationId != value.OrganisationId || saved.PreparationId != value.Preparation.Id ||
		saved.Preparation.Revision > value.Preparation.Revision || saved.InputRevision != value.InputRevision ||
		!bytes.Equal(saved.InputHash, value.InputHash) || !proto.Equal(saved.PaygwInputs, value.PaygwInputs) ||
		!proto.Equal(saved.PaygiInputs, value.PaygiInputs) {
		return mismatch
	}
	inputHash, err := hashPaygInputs(value.PaygwInputs, value.PaygiInputs)
	if err != nil {
		return err
	}
	if !bytes.Equal(inputHash, value.InputHash) {
		return mismatch
	}
	return nil
}

func (repository *CalculationRepository) requireNextVersion(ctx context.Context, value *tammyv1.BasCalculation, version int64) error {
	rows, err := repository.executor.QueryContext(ctx, `SELECT COALESCE(MAX(version),0),COUNT(*),COALESCE(MIN(version),0),
		COALESCE(MIN(organisation_id),''),COALESCE(MAX(organisation_id),''),COALESCE(MIN(family_id),''),COALESCE(MAX(family_id),''),
		COALESCE(MIN(preparation_id),''),COALESCE(MAX(preparation_id),''),
		(SELECT content_hash FROM bas_calculations_v1 WHERE calculation_id=? ORDER BY version DESC LIMIT 1)
		FROM bas_calculations_v1 WHERE calculation_id=?`, value.CalculationId, value.CalculationId)
	if err != nil {
		return errors.Join(ErrCalculationRepository, err)
	}
	if !rows.Next() {
		return errors.Join(ErrCalculationRepository, rows.Err(), rows.Close())
	}
	var maximum, count, minimum int64
	var minimumOrganisation, maximumOrganisation, minimumFamily, maximumFamily string
	var minimumPreparationID, maximumPreparationID string
	var priorHash []byte
	if err := rows.Scan(&maximum, &count, &minimum, &minimumOrganisation, &maximumOrganisation,
		&minimumFamily, &maximumFamily, &minimumPreparationID, &maximumPreparationID, &priorHash); err != nil || rows.Next() || rows.Err() != nil {
		return errors.Join(ErrCalculationRepository, err, rows.Err(), rows.Close())
	}
	if err := rows.Close(); err != nil {
		return errors.Join(ErrCalculationRepository, err)
	}
	if maximum < 0 || count < 0 || minimum < 0 || count != maximum ||
		(count != 0 && minimum != 1) || maximum == math.MaxInt64 || version != maximum+1 {
		return ErrCalculationConflict
	}
	if count == 0 {
		return nil
	}
	if minimumOrganisation != value.OrganisationId || maximumOrganisation != value.OrganisationId ||
		minimumFamily != value.Family.Id || maximumFamily != value.Family.Id ||
		minimumPreparationID != value.Preparation.Id || maximumPreparationID != value.Preparation.Id {
		return ErrCalculationConflict
	}
	prior := &tammyv1.SourceRef{Type: calculationSnapshotSpec.resource, Id: value.CalculationId,
		Revision: uint64(maximum), ContentHash: bytes.Clone(priorHash)}
	if _, err := repository.GetExact(ctx, value.OrganisationId, prior); err != nil {
		return errors.Join(ErrCalculationRepository, err)
	}
	return nil
}

type calculationCoverageInsert struct {
	sequence, sourceRevision                         int64
	candidateKind, disposition, sourceType, sourceID string
	raw                                              bool
	rawSourceType, rawSourceID, proofScope           string
	originalRevision                                 any
	originalRevisionPresent                          bool
	originalContentHash, computedProofHash           []byte
	sourceHash                                       []byte
	amountMinor, currencyCode, taxDate               any
	classification, reason                           string
	lineageWire, evidenceWire, wire, hash            []byte
}

const calculationCoverageIdentityPayloadLimit int64 = 8 * 1024 * 1024

func prepareCalculationCoverage(values []*tammyv1.BasSourceCoverage) ([]calculationCoverageInsert, bool, error) {
	if len(values) > calculationCoverageCountMaximum {
		return nil, false, ErrSnapshotBound
	}
	rows := make([]calculationCoverageInsert, len(values))
	usesNewIdentity := false
	for index, value := range values {
		if value == nil || (value.Source == nil) == (value.RawSource == nil) {
			return nil, false, ErrSnapshotCorrupt
		}
		if value.RawSource != nil {
			if !validBASRawSourceRef(value.RawSource) {
				return nil, false, ErrSnapshotCorrupt
			}
			usesNewIdentity = true
		} else if value.Source.Type == "" || len(value.Source.Type) > 128 ||
			!validBASSourceRef(value.Source, value.Source.Type, "") {
			return nil, false, ErrSnapshotCorrupt
		}
		if len(value.LineageIdentities) > basSourceIdentityMaximum || len(value.EvidenceIdentities) > basSourceIdentityMaximum {
			return nil, false, ErrSnapshotBound
		}
		if !validBASIdentityProjection(value.LineageIdentities, value.Lineage) ||
			!validBASIdentityProjection(value.EvidenceIdentities, value.Evidence) {
			return nil, false, ErrSnapshotCorrupt
		}
		if len(value.LineageIdentities) > 0 || len(value.EvidenceIdentities) > 0 {
			usesNewIdentity = true
		}
		candidateKind, candidateOK := basCoverageCandidateKindSQL(value.CandidateKind)
		disposition, dispositionOK := basCoverageDispositionSQL(value.Disposition)
		if !candidateOK || !dispositionOK {
			return nil, false, ErrSnapshotCorrupt
		}
		var amountMinor, currencyCode, taxDate any
		if value.EligibleAmount != nil {
			amountMinor, currencyCode = value.EligibleAmount.MinorUnits, value.EligibleAmount.CurrencyCode
		}
		if value.TaxDate != nil {
			date, ok := basCivilDateSQLText(value.TaxDate)
			if !ok {
				return nil, false, ErrSnapshotCorrupt
			}
			taxDate = date
		}
		wire, err := marshalCalculationFrame(value, 1024*1024)
		if err != nil {
			return nil, false, err
		}
		lineage, err := marshalCoverageIdentityReferences(value.Lineage, value.LineageIdentities, true)
		if err != nil || len(lineage) > 2*1024*1024 {
			return nil, false, ErrSnapshotBound
		}
		evidence, err := marshalCoverageIdentityReferences(value.Evidence, value.EvidenceIdentities, false)
		if err != nil || len(evidence) > 2*1024*1024 {
			return nil, false, ErrSnapshotBound
		}
		rowHash := sha256.Sum256(wire)
		row := calculationCoverageInsert{
			sequence: int64(index + 1), candidateKind: candidateKind, disposition: disposition,
			amountMinor: amountMinor, currencyCode: currencyCode, taxDate: taxDate,
			classification: value.ClassificationCode, reason: value.Reason,
			lineageWire: lineage, evidenceWire: evidence, wire: wire, hash: bytes.Clone(rowHash[:]),
		}
		if value.Source != nil {
			sourceRevision, revisionOK := basSQLiteRevision(value.Source.Revision)
			if !revisionOK {
				return nil, false, ErrSnapshotCorrupt
			}
			row.sourceType, row.sourceID, row.sourceRevision = value.Source.Type, value.Source.Id, sourceRevision
			row.sourceHash = bytes.Clone(value.Source.ContentHash)
		} else {
			raw := value.RawSource
			row.raw = true
			row.rawSourceType, row.rawSourceID = raw.Type, raw.Id
			row.originalRevisionPresent = raw.OriginalRevision != nil
			if raw.OriginalRevision != nil {
				row.originalRevision = *raw.OriginalRevision
			}
			row.originalContentHash = bytes.Clone(raw.OriginalContentHash)
			row.computedProofHash = bytes.Clone(raw.ComputedProofHash)
			row.proofScope = coverageProofScopeSQL(raw.ProofScope)
		}
		rows[index] = row
	}
	return rows, usesNewIdentity, nil
}

func marshalCoverageIdentityReferences(references []*tammyv1.SourceRef, identities []*tammyv1.BasSourceIdentity,
	lineage bool) ([]byte, error) {
	if !validBASIdentityProjection(identities, references) {
		return nil, ErrSnapshotCorrupt
	}
	if len(identities) == 0 {
		return marshalCoverageReferences(references, lineage)
	}
	frame := &tammyv1.BasSourceCoverage{}
	if lineage {
		frame.LineageIdentities = cloneBASSourceIdentities(identities)
	} else {
		frame.EvidenceIdentities = cloneBASSourceIdentities(identities)
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(frame)
}

func cloneBASSourceIdentities(values []*tammyv1.BasSourceIdentity) []*tammyv1.BasSourceIdentity {
	result := make([]*tammyv1.BasSourceIdentity, len(values))
	for index, value := range values {
		if value != nil {
			result[index] = proto.Clone(value).(*tammyv1.BasSourceIdentity)
		}
	}
	return result
}

func calculationCoveragePayloadBytes(rows []calculationCoverageInsert, calculationID, organisationID, familyID string,
	calculationHash []byte) (int64, error) {
	var total int64
	for _, row := range rows {
		cells := []int64{
			512,
			int64(len(calculationID)), int64(len(organisationID)), int64(len(familyID)), 8, int64(len(calculationHash)),
			8, int64(len(row.candidateKind)), int64(len(row.disposition)),
			int64(len(row.classification)), int64(len(row.reason)),
			int64(len(row.lineageWire)), int64(len(row.evidenceWire)), int64(len(row.wire)), int64(len(row.hash)),
		}
		if row.raw {
			cells = append(cells, int64(len(row.rawSourceType)), int64(len(row.rawSourceID)),
				int64(len(row.computedProofHash)), int64(len(row.proofScope)))
			if row.originalRevisionPresent {
				cells = append(cells, 8)
			}
			cells = append(cells, int64(len(row.originalContentHash)))
		} else {
			cells = append(cells, int64(len(row.sourceType)), int64(len(row.sourceID)), 8, int64(len(row.sourceHash)))
		}
		if row.amountMinor != nil {
			cells = append(cells, 8)
		}
		if currency, ok := row.currencyCode.(string); ok {
			cells = append(cells, int64(len(currency)))
		}
		if date, ok := row.taxDate.(string); ok {
			cells = append(cells, int64(len(date)))
		}
		for _, cell := range cells {
			if cell < 0 || cell > calculationCoverageIdentityPayloadLimit-total {
				return 0, ErrSnapshotBound
			}
			total += cell
		}
	}
	return total, nil
}

func marshalCoverageReferences(values []*tammyv1.SourceRef, lineage bool) ([]byte, error) {
	if len(values) > 20 {
		return nil, ErrSnapshotBound
	}
	for _, reference := range values {
		if reference == nil || reference.Type == "" || len(reference.Type) > 128 ||
			!validBASSourceRef(reference, reference.Type, "") {
			return nil, ErrSnapshotCorrupt
		}
	}
	references := values
	if len(values) == 0 {
		// The schema requires a nonempty protobuf blob for normalized frames.
		// A single empty SourceRef is the canonical storage marker for an empty
		// repeated field; it is never accepted from the calculation body.
		references = []*tammyv1.SourceRef{{}}
	}
	frame := &tammyv1.BasSourceCoverage{}
	if lineage {
		frame.Lineage = cloneCalculationSourceRefs(references)
	} else {
		frame.Evidence = cloneCalculationSourceRefs(references)
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(frame)
}

func cloneCalculationSourceRefs(values []*tammyv1.SourceRef) []*tammyv1.SourceRef {
	refs := make([]*tammyv1.SourceRef, len(values))
	for index, value := range values {
		if value != nil {
			refs[index] = proto.Clone(value).(*tammyv1.SourceRef)
		}
	}
	return refs
}

func marshalCalculationFrame(value proto.Message, maximum int) ([]byte, error) {
	if value == nil || !value.ProtoReflect().IsValid() || hasUnknownSnapshotFields(value.ProtoReflect()) {
		return nil, ErrSnapshotCorrupt
	}
	if proto.Size(value) > maximum {
		return nil, ErrSnapshotBound
	}
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(value)
	if err != nil {
		return nil, ErrSnapshotCorrupt
	}
	if len(wire) == 0 || len(wire) > maximum {
		return nil, ErrSnapshotBound
	}
	return wire, nil
}

func basCoverageCandidateKindSQL(value tammyv1.BasCoverageCandidateKind) (string, bool) {
	switch value {
	case tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_INVOICE_POSTING,
		tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_PURCHASE_SETTLEMENT,
		tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_PURCHASE_CREDIT,
		tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
		tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_PAYG_FACT,
		tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_OPENING_OR_LEGACY_FACT,
		tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_OTHER_OBLIGATION:
		return strings.TrimPrefix(value.String(), "BAS_COVERAGE_CANDIDATE_KIND_"), true
	default:
		return "", false
	}
}

func basCoverageDispositionSQL(value tammyv1.BasCoverageDisposition) (string, bool) {
	switch value {
	case tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE,
		tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
		tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED,
		tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
		tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO,
		tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_NOT_APPLICABLE:
		return strings.TrimPrefix(value.String(), "BAS_COVERAGE_DISPOSITION_"), true
	default:
		return "", false
	}
}

func basCalculationOutcomeSQL(value tammyv1.BasCalculationOutcome) (string, bool) {
	switch value {
	case tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE,
		tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE,
		tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSUPPORTED:
		return strings.TrimPrefix(value.String(), "BAS_CALCULATION_OUTCOME_"), true
	default:
		return "", false
	}
}

func (repository *CalculationRepository) verifyCoverageRows(ctx context.Context, calculation calculationReadRecord,
	value *tammyv1.BasCalculation) error {
	available, err := calculationRawCoverageAvailable(ctx, repository.executor)
	if err != nil {
		return err
	}
	usesNewIdentity := calculationUsesNewCoverageIdentity(value.Coverage)
	if usesNewIdentity && !available {
		return ErrSnapshotCorrupt
	}
	canonicalAggregate, err := calculationCoverageAggregateFor(ctx, repository.executor,
		"bas_calculation_coverage_v1", calculation.id, calculation.version, usesNewIdentity)
	if err != nil {
		return err
	}
	rawAggregate := calculationCoverageAggregate{}
	if available {
		rawAggregate, err = calculationCoverageAggregateFor(ctx, repository.executor,
			"bas_calculation_raw_coverage_v1", calculation.id, calculation.version, usesNewIdentity)
		if err != nil {
			return err
		}
	}
	combinedCount, ok := safeCalculationCoverageSum(canonicalAggregate.count, rawAggregate.count)
	if !ok || combinedCount < 0 || combinedCount > calculationCoverageCountMaximum {
		return ErrSnapshotBound
	}
	if combinedCount != int64(len(value.Coverage)) {
		return ErrSnapshotCorrupt
	}
	var expectedCanonical, expectedRaw int64
	for _, coverage := range value.Coverage {
		if coverage == nil || (coverage.Source == nil) == (coverage.RawSource == nil) {
			return ErrSnapshotCorrupt
		}
		if coverage.RawSource != nil {
			expectedRaw++
		} else {
			expectedCanonical++
		}
	}
	if canonicalAggregate.count != expectedCanonical || rawAggregate.count != expectedRaw {
		return ErrSnapshotCorrupt
	}
	if usesNewIdentity {
		payload, ok := safeCalculationCoverageSum(canonicalAggregate.payloadBytes, rawAggregate.payloadBytes)
		if !ok || payload > calculationCoverageIdentityPayloadLimit {
			return ErrSnapshotBound
		}
	} else {
		if canonicalAggregate.frameBytes > calculationCoverageFrameLimit || rawAggregate.count != 0 {
			return ErrSnapshotBound
		}
	}

	query := calculationCanonicalCoverageReadSQL
	if available {
		query = calculationMergedCoverageReadSQL
	}
	var coverageRows *sql.Rows
	if available {
		coverageRows, err = repository.executor.QueryContext(ctx, query, calculation.id, calculation.version,
			calculation.id, calculation.version)
	} else {
		coverageRows, err = repository.executor.QueryContext(ctx, query, calculation.id, calculation.version)
	}
	if err != nil {
		return err
	}
	for index, expected := range value.Coverage {
		if !coverageRows.Next() {
			return errors.Join(ErrSnapshotCorrupt, coverageRows.Err(), coverageRows.Close())
		}
		var row calculationCoverageReadRecord
		if err := coverageRows.Scan(&row.calculationID, &row.organisationID, &row.familyID, &row.calculationVersion,
			&row.calculationHash, &row.sequence, &row.candidateKind, &row.disposition, &row.sourceType, &row.sourceID,
			&row.sourceRevision, &row.sourceHash, &row.sourceHashPresent, &row.originalRevision, &row.originalContentHash,
			&row.originalContentHashPresent, &row.computedProofHash, &row.computedProofHashPresent, &row.proofScope,
			&row.amountMinor, &row.currencyCode, &row.taxDate, &row.classification, &row.reason,
			&row.lineageWire, &row.lineageLength, &row.evidenceWire, &row.evidenceLength, &row.wire, &row.wireLength,
			&row.hash, &row.raw); err != nil {
			_ = coverageRows.Close()
			return err
		}
		if err := verifyCalculationCoverageRow(row, expected, int64(index+1), calculation); err != nil {
			_ = coverageRows.Close()
			return err
		}
	}
	if coverageRows.Next() || coverageRows.Err() != nil {
		return errors.Join(ErrSnapshotCorrupt, coverageRows.Err(), coverageRows.Close())
	}
	return coverageRows.Close()
}

type calculationCoverageReadRecord struct {
	calculationID, organisationID, familyID, candidateKind, disposition, sourceType, sourceID  string
	classification, reason                                                                     string
	sequence, calculationVersion, lineageLength, evidenceLength, wireLength, sourceHashPresent int64
	originalContentHashPresent, computedProofHashPresent, raw                                  int64
	calculationHash, sourceHash, originalContentHash, computedProofHash                        []byte
	lineageWire, evidenceWire, wire, hash                                                      []byte
	sourceRevision, originalRevision                                                           sql.NullInt64
	proofScope, currencyCode, taxDate                                                          sql.NullString
	amountMinor                                                                                sql.NullInt64
}

func verifyCalculationCoverageRow(record calculationCoverageReadRecord, expected *tammyv1.BasSourceCoverage,
	sequence int64, calculation calculationReadRecord) error {
	if expected == nil || record.sequence != sequence || record.sequence <= 0 || record.calculationID != calculation.id ||
		record.organisationID != calculation.organisationID || record.familyID != calculation.familyID ||
		record.calculationVersion != calculation.version || !bytes.Equal(record.calculationHash, calculation.hash) ||
		len(record.calculationHash) != sha256.Size || len(record.hash) != sha256.Size {
		return ErrSnapshotCorrupt
	}
	if expected == nil || (expected.Source == nil) == (expected.RawSource == nil) ||
		!validBASIdentityProjection(expected.LineageIdentities, expected.Lineage) ||
		!validBASIdentityProjection(expected.EvidenceIdentities, expected.Evidence) {
		return ErrSnapshotCorrupt
	}
	lineage, err := marshalCoverageIdentityReferences(expected.Lineage, expected.LineageIdentities, true)
	if err != nil {
		return err
	}
	evidence, err := marshalCoverageIdentityReferences(expected.Evidence, expected.EvidenceIdentities, false)
	if err != nil {
		return err
	}
	wire, err := marshalCalculationFrame(expected, 1024*1024)
	if err != nil {
		return err
	}
	if err := boundedCalculationRecordBytes(record.lineageWire, record.lineageLength, 2*1024*1024); err != nil {
		return err
	}
	if err := boundedCalculationRecordBytes(record.evidenceWire, record.evidenceLength, 2*1024*1024); err != nil {
		return err
	}
	if err := boundedCalculationRecordBytes(record.wire, record.wireLength, 1024*1024); err != nil {
		return err
	}
	candidate, candidateOK := basCoverageCandidateKindSQL(expected.CandidateKind)
	disposition, dispositionOK := basCoverageDispositionSQL(expected.Disposition)
	if !candidateOK || !dispositionOK {
		return ErrSnapshotCorrupt
	}
	var expectedDate string
	if expected.TaxDate != nil {
		var valid bool
		expectedDate, valid = basCivilDateSQLText(expected.TaxDate)
		if !valid {
			return ErrSnapshotCorrupt
		}
	}
	amountMatches := !record.amountMinor.Valid && expected.EligibleAmount == nil
	currencyMatches := !record.currencyCode.Valid && expected.EligibleAmount == nil
	if expected.EligibleAmount != nil {
		amountMatches = record.amountMinor.Valid && record.amountMinor.Int64 == expected.EligibleAmount.MinorUnits
		currencyMatches = record.currencyCode.Valid && record.currencyCode.String == expected.EligibleAmount.CurrencyCode
	}
	taxDateMatches := !record.taxDate.Valid && expected.TaxDate == nil
	if expected.TaxDate != nil {
		taxDateMatches = record.taxDate.Valid && record.taxDate.String == expectedDate
	}
	digest := sha256.Sum256(wire)
	if record.candidateKind != candidate || record.disposition != disposition || !amountMatches || !currencyMatches || !taxDateMatches ||
		record.classification != expected.ClassificationCode || record.reason != expected.Reason ||
		!bytes.Equal(record.lineageWire, lineage) || !bytes.Equal(record.evidenceWire, evidence) ||
		!bytes.Equal(record.wire, wire) || !bytes.Equal(record.hash, digest[:]) {
		return ErrSnapshotCorrupt
	}
	if expected.Source != nil {
		if record.raw != 0 || !validBASSourceRef(expected.Source, expected.Source.Type, "") ||
			record.sourceType != expected.Source.Type || record.sourceID != expected.Source.Id || !record.sourceRevision.Valid ||
			uint64(record.sourceRevision.Int64) != expected.Source.Revision || record.sourceHashPresent != 1 ||
			!bytes.Equal(record.sourceHash, expected.Source.ContentHash) || record.originalRevision.Valid ||
			record.originalContentHashPresent != 0 || record.computedProofHashPresent != 0 || record.proofScope.Valid {
			return ErrSnapshotCorrupt
		}
	} else {
		raw := expected.RawSource
		if record.raw != 1 || !validBASRawSourceRef(raw) || record.sourceType != raw.Type || record.sourceID != raw.Id ||
			record.sourceRevision.Valid || record.sourceHashPresent != 0 || record.originalContentHashPresent != boolInt(raw.OriginalContentHash != nil) ||
			record.computedProofHashPresent != 1 || !record.proofScope.Valid || record.proofScope.String != coverageProofScopeSQL(raw.ProofScope) ||
			!bytes.Equal(record.computedProofHash, raw.ComputedProofHash) {
			return ErrSnapshotCorrupt
		}
		if raw.OriginalRevision == nil {
			if record.originalRevision.Valid {
				return ErrSnapshotCorrupt
			}
		} else if !record.originalRevision.Valid || record.originalRevision.Int64 != *raw.OriginalRevision {
			return ErrSnapshotCorrupt
		}
		if raw.OriginalContentHash == nil {
			if record.originalContentHashPresent != 0 || record.originalContentHash != nil {
				return ErrSnapshotCorrupt
			}
		} else if record.originalContentHashPresent != 1 || !bytes.Equal(record.originalContentHash, raw.OriginalContentHash) {
			return ErrSnapshotCorrupt
		}
	}
	return nil
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func coverageProofScopeSQL(value tammyv1.BasSourceProofScope) string {
	return strings.TrimPrefix(value.String(), "BAS_SOURCE_PROOF_SCOPE_")
}

const calculationCanonicalCoverageReadSQL = `SELECT calculation_id,organisation_id,family_id,calculation_version,calculation_hash,
	sequence,candidate_kind,disposition,source_type,source_id,source_revision,source_hash,source_hash IS NOT NULL,
	NULL AS original_revision,NULL AS original_content_hash,0 AS original_content_hash_present,
	NULL AS computed_proof_hash,0 AS computed_proof_hash_present,NULL AS proof_scope,
	eligible_amount_minor,currency_code,tax_date,classification_code,reason,
	CASE WHEN length(lineage_proto)<=2097152 THEN lineage_proto ELSE NULL END,length(lineage_proto),
	CASE WHEN length(evidence_proto)<=2097152 THEN evidence_proto ELSE NULL END,length(evidence_proto),
	CASE WHEN length(coverage_proto)<=1048576 THEN coverage_proto ELSE NULL END,length(coverage_proto),content_hash,0 AS is_raw
	FROM bas_calculation_coverage_v1 WHERE calculation_id=? AND calculation_version=? ORDER BY sequence`

const calculationMergedCoverageReadSQL = `SELECT * FROM (
	SELECT calculation_id,organisation_id,family_id,calculation_version,calculation_hash,sequence,candidate_kind,disposition,
		source_type AS source_type,source_id AS source_id,source_revision,source_hash,source_hash IS NOT NULL,
		NULL AS original_revision,NULL AS original_content_hash,0 AS original_content_hash_present,
		NULL AS computed_proof_hash,0 AS computed_proof_hash_present,NULL AS proof_scope,
		eligible_amount_minor,currency_code,tax_date,classification_code,reason,
		CASE WHEN length(lineage_proto)<=2097152 THEN lineage_proto ELSE NULL END,length(lineage_proto),
		CASE WHEN length(evidence_proto)<=2097152 THEN evidence_proto ELSE NULL END,length(evidence_proto),
		CASE WHEN length(coverage_proto)<=1048576 THEN coverage_proto ELSE NULL END,length(coverage_proto),content_hash,0 AS is_raw
	FROM bas_calculation_coverage_v1 WHERE calculation_id=? AND calculation_version=?
	UNION ALL
	SELECT calculation_id,organisation_id,family_id,calculation_version,calculation_hash,sequence,candidate_kind,disposition,
		raw_source_type AS source_type,raw_source_id AS source_id,NULL AS source_revision,NULL AS source_hash,0 AS source_hash_present,
		original_revision,original_content_hash,original_content_hash IS NOT NULL,
		computed_proof_hash,computed_proof_hash IS NOT NULL,proof_scope,
		eligible_amount_minor,currency_code,tax_date,classification_code,reason,
		CASE WHEN length(lineage_proto)<=2097152 THEN lineage_proto ELSE NULL END,length(lineage_proto),
		CASE WHEN length(evidence_proto)<=2097152 THEN evidence_proto ELSE NULL END,length(evidence_proto),
		CASE WHEN length(coverage_proto)<=1048576 THEN coverage_proto ELSE NULL END,length(coverage_proto),content_hash,1 AS is_raw
	FROM bas_calculation_raw_coverage_v1 WHERE calculation_id=? AND calculation_version=?
) ORDER BY sequence`

const canonicalCoveragePayloadExpression = `512
	+length(CAST(calculation_id AS BLOB))+length(CAST(organisation_id AS BLOB))+length(CAST(family_id AS BLOB))
	+8+length(calculation_hash)+8+length(CAST(candidate_kind AS BLOB))+length(CAST(disposition AS BLOB))
	+length(CAST(source_type AS BLOB))+length(CAST(source_id AS BLOB))+8+length(source_hash)
	+CASE WHEN eligible_amount_minor IS NULL THEN 0 ELSE 8 END
	+CASE WHEN currency_code IS NULL THEN 0 ELSE length(CAST(currency_code AS BLOB)) END
	+CASE WHEN tax_date IS NULL THEN 0 ELSE length(CAST(tax_date AS BLOB)) END
	+length(CAST(classification_code AS BLOB))+length(CAST(reason AS BLOB))
	+length(lineage_proto)+length(evidence_proto)+length(coverage_proto)+length(content_hash)`

const rawCoveragePayloadExpression = `512
	+length(CAST(calculation_id AS BLOB))+length(CAST(organisation_id AS BLOB))+length(CAST(family_id AS BLOB))
	+8+length(calculation_hash)+8+length(CAST(candidate_kind AS BLOB))+length(CAST(disposition AS BLOB))
	+length(CAST(raw_source_type AS BLOB))+length(CAST(raw_source_id AS BLOB))
	+CASE WHEN original_revision IS NULL THEN 0 ELSE 8 END
	+CASE WHEN original_content_hash IS NULL THEN 0 ELSE length(original_content_hash) END
	+length(computed_proof_hash)+length(CAST(proof_scope AS BLOB))
	+CASE WHEN eligible_amount_minor IS NULL THEN 0 ELSE 8 END
	+CASE WHEN currency_code IS NULL THEN 0 ELSE length(CAST(currency_code AS BLOB)) END
	+CASE WHEN tax_date IS NULL THEN 0 ELSE length(CAST(tax_date AS BLOB)) END
	+length(CAST(classification_code AS BLOB))+length(CAST(reason AS BLOB))
	+length(lineage_proto)+length(evidence_proto)+length(coverage_proto)+length(content_hash)`

type calculationCoverageAggregate struct {
	count, payloadBytes, frameBytes int64
}

func calculationCoverageAggregateFor(ctx context.Context, executor app.CommandSQLExecutor, table, calculationID string,
	version int64, includeNormalized bool) (calculationCoverageAggregate, error) {
	var expression string
	switch table {
	case "bas_calculation_coverage_v1":
		if includeNormalized {
			expression = canonicalCoveragePayloadExpression
		}
	case "bas_calculation_raw_coverage_v1":
		if includeNormalized {
			expression = rawCoveragePayloadExpression
		}
	default:
		return calculationCoverageAggregate{}, ErrSnapshotCorrupt
	}
	query := "SELECT COUNT(*),COALESCE(SUM(length(lineage_proto)+length(evidence_proto)+length(coverage_proto)),0),0 FROM " + table +
		" WHERE calculation_id=? AND calculation_version=?"
	if includeNormalized {
		query = "SELECT COUNT(*),COALESCE(SUM(length(lineage_proto)+length(evidence_proto)+length(coverage_proto)),0),COALESCE(SUM(" + expression + "),0) FROM " + table +
			" WHERE calculation_id=? AND calculation_version=?"
	}
	rows, err := executor.QueryContext(ctx, query, calculationID, version)
	if err != nil {
		return calculationCoverageAggregate{}, err
	}
	if !rows.Next() {
		return calculationCoverageAggregate{}, errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	var aggregate calculationCoverageAggregate
	if err := rows.Scan(&aggregate.count, &aggregate.frameBytes, &aggregate.payloadBytes); err != nil || rows.Next() || rows.Err() != nil {
		return calculationCoverageAggregate{}, errors.Join(ErrSnapshotCorrupt, err, rows.Err(), rows.Close())
	}
	if err := rows.Close(); err != nil {
		return calculationCoverageAggregate{}, err
	}
	if aggregate.count < 0 || aggregate.frameBytes < 0 || aggregate.payloadBytes < 0 {
		return calculationCoverageAggregate{}, ErrSnapshotCorrupt
	}
	return aggregate, nil
}

func safeCalculationCoverageSum(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

func calculationUsesNewCoverageIdentity(values []*tammyv1.BasSourceCoverage) bool {
	for _, value := range values {
		if value != nil && (value.RawSource != nil || len(value.LineageIdentities) > 0 || len(value.EvidenceIdentities) > 0) {
			return true
		}
	}
	return false
}

func calculationRawCoverageAvailable(ctx context.Context, executor app.CommandSQLExecutor) (bool, error) {
	rows, err := executor.QueryContext(ctx, `SELECT COALESCE(MAX(version),0),EXISTS(
		SELECT 1 FROM sqlite_schema WHERE type='table' AND name='bas_calculation_raw_coverage_v1') FROM schema_migrations`)
	if err != nil {
		return false, errors.Join(ErrCalculationRepository, err)
	}
	if !rows.Next() {
		return false, errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	var version int64
	var present int64
	if err := rows.Scan(&version, &present); err != nil || rows.Next() || rows.Err() != nil {
		return false, errors.Join(ErrSnapshotCorrupt, err, rows.Err(), rows.Close())
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if version < 0 || (version >= 14) != (present == 1) || (present != 0 && present != 1) {
		return false, ErrSnapshotCorrupt
	}
	return present == 1, nil
}

func boundedCalculationRecordBytes(wire []byte, length int64, maximum int) error {
	if length > int64(maximum) {
		return ErrSnapshotBound
	}
	if length <= 0 || int64(len(wire)) != length {
		return ErrSnapshotCorrupt
	}
	return nil
}

type calculationReadRecord struct {
	id, organisationID, familyID, preparationID, inputSnapshotID, profileID, periodID                 string
	outcome, ruleVersion, calculatorVersion, formVersion, schemaVersion, descriptorVersion, createdAt string
	preparationVersion, version, inputRevision, profileVersion, periodVersion                         int64
	preparationHash, hash, inputHash, inputContentHash, profileHash, periodHash                       []byte
	sourceSnapshotHash, coverageManifestHash, revisionVectorHash                                      []byte
	ruleHash, calculatorHash, formHash, schemaHash, descriptorHash, organisationSnapshotHash          []byte
	resultWire, workpaperWire, revisionVectorWire, organisationWire, wire                             []byte
	resultLength, workpaperLength, revisionVectorLength, organisationLength, wireLength               int64
}

const calculationRecordQuery = `SELECT c.calculation_id,c.organisation_id,c.family_id,c.preparation_id,c.preparation_version,c.preparation_hash,
	c.version,c.content_hash,c.input_snapshot_id,c.input_revision,c.input_hash,c.input_content_hash,c.profile_id,c.profile_version,c.profile_hash,
	c.period_id,c.period_version,c.period_hash,c.outcome,c.source_snapshot_hash,c.coverage_manifest_hash,c.revision_vector_hash,
	c.rule_version,c.rule_hash,c.calculator_version,c.calculator_hash,c.form_version,c.form_hash,c.schema_version,c.schema_hash,
	c.descriptor_version,c.descriptor_hash,c.organisation_snapshot_hash,
	CASE WHEN length(c.result_proto)<=2097152 THEN c.result_proto ELSE NULL END,length(c.result_proto),
	CASE WHEN length(c.workpaper_snapshot_proto)<=4194304 THEN c.workpaper_snapshot_proto ELSE NULL END,length(c.workpaper_snapshot_proto),
	CASE WHEN length(c.revision_vector_proto)<=2097152 THEN c.revision_vector_proto ELSE NULL END,length(c.revision_vector_proto),
	CASE WHEN length(c.organisation_snapshot_proto)<=1048576 THEN c.organisation_snapshot_proto ELSE NULL END,length(c.organisation_snapshot_proto),
	CASE WHEN length(c.calculation_proto)<=4194304 THEN c.calculation_proto ELSE NULL END,length(c.calculation_proto),c.created_at
	FROM bas_calculations_v1 c`

func loadCalculationRecord(ctx context.Context, executor app.CommandSQLExecutor, exact *tammyv1.SourceRef,
	organisationID string) (calculationReadRecord, bool, error) {
	rows, err := executor.QueryContext(ctx, calculationRecordQuery+` WHERE c.organisation_id=? AND c.calculation_id=? AND c.version=?`,
		organisationID, exact.Id, int64(exact.Revision))
	if err != nil {
		return calculationReadRecord{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return calculationReadRecord{}, false, err
		}
		if err := rows.Close(); err != nil {
			return calculationReadRecord{}, false, err
		}
		return calculationReadRecord{}, false, nil
	}
	var record calculationReadRecord
	if err := rows.Scan(&record.id, &record.organisationID, &record.familyID, &record.preparationID,
		&record.preparationVersion, &record.preparationHash, &record.version, &record.hash,
		&record.inputSnapshotID, &record.inputRevision, &record.inputHash, &record.inputContentHash,
		&record.profileID, &record.profileVersion, &record.profileHash, &record.periodID, &record.periodVersion, &record.periodHash,
		&record.outcome, &record.sourceSnapshotHash, &record.coverageManifestHash, &record.revisionVectorHash,
		&record.ruleVersion, &record.ruleHash, &record.calculatorVersion, &record.calculatorHash,
		&record.formVersion, &record.formHash, &record.schemaVersion, &record.schemaHash,
		&record.descriptorVersion, &record.descriptorHash, &record.organisationSnapshotHash,
		&record.resultWire, &record.resultLength, &record.workpaperWire, &record.workpaperLength,
		&record.revisionVectorWire, &record.revisionVectorLength, &record.organisationWire, &record.organisationLength,
		&record.wire, &record.wireLength, &record.createdAt); err != nil {
		return calculationReadRecord{}, false, err
	}
	if rows.Next() {
		return calculationReadRecord{}, false, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return calculationReadRecord{}, false, err
	}
	record.preparationHash, record.hash, record.inputHash, record.inputContentHash = bytes.Clone(record.preparationHash), bytes.Clone(record.hash), bytes.Clone(record.inputHash), bytes.Clone(record.inputContentHash)
	record.profileHash, record.periodHash = bytes.Clone(record.profileHash), bytes.Clone(record.periodHash)
	record.sourceSnapshotHash, record.coverageManifestHash, record.revisionVectorHash = bytes.Clone(record.sourceSnapshotHash), bytes.Clone(record.coverageManifestHash), bytes.Clone(record.revisionVectorHash)
	record.ruleHash, record.calculatorHash, record.formHash = bytes.Clone(record.ruleHash), bytes.Clone(record.calculatorHash), bytes.Clone(record.formHash)
	record.schemaHash, record.descriptorHash, record.organisationSnapshotHash = bytes.Clone(record.schemaHash), bytes.Clone(record.descriptorHash), bytes.Clone(record.organisationSnapshotHash)
	record.resultWire, record.workpaperWire = bytes.Clone(record.resultWire), bytes.Clone(record.workpaperWire)
	record.revisionVectorWire, record.organisationWire, record.wire = bytes.Clone(record.revisionVectorWire), bytes.Clone(record.organisationWire), bytes.Clone(record.wire)
	return record, true, nil
}

func openCalculationReadRecord(record calculationReadRecord, exact *tammyv1.SourceRef) (*tammyv1.BasCalculation, error) {
	for _, recordBytes := range []struct {
		wire   []byte
		length int64
		limit  int
	}{{record.resultWire, record.resultLength, 2 * 1024 * 1024},
		{record.workpaperWire, record.workpaperLength, calculationSnapshotLimit},
		{record.revisionVectorWire, record.revisionVectorLength, 2 * 1024 * 1024},
		{record.organisationWire, record.organisationLength, 1024 * 1024},
		{record.wire, record.wireLength, calculationSnapshotLimit}} {
		if err := boundedCalculationRecordBytes(recordBytes.wire, recordBytes.length, recordBytes.limit); err != nil {
			return nil, err
		}
	}
	if !ids.IsCanonicalV7(record.id) || !ids.IsCanonicalV7(record.organisationID) || !ids.IsCanonicalV7(record.familyID) ||
		!ids.IsCanonicalV7(record.preparationID) || !ids.IsCanonicalV7(record.inputSnapshotID) ||
		!ids.IsCanonicalV7(record.profileID) || !ids.IsCanonicalV7(record.periodID) ||
		record.preparationVersion <= 0 || record.version <= 0 || record.inputRevision <= 0 || record.profileVersion <= 0 || record.periodVersion <= 0 ||
		len(record.preparationHash) != sha256.Size || len(record.hash) != sha256.Size || len(record.inputHash) != sha256.Size ||
		len(record.inputContentHash) != sha256.Size || len(record.profileHash) != sha256.Size || len(record.periodHash) != sha256.Size ||
		len(record.sourceSnapshotHash) != sha256.Size || len(record.coverageManifestHash) != sha256.Size || len(record.revisionVectorHash) != sha256.Size ||
		len(record.ruleHash) != sha256.Size || len(record.calculatorHash) != sha256.Size || len(record.formHash) != sha256.Size ||
		len(record.schemaHash) != sha256.Size || len(record.descriptorHash) != sha256.Size || len(record.organisationSnapshotHash) != sha256.Size {
		return nil, ErrSnapshotCorrupt
	}
	if !sameBASSourceRef(exact, &tammyv1.SourceRef{Type: calculationSnapshotSpec.resource, Id: record.id,
		Revision: uint64(record.version), ContentHash: record.hash}) {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openCalculation(record.wire, record.organisationID, exact)
	if err != nil {
		return nil, err
	}
	preparationRef := &tammyv1.SourceRef{Type: preparationSnapshotSpec.resource, Id: record.preparationID,
		Revision: uint64(record.preparationVersion), ContentHash: record.preparationHash}
	profileRef := &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: record.profileID,
		Revision: uint64(record.profileVersion), ContentHash: record.profileHash}
	periodRef := &tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: record.periodID,
		Revision: uint64(record.periodVersion), ContentHash: record.periodHash}
	outcome, outcomeOK := basCalculationOutcomeSQL(value.Result.Outcome)
	createdAt, createdAtOK := basTimestampSQLText(value.CreatedAt)
	if !outcomeOK || !createdAtOK || createdAt != record.createdAt ||
		!sameBASSourceRef(value.Preparation, preparationRef) || value.Family.Id != record.familyID ||
		!sameBASSourceRef(value.Profile, profileRef) || !sameBASSourceRef(value.Period, periodRef) ||
		value.InputRevision != uint64(record.inputRevision) || !bytes.Equal(value.InputHash, record.inputHash) ||
		record.outcome != outcome || !bytes.Equal(record.sourceSnapshotHash, value.SourceSnapshotHash) ||
		!bytes.Equal(record.coverageManifestHash, value.CoverageManifestHash) || !bytes.Equal(record.revisionVectorHash, value.RevisionVectorHash) ||
		record.ruleVersion != value.RuleVersion || !bytes.Equal(record.ruleHash, value.RuleHash) ||
		record.calculatorVersion != value.CalculatorVersion || !bytes.Equal(record.calculatorHash, value.CalculatorHash) ||
		record.formVersion != value.FormVersion || !bytes.Equal(record.formHash, value.FormHash) ||
		record.schemaVersion != value.SchemaVersion || !bytes.Equal(record.schemaHash, value.SchemaHash) ||
		record.descriptorVersion != value.DescriptorVersion || !bytes.Equal(record.descriptorHash, value.DescriptorHash) ||
		!bytes.Equal(record.organisationSnapshotHash, value.OrganisationSnapshotHash) {
		return nil, ErrSnapshotCorrupt
	}
	for _, frame := range []struct {
		wire  []byte
		value proto.Message
	}{{record.resultWire, value.Result}, {record.workpaperWire, value.WorkpaperSnapshot},
		{record.revisionVectorWire, value.RevisionVector}, {record.organisationWire, value.OrganisationSnapshot}} {
		canonical, err := marshalCalculationFrame(frame.value, len(frame.wire))
		if err != nil || !bytes.Equal(frame.wire, canonical) {
			return nil, ErrSnapshotCorrupt
		}
	}
	return value, nil
}
