package reporting

import (
	"context"
	"database/sql"
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

// GetBusinessIncomeSummary aggregates the complete ledger and immutable invoice
// sources. Receivables are a balance as of one date, not just this period's sales.
func (repository *Repository) GetBusinessIncomeSummary(ctx context.Context, organisationID string, periodStart, periodEnd, asOfDate *tammyv1.CivilDate) (*tammyv1.BusinessIncomeSummary, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || !validDate(periodStart) || !validDate(periodEnd) || !validDate(asOfDate) {
		return nil, ErrRepository
	}
	start, end, asOf := civilDateString(periodStart), civilDateString(periodEnd), civilDateString(asOfDate)
	if start > end {
		return nil, ErrRepository
	}
	value := &tammyv1.BusinessIncomeSummary{OrganisationId: organisationID, PeriodStart: proto.Clone(periodStart).(*tammyv1.CivilDate), PeriodEnd: proto.Clone(periodEnd).(*tammyv1.CivilDate), AsOfDate: proto.Clone(asOfDate).(*tammyv1.CivilDate), BusinessIncome: aud(0), InvoiceIncome: aud(0), PaymentsReceived: aud(0), Outstanding: aud(0), InvoiceGst: aud(0), UnpostedIncome: aud(0)}
	var basis int32
	if err := querySingle(repository.executor, ctx, `SELECT gst_basis FROM organisations WHERE id=?`, []any{organisationID}, &basis); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil || basis < 0 || basis > 2 {
		return nil, ErrRepository
	}
	value.GstBasis = tammyv1.GstBasis(basis)
	if err := querySingle(repository.executor, ctx, `SELECT coalesce(sum(l.credit_minor-l.debit_minor),0) FROM journal_lines l JOIN journals j ON j.id=l.journal_id JOIN accounts a ON a.id=l.account_id AND a.organisation_id=j.organisation_id WHERE j.organisation_id=? AND j.state IN('POSTED','REVERSED') AND j.journal_date BETWEEN ? AND ? AND a.account_type IN('REVENUE','OTHER_REVENUE') AND a.designation='ORDINARY'`, []any{organisationID, start, end}, &value.BusinessIncome.MinorUnits); err != nil {
		return nil, ErrRepository
	}
	if err := querySingle(repository.executor, ctx, `SELECT coalesce(sum(invoice_net_minor),0),count(*) FROM invoice_income_postings_v1 WHERE organisation_id=? AND invoice_date BETWEEN ? AND ?`, []any{organisationID, start, end}, &value.InvoiceIncome.MinorUnits, &value.IssuedCount); err != nil {
		return nil, ErrRepository
	}
	if err := querySingle(repository.executor, ctx, `SELECT coalesce(sum(amount_minor),0) FROM invoice_payment_v1 WHERE organisation_id=? AND payment_date BETWEEN ? AND ?`, []any{organisationID, start, end}, &value.PaymentsReceived.MinorUnits); err != nil {
		return nil, ErrRepository
	}
	if err := querySingle(repository.executor, ctx, `SELECT coalesce(sum(p.invoice_total_minor-coalesce((SELECT sum(r.amount_minor) FROM invoice_payment_v1 r WHERE r.invoice_id=p.invoice_id AND r.organisation_id=p.organisation_id AND r.payment_date<=?),0)),0) FROM invoice_income_postings_v1 p WHERE p.organisation_id=? AND p.invoice_date<=?`, []any{asOf, organisationID, asOf}, &value.Outstanding.MinorUnits); err != nil || value.Outstanding.MinorUnits < 0 {
		return nil, ErrRepository
	}
	if err := querySingle(repository.executor, ctx, `SELECT coalesce((SELECT sum(invoice_gst_minor) FROM invoice_income_postings_v1 WHERE organisation_id=? AND gst_basis=2 AND invoice_date BETWEEN ? AND ?),0)+coalesce((SELECT sum(r.allocated_gst_minor) FROM invoice_payment_v1 r JOIN invoice_income_postings_v1 p ON p.invoice_id=r.invoice_id AND p.organisation_id=r.organisation_id WHERE r.organisation_id=? AND p.gst_basis=1 AND r.payment_date BETWEEN ? AND ?),0)`, []any{organisationID, start, end, organisationID, start, end}, &value.InvoiceGst.MinorUnits); err != nil {
		return nil, ErrRepository
	}
	count, unposted, err := repository.unpostedInvoiceTotals(ctx, organisationID, start, end, asOf)
	if err != nil {
		return nil, err
	}
	value.UnpostedIssuedCount = count
	value.UnpostedIncome.MinorUnits = unposted
	if err := querySingle(repository.executor, ctx, `SELECT financial_revision FROM financial_revisions WHERE id=1`, nil, &value.FinancialRevision); err != nil {
		return nil, ErrRepository
	}
	return value, nil
}
