package purchases

// purchaseBASModernFamilies exposes only raw identities and direct retained
// references from the nine schema-14 purchase accounting families. The shared
// projector validates cell storage classes, hashes and the mandatory
// organisation/id columns before these family-specific integrity predicates.
func purchaseBASModernFamilies() []purchaseBASScalarFamily {
	const (
		attributeCommand = "tammy.v1.PurchaseService.AttributePurchaseGst"
	)
	return []purchaseBASScalarFamily{
		{
			kind: BASScalarPosting,
			columns: map[string]string{
				"organisation_id":        "p.organisation_id",
				"id":                     "p.id",
				"document_id":            "p.document_id",
				"source_id":              "p.document_id",
				"source_version":         "p.reviewed_version",
				"journal_id":             "p.journal_id",
				"journal_source_kind":    "j.source_kind",
				"source_type":            "j.source_type",
				"source_hash":            "p.original_content_hash",
				"record_hash":            "p.source_hash",
				"reviewed_snapshot_hash": "p.reviewed_snapshot_hash",
			},
			required: []string{"document_id", "source_id", "source_version", "source_hash", "record_hash", "reviewed_snapshot_hash"},
			from: `purchase_postings_v1 AS p
			 LEFT JOIN organisations AS owner ON owner.id=p.organisation_id
			 LEFT JOIN documents AS d ON d.id=p.document_id
			 LEFT JOIN journals AS j ON j.id=p.journal_id`,
			where: `p.organisation_id=target.organisation_id
			 OR d.organisation_id=target.organisation_id
			 OR j.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR p.id IS NULL OR p.document_id IS NULL
			 OR d.id IS NULL OR d.organisation_id IS NOT p.organisation_id
			 OR typeof(p.reviewed_version)!='integer' OR p.reviewed_version<1
			 OR p.original_content_hash IS NULL OR typeof(p.original_content_hash)!='blob' OR length(p.original_content_hash)!=32
			 OR p.reviewed_snapshot_hash IS NULL OR typeof(p.reviewed_snapshot_hash)!='blob' OR length(p.reviewed_snapshot_hash)!=32
			 OR p.source_hash IS NULL OR typeof(p.source_hash)!='blob' OR length(p.source_hash)!=32
			 OR typeof(p.gross_minor)!='integer' OR p.gross_minor<0
			 OR ((p.gross_minor=0)!=(p.journal_id IS NULL))
			 OR (p.journal_id IS NOT NULL AND (j.id IS NULL OR j.organisation_id IS NOT p.organisation_id
			   OR j.source_kind!=6 OR j.source_type!='PAYABLE' OR j.source_id IS NOT p.id))`,
			root:    "1",
			receipt: "0",
		},
		{
			kind: BASScalarPostingAllocation,
			columns: map[string]string{
				"organisation_id": "a.organisation_id",
				"id":              "a.posting_id",
				"posting_id":      "a.posting_id",
				"sequence":        "a.sequence",
				"account_id":      "a.account_id",
				"account_version": "a.account_version",
			},
			required: []string{"posting_id", "sequence", "account_id", "account_version"},
			from: `purchase_posting_allocations_v1 AS a
			 LEFT JOIN organisations AS owner ON owner.id=a.organisation_id
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=a.posting_id
			 LEFT JOIN accounts AS account ON account.id=a.account_id`,
			where: `a.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR account.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR a.posting_id IS NULL OR a.account_id IS NULL
			 OR p.id IS NULL OR p.organisation_id IS NOT a.organisation_id
			 OR account.id IS NULL OR account.organisation_id IS NOT a.organisation_id
			 OR typeof(a.sequence)!='integer' OR a.sequence<1
			 OR typeof(a.account_version)!='integer' OR a.account_version<1`,
			root:    "0",
			receipt: "0",
		},
		{
			kind: BASScalarPayment,
			columns: map[string]string{
				"organisation_id":     "pay.organisation_id",
				"id":                  "pay.id",
				"posting_id":          "pay.posting_id",
				"account_id":          "pay.bank_account_id",
				"journal_id":          "pay.journal_id",
				"journal_source_kind": "j.source_kind",
				"source_type":         "j.source_type",
				"accounting_version":  "pay.accounting_version",
				"record_hash":         "pay.source_hash",
			},
			required: []string{"posting_id", "account_id", "journal_id", "journal_source_kind", "source_type", "accounting_version", "record_hash"},
			from: `purchase_payments_v1 AS pay
			 LEFT JOIN organisations AS owner ON owner.id=pay.organisation_id
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=pay.posting_id
			 LEFT JOIN accounts AS bank ON bank.id=pay.bank_account_id
			 LEFT JOIN journals AS j ON j.id=pay.journal_id`,
			where: `pay.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR bank.organisation_id=target.organisation_id
			 OR j.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR pay.id IS NULL OR pay.posting_id IS NULL OR pay.bank_account_id IS NULL OR pay.journal_id IS NULL
			 OR p.id IS NULL OR p.organisation_id IS NOT pay.organisation_id
			 OR bank.id IS NULL OR bank.organisation_id IS NOT pay.organisation_id
			 OR j.id IS NULL OR j.organisation_id IS NOT pay.organisation_id
			 OR j.source_kind!=7 OR j.source_type!='BANKING' OR j.source_id IS NOT pay.id
			 OR typeof(pay.accounting_version)!='integer' OR pay.accounting_version<1
			 OR pay.source_hash IS NULL OR typeof(pay.source_hash)!='blob' OR length(pay.source_hash)!=32`,
			root:    "0",
			receipt: "1",
		},
		{
			kind: BASScalarPaymentAllocation,
			columns: map[string]string{
				"organisation_id":             "a.organisation_id",
				"id":                          "a.payment_id",
				"payment_id":                  "a.payment_id",
				"posting_id":                  "a.posting_id",
				"sequence":                    "a.sequence",
				"posting_allocation_sequence": "a.posting_allocation_sequence",
			},
			required: []string{"payment_id", "posting_id", "sequence", "posting_allocation_sequence"},
			from: `purchase_payment_allocations_v1 AS a
			 LEFT JOIN organisations AS owner ON owner.id=a.organisation_id
			 LEFT JOIN purchase_payments_v1 AS pay ON pay.id=a.payment_id
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=a.posting_id
			 LEFT JOIN purchase_posting_allocations_v1 AS pa
			   ON pa.posting_id=a.posting_id AND pa.sequence=a.posting_allocation_sequence`,
			where: `a.organisation_id=target.organisation_id
			 OR pay.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR pa.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR a.payment_id IS NULL OR a.posting_id IS NULL
			 OR pay.id IS NULL OR pay.organisation_id IS NOT a.organisation_id OR pay.posting_id IS NOT a.posting_id
			 OR p.id IS NULL OR p.organisation_id IS NOT a.organisation_id
			 OR pa.posting_id IS NULL OR pa.organisation_id IS NOT a.organisation_id
			 OR typeof(a.sequence)!='integer' OR a.sequence<1
			 OR typeof(a.posting_allocation_sequence)!='integer' OR a.posting_allocation_sequence<1`,
			root:    "0",
			receipt: "0",
		},
		{
			kind: BASScalarEvidence,
			columns: map[string]string{
				"organisation_id":    "e.organisation_id",
				"id":                 "e.id",
				"posting_id":         "e.posting_id",
				"document_id":        "e.document_id",
				"accounting_version": "e.accounting_version",
				"source_id":          "e.source_document_id",
				"source_version":     "e.source_version",
				"source_hash":        "e.source_hash",
				"record_hash":        "e.record_hash",
			},
			required: []string{"posting_id", "document_id", "accounting_version", "record_hash"},
			from: `purchase_tax_evidence_v1 AS e
			 LEFT JOIN organisations AS owner ON owner.id=e.organisation_id
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=e.posting_id
			 LEFT JOIN documents AS d ON d.id=e.document_id
			 LEFT JOIN documents AS source_doc ON source_doc.id=e.source_document_id`,
			where: `e.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR d.organisation_id=target.organisation_id
			 OR source_doc.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR e.id IS NULL OR e.posting_id IS NULL OR e.document_id IS NULL
			 OR p.id IS NULL OR p.organisation_id IS NOT e.organisation_id OR p.document_id IS NOT e.document_id
			 OR d.id IS NULL OR d.organisation_id IS NOT e.organisation_id
			 OR typeof(e.accounting_version)!='integer' OR e.accounting_version<1
			 OR e.record_hash IS NULL OR typeof(e.record_hash)!='blob' OR length(e.record_hash)!=32
			 OR ((e.source_document_id IS NULL AND (e.source_version IS NOT NULL OR e.source_hash IS NOT NULL))
			   OR (e.source_document_id IS NOT NULL AND (e.source_version IS NULL OR e.source_hash IS NULL)))
			 OR (e.source_document_id IS NOT NULL AND (source_doc.id IS NULL OR source_doc.organisation_id IS NOT e.organisation_id))`,
			root:    "0",
			receipt: "0",
		},
		{
			kind: BASScalarEvent,
			columns: map[string]string{
				"organisation_id":     "ev.organisation_id",
				"id":                  "ev.operation_key",
				"operation_key":       "ev.operation_key",
				"scope_kind":          "ev.scope_kind",
				"scope_id":            "ev.scope_id",
				"posting_id":          "ev.posting_id",
				"document_id":         "ev.document_id",
				"opening_payable_id":  "ev.opening_payable_id",
				"conversion_id":       "conversion.id",
				"accounting_version":  "ev.accounting_version",
				"journal_id":          "ev.journal_id",
				"journal_source_kind": "j.source_kind",
				"source_type":         "j.source_type",
				"record_hash":         "ev.result_sha256",
			},
			required: []string{"operation_key", "scope_kind", "scope_id", "accounting_version", "record_hash"},
			from: `purchase_accounting_events_v1 AS ev
			 LEFT JOIN organisations AS owner ON owner.id=ev.organisation_id
			 LEFT JOIN command_idempotency_v1 AS command ON command.operation_key=ev.operation_key
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=ev.posting_id
			 LEFT JOIN documents AS d ON d.id=ev.document_id
			 LEFT JOIN purchase_opening_payables AS payable ON payable.id=ev.opening_payable_id
			 LEFT JOIN opening_conversions AS conversion ON conversion.id=payable.conversion_id
			 LEFT JOIN journals AS j ON j.id=ev.journal_id`,
			where: `ev.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR d.organisation_id=target.organisation_id
			 OR conversion.organisation_id=target.organisation_id
			 OR j.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR ev.operation_key IS NULL OR command.operation_key IS NULL
			 OR typeof(ev.accounting_version)!='integer' OR ev.accounting_version<1
			 OR ev.result_sha256 IS NULL OR typeof(ev.result_sha256)!='blob' OR length(ev.result_sha256)!=32
			 OR (ev.scope_kind='POSTING' AND (ev.posting_id IS NULL OR ev.scope_id IS NOT ev.posting_id
			   OR ev.document_id IS NOT NULL OR ev.opening_payable_id IS NOT NULL
			   OR p.id IS NULL OR p.organisation_id IS NOT ev.organisation_id))
			 OR (ev.scope_kind='DOCUMENT' AND (ev.document_id IS NULL OR ev.scope_id IS NOT ev.document_id
			   OR ev.posting_id IS NOT NULL OR ev.opening_payable_id IS NOT NULL
			   OR d.id IS NULL OR d.organisation_id IS NOT ev.organisation_id))
			 OR (ev.scope_kind='OPENING_PAYABLE' AND (ev.opening_payable_id IS NULL OR ev.scope_id IS NOT ev.opening_payable_id
			   OR ev.posting_id IS NOT NULL OR ev.document_id IS NOT NULL
			   OR payable.id IS NULL OR conversion.id IS NULL OR conversion.organisation_id IS NOT ev.organisation_id))
			 OR ev.scope_kind NOT IN ('POSTING','DOCUMENT','OPENING_PAYABLE')
			 OR (ev.journal_id IS NOT NULL AND (j.id IS NULL OR j.organisation_id IS NOT ev.organisation_id))`,
			root:    "0",
			receipt: "0",
		},
		{
			kind: BASScalarDeferral,
			columns: map[string]string{
				"organisation_id":    "dfr.organisation_id",
				"id":                 "dfr.id",
				"posting_id":         "dfr.posting_id",
				"document_id":        "dfr.document_id",
				"operation_key":      "ev.operation_key",
				"accounting_version": "dfr.accounting_version",
				"record_hash":        "dfr.source_hash",
			},
			required: []string{"posting_id", "document_id", "operation_key", "accounting_version", "record_hash"},
			from: `purchase_gst_deferrals_v1 AS dfr
			 LEFT JOIN organisations AS owner ON owner.id=dfr.organisation_id
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=dfr.posting_id
			 LEFT JOIN documents AS doc ON doc.id=dfr.document_id
			 LEFT JOIN purchase_accounting_events_v1 AS ev
			   ON ev.organisation_id=dfr.organisation_id AND ev.scope_kind='POSTING'
			  AND ev.scope_id=dfr.posting_id AND ev.accounting_version=dfr.accounting_version
			  AND ev.command_type='tammy.v1.PurchaseService.DeferPurchaseGstClaim'`,
			where: `dfr.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR doc.organisation_id=target.organisation_id
			 OR ev.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR dfr.id IS NULL OR dfr.posting_id IS NULL OR dfr.document_id IS NULL
			 OR p.id IS NULL OR p.organisation_id IS NOT dfr.organisation_id OR p.document_id IS NOT dfr.document_id
			 OR doc.id IS NULL OR doc.organisation_id IS NOT dfr.organisation_id
			 OR ev.operation_key IS NULL
			 OR typeof(dfr.accounting_version)!='integer' OR dfr.accounting_version<1
			 OR dfr.source_hash IS NULL OR typeof(dfr.source_hash)!='blob' OR length(dfr.source_hash)!=32`,
			root:    "0",
			receipt: "0",
		},
		{
			kind: BASScalarDeferralPortion,
			columns: map[string]string{
				"organisation_id":         "portion.organisation_id",
				"id":                      "portion.deferral_id",
				"deferral_id":             "portion.deferral_id",
				"sequence":                "portion.sequence",
				"posting_id":              "portion.posting_id",
				"source_kind":             "portion.source_kind",
				"payment_id":              "portion.payment_id",
				"allocation_sequence":     "portion.allocation_sequence",
				"due_date_source_id":      "portion.due_date_source_id",
				"due_date_source_version": "portion.due_date_source_version",
				"due_date_source_hash":    "portion.due_date_source_hash",
				"evidence_id":             "portion.evidence_id",
				"record_hash":             "portion.source_hash",
			},
			required: []string{"deferral_id", "sequence", "posting_id", "source_kind", "allocation_sequence", "due_date_source_id", "due_date_source_version", "due_date_source_hash", "evidence_id", "record_hash"},
			from: `purchase_gst_deferral_portions_v1 AS portion
			 LEFT JOIN organisations AS owner ON owner.id=portion.organisation_id
			 LEFT JOIN purchase_gst_deferrals_v1 AS dfr
			   ON dfr.id=portion.deferral_id
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=portion.posting_id
			 LEFT JOIN purchase_tax_evidence_v1 AS evidence ON evidence.id=portion.evidence_id
			 LEFT JOIN purchase_posting_allocations_v1 AS posting_allocation
			   ON portion.source_kind='POSTING' AND portion.payment_id IS NULL
			  AND posting_allocation.posting_id=portion.posting_id
			  AND posting_allocation.sequence=portion.allocation_sequence
			 LEFT JOIN purchase_payments_v1 AS pay ON pay.id=portion.payment_id
			 LEFT JOIN purchase_payment_allocations_v1 AS payment_allocation
			   ON portion.source_kind='PAYMENT' AND portion.payment_id IS NOT NULL
			  AND payment_allocation.payment_id=portion.payment_id
			  AND payment_allocation.sequence=portion.allocation_sequence
			 LEFT JOIN documents AS due_doc ON due_doc.id=portion.due_date_source_id`,
			where: `portion.organisation_id=target.organisation_id
			 OR dfr.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR evidence.organisation_id=target.organisation_id
			 OR posting_allocation.organisation_id=target.organisation_id
			 OR pay.organisation_id=target.organisation_id
			 OR payment_allocation.organisation_id=target.organisation_id
			 OR due_doc.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR portion.deferral_id IS NULL OR portion.posting_id IS NULL
			 OR dfr.id IS NULL OR dfr.organisation_id IS NOT portion.organisation_id
			 OR dfr.posting_id IS NOT portion.posting_id
			 OR p.id IS NULL OR p.organisation_id IS NOT portion.organisation_id
			 OR evidence.id IS NULL OR evidence.organisation_id IS NOT portion.organisation_id
			 OR evidence.posting_id IS NOT portion.posting_id
			 OR due_doc.id IS NULL OR due_doc.organisation_id IS NOT portion.organisation_id
			 OR portion.source_kind NOT IN ('POSTING','PAYMENT')
			 OR (portion.source_kind='POSTING' AND (portion.payment_id IS NOT NULL OR posting_allocation.posting_id IS NULL
			   OR posting_allocation.organisation_id IS NOT portion.organisation_id))
			 OR (portion.source_kind='PAYMENT' AND (portion.payment_id IS NULL OR pay.id IS NULL
			   OR pay.organisation_id IS NOT portion.organisation_id OR pay.posting_id IS NOT portion.posting_id
			   OR payment_allocation.payment_id IS NULL OR payment_allocation.posting_id IS NOT portion.posting_id
			   OR payment_allocation.organisation_id IS NOT portion.organisation_id))
			 OR typeof(portion.sequence)!='integer' OR portion.sequence<1
			 OR typeof(portion.allocation_sequence)!='integer' OR portion.allocation_sequence<1
			 OR typeof(portion.due_date_source_version)!='integer' OR portion.due_date_source_version<1
			 OR portion.due_date_source_hash IS NULL OR typeof(portion.due_date_source_hash)!='blob' OR length(portion.due_date_source_hash)!=32
			 OR portion.source_hash IS NULL OR typeof(portion.source_hash)!='blob' OR length(portion.source_hash)!=32`,
			root:    "0",
			receipt: "0",
		},
		{
			kind: BASScalarAttribution,
			columns: map[string]string{
				"organisation_id":         "a.organisation_id",
				"id":                      "a.id",
				"operation_key":           "a.operation_key",
				"posting_id":              "a.posting_id",
				"document_id":             "a.document_id",
				"accounting_version":      "a.accounting_version",
				"source_kind":             "a.source_kind",
				"payment_id":              "a.payment_id",
				"allocation_sequence":     "a.allocation_sequence",
				"due_date_source_id":      "a.due_date_source_id",
				"due_date_source_version": "a.due_date_source_version",
				"due_date_source_hash":    "a.due_date_source_hash",
				"evidence_id":             "a.evidence_id",
				"deferral_id":             "a.closes_deferral_id",
				"journal_id":              "a.journal_id",
				"journal_source_kind":     "j.source_kind",
				"source_type":             "j.source_type",
				"record_hash":             "a.source_hash",
			},
			required: []string{"operation_key", "posting_id", "document_id", "accounting_version", "source_kind", "allocation_sequence", "due_date_source_id", "due_date_source_version", "due_date_source_hash", "evidence_id", "journal_id", "journal_source_kind", "source_type", "record_hash"},
			from: `purchase_gst_attributions_v1 AS a
			 LEFT JOIN organisations AS owner ON owner.id=a.organisation_id
			 LEFT JOIN purchase_postings_v1 AS p ON p.id=a.posting_id
			 LEFT JOIN documents AS doc ON doc.id=a.document_id
			 LEFT JOIN purchase_accounting_events_v1 AS ev ON ev.operation_key=a.operation_key
			 LEFT JOIN command_idempotency_v1 AS command ON command.operation_key=a.operation_key
			 LEFT JOIN purchase_tax_evidence_v1 AS evidence
			   ON evidence.id=a.evidence_id
			 LEFT JOIN purchase_gst_deferrals_v1 AS dfr ON dfr.id=a.closes_deferral_id
			 LEFT JOIN purchase_gst_deferral_portions_v1 AS closed_portion
			   ON closed_portion.deferral_id=a.closes_deferral_id
			  AND closed_portion.source_kind=a.source_kind
			  AND closed_portion.payment_id IS a.payment_id
			  AND closed_portion.allocation_sequence=a.allocation_sequence
			 LEFT JOIN purchase_posting_allocations_v1 AS posting_allocation
			   ON a.source_kind='POSTING' AND a.payment_id IS NULL
			  AND posting_allocation.posting_id=a.posting_id
			  AND posting_allocation.sequence=a.allocation_sequence
			 LEFT JOIN purchase_payments_v1 AS pay ON pay.id=a.payment_id
			 LEFT JOIN purchase_payment_allocations_v1 AS payment_allocation
			   ON a.source_kind='PAYMENT' AND a.payment_id IS NOT NULL
			  AND payment_allocation.payment_id=a.payment_id
			  AND payment_allocation.sequence=a.allocation_sequence
			 LEFT JOIN documents AS due_doc ON due_doc.id=a.due_date_source_id
			 LEFT JOIN journals AS j ON j.id=a.journal_id`,
			where: `a.organisation_id=target.organisation_id
			 OR p.organisation_id=target.organisation_id
			 OR doc.organisation_id=target.organisation_id
			 OR ev.organisation_id=target.organisation_id
			 OR evidence.organisation_id=target.organisation_id
			 OR dfr.organisation_id=target.organisation_id
			 OR closed_portion.organisation_id=target.organisation_id
			 OR posting_allocation.organisation_id=target.organisation_id
			 OR pay.organisation_id=target.organisation_id
			 OR payment_allocation.organisation_id=target.organisation_id
			 OR due_doc.organisation_id=target.organisation_id
			 OR j.organisation_id=target.organisation_id`,
			invalid: `owner.id IS NULL OR a.id IS NULL OR a.operation_key IS NULL OR command.operation_key IS NULL
			 OR p.id IS NULL OR p.organisation_id IS NOT a.organisation_id OR p.document_id IS NOT a.document_id
			 OR doc.id IS NULL OR doc.organisation_id IS NOT a.organisation_id
			 OR ev.operation_key IS NULL OR ev.organisation_id IS NOT a.organisation_id
			 OR ev.scope_kind!='POSTING' OR ev.scope_id IS NOT a.posting_id
			 OR ev.accounting_version IS NOT a.accounting_version OR ev.command_type!='` + attributeCommand + `'
			 OR ev.journal_id IS NOT a.journal_id
			 OR evidence.id IS NULL OR evidence.organisation_id IS NOT a.organisation_id
			 OR evidence.posting_id IS NOT a.posting_id
			 OR due_doc.id IS NULL OR due_doc.organisation_id IS NOT a.organisation_id
			 OR (a.closes_deferral_id IS NOT NULL AND (dfr.id IS NULL OR dfr.organisation_id IS NOT a.organisation_id
			   OR dfr.posting_id IS NOT a.posting_id OR closed_portion.deferral_id IS NULL
			   OR closed_portion.posting_id IS NOT a.posting_id OR closed_portion.organisation_id IS NOT a.organisation_id))
			 OR a.source_kind NOT IN ('POSTING','PAYMENT')
			 OR (a.source_kind='POSTING' AND (a.payment_id IS NOT NULL OR posting_allocation.posting_id IS NULL
			   OR posting_allocation.organisation_id IS NOT a.organisation_id))
			 OR (a.source_kind='PAYMENT' AND (a.payment_id IS NULL OR pay.id IS NULL
			   OR pay.organisation_id IS NOT a.organisation_id OR pay.posting_id IS NOT a.posting_id
			   OR payment_allocation.payment_id IS NULL OR payment_allocation.posting_id IS NOT a.posting_id
			   OR payment_allocation.organisation_id IS NOT a.organisation_id))
			 OR typeof(a.accounting_version)!='integer' OR a.accounting_version<1
			 OR typeof(a.allocation_sequence)!='integer' OR a.allocation_sequence<1
			 OR typeof(a.due_date_source_version)!='integer' OR a.due_date_source_version<1
			 OR a.due_date_source_hash IS NULL OR typeof(a.due_date_source_hash)!='blob' OR length(a.due_date_source_hash)!=32
			 OR a.source_hash IS NULL OR typeof(a.source_hash)!='blob' OR length(a.source_hash)!=32
			 OR j.id IS NULL OR j.organisation_id IS NOT a.organisation_id
			 OR j.source_kind!=8 OR j.source_type!='TAX' OR j.source_id IS NOT a.operation_key`,
			root:    "0",
			receipt: "0",
		},
	}
}
