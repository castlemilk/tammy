package accounting

import (
	"context"
	"errors"
	"strings"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// GetForReporting preserves the mature account decoder while binding its owner
// and withholding malformed or oversized cells in the same body statement.
// The caller retains transaction ownership and reserves aggregate account bytes.
func (repository *AccountRepository) GetForReporting(ctx context.Context, organisationID, accountID string) (*tammyv1.Account, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(accountID) {
		return nil, ErrAccountRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrAccountRepository, err)
	}
	rows, err := repository.executor.QueryContext(ctx, accountReportingReplaySQL, accountID, organisationID)
	if err != nil {
		return nil, errors.Join(ErrAccountRepository, ctx.Err())
	}
	if !rows.Next() {
		return nil, errors.Join(ErrAccountRepository, ErrAccountNotFound, rows.Err(), rows.Close(), ctx.Err())
	}
	account, scanErr := scanAccount(rows)
	extra := rows.Next()
	rowsErr, closeErr := rows.Err(), rows.Close()
	if scanErr != nil || extra || rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrAccountRepository, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrAccountRepository, err)
	}
	return account, nil
}

var accountReportingReplaySQL = func() string {
	columns := strings.Fields("id organisation_id version code name account_type subtype normal_balance status designation default_tax_code_id report_classification cash_flow_classification")
	invalid := []string{"typeof(version)!='integer'", "version<1"}
	projection := make([]string, 0, len(columns))
	for _, column := range columns {
		if column != "version" {
			condition := "typeof(" + column + ")!='text'"
			if column == "subtype" || column == "default_tax_code_id" {
				condition = column + " IS NOT NULL AND (" + condition + ")"
			}
			invalid = append(invalid, "("+condition+")")
		}
		projection = append(projection, "CASE WHEN invalid=0 THEN "+column+" END AS "+column)
	}
	invalid = append(invalid, taxFactReportingPayloadSizeSQL(columns...)+">8388608")
	return `/* BAS account reporting replay */ WITH account_row AS MATERIALIZED (
		SELECT ` + strings.Join(columns, ",") + `,CASE WHEN ` + strings.Join(invalid, " OR ") + ` THEN 1 ELSE 0 END AS invalid
		FROM accounts WHERE id=? AND organisation_id=?) SELECT ` + strings.Join(projection, ",") + ` FROM account_row`
}()
