package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var ErrPreparationRead = errors.New("baspreparation: retained preparation read failed")

// PreparationRetainedSnapshot keeps normalized storage lineage separate from
// the public preparation body. Saved inputs have their own immutable envelope
// hash; the preparation's stable input hash identifies only the reviewed pair.
type PreparationRetainedSnapshot struct {
	Preparation     *tammyv1.BasPreparation
	SavedPaygInputs *tammyv1.SourceRef
	BranchID        *string
	AttemptID       string
}

// PreparationReader verifies exact preparation bodies and normalized metadata,
// plus their immutable family/profile/period owners. Calculation, validation,
// approval and event repositories verify their own complete retained bodies.
type PreparationReader struct {
	executor app.CommandSQLExecutor
	cursors  *paging.Codec
}

func NewPreparationReader(executor app.CommandSQLExecutor, cursors *paging.Codec) (*PreparationReader, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrPreparationRead
	}
	return &PreparationReader{executor: executor, cursors: cursors}, nil
}

func (reader *PreparationReader) GetExact(ctx context.Context, organisationID string, exact *tammyv1.SourceRef) (*PreparationRetainedSnapshot, error) {
	if !reader.ready(ctx) || !ids.IsCanonicalV7(organisationID) || !validBASSourceRef(exact, preparationSnapshotSpec.resource, "") {
		return nil, ErrPreparationRead
	}
	version, _ := basSQLiteRevision(exact.Revision)
	rows, err := reader.executor.QueryContext(ctx, preparationRecordQuery+` WHERE v.organisation_id=? AND v.preparation_id=? AND v.version=?`,
		scalarSnapshotLimit, organisationID, exact.Id, version)
	if err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.Join(ErrPreparationRead, rows.Err(), rows.Close())
	}
	var record preparationReadRecord
	if err := rows.Scan(&record.id, &record.org, &record.familyID, &record.familyHash, &record.version,
		&record.profile.id, &record.profile.version, &record.profile.hash,
		&record.period.id, &record.period.version, &record.period.hash,
		&record.input.id, &record.input.version, &record.input.hash, &record.inputHash,
		&record.calculation.id, &record.calculation.version, &record.calculation.hash,
		&record.validation.id, &record.validation.version, &record.validation.hash,
		&record.predecessor.id, &record.predecessor.version, &record.predecessor.hash,
		&record.successor.id, &record.successor.version, &record.successor.hash,
		&record.overlay, &record.sourceHash, &record.state, &record.freezeID, &record.freezeHash,
		&record.hash, &record.wire, &record.wireLength, &record.createdAt, &record.updatedAt,
		&record.branchID, &record.attemptID, &record.identityPredecessor, &record.identityCreatedAt); err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	if rows.Next() {
		return nil, errors.Join(ErrPreparationRead, ErrSnapshotCorrupt)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	retained, err := openPreparationReadRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	if err := reader.verifyScopeOwners(ctx, retained.Preparation); err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	return retained, nil
}

func (reader *PreparationReader) GetCurrent(ctx context.Context, organisationID, preparationID string) (*PreparationRetainedSnapshot, error) {
	if !reader.ready(ctx) || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(preparationID) {
		return nil, ErrPreparationRead
	}
	rows, err := reader.executor.QueryContext(ctx, `SELECT current_version,current_hash,cas_version,updated_at
		FROM bas_preparation_heads_v1 WHERE organisation_id=? AND preparation_id=?`, organisationID, preparationID)
	if err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	defer rows.Close()
	var version, casVersion int64
	var hash []byte
	var updatedAt string
	if !rows.Next() {
		return nil, ErrPreparationRead
	}
	if err := rows.Scan(&version, &hash, &casVersion, &updatedAt); err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	if rows.Next() {
		return nil, errors.Join(ErrPreparationRead, ErrSnapshotCorrupt)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrPreparationRead, err)
	}
	if version <= 0 || version != casVersion {
		return nil, errors.Join(ErrPreparationRead, ErrSnapshotCorrupt)
	}
	value, err := reader.GetExact(ctx, organisationID, &tammyv1.SourceRef{Type: preparationSnapshotSpec.resource,
		Id: preparationID, Revision: uint64(version), ContentHash: bytes.Clone(hash)})
	if err != nil {
		return nil, err
	}
	retainedTime, validTime := basTimestampSQLText(value.Preparation.UpdatedAt)
	if !validTime || updatedAt != retainedTime {
		return nil, errors.Join(ErrPreparationRead, ErrSnapshotCorrupt)
	}
	return value, nil
}

func (reader *PreparationReader) ready(ctx context.Context) bool {
	return reader != nil && !nilBASRepositoryExecutor(reader.executor) && reader.cursors != nil && ctx != nil && ctx.Err() == nil
}

func (reader *PreparationReader) verifyScopeOwners(ctx context.Context, value *tammyv1.BasPreparation) error {
	families, err := NewFamilyIdentityRepository(reader.executor)
	if err != nil {
		return err
	}
	family, err := families.GetExact(ctx, value.OrganisationId, value.Family)
	if err != nil {
		return err
	}
	profiles, err := NewProfileRepository(reader.executor, reader.cursors)
	if err != nil {
		return err
	}
	if _, err := profiles.GetExact(ctx, value.OrganisationId, value.Profile); err != nil {
		return err
	}
	periods, err := NewPeriodRepository(reader.executor, reader.cursors)
	if err != nil {
		return err
	}
	period, err := periods.GetExact(ctx, value.OrganisationId, value.Period)
	if err != nil {
		return err
	}
	scope, err := FamilyScopeFromPeriod(period)
	if err != nil || !SameFamilyScope(scope, FamilyScope{OrganisationID: family.OrganisationId,
		Start: family.PeriodScope.StartDate, End: family.PeriodScope.EndDate, Window: family.Window}) ||
		!sameBASSourceRef(period.Confirmation.Profile, value.Profile) {
		return ErrSnapshotCorrupt
	}
	return nil
}

const preparationRecordQuery = `SELECT v.preparation_id,v.organisation_id,v.family_id,f.identity_hash,v.version,
	v.profile_id,v.profile_version,v.profile_hash,v.period_id,v.period_version,v.period_hash,
	v.input_snapshot_id,v.input_revision,v.input_content_hash,v.input_hash,
	v.calculation_id,v.calculation_version,v.calculation_hash,v.validation_id,v.validation_revision,v.validation_hash,
	v.predecessor_id,v.predecessor_version,v.predecessor_hash,v.superseded_by_preparation_id,v.superseded_by_version,v.superseded_by_hash,
	v.dispute_overlay,v.source_snapshot_hash,v.state,v.freeze_id,v.freeze_hash,v.content_hash,
	CASE WHEN length(v.preparation_proto)<=? THEN v.preparation_proto ELSE NULL END,length(v.preparation_proto),v.created_at,v.updated_at,
	i.branch_id,i.attempt_id,i.predecessor_id,i.created_at
	FROM bas_preparation_versions_v1 v
	JOIN bas_preparation_identities_v1 i ON i.preparation_id=v.preparation_id AND i.organisation_id=v.organisation_id AND i.family_id=v.family_id
	JOIN bas_family_identities_v1 f ON f.family_id=v.family_id AND f.organisation_id=v.organisation_id`

type preparationReadSource struct {
	id      sql.NullString
	version sql.NullInt64
	hash    []byte
}

func (source preparationReadSource) matches(value *tammyv1.SourceRef, resource string) bool {
	if value == nil {
		return !source.id.Valid && !source.version.Valid && source.hash == nil
	}
	return source.id.Valid && source.version.Valid && source.version.Int64 > 0 &&
		validBASSourceRef(value, resource, source.id.String) && uint64(source.version.Int64) == value.Revision && bytes.Equal(source.hash, value.ContentHash)
}

type preparationReadRecord struct {
	id, org, familyID, overlay, state, createdAt, updatedAt, identityCreatedAt, attemptID string
	version, wireLength                                                                   int64
	familyHash, inputHash, sourceHash, freezeHash, hash, wire                             []byte
	profile, period, input, calculation, validation, predecessor, successor               preparationReadSource
	freezeID, branchID, identityPredecessor                                               sql.NullString
}

func openPreparationReadRecord(record preparationReadRecord, exact *tammyv1.SourceRef) (*PreparationRetainedSnapshot, error) {
	if record.wireLength > scalarSnapshotLimit {
		return nil, ErrSnapshotBound
	}
	if record.wireLength <= 0 || int64(len(record.wire)) != record.wireLength || record.version <= 0 ||
		!sameBASSourceRef(exact, &tammyv1.SourceRef{Type: preparationSnapshotSpec.resource, Id: record.id,
			Revision: uint64(record.version), ContentHash: record.hash}) {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openPreparation(record.wire, record.org, exact)
	if err != nil {
		return nil, err
	}
	createdAt, validCreatedAt := basTimestampSQLText(value.CreatedAt)
	updatedAt, validUpdatedAt := basTimestampSQLText(value.UpdatedAt)
	familyRef := &tammyv1.SourceRef{Type: familyIdentitySnapshotSpec.resource, Id: record.familyID, Revision: 1, ContentHash: record.familyHash}
	if !validPreparationIdentity(value) || !sameBASSourceRef(value.Family, familyRef) ||
		!record.profile.matches(value.Profile, profileSnapshotSpec.resource) || !record.period.matches(value.Period, periodSnapshotSpec.resource) ||
		!record.calculation.matches(value.CurrentCalculation, calculationSnapshotSpec.resource) || !record.validation.matches(value.CurrentValidation, "bas_validation") ||
		!record.predecessor.matches(value.Predecessor, preparationSnapshotSpec.resource) || !record.successor.matches(value.SupersededBy, preparationSnapshotSpec.resource) ||
		!record.input.version.Valid || record.input.version.Int64 <= 0 || uint64(record.input.version.Int64) != value.InputRevision ||
		!bytes.Equal(record.inputHash, value.InputHash) || !bytes.Equal(record.sourceHash, value.SourceSnapshotHash) ||
		strings.TrimPrefix(value.State.String(), "BAS_PREPARATION_STATE_") != record.state ||
		strings.TrimPrefix(value.DisputeOverlay.String(), "BAS_DISPUTE_OVERLAY_STATE_") != record.overlay ||
		!validCreatedAt || !validUpdatedAt || createdAt != record.createdAt || createdAt != record.identityCreatedAt || updatedAt != record.updatedAt ||
		!ids.IsCanonicalV7(record.attemptID) || (record.branchID.Valid && !ids.IsCanonicalV7(record.branchID.String)) ||
		(record.identityPredecessor.Valid != (value.Predecessor != nil)) ||
		(record.identityPredecessor.Valid && record.identityPredecessor.String != value.Predecessor.Id) {
		return nil, ErrSnapshotCorrupt
	}
	if value.FrozenBas == nil {
		if record.freezeID.Valid || record.freezeHash != nil {
			return nil, ErrSnapshotCorrupt
		}
	} else if !record.freezeID.Valid || !sameBASSourceRef(value.FrozenBas,
		&tammyv1.SourceRef{Type: frozenSnapshotSpec.resource, Id: record.freezeID.String, Revision: 1, ContentHash: record.freezeHash}) {
		return nil, ErrSnapshotCorrupt
	}
	retained := &PreparationRetainedSnapshot{Preparation: value, AttemptID: record.attemptID}
	if record.branchID.Valid {
		branch := record.branchID.String
		retained.BranchID = &branch
	}
	if record.input.id.Valid {
		exactInput := &tammyv1.SourceRef{Type: "bas_saved_payg_inputs", Id: record.input.id.String,
			Revision: value.InputRevision, ContentHash: bytes.Clone(record.input.hash)}
		if !validBASSourceRef(exactInput, "bas_saved_payg_inputs", "") {
			return nil, ErrSnapshotCorrupt
		}
		retained.SavedPaygInputs = exactInput
	} else if record.input.hash != nil {
		return nil, ErrSnapshotCorrupt
	}
	if !validPreparationPayload(retained) {
		return nil, ErrSnapshotCorrupt
	}
	return retained, nil
}
