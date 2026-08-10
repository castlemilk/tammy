package accounting

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrJournalRepository     = errors.New("accounting: journal repository failure")
	ErrJournalNotFound       = errors.New("accounting: journal not found")
	ErrJournalSourceConflict = errors.New("accounting: journal source revision already posted")
	ErrFinancialRevision     = errors.New("accounting: financial revision conflict")
)

type JournalRepository struct{ executor app.CommandSQLExecutor }

func NewJournalRepository(executor app.CommandSQLExecutor) (*JournalRepository, error) {
	if executor == nil {
		return nil, ErrJournalRepository
	}
	return &JournalRepository{executor: executor}, nil
}

func (repository *JournalRepository) ReserveFinancialRevision(
	ctx context.Context,
	expected *uint64,
	now time.Time,
) (uint64, error) {
	if repository == nil || repository.executor == nil || ctx == nil || now.IsZero() {
		return 0, ErrJournalRepository
	}
	query := `UPDATE financial_revisions
		SET financial_revision = financial_revision + 1,
		    ledger_revision = ledger_revision + 1, updated_at = ? WHERE id = 1`
	arguments := []any{now.UTC().Format(time.RFC3339Nano)}
	if expected != nil {
		query += ` AND financial_revision = ?`
		arguments = append(arguments, *expected)
	}
	result, err := repository.executor.ExecContext(ctx, query, arguments...)
	if err != nil {
		return 0, fmt.Errorf("%w: reserve: %v", ErrJournalRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%w: reserve count: %v", ErrJournalRepository, err)
	}
	if affected != 1 {
		return 0, ErrFinancialRevision
	}
	return repository.currentFinancialRevision(ctx)
}

func (repository *JournalRepository) Post(
	ctx context.Context,
	journal *tammyv1.Journal,
	sourceType, sourceID string,
	sourceRevision uint64,
	taxFacts map[string]TaxFact,
	flows map[string][]CashFlowComponent,
	now time.Time,
) error {
	if repository == nil || repository.executor == nil || ctx == nil || journal == nil ||
		!ids.IsCanonicalV7(sourceID) || sourceRevision == 0 || now.IsZero() ||
		(sourceType != "MANUAL" && sourceType != "REVERSAL" && sourceType != "OPENING") {
		return ErrJournalRepository
	}
	accounts, err := repository.loadJournalAccounts(ctx, journal)
	if err != nil {
		return err
	}
	manual := sourceType == "MANUAL"
	if err := ValidateJournal(journal, accounts, manual); err != nil {
		return err
	}
	if len(flows) != len(journal.Lines) {
		return ErrInvalidCashFlow
	}
	for _, line := range journal.Lines {
		components, ok := flows[line.Id]
		if !ok || ValidateCashFlowAllocation(line, IsCashAccount(accounts[line.AccountId]), components) != nil {
			return ErrInvalidCashFlow
		}
	}
	_, err = repository.executor.ExecContext(ctx, `
		INSERT INTO journals(
			id, organisation_id, version, source_type, source_id, source_revision,
			state, journal_date, description, reversal_of_journal_id,
			total_debits_minor, total_credits_minor, currency_code, financial_revision,
			posted_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 'DRAFT', ?, ?, ?, ?, ?, 'AUD', ?, NULL, ?)`,
		journal.Id, journal.OrganisationId, journal.Version, sourceType, sourceID, sourceRevision,
		civilDateString(journal.PostingDate), journal.Memo, nullableString(journal.ReversalOfJournalId),
		journal.TotalDebits.MinorUnits, journal.TotalCredits.MinorUnits, journal.FinancialRevision,
		now.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if repository.sourceExists(ctx, journal.OrganisationId, sourceType, sourceID, sourceRevision) {
			return ErrJournalSourceConflict
		}
		return fmt.Errorf("%w: insert journal: %v", ErrJournalRepository, err)
	}
	for _, line := range journal.Lines {
		if err := repository.insertLine(ctx, line); err != nil {
			return err
		}
		fact, hasFact := taxFacts[line.Id]
		if (line.TaxCodeId != nil) != hasFact {
			return ErrInvalidTaxFact
		}
		if hasFact {
			fact.JournalLineID = line.Id
			fact.OrganisationID = journal.OrganisationId
			if err := repository.insertTaxFact(ctx, fact, now); err != nil {
				return err
			}
		}
		for index, component := range flows[line.Id] {
			if _, err := repository.executor.ExecContext(ctx, `
				INSERT INTO cash_flow_facts(id, organisation_id, journal_line_id, sequence,
					category, amount_minor, source_revision, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				line.Id+":"+strconv.Itoa(index+1), journal.OrganisationId, line.Id, index+1,
				cashFlowCategoryName(component.Category), component.AmountMinor, sourceRevision,
				now.UTC().Format(time.RFC3339Nano)); err != nil {
				return fmt.Errorf("%w: insert cash flow: %v", ErrJournalRepository, err)
			}
		}
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE journals SET state = 'POSTED', posted_at = ? WHERE id = ? AND state = 'DRAFT'`,
		journal.PostedAt.AsTime().UTC().Format(time.RFC3339Nano), journal.Id)
	if err != nil {
		return fmt.Errorf("%w: post journal: %v", ErrJournalRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return ErrJournalRepository
	}
	return nil
}

func (repository *JournalRepository) insertTaxFact(ctx context.Context, fact TaxFact, now time.Time) error {
	if ValidateTaxFact(fact) != nil {
		return ErrInvalidTaxFact
	}
	_, err := repository.executor.ExecContext(ctx, `
		INSERT INTO tax_facts(
			id, organisation_id, journal_line_id, tax_code, treatment,
			original_gross_minor, original_net_minor, original_gst_minor,
			attributed_gross_minor, attributed_net_minor, attributed_gst_minor,
			remaining_gross_minor, remaining_net_minor, remaining_gst_minor,
			tax_rule_type, tax_rule_id, tax_rule_revision, tax_rule_content_hash,
			source_type, source_id, source_revision, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fact.ID, fact.OrganisationID, fact.JournalLineID, fact.TaxCode, taxTreatmentName(fact.Treatment),
		fact.OriginalGrossMinor, fact.OriginalNetMinor, fact.OriginalGSTMinor,
		fact.AttributedGrossMinor, fact.AttributedNetMinor, fact.AttributedGSTMinor,
		fact.RemainingGrossMinor, fact.RemainingNetMinor, fact.RemainingGSTMinor,
		fact.Rule.Type, fact.Rule.Id, fact.Rule.Revision, fact.Rule.ContentHash,
		fact.Source.Type, fact.Source.Id, fact.Source.Revision, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("%w: insert tax fact: %v", ErrJournalRepository, err)
	}
	return nil
}

func (repository *JournalRepository) insertLine(ctx context.Context, line *tammyv1.JournalLine) error {
	var taxCodeID, taxAmount, ruleType, ruleID, ruleRevision, ruleHash any
	if line.TaxCodeId != nil {
		taxCodeID, taxAmount = *line.TaxCodeId, line.TaxAmount.MinorUnits
		ruleType, ruleID, ruleRevision, ruleHash = line.TaxRule.Type, line.TaxRule.Id, line.TaxRule.Revision, line.TaxRule.ContentHash
	}
	_, err := repository.executor.ExecContext(ctx, `
		INSERT INTO journal_lines(
			id, journal_id, line_number, account_id, debit_minor, credit_minor,
			currency_code, memo, tax_code_id, tax_amount_minor,
			tax_rule_type, tax_rule_id, tax_rule_revision, tax_rule_content_hash
		) VALUES (?, ?, ?, ?, ?, ?, 'AUD', ?, ?, ?, ?, ?, ?, ?)`,
		line.Id, line.JournalId, line.Sequence, line.AccountId, line.Debit.MinorUnits,
		line.Credit.MinorUnits, line.Description, taxCodeID, taxAmount, ruleType, ruleID, ruleRevision, ruleHash)
	if err != nil {
		return fmt.Errorf("%w: insert line: %v", ErrJournalRepository, err)
	}
	return nil
}

func (repository *JournalRepository) Get(ctx context.Context, journalID string) (*tammyv1.Journal, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(journalID) {
		return nil, ErrJournalRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, organisation_id, version, source_type, state, journal_date, description,
		       reversal_of_journal_id, reversed_by_journal_id, total_debits_minor,
		       total_credits_minor, financial_revision, posted_at
		FROM journals WHERE id = ?`, journalID)
	if err != nil {
		return nil, fmt.Errorf("%w: get journal: %v", ErrJournalRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrJournalNotFound
	}
	journal, err := scanJournal(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return nil, ErrJournalRepository
	}
	lines, err := repository.loadJournalLines(ctx, journalID)
	if err != nil {
		return nil, err
	}
	journal.Lines = lines
	return journal, nil
}

func (repository *JournalRepository) Reverse(ctx context.Context, journalID string, expectedVersion uint64,
	date *tammyv1.CivilDate, reason, reversalID string, lineIDs []string, now time.Time,
) (*tammyv1.Journal, *tammyv1.Journal, error) {
	if repository == nil || repository.executor == nil || ctx == nil || expectedVersion == 0 || now.IsZero() {
		return nil, nil, ErrJournalRepository
	}
	original, err := repository.Get(ctx, journalID)
	if err != nil {
		return nil, nil, err
	}
	if original.Version != expectedVersion || original.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || original.ReversedByJournalId != nil {
		return nil, nil, ErrJournalSourceConflict
	}
	revision, err := repository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil {
		return nil, nil, err
	}
	reversed, reversal, err := BuildJournalReversal(original, reversalID, lineIDs, date, reason, revision, now)
	if err != nil {
		return nil, nil, err
	}
	flows, err := repository.reversalCashFlows(ctx, original, reversal)
	if err != nil {
		return nil, nil, err
	}
	taxFacts, err := repository.reversalTaxFacts(ctx, original, reversal)
	if err != nil {
		return nil, nil, err
	}
	if err := repository.Post(ctx, reversal, "REVERSAL", original.Id, original.Version, taxFacts, flows, now); err != nil {
		return nil, nil, err
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE journals SET state='REVERSED', reversed_by_journal_id=?, version=version+1
		WHERE id=? AND version=? AND state='POSTED' AND reversed_by_journal_id IS NULL`,
		reversal.Id, original.Id, expectedVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: link reversal: %v", ErrJournalRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, nil, ErrJournalSourceConflict
	}
	return reversed, reversal, nil
}

func (repository *JournalRepository) reversalCashFlows(ctx context.Context, original, reversal *tammyv1.Journal) (map[string][]CashFlowComponent, error) {
	flows := make(map[string][]CashFlowComponent, len(original.Lines))
	for index, line := range original.Lines {
		rows, err := repository.executor.QueryContext(ctx, `
			SELECT category, amount_minor FROM cash_flow_facts WHERE journal_line_id=? ORDER BY sequence`, line.Id)
		if err != nil {
			return nil, ErrJournalRepository
		}
		components := make([]CashFlowComponent, 0, 4)
		for rows.Next() {
			var category string
			var amount int64
			if err := rows.Scan(&category, &amount); err != nil {
				_ = rows.Close()
				return nil, ErrJournalRepository
			}
			components = append(components, CashFlowComponent{Category: parseCashFlowCategory(category), AmountMinor: -amount})
		}
		finishErr := rows.Err()
		closeErr := rows.Close()
		if finishErr != nil || closeErr != nil || len(components) == 0 {
			return nil, ErrJournalRepository
		}
		flows[reversal.Lines[index].Id] = components
	}
	return flows, nil
}

func (repository *JournalRepository) reversalTaxFacts(ctx context.Context, original, reversal *tammyv1.Journal) (map[string]TaxFact, error) {
	facts := make(map[string]TaxFact)
	for index, line := range original.Lines {
		if line.TaxCodeId == nil {
			continue
		}
		rows, err := repository.executor.QueryContext(ctx, `
			SELECT tax_code, treatment, original_gross_minor, original_net_minor, original_gst_minor,
			       attributed_gross_minor, attributed_net_minor, attributed_gst_minor,
			       remaining_gross_minor, remaining_net_minor, remaining_gst_minor,
			       tax_rule_type, tax_rule_id, tax_rule_revision, tax_rule_content_hash,
			       source_type, source_id, source_revision
			FROM tax_facts WHERE journal_line_id=?`, line.Id)
		if err != nil {
			return nil, ErrJournalRepository
		}
		if !rows.Next() {
			_ = rows.Close()
			return nil, ErrJournalRepository
		}
		fact := TaxFact{ID: reversal.Lines[index].Id + ":tax", OrganisationID: reversal.OrganisationId,
			JournalLineID: reversal.Lines[index].Id, Rule: &tammyv1.SourceRef{}, Source: &tammyv1.SourceRef{}}
		var treatment string
		if err := rows.Scan(&fact.TaxCode, &treatment, &fact.OriginalGrossMinor, &fact.OriginalNetMinor,
			&fact.OriginalGSTMinor, &fact.AttributedGrossMinor, &fact.AttributedNetMinor,
			&fact.AttributedGSTMinor, &fact.RemainingGrossMinor, &fact.RemainingNetMinor,
			&fact.RemainingGSTMinor, &fact.Rule.Type, &fact.Rule.Id, &fact.Rule.Revision,
			&fact.Rule.ContentHash, &fact.Source.Type, &fact.Source.Id, &fact.Source.Revision); err != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			return nil, ErrJournalRepository
		}
		if err := rows.Close(); err != nil {
			return nil, ErrJournalRepository
		}
		fact.Treatment = parseTaxTreatment(treatment)
		fact.OriginalGrossMinor = -fact.OriginalGrossMinor
		fact.OriginalNetMinor = -fact.OriginalNetMinor
		fact.OriginalGSTMinor = -fact.OriginalGSTMinor
		fact.AttributedGrossMinor = -fact.AttributedGrossMinor
		fact.AttributedNetMinor = -fact.AttributedNetMinor
		fact.AttributedGSTMinor = -fact.AttributedGSTMinor
		fact.RemainingGrossMinor = -fact.RemainingGrossMinor
		fact.RemainingNetMinor = -fact.RemainingNetMinor
		fact.RemainingGSTMinor = -fact.RemainingGSTMinor
		if ValidateTaxFact(fact) != nil {
			return nil, ErrJournalRepository
		}
		facts[reversal.Lines[index].Id] = fact
	}
	return facts, nil
}

func (repository *JournalRepository) TrialBalance(
	ctx context.Context,
	organisationID, asOf string,
) ([]*tammyv1.TrialBalanceLine, int64, int64, uint64, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || !validDateString(asOf) {
		return nil, 0, 0, 0, ErrJournalRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT a.id, a.version, a.code, a.name, a.normal_balance,
		       COALESCE(SUM(l.debit_minor),0), COALESCE(SUM(l.credit_minor),0)
		FROM accounts a
		JOIN journal_lines l ON l.account_id = a.id
		JOIN journals j ON j.id = l.journal_id
		WHERE a.organisation_id = ? AND j.state IN ('POSTED','REVERSED') AND j.journal_date <= ?
		GROUP BY a.id, a.version, a.code, a.name, a.normal_balance
		ORDER BY a.code, a.id`, organisationID, asOf)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("%w: trial balance: %v", ErrJournalRepository, err)
	}
	defer rows.Close()
	lines := make([]*tammyv1.TrialBalanceLine, 0, 64)
	var totalDebits, totalCredits int64
	for rows.Next() {
		var id, code, name, normal string
		var version uint64
		var debits, credits int64
		if err := rows.Scan(&id, &version, &code, &name, &normal, &debits, &credits); err != nil {
			return nil, 0, 0, 0, ErrJournalRepository
		}
		var ok bool
		if totalDebits, ok = checkedPositiveAdd(totalDebits, debits); !ok {
			return nil, 0, 0, 0, ErrJournalRepository
		}
		if totalCredits, ok = checkedPositiveAdd(totalCredits, credits); !ok {
			return nil, 0, 0, 0, ErrJournalRepository
		}
		balance := debits - credits
		if normal == "CREDIT" {
			balance = credits - debits
		}
		digest := sha256.Sum256([]byte(id + ":" + strconv.FormatUint(version, 10)))
		lines = append(lines, &tammyv1.TrialBalanceLine{Account: &tammyv1.SourceRef{Type: "account", Id: id,
			Revision: version, ContentHash: digest[:]}, Code: code, Name: name,
			Debits: audMoney(debits), Credits: audMoney(credits), LedgerNormalBalance: audMoney(balance)})
	}
	if err := rows.Err(); err != nil || totalDebits != totalCredits {
		return nil, 0, 0, 0, ErrJournalRepository
	}
	revision, err := repository.currentFinancialRevision(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return lines, totalDebits, totalCredits, revision, nil
}

func (repository *JournalRepository) GeneralLedger(ctx context.Context, organisationID string,
	accountIDs []string, startDate, endDate string, limit int,
) ([]*tammyv1.LedgerEntry, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		!validDateString(startDate) || !validDateString(endDate) || startDate > endDate || limit < 1 || limit > 200 {
		return nil, ErrJournalRepository
	}
	filter := make(map[string]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if !ids.IsCanonicalV7(id) {
			return nil, ErrJournalRepository
		}
		filter[id] = struct{}{}
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT j.id, j.version, j.journal_date, l.id, l.account_id, l.debit_minor,
		       l.credit_minor, a.normal_balance
		FROM journals j JOIN journal_lines l ON l.journal_id=j.id
		JOIN accounts a ON a.id=l.account_id
		WHERE j.organisation_id=? AND j.state IN ('POSTED','REVERSED')
		  AND j.journal_date>=? AND j.journal_date<=?
		ORDER BY j.journal_date, j.id, l.line_number LIMIT ?`, organisationID, startDate, endDate, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: general ledger: %v", ErrJournalRepository, err)
	}
	defer rows.Close()
	entries := make([]*tammyv1.LedgerEntry, 0, limit)
	balances := make(map[string]int64)
	for rows.Next() {
		var journalID, date, lineID, accountID, normal string
		var version uint64
		var debit, credit int64
		if err := rows.Scan(&journalID, &version, &date, &lineID, &accountID, &debit, &credit, &normal); err != nil {
			return nil, ErrJournalRepository
		}
		if len(filter) != 0 {
			if _, selected := filter[accountID]; !selected {
				continue
			}
		}
		movement := debit - credit
		if normal == "CREDIT" {
			movement = credit - debit
		}
		balance, ok := checkedSignedAdd(balances[accountID], movement)
		if !ok {
			return nil, ErrJournalRepository
		}
		balances[accountID] = balance
		digest := sha256.Sum256([]byte(journalID + ":" + strconv.FormatUint(version, 10)))
		entries = append(entries, &tammyv1.LedgerEntry{Journal: &tammyv1.SourceRef{Type: "journal", Id: journalID,
			Revision: version, ContentHash: digest[:]}, JournalLineId: lineID, AccountId: accountID,
			PostingDate: parseCivilDate(date), Debit: audMoney(debit), Credit: audMoney(credit), RunningBalance: audMoney(balance)})
	}
	if err := rows.Err(); err != nil {
		return nil, ErrJournalRepository
	}
	return entries, nil
}

func (repository *JournalRepository) loadJournalAccounts(ctx context.Context, journal *tammyv1.Journal) (map[string]*tammyv1.Account, error) {
	accounts := make(map[string]*tammyv1.Account, len(journal.GetLines()))
	accountRepository, err := NewAccountRepository(repository.executor)
	if err != nil {
		return nil, err
	}
	for _, line := range journal.GetLines() {
		if line == nil {
			return nil, ErrInvalidJournal
		}
		if _, exists := accounts[line.AccountId]; exists {
			continue
		}
		account, err := accountRepository.Get(ctx, line.AccountId)
		if err != nil {
			return nil, err
		}
		accounts[line.AccountId] = account
	}
	return accounts, nil
}

func (repository *JournalRepository) loadJournalLines(ctx context.Context, journalID string) ([]*tammyv1.JournalLine, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, journal_id, line_number, account_id, debit_minor, credit_minor,
		       memo, tax_code_id, tax_amount_minor, tax_rule_type, tax_rule_id,
		       tax_rule_revision, tax_rule_content_hash
		FROM journal_lines WHERE journal_id = ? ORDER BY line_number`, journalID)
	if err != nil {
		return nil, ErrJournalRepository
	}
	defer rows.Close()
	lines := make([]*tammyv1.JournalLine, 0, 8)
	for rows.Next() {
		line := &tammyv1.JournalLine{Debit: audMoney(0), Credit: audMoney(0)}
		var taxCode, ruleType, ruleID sql.NullString
		var taxAmount, ruleRevision sql.NullInt64
		var ruleHash []byte
		if err := rows.Scan(&line.Id, &line.JournalId, &line.Sequence, &line.AccountId,
			&line.Debit.MinorUnits, &line.Credit.MinorUnits, &line.Description, &taxCode,
			&taxAmount, &ruleType, &ruleID, &ruleRevision, &ruleHash); err != nil {
			return nil, ErrJournalRepository
		}
		if taxCode.Valid {
			line.TaxCodeId = &taxCode.String
			line.TaxAmount = audMoney(taxAmount.Int64)
			line.TaxRule = &tammyv1.SourceRef{Type: ruleType.String, Id: ruleID.String,
				Revision: uint64(ruleRevision.Int64), ContentHash: append([]byte(nil), ruleHash...)}
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrJournalRepository
	}
	return lines, nil
}

func scanJournal(scanner interface{ Scan(...any) error }) (*tammyv1.Journal, error) {
	journal := &tammyv1.Journal{TotalDebits: audMoney(0), TotalCredits: audMoney(0)}
	var source, state, date, postedAt string
	var reversalOf, reversedBy sql.NullString
	if err := scanner.Scan(&journal.Id, &journal.OrganisationId, &journal.Version, &source, &state, &date,
		&journal.Memo, &reversalOf, &reversedBy, &journal.TotalDebits.MinorUnits,
		&journal.TotalCredits.MinorUnits, &journal.FinancialRevision, &postedAt); err != nil {
		return nil, err
	}
	journal.Source = parseJournalSource(source)
	journal.State = parseJournalState(state)
	journal.PostingDate = parseCivilDate(date)
	instant, err := time.Parse(time.RFC3339Nano, postedAt)
	if err != nil || journal.PostingDate == nil {
		return nil, ErrJournalRepository
	}
	journal.PostedAt = timestamppb.New(instant)
	if reversalOf.Valid {
		journal.ReversalOfJournalId = &reversalOf.String
	}
	if reversedBy.Valid {
		journal.ReversedByJournalId = &reversedBy.String
	}
	return journal, nil
}

func (repository *JournalRepository) currentFinancialRevision(ctx context.Context) (uint64, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT financial_revision FROM financial_revisions WHERE id = 1`)
	if err != nil {
		return 0, ErrJournalRepository
	}
	defer rows.Close()
	var revision uint64
	if !rows.Next() || rows.Scan(&revision) != nil || rows.Next() || rows.Err() != nil || revision == 0 {
		return 0, ErrJournalRepository
	}
	return revision, nil
}

func (repository *JournalRepository) sourceExists(ctx context.Context, organisationID, sourceType, sourceID string, revision uint64) bool {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT 1 FROM journals WHERE organisation_id = ? AND source_type = ? AND source_id = ? AND source_revision = ?`,
		organisationID, sourceType, sourceID, revision)
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

func cashFlowCategoryName(value CashFlowCategory) string {
	return map[CashFlowCategory]string{CashFlowOperating: "OPERATING", CashFlowInvesting: "INVESTING",
		CashFlowFinancing: "FINANCING", CashFlowTransfer: "TRANSFER", CashFlowNoncash: "NONCASH"}[value]
}

func parseCashFlowCategory(value string) CashFlowCategory {
	return map[string]CashFlowCategory{"OPERATING": CashFlowOperating, "INVESTING": CashFlowInvesting,
		"FINANCING": CashFlowFinancing, "TRANSFER": CashFlowTransfer, "NONCASH": CashFlowNoncash}[value]
}

func parseTaxTreatment(value string) tammyv1.TaxTreatment {
	return map[string]tammyv1.TaxTreatment{"TAXABLE": tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE,
		"GST_FREE":     tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE,
		"INPUT_TAXED":  tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED,
		"OUT_OF_SCOPE": tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE}[value]
}

func parseJournalSource(value string) tammyv1.JournalSource {
	return map[string]tammyv1.JournalSource{"OPENING": tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION,
		"MANUAL": tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, "REVERSAL": tammyv1.JournalSource_JOURNAL_SOURCE_REVERSAL}[value]
}

func parseJournalState(value string) tammyv1.JournalState {
	if value == "POSTED" {
		return tammyv1.JournalState_JOURNAL_STATE_POSTED
	}
	if value == "REVERSED" {
		return tammyv1.JournalState_JOURNAL_STATE_REVERSED
	}
	return tammyv1.JournalState_JOURNAL_STATE_UNSPECIFIED
}

func civilDateString(value *tammyv1.CivilDate) string {
	return fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
}

func parseCivilDate(value string) *tammyv1.CivilDate {
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	return &tammyv1.CivilDate{Year: int32(date.Year()), Month: int32(date.Month()), Day: int32(date.Day())}
}

func validDateString(value string) bool { return parseCivilDate(value) != nil }
func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
func audMoney(minor int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor}
}
