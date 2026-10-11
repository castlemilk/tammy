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
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func TestJournalRepositoryReversesTaxFactWithExactSourceHash(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
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
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := repository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil || revision != 1 {
		t.Fatalf("ReserveFinancialRevision() = %d, %v", revision, err)
	}

	sourceID := "018f0000-0000-7000-8000-000000000415"
	sourceHash := sha256.Sum256([]byte("caller supplied manual evidence content"))
	ruleHash := sha256.Sum256([]byte("tax rule bundle content"))
	fallbackHash := sha256.Sum256([]byte(sourceID))
	if bytes.Equal(sourceHash[:], ruleHash[:]) || bytes.Equal(sourceHash[:], fallbackHash[:]) {
		t.Fatal("fixture source hash must differ from both rule hash and synthesized fallback hash")
	}
	posted := suppliedHashTaxPosting(t, accounts, now, revision,
		"018f0000-0000-7000-8000-000000000410",
		"018f0000-0000-7000-8000-000000000411",
		"018f0000-0000-7000-8000-000000000412",
		"018f0000-0000-7000-8000-000000000413",
		"018f0000-0000-7000-8000-000000000414",
		sourceID, sourceHash[:], ruleHash[:])
	if err := repository.Post(ctx, posted.journal, "MANUAL", posted.journal.Id, 1, posted.facts, posted.flows, now); err != nil {
		t.Fatalf("Post() = %v", err)
	}

	reversalID := "018f0000-0000-7000-8000-000000000431"
	reversalLineIDs := []string{
		"018f0000-0000-7000-8000-000000000432",
		"018f0000-0000-7000-8000-000000000433",
	}
	reversed, reversal, err := repository.Reverse(ctx, posted.journal.Id, 1, posted.journal.PostingDate,
		"Correct office supplies", reversalID, reversalLineIDs, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Reverse() = %v", err)
	}
	if reversed.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED || reversed.Version != 2 ||
		reversed.ReversedByJournalId == nil || *reversed.ReversedByJournalId != reversalID {
		t.Fatalf("Reverse() original projection = state %v version %d reversed_by %v", reversed.State,
			reversed.Version, reversed.ReversedByJournalId)
	}
	if reversal.Id != reversalID || reversal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED ||
		reversal.ReversalOfJournalId == nil || *reversal.ReversalOfJournalId != posted.journal.Id {
		t.Fatalf("Reverse() journal links = id %q state %v reversal_of %v", reversal.Id, reversal.State,
			reversal.ReversalOfJournalId)
	}
	var reversalTaxMinor int64
	if reversal.Lines[1].TaxAmount != nil {
		reversalTaxMinor = reversal.Lines[1].TaxAmount.MinorUnits
	}
	if reversal.Lines[1].TaxCodeId == nil || *reversal.Lines[1].TaxCodeId != "018f0000-0000-7000-8000-000000000413" ||
		reversal.Lines[1].TaxAmount == nil || reversalTaxMinor != -2900 {
		t.Fatalf("reversal tax line = code %v amount %d; want same code and amount -2900",
			reversal.Lines[1].TaxCodeId, reversalTaxMinor)
	}

	storedOriginal, err := repository.Get(ctx, posted.journal.Id)
	if err != nil {
		t.Fatalf("Get(original) after Reverse() = %v", err)
	}
	storedReversal, err := repository.Get(ctx, reversalID)
	if err != nil {
		t.Fatalf("Get(reversal) after Reverse() = %v", err)
	}
	if storedOriginal.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED || storedOriginal.Version != 2 ||
		storedOriginal.ReversedByJournalId == nil || *storedOriginal.ReversedByJournalId != reversalID ||
		storedReversal.ReversalOfJournalId == nil || *storedReversal.ReversalOfJournalId != posted.journal.Id {
		t.Fatalf("persisted reversal links original=%#v reversal=%#v", storedOriginal, storedReversal)
	}

	readFact := func(id string) taxFactReversalSnapshot {
		t.Helper()
		var fact taxFactReversalSnapshot
		err := tx.QueryRowContext(ctx, `
			SELECT source_type, source_id, source_revision, source_content_hash,
			       tax_rule_type, tax_rule_id, tax_rule_revision, tax_rule_content_hash,
			       original_gross_minor, original_net_minor, original_gst_minor,
			       attributed_gross_minor, attributed_net_minor, attributed_gst_minor,
			       remaining_gross_minor, remaining_net_minor, remaining_gst_minor
			FROM tax_facts WHERE id=?`, id).Scan(
			&fact.sourceType, &fact.sourceID, &fact.sourceRevision, &fact.sourceHash,
			&fact.ruleType, &fact.ruleID, &fact.ruleRevision, &fact.ruleHash,
			&fact.amounts[0], &fact.amounts[1], &fact.amounts[2],
			&fact.amounts[3], &fact.amounts[4], &fact.amounts[5],
			&fact.amounts[6], &fact.amounts[7], &fact.amounts[8])
		if err != nil {
			t.Fatalf("read tax fact %q: %v", id, err)
		}
		return fact
	}

	originalFact := readFact(posted.factID)
	reversalFact := readFact(reversal.Lines[1].Id + ":tax")
	wantOriginalAmounts := [9]int64{31900, 29000, 2900, 31900, 29000, 2900, 0, 0, 0}
	if originalFact.amounts != wantOriginalAmounts {
		t.Fatalf("persisted original tax amounts = %v, want %v", originalFact.amounts, wantOriginalAmounts)
	}
	for label, fact := range map[string]taxFactReversalSnapshot{"original": originalFact, "reversal": reversalFact} {
		if fact.sourceType != "manual_evidence" || fact.sourceID != sourceID || fact.sourceRevision != 1 ||
			!bytes.Equal(fact.sourceHash, sourceHash[:]) {
			t.Errorf("%s tax fact source = %q/%q/%d hash %x; want exact caller source ref and hash %x",
				label, fact.sourceType, fact.sourceID, fact.sourceRevision, fact.sourceHash, sourceHash)
		}
		if fact.ruleType != "tax_rule_bundle" || fact.ruleID != "018f0000-0000-7000-8000-000000000430" ||
			fact.ruleRevision != 1 || !bytes.Equal(fact.ruleHash, ruleHash[:]) {
			t.Errorf("%s tax fact rule = %q/%q/%d hash %x; want exact caller rule ref and hash %x",
				label, fact.ruleType, fact.ruleID, fact.ruleRevision, fact.ruleHash, ruleHash)
		}
	}
	if bytes.Equal(reversalFact.sourceHash, reversalFact.ruleHash) || bytes.Equal(reversalFact.sourceHash, fallbackHash[:]) {
		t.Errorf("reversal source hash %x was replaced by a rule or synthesized fallback hash", reversalFact.sourceHash)
	}
	for index, originalAmount := range originalFact.amounts {
		if reversalFact.amounts[index] != -originalAmount || originalAmount+reversalFact.amounts[index] != 0 {
			t.Errorf("tax fact amount column %d original=%d reversal=%d; want exact negation and zero sum",
				index, originalAmount, reversalFact.amounts[index])
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

type taxFactReversalSnapshot struct {
	sourceType, sourceID string
	sourceRevision       uint64
	sourceHash           []byte
	ruleType, ruleID     string
	ruleRevision         uint64
	ruleHash             []byte
	amounts              [9]int64
}
