package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var (
	ErrFamilyIdentityRepository = errors.New("baspreparation: family identity repository operation failed")
	ErrFamilyIdentityConflict   = errors.New("baspreparation: actual statement scope already has a retained family")
)

// FamilyIdentityRepository retains the immutable actual-period identity through
// a caller-owned transaction. Workflow versions and family-head CAS are separate.
type FamilyIdentityRepository struct {
	executor app.CommandSQLExecutor
}

func NewFamilyIdentityRepository(executor app.CommandSQLExecutor) (*FamilyIdentityRepository, error) {
	if nilBASRepositoryExecutor(executor) {
		return nil, ErrFamilyIdentityRepository
	}
	return &FamilyIdentityRepository{executor: executor}, nil
}

func (repository *FamilyIdentityRepository) Append(ctx context.Context, value *tammyv1.BasFamilyIdentity) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || value == nil {
		return nil, ErrFamilyIdentityRepository
	}
	wire, hash, err := sealFamilyIdentity(value)
	if err != nil {
		return nil, errors.Join(ErrFamilyIdentityRepository, err)
	}
	scope := FamilyScope{OrganisationID: value.OrganisationId, Start: value.PeriodScope.StartDate,
		End: value.PeriodScope.EndDate, Window: value.Window}
	start, end, window, ok := familyScopeSQL(scope)
	createdAt, validCreatedAt := basTimestampSQLText(value.CreatedAt)
	if !ok || !validCreatedAt {
		return nil, ErrFamilyIdentityRepository
	}
	// Verify any colliding identity rather than treating corrupt stored bytes as
	// harmless absence or attempting to replace them with a caller's new body.
	record, exists, err := loadFamilyIdentityRecord(ctx, repository.executor,
		familyIdentityRecordQuery+` WHERE family_id=?`, scalarSnapshotLimit, value.FamilyId)
	if err != nil {
		return nil, errors.Join(ErrFamilyIdentityRepository, err)
	}
	if exists {
		if _, err := openFamilyIdentityRecord(record, nil); err != nil {
			return nil, errors.Join(ErrFamilyIdentityRepository, err)
		}
		return nil, ErrFamilyIdentityConflict
	}
	if _, exists, err := repository.FindByScope(ctx, scope); err != nil {
		return nil, err
	} else if exists {
		return nil, ErrFamilyIdentityConflict
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_family_identities_v1(
		family_id,organisation_id,period_start,period_end,reporting_window,identity_hash,identity_proto,created_at)
		VALUES(?,?,?,?,?,?,?,?)`, value.FamilyId, value.OrganisationId, start, end, window, hash, wire, createdAt); err != nil {
		return nil, errors.Join(ErrFamilyIdentityRepository, err)
	}
	return &tammyv1.SourceRef{Type: familyIdentitySnapshotSpec.resource, Id: value.FamilyId,
		Revision: 1, ContentHash: bytes.Clone(hash)}, nil
}

// GetExact never resolves a mutable family head. Its revision must be one and
// its organization, immutable ID and hash must all match the retained identity.
func (repository *FamilyIdentityRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasFamilyIdentity, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) ||
		!validBASSourceRef(exact, familyIdentitySnapshotSpec.resource, "") || exact.Revision != 1 {
		return nil, ErrFamilyIdentityRepository
	}
	record, found, err := loadFamilyIdentityRecord(ctx, repository.executor,
		familyIdentityRecordQuery+` WHERE family_id=? AND organisation_id=?`, scalarSnapshotLimit, exact.Id, organisationID)
	if err != nil || !found {
		return nil, errors.Join(ErrFamilyIdentityRepository, err)
	}
	value, err := openFamilyIdentityRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrFamilyIdentityRepository, err)
	}
	return value, nil
}

// FindByScope uses only the actual civil-date/window scope. Profiles, rules,
// calculation versions and workflow metadata never enter this identity lookup.
func (repository *FamilyIdentityRepository) FindByScope(ctx context.Context, scope FamilyScope) (*tammyv1.BasFamilyIdentity, bool, error) {
	start, end, window, ok := familyScopeSQL(scope)
	if !repository.ready(ctx) || !ok {
		return nil, false, ErrFamilyIdentityRepository
	}
	record, found, err := loadFamilyIdentityRecord(ctx, repository.executor,
		familyIdentityRecordQuery+` WHERE organisation_id=? AND period_start=? AND period_end=? AND reporting_window=?`,
		scalarSnapshotLimit, scope.OrganisationID, start, end, window)
	if err != nil {
		return nil, false, errors.Join(ErrFamilyIdentityRepository, err)
	}
	if !found {
		return nil, false, nil
	}
	value, err := openFamilyIdentityRecord(record, nil)
	if err != nil {
		return nil, false, errors.Join(ErrFamilyIdentityRepository, err)
	}
	return value, true, nil
}

func (repository *FamilyIdentityRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && ctx != nil && ctx.Err() == nil
}

func familyScopeSQL(scope FamilyScope) (string, string, string, bool) {
	if !validScope(scope) {
		return "", "", "", false
	}
	start, validStart := basCivilDateSQLText(scope.Start)
	end, validEnd := basCivilDateSQLText(scope.End)
	window, validWindow := basPeriodWindowSQL(scope.Window)
	return start, end, window, validStart && validEnd && validWindow
}

const familyIdentityRecordQuery = `SELECT family_id,organisation_id,period_start,period_end,reporting_window,
	identity_hash,CASE WHEN length(identity_proto)<=? THEN identity_proto ELSE NULL END,length(identity_proto),created_at
	FROM bas_family_identities_v1`

type familyIdentityRecord struct {
	id, organisationID, start, end, window, createdAt string
	hash, wire                                        []byte
	wireLength                                        int64
}

func loadFamilyIdentityRecord(ctx context.Context, executor app.CommandSQLExecutor, query string,
	arguments ...any) (familyIdentityRecord, bool, error) {
	rows, err := executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return familyIdentityRecord{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return familyIdentityRecord{}, false, errors.Join(rows.Err(), rows.Close())
	}
	var record familyIdentityRecord
	if err := rows.Scan(&record.id, &record.organisationID, &record.start, &record.end, &record.window,
		&record.hash, &record.wire, &record.wireLength, &record.createdAt); err != nil {
		return familyIdentityRecord{}, false, err
	}
	record.hash, record.wire = bytes.Clone(record.hash), bytes.Clone(record.wire)
	if rows.Next() {
		return familyIdentityRecord{}, false, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return familyIdentityRecord{}, false, err
	}
	return record, true, nil
}

func openFamilyIdentityRecord(record familyIdentityRecord, expected *tammyv1.SourceRef) (*tammyv1.BasFamilyIdentity, error) {
	if record.wireLength > scalarSnapshotLimit {
		return nil, ErrSnapshotBound
	}
	if record.wireLength <= 0 || int64(len(record.wire)) != record.wireLength || len(record.hash) != sha256.Size {
		return nil, ErrSnapshotCorrupt
	}
	actual := &tammyv1.SourceRef{Type: familyIdentitySnapshotSpec.resource, Id: record.id, Revision: 1, ContentHash: record.hash}
	if expected != nil && !sameBASSourceRef(expected, actual) {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openFamilyIdentity(record.wire, record.organisationID, actual)
	if err != nil {
		return nil, err
	}
	start, end, window, ok := familyScopeSQL(FamilyScope{OrganisationID: value.OrganisationId,
		Start: value.PeriodScope.StartDate, End: value.PeriodScope.EndDate, Window: value.Window})
	createdAt, validCreatedAt := basTimestampSQLText(value.CreatedAt)
	if !ok || !validCreatedAt || start != record.start || end != record.end || window != record.window || createdAt != record.createdAt {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
