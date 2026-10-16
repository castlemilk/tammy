package purchases

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const maximumBASLegacyDecisionRecords = maximumPostingReportingPayloadSize / postingReportingRowOverhead
const maximumBASLegacySourceDecisions = int64(1000)

// BASLegacyDecisionSummary reserves complete decision records, not candidates,
// currentness or verified support/event bodies. It is not a content fingerprint.
// DecisionBytes charges 512 plus the selected identity/proto/hash cell bytes.
type BASLegacyDecisionSummary struct {
	OrganisationID             string
	RecordCount, DecisionBytes int64
}

// BASLegacyDecisionRead is canonical decision history only. References nested
// in Reviews still require current-owner and full financial/event verification.
type BASLegacyDecisionRead struct {
	Summary BASLegacyDecisionSummary
	Reviews []*tammyv1.LegacyPurchaseReview
}

func (r *Repository) SummarizeBASLegacyDecisions(ctx context.Context, organisationID string) (*BASLegacyDecisionSummary, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	rows, err := r.executor.QueryContext(ctx, `/* BAS legacy decision summary */ `+purchaseBASLegacyDecisionCTE()+
		`, bounded_rows AS (SELECT * FROM source_rows ORDER BY `+purchaseBASLegacyDecisionOrderSQL+` LIMIT ?)
		 SELECT count(*),coalesce(sum(decision_bytes*1.0),0),coalesce(max(invalid_decision),0),coalesce(max(source_record_count),0) FROM bounded_rows`,
		organisationID, maximumBASLegacyDecisionRecords+1)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	if rows == nil {
		return nil, ErrRepository
	}
	if !rows.Next() {
		return nil, errors.Join(ErrRepository, sql.ErrNoRows, rows.Err(), rows.Close())
	}
	summary := BASLegacyDecisionSummary{OrganisationID: organisationID}
	var charged float64
	var invalid, sourceCount int64
	if err := rows.Scan(&summary.RecordCount, &charged, &invalid, &sourceCount); err != nil {
		return nil, errors.Join(ErrRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrRepository, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close(), ctx.Err()); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if summary.RecordCount > maximumBASLegacyDecisionRecords {
		return nil, &SourceBoundError{Resource: "legacy decision records", Limit: maximumBASLegacyDecisionRecords}
	}
	if sourceCount > maximumBASLegacySourceDecisions {
		return nil, &SourceBoundError{Resource: "legacy decision history", Limit: maximumBASLegacySourceDecisions}
	}
	if charged < 0 || math.IsNaN(charged) || math.IsInf(charged, 0) || math.Trunc(charged) != charged {
		return nil, ErrRepository
	}
	if charged > float64(maximumPostingReportingPayloadSize) {
		return nil, &SourceBoundError{Resource: "legacy decision bytes", Limit: maximumPostingReportingPayloadSize}
	}
	if invalid != 0 || summary.RecordCount < 0 || sourceCount < 0 {
		return nil, ErrRepository
	}
	summary.DecisionBytes = int64(charged)
	return &summary, nil
}

// ReadBASLegacyDecisions reads an explicitly reserved decision body category.
// It does not weaken the physical-key API or establish a global pre-body fence.
func (r *Repository) ReadBASLegacyDecisions(ctx context.Context, organisationID string, expected BASLegacyDecisionSummary, maximumRecords, maximumDecisionBytes int64) (*BASLegacyDecisionRead, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || expected.OrganisationID != organisationID ||
		expected.RecordCount < 0 || expected.DecisionBytes < 0 || maximumRecords < 0 || maximumDecisionBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if maximumRecords > maximumBASLegacyDecisionRecords {
		maximumRecords = maximumBASLegacyDecisionRecords
	}
	if maximumDecisionBytes > maximumPostingReportingPayloadSize {
		maximumDecisionBytes = maximumPostingReportingPayloadSize
	}
	if expected.RecordCount > maximumRecords {
		return nil, &SourceBoundError{Resource: "legacy decision records", Limit: maximumRecords}
	}
	if expected.DecisionBytes > maximumDecisionBytes {
		return nil, &SourceBoundError{Resource: "legacy decision bytes", Limit: maximumDecisionBytes}
	}
	actual, err := r.SummarizeBASLegacyDecisions(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if *actual != expected {
		return nil, fmt.Errorf("%w: legacy decision reservation changed before read", ErrRepository)
	}
	rows, err := r.executor.QueryContext(ctx, `/* BAS legacy decision records */ `+purchaseBASLegacyDecisionCTE()+
		`, bounded_rows AS (SELECT * FROM source_rows ORDER BY `+purchaseBASLegacyDecisionOrderSQL+` LIMIT ?), guarded_rows AS (
		 SELECT *,invalid_decision=0 AND source_record_count<=1000
		   AND sum(decision_bytes) OVER (ORDER BY `+purchaseBASLegacyDecisionOrderSQL+` ROWS UNBOUNDED PRECEDING)<=?
		   AND row_number() OVER (ORDER BY `+purchaseBASLegacyDecisionOrderSQL+`)<=? AS admissible FROM bounded_rows
		) SELECT decision_bytes,invalid_decision,source_record_count,
		 CASE WHEN admissible THEN id END,CASE WHEN admissible THEN organisation_id END,
		 CASE WHEN admissible THEN source_kind END,CASE WHEN admissible THEN source_id END,
		 CASE WHEN admissible THEN accounting_version END,
		 CASE WHEN admissible THEN review_proto END AS review_proto,
		 CASE WHEN admissible THEN record_hash END
		 FROM guarded_rows ORDER BY guarded_rows.`+"source_kind,guarded_rows.source_id,guarded_rows.accounting_version,guarded_rows.id",
		organisationID, expected.RecordCount+1, expected.DecisionBytes, expected.RecordCount)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	if rows == nil {
		return nil, ErrRepository
	}
	result := &BASLegacyDecisionRead{Summary: expected, Reviews: make([]*tammyv1.LegacyPurchaseReview, 0)}
	var charged, previousVersion int64
	var previousKind, previousSource string
	seenIDs := make(map[string]struct{})
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		if int64(len(result.Reviews)) == expected.RecordCount {
			return nil, errors.Join(&SourceBoundError{Resource: "legacy decision records", Limit: expected.RecordCount}, rows.Close())
		}
		var rowBytes, invalid, sourceCount int64
		var recordID, organisation, kind, sourceID sql.NullString
		var version sql.NullInt64
		var content, hash []byte
		if err := rows.Scan(&rowBytes, &invalid, &sourceCount, &recordID, &organisation, &kind, &sourceID, &version, &content, &hash); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		if sourceCount > maximumBASLegacySourceDecisions {
			return nil, errors.Join(&SourceBoundError{Resource: "legacy decision history", Limit: maximumBASLegacySourceDecisions}, rows.Close())
		}
		if rowBytes > expected.DecisionBytes-charged {
			return nil, errors.Join(&SourceBoundError{Resource: "legacy decision bytes", Limit: expected.DecisionBytes}, rows.Close())
		}
		if invalid != 0 || sourceCount < 1 || rowBytes < postingReportingRowOverhead || !recordID.Valid || !organisation.Valid || organisation.String != organisationID ||
			!kind.Valid || !sourceID.Valid || !version.Valid || version.Int64 < 1 {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		if kind.String != previousKind || sourceID.String != previousSource {
			if previousKind != "" && (kind.String < previousKind || kind.String == previousKind && sourceID.String <= previousSource) {
				return nil, errors.Join(ErrRepository, rows.Close())
			}
			previousVersion = 0
		}
		if version.Int64 != previousVersion+1 {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		if _, duplicate := seenIDs[recordID.String]; duplicate {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		computedBytes := postingReportingRowOverhead + int64(len(recordID.String)+len(organisation.String)+len(kind.String)+len(sourceID.String)+len(content)+len(hash))
		if rowBytes != computedBytes {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		record := &tammyv1.LegacyPurchaseReview{}
		if proto.Unmarshal(content, record) != nil || !canonicalPurchaseBytes(record, content) || !verifyPurchaseHash(record, hash) || !bytes.Equal(record.SourceHash, hash) ||
			record.Id != recordID.String || !ids.IsCanonicalV7(record.Id) || record.OrganisationId != organisationID || record.SourceRevision != uint64(version.Int64) ||
			!ids.IsCanonicalV7(record.ActorUserId) || record.RecordedAt == nil || record.RecordedAt.CheckValid() != nil || ValidateLegacyReview(record.Review) != nil {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		storedKind, storedID, _, _, _, sourceErr := legacySourceScope(record.Review.Source)
		if sourceErr != nil || storedKind != kind.String || storedID != sourceID.String {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		seenIDs[record.Id] = struct{}{}
		previousKind, previousSource, previousVersion = kind.String, sourceID.String, version.Int64
		charged += rowBytes
		result.Reviews = append(result.Reviews, record)
	}
	if err := errors.Join(rows.Err(), rows.Close(), ctx.Err()); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if int64(len(result.Reviews)) != expected.RecordCount || charged != expected.DecisionBytes {
		return nil, fmt.Errorf("%w: legacy decision count or charge changed during read", ErrRepository)
	}
	for _, record := range result.Reviews {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		if err := r.verifyLegacyScalars(ctx, record); err != nil {
			return nil, errors.Join(err, ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return result, nil
}
