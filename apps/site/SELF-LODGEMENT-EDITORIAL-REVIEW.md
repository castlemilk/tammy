# Tammy free self-lodgement preparation: editorial and product review

Reviewed: 5 November 2026. This work extends the existing public Tools and Tammy MDX Journal source branch. All examples are fictional and all new site paths are development source, not a confirmed production deployment.

## Published-source feature contract

| Route | Output | Strict boundary |
| --- | --- | --- |
| `/tools/bas-workpaper` | GST-only example for Simpler BAS G1, 1A and 1B; whole-dollar reference amounts and ordinary quarterly due date | Cannot determine actual GST attribution, eligibility, adjustments, PAYG, other BAS labels, due-date extensions or lodge |
| `/tools/sole-trader-tax-checklist` | Printable, browser-local checklist for individual tax-return preparation | Not a personal tax estimate; no data storage, filing, deduction eligibility decision, or company tax return |
| `/blog/how-to-lodge-bas-yourself-australia` | ATO channel steps, example workpaper, documentary evidence and submission distinction | Not direct software-based ATO lodging |
| `/blog/lodge-sole-trader-tax-return-mytax` | Individual sole-trader myTax workflow and basic example of business result | Not a company return, complete income tax estimate or substitution for prefilled return data |
| `/blog/quarterly-bas-due-dates-australia` | Q1–Q4 standard reference dates and explicit warning about changes | Not the personalised statutory deadline; does not grant a lodgement extension |

## Key ATO facts and references

- [Business.gov.au BAS lodgement](https://business.gov.au/finance/tax/business-activity-statement): Online services for business, linked myGov for sole traders, agents, other supported channels. Only the ATO performs lodgement.
- [ATO online activity statements](https://www.ato.gov.au/api/public/content/0-d8324851-ae58-4663-a160-847871068f50): electronic preparation/lodgement and confirmation.
- [ATO Simpler BAS reporting](https://www.ato.gov.au/api/public/content/0-1e92db95-a75c-4f4e-a3d4-39f43b1a3b25): standard smaller-business GST labels G1, 1A, 1B; separate obligations still possible.
- [ATO BAS reporting and reference dates](https://www.ato.gov.au/api/public/content/0-9fc804ad-a043-4a35-b540-16b71d9ca9bf): standard Q1 28 Oct, Q2 28 Feb, Q3 28 Apr, Q4 28 July; possible electronic and agent extensions, public holidays and other exceptions.
- [ATO Business/sole-trader income or loss](https://www.ato.gov.au/api/public/content/0-902d3332-9e81-4498-acd1-6cea96aa9720): sole trader business income belongs within the individual's return via myTax.
- [ATO BAS whole-dollar instructions](https://www.ato.gov.au/api/public/content/0-30bd4b2d-40c7-42bb-984a-adf4ee2fa327): omit cents at BAS reporting labels without rounding up. The workpaper preserves internal cent values separately.

## Arithmetic and misuse guardrails

- GST from an inclusive standard-taxable amount: one-eleventh, with the existing Tammy `gstBreakdown` cent-rounding model, then omit cents only for illustrative BAS label display.
- G1 includes the user-confirmed other reportable sales amount; the tool doesn't classify it. An unclassified total cannot establish the final G1.
- Input purchases are assumed *fully creditable* ordinary 10% taxable purchases. Mixed, apportioned, input-taxed or adjustment conditions are unsupported.
- Illustrative example: $22,000 taxable sales inclusive ⇒ $2,000 GST. $7,150 eligible purchases inclusive ⇒ $650 GST credit. G1 $22,000, 1A $2,000, 1B $650, difference $1,350 before any other BAS obligations.
- Standard due date reference may fall on a weekend (e.g. 28 Feb 2027). Actual ATO date must be taken from the activity statement; the tool does not shift dates or presume extensions.
- Sole trader self-tax-preparation checklist is local in memory; it does not store source records, estimates, ABNs or personal data. Printing occurs through the user's browser.
- Existing canonical host stays at the ChatGPT Sites URL until the customer verifies publishing and `tammy.tax` actually serves the site. No Vercel project for Tammy was found in the current linked team, so this source PR does not create or deploy one.

## Release

Run the site CI: typecheck, oxlint, Vitest, MDX editorial lint, SEO sitemap/robots consistency, mocked live-host preflight and production Vinext build. Inspect the live user-visible browser, HTML render and ATO links separately. No assertion of real ATO integration, ATO approval, filing, public paid accounting or custom-domain launch.
