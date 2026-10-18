package invoices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const maxBASScalarKeys = maxBASReportingBytes / 512

type BASScalarKeyKind int64

const (
	BASScalarInvoice BASScalarKeyKind = iota + 1
	BASScalarPosting
	BASScalarIncomeLine
	BASScalarReceipt
	BASScalarAllocation
)

// BASScalarSummary reserves candidate identities only, without body or
// complete-journal proofs. Physical keys and each family count are distinct
// from the coordinator's final semantic coverage count. ScalarBytes charges
// selected TEXT/BLOB cells plus 512 bytes per row, not Go heap size.
type BASScalarSummary struct {
	OrganisationID                                                          string
	RootCount, PostingCount, IncomeLineCount, ReceiptCount, AllocationCount int64
	ScalarKeyCount, ScalarBytes                                             int64
}

// BASScalarKey preserves one physical family/composite identity. The family
// determines which optional fields exist: root owns RootVersion; posting
// retains PostingInvoiceVersion, nullable JournalID and SourceHash; income
// line owns Sequence; receipt owns ReceiptID, JournalID and SourceHash;
// allocation owns ReceiptID and Sequence. These source hashes/versions do
// not become child rows' own original provenance or imply tax-fact ownership.
// There is no retained per-child journal-line mapping in this projection.
// IDs are exact opaque SQL keys, not validated domain references. Ordinary
// invoice/receipt/journal readers still validate canonical IDs and complete
// bodies; a scalar key cannot establish that those proofs are valid.
type BASScalarKey struct {
	Kind                                         BASScalarKeyKind
	OrganisationID, InvoiceID                    string
	ReceiptID                                    *string
	Sequence, RootVersion, PostingInvoiceVersion *uint64
	JournalID                                    *string
	SourceHash                                   []byte
}

type basScalarFamilySQL struct {
	kind                                                                                       BASScalarKeyKind
	organisation, invoice, receipt, sequence, rootVersion, postingVersion, journal, sourceHash string
	from, predicate, invalid                                                                   string
}

// Unattributable child rows have no organisation column. Include them and
// fail closed; an inner join or an inferred owner would silently lose them.
func basScalarRowsSQL() string {
	families := []basScalarFamilySQL{
		{
			kind: BASScalarInvoice, organisation: "invoice.organisation_id", invoice: "invoice.id", rootVersion: "invoice.version",
			from:      `invoices_v1 AS invoice LEFT JOIN organisations AS owner ON owner.id=invoice.organisation_id`,
			predicate: basInvoiceOwnerScopeSQL,
			invalid:   `owner.id IS NULL OR typeof(invoice.organisation_id)!='text' OR invoice.id IS NULL OR invoice.id='' OR typeof(invoice.version)!='integer' OR invoice.version<1`,
		},
		{
			kind: BASScalarPosting, organisation: "posting.organisation_id", invoice: "posting.invoice_id", postingVersion: "posting.invoice_version", journal: "posting.journal_id", sourceHash: "posting.source_hash",
			from: `invoice_income_postings_v1 AS posting
			 LEFT JOIN invoices_v1 AS invoice ON invoice.id=posting.invoice_id
			 LEFT JOIN organisations AS owner ON owner.id=posting.organisation_id
			 LEFT JOIN journals AS journal ON journal.id=posting.journal_id`,
			predicate: basInvoicePostingOwnerScopeSQL,
			invalid: `owner.id IS NULL OR typeof(posting.organisation_id)!='text' OR invoice.id IS NULL OR posting.invoice_id=''
			 OR invoice.organisation_id!=posting.organisation_id OR typeof(posting.invoice_version)!='integer' OR posting.invoice_version<1
			 OR typeof(posting.invoice_total_minor)!='integer' OR ((posting.invoice_total_minor=0)!=(posting.journal_id IS NULL))
			 OR posting.source_hash IS NULL OR length(CAST(posting.source_hash AS BLOB))!=32
			 OR (posting.journal_id IS NOT NULL AND (posting.journal_id='' OR journal.id IS NULL OR journal.organisation_id!=posting.organisation_id))`,
		},
		{
			kind: BASScalarIncomeLine, organisation: "posting.organisation_id", invoice: "income_line.invoice_id", sequence: "income_line.sequence",
			from:      basInvoiceIncomeLineFromSQL,
			predicate: basInvoiceIncomeLineScopeSQL,
			invalid:   basInvoiceIncomeLineInvalidLinkSQL,
		},
		{
			kind: BASScalarReceipt, organisation: "receipt.organisation_id", invoice: "receipt.invoice_id", receipt: "receipt.id", journal: "receipt.journal_id", sourceHash: "receipt.source_hash",
			from: `invoice_payment_v1 AS receipt
			 LEFT JOIN invoice_income_postings_v1 AS posting ON posting.invoice_id=receipt.invoice_id
			 LEFT JOIN invoices_v1 AS invoice ON invoice.id=receipt.invoice_id
			 LEFT JOIN organisations AS owner ON owner.id=receipt.organisation_id
			 LEFT JOIN journals AS journal ON journal.id=receipt.journal_id`,
			predicate: basInvoiceReceiptOwnerScopeSQL + ` OR posting.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR typeof(receipt.organisation_id)!='text' OR posting.invoice_id IS NULL OR invoice.id IS NULL OR receipt.id IS NULL OR receipt.id='' OR receipt.invoice_id=''
			 OR invoice.organisation_id!=receipt.organisation_id OR posting.organisation_id!=receipt.organisation_id
			 OR receipt.journal_id IS NULL OR receipt.journal_id='' OR journal.id IS NULL OR journal.organisation_id!=receipt.organisation_id
			 OR receipt.source_hash IS NULL OR length(CAST(receipt.source_hash AS BLOB))!=32`,
		},
		{
			kind: BASScalarAllocation, organisation: "receipt.organisation_id", invoice: "receipt.invoice_id", receipt: "allocation.payment_id", sequence: "allocation.sequence",
			from:      basInvoiceAllocationFromSQL,
			predicate: basInvoiceAllocationScopeSQL,
			invalid:   basInvoiceAllocationInvalidLinkSQL,
		},
	}
	parts := make([]string, 0, len(families))
	for _, family := range families {
		nullable := func(expression string) string {
			if expression == "" {
				return "NULL"
			}
			return expression
		}
		receipt, journal, hash := nullable(family.receipt), nullable(family.journal), nullable(family.sourceHash)
		parts = append(parts, fmt.Sprintf(`SELECT %d AS key_kind, %s AS organisation_id, %s AS invoice_id,
		 %s AS receipt_id, %s AS sequence, %s AS root_version, %s AS posting_version,
		 %s AS journal_id, %s AS source_hash, %s AS scalar_bytes,
		 CASE WHEN (%s) THEN 1 ELSE 0 END AS invalid_scalar
		 FROM %s CROSS JOIN requested AS target WHERE (%s)`,
			family.kind, family.organisation, family.invoice, receipt, nullable(family.sequence), nullable(family.rootVersion), nullable(family.postingVersion), journal, hash,
			reportingPayloadSize(family.organisation, family.invoice, receipt, journal, hash), family.invalid, family.from, family.predicate))
	}
	return strings.Join(parts, "\nUNION ALL\n")
}

const basScalarOrderSQL = `key_kind,invoice_id,receipt_id,sequence`

// SummarizeBASKeys counts all retained families without any state/date filter
// or typed-key/body read. Root and receipt ceilings are enforced separately.
// Every expected owner/parent is checked before a successful reservation.
// Reads and the transaction lifecycle stay with the caller's executor.
func (port *ReportingPort) SummarizeBASKeys(ctx context.Context, organisationID string) (*BASScalarSummary, error) {
	if port == nil || port.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	rows, err := port.executor.QueryContext(ctx, `/* BAS invoice scalar summary */
	 WITH requested AS (SELECT ? AS organisation_id), scalar_rows AS (`+basScalarRowsSQL()+`),
	 bounded_rows AS (SELECT * FROM scalar_rows ORDER BY `+basScalarOrderSQL+` LIMIT ?)
	 SELECT coalesce(sum(key_kind=1),0),coalesce(sum(key_kind=2),0),coalesce(sum(key_kind=3),0),
	 coalesce(sum(key_kind=4),0),coalesce(sum(key_kind=5),0),count(*),total(scalar_bytes),coalesce(sum(invalid_scalar),0)
	 FROM bounded_rows`, organisationID, maxBASScalarKeys+1)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	if !rows.Next() {
		return nil, errors.Join(ErrRepository, sql.ErrNoRows, rows.Err(), rows.Close())
	}
	summary := BASScalarSummary{OrganisationID: organisationID}
	var scalarBytes float64
	var invalid int64
	if err := rows.Scan(&summary.RootCount, &summary.PostingCount, &summary.IncomeLineCount, &summary.ReceiptCount, &summary.AllocationCount, &summary.ScalarKeyCount, &scalarBytes, &invalid); err != nil {
		return nil, errors.Join(ErrRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrRepository, errors.New("invoice scalar summary returned multiple totals"), rows.Close())
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	for _, bound := range []struct {
		count, limit int64
		resource     string
	}{{summary.RootCount, maxBASReportingInvoices, "invoices"}, {summary.ReceiptCount, maxBASReportingReceipts, "receipts"}, {summary.ScalarKeyCount, maxBASScalarKeys, "BAS scalar keys"}} {
		if bound.count > bound.limit {
			return nil, &SourceBoundError{Resource: bound.resource, Limit: bound.limit}
		}
	}
	if scalarBytes < 0 || math.IsNaN(scalarBytes) || math.IsInf(scalarBytes, 0) || math.Trunc(scalarBytes) != scalarBytes {
		return nil, fmt.Errorf("%w: invalid invoice scalar byte total", ErrRepository)
	}
	if scalarBytes > maxBASReportingBytes {
		return nil, &SourceBoundError{Resource: "BAS scalar bytes", Limit: maxBASReportingBytes}
	}
	if invalid != 0 {
		return nil, fmt.Errorf("%w: invoice scalar keys contain invalid storage or parent links", ErrRepository)
	}
	summary.ScalarBytes = int64(scalarBytes)
	return &summary, nil
}

// ReadBASKeys verifies a prior size reservation and returns only bounded
// candidate identities. The coordinator must apply every module's shared
// fences before this call; this method does not reserve complete body proofs
// or decide GST eligibility. The summary is not a content fingerprint.
func (port *ReportingPort) ReadBASKeys(ctx context.Context, organisationID string, expected BASScalarSummary, maximumKeyCount, maximumScalarBytes int64) ([]BASScalarKey, error) {
	if port == nil || port.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || expected.OrganisationID != organisationID || maximumKeyCount < 0 || maximumScalarBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	for _, count := range []int64{expected.RootCount, expected.PostingCount, expected.IncomeLineCount, expected.ReceiptCount, expected.AllocationCount, expected.ScalarKeyCount, expected.ScalarBytes} {
		if count < 0 {
			return nil, ErrRepository
		}
	}
	if maximumKeyCount > maxBASScalarKeys {
		maximumKeyCount = maxBASScalarKeys
	}
	maximumScalarBytes = normalizeBASReportingPayloadBudget(maximumScalarBytes)
	if expected.ScalarKeyCount > maximumKeyCount {
		return nil, &SourceBoundError{Resource: "BAS scalar keys", Limit: maximumKeyCount}
	}
	if expected.ScalarBytes > maximumScalarBytes {
		return nil, &SourceBoundError{Resource: "BAS scalar bytes", Limit: maximumScalarBytes}
	}
	actual, err := port.SummarizeBASKeys(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if *actual != expected {
		return nil, fmt.Errorf("%w: invoice scalar reservation changed before key read", ErrRepository)
	}
	// Window guards withhold malformed, cumulative-overflow and sentinel
	// cells before the driver copies them or renders a conversion error.
	rows, err := port.executor.QueryContext(ctx, `/* BAS invoice scalar keys */
	 WITH requested AS (SELECT ? AS organisation_id), scalar_rows AS (`+basScalarRowsSQL()+`),
	 bounded_rows AS (SELECT * FROM scalar_rows ORDER BY `+basScalarOrderSQL+` LIMIT ?),
	 guarded_rows AS (SELECT *, invalid_scalar=0
	 AND sum(scalar_bytes) OVER (ORDER BY `+basScalarOrderSQL+` ROWS UNBOUNDED PRECEDING)<=?
	 AND row_number() OVER (ORDER BY `+basScalarOrderSQL+`)<=? AS admissible FROM bounded_rows)
	 SELECT scalar_bytes,invalid_scalar,key_kind,
	 CASE WHEN admissible THEN organisation_id END,CASE WHEN admissible THEN invoice_id END,
	 CASE WHEN admissible THEN receipt_id END,CASE WHEN admissible THEN sequence END,
	 CASE WHEN admissible THEN root_version END,CASE WHEN admissible THEN posting_version END,
	 CASE WHEN admissible THEN journal_id END,CASE WHEN admissible THEN source_hash END
	 FROM guarded_rows ORDER BY `+basScalarOrderSQL,
		organisationID, expected.ScalarKeyCount+1, expected.ScalarBytes, expected.ScalarKeyCount)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	keys := make([]BASScalarKey, 0)
	var charged int64
	var counts [5]int64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		if int64(len(keys)) == expected.ScalarKeyCount {
			return nil, errors.Join(&SourceBoundError{Resource: "BAS scalar keys", Limit: expected.ScalarKeyCount}, rows.Close())
		}
		key, rowBytes, scanErr := scanBASScalarKey(rows, expected.ScalarBytes-charged, expected.ScalarBytes)
		if scanErr != nil {
			return nil, errors.Join(scanErr, rows.Close())
		}
		if key.OrganisationID != organisationID || len(keys) > 0 && !basScalarKeyLess(keys[len(keys)-1], key) {
			return nil, errors.Join(fmt.Errorf("%w: invoice scalar ownership or ordering changed", ErrRepository), rows.Close())
		}
		charged += rowBytes
		counts[int(key.Kind)-1]++
		keys = append(keys, key)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if int64(len(keys)) != expected.ScalarKeyCount || charged != expected.ScalarBytes || counts != [5]int64{expected.RootCount, expected.PostingCount, expected.IncomeLineCount, expected.ReceiptCount, expected.AllocationCount} {
		return nil, fmt.Errorf("%w: invoice scalar family counts or bytes changed during key read", ErrRepository)
	}
	return keys, nil
}

func scanBASScalarKey(rows *sql.Rows, remainingBytes, reservationBytes int64) (BASScalarKey, int64, error) {
	var key BASScalarKey
	var projectedBytes, invalid int64
	var organisation, invoice, receipt, journal sql.NullString
	var sequence, rootVersion, postingVersion sql.NullInt64
	var hash []byte
	if err := rows.Scan(&projectedBytes, &invalid, &key.Kind, &organisation, &invoice, &receipt, &sequence, &rootVersion, &postingVersion, &journal, &hash); err != nil {
		return key, 0, errors.Join(ErrRepository, err)
	}
	if projectedBytes > remainingBytes {
		return key, 0, &SourceBoundError{Resource: "BAS scalar bytes", Limit: reservationBytes}
	}
	if projectedBytes < 512 || invalid != 0 || !organisation.Valid || !invoice.Valid || organisation.String == "" || invoice.String == "" {
		return key, 0, fmt.Errorf("%w: invalid invoice scalar storage or parent", ErrRepository)
	}
	key.OrganisationID, key.InvoiceID = organisation.String, invoice.String
	if receipt.Valid {
		value := receipt.String
		key.ReceiptID = &value
	}
	if journal.Valid {
		value := journal.String
		key.JournalID = &value
	}
	for _, value := range []struct {
		raw    sql.NullInt64
		target **uint64
	}{{sequence, &key.Sequence}, {rootVersion, &key.RootVersion}, {postingVersion, &key.PostingInvoiceVersion}} {
		if value.raw.Valid {
			if value.raw.Int64 < 1 {
				return key, 0, ErrRepository
			}
			copied := uint64(value.raw.Int64)
			*value.target = &copied
		}
	}
	key.SourceHash = bytes.Clone(hash)
	valid := false
	switch key.Kind {
	case BASScalarInvoice:
		valid = key.RootVersion != nil && key.PostingInvoiceVersion == nil && key.Sequence == nil && key.ReceiptID == nil && key.JournalID == nil && hash == nil
	case BASScalarPosting:
		valid = key.RootVersion == nil && key.PostingInvoiceVersion != nil && key.Sequence == nil && key.ReceiptID == nil && len(hash) == sha256.Size && (key.JournalID == nil || *key.JournalID != "")
	case BASScalarIncomeLine:
		valid = key.RootVersion == nil && key.PostingInvoiceVersion == nil && key.Sequence != nil && key.ReceiptID == nil && key.JournalID == nil && hash == nil
	case BASScalarReceipt:
		valid = key.RootVersion == nil && key.PostingInvoiceVersion == nil && key.Sequence == nil && key.ReceiptID != nil && *key.ReceiptID != "" && key.JournalID != nil && *key.JournalID != "" && len(hash) == sha256.Size
	case BASScalarAllocation:
		valid = key.RootVersion == nil && key.PostingInvoiceVersion == nil && key.Sequence != nil && key.ReceiptID != nil && *key.ReceiptID != "" && key.JournalID == nil && hash == nil
	}
	if !valid {
		return key, 0, fmt.Errorf("%w: invalid invoice scalar family shape", ErrRepository)
	}
	var charged int64
	receiptBytes, journalBytes := 0, 0
	if key.ReceiptID != nil {
		receiptBytes = len(*key.ReceiptID)
	}
	if key.JournalID != nil {
		journalBytes = len(*key.JournalID)
	}
	if !addBASReportingRowPayload(&charged, len(key.OrganisationID), len(key.InvoiceID), receiptBytes, journalBytes, len(hash)) || charged != projectedBytes {
		return key, 0, fmt.Errorf("%w: invoice scalar row charge changed", ErrRepository)
	}
	return key, charged, nil
}

func basScalarKeyLess(left, right BASScalarKey) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.InvoiceID != right.InvoiceID {
		return left.InvoiceID < right.InvoiceID
	}
	value := func(input *string) string {
		if input == nil {
			return ""
		}
		return *input
	}
	if value(left.ReceiptID) != value(right.ReceiptID) {
		return value(left.ReceiptID) < value(right.ReceiptID)
	}
	sequence := func(input *uint64) uint64 {
		if input == nil {
			return 0
		}
		return *input
	}
	return sequence(left.Sequence) < sequence(right.Sequence)
}
