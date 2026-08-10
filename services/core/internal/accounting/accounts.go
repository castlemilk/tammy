// Package accounting owns chart-of-accounts policy and ledger-facing ports.
package accounting

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var (
	ErrInvalidAccount     = errors.New("accounting: invalid account")
	ErrProtectedAccount   = errors.New("accounting: protected account")
	ErrAccountNotPostable = errors.New("accounting: account is not postable")
)

var accountCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,31}$`)

// ValidateAccount enforces stable classification and bounded chart metadata.
func ValidateAccount(account *tammyv1.Account) error {
	if account == nil || !ids.IsCanonicalV7(account.Id) || !ids.IsCanonicalV7(account.OrganisationId) ||
		account.Version == 0 || !accountCodePattern.MatchString(account.Code) ||
		!canonicalText(account.Name, 160) || len(account.GetSubtype()) > 96 ||
		!canonicalOptionalText(account.Subtype, 96) || !canonicalOptionalText(account.DefaultTaxCodeId, 64) ||
		!canonicalText(account.ReportClassification, 96) || !canonicalText(account.CashFlowClassification, 96) ||
		(account.DefaultTaxCodeId != nil && !ids.IsCanonicalV7(*account.DefaultTaxCodeId)) ||
		(account.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE && account.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED) ||
		(account.Designation < tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY ||
			account.Designation > tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL) || !validNormalBalance(account) {
		return ErrInvalidAccount
	}
	return nil
}

// ValidateAccountMutation rejects classification/ownership changes and makes
// installed system/control accounts wholly immutable.
func ValidateAccountMutation(before, after *tammyv1.Account) error {
	if ValidateAccount(before) != nil || ValidateAccount(after) != nil || before.Id != after.Id ||
		before.OrganisationId != after.OrganisationId || before.Type != after.Type ||
		before.NormalBalance != after.NormalBalance || before.Designation != after.Designation {
		return ErrInvalidAccount
	}
	if before.Designation != tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY && !proto.Equal(before, after) {
		return ErrProtectedAccount
	}
	return nil
}

// TransitionAccountStatus applies the only ordinary-account lifecycle edges.
func TransitionAccountStatus(account *tammyv1.Account, status tammyv1.AccountStatus) (*tammyv1.Account, error) {
	if ValidateAccount(account) != nil {
		return nil, ErrInvalidAccount
	}
	if account.Designation != tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY {
		return nil, ErrProtectedAccount
	}
	if status == account.Status || (status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE &&
		status != tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED) {
		return nil, ErrInvalidAccount
	}
	updated := proto.Clone(account).(*tammyv1.Account)
	updated.Status = status
	updated.Version++
	return updated, nil
}

// ValidateManualPosting rejects inactive and workflow-owned accounts.
func ValidateManualPosting(account *tammyv1.Account) error {
	if ValidateAccount(account) != nil {
		return ErrInvalidAccount
	}
	if account.Designation != tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY {
		return ErrProtectedAccount
	}
	if account.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE {
		return ErrAccountNotPostable
	}
	return nil
}

type TemplateAccount struct {
	Key                    string
	ID                     string
	Code                   string
	Name                   string
	Type                   tammyv1.AccountType
	NormalBalance          tammyv1.NormalBalance
	Designation            tammyv1.AccountDesignation
	ReportClassification   string
	CashFlowClassification string
}

type AccountTemplate struct {
	Version  string
	Accounts []TemplateAccount
}

type templateDocument struct {
	Version  string                    `json:"version"`
	Accounts []templateAccountDocument `json:"accounts"`
}

type templateAccountDocument struct {
	Key                    string `json:"key"`
	ID                     string `json:"id"`
	Code                   string `json:"code"`
	Name                   string `json:"name"`
	Type                   string `json:"type"`
	NormalBalance          string `json:"normal_balance"`
	Designation            string `json:"designation"`
	ReportClassification   string `json:"report_classification"`
	CashFlowClassification string `json:"cash_flow_classification"`
}

//go:embed templates/au_small_business_v1.json
var auSmallBusinessV1 []byte

// LoadAUSmallBusinessV1 returns a newly-owned validated template projection.
func LoadAUSmallBusinessV1() (AccountTemplate, error) {
	decoder := json.NewDecoder(strings.NewReader(string(auSmallBusinessV1)))
	decoder.DisallowUnknownFields()
	var document templateDocument
	if err := decoder.Decode(&document); err != nil {
		return AccountTemplate{}, ErrInvalidAccount
	}
	if err := ensureJSONEOF(decoder); err != nil || document.Version != "au_small_business_v1" || len(document.Accounts) == 0 {
		return AccountTemplate{}, ErrInvalidAccount
	}
	template := AccountTemplate{Version: document.Version, Accounts: make([]TemplateAccount, 0, len(document.Accounts))}
	seenKeys := make(map[string]struct{}, len(document.Accounts))
	seenCodes := make(map[string]struct{}, len(document.Accounts))
	for _, raw := range document.Accounts {
		account, ok := parseTemplateAccount(raw)
		if !ok {
			return AccountTemplate{}, ErrInvalidAccount
		}
		if _, duplicate := seenKeys[account.Key]; duplicate {
			return AccountTemplate{}, ErrInvalidAccount
		}
		if _, duplicate := seenCodes[account.Code]; duplicate {
			return AccountTemplate{}, ErrInvalidAccount
		}
		seenKeys[account.Key] = struct{}{}
		seenCodes[account.Code] = struct{}{}
		template.Accounts = append(template.Accounts, account)
	}
	return template, nil
}

func parseTemplateAccount(raw templateAccountDocument) (TemplateAccount, bool) {
	types := map[string]tammyv1.AccountType{
		"ASSET": tammyv1.AccountType_ACCOUNT_TYPE_ASSET, "LIABILITY": tammyv1.AccountType_ACCOUNT_TYPE_LIABILITY,
		"EQUITY": tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, "REVENUE": tammyv1.AccountType_ACCOUNT_TYPE_REVENUE,
		"OTHER_REVENUE": tammyv1.AccountType_ACCOUNT_TYPE_OTHER_REVENUE, "EXPENSE": tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE,
		"OTHER_EXPENSE": tammyv1.AccountType_ACCOUNT_TYPE_OTHER_EXPENSE, "CONTRA": tammyv1.AccountType_ACCOUNT_TYPE_CONTRA,
	}
	normals := map[string]tammyv1.NormalBalance{"DEBIT": tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
		"CREDIT": tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT}
	designations := map[string]tammyv1.AccountDesignation{"SYSTEM": tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_SYSTEM,
		"CONTROL": tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL}
	typeValue, typeOK := types[raw.Type]
	normal, normalOK := normals[raw.NormalBalance]
	designation, designationOK := designations[raw.Designation]
	account := TemplateAccount{Key: raw.Key, ID: raw.ID, Code: raw.Code, Name: raw.Name, Type: typeValue,
		NormalBalance: normal, Designation: designation, ReportClassification: raw.ReportClassification,
		CashFlowClassification: raw.CashFlowClassification}
	projection := &tammyv1.Account{Id: raw.ID, OrganisationId: "018f0000-0000-7000-8000-000000000020", Version: 1,
		Code: raw.Code, Name: raw.Name, Type: typeValue, NormalBalance: normal,
		Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: designation,
		ReportClassification: raw.ReportClassification, CashFlowClassification: raw.CashFlowClassification}
	return account, typeOK && normalOK && designationOK && canonicalText(raw.Key, 64) && ValidateAccount(projection) == nil
}

func validNormalBalance(account *tammyv1.Account) bool {
	switch account.Type {
	case tammyv1.AccountType_ACCOUNT_TYPE_ASSET,
		tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE,
		tammyv1.AccountType_ACCOUNT_TYPE_OTHER_EXPENSE:
		return account.NormalBalance == tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT
	case tammyv1.AccountType_ACCOUNT_TYPE_LIABILITY,
		tammyv1.AccountType_ACCOUNT_TYPE_EQUITY,
		tammyv1.AccountType_ACCOUNT_TYPE_REVENUE,
		tammyv1.AccountType_ACCOUNT_TYPE_OTHER_REVENUE:
		return account.NormalBalance == tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
	case tammyv1.AccountType_ACCOUNT_TYPE_CONTRA:
		return account.NormalBalance == tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT ||
			account.NormalBalance == tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
	default:
		return false
	}
}

func canonicalText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value
}

func canonicalOptionalText(value *string, maximum int) bool {
	return value == nil || len(*value) <= maximum && strings.TrimSpace(*value) == *value
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidAccount
	}
	return nil
}
