package purchases

import "strings"

// These closed sets add rejection probes only. Each reader retains its own
// TEXT selection, payload definition and ordering.
type purchaseReportingOwnerSet uint16

const (
	purchaseOwnerPosting purchaseReportingOwnerSet = 1 << iota
	purchaseOwnerPostingAllocation
	purchaseOwnerPayment
	purchaseOwnerPaymentAllocation
	purchaseOwnerEvidence
	purchaseOwnerPostingEvent
	purchaseOwnerDeferral
	purchaseOwnerDeferralPortion
	purchaseOwnerAttribution
	purchaseOwnerCoverageEvents
)

const (
	purchaseOwnersPaymentAllocation = purchaseOwnerPosting | purchaseOwnerPostingAllocation | purchaseOwnerPayment | purchaseOwnerPaymentAllocation
	purchaseOwnersEvidence          = purchaseOwnerPosting | purchaseOwnerEvidence
	purchaseOwnersPostingEvent      = purchaseOwnerPosting | purchaseOwnerPostingEvent
	purchaseOwnersDeferral          = purchaseOwnerPosting | purchaseOwnerEvidence | purchaseOwnerPostingEvent | purchaseOwnerDeferral
	purchaseOwnersDeferralPortion   = purchaseOwnersPaymentAllocation | purchaseOwnersDeferral | purchaseOwnerDeferralPortion
	purchaseOwnersAttribution       = purchaseOwnersDeferralPortion | purchaseOwnerAttribution
)

func purchaseReportingOwnerGuardForSet(org string, owners purchaseReportingOwnerSet) *postingReportingOwnerGuard {
	guard := &postingReportingOwnerGuard{}
	parts := make([]string, 0, 18)
	appendGuard := func(other *postingReportingOwnerGuard) {
		parts = append(parts, other.invalid)
		guard.args = append(guard.args, other.args...)
	}
	if owners&purchaseOwnerPosting != 0 {
		appendGuard(purchaseReportingRootOwnerGuard(org, false))
	}
	if owners&purchaseOwnerPostingAllocation != 0 {
		appendGuard(purchaseReportingPostingAllocationOwnerGuard(org))
	}
	if owners&purchaseOwnerPayment != 0 {
		appendGuard(purchaseReportingPaymentOwnerGuard(org))
	}
	for _, family := range []struct {
		bit   purchaseReportingOwnerSet
		from  string
		scope string
	}{
		{purchaseOwnerPaymentAllocation, "purchase_payment_allocations_v1", ""},
		{purchaseOwnerEvidence, "purchase_tax_evidence_v1", ""},
		{purchaseOwnerPostingEvent, "purchase_accounting_events_v1", " AND owner_modern.scope_kind='POSTING'"},
		{purchaseOwnerDeferral, "purchase_gst_deferrals_v1", ""},
		{purchaseOwnerDeferralPortion, "purchase_gst_deferral_portions_v1", ""},
		{purchaseOwnerAttribution, "purchase_gst_attributions_v1", ""},
		{purchaseOwnerCoverageEvents, "purchase_accounting_events_v1", ""},
	} {
		if owners&family.bit == 0 {
			continue
		}
		appendGuard(&postingReportingOwnerGuard{
			invalid: `EXISTS(SELECT 1 FROM ` + family.from + ` AS owner_modern
				WHERE typeof(owner_modern.organisation_id)='blob' AND length(owner_modern.organisation_id)=36
				AND owner_modern.organisation_id=CAST(? AS BLOB)` + family.scope + `)`,
			args: []any{org},
		})
	}
	for _, family := range []struct {
		bit       purchaseReportingOwnerSet
		kind      BASScalarKeyKind
		allEvents bool
	}{
		{purchaseOwnerPaymentAllocation, BASScalarPaymentAllocation, false},
		{purchaseOwnerEvidence, BASScalarEvidence, false},
		{purchaseOwnerPostingEvent, BASScalarEvent, false},
		{purchaseOwnerDeferral, BASScalarDeferral, false},
		{purchaseOwnerDeferralPortion, BASScalarDeferralPortion, false},
		{purchaseOwnerAttribution, BASScalarAttribution, false},
		{purchaseOwnerCoverageEvents, BASScalarEvent, true},
	} {
		if owners&family.bit != 0 {
			appendGuard(purchaseReportingModernFamilyReferenceGuard(org, family.kind, family.allEvents))
		}
	}
	guard.invalid = strings.Join(parts, " OR ")
	return guard
}

// Audit is coverage-only and is scoped to the same selected operation batch as
// the companion rows. Revision claims and idempotency have no organisation owner.
func purchaseReportingCoverageOperationOwnerGuard(org string, operationKeys []string) *postingReportingOwnerGuard {
	if len(operationKeys) == 0 {
		return nil
	}
	placeholders := purchaseCoveragePlaceholders(len(operationKeys))
	guard := &postingReportingOwnerGuard{
		invalid: `EXISTS(SELECT 1 FROM purchase_command_audit_v1 AS owner_audit
			WHERE typeof(owner_audit.organisation_id)='blob' AND length(owner_audit.organisation_id)=36
			AND owner_audit.organisation_id=CAST(? AS BLOB) AND owner_audit.operation_key IN (` + placeholders + `))
			OR EXISTS(SELECT 1 FROM purchase_accounting_events_v1 AS owner_event
			WHERE typeof(owner_event.organisation_id)='blob' AND length(owner_event.organisation_id)=36
			AND owner_event.organisation_id=CAST(? AS BLOB) AND owner_event.operation_key IN (` + placeholders + `))`,
		args: []any{org},
	}
	guard.args = append(guard.args, batchArgs(operationKeys)...)
	guard.args = append(guard.args, org)
	guard.args = append(guard.args, batchArgs(operationKeys)...)
	return guard
}

func purchaseReportingPaymentAllocationReplayQuery(org, postingID, paymentID string) postingReportingQuery {
	return postingReportingQuery{
		table: "payment allocation replay", alias: "a",
		columns: strings.Fields("sequence posting_allocation_sequence gross_minor supplier_gst_minor eligible_gst_minor use_kind bas_classification cash_flow_classification allocation_proto"),
		from:    "purchase_payment_allocations_v1 AS a", where: "a.payment_id=? AND a.posting_id=? AND a.organisation_id=?",
		args: []any{paymentID, postingID, org}, orderBy: "a.sequence",
		ownerGuard: purchaseReportingOwnerGuardForSet(org, purchaseOwnersPaymentAllocation),
	}
}

func purchaseReportingEvidenceReplayQuery(org, postingID, documentID string) postingReportingQuery {
	return postingReportingQuery{
		table: "evidence replay", alias: "e",
		columns: strings.Fields("id accounting_version classification source_document_id source_version source_hash held_date registration_review_confirmed business_use_review_confirmed evidence_review_confirmed reason evidence_proto record_hash actor_user_id created_at"),
		from:    "purchase_tax_evidence_v1 AS e", where: "e.organisation_id=? AND e.posting_id=? AND e.document_id=?",
		args: []any{org, postingID, documentID}, orderBy: "e.accounting_version,e.id",
		ownerGuard: purchaseReportingOwnerGuardForSet(org, purchaseOwnersEvidence),
	}
}

func purchaseReportingEventReplayQuery(org, postingID string, initial bool) postingReportingQuery {
	query := postingReportingQuery{
		table: "payment event replay", alias: "e",
		columns: strings.Fields("operation_key posting_id document_id opening_payable_id accounting_version command_type financial_revision ledger_revision tax_source_revision settlement_revision result_proto result_sha256 journal_id created_at"),
		from:    "purchase_accounting_events_v1 AS e", where: "e.organisation_id=? AND e.scope_kind='POSTING' AND e.scope_id=? AND e.accounting_version>1",
		args: []any{org, postingID}, orderBy: "e.accounting_version",
		ownerGuard: purchaseReportingOwnerGuardForSet(org, purchaseOwnersPostingEvent),
	}
	if initial {
		query.table = "posting event replay"
		query.where = "e.organisation_id=? AND e.scope_kind='POSTING' AND e.scope_id=? AND e.accounting_version=1"
	}
	return query
}

func purchaseReportingDeferralReplayQuery(org, postingID string) postingReportingQuery {
	return postingReportingQuery{
		table: "deferral replay", alias: "d", columns: strings.Fields("deferral_proto source_hash"),
		from: "purchase_gst_deferrals_v1 AS d", where: "d.posting_id=? AND d.organisation_id=?",
		args: []any{postingID, org}, orderBy: "d.accounting_version,d.rowid",
		ownerGuard: purchaseReportingOwnerGuardForSet(org, purchaseOwnersDeferral),
	}
}

func purchaseReportingDeferralPortionReplayQuery(org, postingID, deferralID string) postingReportingQuery {
	return postingReportingQuery{
		table: "deferral portion replay", alias: "p", columns: strings.Fields("sequence deferred_gst_minor"),
		from: "purchase_gst_deferral_portions_v1 AS p", where: "p.deferral_id=? AND p.posting_id=? AND p.organisation_id=?",
		args: []any{deferralID, postingID, org}, orderBy: "p.sequence",
		ownerGuard: purchaseReportingOwnerGuardForSet(org, purchaseOwnersDeferralPortion),
	}
}

func purchaseReportingAttributionReplayQuery(org, postingID string) postingReportingQuery {
	return postingReportingQuery{
		table: "attribution replay", alias: "a", columns: strings.Fields("operation_key attribution_proto source_hash"),
		from: "purchase_gst_attributions_v1 AS a", where: "a.posting_id=? AND a.organisation_id=?",
		args: []any{postingID, org}, orderBy: "a.accounting_version,a.rowid",
		ownerGuard: purchaseReportingOwnerGuardForSet(org, purchaseOwnersAttribution),
	}
}
