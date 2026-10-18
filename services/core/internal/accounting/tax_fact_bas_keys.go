package accounting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const maxTaxFactBASScalarKeys = maxTaxFactReportingCaptureBytes / 512

// TaxFactBASScalarSummary reserves only the candidate identity projection.
// Accounting facts are neither invoice/purchase roots nor receipts. Their
// physical key count is distinct from the coordinator's final coverage cap.
// ScalarBytes charges selected TEXT/BLOB values plus 512 bytes per row; it
// does not reserve the full fact payload or complete journal proof.
type TaxFactBASScalarSummary struct {
	OrganisationID              string
	RootCount, ReceiptCount     int64
	ScalarKeyCount, ScalarBytes int64
}

// TaxFactBASScalarKey preserves one independent fact candidate and its exact
// source provenance. SourceRevision/hash describe the source, not metadata
// belonging to the fact row. This projection does not infer fact ownership,
// GST direction or timing and is insufficient for duplicate-proof removal.
type TaxFactBASScalarKey struct {
	ID, OrganisationID, JournalLineID, JournalID string
	LineNumber                                   int64
	SourceType, SourceID                         string
	SourceRevision                               uint64
	SourceContentHashPresent                     bool
	SourceContentHash                            []byte
}

func taxFactBASScalarSizeSQL() string {
	return taxFactReportingPayloadSizeSQL(
		"fact.id", "fact.organisation_id", "fact.journal_line_id", "line.journal_id",
		"journal.organisation_id", "fact.source_type", "fact.source_id", "fact.source_content_hash",
	)
}

const taxFactBASScalarFromSQL = `
	FROM tax_facts AS fact
	LEFT JOIN organisations AS owner ON owner.id = fact.organisation_id
	LEFT JOIN journal_lines AS line ON line.id = fact.journal_line_id
	LEFT JOIN journals AS journal ON journal.id = line.journal_id
	CROSS JOIN (SELECT ? AS organisation_id) AS target
	WHERE (` + taxFactReportingOwnerScopeSQL + `)`

func taxFactBASScalarInvalidSQL() string {
	invalidStorage := taxFactReportingInvalidNumericStorageSQL(
		[]string{"fact.source_revision"}, []string{"line.line_number"})
	invalidKey := `CASE WHEN owner.id IS NULL OR typeof(fact.organisation_id)!='text'
	 OR line.id IS NULL OR journal.id IS NULL
	 OR journal.organisation_id IS NULL OR journal.organisation_id != fact.organisation_id
	 OR fact.id IS NULL OR fact.id = '' OR fact.journal_line_id = '' OR line.journal_id = ''
	 OR fact.source_revision < 1 OR line.line_number < 1 OR line.line_number > 4294967295
	 OR (fact.source_content_hash IS NOT NULL AND length(CAST(fact.source_content_hash AS BLOB)) != 32)
	 THEN 1 ELSE 0 END`
	return "(" + invalidStorage + " + " + invalidKey + ")"
}

// SummarizeTaxFactBASKeys reads aggregate totals only, without reading typed
// keys, parent IDs, facts or journal bodies into Go. It includes every fact
// regardless of source, state, date, treatment or amount. Missing/foreign
// parents and non-INTEGER scanned numeric cells fail before any key read.
// The executor and transaction lifecycle remain owned by the caller.
func (repository *JournalRepository) SummarizeTaxFactBASKeys(
	ctx context.Context, organisationID string,
) (*TaxFactBASScalarSummary, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	remaining := int64(maxTaxFactReportingCaptureBytes)
	count, charged, err := repository.reserveTaxFactReportingPayloadDetails(ctx, &remaining,
		"BAS scalar keys", maxTaxFactBASScalarKeys,
		`/* BAS tax fact scalar summary */
		 SELECT count(*), total(payload_bytes), coalesce(sum(invalid_numeric_storage), 0) FROM (
		 SELECT `+taxFactBASScalarSizeSQL()+` AS payload_bytes,
		 `+taxFactBASScalarInvalidSQL()+` AS invalid_numeric_storage
		 `+taxFactBASScalarFromSQL+`
		 ORDER BY fact.id LIMIT ?
		 )`, organisationID, maxTaxFactBASScalarKeys+1)
	if err != nil {
		return nil, err
	}
	return &TaxFactBASScalarSummary{
		OrganisationID: organisationID, ScalarKeyCount: int64(count), ScalarBytes: charged,
	}, nil
}

// ReadTaxFactBASKeys verifies a prior aggregate reservation and reads bounded
// scalar keys through the same caller executor. The coordinator must enforce
// shared root/receipt and payload fences before calling this method. Limits
// above the eight MiB / 512-byte physical ceiling are clamped. A changed
// count or byte charge, cancellation or any error returns no partial keys.
// The summary is a size reservation, not a content fingerprint.
func (repository *JournalRepository) ReadTaxFactBASKeys(
	ctx context.Context, organisationID string, expected TaxFactBASScalarSummary,
	maximumKeyCount, maximumScalarBytes int64,
) ([]TaxFactBASScalarKey, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		maximumKeyCount < 0 || maximumScalarBytes < 0 || expected.OrganisationID != organisationID ||
		expected.RootCount != 0 || expected.ReceiptCount != 0 || expected.ScalarKeyCount < 0 || expected.ScalarBytes < 0 {
		return nil, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if maximumKeyCount > maxTaxFactBASScalarKeys {
		maximumKeyCount = maxTaxFactBASScalarKeys
	}
	if maximumScalarBytes > maxTaxFactReportingCaptureBytes {
		maximumScalarBytes = maxTaxFactReportingCaptureBytes
	}
	if expected.ScalarKeyCount > maximumKeyCount {
		return nil, &TaxFactReportingBoundError{Resource: "BAS scalar keys", Limit: int(maximumKeyCount)}
	}
	if expected.ScalarBytes > maximumScalarBytes {
		return nil, &TaxFactReportingBoundError{Resource: "BAS scalar bytes", Limit: int(maximumScalarBytes)}
	}
	actual, err := repository.SummarizeTaxFactBASKeys(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if *actual != expected {
		return nil, fmt.Errorf("%w: BAS scalar reservation changed before key read", ErrJournalRepository)
	}
	// CASE guards run in SQLite before the driver copies a row. Even a cell
	// changed after the aggregate cannot enter Go or a conversion error until
	// its numeric storage, parents and scalar reservation have been checked.
	rows, err := repository.executor.QueryContext(ctx, `/* BAS tax fact scalar keys */
	 WITH scalar_rows AS (
	 SELECT `+taxFactBASScalarSizeSQL()+` AS scalar_bytes,
	 `+taxFactBASScalarInvalidSQL()+` AS invalid_scalar,
	 fact.id AS fact_id, fact.organisation_id AS fact_owner, fact.journal_line_id AS line_id,
	 line.journal_id AS parent_id, line.line_number AS line_number,
	 journal.organisation_id AS parent_owner, fact.source_type AS source_type,
	 fact.source_id AS source_id, fact.source_revision AS source_revision,
	 fact.source_content_hash AS source_hash
	 `+taxFactBASScalarFromSQL+`
	 ORDER BY fact.id LIMIT ?
	 )
	 SELECT scalar_bytes, invalid_scalar,
	 CASE WHEN admissible THEN fact_id END, CASE WHEN admissible THEN fact_owner END,
	 CASE WHEN admissible THEN line_id END, CASE WHEN admissible THEN parent_id END,
	 CASE WHEN admissible THEN line_number END, CASE WHEN admissible THEN parent_owner END,
	 CASE WHEN admissible THEN source_type END, CASE WHEN admissible THEN source_id END,
	 CASE WHEN admissible THEN source_revision END, CASE WHEN admissible THEN source_hash END
	 FROM (
	 SELECT *, invalid_scalar=0
	 AND sum(scalar_bytes) OVER (ORDER BY fact_id ROWS UNBOUNDED PRECEDING)<=?
	 AND row_number() OVER (ORDER BY fact_id)<=? AS admissible
	 FROM scalar_rows
	 )
	 ORDER BY fact_id`, organisationID, expected.ScalarKeyCount+1, expected.ScalarBytes, expected.ScalarKeyCount)
	if err != nil {
		return nil, taxFactReportingReadError("query BAS scalar keys", err)
	}
	keys := make([]TaxFactBASScalarKey, 0)
	var charged int64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrJournalRepository, err, rows.Close())
		}
		if int64(len(keys)) == expected.ScalarKeyCount {
			return nil, errors.Join(&TaxFactReportingBoundError{Resource: "BAS scalar keys", Limit: int(expected.ScalarKeyCount)}, rows.Close())
		}
		var key TaxFactBASScalarKey
		var id, organisation, lineID, parentID, parentOwner, sourceType, sourceID sql.NullString
		var lineNumber, revision sql.NullInt64
		var projectedBytes, invalidScalar int64
		var hash []byte
		if err := rows.Scan(&projectedBytes, &invalidScalar, &id, &organisation, &lineID, &parentID, &lineNumber,
			&parentOwner, &sourceType, &sourceID, &revision, &hash); err != nil {
			return nil, errors.Join(taxFactReportingReadError("scan BAS scalar key", err), rows.Close())
		}
		if projectedBytes > expected.ScalarBytes-charged {
			return nil, errors.Join(&TaxFactReportingBoundError{Resource: "BAS scalar bytes", Limit: int(expected.ScalarBytes)}, rows.Close())
		}
		if projectedBytes < 512 || invalidScalar != 0 || !id.Valid || !organisation.Valid || !lineID.Valid ||
			!sourceType.Valid || !sourceID.Valid || !revision.Valid {
			return nil, errors.Join(fmt.Errorf("%w: invalid BAS scalar key storage or parent", ErrJournalRepository), rows.Close())
		}
		key.ID, key.OrganisationID, key.JournalLineID = id.String, organisation.String, lineID.String
		key.SourceType, key.SourceID = sourceType.String, sourceID.String
		if key.ID == "" || key.OrganisationID != organisationID || key.JournalLineID == "" ||
			!parentID.Valid || parentID.String == "" || !parentOwner.Valid || parentOwner.String != organisationID ||
			!lineNumber.Valid || lineNumber.Int64 < 1 || lineNumber.Int64 > math.MaxUint32 || revision.Int64 < 1 ||
			(hash != nil && len(hash) != sha256.Size) || (len(keys) > 0 && keys[len(keys)-1].ID >= key.ID) {
			return nil, errors.Join(fmt.Errorf("%w: invalid BAS scalar key or parent", ErrJournalRepository), rows.Close())
		}
		key.JournalID, key.LineNumber, key.SourceRevision = parentID.String, lineNumber.Int64, uint64(revision.Int64)
		key.SourceContentHashPresent = hash != nil
		key.SourceContentHash = bytes.Clone(hash)
		rowBytes := taxFactReportingPayloadBytes(
			[]byte(key.ID), []byte(key.OrganisationID), []byte(key.JournalLineID), []byte(key.JournalID),
			[]byte(parentOwner.String), []byte(key.SourceType), []byte(key.SourceID), hash,
		)
		if rowBytes != projectedBytes {
			return nil, errors.Join(fmt.Errorf("%w: BAS scalar row byte charge changed during key read", ErrJournalRepository), rows.Close())
		}
		charged += rowBytes
		keys = append(keys, key)
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrJournalRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if int64(len(keys)) != expected.ScalarKeyCount || charged != expected.ScalarBytes {
		return nil, fmt.Errorf("%w: BAS scalar count or bytes changed during key read", ErrJournalRepository)
	}
	return keys, nil
}
