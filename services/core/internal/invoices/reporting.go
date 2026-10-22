package invoices

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math"
	"sort"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const (
	maxBASReportingInvoices = 1000
	maxBASReportingReceipts = 1000
	maxBASReportingBytes    = 8 * 1024 * 1024
	basReportingHashTag     = "tammy/invoices/bas-reporting-capture/v1"
)

// BLOB matches enter owner validation only; no accepted identity is normalized.
// TEXT equality alone would omit this corruption before storage checks run.
const basInvoiceOwnerScopeSQL = `invoice.organisation_id=target.organisation_id OR
 (typeof(invoice.organisation_id)='blob' AND length(invoice.organisation_id)=36
  AND invoice.organisation_id=CAST(target.organisation_id AS BLOB))`

var ErrSourceBound = errors.New("invoices: reporting source exceeds its bound")

type SourceBoundError struct {
	Resource string
	Limit    int64
}

func (err *SourceBoundError) Error() string {
	if err == nil {
		return ErrSourceBound.Error()
	}
	return fmt.Sprintf("%s: %s exceeds limit %d", ErrSourceBound, err.Resource, err.Limit)
}

func (err *SourceBoundError) Unwrap() error { return ErrSourceBound }

type ReportingPort struct {
	executor app.CommandSQLExecutor
	journals *accounting.JournalRepository
}

type BASReportingCapture struct {
	Candidates          []BASInvoiceCandidate
	Fingerprint         []byte
	CandidateCount      int
	ReceiptCount        int
	JournalCount        int
	JournalIDs          []string
	BasePayloadBytes    int64
	JournalPayloadBytes map[string]int64
	PayloadBytes        int64
}

// BASReportingPreflight is the exact read-only reservation for the complete
// organisation invoice census. JournalPayloadBytes contains one charge for
// each sorted, unique complete journal parent.
type BASReportingPreflight struct {
	CandidateCount      int
	ReceiptCount        int
	JournalCount        int
	JournalIDs          []string
	BasePayloadBytes    int64
	JournalPayloadBytes map[string]int64
	PayloadBytes        int64
}

type basReportingReservation struct {
	BASReportingPreflight

	invoiceRows, postingRows, incomeLineRows    int64
	receiptRows, allocationRows                 int64
	invoicePayloadBytes, postingPayloadBytes    int64
	incomeLinePayloadBytes, receiptPayloadBytes int64
	allocationPayloadBytes                      int64
}

type BASInvoiceCandidate struct {
	ID, OrganisationID string
	Version            uint64
	State, Number      string
	CreatedAt          string
	UpdatedAt          string
	Invoice            *tammyv1.Invoice
	InvoiceProto       []byte
	InvoiceHash        []byte
	PeriodDisposition  string
	Posting            *BASInvoicePosting
	Receipts           []BASInvoiceReceipt
}

type BASInvoicePosting struct {
	InvoiceID, OrganisationID string
	InvoiceVersion            uint64
	InvoiceDate               string
	GSTBasis                  tammyv1.GstBasis
	RevenueAccountID          string
	JournalID                 *string
	InvoiceNetMinor           int64
	InvoiceGSTMinor           int64
	InvoiceTotalMinor         int64
	BASSalesGrossMinor        int64
	ProfileVersion            uint64
	BasisAuditSequence        uint64
	FinancialRevision         uint64
	SourceHash                []byte
	CreatedAt                 string
	IncomeLines               []BASInvoiceIncomeLine
	Journal                   *accounting.JournalReportingRecord
}

type BASInvoiceIncomeLine struct {
	InvoiceID  string
	Sequence   uint64
	GrossMinor int64
	NetMinor   int64
	GSTMinor   int64
	Treatment  tammyv1.TaxTreatment
}

type BASInvoiceReceipt struct {
	ID, OrganisationID, InvoiceID string
	PaymentDate                   string
	AmountMinor                   int64
	AllocatedGSTMinor             int64
	BASSalesGrossMinor            int64
	BankAccountID                 string
	JournalID                     string
	Reference                     string
	PaymentProto                  []byte
	PaymentHash                   []byte
	SourceHash                    []byte
	CreatedAt                     string
	Payment                       *tammyv1.InvoicePayment
	Allocations                   []BASInvoicePaymentLine
	Journal                       *accounting.JournalReportingRecord
}

type BASInvoicePaymentLine struct {
	PaymentID  string
	Sequence   uint64
	GrossMinor int64
	GSTMinor   int64
}

func NewReportingPort(executor app.CommandSQLExecutor) (*ReportingPort, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	journals, err := accounting.NewJournalRepository(executor)
	if err != nil {
		return nil, err
	}
	return &ReportingPort{executor: executor, journals: journals}, nil
}

// CaptureBAS returns the complete retained invoice and receipt census for an
// organisation. PeriodDisposition describes invoice issue-date coverage only;
// settlement timing and BAS eligibility remain the responsibility of the
// combined preparation producer. Reads stay within the caller-owned executor.
func (port *ReportingPort) CaptureBAS(
	ctx context.Context,
	organisationID string,
	period *tammyv1.BasCivilDateRange,
) (*BASReportingCapture, error) {
	return port.CaptureBASWithPayloadBudget(ctx, organisationID, period, int64(maxBASReportingBytes))
}

// PreflightBASWithPayloadBudget reserves the complete invoice and receipt
// census, including unique complete journal parents, without loading any
// invoice, payment, journal, or journal-line body.
func (port *ReportingPort) PreflightBASWithPayloadBudget(
	ctx context.Context,
	organisationID string,
	period *tammyv1.BasCivilDateRange,
	maximumPayloadBytes int64,
) (*BASReportingPreflight, error) {
	if port == nil || port.executor == nil || port.journals == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || maximumPayloadBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if _, _, err := reportingPeriodBounds(period); err != nil {
		return nil, err
	}
	reservation, err := port.preflightBASReporting(ctx, organisationID, maximumPayloadBytes)
	if err != nil {
		return nil, err
	}
	metadata := cloneBASReportingPreflight(reservation.BASReportingPreflight)
	return &metadata, nil
}

// CaptureBASWithPayloadBudget loads the invoice census only after a single
// caller-budget reservation covers all invoice, receipt, allocation, and
// unique journal-parent payloads.
func (port *ReportingPort) CaptureBASWithPayloadBudget(
	ctx context.Context,
	organisationID string,
	period *tammyv1.BasCivilDateRange,
	maximumPayloadBytes int64,
) (*BASReportingCapture, error) {
	if port == nil || port.executor == nil || port.journals == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || maximumPayloadBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	startDate, endDate, err := reportingPeriodBounds(period)
	if err != nil {
		return nil, err
	}
	maximumPayloadBytes = normalizeBASReportingPayloadBudget(maximumPayloadBytes)
	reservation, err := port.preflightBASReporting(ctx, organisationID, maximumPayloadBytes)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}

	candidates, err := port.loadBASReportingInvoices(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	candidateByID := make(map[string]*BASInvoiceCandidate, len(candidates))
	for index := range candidates {
		candidate := &candidates[index]
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		if _, duplicate := candidateByID[candidate.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate invoice source %s", ErrRepository, candidate.ID)
		}
		candidate.PeriodDisposition, err = invoiceIssuePeriodDisposition(candidate.Invoice.Draft.IssueDate, startDate, endDate)
		if err != nil {
			return nil, fmt.Errorf("%w: invoice %s has an invalid issue date", ErrRepository, candidate.ID)
		}
		candidateByID[candidate.ID] = candidate
	}

	postings, err := port.loadBASReportingPostings(ctx, organisationID, candidateByID)
	if err != nil {
		return nil, err
	}
	incomeLines, err := port.loadBASReportingIncomeLines(ctx, organisationID, postings)
	if err != nil {
		return nil, err
	}
	receipts, receiptByID, err := port.loadBASReportingReceipts(ctx, organisationID, candidateByID)
	if err != nil {
		return nil, err
	}
	allocations, err := port.loadBASReportingAllocations(ctx, organisationID, receiptByID)
	if err != nil {
		return nil, err
	}
	if err := verifyBASReportingBaseReservation(
		ctx, reservation, candidates, postings, incomeLines, receiptByID, allocations,
	); err != nil {
		return nil, err
	}
	journalCache := make(map[string]*accounting.JournalReportingRecord)
	loadedJournalPayloadBytes := make(map[string]int64, reservation.JournalCount)
	for _, invoice := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		candidate := candidateByID[invoice.ID]
		invoiceID := candidate.ID
		if posting := postings[invoiceID]; posting != nil {
			posting.IncomeLines = incomeLines[invoiceID]
			if err := validateBASInvoicePosting(*candidate, posting); err != nil {
				return nil, err
			}
			if posting.JournalID != nil {
				proof, err := port.reportingJournal(
					ctx, organisationID, *posting.JournalID, journalCache,
					reservation.JournalPayloadBytes, loadedJournalPayloadBytes, maximumPayloadBytes,
				)
				if err != nil {
					return nil, err
				}
				posting.Journal = proof
				if err := validateBASInvoiceJournal(*candidate, posting); err != nil {
					return nil, err
				}
			} else if posting.InvoiceTotalMinor != 0 {
				return nil, fmt.Errorf("%w: nonzero invoice posting %s has no journal", ErrRepository, invoiceID)
			}
			candidate.Posting = posting
		} else if len(incomeLines[invoiceID]) != 0 {
			return nil, fmt.Errorf("%w: invoice %s has income lines without a posting", ErrRepository, invoiceID)
		}
	}

	for _, invoice := range candidates {
		invoiceID := invoice.ID
		invoiceReceipts := receipts[invoiceID]
		if len(invoiceReceipts) == 0 {
			continue
		}
		candidate := candidateByID[invoiceID]
		if candidate == nil || candidate.Posting == nil {
			return nil, fmt.Errorf("%w: receipt history has no retained invoice posting for %s", ErrRepository, invoiceID)
		}
		var cumulativePaid int64
		cumulativeGross := make([]int64, len(candidate.Posting.IncomeLines))
		cumulativeGST := make([]int64, len(candidate.Posting.IncomeLines))
		for index := range invoiceReceipts {
			if err := ctx.Err(); err != nil {
				return nil, errors.Join(ErrRepository, err)
			}
			receipt := &invoiceReceipts[index]
			receipt.Allocations = allocations[receipt.ID]
			if err := validateBASInvoiceReceipt(*candidate, *candidate.Posting, receipt); err != nil {
				return nil, err
			}
			var ok bool
			if cumulativePaid, ok = checkedBASAdd(cumulativePaid, receipt.AmountMinor); !ok ||
				cumulativePaid > candidate.Posting.InvoiceTotalMinor {
				return nil, fmt.Errorf("%w: receipt totals exceed invoice %s", ErrRepository, invoiceID)
			}
			for allocationIndex, allocation := range receipt.Allocations {
				line := candidate.Posting.IncomeLines[allocationIndex]
				if cumulativeGross[allocationIndex], ok = checkedBASAdd(cumulativeGross[allocationIndex], allocation.GrossMinor); !ok ||
					cumulativeGross[allocationIndex] > line.GrossMinor {
					return nil, fmt.Errorf("%w: receipts over-allocate invoice %s line %d gross", ErrRepository, invoiceID, allocationIndex+1)
				}
				if cumulativeGST[allocationIndex], ok = checkedBASAdd(cumulativeGST[allocationIndex], allocation.GSTMinor); !ok ||
					cumulativeGST[allocationIndex] > line.GSTMinor {
					return nil, fmt.Errorf("%w: receipts over-allocate invoice %s line %d GST", ErrRepository, invoiceID, allocationIndex+1)
				}
			}
			proof, err := port.reportingJournal(
				ctx, organisationID, receipt.JournalID, journalCache,
				reservation.JournalPayloadBytes, loadedJournalPayloadBytes, maximumPayloadBytes,
			)
			if err != nil {
				return nil, err
			}
			receipt.Journal = proof
			if err := validateBASReceiptJournal(*candidate.Posting, receipt); err != nil {
				return nil, err
			}
		}
		candidate.Receipts = invoiceReceipts
	}
	for receiptID := range allocations {
		if receiptByID[receiptID] == nil {
			return nil, fmt.Errorf("%w: allocations refer to missing receipt %s", ErrRepository, receiptID)
		}
	}
	if err := verifyBASReportingParentCharges(reservation, loadedJournalPayloadBytes); err != nil {
		return nil, err
	}
	ordered := make([]BASInvoiceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		ordered = append(ordered, *candidateByID[candidate.ID])
	}
	fingerprint, err := basReportingFingerprint(ctx, organisationID, startDate, endDate, ordered)
	if err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	metadata := cloneBASReportingPreflight(reservation.BASReportingPreflight)
	return &BASReportingCapture{
		Candidates: ordered, Fingerprint: fingerprint,
		CandidateCount: metadata.CandidateCount, ReceiptCount: metadata.ReceiptCount,
		JournalCount: metadata.JournalCount, JournalIDs: metadata.JournalIDs,
		BasePayloadBytes:    metadata.BasePayloadBytes,
		JournalPayloadBytes: metadata.JournalPayloadBytes, PayloadBytes: metadata.PayloadBytes,
	}, nil
}

func reportingPeriodBounds(period *tammyv1.BasCivilDateRange) (string, string, error) {
	if period == nil {
		return "", "", ErrRepository
	}
	start, okStart := reportingCivilDate(period.StartDate)
	end, okEnd := reportingCivilDate(period.EndDate)
	if !okStart || !okEnd || start > end {
		return "", "", ErrRepository
	}
	return start, end, nil
}

func reportingCivilDate(value *tammyv1.CivilDate) (string, bool) {
	if value == nil {
		return "", false
	}
	formatted := fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
	return formatted, date(formatted)
}

func invoiceIssuePeriodDisposition(issueDate, startDate, endDate string) (string, error) {
	if issueDate == "" {
		return "UNDATED", nil
	}
	if !date(issueDate) {
		return "", ErrInvalidDraft
	}
	if issueDate < startDate || issueDate > endDate {
		return "OUT_OF_PERIOD", nil
	}
	return "IN_PERIOD", nil
}

func (port *ReportingPort) preflightBASReporting(
	ctx context.Context,
	organisationID string,
	maximumPayloadBytes int64,
) (*basReportingReservation, error) {
	if port == nil || port.executor == nil || port.journals == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || maximumPayloadBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	maximumPayloadBytes = normalizeBASReportingPayloadBudget(maximumPayloadBytes)

	invoiceSize := reportingPayloadSize(
		"invoice.id", "invoice.organisation_id", "invoice.state", "invoice.number", "invoice.invoice_proto",
		"invoice.created_at", "invoice.updated_at",
	)
	invoiceInvalidStorage := reportingInvalidNumericStorageRowSQL("invoice.version") +
		`+CASE WHEN typeof(invoice.organisation_id)!='text' THEN 1 ELSE 0 END`
	invoiceRows, invoiceBytes, err := port.queryBASReportingPayload(
		ctx, "invoices", maxBASReportingInvoices, maximumPayloadBytes,
		"WITH requested AS (SELECT ? AS organisation_id) "+
			"SELECT count(*), total(payload_bytes), coalesce(sum(invalid_storage),0) FROM ("+
			"SELECT "+invoiceSize+" AS payload_bytes, "+invoiceInvalidStorage+" AS invalid_storage FROM invoices_v1 AS invoice "+
			"CROSS JOIN requested AS target WHERE ("+basInvoiceOwnerScopeSQL+") ORDER BY invoice.created_at,invoice.id LIMIT ?)",
		organisationID, maxBASReportingInvoices+1,
	)
	if err != nil {
		return nil, err
	}

	postingSize := reportingPayloadSize(
		"posting.invoice_id", "posting.organisation_id", "posting.invoice_date", "posting.revenue_account_id",
		"posting.journal_id", "posting.source_hash", "posting.created_at",
	)
	postingInvalidStorage := reportingInvalidNumericStorageRowSQL(
		"posting.invoice_version", "posting.gst_basis", "posting.invoice_net_minor", "posting.invoice_gst_minor",
		"posting.invoice_total_minor", "posting.bas_sales_gross_minor", "posting.profile_version",
		"posting.basis_audit_sequence", "posting.financial_revision",
	) + `+CASE WHEN (` + basInvoicePostingInvalidOwnerSQL + `) THEN 1 ELSE 0 END`
	postingRows, postingBytes, err := port.queryBASReportingPayload(
		ctx, "invoice postings", 0, maximumPayloadBytes,
		"WITH requested AS (SELECT ? AS organisation_id) "+
			"SELECT count(*), total("+postingSize+"), coalesce(sum("+postingInvalidStorage+"),0) FROM "+basInvoicePostingOwnerFromSQL+
			" CROSS JOIN requested AS target WHERE ("+basInvoicePostingOwnerScopeSQL+")",
		organisationID,
	)
	if err != nil {
		return nil, err
	}

	incomeLineSize := reportingPayloadSize("income_line.invoice_id")
	incomeLineInvalidStorage := "CASE WHEN (" + basInvoiceIncomeLineInvalidFullSQL + ") THEN 1 ELSE 0 END"
	incomeLineRows, incomeLineBytes, err := port.queryBASReportingPayload(
		ctx, "invoice income lines", 0, maximumPayloadBytes,
		"WITH requested AS (SELECT ? AS organisation_id) "+
			"SELECT count(*), total("+incomeLineSize+"), coalesce(sum("+incomeLineInvalidStorage+"),0) FROM "+basInvoiceIncomeLineFromSQL+
			" CROSS JOIN requested AS target WHERE ("+basInvoiceIncomeLineScopeSQL+")",
		organisationID,
	)
	if err != nil {
		return nil, err
	}

	receiptSize := reportingPayloadSize(
		"receipt.id", "receipt.organisation_id", "receipt.invoice_id", "receipt.payment_date",
		"receipt.bank_account_id", "receipt.journal_id", "receipt.reference", "receipt.payment_proto",
		"receipt.source_hash", "receipt.created_at",
	)
	receiptInvalidStorage := reportingInvalidNumericStorageRowSQL(
		"receipt.amount_minor", "receipt.allocated_gst_minor", "receipt.bas_sales_gross_minor",
	) + `+CASE WHEN (` + basInvoiceReceiptInvalidOwnerSQL + `) THEN 1 ELSE 0 END`
	receiptRows, receiptBytes, err := port.queryBASReportingPayload(
		ctx, "receipts", maxBASReportingReceipts, maximumPayloadBytes,
		"WITH requested AS (SELECT ? AS organisation_id) "+
			"SELECT count(*), total(payload_bytes), coalesce(sum(invalid_storage),0) FROM ("+
			"SELECT "+receiptSize+" AS payload_bytes, "+receiptInvalidStorage+" AS invalid_storage FROM "+basInvoiceReceiptOwnerFromSQL+
			" CROSS JOIN requested AS target WHERE ("+basInvoiceReceiptOwnerScopeSQL+") "+
			"ORDER BY receipt.invoice_id,receipt.payment_date,receipt.created_at,receipt.id LIMIT ?)",
		organisationID, maxBASReportingReceipts+1,
	)
	if err != nil {
		return nil, err
	}

	allocationSize := reportingPayloadSize("allocation.payment_id")
	allocationInvalidStorage := "CASE WHEN (" + basInvoiceAllocationInvalidFullSQL + ") THEN 1 ELSE 0 END"
	allocationRows, allocationBytes, err := port.queryBASReportingPayload(
		ctx, "receipt allocations", 0, maximumPayloadBytes,
		"WITH requested AS (SELECT ? AS organisation_id) "+
			"SELECT count(*), total("+allocationSize+"), coalesce(sum("+allocationInvalidStorage+"),0) FROM "+basInvoiceAllocationFromSQL+
			" CROSS JOIN requested AS target WHERE ("+basInvoiceAllocationScopeSQL+")",
		organisationID,
	)
	if err != nil {
		return nil, err
	}

	basePayloadBytes := int64(0)
	for _, charge := range []int64{
		invoiceBytes, postingBytes, incomeLineBytes, receiptBytes, allocationBytes,
	} {
		var ok bool
		if basePayloadBytes, ok = checkedBASAdd(basePayloadBytes, charge); !ok {
			return nil, fmt.Errorf("%w: invoice reporting base payload overflow", ErrRepository)
		}
		if basePayloadBytes > maximumPayloadBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
	}

	journalIDs, err := port.preflightBASReportingJournalIDs(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	remainingBytes := maximumPayloadBytes - basePayloadBytes
	journalPayloadBytes := make(map[string]int64, len(journalIDs))
	for _, journalID := range journalIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		payloadBytes, err := port.journals.PreflightForReportingWithPayloadBudget(
			ctx, organisationID, journalID, remainingBytes,
		)
		if err != nil {
			return nil, invoiceReportingAccountingReadError(err, maximumPayloadBytes)
		}
		if payloadBytes <= 0 {
			return nil, fmt.Errorf("%w: journal %s has no retained reporting payload", ErrRepository, journalID)
		}
		if payloadBytes > remainingBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
		remainingBytes -= payloadBytes
		journalPayloadBytes[journalID] = payloadBytes
	}

	return &basReportingReservation{
		BASReportingPreflight: BASReportingPreflight{
			CandidateCount: int(invoiceRows), ReceiptCount: int(receiptRows),
			JournalCount: len(journalIDs), JournalIDs: journalIDs,
			BasePayloadBytes: basePayloadBytes, JournalPayloadBytes: journalPayloadBytes,
			PayloadBytes: maximumPayloadBytes - remainingBytes,
		},
		invoiceRows: invoiceRows, postingRows: postingRows, incomeLineRows: incomeLineRows,
		receiptRows: receiptRows, allocationRows: allocationRows,
		invoicePayloadBytes: invoiceBytes, postingPayloadBytes: postingBytes,
		incomeLinePayloadBytes: incomeLineBytes, receiptPayloadBytes: receiptBytes,
		allocationPayloadBytes: allocationBytes,
	}, nil
}

// queryBASReportingPayload reads only row counts, retained SQL byte totals,
// and storage/parent violations.
func (port *ReportingPort) queryBASReportingPayload(
	ctx context.Context,
	resource string,
	rowLimit int64,
	maximumPayloadBytes int64,
	query string,
	arguments ...any,
) (int64, int64, error) {
	rows, err := port.executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return 0, 0, errors.Join(ErrRepository, fmt.Errorf("preflight %s: %w", resource, err), ctx.Err())
	}
	if !rows.Next() {
		rowsErr, closeErr := rows.Err(), rows.Close()
		return 0, 0, errors.Join(ErrRepository, sql.ErrNoRows, rowsErr, closeErr)
	}
	var count, invalidStorageRows int64
	var payloadBytes float64
	if err := rows.Scan(&count, &payloadBytes, &invalidStorageRows); err != nil {
		return 0, 0, errors.Join(ErrRepository, err, rows.Close())
	}
	if rows.Next() {
		rowsErr, closeErr := rows.Err(), rows.Close()
		return 0, 0, errors.Join(ErrRepository, errors.New("preflight returned multiple aggregates"), rowsErr, closeErr)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return 0, 0, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, errors.Join(ErrRepository, err)
	}
	if count < 0 {
		return 0, 0, fmt.Errorf("%w: negative preflight row count for %s", ErrRepository, resource)
	}
	if rowLimit > 0 && count > rowLimit {
		return 0, 0, &SourceBoundError{Resource: resource, Limit: rowLimit}
	}
	if payloadBytes < 0 || math.IsNaN(payloadBytes) || math.IsInf(payloadBytes, 0) ||
		math.Trunc(payloadBytes) != payloadBytes {
		return 0, 0, fmt.Errorf("%w: invalid preflight payload size for %s", ErrRepository, resource)
	}
	if payloadBytes > float64(maximumPayloadBytes) {
		return 0, 0, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	if invalidStorageRows != 0 {
		return 0, 0, fmt.Errorf("%w: %s contain invalid storage", ErrRepository, resource)
	}
	return count, int64(payloadBytes), nil
}

func normalizeBASReportingPayloadBudget(maximumPayloadBytes int64) int64 {
	if maximumPayloadBytes > int64(maxBASReportingBytes) {
		return int64(maxBASReportingBytes)
	}
	return maximumPayloadBytes
}

// preflightBASReportingJournalIDs reads bounded parent identities only. Full
// invoice, receipt, journal, and line bodies remain untouched until every
// unique parent has passed its owner and size preflight.
func (port *ReportingPort) preflightBASReportingJournalIDs(
	ctx context.Context,
	organisationID string,
) ([]string, error) {
	unique := make(map[string]struct{})
	readPostings := func() error {
		rows, err := port.executor.QueryContext(ctx,
			basReportingGuardedChildRowsSQL("posting", basInvoicePostingOwnerFromSQL,
				"("+basInvoicePostingOwnerScopeSQL+") AND (posting.journal_id IS NOT NULL OR ("+basInvoicePostingInvalidOwnerSQL+"))",
				basInvoicePostingInvalidOwnerSQL, "invoice_id", "organisation_id", "journal_id")+
				"SELECT posting.invoice_id,posting.organisation_id,posting.journal_id FROM guarded AS posting "+
				"ORDER BY posting.journal_id,posting.invoice_id LIMIT ?",
			organisationID, maxBASReportingInvoices+1)
		if err != nil {
			return errors.Join(ErrRepository, fmt.Errorf("preflight invoice journal identities: %w", err), ctx.Err())
		}
		var count int64
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err, rows.Close())
			}
			var invoiceID, ownerID, journalID string
			if err := rows.Scan(&invoiceID, &ownerID, &journalID); err != nil {
				return errors.Join(ErrRepository, err, rows.Close())
			}
			count++
			if count > maxBASReportingInvoices {
				return errors.Join(&SourceBoundError{Resource: "invoice journals", Limit: maxBASReportingInvoices}, rows.Close())
			}
			if !ids.IsCanonicalV7(invoiceID) || ownerID != organisationID || journalID == "" {
				return errors.Join(fmt.Errorf("%w: invoice journal identity has an invalid owner or ID", ErrRepository), rows.Close())
			}
			unique[journalID] = struct{}{}
		}
		rowsErr, closeErr := rows.Err(), rows.Close()
		if rowsErr != nil || closeErr != nil {
			return errors.Join(ErrRepository, rowsErr, closeErr)
		}
		return nil
	}
	if err := readPostings(); err != nil {
		return nil, err
	}

	readReceipts := func() error {
		rows, err := port.executor.QueryContext(ctx,
			basReportingGuardedChildRowsSQL("receipt", basInvoiceReceiptOwnerFromSQL,
				basInvoiceReceiptOwnerScopeSQL, basInvoiceReceiptInvalidOwnerSQL,
				"id", "organisation_id", "invoice_id", "journal_id")+
				"SELECT receipt.id,receipt.organisation_id,receipt.invoice_id,receipt.journal_id FROM guarded AS receipt "+
				"ORDER BY receipt.journal_id,receipt.id LIMIT ?",
			organisationID, maxBASReportingReceipts+1)
		if err != nil {
			return errors.Join(ErrRepository, fmt.Errorf("preflight receipt journal identities: %w", err), ctx.Err())
		}
		var count int64
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err, rows.Close())
			}
			var receiptID, ownerID, invoiceID, journalID string
			if err := rows.Scan(&receiptID, &ownerID, &invoiceID, &journalID); err != nil {
				return errors.Join(ErrRepository, err, rows.Close())
			}
			count++
			if count > maxBASReportingReceipts {
				return errors.Join(&SourceBoundError{Resource: "receipt journals", Limit: maxBASReportingReceipts}, rows.Close())
			}
			if !ids.IsCanonicalV7(receiptID) || ownerID != organisationID ||
				!ids.IsCanonicalV7(invoiceID) || journalID == "" {
				return errors.Join(fmt.Errorf("%w: receipt journal identity has an invalid owner or ID", ErrRepository), rows.Close())
			}
			unique[journalID] = struct{}{}
		}
		rowsErr, closeErr := rows.Err(), rows.Close()
		if rowsErr != nil || closeErr != nil {
			return errors.Join(ErrRepository, rowsErr, closeErr)
		}
		return nil
	}
	if err := readReceipts(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}

	journalIDs := make([]string, 0, len(unique))
	for journalID := range unique {
		journalIDs = append(journalIDs, journalID)
	}
	sort.Strings(journalIDs)
	return journalIDs, nil
}

func invoiceReportingAccountingReadError(err error, maximumPayloadBytes int64) error {
	var bound *accounting.TaxFactReportingBoundError
	if errors.As(err, &bound) {
		limit := int64(bound.Limit)
		if bound.Resource == "capture bytes" {
			limit = maximumPayloadBytes
		}
		return errors.Join(&SourceBoundError{Resource: bound.Resource, Limit: limit}, err)
	}
	if errors.Is(err, accounting.ErrTaxFactReportingBound) {
		return errors.Join(ErrSourceBound, err)
	}
	return errors.Join(ErrRepository, err)
}

func cloneBASReportingPreflight(value BASReportingPreflight) BASReportingPreflight {
	value.JournalIDs = append([]string(nil), value.JournalIDs...)
	copied := make(map[string]int64, len(value.JournalPayloadBytes))
	for journalID, payloadBytes := range value.JournalPayloadBytes {
		copied[journalID] = payloadBytes
	}
	value.JournalPayloadBytes = copied
	return value
}

// reportingInvalidNumericStorageRowSQL emits a row-level 0/1 expression.
// All identifiers passed here are static repository-owned SQL columns.
func reportingInvalidNumericStorageRowSQL(columns ...string) string {
	predicate := ""
	for _, column := range columns {
		if predicate != "" {
			predicate += " OR "
		}
		predicate += "typeof(" + column + ") <> 'integer'"
	}
	if predicate == "" {
		return "0"
	}
	return "CASE WHEN (" + predicate + ") THEN 1 ELSE 0 END"
}

func reportingPayloadSize(columns ...string) string {
	parts := []string{"512"}
	for _, column := range columns {
		parts = append(parts, "coalesce(length(CAST("+column+" AS BLOB)), 0)")
	}
	return joinSQLParts(parts)
}

func joinSQLParts(parts []string) string {
	result := ""
	for index, part := range parts {
		if index > 0 {
			result += " + "
		}
		result += part
	}
	return result
}

func addBASReportingRowPayload(total *int64, fieldLengths ...int) bool {
	if total == nil || *total < 0 {
		return false
	}
	rowBytes := int64(512)
	for _, length := range fieldLengths {
		if length < 0 || int64(length) > math.MaxInt64-rowBytes {
			return false
		}
		rowBytes += int64(length)
	}
	sum, ok := checkedBASAdd(*total, rowBytes)
	if !ok {
		return false
	}
	*total = sum
	return true
}

func verifyBASReportingBaseReservation(
	ctx context.Context,
	reservation *basReportingReservation,
	candidates []BASInvoiceCandidate,
	postings map[string]*BASInvoicePosting,
	incomeLines map[string][]BASInvoiceIncomeLine,
	receipts map[string]*BASInvoiceReceipt,
	allocations map[string][]BASInvoicePaymentLine,
) error {
	if reservation == nil {
		return ErrRepository
	}
	var invoiceBytes, postingBytes, incomeLineBytes, receiptBytes, allocationBytes int64
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		if !addBASReportingRowPayload(&invoiceBytes,
			len(candidate.ID), len(candidate.OrganisationID), len(candidate.State), len(candidate.Number),
			len(candidate.InvoiceProto), len(candidate.CreatedAt), len(candidate.UpdatedAt)) {
			return fmt.Errorf("%w: invoice retained-byte charge overflow", ErrRepository)
		}
	}
	for _, posting := range postings {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		journalIDBytes := 0
		if posting.JournalID != nil {
			journalIDBytes = len(*posting.JournalID)
		}
		if !addBASReportingRowPayload(&postingBytes,
			len(posting.InvoiceID), len(posting.OrganisationID), len(posting.InvoiceDate),
			len(posting.RevenueAccountID), journalIDBytes, len(posting.SourceHash), len(posting.CreatedAt)) {
			return fmt.Errorf("%w: invoice posting retained-byte charge overflow", ErrRepository)
		}
	}
	var actualIncomeLineRows int64
	for _, lines := range incomeLines {
		for _, line := range lines {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err)
			}
			actualIncomeLineRows++
			if !addBASReportingRowPayload(&incomeLineBytes, len(line.InvoiceID)) {
				return fmt.Errorf("%w: invoice income-line retained-byte charge overflow", ErrRepository)
			}
		}
	}
	for _, receipt := range receipts {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		if receipt == nil || !addBASReportingRowPayload(&receiptBytes,
			len(receipt.ID), len(receipt.OrganisationID), len(receipt.InvoiceID), len(receipt.PaymentDate),
			len(receipt.BankAccountID), len(receipt.JournalID), len(receipt.Reference),
			len(receipt.PaymentProto), len(receipt.SourceHash), len(receipt.CreatedAt)) {
			return fmt.Errorf("%w: invoice receipt retained-byte charge overflow", ErrRepository)
		}
	}
	var actualAllocationRows int64
	for _, lines := range allocations {
		for _, allocation := range lines {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err)
			}
			actualAllocationRows++
			if !addBASReportingRowPayload(&allocationBytes, len(allocation.PaymentID)) {
				return fmt.Errorf("%w: invoice allocation retained-byte charge overflow", ErrRepository)
			}
		}
	}

	if int64(len(candidates)) != reservation.invoiceRows ||
		int64(len(postings)) != reservation.postingRows ||
		actualIncomeLineRows != reservation.incomeLineRows ||
		int64(len(receipts)) != reservation.receiptRows ||
		actualAllocationRows != reservation.allocationRows ||
		invoiceBytes != reservation.invoicePayloadBytes ||
		postingBytes != reservation.postingPayloadBytes ||
		incomeLineBytes != reservation.incomeLinePayloadBytes ||
		receiptBytes != reservation.receiptPayloadBytes ||
		allocationBytes != reservation.allocationPayloadBytes {
		return fmt.Errorf("%w: invoice reporting row counts or retained-byte charges changed after preflight", ErrRepository)
	}

	actualBaseBytes := int64(0)
	for _, charge := range []int64{invoiceBytes, postingBytes, incomeLineBytes, receiptBytes, allocationBytes} {
		var ok bool
		if actualBaseBytes, ok = checkedBASAdd(actualBaseBytes, charge); !ok {
			return fmt.Errorf("%w: loaded invoice reporting payload overflow", ErrRepository)
		}
	}
	if actualBaseBytes != reservation.BasePayloadBytes ||
		int64(len(candidates)) != int64(reservation.CandidateCount) ||
		int64(len(receipts)) != int64(reservation.ReceiptCount) {
		return fmt.Errorf("%w: loaded invoice reporting base does not match its reservation", ErrRepository)
	}

	actualJournalIDs := make(map[string]struct{})
	for _, posting := range postings {
		if posting.JournalID != nil {
			actualJournalIDs[*posting.JournalID] = struct{}{}
		}
	}
	for _, receipt := range receipts {
		actualJournalIDs[receipt.JournalID] = struct{}{}
	}
	actualSortedJournalIDs := make([]string, 0, len(actualJournalIDs))
	for journalID := range actualJournalIDs {
		actualSortedJournalIDs = append(actualSortedJournalIDs, journalID)
	}
	sort.Strings(actualSortedJournalIDs)
	if !equalBASReportingJournalIDs(actualSortedJournalIDs, reservation.JournalIDs) ||
		len(reservation.JournalPayloadBytes) != reservation.JournalCount {
		return fmt.Errorf("%w: loaded invoice journal identities changed after preflight", ErrRepository)
	}
	return nil
}

func verifyBASReportingParentCharges(
	reservation *basReportingReservation,
	loadedJournalPayloadBytes map[string]int64,
) error {
	if reservation == nil || len(loadedJournalPayloadBytes) != reservation.JournalCount ||
		len(reservation.JournalPayloadBytes) != reservation.JournalCount {
		return fmt.Errorf("%w: loaded invoice journal count changed after preflight", ErrRepository)
	}
	for _, journalID := range reservation.JournalIDs {
		expected, ok := reservation.JournalPayloadBytes[journalID]
		actual, loaded := loadedJournalPayloadBytes[journalID]
		if !ok || !loaded || expected <= 0 || actual != expected {
			return fmt.Errorf("%w: loaded journal %s retained-byte charge changed after preflight", ErrRepository, journalID)
		}
	}
	return nil
}

func equalBASReportingJournalIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (port *ReportingPort) loadBASReportingInvoices(
	ctx context.Context,
	organisationID string,
) ([]BASInvoiceCandidate, error) {
	// A same-byte BLOB owner introduced after preflight must remain visible,
	// but none of its cells may reach the driver as an accepted invoice row.
	rows, err := port.executor.QueryContext(ctx, `WITH requested AS (SELECT ? AS organisation_id),
	 guarded AS (
	  SELECT CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.id END AS id,
	   CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.organisation_id END AS organisation_id,
	   CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.version END AS version,
	   CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.state END AS state,
	   CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.number END AS number,
	   CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.invoice_proto END AS invoice_proto,
	   CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.created_at END AS created_at,
	   CASE WHEN typeof(invoice.organisation_id)='text' THEN invoice.updated_at END AS updated_at
	  FROM invoices_v1 AS invoice CROSS JOIN requested AS target
	  WHERE (`+basInvoiceOwnerScopeSQL+`)
	  ORDER BY invoice.created_at,invoice.id LIMIT ?
	 )
	 SELECT id,organisation_id,version,state,number,invoice_proto,created_at,updated_at
	 FROM guarded ORDER BY created_at,id`, organisationID, maxBASReportingInvoices+1)
	if err != nil {
		return nil, errors.Join(ErrRepository, fmt.Errorf("query invoice reporting sources: %w", err), ctx.Err())
	}
	candidates := make([]BASInvoiceCandidate, 0, 64)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		candidate, err := scanBASReportingInvoice(rows, organisationID)
		if err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		candidates = append(candidates, candidate)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	if len(candidates) > maxBASReportingInvoices {
		return nil, &SourceBoundError{Resource: "invoices", Limit: maxBASReportingInvoices}
	}
	return candidates, nil
}

func scanBASReportingInvoice(scanner interface{ Scan(...any) error }, organisationID string) (BASInvoiceCandidate, error) {
	var candidate BASInvoiceCandidate
	var version int64
	var invoiceProto []byte
	if err := scanner.Scan(&candidate.ID, &candidate.OrganisationID, &version, &candidate.State,
		&candidate.Number, &invoiceProto, &candidate.CreatedAt, &candidate.UpdatedAt); err != nil {
		return BASInvoiceCandidate{}, err
	}
	if version < 1 || candidate.ID == "" || !ids.IsCanonicalV7(candidate.ID) ||
		candidate.OrganisationID != organisationID || !ids.IsCanonicalV7(candidate.OrganisationID) ||
		(len(invoiceProto) == 0 || len(invoiceProto) > 131072) {
		return BASInvoiceCandidate{}, errors.New("invoice row has invalid identity, version, owner, or payload")
	}
	invoice := &tammyv1.Invoice{}
	if err := proto.Unmarshal(invoiceProto, invoice); err != nil || !validInvoice(invoice) ||
		invoice.Id != candidate.ID || invoice.OrganisationId != candidate.OrganisationID ||
		invoice.Version != uint64(version) || invoice.Draft.Number != candidate.Number ||
		invoice.State.String() != "INVOICE_STATE_"+candidate.State ||
		invoice.CreatedAt.AsTime().UTC().Format(time.RFC3339Nano) != candidate.CreatedAt ||
		invoice.UpdatedAt.AsTime().UTC().Format(time.RFC3339Nano) != candidate.UpdatedAt {
		return BASInvoiceCandidate{}, errors.New("invoice protobuf disagrees with retained invoice row")
	}
	if candidate.State != "DRAFT" && candidate.State != "ISSUED" {
		return BASInvoiceCandidate{}, errors.New("invoice row has unsupported state")
	}
	hash := sha256.Sum256(invoiceProto)
	candidate.Version = uint64(version)
	candidate.Invoice = invoice
	candidate.InvoiceProto = append([]byte(nil), invoiceProto...)
	candidate.InvoiceHash = append([]byte(nil), hash[:]...)
	return candidate, nil
}

func (port *ReportingPort) loadBASReportingPostings(
	ctx context.Context,
	organisationID string,
	candidates map[string]*BASInvoiceCandidate,
) (map[string]*BASInvoicePosting, error) {
	guardedRows := basReportingGuardedChildRowsSQL("posting", basInvoicePostingOwnerFromSQL,
		basInvoicePostingOwnerScopeSQL, basInvoicePostingInvalidOwnerSQL,
		"invoice_id", "organisation_id", "invoice_version", "invoice_date", "gst_basis", "revenue_account_id", "journal_id",
		"invoice_net_minor", "invoice_gst_minor", "invoice_total_minor", "bas_sales_gross_minor", "profile_version",
		"basis_audit_sequence", "financial_revision", "source_hash", "created_at")
	rows, err := port.executor.QueryContext(ctx, guardedRows+`SELECT posting.invoice_id,posting.organisation_id,
		posting.invoice_version,posting.invoice_date,posting.gst_basis,posting.revenue_account_id,posting.journal_id,
		posting.invoice_net_minor,posting.invoice_gst_minor,posting.invoice_total_minor,posting.bas_sales_gross_minor,
		posting.profile_version,posting.basis_audit_sequence,posting.financial_revision,posting.source_hash,posting.created_at
		FROM guarded AS posting ORDER BY posting.invoice_id`, organisationID)
	if err != nil {
		return nil, errors.Join(ErrRepository, fmt.Errorf("query invoice posting reporting sources: %w", err), ctx.Err())
	}
	postings := make(map[string]*BASInvoicePosting)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		posting, err := scanBASReportingPosting(rows)
		if err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		candidate := candidates[posting.InvoiceID]
		if candidate == nil || candidate.OrganisationID != posting.OrganisationID {
			return nil, errors.Join(ErrRepository, errors.New("invoice posting owner does not match its invoice"), rows.Close())
		}
		if _, duplicate := postings[posting.InvoiceID]; duplicate {
			return nil, errors.Join(ErrRepository, errors.New("duplicate invoice posting"), rows.Close())
		}
		postings[posting.InvoiceID] = &posting
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	return postings, nil
}

func scanBASReportingPosting(scanner interface{ Scan(...any) error }) (BASInvoicePosting, error) {
	var posting BASInvoicePosting
	var version, basis, profileVersion, basisSequence, financialRevision int64
	var journalID sql.NullString
	if err := scanner.Scan(&posting.InvoiceID, &posting.OrganisationID, &version, &posting.InvoiceDate,
		&basis, &posting.RevenueAccountID, &journalID, &posting.InvoiceNetMinor, &posting.InvoiceGSTMinor,
		&posting.InvoiceTotalMinor, &posting.BASSalesGrossMinor, &profileVersion, &basisSequence,
		&financialRevision, &posting.SourceHash, &posting.CreatedAt); err != nil {
		return BASInvoicePosting{}, err
	}
	if version < 1 || basis < 0 || basis > int64(tammyv1.GstBasis_GST_BASIS_NON_CASH) ||
		posting.RevenueAccountID == "" || profileVersion < 1 || basisSequence < 0 || financialRevision < 1 ||
		len(posting.SourceHash) != sha256.Size || posting.InvoiceNetMinor < 0 || posting.InvoiceGSTMinor < 0 ||
		posting.InvoiceTotalMinor < 0 || posting.BASSalesGrossMinor < 0 ||
		posting.InvoiceNetMinor > math.MaxInt64-posting.InvoiceGSTMinor ||
		posting.InvoiceNetMinor+posting.InvoiceGSTMinor != posting.InvoiceTotalMinor ||
		!validReportingTimestamp(posting.CreatedAt) {
		return BASInvoicePosting{}, errors.New("invoice posting row has invalid retained values")
	}
	if journalID.Valid {
		if journalID.String == "" {
			return BASInvoicePosting{}, errors.New("invoice posting has an empty present journal ID")
		}
		posting.JournalID = stringAddress(journalID.String)
	}
	posting.InvoiceVersion = uint64(version)
	posting.GSTBasis = tammyv1.GstBasis(basis)
	posting.ProfileVersion = uint64(profileVersion)
	posting.BasisAuditSequence = uint64(basisSequence)
	posting.FinancialRevision = uint64(financialRevision)
	posting.SourceHash = append([]byte(nil), posting.SourceHash...)
	return posting, nil
}

func (port *ReportingPort) loadBASReportingIncomeLines(
	ctx context.Context,
	organisationID string,
	postings map[string]*BASInvoicePosting,
) (map[string][]BASInvoiceIncomeLine, error) {
	guardedRows := basReportingGuardedChildRowsSQL("income_line", basInvoiceIncomeLineFromSQL,
		basInvoiceIncomeLineScopeSQL, basInvoiceIncomeLineInvalidFullSQL,
		"invoice_id", "sequence", "gross_minor", "net_minor", "gst_minor", "treatment")
	rows, err := port.executor.QueryContext(ctx, guardedRows+`SELECT income_line.invoice_id,income_line.sequence,
		income_line.gross_minor,income_line.net_minor,income_line.gst_minor,income_line.treatment
		FROM guarded AS income_line ORDER BY income_line.invoice_id,income_line.sequence`, organisationID)
	if err != nil {
		return nil, errors.Join(ErrRepository, fmt.Errorf("query invoice income-line reporting sources: %w", err), ctx.Err())
	}
	lines := make(map[string][]BASInvoiceIncomeLine)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		var line BASInvoiceIncomeLine
		var sequence, treatment int64
		if err := rows.Scan(&line.InvoiceID, &sequence, &line.GrossMinor, &line.NetMinor, &line.GSTMinor, &treatment); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		if postings[line.InvoiceID] == nil || sequence < 1 || line.GrossMinor < 0 || line.NetMinor < 0 ||
			line.GSTMinor < 0 || line.NetMinor > math.MaxInt64-line.GSTMinor ||
			line.GrossMinor != line.NetMinor+line.GSTMinor || treatment < 1 || treatment > 4 {
			return nil, errors.Join(ErrRepository, errors.New("invoice income line has invalid retained values"), rows.Close())
		}
		line.Sequence = uint64(sequence)
		line.Treatment = tammyv1.TaxTreatment(treatment)
		current := lines[line.InvoiceID]
		if sequence != int64(len(current)+1) {
			return nil, errors.Join(ErrRepository, errors.New("invoice income lines are not a contiguous ordered sequence"), rows.Close())
		}
		lines[line.InvoiceID] = append(current, line)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	return lines, nil
}

func (port *ReportingPort) loadBASReportingReceipts(
	ctx context.Context,
	organisationID string,
	candidates map[string]*BASInvoiceCandidate,
) (map[string][]BASInvoiceReceipt, map[string]*BASInvoiceReceipt, error) {
	guardedRows := basReportingGuardedChildRowsSQL("receipt", basInvoiceReceiptOwnerFromSQL,
		basInvoiceReceiptOwnerScopeSQL, basInvoiceReceiptInvalidOwnerSQL,
		"id", "organisation_id", "invoice_id", "payment_date", "amount_minor", "allocated_gst_minor", "bas_sales_gross_minor",
		"bank_account_id", "journal_id", "reference", "payment_proto", "source_hash", "created_at")
	rows, err := port.executor.QueryContext(ctx, guardedRows+`SELECT receipt.id,receipt.organisation_id,receipt.invoice_id,
		receipt.payment_date,receipt.amount_minor,receipt.allocated_gst_minor,receipt.bas_sales_gross_minor,
		receipt.bank_account_id,receipt.journal_id,receipt.reference,receipt.payment_proto,receipt.source_hash,receipt.created_at
		FROM guarded AS receipt
		ORDER BY receipt.invoice_id,receipt.payment_date,receipt.created_at,receipt.id`, organisationID)
	if err != nil {
		return nil, nil, errors.Join(ErrRepository, fmt.Errorf("query invoice receipt reporting sources: %w", err), ctx.Err())
	}
	byInvoice := make(map[string][]BASInvoiceReceipt)
	byID := make(map[string]*BASInvoiceReceipt)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, nil, errors.Join(ErrRepository, err, rows.Close())
		}
		receipt, err := scanBASReportingReceipt(rows)
		if err != nil {
			return nil, nil, errors.Join(ErrRepository, err, rows.Close())
		}
		candidate := candidates[receipt.InvoiceID]
		if candidate == nil || candidate.OrganisationID != receipt.OrganisationID {
			return nil, nil, errors.Join(ErrRepository, errors.New("receipt owner does not match its invoice"), rows.Close())
		}
		if _, duplicate := byID[receipt.ID]; duplicate {
			return nil, nil, errors.Join(ErrRepository, errors.New("duplicate invoice receipt ID"), rows.Close())
		}
		copy := receipt
		byID[receipt.ID] = &copy
		byInvoice[receipt.InvoiceID] = append(byInvoice[receipt.InvoiceID], receipt)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	if len(byID) > maxBASReportingReceipts {
		return nil, nil, &SourceBoundError{Resource: "receipts", Limit: maxBASReportingReceipts}
	}
	return byInvoice, byID, nil
}

func scanBASReportingReceipt(scanner interface{ Scan(...any) error }) (BASInvoiceReceipt, error) {
	var receipt BASInvoiceReceipt
	var paymentProto []byte
	if err := scanner.Scan(&receipt.ID, &receipt.OrganisationID, &receipt.InvoiceID, &receipt.PaymentDate,
		&receipt.AmountMinor, &receipt.AllocatedGSTMinor, &receipt.BASSalesGrossMinor, &receipt.BankAccountID,
		&receipt.JournalID, &receipt.Reference, &paymentProto, &receipt.SourceHash, &receipt.CreatedAt); err != nil {
		return BASInvoiceReceipt{}, err
	}
	if receipt.ID == "" || !ids.IsCanonicalV7(receipt.ID) || !ids.IsCanonicalV7(receipt.OrganisationID) ||
		!ids.IsCanonicalV7(receipt.InvoiceID) || !ids.IsCanonicalV7(receipt.BankAccountID) || receipt.JournalID == "" ||
		receipt.PaymentDate == "" || !date(receipt.PaymentDate) || receipt.AmountMinor <= 0 ||
		receipt.AllocatedGSTMinor < 0 || receipt.AllocatedGSTMinor > receipt.AmountMinor ||
		receipt.BASSalesGrossMinor < 0 || receipt.BASSalesGrossMinor > receipt.AmountMinor ||
		len(paymentProto) == 0 || len(paymentProto) > 4096 || len(receipt.SourceHash) != sha256.Size ||
		!validReportingTimestamp(receipt.CreatedAt) {
		return BASInvoiceReceipt{}, errors.New("invoice receipt row has invalid retained values")
	}
	payment := &tammyv1.InvoicePayment{}
	if err := proto.Unmarshal(paymentProto, payment); err != nil || payment.Id != receipt.ID ||
		payment.OrganisationId != receipt.OrganisationID || payment.InvoiceId != receipt.InvoiceID ||
		payment.Amount == nil || payment.Amount.CurrencyCode != "AUD" || payment.Amount.MinorUnits != receipt.AmountMinor ||
		payment.AllocatedGst == nil || payment.AllocatedGst.CurrencyCode != "AUD" ||
		payment.AllocatedGst.MinorUnits != receipt.AllocatedGSTMinor || payment.BasSalesGross == nil ||
		payment.BasSalesGross.CurrencyCode != "AUD" || payment.BasSalesGross.MinorUnits != receipt.BASSalesGrossMinor ||
		payment.BankAccountId != receipt.BankAccountID || payment.JournalId != receipt.JournalID ||
		payment.Reference != receipt.Reference || payment.CreatedAt == nil || payment.CreatedAt.CheckValid() != nil {
		return BASInvoiceReceipt{}, errors.New("invoice payment protobuf disagrees with its retained row")
	}
	paymentDate, ok := reportingCivilDate(payment.PaymentDate)
	if !ok || paymentDate != receipt.PaymentDate {
		return BASInvoiceReceipt{}, errors.New("invoice payment date disagrees with its retained row")
	}
	hash := sha256.Sum256(paymentProto)
	if !bytesEqualReporting(hash[:], receipt.SourceHash) {
		return BASInvoiceReceipt{}, errors.New("invoice payment source hash does not match its exact payload")
	}
	receipt.Payment = payment
	receipt.PaymentProto = append([]byte(nil), paymentProto...)
	receipt.PaymentHash = append([]byte(nil), hash[:]...)
	receipt.SourceHash = append([]byte(nil), receipt.SourceHash...)
	return receipt, nil
}

func (port *ReportingPort) loadBASReportingAllocations(
	ctx context.Context,
	organisationID string,
	receipts map[string]*BASInvoiceReceipt,
) (map[string][]BASInvoicePaymentLine, error) {
	guardedRows := basReportingGuardedChildRowsSQL("allocation", basInvoiceAllocationFromSQL,
		basInvoiceAllocationScopeSQL, basInvoiceAllocationInvalidFullSQL,
		"payment_id", "sequence", "gross_minor", "gst_minor")
	rows, err := port.executor.QueryContext(ctx, guardedRows+`SELECT allocation.payment_id,allocation.sequence,
		allocation.gross_minor,allocation.gst_minor FROM guarded AS allocation
		ORDER BY allocation.payment_id,allocation.sequence`, organisationID)
	if err != nil {
		return nil, errors.Join(ErrRepository, fmt.Errorf("query invoice receipt allocation sources: %w", err), ctx.Err())
	}
	allocations := make(map[string][]BASInvoicePaymentLine)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		var allocation BASInvoicePaymentLine
		var sequence int64
		if err := rows.Scan(&allocation.PaymentID, &sequence, &allocation.GrossMinor, &allocation.GSTMinor); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		if receipts[allocation.PaymentID] == nil || sequence < 1 || allocation.GrossMinor < 0 ||
			allocation.GSTMinor < 0 || allocation.GSTMinor > allocation.GrossMinor {
			return nil, errors.Join(ErrRepository, errors.New("invoice payment allocation has invalid retained values"), rows.Close())
		}
		allocation.Sequence = uint64(sequence)
		current := allocations[allocation.PaymentID]
		if sequence != int64(len(current)+1) {
			return nil, errors.Join(ErrRepository, errors.New("invoice payment allocations are not a contiguous ordered sequence"), rows.Close())
		}
		allocations[allocation.PaymentID] = append(current, allocation)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	return allocations, nil
}

func validateBASInvoicePosting(candidate BASInvoiceCandidate, posting *BASInvoicePosting) error {
	if posting == nil || candidate.Invoice == nil || posting.InvoiceID != candidate.ID ||
		posting.OrganisationID != candidate.OrganisationID || posting.InvoiceVersion != candidate.Version ||
		posting.InvoiceDate != candidate.Invoice.Draft.IssueDate || posting.RevenueAccountID == "" ||
		posting.InvoiceNetMinor != candidate.Invoice.SubtotalMinor || posting.InvoiceGSTMinor != candidate.Invoice.GstMinor ||
		posting.InvoiceTotalMinor != candidate.Invoice.TotalMinor ||
		!bytesEqualReporting(posting.SourceHash, candidate.InvoiceHash) {
		return fmt.Errorf("%w: invoice posting does not match retained invoice %s", ErrRepository, candidate.ID)
	}
	if candidate.Invoice.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED {
		return fmt.Errorf("%w: draft invoice %s has an income posting", ErrRepository, candidate.ID)
	}
	if (posting.InvoiceTotalMinor == 0) != (posting.JournalID == nil) {
		return fmt.Errorf("%w: invoice %s journal presence disagrees with its total", ErrRepository, candidate.ID)
	}
	if len(posting.IncomeLines) != len(candidate.Invoice.Draft.Lines) ||
		len(posting.IncomeLines) != len(candidate.Invoice.LineTotals) {
		return fmt.Errorf("%w: invoice %s posting income-line count does not match its source", ErrRepository, candidate.ID)
	}
	var basGross int64
	for index, line := range posting.IncomeLines {
		wantTotals := candidate.Invoice.LineTotals[index]
		wantTreatment := candidate.Invoice.Draft.Lines[index].TaxTreatment
		if line.InvoiceID != candidate.ID || line.Sequence != uint64(index+1) ||
			line.GrossMinor != wantTotals.TotalMinor || line.NetMinor != wantTotals.SubtotalMinor ||
			line.GSTMinor != wantTotals.GstMinor || line.Treatment != wantTreatment {
			return fmt.Errorf("%w: invoice %s income line %d disagrees with retained document", ErrRepository, candidate.ID, index+1)
		}
		if line.Treatment != tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			var ok bool
			if basGross, ok = checkedBASAdd(basGross, line.GrossMinor); !ok {
				return fmt.Errorf("%w: invoice %s BAS gross overflow", ErrRepository, candidate.ID)
			}
		}
	}
	if basGross != posting.BASSalesGrossMinor {
		return fmt.Errorf("%w: invoice %s BAS gross does not match its retained line treatments", ErrRepository, candidate.ID)
	}
	return nil
}

func validateBASInvoiceReceipt(
	candidate BASInvoiceCandidate,
	posting BASInvoicePosting,
	receipt *BASInvoiceReceipt,
) error {
	if receipt == nil || candidate.Invoice == nil || receipt.OrganisationID != candidate.OrganisationID ||
		receipt.InvoiceID != candidate.ID || receipt.AmountMinor <= 0 || receipt.Payment == nil ||
		posting.InvoiceTotalMinor <= 0 || len(receipt.Allocations) != len(posting.IncomeLines) {
		return fmt.Errorf("%w: receipt does not match a posted invoice source %s", ErrRepository, candidate.ID)
	}
	if candidate.Invoice.Draft.IssueDate == "" || receipt.PaymentDate < candidate.Invoice.Draft.IssueDate {
		return fmt.Errorf("%w: receipt %s predates its issued invoice", ErrRepository, receipt.ID)
	}
	var gross, gst, basGross int64
	for index, allocation := range receipt.Allocations {
		if allocation.PaymentID != receipt.ID || allocation.Sequence != uint64(index+1) ||
			allocation.GrossMinor < 0 || allocation.GSTMinor < 0 || allocation.GSTMinor > allocation.GrossMinor {
			return fmt.Errorf("%w: receipt %s has invalid ordered allocations", ErrRepository, receipt.ID)
		}
		var ok bool
		if gross, ok = checkedBASAdd(gross, allocation.GrossMinor); !ok {
			return fmt.Errorf("%w: receipt %s allocation gross overflow", ErrRepository, receipt.ID)
		}
		if gst, ok = checkedBASAdd(gst, allocation.GSTMinor); !ok {
			return fmt.Errorf("%w: receipt %s allocation GST overflow", ErrRepository, receipt.ID)
		}
		if posting.IncomeLines[index].Treatment != tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			if basGross, ok = checkedBASAdd(basGross, allocation.GrossMinor); !ok {
				return fmt.Errorf("%w: receipt %s BAS gross overflow", ErrRepository, receipt.ID)
			}
		}
	}
	if gross != receipt.AmountMinor || gst != receipt.AllocatedGSTMinor || basGross != receipt.BASSalesGrossMinor {
		return fmt.Errorf("%w: receipt %s scalar totals disagree with its complete allocations", ErrRepository, receipt.ID)
	}
	return nil
}

func (port *ReportingPort) reportingJournal(
	ctx context.Context,
	organisationID string,
	journalID string,
	cache map[string]*accounting.JournalReportingRecord,
	reservedJournalPayloadBytes map[string]int64,
	loadedJournalPayloadBytes map[string]int64,
	maximumPayloadBytes int64,
) (*accounting.JournalReportingRecord, error) {
	reservedPayloadBytes, reserved := reservedJournalPayloadBytes[journalID]
	if !reserved || reservedPayloadBytes <= 0 {
		return nil, fmt.Errorf("%w: journal %s was not reserved before hydration", ErrRepository, journalID)
	}
	if proof := cache[journalID]; proof != nil {
		if loadedJournalPayloadBytes[journalID] != reservedPayloadBytes {
			return nil, fmt.Errorf("%w: cached journal %s differs from its reservation", ErrRepository, journalID)
		}
		return proof, nil
	}
	proof, err := port.journals.GetForReportingWithPayloadBudget(
		ctx, organisationID, journalID, reservedPayloadBytes,
	)
	if err != nil {
		return nil, invoiceReportingAccountingReadError(err, maximumPayloadBytes)
	}
	if proof == nil || proof.Journal == nil || len(proof.ContentHash) != sha256.Size || proof.PayloadBytes <= 0 {
		return nil, fmt.Errorf("%w: accounting journal %s has incomplete reporting proof", ErrRepository, journalID)
	}
	if proof.PayloadBytes != reservedPayloadBytes {
		return nil, fmt.Errorf("%w: accounting journal %s retained-byte charge changed after preflight", ErrRepository, journalID)
	}
	loadedJournalPayloadBytes[journalID] = proof.PayloadBytes
	cache[journalID] = proof
	return proof, nil
}
func validateBASInvoiceJournal(candidate BASInvoiceCandidate, posting *BASInvoicePosting) error {
	if posting == nil || posting.JournalID == nil || posting.Journal == nil {
		return fmt.Errorf("%w: invoice %s posting has no full journal proof", ErrRepository, candidate.ID)
	}
	proof := posting.Journal
	if err := validateBASJournalProof(proof, *posting.JournalID, candidate.OrganisationID, "RECEIVABLE", 0,
		candidate.ID, candidate.Version, posting.InvoiceDate, tammyv1.JournalSource_JOURNAL_SOURCE_RECEIVABLE); err != nil {
		return err
	}
	journal := proof.Journal
	if journal.FinancialRevision != posting.FinancialRevision {
		return fmt.Errorf("%w: invoice %s journal financial revision does not match its posting", ErrRepository, candidate.ID)
	}
	debits, credits, err := accounting.CheckedJournalTotals(journal.Lines)
	if err != nil || journal.TotalDebits == nil || journal.TotalCredits == nil ||
		journal.TotalDebits.CurrencyCode != "AUD" || journal.TotalCredits.CurrencyCode != "AUD" ||
		debits != posting.InvoiceTotalMinor || credits != posting.InvoiceTotalMinor ||
		journal.TotalDebits.MinorUnits != debits || journal.TotalCredits.MinorUnits != credits {
		return fmt.Errorf("%w: invoice %s journal totals do not match the retained posting", ErrRepository, candidate.ID)
	}
	var revenueCredits, otherCredits int64
	for _, line := range journal.Lines {
		if line.Credit.CurrencyCode != "AUD" || line.Debit.CurrencyCode != "AUD" {
			return fmt.Errorf("%w: invoice %s journal has non-AUD lines", ErrRepository, candidate.ID)
		}
		if line.AccountId == posting.RevenueAccountID {
			revenueCredits, err = addReportingAmount(revenueCredits, line.Credit.MinorUnits)
		} else {
			otherCredits, err = addReportingAmount(otherCredits, line.Credit.MinorUnits)
		}
		if err != nil {
			return fmt.Errorf("%w: invoice %s journal credit overflow", ErrRepository, candidate.ID)
		}
	}
	if revenueCredits != posting.InvoiceNetMinor || otherCredits != posting.InvoiceGSTMinor {
		return fmt.Errorf("%w: invoice %s journal credits do not reconcile to retained net/GST", ErrRepository, candidate.ID)
	}
	return nil
}

func validateBASReceiptJournal(posting BASInvoicePosting, receipt *BASInvoiceReceipt) error {
	if receipt == nil || receipt.Journal == nil || receipt.JournalID == "" {
		return fmt.Errorf("%w: receipt has no full journal proof", ErrRepository)
	}
	proof := receipt.Journal
	if err := validateBASJournalProof(proof, receipt.JournalID, receipt.OrganisationID, "BANKING", 0,
		receipt.ID, 1, receipt.PaymentDate, tammyv1.JournalSource_JOURNAL_SOURCE_INVOICE_PAYMENT); err != nil {
		return err
	}
	journal := proof.Journal
	debits, credits, err := accounting.CheckedJournalTotals(journal.Lines)
	extraGST := int64(0)
	if posting.GSTBasis == tammyv1.GstBasis_GST_BASIS_CASH {
		extraGST = receipt.AllocatedGSTMinor
	}
	expectedTotal, ok := checkedBASAdd(receipt.AmountMinor, extraGST)
	if !ok || err != nil || journal.TotalDebits == nil || journal.TotalCredits == nil ||
		journal.TotalDebits.CurrencyCode != "AUD" || journal.TotalCredits.CurrencyCode != "AUD" ||
		debits != expectedTotal || credits != expectedTotal || journal.TotalDebits.MinorUnits != expectedTotal ||
		journal.TotalCredits.MinorUnits != expectedTotal {
		return fmt.Errorf("%w: receipt %s journal totals do not match its retained payment", ErrRepository, receipt.ID)
	}
	var bankDebits, otherDebits int64
	for _, line := range journal.Lines {
		if line.Debit.CurrencyCode != "AUD" || line.Credit.CurrencyCode != "AUD" {
			return fmt.Errorf("%w: receipt %s journal has non-AUD lines", ErrRepository, receipt.ID)
		}
		if line.AccountId == receipt.BankAccountID {
			bankDebits, err = addReportingAmount(bankDebits, line.Debit.MinorUnits)
		} else {
			otherDebits, err = addReportingAmount(otherDebits, line.Debit.MinorUnits)
		}
		if err != nil {
			return fmt.Errorf("%w: receipt %s journal debit overflow", ErrRepository, receipt.ID)
		}
	}
	if bankDebits != receipt.AmountMinor || otherDebits != extraGST {
		return fmt.Errorf("%w: receipt %s journal debits do not reconcile to bank/GST amounts", ErrRepository, receipt.ID)
	}
	return nil
}

func validateBASJournalProof(
	proof *accounting.JournalReportingRecord,
	journalID string,
	organisationID, sourceType string,
	sourceKind int64,
	sourceID string,
	sourceRevision uint64,
	postingDate string,
	source tammyv1.JournalSource,
) error {
	if proof == nil || proof.Journal == nil || proof.Journal.Id != journalID || proof.Journal.Id == "" ||
		proof.Journal.OrganisationId != organisationID || proof.SourceType != sourceType ||
		proof.SourceKind != sourceKind || proof.SourceID != sourceID || proof.SourceRevision != sourceRevision ||
		proof.Journal.FinancialRevision == 0 || proof.Journal.PostingDate == nil ||
		civilDateString(proof.Journal.PostingDate) != postingDate ||
		proof.Journal.Source != source {
		return fmt.Errorf("%w: accounting journal source tuple or context does not match its invoice owner", ErrRepository)
	}
	stateOK := (proof.State == "POSTED" && proof.Journal.State == tammyv1.JournalState_JOURNAL_STATE_POSTED &&
		proof.Journal.ReversalOfJournalId == nil && proof.Journal.ReversedByJournalId == nil) ||
		(proof.State == "REVERSED" && proof.Journal.State == tammyv1.JournalState_JOURNAL_STATE_REVERSED &&
			proof.Journal.ReversalOfJournalId == nil && proof.Journal.ReversedByJournalId != nil &&
			*proof.Journal.ReversedByJournalId != "")
	if !stateOK || len(proof.Journal.Lines) < 2 || len(proof.Journal.Lines) > 1000 {
		return fmt.Errorf("%w: accounting journal is not a supported posted or reversed parent", ErrRepository)
	}
	for index, line := range proof.Journal.Lines {
		if line == nil || line.Id == "" || line.JournalId != proof.Journal.Id ||
			!ids.IsCanonicalV7(line.AccountId) || line.Sequence != uint32(index+1) ||
			line.Debit == nil || line.Credit == nil || line.Debit.CurrencyCode != "AUD" ||
			line.Credit.CurrencyCode != "AUD" || line.Debit.MinorUnits < 0 || line.Credit.MinorUnits < 0 ||
			(line.Debit.MinorUnits > 0) == (line.Credit.MinorUnits > 0) {
			return fmt.Errorf("%w: accounting journal contains an invalid ordered line", ErrRepository)
		}
	}
	debits, credits, err := accounting.CheckedJournalTotals(proof.Journal.Lines)
	if err != nil || debits <= 0 || debits != credits || proof.Journal.TotalDebits == nil || proof.Journal.TotalCredits == nil ||
		proof.Journal.TotalDebits.CurrencyCode != "AUD" || proof.Journal.TotalCredits.CurrencyCode != "AUD" ||
		proof.Journal.TotalDebits.MinorUnits != debits || proof.Journal.TotalCredits.MinorUnits != credits {
		return fmt.Errorf("%w: accounting journal parent is not fully balanced", ErrRepository)
	}
	return nil
}

func validReportingTimestamp(value string) bool {
	instant, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && instant.UTC().Format(time.RFC3339Nano) == value
}

func civilDateString(value *tammyv1.CivilDate) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
}

func checkedBASAdd(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

func addReportingAmount(left, right int64) (int64, error) {
	result, ok := checkedBASAdd(left, right)
	if !ok {
		return 0, ErrRepository
	}
	return result, nil
}

func stringAddress(value string) *string { return &value }

func bytesEqualReporting(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func basReportingFingerprint(
	ctx context.Context,
	organisationID, startDate, endDate string,
	candidates []BASInvoiceCandidate,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	digest := sha256.New()
	hashBASFrame(digest, []byte(basReportingHashTag))
	hashBASFrame(digest, []byte(organisationID))
	hashBASFrame(digest, []byte(startDate))
	hashBASFrame(digest, []byte(endDate))
	hashBASInt64(digest, int64(len(candidates)))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hashBASFrame(digest, []byte(candidate.ID))
		hashBASFrame(digest, []byte(candidate.OrganisationID))
		hashBASUint64(digest, candidate.Version)
		hashBASFrame(digest, []byte(candidate.State))
		hashBASFrame(digest, []byte(candidate.Number))
		hashBASFrame(digest, []byte(candidate.CreatedAt))
		hashBASFrame(digest, []byte(candidate.UpdatedAt))
		hashBASFrame(digest, candidate.InvoiceProto)
		hashBASFrame(digest, candidate.InvoiceHash)
		hashBASFrame(digest, []byte(candidate.PeriodDisposition))
		hashBASBool(digest, candidate.Posting != nil)
		if candidate.Posting != nil {
			hashBASPosting(digest, *candidate.Posting)
		}
		hashBASInt64(digest, int64(len(candidate.Receipts)))
		for _, receipt := range candidate.Receipts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			hashBASReceipt(digest, receipt)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
}

func hashBASPosting(digest hash.Hash, posting BASInvoicePosting) {
	hashBASFrame(digest, []byte(posting.InvoiceID))
	hashBASFrame(digest, []byte(posting.OrganisationID))
	hashBASUint64(digest, posting.InvoiceVersion)
	hashBASFrame(digest, []byte(posting.InvoiceDate))
	hashBASInt64(digest, int64(posting.GSTBasis))
	hashBASFrame(digest, []byte(posting.RevenueAccountID))
	hashBASBool(digest, posting.JournalID != nil)
	if posting.JournalID != nil {
		hashBASFrame(digest, []byte(*posting.JournalID))
	}
	hashBASInt64(digest, posting.InvoiceNetMinor)
	hashBASInt64(digest, posting.InvoiceGSTMinor)
	hashBASInt64(digest, posting.InvoiceTotalMinor)
	hashBASInt64(digest, posting.BASSalesGrossMinor)
	hashBASUint64(digest, posting.ProfileVersion)
	hashBASUint64(digest, posting.BasisAuditSequence)
	hashBASUint64(digest, posting.FinancialRevision)
	hashBASFrame(digest, posting.SourceHash)
	hashBASFrame(digest, []byte(posting.CreatedAt))
	hashBASInt64(digest, int64(len(posting.IncomeLines)))
	for _, line := range posting.IncomeLines {
		hashBASFrame(digest, []byte(line.InvoiceID))
		hashBASUint64(digest, line.Sequence)
		hashBASInt64(digest, line.GrossMinor)
		hashBASInt64(digest, line.NetMinor)
		hashBASInt64(digest, line.GSTMinor)
		hashBASInt64(digest, int64(line.Treatment))
	}
	hashBASBool(digest, posting.Journal != nil)
	if posting.Journal != nil {
		hashBASFrame(digest, posting.Journal.ContentHash)
		hashBASInt64(digest, posting.Journal.PayloadBytes)
	}
}

func hashBASReceipt(digest hash.Hash, receipt BASInvoiceReceipt) {
	for _, value := range []string{receipt.ID, receipt.OrganisationID, receipt.InvoiceID, receipt.PaymentDate} {
		hashBASFrame(digest, []byte(value))
	}
	hashBASInt64(digest, receipt.AmountMinor)
	hashBASInt64(digest, receipt.AllocatedGSTMinor)
	hashBASInt64(digest, receipt.BASSalesGrossMinor)
	hashBASFrame(digest, []byte(receipt.BankAccountID))
	hashBASFrame(digest, []byte(receipt.JournalID))
	hashBASFrame(digest, []byte(receipt.Reference))
	hashBASFrame(digest, receipt.PaymentProto)
	hashBASFrame(digest, receipt.PaymentHash)
	hashBASFrame(digest, receipt.SourceHash)
	hashBASFrame(digest, []byte(receipt.CreatedAt))
	hashBASInt64(digest, int64(len(receipt.Allocations)))
	for _, allocation := range receipt.Allocations {
		hashBASFrame(digest, []byte(allocation.PaymentID))
		hashBASUint64(digest, allocation.Sequence)
		hashBASInt64(digest, allocation.GrossMinor)
		hashBASInt64(digest, allocation.GSTMinor)
	}
	hashBASBool(digest, receipt.Journal != nil)
	if receipt.Journal != nil {
		hashBASFrame(digest, receipt.Journal.ContentHash)
		hashBASInt64(digest, receipt.Journal.PayloadBytes)
	}
}

func hashBASFrame(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

func hashBASInt64(digest hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = digest.Write(encoded[:])
}

func hashBASUint64(digest hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = digest.Write(encoded[:])
}

func hashBASBool(digest hash.Hash, value bool) {
	if value {
		_, _ = digest.Write([]byte{1})
		return
	}
	_, _ = digest.Write([]byte{0})
}
