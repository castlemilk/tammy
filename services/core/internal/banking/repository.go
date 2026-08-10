package banking

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
	ErrRepository = errors.New("banking: repository failure")
	ErrConflict   = errors.New("banking: state conflict")
	ErrNotFound   = errors.New("banking: record not found")
)

type Repository struct{ executor app.CommandSQLExecutor }

func NewRepository(executor app.CommandSQLExecutor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	return &Repository{executor: executor}, nil
}

func (repository *Repository) ImportStatement(
	ctx context.Context,
	operationKey, organisationID, importID string,
	lineIDs []string,
	openingBalance int64,
	lines []*tammyv1.BankStatementLineInput,
	now time.Time,
) (*tammyv1.BankStatementImport, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(operationKey) || !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(importID) ||
		len(lines) < 1 || len(lines) > 1000 || len(lineIDs) != len(lines) || now.IsZero() {
		return nil, ErrRepository
	}
	if existing, err := repository.getImportByOperation(ctx, operationKey); err == nil {
		if existing.OrganisationId == organisationID && existing.OpeningBalance.MinorUnits == openingBalance && int(existing.LineCount) == len(lines) {
			return existing, nil
		}
		return nil, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	closing := openingBalance
	for index, line := range lines {
		if !ids.IsCanonicalV7(lineIDs[index]) || !validLineInput(line) {
			return nil, ErrRepository
		}
		closing += line.Amount.MinorUnits
	}
	importedAt := now.UTC().Format(time.RFC3339Nano)
	if _, err := repository.executor.ExecContext(ctx, `
		INSERT INTO bank_statement_imports(
			id, operation_key, organisation_id, opening_balance_minor,
			closing_balance_minor, line_count, imported_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		importID, operationKey, organisationID, openingBalance, closing, len(lines), importedAt,
	); err != nil {
		return nil, fmt.Errorf("%w: insert import: %v", ErrRepository, err)
	}
	for index, line := range lines {
		if _, err := repository.executor.ExecContext(ctx, `
			INSERT INTO bank_statement_lines(
				id, statement_import_id, organisation_id, sequence, version,
				transaction_date, description, amount_minor, status, match_reference
			) VALUES (?, ?, ?, ?, 1, ?, ?, ?, 'UNMATCHED', '')`,
			lineIDs[index], importID, organisationID, index+1, civilDateString(line.TransactionDate),
			line.Description, line.Amount.MinorUnits,
		); err != nil {
			return nil, fmt.Errorf("%w: insert line: %v", ErrRepository, err)
		}
	}
	return &tammyv1.BankStatementImport{
		Id: importID, OrganisationId: organisationID,
		OpeningBalance: aud(openingBalance), ClosingBalance: aud(closing),
		LineCount: uint32(len(lines)), ImportedAt: timestamppb.New(now),
	}, nil
}

func (repository *Repository) ListLines(ctx context.Context, organisationID string, limit int) ([]*tammyv1.BankStatementLine, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || limit < 1 || limit > 200 {
		return nil, ErrRepository
	}
	rows, err := repository.executor.QueryContext(ctx, bankLineSelect+`
		WHERE organisation_id = ? ORDER BY transaction_date DESC, id DESC LIMIT ?`, organisationID, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: list lines: %v", ErrRepository, err)
	}
	defer rows.Close()
	result := make([]*tammyv1.BankStatementLine, 0, limit)
	for rows.Next() {
		line, scanErr := scanLine(rows)
		if scanErr != nil {
			return nil, ErrRepository
		}
		result = append(result, line)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrRepository
	}
	return result, nil
}

func (repository *Repository) MatchLine(ctx context.Context, lineID string, expectedVersion uint64, reference string) (*tammyv1.BankStatementLine, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(lineID) || expectedVersion == 0 || reference == "" || len(reference) > 128 {
		return nil, ErrRepository
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE bank_statement_lines SET version = version + 1, status = 'MATCHED', match_reference = ?
		WHERE id = ? AND version = ? AND status = 'UNMATCHED'`, reference, lineID, expectedVersion)
	if err != nil {
		return nil, ErrRepository
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, ErrConflict
	}
	return repository.getLine(ctx, lineID)
}

func (repository *Repository) CompleteReconciliation(ctx context.Context, operationKey, reconciliationID, organisationID string, now time.Time) (uint32, int64, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(operationKey) ||
		!ids.IsCanonicalV7(reconciliationID) || !ids.IsCanonicalV7(organisationID) || now.IsZero() {
		return 0, 0, ErrRepository
	}
	var importID string
	var closing int64
	if err := querySingle(repository.executor, ctx, `
		SELECT id, closing_balance_minor FROM bank_statement_imports
		WHERE organisation_id = ? ORDER BY imported_at DESC, id DESC LIMIT 1`, []any{organisationID}, &importID, &closing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrNotFound
		}
		return 0, 0, ErrRepository
	}
	var unmatched, matched uint32
	if err := querySingle(repository.executor, ctx, `
		SELECT COALESCE(SUM(CASE WHEN status = 'UNMATCHED' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status = 'MATCHED' THEN 1 ELSE 0 END), 0)
		FROM bank_statement_lines WHERE statement_import_id = ?`, []any{importID}, &unmatched, &matched); err != nil {
		return 0, 0, ErrRepository
	}
	if unmatched != 0 || matched == 0 {
		return 0, 0, ErrConflict
	}
	if _, err := repository.executor.ExecContext(ctx, `
		INSERT INTO bank_reconciliations(id, operation_key, organisation_id, statement_import_id,
			reconciled_line_count, closing_balance_minor, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, reconciliationID, operationKey, organisationID, importID, matched, closing, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, 0, ErrConflict
	}
	if _, err := repository.executor.ExecContext(ctx, `
		UPDATE bank_statement_lines SET version = version + 1, status = 'RECONCILED'
		WHERE statement_import_id = ? AND status = 'MATCHED'`, importID); err != nil {
		return 0, 0, ErrRepository
	}
	return matched, closing, nil
}

func (repository *Repository) Summary(ctx context.Context, organisationID string) (uint32, uint32, uint32, int64, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return 0, 0, 0, 0, ErrRepository
	}
	var total, unmatched, unreconciled uint32
	if err := querySingle(repository.executor, ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'UNMATCHED' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status != 'RECONCILED' THEN 1 ELSE 0 END), 0)
		FROM bank_statement_lines WHERE organisation_id = ?`, []any{organisationID}, &total, &unmatched, &unreconciled); err != nil {
		return 0, 0, 0, 0, ErrRepository
	}
	var closing sql.NullInt64
	if err := querySingle(repository.executor, ctx, `
		SELECT closing_balance_minor FROM bank_statement_imports
		WHERE organisation_id = ? ORDER BY imported_at DESC, id DESC LIMIT 1`, []any{organisationID}, &closing); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, 0, ErrRepository
	}
	return total, unmatched, unreconciled, closing.Int64, nil
}

func (repository *Repository) getLine(ctx context.Context, lineID string) (*tammyv1.BankStatementLine, error) {
	rows, err := repository.executor.QueryContext(ctx, bankLineSelect+` WHERE id = ?`, lineID)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	line, err := scanLine(rows)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return line, err
}

func (repository *Repository) getImportByOperation(ctx context.Context, operationKey string) (*tammyv1.BankStatementImport, error) {
	var value tammyv1.BankStatementImport
	value.OpeningBalance, value.ClosingBalance = aud(0), aud(0)
	var importedAt string
	err := querySingle(repository.executor, ctx, `
		SELECT id, organisation_id, opening_balance_minor, closing_balance_minor, line_count, imported_at
		FROM bank_statement_imports WHERE operation_key = ?`, []any{operationKey},
		&value.Id, &value.OrganisationId, &value.OpeningBalance.MinorUnits, &value.ClosingBalance.MinorUnits, &value.LineCount, &importedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrRepository
	}
	parsed, err := time.Parse(time.RFC3339Nano, importedAt)
	if err != nil {
		return nil, ErrRepository
	}
	value.ImportedAt = timestamppb.New(parsed)
	return &value, nil
}

const bankLineSelect = `SELECT id, statement_import_id, organisation_id, version,
	transaction_date, description, amount_minor, status, match_reference FROM bank_statement_lines`

func scanLine(scanner interface{ Scan(...any) error }) (*tammyv1.BankStatementLine, error) {
	line := &tammyv1.BankStatementLine{Amount: aud(0)}
	var date, status string
	if err := scanner.Scan(&line.Id, &line.StatementImportId, &line.OrganisationId, &line.Version,
		&date, &line.Description, &line.Amount.MinorUnits, &status, &line.MatchReference); err != nil {
		return nil, err
	}
	line.TransactionDate = parseCivilDate(date)
	switch status {
	case "UNMATCHED":
		line.Status = tammyv1.BankStatementLineStatus_BANK_STATEMENT_LINE_STATUS_UNMATCHED
	case "MATCHED":
		line.Status = tammyv1.BankStatementLineStatus_BANK_STATEMENT_LINE_STATUS_MATCHED
	case "RECONCILED":
		line.Status = tammyv1.BankStatementLineStatus_BANK_STATEMENT_LINE_STATUS_RECONCILED
	default:
		return nil, ErrRepository
	}
	return line, nil
}

func validLineInput(line *tammyv1.BankStatementLineInput) bool {
	return line != nil && line.TransactionDate != nil && line.TransactionDate.Year > 0 &&
		line.TransactionDate.Month >= 1 && line.TransactionDate.Month <= 12 && line.TransactionDate.Day >= 1 && line.TransactionDate.Day <= 31 &&
		line.Description != "" && len(line.Description) <= 256 && line.Amount != nil && line.Amount.CurrencyCode == "AUD" && line.Amount.MinorUnits != 0
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
