package purchases

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const purchaseTaxFactReuseProofMarker = "/* BAS purchase tax fact reuse proof */"

var purchaseTaxFactReuseColumns = strings.Fields(
	"id organisation_id journal_line_id tax_code treatment original_gross_minor original_net_minor original_gst_minor " +
		"attributed_gross_minor attributed_net_minor attributed_gst_minor remaining_gross_minor remaining_net_minor remaining_gst_minor " +
		"tax_rule_type tax_rule_id tax_rule_revision tax_rule_content_hash source_type source_id source_revision source_content_hash created_at",
)

func preflightPurchaseTaxFactReuse(
	ctx context.Context,
	executor interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	organisationID string,
	queries []postingReportingQuery,
	queryCounts, queryPayloadBytes []int64,
	expectedCount, expectedPayloadBytes int64,
) (BASPurchaseTaxFactReuseProof, error) {
	if ctx == nil || executor == nil || !ids.IsCanonicalV7(organisationID) ||
		len(queries) != len(queryCounts) || len(queries) != len(queryPayloadBytes) ||
		expectedCount < 0 || expectedCount > maximumPostingReportingPayloadSize/postingReportingRowOverhead ||
		expectedPayloadBytes < 0 || expectedPayloadBytes > maximumPostingReportingPayloadSize ||
		expectedPayloadBytes < expectedCount*postingReportingRowOverhead {
		return BASPurchaseTaxFactReuseProof{}, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return BASPurchaseTaxFactReuseProof{}, errors.Join(ErrRepository, err)
	}

	var reservedCount, reservedBytes int64
	for index, query := range queries {
		if query.table != "tax_facts" {
			continue
		}
		var ok bool
		reservedCount, ok = addPostingReportingBytes(reservedCount, queryCounts[index])
		if !ok {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
		reservedBytes, ok = addPostingReportingBytes(reservedBytes, queryPayloadBytes[index])
		if !ok || reservedBytes > maximumPostingReportingPayloadSize {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
	}
	if reservedCount != expectedCount || reservedBytes != expectedPayloadBytes {
		return BASPurchaseTaxFactReuseProof{}, ErrRepository
	}

	proof := BASPurchaseTaxFactReuseProof{}
	remainingRows := expectedCount
	for _, query := range queries {
		if query.table != "tax_facts" {
			continue
		}
		if query.alias != "f" || query.ownerGuard == nil || query.ownerGuard.invalid == "" ||
			query.from != "tax_facts AS f JOIN journal_lines AS l ON l.id=f.journal_line_id JOIN journals AS j ON j.id=l.journal_id" ||
			query.where == "" || query.orderBy != "j.id,l.line_number,f.id" || query.limit != "" ||
			len(query.columns) != len(purchaseTaxFactReuseColumns) {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
		for index, column := range purchaseTaxFactReuseColumns {
			if query.columns[index] != column {
				return BASPurchaseTaxFactReuseProof{}, ErrRepository
			}
		}
		if remainingRows < 0 {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}

		rowLimit := remainingRows + 1
		selectedColumns := make([]string, len(query.columns))
		comparisons := make([]string, len(query.columns))
		for index, column := range query.columns {
			selectedColumns[index] = "f." + column
			comparisons[index] = "p." + column + " IS NOT c." + column
		}
		payloadSize := postingReportingPayloadSizeSQL("p", query.columns)
		mismatch := "c.id IS NULL OR " + strings.Join(comparisons, " OR ")
		sqlText := query.ownerGuard.cte() + `,
			purchase_fact_rows AS MATERIALIZED (
			 SELECT ` + strings.Join(selectedColumns, ",") + `
			 FROM ` + query.from + `
			 WHERE (` + query.where + `)
			   AND (SELECT invalid_owner FROM purchase_owner_guard)=0
			 ORDER BY ` + query.orderBy + ` LIMIT ?
			),
			comparison_rows AS MATERIALIZED (
			 SELECT ` + payloadSize + ` AS payload_bytes,
			        CASE WHEN ` + mismatch + ` THEN 1 ELSE 0 END AS mismatch
			 FROM purchase_fact_rows AS p
			 LEFT JOIN tax_facts AS c
			   ON c.id=p.id
			  AND typeof(c.organisation_id)='text'
			  AND c.organisation_id=p.organisation_id
			)
			` + purchaseTaxFactReuseProofMarker + `
			SELECT count(*) AS fact_count,
			       coalesce(total(payload_bytes),0) AS payload_bytes,
			       coalesce(sum(CASE WHEN mismatch=0 THEN 1 ELSE 0 END),0) AS matched_fact_count,
			       coalesce(sum(mismatch),0) AS mismatch_count,
			       (SELECT invalid_owner FROM purchase_owner_guard) AS invalid_owner
			FROM comparison_rows`
		args := append([]any(nil), query.ownerGuard.args...)
		args = append(args, query.args...)
		args = append(args, rowLimit)
		rows, err := executor.QueryContext(ctx, sqlText, args...)
		if err != nil {
			return BASPurchaseTaxFactReuseProof{}, errors.Join(ErrRepository, err, ctx.Err())
		}
		if rows == nil {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}

		var count, matched, mismatches, invalidOwner int64
		var charged float64
		if !rows.Next() {
			readErr := errors.Join(sql.ErrNoRows, rows.Err())
			closeErr := rows.Close()
			return BASPurchaseTaxFactReuseProof{}, errors.Join(ErrRepository, readErr, closeErr, ctx.Err())
		}
		scanErr := rows.Scan(&count, &charged, &matched, &mismatches, &invalidOwner)
		extraRow := rows.Next()
		rowsErr := rows.Err()
		closeErr := rows.Close()
		if scanErr != nil || extraRow || rowsErr != nil || closeErr != nil {
			return BASPurchaseTaxFactReuseProof{}, errors.Join(ErrRepository, scanErr, rowsErr, closeErr, ctx.Err())
		}
		if err := ctx.Err(); err != nil {
			return BASPurchaseTaxFactReuseProof{}, errors.Join(ErrRepository, err)
		}

		if count < 0 || count > remainingRows || invalidOwner != 0 ||
			charged < 0 || math.IsNaN(charged) || math.IsInf(charged, 0) || math.Trunc(charged) != charged {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
		if charged > float64(maximumPostingReportingPayloadSize-proof.PayloadBytes) {
			return BASPurchaseTaxFactReuseProof{}, &SourceBoundError{
				Resource: "capture bytes", Limit: maximumPostingReportingPayloadSize,
			}
		}
		charge := int64(charged)
		if float64(charge) != charged || matched != count || mismatches != 0 ||
			charge > expectedPayloadBytes-proof.PayloadBytes {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}

		var ok bool
		proof.FactCount, ok = addPostingReportingBytes(proof.FactCount, count)
		if !ok {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
		proof.PayloadBytes, ok = addPostingReportingBytes(proof.PayloadBytes, charge)
		if !ok {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
		proof.MatchedFactCount, ok = addPostingReportingBytes(proof.MatchedFactCount, matched)
		if !ok {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
		proof.MismatchCount, ok = addPostingReportingBytes(proof.MismatchCount, mismatches)
		if !ok {
			return BASPurchaseTaxFactReuseProof{}, ErrRepository
		}
		remainingRows -= count
	}
	if proof.FactCount != expectedCount || proof.PayloadBytes != expectedPayloadBytes ||
		proof.MatchedFactCount != expectedCount || proof.MismatchCount != 0 {
		return BASPurchaseTaxFactReuseProof{}, ErrRepository
	}
	return proof, nil
}
