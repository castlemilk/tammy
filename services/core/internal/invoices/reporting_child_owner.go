package invoices

import (
	"fmt"
	"strings"
)

// Exact BLOB owner matches enter rejection only; accepted owners stay TEXT.
const basInvoicePostingOwnerScopeSQL = `posting.organisation_id=target.organisation_id
 OR invoice.organisation_id=target.organisation_id
 OR (typeof(posting.organisation_id)='blob' AND length(posting.organisation_id)=36
     AND posting.organisation_id=CAST(target.organisation_id AS BLOB))`

const basInvoiceReceiptOwnerScopeSQL = `receipt.organisation_id=target.organisation_id
 OR invoice.organisation_id=target.organisation_id
 OR (typeof(receipt.organisation_id)='blob' AND length(receipt.organisation_id)=36
     AND receipt.organisation_id=CAST(target.organisation_id AS BLOB))`

const basInvoicePostingInvalidOwnerSQL = `typeof(posting.organisation_id)!='text'
 OR posting.organisation_id!=target.organisation_id
 OR invoice.id IS NULL OR typeof(invoice.organisation_id)!='text'
 OR invoice.organisation_id!=target.organisation_id`

const basInvoiceReceiptInvalidOwnerSQL = `typeof(receipt.organisation_id)!='text'
 OR receipt.organisation_id!=target.organisation_id
 OR invoice.id IS NULL OR typeof(invoice.organisation_id)!='text'
 OR invoice.organisation_id!=target.organisation_id
 OR posting.invoice_id IS NULL OR typeof(posting.organisation_id)!='text'
 OR posting.organisation_id!=target.organisation_id`

const basInvoicePostingOwnerFromSQL = `invoice_income_postings_v1 AS posting
 LEFT JOIN invoices_v1 AS invoice ON invoice.id=posting.invoice_id`

const basInvoiceReceiptOwnerFromSQL = `invoice_payment_v1 AS receipt
 LEFT JOIN invoice_income_postings_v1 AS posting ON posting.invoice_id=receipt.invoice_id
 LEFT JOIN invoices_v1 AS invoice ON invoice.id=receipt.invoice_id`

// Keep rejected rows visible, but withhold every selected cell before the
// driver copies it. Callers retain their original outer SELECT/order/limit.
func basReportingGuardedChildRowsSQL(alias, from, scope, invalid string, columns ...string) string {
	projection := make([]string, len(columns))
	for index, column := range columns {
		projection[index] = fmt.Sprintf("CASE WHEN NOT (%s) THEN %s.%s END AS %s", invalid, alias, column, column)
	}
	return `WITH requested AS (SELECT ? AS organisation_id), guarded AS (SELECT ` +
		strings.Join(projection, ",") + ` FROM ` + from +
		` CROSS JOIN requested AS target WHERE (` + scope + `)) `
}
