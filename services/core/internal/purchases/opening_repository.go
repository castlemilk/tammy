package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrOpeningRepository = errors.New("purchases: opening repository failure")

type OpeningRepository struct {
	executor             app.CommandSQLExecutor
	reportingOwnerGuards bool
}

func NewOpeningRepository(executor app.CommandSQLExecutor) (*OpeningRepository, error) {
	if executor == nil {
		return nil, ErrOpeningRepository
	}
	return &OpeningRepository{executor: executor}, nil
}

func (repository *OpeningRepository) RecordOpeningPayable(ctx context.Context, conversionID string,
	input *tammyv1.OpeningBalanceInput, now time.Time,
) error {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(conversionID) || input == nil ||
		!ids.IsCanonicalV7(input.ClientLineId) || now.IsZero() ||
		(input.Kind != tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM &&
			input.Kind != tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_UNALLOCATED_CREDIT) {
		return ErrOpeningRepository
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil || len(encoded) == 0 || len(encoded) > 1<<20 {
		return ErrOpeningRepository
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO purchase_opening_payables(id, conversion_id, retained_input_proto, created_at) VALUES(?,?,?,?)`,
		input.ClientLineId, conversionID, encoded, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return ErrOpeningRepository
	}
	return nil
}

// MatchingPriorSources reads all retained owned opening payables, including
// replaced conversions. An opening source remains prior accounting even when
// its original journal was reversed as part of a replacement. No supplier or
// invoice identity is inferred from amounts or dates.
func (repository *OpeningRepository) MatchingPriorSources(ctx context.Context, organisationID, documentID string,
	originalHash, reviewedHash []byte,
) ([]*tammyv1.SourceRef, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		!ids.IsCanonicalV7(documentID) || len(originalHash) != sha256.Size || len(reviewedHash) != sha256.Size {
		return nil, ErrOpeningRepository
	}
	query := `
		SELECT p.id,p.conversion_id,p.retained_input_proto,c.version,c.conversion_date,
		       i.id,i.account_id,i.item_kind,i.debit_minor,i.credit_minor,i.currency_code,
		       i.source_type,i.source_id,i.source_revision,i.source_content_hash,
		       i.original_issue_date,i.original_due_date,i.outstanding_gst_minor,
		       i.prior_gst_attributed_minor,i.latest_statement_date,i.latest_statement_balance_minor,
		       a.organisation_id,a.normal_balance,a.report_classification
		FROM purchase_opening_payables p
		JOIN opening_conversions c ON c.id=p.conversion_id
		LEFT JOIN opening_items i ON i.id=p.id AND i.conversion_id=p.conversion_id
		LEFT JOIN accounts a ON a.id=i.account_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED')
		ORDER BY p.id`
	args := []any{organisationID}
	if repository.reportingOwnerGuards {
		query, args = postingReportingReadSQL(purchaseReportingOpeningReplayQuery(organisationID, "", true))
	}
	rows, err := repository.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, ErrOpeningRepository
	}
	defer rows.Close()
	refs := make([]*tammyv1.SourceRef, 0)
	for rows.Next() {
		var scanner paymentScanner = rows
		if repository.reportingOwnerGuards {
			scanner = purchaseReportingOwnerRow{rows: rows}
		}
		var retained openingPriorSourceRow
		if err := scanner.Scan(&retained.id, &retained.conversionID, &retained.encoded, &retained.version, &retained.conversionDate,
			&retained.itemID, &retained.accountID, &retained.kind, &retained.debit, &retained.credit, &retained.currency,
			&retained.sourceType, &retained.sourceID, &retained.sourceVersion, &retained.sourceHash,
			&retained.issueDate, &retained.dueDate, &retained.outstandingGST, &retained.priorGST,
			&retained.statementDate, &retained.statementBalance, &retained.accountOrganisation,
			&retained.normalBalance, &retained.reportClassification); err != nil {
			return nil, ErrOpeningRepository
		}
		input, err := retained.verify(organisationID)
		if err != nil {
			return nil, err
		}
		if input.Source == nil || (input.Source.Id != documentID &&
			!bytes.Equal(input.Source.ContentHash, originalHash) && !bytes.Equal(input.Source.ContentHash, reviewedHash)) {
			continue
		}
		if len(refs) == 50 {
			return nil, fmt.Errorf("%w: matching prior sources exceed 50", ErrOpeningRepository)
		}
		hash := sha256.Sum256(retained.encoded)
		refs = append(refs, &tammyv1.SourceRef{Type: "purchase_opening_payable", Id: input.ClientLineId,
			Revision: uint64(retained.version), ContentHash: append([]byte(nil), hash[:]...)})
	}
	if rows.Err() != nil {
		return nil, ErrOpeningRepository
	}
	if err := rows.Close(); err != nil {
		return nil, ErrOpeningRepository
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Id < refs[j].Id })
	return refs, nil
}

type openingPriorSourceRow struct {
	id, conversionID, conversionDate           string
	encoded                                    []byte
	version                                    int64
	itemID, accountID, kind, currency          sql.NullString
	debit, credit                              sql.NullInt64
	sourceType, sourceID                       sql.NullString
	sourceVersion                              sql.NullInt64
	sourceHash                                 []byte
	issueDate, dueDate, statementDate          sql.NullString
	outstandingGST, priorGST, statementBalance sql.NullInt64
	accountOrganisation, normalBalance         sql.NullString
	reportClassification                       sql.NullString
}

func (row openingPriorSourceRow) verify(organisationID string) (*tammyv1.OpeningBalanceInput, error) {
	conversionDate, dateErr := time.Parse("2006-01-02", row.conversionDate)
	if !ids.IsCanonicalV7(row.id) || !ids.IsCanonicalV7(row.conversionID) || row.version <= 0 ||
		dateErr != nil || conversionDate.Year() < 1900 || conversionDate.Year() > 9999 ||
		len(row.encoded) == 0 || len(row.encoded) > 1<<20 || !row.itemID.Valid || row.itemID.String != row.id ||
		!row.accountID.Valid || !ids.IsCanonicalV7(row.accountID.String) || !row.kind.Valid ||
		!row.currency.Valid || row.currency.String != "AUD" || !row.debit.Valid || !row.credit.Valid ||
		!row.accountOrganisation.Valid || row.accountOrganisation.String != organisationID ||
		!row.normalBalance.Valid || !row.reportClassification.Valid || row.reportClassification.String != "balance_sheet.payables" {
		return nil, ErrOpeningRepository
	}
	input := &tammyv1.OpeningBalanceInput{}
	if proto.Unmarshal(row.encoded, input) != nil || input.ClientLineId != row.id || input.AccountId != row.accountID.String ||
		input.LedgerBalance == nil || input.LedgerBalance.CurrencyCode != "AUD" ||
		input.LedgerBalance.MinorUnits == 0 || input.LedgerBalance.MinorUnits == math.MinInt64 {
		return nil, ErrOpeningRepository
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil || !bytes.Equal(encoded, row.encoded) {
		return nil, ErrOpeningRepository
	}
	switch input.Kind {
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM:
		if row.kind.String != "SUPPLIER_OPEN_ITEM" || input.OriginalIssueDate == nil || input.OriginalDueDate == nil {
			return nil, ErrOpeningRepository
		}
	case tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_UNALLOCATED_CREDIT:
		if row.kind.String != "UNALLOCATED_CREDIT" {
			return nil, ErrOpeningRepository
		}
	default:
		return nil, ErrOpeningRepository
	}
	debit, credit := input.LedgerBalance.MinorUnits, int64(0)
	if row.normalBalance.String == "CREDIT" {
		debit, credit = 0, debit
	} else if row.normalBalance.String != "DEBIT" {
		return nil, ErrOpeningRepository
	}
	if debit < 0 || credit < 0 {
		debit, credit = -credit, -debit
	}
	if row.debit.Int64 != debit || row.credit.Int64 != credit ||
		!openingRetainedDateMatches(input.OriginalIssueDate, row.issueDate) ||
		!openingRetainedDateMatches(input.OriginalDueDate, row.dueDate) ||
		!openingRetainedDateMatches(input.LatestStatementDate, row.statementDate) ||
		!openingRetainedMoneyMatches(input.OutstandingGst, row.outstandingGST) ||
		!openingRetainedMoneyMatches(input.PriorGstAttributed, row.priorGST) ||
		!openingRetainedMoneyMatches(input.LatestStatementBalance, row.statementBalance) ||
		(input.OutstandingGst == nil) != (input.PriorGstAttributed == nil) {
		return nil, ErrOpeningRepository
	}
	if input.OutstandingGst != nil {
		outstanding, attributed := input.OutstandingGst.MinorUnits, input.PriorGstAttributed.MinorUnits
		if outstanding == math.MinInt64 || attributed == math.MinInt64 {
			return nil, ErrOpeningRepository
		}
		if outstanding < 0 {
			outstanding = -outstanding
		}
		if attributed < 0 {
			attributed = -attributed
		}
		if attributed > outstanding {
			return nil, ErrOpeningRepository
		}
	}
	if input.Source == nil {
		if row.sourceType.Valid || row.sourceID.Valid || row.sourceVersion.Valid || row.sourceHash != nil {
			return nil, ErrOpeningRepository
		}
	} else {
		source := input.Source
		if !validOpeningRetainedSource(source) || !row.sourceType.Valid || row.sourceType.String != source.Type ||
			!row.sourceID.Valid || row.sourceID.String != source.Id || !row.sourceVersion.Valid ||
			row.sourceVersion.Int64 != int64(source.Revision) || !bytes.Equal(row.sourceHash, source.ContentHash) {
			return nil, ErrOpeningRepository
		}
	}
	return input, nil
}

func openingRetainedDateMatches(value *tammyv1.CivilDate, retained sql.NullString) bool {
	if value == nil {
		return !retained.Valid
	}
	if !retained.Valid || value.Year < 1900 || value.Year > 9999 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return false
	}
	date := time.Date(int(value.Year), time.Month(value.Month), int(value.Day), 0, 0, 0, 0, time.UTC)
	return date.Year() == int(value.Year) && int32(date.Month()) == value.Month && date.Day() == int(value.Day) &&
		retained.String == fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
}

func openingRetainedMoneyMatches(value *tammyv1.Money, retained sql.NullInt64) bool {
	if value == nil {
		return !retained.Valid
	}
	return retained.Valid && value.CurrencyCode == "AUD" && value.MinorUnits == retained.Int64
}

func validOpeningRetainedSource(source *tammyv1.SourceRef) bool {
	if source == nil || !ids.IsCanonicalV7(source.Id) || source.Revision == 0 || source.Revision > math.MaxInt64 ||
		len(source.ContentHash) != sha256.Size || len(source.Type) == 0 || len(source.Type) > 64 || source.Type[0] < 'a' || source.Type[0] > 'z' {
		return false
	}
	for _, character := range source.Type {
		if character != '_' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}
