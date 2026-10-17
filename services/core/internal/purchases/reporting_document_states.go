package purchases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const maximumBASDocumentStates = maximumPostingReportingPayloadSize / postingReportingRowOverhead

// BASDocumentStateSummary reserves complete scalar owner state, not candidates
// or verified source bodies. StateBytes charges selected cells plus 512 per row;
// it is neither a content fingerprint nor a Go heap-size measurement.
type BASDocumentStateSummary struct {
	OrganisationID            string
	DocumentCount, StateBytes int64
}

// BASDocumentState retains current owner cells only. ContentHash is the stored
// raw document SHA, not a verified byte/candidate hash. PostingID establishes
// owner identity without proving the posting body or its complete history.
type BASDocumentState struct {
	OrganisationID, ID string
	Version            uint64
	Status             tammyv1.DocumentStatus
	ContentHash        []byte
	PostingID          *string
}

type BASDocumentStateRead struct {
	Summary   BASDocumentStateSummary
	Documents []BASDocumentState
}

func (r *Repository) SummarizeBASDocumentStates(ctx context.Context, organisationID string) (*BASDocumentStateSummary, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	rows, err := r.executor.QueryContext(ctx, purchaseBASDocumentStateSummarySQL, organisationID, maximumBASDocumentStates+1)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	if rows == nil {
		return nil, ErrRepository
	}
	if !rows.Next() {
		return nil, errors.Join(ErrRepository, sql.ErrNoRows, rows.Err(), rows.Close())
	}
	summary := BASDocumentStateSummary{OrganisationID: organisationID}
	var charged float64
	var invalid, brokenLinks int64
	if err := rows.Scan(&summary.DocumentCount, &charged, &invalid, &brokenLinks); err != nil {
		return nil, errors.Join(ErrRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrRepository, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close(), ctx.Err()); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if summary.DocumentCount > maximumBASDocumentStates {
		return nil, &SourceBoundError{Resource: "document state records", Limit: maximumBASDocumentStates}
	}
	if charged < 0 || math.IsNaN(charged) || math.IsInf(charged, 0) || math.Trunc(charged) != charged {
		return nil, ErrRepository
	}
	if charged > float64(maximumPostingReportingPayloadSize) {
		return nil, &SourceBoundError{Resource: "document state bytes", Limit: maximumPostingReportingPayloadSize}
	}
	if invalid != 0 || brokenLinks != 0 || summary.DocumentCount < 0 {
		return nil, ErrRepository
	}
	summary.StateBytes = int64(charged)
	return &summary, nil
}

// ReadBASDocumentStates reads a separate scalar-state budget. It does not alter
// physical key discovery or establish currentness, eligibility or shared fences.
func (r *Repository) ReadBASDocumentStates(ctx context.Context, organisationID string, expected BASDocumentStateSummary, maximumDocuments, maximumStateBytes int64) (*BASDocumentStateRead, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || expected.OrganisationID != organisationID ||
		expected.DocumentCount < 0 || expected.StateBytes < 0 || maximumDocuments < 0 || maximumStateBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if maximumDocuments > maximumBASDocumentStates {
		maximumDocuments = maximumBASDocumentStates
	}
	if maximumStateBytes > maximumPostingReportingPayloadSize {
		maximumStateBytes = maximumPostingReportingPayloadSize
	}
	if expected.DocumentCount > maximumDocuments {
		return nil, &SourceBoundError{Resource: "document state records", Limit: maximumDocuments}
	}
	if expected.StateBytes > maximumStateBytes {
		return nil, &SourceBoundError{Resource: "document state bytes", Limit: maximumStateBytes}
	}
	actual, err := r.SummarizeBASDocumentStates(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if *actual != expected {
		return nil, fmt.Errorf("%w: document state reservation changed before read", ErrRepository)
	}
	rows, err := r.executor.QueryContext(ctx, purchaseBASDocumentStateReadSQL, organisationID, expected.DocumentCount+1, expected.StateBytes, expected.DocumentCount)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	if rows == nil {
		return nil, ErrRepository
	}
	result := &BASDocumentStateRead{Summary: expected, Documents: make([]BASDocumentState, 0)}
	var charged int64
	var previousID string
	headerSeen := false
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		var kind, ordinal, rowBytes, invalid, brokenLinks int64
		var id, org, status, posting sql.NullString
		var version sql.NullInt64
		var hash []byte
		if err := rows.Scan(&kind, &ordinal, &rowBytes, &invalid, &brokenLinks, &id, &org, &status, &version, &hash, &posting); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		if kind == 0 {
			if headerSeen || ordinal != 0 || rowBytes != 0 || invalid != 0 || brokenLinks != 0 ||
				id.Valid || org.Valid || status.Valid || version.Valid || len(hash) != 0 || posting.Valid {
				return nil, errors.Join(ErrRepository, rows.Close())
			}
			headerSeen = true
			continue
		}
		if !headerSeen || kind != 1 || ordinal != int64(len(result.Documents))+1 || brokenLinks != 0 {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		if int64(len(result.Documents)) == expected.DocumentCount {
			return nil, errors.Join(&SourceBoundError{Resource: "document state records", Limit: expected.DocumentCount}, rows.Close())
		}
		if rowBytes > expected.StateBytes-charged {
			return nil, errors.Join(&SourceBoundError{Resource: "document state bytes", Limit: expected.StateBytes}, rows.Close())
		}
		if invalid != 0 || rowBytes < postingReportingRowOverhead || !id.Valid || !ids.IsCanonicalV7(id.String) || id.String <= previousID ||
			!org.Valid || org.String != organisationID || !status.Valid || !version.Valid || version.Int64 < 1 || len(hash) != 32 ||
			(posting.Valid && !ids.IsCanonicalV7(posting.String)) {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		state := BASDocumentState{OrganisationID: org.String, ID: id.String, Version: uint64(version.Int64), ContentHash: hash}
		switch status.String {
		case "NEEDS_REVIEW":
			state.Status = tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW
		case "REVIEWED":
			state.Status = tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED
		default:
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		if posting.Valid {
			postingID := posting.String
			state.PostingID = &postingID
		}
		computedBytes := postingReportingRowOverhead + int64(len(id.String)+len(org.String)+len(status.String)+len(hash)+len(posting.String))
		if rowBytes != computedBytes {
			return nil, errors.Join(ErrRepository, rows.Close())
		}
		charged += rowBytes
		previousID = id.String
		result.Documents = append(result.Documents, state)
	}
	if err := errors.Join(rows.Err(), rows.Close(), ctx.Err()); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if !headerSeen || int64(len(result.Documents)) != expected.DocumentCount || charged != expected.StateBytes {
		return nil, fmt.Errorf("%w: document state count or charge changed during read", ErrRepository)
	}
	return result, nil
}
