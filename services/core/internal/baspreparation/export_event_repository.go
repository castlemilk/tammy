package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var (
	ErrExportEventRepository = errors.New("baspreparation: export event repository operation failed")
	ErrExportEventConflict   = errors.New("baspreparation: export history conflicts with its exact target")
)

// ExportEventRepository retains export facts against an immutable target. The
// caller owns the transaction, authority, audit and export-attempt operation key.
type ExportEventRepository struct {
	executor     app.CommandSQLExecutor
	cursors      *paging.Codec
	calculations *CalculationRepository
	frozen       *FrozenRepository
}

func NewExportEventRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*ExportEventRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrExportEventRepository
	}
	calculations, err := NewCalculationRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	frozen, err := NewFrozenRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	return &ExportEventRepository{executor: executor, cursors: cursors, calculations: calculations, frozen: frozen}, nil
}

func (repository *ExportEventRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && repository.cursors != nil &&
		repository.calculations != nil && repository.frozen != nil && ctx != nil && ctx.Err() == nil
}

func validBASOperationKey(value string) bool {
	return utf8.ValidString(value) && len(value) <= 256 && strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0)
}

func exportTargetRef(target *tammyv1.BasExportTarget) (string, *tammyv1.SourceRef, bool) {
	if target == nil {
		return "", nil, false
	}
	switch value := target.ExactTarget.(type) {
	case *tammyv1.BasExportTarget_Calculation:
		if value != nil && validBASSourceRef(value.Calculation, calculationSnapshotSpec.resource, "") {
			return "CALCULATION", value.Calculation, true
		}
	case *tammyv1.BasExportTarget_FrozenBas:
		if value != nil && validBASSourceRef(value.FrozenBas, frozenSnapshotSpec.resource, "") && value.FrozenBas.Revision == 1 {
			return "FROZEN", value.FrozenBas, true
		}
	}
	return "", nil, false
}

func (repository *ExportEventRepository) verifyTarget(ctx context.Context, org string, target *tammyv1.BasExportTarget) (*tammyv1.SourceRef, error) {
	kind, exact, ok := exportTargetRef(target)
	if !ok {
		return nil, ErrExportEventRepository
	}
	if kind == "CALCULATION" {
		value, err := repository.calculations.GetExact(ctx, org, exact)
		if err != nil {
			return nil, err
		}
		return cloneRef(value.Family), nil
	}
	value, err := repository.frozen.GetExact(ctx, org, exact)
	if err != nil {
		return nil, err
	}
	return cloneRef(value.Family), nil
}

func exportTargetSQLFilter(org string, target *tammyv1.BasExportTarget) (string, []any, bool) {
	kind, exact, ok := exportTargetRef(target)
	if !ok {
		return "", nil, false
	}
	if kind == "CALCULATION" {
		return "organisation_id=? AND target_kind='CALCULATION' AND calculation_id=? AND calculation_version=? AND calculation_hash=?",
			[]any{org, exact.Id, int64(exact.Revision), exact.ContentHash}, true
	}
	return "organisation_id=? AND target_kind='FROZEN' AND frozen_id=? AND frozen_hash=?", []any{org, exact.Id, exact.ContentHash}, true
}

// Append checks a consecutive version scoped to the full exact target. Export
// event identities themselves remain immutable revision-one references.
func (repository *ExportEventRepository) Append(ctx context.Context, org, operationKey string, expectedVersion uint64,
	value *tammyv1.BasExportEvent) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASOperationKey(operationKey) ||
		value == nil || value.OrganisationId != org || expectedVersion >= math.MaxInt64 {
		return nil, ErrExportEventRepository
	}
	wire, digest, err := sealExport(value)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	family, err := repository.verifyTarget(ctx, org, value.Event.Target)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	request := ExportEventListRequest{OrganisationID: org, Target: value.Event.Target}
	head, err := repository.loadExportPageRecords(ctx, request, "ORDER BY append_ordinal DESC LIMIT 1")
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	if len(head) == 0 {
		if expectedVersion != 0 {
			return nil, ErrExportEventConflict
		}
	} else {
		if len(head) != 1 || uint64(head[0].version) != expectedVersion {
			return nil, ErrExportEventConflict
		}
		if _, err := repository.GetExact(ctx, org, head[0].exact); err != nil {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
	}
	recordedAt, ok := basTimestampSQLText(value.RecordedAt)
	if !ok {
		return nil, ErrExportEventRepository
	}
	kind, target, _ := exportTargetRef(value.Event.Target)
	var calculationID, calculationVersion, calculationHash, frozenID, frozenHash, outcome, artifact any
	if kind == "CALCULATION" {
		calculationID, calculationVersion, calculationHash = target.Id, int64(target.Revision), target.ContentHash
	} else {
		frozenID, frozenHash = target.Id, target.ContentHash
	}
	if value.Event.Outcome != nil {
		outcome = strings.TrimPrefix(value.Event.GetOutcome().String(), "BAS_EXPORT_OUTCOME_")
	}
	if value.Event.ArtifactHash != nil {
		artifact = value.Event.ArtifactHash
	}
	_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_export_events_v1(
		export_event_id,operation_key,organisation_id,family_id,target_kind,event_version,
		calculation_id,calculation_version,calculation_hash,frozen_id,frozen_hash,format,phase,outcome,artifact_hash,
		actor_user_id,reason,content_hash,export_proto,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ExportEventId, operationKey, org, family.Id, kind, int64(expectedVersion+1),
		calculationID, calculationVersion, calculationHash, frozenID, frozenHash,
		strings.TrimPrefix(value.Event.Format.String(), "BAS_EXPORT_FORMAT_"), strings.TrimPrefix(value.Event.Phase.String(), "BAS_EXPORT_PHASE_"),
		outcome, artifact, value.ActorUserId, value.Event.Reason, digest, wire, recordedAt)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, ErrExportEventConflict, err)
	}
	return &tammyv1.SourceRef{Type: exportSnapshotSpec.resource, Id: value.ExportEventId, Revision: 1, ContentHash: bytes.Clone(digest)}, nil
}

func (repository *ExportEventRepository) GetExact(ctx context.Context, org string, exact *tammyv1.SourceRef) (*tammyv1.BasExportEvent, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, exportSnapshotSpec.resource, "") || exact.Revision != 1 {
		return nil, ErrExportEventRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT append_ordinal,event_version,operation_key,family_id,target_kind,
		calculation_id,calculation_version,calculation_hash,frozen_id,frozen_hash,format,phase,outcome,artifact_hash,
		actor_user_id,reason,content_hash,CASE WHEN length(export_proto)<=? THEN export_proto ELSE NULL END,recorded_at
		FROM bas_export_events_v1 WHERE organisation_id=? AND export_event_id=?`, exportSnapshotSpec.maxBytes, org, exact.Id)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	var ordinal, version int64
	var operationKey, familyID, kind, format, phase, actor, reason, recordedAt string
	var calculationID, frozenID, outcome sql.NullString
	var calculationVersion sql.NullInt64
	var calculationHash, frozenHash, artifact, hash, wire []byte
	if !rows.Next() {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&ordinal, &version, &operationKey, &familyID, &kind, &calculationID, &calculationVersion, &calculationHash, &frozenID, &frozenHash,
		&format, &phase, &outcome, &artifact, &actor, &reason, &hash, &wire, &recordedAt); err != nil {
		return nil, errors.Join(ErrExportEventRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	value, err := openExport(wire, org, exact)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, err)
	}
	expectedKind, target, ok := exportTargetRef(value.Event.Target)
	expectedTime, timeOK := basTimestampSQLText(value.RecordedAt)
	if !ok || !timeOK || ordinal <= 0 || version <= 0 || !validBASOperationKey(operationKey) || !bytes.Equal(hash, exact.ContentHash) ||
		kind != expectedKind || format != strings.TrimPrefix(value.Event.Format.String(), "BAS_EXPORT_FORMAT_") ||
		phase != strings.TrimPrefix(value.Event.Phase.String(), "BAS_EXPORT_PHASE_") || actor != value.ActorUserId || reason != value.Event.Reason || recordedAt != expectedTime ||
		outcome.Valid != (value.Event.Outcome != nil) || (outcome.Valid && outcome.String != strings.TrimPrefix(value.Event.GetOutcome().String(), "BAS_EXPORT_OUTCOME_")) ||
		(artifact != nil) != (value.Event.ArtifactHash != nil) || !bytes.Equal(artifact, value.Event.ArtifactHash) {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
	}
	if kind == "CALCULATION" {
		if !calculationID.Valid || calculationID.String != target.Id || !calculationVersion.Valid || calculationVersion.Int64 != int64(target.Revision) ||
			!bytes.Equal(calculationHash, target.ContentHash) || frozenID.Valid || frozenHash != nil {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
		}
	} else {
		if !frozenID.Valid || frozenID.String != target.Id || !bytes.Equal(frozenHash, target.ContentHash) ||
			calculationID.Valid || calculationVersion.Valid || calculationHash != nil {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
		}
	}
	family, err := repository.verifyTarget(ctx, org, value.Event.Target)
	if err != nil || family == nil || family.Id != familyID {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, err)
	}
	if err := repository.verifyExportVersionPrefix(ctx, org, value.Event.Target, ordinal, version); err != nil {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, err)
	}
	return value, nil
}

func (repository *ExportEventRepository) verifyExportVersionPrefix(ctx context.Context, org string, target *tammyv1.BasExportTarget, ordinal, version int64) error {
	filter, args, ok := exportTargetSQLFilter(org, target)
	if !ok {
		return ErrSnapshotCorrupt
	}
	args = append(args, ordinal)
	rows, err := repository.executor.QueryContext(ctx, "SELECT COUNT(*),COUNT(DISTINCT event_version),MIN(event_version),MAX(event_version) FROM bas_export_events_v1 WHERE "+filter+" AND append_ordinal<=?", args...)
	if err != nil {
		return err
	}
	var count, distinct, first, last int64
	if !rows.Next() {
		return errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&count, &distinct, &first, &last); err != nil {
		return errors.Join(err, rows.Close())
	}
	if rows.Next() {
		return errors.Join(ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if count != version || distinct != count || first != 1 || last != version {
		return ErrSnapshotCorrupt
	}
	return nil
}
