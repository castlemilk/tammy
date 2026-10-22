package accounting

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Both body statements recheck the complete reserved parent in SQLite before
// returning retained cells. A late change cannot be copied before rejection.
var taxFactReportingJournalBodyGuardSQL = func() string {
	headerSize := taxFactReportingPayloadSizeSQL("j.id", "j.organisation_id", "j.source_type", "j.source_id", "j.state", "j.journal_date", "j.description", "j.reversal_of_journal_id", "j.reversed_by_journal_id", "j.currency_code", "j.posted_at", "j.created_at")
	lineSize := taxFactReportingPayloadSizeSQL("line.id", "line.journal_id", "line.account_id", "line.currency_code", "line.memo", "line.tax_code_id", "line.tax_rule_type", "line.tax_rule_id", "line.tax_rule_content_hash")
	headerNumeric := taxFactReportingInvalidNumericStorageSQL([]string{"j.version", "j.source_kind", "j.source_revision", "j.total_debits_minor", "j.total_credits_minor", "j.financial_revision"}, nil)
	lineNumeric := taxFactReportingInvalidNumericStorageSQL([]string{"line.line_number", "line.debit_minor", "line.credit_minor"}, []string{"line.tax_amount_minor", "line.tax_rule_revision"})
	return `WITH reporting_target(journal_id,organisation_id,expected_payload_bytes,expected_line_count) AS MATERIALIZED (SELECT ?,?,?,?),
	 header_state AS MATERIALIZED (
	 SELECT count(*) AS parent_count,total(` + headerSize + `) AS payload_bytes,
	 coalesce(sum(` + headerNumeric + `),0) AS invalid_body,
	 coalesce(sum(CASE WHEN typeof(j.organisation_id)!='text' OR j.organisation_id IS NOT target.organisation_id THEN 1 ELSE 0 END),0) AS invalid_owner
	 FROM journals AS j CROSS JOIN reporting_target AS target WHERE j.id=target.journal_id),
	 line_state AS MATERIALIZED (
	 SELECT count(*) AS line_count,total(payload_bytes) AS payload_bytes,coalesce(sum(invalid_body),0) AS invalid_body FROM (
	 SELECT ` + lineSize + ` AS payload_bytes,` + lineNumeric + ` + CASE WHEN
	 typeof(line.journal_id)!='text' OR line.journal_id IS NOT target.journal_id OR
	 line.line_number<1 OR line.line_number>4294967295 OR line.currency_code IS NULL OR line.currency_code='' OR
	 (line.tax_rule_content_hash IS NOT NULL AND (typeof(line.tax_rule_content_hash)!='blob' OR length(line.tax_rule_content_hash)!=32))
	 THEN 1 ELSE 0 END AS invalid_body
	 FROM journal_lines AS line CROSS JOIN reporting_target AS target
	 WHERE line.journal_id=target.journal_id OR (typeof(line.journal_id)='blob' AND line.journal_id=CAST(target.journal_id AS BLOB))
	 ORDER BY line.line_number,line.id LIMIT ` + fmt.Sprint(maxTaxFactReportingJournalLines+1) + `)),
	 reporting_guard AS MATERIALIZED (
	 SELECT CASE WHEN header_state.parent_count!=1 OR header_state.invalid_owner!=0 OR header_state.invalid_body!=0 OR
	 line_state.line_count!=target.expected_line_count OR line_state.invalid_body!=0 OR
	 header_state.payload_bytes+line_state.payload_bytes!=target.expected_payload_bytes THEN 1 ELSE 0 END AS invalid
	 FROM reporting_target AS target CROSS JOIN header_state CROSS JOIN line_state) `
}()

var taxFactReportingJournalHeaderBodySQL = func() string {
	columns := strings.Fields("id organisation_id version source_type source_kind source_id source_revision state journal_date description reversal_of_journal_id reversed_by_journal_id total_debits_minor total_credits_minor currency_code financial_revision posted_at created_at")
	projection := []string{"reporting_guard.invalid"}
	for _, column := range columns {
		fallback := "''"
		switch column {
		case "version", "source_kind", "source_revision", "total_debits_minor", "total_credits_minor", "financial_revision":
			fallback = "0"
		case "reversal_of_journal_id", "reversed_by_journal_id", "posted_at":
			fallback = "NULL"
		}
		projection = append(projection, "CASE WHEN reporting_guard.invalid=0 THEN j."+column+" ELSE "+fallback+" END AS "+column)
	}
	return "/* tax fact reporting journal header body */ " + taxFactReportingJournalBodyGuardSQL +
		"SELECT " + strings.Join(projection, ",") + " FROM reporting_guard CROSS JOIN reporting_target AS target LEFT JOIN journals AS j ON reporting_guard.invalid=0 AND j.id=target.journal_id"
}()

var taxFactReportingJournalLinesBodySQL = func() string {
	columns := strings.Fields("id journal_id line_number account_id debit_minor credit_minor currency_code memo tax_code_id tax_amount_minor tax_rule_type tax_rule_id tax_rule_revision tax_rule_content_hash")
	projection := []string{"reporting_guard.invalid", "coalesce(body_line.has_line,0) AS has_line"}
	for _, column := range columns {
		fallback := "NULL"
		switch column {
		case "id", "journal_id", "account_id", "currency_code":
			fallback = "''"
		case "line_number", "debit_minor", "credit_minor":
			fallback = "0"
		}
		projection = append(projection, "CASE WHEN reporting_guard.invalid=0 AND body_line.has_line=1 THEN body_line."+column+" ELSE "+fallback+" END AS "+column)
	}
	return "/* tax fact reporting journal lines body */ " + taxFactReportingJournalBodyGuardSQL +
		`, body_lines AS MATERIALIZED (SELECT line.*,1 AS has_line FROM journal_lines AS line CROSS JOIN reporting_target AS target CROSS JOIN reporting_guard
		WHERE reporting_guard.invalid=0 AND line.journal_id=target.journal_id ORDER BY line.line_number,line.id LIMIT ?)
		SELECT ` + strings.Join(projection, ",") + ` FROM reporting_guard LEFT JOIN body_lines AS body_line ON 1=1 ORDER BY body_line.line_number,body_line.id`
}()

var errTaxFactReportingNoLine = errors.New("accounting: reporting parent has no line")

type taxFactReportingBodyScanner struct {
	rows *sql.Rows
	line bool
}

func (scanner taxFactReportingBodyScanner) Scan(destinations ...any) error {
	var invalid, hasLine int64
	values := []any{&invalid}
	if scanner.line {
		values = append(values, &hasLine)
	}
	values = append(values, destinations...)
	if err := scanner.rows.Scan(values...); err != nil {
		return err
	}
	if invalid != 0 {
		return ErrJournalRepository
	}
	if scanner.line && hasLine == 0 {
		return errTaxFactReportingNoLine
	}
	return nil
}
