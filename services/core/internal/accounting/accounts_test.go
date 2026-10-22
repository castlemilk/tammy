package accounting_test

import (
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

const (
	accountOrganisationID = "018f0000-0000-7000-8000-000000000020"
	accountID             = "018f0000-0000-7000-8000-000000000030"
)

func TestAccountClassificationRequiresExpectedNormalBalance(t *testing.T) {
	for _, test := range []struct {
		typeValue tammyv1.AccountType
		normal    tammyv1.NormalBalance
	}{
		{tammyv1.AccountType_ACCOUNT_TYPE_ASSET, tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT},
		{tammyv1.AccountType_ACCOUNT_TYPE_LIABILITY, tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT},
		{tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT},
		{tammyv1.AccountType_ACCOUNT_TYPE_REVENUE, tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT},
		{tammyv1.AccountType_ACCOUNT_TYPE_OTHER_REVENUE, tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT},
		{tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT},
		{tammyv1.AccountType_ACCOUNT_TYPE_OTHER_EXPENSE, tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT},
	} {
		account := validAccount()
		account.Type = test.typeValue
		account.NormalBalance = test.normal
		if err := accounting.ValidateAccount(account); err != nil {
			t.Fatalf("ValidateAccount(%s/%s) = %v", test.typeValue, test.normal, err)
		}
		account.NormalBalance = oppositeNormal(test.normal)
		if err := accounting.ValidateAccount(account); !errors.Is(err, accounting.ErrInvalidAccount) {
			t.Fatalf("opposite normal error = %v", err)
		}
	}
}

func TestAccountLifecycleAndProtectedAccountsFailClosed(t *testing.T) {
	ordinary := validAccount()
	archived, err := accounting.TransitionAccountStatus(ordinary, tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED)
	if err != nil || archived.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED || archived.Version != 2 {
		t.Fatalf("archive = %#v, %v", archived, err)
	}
	reactivated, err := accounting.TransitionAccountStatus(archived, tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE)
	if err != nil || reactivated.Version != 3 {
		t.Fatalf("reactivate = %#v, %v", reactivated, err)
	}
	if err := accounting.ValidateManualPosting(archived); !errors.Is(err, accounting.ErrAccountNotPostable) {
		t.Fatalf("archived posting error = %v", err)
	}

	for _, designation := range []tammyv1.AccountDesignation{
		tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_SYSTEM,
		tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_CONTROL,
	} {
		protected := validAccount()
		protected.Designation = designation
		if _, err := accounting.TransitionAccountStatus(protected, tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED); !errors.Is(err, accounting.ErrProtectedAccount) {
			t.Fatalf("protected transition error = %v", err)
		}
		if err := accounting.ValidateManualPosting(protected); !errors.Is(err, accounting.ErrProtectedAccount) {
			t.Fatalf("protected posting error = %v", err)
		}
		changed := proto.Clone(protected).(*tammyv1.Account)
		changed.Name = "Repurposed"
		if err := accounting.ValidateAccountMutation(protected, changed); !errors.Is(err, accounting.ErrProtectedAccount) {
			t.Fatalf("protected mutation error = %v", err)
		}
	}
}

func validAccount() *tammyv1.Account {
	return &tammyv1.Account{Id: accountID, OrganisationId: accountOrganisationID, Version: 1,
		Code: "6100", Name: "Office expenses", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE,
		NormalBalance:          tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT,
		Status:                 tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation:            tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification:   "profit_and_loss.expenses",
		CashFlowClassification: "operating"}
}

func oppositeNormal(value tammyv1.NormalBalance) tammyv1.NormalBalance {
	if value == tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT {
		return tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT
	}
	return tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT
}
