package baspreparation

import (
	"bytes"
	"context"
	"errors"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

// ErrProfileNotEffective means the organisation has no complete profile assertion
// covering the requested civil date or period. It is a normal unsupported-period
// outcome, distinct from corrupt retained data.
var ErrProfileNotEffective = errors.New("baspreparation: no effective profile covers the requested period")

// GetEffectiveForPeriod resolves the exact latest profile assertion that covers
// the whole confirmed period. A later profile start logically closes an older
// open-ended assertion for selection; the immutable older row is never rewritten.
// Exact same-interval corrections select the latest revision. An explicit closed
// end remains closed, so a later gap cannot fall back to an older open-ended row.
func (repository *ProfileRepository) GetEffectiveForPeriod(ctx context.Context, organisationID string,
	period *tammyv1.BasCivilDateRange) (*tammyv1.BasObligationsProfile, *tammyv1.SourceRef, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || period == nil {
		return nil, nil, ErrProfileRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, errors.Join(ErrProfileRepository, err)
	}
	start, startOK := basCivilDateSQLText(period.StartDate)
	end, endOK := basCivilDateSQLText(period.EndDate)
	if !startOK || !endOK || start > end {
		return nil, nil, ErrProfileRepository
	}
	current, _, err := repository.CurrentForOrganisation(ctx, organisationID)
	if err != nil {
		return nil, nil, errors.Join(err, ctx.Err())
	}
	if current == nil {
		return nil, nil, ErrProfileNotEffective
	}

	record, found, err := loadProfileRecord(ctx, repository.executor, `SELECT
        v.append_ordinal,v.profile_id,v.organisation_id,v.version,v.effective_start,v.effective_end,
        v.content_hash,CASE WHEN length(v.profile_proto)<=? THEN v.profile_proto ELSE NULL END,
        length(v.profile_proto),v.created_at,v.actor_user_id,v.previous_version,v.previous_hash
        FROM bas_profile_versions_v1 v
        WHERE v.profile_id=? AND v.organisation_id=? AND v.effective_start<=? AND v.effective_end>=?
          AND NOT EXISTS (
            SELECT 1 FROM bas_profile_versions_v1 later
            WHERE later.profile_id=v.profile_id AND later.organisation_id=v.organisation_id
              AND later.effective_start>v.effective_start AND later.effective_start<=?)
          AND NOT EXISTS (
            SELECT 1 FROM bas_profile_versions_v1 correction
            WHERE correction.profile_id=v.profile_id AND correction.organisation_id=v.organisation_id
              AND correction.effective_start=v.effective_start AND correction.effective_end=v.effective_end
              AND correction.version>v.version)
        ORDER BY v.effective_start DESC,v.version DESC LIMIT 2`,
		scalarSnapshotLimit, current.ProfileId, organisationID, start, end, end)
	if err != nil {
		return nil, nil, errors.Join(ErrProfileRepository, err, ctx.Err())
	}
	if !found {
		return nil, nil, ErrProfileNotEffective
	}
	exact := &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: record.profileID,
		Revision: uint64(record.version), ContentHash: bytes.Clone(record.hash)}
	value, err := openProfileRecord(record, exact)
	if err != nil {
		return nil, nil, errors.Join(ErrProfileRepository, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, errors.Join(ErrProfileRepository, err)
	}
	return value, exact, nil
}

// GetEffectiveAt resolves one civil-date point through the same interval chain
// used for full confirmed periods.
func (repository *ProfileRepository) GetEffectiveAt(ctx context.Context, organisationID string,
	date *tammyv1.CivilDate) (*tammyv1.BasObligationsProfile, *tammyv1.SourceRef, error) {
	if _, ok := basCivilDateSQLText(date); !ok {
		return nil, nil, ErrProfileRepository
	}
	return repository.GetEffectiveForPeriod(ctx, organisationID,
		&tammyv1.BasCivilDateRange{StartDate: proto.Clone(date).(*tammyv1.CivilDate),
			EndDate: proto.Clone(date).(*tammyv1.CivilDate)})
}

// validBASProfileIntervalSuccessor permits a correction only when both bounds
// are unchanged. A new span must start later than the current span; a finite
// predecessor must also have ended before that start. For an open-ended
// predecessor, the later start is its logical end-exclusive boundary during
// selection, while the retained predecessor row stays byte-for-byte immutable.
func validBASProfileIntervalSuccessor(previous, next *tammyv1.BasEffectiveInterval) bool {
	previousStart, previousEnd, previousOK := basProfileIntervalSQLDates(previous)
	nextStart, nextEnd, nextOK := basProfileIntervalSQLDates(next)
	if !previousOK || !nextOK || previousStart > previousEnd || nextStart > nextEnd {
		return false
	}
	if previousStart == nextStart && previousEnd == nextEnd {
		return true
	}
	if nextStart <= previousStart {
		return false
	}
	if previous.EndDate != nil && nextStart <= previousEnd {
		return false
	}
	return true
}

func basProfileIntervalCoversPeriod(interval *tammyv1.BasEffectiveInterval,
	period *tammyv1.BasCivilDateRange) bool {
	profileStart, profileEnd, profileOK := basProfileIntervalSQLDates(interval)
	periodStart, periodStartOK := basCivilDateSQLText(period.GetStartDate())
	periodEnd, periodEndOK := basCivilDateSQLText(period.GetEndDate())
	return profileOK && periodStartOK && periodEndOK && periodStart <= periodEnd &&
		profileStart <= periodStart && periodEnd <= profileEnd
}
