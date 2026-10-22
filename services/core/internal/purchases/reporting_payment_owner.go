package purchases

import (
	"database/sql"
	"strings"
)

// A matching BLOB owner is visible only to reject it; payment selection keeps
// its ordinary TEXT scope and its existing row and payload definitions.
func purchaseReportingPaymentOwnerGuard(organisationID string) *postingReportingOwnerGuard {
	guard := &postingReportingOwnerGuard{
		invalid: `EXISTS(SELECT 1 FROM purchase_payments_v1 AS owner_payment
			WHERE typeof(owner_payment.organisation_id)='blob' AND length(owner_payment.organisation_id)=36
			AND owner_payment.organisation_id=CAST(? AS BLOB))`,
		args: []any{organisationID},
	}
	return purchaseReportingCombineOwnerGuards(guard, purchaseReportingModernFamilyReferenceGuard(organisationID, BASScalarPayment, false))
}

func purchaseReportingPaymentReplayQuery(organisationID, postingID string) postingReportingQuery {
	return postingReportingQuery{
		table: "payment replay", alias: "p",
		columns: strings.Fields("id accounting_version payment_date amount_minor currency_code bank_account_id journal_id reference payment_proto source_hash actor_user_id created_at"),
		from:    "purchase_payments_v1 AS p", where: "p.posting_id=? AND p.organisation_id=?",
		args: []any{postingID, organisationID}, orderBy: "p.accounting_version,p.id",
		ownerGuard: purchaseReportingPaymentOwnerGuard(organisationID),
	}
}

type purchaseReportingOwnerRow struct{ rows *sql.Rows }

// Mature decoders consume their original destinations after the guard flag.
// The guarded SQL masks every body cell on rejection.
func (row purchaseReportingOwnerRow) Scan(destinations ...any) error {
	var invalidOwner int64
	values := append([]any{&invalidOwner}, destinations...)
	if err := row.rows.Scan(values...); err != nil || invalidOwner != 0 {
		return ErrRepository
	}
	return nil
}

func (r *Repository) reportingOwnerScanner(rows *sql.Rows) paymentScanner {
	if r.reportingRootOwners {
		return purchaseReportingOwnerRow{rows: rows}
	}
	return rows
}
