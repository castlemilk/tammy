//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package banking_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/banking"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func TestImportStatementReplayRequiresEveryStoredLineToMatch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 6); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	organisationID := "018f0000-0000-7000-8000-000000000020"
	organisationRepository, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := organisationRepository.Create(ctx, bankingTestOrganisation(organisationID), time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	repository, err := banking.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}

	operationKey := "018f0000-0000-7000-8000-000000000101"
	lines := []*tammyv1.BankStatementLineInput{
		{
			TransactionDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 9},
			Description:     "Opening deposit",
			Amount:          &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 10000},
		},
		{
			TransactionDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
			Description:     "Office supplies",
			Amount:          &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: -31900},
		},
	}
	first, err := repository.ImportStatement(ctx, operationKey, organisationID,
		"018f0000-0000-7000-8000-000000000102",
		[]string{"018f0000-0000-7000-8000-000000000103", "018f0000-0000-7000-8000-000000000104"},
		100000, lines, time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.ImportStatement(ctx, operationKey, organisationID,
		"018f0000-0000-7000-8000-000000000105",
		[]string{"018f0000-0000-7000-8000-000000000106", "018f0000-0000-7000-8000-000000000107"},
		100000, proto.Clone(&tammyv1.ImportBankStatementRequest{Lines: lines}).(*tammyv1.ImportBankStatementRequest).Lines,
		time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC))
	if err != nil || !proto.Equal(replayed, first) {
		t.Fatalf("exact replay = %#v, %v; want owned logical result %#v", replayed, err, first)
	}
	if replayed == first || replayed.OpeningBalance == first.OpeningBalance {
		t.Fatal("exact replay aliases the first result")
	}

	tests := []struct {
		name   string
		mutate func(*tammyv1.BankStatementLineInput)
	}{
		{name: "date", mutate: func(line *tammyv1.BankStatementLineInput) { line.TransactionDate.Day = 8 }},
		{name: "description", mutate: func(line *tammyv1.BankStatementLineInput) { line.Description = "Different supplies" }},
		{name: "amount", mutate: func(line *tammyv1.BankStatementLineInput) { line.Amount.MinorUnits = -31800 }},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := proto.Clone(&tammyv1.ImportBankStatementRequest{Lines: lines}).(*tammyv1.ImportBankStatementRequest).Lines
			test.mutate(changed[1])
			_, err := repository.ImportStatement(ctx, operationKey, organisationID,
				"018f0000-0000-7000-8000-000000000108",
				[]string{"018f0000-0000-7000-8000-000000000109", "018f0000-0000-7000-8000-00000000010a"},
				100000, changed, time.Date(2026, 8, 10, 3+index, 0, 0, 0, time.UTC))
			if !errors.Is(err, banking.ErrConflict) {
				t.Fatalf("changed replay error = %v; want conflict", err)
			}
		})
	}
}

func bankingTestOrganisation(id string) *tammyv1.Organisation {
	return &tammyv1.Organisation{
		Id: id, Version: 1, Abn: "51824753556", LegalName: "Tammy Pty Ltd", DisplayName: "Tammy",
		EntityType: "AU_PRIVATE_COMPANY", GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6,
		VerificationState:     tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		OwnerUserId:           "018f0000-0000-7000-8000-000000000010",
		ActiveTaxRuleBundle: &tammyv1.SourceRef{
			Type: "rule_bundle", Id: "018f0000-0000-7000-8000-000000000030", Revision: 1, ContentHash: make([]byte, 32),
		},
	}
}
