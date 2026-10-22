//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestAccountRepositoryInstallsTemplateAndEnforcesOptimisticUniqueChart(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	organisationRepository, _ := organisations.NewRepository(tx)
	if err := organisationRepository.Create(context.Background(), accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	repository, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	template, _ := accounting.LoadAUSmallBusinessV1()
	if err := repository.InstallTemplate(context.Background(), accountTestOrganisationID, template, now); err != nil {
		t.Fatal(err)
	}
	accounts, err := repository.List(context.Background(), accountTestOrganisationID, "", "", 200)
	if err != nil || len(accounts) != len(template.Accounts) {
		t.Fatalf("List() count = %d, %v; want %d", len(accounts), err, len(template.Accounts))
	}

	ordinary := &tammyv1.Account{Id: "018f0000-0000-7000-8000-000000000150", OrganisationId: accountTestOrganisationID,
		Version: 1, Code: "5000", Name: "Subscriptions", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation:          tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: "profit_loss.operating_expense", CashFlowClassification: "operating"}
	if err := repository.Create(context.Background(), ordinary, now); err != nil {
		t.Fatal(err)
	}
	duplicate := proto.Clone(ordinary).(*tammyv1.Account)
	duplicate.Id = "018f0000-0000-7000-8000-000000000151"
	if err := repository.Create(context.Background(), duplicate, now); !errors.Is(err, accounting.ErrAccountCodeConflict) {
		t.Fatalf("duplicate Create() error = %v; want code conflict", err)
	}
	archived, _ := accounting.TransitionAccountStatus(ordinary, tammyv1.AccountStatus_ACCOUNT_STATUS_ARCHIVED)
	if err := repository.Update(context.Background(), ordinary.Version, archived, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repository.Update(context.Background(), ordinary.Version, archived, now.Add(time.Minute)); !errors.Is(err, accounting.ErrAccountConflict) {
		t.Fatalf("stale Update() error = %v; want conflict", err)
	}
}

const accountTestOrganisationID = "018f0000-0000-7000-8000-000000000020"

func accountTestOrganisation() *tammyv1.Organisation {
	return &tammyv1.Organisation{Id: accountTestOrganisationID, Version: 1, Abn: "51824753556",
		LegalName: "Tammy Pty Ltd", DisplayName: "Tammy", EntityType: "AU_PRIVATE_COMPANY",
		GstBasis:              tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6,
		VerificationState:     tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		OwnerUserId:           "018f0000-0000-7000-8000-000000000010",
		ActiveTaxRuleBundle:   &tammyv1.SourceRef{Type: "rule_bundle", Id: "018f0000-0000-7000-8000-000000000030", Revision: 1, ContentHash: make([]byte, 32)}}
}
