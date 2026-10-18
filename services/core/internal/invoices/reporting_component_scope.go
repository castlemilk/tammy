package invoices

// Components have no owner column. Preserve the scalar census's global
// rejection sentinel for rows with no usable parent, without inferring owners.
const basInvoiceIncomeLineFromSQL = `invoice_income_lines_v1 AS income_line
 LEFT JOIN invoice_income_postings_v1 AS posting ON posting.invoice_id=income_line.invoice_id
 LEFT JOIN invoices_v1 AS invoice ON invoice.id=income_line.invoice_id
 LEFT JOIN organisations AS owner ON owner.id=posting.organisation_id`

const basInvoiceIncomeLineScopeSQL = `posting.organisation_id=target.organisation_id
 OR invoice.organisation_id=target.organisation_id
 OR (posting.invoice_id IS NULL AND invoice.id IS NULL)`

const basInvoiceIncomeLineInvalidLinkSQL = `owner.id IS NULL
 OR posting.invoice_id IS NULL OR invoice.id IS NULL OR income_line.invoice_id=''
 OR invoice.organisation_id!=posting.organisation_id
 OR typeof(income_line.sequence)!='integer' OR income_line.sequence<1`

const basInvoiceIncomeLineInvalidFullSQL = basInvoiceIncomeLineInvalidLinkSQL + `
 OR typeof(income_line.gross_minor)!='integer' OR typeof(income_line.net_minor)!='integer'
 OR typeof(income_line.gst_minor)!='integer' OR typeof(income_line.treatment)!='integer'`

const basInvoiceAllocationFromSQL = `invoice_payment_lines_v1 AS allocation
 LEFT JOIN invoice_payment_v1 AS receipt ON receipt.id=allocation.payment_id
 LEFT JOIN invoice_income_postings_v1 AS posting ON posting.invoice_id=receipt.invoice_id
 LEFT JOIN invoices_v1 AS invoice ON invoice.id=receipt.invoice_id
 LEFT JOIN organisations AS owner ON owner.id=receipt.organisation_id`

const basInvoiceAllocationScopeSQL = `receipt.organisation_id=target.organisation_id
 OR posting.organisation_id=target.organisation_id OR invoice.organisation_id=target.organisation_id
 OR receipt.id IS NULL`

const basInvoiceAllocationInvalidLinkSQL = `owner.id IS NULL OR receipt.id IS NULL
 OR posting.invoice_id IS NULL OR invoice.id IS NULL OR allocation.payment_id=''
 OR invoice.organisation_id!=receipt.organisation_id OR posting.organisation_id!=receipt.organisation_id
 OR typeof(allocation.sequence)!='integer' OR allocation.sequence<1`

const basInvoiceAllocationInvalidFullSQL = basInvoiceAllocationInvalidLinkSQL + `
 OR typeof(allocation.gross_minor)!='integer' OR typeof(allocation.gst_minor)!='integer'`
