package purchases

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

// OpeningPayable retains the owned accounting identity independently of an
// optional document link. It is a read projection, not a settlement facility.
type OpeningPayable struct {
	OrganisationID, ID, ConversionID, JournalID, State string
	SourceVersion, FinancialRevision                   uint64
	SourceHash, ConversionHash                         []byte
	Input                                              *tammyv1.OpeningBalanceInput
}

func (p *OpeningPayable) LegacySource() *tammyv1.LegacyPurchaseSourceRef {
	if p == nil {
		return nil
	}
	return &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_OpeningPayable{
		OpeningPayable: &tammyv1.LegacyPurchaseOpeningPayableRef{OpeningPayableId: p.ID,
			SourceVersion: p.SourceVersion, SourceHash: append([]byte(nil), p.SourceHash...)}}}
}

func (r *OpeningRepository) GetOpeningPayable(ctx context.Context, org, id string) (*OpeningPayable, error) {
	if !ids.IsCanonicalV7(id) {
		return nil, ErrOpeningRepository
	}
	values, err := r.readOpeningPayables(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, ErrNotFound
	}
	return values[0], nil
}

// ListOpeningPayables is complete within the supported reporting bound. No
// document page or invoice-date predicate limits old/undated source discovery.
func (r *OpeningRepository) ListOpeningPayables(ctx context.Context, org string) ([]*OpeningPayable, error) {
	return r.readOpeningPayables(ctx, org, "")
}

func (r *OpeningRepository) readOpeningPayables(ctx context.Context, org, id string) ([]*OpeningPayable, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(org) {
		return nil, ErrOpeningRepository
	}
	query := `
		SELECT p.id,p.conversion_id,p.retained_input_proto,c.version,c.conversion_date,
		       i.id,i.account_id,i.item_kind,i.debit_minor,i.credit_minor,i.currency_code,
		       i.source_type,i.source_id,i.source_revision,i.source_content_hash,
		       i.original_issue_date,i.original_due_date,i.outstanding_gst_minor,
		       i.prior_gst_attributed_minor,i.latest_statement_date,i.latest_statement_balance_minor,
		       a.organisation_id,a.normal_balance,a.report_classification,
		       c.state,c.journal_id,c.financial_revision,c.source_sha256,c.replaced_by_id
		FROM purchase_opening_payables p
		JOIN opening_conversions c ON c.id=p.conversion_id
		LEFT JOIN opening_items i ON i.id=p.id AND i.conversion_id=p.conversion_id
		LEFT JOIN accounts a ON a.id=i.account_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED') AND (?='' OR p.id=?)
		ORDER BY p.id`
	args := []any{org, id, id}
	if r.reportingOwnerGuards {
		query, args = postingReportingReadSQL(purchaseReportingOpeningReplayQuery(org, id, false))
	}
	rows, err := r.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, ErrOpeningRepository
	}
	type retainedOpening struct {
		row                   openingPriorSourceRow
		state, hash           string
		journalID, replacedBy sql.NullString
		financial             sql.NullInt64
	}
	var retained []retainedOpening
	for rows.Next() {
		var scanner paymentScanner = rows
		if r.reportingOwnerGuards {
			scanner = purchaseReportingOwnerRow{rows: rows}
		}
		var value retainedOpening
		row := &value.row
		if scanner.Scan(&row.id, &row.conversionID, &row.encoded, &row.version, &row.conversionDate,
			&row.itemID, &row.accountID, &row.kind, &row.debit, &row.credit, &row.currency,
			&row.sourceType, &row.sourceID, &row.sourceVersion, &row.sourceHash,
			&row.issueDate, &row.dueDate, &row.outstandingGST, &row.priorGST,
			&row.statementDate, &row.statementBalance, &row.accountOrganisation,
			&row.normalBalance, &row.reportClassification, &value.state, &value.journalID,
			&value.financial, &value.hash, &value.replacedBy) != nil {
			_ = rows.Close()
			return nil, ErrOpeningRepository
		}
		retained = append(retained, value)
		if len(retained) > 1000 {
			_ = rows.Close()
			return nil, ErrSourceBound
		}
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return nil, ErrOpeningRepository
	}
	result := make([]*OpeningPayable, 0, len(retained))
	journalRepo, _ := accounting.NewJournalRepository(r.executor)
	for _, value := range retained {
		input, err := value.row.verify(org)
		if err != nil {
			return nil, err
		}
		conversionHash, err := hex.DecodeString(value.hash)
		if err != nil || len(conversionHash) != sha256.Size || hex.EncodeToString(conversionHash) != value.hash ||
			!value.journalID.Valid || !ids.IsCanonicalV7(value.journalID.String) || !value.financial.Valid || value.financial.Int64 <= 0 ||
			(value.state == "POSTED" && value.replacedBy.Valid) ||
			(value.state == "REPLACED" && (!value.replacedBy.Valid || !ids.IsCanonicalV7(value.replacedBy.String))) {
			return nil, ErrOpeningRepository
		}
		var journal *tammyv1.Journal
		if r.reportingOwnerGuards {
			var proof *accounting.JournalReportingRecord
			proof, err = journalRepo.GetForReporting(ctx, org, value.journalID.String)
			if err == nil && proof != nil {
				journal = proof.Journal
			}
		} else {
			journal, err = journalRepo.Get(ctx, value.journalID.String)
		}
		if err != nil || journal == nil || journal.OrganisationId != org || journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION ||
			journal.FinancialRevision != uint64(value.financial.Int64) ||
			(journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED && journal.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED) {
			return nil, ErrOpeningRepository
		}
		rows, err := r.executor.QueryContext(ctx, `SELECT count(*) FROM journals WHERE id=? AND organisation_id=? AND source_type='OPENING' AND source_id=? AND source_revision=1 AND source_kind=0`, journal.Id, org, value.row.conversionID)
		if err != nil {
			return nil, ErrOpeningRepository
		}
		var count int
		if (purchaseRevisionRow{rows: rows}).Scan(&count) != nil || count != 1 {
			return nil, ErrOpeningRepository
		}
		matched := false
		for _, line := range journal.Lines {
			if line.AccountId == input.AccountId && line.GetDebit().GetCurrencyCode() == "AUD" && line.GetCredit().GetCurrencyCode() == "AUD" &&
				line.GetDebit().GetMinorUnits() == value.row.debit.Int64 && line.GetCredit().GetMinorUnits() == value.row.credit.Int64 {
				matched = true
			}
		}
		if !matched {
			return nil, ErrOpeningRepository
		}
		if value.replacedBy.Valid {
			rows, err := r.executor.QueryContext(ctx, `SELECT count(*) FROM opening_conversions WHERE id=? AND organisation_id=?`, value.replacedBy.String, org)
			if err != nil {
				return nil, ErrOpeningRepository
			}
			if (purchaseRevisionRow{rows: rows}).Scan(&count) != nil || count != 1 {
				return nil, ErrOpeningRepository
			}
		}
		digest := sha256.Sum256(value.row.encoded)
		result = append(result, &OpeningPayable{OrganisationID: org, ID: input.ClientLineId, ConversionID: value.row.conversionID,
			JournalID: journal.Id, State: value.state, SourceVersion: uint64(value.row.version), FinancialRevision: journal.FinancialRevision,
			SourceHash: append([]byte(nil), digest[:]...), ConversionHash: append([]byte(nil), conversionHash...), Input: proto.Clone(input).(*tammyv1.OpeningBalanceInput)})
	}
	return result, nil
}
