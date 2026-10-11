package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrPeriodRepository = errors.New("baspreparation: period repository operation failed")
	ErrPeriodConflict   = errors.New("baspreparation: period append conflicts with retained identity or head")
)

// PeriodRepository retains confirmed period versions through a caller-owned
// SQL transaction and an injected durable cursor codec.
type PeriodRepository struct {
	executor app.CommandSQLExecutor
	cursors  *paging.Codec
}

type PeriodListRequest struct {
	OrganisationID string
	Range          *tammyv1.BasCivilDateRange
	PageSize       uint32
	Cursor         *string
}

type PeriodPage struct {
	Periods []*tammyv1.BasPeriodSummary
	Page    *tammyv1.PageInfo
}

func NewPeriodRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*PeriodRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrPeriodRepository
	}
	return &PeriodRepository{executor: executor, cursors: cursors}, nil
}

// Append seals one confirmed period version. expectedCurrent must be nil for
// the first version and must exactly identify the current version afterward.
func (repository *PeriodRepository) Append(ctx context.Context, value *tammyv1.BasConfirmedPeriod,
	expectedCurrent *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil || value == nil {
		return nil, ErrPeriodRepository
	}
	wire, digest, err := sealPeriod(value)
	if err != nil {
		return nil, errors.Join(ErrPeriodRepository, err)
	}
	start, ok := basCivilDateSQLText(value.Confirmation.GetPeriod().GetStartDate())
	if !ok {
		return nil, ErrPeriodRepository
	}
	end, ok := basCivilDateSQLText(value.Confirmation.GetPeriod().GetEndDate())
	if !ok {
		return nil, ErrPeriodRepository
	}
	window, ok := basPeriodWindowSQL(value.Confirmation.Window)
	if !ok {
		return nil, ErrPeriodRepository
	}
	createdAt, ok := basTimestampSQLText(value.ConfirmedAt)
	if !ok {
		return nil, ErrPeriodRepository
	}
	version, ok := basSQLiteRevision(value.Version)
	if !ok || !validBASSourceRef(value.Confirmation.Profile, profileSnapshotSpec.resource, "") {
		return nil, ErrPeriodRepository
	}
	// The normalized foreign key pins the exact historical identity, but cannot
	// verify its retained protobuf bytes. Verify those bytes before any write.
	profiles, err := NewProfileRepository(repository.executor, repository.cursors)
	if err != nil {
		return nil, errors.Join(ErrPeriodRepository, err)
	}
	if _, err := profiles.GetExact(ctx, value.OrganisationId, value.Confirmation.Profile); err != nil {
		return nil, errors.Join(ErrPeriodRepository, err)
	}
	identityHash := basPeriodIdentityHash(value.OrganisationId, start, end, window)
	identity, err := loadPeriodAppendIdentity(ctx, repository.executor, value.PeriodId)
	if err != nil {
		return nil, ErrPeriodRepository
	}
	if !identity.found {
		if expectedCurrent != nil || value.Version != 1 {
			return nil, ErrPeriodConflict
		}
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_periods_v1(
			period_id,organisation_id,period_start,period_end,reporting_window,identity_hash,created_at)
			VALUES(?,?,?,?,?,?,?)`, value.PeriodId, value.OrganisationId, start, end, window, identityHash, createdAt); err != nil {
			return nil, ErrPeriodConflict
		}
	} else {
		if identity.organisationID != value.OrganisationId || identity.start != start || identity.end != end ||
			identity.window != window || len(identity.identityHash) != sha256.Size ||
			subtle.ConstantTimeCompare(identity.identityHash, identityHash) != 1 || !identity.hasHead ||
			identity.version <= 0 || identity.casVersion != identity.version || len(identity.currentHash) != sha256.Size ||
			expectedCurrent == nil || !validBASSourceRef(expectedCurrent, periodSnapshotSpec.resource, value.PeriodId) ||
			expectedCurrent.Revision != uint64(identity.version) || subtle.ConstantTimeCompare(expectedCurrent.ContentHash, identity.currentHash) != 1 ||
			identity.version == math.MaxInt64 || version != identity.version+1 {
			return nil, ErrPeriodConflict
		}
		if _, err := repository.GetExact(ctx, value.OrganisationId, expectedCurrent); err != nil {
			return nil, err
		}
	}
	profile := value.Confirmation.Profile
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_period_versions_v1(
		period_id,organisation_id,version,profile_id,profile_version,profile_hash,content_hash,period_proto,actor_user_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, value.PeriodId, value.OrganisationId, version, profile.Id,
		int64(profile.Revision), bytes.Clone(profile.ContentHash), digest, wire, value.ActorUserId, createdAt); err != nil {
		return nil, errors.Join(ErrPeriodRepository, err)
	}
	if !identity.found {
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_period_heads_v1(
			period_id,organisation_id,current_version,current_hash,cas_version,updated_at) VALUES(?,?,?,?,1,?)`,
			value.PeriodId, value.OrganisationId, version, digest, createdAt); err != nil {
			return nil, ErrPeriodRepository
		}
	} else {
		result, err := repository.executor.ExecContext(ctx, `UPDATE bas_period_heads_v1
			SET current_version=?,current_hash=?,cas_version=cas_version+1,updated_at=?
			WHERE period_id=? AND organisation_id=? AND current_version=? AND current_hash=? AND cas_version=?`,
			version, digest, createdAt, value.PeriodId, value.OrganisationId,
			identity.version, identity.currentHash, identity.casVersion)
		if err != nil {
			return nil, ErrPeriodRepository
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return nil, ErrPeriodConflict
		}
	}
	return &tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: value.PeriodId,
		Revision: value.Version, ContentHash: bytes.Clone(digest)}, nil
}

// GetExact returns one retained confirmation without resolving the current
// period pointer.
func (repository *PeriodRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasConfirmedPeriod, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(organisationID) || !validBASSourceRef(exact, periodSnapshotSpec.resource, "") {
		return nil, ErrPeriodRepository
	}
	version, _ := basSQLiteRevision(exact.Revision)
	record, found, err := loadPeriodRecord(ctx, repository.executor, `SELECT `+periodRecordColumns+` `+periodRecordFrom+
		` WHERE v.period_id=? AND v.organisation_id=? AND v.version=?`,
		scalarSnapshotLimit, exact.Id, organisationID, version)
	if err != nil || !found {
		return nil, ErrPeriodRepository
	}
	value, err := openPeriodRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrPeriodRepository, err)
	}
	return value, nil
}

// GetCurrent resolves and validates the independent mutable current pointer.
func (repository *PeriodRepository) GetCurrent(ctx context.Context, organisationID, periodID string) (*tammyv1.BasConfirmedPeriod, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(periodID) {
		return nil, ErrPeriodRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT current_version,current_hash,cas_version
		FROM bas_period_heads_v1 WHERE organisation_id=? AND period_id=?`, organisationID, periodID)
	if err != nil {
		return nil, ErrPeriodRepository
	}
	var version, casVersion int64
	var hash []byte
	if !rows.Next() || rows.Scan(&version, &hash, &casVersion) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return nil, ErrPeriodRepository
	}
	if rows.Close() != nil || version <= 0 || casVersion != version || len(hash) != sha256.Size {
		return nil, ErrPeriodRepository
	}
	exact := &tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: periodID,
		Revision: uint64(version), ContentHash: bytes.Clone(hash)}
	record, found, err := loadPeriodRecord(ctx, repository.executor, `SELECT `+periodRecordColumns+` `+periodRecordFrom+
		` JOIN bas_period_heads_v1 h ON h.period_id=v.period_id AND h.organisation_id=v.organisation_id
		AND h.current_version=v.version AND h.current_hash=v.content_hash
		WHERE h.organisation_id=? AND h.period_id=? AND v.version=?`,
		scalarSnapshotLimit, organisationID, periodID, version)
	if err != nil || !found {
		return nil, ErrPeriodRepository
	}
	value, err := openPeriodRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrPeriodRepository, err)
	}
	return value, nil
}

// List returns the newest period version for each immutable actual-date/window
// identity as of a signed append watermark. Exact older revisions remain
// available through GetExact.
func (repository *PeriodRepository) List(ctx context.Context, request PeriodListRequest) (*PeriodPage, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(request.OrganisationID) {
		return nil, ErrPeriodRepository
	}
	pageSize, ok := basNormalizedPageSize(request.PageSize)
	if !ok {
		return nil, ErrPeriodRepository
	}
	rangeStart, rangeEnd, ok := basPeriodListRange(request.Range)
	if !ok {
		return nil, ErrPeriodRepository
	}
	queryHash := basPageQueryHash("bas_period_current_list_v1", request.OrganisationID, rangeStart, rangeEnd)
	snapshot := basPageSnapshot{}
	var position periodPageKey
	if request.Cursor == nil {
		var err error
		snapshot.ordinal, snapshot.hash, err = basPeriodSnapshot(ctx, repository.executor, request.OrganisationID)
		if err != nil {
			return nil, ErrPeriodRepository
		}
		if snapshot.ordinal == 0 {
			return &PeriodPage{Periods: []*tammyv1.BasPeriodSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrPeriodRepository
		}
		snapshot, ok = basParsePageSnapshot(cursor.Snapshot)
		if !ok {
			return nil, ErrPeriodRepository
		}
		position, ok = parsePeriodPagePosition(cursor.Position)
		if !ok {
			return nil, ErrPeriodRepository
		}
		maximum, err := basMaximumPeriodOrdinal(ctx, repository.executor, request.OrganisationID)
		if err != nil || maximum < snapshot.ordinal {
			return nil, ErrPeriodRepository
		}
		endpoint, found, err := loadPeriodRecord(ctx, repository.executor, `SELECT `+periodRecordColumns+` `+periodRecordFrom+
			` WHERE v.organisation_id=? AND v.append_ordinal=?`, scalarSnapshotLimit, request.OrganisationID, snapshot.ordinal)
		if err != nil || !found || endpoint.ordinal != snapshot.ordinal || !bytes.Equal(endpoint.hash, snapshot.hash) {
			return nil, ErrPeriodRepository
		}
		if _, err := openPeriodRecord(endpoint, nil); err != nil {
			return nil, ErrPeriodRepository
		}
		anchor, found, err := loadPeriodRecord(ctx, repository.executor, `SELECT `+periodRecordColumns+` `+periodRecordFrom+
			` WHERE p.period_id=? AND p.organisation_id=? AND v.append_ordinal=(
			SELECT MAX(v2.append_ordinal) FROM bas_period_versions_v1 v2
			WHERE v2.period_id=p.period_id AND v2.organisation_id=p.organisation_id AND v2.append_ordinal<=?)`,
			scalarSnapshotLimit, position.periodID, request.OrganisationID, snapshot.ordinal)
		if err != nil || !found || anchor.ordinal > snapshot.ordinal {
			return nil, ErrPeriodRepository
		}
		if _, err := openPeriodRecord(anchor, nil); err != nil {
			return nil, ErrPeriodRepository
		}
		if periodRecordKey(anchor) != position || !basPeriodOverlapsRange(anchor.start, anchor.end, rangeStart, rangeEnd) {
			return nil, ErrPeriodRepository
		}
	}
	query := `SELECT ` + periodRecordColumns + ` ` + periodRecordFrom + `
		WHERE p.organisation_id=? AND v.append_ordinal<=? AND v.append_ordinal=(
		SELECT MAX(v2.append_ordinal) FROM bas_period_versions_v1 v2
		WHERE v2.period_id=v.period_id AND v2.organisation_id=v.organisation_id AND v2.append_ordinal<=?)`
	arguments := []any{scalarSnapshotLimit, request.OrganisationID, snapshot.ordinal, snapshot.ordinal}
	if rangeStart != "" {
		query += ` AND p.period_end>=? AND p.period_start<=?`
		arguments = append(arguments, rangeStart, rangeEnd)
	}
	if position.periodID != "" {
		query += ` AND (p.period_start>? OR (p.period_start=? AND p.period_end>?) OR
			(p.period_start=? AND p.period_end=? AND p.reporting_window>?) OR
			(p.period_start=? AND p.period_end=? AND p.reporting_window=? AND p.period_id>?))`
		arguments = append(arguments, position.start, position.start, position.end,
			position.start, position.end, position.window, position.start, position.end, position.window, position.periodID)
	}
	query += ` ORDER BY p.period_start,p.period_end,p.reporting_window,p.period_id LIMIT ?`
	arguments = append(arguments, pageSize+1)
	rows, err := repository.executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, ErrPeriodRepository
	}
	records := make([]periodVersionRecord, 0, pageSize+1)
	for rows.Next() {
		record, scanErr := scanPeriodVersionRecord(rows)
		if scanErr != nil || record.ordinal > snapshot.ordinal ||
			(position.periodID != "" && !periodPageKeyLess(position, periodRecordKey(record))) ||
			(len(records) != 0 && !periodRecordKeyLess(records[len(records)-1], record)) {
			_ = rows.Close()
			return nil, ErrPeriodRepository
		}
		if _, openErr := openPeriodRecord(record, nil); openErr != nil ||
			!basPeriodOverlapsRange(record.start, record.end, rangeStart, rangeEnd) {
			_ = rows.Close()
			return nil, ErrPeriodRepository
		}
		records = append(records, record)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil || closeErr != nil || len(records) > pageSize+1 {
		return nil, ErrPeriodRepository
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records = records[:pageSize]
	}
	page := &PeriodPage{Periods: make([]*tammyv1.BasPeriodSummary, len(records)),
		Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(records))}}
	for index, record := range records {
		value, openErr := openPeriodRecord(record, nil)
		if openErr != nil {
			return nil, ErrPeriodRepository
		}
		page.Periods[index] = periodSummary(value)
	}
	if hasMore && len(records) != 0 {
		cursor, encodeErr := repository.cursors.Encode(paging.Cursor{
			Snapshot: formatBASPageSnapshot(snapshot), Position: formatPeriodPagePosition(periodRecordKey(records[len(records)-1])), QueryHash: queryHash,
		})
		if encodeErr != nil || cursor == "" {
			return nil, ErrPeriodRepository
		}
		page.Page.NextCursor = &cursor
	}
	return page, nil
}

const periodRecordColumns = `v.append_ordinal,v.period_id,v.organisation_id,v.version,v.profile_id,v.profile_version,
	v.profile_hash,v.content_hash,CASE WHEN length(v.period_proto)<=? THEN v.period_proto ELSE NULL END,
	length(v.period_proto),v.created_at,v.actor_user_id,p.period_start,p.period_end,p.reporting_window,p.identity_hash`

const periodRecordFrom = `FROM bas_period_versions_v1 v JOIN bas_periods_v1 p
	ON p.period_id=v.period_id AND p.organisation_id=v.organisation_id`

type periodAppendIdentity struct {
	found          bool
	organisationID string
	start          string
	end            string
	window         string
	identityHash   []byte
	hasHead        bool
	version        int64
	currentHash    []byte
	casVersion     int64
}

func loadPeriodAppendIdentity(ctx context.Context, executor app.CommandSQLExecutor, periodID string) (periodAppendIdentity, error) {
	rows, err := executor.QueryContext(ctx, `SELECT p.organisation_id,p.period_start,p.period_end,p.reporting_window,
		p.identity_hash,h.current_version,h.current_hash,h.cas_version
		FROM bas_periods_v1 p LEFT JOIN bas_period_heads_v1 h
		ON h.period_id=p.period_id AND h.organisation_id=p.organisation_id WHERE p.period_id=?`, periodID)
	if err != nil {
		return periodAppendIdentity{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return periodAppendIdentity{}, err
		}
		if err := rows.Close(); err != nil {
			return periodAppendIdentity{}, err
		}
		return periodAppendIdentity{}, nil
	}
	var identity periodAppendIdentity
	var version, casVersion sql.NullInt64
	if err := rows.Scan(&identity.organisationID, &identity.start, &identity.end, &identity.window,
		&identity.identityHash, &version, &identity.currentHash, &casVersion); err != nil {
		return periodAppendIdentity{}, err
	}
	if rows.Next() || rows.Err() != nil {
		return periodAppendIdentity{}, ErrPeriodRepository
	}
	if err := rows.Close(); err != nil {
		return periodAppendIdentity{}, err
	}
	identity.found = true
	identity.hasHead = version.Valid && casVersion.Valid
	if identity.hasHead {
		identity.version, identity.casVersion = version.Int64, casVersion.Int64
	}
	identity.identityHash, identity.currentHash = bytes.Clone(identity.identityHash), bytes.Clone(identity.currentHash)
	return identity, nil
}

type periodVersionRecord struct {
	ordinal        int64
	periodID       string
	organisationID string
	version        int64
	profileID      string
	profileVersion int64
	profileHash    []byte
	hash           []byte
	wire           []byte
	wireLength     int64
	createdAt      string
	actorUserID    string
	start          string
	end            string
	window         string
	identityHash   []byte
}

func loadPeriodRecord(ctx context.Context, executor app.CommandSQLExecutor, query string, arguments ...any) (periodVersionRecord, bool, error) {
	rows, err := executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return periodVersionRecord{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return periodVersionRecord{}, false, err
		}
		if err := rows.Close(); err != nil {
			return periodVersionRecord{}, false, err
		}
		return periodVersionRecord{}, false, nil
	}
	record, err := scanPeriodVersionRecord(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return periodVersionRecord{}, false, ErrPeriodRepository
	}
	if err := rows.Close(); err != nil {
		return periodVersionRecord{}, false, err
	}
	return record, true, nil
}

type periodRowScanner interface{ Scan(...any) error }

func scanPeriodVersionRecord(scanner periodRowScanner) (periodVersionRecord, error) {
	var record periodVersionRecord
	if err := scanner.Scan(&record.ordinal, &record.periodID, &record.organisationID, &record.version,
		&record.profileID, &record.profileVersion, &record.profileHash, &record.hash, &record.wire,
		&record.wireLength, &record.createdAt, &record.actorUserID, &record.start, &record.end, &record.window, &record.identityHash); err != nil {
		return periodVersionRecord{}, err
	}
	record.profileHash, record.hash, record.wire, record.identityHash = bytes.Clone(record.profileHash),
		bytes.Clone(record.hash), bytes.Clone(record.wire), bytes.Clone(record.identityHash)
	return record, nil
}

func openPeriodRecord(record periodVersionRecord, expected *tammyv1.SourceRef) (*tammyv1.BasConfirmedPeriod, error) {
	if record.ordinal <= 0 || record.version <= 0 || record.profileVersion <= 0 ||
		record.wireLength <= 0 || record.wireLength > scalarSnapshotLimit || int64(len(record.wire)) != record.wireLength ||
		len(record.profileHash) != sha256.Size || len(record.hash) != sha256.Size || len(record.identityHash) != sha256.Size ||
		!ids.IsCanonicalV7(record.periodID) || !ids.IsCanonicalV7(record.organisationID) || !ids.IsCanonicalV7(record.profileID) {
		return nil, ErrSnapshotCorrupt
	}
	exact := &tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: record.periodID,
		Revision: uint64(record.version), ContentHash: bytes.Clone(record.hash)}
	if expected != nil && !sameBASSourceRef(exact, expected) {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openPeriod(record.wire, record.organisationID, exact)
	if err != nil {
		return nil, err
	}
	start, startOK := basCivilDateSQLText(value.Confirmation.GetPeriod().GetStartDate())
	end, endOK := basCivilDateSQLText(value.Confirmation.GetPeriod().GetEndDate())
	window, windowOK := basPeriodWindowSQL(value.Confirmation.Window)
	createdAt, timestampOK := basTimestampSQLText(value.ConfirmedAt)
	profile := value.Confirmation.GetProfile()
	identityHash := basPeriodIdentityHash(record.organisationID, start, end, window)
	if !startOK || !endOK || !windowOK || !timestampOK || start != record.start || end != record.end ||
		window != record.window || createdAt != record.createdAt || value.ActorUserId != record.actorUserID || len(identityHash) != len(record.identityHash) ||
		subtle.ConstantTimeCompare(identityHash, record.identityHash) != 1 || profile == nil ||
		profile.Id != record.profileID || profile.Revision != uint64(record.profileVersion) ||
		len(profile.ContentHash) != sha256.Size || subtle.ConstantTimeCompare(profile.ContentHash, record.profileHash) != 1 {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}

func basMaximumPeriodOrdinal(ctx context.Context, executor app.CommandSQLExecutor, organisationID string) (int64, error) {
	rows, err := executor.QueryContext(ctx, `SELECT COALESCE(MAX(append_ordinal),0) FROM bas_period_versions_v1 WHERE organisation_id=?`, organisationID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, ErrPeriodRepository
	}
	var maximum int64
	if err := rows.Scan(&maximum); err != nil || rows.Next() || rows.Err() != nil || maximum < 0 {
		return 0, ErrPeriodRepository
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	return maximum, nil
}

func basPeriodSnapshot(ctx context.Context, executor app.CommandSQLExecutor, organisationID string) (int64, []byte, error) {
	maximum, err := basMaximumPeriodOrdinal(ctx, executor, organisationID)
	if err != nil || maximum == 0 {
		return maximum, nil, err
	}
	record, found, err := loadPeriodRecord(ctx, executor, `SELECT `+periodRecordColumns+` `+periodRecordFrom+
		` WHERE v.organisation_id=? AND v.append_ordinal=?`, scalarSnapshotLimit, organisationID, maximum)
	if err != nil || !found || record.ordinal != maximum {
		return 0, nil, ErrPeriodRepository
	}
	if _, err := openPeriodRecord(record, nil); err != nil {
		return 0, nil, ErrPeriodRepository
	}
	return maximum, bytes.Clone(record.hash), nil
}

func periodSummary(value *tammyv1.BasConfirmedPeriod) *tammyv1.BasPeriodSummary {
	return &tammyv1.BasPeriodSummary{
		Period: &tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: value.PeriodId,
			Revision: value.Version, ContentHash: bytes.Clone(value.ContentHash)},
		Dates:  proto.Clone(value.Confirmation.Period).(*tammyv1.BasCivilDateRange),
		Window: value.Confirmation.Window, ConfirmedDueDate: proto.Clone(value.Confirmation.ConfirmedDueDate).(*tammyv1.CivilDate),
		SupportState: value.Confirmation.SupportState, Profile: proto.Clone(value.Confirmation.Profile).(*tammyv1.SourceRef),
		ConfirmedAt: proto.Clone(value.ConfirmedAt).(*timestamppb.Timestamp),
	}
}

func basPeriodWindowSQL(window tammyv1.BasPeriodWindow) (string, bool) {
	switch window {
	case tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_MONTHLY:
		return "MONTHLY", true
	case tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP:
		return "JUL_SEP", true
	case tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC:
		return "OCT_DEC", true
	case tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR:
		return "JAN_MAR", true
	case tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN:
		return "APR_JUN", true
	case tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_CUSTOM:
		return "CUSTOM", true
	default:
		return "", false
	}
}

func basPeriodWindowFromSQL(window string) (tammyv1.BasPeriodWindow, bool) {
	switch window {
	case "MONTHLY":
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_MONTHLY, true
	case "JUL_SEP":
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, true
	case "OCT_DEC":
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC, true
	case "JAN_MAR":
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR, true
	case "APR_JUN":
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN, true
	case "CUSTOM":
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_CUSTOM, true
	default:
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_UNSPECIFIED, false
	}
}

func basPeriodIdentityHash(organisationID, start, end, window string) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("tammy-bas-period-identity-v1\x00"))
	for _, value := range []string{organisationID, start, end, window} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	return hash.Sum(nil)
}

func basPeriodListRange(value *tammyv1.BasCivilDateRange) (string, string, bool) {
	if value == nil {
		return "", "", true
	}
	start, startOK := basCivilDateSQLText(value.StartDate)
	end, endOK := basCivilDateSQLText(value.EndDate)
	return start, end, startOK && endOK && start <= end
}

func basPeriodOverlapsRange(periodStart, periodEnd, rangeStart, rangeEnd string) bool {
	return rangeStart == "" || (periodEnd >= rangeStart && periodStart <= rangeEnd)
}

type periodPageKey struct {
	start    string
	end      string
	window   string
	periodID string
}

func periodRecordKey(record periodVersionRecord) periodPageKey {
	return periodPageKey{start: record.start, end: record.end, window: record.window, periodID: record.periodID}
}

func formatPeriodPagePosition(key periodPageKey) string {
	return key.start + "|" + key.end + "|" + key.window + "|" + key.periodID
}

func parsePeriodPagePosition(value string) (periodPageKey, bool) {
	parts := strings.Split(value, "|")
	if len(parts) != 4 || !ids.IsCanonicalV7(parts[3]) {
		return periodPageKey{}, false
	}
	if _, ok := basCivilDateSQLText(periodPageKeyCivilDate(parts[0])); !ok {
		return periodPageKey{}, false
	}
	if _, ok := basCivilDateSQLText(periodPageKeyCivilDate(parts[1])); !ok {
		return periodPageKey{}, false
	}
	if _, ok := basPeriodWindowFromSQL(parts[2]); !ok || parts[0] > parts[1] {
		return periodPageKey{}, false
	}
	return periodPageKey{start: parts[0], end: parts[1], window: parts[2], periodID: parts[3]}, true
}

func periodPageKeyCivilDate(value string) *tammyv1.CivilDate {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return nil
	}
	year, yearErr := strconv.Atoi(value[:4])
	month, monthErr := strconv.Atoi(value[5:7])
	day, dayErr := strconv.Atoi(value[8:])
	if yearErr != nil || monthErr != nil || dayErr != nil {
		return nil
	}
	date := &tammyv1.CivilDate{Year: int32(year), Month: int32(month), Day: int32(day)}
	canonical, ok := basCivilDateSQLText(date)
	if !ok || canonical != value {
		return nil
	}
	return date
}

func periodRecordKeyLess(left, right periodVersionRecord) bool {
	return periodPageKeyLess(periodRecordKey(left), periodRecordKey(right))
}

func periodPageKeyLess(a, b periodPageKey) bool {
	if a.start != b.start {
		return a.start < b.start
	}
	if a.end != b.end {
		return a.end < b.end
	}
	if a.window != b.window {
		return a.window < b.window
	}
	return a.periodID < b.periodID
}

var _ app.CommandSQLExecutor = (*sql.Tx)(nil)
