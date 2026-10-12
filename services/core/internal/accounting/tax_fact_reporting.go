package accounting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math"
	"sort"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxTaxFactReportingFacts        = 1000
	maxTaxFactReportingJournalLines = 1000
	maxTaxFactReportingCaptureBytes = 8 * 1024 * 1024
	taxFactReportingFingerprintTag  = "tammy/accounting/tax-fact-reporting-capture/v1"
)

var ErrTaxFactReportingBound = errors.New("accounting: tax fact reporting capture exceeds its bound")

// TaxFactReportingBoundError identifies which finite reporting projection
// bound was crossed. It also unwraps to ErrTaxFactReportingBound.
type TaxFactReportingBoundError struct {
	Resource string
	Limit    int
}

func (err *TaxFactReportingBoundError) Error() string {
	if err == nil {
		return ErrTaxFactReportingBound.Error()
	}
	return fmt.Sprintf("%s: %s exceeds limit %d", ErrTaxFactReportingBound, err.Resource, err.Limit)
}

func (err *TaxFactReportingBoundError) Unwrap() error { return ErrTaxFactReportingBound }

// TaxFactReportingPreflight is the bounded retained-payload reservation for
// one organisation's tax facts and each distinct complete parent journal.
// PayloadBytes counts raw TEXT/BLOB bytes plus the fixed per-row allowance; it
// is not protobuf size or a Go heap estimate.
type TaxFactReportingPreflight struct {
	BasePayloadBytes    int64
	PayloadBytes        int64
	FactCount           int
	JournalCount        int
	JournalIDs          []string
	JournalPayloadBytes map[string]int64
}

// TaxFactReportingCapture is one immutable-in-transaction view of the
// organisation's retained tax facts and their complete journal provenance.
type TaxFactReportingCapture struct {
	Facts               []TaxFactReportingRecord
	Fingerprint         []byte
	BasePayloadBytes    int64
	PayloadBytes        int64
	FactCount           int
	JournalCount        int
	JournalIDs          []string
	JournalPayloadBytes map[string]int64
}

// TaxFactReportingRecord preserves the SQL fact values and their source
// journal without applying tax-direction or source-kind interpretation.
type TaxFactReportingRecord struct {
	ID, OrganisationID, JournalLineID string
	TaxCode, Treatment                string

	OriginalGrossMinor, OriginalNetMinor, OriginalGSTMinor       int64
	AttributedGrossMinor, AttributedNetMinor, AttributedGSTMinor int64
	RemainingGrossMinor, RemainingNetMinor, RemainingGSTMinor    int64

	Rule *tammyv1.SourceRef

	SourceType, SourceID     string
	SourceRevision           uint64
	SourceContentHashPresent bool
	SourceContentHash        []byte
	CreatedAt                string

	Journal *tammyv1.Journal

	JournalSourceType     string
	JournalSourceKind     int64
	JournalSourceID       string
	JournalSourceRevision uint64
	JournalCreatedAt      string
	JournalState          string
}

type taxFactReportingCandidate struct {
	record                    TaxFactReportingRecord
	journalID                 string
	lineNumber                int64
	journalDate               string
	parentOrganisationID      string
	journalIDPresent          bool
	lineNumberPresent         bool
	journalDatePresent        bool
	parentOrganisationPresent bool
}

type taxFactReportingJournalSnapshot struct {
	journal      *tammyv1.Journal
	journalProto []byte
	raw          []byte
	payloadBytes int64
	lineCount    int
	organisation string
	sourceType   string
	sourceKind   int64
	sourceID     string
	sourceRev    uint64
	createdAt    string
	state        string
}

type taxFactReportingRawLine struct {
	line         *tammyv1.JournalLine
	id           string
	journalID    string
	lineNumber   int64
	accountID    string
	debit        int64
	credit       int64
	currency     string
	memo         sql.NullString
	taxCode      sql.NullString
	taxAmount    sql.NullInt64
	ruleType     sql.NullString
	ruleID       sql.NullString
	ruleRevision sql.NullInt64
	ruleHash     []byte
}

type taxFactReportingReservation struct {
	TaxFactReportingPreflight
	journalLineCounts map[string]int
}

// CaptureTaxFactsForReporting returns every retained tax fact for one
// organisation. It reads only through the caller-owned executor and never
// commits, rolls back, or filters by journal state, source, date, treatment,
// or amount sign.
func (repository *JournalRepository) CaptureTaxFactsForReporting(
	ctx context.Context,
	organisationID string,
) (*TaxFactReportingCapture, error) {
	return repository.CaptureTaxFactsForReportingWithPayloadBudget(
		ctx, organisationID, maxTaxFactReportingCaptureBytes)
}

// PreflightTaxFactsForReportingWithPayloadBudget reserves the retained bytes
// for every fact and each distinct complete parent before a caller hydrates
// any fact or journal body. Oversized caller limits are clamped to the normal
// eight MiB accounting ceiling.
func (repository *JournalRepository) PreflightTaxFactsForReportingWithPayloadBudget(
	ctx context.Context, organisationID string, maximumPayloadBytes int64,
) (*TaxFactReportingPreflight, error) {
	reservation, err := repository.preflightTaxFactReportingCapture(ctx, organisationID, maximumPayloadBytes)
	if err != nil {
		return nil, err
	}
	return &reservation.TaxFactReportingPreflight, nil
}

// CaptureTaxFactsForReportingWithPayloadBudget returns the same complete
// provenance as CaptureTaxFactsForReporting while applying a caller's shared
// retained-payload budget. The complete reservation is made before the first
// fact or journal body is loaded.
func (repository *JournalRepository) CaptureTaxFactsForReportingWithPayloadBudget(
	ctx context.Context, organisationID string, maximumPayloadBytes int64,
) (*TaxFactReportingCapture, error) {
	reservation, err := repository.preflightTaxFactReportingCapture(ctx, organisationID, maximumPayloadBytes)
	if err != nil {
		return nil, err
	}
	candidates, err := repository.loadTaxFactReportingCandidates(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if len(candidates) > maxTaxFactReportingFacts {
		return nil, &TaxFactReportingBoundError{Resource: "tax facts", Limit: maxTaxFactReportingFacts}
	}
	var loadedFactPayloadBytes int64
	for _, candidate := range candidates {
		loadedFactPayloadBytes += taxFactReportingCandidatePayloadBytes(candidate)
	}
	if len(candidates) != reservation.FactCount || loadedFactPayloadBytes != reservation.BasePayloadBytes {
		return nil, fmt.Errorf("%w: tax fact rows changed between payload preflight and capture", ErrJournalRepository)
	}

	journalCache := make(map[string]*taxFactReportingJournalSnapshot)
	records := make([]TaxFactReportingRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrJournalRepository, err)
		}
		if !candidate.journalIDPresent || !candidate.lineNumberPresent || !candidate.journalDatePresent ||
			!candidate.parentOrganisationPresent || candidate.journalID == "" || candidate.lineNumber < 1 ||
			candidate.lineNumber > math.MaxUint32 || candidate.journalDate == "" ||
			candidate.parentOrganisationID != organisationID {
			return nil, fmt.Errorf("%w: tax fact %s has a missing or mismatched journal parent", ErrJournalRepository, candidate.record.ID)
		}

		snapshot := journalCache[candidate.journalID]
		if snapshot == nil {
			snapshot, err = repository.loadTaxFactReportingJournal(ctx, candidate.journalID, organisationID)
			if err != nil {
				return nil, err
			}
			if expectedBytes, ok := reservation.JournalPayloadBytes[candidate.journalID]; !ok ||
				snapshot.payloadBytes != expectedBytes || snapshot.lineCount != reservation.journalLineCounts[candidate.journalID] {
				return nil, fmt.Errorf("%w: journal %s rows changed between payload preflight and capture", ErrJournalRepository, candidate.journalID)
			}
			journalCache[candidate.journalID] = snapshot
		}
		if snapshot.organisation != candidate.record.OrganisationID || snapshot.journal.PostingDate == nil ||
			candidate.journalDate != fmt.Sprintf("%04d-%02d-%02d", snapshot.journal.PostingDate.Year,
				snapshot.journal.PostingDate.Month, snapshot.journal.PostingDate.Day) {
			return nil, fmt.Errorf("%w: tax fact %s parent changed during capture", ErrJournalRepository, candidate.record.ID)
		}
		lineFound := false
		for _, line := range snapshot.journal.Lines {
			if line.Id == candidate.record.JournalLineID {
				lineFound = int64(line.Sequence) == candidate.lineNumber
				break
			}
		}
		if !lineFound {
			return nil, fmt.Errorf("%w: tax fact %s journal line is absent from its parent", ErrJournalRepository, candidate.record.ID)
		}

		candidate.record.Journal = snapshot.journal
		candidate.record.JournalSourceType = snapshot.sourceType
		candidate.record.JournalSourceKind = snapshot.sourceKind
		candidate.record.JournalSourceID = snapshot.sourceID
		candidate.record.JournalSourceRevision = snapshot.sourceRev
		candidate.record.JournalCreatedAt = snapshot.createdAt
		candidate.record.JournalState = snapshot.state
		records = append(records, candidate.record)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if len(journalCache) != reservation.JournalCount {
		return nil, fmt.Errorf("%w: journal parents changed between payload preflight and capture", ErrJournalRepository)
	}

	fingerprint, err := taxFactReportingFingerprint(ctx, organisationID, records, journalCache)
	if err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	return &TaxFactReportingCapture{
		Facts: records, Fingerprint: fingerprint,
		BasePayloadBytes: reservation.BasePayloadBytes, PayloadBytes: reservation.PayloadBytes,
		FactCount: reservation.FactCount, JournalCount: reservation.JournalCount,
		JournalIDs:          append([]string(nil), reservation.JournalIDs...),
		JournalPayloadBytes: cloneTaxFactReportingJournalPayloadBytes(reservation.JournalPayloadBytes),
	}, nil
}

func (repository *JournalRepository) preflightTaxFactReportingCapture(
	ctx context.Context, organisationID string, maximumPayloadBytes int64,
) (*taxFactReportingReservation, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || maximumPayloadBytes < 0 {
		return nil, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if maximumPayloadBytes > maxTaxFactReportingCaptureBytes {
		maximumPayloadBytes = maxTaxFactReportingCaptureBytes
	}
	remainingBytes := maximumPayloadBytes
	baseSize := taxFactReportingPayloadSizeSQL(
		"fact.id", "fact.organisation_id", "fact.journal_line_id", "fact.tax_code", "fact.treatment",
		"fact.tax_rule_type", "fact.tax_rule_id", "fact.tax_rule_content_hash",
		"fact.source_type", "fact.source_id", "fact.source_content_hash", "fact.created_at",
		"line.journal_id", "journal.organisation_id", "journal.journal_date",
	)
	baseNumericStorage := taxFactReportingInvalidNumericStorageSQL(
		[]string{
			"fact.original_gross_minor", "fact.original_net_minor", "fact.original_gst_minor",
			"fact.attributed_gross_minor", "fact.attributed_net_minor", "fact.attributed_gst_minor",
			"fact.remaining_gross_minor", "fact.remaining_net_minor", "fact.remaining_gst_minor",
			"fact.tax_rule_revision", "fact.source_revision",
		},
		[]string{"line.line_number"},
	)
	factCount, basePayloadBytes, err := repository.reserveTaxFactReportingPayloadDetails(ctx, &remainingBytes,
		"tax facts", maxTaxFactReportingFacts,
		`SELECT count(*), total(payload_bytes), coalesce(sum(invalid_numeric_storage), 0) FROM (
		 SELECT `+baseSize+` AS payload_bytes, `+baseNumericStorage+` AS invalid_numeric_storage
		 FROM tax_facts AS fact
		 LEFT JOIN journal_lines AS line ON line.id = fact.journal_line_id
		 LEFT JOIN journals AS journal ON journal.id = line.journal_id
		 WHERE fact.organisation_id = ?
		 ORDER BY journal.journal_date, journal.id, line.line_number, fact.id LIMIT ?
		)`, organisationID, maxTaxFactReportingFacts+1)
	if err != nil {
		return nil, withTaxFactReportingBudgetLimit(err, maximumPayloadBytes)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	invalidCount, err := repository.countInvalidTaxFactReportingParents(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if invalidCount != 0 {
		return nil, fmt.Errorf("%w: tax facts contain a missing or mismatched journal parent", ErrJournalRepository)
	}
	journalIDs, err := repository.taxFactReportingJournalIDs(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	journalPayloadBytes := make(map[string]int64, len(journalIDs))
	journalLineCounts := make(map[string]int, len(journalIDs))
	for _, journalID := range journalIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrJournalRepository, err)
		}
		payloadBytes, lineCount, err := repository.preflightTaxFactReportingJournal(
			ctx, journalID, organisationID, &remainingBytes)
		if err != nil {
			return nil, withTaxFactReportingBudgetLimit(err, maximumPayloadBytes)
		}
		journalPayloadBytes[journalID] = payloadBytes
		journalLineCounts[journalID] = lineCount
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	var totalPayloadBytes = basePayloadBytes
	for _, journalID := range journalIDs {
		totalPayloadBytes += journalPayloadBytes[journalID]
	}
	return &taxFactReportingReservation{
		TaxFactReportingPreflight: TaxFactReportingPreflight{
			BasePayloadBytes: basePayloadBytes, PayloadBytes: totalPayloadBytes,
			FactCount: factCount, JournalCount: len(journalIDs),
			JournalIDs: journalIDs, JournalPayloadBytes: journalPayloadBytes,
		},
		journalLineCounts: journalLineCounts,
	}, nil
}

func (repository *JournalRepository) countInvalidTaxFactReportingParents(
	ctx context.Context, organisationID string,
) (int, error) {
	count, err := repository.queryTaxFactReportingCount(ctx, `
		SELECT count(*)
		FROM tax_facts AS fact
		LEFT JOIN journal_lines AS line ON line.id = fact.journal_line_id
		LEFT JOIN journals AS journal ON journal.id = line.journal_id
		WHERE fact.organisation_id = ?
		  AND (line.id IS NULL OR journal.id IS NULL OR journal.organisation_id != fact.organisation_id
		       OR journal.id = '' OR line.journal_id = '' OR journal.journal_date IS NULL OR journal.journal_date = ''
		       OR line.line_number < 1 OR line.line_number > ?)`,
		organisationID, math.MaxUint32)
	if err != nil {
		return 0, taxFactReportingReadError("validate tax fact parents", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, errors.Join(ErrJournalRepository, err)
	}
	return count, nil
}

func (repository *JournalRepository) queryTaxFactReportingCount(
	ctx context.Context, query string, arguments ...any,
) (int, error) {
	rows, err := repository.executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return 0, err
	}
	if !rows.Next() {
		return 0, errors.Join(sql.ErrNoRows, rows.Err(), rows.Close())
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, errors.Join(err, rows.Close())
	}
	if rows.Next() {
		return 0, errors.Join(errors.New("count query returned multiple rows"), rows.Close())
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return 0, errors.Join(rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func (repository *JournalRepository) taxFactReportingJournalIDs(
	ctx context.Context, organisationID string,
) ([]string, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT DISTINCT journal.id
		FROM tax_facts AS fact
		JOIN journal_lines AS line ON line.id = fact.journal_line_id
		JOIN journals AS journal ON journal.id = line.journal_id
		WHERE fact.organisation_id = ? AND journal.organisation_id = fact.organisation_id
		ORDER BY journal.id LIMIT ?`, organisationID, maxTaxFactReportingFacts+1)
	if err != nil {
		return nil, taxFactReportingReadError("query distinct tax fact parents", err)
	}
	journalIDs := make([]string, 0)
	for rows.Next() {
		if len(journalIDs) == maxTaxFactReportingFacts {
			return nil, errors.Join(&TaxFactReportingBoundError{
				Resource: "journal parents", Limit: maxTaxFactReportingFacts,
			}, rows.Close())
		}
		var journalID string
		if err := rows.Scan(&journalID); err != nil {
			_ = rows.Close()
			return nil, taxFactReportingReadError("scan distinct tax fact parent", err)
		}
		if journalID == "" || len(journalIDs) > 0 && journalIDs[len(journalIDs)-1] >= journalID {
			_ = rows.Close()
			return nil, fmt.Errorf("%w: tax fact parent identities are not unique and sorted", ErrJournalRepository)
		}
		journalIDs = append(journalIDs, journalID)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrJournalRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	return journalIDs, nil
}

func withTaxFactReportingBudgetLimit(err error, limit int64) error {
	var bound *TaxFactReportingBoundError
	if errors.As(err, &bound) && bound.Resource == "capture bytes" {
		return &TaxFactReportingBoundError{Resource: bound.Resource, Limit: int(limit)}
	}
	return err
}

func cloneTaxFactReportingJournalPayloadBytes(values map[string]int64) map[string]int64 {
	cloned := make(map[string]int64, len(values))
	for journalID, payloadBytes := range values {
		cloned[journalID] = payloadBytes
	}
	return cloned
}

func (repository *JournalRepository) preflightTaxFactReportingJournal(
	ctx context.Context, journalID, organisationID string, remainingBytes *int64,
) (int64, int, error) {
	headerSize := taxFactReportingPayloadSizeSQL(
		"id", "organisation_id", "source_type", "source_id", "state", "journal_date", "description",
		"reversal_of_journal_id", "reversed_by_journal_id", "currency_code", "posted_at", "created_at",
	)
	headerNumericStorage := taxFactReportingInvalidNumericStorageSQL(
		[]string{"version", "source_kind", "source_revision", "total_debits_minor", "total_credits_minor", "financial_revision"},
		nil,
	)
	headerCount, headerBytes, err := repository.reserveTaxFactReportingPayloadDetails(ctx, remainingBytes, "journals", 1,
		`SELECT count(*), total(`+headerSize+`), coalesce(sum(`+headerNumericStorage+`), 0) FROM journals WHERE id = ? AND organisation_id = ?`, journalID, organisationID)
	if err != nil {
		return 0, 0, err
	}
	if headerCount != 1 {
		return 0, 0, fmt.Errorf("%w: reporting parent journal %s is missing or foreign", ErrJournalRepository, journalID)
	}
	lineSize := taxFactReportingPayloadSizeSQL(
		"id", "journal_id", "account_id", "currency_code", "memo", "tax_code_id",
		"tax_rule_type", "tax_rule_id", "tax_rule_content_hash",
	)
	lineNumericStorage := taxFactReportingInvalidNumericStorageSQL(
		[]string{"line.line_number", "line.debit_minor", "line.credit_minor"},
		[]string{"line.tax_amount_minor", "line.tax_rule_revision"},
	)
	lineCount, lineBytes, err := repository.reserveTaxFactReportingPayloadDetails(ctx, remainingBytes,
		"journal lines", maxTaxFactReportingJournalLines,
		`SELECT count(*), total(payload_bytes), coalesce(sum(invalid_numeric_storage), 0) FROM (
		 SELECT `+lineSize+` AS payload_bytes, `+lineNumericStorage+` AS invalid_numeric_storage
		 FROM journal_lines AS line WHERE journal_id = ?
		 AND EXISTS (SELECT 1 FROM journals WHERE id = line.journal_id AND organisation_id = ?)
		 ORDER BY line_number, id LIMIT ?
		)`, journalID, organisationID, maxTaxFactReportingJournalLines+1)
	if err != nil {
		return 0, 0, err
	}
	return headerBytes + lineBytes, lineCount, nil
}

// Each row costs its retained TEXT/BLOB bytes plus a conservative 512-byte
// allowance for scalar values, framing, tags and projection structure. This
// is a retained-payload budget, not an exact Go heap measurement. Parent raw
// and protobuf buffers are additional bounded copies of that payload.
// All column names supplied here are static repository-owned SQL identifiers.
func taxFactReportingPayloadSizeSQL(columns ...string) string {
	parts := []string{"512"}
	for _, column := range columns {
		parts = append(parts, "coalesce(length(CAST("+column+" AS BLOB)), 0)")
	}
	return strings.Join(parts, " + ")
}

// taxFactReportingInvalidNumericStorageSQL emits a row-level 0/1 expression.
// All identifiers passed here are static repository-owned SQL columns.
func taxFactReportingInvalidNumericStorageSQL(requiredColumns, nullableColumns []string) string {
	conditions := make([]string, 0, len(requiredColumns)+len(nullableColumns))
	for _, column := range requiredColumns {
		conditions = append(conditions, "typeof("+column+") <> 'integer'")
	}
	for _, column := range nullableColumns {
		conditions = append(conditions, "("+column+" IS NOT NULL AND typeof("+column+") <> 'integer')")
	}
	if len(conditions) == 0 {
		return "0"
	}
	return "CASE WHEN (" + strings.Join(conditions, " OR ") + ") THEN 1 ELSE 0 END"
}

func (repository *JournalRepository) reserveTaxFactReportingPayload(
	ctx context.Context, remainingBytes *int64, resource string, rowLimit int, query string, args ...any,
) error {
	_, _, err := repository.reserveTaxFactReportingPayloadDetails(ctx, remainingBytes, resource, rowLimit, query, args...)
	return err
}

func (repository *JournalRepository) reserveTaxFactReportingPayloadDetails(
	ctx context.Context, remainingBytes *int64, resource string, rowLimit int, query string, args ...any,
) (int, int64, error) {
	var count, invalidNumericRows int
	var payloadBytes float64
	// SQLite total() avoids integer-sum overflow for unconstrained legacy
	// cells. Integral byte counts remain exact throughout the 8 MiB budget.
	rows, err := repository.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, taxFactReportingReadError("preflight "+resource, err)
	}
	if !rows.Next() {
		return 0, 0, errors.Join(ErrJournalRepository, sql.ErrNoRows, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&count, &payloadBytes, &invalidNumericRows); err != nil {
		return 0, 0, errors.Join(taxFactReportingReadError("scan preflight "+resource, err), rows.Close())
	}
	if rows.Next() {
		return 0, 0, errors.Join(ErrJournalRepository, errors.New("preflight returned multiple totals"), rows.Close())
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return 0, 0, errors.Join(ErrJournalRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, errors.Join(ErrJournalRepository, err)
	}
	if count > rowLimit {
		return 0, 0, &TaxFactReportingBoundError{Resource: resource, Limit: rowLimit}
	}
	if payloadBytes < 0 || math.IsNaN(payloadBytes) || math.IsInf(payloadBytes, 0) {
		return 0, 0, fmt.Errorf("%w: invalid %s payload size", ErrJournalRepository, resource)
	}
	if payloadBytes > float64(*remainingBytes) {
		return 0, 0, &TaxFactReportingBoundError{Resource: "capture bytes", Limit: maxTaxFactReportingCaptureBytes}
	}
	payloadByteCount := int64(payloadBytes)
	if float64(payloadByteCount) != payloadBytes {
		return 0, 0, fmt.Errorf("%w: non-integral %s payload size", ErrJournalRepository, resource)
	}
	if invalidNumericRows != 0 {
		return 0, 0, fmt.Errorf("%w: %s contain non-integer numeric storage", ErrJournalRepository, resource)
	}
	*remainingBytes -= payloadByteCount
	return count, payloadByteCount, nil
}

func (repository *JournalRepository) loadTaxFactReportingCandidates(
	ctx context.Context,
	organisationID string,
) ([]taxFactReportingCandidate, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code, fact.treatment,
		       fact.original_gross_minor, fact.original_net_minor, fact.original_gst_minor,
		       fact.attributed_gross_minor, fact.attributed_net_minor, fact.attributed_gst_minor,
		       fact.remaining_gross_minor, fact.remaining_net_minor, fact.remaining_gst_minor,
		       fact.tax_rule_type, fact.tax_rule_id, fact.tax_rule_revision, fact.tax_rule_content_hash,
		       fact.source_type, fact.source_id, fact.source_revision, fact.source_content_hash, fact.created_at,
		       line.journal_id, line.line_number, journal.organisation_id, journal.journal_date
		FROM tax_facts AS fact
	LEFT JOIN journal_lines AS line ON line.id = fact.journal_line_id
	LEFT JOIN journals AS journal ON journal.id = line.journal_id
		WHERE fact.organisation_id = ?
		ORDER BY journal.journal_date, journal.id, line.line_number, fact.id
		LIMIT ?`, organisationID, maxTaxFactReportingFacts+1)
	if err != nil {
		return nil, taxFactReportingReadError("query tax facts", err)
	}
	candidates := make([]taxFactReportingCandidate, 0, 64)
	for rows.Next() {
		candidate, scanErr := scanTaxFactReportingCandidate(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, taxFactReportingReadError("scan tax fact", scanErr)
		}
		candidates = append(candidates, candidate)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrJournalRepository, rowsErr, closeErr)
	}
	if len(candidates) > maxTaxFactReportingFacts {
		return nil, &TaxFactReportingBoundError{Resource: "tax facts", Limit: maxTaxFactReportingFacts}
	}
	return candidates, nil
}

func scanTaxFactReportingCandidate(scanner interface{ Scan(...any) error }) (taxFactReportingCandidate, error) {
	candidate := taxFactReportingCandidate{record: TaxFactReportingRecord{Rule: &tammyv1.SourceRef{}}}
	var ruleType, ruleID, sourceType, sourceID string
	var ruleRevision, sourceRevision int64
	var ruleHash, sourceHash []byte
	var journalID, parentOrganisationID, journalDate sql.NullString
	var lineNumber sql.NullInt64
	err := scanner.Scan(
		&candidate.record.ID, &candidate.record.OrganisationID, &candidate.record.JournalLineID,
		&candidate.record.TaxCode, &candidate.record.Treatment,
		&candidate.record.OriginalGrossMinor, &candidate.record.OriginalNetMinor, &candidate.record.OriginalGSTMinor,
		&candidate.record.AttributedGrossMinor, &candidate.record.AttributedNetMinor, &candidate.record.AttributedGSTMinor,
		&candidate.record.RemainingGrossMinor, &candidate.record.RemainingNetMinor, &candidate.record.RemainingGSTMinor,
		&ruleType, &ruleID, &ruleRevision, &ruleHash,
		&sourceType, &sourceID, &sourceRevision, &sourceHash, &candidate.record.CreatedAt,
		&journalID, &lineNumber, &parentOrganisationID, &journalDate,
	)
	if err != nil {
		return taxFactReportingCandidate{}, err
	}
	if ruleRevision <= 0 || sourceRevision <= 0 || len(ruleHash) != sha256.Size ||
		(sourceHash != nil && len(sourceHash) != sha256.Size) {
		return taxFactReportingCandidate{}, errors.New("tax fact provenance has invalid retained hash or revision")
	}
	candidate.record.Rule = &tammyv1.SourceRef{Type: ruleType, Id: ruleID, Revision: uint64(ruleRevision), ContentHash: bytes.Clone(ruleHash)}
	candidate.record.SourceType = sourceType
	candidate.record.SourceID = sourceID
	candidate.record.SourceRevision = uint64(sourceRevision)
	candidate.record.SourceContentHashPresent = sourceHash != nil
	if sourceHash != nil {
		candidate.record.SourceContentHash = bytes.Clone(sourceHash)
	}
	candidate.journalIDPresent = journalID.Valid
	candidate.lineNumberPresent = lineNumber.Valid
	candidate.parentOrganisationPresent = parentOrganisationID.Valid
	candidate.journalDatePresent = journalDate.Valid
	candidate.journalID = journalID.String
	candidate.lineNumber = lineNumber.Int64
	candidate.parentOrganisationID = parentOrganisationID.String
	candidate.journalDate = journalDate.String
	return candidate, nil
}

func taxFactReportingCandidatePayloadBytes(candidate taxFactReportingCandidate) int64 {
	var ruleType, ruleID string
	var ruleHash []byte
	if candidate.record.Rule != nil {
		ruleType, ruleID, ruleHash = candidate.record.Rule.Type, candidate.record.Rule.Id, candidate.record.Rule.ContentHash
	}
	return taxFactReportingPayloadBytes(
		[]byte(candidate.record.ID), []byte(candidate.record.OrganisationID), []byte(candidate.record.JournalLineID),
		[]byte(candidate.record.TaxCode), []byte(candidate.record.Treatment), []byte(ruleType), []byte(ruleID), ruleHash,
		[]byte(candidate.record.SourceType), []byte(candidate.record.SourceID), candidate.record.SourceContentHash,
		[]byte(candidate.record.CreatedAt), []byte(candidate.journalID), []byte(candidate.parentOrganisationID),
		[]byte(candidate.journalDate),
	)
}

func taxFactReportingPayloadBytes(values ...[]byte) int64 {
	var payloadBytes int64 = 512
	for _, value := range values {
		payloadBytes += int64(len(value))
	}
	return payloadBytes
}

func taxFactReportingNullableStringPayloadBytes(value sql.NullString) []byte {
	if !value.Valid {
		return nil
	}
	return []byte(value.String)
}

func (repository *JournalRepository) loadTaxFactReportingJournal(
	ctx context.Context,
	journalID, organisationID string,
) (*taxFactReportingJournalSnapshot, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, organisation_id, version, source_type, source_kind, source_id, source_revision,
		       state, journal_date, description, reversal_of_journal_id, reversed_by_journal_id,
		       total_debits_minor, total_credits_minor, currency_code, financial_revision, posted_at, created_at
		FROM journals WHERE id = ? AND organisation_id = ?`, journalID, organisationID)
	if err != nil {
		return nil, taxFactReportingReadError("query parent journal", err)
	}
	if !rows.Next() {
		rowsErr := rows.Err()
		closeErr := rows.Close()
		if rowsErr != nil || closeErr != nil {
			return nil, errors.Join(ErrJournalRepository, rowsErr, closeErr)
		}
		return nil, fmt.Errorf("%w: tax fact parent journal %s is missing", ErrJournalRepository, journalID)
	}

	var id, parentOrganisation, sourceType, sourceID, state, journalDate, description, currencyCode, createdAt string
	var version, sourceRevision, totalDebits, totalCredits, financialRevision, sourceKind int64
	var reversalOf, reversedBy, postedAt sql.NullString
	if err := rows.Scan(&id, &parentOrganisation, &version, &sourceType, &sourceKind, &sourceID, &sourceRevision,
		&state, &journalDate, &description, &reversalOf, &reversedBy, &totalDebits, &totalCredits, &currencyCode,
		&financialRevision, &postedAt, &createdAt); err != nil {
		_ = rows.Close()
		return nil, taxFactReportingReadError("scan parent journal", err)
	}
	if rows.Next() || rows.Err() != nil {
		rowsErr := rows.Err()
		_ = rows.Close()
		return nil, errors.Join(ErrJournalRepository, rowsErr, errors.New("parent journal ID is not unique"))
	}
	if err := rows.Close(); err != nil {
		return nil, taxFactReportingReadError("close parent journal rows", err)
	}
	if id != journalID || parentOrganisation != organisationID || version <= 0 || sourceRevision <= 0 ||
		financialRevision <= 0 || parseCivilDate(journalDate) == nil {
		return nil, fmt.Errorf("%w: parent journal %s has invalid identity, ownership, revision, or date", ErrJournalRepository, journalID)
	}

	journal := &tammyv1.Journal{
		Id: id, OrganisationId: parentOrganisation, Version: uint64(version),
		Source: parseJournalSource(sourceType, sourceKind), State: parseJournalState(state),
		PostingDate: parseCivilDate(journalDate), Memo: description,
		TotalDebits:       &tammyv1.Money{CurrencyCode: currencyCode, MinorUnits: totalDebits},
		TotalCredits:      &tammyv1.Money{CurrencyCode: currencyCode, MinorUnits: totalCredits},
		FinancialRevision: uint64(financialRevision),
	}
	if reversalOf.Valid {
		journal.ReversalOfJournalId = &reversalOf.String
	}
	if reversedBy.Valid {
		journal.ReversedByJournalId = &reversedBy.String
	}
	if postedAt.Valid {
		postedInstant, err := time.Parse(time.RFC3339Nano, postedAt.String)
		if err != nil {
			return nil, errors.Join(ErrJournalRepository,
				fmt.Errorf("parent journal %s has an invalid posted time: %w", journalID, err))
		}
		journal.PostedAt = timestamppb.New(postedInstant)
	}

	headerPayloadBytes := taxFactReportingPayloadBytes(
		[]byte(id), []byte(parentOrganisation), []byte(sourceType), []byte(sourceID), []byte(state),
		[]byte(journalDate), []byte(description), taxFactReportingNullableStringPayloadBytes(reversalOf),
		taxFactReportingNullableStringPayloadBytes(reversedBy), []byte(currencyCode),
		taxFactReportingNullableStringPayloadBytes(postedAt), []byte(createdAt),
	)
	lines, rawLines, linePayloadBytes, err := repository.loadTaxFactReportingLines(ctx, journalID)
	if err != nil {
		return nil, err
	}
	journal.Lines = lines
	protoBytes, err := (proto.MarshalOptions{Deterministic: true}).Marshal(journal)
	if err != nil {
		return nil, taxFactReportingReadError("marshal journal projection", err)
	}

	raw := &bytes.Buffer{}
	appendTaxFactReportingFrame(raw, []byte("journal-row/v1"))
	appendTaxFactReportingFrame(raw, []byte(id))
	appendTaxFactReportingFrame(raw, []byte(parentOrganisation))
	appendTaxFactReportingInt64(raw, version)
	appendTaxFactReportingFrame(raw, []byte(sourceType))
	appendTaxFactReportingInt64(raw, sourceKind)
	appendTaxFactReportingFrame(raw, []byte(sourceID))
	appendTaxFactReportingInt64(raw, sourceRevision)
	appendTaxFactReportingFrame(raw, []byte(state))
	appendTaxFactReportingFrame(raw, []byte(journalDate))
	appendTaxFactReportingFrame(raw, []byte(description))
	appendTaxFactReportingNullableString(raw, reversalOf)
	appendTaxFactReportingNullableString(raw, reversedBy)
	appendTaxFactReportingInt64(raw, totalDebits)
	appendTaxFactReportingInt64(raw, totalCredits)
	appendTaxFactReportingFrame(raw, []byte(currencyCode))
	appendTaxFactReportingInt64(raw, financialRevision)
	appendTaxFactReportingNullableString(raw, postedAt)
	appendTaxFactReportingFrame(raw, []byte(createdAt))
	appendTaxFactReportingFrame(raw, rawLines)

	return &taxFactReportingJournalSnapshot{
		journal: journal, journalProto: protoBytes, raw: raw.Bytes(), organisation: parentOrganisation,
		payloadBytes: headerPayloadBytes + linePayloadBytes, lineCount: len(lines),
		sourceType: sourceType, sourceKind: sourceKind, sourceID: sourceID,
		sourceRev: uint64(sourceRevision), createdAt: createdAt, state: state,
	}, nil
}

func (repository *JournalRepository) loadTaxFactReportingLines(
	ctx context.Context,
	journalID string,
) ([]*tammyv1.JournalLine, []byte, int64, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, journal_id, line_number, account_id, debit_minor, credit_minor, currency_code, memo,
		       tax_code_id, tax_amount_minor, tax_rule_type, tax_rule_id, tax_rule_revision, tax_rule_content_hash
		FROM journal_lines WHERE journal_id = ? ORDER BY line_number, id LIMIT ?`, journalID, maxTaxFactReportingJournalLines+1)
	if err != nil {
		return nil, nil, 0, taxFactReportingReadError("query journal lines", err)
	}
	lines := make([]*tammyv1.JournalLine, 0, 16)
	rawLines := &bytes.Buffer{}
	var payloadBytes int64
	for rows.Next() {
		if len(lines) == maxTaxFactReportingJournalLines {
			_ = rows.Close()
			return nil, nil, 0, &TaxFactReportingBoundError{Resource: "journal lines", Limit: maxTaxFactReportingJournalLines}
		}
		line, err := scanTaxFactReportingLine(rows)
		if err != nil {
			_ = rows.Close()
			return nil, nil, 0, taxFactReportingReadError("scan journal line", err)
		}
		if line.journalID != journalID || line.lineNumber < 1 || line.lineNumber > math.MaxUint32 {
			_ = rows.Close()
			return nil, nil, 0, fmt.Errorf("%w: journal %s contains an invalid line identity or sequence", ErrJournalRepository, journalID)
		}
		payloadBytes += taxFactReportingRawLinePayloadBytes(line)
		appendTaxFactReportingRawLine(rawLines, line)
		lines = append(lines, line.line)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, nil, 0, errors.Join(ErrJournalRepository, rowsErr, closeErr)
	}
	appendTaxFactReportingInt64(rawLines, int64(len(lines)))
	return lines, rawLines.Bytes(), payloadBytes, nil
}

func taxFactReportingRawLinePayloadBytes(line taxFactReportingRawLine) int64 {
	return taxFactReportingPayloadBytes(
		[]byte(line.id), []byte(line.journalID), []byte(line.accountID), []byte(line.currency),
		taxFactReportingNullableStringPayloadBytes(line.memo),
		taxFactReportingNullableStringPayloadBytes(line.taxCode),
		taxFactReportingNullableStringPayloadBytes(line.ruleType),
		taxFactReportingNullableStringPayloadBytes(line.ruleID), line.ruleHash,
	)
}

func scanTaxFactReportingLine(scanner interface{ Scan(...any) error }) (taxFactReportingRawLine, error) {
	var raw taxFactReportingRawLine
	var memo, taxCode, ruleType, ruleID sql.NullString
	var taxAmount, ruleRevision sql.NullInt64
	var ruleHash []byte
	if err := scanner.Scan(&raw.id, &raw.journalID, &raw.lineNumber, &raw.accountID, &raw.debit, &raw.credit,
		&raw.currency, &memo, &taxCode, &taxAmount, &ruleType, &ruleID, &ruleRevision, &ruleHash); err != nil {
		return taxFactReportingRawLine{}, err
	}
	if raw.lineNumber < 1 || raw.currency == "" || ruleHash != nil && len(ruleHash) != sha256.Size {
		return taxFactReportingRawLine{}, errors.New("journal line contains invalid retained data")
	}
	raw.memo, raw.taxCode, raw.taxAmount = memo, taxCode, taxAmount
	raw.ruleType, raw.ruleID, raw.ruleRevision, raw.ruleHash = ruleType, ruleID, ruleRevision, bytes.Clone(ruleHash)
	line := &tammyv1.JournalLine{
		Id: raw.id, JournalId: raw.journalID, AccountId: raw.accountID, Sequence: uint32(raw.lineNumber),
		Debit:  &tammyv1.Money{CurrencyCode: raw.currency, MinorUnits: raw.debit},
		Credit: &tammyv1.Money{CurrencyCode: raw.currency, MinorUnits: raw.credit},
	}
	if memo.Valid {
		line.Description = memo.String
	}
	if taxCode.Valid {
		value := taxCode.String
		line.TaxCodeId = &value
	}
	if taxAmount.Valid {
		line.TaxAmount = &tammyv1.Money{CurrencyCode: raw.currency, MinorUnits: taxAmount.Int64}
	}
	if ruleType.Valid || ruleID.Valid || ruleRevision.Valid || ruleHash != nil {
		line.TaxRule = &tammyv1.SourceRef{Type: ruleType.String, Id: ruleID.String, ContentHash: bytes.Clone(ruleHash)}
		if ruleRevision.Valid && ruleRevision.Int64 > 0 {
			line.TaxRule.Revision = uint64(ruleRevision.Int64)
		}
	}
	raw.line = line
	return raw, nil
}

func appendTaxFactReportingRawLine(buffer *bytes.Buffer, line taxFactReportingRawLine) {
	appendTaxFactReportingFrame(buffer, []byte("journal-line/v1"))
	appendTaxFactReportingFrame(buffer, []byte(line.id))
	appendTaxFactReportingFrame(buffer, []byte(line.journalID))
	appendTaxFactReportingInt64(buffer, line.lineNumber)
	appendTaxFactReportingFrame(buffer, []byte(line.accountID))
	appendTaxFactReportingInt64(buffer, line.debit)
	appendTaxFactReportingInt64(buffer, line.credit)
	appendTaxFactReportingFrame(buffer, []byte(line.currency))
	appendTaxFactReportingNullableString(buffer, line.memo)
	appendTaxFactReportingNullableString(buffer, line.taxCode)
	appendTaxFactReportingNullableInt64(buffer, line.taxAmount)
	appendTaxFactReportingNullableString(buffer, line.ruleType)
	appendTaxFactReportingNullableString(buffer, line.ruleID)
	appendTaxFactReportingNullableInt64(buffer, line.ruleRevision)
	appendTaxFactReportingNullableBytes(buffer, line.ruleHash)
}

func taxFactReportingFingerprint(
	ctx context.Context,
	organisationID string,
	records []TaxFactReportingRecord,
	journalCache map[string]*taxFactReportingJournalSnapshot,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	digest := sha256.New()
	hashTaxFactReportingFrame(digest, []byte(taxFactReportingFingerprintTag))
	hashTaxFactReportingFrame(digest, []byte(organisationID))
	hashTaxFactReportingInt64(digest, int64(len(records)))
	journalIDs := make([]string, 0, len(journalCache))
	for journalID := range journalCache {
		journalIDs = append(journalIDs, journalID)
	}
	sort.Strings(journalIDs)
	hashTaxFactReportingInt64(digest, int64(len(journalIDs)))
	for _, journalID := range journalIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		snapshot := journalCache[journalID]
		if snapshot == nil || snapshot.journal == nil || snapshot.journal.Id != journalID {
			return nil, errors.New("tax fact reporting journal snapshot is missing")
		}
		hashTaxFactReportingFrame(digest, []byte(journalID))
		hashTaxFactReportingFrame(digest, snapshot.journalProto)
		hashTaxFactReportingFrame(digest, snapshot.raw)
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, value := range []string{record.ID, record.OrganisationID, record.JournalLineID, record.TaxCode, record.Treatment} {
			hashTaxFactReportingFrame(digest, []byte(value))
		}
		hashTaxFactReportingInt64(digest, record.OriginalGrossMinor)
		hashTaxFactReportingInt64(digest, record.OriginalNetMinor)
		hashTaxFactReportingInt64(digest, record.OriginalGSTMinor)
		hashTaxFactReportingInt64(digest, record.AttributedGrossMinor)
		hashTaxFactReportingInt64(digest, record.AttributedNetMinor)
		hashTaxFactReportingInt64(digest, record.AttributedGSTMinor)
		hashTaxFactReportingInt64(digest, record.RemainingGrossMinor)
		hashTaxFactReportingInt64(digest, record.RemainingNetMinor)
		hashTaxFactReportingInt64(digest, record.RemainingGSTMinor)
		if record.Rule == nil {
			hashTaxFactReportingBool(digest, false)
		} else {
			hashTaxFactReportingBool(digest, true)
			hashTaxFactReportingFrame(digest, []byte(record.Rule.Type))
			hashTaxFactReportingFrame(digest, []byte(record.Rule.Id))
			hashTaxFactReportingUint64(digest, record.Rule.Revision)
			hashTaxFactReportingFrame(digest, record.Rule.ContentHash)
		}
		hashTaxFactReportingFrame(digest, []byte(record.SourceType))
		hashTaxFactReportingFrame(digest, []byte(record.SourceID))
		hashTaxFactReportingUint64(digest, record.SourceRevision)
		hashTaxFactReportingBool(digest, record.SourceContentHashPresent)
		if record.SourceContentHashPresent {
			hashTaxFactReportingFrame(digest, record.SourceContentHash)
		}
		hashTaxFactReportingFrame(digest, []byte(record.CreatedAt))
		// All parent metadata is committed once in its complete raw/protobuf
		// snapshot above. Bind each fact to that parent without rehashing its
		// potentially large strings once per fact.
		if record.Journal == nil || journalCache[record.Journal.Id] == nil {
			return nil, errors.New("tax fact reporting journal projection is missing")
		}
		hashTaxFactReportingFrame(digest, []byte(record.Journal.Id))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
}

func taxFactReportingReadError(operation string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrJournalRepository, operation)
	}
	return errors.Join(ErrJournalRepository, fmt.Errorf("%s: %w", operation, err))
}

func appendTaxFactReportingFrame(buffer *bytes.Buffer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = buffer.Write(length[:])
	_, _ = buffer.Write(value)
}

func appendTaxFactReportingInt64(buffer *bytes.Buffer, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = buffer.Write(encoded[:])
}

func appendTaxFactReportingNullableString(buffer *bytes.Buffer, value sql.NullString) {
	appendTaxFactReportingBool(buffer, value.Valid)
	if value.Valid {
		appendTaxFactReportingFrame(buffer, []byte(value.String))
	}
}

func appendTaxFactReportingNullableInt64(buffer *bytes.Buffer, value sql.NullInt64) {
	appendTaxFactReportingBool(buffer, value.Valid)
	if value.Valid {
		appendTaxFactReportingInt64(buffer, value.Int64)
	}
}

func appendTaxFactReportingNullableBytes(buffer *bytes.Buffer, value []byte) {
	appendTaxFactReportingBool(buffer, value != nil)
	if value != nil {
		appendTaxFactReportingFrame(buffer, value)
	}
}

func appendTaxFactReportingBool(buffer *bytes.Buffer, value bool) {
	if value {
		_, _ = buffer.Write([]byte{1})
		return
	}
	_, _ = buffer.Write([]byte{0})
}

func hashTaxFactReportingFrame(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

func hashTaxFactReportingInt64(digest hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = digest.Write(encoded[:])
}

func hashTaxFactReportingUint64(digest hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = digest.Write(encoded[:])
}

func hashTaxFactReportingBool(digest hash.Hash, value bool) {
	if value {
		_, _ = digest.Write([]byte{1})
		return
	}
	_, _ = digest.Write([]byte{0})
}
