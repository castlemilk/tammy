package reporting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

const maximumBASSources = 1000

// A source snapshot is independent of financial_revision. Creating a workpaper
// itself changes that revision, while its eligible immutable sources do not.
type basSources struct {
	basis                 tammyv1.GstBasis
	purchases             []*tammyv1.BasSourceLine
	purchaseVersions      []uint64
	sales                 []*tammyv1.BasSalesSourceLine
	warnings              []string
	excludedCashPurchases uint64
	fingerprint           []byte
	ownedPurchases        *purchases.BASSnapshot
}

func (repository *Repository) collectBASSources(ctx context.Context, organisationID, start, end string, asOf time.Time) (*basSources, error) {
	sources := &basSources{}
	var basis int32
	if err := querySingle(repository.executor, ctx, `SELECT gst_basis FROM organisations WHERE id=?`, []any{organisationID}, &basis); err != nil || basis < 1 || basis > 2 {
		return nil, ErrGSTBasis
	}
	sources.basis = tammyv1.GstBasis(basis)
	if _, err := invoices.CheckGSTBasisHistory(ctx, repository.executor, organisationID, 0); err != nil {
		return nil, ErrGSTBasis
	}
	var modern int
	if err := querySingle(repository.executor, ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='bas_workpaper_purchase_metadata_v1'`, nil, &modern); err != nil {
		return nil, ErrRepository
	}
	if modern == 1 {
		period := &tammyv1.PurchaseClaimPeriod{PeriodStart: parseCivilDate(start), PeriodEnd: parseCivilDate(end)}
		capture, err := repository.purchasePort.CaptureBAS(ctx, organisationID, period, period.PeriodEnd, asOf)
		if errors.Is(err, purchases.ErrSourceBound) {
			return nil, ErrSourceBound
		}
		if errors.Is(err, invoices.ErrGSTBasisHistory) {
			return nil, ErrGSTBasis
		}
		if err != nil || capture == nil {
			return nil, ErrRepository
		}
		sources.ownedPurchases = capture
		sources.excludedCashPurchases = capture.ExcludedCashPurchaseCount
		if len(capture.Reviews) > 0 {
			sources.warnings = append(sources.warnings, fmt.Sprintf("%d retained purchase review rows include legacy candidates, evidence suspense, exclusions or future claims. Review the complete typed purchase details before relying on this draft.", len(capture.Reviews)))
		}
	} else {
		rows, err := repository.executor.QueryContext(ctx, `SELECT id,version,supplier_name,invoice_number,document_date,total_minor,gst_minor FROM documents WHERE organisation_id=? AND status='REVIEWED' AND document_date BETWEEN ? AND ? ORDER BY document_date,id LIMIT 1001`, organisationID, start, end)
		if err != nil {
			return nil, ErrRepository
		}
		for rows.Next() {
			line := &tammyv1.BasSourceLine{Gross: aud(0), GstCredit: aud(0)}
			var date string
			var version uint64
			if err := rows.Scan(&line.DocumentId, &version, &line.SupplierName, &line.InvoiceNumber, &date, &line.Gross.MinorUnits, &line.GstCredit.MinorUnits); err != nil {
				rows.Close()
				return nil, ErrRepository
			}
			line.DocumentDate = parseCivilDate(date)
			if line.DocumentDate == nil || line.Gross.MinorUnits < 0 || line.GstCredit.MinorUnits < 0 || line.GstCredit.MinorUnits > line.Gross.MinorUnits {
				rows.Close()
				return nil, ErrRepository
			}
			sources.purchases = append(sources.purchases, line)
			sources.purchaseVersions = append(sources.purchaseVersions, version)
			if len(sources.purchases) > maximumBASSources {
				rows.Close()
				return nil, ErrSourceBound
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, ErrRepository
		}
		rows.Close()
		if sources.basis == tammyv1.GstBasis_GST_BASIS_CASH {
			sources.excludedCashPurchases = uint64(len(sources.purchases))
			// Retain these source identities in the fingerprint even though their credits
			// are excluded: newly reviewed receipts still change what needs review.
			sources.warnings = append(sources.warnings, fmt.Sprintf("Cash-basis purchase credits need payment evidence. %d reviewed expenses dated in this period are excluded because purchase payments are not recorded.", sources.excludedCashPurchases))
		}
	}
	sales, err := repository.invoiceBASSales(ctx, organisationID, start, end)
	if err != nil {
		return nil, err
	}
	sources.sales = sales
	unposted, _, err := repository.unpostedInvoiceTotals(ctx, organisationID, start, end, end)
	if err != nil {
		return nil, err
	}
	if unposted > 0 {
		sources.warnings = append(sources.warnings, fmt.Sprintf("%d issued invoices dated on or before the period end have not been added to accounts. Review them before relying on this workpaper.", unposted))
	}
	fingerprintView := &tammyv1.BasWorkpaper{OrganisationId: organisationID, PeriodStart: parseCivilDate(start), PeriodEnd: parseCivilDate(end), GstBasis: sources.basis, Sources: sources.purchases, SalesSources: sources.sales, ReviewWarnings: sources.warnings, ExcludedCashPurchaseCount: sources.excludedCashPurchases}
	if sources.ownedPurchases != nil {
		fingerprintView.PurchaseSources = sources.ownedPurchases.Sources
		fingerprintView.PurchaseReviews = sources.ownedPurchases.Reviews
		fingerprintView.LegacyPurchaseSupport = sources.ownedPurchases.LegacySupport
		fingerprintView.LegacyCandidateGst = sources.ownedPurchases.LegacyCandidateGST
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(fingerprintView)
	if err != nil {
		return nil, ErrRepository
	}
	digest := sha256.New()
	digest.Write(encoded)
	if sources.ownedPurchases != nil {
		digest.Write(sources.ownedPurchases.Fingerprint)
	}
	for _, version := range sources.purchaseVersions {
		var value [8]byte
		binary.BigEndian.PutUint64(value[:], version)
		digest.Write(value[:])
	}
	sources.fingerprint = digest.Sum(nil)
	return sources, nil
}

func (repository *Repository) invoiceBASSales(ctx context.Context, organisationID, start, end string) ([]*tammyv1.BasSalesSourceLine, error) {
	// Non-cash attribution is the issued document. Cash attribution is each actual
	// dated receipt and its immutable cumulative allocation. Neither list uses the
	// invoice browser's 200-record page limit.
	rows, err := repository.executor.QueryContext(ctx, `
 SELECT p.invoice_id,i.number,i.invoice_proto,p.invoice_date,p.bas_sales_gross_minor,p.invoice_gst_minor,NULL,p.invoice_version,p.source_hash,NULL
 FROM invoice_income_postings_v1 p JOIN invoices_v1 i ON i.id=p.invoice_id AND i.organisation_id=p.organisation_id
 WHERE p.organisation_id=? AND p.gst_basis=2 AND p.invoice_date BETWEEN ? AND ? AND (p.bas_sales_gross_minor>0 OR p.invoice_gst_minor>0)
 UNION ALL
 SELECT p.invoice_id,i.number,i.invoice_proto,r.payment_date,r.bas_sales_gross_minor,r.allocated_gst_minor,r.id,1,r.source_hash,r.payment_proto
 FROM invoice_payment_v1 r JOIN invoice_income_postings_v1 p ON p.invoice_id=r.invoice_id AND p.organisation_id=r.organisation_id JOIN invoices_v1 i ON i.id=p.invoice_id AND i.organisation_id=p.organisation_id
 WHERE p.organisation_id=? AND p.gst_basis=1 AND r.payment_date BETWEEN ? AND ? AND (r.bas_sales_gross_minor>0 OR r.allocated_gst_minor>0)
 ORDER BY 4,1,7 LIMIT 1001`, organisationID, start, end, organisationID, start, end)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	var result []*tammyv1.BasSalesSourceLine
	for rows.Next() {
		source := &tammyv1.BasSalesSourceLine{Gross: aud(0), Gst: aud(0)}
		var encoded, paymentEncoded []byte
		var date string
		var payment *string
		if err := rows.Scan(&source.InvoiceId, &source.InvoiceNumber, &encoded, &date, &source.Gross.MinorUnits, &source.Gst.MinorUnits, &payment, &source.SourceRevision, &source.SourceHash, &paymentEncoded); err != nil {
			return nil, ErrRepository
		}
		invoice := &tammyv1.Invoice{}
		if err := proto.Unmarshal(encoded, invoice); err != nil || invoice.Id != source.InvoiceId || invoice.OrganisationId != organisationID || invoice.Draft == nil {
			return nil, ErrRepository
		}
		if payment == nil {
			digest := sha256.Sum256(encoded)
			if !bytes.Equal(digest[:], source.SourceHash) || invoice.Version != source.SourceRevision {
				return nil, ErrRepository
			}
		} else {
			receipt := &tammyv1.InvoicePayment{}
			digest := sha256.Sum256(paymentEncoded)
			if proto.Unmarshal(paymentEncoded, receipt) != nil || !bytes.Equal(digest[:], source.SourceHash) || receipt.Id != *payment || receipt.InvoiceId != source.InvoiceId || receipt.OrganisationId != organisationID || receipt.PaymentDate == nil || civilDateString(receipt.PaymentDate) != date || receipt.GetBasSalesGross().GetMinorUnits() != source.Gross.MinorUnits || receipt.GetAllocatedGst().GetMinorUnits() != source.Gst.MinorUnits {
				return nil, ErrRepository
			}
		}
		source.CustomerName = invoice.Draft.CustomerName
		source.TaxDate = parseCivilDate(date)
		source.PaymentId = payment
		if source.TaxDate == nil || len(source.SourceHash) != 32 || source.Gross.MinorUnits < 0 || source.Gst.MinorUnits < 0 || source.Gst.MinorUnits > source.Gross.MinorUnits {
			return nil, ErrRepository
		}
		result = append(result, source)
		if len(result) > maximumBASSources {
			return nil, ErrSourceBound
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ErrRepository
	}
	return result, nil
}

func (repository *Repository) unpostedInvoiceTotals(ctx context.Context, organisationID, start, end, asOf string) (uint64, int64, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT i.id,i.invoice_proto FROM invoices_v1 i LEFT JOIN invoice_income_postings_v1 p ON p.invoice_id=i.id WHERE i.organisation_id=? AND i.state='ISSUED' AND p.invoice_id IS NULL ORDER BY i.id`, organisationID)
	if err != nil {
		return 0, 0, ErrRepository
	}
	defer rows.Close()
	var count uint64
	var income int64
	for rows.Next() {
		var id string
		var encoded []byte
		if err := rows.Scan(&id, &encoded); err != nil {
			return 0, 0, ErrRepository
		}
		invoice := &tammyv1.Invoice{}
		if err := proto.Unmarshal(encoded, invoice); err != nil || invoice.Id != id || invoice.OrganisationId != organisationID || invoice.Draft == nil || parseCivilDate(invoice.Draft.IssueDate) == nil || invoice.SubtotalMinor < 0 {
			return 0, 0, ErrRepository
		}
		if invoice.Draft.IssueDate <= asOf {
			count++
		}
		if invoice.Draft.IssueDate >= start && invoice.Draft.IssueDate <= end {
			if invoice.SubtotalMinor > math.MaxInt64-income {
				return 0, 0, ErrRepository
			}
			income += invoice.SubtotalMinor
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, ErrRepository
	}
	return count, income, nil
}
