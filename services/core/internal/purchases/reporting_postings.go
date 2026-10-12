package purchases

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
	"strconv"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const (
	maximumPostingReportingRoots       = 1000
	maximumPostingReportingPayments    = 1000
	maximumPostingReportingPayloadSize = int64(8 * 1024 * 1024)
	postingReportingRowOverhead        = int64(512)
	postingReportingFingerprintTag     = "tammy/purchases/posting-reporting/v1"
)

// SourceBoundError identifies a finite modern purchase reporting bound.
// Callers may use errors.Is(err, ErrSourceBound) when the exact resource is
// not relevant.
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

// PostingReportingCapture is a read-only projection of modern purchase
// postings and their retained history. It deliberately excludes legacy
// documents and opening-payable roots, so it is not a complete purchase
// census. Fingerprint commits the retained SQL rows and unique journal proofs;
// PayloadBytes is the bounded retained-cell payload, not a Go heap estimate.
// Hydrated protobuf and document values are additional bounded copies.
type PostingReportingCapture struct {
	OrganisationID string
	Postings       []PostingReportingRecord
	Documents      []PostingDocumentReportingRecord
	Journals       []*accounting.JournalReportingRecord
	Rows           []PurchaseReportingRetainedRow
	Fingerprint    []byte
	PayloadBytes   int64
}

type PostingReportingRecord struct {
	Posting    *tammyv1.PurchasePosting
	Accounting *tammyv1.PurchaseAccounting
	Payments   []*tammyv1.PurchasePayment
	Evidences  []*tammyv1.PurchaseTaxEvidence
	Claims     VerifiedClaimHistory
}

type PostingDocumentReportingRecord struct {
	Document *tammyv1.Document
}

// PurchaseReportingRetainedRow preserves a complete known SQL row. Column
// names are fixed by this package; the capture never introspects the schema
// or uses caller-provided SQL identifiers.
type PurchaseReportingRetainedRow struct {
	Table   string
	Columns []string
	Values  []PurchaseReportingRetainedValue
}

type PurchaseReportingRetainedValue struct {
	Kind    string
	Integer int64
	Text    string
	Bytes   []byte
}

type postingReportingQuery struct {
	table   string
	alias   string
	columns []string
	from    string
	where   string
	args    []any
	orderBy string
	limit   string
}

type postingReportingRowsResult struct {
	rows        []PurchaseReportingRetainedRow
	payloadSize int64
}

// CapturePostingsForReporting reads the complete bounded set of modern
// purchase postings through the caller-owned executor. It does not read
// legacy/opening purchase roots, change the transaction, or apply BAS rules.
func (r *Repository) CapturePostingsForReporting(
	ctx context.Context,
	organisationID string,
) (*PostingReportingCapture, error) {
	return r.CapturePostingsForReportingWithPayloadBudget(ctx, organisationID, maximumPostingReportingPayloadSize)
}

// CapturePostingsForReportingWithPayloadBudget captures all modern purchase
// posting history for one organisation. It performs SQL-side byte preflights
// before loading retained protobufs, source documents, cash-flow facts, or
// journals. A larger caller budget is clamped to the package's fixed cap.
// Legacy document and opening-payable candidates are intentionally outside
// this capture and must be read through their owning repositories.
func (r *Repository) CapturePostingsForReportingWithPayloadBudget(
	ctx context.Context,
	organisationID string,
	maximumPayloadBytes int64,
) (*PostingReportingCapture, error) {
	if r == nil || r.executor == nil || r.now == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || maximumPayloadBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if maximumPayloadBytes > maximumPostingReportingPayloadSize {
		maximumPayloadBytes = maximumPostingReportingPayloadSize
	}

	rootCount, err := postingReportingCount(ctx, r.executor,
		`SELECT count(*) FROM (SELECT 1 FROM purchase_postings_v1 WHERE organisation_id=? ORDER BY id LIMIT ?)`,
		organisationID, maximumPostingReportingRoots+1)
	if err != nil {
		return nil, postingReportingReadError(ctx, err)
	}
	if rootCount > maximumPostingReportingRoots {
		return nil, &SourceBoundError{Resource: "posting roots", Limit: maximumPostingReportingRoots}
	}
	paymentCount, err := postingReportingCount(ctx, r.executor,
		`SELECT count(*) FROM (SELECT 1 FROM purchase_payments_v1 WHERE organisation_id=? ORDER BY posting_id,accounting_version,id LIMIT ?)`,
		organisationID, maximumPostingReportingPayments+1)
	if err != nil {
		return nil, postingReportingReadError(ctx, err)
	}
	if paymentCount > maximumPostingReportingPayments {
		return nil, &SourceBoundError{Resource: "payments", Limit: maximumPostingReportingPayments}
	}
	if rootCount == 0 {
		if paymentCount != 0 {
			return nil, ErrRepository
		}
		fingerprint, err := postingReportingFingerprint(ctx, organisationID, nil, nil)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		return &PostingReportingCapture{OrganisationID: organisationID, Fingerprint: fingerprint}, nil
	}

	remaining := maximumPayloadBytes
	queries := postingReportingSourceQueries(organisationID)
	preflightSizes := make([]int64, 0, len(queries))
	for _, query := range queries {
		size, _, err := preflightPostingReportingQuery(ctx, r.executor, query, &remaining, maximumPayloadBytes)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		preflightSizes = append(preflightSizes, size)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}

	retainedRows := make([]PurchaseReportingRetainedRow, 0, len(queries)*4)
	var sourcePayload int64
	for index, query := range queries {
		loaded, err := loadPostingReportingQuery(ctx, r.executor, query)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		if loaded.payloadSize != preflightSizes[index] {
			return nil, postingReportingReadError(ctx, errors.Join(ErrRepository, errors.New("purchase reporting source changed during capture")))
		}
		var ok bool
		sourcePayload, ok = addPostingReportingBytes(sourcePayload, loaded.payloadSize)
		if !ok {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
		retainedRows = append(retainedRows, loaded.rows...)
	}
	if sourcePayload > maximumPayloadBytes {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}

	documentIDs, journalIDs, needsExceptionRule, err := discoverPostingReportingReferences(ctx, retainedRows)
	if err != nil {
		return nil, postingReportingReadError(ctx, err)
	}
	if len(documentIDs) == 0 {
		return nil, ErrRepository
	}

	documentQueries := postingReportingDocumentQueries(organisationID, documentIDs)
	documentPreflightSizes := make([]int64, 0, len(documentQueries))
	for _, query := range documentQueries {
		size, count, err := preflightPostingReportingQuery(ctx, r.executor, query, &remaining, maximumPayloadBytes)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		if count != int64(len(query.args)-1) {
			return nil, ErrRepository
		}
		documentPreflightSizes = append(documentPreflightSizes, size)
	}
	if needsExceptionRule {
		query := postingReportingLowValueRuleQuery()
		size, count, err := preflightPostingReportingQuery(ctx, r.executor, query, &remaining, maximumPayloadBytes)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		if count != 1 {
			return nil, ErrRepository
		}
		documentPreflightSizes = append(documentPreflightSizes, size)
	}

	journalFactQueries := postingReportingJournalFactQueries(organisationID, journalIDs)
	journalFactPreflightSizes := make([]int64, 0, len(journalFactQueries))
	for _, query := range journalFactQueries {
		size, _, err := preflightPostingReportingQuery(ctx, r.executor, query, &remaining, maximumPayloadBytes)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		journalFactPreflightSizes = append(journalFactPreflightSizes, size)
	}
	controlAccountQueries := postingReportingControlAccountQueries(organisationID, len(journalIDs) > 0)
	controlAccountPreflightSizes := make([]int64, 0, len(controlAccountQueries))
	for _, query := range controlAccountQueries {
		size, _, err := preflightPostingReportingQuery(ctx, r.executor, query, &remaining, maximumPayloadBytes)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		controlAccountPreflightSizes = append(controlAccountPreflightSizes, size)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}

	// Document originals and support facts are hydrated only after all of their
	// SQL cells have been reserved from the common retained-payload budget.
	for index, query := range documentQueries {
		loaded, err := loadPostingReportingQuery(ctx, r.executor, query)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		if loaded.payloadSize != documentPreflightSizes[index] {
			return nil, postingReportingReadError(ctx, errors.Join(ErrRepository, errors.New("purchase reporting document changed during capture")))
		}
		var ok bool
		sourcePayload, ok = addPostingReportingBytes(sourcePayload, loaded.payloadSize)
		if !ok || sourcePayload > maximumPayloadBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
		retainedRows = append(retainedRows, loaded.rows...)
	}
	if needsExceptionRule {
		query := postingReportingLowValueRuleQuery()
		loaded, err := loadPostingReportingQuery(ctx, r.executor, query)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		want := documentPreflightSizes[len(documentQueries)]
		if loaded.payloadSize != want || len(loaded.rows) != 1 {
			return nil, postingReportingReadError(ctx, errors.Join(ErrRepository, errors.New("purchase exception rule changed during capture")))
		}
		var ok bool
		sourcePayload, ok = addPostingReportingBytes(sourcePayload, loaded.payloadSize)
		if !ok || sourcePayload > maximumPayloadBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
		retainedRows = append(retainedRows, loaded.rows...)
	}
	for index, query := range journalFactQueries {
		loaded, err := loadPostingReportingQuery(ctx, r.executor, query)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		if loaded.payloadSize != journalFactPreflightSizes[index] {
			return nil, postingReportingReadError(ctx, errors.Join(ErrRepository, errors.New("purchase journal facts changed during capture")))
		}
		var ok bool
		sourcePayload, ok = addPostingReportingBytes(sourcePayload, loaded.payloadSize)
		if !ok || sourcePayload > maximumPayloadBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
		retainedRows = append(retainedRows, loaded.rows...)
	}
	for index, query := range controlAccountQueries {
		loaded, err := loadPostingReportingQuery(ctx, r.executor, query)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		if loaded.payloadSize != controlAccountPreflightSizes[index] {
			return nil, postingReportingReadError(ctx, errors.Join(ErrRepository, errors.New("purchase control accounts changed during capture")))
		}
		var ok bool
		sourcePayload, ok = addPostingReportingBytes(sourcePayload, loaded.payloadSize)
		if !ok || sourcePayload > maximumPayloadBytes {
			return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
		}
		retainedRows = append(retainedRows, loaded.rows...)
	}

	journalRecords, journalPayload, err := loadPostingReportingJournals(ctx, r.executor, organisationID, journalIDs, remaining, maximumPayloadBytes)
	if err != nil {
		return nil, postingReportingReadError(ctx, err)
	}
	remaining -= journalPayload
	if remaining < 0 {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	if ctx.Err() != nil {
		return nil, errors.Join(ErrRepository, ctx.Err())
	}

	reader := *r
	readAsOf := r.now().UTC()
	if readAsOf.IsZero() {
		return nil, ErrRepository
	}
	reader.now = func() time.Time { return readAsOf }
	postings := make([]PostingReportingRecord, 0, int(rootCount))
	for _, documentID := range postingReportingRootDocumentIDs(retainedRows) {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		state, err := reader.Get(ctx, organisationID, documentID)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		if state == nil || !state.Posted || state.Posting == nil || state.Posting.DocumentId != documentID || state.OrganisationId != organisationID {
			return nil, ErrRepository
		}
		payments, err := reader.loadPayments(ctx, state.Posting)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		evidences, err := reader.retainedEvidenceHistory(ctx, state.Posting)
		if err != nil || len(evidences) == 0 {
			return nil, postingReportingReadError(ctx, errors.Join(ErrRepository, err))
		}
		claims, err := reader.VerifiedClaims(ctx, organisationID, documentID)
		if err != nil {
			return nil, postingReportingReadError(ctx, err)
		}
		postings = append(postings, PostingReportingRecord{Posting: state.Posting, Accounting: state,
			Payments: payments, Evidences: evidences, Claims: claims})
	}

	documents := make([]PostingDocumentReportingRecord, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
		document, err := reader.document(ctx, organisationID, documentID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrRepository
			}
			return nil, postingReportingReadError(ctx, err)
		}
		documents = append(documents, PostingDocumentReportingRecord{Document: document})
	}

	payloadBytes, ok := addPostingReportingBytes(sourcePayload, journalPayload)
	if !ok || payloadBytes > maximumPayloadBytes {
		return nil, &SourceBoundError{Resource: "capture bytes", Limit: maximumPayloadBytes}
	}
	fingerprint, err := postingReportingFingerprint(ctx, organisationID, retainedRows, journalRecords)
	if err != nil {
		return nil, postingReportingReadError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return &PostingReportingCapture{
		OrganisationID: organisationID, Postings: postings, Documents: documents,
		Journals: journalRecords, Rows: retainedRows, Fingerprint: fingerprint, PayloadBytes: payloadBytes,
	}, nil
}

func postingReportingSourceQueries(organisationID string) []postingReportingQuery {
	const rootIDs = `SELECT id FROM purchase_postings_v1 WHERE organisation_id=?`
	const eventKeys = `SELECT e.operation_key FROM purchase_accounting_events_v1 AS e WHERE e.organisation_id=? AND e.scope_kind='POSTING' AND e.scope_id IN (` + rootIDs + `)`
	return []postingReportingQuery{
		{table: "purchase_postings_v1", alias: "p", columns: strings.Fields("id organisation_id document_id reviewed_version original_content_hash reviewed_snapshot_hash reviewed_source_proto supplier_name supplier_identity invoice_number invoice_date gst_basis gst_profile_version gst_history_watermark gross_minor supplier_gst_minor eligible_gst_minor currency_code journal_id financial_revision duplicate_decision duplicate_reason prior_accounting_decision prior_accounting_reason normalized_supplier normalized_invoice source_hash posting_proto actor_user_id created_at"), from: "purchase_postings_v1 AS p", where: "p.organisation_id=?", args: []any{organisationID}, orderBy: "p.id", limit: " LIMIT 1001"},
		{table: "purchase_posting_allocations_v1", alias: "a", columns: strings.Fields("posting_id organisation_id sequence account_id account_version gross_minor supplier_gst_minor eligible_gst_minor use_kind bas_classification cash_flow_classification noncreditable_reason allocation_proto"), from: "purchase_posting_allocations_v1 AS a", where: "a.organisation_id=? AND a.posting_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "a.posting_id,a.sequence"},
		{table: "purchase_payments_v1", alias: "p", columns: strings.Fields("id posting_id organisation_id accounting_version payment_date amount_minor currency_code bank_account_id journal_id reference payment_proto source_hash actor_user_id created_at"), from: "purchase_payments_v1 AS p", where: "p.organisation_id=? AND p.posting_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "p.posting_id,p.accounting_version,p.id", limit: " LIMIT 1001"},
		{table: "purchase_payment_allocations_v1", alias: "a", columns: strings.Fields("payment_id posting_id organisation_id sequence posting_allocation_sequence gross_minor supplier_gst_minor eligible_gst_minor use_kind bas_classification cash_flow_classification allocation_proto"), from: "purchase_payment_allocations_v1 AS a", where: "a.organisation_id=? AND a.posting_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "a.posting_id,a.payment_id,a.sequence"},
		{table: "purchase_tax_evidence_v1", alias: "e", columns: strings.Fields("id organisation_id posting_id document_id accounting_version classification source_document_id source_version source_hash held_date registration_review_confirmed business_use_review_confirmed evidence_review_confirmed reason evidence_proto record_hash actor_user_id created_at"), from: "purchase_tax_evidence_v1 AS e", where: "e.organisation_id=? AND e.posting_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "e.posting_id,e.accounting_version,e.id"},
		{table: "purchase_accounting_events_v1", alias: "e", columns: strings.Fields("operation_key organisation_id scope_kind scope_id posting_id document_id opening_payable_id accounting_version command_type financial_revision ledger_revision tax_source_revision settlement_revision result_sha256 result_proto journal_id created_at"), from: "purchase_accounting_events_v1 AS e", where: "e.organisation_id=? AND e.scope_kind='POSTING' AND e.scope_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "e.scope_id,e.accounting_version,e.operation_key"},
		{table: "purchase_gst_deferrals_v1", alias: "d", columns: strings.Fields("id organisation_id posting_id document_id accounting_version affected_period_start affected_period_end reviewed_as_of timing_reason deferred_gst_minor currency_code ordinary_last_claim_date reason deferral_proto source_hash actor_user_id created_at"), from: "purchase_gst_deferrals_v1 AS d", where: "d.organisation_id=? AND d.posting_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "d.posting_id,d.accounting_version,d.id"},
		{table: "purchase_gst_deferral_portions_v1", alias: "p", columns: strings.Fields("deferral_id sequence organisation_id posting_id deferred_gst_minor source_kind payment_id allocation_sequence normal_event_date earliest_period_start earliest_period_end required_return_due_date due_date_source_id due_date_source_version due_date_source_hash ordinary_last_claim_date evidence_id evidence_held_date portion_proto source_hash"), from: "purchase_gst_deferral_portions_v1 AS p", where: "p.organisation_id=? AND p.posting_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "p.posting_id,p.deferral_id,p.sequence"},
		{table: "purchase_gst_attributions_v1", alias: "a", columns: strings.Fields("id operation_key organisation_id posting_id document_id accounting_version source_kind payment_id allocation_sequence normal_event_date earliest_period_start earliest_period_end required_return_due_date due_date_source_id due_date_source_version due_date_source_hash ordinary_last_claim_date evidence_id evidence_held_date claim_period_start claim_period_end release_posting_date reviewed_as_of timing_reason claimed_gst_minor currency_code closes_deferral_id journal_id financial_revision source_hash attribution_proto actor_user_id created_at"), from: "purchase_gst_attributions_v1 AS a", where: "a.organisation_id=? AND a.posting_id IN (" + rootIDs + ")", args: []any{organisationID, organisationID}, orderBy: "a.posting_id,a.accounting_version,a.operation_key,a.id"},
		{table: "financial_revision_claims", alias: "c", columns: strings.Fields("operation_key domain_mask financial_revision ledger_revision settlement_revision banking_revision tax_source_revision organisation_profile_revision rule_bundle_revision updated_at"), from: "financial_revision_claims AS c", where: "c.operation_key IN (" + eventKeys + ")", args: []any{organisationID, organisationID}, orderBy: "c.operation_key"},
		{table: "command_idempotency_v1", alias: "c", columns: strings.Fields("workspace_id actor_user_id fully_qualified_rpc_name operation_key semantic_hash_version request_type normalized_hash result_type result_proto outcome failure_code result_resource_id attempt retention_policy created_at completed_at"), from: "command_idempotency_v1 AS c", where: "c.operation_key IN (" + eventKeys + ")", args: []any{organisationID, organisationID}, orderBy: "c.operation_key"},
	}
}

func postingReportingDocumentQueries(organisationID string, documentIDs []string) []postingReportingQuery {
	queries := make([]postingReportingQuery, 0, (len(documentIDs)+499)/500)
	columns := strings.Fields("id operation_key organisation_id version status source_display_name mime_type byte_length sha256 original_bytes extracted_text supplier_name invoice_number document_date subtotal_minor gst_minor total_minor created_by_user_id created_at reviewed_at")
	for start := 0; start < len(documentIDs); start += 500 {
		end := start + 500
		if end > len(documentIDs) {
			end = len(documentIDs)
		}
		batch := append([]string(nil), documentIDs[start:end]...)
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+1)
		for _, id := range batch {
			args = append(args, id)
		}
		args = append(args, organisationID)
		queries = append(queries, postingReportingQuery{
			table: "documents", alias: "d", columns: columns, from: "documents AS d",
			where: "d.id IN (" + placeholders + ") AND d.organisation_id=?", args: args, orderBy: "d.id",
		})
	}
	return queries
}

func postingReportingLowValueRuleQuery() postingReportingQuery {
	return postingReportingQuery{
		table: "rule_bundles", alias: "b",
		columns: strings.Fields("id organisation_id bundle_type version semantic_sha256 rules_proto effective_from effective_to retained_at"),
		from:    "rule_bundles AS b", where: "b.id=? AND b.organisation_id IS NULL", args: []any{lowValueRuleID}, orderBy: "b.id",
	}
}

func postingReportingJournalFactQueries(organisationID string, journalIDs []string) []postingReportingQuery {
	queries := make([]postingReportingQuery, 0, 2*((len(journalIDs)+499)/500))
	chunks := make([][]string, 0, (len(journalIDs)+499)/500)
	for start := 0; start < len(journalIDs); start += 500 {
		end := start + 500
		if end > len(journalIDs) {
			end = len(journalIDs)
		}
		chunks = append(chunks, append([]string(nil), journalIDs[start:end]...))
	}
	for _, batch := range chunks {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+2)
		args = append(args, organisationID, organisationID)
		for _, id := range batch {
			args = append(args, id)
		}
		queries = append(queries, postingReportingQuery{
			table: "tax_facts", alias: "f",
			columns: strings.Fields("id organisation_id journal_line_id tax_code treatment original_gross_minor original_net_minor original_gst_minor attributed_gross_minor attributed_net_minor attributed_gst_minor remaining_gross_minor remaining_net_minor remaining_gst_minor tax_rule_type tax_rule_id tax_rule_revision tax_rule_content_hash source_type source_id source_revision source_content_hash created_at"),
			from:    "tax_facts AS f JOIN journal_lines AS l ON l.id=f.journal_line_id JOIN journals AS j ON j.id=l.journal_id",
			where:   "f.organisation_id=? AND j.organisation_id=? AND j.id IN (" + placeholders + ")", args: args,
			orderBy: "j.id,l.line_number,f.id",
		})
	}
	for _, batch := range chunks {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+2)
		args = append(args, organisationID, organisationID)
		for _, id := range batch {
			args = append(args, id)
		}
		queries = append(queries, postingReportingQuery{
			table: "cash_flow_facts", alias: "f",
			columns: strings.Fields("id organisation_id journal_line_id sequence category amount_minor source_revision created_at"),
			from:    "cash_flow_facts AS f JOIN journal_lines AS l ON l.id=f.journal_line_id JOIN journals AS j ON j.id=l.journal_id",
			where:   "f.organisation_id=? AND j.organisation_id=? AND j.id IN (" + placeholders + ")", args: args,
			orderBy: "j.id,l.line_number,f.sequence,f.id",
		})
	}
	return queries
}

func postingReportingControlAccountQueries(organisationID string, required bool) []postingReportingQuery {
	if !required {
		return nil
	}
	// Existing journal replay resolves these four organisation-owned control
	// accounts through AccountRepository.Get, so retain and preflight them before
	// invoking that hydration path.
	return []postingReportingQuery{{
		table: "accounts", alias: "a",
		columns: strings.Fields("id organisation_id code name account_type subtype normal_balance status designation default_tax_code_id report_classification cash_flow_classification owner_module version created_at updated_at"),
		from:    "accounts AS a", where: "a.organisation_id=? AND a.code IN ('2000','1150','1160','1170')",
		args: []any{organisationID}, orderBy: "a.code,a.id",
	}}
}

func postingReportingCount(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) (int64, error) {
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, errors.Join(ErrRepository, err, ctx.Err())
	}
	var count int64
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
		return 0, errors.Join(ErrRepository, rows.Err(), rows.Close(), ctx.Err())
	}
	if err := rows.Close(); err != nil {
		return 0, errors.Join(ErrRepository, err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return 0, errors.Join(ErrRepository, err)
	}
	return count, nil
}

func preflightPostingReportingQuery(
	ctx context.Context,
	executor interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	query postingReportingQuery,
	remaining *int64,
	limit int64,
) (int64, int64, error) {
	if remaining == nil || *remaining < 0 || len(query.columns) == 0 || query.from == "" || query.where == "" {
		return 0, 0, ErrRepository
	}
	size := postingReportingPayloadSizeSQL(query.alias, query.columns)
	sqlText := `SELECT count(*),total(payload_bytes) FROM (SELECT ` + size + ` AS payload_bytes FROM ` + query.from + ` WHERE ` + query.where + `)`
	rows, err := executor.QueryContext(ctx, sqlText, query.args...)
	if err != nil {
		return 0, 0, errors.Join(ErrRepository, err, ctx.Err())
	}
	var count int64
	var payload float64
	if !rows.Next() || rows.Scan(&count, &payload) != nil || rows.Next() || rows.Err() != nil {
		return 0, 0, errors.Join(ErrRepository, rows.Err(), rows.Close(), ctx.Err())
	}
	if err := rows.Close(); err != nil {
		return 0, 0, errors.Join(ErrRepository, err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, errors.Join(ErrRepository, err)
	}
	if count < 0 || payload < 0 || math.IsNaN(payload) || math.IsInf(payload, 0) {
		return 0, 0, ErrRepository
	}
	if payload > float64(*remaining) {
		return 0, 0, &SourceBoundError{Resource: "capture bytes", Limit: limit}
	}
	intPayload := int64(payload)
	if float64(intPayload) != payload {
		return 0, 0, ErrRepository
	}
	*remaining -= intPayload
	return intPayload, count, nil
}

func postingReportingPayloadSizeSQL(alias string, columns []string) string {
	parts := []string{strconv.FormatInt(postingReportingRowOverhead, 10)}
	for _, column := range columns {
		parts = append(parts, "coalesce(length(CAST("+alias+"."+column+" AS BLOB)),0)")
	}
	return strings.Join(parts, "+")
}

func loadPostingReportingQuery(
	ctx context.Context,
	executor interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	query postingReportingQuery,
) (postingReportingRowsResult, error) {
	if len(query.columns) == 0 || query.from == "" || query.where == "" || query.orderBy == "" {
		return postingReportingRowsResult{}, ErrRepository
	}
	selects := make([]string, len(query.columns))
	for i, column := range query.columns {
		selects[i] = query.alias + "." + column
	}
	sqlText := "SELECT " + strings.Join(selects, ",") + " FROM " + query.from + " WHERE " + query.where + " ORDER BY " + query.orderBy + query.limit
	rows, err := executor.QueryContext(ctx, sqlText, query.args...)
	if err != nil {
		return postingReportingRowsResult{}, errors.Join(ErrRepository, err, ctx.Err())
	}
	result := postingReportingRowsResult{rows: make([]PurchaseReportingRetainedRow, 0, 8)}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			_ = rows.Close()
			return postingReportingRowsResult{}, errors.Join(ErrRepository, err)
		}
		destinations := make([]any, len(query.columns))
		for i := range destinations {
			destinations[i] = new(any)
		}
		if err := rows.Scan(destinations...); err != nil {
			_ = rows.Close()
			return postingReportingRowsResult{}, errors.Join(ErrRepository, err, ctx.Err())
		}
		row := PurchaseReportingRetainedRow{Table: query.table, Columns: append([]string(nil), query.columns...), Values: make([]PurchaseReportingRetainedValue, len(query.columns))}
		rowBytes := postingReportingRowOverhead
		for index := range destinations {
			raw := *(destinations[index].(*any))
			value, byteCount, err := postingReportingValue(raw)
			if err != nil {
				_ = rows.Close()
				return postingReportingRowsResult{}, errors.Join(ErrRepository, err)
			}
			row.Values[index] = value
			var ok bool
			rowBytes, ok = addPostingReportingBytes(rowBytes, byteCount)
			if !ok {
				_ = rows.Close()
				return postingReportingRowsResult{}, &SourceBoundError{Resource: "capture bytes", Limit: maximumPostingReportingPayloadSize}
			}
		}
		var ok bool
		result.payloadSize, ok = addPostingReportingBytes(result.payloadSize, rowBytes)
		if !ok {
			_ = rows.Close()
			return postingReportingRowsResult{}, &SourceBoundError{Resource: "capture bytes", Limit: maximumPostingReportingPayloadSize}
		}
		result.rows = append(result.rows, row)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return postingReportingRowsResult{}, errors.Join(ErrRepository, err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return postingReportingRowsResult{}, errors.Join(ErrRepository, err)
	}
	return result, nil
}

func postingReportingValue(raw any) (PurchaseReportingRetainedValue, int64, error) {
	switch value := raw.(type) {
	case nil:
		return PurchaseReportingRetainedValue{Kind: "NULL"}, 0, nil
	case int64:
		return PurchaseReportingRetainedValue{Kind: "INTEGER", Integer: value}, int64(len(strconv.FormatInt(value, 10))), nil
	case string:
		return PurchaseReportingRetainedValue{Kind: "TEXT", Text: value}, int64(len(value)), nil
	case []byte:
		return PurchaseReportingRetainedValue{Kind: "BLOB", Bytes: bytes.Clone(value)}, int64(len(value)), nil
	default:
		return PurchaseReportingRetainedValue{}, 0, fmt.Errorf("unsupported retained SQL value type %T", raw)
	}
}

func discoverPostingReportingReferences(
	ctx context.Context,
	rows []PurchaseReportingRetainedRow,
) ([]string, []string, bool, error) {
	documents := make(map[string]struct{})
	journals := make(map[string]struct{})
	needsExceptionRule := false
	addDocument := func(id string) {
		if id != "" {
			documents[id] = struct{}{}
		}
	}
	addReview := func(review *tammyv1.PurchaseEvidenceInput) {
		if review == nil {
			return
		}
		if review.Source != nil {
			addDocument(review.Source.SourceId)
		}
		if review.Classification == tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION {
			needsExceptionRule = true
		}
		if review.ExceptionReview != nil {
			for _, source := range review.ExceptionReview.SupportingRecords {
				if source != nil {
					addDocument(source.SourceId)
				}
			}
		}
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		switch row.Table {
		case "purchase_postings_v1":
			addDocument(postingReportingString(row, "document_id"))
			content, ok := postingReportingBytes(row, "posting_proto")
			if !ok {
				return nil, nil, false, ErrRepository
			}
			posting := &tammyv1.PurchasePosting{}
			if proto.Unmarshal(content, posting) != nil {
				return nil, nil, false, ErrRepository
			}
			addReview(posting.Evidence)
			if journal := postingReportingNullableString(row, "journal_id"); journal != "" {
				journals[journal] = struct{}{}
			}
		case "purchase_payments_v1":
			if journal := postingReportingString(row, "journal_id"); journal != "" {
				journals[journal] = struct{}{}
			}
		case "purchase_tax_evidence_v1":
			if source := postingReportingNullableString(row, "source_document_id"); source != "" {
				addDocument(source)
			}
			content, ok := postingReportingBytes(row, "evidence_proto")
			if !ok {
				return nil, nil, false, ErrRepository
			}
			evidence := &tammyv1.PurchaseTaxEvidence{}
			if proto.Unmarshal(content, evidence) != nil {
				return nil, nil, false, ErrRepository
			}
			addReview(evidence.Review)
		case "purchase_accounting_events_v1":
			if journal := postingReportingNullableString(row, "journal_id"); journal != "" {
				journals[journal] = struct{}{}
			}
		case "purchase_gst_attributions_v1":
			if dueDateSource := postingReportingNullableString(row, "due_date_source_id"); dueDateSource != "" {
				addDocument(dueDateSource)
			}
			if journal := postingReportingString(row, "journal_id"); journal != "" {
				journals[journal] = struct{}{}
			}
			content, ok := postingReportingBytes(row, "attribution_proto")
			if !ok {
				return nil, nil, false, ErrRepository
			}
			attribution := &tammyv1.PurchaseGstAttribution{}
			if proto.Unmarshal(content, attribution) != nil {
				return nil, nil, false, ErrRepository
			}
			addPostingReportingPortionDocument(attribution.Portion, addDocument)
		case "purchase_gst_deferrals_v1":
			content, ok := postingReportingBytes(row, "deferral_proto")
			if !ok {
				return nil, nil, false, ErrRepository
			}
			deferral := &tammyv1.PurchaseGstDeferral{}
			if proto.Unmarshal(content, deferral) != nil {
				return nil, nil, false, ErrRepository
			}
			for _, portion := range deferral.Portions {
				addPostingReportingPortionDocument(portion, addDocument)
			}
		case "purchase_gst_deferral_portions_v1":
			if dueDateSource := postingReportingString(row, "due_date_source_id"); dueDateSource != "" {
				addDocument(dueDateSource)
			}
		}
	}
	documentIDs := make([]string, 0, len(documents))
	for id := range documents {
		documentIDs = append(documentIDs, id)
	}
	sort.Strings(documentIDs)
	journalIDs := make([]string, 0, len(journals))
	for id := range journals {
		if !ids.IsCanonicalV7(id) {
			return nil, nil, false, ErrRepository
		}
		journalIDs = append(journalIDs, id)
	}
	sort.Strings(journalIDs)
	return documentIDs, journalIDs, needsExceptionRule, nil
}

func addPostingReportingPortionDocument(portion *tammyv1.PurchaseGstSourcePortion, add func(string)) {
	if portion != nil && portion.DueDateEvidence != nil {
		add(portion.DueDateEvidence.SourceId)
	}
}

func postingReportingRootDocumentIDs(rows []PurchaseReportingRetainedRow) []string {
	ids := make([]string, 0, maximumPostingReportingRoots)
	for _, row := range rows {
		if row.Table == "purchase_postings_v1" {
			ids = append(ids, postingReportingString(row, "document_id"))
		}
	}
	return ids
}

func postingReportingString(row PurchaseReportingRetainedRow, column string) string {
	for index, name := range row.Columns {
		if name == column && index < len(row.Values) && row.Values[index].Kind == "TEXT" {
			return row.Values[index].Text
		}
	}
	return ""
}

func postingReportingNullableString(row PurchaseReportingRetainedRow, column string) string {
	return postingReportingString(row, column)
}

func postingReportingBytes(row PurchaseReportingRetainedRow, column string) ([]byte, bool) {
	for index, name := range row.Columns {
		if name == column && index < len(row.Values) && row.Values[index].Kind == "BLOB" {
			return row.Values[index].Bytes, true
		}
	}
	return nil, false
}

func loadPostingReportingJournals(
	ctx context.Context,
	executor app.CommandSQLExecutor,
	organisationID string,
	journalIDs []string,
	remaining int64,
	limit int64,
) ([]*accounting.JournalReportingRecord, int64, error) {
	repository, err := accounting.NewJournalRepository(executor)
	if err != nil {
		return nil, 0, errors.Join(ErrRepository, err)
	}
	result := make([]*accounting.JournalReportingRecord, 0, len(journalIDs))
	var used int64
	for _, id := range journalIDs {
		if err := ctx.Err(); err != nil {
			return nil, 0, errors.Join(ErrRepository, err)
		}
		proof, err := repository.GetForReportingWithPayloadBudget(ctx, organisationID, id, remaining)
		if err != nil {
			var bound *accounting.TaxFactReportingBoundError
			if errors.As(err, &bound) {
				if bound.Resource == "capture bytes" {
					return nil, 0, errors.Join(&SourceBoundError{Resource: "capture bytes", Limit: limit}, err, ctx.Err())
				}
				return nil, 0, errors.Join(&SourceBoundError{Resource: bound.Resource, Limit: int64(bound.Limit)}, err, ctx.Err())
			}
			return nil, 0, errors.Join(ErrRepository, err, ctx.Err())
		}
		if proof == nil || proof.Journal == nil || proof.Journal.OrganisationId != organisationID || proof.Journal.Id != id || len(proof.ContentHash) != sha256.Size || proof.PayloadBytes < 0 || proof.PayloadBytes > remaining {
			return nil, 0, ErrRepository
		}
		var ok bool
		used, ok = addPostingReportingBytes(used, proof.PayloadBytes)
		if !ok {
			return nil, 0, &SourceBoundError{Resource: "capture bytes", Limit: limit}
		}
		remaining -= proof.PayloadBytes
		result = append(result, proof)
	}
	return result, used, nil
}

func postingReportingFingerprint(
	ctx context.Context,
	organisationID string,
	rows []PurchaseReportingRetainedRow,
	journals []*accounting.JournalReportingRecord,
) ([]byte, error) {
	digest := sha256.New()
	if err := postingReportingHashFrame(ctx, digest, []byte(postingReportingFingerprintTag)); err != nil {
		return nil, err
	}
	if err := postingReportingHashFrame(ctx, digest, []byte(organisationID)); err != nil {
		return nil, err
	}
	if err := postingReportingHashInt64(ctx, digest, int64(len(rows))); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := postingReportingHashFrame(ctx, digest, []byte(row.Table)); err != nil {
			return nil, err
		}
		if err := postingReportingHashInt64(ctx, digest, int64(len(row.Columns))); err != nil {
			return nil, err
		}
		if len(row.Columns) != len(row.Values) {
			return nil, ErrRepository
		}
		for index, column := range row.Columns {
			if err := postingReportingHashFrame(ctx, digest, []byte(column)); err != nil {
				return nil, err
			}
			value := row.Values[index]
			if err := postingReportingHashFrame(ctx, digest, []byte(value.Kind)); err != nil {
				return nil, err
			}
			switch value.Kind {
			case "NULL":
			case "INTEGER":
				if err := postingReportingHashInt64(ctx, digest, value.Integer); err != nil {
					return nil, err
				}
			case "TEXT":
				if err := postingReportingHashFrame(ctx, digest, []byte(value.Text)); err != nil {
					return nil, err
				}
			case "BLOB":
				if err := postingReportingHashFrame(ctx, digest, value.Bytes); err != nil {
					return nil, err
				}
			default:
				return nil, ErrRepository
			}
		}
	}
	if err := postingReportingHashInt64(ctx, digest, int64(len(journals))); err != nil {
		return nil, err
	}
	for _, journal := range journals {
		if journal == nil || journal.Journal == nil || len(journal.ContentHash) != sha256.Size {
			return nil, ErrRepository
		}
		if err := postingReportingHashFrame(ctx, digest, []byte(journal.Journal.Id)); err != nil {
			return nil, err
		}
		if err := postingReportingHashFrame(ctx, digest, journal.ContentHash); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
}

func postingReportingHashFrame(ctx context.Context, digest hash.Hash, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(value)
	return ctx.Err()
}

func postingReportingHashInt64(ctx context.Context, digest hash.Hash, value int64) error {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	return postingReportingHashFrame(ctx, digest, encoded[:])
}

func addPostingReportingBytes(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || right > math.MaxInt64-left {
		return 0, false
	}
	return left + right, true
}

func postingReportingReadError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx == nil || ctx.Err() == nil {
		return err
	}
	return errors.Join(err, ctx.Err())
}
