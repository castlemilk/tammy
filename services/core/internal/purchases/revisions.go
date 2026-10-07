package purchases

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/revisions"
	"google.golang.org/protobuf/proto"
)

var ErrPurchaseRevision = errors.New("purchases: invalid command revision or event")

const purchaseService = "tammy.v1.PurchaseService."

func PurchaseRevisionDomains(commandType string, journalPosted bool) (revisions.Domains, error) {
	domains := revisions.Domains{TaxSource: true, Ledger: journalPosted}
	switch commandType {
	case purchaseService + "PostReviewedPurchase":
	case purchaseService + "RecordPurchasePayment":
		if !journalPosted {
			return revisions.Domains{}, ErrPurchaseRevision
		}
		domains.Settlement = true
	case purchaseService + "AttributePurchaseGst":
		if !journalPosted {
			return revisions.Domains{}, ErrPurchaseRevision
		}
	case purchaseService + "RecordPurchaseTaxEvidence", purchaseService + "DeferPurchaseGstClaim", purchaseService + "ResolveLegacyPurchaseReview":
		if journalPosted {
			return revisions.Domains{}, ErrPurchaseRevision
		}
	default:
		return revisions.Domains{}, ErrPurchaseRevision
	}
	return domains, nil
}

// ReservePurchaseRevision is the only purchase reservation. Journal writers use
// Snapshot.Financial directly and never reserve a second ledger-only revision.
func ReservePurchaseRevision(ctx context.Context, executor app.CommandSQLExecutor, operationKey, commandType string, journalPosted bool, now time.Time) (revisions.Snapshot, error) {
	if ctx == nil || executor == nil || now.IsZero() {
		return revisions.Snapshot{}, ErrPurchaseRevision
	}
	domains, err := PurchaseRevisionDomains(commandType, journalPosted)
	if err != nil {
		return revisions.Snapshot{}, err
	}
	repository, err := revisions.NewWithExecutor(executor.ExecContext, func(ctx context.Context, query string, args ...any) revisions.RowScanner {
		rows, err := executor.QueryContext(ctx, query, args...)
		return purchaseRevisionRow{rows, err}
	}, clock.Func(func() time.Time { return now }))
	if err != nil {
		return revisions.Snapshot{}, err
	}
	snapshot, _, err := repository.Bump(ctx, operationKey, domains)
	return snapshot, err
}

type AccountingEvent struct {
	OperationKey, OrganisationID, ScopeKind, ScopeID, CommandType string
	AccountingVersion                                             uint64
	Revision                                                      revisions.Snapshot
	Result                                                        proto.Message
	JournalID                                                     *string
	Now                                                           time.Time
}

func RecordAccountingEvent(ctx context.Context, executor app.CommandSQLExecutor, event AccountingEvent) error {
	if ctx == nil || executor == nil || !ids.IsCanonicalV7(event.OperationKey) || !ids.IsCanonicalV7(event.OrganisationID) || !ids.IsCanonicalV7(event.ScopeID) ||
		event.AccountingVersion == 0 || event.AccountingVersion > math.MaxInt64 || event.Now.IsZero() || event.Result == nil || !event.Result.ProtoReflect().IsValid() ||
		event.Revision.Financial == 0 || event.Revision.Financial > math.MaxInt64 || event.Revision.Ledger > math.MaxInt64 || event.Revision.TaxSource == 0 || event.Revision.TaxSource > math.MaxInt64 || event.Revision.Settlement > math.MaxInt64 ||
		(event.JournalID != nil && !ids.IsCanonicalV7(*event.JournalID)) {
		return ErrPurchaseRevision
	}
	domains, err := PurchaseRevisionDomains(event.CommandType, event.JournalID != nil)
	if err != nil {
		return err
	}
	responseTypes := map[string]string{
		purchaseService + "PostReviewedPurchase":        "tammy.v1.PostReviewedPurchaseResponse",
		purchaseService + "RecordPurchasePayment":       "tammy.v1.RecordPurchasePaymentResponse",
		purchaseService + "RecordPurchaseTaxEvidence":   "tammy.v1.RecordPurchaseTaxEvidenceResponse",
		purchaseService + "AttributePurchaseGst":        "tammy.v1.AttributePurchaseGstResponse",
		purchaseService + "DeferPurchaseGstClaim":       "tammy.v1.DeferPurchaseGstClaimResponse",
		purchaseService + "ResolveLegacyPurchaseReview": "tammy.v1.ResolveLegacyPurchaseReviewResponse",
	}
	if string(event.Result.ProtoReflect().Descriptor().FullName()) != responseTypes[event.CommandType] {
		return ErrPurchaseRevision
	}
	var postingID, documentID, openingID any
	switch event.ScopeKind {
	case "POSTING":
		postingID = event.ScopeID
	case "DOCUMENT":
		documentID = event.ScopeID
	case "OPENING_PAYABLE":
		openingID = event.ScopeID
	default:
		return ErrPurchaseRevision
	}
	if event.CommandType != purchaseService+"ResolveLegacyPurchaseReview" && event.ScopeKind != "POSTING" {
		return ErrPurchaseRevision
	}
	var mask uint8
	var financial, ledger, taxSource, settlement uint64
	rows, err := executor.QueryContext(ctx, `SELECT domain_mask,financial_revision,ledger_revision,tax_source_revision,settlement_revision FROM financial_revision_claims WHERE operation_key=?`, event.OperationKey)
	if err != nil {
		return errors.Join(ErrPurchaseRevision, err)
	}
	if err := (purchaseRevisionRow{rows: rows}).Scan(&mask, &financial, &ledger, &taxSource, &settlement); err != nil {
		return errors.Join(ErrPurchaseRevision, err)
	}
	expectedMask := uint8(8) // Tax source changes for every purchase command.
	if domains.Ledger {
		expectedMask |= 1
	}
	if domains.Settlement {
		expectedMask |= 2
	}
	if mask != expectedMask || financial != event.Revision.Financial || ledger != event.Revision.Ledger ||
		taxSource != event.Revision.TaxSource || settlement != event.Revision.Settlement {
		return ErrPurchaseRevision
	}
	if event.JournalID != nil {
		if err := checkEventFinancialRevision(ctx, executor, `SELECT organisation_id,financial_revision FROM journals WHERE id=?`, *event.JournalID, event); err != nil {
			return err
		}
	}
	if event.CommandType == purchaseService+"PostReviewedPurchase" {
		if err := checkEventFinancialRevision(ctx, executor, `SELECT organisation_id,financial_revision FROM purchase_postings_v1 WHERE id=?`, event.ScopeID, event); err != nil {
			return err
		}
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(event.Result)
	if err != nil || len(encoded) == 0 || len(encoded) > 1<<20 {
		return ErrPurchaseRevision
	}
	hash := sha256.Sum256(encoded)
	var journalID any
	if event.JournalID != nil {
		journalID = *event.JournalID
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO purchase_accounting_events_v1(operation_key,organisation_id,scope_kind,scope_id,posting_id,document_id,opening_payable_id,accounting_version,command_type,financial_revision,ledger_revision,tax_source_revision,settlement_revision,result_sha256,result_proto,journal_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.OperationKey, event.OrganisationID, event.ScopeKind, event.ScopeID, postingID, documentID, openingID, event.AccountingVersion, event.CommandType, event.Revision.Financial, event.Revision.Ledger, event.Revision.TaxSource, event.Revision.Settlement, hash[:], encoded, journalID, event.Now.UTC().Format(time.RFC3339Nano))
	return err
}

func checkEventFinancialRevision(ctx context.Context, executor app.CommandSQLExecutor, query, sourceID string, event AccountingEvent) error {
	rows, err := executor.QueryContext(ctx, query, sourceID)
	if err != nil {
		return errors.Join(ErrPurchaseRevision, err)
	}
	var organisationID string
	var financialRevision uint64
	if err := (purchaseRevisionRow{rows: rows}).Scan(&organisationID, &financialRevision); err != nil {
		return errors.Join(ErrPurchaseRevision, err)
	}
	if organisationID != event.OrganisationID || financialRevision != event.Revision.Financial {
		return ErrPurchaseRevision
	}
	return nil
}

type purchaseRevisionRow struct {
	rows *sql.Rows
	err  error
}

func (row purchaseRevisionRow) Scan(values ...any) (scanErr error) {
	if row.err != nil {
		return row.err
	}
	if row.rows == nil {
		return ErrPurchaseRevision
	}
	defer func() { scanErr = errors.Join(scanErr, row.rows.Close()) }()
	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := row.rows.Scan(values...); err != nil {
		return err
	}
	if row.rows.Next() {
		return ErrPurchaseRevision
	}
	return row.rows.Err()
}
