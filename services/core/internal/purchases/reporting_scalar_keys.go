package purchases

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const maximumBASScalarKeys = maximumPostingReportingPayloadSize / postingReportingRowOverhead

type BASScalarKeyKind int64

const (
	BASScalarPosting BASScalarKeyKind = iota + 1
	BASScalarPostingAllocation
	BASScalarPayment
	BASScalarPaymentAllocation
	BASScalarEvidence
	BASScalarEvent
	BASScalarDeferral
	BASScalarDeferralPortion
	BASScalarAttribution
	BASScalarDocument
	BASScalarLegacyReview
	BASScalarOpeningPayable
	BASScalarOpeningConversion
	BASScalarOpeningItem
	BASScalarManualJournal
	BASScalarFamilyCount = 15
)

// BASScalarSummary reserves physical identities, not semantic coverage or
// complete body proofs. FamilyCounts uses Kind-1 indexes. ScalarBytes charges
// selected TEXT/BLOB cells plus 512 bytes per row; it is not Go heap size.
type BASScalarSummary struct {
	OrganisationID                                       string
	RootCount, ReceiptCount, ScalarKeyCount, ScalarBytes int64
	FamilyCounts                                         [BASScalarFamilyCount]int64
}

// BASScalarKey preserves one exact opaque SQL identity. ID is the own id,
// operation_key for events, or the first composite primary-key column for
// posting/payment allocations and deferral portions; Sequence is the second.
// Applicable references and hashes remain independently nullable. RecordHash
// is a retained record/result proof, never replacement source provenance.
// ObjectVersion belongs to the document/conversion/journal; account versions
// and source versions retain their distinct snapshot/reference meaning.
// Conversion SourceHash retains its raw 64-byte hex TEXT; other hashes retain
// exact BLOB bytes. Neither these keys nor their flags validate domain IDs,
// complete bodies, GST eligibility, journal-line mappings or duplicate proofs.
type BASScalarKey struct {
	Kind                                                                                     BASScalarKeyKind
	OrganisationID, ID                                                                       string
	PostingID, DocumentID, OpeningPayableID, ConversionID, PaymentID                         *string
	AccountID, JournalID, EvidenceID, DeferralID, OperationKey, RelatedID                    *string
	ScopeKind, ScopeID, SourceKind, SourceType, SourceID                                     *string
	ActualPaymentDate, DueDateSourceID, ClaimEvidenceSourceID                                *string
	Sequence, PostingAllocationSequence, AllocationSequence                                  *uint64
	ObjectVersion, AccountingVersion, AccountVersion, SourceVersion                          *uint64
	DueDateSourceVersion, ClaimEvidenceSourceVersion, JournalSourceKind                      *uint64
	SourceHash, RecordHash, ReviewedSnapshotHash, DueDateSourceHash, ClaimEvidenceSourceHash []byte
	IsRoot, IsReceipt                                                                        bool
}

type purchaseBASScalarFamily struct {
	kind                                BASScalarKeyKind
	columns                             map[string]string
	required                            []string
	from, where, invalid, root, receipt string
}

var purchaseBASScalarStringColumns = []string{
	"organisation_id", "id", "posting_id", "document_id", "opening_payable_id", "conversion_id", "payment_id",
	"account_id", "journal_id", "evidence_id", "deferral_id", "operation_key", "related_id", "scope_kind", "scope_id",
	"source_kind", "source_type", "source_id", "due_date_source_id", "claim_evidence_source_id",
	"actual_payment_date",
}

var purchaseBASScalarIntegerColumns = []string{
	"sequence", "posting_allocation_sequence", "allocation_sequence", "object_version", "accounting_version", "account_version",
	"source_version", "due_date_source_version", "claim_evidence_source_version", "journal_source_kind",
}

var purchaseBASScalarHashColumns = []string{
	"source_hash", "record_hash", "reviewed_snapshot_hash", "due_date_source_hash", "claim_evidence_source_hash",
}

const purchaseBASScalarOrderSQL = `key_kind,id,sequence`

func purchaseBASScalarCTE() string {
	families := append(purchaseBASModernFamilies(), purchaseBASLegacyFamilies()...)
	// An unresolved link has no physical parent row to project. Keep a finite
	// invalid marker even when the valid family set is empty; it never returns
	// as a public key and contains none of the damaged retained cells.
	families = append(families, purchaseBASScalarFamily{
		columns: map[string]string{"organisation_id": "target.organisation_id", "id": "'unresolved purchase source link'"},
		from:    "purchase_bas_invalid_links AS broken", where: "broken.invalid_count>0", invalid: "1",
	})
	parts := make([]string, 0, len(families))
	for _, family := range families {
		expression := func(name string) string {
			if value := family.columns[name]; value != "" {
				return value
			}
			return "NULL"
		}
		boolean := func(value string) string {
			if value == "" {
				return "0"
			}
			return value
		}
		selected := []string{fmt.Sprintf("%d AS key_kind", family.kind), "(" + boolean(family.root) + ") AS is_root", "(" + boolean(family.receipt) + ") AS is_receipt"}
		invalid := []string{"(" + boolean(family.invalid) + ")"}
		charge := []string{"512"}
		for _, name := range purchaseBASScalarStringColumns {
			value := expression(name)
			selected = append(selected, value+" AS "+name)
			if value != "NULL" {
				invalid = append(invalid, "("+value+" IS NOT NULL AND (typeof("+value+")!='text' OR length(CAST("+value+" AS BLOB))=0))")
				charge = append(charge, "coalesce(length(CAST("+value+" AS BLOB)),0)")
			}
		}
		for _, name := range purchaseBASScalarIntegerColumns {
			value := expression(name)
			selected = append(selected, value+" AS "+name)
			if value != "NULL" {
				minimum := "1"
				if name == "journal_source_kind" {
					minimum = "0"
				}
				invalid = append(invalid, "("+value+" IS NOT NULL AND (typeof("+value+")!='integer' OR "+value+"<"+minimum+"))")
			}
		}
		for _, name := range purchaseBASScalarHashColumns {
			value := expression(name)
			selected = append(selected, value+" AS "+name)
			if value != "NULL" {
				shape := "typeof(" + value + ")!='blob' OR length(" + value + ")!=32"
				if family.kind == BASScalarOpeningConversion && name == "source_hash" {
					shape = "typeof(" + value + ")!='text' OR length(CAST(" + value + " AS BLOB))!=64 OR " + value + "!=lower(" + value + ") OR " + value + " GLOB '*[^0-9a-f]*'"
				}
				invalid = append(invalid, "("+value+" IS NOT NULL AND ("+shape+"))")
				charge = append(charge, "coalesce(length(CAST("+value+" AS BLOB)),0)")
			}
		}
		for _, name := range append([]string{"organisation_id", "id"}, family.required...) {
			invalid = append(invalid, expression(name)+" IS NULL")
		}
		selected = append(selected, strings.Join(charge, "+")+" AS scalar_bytes", "CASE WHEN ("+strings.Join(invalid, " OR ")+") THEN 1 ELSE 0 END AS invalid_scalar")
		parts = append(parts, "SELECT "+strings.Join(selected, ",")+" FROM "+family.from+" CROSS JOIN requested AS target WHERE ("+family.where+")")
	}
	contextSQL := strings.TrimSuffix(strings.TrimSpace(purchaseBASScalarContextSQL), ",")
	if contextSQL != "" {
		contextSQL += ","
	}
	return `WITH RECURSIVE requested AS (SELECT ? AS organisation_id), ` + contextSQL + ` scalar_rows AS (` + strings.Join(parts, " UNION ALL ") + `)`
}

// SummarizeBASKeys returns only aggregate reservations. It neither copies
// typed parent identities into Go nor reads bodies or owns the transaction.
func (r *Repository) SummarizeBASKeys(ctx context.Context, organisationID string) (*BASScalarSummary, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	selects := []string{"coalesce(sum(is_root),0)", "coalesce(sum(is_receipt),0)", "count(*)", "total(scalar_bytes)", "coalesce(sum(invalid_scalar),0)"}
	for kind := 1; kind <= BASScalarFamilyCount; kind++ {
		selects = append(selects, fmt.Sprintf("coalesce(sum(key_kind=%d),0)", kind))
	}
	rows, err := r.executor.QueryContext(ctx, `/* BAS purchase scalar summary */ `+purchaseBASScalarCTE()+`, bounded_rows AS (SELECT * FROM scalar_rows ORDER BY `+purchaseBASScalarOrderSQL+` LIMIT ?) SELECT `+strings.Join(selects, ",")+` FROM bounded_rows`, organisationID, maximumBASScalarKeys+1)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	if !rows.Next() {
		return nil, errors.Join(ErrRepository, sql.ErrNoRows, rows.Err(), rows.Close())
	}
	summary := BASScalarSummary{OrganisationID: organisationID}
	var scalarBytes float64
	var invalid int64
	targets := []any{&summary.RootCount, &summary.ReceiptCount, &summary.ScalarKeyCount, &scalarBytes, &invalid}
	for index := range summary.FamilyCounts {
		targets = append(targets, &summary.FamilyCounts[index])
	}
	if err := rows.Scan(targets...); err != nil {
		return nil, errors.Join(ErrRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrRepository, errors.New("purchase scalar summary returned multiple totals"), rows.Close())
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
	}{
		{summary.RootCount, maximumPostingReportingRoots, "purchase roots"},
		{summary.ReceiptCount, maximumPostingReportingPayments, "receipt sources"},
		{summary.ScalarKeyCount, maximumBASScalarKeys, "BAS scalar keys"},
	} {
		if bound.count > bound.limit {
			return nil, &SourceBoundError{Resource: bound.resource, Limit: bound.limit}
		}
	}
	if scalarBytes < 0 || math.IsNaN(scalarBytes) || math.IsInf(scalarBytes, 0) || math.Trunc(scalarBytes) != scalarBytes {
		return nil, fmt.Errorf("%w: invalid purchase scalar byte total", ErrRepository)
	}
	if scalarBytes > float64(maximumPostingReportingPayloadSize) {
		return nil, &SourceBoundError{Resource: "BAS scalar bytes", Limit: maximumPostingReportingPayloadSize}
	}
	if invalid != 0 {
		return nil, fmt.Errorf("%w: purchase scalar keys contain invalid storage or parent links", ErrRepository)
	}
	summary.ScalarBytes = int64(scalarBytes)
	return &summary, nil
}

// ReadBASKeys verifies a prior reservation and guards every selected cell in
// SQL before the driver copies it. Future coordination must enforce all
// modules' shared fences first. The summary is not a content fingerprint.
func (r *Repository) ReadBASKeys(ctx context.Context, organisationID string, expected BASScalarSummary, maximumKeyCount, maximumScalarBytes int64) ([]BASScalarKey, error) {
	if r == nil || r.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || expected.OrganisationID != organisationID || maximumKeyCount < 0 || maximumScalarBytes < 0 {
		return nil, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	for _, value := range append([]int64{expected.RootCount, expected.ReceiptCount, expected.ScalarKeyCount, expected.ScalarBytes}, expected.FamilyCounts[:]...) {
		if value < 0 {
			return nil, ErrRepository
		}
	}
	if maximumKeyCount > maximumBASScalarKeys {
		maximumKeyCount = maximumBASScalarKeys
	}
	if maximumScalarBytes > maximumPostingReportingPayloadSize {
		maximumScalarBytes = maximumPostingReportingPayloadSize
	}
	if expected.ScalarKeyCount > maximumKeyCount {
		return nil, &SourceBoundError{Resource: "BAS scalar keys", Limit: maximumKeyCount}
	}
	if expected.ScalarBytes > maximumScalarBytes {
		return nil, &SourceBoundError{Resource: "BAS scalar bytes", Limit: maximumScalarBytes}
	}
	actual, err := r.SummarizeBASKeys(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if *actual != expected {
		return nil, fmt.Errorf("%w: purchase scalar reservation changed before key read", ErrRepository)
	}
	selected := []string{"scalar_bytes", "invalid_scalar", "key_kind", "CASE WHEN admissible THEN is_root ELSE 0 END", "CASE WHEN admissible THEN is_receipt ELSE 0 END"}
	for _, names := range [][]string{purchaseBASScalarStringColumns, purchaseBASScalarIntegerColumns, purchaseBASScalarHashColumns} {
		for _, name := range names {
			selected = append(selected, "CASE WHEN admissible THEN "+name+" END AS "+name)
		}
	}
	// Order on source columns: withheld NULL output aliases must not move a
	// rejected row ahead of the permitted prefix or its count sentinel.
	rows, err := r.executor.QueryContext(ctx, `/* BAS purchase scalar keys */ `+purchaseBASScalarCTE()+`, bounded_rows AS (SELECT * FROM scalar_rows ORDER BY `+purchaseBASScalarOrderSQL+` LIMIT ?), guarded_rows AS (SELECT *, invalid_scalar=0 AND sum(scalar_bytes) OVER (ORDER BY `+purchaseBASScalarOrderSQL+` ROWS UNBOUNDED PRECEDING)<=? AND row_number() OVER (ORDER BY `+purchaseBASScalarOrderSQL+`)<=? AS admissible FROM bounded_rows) SELECT `+strings.Join(selected, ",")+` FROM guarded_rows ORDER BY guarded_rows.key_kind,guarded_rows.id,guarded_rows.sequence`, organisationID, expected.ScalarKeyCount+1, expected.ScalarBytes, expected.ScalarKeyCount)
	if err != nil {
		return nil, errors.Join(ErrRepository, err, ctx.Err())
	}
	keys := make([]BASScalarKey, 0)
	var charged, roots, receipts int64
	var counts [BASScalarFamilyCount]int64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrRepository, err, rows.Close())
		}
		if int64(len(keys)) == expected.ScalarKeyCount {
			return nil, errors.Join(&SourceBoundError{Resource: "BAS scalar keys", Limit: expected.ScalarKeyCount}, rows.Close())
		}
		key, rowBytes, err := scanPurchaseBASScalarKey(rows, expected.ScalarBytes-charged, expected.ScalarBytes)
		if err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		if key.OrganisationID != organisationID || len(keys) > 0 && !purchaseBASScalarKeyLess(keys[len(keys)-1], key) {
			return nil, errors.Join(fmt.Errorf("%w: purchase scalar ownership or order changed", ErrRepository), rows.Close())
		}
		charged += rowBytes
		counts[int(key.Kind)-1]++
		if key.IsRoot {
			roots++
		}
		if key.IsReceipt {
			receipts++
		}
		keys = append(keys, key)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if int64(len(keys)) != expected.ScalarKeyCount || charged != expected.ScalarBytes || counts != expected.FamilyCounts || roots != expected.RootCount || receipts != expected.ReceiptCount {
		return nil, fmt.Errorf("%w: purchase scalar counts or charge changed during key read", ErrRepository)
	}
	return keys, nil
}

func scanPurchaseBASScalarKey(rows *sql.Rows, remainingBytes, reservationBytes int64) (BASScalarKey, int64, error) {
	var key BASScalarKey
	var projectedBytes, invalid int64
	text := make([]sql.NullString, len(purchaseBASScalarStringColumns))
	numbers := make([]sql.NullInt64, len(purchaseBASScalarIntegerColumns))
	hashes := make([][]byte, len(purchaseBASScalarHashColumns))
	targets := []any{&projectedBytes, &invalid, &key.Kind, &key.IsRoot, &key.IsReceipt}
	for i := range text {
		targets = append(targets, &text[i])
	}
	for i := range numbers {
		targets = append(targets, &numbers[i])
	}
	for i := range hashes {
		targets = append(targets, &hashes[i])
	}
	if err := rows.Scan(targets...); err != nil {
		return key, 0, errors.Join(ErrRepository, err)
	}
	if projectedBytes > remainingBytes {
		return key, 0, &SourceBoundError{Resource: "BAS scalar bytes", Limit: reservationBytes}
	}
	if projectedBytes < postingReportingRowOverhead || invalid != 0 || key.Kind < BASScalarPosting || key.Kind > BASScalarManualJournal || !text[0].Valid || !text[1].Valid {
		return key, 0, fmt.Errorf("%w: invalid purchase scalar row", ErrRepository)
	}
	key.OrganisationID, key.ID = text[0].String, text[1].String
	stringTargets := []**string{&key.PostingID, &key.DocumentID, &key.OpeningPayableID, &key.ConversionID, &key.PaymentID, &key.AccountID, &key.JournalID, &key.EvidenceID, &key.DeferralID, &key.OperationKey, &key.RelatedID, &key.ScopeKind, &key.ScopeID, &key.SourceKind, &key.SourceType, &key.SourceID, &key.DueDateSourceID, &key.ClaimEvidenceSourceID, &key.ActualPaymentDate}
	for i, target := range stringTargets {
		if raw := text[i+2]; raw.Valid {
			value := raw.String
			*target = &value
		}
	}
	integerTargets := []**uint64{&key.Sequence, &key.PostingAllocationSequence, &key.AllocationSequence, &key.ObjectVersion, &key.AccountingVersion, &key.AccountVersion, &key.SourceVersion, &key.DueDateSourceVersion, &key.ClaimEvidenceSourceVersion, &key.JournalSourceKind}
	for i, target := range integerTargets {
		if raw := numbers[i]; raw.Valid {
			if raw.Int64 < 0 || i != len(numbers)-1 && raw.Int64 == 0 {
				return key, 0, ErrRepository
			}
			value := uint64(raw.Int64)
			*target = &value
		}
	}
	hashTargets := []*[]byte{&key.SourceHash, &key.RecordHash, &key.ReviewedSnapshotHash, &key.DueDateSourceHash, &key.ClaimEvidenceSourceHash}
	for i, target := range hashTargets {
		*target = bytes.Clone(hashes[i])
	}
	charged := postingReportingRowOverhead
	for _, value := range text {
		if value.Valid {
			var ok bool
			charged, ok = addPostingReportingBytes(charged, int64(len(value.String)))
			if !ok {
				return key, 0, ErrRepository
			}
		}
	}
	for _, value := range hashes {
		var ok bool
		charged, ok = addPostingReportingBytes(charged, int64(len(value)))
		if !ok {
			return key, 0, ErrRepository
		}
	}
	if charged != projectedBytes {
		return key, 0, fmt.Errorf("%w: purchase scalar row charge changed", ErrRepository)
	}
	return key, charged, nil
}

func purchaseBASScalarKeyLess(left, right BASScalarKey) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	sequence := func(value *uint64) uint64 {
		if value == nil {
			return 0
		}
		return *value
	}
	return sequence(left.Sequence) < sequence(right.Sequence)
}
