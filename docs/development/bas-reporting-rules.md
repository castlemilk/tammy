# Supported BAS rules reference

Verified against official ATO sources on 2026-10-05 for the reporting programme. These references constrain implementation and golden tests; they do not establish any user's registration, eligibility, actual statement obligations, due date, entitlement or external acceptance.

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

## Calendar and due-date guidance

GST quarters use July–September, October–December, January–March and April–June irrespective of financial-year end. Normal quarterly guidance dates are 28 October, 28 February, 28 April and 28 July respectively; normal monthly guidance is the following month's 21st day. Actual due dates appear on the user's statement and can depend on weekends/public holidays, reporting arrangements and externally granted extensions. Retain confirmed dates and their source; do not infer any online/agent/disaster extension or assume a nominal due date establishes a reviewed claim deadline. [ATO BAS due dates](https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/business-activity-statements-bas/due-dates-for-lodging-and-paying-your-bas).

## Verification provenance

Direct ATO public-content API fetches returned HTTP 403 in the browser tool on this date. The same official pages were retrieved through indexed official-source search results, including the sample statement fields. Implementation must retain a rule/form/calculator version and inspectable golden calculation traces; no live ATO service is invoked to prepare, freeze, export or record external observations.
