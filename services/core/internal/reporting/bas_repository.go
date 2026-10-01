package reporting

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrRepository  = errors.New("reporting: repository failure")
	ErrNotFound    = errors.New("reporting: BAS draft not found")
	ErrConflict    = errors.New("reporting: BAS draft conflict")
	ErrGSTBasis    = errors.New("reporting: GST basis history requires review")
	ErrSourceBound = errors.New("reporting: more than 1000 sources; choose a shorter reporting period")
)

type Repository struct{ executor app.CommandSQLExecutor }

func NewRepository(executor app.CommandSQLExecutor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	return &Repository{executor: executor}, nil
}

func (repository *Repository) CreateBASDraft(
	ctx context.Context,
	operationKey, workpaperID, organisationID string,
	periodStart, periodEnd *tammyv1.CivilDate,
	now time.Time,
) (*tammyv1.BasWorkpaper, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(operationKey) ||
		!ids.IsCanonicalV7(workpaperID) || !ids.IsCanonicalV7(organisationID) || !validDate(periodStart) || !validDate(periodEnd) || now.IsZero() {
		return nil, ErrRepository
	}
	start, end := civilDateString(periodStart), civilDateString(periodEnd)
	if start > end {
		return nil, ErrRepository
	}
	if existing, err := repository.getByOperation(ctx, operationKey); err == nil {
		if existing.OrganisationId == organisationID && civilDateString(existing.PeriodStart) == start && civilDateString(existing.PeriodEnd) == end {
			return existing, nil
		}
		return nil, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	captured, err := repository.collectBASSources(ctx, organisationID, start, end)
	if err != nil {
		return nil, err
	}
	var sales, gst, credits int64
	for _, source := range captured.sales {
		sales += source.Gross.MinorUnits
		gst += source.Gst.MinorUnits
	}
	purchases := captured.purchases
	if captured.basis == tammyv1.GstBasis_GST_BASIS_CASH {
		purchases = nil
	}
	for _, source := range purchases {
		credits += source.GstCredit.MinorUnits
	}
	warningsJSON, err := json.Marshal(captured.warnings)
	if err != nil {
		return nil, ErrRepository
	}
	createdAt := now.UTC().Format(time.RFC3339Nano)
	if _, err := repository.executor.ExecContext(ctx, `
		INSERT INTO bas_workpapers(
			id, operation_key, organisation_id, version, period_start, period_end, status,
			sales_g1_minor, gst_on_sales_1a_minor, gst_credits_1b_minor, net_gst_payable_minor, created_at,gst_basis,source_fingerprint,review_warnings_json,excluded_cash_purchase_count
		) VALUES (?, ?, ?, 1, ?, ?, 'DRAFT_NOT_LODGED', ?, ?, ?, ?, ?,?,?,?,?)`,
		workpaperID, operationKey, organisationID, start, end, sales, gst, credits, gst-credits, createdAt, int32(captured.basis), captured.fingerprint, string(warningsJSON), captured.excludedCashPurchases); err != nil {
		return nil, ErrRepository
	}
	for index, source := range purchases {
		if _, err := repository.executor.ExecContext(ctx, `
			INSERT INTO bas_workpaper_sources(
				workpaper_id, sequence, document_id, document_version, supplier_name,
				invoice_number, document_date, gross_minor, gst_credit_minor
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, workpaperID, index+1, source.DocumentId,
			captured.purchaseVersions[index], source.SupplierName, source.InvoiceNumber, civilDateString(source.DocumentDate),
			source.Gross.MinorUnits, source.GstCredit.MinorUnits); err != nil {
			return nil, ErrRepository
		}
	}
	for index, source := range captured.sales {
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_workpaper_sales_sources_v1(workpaper_id,sequence,invoice_id,invoice_number,customer_name,tax_date,gross_minor,gst_minor,payment_id,source_revision,source_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, workpaperID, index+1, source.InvoiceId, source.InvoiceNumber, source.CustomerName, civilDateString(source.TaxDate), source.Gross.MinorUnits, source.Gst.MinorUnits, source.PaymentId, source.SourceRevision, source.SourceHash); err != nil {
			return nil, ErrRepository
		}
	}
	return &tammyv1.BasWorkpaper{
		Id: workpaperID, OrganisationId: organisationID, Version: 1,
		PeriodStart: periodStart, PeriodEnd: periodEnd,
		Status:  tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED,
		SalesG1: aud(sales), GstOnSales_1A: aud(gst), GstCredits_1B: aud(credits),
		NetGstPayable: aud(gst - credits), Sources: purchases, CreatedAt: timestamppb.New(now),
		GstBasis: captured.basis, SalesSources: captured.sales, SourceFingerprint: captured.fingerprint, ReviewWarnings: captured.warnings, ExcludedCashPurchaseCount: captured.excludedCashPurchases,
	}, nil
}

func (repository *Repository) GetCurrentBASDraft(ctx context.Context, organisationID string) (*tammyv1.BasWorkpaper, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrRepository
	}
	return repository.getOne(ctx, `WHERE organisation_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, organisationID)
}

func (repository *Repository) getByOperation(ctx context.Context, operationKey string) (*tammyv1.BasWorkpaper, error) {
	return repository.getOne(ctx, `WHERE operation_key = ?`, operationKey)
}

func (repository *Repository) getOne(ctx context.Context, predicate string, value any) (*tammyv1.BasWorkpaper, error) {
	workpaper := &tammyv1.BasWorkpaper{
		SalesG1: aud(0), GstOnSales_1A: aud(0), GstCredits_1B: aud(0), NetGstPayable: aud(0),
		Status: tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED,
	}
	var start, end, createdAt, status, warningsJSON string
	var basis int32
	err := querySingle(repository.executor, ctx, `
		SELECT id, organisation_id, version, period_start, period_end, status,
		       sales_g1_minor, gst_on_sales_1a_minor, gst_credits_1b_minor,
		       net_gst_payable_minor, created_at,gst_basis,source_fingerprint,review_warnings_json,excluded_cash_purchase_count FROM bas_workpapers `+predicate, []any{value},
		&workpaper.Id, &workpaper.OrganisationId, &workpaper.Version, &start, &end, &status,
		&workpaper.SalesG1.MinorUnits, &workpaper.GstOnSales_1A.MinorUnits,
		&workpaper.GstCredits_1B.MinorUnits, &workpaper.NetGstPayable.MinorUnits, &createdAt, &basis, &workpaper.SourceFingerprint, &warningsJSON, &workpaper.ExcludedCashPurchaseCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil || status != "DRAFT_NOT_LODGED" {
		return nil, ErrRepository
	}
	workpaper.PeriodStart, workpaper.PeriodEnd = parseCivilDate(start), parseCivilDate(end)
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, ErrRepository
	}
	workpaper.CreatedAt = timestamppb.New(created)
	workpaper.GstBasis = tammyv1.GstBasis(basis)
	if workpaper.PeriodStart == nil || workpaper.PeriodEnd == nil || json.Unmarshal([]byte(warningsJSON), &workpaper.ReviewWarnings) != nil {
		return nil, ErrRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT document_id, supplier_name, invoice_number, document_date, gross_minor, gst_credit_minor
		FROM bas_workpaper_sources WHERE workpaper_id = ? ORDER BY sequence`, workpaper.Id)
	if err != nil {
		return nil, ErrRepository
	}
	for rows.Next() {
		line := &tammyv1.BasSourceLine{Gross: aud(0), GstCredit: aud(0)}
		var date string
		if err := rows.Scan(&line.DocumentId, &line.SupplierName, &line.InvoiceNumber, &date, &line.Gross.MinorUnits, &line.GstCredit.MinorUnits); err != nil {
			rows.Close()
			return nil, ErrRepository
		}
		line.DocumentDate = parseCivilDate(date)
		workpaper.Sources = append(workpaper.Sources, line)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, ErrRepository
	}
	rows.Close()
	salesRows, err := repository.executor.QueryContext(ctx, `SELECT invoice_id,invoice_number,customer_name,tax_date,gross_minor,gst_minor,payment_id,source_revision,source_hash FROM bas_workpaper_sales_sources_v1 WHERE workpaper_id=? ORDER BY sequence`, workpaper.Id)
	if err != nil {
		return nil, ErrRepository
	}
	for salesRows.Next() {
		source := &tammyv1.BasSalesSourceLine{Gross: aud(0), Gst: aud(0)}
		var date string
		if err := salesRows.Scan(&source.InvoiceId, &source.InvoiceNumber, &source.CustomerName, &date, &source.Gross.MinorUnits, &source.Gst.MinorUnits, &source.PaymentId, &source.SourceRevision, &source.SourceHash); err != nil {
			salesRows.Close()
			return nil, ErrRepository
		}
		source.TaxDate = parseCivilDate(date)
		if source.TaxDate == nil {
			salesRows.Close()
			return nil, ErrRepository
		}
		workpaper.SalesSources = append(workpaper.SalesSources, source)
	}
	if err := salesRows.Err(); err != nil {
		salesRows.Close()
		return nil, ErrRepository
	}
	salesRows.Close()
	current, err := repository.collectBASSources(ctx, workpaper.OrganisationId, start, end)
	if errors.Is(err, ErrGSTBasis) {
		workpaper.SourcesChanged = true
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings, "GST basis history needs review before regenerating this workpaper. Saved figures have been retained.")
	} else if errors.Is(err, ErrSourceBound) {
		workpaper.SourcesChanged = true
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings, "The current source count exceeds 1000. Choose a shorter period when regenerating this workpaper.")
	} else if err != nil {
		return nil, err
	} else {
		workpaper.SourcesChanged = len(workpaper.SourceFingerprint) != 32 || !bytes.Equal(workpaper.SourceFingerprint, current.fingerprint)
	}
	if len(workpaper.SourceFingerprint) == 0 {
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings, "This older workpaper has no complete sales fingerprint. Regenerate it to include linked invoice income.")
	}
	return workpaper, nil
}

func validDate(value *tammyv1.CivilDate) bool {
	if value == nil || value.Year < 1 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return false
	}
	_, err := time.Parse("2006-01-02", civilDateString(value))
	return err == nil
}

func civilDateString(value *tammyv1.CivilDate) string {
	return fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
}

func parseCivilDate(value string) *tammyv1.CivilDate {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	return &tammyv1.CivilDate{Year: int32(parsed.Year()), Month: int32(parsed.Month()), Day: int32(parsed.Day())}
}

func aud(minor int64) *tammyv1.Money { return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor} }

func querySingle(executor app.CommandSQLExecutor, ctx context.Context, query string, args []any, destinations ...any) error {
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(destinations...); err != nil {
		return err
	}
	if rows.Next() {
		return ErrRepository
	}
	return rows.Err()
}
