package reporting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrRepository = errors.New("reporting: repository failure")
	ErrNotFound   = errors.New("reporting: BAS draft not found")
	ErrConflict   = errors.New("reporting: BAS draft conflict")
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
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, version, supplier_name, invoice_number, document_date, total_minor, gst_minor
		FROM documents
		WHERE organisation_id = ? AND status = 'REVIEWED' AND document_date BETWEEN ? AND ?
		ORDER BY document_date, id LIMIT 1000`, organisationID, start, end)
	if err != nil {
		return nil, ErrRepository
	}
	sources := make([]*tammyv1.BasSourceLine, 0)
	versions := make([]uint64, 0)
	for rows.Next() {
		line := &tammyv1.BasSourceLine{Gross: aud(0), GstCredit: aud(0)}
		var date string
		var version uint64
		if err := rows.Scan(&line.DocumentId, &version, &line.SupplierName, &line.InvoiceNumber, &date, &line.Gross.MinorUnits, &line.GstCredit.MinorUnits); err != nil {
			rows.Close()
			return nil, ErrRepository
		}
		line.DocumentDate = parseCivilDate(date)
		if line.DocumentDate == nil {
			rows.Close()
			return nil, ErrRepository
		}
		sources = append(sources, line)
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, ErrRepository
	}
	rows.Close()
	var credits int64
	for _, source := range sources {
		credits += source.GstCredit.MinorUnits
	}
	createdAt := now.UTC().Format(time.RFC3339Nano)
	if _, err := repository.executor.ExecContext(ctx, `
		INSERT INTO bas_workpapers(
			id, operation_key, organisation_id, version, period_start, period_end, status,
			sales_g1_minor, gst_on_sales_1a_minor, gst_credits_1b_minor, net_gst_payable_minor, created_at
		) VALUES (?, ?, ?, 1, ?, ?, 'DRAFT_NOT_LODGED', 0, 0, ?, ?, ?)`,
		workpaperID, operationKey, organisationID, start, end, credits, -credits, createdAt); err != nil {
		return nil, ErrRepository
	}
	for index, source := range sources {
		if _, err := repository.executor.ExecContext(ctx, `
			INSERT INTO bas_workpaper_sources(
				workpaper_id, sequence, document_id, document_version, supplier_name,
				invoice_number, document_date, gross_minor, gst_credit_minor
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, workpaperID, index+1, source.DocumentId,
			versions[index], source.SupplierName, source.InvoiceNumber, civilDateString(source.DocumentDate),
			source.Gross.MinorUnits, source.GstCredit.MinorUnits); err != nil {
			return nil, ErrRepository
		}
	}
	return &tammyv1.BasWorkpaper{
		Id: workpaperID, OrganisationId: organisationID, Version: 1,
		PeriodStart: periodStart, PeriodEnd: periodEnd,
		Status:  tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED,
		SalesG1: aud(0), GstOnSales_1A: aud(0), GstCredits_1B: aud(credits),
		NetGstPayable: aud(-credits), Sources: sources, CreatedAt: timestamppb.New(now),
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
	var start, end, createdAt, status string
	err := querySingle(repository.executor, ctx, `
		SELECT id, organisation_id, version, period_start, period_end, status,
		       sales_g1_minor, gst_on_sales_1a_minor, gst_credits_1b_minor,
		       net_gst_payable_minor, created_at FROM bas_workpapers `+predicate, []any{value},
		&workpaper.Id, &workpaper.OrganisationId, &workpaper.Version, &start, &end, &status,
		&workpaper.SalesG1.MinorUnits, &workpaper.GstOnSales_1A.MinorUnits,
		&workpaper.GstCredits_1B.MinorUnits, &workpaper.NetGstPayable.MinorUnits, &createdAt)
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
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT document_id, supplier_name, invoice_number, document_date, gross_minor, gst_credit_minor
		FROM bas_workpaper_sources WHERE workpaper_id = ? ORDER BY sequence`, workpaper.Id)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	for rows.Next() {
		line := &tammyv1.BasSourceLine{Gross: aud(0), GstCredit: aud(0)}
		var date string
		if err := rows.Scan(&line.DocumentId, &line.SupplierName, &line.InvoiceNumber, &date, &line.Gross.MinorUnits, &line.GstCredit.MinorUnits); err != nil {
			return nil, ErrRepository
		}
		line.DocumentDate = parseCivilDate(date)
		workpaper.Sources = append(workpaper.Sources, line)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrRepository
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
