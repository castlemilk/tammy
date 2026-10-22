package purchases

import "strings"

// Combine only module-owned fixed rejection predicates. Bound identities never
// enter SQL text; projection and charging remain with each existing reader.
func purchaseReportingCombineOwnerGuards(guards ...*postingReportingOwnerGuard) *postingReportingOwnerGuard {
	result := &postingReportingOwnerGuard{}
	parts := make([]string, 0, len(guards))
	for _, guard := range guards {
		if guard == nil || guard.invalid == "" {
			continue
		}
		parts = append(parts, "("+guard.invalid+")")
		result.args = append(result.args, guard.args...)
	}
	if len(parts) == 0 {
		return nil
	}
	result.invalid = strings.Join(parts, " OR ")
	return result
}

// Retain only the immutable SQL policy once. Building a reporting guard must
// not rebuild the scalar projector's column maps for every dependent statement.
var purchaseReportingModernReferenceSQL = func() [BASScalarAttribution + 1][2]string {
	var result [BASScalarAttribution + 1][2]string
	for _, family := range purchaseBASModernFamilies() {
		for eventScope := range 2 {
			scope := ""
			if family.kind == BASScalarEvent && eventScope == 0 {
				scope = " AND ev.scope_kind='POSTING'"
			}
			result[family.kind][eventScope] = `EXISTS(SELECT 1 FROM ` + family.from + `
			CROSS JOIN (SELECT ? AS organisation_id) AS target
			WHERE (` + family.where + `)` + scope + ` AND (` + family.invalid + `))`
		}
	}
	return result
}()

// Reuse the scalar reader's exact required-link policy. The closed family
// selector covers the organisation's reserved family, including earlier rows
// whose parents may change before a later sibling body statement.
func purchaseReportingModernFamilyReferenceGuard(org string, kind BASScalarKeyKind, allEventScopes bool) *postingReportingOwnerGuard {
	if kind >= BASScalarPosting && kind <= BASScalarAttribution {
		scope := 0
		if allEventScopes {
			scope = 1
		}
		return &postingReportingOwnerGuard{invalid: purchaseReportingModernReferenceSQL[kind][scope], args: []any{org}}
	}
	// An unknown internal family cannot silently disable a reporting guard.
	return &postingReportingOwnerGuard{invalid: "1"}
}

// Manual references remain scoped to the exact already-reserved source batch.
func purchaseReportingManualSourceReferenceGuard(org string, sourceIDs []string) *postingReportingOwnerGuard {
	if len(sourceIDs) == 0 {
		return nil
	}
	values := strings.TrimSuffix(strings.Repeat("(?),", len(sourceIDs)), ",")
	return purchaseReportingManualReferenceGuard(org, "VALUES "+values, batchArgs(sourceIDs), false)
}

// Receipt discovery has the same finite source relation as its existing count.
// The extra account branch validates the credit-side classification inputs
// before a malformed owner can make the count silently return zero.
func purchaseReportingReceiptReferenceGuard(org string) *postingReportingOwnerGuard {
	return purchaseReportingManualReferenceGuard(org, `SELECT id FROM documents WHERE organisation_id=?
		UNION SELECT p.id FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED')`, []any{org, org}, true)
}

// sourceSQL is supplied only by the two closed constructors above. All source
// identities and organisations are bound values, including rejection-only BLOB
// references; valid TEXT row selection remains in the original reader.
func purchaseReportingManualReferenceGuard(org, sourceSQL string, sourceArgs []any, receipt bool) *postingReportingOwnerGuard {
	invalid := `EXISTS(WITH reference_target(organisation_id) AS (SELECT ?),
		reference_sources(id) AS (` + sourceSQL + `)
		SELECT 1 FROM (
		SELECT 1 FROM journals AS ref_j JOIN reference_sources AS source
		ON ref_j.source_id=source.id OR (typeof(ref_j.source_id)='blob' AND length(ref_j.source_id)=36 AND ref_j.source_id=CAST(source.id AS BLOB))
		CROSS JOIN reference_target AS target
		WHERE (ref_j.source_type IS NULL OR ref_j.source_type<>'OPENING')
		AND (ref_j.source_kind=0 OR typeof(ref_j.source_kind)!='integer' OR ref_j.source_kind NOT IN (0,6,7,8))
		AND (typeof(ref_j.id)!='text' OR typeof(ref_j.source_id)!='text' OR typeof(ref_j.organisation_id)!='text' OR ref_j.organisation_id IS NOT target.organisation_id
			OR typeof(ref_j.source_kind)!='integer' OR ref_j.source_kind!=0
			OR typeof(ref_j.source_type)!='text' OR ref_j.source_type='')
		UNION ALL
		SELECT 1 FROM tax_facts AS ref_f JOIN reference_sources AS source
		ON ref_f.source_id=source.id OR (typeof(ref_f.source_id)='blob' AND length(ref_f.source_id)=36 AND ref_f.source_id=CAST(source.id AS BLOB))
		LEFT JOIN journal_lines AS ref_l ON ref_l.id=ref_f.journal_line_id
		LEFT JOIN journals AS ref_j ON ref_j.id=ref_l.journal_id
		CROSS JOIN reference_target AS target
		WHERE typeof(ref_f.source_id)!='text' OR typeof(ref_f.journal_line_id)!='text'
		OR typeof(ref_f.organisation_id)!='text' OR ref_f.organisation_id IS NOT target.organisation_id
		OR ref_l.id IS NULL OR ref_j.id IS NULL OR typeof(ref_j.id)!='text' OR typeof(ref_l.journal_id)!='text'
		OR typeof(ref_j.organisation_id)!='text' OR ref_j.organisation_id IS NOT target.organisation_id
		OR typeof(ref_j.source_kind)!='integer' OR ref_j.source_kind NOT IN (0,6,7,8)
		OR (ref_j.source_kind=0 AND (typeof(ref_j.source_type)!='text' OR ref_j.source_type=''))`
	if receipt {
		invalid += ` UNION ALL
		SELECT 1 FROM journals AS ref_j JOIN journal_lines AS ref_l ON ref_l.journal_id=ref_j.id
		LEFT JOIN accounts AS ref_a ON ref_a.id=ref_l.account_id
		WHERE ref_j.state='POSTED' AND ref_j.source_kind=0 AND ref_j.source_type NOT IN ('OPENING','REVERSAL')
		AND (ref_j.source_id IN (SELECT id FROM reference_sources) OR EXISTS(
			SELECT 1 FROM tax_facts AS ref_source JOIN journal_lines AS ref_source_line ON ref_source_line.id=ref_source.journal_line_id
			WHERE ref_source.organisation_id=ref_j.organisation_id AND ref_source_line.journal_id=ref_j.id
			AND ref_source.source_id IN (SELECT id FROM reference_sources)))
		AND ref_l.credit_minor>ref_l.debit_minor
		AND (ref_a.id IS NULL OR typeof(ref_l.account_id)!='text' OR typeof(ref_a.organisation_id)!='text'
			OR ref_a.organisation_id IS NOT ref_j.organisation_id)`
	}
	invalid += `) LIMIT 1)`
	return &postingReportingOwnerGuard{invalid: invalid, args: append([]any{org}, sourceArgs...)}
}

// Selected opening conversions that represent posted or replaced history must
// retain their required journal parent. The existing reserved-header guard
// keeps this relation bounded to the exact selected conversion batch.
func purchaseReportingConversionJournalReferenceGuard(org string, conversionIDs []string) *postingReportingOwnerGuard {
	if len(conversionIDs) == 0 {
		return nil
	}
	values := strings.TrimSuffix(strings.Repeat("(?),", len(conversionIDs)), ",")
	journalReference := &postingReportingOwnerGuard{
		invalid: `EXISTS(WITH reference_target(organisation_id) AS (SELECT ?),
			reference_conversions(id) AS (VALUES ` + values + `)
			SELECT 1 FROM reference_conversions AS selected
			JOIN opening_conversions AS ref_c ON ref_c.id=selected.id
			LEFT JOIN journals AS ref_j ON ref_j.id=ref_c.journal_id
				OR (typeof(ref_j.id)='blob' AND ref_j.id=CAST(ref_c.journal_id AS BLOB))
			CROSS JOIN reference_target AS target
			WHERE ref_c.state IN ('POSTED','REPLACED') AND
				(typeof(ref_c.journal_id)!='text' OR ref_j.id IS NULL OR typeof(ref_j.id)!='text'
				OR typeof(ref_j.organisation_id)!='text' OR ref_j.organisation_id IS NOT target.organisation_id)
			LIMIT 1)`,
		args: append([]any{org}, batchArgs(conversionIDs)...),
	}
	return purchaseReportingCombineOwnerGuards(purchaseReportingConversionOwnerGuard(org, conversionIDs), journalReference)
}

// Journal-account discovery validates only the journals selected by the
// bounded header closure and their line/account references. BLOB equality arms
// surface malformed links without imposing UUID shapes on TEXT identities.
func purchaseReportingJournalAccountReferenceGuard(org string, journalIDs []string) *postingReportingOwnerGuard {
	if len(journalIDs) == 0 {
		return nil
	}
	values := strings.TrimSuffix(strings.Repeat("(?),", len(journalIDs)), ",")
	return &postingReportingOwnerGuard{
		invalid: `EXISTS(WITH reference_target(organisation_id) AS (SELECT ?),
			reference_journals(id) AS (VALUES ` + values + `)
			SELECT 1 FROM (
			SELECT 1 FROM reference_journals AS selected
			LEFT JOIN journals AS ref_j ON ref_j.id=selected.id
				OR (typeof(ref_j.id)='blob' AND ref_j.id=CAST(selected.id AS BLOB))
			CROSS JOIN reference_target AS target
			WHERE ref_j.id IS NULL OR typeof(ref_j.id)!='text' OR typeof(ref_j.organisation_id)!='text'
				OR ref_j.organisation_id IS NOT target.organisation_id
			UNION ALL
			SELECT 1 FROM reference_journals AS selected
			JOIN journals AS ref_j ON ref_j.id=selected.id
				OR (typeof(ref_j.id)='blob' AND ref_j.id=CAST(selected.id AS BLOB))
			JOIN journal_lines AS ref_l ON ref_l.journal_id=ref_j.id
				OR (typeof(ref_l.journal_id)='blob' AND ref_l.journal_id=CAST(ref_j.id AS BLOB))
			LEFT JOIN accounts AS ref_a ON ref_a.id=ref_l.account_id
			CROSS JOIN reference_target AS target
			WHERE typeof(ref_j.id)!='text' OR typeof(ref_j.organisation_id)!='text'
				OR ref_j.organisation_id IS NOT target.organisation_id
				OR typeof(ref_l.id)!='text' OR typeof(ref_l.journal_id)!='text' OR typeof(ref_l.account_id)!='text'
				OR ref_a.id IS NULL OR typeof(ref_a.id)!='text' OR typeof(ref_a.organisation_id)!='text'
				OR ref_a.organisation_id IS NOT target.organisation_id
			) LIMIT 1)`,
		args: append([]any{org}, batchArgs(journalIDs)...),
	}
}

// Opening-account discovery is limited to the selected conversion batch and
// validates each item's account parent before its account ID is copied.
func purchaseReportingOpeningAccountReferenceGuard(org string, conversionIDs []string) *postingReportingOwnerGuard {
	if len(conversionIDs) == 0 {
		return nil
	}
	values := strings.TrimSuffix(strings.Repeat("(?),", len(conversionIDs)), ",")
	itemAccounts := &postingReportingOwnerGuard{
		invalid: `EXISTS(WITH reference_conversions(id) AS (VALUES ` + values + `)
			SELECT 1 FROM reference_conversions AS selected
			JOIN opening_conversions AS ref_c ON ref_c.id=selected.id
			JOIN opening_items AS ref_i ON ref_i.conversion_id=ref_c.id
				OR (typeof(ref_i.conversion_id)='blob' AND ref_i.conversion_id=CAST(ref_c.id AS BLOB))
			LEFT JOIN accounts AS ref_a ON ref_a.id=ref_i.account_id
			WHERE typeof(ref_c.id)!='text' OR typeof(ref_i.id)!='text' OR typeof(ref_i.conversion_id)!='text'
				OR typeof(ref_i.account_id)!='text' OR ref_a.id IS NULL OR typeof(ref_a.id)!='text'
				OR typeof(ref_a.organisation_id)!='text' OR ref_a.organisation_id IS NOT ?
			LIMIT 1)`,
		args: append(batchArgs(conversionIDs), org),
	}
	return purchaseReportingCombineOwnerGuards(purchaseReportingConversionOwnerGuard(org, conversionIDs), itemAccounts)
}

// Fact selection is bounded by the already-discovered journal batch. BLOB
// reference arms only surface malformed links to those same parents.
func purchaseReportingJournalFactReferenceGuard(org string, journalIDs []string, table string) *postingReportingOwnerGuard {
	if len(journalIDs) == 0 {
		return nil
	}
	if table != "tax_facts" && table != "cash_flow_facts" {
		return &postingReportingOwnerGuard{invalid: "1"}
	}
	values := strings.TrimSuffix(strings.Repeat("(?),", len(journalIDs)), ",")
	return &postingReportingOwnerGuard{
		invalid: `EXISTS(WITH reference_target(organisation_id) AS (SELECT ?),
			reference_journals(id) AS (VALUES ` + values + `)
			SELECT 1 FROM (
			SELECT 1 FROM reference_journals AS selected
			LEFT JOIN journals AS ref_j ON ref_j.id=selected.id CROSS JOIN reference_target AS target
			WHERE ref_j.id IS NULL OR typeof(ref_j.organisation_id)!='text' OR ref_j.organisation_id IS NOT target.organisation_id
			UNION ALL
			SELECT 1 FROM reference_journals AS selected JOIN journals AS ref_j ON ref_j.id=selected.id
			JOIN journal_lines AS ref_l ON ref_l.journal_id=ref_j.id
				OR (typeof(ref_l.journal_id)='blob' AND ref_l.journal_id=CAST(ref_j.id AS BLOB))
			JOIN ` + table + ` AS ref_f ON ref_f.journal_line_id=ref_l.id
				OR (typeof(ref_f.journal_line_id)='blob' AND ref_f.journal_line_id=CAST(ref_l.id AS BLOB))
			CROSS JOIN reference_target AS target
			WHERE typeof(ref_l.id)!='text' OR typeof(ref_l.journal_id)!='text'
			OR typeof(ref_f.id)!='text' OR typeof(ref_f.journal_line_id)!='text'
			OR typeof(ref_f.organisation_id)!='text' OR ref_f.organisation_id IS NOT target.organisation_id
			) LIMIT 1)`,
		args: append([]any{org}, batchArgs(journalIDs)...),
	}
}

// Accounts stay scoped to the exact selected IDs and the existing four control
// codes. An unrelated foreign owner's control code does not enter this scope.
func purchaseReportingAccountReferenceGuard(org string, accountIDs []string, controls bool) *postingReportingOwnerGuard {
	var guards []*postingReportingOwnerGuard
	if len(accountIDs) > 0 {
		values := strings.TrimSuffix(strings.Repeat("(?),", len(accountIDs)), ",")
		guards = append(guards, &postingReportingOwnerGuard{
			invalid: `EXISTS(WITH reference_target(organisation_id) AS (SELECT ?),
				reference_accounts(id) AS (VALUES ` + values + `)
				SELECT 1 FROM reference_accounts AS selected LEFT JOIN accounts AS ref_a ON ref_a.id=selected.id
				CROSS JOIN reference_target AS target WHERE ref_a.id IS NULL OR typeof(ref_a.id)!='text'
				OR typeof(ref_a.organisation_id)!='text' OR ref_a.organisation_id IS NOT target.organisation_id)`,
			args: append([]any{org}, batchArgs(accountIDs)...),
		})
	}
	if controls {
		guards = append(guards, &postingReportingOwnerGuard{
			invalid: `EXISTS(SELECT 1 FROM accounts AS ref_a CROSS JOIN (SELECT ? AS organisation_id) AS target
				WHERE ref_a.code IN ('2000','1150','1160','1170')
				AND (ref_a.organisation_id=target.organisation_id OR
					(typeof(ref_a.organisation_id)='blob' AND length(ref_a.organisation_id)=36 AND ref_a.organisation_id=CAST(target.organisation_id AS BLOB)))
				AND (typeof(ref_a.id)!='text' OR length(CAST(ref_a.id AS BLOB))!=36 OR typeof(ref_a.organisation_id)!='text'))`,
			args: []any{org},
		})
	}
	return purchaseReportingCombineOwnerGuards(guards...)
}

func purchaseReportingJournalClosureReferenceGuard(org string, journalIDs []string) *postingReportingOwnerGuard {
	if len(journalIDs) == 0 {
		return nil
	}
	values := strings.TrimSuffix(strings.Repeat("(?),", len(journalIDs)), ",")
	return &postingReportingOwnerGuard{
		invalid: `EXISTS(WITH reference_target(organisation_id) AS (SELECT ?),
			reference_journals(id) AS (VALUES ` + values + `)
			SELECT 1 FROM (
			SELECT 1 FROM reference_journals AS selected
			LEFT JOIN journals AS ref_j ON ref_j.id=selected.id
				OR (typeof(ref_j.id)='blob' AND ref_j.id=CAST(selected.id AS BLOB))
			CROSS JOIN reference_target AS target
			WHERE ref_j.id IS NULL OR typeof(ref_j.id)!='text' OR ref_j.id=''
				OR typeof(ref_j.organisation_id)!='text' OR ref_j.organisation_id IS NOT target.organisation_id
				OR typeof(ref_j.source_type)!='text' OR typeof(ref_j.source_id)!='text' OR typeof(ref_j.state)!='text'
				OR typeof(ref_j.version)!='integer' OR ref_j.version<=0
				OR typeof(ref_j.source_revision)!='integer' OR ref_j.source_revision<0
				OR (ref_j.reversal_of_journal_id IS NOT NULL AND
					(typeof(ref_j.reversal_of_journal_id)!='text' OR ref_j.reversal_of_journal_id=''))
				OR (ref_j.reversed_by_journal_id IS NOT NULL AND
					(typeof(ref_j.reversed_by_journal_id)!='text' OR ref_j.reversed_by_journal_id=''))
				OR ((ref_j.source_type='REVERSAL') != (ref_j.reversal_of_journal_id IS NOT NULL))
				OR ((ref_j.state='REVERSED') != (ref_j.reversed_by_journal_id IS NOT NULL))

			UNION ALL
			SELECT 1 FROM reference_journals AS selected
			JOIN journals AS ref_j ON ref_j.id=selected.id
			LEFT JOIN journals AS ref_parent ON ref_parent.id=ref_j.reversal_of_journal_id
				OR (typeof(ref_parent.id)='blob' AND ref_parent.id=CAST(ref_j.reversal_of_journal_id AS BLOB))
			CROSS JOIN reference_target AS target
			WHERE ref_j.reversal_of_journal_id IS NOT NULL AND (
				ref_parent.id IS NULL OR typeof(ref_parent.id)!='text'
				OR typeof(ref_parent.organisation_id)!='text' OR ref_parent.organisation_id IS NOT target.organisation_id
				OR typeof(ref_parent.version)!='integer' OR ref_parent.version<2
				OR typeof(ref_parent.state)!='text' OR ref_parent.state!='REVERSED'
				OR typeof(ref_parent.reversed_by_journal_id)!='text' OR ref_parent.reversed_by_journal_id IS NOT ref_j.id
				OR typeof(ref_j.source_type)!='text' OR ref_j.source_type!='REVERSAL'
				OR typeof(ref_j.source_id)!='text' OR ref_j.source_id IS NOT ref_parent.id
				OR typeof(ref_j.source_revision)!='integer' OR ref_j.source_revision!=ref_parent.version-1)

			UNION ALL
			SELECT 1 FROM reference_journals AS selected
			JOIN journals AS ref_j ON ref_j.id=selected.id
			LEFT JOIN journals AS ref_child ON ref_child.id=ref_j.reversed_by_journal_id
				OR (typeof(ref_child.id)='blob' AND ref_child.id=CAST(ref_j.reversed_by_journal_id AS BLOB))
			CROSS JOIN reference_target AS target
			WHERE ref_j.reversed_by_journal_id IS NOT NULL AND (
				ref_child.id IS NULL OR typeof(ref_child.id)!='text'
				OR typeof(ref_child.organisation_id)!='text' OR ref_child.organisation_id IS NOT target.organisation_id
				OR typeof(ref_child.reversal_of_journal_id)!='text' OR ref_child.reversal_of_journal_id IS NOT ref_j.id
				OR typeof(ref_child.source_type)!='text' OR ref_child.source_type!='REVERSAL'
				OR typeof(ref_child.source_id)!='text' OR ref_child.source_id IS NOT ref_j.id
				OR typeof(ref_j.version)!='integer' OR ref_j.version<2
				OR typeof(ref_child.source_revision)!='integer' OR ref_child.source_revision!=ref_j.version-1
				OR typeof(ref_child.state)!='text' OR ref_child.state NOT IN ('POSTED','REVERSED'))
			) LIMIT 1)`,
		args: append([]any{org}, batchArgs(journalIDs)...),
	}
}

// After the bounded SQL preflight succeeds, revalidate the exact reserved
// header count and byte total inside the masked body statement. This catches
// same-transaction drift before database/sql can copy a changed TEXT/BLOB link.
func purchaseReportingJournalClosureBodyReservationGuard(query postingReportingQuery, reservedBytes, reservedCount int64) *postingReportingOwnerGuard {
	if query.from == "" || query.where == "" || len(query.columns) == 0 || reservedBytes < 0 || reservedCount < 0 {
		return &postingReportingOwnerGuard{invalid: "1"}
	}
	sizeSQL := postingReportingPayloadSizeSQL(query.alias, query.columns)
	return &postingReportingOwnerGuard{
		invalid: `((SELECT count(*) FROM ` + query.from + ` WHERE ` + query.where + `)!=?) OR
			((SELECT total(payload_bytes) FROM (SELECT ` + sizeSQL + ` AS payload_bytes FROM ` + query.from + ` WHERE ` + query.where + `))!=?)`,
		args: append(append(append(append([]any(nil), query.args...), reservedCount), query.args...), reservedBytes),
	}
}
