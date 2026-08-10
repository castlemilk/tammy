package accounting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var (
	ErrAccountNotFound     = errors.New("accounting: account not found")
	ErrAccountCodeConflict = errors.New("accounting: account code already exists")
	ErrAccountConflict     = errors.New("accounting: stale account version")
	ErrAccountRepository   = errors.New("accounting: account repository failure")
)

// AccountRepository is transaction-scoped and exposes no transaction terminal.
type AccountRepository struct{ executor app.CommandSQLExecutor }

func NewAccountRepository(executor app.CommandSQLExecutor) (*AccountRepository, error) {
	if executor == nil {
		return nil, ErrAccountRepository
	}
	return &AccountRepository{executor: executor}, nil
}

func (repository *AccountRepository) Create(ctx context.Context, account *tammyv1.Account, now time.Time) error {
	if repository == nil || repository.executor == nil || ctx == nil || ValidateAccount(account) != nil || now.IsZero() {
		return ErrAccountRepository
	}
	occupied, err := repository.codeOccupied(ctx, account.OrganisationId, account.Code)
	if err != nil {
		return err
	}
	if occupied {
		return ErrAccountCodeConflict
	}
	_, err = repository.executor.ExecContext(ctx, `
		INSERT INTO accounts(
			id, organisation_id, code, name, account_type, subtype,
			normal_balance, status, designation, default_tax_code_id,
			report_classification, cash_flow_classification, owner_module,
			version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ledger', ?, ?, NULL)`,
		account.Id, account.OrganisationId, account.Code, account.Name, accountTypeName(account.Type),
		nullableText(account.Subtype), normalBalanceName(account.NormalBalance), accountStatusName(account.Status),
		accountDesignationName(account.Designation), nullableText(account.DefaultTaxCodeId),
		account.ReportClassification, account.CashFlowClassification, account.Version,
		now.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("%w: insert account: %v", ErrAccountRepository, err)
	}
	return nil
}

func (repository *AccountRepository) Get(ctx context.Context, accountID string) (*tammyv1.Account, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(accountID) {
		return nil, ErrAccountRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, organisation_id, version, code, name, account_type, subtype,
		       normal_balance, status, designation, default_tax_code_id,
		       report_classification, cash_flow_classification
		FROM accounts WHERE id = ?`, accountID)
	if err != nil {
		return nil, fmt.Errorf("%w: query account: %v", ErrAccountRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("%w: read account: %v", ErrAccountRepository, err)
		}
		return nil, ErrAccountNotFound
	}
	account, err := scanAccount(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return nil, ErrAccountRepository
	}
	return account, nil
}

func (repository *AccountRepository) Update(
	ctx context.Context,
	expectedVersion uint64,
	account *tammyv1.Account,
	now time.Time,
) error {
	if repository == nil || repository.executor == nil || ctx == nil || account == nil ||
		expectedVersion == 0 || account.Version != expectedVersion+1 || ValidateAccount(account) != nil || now.IsZero() {
		return ErrAccountRepository
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE accounts SET code = ?, name = ?, subtype = ?, status = ?,
			default_tax_code_id = ?, report_classification = ?,
			cash_flow_classification = ?, version = ?, updated_at = ?
		WHERE id = ? AND organisation_id = ? AND version = ?`,
		account.Code, account.Name, nullableText(account.Subtype), accountStatusName(account.Status),
		nullableText(account.DefaultTaxCodeId), account.ReportClassification,
		account.CashFlowClassification, account.Version, now.UTC().Format(time.RFC3339Nano),
		account.Id, account.OrganisationId, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("%w: update account: %v", ErrAccountRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: count account update: %v", ErrAccountRepository, err)
	}
	if affected != 1 {
		return ErrAccountConflict
	}
	return nil
}

// List returns a bounded stable keyset page ordered by code then ID.
func (repository *AccountRepository) List(
	ctx context.Context,
	organisationID, afterCode, afterID string,
	limit int,
) ([]*tammyv1.Account, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		limit < 1 || limit > 200 || afterCode == "" != (afterID == "") {
		return nil, ErrAccountRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, organisation_id, version, code, name, account_type, subtype,
		       normal_balance, status, designation, default_tax_code_id,
		       report_classification, cash_flow_classification
		FROM accounts
		WHERE organisation_id = ? AND (? = '' OR code > ? OR (code = ? AND id > ?))
		ORDER BY code, id LIMIT ?`,
		organisationID, afterCode, afterCode, afterCode, afterID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: list accounts: %v", ErrAccountRepository, err)
	}
	defer rows.Close()
	accounts := make([]*tammyv1.Account, 0, limit)
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, ErrAccountRepository
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: finish account list: %v", ErrAccountRepository, err)
	}
	return accounts, nil
}

func (repository *AccountRepository) InstallTemplate(
	ctx context.Context,
	organisationID string,
	template AccountTemplate,
	now time.Time,
) error {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		template.Version != "au_small_business_v1" || len(template.Accounts) == 0 || now.IsZero() {
		return ErrAccountRepository
	}
	if _, err := repository.executor.ExecContext(ctx, `SAVEPOINT install_account_template`); err != nil {
		return fmt.Errorf("%w: begin template install: %v", ErrAccountRepository, err)
	}
	fail := func(cause error) error {
		_, rollbackErr := repository.executor.ExecContext(context.WithoutCancel(ctx), `ROLLBACK TO install_account_template`)
		_, releaseErr := repository.executor.ExecContext(context.WithoutCancel(ctx), `RELEASE install_account_template`)
		return errors.Join(cause, rollbackErr, releaseErr)
	}
	for _, templateAccount := range template.Accounts {
		account := &tammyv1.Account{Id: templateAccount.ID, OrganisationId: organisationID, Version: 1,
			Code: templateAccount.Code, Name: templateAccount.Name, Type: templateAccount.Type,
			NormalBalance: templateAccount.NormalBalance, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
			Designation: templateAccount.Designation, ReportClassification: templateAccount.ReportClassification,
			CashFlowClassification: templateAccount.CashFlowClassification}
		if err := repository.Create(ctx, account, now); err != nil {
			return fail(err)
		}
	}
	if _, err := repository.executor.ExecContext(ctx, `RELEASE install_account_template`); err != nil {
		return fail(fmt.Errorf("%w: commit template install: %v", ErrAccountRepository, err))
	}
	return nil
}

func (repository *AccountRepository) codeOccupied(ctx context.Context, organisationID, code string) (bool, error) {
	rows, err := repository.executor.QueryContext(ctx,
		`SELECT count(*) FROM accounts WHERE organisation_id = ? AND code = ?`, organisationID, code)
	if err != nil {
		return false, fmt.Errorf("%w: check account code: %v", ErrAccountRepository, err)
	}
	defer rows.Close()
	var count int
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
		return false, ErrAccountRepository
	}
	return count != 0, nil
}

func scanAccount(scanner interface{ Scan(...any) error }) (*tammyv1.Account, error) {
	account := &tammyv1.Account{}
	var accountType, normalBalance, status, designation string
	var subtype, defaultTaxCode sql.NullString
	if err := scanner.Scan(&account.Id, &account.OrganisationId, &account.Version, &account.Code, &account.Name,
		&accountType, &subtype, &normalBalance, &status, &designation, &defaultTaxCode,
		&account.ReportClassification, &account.CashFlowClassification); err != nil {
		return nil, err
	}
	account.Type = parseAccountType(accountType)
	account.NormalBalance = parseNormalBalance(normalBalance)
	account.Status = parseAccountStatus(status)
	account.Designation = parseAccountDesignation(designation)
	if subtype.Valid {
		account.Subtype = &subtype.String
	}
	if defaultTaxCode.Valid {
		account.DefaultTaxCodeId = &defaultTaxCode.String
	}
	if ValidateAccount(account) != nil {
		return nil, ErrAccountRepository
	}
	return account, nil
}

func nullableText(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func accountTypeName(value tammyv1.AccountType) string {
	return map[tammyv1.AccountType]string{
		tammyv1.AccountType_ACCOUNT_TYPE_ASSET: "ASSET", tammyv1.AccountType_ACCOUNT_TYPE_LIABILITY: "LIABILITY",
		tammyv1.AccountType_ACCOUNT_TYPE_EQUITY: "EQUITY", tammyv1.AccountType_ACCOUNT_TYPE_REVENUE: "REVENUE",
		tammyv1.AccountType_ACCOUNT_TYPE_OTHER_REVENUE: "OTHER_REVENUE", tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE: "EXPENSE",
		tammyv1.AccountType_ACCOUNT_TYPE_OTHER_EXPENSE: "OTHER_EXPENSE", tammyv1.AccountType_ACCOUNT_TYPE_CONTRA: "CONTRA",
	}[value]
}

func parseAccountType(value string) tammyv1.AccountType {
	for enum, name := range map[tammyv1.AccountType]string{
		tammyv1.AccountType_ACCOUNT_TYPE_ASSET: "ASSET", tammyv1.AccountType_ACCOUNT_TYPE_LIABILITY: "LIABILITY",
		tammyv1.AccountType_ACCOUNT_TYPE_EQUITY: "EQUITY", tammyv1.AccountType_ACCOUNT_TYPE_REVENUE: "REVENUE",
		tammyv1.AccountType_ACCOUNT_TYPE_OTHER_REVENUE: "OTHER_REVENUE", tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE: "EXPENSE",
		tammyv1.AccountType_ACCOUNT_TYPE_OTHER_EXPENSE: "OTHER_EXPENSE", tammyv1.AccountType_ACCOUNT_TYPE_CONTRA: "CONTRA",
	} {
		if name == value {
			return enum
		}
	}
	return tammyv1.AccountType_ACCOUNT_TYPE_UNSPECIFIED
}

func normalBalanceName(value tammyv1.NormalBalance) string {
	if value == tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT {
		return "DEBIT"
	}
	if value == tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT {
		return "CREDIT"
	}
	return ""
}

func parseNormalBalance(value string) tammyv1.NormalBalance {
	if value == "DEBIT" {
		return tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT
	}
	if value == "CREDIT" {
		return tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
	}
	return tammyv1.NormalBalance_NORMAL_BALANCE_UNSPECIFIED
}

func accountStatusName(value tammyv1.AccountStatus) string {
	if value == tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE {
		return "ACTIVE"
	}
	if value == tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED {
		return "ARCHIVED"
	}
	return ""
}

func parseAccountStatus(value string) tammyv1.AccountStatus {
	if value == "ACTIVE" {
		return tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE
	}
	if value == "ARCHIVED" {
		return tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED
	}
	return tammyv1.AccountStatus_ACCOUNT_STATUS_UNSPECIFIED
}

func accountDesignationName(value tammyv1.AccountDesignation) string {
	switch value {
	case tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY:
		return "ORDINARY"
	case tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_SYSTEM:
		return "SYSTEM"
	case tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL:
		return "CONTROL"
	default:
		return ""
	}
}

func parseAccountDesignation(value string) tammyv1.AccountDesignation {
	switch value {
	case "ORDINARY":
		return tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY
	case "SYSTEM":
		return tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_SYSTEM
	case "CONTROL":
		return tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL
	default:
		return tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_UNSPECIFIED
	}
}
