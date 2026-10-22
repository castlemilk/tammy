package purchases

// purchaseBASScalarContextSQL is appended after:
//
//	WITH RECURSIVE requested AS (SELECT ? AS organisation_id),
//
// The replacement CTE is deliberately a bounded, bidirectional identity
// closure. It does not prove acyclicity or replacement-chain correctness;
// the existing full opening reader/preflight remains responsible for that.
// The aggregate invalid_links CTE must be included in ROOT's invalid count,
// including when the requested organisation has no scalar family rows.
const purchaseBASScalarContextSQL = `
purchase_bas_opening_roots(opening_payable_id,conversion_id,organisation_id) AS (
	SELECT p.id,p.conversion_id,c.organisation_id
	FROM purchase_opening_payables AS p
	LEFT JOIN opening_conversions AS c ON c.id=p.conversion_id
	CROSS JOIN requested AS target
	WHERE c.organisation_id=target.organisation_id
	  AND c.state IN ('POSTED','REPLACED')
),
purchase_bas_opening_closure(id,organisation_id) AS (
	SELECT conversion_id,organisation_id FROM purchase_bas_opening_roots
	UNION
	SELECT candidate.id,candidate.organisation_id
	FROM purchase_bas_opening_closure AS current
	JOIN opening_conversions AS present ON present.id=current.id
	JOIN opening_conversions AS candidate
	  ON candidate.id=present.replaced_by_id OR candidate.replaced_by_id=present.id
	LIMIT 16385
),
purchase_bas_source_ids(id) AS (
	SELECT d.id
	FROM documents AS d CROSS JOIN requested AS target
	WHERE d.organisation_id=target.organisation_id
	UNION
	SELECT opening_payable_id FROM purchase_bas_opening_roots
),
purchase_bas_invalid_links(invalid_count) AS (
	SELECT
	  (SELECT count(*) FROM purchase_opening_payables AS owner_p
	   JOIN opening_conversions AS owner_c ON owner_c.id=owner_p.conversion_id
	   CROSS JOIN requested AS target
	   WHERE typeof(owner_c.organisation_id)='blob' AND length(owner_c.organisation_id)=36
	     AND owner_c.organisation_id=CAST(target.organisation_id AS BLOB)
	     AND owner_c.state IN ('POSTED','REPLACED'))
	+
	  (SELECT count(*)
	   FROM purchase_opening_payables AS p
	   LEFT JOIN opening_conversions AS c ON c.id=p.conversion_id
	   LEFT JOIN opening_items AS i ON i.id=p.id AND i.conversion_id=p.conversion_id
	   CROSS JOIN requested AS target
	   WHERE c.id IS NULL OR (c.organisation_id=target.organisation_id
	     AND c.state IN ('POSTED','REPLACED') AND i.id IS NULL))
	+ (SELECT count(*)
	   FROM purchase_opening_payables AS p
	   JOIN opening_conversions AS c ON c.id=p.conversion_id
	   CROSS JOIN requested AS target
	   WHERE c.organisation_id=target.organisation_id
	     AND (c.state IS NULL OR c.state NOT IN ('DRAFT','POSTED','REPLACED')))
	+ (SELECT count(*)
	   FROM opening_items AS i
	   LEFT JOIN opening_conversions AS c ON c.id=i.conversion_id
	   WHERE c.id IS NULL)
	+ (SELECT count(*)
	   FROM opening_conversions AS c
	   JOIN purchase_bas_opening_closure AS closure ON closure.id=c.id
	   LEFT JOIN opening_conversions AS next ON next.id=c.replaced_by_id
	   WHERE c.replaced_by_id IS NOT NULL
	     AND (next.id IS NULL OR next.organisation_id IS NOT c.organisation_id))
	+ (SELECT count(*)
	   FROM opening_conversions AS previous
	   JOIN purchase_bas_opening_closure AS closure ON closure.id=previous.replaced_by_id
	   WHERE previous.organisation_id IS NOT closure.organisation_id)
	+ (SELECT count(*)
	   FROM tax_facts AS f
	   CROSS JOIN requested AS target
	   LEFT JOIN organisations AS fact_owner ON fact_owner.id=f.organisation_id
	   LEFT JOIN journal_lines AS line ON line.id=f.journal_line_id
	   LEFT JOIN journals AS journal ON journal.id=line.journal_id
	   WHERE f.source_id IN (SELECT id FROM purchase_bas_source_ids)
	     AND (f.organisation_id IS NOT target.organisation_id
	       OR fact_owner.id IS NULL OR line.id IS NULL OR journal.id IS NULL
	       OR journal.organisation_id IS NOT f.organisation_id))
)
`

func purchaseBASLegacyFamilies() []purchaseBASScalarFamily {
	return []purchaseBASScalarFamily{
		{
			kind: BASScalarDocument,
			columns: map[string]string{
				"organisation_id": "d.organisation_id",
				"id":              "d.id",
				"object_version":  "d.version",
				"source_hash":     "d.sha256",
			},
			required: []string{"organisation_id", "id", "object_version", "source_hash"},
			from: `documents AS d
			LEFT JOIN organisations AS owner ON owner.id=d.organisation_id`,
			where: `d.organisation_id=target.organisation_id
				OR (typeof(d.organisation_id)='blob' AND length(d.organisation_id)=36
				  AND d.organisation_id=CAST(target.organisation_id AS BLOB))`,
			invalid: `owner.id IS NULL OR d.organisation_id IS NOT target.organisation_id
				OR d.id IS NULL OR d.id=''
				OR typeof(d.version)!='integer' OR d.version<1
				OR typeof(d.sha256)!='blob' OR length(d.sha256)!=32`,
			root: `CASE WHEN NOT EXISTS (
				SELECT 1 FROM purchase_postings_v1 AS posting
				WHERE posting.organisation_id=d.organisation_id AND posting.document_id=d.id
			) THEN 1 ELSE 0 END`,
			receipt: `0`,
		},
		{
			kind: BASScalarLegacyReview,
			columns: map[string]string{
				"organisation_id":               "r.organisation_id",
				"id":                            "r.id",
				"document_id":                   "r.document_id",
				"opening_payable_id":            "r.opening_payable_id",
				"source_kind":                   "r.source_kind",
				"source_id":                     "COALESCE(r.document_id,r.opening_payable_id)",
				"accounting_version":            "r.accounting_version",
				"source_version":                "r.source_version",
				"source_hash":                   "r.source_hash",
				"record_hash":                   "r.record_hash",
				"reviewed_snapshot_hash":        "r.reviewed_snapshot_hash",
				"claim_evidence_source_id":      "r.claim_evidence_source_id",
				"claim_evidence_source_version": "r.claim_evidence_source_version",
				"claim_evidence_source_hash":    "r.claim_evidence_source_hash",
				"posting_id":                    "r.linked_posting_id",
			},
			required: []string{"organisation_id", "id", "source_kind", "source_id", "accounting_version", "source_version", "source_hash", "record_hash"},
			from: `purchase_legacy_reviews_v1 AS r
			LEFT JOIN organisations AS owner ON owner.id=r.organisation_id
			LEFT JOIN documents AS source_document ON source_document.id=r.document_id
			LEFT JOIN purchase_opening_payables AS source_payable ON source_payable.id=r.opening_payable_id
			LEFT JOIN opening_conversions AS source_conversion ON source_conversion.id=source_payable.conversion_id
			LEFT JOIN documents AS claim_document ON claim_document.id=r.claim_evidence_source_id
			LEFT JOIN purchase_postings_v1 AS linked_posting ON linked_posting.id=r.linked_posting_id`,
			where: `r.organisation_id=target.organisation_id
				OR (typeof(r.organisation_id)='blob' AND length(r.organisation_id)=36
				  AND r.organisation_id=CAST(target.organisation_id AS BLOB))
				OR source_document.organisation_id=target.organisation_id
				OR source_conversion.organisation_id=target.organisation_id
				OR claim_document.organisation_id=target.organisation_id
				OR linked_posting.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR r.organisation_id IS NOT target.organisation_id
				OR r.id IS NULL OR r.id=''
				OR r.source_kind IS NULL OR r.source_kind NOT IN ('DOCUMENT','OPENING_PAYABLE')
				OR r.source_version<1 OR r.accounting_version<1
				OR typeof(r.source_version)!='integer' OR typeof(r.accounting_version)!='integer'
				OR typeof(r.source_hash)!='blob' OR length(r.source_hash)!=32
				OR typeof(r.record_hash)!='blob' OR length(r.record_hash)!=32
				OR (r.reviewed_snapshot_hash IS NOT NULL AND
					(typeof(r.reviewed_snapshot_hash)!='blob' OR length(r.reviewed_snapshot_hash)!=32))
				OR (r.source_kind='DOCUMENT' AND
					(r.document_id IS NULL OR r.opening_payable_id IS NOT NULL OR r.reviewed_snapshot_hash IS NULL
					 OR source_document.id IS NULL OR source_document.organisation_id IS NOT r.organisation_id
					))
				OR (r.source_kind='OPENING_PAYABLE' AND
					(r.opening_payable_id IS NULL OR r.document_id IS NOT NULL OR r.reviewed_snapshot_hash IS NOT NULL
					 OR source_payable.id IS NULL OR source_conversion.id IS NULL
					 OR source_conversion.organisation_id IS NOT r.organisation_id
					))
				OR ((r.claim_evidence_source_id IS NULL)!=(r.claim_evidence_source_version IS NULL))
				OR ((r.claim_evidence_source_id IS NULL)!=(r.claim_evidence_source_hash IS NULL))
				OR (r.claim_evidence_source_id IS NOT NULL AND
					(typeof(r.claim_evidence_source_version)!='integer' OR r.claim_evidence_source_version<1
					 OR typeof(r.claim_evidence_source_hash)!='blob' OR length(r.claim_evidence_source_hash)!=32
					 OR claim_document.id IS NULL OR claim_document.organisation_id IS NOT r.organisation_id
					))
				OR (r.linked_posting_id IS NOT NULL AND
					(linked_posting.id IS NULL OR linked_posting.organisation_id IS NOT r.organisation_id))`,
			root:    `0`,
			receipt: `0`,
		},
		{
			kind: BASScalarOpeningPayable,
			columns: map[string]string{
				"organisation_id": "c.organisation_id",
				"id":              "p.id",
				"conversion_id":   "p.conversion_id",
			},
			required: []string{"organisation_id", "id", "conversion_id"},
			from: `purchase_opening_payables AS p
			LEFT JOIN opening_conversions AS c ON c.id=p.conversion_id
			LEFT JOIN organisations AS owner ON owner.id=c.organisation_id
			LEFT JOIN opening_items AS item ON item.id=p.id
			LEFT JOIN accounts AS account ON account.id=item.account_id`,
			where: `(c.organisation_id=target.organisation_id AND
					(c.state IN ('POSTED','REPLACED') OR c.state IS NULL OR c.state NOT IN ('DRAFT','POSTED','REPLACED')))
				OR EXISTS (SELECT 1 FROM purchase_bas_opening_closure AS closure WHERE closure.id=c.id)
				OR c.id IS NULL
				OR (c.organisation_id IS NOT target.organisation_id AND account.organisation_id=target.organisation_id)`,
			invalid: `p.id IS NULL OR p.id='' OR p.conversion_id IS NULL OR p.conversion_id=''
				OR c.id IS NULL OR owner.id IS NULL OR c.organisation_id IS NOT target.organisation_id
				OR c.state IS NULL OR c.state NOT IN ('DRAFT','POSTED','REPLACED')
				OR (item.id IS NULL OR item.conversion_id IS NOT p.conversion_id
					 OR item.item_kind NOT IN ('SUPPLIER_OPEN_ITEM','UNALLOCATED_CREDIT')
					 OR account.id IS NULL OR account.organisation_id IS NOT c.organisation_id)`,
			root:    `CASE WHEN c.organisation_id=target.organisation_id AND c.state IN ('POSTED','REPLACED') THEN 1 ELSE 0 END`,
			receipt: `0`,
		},
		{
			kind: BASScalarOpeningConversion,
			columns: map[string]string{
				"organisation_id":     "c.organisation_id",
				"id":                  "c.id",
				"object_version":      "c.version",
				"source_hash":         "c.source_sha256",
				"journal_id":          "c.journal_id",
				"related_id":          "c.replaced_by_id",
				"journal_source_kind": "journal.source_kind",
				"source_type":         "journal.source_type",
				"source_id":           "journal.source_id",
				"source_version":      "journal.source_revision",
			},
			required: []string{"organisation_id", "id", "object_version", "source_hash"},
			from: `opening_conversions AS c
			LEFT JOIN organisations AS owner ON owner.id=c.organisation_id
			LEFT JOIN journals AS journal ON journal.id=c.journal_id
			LEFT JOIN opening_conversions AS replacement ON replacement.id=c.replaced_by_id`,
			where: `EXISTS (SELECT 1 FROM purchase_bas_opening_closure AS closure WHERE closure.id=c.id)`,
			invalid: `owner.id IS NULL OR c.organisation_id IS NOT target.organisation_id
				OR c.id IS NULL OR c.id=''
				OR typeof(c.version)!='integer' OR c.version<1
				OR typeof(c.source_sha256)!='text' OR length(c.source_sha256)!=64
				OR c.source_sha256 GLOB '*[^0-9a-f]*'
				OR c.state IS NULL OR c.state NOT IN ('DRAFT','POSTED','REPLACED')
				OR (c.financial_revision IS NOT NULL AND
					(typeof(c.financial_revision)!='integer' OR c.financial_revision<1))
				OR (c.state='DRAFT' AND (c.journal_id IS NOT NULL OR c.financial_revision IS NOT NULL))
				OR (c.state IN ('POSTED','REPLACED') AND
					(c.journal_id IS NULL OR journal.id IS NULL
					 OR journal.organisation_id IS NOT c.organisation_id
					 OR typeof(journal.source_kind)!='integer' OR journal.source_kind!=0
					 OR journal.source_type!='OPENING' OR journal.source_id IS NOT c.id
					 OR typeof(journal.source_revision)!='integer' OR journal.source_revision!=1
					 OR typeof(journal.financial_revision)!='integer' OR journal.financial_revision<1
					 OR journal.financial_revision IS NOT c.financial_revision
					 OR journal.state NOT IN ('POSTED','REVERSED')))
				OR (c.state='POSTED' AND c.replaced_by_id IS NOT NULL)
				OR (c.state='REPLACED' AND c.replaced_by_id IS NULL)
				OR (c.replaced_by_id IS NOT NULL AND
					(replacement.id IS NULL OR replacement.organisation_id IS NOT c.organisation_id))`,
			root:    `0`,
			receipt: `0`,
		},
		{
			kind: BASScalarOpeningItem,
			columns: map[string]string{
				"organisation_id": "c.organisation_id",
				"id":              "item.id",
				"conversion_id":   "item.conversion_id",
				"account_id":      "item.account_id",
				"source_type":     "item.source_type",
				"source_id":       "item.source_id",
				"source_version":  "item.source_revision",
				"source_hash":     "item.source_content_hash",
			},
			required: []string{"organisation_id", "id", "conversion_id", "account_id"},
			from: `opening_items AS item
			LEFT JOIN opening_conversions AS c ON c.id=item.conversion_id
			LEFT JOIN organisations AS owner ON owner.id=c.organisation_id
			LEFT JOIN accounts AS account ON account.id=item.account_id
			LEFT JOIN documents AS source_document
				ON item.source_type='document' AND source_document.id=item.source_id`,
			where: `EXISTS (SELECT 1 FROM purchase_bas_opening_closure AS closure WHERE closure.id=c.id)
				OR c.id IS NULL
				OR (c.organisation_id IS NOT target.organisation_id AND
					(source_document.organisation_id=target.organisation_id OR account.organisation_id=target.organisation_id))`,
			invalid: `item.id IS NULL OR item.id=''
				OR item.conversion_id IS NULL OR item.conversion_id=''
				OR c.id IS NULL OR owner.id IS NULL OR c.organisation_id IS NOT target.organisation_id
				OR account.id IS NULL OR account.organisation_id IS NOT c.organisation_id
				OR ((item.source_type IS NULL)!=(item.source_id IS NULL))
				OR ((item.source_type IS NULL)!=(item.source_revision IS NULL))
				OR ((item.source_type IS NULL)!=(item.source_content_hash IS NULL))
				OR (item.source_revision IS NOT NULL AND
					(typeof(item.source_revision)!='integer' OR item.source_revision<1))
				OR (item.source_content_hash IS NOT NULL AND
					(typeof(item.source_content_hash)!='blob' OR length(item.source_content_hash)!=32))
				OR (item.source_type='document' AND
					(source_document.id IS NULL OR source_document.organisation_id IS NOT c.organisation_id))`,
			root:    `0`,
			receipt: `0`,
		},
		{
			kind: BASScalarManualJournal,
			columns: map[string]string{
				"organisation_id":     "j.organisation_id",
				"id":                  "j.id",
				"object_version":      "j.version",
				"source_type":         "j.source_type",
				"source_id":           "j.source_id",
				"source_version":      "j.source_revision",
				"journal_source_kind": "j.source_kind",
			},
			required: []string{"organisation_id", "id", "object_version", "source_type", "source_id", "source_version", "journal_source_kind"},
			from: `journals AS j
			LEFT JOIN organisations AS owner ON owner.id=j.organisation_id`,
			where: `(j.source_type IS NULL OR j.source_type!='OPENING')
				AND (j.source_kind=0 OR typeof(j.source_kind)!='integer'
					OR j.source_kind NOT IN (0,6,7,8))
				AND (j.source_id IN (SELECT id FROM purchase_bas_source_ids)
					OR EXISTS (
						SELECT 1 FROM tax_facts AS fact
						JOIN journal_lines AS source_line ON source_line.id=fact.journal_line_id
						WHERE fact.organisation_id=j.organisation_id
							AND source_line.journal_id=j.id
							AND fact.source_id IN (SELECT id FROM purchase_bas_source_ids)
					))`,
			invalid: `owner.id IS NULL OR j.organisation_id IS NOT target.organisation_id
				OR j.id IS NULL OR j.id=''
				OR typeof(j.version)!='integer' OR j.version<1
				OR typeof(j.source_revision)!='integer' OR j.source_revision<1
				OR typeof(j.source_kind)!='integer' OR j.source_kind!=0
				OR j.source_type IS NULL OR j.source_type=''
				OR j.source_id IS NULL OR j.source_id=''
				OR j.state IS NULL OR j.state NOT IN ('DRAFT','POSTED','REVERSED')
				OR (j.state='POSTED' AND j.source_type NOT IN ('OPENING','REVERSAL') AND
					(NOT EXISTS (SELECT 1 FROM journal_lines AS line WHERE line.journal_id=j.id)
					 OR EXISTS (
						SELECT 1 FROM journal_lines AS line
						LEFT JOIN accounts AS account ON account.id=line.account_id
						WHERE line.journal_id=j.id
							AND (account.id IS NULL OR account.organisation_id IS NOT j.organisation_id
							 OR typeof(line.debit_minor)!='integer' OR typeof(line.credit_minor)!='integer')
					)))`,
			root: `0`,
			receipt: `CASE WHEN j.state='POSTED' AND j.source_kind=0
				AND j.source_type NOT IN ('OPENING','REVERSAL')
				AND EXISTS (
					SELECT 1 FROM journal_lines AS line
					JOIN accounts AS account
					  ON account.id=line.account_id AND account.organisation_id=j.organisation_id
					WHERE line.journal_id=j.id
					  AND account.report_classification IN ('balance_sheet.cash','balance_sheet.cash_equivalent')
					  AND line.credit_minor>line.debit_minor
				) THEN 1 ELSE 0 END`,
		},
	}
}
