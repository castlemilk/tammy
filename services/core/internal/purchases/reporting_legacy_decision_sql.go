package purchases

import "strings"

const purchaseBASLegacyDecisionOrderSQL = "source_kind,source_id,accounting_version,id"

func purchaseBASLegacyDecisionCTE() string {
	invalid := []string{
		"owner.id IS NULL OR r.organisation_id IS NOT target.organisation_id OR actor.id IS NULL",
		"r.source_kind NOT IN ('DOCUMENT','OPENING_PAYABLE')",
		"typeof(r.review_proto)!='blob' OR length(r.review_proto) NOT BETWEEN 1 AND 131072",
		"typeof(r.disposition)!='integer' OR r.disposition NOT BETWEEN 1 AND 6",
		"(r.previously_claimed_period_start IS NULL)!=(r.previously_claimed_period_end IS NULL)",
		"(r.claim_evidence_source_id IS NULL)!=(r.claim_evidence_source_version IS NULL)",
		"(r.claim_evidence_source_id IS NULL)!=(r.claim_evidence_source_hash IS NULL)",
		`r.source_kind='DOCUMENT' AND (r.document_id IS NULL OR r.opening_payable_id IS NOT NULL
		 OR r.reviewed_snapshot_hash IS NULL OR source_document.id IS NULL
		 OR source_document.organisation_id IS NOT r.organisation_id)`,
		`r.source_kind='OPENING_PAYABLE' AND (r.opening_payable_id IS NULL OR r.document_id IS NOT NULL
		 OR r.reviewed_snapshot_hash IS NOT NULL OR source_payable.id IS NULL OR source_conversion.id IS NULL
		 OR source_conversion.organisation_id IS NOT r.organisation_id)`,
		`r.claim_evidence_source_id IS NOT NULL AND (claim_document.id IS NULL
		 OR claim_document.organisation_id IS NOT r.organisation_id)`,
		`r.linked_posting_id IS NOT NULL AND (linked_posting.id IS NULL
		 OR linked_posting.organisation_id IS NOT r.organisation_id)`,
	}
	for _, column := range []string{"id", "organisation_id", "actor_user_id"} {
		invalid = append(invalid, "typeof(r."+column+")!='text' OR length(r."+column+") NOT BETWEEN 1 AND 128")
	}
	for _, column := range []string{"document_id", "opening_payable_id", "claim_evidence_source_id", "linked_posting_id"} {
		invalid = append(invalid, "r."+column+" IS NOT NULL AND (typeof(r."+column+")!='text' OR length(r."+column+") NOT BETWEEN 1 AND 128)")
	}
	for _, column := range []string{"affected_period_start", "affected_period_end", "reporting_balance_date"} {
		invalid = append(invalid, "typeof(r."+column+")!='text' OR length(r."+column+")!=10")
	}
	for _, column := range []string{"previously_claimed_period_start", "previously_claimed_period_end"} {
		invalid = append(invalid, "r."+column+" IS NOT NULL AND (typeof(r."+column+")!='text' OR length(r."+column+")!=10)")
	}
	invalid = append(invalid,
		"typeof(r.source_kind)!='text'",
		"typeof(r.reason)!='text' OR length(r.reason) NOT BETWEEN 1 AND 512",
		"typeof(r.created_at)!='text' OR length(r.created_at) NOT BETWEEN 1 AND 64")
	for _, column := range []string{"source_version", "accounting_version"} {
		invalid = append(invalid, "typeof(r."+column+")!='integer' OR r."+column+"<1")
	}
	invalid = append(invalid, "r.claim_evidence_source_version IS NOT NULL AND (typeof(r.claim_evidence_source_version)!='integer' OR r.claim_evidence_source_version<1)")
	for _, column := range []string{"source_hash", "record_hash"} {
		invalid = append(invalid, "typeof(r."+column+")!='blob' OR length(r."+column+")!=32")
	}
	for _, column := range []string{"reviewed_snapshot_hash", "claim_evidence_source_hash"} {
		invalid = append(invalid, "r."+column+" IS NOT NULL AND (typeof(r."+column+")!='blob' OR length(r."+column+")!=32)")
	}
	for index := range invalid {
		invalid[index] = "(" + invalid[index] + ")"
	}
	// Include a same-byte BLOB owner only so the existing invalid-owner checks
	// reject it; TEXT equality alone would silently omit it before validation.
	return `WITH requested AS (SELECT ? AS organisation_id), decision_rows AS (
	 SELECT r.id,r.organisation_id,r.source_kind,COALESCE(r.document_id,r.opening_payable_id) AS source_id,
	        r.accounting_version,r.review_proto,r.record_hash,
	        512+coalesce(length(CAST(r.id AS BLOB)),0)+coalesce(length(CAST(r.organisation_id AS BLOB)),0)
	        +coalesce(length(CAST(r.source_kind AS BLOB)),0)
	        +coalesce(length(CAST(COALESCE(r.document_id,r.opening_payable_id) AS BLOB)),0)
	        +coalesce(length(r.review_proto),0)+coalesce(length(r.record_hash),0) AS decision_bytes,
	        CASE WHEN ` + strings.Join(invalid, " OR ") + ` THEN 1 ELSE 0 END AS invalid_decision
	 FROM purchase_legacy_reviews_v1 AS r CROSS JOIN requested AS target
	 LEFT JOIN organisations AS owner ON owner.id=r.organisation_id
	 LEFT JOIN users AS actor ON actor.id=r.actor_user_id
	 LEFT JOIN documents AS source_document ON source_document.id=r.document_id
	 LEFT JOIN purchase_opening_payables AS source_payable ON source_payable.id=r.opening_payable_id
	 LEFT JOIN opening_conversions AS source_conversion ON source_conversion.id=source_payable.conversion_id
	 LEFT JOIN documents AS claim_document ON claim_document.id=r.claim_evidence_source_id
	 LEFT JOIN purchase_postings_v1 AS linked_posting ON linked_posting.id=r.linked_posting_id
	 WHERE r.organisation_id=target.organisation_id
	    OR (typeof(r.organisation_id)='blob' AND length(r.organisation_id)=36
	        AND r.organisation_id=CAST(target.organisation_id AS BLOB))
	    OR source_document.organisation_id=target.organisation_id
	    OR source_conversion.organisation_id=target.organisation_id OR claim_document.organisation_id=target.organisation_id
	    OR linked_posting.organisation_id=target.organisation_id
	), source_rows AS (
	 SELECT *,count(*) OVER (PARTITION BY source_kind,source_id) AS source_record_count FROM decision_rows
	)`
}
