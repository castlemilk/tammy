package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrProfileRepository = errors.New("baspreparation: profile repository operation failed")
	ErrProfileConflict   = errors.New("baspreparation: profile append conflicts with the retained head")
)

const basRepositoryPageSizeMaximum = 200

// ProfileRepository writes and reads immutable profile snapshots through a
// caller-owned transaction. It never begins or finishes a transaction.
type ProfileRepository struct {
	executor app.CommandSQLExecutor
	cursors  *paging.Codec
}

type ProfileListRequest struct {
	OrganisationID string
	PageSize       uint32
	Cursor         *string
}

type ProfilePage struct {
	Profiles []*tammyv1.BasObligationsProfileSummary
	Page     *tammyv1.PageInfo
}

func NewProfileRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*ProfileRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrProfileRepository
	}
	return &ProfileRepository{executor: executor, cursors: cursors}, nil
}

// Append seals one complete profile revision and compare-and-swaps the current
// pointer against expectedCurrent. Pass nil only for the first revision.
func (repository *ProfileRepository) Append(ctx context.Context, value *tammyv1.BasObligationsProfile,
	expectedCurrent *tammyv1.SourceRef) (*tammyv1.SourceRef, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil || value == nil {
		return nil, ErrProfileRepository
	}
	wire, digest, err := sealProfile(value)
	if err != nil {
		return nil, errors.Join(ErrProfileRepository, err)
	}
	start, end, ok := basProfileIntervalSQLDates(value.Profile.GetEffectiveInterval())
	if !ok {
		return nil, ErrProfileRepository
	}
	createdAt, ok := basTimestampSQLText(value.RecordedAt)
	if !ok {
		return nil, ErrProfileRepository
	}
	version, ok := basSQLiteRevision(value.Revision)
	if !ok {
		return nil, ErrProfileRepository
	}
	identity, err := loadProfileAppendIdentity(ctx, repository.executor, value.ProfileId)
	if err != nil {
		return nil, ErrProfileRepository
	}
	if !identity.found {
		if expectedCurrent != nil || value.Revision != 1 || value.PreviousProfile != nil {
			return nil, ErrProfileConflict
		}
		if _, err := repository.executor.ExecContext(ctx,
			`INSERT INTO bas_profiles_v1(profile_id,organisation_id,created_at) VALUES(?,?,?)`,
			value.ProfileId, value.OrganisationId, createdAt); err != nil {
			return nil, ErrProfileRepository
		}
	} else {
		if identity.organisationID != value.OrganisationId || !identity.hasHead || identity.version <= 0 ||
			identity.casVersion != identity.version || len(identity.hash) != sha256.Size ||
			expectedCurrent == nil || !validBASSourceRef(expectedCurrent, profileSnapshotSpec.resource, value.ProfileId) ||
			expectedCurrent.Revision != uint64(identity.version) || subtle.ConstantTimeCompare(expectedCurrent.ContentHash, identity.hash) != 1 ||
			identity.version == math.MaxInt64 || version != identity.version+1 ||
			!sameBASSourceRef(value.PreviousProfile, expectedCurrent) {
			return nil, ErrProfileConflict
		}
		if _, err := repository.GetExact(ctx, value.OrganisationId, expectedCurrent); err != nil {
			return nil, err
		}
	}
	var previousVersion, previousHash any
	if expectedCurrent != nil {
		previousVersion, previousHash = int64(expectedCurrent.Revision), bytes.Clone(expectedCurrent.ContentHash)
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_profile_versions_v1(
		profile_id,organisation_id,version,previous_version,previous_hash,effective_start,effective_end,content_hash,profile_proto,actor_user_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, value.ProfileId, value.OrganisationId, version, previousVersion, previousHash, start, end,
		digest, wire, value.ActorUserId, createdAt); err != nil {
		return nil, errors.Join(ErrProfileRepository, err)
	}
	if !identity.found {
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_profile_heads_v1(
			profile_id,organisation_id,current_version,current_hash,cas_version,updated_at) VALUES(?,?,?,?,1,?)`,
			value.ProfileId, value.OrganisationId, version, digest, createdAt); err != nil {
			return nil, ErrProfileRepository
		}
	} else {
		result, err := repository.executor.ExecContext(ctx, `UPDATE bas_profile_heads_v1
			SET current_version=?,current_hash=?,cas_version=cas_version+1,updated_at=?
			WHERE profile_id=? AND organisation_id=? AND current_version=? AND current_hash=? AND cas_version=?`,
			version, digest, createdAt, value.ProfileId, value.OrganisationId,
			identity.version, identity.hash, identity.casVersion)
		if err != nil {
			return nil, ErrProfileRepository
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return nil, ErrProfileConflict
		}
	}
	return &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: value.ProfileId,
		Revision: value.Revision, ContentHash: bytes.Clone(digest)}, nil
}

// GetExact reads one immutable revision by its organization-scoped content
// reference. It does not consult the current pointer.
func (repository *ProfileRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasObligationsProfile, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(organisationID) || !validBASSourceRef(exact, profileSnapshotSpec.resource, "") {
		return nil, ErrProfileRepository
	}
	version, _ := basSQLiteRevision(exact.Revision)
	record, found, err := loadProfileRecord(ctx, repository.executor, `SELECT v.append_ordinal,v.profile_id,v.organisation_id,
		v.version,v.effective_start,v.effective_end,v.content_hash,
		CASE WHEN length(v.profile_proto)<=? THEN v.profile_proto ELSE NULL END,length(v.profile_proto),v.created_at,v.actor_user_id,v.previous_version,v.previous_hash
		FROM bas_profile_versions_v1 v WHERE v.profile_id=? AND v.organisation_id=? AND v.version=?`,
		scalarSnapshotLimit, exact.Id, organisationID, version)
	if err != nil || !found {
		return nil, ErrProfileRepository
	}
	value, err := openProfileRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrProfileRepository, err)
	}
	return value, nil
}

// GetCurrent resolves a profile identity's current pointer and validates its
// retained body independently from exact historical reads.
func (repository *ProfileRepository) GetCurrent(ctx context.Context, organisationID, profileID string) (*tammyv1.BasObligationsProfile, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(profileID) {
		return nil, ErrProfileRepository
	}
	head, found, err := loadProfileHead(ctx, repository.executor, organisationID, profileID)
	if err != nil || !found || !head.hasHead || head.version <= 0 || head.casVersion != head.version || len(head.hash) != sha256.Size {
		return nil, ErrProfileRepository
	}
	exact := &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: profileID,
		Revision: uint64(head.version), ContentHash: bytes.Clone(head.hash)}
	version, _ := basSQLiteRevision(exact.Revision)
	record, found, err := loadProfileRecord(ctx, repository.executor, `SELECT v.append_ordinal,v.profile_id,v.organisation_id,
		v.version,v.effective_start,v.effective_end,v.content_hash,
		CASE WHEN length(v.profile_proto)<=? THEN v.profile_proto ELSE NULL END,length(v.profile_proto),v.created_at,v.actor_user_id,v.previous_version,v.previous_hash
		FROM bas_profile_heads_v1 h JOIN bas_profile_versions_v1 v
		ON v.profile_id=h.profile_id AND v.organisation_id=h.organisation_id
		AND v.version=h.current_version AND v.content_hash=h.current_hash
		WHERE h.organisation_id=? AND h.profile_id=? AND h.current_version=?`,
		scalarSnapshotLimit, organisationID, profileID, version)
	if err != nil || !found {
		return nil, ErrProfileRepository
	}
	value, err := openProfileRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrProfileRepository, err)
	}
	return value, nil
}

// List returns every immutable profile revision at or below a signed append-ordinal
// watermark. A later append therefore cannot shift an existing cursor.
func (repository *ProfileRepository) List(ctx context.Context, request ProfileListRequest) (*ProfilePage, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(request.OrganisationID) {
		return nil, ErrProfileRepository
	}
	pageSize, ok := basNormalizedPageSize(request.PageSize)
	if !ok {
		return nil, ErrProfileRepository
	}
	queryHash := basPageQueryHash("bas_profile_history_v1", request.OrganisationID)
	snapshot, position := basPageSnapshot{}, int64(0)
	if request.Cursor == nil {
		var err error
		snapshot.ordinal, snapshot.hash, err = basProfileSnapshot(ctx, repository.executor, request.OrganisationID)
		if err != nil {
			return nil, ErrProfileRepository
		}
		if snapshot.ordinal == 0 {
			return &ProfilePage{Profiles: []*tammyv1.BasObligationsProfileSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrProfileRepository
		}
		snapshot, ok = basParsePageSnapshot(cursor.Snapshot)
		if !ok {
			return nil, ErrProfileRepository
		}
		position, ok = basParsePositiveOrdinal(cursor.Position)
		if !ok || position > snapshot.ordinal {
			return nil, ErrProfileRepository
		}
		maximum, err := basMaximumProfileOrdinal(ctx, repository.executor, request.OrganisationID)
		if err != nil || maximum < snapshot.ordinal {
			return nil, ErrProfileRepository
		}
		endpoint, found, err := loadProfileRecord(ctx, repository.executor, `SELECT v.append_ordinal,v.profile_id,v.organisation_id,
			v.version,v.effective_start,v.effective_end,v.content_hash,
			CASE WHEN length(v.profile_proto)<=? THEN v.profile_proto ELSE NULL END,length(v.profile_proto),v.created_at,v.actor_user_id,v.previous_version,v.previous_hash
			FROM bas_profile_versions_v1 v WHERE v.organisation_id=? AND v.append_ordinal=?`,
			scalarSnapshotLimit, request.OrganisationID, snapshot.ordinal)
		if err != nil || !found || !bytes.Equal(endpoint.hash, snapshot.hash) {
			return nil, ErrProfileRepository
		}
		if _, err := openProfileRecord(endpoint, nil); err != nil {
			return nil, ErrProfileRepository
		}
		anchor, found, err := loadProfileRecord(ctx, repository.executor, `SELECT v.append_ordinal,v.profile_id,v.organisation_id,
			v.version,v.effective_start,v.effective_end,v.content_hash,
			CASE WHEN length(v.profile_proto)<=? THEN v.profile_proto ELSE NULL END,length(v.profile_proto),v.created_at,v.actor_user_id,v.previous_version,v.previous_hash
			FROM bas_profile_versions_v1 v WHERE v.organisation_id=? AND v.append_ordinal=? AND v.append_ordinal<=?`,
			scalarSnapshotLimit, request.OrganisationID, position, snapshot.ordinal)
		if err != nil || !found || anchor.ordinal != position {
			return nil, ErrProfileRepository
		}
		if _, err := openProfileRecord(anchor, nil); err != nil {
			return nil, ErrProfileRepository
		}
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT v.append_ordinal,v.profile_id,v.organisation_id,
		v.version,v.effective_start,v.effective_end,v.content_hash,
		CASE WHEN length(v.profile_proto)<=? THEN v.profile_proto ELSE NULL END,length(v.profile_proto),v.created_at,v.actor_user_id,v.previous_version,v.previous_hash
		FROM bas_profile_versions_v1 v WHERE v.organisation_id=? AND v.append_ordinal>? AND v.append_ordinal<=?
		ORDER BY v.append_ordinal LIMIT ?`, scalarSnapshotLimit, request.OrganisationID, position, snapshot.ordinal, pageSize+1)
	if err != nil {
		return nil, ErrProfileRepository
	}
	records := make([]profileVersionRecord, 0, pageSize+1)
	for rows.Next() {
		record, scanErr := scanProfileVersionRecord(rows)
		if scanErr != nil || record.ordinal <= position || record.ordinal > snapshot.ordinal ||
			(len(records) != 0 && record.ordinal <= records[len(records)-1].ordinal) {
			_ = rows.Close()
			return nil, ErrProfileRepository
		}
		if _, openErr := openProfileRecord(record, nil); openErr != nil {
			_ = rows.Close()
			return nil, ErrProfileRepository
		}
		records = append(records, record)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil || closeErr != nil || len(records) > pageSize+1 {
		return nil, ErrProfileRepository
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records = records[:pageSize]
	}
	page := &ProfilePage{Profiles: make([]*tammyv1.BasObligationsProfileSummary, len(records)),
		Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(records))}}
	for index, record := range records {
		value, openErr := openProfileRecord(record, nil)
		if openErr != nil {
			return nil, ErrProfileRepository
		}
		page.Profiles[index] = profileSummary(value)
	}
	if hasMore && len(records) != 0 {
		last := records[len(records)-1]
		token, encodeErr := repository.cursors.Encode(paging.Cursor{
			Snapshot: formatBASPageSnapshot(snapshot), Position: strconv.FormatInt(last.ordinal, 10), QueryHash: queryHash,
		})
		if encodeErr != nil || token == "" {
			return nil, ErrProfileRepository
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

type profileAppendIdentity struct {
	found          bool
	organisationID string
	hasHead        bool
	version        int64
	hash           []byte
	casVersion     int64
}

func loadProfileAppendIdentity(ctx context.Context, executor app.CommandSQLExecutor, profileID string) (profileAppendIdentity, error) {
	rows, err := executor.QueryContext(ctx, `SELECT p.organisation_id,h.current_version,h.current_hash,h.cas_version
		FROM bas_profiles_v1 p LEFT JOIN bas_profile_heads_v1 h ON h.profile_id=p.profile_id
		WHERE p.profile_id=?`, profileID)
	if err != nil {
		return profileAppendIdentity{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return profileAppendIdentity{}, err
		}
		if err := rows.Close(); err != nil {
			return profileAppendIdentity{}, err
		}
		return profileAppendIdentity{}, nil
	}
	var identity profileAppendIdentity
	var version, casVersion sql.NullInt64
	if err := rows.Scan(&identity.organisationID, &version, &identity.hash, &casVersion); err != nil {
		return profileAppendIdentity{}, err
	}
	if rows.Next() || rows.Err() != nil {
		return profileAppendIdentity{}, ErrProfileRepository
	}
	if err := rows.Close(); err != nil {
		return profileAppendIdentity{}, err
	}
	identity.found = true
	identity.hasHead = version.Valid && casVersion.Valid
	if identity.hasHead {
		identity.version, identity.casVersion = version.Int64, casVersion.Int64
	}
	identity.hash = bytes.Clone(identity.hash)
	return identity, nil
}

type profileVersionRecord struct {
	ordinal         int64
	profileID       string
	organisationID  string
	version         int64
	effectiveStart  string
	effectiveEnd    string
	hash            []byte
	wire            []byte
	wireLength      int64
	createdAt       string
	actorUserID     string
	previousVersion sql.NullInt64
	previousHash    []byte
}

func loadProfileRecord(ctx context.Context, executor app.CommandSQLExecutor, query string, arguments ...any) (profileVersionRecord, bool, error) {
	rows, err := executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return profileVersionRecord{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return profileVersionRecord{}, false, err
		}
		if err := rows.Close(); err != nil {
			return profileVersionRecord{}, false, err
		}
		return profileVersionRecord{}, false, nil
	}
	record, err := scanProfileVersionRecord(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return profileVersionRecord{}, false, ErrProfileRepository
	}
	if err := rows.Close(); err != nil {
		return profileVersionRecord{}, false, err
	}
	return record, true, nil
}

type profileRowScanner interface{ Scan(...any) error }

func scanProfileVersionRecord(scanner profileRowScanner) (profileVersionRecord, error) {
	var record profileVersionRecord
	if err := scanner.Scan(&record.ordinal, &record.profileID, &record.organisationID, &record.version,
		&record.effectiveStart, &record.effectiveEnd, &record.hash, &record.wire, &record.wireLength, &record.createdAt, &record.actorUserID, &record.previousVersion, &record.previousHash); err != nil {
		return profileVersionRecord{}, err
	}
	record.hash, record.wire, record.previousHash = bytes.Clone(record.hash), bytes.Clone(record.wire), bytes.Clone(record.previousHash)
	return record, nil
}

func openProfileRecord(record profileVersionRecord, expected *tammyv1.SourceRef) (*tammyv1.BasObligationsProfile, error) {
	if record.ordinal <= 0 || record.version <= 0 || record.wireLength <= 0 || record.wireLength > scalarSnapshotLimit ||
		int64(len(record.wire)) != record.wireLength || len(record.hash) != sha256.Size ||
		!ids.IsCanonicalV7(record.profileID) || !ids.IsCanonicalV7(record.organisationID) {
		return nil, ErrSnapshotCorrupt
	}
	exact := &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: record.profileID,
		Revision: uint64(record.version), ContentHash: bytes.Clone(record.hash)}
	if expected != nil && !sameBASSourceRef(exact, expected) {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openProfile(record.wire, record.organisationID, exact)
	if err != nil {
		return nil, err
	}
	start, end, ok := basProfileIntervalSQLDates(value.Profile.GetEffectiveInterval())
	createdAt, timestampOK := basTimestampSQLText(value.RecordedAt)
	if !ok || !timestampOK || start != record.effectiveStart || end != record.effectiveEnd || createdAt != record.createdAt || value.ActorUserId != record.actorUserID {
		return nil, ErrSnapshotCorrupt
	}
	if value.PreviousProfile == nil {
		if record.previousVersion.Valid || len(record.previousHash) != 0 {
			return nil, ErrSnapshotCorrupt
		}
	} else if !record.previousVersion.Valid || record.previousVersion.Int64 <= 0 || !sameRef(value.PreviousProfile, &tammyv1.SourceRef{
		Type: profileSnapshotSpec.resource, Id: value.ProfileId, Revision: uint64(record.previousVersion.Int64), ContentHash: record.previousHash,
	}) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}

func loadProfileHead(ctx context.Context, executor app.CommandSQLExecutor, organisationID, profileID string) (profileAppendIdentity, bool, error) {
	identity, err := loadProfileAppendIdentity(ctx, executor, profileID)
	if err != nil || !identity.found || identity.organisationID != organisationID {
		return profileAppendIdentity{}, false, err
	}
	return identity, true, nil
}

func basMaximumProfileOrdinal(ctx context.Context, executor app.CommandSQLExecutor, organisationID string) (int64, error) {
	rows, err := executor.QueryContext(ctx, `SELECT COALESCE(MAX(append_ordinal),0) FROM bas_profile_versions_v1 WHERE organisation_id=?`, organisationID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, ErrProfileRepository
	}
	var maximum int64
	if err := rows.Scan(&maximum); err != nil || rows.Next() || rows.Err() != nil || maximum < 0 {
		return 0, ErrProfileRepository
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	return maximum, nil
}

func profileSummary(value *tammyv1.BasObligationsProfile) *tammyv1.BasObligationsProfileSummary {
	ref := &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: value.ProfileId,
		Revision: value.Revision, ContentHash: bytes.Clone(value.ContentHash)}
	var previous *tammyv1.SourceRef
	if value.PreviousProfile != nil {
		previous = proto.Clone(value.PreviousProfile).(*tammyv1.SourceRef)
	}
	return &tammyv1.BasObligationsProfileSummary{
		Profile: ref, EffectiveInterval: proto.Clone(value.Profile.EffectiveInterval).(*tammyv1.BasEffectiveInterval),
		PreviousProfile: previous, ActorUserId: value.ActorUserId,
		RecordedAt: proto.Clone(value.RecordedAt).(*timestamppb.Timestamp),
	}
}

func basProfileIntervalSQLDates(interval *tammyv1.BasEffectiveInterval) (string, string, bool) {
	if interval == nil {
		return "", "", false
	}
	start, ok := basCivilDateSQLText(interval.StartDate)
	if !ok {
		return "", "", false
	}
	if interval.EndDate == nil {
		return start, "9999-12-31", true
	}
	end, ok := basCivilDateSQLText(interval.EndDate)
	return start, end, ok
}

func basCivilDateSQLText(value *tammyv1.CivilDate) (string, bool) {
	if value == nil || value.Year < 1 || value.Year > 9999 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return "", false
	}
	date := time.Date(int(value.Year), time.Month(value.Month), int(value.Day), 0, 0, 0, 0, time.UTC)
	if date.Year() != int(value.Year) || int(date.Month()) != int(value.Month) || date.Day() != int(value.Day) {
		return "", false
	}
	return date.Format("2006-01-02"), true
}

func basTimestampSQLText(value *timestamppb.Timestamp) (string, bool) {
	if value == nil || !value.IsValid() {
		return "", false
	}
	return value.AsTime().UTC().Format(time.RFC3339Nano), true
}

func basSQLiteRevision(value uint64) (int64, bool) {
	if value == 0 || value > math.MaxInt64 {
		return 0, false
	}
	return int64(value), true
}

func validBASSourceRef(value *tammyv1.SourceRef, resource, id string) bool {
	if value == nil || value.Type != resource || !ids.IsCanonicalV7(value.Id) || value.Revision == 0 ||
		value.Revision > math.MaxInt64 || len(value.ContentHash) != sha256.Size {
		return false
	}
	return id == "" || value.Id == id
}

func sameBASSourceRef(left, right *tammyv1.SourceRef) bool {
	return left != nil && right != nil && left.Type == right.Type && left.Id == right.Id &&
		left.Revision == right.Revision && len(left.ContentHash) == sha256.Size && len(right.ContentHash) == sha256.Size &&
		subtle.ConstantTimeCompare(left.ContentHash, right.ContentHash) == 1
}

func nilBASRepositoryExecutor(executor any) bool {
	if executor == nil {
		return true
	}
	value := reflect.ValueOf(executor)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func basNormalizedPageSize(size uint32) (int, bool) {
	if size == 0 {
		return 50, true
	}
	if size > basRepositoryPageSizeMaximum {
		return 0, false
	}
	return int(size), true
}

func basPageQueryHash(resource string, fields ...string) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("tammy-bas-keyset-v1\x00"))
	write := func(value string) {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	write(resource)
	for _, field := range fields {
		write(field)
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func basParsePositiveOrdinal(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == value
}

type basPageSnapshot struct {
	ordinal int64
	hash    []byte
}

func formatBASPageSnapshot(snapshot basPageSnapshot) string {
	return strconv.FormatInt(snapshot.ordinal, 10) + ":" + hex.EncodeToString(snapshot.hash)
}

func basParsePageSnapshot(value string) (basPageSnapshot, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return basPageSnapshot{}, false
	}
	ordinal, ok := basParsePositiveOrdinal(parts[0])
	if !ok {
		return basPageSnapshot{}, false
	}
	hash, err := hex.DecodeString(parts[1])
	if err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != parts[1] {
		return basPageSnapshot{}, false
	}
	return basPageSnapshot{ordinal: ordinal, hash: hash}, true
}

func basProfileSnapshot(ctx context.Context, executor app.CommandSQLExecutor, organisationID string) (int64, []byte, error) {
	maximum, err := basMaximumProfileOrdinal(ctx, executor, organisationID)
	if err != nil || maximum == 0 {
		return maximum, nil, err
	}
	record, found, err := loadProfileRecord(ctx, executor, `SELECT v.append_ordinal,v.profile_id,v.organisation_id,
		v.version,v.effective_start,v.effective_end,v.content_hash,
		CASE WHEN length(v.profile_proto)<=? THEN v.profile_proto ELSE NULL END,length(v.profile_proto),v.created_at,v.actor_user_id,v.previous_version,v.previous_hash
		FROM bas_profile_versions_v1 v WHERE v.organisation_id=? AND v.append_ordinal=?`,
		scalarSnapshotLimit, organisationID, maximum)
	if err != nil || !found || record.ordinal != maximum {
		return 0, nil, ErrProfileRepository
	}
	if _, err := openProfileRecord(record, nil); err != nil {
		return 0, nil, ErrProfileRepository
	}
	return maximum, bytes.Clone(record.hash), nil
}

var _ app.CommandSQLExecutor = (*sql.Tx)(nil)
