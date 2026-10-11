//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func TestJournalRepositoryRetainsTaxFactSourceHashAcrossReopenAndRollback(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	organisationRepository, _ := organisations.NewRepository(tx)
	if err := organisationRepository.Create(ctx, accountTestOrganisation(), now); err != nil {
		t.Fatal(err)
	}
	accounts := []*tammyv1.Account{
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000401", "1001", "Operating bank", "balance_sheet.cash"),
		postingRepositoryAccount("018f0000-0000-7000-8000-000000000402", "5001", "Office expenses", "profit_loss.operating_expense"),
	}
	accountRepository, _ := accounting.NewAccountRepository(tx)
	for _, account := range accounts {
		if err := accountRepository.Create(ctx, account, now); err != nil {
			t.Fatal(err)
		}
	}
	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}

	sourceHash := sha256.Sum256([]byte("manual evidence content v1"))
	ruleHash := sha256.Sum256([]byte("tax rule content v1"))
	if bytes.Equal(sourceHash[:], ruleHash[:]) {
		t.Fatal("fixture source and rule hashes must differ")
	}
	revision, err := journalRepository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil || revision != 1 {
		t.Fatalf("ReserveFinancialRevision() = %d, %v", revision, err)
	}
	posted := suppliedHashTaxPosting(t, accounts, now, revision,
		"018f0000-0000-7000-8000-000000000410",
		"018f0000-0000-7000-8000-000000000411",
		"018f0000-0000-7000-8000-000000000412",
		"018f0000-0000-7000-8000-000000000413",
		"018f0000-0000-7000-8000-000000000414",
		"018f0000-0000-7000-8000-000000000415", sourceHash[:], ruleHash[:])
	if err := journalRepository.Post(ctx, posted.journal, "MANUAL", posted.journal.Id, 1, posted.facts, posted.flows, now); err != nil {
		t.Fatalf("Post() = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatalf("reopen encrypted workspace: %v", err)
	}
	defer reopened.Close()
	var storedSourceHash, storedRuleHash []byte
	if err := reopened.QueryRowContext(ctx,
		`SELECT source_content_hash,tax_rule_content_hash FROM tax_facts WHERE id=?`, posted.factID,
	).Scan(&storedSourceHash, &storedRuleHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedSourceHash, sourceHash[:]) {
		t.Fatalf("persisted tax_facts.source_content_hash = %x, want caller hash %x", storedSourceHash, sourceHash)
	}
	if bytes.Equal(storedSourceHash, storedRuleHash) || !bytes.Equal(storedRuleHash, ruleHash[:]) {
		t.Fatalf("persisted tax hashes source=%x rule=%x; want distinct caller source and rule hashes", storedSourceHash, storedRuleHash)
	}

	rollbackTx, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rollbackRepository, err := accounting.NewJournalRepository(rollbackTx)
	if err != nil {
		t.Fatal(err)
	}
	rollbackRevision, err := rollbackRepository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil || rollbackRevision != 2 {
		t.Fatalf("rollback ReserveFinancialRevision() = %d, %v", rollbackRevision, err)
	}
	rollbackHash := sha256.Sum256([]byte("manual evidence content rollback"))
	rollbackRuleHash := sha256.Sum256([]byte("tax rule content rollback"))
	rolledBack := suppliedHashTaxPosting(t, accounts, now, rollbackRevision,
		"018f0000-0000-7000-8000-000000000420",
		"018f0000-0000-7000-8000-000000000421",
		"018f0000-0000-7000-8000-000000000422",
		"018f0000-0000-7000-8000-000000000423",
		"018f0000-0000-7000-8000-000000000424",
		"018f0000-0000-7000-8000-000000000425", rollbackHash[:], rollbackRuleHash[:])
	if err := rollbackRepository.Post(ctx, rolledBack.journal, "MANUAL", rolledBack.journal.Id, 1, rolledBack.facts, rolledBack.flows, now); err != nil {
		t.Fatalf("Post() in outer transaction = %v", err)
	}
	if err := rollbackTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var remainingFacts, remainingJournals int
	if err := reopened.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM tax_facts WHERE id=?),
		       (SELECT count(*) FROM journals WHERE id=?)`, rolledBack.factID, rolledBack.journal.Id,
	).Scan(&remainingFacts, &remainingJournals); err != nil {
		t.Fatal(err)
	}
	if remainingFacts != 0 || remainingJournals != 0 {
		t.Fatalf("outer rollback retained fact=%d journal=%d", remainingFacts, remainingJournals)
	}
}

type suppliedHashPosting struct {
	journal *tammyv1.Journal
	facts   map[string]accounting.TaxFact
	flows   map[string][]accounting.CashFlowComponent
	factID  string
}

func suppliedHashTaxPosting(
	t *testing.T,
	accounts []*tammyv1.Account,
	now time.Time,
	revision uint64,
	journalID, debitLineID, creditLineID, taxCodeID, factID, sourceID string,
	sourceHash, ruleHash []byte,
) suppliedHashPosting {
	t.Helper()
	journal := repositoryJournal(accounts, revision, now)
	journal.Id = journalID
	for index, line := range journal.Lines {
		line.JournalId = journalID
		if index == 0 {
			line.Id = debitLineID
			continue
		}
		line.Id = creditLineID
	}
	rule := &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: "018f0000-0000-7000-8000-000000000430", Revision: 1, ContentHash: ruleHash}
	journal.Lines[1].TaxCodeId = &taxCodeID
	journal.Lines[1].TaxAmount = &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 2900}
	journal.Lines[1].TaxRule = rule
	taxCode := &tammyv1.TaxCode{Id: taxCodeID, Code: "GST", Treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, Rule: rule}
	fact, err := accounting.BuildNonCashTaxFact(factID, accountTestOrganisationID, creditLineID, taxCode,
		29000, 2900,
		&tammyv1.SourceRef{Type: "manual_evidence", Id: sourceID, Revision: 1, ContentHash: sourceHash}, sourceID)
	if err != nil {
		t.Fatalf("BuildNonCashTaxFact() = %v", err)
	}
	return suppliedHashPosting{
		journal: journal,
		facts:   map[string]accounting.TaxFact{creditLineID: fact},
		flows: map[string][]accounting.CashFlowComponent{
			debitLineID:  {{Category: accounting.CashFlowOperating, AmountMinor: 31900}},
			creditLineID: {{Category: accounting.CashFlowNoncash, AmountMinor: -31900}},
		},
		factID: factID,
	}
}
