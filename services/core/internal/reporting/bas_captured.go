package reporting

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

type BASDraftCapture struct {
	Workpaper *tammyv1.BasWorkpaper
	// Parallel to Workpaper.Sources. These are source versions discovered by the
	// finite source capture, not fetched again while saving.
	LegacyPurchaseVersions []uint64
}

func (repository *Repository) SaveCapturedBASDraft(ctx context.Context, operationKey string,
	capture BASDraftCapture) (*tammyv1.BasWorkpaper, error) {
	if repository == nil || repository.executor == nil || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(operationKey) || capture.Workpaper == nil {
		return nil, ErrRepository
	}
	w := proto.Clone(capture.Workpaper).(*tammyv1.BasWorkpaper)
	if !ids.IsCanonicalV7(w.Id) || !ids.IsCanonicalV7(w.OrganisationId) || w.Version != 1 ||
		w.Status != tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED ||
		!validDate(w.PeriodStart) || !validDate(w.PeriodEnd) ||
		civilDateString(w.PeriodStart) > civilDateString(w.PeriodEnd) ||
		w.CreatedAt == nil || !w.CreatedAt.IsValid() ||
		w.SalesG1 == nil || w.GstOnSales_1A == nil || w.GstCredits_1B == nil || w.NetGstPayable == nil ||
		len(w.SourceFingerprint) != sha256.Size || w.SourcesChanged ||
		len(capture.LegacyPurchaseVersions) != len(w.Sources) || !basResponseFits(w) || protovalidate.Validate(w) != nil {
		return nil, ErrRepository
	}
	for index, source := range w.Sources {
		if source == nil || !ids.IsCanonicalV7(source.DocumentId) ||
			capture.LegacyPurchaseVersions[index] == 0 ||
			source.DocumentDate == nil || source.Gross == nil || source.GstCredit == nil {
			return nil, ErrRepository
		}
	}
	for _, source := range w.SalesSources {
		if source == nil || !ids.IsCanonicalV7(source.InvoiceId) || source.SourceRevision == 0 ||
			len(source.SourceHash) != sha256.Size {
			return nil, ErrRepository
		}
	}
	if w.PurchaseSourceFormat != nil {
		if *w.PurchaseSourceFormat != tammyv1.BasPurchaseSourceFormat_BAS_PURCHASE_SOURCE_FORMAT_RECONCILED_V1 ||
			w.LegacyCandidateGst == nil {
			return nil, ErrRepository
		}
	} else if len(w.PurchaseSources) != 0 || len(w.PurchaseReviews) != 0 ||
		len(w.LegacyPurchaseSupport) != 0 || w.LegacyCandidateGst != nil {
		return nil, ErrRepository
	}
	if !basCapturedAmountsConserve(w) {
		return nil, ErrRepository
	}

	var operationCount int
	if err := querySingle(repository.executor, ctx,
		`SELECT count(*) FROM bas_workpapers WHERE operation_key=?`, []any{operationKey}, &operationCount); err != nil {
		return nil, ErrRepository
	}
	if operationCount != 0 {
		return nil, ErrConflict
	}
	warnings, err := json.Marshal(w.ReviewWarnings)
	if err != nil {
		return nil, ErrRepository
	}
	start, end := civilDateString(w.PeriodStart), civilDateString(w.PeriodEnd)
	createdAt := w.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano)
	if _, err := repository.executor.ExecContext(ctx, `
		INSERT INTO bas_workpapers(
			id,operation_key,organisation_id,version,period_start,period_end,status,
			sales_g1_minor,gst_on_sales_1a_minor,gst_credits_1b_minor,net_gst_payable_minor,
			created_at,gst_basis,source_fingerprint,review_warnings_json,excluded_cash_purchase_count
		) VALUES(?,?,?,1,?,?,'DRAFT_NOT_LODGED',?,?,?,?,?,?,?,?,?)`,
		w.Id, operationKey, w.OrganisationId, start, end, w.SalesG1.MinorUnits,
		w.GstOnSales_1A.MinorUnits, w.GstCredits_1B.MinorUnits, w.NetGstPayable.MinorUnits,
		createdAt, int32(w.GstBasis), w.SourceFingerprint, string(warnings),
		w.ExcludedCashPurchaseCount); err != nil {
		return nil, ErrRepository
	}
	for index, source := range w.Sources {
		if _, err := repository.executor.ExecContext(ctx, `
			INSERT INTO bas_workpaper_sources(
				workpaper_id,sequence,document_id,document_version,supplier_name,invoice_number,
				document_date,gross_minor,gst_credit_minor
			) VALUES(?,?,?,?,?,?,?,?,?)`, w.Id, index+1, source.DocumentId,
			capture.LegacyPurchaseVersions[index], source.SupplierName, source.InvoiceNumber,
			civilDateString(source.DocumentDate), source.Gross.MinorUnits, source.GstCredit.MinorUnits); err != nil {
			return nil, ErrRepository
		}
	}
	for index, source := range w.SalesSources {
		if _, err := repository.executor.ExecContext(ctx, `
			INSERT INTO bas_workpaper_sales_sources_v1(
				workpaper_id,sequence,invoice_id,invoice_number,customer_name,tax_date,gross_minor,gst_minor,
				payment_id,source_revision,source_hash
			) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, w.Id, index+1, source.InvoiceId, source.InvoiceNumber,
			source.CustomerName, civilDateString(source.TaxDate), source.Gross.MinorUnits, source.Gst.MinorUnits,
			source.PaymentId, source.SourceRevision, source.SourceHash); err != nil {
			return nil, ErrRepository
		}
	}
	if w.PurchaseSourceFormat != nil {
		if err := repository.savePurchaseSnapshot(ctx, w); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// The saver checks the same exact source sums as the ordinary capture path.
// A caller cannot persist totals that disagree with its retained source lines.
func basCapturedAmountsConserve(w *tammyv1.BasWorkpaper) bool {
	if w == nil || (w.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH && w.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) {
		return false
	}
	validAmount := func(m *tammyv1.Money) bool { return m != nil && m.CurrencyCode == "AUD" && m.MinorUnits >= 0 }
	var gross, salesGST, credit int64
	for _, source := range w.SalesSources {
		if source == nil || !validAmount(source.Gross) || !validAmount(source.Gst) ||
			!addBASAmount(&gross, source.Gross.MinorUnits) || !addBASAmount(&salesGST, source.Gst.MinorUnits) {
			return false
		}
	}
	if w.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH && len(w.Sources) != 0 {
		return false
	}
	for _, source := range w.Sources {
		if source == nil || !validAmount(source.Gross) || !validAmount(source.GstCredit) || !addBASAmount(&credit, source.GstCredit.MinorUnits) {
			return false
		}
	}
	for _, source := range w.PurchaseSources {
		if source == nil || !validAmount(source.ClaimedGst) || !addBASAmount(&credit, source.ClaimedGst.MinorUnits) {
			return false
		}
	}
	return validAmount(w.SalesG1) && validAmount(w.GstOnSales_1A) && validAmount(w.GstCredits_1B) &&
		w.NetGstPayable != nil && w.NetGstPayable.CurrencyCode == "AUD" &&
		w.SalesG1.MinorUnits == gross && w.GstOnSales_1A.MinorUnits == salesGST &&
		w.GstCredits_1B.MinorUnits == credit && w.NetGstPayable.MinorUnits == salesGST-credit
}

// Deterministic whole-body identity for the legacy workpaper resource. Its SQL
// rows are immutable, but the historical schema did not retain one aggregate
// SHA-256 field; the exact loader reconstructs the body and this digest commits
// that complete canonical protobuf.
func BASWorkpaperContentHash(value *tammyv1.BasWorkpaper) ([]byte, error) {
	if value == nil || !basResponseFits(value) || protovalidate.Validate(value) != nil {
		return nil, ErrRepository
	}
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(value)
	if err != nil {
		return nil, ErrRepository
	}
	digest := sha256.Sum256(wire)
	return append([]byte(nil), digest[:]...), nil
}

func (repository *Repository) GetExactBASDraft(ctx context.Context, organisationID, workpaperID string) (*tammyv1.BasWorkpaper, error) {
	if repository == nil || repository.executor == nil || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(workpaperID) {
		return nil, ErrRepository
	}
	return repository.getOneMode(ctx, `WHERE id = ? AND organisation_id = ?`, []any{workpaperID, organisationID}, false)
}
