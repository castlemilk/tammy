package baspreparation

import (
	"bytes"
	"context"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// basExactRevisionByHash resolves one immutable revision from a scoped hash.
// It selects scalar metadata only; the existing exact reader then bounds and
// verifies the retained protobuf and all of its parents.
func basExactRevisionByHash(ctx context.Context, executor app.CommandSQLExecutor,
	query string, arguments ...any) (int64, bool, error) {
	if nilBASRepositoryExecutor(executor) || ctx == nil || ctx.Err() != nil {
		return 0, false, ErrSnapshotCorrupt
	}
	rows, err := executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return 0, false, err
	}
	if !rows.Next() {
		rowsErr, closeErr := rows.Err(), rows.Close()
		if rowsErr != nil || closeErr != nil {
			return 0, false, errors.Join(rowsErr, closeErr)
		}
		return 0, false, nil
	}
	var revision int64
	if err := rows.Scan(&revision); err != nil {
		_ = rows.Close()
		return 0, false, err
	}
	if rows.Next() {
		_ = rows.Close()
		return 0, false, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, false, err
	}
	if revision <= 0 {
		return 0, false, ErrSnapshotCorrupt
	}
	return revision, true, nil
}

// GetExactByHash reads the exact retained profile named by its scoped ID/hash.
// The query contract intentionally omits revision because content_hash binds it.
func (repository *ProfileRepository) GetExactByHash(ctx context.Context, organisationID, profileID string,
	contentHash []byte) (*tammyv1.BasObligationsProfile, bool, error) {
	if repository == nil || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(profileID) || len(contentHash) != 32 {
		return nil, false, ErrProfileRepository
	}
	revision, found, err := basExactRevisionByHash(ctx, repository.executor,
		`SELECT version FROM bas_profile_versions_v1
         WHERE organisation_id=? AND profile_id=? AND content_hash=? LIMIT 2`,
		organisationID, profileID, contentHash)
	if err != nil {
		return nil, false, errors.Join(ErrProfileRepository, err)
	}
	if !found {
		return nil, false, nil
	}
	ref := &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: profileID,
		Revision: uint64(revision), ContentHash: bytes.Clone(contentHash)}
	value, err := repository.GetExact(ctx, organisationID, ref)
	if err != nil {
		return nil, false, errors.Join(ErrProfileRepository, err)
	}
	return value, true, nil
}

func (repository *PreparationRepository) GetExactByHash(ctx context.Context, organisationID, preparationID string,
	contentHash []byte) (*PreparationRetainedSnapshot, bool, error) {
	if repository == nil || repository.reader == nil || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(preparationID) || len(contentHash) != 32 {
		return nil, false, ErrPreparationRepository
	}
	revision, found, err := basExactRevisionByHash(ctx, repository.reader.executor,
		`SELECT version FROM bas_preparation_versions_v1
         WHERE organisation_id=? AND preparation_id=? AND content_hash=? LIMIT 2`,
		organisationID, preparationID, contentHash)
	if err != nil {
		return nil, false, errors.Join(ErrPreparationRepository, err)
	}
	if !found {
		return nil, false, nil
	}
	ref := &tammyv1.SourceRef{Type: preparationSnapshotSpec.resource, Id: preparationID,
		Revision: uint64(revision), ContentHash: bytes.Clone(contentHash)}
	value, err := repository.GetExact(ctx, organisationID, ref)
	if err != nil {
		return nil, false, errors.Join(ErrPreparationRepository, err)
	}
	return value, true, nil
}

func (repository *CalculationRepository) GetExactByHash(ctx context.Context, organisationID, calculationID string,
	contentHash []byte) (*tammyv1.BasCalculation, bool, error) {
	if repository == nil || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(calculationID) || len(contentHash) != 32 {
		return nil, false, ErrCalculationRepository
	}
	revision, found, err := basExactRevisionByHash(ctx, repository.executor,
		`SELECT version FROM bas_calculations_v1
         WHERE organisation_id=? AND calculation_id=? AND content_hash=? LIMIT 2`,
		organisationID, calculationID, contentHash)
	if err != nil {
		return nil, false, errors.Join(ErrCalculationRepository, err)
	}
	if !found {
		return nil, false, nil
	}
	ref := &tammyv1.SourceRef{Type: calculationSnapshotSpec.resource, Id: calculationID,
		Revision: uint64(revision), ContentHash: bytes.Clone(contentHash)}
	value, err := repository.GetExact(ctx, organisationID, ref)
	if err != nil {
		return nil, false, errors.Join(ErrCalculationRepository, err)
	}
	return value, true, nil
}
