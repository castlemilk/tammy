package purchases

import "strings"

func purchaseReportingPostingAllocationOwnerGuard(organisationID string) *postingReportingOwnerGuard {
	return &postingReportingOwnerGuard{
		invalid: `EXISTS(SELECT 1 FROM purchase_posting_allocations_v1 AS owner_allocation
			WHERE typeof(owner_allocation.organisation_id)='blob' AND length(owner_allocation.organisation_id)=36
			AND owner_allocation.organisation_id=CAST(? AS BLOB))`,
		args: []any{organisationID},
	}
}

func purchaseReportingPostingAllocationReplayQuery(organisationID, postingID string) postingReportingQuery {
	return postingReportingQuery{
		table: "posting allocation replay", alias: "a",
		columns: strings.Fields("sequence account_id account_version gross_minor supplier_gst_minor eligible_gst_minor use_kind bas_classification cash_flow_classification noncreditable_reason allocation_proto"),
		from:    "purchase_posting_allocations_v1 AS a", where: "a.posting_id=? AND a.organisation_id=?",
		args: []any{postingID, organisationID}, orderBy: "a.sequence",
		ownerGuard: purchaseReportingPostingAllocationOwnerGuard(organisationID),
	}
}
