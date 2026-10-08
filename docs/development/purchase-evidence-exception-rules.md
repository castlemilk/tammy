# Purchase evidence exception rule, 2026-10-07

The core currently implements `LOW_VALUE_ACQUISITION` only. A low amount or OCR result does not establish this exception. The command requires the existing explicit registration, business-use and evidence review, the typed exception review, and retained supporting-record requirements confirmation. Specific retained waivers and Commissioner discretion remain unsupported.

The rule is pinned to [GST Regulations 2019, compilation 5, 1 November 2025, regulation 29-80.01](https://www.legislation.gov.au/F2019L00417/2025-11-01/2025-11-01/text/original/pdf). Its $75 value threshold was verified from the official compilation on 2026-10-07. [GSTR 2013/1](https://www.ato.gov.au/law/view/document?LocID=%22GST%2FGSTR20131%2FNAT%2FATO%2Ffp81%22&PiT=20231128000001), consolidated ruling dated 3 April 2019 (point-in-time 28 November 2023), paragraphs 58 and 91, applies the threshold to aggregate taxable supplies in a single transaction. The indexed official ATO text was used because direct document/PDF retrieval returned an access error. The exact sources, access date and application limits are retained in the rule bytes.

Because reviewed `DocumentCandidate` records contain subtotal, GST and total but no independently classified taxable line items, this implementation conservatively checks the complete transaction's reviewed subtotal at or below 7,500 AUD cents. It does not subtract private/noncreditable allocations or infer a taxable-only amount from OCR. At a 10% GST charge, the boundary examples are $82.50 accepted and $82.51 rejected; five $20 supplies in the same transaction exceed the aggregate threshold. The application starts on the verified compilation date, 2025-11-01; this is an implementation support boundary, not a statement that the legal threshold began on that date.

The acquisition document itself must appear among the supporting records. Every supporting document must belong to the same organisation, be reviewed at the referenced version, and match its original retained SHA-256 and actual bytes. The evidence source must also appear among those records. These checks retain the reviewer's confirmation of the actual supporting-record requirements. The current candidate schema does not contain supplier ABN or item descriptions; the core does not invent or extract those facts to approve an exception.

The core pin is:

| Field | Value |
| --- | --- |
| Source ID | `019a4210-0000-7000-8000-000000000075` |
| Source version | `1` |
| SHA-256 | `11cc08f62cb53e5e896e0af18ad2056f1bf0a572a4cc2b98c342de6ba7398157` |
| Bundle version | `au-gst-tax-invoice-low-value-v1` |
| Bundle type | `AU_GST_PURCHASE_TAX_INVOICE_EXCEPTION` |
| Organisation owner | SQL `NULL` |

`Repository.RetainEvidenceRules(ctx)` installs the exact embedded bytes in the existing immutable `rule_bundles` table during activation. Repeating installation verifies the retained owner, type, version, hash, bytes, effective dates and timestamp; a mismatched row is rejected. It does not reserve a financial revision. `Repository.LowValueExceptionRule(ctx)` reads the verified descriptor. `Repository.ResolveEvidenceRule(ctx, organisationID, acquisitionDocumentID, review)` additionally verifies transaction and supporting-record facts before providing the pure domain helper with a `ReviewedExceptionRule`.

Posting, current payments, evidence append, retained evidence reads and full historical accounting-event verification use that same resolver. Appending evidence changes only the tax source revision and accounting version. It cannot release a historical suspense credit. A subsequent cash payment can use newly retained evidence that was actually held by that payment's date and trusted command as-of time; previous payment and posting results remain immutable.

The descriptor has no new RPC. The UI may use the exact bundled pin when offering this narrowly supported exception, with the core resolving and checking every submitted reference. A future descriptor transport must preserve the same verification and must not treat a displayed rule reference as credit or claim authority.
