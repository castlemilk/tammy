package accounting

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

const (
	maxBASLegacyCashSources           = 1000
	maxBASLegacyCashJournalsPerSource = 1000
	maxBASLegacyCashClosurePairs      = maxBASLegacyCashSources * maxBASLegacyCashJournalsPerSource
	maxBASLegacyCashRows              = maxTaxFactReportingCaptureBytes / 512
	maxBASLegacyCashBytes             = maxTaxFactReportingCaptureBytes
	basLegacyCashClosureSentinel      = maxBASLegacyCashClosurePairs + 1
)

type BASLegacyCashQuery struct {
	OrganisationID string
	SourceIDs      []string
}

type BASLegacyCashSummary struct {
	OrganisationID                 string
	AssociationCount, JournalCount int64
	LineCount, RowCount, Bytes     int64
}

type BASLegacyCashAssociation struct {
	SourceID, JournalID, Kind    string
	TaxFactID, JournalLineID     *string
	FactSourceType               *string
	FactSourceRevision           *uint64
	FactSourceContentHashPresent bool
	FactSourceContentHash        []byte
}

type BASLegacyCashJournal struct {
	OrganisationID, ID          string
	Version, SourceRevision     uint64
	SourceKind                  int64
	SourceType, SourceID, State string
	JournalDate                 string
	ReversalOfID, ReversedByID  *string
}

type BASLegacyCashLine struct {
	JournalID, ID, AccountID string
	AccountOrganisationID    string
	Sequence, AccountVersion uint64
	DebitMinor, CreditMinor  int64
	ReportClassification     string
}

type BASLegacyCashRead struct {
	Summary      BASLegacyCashSummary
	Associations []BASLegacyCashAssociation
	Journals     []BASLegacyCashJournal
	Lines        []BASLegacyCashLine
}

func (repository *JournalRepository) SummarizeBASLegacyCash(
	ctx context.Context, query BASLegacyCashQuery,
) (*BASLegacyCashSummary, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !validBASLegacyCashQuery(query) {
		return nil, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	closureCount, maximumPerSource, err := repository.summarizeBASLegacyCashClosure(ctx, query)
	if err != nil {
		return nil, err
	}
	if closureCount > maxBASLegacyCashClosurePairs || maximumPerSource > maxBASLegacyCashJournalsPerSource {
		return nil, &TaxFactReportingBoundError{Resource: "BAS legacy cash reversal closure", Limit: maxBASLegacyCashJournalsPerSource}
	}

	prefix, args := basLegacyCashPrefix(query)
	querySQL := prefix + `,
	association_rows AS (
	 SELECT 'A' AS row_kind, source_id AS sort1, journal_id AS sort2, association_kind AS sort3,
	        coalesce(tax_fact_id,'') AS sort4, 0 AS sort_number, payload_bytes, invalid
	 FROM bas_legacy_cash_associations ORDER BY source_id,journal_id,association_kind,coalesce(tax_fact_id,'')
	 LIMIT ?
	),
	journal_rows AS (
	 SELECT 'J' AS row_kind, id AS sort1, '' AS sort2, '' AS sort3, '' AS sort4, 0 AS sort_number,
	        payload_bytes, invalid
	 FROM bas_legacy_cash_journals ORDER BY id LIMIT ?
	),
	line_rows AS (
	 SELECT 'L' AS row_kind, journal_id AS sort1, id AS sort2, '' AS sort3, '' AS sort4, sequence AS sort_number,
	        payload_bytes, invalid
	 FROM bas_legacy_cash_lines ORDER BY journal_id,sequence,id LIMIT ?
	),
	all_rows AS (
	 SELECT * FROM association_rows UNION ALL SELECT * FROM journal_rows UNION ALL SELECT * FROM line_rows
	),
	bounded_rows AS (
	 SELECT * FROM all_rows ORDER BY row_kind,sort1,sort2,sort3,sort4,sort_number LIMIT ?
	)
	SELECT count(*), total(payload_bytes), coalesce(sum(invalid),0),
	       coalesce(sum(CASE WHEN row_kind='A' THEN 1 ELSE 0 END),0),
	       coalesce(sum(CASE WHEN row_kind='J' THEN 1 ELSE 0 END),0),
	       coalesce(sum(CASE WHEN row_kind='L' THEN 1 ELSE 0 END),0)
	FROM bounded_rows`
	args = append(args, maxBASLegacyCashRows+1, maxBASLegacyCashRows+1, maxBASLegacyCashRows+1, maxBASLegacyCashRows+1)
	rows, err := repository.executor.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, taxFactReportingReadError("summarize BAS legacy cash", err)
	}
	if !rows.Next() {
		return nil, errors.Join(ErrJournalRepository, sql.ErrNoRows, rows.Err(), rows.Close())
	}
	var rowCount, invalid, associationCount, journalCount, lineCount int64
	var totalBytes float64
	if err := rows.Scan(&rowCount, &totalBytes, &invalid, &associationCount, &journalCount, &lineCount); err != nil {
		return nil, errors.Join(taxFactReportingReadError("scan BAS legacy cash summary", err), rows.Close())
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrJournalRepository, rowsErr, closeErr)
	}
	if rowCount > maxBASLegacyCashRows {
		return nil, &TaxFactReportingBoundError{Resource: "BAS legacy cash rows", Limit: maxBASLegacyCashRows}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if invalid != 0 || totalBytes < 0 || math.IsNaN(totalBytes) || math.IsInf(totalBytes, 0) || math.Trunc(totalBytes) != totalBytes {
		return nil, fmt.Errorf("%w: invalid BAS legacy cash scalar storage", ErrJournalRepository)
	}
	if totalBytes > float64(maxBASLegacyCashBytes) {
		return nil, &TaxFactReportingBoundError{Resource: "BAS legacy cash bytes", Limit: maxBASLegacyCashBytes}
	}
	if rowCount != associationCount+journalCount+lineCount || int64(totalBytes) < rowCount*512 {
		return nil, fmt.Errorf("%w: inconsistent BAS legacy cash reservation", ErrJournalRepository)
	}
	return &BASLegacyCashSummary{
		OrganisationID: query.OrganisationID, AssociationCount: associationCount, JournalCount: journalCount,
		LineCount: lineCount, RowCount: rowCount, Bytes: int64(totalBytes),
	}, nil
}

func (repository *JournalRepository) ReadBASLegacyCash(
	ctx context.Context, query BASLegacyCashQuery, expected BASLegacyCashSummary,
	maximumRows, maximumBytes int64,
) (*BASLegacyCashRead, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !validBASLegacyCashQuery(query) ||
		expected.OrganisationID != query.OrganisationID || expected.AssociationCount < 0 || expected.JournalCount < 0 ||
		expected.LineCount < 0 || expected.RowCount < 0 || expected.Bytes < 0 || maximumRows < 0 || maximumBytes < 0 ||
		expected.AssociationCount > maxBASLegacyCashRows || expected.JournalCount > maxBASLegacyCashRows ||
		expected.LineCount > maxBASLegacyCashRows || expected.RowCount > maxBASLegacyCashRows || expected.Bytes > maxBASLegacyCashBytes ||
		expected.RowCount != expected.AssociationCount+expected.JournalCount+expected.LineCount || expected.Bytes < expected.RowCount*512 {
		return nil, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if maximumRows > maxBASLegacyCashRows {
		maximumRows = maxBASLegacyCashRows
	}
	if maximumBytes > maxBASLegacyCashBytes {
		maximumBytes = maxBASLegacyCashBytes
	}
	if expected.RowCount > maximumRows {
		return nil, &TaxFactReportingBoundError{Resource: "BAS legacy cash rows", Limit: int(maximumRows)}
	}
	if expected.Bytes > maximumBytes {
		return nil, &TaxFactReportingBoundError{Resource: "BAS legacy cash bytes", Limit: int(maximumBytes)}
	}
	before, err := repository.SummarizeBASLegacyCash(ctx, query)
	if err != nil {
		return nil, err
	}
	if *before != expected {
		return nil, fmt.Errorf("%w: BAS legacy cash reservation changed before read", ErrJournalRepository)
	}
	read, err := repository.readBASLegacyCashRows(ctx, query, expected)
	if err != nil {
		return nil, err
	}
	after, err := repository.SummarizeBASLegacyCash(ctx, query)
	if err != nil {
		return nil, err
	}
	if *after != expected || read.Summary != expected {
		return nil, fmt.Errorf("%w: BAS legacy cash projection changed during read", ErrJournalRepository)
	}
	if err := validateBASLegacyCashRead(query, read); err != nil {
		return nil, err
	}
	return read, nil
}

func validBASLegacyCashQuery(query BASLegacyCashQuery) bool {
	if !ids.IsCanonicalV7(query.OrganisationID) || len(query.SourceIDs) > maxBASLegacyCashSources {
		return false
	}
	previous := ""
	for _, sourceID := range query.SourceIDs {
		if !ids.IsCanonicalV7(sourceID) || previous != "" && sourceID <= previous {
			return false
		}
		previous = sourceID
	}
	return true
}

func (repository *JournalRepository) summarizeBASLegacyCashClosure(
	ctx context.Context, query BASLegacyCashQuery,
) (total, maximumPerSource int64, err error) {
	prefix, args := basLegacyCashPrefix(query)
	rows, err := repository.executor.QueryContext(ctx, prefix+`,
	closure_counts AS (SELECT source_id,count(*) AS journal_count FROM bas_legacy_cash_closure GROUP BY source_id)
	SELECT coalesce(sum(journal_count),0), coalesce(max(journal_count),0) FROM closure_counts`, args...)
	if err != nil {
		return 0, 0, taxFactReportingReadError("bound BAS legacy cash closure", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, 0, errors.Join(ErrJournalRepository, sql.ErrNoRows, rows.Err())
	}
	if err := rows.Scan(&total, &maximumPerSource); err != nil {
		return 0, 0, taxFactReportingReadError("scan BAS legacy cash closure bound", err)
	}
	if rows.Next() || rows.Err() != nil {
		return 0, 0, errors.Join(ErrJournalRepository, rows.Err(), errors.New("BAS legacy cash closure aggregate is not singular"))
	}
	if closeErr := rows.Close(); closeErr != nil {
		return 0, 0, taxFactReportingReadError("close BAS legacy cash closure aggregate", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, errors.Join(ErrJournalRepository, err)
	}
	return total, maximumPerSource, nil
}

// basLegacyCashPrefix builds only static SQL plus bound caller identifiers.
// The recursive CTE is capped one row beyond the maximum source/journal pairs.
func basLegacyCashPrefix(query BASLegacyCashQuery) (string, []any) {
	args := []any{query.OrganisationID}
	sourceSQL := `SELECT CAST(NULL AS TEXT) AS source_id WHERE 0`
	if len(query.SourceIDs) > 0 {
		placeholders := make([]string, len(query.SourceIDs))
		for index, sourceID := range query.SourceIDs {
			placeholders[index] = `(?)`
			args = append(args, sourceID)
		}
		sourceSQL = `VALUES ` + strings.Join(placeholders, ",")
	}
	return `/* BAS legacy cash */ WITH RECURSIVE
	target(organisation_id) AS (SELECT ?),
	source_ids(source_id) AS (` + sourceSQL + `),
	bas_legacy_cash_association_candidates AS (
	 SELECT source_ids.source_id AS source_id, journal.id AS journal_id, 'DIRECT' AS association_kind,
	        NULL AS tax_fact_id, NULL AS tax_fact_line_id, NULL AS fact_source_type,
	        NULL AS fact_source_revision, NULL AS fact_source_hash, 0 AS fact_hash_present,
	        512 + length(CAST(source_ids.source_id AS BLOB)) + coalesce(length(CAST(journal.id AS BLOB)),0) + 6 AS payload_bytes,
	        CASE WHEN typeof(journal.id)!='text' OR journal.id='' OR
	          typeof(journal.organisation_id)!='text' OR journal.organisation_id!=target.organisation_id OR
	          typeof(journal.source_kind)!='integer' OR journal.source_kind!=0 OR
	          typeof(journal.source_type)!='text' OR journal.source_type='' OR journal.source_type='OPENING' OR
	          typeof(journal.source_id)!='text' OR journal.source_id!=source_ids.source_id
	        THEN 1 ELSE 0 END AS invalid
	 FROM source_ids JOIN target
	 JOIN journals AS journal ON
	   (journal.source_id=source_ids.source_id OR
	    (typeof(journal.source_id)='blob' AND journal.source_id=CAST(source_ids.source_id AS BLOB)))
	 WHERE (journal.source_kind=0 OR typeof(journal.source_kind)!='integer' OR
	        journal.source_kind NOT IN (0,6,7,8)) AND
	       (typeof(journal.source_type)!='text' OR journal.source_type!='OPENING')
	 UNION ALL
	 SELECT source_ids.source_id AS source_id, journal.id AS journal_id, 'TAX_FACT' AS association_kind,
	        fact.id AS tax_fact_id, fact.journal_line_id AS tax_fact_line_id,
	        fact.source_type AS fact_source_type, fact.source_revision AS fact_source_revision,
	        fact.source_content_hash AS fact_source_hash,
	        CASE WHEN fact.source_content_hash IS NULL THEN 0 ELSE 1 END AS fact_hash_present,
	        512 + length(CAST(source_ids.source_id AS BLOB)) + coalesce(length(CAST(journal.id AS BLOB)),0) + 8 +
	          coalesce(length(CAST(fact.id AS BLOB)),0) + coalesce(length(CAST(fact.journal_line_id AS BLOB)),0) +
	          coalesce(length(CAST(fact.source_type AS BLOB)),0) + coalesce(length(CAST(fact.source_content_hash AS BLOB)),0) AS payload_bytes,
	        CASE WHEN typeof(fact.id)!='text' OR fact.id='' OR
	          typeof(fact.organisation_id)!='text' OR fact.organisation_id!=target.organisation_id OR
	          line.id IS NULL OR typeof(line.id)!='text' OR line.id='' OR
	          typeof(fact.journal_line_id)!='text' OR fact.journal_line_id!=line.id OR
	          journal.id IS NULL OR typeof(journal.id)!='text' OR journal.id='' OR
	          typeof(journal.organisation_id)!='text' OR journal.organisation_id!=target.organisation_id OR
	          typeof(journal.source_kind)!='integer' OR journal.source_kind!=0 OR
	          typeof(journal.source_type)!='text' OR journal.source_type='' OR journal.source_type='OPENING' OR
	          typeof(fact.source_type)!='text' OR fact.source_type='' OR
	          typeof(fact.source_id)!='text' OR fact.source_id!=source_ids.source_id OR
	          typeof(fact.source_revision)!='integer' OR fact.source_revision<1 OR
	          (fact.source_content_hash IS NOT NULL AND
	             (typeof(fact.source_content_hash)!='blob' OR length(CAST(fact.source_content_hash AS BLOB))!=32))
	        THEN 1 ELSE 0 END AS invalid
	 FROM source_ids JOIN target
	 JOIN tax_facts AS fact ON
	   (fact.source_id=source_ids.source_id OR
	    (typeof(fact.source_id)='blob' AND fact.source_id=CAST(source_ids.source_id AS BLOB)))
	 LEFT JOIN journal_lines AS line ON line.id=fact.journal_line_id
	 LEFT JOIN journals AS journal ON journal.id=line.journal_id
	 WHERE (journal.id IS NULL OR (
	        (journal.source_kind=0 OR typeof(journal.source_kind)!='integer' OR
	         journal.source_kind NOT IN (0,6,7,8)) AND
	        (typeof(journal.source_type)!='text' OR journal.source_type!='OPENING')))
	),
	bas_legacy_cash_associations AS (
	 SELECT * FROM bas_legacy_cash_association_candidates
	 ORDER BY source_id,journal_id,association_kind,coalesce(tax_fact_id,'')
	 LIMIT ` + fmt.Sprint(maxBASLegacyCashRows+1) + `
	),
	bas_legacy_cash_roots AS (
	 SELECT source_id,journal_id FROM bas_legacy_cash_associations WHERE journal_id IS NOT NULL GROUP BY source_id,journal_id
	),
	bas_legacy_cash_closure(source_id,journal_id) AS (
	 SELECT source_id,journal_id FROM bas_legacy_cash_roots
	 UNION
	 SELECT closure.source_id, linked.id
	 FROM bas_legacy_cash_closure AS closure
	 JOIN journals AS current ON current.id=closure.journal_id
	 JOIN target ON ` + basLegacyCashOwnerScopeSQL("current.organisation_id", "target.organisation_id") + `
	 JOIN journals AS linked ON
	   (linked.id=current.reversal_of_journal_id OR linked.id=current.reversed_by_journal_id)
	   AND (` + basLegacyCashOwnerScopeSQL("linked.organisation_id", "target.organisation_id") + `)
	 LIMIT ` + fmt.Sprint(basLegacyCashClosureSentinel) + `
	),
	bas_legacy_cash_journal_ids AS (SELECT journal_id AS id FROM bas_legacy_cash_closure GROUP BY journal_id),
	bas_legacy_cash_journals AS (
	 SELECT journal.organisation_id AS organisation_id, journal.id AS id, journal.version AS version,
	        journal.source_revision AS source_revision, journal.source_kind AS source_kind,
	        journal.source_type AS source_type, journal.source_id AS source_id, journal.state AS state,
	        journal.journal_date AS journal_date, journal.reversal_of_journal_id AS reversal_of_id,
	        journal.reversed_by_journal_id AS reversed_by_id,
	        512 + coalesce(length(CAST(journal.organisation_id AS BLOB)),0) + coalesce(length(CAST(journal.id AS BLOB)),0) +
	          coalesce(length(CAST(journal.source_type AS BLOB)),0) + coalesce(length(CAST(journal.source_id AS BLOB)),0) +
	          coalesce(length(CAST(journal.state AS BLOB)),0) + coalesce(length(CAST(journal.journal_date AS BLOB)),0) +
	          coalesce(length(CAST(journal.reversal_of_journal_id AS BLOB)),0) +
	          coalesce(length(CAST(journal.reversed_by_journal_id AS BLOB)),0) AS payload_bytes,
	        CASE WHEN typeof(journal.id)!='text' OR journal.id='' OR
	          typeof(journal.organisation_id)!='text' OR journal.organisation_id!=target.organisation_id OR
	          typeof(journal.version)!='integer' OR journal.version<1 OR
		          typeof(journal.source_revision)!='integer' OR journal.source_revision<1 OR
	          typeof(journal.source_kind)!='integer' OR journal.source_kind!=0 OR
	          typeof(journal.source_type)!='text' OR journal.source_type='' OR
	          typeof(journal.source_id)!='text' OR journal.source_id='' OR
	          typeof(journal.state)!='text' OR journal.state='' OR
	          typeof(journal.journal_date)!='text' OR journal.journal_date='' OR
	          (journal.reversal_of_journal_id IS NOT NULL AND
	             (typeof(journal.reversal_of_journal_id)!='text' OR journal.reversal_of_journal_id='')) OR
	          (journal.reversed_by_journal_id IS NOT NULL AND
	             (typeof(journal.reversed_by_journal_id)!='text' OR journal.reversed_by_journal_id='')) OR
	          ((journal.source_type='REVERSAL') != (journal.reversal_of_journal_id IS NOT NULL)) OR
	          ((journal.state='REVERSED') != (journal.reversed_by_journal_id IS NOT NULL)) OR
	          (journal.reversal_of_journal_id IS NOT NULL AND NOT EXISTS (
	             SELECT 1 FROM journals AS parent WHERE parent.id=journal.reversal_of_journal_id AND
	             (` + basLegacyCashOwnerScopeSQL("parent.organisation_id", "target.organisation_id") + `) AND
	             parent.reversed_by_journal_id=journal.id AND parent.state='REVERSED' AND
	             typeof(parent.version)='integer' AND parent.version>=2 AND
	             journal.source_type='REVERSAL' AND journal.source_id=parent.id AND
	             journal.source_revision=parent.version-1)) OR
	          (journal.reversed_by_journal_id IS NOT NULL AND NOT EXISTS (
	             SELECT 1 FROM journals AS child WHERE child.id=journal.reversed_by_journal_id AND
	             (` + basLegacyCashOwnerScopeSQL("child.organisation_id", "target.organisation_id") + `) AND
	             child.reversal_of_journal_id=journal.id AND child.source_type='REVERSAL' AND
	             child.source_id=journal.id AND typeof(child.source_revision)='integer' AND
	             typeof(journal.version)='integer' AND journal.version>=2 AND
	             child.source_revision=journal.version-1 AND child.state IN ('POSTED','REVERSED')))
	        THEN 1 ELSE 0 END AS invalid
	 FROM bas_legacy_cash_journal_ids AS selected
	 JOIN journals AS journal ON journal.id=selected.id
	 JOIN target ON ` + basLegacyCashOwnerScopeSQL("journal.organisation_id", "target.organisation_id") + `
	 ORDER BY journal.id LIMIT ` + fmt.Sprint(maxBASLegacyCashRows+1) + `
	),
	bas_legacy_cash_lines AS (
	 SELECT line.journal_id AS journal_id, line.id AS id, line.account_id AS account_id,
	        account.organisation_id AS account_organisation_id, line.line_number AS sequence,
	        account.version AS account_version, line.debit_minor AS debit_minor, line.credit_minor AS credit_minor,
	        account.report_classification AS report_classification,
	        512 + coalesce(length(CAST(line.journal_id AS BLOB)),0) + coalesce(length(CAST(line.id AS BLOB)),0) +
	          coalesce(length(CAST(line.account_id AS BLOB)),0) + coalesce(length(CAST(account.organisation_id AS BLOB)),0) +
	          coalesce(length(CAST(account.report_classification AS BLOB)),0) AS payload_bytes,
	        CASE WHEN typeof(line.journal_id)!='text' OR line.journal_id='' OR
	          typeof(line.id)!='text' OR line.id='' OR typeof(line.account_id)!='text' OR line.account_id='' OR
	          typeof(line.line_number)!='integer' OR line.line_number<1 OR line.line_number>4294967295 OR
	          typeof(line.debit_minor)!='integer' OR line.debit_minor<0 OR
	          typeof(line.credit_minor)!='integer' OR line.credit_minor<0 OR
	          account.id IS NULL OR typeof(account.id)!='text' OR account.id!=line.account_id OR
	          typeof(account.organisation_id)!='text' OR account.organisation_id!=target.organisation_id OR
	          typeof(account.version)!='integer' OR account.version<1 OR
	          typeof(account.report_classification)!='text' OR account.report_classification=''
	        THEN 1 ELSE 0 END AS invalid
	 FROM bas_legacy_cash_journal_ids AS selected
	 JOIN journal_lines AS line ON line.journal_id=selected.id
		OR (typeof(line.journal_id)='blob' AND line.journal_id=CAST(selected.id AS BLOB))
	 LEFT JOIN accounts AS account ON account.id=line.account_id
	 CROSS JOIN target
	 ORDER BY line.journal_id,line.line_number,line.id LIMIT ` + fmt.Sprint(maxBASLegacyCashRows+1) + `
	)
	`, args
}

func basLegacyCashOwnerScopeSQL(column, target string) string {
	return `(` + column + `=` + target + ` OR (typeof(` + column + `)='blob' AND length(` + column + `)=36 AND ` + column + `=CAST(` + target + ` AS BLOB)))`
}

func (repository *JournalRepository) readBASLegacyCashRows(
	ctx context.Context, query BASLegacyCashQuery, expected BASLegacyCashSummary,
) (*BASLegacyCashRead, error) {
	prefix, args := basLegacyCashPrefix(query)
	querySQL := prefix + `,
	association_rows AS (
	 SELECT 'A' AS row_kind, source_id AS sort1, journal_id AS sort2, association_kind AS sort3,
	        coalesce(tax_fact_id,'') AS sort4, 0 AS sort_number, payload_bytes, invalid,
	        source_id AS t1, journal_id AS t2, association_kind AS t3, tax_fact_id AS t4,
	        tax_fact_line_id AS t5, fact_source_type AS t6, NULL AS t7, NULL AS t8,
	        fact_source_revision AS n1, fact_hash_present AS n2, NULL AS n3, NULL AS n4, fact_source_hash AS b1
	 FROM bas_legacy_cash_associations ORDER BY source_id,journal_id,association_kind,coalesce(tax_fact_id,'') LIMIT ?
	),
	journal_rows AS (
	 SELECT 'J' AS row_kind, id AS sort1, '' AS sort2, '' AS sort3, '' AS sort4, 0 AS sort_number,
	        payload_bytes, invalid,
	        organisation_id AS t1, id AS t2, source_type AS t3, source_id AS t4, state AS t5,
	        journal_date AS t6, reversal_of_id AS t7, reversed_by_id AS t8,
	        version AS n1, source_revision AS n2, source_kind AS n3, NULL AS n4, NULL AS b1
	 FROM bas_legacy_cash_journals ORDER BY id LIMIT ?
	),
	line_rows AS (
	 SELECT 'L' AS row_kind, journal_id AS sort1, id AS sort2, '' AS sort3, '' AS sort4, sequence AS sort_number,
	        payload_bytes, invalid,
	        journal_id AS t1, id AS t2, account_id AS t3, account_organisation_id AS t4,
	        report_classification AS t5, NULL AS t6, NULL AS t7, NULL AS t8,
	        sequence AS n1, account_version AS n2, debit_minor AS n3, credit_minor AS n4, NULL AS b1
	 FROM bas_legacy_cash_lines ORDER BY journal_id,sequence,id LIMIT ?
	),
	all_rows AS (
	 SELECT * FROM association_rows UNION ALL SELECT * FROM journal_rows UNION ALL SELECT * FROM line_rows
	),
	bounded_rows AS (
	 SELECT *, row_number() OVER (ORDER BY row_kind,sort1,sort2,sort3,sort4,sort_number) AS row_number,
	        sum(payload_bytes) OVER (ORDER BY row_kind,sort1,sort2,sort3,sort4,sort_number ROWS UNBOUNDED PRECEDING) AS cumulative_bytes
	 FROM all_rows ORDER BY row_kind,sort1,sort2,sort3,sort4,sort_number LIMIT ?
	)
	SELECT row_kind,payload_bytes,invalid,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t1 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t2 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t3 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t4 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t5 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t6 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t7 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN t8 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN n1 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN n2 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN n3 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN n4 END,
	       CASE WHEN invalid=0 AND row_number<=? AND cumulative_bytes<=? THEN b1 END
	FROM bounded_rows ORDER BY row_kind,sort1,sort2,sort3,sort4,sort_number`
	args = append(args,
		maxBASLegacyCashRows+1, maxBASLegacyCashRows+1, maxBASLegacyCashRows+1, expected.RowCount+1,
	)
	for index := 0; index < 13; index++ {
		args = append(args, expected.RowCount, expected.Bytes)
	}
	rows, err := repository.executor.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, taxFactReportingReadError("read BAS legacy cash rows", err)
	}
	read := &BASLegacyCashRead{
		Summary:      expected,
		Associations: make([]BASLegacyCashAssociation, 0, expected.AssociationCount),
		Journals:     make([]BASLegacyCashJournal, 0, expected.JournalCount),
		Lines:        make([]BASLegacyCashLine, 0, expected.LineCount),
	}
	var charged int64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrJournalRepository, err, rows.Close())
		}
		if int64(len(read.Associations)+len(read.Journals)+len(read.Lines)) >= expected.RowCount {
			return nil, errors.Join(&TaxFactReportingBoundError{Resource: "BAS legacy cash rows", Limit: int(expected.RowCount)}, rows.Close())
		}
		var rowKind string
		var payloadFloat float64
		var invalid int64
		var texts [8]sql.NullString
		var numbers [4]sql.NullInt64
		var hash []byte
		if err := rows.Scan(&rowKind, &payloadFloat, &invalid,
			&texts[0], &texts[1], &texts[2], &texts[3], &texts[4], &texts[5], &texts[6], &texts[7],
			&numbers[0], &numbers[1], &numbers[2], &numbers[3], &hash); err != nil {
			return nil, errors.Join(taxFactReportingReadError("scan BAS legacy cash row", err), rows.Close())
		}
		if invalid != 0 || math.IsNaN(payloadFloat) || math.IsInf(payloadFloat, 0) || math.Trunc(payloadFloat) != payloadFloat ||
			payloadFloat < 512 || payloadFloat > float64(expected.Bytes-charged) {
			return nil, errors.Join(fmt.Errorf("%w: invalid or over-budget BAS legacy cash row", ErrJournalRepository), rows.Close())
		}
		payload := int64(payloadFloat)
		var retainedPayloadBytes int64
		requiredText := func(index int) (string, bool) { return texts[index].String, texts[index].Valid }
		requiredNumber := func(index int) (int64, bool) { return numbers[index].Int64, numbers[index].Valid }
		switch rowKind {
		case "A":
			var association BASLegacyCashAssociation
			var ok bool
			if association.SourceID, ok = requiredText(0); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if association.JournalID, ok = requiredText(1); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if association.Kind, ok = requiredText(2); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if texts[3].Valid {
				association.TaxFactID = basLegacyCashStringPointer(texts[3].String)
			}
			if texts[4].Valid {
				association.JournalLineID = basLegacyCashStringPointer(texts[4].String)
			}
			if texts[5].Valid {
				association.FactSourceType = basLegacyCashStringPointer(texts[5].String)
			}
			if numbers[0].Valid {
				if numbers[0].Int64 < 0 {
					return nil, errors.Join(ErrJournalRepository, rows.Close())
				}
				revision := uint64(numbers[0].Int64)
				association.FactSourceRevision = &revision
			}
			hashPresent, hashFlagOK := requiredNumber(1)
			if !hashFlagOK || hashPresent < 0 || hashPresent > 1 {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			association.FactSourceContentHashPresent = hashPresent == 1
			if association.FactSourceContentHashPresent {
				if len(hash) != 32 {
					return nil, errors.Join(ErrJournalRepository, rows.Close())
				}
				association.FactSourceContentHash = bytes.Clone(hash)
			} else if hash != nil {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if association.Kind == "DIRECT" {
				if association.TaxFactID != nil || association.JournalLineID != nil || association.FactSourceType != nil ||
					association.FactSourceRevision != nil || association.FactSourceContentHashPresent {
					return nil, errors.Join(ErrJournalRepository, rows.Close())
				}
			} else if association.Kind != "TAX_FACT" || association.TaxFactID == nil || association.JournalLineID == nil ||
				association.FactSourceType == nil || association.FactSourceRevision == nil || *association.FactSourceRevision == 0 {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			associationFields := [][]byte{[]byte(association.SourceID), []byte(association.JournalID), []byte(association.Kind)}
			if association.TaxFactID != nil {
				associationFields = append(associationFields, []byte(*association.TaxFactID))
			}
			if association.JournalLineID != nil {
				associationFields = append(associationFields, []byte(*association.JournalLineID))
			}
			if association.FactSourceType != nil {
				associationFields = append(associationFields, []byte(*association.FactSourceType))
			}
			if association.FactSourceContentHashPresent {
				associationFields = append(associationFields, association.FactSourceContentHash)
			}
			retainedPayloadBytes = taxFactReportingPayloadBytes(associationFields...)
			read.Associations = append(read.Associations, association)
		case "J":
			var journal BASLegacyCashJournal
			var ok bool
			if journal.OrganisationID, ok = requiredText(0); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if journal.ID, ok = requiredText(1); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if journal.SourceType, ok = requiredText(2); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if journal.SourceID, ok = requiredText(3); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if journal.State, ok = requiredText(4); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if journal.JournalDate, ok = requiredText(5); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if texts[6].Valid {
				journal.ReversalOfID = basLegacyCashStringPointer(texts[6].String)
			}
			if texts[7].Valid {
				journal.ReversedByID = basLegacyCashStringPointer(texts[7].String)
			}
			version, vOK := requiredNumber(0)
			sourceRevision, revisionOK := requiredNumber(1)
			sourceKind, kindOK := requiredNumber(2)
			if !vOK || !revisionOK || !kindOK || version < 1 || sourceRevision < 1 {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			journal.Version, journal.SourceRevision, journal.SourceKind = uint64(version), uint64(sourceRevision), sourceKind
			if journal.OrganisationID != query.OrganisationID || journal.ID == "" || journal.SourceID == "" ||
				!validDateString(journal.JournalDate) {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			journalFields := [][]byte{[]byte(journal.OrganisationID), []byte(journal.ID), []byte(journal.SourceType),
				[]byte(journal.SourceID), []byte(journal.State), []byte(journal.JournalDate)}
			if journal.ReversalOfID != nil {
				journalFields = append(journalFields, []byte(*journal.ReversalOfID))
			}
			if journal.ReversedByID != nil {
				journalFields = append(journalFields, []byte(*journal.ReversedByID))
			}
			retainedPayloadBytes = taxFactReportingPayloadBytes(journalFields...)
			read.Journals = append(read.Journals, journal)
		case "L":
			var line BASLegacyCashLine
			var ok bool
			if line.JournalID, ok = requiredText(0); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if line.ID, ok = requiredText(1); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if line.AccountID, ok = requiredText(2); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if line.AccountOrganisationID, ok = requiredText(3); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			if line.ReportClassification, ok = requiredText(4); !ok {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			sequence, seqOK := requiredNumber(0)
			accountVersion, accountOK := requiredNumber(1)
			debit, debitOK := requiredNumber(2)
			credit, creditOK := requiredNumber(3)
			if !seqOK || !accountOK || !debitOK || !creditOK || sequence < 1 || sequence > math.MaxUint32 ||
				accountVersion < 1 || debit < 0 || credit < 0 || line.AccountOrganisationID != query.OrganisationID ||
				line.JournalID == "" || line.ID == "" {
				return nil, errors.Join(ErrJournalRepository, rows.Close())
			}
			line.Sequence, line.AccountVersion = uint64(sequence), uint64(accountVersion)
			line.DebitMinor, line.CreditMinor = debit, credit
			retainedPayloadBytes = taxFactReportingPayloadBytes([]byte(line.JournalID), []byte(line.ID),
				[]byte(line.AccountID), []byte(line.AccountOrganisationID), []byte(line.ReportClassification))
			read.Lines = append(read.Lines, line)
		default:
			return nil, errors.Join(ErrJournalRepository, rows.Close())
		}
		if retainedPayloadBytes != payload {
			return nil, errors.Join(fmt.Errorf("%w: BAS legacy cash SQL charge differs from decoded cells", ErrJournalRepository), rows.Close())
		}
		charged += retainedPayloadBytes
	}
	rowsErr, closeErr := rows.Err(), rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(ErrJournalRepository, rowsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if int64(len(read.Associations)) != expected.AssociationCount || int64(len(read.Journals)) != expected.JournalCount ||
		int64(len(read.Lines)) != expected.LineCount || int64(len(read.Associations)+len(read.Journals)+len(read.Lines)) != expected.RowCount || charged != expected.Bytes {
		return nil, fmt.Errorf("%w: BAS legacy cash rows changed after reservation", ErrJournalRepository)
	}
	return read, nil
}

func basLegacyCashStringPointer(value string) *string { return &value }

func validateBASLegacyCashRead(query BASLegacyCashQuery, read *BASLegacyCashRead) error {
	if read == nil || read.Summary.OrganisationID != query.OrganisationID ||
		read.Summary.AssociationCount != int64(len(read.Associations)) || read.Summary.JournalCount != int64(len(read.Journals)) ||
		read.Summary.LineCount != int64(len(read.Lines)) || read.Summary.RowCount != int64(len(read.Associations)+len(read.Journals)+len(read.Lines)) {
		return ErrJournalRepository
	}
	journalByID := make(map[string]BASLegacyCashJournal, len(read.Journals))
	for _, journal := range read.Journals {
		if _, duplicate := journalByID[journal.ID]; duplicate {
			return fmt.Errorf("%w: duplicate BAS legacy cash journal", ErrJournalRepository)
		}
		if journal.SourceKind != 0 || journal.SourceType == "" || journal.SourceID == "" || journal.SourceRevision == 0 ||
			(journal.State != "DRAFT" && journal.State != "POSTED" && journal.State != "REVERSED") ||
			(journal.SourceType == "REVERSAL") != (journal.ReversalOfID != nil) ||
			(journal.State == "REVERSED") != (journal.ReversedByID != nil) {
			return ErrJournalRepository
		}
		journalByID[journal.ID] = journal
	}
	for _, journal := range read.Journals {
		if journal.ReversalOfID != nil {
			parent, ok := journalByID[*journal.ReversalOfID]
			if !ok || parent.ReversedByID == nil || *parent.ReversedByID != journal.ID || parent.State != "REVERSED" ||
				journal.SourceType != "REVERSAL" || journal.SourceID != parent.ID || parent.Version < 2 ||
				journal.SourceRevision != parent.Version-1 {
				return fmt.Errorf("%w: invalid BAS legacy cash reversal parent", ErrJournalRepository)
			}
		}
		if journal.ReversedByID != nil {
			child, ok := journalByID[*journal.ReversedByID]
			if !ok || child.ReversalOfID == nil || *child.ReversalOfID != journal.ID || child.SourceType != "REVERSAL" ||
				child.SourceID != journal.ID || journal.Version < 2 || child.SourceRevision != journal.Version-1 ||
				(child.State != "POSTED" && child.State != "REVERSED") {
				return fmt.Errorf("%w: invalid BAS legacy cash reversal child", ErrJournalRepository)
			}
		}
	}
	// A reciprocal graph can still contain a closed cycle. Every retained
	// reversal predecessor chain must terminate at a non-reversal root.
	state := make(map[string]uint8, len(journalByID))
	for id := range journalByID {
		if state[id] == 2 {
			continue
		}
		path := make([]string, 0)
		for next := id; next != "" && state[next] != 2; {
			if state[next] == 1 {
				return fmt.Errorf("%w: cyclic BAS legacy cash reversal graph", ErrJournalRepository)
			}
			state[next] = 1
			path = append(path, next)
			header := journalByID[next]
			next = ""
			if header.ReversalOfID != nil {
				next = *header.ReversalOfID
			}
		}
		for _, member := range path {
			state[member] = 2
		}
	}
	requestedSources := make(map[string]struct{}, len(query.SourceIDs))
	for _, sourceID := range query.SourceIDs {
		requestedSources[sourceID] = struct{}{}
	}
	roots := make(map[string]struct{})
	seenAssociation := make(map[string]struct{}, len(read.Associations))
	for _, association := range read.Associations {
		if association.Kind != "DIRECT" && association.Kind != "TAX_FACT" {
			return ErrJournalRepository
		}
		if _, requested := requestedSources[association.SourceID]; !requested {
			return fmt.Errorf("%w: association source outside query", ErrJournalRepository)
		}
		header, ok := journalByID[association.JournalID]
		if !ok {
			return fmt.Errorf("%w: source association outside reversal closure", ErrJournalRepository)
		}
		if header.SourceKind != 0 || header.SourceType == "OPENING" ||
			association.Kind == "DIRECT" && header.SourceID != association.SourceID {
			return fmt.Errorf("%w: invalid BAS legacy cash root association", ErrJournalRepository)
		}
		key := association.SourceID + "\x00" + association.JournalID + "\x00" + association.Kind + "\x00"
		if association.TaxFactID != nil {
			key += *association.TaxFactID
		}
		if _, duplicate := seenAssociation[key]; duplicate {
			return fmt.Errorf("%w: duplicate BAS legacy cash association", ErrJournalRepository)
		}
		seenAssociation[key] = struct{}{}
		roots[association.JournalID] = struct{}{}
		if association.Kind == "TAX_FACT" && (*association.TaxFactID == "" || *association.JournalLineID == "" ||
			*association.FactSourceType == "" || association.FactSourceContentHashPresent && len(association.FactSourceContentHash) != 32 ||
			!association.FactSourceContentHashPresent && association.FactSourceContentHash != nil) {
			return fmt.Errorf("%w: invalid retained BAS legacy tax-fact provenance", ErrJournalRepository)
		}
	}
	// Reconstruct the closure from every retained source root so a query-shape
	// regression cannot add or omit an unrelated same-owner journal.
	queue := make([]string, 0, len(roots))
	for id := range roots {
		queue = append(queue, id)
	}
	reached := make(map[string]struct{}, len(journalByID))
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if _, known := reached[id]; known {
			continue
		}
		header, ok := journalByID[id]
		if !ok {
			return fmt.Errorf("%w: missing BAS legacy cash closure node", ErrJournalRepository)
		}
		reached[id] = struct{}{}
		for _, linked := range []*string{header.ReversalOfID, header.ReversedByID} {
			if linked == nil {
				continue
			}
			if _, exists := journalByID[*linked]; !exists {
				return fmt.Errorf("%w: incomplete BAS legacy cash closure", ErrJournalRepository)
			}
			queue = append(queue, *linked)
		}
	}
	if len(reached) != len(journalByID) {
		return fmt.Errorf("%w: extraneous BAS legacy cash closure node", ErrJournalRepository)
	}
	seenLine := make(map[string]struct{}, len(read.Lines))
	seenSequence := make(map[string]struct{}, len(read.Lines))
	taxFactLines := make([]string, 0)
	for _, association := range read.Associations {
		if association.Kind == "TAX_FACT" {
			taxFactLines = append(taxFactLines, association.JournalID+"\x00"+*association.JournalLineID)
		}
	}
	for _, line := range read.Lines {
		if _, ok := journalByID[line.JournalID]; !ok {
			return fmt.Errorf("%w: line outside selected journal set", ErrJournalRepository)
		}
		if line.AccountOrganisationID != query.OrganisationID || line.AccountID == "" ||
			line.ReportClassification == "" || line.Sequence == 0 || line.Sequence > math.MaxUint32 {
			return ErrJournalRepository
		}
		key := line.JournalID + "\x00" + line.ID
		if _, duplicate := seenLine[key]; duplicate {
			return fmt.Errorf("%w: duplicate BAS legacy cash line", ErrJournalRepository)
		}
		sequenceKey := fmt.Sprintf("%s\x00%d", line.JournalID, line.Sequence)
		if _, duplicate := seenSequence[sequenceKey]; duplicate {
			return fmt.Errorf("%w: duplicate BAS legacy cash line sequence", ErrJournalRepository)
		}
		seenLine[key] = struct{}{}
		seenSequence[sequenceKey] = struct{}{}
	}
	for _, key := range taxFactLines {
		if _, exists := seenLine[key]; !exists {
			return fmt.Errorf("%w: tax fact line is outside the selected line proof", ErrJournalRepository)
		}
	}
	return nil
}
