package purchases

import (
	"context"
	"database/sql"
	"strings"
)

// Owner guards discover malformed identities only to reject them. Normal row
// selection, root/fallback decisions and payload charges keep their TEXT scopes.
type postingReportingOwnerGuard struct {
	invalid string
	args    []any
}

func purchaseReportingRootOwnerGuard(organisationID string, allDocuments bool) *postingReportingOwnerGuard {
	guard := &postingReportingOwnerGuard{
		invalid: `EXISTS(SELECT 1 FROM purchase_postings_v1 AS owner_posting
			WHERE typeof(owner_posting.organisation_id)='blob' AND length(owner_posting.organisation_id)=36
			AND owner_posting.organisation_id=CAST(? AS BLOB))`,
		args: []any{organisationID},
	}
	if allDocuments {
		guard.invalid += ` OR EXISTS(SELECT 1 FROM documents AS owner_document
			WHERE typeof(owner_document.organisation_id)='blob' AND length(owner_document.organisation_id)=36
			AND owner_document.organisation_id=CAST(? AS BLOB))`
		guard.args = append(guard.args, organisationID)
	}
	return purchaseReportingCombineOwnerGuards(guard, purchaseReportingModernFamilyReferenceGuard(organisationID, BASScalarPosting, false))
}

func (guard *postingReportingOwnerGuard) cte() string {
	return `WITH purchase_owner_guard AS MATERIALIZED (SELECT CASE WHEN (` + guard.invalid +
		`) THEN 1 ELSE 0 END AS invalid_owner) `
}

func purchaseReportingDocumentOwnerGuard(organisationID string, documentIDs []string) *postingReportingOwnerGuard {
	guard := purchaseReportingRootOwnerGuard(organisationID, false)
	guard.invalid += ` OR EXISTS(SELECT 1 FROM documents AS owner_document
		WHERE typeof(owner_document.organisation_id)='blob' AND length(owner_document.organisation_id)=36
		AND owner_document.organisation_id=CAST(? AS BLOB)
		AND owner_document.id IN (` + purchaseCoveragePlaceholders(len(documentIDs)) + `))`
	guard.args = append(guard.args, organisationID)
	guard.args = append(guard.args, batchArgs(documentIDs)...)
	return guard
}

// Preserve the original source order/limit, withhold every cell on rejection,
// and retain one invalid sentinel even when the TEXT selection is empty.
func postingReportingReadSQL(query postingReportingQuery) (string, []any) {
	columns := make([]string, len(query.columns))
	for index, column := range query.columns {
		columns[index] = query.alias + "." + column
	}
	if query.ownerGuard == nil {
		return "SELECT " + strings.Join(columns, ",") + " FROM " + query.from + " WHERE " + query.where +
			" ORDER BY " + query.orderBy + query.limit, query.args
	}
	projection := make([]string, len(query.columns))
	for index, column := range query.columns {
		columns[index] += " AS " + column
		projection[index] = "CASE WHEN owner_guard.invalid_owner=0 THEN owner_rows." + column + " END AS " + column
	}
	sqlText := query.ownerGuard.cte() + `, purchase_owner_rows AS (
		SELECT row_number() OVER (ORDER BY ` + query.orderBy + `) AS owner_ordinal,` + strings.Join(columns, ",") +
		` FROM ` + query.from + ` WHERE (` + query.where + `)
		AND (SELECT invalid_owner FROM purchase_owner_guard)=0 ORDER BY ` + query.orderBy + query.limit + `)
		/* BAS purchase owner body ` + query.table + ` */
		SELECT owner_guard.invalid_owner,` + strings.Join(projection, ",") + `
		FROM purchase_owner_guard AS owner_guard LEFT JOIN purchase_owner_rows AS owner_rows ON owner_guard.invalid_owner=0
		WHERE owner_guard.invalid_owner!=0 OR owner_rows.owner_ordinal IS NOT NULL ORDER BY owner_rows.owner_ordinal`
	return sqlText, append(append([]any(nil), query.ownerGuard.args...), query.args...)
}

func purchaseReportingOwnerCount(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, guard *postingReportingOwnerGuard, countSQL string, args ...any) (int64, error) {
	guardedSQL := guard.cte() + `SELECT CASE WHEN invalid_owner!=0 THEN -1 ELSE (` + countSQL +
		`) END FROM purchase_owner_guard`
	guardedArgs := append(append([]any(nil), guard.args...), args...)
	count, err := postingReportingCount(ctx, executor, guardedSQL, guardedArgs...)
	if err != nil {
		return 0, err
	}
	if count < 0 {
		return 0, ErrRepository
	}
	return count, nil
}
