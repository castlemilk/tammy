# Supported BAS rules reference

Verified against official ATO sources on 2026-10-05, with purchase evidence and claim rules rechecked against current legislation and official ATO sources on 2026-10-06. These references constrain implementation and golden tests; they do not establish any user's registration, eligibility, actual statement obligations, due date, entitlement or external acceptance.

## Form labels and exact arithmetic

The supported Simpler GST labels are G1, 1A and 1B. Applicable PAYGW/PAYGI sections remain separate obligations. [ATO software developer Simpler BAS guidance](https://softwaredevelopers.ato.gov.au/SimplerBAS).

Atomic monetary form inputs omit cents. Calculate reported W5 from reported W2 + W3 + W4 and copy W5 to label 4. W1 is never added to W5. Preserve independent exact-cent workpapers and rounding differences. [ATO PAYG withholding labels](https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/business-activity-statements-bas/pay-as-you-go-payg-withholding), [ATO BAS preparation tips](https://www.ato.gov.au/api/public/content/0-30bd4b2d-40c7-42bb-984a-adf4ee2fa327).

The amount method copies notified T7 to 5A. The rate method applies the notified exact T2 percentage to reportable T1, producing T11 and then 5A. T1 requires its own reviewed instalment-income evidence and is not interchangeable with the Overview business-income metric. This release initiates no variation and accepts no unsupported variation/credit fields. [ATO PAYGI instructions](https://www.ato.gov.au/api/public/content/0-c9dc388e-c500-43a2-863b-bbdc01b6ff3a), [ATO amount/rate explanation](https://www.ato.gov.au/api/public/content/0-4b6d5184-5815-43dc-9cb6-7b3f3500889f), [ATO example statement](https://www.ato.gov.au/api/public/content/533b1e6c-2d50-49ef-a7cf-b2b6e28acbb4_CAS27842BASU_pdf).

For this supported set of obligations, form totals derive from reported component labels: 8A = 1A + 4 + 5A; 8B = 1B; label 9 is the nonnegative magnitude of their difference with a separately retained payable/refundable/nil direction. Unsupported other debit/credit labels block a complete statement rather than contributing assumed zeros. Golden tests must cover component-cent truncation separately from exact-cent total calculations.

## Purchase attribution and claim timing

Cash GST credits follow actual paid consideration to the eligible extent. Non-cash ordinarily follows the earlier invoice/payment event; the implemented slice explicitly rejects advances before invoice date, so its normal event is the retained invoice. Registration and evidence eligibility remain required. [GSTR 2000/13, paragraphs 12–18](https://www.ato.gov.au/law/view/document?LocID=%22GST%2FGSTR200013%2FNAT%2FATO%2Ffp24%22&PiT=20241204000001).

On 2026-10-06, paid-extent and partly creditable acquisition rules were rechecked against [GSTR 2000/29, paragraph 18](https://www.ato.gov.au/law/view/document?LocID=%22GST%2FGSTR200029%2FNAT%2FATO%2FatH215%22&PiT=20150806000001) and [GSTR 2006/4, paragraph 44](https://www.ato.gov.au/law/view/document?LocID=%22GST%2FGSTR20064%2FNAT%2FATO%2Fft7%22&PiT=20240814000001). The repository's exact-cent implementation choice extends the existing invoice cumulative allocation: independently floor cumulative supplier GST and reviewed eligible GST against the allocated gross paid, then retain payment deltas. This rounding choice is an implementation inference, not a rounding prescription in those rulings. Cumulative eligible GST never exceeds supplier GST; independently rounded individual deltas can cross different cent boundaries. Nesting the eligible floor through rounded supplier GST would add an artificial delay and is not the chosen model.

Keep invoice date, actual payment date, evidence-held date, reviewed claim period and suspense-release posting date separate. Late evidence does not justify guessing a tax date or silently backdating a credit. The ordinary four-year credit limit starts at the due date of the earliest otherwise eligible statement, disregarding the requirement to hold a tax invoice; each part payment can have its own starting period. Exceptional or indeterminable limits remain unsupported review. [ATO GST credit time limits](https://www.ato.gov.au/api/public/content/0-a7296cdd-cec8-49dc-b597-1d94ac991a01), [ATO GST credit guidance](https://www.ato.gov.au/api/public/content/0-1e92db95-a75c-4f4e-a3d4-39f43b1a3b25).

A later claim of an unclaimed credit differs from correction of a GST error or a later adjustment to an originally correct transaction. This workflow does not automatically revise/carry differences or claim the same source twice. [ATO explanation of GST errors](https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst/in-detail/managing-gst-in-your-business/reporting-paying-and-activity-statements/correcting-gst-errors/what-is-a-gst-error).

The current GST Act, compilation 96 effective 2026-01-01, separates invoice-holding delay under section 29-10(3) from later-period election under sections 29-10(4)–(6). The latter requires an otherwise attributable credit not already included in that period's assessment and approved-form notification; it does not require an existing original assessment. The election cannot be revoked or amended. Local draft, attribution and deferral records retain intended-period and reviewed history facts; they do not establish completed external notification or assessment. Unknown filing/invoice-held history or disputed application remains typed review. [Current GST Act, section 29-10](https://www.legislation.gov.au/C2004A00446/2026-01-01/2026-01-01/text/original/epub/OEBPS/document_1/document_1.html).

The amendment commenced on 2026-01-01 and includes historical application and transitional exceptions. No retrieved primary ATO source established the current approved notification form or whether a later BAS itself supplies it. Do not invent a separate mandatory advance receipt or claim that local review completes the election. [Act 72/2025, commencement and Schedule 4 items 36–38 and 43](https://www.legislation.gov.au/C2025A00072/asmade/2024-07-01/text/original/epub/OEBPS/document_1/document_1.html).

## Purchase evidence exceptions

The low-value tax-invoice threshold is $75 excluding GST, normally $82.50 including GST for a fully taxable 10% transaction. Aggregate taxable supplies within one transaction; do not apply the threshold separately to allocations or infer it from mixed-transaction gross alone. Golden cases include $82.50, $82.51 and five $20 taxable items in one transaction. [GST Regulations 2019, compilation 5, section 29-80.01](https://www.legislation.gov.au/F2019L00417/2025-11-01/2025-11-01/text/original/pdf), [GSTR 2013/1, paragraphs 58 and 91](https://www.ato.gov.au/law/view/document?LocID=%22GST%2FGSTR20131%2FNAT%2FATO%2Ffp81%22&PiT=20231128000001).

Retain supporting records and explicit reviewed entitlement. A small amount or OCR result alone does not prove registration, GST charged, business use or eligible extent. The ATO describes invoices/receipts/dockets or, where unavailable, a purchase record containing supplier name/ABN, date, items and amount. [ATO GST credit guidance, small purchases](https://www.ato.gov.au/api/public/content/0-1e92db95-a75c-4f4e-a3d4-39f43b1a3b25).

A specific waiver requires an identified, applicable, implemented rule and its retained supporting facts. Unknown rules remain unsupported review. Keep a Commissioner decision treating a particular document as a tax invoice separate from a class waiver; user confirmation does not exercise that discretion. [Current GST Act, sections 29-10(3) and 29-70(1B)](https://www.legislation.gov.au/C2004A00446/2026-01-01/2026-01-01/text/original/epub/OEBPS/document_1/document_1.html).

## Claim-deadline golden cases

Use the actual required return day for each otherwise eligible source portion, retaining its evidence. Ordinary on-time posting/payment needs no invented delayed-claim due-date entry. Preserve the following civil-date cases:

| Required return day | Last ordinary claim day | First outside day | Source |
| --- | --- | --- | --- |
| 2017-07-28 | 2021-07-28 | 2021-07-29 | [ATO Tim example](https://www.ato.gov.au/api/public/content/0-a7296cdd-cec8-49dc-b597-1d94ac991a01) |
| 2020-04-28 | 2024-04-28 | 2024-04-29 | [ATO Mia example](https://www.ato.gov.au/api/public/content/0-1e92db95-a75c-4f4e-a3d4-39f43b1a3b25) |
| 2024-02-29 | 2028-02-29 | 2028-03-01 | Implementation inference from the ordinary anniversary rule; not an ATO worked leap-day example. |

Do not add the assessment period of review's extra day or extend the ordinary credit limit because that review is extended. [ATO period of review, Esmeralda example](https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/guide-to-self-assessment-for-indirect-taxes/period-of-review). Do not automatically roll the entitlement deadline to the next working day. The missing 2100 anniversary for 2096-02-29, and dates beyond the supported calendar, remain indeterminable review until a verified rule covers them; Go date normalization is not legal authority. Compare civil dates, not four times 365 days or elapsed UTC durations.

## Calendar and due-date guidance

GST quarters use July–September, October–December, January–March and April–June irrespective of financial-year end. Normal quarterly guidance dates are 28 October, 28 February, 28 April and 28 July respectively; normal monthly guidance is the following month's 21st day. Actual due dates appear on the user's statement and can depend on weekends/public holidays, reporting arrangements and externally granted extensions. Retain confirmed dates and their source; do not infer any online/agent/disaster extension or assume a nominal due date establishes a reviewed claim deadline. [ATO BAS due dates](https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/business-activity-statements-bas/due-dates-for-lodging-and-paying-your-bas).

## Verification provenance

Direct ATO public-content API fetches returned HTTP 403 or browser retrieval errors on these dates. Official indexed contents were available, including the sample statement fields and worked deadline examples. Complete current GST Act text and the current regulations PDF were retrieved on 2026-10-06. The linked GSTR 2013/1 point-in-time text supports the cited paragraphs; its newest exact revision date was not independently established. Implementation must retain a rule/form/calculator version and inspectable golden calculation traces; no live ATO service is invoked to prepare, freeze, export or record external observations.
