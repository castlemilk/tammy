package purchases

import "strings"

// Only payable-backed posted/replaced headers seed purchase opening roots.
// Unlinked drafts and ordinary conversions retain their existing scope.
func purchaseReportingOpeningRootOwnerGuard(org string) *postingReportingOwnerGuard {
	return &postingReportingOwnerGuard{
		invalid: `EXISTS(SELECT 1 FROM purchase_opening_payables owner_p
			JOIN opening_conversions owner_c ON owner_c.id=owner_p.conversion_id
			WHERE typeof(owner_c.organisation_id)='blob' AND length(owner_c.organisation_id)=36
			AND owner_c.organisation_id=CAST(? AS BLOB) AND owner_c.state IN ('POSTED','REPLACED'))`,
		args: []any{org},
	}
}

func purchaseReportingCoverageRootOwnerGuard(org string) *postingReportingOwnerGuard {
	guard := purchaseReportingRootOwnerGuard(org, true)
	opening := purchaseReportingOpeningRootOwnerGuard(org)
	guard.invalid += " OR " + opening.invalid
	guard.args = append(guard.args, opening.args...)
	return guard
}

// Every reserved header must still exist as an owned TEXT row. This only
// examines the exact closure batch; it does not widen conversion discovery.
func purchaseReportingConversionOwnerGuard(org string, ids []string) *postingReportingOwnerGuard {
	return &postingReportingOwnerGuard{
		invalid: `(SELECT count(*) FROM opening_conversions owner_c
			WHERE owner_c.organisation_id=? AND typeof(owner_c.organisation_id)='text'
			AND owner_c.id IN (` + purchaseCoveragePlaceholders(len(ids)) + `))!=?`,
		args: append(append([]any{org}, batchArgs(ids)...), len(ids)),
	}
}

func purchaseReportingLegacyReviewOwnerGuard(org string) *postingReportingOwnerGuard {
	return &postingReportingOwnerGuard{
		invalid: `EXISTS(SELECT 1 FROM purchase_legacy_reviews_v1 owner_review
			WHERE typeof(owner_review.organisation_id)='blob' AND length(owner_review.organisation_id)=36
			AND owner_review.organisation_id=CAST(? AS BLOB))`,
		args: []any{org},
	}
}

func purchaseReportingLegacyReplayQuery(org, kind, id string) postingReportingQuery {
	return postingReportingQuery{
		table: "legacy review replay", alias: "r", columns: strings.Fields("id review_proto record_hash"),
		from:  "purchase_legacy_reviews_v1 AS r",
		where: "r.organisation_id=? AND r.source_kind=? AND COALESCE(r.document_id,r.opening_payable_id)=?",
		args:  []any{org, kind, id}, orderBy: "r.accounting_version",
		ownerGuard: purchaseReportingLegacyReviewOwnerGuard(org),
	}
}

func purchaseReportingLegacyPriorReplayQuery(org, documentID string, hash []byte) postingReportingQuery {
	return postingReportingQuery{
		table: "legacy prior review replay", alias: "r", columns: strings.Fields("id review_proto record_hash"),
		from: "purchase_legacy_reviews_v1 AS r", where: "r.organisation_id=? AND (r.document_id=? OR r.source_hash=?)",
		args: []any{org, documentID, hash}, orderBy: "r.id",
		ownerGuard: purchaseReportingLegacyReviewOwnerGuard(org),
	}
}

func purchaseReportingLegacyEventReplayQuery(org, kind, id string) postingReportingQuery {
	guard := purchaseReportingLegacyReviewOwnerGuard(org)
	events := purchaseReportingOwnerGuardForSet(org, purchaseOwnerCoverageEvents)
	guard.invalid += " OR " + events.invalid
	guard.args = append(guard.args, events.args...)
	return postingReportingQuery{
		table: "legacy event replay", alias: "r",
		columns: strings.Fields("operation_key accounting_version command_type result_proto result_sha256 created_at posting_id document_id opening_payable_id journal_id financial_revision ledger_revision tax_source_revision settlement_revision claim_domain_mask claim_financial_revision claim_ledger_revision claim_tax_source_revision claim_settlement_revision"),
		from: `(SELECT e.operation_key,e.accounting_version,e.command_type,e.result_proto,e.result_sha256,e.created_at,
			e.posting_id,e.document_id,e.opening_payable_id,e.journal_id,e.financial_revision,e.ledger_revision,e.tax_source_revision,e.settlement_revision,
			c.domain_mask AS claim_domain_mask,c.financial_revision AS claim_financial_revision,c.ledger_revision AS claim_ledger_revision,
			c.tax_source_revision AS claim_tax_source_revision,c.settlement_revision AS claim_settlement_revision
			FROM purchase_accounting_events_v1 e LEFT JOIN financial_revision_claims c ON c.operation_key=e.operation_key
			WHERE e.organisation_id=? AND e.scope_kind=? AND e.scope_id=?) AS r`,
		where: "1=1", args: []any{org, kind, id}, orderBy: "r.accounting_version", ownerGuard: guard,
	}
}

// Explicit aliases preserve the mature 24/29-cell decoder, including duplicate
// IDs from joined tables, while the outer read can withhold every body cell.
func purchaseReportingOpeningReplayQuery(org, payableID string, prior bool) postingReportingQuery {
	projection := `p.id,p.conversion_id,p.retained_input_proto,c.version,c.conversion_date,
		i.id AS item_id,i.account_id,i.item_kind,i.debit_minor,i.credit_minor,i.currency_code,
		i.source_type,i.source_id,i.source_revision,i.source_content_hash,
		i.original_issue_date,i.original_due_date,i.outstanding_gst_minor,i.prior_gst_attributed_minor,
		i.latest_statement_date,i.latest_statement_balance_minor,
		a.organisation_id AS account_organisation_id,a.normal_balance,a.report_classification`
	columns := strings.Fields("id conversion_id retained_input_proto version conversion_date item_id account_id item_kind debit_minor credit_minor currency_code source_type source_id source_revision source_content_hash original_issue_date original_due_date outstanding_gst_minor prior_gst_attributed_minor latest_statement_date latest_statement_balance_minor account_organisation_id normal_balance report_classification")
	guard := purchaseReportingOpeningRootOwnerGuard(org)
	table := "opening prior replay"
	where := "c.organisation_id=? AND c.state IN ('POSTED','REPLACED')"
	args := []any{org}
	if !prior {
		table = "opening payable replay"
		projection += ",c.state,c.journal_id,c.financial_revision,c.source_sha256,c.replaced_by_id"
		columns = append(columns, strings.Fields("state journal_id financial_revision source_sha256 replaced_by_id")...)
		where += " AND (?='' OR p.id=?)"
		args = append(args, payableID, payableID)
		if payableID != "" {
			// The API receives a payable ID; resolve it through its conversion.
			guard.invalid += ` OR EXISTS(SELECT 1 FROM purchase_opening_payables owner_p
				LEFT JOIN opening_conversions owner_c ON owner_c.id=owner_p.conversion_id
				WHERE owner_p.id=? AND (owner_c.id IS NULL OR owner_c.organisation_id IS NOT ?))`
			guard.args = append(guard.args, payableID, org)
		}
	}
	return postingReportingQuery{
		table: table, alias: "r", columns: columns,
		from: `(SELECT ` + projection + ` FROM purchase_opening_payables p
			JOIN opening_conversions c ON c.id=p.conversion_id
			LEFT JOIN opening_items i ON i.id=p.id AND i.conversion_id=p.conversion_id
			LEFT JOIN accounts a ON a.id=i.account_id WHERE ` + where + `) AS r`,
		where: "1=1", args: args, orderBy: "r.id", ownerGuard: guard,
	}
}
